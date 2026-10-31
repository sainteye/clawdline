package githubissues

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/git"
)

// fakeGitHub answers the three endpoints the connection uses. polls is the
// sequence of answers to successive device polls; the last one repeats.
type fakeGitHub struct {
	mu        sync.Mutex
	polls     []string
	pollCount int
	starts    int32
	refreshes int32
	refresh   string // "ok" or an OAuth error code
	now       time.Time
}

func (f *fakeGitHub) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login/device/code":
			atomic.AddInt32(&f.starts, 1)
			if r.FormValue("client_id") == "" || r.FormValue("scope") != Scope {
				t.Errorf("device request %v", r.Form)
			}
			json.NewEncoder(w).Encode(map[string]any{"device_code": "dev-secret", "user_code": "ABCD-1234",
				"verification_uri": "https://github.com/login/device", "expires_in": 900, "interval": 5})
		case "/login/oauth/access_token":
			if r.FormValue("client_secret") != "" {
				t.Errorf("a client secret was sent")
			}
			if r.FormValue("grant_type") == "refresh_token" {
				atomic.AddInt32(&f.refreshes, 1)
				time.Sleep(10 * time.Millisecond)
				if f.refresh != "ok" {
					json.NewEncoder(w).Encode(map[string]any{"error": f.refresh})
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"access_token": "access-2", "refresh_token": "refresh-2",
					"expires_in": 28800, "refresh_token_expires_in": 15897600, "scope": "repo"})
				return
			}
			f.mu.Lock()
			answer := f.polls[min(f.pollCount, len(f.polls)-1)]
			f.pollCount++
			f.mu.Unlock()
			if answer != "token" {
				json.NewEncoder(w).Encode(map[string]any{"error": answer})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "access-1", "refresh_token": "refresh-1",
				"expires_in": 28800, "refresh_token_expires_in": 15897600, "scope": "repo"})
		case "/user":
			if r.Header.Get("Authorization") != "Bearer access-1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"login": "octocat"})
		default:
			http.NotFound(w, r)
		}
	})
}

func newConnection(t *testing.T, f *fakeGitHub) (*Connection, chan time.Duration) {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	f.now = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	client := &Client{HTTP: srv.Client(), OAuthURL: srv.URL, APIURL: srv.URL, ClientID: ClientID,
		Now: func() time.Time { return f.now }}
	c := NewConnection(client, filepath.Join(t.TempDir(), "integrations", "github"))
	t.Cleanup(c.Close)
	waits := make(chan time.Duration, 64)
	c.Sleep = func(ctx context.Context, d time.Duration) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		waits <- d
		return nil
	}
	return c, waits
}

func waitFor(t *testing.T, c *Connection, want State) Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s := c.Status(); s.State == want {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("state %q never reached; last %+v", want, c.Status())
	return Status{}
}

func TestADeviceFlowConnectsAfterThePersonEntersTheCode(t *testing.T) {
	f := &fakeGitHub{polls: []string{"authorization_pending", "slow_down", "token"}}
	c, waits := newConnection(t, f)
	s, err := c.Start(context.Background())
	if err != nil || s.State != StatePending || s.UserCode != "ABCD-1234" || s.VerificationURI == "" {
		t.Fatalf("start: %+v %v", s, err)
	}
	got := waitFor(t, c, StateConnected)
	if got.Login != "octocat" {
		t.Fatalf("connected as %q", got.Login)
	}
	if !c.Authorized() {
		t.Fatal("a connected machine is not authorized")
	}
	// slow_down adds five seconds to every later wait.
	first, second, third := <-waits, <-waits, <-waits
	if first != 5*time.Second || second != 5*time.Second || third != 10*time.Second {
		t.Fatalf("waits %v %v %v", first, second, third)
	}
	raw, _ := json.Marshal(got)
	for _, secret := range []string{"access-1", "refresh-1", "dev-secret"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("status carries a secret: %s", raw)
		}
	}
	tok, err := c.AccessToken(context.Background())
	if err != nil || tok != "access-1" {
		t.Fatalf("access token %q %v", tok, err)
	}
}

func TestAFlowThatEndsWithoutATokenSaysHow(t *testing.T) {
	for poll, want := range map[string]State{"access_denied": StateDenied, "expired_token": StateExpired,
		"device_flow_disabled": StateFailed} {
		t.Run(poll, func(t *testing.T) {
			c, _ := newConnection(t, &fakeGitHub{polls: []string{"authorization_pending", poll}})
			if _, err := c.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			s := waitFor(t, c, want)
			if s.Error != poll || c.Authorized() {
				t.Fatalf("%+v authorized=%v", s, c.Authorized())
			}
		})
	}
}

func TestOnlyOneFlowWaitsAtATime(t *testing.T) {
	f := &fakeGitHub{polls: []string{"authorization_pending"}}
	c, _ := newConnection(t, f)
	a, _ := c.Start(context.Background())
	b, _ := c.Start(context.Background())
	if a.UserCode != b.UserCode || atomic.LoadInt32(&f.starts) != 1 {
		t.Fatalf("two starts asked GitHub %d times", f.starts)
	}
}

func TestDisconnectingDuringAFlowKeepsItsTokenOut(t *testing.T) {
	c, _ := newConnection(t, &fakeGitHub{polls: []string{"authorization_pending"}})
	if _, err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	flow := c.gen
	c.mu.Unlock()
	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	// The flow's poll got its token just before the person disconnected and
	// saves it just after: the save must not bring the connection back.
	if err := c.saveIfCurrent(flow, Token{AccessToken: "late", Login: "octocat"}); err != nil {
		t.Fatal(err)
	}
	if s := c.Status(); s.State != StateDisconnected {
		t.Fatalf("a disconnected flow saved its token: %+v", s)
	}
}

func connected(t *testing.T, f *fakeGitHub) *Connection {
	t.Helper()
	f.polls = []string{"token"}
	c, _ := newConnection(t, f)
	if _, err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, StateConnected)
	return c
}

func TestAnExpiringTokenIsRefreshedOnceAndSavedBeforeUse(t *testing.T) {
	f := &fakeGitHub{refresh: "ok"}
	c := connected(t, f)
	f.now = f.now.Add(8*time.Hour - time.Minute)
	var wg sync.WaitGroup
	got := make([]string, 4)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i], _ = c.AccessToken(context.Background())
		}(i)
	}
	wg.Wait()
	for _, tok := range got {
		if tok != "access-2" {
			t.Fatalf("tokens %v", got)
		}
	}
	if n := atomic.LoadInt32(&f.refreshes); n != 1 {
		t.Fatalf("the refresh token was spent %d times", n)
	}
	saved, err := c.store.Load()
	if err != nil || saved.AccessToken != "access-2" || saved.RefreshToken != "refresh-2" || saved.Login != "octocat" {
		t.Fatalf("saved %+v %v", saved, err)
	}
}

func TestARefusedRefreshAsksForANewAuthorization(t *testing.T) {
	f := &fakeGitHub{refresh: "bad_refresh_token"}
	c := connected(t, f)
	f.now = f.now.Add(8 * time.Hour)
	if _, err := c.AccessToken(context.Background()); !errors.Is(err, ErrReauthorize) {
		t.Fatalf("refused refresh: %v", err)
	}
	f.now = f.now.Add(200 * 24 * time.Hour)
	if s := c.Status(); s.State != StateReauthorize || c.Authorized() {
		t.Fatalf("an expired refresh token still reads %+v", s)
	}
}

func TestTheTokenFileIsOwnerOnlyAndNeverFollowed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "github")
	s := TokenStore{Dir: dir}
	if _, err := s.Load(); !errors.Is(err, ErrNoToken) {
		t.Fatalf("absent token: %v", err)
	}
	if err := s.Save(Token{AccessToken: "a", Login: "octocat"}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		for path, want := range map[string]os.FileMode{dir: 0o700, s.path(): 0o600} {
			info, _ := os.Stat(path)
			if info.Mode().Perm() != want {
				t.Fatalf("%s is %v", path, info.Mode().Perm())
			}
		}
	}
	os.WriteFile(s.path(), []byte("{not json"), 0o600)
	if _, err := s.Load(); err == nil || errors.Is(err, ErrNoToken) {
		t.Fatalf("a damaged file read as %v", err)
	}
	os.WriteFile(s.path(), []byte(strings.Repeat(" ", MaxTokenFileBytes+1)), 0o600)
	if _, err := s.Load(); err == nil || errors.Is(err, ErrNoToken) {
		t.Fatalf("an oversized file read as %v", err)
	}
	if runtime.GOOS != "windows" {
		elsewhere := filepath.Join(t.TempDir(), "other")
		os.WriteFile(elsewhere, []byte(`{"access_token":"x"}`), 0o600)
		os.Remove(s.path())
		os.Symlink(elsewhere, s.path())
		if _, err := s.Load(); err == nil {
			t.Fatal("a symlinked token file was read")
		}
	}
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(); err != nil {
		t.Fatalf("deleting twice: %v", err)
	}
}

func TestALargeOrFailedAnswerIsRefusedWithoutEchoingIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("echo-of-secret"))
			return
		}
		w.Write([]byte(`{"pad":"` + strings.Repeat("x", MaxResponseBytes) + `"}`))
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), OAuthURL: srv.URL, APIURL: srv.URL, ClientID: ClientID}
	if _, err := c.StartDevice(context.Background()); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("oversized answer: %v", err)
	}
	if _, err := c.Login(context.Background(), "t"); err == nil || strings.Contains(err.Error(), "echo") {
		t.Fatalf("failed answer: %v", err)
	}
}

func TestOnlyAGitHubDotComRemoteNamesARepository(t *testing.T) {
	cases := map[string]string{
		"git@github.com:octo-org/widgets.git":           "octo-org/widgets",
		"https://github.com/octo-org/widgets":           "octo-org/widgets",
		"https://github.com/octo-org/widgets.git\n":     "octo-org/widgets",
		"ssh://git@github.com/octo-org/widgets.git":     "octo-org/widgets",
		"https://GitHub.com/octo-org/widgets.git":       "octo-org/widgets",
		"https://github.com.evil.example/octo/widgets":  "",
		"git@ghe.example.com:octo/widgets.git":          "",
		"https://notgithub.com/octo/widgets":            "",
		"https://github.com/octo-org":                   "",
		"https://github.com/octo-org/widgets/tree/main": "",
		"file:///srv/github.com/octo/widgets.git":       "",
		"/srv/repos/widgets.git":                        "",
	}
	for remote, want := range cases {
		if got := GitHubRepo(remote); got != want {
			t.Errorf("GitHubRepo(%q) = %q, want %q", remote, got, want)
		}
	}
}

type remoteAnswer struct {
	url string
	err error
}

func (r remoteAnswer) Remote(context.Context, string) (string, error) { return r.url, r.err }

func TestAProjectWithoutAGitHubRemoteSaysWhichWay(t *testing.T) {
	cases := []struct {
		r    remoteAnswer
		want error
	}{
		{remoteAnswer{err: git.ErrNoRemote}, ErrNoRemote},
		{remoteAnswer{err: git.ErrNotRepository}, ErrNoRepository},
		{remoteAnswer{err: git.ErrTimedOut}, ErrRemoteUnread},
		{remoteAnswer{url: "https://gitlab.com/octo/widgets.git"}, ErrRemoteNotGitHub},
	}
	for _, c := range cases {
		if _, err := TargetFor(context.Background(), c.r, "/p"); !errors.Is(err, c.want) {
			t.Errorf("%+v: %v, want %v", c.r, err, c.want)
		}
	}
	got, err := TargetFor(context.Background(), remoteAnswer{url: "git@github.com:octo/widgets.git"}, "/p")
	if err != nil || got.Repo != "octo/widgets" {
		t.Fatalf("%+v %v", got, err)
	}
}
