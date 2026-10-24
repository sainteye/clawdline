package cloud

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// The same authenticated browser is checked through the Session bridge and
// the terminal gates. Expected outcomes are explicit: comparing the two alone
// would also pass if both accidentally refused every operator.
func TestSessionAndTerminalShareViewerAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		change                        func(*testing.T, *unverifiedFixture)
		readCode, sendCode            string
		terminalRead, terminalOperate terminal.RefusalCode
	}{
		{name: "operator"},
		{name: "read-only browser", change: func(t *testing.T, f *unverifiedFixture) {
			if err := f.l.pinned.SetScope("viewer", adaptercloud.ScopeReadOnly); err != nil {
				t.Fatal(err)
			}
		}, sendCode: "viewer_read_only", terminalOperate: terminal.CodeViewerReadOnly},
		{name: "local revocation", change: func(t *testing.T, f *unverifiedFixture) {
			if _, err := f.l.pinned.Revoke("viewer", f.now); err != nil {
				t.Fatal(err)
			}
		}, readCode: "unknown_sender", sendCode: "unknown_sender", terminalRead: terminal.CodeForbidden, terminalOperate: terminal.CodeForbidden},
		{name: "account revocation", change: func(t *testing.T, f *unverifiedFixture) {
			revoked := "2026-10-24T00:00:00Z"
			f.set(func(f *unverifiedFixture) { f.row.RevokedAt = &revoked })
		}, readCode: "unknown_sender", sendCode: "unknown_sender", terminalRead: terminal.CodeForbidden, terminalOperate: terminal.CodeForbidden},
		{name: "substituted account key", change: func(t *testing.T, f *unverifiedFixture) {
			key, _, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			f.set(func(f *unverifiedFixture) { f.row.PublicKey = base64.StdEncoding.EncodeToString(key) })
		}, readCode: "unknown_sender", sendCode: "unknown_sender", terminalRead: terminal.CodeForbidden, terminalOperate: terminal.CodeForbidden},
		{name: "temporarily unreadable roster", change: func(t *testing.T, f *unverifiedFixture) {
			f.set(func(f *unverifiedFixture) { f.fail = true })
		}, readCode: "command_roster_unreadable", sendCode: "command_roster_unreadable", terminalRead: terminal.CodeBusy, terminalOperate: terminal.CodeBusy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUnverifiedFixture(t)
			f.set(func(f *unverifiedFixture) { f.row.Caps = []string{"read_transcript", "send_prompt"} })
			rawKey, err := base64.StdEncoding.DecodeString(f.row.PublicKey)
			if err != nil {
				t.Fatal(err)
			}
			public := ed25519.PublicKey(rawKey)
			local := &fixedRouter{body: `{"ok":true,"sessions":[]}`}
			bridge := cloudops.Bridge{MachineID: "machine", Router: local, AllowCommands: f.l.allowCommands,
				Authority: f.l.authority, TranscriptAuthority: f.l.transcriptAuthority}
			read := func() cloudops.Answer {
				return bridge.Handle(context.Background(), cloudops.Command{Channel: "r/machine", Class: cloudops.ClassCtl,
					Sender: "viewer", VerifiedKey: public, Sequence: 1,
					Plaintext: []byte(`{"type":"sessions.list","machine_id":"machine","session":"__clawdline_machine__","request":"authority-list"}`)})
			}
			send := func() cloudops.Answer {
				return bridge.Handle(context.Background(), cloudops.Command{Channel: "ctl/machine", Class: cloudops.ClassCtl,
					Sender: "viewer", VerifiedKey: public, Sequence: 2,
					Plaintext: []byte(`{"type":"send","session":"%19","text":"synthetic","request":"authority-send","images":[]}`)})
			}
			// Warm a real roster and prove the initial grant before withdrawing it.
			if a := read(); !a.OK() {
				t.Fatalf("initial Session read: %s %s", a.Code, a.Payload)
			}
			if a := send(); !a.OK() {
				t.Fatalf("initial Session send: %s %s", a.Code, a.Payload)
			}
			if err := f.l.TerminalViewerAccess("viewer"); err != nil {
				t.Fatal(err)
			}
			if err := f.l.TerminalViewerOperate("viewer"); err != nil {
				t.Fatal(err)
			}
			if tc.change != nil {
				tc.change(t, f)
			}
			f.advance(CloudTerminalRosterRefreshLimit * time.Second)
			readsBefore := local.calls
			a := read()
			if a.Code != tc.readCode || a.OK() != (tc.readCode == "") {
				t.Fatalf("Session read: status=%d code=%s want=%s payload=%s", a.Status, a.Code, tc.readCode, a.Payload)
			}
			terminalRead, _ := terminal.CodeOf(f.l.TerminalViewerAccess("viewer"))
			if terminalRead != tc.terminalRead || a.OK() != (terminalRead == "") {
				t.Fatalf("read parity: Session=%s terminal=%s want=%s", a.Code, terminalRead, tc.terminalRead)
			}
			if tc.readCode != "" && local.calls != readsBefore {
				t.Fatal("refused Session read reached its local route")
			}
			callsBefore := local.calls
			a = send()
			if a.Code != tc.sendCode || a.OK() != (tc.sendCode == "") {
				t.Fatalf("Session send: status=%d code=%s want=%s payload=%s", a.Status, a.Code, tc.sendCode, a.Payload)
			}
			operate, _ := terminal.CodeOf(f.l.TerminalViewerOperate("viewer"))
			if operate != tc.terminalOperate || a.OK() != (operate == "") {
				t.Fatalf("operation parity: Session=%s terminal=%s want=%s", a.Code, operate, tc.terminalOperate)
			}
			if tc.sendCode != "" && local.calls != callsBefore {
				t.Fatal("refused Session send reached its local route")
			}
		})
	}
}
