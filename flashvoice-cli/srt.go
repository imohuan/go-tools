package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

func formatSRTTime(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	h := ms / 3_600_000
	ms %= 3_600_000
	m := ms / 60_000
	ms %= 60_000
	s := ms / 1_000
	ms %= 1_000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

func segmentsToSRT(segments []Segment) string {
	if len(segments) == 0 {
		return ""
	}
	var b strings.Builder
	for i, seg := range segments {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%d\n", i+1)
		b.WriteString(formatSRTTime(seg.StartMs))
		b.WriteString(" --> ")
		b.WriteString(formatSRTTime(seg.EndMs))
		b.WriteByte('\n')
		b.WriteString(strings.TrimSpace(seg.Text))
		b.WriteByte('\n')
	}
	return b.String()
}

func srtBaseName(filePath string) string {
	base := filepath.Base(filePath)
	ext := filepath.Ext(base)
	if ext != "" {
		return strings.TrimSuffix(base, ext) + ".srt"
	}
	return base + ".srt"
}
