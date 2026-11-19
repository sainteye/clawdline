//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package botdevserverprocess

import (
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func command(string, []string) (acp.Configuration, error) {
	return acp.Configuration{}, failure(pa.SandboxUnavailable, "server_process_supervisor")
}
