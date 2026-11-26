package projectassistant

import (
	"context"
	"errors"
	"fmt"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestProviderFailurePreservesDomainCodeAndUnknown(t *testing.T) {
	for _, code := range []pa.ErrorCode{pa.Unsupported, pa.ExecutionEffectUnknown, pa.OutcomeUnknown} {
		original := &pa.Failure{Code: code, Field: "rpc_error"}
		failure := &ProviderFailure{Reason: ProviderUsageExhausted, RPCCode: -32603, Cause: original}
		err := fmt.Errorf("wait: %w", failure)
		var detail *ProviderFailure
		var domain *pa.Failure
		if !errors.As(err, &detail) || detail != failure || !errors.As(err, &domain) || domain != original || pa.Code(err) != code {
			t.Fatalf("lost error identity or code: %v", err)
		}
		joined := errors.Join(&pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "cleanup"}, err)
		if pa.Code(joined) != pa.ExecutionEffectUnknown || !errors.As(joined, &detail) {
			t.Fatalf("cleanup lost precedence: %v", joined)
		}
	}
}

type failingWaitProvider struct {
	*runtimeProvider
	failure error
}

func (p failingWaitProvider) Open(ctx context.Context, q OpenRequest, sink func(context.Context, ProtocolEvent) error) (ProviderSession, error) {
	s, err := p.runtimeProvider.Open(ctx, q, sink)
	if err != nil {
		return nil, err
	}
	return failingWaitSession{ProviderSession: s, failure: p.failure}, nil
}

type failingWaitSession struct {
	ProviderSession
	failure error
}

func (s failingWaitSession) Send(ctx context.Context, q PromptRequest) (PendingPrompt, error) {
	if _, err := s.ProviderSession.Send(ctx, q); err != nil {
		return nil, err
	}
	return failedProviderPrompt{failure: s.failure}, nil
}

type failedProviderPrompt struct{ failure error }

func (p failedProviderPrompt) Wait(context.Context) (PromptResult, error) {
	return PromptResult{}, p.failure
}

func TestProviderFailureAfterSendRemainsUnknownWithoutReplay(t *testing.T) {
	for _, tt := range []struct {
		name    string
		failure error
	}{
		{"quota", &ProviderFailure{Reason: ProviderUsageExhausted, RPCCode: -32603, Cause: &pa.Failure{Code: pa.Unsupported, Field: "rpc_-32603"}}},
		{"eof", &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "acp_eof"}},
		{"quota-with-unknown", errors.Join(&pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "partial_stdin"}, &ProviderFailure{Reason: ProviderUsageExhausted, RPCCode: -32603, Cause: &pa.Failure{Code: pa.Unsupported, Field: "rpc_-32603"}})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newRuntimeFixture(t)
			f.r.o.Provider = failingWaitProvider{runtimeProvider: f.provider, failure: tt.failure}
			receipt, err := f.submit(t, "one", "one")
			if err != nil {
				t.Fatal(err)
			}
			receivePrompt(t, f.provider)
			awaitTurn(t, f, receipt.ResourceID, pa.TurnUnknown)
			replayed, err := f.submit(t, "one", "one")
			if err != nil || replayed.ID != receipt.ID {
				t.Fatalf("receipt=%+v err=%v", replayed, err)
			}
			if err = f.r.Recover(context.Background(), f.key); err != nil {
				t.Fatal(err)
			}
			if f.provider.count() != 1 || len(f.provider.sent) != 0 {
				t.Fatal("unknown prompt opened another provider or sent again")
			}
		})
	}
}

func TestUnknownTurnExplainsInterruptionWithoutReplaying(t *testing.T) {
	for _, tt := range []struct {
		name  string
		cause error
		field string
	}{
		{"deadline", context.DeadlineExceeded, "prompt_timeout"},
		{"cleanup deadline", &ownedCleanupFailure{context.DeadlineExceeded}, "owned_cleanup_timeout"},
		{"cleanup after prompt cancellation", errors.Join(context.Canceled, &ownedCleanupFailure{context.DeadlineExceeded}), "owned_cleanup_timeout"},
		{"cancelled", context.Canceled, "prompt_stopped"},
		{"connection", &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "acp_eof"}, "acp_eof"},
		{"output limit", &pa.Failure{Code: pa.Unsupported, Field: "prompt_max_tokens"}, "prompt_max_tokens"},
		{"quota", &ProviderFailure{Reason: ProviderUsageExhausted, Cause: &pa.Failure{Code: pa.Unsupported, Field: "rpc_-32603"}}, "provider_usage_limit"},
		{"untrusted field", &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "private native error narrative"}, "execution"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newRuntimeFixture(t)
			f.r.o.Provider = failingWaitProvider{runtimeProvider: f.provider, failure: tt.cause}
			receipt, err := f.submit(t, "synthetic input", "interruption")
			if err != nil {
				t.Fatal(err)
			}
			receivePrompt(t, f.provider)
			awaitTurn(t, f, receipt.ResourceID, pa.TurnUnknown)
			turn, err := f.store.Turn(context.Background(), f.key, receipt.ResourceID)
			if err != nil || turn.Failure == nil || turn.Failure.Field != tt.field {
				t.Fatalf("interruption detail: %+v %v, want %s", turn.Failure, err, tt.field)
			}
			replayed, err := f.submit(t, "synthetic input", "interruption")
			if err != nil || replayed.ID != receipt.ID {
				t.Fatalf("receipt changed: %+v %v", replayed, err)
			}
			if err = f.r.Recover(context.Background(), f.key); err != nil {
				t.Fatal(err)
			}
			if f.provider.count() != 1 || len(f.provider.sent) != 0 {
				t.Fatal("unknown prompt replayed")
			}
		})
	}
}

// A context refusal names the exact check that refused, so an interrupted
// turn says which context moved instead of the generic execution field.
func TestUnknownTurnKeepsTheContextCheckThatRefused(t *testing.T) {
	stale := fmt.Errorf("send: %w", &pa.Failure{Code: pa.ContextStale, Field: "memory_provenance"})
	if got := executionFailureField(stale); got != "memory_provenance" {
		t.Fatalf("context refusal lost its check: %q", got)
	}
	for _, other := range []error{
		&pa.Failure{Code: pa.Unsupported, Field: "private native narrative"},
		&pa.Failure{Code: pa.ContextStale},
		errors.New("plain"),
	} {
		if got := executionFailureField(other); got != "execution" {
			t.Fatalf("%v became public field %q", other, got)
		}
	}
}
