package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/githubissues"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestCopilotGitHubChangesUseTheSessionRepositoryAndExactRange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "git@github.com:example/voice-fixture.git"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	var calls atomic.Int32
	var noRelease atomic.Bool
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		const target = "/repos/example/voice-fixture"
		commit := func(i int, message string) map[string]any {
			return map[string]any{
				"sha":    fmt.Sprintf("%040x", i),
				"commit": map[string]any{"message": message, "committer": map[string]string{"date": "2026-11-22T02:00:00Z"}},
			}
		}
		switch r.URL.Path {
		case target:
			json.NewEncoder(w).Encode(map[string]string{"default_branch": "trunk"})
		case target + "/releases/latest":
			if noRelease.Load() {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"tag_name": "v2.3.4", "published_at": "2026-11-21T12:00:00Z", "body": "unused release body"})
		case target + "/compare/refs/tags/v2.3.4...refs/heads/trunk":
			json.NewEncoder(w).Encode(map[string]any{"status": "ahead", "total_commits": 2, "files": []string{"unused patch"},
				"commits": []map[string]any{commit(1, "feat: add a voice report"), commit(2, "fix: read GitHub history\n\n"+strings.Repeat("字", 300))}})
		case target + "/commits":
			q := r.URL.Query()
			if q.Get("sha") != "trunk" || q.Get("since") != "2026-11-20T16:00:00Z" || q.Get("until") != "2026-11-22T15:59:59Z" {
				t.Errorf("GitHub received the wrong window: %v", q)
			}
			rows := make([]map[string]any, githubissues.ChangeRowsLimit+1)
			for i := range rows {
				rows[i] = commit(i+1, "Includes voice changes")
			}
			json.NewEncoder(w).Encode(rows)
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fixture.Close)
	client := githubissues.NewClient()
	client.HTTP, client.APIURL = fixture.Client(), fixture.URL
	row := session.Session{ID: "claude-remote:42:voice-changes", RemoteSessionID: "voice-changes", ConversationID: "conversation-a",
		PID: 42, Backend: session.BackendClaudeRemote, Assistant: session.AssistantClaude, CWD: repo}
	s := &Server{cfg: config.Config{Dir: t.TempDir()}, inventory: app.Inventory{Process: rcProcesses{row}}, claudeRemote: &questionRemote{},
		githubConn: githubissues.NewConnection(client, filepath.Join(t.TempDir(), "github"))}
	t.Cleanup(s.StopGitHub)
	base := copilotGitHubChangesPath + "?session=" + url.QueryEscape(row.ID)
	handler := s.Handler()
	local, err := s.gate().files.ReadLocalToken()
	if err != nil {
		t.Fatal(err)
	}
	read := func(query string) *httptest.ResponseRecorder {
		return getReport(handler, local, base+query, nil)
	}
	for _, c := range []struct {
		query, code string
		status      int
	}{
		{"&conversation=replaced&range=release", "copilot_github_session_changed", 409},
		{"&conversation=conversation-a&range=dates&since=today", "copilot_github_invalid_dates", 400},
	} {
		if res := read(c.query); res.Code != c.status || !strings.Contains(res.Body.String(), c.code) {
			t.Fatalf("refusal: %d %s", res.Code, res.Body)
		}
	}
	unpaired := getReport(handler, "", base+"&conversation=conversation-a&range=release", nil)
	if unpaired.Code != http.StatusUnauthorized || calls.Load() != 0 {
		t.Fatalf("unpaired or invalid query contacted GitHub: %d, %d requests", unpaired.Code, calls.Load())
	}
	release := read("&conversation=conversation-a&range=release")
	var report githubissues.GitHubChanges
	if err := json.Unmarshal(release.Body.Bytes(), &report); release.Code != 200 || err != nil || report.Release == nil ||
		report.Release.Tag != "v2.3.4" || !report.Complete || len(report.Commits) != 2 ||
		!strings.HasPrefix(report.Commits[0].Message, "fix:") || utf8.RuneCountInString(report.Commits[0].Message) != githubissues.ChangeEvidenceRunesLimit ||
		strings.Contains(release.Body.String(), "unused ") || strings.Contains(release.Body.String(), repo) {
		t.Fatalf("release: %d %s (%v)", release.Code, release.Body, err)
	}
	dates := read("&conversation=conversation-a&range=dates&since=2026-11-20T16:00:00Z&until=2026-11-22T15:59:59Z")
	report = githubissues.GitHubChanges{}
	if err := json.Unmarshal(dates.Body.Bytes(), &report); dates.Code != 200 || err != nil || report.Release != nil ||
		report.Complete || report.Total != nil || len(report.Commits) != githubissues.ChangeRowsLimit {
		t.Fatalf("dates: %d %+v (%v)", dates.Code, report, err)
	}
	noRelease.Store(true)
	missing := read("&conversation=conversation-a&range=release")
	if missing.Code != 404 || !strings.Contains(missing.Body.String(), "github_no_published_release") || calls.Load() != 7 {
		t.Fatalf("missing release: %d %s, %d calls", missing.Code, missing.Body, calls.Load())
	}
}
