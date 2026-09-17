package http

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// screenPath is GET /v1/sessions/{id}/screen.
//
// **Deliberately a GET.** Watching a screen types nothing and changes no
// session, and a POST would put it behind the send gate, which is about running
// code on this machine. A device that may only read must still be able to look.
func screenPath(r *http.Request) (string, bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return "", false
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/v1/sessions/")
	if !ok {
		return "", false
	}
	id, ok := strings.CutSuffix(rest, "/screen")
	return id, ok && id != ""
}

// screenWire is contract.Screen as it is sent. It exists for the three keys the
// generator cannot express: `text`, `lines` and `at` are absent until the first
// capture completes, and each has a real value that omitempty would drop — an
// empty screen, a screen of zero rows, and, in principle, a capture at the
// epoch. A pointer says absent; the zero value says the answer is zero. The
// outer fields shadow the embedded ones for encoding/json.
type screenWire struct {
	contract.Screen
	Text  *string `json:"text,omitempty"`
	Lines *int64  `json:"lines,omitempty"`
	At    *int64  `json:"at,omitempty"`
}

type screenAnswerWire struct {
	Screen screenWire `json:"screen"`
}

// sessionScreenRoute answers one session's live screen.
//
// **Reading it is the subscription.** There is no route that attaches a pipe
// and none that takes one off: this renews a thirty-second lease, and a lease
// nobody renews expires and takes the pipe with it.
//
// It never waits on a terminal. The answer comes out of what this daemon
// already holds and the capture happens behind it, so the first read of a
// session answers `pending` and the screen arrives a few milliseconds later on
// the `screen` event like every other one.
func (s *Server) sessionScreenRoute(w http.ResponseWriter, r *http.Request, id string) {
	if !ownsSessions() {
		s.proxy.ServeHTTP(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	item, err := s.actions().Find(ctx, id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	writeJSON(w, screenAnswerWire{Screen: wireScreen(s.screens.Read(item, time.Now()))})
}

// wireScreen says what it is looking at before it says what it saw.
//
// `backend` and `channel` are first in the contract because a screen with no
// backend named is the defect this exists to avoid, and `lines` is what came
// back rather than what was asked for — an alternate-screen program has no
// history to give and the payload must not imply otherwise.
func wireScreen(reading app.ScreenReading) screenWire {
	out := screenWire{Screen: contract.Screen{
		ID:            reading.SessionID,
		Backend:       contract.Backend(reading.Backend),
		Channel:       contract.ScreenChannel(reading.Channel),
		Revision:      reading.Revision,
		Readable:      reading.Readable,
		Pending:       reading.Pending(),
		WatchingUntil: reading.WatchingUntil.Unix(),
		Captures:      int64(reading.Captures),
		Signals:       int64(reading.Signals),
	}}
	if reading.Text != nil {
		text := *reading.Text
		out.Text = &text
		// The trailing newline a capture ends with is a terminator, not a row.
		// Counting it would report 26 lines for a 25-line screen, and this
		// number is the one place the payload says how much there actually was.
		body := strings.TrimSuffix(text, "\n")
		lines := int64(0)
		if body != "" {
			lines = int64(strings.Count(body, "\n") + 1)
		}
		out.Lines = &lines
	}
	if !reading.At.IsZero() {
		at := reading.At.Unix()
		out.At = &at
	}
	if reading.Channel == app.ScreenOnDemand {
		out.AskAgainAfterMs = int64(app.ScreenOnDemandFloor / time.Millisecond)
	}
	return out
}

// screensRoute is the machine state this feature creates, published so that
// somebody can see it.
//
// `tmux list-panes -a -F '#{pane_id} #{pane_pipe}'` answers the same question in
// one command; this answers it *and* says which of those pipes this daemon can
// account for, which tmux cannot, because `#{pane_pipe}` is a boolean with no
// owner in it.
func (s *Server) screensRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"what this daemon is watching is read with GET")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	reading := s.screens.Inventory(ctx)
	rows := make([]contract.ScreenWatch, 0, len(reading.Screens))
	for _, row := range reading.Screens {
		rows = append(rows, contract.ScreenWatch{
			ID:        row.ID,
			Backend:   contract.Backend(row.Backend),
			Channel:   contract.ScreenChannel(row.Channel),
			Watching:  row.Watching,
			ExpiresIn: int64(row.ExpiresIn),
			Signalled: row.Signalled,
			Captures:  int64(row.Captures),
			Signals:   int64(row.Signals),
			Readable:  row.Readable,
		})
	}
	writeJSON(w, contract.ScreenInventory{
		Screens:      rows,
		Attached:     orEmpty(reading.Attached),
		Piped:        orEmpty(reading.Piped),
		Unattributed: orEmpty(reading.Unattributed),
		WindowMs:     int64(app.ScreenWindow / time.Millisecond),
		LeaseSeconds: int64(app.ScreenLease / time.Second),
	})
}

func orEmpty(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// screenBus fans one revision out to every stream this daemon is publishing.
//
// Only the revision travels. The screen itself is fetched through the
// authenticated GET, exactly as a transcript append is: a stream frame reaches
// every device on that connection, and a terminal somebody else is watching has
// no business on this phone's socket.
//
// A subscriber that is not keeping up is skipped rather than waited for. The
// frame it misses costs it one stale revision, which the next signal or the
// panel's own fifteen-second lease renewal corrects; blocking here would let one
// slow client hold the goroutine that reads every pane's FIFO.
type screenBus struct {
	mu   sync.Mutex
	next int
	subs map[int]chan contract.ScreenEvent
}

func newScreenBus() *screenBus {
	return &screenBus{subs: map[int]chan contract.ScreenEvent{}}
}

func (b *screenBus) subscribe() (int, <-chan contract.ScreenEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	ch := make(chan contract.ScreenEvent, 16)
	b.subs[b.next] = ch
	return b.next, ch
}

func (b *screenBus) unsubscribe(token int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subs, token)
}

func (b *screenBus) publish(id, revision string) {
	event := contract.ScreenEvent{ID: id, Revision: revision, At: time.Now().Unix()}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- event:
		default:
		}
	}
}
