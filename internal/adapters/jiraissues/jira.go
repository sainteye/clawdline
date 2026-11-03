// Package jiraissues connects this machine to Jira Cloud for issue sync
// (docs/issue-sync.md, D73): the person's site, account email and API token,
// kept in an owner-only file, and the REST calls that open, find, close and
// reopen one item's issue.
//
// Jira Cloud's OAuth needs a client secret and a server to hold it; an API
// token is what a person can give a daemon on their own machine.
package jiraissues

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MaxResponseBytes bounds one answer read from Jira. A transitions list or a
// page of projects is a few tens of kilobytes at most.
const MaxResponseBytes = 256 << 10

var (
	ErrUnauthorized   = errors.New("jira_reauthorization_required")
	ErrRateLimited    = errors.New("jira_rate_limited")
	ErrTargetNotFound = errors.New("issue_target_not_found")
	ErrRefused        = errors.New("issue_target_refused")
	ErrNoTransition   = errors.New("jira_transition_missing")
)

type RateLimitError struct{ RetryAfter time.Duration }

func (e RateLimitError) Error() string { return ErrRateLimited.Error() }
func (e RateLimitError) Unwrap() error { return ErrRateLimited }

// Credential is one Jira Cloud site's sign-in. Token is a secret: no route
// answers it.
type Credential struct {
	Site    string `json:"site"`
	Email   string `json:"email"`
	Token   string `json:"token"`
	Account string `json:"account"`
}

type Client struct {
	HTTP *http.Client
	Now  func() time.Time
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 20 * time.Second}, Now: time.Now}
}

func (c *Client) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

// Myself checks a credential and names its account.
func (c *Client) Myself(ctx context.Context, cred Credential) (string, error) {
	var out struct {
		DisplayName  string `json:"displayName"`
		EmailAddress string `json:"emailAddress"`
	}
	if err := c.call(ctx, cred, http.MethodGet, "/rest/api/3/myself", nil, &out); err != nil {
		return "", err
	}
	if out.DisplayName != "" {
		return out.DisplayName, nil
	}
	return cred.Email, nil
}

// Project is one choice in the Console's Jira target picker.
type Project struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	IssueTypes []string `json:"issue_types"`
}

// MaxProjects bounds the projects one picker lists.
const MaxProjects = 100

// Projects lists the projects this account can create issues in, with their
// issue types.
func (c *Client) Projects(ctx context.Context, cred Credential) ([]Project, error) {
	var out struct {
		Values []struct {
			Key        string `json:"key"`
			Name       string `json:"name"`
			IssueTypes []struct {
				Name    string `json:"name"`
				Subtask bool   `json:"subtask"`
			} `json:"issueTypes"`
		} `json:"values"`
	}
	path := "/rest/api/3/project/search?expand=issueTypes&maxResults=" + strconv.Itoa(MaxProjects)
	if err := c.call(ctx, cred, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	projects := make([]Project, 0, len(out.Values))
	for _, v := range out.Values {
		p := Project{Key: v.Key, Name: v.Name}
		for _, t := range v.IssueTypes {
			if !t.Subtask {
				p.IssueTypes = append(p.IssueTypes, t.Name)
			}
		}
		projects = append(projects, p)
	}
	return projects, nil
}

type Issue struct {
	Key  string
	URL  string
	Open bool
}

// Label marks the issue an item opened, so a retried create finds it.
func Label(itemID string) string { return "clawdline-item-" + itemID }

var labelPattern = regexp.MustCompile(`^clawdline-item-[0-9a-f-]{36}$`)

// FindIssue answers the issue carrying the item's label, if one exists.
func (c *Client) FindIssue(ctx context.Context, cred Credential, project, itemID string) (Issue, bool, error) {
	label := Label(itemID)
	if !labelPattern.MatchString(label) {
		return Issue{}, false, errors.New("an item id is a UUID")
	}
	jql := fmt.Sprintf(`project = "%s" AND labels = "%s"`, project, label)
	var out struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Status struct {
					StatusCategory struct {
						Key string `json:"key"`
					} `json:"statusCategory"`
				} `json:"status"`
			} `json:"fields"`
		} `json:"issues"`
	}
	path := "/rest/api/3/search/jql?maxResults=1&fields=status&jql=" + url.QueryEscape(jql)
	if err := c.call(ctx, cred, http.MethodGet, path, nil, &out); err != nil {
		return Issue{}, false, err
	}
	if len(out.Issues) == 0 {
		return Issue{}, false, nil
	}
	i := out.Issues[0]
	return Issue{Key: i.Key, URL: cred.Site + "/browse/" + i.Key, Open: i.Fields.Status.StatusCategory.Key != "done"}, true, nil
}

// CreateIssue opens the issue. The description is plain paragraphs: Jira
// Cloud's v3 API takes Atlassian Document Format, not Markdown.
func (c *Client) CreateIssue(ctx context.Context, cred Credential, project, issueType, itemID, title, description string) (Issue, error) {
	fields := map[string]any{
		"project":     map[string]any{"key": project},
		"issuetype":   map[string]any{"name": issueType},
		"summary":     title,
		"labels":      []string{Label(itemID)},
		"description": document(description),
	}
	var out struct {
		Key string `json:"key"`
	}
	if err := c.call(ctx, cred, http.MethodPost, "/rest/api/3/issue", map[string]any{"fields": fields}, &out); err != nil {
		return Issue{}, err
	}
	if out.Key == "" {
		return Issue{}, errors.New("Jira answered the new issue without a key")
	}
	return Issue{Key: out.Key, URL: cred.Site + "/browse/" + out.Key, Open: true}, nil
}

// document is the smallest Atlassian Document: one paragraph per line block.
func document(text string) map[string]any {
	var content []any
	for _, block := range strings.Split(strings.TrimSpace(text), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		content = append(content, map[string]any{"type": "paragraph",
			"content": []any{map[string]any{"type": "text", "text": block}}})
	}
	if content == nil {
		content = []any{map[string]any{"type": "paragraph", "content": []any{}}}
	}
	return map[string]any{"type": "doc", "version": 1, "content": content}
}

// Close moves the issue to a done-category status. notPlanned prefers a
// transition named like "Won't Do" or "Cancelled" when the workflow has one,
// and otherwise uses the ordinary done transition.
func (c *Client) Close(ctx context.Context, cred Credential, key string, notPlanned bool) error {
	return c.transition(ctx, cred, key, func(ts []transition) string {
		var done, declined string
		for _, t := range ts {
			if t.To.StatusCategory.Key != "done" {
				continue
			}
			name := strings.ToLower(t.Name + " " + t.To.Name)
			if declinedName.MatchString(name) {
				if declined == "" {
					declined = t.ID
				}
			} else if done == "" {
				done = t.ID
			}
		}
		if notPlanned && declined != "" {
			return declined
		}
		if done != "" {
			return done
		}
		return declined
	})
}

var declinedName = regexp.MustCompile(`won'?t|cancel|reject|declin|not planned|obsolete|duplicate`)

// Reopen moves the issue back to a to-do or in-progress status.
func (c *Client) Reopen(ctx context.Context, cred Credential, key string) error {
	return c.transition(ctx, cred, key, func(ts []transition) string {
		var todo string
		for _, t := range ts {
			switch t.To.StatusCategory.Key {
			case "indeterminate":
				return t.ID
			case "new":
				if todo == "" {
					todo = t.ID
				}
			}
		}
		return todo
	})
}

type transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   struct {
		Name           string `json:"name"`
		StatusCategory struct {
			Key string `json:"key"`
		} `json:"statusCategory"`
	} `json:"to"`
}

func (c *Client) transition(ctx context.Context, cred Credential, key string, choose func([]transition) string) error {
	var out struct {
		Transitions []transition `json:"transitions"`
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/transitions"
	if err := c.call(ctx, cred, http.MethodGet, path, nil, &out); err != nil {
		return err
	}
	id := choose(out.Transitions)
	if id == "" {
		return ErrNoTransition
	}
	return c.call(ctx, cred, http.MethodPost, path, map[string]any{"transition": map[string]any{"id": id}}, nil)
}

func (c *Client) Comment(ctx context.Context, cred Credential, key, text string) error {
	return c.call(ctx, cred, http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(key)+"/comment",
		map[string]any{"body": document(text)}, nil)
}

func (c *Client) call(ctx context.Context, cred Credential, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, cred.Site+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(cred.Email+":"+cred.Token)))
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Jira could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("Jira's answer could not be read: %w", err)
	}
	if len(raw) > MaxResponseBytes {
		return errors.New("Jira's answer is larger than expected")
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusTooManyRequests:
		wait := time.Minute
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
			wait = time.Duration(s) * time.Second
		}
		return RateLimitError{RetryAfter: wait}
	case resp.StatusCode == http.StatusNotFound:
		return ErrTargetNotFound
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusBadRequest:
		return ErrRefused
	case resp.StatusCode/100 != 2:
		return fmt.Errorf("Jira answered %d", resp.StatusCode)
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.New("Jira's answer is not JSON")
	}
	return nil
}
