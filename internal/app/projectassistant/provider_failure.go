package projectassistant

import (
	"context"
	"errors"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// executionFailureField keeps only diagnostic categories owned by the host.
// Neither a native error narrative nor an arbitrary adapter field is public copy.
// This metadata never proves no effects and never authorizes a prompt replay.
func executionFailureField(cause error) string {
	if errors.Is(cause, context.DeadlineExceeded) {
		return "prompt_timeout"
	}
	if errors.Is(cause, context.Canceled) {
		return "prompt_stopped"
	}
	var provider *ProviderFailure
	if errors.As(cause, &provider) {
		switch provider.Reason {
		case ProviderUsageExhausted, ProviderOutOfCredits, ProviderRateLimited,
			ProviderOverloaded, ProviderSignedOut, ProviderModelMissing:
			return "provider_" + string(provider.Reason)
		}
	}
	var failure *pa.Failure
	if errors.As(cause, &failure) {
		// Capacity fields already identify the registered local boundary.
		if failure.Code == pa.CapacityFull && failure.Field != "" {
			return failure.Field
		}
		if failure.Code == pa.Invalid {
			switch failure.Field {
			case "update", "tool_content", "tool_terminal_id":
				return "native_update"
			}
		}
		switch failure.Field {
		case "acp_eof", "prompt_stopped", "prompt_result", "prompt_max_tokens", "prompt_max_turn_requests", "owned_cleanup":
			return failure.Field
		}
	}
	return "execution"
}

// ProviderFailureReason describes an allowlisted native error category. It is
// diagnostic metadata, never evidence that a prompt had no effects or may replay.
type ProviderFailureReason string

const (
	ProviderUsageExhausted ProviderFailureReason = "usage_limit"
	ProviderOutOfCredits   ProviderFailureReason = "out_of_credits"
	ProviderRateLimited    ProviderFailureReason = "rate_limited"
	ProviderOverloaded     ProviderFailureReason = "overloaded"
	ProviderSignedOut      ProviderFailureReason = "signed_out"
	ProviderModelMissing   ProviderFailureReason = "model_missing"
)

// ProviderFailure preserves the original domain failure through errors.As and
// pa.Code. Only normalized metadata belongs here; native messages and data must
// not escape the adapter into projections, persistence or logs. An empty Reason
// means the RPC error has no recognized classification. Unknown effect and
// cleanup failures retain their existing authority regardless of this metadata.
type ProviderFailure struct {
	Reason  ProviderFailureReason
	RPCCode int
	Cause   error
}

func (f *ProviderFailure) Error() string { return f.Cause.Error() }
func (f *ProviderFailure) Unwrap() error { return f.Cause }

// TerminalQuotaFailure is the native adapter's typed terminal quota rejection
// for the current prompt. The engine must still rule out observed activity
// and close the owned session before another Agent can receive the input.
type TerminalQuotaFailure struct {
	Failure  *ProviderFailure
	NoOutput bool
}

func (f *TerminalQuotaFailure) Error() string { return f.Failure.Error() }
func (f *TerminalQuotaFailure) Unwrap() error { return f.Failure }

// PreSendRefusal is a provider's authoritative guarantee that Open refused
// before any prompt or external action. It must return a cleanup handle too.
// Native adapters do not construct this until separately qualified. A typed
// ProviderFailure, including usage_limit, does not implement this guarantee.
type PreSendRefusal struct{ Failure *ProviderFailure }

func (f *PreSendRefusal) Error() string { return f.Failure.Error() }
func (f *PreSendRefusal) Unwrap() error { return f.Failure }

// OpenNotStarted is a trusted host-only fact: provider Open was never entered
// and setup registrations have been reclaimed. It does not prove no effects
// and never permits fallback. It only distinguishes no process to clean up
// from an unknown Open that returned no cleanup handle.
type OpenNotStarted struct{ Cause error }

func (f *OpenNotStarted) Error() string { return f.Cause.Error() }
func (f *OpenNotStarted) Unwrap() error { return f.Cause }
