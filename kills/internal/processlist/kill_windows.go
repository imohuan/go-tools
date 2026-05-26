//go:build windows

package processlist

import (
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

// KillByImage terminates all processes whose image name matches (case-insensitive).
// imageName may include or omit the .exe suffix.
func KillByImage(imageName string) (killed int, accessDenied bool, notFound bool) {
	target := normalizeImage(imageName)
	if target == "" {
		return 0, false, true
	}

	procs, err := listProcesses()
	if err != nil {
		return 0, false, true
	}

	var matches []proc
	for _, p := range procs {
		if strings.EqualFold(p.name, target) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return 0, false, true
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range matches {
		wg.Add(1)
		go func(pid uint32) {
			defer wg.Done()
			ok := terminatePID(pid)
			mu.Lock()
			if ok {
				killed++
			} else {
				accessDenied = true
			}
			mu.Unlock()
		}(p.pid)
	}
	wg.Wait()

	if killed == 0 {
		notFound = !accessDenied
	}
	return killed, accessDenied, notFound
}

func normalizeImage(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(s), ".exe") {
		s += ".exe"
	}
	return s
}

func terminatePID(pid uint32) bool {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	err = windows.TerminateProcess(h, 1)
	return err == nil
}

// KillMessage builds a user-facing result message.
func KillMessage(imageName string, killed int, accessDenied, notFound bool) (success bool, message string) {
	if killed > 0 {
		if killed == 1 {
			return true, "已结束"
		}
		return true, fmt.Sprintf("已结束 %d 个实例", killed)
	}
	if accessDenied {
		return false, "拒绝访问（可尝试以管理员身份运行）"
	}
	if notFound {
		return true, "未找到运行中的进程"
	}
	return false, "结束进程失败"
}
