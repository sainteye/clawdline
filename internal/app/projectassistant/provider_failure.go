package projectassistant

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
