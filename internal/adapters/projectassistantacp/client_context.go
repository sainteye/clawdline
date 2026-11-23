package projectassistantacp

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

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
	if c.sessionID == "" || c.failure != nil || c.cancelled {
		return state
	}
	state.Model = c.observedModel
	if c.nativeContext.NativeSessionID != c.sessionID {
		return state
	}
	state.CommandsKnown = c.nativeContext.CommandsKnown
	state.CompactAdvertised = c.nativeContext.CompactAdvertised
	state.UsageKnown = c.nativeContext.UsageKnown
	state.UsedTokens = c.nativeContext.UsedTokens
	state.WindowTokens = c.nativeContext.WindowTokens
	if c.observed.count > 0 {
		state.CompactedTurnID, state.Compactions = c.observed.turnID, c.observed.count
		state.CompactedBefore, state.CompactedAfter = c.observed.before, c.observed.after
	}
	// A completed compaction with no reading after it: the last reading is
	// the compaction call's own (Codex sends it first), not the context left.
	if c.observed.awaitUsage {
		state.UsageKnown, state.UsedTokens, state.WindowTokens = false, 0, 0
	}
	return state
}

func modelLabel(catalog pa.ConfigurationCatalog) string {
	if catalog.Model == "" {
		return ""
	}
	visible := func(label string) bool {
		return strings.TrimSpace(label) != "" && utf8.RuneCountInString(label) <= pa.MaxNameRunes &&
			!strings.ContainsFunc(label, unicode.IsControl)
	}
	for _, option := range catalog.Options {
		if option.Category != "model" {
			continue
		}
		for _, choice := range option.Choices {
			if choice.Value == catalog.Model && visible(choice.Name) {
				return strings.TrimSpace(choice.Name)
			}
		}
	}
	if visible(catalog.Model) {
		return strings.TrimSpace(catalog.Model)
	}
	return ""
}

func (c *Client) updateObservedModel(sessionID string, params json.RawMessage) error {
	var update struct {
		Configuration json.RawMessage `json:"update"`
	}
	var model string
	if json.Unmarshal(params, &update) == nil {
		if catalog, err := parseConfiguration(update.Configuration, c.agent); err == nil {
			model = modelLabel(catalog)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if sessionID == "" || (c.sessionID != "" && sessionID != c.sessionID) {
		return refusal(pa.StaleGeneration, "native_session")
	}
	if c.sessionID == sessionID && c.failure == nil && !c.cancelled {
		c.observedModel = model
	}
	return nil
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
		if known {
			c.observeUsageLocked(c.nativeContext.UsageKnown, c.nativeContext.UsedTokens, *usedTokens)
		}
		c.nativeContext.UsageKnown = known
		c.nativeContext.UsedTokens, c.nativeContext.WindowTokens = 0, 0
		if known {
			c.nativeContext.UsedTokens, c.nativeContext.WindowTokens = *usedTokens, *windowTokens
		}
	}
	return nil
}
