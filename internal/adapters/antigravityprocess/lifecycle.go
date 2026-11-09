package antigravityprocess

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"
)

// Lifecycle describes protocol evidence, not whether the model is using CPU.
// Only the matching prompt response ends a turn. Silence and text never do.
type Lifecycle struct {
	UnmatchedResponses uint64 `json:"unmatched_responses,omitempty"`
	ResponseIDKind     string `json:"response_id_kind,omitempty"`
	ResponseIDRelation string `json:"response_id_relation,omitempty"`
	Phase              string `json:"phase"`
	RequestID          string `json:"request_id,omitempty"`
	Operation          string `json:"operation,omitempty"`
	StartedAt          int64  `json:"started_at,omitempty"`
	CancelRequestedAt  int64  `json:"cancel_requested_at,omitempty"`
	LastReceivedAt     int64  `json:"last_received_at,omitempty"`
	LastReceivedKind   string `json:"last_received_kind,omitempty"`
	LastAppliedAt      int64  `json:"last_applied_at,omitempty"`
	LastAppliedKind    string `json:"last_applied_kind,omitempty"`
	CompletedAt        int64  `json:"completed_at,omitempty"`
	StopReason         string `json:"stop_reason,omitempty"`
	RPCError           *int64 `json:"rpc_error,omitempty"`
	Failure            string `json:"failure,omitempty"`
	Sequence           uint64 `json:"sequence"`
	Unrecognized       uint64 `json:"unrecognized"`
	TraceEnabled       bool   `json:"trace_enabled"`
	TraceError         string `json:"trace_error,omitempty"`
}

// WireEvent is private diagnostic evidence. Frame is the exact line before
// decoding, or outbound JSON before the newline is added. Written counts pipe
// bytes, not provider acceptance. Local events have no frame.
type WireEvent struct {
	Sequence   uint64 `json:"sequence"`
	At         int64  `json:"at_ns"`
	Direction  string `json:"direction"`
	Stage      string `json:"stage"`
	Kind       string `json:"kind,omitempty"`
	Frame      []byte `json:"frame,omitempty"`
	Written    int    `json:"written,omitempty"`
	Phase      string `json:"phase"`
	RequestID  string `json:"request_id,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
	Failure    string `json:"failure,omitempty"`
	PID        int    `json:"pid"`
}

func (c *Client) Lifecycle() Lifecycle {
	c.lifeMu.Lock()
	defer c.lifeMu.Unlock()
	out := c.lifecycle
	if out.Phase == "" {
		out.Phase = "starting"
	}
	if out.RPCError != nil {
		n := *out.RPCError
		out.RPCError = &n
	}
	return out
}

// observe orders diagnostic enqueue calls independently of conversation/state
// locks. An unavailable recorder is visible but cannot strand the protocol.
func (c *Client) observe(direction, stage string, frame []byte, written int) {
	c.lifeMu.Lock()
	defer c.lifeMu.Unlock()
	l := &c.lifecycle
	l.Sequence++
	l.TraceEnabled = c.opts.OnWire != nil
	at := time.Now().UnixNano()
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Update struct {
				Kind string `json:"sessionUpdate"`
			} `json:"update"`
		} `json:"params"`
	}
	_ = json.Unmarshal(frame, &m)
	kind := wireKind(m.Method, m.Params.Update.Kind)
	if m.Method == "" && len(m.ID) > 0 {
		kind = "response"
	}
	if direction == "in" && stage == "received" {
		l.LastReceivedAt, l.LastReceivedKind = at, kind
		if m.Method == "" {
			l.ResponseIDKind, l.ResponseIDRelation = responseIdentity(m.ID, l.RequestID)
		}
		if kind == "unrecognized" {
			l.Unrecognized++
		}
	}
	if stage == "applied" {
		l.LastAppliedAt, l.LastAppliedKind = at, kind
	}
	e := WireEvent{Sequence: l.Sequence, At: at, Direction: direction, Stage: stage, Kind: kind, Frame: frame, Written: written, Phase: l.Phase, RequestID: l.RequestID, StopReason: l.StopReason, Failure: l.Failure}
	if c.cmd != nil && c.cmd.Process != nil {
		e.PID = c.cmd.Process.Pid
	}
	if stage != "received" && stage != "write_started" {
		e.Frame = nil
	}
	// The persisted record owns its own encoding before this borrowed frame returns.
	if c.opts.OnWire != nil && c.opts.OnWire(e) != nil {
		l.TraceError = "trace_write_failed"
	}
}

func wireKind(method, update string) string {
	if method == "session/update" {
		switch update {
		case "user_message_chunk", "agent_message_chunk", "agent_thought_chunk", "tool_call", "tool_call_update", "plan", "usage_update", "available_commands_update", "current_mode_update", "config_option_update", "session_info_update", "notice", "compaction_update", "compaction_summary_chunk":
			return update
		}
		return "unrecognized"
	}
	switch method {
	case "initialize", "authenticate", "session/new", "session/load", "session/resume", "session/list", "session/prompt", "session/cancel", "session/request_permission", "session/set_model", "session/set_mode", "session/set_config_option", "$/cancel_request":
		return method
	}
	return "unrecognized"
}

func (c *Client) requestStarted(id, method string, metadata bool) {
	c.lifeMu.Lock()
	defer c.lifeMu.Unlock()
	l := &c.lifecycle
	if l.Phase == "closed" {
		return
	}
	l.Phase, l.RequestID, l.Operation = "control_pending", id, method
	if method == "session/prompt" {
		l.Phase, l.Operation = "prompt_pending", "prompt"
	}
	if metadata {
		l.Operation = "usage"
	}
	l.ResponseIDKind, l.ResponseIDRelation = "", ""
	l.UnmatchedResponses = 0
	l.StartedAt, l.CancelRequestedAt = time.Now().UnixNano(), 0
	l.CompletedAt, l.StopReason, l.RPCError = 0, "", nil
}

func (c *Client) requestSettled(id string, result json.RawMessage, err error) {
	c.lifeMu.Lock()
	l := &c.lifecycle
	if l.RequestID == id && l.Phase != "closed" {
		var r struct {
			StopReason string `json:"stopReason"`
		}
		_ = json.Unmarshal(result, &r)
		switch r.StopReason {
		case "", "end_turn", "max_tokens", "max_turn_requests", "refusal", "cancelled":
			l.StopReason = r.StopReason
		default:
			// Opaque provider strings remain only in the private frame.
			l.StopReason = "unrecognized"
		}
		if err == nil || isRPCError(err) {
			l.Phase = "idle"
			l.RequestID = ""
			l.CompletedAt = time.Now().UnixNano()
		}
		if e, ok := err.(*Error); ok && e.Code == "rpc_error" {
			l.Phase = "request_failed"
			n := e.RPCCode
			l.RPCError = &n
		}
	}
	c.lifeMu.Unlock()
	c.observe("local", "request_settled", nil, 0)
}

func isRPCError(err error) bool {
	e, ok := err.(*Error)
	return ok && e.Code == "rpc_error" && e.Delivery == Acknowledged
}

func (c *Client) permissionPhase(waiting bool) {
	c.lifeMu.Lock()
	if c.lifecycle.RequestID == "" || c.lifecycle.Phase == "closed" || c.lifecycle.Phase == "cancel_requested" {
		c.lifeMu.Unlock()
		return
	}
	if waiting {
		c.lifecycle.Phase = "permission_pending"
	} else {
		c.lifecycle.Phase = "prompt_pending"
	}
	c.lifeMu.Unlock()
	stage := "permission_resolved"
	if waiting {
		stage = "permission_opened"
	}
	c.observe("local", stage, nil, 0)
}

func (c *Client) sessionReady() {
	c.lifeMu.Lock()
	if c.lifecycle.RequestID == "" && c.lifecycle.Phase != "closed" {
		c.lifecycle.Phase = "idle"
	}
	c.lifeMu.Unlock()
}

func (c *Client) cancelRequested(id string) {
	c.lifeMu.Lock()
	if c.lifecycle.RequestID == id && c.lifecycle.Phase != "closed" {
		c.lifecycle.Phase = "cancel_requested"
		c.lifecycle.CancelRequestedAt = time.Now().UnixNano()
	}
	c.lifeMu.Unlock()
	c.observe("local", "cancel_requested", nil, 0)
}

// Keep only type and relation to our own numeric request sequence. Provider IDs
// can contain account data, so neither the ID nor a provider error body is kept.
func responseIdentity(raw json.RawMessage, expected string) (kind, relation string) {
	raw = bytes.TrimSpace(raw)
	kind, relation = "invalid", "different"
	switch {
	case len(raw) == 0:
		return "missing", relation
	case bytes.Equal(raw, []byte("null")):
		return "null", relation
	case raw[0] == '"':
		var value string
		if json.Unmarshal(raw, &value) == nil {
			return "string", relation
		}
	default:
		var value json.Number
		if json.Unmarshal(raw, &value) == nil {
			kind = "number"
		}
	}
	if kind == "number" {
		if string(raw) == expected {
			return kind, "matching"
		}
		id, err := strconv.ParseUint(string(raw), 10, 64)
		want, wantErr := strconv.ParseUint(expected, 10, 64)
		if err == nil && wantErr == nil {
			if id < want {
				relation = "previous"
			} else if id > want {
				relation = "future"
			}
		}
	}
	return kind, relation
}
