package http

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// events serves the console's one stream.
//
// There are two ways to do that and the difference is which app is running.
// Standing alone, this daemon publishes its own (see ownEvents). Standing in
// front of the Swift app, it owns half of the stream and edits the rest. The stream carries two
// whole-snapshot payloads — `sessions` and `orchestrator` — and this daemon has
// a reading for the first and none for the second, so upstream's stream is
// consumed frame by frame, `sessions` frames are replaced with ours, and
// everything else passes through untouched.
//
// Replacing in place rather than publishing on our own clock keeps the update
// rhythm exactly as it was: upstream still decides when a session update is
// due, and only the content changes. It also means the client never sees two
// sources for one payload, which is what makes the generation counter it
// enforces meaningful.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if !ownsSessions() {
		s.proxy.ServeHTTP(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.proxy.ServeHTTP(w, r)
		return
	}

	// With nothing behind this daemon there is nothing to splice into, so the
	// stream is published rather than edited. See ownEvents for what that costs.
	if standalone() {
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
				s.forward(w, flusher, frame)
				frame = frame[:0]
			}
		}
		if err != nil {
			if err != io.EOF {
				return
			}
			return
		}
		select {
		case <-r.Context().Done():
			return
		default:
		}
	}
}

// forward writes one complete frame, substituting the payloads this daemon owns.
func (s *Server) forward(w http.ResponseWriter, flusher http.Flusher, frame []string) {
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
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		list, err := s.tasksPayload(ctx, 0, 50)
		cancel()
		if err == nil {
			if payload, err := json.Marshal(list); err == nil {
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
