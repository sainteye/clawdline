package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func TestIntentBoundsMatchTheCapacityRegister(t *testing.T) {
	if got := CapacityLimit(capacity.IntentRequestBytes); got != intentLimit {
		t.Fatalf("request bytes: route %d, register %d", intentLimit, got)
	}
	if got := CapacityLimit(capacity.IntentPlannerQueue); got != intentQueueLimit {
		t.Fatalf("planner queue: route %d, register %d", intentQueueLimit, got)
	}
	if got := time.Duration(CapacityLimit(capacity.IntentPlannerSeconds)) * time.Second; got != intentTimeLimit {
		t.Fatalf("planner timeout: route %s, register %s", intentTimeLimit, got)
	}
	if got := time.Duration(CapacityLimit(capacity.IntentCloudWaitSeconds)) * time.Second; got != intentCloudWaitLimit {
		t.Fatalf("Cloud wait: console %s, register %s", intentCloudWaitLimit, got)
	}
}

func intentCall(s *Server, key, body string, caps auth.Caps) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/intents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{
		verdict: auth.Verdict{Allowed: true, Device: "test-device", Caps: caps},
	}))
	rec := httptest.NewRecorder()
	s.intentRoute(rec, req)
	return rec
}

func TestIntentTurnsOneSentenceIntoAReceiptedDraft(t *testing.T) {
	s := voiceServer(t)
	s.intentContext = func(context.Context) ([]planner.Place, []string) {
		return []planner.Place{{ID: "project-one", Label: "One", Path: "/one"}}, []string{"claude", "codex"}
	}
	runs := 0
	s.planIntent = func(_ context.Context, text string, places []planner.Place, assistants []string) (contract.IntentDraft, error) {
		runs++
		if text != "start the review" || len(places) != 1 || strings.Join(assistants, ",") != "claude,codex" {
			t.Fatalf("input = %q %#v %#v", text, places, assistants)
		}
		id := places[0].ID
		return contract.IntentDraft{PlaceID: &id, Assistant: "claude", Model: "opus", Instructions: "Review it",
			Title: "Review", Confidence: .9, Kind: "session", Days: []string{}}, nil
	}
	for i := 0; i < 2; i++ {
		rec := intentCall(s, "one-press", `{"text":"  start the review  "}`, auth.NewCaps(auth.Read, auth.Send))
		if rec.Code != http.StatusOK {
			t.Fatalf("pass %d: %d %s", i, rec.Code, rec.Body)
		}
		var got contract.IntentResult
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Draft.PlaceID == nil || *got.Draft.PlaceID != "project-one" {
			t.Fatalf("result = %s (%v)", rec.Body, err)
		}
	}
	if runs != 1 {
		t.Fatalf("planner ran %d times", runs)
	}
}

func TestIntentRefusesBeforeSpendingAPlannerTurn(t *testing.T) {
	s := voiceServer(t)
	runs := 0
	s.planIntent = func(context.Context, string, []planner.Place, []string) (contract.IntentDraft, error) {
		runs++
		return contract.IntentDraft{}, nil
	}
	s.intentContext = func(context.Context) ([]planner.Place, []string) { return nil, nil }
	send := auth.NewCaps(auth.Read, auth.Send)
	cases := []struct {
		name, key, body string
		caps            auth.Caps
		status          int
	}{
		{"reader", "a", `{"text":"hello"}`, auth.NewCaps(auth.Read), http.StatusForbidden},
		{"no key", "", `{"text":"hello"}`, send, http.StatusBadRequest},
		{"empty", "b", `{"text":"  "}`, send, http.StatusBadRequest},
		{"not json", "c", `{`, send, http.StatusBadRequest},
		{"too long", "d", `{"text":"` + strings.Repeat("x", intentLimit+1) + `"}`, send, http.StatusBadRequest},
	}
	for _, tc := range cases {
		if rec := intentCall(s, tc.key, tc.body, tc.caps); rec.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.name, rec.Code, rec.Body)
		}
	}
	if runs != 0 {
		t.Fatalf("planner ran %d times", runs)
	}
}

func TestIntentDoesNotFileTemporaryFailures(t *testing.T) {
	s := voiceServer(t)
	s.intentContext = func(context.Context) ([]planner.Place, []string) { return nil, nil }
	runs := 0
	s.planIntent = func(context.Context, string, []planner.Place, []string) (contract.IntentDraft, error) {
		runs++
		if runs == 1 {
			return contract.IntentDraft{}, planner.ErrNoPlanner
		}
		if runs == 2 {
			return contract.IntentDraft{}, errors.New("bad answer")
		}
		return contract.IntentDraft{Assistant: "claude", Kind: "session", Days: []string{}}, nil
	}
	send := auth.NewCaps(auth.Read, auth.Send)
	want := []int{http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusOK}
	for i, status := range want {
		rec := intentCall(s, "retry", `{"text":"hello"}`, send)
		if rec.Code != status {
			t.Fatalf("pass %d: %d %s", i, rec.Code, rec.Body)
		}
	}
}

func TestIntentQueueRefusesTheThirdTurn(t *testing.T) {
	s := voiceServer(t)
	s.intentQueued = intentQueueLimit
	s.intentContext = func(context.Context) ([]planner.Place, []string) { return nil, nil }
	s.planIntent = func(context.Context, string, []planner.Place, []string) (contract.IntentDraft, error) {
		t.Fatal("a full queue ran the planner")
		return contract.IntentDraft{}, nil
	}
	rec := intentCall(s, "busy", `{"text":"hello"}`, auth.NewCaps(auth.Read, auth.Send))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("busy = %d %s", rec.Code, rec.Body)
	}
}

func TestIntentRunsOnePlannerAtATimeWhileOneWaits(t *testing.T) {
	s := voiceServer(t)
	s.intentContext = func(context.Context) ([]planner.Place, []string) { return nil, nil }
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var active atomic.Int32
	var highest atomic.Int32
	s.planIntent = func(context.Context, string, []planner.Place, []string) (contract.IntentDraft, error) {
		now := active.Add(1)
		for old := highest.Load(); now > old && !highest.CompareAndSwap(old, now); old = highest.Load() {
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		return contract.IntentDraft{Assistant: "claude", Kind: "session", Days: []string{}}, nil
	}

	answers := make(chan *httptest.ResponseRecorder, 2)
	send := auth.NewCaps(auth.Read, auth.Send)
	go func() { answers <- intentCall(s, "serial-one", `{"text":"one"}`, send) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("the first planner did not start")
	}
	go func() { answers <- intentCall(s, "serial-two", `{"text":"two"}`, send) }()
	deadline := time.Now().Add(time.Second)
	for {
		s.intentMu.Lock()
		queued := s.intentQueued
		s.intentMu.Unlock()
		if queued == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the second planner was not admitted to wait")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-entered:
		t.Fatal("two planner CLIs ran together")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	for range 2 {
		if rec := <-answers; rec.Code != http.StatusOK {
			t.Fatalf("planner answer = %d %s", rec.Code, rec.Body)
		}
	}
	if got := highest.Load(); got != 1 {
		t.Fatalf("highest concurrent planners = %d", got)
	}
}
