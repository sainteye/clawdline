package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Actual Bots/room navigation over authenticated HTTP and SQLite. The only
// provider is the in-process scripted fixture; no native process or model runs.
func TestGroupMemberNavigationBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_GROUP_NAV_ARTIFACTS")
	if dir == "" {
		t.Skip("set CLAWDLINE_GROUP_NAV_ARTIFACTS and PLAYWRIGHT_MODULE for room/member navigation")
	}
	f := newGroupHTTPFixture(t)
	f.create(t)
	for i := 0; i < 6; i++ {
		status, body := f.request(t, http.MethodPost, "/v1/project-assistants/rooms/groups/room/messages", "person", contract.ProjectAssistantGroupSendRequest{Command: f.command(pa.SendOperation, fmt.Sprintf("navigation-seed-%d", i)), Text: fmt.Sprintf("Stored discussion line %d with enough context to exercise the reader's position.", i)})
		if status != http.StatusOK {
			t.Fatalf("seed room: %d %s", status, body)
		}
		f.rooms.Idle()
	}
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bridge := cloudops.Bridge{MachineID: "machine", Router: groupUIRouter{origin: f.server.URL, token: "person"}, AllowCommands: func() bool { return true }}
		answer := bridge.Handle(r.Context(), cloudops.Command{Channel: "ctl/machine", Class: cloudops.ClassCtl, Sender: "synthetic-navigation-viewer", Sequence: 1, Plaintext: data})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.Status)
		_, _ = w.Write(answer.Payload)
	}))
	defer cloud.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../../../web/console/tools/project-assistants-group-navigation.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+f.server.URL, "BOTS_CLOUD_ORIGIN="+cloud.URL, "BOTS_ARTIFACTS="+dir)
	out, err := cmd.CombinedOutput()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), out, 0600)
	if err != nil {
		t.Fatalf("room/member navigation: %v\n%s", err, out)
	}
	t.Log(string(out))
}
