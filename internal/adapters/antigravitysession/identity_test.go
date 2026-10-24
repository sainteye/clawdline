package antigravitysession

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func assertIdentity(t *testing.T, path, id string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]string
	if json.Unmarshal(raw, &record) != nil || len(record) != 2 || record["assistant"] != "antigravity" || record["conversation_id"] != id {
		t.Fatalf("identity %s", raw)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("not private: %v %v", info, err)
	}
}

func TestIdentityOwnedConversationsSurviveRestartWithoutPrompts(t *testing.T) {
	t.Setenv("CLW_IDENTITY_CHECK", "yes")
	h, a, opts := persistentFixture(t)
	b, err := h.Open(context.Background(), ports.OpenRequest{Cwd: opts.AccountDir})
	if err != nil {
		t.Fatal(err)
	}
	pa, pb := h.identityPath(a.ID), h.identityPath(b.ID)
	if pa == pb || a.ConversationID == b.ConversationID {
		t.Fatal("conversations share identity")
	}
	for _, row := range []session.Session{a, b} {
		assertIdentity(t, h.identityPath(row.ID), row.ConversationID)
	}
	if got := readFile(t, filepath.Join(opts.AccountDir, "prompts")); got != "" {
		t.Fatalf("open prompted: %q", got)
	}
	stopHost(t, h)
	// Restore repairs a missing companion at the same path before process start.
	if err := os.Remove(pa); err != nil {
		t.Fatal(err)
	}
	h = reopenHost(t, opts)
	for _, row := range []session.Session{a, b} {
		resumed, err := h.Resume(context.Background(), row.CWD, row.ConversationID)
		if err != nil {
			t.Fatal(err)
		}
		if resumed.ID != row.ID {
			t.Fatal("owned identity replaced")
		}
		assertIdentity(t, h.identityPath(row.ID), row.ConversationID)
		if got := readFile(t, filepath.Join(opts.AccountDir, "identity-start-"+row.ConversationID)); got != h.identityPath(row.ID) {
			t.Fatalf("restore started before identity publication: %q", got)
		}
	}
	if h.identityPath(a.ID) != pa || h.identityPath(b.ID) != pb {
		t.Fatal("paths changed")
	}
	if got := readFile(t, filepath.Join(opts.AccountDir, "prompts")); got != "" {
		t.Fatalf("restore prompted: %q", got)
	}
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, row := range []session.Session{a, b} {
		if err := h.Send(context.Background(), row, "synthetic prompt"); err != nil {
			t.Fatal(err)
		}
		idleHost(t, h, row)
		if got := readFile(t, filepath.Join(opts.AccountDir, "identity-read-"+row.ConversationID)); got != h.identityPath(row.ID) {
			t.Fatalf("peer read %q", got)
		}
	}
}

func TestIdentityPublicationFailureIsExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "identity.json")
	if err := publishIdentity(path, "native"); err == nil {
		t.Fatal("publication failure hidden")
	}
	if err := publishIdentity("", "native"); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), "identity.json")
	if err := publishIdentity(path, ""); err == nil {
		t.Fatal("empty identity published")
	}
	if err := publishIdentity(path, "first"); err != nil {
		t.Fatal(err)
	}
	if err := publishIdentity(path, "second"); err != nil {
		t.Fatal(err)
	}
	assertIdentity(t, path, "second")
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary residue: %v %v", entries, err)
	}
}
