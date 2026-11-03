package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/githubissues"
)

// fakeIssues is GitHub's issues API for one repository.
type fakeIssues struct {
	mu     sync.Mutex
	issues map[int]map[string]any
}

func (f *fakeIssues) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		base := "/repos/octo/widgets/issues"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == base:
			var rows []map[string]any
			for _, i := range f.issues {
				rows = append(rows, i)
			}
			json.NewEncoder(w).Encode(rows)
		case r.Method == http.MethodPost && r.URL.Path == base:
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			n := len(f.issues) + 1
			body["number"], body["state"] = n, "open"
			body["html_url"] = "https://github.com/octo/widgets/issues/" + strconv.Itoa(n)
			f.issues[n] = body
			json.NewEncoder(w).Encode(body)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, base+"/"):
			n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, base+"/"))
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			f.issues[n]["state"], f.issues[n]["state_reason"] = body["state"], body["state_reason"]
			json.NewEncoder(w).Encode(f.issues[n])
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}
}

func gitRepoWithRemote(t *testing.T, dir, remote string) {
	t.Helper()
	os.RemoveAll(filepath.Join(dir, ".git"))
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

func TestASyncedItemOpensItsIssueAtImplementationAndClosesItWhenDone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	s, p, project := sessionItemServer(t)
	gitRepoWithRemote(t, p.s.CWD, "git@github.com:octo/widgets.git")

	fake := &fakeIssues{issues: map[int]map[string]any{}}
	gh := httptest.NewServer(fake.handler(t))
	defer gh.Close()
	client := githubissues.NewClient()
	client.HTTP, client.OAuthURL, client.APIURL = gh.Client(), gh.URL, gh.URL
	tokens := filepath.Join(t.TempDir(), "github")
	s.githubConn = githubissues.NewConnection(client, tokens)

	create := func(key string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, personWorkV2Request(http.MethodPost, "/v1/work/v2/items",
			`{"project_id":"`+project+`","kind":"issue","title":"Login fails","description":"Steps.","issue_sync":{"provider":"github"}}`, key))
		return rec
	}
	// Without an authorization nothing is written.
	if rec := create("sync-unauthorized"); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "issue_sync_authorization_required") {
		t.Fatalf("unauthorized create: %d %s", rec.Code, rec.Body)
	}
	if rows, _, err := s.workV2().List(context.Background(), "", "", "all", ""); err != nil || len(rows) != 0 {
		t.Fatalf("a refused create left %d items (%v)", len(rows), err)
	}

	if err := (githubissues.TokenStore{Dir: tokens}).Save(githubissues.Token{AccessToken: "tok", Login: "octocat"}); err != nil {
		t.Fatal(err)
	}
	rec := create("sync-authorized")
	var created struct {
		Item workV2ItemWire `json:"item"`
	}
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &created) != nil || created.Item.IssueSync == nil ||
		created.Item.IssueSync.State != "waiting" || created.Item.IssueSync.Repo != "octo/widgets" {
		t.Fatalf("authorized create: %d %s", rec.Code, rec.Body)
	}
	id := created.Item.ID
	pass := func() int {
		t.Helper()
		n, err := s.issueReconciler().RunOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := pass(); n != 0 || len(fake.issues) != 0 {
		t.Fatalf("an unstarted item made %d calls", n)
	}

	assigned, err := s.assignWorkV2(context.Background(), id, "local", created.Item.Version, "existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	phase := httptest.NewRecorder()
	s.workV2Route(phase, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items/"+id+"/phase",
		`{"session_id":"`+p.s.ConversationID+`","next":"implementing"}`, "sync-implementing"))
	if phase.Code != http.StatusOK {
		t.Fatalf("implementing: %d %s", phase.Code, phase.Body)
	}
	if n := pass(); n != 1 || len(fake.issues) != 1 || fake.issues[1]["title"] != "Login fails" ||
		!strings.Contains(fake.issues[1]["body"].(string), "clawdline:item:"+id) {
		t.Fatalf("implementation opened %v after %d calls", fake.issues, n)
	}
	// A pass after the issue is open owes nothing, and a lost answer would
	// find the issue by its marker rather than open another.
	if n := pass(); n != 0 {
		t.Fatalf("an open issue on a working item made %d calls", n)
	}
	read, _ := s.workV2().Item(context.Background(), id)
	if wire := s.workV2ItemOf(s.workV2Projects(context.Background()), read).IssueSync; wire == nil || wire.State != "open" || wire.ExternalKey != "#1" {
		t.Fatalf("card after opening: %+v", wire)
	}

	done := httptest.NewRecorder()
	s.workV2Route(done, personWorkV2Request(http.MethodPost, "/v1/work/v2/items/"+id+"/complete",
		`{"expected_version":`+strconv.FormatInt(read.Item.Version, 10)+`,"reason":"Fixed"}`, "sync-complete"))
	if done.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", done.Code, done.Body)
	}
	if n := pass(); n != 1 || fake.issues[1]["state"] != "closed" || fake.issues[1]["state_reason"] != "completed" {
		t.Fatalf("done left %v", fake.issues[1])
	}
	_ = assigned
}
