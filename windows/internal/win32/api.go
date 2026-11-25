package win32

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW        = user32.NewProc("RegisterClassExW")
	procCreateWindowExW         = user32.NewProc("CreateWindowExW")
	procDestroyWindow           = user32.NewProc("DestroyWindow")
	procShowWindow              = user32.NewProc("ShowWindow")
	procUpdateWindow            = user32.NewProc("UpdateWindow")
	procSetWindowPos            = user32.NewProc("SetWindowPos")
	procMoveWindow              = user32.NewProc("MoveWindow")
	procGetWindowRect           = user32.NewProc("GetWindowRect")
	procGetClientRect           = user32.NewProc("GetClientRect")
	procSetWindowTextW          = user32.NewProc("SetWindowTextW")
	procGetWindowTextW          = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW    = user32.NewProc("GetWindowTextLengthW")
	procDefWindowProcW          = user32.NewProc("DefWindowProcW")
	procGetMessageW             = user32.NewProc("GetMessageW")
	procTranslateMessage        = user32.NewProc("TranslateMessage")
	procDispatchMessageW        = user32.NewProc("DispatchMessageW")
	procPostMessageW            = user32.NewProc("PostMessageW")
	procSendMessageW            = user32.NewProc("SendMessageW")
	procPostQuitMessage         = user32.NewProc("PostQuitMessage")
	procSetTimer                = user32.NewProc("SetTimer")
	procKillTimer               = user32.NewProc("KillTimer")
	procGetDC                   = user32.NewProc("GetDC")
	procReleaseDC               = user32.NewProc("ReleaseDC")
	procBeginPaint              = user32.NewProc("BeginPaint")
	procEndPaint                = user32.NewProc("EndPaint")
	procInvalidateRect          = user32.NewProc("InvalidateRect")
	procUpdateLayeredWindow     = user32.NewProc("UpdateLayeredWindow")
	procGetCursorPos            = user32.NewProc("GetCursorPos")
	procScreenToClient          = user32.NewProc("ScreenToClient")
	procSetCapture              = user32.NewProc("SetCapture")
	procReleaseCapture          = user32.NewProc("ReleaseCapture")
	procLoadCursorW             = user32.NewProc("LoadCursorW")
	procLoadImageW              = user32.NewProc("LoadImageW")
	procSetCursor               = user32.NewProc("SetCursor")
	procEnumDisplayMonitors     = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW         = user32.NewProc("GetMonitorInfoW")
	procMonitorFromPoint        = user32.NewProc("MonitorFromPoint")
	procGetDpiForWindow         = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem         = user32.NewProc("GetDpiForSystem")
	procSetProcessDpiAwareness  = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetWindowDisplayAffin   = user32.NewProc("SetWindowDisplayAffinity")
	procSetForegroundWindow     = user32.NewProc("SetForegroundWindow")
	procCreatePopupMenu         = user32.NewProc("CreatePopupMenu")
	procAppendMenuW             = user32.NewProc("AppendMenuW")
	procTrackPopupMenu          = user32.NewProc("TrackPopupMenu")
	procDestroyMenu             = user32.NewProc("DestroyMenu")
	procRegisterHotKey          = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey        = user32.NewProc("UnregisterHotKey")
	procDestroyIcon             = user32.NewProc("DestroyIcon")
	procIsWindowVisible         = user32.NewProc("IsWindowVisible")
	procEnableWindow            = user32.NewProc("EnableWindow")
	procSetFocus                = user32.NewProc("SetFocus")
	procOpenClipboard           = user32.NewProc("OpenClipboard")
	procCloseClipboard          = user32.NewProc("CloseClipboard")
	procEmptyClipboard          = user32.NewProc("EmptyClipboard")
	procSetClipboardData        = user32.NewProc("SetClipboardData")
	procGetSysColorBrush        = user32.NewProc("GetSysColorBrush")
	procFillRect                = user32.NewProc("FillRect")
	procGetScrollInfo           = user32.NewProc("GetScrollInfo")
	procSetScrollInfo           = user32.NewProc("SetScrollInfo")
	procScrollWindowEx          = user32.NewProc("ScrollWindowEx")

	procCreateCompatibleDC      = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC                = gdi32.NewProc("DeleteDC")
	procCreateDIBSection        = gdi32.NewProc("CreateDIBSection")
	procSelectObject            = gdi32.NewProc("SelectObject")
	procDeleteObject            = gdi32.NewProc("DeleteObject")
	procCreateFontW             = gdi32.NewProc("CreateFontW")
	procCreateSolidBrush        = gdi32.NewProc("CreateSolidBrush")
	procSetTextColor            = gdi32.NewProc("SetTextColor")
	procSetBkColor              = gdi32.NewProc("SetBkColor")
	procSetBkMode               = gdi32.NewProc("SetBkMode")

	procShellNotifyIconW        = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW           = shell32.NewProc("ShellExecuteW")
	procDwmSetWindowAttribute   = dwmapi.NewProc("DwmSetWindowAttribute")
	procGetModuleHandleW        = kernel32.NewProc("GetModuleHandleW")
	procGlobalAlloc             = kernel32.NewProc("GlobalAlloc")
	procGlobalLock              = kernel32.NewProc("GlobalLock")
	procGlobalUnlock            = kernel32.NewProc("GlobalUnlock")
	procRtlMoveMemory           = kernel32.NewProc("RtlMoveMemory")
)

const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_POPUP            = 0x80000000
	WS_CHILD            = 0x40000000
	WS_VISIBLE          = 0x10000000
	WS_CLIPCHILDREN     = 0x02000000
	WS_CLIPSIBLINGS     = 0x04000000
	WS_VSCROLL          = 0x00200000
	WS_TABSTOP          = 0x00010000
	WS_BORDER           = 0x00800000

	ES_AUTOHSCROLL = 0x0080
	ES_PASSWORD    = 0x0020
	ES_MULTILINE   = 0x0004
	ES_READONLY    = 0x0800

	BS_PUSHBUTTON   = 0x00000000
	BS_DEFPUSHBUTTON = 0x00000001
	BS_AUTOCHECKBOX = 0x00000003
	CBS_DROPDOWNLIST = 0x0003

	WS_EX_TOPMOST       = 0x00000008
	WS_EX_TOOLWINDOW    = 0x00000080
	WS_EX_CLIENTEDGE    = 0x00000200
	WS_EX_LAYERED       = 0x00080000
	WS_EX_NOACTIVATE    = 0x08000000

	SW_HIDE          = 0
	SW_SHOW          = 5
	SW_SHOWNOACTIVATE = 4
	SW_RESTORE       = 9

	HWND_TOPMOST   = ^uintptr(0) // -1
	SWP_NOSIZE     = 0x0001
	SWP_NOMOVE     = 0x0002
	SWP_NOZORDER   = 0x0004
	SWP_NOACTIVATE = 0x0010
	SWP_SHOWWINDOW = 0x0040

	WM_CREATE       = 0x0001
	WM_DESTROY      = 0x0002
	WM_MOVE         = 0x0003
	WM_SIZE         = 0x0005
	WM_ACTIVATE     = 0x0006
	WM_SETFONT      = 0x0030
	WM_CLOSE        = 0x0010
	WM_PAINT        = 0x000F
	WM_ERASEBKGND   = 0x0014
	WM_CTLCOLOREDIT = 0x0133
	WM_CTLCOLORLISTBOX = 0x0134
	WM_CTLCOLORBTN  = 0x0135
	WM_CTLCOLORSTATIC = 0x0138
	WM_COMMAND      = 0x0111
	WM_VSCROLL      = 0x0115
	WM_TIMER        = 0x0113
	WM_MOUSEMOVE    = 0x0200
	WM_LBUTTONDOWN  = 0x0201
	WM_LBUTTONUP    = 0x0202
	WM_RBUTTONDOWN  = 0x0204
	WM_RBUTTONUP    = 0x0205
	WM_MOUSEWHEEL   = 0x020A
	WM_HOTKEY       = 0x0312
	WM_DPICHANGED   = 0x02E0
	WM_CONTEXTMENU  = 0x007B
	WM_KEYDOWN      = 0x0100
	WM_APP          = 0x8000

	BM_GETCHECK = 0x00F0
	BM_SETCHECK = 0x00F1
	BST_UNCHECKED = 0
	BST_CHECKED   = 1

	CB_ADDSTRING  = 0x0143
	CB_GETCURSEL  = 0x0147
	CB_SETCURSEL  = 0x014E
	CBN_SELCHANGE = 1
	EN_CHANGE     = 0x0300

	IDC_ARROW = 32512
	IDC_HAND  = 32649

	MF_STRING    = 0x0000
	MF_SEPARATOR = 0x0800
	MF_CHECKED   = 0x0008
	MF_GRAYED    = 0x0001
	TPM_RIGHTALIGN  = 0x0008
	TPM_BOTTOMALIGN = 0x0020
	TPM_RETURNCMD   = 0x0100

	ULW_ALPHA  = 0x00000002
	AC_SRC_OVER  = 0x00
	AC_SRC_ALPHA = 0x01

	WDA_NONE               = 0x00000000
	WDA_EXCLUDEFROMCAPTURE = 0x00000011

	DWMWA_USE_IMMUTABLE_DARK_MODE = 20

	NIM_ADD        = 0x00000000
	NIM_MODIFY     = 0x00000001
	NIM_DELETE     = 0x00000002
	NIM_SETVERSION = 0x00000004
	NIF_MESSAGE    = 0x00000001
	NIF_ICON       = 0x00000002
	NIF_TIP        = 0x00000004
	NIF_INFO       = 0x00000010
	NIIF_INFO      = 0x00000001
	NOTIFYICON_VERSION_4 = 4

	NIN_SELECT     = WM_USER + 0
	NIN_KEYSELECT  = WM_USER + 1
	WM_USER        = 0x0400

	MOD_ALT     = 0x0001
	MOD_CONTROL = 0x0002
	MOD_SHIFT   = 0x0004
	MOD_WIN     = 0x0008
	MOD_NOREPEAT = 0x4000

	MONITOR_DEFAULTTONEAREST = 2
)

type POINT struct {
	X, Y int32
}

type SIZE struct {
	CX, CY int32
}

type RECT struct {
	Left, Top, Right, Bottom int32
}

func (r RECT) Width() int32  { return r.Right - r.Left }
func (r RECT) Height() int32 { return r.Bottom - r.Top }

type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type PAINTSTRUCT struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type BLENDFUNCTION struct {
	BlendOp             byte
	BlendFlags          byte
	SourceConstantAlpha byte
	AlphaFormat         byte
}

type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

type BITMAPINFO struct {
	BmiHeader BITMAPINFOHEADER
	BmiColors [1]uint32
}

type MONITORINFO struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type SCROLLINFO struct {
	CbSize    uint32
	FMask     uint32
	NMin      int32
	NMax      int32
	NPage     uint32
	NPos      int32
	NTrackPos int32
}

type MonitorBounds struct {
	Index   int
	Full    RECT
	Work    RECT
	Primary bool
}

func InitDPIAwareness() {
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
	_, _, _ = procSetProcessDpiAwareness.Call(^uintptr(3))
}

func ModuleHandle() uintptr {
	r, _, _ := procGetModuleHandleW.Call(0)
	return r
}

// LoadAppIcon loads the embedded PE icon resource (ID 1) at the requested pixel size.
func LoadAppIcon(size int32) uintptr {
	const (
		imageIcon      = 1
		lrDefaultColor = 0
	)
	r, _, _ := procLoadImageW.Call(ModuleHandle(), uintptr(1), imageIcon, uintptr(size), uintptr(size), lrDefaultColor)
	return r
}

func RegisterClass(className string, wndProc uintptr, bgBrush uintptr) error {
	cname, _ := windows.UTF16PtrFromString(className)
	cursor, _, _ := procLoadCursorW.Call(0, IDC_ARROW)
	hIconLg := LoadAppIcon(32)
	hIconSm := LoadAppIcon(16)
	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		Style:         0x0003, // CS_HREDRAW | CS_VREDRAW
		LpfnWndProc:   wndProc,
		HInstance:     ModuleHandle(),
		HIcon:         hIconLg,
		HIconSm:       hIconSm,
		HCursor:       cursor,
		HbrBackground: bgBrush,
		LpszClassName: cname,
	}
	r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return err
	}
	return nil
}

func CreateWindowEx(exStyle uint32, className, title string, style uint32, x, y, w, h int32, parent, menu uintptr) uintptr {
	cname, _ := windows.UTF16PtrFromString(className)
	var tptr *uint16
	if title != "" {
		tptr, _ = windows.UTF16PtrFromString(title)
	}
	hwnd, _, _ := procCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(cname)),
		uintptr(unsafe.Pointer(tptr)),
		uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, menu, ModuleHandle(), 0,
	)
	return hwnd
}

func DestroyWindow(hwnd uintptr) {
	_, _, _ = procDestroyWindow.Call(hwnd)
}

func ShowWindow(hwnd uintptr, cmd int32) {
	_, _, _ = procShowWindow.Call(hwnd, uintptr(cmd))
}

func IsWindowVisible(hwnd uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}

func SetWindowPos(hwnd, insertAfter uintptr, x, y, cx, cy int32, flags uint32) {
	_, _, _ = procSetWindowPos.Call(hwnd, insertAfter, uintptr(x), uintptr(y), uintptr(cx), uintptr(cy), uintptr(flags))
}

func MoveWindow(hwnd uintptr, x, y, w, h int32, repaint bool) {
	var r uintptr
	if repaint {
		r = 1
	}
	_, _, _ = procMoveWindow.Call(hwnd, uintptr(x), uintptr(y), uintptr(w), uintptr(h), r)
}

func GetWindowRect(hwnd uintptr) RECT {
	var r RECT
	_, _, _ = procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func GetClientRect(hwnd uintptr) RECT {
	var r RECT
	_, _, _ = procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func SetWindowText(hwnd uintptr, text string) {
	ptr, _ := windows.UTF16PtrFromString(text)
	_, _, _ = procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(ptr)))
}

func GetWindowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	_, _, _ = procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return windows.UTF16ToString(buf)
}

func DefWindowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func PostMessage(hwnd uintptr, msg uint32, wParam, lParam uintptr) {
	_, _, _ = procPostMessageW.Call(hwnd, uintptr(msg), wParam, lParam)
}

func SendMessage(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	r, _, _ := procSendMessageW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func PostQuitMessage(code int32) {
	_, _, _ = procPostQuitMessage.Call(uintptr(code))
}

func SetTimer(hwnd uintptr, id uintptr, ms uint32) {
	_, _, _ = procSetTimer.Call(hwnd, id, uintptr(ms), 0)
}

func KillTimer(hwnd uintptr, id uintptr) {
	_, _, _ = procKillTimer.Call(hwnd, id)
}

func InvalidateRect(hwnd uintptr, erase bool) {
	var e uintptr
	if erase {
		e = 1
	}
	_, _, _ = procInvalidateRect.Call(hwnd, 0, e)
}

func GetCursorPos() (int32, int32) {
	var pt POINT
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return pt.X, pt.Y
}

func SetCapture(hwnd uintptr) {
	_, _, _ = procSetCapture.Call(hwnd)
}

func ReleaseCapture() {
	_, _, _ = procReleaseCapture.Call()
}

func SetForegroundWindow(hwnd uintptr) {
	_, _, _ = procSetForegroundWindow.Call(hwnd)
}

func SetExcludeFromCapture(hwnd uintptr, exclude bool) {
	aff := uintptr(WDA_NONE)
	if exclude {
		aff = WDA_EXCLUDEFROMCAPTURE
	}
	_, _, _ = procSetWindowDisplayAffin.Call(hwnd, aff)
}

func SetDarkTitleBar(hwnd uintptr, dark bool) {
	var v int32
	if dark {
		v = 1
	}
	_, _, _ = procDwmSetWindowAttribute.Call(hwnd, DWMWA_USE_IMMUTABLE_DARK_MODE, uintptr(unsafe.Pointer(&v)), 4)
}

func GetDpiForWindow(hwnd uintptr) float64 {
	if hwnd != 0 {
		if r, _, _ := procGetDpiForWindow.Call(hwnd); r >= 96 {
			return float64(r) / 96.0
		}
	}
	if r, _, _ := procGetDpiForSystem.Call(); r >= 96 {
		return float64(r) / 96.0
	}
	return 1.0
}

func EnumMonitors() []MonitorBounds {
	var list []MonitorBounds
	cb := syscall.NewCallback(func(hMon uintptr, hdc uintptr, lprc uintptr, dwData uintptr) uintptr {
		var mi MONITORINFO
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		if r, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi))); r != 0 {
			list = append(list, MonitorBounds{
				Index:   len(list),
				Full:    mi.RcMonitor,
				Work:    mi.RcWork,
				Primary: (mi.DwFlags & 1) != 0,
			})
		}
		return 1
	})
	_, _, _ = procEnumDisplayMonitors.Call(0, 0, cb, 0)
	return list
}

func MonitorAtPoint(x, y int32) MonitorBounds {
	pt := uintptr(uint32(x)) | (uintptr(uint32(y)) << 32)
	hMon, _, _ := procMonitorFromPoint.Call(pt, MONITOR_DEFAULTTONEAREST)
	var mi MONITORINFO
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if hMon != 0 {
		if r, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi))); r != 0 {
			return MonitorBounds{Full: mi.RcMonitor, Work: mi.RcWork, Primary: (mi.DwFlags & 1) != 0}
		}
	}
	mons := EnumMonitors()
	if len(mons) > 0 {
		return mons[0]
	}
	return MonitorBounds{
		Full:    RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		Work:    RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1040},
		Primary: true,
	}
}

func OpenURL(url string) {
	verb, _ := windows.UTF16PtrFromString("open")
	target, _ := windows.UTF16PtrFromString(url)
	_, _, _ = procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, SW_SHOW)
}

func CopyText(text string) {
	u16, err := windows.UTF16FromString(text)
	if err != nil {
		return
	}
	r, _, _ := procOpenClipboard.Call(0)
	if r == 0 {
		return
	}
	defer procCloseClipboard.Call()
	_, _, _ = procEmptyClipboard.Call()
	sz := uintptr(len(u16) * 2)
	hMem, _, _ := procGlobalAlloc.Call(0x0002, sz) // GMEM_MOVEABLE
	if hMem == 0 {
		return
	}
	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr != 0 {
		_, _, _ = procRtlMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&u16[0])), sz)
		_, _, _ = procGlobalUnlock.Call(hMem)
	}
	_, _, _ = procSetClipboardData.Call(13, hMem) // CF_UNICODETEXT
}

func CreateUIFont(height int32, weight int32) uintptr {
	face, _ := windows.UTF16PtrFromString("Segoe UI")
	h, _, _ := procCreateFontW.Call(
		uintptr(height), 0, 0, 0, uintptr(weight),
		0, 0, 0, 1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(face)),
	)
	return h
}

func MessageLoop() {
	var msg MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func CreatePopupMenu() uintptr {
	r, _, _ := procCreatePopupMenu.Call()
	return r
}

func AppendMenu(hMenu uintptr, flags uint32, id uintptr, text string) {
	var ptr *uint16
	if text != "" {
		ptr, _ = windows.UTF16PtrFromString(text)
	}
	_, _, _ = procAppendMenuW.Call(hMenu, uintptr(flags), id, uintptr(unsafe.Pointer(ptr)))
}

func TrackPopupMenu(hMenu uintptr, flags uint32, x, y int32, hwnd uintptr) uint32 {
	r, _, _ := procTrackPopupMenu.Call(hMenu, uintptr(flags), uintptr(x), uintptr(y), 0, hwnd, 0)
	return uint32(r)
}

func DestroyMenu(hMenu uintptr) {
	_, _, _ = procDestroyMenu.Call(hMenu)
}

func ShellNotifyIcon(msg uint32, nid *NOTIFYICONDATAW) bool {
	r, _, _ := procShellNotifyIconW.Call(uintptr(msg), uintptr(unsafe.Pointer(nid)))
	return r != 0
}

func DestroyIcon(hIcon uintptr) {
	if hIcon != 0 {
		_, _, _ = procDestroyIcon.Call(hIcon)
	}
}

func RegisterHotKey(hwnd uintptr, id int32, mods, vk uint32) bool {
	r, _, _ := procRegisterHotKey.Call(hwnd, uintptr(id), uintptr(mods), uintptr(vk))
	return r != 0
}

func UnregisterHotKey(hwnd uintptr, id int32) {
	_, _, _ = procUnregisterHotKey.Call(hwnd, uintptr(id))
}

func BeginPaint(hwnd uintptr, ps *PAINTSTRUCT) uintptr {
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(ps)))
	return hdc
}

func EndPaint(hwnd uintptr, ps *PAINTSTRUCT) {
	_, _, _ = procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(ps)))
}

func EnableWindow(hwnd uintptr, enable bool) {
	var e uintptr
	if enable {
		e = 1
	}
	_, _, _ = procEnableWindow.Call(hwnd, e)
}

func SetHandCursor() {
	c, _, _ := procLoadCursorW.Call(0, IDC_HAND)
	_, _, _ = procSetCursor.Call(c)
}

func SetArrowCursor() {
	c, _, _ := procLoadCursorW.Call(0, IDC_ARROW)
	_, _, _ = procSetCursor.Call(c)
}

func CreateSolidBrush(rgb uint32) uintptr {
	r, _, _ := procCreateSolidBrush.Call(uintptr(rgb))
	return r
}

func DeleteObject(obj uintptr) {
	if obj != 0 {
		_, _, _ = procDeleteObject.Call(obj)
	}
}

func SetTextColor(hdc uintptr, rgb uint32) {
	_, _, _ = procSetTextColor.Call(hdc, uintptr(rgb))
}

func SetBkColor(hdc uintptr, rgb uint32) {
	_, _, _ = procSetBkColor.Call(hdc, uintptr(rgb))
}

func SetBkMode(hdc uintptr, mode int32) {
	_, _, _ = procSetBkMode.Call(hdc, uintptr(mode))
}

func FillRect(hdc uintptr, rc *RECT, brush uintptr) {
	_, _, _ = procFillRect.Call(hdc, uintptr(unsafe.Pointer(rc)), brush)
}

func SetScrollInfo(hwnd uintptr, bar int32, si *SCROLLINFO, redraw bool) int32 {
	var r uintptr
	if redraw {
		r = 1
	}
	res, _, _ := procSetScrollInfo.Call(hwnd, uintptr(bar), uintptr(unsafe.Pointer(si)), r)
	return int32(res)
}

func GetScrollInfo(hwnd uintptr, bar int32, si *SCROLLINFO) bool {
	res, _, _ := procGetScrollInfo.Call(hwnd, uintptr(bar), uintptr(unsafe.Pointer(si)))
	return res != 0
}

func ScrollWindowEx(hwnd uintptr, dx, dy int32) {
	_, _, _ = procScrollWindowEx.Call(hwnd, uintptr(dx), uintptr(dy), 0, 0, 0, 0, 0x0007) // SW_SCROLLCHILDREN|SW_INVALIDATE|SW_ERASE
}
