package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/workflow"
)

func guidanceServer(t *testing.T) (*Server, *pane) {
	t.Helper()
	p := waitingPane("%4", "")
	p.s.ConversationID = "native-one"
	p.s.State = session.StateIdle
	s := paneServer(t, p)
	s.cfg.Dir = t.TempDir()
	if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"codegraph_enabled": true}); err != nil {
		t.Fatal(err)
	}
	return s, p
}
func TestCodegraphGuidanceHTTPOnceCommandsReplaysAndConversationChange(t *testing.T) {
	s, p := guidanceServer(t)
	choice := `{"conversation":"native-one","revision":0,"enabled":true,"again":false,"language":"en"}`
	enabled := act(t, s, "codegraph-guidance", "%4", "pref-one", choice)
	if enabled.Code != 200 {
		t.Fatal(enabled.Code, enabled.Body)
	}
	if len(p.done()) != 0 {
		t.Fatal("preference typed without a user message")
	}
	for i, body := range []string{`{"text":"/compact","conversation":"native-one"}`, `{"text":"","conversation":"native-one"}`, `{"text":"first","conversation":"native-one"}`, `{"text":"second","conversation":"native-one"}`} {
		rec := act(t, s, "send", "%4", []string{"command", "empty", "first", "second"}[i], body)
		if i == 1 {
			if rec.Code != 400 {
				t.Fatal("empty message sent", rec.Body)
			}
			continue
		}
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body)
		}
	}
	done := p.done()
	if len(done) != 3 || done[0] != "send:/compact" || !strings.Contains(done[1], "clawdline codegraph query") || done[2] != "send:second" {
		t.Fatalf("typed=%q", done)
	}
	replay := act(t, s, "send", "%4", "first", `{"text":"first","conversation":"native-one"}`)
	if replay.Code != 200 || len(p.done()) != 3 || !strings.Contains(replay.Body.String(), "codegraph_suffix") {
		t.Fatal("replay lost exact answer", replay.Body)
	}
	g, found, err := s.store.CodegraphGuidance(context.Background(), guidanceConversation(p.s))
	if err != nil || !found || g.State != "provided" {
		t.Fatal(g, err)
	}
	p.s.ConversationID = "native-two"
	moved := act(t, s, "send", "%4", "stale-conversation", `{"text":"must not type","conversation":"native-one"}`)
	if moved.Code != 409 || len(p.done()) != 3 {
		t.Fatal("stale target typed", moved.Code, moved.Body)
	}
	if _, found, _ := s.store.CodegraphGuidance(context.Background(), guidanceConversation(p.s)); found {
		t.Fatal("terminal reuse inherited preference")
	}
}
func TestCodegraphGuidanceRefusedSendRestoresOnlyProvedUnsent(t *testing.T) {
	s, p := guidanceServer(t)
	ctx := context.Background()
	conversation := guidanceConversation(p.s)
	for _, tc := range []struct {
		name  string
		err   error
		state string
	}{
		{"pre-byte", app.Refusal{Code: "session_asking", Cause: terminal.Unsent{Why: "injected before typing"}}, "pending"},
		{"unsubmitted", app.Refusal{Code: "send_unsubmitted"}, "unknown"},
		{"partial", app.Refusal{Code: "send_failed"}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous, _, _ := s.store.CodegraphGuidance(ctx, conversation)
			_, err := s.store.ConfigureCodegraphGuidance(ctx, conversation, previous.Revision, true, true, "1", "en", false)
			if err != nil {
				t.Fatal(err)
			}
			suffix, finish, err := s.prepareCodegraphSend(ctx, p.s.ID, p.s.ConversationID, "hello")
			if err != nil || suffix == "" {
				t.Fatal(suffix, err)
			}
			finish(tc.err)
			g, _, _ := s.store.CodegraphGuidance(ctx, conversation)
			if g.State != tc.state {
				t.Fatal(g)
			}
			w := httptest.NewRecorder()
			writeGuidanceSendRefusal(w, tc.err, suffix)
			var body contract.Refusal
			if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.CodegraphSuffix != suffix {
				t.Fatal("lost actual typed suffix", w.Body)
			}
		})
	}
}
func TestCodegraphGuidanceInfoUsesLaunchDefaultWithoutDuplicate(t *testing.T) {
	s, p := guidanceServer(t)
	ctx := context.Background()
	snap := sealedLaunchSnapshot(t, true)
	launch, err := s.store.PrepareWorkflowLaunch(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.store.RecordWorkflowTerminal(ctx, launch.ID, p.s.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.store.BindWorkflowConversation(ctx, launch.ID, p.s.ID, p.s.ConversationID); err != nil {
		t.Fatal(err)
	}
	var info contract.SessionInfo
	s.fillCodegraphGuidance(ctx, p.s, &info)
	if info.CodegraphGuidance == nil || info.CodegraphGuidance.State != "launch" {
		t.Fatal(info.CodegraphGuidance)
	}
	suffix, finish, err := s.prepareCodegraphSend(ctx, p.s.ID, p.s.ConversationID, "hello")
	if err != nil || suffix != "" {
		t.Fatal("launch guide repeated", suffix, err)
	}
	finish(nil)
	_, _ = s.store.ConfigureCodegraphGuidance(ctx, guidanceConversation(p.s), 1, false, false, "1", "en", false)
	suffix, finish, err = s.prepareCodegraphSend(ctx, p.s.ID, p.s.ConversationID, "hello")
	if err != nil || !strings.Contains(suffix, "Stop using") {
		t.Fatal(suffix, err)
	}
	finish(nil)
}

// Keep the snapshot fixture within the public workflow contract.
func sealedLaunchSnapshot(t *testing.T, enabled bool) workflow.Snapshot {
	t.Helper()
	snap, err := workflow.Seal(workflow.Snapshot{Schema: 2, ProfileID: "clawdline", Version: "1", Source: "test", Instructions: "fixture", CodeGraphEnabled: enabled, Stages: []workflow.Stage{}})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func TestCodegraphGuidancePreferenceRejectsStaleMissingAndMalformedChoices(t *testing.T) {
	s, p := guidanceServer(t)
	for _, tc := range []struct {
		name, key, body string
		status          int
	}{
		{"no receipt", "", `{"conversation":"native-one","revision":0,"enabled":true,"again":false,"language":"en"}`, 400},
		{"other conversation", "other", `{"conversation":"native-old","revision":0,"enabled":true,"again":false,"language":"en"}`, 409},
		{"null choice", "null", `{"conversation":"native-one","revision":0,"enabled":null,"again":false,"language":"en"}`, 400},
		{"extra field", "extra", `{"conversation":"native-one","revision":0,"enabled":true,"again":false,"language":"en","path":"/tmp/other"}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := act(t, s, "codegraph-guidance", p.s.ID, tc.key, tc.body)
			if r.Code != tc.status {
				t.Fatal(r.Code, r.Body)
			}
		})
	}
	if len(p.done()) != 0 {
		t.Fatal("invalid preference typed input")
	}
	if _, found, _ := s.store.CodegraphGuidance(context.Background(), guidanceConversation(p.s)); found {
		t.Fatal("invalid choice was saved")
	}
	r := httptest.NewRequest("POST", "/v1/sessions/%254/codegraph-guidance", strings.NewReader(`{"conversation":"native-one","revision":0,"enabled":true,"again":false,"language":"en"}`))
	r = r.WithContext(context.WithValue(r.Context(), accessKey{}, access{verdict: auth.Verdict{Allowed: true, Device: "reader", Caps: auth.NewCaps(auth.Read)}}))
	w := httptest.NewRecorder()
	s.sessionAction(w, r)
	if w.Code != 403 {
		t.Fatal("read-only viewer changed preference", w.Code, w.Body)
	}
}

func TestCodegraphGuidanceCanDisableLaunchContextBeforeFirstHumanSend(t *testing.T) {
	s, p := guidanceServer(t)
	ctx := context.Background()
	snap := sealedLaunchSnapshot(t, true)
	launch, err := s.store.PrepareWorkflowLaunch(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.store.RecordWorkflowTerminal(ctx, launch.ID, p.s.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.store.BindWorkflowConversation(ctx, launch.ID, p.s.ID, p.s.ConversationID); err != nil {
		t.Fatal(err)
	}
	rec := act(t, s, "codegraph-guidance", p.s.ID, "initial-off", `{"conversation":"native-one","revision":0,"enabled":false,"again":false,"language":"en"}`)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body)
	}
	rec = act(t, s, "send", p.s.ID, "first-off", `{"text":"hello","conversation":"native-one"}`)
	if rec.Code != 200 || len(p.done()) != 1 || !strings.Contains(p.done()[0], "Stop using") {
		t.Fatal("launch opt-out was not provided", rec.Code, rec.Body, p.done())
	}
	original, found, err := s.store.WorkflowSnapshotForConversation(ctx, p.s.ConversationID)
	if err != nil || !found || !original.CodeGraphEnabled || original.Digest != snap.Digest {
		t.Fatal("immutable launch receipt was rewritten", original, err)
	}
}
