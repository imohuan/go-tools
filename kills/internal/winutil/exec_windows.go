//go:build windows

package winutil

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// HideConsole prevents child processes from flashing a console window.
func HideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
