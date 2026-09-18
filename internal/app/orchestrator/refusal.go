package orchestrator

import "net/http"

// Refusal is a typed no in the Swift app's envelope.
//
// The envelope matters as much as the code. Every refusal from the Swift
// broker is `{"error":{"code","message","request_id", …extra}}`, and the extra
// keys go **inside** `error` — a caller reading `stale_inventory` finds the
// whole inventory there, and one reading `workspace_busy` finds the blocking
// task there. A reimplementation that puts them beside `error` is a different
// protocol wearing the same words.
type Refusal struct {
	Status  int
	Code    string
	Message string
	// Extra is merged into the error object. It is `map[string]any` rather
	// than a generated type because its shape differs per code, and the one
	// thing it must never be is a second envelope.
	Extra map[string]any
}

func (r Refusal) Error() string { return r.Code + ": " + r.Message }

func refuse(status int, code, message string) Refusal {
	return Refusal{Status: status, Code: code, Message: message}
}

func refuseWith(status int, code, message string, extra map[string]any) Refusal {
	return Refusal{Status: status, Code: code, Message: message, Extra: extra}
}

// The refusals more than one route raises, spelled once.
var (
	errNoTask   = refuse(http.StatusNotFound, "not_found", "No task named that")
	errBadSecre = refuse(http.StatusForbidden, "forbidden", "That is not this task's secret.")
)

// notFoundTask is the 404 every route but the ACK uses.
func notFoundTask() Refusal { return errNoTask }

// isNotFound is the one answer that proves an id is free.
func isNotFound(err error) bool {
	ref, ok := err.(Refusal)
	return ok && ref.Code == errNoTask.Code && ref.Status == errNoTask.Status
}

// badSecret is the 403 a wrong task secret gets. It says nothing about whether
// the task exists — the task is looked up first, so a caller with a bad secret
// has already been told the id is real, which is the Swift app's order and is
// not a leak: holding an id is not holding a capability.
func badSecret() Refusal { return errBadSecre }
