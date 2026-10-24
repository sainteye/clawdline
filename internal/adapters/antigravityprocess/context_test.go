//go:build darwin || linux

package antigravityprocess

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextPromptUsesCheckedSingleWrite(t *testing.T) {
	c, dir := fake(t, "happy", nil)
	ready(t, c)
	ctx := context.Background()
	denied := errors.New("synthetic denial")
	writes := 0
	_, err := c.PromptContextChecked(ctx, "original", "file:///synthetic/prompt.md", "role and memory", func() error { return denied }, func() { writes++ })
	if !errors.Is(err, denied) || writes != 0 || len(methods(t, dir)) != 3 {
		t.Fatalf("authority boundary: %v %d", err, writes)
	}
	_, err = c.PromptContextChecked(ctx, "original", "file:///synthetic/prompt.md", "role and memory", nil, func() { writes++ })
	if err != nil || writes != 1 {
		t.Fatalf("accepted: %v %d", err, writes)
	}
	if _, err := c.Prompt(ctx, "next"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "wire"))
	if err != nil {
		t.Fatal(err)
	}
	var prompts [][]json.RawMessage
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var frame struct {
			Method string
			Params struct{ Prompt []json.RawMessage }
		}
		if json.Unmarshal([]byte(line), &frame) != nil {
			t.Fatal("invalid peer wire")
		}
		if frame.Method == "session/prompt" {
			prompts = append(prompts, frame.Params.Prompt)
		}
	}
	if len(prompts) != 2 || len(prompts[0]) != 2 || len(prompts[1]) != 1 {
		t.Fatalf("prompts: %s", raw)
	}
	var resource struct {
		Type     string
		Resource struct{ URI, MimeType, Text string }
	}
	_ = json.Unmarshal(prompts[0][0], &resource)
	if resource.Type != "resource" || resource.Resource.URI != "file:///synthetic/prompt.md" || resource.Resource.Text != "role and memory" || resource.Resource.MimeType != "text/plain" {
		t.Fatalf("resource: %+v", resource)
	}
	if string(prompts[0][1]) != `{"text":"original","type":"text"}` {
		t.Fatal(string(prompts[0][1]))
	}
}

func TestContextRefusalAndPersonalGuardWriteNothing(t *testing.T) {
	c, dir := fake(t, "no-context", nil)
	ready(t, c)
	_, err := c.PromptContextChecked(context.Background(), "original", "file:///synthetic/prompt", "role", nil, nil)
	requireError(t, err, "embedded_context_unsupported", NotWritten)
	if len(methods(t, dir)) != 3 {
		t.Fatal("unsupported context was written")
	}
	// Empty context deliberately preserves plain prompt behavior.
	if _, err = c.PromptContextChecked(context.Background(), "plain", "", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	settings(t, dir, `{"useG1Credits":true,"auth":{"type":"oauth-personal"}}`)
	_, err = c.PromptContextChecked(context.Background(), "blocked", "file:///synthetic/prompt", "role", nil, nil)
	requireError(t, err, "personal_settings_required", NotWritten)
	if len(methods(t, dir)) != 4 {
		t.Fatal("credits-enabled context was written")
	}
}

func TestContextEnvironmentDropsParentIdentity(t *testing.T) {
	for _, key := range []string{"CLAWDLINE_SQUAD_CAPABILITY_FILE", "CLAWDLINE_ANTIGRAVITY_CAPABILITY_FILE", "CODEX_THREAD_ID", "CODEX_SESSION_ID", "CLAUDE_CODE_SESSION_ID", "CLAWDLINE_CONVERSATION_ID", "CLAWDLINE_TASK_SECRET"} {
		t.Setenv(key, "parent")
	}
	for _, pair := range personalEnv("/synthetic/profile") {
		if strings.HasSuffix(pair, "=parent") {
			t.Fatalf("inherited identity: %s", pair)
		}
	}
}

func TestContextValidationAndFrameBudgetPrecedeAuthorityAndWrite(t *testing.T) {
	c, dir := fake(t, "happy", nil)
	ready(t, c)
	checks, accepted := 0, 0
	before := func() error { checks++; return nil }
	written := func() { accepted++ }
	_, err := c.PromptContextChecked(context.Background(), "original", "https://synthetic.invalid/context", "role", before, written)
	requireError(t, err, "invalid_context_uri", NotWritten)
	_, err = c.PromptContextChecked(context.Background(), "original", "file:///synthetic/prompt", strings.Repeat("x", c.opts.MaxFrameBytes), before, written)
	requireError(t, err, "frame_too_large", NotWritten)
	if checks != 0 || accepted != 0 || len(methods(t, dir)) != 3 {
		t.Fatal("invalid context crossed write boundary")
	}
}
