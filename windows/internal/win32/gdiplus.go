package win32

import (
	"math"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdiplus = windows.NewLazySystemDLL("gdiplus.dll")

	procGdiplusStartup              = gdiplus.NewProc("GdiplusStartup")
	procGdiplusShutdown             = gdiplus.NewProc("GdiplusShutdown")
	procGdipCreateBitmapFromScan0   = gdiplus.NewProc("GdipCreateBitmapFromScan0")
	procGdipCreateHICONFromBitmap   = gdiplus.NewProc("GdipCreateHICONFromBitmap")
	procGdipDisposeImage            = gdiplus.NewProc("GdipDisposeImage")
	procGdipGetImageGraphicsContext = gdiplus.NewProc("GdipGetImageGraphicsContext")
	procGdipDeleteGraphics          = gdiplus.NewProc("GdipDeleteGraphics")
	procGdipFlush                   = gdiplus.NewProc("GdipFlush")
	procGdipGraphicsClear           = gdiplus.NewProc("GdipGraphicsClear")
	procGdipSetSmoothingMode        = gdiplus.NewProc("GdipSetSmoothingMode")
	procGdipSetTextRenderingHint    = gdiplus.NewProc("GdipSetTextRenderingHint")
	procGdipSetPixelOffsetMode      = gdiplus.NewProc("GdipSetPixelOffsetMode")
	procGdipCreateSolidFill         = gdiplus.NewProc("GdipCreateSolidFill")
	procGdipSetSolidFillColor       = gdiplus.NewProc("GdipSetSolidFillColor")
	procGdipDeleteBrush             = gdiplus.NewProc("GdipDeleteBrush")
	procGdipCreatePen1              = gdiplus.NewProc("GdipCreatePen1")
	procGdipSetPenColor             = gdiplus.NewProc("GdipSetPenColor")
	procGdipSetPenWidth             = gdiplus.NewProc("GdipSetPenWidth")
	procGdipSetPenStartCap          = gdiplus.NewProc("GdipSetPenStartCap")
	procGdipSetPenEndCap            = gdiplus.NewProc("GdipSetPenEndCap")
	procGdipSetPenLineJoin          = gdiplus.NewProc("GdipSetPenLineJoin")
	procGdipDeletePen               = gdiplus.NewProc("GdipDeletePen")
	procGdipCreatePath              = gdiplus.NewProc("GdipCreatePath")
	procGdipDeletePath              = gdiplus.NewProc("GdipDeletePath")
	procGdipClonePath               = gdiplus.NewProc("GdipClonePath")
	procGdipStartPathFigure         = gdiplus.NewProc("GdipStartPathFigure")
	procGdipClosePathFigure         = gdiplus.NewProc("GdipClosePathFigure")
	procGdipAddPathLine             = gdiplus.NewProc("GdipAddPathLine")
	procGdipAddPathLine2            = gdiplus.NewProc("GdipAddPathLine2")
	procGdipAddPathArc              = gdiplus.NewProc("GdipAddPathArc")
	procGdipAddPathBezier           = gdiplus.NewProc("GdipAddPathBezier")
	procGdipTransformPath           = gdiplus.NewProc("GdipTransformPath")
	procGdipCreateMatrix            = gdiplus.NewProc("GdipCreateMatrix")
	procGdipDeleteMatrix            = gdiplus.NewProc("GdipDeleteMatrix")
	procGdipTranslateMatrix         = gdiplus.NewProc("GdipTranslateMatrix")
	procGdipScaleMatrix             = gdiplus.NewProc("GdipScaleMatrix")
	procGdipFillPath                = gdiplus.NewProc("GdipFillPath")
	procGdipDrawPath                = gdiplus.NewProc("GdipDrawPath")
	procGdipFillEllipse             = gdiplus.NewProc("GdipFillEllipse")
	procGdipDrawEllipse             = gdiplus.NewProc("GdipDrawEllipse")
	procGdipDrawArc                 = gdiplus.NewProc("GdipDrawArc")
	procGdipFillRectangle           = gdiplus.NewProc("GdipFillRectangle")
	procGdipDrawLine                = gdiplus.NewProc("GdipDrawLine")
	procGdipSetClipPath             = gdiplus.NewProc("GdipSetClipPath")
	procGdipResetClip               = gdiplus.NewProc("GdipResetClip")
	procGdipCreateFontFamily        = gdiplus.NewProc("GdipCreateFontFamilyFromName")
	procGdipDeleteFontFamily        = gdiplus.NewProc("GdipDeleteFontFamily")
	procGdipCreateFont              = gdiplus.NewProc("GdipCreateFont")
	procGdipDeleteFont              = gdiplus.NewProc("GdipDeleteFont")
	procGdipCreateStringFormat      = gdiplus.NewProc("GdipCreateStringFormat")
	procGdipDeleteStringFormat      = gdiplus.NewProc("GdipDeleteStringFormat")
	procGdipSetStringFormatAlign    = gdiplus.NewProc("GdipSetStringFormatAlign")
	procGdipSetStringFormatLineAln  = gdiplus.NewProc("GdipSetStringFormatLineAlign")
	procGdipSetStringFormatTrimming = gdiplus.NewProc("GdipSetStringFormatTrimming")
	procGdipSetStringFormatFlags    = gdiplus.NewProc("GdipSetStringFormatFlags")
	procGdipDrawString              = gdiplus.NewProc("GdipDrawString")
	procBitBlt                      = gdi32.NewProc("BitBlt")
)

const (
	PixelFormat32bppPARGB = 0x000E200B

	FontStyleRegular = 0
	FontStyleBold    = 1

	AlignNear   = 0
	AlignCenter = 1
	AlignFar    = 2
)

type RectF struct {
	X, Y, W, H float32
}

type PointF struct {
	X, Y float32
}

type gdiplusStartupInput struct {
	GdiplusVersion           uint32
	DebugEventCallback       uintptr
	SuppressBackgroundThread int32
	SuppressExternalCodecs   int32
}

var (
	gdipToken  uintptr
	resMu      sync.Mutex
	uiFamily   uintptr
	fontCache  = map[uint64]uintptr{}
	fmtCache   = map[uint32]uintptr{}
)

func InitGDIPlus() {
	in := gdiplusStartupInput{GdiplusVersion: 1}
	_, _, _ = procGdiplusStartup.Call(uintptr(unsafe.Pointer(&gdipToken)), uintptr(unsafe.Pointer(&in)), 0)
	famName, _ := windows.UTF16PtrFromString("Segoe UI")
	_, _, _ = procGdipCreateFontFamily.Call(uintptr(unsafe.Pointer(famName)), 0, uintptr(unsafe.Pointer(&uiFamily)))
}

func ShutdownGDIPlus() {
	resMu.Lock()
	for _, f := range fontCache {
		_, _, _ = procGdipDeleteFont.Call(f)
	}
	fontCache = map[uint64]uintptr{}
	for _, fm := range fmtCache {
		_, _, _ = procGdipDeleteStringFormat.Call(fm)
	}
	fmtCache = map[uint32]uintptr{}
	if uiFamily != 0 {
		_, _, _ = procGdipDeleteFontFamily.Call(uiFamily)
		uiFamily = 0
	}
	resMu.Unlock()

	if gdipToken != 0 {
		_, _, _ = procGdiplusShutdown.Call(gdipToken)
		gdipToken = 0
	}
}

func getCachedFont(px float32, bold bool) uintptr {
	// Quantize to 0.5px to keep cache tiny and hit rate ~100%
	q := uint32(math.Round(float64(px * 2)))
	var b uint32
	if bold {
		b = 1
	}
	key := (uint64(q) << 1) | uint64(b)
	resMu.Lock()
	defer resMu.Unlock()
	if f, ok := fontCache[key]; ok {
		return f
	}
	if uiFamily == 0 {
		return 0
	}
	style := int32(FontStyleRegular)
	if bold {
		style = FontStyleBold
	}
	var f uintptr
	_, _, _ = procGdipCreateFont.Call(uiFamily, f32(float32(q)/2.0), uintptr(style), 2, uintptr(unsafe.Pointer(&f)))
	if f != 0 {
		fontCache[key] = f
	}
	return f
}

func getCachedFormat(align, valign int32, ellipsis bool) uintptr {
	var e uint32
	if ellipsis {
		e = 1
	}
	key := (uint32(align) << 8) | (uint32(valign) << 2) | e
	resMu.Lock()
	defer resMu.Unlock()
	if fm, ok := fmtCache[key]; ok {
		return fm
	}
	var fm uintptr
	_, _, _ = procGdipCreateStringFormat.Call(0, 0, uintptr(unsafe.Pointer(&fm)))
	if fm != 0 {
		_, _, _ = procGdipSetStringFormatAlign.Call(fm, uintptr(align))
		_, _, _ = procGdipSetStringFormatLineAln.Call(fm, uintptr(valign))
		if ellipsis {
			_, _, _ = procGdipSetStringFormatFlags.Call(fm, 0x1000) // NoWrap
			_, _, _ = procGdipSetStringFormatTrimming.Call(fm, 3)  // EllipsisCharacter
		}
		fmtCache[key] = fm
	}
	return fm
}

func f32(v float32) uintptr {
	return uintptr(math.Float32bits(v))
}

func ARGB(a, r, g, b uint8) uint32 {
	return (uint32(a) << 24) | (uint32(r) << 16) | (uint32(g) << 8) | uint32(b)
}

func RGBAlpha(rgb uint32, a uint8) uint32 {
	return (uint32(a) << 24) | (rgb & 0x00FFFFFF)
}

// LayeredCanvas is a reusable offscreen 32bpp PARGB surface backed by a GDI DIBSection + GDI+ Graphics.
type LayeredCanvas struct {
	Width   int32
	Height  int32
	MemDC   uintptr
	HBitmap uintptr
	OldObj  uintptr
	Bitmap  uintptr
	Gfx     uintptr
	brush   uintptr
	pen     uintptr
	penCap  bool
}

func NewLayeredCanvas(w, h int32) *LayeredCanvas {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	screenDC, _, _ := procGetDC.Call(0)
	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	_, _, _ = procReleaseDC.Call(0, screenDC)

	var bmi BITMAPINFO
	bmi.BmiHeader.BiSize = uint32(unsafe.Sizeof(bmi.BmiHeader))
	bmi.BmiHeader.BiWidth = w
	bmi.BmiHeader.BiHeight = -h // top-down
	bmi.BmiHeader.BiPlanes = 1
	bmi.BmiHeader.BiBitCount = 32
	bmi.BmiHeader.BiCompression = 0

	var bits *byte
	hbmp, _, _ := procCreateDIBSection.Call(memDC, uintptr(unsafe.Pointer(&bmi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	oldObj, _, _ := procSelectObject.Call(memDC, hbmp)

	var gdipBmp uintptr
	_, _, _ = procGdipCreateBitmapFromScan0.Call(
		uintptr(w), uintptr(h), uintptr(w*4), PixelFormat32bppPARGB,
		uintptr(unsafe.Pointer(bits)), uintptr(unsafe.Pointer(&gdipBmp)),
	)
	var gfx uintptr
	_, _, _ = procGdipGetImageGraphicsContext.Call(gdipBmp, uintptr(unsafe.Pointer(&gfx)))
	_, _, _ = procGdipSetSmoothingMode.Call(gfx, 4)     // AntiAlias
	_, _, _ = procGdipSetTextRenderingHint.Call(gfx, 4) // AntiAlias
	_, _, _ = procGdipSetPixelOffsetMode.Call(gfx, 4)   // Half

	var brush, pen uintptr
	_, _, _ = procGdipCreateSolidFill.Call(0xFFFFFFFF, uintptr(unsafe.Pointer(&brush)))
	_, _, _ = procGdipCreatePen1.Call(0xFFFFFFFF, f32(1), 2, uintptr(unsafe.Pointer(&pen)))

	return &LayeredCanvas{
		Width:   w,
		Height:  h,
		MemDC:   memDC,
		HBitmap: hbmp,
		OldObj:  oldObj,
		Bitmap:  gdipBmp,
		Gfx:     gfx,
		brush:   brush,
		pen:     pen,
	}
}

func (c *LayeredCanvas) Close() {
	if c == nil {
		return
	}
	if c.brush != 0 {
		_, _, _ = procGdipDeleteBrush.Call(c.brush)
	}
	if c.pen != 0 {
		_, _, _ = procGdipDeletePen.Call(c.pen)
	}
	if c.Gfx != 0 {
		_, _, _ = procGdipDeleteGraphics.Call(c.Gfx)
	}
	if c.Bitmap != 0 {
		_, _, _ = procGdipDisposeImage.Call(c.Bitmap)
	}
	if c.MemDC != 0 {
		_, _, _ = procSelectObject.Call(c.MemDC, c.OldObj)
		_, _, _ = procDeleteObject.Call(c.HBitmap)
		_, _, _ = procDeleteDC.Call(c.MemDC)
	}
}

func (c *LayeredCanvas) useBrush(argb uint32) uintptr {
	_, _, _ = procGdipSetSolidFillColor.Call(c.brush, uintptr(argb))
	return c.brush
}

func (c *LayeredCanvas) usePen(argb uint32, width float32, roundCap bool) uintptr {
	_, _, _ = procGdipSetPenColor.Call(c.pen, uintptr(argb))
	_, _, _ = procGdipSetPenWidth.Call(c.pen, f32(width))
	if roundCap != c.penCap {
		capVal := uintptr(0) // LineCapFlat
		joinVal := uintptr(0)
		if roundCap {
			capVal = 2 // LineCapRound
			joinVal = 2
		}
		_, _, _ = procGdipSetPenStartCap.Call(c.pen, capVal)
		_, _, _ = procGdipSetPenEndCap.Call(c.pen, capVal)
		_, _, _ = procGdipSetPenLineJoin.Call(c.pen, joinVal)
		c.penCap = roundCap
	}
	return c.pen
}

func (c *LayeredCanvas) Clear(argb uint32) {
	_, _, _ = procGdipGraphicsClear.Call(c.Gfx, uintptr(argb))
}

func (c *LayeredCanvas) Flush() {
	_, _, _ = procGdipFlush.Call(c.Gfx, 1)
}

func (c *LayeredCanvas) BlitToHDC(dstHDC uintptr, x, y int32) {
	c.Flush()
	_, _, _ = procBitBlt.Call(dstHDC, uintptr(x), uintptr(y), uintptr(c.Width), uintptr(c.Height), c.MemDC, 0, 0, 0x00CC0020) // SRCCOPY
}

func (c *LayeredCanvas) CommitToWindow(hwnd uintptr, x, y int32) {
	c.Flush()
	ptDst := POINT{X: x, Y: y}
	sz := SIZE{CX: c.Width, CY: c.Height}
	ptSrc := POINT{X: 0, Y: 0}
	blend := BLENDFUNCTION{
		BlendOp:             AC_SRC_OVER,
		BlendFlags:          0,
		SourceConstantAlpha: 255,
		AlphaFormat:         AC_SRC_ALPHA,
	}
	screenDC, _, _ := procGetDC.Call(0)
	_, _, _ = procUpdateLayeredWindow.Call(
		hwnd,
		screenDC,
		uintptr(unsafe.Pointer(&ptDst)),
		uintptr(unsafe.Pointer(&sz)),
		c.MemDC,
		uintptr(unsafe.Pointer(&ptSrc)),
		0,
		uintptr(unsafe.Pointer(&blend)),
		ULW_ALPHA,
	)
	_, _, _ = procReleaseDC.Call(0, screenDC)
}

func (c *LayeredCanvas) ToHICON() uintptr {
	c.Flush()
	var hicon uintptr
	_, _, _ = procGdipCreateHICONFromBitmap.Call(c.Bitmap, uintptr(unsafe.Pointer(&hicon)))
	return hicon
}

func (c *LayeredCanvas) FillEllipse(x, y, w, h float32, argb uint32) {
	_, _, _ = procGdipFillEllipse.Call(c.Gfx, c.useBrush(argb), f32(x), f32(y), f32(w), f32(h))
}

func (c *LayeredCanvas) DrawEllipse(x, y, w, h, strokeWidth float32, argb uint32) {
	_, _, _ = procGdipDrawEllipse.Call(c.Gfx, c.usePen(argb, strokeWidth, false), f32(x), f32(y), f32(w), f32(h))
}

func (c *LayeredCanvas) DrawArc(x, y, w, h, startDeg, sweepDeg, strokeWidth float32, argb uint32, roundCap bool) {
	if sweepDeg == 0 {
		return
	}
	_, _, _ = procGdipDrawArc.Call(c.Gfx, c.usePen(argb, strokeWidth, roundCap), f32(x), f32(y), f32(w), f32(h), f32(startDeg), f32(sweepDeg))
}

func (c *LayeredCanvas) FillRect(x, y, w, h float32, argb uint32) {
	_, _, _ = procGdipFillRectangle.Call(c.Gfx, c.useBrush(argb), f32(x), f32(y), f32(w), f32(h))
}

func (c *LayeredCanvas) DrawLine(x1, y1, x2, y2, strokeWidth float32, argb uint32) {
	_, _, _ = procGdipDrawLine.Call(c.Gfx, c.usePen(argb, strokeWidth, false), f32(x1), f32(y1), f32(x2), f32(y2))
}

func NewPath(winding bool) uintptr {
	mode := uintptr(0)
	if winding {
		mode = 1
	}
	var path uintptr
	_, _, _ = procGdipCreatePath.Call(mode, uintptr(unsafe.Pointer(&path)))
	return path
}

func PathStartFigure(path uintptr) {
	_, _, _ = procGdipStartPathFigure.Call(path)
}

func PathCloseFigure(path uintptr) {
	_, _, _ = procGdipClosePathFigure.Call(path)
}

func PathAddLine(path uintptr, x1, y1, x2, y2 float32) {
	_, _, _ = procGdipAddPathLine.Call(path, f32(x1), f32(y1), f32(x2), f32(y2))
}

func PathAddLines(path uintptr, pts []PointF) {
	if len(pts) < 2 {
		return
	}
	_, _, _ = procGdipAddPathLine2.Call(path, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
}

func PathAddBezier(path uintptr, x1, y1, x2, y2, x3, y3, x4, y4 float32) {
	_, _, _ = procGdipAddPathBezier.Call(path, f32(x1), f32(y1), f32(x2), f32(y2), f32(x3), f32(y3), f32(x4), f32(y4))
}

func PathAddArc(path uintptr, x, y, w, h, startAngle, sweepAngle float32) {
	_, _, _ = procGdipAddPathArc.Call(path, f32(x), f32(y), f32(w), f32(h), f32(startAngle), f32(sweepAngle))
}

func NewRoundRectPath(x, y, w, h, r float32) uintptr {
	path := NewPath(true)
	if path == 0 {
		return 0
	}
	if r*2 > w {
		r = w / 2
	}
	if r*2 > h {
		r = h / 2
	}
	if r <= 0.1 {
		PathAddLine(path, x, y, x+w, y)
		PathAddLine(path, x+w, y, x+w, y+h)
		PathAddLine(path, x+w, y+h, x, y+h)
		PathCloseFigure(path)
		return path
	}
	d := r * 2
	PathStartFigure(path)
	PathAddArc(path, x, y, d, d, 180, 90)
	PathAddArc(path, x+w-d, y, d, d, 270, 90)
	PathAddArc(path, x+w-d, y+h-d, d, d, 0, 90)
	PathAddArc(path, x, y+h-d, d, d, 90, 90)
	PathCloseFigure(path)
	return path
}

func (c *LayeredCanvas) FillRoundRect(x, y, w, h, r float32, argb uint32) {
	path := NewRoundRectPath(x, y, w, h, r)
	if path == 0 {
		return
	}
	c.FillPath(path, argb)
	DeletePath(path)
}

func (c *LayeredCanvas) DrawRoundRect(x, y, w, h, r, strokeWidth float32, argb uint32) {
	path := NewRoundRectPath(x, y, w, h, r)
	if path == 0 {
		return
	}
	c.DrawPath(path, strokeWidth, argb, false)
	DeletePath(path)
}

func (c *LayeredCanvas) FillPath(path uintptr, argb uint32) {
	_, _, _ = procGdipFillPath.Call(c.Gfx, c.useBrush(argb), path)
}

func (c *LayeredCanvas) DrawPath(path uintptr, strokeWidth float32, argb uint32, roundCap bool) {
	_, _, _ = procGdipDrawPath.Call(c.Gfx, c.usePen(argb, strokeWidth, roundCap), path)
}

func (c *LayeredCanvas) SetClipPath(path uintptr) {
	_, _, _ = procGdipSetClipPath.Call(c.Gfx, path, 0)
}

func (c *LayeredCanvas) ResetClip() {
	_, _, _ = procGdipResetClip.Call(c.Gfx)
}

func DeletePath(path uintptr) {
	if path != 0 {
		_, _, _ = procGdipDeletePath.Call(path)
	}
}

func (c *LayeredCanvas) DrawText(text string, x, y, w, h, fontSize float32, bold bool, align, valign int32, argb uint32, ellipsis bool) {
	if text == "" {
		return
	}
	u16, err := windows.UTF16FromString(text)
	if err != nil {
		return
	}
	font := getCachedFont(fontSize, bold)
	fm := getCachedFormat(align, valign, ellipsis)
	if font == 0 || fm == 0 {
		return
	}
	rc := RectF{X: x, Y: y, W: w, H: h}
	_, _, _ = procGdipDrawString.Call(
		c.Gfx,
		uintptr(unsafe.Pointer(&u16[0])),
		uintptr(len(u16)-1),
		font,
		uintptr(unsafe.Pointer(&rc)),
		fm,
		c.useBrush(argb),
	)
}
