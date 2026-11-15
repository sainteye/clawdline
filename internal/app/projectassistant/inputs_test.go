package projectassistant

import (
	"context"
	"errors"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"strings"
	"testing"
	"time"
)

// A controlled read-port interruption, separate from the real HTTP/SQLite/ACP
// byte-reading fixture. The background source remains writable while the
// request's human grant changes during verification.
type changedInputGrant struct{ changed *bool }

func (r changedInputGrant) Resolve(_ context.Context, _ pa.Fence, id string, _ ...InputOwner) (pa.InputFile, string, error) {
	*r.changed = true
	return pa.InputFile{ID: id, Name: "empty", SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}, "/synthetic/not-opened", nil
}
func (r changedInputGrant) SentPath(context.Context, pa.Fence, pa.InputFile) (string, error) {
	panic("changed grant must never reach execution")
}

func TestInputGrantChangeDuringVerificationCannotAdmitOrStartNativeWork(t *testing.T) {
	f := newRuntimeFixture(t)
	changed := false
	f.r.o.Inputs = changedInputGrant{changed: &changed}
	_, err := f.r.Submit(context.Background(), Submission{Command: pa.Command{Key: f.key, Operation: pa.SendOperation, IdempotencyKey: "file", ContentHash: strings.Repeat("a", 64)}, Message: pa.SendMessage{Fence: currentFence(f.store.bot, f.store.c), UploadIDs: []string{"11111111-1111-4111-8111-111111111111"}}, Feature: pa.FeatureState{Enabled: true}, Source: f.source, InputCheck: func(context.Context) error {
		if changed {
			return runtimeFailure(pa.StaleVersion, "human_grant_changed")
		}
		return nil
	}})
	if pa.Code(err) != pa.StaleVersion || len(f.store.messages) != 0 || len(f.provider.opens) != 0 {
		t.Fatal("changed human grant admitted input", err)
	}
}

type takebackInputStore struct {
	InputReader
	remove func(context.Context, pa.Fence, pa.InputFile) error
}

func (s takebackInputStore) Remove(ctx context.Context, fence pa.Fence, file pa.InputFile) error {
	return s.remove(ctx, fence, file)
}

func TestInputTakebackCleanupDoesNotHoldExecutionAndRetriesOnlyCleanup(t *testing.T) {
	f := newRuntimeFixture(t)
	if _, err := f.submit(t, "keep working", "running"); err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	queued, err := f.submit(t, "take this back", "queued")
	if err != nil {
		t.Fatal(err)
	}
	file := pa.InputFile{ID: "11111111-1111-4111-8111-111111111111", Name: "empty", SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}
	f.store.mu.Lock()
	for i := range f.store.messages {
		if f.store.messages[i].TurnID == queued.ResourceID {
			f.store.messages[i].Files = []pa.InputFile{file}
		}
	}
	fence := currentFence(f.store.bot, f.store.c)
	f.store.mu.Unlock()
	calls := 0
	injected := errors.New("temporary filesystem failure")
	f.r.o.Inputs = takebackInputStore{remove: func(_ context.Context, _ pa.Fence, actual pa.InputFile) error {
		calls++
		if actual != file {
			t.Fatal("cleanup changed durable file identity")
		}
		if calls == 1 {
			other := make(chan error, 1)
			go func() { _, e := f.submit(t, "independent message", "while-cleaning"); other <- e }()
			select {
			case e := <-other:
				if e != nil {
					t.Fatal(e)
				}
			case <-time.After(time.Second):
				t.Fatal("filesystem cleanup blocked another admission")
			}
			return injected
		}
		return nil
	}}
	q := StopRequest{Command: pa.Command{Key: f.key, Operation: pa.StopOperation, IdempotencyKey: "takeback", ContentHash: strings.Repeat("b", 64)}, Fence: fence, TurnID: queued.ResourceID}
	first, err := f.r.Stop(context.Background(), q)
	if !errors.Is(err, injected) || first.State != pa.EffectDelivered {
		t.Fatal("cleanup failure lost cancellation receipt", first, err)
	}
	second, err := f.r.Stop(context.Background(), q)
	if err != nil || second.ID != first.ID || calls != 2 {
		t.Fatal("receipt replay did not retry cleanup", second, err, calls)
	}
	if f.provider.count() != 1 {
		t.Fatal("cleanup retry restarted native work")
	}
}
