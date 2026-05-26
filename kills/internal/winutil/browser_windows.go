//go:build windows

package winutil

import (
	"syscall"
	"unsafe"
)

var (
	modShell32  = syscall.NewLazyDLL("shell32.dll")
	procShellEx = modShell32.NewProc("ShellExecuteW")
)

// OpenURL opens a URL in the default browser without spawning cmd.exe.
func OpenURL(url string) error {
	u, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	op, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	r, _, err := procShellEx.Call(
		0,
		uintptr(unsafe.Pointer(op)),
		uintptr(unsafe.Pointer(u)),
		0,
		0,
		1, // SW_SHOWNORMAL
	)
	if r <= 32 {
		return err
	}
	return nil
}
