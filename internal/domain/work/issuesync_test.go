package work

import (
	"strings"
	"testing"
)

func TestAnIssueIsOpenedAtImplementationAndClosedAtTheEnd(t *testing.T) {
	cases := []struct {
		name     string
		phase    Phase
		observed IssueObserved
		want     IssueOp
	}{
		{"an item only created leaves the tracker alone", PhaseCreated, IssueNotCreated, IssueOpNone},
		{"an item only assigned leaves the tracker alone", PhaseAssigned, IssueNotCreated, IssueOpNone},
		{"implementation opens the issue", PhaseImplementing, IssueNotCreated, IssueOpCreate},
		{"a later phase still opens a missing issue", PhaseVerifying, IssueNotCreated, IssueOpCreate},
		{"an open issue stays open while the work goes on", PhaseMerging, IssueOpen, IssueOpNone},
		{"done closes it as completed", PhaseDone, IssueOpen, IssueOpCloseCompleted},
		{"cancelled closes it as not planned", PhaseCancelled, IssueOpen, IssueOpCloseNotPlanned},
		{"an item closed before implementation opens nothing", PhaseDone, IssueNotCreated, IssueOpNone},
		{"an item cancelled before implementation opens nothing", PhaseCancelled, IssueNotCreated, IssueOpNone},
		{"a reopened item reopens its issue", PhaseAssigned, IssueClosed, IssueOpReopen},
		{"a closed issue is not closed again", PhaseCancelled, IssueClosed, IssueOpNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NextIssueOp(c.phase, c.observed); got != c.want {
				t.Fatalf("NextIssueOp(%s, %q) = %q, want %q", c.phase, c.observed, got, c.want)
			}
		})
	}
}

func TestSyncCannotBeSentWithoutAnAuthorization(t *testing.T) {
	gh := IssueTarget{Provider: IssueProviderGitHub, Repo: "octo-org/widgets"}
	err := IssueSyncGate(gh, false)
	if r, ok := AsRefusalV2(err); !ok || r.Code != "issue_sync_authorization_required" {
		t.Fatalf("unauthorized sync: %v, want issue_sync_authorization_required", err)
	}
	if err := IssueSyncGate(gh, true); err != nil {
		t.Fatalf("authorized sync refused: %v", err)
	}
	// An invalid target is refused for what it is, not as a missing authorization.
	err = IssueSyncGate(IssueTarget{Provider: IssueProviderGitHub, Repo: "widgets"}, false)
	if r, ok := AsRefusalV2(err); !ok || r.Code != "issue_sync_target_invalid" {
		t.Fatalf("invalid target: %v, want issue_sync_target_invalid", err)
	}
}

func TestOnlyAnAddressableTargetIsAccepted(t *testing.T) {
	jira := IssueTarget{Provider: IssueProviderJira, JiraSite: "https://acme.atlassian.net", JiraProject: "OPS", JiraIssueType: "Task"}
	good := []IssueTarget{
		{Provider: IssueProviderGitHub, Repo: "octo-org/widgets"},
		{Provider: IssueProviderGitHub, Repo: "a/.github"},
		jira,
	}
	for _, g := range good {
		if err := g.Validate(); err != nil {
			t.Errorf("%+v refused: %v", g, err)
		}
	}
	with := func(f func(*IssueTarget)) IssueTarget { c := jira; f(&c); return c }
	bad := map[string]IssueTarget{
		"no provider":           {Repo: "octo-org/widgets"},
		"repo without owner":    {Provider: IssueProviderGitHub, Repo: "widgets"},
		"owner with edge dash":  {Provider: IssueProviderGitHub, Repo: "-octo/widgets"},
		"owner too long":        {Provider: IssueProviderGitHub, Repo: strings.Repeat("a", 40) + "/w"},
		"repo is a dot":         {Provider: IssueProviderGitHub, Repo: "octo/.."},
		"nested path":           {Provider: IssueProviderGitHub, Repo: "octo/widgets/issues"},
		"github with jira":      {Provider: IssueProviderGitHub, Repo: "octo/widgets", JiraProject: "OPS"},
		"jira data center site": with(func(t *IssueTarget) { t.JiraSite = "https://jira.acme.example" }),
		"jira over http":        with(func(t *IssueTarget) { t.JiraSite = "http://acme.atlassian.net" }),
		"jira lowercase key":    with(func(t *IssueTarget) { t.JiraProject = "ops" }),
		"jira without type":     with(func(t *IssueTarget) { t.JiraIssueType = " " }),
		"jira with a repo":      with(func(t *IssueTarget) { t.Repo = "octo/widgets" }),
	}
	for name, b := range bad {
		if r, ok := AsRefusalV2(b.Validate()); !ok || r.Code != "issue_sync_target_invalid" {
			t.Errorf("%s: %+v accepted or refused wrongly", name, b)
		}
	}
}

func TestTheMarkerNamesExactlyOneItem(t *testing.T) {
	a := IssueMarker("2f0c6b9e-0000-4000-8000-000000000001")
	b := IssueMarker("2f0c6b9e-0000-4000-8000-000000000002")
	if a == b || !strings.HasPrefix(a, "<!--") || !strings.HasSuffix(a, "-->") {
		t.Fatalf("markers %q and %q", a, b)
	}
}
