package githubissues

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// RemoteReader is the one git question a Project's target needs.
type RemoteReader interface {
	Remote(ctx context.Context, cwd string) (string, error)
}

// Typed reasons a Project has no GitHub target. Each asks the person for a
// different fix, so none of them is folded into another.
var (
	ErrNoRepository    = errors.New("issue_sync_not_a_repository")
	ErrNoRemote        = errors.New("issue_sync_no_remote")
	ErrRemoteNotGitHub = errors.New("issue_sync_remote_not_github")
	ErrRemoteUnread    = errors.New("issue_sync_remote_unreadable")
)

// TargetFor is the Project's GitHub repository, read from its origin remote
// on this machine. Nothing is asked of GitHub.
func TargetFor(ctx context.Context, r RemoteReader, projectPath string) (work.IssueTarget, error) {
	remote, err := r.Remote(ctx, projectPath)
	switch {
	case err == nil:
	case errors.Is(err, git.ErrNoRemote):
		return work.IssueTarget{}, ErrNoRemote
	case errors.Is(err, git.ErrNotRepository):
		return work.IssueTarget{}, ErrNoRepository
	default:
		return work.IssueTarget{}, ErrRemoteUnread
	}
	t := work.IssueTarget{Provider: work.IssueProviderGitHub, Repo: GitHubRepo(remote)}
	if t.Repo == "" || t.Validate() != nil {
		return work.IssueTarget{}, ErrRemoteNotGitHub
	}
	return t, nil
}

// GitHubRepo is "owner/repo" out of a github.com remote in any of git's
// three spellings (scp-like, ssh:// and https://), and "" for any other host.
// A host that merely contains "github.com", such as an Enterprise server or a
// mirror, is not GitHub.
func GitHubRepo(remote string) string {
	remote = strings.TrimSpace(remote)
	var host, path string
	if u, err := url.Parse(remote); err == nil && u.Scheme != "" && u.Host != "" {
		if u.Scheme != "https" && u.Scheme != "ssh" && u.Scheme != "git" {
			return ""
		}
		host, path = u.Hostname(), u.Path
	} else if at, rest, ok := strings.Cut(remote, "@"); ok && !strings.Contains(at, "/") {
		host, path, ok = strings.Cut(rest, ":")
		if !ok {
			return ""
		}
	} else {
		return ""
	}
	if !strings.EqualFold(host, "github.com") {
		return ""
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	owner, repo, ok := strings.Cut(path, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return ""
	}
	return owner + "/" + repo
}
