package http

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// The Mac half of `web/console/src/cloud/write-path.e2e.ts`, on a loopback
// socket, so that the page's own code — the card, the press, the fingerprint,
// the relay seam — can be driven against this daemon's bridge and routes in one
// run. It is skipped unless asked for:
//
//	CLAWDLINE_WRITE_PATH_ADDR=127.0.0.1:<port> go test ./internal/transport/http -run TestServeTheWritePathForAPage
//
// The e2e file starts it itself. Only the relay is missing: the page's copied
// client is replaced by one that hands the plaintext it would have sealed to
// POST /command, which is exactly what this daemon decrypts it to.
//
//	POST /command  {"seq":n,"body":{…}}  → the Answer: status, code, read, payload
//	GET  /row      the session's row as /v1/sessions sends it (its menu)
//	GET  /transcript  the words typed so far, as user turns
//	POST /prompt   {"command":"…"} puts that permission prompt up; "" clears it
//	POST /lag      {"on":true} makes /transcript answer empty (a record not written yet)
//	GET  /acts     everything typed, in order
//	POST /stop
func TestServeTheWritePathForAPage(t *testing.T) {
	address := os.Getenv("CLAWDLINE_WRITE_PATH_ADDR")
	if address == "" {
		t.Skip("set CLAWDLINE_WRITE_PATH_ADDR=127.0.0.1:<port> to serve the write path for write-path.e2e.ts")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		t.Fatalf("this listens on 127.0.0.1 only, not %q", address)
	}
	p := waitingPane("%4", "")
	var mu sync.Mutex
	var typed []map[string]any
	lag := false
	// A prompt answered puts the next tool call's prompt up — the moment a
	// digit meant for the first can land on the second.
	p.onKey = func(p *pane, b []byte) {
		if string(b) == "\r" {
			p.show(prompt("rm -rf /", 1))
		}
	}
	s := paneServer(t, p)
	bridge := cloudBridge(s)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stop := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/command", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Seq  uint64         `json:"seq"`
			Body map[string]any `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		before := len(p.done())
		answer := bridge.Handle(r.Context(), sealed(t, in.Seq, in.Body))
		// What a send typed becomes a user turn, dated now, as a transcript
		// would record it.
		for _, act := range p.done()[before:] {
			if text, ok := strings.CutPrefix(act, "send:"); ok {
				mu.Lock()
				typed = append(typed, map[string]any{"role": "user", "text": text, "at": time.Now().Unix()})
				mu.Unlock()
			}
		}
		var payload any
		_ = json.Unmarshal(answer.Payload, &payload)
		reply(w, map[string]any{"status": answer.Status, "code": answer.Code, "read": answer.Name, "payload": payload})
	})
	mux.HandleFunc("/row", func(w http.ResponseWriter, r *http.Request) {
		inv := s.inventory.Read(r.Context())
		for _, item := range inv.Sessions {
			if item.ID == "%4" {
				reply(w, map[string]any{"id": item.ID, "state": item.State, "menu": wireMenu(item)})
				return
			}
		}
		http.Error(w, "no row", 404)
	})
	mux.HandleFunc("/transcript", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		entries := append([]map[string]any{}, typed...)
		if lag {
			entries = []map[string]any{}
		}
		reply(w, map[string]any{"id": "%4", "entries": entries, "signature": time.Now().String(), "evidence": "transcript"})
	})
	mux.HandleFunc("/prompt", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Command string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Command == "" {
			p.show("")
		} else {
			p.show(prompt(in.Command, 1))
		}
		reply(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/lag", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ On bool }
		_ = json.NewDecoder(r.Body).Decode(&in)
		mu.Lock()
		lag = in.On
		mu.Unlock()
		reply(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/acts", func(w http.ResponseWriter, r *http.Request) {
		// An empty list is a list: `null` would read as "could not say".
		reply(w, append([]string{}, p.done()...))
	})
	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"ok": true})
		once.Do(func() { close(stop) })
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Logf("serving the write path on %s", address)
	select {
	case <-stop:
	case <-time.After(5 * time.Minute):
		t.Error("nobody stopped the write path within five minutes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
