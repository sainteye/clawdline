//go:build darwin || linux

package antigravitysession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestIdentityAndRoleKeepSeparateOwnershipOnResume(t *testing.T) {
	t.Setenv("CLW_IDENTITY_CHECK", "yes")
	t.Setenv("CLAWDLINE_PROTOCOL_SESSION_FILE", "/parent/identity")
	t.Setenv("CLAWDLINE_SQUAD_CAPABILITY_FILE", "/parent/capability")
	h, plain, opts := persistentFixture(t)
	launch := contextLaunch("backend")
	role := openContext(t, h, opts, launch)
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, row := range []session.Session{plain, role} {
		if err := h.Send(context.Background(), row, "first input"); err != nil {
			t.Fatal(err)
		}
		idleHost(t, h, row)
		assertIdentity(t, h.identityPath(row.ID), row.ConversationID)
		want := ""
		if row.ID == role.ID {
			want = launch.SquadCapabilityPath
		}
		if got := readFile(t, filepath.Join(opts.AccountDir, row.ConversationID+"-capability")); got != want {
			t.Fatalf("wrong actor for %s: %q", row.ID, got)
		}
	}
	stopHost(t, h)
	h = reopenHost(t, opts)
	for _, row := range []session.Session{plain, role} {
		resumed, err := h.Resume(context.Background(), row.CWD, row.ConversationID)
		if err != nil {
			t.Fatal(err)
		}
		if resumed.Persona != row.Persona || resumed.SquadLaunchID != row.SquadLaunchID {
			t.Fatal("resume changed role")
		}
		if err := h.Send(context.Background(), resumed, "next input"); err != nil {
			t.Fatal(err)
		}
		idleHost(t, h, resumed)
		assertIdentity(t, h.identityPath(row.ID), row.ConversationID)
		want := ""
		if row.ID == role.ID {
			want = launch.SquadCapabilityPath
		}
		if got := readFile(t, filepath.Join(opts.AccountDir, row.ConversationID+"-capability")); got != want {
			t.Fatal("restored actor differs")
		}
	}
}
