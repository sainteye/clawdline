package githubissues

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func issueServer(t *testing.T, h http.HandlerFunc) *Client {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{HTTP: srv.Client(), OAuthURL: srv.URL, APIURL: srv.URL, ClientID: ClientID,
		Now: func() time.Time { return time.Unix(1_800_000_000, 0) }}
}

func TestAnEarlierAttemptsIssueIsFoundByItsMarker(t *testing.T) {
	marker := "<!-- clawdline:item:abc -->"
	c := issueServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/octo/widgets/issues" || r.URL.Query().Get("creator") != "octocat" ||
			r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("request %s %v", r.URL, r.Header)
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 9, "html_url": "https://github.com/octo/widgets/pull/9", "state": "open", "body": marker, "pull_request": map[string]any{}},
			{"number": 8, "html_url": "https://github.com/octo/widgets/issues/8", "state": "closed", "body": "text\n" + marker},
		})
	})
	got, ok, err := c.FindIssue(context.Background(), "tok", "octo/widgets", "octocat", marker)
	if err != nil || !ok || got.Number != 8 || got.Open {
		t.Fatalf("%+v %v %v", got, ok, err)
	}
	if _, ok, _ := c.FindIssue(context.Background(), "tok", "octo/widgets", "octocat", "<!-- other -->"); ok {
		t.Fatal("an unmarked issue was taken for the item's")
	}
}

func TestOpeningClosingAndReopeningSendWhatGitHubExpects(t *testing.T) {
	var got []map[string]any
	c := issueServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		body["_"] = r.Method + " " + r.URL.Path
		got = append(got, body)
		json.NewEncoder(w).Encode(map[string]any{"number": 12, "html_url": "https://github.com/octo/widgets/issues/12"})
	})
	issue, err := c.CreateIssue(context.Background(), "tok", "octo/widgets", "Fix login", "body")
	if err != nil || issue.Number != 12 || !issue.Open {
		t.Fatalf("%+v %v", issue, err)
	}
	c.SetIssueState(context.Background(), "tok", "octo/widgets", 12, "not_planned")
	c.SetIssueState(context.Background(), "tok", "octo/widgets", 12, "")
	if got[0]["_"] != "POST /repos/octo/widgets/issues" || got[0]["title"] != "Fix login" {
		t.Fatalf("create %v", got[0])
	}
	if got[1]["_"] != "PATCH /repos/octo/widgets/issues/12" || got[1]["state"] != "closed" || got[1]["state_reason"] != "not_planned" {
		t.Fatalf("close %v", got[1])
	}
	if got[2]["state"] != "open" || got[2]["state_reason"] != nil {
		t.Fatalf("reopen %v", got[2])
	}
}

func TestEachRefusalIsTyped(t *testing.T) {
	cases := []struct {
		status  int
		headers map[string]string
		want    error
	}{
		{http.StatusUnauthorized, nil, ErrReauthorize},
		{http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1800000090"}, ErrRateLimited},
		{http.StatusTooManyRequests, map[string]string{"Retry-After": "30"}, ErrRateLimited},
		{http.StatusNotFound, nil, ErrTargetNotFound},
		{http.StatusGone, nil, ErrTargetNotFound},
		{http.StatusForbidden, nil, ErrIssuesDisabled},
		{http.StatusUnprocessableEntity, nil, ErrIssuesDisabled},
	}
	for _, tc := range cases {
		c := issueServer(t, func(w http.ResponseWriter, r *http.Request) {
			for k, v := range tc.headers {
				w.Header().Set(k, v)
			}
			w.WriteHeader(tc.status)
		})
		_, err := c.CreateIssue(context.Background(), "tok", "octo/widgets", "t", "b")
		if !errors.Is(err, tc.want) {
			t.Errorf("%d %v: %v, want %v", tc.status, tc.headers, err, tc.want)
		}
		var rl RateLimitError
		if errors.As(err, &rl) {
			want := 30 * time.Second
			if tc.headers["X-RateLimit-Reset"] != "" {
				want = 90 * time.Second
			}
			if rl.RetryAfter != want {
				t.Errorf("%d waits %v, want %v", tc.status, rl.RetryAfter, want)
			}
		}
	}
}
