package botdevserver

import (
	"context"
	"errors"
	"reflect"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type listeningFixture struct {
	*processFixture
	observe func(context.Context) ([]Listener, error)
}

func (p *listeningFixture) TCPListeners(ctx context.Context) ([]Listener, error) {
	return p.observe(ctx)
}

func TestServerListenersBelongToTheExactCurrentAttempt(t *testing.T) {
	o, _, _, _ := registryFixture(t)
	want := []Listener{{Address: "127.0.0.1", Port: 43123}}
	p := &listeningFixture{processFixture: &processFixture{exit: make(chan struct{})}, observe: func(context.Context) ([]Listener, error) { return want, nil }}
	o.Launch = func(context.Context, Spec) (Process, error) { return p, nil }
	r := openRegistry(t, o)
	spec := specFixture()
	first, err := r.Start(context.Background(), spec, "first")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, first, Running)
	first = r.Snapshot(spec.Key, spec.Project)[0]
	got, err := r.ObserveListeners(context.Background(), first)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	changed := first
	changed.Attempt.Version++
	if got, err := r.ObserveListeners(context.Background(), changed); pa.Code(err) != pa.StaleVersion || got != nil {
		t.Fatal("mismatched revision disclosed listeners", got, err)
	}
	if err := r.StopExpected(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	stopped := r.Snapshot(spec.Key, spec.Project)[0]
	if got, err := r.ObserveListeners(context.Background(), stopped); err != nil || len(got) != 0 {
		t.Fatal("proven stop is not known empty", got, err)
	}
	p = &listeningFixture{processFixture: &processFixture{exit: make(chan struct{})}, observe: func(context.Context) ([]Listener, error) { return []Listener{{Address: "::1", Port: 43124}}, nil }}
	second, err := r.StartExpected(context.Background(), stopped, "second")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, second, Running)
	if got, err := r.ObserveListeners(context.Background(), first); pa.Code(err) != pa.StaleVersion || got != nil {
		t.Fatal("old attempt disclosed later listeners", got, err)
	}
}

func TestServerListenersDiscardAnObservationWhenStopRacesTheProbe(t *testing.T) {
	o, _, _, _ := registryFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	p := &listeningFixture{processFixture: &processFixture{exit: make(chan struct{})}, observe: func(context.Context) ([]Listener, error) {
		close(entered)
		<-release
		return []Listener{{Address: "127.0.0.1", Port: 43123}}, nil
	}}
	o.Launch = func(context.Context, Spec) (Process, error) { return p, nil }
	r := openRegistry(t, o)
	spec := specFixture()
	saved, err := r.Start(context.Background(), spec, "first")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, saved, Running)
	saved = r.Snapshot(spec.Key, spec.Project)[0]
	type result struct {
		rows []Listener
		err  error
	}
	done := make(chan result, 1)
	go func() { rows, err := r.ObserveListeners(context.Background(), saved); done <- result{rows, err} }()
	awaitOwner(t, entered)
	// Stop must finish while the OS observer is blocked: no registry or Owner
	// mutex may span the probe. Its late result must then be discarded.
	if err := r.StopExpected(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	close(release)
	got := <-done
	if got.err == nil || got.rows != nil {
		t.Fatal("stopped attempt leaked its late listener", got)
	}
}

func TestServerListenersKeepUnsupportedAndLostOwnersUnknown(t *testing.T) {
	o, _, _, _ := registryFixture(t)
	r := openRegistry(t, o)
	spec := specFixture()
	saved, err := r.Start(context.Background(), spec, "first")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, saved, Running)
	saved = r.Snapshot(spec.Key, spec.Project)[0]
	if got, err := r.ObserveListeners(context.Background(), saved); pa.Code(err) != pa.Unsupported || got != nil {
		t.Fatal(got, err)
	}
	if err := r.StopExpected(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	e := r.rows[saved.ID]
	e.owner = nil
	e.saved.Attempt.State = Unknown
	r.mu.Unlock()
	lost := r.Snapshot(spec.Key, spec.Project)[0]
	if got, err := r.ObserveListeners(context.Background(), lost); pa.Code(err) != pa.ExecutionEffectUnknown || got != nil {
		t.Fatal("lost owner became empty", got, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.ObserveListeners(cancelled, lost); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestServerListenersDoNotReturnAddressesAfterReadCancellation(t *testing.T) {
	o, _, _, _ := registryFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &listeningFixture{processFixture: &processFixture{exit: make(chan struct{})}, observe: func(context.Context) ([]Listener, error) {
		cancel()
		return []Listener{{Address: "127.0.0.1", Port: 43123}}, nil
	}}
	o.Launch = func(context.Context, Spec) (Process, error) { return p, nil }
	r := openRegistry(t, o)
	spec := specFixture()
	saved, err := r.Start(context.Background(), spec, "first")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, saved, Running)
	saved = r.Snapshot(spec.Key, spec.Project)[0]
	if rows, err := r.ObserveListeners(ctx, saved); !errors.Is(err, context.Canceled) || rows != nil {
		t.Fatal(rows, err)
	}
}
