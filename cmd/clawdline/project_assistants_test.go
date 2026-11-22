package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBotConsentSyncUsesLocalOwnerAndReportsRefusals(t *testing.T) {
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "local-token"), []byte("test-local"), 0600); err != nil {
		t.Fatal(err)
	}
	var responseStatus atomic.Int32
	responseStatus.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) != 0 || r.Method != http.MethodPost || r.URL.Path != "/v1/bot-network/consents/approved-consent/sync" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer test-local" || r.Header.Get("X-Clawdline-Orchestrator") != "" {
			t.Error("consent synchronization changed its scope or credential")
		}
		w.Header().Set("Content-Type", "application/json")
		status := int(responseStatus.Load())
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = io.WriteString(w, `{"synced":true,"authority_id":"approved-consent"}`)
		} else {
			_, _ = io.WriteString(w, `{"error":"pa_revoked","detail":"Consent was revoked"}`)
		}
	}))
	defer server.Close()
	t.Setenv("CLAWDLINE_NEXT_DIR", state)
	t.Setenv("CLAWDLINE_NEXT_PORT", strings.TrimPrefix(server.URL, "http://127.0.0.1:"))
	var out, errs bytes.Buffer
	if code := projectAssistantsCommand(context.Background(), []string{"network-consent-sync", "approved-consent"}, state, &out, &errs); code != 0 || !strings.Contains(out.String(), `"synced":true`) || errs.Len() != 0 {
		t.Fatalf("sync: code=%d output=%q error=%q", code, out.String(), errs.String())
	}
	out.Reset()
	responseStatus.Store(http.StatusForbidden)
	if code := projectAssistantsCommand(context.Background(), []string{"network-consent-sync", "approved-consent"}, state, &out, &errs); code != 1 || out.Len() != 0 || !strings.Contains(errs.String(), "pa_revoked") {
		t.Fatalf("refusal: code=%d output=%q error=%q", code, out.String(), errs.String())
	}
}

func TestClaudeSetupWithoutClaudeCodeChangesNothing(t *testing.T) {
	state := t.TempDir()
	var out, err bytes.Buffer
	if code := projectAssistantsCommand(context.Background(), []string{"setup-claude", "--cli", "/elsewhere"}, state, &out, &err); code != 2 {
		t.Fatalf("paths are no longer accepted: code=%d", code)
	}
	want := "cli_not_installed"
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		want = "pa_unsupported"
	}
	if code := projectAssistantsCommand(context.Background(), []string{"setup-claude"}, state, &out, &err); code != 1 || out.Len() != 0 || !strings.Contains(err.String(), want) {
		t.Fatalf("setup without tools: code=%d output=%q error=%q", code, out.String(), err.String())
	}
	entries, e := os.ReadDir(state)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatal("refused setup changed private state")
	}
}

func TestTheBotsSwitchReportsTheDaemonsState(t *testing.T) {
	var out, errOut bytes.Buffer
	var sent any
	call := func(method string, body any) (int, []byte, error) {
		sent = body
		if method == "GET" {
			return 200, []byte(`{"enabled":false}`), nil
		}
		return 200, []byte(`{"enabled":true}`), nil
	}
	if code := projectAssistantsSwitch("enable", &out, &errOut, call); code != 0 || !strings.Contains(out.String(), "Bots are on") {
		t.Fatalf("enable: %d %q %q", code, out.String(), errOut.String())
	}
	if m, ok := sent.(map[string]bool); !ok || !m["enabled"] {
		t.Fatalf("enable sent %v", sent)
	}
	out.Reset()
	if code := projectAssistantsSwitch("status", &out, &errOut, call); code != 0 || !strings.Contains(out.String(), "Bots are off") || sent != nil {
		t.Fatalf("status: %d %q %v", code, out.String(), sent)
	}
	refused := func(string, any) (int, []byte, error) { return 403, []byte(`{"error":"forbidden"}`), nil }
	if code := projectAssistantsSwitch("disable", &out, &errOut, refused); code != 1 {
		t.Fatalf("a refused switch reported success: %d", code)
	}
}
