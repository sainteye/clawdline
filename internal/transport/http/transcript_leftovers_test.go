package http

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// A completion card that says what was left for its session opens into the
// items: the notice typed into the root carries only the count, and the page
// served to the console carries the titles and reasons read from the task's
// result. A line that only looks like that task's notice gets nothing.
func TestACompletionNoticeCarriesWhatTheDeliveryLeft(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	b := &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir}
	s := &Server{store: st, broker: b}
	ctx := context.Background()

	settle := func(id string, leftovers []work.Leftover) orchestrator.Record {
		t.Helper()
		r := orchestrator.Record{Protocol: orchestrator.Protocol, ID: id, Kind: "custom", Assistant: "claude",
			Title: "child " + id, ProjectDir: "/p", State: orchestrator.StateBriefed, CreatedAt: time.Now(),
			Root: &orchestrator.RootRef{SessionID: "root-conv", Assistant: "claude"}}
		body, _ := json.Marshal(r)
		if _, err := st.CreateBrokerTask(ctx, store.BrokerRow{ID: id, Project: "/p", Assistant: "claude",
			State: string(r.State), CreatedAt: r.CreatedAt, SecretHash: orchestrator.HashSecret("s"),
			Record: body}, nil); err != nil {
			t.Fatal(err)
		}
		out, err := b.Settle(ctx, id, orchestrator.StateSuccess, "done", &taskdir.Result{
			Status: "success", Summary: "done", Leftovers: leftovers})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	left := []work.Leftover{
		{Title: "The paid comparison has measured evidence", Why: "Only local mocks were authorized here."},
		{Title: "This fix is integrated and released", Why: "A child does not commit or deploy."},
	}
	withLeft := settle("t-left", left)
	other := settle("t-other", []work.Leftover{{Title: "Another task's leftover is recorded"}})

	wire, err := b.NoticeWire(ctx, withLeft)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(wire, left[0].Title) {
		t.Fatalf("the typed notice carries the list, not just the count: %s", wire)
	}
	// The same notice claiming the other task's id with this task's notice id.
	borrowed := strings.Replace(wire, `"id":"t-left"`, `"id":"`+other.ID+`"`, 1)
	if borrowed == wire {
		t.Fatalf("could not forge the borrowed notice from %s", wire)
	}

	item, path, _ := recordSession(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	row := func(text string) string {
		quoted, _ := json.Marshal(text)
		return `{"type":"user","timestamp":"2026-11-07T10:00:00Z","message":{"content":` + string(quoted) + `}}` + "\n"
	}
	if err := os.WriteFile(path, []byte(row(wire)+row(borrowed)), 0o600); err != nil {
		t.Fatal(err)
	}
	page := s.transcriptPageBefore(item.ID, item, 200, 0)
	if len(page.Entries) != 2 || page.Entries[0].Notice == nil || page.Entries[1].Notice == nil {
		t.Fatalf("the two notices were not decoded as notices: %+v", page.Entries)
	}
	if page.Entries[0].Notice.LeftoverItems != nil {
		t.Fatal("the page builder attached items on its own; the route is what attaches them")
	}
	s.attachLeftovers(ctx, page.Entries)

	got := page.Entries[0].Notice
	if got.Leftovers != 2 || len(got.LeftoverItems) != 2 ||
		got.LeftoverItems[0].Title != left[0].Title || got.LeftoverItems[0].Why != left[0].Why ||
		got.LeftoverItems[1].Title != left[1].Title || got.LeftoverItems[1].Why != left[1].Why {
		t.Fatalf("the notice did not carry its delivery's leftovers: %+v", got)
	}
	if items := page.Entries[1].Notice.LeftoverItems; len(items) != 0 {
		t.Fatalf("a notice whose notice_id belongs to another task borrowed its leftovers: %+v", items)
	}

	// A broker that cannot find the task leaves the card as it was.
	(&Server{store: st, broker: &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir}}).attachLeftovers(ctx, nil)
	page.Entries[0].Notice.LeftoverItems = nil
	page.Entries[0].Notice.Task.ID = "t-missing"
	s.attachLeftovers(ctx, page.Entries)
	if page.Entries[0].Notice.LeftoverItems != nil {
		t.Fatalf("an unknown task produced items: %+v", page.Entries[0].Notice.LeftoverItems)
	}
}
