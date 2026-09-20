//go:build windows

package main

import "golang.org/x/sys/windows"

// Every string this program writes is UTF-8, and a Windows console decodes
// stdout with its own code page — 437 on a US image, 950 on a Traditional
// Chinese one. Neither reads UTF-8, so `clawdline guide` printed 4094 correct
// code points as mojibake on a machine whose console had never been told.
//
// Saying so once, at startup, is the whole fix; a console that refuses (a
// redirected pipe has no code page to set) is left as it is, because the bytes
// were already right and only the screen was wrong.
func init() {
	// 65001 is CP_UTF8.
	_ = windows.SetConsoleOutputCP(65001)
}
