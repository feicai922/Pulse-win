package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"pulse-windows/internal/model"
)

var (
	crypt32              = windows.NewLazySystemDLL("crypt32.dll")
	procCryptProtectData = crypt32.NewProc("CryptProtectData")
	procCryptUnprotect   = crypt32.NewProc("CryptUnprotectData")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

// ProtectDPAPI encrypts bytes using the current Windows user's DPAPI credentials.
func ProtectDPAPI(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, nil
	}
	in := dataBlob{cbData: uint32(len(plain)), pbData: &plain[0]}
	var out dataBlob
	r, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		0x1, // CRYPTPROTECT_UI_FORBIDDEN
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.pbData)))
	res := make([]byte, out.cbData)
	copy(res, unsafe.Slice(out.pbData, out.cbData))
	return res, nil
}

// UnprotectDPAPI decrypts bytes previously encrypted via ProtectDPAPI (or Chromium DPAPI).
func UnprotectDPAPI(cipher []byte) ([]byte, error) {
	if len(cipher) == 0 {
		return nil, errors.New("empty ciphertext")
	}
	in := dataBlob{cbData: uint32(len(cipher)), pbData: &cipher[0]}
	var out dataBlob
	r, _, err := procCryptUnprotect.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		0x1, // CRYPTPROTECT_UI_FORBIDDEN
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.pbData)))
	res := make([]byte, out.cbData)
	copy(res, unsafe.Slice(out.pbData, out.cbData))
	return res, nil
}

// KeyStore stores API keys encrypted with Windows DPAPI on disk.
type KeyStore struct {
	mu   sync.Mutex
	path string
	data map[string]string // entry key -> base64(DPAPI(secret))
}

func NewKeyStore() *KeyStore {
	ks := &KeyStore{
		path: filepath.Join(model.AppDataDir(), "keys.dat"),
		data: map[string]string{},
	}
	ks.load()
	return ks
}

func (k *KeyStore) load() {
	raw, err := os.ReadFile(k.path)
	if err != nil {
		return
	}
	var m map[string]string
	if json.Unmarshal(raw, &m) == nil && m != nil {
		k.data = m
	}
}

func (k *KeyStore) saveLocked() error {
	raw, err := json.MarshalIndent(k.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := k.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, k.path)
}

func (k *KeyStore) Get(entry string) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	encB64, ok := k.data[entry]
	if !ok || encB64 == "" {
		return ""
	}
	enc, err := base64.StdEncoding.DecodeString(encB64)
	if err != nil {
		return ""
	}
	plain, err := UnprotectDPAPI(enc)
	if err != nil {
		return ""
	}
	return string(plain)
}

func (k *KeyStore) Set(entry, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if value == "" {
		delete(k.data, entry)
		return k.saveLocked()
	}
	enc, err := ProtectDPAPI([]byte(value))
	if err != nil {
		return err
	}
	k.data[entry] = base64.StdEncoding.EncodeToString(enc)
	return k.saveLocked()
}

func (k *KeyStore) Has(entry string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.data[entry]
	return ok && v != ""
}
