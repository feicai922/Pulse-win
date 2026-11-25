package win32

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ntdll                        = windows.NewLazySystemDLL("ntdll.dll")
	iphlpapi                     = windows.NewLazySystemDLL("iphlpapi.dll")
	procNtQueryInformationProc   = ntdll.NewProc("NtQueryInformationProcess")
	procGetExtendedTcpTable      = iphlpapi.NewProc("GetExtendedTcpTable")
)

type ProcessMatch struct {
	PID     uint32
	ExePath string
	CmdLine string
	Env     map[string]string
}

// FindAntigravityServers enumerates running language_server processes belonging to Antigravity.
func FindAntigravityServers() []ProcessMatch {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return nil
	}

	var out []ProcessMatch
	for {
		name := strings.ToLower(windows.UTF16ToString(pe.ExeFile[:]))
		if strings.Contains(name, "language_server") {
			if m, ok := inspectProcessPEB(pe.ProcessID); ok {
				lowerPath := strings.ToLower(m.ExePath)
				lowerCmd := strings.ToLower(m.CmdLine)
				if strings.Contains(lowerPath, "antigravity") || strings.Contains(lowerCmd, "antigravity") {
					out = append(out, m)
				}
			}
		}
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	return out
}

func inspectProcessPEB(pid uint32) (ProcessMatch, bool) {
	const access = windows.PROCESS_QUERY_INFORMATION | windows.PROCESS_VM_READ
	h, err := windows.OpenProcess(access, false, pid)
	if err != nil {
		return ProcessMatch{}, false
	}
	defer windows.CloseHandle(h)

	var exeBuf [1024]uint16
	sz := uint32(len(exeBuf))
	exePath := ""
	if windows.QueryFullProcessImageName(h, 0, &exeBuf[0], &sz) == nil {
		exePath = windows.UTF16ToString(exeBuf[:sz])
	}

	// Read PROCESS_BASIC_INFORMATION (48 bytes on amd64)
	var pbi [48]byte
	var retLen uint32
	status, _, _ := procNtQueryInformationProc.Call(
		uintptr(h),
		0, // ProcessBasicInformation
		uintptr(unsafe.Pointer(&pbi[0])),
		uintptr(len(pbi)),
		uintptr(unsafe.Pointer(&retLen)),
	)
	if status != 0 {
		return ProcessMatch{PID: pid, ExePath: exePath}, exePath != ""
	}
	pebAddr := uintptr(binary.LittleEndian.Uint64(pbi[8:16]))
	if pebAddr == 0 {
		return ProcessMatch{PID: pid, ExePath: exePath}, false
	}

	// PEB.ProcessParameters is at offset 0x20 on 64-bit Windows
	var ptrBuf [8]byte
	if err := readProcMem(h, pebAddr+0x20, ptrBuf[:]); err != nil {
		return ProcessMatch{PID: pid, ExePath: exePath}, false
	}
	paramsAddr := uintptr(binary.LittleEndian.Uint64(ptrBuf[:]))
	if paramsAddr == 0 {
		return ProcessMatch{PID: pid, ExePath: exePath}, false
	}

	// RTL_USER_PROCESS_PARAMETERS on 64-bit Windows:
	// CommandLine UNICODE_STRING at 0x70 (Length uint16, MaxLen uint16, pad 4, Buffer uint64)
	// Environment pointer at 0x80 (uint64)
	// EnvironmentSize at 0x03F0 (optional; we read bounded chunks up to NUL\0NUL\0)
	var params [0x90]byte
	if err := readProcMem(h, paramsAddr, params[:]); err != nil {
		return ProcessMatch{PID: pid, ExePath: exePath}, false
	}
	cmdLen := binary.LittleEndian.Uint16(params[0x70:0x72])
	cmdBuf := uintptr(binary.LittleEndian.Uint64(params[0x78:0x80]))
	envPtr := uintptr(binary.LittleEndian.Uint64(params[0x80:0x88]))

	cmdLine := ""
	if cmdLen > 0 && cmdLen < 32768 && cmdBuf != 0 {
		raw := make([]byte, cmdLen)
		if readProcMem(h, cmdBuf, raw) == nil {
			cmdLine = utf16BytesToString(raw)
		}
	}

	env := map[string]string{}
	if envPtr != 0 {
		env = readProcessEnvToken(h, envPtr, "ANTIGRAVITY_SERVER_CSRF_TOKEN")
	}

	return ProcessMatch{
		PID:     pid,
		ExePath: exePath,
		CmdLine: cmdLine,
		Env:     env,
	}, true
}

func readProcMem(h windows.Handle, addr uintptr, buf []byte) error {
	var read uintptr
	return windows.ReadProcessMemory(h, addr, &buf[0], uintptr(len(buf)), &read)
}

func readProcessEnvToken(h windows.Handle, envPtr uintptr, wantKey string) map[string]string {
	res := map[string]string{}
	// Read page-by-page (aligned to 4KB boundaries) so ReadProcessMemory never fails with
	// ERROR_PARTIAL_COPY when crossing the end of the environment block allocation.
	var raw []byte
	const maxEnvBytes = 128 * 1024
	addr := envPtr
	for len(raw) < maxEnvBytes {
		pageRem := 4096 - int(addr&0xFFF)
		if pageRem <= 0 {
			pageRem = 4096
		}
		buf := make([]byte, pageRem)
		var read uintptr
		err := windows.ReadProcessMemory(h, addr, &buf[0], uintptr(pageRem), &read)
		if read == 0 {
			break
		}
		raw = append(raw, buf[:read]...)
		if hasDoubleNulUTF16(raw) || err != nil {
			break
		}
		addr += read
	}
	if len(raw) < 4 {
		return res
	}
	u16 := make([]uint16, len(raw)/2)
	for i := range u16 {
		u16[i] = binary.LittleEndian.Uint16(raw[i*2 : i*2+2])
	}
	start := 0
	for i := 0; i < len(u16); i++ {
		if u16[i] == 0 {
			if i == start {
				break // double-NUL end of environment block
			}
			entry := string(utf16.Decode(u16[start:i]))
			if eq := strings.IndexByte(entry, '='); eq > 0 {
				k := entry[:eq]
				if k == wantKey {
					res[k] = entry[eq+1:]
					return res
				}
			}
			start = i + 1
		}
	}
	return res
}

func hasDoubleNulUTF16(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	// Check aligned UTF-16 double-NUL (\0\0 \0\0) at 2-byte boundary
	n := len(b) &^ 1
	for i := 0; i+3 < n; i += 2 {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 0 && b[i+3] == 0 {
			return true
		}
	}
	return false
}

func utf16BytesToString(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[i*2 : i*2+2])
	}
	return string(utf16.Decode(u))
}

// ListeningPortsForPID returns TCP listening ports on IPv4 (loopback or 0.0.0.0) for a given PID.
func ListeningPortsForPID(pid uint32) []int {
	const (
		afINET               = 2
		tcpTableOwnerPidList = 5
		mibTcpStateListen    = 2
	)
	var size uint32 = 65536
	for attempt := 0; attempt < 3; attempt++ {
		buf := make([]byte, size)
		r, _, _ := procGetExtendedTcpTable.Call(
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&size)),
			1, // sorted
			afINET,
			tcpTableOwnerPidList,
			0,
		)
		if r == 122 { // ERROR_INSUFFICIENT_BUFFER
			continue
		}
		if r != 0 || len(buf) < 4 {
			return nil
		}
		numEntries := int(binary.LittleEndian.Uint32(buf[0:4]))
		// Each MIB_TCPROW_OWNER_PID is 24 bytes:
		// dwState(4), dwLocalAddr(4), dwLocalPort(4), dwRemoteAddr(4), dwRemotePort(4), dwOwningPid(4)
		var ports []int
		seen := map[int]bool{}
		for i := 0; i < numEntries; i++ {
			off := 4 + i*24
			if off+24 > len(buf) {
				break
			}
			state := binary.LittleEndian.Uint32(buf[off : off+4])
			localPortNet := binary.BigEndian.Uint16(buf[off+8 : off+10])
			ownerPid := binary.LittleEndian.Uint32(buf[off+20 : off+24])
			if ownerPid == pid && state == mibTcpStateListen {
				p := int(localPortNet)
				if p > 0 && !seen[p] {
					seen[p] = true
					ports = append(ports, p)
				}
			}
		}
		return ports
	}
	return nil
}
