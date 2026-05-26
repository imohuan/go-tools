//go:build windows

package winui

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"tray-view/internal/process"
)

const (
	winClass = "TrayViewPopup"
	winW     = 380
	winH     = 520
	rowH     = 36
	pad      = 10
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procInvalidateRect   = user32.NewProc("InvalidateRect")
	procBeginPaint       = user32.NewProc("BeginPaint")
	procEndPaint         = user32.NewProc("EndPaint")
	procFillRect         = user32.NewProc("FillRect")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procCreateFontW      = gdi32.NewProc("CreateFontW")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procDrawTextW        = user32.NewProc("DrawTextW")
	procSetTimer         = user32.NewProc("SetTimer")
	procKillTimer        = user32.NewProc("KillTimer")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

const (
	wsExToolWindow  = 0x00000080
	wsExTopmost     = 0x00000008
	wsPopup         = 0x80000000
	wsVisible       = 0x10000000
	swShow          = 5
	swHide          = 0
	swPShow         = 0x0040
	wmDestroy       = 0x0002
	wmPaint         = 0x000F
	wmTimer         = 0x0113
	wmClose         = 0x0010
	wmNCCreate      = 0x0081
	dtLeft          = 0x00000000
	dtVCenter       = 0x00000004
	dtSingleLine    = 0x00000020
	dtEndEllipsis   = 0x00008000
	transparent     = 1
	timerID         = 1
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   syscall.Handle
	Icon       syscall.Handle
	Cursor     syscall.Handle
	Background syscall.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     syscall.Handle
}

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type paintStruct struct {
	HDC         syscall.Handle
	Erase       int32
	RcPaint     rect
	Restore     int32
	IncUpdate   int32
	Reserved    [32]byte
}
type msg struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

// Controller is the native popup window.
type Controller struct {
	hwnd    syscall.Handle
	visible bool
	mu      sync.Mutex
	snap    *process.Snapshot
	hover   int
	font    syscall.Handle
	brushBG syscall.Handle
}

// New creates the UI controller.
func New() *Controller {
	return &Controller{hover: -1}
}

// Run starts the Win32 message loop (main thread).
func (c *Controller) Run() {
	hInst := getModuleHandle()
	className, _ := windows.UTF16PtrFromString(winClass)

	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   syscall.NewCallback(c.wndProc),
		Instance:  hInst,
		ClassName: className,
	}
	cursor, _, _ := procLoadCursorW.Call(0, 32512) // IDC_ARROW
	wc.Cursor = syscall.Handle(cursor)

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	title, _ := windows.UTF16PtrFromString("进程查看")
	c.hwnd = syscall.Handle(createWindow(
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		uintptr(wsPopup),
		80, 80, winW, winH,
		0, 0, uintptr(hInst), 0,
	))

	c.font = createFont(15)
	c.brushBG = solidBrush(0x2b2b2b)
	c.refresh()
	procShowWindow.Call(uintptr(c.hwnd), swHide)
	procSetTimer.Call(uintptr(c.hwnd), timerID, 2000, 0)

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if ret == 0 || ret == ^uintptr(0) {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (c *Controller) wndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmClose:
		c.hide()
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	case wmTimer:
		if wParam == timerID && c.visible {
			c.refresh()
			procInvalidateRect.Call(uintptr(hwnd), 0, 1)
		}
		return 0
	case wmPaint:
		c.onPaint(hwnd)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

func (c *Controller) onPaint(hwnd syscall.Handle) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

	rc := rect{0, 0, winW, winH}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), uintptr(c.brushBG))

	c.mu.Lock()
	snap := c.snap
	hover := c.hover
	c.mu.Unlock()

	if snap == nil {
		drawText(hdc, c.font, 0xffffff, pad, pad, winW-pad*2, rowH, "正在加载…")
		return
	}

	y := pad
	idx := 0
	y = c.drawRow(hdc, y, idx, "内存临时垃圾", snap.TrashMB, 0, true, hover == idx)
	idx++
	if len(snap.Apps) > 0 {
		a := snap.Apps[0]
		y = c.drawRow(hdc, y, idx, a.Display, a.MemMB, a.CPUPercent, false, hover == idx)
		idx++
	}
	y = c.drawRow(hdc, y, idx, snap.System.Display, snap.System.MemMB, snap.System.CPUPercent, true, hover == idx)
	idx++
	start := 1
	if len(snap.Apps) <= 1 {
		start = len(snap.Apps)
	}
	for i := start; i < len(snap.Apps); i++ {
		a := snap.Apps[i]
		y = c.drawRow(hdc, y, idx, a.Display, a.MemMB, a.CPUPercent, false, hover == idx)
		idx++
		if y > winH-rowH {
			break
		}
	}
}

func (c *Controller) drawRow(hdc uintptr, y, index int, label string, mem, cpu float64, systemBar, hover bool) int {
	if hover {
		r := rect{pad, int32(y), winW - pad, int32(y + rowH - 2)}
		brush := solidBrush(0x3d3d3d)
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), uintptr(brush))
		procDeleteObject.Call(uintptr(brush))
	}

	x := pad + 4
	// checkbox
	box := rect{int32(x), int32(y + 10), int32(x + 14), int32(y + 24)}
	chkBrush := solidBrush(0x4a9eff)
	if index != 0 {
		chkBrush = solidBrush(0x555555)
	}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&box)), uintptr(chkBrush))
	procDeleteObject.Call(uintptr(chkBrush))

	x += 22
	// icon block
	icon := rect{int32(x), int32(y + 7), int32(x + 22), int32(y + 29)}
	iconBrush := solidBrush(iconColor(label))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&icon)), uintptr(iconBrush))
	procDeleteObject.Call(uintptr(iconBrush))

	x += 30
	drawText(hdc, c.font, 0xffffff, x, y+8, 160, rowH, label)

	if index == 1 {
		metrics := fmt.Sprintf("内存 %.1fMB CPU %.2f%%", mem, cpu)
		drawText(hdc, c.font, 0xe57373, winW-200, y+8, 190, rowH, metrics)
	} else {
		drawBar(hdc, winW-130, y+14, 110, 8, mem, cpu, systemBar)
	}
	return y + rowH
}

func drawBar(hdc uintptr, x, y, w, h int, mem, cpu float64, system bool) {
	track := solidBrush(0x454545)
	r := rect{int32(x), int32(y), int32(x + w), int32(y + h)}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), uintptr(track))
	procDeleteObject.Call(uintptr(track))

	usage := mem / 2048
	if cpu > 0 {
		usage = usage*0.6 + (cpu/100)*0.4
	}
	if system {
		usage = 0.82
	}
	if usage < 0.05 {
		usage = 0.05
	}
	if usage > 1 {
		usage = 1
	}
	fw := int(float64(w) * usage)
	if fw < 2 {
		fw = 2
	}
	fill := rect{int32(x), int32(y), int32(x + fw), int32(y + h)}
	if system {
		seg := fw / 3
		if seg < 1 {
			seg = 1
		}
		b1 := solidBrush(0x66bb6a)
		r1 := rect{int32(x), int32(y), int32(x + seg), int32(y + h)}
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r1)), uintptr(b1))
		procDeleteObject.Call(uintptr(b1))
		b2 := solidBrush(0xffca28)
		r2 := rect{int32(x + seg), int32(y), int32(x + seg*2), int32(y + h)}
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r2)), uintptr(b2))
		procDeleteObject.Call(uintptr(b2))
		b3 := solidBrush(0xef5350)
		r3 := rect{int32(x + seg*2), int32(y), int32(x + fw), int32(y + h)}
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r3)), uintptr(b3))
		procDeleteObject.Call(uintptr(b3))
	} else {
		gb := solidBrush(0x66bb6a)
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&fill)), uintptr(gb))
		procDeleteObject.Call(uintptr(gb))
	}
}

func drawText(hdc uintptr, font syscall.Handle, color uint32, x, y, w, h int, text string) {
	procSelectObject.Call(hdc, uintptr(font))
	procSetBkMode.Call(hdc, transparent)
	procSetTextColor.Call(hdc, uintptr(color))
	r := rect{int32(x), int32(y), int32(x + w), int32(y + h)}
	p, _ := windows.UTF16PtrFromString(text)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(p)), ^uintptr(0),
		uintptr(unsafe.Pointer(&r)), dtLeft|dtVCenter|dtSingleLine|dtEndEllipsis)
}

func iconColor(label string) uint32 {
	h := uint32(0)
	for _, ch := range label {
		h = h*31 + uint32(ch)
	}
	palette := []uint32{0x5c6bc0, 0x43a047, 0xe53935, 0xfb8c00, 0x8e24aa, 0x1e88e5}
	return palette[h%uint32(len(palette))]
}

func (c *Controller) refresh() {
	s, err := process.Collect()
	if err != nil {
		return
	}
	c.mu.Lock()
	c.snap = s
	c.mu.Unlock()
}

// Show shows the popup near (x,y).
func (c *Controller) Show(x, y int) {
	if c.hwnd == 0 {
		return
	}
	c.visible = true
	c.refresh()
	procSetWindowPos.Call(uintptr(c.hwnd), 0, uintptr(x), uintptr(y), winW, winH, swPShow)
	procShowWindow.Call(uintptr(c.hwnd), swShow)
	procUpdateWindow.Call(uintptr(c.hwnd))
	procInvalidateRect.Call(uintptr(c.hwnd), 0, 1)
}

func (c *Controller) hide() {
	c.visible = false
	procShowWindow.Call(uintptr(c.hwnd), swHide)
}

// Hide hides the window.
func (c *Controller) Hide() { c.hide() }

// IsVisible reports visibility.
func (c *Controller) IsVisible() bool { return c.visible }

// Toggle toggles visibility.
func (c *Controller) Toggle(x, y int) {
	if c.IsVisible() {
		c.Hide()
	} else {
		c.Show(x, y)
	}
}

// WinW is popup width.
const WinW = winW

// WinH is popup height.
const WinH = winH

func getModuleHandle() syscall.Handle {
	h, _, _ := procGetModuleHandleW.Call(0)
	return syscall.Handle(h)
}

func createWindow(class, title, style uintptr, x, y, w, h int, parent, menu, inst, param uintptr) uintptr {
	r, _, _ := procCreateWindowExW.Call(
		wsExToolWindow|wsExTopmost,
		class, title, style,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, menu, inst, param,
	)
	return r
}

func solidBrush(rgb uint32) syscall.Handle {
	r := byte(rgb & 0xff)
	g := byte((rgb >> 8) & 0xff)
	b := byte((rgb >> 16) & 0xff)
	c := uint32(b)<<16 | uint32(g)<<8 | uint32(r)
	h, _, _ := procCreateSolidBrush.Call(uintptr(c))
	return syscall.Handle(h)
}

func createFont(height int) syscall.Handle {
	h, _, _ := procCreateFontW.Call(
		uintptr(height), 0, 0, 0, 400, 0, 0, 0,
		1, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Microsoft YaHei UI"))),
	)
	return syscall.Handle(h)
}
