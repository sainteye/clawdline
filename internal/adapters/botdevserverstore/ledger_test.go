package botdevserverstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	botstore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	_ "modernc.org/sqlite"
)

func testDatabase(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "synthetic.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func identity() servers.Identity {
	return servers.Identity{Key: pa.BotKey{MachineID: "synthetic-machine", BotID: "synthetic-bot"},
		Project:  pa.ProjectScope{MachineID: "synthetic-machine", ProjectID: "synthetic-project", CanonicalID: "synthetic-repository", Kind: pa.RepositoryProject},
		ServerID: "synthetic-server", AttemptID: "synthetic-attempt", Fingerprint: strings.Repeat("a", 64)}
}

func ready(t *testing.T, db *sql.DB) *Ledger {
	t.Helper()
	if err := Install(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReadDoesNotInstallAndInstallationPreservesTheHost(t *testing.T) {
	db, _ := testDatabase(t)
	ctx := context.Background()
	if _, err := db.Exec(`PRAGMA user_version=77; CREATE TABLE unrelated_host (value TEXT); INSERT INTO unrelated_host VALUES('synthetic-host')`); err != nil {
		t.Fatal(err)
	}
	if _, err := New(ctx, db); pa.Code(err) != pa.Disabled {
		t.Fatalf("absent ledger read=%v", err)
	}
	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name LIKE 'bot_devserver_%'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("read installed the ledger")
	}
	s := ready(t, db)
	if err := Install(ctx, db); err != nil {
		t.Fatal(err)
	}
	if r, err := s.Load(ctx, identity()); err != nil || r.Version != 0 {
		t.Fatalf("new source ledger=%+v %v", r, err)
	}
	var version int
	var host string
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 77 {
		t.Fatalf("host version=%d %v", version, err)
	}
	if err := db.QueryRow(`SELECT value FROM unrelated_host`).Scan(&host); err != nil || host != "synthetic-host" {
		t.Fatalf("host data changed: %q %v", host, err)
	}
}

func TestInterruptedAcceptedAttemptSurvivesDatabaseReopenWithoutLaunch(t *testing.T) {
	db, path := testDatabase(t)
	s := ready(t, db)
	r := servers.Record{Identity: identity(), Version: 1, State: servers.Accepted}
	if err := s.Save(context.Background(), 0, r); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s = ready(t, reopened)
	var launches atomic.Int32
	owner, err := servers.Open(context.Background(), servers.Options{Identity: r.Identity, Lifetime: context.Background(), Journal: s,
		CleanupTimeout: time.Second, Check: func(context.Context) error { return nil },
		Launch: func(context.Context) (servers.Process, error) { launches.Add(1); return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if got := owner.Snapshot(); got.State != servers.Unknown || got.Failure != "owner_lost" {
		t.Fatalf("interrupted source record claimed live ownership: %+v", got)
	}
	if _, err := owner.Start(context.Background()); pa.Code(err) != pa.ExecutionEffectUnknown || launches.Load() != 0 {
		t.Fatalf("restart replayed accepted intent: %v launches=%d", err, launches.Load())
	}
	if err := owner.Close(context.Background()); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("unobserved process owner was declared clean")
	}
	got, err := s.Load(context.Background(), r.Identity)
	if err != nil || got != r {
		t.Fatalf("read/reopen rewrote the original accepted fact: %+v %v", got, err)
	}
}

func TestCASHasOneWinnerAndChangedApprovalCannotSelectAnEmptyAttempt(t *testing.T) {
	db, _ := testDatabase(t)
	s := ready(t, db)
	r := servers.Record{Identity: identity(), Version: 1, State: servers.Accepted}
	results := make(chan error, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() { defer workers.Done(); <-start; results <- s.Save(context.Background(), 0, r) }()
	}
	close(start)
	workers.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if pa.Code(err) != pa.StaleVersion {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("one accepted intent has %d winners", winners)
	}
	for _, change := range []func(*servers.Identity){
		func(i *servers.Identity) { i.Fingerprint = strings.Repeat("b", 64) },
		func(i *servers.Identity) { i.Project.CanonicalID = "changed-project" },
	} {
		i := r.Identity
		change(&i)
		if _, err := s.Load(context.Background(), i); pa.Code(err) != pa.ScopeDenied {
			t.Fatalf("changed identity looked like a never-started attempt: %v", err)
		}
		changed := r
		changed.Identity = i
		if err := s.Save(context.Background(), 0, changed); pa.Code(err) != pa.ScopeDenied {
			t.Fatalf("changed payload replaced original intent: %v", err)
		}
	}
	if got, err := s.Load(context.Background(), r.Identity); err != nil || got != r {
		t.Fatalf("CAS loser changed original fact: %+v %v", got, err)
	}
}

func TestUnknownAndPartialLedgersAreNotRepairedOrStampedDown(t *testing.T) {
	for _, state := range []string{"future", "marker missing", "attempts missing"} {
		t.Run(state, func(t *testing.T) {
			db, _ := testDatabase(t)
			s := ready(t, db)
			r := servers.Record{Identity: identity(), Version: 1, State: servers.Accepted}
			if err := s.Save(context.Background(), 0, r); err != nil {
				t.Fatal(err)
			}
			query := map[string]string{"future": `UPDATE bot_devserver_schema SET version=2`, "marker missing": `DROP TABLE bot_devserver_schema`, "attempts missing": `DROP TABLE bot_devserver_attempts`}[state]
			if _, err := db.Exec(query); err != nil {
				t.Fatal(err)
			}
			want := pa.Invalid
			if state == "future" {
				want = pa.Unsupported
			}
			if err := Install(context.Background(), db); pa.Code(err) != want {
				t.Fatalf("install(%s)=%v", state, err)
			}
			if _, err := New(context.Background(), db); pa.Code(err) != want {
				t.Fatalf("new(%s)=%v", state, err)
			}
			if _, err := s.Load(context.Background(), r.Identity); pa.Code(err) != want {
				t.Fatalf("existing reader crossed changed schema: %v", err)
			}
			r.Version, r.State = 2, servers.Running
			if err := s.Save(context.Background(), 1, r); pa.Code(err) != want {
				t.Fatalf("writer crossed changed schema: %v", err)
			}
			var count int
			if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('bot_devserver_schema','bot_devserver_attempts')`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if state != "future" && count != 1 {
				t.Fatal("partial source authority was silently recreated")
			}
			if state == "future" {
				var version int
				if err := db.QueryRow(`SELECT version FROM bot_devserver_schema`).Scan(&version); err != nil {
					t.Fatal(err)
				}
				if version != 2 {
					t.Fatal("future source format was stamped down")
				}
			}
		})
	}
}

func TestFailedWriteAndTerminalReactivationPreserveThePriorFact(t *testing.T) {
	db, _ := testDatabase(t)
	s := ready(t, db)
	r := servers.Record{Identity: identity(), Version: 1, State: servers.Accepted}
	if err := s.Save(context.Background(), 0, r); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER synthetic_failure BEFORE UPDATE ON bot_devserver_attempts BEGIN SELECT RAISE(ABORT,'synthetic failure'); END`); err != nil {
		t.Fatal(err)
	}
	next := r
	next.Version, next.State = 2, servers.Running
	if err := s.Save(context.Background(), 1, next); err == nil {
		t.Fatal("injected database failure was hidden")
	}
	if got, err := s.Load(context.Background(), r.Identity); err != nil || got != r {
		t.Fatalf("failed write changed the intent: %+v %v", got, err)
	}
	if _, err := db.Exec(`DROP TRIGGER synthetic_failure`); err != nil {
		t.Fatal(err)
	}
	next.State = servers.Unknown
	if err := s.Save(context.Background(), 1, next); err != nil {
		t.Fatal(err)
	}
	reactivated := next
	reactivated.Version, reactivated.State = 3, servers.Running
	if err := s.Save(context.Background(), 2, reactivated); pa.Code(err) != pa.Invalid {
		t.Fatalf("unknown fact reactivated: %v", err)
	}
	if got, err := s.Load(context.Background(), r.Identity); err != nil || got != next {
		t.Fatalf("refusal replaced the unknown fact: %+v %v", got, err)
	}
}

func TestUnknownTableShapeCannotReuseAnOldMarker(t *testing.T) {
	db, _ := testDatabase(t)
	s := ready(t, db)
	r := servers.Record{Identity: identity(), Version: 1, State: servers.Accepted}
	if err := s.Save(context.Background(), 0, r); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE bot_devserver_attempts ADD COLUMN future_fact TEXT`); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), db); pa.Code(err) != pa.Unsupported {
		t.Fatalf("install claimed an unfamiliar shape was current: %v", err)
	}
	if _, err := New(context.Background(), db); pa.Code(err) != pa.Unsupported {
		t.Fatalf("new claimed an unfamiliar shape was current: %v", err)
	}
	if _, err := s.Load(context.Background(), r.Identity); pa.Code(err) != pa.Unsupported {
		t.Fatalf("existing reader crossed a changed shape: %v", err)
	}
	next := r
	next.Version, next.State = 2, servers.Running
	if err := s.Save(context.Background(), 1, next); pa.Code(err) != pa.Unsupported {
		t.Fatalf("existing writer crossed a changed shape: %v", err)
	}
	var version int64
	var state string
	var future any
	if err := db.QueryRow(`SELECT revision,state,future_fact FROM bot_devserver_attempts`).Scan(&version, &state, &future); err != nil || version != 1 || state != string(servers.Accepted) {
		t.Fatalf("shape refusal changed or discarded stored facts: %d %s %v", version, state, err)
	}
}

func TestLedgerDoesNotChangeTheRealBotArchiveOrSchema(t *testing.T) {
	db, _ := testDatabase(t)
	ctx := context.Background()
	if err := (botstore.Installer{}).Install(ctx, db); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("../projectassistantstore/testdata/schema1-archive.json")
	if err != nil {
		t.Fatal(err)
	}
	var archive botstore.Archive
	if err := json.Unmarshal(body, &archive); err != nil {
		t.Fatal(err)
	}
	bots := botstore.New(db)
	if err := bots.RestoreArchive(ctx, archive); err != nil {
		t.Fatal(err)
	}
	before, err := bots.ExportArchive(ctx, archive.Data.Bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	var beforeVersion int
	if err := db.QueryRow(`SELECT version FROM project_assistant_schema`).Scan(&beforeVersion); err != nil || beforeVersion != pa.SchemaVersion || before.Manifest.SchemaVersion != beforeVersion || before.Data.SchemaVersion != beforeVersion {
		t.Fatalf("fixture does not establish the current Bot schema: %d %v", beforeVersion, err)
	}
	s := ready(t, db)
	i := identity()
	i.Key, i.Project = before.Data.Bot.Key, before.Data.Bot.Project
	r := servers.Record{Identity: i, Version: 1, State: servers.Accepted}
	if err := s.Save(ctx, 0, r); err != nil {
		t.Fatal(err)
	}
	r.Version, r.State = 2, servers.Stopped
	if err := s.Save(ctx, 1, r); err != nil {
		t.Fatal(err)
	}
	c := readyCatalog(t, db)
	saved := savedDefinition("catalog-archive-boundary")
	saved.Spec.Key, saved.Spec.Project = before.Data.Bot.Key, before.Data.Bot.Project
	if err := c.Create(ctx, saved); err != nil {
		t.Fatal(err)
	}
	j, intent := newIntent(t, c, saved, "first")
	if err := j.Save(ctx, 0, intent); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := db.QueryRow(`SELECT version FROM project_assistant_schema`).Scan(&version); err != nil || version != beforeVersion {
		t.Fatalf("source-local execution fact changed the Bot schema: %d -> %d %v", beforeVersion, version, err)
	}
	after, err := bots.ExportArchive(ctx, before.Data.Bot.Key)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("source-local execution fact changed the portable Bot or archive manifest: %v", err)
	}
}
