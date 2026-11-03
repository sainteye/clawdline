package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/sessionfiles"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestOutputRoutesGateRegistrationAndBindFilesToConversation(t *testing.T) {
	f, _ := newGateFixture(t)
	cwd := t.TempDir()
	file := filepath.Join(cwd, "plan.md")
	os.WriteFile(file, []byte("session output"), 0600)
	inv := session.Inventory{Sessions: []session.Session{{ID: "%91", CWD: cwd, Assistant: session.AssistantCodex, ConversationID: "10000000-0000-4000-8000-000000000001"}, {ID: "%92", CWD: cwd, Assistant: session.AssistantCodex, ConversationID: "10000000-0000-4000-8000-000000000002"}}}
	s := &Server{cfg: config.Config{Dir: f.g.dir, Port: 7757}, inventory: app.Inventory{Process: todoProcesses{inv}}}
	handler := f.g.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, output, ok := outputPath(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		s.sessionOutputs(w, r, id, output)
	}))
	body, _ := json.Marshal(map[string]string{"path": file, "kind": "created"})
	for _, row := range []struct {
		headers map[string]string
		status  int
	}{{nil, 401}, {map[string]string{"Authorization": "Bearer " + f.read}, 403}, {map[string]string{"Authorization": "Bearer " + f.local}, 403}, {map[string]string{machineHeader: f.machine}, 200}} {
		headers := row.headers
		if headers != nil {
			headers["Content-Type"] = "application/json"
		}
		got := call{method: "POST", path: "/v1/sessions/%2591/outputs", body: string(body), headers: headers}.do(handler)
		if got.Code != row.status {
			t.Fatalf("registration gate: %d %s", got.Code, got.Body)
		}
	}
	read := func(path string) *httptest.ResponseRecorder {
		return call{method: "GET", path: path, headers: map[string]string{"Authorization": "Bearer " + f.read}}.do(handler)
	}
	list := read("/v1/sessions/%2591/outputs")
	var answer sessionfiles.Listing
	if list.Code != 200 || json.Unmarshal(list.Body.Bytes(), &answer) != nil || len(answer.Files) != 1 {
		t.Fatalf("list: %d %s", list.Code, list.Body)
	}
	id := answer.Files[0].ID
	if got := read("/v1/sessions/%2591/outputs/" + id + "?offset=0&revision=&size="); got.Code != 200 || !strings.Contains(got.Body.String(), "c2Vzc2lvbiBvdXRwdXQ=") {
		t.Fatalf("download: %d %s", got.Code, got.Body)
	}
	if got := read("/v1/sessions/%2592/outputs/" + id + "?offset=0&revision="); got.Code != 404 {
		t.Fatalf("cross-conversation file: %d %s", got.Code, got.Body)
	}
	if got := read("/v1/sessions/%2591/outputs/" + id + "?offset=0&revision=&path=/etc/passwd"); got.Code != 416 {
		t.Fatalf("arbitrary path: %d %s", got.Code, got.Body)
	}
	os.Remove(file)
	if got := read("/v1/sessions/%2591/outputs/" + id + "?offset=0&revision="); got.Code != 404 {
		t.Fatalf("deleted file: %d %s", got.Code, got.Body)
	}
}
