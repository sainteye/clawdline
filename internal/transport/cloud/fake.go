package cloud

import (
	"context"
	"errors"
	"sync"

	"github.com/sainteye/clawdline-go/internal/domain/capacity"
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
	// FailIsChannelFull makes Fail read as one channel at its cap, which is
	// what Service answers with a typed refusal rather than a log line
	// (`Congested`). ChannelBytes is the bound that refusal's detail carries.
	FailIsChannelFull bool
	ChannelBytes      int
	// FailRefusals keeps even the refusal from leaving, which is the one case
	// where the waiter really is told nothing.
	FailRefusals bool
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
	if f.Fail != nil && (!out.Refusal || f.FailRefusals) {
		// A refusal is admitted under the spool's reserve, so a full channel
		// stops the answer and not the sentence that says so. FailRefusals is
		// the other case: the reserve itself could not take it.
		return f.Fail
	}
	f.published = append(f.published, out)
	return nil
}

// ChannelFull reports whether Publish's error was one channel at its cap.
func (f *Fake) ChannelFull(err error) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.FailIsChannelFull && err != nil && errors.Is(err, f.Fail)
}

// ChannelByteLimit is the bound a refusal's detail reports.
func (f *Fake) ChannelByteLimit() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ChannelBytes > 0 {
		return f.ChannelBytes
	}
	return int(capacity.Default(capacity.CloudSpoolChannelBytes))
}

// Published is a copy of what has been published so far.
func (f *Fake) Published() []Outbound {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Outbound(nil), f.published...)
}
