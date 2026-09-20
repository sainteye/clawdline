package http

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// events serves the console's one stream.
//
// There are two ways to do that and the difference is whether anything is
// behind this daemon. With nothing behind it — the ordinary case — it publishes
// its own (see ownEvents). Standing in front of another daemon, which is what
// it did in front of the Swift app until 2026-09-19 and what an operator can
// still ask for with config.UpstreamPortEnv, it owns half of the stream and
// edits the rest. The stream carries two whole-snapshot payloads — `sessions`
// and `orchestrator` — and this daemon has a reading for the first and none for
// the second, so upstream's stream is consumed frame by frame, `sessions`
// frames are replaced with ours, and everything else passes through untouched.
//
// Replacing in place rather than publishing on our own clock keeps the update
// rhythm exactly as it was: upstream still decides when a session update is
// due, and only the content changes. It also means the client never sees two
// sources for one payload, which is what makes the generation counter it
// enforces meaningful.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if !s.ownsSessions() {
		s.forwardUpstream(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.forwardUpstream(w, r)
		return
	}

	// With nothing behind this daemon there is nothing to splice into, so the
	// stream is published rather than edited. See ownEvents for what that costs.
	if !s.proxying() {
		s.ownEvents(w, r)
		return
	}

	upstream, err := s.openUpstreamEvents(r)
	if err != nil {
		writeRefusal(w, http.StatusBadGateway, "upstream_unreachable", err.Error())
		return
	}
	defer upstream.Body.Close()

	for k, vs := range upstream.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(upstream.StatusCode)
	flusher.Flush()

	// Upstream is read on its own goroutine so that this one can also carry
	// the broker's progress notes, which upstream knows nothing about, without
	// two goroutines ever writing to the response.
	frames := make(chan []string)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		defer close(frames)
		reader := bufio.NewReader(upstream.Body)
		frame := make([]string, 0, 4)
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				trimmed := strings.TrimRight(line, "\r\n")
				if trimmed != "" {
					frame = append(frame, trimmed)
				} else {
					// A blank line ends a frame. Comments such as the keepalive
					// ping arrive as their own frame and pass through unchanged.
					select {
					case frames <- frame:
					case <-stop:
						return
					}
					frame = make([]string, 0, 4)
				}
			}
			if err != nil {
				return
			}
		}
	}()

	feed := s.openProgress()
	defer feed.close()
	stream := &eventStream{}
	feed.snapshot(r.Context(), s, w, flusher)
	for {
		select {
		case <-r.Context().Done():
			return
		case frame, ok := <-frames:
			if !ok {
				return
			}
			s.forward(w, flusher, frame, stream)
		case note := <-feed.notes:
			feed.write(w, flusher, note)
		}
	}
}

// eventStream is what one connection has already been sent, so a frame that
// says nothing new is not sent again.
type eventStream struct {
	orchestrator []byte
}

// orchestratorIdentity is the part of a task list a reader can see change.
// `at` is left out on purpose: it moves on every build by construction, and
// comparing it would make every upstream delta look like news — which is the
// Swift app's measured 234 KB to advance a clock (SnapshotBroadcastIdentity).
func orchestratorIdentity(list contract.TaskList) []byte {
	body, err := json.Marshal(struct {
		Tasks any
		Page  any
		Store any
	}{list.Tasks, list.Page, list.Store})
	if err != nil {
		return nil
	}
	return body
}

// forward writes one complete frame, substituting the payloads this daemon owns.
func (s *Server) forward(w http.ResponseWriter, flusher http.Flusher, frame []string, stream *eventStream) {
	if len(frame) == 0 {
		return
	}
	name, id := "", ""
	for _, line := range frame {
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "id: "):
			id = strings.TrimPrefix(line, "id: ")
		}
	}

	if name == "sessions" {
		// Upstream's own id is kept so the counter stays continuous for the
		// client. Nothing reads it for resume, but a stream that renumbers
		// itself mid-flight is a lie about being one stream.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		payload, err := json.Marshal(s.sessionsPayload(ctx))
		cancel()
		if err == nil {
			fmt.Fprintf(w, "event: sessions\nid: %s\ndata: %s\n\n", id, payload)
			flusher.Flush()
			return
		}
		// A reading we could not take is not a reason to drop the frame: the
		// upstream one is still true, just not ours.
	}

	if name == "orchestrator" || name == "orchestrator-delta" {
		// The task list is this daemon's too: the Swift app's rows as read from
		// its store, joined by this daemon's own. A delta against upstream's
		// base would be applied to our rows, so both are replaced by a whole
		// list, which is what a delta may always be replaced with.
		//
		// And a whole list that says what this connection was last told is not
		// sent at all. Upstream emits a delta whenever any of its own rows
		// moves — including the clocks the Swift app writes on every executor
		// reading — and replacing each one with the full list turned every
		// such tick into a frame (docs/broker-design.md §6.2). An identity that
		// cannot be computed is sent: not shown to be a duplicate is not the
		// same as shown to be one.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		list, err := s.tasksPayload(ctx, 0, 50)
		cancel()
		if err == nil {
			identity := orchestratorIdentity(list)
			if identity != nil && stream != nil && bytes.Equal(identity, stream.orchestrator) {
				return
			}
			if payload, err := json.Marshal(list); err == nil {
				if stream != nil {
					stream.orchestrator = identity
				}
				fmt.Fprintf(w, "event: orchestrator\nid: %s\ndata: %s\n\n", id, payload)
				flusher.Flush()
				return
			}
		}
	}

	for _, line := range frame {
		fmt.Fprint(w, line, "\n")
	}
	fmt.Fprint(w, "\n")
	flusher.Flush()
}

func (s *Server) openUpstreamEvents(r *http.Request) (*http.Response, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d%s", s.cfg.UpstreamPort, r.URL.RequestURI())
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// The console authenticates itself; this carries its credentials through
	// rather than holding any of its own.
	for _, h := range []string{"Cookie", "Authorization", "Accept", "Last-Event-ID"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	client := &http.Client{Timeout: 0}
	return client.Do(req)
}

// The broker's progress notes, on the stream (docs/broker-design.md E4, #25).
//
// A child's note is never typed into its root's terminal: that would interrupt
// a root in the middle of the turn it is waiting in. It used to reach nobody
// at all — two shipped guides said a note would wake the root, and a child
// once asked its root a question that way, heard nothing, and decided alone.
// So a note goes where the root's person is looking instead: this stream, as
// an `orchestrator-progress` event. A child that must wake somebody has
// /notify.
//
// Each note's identity is its row in the store. A connection is sent the
// newest notes of every live task when it opens — a page that reconnects is
// level without asking, and never has to replay anything — and then each note
// as it is accepted, and never the same note twice.

// progressFeed is one connection's view of the broker's notes.
type progressFeed struct {
	notes  <-chan store.BrokerNote
	cancel func()
	// sent is the identities already written, bounded: notes are published
	// after their commit, so two notes can arrive out of order, and a
	// high-water mark would drop the earlier one.
	sent  map[int64]bool
	order []int64
}

const progressIdentitiesKept = 1024

// openProgress subscribes before anything is read, so a note accepted while
// the snapshot is being built is either in the snapshot or on the channel —
// possibly both, which the identity check absorbs — and never in neither.
func (s *Server) openProgress() *progressFeed {
	f := &progressFeed{sent: map[int64]bool{}}
	if s.broker == nil {
		ch := make(chan store.BrokerNote)
		f.notes, f.cancel = ch, func() {}
		return f
	}
	f.notes, f.cancel = s.broker.SubscribeProgress()
	return f
}

func (f *progressFeed) close() { f.cancel() }

// snapshot writes the newest notes of every live task, as one frame, when
// there are any.
func (f *progressFeed) snapshot(ctx context.Context, s *Server, w http.ResponseWriter, flusher http.Flusher) {
	if s.broker == nil {
		return
	}
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	notes, err := s.broker.RecentProgress(readCtx)
	if err != nil || len(notes) == 0 {
		return
	}
	f.emit(w, flusher, notes)
}

// write sends one note, unless this connection has already been sent it.
func (f *progressFeed) write(w http.ResponseWriter, flusher http.Flusher, n store.BrokerNote) {
	f.emit(w, flusher, []store.BrokerNote{n})
}

func (f *progressFeed) emit(w http.ResponseWriter, flusher http.Flusher, notes []store.BrokerNote) {
	frame := contract.BrokerProgressFrame{Notes: []contract.BrokerProgressEvent{}}
	for _, n := range notes {
		if f.sent[n.Seq] {
			continue
		}
		f.remember(n.Seq)
		frame.Notes = append(frame.Notes, contract.BrokerProgressEvent{
			Seq: n.Seq, TaskID: n.TaskID, Note: n.Note, At: n.At.Unix(),
		})
	}
	if len(frame.Notes) == 0 {
		return
	}
	payload, err := json.Marshal(frame)
	if err != nil {
		return
	}
	// No `id:` line. The stream's ids are the session scan's generation, and
	// a second numbering on the same stream would make Last-Event-ID mean two
	// things; the snapshot on reconnect is what makes a page level instead.
	fmt.Fprintf(w, "event: orchestrator-progress\ndata: %s\n\n", payload)
	flusher.Flush()
}

func (f *progressFeed) remember(seq int64) {
	f.sent[seq] = true
	f.order = append(f.order, seq)
	if len(f.order) > progressIdentitiesKept {
		delete(f.sent, f.order[0])
		f.order = f.order[1:]
	}
}
