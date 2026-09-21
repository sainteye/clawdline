package http

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectlinks"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// linksPath recognises GET /v1/sessions/{id}/links and returns the id.
func linksPath(r *http.Request) (string, bool) {
	return sessionVerbIs(r, "links", http.MethodGet)
}

// projectLinksReader is built per walk rather than held, and the walk happens
// at most once per directory per FreshFor — so what this costs is nothing, and
// what it buys is that the home directory and the status directory are the
// ones in force now. A reader held from the first request onwards would answer
// every later one from wherever that first one happened to be.
func projectLinksReader() *projectlinks.Reader {
	home, _ := os.UserHomeDir()
	config := swiftstore.OpenQuotaConfig(swiftstore.Dir())
	reader := projectlinks.NewReader(home, func() string { return config.Read().StatusDir })
	// Where a project says its health check lives. The result of that check is
	// a fact about this minute and comes from the status cache; the endpoint
	// is a fact about the project and comes from here.
	reader.Registry = projects.RegistryRow
	return reader
}

// sessionLinksRoute answers the Links sheet: everything this project has an
// address for (links.schema.json).
//
// **A route rather than a field on the session list.** The list goes out on
// the event stream every time anything moves, and working these out costs a
// `git` invocation and a handful of file reads per project. Opening a sheet is
// rare and paying for it then is cheap; paying for it on every beat of the
// stream is a subprocess per session per second — which is the sentence the
// Swift route carries and the reason `GET /v1/sessions` stays at its
// hundredth of a second.
func (s *Server) sessionLinksRoute(w http.ResponseWriter, r *http.Request, id string) {
	if !s.ownsSessions() {
		s.forwardUpstream(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	item, err := s.actions().Find(ctx, id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	// No working directory is this daemon not knowing where the session is —
	// the Swift route's 404 — and not a project with no addresses.
	if strings.TrimSpace(item.CWD) == "" {
		writeRefusal(w, http.StatusNotFound, "not_found",
			"Could not find that session's working directory")
		return
	}
	reading, at := s.projectLinks(ctx, item.CWD)
	reply := contract.ProjectLinksReply{
		Links:       wireLinks(reading.Links),
		ObservedAt:  seconds(at),
		Repository:  contract.ProjectRepository(reading.Repo),
		Unreadable:  contract.ProjectGitFailure(reading.Unreadable),
		DeployQuiet: wireDeployQuiet(reading.DeployQuiet),
		Truncated:   reading.Truncated,
	}
	writeJSON(w, reply)
}

// projectLinks is the one way in to the projection, used by this route and by
// `/info`. A server built without the cache reads directly, which is what a
// test that constructed a bare Server gets.
func (s *Server) projectLinks(ctx context.Context, cwd string) (projectlinks.Reading, time.Time) {
	read := func(ctx context.Context) projectlinks.Reading {
		return projectLinksReader().Read(ctx, cwd)
	}
	if s.links == nil {
		return read(ctx), time.Now()
	}
	return s.links.Get(ctx, cwd, read)
}

// wireLinks is a walk's rows as the wire spells them.
func wireLinks(rows []projectlinks.Link) []contract.ProjectLink {
	out := make([]contract.ProjectLink, 0, len(rows))
	for _, row := range rows {
		out = append(out, contract.ProjectLink{
			Label: row.Label, URL: row.URL, Kind: row.Kind,
			State: row.State, Status: row.Status, Why: row.Why, Local: row.Local,
			StartedAt: row.StartedAt, TypicalSeconds: row.TypicalSeconds,
			Phase:         row.Phase,
			UnknownReason: contract.ProjectServerUnknown(row.UnknownReason),
		})
	}
	return out
}

// wireDeployQuiet carries the reason a repository on GitHub still has no
// deploy row. Nothing is translated on the way: `state` and `why` are the
// producer's own words and the screen owns the sentence, which is what keeps a
// word that tool learns tomorrow from arriving as another blank cell.
func wireDeployQuiet(quiet *projectlinks.DeployQuiet) *contract.ProjectDeployQuiet {
	if quiet == nil {
		return nil
	}
	return &contract.ProjectDeployQuiet{
		Kind:      contract.ProjectDeployQuietKind(quiet.Kind),
		State:     quiet.State,
		Why:       quiet.Why,
		UpdatedAt: quiet.UpdatedAt,
	}
}

// deployRows is the Swift app's `/info` filter: the `links` rows a status line
// draws a chip from, unchanged, so a state means there what it means in the
// sheet.
func deployRows(rows []contract.ProjectLink) []contract.ProjectLink {
	out := make([]contract.ProjectLink, 0, len(rows))
	for _, row := range rows {
		switch row.Kind {
		case "deploy", "ci", "run":
			out = append(out, row)
		}
	}
	return out
}

func seconds(at time.Time) float64 {
	return float64(at.UnixMilli()) / 1000
}

// linksReading is the `cache.session_links` row. A server built without the
// projection has walked nobody's directory, which is a known zero.
func (s *Server) linksReading() capacity.Reading {
	if s.links == nil {
		return capacity.Reading{Known: true, Note: "nothing has asked where a project can be opened"}
	}
	return s.links.Reading()
}
