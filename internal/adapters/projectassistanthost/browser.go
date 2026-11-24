package projectassistanthost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// BrowserAccess is the composition's private path to the existing browser
// engine. Host calls it only from a bound tool of the current turn and passes
// an owner it derived itself; there is no route, token or BotID entry point.
type BrowserAccess struct {
	Status  func(ctx context.Context, owner string) (app.BrowserStatus, error)
	Command func(ctx context.Context, owner string, command app.BrowserCommand) (app.BrowserResult, error)
}

// BrowserOwnerPrefix marks owners that are Bots, never terminal Sessions.
const BrowserOwnerPrefix = "bot-"

// BrowserOwner is the stable tab owner for one Bot conversation: its machine,
// Bot, complete Project scope, conversation and forget epoch. Generation,
// binding, native session and turn are deliberately absent, so a resumed or
// restarted process keeps its own tabs, while a forget, another conversation
// or another Project never reaches them. It is an opaque key, not a credential.
func BrowserOwner(f pa.Fence, forgottenThrough int64) string {
	h := sha256.New()
	for _, part := range []string{
		"clawdline-bot-browser-owner-v1",
		f.Key.MachineID, f.Key.BotID,
		f.Project.MachineID, f.Project.ProjectID, f.Project.CanonicalID, string(f.Project.Kind),
		f.ConversationID, strconv.FormatInt(forgottenThrough, 10),
	} {
		h.Write([]byte(strconv.Itoa(len(part))))
		h.Write([]byte{0})
		h.Write([]byte(part))
	}
	return BrowserOwnerPrefix + hex.EncodeToString(h.Sum(nil))
}

// browserScope admits the person's own main chat and a Bot answering another
// Bot's message_bot on this machine. The owner is derived from the turn's own
// conversation, so a peer request's private thread worker reaches only the
// tabs it opened. A routine slot and a request from another machine are
// refused rather than borrowing the main lane's tabs.
func browserScope(binding pa.ExecutionBinding, turn pa.Turn) error {
	if binding.SessionSlot != "" {
		return failure(pa.ScopeDenied, "browser_session_slot")
	}
	if turn.Room != nil {
		return failure(pa.ScopeDenied, "browser_room")
	}
	switch turn.Origin {
	case "", pa.UserOrigin, pa.HostNoticeOrigin, pa.BotRequestOrigin:
		return nil
	}
	return failure(pa.ScopeDenied, "browser_turn_origin")
}

func (w NativeWork) BrowserStatus(ctx context.Context) (app.BrowserStatus, error) {
	if w.browserStatus == nil {
		return app.BrowserStatus{}, failure(pa.Disabled, "browser_tools")
	}
	return w.browserStatus(ctx)
}

func (w NativeWork) BrowserCommand(ctx context.Context, command app.BrowserCommand) (app.BrowserResult, error) {
	if w.browserCommand == nil {
		return app.BrowserResult{}, failure(pa.Disabled, "browser_tools")
	}
	return w.browserCommand(ctx, command)
}
