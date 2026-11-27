//go:build shots

package ui

import (
	"image"
	"time"

	"pulse-windows/internal/auth"
	"pulse-windows/internal/model"
	"pulse-windows/internal/store"
	"pulse-windows/internal/win32"
)

func RenderPanelPreview(settings *model.AppSettings, st *store.UsageStore, scale float32, hoverIdx int) (*image.NRGBA, int32, int32, float32, float32, float32, float32) {
	p := NewFloatingPanel(settings, st, nil, nil)
	p.dpi = float64(scale)
	p.scale = scale
	p.liveDock = settings.Snapshot().Dock
	p.liveMonIdx = 0
	p.liveHRatio = 0.5
	p.liveVRatio = 0.5
	p.cachedMonitors = []win32.MonitorBounds{{
		Index: 0,
		Full:  win32.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		Work:  win32.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1032},
	}}
	p.isExpanded = true
	p.openness = 1.0
	p.targetOpen = 1.0
	p.computeLayout(settings.Snapshot(), settings.OrderedEnabled())
	if hoverIdx >= 0 && hoverIdx < len(p.slots) {
		p.hoveredIdx = hoverIdx
		p.cardVisible = true
		p.cardAlpha = 1.0
		p.ptrCoord = p.slots[hoverIdx].cy
		p.targetPtr = p.ptrCoord
	}
	// Create a hidden layered window so CommitToWindow succeeds cleanly without showing on desktop
	_ = p.Create()
	// Re-apply preview state after Create()
	p.dpi = float64(scale)
	p.scale = scale
	p.cachedMonitors = []win32.MonitorBounds{{
		Index: 0,
		Full:  win32.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		Work:  win32.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1032},
	}}
	p.computeLayout(settings.Snapshot(), settings.OrderedEnabled())
	if hoverIdx >= 0 && hoverIdx < len(p.slots) {
		p.hoveredIdx = hoverIdx
		p.cardVisible = true
		p.cardAlpha = 1.0
		p.ptrCoord = p.slots[hoverIdx].cy
		p.targetPtr = p.ptrCoord
	}
	win32.ShowWindow(p.hwnd, win32.SW_HIDE)
	p.render()
	img := p.canvas.ToNRGBA()
	rx, ry, rw, rh := p.railX, p.railY, p.railW, p.railH
	w, h := p.winW, p.winH
	win32.DestroyWindow(p.hwnd)
	p.canvas.Close()
	return img, w, h, rx, ry, rw, rh
}

func RenderTrayPopupPreview(settings *model.AppSettings, st *store.UsageStore) *image.NRGBA {
	t := NewTrayManager(settings, st, nil, nil, nil)
	_ = t.Create()
	win32.ShowWindow(t.popupHwnd, win32.SW_HIDE)
	s := float32(win32.GetDpiForWindow(t.popupHwnd))
	rows := len(settings.OrderedEnabled())
	t.popupVisible = true
	t.popupW = int32(332 * s)
	t.popupH = int32((106 + float32(rows)*62) * s)
	t.renderPopup()
	img := t.popupCanvas.ToNRGBA()
	t.Destroy()
	return img
}

func RenderSettingsPreview(settings *model.AppSettings, keys *auth.KeyStore, st *store.UsageStore, pane string) *image.NRGBA {
	sw := NewSettingsWindow(settings, keys, st, nil, nil)
	sw.Open("")
	win32.ShowWindow(sw.hwnd, win32.SW_HIDE)
	sw.activePane = pane
	sw.syncInputsForPane()
	sw.paint(0)
	img := sw.canvas.ToNRGBA()
	win32.DestroyWindow(sw.hwnd)
	return img
}

var _ = time.Now
