//go:build !darwin && !linux

package main

import "os/exec"

func detach(*exec.Cmd) {}
