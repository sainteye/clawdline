package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// TestBoardEditConflictBrowser draws the production Board edit dialog in a
// real browser against the real work route and store: the title is changed in
// the dialog and elsewhere, so the save is a field_conflict and the dialog
// offers its two choices. It leaves a 390x844 and a 1280x800 screenshot.
func TestBoardEditConflictBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_BOARD_CONFLICT_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_BOARD_CONFLICT_BROWSER_DIR for the real browser")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, _, v := workV2AssignmentServer(t, session.StateWorking)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.workV2Route(w, r.WithContext(context.WithValue(r.Context(), accessKey{}, access{verdict: auth.Verdict{
			Allowed: true, Device: "phone", Caps: auth.NewCaps(auth.Read, auth.Send),
		}})))
	}))
	defer server.Close()
	cmd := exec.Command("node", "../../../web/console/tools/board-edit-conflict.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOARD_ORIGIN="+server.URL, "BOARD_ITEM="+v.Item.ID, "BOARD_ARTIFACTS="+dir)
	log, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "board-edit-conflict.log"), log, 0600)
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, log)
	}
}
