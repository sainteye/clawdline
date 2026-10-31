package http

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/jiraissues"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func (s *Server) jiraStore() jiraissues.Store {
	return jiraissues.Store{Dir: filepath.Join(s.cfg.Dir, "integrations", "jira")}
}

func (s *Server) jiraClient() *jiraissues.Client {
	s.githubMu.Lock()
	defer s.githubMu.Unlock()
	if s.jiraHTTP == nil {
		s.jiraHTTP = jiraissues.NewClient()
	}
	return s.jiraHTTP
}

// jiraStatus never carries the token.
type jiraStatus struct {
	State   string `json:"state"`
	Site    string `json:"site,omitempty"`
	Email   string `json:"email,omitempty"`
	Account string `json:"account,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (s *Server) jiraStatus() jiraStatus {
	cred, err := s.jiraStore().Load()
	switch {
	case err == nil:
		return jiraStatus{State: "connected", Site: cred.Site, Email: cred.Email, Account: cred.Account}
	case errors.Is(err, jiraissues.ErrNotConnected):
		return jiraStatus{State: "disconnected"}
	default:
		return jiraStatus{State: "failed", Error: "jira_credential_unreadable"}
	}
}

// jiraIntegrationRoute reads (GET), connects (POST {site, email, token})
// and forgets (DELETE) this machine's Jira Cloud sign-in. A POST is checked
// against Jira before it is saved, so a wrong token is never kept.
func (s *Server) jiraIntegrationRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	case http.MethodDelete:
		if err := s.jiraStore().Delete(); err != nil {
			writeRefusal(w, http.StatusInternalServerError, "jira_credential_unwritable", "The Jira sign-in could not be removed from this machine.")
			return
		}
	case http.MethodPost:
		if contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || contentType != "application/json" {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "A Jira connection is application/json.")
			return
		}
		var body struct {
			Site  string `json:"site"`
			Email string `json:"email"`
			Token string `json:"token"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
		if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "That body is not a Jira connection.")
			return
		}
		cred := jiraissues.Credential{Site: strings.TrimRight(strings.TrimSpace(strings.ToLower(body.Site)), "/"),
			Email: strings.TrimSpace(body.Email), Token: strings.TrimSpace(body.Token)}
		probe := work.IssueTarget{Provider: work.IssueProviderJira, JiraSite: cred.Site, JiraProject: "AB", JiraIssueType: "Task"}
		if probe.Validate() != nil || cred.Email == "" || cred.Token == "" {
			writeRefusal(w, http.StatusBadRequest, "jira_connection_invalid", "Enter a Jira Cloud site (https://<name>.atlassian.net), your account email and an API token.")
			return
		}
		account, err := s.jiraClient().Myself(r.Context(), cred)
		if err != nil {
			writeJiraFailure(w, err)
			return
		}
		cred.Account = account
		if err := s.jiraStore().Save(cred); err != nil {
			writeRefusal(w, http.StatusInternalServerError, "jira_credential_unwritable", "The Jira sign-in could not be saved on this machine.")
			return
		}
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET, POST or DELETE")
		return
	}
	writeJSON(w, s.jiraStatus())
}

// jiraProjectsRoute lists the projects and issue types the target picker
// offers.
func (s *Server) jiraProjectsRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET")
		return
	}
	cred, err := s.jiraStore().Load()
	if err != nil {
		writeRefusal(w, http.StatusConflict, "issue_sync_authorization_required", "Connect Jira on this machine first.")
		return
	}
	projects, err := s.jiraClient().Projects(r.Context(), cred)
	if err != nil {
		writeJiraFailure(w, err)
		return
	}
	writeJSON(w, map[string]any{"site": cred.Site, "projects": projects})
}

func writeJiraFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, jiraissues.ErrUnauthorized):
		writeRefusal(w, http.StatusBadRequest, "jira_unauthorized", "Jira did not accept that email and API token.")
	case errors.Is(err, jiraissues.ErrRateLimited):
		writeRefusal(w, http.StatusTooManyRequests, "jira_rate_limited", "Jira asked Clawdline to wait. Try again in a minute.")
	case errors.Is(err, jiraissues.ErrTargetNotFound):
		writeRefusal(w, http.StatusBadRequest, "jira_site_not_found", "That Jira site did not answer as a Jira Cloud site.")
	default:
		writeRefusal(w, http.StatusBadGateway, "jira_unreachable", "Jira did not answer. Try again.")
	}
}
