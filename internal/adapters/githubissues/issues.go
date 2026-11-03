package githubissues

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Typed failures of an issue call. Each one asks for a different response:
// connect again, wait, or tell the person the repository refused.
var (
	ErrRateLimited    = errors.New("github_rate_limited")
	ErrTargetNotFound = errors.New("issue_target_not_found")
	ErrIssuesDisabled = errors.New("issue_target_refused")
)

// RateLimitError carries how long GitHub asked to wait.
type RateLimitError struct{ RetryAfter time.Duration }

func (e RateLimitError) Error() string { return ErrRateLimited.Error() }
func (e RateLimitError) Unwrap() error { return ErrRateLimited }

// Issue is what the item records about its issue.
type Issue struct {
	Number int
	URL    string
	Open   bool
}

// MaxIssueScanRows is how many of the account's newest issues in the
// repository a create reads to find one an earlier attempt already opened.
// A retry follows its attempt within minutes, so the earlier issue is among
// the newest few.
const MaxIssueScanRows = 30

// FindIssue looks for the issue carrying marker among the newest issues this
// account opened in the repository. It answers ok=false when there is none.
func (c *Client) FindIssue(ctx context.Context, token, repo, login, marker string) (Issue, bool, error) {
	path := "/repos/" + repo + "/issues?state=all&sort=created&direction=desc&per_page=" +
		strconv.Itoa(MaxIssueScanRows) + "&creator=" + login
	var rows []struct {
		Number      int             `json:"number"`
		HTMLURL     string          `json:"html_url"`
		State       string          `json:"state"`
		Body        string          `json:"body"`
		PullRequest json.RawMessage `json:"pull_request"`
	}
	if err := c.api(ctx, token, http.MethodGet, path, nil, &rows); err != nil {
		return Issue{}, false, err
	}
	for _, r := range rows {
		if len(r.PullRequest) == 0 && strings.Contains(r.Body, marker) {
			return Issue{Number: r.Number, URL: r.HTMLURL, Open: r.State == "open"}, true, nil
		}
	}
	return Issue{}, false, nil
}

func (c *Client) CreateIssue(ctx context.Context, token, repo, title, body string) (Issue, error) {
	var out struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	err := c.api(ctx, token, http.MethodPost, "/repos/"+repo+"/issues", map[string]any{"title": title, "body": body}, &out)
	if err != nil {
		return Issue{}, err
	}
	if out.Number <= 0 {
		return Issue{}, errors.New("GitHub answered the new issue without a number")
	}
	return Issue{Number: out.Number, URL: out.HTMLURL, Open: true}, nil
}

// SetIssueState closes an issue with a reason ("completed" or "not_planned")
// or reopens it when reason is empty.
func (c *Client) SetIssueState(ctx context.Context, token, repo string, number int, reason string) error {
	body := map[string]any{"state": "open"}
	if reason != "" {
		body = map[string]any{"state": "closed", "state_reason": reason}
	}
	return c.api(ctx, token, http.MethodPatch, "/repos/"+repo+"/issues/"+strconv.Itoa(number), body, nil)
}

func (c *Client) Comment(ctx context.Context, token, repo string, number int, text string) error {
	return c.api(ctx, token, http.MethodPost, "/repos/"+repo+"/issues/"+strconv.Itoa(number)+"/comments",
		map[string]any{"body": text}, nil)
}

// api is one REST call. Statuses become the typed failures above; a body is
// never repeated in an error.
func (c *Client) api(ctx context.Context, token, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.APIURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("GitHub's answer could not be read: %w", err)
	}
	if len(raw) > MaxResponseBytes {
		return errors.New("GitHub's answer is larger than expected")
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrReauthorize
	case resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusForbidden && (resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-RateLimit-Remaining") == "0")):
		return RateLimitError{RetryAfter: retryAfter(resp.Header, c.now())}
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrTargetNotFound
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnprocessableEntity:
		return ErrIssuesDisabled
	case resp.StatusCode/100 != 2:
		return fmt.Errorf("GitHub answered %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.New("GitHub's answer is not JSON")
	}
	return nil
}

// retryAfter reads Retry-After seconds, or the reset time of the exhausted
// rate limit, and answers a minute when GitHub said neither.
func retryAfter(h http.Header, now time.Time) time.Duration {
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		if d := time.Unix(reset, 0).Sub(now); d > 0 {
			return d
		}
	}
	return time.Minute
}
