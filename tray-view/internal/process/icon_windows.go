//go:build windows

package process

import (
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32           = windows.NewLazySystemDLL("shell32.dll")
	user32Icon        = windows.NewLazySystemDLL("user32.dll")
	procSHGetFileInfo = shell32.NewProc("SHGetFileInfoW")
	procDestroyIcon   = user32Icon.NewProc("DestroyIcon")
)

const (
	shgfiIcon      = 0x00000100
	shgfiSmallIcon = 0x00000001
)

type shFileInfo struct {
	HIcon         syscall.Handle
	IIcon         int32
	DwAttributes  uint32
	SzDisplayName [260]uint16
	SzTypeName    [80]uint16
}

var (
	iconMu    sync.RWMutex
	iconCache = make(map[string]syscall.Handle)
	iconOrder []string
	iconLimit = 80
)

// SmallIcon returns HICON for an executable path (cached).
func SmallIcon(exePath string) syscall.Handle {
	if exePath == "" {
		return 0
	}
	iconMu.RLock()
	if h, ok := iconCache[exePath]; ok {
		iconMu.RUnlock()
		return h
	}
	iconMu.RUnlock()

	h := loadSmallIcon(exePath)
	if h == 0 {
		return 0
	}

	iconMu.Lock()
	defer iconMu.Unlock()
	if old, ok := iconCache[exePath]; ok {
		procDestroyIcon.Call(uintptr(old))
		iconCache[exePath] = h
		return h
	}
	iconCache[exePath] = h
	iconOrder = append(iconOrder, exePath)
	for len(iconOrder) > iconLimit {
		evict := iconOrder[0]
		iconOrder = iconOrder[1:]
		if eh, ok := iconCache[evict]; ok {
			procDestroyIcon.Call(uintptr(eh))
			delete(iconCache, evict)
		}
	}
	return h
}

func loadSmallIcon(path string) syscall.Handle {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	var fi shFileInfo
	r, _, _ := procSHGetFileInfo.Call(
		uintptr(unsafe.Pointer(p)),
		0,
		uintptr(unsafe.Pointer(&fi)),
		unsafe.Sizeof(fi),
		shgfiIcon|shgfiSmallIcon,
	)
	if r == 0 {
		return 0
	}
	return fi.HIcon
}
