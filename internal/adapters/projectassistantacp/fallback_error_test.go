package projectassistantacp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/adapters/projectassistanttest"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Every classification goes through the real subprocess, scanner, ordered
// consumer and PendingPrompt.Wait. No provider or authentication service runs.
func TestACPProviderFailureClassification(t *testing.T) {
	for _, tt := range []struct {
		name, payload string
		reason        app.ProviderFailureReason
		code          int
	}{
		{"usage", `{"code":-32603,"message":"private diagnostic","data":{"errorKind":"usage_limit_exceeded"}}`, app.ProviderUsageExhausted, -32603},
		{"auth-code", `{"code":-32000,"message":"private diagnostic"}`, app.ProviderSignedOut, -32000},
		{"auth-category", `{"code":-32603,"message":"private diagnostic","data":{"category":"auth"}}`, app.ProviderSignedOut, -32603},
		{"rate", `{"code":-32603,"message":"429","data":{"errorKind":"rate_limit"}}`, app.ProviderRateLimited, -32603},
		{"billing", `{"code":-32603,"message":"credits low","data":{"errorKind":"billing_error"}}`, app.ProviderOutOfCredits, -32603},
		{"usage-before-billing", `{"code":-32603,"message":"Your org is out of usage","data":{"errorKind":"billing_error"}}`, app.ProviderUsageExhausted, -32603},
		{"usage-details", `{"code":-32603,"message":"Internal error","data":{"category":"account_on_hold","details":"You've hit your limit"}}`, app.ProviderUsageExhausted, -32603},
		{"overloaded", `{"code":-32603,"message":"private diagnostic","data":{"category":"server_overloaded"}}`, app.ProviderOverloaded, -32603},
		{"codex-string", `{"code":-32603,"message":"private diagnostic","data":{"codexErrorInfo":"usageLimitExceeded"}}`, app.ProviderUsageExhausted, -32603},
		{"codex-object", `{"code":-32603,"message":"private diagnostic","data":{"codexErrorInfo":{"modelNotFound":{}}}}`, app.ProviderModelMissing, -32603},
		{"field-precedence", `{"code":-32603,"message":"private diagnostic","data":{"errorKind":"auth","category":"billing_error","codexErrorInfo":"usageLimitExceeded"}}`, app.ProviderSignedOut, -32603},
		{"invalid-category", `{"code":-32603,"message":"usage limit","data":{"errorKind":"usage-limit-exceeded","category":"auth"}}`, "", -32603},
		{"non-ascii", `{"code":-32603,"message":"usage limit","data":{"category":"額度"}}`, "", -32603},
		{"oversize", fmt.Sprintf(`{"code":-32603,"message":"usage limit","data":{"category":%q}}`, strings.Repeat("a", 41)), "", -32603},
		{"empty-first-field", `{"code":-32603,"message":"usage limit","data":{"errorKind":"","category":"auth"}}`, "", -32603},
		{"unknown", `{"code":-32603,"message":"usage limit","data":{"category":"invalid_request"}}`, "", -32603},
		{"generic-internal", `{"code":-32603,"message":"Internal error"}`, "", -32603},
		{"uncategorized-quote", `{"code":-32603,"message":"usage limit authentication_failed 429"}`, "", -32603},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLAWDLINE_ACP_ERROR", tt.payload)
			c, _ := openTestClient(t, "provider-error", func(context.Context, app.ProtocolEvent) error { return nil })
			_, err := sendTestPrompt(t, c).Wait(context.Background())
			var detail *app.ProviderFailure
			var original *pa.Failure
			if !errors.As(fmt.Errorf("wait: %w", err), &detail) || detail.Reason != tt.reason || detail.RPCCode != tt.code {
				t.Fatalf("detail=%+v err=%v", detail, err)
			}
			if pa.Code(err) != pa.Unsupported || !errors.As(err, &original) || original.Field != fmt.Sprintf("rpc_%d", tt.code) {
				t.Fatalf("domain semantics changed: %v", err)
			}
			if err.Error() != fmt.Sprintf("pa_unsupported: rpc_%d", tt.code) {
				t.Fatalf("native diagnostic escaped: %v", err)
			}
		})
	}
}

type acpFailureRuntimeProvider struct {
	app.WorkBridge
	configuration Configuration
	opens         atomic.Int32
}

func (*acpFailureRuntimeProvider) Capabilities(_ context.Context, b pa.Bot) (pa.Capabilities, error) {
	return pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, CLIVersion: "scripted", AdapterVersion: "fixture", Evidence: "scripted-only", ReadOnlyEnforced: true, NativeMemory: pa.NativeMemoryDisabled, NativeMemoryVerified: true}, nil
}

func (p *acpFailureRuntimeProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	p.opens.Add(1)
	proc, err := StartProcess(ctx, ProcessOptions{Configuration: p.configuration, Directory: q.Directory, Limits: testLimits()})
	if err != nil {
		return nil, err
	}
	c, err := NewClient(proc, testLimits(), q.Binding.Fence, sink, nil)
	if err != nil {
		_ = proc.Close(context.Background())
		return nil, err
	}
	if err = c.initialize(ctx, q); err != nil {
		return c, err
	}
	return c, nil
}

type acpFailureResolver struct {
	scope     pa.ProjectScope
	directory string
}

func (r acpFailureResolver) Resolve(context.Context, pa.ProjectSelection) (pa.ProjectResolution, error) {
	return pa.ProjectResolution{Scope: r.scope, Directory: r.directory, Available: true, Current: true}, nil
}

// This crosses the real ACP client and runtime persistence boundary together:
// a written prompt followed by quota/EOF remains unknown, including recovery.
func TestACPProviderFailureRuntimeDoesNotReplay(t *testing.T) {
	if inventoryUnavailable() {
		t.Skip("process inventory is unavailable; safe startup admission must refuse")
	}
	for _, mode := range []string{"provider-error", "provider-error-eof", "exit"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "prompts.jsonl")
			t.Setenv("CLAWDLINE_ACP_PROMPT_LOG", path)
			t.Setenv("CLAWDLINE_ACP_ERROR", `{"code":-32603,"message":"private diagnostic","data":{"errorKind":"usage_limit_exceeded"}}`)
			db, err := projectassistanttest.Bootstrap(ctx, store.Installer{})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			repo := store.New(db)
			fence := testFence()
			bot := pa.Bot{Key: fence.Key, Project: fence.Project, ConversationID: fence.ConversationID, Name: "Scripted Bot", Appearance: pa.Appearance{Shape: pa.Blob, Color: "#e39567"}, Agent: pa.Codex, PermissionPolicy: pa.Ask, Instructions: pa.InstructionSnapshot{Revision: 1, Text: "fixed"}, Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			conversation := pa.Conversation{Key: bot.Key, ID: bot.ConversationID, Version: 1, MemoryRevision: 1}
			command := pa.Command{Key: bot.Key, Operation: pa.CreateOperation, IdempotencyKey: "create", ContentHash: strings.Repeat("a", 64)}
			if _, err = repo.Commit(ctx, app.Mutation{Command: command, Bot: &bot, Conversation: &conversation, Receipt: pa.Receipt{ID: "created", State: pa.EffectDelivered, ResourceID: bot.Key.BotID}}); err != nil {
				t.Fatal(err)
			}
			provider := &acpFailureRuntimeProvider{configuration: peerConfiguration(t, mode)}
			source := pa.SourceStatus{MachineID: bot.Key.MachineID, State: pa.SourceOnline, Readable: true, Writable: true, Fresh: true, Access: pa.Interactive}
			var sequence atomic.Int32
			r, err := app.NewRuntime(app.RuntimeOptions{Repository: repo, Memory: repo, WorkBridge: provider, ProjectResolver: acpFailureResolver{scope: bot.Project, directory: t.TempDir()}, Provider: provider, Clock: time.Now, ID: func() string { return fmt.Sprintf("id-%d", sequence.Add(1)) }, Authorize: func(context.Context, pa.BotKey) (pa.FeatureState, pa.SourceStatus, error) {
				return pa.FeatureState{Enabled: true}, source, nil
			}, Limits: app.RuntimeLimits{MaxBots: 1, MaxProcesses: 1, PromptTimeout: 3 * time.Second, CleanupTimeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close(ctx)
			command.Operation = pa.SendOperation
			command.IdempotencyKey = "send"
			input := app.Submission{Command: command, Feature: pa.FeatureState{Enabled: true}, Source: source, Message: pa.SendMessage{Fence: pa.Fence{Key: bot.Key, Project: bot.Project, ConversationID: bot.ConversationID}, Text: "hello"}}
			receipt, err := r.Submit(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				turn, e := repo.Turn(ctx, bot.Key, receipt.ResourceID)
				if e == nil && turn.State == pa.TurnUnknown {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("turn=%+v err=%v", turn, e)
				}
				time.Sleep(time.Millisecond)
			}
			replayed, err := r.Submit(ctx, input)
			if err != nil || replayed.ID != receipt.ID {
				t.Fatalf("replay=%+v err=%v", replayed, err)
			}
			if err = r.Recover(ctx, bot.Key); err != nil {
				t.Fatal(err)
			}
			if err = r.Close(ctx); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(path)
			if err != nil || strings.Count(string(b), "\n") != 1 || provider.opens.Load() != 1 {
				t.Fatalf("prompts=%q opens=%d err=%v", b, provider.opens.Load(), err)
			}
		})
	}
}

func TestACPProviderFailureDoesNotReplayWrittenPrompt(t *testing.T) {
	for _, mode := range []string{"provider-error", "provider-error-eof", "exit"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "prompts.jsonl")
			t.Setenv("CLAWDLINE_ACP_PROMPT_LOG", path)
			t.Setenv("CLAWDLINE_ACP_ERROR", `{"code":-32603,"message":"private diagnostic","data":{"errorKind":"usage_limit_exceeded"}}`)
			c, _ := openTestClient(t, mode, func(context.Context, app.ProtocolEvent) error { return nil })
			pending := sendTestPrompt(t, c)
			if _, err := pending.Wait(context.Background()); err == nil {
				t.Fatal("failure became success")
			} else if code := pa.Code(err); code != pa.Unsupported && code != pa.ExecutionEffectUnknown {
				t.Fatalf("code=%s", code)
			}
			b, err := os.ReadFile(path)
			if err != nil || strings.Count(string(b), "\n") != 1 {
				t.Fatalf("written prompts=%q err=%v", b, err)
			}
		})
	}
}

func TestACPReplyQuotaWordsAreOrdinaryText(t *testing.T) {
	c, _ := openTestClient(t, "quota-reply", func(context.Context, app.ProtocolEvent) error { return nil })
	result, err := sendTestPrompt(t, c).Wait(context.Background())
	if err != nil || !strings.Contains(result.FinalText, "usage limit") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestACPErrorKindBoundary(t *testing.T) {
	for _, tt := range []struct {
		kind  string
		valid bool
	}{{strings.Repeat("a", 40), true}, {strings.Repeat("a", 41), false}, {"rate_limit", true}, {"rateLimit", false}, {"", false}} {
		if validErrorKind(tt.kind) != tt.valid {
			t.Fatalf("validation of %q", tt.kind)
		}
	}
}
