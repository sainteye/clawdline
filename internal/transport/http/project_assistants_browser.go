package http

import (
	"context"
	"errors"

	"github.com/sainteye/clawdline/internal/adapters/projectassistanthost"
	"github.com/sainteye/clawdline/internal/app/browserbridge"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
)

// projectAssistantBrowser gives Host-bound Bot turns the same browser engine
// the CLI uses, under a Host-derived owner. It adds no route: the live-Session
// check on /v1/browser/commands is unchanged, and a model never reaches here
// except through its current turn's owned MCP tools.
func (s *Server) projectAssistantBrowser() *projectassistanthost.BrowserAccess {
	return &projectassistanthost.BrowserAccess{
		Status: func(ctx context.Context, owner string) (app.BrowserStatus, error) {
			policy, err := s.browserPolicy()
			if err != nil {
				return app.BrowserStatus{}, browserFailure(browserbridge.StateWriteFailed)
			}
			mode, status, _ := s.browsers().SelectedStatus(owner, policy)
			out := app.BrowserStatus{Mode: mode, Connected: status.Connected, Stopped: status.Stopped, Tabs: []app.BrowserTab{}}
			for _, tab := range status.Tabs {
				if tab.Conversation != owner {
					continue
				}
				out.Tabs = append(out.Tabs, app.BrowserTab{Tab: tab.Tab, Mode: tab.Mode, Current: tab.Current, Person: tab.Person})
			}
			return out, ctx.Err()
		},
		Command: func(ctx context.Context, owner string, command app.BrowserCommand) (app.BrowserResult, error) {
			policy, err := s.browserPolicy()
			if err != nil {
				return app.BrowserResult{}, browserFailure(browserbridge.StateWriteFailed)
			}
			value, err := s.browsers().DoOwned(ctx, owner, command.Op, command.Args, policy, command.Mode)
			if err != nil {
				return app.BrowserResult{}, browserFailure(err)
			}
			return app.BrowserResult{Value: value}, nil
		},
	}
}

// browserFailure keeps the engine's typed code for the Bot, as the CLI route does.
func browserFailure(err error) error {
	var arg *browserbridge.ArgError
	if errors.As(err, &arg) {
		return &app.BrowserFailure{Code: string(arg.Code), Message: arg.Field + ": " + arg.Message}
	}
	if refusal, ok := browserbridge.IsRefusal(err); ok {
		return &app.BrowserFailure{Code: string(refusal)}
	}
	var page *browserbridge.ExtensionError
	if errors.As(err, &page) {
		return &app.BrowserFailure{Code: "browser_page_error", Message: page.Message}
	}
	return &app.BrowserFailure{Code: "browser_internal"}
}
