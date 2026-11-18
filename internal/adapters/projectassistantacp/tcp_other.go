//go:build !darwin && !linux

package projectassistantacp

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func openTCPRoot(context.Context, int, int) (tcpRoot, error) {
	return nil, refusal(pa.Unsupported, "owned_tcp_observation")
}
func tcpProcessIdentity(context.Context, int, int) (tcpIdentity, error) {
	return tcpIdentity{}, refusal(pa.Unsupported, "owned_tcp_observation")
}
func queryTCPListeners(context.Context, []int, Limits) ([]tcpBind, error) {
	return nil, refusal(pa.Unsupported, "owned_tcp_observation")
}
func (p *Process) tcpIdentities(context.Context) ([]processIdentity, error) {
	return nil, refusal(pa.Unsupported, "owned_tcp_observation")
}
