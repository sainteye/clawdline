package projectassistantwebhook

import (
	"encoding/json"
	"net/http"
	"testing"
)

func githubEvent(t *testing.T, name, body string) map[string]any {
	t.Helper()
	in, err := Decode(headers("X-GitHub-Event", name), []byte(body), upstreamBounds)
	if err != nil || in.Ignored {
		t.Fatalf("%s: %+v, %v", name, in, err)
	}
	return in.Event
}

func TestGitHubNormalization(t *testing.T) {
	e := githubEvent(t, "pull_request", `{"action":"opened","pull_request":{"title":"Fix login","html_url":"https://example.invalid/o/r/pull/1"},"repository":{"full_name":"o/r"},"sender":{"login":"kai"}}`)
	want := map[string]any{"source": "github", "event": "pull_request.opened", "repo": "o/r", "sender": "kai", "text": "Fix login", "url": "https://example.invalid/o/r/pull/1"}
	for k, v := range want {
		if e[k] != v {
			t.Errorf("%s = %v, want %v", k, e[k], v)
		}
	}
	payload, _ := e["payload"].(map[string]any)
	if payload["sender"].(map[string]any)["login"] != "kai" || payload["action"] != "opened" {
		t.Fatalf("payload not kept whole: %v", payload)
	}
	if len(e) != 7 {
		t.Fatalf("unexpected fields: %v", e)
	}

	push := githubEvent(t, "push", `{"head_commit":{"message":"m"},"compare":"c"}`)
	if push["event"] != "push" || push["text"] != "m" || push["url"] != "c" || push["repo"] != nil || push["sender"] != nil {
		t.Fatalf("push: %v", push)
	}

	// Upstream precedence: a comment's URL beats its pull request's, but the
	// pull request's title beats the comment's body.
	review := githubEvent(t, "pull_request_review_comment", `{"action":"created","pull_request":{"title":"T","html_url":"pr"},"comment":{"body":"B","html_url":"c"}}`)
	if review["text"] != "T" || review["url"] != "c" || review["event"] != "pull_request_review_comment.created" {
		t.Fatalf("precedence: %v", review)
	}
	// An empty string is skipped for the next path; a non-string is skipped too.
	release := githubEvent(t, "release", `{"action":"published","release":{"name":"","tag_name":"v1.2","html_url":7}}`)
	if release["text"] != "v1.2" || release["url"] != nil {
		t.Fatalf("release: %v", release)
	}
	// A non-string action is not appended; an empty string action is, as upstream.
	if e := githubEvent(t, "issues", `{"action":5}`); e["event"] != "issues" {
		t.Fatalf("numeric action: %v", e["event"])
	}
	if e := githubEvent(t, "issues", `{"action":""}`); e["event"] != "issues." {
		t.Fatalf("empty action: %v", e["event"])
	}
	// repo and sender keep the JSON value they had; they only match a filter as strings.
	odd := githubEvent(t, "workflow_run", `{"repository":{"full_name":42},"sender":"kai","workflow_run":{"name":"CI","html_url":"w"}}`)
	if n, ok := odd["repo"].(json.Number); !ok || n.String() != "42" || odd["sender"] != nil || odd["text"] != "CI" || odd["url"] != "w" {
		t.Fatalf("non-string fields: %#v", odd)
	}
	// A whitespace body is an empty delivery, as upstream; any other non-object is refused.
	if e := githubEvent(t, "push", " "); e["event"] != "push" || e["text"] != nil {
		t.Fatalf("empty push: %v", e)
	}
	for _, body := range []string{"payload=%7B%7D", "[1]", "null"} {
		if _, err := Decode(headers("X-GitHub-Event", "push"), []byte(body), upstreamBounds); FailureOf(err) == nil || FailureOf(err).Code != Invalid {
			t.Errorf("%q accepted", body)
		}
	}
	// Header lookup is case-insensitive through http.Header.
	h := http.Header{}
	h.Set("x-github-event", "push")
	if in, err := Decode(h, []byte(`{}`), upstreamBounds); err != nil || in.Event["source"] != "github" {
		t.Fatalf("canonical header: %+v, %v", in, err)
	}
}
