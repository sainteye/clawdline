package projectassistanthost

import (
	"context"
	"path/filepath"
	"testing"

	hoststore "github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type hostReadViewPort struct {
	view   app.ReadView
	during func()
	calls  int
}

func (p *hostReadViewPort) Read(context.Context, app.ReadViewQuery) (app.ReadView, error) {
	p.calls++
	if p.during != nil {
		p.during()
	}
	return p.view, nil
}

func TestHostReadViewOptionalPortAndLateResultOwnership(t *testing.T) {
	f := newHostFixture(t)
	p := &hostReadViewPort{}
	f.o.ReadViews = p
	f.o.ReadViewByteLimit = cloudops.ReadPlaintextBytesLimit
	f.start(t)
	if p.calls != 0 || f.h.service != nil || f.h.engine != nil || f.factories.Load() != 0 {
		t.Fatal("default off initialized feature or provider")
	}
	f.enable(t)
	b := f.create(t, "read-only")
	s, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	_, c, _, err := s.Get(context.Background(), b.Key)
	if err != nil {
		t.Fatal(err)
	}
	p.view = app.ReadView{Mode: pa.ReadViewSnapshot, SourceEpoch: "fixture-reader", Head: app.ReadViewHead{Key: b.Key, Project: b.Project, Conversation: c, CurrentFence: pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID}}, Snapshot: &app.ReadSnapshot{Bot: b}}
	q := pa.ReadViewRequest{Key: b.Key, ConversationID: c.ID, Mode: pa.ReadViewSnapshot, PageSize: 100}
	r, err := s.View(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.h.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = r.Revalidate(context.Background()); err == nil {
		t.Fatal("late interactive result published after disable")
	}
	history, err := s.View(context.Background(), q)
	if err != nil || history.Source.Access != pa.HistoryOnly {
		t.Fatalf("disabled history unavailable: %+v %v", history.Source, err)
	}
	if f.h.engine != nil || f.factories.Load() != 0 || f.launcher.calls.Load() != 0 {
		t.Fatal("read created runtime/provider/native work")
	}
	if err = f.h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = history.Revalidate(context.Background()); pa.Code(err) != pa.Disabled {
		t.Fatalf("closed host published late history: %v", err)
	}
	if _, err = s.View(context.Background(), q); pa.Code(err) != pa.Disabled {
		t.Fatalf("closed host accepted new history: %v", err)
	}
}

func TestHostWithoutReadViewCompositionKeepsExistingMethods(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "legacy")
	s, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	_, c, _, err := s.Get(context.Background(), b.Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.View(context.Background(), pa.ReadViewRequest{Key: b.Key, ConversationID: c.ID, Mode: pa.ReadViewSnapshot, PageSize: 100}); pa.Code(err) != pa.Unsupported {
		t.Fatal(err)
	}
	if f.h.engine != nil || f.factories.Load() != 0 {
		t.Fatal("optional view changed old metadata behavior")
	}
}

func TestHostReadViewBorrowsReaderAndRebuildExpiresEpoch(t *testing.T) {
	f := newHostFixture(t)
	f.o.Reader = f.st.ProjectAssistantReader()
	f.o.ReadViewByteLimit = cloudops.ReadPlaintextBytesLimit
	f.start(t)
	if f.h.service != nil {
		t.Fatal("fresh default-off constructed SQL view")
	}
	f.enable(t)
	b := f.create(t, "sql-reader")
	s, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	_, c, _, err := s.Get(context.Background(), b.Key)
	if err != nil {
		t.Fatal(err)
	}
	q := pa.ReadViewRequest{Key: b.Key, ConversationID: c.ID, Mode: pa.ReadViewSnapshot, PageSize: 100}
	r, err := s.View(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	// The enabled Host's routine tick and restart recovery read this same pool
	// concurrently, so its InUse count cannot be attributed to the View. The
	// transaction-release check runs on a pool only a never-enabled Host uses.
	if _, err = f.o.Reader.Exec(`CREATE TABLE reader_cannot_write (id INTEGER)`); err == nil {
		t.Fatal("Host read port bypassed query_only")
	}
	if err = f.h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	var one int
	if err = f.o.Reader.QueryRow(`SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Fatal("Host closed borrowed reader", err)
	}
	// Host replacement reuses the same database pool but constructs a new
	// ViewStore incarnation. Disabling alone retains the existing read owner.
	f.start(t)
	s, err = f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.View(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if next.View.SourceEpoch == r.View.SourceEpoch || next.Source.Access != pa.HistoryOnly {
		t.Fatal("reader rebuild reused epoch or lost history access")
	}
	q.Mode = pa.ReadViewEvents
	q.SourceEpoch = r.View.SourceEpoch
	if _, err = s.View(context.Background(), q); pa.Code(err) != pa.CursorExpired {
		t.Fatalf("old reader epoch accepted: %v", err)
	}
	if f.factories.Load() != 0 || f.launcher.calls.Load() != 0 {
		t.Fatal("history read initialized provider/native work")
	}

	// Reopen the SQLite store so the next Host borrows fresh pools. That Host is
	// never enabled, so it starts no background readers, and the earlier Host's
	// goroutines hold the closed pools, not these. Every connection checked out
	// of the fresh reader after View returns therefore belongs to View's path.
	if err = f.h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	var dbSequence int
	var dbName, dbPath string
	if err = f.o.Database.QueryRow("PRAGMA database_list").Scan(&dbSequence, &dbName, &dbPath); err != nil || dbName != "main" || dbPath == "" {
		t.Fatal("fixture database path", err, dbName)
	}
	if err = f.st.Close(); err != nil {
		t.Fatal(err)
	}
	if f.st, err = hoststore.Open(filepath.Dir(dbPath)); err != nil {
		t.Fatal(err)
	}
	old := f.o.Broker
	f.o.Database = f.st.ProjectAssistantDatabase()
	f.o.Reader = f.st.ProjectAssistantReader()
	f.o.Broker = &orchestrator.Broker{Store: f.st, Tasks: old.Tasks, Git: old.Git, Dir: old.Dir, Launcher: old.Launcher, ScreenReadEvery: old.ScreenReadEvery, Terminal: old.Terminal, Live: old.Live, Type: old.Type, Screen: old.Screen}
	f.start(t)
	s, err = f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	q.Mode, q.SourceEpoch = pa.ReadViewSnapshot, ""
	isolated, err := s.View(context.Background(), q)
	if err != nil || isolated.Source.Access != pa.HistoryOnly {
		t.Fatalf("reopened history unavailable: %+v %v", isolated.Source, err)
	}
	if f.o.Reader.Stats().InUse != 0 {
		t.Fatal("SQL reader retained transaction after returning facts")
	}
	if f.factories.Load() != 0 || f.launcher.calls.Load() != 0 {
		t.Fatal("reopened history read initialized provider/native work")
	}
}
