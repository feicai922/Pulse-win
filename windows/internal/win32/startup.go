package win32

import (
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const runValueName = "Pulse"

func IsLaunchAtStartupEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	val, _, err := k.GetStringValue(runValueName)
	return err == nil && strings.TrimSpace(val) != ""
}

func SetLaunchAtStartup(enabled bool) error {
	if !enabled {
		k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if err != nil {
			return nil
		}
		defer k.Close()
		err = k.DeleteValue(runValueName)
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runValueName, `"`+exe+`"`)
}

// ParseHotkey parses shortcuts like "Ctrl+Alt+P", "Ctrl+Shift+U", "Alt+F12"
func ParseHotkey(spec string) (mods uint32, vk uint32, ok bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, 0, false
	}
	parts := strings.Split(spec, "+")
	for i, p := range parts {
		token := strings.ToUpper(strings.TrimSpace(p))
		if i < len(parts)-1 {
			switch token {
			case "CTRL", "CONTROL":
				mods |= MOD_CONTROL
			case "ALT":
				mods |= MOD_ALT
			case "SHIFT":
				mods |= MOD_SHIFT
			case "WIN", "SUPER":
				mods |= MOD_WIN
			}
			continue
		}
		if len(token) == 1 && ((token[0] >= 'A' && token[0] <= 'Z') || (token[0] >= '0' && token[0] <= '9')) {
			vk = uint32(token[0])
			ok = true
		} else if strings.HasPrefix(token, "F") && len(token) >= 2 {
			var n int
			for _, ch := range token[1:] {
				if ch >= '0' && ch <= '9' {
					n = n*10 + int(ch-'0')
				}
			}
			if n >= 1 && n <= 12 {
				vk = uint32(0x70 + n - 1)
				ok = true
			}
		} else if token == "SPACE" {
			vk = 0x20
			ok = true
		}
	}
	if !ok || mods == 0 {
		return 0, 0, false
	}
	return mods | MOD_NOREPEAT, vk, true
}

func ShowBalloonNotification(hwnd uintptr, uid uint32, title, body string) {
	if hwnd == 0 {
		return
	}
	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = uid
	nid.UFlags = NIF_INFO
	nid.DwInfoFlags = NIIF_INFO
	copyUTF16(nid.SzInfoTitle[:], title)
	copyUTF16(nid.SzInfo[:], body)
	ShellNotifyIcon(NIM_MODIFY, &nid)
}

func copyUTF16(dst []uint16, s string) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return
	}
	n := len(u)
	if n > len(dst) {
		n = len(dst)
	}
	copy(dst[:n], u[:n])
	if len(dst) > 0 {
		dst[len(dst)-1] = 0
	}
}
