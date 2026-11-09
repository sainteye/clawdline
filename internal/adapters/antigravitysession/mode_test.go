//go:build darwin || linux

package antigravitysession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
)

func syntheticModeOption(current string) any {
	values := []any{map[string]string{"value": "default"}, map[string]string{"value": "auto_edit"}, map[string]string{"value": "yolo"}}
	if os.Getenv("CLW_MODE_PEER") == "unoffered" {
		values = values[:1]
	}
	if os.Getenv("CLW_MODE_PEER") == "native-1.5" {
		values = []any{map[string]string{"value": "default"}, map[string]string{"value": "accept-edits"}, map[string]string{"value": "plan"}}
	}
	if os.Getenv("CLW_MODE_PEER") == "malformed" {
		return map[string]any{"id": "mode", "type": "select", "currentValue": false}
	}
	return map[string]any{"id": "mode", "type": "select", "currentValue": current, "options": values}
}

func assertConfigWrites(t *testing.T, dir, want string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "config-writes"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if string(raw) != want {
		t.Fatalf("config writes=%q, want %q", raw, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts")); !os.IsNotExist(err) {
		t.Fatal("configuration wrote a prompt", err)
	}
}

func TestConfigureDispatchOrderedNativeACKs(t *testing.T) {
	for _, permission := range []struct{ input, mode string }{{"ask", "default"}, {"edits", "auto_edit"}, {"full", "yolo"}} {
		t.Run(permission.input, func(t *testing.T) {
			t.Setenv("CLW_CONFIG_PEER", "yes")
			t.Setenv("CLW_MODE_PEER", "yes")
			h, row, dir := fixture(t)
			if err := h.ConfigureDispatch(context.Background(), row, "synthetic-pro", "high", permission.input); err != nil {
				t.Fatal(err)
			}
			assertConfigWrites(t, dir, "mode="+permission.mode+"\nmodel=synthetic-pro\nthinking_level=high\n")
			if page, _ := h.Transcript(row, 10); len(page.Entries) != 0 {
				t.Fatal(page)
			}
		})
	}
}

func TestConfigureDispatchRejectsUnavailableMode(t *testing.T) {
	for _, variant := range []string{"", "unoffered", "malformed", "wrong-ack", "missing-ack"} {
		t.Run("native-"+variant, func(t *testing.T) {
			t.Setenv("CLW_CONFIG_PEER", "yes")
			t.Setenv("CLW_MODE_PEER", variant)
			h, row, dir := fixture(t)
			err := h.ConfigureDispatch(context.Background(), row, "synthetic-pro", "high", "edits")
			var typed *Error
			if !errors.As(err, &typed) || typed.Code != "permission_unavailable" {
				t.Fatalf("error=%v", err)
			}
			want := ""
			if variant == "wrong-ack" || variant == "missing-ack" {
				want = "mode=auto_edit\n"
			}
			assertConfigWrites(t, dir, want)
		})
	}
}

func TestConfigureDispatchUsesUpdatedEffortsAndNativeDefaults(t *testing.T) {
	t.Setenv("CLW_CONFIG_PEER", "yes")
	t.Setenv("CLW_MODE_PEER", "yes")
	h, row, dir := fixture(t)
	if err := h.ConfigureDispatch(context.Background(), row, "synthetic-pro", "low", "ask"); err == nil {
		t.Fatal("stale effort accepted")
	}
	assertConfigWrites(t, dir, "mode=default\nmodel=synthetic-pro\n")
	if err := h.ConfigureDispatch(context.Background(), row, "", "", "ask"); err != nil {
		t.Fatal(err)
	}
	assertConfigWrites(t, dir, "mode=default\nmodel=synthetic-pro\nmode=default\n")
	facts, _ := h.Facts(row)
	if facts.Model != "synthetic-pro" || facts.Effort != "medium" {
		t.Fatal(facts)
	}
}

func TestConfigureDispatchLeaseRejectsCloseAndConcurrentCalls(t *testing.T) {
	t.Setenv("CLW_CONFIG_PEER", "yes")
	t.Setenv("CLW_MODE_PEER", "yes")
	h, row, dir := fixture(t)
	if err := os.WriteFile(filepath.Join(dir, "hold-config"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- h.ConfigureDispatch(context.Background(), row, "synthetic-pro", "high", "edits") }()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "config-writes")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no configuration write")
		}
		time.Sleep(time.Millisecond)
	}
	for _, err := range []error{h.Close(context.Background(), row), h.ConfigureDispatch(context.Background(), row, "", "", "ask"), h.Send(context.Background(), row, "must not prompt")} {
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != "busy" {
			t.Fatalf("busy error=%v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "release-config"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := h.Close(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	if err := h.ConfigureDispatch(context.Background(), row, "", "", "ask"); err == nil {
		t.Fatal("closed client admitted")
	}
	assertConfigWrites(t, dir, "mode=auto_edit\nmodel=synthetic-pro\nthinking_level=high\n")
}

func TestNativeModeUnknownAndDuplicateStayUnavailable(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `[]`, `[{"id":"mode","type":"select","currentValue":"default","options":[{"value":"default"},{"value":"default"}]}]`, `[{"id":"mode","type":"unknown"}]`} {
		if _, err := nativeMode(json.RawMessage(raw), "default"); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestConfigureDispatchShutdownCancelsLease(t *testing.T) {
	t.Setenv("CLW_CONFIG_PEER", "yes")
	t.Setenv("CLW_MODE_PEER", "yes")
	h, row, dir := fixture(t)
	if err := os.WriteFile(filepath.Join(dir, "hold-config"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- h.ConfigureDispatch(context.Background(), row, "synthetic-pro", "high", "full") }()
	deadline := time.Now().Add(time.Second)
	for {
		// File creation precedes the peer write; wait for the request bytes.
		if raw, err := os.ReadFile(filepath.Join(dir, "config-writes")); err == nil && string(raw) == "mode=yolo\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("configuration never reached pipe")
		}
		time.Sleep(time.Millisecond)
	}
	if err := h.Shutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("shutdown configuration succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not release configuration")
	}
	assertConfigWrites(t, dir, "mode=yolo\n")
}

func TestConfigureDispatchMissingModeKeepsOrdinaryChat(t *testing.T) {
	t.Setenv("CLW_CONFIG_PEER", "yes")
	h, row, dir := fixture(t)
	if err := h.ConfigureDispatch(context.Background(), row, "", "", "ask"); err == nil {
		t.Fatal("missing mode accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "ordinary chat"); err != nil {
		t.Fatal(err)
	}
	page := settled(t, h, row)
	if len(page.Entries) == 0 || page.Entries[0].Text != "ordinary chat" {
		t.Fatal(page)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "prompts"))
	if string(raw) != "prompt\n" {
		t.Fatalf("prompts=%q", raw)
	}
}

func TestConfigureDispatchReconnectReadsNewNativeOffers(t *testing.T) {
	t.Setenv("CLW_CONFIG_PEER", "yes")
	t.Setenv("CLW_MODE_PEER", "yes")
	h, row, opts := persistentFixture(t)
	if err := h.ConfigureDispatch(context.Background(), row, "", "", "full"); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLW_MODE_PEER", "unoffered")
	err := h.ConfigureDispatch(context.Background(), row, "", "", "edits")
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "permission_unavailable" {
		t.Fatalf("stale mode offers reused: %v", err)
	}
	assertConfigWrites(t, opts.AccountDir, "mode=yolo\n")
	if err := h.ConfigureDispatch(context.Background(), row, "", "", "ask"); err != nil {
		t.Fatal(err)
	}
	assertConfigWrites(t, opts.AccountDir, "mode=yolo\nmode=default\n")
}

func TestConfigureDispatchRejectsUnsupportedPermissionAndModel(t *testing.T) {
	t.Setenv("CLW_CONFIG_PEER", "yes")
	t.Setenv("CLW_MODE_PEER", "yes")
	h, row, dir := fixture(t)
	for _, permission := range []string{"", "unknown"} {
		if err := h.ConfigureDispatch(context.Background(), row, "", "", permission); err == nil {
			t.Fatal("unsupported permission accepted")
		}
	}
	assertConfigWrites(t, dir, "")
	if err := h.ConfigureDispatch(context.Background(), row, "invented", "high", "ask"); err == nil {
		t.Fatal("unoffered model accepted")
	}
	assertConfigWrites(t, dir, "mode=default\n")
}

func TestNewConversationSelectsYoloWithoutChangingExistingMode(t *testing.T) {
	t.Setenv("CLW_CONFIG_PEER", "yes")
	t.Setenv("CLW_MODE_PEER", "yes")
	h, existing, dir := fixture(t)
	h.opts.NewSessionMode = "yolo"
	created, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	assertConfigWrites(t, dir, "mode=yolo\n")
	old, err := h.lookup(existing)
	if err != nil {
		t.Fatal(err)
	}
	current, err := nativeMode(old.client.ConfigOptions(), "default")
	if err != nil || current != "default" {
		t.Fatal(current, err)
	}
	fresh, err := h.lookup(created)
	if err != nil {
		t.Fatal(err)
	}
	current, err = nativeMode(fresh.client.ConfigOptions(), "yolo")
	if err != nil || current != "yolo" {
		t.Fatal(current, err)
	}
	if _, err := h.Resume(context.Background(), existing.CWD, existing.ConversationID); err != nil {
		t.Fatal(err)
	}
	assertConfigWrites(t, dir, "mode=yolo\n")
}

func TestResumeReappliesOnlyExplicitGlobalMaximum(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "review", true: "full"}[full], func(t *testing.T) {
			t.Setenv("CLW_CONFIG_PEER", "yes")
			t.Setenv("CLW_MODE_PEER", "native-1.5")
			h, row, opts := persistentFixture(t)
			if full {
				if err := os.WriteFile(filepath.Join(opts.AccountDir, "antigravity-acp", "settings.json"), []byte(`{"useG1Credits":false,"auth":{"type":"oauth-personal"},"toolPermission":"always-proceed"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			stopHost(t, h)
			opts.NewSessionMode = "yolo"
			h = reopenHost(t, opts)
			resumed, err := h.Resume(context.Background(), row.CWD, row.ConversationID)
			if err != nil {
				t.Fatal(err)
			}
			if resumed.ID != row.ID || resumed.ConversationID != row.ConversationID {
				t.Fatal("resume replaced identity", resumed)
			}
			c, err := h.lookup(resumed)
			if err != nil {
				t.Fatal(err)
			}
			current, err := nativeMode(c.client.ConfigOptions(), "accept-edits")
			want := "default"
			writes := ""
			if full {
				want = "accept-edits"
				writes = "mode=accept-edits\n"
			}
			if err != nil || current != want {
				t.Fatal("restored mode", current, err, "want", want)
			}
			assertConfigWrites(t, opts.AccountDir, writes)
		})
	}
}

func TestNewConversationNeverClaimsUnacknowledgedYolo(t *testing.T) {
	for _, variant := range []string{"malformed", "wrong-ack", "missing-ack"} {
		t.Run(variant, func(t *testing.T) {
			t.Setenv("CLW_CONFIG_PEER", "yes")
			t.Setenv("CLW_MODE_PEER", variant)
			h, _, dir := fixture(t)
			h.opts.NewSessionMode = "yolo"
			if _, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir}); err == nil {
				t.Fatal("unavailable startup mode was claimed")
			}
			if _, err := os.Stat(filepath.Join(dir, "prompts")); !os.IsNotExist(err) {
				t.Fatal("startup mode made a model request", err)
			}
		})
	}
}

func TestNativeModeAcceptsAnOfferedCurrentModeWithoutANameAllowlist(t *testing.T) {
	raw := json.RawMessage(`[{"id":"mode","type":"select","currentValue":"future mode","options":[{"value":"future mode"},{"value":"yolo"}]}]`)
	if current, err := nativeMode(raw, "yolo"); err != nil || current != "future mode" {
		t.Fatalf("current=%q err=%v", current, err)
	}
}

func TestNewConversationUsesModernMaximumOnlyWithAlwaysProceed(t *testing.T) {
	for _, tc := range []struct {
		name, offer, policy string
		succeeds            bool
	}{
		{"modern-full", "native-1.5", "always-proceed", true},
		{"modern-review", "native-1.5", "request-review", false},
		{"modern-unset", "native-1.5", "", false},
		{"no-edit-mode", "unoffered", "always-proceed", false},
		{"malformed-offer", "malformed", "always-proceed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLW_CONFIG_PEER", "yes")
			t.Setenv("CLW_MODE_PEER", tc.offer)
			h, _, dir := fixture(t)
			settings, _ := json.Marshal(map[string]any{"useG1Credits": false, "auth": map[string]string{"type": "oauth-personal"}, "toolPermission": tc.policy})
			if err := os.WriteFile(filepath.Join(dir, "antigravity-acp", "settings.json"), settings, 0600); err != nil {
				t.Fatal(err)
			}
			h.opts.NewSessionMode = "yolo"
			row, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir})
			if !tc.succeeds {
				if err == nil {
					t.Fatal("maximum permission claimed without an available native combination")
				}
				assertConfigWrites(t, dir, "")
				return
			}
			if err != nil {
				t.Fatalf("modern maximum permission could not start: %v", err)
			}
			c, err := h.lookup(row)
			if err != nil {
				t.Fatal(err)
			}
			if mode, err := nativeMode(c.client.ConfigOptions(), "accept-edits"); err != nil || mode != "accept-edits" {
				t.Fatalf("native mode=%q err=%v", mode, err)
			}
			assertConfigWrites(t, dir, "mode=accept-edits\n")
			if err := h.ConfigureDispatch(context.Background(), row, "", "", "full"); err != nil {
				t.Fatalf("explicit full policy rejected: %v", err)
			}
			assertConfigWrites(t, dir, "mode=accept-edits\nmode=accept-edits\n")
			for _, permission := range []string{"ask", "edits"} {
				if err := h.ConfigureDispatch(context.Background(), row, "", "", permission); err == nil {
					t.Fatal("always-proceed was mislabeled as a lower permission")
				}
			}
			assertConfigWrites(t, dir, "mode=accept-edits\nmode=accept-edits\n")
		})
	}
}
