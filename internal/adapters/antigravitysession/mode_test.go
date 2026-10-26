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

func TestNewConversationNeverClaimsUnacknowledgedYolo(t *testing.T) {
	for _, variant := range []string{"unoffered", "wrong-ack", "missing-ack"} {
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
