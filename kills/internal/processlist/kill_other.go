//go:build !windows

package processlist

func KillByImage(imageName string) (killed int, accessDenied bool, notFound bool) {
	return 0, false, true
}

func KillMessage(imageName string, killed int, accessDenied, notFound bool) (success bool, message string) {
	return false, "仅支持 Windows"
}
