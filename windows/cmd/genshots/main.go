//go:build shots

package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"pulse-windows/internal/auth"
	"pulse-windows/internal/i18n"
	"pulse-windows/internal/model"
	"pulse-windows/internal/store"
	"pulse-windows/internal/ui"
	"pulse-windows/internal/win32"
)

func main() {
	runtime.LockOSThread()
	win32.InitDPIAwareness()
	win32.InitGDIPlus()
	defer win32.ShutdownGDIPlus()

	i18n.SetLanguage(model.LangZhCN)

	settings := model.DefaultSettings()
	settings.EnabledProviders = []string{
		string(model.ProviderClaudeCode),
		string(model.ProviderCodex),
		string(model.ProviderCursor),
		string(model.ProviderAntigravity),
	}
	settings.Dock = model.DockRight
	settings.Surface = model.SurfaceObsidian
	settings.ShowsRemaining = true
	settings.ShowsSecondRing = true
	settings.ShowsForecast = true
	settings.HasCompletedSetup = true

	st := store.NewUsageStore(settings, nil, store.NewCache())
	now := time.Now()
	t3h := now.Add(3*time.Hour + 18*time.Minute)
	t4d := now.Add(4*24*time.Hour + 6*time.Hour)
	t12d := now.Add(12 * 24 * time.Hour)
	t4h := now.Add(4*time.Hour + 12*time.Minute)

	st.SeedUsageForPreview(model.ProviderUsage{
		Provider:   model.ProviderClaudeCode,
		State:      model.StateLive,
		ObservedAt: &now,
		Plan:       "Max 5x",
		Windows: []model.UsageWindow{
			{ID: "session", Kind: model.KindFiveHour, UsedFraction: 0.24, WindowSeconds: 5 * 3600, ResetsAt: &t3h, ReportsLength: true},
			{ID: "weekly", Kind: model.KindWeekly, Scope: "All Models", UsedFraction: 0.41, WindowSeconds: 7 * 86400, ResetsAt: &t4d, ReportsLength: true},
		},
	})
	st.SeedUsageForPreview(model.ProviderUsage{
		Provider:   model.ProviderCodex,
		State:      model.StateLive,
		ObservedAt: &now,
		Plan:       "Plus",
		Windows: []model.UsageWindow{
			{ID: "5h", Kind: model.KindFiveHour, UsedFraction: 0.18, WindowSeconds: 5 * 3600, ResetsAt: &t3h, ReportsLength: true},
			{ID: "weekly", Kind: model.KindWeekly, UsedFraction: 0.34, WindowSeconds: 7 * 86400, ResetsAt: &t4d, ReportsLength: true},
		},
	})
	st.SeedUsageForPreview(model.ProviderUsage{
		Provider:   model.ProviderCursor,
		State:      model.StateLive,
		ObservedAt: &now,
		Plan:       "Pro",
		Windows: []model.UsageWindow{
			{ID: "auto", Kind: model.KindMonthly, Scope: "Included", UsedFraction: 0.62, WindowSeconds: 30 * 86400, ResetsAt: &t12d, ReportsLength: true},
			{ID: "api", Kind: model.KindMonthly, Scope: "On-Demand", UsedFraction: 0.12, WindowSeconds: 30 * 86400, ResetsAt: &t12d, ReportsLength: true},
		},
	})
	st.SeedUsageForPreview(model.ProviderUsage{
		Provider:   model.ProviderAntigravity,
		State:      model.StateLive,
		ObservedAt: &now,
		Plan:       "Pro",
		Windows: []model.UsageWindow{
			{ID: "gemini_5h", Kind: model.KindFiveHour, Scope: "Gemini", UsedFraction: 0.08, WindowSeconds: 5 * 3600, ResetsAt: &t4h, ReportsLength: true},
			{ID: "claude_weekly", Kind: model.KindWeekly, Scope: "Claude & GPT", UsedFraction: 0.28, WindowSeconds: 7 * 86400, ResetsAt: &t4d, ReportsLength: true},
		},
	})

	outDir := filepath.FromSlash("docs/images")
	_ = os.MkdirAll(outDir, 0o755)

	// 1. Panel + Hover Card over dark Win11 desktop backdrop
	panelImg, pw, ph, _, _, _, _ := ui.RenderPanelPreview(settings, st, 1.5, 3)
	bgW, bgH := int(pw)+64, int(ph)+48
	bg := makeBackdrop(bgW, bgH)
	// Align right edge of panel cleanly against right edge of backdrop
	dstRect := image.Rect(bgW-int(pw), (bgH-int(ph))/2, bgW, (bgH-int(ph))/2+int(ph))
	draw.Draw(bg, dstRect, panelImg, image.Point{}, draw.Over)
	savePNG(filepath.Join(outDir, "panel.png"), bg)

	// 2. Tray Popup over sleek dark taskbar frame
	trayImg := ui.RenderTrayPopupPreview(settings, st)
	tb := trayImg.Bounds()
	tBgW, tBgH := tb.Dx()+64, tb.Dy()+64
	tBg := makeBackdrop(tBgW, tBgH)
	draw.Draw(tBg, image.Rect(32, 32, 32+tb.Dx(), 32+tb.Dy()), trayImg, image.Point{}, draw.Over)
	savePNG(filepath.Join(outDir, "tray-popup.png"), tBg)

	// 3. Settings Window (HiDPI rendered cleanly, with dark Win11 titlebar)
	swImg := ui.RenderSettingsPreview(settings, auth.NewKeyStore(), st, "appearance")
	swW, swH := swImg.Bounds().Dx(), swImg.Bounds().Dy()
	dpi := float32(win32.GetDpiForWindow(0))
	titleH := int(34 * dpi)
	winFull := image.NewNRGBA(image.Rect(0, 0, swW, swH+titleH))
	draw.Draw(winFull, image.Rect(0, titleH, swW, swH+titleH), swImg, image.Point{}, draw.Over)
	// Subtle dark titlebar caption via GDI+
	tc := win32.NewLayeredCanvas(int32(swW), int32(titleH))
	tc.Clear(win32.ARGB(255, 24, 25, 29))
	tc.DrawSVGIcon("pulse-mark", 14*dpi, 9*dpi, 15*dpi, win32.ARGB(255, 34, 197, 94))
	tc.DrawText("Pulse 设置", 36*dpi, 0, 200*dpi, float32(titleH), 12*dpi, false, win32.AlignNear, win32.AlignCenter, win32.ARGB(255, 226, 232, 240), false)
	tc.DrawLine(0, float32(titleH)-1, float32(swW), float32(titleH)-1, 1, win32.ARGB(255, 40, 42, 50))
	draw.Draw(winFull, image.Rect(0, 0, swW, titleH), tc.ToNRGBA(), image.Point{}, draw.Src)
	tc.Close()
	savePNG(filepath.Join(outDir, "settings.png"), winFull)

	fmt.Println("Generated screenshots in", outDir)
}

func makeBackdrop(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		ny := float64(y) / float64(h)
		for x := 0; x < w; x++ {
			nx := float64(x) / float64(w)
			// Subtle Windows 11 Bloom dark indigo-teal atmospheric gradient
			glow := math.Exp(-((nx-0.25)*(nx-0.25)*3.0 + (ny-0.35)*(ny-0.35)*3.5))
			glow2 := math.Exp(-((nx-0.80)*(nx-0.80)*4.0 + (ny-0.75)*(ny-0.75)*4.0))
			r := clampByte(16 + glow*22 + glow2*10)
			g := clampByte(22 + glow*44 + glow2*38)
			b := clampByte(38 + glow*78 + glow2*58)
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}

func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func savePNG(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}
