package projectassistanthost

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// BrowserTarget is a person's view of one Bot conversation's browser tabs.
// Owner is derived here, from the current stored Bot and memory frontier; it
// is a key for filtering and admission, not a credential a client may send.
type BrowserTarget struct {
	Key            pa.BotKey
	ConversationID string
	Owner          string
}

// BrowserTarget resolves the tab owner of the Bot's current conversation for
// the person behind ctx. The machine is this Host's; the Bot and the
// conversation the person expects must both be current. Read-only access
// may look; write also requires that the person may send to this Bot.
//
// The person's authority comes from the same Authority callback the Bot
// routes use (device gate, Cloud viewer grant, pairing), checked as the
// Service checks a read or a send, and the feature must be interactive:
// a disabled or hidden feature shows no browser. It reads one Bot row and
// its memory frontier, never a conversation export, and holds no lock while
// the caller then talks to the browser.
//
// A Bot hidden from the list stays reachable here, as its chat does
// (pa.Bot.Hidden), so its person can still take over or close its tabs.
func (h *Host) BrowserTarget(ctx context.Context, botID, conversation string, write bool) (BrowserTarget, error) {
	if err := ctx.Err(); err != nil {
		return BrowserTarget{}, err
	}
	if botID == "" || conversation == "" {
		return BrowserTarget{}, failure(pa.Invalid, "browser_target")
	}
	key := pa.BotKey{MachineID: h.o.MachineID, BotID: botID}
	a, err := h.human(ctx, key, write)
	if err != nil {
		return BrowserTarget{}, err
	}
	if a.PrincipalID == "" || a.ReaderID == "" || a.AuthorizationRevision == "" || a.Source.MachineID != key.MachineID {
		return BrowserTarget{}, failure(pa.ScopeDenied, "authority")
	}
	if err = pa.CheckSource(a.Source, write); err != nil {
		return BrowserTarget{}, err
	}
	if a.Feature.Access() != pa.Interactive {
		return BrowserTarget{}, failure(pa.Disabled, "feature")
	}
	if write {
		if err = a.Feature.CheckWrite(); err != nil {
			return BrowserTarget{}, err
		}
	}
	h.mu.Lock()
	st := h.store
	h.mu.Unlock()
	if st == nil {
		return BrowserTarget{}, failure(pa.Disabled, "feature")
	}
	bot, err := st.Bot(ctx, key)
	if err != nil {
		return BrowserTarget{}, err
	}
	if bot.Key != key || bot.Project.MachineID != key.MachineID {
		return BrowserTarget{}, failure(pa.ScopeDenied, "browser_target")
	}
	if bot.ConversationID != conversation {
		return BrowserTarget{}, failure(pa.StaleVersion, "conversation")
	}
	memory, err := st.Current(ctx, key)
	if err != nil {
		return BrowserTarget{}, err
	}
	if memory.ForgottenThrough < 0 {
		return BrowserTarget{}, failure(pa.Invalid, "memory")
	}
	owner := BrowserOwner(pa.Fence{Key: bot.Key, Project: bot.Project, ConversationID: bot.ConversationID}, memory.ForgottenThrough)
	return BrowserTarget{Key: key, ConversationID: bot.ConversationID, Owner: owner}, nil
}

// BrowserOwnerBot is the Bot whose current conversation owns a browser tab
// owner, for the daemon's own new-site notice. It grants nothing: the person
// who taps the notice still opens the Bot through its routes and authority.
type BrowserOwnerBot struct {
	Key            pa.BotKey
	ConversationID string
	Name           string
}

// BrowserOwnerBot finds the stored Bot on this machine whose current
// conversation and forget epoch derive owner. false means no current Bot
// does: the Bot was deleted, forgotten or moved to another conversation after
// its command, and the notice must not guess one. It reads each Bot row and
// its memory frontier once, page by page, and holds no lock while reading.
func (h *Host) BrowserOwnerBot(ctx context.Context, owner string) (BrowserOwnerBot, bool, error) {
	if len(owner) <= len(BrowserOwnerPrefix) || owner[:len(BrowserOwnerPrefix)] != BrowserOwnerPrefix {
		return BrowserOwnerBot{}, false, nil
	}
	h.mu.Lock()
	st := h.store
	h.mu.Unlock()
	if st == nil {
		return BrowserOwnerBot{}, false, nil
	}
	query := pa.ListBots{MachineID: h.o.MachineID, PageSize: pa.MaxPageRows}
	for {
		page, err := st.List(ctx, query)
		if err != nil {
			return BrowserOwnerBot{}, false, err
		}
		for _, item := range page.Items {
			bot := item.Bot
			if bot.Key.MachineID != h.o.MachineID || bot.Project.MachineID != h.o.MachineID || bot.ConversationID == "" {
				continue
			}
			memory, err := st.Current(ctx, bot.Key)
			if err != nil {
				return BrowserOwnerBot{}, false, err
			}
			if memory.ForgottenThrough < 0 {
				continue
			}
			if BrowserOwner(pa.Fence{Key: bot.Key, Project: bot.Project, ConversationID: bot.ConversationID}, memory.ForgottenThrough) == owner {
				return BrowserOwnerBot{Key: bot.Key, ConversationID: bot.ConversationID, Name: bot.Name}, true, nil
			}
		}
		if page.NextCursor == "" {
			return BrowserOwnerBot{}, false, nil
		}
		query.Cursor = page.NextCursor
	}
}
