package taskresources

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// Diagnostic contains source-owned correlation only, never database error text,
// filesystem paths, query contents or resource/output data.
type Diagnostic struct {
	Code              string `json:"code"`
	Caller            string `json:"caller"`
	CallerID          string `json:"caller_id,omitempty"`
	CallbackTaskID    string `json:"callback_task_id,omitempty"`
	Phase             string `json:"phase"`
	ResourceID        string `json:"resource_id,omitempty"`
	Cause             string `json:"cause"`
	ContextState      string `json:"context_state"`
	HasDeadline       bool   `json:"has_deadline"`
	DeadlineUnixMilli int64  `json:"deadline_unix_ms,omitempty"`
}

type Failure struct {
	Code   string `json:"code"`
	Cause  error  `json:"-"`
	detail Diagnostic
}

var errMeasurementChanged = errors.New("accounted resources changed during measurement")

func (e Failure) Error() string { return e.Code }
func (e Failure) Unwrap() error { return e.Cause }
func fail(code string) Failure  { return Failure{Code: code} }

func processTableDecisionFailure(ctx context.Context, code, phase, resource string) error {
	if !taskresource.IsProcessTableFailure(code) {
		return nil
	}
	return fail(code).WithContext(ctx, phase, resource)
}

type callerKey struct{}
type caller struct{ kind, id, callback string }

// WithCaller is set by the owning adapter, not decoded from request metadata.
func WithCaller(ctx context.Context, kind, id string) context.Context {
	switch kind {
	case "callback.start", "heavy.acquire", "resources.admit", "resources.allocate", "resources.inspect", "resources.publish":
	default:
		kind = "resources"
	}
	c := caller{kind: kind, id: opaqueID(id)}
	if kind == "callback.start" {
		c.callback = c.id
	}
	return context.WithValue(ctx, callerKey{}, c)
}

// WithCallback retains the correlation supplied by an accepted lease request.
// It is not proof that the callback started or that the caller owns its data.
func WithCallback(ctx context.Context, id string) context.Context {
	c, _ := ctx.Value(callerKey{}).(caller)
	c.callback = opaqueID(id)
	return context.WithValue(ctx, callerKey{}, c)
}

func opaqueID(id string) string {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return ""
	}
	return id
}

func causeCode(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, sql.ErrNoRows):
		return "not_found"
	case errors.Is(err, store.ErrBusy):
		return "store_busy"
	case errors.Is(err, errMeasurementChanged):
		return "revision_changed"
	case err != nil:
		return "read_failed"
	default:
		return "unavailable"
	}
}

func (e Failure) WithContext(ctx context.Context, phase, resource string) Failure {
	if e.detail.Phase != "" {
		return e
	}
	c, ok := ctx.Value(callerKey{}).(caller)
	if !ok {
		c.kind = "resources"
	}
	e.detail = Diagnostic{Code: e.Code, Caller: c.kind, CallerID: c.id, CallbackTaskID: c.callback, Phase: phase, ResourceID: opaqueID(resource), Cause: causeCode(e.Cause), ContextState: "active"}
	if e.Cause == nil {
		switch e.Code {
		case "disk_reserve_unavailable":
			e.detail.Cause = "capacity"
		case taskresource.ProcessTableStalled:
			e.detail.Cause = taskresource.ProcessTableStalled
		case taskresource.ProcessTableTimeout:
			e.detail.Cause = "deadline_exceeded"
		}
	}
	if err := ctx.Err(); err != nil {
		e.detail.ContextState = causeCode(err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		e.detail.HasDeadline = true
		e.detail.DeadlineUnixMilli = deadline.UnixMilli()
	}
	return e
}

func (e Failure) Diagnostic() Diagnostic { return e.detail }

// Fields keeps the structured receipt and exposes scalar correlation to the
// existing CLI refusal renderer, which deliberately skips nested objects.
func (d Diagnostic) Fields() map[string]any {
	fields := map[string]any{"resource_diagnostic": d, "caller": d.Caller, "phase": d.Phase, "cause": d.Cause, "context_state": d.ContextState, "has_deadline": d.HasDeadline}
	if d.CallerID != "" {
		fields["caller_id"] = d.CallerID
	}
	if d.CallbackTaskID != "" {
		fields["callback_task_id"] = d.CallbackTaskID
	}
	if d.ResourceID != "" {
		fields["resource_id"] = d.ResourceID
	}
	if d.HasDeadline {
		fields["deadline_unix_ms"] = d.DeadlineUnixMilli
	}
	return fields
}

// Explain adds safe correlation without changing an existing typed cause.
func Explain(ctx context.Context, err error, phase string) Failure {
	var failure Failure
	if errors.As(err, &failure) {
		return failure.WithContext(ctx, phase, "")
	}
	// Older source adapters return these fixed codes as plain errors. Keep
	// their stated reason without copying arbitrary error text into a receipt.
	// In particular, plain text "resource_not_found" is never absence proof.
	if err != nil {
		switch err.Error() {
		case "resource_measurement_unknown", "resource_disk_space_unknown", "resource_service_unavailable", "resource_owner_unknown", "resource_project_unknown", "resource_owner_already_started", "resource_identity_changed", "resource_shared_cache_unknown", "resource_source_unknown", "resource_shared_use_unverified", "resource_shared_use_missing":
			return (Failure{Code: err.Error(), Cause: err}).WithContext(ctx, phase, "")
		}
	}
	return readFailure(ctx, err, phase, "")
}

func withDefaultCaller(ctx context.Context, kind string) context.Context {
	if _, ok := ctx.Value(callerKey{}).(caller); ok {
		return ctx
	}
	return WithCaller(ctx, kind, "")
}

func readFailure(ctx context.Context, err error, phase, resource string) Failure {
	code := "resource_read_failed"
	switch {
	case errors.Is(err, sql.ErrNoRows):
		code = "resource_not_found"
	case errors.Is(err, context.Canceled):
		code = "resource_lookup_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		code = "resource_lookup_deadline_exceeded"
	}
	return (Failure{Code: code, Cause: err}).WithContext(ctx, phase, resource)
}

func measurementFailure(code string, cause error) Failure {
	var source Failure
	if errors.As(cause, &source) {
		switch source.Code {
		case "resource_not_found", "resource_lookup_canceled", "resource_lookup_deadline_exceeded", "resource_read_failed", "resource_measurement_interrupted":
			return source
		}
	}
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		code = "resource_measurement_interrupted"
	}
	return Failure{Code: code, Cause: cause}
}
