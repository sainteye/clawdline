package projectassistant

import (
	"context"
	"encoding/json"
)

// BrowserTools is optional and entirely bound by the current Host turn. The
// Host derives the tab owner; no argument names an owner, Bot, Fence, approval
// or explicit browser choice.
type BrowserTools interface {
	BrowserStatus(context.Context) (BrowserStatus, error)
	BrowserCommand(context.Context, BrowserCommand) (BrowserResult, error)
}

// BrowserCommand is one existing browser operation with its unchanged JSON
// arguments. Mode may name a browser profile only when automatic selection
// already allows it.
type BrowserCommand struct {
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args,omitempty"`
	Mode string          `json:"browser_mode,omitempty"`
}

// BrowserResult is the extension's value for the command.
type BrowserResult struct {
	Value json.RawMessage `json:"value,omitempty"`
}

// BrowserStatus reports only the current owner's selected browser and tabs.
type BrowserStatus struct {
	Mode      string       `json:"browser_mode"`
	Connected bool         `json:"connected"`
	Stopped   bool         `json:"stopped"`
	Tabs      []BrowserTab `json:"tabs"`
}

type BrowserTab struct {
	Tab     int    `json:"tab"`
	Mode    string `json:"browser_mode"`
	Current bool   `json:"current"`
	Person  bool   `json:"person_controlling"`
}

// BrowserFailure keeps the browser engine's typed refusal code, or
// browser_page_error with the page's own message, across the MCP boundary.
type BrowserFailure struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

func (f *BrowserFailure) Error() string { return f.Code }
