package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sainteye/clawdline/internal/adapters/install"
)

// namedDaemonPath also covers the first update installed by an older updater:
// its LaunchAgent still names service/clawdline, but this CLI hands serve to
// the dedicated signed daemon before it opens state or listens on a port.
func namedDaemonPath(exe string, layout install.Layout) (string, error) {
	if filepath.Base(exe) == install.DaemonBinary {
		return "", nil
	}
	if name, _ := layout.ServiceRelease(exe); name != "" {
		if current, _ := layout.CurrentRelease(); current != "" {
			name = current
		}
		if layout.ServiceExecutable(name) == layout.ServiceBin {
			if err := layout.InstallServiceBinary(name); err != nil {
				return "", err
			}
			return layout.ServiceBin, nil
		}
		return "", nil
	}
	sibling := filepath.Join(filepath.Dir(exe), install.DaemonBinary)
	info, err := os.Lstat(sibling)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("the bundled background service is not an executable regular file")
	}
	return sibling, nil
}

func delegateNamedDaemon() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	layout, err := install.DefaultLayout()
	if err != nil {
		return err
	}
	target, err := namedDaemonPath(exe, layout)
	if err != nil || target == "" {
		return err
	}
	args := []string{target}
	if commandLanguage != "" {
		args = append(args, "--lang", commandLanguage)
	}
	args = append(args, os.Args[1:]...)
	return execNamedDaemon(target, args, os.Environ())
}
