package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestDeliveredChildCanAcceptThroughHTTPAfterWaitingForApproval(t *testing.T) {
	const id = "d1a10008-0000-4000-8000-000000000008"
	p := waitingPane("%71", "Bash command\nclawdline task accept\n\nDo you want to proceed?\n❯ 1. Yes\n  2. No\nEnter to confirm · Esc to cancel\n")
	s := paneServer(t, p)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	s.broker.Clock = func() time.Time { return now }
	s.broker.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Complete: true, Sources: map[string]bool{"tmux": true}, Sessions: []session.Session{p.s}}
	}
	s.broker.Screen = func(context.Context, string) (string, bool) { return p.screen, true }
	typed := 0
	s.broker.Type = func(context.Context, string, string) error { typed++; return nil }
	r := orchestrator.Record{Protocol: orchestrator.Protocol, ID: id, Kind: "custom", Assistant: "claude",
		Title: "Wait for tool approval", State: orchestrator.StateSpawning, Claims: []string{},
		ProjectDir: t.TempDir(), CreatedAt: now, SpawnedAt: now, TimeoutMinutes: 60,
		ChildTerminalID: p.s.ID, ChildBackend: "tmux"}
	if _, err := s.store.CreateBrokerTask(context.Background(), store.BrokerRow{ID: id, Project: r.ProjectDir,
		Assistant: r.Assistant, State: string(r.State), CreatedAt: now, UpdatedAt: now,
		SecretHash: orchestrator.HashSecret("fixture-secret"), Record: r.Encode()}, nil); err != nil {
		t.Fatal(err)
	}
	// Use real loopback HTTP and the production lifecycle handlers. The task
	// secret is checked by acceptance; no native provider or model is started.
	server := httptest.NewServer(http.HandlerFunc(s.orchestratorTaskRoute))
	defer server.Close()
	now = now.Add(5 * time.Minute)
	s.broker.Pass(context.Background())
	check := func(want string) {
		t.Helper()
		response, err := server.Client().Get(server.URL + "/v1/orchestrator/tasks/" + id)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var detail struct {
			Task struct {
				State string `json:"state"`
			} `json:"task"`
		}
		if err := json.NewDecoder(response.Body).Decode(&detail); err != nil || response.StatusCode != 200 || detail.Task.State != want {
			t.Fatalf("task HTTP read: status=%d state=%q error=%v; want %q", response.StatusCode, detail.Task.State, err, want)
		}
	}
	check("spawning")
	for _, secret := range []string{"wrong-secret", "fixture-secret"} {
		req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/orchestrator/tasks/"+id+"/accepted", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Clawdline-Task-Secret", secret)
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if secret == "wrong-secret" {
			if response.StatusCode < 400 {
				t.Fatalf("wrong secret accepted: %d", response.StatusCode)
			}
			check("spawning")
		} else if response.StatusCode != http.StatusOK {
			t.Fatalf("signed acceptance: %d", response.StatusCode)
		}
	}
	check("briefed")
	if typed != 0 || p.closeCalls != 0 {
		t.Fatalf("broker typed %d times or closed %d times", typed, p.closeCalls)
	}
}
