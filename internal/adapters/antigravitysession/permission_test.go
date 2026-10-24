//go:build darwin || linux

package antigravitysession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func waitingPermission(t *testing.T, h *Host, previous string) session.Session {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		inv, err := h.Inventory(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range inv.Sessions {
			if s.Menu != nil && s.Menu.Revision != previous && s.State == session.StateWaiting {
				return s
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("permission was not exposed while native peer waited")
	return session.Session{}
}

func TestOwnedPermissionsExposeAndSelectExactNativeOffers(t *testing.T) {
	t.Setenv("CLW_PERMISSION_PEER", "yes")
	h, row, dir := fixture(t)
	ctx := context.Background()
	if err := h.Send(ctx, row, "synthetic permission input"); err != nil {
		t.Fatal(err)
	}
	first := waitingPermission(t, h, "")
	if first.Line == "" {
		t.Fatal("native permission lacks the line required by waiting buttons")
	}
	expect := session.MenuFingerprint(*first.Menu)
	if first.Menu.Question != "Run synthetic command?" || first.Menu.Options[1].Label != "Reject once" {
		t.Fatal(first.Menu)
	}
	if err := h.SelectProtocol(ctx, row, 9, expect, nil); err == nil {
		t.Fatal("unknown choice accepted")
	}
	wrong := row
	wrong.ConversationID = "other"
	if err := h.SelectProtocol(ctx, wrong, 1, expect, nil); err == nil {
		t.Fatal("wrong session accepted")
	}
	denied := errors.New("synthetic revocation")
	if err := h.SelectProtocol(ctx, row, 1, expect, func() error { return denied }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "permissions")); !os.IsNotExist(err) {
		t.Fatal("refusal wrote a native response")
	}
	if err := h.SelectProtocol(ctx, row, 1, expect, nil); err != nil {
		t.Fatal(err)
	}
	second := waitingPermission(t, h, first.Menu.Revision)
	if session.MenuFingerprint(*second.Menu) == expect {
		t.Fatal("identical later offer reused fingerprint")
	}
	if err := h.SelectProtocol(ctx, row, 1, expect, nil); err == nil {
		t.Fatal("duplicate/stale choice accepted")
	}
	if err := h.SelectProtocol(ctx, row, 2, session.MenuFingerprint(*second.Menu), nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	var raw []byte
	for time.Now().Before(deadline) {
		raw, _ = os.ReadFile(filepath.Join(dir, "permissions"))
		if strings.Count(string(raw), "\n") == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("responses=%s", raw)
	}
	for i, want := range []string{"allow-exact", "reject-exact"} {
		var response struct {
			ID     string `json:"id"`
			Result struct {
				Outcome struct{ Outcome, OptionID string }
			}
		}
		if json.Unmarshal([]byte(lines[i]), &response) != nil || response.ID != "native-permission" || response.Result.Outcome.Outcome != "selected" || response.Result.Outcome.OptionID != want {
			t.Fatalf("native response=%s", lines[i])
		}
	}
	if err := h.SelectProtocol(ctx, row, 2, session.MenuFingerprint(*second.Menu), nil); err == nil {
		t.Fatal("consumed offer accepted")
	}
}

func TestOwnedPermissionCloseClearsWaitingMenu(t *testing.T) {
	t.Setenv("CLW_PERMISSION_PEER", "yes")
	h, row, _ := fixture(t)
	if err := h.Send(context.Background(), row, "synthetic input"); err != nil {
		t.Fatal(err)
	}
	waiting := waitingPermission(t, h, "")
	if err := h.Close(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	inv, err := h.Inventory(context.Background())
	if err != nil || len(inv.Sessions) != 0 {
		t.Fatal(inv, err)
	}
	if err := h.SelectProtocol(context.Background(), row, 1, session.MenuFingerprint(*waiting.Menu), nil); err == nil {
		t.Fatal("closed permission accepted")
	}
}
