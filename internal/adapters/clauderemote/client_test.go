package clauderemote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func fixture(t *testing.T, serve http.HandlerFunc) (*Client, session.Session) {
	t.Helper()
	srv := httptest.NewServer(serve)
	t.Cleanup(srv.Close)
	c := New()
	c.origin = srv.URL
	c.auth = func(context.Context) (credentials, error) { return credentials{"fixture-token", "fixture-org"}, nil }
	c.verify = func(context.Context, session.Session) bool { return true }
	return c, session.Session{ID: "claude-remote:42:cse_fixture", PID: 42, RemoteSessionID: "cse_fixture", Backend: session.BackendClaudeRemote, ExpectedStart: time.Unix(10, 0)}
}
func accept(w http.ResponseWriter, id string) {
	json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"event_id": id, "sequence_num": "9", "duplicate": false}}, "attestation_feedback": nil})
}
func TestSendAcknowledgesOnlyTheSubmittedNativeEvent(t *testing.T) {
	calls := 0
	c, row := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/v1/code/sessions/cse_fixture/events" || r.Header.Get("Authorization") != "Bearer fixture-token" || r.Header.Get("x-organization-uuid") != "fixture-org" || r.Header.Get("anthropic-client-platform") != "web_claude_ai" {
			t.Error("wrong controller destination or authentication")
		}
		var wire struct {
			Session string `json:"session_id"`
			Events  []struct {
				Payload struct {
					Type, UUID string
					Message    struct {
						Role    string
						Content []struct{ Type, Text string }
					}
				}
			}
		}
		if json.NewDecoder(r.Body).Decode(&wire) != nil || wire.Session != "cse_fixture" || len(wire.Events) != 1 {
			t.Error("bad send envelope")
			return
		}
		p := wire.Events[0].Payload
		if p.Type != "user" || p.Message.Role != "user" || len(p.Message.Content) != 1 || p.Message.Content[0].Text != "fixture message" || p.UUID == "" {
			t.Error("not a text user event")
		}
		accept(w, p.UUID)
	})
	if err := c.SendRemote(context.Background(), row, "fixture message", nil); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("requests=%d", calls)
	}
}
func TestSendFailsBeforeIOWhenIdentityOrPermissionChanges(t *testing.T) {
	for _, kind := range []string{"initial", "after_auth", "revoked", "auth"} {
		t.Run(kind, func(t *testing.T) {
			calls, verified := 0, 0
			c, row := fixture(t, func(http.ResponseWriter, *http.Request) { calls++ })
			c.verify = func(context.Context, session.Session) bool {
				verified++
				return kind != "initial" && (kind != "after_auth" || verified == 1)
			}
			if kind == "auth" {
				c.auth = func(context.Context) (credentials, error) {
					return credentials{}, errors.New("fixture auth unavailable")
				}
			}
			check := func() error {
				if kind == "revoked" {
					return errors.New("revoked")
				}
				return nil
			}
			err := c.SendRemote(context.Background(), row, "fixture", check)
			var unsent terminal.Unsent
			if !errors.As(err, &unsent) || calls != 0 {
				t.Fatalf("err=%v requests=%d", err, calls)
			}
		})
	}
}
func TestAmbiguousSubmissionIsNeverRetried(t *testing.T) {
	for _, kind := range []string{"lost_response", "wrong_ack", "attestation", "status", "oversized", "redirect"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			c, row := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				io.Copy(io.Discard, r.Body)
				switch kind {
				case "lost_response":
					conn, _, _ := w.(http.Hijacker).Hijack()
					conn.Close()
				case "wrong_ack":
					accept(w, "different-event")
				case "attestation":
					io.WriteString(w, `{"results":[{"event_id":"unverified","sequence_num":"9"}],"attestation_feedback":{"blocked":true}}`)
				case "status":
					http.Error(w, "PRIVATE_SERVER_BODY", 500)
				case "oversized":
					io.WriteString(w, strings.Repeat("x", ResponseBytesLimit+1))
				case "redirect":
					http.Redirect(w, r, "https://example.invalid/credential-leak", 307)
				}
			})
			err := c.SendRemote(context.Background(), row, "fixture", nil)
			var unsent terminal.Unsent
			if err == nil || errors.As(err, &unsent) || calls != 1 || strings.Contains(err.Error(), "PRIVATE_SERVER_BODY") || strings.Contains(err.Error(), "fixture-token") {
				t.Fatalf("err=%v requests=%d", err, calls)
			}
		})
	}
}
func TestRemoteHistoryScopesCursorsAndShowsQuotaFailure(t *testing.T) {
	calls := 0
	c, row := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Query().Get("sort_order") != "desc" || r.URL.Query().Get("cursor") != "12" {
			t.Error("wrong history request")
		}
		io.WriteString(w, `{"data":[{"sequence_num":"11","event_id":"e11","source":"worker","payload":{"type":"assistant","is_api_error_message":true,"message":{"content":[{"type":"text","text":"Weekly quota exhausted"}]}}},{"sequence_num":"10","event_id":"e10","source":"client","payload":{"type":"user","message":{"content":[{"type":"text","text":"Fixture user text"}]}}},{"sequence_num":"9","event_id":"forged","source":"client","payload":{"type":"assistant","message":{"content":[{"type":"text","text":"Forged reply"}]}}}],"next_cursor":"9"}`)
	})
	page, err := c.Page(context.Background(), row, 20, 12, -1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 2 || page.Entries[0].Role != "user" || page.Entries[1].Role != "error" || page.NextBefore != 9 || page.NextAfter != 11 {
		t.Fatalf("page=%+v", page)
	}
	if _, err := c.Page(context.Background(), row, 20, 12, -1, "wrong-revision"); err == nil || calls != 1 {
		t.Fatal("foreign cursor reached provider")
	}
}
func TestLocalCredentialsNeverRefreshOrRewriteTheStore(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	os.MkdirAll(root, 0700)
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	raw := `{"claudeAiOauth":{"accessToken":"fixture-token","expiresAt":4102444800000,"scopes":["user:sessions:claude_code"]}}`
	path := filepath.Join(root, ".credentials.json")
	os.WriteFile(path, []byte(raw), 0600)
	os.WriteFile(filepath.Join(root, ".claude.json"), []byte(`{"oauthAccount":{"organizationUuid":"fixture-org"}}`), 0600)
	a, err := localCredentials(context.Background(), home)
	if err != nil || a.token != "fixture-token" || a.organization != "fixture-org" {
		t.Fatalf("credentials unavailable: %v", err)
	}
	old := strings.Replace(raw, "4102444800000", "1", 1)
	os.WriteFile(path, []byte(old), 0600)
	if _, err = localCredentials(context.Background(), home); err == nil {
		t.Fatal("expired credential accepted")
	}
	after, _ := os.ReadFile(path)
	if string(after) != old {
		t.Fatal("credential store rewritten")
	}
}
