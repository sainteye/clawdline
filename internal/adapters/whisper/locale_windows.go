//go:build windows

package whisper

import (
	"syscall"
	"unsafe"
)

// userLocale is GetUserDefaultLocaleName: the locale the person chose in
// Windows' own settings, as a BCP 47 name like `zh-TW`.
func userLocale() string {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")
	if proc.Find() != nil {
		return ""
	}
	// LOCALE_NAME_MAX_LENGTH
	buf := make([]uint16, 85)
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
