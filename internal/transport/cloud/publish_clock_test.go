package cloud

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/sainteye/clawdline/internal/app/cloudops"
)

// workingScan is one working Session whose line carries the assistant's clock.
func workingScan(line, freshness string) string {
	return fmt.Sprintf(`{"at":17,"scan":{"complete":true},"sessions":[
  {"id":"%%19","assistant":"claude","tty":"ttys001","state":"working","line":%q,"working_since":1000,
   "source":{"freshness":%q,"observed_at":4,"provenance":"tmux"},
   "closeability":{"state":"open","observed_at":1,"session_generation":2,"source":{"freshness":"fresh","observed_at":3}}}]}`,
		line, freshness)
}

func sessionChannels(names []string) []string {
	var out []string
	for _, name := range names {
		if strings.HasPrefix(name, "s/mac-01/%25") {
			out = append(out, name)
		}
	}
	return out
}

// Measured over 113.9 hours, one working Session alone was published 6,621
// times: its line's clock moved on every screen refresh and the row compared
// it whole.
func TestARowWhoseOnlyChangeIsTheClockIsNotRepublished(t *testing.T) {
	router := &fixedRouter{body: workingScan("Thinking… (1m 12s · ↑ 3k tokens)", "current")}
	out := &collector{}
	publisher := newPublisher(router, out)
	publisher.firstPass(context.Background())
	if names := sessionChannels(out.channels()); len(names) != 1 {
		t.Fatalf("the first pass did not publish the row: %v", out.channels())
	}

	out.reset()
	router.set(workingScan("Thinking… (1m 19s · ↑ 3k tokens)", "current"))
	publisher.Pass(context.Background())
	if names := sessionChannels(out.channels()); len(names) != 0 {
		t.Errorf("a row whose only change is its clock was republished: %v", names)
	}

	// The rest of the line is still content.
	out.reset()
	router.set(workingScan("Thinking… (1m 25s · ↑ 9k tokens)", "current"))
	publisher.Pass(context.Background())
	if names := sessionChannels(out.channels()); len(names) != 1 {
		t.Errorf("a line that said something new was not republished: %v", names)
	}
	if got := out.payload(t, "s/mac-01/%2519")["session"].(map[string]any)["line"]; got != "Thinking… (1m 25s · ↑ 9k tokens)" {
		t.Errorf("the published row is not the row as read: %v", got)
	}
}

// A source whose refresh is in progress answers with its last reading, marked
// unverified. Flipping to it and back says nothing new.
func TestTheUnverifiedFlipIsNotAChange(t *testing.T) {
	line := "Working (7s • esc to interrupt)"
	router := &fixedRouter{body: workingScan(line, "current")}
	out := &collector{}
	publisher := newPublisher(router, out)
	publisher.firstPass(context.Background())

	out.reset()
	router.set(workingScan(line, "unverified"))
	publisher.Pass(context.Background())
	router.set(workingScan(line, "current"))
	publisher.Pass(context.Background())
	if names := sessionChannels(out.channels()); len(names) != 0 {
		t.Errorf("the unverified flip was republished: %v", names)
	}

	// Missing is not a flip: the source has stopped answering for the row.
	router.set(workingScan(line, "missing"))
	publisher.Pass(context.Background())
	if names := sessionChannels(out.channels()); len(names) != 1 {
		t.Errorf("a row whose source went missing was not republished: %v", names)
	}
}

// The inventory line used to be written before the comparison, so it said
// "published" on every pass: 75,127 lines for 8,978 markers sent.
func TestTheInventoryLineCountsOnlyMarkersSent(t *testing.T) {
	router := &fixedRouter{body: completeScan}
	out := &collector{}
	publisher := newPublisher(router, out)
	var mu sync.Mutex
	var lines []string
	publisher.Log = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}
	publisher.firstPass(context.Background())
	publisher.Pass(context.Background())
	publisher.Pass(context.Background())
	mu.Lock()
	defer mu.Unlock()
	said := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "cloud: inventory published:") {
			said++
		}
	}
	sent := 0
	for _, name := range out.channels() {
		if name == "s/mac-01/"+InventorySessionID {
			sent++
		}
	}
	if sent != 1 || said != sent {
		t.Errorf("%d inventory line(s) for %d marker(s) sent: %v", said, sent, lines)
	}
}

// The publisher reads the daemon's shared session list when it is given one,
// and the route only when it is not.
func TestThePublisherReadsTheSharedListBeforeTheRoute(t *testing.T) {
	var listReads int
	router := routerFunc(func(_ context.Context, req cloudops.LocalRequest) (cloudops.LocalResponse, error) {
		if req.Path == "/v1/sessions" {
			listReads++
		}
		return cloudops.LocalResponse{Status: 503, Body: []byte(`{}`)}, nil
	})
	out := &collector{}
	publisher := newPublisher(router, out)
	publisher.Sessions = func(context.Context) ([]byte, bool) { return []byte(completeScan), true }
	publisher.firstPass(context.Background())
	if listReads != 0 {
		t.Errorf("the route was read %d time(s) beside the shared list", listReads)
	}
	if names := sessionChannels(out.channels()); len(names) != 1 {
		t.Errorf("the shared list's row was not published: %v", out.channels())
	}

	publisher.Sessions = func(context.Context) ([]byte, bool) { return nil, false }
	publisher.Pass(context.Background())
	if listReads != 1 {
		t.Errorf("a publisher with no shared list did not read the route: %d", listReads)
	}
}
