package http

import (
	"encoding/json"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"net/http"
	"strings"
	"time"
)

func paRoutineRoute(r *http.Request) (paHTTPRoute, bool) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/project-assistants/"), "/")
	if len(parts) < 2 || parts[1] != "routines" || !paHTTPSafeBotID(parts[0]) {
		return paHTTPRoute{}, false
	}
	out := paHTTPRoute{bot: parts[0]}
	if len(parts) == 2 {
		if r.Method == "GET" {
			out.action = "routines"
		}
		if r.Method == "POST" {
			out.action = "routine-save"
		}
	}
	if len(parts) >= 3 && paHTTPSafeBotID(parts[2]) {
		out.offer = parts[2]
		if len(parts) == 3 && r.Method == "DELETE" {
			out.action = "routine-delete"
		}
		if len(parts) == 4 && parts[3] == "enabled" && r.Method == "POST" {
			out.action = "routine-enabled"
		}
		if len(parts) == 4 && parts[3] == "runs" {
			if r.Method == "GET" {
				out.action = "routine-runs"
			}
			if r.Method == "POST" {
				out.action = "routine-test"
			}
		}
	}
	return out, out.action != ""
}

func paRoutineTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
func paRoutineTrigger(t pa.RoutineTrigger) contract.ProjectAssistantRoutineTrigger {
	return contract.ProjectAssistantRoutineTrigger{Type: t.Type, Seconds: t.Seconds, At: t.At, Expression: t.Expression, TimeZone: t.TimeZone}
}

func (h *projectAssistantHTTP) readRoutines(r *http.Request, route paHTTPRoute) (any, error) {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return nil, paHTTPFailure(pa.Invalid, "body")
	}
	q, err := paHTTPQuery(r, "machine_id", "project_id", "canonical_id", "kind", "conversation_id", "cursor", "page_size")
	if err != nil {
		return nil, err
	}
	key, err := h.queryKey(q, route)
	if err != nil {
		return nil, err
	}
	size, err := paHTTPNumber(q, "page_size")
	if err != nil || size < 1 || size > pa.MaxPageRows {
		return nil, paHTTPFailure(pa.Invalid, "page_size")
	}
	project := pa.ProjectScope{MachineID: key.MachineID, ProjectID: q.Get("project_id"), CanonicalID: q.Get("canonical_id"), Kind: pa.ProjectKind(q.Get("kind"))}
	if err = pa.ValidateScope(project); err != nil {
		return nil, err
	}
	result, err := h.options.Service.RoutinePage(r.Context(), app.RoutineQuery{Key: key, Project: project, ConversationID: q.Get("conversation_id"), RoutineID: route.offer, Cursor: q.Get("cursor"), PageSize: int(size)})
	if err != nil {
		return nil, err
	}
	source, err := paWireSourceStatus(result.Source)
	if err != nil {
		return nil, err
	}
	out := contract.ProjectAssistantRoutineListResponse{ContractVersion: pa.ContractVersion, Source: source, Fence: paWireFence(result.Page.Fence), Definitions: []contract.ProjectAssistantRoutineDefinition{}, Runs: []contract.ProjectAssistantRoutineRun{}, NextCursor: result.Page.NextCursor}
	for _, d := range result.Page.Definitions {
		v := contract.ProjectAssistantRoutineDefinition{ID: d.ID, Key: paWireBotKey(d.Key), Project: paWireProjectScope(d.Project), ConversationID: d.ConversationID, Name: d.Name, Instruction: d.Instruction, TimeoutSeconds: d.TimeoutSeconds, Enabled: d.Enabled, Deleted: d.Deleted, NextDue: paRoutineTime(d.NextDue), Version: d.Version, Triggers: []contract.ProjectAssistantRoutineTrigger{}, NextRuns: []string{}}
		for _, t := range d.ScheduleTriggers() {
			v.Triggers = append(v.Triggers, paRoutineTrigger(t))
		}
		for _, at := range d.NextRuns {
			v.NextRuns = append(v.NextRuns, paRoutineTime(at))
		}
		if len(d.Triggers) == 0 {
			v.NextRuns = append(v.NextRuns, paRoutineTime(d.NextDue))
		}
		out.Definitions = append(out.Definitions, v)
	}
	for _, item := range result.Page.Runs {
		run := item.Run
		v := contract.ProjectAssistantRoutineRun{ID: run.ID, RoutineID: run.RoutineID, Source: run.Source, State: contract.ProjectAssistantTurnState(run.State), TurnID: run.TurnID, CreatedAt: paRoutineTime(run.CreatedAt), Result: item.Result}
		if run.FinishedAt != nil {
			v.FinishedAt = paRoutineTime(*run.FinishedAt)
		}
		if item.Failure != nil {
			f := paWireFailure(*item.Failure)
			v.Failure = &f
		}
		out.Runs = append(out.Runs, v)
	}
	body, err := paPackRoutinePage(out, result.Page.Cursors)
	if err != nil {
		return nil, err
	}
	return paEncodedOutput{body: body, check: result.Revalidate}, nil
}

// Find the largest whole prefix that fits the actual encrypted-reply envelope.
// Every row carries its source-signed continuation; no cursor is synthesized.
func paPackRoutinePage(out contract.ProjectAssistantRoutineListResponse, cursors []string) ([]byte, error) {
	n := len(out.Definitions) + len(out.Runs)
	if n != len(cursors) {
		return nil, paHTTPFailure(pa.Invalid, "routine_cursors")
	}
	encode := func(count int) ([]byte, bool, error) {
		candidate := out
		if len(out.Definitions) > 0 {
			candidate.Definitions = out.Definitions[:count]
		} else {
			candidate.Runs = out.Runs[:count]
		}
		if count < n && count > 0 {
			candidate.NextCursor = cursors[count-1]
		}
		body, err := json.Marshal(candidate)
		if err != nil {
			return nil, false, err
		}
		size, err := cloudops.ProjectAssistantReadReplySize("read:"+strings.Repeat("<", 128), http.StatusOK, body)
		return body, len(body) <= cloudops.ReadPlaintextBytesLimit && size <= cloudops.ReadPlaintextBytesLimit, err
	}
	if body, fits, err := encode(n); err != nil {
		return nil, err
	} else if fits {
		return body, nil
	}
	low, high := 1, n-1
	var best []byte
	for low <= high {
		mid := low + (high-low)/2
		body, fits, err := encode(mid)
		if err != nil {
			return nil, err
		}
		if fits {
			best = body
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	if best == nil {
		return nil, paHTTPFailure(pa.CapacityFull, "routine_page_bytes")
	}
	return best, nil
}

func (h *projectAssistantHTTP) writeRoutine(w http.ResponseWriter, r *http.Request, route paHTTPRoute) (any, error) {
	var cmd contract.ProjectAssistantCommand
	var fence contract.ProjectAssistantFence
	var revision int64
	var id string
	q := app.RoutineRequest{}
	switch route.action {
	case "routine-save":
		v, err := paDecodeHTTP[contract.ProjectAssistantRoutineSaveRequest](w, r)
		if err != nil {
			return nil, err
		}
		cmd, fence, revision, id = v.Command, v.Fence, v.ExpectedRevision, v.ID
		q.Action = "save"
		q.Name = v.Name
		q.Instruction = v.Instruction
		q.TimeoutSeconds = v.TimeoutSeconds
		q.Enabled = &v.Enabled
		q.Triggers = []pa.RoutineTrigger{}
		if len(v.Triggers) < 1 || len(v.Triggers) > pa.MaxRoutineTriggers {
			return nil, paHTTPFailure(pa.Invalid, "routine_triggers")
		}
		for _, t := range v.Triggers {
			q.Triggers = append(q.Triggers, pa.RoutineTrigger{Type: t.Type, Seconds: t.Seconds, At: t.At, Expression: t.Expression, TimeZone: t.TimeZone})
		}
	case "routine-enabled":
		v, err := paDecodeHTTP[contract.ProjectAssistantRoutineEnabledRequest](w, r)
		if err != nil {
			return nil, err
		}
		cmd, fence, revision, id = v.Command, v.Fence, v.ExpectedRevision, v.ID
		q.Action = "enable"
		q.Enabled = &v.Enabled
	case "routine-delete", "routine-test":
		v, err := paDecodeHTTP[contract.ProjectAssistantRoutineActionRequest](w, r)
		if err != nil {
			return nil, err
		}
		cmd, fence, revision, id = v.Command, v.Fence, v.ExpectedRevision, v.ID
		if route.action == "routine-delete" {
			q.Action = "delete"
		} else {
			q.Action = "test"
		}
	default:
		return nil, paHTTPFailure(pa.Invalid, "routine_action")
	}
	if route.offer != "" && id != route.offer {
		return nil, paHTTPFailure(pa.ScopeDenied, "routine_id")
	}
	c, err := h.command(r, route, cmd, pa.EditOperation)
	if err != nil {
		return nil, err
	}
	f, err := paHTTPFence(c, fence)
	if err != nil {
		return nil, err
	}
	q.ID = id
	check, err := h.options.Service.HumanRoutinePublication(r.Context(), c.Key)
	if err != nil {
		return nil, err
	}
	receipt, err := h.options.Service.HumanRoutine(r.Context(), app.HumanRoutineCommand{Command: c, Fence: f, ExpectedRevision: revision, Request: q})
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(contract.ProjectAssistantReceiptResponse{ContractVersion: pa.ContractVersion, Receipt: paWireReceipt(receipt)})
	if err != nil {
		return nil, err
	}
	return paEncodedOutput{body: body, check: check}, nil
}
