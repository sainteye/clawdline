package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/domain/icon"
)

func TestCloudCopiesOnlyAnIconToAReceivingProject(t *testing.T) {
	s := cloudStandIn(t)
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
	s := cloudStandIn(t)
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
		req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(test.body))
		req.Header.Set("Authorization", "Bearer "+local)
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
