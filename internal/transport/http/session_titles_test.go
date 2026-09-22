package http

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
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
