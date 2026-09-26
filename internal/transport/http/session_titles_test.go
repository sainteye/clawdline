package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// A title chosen in Session Info is durable in this daemon's own config, is
// the first label rung on the fleet list, and clearing it reveals the title the
// conversation had before. Nothing is written to the retired app's store.
func TestASessionTitleIsSavedShownAndCleared(t *testing.T) {
	item := session.Session{
		ID: "%41", TTY: "ttys041", Backend: session.BackendTmux,
		Assistant: session.AssistantCodex, State: session.StateIdle,
		Label: "Original title", Rungs: session.LabelRungs{Conversation: "Original title"},
	}
	p := &pane{s: item}
	s := paneServer(t, p)
	s.cfg = config.Config{Dir: filepath.Join(t.TempDir(), "clawdline-next")}
	s.icons = &icon.Registry{}

	rec := act(t, s, "title", item.ID, "title-1", `{"title":"  Release\n  helper  "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	var saved contract.SessionTitleReply
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Title != "Release helper" || saved.DisplayTitle != "Release helper" ||
		!saved.LocalApplied || saved.Downstream != "local_only" || saved.DownstreamSynced {
		t.Fatalf("save = %+v", saved)
	}

	v, err := nextconfig.Open(s.cfg.Dir).Read()
	if err != nil {
		t.Fatal(err)
	}
	rows := ownSessionTitles(v, time.Now())
	if len(rows) != 1 || rows[0].Title != "Release helper" || rows[0].TerminalID != item.ID {
		t.Fatalf("stored rows = %+v", rows)
	}
	snap := s.sessionsPayloadFrom(t.Context(), session.Inventory{
		Complete: true, Provenance: "test", Sessions: []session.Session{item},
	})
	if len(snap.Sessions) != 1 || snap.Sessions[0].Label != "Release helper" {
		t.Fatalf("fleet label = %+v", snap.Sessions)
	}

	cleared := act(t, s, "title", item.ID, "title-2", `{"title":" \t "}`)
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", cleared.Code, cleared.Body)
	}
	var answer contract.SessionTitleReply
	if err := json.Unmarshal(cleared.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Title != "" || answer.DisplayTitle != "Original title" {
		t.Fatalf("clear = %+v", answer)
	}
	v, _ = nextconfig.Open(s.cfg.Dir).Read()
	if rows := ownSessionTitles(v, time.Now()); len(rows) != 0 {
		t.Fatalf("clear retained %+v", rows)
	}
}

func TestSmartTitleUsesOneReceiptedTurnAndSavesItsAnswer(t *testing.T) {
	item := session.Session{ID: "%43", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		ConversationID: "conversation-43", State: session.StateIdle,
		Label: "Before", Rungs: session.LabelRungs{Conversation: "Before"}}
	s := paneServer(t, &pane{s: item})
	s.cfg = config.Config{Dir: filepath.Join(t.TempDir(), "clawdline-next")}
	s.icons = &icon.Registry{}
	if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"auto_name_assistant": "claude"}); err != nil {
		t.Fatal(err)
	}
	s.firstSessionRequest = func(got session.Session) (string, error) {
		if got.ID != item.ID {
			t.Fatalf("read first request from %q", got.ID)
		}
		return strings.Repeat("界", 2000) + " trailing words", nil
	}
	runs := 0
	s.nameSession = func(_ context.Context, text, assistant string) (string, error) {
		runs++
		if assistant != "claude" {
			t.Fatalf("assistant = %q", assistant)
		}
		if len([]byte(text)) > int(capacity.Default(capacity.IntentRequestBytes)) {
			t.Fatalf("naming input was not capped: %d bytes", len([]byte(text)))
		}
		return "  Release\n helper  ", nil
	}
	first := act(t, s, "smart-title", item.ID, "smart-1", `{}`)
	again := act(t, s, "smart-title", item.ID, "smart-1", `{}`)
	if first.Code != http.StatusOK || again.Code != http.StatusOK {
		t.Fatalf("answers = %d %s / %d %s", first.Code, first.Body, again.Code, again.Body)
	}
	if runs != 1 || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("runs=%d replay=%q", runs, again.Header().Get("Idempotent-Replayed"))
	}
	var answer contract.SessionTitleReply
	if err := json.Unmarshal(first.Body.Bytes(), &answer); err != nil || answer.Title != "Release helper" || answer.DisplayTitle != "Release helper" {
		t.Fatalf("answer = %+v (%v)", answer, err)
	}
	values, err := nextconfig.Open(s.cfg.Dir).Read()
	if err != nil {
		t.Fatal(err)
	}
	rows := ownSessionTitles(values, time.Now())
	if len(rows) != 1 || rows[0].Title != "Release helper" {
		t.Fatalf("stored rows = %+v", rows)
	}
}

func TestSmartTitleRefusesBeforeSpendingWithoutAFirstRequestOrReceipt(t *testing.T) {
	item := session.Session{ID: "%44", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
		ConversationID: "conversation-44", State: session.StateIdle}
	s := paneServer(t, &pane{s: item})
	s.cfg = config.Config{Dir: filepath.Join(t.TempDir(), "clawdline-next")}
	runs := 0
	s.nameSession = func(context.Context, string, string) (string, error) {
		runs++
		return "Should not run", nil
	}
	s.firstSessionRequest = func(session.Session) (string, error) { return "", transcript.ErrNotFound }
	if rec := act(t, s, "smart-title", item.ID, "", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing receipt = %d %s", rec.Code, rec.Body)
	}
	if rec := act(t, s, "smart-title", item.ID, "smart-empty", `{}`); rec.Code != http.StatusConflict || codeOf(t, rec) != "conversation_empty" {
		t.Fatalf("empty conversation = %d %s", rec.Code, rec.Body)
	}
	if runs != 0 {
		t.Fatalf("refused requests spent %d model turns", runs)
	}
}

func TestSmartTitleFailureDoesNotReplaceTheExistingName(t *testing.T) {
	item := session.Session{ID: "%45", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
		ConversationID: "conversation-45", State: session.StateIdle,
		Label: "Keep me", Rungs: session.LabelRungs{Conversation: "Keep me"}}
	s := paneServer(t, &pane{s: item})
	s.cfg = config.Config{Dir: filepath.Join(t.TempDir(), "clawdline-next")}
	s.firstSessionRequest = func(session.Session) (string, error) { return "name this", nil }
	s.nameSession = func(context.Context, string, string) (string, error) { return "", errors.New("provider unavailable") }
	rec := act(t, s, "smart-title", item.ID, "smart-fail", `{}`)
	if rec.Code != http.StatusBadGateway || codeOf(t, rec) != "naming_failed" {
		t.Fatalf("failure = %d %s", rec.Code, rec.Body)
	}
	if values, err := nextconfig.Open(s.cfg.Dir).Read(); err != nil || values.Exists {
		t.Fatalf("failed naming changed settings: exists=%v err=%v", values.Exists, err)
	}
}

func TestSmartTitleSaysWhenTheAssistantIsOutOfQuota(t *testing.T) {
	item := session.Session{ID: "%46", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		ConversationID: "conversation-46", State: session.StateIdle}
	s := paneServer(t, &pane{s: item})
	s.cfg = config.Config{Dir: filepath.Join(t.TempDir(), "clawdline-next")}
	s.firstSessionRequest = func(session.Session) (string, error) { return "name this", nil }
	s.nameSession = func(context.Context, string, string) (string, error) {
		return "", fmt.Errorf("%w: exit status 1", planner.ErrOutOfQuota)
	}
	rec := act(t, s, "smart-title", item.ID, "smart-quota", `{}`)
	if rec.Code != http.StatusServiceUnavailable || codeOf(t, rec) != "namer_out_of_quota" {
		t.Fatalf("out of quota = %d %s", rec.Code, rec.Body)
	}
}

func TestAnOverlongSessionTitleChangesNothing(t *testing.T) {
	item := session.Session{ID: "%42", Backend: session.BackendTmux, State: session.StateIdle}
	p := &pane{s: item}
	s := paneServer(t, p)
	s.cfg = config.Config{Dir: filepath.Join(t.TempDir(), "clawdline-next")}

	limit := int(capacity.Default(capacity.SessionTitleCharacters))
	body, _ := json.Marshal(contract.SessionTitleRequest{Title: strings.Repeat("界", limit+1)})
	rec := act(t, s, "title", item.ID, "title-long", string(body))
	if rec.Code != http.StatusBadRequest || codeOf(t, rec) != "bad_request" {
		t.Fatalf("overlong: %d %s", rec.Code, rec.Body)
	}
	if v, err := nextconfig.Open(s.cfg.Dir).Read(); err != nil || v.Exists {
		t.Fatalf("the refused write touched config: exists=%v err=%v", v.Exists, err)
	}
}

func TestSessionTitleRowsExpireAndKeepTheNewestBound(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	old := swiftstore.Seconds(float64(now.Add(-91 * 24 * time.Hour).Unix()))
	newer := swiftstore.Seconds(float64(now.Add(-time.Hour).Unix()))
	rows := []swiftstore.SessionTitle{
		{Title: "expired", TerminalID: "%1", UpdatedAt: &old},
		{Title: "kept", TerminalID: "%2", UpdatedAt: &newer},
	}
	raw, _ := json.Marshal(rows)
	v := nextconfig.Values{Exists: true, Raw: map[string]json.RawMessage{sessionTitlesKey: raw}}
	got := ownSessionTitles(v, now)
	if len(got) != 1 || got[0].Title != "kept" {
		t.Fatalf("rows = %+v", got)
	}
}
