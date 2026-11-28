package cloud

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
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

// Older viewers can possess this machine's key without having a local pin.
// Compare the two request paths at the authenticated envelope boundary.
func TestSessionsAndBotsShareLegacyViewerAuthorization(t *testing.T) {
	f := newUnverifiedFixture(t)
	f.l.identity.AccountID = "account"
	f.set(func(f *unverifiedFixture) { f.row.Caps = []string{"read_transcript", "send_prompt"} })
	if err := f.l.pinned.Forget(); err != nil {
		t.Fatal(err)
	}
	rawKey, err := base64.StdEncoding.DecodeString(f.row.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.PublicKey(rawKey)
	var grant adaptercloud.AssistantViewerGrant
	router := Router{ViewerGrant: f.l.viewerGrant, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/project-assistants" {
			var err error
			grant, err = CloudViewerGrant(r.Context(), "account")
			if err != nil {
				http.Error(w, "viewer denied", http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"contract_version":"project-assistants.v1","items":[],"sources":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"sessions":[]}`))
	})}
	b := cloudops.Bridge{MachineID: "machine", Router: router, AllowCommands: f.l.allowCommands,
		Authority: f.l.authority, TranscriptAuthority: f.l.transcriptAuthority}
	request := func(channel, body string, verified ed25519.PublicKey) cloudops.Answer {
		t.Helper()
		return b.Handle(t.Context(), cloudops.Command{Channel: channel + "/machine", Class: cloudops.ClassCtl,
			Sender: "viewer", VerifiedKey: verified, Sequence: 1, Plaintext: []byte(body)})
	}
	const sessions = `{"type":"sessions.list","machine_id":"machine","session":"__clawdline_machine__","request":"session-list"}`
	const bots = `{"type":"project-assistants.list","machine_id":"machine","session":"__clawdline_machine__","request":"bot-list","query":"","cursor":"","page_size":20}`
	check := func(verified ed25519.PublicKey, readable, writable bool) {
		t.Helper()
		if a := request("r", sessions, verified); a.OK() != readable {
			t.Fatalf("Session list status %d, code %s, want readable=%t", a.Status, a.Code, readable)
		}
		for _, channel := range []string{"r", "ctl"} {
			grant = adaptercloud.AssistantViewerGrant{}
			if a := request(channel, bots, verified); a.OK() != readable {
				t.Fatalf("Bot list on %s: status %d, code %s, want readable=%t: %s", channel, a.Status, a.Code, readable, a.Payload)
			}
			if readable && grant.Writable != writable {
				t.Fatalf("Bot grant on %s: writable=%t, want %t", channel, grant.Writable, writable)
			}
		}
	}
	check(key, true, true)
	owner, reader := adaptercloud.ProjectAssistantOwnerIdentity("account")
	if grant.PrincipalID != owner || grant.ReaderID != reader {
		t.Fatalf("legacy viewer changed the existing account reader: %+v", grant)
	}
	check(nil, false, false)
	f.set(func(f *unverifiedFixture) { f.row.Caps = []string{"send_prompt"} })
	f.advance(CloudTerminalRosterRefreshLimit * time.Second)
	check(key, false, false)
	f.set(func(f *unverifiedFixture) { f.row.Caps = []string{"read_transcript", "send_prompt"} })
	f.advance(CloudTerminalRosterRefreshLimit * time.Second)
	pin := adaptercloud.PinnedDevice{DeviceID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(key),
		Fingerprint: domaincloud.Fingerprint(key), Scope: adaptercloud.ScopeReadOnly}
	if err := f.l.pinned.Pin("account", pin); err != nil {
		t.Fatal(err)
	}
	check(key, true, false)
	if _, err := f.l.pinned.Revoke("viewer", f.now); err != nil {
		t.Fatal(err)
	}
	check(key, false, false)

	f.set(func(f *unverifiedFixture) { f.row.Kind = adaptercloud.RosterKindInvitedBrowser })
	f.advance(CloudTerminalRosterRefreshLimit * time.Second)
	pin.Invited = true
	if err := f.l.pinned.Pin("account", pin); err != nil {
		t.Fatal(err)
	}
	check(key, true, false)
	if !grant.Invited || grant.PrincipalID == owner || grant.ReaderID == reader {
		t.Fatalf("invited viewer inherited the account reader: %+v", grant)
	}
	if err := f.l.pinned.Forget(); err != nil {
		t.Fatal(err)
	}
	check(key, false, false)
}
