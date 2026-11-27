package ui

import (
	"fmt"
	"math"
	"syscall"
	"time"

	"pulse-windows/internal/i18n"
	"pulse-windows/internal/model"
	"pulse-windows/internal/store"
	"pulse-windows/internal/win32"
)

const (
	panelClassName  = "PulseFloatingPanelClass"
	timerHoverPoll  = 1
	timerAnimTick   = 2
	timerFollowDisp = 3
	wmAppRepaint    = win32.WM_APP + 10
	wmAppRelayout   = win32.WM_APP + 11

	menuCmdRefreshAll = 1001
	menuCmdToggleRem  = 1002
	menuCmdAutoHide   = 1003
	menuCmdHidePanel  = 1004
	menuCmdSettings   = 1005
	menuCmdQuit       = 1006
)

type slotRect struct {
	provider model.ProviderID
	cx, cy   float32
	x, y     float32
	w, h     float32
}

type FloatingPanel struct {
	hwnd     uintptr
	canvas   *win32.LayeredCanvas
	settings *model.AppSettings
	store    *store.UsageStore

	onOpenSettings func(model.ProviderID)
	onQuit         func()

	dpi   float64
	scale float32

	// Placement state (screen px)
	winX, winY int32
	winW, winH int32
	railX, railY, railW, railH float32
	cardOpensRight             bool
	cardOpensDown              bool
	slots                      []slotRect

	// Cached monitor list & active drag state (zero disk I/O during drag)
	cachedMonitors []win32.MonitorBounds
	dragging       bool
	dragMoved      bool
	dragStartX     int32
	dragStartY     int32
	railStartX     int32
	railStartY     int32
	liveDock       model.PanelDock
	liveMonIdx     int
	liveHRatio     float64
	liveVRatio     float64

	// Smooth animation & hover state
	hoveredIdx   int
	cardVisible  bool
	isExpanded   bool
	openness     float32 // 0 = sliver, 1 = full berth
	targetOpen   float32
	cardAlpha    float32 // 0..1 reveal
	ptrCoord     float32 // smoothed pointer coordinate along card
	targetPtr    float32
	lastInsideAt time.Time
	spinPhase    float64
	spinUntil    map[model.ProviderID]time.Time
	animRunning  bool
	menuOpen     bool
	cardRect     win32.RectF
}

var globalPanel *FloatingPanel

func NewFloatingPanel(settings *model.AppSettings, st *store.UsageStore, onOpenSettings func(model.ProviderID), onQuit func()) *FloatingPanel {
	p := &FloatingPanel{
		settings:       settings,
		store:          st,
		onOpenSettings: onOpenSettings,
		onQuit:         onQuit,
		hoveredIdx:     -1,
		dpi:            1.0,
		scale:          1.0,
		isExpanded:     true,
		openness:       1.0,
		targetOpen:     1.0,
		lastInsideAt:   time.Now(),
		spinUntil:      map[model.ProviderID]time.Time{},
	}
	globalPanel = p
	return p
}

func (p *FloatingPanel) Create() error {
	cb := syscall.NewCallback(floatingPanelWndProc)
	_ = win32.RegisterClass(panelClassName, cb, 0)
	exStyle := uint32(win32.WS_EX_LAYERED | win32.WS_EX_TOPMOST | win32.WS_EX_TOOLWINDOW | win32.WS_EX_NOACTIVATE)
	p.hwnd = win32.CreateWindowEx(exStyle, panelClassName, "Pulse Panel", win32.WS_POPUP, 0, 0, 100, 100, 0, 0)
	if p.hwnd == 0 {
		return fmt.Errorf("CreateWindowEx failed for floating panel")
	}
	p.dpi = win32.GetDpiForWindow(p.hwnd)
	p.applySettingsAndPlace()
	win32.SetTimer(p.hwnd, timerHoverPoll, 45)
	win32.SetTimer(p.hwnd, timerFollowDisp, 300)
	return nil
}

func (p *FloatingPanel) HWND() uintptr {
	return p.hwnd
}

func (p *FloatingPanel) RequestRepaint() {
	if p.hwnd != 0 {
		win32.PostMessage(p.hwnd, wmAppRepaint, 0, 0)
	}
}

func (p *FloatingPanel) RequestRelayout() {
	if p.hwnd != 0 {
		win32.PostMessage(p.hwnd, wmAppRelayout, 0, 0)
	}
}

func (p *FloatingPanel) startAnim() {
	if !p.animRunning && p.hwnd != 0 {
		p.animRunning = true
		win32.SetTimer(p.hwnd, timerAnimTick, 16)
	}
}

func (p *FloatingPanel) stopAnim() {
	if p.animRunning && p.hwnd != 0 {
		p.animRunning = false
		win32.KillTimer(p.hwnd, timerAnimTick)
	}
}

func (p *FloatingPanel) applySettingsAndPlace() {
	if p.hwnd == 0 {
		return
	}
	snap := p.settings.Snapshot()
	win32.SetExcludeFromCapture(p.hwnd, snap.HideFromScreenshots)

	enabled := p.settings.OrderedEnabled()
	if !snap.IsPanelVisible || len(enabled) == 0 {
		win32.ShowWindow(p.hwnd, win32.SW_HIDE)
		p.stopAnim()
		return
	}

	p.dpi = win32.GetDpiForWindow(p.hwnd)
	p.scale = float32(p.dpi * snap.PanelSize.Scale())
	p.cachedMonitors = win32.EnumMonitors()
	if !p.dragging {
		p.liveDock = snap.Dock
		p.liveMonIdx = snap.MonitorIndex
		p.liveHRatio = snap.HorizontalRatio
		p.liveVRatio = snap.VerticalRatio
	}
	if !snap.AutoCollapse || !p.liveDock.IsDocked() {
		p.isExpanded = true
		p.openness = 1.0
		p.targetOpen = 1.0
	}

	p.computeLayout(snap, enabled)
	win32.SetWindowPos(p.hwnd, win32.HWND_TOPMOST, p.winX, p.winY, p.winW, p.winH, win32.SWP_NOACTIVATE|win32.SWP_SHOWWINDOW)
	p.render()
}

func (p *FloatingPanel) computeLayout(snap model.AppSettings, enabled []model.ProviderID) {
	s := p.scale
	n := len(enabled)
	if n == 0 {
		return
	}

	mon := win32.MonitorBounds{
		Full: win32.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		Work: win32.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1040},
	}
	if len(p.cachedMonitors) > 0 {
		idx := p.liveMonIdx
		if idx < 0 || idx >= len(p.cachedMonitors) {
			idx = 0
		}
		mon = p.cachedMonitors[idx]
	}

	dock := p.liveDock
	docked := dock.IsDocked()
	horiz := dock.IsHorizontal()
	room := mon.Work
	if !snap.SnapsToEdges && !docked {
		room = mon.Full
	}

	// Exact DockLayout proportions from Sources/Pulse/Panel/UsageDockView.swift
	width := 64 * s
	cornerR := 26 * s
	flareH := 24 * s
	vertPad := 46 * s
	if snap.UsesRoundEnds {
		cornerR = width / 2
		flareH = cornerR
		vertPad = flareH + cornerR - 18*s + 8*s
	}
	endPad := vertPad
	if !docked {
		endPad = vertPad - flareH
	}

	ringDia := 36 * s
	ringToText := 6 * s
	if snap.ShowsWindowClock {
		ringToText += 5 * s
	}
	pctH := 16 * s
	pctW := 38 * s
	spacing := float32(snap.Spacing)
	if spacing <= 0 {
		spacing = 1.0
	}
	itemGap := 30 * s * spacing

	var rW, rH, itemLen float32
	if horiz {
		itemLen = maxF(ringDia, pctW)
		if !docked {
			itemGap = 20 * s * spacing
		}
		crossPad := 10 * s
		if !docked {
			crossPad = 14 * s
		}
		extra := float32(0)
		if snap.ShowsWindowClock {
			extra = 10 * s
		}
		itemH := ringDia + ringToText + pctH
		rW = endPad*2 + itemLen*float32(n) + itemGap*float32(n-1)
		rH = itemH + crossPad*2 + extra
	} else {
		itemLen = ringDia + ringToText + pctH
		rW = width
		rH = endPad*2 + itemLen*float32(n) + itemGap*float32(n-1)
	}

	// Exact DetailCardLayout reach
	cardW := 250 * s
	ptrW := 20 * s
	hGap := 8 * s
	reach := cardW + ptrW + hGap
	maxCardH := 380 * s
	if lim := float32(room.Height()) * 0.75; maxCardH > lim {
		maxCardH = lim
	}

	var totalW, totalH float32
	if horiz {
		totalW = maxF(rW, cardW)
		totalH = rH + hGap + ptrW + maxCardH
	} else {
		totalW = reach + rW
		totalH = maxF(rH, maxCardH)
	}

	p.winW = int32(math.Ceil(float64(totalW)))
	p.winH = int32(math.Ceil(float64(totalH)))

	availW := float32(room.Width()) - rW
	if availW < 0 {
		availW = 0
	}
	availH := float32(room.Height()) - rH
	if availH < 0 {
		availH = 0
	}

	var screenRailX, screenRailY float32
	switch dock {
	case model.DockLeft:
		screenRailX = float32(room.Left)
		screenRailY = float32(room.Top) + float32(p.liveVRatio)*availH
	case model.DockRight:
		screenRailX = float32(room.Right) - rW
		screenRailY = float32(room.Top) + float32(p.liveVRatio)*availH
	case model.DockTop:
		screenRailX = float32(room.Left) + float32(p.liveHRatio)*availW
		screenRailY = float32(room.Top)
	case model.DockBottom:
		screenRailX = float32(room.Left) + float32(p.liveHRatio)*availW
		screenRailY = float32(room.Bottom) - rH
	default:
		screenRailX = float32(room.Left) + float32(p.liveHRatio)*availW
		screenRailY = float32(room.Top) + float32(p.liveVRatio)*availH
	}

	p.cardOpensRight = dock == model.DockLeft
	if dock == model.DockFloatVertical {
		p.cardOpensRight = (screenRailX + rW/2) < float32(room.Left+room.Right)/2
	}
	p.cardOpensDown = dock == model.DockTop
	if dock == model.DockFloatHoriz {
		p.cardOpensDown = (screenRailY + rH/2) < float32(room.Top+room.Bottom)/2
	}

	if horiz {
		wx := screenRailX + rW/2 - float32(p.winW)/2
		wx = clampF(wx, float32(room.Left), maxF(float32(room.Left), float32(room.Right)-float32(p.winW)))
		p.winX = int32(math.Round(float64(wx)))
		p.railX = screenRailX - float32(p.winX)
		if p.cardOpensDown {
			p.winY = int32(math.Round(float64(screenRailY)))
			p.railY = 0
		} else {
			p.winY = int32(math.Round(float64(screenRailY + rH - float32(p.winH))))
			p.railY = float32(p.winH) - rH
		}
	} else {
		wy := screenRailY + rH/2 - float32(p.winH)/2
		wy = clampF(wy, float32(room.Top), maxF(float32(room.Top), float32(room.Bottom)-float32(p.winH)))
		p.winY = int32(math.Round(float64(wy)))
		p.railY = screenRailY - float32(p.winY)
		if p.cardOpensRight {
			p.winX = int32(math.Round(float64(screenRailX)))
			p.railX = 0
		} else {
			p.winX = int32(math.Round(float64(screenRailX + rW - float32(p.winW))))
			p.railX = float32(p.winW) - rW
		}
	}

	p.railW = rW
	p.railH = rH

	// Populate ring centers matching DockLayout.firstRingAlong & ringStep
	p.slots = make([]slotRect, n)
	crossPad := 10 * s
	if horiz && !docked {
		crossPad = 14 * s
	}
	if horiz && snap.ShowsWindowClock {
		crossPad += 10 * s
	}
	for i, id := range enabled {
		if horiz {
			ix := p.railX + endPad + float32(i)*(itemLen+itemGap)
			cx := ix + itemLen/2
			cy := p.railY + crossPad + ringDia/2
			p.slots[i] = slotRect{provider: id, cx: cx, cy: cy, x: ix, y: p.railY, w: itemLen, h: rH}
		} else {
			iy := p.railY + endPad + float32(i)*(itemLen+itemGap)
			cx := p.railX + rW/2
			cy := iy + ringDia/2
			p.slots[i] = slotRect{provider: id, cx: cx, cy: cy, x: p.railX, y: iy - 4*s, w: rW, h: itemLen + 8*s}
		}
	}
}

func (p *FloatingPanel) sliverBounds(dock model.PanelDock) (x, y, w, h float32) {
	s := p.scale
	thick := 6 * s
	length := 96 * s
	switch dock {
	case model.DockLeft:
		return p.railX, p.railY + (p.railH-length)/2, thick, length
	case model.DockRight:
		return p.railX + p.railW - thick, p.railY + (p.railH-length)/2, thick, length
	case model.DockTop:
		return p.railX + (p.railW-length)/2, p.railY, length, thick
	case model.DockBottom:
		return p.railX + (p.railW-length)/2, p.railY + p.railH - thick, length, thick
	default:
		return p.railX, p.railY, p.railW, p.railH
	}
}

func (p *FloatingPanel) ensureCanvas() *win32.LayeredCanvas {
	if p.canvas == nil || p.canvas.Width != p.winW || p.canvas.Height != p.winH {
		if p.canvas != nil {
			p.canvas.Close()
		}
		p.canvas = win32.NewLayeredCanvas(p.winW, p.winH)
	}
	return p.canvas
}

func (p *FloatingPanel) render() {
	if p.hwnd == 0 || p.winW <= 0 || p.winH <= 0 {
		return
	}
	snap := p.settings.Snapshot()
	enabled := p.settings.OrderedEnabled()
	if !snap.IsPanelVisible || len(enabled) == 0 {
		return
	}

	c := p.ensureCanvas()
	c.Clear(0)

	pal := GetPalette(snap.Surface, snap.GlassOpacity)
	// Obsidian matches macOS pure black panel
	if snap.Surface == model.SurfaceObsidian {
		pal.PanelBg = win32.ARGB(255, 0, 0, 0)
		pal.CardBg = win32.ARGB(255, 0, 0, 0)
		pal.RingTrack = win32.ARGB(255, 38, 40, 46)
	}
	s := p.scale
	dock := p.liveDock
	open := p.openness

	// Interpolate berth geometry between sliver and full rail
	bx, by, bw, bh := p.railX, p.railY, p.railW, p.railH
	if dock.IsDocked() && open < 0.999 {
		sx, sy, sw, sh := p.sliverBounds(dock)
		bx = sx + (p.railX-sx)*open
		by = sy + (p.railY-sy)*open
		bw = sw + (p.railW-sw)*open
		bh = sh + (p.railH-sh)*open
	}

	fillColor := pal.PanelBg
	if dock.IsDocked() && open < 0.25 {
		for _, id := range enabled {
			u := p.store.Usage(id)
			if u.State != model.StateUnavailable {
				if head := u.HeadlineWindow(snap.PinnedWindows[string(id)]); head != nil {
					if head.IsExhausted || head.UsedFraction >= 1 {
						fillColor = win32.ARGB(255, 196, 32, 48)
						break
					}
					if head.UsedFraction >= float64(snap.WarningThreshold)/100.0 {
						fillColor = win32.ARGB(255, 255, 72, 72)
					}
				}
			}
		}
	}

	berthPath := BuildDockBerthPath(dock, snap.UsesRoundEnds, open, bx, by, bw, bh, s)
	if berthPath != 0 {
		c.FillPath(berthPath, fillColor)
		if pal.IsLight || snap.Surface == model.SurfaceTranslucent {
			c.DrawPath(berthPath, 1.0, pal.PanelBorder, false)
		}
		win32.DeletePath(berthPath)
	}

	// Draw rings & labels when berth is open enough (matches Pulse's fade-in after berth opens)
	if open > 0.45 {
		contentAlpha := clampF((open-0.45)/0.55, 0, 1)
		ringDia := 36 * s
		lineW := 4.0 * s
		ringToText := 6 * s
		if snap.ShowsWindowClock {
			ringToText += 5 * s
		}
		now := time.Now()

		for i, slot := range p.slots {
			u := p.store.Usage(slot.provider)
			pinned := snap.PinnedWindows[string(slot.provider)]
			fracPtr, figText, isSpent, hasReading := u.RailFigure(pinned, snap.ShowsRemaining)
			head := u.HeadlineWindow(pinned)
			info, _ := model.FindProvider(slot.provider)

			// Subtle halo on hovered ring
			if i == p.hoveredIdx && p.cardVisible {
				c.FillEllipse(slot.cx-ringDia/2-4*s, slot.cy-ringDia/2-4*s, ringDia+8*s, ringDia+8*s, win32.ARGB(uint8(32*contentAlpha), 255, 255, 255))
			}

			// Optional outer window clock arc
			if snap.ShowsWindowClock && head != nil {
				if el, ok := head.ElapsedFraction(now); ok {
					clockDia := ringDia + 8*s
					cx0 := slot.cx - clockDia/2
					cy0 := slot.cy - clockDia/2
					c.DrawEllipse(cx0, cy0, clockDia, clockDia, 1.4*s, win32.ARGB(uint8(42*contentAlpha), 160, 170, 190))
					c.DrawArc(cx0, cy0, clockDia, clockDia, -90, float32(el*360), 1.5*s, win32.ARGB(uint8(165*contentAlpha), 210, 220, 235), true)
				}
			}

			// Main ring track & colored arc (matching UsageRingView.swift)
			rx := slot.cx - ringDia/2
			ry := slot.cy - ringDia/2
			c.DrawEllipse(rx, ry, ringDia, ringDia, lineW, win32.RGBAlpha(pal.RingTrack, uint8(255*contentAlpha)))

			isRef := snap.AnimatesActivity && p.isProviderSpinning(slot.provider, now)
			if fracPtr != nil {
				used := *fracPtr
				baseCol := UsageColor(used, isSpent, snap.WarningThreshold, pal.IsLight, snap.RingColors[string(slot.provider)])
				drawFrac := used
				if snap.ShowsRemaining && !isSpent {
					drawFrac = 1 - used
				}
				arcAlpha := contentAlpha
				if isRef {
					// Quietened in place while a fresh reading is fetched (opacity 0.3), never replaced.
					arcAlpha *= 0.30
				}
				if drawFrac > 0 {
					sweep := float32(drawFrac * 360)
					if sweep < 6 {
						sweep = 6
					}
					c.DrawArc(rx, ry, ringDia, ringDia, -90, sweep, lineW, win32.RGBAlpha(baseCol, uint8(255*arcAlpha)), true)
				}
				if isRef {
					// Short travelling refreshMark over the ring (sweep 0.16 = 57.6°, period 0.85s).
					startDeg := float32(-90 + p.spinPhase*360)
					c.DrawArc(rx, ry, ringDia, ringDia, startDeg, 57.6, lineW, win32.RGBAlpha(baseCol, uint8(255*contentAlpha)), true)
				}
			} else if isRef {
				startDeg := float32(-90 + p.spinPhase*360)
				c.DrawArc(rx, ry, ringDia, ringDia, startDeg, 57.6, lineW, win32.RGBAlpha(pal.TextSecondary, uint8(210*contentAlpha)), true)
			}

			// Optional second inner ring
			if snap.ShowsSecondRing {
				if sec := u.SecondWindow(pinned); sec != nil {
					secDia := ringDia - (lineW+1.5*s)*2
					if secDia > 14*s {
						sx0 := slot.cx - secDia/2
						sy0 := slot.cy - secDia/2
						c.DrawEllipse(sx0, sy0, secDia, secDia, lineW*0.75, win32.RGBAlpha(pal.RingTrack, uint8(255*contentAlpha)))
						secSpent := sec.IsExhausted || sec.UsedFraction >= 1
						scol := UsageColor(sec.UsedFraction, secSpent, snap.WarningThreshold, pal.IsLight, snap.RingColors[string(slot.provider)])
						sf := sec.UsedFraction
						if snap.ShowsRemaining && !secSpent {
							sf = 1 - sf
						}
						if sf > 0 {
							sw := float32(sf * 360)
							if sw < 6 {
								sw = 6
							}
							c.DrawArc(sx0, sy0, secDia, secDia, -90, sw, lineW*0.75, win32.RGBAlpha(scol, uint8(255*contentAlpha)), true)
						}
					}
				}
			}

			// Crisp pure white/dark icon directly in center (NO gray disc, matching panel.png!)
			discDia := ringDia - lineW*2 - 8*s
			if snap.ShowsSecondRing {
				discDia -= 4 * s
			}
			iconAlpha := uint8(255 * contentAlpha)
			if !hasReading {
				iconAlpha = uint8(110 * contentAlpha)
			}
			iconCol := win32.ARGB(iconAlpha, 255, 255, 255)
			if pal.IsLight {
				iconCol = win32.ARGB(iconAlpha, 22, 24, 28)
			}
			iconSz := discDia
			if !c.DrawSVGIcon(info.Icon, slot.cx-iconSz/2, slot.cy-iconSz/2, iconSz, iconCol) {
				c.DrawText(info.DisplayName[:1], slot.cx-discDia/2, slot.cy-discDia/2, discDia, discDia, 11*s, true, win32.AlignCenter, win32.AlignCenter, iconCol, false)
			}

			// Percentage / balance text
			txt := figText
			if txt == "" {
				txt = "—"
			}
			txtCol := pal.TextPrimary
			if !hasReading {
				txtCol = win32.RGBAlpha(pal.TextPrimary, uint8(100*contentAlpha))
			} else if isSpent {
				txtCol = win32.RGBAlpha(UsageColor(1, true, snap.WarningThreshold, pal.IsLight, ""), uint8(255*contentAlpha))
			} else {
				txtCol = win32.RGBAlpha(pal.TextPrimary, uint8(255*contentAlpha))
			}
			if u.State == model.StateStale && hasReading {
				txtCol = win32.RGBAlpha(txtCol, uint8(180*contentAlpha))
			}
			lblY := slot.cy + ringDia/2 + ringToText
			c.DrawText(txt, slot.cx-24*s, lblY, 48*s, 17*s, 12.5*s, true, win32.AlignCenter, win32.AlignNear, txtCol, true)
		}
	}

	// Hover detail card with UsageBubbleShape pointer
	if p.cardAlpha > 0.02 && p.hoveredIdx >= 0 && p.hoveredIdx < len(p.slots) && !p.dragging && open > 0.85 {
		p.drawDetailCard(c, snap, pal, p.slots[p.hoveredIdx], time.Now(), i18n.IsChinese())
	} else {
		p.cardRect = win32.RectF{}
	}

	c.CommitToWindow(p.hwnd, p.winX, p.winY)
}

func (p *FloatingPanel) drawDetailCard(c *win32.LayeredCanvas, snap model.AppSettings, pal Palette, slot slotRect, now time.Time, isZh bool) {
	s := p.scale
	a := p.cardAlpha
	u := p.store.Usage(slot.provider)
	info, _ := model.FindProvider(slot.provider)

	bodyW := 250 * s
	pad := 18 * s
	headerH := 20 * s
	rowGap := 14 * s
	ptrW := 20 * s
	ptrH := 40 * s
	hGap := 8 * s
	cornerR := 20 * s

	rowH := 46 * s
	if snap.UsageBarStyle == model.BarSegmented {
		rowH = 52 * s
	}
	if snap.ShowsForecast {
		rowH += 16 * s
	}

	numRows := len(u.Windows)
	if numRows > 6 {
		numRows = 6
	}
	bodyH := pad*2 + headerH
	if u.State == model.StateUnavailable && len(u.Windows) == 0 {
		bodyH += rowGap + 38*s
	} else {
		bodyH += float32(numRows) * (rowGap + rowH)
		if len(u.Windows) == 0 && u.CreditBalance != "" {
			bodyH += rowGap + 24*s
		} else if u.CreditBalance != "" && len(u.Windows) > 0 {
			bodyH += rowGap + 18*s
		}
	}
	if u.State == model.StateStale {
		bodyH += rowGap + 16*s
	}

	var bx, by, ptrCenter float32
	dock := p.liveDock
	if !dock.IsHorizontal() {
		if p.cardOpensRight {
			bx = p.railX + p.railW + hGap + ptrW
			p.cardRect = win32.RectF{X: bx - ptrW, Y: by, W: bodyW + ptrW, H: bodyH}
		} else {
			bx = p.railX - hGap - ptrW - bodyW
			p.cardRect = win32.RectF{X: bx, Y: by, W: bodyW + ptrW, H: bodyH}
		}
		by = clampF(slot.cy-bodyH/2, 4*s, maxF(4*s, float32(p.winH)-bodyH-4*s))
		ptrCenter = p.ptrCoord - by
		p.cardRect.Y = by
	} else {
		bx = clampF(slot.cx-bodyW/2, 4*s, maxF(4*s, float32(p.winW)-bodyW-4*s))
		if p.cardOpensDown {
			by = p.railY + p.railH + hGap + ptrW
			p.cardRect = win32.RectF{X: bx, Y: by - ptrW, W: bodyW, H: bodyH + ptrW}
		} else {
			by = p.railY - hGap - ptrW - bodyH
			p.cardRect = win32.RectF{X: bx, Y: by, W: bodyW, H: bodyH + ptrW}
		}
		ptrCenter = p.ptrCoord - bx
	}

	bubblePath := BuildUsageBubblePath(dock, p.cardOpensRight, p.cardOpensDown, snap.UsesRoundEnds,
		bx, by, bodyW, bodyH, ptrW, ptrH, cornerR, ptrCenter)
	if bubblePath != 0 {
		c.FillPath(bubblePath, win32.RGBAlpha(pal.CardBg, uint8(255*a)))
		if pal.IsLight || snap.Surface == model.SurfaceTranslucent {
			c.DrawPath(bubblePath, 1.0, win32.RGBAlpha(pal.CardBorder, uint8(220*a)), false)
		}
		win32.DeletePath(bubblePath)
	}

	// Header: icon + "{Name} Usage" + optional plan badge
	iconSz := 16 * s
	iconCol := win32.RGBAlpha(pal.TextPrimary, uint8(255*a))
	title := i18n.TF("%s Usage", "%s 用量", info.DisplayName)
	c.DrawSVGIcon(info.Icon, bx+pad, by+pad+2*s, iconSz, iconCol)

	planW := float32(0)
	if u.Plan != "" {
		planW = 62 * s
	}
	c.DrawText(title, bx+pad+iconSz+8*s, by+pad, bodyW-pad*2-iconSz-8*s-planW, headerH, 14*s, true, win32.AlignNear, win32.AlignCenter, iconCol, true)
	if u.Plan != "" {
		pillX := bx + bodyW - pad - planW + 4*s
		c.FillRoundRect(pillX, by+pad+1*s, planW-4*s, 18*s, 9*s, win32.ARGB(uint8(32*a), 255, 255, 255))
		c.DrawText(u.Plan, pillX+4*s, by+pad+1*s, planW-12*s, 18*s, 10*s, true, win32.AlignCenter, win32.AlignCenter, win32.RGBAlpha(pal.TextSecondary, uint8(255*a)), true)
	}

	curY := by + pad + headerH + rowGap

	if u.State == model.StateUnavailable && len(u.Windows) == 0 {
		c.DrawText(u.Reason.Message(isZh), bx+pad, curY, bodyW-pad*2, 38*s, 12*s, false, win32.AlignNear, win32.AlignNear, win32.RGBAlpha(pal.TextSecondary, uint8(255*a)), false)
		return
	}

	if len(u.Windows) == 0 && u.CreditBalance != "" {
		c.DrawText(i18n.T("Credit balance", "可用余额"), bx+pad, curY, 100*s, 20*s, 12*s, false, win32.AlignNear, win32.AlignCenter, win32.RGBAlpha(pal.TextSecondary, uint8(255*a)), true)
		c.DrawText(u.CreditBalance, bx+pad+100*s, curY, bodyW-pad*2-100*s, 22*s, 14*s, true, win32.AlignFar, win32.AlignCenter, win32.RGBAlpha(pal.TextPrimary, uint8(255*a)), true)
		return
	}

	barW := bodyW - pad*2
	for idx := 0; idx < numRows; idx++ {
		w := u.Windows[idx]
		c.DrawText(w.DisplayName(isZh), bx+pad, curY, barW, 15*s, 11.5*s, false, win32.AlignNear, win32.AlignNear, win32.RGBAlpha(pal.TextPrimary, uint8(255*a)), true)
		curY += 15*s + 7*s

		fillCol := win32.RGBAlpha(UsageColor(w.UsedFraction, w.IsExhausted, snap.WarningThreshold, pal.IsLight, ""), uint8(255*a))
		fillFrac := w.UsedFraction
		if snap.ShowsRemaining && !w.IsExhausted {
			fillFrac = w.RemainingFraction()
		}

		if snap.UsageBarStyle == model.BarSegmented {
			barH := 12 * s
			tickW := 2.5 * s
			tickGap := 2.0 * s
			cnt := int((barW + tickGap) / (tickW + tickGap))
			if cnt < 4 {
				cnt = 4
			}
			lit := int(math.Round(fillFrac * float64(cnt)))
			if fillFrac > 0 && lit == 0 {
				lit = 1
			}
			for t := 0; t < cnt; t++ {
				tx := bx + pad + float32(t)*(tickW+tickGap)
				col := win32.RGBAlpha(pal.BarTrack, uint8(255*a))
				if t < lit {
					col = fillCol
				}
				c.FillRoundRect(tx, curY, tickW, barH, 1.2*s, col)
			}
			curY += barH + 7*s
		} else {
			barH := 6 * s
			c.FillRoundRect(bx+pad, curY, barW, barH, barH/2, win32.RGBAlpha(pal.BarTrack, uint8(255*a)))
			fw := float32(fillFrac) * barW
			if fillFrac > 0 && fw < barH {
				fw = barH
			}
			if fw > 0 {
				c.FillRoundRect(bx+pad, curY, fw, barH, barH/2, fillCol)
			}
			curY += barH + 7*s
		}

		pctStr := i18n.TF("%s Used", "已用 %s", w.PercentText(false))
		if snap.ShowsRemaining {
			pctStr = i18n.TF("%s Left", "剩余 %s", w.PercentText(true))
		}
		pctCol := win32.RGBAlpha(pal.TextSecondary, uint8(255*a))
		if w.IsExhausted || w.UsedFraction >= 1 {
			pctCol = fillCol
			pctStr = i18n.T("Exhausted", "已用尽")
		}
		rst := model.FormatResetCountdown(w.ResetsAt, now, isZh)
		c.DrawText(pctStr, bx+pad, curY, barW*0.44, 15*s, 11.5*s, false, win32.AlignNear, win32.AlignNear, pctCol, true)
		if rst != "" {
			c.DrawText(rst, bx+pad+barW*0.44, curY, barW*0.56, 15*s, 11.5*s, false, win32.AlignFar, win32.AlignNear, win32.RGBAlpha(pal.TextMuted, uint8(255*a)), true)
		}
		curY += 15 * s

		if snap.ShowsForecast {
			if eta, ok := w.ProjectedExhaustion(now); ok {
				mins := int(eta.Minutes())
				var etaStr string
				if mins >= 60 {
					etaStr = i18n.TF("At this pace, runs out in %dh %dm", "按当前速度约 %d小时%d分 后用尽", mins/60, mins%60)
				} else {
					etaStr = i18n.TF("At this pace, runs out in %dm", "按当前速度约 %d 分钟后用尽", mins)
				}
				c.DrawText(etaStr, bx+pad, curY+3*s, barW, 14*s, 10.5*s, false, win32.AlignNear, win32.AlignNear, win32.RGBAlpha(UsageColor(0.85, false, snap.WarningThreshold, pal.IsLight, ""), uint8(255*a)), true)
			}
			curY += 16 * s
		}
		curY += rowGap
	}

	if u.CreditBalance != "" && len(u.Windows) > 0 {
		c.DrawText(i18n.TF("Balance: %s", "余额：%s", u.CreditBalance), bx+pad, curY, barW, 16*s, 11*s, false, win32.AlignNear, win32.AlignNear, win32.RGBAlpha(pal.TextSecondary, uint8(255*a)), true)
		curY += 18 * s
	}
	if u.State == model.StateStale {
		c.DrawText(model.FormatObservedAgo(u.ObservedAt, now, isZh), bx+pad, curY, barW, 14*s, 10.5*s, false, win32.AlignNear, win32.AlignNear, win32.RGBAlpha(pal.TextMuted, uint8(255*a)), true)
	}
}

func (p *FloatingPanel) onHoverTick() {
	if p.hwnd == 0 || !win32.IsWindowVisible(p.hwnd) || p.dragging || p.menuOpen {
		return
	}
	snap := p.settings.Snapshot()
	cx, cy := win32.GetCursorPos()
	lx := float32(cx - p.winX)
	ly := float32(cy - p.winY)

	inRail := false
	if !p.isExpanded && snap.AutoCollapse && p.liveDock.IsDocked() {
		sx, sy, sw, sh := p.sliverBounds(p.liveDock)
		hitPad := 12 * p.scale
		if lx >= sx-hitPad && lx <= sx+sw+hitPad && ly >= sy-hitPad && ly <= sy+sh+hitPad {
			p.isExpanded = true
			p.targetOpen = 1.0
			p.lastInsideAt = time.Now()
			p.startAnim()
		}
		return
	}

	inRail = lx >= p.railX && lx <= p.railX+p.railW && ly >= p.railY && ly <= p.railY+p.railH
	inCard := p.cardVisible && p.cardRect.W > 0 &&
		lx >= p.cardRect.X && lx <= p.cardRect.X+p.cardRect.W &&
		ly >= p.cardRect.Y && ly <= p.cardRect.Y+p.cardRect.H

	hitIdx := -1
	if inRail {
		p.lastInsideAt = time.Now()
		for i, sl := range p.slots {
			if lx >= sl.x && lx <= sl.x+sl.w && ly >= sl.y && ly <= sl.y+sl.h {
				hitIdx = i
				break
			}
		}
	} else if inCard {
		p.lastInsideAt = time.Now()
		hitIdx = p.hoveredIdx
	}

	needAnim := false
	if hitIdx >= 0 {
		if hitIdx != p.hoveredIdx {
			p.hoveredIdx = hitIdx
			p.cardVisible = true
			t := p.slots[hitIdx].cy
			if p.liveDock.IsHorizontal() {
				t = p.slots[hitIdx].cx
			}
			if p.cardAlpha < 0.05 {
				p.ptrCoord = t
			}
			p.targetPtr = t
			needAnim = true
		}
	} else if !inRail && !inCard && p.cardVisible {
		p.cardVisible = false
		needAnim = true
	}

	if snap.AutoCollapse && p.liveDock.IsDocked() && !inRail && !inCard {
		if time.Since(p.lastInsideAt) > 900*time.Millisecond && p.isExpanded {
			p.isExpanded = false
			p.targetOpen = 0.0
			p.cardVisible = false
			needAnim = true
		}
	}

	if snap.AnimatesActivity {
		now := time.Now()
		for _, sl := range p.slots {
			if p.isProviderSpinning(sl.provider, now) {
				needAnim = true
				break
			}
		}
	}
	if needAnim {
		p.startAnim()
	}
}

func (p *FloatingPanel) isProviderSpinning(id model.ProviderID, now time.Time) bool {
	if p.store.IsRefreshing(id) {
		return true
	}
	if until, ok := p.spinUntil[id]; ok {
		if now.Before(until) {
			return true
		}
		delete(p.spinUntil, id)
	}
	return false
}

func (p *FloatingPanel) onAnimTick() {
	snap := p.settings.Snapshot()
	changed := false

	// Smooth berth openness (0 <-> 1)
	if math.Abs(float64(p.targetOpen-p.openness)) > 0.008 {
		p.openness += (p.targetOpen - p.openness) * 0.24
		changed = true
	} else if p.openness != p.targetOpen {
		p.openness = p.targetOpen
		changed = true
	}

	// Smooth card reveal
	targetAlpha := float32(0)
	if p.cardVisible {
		targetAlpha = 1
	}
	if math.Abs(float64(targetAlpha-p.cardAlpha)) > 0.01 {
		p.cardAlpha += (targetAlpha - p.cardAlpha) * 0.28
		changed = true
	} else if p.cardAlpha != targetAlpha {
		p.cardAlpha = targetAlpha
		if p.cardAlpha == 0 {
			p.hoveredIdx = -1
		}
		changed = true
	}

	// Smooth pointer slide along card
	if math.Abs(float64(p.targetPtr-p.ptrCoord)) > 0.4 {
		p.ptrCoord += (p.targetPtr - p.ptrCoord) * 0.30
		changed = true
	} else if p.ptrCoord != p.targetPtr {
		p.ptrCoord = p.targetPtr
		changed = true
	}

	// Spinner only if actively refreshing or within click spin feedback window
	anyRef := false
	if snap.AnimatesActivity {
		now := time.Now()
		for _, sl := range p.slots {
			if p.isProviderSpinning(sl.provider, now) {
				anyRef = true
				break
			}
		}
	}
	if anyRef {
		p.spinPhase += 0.019 // ~0.85s per revolution at 60fps, matching UsageRingView.refreshPeriod
		if p.spinPhase >= 1 {
			p.spinPhase -= 1
		}
		changed = true
	}

	if changed {
		p.render()
	} else {
		p.stopAnim()
	}
}

func (p *FloatingPanel) onFollowDisplayTick() {
	if p.hwnd == 0 || !win32.IsWindowVisible(p.hwnd) || p.dragging {
		return
	}
	snap := p.settings.Snapshot()
	if !snap.FollowsActiveDisplay {
		return
	}
	cx, cy := win32.GetCursorPos()
	mons := win32.EnumMonitors()
	for idx, m := range mons {
		if cx >= m.Full.Left && cx < m.Full.Right && cy >= m.Full.Top && cy < m.Full.Bottom {
			if idx != snap.MonitorIndex {
				p.settings.Update(func(s *model.AppSettings) {
					s.MonitorIndex = idx
				})
				p.applySettingsAndPlace()
			}
			break
		}
	}
}

func (p *FloatingPanel) handleMouseDown(lx, ly int32) {
	if float32(lx) < p.railX || float32(lx) > p.railX+p.railW || float32(ly) < p.railY || float32(ly) > p.railY+p.railH {
		return
	}
	cx, cy := win32.GetCursorPos()
	p.dragging = true
	p.dragMoved = false
	p.dragStartX = cx
	p.dragStartY = cy
	p.railStartX = p.winX + int32(p.railX)
	p.railStartY = p.winY + int32(p.railY)
	p.cachedMonitors = win32.EnumMonitors()
	if p.cardVisible {
		p.cardVisible = false
		p.cardAlpha = 0
		p.hoveredIdx = -1
		p.render()
	}
	win32.SetCapture(p.hwnd)
}

func (p *FloatingPanel) handleMouseMove() {
	if !p.dragging {
		return
	}
	cx, cy := win32.GetCursorPos()
	dx := cx - p.dragStartX
	dy := cy - p.dragStartY
	if !p.dragMoved && (dx*dx+dy*dy) < 25 {
		return
	}
	p.dragMoved = true

	newRailX := float64(p.railStartX + dx)
	newRailY := float64(p.railStartY + dy)

	monIdx := 0
	mon := win32.MonitorBounds{
		Full: win32.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		Work: win32.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1040},
	}
	for i, m := range p.cachedMonitors {
		if cx >= m.Full.Left && cx < m.Full.Right && cy >= m.Full.Top && cy < m.Full.Bottom {
			monIdx = i
			mon = m
			break
		}
	}
	if len(p.cachedMonitors) > 0 && mon.Full.Width() == 0 {
		mon = p.cachedMonitors[0]
	}

	snap := p.settings.Snapshot()
	room := mon.Work
	if !snap.SnapsToEdges {
		room = mon.Full
	}

	oldDock := p.liveDock
	nextDock := oldDock
	dockDist := float64(32 * p.scale)
	if snap.SnapsToEdges {
		switch {
		case float64(cx-room.Left) <= dockDist:
			nextDock = model.DockLeft
		case float64(room.Right-cx) <= dockDist:
			nextDock = model.DockRight
		case float64(cy-room.Top) <= dockDist:
			nextDock = model.DockTop
		case float64(room.Bottom-cy) <= dockDist:
			nextDock = model.DockBottom
		default:
			if oldDock.IsHorizontal() {
				nextDock = model.DockFloatHoriz
			} else {
				nextDock = model.DockFloatVertical
			}
		}
	} else {
		if oldDock.IsHorizontal() {
			nextDock = model.DockFloatHoriz
		} else {
			nextDock = model.DockFloatVertical
		}
	}

	availW := float64(room.Width()) - float64(p.railW)
	availH := float64(room.Height()) - float64(p.railH)
	hRatio, vRatio := 0.5, 0.5
	if availW > 0 {
		hRatio = (newRailX - float64(room.Left)) / availW
	}
	if availH > 0 {
		vRatio = (newRailY - float64(room.Top)) / availH
	}
	if hRatio < 0 {
		hRatio = 0
	} else if hRatio > 1 {
		hRatio = 1
	}
	if vRatio < 0 {
		vRatio = 0
	} else if vRatio > 1 {
		vRatio = 1
	}

	oldW, oldH := p.winW, p.winH
	oldOpensRight, oldOpensDown := p.cardOpensRight, p.cardOpensDown

	p.liveDock = nextDock
	p.liveMonIdx = monIdx
	p.liveHRatio = hRatio
	p.liveVRatio = vRatio
	p.computeLayout(snap, p.settings.OrderedEnabled())

	// Fast path: move GPU-composited window without redrawing unless silhouette changed!
	if nextDock != oldDock || p.winW != oldW || p.winH != oldH ||
		p.cardOpensRight != oldOpensRight || p.cardOpensDown != oldOpensDown {
		win32.SetWindowPos(p.hwnd, win32.HWND_TOPMOST, p.winX, p.winY, p.winW, p.winH, win32.SWP_NOACTIVATE)
		p.render()
	} else {
		win32.SetWindowPos(p.hwnd, win32.HWND_TOPMOST, p.winX, p.winY, 0, 0, win32.SWP_NOSIZE|win32.SWP_NOZORDER|win32.SWP_NOACTIVATE)
	}
}

func (p *FloatingPanel) handleMouseUp(lx, ly int32) {
	if !p.dragging {
		return
	}
	wasMoved := p.dragMoved
	p.dragging = false
	win32.ReleaseCapture()

	if wasMoved {
		// Persist placement ONCE on drop (zero disk I/O during drag)
		p.settings.Update(func(s *model.AppSettings) {
			s.Dock = p.liveDock
			s.MonitorIndex = p.liveMonIdx
			s.HorizontalRatio = p.liveHRatio
			s.VerticalRatio = p.liveVRatio
		})
	} else {
		for _, sl := range p.slots {
			if float32(lx) >= sl.x && float32(lx) <= sl.x+sl.w && float32(ly) >= sl.y && float32(ly) <= sl.y+sl.h {
				p.spinUntil[sl.provider] = time.Now().Add(850 * time.Millisecond)
				p.store.RefreshOne(sl.provider)
				p.startAnim()
				break
			}
		}
	}
	p.render()
}

func (p *FloatingPanel) showContextMenu() {
	p.menuOpen = true
	defer func() { p.menuOpen = false }()

	snap := p.settings.Snapshot()
	hMenu := win32.CreatePopupMenu()
	defer win32.DestroyMenu(hMenu)

	win32.AppendMenu(hMenu, win32.MF_STRING, menuCmdRefreshAll, i18n.T("Refresh Now", "立即刷新"))
	remFlags := uint32(win32.MF_STRING)
	if snap.ShowsRemaining {
		remFlags |= win32.MF_CHECKED
	}
	win32.AppendMenu(hMenu, remFlags, menuCmdToggleRem, i18n.T("Show Remaining %", "显示剩余百分比"))
	ahFlags := uint32(win32.MF_STRING)
	if snap.AutoCollapse {
		ahFlags |= win32.MF_CHECKED
	}
	win32.AppendMenu(hMenu, ahFlags, menuCmdAutoHide, i18n.T("Auto-Collapse Edge Sliver", "闲置时收起为边缘细线"))
	win32.AppendMenu(hMenu, win32.MF_STRING, menuCmdHidePanel, i18n.T("Hide Floating Panel", "隐藏悬浮面板"))
	win32.AppendMenu(hMenu, win32.MF_SEPARATOR, 0, "")
	win32.AppendMenu(hMenu, win32.MF_STRING, menuCmdSettings, i18n.T("Settings…", "设置…"))
	win32.AppendMenu(hMenu, win32.MF_STRING, menuCmdQuit, i18n.T("Quit Pulse", "退出 Pulse"))

	cx, cy := win32.GetCursorPos()
	win32.SetForegroundWindow(p.hwnd)
	cmd := win32.TrackPopupMenu(hMenu, win32.TPM_RETURNCMD, cx, cy, p.hwnd)
	switch cmd {
	case menuCmdRefreshAll:
		now := time.Now().Add(850 * time.Millisecond)
		for _, sl := range p.slots {
			p.spinUntil[sl.provider] = now
		}
		p.startAnim()
		p.store.TriggerRefreshAll()
	case menuCmdToggleRem:
		p.settings.Update(func(s *model.AppSettings) { s.ShowsRemaining = !s.ShowsRemaining })
		p.render()
	case menuCmdAutoHide:
		p.settings.Update(func(s *model.AppSettings) { s.AutoCollapse = !s.AutoCollapse })
		p.applySettingsAndPlace()
	case menuCmdHidePanel:
		p.settings.Update(func(s *model.AppSettings) { s.IsPanelVisible = false })
		p.applySettingsAndPlace()
	case menuCmdSettings:
		if p.onOpenSettings != nil {
			p.onOpenSettings("")
		}
	case menuCmdQuit:
		if p.onQuit != nil {
			p.onQuit()
		}
	}
}

func floatingPanelWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	p := globalPanel
	if p == nil {
		return win32.DefWindowProc(hwnd, msg, wParam, lParam)
	}
	switch msg {
	case win32.WM_TIMER:
		switch wParam {
		case timerHoverPoll:
			p.onHoverTick()
		case timerAnimTick:
			p.onAnimTick()
		case timerFollowDisp:
			p.onFollowDisplayTick()
		}
		return 0
	case wmAppRepaint:
		p.render()
		return 0
	case wmAppRelayout:
		p.applySettingsAndPlace()
		return 0
	case win32.WM_LBUTTONDOWN:
		lx := int32(int16(lParam & 0xFFFF))
		ly := int32(int16((lParam >> 16) & 0xFFFF))
		p.handleMouseDown(lx, ly)
		return 0
	case win32.WM_MOUSEMOVE:
		p.handleMouseMove()
		return 0
	case win32.WM_LBUTTONUP:
		lx := int32(int16(lParam & 0xFFFF))
		ly := int32(int16((lParam >> 16) & 0xFFFF))
		p.handleMouseUp(lx, ly)
		return 0
	case win32.WM_RBUTTONUP:
		p.showContextMenu()
		return 0
	case win32.WM_DPICHANGED:
		p.applySettingsAndPlace()
		return 0
	}
	return win32.DefWindowProc(hwnd, msg, wParam, lParam)
}
