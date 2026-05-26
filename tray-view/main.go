//go:build windows

package main

import (
	"os"
	"runtime"

	"github.com/getlantern/systray"
	"tray-view/internal/winui"
)

func main() {
	ctrl := winui.New()

	go func() {
		runtime.LockOSThread()
		ctrl.Run()
	}()

	<-ctrl.Ready()

	systray.Run(func() {
		if len(trayIcon) > 0 {
			systray.SetIcon(trayIcon)
		}
		systray.SetTitle("进程查看")
		systray.SetTooltip("进程查看 — 右键托盘图标，选择「显示/隐藏」")

		mShow := systray.AddMenuItem("显示/隐藏", "在屏幕中央打开进程列表")
		mQuit := systray.AddMenuItem("退出", "")

		go func() {
			for range mShow.ClickedCh {
				ctrl.PostToggle()
			}
		}()
		go func() {
			for range mQuit.ClickedCh {
				systray.Quit()
				os.Exit(0)
			}
		}()
	}, nil)
}
