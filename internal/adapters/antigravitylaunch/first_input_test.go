package antigravitylaunch

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

const firstInputKey = "74000000-0000-4000-8000-000000000090"

func firstDigest() string { return hash([]byte("Synthetic first input")) }

func TestFirstInputIssuesOneGrantAndNeverReplaysAfterRestart(t *testing.T) {
	f := setup(t)
	p := prep(t, f)
	native := pin(t, f.store, p)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var grant *FirstInputAttempt
	count := 0
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, a, err := f.store.ReserveFirstInput(ctx, p.Record.ID, firstInputKey, firstDigest(), native)
			if err != nil {
				t.Error(err)
			}
			if a != nil {
				mu.Lock()
				count++
				grant = a
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if count != 1 || grant == nil {
		t.Fatalf("execution grants=%d", count)
	}
	if err := grant.BeforeEnter(ctx); err == nil {
		t.Fatal("Enter before paste admitted")
	}
	if err := grant.BeforePaste(ctx); err != nil {
		t.Fatal(err)
	}
	if err := grant.BeforePaste(ctx); err == nil {
		t.Fatal("second paste admitted")
	}
	if err := grant.BeforeEnter(ctx); err != nil {
		t.Fatal(err)
	}
	if err := grant.BeforeEnter(ctx); err == nil {
		t.Fatal("second Enter admitted")
	}
	status, err := f.store.FirstInputStatus(ctx, p.Record.ID, firstInputKey)
	if err != nil || status.Phase != FirstInputEnterAttempted {
		t.Fatal(status, err)
	}
	f.store.Close()
	reopened, err := Open(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := grant.Check(ctx); err == nil {
		t.Fatal("old grant revived")
	}
	if err := reopened.PinProcess(ctx, p.Record.ID, native); err != nil {
		t.Fatal(err)
	}
	receipt, again, err := reopened.ReserveFirstInput(ctx, p.Record.ID, firstInputKey, firstDigest(), native)
	if err != nil || again != nil || receipt != status {
		t.Fatal("restart granted another input", err)
	}
	_, _, err = reopened.ReserveFirstInput(ctx, p.Record.ID, "74000000-0000-4000-8000-000000000091", firstDigest(), native)
	code(t, err, "first_input_conflict")
	_, _, err = reopened.ReserveFirstInput(ctx, p.Record.ID, firstInputKey, hash([]byte("changed")), native)
	code(t, err, "first_input_conflict")
}

func TestFirstInputCrashAtEveryPhaseRetainsConsumedSlot(t *testing.T) {
	for _, phase := range []FirstInputPhase{FirstInputReserved, FirstInputPastePossible, FirstInputEnterAttempted} {
		t.Run(string(phase), func(t *testing.T) {
			f := setup(t)
			p := prep(t, f)
			native := pin(t, f.store, p)
			ctx := context.Background()
			_, a, err := f.store.ReserveFirstInput(ctx, p.Record.ID, firstInputKey, firstDigest(), native)
			if err != nil {
				t.Fatal(err)
			}
			if phase != FirstInputReserved {
				if err = a.BeforePaste(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if phase == FirstInputEnterAttempted {
				if err = a.BeforeEnter(ctx); err != nil {
					t.Fatal(err)
				}
			}
			f.store.Close()
			s, err := Open(f.config)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			// Historical status is available without reviving process authority.
			r, err := s.FirstInputStatus(ctx, p.Record.ID, firstInputKey)
			if err != nil || r.Phase != phase {
				t.Fatal(r, err)
			}
			_, again, err := s.ReserveFirstInput(ctx, p.Record.ID, firstInputKey, firstDigest(), native)
			if err != nil || again != nil {
				t.Fatal("recovered metadata granted effects", err)
			}
		})
	}
}

func TestFirstInputPublicationCancellationNeverGrantsExecution(t *testing.T) {
	for _, phase := range []FirstInputPhase{FirstInputReserved, FirstInputPastePossible, FirstInputEnterAttempted} {
		t.Run(string(phase), func(t *testing.T) {
			f := setup(t)
			p := prep(t, f)
			native := pin(t, f.store, p)
			var attempt *FirstInputAttempt
			var err error
			if phase != FirstInputReserved {
				_, attempt, err = f.store.ReserveFirstInput(context.Background(), p.Record.ID, firstInputKey, firstDigest(), native)
				if err != nil {
					t.Fatal(err)
				}
			}
			if phase == FirstInputEnterAttempted {
				if err = attempt.BeforePaste(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.store.afterPublish = cancel
			switch phase {
			case FirstInputReserved:
				_, attempt, err = f.store.ReserveFirstInput(ctx, p.Record.ID, firstInputKey, firstDigest(), native)
				if attempt != nil {
					t.Fatal("canceled reservation granted effects")
				}
			case FirstInputPastePossible:
				err = attempt.BeforePaste(ctx)
			case FirstInputEnterAttempted:
				err = attempt.BeforeEnter(ctx)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatal("canceled publication admitted next effect", err)
			}
			if attempt != nil && attempt.Check(context.Background()) == nil {
				t.Fatal("uncertain owner retained authority")
			}
			f.store.Close()
			s, err := Open(f.config)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			r, again, err := s.ReserveFirstInput(context.Background(), p.Record.ID, firstInputKey, firstDigest(), native)
			if err != nil || again != nil || r.Phase != phase {
				t.Fatal("published reservation lost or regranted", r, err)
			}
		})
	}
}

func TestFirstInputFailureBeforeWriteAndIntegrityLossStopNextEffect(t *testing.T) {
	f := setup(t)
	p := prep(t, f)
	native := pin(t, f.store, p)
	ctx := context.Background()
	f.store.beforeWrite = func(string) error { return errors.New("synthetic disk failure") }
	_, a, err := f.store.ReserveFirstInput(ctx, p.Record.ID, firstInputKey, firstDigest(), native)
	code(t, err, "write_failed")
	if a != nil {
		t.Fatal("disk error granted effects")
	}
	f.store.beforeWrite = nil
	_, a, err = f.store.ReserveFirstInput(ctx, p.Record.ID, firstInputKey, firstDigest(), native)
	if err != nil || a == nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Record.ContextPath, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = a.BeforePaste(ctx); err == nil {
		t.Fatal("changed context allowed paste")
	}
	r, err := f.store.FirstInputStatus(ctx, p.Record.ID, firstInputKey)
	if err != nil || r.Phase != FirstInputReserved {
		t.Fatal("integrity refusal advanced phase", err)
	}
}

func TestFirstInputRequiresFreshPinAndANewUnboundLaunch(t *testing.T) {
	f := setup(t)
	p := prep(t, f)
	native := process(p)
	ctx := context.Background()
	_, _, err := f.store.ReserveFirstInput(ctx, p.Record.ID, firstInputKey, firstDigest(), native)
	code(t, err, "process_unverified")
	native = pin(t, f.store, p)
	if err = f.store.RecordBinding(ctx, p.Record.ID, binding(native, firstInputKey, 1), KeepConversation); err != nil {
		t.Fatal(err)
	}
	_, _, err = f.store.ReserveFirstInput(ctx, p.Record.ID, firstInputKey, firstDigest(), native)
	code(t, err, "first_input_not_new")
	if err = f.store.Deactivate(ctx, p.Record.ID); err != nil {
		t.Fatal(err)
	}
	f.request.Resume = firstInputKey
	resumed, err := f.store.Prepare(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	native = pin(t, f.store, resumed)
	_, _, err = f.store.ReserveFirstInput(ctx, resumed.Record.ID, firstInputKey, firstDigest(), native)
	code(t, err, "first_input_not_new")
}

func TestFirstInputLegacyIndexOpensWithoutWritingAndPublishesCurrentVersion(t *testing.T) {
	f := setup(t)
	p := prep(t, f)
	native := pin(t, f.store, p)
	f.store.Close()
	path := filepath.Join(f.config.StateDir, "context-index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var old map[string]any
	if err = json.Unmarshal(data, &old); err != nil {
		t.Fatal(err)
	}
	old["version"] = 1
	delete(old, "first_inputs")
	legacy, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, legacy)
	s, err := Open(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(legacy) {
		t.Fatal("Open rewrote legacy bytes", err)
	}
	if err = s.PinProcess(context.Background(), p.Record.ID, native); err != nil {
		t.Fatal(err)
	}
	_, a, err := s.ReserveFirstInput(context.Background(), p.Record.ID, firstInputKey, firstDigest(), native)
	if err != nil || a == nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var next map[string]any
	json.Unmarshal(data, &next)
	if next["version"] != float64(3) || next["first_inputs"] == nil {
		t.Fatal("reservation is readable by old v1 reader")
	}
	stats, err := s.Stats(context.Background())
	if err != nil || stats.FirstInputs != 1 || stats.IndexBytes != int64(len(data)) {
		t.Fatal("actual slot bytes not measured", stats, err)
	}
	t.Logf("one real slot retained in %d-byte index", len(data))
}

func TestFirstInputIndexRejectsFutureFieldsAndForgedPhases(t *testing.T) {
	for _, kind := range []string{"future", "phase", "downgrade", "null", "missing-version"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			p := prep(t, f)
			native := pin(t, f.store, p)
			_, _, err := f.store.ReserveFirstInput(context.Background(), p.Record.ID, firstInputKey, firstDigest(), native)
			if err != nil {
				t.Fatal(err)
			}
			f.store.Close()
			path := filepath.Join(f.config.StateDir, "context-index.json")
			data, _ := os.ReadFile(path)
			var doc map[string]any
			json.Unmarshal(data, &doc)
			switch kind {
			case "future":
				doc["future_input_authority"] = true
			case "phase":
				doc["first_inputs"].(map[string]any)[p.Record.ID].(map[string]any)["phase"] = "reset"
			case "downgrade":
				doc["version"] = 1
			case "missing-version":
				delete(doc, "version")
			}
			changed, _ := json.Marshal(doc)
			if kind == "null" {
				changed = []byte("null")
			}
			put(t, path, changed)
			s, err := Open(f.config)
			if s != nil {
				s.Close()
				t.Fatal("invalid input authority opened")
			}
			code(t, err, "index_invalid")
		})
	}
}
