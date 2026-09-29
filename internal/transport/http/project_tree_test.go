package http

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projectfiles"
)

func TestProjectTreeRouteListsAndReadsOnlyWithinChosenProject(t *testing.T) {
	s := iconCloudStandIn(t)
	project, ok := s.server.workV2Project(t.Context(), s.place)
	if !ok {
		t.Fatal("fixture place")
	}
	if err := os.Mkdir(filepath.Join(project.Path, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project.Path, "src", "page.ts"), []byte("export {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	local, _, err := s.server.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, target, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://127.0.0.1:7757"+target, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		out := httptest.NewRecorder()
		s.handler.ServeHTTP(out, req)
		return out
	}
	base := "/v1/projects/" + s.place + "/tree"
	if out := request("GET", base, ""); out.Code == 200 {
		t.Fatal("unauthorized tree read accepted")
	}
	root := request("GET", base, local)
	var listing projectfiles.TreeListing
	if root.Code != 200 || json.Unmarshal(root.Body.Bytes(), &listing) != nil {
		t.Fatalf("root: %d %s", root.Code, root.Body.String())
	}
	var found bool
	for _, entry := range listing.Entries {
		found = found || entry.Name == "src" && entry.Kind == "directory"
	}
	if !found {
		t.Fatalf("src missing: %+v", listing.Entries)
	}
	nested := request("GET", base+"?directory=src", local)
	if nested.Code != 200 || json.Unmarshal(nested.Body.Bytes(), &listing) != nil ||
		len(listing.Entries) != 1 || listing.Entries[0].Path != "src/page.ts" {
		t.Fatalf("nested: %d %s", nested.Code, nested.Body.String())
	}
	read := request("GET", base+"/file?path=src%2Fpage.ts", local)
	var content projectfiles.TreeContent
	if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &content) != nil || content.Text != "export {}\n" {
		t.Fatalf("read: %d %s", read.Code, read.Body.String())
	}
	for _, target := range []string{
		base + "/file?path=" + url.QueryEscape("../secret"),
		base + "/file?path=" + url.QueryEscape(".git/config"),
		base + "/file?path=src%2Fpage.ts&path=src%2Fpage.ts",
		base + "?directory=src&unexpected=x",
	} {
		if out := request("GET", target, local); out.Code != 400 {
			t.Errorf("bad path %q: %d %s", target, out.Code, out.Body.String())
		}
	}
	if out := request("PUT", base+"/file?path=src%2Fpage.ts", local); out.Code != 405 {
		t.Fatalf("read-only route: %d %s", out.Code, out.Body.String())
	}
	if got, _ := os.ReadFile(filepath.Join(project.Path, "src", "page.ts")); strings.TrimSpace(string(got)) != "export {}" {
		t.Fatalf("file changed: %q", got)
	}
}
