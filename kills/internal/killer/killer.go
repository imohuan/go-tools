package killer

import (
	"fmt"
	"strings"
	"sync"

	"kills/internal/processlist"
)

type Result struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func KillAll(names []string) []Result {
	targets := dedupeNames(names)
	if len(targets) == 0 {
		return nil
	}

	results := make([]Result, len(targets))
	var wg sync.WaitGroup
	for i, name := range targets {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			results[i] = killOne(name)
		}(i, name)
	}
	wg.Wait()
	return results
}

func dedupeNames(names []string) []string {
	seen := make(map[string]struct{})
	var targets []string
	for _, raw := range names {
		name := normalizeName(raw)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, name)
	}
	return targets
}

func normalizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(s), ".exe") {
		s += ".exe"
	}
	return s
}

func killOne(imageName string) Result {
	killed, denied, notFound := processlist.KillByImage(imageName)
	ok, msg := processlist.KillMessage(imageName, killed, denied, notFound)
	return Result{Name: imageName, Success: ok, Message: msg}
}

func Summary(results []Result) string {
	ok, fail := 0, 0
	for _, r := range results {
		if r.Success {
			ok++
		} else {
			fail++
		}
	}
	return fmt.Sprintf("完成：成功 %d，失败 %d", ok, fail)
}
