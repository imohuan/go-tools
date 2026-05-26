//go:build windows

package winui

import (
	"syscall"
	"unsafe"
)

func (c *Controller) initGDI() {
	c.brushBG = solidBrush(0x121212)
	c.brushTitle = solidBrush(0x0d0d0d)
	c.brushHeader = solidBrush(0x181818)
	c.brushListBg = solidBrush(0x161616)
	c.brushRowEven = solidBrush(0x1c1c1c)
	c.brushRowOdd = solidBrush(0x181818)
	c.brushRowHover = solidBrush(0x2a3140)
	c.brushBtn = solidBrush(0x2a2a2a)
	c.brushBtnHover = solidBrush(0x3d3d3d)
	c.brushChkOff = solidBrush(0x1c1c1c)
	c.brushChkOn = solidBrush(0x3d8bfd)
	c.brushBarTrack = solidBrush(0x2e2e2e)
	c.brushBarGreen = solidBrush(0x4caf50)
	c.brushBarYellow = solidBrush(0xffb74d)
	c.brushBarRed = solidBrush(0xef5350)
	c.brushScrollTrack = solidBrush(0x1f1f1f)
	c.brushScrollThumb = solidBrush(0x505050)
	c.brushScrollThumbHot = solidBrush(0x6a6a6a)
	c.brushBorder = solidBrush(0x2e2e2e)
	c.brushCloseHover = solidBrush(0xc42b1c)

	c.font = createUIFont(13, false)
	c.fontTitle = createUIFont(14, true)
	c.fontHeader = createUIFont(12, false)
	c.fontMetrics = createUIFont(11, false)
}

func (c *Controller) freeGDI() {
	for _, h := range []syscall.Handle{
		c.brushBG, c.brushTitle, c.brushHeader, c.brushListBg,
		c.brushRowEven, c.brushRowOdd, c.brushRowHover,
		c.brushBtn, c.brushBtnHover, c.brushChkOff, c.brushChkOn,
		c.brushBarTrack, c.brushBarGreen, c.brushBarYellow, c.brushBarRed,
		c.brushScrollTrack, c.brushScrollThumb, c.brushScrollThumbHot, c.brushBorder, c.brushCloseHover,
		c.font, c.fontTitle, c.fontHeader, c.fontMetrics,
	} {
		if h != 0 {
			procDeleteObject.Call(uintptr(h))
		}
	}
}

func createUIFont(pixels int, bold bool) syscall.Handle {
	weight := uintptr(400)
	if bold {
		weight = 600
	}
	name, _ := syscall.UTF16PtrFromString("Segoe UI")
	h, _, _ := procCreateFontW.Call(
		uintptr(-pixels), 0, 0, 0, weight, 0, 0, 0,
		1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(name)),
	)
	if h == 0 {
		name, _ = syscall.UTF16PtrFromString("Microsoft YaHei UI")
		h, _, _ = procCreateFontW.Call(
			uintptr(-pixels), 0, 0, 0, weight, 0, 0, 0,
			1, 0, 0, 5, 0,
			uintptr(unsafe.Pointer(name)),
		)
	}
	return syscall.Handle(h)
}
