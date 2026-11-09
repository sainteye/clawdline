package antigravityprocess

import (
	"bytes"
	"encoding/json"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// Grok replies are routed against a registered client RPC, not the lifetime of
// a model turn. An idle reply must never block the reader on the next caller.
// Our IDs are numeric; string replies cannot settle any of our RPCs. Active
// numeric mismatches still fail closed. No discarded frame is queued or applied.
func (c *Client) routeGrokResponse(frame json.RawMessage, fields map[string]json.RawMessage) (bool, *Error) {
	if c.opts.Assistant != session.AssistantGrok {
		return false, nil
	}
	c.stateMu.Lock()
	expected := c.pendingResponseID
	c.stateMu.Unlock()
	id := bytes.TrimSpace(fields["id"])
	stringID := len(id) > 0 && id[0] == '"'
	if expected != "" && !stringID {
		if string(id) != expected {
			return false, failure("unexpected_id", Uncertain)
		}
		if _, err := response(frame, expected); err != nil && !isRPCError(err) {
			return false, failure("invalid_response", Uncertain)
		}
		return false, nil
	}
	// Null is valid only as an uncorrelated JSON-RPC response. It never settles
	// an active numeric request. The ordinary response codec validates its body.
	if !validID(id) && !bytes.Equal(id, []byte("null")) {
		return false, failure("invalid_response", Uncertain)
	}
	if _, err := response(frame, string(id)); err != nil && !isRPCError(err) {
		return false, failure("invalid_response", Uncertain)
	}
	c.lifeMu.Lock()
	if expected == "" {
		c.lifecycle.OrphanResponses++
		c.lifecycle.OrphanResponseIDKind, _ = responseIdentity(id, "")
	} else {
		c.lifecycle.UnmatchedResponses++
	}
	c.lifeMu.Unlock()
	stage := "unmatched_response"
	if expected == "" {
		stage = "orphan_response"
	}
	c.observe("local", stage, nil, 0)
	return true, nil
}
