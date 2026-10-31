package http

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// screenPath is GET /v1/sessions/{id}/screen.
//
// **Deliberately a GET.** Watching a screen types nothing and changes no
// session, and a POST would put it behind the send gate, which is about running
// code on this machine. A read should never need that action's authority.
func screenPath(r *http.Request) (string, bool) {
	return sessionVerbIs(r, "screen", http.MethodGet, http.MethodHead)
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
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	item, err := s.actions().Find(ctx, id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	item.ExpectedStart = ownrecords.ProcessStart(item.PID)
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
	out.PromptSuggestion = wirePromptSuggestion(reading)
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
// A subscriber that is not keeping up is never waited for — blocking here
// would let one slow client hold the goroutine that reads every pane's FIFO —
// and, since C3, never skipped either (limits N19). It used to be a channel of
// sixteen that dropped whatever did not fit, without a count, so a slow page
// could be left showing a screen that had moved on and nothing anywhere said
// so. A revision is a latest value: what a stream is owed is the newest
// revision of each screen, not every one in between. So each stream keeps one
// waiting frame per screen, a newer revision replaces a waiting one (counted
// as coalesced), and the stream writes whatever is waiting when it next can.
// A stream with more different screens waiting than the register allows is
// ended instead (counted as disconnected); the page's EventSource reconnects
// and the panel reads each screen afresh, which is what the O26 rule asks of a
// consumer that fell behind.
type screenBus struct {
	mu    sync.Mutex
	next  int
	subs  map[int]*screenSub
	limit int

	coalesced    int64
	disconnected int64
	lastAt       time.Time
}

// screenSub is one stream's waiting frames.
type screenSub struct {
	// ready holds one token while anything is waiting: a signal, not a queue.
	ready chan struct{}
	// gone is closed when the bus ends this stream.
	gone    chan struct{}
	pending map[string]contract.ScreenEvent
	// order is the screens waiting, in the order they first moved.
	order []string
}

func newScreenBus() *screenBus {
	return &screenBus{subs: map[int]*screenSub{}, limit: int(CapacityLimit(capacity.SSEScreenPending))}
}

func (b *screenBus) subscribe() (int, *screenSub) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	sub := &screenSub{ready: make(chan struct{}, 1), gone: make(chan struct{}),
		pending: map[string]contract.ScreenEvent{}}
	b.subs[b.next] = sub
	return b.next, sub
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
	for token, sub := range b.subs {
		if _, waiting := sub.pending[id]; waiting {
			sub.pending[id] = event
			b.coalesced++
			b.lastAt = time.Now()
		} else if len(sub.pending) >= b.limit {
			delete(b.subs, token)
			close(sub.gone)
			b.disconnected++
			b.lastAt = time.Now()
			log.Printf("screen: a stream had %d screens waiting and was ended; its page reconnects and reads them afresh", len(sub.pending))
			continue
		} else {
			sub.pending[id] = event
			sub.order = append(sub.order, id)
		}
		select {
		case sub.ready <- struct{}{}:
		default:
		}
	}
}

// take is every frame waiting for one stream, oldest move first, and leaves
// it with none.
func (b *screenBus) take(sub *screenSub) []contract.ScreenEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]contract.ScreenEvent, 0, len(sub.order))
	for _, id := range sub.order {
		out = append(out, sub.pending[id])
	}
	sub.order = sub.order[:0]
	clear(sub.pending)
	return out
}

// reading is the `sse.screen_pending` row: the most frames any one stream has
// waiting now, and what the bus has coalesced and disconnected since it began.
func (b *screenBus) reading() capacity.Reading {
	b.mu.Lock()
	defer b.mu.Unlock()
	r := capacity.Reading{Known: true, Counters: capacity.Counters{
		Coalesced: b.coalesced, Disconnected: b.disconnected, LastActionAt: b.lastAt,
	}}
	for _, sub := range b.subs {
		r.Used = max(r.Used, int64(len(sub.pending)))
	}
	r.Note = strconv.Itoa(len(b.subs)) + " stream(s) open"
	return r
}

func wirePromptSuggestion(reading app.ScreenReading) *contract.PromptSuggestion {
	if reading.PromptConversation == "" || reading.PromptAt <= 0 {
		return nil
	}
	state := "none"
	if reading.PromptID != "" && reading.PromptText != "" {
		state = "available"
	}
	return &contract.PromptSuggestion{ID: reading.PromptID, Text: reading.PromptText, ConversationID: reading.PromptConversation, Source: "claude-terminal", State: state, ObservedAt: reading.PromptAt}
}

// CloudPromptChanges is a content-free invalidation signal. Only the current
// authenticated s/ row carries the suggestion, never the ss/ status feed.
func (s *Server) CloudPromptChanges() <-chan struct{} {
	if s.screens == nil {
		return nil
	}
	return s.screens.PromptChanges()
}
