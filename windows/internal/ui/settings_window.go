package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pulse-windows/internal/auth"
	"pulse-windows/internal/i18n"
	"pulse-windows/internal/model"
	"pulse-windows/internal/providers"
	"pulse-windows/internal/store"
	"pulse-windows/internal/win32"
)

const (
	settingsClassName = "PulseSettingsWindowClass"
	wmSettingsSync    = win32.WM_APP + 80
	editKeyID         = 9001
	editAddrID        = 9002
)

type hitAction struct {
	id         string
	x, y, w, h float32
}

type SettingsWindow struct {
	hwnd     uintptr
	canvas   *win32.LayeredCanvas
	settings *model.AppSettings
	keys     *auth.KeyStore
	store    *store.UsageStore
	panel    *FloatingPanel
	tray     *TrayManager

	activePane   string // "appearance", "rings", "position", "general", "notifications", "about", or "prov:<id>"
	discovered   map[model.ProviderID]bool
	sideScroll   float32
	bodyScroll   float32
	sideMaxScr   float32
	bodyMaxScr   float32
	hoverID      string
	hits         []hitAction

	// Lightweight native EDIT overlays only for active provider/general text inputs
	editKey      uintptr
	editAddr     uintptr
	editBrush    uintptr
	editFont     uintptr
	editProvider model.ProviderID
	ghCanceler   context.CancelFunc
}

var globalSettingsWin *SettingsWindow

func NewSettingsWindow(settings *model.AppSettings, keys *auth.KeyStore, st *store.UsageStore, panel *FloatingPanel, tray *TrayManager) *SettingsWindow {
	w := &SettingsWindow{
		settings:   settings,
		keys:       keys,
		store:      st,
		panel:      panel,
		tray:       tray,
		activePane: "appearance",
		discovered: providers.DiscoverInstalled(),
	}
	if !settings.Snapshot().HasCompletedSetup && len(settings.OrderedEnabled()) == 0 {
		w.activePane = "prov:codex"
	}
	globalSettingsWin = w
	return w
}

func (w *SettingsWindow) Open(focusProvider model.ProviderID) {
	if focusProvider != "" {
		w.activePane = "prov:" + string(focusProvider)
	} else if !w.settings.Snapshot().HasCompletedSetup && len(w.settings.OrderedEnabled()) == 0 {
		w.activePane = "prov:codex"
	}
	if w.hwnd != 0 {
		win32.ShowWindow(w.hwnd, win32.SW_RESTORE)
		win32.SetForegroundWindow(w.hwnd)
		w.syncInputsForPane()
		win32.InvalidateRect(w.hwnd, false)
		return
	}

	cb := syscall.NewCallback(settingsWndProc)
	_ = win32.RegisterClass(settingsClassName, cb, 0)

	dpi := win32.GetDpiForWindow(0)
	winW := int32(820 * dpi)
	winH := int32(600 * dpi)
	mon := win32.MonitorAtPoint(100, 100)
	x := mon.Work.Left + (mon.Work.Width()-winW)/2
	y := mon.Work.Top + (mon.Work.Height()-winH)/2

	w.hwnd = win32.CreateWindowEx(
		0,
		settingsClassName,
		i18n.T("Pulse Settings", "Pulse 设置"),
		uint32(win32.WS_OVERLAPPEDWINDOW&^(0x00010000|0x00040000)),
		x, y, winW, winH, 0, 0,
	)
	if w.hwnd == 0 {
		return
	}
	win32.SetDarkTitleBar(w.hwnd, true)
	w.editBrush = win32.CreateSolidBrush(0x00262320)
	w.editFont = win32.CreateUIFont(int32(-13*dpi), 400)
	w.syncInputsForPane()
	win32.ShowWindow(w.hwnd, win32.SW_SHOW)
	win32.SetForegroundWindow(w.hwnd)
}

func (w *SettingsWindow) RequestStatusSync() {
	if w.hwnd != 0 {
		win32.PostMessage(w.hwnd, wmSettingsSync, 0, 0)
	}
}

func (w *SettingsWindow) destroyEdits() {
	if w.editKey != 0 {
		win32.DestroyWindow(w.editKey)
		w.editKey = 0
	}
	if w.editAddr != 0 {
		win32.DestroyWindow(w.editAddr)
		w.editAddr = 0
	}
	w.editProvider = ""
}

func (w *SettingsWindow) syncInputsForPane() {
	if w.hwnd == 0 {
		return
	}
	w.destroyEdits()
	if !strings.HasPrefix(w.activePane, "prov:") {
		if w.activePane == "general" {
			dpi := float32(win32.GetDpiForWindow(w.hwnd))
			snap := w.settings.Snapshot()
			sideW := 220 * dpi
			cx := sideW + 28*dpi
			cw := float32(win32.GetClientRect(w.hwnd).Width()) - cx - 28*dpi
			// Proxy & Hotkey inline edits
			w.editKey = win32.CreateWindowEx(0, "EDIT", snap.GlobalHotkey,
				win32.WS_CHILD|win32.WS_VISIBLE|win32.ES_AUTOHSCROLL|win32.WS_TABSTOP,
				int32(cx+cw-190*dpi), int32(198*dpi-w.bodyScroll), int32(174*dpi), int32(22*dpi), w.hwnd, editKeyID)
			w.editAddr = win32.CreateWindowEx(0, "EDIT", snap.ProxyURL,
				win32.WS_CHILD|win32.WS_VISIBLE|win32.ES_AUTOHSCROLL|win32.WS_TABSTOP,
				int32(cx+cw-240*dpi), int32(314*dpi-w.bodyScroll), int32(224*dpi), int32(22*dpi), w.hwnd, editAddrID)
			win32.SendMessage(w.editKey, win32.WM_SETFONT, w.editFont, 1)
			win32.SendMessage(w.editAddr, win32.WM_SETFONT, w.editFont, 1)
		}
		return
	}

	pid := model.ProviderID(strings.TrimPrefix(w.activePane, "prov:"))
	info, ok := model.FindProvider(pid)
	if !ok {
		return
	}
	w.editProvider = pid
	dpi := float32(win32.GetDpiForWindow(w.hwnd))
	rc := win32.GetClientRect(w.hwnd)
	sideW := 220 * dpi
	cx := sideW + 28*dpi
	cw := float32(rc.Width()) - cx - 28*dpi
	snap := w.settings.Snapshot()

	if info.Credential == model.CredAPIKey || info.Credential == model.CredAPIKeyOpt ||
		info.Credential == model.CredKeyAndAddress || info.Credential == model.CredGitHubDevice {
		val := w.keys.Get(string(pid))
		if pid == model.ProviderMiniMax && snap.MiniMaxSite == "china" {
			val = w.keys.Get("minimax.minimaxi.com")
		}
		yPos := 162*dpi - w.bodyScroll
		w.editKey = win32.CreateWindowEx(0, "EDIT", val,
			win32.WS_CHILD|win32.WS_VISIBLE|win32.ES_AUTOHSCROLL|win32.ES_PASSWORD|win32.WS_TABSTOP,
			int32(cx+18*dpi), int32(yPos), int32(cw-120*dpi), int32(22*dpi), w.hwnd, editKeyID)
		win32.SendMessage(w.editKey, win32.WM_SETFONT, w.editFont, 1)
	}
	if info.Credential == model.CredKeyAndAddress {
		yPos := 230*dpi - w.bodyScroll
		w.editAddr = win32.CreateWindowEx(0, "EDIT", snap.ServerAddresses[string(pid)],
			win32.WS_CHILD|win32.WS_VISIBLE|win32.ES_AUTOHSCROLL|win32.WS_TABSTOP,
			int32(cx+18*dpi), int32(yPos), int32(cw-120*dpi), int32(22*dpi), w.hwnd, editAddrID)
		win32.SendMessage(w.editAddr, win32.WM_SETFONT, w.editFont, 1)
	}
}

func (w *SettingsWindow) paint(hdc uintptr) {
	rc := win32.GetClientRect(w.hwnd)
	cw, ch := rc.Width(), rc.Height()
	if cw < 10 || ch < 10 {
		return
	}
	if w.canvas == nil || w.canvas.Width != cw || w.canvas.Height != ch {
		if w.canvas != nil {
			w.canvas.Close()
		}
		w.canvas = win32.NewLayeredCanvas(cw, ch)
	}
	c := w.canvas
	s := float32(win32.GetDpiForWindow(w.hwnd))
	snap := w.settings.Snapshot()
	w.hits = nil

	// Modern dark macOS System Settings palette
	bgSide := win32.ARGB(255, 24, 25, 29)
	bgMain := win32.ARGB(255, 17, 18, 21)
	cardBg := win32.ARGB(255, 28, 29, 34)
	cardBorder := win32.ARGB(255, 44, 46, 54)
	divider := win32.ARGB(255, 40, 42, 50)
	textPri := win32.ARGB(255, 244, 245, 248)
	textSec := win32.ARGB(255, 152, 158, 172)
	textMut := win32.ARGB(255, 108, 114, 128)
	selBg := win32.ARGB(255, 44, 47, 56)
	hoverBg := win32.ARGB(255, 34, 36, 43)
	accent := win32.ARGB(255, 34, 197, 94) // Pulse emerald green toggle

	c.Clear(bgMain)
	sideW := 220 * s
	c.FillRect(0, 0, sideW, float32(ch), bgSide)
	c.DrawLine(sideW, 0, sideW, float32(ch), 1, divider)

	// --- Sidebar ---
	sy := 16*s - w.sideScroll
	addSideHeader := func(title string) {
		c.DrawText(title, 18*s, sy, sideW-28*s, 18*s, 11*s, true, win32.AlignNear, win32.AlignCenter, textMut, true)
		sy += 22 * s
	}
	addSideItem := func(id, icon, label string, dotColor uint32) {
		itemY := sy
		if itemY+30*s > 0 && itemY < float32(ch) {
			if w.activePane == id {
				c.FillRoundRect(10*s, itemY, sideW-20*s, 30*s, 8*s, selBg)
			} else if w.hoverID == "side:"+id {
				c.FillRoundRect(10*s, itemY, sideW-20*s, 30*s, 8*s, hoverBg)
			}
			if icon != "" {
				c.DrawSVGIcon(icon, 20*s, itemY+7*s, 15*s, textPri)
			} else {
				c.FillEllipse(24*s, itemY+12*s, 7*s, 7*s, textSec)
			}
			c.DrawText(label, 42*s, itemY, sideW-66*s, 30*s, 12.5*s, w.activePane == id, win32.AlignNear, win32.AlignCenter, textPri, true)
			if dotColor != 0 {
				c.FillEllipse(sideW-22*s, itemY+12*s, 6*s, 6*s, dotColor)
			}
			w.hits = append(w.hits, hitAction{id: "side:" + id, x: 10 * s, y: itemY, w: sideW - 20*s, h: 30 * s})
		}
		sy += 32 * s
	}

	addSideHeader(i18n.T("Panel", "悬浮面板"))
	addSideItem("appearance", "", i18n.T("Appearance", "外观"), 0)
	addSideItem("rings", "", i18n.T("Rings and figures", "圆环与数字"), 0)
	addSideItem("position", "", i18n.T("Position and behavior", "位置与行为"), 0)
	sy += 10 * s

	addSideHeader(i18n.T("Application", "应用"))
	addSideItem("general", "", i18n.T("General", "通用与网络"), 0)
	addSideItem("notifications", "", i18n.T("Notifications", "提醒通知"), 0)
	addSideItem("about", "pulse-mark", i18n.T("About", "关于 Pulse"), 0)
	sy += 10 * s

	var enabledList, availList []model.ProviderInfo
	for _, p := range model.BuiltInProviders {
		if w.settings.IsEnabled(p.ID) {
			enabledList = append(enabledList, p)
		} else {
			availList = append(availList, p)
		}
	}
	if len(enabledList) > 0 {
		addSideHeader(i18n.T("Enabled", "已开启"))
		for _, p := range enabledList {
			u := w.store.Usage(p.ID)
			dot := win32.ARGB(255, 16, 224, 138)
			if u.State == model.StateUnavailable {
				dot = win32.ARGB(255, 245, 158, 11)
			}
			addSideItem("prov:"+string(p.ID), p.Icon, p.DisplayName, dot)
		}
		sy += 10 * s
	}
	addSideHeader(i18n.T("Available", "可添加服务"))
	for _, p := range availList {
		var dot uint32
		if w.discovered[p.ID] {
			dot = win32.ARGB(255, 96, 165, 250) // detected locally
		}
		addSideItem("prov:"+string(p.ID), p.Icon, p.DisplayName, dot)
	}
	totalSideH := sy + w.sideScroll + 16*s
	w.sideMaxScr = maxF(0, totalSideH-float32(ch))

	// --- Right Content Area ---
	cx := sideW + 28*s
	bodyW := float32(cw) - cx - 28*s
	cy := 22*s - w.bodyScroll

	drawHeader := func(title string) {
		c.DrawText(title, cx, cy, bodyW, 30*s, 18*s, true, win32.AlignNear, win32.AlignCenter, textPri, true)
		cy += 42 * s
	}

	type cardRow struct {
		title, sub, kind, id, val string
		on                        bool
	}
	drawCard := func(rows []cardRow) {
		rowH := 56 * s
		cardH := float32(len(rows)) * rowH
		c.FillRoundRect(cx, cy, bodyW, cardH, 12*s, cardBg)
		c.DrawRoundRect(cx, cy, bodyW, cardH, 12*s, 1, cardBorder)
		for i, r := range rows {
			ry := cy + float32(i)*rowH
			if i > 0 {
				c.DrawLine(cx+16*s, ry, cx+bodyW-16*s, ry, 1, divider)
			}
			c.DrawText(r.title, cx+18*s, ry+8*s, bodyW-165*s, 20*s, 13*s, true, win32.AlignNear, win32.AlignCenter, textPri, true)
			c.DrawText(r.sub, cx+18*s, ry+28*s, bodyW-165*s, 18*s, 11*s, false, win32.AlignNear, win32.AlignCenter, textSec, true)

			switch r.kind {
			case "toggle":
				tw, th := 42*s, 24*s
				tx := cx + bodyW - 18*s - tw
				ty := ry + (rowH-th)/2
				track := win32.ARGB(255, 62, 66, 78)
				knobX := tx + 3*s
				if r.on {
					track = accent
					knobX = tx + tw - th + 3*s
				}
				c.FillRoundRect(tx, ty, tw, th, th/2, track)
				c.FillEllipse(knobX, ty+3*s, th-6*s, th-6*s, win32.ARGB(255, 255, 255, 255))
				w.hits = append(w.hits, hitAction{id: r.id, x: tx - 6*s, y: ty - 4*s, w: tw + 12*s, h: th + 8*s})
			case "pill":
				pw, ph := 124*s, 28*s
				px := cx + bodyW - 18*s - pw
				py := ry + (rowH-ph)/2
				bg := win32.ARGB(255, 44, 47, 56)
				if w.hoverID == r.id {
					bg = win32.ARGB(255, 58, 62, 74)
				}
				c.FillRoundRect(px, py, pw, ph, 8*s, bg)
				c.DrawText(r.val+" ▾", px+8*s, py, pw-16*s, ph, 11.5*s, false, win32.AlignCenter, win32.AlignCenter, textPri, true)
				w.hits = append(w.hits, hitAction{id: r.id, x: px, y: py, w: pw, h: ph})
			case "btn":
				bw, bh := 112*s, 28*s
				bx := cx + bodyW - 18*s - bw
				by := ry + (rowH-bh)/2
				bg := win32.ARGB(255, 52, 56, 68)
				if w.hoverID == r.id {
					bg = win32.ARGB(255, 68, 74, 90)
				}
				c.FillRoundRect(bx, by, bw, bh, 8*s, bg)
				c.DrawText(r.val, bx+6*s, by, bw-12*s, bh, 11.5*s, true, win32.AlignCenter, win32.AlignCenter, textPri, true)
				w.hits = append(w.hits, hitAction{id: r.id, x: bx, y: by, w: bw, h: bh})
			}
		}
		cy += cardH + 16*s
	}

	switch {
	case w.activePane == "appearance":
		drawHeader(i18n.T("Appearance", "外观"))
		szLbl := map[model.PanelSize]string{model.SizeSmall: i18n.T("Small", "小"), model.SizeMedium: i18n.T("Standard", "标准"), model.SizeLarge: i18n.T("Large", "大")}[snap.PanelSize]
		spLbl := i18n.T("Standard", "标准")
		if snap.Spacing < 0.9 {
			spLbl = i18n.T("Compact", "紧凑")
		} else if snap.Spacing > 1.1 {
			spLbl = i18n.T("Relaxed", "宽松")
		}
		barLbl := i18n.T("Continuous", "连续")
		if snap.UsageBarStyle == model.BarSegmented {
			barLbl = i18n.T("Segmented", "分段")
		}
		surfLbl := i18n.T("Black", "纯黑")
		if snap.Surface == model.SurfaceLight {
			surfLbl = i18n.T("Light", "浅色")
		} else if snap.Surface == model.SurfaceTranslucent {
			surfLbl = i18n.T("Translucent", "半透明")
		}
		drawCard([]cardRow{
			{i18n.T("Size", "大小"), i18n.T("Size of the rail on screen.", "屏幕上悬浮条的缩放大小。"), "pill", "act:size", szLbl, false},
			{i18n.T("Spacing", "间距"), i18n.T("How much air there is between the rings.", "各圆环之间的间距。"), "pill", "act:spacing", spLbl, false},
			{i18n.T("Round ends", "圆润两端"), i18n.T("The rail's ends and the card's tail follow the ring's curve.", "悬浮条两端和卡片尾标跟随圆环的圆弧曲率。"), "toggle", "act:roundEnds", "", snap.UsesRoundEnds},
			{i18n.T("Progress bars", "进度条样式"), i18n.T("How limit bars are drawn on the card.", "详情卡片中的进度条绘制方式。"), "pill", "act:barStyle", barLbl, false},
			{i18n.T("Panel colour", "面板材质"), i18n.T("Black, light surface, or translucent glass.", "纯黑、浅色或半透明质感。"), "pill", "act:surface", surfLbl, false},
			{i18n.T("Ring activity animation", "圆环刷新动画"), i18n.T("The turning arc while a reading is fetched.", "刷新读取额度时圆环上的旋转动画。"), "toggle", "act:anim", "", snap.AnimatesActivity},
		})

	case w.activePane == "rings":
		drawHeader(i18n.T("Rings and figures", "圆环与数字"))
		drawCard([]cardRow{
			{i18n.T("Show remaining %", "显示剩余百分比"), i18n.T("Count down what is left instead of what is spent.", "显示还剩多少百分比，而非已用多少。"), "toggle", "act:remaining", "", snap.ShowsRemaining},
			{i18n.T("Second inner ring", "内圈第二限额"), i18n.T("Draw the next-tightest limit inside the main ring.", "在主圆环内圈同时显示第二紧迫的限额。"), "toggle", "act:secondRing", "", snap.ShowsSecondRing},
			{i18n.T("Window clock arc", "时间窗口外弧"), i18n.T("Thin outer arc showing how much of the window elapsed.", "在圆环外侧绘制细弧以展示当前周期已过去的时间。"), "toggle", "act:clockArc", "", snap.ShowsWindowClock},
			{i18n.T("Burn-rate forecast", "消耗速度预测"), i18n.T("Project whether current pace will exhaust before reset.", "按当前消耗速度预测是否会在重置前用尽。"), "toggle", "act:forecast", "", snap.ShowsForecast},
			{i18n.T("Warning threshold", "红色警戒线"), i18n.T("Where the ring and figure turn warning red.", "用量达到该百分比时转为红色警戒状态。"), "pill", "act:warnPct", fmt.Sprintf("%d%%", snap.WarningThreshold), false},
		})

	case w.activePane == "position":
		drawHeader(i18n.T("Position and behavior", "位置与行为"))
		dockLbl := map[model.PanelDock]string{
			model.DockRight:         i18n.T("Right edge", "右侧边缘"),
			model.DockLeft:          i18n.T("Left edge", "左侧边缘"),
			model.DockTop:           i18n.T("Top edge", "顶部边缘"),
			model.DockBottom:        i18n.T("Bottom edge", "底部边缘"),
			model.DockFloatVertical: i18n.T("Free upright", "自由竖向"),
			model.DockFloatHoriz:    i18n.T("Free across", "自由横向"),
		}[snap.Dock]
		drawCard([]cardRow{
			{i18n.T("Show floating panel", "显示悬浮面板"), i18n.T("Keep the ring rail visible on screen.", "在屏幕上显示悬浮用量圆环面板。"), "toggle", "act:panelVis", "", snap.IsPanelVisible},
			{i18n.T("Dock position", "停靠位置"), i18n.T("Which edge the rail attaches to, or float freely.", "选择悬浮面板贴附的屏幕边缘或自由悬浮。"), "pill", "act:dock", dockLbl, false},
			{i18n.T("Snap to screen edges", "拖动吸附边缘"), i18n.T("Fuse to a screen edge when dragged close to it.", "拖动靠近屏幕边缘时自动贴合并切换方向。"), "toggle", "act:snap", "", snap.SnapsToEdges},
			{i18n.T("Auto-collapse when idle", "闲置自动收起"), i18n.T("Fold into a thin edge sliver when pointer leaves.", "鼠标离开后收起为屏幕边缘细线。"), "toggle", "act:autoCollapse", "", snap.AutoCollapse},
			{i18n.T("Follow active display", "跟随鼠标显示器"), i18n.T("Move the rail to whichever monitor holds the pointer.", "多屏环境下自动跟随鼠标当前所在的显示器。"), "toggle", "act:followDisp", "", snap.FollowsActiveDisplay},
			{i18n.T("Hide from screenshots", "截图时隐藏"), i18n.T("Exclude the floating panel from screen captures.", "在截图和录屏中隐藏悬浮面板（WDA_EXCLUDEFROMCAPTURE）。"), "toggle", "act:hideCap", "", snap.HideFromScreenshots},
			{i18n.T("Show ring in system tray", "系统托盘显示用量"), i18n.T("Draw the tightest quota ring in the Windows tray.", "在任务栏系统托盘实时绘制最高用量的圆环图标。"), "toggle", "act:trayUsage", "", snap.ShowTrayUsage},
		})

	case w.activePane == "general":
		drawHeader(i18n.T("General", "通用与网络"))
		langLbl := map[model.Language]string{model.LangAuto: i18n.T("Auto", "跟随系统"), model.LangZhCN: "简体中文", model.LangEn: "English"}[snap.Language]
		refLbl := map[model.RefreshMode]string{
			model.RefreshAdaptive: i18n.T("Adaptive", "自适应"),
			model.Refresh5Min:     i18n.T("5 min", "5 分钟"),
			model.Refresh15Min:    i18n.T("15 min", "15 分钟"),
			model.Refresh30Min:    i18n.T("30 min", "30 分钟"),
			model.RefreshManual:   i18n.T("Manual", "手动"),
		}[snap.RefreshMode]
		drawCard([]cardRow{
			{i18n.T("Language", "界面语言"), i18n.T("Interface and notification language.", "界面与通知文案语言。"), "pill", "act:lang", langLbl, false},
			{i18n.T("Launch at startup", "开机自启动"), i18n.T("Start Pulse automatically when signing in to Windows.", "登录 Windows 时自动启动 Pulse。"), "toggle", "act:startup", "", win32.IsLaunchAtStartupEnabled()},
			{i18n.T("Global hotkey", "全局快捷键"), i18n.T("Toggle floating panel visibility (e.g. Ctrl+Alt+P).", "一键显示/隐藏悬浮面板的全局快捷键。"), "none", "", "", false},
			{i18n.T("Refresh cadence", "刷新频率"), i18n.T("Adaptive paces between 2 and 30 minutes.", "自适应模式会根据接近限额程度在 2–30 分钟间动态调节。"), "pill", "act:refresh", refLbl, false},
			{i18n.T("Network proxy", "网络代理"), i18n.T("Optional HTTP/SOCKS5 proxy URL (blank = system).", "可选代理地址（如 http://127.0.0.1:7890，留空走系统代理）。"), "none", "", "", false},
			{i18n.T("Apply network & hotkey", "应用网络与快捷键"), i18n.T("Save hotkey and proxy edits above.", "保存上方填写的快捷键和代理设置。"), "btn", "act:applyGen", i18n.T("Save", "保存"), false},
		})
		// Draw rounded border around the two inline EDIT controls
		c.DrawRoundRect(cx+bodyW-196*s, 194*s-w.bodyScroll, 186*s, 28*s, 6*s, 1, cardBorder)
		c.DrawRoundRect(cx+bodyW-246*s, 310*s-w.bodyScroll, 236*s, 28*s, 6*s, 1, cardBorder)

	case w.activePane == "notifications":
		drawHeader(i18n.T("Notifications", "提醒通知"))
		drawCard([]cardRow{
			{i18n.T("Quota threshold alert", "用量阈值提醒"), i18n.T("Notify once when a limit crosses the selected mark.", "当任意限额使用超过设定阈值时发送通知。"), "toggle", "act:nThresh", "", snap.NotifyThreshold},
			{i18n.T("Threshold percentage", "提醒阈值"), i18n.T("Trigger threshold for quota alerts.", "触发通知提醒的使用百分比阈值。"), "pill", "act:nPct", fmt.Sprintf("%d%%", snap.NotifyThresholdPct), false},
			{i18n.T("Exhausted alert", "用尽提醒"), i18n.T("Notify when a provider reports a limit as spent.", "当服务商报告某项限额已用尽时提醒。"), "toggle", "act:nExh", "", snap.NotifyExhausted},
			{i18n.T("Reset alert", "重置提醒"), i18n.T("Notify when a previously warned limit resets.", "曾触发提醒的限额周期重置后通知。"), "toggle", "act:nReset", "", snap.NotifyReset},
			{i18n.T("Failure alert", "连续失败提醒"), i18n.T("Notify after 3 consecutive refresh failures.", "连续 3 次读取失败时发送提醒。"), "toggle", "act:nFail", "", snap.NotifyFailure},
		})

	case w.activePane == "about":
		drawHeader(i18n.T("About Pulse", "关于 Pulse"))
		c.FillRoundRect(cx, cy, bodyW, 200*s, 12*s, cardBg)
		c.DrawRoundRect(cx, cy, bodyW, 200*s, 12*s, 1, cardBorder)
		c.DrawSVGIcon("pulse-mark", cx+20*s, cy+20*s, 28*s, textPri)
		c.DrawText("Pulse for Windows", cx+58*s, cy+20*s, bodyW-76*s, 28*s, 16*s, true, win32.AlignNear, win32.AlignCenter, textPri, false)
		aboutTxt := i18n.T(
			"Local-first AI coding quota monitor. No Pulse backend, no Pulse account.\n\n"+
				"• Screen-edge docked ring rail with hover detail cards & auto-collapse sliver\n"+
				"• System tray live usage ring, quick dashboard & global hotkey\n"+
				"• Windows DPAPI encrypted local credential store & Pulse.exe --json",
			"本地优先的开源 AI 编程额度监视器，无 Pulse 服务器，无需 Pulse 账号。\n\n"+
				"• 屏幕边缘吸附圆环悬浮条、悬停详情卡片与闲置收起细线\n"+
				"• Windows 系统托盘实时用量圆环、快捷面板与全局快捷键\n"+
				"• API Key 使用 Windows DPAPI 本地加密保存，支持 Pulse.exe --json",
		)
		c.DrawText(aboutTxt, cx+20*s, cy+60*s, bodyW-40*s, 125*s, 12*s, false, win32.AlignNear, win32.AlignNear, textSec, false)
		cy += 216 * s

	case strings.HasPrefix(w.activePane, "prov:"):
		pid := model.ProviderID(strings.TrimPrefix(w.activePane, "prov:"))
		info, ok := model.FindProvider(pid)
		if !ok {
			break
		}
		drawHeader(info.DisplayName)

		drawCard([]cardRow{
			{i18n.T("Enable monitoring", "开启监控"), i18n.T("Show this provider on the floating rail and refresh it.", "在悬浮面板显示该服务商并定时读取额度。"), "toggle", "act:provToggle", "", w.settings.IsEnabled(pid)},
		})

		// Credential & status card
		if info.Credential != model.CredLocalLogin {
			boxH := 76 * s
			if info.Credential == model.CredKeyAndAddress {
				boxH = 144 * s
			}
			c.FillRoundRect(cx, cy, bodyW, boxH, 12*s, cardBg)
			c.DrawRoundRect(cx, cy, bodyW, boxH, 12*s, 1, cardBorder)
			lbl := i18n.T("API Key (DPAPI encrypted)", "API Key（Windows DPAPI 加密保存）")
			if info.Credential == model.CredGitHubDevice {
				lbl = i18n.T("GitHub Copilot Token (or use Device Sign-in below)", "GitHub Token（或使用下方一键扫码/网页授权）")
			} else if info.Credential == model.CredAPIKeyOpt {
				lbl = i18n.T("API Key (optional — falls back to local CLI login)", "API Key（可选 — 留空时自动读取本机 CLI 登录）")
			}
			c.DrawText(lbl, cx+18*s, cy+10*s, bodyW-36*s, 18*s, 11.5*s, true, win32.AlignNear, win32.AlignCenter, textSec, true)
			c.DrawRoundRect(cx+14*s, cy+34*s, bodyW-114*s, 28*s, 6*s, 1, cardBorder)

			// Save Key button
			bx, by, bw, bh := cx+bodyW-90*s, cy+34*s, 76*s, 28*s
			c.FillRoundRect(bx, by, bw, bh, 7*s, win32.ARGB(255, 37, 99, 235))
			c.DrawText(i18n.T("Save", "保存"), bx, by, bw, bh, 11.5*s, true, win32.AlignCenter, win32.AlignCenter, win32.ARGB(255, 255, 255, 255), false)
			w.hits = append(w.hits, hitAction{id: "act:saveKey", x: bx, y: by, w: bw, h: bh})

			if info.Credential == model.CredKeyAndAddress {
				c.DrawText(i18n.T("Server Address (HTTPS URL)", "服务器地址（HTTPS URL）"), cx+18*s, cy+76*s, bodyW-36*s, 18*s, 11.5*s, true, win32.AlignNear, win32.AlignCenter, textSec, true)
				c.DrawRoundRect(cx+14*s, cy+100*s, bodyW-114*s, 28*s, 6*s, 1, cardBorder)
			}
			cy += boxH + 16*s
		} else {
			c.FillRoundRect(cx, cy, bodyW, 56*s, 12*s, cardBg)
			c.DrawRoundRect(cx, cy, bodyW, 56*s, 12*s, 1, cardBorder)
			c.DrawText(localLoginHelp(pid), cx+18*s, cy+8*s, bodyW-36*s, 40*s, 11.5*s, false, win32.AlignNear, win32.AlignCenter, textSec, false)
			cy += 72 * s
		}

		// Optional provider options card
		var extraRows []cardRow
		if info.Credential == model.CredGitHubDevice {
			extraRows = append(extraRows, cardRow{i18n.T("GitHub Device Login", "GitHub 网页授权登录"), i18n.T("Copy code and authorize in browser (read:user).", "复制设备码并在浏览器确认授权（仅 read:user 权限）。"), "btn", "act:ghLogin", i18n.T("Sign In", "登录授权"), false})
		}
		if info.HasSiteOption {
			sLbl := "minimax.io"
			if snap.MiniMaxSite == "china" {
				sLbl = "minimaxi.com"
			}
			extraRows = append(extraRows, cardRow{i18n.T("MiniMax Site", "MiniMax 站点"), i18n.T("International or China mainland console.", "选择国际站或国内站账号。"), "pill", "act:mmSite", sLbl, false})
		}
		if info.SupportsBalanceBasis {
			bLbl := map[model.BalanceBasis]string{
				model.BasisSinceTopUp:  i18n.T("Since top-up", "自充值峰值"),
				model.BasisYourBudget:  i18n.T("Your budget", "自定义预算"),
				model.BasisBalanceOnly: i18n.T("Balance only", "仅显示余额"),
			}[snap.BalanceBases[string(pid)]]
			if bLbl == "" {
				bLbl = i18n.T("Since top-up", "自充值峰值")
			}
			extraRows = append(extraRows, cardRow{i18n.T("Ring measures", "圆环计算方式"), i18n.T("How balance is turned into a ring percentage.", "余额如何换算为圆环百分比。"), "pill", "act:balBasis", bLbl, false})
		}
		extraRows = append(extraRows, cardRow{i18n.T("Current usage", "当前用量状态"), w.formatShortStatus(pid, snap), "btn", "act:provRefresh", i18n.T("Refresh", "立即读取"), false})
		if info.UsageURL != "" {
			extraRows = append(extraRows, cardRow{i18n.T("Usage webpage", "官方用量页面"), info.UsageURL, "btn", "act:openURL", i18n.T("Open", "打开网页"), false})
		}
		drawCard(extraRows)
	}

	totalBodyH := cy + w.bodyScroll + 20*s
	w.bodyMaxScr = maxF(0, totalBodyH-float32(ch))

	c.BlitToHDC(hdc, 0, 0)
}

func (w *SettingsWindow) formatShortStatus(id model.ProviderID, snap model.AppSettings) string {
	isZh := i18n.IsChinese()
	if !w.settings.IsEnabled(id) {
		return i18n.T("Not enabled", "未开启")
	}
	if w.store.IsRefreshing(id) {
		return i18n.T("Refreshing…", "读取中…")
	}
	u := w.store.Usage(id)
	if u.State == model.StateUnavailable {
		return u.Reason.Message(isZh)
	}
	if head := u.HeadlineWindow(snap.PinnedWindows[string(id)]); head != nil {
		mode := i18n.T("used", "已用")
		if snap.ShowsRemaining {
			mode = i18n.T("left", "剩余")
		}
		res := fmt.Sprintf("%s: %s %s", head.DisplayName(isZh), head.PercentText(snap.ShowsRemaining), mode)
		if u.Plan != "" {
			res = u.Plan + " · " + res
		}
		return res
	}
	if u.CreditBalance != "" {
		return i18n.T("Balance: ", "余额：") + u.CreditBalance
	}
	return i18n.T("Live", "已连接")
}

func (w *SettingsWindow) handleClick(x, y int32) {
	fx, fy := float32(x), float32(y)
	for _, h := range w.hits {
		if fx >= h.x && fx <= h.x+h.w && fy >= h.y && fy <= h.y+h.h {
			w.dispatchAction(h.id)
			return
		}
	}
}

func (w *SettingsWindow) dispatchAction(id string) {
	if strings.HasPrefix(id, "side:") {
		w.activePane = strings.TrimPrefix(id, "side:")
		w.bodyScroll = 0
		w.syncInputsForPane()
		win32.InvalidateRect(w.hwnd, false)
		return
	}

	switch id {
	case "act:size":
		w.settings.Update(func(s *model.AppSettings) {
			switch s.PanelSize {
			case model.SizeSmall:
				s.PanelSize = model.SizeMedium
			case model.SizeMedium:
				s.PanelSize = model.SizeLarge
			default:
				s.PanelSize = model.SizeSmall
			}
		})
	case "act:spacing":
		w.settings.Update(func(s *model.AppSettings) {
			if s.Spacing < 0.9 {
				s.Spacing = 1.0
			} else if s.Spacing < 1.15 {
				s.Spacing = 1.25
			} else {
				s.Spacing = 0.8
			}
		})
	case "act:roundEnds":
		w.settings.Update(func(s *model.AppSettings) { s.UsesRoundEnds = !s.UsesRoundEnds })
	case "act:barStyle":
		w.settings.Update(func(s *model.AppSettings) {
			if s.UsageBarStyle == model.BarContinuous {
				s.UsageBarStyle = model.BarSegmented
			} else {
				s.UsageBarStyle = model.BarContinuous
			}
		})
	case "act:surface":
		w.settings.Update(func(s *model.AppSettings) {
			switch s.Surface {
			case model.SurfaceObsidian:
				s.Surface = model.SurfaceLight
			case model.SurfaceLight:
				s.Surface = model.SurfaceTranslucent
			default:
				s.Surface = model.SurfaceObsidian
			}
		})
	case "act:anim":
		w.settings.Update(func(s *model.AppSettings) { s.AnimatesActivity = !s.AnimatesActivity })
	case "act:remaining":
		w.settings.Update(func(s *model.AppSettings) { s.ShowsRemaining = !s.ShowsRemaining })
	case "act:secondRing":
		w.settings.Update(func(s *model.AppSettings) { s.ShowsSecondRing = !s.ShowsSecondRing })
	case "act:clockArc":
		w.settings.Update(func(s *model.AppSettings) { s.ShowsWindowClock = !s.ShowsWindowClock })
	case "act:forecast":
		w.settings.Update(func(s *model.AppSettings) { s.ShowsForecast = !s.ShowsForecast })
	case "act:warnPct":
		opts := []int{60, 70, 75, 80, 85, 90}
		w.settings.Update(func(s *model.AppSettings) {
			for i, o := range opts {
				if o == s.WarningThreshold {
					s.WarningThreshold = opts[(i+1)%len(opts)]
					return
				}
			}
			s.WarningThreshold = 75
		})
	case "act:panelVis":
		w.settings.Update(func(s *model.AppSettings) { s.IsPanelVisible = !s.IsPanelVisible })
	case "act:dock":
		order := []model.PanelDock{model.DockRight, model.DockLeft, model.DockTop, model.DockBottom, model.DockFloatVertical, model.DockFloatHoriz}
		w.settings.Update(func(s *model.AppSettings) {
			for i, d := range order {
				if d == s.Dock {
					s.Dock = order[(i+1)%len(order)]
					return
				}
			}
			s.Dock = model.DockRight
		})
	case "act:snap":
		w.settings.Update(func(s *model.AppSettings) { s.SnapsToEdges = !s.SnapsToEdges })
	case "act:autoCollapse":
		w.settings.Update(func(s *model.AppSettings) { s.AutoCollapse = !s.AutoCollapse })
	case "act:followDisp":
		w.settings.Update(func(s *model.AppSettings) { s.FollowsActiveDisplay = !s.FollowsActiveDisplay })
	case "act:hideCap":
		w.settings.Update(func(s *model.AppSettings) { s.HideFromScreenshots = !s.HideFromScreenshots })
	case "act:trayUsage":
		w.settings.Update(func(s *model.AppSettings) { s.ShowTrayUsage = !s.ShowTrayUsage })

	case "act:lang":
		w.settings.Update(func(s *model.AppSettings) {
			switch s.Language {
			case model.LangAuto:
				s.Language = model.LangZhCN
			case model.LangZhCN:
				s.Language = model.LangEn
			default:
				s.Language = model.LangAuto
			}
			i18n.SetLanguage(s.Language)
		})
		win32.SetWindowText(w.hwnd, i18n.T("Pulse Settings", "Pulse 设置"))
	case "act:startup":
		next := !win32.IsLaunchAtStartupEnabled()
		_ = win32.SetLaunchAtStartup(next)
		w.settings.Update(func(s *model.AppSettings) { s.LaunchAtStartup = next })
	case "act:refresh":
		modes := []model.RefreshMode{model.RefreshAdaptive, model.Refresh5Min, model.Refresh15Min, model.Refresh30Min, model.RefreshManual}
		w.settings.Update(func(s *model.AppSettings) {
			for i, m := range modes {
				if m == s.RefreshMode {
					s.RefreshMode = modes[(i+1)%len(modes)]
					return
				}
			}
			s.RefreshMode = model.RefreshAdaptive
		})
	case "act:applyGen":
		hk := strings.TrimSpace(win32.GetWindowText(w.editKey))
		pxy := strings.TrimSpace(win32.GetWindowText(w.editAddr))
		w.settings.Update(func(s *model.AppSettings) {
			s.GlobalHotkey = hk
			s.ProxyURL = pxy
		})
		w.tray.ApplyHotkey()

	case "act:nThresh":
		w.settings.Update(func(s *model.AppSettings) { s.NotifyThreshold = !s.NotifyThreshold })
	case "act:nPct":
		opts := []int{75, 80, 90, 95}
		w.settings.Update(func(s *model.AppSettings) {
			for i, o := range opts {
				if o == s.NotifyThresholdPct {
					s.NotifyThresholdPct = opts[(i+1)%len(opts)]
					return
				}
			}
			s.NotifyThresholdPct = 80
		})
	case "act:nExh":
		w.settings.Update(func(s *model.AppSettings) { s.NotifyExhausted = !s.NotifyExhausted })
	case "act:nReset":
		w.settings.Update(func(s *model.AppSettings) { s.NotifyReset = !s.NotifyReset })
	case "act:nFail":
		w.settings.Update(func(s *model.AppSettings) { s.NotifyFailure = !s.NotifyFailure })

	case "act:provToggle":
		pid := w.currentProv()
		on := !w.settings.IsEnabled(pid)
		w.saveProvEdits(pid)
		w.settings.Update(func(s *model.AppSettings) { s.SetEnabled(pid, on) })
		if on {
			w.store.RefreshOne(pid)
		}
	case "act:saveKey":
		pid := w.currentProv()
		w.saveProvEdits(pid)
		if !w.settings.IsEnabled(pid) {
			w.settings.Update(func(s *model.AppSettings) { s.SetEnabled(pid, true) })
		}
		w.store.RefreshOne(pid)
	case "act:mmSite":
		w.settings.Update(func(s *model.AppSettings) {
			if s.MiniMaxSite == "china" {
				s.MiniMaxSite = "international"
			} else {
				s.MiniMaxSite = "china"
			}
		})
		w.syncInputsForPane()
	case "act:balBasis":
		pid := w.currentProv()
		bases := []model.BalanceBasis{model.BasisSinceTopUp, model.BasisYourBudget, model.BasisBalanceOnly}
		w.settings.Update(func(s *model.AppSettings) {
			cur := s.BalanceBases[string(pid)]
			for i, b := range bases {
				if b == cur {
					s.BalanceBases[string(pid)] = bases[(i+1)%len(bases)]
					return
				}
			}
			s.BalanceBases[string(pid)] = model.BasisBalanceOnly
		})
		if b, _ := strconv.ParseFloat("0", 64); b == 0 {
		}
	case "act:provRefresh":
		pid := w.currentProv()
		w.saveProvEdits(pid)
		if !w.settings.IsEnabled(pid) {
			w.settings.Update(func(s *model.AppSettings) { s.SetEnabled(pid, true) })
		}
		w.store.RefreshOne(pid)
	case "act:openURL":
		if info, ok := model.FindProvider(w.currentProv()); ok && info.UsageURL != "" {
			win32.OpenURL(info.UsageURL)
		}
	case "act:ghLogin":
		w.startGitHubLogin()
	}

	w.panel.RequestRelayout()
	w.tray.RequestUpdate()
	win32.InvalidateRect(w.hwnd, false)
}

func (w *SettingsWindow) currentProv() model.ProviderID {
	return model.ProviderID(strings.TrimPrefix(w.activePane, "prov:"))
}

func (w *SettingsWindow) saveProvEdits(pid model.ProviderID) {
	snap := w.settings.Snapshot()
	if w.editKey != 0 {
		val := strings.TrimSpace(win32.GetWindowText(w.editKey))
		storeKey := string(pid)
		if pid == model.ProviderMiniMax && snap.MiniMaxSite == "china" {
			storeKey = "minimax.minimaxi.com"
		}
		_ = w.keys.Set(storeKey, val)
	}
	if w.editAddr != 0 {
		addr := strings.TrimSpace(win32.GetWindowText(w.editAddr))
		w.settings.Update(func(s *model.AppSettings) { s.ServerAddresses[string(pid)] = addr })
	}
}

func (w *SettingsWindow) startGitHubLogin() {
	if w.ghCanceler != nil {
		w.ghCanceler()
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.ghCanceler = cancel
	client := providers.NewHTTPClient(w.settings.Snapshot().ProxyURL, 20*time.Second, true)
	go func() {
		prompt, err := auth.StartGitHubDeviceLogin(ctx, client)
		if err != nil {
			return
		}
		win32.CopyText(prompt.UserCode)
		win32.OpenURL(prompt.VerificationURL)
		tok, err := auth.AwaitGitHubDeviceToken(ctx, client, prompt)
		if err != nil {
			return
		}
		_ = w.keys.Set(string(model.ProviderCopilot), tok)
		w.settings.Update(func(s *model.AppSettings) {
			s.SetEnabled(model.ProviderCopilot, true)
		})
		w.store.RefreshOne(model.ProviderCopilot)
		w.panel.RequestRelayout()
		w.RequestStatusSync()
	}()
}

func localLoginHelp(id model.ProviderID) string {
	switch id {
	case model.ProviderClaudeCode:
		return i18n.T("Reads OAuth credentials saved by Claude Code CLI in ~/.claude/.credentials.json.", "自动读取 Claude Code CLI 保存在 ~/.claude/.credentials.json 的登录凭据。")
	case model.ProviderCodex:
		return i18n.T("Reads Codex CLI login from ~/.codex/auth.json.", "自动读取 Codex CLI 保存在 ~/.codex/auth.json 的登录凭据。")
	case model.ProviderCursor:
		return i18n.T("Reads Cursor editor's local session from %APPDATA%\\Cursor\\User\\globalStorage\\state.vscdb.", "自动从本机 Cursor 存储（state.vscdb + DPAPI）读取登录会话，无需手动输入。")
	case model.ProviderAntigravity:
		return i18n.T("Detects running Antigravity language server on loopback and queries its quota.", "自动检测正在运行的 Antigravity 本地服务端口并读取实时额度。")
	case model.ProviderGemini:
		return i18n.T("Reads Google login saved by Gemini CLI in ~/.gemini/oauth_creds.json.", "自动读取 Gemini CLI 保存在 ~/.gemini/oauth_creds.json 的登录凭据。")
	case model.ProviderGrok:
		return i18n.T("Reads Grok CLI login from ~/.grok/auth.json.", "自动读取 Grok CLI 保存在 ~/.grok/auth.json 的登录凭据。")
	default:
		return i18n.T("Reads local credentials saved by the tool.", "自动读取该工具保存在本机的登录凭据。")
	}
}

func settingsWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	w := globalSettingsWin
	if w == nil {
		return win32.DefWindowProc(hwnd, msg, wParam, lParam)
	}
	switch msg {
	case win32.WM_ERASEBKGND:
		return 1
	case win32.WM_PAINT:
		var ps win32.PAINTSTRUCT
		hdc := win32.BeginPaint(hwnd, &ps)
		w.paint(hdc)
		win32.EndPaint(hwnd, &ps)
		return 0
	case win32.WM_MOUSEMOVE:
		lx := float32(int32(int16(lParam & 0xFFFF)))
		ly := float32(int32(int16((lParam >> 16) & 0xFFFF)))
		hover := ""
		for _, h := range w.hits {
			if lx >= h.x && lx <= h.x+h.w && ly >= h.y && ly <= h.y+h.h {
				hover = h.id
				break
			}
		}
		if hover != w.hoverID {
			w.hoverID = hover
			win32.InvalidateRect(hwnd, false)
		}
		if hover != "" {
			win32.SetHandCursor()
		} else {
			win32.SetArrowCursor()
		}
		return 0
	case win32.WM_LBUTTONUP:
		lx := int32(int16(lParam & 0xFFFF))
		ly := int32(int16((lParam >> 16) & 0xFFFF))
		w.handleClick(lx, ly)
		return 0
	case win32.WM_MOUSEWHEEL:
		delta := float32(int16((wParam >> 16) & 0xFFFF)) / 120.0 * 38.0
		cx, _ := win32.GetCursorPos()
		rc := win32.GetWindowRect(hwnd)
		s := float32(win32.GetDpiForWindow(hwnd))
		if float32(cx-rc.Left) < 220*s {
			w.sideScroll = clampF(w.sideScroll-delta, 0, w.sideMaxScr)
		} else {
			w.bodyScroll = clampF(w.bodyScroll-delta, 0, w.bodyMaxScr)
			w.syncInputsForPane()
		}
		win32.InvalidateRect(hwnd, false)
		return 0
	case wmSettingsSync:
		win32.InvalidateRect(hwnd, false)
		return 0
	case win32.WM_CTLCOLOREDIT:
		win32.SetTextColor(wParam, 0x00F4F5F8)
		win32.SetBkColor(wParam, 0x00262320)
		return w.editBrush
	case win32.WM_CLOSE:
		if w.ghCanceler != nil {
			w.ghCanceler()
		}
		w.destroyEdits()
		if w.canvas != nil {
			w.canvas.Close()
			w.canvas = nil
		}
		win32.DeleteObject(w.editBrush)
		win32.DeleteObject(w.editFont)
		win32.DestroyWindow(hwnd)
		w.hwnd = 0
		return 0
	}
	return win32.DefWindowProc(hwnd, msg, wParam, lParam)
}
