package work

import (
	"regexp"
	"strings"
)

// Issue sync (docs/issue-sync.md, D73) mirrors a Board item into one external
// tracker in one direction. The person opts in when creating or assigning the
// item; the daemon opens the issue when the item reaches implementing and
// closes it when the item is done or cancelled. Nothing read from the tracker
// ever changes the item: the tracker's state is an echo, not evidence (D01).

type IssueProvider string

const (
	IssueProviderGitHub IssueProvider = "github"
	IssueProviderJira   IssueProvider = "jira"
)

func (p IssueProvider) Valid() bool { return p == IssueProviderGitHub || p == IssueProviderJira }

// IssueTarget is where an item's issue lives. A GitHub target names one
// repository; a Jira target names one Jira Cloud site, project and issue type.
type IssueTarget struct {
	Provider      IssueProvider
	Repo          string // GitHub "owner/repo"
	JiraSite      string // "https://<name>.atlassian.net"
	JiraProject   string // project key, e.g. "OPS"
	JiraIssueType string // e.g. "Task"
}

// GitHub's own naming rules: an owner is 1-39 alphanumerics or single inner
// hyphens; a repository is 1-100 of [A-Za-z0-9._-] and is never "." or "..".
var (
	githubOwnerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9]|-(?:[A-Za-z0-9])){0,38}$`)
	githubRepoPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	jiraSitePattern    = regexp.MustCompile(`^https://[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.atlassian\.net$`)
	jiraProjectPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,9}$`)
)

// Validate refuses a target the daemon could not address. Only Jira Cloud is
// supported: Data Center sites and their tokens are a later step.
func (t IssueTarget) Validate() error {
	switch t.Provider {
	case IssueProviderGitHub:
		owner, repo, ok := strings.Cut(t.Repo, "/")
		if !ok || !githubOwnerPattern.MatchString(owner) || !githubRepoPattern.MatchString(repo) ||
			repo == "." || repo == ".." {
			return RefuseV2("issue_sync_target_invalid", "A GitHub target is owner/repo.")
		}
		if t.JiraSite != "" || t.JiraProject != "" || t.JiraIssueType != "" {
			return RefuseV2("issue_sync_target_invalid", "A GitHub target carries no Jira fields.")
		}
	case IssueProviderJira:
		if !jiraSitePattern.MatchString(t.JiraSite) {
			return RefuseV2("issue_sync_target_invalid", "A Jira target is a Jira Cloud site, https://<name>.atlassian.net.")
		}
		if !jiraProjectPattern.MatchString(t.JiraProject) {
			return RefuseV2("issue_sync_target_invalid", "A Jira project key is an uppercase letter followed by 1-9 uppercase letters, digits or underscores.")
		}
		if strings.TrimSpace(t.JiraIssueType) == "" || len([]rune(t.JiraIssueType)) > 60 {
			return RefuseV2("issue_sync_target_invalid", "A Jira target names its issue type.")
		}
		if t.Repo != "" {
			return RefuseV2("issue_sync_target_invalid", "A Jira target carries no GitHub repository.")
		}
	default:
		return RefuseV2("issue_sync_target_invalid", "Issue sync is GitHub or Jira.")
	}
	return nil
}

// IssueSyncGate is the person's rule for turning sync on: the option can be
// sent only for a target this machine already holds an authorization for.
// The Console answers the refusal by running the authorization and sending
// the same request again, so an item is never created waiting for one.
func IssueSyncGate(t IssueTarget, authorized bool) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if !authorized {
		return RefuseV2("issue_sync_authorization_required",
			"Connect "+t.Provider.Label()+" for this Project before syncing this item.")
	}
	return nil
}

func (p IssueProvider) Label() string {
	if p == IssueProviderJira {
		return "Jira"
	}
	return "GitHub"
}

// IssueObserved is what the daemon last recorded about the external issue.
type IssueObserved string

const (
	IssueNotCreated IssueObserved = ""
	IssueOpen       IssueObserved = "open"
	IssueClosed     IssueObserved = "closed"
)

// IssueOp is the one call the outbox owes the tracker to match the item.
type IssueOp string

const (
	IssueOpNone            IssueOp = ""
	IssueOpCreate          IssueOp = "create"
	IssueOpCloseCompleted  IssueOp = "close_completed"
	IssueOpCloseNotPlanned IssueOp = "close_not_planned"
	IssueOpReopen          IssueOp = "reopen"
)

// NextIssueOp compares an item's phase with the recorded external issue.
//
// The issue is created at the first implementing: an item that is only
// assigned, or that is closed without ever reaching implementation, leaves
// nothing behind in the tracker. Once it exists, any open phase keeps it
// open (a reopened item reopens it), done closes it as completed and
// cancelled as not planned. A closed issue is never closed again for the
// other reason, so the tracker's history does not churn.
func NextIssueOp(phase Phase, observed IssueObserved) IssueOp {
	switch observed {
	case IssueNotCreated:
		if issuePhaseStarted(phase) {
			return IssueOpCreate
		}
	case IssueOpen:
		switch phase {
		case PhaseDone:
			return IssueOpCloseCompleted
		case PhaseCancelled:
			return IssueOpCloseNotPlanned
		}
	case IssueClosed:
		if !phase.Terminal() {
			return IssueOpReopen
		}
	}
	return IssueOpNone
}

func issuePhaseStarted(p Phase) bool {
	return p == PhaseImplementing || p == PhaseVerifying || p == PhaseMerging || p == PhaseDeploying
}

// IssueMarker is written into the issue body so that a retried create finds
// the issue an earlier, unacknowledged attempt already opened instead of
// opening a second one. Neither tracker accepts an idempotency key.
func IssueMarker(itemID string) string { return "<!-- clawdline:item:" + itemID + " -->" }
