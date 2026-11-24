//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package botdevserverprocess

import (
	"context"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
)

// Adopt has nothing to adopt where Start cannot launch a dev server.
func (l *Launcher) Adopt(context.Context, servers.Spec, servers.Mark) (servers.Process, error) {
	return nil, nil
}
