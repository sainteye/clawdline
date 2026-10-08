package cloudops

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

const testExecution = "0123456789abcdef0123456789abcdef"

type receiptRouter struct {
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (r *receiptRouter) Do(_ context.Context, _ LocalRequest) (LocalResponse, error) {
	if r.calls.Add(1) == 1 && r.entered != nil {
		close(r.entered)
		<-r.release
	}
	return LocalResponse{Status: 200, Body: []byte(`{"ok":true}`)}, nil
}

func receiptBridge(st *store.Store, router LocalRouter) Bridge {
	b := open(router)
	b.Receipts, b.EnforceSessionReceipts = st, true
	b.AdmitExecution = func(_ context.Context, _, _, generation string) error {
		if generation != testExecution {
			return errors.New("execution_generation_changed")
		}
		return nil
	}
	return b
}

func receiptCommand(t *testing.T, text string) Command {
	t.Helper()
	return request(t, ClassCtl, map[string]any{"type": "send", "session": pane,
		"request": "press-1", "execution_generation": testExecution, "text": text, "images": []any{}})
}

func TestSessionAdmissionIsAtomicAndAChangedBodyConflicts(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := &receiptRouter{entered: make(chan struct{}), release: make(chan struct{})}
	b := receiptBridge(st, r)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); b.Handle(context.Background(), receiptCommand(t, "first")) }()
	select {
	case <-r.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("the first effect did not start")
	}
	changed := b.Handle(context.Background(), receiptCommand(t, "changed"))
	if changed.Code != "idempotency_key_reused" {
		t.Fatalf("changed body: %+v", changed)
	}
	if pending := b.Handle(context.Background(), receiptCommand(t, "first")); pending.Code != "receipt_pending" {
		t.Fatalf("same request in flight: %+v", pending)
	}
	close(r.release)
	wg.Wait()
	if got := r.calls.Load(); got != 1 {
		t.Fatalf("executed %d times", got)
	}
	if replay := b.Handle(context.Background(), receiptCommand(t, "first")); !replay.OK() || r.calls.Load() != 1 {
		t.Fatalf("replay: %+v, calls %d", replay, r.calls.Load())
	}
}

func TestSessionReceiptSurvivesRestartAndAnswersLostReply(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := &receiptRouter{}
	b := receiptBridge(st, r)
	// The caller loses this answer; the persisted receipt is the only proof.
	if answer := b.Handle(context.Background(), receiptCommand(t, "first")); !answer.OK() {
		t.Fatalf("effect: %+v", answer)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b = receiptBridge(st, r)
	b.AdmitExecution = func(context.Context, string, string, string) error {
		return errors.New("execution_target_missing")
	}
	if replay := b.Handle(context.Background(), receiptCommand(t, "first")); !replay.OK() || r.calls.Load() != 1 {
		t.Fatalf("restart replay: %+v, calls %d", replay, r.calls.Load())
	}
	lookup := request(t, ClassCtl, map[string]any{"type": "session-receipt", "session": pane,
		"request": "query-1", "target_request": "press-1", "action": "send", "execution_generation": testExecution})
	answer := b.Handle(context.Background(), lookup)
	var payload struct {
		Body struct {
			MachineExecution string `json:"machine_execution"`
		} `json:"body"`
	}
	if err := json.Unmarshal(answer.Payload, &payload); err != nil || payload.Body.MachineExecution != "completed" {
		t.Fatalf("lookup %+v %v: %s", answer, err, answer.Payload)
	}
}

func TestSessionReceiptRejectsMissingKeyStaleExecutionAndMissingAuthority(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := &receiptRouter{}
	b := receiptBridge(st, r)
	missing := request(t, ClassCtl, map[string]any{"type": "send", "session": pane,
		"execution_generation": testExecution, "text": "x", "images": []any{}})
	if got := b.Handle(context.Background(), missing); got.Code != "idempotency_key_required" {
		t.Fatalf("missing key: %+v", got)
	}
	stale := receiptCommand(t, "x")
	var payload map[string]any
	_ = json.Unmarshal(stale.Plaintext, &payload)
	payload["execution_generation"] = "ffffffffffffffffffffffffffffffff"
	stale.Plaintext, _ = json.Marshal(payload)
	if got := b.Handle(context.Background(), stale); got.Code != "execution_generation_changed" {
		t.Fatalf("stale: %+v", got)
	}
	b.AllowCommands = func() bool { return false }
	if got := b.Handle(context.Background(), receiptCommand(t, "x")); got.Code != "cloud_commands_disabled" {
		t.Fatalf("permission: %+v", got)
	}
	if got := r.calls.Load(); got != 0 {
		t.Fatalf("refusals executed %d effects", got)
	}
}

func TestOrphanedReceiptIsUnknownAndNeverReplayedAsANewEffect(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	k := sessionReceiptKey("viewer-device-01", "mac-01", pane, testExecution, "send", "press-1")
	parsed, _ := decodeBody(receiptCommand(t, "first").Plaintext)
	claim, err := st.ClaimReceipt(context.Background(), k, sessionDigest(parsed), store.ReceiptPolicy{}, time.Now())
	if err != nil || claim.Outcome != store.ReceiptNew {
		t.Fatalf("claim: %+v %v", claim, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := &receiptRouter{}
	got := receiptBridge(st, r).Handle(context.Background(), receiptCommand(t, "first"))
	if got.Code != "receipt_outcome_unknown" || r.calls.Load() != 0 {
		t.Fatalf("orphan: %+v, calls %d", got, r.calls.Load())
	}
}
