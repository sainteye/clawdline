package projectassistant

import "errors"

// ErrorCode is stable across ports, HTTP, Cloud and scripted fixtures. Message
// copy belongs to the transport's localized catalog, not error string parsing.
type ErrorCode string

const (
	Invalid                          ErrorCode = "pa_invalid"
	Disabled                         ErrorCode = "pa_disabled"
	ReadOnly                         ErrorCode = "pa_read_only"
	NotFound                         ErrorCode = "pa_not_found"
	Offline                          ErrorCode = "pa_offline"
	Unsupported                      ErrorCode = "pa_unsupported"
	Unpaired                         ErrorCode = "pa_unpaired"
	Revoked                          ErrorCode = "pa_revoked"
	StaleGeneration                  ErrorCode = "pa_stale_generation"
	StaleVersion                     ErrorCode = "pa_stale_version"
	IdempotencyConflict              ErrorCode = "pa_idempotency_conflict"
	CapacityFull                     ErrorCode = "pa_capacity_full"
	OutcomeUnknown                   ErrorCode = "pa_outcome_unknown"
	Expired                          ErrorCode = "pa_expired"
	ScopeDenied                      ErrorCode = "pa_scope_denied"
	ContextStale                     ErrorCode = "pa_context_stale"
	CursorExpired                    ErrorCode = "pa_cursor_expired"
	CLINotInstalled                  ErrorCode = "cli_not_installed"
	AdapterNotFound                  ErrorCode = "adapter_not_found"
	AdapterInventoryUnknown          ErrorCode = "adapter_inventory_unknown"
	ProviderVersionUnsupported       ErrorCode = "provider_version_unsupported"
	AuthRequired                     ErrorCode = "auth_required"
	AuthProbeUnknown                 ErrorCode = "auth_probe_unknown"
	AuthRejected                     ErrorCode = "auth_rejected"
	AuthUnavailableInIsolatedRuntime ErrorCode = "auth_unavailable_in_isolated_runtime"
	SandboxUnavailable               ErrorCode = "sandbox_unavailable"
	ReadOnlyUnverified               ErrorCode = "read_only_unverified"
	ScopeExpansionDenied             ErrorCode = "scope_expansion_denied"
	MemoryIsolationUnverified        ErrorCode = "memory_isolation_unverified"
	ResumeForbiddenAfterForget       ErrorCode = "resume_forbidden_after_forget"
	ResumeUnsupported                ErrorCode = "resume_unsupported"
	ExecutionEffectUnknown           ErrorCode = "execution_effect_unknown"
)

type Failure struct {
	Code              ErrorCode `json:"code"`
	Field             string    `json:"field"`
	RetryAfterSeconds int64     `json:"retry_after_seconds"`
}

func (f *Failure) Error() string              { return string(f.Code) + ": " + f.Field }
func fail(code ErrorCode, field string) error { return &Failure{Code: code, Field: field} }

func Code(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var f *Failure
	if errors.As(err, &f) {
		return f.Code
	}
	return OutcomeUnknown
}
