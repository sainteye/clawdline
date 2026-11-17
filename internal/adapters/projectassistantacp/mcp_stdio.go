package projectassistantacp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type connectorStdio struct {
	p          *Process
	scan       *bufio.Scanner
	gate       chan struct{}
	mu         sync.Mutex
	failed     bool
	owner      string
	limits     Limits
	check      func(context.Context) error
	quarantine func(context.Context) error
}

func mcpRequestID(raw json.RawMessage) bool {
	if _, err := domaincloud.Parse(raw); err != nil {
		return false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	switch value.(type) {
	case string, float64:
		return true
	}
	return false
}

func startConnector(ctx context.Context, q app.OpenRequest, c app.NativeConnector, l Limits) (*connectorStdio, error) {
	guard, ok := q.Tools.(app.ExtensionGuard)
	if !ok {
		return nil, refusal(pa.Unsupported, "connector_guard")
	}
	if err := guard.CheckExtension(ctx, c.Ref); err != nil {
		return nil, err
	}
	owner, err := reserveExtension(q.Extensions.State, q.Binding.Fence)
	if err != nil {
		return nil, err
	}
	env := []string{}
	if c.Credential != nil {
		value, err := c.Credential(ctx)
		if err != nil {
			_ = os.RemoveAll(owner) // no connector process has started
			return nil, err
		}
		env = append(env, c.CredentialEnv+"="+string(value))
		for i := range value {
			value[i] = 0
		}
	}
	p, err := StartProcess(ctx, ProcessOptions{Configuration: Configuration{Executable: c.Executable, Arguments: c.Arguments, Environment: env}, Directory: q.Directory, Limits: l})
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), l.CleanupTimeout)
		defer cancel()
		if pa.Code(err) == pa.ExecutionEffectUnknown {
			if c.Quarantine != nil {
				_ = c.Quarantine(cleanup)
			}
		} else {
			if c.ReleaseCredential != nil {
				if e := c.ReleaseCredential(cleanup); e != nil {
					return nil, errors.Join(err, e)
				}
			}
			_ = os.RemoveAll(owner)
		}
		return nil, err
	} // uncertain startup keeps the reservation
	scan := bufio.NewScanner(p.stdout)
	scan.Buffer(make([]byte, min(l.FrameBytes, 4096)), l.FrameBytes)
	check := func(ctx context.Context) error {
		if err := guard.CheckExtension(ctx, c.Ref); err != nil {
			return err
		}
		if c.Credential != nil {
			// The source closure validates the captured credential revision. Its
			// value cannot replace the environment of this already owned process.
			value, err := c.Credential(ctx)
			for i := range value {
				value[i] = 0
			}
			return err
		}
		return ctx.Err()
	}
	return &connectorStdio{p: p, scan: scan, gate: make(chan struct{}, 1), owner: owner, limits: l, check: check, quarantine: c.Quarantine}, nil
}

// Exchange preserves the native MCP initialize/initialized/list/call sequence.
// An uncertain write, timeout, EOF or invalid response poisons this execution's
// connection. A later call never launches a replacement or replays an effect.
func (c *connectorStdio) exchange(ctx context.Context, body []byte) ([]byte, error) {
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, refusal(pa.Revoked, "connector_admission")
	}
	defer func() { <-c.gate }()
	c.mu.Lock()
	failed := c.failed
	c.mu.Unlock()
	if failed {
		return nil, refusal(pa.OutcomeUnknown, "connector_disconnected")
	}
	// A request waiting behind another RPC has not reached its effect yet.
	// Recheck after acquiring the serialized write slot, so queued requests
	// cannot reuse authority or a credential revision revoked while waiting.
	if c.check == nil {
		return nil, refusal(pa.Unsupported, "connector_guard")
	}
	if err := c.check(ctx); err != nil {
		return nil, err
	}
	var request frame
	if err := mcpDecode(body, &request); err != nil || request.JSONRPC != "2.0" || request.Method == "" || len(request.Result) != 0 || request.Error != nil {
		return nil, refusal(pa.Invalid, "connector_rpc")
	}
	if len(request.ID) > 0 && !mcpRequestID(request.ID) {
		return nil, refusal(pa.Invalid, "connector_rpc_id")
	}
	poison := func() ([]byte, error) {
		c.mu.Lock()
		c.failed = true
		c.mu.Unlock()
		return nil, refusal(pa.OutcomeUnknown, "connector_effect_unknown")
	}
	if err := c.p.Write(ctx, append(append([]byte{}, bytes.TrimSpace(body)...), '\n')); err != nil {
		return poison()
	}
	if len(request.ID) == 0 {
		return nil, nil
	}
	reader, ok := c.p.stdout.(interface{ SetReadDeadline(time.Time) error })
	if !ok {
		return poison()
	}
	deadline, _ := ctx.Deadline()
	if reader.SetReadDeadline(deadline) != nil {
		return poison()
	}
	for n := 0; n < c.limits.Notifications; n++ {
		if !c.scan.Scan() {
			return poison()
		}
		raw := append([]byte{}, c.scan.Bytes()...)
		var reply frame
		if mcpDecode(raw, &reply) != nil || reply.JSONRPC != "2.0" {
			return poison()
		}
		if len(reply.ID) == 0 && reply.Method != "" {
			continue
		}
		if !bytes.Equal(reply.ID, request.ID) || reply.Method != "" || (len(reply.Result) == 0) == (reply.Error == nil) {
			return poison()
		}
		return raw, nil
	}
	return poison()
}

func (c *connectorStdio) close(ctx context.Context) error {
	if err := c.p.Close(ctx); err != nil {
		if c.quarantine != nil {
			return errors.Join(err, c.quarantine(ctx))
		}
		return err
	}
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return refusal(pa.ExecutionEffectUnknown, "connector_cleanup")
	}
	defer func() { <-c.gate }()
	c.mu.Lock()
	c.failed = true
	c.mu.Unlock()
	if err := os.RemoveAll(c.owner); err != nil {
		return refusal(pa.ExecutionEffectUnknown, "connector_reservation_cleanup")
	}
	return nil
}
