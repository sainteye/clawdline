//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package botdevserverprocess

import acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"

func command(text string, environment []string) (acp.Configuration, error) {
	// No login/interactive shell: do not load a provider's startup hooks.
	return acp.Configuration{Executable: "/bin/sh", Arguments: []string{"-c", text}, Environment: environment}, nil
}
