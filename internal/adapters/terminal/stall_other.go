//go:build !darwin

package terminal

// itermStallSteps is nothing on a platform with no iTerm2: nothing records a
// failure there, so no diagnosis is ever started.
func itermStallSteps(dir string) []stallStep { return nil }
