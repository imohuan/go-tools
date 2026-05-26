package winui

import "tray-view/internal/process"

type uiRow struct {
	ID         string
	Label      string
	MemMB      float64
	CPUPercent float64
	ExePath    string
	SystemBar  bool
}

func buildRows(snap *process.Snapshot) []uiRow {
	if snap == nil {
		return nil
	}
	rows := []uiRow{{
		ID: "trash", Label: "内存临时垃圾", MemMB: snap.TrashMB, SystemBar: true,
	}}
	rows = append(rows, uiRow{
		ID: snap.System.Name, Label: snap.System.Display,
		MemMB: snap.System.MemMB, CPUPercent: snap.System.CPUPercent, SystemBar: true,
	})
	for _, a := range snap.Apps {
		rows = append(rows, uiRow{
			ID: a.Name, Label: a.Display, MemMB: a.MemMB,
			CPUPercent: a.CPUPercent, ExePath: a.ExePath,
		})
	}
	return rows
}
