package projectassistantacp

import (
	"bytes"
	"encoding/json"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

var _ app.NativeContextReader = (*Client)(nil)

// NativeContext returns only this live native session's own readings. Reports
// received before session/new answers remain unqualified until its ID matches.
func (c *Client) NativeContext() app.NativeContextState {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := app.NativeContextState{Fence: c.fence, NativeSessionID: c.sessionID}
	if c.sessionID == "" || c.nativeContext.NativeSessionID != c.sessionID || c.failure != nil || c.cancelled {
		return state
	}
	state.CommandsKnown = c.nativeContext.CommandsKnown
	state.CompactAdvertised = c.nativeContext.CompactAdvertised
	state.UsageKnown = c.nativeContext.UsageKnown
	state.UsedTokens = c.nativeContext.UsedTokens
	state.WindowTokens = c.nativeContext.WindowTokens
	return state
}

func (c *Client) updateNativeContext(sessionID, kind string, commands, used, size json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if sessionID == "" || (c.sessionID != "" && c.sessionID != sessionID) {
		return refusal(pa.StaleGeneration, "native_session")
	}
	// One Client opens one session. Before its reply, never combine metadata
	// from two identities or let optional reports establish the session ID.
	if c.sessionID == "" && c.nativeContext.NativeSessionID != "" && c.nativeContext.NativeSessionID != sessionID {
		return refusal(pa.StaleGeneration, "native_session")
	}
	if c.failure != nil || c.cancelled {
		return nil
	}
	if c.nativeContext.NativeSessionID != sessionID {
		c.nativeContext = app.NativeContextState{NativeSessionID: sessionID}
	}
	switch kind {
	case "available_commands_update":
		var offered []struct {
			Name string `json:"name"`
		}
		known := len(commands) > 0 && !bytes.Equal(bytes.TrimSpace(commands), []byte("null")) && json.Unmarshal(commands, &offered) == nil
		compact := false
		for _, command := range offered {
			if command.Name == "" {
				known = false
			}
			compact = compact || command.Name == "compact"
		}
		c.nativeContext.CommandsKnown = known
		c.nativeContext.CompactAdvertised = known && compact
	case "usage_update":
		var usedTokens, windowTokens *int64
		known := json.Unmarshal(used, &usedTokens) == nil && json.Unmarshal(size, &windowTokens) == nil && usedTokens != nil && windowTokens != nil && *usedTokens >= 0 && *windowTokens > 0
		c.nativeContext.UsageKnown = known
		c.nativeContext.UsedTokens, c.nativeContext.WindowTokens = 0, 0
		if known {
			c.nativeContext.UsedTokens, c.nativeContext.WindowTokens = *usedTokens, *windowTokens
		}
	}
	return nil
}
