package githubissues

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	ChangeRowsLimit          = 50
	ChangeEvidenceRunesLimit = 240
	// GitHub includes changed file patches on the first comparison page.
	ChangeResponseBytesLimit = 4 << 20
)

var (
	ErrNoPublishedRelease = errors.New("github_no_published_release")
	ErrReleaseOffBranch   = errors.New("github_release_off_default_branch")
	ErrComparisonMissing  = errors.New("github_comparison_unavailable")
	gitObjectID           = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type ChangeWindow struct {
	// Zero times select the latest published release's tag as the base.
	Since, Until time.Time
}

type GitHubChanges struct {
	Repository string         `json:"repository"`
	Branch     string         `json:"branch"`
	Source     string         `json:"source_url"`
	Release    *ChangeRelease `json:"release,omitempty"`
	Since      *time.Time     `json:"since,omitempty"`
	Until      *time.Time     `json:"until,omitempty"`
	Commits    []ChangeCommit `json:"commits"`
	Complete   bool           `json:"complete"`
	Total      *int           `json:"total,omitempty"`
}

type ChangeRelease struct {
	Tag         string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	URL         string    `json:"url"`
}

type ChangeCommit struct {
	SHA     string    `json:"sha"`
	At      time.Time `json:"committed_at"`
	Message string    `json:"message"`
	URL     string    `json:"url"`
}

type githubChangeCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message   string `json:"message"`
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

// RepositoryChanges reads GitHub's default branch. Local commits, working
// tree changes, draft releases and prereleases are not evidence for this report.
func (c *Client) RepositoryChanges(ctx context.Context, token, repo string, window ChangeWindow) (GitHubChanges, error) {
	web := "https://github.com/" + repo
	path := "/repos/" + repo
	var repository struct {
		Branch string `json:"default_branch"`
	}
	if err := c.api(ctx, token, http.MethodGet, path, nil, &repository); err != nil {
		return GitHubChanges{}, err
	}
	if repository.Branch == "" {
		return GitHubChanges{}, ErrComparisonMissing
	}
	branch := url.PathEscape(repository.Branch)
	report := GitHubChanges{Repository: repo, Branch: repository.Branch, Source: web + "/commits/" + branch, Complete: true}
	var rows []githubChangeCommit
	read := func(path string, out any) error {
		return c.apiWithLimit(ctx, token, http.MethodGet, path, nil, out, ChangeResponseBytesLimit)
	}
	if window.Since.IsZero() {
		var release ChangeRelease
		// Release bodies share the larger response budget; the report keeps
		// only the tag and publication time.
		if err := read(path+"/releases/latest", &release); err != nil {
			if errors.Is(err, ErrTargetNotFound) {
				return GitHubChanges{}, ErrNoPublishedRelease
			}
			return GitHubChanges{}, err
		}
		if release.Tag == "" || release.PublishedAt.IsZero() {
			return GitHubChanges{}, ErrComparisonMissing
		}
		release.URL = web + "/releases/tag/" + url.PathEscape(release.Tag)
		report.Release = &release
		// Qualify both refs so a branch and tag with the same name cannot
		// silently change the comparison.
		refs := url.PathEscape("refs/tags/"+release.Tag) + "..." + url.PathEscape("refs/heads/"+repository.Branch)
		var comparison struct {
			Status  string               `json:"status"`
			Total   int                  `json:"total_commits"`
			Commits []githubChangeCommit `json:"commits"`
		}
		if err := read(path+"/compare/"+refs+"?per_page="+strconv.Itoa(ChangeRowsLimit), &comparison); err != nil {
			if errors.Is(err, ErrTargetNotFound) {
				return GitHubChanges{}, ErrComparisonMissing
			}
			return GitHubChanges{}, err
		}
		switch comparison.Status {
		case "identical", "ahead":
		case "behind", "diverged":
			return GitHubChanges{}, ErrReleaseOffBranch
		default:
			return GitHubChanges{}, ErrComparisonMissing
		}
		report.Source = web + "/compare/" + refs
		report.Total = &comparison.Total
		rows = comparison.Commits
		report.Complete = comparison.Total == len(rows)
		// GitHub sorts a comparison oldest first, the reverse of list commits.
		slices.Reverse(rows)
	} else {
		report.Since, report.Until = &window.Since, &window.Until
		q := url.Values{
			"sha":      {repository.Branch},
			"since":    {window.Since.UTC().Format(time.RFC3339)},
			"until":    {window.Until.UTC().Format(time.RFC3339)},
			"per_page": {strconv.Itoa(ChangeRowsLimit + 1)},
		}
		if err := read(path+"/commits?"+q.Encode(), &rows); err != nil {
			return GitHubChanges{}, err
		}
		if len(rows) <= ChangeRowsLimit {
			total := len(rows)
			report.Total = &total
		}
	}
	if len(rows) > ChangeRowsLimit {
		rows = rows[:ChangeRowsLimit]
		report.Complete = false
	}
	report.Commits = make([]ChangeCommit, 0, len(rows))
	for _, row := range rows {
		if !gitObjectID.MatchString(row.SHA) || row.Commit.Committer.Date.IsZero() {
			return GitHubChanges{}, ErrComparisonMissing
		}
		message := strings.TrimSpace(row.Commit.Message)
		if runes := []rune(message); len(runes) > ChangeEvidenceRunesLimit {
			message = string(runes[:ChangeEvidenceRunesLimit-1]) + "…"
		}
		report.Commits = append(report.Commits, ChangeCommit{SHA: row.SHA, At: row.Commit.Committer.Date,
			Message: message, URL: web + "/commit/" + row.SHA})
	}
	return report, nil
}
