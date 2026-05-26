//go:build windows

package winutil

import (
	"bytes"
	"io"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// DecodeConsole converts Windows console output (often GBK) to UTF-8 string.
func DecodeConsole(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return string(b)
	}
	reader := transform.NewReader(bytes.NewReader(b), simplifiedchinese.GBK.NewDecoder())
	out, err := io.ReadAll(reader)
	if err != nil {
		return string(b)
	}
	return string(out)
}
