package http

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline-go/internal/config"
)

// upstreamFixture is a daemon built the way `clawdline serve` builds one, with
// its own state directory and this machine's own token, so that what is being
// read is the real routing decision behind the real gate.
func upstreamFixture(t *testing.T, cfg config.Config) (http.Handler, string) {
	t.Helper()
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(t.TempDir(), "swift"))
	t.Setenv("CLAWDLINE_NEXT_WEB", "")
	cfg.Dir = filepath.Join(t.TempDir(), "next")
	if cfg.Port == 0 {
		cfg.Port = 7757
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.store.Close() })
	local, _, err := s.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	return s.Handler(), local
}

func askUnowned(t *testing.T, h http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/a-route-nobody-has-written-yet", nil)
	req.Host = "127.0.0.1:7757"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A route this daemon has not taken over says its own name.
//
// It did not, and the reason was a default: `UpstreamPort` was 7717, the port
// the Swift app held, so every unported route was forwarded there. That app was
// stopped on 2026-09-19 and nothing has answered 7717 since, which turned
// "this daemon does not own that route yet" — a fact a reader can act on — into
// `502 upstream_unreachable` pointing at a port nobody holds. This pins the
// answer, not the environment variable that used to have to be remembered to
// get it: no `CLAWDLINE_NEXT_STANDALONE` is set anywhere below.
func TestAnUnownedRouteNamesItself(t *testing.T) {
	h, token := upstreamFixture(t, config.Load())

	rec := askUnowned(t, h, token)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("an unowned route answered %d, not 501: %s", rec.Code, rec.Body)
	}
	var refusal struct {
		Error string `json:"error"`
		Route string `json:"route"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &refusal); err != nil {
		t.Fatalf("the refusal is not json: %s", rec.Body)
	}
	if refusal.Error != "not_implemented" {
		t.Errorf("the refusal is %q, not not_implemented: %s", refusal.Error, rec.Body)
	}
	if refusal.Route != "/v1/a-route-nobody-has-written-yet" {
		t.Errorf("the refusal does not name the route: %s", rec.Body)
	}
}

// The default carries no upstream at all, and `doctor` says so in words.
func TestLoadedConfigHasNoUpstream(t *testing.T) {
	t.Setenv(config.UpstreamPortEnv, "")
	if port, ok := config.Load().Upstream(); ok {
		t.Fatalf("the default config forwards to :%d; nothing is listening there and an unowned route would 502", port)
	}
	t.Setenv(config.UpstreamPortEnv, "7717")
	if port, ok := config.Load().Upstream(); !ok || port != 7717 {
		t.Fatalf("asking for an upstream out loud did not produce one: %d %v", port, ok)
	}
	t.Setenv(config.UpstreamPortEnv, "not-a-port")
	if _, ok := config.Load().Upstream(); ok {
		t.Fatal("a misspelt port produced an upstream; it must leave the default standing")
	}
}

// Forwarding still works when somebody asks for it, and still names the hop
// that failed. This is the other half of the flip: the scaffold was not removed,
// it was moved behind a request.
func TestForwardingIsOptInAndStillNamesTheHop(t *testing.T) {
	// A port that is bound and then released: nothing is listening on it, and
	// nothing else on this machine is claimed while the test runs.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	t.Setenv(config.UpstreamPortEnv, fmt.Sprint(port))
	h, token := upstreamFixture(t, config.Load())

	rec := askUnowned(t, h, token)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("an asked-for upstream answered %d, not 502: %s", rec.Code, rec.Body)
	}
	var refusal struct {
		Error    string `json:"error"`
		Upstream string `json:"upstream"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &refusal); err != nil {
		t.Fatalf("the refusal is not json: %s", rec.Body)
	}
	if refusal.Error != "upstream_unreachable" {
		t.Errorf("the refusal is %q, not upstream_unreachable: %s", refusal.Error, rec.Body)
	}
	if refusal.Upstream != fmt.Sprintf("http://127.0.0.1:%d", port) {
		t.Errorf("the refusal does not name the hop that failed: %s", rec.Body)
	}
}

// CLAWDLINE_NEXT_STANDALONE=1 was how a person opted out of forwarding while
// forwarding was the default. It still means "never forward", so a script or a
// bundle written before the flip keeps meaning what it meant.
func TestStandaloneStillRefusesToForward(t *testing.T) {
	t.Setenv(config.UpstreamPortEnv, "7717")
	t.Setenv("CLAWDLINE_NEXT_STANDALONE", "1")
	h, token := upstreamFixture(t, config.Load())

	if rec := askUnowned(t, h, token); rec.Code != http.StatusNotImplemented {
		t.Fatalf("STANDALONE=1 forwarded anyway: %d %s", rec.Code, rec.Body)
	}
}

// /v1/sessions follows the same rule. With nobody behind this daemon it is
// answered here rather than handed to a port that is not there: the session
// list is the product, and `502 upstream_unreachable` was the answer a stock
// Linux build gave for it.
func TestSessionsAreOwnedWhenNothingIsBehindThisDaemon(t *testing.T) {
	t.Setenv("CLAWDLINE_NEXT_OWN_SESSIONS", "")
	h, token := upstreamFixture(t, config.Load())

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	req.Host = "127.0.0.1:7757"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/v1/sessions answered %d, not 200: %s", rec.Code, rec.Body)
	}
	var reply map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatalf("/v1/sessions is not json: %s", rec.Body)
	}
	if _, ok := reply["sessions"]; !ok {
		t.Errorf("/v1/sessions answered without a session list: %s", rec.Body)
	}
}
