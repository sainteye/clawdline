//go:build darwin || linux

package main

import "syscall"

func execNamedDaemon(path string, args, env []string) error { return syscall.Exec(path, args, env) }
