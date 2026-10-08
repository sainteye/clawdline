package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/devices"
	"github.com/sainteye/clawdline/internal/adapters/projects"
)

func TestProjectAddUsesDaemonAnswerAndResolvesRelativePathLocally(t *testing.T) {
	state := t.TempDir()
	project := filepath.Join(t.TempDir(), "new-app")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	var err error
	state, err = filepath.EvalSymlinks(state)
	if err != nil {
		t.Fatal(err)
	}
	project, err = filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, devices.MachineTokenFile), []byte("test-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAWDLINE_NEXT_DIR", state)
	seen := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/places" || r.Header.Get("X-Clawdline-Orchestrator") != "test-token" {
			t.Errorf("request = %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var body struct {
			Paths []string `json:"paths"`
		}
		want := project
		if seen == 1 {
			want = state
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Paths) != 1 || body.Paths[0] != want {
			t.Errorf("paths = %#v, %v", body.Paths, err)
		}
		seen++
		_ = json.NewEncoder(w).Encode(struct {
			Registered bool                       `json:"registered"`
			Places     []projects.RegisteredPlace `json:"places"`
		}{true, []projects.RegisteredPlace{{Path: project, AddedAt: 1}}})
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strconv.Atoi(port); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAWDLINE_NEXT_PORT", port)
	relative, err := filepath.Rel(state, project)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(state); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	rows, err := projectRegistryRequest(http.MethodPost, []string{relative})
	if err != nil || seen != 1 || len(rows) != 1 || rows[0].Path != project {
		t.Fatalf("daemon registration = %#v, seen %v, %v", rows, seen, err)
	}
	if _, err := projectRegistryRequest(http.MethodPost, nil); err != nil || seen != 2 {
		t.Fatalf("current directory registration: seen %d, %v", seen, err)
	}
	if _, err := os.Stat(filepath.Join(state, projects.PlaceRegistryFile)); !os.IsNotExist(err) {
		t.Fatalf("CLI wrote a second registry: %v", err)
	}
}
