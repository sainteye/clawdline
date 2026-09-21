package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/app/cloudops"
)

// The switch is the whole of `docs/remote.md` design principle 1: the free
// product does not depend on Cloud, so a daemon whose settings say nothing must
// behave exactly as it did before this package existed.

func TestALineIsOffUntilSomebodySaysOtherwise(t *testing.T) {
	for _, config := range []struct {
		name string
		body string
	}{
		{"no settings file at all", ""},
		{"a settings file with no cloud keys", `{"remote":true}`},
		{"the switch written false", `{"cloud_enabled":false}`},
		{"the switch written as a string", `{"cloud_enabled":"true"}`},
	} {
		t.Run(config.name, func(t *testing.T) {
			dir := t.TempDir()
			if config.body != "" {
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(config.body), 0o600); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			link, err := Open(LinkOptions{Dir: dir})
			if config.name == "the switch written as a string" {
				// A malformed switch is a refusal, not a default: the
				// alternative is a daemon that decides for itself.
				if err == nil {
					t.Fatal("a switch that is not a boolean was accepted")
				}
				return
			}
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if link.Enabled() {
				t.Fatal("the line was on")
			}
			if got := link.Status(); got.Enabled || got.State != StateOff {
				t.Errorf("status said %+v", got)
			}
			if err := link.Run(context.Background()); !errors.Is(err, ErrDisabledLink) {
				t.Errorf("Run answered %v", err)
			}
			// Nothing was opened: no key was minted in a directory whose owner
			// never asked for a Cloud account.
			if _, err := os.Stat(filepath.Join(dir, "cloud")); !os.IsNotExist(err) {
				t.Errorf("a line that is off created a key store: %v", err)
			}
		})
	}
}

func TestALineThatIsOnWithNoIdentitySaysWhy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"cloud_enabled":true,"cloud_api_base":"http://127.0.0.1:9","cloud_relay_url":"ws://127.0.0.1:9/v1/connect"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	link, err := Open(LinkOptions{Dir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	status := link.Status()
	if !status.Enabled || status.Configured {
		t.Errorf("status said enabled=%v configured=%v", status.Enabled, status.Configured)
	}
	if status.LastError == "" {
		// "Not connected" with no reason is what sends a person to the log.
		t.Error("the status route could not say why the line is not up")
	}
	if err := link.Run(context.Background()); err == nil {
		t.Error("Run reported success for a machine that was never enrolled")
	}
}

func TestTheSettingsRefuseARelayURLThisBuildWillNotDial(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"cloud_enabled":true,"cloud_relay_url":"wss://relay.example.com/v1/somewhere-else"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// A path this build does not recognise is refused rather than rewritten,
	// and the refusal must reach the caller rather than falling back to the
	// production relay.
	if _, err := Open(LinkOptions{Dir: dir}); err == nil {
		t.Fatal("a relay URL with a path this build will not dial was accepted")
	}
}

func TestTheCloudRouterCarriesThisMachinesOwnCredentials(t *testing.T) {
	var seen *http.Request
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		w.WriteHeader(http.StatusNoContent)
	})
	router := Router{Handler: handler, Authorize: LocalAuthorizer("local-token", "machine-token")}

	if _, err := router.Do(context.Background(), cloudops.LocalRequest{Method: http.MethodGet, Path: "/v1/sessions"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := seen.Header.Get("Authorization"); got != "Bearer local-token" {
		t.Errorf("a session route was asked with %q", got)
	}
	if got := seen.Header.Get("X-Clawdline-Orchestrator"); got != "" {
		// A session route is not machine-scoped, and carrying the
		// orchestrator credential everywhere is how one door becomes two.
		t.Errorf("a session route carried the orchestrator token: %q", got)
	}

	if _, err := router.Do(context.Background(), cloudops.LocalRequest{Method: http.MethodGet, Path: "/v1/orchestrator/schedules"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := seen.Header.Get("X-Clawdline-Orchestrator"); got != "machine-token" {
		t.Errorf("a machine-scoped route was asked with %q", got)
	}
}

func TestTheStatusRouteShapeIsWhatTheSettingsPageReads(t *testing.T) {
	dir := t.TempDir()
	link, err := Open(LinkOptions{Dir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	encoded, err := json.Marshal(link.Status())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The three the card cannot draw without, and the one that answers "why is
	// nothing happening" nine times out of ten.
	for _, key := range []string{"enabled", "commands", "configured", "state"} {
		if _, ok := body[key]; !ok {
			t.Errorf("the status route answered without %q: %v", key, body)
		}
	}
}

// A server standing in for the daemon, only to prove the route is reachable
// in-process rather than over a socket.
func TestTheRouterDispatchesInProcess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the Cloud router opened a socket")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	answered := false
	router := Router{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		answered = true
		w.WriteHeader(http.StatusOK)
	})}
	if _, err := router.Do(context.Background(), cloudops.LocalRequest{Method: http.MethodGet, Path: "/v1/places"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !answered {
		t.Error("the handler was not called")
	}
}
