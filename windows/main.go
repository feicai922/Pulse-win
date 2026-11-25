package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"pulse-windows/internal/auth"
	"pulse-windows/internal/i18n"
	"pulse-windows/internal/model"
	"pulse-windows/internal/store"
	"pulse-windows/internal/ui"
	"pulse-windows/internal/win32"
)

func main() {
	runtime.LockOSThread()

	settings := model.LoadSettings()
	i18n.SetLanguage(settings.Language)
	cache := store.NewCache()

	var wantUninstall, wantSilent bool
	for _, arg := range os.Args[1:] {
		if arg == "--json" {
			attachParentConsole()
			rep := store.BuildJSONReport(settings, cache)
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(rep)
			return
		}
		if arg == "--uninstall" {
			wantUninstall = true
		}
		if arg == "--silent" || strings.EqualFold(arg, "/S") {
			wantSilent = true
		}
		if arg == "--help" || arg == "-h" {
			attachParentConsole()
			fmt.Println("Pulse for Windows")
			fmt.Println("  Pulse.exe              Start floating edge panel and system tray monitor")
			fmt.Println("  Pulse.exe --json       Print cached usage readings as JSON without fetching")
			fmt.Println("  Pulse.exe --uninstall  Uninstall Pulse shortcuts, startup entry, and binary")
			return
		}
	}
	if wantUninstall {
		runUninstall(wantSilent)
		return
	}

	// Single-instance named mutex
	mName, _ := windows.UTF16PtrFromString("Global\\PulseWindowsSingleInstanceMutex")
	hMutex, err := windows.CreateMutex(nil, false, mName)
	if err == windows.ERROR_ALREADY_EXISTS {
		if hMutex != 0 {
			windows.CloseHandle(hMutex)
		}
		return
	}
	if hMutex != 0 {
		defer windows.CloseHandle(hMutex)
	}

	win32.InitDPIAwareness()
	win32.InitGDIPlus()
	defer win32.ShutdownGDIPlus()

	keys := auth.NewKeyStore()
	usageStore := store.NewUsageStore(settings, keys, cache)

	var settingsWin *ui.SettingsWindow
	var tray *ui.TrayManager
	var panel *ui.FloatingPanel

	quit := func() {
		usageStore.Stop()
		if tray != nil {
			tray.Destroy()
		}
		win32.PostQuitMessage(0)
	}

	openSettings := func(focus model.ProviderID) {
		if settingsWin != nil {
			settingsWin.Open(focus)
		}
	}

	panel = ui.NewFloatingPanel(settings, usageStore, openSettings, quit)
	if err := panel.Create(); err != nil {
		msg, _ := windows.UTF16PtrFromString(err.Error())
		title, _ := windows.UTF16PtrFromString("Pulse")
		_, _, _ = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(msg)), uintptr(unsafe.Pointer(title)), 0x10)
		return
	}

	tray = ui.NewTrayManager(settings, usageStore, panel, openSettings, quit)
	if err := tray.Create(); err != nil {
		return
	}

	settingsWin = ui.NewSettingsWindow(settings, keys, usageStore, panel, tray)

	usageStore.SetCallbacks(
		func() {
			panel.RequestRepaint()
			tray.RequestUpdate()
			settingsWin.RequestStatusSync()
		},
		func(title, body string) {
			tray.ShowNotification(title, body)
		},
	)
	usageStore.Start()

	if !settings.Snapshot().HasCompletedSetup && len(settings.OrderedEnabled()) == 0 {
		settingsWin.Open("")
	}

	win32.MessageLoop()
}

func attachParentConsole() {
	// If stdout is already redirected (e.g. piped to jq or PowerShell capture), keep it.
	if st, err := os.Stdout.Stat(); err == nil && st != nil {
		return
	}
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	const attachParentProcess = ^uintptr(0) // -1
	r, _, _ := proc.Call(attachParentProcess)
	if r != 0 {
		if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			os.Stdout = f
		}
	}
}

func runUninstall(silent bool) {
	if !silent {
		msg := i18n.T(
			"Are you sure you want to uninstall Pulse?\n\nThis removes the program binary, Start Menu & Desktop shortcuts, and startup entry.",
			"确定要卸载 Pulse 吗？\n\n这将移除主程序、桌面及开始菜单快捷方式和开机自启动项。",
		)
		res := win32.MessageBox(0, i18n.T("Uninstall Pulse", "卸载 Pulse"), msg, win32.MB_YESNO|win32.MB_ICONQUESTION)
		if res != win32.IDYES {
			return
		}
	}

	win32.StopOtherPulseProcesses()
	_ = win32.SetLaunchAtStartup(false)
	win32.RemoveUninstallEntry()
	_ = os.Remove(win32.StartMenuShortcutPath())
	_ = os.Remove(win32.DesktopShortcutPath())

	exePath, err := os.Executable()
	if err == nil && exePath != "" {
		if abs, err2 := filepath.Abs(exePath); err2 == nil {
			win32.ScheduleSelfDelete(abs)
		}
	}

	if !silent {
		win32.MessageBox(
			0,
			i18n.T("Uninstall Complete", "卸载完成"),
			i18n.T("Pulse has been uninstalled from your computer.", "Pulse 已从您的电脑卸载。"),
			win32.MB_OK|win32.MB_ICONINFORMATION,
		)
	}
}
