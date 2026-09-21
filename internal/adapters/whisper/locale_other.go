//go:build !windows

package whisper

// userLocale has nothing to read off Windows.
func userLocale() string { return "" }
