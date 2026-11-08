package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// The HTTP handlers, SQLite ledger and Git reads are real; the roster and
// archived records are synthetic. No live daemon, terminal or user store is used.
func brokerProjectionFixture(t testing.TB, count int) (*Server, string) {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "LC_ALL=C")
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git: %s %v", b, err)
		}
		return strings.TrimSpace(string(b))
	}
	run("init", "-q", "--initial-branch=main")
	run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "--allow-empty", "-m", "Initial fixture")
	base := run("rev-parse", "HEAD")
	state := t.TempDir()
	st, err := store.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	broker := &orchestrator.Broker{Store: st, Tasks: taskdir.New(state), Dir: state, Git: git.New()}
	ctx := context.Background()
	for i := 0; i < count; i++ {
		r := orchestrator.Record{Protocol: orchestrator.Protocol, ID: fmt.Sprintf("b9000000-0000-4000-8000-%012d", i), Title: "Archived fixture", Assistant: "claude", State: orchestrator.StateSuccess, CreatedAt: time.Unix(1700000000, 0), ProjectDir: repo, Repository: repo,
			Worktree: &orchestrator.Worktree{Repository: repo, Path: filepath.Join(state, "removed", fmt.Sprint(i)), Branch: "removed-delivery", Base: base, Head: base}, Landing: &orchestrator.Landing{State: orchestrator.LandingLanded, Target: "main"}}
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SaveBrokerTask(ctx, store.BrokerRow{ID: r.ID, Project: repo, Repository: repo, Assistant: r.Assistant, State: string(r.State), CreatedAt: r.CreatedAt, Record: raw}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := st.MarkReclaim(ctx, store.Reclaim{Task: r.ID, Subject: orchestrator.ReclaimWorktree, Outcome: orchestrator.ReclaimRemoved, LastAt: time.Now()}, nil); err != nil {
			t.Fatal(err)
		}
	}
	live := orchestrator.Record{Protocol: orchestrator.Protocol, ID: "b9100000-0000-4000-8000-000000000001", Title: "Outstanding fixture", Assistant: "claude", State: orchestrator.StateBriefed, ProjectDir: repo, Repository: repo, CreatedAt: time.Unix(1700000001, 0)}
	raw, _ := json.Marshal(live)
	if err := st.SaveBrokerTask(ctx, store.BrokerRow{ID: live.ID, Project: repo, Repository: repo, Assistant: live.Assistant, State: string(live.State), CreatedAt: live.CreatedAt, Record: raw}, nil); err != nil {
		t.Fatal(err)
	}
	if records, bad, err := broker.Records(ctx); err != nil || len(records) != count+1 || len(bad) != 0 {
		t.Fatalf("fixture records=%d unreadable=%d err=%v", len(records), len(bad), err)
	}
	return &Server{broker: broker, store: st}, repo
}

func BenchmarkBrokerHistoryHTTP(b *testing.B) {
	for _, route := range []string{"inflight", "inventory"} {
		b.Run(route, func(b *testing.B) {
			s, repo := brokerProjectionFixture(b, 128)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if route == "inflight" {
					s.brokerInflight(w, r)
				} else {
					s.brokerInventory(w, r)
				}
			}))
			defer server.Close()
			endpoint := server.URL + "?project=" + url.QueryEscape(repo)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resp, err := server.Client().Get(endpoint)
				if err != nil {
					b.Fatal(err)
				}
				raw, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil || resp.StatusCode != 200 {
					b.Fatalf("response=%d err=%v body=%s", resp.StatusCode, err, raw)
				}
				var body map[string]json.RawMessage
				if err := json.Unmarshal(raw, &body); err != nil {
					b.Fatal(err)
				}
				field := "inflight"
				if route == "inventory" {
					field = "live"
				}
				var rows []struct {
					ID   string `json:"id"`
					Task string `json:"task"`
				}
				if err := json.Unmarshal(body[field], &rows); err != nil || len(rows) != 1 {
					b.Fatalf("fixture membership: rows=%d err=%v", len(rows), err)
				}
			}
		})
	}
}

func TestInflightProjectResolutionAndAliases(t *testing.T) {
	s, repo := brokerProjectionFixture(t, 2)
	for _, query := range []string{"project=" + url.QueryEscape(repo), "project_dir=" + url.QueryEscape(repo)} {
		rec := httptest.NewRecorder()
		s.brokerInflight(rec, httptest.NewRequest("GET", "/?"+query, nil))
		var body struct {
			Repository string `json:"repository"`
			Inflight   []any  `json:"inflight"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != 200 || body.Repository != repo || len(body.Inflight) != 1 {
			t.Fatalf("query=%s code=%d body=%s err=%v", query, rec.Code, rec.Body.String(), err)
		}
	}
	for _, project := range []string{"", "relative", t.TempDir()} {
		rec := httptest.NewRecorder()
		s.brokerInflight(rec, httptest.NewRequest("GET", "/?project="+url.QueryEscape(project), nil))
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), `"bad_request"`) {
			t.Fatalf("invalid project response=%d %s", rec.Code, rec.Body.String())
		}
	}
}

// The repository-wide route must not build the full cleanup inventory just
// to resolve a project before asking for outstanding work.
func TestInflightHTTPReadsOnlyOutstandingWork(t *testing.T) {
	s, repo := brokerProjectionFixture(t, 8)
	trace := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	rec := httptest.NewRecorder()
	s.brokerInflight(rec, httptest.NewRequest("GET", "/?project="+url.QueryEscape(repo), nil))
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	commands := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		var e struct {
			Event string `json:"event"`
		}
		if json.Unmarshal(line, &e) == nil && e.Event == "start" {
			commands++
		}
	}
	if commands != 1 {
		t.Fatalf("ran %d Git commands; want only project resolution", commands)
	}
}
