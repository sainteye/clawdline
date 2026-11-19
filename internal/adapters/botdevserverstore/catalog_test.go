package botdevserverstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func savedDefinition(id string) servers.SavedServer {
	i := identity()
	return servers.SavedServer{ID: id, Spec: servers.Spec{Key: i.Key, Project: i.Project,
		Name: "Synthetic server", Command: "synthetic-server --foreground " + id, Directory: "/synthetic/project"}}
}

func readyCatalog(t *testing.T, db *sql.DB) *Catalog {
	t.Helper()
	if err := InstallCatalog(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	c, err := NewCatalog(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newIntent(t *testing.T, c *Catalog, saved servers.SavedServer, attempt string) (servers.Journal, servers.Record) {
	t.Helper()
	fingerprint, err := saved.Spec.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	i := servers.Identity{Key: saved.Spec.Key, Project: saved.Spec.Project, ServerID: saved.ID, AttemptID: attempt, Fingerprint: fingerprint}
	j, err := c.Journal(context.Background(), saved, i)
	if err != nil {
		t.Fatal(err)
	}
	return j, servers.Record{Identity: i, Version: 1, State: servers.Accepted}
}

func TestCatalogReadDoesNotInstallOrChangeHostState(t *testing.T) {
	db, _ := testDatabase(t)
	ctx := context.Background()
	if _, err := NewCatalog(ctx, db); pa.Code(err) != pa.Disabled {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name LIKE 'bot_devserver_%'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("read installed tables: %d %v", count, err)
	}
	if _, err := db.Exec(`PRAGMA user_version=77; CREATE TABLE unrelated_host(value TEXT); INSERT INTO unrelated_host VALUES('synthetic')`); err != nil {
		t.Fatal(err)
	}
	c := readyCatalog(t, db)
	saved := savedDefinition("first")
	if err := c.Create(ctx, saved); err != nil {
		t.Fatal(err)
	}
	rows, err := c.List(ctx, capacity.MaximumBotServerDefinitions)
	if err != nil || len(rows) != 1 || rows[0] != saved {
		t.Fatalf("definition creation admitted an attempt: %+v %v", rows, err)
	}
	var version int
	var data string
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 77 {
		t.Fatalf("host version=%d %v", version, err)
	}
	if err := db.QueryRow(`SELECT value FROM unrelated_host`).Scan(&data); err != nil || data != "synthetic" {
		t.Fatalf("host state=%s %v", data, err)
	}
}

func TestCatalogIntentAndCurrentPointerCommitOrRollbackTogether(t *testing.T) {
	db, _ := testDatabase(t)
	c := readyCatalog(t, db)
	ctx := context.Background()
	saved := savedDefinition("first")
	if err := c.Create(ctx, saved); err != nil {
		t.Fatal(err)
	}
	j, intent := newIntent(t, c, saved, "one")
	if _, err := db.Exec(`CREATE TRIGGER refuse_server_pointer BEFORE UPDATE OF current_attempt ON bot_devserver_definitions BEGIN SELECT RAISE(ABORT,'synthetic pointer failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := j.Save(ctx, 0, intent); err == nil {
		t.Fatal("injected pointer failure succeeded")
	}
	var attempts int
	var pointer string
	if err := db.QueryRow(`SELECT count(*) FROM bot_devserver_attempts`).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("failed pointer left an admitted orphan: %d %v", attempts, err)
	}
	if err := db.QueryRow(`SELECT current_attempt FROM bot_devserver_definitions`).Scan(&pointer); err != nil || pointer != "" {
		t.Fatalf("failed attempt left a current pointer: %s %v", pointer, err)
	}
	if _, err := db.Exec(`DROP TRIGGER refuse_server_pointer`); err != nil {
		t.Fatal(err)
	}
	if err := j.Save(ctx, 0, intent); err != nil {
		t.Fatal(err)
	}
	rows, err := c.List(ctx, capacity.MaximumBotServerDefinitions)
	if err != nil || len(rows) != 1 || rows[0].Attempt != intent {
		t.Fatalf("committed pointer does not identify its intent: %+v %v", rows, err)
	}
}

func TestCatalogCompetingAttemptsHaveOneDurableWinner(t *testing.T) {
	db, _ := testDatabase(t)
	c := readyCatalog(t, db)
	saved := savedDefinition("first")
	if err := c.Create(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	left, a := newIntent(t, c, saved, "left")
	right, b := newIntent(t, c, saved, "right")
	var won atomic.Int32
	var workers sync.WaitGroup
	for _, candidate := range []struct {
		journal servers.Journal
		intent  servers.Record
	}{{left, a}, {right, b}} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := candidate.journal.Save(context.Background(), 0, candidate.intent); err == nil {
				won.Add(1)
			}
		}()
	}
	workers.Wait()
	rows, err := c.List(context.Background(), capacity.MaximumBotServerDefinitions)
	if err != nil || won.Load() != 1 || len(rows) != 1 || (rows[0].Attempt != a && rows[0].Attempt != b) {
		t.Fatalf("CAS winners=%d rows=%+v %v", won.Load(), rows, err)
	}
}

func TestIndependentCatalogConnectionsCannotOverbookTheLastProcessSlot(t *testing.T) {
	db, path := testDatabase(t)
	c := readyCatalog(t, db)
	ctx := context.Background()
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000`); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < capacity.MaximumBotServerProcesses-1; n++ {
		saved := savedDefinition(fmt.Sprint(n))
		if err := c.Create(ctx, saved); err != nil {
			t.Fatal(err)
		}
		j, r := newIntent(t, c, saved, "first")
		if err := j.Save(ctx, 0, r); err != nil {
			t.Fatal(err)
		}
	}
	other, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	other.SetMaxOpenConns(1)
	if _, err := other.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		t.Fatal(err)
	}
	c2, err := NewCatalog(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	var journals []servers.Journal
	var intents []servers.Record
	for n, catalog := range []*Catalog{c, c2} {
		saved := savedDefinition(fmt.Sprintf("contender-%d", n))
		if err := catalog.Create(ctx, saved); err != nil {
			t.Fatal(err)
		}
		j, r := newIntent(t, catalog, saved, "first")
		journals, intents = append(journals, j), append(intents, r)
	}
	var workers sync.WaitGroup
	var wins atomic.Int32
	start := make(chan struct{})
	for n := range journals {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			if err := journals[n].Save(ctx, 0, intents[n]); err == nil {
				wins.Add(1)
			}
		}()
	}
	close(start)
	workers.Wait()
	var active int
	if err := db.QueryRow(`SELECT count(*) FROM bot_devserver_attempts WHERE state='accepted'`).Scan(&active); err != nil || active != capacity.MaximumBotServerProcesses || wins.Load() != 1 {
		t.Fatalf("independent connections overbooked: active=%d wins=%d %v", active, wins.Load(), err)
	}
}

func TestCatalogEnforcesPersistentDefinitionAndProcessBounds(t *testing.T) {
	db, _ := testDatabase(t)
	c := readyCatalog(t, db)
	ctx := context.Background()
	for n := 0; n < capacity.MaximumBotServerDefinitions; n++ {
		saved := savedDefinition(fmt.Sprint(n))
		if err := c.Create(ctx, saved); err != nil {
			t.Fatal(err)
		}
		if n <= capacity.MaximumBotServerProcesses {
			j, r := newIntent(t, c, saved, "first")
			err := j.Save(ctx, 0, r)
			if n < capacity.MaximumBotServerProcesses && err != nil {
				t.Fatal(err)
			}
			if n == capacity.MaximumBotServerProcesses && pa.Code(err) != pa.CapacityFull {
				t.Fatalf("persistent process bound=%v", err)
			}
		}
	}
	if err := c.Create(ctx, savedDefinition("overflow")); pa.Code(err) != pa.CapacityFull {
		t.Fatal(err)
	}
	if rows, err := c.List(ctx, 1); pa.Code(err) != pa.CapacityFull || len(rows) != 0 {
		t.Fatalf("an incomplete catalog was silently accepted: %d %v", len(rows), err)
	}
	rows, err := c.List(ctx, capacity.MaximumBotServerDefinitions)
	if err != nil || len(rows) != capacity.MaximumBotServerDefinitions {
		t.Fatal(err)
	}
	for _, saved := range rows {
		if saved.Attempt.Version == 0 {
			if err := c.Remove(ctx, saved); err != nil {
				t.Fatal(err)
			}
			if err := c.Create(ctx, saved); pa.Code(err) != pa.IdempotencyConflict {
				t.Fatal("a removed ID was reused", err)
			}
			if err := c.Create(ctx, savedDefinition("after-removal")); err != nil {
				t.Fatal("a removed clean definition did not free capacity", err)
			}
			return
		}
	}
	t.Fatal("fixture did not contain an unstarted definition")
}

func TestCatalogReopenPreservesInterruptedIntentWithoutLaunching(t *testing.T) {
	db, path := testDatabase(t)
	c := readyCatalog(t, db)
	saved := savedDefinition("first")
	ctx := context.Background()
	if err := c.Create(ctx, saved); err != nil {
		t.Fatal(err)
	}
	j, intent := newIntent(t, c, saved, "first")
	if err := j.Save(ctx, 0, intent); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c, err = NewCatalog(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	var launches atomic.Int32
	r, err := servers.OpenRegistry(ctx, registryOptions(c, &launches, nil))
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Start(ctx, saved.Spec, "another")
	if pa.Code(err) != pa.ExecutionEffectUnknown || got.Attempt.State != servers.Unknown || launches.Load() != 0 || r.Usage().Processes != 1 || r.Usage().Known {
		t.Fatalf("interrupted intent replayed: %+v %v usage=%+v", got, err, r.Usage())
	}
	if err := r.Close(ctx); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal(err)
	}
	rows, err := c.List(ctx, capacity.MaximumBotServerDefinitions)
	if err != nil || rows[0].Attempt != intent {
		t.Fatal("reopen inspection rewrote the accepted fact", err)
	}
}

type catalogProcess struct {
	waiting, done chan struct{}
	once          sync.Once
}

func (*catalogProcess) Output() io.Reader { return strings.NewReader("synthetic output\n") }
func (p *catalogProcess) WaitExit(ctx context.Context) error {
	close(p.waiting)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return nil
	}
}
func (p *catalogProcess) Close(context.Context) error {
	p.once.Do(func() { close(p.done) })
	return nil
}

func registryOptions(c servers.Catalog, launches *atomic.Int32, started chan *catalogProcess) servers.RegistryOptions {
	var ids atomic.Int32
	return servers.RegistryOptions{MachineID: identity().Key.MachineID, Lifetime: context.Background(), Catalog: c,
		ID: func() string { return fmt.Sprintf("owned-%d", ids.Add(1)) }, CleanupTimeout: time.Second,
		CheckControl: func(ctx context.Context, _ servers.Spec) error { return ctx.Err() },
		AuthorizeStart: func(ctx context.Context, _ servers.SavedServer) (func(context.Context) error, error) {
			return func(ctx context.Context) error { return ctx.Err() }, nil
		}, Launch: func(context.Context, servers.Spec) (servers.Process, error) {
			launches.Add(1)
			p := &catalogProcess{waiting: make(chan struct{}), done: make(chan struct{})}
			if started != nil {
				started <- p
			}
			return p, nil
		}}
}

func awaitCatalogProcess(t *testing.T, started <-chan *catalogProcess) {
	t.Helper()
	select {
	case p := <-started:
		select {
		case <-p.waiting:
		case <-time.After(5 * time.Second):
			t.Fatal("accepted process did not publish running")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("accepted process was not launched")
	}
}

func TestRealCatalogRegistryRestartAndOldControlsKeepTheirAttemptFence(t *testing.T) {
	db, _ := testDatabase(t)
	c := readyCatalog(t, db)
	ctx := context.Background()
	var launches atomic.Int32
	started := make(chan *catalogProcess, 2)
	r, err := servers.OpenRegistry(ctx, registryOptions(c, &launches, started))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(ctx) })
	spec := savedDefinition("first").Spec
	first, err := r.Start(ctx, spec, "first")
	if err != nil {
		t.Fatal(err)
	}
	awaitCatalogProcess(t, started)
	if err := r.Stop(ctx, spec.Key, spec.Project, first.ID, "first"); err != nil {
		t.Fatal(err)
	}
	oldRows, err := c.List(ctx, capacity.MaximumBotServerDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	old := oldRows[0]
	oldJournal, err := c.Journal(ctx, old, old.Attempt.Identity)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Start(ctx, spec, "second")
	if err != nil {
		t.Fatal("restart did not use the current persisted receipt", err)
	}
	awaitCatalogProcess(t, started)
	if second.ID != first.ID || launches.Load() != 2 {
		t.Fatal("restart replaced the definition or duplicated its launch")
	}
	if err := r.Stop(ctx, spec.Key, spec.Project, first.ID, "first"); pa.Code(err) != pa.StaleGeneration {
		t.Fatal(err)
	}
	if _, err := oldJournal.Load(ctx, old.Attempt.Identity); pa.Code(err) != pa.StaleGeneration {
		t.Fatal("historical journal rewound the current attempt", err)
	}
	if err := c.Remove(ctx, old); pa.Code(err) != pa.StaleVersion {
		t.Fatal("old removal affected the current attempt", err)
	}
	if err := r.Stop(ctx, spec.Key, spec.Project, second.ID, "second"); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove(ctx, spec.Key, spec.Project, second.ID, "second"); err != nil {
		t.Fatal(err)
	}
	rows, err := c.List(ctx, capacity.MaximumBotServerDefinitions)
	if err != nil || len(rows) != 0 {
		t.Fatalf("clean removal=%+v %v", rows, err)
	}
	var textBytes, attempts int
	if err := db.QueryRow(`SELECT length(spec) FROM bot_devserver_definitions WHERE id=?`, first.ID).Scan(&textBytes); err != nil || textBytes != 0 {
		t.Fatalf("removed executable text retained: %d %v", textBytes, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM bot_devserver_attempts`).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatalf("removal discarded historical receipts: %d %v", attempts, err)
	}
}

type lostCatalog struct{ *Catalog }
type lostCatalogJournal struct{ servers.Journal }

func (c lostCatalog) Journal(ctx context.Context, saved servers.SavedServer, i servers.Identity) (servers.Journal, error) {
	j, err := c.Catalog.Journal(ctx, saved, i)
	return lostCatalogJournal{j}, err
}
func (j lostCatalogJournal) Save(ctx context.Context, expected int64, r servers.Record) error {
	if err := j.Journal.Save(ctx, expected, r); err != nil {
		return err
	}
	if expected == 0 {
		return errors.New("synthetic lost commit acknowledgement")
	}
	return nil
}

func TestLostCatalogCommitAcknowledgementNeverStartsAProcess(t *testing.T) {
	db, _ := testDatabase(t)
	c := readyCatalog(t, db)
	var launches atomic.Int32
	ctx := context.Background()
	r, err := servers.OpenRegistry(ctx, registryOptions(lostCatalog{c}, &launches, nil))
	if err != nil {
		t.Fatal(err)
	}
	spec := savedDefinition("first").Spec
	for range 2 {
		if _, err := r.Start(ctx, spec, "same"); pa.Code(err) != pa.ExecutionEffectUnknown {
			t.Fatal(err)
		}
	}
	if launches.Load() != 0 || r.Usage().Known || r.Usage().Processes != 1 {
		t.Fatalf("lost acknowledgement did not retain the unknown owner: %+v", r.Usage())
	}
	if err := r.Close(ctx); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal(err)
	}
	rows, err := c.List(ctx, capacity.MaximumBotServerDefinitions)
	if err != nil || len(rows) != 1 || rows[0].Attempt.State != servers.Accepted || rows[0].Attempt.Version != 1 {
		t.Fatalf("lost acknowledgement did not preserve its commit: %+v %v", rows, err)
	}
}

func TestCatalogUnknownAndPartialSchemasRemainUnchanged(t *testing.T) {
	for _, mutate := range []string{
		`UPDATE bot_devserver_catalog_schema SET version=99`,
		`DELETE FROM bot_devserver_catalog_schema`,
		`DROP TABLE bot_devserver_definitions`,
		`ALTER TABLE bot_devserver_definitions ADD COLUMN future TEXT`,
		`ALTER TABLE bot_devserver_catalog_schema ADD COLUMN future TEXT`,
		`UPDATE bot_devserver_definitions SET rule_decision='unknown',rule_version=1`,
		`UPDATE bot_devserver_definitions SET rule_version=-1`,
	} {
		t.Run(mutate, func(t *testing.T) {
			db, _ := testDatabase(t)
			c := readyCatalog(t, db)
			if err := c.Create(context.Background(), savedDefinition("first")); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(mutate); err != nil {
				t.Fatal(err)
			}
			before := catalogDatabaseState(t, db)
			if _, err := NewCatalog(context.Background(), db); err == nil {
				t.Fatal("reader accepted an unknown schema")
			}
			if err := InstallCatalog(context.Background(), db); err == nil {
				t.Fatal("installer repaired or downstamped an unknown schema")
			}
			if err := c.Create(context.Background(), savedDefinition("second")); err == nil {
				t.Fatal("existing writer ignored a changed schema")
			}
			if after := catalogDatabaseState(t, db); after != before {
				t.Fatal("schema refusal changed stored schema or rows")
			}
		})
	}
}

func catalogDatabaseState(t *testing.T, db *sql.DB) string {
	t.Helper()
	tables := []string{"bot_devserver_schema", "bot_devserver_attempts", "bot_devserver_catalog_schema", "bot_devserver_definitions"}
	var state []any
	for _, table := range tables {
		var ddl string
		err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&ddl)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		state = append(state, ddl)
		rows, err := db.Query(`SELECT * FROM ` + table + ` ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for n := range values {
				pointers[n] = &values[n]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			state = append(state, values)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestCatalogRejectsCorruptCommandDataWithoutRewritingIt(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"unexpected":"synthetic"}`), []byte(`{"name":"trailing"} {}`), []byte{0xff},
		[]byte(strings.Repeat("x", capacity.MaximumBotServerSpecBytes+1)),
	} {
		t.Run(fmt.Sprint(len(raw)), func(t *testing.T) {
			db, _ := testDatabase(t)
			c := readyCatalog(t, db)
			ctx := context.Background()
			if err := c.Create(ctx, savedDefinition("first")); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE bot_devserver_definitions SET spec=?`, raw); err != nil {
				t.Fatal(err)
			}
			before := catalogDatabaseState(t, db)
			if _, err := c.List(ctx, capacity.MaximumBotServerDefinitions); pa.Code(err) != pa.Invalid {
				t.Fatalf("corrupt command data accepted: %v", err)
			}
			if after := catalogDatabaseState(t, db); after != before {
				t.Fatal("read rewrote invalid command data")
			}
		})
	}
}

func TestCatalogRefusesAnUnownedLiveReceiptWrittenAfterInstallation(t *testing.T) {
	db, _ := testDatabase(t)
	c := readyCatalog(t, db)
	ctx := context.Background()
	ledger, err := New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Save(ctx, 0, servers.Record{Identity: identity(), Version: 1, State: servers.Accepted}); err != nil {
		t.Fatal(err)
	}
	before := catalogDatabaseState(t, db)
	if _, err := c.List(ctx, capacity.MaximumBotServerDefinitions); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("an orphaned accepted receipt was hidden by the catalog", err)
	}
	if err := c.Create(ctx, savedDefinition("new")); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("new work was admitted over an unowned live receipt", err)
	}
	if after := catalogDatabaseState(t, db); after != before {
		t.Fatal("uncertain ownership refusal changed durable facts")
	}
}

func TestCatalogDoesNotHideUnownedLedgerWorkOrDanglingPointers(t *testing.T) {
	db, _ := testDatabase(t)
	ledger := ready(t, db)
	ctx := context.Background()
	r := servers.Record{Identity: identity(), Version: 1, State: servers.Accepted}
	if err := ledger.Save(ctx, 0, r); err != nil {
		t.Fatal(err)
	}
	if err := InstallCatalog(ctx, db); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("catalog installation hid an older unowned attempt", err)
	}
	r.Version, r.State = 2, servers.Stopped
	if err := ledger.Save(ctx, 1, r); err != nil {
		t.Fatal(err)
	}
	c := readyCatalog(t, db)
	saved := savedDefinition("new")
	if err := c.Create(ctx, saved); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE bot_devserver_definitions SET current_attempt='missing'`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.List(ctx, capacity.MaximumBotServerDefinitions); pa.Code(err) != pa.Invalid {
		t.Fatal("missing attempt was treated as an idle server", err)
	}
}
