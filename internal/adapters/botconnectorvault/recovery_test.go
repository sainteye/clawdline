package botconnectorvault

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/botprofilevault"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A restart during a credential write or a held execution leaves the
// credential unknown. Entering it again is the explicit way out: the new
// revision revokes every earlier capture, and the connector works again
// without another restart. A read that is interrupted is not cached.
func TestRestartUnknownConnectorCredentialRecoversWithAReplacement(t *testing.T) {
	ctx := context.Background()
	for _, restart := range []string{"accepted write", "held execution", "uncertain removal"} {
		f := newFixture(t)
		f.store(t, []byte("SYNTHETIC_FIRST_VALUE"))
		var oldRevision int64
		switch restart {
		case "accepted write":
			if _, err := f.db.Exec(`UPDATE project_assistant_connector_credentials SET state='accepted'`); err != nil {
				t.Fatal(err)
			}
		default:
			value, revision, _, _, err := f.r.Acquire(ctx, f.scope)
			if err != nil {
				t.Fatal(err)
			}
			botprofilevault.Wipe(value)
			oldRevision = revision
		}
		reopened, err := New(ctx, f.db, f.state, "synthetic-account", "machine")
		if err != nil {
			t.Fatal(err)
		}
		if status, err := reopened.Status(ctx, f.scope); err != nil || status.State != "unknown" {
			t.Fatal(restart, "restart did not mark the credential unknown", status, err)
		}
		if restart == "uncertain removal" {
			if removed, err := reopened.Apply(ctx, f.scope, f.action("remove", ""), nil, false, allow); err != nil || removed.State != "cleanup_unknown" {
				t.Fatal(restart, removed, err)
			}
		}
		if _, _, err = reopened.Get(ctx, f.scope); pa.Code(err) != pa.OutcomeUnknown {
			t.Fatal(restart, "an unknown credential was lent", err)
		}
		f.r = reopened
		f.store(t, []byte("SYNTHETIC_REPLACEMENT_VALUE"))
		got, revision, err := reopened.Get(ctx, f.scope)
		if err != nil || !bytes.Equal(got, []byte("SYNTHETIC_REPLACEMENT_VALUE")) {
			t.Fatal(restart, "replacement did not recover the credential", err)
		}
		botprofilevault.Wipe(got)
		if oldRevision != 0 {
			if err = reopened.Check(ctx, f.scope, oldRevision); pa.Code(err) != pa.Revoked {
				t.Fatal(restart, "an earlier capture survived the replacement", err)
			}
			if used, err := reopened.HoldUsage(ctx); err != nil || used != 1 {
				t.Fatal(restart, "the uncertain hold was forgotten", used, err)
			}
		}
		if err = reopened.Check(ctx, f.scope, revision); err != nil {
			t.Fatal(restart, err)
		}
	}
}

func TestInterruptedConnectorReadIsNotCached(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.store(t, []byte("SYNTHETIC_VALUE"))
	vault := filepath.Join(f.r.root, "connector-key-vault-v1")
	if err := os.Chmod(vault, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(vault, 0700) })
	if _, _, err := f.r.Get(ctx, f.scope); pa.Code(err) != pa.AuthProbeUnknown {
		t.Fatal("interrupted read", err)
	}
	if status, err := f.r.Status(ctx, f.scope); err != nil || status.State != "ready" {
		t.Fatal("an interrupted read changed the stored state", status, err)
	}
	if err := os.Chmod(vault, 0700); err != nil {
		t.Fatal(err)
	}
	got, _, err := f.r.Get(ctx, f.scope)
	if err != nil || !bytes.Equal(got, []byte("SYNTHETIC_VALUE")) {
		t.Fatal("the credential stayed unreadable after the fault cleared", err)
	}
	botprofilevault.Wipe(got)
}
