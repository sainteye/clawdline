//go:build !darwin && !linux

package main

import "errors"

func execNamedDaemon(string, []string, []string) error {
	return errors.New("named daemon execution is not supported on this platform")
}
