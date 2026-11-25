package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
)

// TestGroupPicturesBrowser sends a picture-only room line from a 390px phone
// and a desktop browser through the real source client, the Cloud relay
// words and the production Bridge, and keeps a screenshot of each preview and
// sent line. No Cloud service, model or physical phone is contacted.
func TestGroupPicturesBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_GROUP_PICTURES_ARTIFACTS")
	if dir == "" || os.Getenv("PLAYWRIGHT_MODULE") == "" {
		t.Skip("set CLAWDLINE_GROUP_PICTURES_ARTIFACTS and PLAYWRIGHT_MODULE")
	}
	f := newGroupHTTPFixture(t)
	f.create(t)
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
		if err != nil {
			w.WriteHeader(400)
			return
		}
		bridge := cloudops.Bridge{MachineID: "machine", Router: groupUIRouter{origin: f.server.URL, token: "person"}, AllowCommands: func() bool { return true }}
		answer := bridge.Handle(r.Context(), cloudops.Command{Channel: "ctl/machine", Class: cloudops.ClassCtl, Sender: "synthetic-picture-phone", Sequence: 1, Plaintext: data})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.Status)
		_, _ = w.Write(answer.Payload)
	}))
	defer cloud.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../../../web/console/tools/project-assistants-group-pictures.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_CLOUD_ORIGIN="+cloud.URL, "BOTS_ARTIFACTS="+dir)
	output, err := cmd.CombinedOutput()
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), output, 0600)
	if err != nil {
		t.Fatalf("group pictures: %v\n%s", err, output)
	}
	t.Log(string(output))
}
