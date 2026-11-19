package botdevserver

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"sync"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type callReceipt struct {
	hash   string
	done   chan struct{}
	result json.RawMessage
	err    error
}

// Caller belongs to exactly one source-authenticated native execution. The
// Host owns its lifetime/check, never tool arguments. Mutation handlers still
// need durable intent before external IO; this bounded cache also remembers
// no-op and uncertain replies within the same execution. Reads do not use it.
type Caller struct {
	lifetime context.Context
	check    func(context.Context) error
	mu       sync.Mutex
	receipts map[string]*callReceipt
}

func NewCaller(lifetime context.Context, check func(context.Context) error) (*Caller, error) {
	if lifetime == nil || check == nil {
		return nil, failure(pa.Invalid, "server_caller")
	}
	c := &Caller{lifetime: lifetime, check: check, receipts: map[string]*callReceipt{}}
	context.AfterFunc(lifetime, func() {
		c.mu.Lock()
		c.receipts = nil
		c.mu.Unlock()
	})
	return c, nil
}

func (c *Caller) Do(ctx context.Context, key, hash string, run func(context.Context) (json.RawMessage, error)) (json.RawMessage, error) {
	digest, err := hex.DecodeString(hash)
	if key == "" || len(key) > capacity.MaximumBotFrameBytes || err != nil || len(digest) != 32 || run == nil {
		return nil, failure(pa.Invalid, "server_command")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if c.lifetime.Err() != nil {
		return nil, failure(pa.Revoked, "server_caller")
	}
	op, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.lifetime, cancel)
	defer func() { stop(); cancel() }()
	if err = c.check(op); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.lifetime.Err() != nil || c.receipts == nil {
		c.mu.Unlock()
		return nil, failure(pa.Revoked, "server_caller")
	}
	if prior := c.receipts[key]; prior != nil {
		c.mu.Unlock()
		if prior.hash != hash {
			return nil, failure(pa.IdempotencyConflict, "server_command")
		}
		select {
		case <-prior.done:
			return c.deliver(op, prior)
		case <-op.Done():
			return nil, op.Err()
		}
	}
	if len(c.receipts) >= capacity.MaximumBotServerCallerReceipts {
		c.mu.Unlock()
		return nil, failure(pa.CapacityFull, "server_caller_receipts")
	}
	r := &callReceipt{hash: hash, done: make(chan struct{})}
	c.receipts[key] = r
	c.mu.Unlock()
	r.result, r.err = run(op)
	if r.err == nil && (!json.Valid(r.result) || len(r.result) > capacity.MaximumBotFrameBytes) {
		r.result, r.err = nil, failure(pa.OutcomeUnknown, "server_reply")
	}
	if r.err != nil {
		r.result = nil
	} else {
		r.result = append(json.RawMessage(nil), r.result...)
	}
	close(r.done)
	return c.deliver(op, r)
}

// Remembering an earlier result does not preserve permission to disclose it.
// The transport must also retain its normal source publication/Forget fence.
func (c *Caller) deliver(ctx context.Context, r *callReceipt) (json.RawMessage, error) {
	if c.lifetime.Err() != nil {
		return nil, failure(pa.Revoked, "server_caller")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.check(ctx); err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), r.result...), r.err
}
