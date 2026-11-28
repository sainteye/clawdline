package projectassistanthost

import (
	"context"
	"testing"

	network "github.com/sainteye/clawdline/internal/app/botnetwork"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestComputeToolVisibilityMatchesCapabilitiesAndTurnOrigin(t *testing.T) {
	baseScope := func(origin pa.TurnOrigin, slot string, hasBrowser, hasNetwork, hasDispatch, bgThread bool) NativeWork {
		h := &Host{}
		if hasBrowser {
			h.o.Browser = &BrowserAccess{
				Status: func(context.Context, string) (app.BrowserStatus, error) { return app.BrowserStatus{}, nil },
				Command: func(context.Context, string, app.BrowserCommand) (app.BrowserResult, error) {
					return app.BrowserResult{}, nil
				},
			}
		}
		if hasNetwork {
			h.o.Network = &network.Service{}
			h.o.NetworkTransport = func(context.Context, string) (network.Transport, error) { return nil, nil }
		}
		var actor pa.AssistantExecutionActor
		if hasDispatch {
			actor.CapabilityID = "cap-1"
		}
		var owner pa.BotKey
		if bgThread {
			owner = pa.BotKey{MachineID: "m", BotID: "owner"}
		}
		s := &toolScope{
			h:         h,
			q:         app.OpenRequest{Binding: pa.ExecutionBinding{SessionSlot: slot}},
			turn:      pa.Turn{Origin: origin},
			taskOwner: owner,
			actor:     actor,
		}
		w := NativeWork{
			memoryCard: origin == pa.BotRequestOrigin || origin == pa.BotReplyOrigin,
			routines:   func(context.Context, app.RoutineRequest) (app.RoutineResult, error) { return app.RoutineResult{}, nil },
			listBots: func(context.Context, pa.BotDirectoryQuery) (pa.BotDirectoryPage, error) {
				return pa.BotDirectoryPage{}, nil
			},
			messageBot: func(context.Context, pa.BotMessageRequest) (pa.Exchange, error) { return pa.Exchange{}, nil },
			dispatch: func(context.Context, string, string, string, []string, string, string, string, bool) (pa.WorkLink, error) {
				return pa.WorkLink{}, nil
			},
		}
		s.restrictBackgroundThread(&w)
		s.computeToolVisibility(&w)
		return w
	}

	t.Run("direct user without browser or network", func(t *testing.T) {
		w := baseScope(pa.UserOrigin, "", false, false, true, false)
		for _, hidden := range []string{"browser_status", "browser_command", "network_bot", "bot_request_read"} {
			if w.ToolVisible(hidden) {
				t.Errorf("%q should be hidden", hidden)
			}
		}
		for _, visible := range []string{"react", "send_message", "send_file", "publish_artifact", "dispatch_work", "save_routine", "follow_up", "edit_memory"} {
			if !w.ToolVisible(visible) {
				t.Errorf("%q should be visible", visible)
			}
		}
	})

	t.Run("direct user with browser and network", func(t *testing.T) {
		w := baseScope(pa.UserOrigin, "", true, true, true, false)
		for _, visible := range []string{"browser_status", "browser_command", "network_bot", "react", "dispatch_work"} {
			if !w.ToolVisible(visible) {
				t.Errorf("%q should be visible", visible)
			}
		}
	})

	t.Run("same-machine peer request hides forbidden tools", func(t *testing.T) {
		w := baseScope(pa.BotRequestOrigin, "", true, true, true, false)
		for _, hidden := range []string{"react", "network_bot", "send_file", "publish_artifact", "save_routine", "follow_up", "save_progress", "read_webhook_guide"} {
			if w.ToolVisible(hidden) {
				t.Errorf("%q should be hidden in peer request", hidden)
			}
		}
		for _, visible := range []string{"list_routines", "browser_command", "dispatch_work", "ask_person", "notify_person", "edit_memory", "list_bots", "message_bot"} {
			if !w.ToolVisible(visible) {
				t.Errorf("%q should remain visible in same-machine peer request", visible)
			}
		}
	})

	t.Run("network request hides all restricted tools", func(t *testing.T) {
		w := baseScope(pa.NetworkRequestOrigin, "", true, true, true, false)
		for _, hidden := range []string{
			"react", "network_bot", "browser_status", "browser_command",
			"send_file", "publish_artifact", "list_artifacts",
			"list_routines", "save_routine", "follow_up",
			"list_bots", "message_bot", "dispatch_work", "work_status", "message_work",
			"edit_memory", "bot_request_read",
		} {
			if w.ToolVisible(hidden) {
				t.Errorf("%q should be hidden in network request", hidden)
			}
		}
		for _, visible := range []string{"read_memory", "search_memory", "send_message"} {
			if !w.ToolVisible(visible) {
				t.Errorf("%q should remain visible in network request", visible)
			}
		}
	})

	t.Run("isolated routine hides browser, react and follow_up", func(t *testing.T) {
		w := baseScope(pa.HostNoticeOrigin, "routine-1", true, false, true, false)
		for _, hidden := range []string{"browser_status", "browser_command", "react", "follow_up", "cancel_follow_up"} {
			if w.ToolVisible(hidden) {
				t.Errorf("%q should be hidden in isolated routine", hidden)
			}
		}
		for _, visible := range []string{"save_progress", "read_progress", "notify_person", "send_message", "dispatch_work"} {
			if !w.ToolVisible(visible) {
				t.Errorf("%q should remain visible in isolated routine", visible)
			}
		}
	})

	t.Run("background thread hides routines, team, bots and dispatch", func(t *testing.T) {
		w := baseScope(pa.UserOrigin, "", false, false, true, true)
		for _, hidden := range []string{"list_routines", "save_routine", "read_webhook_guide", "list_bots", "message_bot", "dispatch_work", "message_work"} {
			if w.ToolVisible(hidden) {
				t.Errorf("%q should be hidden in background thread", hidden)
			}
		}
		for _, visible := range []string{"send_message", "ask_person", "read_memory", "edit_memory"} {
			if !w.ToolVisible(visible) {
				t.Errorf("%q should remain visible in background thread", visible)
			}
		}
	})
}
