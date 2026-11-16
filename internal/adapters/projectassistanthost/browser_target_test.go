package projectassistanthost

import (
	"context"
	"testing"

	"github.com/sainteye/clawdline/internal/app/browserbridge"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The engine keeps a request that names no Bot away from Bot tabs by this
// prefix, so the two spellings must agree.
func TestBrowserOwnersBeginWithTheEnginesBotPrefix(t *testing.T) {
	if BrowserOwnerPrefix != browserbridge.BotOwnerPrefix {
		t.Fatalf("%q != %q", BrowserOwnerPrefix, browserbridge.BotOwnerPrefix)
	}
	owner := BrowserOwner(pa.Fence{Key: pa.BotKey{MachineID: "m", BotID: "b"}, ConversationID: "c"}, 0)
	if len(owner) > 128 || owner[:len(browserbridge.BotOwnerPrefix)] != browserbridge.BotOwnerPrefix {
		t.Fatalf("owner %q", owner)
	}
}

// A partial target, or one this person may not read, is refused before any
// stored Bot is read; a closed Host shows no browser.
func TestABrowserTargetRefusesBeforeReadingTheBot(t *testing.T) {
	calls := 0
	h := &Host{o: Options{MachineID: "m", Authority: func(context.Context, pa.BotKey, bool) (app.Authority, error) {
		calls++
		return app.Authority{}, failure(pa.Revoked, "gate_proof")
	}}}
	for _, c := range [][2]string{{"", "c"}, {"b", ""}} {
		if _, err := h.BrowserTarget(context.Background(), c[0], c[1], false); pa.Code(err) != pa.Invalid {
			t.Fatalf("%v: %v", c, err)
		}
	}
	if calls != 0 {
		t.Fatal("a partial target asked for authority")
	}
	if _, err := h.BrowserTarget(context.Background(), "b", "c", true); pa.Code(err) != pa.Revoked {
		t.Fatalf("revoked person: %v", err)
	}
	h.closed = true
	if _, err := h.BrowserTarget(context.Background(), "b", "c", false); pa.Code(err) != pa.Disabled {
		t.Fatalf("closed Host: %v", err)
	}
}
