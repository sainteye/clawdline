package cloudops

import (
	"context"
	"crypto/ed25519"
	"testing"
)

func invitedAuthority(invited bool) func(context.Context, string, ed25519.PublicKey, bool) Authority {
	return func(context.Context, string, ed25519.PublicKey, bool) Authority {
		return Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true,
			WriteGateAllows: true, Invited: invited}
	}
}

// An invited browser may act like any operate viewer, except through a word
// that runs as this machine itself: it must not invite more browsers or carry
// a pairing to another machine.
func TestAnInvitedBrowserIsRefusedEveryMachineOnlyWord(t *testing.T) {
	if !machineOnlyWords["pair-agent-start"] || !machineOnlyWords["pair-agent-status"] {
		t.Fatalf("the table's local_exemption words were not read: %v", machineOnlyWords)
	}
	body := map[string]any{"type": "pair-agent-start", "session": MachineReplySession, "request": "req-pair-agent",
		"offer": "opaque", "machine_id": "mac_target", "machine_name": "Target"}
	r := &router{}
	b := open(r)
	b.Authority = invitedAuthority(true)
	a := b.Handle(context.Background(), request(t, ClassCtl, body))
	if a.Code != "invited_browser_machine_word" {
		t.Fatalf("an invited browser's pair-agent-start answered %q", a.Code)
	}
	if len(r.seen) != 0 {
		t.Fatalf("the refused word still reached a local route: %+v", r.seen)
	}

	r = &router{}
	b = open(r)
	b.Authority = invitedAuthority(false)
	if a := b.Handle(context.Background(), request(t, ClassCtl, body)); a.Code != "" || len(r.seen) != 1 {
		t.Fatalf("an account viewer's pair-agent-start answered %q with %d routes", a.Code, len(r.seen))
	}
}

func TestAnInvitedBrowserStillUsesOrdinaryWords(t *testing.T) {
	r := &router{body: `{"documents":[]}`}
	b := open(r)
	b.Authority = invitedAuthority(true)
	a := b.Handle(context.Background(), request(t, ClassCtl, map[string]any{"type": "documents", "session": pane}))
	if a.Code != "" || len(r.seen) != 1 {
		t.Fatalf("an invited browser's documents read answered %q with %d routes", a.Code, len(r.seen))
	}
}
