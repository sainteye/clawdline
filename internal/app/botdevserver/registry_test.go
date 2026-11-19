package botdevserver

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type catalogFixture struct {
	mu       sync.Mutex
	rows     map[string]SavedServer
	journals map[Identity]*journalFixture
}

func (c *catalogFixture) List(context.Context, int) ([]SavedServer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var rows []SavedServer
	for _, r := range c.rows {
		rows = append(rows, r)
	}
	return rows, nil
}
func (c *catalogFixture) Create(_ context.Context, r SavedServer) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.rows[r.ID]; exists {
		return failure(pa.IdempotencyConflict, "synthetic_definition")
	}
	c.rows[r.ID] = r
	return nil
}
func (c *catalogFixture) Remove(_ context.Context, r SavedServer) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rows[r.ID] != r || activeState(r.Attempt.State) {
		return failure(pa.StaleGeneration, "synthetic_definition")
	}
	delete(c.rows, r.ID)
	return nil
}
func (c *catalogFixture) Journal(_ context.Context, _ SavedServer, i Identity) (Journal, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	j := c.journals[i]
	if j == nil {
		j = &journalFixture{changed: make(chan Record, 16)}
		c.journals[i] = j
	}
	return &catalogJournalFixture{c, j}, nil
}

type catalogJournalFixture struct {
	c *catalogFixture
	j *journalFixture
}

func (j *catalogJournalFixture) Load(ctx context.Context, i Identity) (Record, error) {
	return j.j.Load(ctx, i)
}
func (j *catalogJournalFixture) Save(ctx context.Context, version int64, r Record) error {
	// Model the Catalog's required atomic current-attempt pointer. The real
	// SQLite adapter is qualified separately; this fixture cannot prove it.
	j.c.mu.Lock()
	defer j.c.mu.Unlock()
	err := j.j.Save(ctx, version, r)
	j.j.mu.Lock()
	saved := j.c.rows[r.Identity.ServerID]
	saved.Attempt = j.j.record
	j.j.mu.Unlock()
	j.c.rows[saved.ID] = saved
	return err
}

func registryFixture(t *testing.T) (RegistryOptions, *catalogFixture, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	c := &catalogFixture{rows: map[string]SavedServer{}, journals: map[Identity]*journalFixture{}}
	var ids atomic.Int32
	launches, approvals := &atomic.Int32{}, &atomic.Int32{}
	o := RegistryOptions{MachineID: "synthetic-machine", Lifetime: context.Background(), Catalog: c,
		ID: func() string { return fmt.Sprintf("synthetic-%d", ids.Add(1)) }, CleanupTimeout: time.Second,
		CheckControl: func(ctx context.Context, _ Spec) error { return ctx.Err() },
		AuthorizeStart: func(ctx context.Context, _ SavedServer) (func(context.Context) error, error) {
			approvals.Add(1)
			return func(ctx context.Context) error { return ctx.Err() }, ctx.Err()
		},
		Launch: func(context.Context, Spec) (Process, error) {
			launches.Add(1)
			return &processFixture{exit: make(chan struct{})}, nil
		}}
	return o, c, launches, approvals
}

func openRegistry(t *testing.T, o RegistryOptions) *Registry {
	t.Helper()
	r, err := OpenRegistry(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	return r
}

func waitRegistry(t *testing.T, r *Registry, saved SavedServer, state State) {
	t.Helper()
	r.mu.Lock()
	e := r.rows[saved.ID]
	owner := e.owner
	r.mu.Unlock()
	if state == Stopped || state == Failed {
		awaitOwner(t, owner.done)
	} else {
		c := r.o.Catalog.(*catalogFixture)
		i := owner.Snapshot().Identity
		c.mu.Lock()
		j := c.journals[i]
		c.mu.Unlock()
		j.wait(t, state)
	}
	if got := r.view(e); got.Attempt.State != state {
		t.Fatalf("server state=%+v want=%s", got, state)
	}
}

func TestServerRegistryRetainsTheProcessAcrossRequestCancellationAndFencesStop(t *testing.T) {
	o, _, launches, approvals := registryFixture(t)
	r := openRegistry(t, o)
	s := specFixture()
	request, cancel := context.WithCancel(context.Background())
	first, err := r.Start(request, s, "first")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, first, Running)
	cancel()
	second, err := r.Start(context.Background(), s, "second")
	if err != nil || second.ID != first.ID || launches.Load() != 1 || approvals.Load() != 1 {
		t.Fatalf("running no-op=%+v %v launches=%d approvals=%d", second, err, launches.Load(), approvals.Load())
	}
	if err := r.Stop(context.Background(), s.Key, s.Project, first.ID, "first"); err != nil {
		t.Fatal(err)
	}
	if r.Usage().Processes != 0 {
		t.Fatal("a proven stop retained its slot")
	}
	second, err = r.Start(context.Background(), s, "second")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, second, Running)
	if err := r.Stop(context.Background(), s.Key, s.Project, first.ID, "first"); pa.Code(err) != pa.StaleGeneration {
		t.Fatalf("old stop reached a later process: %v", err)
	}
	if u := r.Usage(); u.Processes != 1 || u.Definitions != 1 || !u.Known || launches.Load() != 2 {
		t.Fatalf("restart usage=%+v launches=%d", u, launches.Load())
	}
	rows, err := r.Inspect(context.Background(), s.Key, s.Project)
	if err != nil || len(rows) != 1 || rows[0].Attempt.Identity.AttemptID != "second" {
		t.Fatalf("inspect=%+v %v", rows, err)
	}
	foreign := s.Project
	foreign.CanonicalID = "other-repository"
	if _, err := r.ReadLog(context.Background(), s.Key, foreign, first.ID, "second"); pa.Code(err) != pa.ScopeDenied {
		t.Fatal(err)
	}
}

func TestServerRegistryAdmitsOnlyTheBoundedNumberOfOwnedProcesses(t *testing.T) {
	o, _, launches, approvals := registryFixture(t)
	r := openRegistry(t, o)
	var first SavedServer
	for n := 0; n < capacity.MaximumBotServerProcesses; n++ {
		s := specFixture()
		s.Command += fmt.Sprint(n)
		saved, err := r.Start(context.Background(), s, "first")
		if err != nil {
			t.Fatal(err)
		}
		waitRegistry(t, r, saved, Running)
		if n == 0 {
			first = saved
		}
	}
	s := specFixture()
	if _, err := r.Start(context.Background(), s, "overflow"); pa.Code(err) != pa.CapacityFull || launches.Load() != capacity.MaximumBotServerProcesses || approvals.Load() != capacity.MaximumBotServerProcesses {
		t.Fatalf("process overflow=%v launches=%d approvals=%d", err, launches.Load(), approvals.Load())
	}
	if err := r.Stop(context.Background(), first.Spec.Key, first.Spec.Project, first.ID, "first"); err != nil {
		t.Fatal(err)
	}
	saved, err := r.Start(context.Background(), s, "after-stop")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, saved, Running)
	if u := r.Usage(); u.Processes != capacity.MaximumBotServerProcesses || u.Definitions != capacity.MaximumBotServerProcesses+1 {
		t.Fatalf("usage after replacement=%+v", u)
	}
}

func TestServerRegistryReopenNeverAdoptsOrRestartsUnprovedProcesses(t *testing.T) {
	o, c, launches, approvals := registryFixture(t)
	s := specFixture()
	fingerprint, _ := s.Fingerprint()
	i := Identity{Key: s.Key, Project: s.Project, ServerID: "saved", AttemptID: "interrupted", Fingerprint: fingerprint}
	c.rows[i.ServerID] = SavedServer{ID: i.ServerID, Spec: s, Attempt: Record{Identity: i, Version: 2, State: Running}}
	r := openRegistry(t, o)
	if u := r.Usage(); u.Known || u.Processes != 1 || u.Definitions != 1 {
		t.Fatalf("unknown ownership disappeared=%+v", u)
	}
	if saved, err := r.Start(context.Background(), s, "retry"); pa.Code(err) != pa.ExecutionEffectUnknown || saved.Attempt.State != Unknown || launches.Load() != 0 || approvals.Load() != 0 {
		t.Fatalf("interrupted process replayed=%+v %v", saved, err)
	}
	if err := r.Stop(context.Background(), s.Key, s.Project, i.ServerID, i.AttemptID); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal(err)
	}
	if err := r.Remove(context.Background(), s.Key, s.Project, i.ServerID, i.AttemptID); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal(err)
	}
	if _, err := r.ReadLog(context.Background(), s.Key, s.Project, i.ServerID, i.AttemptID); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal(err)
	}
	if err := r.Close(context.Background()); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal(err)
	}
	if c.rows[i.ServerID].Attempt.State != Running {
		t.Fatal("inspection rewrote a durable unobserved fact")
	}
}

func TestServerRegistryRefusesAFullOrInvalidDefinitionCatalog(t *testing.T) {
	o, c, launches, approvals := registryFixture(t)
	for n := 0; n < capacity.MaximumBotServerDefinitions; n++ {
		s := specFixture()
		s.Command += fmt.Sprint(n)
		id := fmt.Sprint(n)
		c.rows[id] = SavedServer{ID: id, Spec: s}
	}
	r := openRegistry(t, o)
	if _, err := r.Start(context.Background(), specFixture(), "first"); pa.Code(err) != pa.CapacityFull || launches.Load() != 0 || approvals.Load() != 0 {
		t.Fatalf("full definitions=%v", err)
	}
	c.rows["extra"] = SavedServer{ID: "extra", Spec: specFixture()}
	if _, err := OpenRegistry(context.Background(), o); pa.Code(err) != pa.CapacityFull {
		t.Fatal(err)
	}
	delete(c.rows, "extra")
	bad := c.rows["0"]
	bad.Spec.Key.MachineID = "foreign"
	c.rows["0"] = bad
	if _, err := OpenRegistry(context.Background(), o); err == nil {
		t.Fatal("foreign definition was admitted")
	}
}

func TestServerRegistryRechecksAdmissionAfterWaitingForApproval(t *testing.T) {
	o, _, launches, _ := registryFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	o.AuthorizeStart = func(context.Context, SavedServer) (func(context.Context) error, error) {
		close(entered)
		<-release
		return func(ctx context.Context) error { return ctx.Err() }, nil
	}
	r := openRegistry(t, o)
	done := make(chan error, 1)
	go func() {
		_, err := r.Start(context.Background(), specFixture(), "first")
		done <- err
	}()
	awaitOwner(t, entered)
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil || launches.Load() != 0 || r.Usage().Definitions != 1 || r.Usage().Processes != 0 {
		t.Fatalf("a late approval escaped shutdown: %v", err)
	}
}

func TestServerRegistryConcurrentStartsHaveOneOwner(t *testing.T) {
	o, _, launches, _ := registryFixture(t)
	r := openRegistry(t, o)
	var workers sync.WaitGroup
	for n := range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if _, err := r.Start(context.Background(), specFixture(), fmt.Sprint(n)); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	rows, err := r.Inspect(context.Background(), specFixture().Key, specFixture().Project)
	if err != nil || len(rows) != 1 {
		t.Fatalf("definitions=%+v %v", rows, err)
	}
	waitRegistry(t, r, rows[0], Running)
	if launches.Load() != 1 || r.Usage().Processes != 1 {
		t.Fatal("concurrent starts acquired multiple process owners")
	}
}

func TestRemovingAReviewedDefinitionCannotLaunchOrReuseItsApproval(t *testing.T) {
	o, _, launches, _ := registryFixture(t)
	entered, release := make(chan SavedServer, 1), make(chan struct{})
	o.AuthorizeStart = func(_ context.Context, saved SavedServer) (func(context.Context) error, error) {
		entered <- saved
		<-release
		return func(context.Context) error { return nil }, nil
	}
	r := openRegistry(t, o)
	done := make(chan error, 1)
	go func() { _, err := r.Start(context.Background(), specFixture(), "reviewed"); done <- err }()
	saved := <-entered
	if err := r.Remove(context.Background(), saved.Spec.Key, saved.Spec.Project, saved.ID, ""); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; pa.Code(err) != pa.StaleVersion || launches.Load() != 0 {
		t.Fatal("deleted definition retained approval", err)
	}
}

func TestReconcileStopsOnlyTheForgottenDefinitionAndKeepsCurrentEpoch(t *testing.T) {
	o, _, _, _ := registryFixture(t)
	r := openRegistry(t, o)
	old := specFixture()
	first, err := r.Start(context.Background(), old, "old")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, first, Running)
	current := old
	current.ForgottenThrough = 2
	second, err := r.Start(context.Background(), current, "new")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, second, Running)
	if err := r.Reconcile(context.Background(), func(_ context.Context, spec Spec) error {
		if spec.ForgottenThrough == 0 {
			return failure(pa.ContextStale, "synthetic_epoch")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := r.Inspect(context.Background(), old.Key, old.Project)
	if err != nil {
		t.Fatal(err)
	}
	for _, saved := range rows {
		if saved.ID == first.ID && saved.Attempt.State != Stopped {
			t.Fatal("old definition survived", saved)
		}
		if saved.ID == second.ID && saved.Attempt.State != Running {
			t.Fatal("old definition stopped the new context", saved)
		}
	}
	if u := r.Usage(); !u.Known || u.Processes != 1 {
		t.Fatal(u)
	}
}

func TestExpectedStartCannotReplayAcrossAnInterveningCompletedAttempt(t *testing.T) {
	o, _, launches, _ := registryFixture(t)
	r := openRegistry(t, o)
	spec := specFixture()
	ctx := context.Background()
	first, err := r.Start(ctx, spec, "first")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, first, Running)
	if err := r.Stop(ctx, spec.Key, spec.Project, first.ID, "first"); err != nil {
		t.Fatal(err)
	}
	baseline := r.Snapshot(spec.Key, spec.Project)[0]
	entered, release := make(chan struct{}), make(chan struct{})
	var reviews atomic.Int32
	r.o.AuthorizeStart = func(context.Context, SavedServer) (func(context.Context) error, error) {
		if reviews.Add(1) == 1 {
			close(entered)
			<-release
		}
		return func(context.Context) error { return nil }, nil
	}
	done := make(chan error, 1)
	go func() { _, err := r.StartExpected(ctx, baseline, "old-human-intent"); done <- err }()
	awaitOwner(t, entered)
	second, err := r.Start(ctx, spec, "intervening")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, second, Running)
	if err := r.Stop(ctx, spec.Key, spec.Project, second.ID, "intervening"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; pa.Code(err) != pa.StaleVersion || launches.Load() != 2 {
		t.Fatal("old human request became a new execution", err, launches.Load())
	}
	if err := r.StopExpected(ctx, baseline); pa.Code(err) != pa.StaleVersion {
		t.Fatal("old stop crossed its baseline", err)
	}
	if err := r.RemoveExpected(ctx, baseline); pa.Code(err) != pa.StaleVersion {
		t.Fatal("old removal crossed its baseline", err)
	}
}

func TestOldServerCleanupCannotReleaseANewerReservation(t *testing.T) {
	o, _, _, _ := registryFixture(t)
	r := openRegistry(t, o)
	saved, err := r.Start(context.Background(), specFixture(), "first")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, saved, Running)
	r.mu.Lock()
	e := r.rows[saved.ID]
	old := e.owner
	r.mu.Unlock()
	if err := r.Stop(context.Background(), saved.Spec.Key, saved.Spec.Project, saved.ID, "first"); err != nil {
		t.Fatal(err)
	}
	saved, err = r.Start(context.Background(), specFixture(), "second")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, saved, Running)
	// Inject a delayed, duplicated old cleanup notification after the new
	// owner exists. This is the interleaving the generation fence must reject.
	r.release(e, old)
	if u := r.Usage(); u.Processes != 1 {
		t.Fatalf("old cleanup released a later owner's slot: %+v", u)
	}
}

func TestServerRegistryLogAndControlRecheckTheSource(t *testing.T) {
	o, _, _, _ := registryFixture(t)
	var revoked atomic.Bool
	o.CheckControl = func(context.Context, Spec) error {
		if revoked.Load() {
			return failure(pa.ContextStale, "synthetic_forget")
		}
		return nil
	}
	r := openRegistry(t, o)
	s := specFixture()
	saved, err := r.Start(context.Background(), s, "first")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, saved, Running)
	r.mu.Lock()
	log := r.rows[saved.ID].log
	r.mu.Unlock()
	_, _ = log.Write([]byte(strings.Repeat("synthetic", capacity.MaximumBotServerLogBytes)))
	got, err := r.ReadLog(context.Background(), s.Key, s.Project, saved.ID, "first")
	if err != nil || !got.Truncated || len(got.Text) > capacity.MaximumBotServerLogBytes {
		t.Fatalf("log summary=%d %v", len(got.Text), err)
	}
	revoked.Store(true)
	if got, err := r.ReadLog(context.Background(), s.Key, s.Project, saved.ID, "first"); pa.Code(err) != pa.ContextStale || got.Text != "" {
		t.Fatal("revoked log content disclosed")
	}
	if _, err := r.Inspect(context.Background(), s.Key, s.Project); pa.Code(err) != pa.ContextStale {
		t.Fatal(err)
	}
	if err := r.Stop(context.Background(), s.Key, s.Project, saved.ID, "first"); pa.Code(err) != pa.ContextStale {
		t.Fatal(err)
	}
	// Trusted Host shutdown remains available after user/tool authority ends.
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestServerRegistryRemovalRequiresTheExactCleanAttempt(t *testing.T) {
	o, _, _, _ := registryFixture(t)
	r := openRegistry(t, o)
	s := specFixture()
	saved, err := r.Start(context.Background(), s, "first")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, saved, Running)
	if err := r.Remove(context.Background(), s.Key, s.Project, saved.ID, "first"); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal(err)
	}
	if err := r.Stop(context.Background(), s.Key, s.Project, saved.ID, "first"); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove(context.Background(), s.Key, s.Project, saved.ID, "other"); pa.Code(err) != pa.StaleGeneration {
		t.Fatal(err)
	}
	if err := r.Remove(context.Background(), s.Key, s.Project, saved.ID, "first"); err != nil {
		t.Fatal(err)
	}
	if got := r.Usage(); got.Definitions != 0 || got.Processes != 0 || !got.Known {
		t.Fatalf("removed definition retained capacity: %+v", got)
	}
	rows, err := r.Inspect(context.Background(), s.Key, s.Project)
	if err != nil || len(rows) != 0 {
		t.Fatalf("removed definition still visible: %+v %v", rows, err)
	}
}

func TestRestartedRegistryReportsLostHistoricalOutput(t *testing.T) {
	o, c, _, _ := registryFixture(t)
	s := specFixture()
	fingerprint, _ := s.Fingerprint()
	i := Identity{Key: s.Key, Project: s.Project, ServerID: "saved", AttemptID: "past", Fingerprint: fingerprint}
	c.rows[i.ServerID] = SavedServer{ID: i.ServerID, Spec: s, Attempt: Record{Identity: i, Version: 3, State: Stopped}}
	r := openRegistry(t, o)
	got, err := r.ReadLog(context.Background(), s.Key, s.Project, i.ServerID, i.AttemptID)
	if err != nil || !got.Truncated || got.Text != "" {
		t.Fatalf("lost output was presented as complete: %+v %v", got, err)
	}
}

func TestDefinitionCanBeRemovedWhenAuthorityClosesBeforeIntent(t *testing.T) {
	o, _, launches, _ := registryFixture(t)
	var checks atomic.Int32
	o.AuthorizeStart = func(context.Context, SavedServer) (func(context.Context) error, error) {
		return func(context.Context) error {
			if checks.Add(1) > 1 {
				return failure(pa.Revoked, "synthetic_approval")
			}
			return nil
		}, nil
	}
	r := openRegistry(t, o)
	s := specFixture()
	saved, err := r.Start(context.Background(), s, "not-admitted")
	if pa.Code(err) != pa.Revoked || saved.Attempt != (Record{}) || launches.Load() != 0 {
		t.Fatalf("non-admission acquired an attempt: %+v %v", saved, err)
	}
	if err := r.Remove(context.Background(), s.Key, s.Project, saved.ID, ""); err != nil {
		t.Fatal(err)
	}
	if u := r.Usage(); u.Definitions != 0 || u.Processes != 0 {
		t.Fatalf("a definition without an admitted attempt could not be removed: %+v", u)
	}
}
