//go:build windows

package winui

import (
	"syscall"
	"unsafe"
)

const (
	wmEraseBkgnd = 0x0014
	srccopy      = 0x00CC0020
)

// wmUserRefresh is defined in message.go

var (
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
)

// withDoubleBuffer draws to an off-screen bitmap then copies once (reduces flicker).
func withDoubleBuffer(hdc uintptr, draw func(memDC uintptr)) {
	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		draw(hdc)
		return
	}
	defer procDeleteDC.Call(memDC)

	bmp, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(winW), uintptr(winH))
	if bmp == 0 {
		draw(hdc)
		return
	}
	defer procDeleteObject.Call(bmp)

	old, _, _ := procSelectObject.Call(memDC, bmp)
	draw(memDC)
	procSelectObject.Call(memDC, old)
	procBitBlt.Call(hdc, 0, 0, uintptr(winW), uintptr(winH), memDC, 0, 0, srccopy)
}

func invalidateRect(hwnd syscall.Handle, r rect) {
	procInvalidateRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)), 0)
}

func invalidateFull(hwnd syscall.Handle) {
	procInvalidateRect.Call(uintptr(hwnd), 0, 0)
}

func (c *Controller) listContentRect() rect {
	return rect{
		Left:   listLeft - 2,
		Top:    int32(listTop - 2),
		Right:  winW,
		Bottom: int32(winH - listPad),
	}
}

func (c *Controller) scrollBarRect() rect {
	return c.scrollRect()
}

func (c *Controller) titleBarRect() rect {
	return rect{0, 0, winW, titleBarH}
}

func (c *Controller) headerRect() rect {
	return rect{0, int32(titleBarH), winW, int32(listTop)}
}

func (c *Controller) rowRect(displayIndex, scroll int) rect {
	y := listTop + displayIndex*rowH - scroll
	return rect{
		Left:   listLeft - 2,
		Top:    int32(y),
		Right:  listRight + 2,
		Bottom: int32(y + rowH),
	}
}

func (c *Controller) invalidateRow(hwnd syscall.Handle, displayIndex, scroll int) {
	if displayIndex < 0 {
		return
	}
	r := c.rowRect(displayIndex, scroll)
	// clip to visible list
	if r.Bottom < listTop || r.Top > winH-listPad {
		return
	}
	invalidateRect(hwnd, r)
}

func (c *Controller) invalidateListAndScroll(hwnd syscall.Handle) {
	invalidateRect(hwnd, c.listContentRect())
	invalidateRect(hwnd, c.scrollBarRect())
}
