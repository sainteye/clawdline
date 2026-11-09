package antigravityprocess

import (
	"bytes"
	"encoding/json"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// Grok can emit replies in a string-ID namespace while our requests use only
// numbers. Such replies cannot cross the active prompt metadata/completion
// boundary. An unrelated, valid response is observed and discarded, never
// queued for a later request. Numeric mismatches and control RPCs retain their
// fail-closed behavior. JSON strings are never coerced to numbers.
func (c *Client) skipUnmatchedGrokResponse(frame json.RawMessage, fields map[string]json.RawMessage) (bool, *Error) {
	if c.opts.Assistant != session.AssistantGrok {
		return false, nil
	}
	c.stateMu.Lock()
	expected := c.activePrompt
	c.stateMu.Unlock()
	id := bytes.TrimSpace(fields["id"])
	if expected == "" || string(id) == expected || len(id) == 0 || id[0] != '"' || !validID(id) {
		return false, nil
	}
	// The wrong identity does not excuse an invalid response envelope.
	if _, err := response(frame, string(id)); err != nil && !isRPCError(err) {
		return false, failure("invalid_response", Uncertain)
	}
	c.lifeMu.Lock()
	c.lifecycle.UnmatchedResponses++
	c.lifeMu.Unlock()
	c.observe("local", "unmatched_response", nil, 0)
	return true, nil
}
