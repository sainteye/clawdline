package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// projectFixture lays out a repository, a linked worktree of it outside its
// tree, a subdirectory, and an unrelated repository, the way git writes them.
func projectFixture(t *testing.T) (repo, worktree, sub, other string) {
	t.Helper()
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	worktree = filepath.Join(root, "scratch", "feature")
	sub = filepath.Join(repo, "web", "console")
	other = filepath.Join(root, "other")
	admin := filepath.Join(repo, ".git", "worktrees", "feature")
	for _, dir := range []string{admin, worktree, sub, filepath.Join(other, ".git")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, text string) {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(worktree, ".git"), "gitdir: "+admin+"\n")
	write(filepath.Join(admin, "commondir"), "../..\n")
	return repo, worktree, sub, other
}

// The assignment dialog offers a Session by `project_key`; the claim accepts
// it by sessionInProject. Both must give the same answer for every place a
// Session can be working in, or the dialog offers what the claim refuses.
func TestTheSessionListsProjectKeyIsTheClaimsAnswer(t *testing.T) {
	repo, worktree, sub, other := projectFixture(t)
	want, ok := sessionProjectKey(repo)
	if !ok {
		t.Fatal("the repository has no key")
	}
	item := work.ItemV2{ProjectPath: want}
	var cache projectKeyCache
	for _, c := range []struct {
		cwd    string
		inside bool
	}{{repo, true}, {worktree, true}, {sub, true}, {other, false}} {
		key, ok := cache.key(c.cwd)
		if !ok {
			t.Fatalf("%s: no key", c.cwd)
		}
		claim := sessionInProject(session.Session{CWD: c.cwd}, item)
		if (key == want) != claim || claim != c.inside {
			t.Errorf("%s: list key %q (project %q), claim accepts %v, want %v", c.cwd, key, want, claim, c.inside)
		}
	}
	if key, ok := cache.key(""); ok || key != "" {
		t.Errorf("an unread cwd has a key: %q", key)
	}
}

// What the console and Cloud read: the full list and the machine list both
// carry the key beside the cwd.
func TestBothSessionListsCarryTheProjectKey(t *testing.T) {
	_, worktree, _, _ := projectFixture(t)
	want, _ := sessionProjectKey(worktree)
	item := session.Session{ID: "%7", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
		State: session.StateIdle, CWD: worktree, ConversationID: "conversation-7"}
	s := paneServer(t, &pane{s: item})
	s.cfg = config.Config{Dir: filepath.Join(t.TempDir(), "clawdline-next")}
	for _, path := range []string{"/v1/sessions", "/v1/sessions?parts=list"} {
		rec := httptest.NewRecorder()
		s.sessions(rec, httptest.NewRequest(http.MethodGet, path, nil))
		var payload struct {
			Sessions []map[string]any `json:"sessions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil || len(payload.Sessions) != 1 {
			t.Fatalf("%s: %v %s", path, err, rec.Body)
		}
		if got := payload.Sessions[0]["project_key"]; got != want || want == worktree {
			t.Errorf("%s: project_key %v, want the repository %q", path, got, want)
		}
	}
}

func TestProjectKeysAreHeldForAWhileAndBounded(t *testing.T) {
	repo, _, _, _ := projectFixture(t)
	at := time.Unix(1_000_000, 0)
	cache := projectKeyCache{now: func() time.Time { return at }}
	nested := filepath.Join(repo, "web", "console")
	first, _ := cache.key(nested)
	// The repository marker goes away: a held answer stands, a stale one is read again.
	if err := os.RemoveAll(filepath.Join(repo, ".git")); err != nil {
		t.Fatal(err)
	}
	if again, _ := cache.key(nested); again != first {
		t.Fatalf("a held answer was read again: %q then %q", first, again)
	}
	at = at.Add(projectKeyFresh)
	if fresh, _ := cache.key(nested); fresh == first {
		t.Fatalf("an answer older than %v was not read again: %q", projectKeyFresh, fresh)
	}
	for i := 0; i < ProjectKeyCacheLimit+5; i++ {
		at = at.Add(time.Millisecond)
		cache.key(fmt.Sprintf("/nowhere/%d", i))
	}
	reading := cache.Reading()
	if reading.Used != ProjectKeyCacheLimit || reading.Counters.Evicted != 6 {
		t.Fatalf("held %d, evicted %d; want %d held and 6 let go", reading.Used, reading.Counters.Evicted, ProjectKeyCacheLimit)
	}
	if _, held := cache.entries[nested]; held {
		t.Fatal("the answer read longest ago was kept past the limit")
	}
}

func TestSessionKeyCacheDoesNotInspectPersonalDirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var cache projectKeyCache
	for _, name := range []string{"Desktop", "Documents", "Downloads", "Pictures", "Music", "Movies", "Library"} {
		if key, ok := cache.key(filepath.Join(home, name, "project")); ok || key != "" {
			t.Errorf("personal directory acquired a background key: %q", key)
		}
	}
	if len(cache.entries) != 0 {
		t.Fatal("personal directories reached the repository lookup cache")
	}
}
