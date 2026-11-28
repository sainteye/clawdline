package config

import "os"

// Isolated selects a disposable environment for the test daemon.
func Isolated() bool {
	return os.Getenv("CLAWDLINE_ISOLATED") == "1"
}
