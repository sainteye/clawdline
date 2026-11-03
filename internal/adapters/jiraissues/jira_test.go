package jiraissues

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const itemID = "2f0c6b9e-0000-4000-8000-000000000001"

type fakeJira struct {
	transitions []map[string]any
	moved       string
	created     map[string]any
	found       bool
}

func (f *fakeJira) server(t *testing.T) (*Client, Credential) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "me@example.test" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/rest/api/3/myself":
			json.NewEncoder(w).Encode(map[string]any{"displayName": "Pat"})
		case r.URL.Path == "/rest/api/3/search/jql":
			if !strings.Contains(r.URL.Query().Get("jql"), Label(itemID)) {
				t.Errorf("search %q", r.URL.Query().Get("jql"))
			}
			issues := []any{}
			if f.found {
				issues = append(issues, map[string]any{"key": "OPS-7", "fields": map[string]any{
					"status": map[string]any{"statusCategory": map[string]any{"key": "indeterminate"}}}})
			}
			json.NewEncoder(w).Encode(map[string]any{"issues": issues})
		case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
			raw, _ := io.ReadAll(r.Body)
			json.Unmarshal(raw, &f.created)
			json.NewEncoder(w).Encode(map[string]any{"key": "OPS-8"})
		case strings.HasSuffix(r.URL.Path, "/transitions") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"transitions": f.transitions})
		case strings.HasSuffix(r.URL.Path, "/transitions") && r.Method == http.MethodPost:
			var body struct {
				Transition struct {
					ID string `json:"id"`
				} `json:"transition"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			f.moved = body.Transition.ID
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{HTTP: srv.Client()}, Credential{Site: srv.URL, Email: "me@example.test", Token: "secret"}
}

func to(id, name, category string) map[string]any {
	return map[string]any{"id": id, "name": name, "to": map[string]any{"name": name,
		"statusCategory": map[string]any{"key": category}}}
}

func TestACreatedIssueCarriesTheItemsLabelAndAReadableDescription(t *testing.T) {
	f := &fakeJira{}
	c, cred := f.server(t)
	if _, ok, err := c.FindIssue(context.Background(), cred, "OPS", itemID); ok || err != nil {
		t.Fatalf("an empty project found %v %v", ok, err)
	}
	issue, err := c.CreateIssue(context.Background(), cred, "OPS", "Task", itemID, "Fix login", "First line.\n\nSecond paragraph.")
	if err != nil || issue.Key != "OPS-8" || !strings.HasSuffix(issue.URL, "/browse/OPS-8") {
		t.Fatalf("%+v %v", issue, err)
	}
	fields := f.created["fields"].(map[string]any)
	if fields["labels"].([]any)[0] != Label(itemID) || fields["summary"] != "Fix login" {
		t.Fatalf("fields %v", fields)
	}
	doc := fields["description"].(map[string]any)
	if doc["type"] != "doc" || len(doc["content"].([]any)) != 2 {
		t.Fatalf("description %v", doc)
	}
	f.found = true
	if found, ok, err := c.FindIssue(context.Background(), cred, "OPS", itemID); !ok || err != nil || found.Key != "OPS-7" || !found.Open {
		t.Fatalf("an earlier issue was not found: %+v %v %v", found, ok, err)
	}
}

func TestClosingPicksTheTransitionTheReasonAsksFor(t *testing.T) {
	workflow := []map[string]any{to("11", "In Progress", "indeterminate"), to("21", "Won't Do", "done"),
		to("31", "Done", "done"), to("41", "To Do", "new")}
	cases := []struct {
		name string
		ts   []map[string]any
		run  func(*Client, Credential) error
		want string
	}{
		{"completed uses Done", workflow, func(c *Client, cr Credential) error { return c.Close(context.Background(), cr, "OPS-1", false) }, "31"},
		{"not planned prefers Won't Do", workflow, func(c *Client, cr Credential) error { return c.Close(context.Background(), cr, "OPS-1", true) }, "21"},
		{"not planned without one uses Done", []map[string]any{to("31", "Done", "done")}, func(c *Client, cr Credential) error { return c.Close(context.Background(), cr, "OPS-1", true) }, "31"},
		{"reopen prefers in progress", workflow, func(c *Client, cr Credential) error { return c.Reopen(context.Background(), cr, "OPS-1") }, "11"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeJira{transitions: tc.ts}
			c, cred := f.server(t)
			if err := tc.run(c, cred); err != nil || f.moved != tc.want {
				t.Fatalf("moved %q, want %q (%v)", f.moved, tc.want, err)
			}
		})
	}
	f := &fakeJira{transitions: []map[string]any{to("11", "In Progress", "indeterminate")}}
	c, cred := f.server(t)
	if err := c.Close(context.Background(), cred, "OPS-1", false); !errors.Is(err, ErrNoTransition) {
		t.Fatalf("a workflow with no done status: %v", err)
	}
}

func TestAWrongTokenIsAskedForAgain(t *testing.T) {
	c, cred := (&fakeJira{}).server(t)
	cred.Token = "wrong"
	if _, err := c.Myself(context.Background(), cred); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong token: %v", err)
	}
}

func TestTheCredentialFileIsOwnerOnly(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "jira")}
	if _, err := s.Load(); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("absent: %v", err)
	}
	cred := Credential{Site: "https://acme.atlassian.net", Email: "me@example.test", Token: "secret"}
	if err := s.Save(cred); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Load(); err != nil || got != cred {
		t.Fatalf("%+v %v", got, err)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(s.path()); info.Mode().Perm() != 0o600 {
			t.Fatalf("file mode %v", info.Mode().Perm())
		}
	}
	os.WriteFile(s.path(), []byte("{"), 0o600)
	if _, err := s.Load(); err == nil || errors.Is(err, ErrNotConnected) {
		t.Fatalf("damaged: %v", err)
	}
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
}
