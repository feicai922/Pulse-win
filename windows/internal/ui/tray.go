package ui

import (
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"pulse-windows/internal/i18n"
	"pulse-windows/internal/model"
	"pulse-windows/internal/store"
	"pulse-windows/internal/win32"
)

const (
	trayClassName      = "PulseTrayHostClass"
	trayPopupClassName = "PulseTrayPopupClass"
	trayUID            = 1
	wmTrayCallback     = win32.WM_APP + 50
	wmTrayRefresh      = win32.WM_APP + 51
	hotkeyToggleID     = 1

	trayCmdShowPanel = 2001
	trayCmdRefresh   = 2002
	trayCmdSettings  = 2003
	trayCmdAutostart = 2004
	trayCmdQuit      = 2005
)

type trayBtnRect struct {
	id         string
	provider   model.ProviderID
	x, y, w, h float32
}

type TrayManager struct {
	hwnd      uintptr
	popupHwnd uintptr
	hIcon     uintptr
	settings  *model.AppSettings
	store     *store.UsageStore
	panel     *FloatingPanel

	onOpenSettings func(model.ProviderID)
	onQuit         func()

	popupVisible  bool
	popupShownAt  time.Time
	popupHiddenAt time.Time
	menuShownAt   time.Time
	popupCanvas   *win32.LayeredCanvas
	popupX        int32
	popupY        int32
	popupW        int32
	popupH        int32
	popupHoverID  string
	popupBtns     []trayBtnRect
}

var globalTray *TrayManager

func NewTrayManager(settings *model.AppSettings, st *store.UsageStore, panel *FloatingPanel, onOpenSettings func(model.ProviderID), onQuit func()) *TrayManager {
	t := &TrayManager{
		settings:       settings,
		store:          st,
		panel:          panel,
		onOpenSettings: onOpenSettings,
		onQuit:         onQuit,
	}
	globalTray = t
	return t
}

func (t *TrayManager) Create() error {
	cb := syscall.NewCallback(trayWndProc)
	_ = win32.RegisterClass(trayClassName, cb, 0)
	t.hwnd = win32.CreateWindowEx(0, trayClassName, "PulseTrayHost", 0, 0, 0, 0, 0, 0, 0)
	if t.hwnd == 0 {
		return fmt.Errorf("failed to create tray host window")
	}

	pcb := syscall.NewCallback(trayPopupWndProc)
	_ = win32.RegisterClass(trayPopupClassName, pcb, 0)
	ex := uint32(win32.WS_EX_LAYERED | win32.WS_EX_TOPMOST | win32.WS_EX_TOOLWINDOW)
	t.popupHwnd = win32.CreateWindowEx(ex, trayPopupClassName, "Pulse Dashboard", win32.WS_POPUP, 0, 0, 320, 400, 0, 0)

	t.addTrayIcon()
	t.ApplyHotkey()
	t.UpdateIconAndTip()
	return nil
}

func (t *TrayManager) Destroy() {
	if t.hwnd != 0 {
		win32.UnregisterHotKey(t.hwnd, hotkeyToggleID)
		var nid win32.NOTIFYICONDATAW
		nid.CbSize = uint32(unsafe.Sizeof(nid))
		nid.HWnd = t.hwnd
		nid.UID = trayUID
		win32.ShellNotifyIcon(win32.NIM_DELETE, &nid)
		win32.DestroyWindow(t.hwnd)
		t.hwnd = 0
	}
	if t.popupHwnd != 0 {
		win32.DestroyWindow(t.popupHwnd)
		t.popupHwnd = 0
	}
	if t.popupCanvas != nil {
		t.popupCanvas.Close()
		t.popupCanvas = nil
	}
	if t.hIcon != 0 {
		win32.DestroyIcon(t.hIcon)
		t.hIcon = 0
	}
}

func (t *TrayManager) ApplyHotkey() {
	if t.hwnd == 0 {
		return
	}
	win32.UnregisterHotKey(t.hwnd, hotkeyToggleID)
	snap := t.settings.Snapshot()
	if mods, vk, ok := win32.ParseHotkey(snap.GlobalHotkey); ok {
		win32.RegisterHotKey(t.hwnd, hotkeyToggleID, mods, vk)
	}
}

func (t *TrayManager) RequestUpdate() {
	if t.hwnd != 0 {
		win32.PostMessage(t.hwnd, wmTrayRefresh, 0, 0)
	}
}

func (t *TrayManager) ShowNotification(title, body string) {
	win32.ShowBalloonNotification(t.hwnd, trayUID, title, body)
}

func (t *TrayManager) addTrayIcon() {
	t.hIcon = t.buildTrayHICON(nil, false, false, 75)
	var nid win32.NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = t.hwnd
	nid.UID = trayUID
	nid.UFlags = win32.NIF_MESSAGE | win32.NIF_ICON | win32.NIF_TIP
	nid.UCallbackMessage = wmTrayCallback
	nid.HIcon = t.hIcon
	setTip(&nid, "Pulse")
	win32.ShellNotifyIcon(win32.NIM_ADD, &nid)
	nid.UVersion = win32.NOTIFYICON_VERSION_4
	win32.ShellNotifyIcon(win32.NIM_SETVERSION, &nid)
}

func (t *TrayManager) UpdateIconAndTip() {
	if t.hwnd == 0 {
		return
	}
	snap := t.settings.Snapshot()
	enabled := t.settings.OrderedEnabled()
	isZh := i18n.IsChinese()

	var bestFrac *float64
	bestSpent := false
	var tipParts []string

	for _, id := range enabled {
		u := t.store.Usage(id)
		info, _ := model.FindProvider(id)
		pinned := snap.PinnedWindows[string(id)]
		f, fig, sp, has := u.RailFigure(pinned, snap.ShowsRemaining)
		if has && fig != "" {
			label := fig
			if f != nil {
				if snap.ShowsRemaining {
					label = fig + i18n.T(" left", " 剩余")
				} else {
					label = fig + i18n.T(" used", " 已用")
				}
			}
			if len(tipParts) < 4 {
				tipParts = append(tipParts, info.DisplayName+": "+label)
			}
		}
		if f != nil && (bestFrac == nil || *f > *bestFrac || (sp && !bestSpent)) {
			v := *f
			bestFrac = &v
			bestSpent = sp
		}
	}

	var newIcon uintptr
	if snap.ShowTrayUsage && bestFrac != nil {
		newIcon = t.buildTrayHICON(bestFrac, bestSpent, snap.ShowsRemaining, snap.WarningThreshold)
	} else {
		newIcon = t.buildTrayHICON(nil, false, false, snap.WarningThreshold)
	}

	tip := "Pulse"
	if len(tipParts) > 0 {
		tip = "Pulse\n" + strings.Join(tipParts, "\n")
	} else if len(enabled) == 0 {
		tip = i18n.T("Pulse — Click to set up providers", "Pulse — 点击选择要监控的服务")
	}
	_ = isZh

	var nid win32.NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = t.hwnd
	nid.UID = trayUID
	nid.UFlags = win32.NIF_ICON | win32.NIF_TIP
	nid.HIcon = newIcon
	setTip(&nid, tip)
	win32.ShellNotifyIcon(win32.NIM_MODIFY, &nid)

	if t.hIcon != 0 {
		win32.DestroyIcon(t.hIcon)
	}
	t.hIcon = newIcon

	if t.popupVisible {
		t.renderPopup()
	}
}

func (t *TrayManager) buildTrayHICON(usedFrac *float64, isSpent, showRemaining bool, warnPct int) uintptr {
	const sz = 32
	c := win32.NewLayeredCanvas(sz, sz)
	defer c.Close()
	c.Clear(0)

	if usedFrac == nil {
		// Dark badge with crisp white Pulse "P" mark
		c.FillEllipse(2, 2, sz-4, sz-4, win32.ARGB(255, 22, 24, 30))
		c.DrawEllipse(2, 2, sz-4, sz-4, 1.5, win32.ARGB(255, 90, 96, 112))
		c.DrawSVGIcon("pulse-mark", 5, 5, sz-10, win32.ARGB(255, 248, 250, 252))
		return c.ToHICON()
	}

	c.FillEllipse(1, 1, sz-2, sz-2, win32.ARGB(255, 18, 20, 26))
	c.DrawEllipse(3.5, 3.5, sz-7, sz-7, 3.8, win32.ARGB(255, 52, 56, 68))

	col := UsageColor(*usedFrac, isSpent, warnPct, false, "")
	drawFrac := *usedFrac
	if showRemaining && !isSpent {
		drawFrac = 1 - drawFrac
	}
	if drawFrac > 0 {
		sweep := float32(drawFrac * 360)
		if sweep < 14 {
			sweep = 14
		}
		c.DrawArc(3.5, 3.5, sz-7, sz-7, -90, sweep, 3.8, col, true)
	}

	// Center percentage number (compact)
	pct := int(drawFrac*100 + 0.5)
	if isSpent {
		pct = 0
		if !showRemaining {
			pct = 100
		}
	}
	txt := fmt.Sprintf("%d", pct)
	c.DrawText(txt, 2, 2, sz-4, sz-4, 10.5, true, win32.AlignCenter, win32.AlignCenter, win32.ARGB(255, 248, 250, 252), false)
	return c.ToHICON()
}

func (t *TrayManager) TogglePopup() {
	if t.popupVisible {
		t.HidePopup()
		return
	}
	// Clicking the tray icon while the popup is open first triggers WM_ACTIVATE(WA_INACTIVE)
	// which hides the popup milliseconds before NIN_SELECT arrives.
	if time.Since(t.popupHiddenAt) < 350*time.Millisecond {
		return
	}
	t.ShowPopup()
}

func (t *TrayManager) ShowPopup() {
	if t.popupHwnd == 0 {
		return
	}
	cx, cy := win32.GetCursorPos()
	mon := win32.MonitorAtPoint(cx, cy)
	dpi := win32.GetDpiForWindow(t.popupHwnd)
	s := float32(dpi)

	enabled := t.settings.OrderedEnabled()
	rows := len(enabled)
	if rows > 8 {
		rows = 8
	}
	t.popupW = int32(332 * s)
	if rows == 0 {
		t.popupH = int32(190 * s)
	} else {
		t.popupH = int32((106 + float32(rows)*62) * s)
	}

	px := cx - t.popupW/2
	py := mon.Work.Bottom - t.popupH - int32(8*s)
	if cy < mon.Work.Top+mon.Work.Height()/2 {
		py = mon.Work.Top + int32(8*s)
	}
	if px+t.popupW > mon.Work.Right-int32(8*s) {
		px = mon.Work.Right - t.popupW - int32(8*s)
	}
	if px < mon.Work.Left+int32(8*s) {
		px = mon.Work.Left + int32(8*s)
	}
	t.popupX = px
	t.popupY = py
	t.popupVisible = true
	t.popupShownAt = time.Now()
	t.renderPopup()
	win32.SetWindowPos(t.popupHwnd, win32.HWND_TOPMOST, px, py, t.popupW, t.popupH, win32.SWP_SHOWWINDOW)
	win32.SetForegroundWindow(t.popupHwnd)
}

func (t *TrayManager) HidePopup() {
	wasVisible := t.popupVisible
	t.popupVisible = false
	if wasVisible {
		t.popupHiddenAt = time.Now()
	}
	if t.popupHwnd != 0 {
		win32.ShowWindow(t.popupHwnd, win32.SW_HIDE)
	}
}

func (t *TrayManager) renderPopup() {
	if t.popupHwnd == 0 || !t.popupVisible {
		return
	}
	dpi := win32.GetDpiForWindow(t.popupHwnd)
	s := float32(dpi)
	snap := t.settings.Snapshot()
	pal := GetPalette(snap.Surface, 96)
	if snap.Surface == model.SurfaceTranslucent {
		pal = GetPalette(model.SurfaceObsidian, 96)
	}
	enabled := t.settings.OrderedEnabled()
	isZh := i18n.IsChinese()
	now := time.Now()

	if t.popupCanvas == nil || t.popupCanvas.Width != t.popupW || t.popupCanvas.Height != t.popupH {
		if t.popupCanvas != nil {
			t.popupCanvas.Close()
		}
		t.popupCanvas = win32.NewLayeredCanvas(t.popupW, t.popupH)
	}
	c := t.popupCanvas
	c.Clear(0)

	w := float32(t.popupW)
	h := float32(t.popupH)
	pad := 14 * s

	c.FillRoundRect(0, 0, w, h, 14*s, pal.CardBg)
	c.DrawRoundRect(0, 0, w, h, 14*s, 1, pal.CardBorder)

	t.popupBtns = nil

	// Header
	c.DrawSVGIcon("pulse-mark", pad, pad+2*s, 16*s, pal.TextPrimary)
	c.DrawText("Pulse", pad+22*s, pad, 120*s, 22*s, 13.5*s, true, win32.AlignNear, win32.AlignCenter, pal.TextPrimary, false)

	// Header buttons: Refresh, Panel Toggle, Settings
	addHdrBtn := func(id, label string, bx, bw float32) {
		if t.popupHoverID == id {
			c.FillRoundRect(bx, pad, bw, 22*s, 6*s, pal.HoverBg)
		}
		c.DrawText(label, bx, pad, bw, 22*s, 10.5*s, true, win32.AlignCenter, win32.AlignCenter, pal.TextSecondary, false)
		t.popupBtns = append(t.popupBtns, trayBtnRect{id: id, x: bx, y: pad, w: bw, h: 22 * s})
	}
	addHdrBtn("settings", i18n.T("Settings", "设置"), w-pad-54*s, 54*s)
	addHdrBtn("refresh", i18n.T("Refresh", "刷新"), w-pad-112*s, 52*s)

	curY := pad + 32*s
	c.DrawLine(pad, curY, w-pad, curY, 1, pal.Divider)
	curY += 10 * s

	if len(enabled) == 0 {
		c.DrawText(
			i18n.T("No providers enabled yet.\nClick Settings to choose services to monitor.", "尚未启用任何服务商。\n点击下方按钮选择要监控的服务。"),
			pad, curY+8*s, w-pad*2, 48*s, 11.5*s, false, win32.AlignCenter, win32.AlignCenter, pal.TextSecondary, false,
		)
		bw := 140 * s
		bx := (w - bw) / 2
		by := curY + 66*s
		c.FillRoundRect(bx, by, bw, 30*s, 8*s, win32.ARGB(255, 37, 99, 235))
		c.DrawText(i18n.T("Choose Providers", "选择服务商"), bx, by, bw, 30*s, 11.5*s, true, win32.AlignCenter, win32.AlignCenter, win32.ARGB(255, 255, 255, 255), false)
		t.popupBtns = append(t.popupBtns, trayBtnRect{id: "settings", x: bx, y: by, w: bw, h: 30 * s})
	} else {
		shown := enabled
		if len(shown) > 8 {
			shown = shown[:8]
		}
		for _, id := range shown {
			info, _ := model.FindProvider(id)
			u := t.store.Usage(id)
			pinned := snap.PinnedWindows[string(id)]
			head := u.HeadlineWindow(pinned)
			_, figText, isSpent, hasReading := u.RailFigure(pinned, snap.ShowsRemaining)

			rowH := 56 * s
			rowID := "provider:" + string(id)
			if t.popupHoverID == rowID {
				c.FillRoundRect(pad-4*s, curY, w-(pad-4*s)*2, rowH, 8*s, pal.HoverBg)
			}
			t.popupBtns = append(t.popupBtns, trayBtnRect{id: rowID, provider: id, x: pad - 4*s, y: curY, w: w - (pad-4*s)*2, h: rowH})

			iconCol := pal.TextPrimary
			if info.BrandColor != 0 && info.BrandColor != 0x000000 && info.BrandColor != 0x111111 {
				iconCol = BrandColorARGB(info.BrandColor, pal.IsLight)
			}
			c.DrawSVGIcon(info.Icon, pad+4*s, curY+8*s, 16*s, iconCol)
			c.DrawText(info.DisplayName, pad+26*s, curY+6*s, 145*s, 20*s, 12*s, true, win32.AlignNear, win32.AlignCenter, pal.TextPrimary, true)

			rightTxt := figText
			if rightTxt == "" {
				rightTxt = u.Reason.Message(isZh)
			} else if head != nil {
				if snap.ShowsRemaining {
					rightTxt += i18n.T(" left", " 剩余")
				} else {
					rightTxt += i18n.T(" used", " 已用")
				}
			}
			rightCol := pal.TextPrimary
			if !hasReading {
				rightCol = pal.TextMuted
			} else if head != nil {
				rightCol = UsageColor(head.UsedFraction, isSpent, snap.WarningThreshold, pal.IsLight, "")
			}
			c.DrawText(rightTxt, pad+165*s, curY+6*s, w-pad*2-169*s, 20*s, 11.5*s, true, win32.AlignFar, win32.AlignCenter, rightCol, true)

			// Mini progress bar + reset text
			barX := pad + 26*s
			barW := w - pad*2 - 30*s
			barY := curY + 30*s
			c.FillRoundRect(barX, barY, barW, 5*s, 2.5*s, pal.BarTrack)
			if head != nil {
				fillF := head.UsedFraction
				if snap.ShowsRemaining && !isSpent {
					fillF = head.RemainingFraction()
				}
				fillCol := UsageColor(head.UsedFraction, isSpent, snap.WarningThreshold, pal.IsLight, "")
				fw := float32(fillF) * barW
				if fillF > 0 && fw < 5*s {
					fw = 5 * s
				}
				if fw > 0 {
					c.FillRoundRect(barX, barY, fw, 5*s, 2.5*s, fillCol)
				}
				sub := head.DisplayName(isZh)
				if rst := model.FormatResetCountdown(head.ResetsAt, now, isZh); rst != "" {
					sub += " · " + rst
				}
				c.DrawText(sub, barX, curY+38*s, barW, 14*s, 9.5*s, false, win32.AlignNear, win32.AlignNear, pal.TextMuted, true)
			} else {
				c.DrawText(model.FormatObservedAgo(u.ObservedAt, now, isZh), barX, curY+38*s, barW, 14*s, 9.5*s, false, win32.AlignNear, win32.AlignNear, pal.TextMuted, true)
			}

			curY += rowH + 6*s
		}
	}

	// Footer
	footY := h - 38*s
	c.DrawLine(pad, footY, w-pad, footY, 1, pal.Divider)
	panelLbl := i18n.T("Show Floating Panel", "显示悬浮面板")
	if snap.IsPanelVisible {
		panelLbl = i18n.T("Hide Floating Panel", "隐藏悬浮面板")
	}
	if t.popupHoverID == "togglePanel" {
		c.FillRoundRect(pad, footY+6*s, 140*s, 24*s, 6*s, pal.HoverBg)
	}
	c.DrawText(panelLbl, pad+4*s, footY+6*s, 132*s, 24*s, 11*s, false, win32.AlignNear, win32.AlignCenter, pal.TextSecondary, true)
	t.popupBtns = append(t.popupBtns, trayBtnRect{id: "togglePanel", x: pad, y: footY + 6*s, w: 140 * s, h: 24 * s})

	if t.popupHoverID == "quit" {
		c.FillRoundRect(w-pad-66*s, footY+6*s, 66*s, 24*s, 6*s, pal.HoverBg)
	}
	c.DrawText(i18n.T("Quit", "退出"), w-pad-62*s, footY+6*s, 58*s, 24*s, 11*s, false, win32.AlignFar, win32.AlignCenter, pal.TextMuted, false)
	t.popupBtns = append(t.popupBtns, trayBtnRect{id: "quit", x: w - pad - 66*s, y: footY + 6*s, w: 66 * s, h: 24 * s})

	c.CommitToWindow(t.popupHwnd, t.popupX, t.popupY)
}

func (t *TrayManager) showContextMenu() {
	snap := t.settings.Snapshot()
	hMenu := win32.CreatePopupMenu()
	defer win32.DestroyMenu(hMenu)

	visFlags := uint32(win32.MF_STRING)
	if snap.IsPanelVisible {
		visFlags |= win32.MF_CHECKED
	}
	win32.AppendMenu(hMenu, visFlags, trayCmdShowPanel, i18n.T("Floating Edge Panel", "屏幕边缘悬浮面板"))
	win32.AppendMenu(hMenu, win32.MF_STRING, trayCmdRefresh, i18n.T("Refresh All Now", "立即全部刷新"))
	win32.AppendMenu(hMenu, win32.MF_SEPARATOR, 0, "")

	autoFlags := uint32(win32.MF_STRING)
	if win32.IsLaunchAtStartupEnabled() {
		autoFlags |= win32.MF_CHECKED
	}
	win32.AppendMenu(hMenu, autoFlags, trayCmdAutostart, i18n.T("Launch at Windows Startup", "开机自启动"))
	win32.AppendMenu(hMenu, win32.MF_STRING, trayCmdSettings, i18n.T("Settings…", "设置…"))
	win32.AppendMenu(hMenu, win32.MF_SEPARATOR, 0, "")
	win32.AppendMenu(hMenu, win32.MF_STRING, trayCmdQuit, i18n.T("Quit Pulse", "退出 Pulse"))

	cx, cy := win32.GetCursorPos()
	win32.SetForegroundWindow(t.hwnd)
	cmd := win32.TrackPopupMenu(hMenu, win32.TPM_RETURNCMD, cx, cy, t.hwnd)
	switch cmd {
	case trayCmdShowPanel:
		t.settings.Update(func(s *model.AppSettings) { s.IsPanelVisible = !s.IsPanelVisible })
		t.panel.RequestRelayout()
	case trayCmdRefresh:
		t.store.TriggerRefreshAll()
	case trayCmdAutostart:
		next := !win32.IsLaunchAtStartupEnabled()
		_ = win32.SetLaunchAtStartup(next)
		t.settings.Update(func(s *model.AppSettings) { s.LaunchAtStartup = next })
	case trayCmdSettings:
		if t.onOpenSettings != nil {
			t.onOpenSettings("")
		}
	case trayCmdQuit:
		if t.onQuit != nil {
			t.onQuit()
		}
	}
}

func setTip(nid *win32.NOTIFYICONDATAW, tip string) {
	u, err := windows.UTF16FromString(tip)
	if err != nil {
		return
	}
	n := len(u)
	if n > len(nid.SzTip) {
		n = len(nid.SzTip)
	}
	copy(nid.SzTip[:n], u[:n])
	nid.SzTip[len(nid.SzTip)-1] = 0
}

func trayWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	t := globalTray
	if t == nil {
		return win32.DefWindowProc(hwnd, msg, wParam, lParam)
	}
	switch msg {
	case wmTrayCallback:
		ev := uint32(lParam & 0xFFFF)
		switch ev {
		case win32.NIN_SELECT, win32.NIN_KEYSELECT:
			t.TogglePopup()
		case win32.WM_RBUTTONUP:
			t.HidePopup()
			t.menuShownAt = time.Now()
			t.showContextMenu()
		case win32.WM_CONTEXTMENU:
			if time.Since(t.menuShownAt) > 300*time.Millisecond {
				t.HidePopup()
				t.menuShownAt = time.Now()
				t.showContextMenu()
			}
		}
		return 0
	case wmTrayRefresh:
		t.UpdateIconAndTip()
		return 0
	case win32.WM_HOTKEY:
		if wParam == hotkeyToggleID {
			t.settings.Update(func(s *model.AppSettings) {
				s.IsPanelVisible = !s.IsPanelVisible
			})
			t.panel.RequestRelayout()
			t.UpdateIconAndTip()
		}
		return 0
	}
	return win32.DefWindowProc(hwnd, msg, wParam, lParam)
}

func trayPopupWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	t := globalTray
	if t == nil {
		return win32.DefWindowProc(hwnd, msg, wParam, lParam)
	}
	switch msg {
	case win32.WM_ACTIVATE:
		if (wParam & 0xFFFF) == 0 {
			if time.Since(t.popupShownAt) > 150*time.Millisecond {
				t.HidePopup()
			}
		}
		return 0
	case win32.WM_KEYDOWN:
		if wParam == 0x1B { // VK_ESCAPE
			t.HidePopup()
		}
		return 0
	case win32.WM_MOUSEMOVE:
		lx := float32(int32(int16(lParam & 0xFFFF)))
		ly := float32(int32(int16((lParam >> 16) & 0xFFFF)))
		hover := ""
		for _, b := range t.popupBtns {
			if lx >= b.x && lx <= b.x+b.w && ly >= b.y && ly <= b.y+b.h {
				hover = b.id
				break
			}
		}
		if hover != t.popupHoverID {
			t.popupHoverID = hover
			t.renderPopup()
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
		for _, b := range t.popupBtns {
			if lx >= b.x && lx <= b.x+b.w && ly >= b.y && ly <= b.y+b.h {
				switch {
				case b.id == "refresh":
					t.store.TriggerRefreshAll()
				case b.id == "settings":
					t.HidePopup()
					if t.onOpenSettings != nil {
						t.onOpenSettings("")
					}
				case b.id == "togglePanel":
					t.settings.Update(func(s *model.AppSettings) { s.IsPanelVisible = !s.IsPanelVisible })
					t.panel.RequestRelayout()
					t.renderPopup()
				case b.id == "quit":
					t.HidePopup()
					if t.onQuit != nil {
						t.onQuit()
					}
				case strings.HasPrefix(b.id, "provider:"):
					t.HidePopup()
					if t.onOpenSettings != nil {
						t.onOpenSettings(b.provider)
					}
				}
				break
			}
		}
		return 0
	}
	return win32.DefWindowProc(hwnd, msg, wParam, lParam)
}
