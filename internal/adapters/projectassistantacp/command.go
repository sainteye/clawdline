package projectassistantacp

import (
	"context"
	"io"
)

// Output lends the owned command's stdout to one consumer. StartProcess does
// not require an ACP peer: a source-owned foreground command can use the same
// process identity and cleanup rules without constructing a Client/provider.
// The consumer owns framing and retention; this port adds no output buffer.
// Close interrupts a blocked read. Never attach an ACP Client as a second reader.
func (p *Process) Output() io.Reader { return p.stdout }

// WaitExit observes only the direct child's reap receipt. Its caller context
// bounds this observation, not the lifetime of the owned command. A successful
// wait does not establish descendant cleanup; the owner must still call Close
// and retain an unknown cleanup result instead of silently starting a new owner.
func (p *Process) WaitExit(ctx context.Context) error {
	if !waitOwnedDone(ctx, p.done) {
		return ctx.Err()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}
