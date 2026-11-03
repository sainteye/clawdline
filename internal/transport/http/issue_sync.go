package http

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	gitadapter "github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/githubissues"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/issuesync"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// issueSyncRequest is the person's choice on the create and assign forms.
// A GitHub target is the Project's own repository; a Jira target is a
// project and issue type on the site this machine is connected to.
type issueSyncRequest struct {
	Provider      string `json:"provider"`
	JiraProject   string `json:"jira_project"`
	JiraIssueType string `json:"jira_issue_type"`
}

// issueSyncWire is what a card shows. State is waiting (no issue yet),
// open, closed, or failed (the last call failed; it is tried again).
type issueSyncWire struct {
	Provider    string `json:"provider"`
	Repo        string `json:"repo,omitempty"`
	JiraSite    string `json:"jira_site,omitempty"`
	JiraProject string `json:"jira_project,omitempty"`
	State       string `json:"state"`
	ExternalKey string `json:"external_key,omitempty"`
	URL         string `json:"url,omitempty"`
	Error       string `json:"error,omitempty"`
	RetryAt     *int64 `json:"retry_at,omitempty"`
}

func issueSyncWireOf(l *store.IssueLink) *issueSyncWire {
	if l == nil {
		return nil
	}
	out := &issueSyncWire{Provider: string(l.Target.Provider), Repo: l.Target.Repo, JiraSite: l.Target.JiraSite,
		JiraProject: l.Target.JiraProject, ExternalKey: l.ExternalKey, URL: l.URL, Error: l.LastError}
	switch {
	case l.LastError != "":
		out.State = "failed"
		if !l.NextAttemptAt.IsZero() {
			at := l.NextAttemptAt.Unix()
			out.RetryAt = &at
		}
	case l.Observed == work.IssueOpen:
		out.State = "open"
	case l.Observed == work.IssueClosed:
		out.State = "closed"
	default:
		out.State = "waiting"
	}
	return out
}

// issueSyncTarget resolves the person's choice for a Project and applies
// IssueSyncGate. It answers nil, true when nothing was chosen. Without an
// authorization it refuses with issue_sync_authorization_required and
// writes nothing: the Console authorizes and sends the same request again.
func (s *Server) issueSyncTarget(w http.ResponseWriter, r *http.Request, projectPath string, req *issueSyncRequest) (*work.IssueTarget, bool) {
	if req == nil {
		return nil, true
	}
	var target work.IssueTarget
	authorized := false
	switch work.IssueProvider(req.Provider) {
	case work.IssueProviderGitHub:
		t, err := githubissues.TargetFor(r.Context(), gitadapter.New(), projectPath)
		if err != nil {
			writeRefusal(w, http.StatusConflict, err.Error(), issueSyncRemoteMessage(err))
			return nil, false
		}
		target, authorized = t, s.githubIntegration().Authorized()
	case work.IssueProviderJira:
		cred, err := s.jiraStore().Load()
		if err != nil {
			writeRefusal(w, http.StatusConflict, "issue_sync_authorization_required", "Connect Jira on this machine before syncing this item.")
			return nil, false
		}
		target = work.IssueTarget{Provider: work.IssueProviderJira, JiraSite: cred.Site,
			JiraProject: strings.TrimSpace(req.JiraProject), JiraIssueType: strings.TrimSpace(req.JiraIssueType)}
		authorized = true
	default:
		writeRefusal(w, http.StatusBadRequest, "issue_sync_target_invalid", "Issue sync is GitHub or Jira.")
		return nil, false
	}
	if err := work.IssueSyncGate(target, authorized); err != nil {
		var refusal work.RefusalV2
		errors.As(err, &refusal)
		status := http.StatusBadRequest
		if refusal.Code == "issue_sync_authorization_required" {
			status = http.StatusConflict
		}
		writeRefusal(w, status, refusal.Code, refusal.Message)
		return nil, false
	}
	return &target, true
}

func issueSyncRemoteMessage(err error) string {
	switch {
	case errors.Is(err, githubissues.ErrNoRemote):
		return "This Project's repository has no origin remote to open GitHub issues in."
	case errors.Is(err, githubissues.ErrNoRepository):
		return "This Project is not a Git repository."
	case errors.Is(err, githubissues.ErrRemoteNotGitHub):
		return "This Project's origin is not on github.com."
	}
	return "This Project's Git remote could not be read."
}

// issueSyncStore adapts the store's link rows to the reconciler.
type issueSyncStore struct{ s *store.Store }

func (a issueSyncStore) PendingIssueLinks(ctx context.Context, now time.Time, limit int) ([]issuesync.Due, error) {
	rows, err := a.s.PendingIssueLinks(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	out := make([]issuesync.Due, 0, len(rows))
	for _, d := range rows {
		l := d.Link
		out = append(out, issuesync.Due{Title: d.Title, Description: d.Description, Phase: d.Phase,
			Link: issuesync.Link{ItemID: l.WorkID, Target: l.Target, Observed: l.Observed, ExternalKey: l.ExternalKey,
				URL: l.URL, LastError: l.LastError, Attempts: l.Attempts, NextAttemptAt: l.NextAttemptAt}})
	}
	return out, nil
}

func (a issueSyncStore) RecordIssueSynced(ctx context.Context, id string, r issuesync.Result, at time.Time) error {
	return a.s.RecordIssueSynced(ctx, id, r.Observed, r.ExternalKey, r.URL, at)
}

func (a issueSyncStore) RecordIssueFailed(ctx context.Context, id, code string, attempts int, next, at time.Time) error {
	return a.s.RecordIssueFailed(ctx, id, code, attempts, next, at)
}

// issueSyncInterval is how often the reconciler looks without being woken:
// it is what catches an agent's phase change, which nothing wakes it for.
const issueSyncInterval = 15 * time.Second

type issueSyncWorker struct {
	once sync.Once
	wake chan struct{}
}

func (s *Server) issueSyncWorker() *issueSyncWorker {
	s.githubMu.Lock()
	defer s.githubMu.Unlock()
	if s.issueSync == nil {
		s.issueSync = &issueSyncWorker{wake: make(chan struct{}, 1)}
	}
	return s.issueSync
}

// wakeIssueSync asks for a pass now, after a person linked an item.
func (s *Server) wakeIssueSync() {
	select {
	case s.issueSyncWorker().wake <- struct{}{}:
	default:
	}
}

func (s *Server) issueReconciler() *issuesync.Reconciler {
	conn := s.githubIntegration()
	return &issuesync.Reconciler{Store: issueSyncStore{s.store}, Trackers: map[work.IssueProvider]issuesync.Tracker{
		work.IssueProviderGitHub: issuesync.GitHubTracker{Conn: conn, Client: conn.Client()},
		work.IssueProviderJira:   issuesync.JiraTracker{Store: s.jiraStore(), Client: s.jiraClient()},
	}}
}

// StartIssueSync runs the reconciler until ctx ends.
func (s *Server) StartIssueSync(ctx context.Context) func() {
	worker := s.issueSyncWorker()
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	worker.once.Do(func() {
		r := s.issueReconciler()
		go func() {
			defer close(done)
			ticker := time.NewTicker(issueSyncInterval)
			defer ticker.Stop()
			for {
				if _, err := r.RunOnce(ctx); err != nil && ctx.Err() == nil {
					log.Printf("issue sync: pass failed: %v", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				case <-worker.wake:
				}
			}
		}()
	})
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}

// issueSyncRoute is POST /v1/issue-sync/<item>/retry: the person's Retry on
// a failed link, which makes it due now.
func (s *Server) issueSyncRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(routePath(r), "/v1/issue-sync/")
	id, action, ok := strings.Cut(rest, "/")
	if !ok || action != "retry" || id == "" {
		writeRefusal(w, http.StatusNotFound, "not_found", "POST /v1/issue-sync/<item>/retry")
		return
	}
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST")
		return
	}
	found, err := s.store.RetryIssueLink(r.Context(), id, time.Now())
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The retry could not be recorded.")
		return
	}
	if !found {
		writeRefusal(w, http.StatusNotFound, "issue_sync_not_linked", "This item is not synced to an issue.")
		return
	}
	s.wakeIssueSync()
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{"accepted": true})
}
