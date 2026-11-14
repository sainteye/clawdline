package projectassistantstore

import (
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func memoryWrite(b pa.Bot, revision int64, id, source, text string) app.MemoryWrite {
	return app.MemoryWrite{Command: command(b.Key, pa.MemorySaveOperation, id), ExpectedRevision: revision, Entry: pa.MemoryEntry{ID: id, Key: b.Key, Project: b.Project, Text: text, Revision: revision, Sources: []pa.MemorySource{{ID: source, Key: b.Key, Project: b.Project, Kind: "message", Revision: revision}}}}
}

func TestMemoryForgetFiltersProvenanceAndCannotResurrect(t *testing.T) {
	db, s, path := testDB(t)
	b := seed(t, s, "one")
	populate(t, s, b, pa.EffectDelivered)
	w := memoryWrite(b, 1, "fact", "message", "A remembered fact")
	r, err := s.Save(testContext, w)
	if err != nil || r.Revision != 2 {
		t.Fatalf("save %+v %v", r, err)
	}
	snap, err := s.Snapshot(testContext, b.Key)
	if err != nil || snap.Text != w.Entry.Text || len(snap.Sources) != 1 {
		t.Fatalf("snapshot %+v %v", snap, err)
	}
	forget := app.ForgetRequest{Command: command(b.Key, pa.ForgetOperation, "forget"), Project: b.Project, ExpectedRevision: 2, SourceIDs: []string{"message"}}
	r, err = s.Forget(testContext, forget)
	if err != nil || r.Revision != 3 || r.ForgottenThrough != 3 {
		t.Fatalf("forget %+v %v", r, err)
	}
	snap, err = s.Snapshot(testContext, b.Key)
	if err != nil || snap.Text != "" || len(snap.Sources) != 0 {
		t.Fatalf("forgotten snapshot %+v %v", snap, err)
	}
	binding, _ := s.Binding(testContext, b.Key, "binding")
	requireCode(t, pa.CheckOpaqueResume(binding, r), pa.ResumeForbiddenAfterForget)
	_, err = s.Save(testContext, memoryWrite(b, 3, "new", "message", "resurrect"))
	requireCode(t, err, pa.ContextStale)
	resurrect := memoryWrite(b, 3, "fact", "new-source", "resurrect entry")
	resurrect.Command = command(b.Key, pa.MemorySaveOperation, "resurrect")
	_, err = s.Save(testContext, resurrect)
	requireCode(t, err, pa.ContextStale)
	old, err := s.Save(testContext, w)
	if err != nil || old.Revision != 2 {
		t.Fatalf("replayed save lost old result %+v %v", old, err)
	}
	_, err = s.Save(testContext, memoryWrite(b, 3, "safe", "safe-source", "Safe fact"))
	if err != nil {
		t.Fatal(err)
	}
	old, err = s.Forget(testContext, forget)
	if err != nil || old.Revision != 3 {
		t.Fatalf("replayed forget %+v %v", old, err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	again := New(db2)
	snap, err = again.Snapshot(testContext, b.Key)
	if err != nil || snap.Text != "Safe fact" || !contains(snap.Memory.ForgottenSourceIDs, "message") {
		t.Fatalf("restart %+v %v", snap, err)
	}
	page, err := again.Messages(testContext, app.HistoryQuery{Key: b.Key, ConversationID: b.ConversationID, PageSize: 1})
	if err != nil || page.Items[0].Text != "Hello" {
		t.Fatalf("forget removed readable history %+v %v", page, err)
	}
}

func TestMemoryIsolationAndStaleWrites(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	other := seed(t, s, "other")
	w := memoryWrite(b, 1, "fact", "message", "text")
	w.Entry.Sources[0].Key = other.Key
	_, err := s.Save(testContext, w)
	requireCode(t, err, pa.ContextStale)
	w = memoryWrite(b, 1, "fact", "message", "text")
	w.Entry.Project.ProjectID = "foreign"
	_, err = s.Save(testContext, w)
	requireCode(t, err, pa.ScopeDenied)
	w = memoryWrite(b, 1, "fact", "message", "text")
	if _, err = s.Save(testContext, w); err != nil {
		t.Fatal(err)
	}
	_, err = s.Save(testContext, memoryWrite(b, 1, "stale", "message", "text"))
	requireCode(t, err, pa.StaleVersion)
	_, err = s.Forget(testContext, app.ForgetRequest{Command: command(b.Key, pa.ForgetOperation, "foreign"), Project: other.Project, ExpectedRevision: 2, SourceIDs: []string{"message"}})
	if err == nil { // Same canonical scope is valid for both Bots; the command key still fences writes.
		snap, e := s.Snapshot(testContext, other.Key)
		if e != nil || snap.Memory.Revision != 1 {
			t.Fatalf("cross-Bot mutation %+v %v", snap, e)
		}
	} else {
		t.Fatal(err)
	}
	foreign := b.Project
	foreign.CanonicalID = "foreign"
	_, err = s.Forget(testContext, app.ForgetRequest{Command: command(b.Key, pa.ForgetOperation, "foreign-project"), Project: foreign, ExpectedRevision: 3, SourceIDs: []string{"message"}})
	requireCode(t, err, pa.ScopeDenied)
}

func TestWithCurrentSerializesForgetWithoutHoldingTransaction(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "one")
	if _, err := s.Save(testContext, memoryWrite(b, 1, "fact", "message", "remember")); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	callbackDone := make(chan error, 1)
	forgetDone := make(chan error, 1)
	var calls atomic.Int32
	sentinel := errors.New("stdin effect may be unknown")
	go func() {
		callbackDone <- s.WithCurrent(testContext, b.Key, 2, func(snap pa.ContextSnapshot) error {
			calls.Add(1)
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM project_assistant_bots`).Scan(&n); err != nil {
				return err
			}
			close(entered)
			<-release
			return sentinel
		})
	}()
	<-entered
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := s.Forget(testContext, app.ForgetRequest{Command: command(b.Key, pa.ForgetOperation, "forget"), Project: b.Project, ExpectedRevision: 2, SourceIDs: []string{"message"}})
		forgetDone <- err
	}()
	<-started
	close(release)
	if err := <-callbackDone; !errors.Is(err, sentinel) {
		t.Fatalf("callback error %v", err)
	}
	if err := <-forgetDone; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("callback replayed %d", calls.Load())
	}
	err := s.WithCurrent(testContext, b.Key, 2, func(pa.ContextSnapshot) error { calls.Add(1); return nil })
	requireCode(t, err, pa.ContextStale)
	if calls.Load() != 1 {
		t.Fatal("stale callback invoked")
	}
	err = s.WithCurrent(testContext, b.Key, 3, func(snap pa.ContextSnapshot) error {
		if snap.Text != "" {
			t.Fatal("forgotten fact sent")
		}
		calls.Add(1)
		return nil
	})
	if err != nil || calls.Load() != 2 {
		t.Fatalf("fresh callback %v", err)
	}
}

func TestMemoryLateFailureRollsBackTombstones(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "one")
	if _, err := s.Save(testContext, memoryWrite(b, 1, "fact", "message", "remember")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_memory_receipt BEFORE INSERT ON project_assistant_receipts BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := s.Forget(testContext, app.ForgetRequest{Command: command(b.Key, pa.ForgetOperation, "forget"), Project: b.Project, ExpectedRevision: 2, SourceIDs: []string{"message"}})
	if err == nil {
		t.Fatal("expected injected failure")
	}
	snap, err := s.Snapshot(testContext, b.Key)
	if err != nil || snap.Memory.Revision != 2 || snap.Text != "remember" || len(snap.Memory.ForgottenSourceIDs) != 0 {
		t.Fatalf("forget leaked %+v %v", snap, err)
	}
}

// A Bot keeps at most pa.MaxMemoryEntries live entries: one more is refused,
// changing an existing one is not, and forgetting one makes room.
func TestMemoryEntriesStopAtTheirBound(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	revision := int64(1)
	for i := 0; i < pa.MaxMemoryEntries; i++ {
		id := fmt.Sprintf("fact-%d", i)
		r, err := s.Save(testContext, memoryWrite(b, revision, id, "source-"+id, "Fact "+id))
		if err != nil {
			t.Fatal(i, err)
		}
		revision = r.Revision
	}
	_, err := s.Save(testContext, memoryWrite(b, revision, "one-more", "source-one-more", "One more"))
	requireCode(t, err, pa.CapacityFull)
	changed := memoryWrite(b, revision, "fact-0", "source-fact-0", "Fact changed")
	changed.Command = command(b.Key, pa.MemorySaveOperation, "change-0")
	r, err := s.Save(testContext, changed)
	if err != nil {
		t.Fatal("changing an entry at the bound was refused", err)
	}
	r, err = s.Forget(testContext, app.ForgetRequest{Command: command(b.Key, pa.ForgetOperation, "forget-1"), Project: b.Project, ExpectedRevision: r.Revision, SourceIDs: []string{"fact-1"}})
	if err != nil {
		t.Fatal(err)
	}
	more := memoryWrite(b, r.Revision, "one-more", "source-one-more", "One more")
	more.Command = command(b.Key, pa.MemorySaveOperation, "one-more-after")
	if _, err = s.Save(testContext, more); err != nil {
		t.Fatal("forgetting did not make room", err)
	}
}
