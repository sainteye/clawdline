package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// streamTick is how often this daemon looks at the machine for the stream.
//
// It is a decision this daemon now has to make for itself. While the stream was
// spliced from the Swift app, upstream decided when a session update was due
// and only the content changed; without it, the rhythm is ours.
func streamTick() time.Duration {
	if v := os.Getenv("CLAWDLINE_NEXT_STREAM"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 250*time.Millisecond {
			return d
		}
	}
	return 2 * time.Second
}

// heartbeat is how often the stream says something even when nothing changed.
//
// A stream that goes quiet is indistinguishable from a stream that died, to
// every proxy between here and the browser and to the browser itself. The
// comment costs two bytes and removes that ambiguity.
const heartbeat = 15 * time.Second

// ownEvents publishes this daemon's own stream, with no upstream behind it.
//
// This is what `standalone` was always claiming and could not do: until now
// /v1/events consumed the Swift app's stream unconditionally, so the clock, the
// authentication and the `orchestrator` payload were all upstream's. A console
// pointed at this daemon was reading the other app's heartbeat and reporting it
// as this one's.
//
// What is lost by not splicing is the `orchestrator` frame, which this daemon
// has no reading for. It is absent rather than faked: a client that needs it
// can tell, which is the whole reason absence is modelled here at all.
func (s *Server) ownEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeRefusal(w, http.StatusInternalServerError, "no_flush",
			"this connection cannot stream")
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Nothing should buffer an event stream on the way to the client. Local
	// proxies and reverse proxies both honour this, and without it the console
	// looks frozen for as long as the buffer takes to fill.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ticker := time.NewTicker(streamTick())
	defer ticker.Stop()
	beat := time.NewTicker(heartbeat)
	defer beat.Stop()

	// last is the part of the payload that carries information, so an unchanged
	// machine does not wake the client. The timestamp and the counters are
	// excluded deliberately: they differ on every read by construction, and
	// comparing them would make every tick look like news.
	var last []byte
	send := func() {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		snapshot := s.sessionsPayload(ctx)
		cancel()
		body, err := json.Marshal(snapshot.Sessions)
		if err != nil {
			return
		}
		if bytes.Equal(body, last) {
			return
		}
		last = body
		payload, err := json.Marshal(snapshot)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: sessions\nid: %d\ndata: %s\n\n", snapshot.Scan.Generation, payload)
		flusher.Flush()
	}

	// The first frame goes out at once. A client that had to wait one tick to
	// see anything would show an empty fleet for two seconds, and an empty
	// fleet is a statement.
	send()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			send()
		case <-beat.C:
			fmt.Fprint(w, ": still here\n\n")
			flusher.Flush()
		}
	}
}
