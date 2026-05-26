//go:build !windows

package winutil

func DecodeConsole(b []byte) string {
	return string(b)
}
