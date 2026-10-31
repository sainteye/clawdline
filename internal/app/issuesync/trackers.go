package issuesync

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/githubissues"
	"github.com/sainteye/clawdline/internal/adapters/jiraissues"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// failure is a tracker error with its stable code and, for a rate limit,
// the wait the tracker asked for.
type failure struct {
	code string
	wait time.Duration
	err  error
}

func (f failure) Error() string       { return f.code }
func (f failure) Unwrap() error       { return f.err }
func (f failure) Code() string        { return f.code }
func (f failure) Wait() time.Duration { return f.wait }

// Body is the issue text: the item's description, a line saying where the
// issue came from, and the marker a retried create looks for.
func Body(description, itemID string) string {
	text := strings.TrimSpace(description)
	if text != "" {
		text += "\n\n"
	}
	return text + "---\nOpened by Clawdline for a Board item; it closes when the item is done.\n\n" + work.IssueMarker(itemID)
}

type GitHubTracker struct {
	Conn   *githubissues.Connection
	Client *githubissues.Client
}

func (g GitHubTracker) token(ctx context.Context) (string, string, error) {
	token, err := g.Conn.AccessToken(ctx)
	if err != nil {
		return "", "", githubFailure(err)
	}
	return token, g.Conn.Status().Login, nil
}

func (g GitHubTracker) Create(ctx context.Context, d Due) (Result, error) {
	token, login, err := g.token(ctx)
	if err != nil {
		return Result{}, err
	}
	repo := d.Link.Target.Repo
	if found, ok, err := g.Client.FindIssue(ctx, token, repo, login, work.IssueMarker(d.Link.ItemID)); err != nil {
		return Result{}, githubFailure(err)
	} else if ok {
		return githubResult(found), nil
	}
	issue, err := g.Client.CreateIssue(ctx, token, repo, d.Title, Body(d.Description, d.Link.ItemID))
	if err != nil {
		return Result{}, githubFailure(err)
	}
	return githubResult(issue), nil
}

func githubResult(i githubissues.Issue) Result {
	r := Result{Observed: work.IssueClosed, ExternalKey: "#" + strconv.Itoa(i.Number), URL: i.URL}
	if i.Open {
		r.Observed = work.IssueOpen
	}
	return r
}

func (g GitHubTracker) number(d Due) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(d.Link.ExternalKey, "#"))
	if err != nil || n <= 0 {
		return 0, failure{code: "issue_link_broken"}
	}
	return n, nil
}

func (g GitHubTracker) Close(ctx context.Context, d Due, notPlanned bool) error {
	return g.setState(ctx, d, map[bool]string{true: "not_planned", false: "completed"}[notPlanned])
}

func (g GitHubTracker) Reopen(ctx context.Context, d Due) error { return g.setState(ctx, d, "") }

func (g GitHubTracker) setState(ctx context.Context, d Due, reason string) error {
	n, err := g.number(d)
	if err != nil {
		return err
	}
	token, _, err := g.token(ctx)
	if err != nil {
		return err
	}
	if err := g.Client.SetIssueState(ctx, token, d.Link.Target.Repo, n, reason); err != nil {
		return githubFailure(err)
	}
	return nil
}

func githubFailure(err error) error {
	var rl githubissues.RateLimitError
	switch {
	case errors.As(err, &rl):
		return failure{code: "github_rate_limited", wait: rl.RetryAfter, err: err}
	case errors.Is(err, githubissues.ErrNoToken):
		return failure{code: "issue_sync_authorization_required", err: err}
	case errors.Is(err, githubissues.ErrReauthorize):
		return failure{code: "github_reauthorization_required", err: err}
	case errors.Is(err, githubissues.ErrTargetNotFound):
		return failure{code: "issue_target_not_found", err: err}
	case errors.Is(err, githubissues.ErrIssuesDisabled):
		return failure{code: "issue_target_refused", err: err}
	}
	return failure{code: "github_unreachable", err: err}
}

type JiraTracker struct {
	Store  jiraissues.Store
	Client *jiraissues.Client
}

// credential answers the saved sign-in, refusing a target on another site:
// the item was synced to a site this machine is no longer connected to.
func (j JiraTracker) credential(d Due) (jiraissues.Credential, error) {
	cred, err := j.Store.Load()
	if err != nil {
		return cred, failure{code: "issue_sync_authorization_required", err: err}
	}
	if cred.Site != d.Link.Target.JiraSite {
		return cred, failure{code: "issue_sync_authorization_required"}
	}
	return cred, nil
}

func (j JiraTracker) Create(ctx context.Context, d Due) (Result, error) {
	cred, err := j.credential(d)
	if err != nil {
		return Result{}, err
	}
	t := d.Link.Target
	if found, ok, err := j.Client.FindIssue(ctx, cred, t.JiraProject, d.Link.ItemID); err != nil {
		return Result{}, jiraFailure(err)
	} else if ok {
		return jiraResult(found), nil
	}
	issue, err := j.Client.CreateIssue(ctx, cred, t.JiraProject, t.JiraIssueType, d.Link.ItemID, d.Title,
		strings.TrimSpace(d.Description+"\n\nOpened by Clawdline for a Board item; it closes when the item is done."))
	if err != nil {
		return Result{}, jiraFailure(err)
	}
	return jiraResult(issue), nil
}

func jiraResult(i jiraissues.Issue) Result {
	r := Result{Observed: work.IssueClosed, ExternalKey: i.Key, URL: i.URL}
	if i.Open {
		r.Observed = work.IssueOpen
	}
	return r
}

func (j JiraTracker) Close(ctx context.Context, d Due, notPlanned bool) error {
	cred, err := j.credential(d)
	if err != nil {
		return err
	}
	if d.Link.ExternalKey == "" {
		return failure{code: "issue_link_broken"}
	}
	if err := j.Client.Close(ctx, cred, d.Link.ExternalKey, notPlanned); err != nil {
		return jiraFailure(err)
	}
	return nil
}

func (j JiraTracker) Reopen(ctx context.Context, d Due) error {
	cred, err := j.credential(d)
	if err != nil {
		return err
	}
	if d.Link.ExternalKey == "" {
		return failure{code: "issue_link_broken"}
	}
	if err := j.Client.Reopen(ctx, cred, d.Link.ExternalKey); err != nil {
		return jiraFailure(err)
	}
	return nil
}

func jiraFailure(err error) error {
	var rl jiraissues.RateLimitError
	switch {
	case errors.As(err, &rl):
		return failure{code: "jira_rate_limited", wait: rl.RetryAfter, err: err}
	case errors.Is(err, jiraissues.ErrUnauthorized):
		return failure{code: "jira_reauthorization_required", err: err}
	case errors.Is(err, jiraissues.ErrTargetNotFound):
		return failure{code: "issue_target_not_found", err: err}
	case errors.Is(err, jiraissues.ErrRefused):
		return failure{code: "issue_target_refused", err: err}
	case errors.Is(err, jiraissues.ErrNoTransition):
		return failure{code: "jira_transition_missing", err: err}
	}
	return failure{code: "jira_unreachable", err: err}
}
