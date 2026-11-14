package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/icon"
)

func TestProjectFolderPickerListsDirectoriesOnly(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"project", ".private"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "project"), filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/project-folders?path="+url.QueryEscape(dir), nil)
	out := httptest.NewRecorder()
	(&Server{}).projectFoldersRoute(out, req)
	var page struct {
		Path    string          `json:"path"`
		Folders []projectFolder `json:"folders"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil {
		t.Fatalf("folders: %d %s", out.Code, out.Body.String())
	}
	if page.Path != dir || len(page.Folders) != 1 || page.Folders[0].Name != "project" {
		t.Fatalf("folders = %#v", page)
	}

	bad := httptest.NewRecorder()
	(&Server{}).projectFoldersRoute(bad, httptest.NewRequest(http.MethodGet, "/v1/project-folders?path=relative", nil))
	if bad.Code != 400 {
		t.Fatalf("relative path = %d %s", bad.Code, bad.Body.String())
	}
}

func TestPairedSenderCanAddProjectWithoutRemovingItsSettings(t *testing.T) {
	home := placesHome(t)
	state := filepath.Join(home, ".config", "clawdline-next")
	dir := filepath.Join(home, "projects", "new")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: state}, icons: &icon.Registry{}}
	request := func(method string, allowed bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/v1/places", strings.NewReader(`{"paths":["`+dir+`"]}`))
		if allowed {
			req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{verdict: auth.Verdict{Allowed: true, Caps: auth.NewCaps(auth.Read, auth.Send)}}))
		}
		out := httptest.NewRecorder()
		s.placesRoute(out, req)
		return out
	}
	if got := request(http.MethodPost, false); got.Code != 403 {
		t.Fatalf("unpaired add = %d", got.Code)
	}
	if got := request(http.MethodPost, true); got.Code != 200 {
		t.Fatalf("paired add = %d %s", got.Code, got.Body.String())
	}
	if got := request(http.MethodDelete, true); got.Code != 200 {
		t.Fatalf("paired remove = %d %s", got.Code, got.Body.String())
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("removal touched directory: %v", err)
	}
}

func TestCloudProjectFolderChoiceAndAddUseTheSelectedMachine(t *testing.T) {
	s := projectCloudStandIn(t)
	dir := filepath.Join(filepath.Dir(s.server.cfg.Dir), "new-project")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	machine := cloudops.MachineReplySession
	read, _ := s.ask(t, 1, map[string]any{"type": "project-folders", "session": machine, "request": "folders", "path": filepath.Dir(dir)})
	if !read.OK() || !strings.Contains(string(read.Payload), "new-project") {
		t.Fatalf("folder read: %d %s", read.Status, read.Payload)
	}
	add := func(seq uint64) cloudops.Answer {
		answer, _ := s.ask(t, seq, map[string]any{"type": "project-add", "session": machine, "request": "add", "path": dir})
		return answer
	}
	s.bridge.AllowCommands = func() bool { return false }
	if result := add(2); result.OK() {
		t.Fatal("Cloud add passed the command gate while off")
	}
	s.bridge.AllowCommands = func() bool { return true }
	if result := add(3); !result.OK() {
		t.Fatalf("Cloud add: %d %s", result.Status, result.Payload)
	}
	rows, err := s.server.projectReaders().registry.List()
	if err != nil || len(rows) != 1 || rows[0].Path != dir {
		t.Fatalf("registered = %#v, %v", rows, err)
	}
}

func TestProjectFolderCreationConfirmsOneChildAndReplaysReceipt(t *testing.T) {
	s := projectCloudStandIn(t)
	parent := filepath.Dir(s.server.cfg.Dir)
	create := func(key, name string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"parent": parent, "name": name})
		req := httptest.NewRequest(http.MethodPost, "/v1/project-folders", strings.NewReader(string(body)))
		req.Header.Set("Idempotency-Key", key)
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{verdict: auth.Verdict{Allowed: true, Device: "sender", Caps: auth.NewCaps(auth.Read, auth.Send)}}))
		out := httptest.NewRecorder()
		s.server.projectFolderCreateRoute(out, req)
		return out
	}
	first := create("create-one", "new project")
	if first.Code != 200 {
		t.Fatalf("create: %d %s", first.Code, first.Body)
	}
	var folder projectFolder
	if err := json.Unmarshal(first.Body.Bytes(), &folder); err != nil || folder.Path != filepath.Join(parent, "new project") {
		t.Fatalf("created folder = %#v, %v", folder, err)
	}
	if info, err := os.Stat(folder.Path); err != nil || !info.IsDir() {
		t.Fatalf("created directory: %v", err)
	}
	replay := create("create-one", "new project")
	if replay.Code != 200 || replay.Body.String() != first.Body.String() {
		t.Fatalf("replayed receipt = %d %s", replay.Code, replay.Body)
	}
	if duplicate := create("create-two", "new project"); duplicate.Code != 409 || !strings.Contains(duplicate.Body.String(), "project_folder_exists") {
		t.Fatalf("same name = %d %s", duplicate.Code, duplicate.Body)
	}
	if changed := create("create-one", "another"); changed.Code != 409 {
		t.Fatalf("key reused for another name = %d %s", changed.Code, changed.Body)
	}
	if _, err := os.Stat(filepath.Join(parent, "another")); !os.IsNotExist(err) {
		t.Fatal("reused key created another folder")
	}
	rows, err := s.server.projectReaders().registry.List()
	if err != nil || len(rows) != 0 {
		t.Fatalf("creation registered a Project: %v %v", rows, err)
	}
}

func TestProjectFolderCreationRejectsEscapesAndExistingFiles(t *testing.T) {
	s := projectCloudStandIn(t)
	parent := filepath.Dir(s.server.cfg.Dir)
	create := func(body string, sender bool, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/project-folders", strings.NewReader(body))
		if sender {
			req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{verdict: auth.Verdict{Allowed: true, Caps: auth.NewCaps(auth.Read, auth.Send)}}))
		}
		req.Header.Set("Idempotency-Key", key)
		out := httptest.NewRecorder()
		s.server.projectFolderCreateRoute(out, req)
		return out
	}
	body := func(parent, name string) string {
		data, _ := json.Marshal(map[string]string{"parent": parent, "name": name})
		return string(data)
	}
	if out := create(body(parent, "unpaired"), false, "no-send"); out.Code != 403 {
		t.Fatalf("read only = %d", out.Code)
	}
	if out := create(body(parent, "no-key"), true, ""); out.Code != 400 {
		t.Fatalf("missing key = %d", out.Code)
	}
	for _, name := range []string{"", "  ", ".", "..", "../outside", "a/b", `a\b`, "bad\x00name", "bad\nname"} {
		if out := create(body(parent, name), true, "bad-name"); out.Code != 400 {
			t.Errorf("name %q = %d %s", name, out.Code, out.Body)
		}
	}
	for _, data := range []string{body("relative", "sample"), body(filepath.Join(parent, "missing"), "sample"), `{"parent":"/tmp","name":"sample","extra":true}`, body(parent, "sample") + ` {}`} {
		if out := create(data, true, "bad-body"); out.Code < 400 {
			t.Errorf("invalid request succeeded: %s", data)
		}
	}
	file := filepath.Join(parent, "existing-file")
	if err := os.WriteFile(file, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := create(body(parent, "existing-file"), true, "file-exists")
	if out.Code != 409 {
		t.Fatalf("existing file = %d %s", out.Code, out.Body)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "keep" {
		t.Fatal("existing file changed")
	}
	if _, err := os.Stat(filepath.Join(parent, "unpaired")); !os.IsNotExist(err) {
		t.Fatal("read-only device created a folder")
	}
}

func TestCloudProjectFolderCreationUsesCommandGateAndReceipt(t *testing.T) {
	s := projectCloudStandIn(t)
	parent := filepath.Dir(s.server.cfg.Dir)
	request := map[string]any{"type": "project-folder-create", "session": cloudops.MachineReplySession, "request": "folder-create", "parent": parent, "name": "cloud-project"}
	s.bridge.AllowCommands = func() bool { return false }
	if result, _ := s.ask(t, 1, request); result.OK() {
		t.Fatal("folder creation passed disabled commands")
	}
	if _, err := os.Stat(filepath.Join(parent, "cloud-project")); !os.IsNotExist(err) {
		t.Fatal("disabled command wrote to disk")
	}
	s.bridge.AllowCommands = func() bool { return true }
	first, _ := s.ask(t, 2, request)
	if !first.OK() || !strings.Contains(string(first.Payload), "cloud-project") {
		t.Fatalf("create = %d %s", first.Status, first.Payload)
	}
	replay, _ := s.ask(t, 3, request)
	if !replay.OK() || string(replay.Payload) != string(first.Payload) {
		t.Fatalf("replay = %d %s", replay.Status, replay.Payload)
	}
	read, _ := s.ask(t, 4, map[string]any{"type": "project-folders", "session": cloudops.MachineReplySession, "request": "read-created", "path": filepath.Join(parent, "cloud-project")})
	if !read.OK() {
		t.Fatalf("read created = %d %s", read.Status, read.Payload)
	}
}

func TestProjectFolderSearchFindsNamesTwoLevelsUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, dir := range []string{"alpha-app", "code/alpha-api", "code/deep/alpha-too-deep", "Documents/alpha-doc", ".alpha-hidden"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "alpha.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	search := func(skip bool, q string) (int, []string, bool) {
		t.Helper()
		was := projectFolderSearchSkipsProtected
		projectFolderSearchSkipsProtected = skip
		defer func() { projectFolderSearchSkipsProtected = was }()
		out := httptest.NewRecorder()
		(&Server{}).projectFoldersRoute(out, httptest.NewRequest(http.MethodGet, "/v1/project-folders?q="+url.QueryEscape(q), nil))
		var answer struct {
			Folders          []projectFolder `json:"folders"`
			ProtectedSkipped bool            `json:"protected_skipped"`
		}
		if out.Code == 200 && json.Unmarshal(out.Body.Bytes(), &answer) != nil {
			t.Fatalf("answer: %s", out.Body.String())
		}
		names := []string{}
		for _, folder := range answer.Folders {
			names = append(names, strings.TrimPrefix(folder.Path, home+string(filepath.Separator)))
		}
		return out.Code, names, answer.ProtectedSkipped
	}

	code, names, skipped := search(true, "ALPHA")
	if code != 200 || strings.Join(names, ",") != "alpha-app,code/alpha-api" || !skipped {
		t.Fatalf("macOS search = %d %v skipped=%v", code, names, skipped)
	}
	code, names, skipped = search(false, "alpha")
	if code != 200 || strings.Join(names, ",") != "alpha-app,Documents/alpha-doc,code/alpha-api" || skipped {
		t.Fatalf("search = %d %v skipped=%v", code, names, skipped)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("a", maxProjectFolderQueryRunes+1)} {
		if code, _, _ := search(false, bad); code != 400 {
			t.Fatalf("query %q = %d", bad, code)
		}
	}
	both := httptest.NewRecorder()
	(&Server{}).projectFoldersRoute(both, httptest.NewRequest(http.MethodGet, "/v1/project-folders?q=a&path="+url.QueryEscape(home), nil))
	if both.Code != 400 {
		t.Fatalf("q with path = %d", both.Code)
	}
}

func TestProjectFolderSearchStopsAtItsAnswerBound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for i := 0; i < maxProjectFolderSearchRows+5; i++ {
		if err := os.Mkdir(filepath.Join(home, "match-"+strings.Repeat("x", i%3)+string(rune('a'+i%26))+string(rune('a'+i/26))), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	out := httptest.NewRecorder()
	(&Server{}).projectFoldersRoute(out, httptest.NewRequest(http.MethodGet, "/v1/project-folders?q=match", nil))
	var answer struct {
		Folders   []projectFolder `json:"folders"`
		Truncated bool            `json:"truncated"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &answer) != nil {
		t.Fatalf("answer: %d %s", out.Code, out.Body.String())
	}
	if len(answer.Folders) != maxProjectFolderSearchRows || !answer.Truncated {
		t.Fatalf("rows = %d truncated=%v", len(answer.Folders), answer.Truncated)
	}
}
