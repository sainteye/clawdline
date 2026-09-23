package http

import (
	"encoding/json"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/icon"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCloudCopiesOnlyAnIconToAReceivingProject(t *testing.T) {
	s := iconCloudStandIn(t)
	var err error
	s.server.icons, err = icon.NewRegistryWithOverrides(s.server.cfg.Dir)
	if err != nil {
		t.Fatal(err)
	}
	project, ok := s.server.workV2Project(t.Context(), s.place)
	if !ok {
		t.Fatal("missing fixture place")
	}
	before := s.server.icons.For(project.Path)
	c := "#123456"
	copied := icon.Grid{Accent: c, Cells: [][]*string{{&c, nil}}}
	ask := func(seq uint64, id string, grid, expected icon.Grid) cloudops.Answer {
		a, _ := s.ask(t, seq, map[string]any{"type": "project-icon-copy", "session": cloudops.MachineReplySession,
			"request": "copy-request", "id": id, "item": map[string]any{"icon": grid, "expected": expected}})
		return a
	}
	if a := ask(1, s.place, copied, before); !a.OK() {
		t.Fatalf("copy: %d %s", a.Status, a.Payload)
	}
	if a := ask(2, s.place, copied, before); !a.OK() {
		t.Fatalf("retry: %d %s", a.Status, a.Payload)
	}
	reopened, err := icon.NewRegistryWithOverrides(s.server.cfg.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reopened.For(filepath.Join(project.Path, "src")), copied) {
		t.Fatal("copy not durable")
	}
	if a := ask(3, s.place, before, before); a.Status != 409 {
		t.Fatalf("stale overwrite: %d %s", a.Status, a.Payload)
	}
	if a := ask(4, "not-a-place", copied, before); a.Status != 404 {
		t.Fatalf("unknown target: %d", a.Status)
	}
	unsafe := "javascript:alert(1)"
	bad := icon.Grid{Accent: unsafe, Cells: copied.Cells}
	if a := ask(5, s.place, bad, copied); a.Status != 422 {
		t.Fatalf("unsafe grid: %d", a.Status)
	}
	s.bridge.AllowCommands = func() bool { return false }
	if a := ask(6, s.place, before, copied); a.OK() {
		t.Fatal("write gate bypassed")
	}
}

func TestIconRouteRequiresSendAndBoundsBody(t *testing.T) {
	s := iconCloudStandIn(t)
	path := "/v1/projects/" + s.place + "/icon"
	// The handler itself refuses an unauthenticated/read-only caller even if
	// reached without the outer gate.
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{}`))
	out := httptest.NewRecorder()
	s.server.projectIconRoute(out, req, s.place)
	if out.Code != 403 {
		t.Fatalf("unprivileged write: %d", out.Code)
	}
	local, _, err := s.server.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		body string
		want int
	}{
		{strings.Repeat(" ", icon.MaxIconRequestBytes) + `{}`, 413},
		{`{"icon":{},"expected":{},"path":"/elsewhere"}`, 400},
		{`{} {}`, 400},
	} {
		req := httptest.NewRequest(http.MethodPut, "http://127.0.0.1:7757"+path, strings.NewReader(test.body))
		req.Header.Set("Authorization", "Bearer "+local)
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		s.handler.ServeHTTP(out, req)
		if out.Code != test.want {
			t.Fatalf("body: got %d want %d: %s", out.Code, test.want, out.Body.String())
		}
	}
	// Places keep the existing wire shape after a copy.
	data, _ := json.Marshal(icon.Grid{})
	if strings.Contains(string(data), "Cells") {
		t.Fatal("exported Go field leaked onto wire")
	}
}

// Use the places fixture seam instead of provider history: provider history
// deliberately excludes /tmp, which otherwise skips these tests on Linux.
func iconCloudStandIn(t *testing.T) *standIn {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(home, "swift"))
	state := filepath.Join(home, "state")
	project := filepath.Join(home, "project")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	icons, err := icon.NewRegistryWithOverrides(state)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfg: config.Config{Dir: state, Port: 7757}, store: st, icons: icons,
		broker: &orchestrator.Broker{Store: st, Tasks: taskdir.New(state), Dir: state}}
	server.projectReaders().places.Fixture = []string{project}
	places := server.projectReaders().places.List(nil, 40)
	if len(places) != 1 {
		t.Fatalf("fixture places: %v", places)
	}
	handler := server.Handler()
	local, machine, err := server.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	return &standIn{server: server, handler: handler, place: places[0].ID, machine: machine,
		bridge: cloudops.Bridge{MachineID: "mac-01", Router: cloudtransport.Router{Handler: handler, Authorize: cloudtransport.LocalAuthorizer(local, machine)}, AllowCommands: func() bool { return true }}}
}
