package http

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline-go/internal/contract"
)

// The retired per-message workflow route.
//
// The Swift app put a `<clawdline-workflow>` envelope after every message it
// sent into a session, and the envelope told the assistant to run the app's
// `clawdline-board-workflow` helper: a shell script that asks whoami for its
// terminal and then posts a `begin` or a `deliver` to
// POST /v1/orchestrator/sessions/<terminal>/workflow. That envelope and the
// classification it carried are not carried over (design-decisions D36, U5):
// the board is read from broker facts and a person's decisions, and an
// assistant opening its own cards was the source of the noise that decision
// removed.
//
// The helper does not know that. It lives in the Swift app's bundle, sessions
// briefed before the switch keep running it, and it answers every refusal the
// way a broken step answers — non-zero, with the error on stdout — so a 404
// here would be read, in every one of those sessions, as a board that broke.
// So the route stays, as the smallest thing that is neither: it takes the
// command in the Swift app's envelope, records nothing — no run, no receipt,
// no card, no event — and answers 200 with a code that says the step is
// retired. What it must never do is look like the Swift app's receipt (a 202,
// `workflow_<operation>_recorded`, an `item_id`): that would be the retired
// protocol answering again, and an assistant would go on believing a board
// item was bound.
//
// How often it is still called is counted, in memory, for /v1/diagnostics:
// that count staying at zero is when this route can be deleted.

// workflowBodyLimit is the Swift route's own bound on one command, which is
// also what the helper checks before it sends.
const workflowBodyLimit = 64 << 10

// workflowRetiredMessage is the one sentence the helper prints.
const workflowRetiredMessage = "Clawdline no longer records per-message workflow steps: " +
	"nothing was recorded, nothing is owed, and this step can be skipped from now on."

// retiredWorkflow counts the calls. An observation of this process: a restart
// starts it again, and `since` says from when.
type retiredWorkflow struct {
	calls atomic.Int64
	last  atomic.Int64
}

func (c *retiredWorkflow) note(at time.Time) {
	c.calls.Add(1)
	c.last.Store(at.Unix())
}

// diagnostics is the block /v1/diagnostics carries under broker.
func (c *retiredWorkflow) diagnostics() *contract.BrokerWorkflowRetired {
	return &contract.BrokerWorkflowRetired{Calls: c.calls.Load(), LastAt: c.last.Load(), Since: epoch}
}

// brokerSessionWorkflow answers the helper. Identity is not resolved: the
// answer does not depend on which session asked, and resolving it would add a
// way for a step that does nothing to fail.
func (s *Server) brokerSessionWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"the workflow route is read with GET and written with POST")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden",
			"Recording managed workflow facts needs the machine credential.")
		return
	}
	var runID *string
	if r.Method == http.MethodPost {
		raw, err := io.ReadAll(io.LimitReader(r.Body, workflowBodyLimit+1))
		var body map[string]any
		if err != nil || len(raw) > workflowBodyLimit || json.Unmarshal(raw, &body) != nil || body == nil ||
			strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
			writeAuthRefusal(w, http.StatusBadRequest, "bad_request",
				"A bounded JSON command and Idempotency-Key are required.")
			return
		}
		// The caller's own run id, echoed so the answer reads as an answer to
		// this command; bounded as the Swift route bounded it.
		if v, ok := body["run_id"].(string); ok && v != "" && len(v) <= 80 {
			runID = &v
		}
		s.retired.note(time.Now())
	}
	writeJSON(w, contract.BrokerWorkflowAnswer{
		OK: true,
		Workflow: contract.BrokerWorkflowOutcome{
			Status:    http.StatusOK,
			Code:      "workflow_retired",
			Authority: "none",
			Recorded:  false,
			RunID:     runID,
			Message:   workflowRetiredMessage,
		},
	})
}
