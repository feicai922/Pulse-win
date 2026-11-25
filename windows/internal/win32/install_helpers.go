package win32

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const uninstallKeyPath = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Pulse`

var (
	procMessageBoxW      = user32.NewProc("MessageBoxW")
	procSHBrowseForFoldW = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDW = shell32.NewProc("SHGetPathFromIDListW")
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
)

const (
	MB_OK              = 0x00000000
	MB_YESNO           = 0x00000004
	MB_ICONQUESTION    = 0x00000020
	MB_ICONINFORMATION = 0x00000040
	MB_ICONERROR       = 0x00000010
	IDYES              = 6
)

type browseInfoW struct {
	hwndOwner      uintptr
	pidlRoot       uintptr
	pszDisplayName *uint16
	lpszTitle      *uint16
	ulFlags        uint32
	lpfn           uintptr
	lParam         uintptr
	iImage         int32
}

// MessageBox shows a native Windows modal dialog box.
func MessageBox(hwnd uintptr, title, text string, flags uint32) int32 {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	r, _, _ := procMessageBoxW.Call(hwnd, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), uintptr(flags))
	return int32(r)
}

// BrowseForFolder opens the standard Windows folder picker dialog.
func BrowseForFolder(hwnd uintptr, title string) (string, bool) {
	t, _ := windows.UTF16PtrFromString(title)
	disp := make([]uint16, windows.MAX_PATH)
	bi := browseInfoW{
		hwndOwner:      hwnd,
		pszDisplayName: &disp[0],
		lpszTitle:      t,
		ulFlags:        0x00000001 | 0x00000040, // BIF_RETURNONLYFSDIRS | BIF_NEWDIALOGSTYLE
	}
	pidl, _, _ := procSHBrowseForFoldW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return "", false
	}
	defer procCoTaskMemFree.Call(pidl)
	out := make([]uint16, windows.MAX_PATH)
	ok, _, _ := procSHGetPathFromIDW.Call(pidl, uintptr(unsafe.Pointer(&out[0])))
	if ok == 0 {
		return "", false
	}
	return windows.UTF16ToString(out), true
}

// DefaultInstallDir returns %LOCALAPPDATA%\Programs\Pulse.
func DefaultInstallDir() string {
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return filepath.Join(local, "Programs", "Pulse")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "AppData", "Local", "Programs", "Pulse")
}

// SetLaunchAtStartupPath sets or clears the HKCU Run entry pointing to exePath.
func SetLaunchAtStartupPath(enabled bool, exePath string) error {
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
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runValueName, `"`+exePath+`"`)
}

// WriteUninstallEntry registers Pulse in Windows Apps & Features (HKCU).
func WriteUninstallEntry(installDir, exePath, version string, exeSizeBytes int64) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	_ = k.SetStringValue("DisplayName", "Pulse")
	_ = k.SetStringValue("DisplayVersion", version)
	_ = k.SetStringValue("Publisher", "Pulse")
	_ = k.SetStringValue("InstallLocation", installDir)
	_ = k.SetStringValue("DisplayIcon", exePath+",0")
	_ = k.SetStringValue("UninstallString", `"`+exePath+`" --uninstall`)
	_ = k.SetStringValue("QuietUninstallString", `"`+exePath+`" --uninstall --silent`)
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	_ = k.SetDWordValue("EstimatedSize", uint32(exeSizeBytes/1024))
	return nil
}

// RemoveUninstallEntry deletes the HKCU Uninstall registry key.
func RemoveUninstallEntry() {
	_ = registry.DeleteKey(registry.CURRENT_USER, uninstallKeyPath)
}

// StartMenuShortcutPath returns the per-user Start Menu shortcut path.
func StartMenuShortcutPath() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, _ := os.UserHomeDir()
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Pulse.lnk")
}

// DesktopShortcutPath returns the user's Desktop shortcut path.
func DesktopShortcutPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Desktop", "Pulse.lnk")
}

// CreateShortcut creates a Windows .lnk file silently via WScript.Shell COM.
func CreateShortcut(lnkPath, targetExe, workDir, description string) error {
	if err := os.MkdirAll(filepath.Dir(lnkPath), 0o755); err != nil {
		return err
	}
	esc := func(s string) string {
		return strings.ReplaceAll(s, "'", "''")
	}
	ps := fmt.Sprintf(
		"$ws = New-Object -ComObject WScript.Shell; "+
			"$s = $ws.CreateShortcut('%s'); "+
			"$s.TargetPath = '%s'; "+
			"$s.WorkingDirectory = '%s'; "+
			"$s.IconLocation = '%s,0'; "+
			"$s.Description = '%s'; "+
			"$s.Save()",
		esc(lnkPath), esc(targetExe), esc(workDir), esc(targetExe), esc(description),
	)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Run()
}

// StopOtherPulseProcesses stops any running Pulse.exe processes other than the current PID.
func StopOtherPulseProcesses() {
	myPID := os.Getpid()
	ps := fmt.Sprintf("Get-Process -Name Pulse -ErrorAction SilentlyContinue | Where-Object { $_.Id -ne %d } | Stop-Process -Force", myPID)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd.Run()
}

// ScheduleSelfDelete spawns a hidden background PowerShell cleanup to remove exePath and empty dir after process exit.
func ScheduleSelfDelete(exePath string) {
	dir := filepath.Dir(exePath)
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	ps := fmt.Sprintf(
		"for ($i=0; $i -lt 25; $i++) { Start-Sleep -Milliseconds 200; Remove-Item -LiteralPath '%s' -Force -ErrorAction SilentlyContinue; if (-not (Test-Path -LiteralPath '%s')) { break } }; "+
			"$items = Get-ChildItem -LiteralPath '%s' -Force -ErrorAction SilentlyContinue; "+
			"if (-not $items -or $items.Count -eq 0) { Remove-Item -LiteralPath '%s' -Force -ErrorAction SilentlyContinue }",
		esc(exePath), esc(exePath), esc(dir), esc(dir),
	)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", ps)
	cmd.Dir = os.TempDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd.Start()
}

// LaunchDetached starts targetExe independently of the current process.
func LaunchDetached(targetExe string) error {
	cmd := exec.Command(targetExe)
	cmd.Dir = filepath.Dir(targetExe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008} // DETACHED_PROCESS
	return cmd.Start()
}
