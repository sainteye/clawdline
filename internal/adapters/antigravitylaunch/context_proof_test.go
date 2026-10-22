package antigravitylaunch

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app/ports"
)

func proofFixture(t *testing.T) (fixture, Prepared, ports.AntigravityBinding, string, string, transcript.AntigravityContextCheck, transcript.AntigravityContextFence, string) {
	t.Helper()
	f := setup(t)
	p := prep(t, f)
	native := pin(t, f.store, p)
	home := t.TempDir()
	b := binding(native, uuid.NewString(), 1)
	path, ok := transcript.AntigravityPath(home, b.ConversationID)
	if !ok {
		t.Fatal("synthetic native path refused")
	}
	b.TranscriptPath = path
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	initial := `{"step_index":0,"source":"MODEL","type":"GENERIC","status":"DONE","content":"synthetic native baseline","created_at":"2026-10-01T12:00:00Z"}` + "\n"
	put(t, path, []byte(initial))
	if err := f.store.RecordBinding(context.Background(), p.Record.ID, b, KeepConversation); err != nil {
		t.Fatal(err)
	}
	check := func(_ context.Context, current ports.AntigravityBinding) error {
		if current != b {
			return errors.New("synthetic binding mismatch")
		}
		return nil
	}
	fence, err := transcript.CaptureAntigravityContextFence(context.Background(), home, b, check)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../transcript/testdata/antigravity-context.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	injection, err := f.store.ExpectedInjection(context.Background(), p.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	const fixtureText = "Synthetic immutable context.\nRole ALDER; language zh-Hant-TW; memory MAPLE."
	content := row["content"].(string)
	if strings.Count(content, fixtureText) != 1 {
		t.Fatal("native fixture context missing")
	}
	row["content"] = strings.Replace(content, fixtureText, injection.Text, 1)
	data, err = json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	return f, p, b, home, path, check, fence, initial + string(data) + "\n"
}

func TestContextProofPersistsBaselineBeforeResponseAndLoadsOnce(t *testing.T) {
	ctx := context.Background()
	f, p, b, home, path, check, fence, data := proofFixture(t)
	response, err := f.store.PrepareFencedResponse(ctx, b, fence)
	if err != nil || len(response) == 0 {
		t.Fatal(err)
	}
	receipt, ok, err := f.store.Receipt(ctx, p.Record.ID)
	if err != nil || !ok || !receipt.Fenced || receipt.Loaded || receipt.Fence != fence || receipt.Status != "PREPARED" {
		t.Fatal("response escaped without baseline", receipt, err)
	}
	if err := f.store.VerifyContextLoaded(ctx, p.Record.ID, home, check); err == nil {
		t.Fatal("prepared became loaded without native row")
	}
	put(t, path, []byte(data))
	if err := f.store.VerifyContextLoaded(ctx, p.Record.ID, home, check); err != nil {
		t.Fatal(err)
	}
	if err := f.store.CheckContextLoaded(ctx, p.Record.ID, home, check); err != nil {
		t.Fatal(err)
	}
	receipt, _, err = f.store.Receipt(ctx, p.Record.ID)
	if err != nil || !receipt.Loaded {
		t.Fatal("native proof not durable", err)
	}
	if err := f.store.VerifyContextLoaded(ctx, p.Record.ID, home, check); err != nil {
		t.Fatal("idempotent proof changed", err)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	historical, _, err := reopened.Receipt(ctx, p.Record.ID)
	if err != nil || historical != receipt {
		t.Fatal("reopen lost historical proof", err)
	}
	if err := reopened.CheckContextLoaded(ctx, p.Record.ID, home, check); err == nil {
		t.Fatal("historical proof reactivated process")
	}
	if err := reopened.PinProcess(ctx, p.Record.ID, b.Process); err != nil {
		t.Fatal(err)
	}
	if err := reopened.CheckContextLoaded(ctx, p.Record.ID, home, check); err != nil {
		t.Fatal("fresh native proof could not restore badge eligibility", err)
	}
	put(t, path, []byte(strings.Replace(data, "12:00:01Z", "12:00:02Z", 1)))
	if err := reopened.CheckContextLoaded(ctx, p.Record.ID, home, check); err == nil {
		t.Fatal("rewritten proof row retained badge")
	}
}

func TestContextProofDoesNotRetrofitOrReplaceOutstandingResponse(t *testing.T) {
	for _, kind := range []string{"retrofit", "pending", "wrong-binding", "write-failure", "lost-publication", "deactivated-during-read", "invalidated-context"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			f, p, b, home, path, check, fence, data := proofFixture(t)
			if kind == "retrofit" {
				if _, err := f.store.PreInvocationResponse(ctx, b); err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.PrepareFencedResponse(ctx, b, fence); err == nil {
					t.Fatal("retrofit historical unfenced response")
				}
				return
			}
			if kind == "wrong-binding" {
				fence.Binding.Sequence++
				if _, err := f.store.PrepareFencedResponse(ctx, b, fence); err == nil {
					t.Fatal("foreign binding fence accepted")
				}
				return
			}
			if _, err := f.store.PrepareFencedResponse(ctx, b, fence); err != nil {
				t.Fatal(err)
			}
			put(t, path, []byte(data))
			switch kind {
			case "pending":
				next := b
				next.Sequence++
				if err := f.store.RecordBinding(ctx, p.Record.ID, next, KeepConversation); err != nil {
					t.Fatal(err)
				}
				nf := fence
				nf.Binding = next
				if _, err := f.store.PrepareFencedResponse(ctx, next, nf); err == nil {
					t.Fatal("overwrote outstanding fence")
				}
				if _, err := f.store.PreInvocationResponse(ctx, next); err == nil {
					t.Fatal("unfenced path bypassed outstanding proof")
				}
				if err := f.store.VerifyContextLoaded(ctx, p.Record.ID, home, check); err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.PrepareFencedResponse(ctx, next, nf); err != nil {
					t.Fatal("loaded response could not advance", err)
				}
				return
			case "write-failure":
				f.store.beforeWrite = func(string) error { return errors.New("synthetic full disk") }
			case "lost-publication":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				f.store.afterPublish = cancel
			case "deactivated-during-read":
				count := 0
				check = func(context.Context, ports.AntigravityBinding) error {
					count++
					if count == 2 {
						return f.store.Deactivate(context.Background(), p.Record.ID)
					}
					return nil
				}
			case "invalidated-context":
				put(t, p.Record.ContextPath, []byte("synthetic changed context"))
			}
			if err := f.store.VerifyContextLoaded(ctx, p.Record.ID, home, check); err == nil {
				t.Fatal("failed publication/native generation became loaded")
			}
			if kind == "lost-publication" {
				code(t, f.store.ready(context.Background()), "reopen_required")
			} else {
				receipt, _, err := f.store.Receipt(ctx, p.Record.ID)
				if err == nil && receipt.Loaded {
					t.Fatal("failed proof acquired badge authority")
				}
			}
		})
	}
}

func TestContextProofCurrentCheckRejectsChangedLatestReceipt(t *testing.T) {
	ctx := context.Background()
	f, p, b, home, path, check, fence, data := proofFixture(t)
	if _, err := f.store.PrepareFencedResponse(ctx, b, fence); err != nil {
		t.Fatal(err)
	}
	put(t, path, []byte(data))
	if err := f.store.VerifyContextLoaded(ctx, p.Record.ID, home, check); err != nil {
		t.Fatal(err)
	}
	calls := 0
	check = func(context.Context, ports.AntigravityBinding) error {
		calls++
		if calls == 2 {
			next := b
			next.Sequence++
			if err := f.store.RecordBinding(ctx, p.Record.ID, next, KeepConversation); err != nil {
				return err
			}
			newer := fence
			newer.Binding = next
			_, err := f.store.PrepareFencedResponse(ctx, next, newer)
			return err
		}
		return nil
	}
	code(t, f.store.CheckContextLoaded(ctx, p.Record.ID, home, check), "proof_stale")
}

func TestContextProofVersionTwoMigrationPreservesConsumedSlot(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	p := prep(t, f)
	native := pin(t, f.store, p)
	if _, _, err := f.store.ReserveFirstInput(ctx, p.Record.ID, uuid.NewString(), hash([]byte("synthetic input")), native); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(f.config.StateDir, "context-index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var old map[string]any
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatal(err)
	}
	old["version"] = 2
	data, err = json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	put(t, indexPath, data)
	reopened, err := Open(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := os.ReadFile(indexPath)
	if err != nil || string(after) != string(data) {
		t.Fatal("Open rewrote legacy index", err)
	}
	if len(reopened.state.FirstInputs) != 1 || reopened.state.Version != 3 {
		t.Fatal("migration dropped consumed slot")
	}
	if err := reopened.PinProcess(ctx, p.Record.ID, native); err != nil {
		t.Fatal(err)
	}
	for _, r := range reopened.state.FirstInputs {
		_, attempt, err := reopened.ReserveFirstInput(ctx, p.Record.ID, r.RequestID, r.TextDigest, native)
		if err != nil || attempt != nil {
			t.Fatal("migration reconstructed execution grant", err)
		}
	}
}
