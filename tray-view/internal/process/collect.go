package process

import (
	"path/filepath"
	"sort"
	"strings"

	ps "github.com/shirou/gopsutil/v3/process"
)

// Entry is one row in the tray list (aggregated by executable name).
type Entry struct {
	Name       string
	Display    string
	MemMB      float64
	CPUPercent float64
	ExePath    string
	IsSystem   bool
}

// Snapshot holds list data for the UI.
type Snapshot struct {
	TrashMB float64
	System  Entry
	Apps    []Entry
}

type agg struct {
	name    string
	display string
	mem     float64
	cpu     float64
	exe     string
	count   int
}

var systemExe = map[string]bool{
	"system": true, "registry": true, "smss.exe": true, "csrss.exe": true,
	"wininit.exe": true, "services.exe": true, "lsass.exe": true, "svchost.exe": true,
	"fontdrvhost.exe": true, "dwm.exe": true, "conhost.exe": true, "runtimebroker.exe": true,
	"searchindexer.exe": true, "sihost.exe": true, "taskhostw.exe": true,
	"spoolsv.exe": true, "dllhost.exe": true,
}

// Collect builds a process snapshot for display.
func Collect() (*Snapshot, error) {
	procs, err := ps.Processes()
	if err != nil {
		return nil, err
	}

	groups := make(map[string]*agg)
	systemAgg := &agg{display: "windows系统进程", name: "__system__"}

	for _, p := range procs {
		name, err := p.Name()
		if err != nil || name == "" {
			continue
		}
		memInfo, _ := p.MemoryInfo()
		memMB := 0.0
		if memInfo != nil {
			memMB = float64(memInfo.RSS) / (1024 * 1024)
		}
		if memMB < 0.5 {
			continue
		}
		cpuPct, _ := p.CPUPercent()
		if cpuPct < 0 {
			cpuPct = 0
		}
		exe, _ := p.Exe()
		base := strings.ToLower(filepath.Base(name))
		display := friendlyName(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
		if exe != "" {
			display = friendlyName(strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe)))
		}

		if systemExe[base] {
			systemAgg.mem += memMB
			systemAgg.cpu += cpuPct
			systemAgg.count++
			continue
		}

		g, ok := groups[base]
		if !ok {
			groups[base] = &agg{name: base, display: display, exe: exe}
			g = groups[base]
		}
		g.mem += memMB
		g.cpu += cpuPct
		g.count++
		if g.exe == "" && exe != "" {
			g.exe = exe
		}
	}

	apps := make([]Entry, 0, len(groups))
	for _, g := range groups {
		if g.mem < 1 {
			continue
		}
		cpu := g.cpu
		if g.count > 1 {
			cpu /= float64(g.count)
		}
		apps = append(apps, Entry{
			Name: g.name, Display: g.display, MemMB: g.mem,
			CPUPercent: cpu, ExePath: g.exe,
		})
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].MemMB > apps[j].MemMB })
	if len(apps) > 18 {
		apps = apps[:18]
	}

	sysCPU := systemAgg.cpu
	if systemAgg.count > 0 {
		sysCPU /= float64(systemAgg.count)
	}

	return &Snapshot{
		TrashMB: estimateTrashMB(apps),
		System: Entry{
			Name: "__system__", Display: "windows系统进程",
			MemMB: systemAgg.mem, CPUPercent: sysCPU, IsSystem: true,
		},
		Apps: apps,
	}, nil
}

func friendlyName(base string) string {
	known := map[string]string{
		"chrome": "Chrome浏览器", "cursor": "Cursor", "node": "Node.js",
		"powershell": "Windows PowerShell", "clash": "Clash",
		"clash-verge": "Clash Verge", "nvidia": "NVIDIA APP",
		"yourphone": "微软YourPhone", "wechat": "微信",
	}
	low := strings.ToLower(base)
	for k, v := range known {
		if strings.Contains(low, k) {
			return v
		}
	}
	return base
}

func estimateTrashMB(apps []Entry) float64 {
	var idle float64
	for _, a := range apps {
		if a.MemMB < 30 && a.CPUPercent < 1 {
			idle += a.MemMB * 0.15
		}
	}
	if idle < 8 {
		idle = 8 + float64(len(apps))*0.3
	}
	if idle > 512 {
		idle = 512
	}
	return idle
}
