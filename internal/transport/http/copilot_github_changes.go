package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	gitadapter "github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/githubissues"
)

const copilotGitHubChangesPath = "/v1/voice/copilot-github-changes"
const maxCopilotGitHubChangesDuration = 12 * time.Second

func (s *Server) copilotGitHubChangesRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET")
		return
	}
	q := r.URL.Query()
	sessionID, conversation := q.Get("session"), q.Get("conversation")
	if sessionID == "" || conversation == "" {
		writeRefusal(w, http.StatusBadRequest, "copilot_github_session_required", "Open or choose a Session to report on its GitHub repository.")
		return
	}
	var window githubissues.ChangeWindow
	switch q.Get("range") {
	case "release":
		if q.Get("since") != "" || q.Get("until") != "" {
			writeCopilotGitHubDateRefusal(w)
			return
		}
	case "dates":
		since, fromErr := time.Parse(time.RFC3339, q.Get("since"))
		until, toErr := time.Parse(time.RFC3339, q.Get("until"))
		if fromErr != nil || toErr != nil || since.Year() < 1970 || until.Year() > 2099 || since.After(until) {
			writeCopilotGitHubDateRefusal(w)
			return
		}
		window = githubissues.ChangeWindow{Since: since, Until: until}
	default:
		writeCopilotGitHubDateRefusal(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), maxCopilotGitHubChangesDuration)
	defer cancel()
	item, err := s.actions().FindForRead(ctx, sessionID)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	if item.ConversationID != conversation || item.DiscoveryExcluded {
		writeRefusal(w, http.StatusConflict, "copilot_github_session_changed", "The Session conversation changed. Choose it again.")
		return
	}
	if item.CWD == "" {
		writeRefusal(w, http.StatusConflict, "copilot_github_remote_missing", "This Session has no working directory with a GitHub origin.")
		return
	}
	target, err := githubissues.TargetFor(ctx, gitadapter.New(), item.CWD)
	if err != nil {
		if errors.Is(err, githubissues.ErrRemoteUnread) {
			writeRefusal(w, http.StatusServiceUnavailable, "copilot_github_remote_unreadable", "The Session's Git origin could not be read.")
		} else {
			writeRefusal(w, http.StatusConflict, "copilot_github_remote_missing", "This Session's working directory has no GitHub origin.")
		}
		return
	}
	conn := s.githubIntegration()
	var token string
	if conn.Authorized() {
		token, _ = conn.AccessToken(ctx)
	}
	report, err := conn.Client().RepositoryChanges(ctx, token, target.Repo, window)
	switch {
	case err == nil:
		writeJSON(w, report)
	case errors.Is(err, githubissues.ErrNoPublishedRelease):
		writeRefusal(w, http.StatusNotFound, err.Error(), "GitHub has no published stable release for this repository. Ask for a date range instead.")
	case errors.Is(err, githubissues.ErrReleaseOffBranch):
		writeRefusal(w, http.StatusConflict, err.Error(), "The latest release is outside the default branch history. Ask for a date range instead.")
	case errors.Is(err, githubissues.ErrComparisonMissing):
		writeRefusal(w, http.StatusBadGateway, err.Error(), "GitHub did not return a usable commit comparison.")
	case errors.Is(err, githubissues.ErrRateLimited):
		writeRefusal(w, http.StatusTooManyRequests, "github_rate_limited", "GitHub is limiting requests. Try again later.")
	case errors.Is(err, githubissues.ErrReauthorize), errors.Is(err, githubissues.ErrTargetNotFound), errors.Is(err, githubissues.ErrIssuesDisabled):
		writeRefusal(w, http.StatusForbidden, "copilot_github_access_required", "GitHub could not open this repository or its commits. Check the repository and the machine's GitHub connection.")
	default:
		writeRefusal(w, http.StatusBadGateway, "copilot_github_unavailable", "GitHub did not return the change history.")
	}
}

func writeCopilotGitHubDateRefusal(w http.ResponseWriter) {
	writeRefusal(w, http.StatusBadRequest, "copilot_github_invalid_dates", "Ask for changes since the latest release or a valid start and end date.")
}
