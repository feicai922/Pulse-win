package ui

import (
	"strconv"
	"strings"

	"pulse-windows/internal/model"
	"pulse-windows/internal/win32"
)

type Palette struct {
	IsLight       bool
	PanelBg       uint32
	PanelBorder   uint32
	CardBg        uint32
	CardBorder    uint32
	TextPrimary   uint32
	TextSecondary uint32
	TextMuted     uint32
	RingTrack     uint32
	RingCenter    uint32
	BarTrack      uint32
	Divider       uint32
	HoverBg       uint32
}

func GetPalette(surface model.SurfaceStyle, glassOpacity int) Palette {
	if surface == model.SurfaceLight {
		return Palette{
			IsLight:       true,
			PanelBg:       win32.ARGB(246, 245, 246, 249),
			PanelBorder:   win32.ARGB(255, 210, 214, 222),
			CardBg:        win32.ARGB(250, 250, 251, 253),
			CardBorder:   win32.ARGB(255, 206, 210, 220),
			TextPrimary:   win32.ARGB(255, 20, 22, 26),
			TextSecondary: win32.ARGB(255, 84, 90, 102),
			TextMuted:     win32.ARGB(255, 130, 136, 148),
			RingTrack:     win32.ARGB(255, 220, 224, 232),
			RingCenter:    win32.ARGB(255, 236, 238, 243),
			BarTrack:      win32.ARGB(255, 224, 228, 235),
			Divider:       win32.ARGB(255, 226, 230, 236),
			HoverBg:       win32.ARGB(30, 0, 0, 0),
		}
	}
	alpha := uint8(248)
	if surface == model.SurfaceTranslucent {
		if glassOpacity < 25 {
			glassOpacity = 25
		}
		if glassOpacity > 100 {
			glassOpacity = 100
		}
		alpha = uint8(float64(glassOpacity) * 2.55)
	}
	return Palette{
		IsLight:       false,
		PanelBg:       win32.ARGB(alpha, 14, 15, 19),
		PanelBorder:   win32.ARGB(190, 56, 60, 72),
		CardBg:        win32.ARGB(250, 18, 19, 24),
		CardBorder:    win32.ARGB(210, 62, 66, 80),
		TextPrimary:   win32.ARGB(255, 245, 247, 250),
		TextSecondary: win32.ARGB(255, 168, 174, 188),
		TextMuted:     win32.ARGB(255, 118, 124, 138),
		RingTrack:     win32.ARGB(255, 42, 45, 56),
		RingCenter:    win32.ARGB(255, 24, 26, 33),
		BarTrack:      win32.ARGB(255, 44, 48, 60),
		Divider:       win32.ARGB(255, 46, 50, 62),
		HoverBg:       win32.ARGB(36, 255, 255, 255),
	}
}

func UsageColor(usedFraction float64, isExhausted bool, warningThresholdPct int, isLight bool, customHex string) uint32 {
	if isExhausted || usedFraction >= 1.0 {
		if isLight {
			return win32.ARGB(255, 148, 18, 30)
		}
		return win32.ARGB(255, 196, 32, 48)
	}
	if customHex != "" {
		if c, ok := ParseHexARGB(customHex); ok {
			return c
		}
	}
	warn := float64(warningThresholdPct) / 100.0
	if warn < 0.55 || warn > 0.95 {
		warn = 0.75
	}
	if usedFraction < 0.50 {
		if isLight {
			return win32.ARGB(255, 0, 152, 92)
		}
		return win32.ARGB(255, 16, 224, 138)
	}
	if usedFraction < warn {
		if isLight {
			return win32.ARGB(255, 188, 122, 0)
		}
		return win32.ARGB(255, 255, 184, 16)
	}
	if isLight {
		return win32.ARGB(255, 208, 36, 36)
	}
	return win32.ARGB(255, 255, 72, 72)
}

func ParseHexARGB(hex string) (uint32, bool) {
	s := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(s) != 6 {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	r := uint8((v >> 16) & 0xFF)
	g := uint8((v >> 8) & 0xFF)
	b := uint8(v & 0xFF)
	return win32.ARGB(255, r, g, b), true
}

func BrandColorARGB(rgb uint32, isLight bool) uint32 {
	if rgb == 0 {
		if isLight {
			return win32.ARGB(255, 24, 26, 32)
		}
		return win32.ARGB(255, 236, 240, 246)
	}
	return win32.RGBAlpha(rgb, 255)
}
