package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
)

const cursorSealedPrefix = "cursor-safe-storage:v1:"

func CursorAppDataDir() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, _ := os.UserHomeDir()
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appData, "Cursor")
}

func CursorDBPath() string {
	return filepath.Join(CursorAppDataDir(), "User", "globalStorage", "state.vscdb")
}

func HasCursorLogin() bool {
	_, err := os.Stat(CursorDBPath())
	return err == nil
}

// LoadCursorSessionCookie reads Cursor's access token from state.vscdb (and -wal if active),
// unseals Electron safeStorage (v10 AES-256-GCM using DPAPI-protected Local State key),
// and formats WorkosCursorSessionToken=<accountID>%3A%3A<token>.
func LoadCursorSessionCookie() (cookie string, reason string) {
	dbPath := CursorDBPath()
	if _, err := os.Stat(dbPath); err != nil {
		return "", "absent"
	}
	stored, err := readSQLiteItemTableValue(dbPath, "cursorAuth/accessToken")
	if err != nil || stored == "" {
		return "", "absent"
	}

	token := stored
	if strings.HasPrefix(stored, cursorSealedPrefix) {
		encB64 := strings.TrimPrefix(stored, cursorSealedPrefix)
		ciphertext, err := base64.StdEncoding.DecodeString(encB64)
		if err != nil {
			return "", "unreadable"
		}
		masterKey, err := loadCursorMasterKey()
		if err != nil {
			return "", "unreadable"
		}
		plain, err := decryptChromiumV10(ciphertext, masterKey)
		if err != nil || plain == "" {
			return "", "unreadable"
		}
		token = plain
	}

	c := buildCursorCookieFromJWT(token)
	if c == "" {
		return "", "expired"
	}
	return c, ""
}

func loadCursorMasterKey() ([]byte, error) {
	localStatePath := filepath.Join(CursorAppDataDir(), "Local State")
	raw, err := os.ReadFile(localStatePath)
	if err != nil {
		return nil, err
	}
	var doc struct {
		OSCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.OSCrypt.EncryptedKey == "" {
		return nil, fmt.Errorf("missing os_crypt.encrypted_key")
	}
	blob, err := base64.StdEncoding.DecodeString(doc.OSCrypt.EncryptedKey)
	if err != nil {
		return nil, err
	}
	// Strip "DPAPI" 5-byte prefix
	if len(blob) <= 5 || string(blob[:5]) != "DPAPI" {
		return nil, fmt.Errorf("unexpected key prefix")
	}
	return UnprotectDPAPI(blob[5:])
}

func decryptChromiumV10(ciphertext, key []byte) (string, error) {
	if len(ciphertext) < 3+12+16 {
		return "", fmt.Errorf("ciphertext too short")
	}
	prefix := string(ciphertext[:3])
	if prefix != "v10" && prefix != "v11" {
		return "", fmt.Errorf("unsupported prefix %q", prefix)
	}
	nonce := ciphertext[3:15]
	enc := ciphertext[15:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, nonce, enc, nil)
	if err != nil {
		return "", err
	}
	return decodeMaybeUTF16(plain), nil
}

func buildCursorCookieFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload := parts[1]
	payload = strings.ReplaceAll(payload, "-", "+")
	payload = strings.ReplaceAll(payload, "_", "/")
	if m := len(payload) % 4; m != 0 {
		payload += strings.Repeat("=", 4-m)
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string  `json:"sub"`
		Exp float64 `json:"exp"`
	}
	if err := json.Unmarshal(data, &claims); err != nil || claims.Sub == "" {
		return ""
	}
	if claims.Exp > 0 && time.Unix(int64(claims.Exp), 0).Before(time.Now().Add(60*time.Second)) {
		return ""
	}
	account := claims.Sub
	if idx := strings.LastIndex(claims.Sub, "|"); idx >= 0 && idx+1 < len(claims.Sub) {
		account = claims.Sub[idx+1:]
	}
	if account == "" {
		return ""
	}
	return "WorkosCursorSessionToken=" + account + "%3A%3A" + token
}

func decodeMaybeUTF16(b []byte) string {
	if len(b) >= 2 && len(b)%2 == 0 && b[0] != 0 && b[1] == 0 {
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(b[i*2 : i*2+2])
		}
		return string(utf16.Decode(u))
	}
	return string(b)
}

// --- Pure-Go SQLite B-Tree + WAL reader for VSCode/Cursor state.vscdb ---

type sqlitePages struct {
	pageSize int
	base     []byte
	wal      map[uint32][]byte
}

func openSQLitePages(dbPath string) (*sqlitePages, error) {
	base, err := os.ReadFile(dbPath)
	if err != nil {
		return nil, err
	}
	if len(base) < 100 || string(base[:16]) != "SQLite format 3\x00" {
		return nil, fmt.Errorf("not sqlite3")
	}
	ps := int(binary.BigEndian.Uint16(base[16:18]))
	if ps == 1 {
		ps = 65536
	}
	if ps < 512 {
		return nil, fmt.Errorf("invalid page size")
	}
	sp := &sqlitePages{pageSize: ps, base: base, wal: map[uint32][]byte{}}

	// Overlay committed frames from -wal if present and non-empty
	if walData, err := os.ReadFile(dbPath + "-wal"); err == nil && len(walData) >= 32 {
		magic := binary.BigEndian.Uint32(walData[0:4])
		if magic == 0x377f0682 || magic == 0x377f0683 {
			walPS := int(binary.BigEndian.Uint32(walData[8:12]))
			if walPS == ps {
				salt1 := walData[16:20]
				salt2 := walData[20:24]
				frameLen := 24 + ps
				var tx [][2]any
				for off := 32; off+frameLen <= len(walData); off += frameLen {
					fh := walData[off : off+24]
					// Stop if WAL salts no longer match current WAL generation
					if string(fh[8:12]) != string(salt1) || string(fh[12:16]) != string(salt2) {
						break
					}
					pgNo := binary.BigEndian.Uint32(fh[0:4])
					commit := binary.BigEndian.Uint32(fh[4:8])
					pgData := walData[off+24 : off+frameLen]
					tx = append(tx, [2]any{pgNo, pgData})
					if commit != 0 {
						for _, item := range tx {
							sp.wal[item[0].(uint32)] = item[1].([]byte)
						}
						tx = nil
					}
				}
			}
		}
	}
	return sp, nil
}

func (s *sqlitePages) page(num uint32) ([]byte, int) {
	if num == 0 {
		return nil, 0
	}
	headerOff := 0
	if num == 1 {
		headerOff = 100
	}
	if p, ok := s.wal[num]; ok && len(p) == s.pageSize {
		return p, headerOff
	}
	off := int(num-1) * s.pageSize
	if off < 0 || off+s.pageSize > len(s.base) {
		return nil, 0
	}
	return s.base[off : off+s.pageSize], headerOff
}

func readSQLiteItemTableValue(dbPath, targetKey string) (string, error) {
	sp, err := openSQLitePages(dbPath)
	if err != nil {
		return "", err
	}
	// Step 1: Find ItemTable rootpage in sqlite_schema (page 1)
	var itemRoot uint32
	walkTableBTree(sp, 1, func(cols [][]byte, types []uint64) bool {
		if len(cols) >= 4 && string(cols[0]) == "table" && string(cols[1]) == "ItemTable" {
			itemRoot = uint32(decodeSQLiteInt(cols[3], types[3]))
			return false
		}
		return true
	})
	if itemRoot == 0 {
		return "", fmt.Errorf("ItemTable not found")
	}

	// Step 2: Find key in ItemTable
	var result string
	var found bool
	walkTableBTree(sp, itemRoot, func(cols [][]byte, types []uint64) bool {
		if len(cols) >= 2 && string(cols[0]) == targetKey {
			result = decodeMaybeUTF16(cols[1])
			found = true
			return false
		}
		return true
	})
	if !found {
		return "", fmt.Errorf("key not found")
	}
	return result, nil
}

func walkTableBTree(sp *sqlitePages, pageNo uint32, visitor func(cols [][]byte, types []uint64) bool) bool {
	pg, hOff := sp.page(pageNo)
	if pg == nil || hOff+8 > len(pg) {
		return true
	}
	pageType := pg[hOff]
	cellCount := int(binary.BigEndian.Uint16(pg[hOff+3 : hOff+5]))
	switch pageType {
	case 0x05: // Interior table B-tree page
		if hOff+12 > len(pg) {
			return true
		}
		rightChild := binary.BigEndian.Uint32(pg[hOff+8 : hOff+12])
		ptrStart := hOff + 12
		for i := 0; i < cellCount; i++ {
			if ptrStart+i*2+2 > len(pg) {
				break
			}
			cellOff := int(binary.BigEndian.Uint16(pg[ptrStart+i*2 : ptrStart+i*2+2]))
			if cellOff+4 > len(pg) {
				continue
			}
			child := binary.BigEndian.Uint32(pg[cellOff : cellOff+4])
			if !walkTableBTree(sp, child, visitor) {
				return false
			}
		}
		return walkTableBTree(sp, rightChild, visitor)
	case 0x0D: // Leaf table B-tree page
		ptrStart := hOff + 8
		for i := 0; i < cellCount; i++ {
			if ptrStart+i*2+2 > len(pg) {
				break
			}
			cellOff := int(binary.BigEndian.Uint16(pg[ptrStart+i*2 : ptrStart+i*2+2]))
			if cellOff >= len(pg) {
				continue
			}
			payload := readLeafCellPayload(sp, pg, cellOff)
			if payload == nil {
				continue
			}
			cols, types := parseSQLiteRecord(payload)
			if cols != nil && !visitor(cols, types) {
				return false
			}
		}
	}
	return true
}

func readLeafCellPayload(sp *sqlitePages, pg []byte, cellOff int) []byte {
	payloadLen, n1 := readVarint(pg[cellOff:])
	if n1 <= 0 {
		return nil
	}
	_, n2 := readVarint(pg[cellOff+n1:])
	if n2 <= 0 {
		return nil
	}
	pStart := cellOff + n1 + n2
	p := int(payloadLen)
	if p <= 0 || p > 10*1024*1024 {
		return nil
	}
	usable := sp.pageSize
	maxLocal := usable - 35
	if p <= maxLocal {
		if pStart+p > len(pg) {
			return nil
		}
		return pg[pStart : pStart+p]
	}
	// Handle overflow pages
	minLocal := ((usable - 12) * 32 / 255) - 23
	surplus := minLocal + (p-minLocal)%(usable-4)
	local := minLocal
	if surplus <= maxLocal {
		local = surplus
	}
	if pStart+local+4 > len(pg) {
		return nil
	}
	out := make([]byte, 0, p)
	out = append(out, pg[pStart:pStart+local]...)
	nextPg := binary.BigEndian.Uint32(pg[pStart+local : pStart+local+4])
	for len(out) < p && nextPg != 0 {
		ov, _ := sp.page(nextPg)
		if ov == nil || len(ov) < 4 {
			break
		}
		nextPg = binary.BigEndian.Uint32(ov[0:4])
		take := p - len(out)
		if take > len(ov)-4 {
			take = len(ov) - 4
		}
		out = append(out, ov[4:4+take]...)
	}
	return out
}

func parseSQLiteRecord(payload []byte) ([][]byte, []uint64) {
	hdrLen, n := readVarint(payload)
	if n <= 0 || int(hdrLen) > len(payload) {
		return nil, nil
	}
	var types []uint64
	pos := n
	for pos < int(hdrLen) {
		st, k := readVarint(payload[pos:])
		if k <= 0 {
			return nil, nil
		}
		types = append(types, st)
		pos += k
	}
	cols := make([][]byte, len(types))
	dataPos := int(hdrLen)
	for i, st := range types {
		sz := serialTypeSize(st)
		if dataPos+sz > len(payload) {
			return nil, nil
		}
		cols[i] = payload[dataPos : dataPos+sz]
		dataPos += sz
	}
	return cols, types
}

func serialTypeSize(st uint64) int {
	switch st {
	case 0, 8, 9:
		return 0
	case 1:
		return 1
	case 2:
		return 2
	case 3:
		return 3
	case 4:
		return 4
	case 5:
		return 6
	case 6, 7:
		return 8
	case 10, 11:
		return 0
	default:
		if st >= 12 {
			if st%2 == 0 {
				return int((st - 12) / 2)
			}
			return int((st - 13) / 2)
		}
		return 0
	}
}

func decodeSQLiteInt(b []byte, st uint64) int64 {
	if st == 8 {
		return 0
	}
	if st == 9 {
		return 1
	}
	var v int64
	for i, by := range b {
		if i == 0 && (by&0x80) != 0 {
			v = -1
		}
		v = (v << 8) | int64(by)
	}
	return v
}

func readVarint(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < len(b) && i < 9; i++ {
		by := b[i]
		if i == 8 {
			v = (v << 8) | uint64(by)
			return v, 9
		}
		v = (v << 7) | uint64(by&0x7F)
		if (by & 0x80) == 0 {
			return v, i + 1
		}
	}
	return 0, 0
}
