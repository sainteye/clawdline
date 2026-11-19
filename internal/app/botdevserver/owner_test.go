package botdevserver

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type journalFixture struct {
	mu         sync.Mutex
	record     Record
	failAt     int64
	commitLoss bool
	saves      int
	changed    chan Record
	history    []State
}

func (j *journalFixture) Load(context.Context, Identity) (Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.record, nil
}
func (j *journalFixture) Save(_ context.Context, version int64, r Record) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.saves++
	if j.record.Version != version {
		return errors.New("synthetic CAS conflict")
	}
	fail := j.failAt == r.Version
	if !fail || j.commitLoss {
		j.record = r
		j.history = append(j.history, r.State)
		j.changed <- r
	}
	if fail {
		return errors.New("synthetic lost journal acknowledgement")
	}
	return nil
}
func (j *journalFixture) wait(t *testing.T, state State) Record {
	t.Helper()
	for {
		select {
		case r := <-j.changed:
			if r.State == state {
				return r
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no journal state %s", state)
			return Record{}
		}
	}
}

func (j *journalFixture) published(state State) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, s := range j.history {
		if s == state {
			return true
		}
	}
	return false
}

type processFixture struct {
	exit       chan struct{}
	exitErr    error
	closeErr   error
	closeCount atomic.Int32
}

func (p *processFixture) Output() io.Reader { return strings.NewReader("synthetic log") }
func (p *processFixture) WaitExit(ctx context.Context) error {
	select {
	case <-p.exit:
		return p.exitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *processFixture) Close(context.Context) error {
	p.closeCount.Add(1)
	return p.closeErr
}

func ownerFixture(t *testing.T) (Options, *journalFixture, *processFixture, *atomic.Int32) {
	t.Helper()
	j := &journalFixture{changed: make(chan Record, 16)}
	p := &processFixture{exit: make(chan struct{})}
	launches := &atomic.Int32{}
	o := Options{Identity: Identity{Key: pa.BotKey{MachineID: "synthetic-machine", BotID: "synthetic-bot"},
		Project:  pa.ProjectScope{MachineID: "synthetic-machine", ProjectID: "synthetic-project", CanonicalID: "synthetic-repository", Kind: pa.RepositoryProject},
		ServerID: "synthetic-server", AttemptID: "synthetic-attempt", Fingerprint: strings.Repeat("a", 64)},
		Lifetime: context.Background(), Journal: j, CleanupTimeout: time.Second,
		Check: func(ctx context.Context) error { return ctx.Err() },
		Launch: func(context.Context) (Process, error) {
			launches.Add(1)
			return p, nil
		}}
	return o, j, p, launches
}

func openOwner(t *testing.T, o Options) *Owner {
	t.Helper()
	owner, err := Open(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = owner.Close(ctx)
	})
	return owner
}

func awaitOwner(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("owner barrier did not finish")
	}
}

func TestRequestCancellationAndDuplicateStartDoNotLoseTheOwner(t *testing.T) {
	o, j, p, launches := ownerFixture(t)
	owner := openOwner(t, o)
	request, cancel := context.WithCancel(context.Background())
	r, err := owner.Start(request)
	if err != nil || r.State != Accepted {
		t.Fatalf("acceptance=%+v %v", r, err)
	}
	j.wait(t, Running)
	cancel()
	for range 2 {
		r, err = owner.Start(context.Background())
		if err != nil || r.State != Running || launches.Load() != 1 {
			t.Fatalf("replay launched again or lost running fact: %+v %v", r, err)
		}
	}
	if err := owner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if owner.Snapshot().State != Stopped || p.closeCount.Load() != 1 {
		t.Fatalf("stop=%+v cleanup calls=%d", owner.Snapshot(), p.closeCount.Load())
	}
	_, _ = owner.Start(context.Background())
	if launches.Load() != 1 {
		t.Fatal("completed attempt replayed")
	}
}

func TestStopDuringLateLaunchKeepsItsHandleAndNeverPublishesRunning(t *testing.T) {
	o, j, p, launches := ownerFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	o.Launch = func(context.Context) (Process, error) {
		launches.Add(1)
		close(entered)
		<-release // Deliberately ignore cancellation, then return the late handle.
		return p, nil
	}
	owner := openOwner(t, o)
	var once sync.Once
	releaseLaunch := func() { once.Do(func() { close(release) }) }
	t.Cleanup(releaseLaunch)
	_, _ = owner.Start(context.Background())
	awaitOwner(t, entered)
	stopped := make(chan error, 1)
	go func() { stopped <- owner.Close(context.Background()) }()
	j.wait(t, Stopping)
	releaseLaunch()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if r := owner.Snapshot(); r.State != Stopped || p.closeCount.Load() != 1 || launches.Load() != 1 || j.published(Running) {
		t.Fatalf("late launch escaped cleanup or published running: %+v", r)
	}
}

func TestLostIntentAcknowledgementNeverLaunchesOrReplays(t *testing.T) {
	o, j, _, launches := ownerFixture(t)
	j.failAt, j.commitLoss = 1, true
	owner := openOwner(t, o)
	for range 2 {
		r, err := owner.Start(context.Background())
		if pa.Code(err) != pa.ExecutionEffectUnknown || r.State != Unknown {
			t.Fatalf("uncertain intent=%+v %v", r, err)
		}
	}
	reopened := openOwner(t, o)
	_, err := reopened.Start(context.Background())
	if pa.Code(err) != pa.ExecutionEffectUnknown || launches.Load() != 0 {
		t.Fatalf("interrupted intent was replayed: %v, launches=%d", err, launches.Load())
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.saves != 1 || j.record.State != Accepted {
		t.Fatalf("read-only reopen overwrote an unobserved fact: %+v saves=%d", j.record, j.saves)
	}
}

func TestLostRunningAcknowledgementClosesTheExistingProcess(t *testing.T) {
	o, j, p, launches := ownerFixture(t)
	j.failAt, j.commitLoss = 2, true
	owner := openOwner(t, o)
	_, _ = owner.Start(context.Background())
	awaitOwner(t, owner.done)
	if owner.Snapshot().State != Unknown || p.closeCount.Load() != 1 || launches.Load() != 1 {
		t.Fatalf("lost running receipt lost the cleanup owner: %+v", owner.Snapshot())
	}
	reopened := openOwner(t, o)
	if reopened.Snapshot().State != Unknown {
		t.Fatal("a persisted running row proved live process ownership after restart")
	}
	_, _ = reopened.Start(context.Background())
	if launches.Load() != 1 {
		t.Fatal("lost running receipt caused a second command")
	}
}

func TestRevocationDuringLaunchClosesWithoutRunningPublication(t *testing.T) {
	o, j, p, _ := ownerFixture(t)
	var revoked atomic.Bool
	o.Check = func(ctx context.Context) error {
		if revoked.Load() {
			return failure(pa.Revoked, "synthetic_source")
		}
		return ctx.Err()
	}
	entered, release := make(chan struct{}), make(chan struct{})
	o.Launch = func(context.Context) (Process, error) { close(entered); <-release; return p, nil }
	owner := openOwner(t, o)
	var once sync.Once
	releaseLaunch := func() { once.Do(func() { close(release) }) }
	t.Cleanup(releaseLaunch)
	_, _ = owner.Start(context.Background())
	awaitOwner(t, entered)
	revoked.Store(true)
	releaseLaunch()
	j.wait(t, Failed)
	if j.published(Running) || p.closeCount.Load() != 1 {
		t.Fatal("revoked launch published running or lost its owned process")
	}
}

func TestDirectExitStillNeedsCleanupAndUnknownCleanupCannotRestart(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "nonzero exit", true: "unknown cleanup"}[unknown], func(t *testing.T) {
			o, j, p, launches := ownerFixture(t)
			p.exitErr = errors.New("synthetic nonzero exit")
			if unknown {
				p.closeErr = failure(pa.ExecutionEffectUnknown, "synthetic_cleanup")
			}
			owner := openOwner(t, o)
			_, _ = owner.Start(context.Background())
			j.wait(t, Running)
			close(p.exit)
			awaitOwner(t, owner.done)
			want := Failed
			if unknown {
				want = Unknown
			}
			if owner.Snapshot().State != want || p.closeCount.Load() != 1 {
				t.Fatalf("root exit claimed clean resources: %+v", owner.Snapshot())
			}
			_, _ = owner.Start(context.Background())
			if launches.Load() != 1 {
				t.Fatal("terminal/unknown command retried")
			}
		})
	}
}

func TestForeignLedgerIdentityCannotBeAdopted(t *testing.T) {
	o, j, _, launches := ownerFixture(t)
	j.record = Record{Identity: o.Identity, Version: 1, State: Running}
	j.record.Identity.Project.CanonicalID = "foreign-project"
	if _, err := Open(context.Background(), o); pa.Code(err) != pa.ScopeDenied || launches.Load() != 0 {
		t.Fatalf("foreign command adopted: %v", err)
	}
}

func TestShutdownBudgetRetainsALaunchThatIgnoresCancellation(t *testing.T) {
	o, j, p, _ := ownerFixture(t)
	o.CleanupTimeout = 50 * time.Millisecond
	entered, release := make(chan struct{}), make(chan struct{})
	o.Launch = func(context.Context) (Process, error) { close(entered); <-release; return p, nil }
	owner := openOwner(t, o)
	var once sync.Once
	releaseLaunch := func() { once.Do(func() { close(release) }) }
	t.Cleanup(releaseLaunch)
	_, _ = owner.Start(context.Background())
	awaitOwner(t, entered)
	result := make(chan error, 1)
	go func() { result <- owner.Close(context.Background()) }()
	select {
	case err := <-result:
		if pa.Code(err) != pa.ExecutionEffectUnknown {
			t.Fatalf("late launch shutdown was claimed complete: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close ignored the source-owned total cleanup budget")
	}
	if p.closeCount.Load() != 0 {
		t.Fatal("an unreturned handle was claimed cleaned")
	}
	releaseLaunch()
	awaitOwner(t, owner.done)
	if owner.Snapshot().State != Stopped || p.closeCount.Load() != 1 || j.published(Running) {
		t.Fatalf("timed-out owner lost late cleanup: %+v", owner.Snapshot())
	}
}
