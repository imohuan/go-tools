//go:build windows

package winui

import (
	"syscall"
	"time"
	"unsafe"
)

const (
	wmUserRefresh = 0x0400 + 1

	hwndTopmost = ^uintptr(0)
	swShowNorm  = 1
	asfwAny     = ^uintptr(0)

	spiGetWorkArea = 0x0030
	pmRemove       = 0x0001
	wmQuit         = 0x0012
)

var (
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procSystemParametersInfoW    = user32.NewProc("SystemParametersInfoW")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procIsWindow                 = user32.NewProc("IsWindow")
	procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
	procPeekMessageW             = user32.NewProc("PeekMessageW")
)

// HWND returns the main window handle.
func (c *Controller) HWND() syscall.Handle {
	return c.hwnd
}

// Ready returns a channel closed when the window handle is created.
func (c *Controller) Ready() <-chan struct{} {
	return c.ready
}

// PostToggle requests show/hide; handled on the UI thread via toggleCh.
func (c *Controller) PostToggle() {
	<-c.ready
	if c.hwnd == 0 || !isWindow(c.hwnd) {
		return
	}
	select {
	case c.toggleCh <- struct{}{}:
	default:
	}
}

func isWindow(hwnd syscall.Handle) bool {
	r, _, _ := procIsWindow.Call(uintptr(hwnd))
	return r != 0
}

func WorkAreaCenter() (int, int) {
	var area rect
	ok, _, _ := procSystemParametersInfoW.Call(
		spiGetWorkArea, 0,
		uintptr(unsafe.Pointer(&area)), 0,
	)
	if ok == 0 {
		cx, _, _ := procGetSystemMetrics.Call(0)
		cy, _, _ := procGetSystemMetrics.Call(1)
		return clampToRect(rect{0, 0, int32(cx), int32(cy)}, winW, winH)
	}
	return clampToRect(area, winW, winH)
}

func clampToRect(area rect, w, h int) (int, int) {
	aw := int(area.Right - area.Left)
	ah := int(area.Bottom - area.Top)
	x := int(area.Left) + (aw-w)/2
	y := int(area.Top) + (ah-h)/2
	maxX := int(area.Right) - w
	maxY := int(area.Bottom) - h
	if x < int(area.Left) {
		x = int(area.Left)
	}
	if y < int(area.Top) {
		y = int(area.Top)
	}
	if x > maxX {
		x = maxX
	}
	if y > maxY {
		y = maxY
	}
	return x, y
}

func (c *Controller) windowActuallyVisible() bool {
	if c.hwnd == 0 {
		return false
	}
	r, _, _ := procIsWindowVisible.Call(uintptr(c.hwnd))
	return r != 0
}

func (c *Controller) activateWindow(x, y int) {
	procAllowSetForegroundWindow.Call(asfwAny)
	procSetWindowPos.Call(
		uintptr(c.hwnd), hwndTopmost,
		uintptr(x), uintptr(y), uintptr(winW), uintptr(winH),
		0x0040,
	)
	procShowWindow.Call(uintptr(c.hwnd), swShowNorm)
	procBringWindowToTop.Call(uintptr(c.hwnd))
	procSetForegroundWindow.Call(uintptr(c.hwnd))
	procSetFocus.Call(uintptr(c.hwnd))
}

// messagePump runs on the UI thread; drains Win32 messages and toggleCh.
func (c *Controller) messagePump() {
	var m msg
	for {
		for {
			ret, _, _ := procPeekMessageW.Call(
				uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmRemove,
			)
			if ret == 0 {
				break
			}
			if m.Message == wmQuit {
				return
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}

		select {
		case <-c.toggleCh:
			c.toggleAt()
		default:
			// Block until toggle or next Win32 message (50ms poll).
			select {
			case <-c.toggleCh:
				c.toggleAt()
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
}
