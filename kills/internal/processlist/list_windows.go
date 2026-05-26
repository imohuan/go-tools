//go:build windows

package processlist

import (
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Entry struct {
	Name  string `json:"name"`
	PID   uint32 `json:"pid"`
	Count int    `json:"count"`
}

func Search(query string, limit int) ([]Entry, error) {
	query = strings.TrimSpace(strings.ToLower(query))
	if limit <= 0 {
		limit = 50
	}

	all, err := listProcesses()
	if err != nil {
		return nil, err
	}

	byName := make(map[string]*Entry)
	for _, p := range all {
		name := p.name
		lower := strings.ToLower(name)
		if query != "" && !strings.Contains(lower, query) {
			continue
		}
		if e, ok := byName[lower]; ok {
			e.Count++
			continue
		}
		byName[lower] = &Entry{Name: name, PID: p.pid, Count: 1}
	}

	out := make([]Entry, 0, len(byName))
	for _, e := range byName {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type proc struct {
	name string
	pid  uint32
}

func listProcesses() ([]proc, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))

	if err := windows.Process32First(snap, &pe); err != nil {
		return nil, err
	}

	var list []proc
	for {
		name := strings.TrimSpace(syscall.UTF16ToString(pe.ExeFile[:]))
		if name != "" && !strings.EqualFold(name, "[System Process]") {
			list = append(list, proc{name: name, pid: pe.ProcessID})
		}
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	return list, nil
}
