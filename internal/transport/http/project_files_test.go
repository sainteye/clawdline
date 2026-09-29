package http

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projectfiles"
)

func TestProjectFilesRouteListsReadsAndComparesWrites(t *testing.T) {
	s := iconCloudStandIn(t)
	project, ok := s.server.workV2Project(t.Context(), s.place)
	if !ok {
		t.Fatal("fixture place")
	}
	path := filepath.Join(project.Path, "AGENTS.md")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	local, _, err := s.server.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, url, body, key, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://127.0.0.1:7757"+url, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		out := httptest.NewRecorder()
		s.handler.ServeHTTP(out, req)
		return out
	}
	base := "/v1/projects/" + s.place + "/files"
	list := request("GET", base, "", "", local)
	if list.Code != 200 {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	var catalog projectfiles.Listing
	if err := json.Unmarshal(list.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	var id string
	for _, f := range catalog.Files {
		if f.Location == "AGENTS.md" {
			id = f.ID
			break
		}
	}
	if id == "" {
		t.Fatal("AGENTS.md absent")
	}
	fileURL := base + "/" + id
	read := request("GET", fileURL, "", "", local)
	var before projectfiles.Content
	if err := json.Unmarshal(read.Body.Bytes(), &before); err != nil || before.Text != "before" {
		t.Fatalf("read: %d %s %v", read.Code, read.Body.String(), err)
	}
	update := `{"expected_version":"` + before.Version + `","content":"after"}`
	unauthorized := request("PUT", fileURL, update, "once", "")
	if unauthorized.Code == 200 {
		t.Fatal("unauthorized write accepted")
	}
	write := request("PUT", fileURL, update, "once", local)
	if write.Code != 200 {
		t.Fatalf("write: %d %s", write.Code, write.Body.String())
	}
	if got, _ := os.ReadFile(path); string(got) != "after" {
		t.Fatalf("saved %q", got)
	}
	if replay := request("PUT", fileURL, update, "once", local); replay.Code != 200 {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body.String())
	}
	changedBody := request("PUT", fileURL, `{"expected_version":"`+before.Version+`","content":"other"}`, "once", local)
	if changedBody.Code != 409 {
		t.Fatalf("same key different body: %d %s", changedBody.Code, changedBody.Body.String())
	}
	stale := request("PUT", fileURL, update, "twice", local)
	if stale.Code != 409 {
		t.Fatalf("stale: %d %s", stale.Code, stale.Body.String())
	}
	unknown := request("GET", base+"/unknown", "", "", local)
	if unknown.Code != 404 {
		t.Fatalf("unknown id: %d", unknown.Code)
	}
	otherPlace := request("GET", "/v1/projects/not-a-place/files", "", "", local)
	if otherPlace.Code != 404 {
		t.Fatalf("unknown project: %d", otherPlace.Code)
	}
}
