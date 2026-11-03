package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/githubissues"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

func TestTheGitHubConnectionIsChangedOnlyByThePerson(t *testing.T) {
	readOnly := auth.Verdict{Allowed: true, Caps: auth.NewCaps(auth.Read)}
	for _, path := range []string{"/v1/integrations/github", "/v1/integrations/github/device"} {
		for _, method := range []string{http.MethodPost, http.MethodDelete} {
			if status, code, _ := writePolicy(method, path, false, readOnly); status != http.StatusForbidden || code != "forbidden" {
				t.Fatalf("%s %s by a read-only device: %d %q", method, path, status, code)
			}
			if status, _, _ := writePolicy(method, path, true, auth.Verdict{}); status != http.StatusForbidden {
				t.Fatalf("%s %s by the machine token: %d", method, path, status)
			}
		}
		for name, verdict := range map[string]auth.Verdict{
			"paired sender": {Allowed: true, Caps: auth.NewCaps(auth.Read, auth.Send)},
			"local console": {Allowed: true, Local: true, Caps: auth.NewCaps(auth.Read)},
		} {
			if status, code, message := writePolicy(http.MethodPost, path, false, verdict); status != 0 {
				t.Fatalf("%s on %s: %d %q %q", name, path, status, code, message)
			}
		}
		if status, _, _ := writePolicy(http.MethodGet, path, false, readOnly); status != 0 {
			t.Fatalf("reading %s was treated as a write: %d", path, status)
		}
	}
}

func TestTheGitHubRoutesNeverAnswerASecret(t *testing.T) {
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"device_code": "device-secret", "user_code": "WXYZ-9876",
			"verification_uri": "https://github.com/login/device", "expires_in": 900, "interval": 5,
			"error": map[bool]string{true: "authorization_pending"}[r.URL.Path == "/login/oauth/access_token"]})
	}))
	defer github.Close()
	client := githubissues.NewClient()
	client.HTTP, client.OAuthURL, client.APIURL = github.Client(), github.URL, github.URL
	s := &Server{githubConn: githubissues.NewConnection(client, filepath.Join(t.TempDir(), "github"))}
	defer s.StopGitHub()

	answer := func(method, path string, route http.HandlerFunc) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		route(rec, httptest.NewRequest(method, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "device-secret") || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s answered %s with %q", method, path, rec.Body, rec.Header().Get("Cache-Control"))
		}
		var body map[string]any
		json.Unmarshal(rec.Body.Bytes(), &body)
		return body
	}
	if got := answer(http.MethodGet, "/v1/integrations/github", s.githubIntegrationRoute); got["state"] != "disconnected" {
		t.Fatalf("before connecting: %v", got)
	}
	started := answer(http.MethodPost, "/v1/integrations/github/device", s.githubDeviceRoute)
	if started["state"] != "pending" || started["user_code"] != "WXYZ-9876" {
		t.Fatalf("start: %v", started)
	}
	if again := answer(http.MethodPost, "/v1/integrations/github/device", s.githubDeviceRoute); again["user_code"] != started["user_code"] {
		t.Fatalf("a second start issued %v", again)
	}
	if got := answer(http.MethodDelete, "/v1/integrations/github", s.githubIntegrationRoute); got["state"] != "disconnected" {
		t.Fatalf("after disconnecting: %v", got)
	}
}
