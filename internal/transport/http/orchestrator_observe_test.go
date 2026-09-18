package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// The open health route says the beat stopped, within three ticks of it
// stopping — the one thing the Swift app's could not say (§2.6: `ok: true`
// with the heartbeat stopped).
func TestHealthSaysTheBeatStalled(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	release := make(chan struct{})
	defer close(release)
	b := &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir,
		Fault: func(pass int64) {
			if pass == 2 {
				<-release
			}
		}}
	s := &Server{broker: b, store: st}
	read := func() contract.Health {
		rec := httptest.NewRecorder()
		s.health(rec, httptest.NewRequest("GET", "/v1/health", nil))
		var h contract.Health
		if err := json.Unmarshal(rec.Body.Bytes(), &h); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 {
			t.Fatalf("health answered %d; the answer is the JSON, not the status", rec.Code)
		}
		return h
	}
	if h := read(); !h.OK || h.Reason != "" {
		t.Fatalf("a daemon with no beat started reads %+v", h)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx, 20*time.Millisecond, nil)
	deadline := time.Now().Add(3 * time.Second)
	for {
		h := read()
		if !h.OK {
			if h.Reason != reasonBeatStalled {
				t.Fatalf("health is not ok for the wrong reason: %+v", h)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("health never said the beat stalled")
		}
		time.Sleep(10 * time.Millisecond)
	}
	diag := s.brokerDiagnostics(ctx)
	if !diag.Beat.Stalled || !diag.Beat.InPass || diag.Beat.Passes != 2 || diag.Store.Status != contract.BrokerStoreStatusReady {
		t.Fatalf("diagnostics = %+v", diag.Beat)
	}
}

// A connection is sent each note once: the snapshot and the live feed can
// both carry a note accepted while the connection opened, and notes can
// arrive out of order, so identity is the note's row and not a high-water
// mark.
func TestAProgressNoteIsSentOncePerConnection(t *testing.T) {
	rec := httptest.NewRecorder()
	f := &progressFeed{sent: map[int64]bool{}}
	at := time.Unix(1789700000, 0)
	f.emit(rec, rec, []store.BrokerNote{{Seq: 1, TaskID: "t", Note: "one", At: at}, {Seq: 3, TaskID: "t", Note: "three", At: at}})
	f.write(rec, rec, store.BrokerNote{Seq: 3, TaskID: "t", Note: "three", At: at})
	f.write(rec, rec, store.BrokerNote{Seq: 2, TaskID: "u", Note: "two", At: at})
	f.write(rec, rec, store.BrokerNote{Seq: 1, TaskID: "t", Note: "one", At: at})
	frames := strings.Split(strings.TrimSpace(rec.Body.String()), "\n\n")
	if len(frames) != 2 {
		t.Fatalf("wrote %d frames, want 2:\n%s", len(frames), rec.Body.String())
	}
	var seqs []int64
	for _, frame := range frames {
		if !strings.HasPrefix(frame, "event: orchestrator-progress\ndata: ") {
			t.Fatalf("frame shape: %q", frame)
		}
		var body contract.BrokerProgressFrame
		if err := json.Unmarshal([]byte(strings.TrimPrefix(frame, "event: orchestrator-progress\ndata: ")), &body); err != nil {
			t.Fatal(err)
		}
		for _, n := range body.Notes {
			seqs = append(seqs, n.Seq)
		}
	}
	if len(seqs) != 3 || seqs[0] != 1 || seqs[1] != 3 || seqs[2] != 2 {
		t.Fatalf("sent notes %v, want [1 3 2]", seqs)
	}
}

// Two task lists that differ only in when they were built are the same frame.
func TestOrchestratorIdentityIgnoresTheClock(t *testing.T) {
	a := contract.TaskList{At: 1, Tasks: []contract.TaskRow{{ID: "x", State: "briefed"}}}
	b := a
	b.At = 2
	if !bytes.Equal(orchestratorIdentity(a), orchestratorIdentity(b)) {
		t.Fatal("a list that moved only its clock has a new identity")
	}
	c := contract.TaskList{At: 1, Tasks: []contract.TaskRow{{ID: "x", State: "success"}}}
	if bytes.Equal(orchestratorIdentity(a), orchestratorIdentity(c)) {
		t.Fatal("a state change kept the old identity")
	}
}
