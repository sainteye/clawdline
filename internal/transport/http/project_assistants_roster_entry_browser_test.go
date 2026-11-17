package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
)

func TestProjectAssistantHiddenRosterEntryBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_ROSTER_ENTRY_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_ROSTER_ENTRY_BROWSER_DIR for the isolated Cloud roster entry proof")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f := newPAServerFixture(t, true)
	f.s.projectAssistants.provider = func() (app.Provider, error) { return &alignmentProvider{}, nil }
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	source := httptest.NewServer(f.h)
	defer source.Close()
	// The outer encrypted carrier is simulated. The closed Cloud decoder,
	// authenticated HTTP gate, normal Host and SQLite are real.
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bridge := cloudops.Bridge{MachineID: f.key.MachineID, Router: groupUIRouter{origin: source.URL, token: f.local}, AllowCommands: func() bool { return true }}
		answer := bridge.Handle(r.Context(), cloudops.Command{Channel: "ctl/" + f.key.MachineID, Class: cloudops.ClassCtl, Sender: "synthetic-viewer", Sequence: 1, Plaintext: raw})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.Status)
		_, _ = w.Write(answer.Payload)
	}))
	defer cloud.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "../../../web/console/tools/project-assistants-roster-entry.e2e.mjs")
	command.Env = append(os.Environ(), "BOTS_CLOUD_ORIGIN="+cloud.URL, "BOTS_MACHINE="+f.key.MachineID, "BOTS_ARTIFACTS="+dir)
	output, err := command.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), output, 0600)
	if err != nil {
		t.Fatalf("hidden roster entry: %v\n%s", err, output)
	}
	var result struct {
		Cases int `json:"cases"`
	}
	if err := json.Unmarshal(output, &result); err != nil || result.Cases != 6 {
		t.Fatalf("browser proof did not complete all six cases: %v\n%s", err, output)
	}
	t.Log(string(output))
}
