package winui

import (
	"sort"
	"strings"
)

// SortField is the active sort column.
type SortField int

const (
	SortByMem SortField = iota
	SortByName
	SortByCPU
)

func (c *Controller) toggleSort(field SortField) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sortBy == field {
		c.sortAsc = !c.sortAsc
		return
	}
	c.sortBy = field
	switch field {
	case SortByName:
		c.sortAsc = true
	default:
		c.sortAsc = false
	}
}

func sortRows(rows []uiRow, by SortField, asc bool) []uiRow {
	if len(rows) == 0 {
		return rows
	}

	pinned := make([]uiRow, 0, 2)
	rest := make([]uiRow, 0, len(rows))
	for _, r := range rows {
		if r.ID == "trash" || r.ID == "__system__" {
			if r.ID == "trash" {
				pinned = append([]uiRow{r}, pinned...)
			} else {
				pinned = append(pinned, r)
			}
			continue
		}
		rest = append(rest, r)
	}

	sort.Slice(rest, func(i, j int) bool {
		less := false
		switch by {
		case SortByName:
			less = strings.ToLower(rest[i].Label) < strings.ToLower(rest[j].Label)
		case SortByCPU:
			less = rest[i].CPUPercent < rest[j].CPUPercent
		default:
			less = rest[i].MemMB < rest[j].MemMB
		}
		if asc {
			return less
		}
		return !less
	})

	out := make([]uiRow, 0, len(rows))
	out = append(out, pinned...)
	out = append(out, rest...)
	return out
}

func (c *Controller) sortedRows(rows []uiRow) []uiRow {
	c.mu.Lock()
	by, asc := c.sortBy, c.sortAsc
	c.mu.Unlock()
	return sortRows(rows, by, asc)
}

func sortLabel(field SortField, active SortField, asc bool) string {
	base := ""
	switch field {
	case SortByName:
		base = "名称"
	case SortByCPU:
		base = "CPU"
	default:
		base = "内存"
	}
	if field != active {
		return base
	}
	if asc {
		return base + " ▲"
	}
	return base + " ▼"
}
