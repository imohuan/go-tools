//go:build windows

package main

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/getlantern/systray"
	"tray-view/internal/winui"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procGetCursorPos = user32.NewProc("GetCursorPos")
)

type point struct {
	X, Y int32
}

func main() {
	runtime.LockOSThread()

	ctrl := winui.New()

	go systray.Run(func() {
		if len(trayIcon) > 0 {
			systray.SetIcon(trayIcon)
		}
		systray.SetTitle("进程查看")
		systray.SetTooltip("进程查看 — 托盘菜单打开列表")

		mShow := systray.AddMenuItem("显示/隐藏", "打开进程列表")
		mQuit := systray.AddMenuItem("退出", "")

		go func() {
			for range mShow.ClickedCh {
				x, y := cursorPos()
				ctrl.Toggle(x-winui.WinW/2, y)
			}
		}()
		go func() {
			for range mQuit.ClickedCh {
				systray.Quit()
				os.Exit(0)
			}
		}()
	}, nil)

	ctrl.Run()
}

func cursorPos() (int, int) {
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return int(p.X), int(p.Y) - winui.WinH - 8
}
