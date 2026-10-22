package antigravitylaunch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPreInvocationPreparedReceiptAndFailure(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := prep(t, f)
	native := pin(t, f.store, p)
	b := binding(native, uuid.NewString(), 1)
	if _, err := f.store.PreInvocationResponse(ctx, b); err == nil {
		t.Fatal("unrecorded binding accepted")
	}
	if err := f.store.RecordBinding(ctx, p.Record.ID, b, KeepConversation); err != nil {
		t.Fatal(err)
	}
	f.store.beforeWrite = func(string) error { return errors.New("synthetic full disk") }
	if _, err := f.store.PreInvocationResponse(ctx, b); err == nil {
		t.Fatal("write failure swallowed")
	}
	if _, ok, err := f.store.Receipt(ctx, p.Record.ID); err != nil || ok {
		t.Fatal("failed response created receipt", err)
	}
	f.store.beforeWrite = nil
	response, err := f.store.PreInvocationResponse(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := f.store.ExpectedInjection(ctx, p.Record.ID)
	if err != nil || !strings.Contains(expected.Text, "ROLE_COPPER") || !strings.Contains(expected.Text, "MEMORY_FIR") || !strings.Contains(expected.Text, "zh-Hant-TW") {
		t.Fatal("original context missing", err)
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(response, &object) != nil || len(object["injectSteps"]) == 0 {
		t.Fatal("no native injection envelope")
	}
	receipt, ok, err := f.store.Receipt(ctx, p.Record.ID)
	if err != nil || !ok || receipt.Status != "PREPARED" || receipt.InjectionDigest != expected.Digest || receipt.ResponseDigest != hash(response) || receipt.Process != native {
		t.Fatal(receipt, ok, err)
	}
	again, err := f.store.PreInvocationResponse(ctx, b)
	if err != nil || string(again) != string(response) {
		t.Fatal("identical retry changed prepared bytes", err)
	}
	for seq := uint64(2); seq <= 4; seq++ {
		b = binding(native, b.ConversationID, seq)
		if err := f.store.RecordBinding(ctx, p.Record.ID, b, KeepConversation); err != nil {
			t.Fatal(err)
		}
		if seq == 2 {
			cancelCtx, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := f.store.PreInvocationResponse(cancelCtx, b); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			r, ok, err := f.store.Receipt(ctx, p.Record.ID)
			if err != nil || !ok || r != receipt {
				t.Fatal("cancellation overwrote earlier prepared receipt")
			}
		}
		if _, err := f.store.PreInvocationResponse(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.store.state.Receipts) != 1 {
		t.Fatal("receipt queue grew per event")
	}
	old := binding(native, b.ConversationID, 1)
	if _, err := f.store.PreInvocationResponse(ctx, old); err == nil {
		t.Fatal("old event executed")
	}
	if r, ok, err := f.store.Receipt(ctx, p.Record.ID); err != nil || !ok || r.Status != "PREPARED" || r.Sequence != 4 {
		t.Fatal(r, ok, err)
	}
}

func TestBindingConflictsNeverOverwriteHistory(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := prep(t, f)
	native := pin(t, f.store, p)
	id := uuid.NewString()
	b := binding(native, id, 1)
	unverified := b
	unverified.Verified = false
	code(t, f.store.RecordBinding(ctx, p.Record.ID, unverified, KeepConversation), "binding_invalid")
	if err := f.store.RecordBinding(ctx, p.Record.ID, b, KeepConversation); err != nil {
		t.Fatal(err)
	}
	changed := b
	changed.TranscriptPath += "changed"
	code(t, f.store.RecordBinding(ctx, p.Record.ID, changed, KeepConversation), "binding_conflict")
	// Fill historical native UUIDs, each supported by a positive binding; the
	// refusal keeps the first context and latest valid binding intact.
	for i := 1; i < MaxHistory; i++ {
		b = binding(native, uuid.NewString(), uint64(i+1))
		if err := f.store.RecordBinding(ctx, p.Record.ID, b, AllowConversationSwitch); err != nil {
			t.Fatal(i, err)
		}
	}
	last := f.store.state.Records[p.Record.ID]
	code(t, f.store.RecordBinding(ctx, p.Record.ID, binding(native, uuid.NewString(), MaxHistory+1), AllowConversationSwitch), "history_full")
	if f.store.state.Records[p.Record.ID] != last {
		t.Fatal("full history published rejected binding")
	}
	if _, err := f.store.Restore(ctx, p.Request.ProjectRoot, id); err != nil {
		t.Fatal("full history lost original", err)
	}
}

func TestResponseCancelledAfterRenameRequiresReopen(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := prep(t, f)
	native := pin(t, f.store, p)
	b := binding(native, uuid.NewString(), 1)
	if err := f.store.RecordBinding(ctx, p.Record.ID, b, KeepConversation); err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	f.store.afterPublish = cancel
	if response, err := f.store.PreInvocationResponse(cancelCtx, b); !errors.Is(err, context.Canceled) || response != nil {
		t.Fatal("uncertain response escaped", err)
	}
	_, _, err := f.store.Receipt(ctx, p.Record.ID)
	code(t, err, "reopen_required")
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	r, ok, err := reopened.Receipt(ctx, p.Record.ID)
	if err != nil || !ok || r.Status != "PREPARED" || r.Sequence != b.Sequence {
		t.Fatal("durable prepared evidence lost", r, err)
	}
	if _, err := reopened.LookupProcess(ctx, native); err == nil {
		t.Fatal("restart activated process")
	}
}
