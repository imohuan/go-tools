//go:build !windows

package winutil

import "os/exec"

func HideConsole(cmd *exec.Cmd) {}

func OpenURL(url string) error { return nil }
