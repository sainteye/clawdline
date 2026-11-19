//go:build darwin || linux

package projectassistantacp

import "context"

func (p *Process) tcpIdentities(ctx context.Context) ([]processIdentity, error) {
	return p.identities(ctx)
}
