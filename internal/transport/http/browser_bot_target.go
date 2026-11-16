package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/projectassistanthost"
	"github.com/sainteye/clawdline/internal/app/browserbridge"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A person's browser request may name a Bot instead of a terminal Session:
// `bot_id` together with the Bot's current `conversation`, on the same
// status, activity, audit, screenshot, control, person, open and mute routes
// (docs/browser.md, "A Bot's tabs in the person's view"). The machine is this
// daemon's. The daemon resolves the Bot through the Host with the person's
// own authority and derives the tab owner itself; a client never supplies an
// owner. This is the person's view, not an agent privilege: a machine token
// cannot name a Bot, and /v1/browser/commands still needs a live Session.
//
// A request that names no Bot cannot reach a Bot's tabs, nor name a Bot's
// owner key as its conversation.

// browserBotTarget is the optional Bot a person's request names.
type browserBotTarget struct {
	botID        string
	conversation string
}

func (t browserBotTarget) named() bool { return t.botID != "" }

// browserTargetFrom validates the pair. A Bot without its conversation, or a
// Session conversation spelled like a Bot owner, is a bad request, not a
// silently wider read.
func browserTargetFrom(botID, conversation string) (browserBotTarget, bool) {
	if botID == "" {
		return browserBotTarget{conversation: conversation}, !strings.HasPrefix(conversation, browserbridge.BotOwnerPrefix)
	}
	if conversation == "" || len(conversation) > 128 || !paHTTPSafeBotID(botID) {
		return browserBotTarget{}, false
	}
	return browserBotTarget{botID: botID, conversation: conversation}, true
}

func (s *Server) browserBotResolve(ctx context.Context, botID, conversation string, write bool) (projectassistanthost.BrowserTarget, error) {
	if a, ok := ctx.Value(accessKey{}).(access); !ok || a.machine {
		return projectassistanthost.BrowserTarget{}, paHTTPFailure(pa.ScopeDenied, "browser_target_person")
	}
	p := &s.projectAssistants
	p.mu.RLock()
	h, stopped := p.host, p.stopped
	p.mu.RUnlock()
	if h == nil || stopped {
		return projectassistanthost.BrowserTarget{}, paHTTPFailure(pa.Disabled, "feature")
	}
	return h.BrowserTarget(ctx, botID, conversation, write)
}

// browserPersonTarget is the engine scope of a person's tab request: the Bot's
// own tabs with its authority checked again at admission, or every tab that
// is not a Bot's. It returns the resolved owner for a Bot.
func (s *Server) browserPersonTarget(ctx context.Context, t browserBotTarget, write bool) (browserbridge.PersonTarget, string, error) {
	if !t.named() {
		return browserbridge.PersonTarget{Exclude: browserbridge.BotOwnerPrefix}, "", nil
	}
	target, err := s.browserBotResolve(ctx, t.botID, t.conversation, write)
	if err != nil {
		return browserbridge.PersonTarget{}, "", err
	}
	owner := target.Owner
	return browserbridge.PersonTarget{Owner: owner, Admit: func() error {
		return s.browserBotStill(ctx, t, write, owner)
	}}, owner, nil
}

// browserBotStill re-resolves the Bot and requires the same owner: a forget,
// a revoked device or grant, a disabled feature or a changed conversation
// between the first check and this one refuses.
func (s *Server) browserBotStill(ctx context.Context, t browserBotTarget, write bool, owner string) error {
	again, err := s.browserBotResolve(ctx, t.botID, t.conversation, write)
	if err != nil {
		return err
	}
	if again.Owner != owner {
		return paHTTPFailure(pa.StaleVersion, "browser_owner")
	}
	return nil
}

// writeBrowserTargetError writes a Bot target's typed refusal in the Bot
// routes' own error shape, and anything else as the browser routes do.
func writeBrowserTargetError(w http.ResponseWriter, err error) {
	var typed *pa.Failure
	if errors.As(err, &typed) {
		paWriteHTTPFailure(w, err)
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		writeBrowserError(w, browserbridge.TimedOut)
		return
	}
	writeBrowserError(w, err)
}

// withoutBotTabs drops Bot-owned tabs from a view that named no Bot.
func withoutBotTabs(st browserbridge.Status) browserbridge.Status {
	kept := st.Tabs[:0:0]
	for _, tab := range st.Tabs {
		if !strings.HasPrefix(tab.Conversation, browserbridge.BotOwnerPrefix) {
			kept = append(kept, tab)
		}
	}
	st.Tabs = kept
	persons := []int{}
	for _, tab := range st.PersonTabs {
		for _, kept := range st.Tabs {
			if kept.Tab == tab {
				persons = append(persons, tab)
				break
			}
		}
	}
	st.PersonTabs = persons
	muted := []string{}
	for _, conversation := range st.Muted {
		if !strings.HasPrefix(conversation, browserbridge.BotOwnerPrefix) {
			muted = append(muted, conversation)
		}
	}
	st.Muted = muted
	return st
}
