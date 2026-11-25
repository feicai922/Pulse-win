//go:build installer

package main

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"pulse-windows/internal/i18n"
	"pulse-windows/internal/win32"
)

//go:embed payload/Pulse.exe.gz
var embeddedPulseGZ []byte

const (
	installerClassName = "PulseInstallerWindowClass"
	wmInstallDone      = win32.WM_APP + 91
	appVersion         = "1.0.0"
)

type hitRect struct {
	id         string
	x, y, w, h float32
}

type installerState struct {
	hwnd   uintptr
	canvas *win32.LayeredCanvas

	installDir   string
	optDesktop   bool
	optStartMenu bool
	optAutostart bool
	optLaunchNow bool

	installing bool
	done       bool
	installErr string

	hoverID string
	hits    []hitRect
	mu      sync.Mutex
}

var inst *installerState

func main() {
	runtime.LockOSThread()

	silent := false
	customDir := ""
	desktop := true
	startMenu := true
	autostart := true
	launchNow := true

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--silent" || strings.EqualFold(a, "/S"):
			silent = true
		case a == "--dir" && i+1 < len(args):
			customDir = args[i+1]
			i++
		case a == "--no-desktop":
			desktop = false
		case a == "--no-startmenu":
			startMenu = false
		case a == "--no-autostart":
			autostart = false
		case a == "--no-launch":
			launchNow = false
		}
	}

	dir := win32.DefaultInstallDir()
	if customDir != "" {
		dir = customDir
	}

	if silent {
		if _, err := performInstall(dir, desktop, startMenu, autostart, launchNow); err != nil {
			os.Exit(1)
		}
		return
	}

	win32.InitDPIAwareness()
	win32.InitGDIPlus()
	defer win32.ShutdownGDIPlus()

	inst = &installerState{
		installDir:   dir,
		optDesktop:   desktop,
		optStartMenu: startMenu,
		optAutostart: autostart,
		optLaunchNow: launchNow,
	}

	cb := syscall.NewCallback(installerWndProc)
	_ = win32.RegisterClass(installerClassName, cb, 0)

	dpi := float32(win32.GetDpiForWindow(0))
	winW := int32(520 * dpi)
	winH := int32(430 * dpi)
	mon := win32.MonitorAtPoint(100, 100)
	x := mon.Work.Left + (mon.Work.Width()-winW)/2
	y := mon.Work.Top + (mon.Work.Height()-winH)/2

	inst.hwnd = win32.CreateWindowEx(
		0,
		installerClassName,
		i18n.T("Pulse Setup", "Pulse 安装程序"),
		uint32(win32.WS_OVERLAPPEDWINDOW&^(0x00010000|0x00040000)),
		x, y, winW, winH, 0, 0,
	)
	if inst.hwnd == 0 {
		return
	}
	win32.SetDarkTitleBar(inst.hwnd, true)
	win32.ShowWindow(inst.hwnd, win32.SW_SHOW)
	win32.SetForegroundWindow(inst.hwnd)

	win32.MessageLoop()
}

func decompressPulseExe() ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(embeddedPulseGZ))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func performInstall(dir string, desktop, startMenu, autostart, launchNow bool) (string, error) {
	payload, err := decompressPulseExe()
	if err != nil {
		return "", fmt.Errorf("decompress payload: %w", err)
	}

	win32.StopOtherPulseProcesses()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create install dir: %w", err)
	}
	exePath := filepath.Join(dir, "Pulse.exe")
	if err := os.WriteFile(exePath, payload, 0o755); err != nil {
		return "", fmt.Errorf("write Pulse.exe: %w", err)
	}

	desc := i18n.T("Pulse AI Quota Monitor", "Pulse AI 额度监控")
	if startMenu {
		_ = win32.CreateShortcut(win32.StartMenuShortcutPath(), exePath, dir, desc)
	}
	if desktop {
		_ = win32.CreateShortcut(win32.DesktopShortcutPath(), exePath, dir, desc)
	}
	_ = win32.SetLaunchAtStartupPath(autostart, exePath)
	_ = win32.WriteUninstallEntry(dir, exePath, appVersion, int64(len(payload)))

	if launchNow {
		_ = win32.LaunchDetached(exePath)
	}
	return exePath, nil
}

func (s *installerState) startInstall() {
	s.mu.Lock()
	if s.installing {
		s.mu.Unlock()
		return
	}
	s.installing = true
	s.done = false
	s.installErr = ""
	dir := s.installDir
	desktop := s.optDesktop
	startMenu := s.optStartMenu
	autostart := s.optAutostart
	launchNow := s.optLaunchNow
	s.mu.Unlock()

	win32.InvalidateRect(s.hwnd, false)

	go func() {
		_, err := performInstall(dir, desktop, startMenu, autostart, launchNow)
		s.mu.Lock()
		s.installing = false
		if err != nil {
			s.installErr = err.Error()
		} else {
			s.done = true
		}
		s.mu.Unlock()
		win32.PostMessage(s.hwnd, wmInstallDone, 0, 0)
	}()
}

func (s *installerState) render(hdc uintptr, w, h int32) {
	if s.canvas == nil || s.canvas.Width != w || s.canvas.Height != h {
		if s.canvas != nil {
			s.canvas.Close()
		}
		s.canvas = win32.NewLayeredCanvas(w, h)
	}
	c := s.canvas
	dpi := float32(win32.GetDpiForWindow(s.hwnd))

	bg := win32.ARGB(255, 20, 20, 24)
	cardBg := win32.ARGB(255, 29, 30, 36)
	cardBorder := win32.ARGB(255, 50, 52, 62)
	textPrim := win32.ARGB(255, 242, 245, 250)
	textSec := win32.ARGB(255, 148, 156, 170)
	accent := win32.ARGB(255, 34, 197, 94)
	accentHover := win32.ARGB(255, 45, 212, 108)
	btnSec := win32.ARGB(255, 42, 44, 54)
	btnSecHover := win32.ARGB(255, 56, 59, 72)

	c.Clear(bg)

	s.mu.Lock()
	dir := s.installDir
	desktop := s.optDesktop
	startMenu := s.optStartMenu
	autostart := s.optAutostart
	launchNow := s.optLaunchNow
	installing := s.installing
	done := s.done
	errMsg := s.installErr
	hover := s.hoverID
	s.mu.Unlock()

	s.hits = nil
	fw := float32(w)
	fh := float32(h)
	pad := 24 * dpi

	// Header card
	c.DrawSVGIcon("pulse-mark", pad, pad+2*dpi, 32*dpi, accent)
	c.DrawText("Pulse", pad+42*dpi, pad-2*dpi, 240*dpi, 24*dpi, 18*dpi, true, win32.AlignNear, win32.AlignCenter, textPrim, false)
	c.DrawText(
		i18n.T("AI Coding Quota Monitor for Windows · v"+appVersion, "Windows 版 AI 编程额度悬浮监控 · v"+appVersion),
		pad+42*dpi, pad+22*dpi, fw-pad*2-42*dpi, 18*dpi, 11*dpi, false, win32.AlignNear, win32.AlignCenter, textSec, false,
	)

	y := pad + 56*dpi

	if done {
		boxH := float32(150 * dpi)
		c.FillRoundRect(pad, y, fw-pad*2, boxH, 12*dpi, cardBg)
		c.DrawRoundRect(pad, y, fw-pad*2, boxH, 12*dpi, 1, cardBorder)
		c.DrawText(
			i18n.T("Installation Complete", "安装完成"),
			pad+20*dpi, y+22*dpi, fw-pad*2-40*dpi, 28*dpi, 15*dpi, true, win32.AlignNear, win32.AlignCenter, accent, false,
		)
		c.DrawText(
			i18n.T("Installed to: ", "安装位置：")+dir+"\n"+i18n.T("You can uninstall anytime from Windows Apps & Features.", "您可以随时在 Windows「应用和功能」中卸载。"),
			pad+20*dpi, y+58*dpi, fw-pad*2-40*dpi, 70*dpi, 11.5*dpi, false, win32.AlignNear, win32.AlignNear, textSec, false,
		)

		btnW := float32(128 * dpi)
		btnH := float32(36 * dpi)
		bx := fw - pad - btnW
		by := fh - pad - btnH
		fill := accent
		if hover == "close" {
			fill = accentHover
		}
		c.FillRoundRect(bx, by, btnW, btnH, 8*dpi, fill)
		c.DrawText(i18n.T("Finish", "完成"), bx, by, btnW, btnH, 12.5*dpi, true, win32.AlignCenter, win32.AlignCenter, win32.ARGB(255, 10, 16, 12), false)
		s.hits = append(s.hits, hitRect{id: "close", x: bx, y: by, w: btnW, h: btnH})
		c.BlitToHDC(hdc, 0, 0)
		return
	}

	// Install path card
	pathCardH := float32(82 * dpi)
	c.FillRoundRect(pad, y, fw-pad*2, pathCardH, 12*dpi, cardBg)
	c.DrawRoundRect(pad, y, fw-pad*2, pathCardH, 12*dpi, 1, cardBorder)
	c.DrawText(i18n.T("Install Location", "安装位置"), pad+14*dpi, y+10*dpi, 200*dpi, 20*dpi, 11.5*dpi, true, win32.AlignNear, win32.AlignCenter, textPrim, false)

	browseW := float32(82 * dpi)
	pathBoxW := fw - pad*2 - 28*dpi - browseW - 10*dpi
	pathBoxY := y + 36*dpi
	pathBoxH := float32(32 * dpi)
	c.FillRoundRect(pad+14*dpi, pathBoxY, pathBoxW, pathBoxH, 7*dpi, win32.ARGB(255, 20, 21, 26))
	c.DrawRoundRect(pad+14*dpi, pathBoxY, pathBoxW, pathBoxH, 7*dpi, 1, cardBorder)
	c.DrawText(dir, pad+24*dpi, pathBoxY, pathBoxW-16*dpi, pathBoxH, 11*dpi, false, win32.AlignNear, win32.AlignCenter, textSec, true)

	bxBrowse := pad + 14*dpi + pathBoxW + 10*dpi
	bFill := btnSec
	if hover == "browse" {
		bFill = btnSecHover
	}
	c.FillRoundRect(bxBrowse, pathBoxY, browseW, pathBoxH, 7*dpi, bFill)
	c.DrawText(i18n.T("Browse…", "浏览…"), bxBrowse, pathBoxY, browseW, pathBoxH, 11*dpi, true, win32.AlignCenter, win32.AlignCenter, textPrim, false)
	s.hits = append(s.hits, hitRect{id: "browse", x: bxBrowse, y: pathBoxY, w: browseW, h: pathBoxH})

	y += pathCardH + 14*dpi

	// Options card
	optCardH := float32(148 * dpi)
	c.FillRoundRect(pad, y, fw-pad*2, optCardH, 12*dpi, cardBg)
	c.DrawRoundRect(pad, y, fw-pad*2, optCardH, 12*dpi, 1, cardBorder)

	drawCheck := func(id string, checked bool, label string, rowY float32) {
		boxSz := float32(16 * dpi)
		bx := pad + 16*dpi
		by := rowY + 8*dpi
		if hover == id {
			c.FillRoundRect(pad+6*dpi, rowY, fw-pad*2-12*dpi, 32*dpi, 7*dpi, win32.ARGB(255, 38, 40, 48))
		}
		if checked {
			c.FillRoundRect(bx, by, boxSz, boxSz, 4*dpi, accent)
			c.DrawText("✓", bx, by-1*dpi, boxSz, boxSz, 11*dpi, true, win32.AlignCenter, win32.AlignCenter, win32.ARGB(255, 10, 16, 12), false)
		} else {
			c.DrawRoundRect(bx, by, boxSz, boxSz, 4*dpi, 1.2, textSec)
		}
		c.DrawText(label, bx+boxSz+10*dpi, rowY, fw-pad*2-50*dpi, 32*dpi, 11.5*dpi, false, win32.AlignNear, win32.AlignCenter, textPrim, false)
		s.hits = append(s.hits, hitRect{id: id, x: pad + 6*dpi, y: rowY, w: fw - pad*2 - 12*dpi, h: 32 * dpi})
	}

	drawCheck("opt_desktop", desktop, i18n.T("Create Desktop shortcut", "创建桌面快捷方式"), y+10*dpi)
	drawCheck("opt_startmenu", startMenu, i18n.T("Create Start Menu shortcut", "创建开始菜单快捷方式"), y+42*dpi)
	drawCheck("opt_autostart", autostart, i18n.T("Launch Pulse at Windows startup", "开机自动启动 Pulse"), y+74*dpi)
	drawCheck("opt_launch", launchNow, i18n.T("Launch Pulse after installation", "安装完成后立即启动 Pulse"), y+106*dpi)

	if errMsg != "" {
		c.DrawText(errMsg, pad, fh-pad-64*dpi, fw-pad*2, 22*dpi, 10.5*dpi, false, win32.AlignNear, win32.AlignCenter, win32.ARGB(255, 248, 113, 113), true)
	}

	// Bottom CTA
	btnW := float32(148 * dpi)
	btnH := float32(36 * dpi)
	ix := fw - pad - btnW
	iy := fh - pad - btnH
	ctaFill := accent
	if hover == "install" {
		ctaFill = accentHover
	}
	if installing {
		ctaFill = win32.ARGB(255, 28, 130, 68)
	}
	c.FillRoundRect(ix, iy, btnW, btnH, 8*dpi, ctaFill)

	ctaText := i18n.T("Install", "立即安装")
	if _, err := os.Stat(filepath.Join(dir, "Pulse.exe")); err == nil {
		ctaText = i18n.T("Update / Reinstall", "更新安装")
	}
	if installing {
		ctaText = i18n.T("Installing…", "正在安装…")
	}
	c.DrawText(ctaText, ix, iy, btnW, btnH, 12.5*dpi, true, win32.AlignCenter, win32.AlignCenter, win32.ARGB(255, 10, 16, 12), false)
	if !installing {
		s.hits = append(s.hits, hitRect{id: "install", x: ix, y: iy, w: btnW, h: btnH})
	}

	c.BlitToHDC(hdc, 0, 0)
}

func installerWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	s := inst
	if s == nil {
		return win32.DefWindowProc(hwnd, msg, wParam, lParam)
	}
	switch msg {
	case win32.WM_PAINT:
		var ps win32.PAINTSTRUCT
		hdc := win32.BeginPaint(hwnd, &ps)
		rc := win32.GetClientRect(hwnd)
		s.render(hdc, rc.Width(), rc.Height())
		win32.EndPaint(hwnd, &ps)
		return 0
	case win32.WM_ERASEBKGND:
		return 1
	case win32.WM_MOUSEMOVE:
		lx := float32(int32(int16(lParam & 0xFFFF)))
		ly := float32(int32(int16((lParam >> 16) & 0xFFFF)))
		hover := ""
		for _, h := range s.hits {
			if lx >= h.x && lx <= h.x+h.w && ly >= h.y && ly <= h.y+h.h {
				hover = h.id
				break
			}
		}
		if hover != s.hoverID {
			s.hoverID = hover
			win32.InvalidateRect(hwnd, false)
		}
		if hover != "" {
			win32.SetHandCursor()
		} else {
			win32.SetArrowCursor()
		}
		return 0
	case win32.WM_LBUTTONUP:
		lx := float32(int32(int16(lParam & 0xFFFF)))
		ly := float32(int32(int16((lParam >> 16) & 0xFFFF)))
		for _, h := range s.hits {
			if lx >= h.x && lx <= h.x+h.w && ly >= h.y && ly <= h.y+h.h {
				switch h.id {
				case "browse":
					if chosen, ok := win32.BrowseForFolder(hwnd, i18n.T("Select Pulse Install Folder", "选择 Pulse 安装目录")); ok && chosen != "" {
						if !strings.EqualFold(filepath.Base(chosen), "Pulse") {
							chosen = filepath.Join(chosen, "Pulse")
						}
						s.mu.Lock()
						s.installDir = chosen
						s.mu.Unlock()
					}
				case "opt_desktop":
					s.mu.Lock()
					s.optDesktop = !s.optDesktop
					s.mu.Unlock()
				case "opt_startmenu":
					s.mu.Lock()
					s.optStartMenu = !s.optStartMenu
					s.mu.Unlock()
				case "opt_autostart":
					s.mu.Lock()
					s.optAutostart = !s.optAutostart
					s.mu.Unlock()
				case "opt_launch":
					s.mu.Lock()
					s.optLaunchNow = !s.optLaunchNow
					s.mu.Unlock()
				case "install":
					s.startInstall()
					return 0
				case "close":
					win32.PostQuitMessage(0)
					return 0
				}
				win32.InvalidateRect(hwnd, false)
				return 0
			}
		}
		return 0
	case wmInstallDone:
		win32.InvalidateRect(hwnd, false)
		return 0
	case win32.WM_DESTROY:
		if s.canvas != nil {
			s.canvas.Close()
			s.canvas = nil
		}
		win32.PostQuitMessage(0)
		return 0
	}
	return win32.DefWindowProc(hwnd, msg, wParam, lParam)
}
