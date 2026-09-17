package cloud

import (
	"context"
	"sync"
)

// Fake is a transport with no relay behind it: requests are handed in, answers
// are kept. It is in the package rather than in a test file because the
// connection half and the wiring above both need something to build against
// before a relay exists, and a fake that only tests can see is a fake that
// gets written twice.
type Fake struct {
	requests chan Inbound

	mu sync.Mutex
	// Published is every answer this transport was given, in order.
	published []Outbound
	// Fail, when set, is returned by Publish instead of delivering.
	Fail error
}

// NewFake returns a fake with room for depth queued requests.
func NewFake(depth int) *Fake {
	if depth < 1 {
		depth = 1
	}
	return &Fake{requests: make(chan Inbound, depth)}
}

// Deliver queues one request for the service to answer.
func (f *Fake) Deliver(in Inbound) { f.requests <- in }

// Close ends Service.Run.
func (f *Fake) Close() { close(f.requests) }

func (f *Fake) Requests() <-chan Inbound { return f.requests }

func (f *Fake) Publish(_ context.Context, out Outbound) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Fail != nil {
		return f.Fail
	}
	f.published = append(f.published, out)
	return nil
}

// Published is a copy of what has been published so far.
func (f *Fake) Published() []Outbound {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Outbound(nil), f.published...)
}
