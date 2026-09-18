package board

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// A log that cannot be read is said to be unreadable; a line still being
// written is left for the next read; a card id never leaves the directory.
func TestHistoryReadsWhatIsThereAndSaysWhatIsNot(t *testing.T) {
	root := t.TempDir()
	board := filepath.Join(root, "Clawdline", "project-board.json")
	dir := HistoryDir(board)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("torn.jsonl", `{"kind":"item_created","at":10}`+"\n"+`{"kind":"span_started","at":20}`+"\n"+`{"kind":"checkl`)
	write("broken.jsonl", `{"kind":"item_created","at":10}`+"\n"+`not json`+"\n"+`{"kind":"span_started","at":20}`+"\n")
	write("timeless.jsonl", `{"kind":"item_created"}`+"\n")
	// Beside the board, where "../escape" would point if it were a path.
	if err := os.WriteFile(filepath.Join(root, "Clawdline", "escape.jsonl"),
		[]byte(`{"kind":"item_created","at":1}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := OpenHistory(dir)
	got, source := h.Read([]string{"torn", "broken", "timeless", "none", "../escape"})
	if torn := got["torn"]; torn == nil || !torn.Readable || len(torn.Events) != 2 || torn.Events[1].At != 20 {
		t.Fatalf("a last line still being written: %+v", torn)
	}
	for _, id := range []string{"broken", "timeless", "../escape"} {
		if h := got[id]; h == nil || h.Readable {
			t.Fatalf("%s: %+v, want unreadable", id, h)
		}
	}
	if _, ok := got["none"]; ok {
		t.Fatalf("a card with no log has no history, not an empty one")
	}
	if source != (work.HistorySource{Status: "ok", Files: 3, Unreadable: 3}) {
		t.Fatalf("source: %+v", source)
	}

	// The writer finishes its line: the next read sees it.
	f, err := os.OpenFile(filepath.Join(dir, "torn.jsonl"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`ist_updated","at":30}` + "\n")
	f.Close()
	got, _ = h.Read([]string{"torn"})
	if torn := got["torn"]; len(torn.Events) != 3 || torn.Events[2].At != 30 {
		t.Fatalf("after the line was finished: %+v", torn)
	}

	// No log directory at all is not a board where nothing ever moved.
	got, source = OpenHistory(filepath.Join(root, "nowhere")).Read([]string{"torn"})
	if source.Status != "absent" || got["torn"] == nil || got["torn"].Readable {
		t.Fatalf("missing directory: %+v %+v", source, got["torn"])
	}
}

func TestTrackCardsCopiesTheFacts(t *testing.T) {
	user, broker, queued, archive := "user", "broker", "queued", "archive"
	ended := 5.0
	item := StoredItem{
		ID: "a", Key: "CLA-1", ProjectID: "p", Title: "T", Type: "bug", State: "execution",
		CreatedAt: 1, UpdatedAt: 2,
		Links: []StoredLink{
			{Kind: "task", TargetID: "t1", Source: &broker, AttemptState: &queued},
			{Kind: "task", TargetID: "t2"},
			{Kind: "session", TargetID: "s"},
		},
		Spans:              []StoredSpan{{SessionID: "open"}, {SessionID: "closed", EndedAt: &ended}},
		Checklist:          []StoredChecklistRow{{Status: "done"}, {Status: "pending"}},
		Obligations:        []StoredObligation{{ActorKind: &user}, {Resolved: true}},
		SessionDeliveries:  []StoredSessionDelivery{{}},
		Evidence:           []StoredEvidence{{}, {}},
		CatalogDisposition: &StoredCatalogDisposition{Audience: archive},
	}
	state := &StoredState{Items: []StoredItem{item}}
	history := &work.History{Readable: true}
	cards := TrackCards(state, map[string]*work.History{"a": history})
	if len(cards) != 1 {
		t.Fatalf("cards: %d", len(cards))
	}
	c := cards[0]
	want := ProgressOf(item, state.Items)
	if c.Progress != (work.Progress{State: want.State, Group: want.Group, Active: want.Active}) {
		t.Fatalf("progress: %+v, want %+v", c.Progress, want)
	}
	if c.Audience != "archive" || c.Type != "bug" || c.Key != "CLA-1" || c.CreatedAt != 1 || c.UpdatedAt != 2 ||
		c.History != history || c.Deliveries != 1 || c.Evidence != 2 {
		t.Fatalf("card: %+v", c)
	}
	if len(c.Attempts) != 2 || c.Attempts[0] != (work.Attempt{Source: "broker", State: "queued"}) ||
		c.Attempts[1] != (work.Attempt{Source: "unknown", State: ""}) {
		t.Fatalf("only task links are attempts, and an unnamed source is the old app's unknown: %+v", c.Attempts)
	}
	if len(c.Spans) != 2 || !c.Spans[0].Open || c.Spans[1].Open {
		t.Fatalf("spans: %+v", c.Spans)
	}
	if len(c.Checklist) != 2 || c.Checklist[0] != "done" {
		t.Fatalf("checklist: %+v", c.Checklist)
	}
	if len(c.Obligations) != 2 || c.Obligations[0] != (work.Obligation{ActorKind: "user"}) || !c.Obligations[1].Resolved {
		t.Fatalf("obligations: %+v", c.Obligations)
	}
}
