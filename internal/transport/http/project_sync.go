package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	psync "github.com/sainteye/clawdline/internal/adapters/projectsync"
	"github.com/sainteye/clawdline/internal/domain/icon"
	domain "github.com/sainteye/clawdline/internal/domain/projectsync"
)

// CloneRootEnv overrides where a mirror clones a repository it does not have.
const CloneRootEnv = "CLAWDLINE_PROJECTS_ROOT"

// syncPlaces is how many places a manifest is built from. The start sheet
// shows forty; a manifest is every project this machine knows.
const syncPlaces = 256

// newProjectSync joins the mirror to this daemon's places and icons.
func (s *Server) newProjectSync(mirror *domain.Mirror) *psync.Service {
	return &psync.Service{
		Mirror:    mirror,
		Checkouts: s.syncCheckouts,
		Icon:      s.icons.For,
		CloneRoot: s.cloneRoot,
		Register: func(path string) error {
			_, err := s.projectReaders().registry.Add([]string{path}, time.Now())
			return err
		},
	}
}

// syncCheckouts are this machine's project roots: every place, folded onto
// the repository it belongs to, so a worktree is not a second project.
func (s *Server) syncCheckouts(ctx context.Context) []psync.Checkout {
	readers := s.projectReaders()
	list := readers.places.List(s.liveDirectories(ctx), syncPlaces)
	seen := map[string]bool{}
	var out []psync.Checkout
	for _, p := range list {
		root, ok := projects.CanonicalProjectKey(p.Path)
		if !ok || seen[root] {
			continue
		}
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue
		}
		seen[root] = true
		label := s.icons.Label(root)
		if label == "" {
			label = filepath.Base(root)
		}
		out = append(out, psync.Checkout{Path: root, Label: label})
	}
	return out
}

// cloneRoot is where a repository this machine lacks is cloned: the
// environment's choice, else the directory most of this machine's projects
// already sit in, else ~/projects.
func (s *Server) cloneRoot() string {
	if dir := os.Getenv(CloneRootEnv); dir != "" && filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	counts := map[string]int{}
	for _, c := range s.syncCheckouts(context.Background()) {
		counts[filepath.Dir(c.Path)]++
	}
	best, most := "", 0
	parents := make([]string, 0, len(counts))
	for p := range counts {
		parents = append(parents, p)
	}
	sort.Strings(parents)
	for _, p := range parents {
		if counts[p] > most {
			best, most = p, counts[p]
		}
	}
	if best != "" {
		return best
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "projects")
}

// projectSyncRoute is the source's offer and the mirror's own record:
//
//	GET    /v1/project-sync/manifest            what this machine offers
//	GET    /v1/project-sync/entry?repo=…        one offered project, with contents
//	GET    /v1/project-sync/mirror              what this machine mirrors
//	POST   /v1/project-sync/mirror              apply one project from a source
//	DELETE /v1/project-sync/mirror?repo=…       make a mirrored project local again
func (s *Server) projectSyncRoute(w http.ResponseWriter, r *http.Request) {
	if s.projectSync == nil {
		writeRefusal(w, 503, "project_sync_unavailable", "Project settings sync is not available on this machine.")
		return
	}
	switch strings.TrimPrefix(routePath(r), "/v1/project-sync/") {
	case "manifest":
		if r.Method != http.MethodGet {
			writeRefusal(w, 405, "method_not_allowed", "GET only")
			return
		}
		if r.URL.RawQuery != "" {
			writeRefusal(w, 400, "bad_request", "The manifest takes no query fields.")
			return
		}
		writeJSON(w, s.projectSync.Manifest(r.Context()))
	case "entry":
		if r.Method != http.MethodGet {
			writeRefusal(w, 405, "method_not_allowed", "GET only")
			return
		}
		repo, ok := onlyRepo(w, r)
		if !ok {
			return
		}
		entry, err := s.projectSync.Entry(r.Context(), repo)
		if errors.Is(err, psync.ErrNotOffered) {
			writeRefusal(w, 404, "project_not_offered", "This machine does not offer "+repo+".")
			return
		}
		if err != nil {
			writeRefusal(w, 503, "project_unreadable", err.Error())
			return
		}
		writeJSON(w, map[string]any{"project": entry})
	case "mirror":
		switch r.Method {
		case http.MethodGet:
			if r.URL.RawQuery != "" {
				writeRefusal(w, 400, "bad_request", "The mirror takes no query fields.")
				return
			}
			writeJSON(w, s.projectSync.State())
		case http.MethodPost:
			s.projectMirrorApply(w, r)
		case http.MethodDelete:
			if !maySend(r) {
				writeRefusal(w, 403, "forbidden", "This device may read, and not send.")
				return
			}
			repo, ok := onlyRepo(w, r)
			if !ok {
				return
			}
			removed, err := s.projectSync.Detach(repo)
			if err != nil {
				writeRefusal(w, 503, "mirror_store_unavailable", "The mirror could not be saved. Try again.")
				return
			}
			writeJSON(w, map[string]any{"ok": true, "repo": repo, "removed": removed})
		default:
			writeRefusal(w, 405, "method_not_allowed", "GET, POST or DELETE")
		}
	default:
		writeRefusal(w, 404, "not_found", "No such project sync route.")
	}
}

func onlyRepo(w http.ResponseWriter, r *http.Request) (string, bool) {
	q := r.URL.Query()
	repo := q.Get("repo")
	if len(q) != 1 || len(q["repo"]) != 1 || !domain.ValidRepo(repo) {
		writeRefusal(w, 400, "bad_request", "Name one repository as ?repo=host/owner/name.")
		return "", false
	}
	return repo, true
}

func (s *Server) projectMirrorApply(w http.ResponseWriter, r *http.Request) {
	if !maySend(r) {
		writeRefusal(w, 403, "forbidden", "This device may read, and not send.")
		return
	}
	if r.URL.RawQuery != "" {
		writeRefusal(w, 400, "bad_request", "An apply takes no query fields.")
		return
	}
	var req psync.ApplyRequest
	r.Body = http.MaxBytesReader(w, r.Body, domain.MaxEntryBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeRefusal(w, 413, "body_too_large", "One project's settings exceed 4 MiB.")
		} else {
			writeRefusal(w, 400, "invalid_project", "Supply a source, a project and whether to clone it.")
		}
		return
	}
	if dec.Decode(new(any)) != io.EOF {
		writeRefusal(w, 400, "invalid_project", "Supply one JSON object.")
		return
	}
	if len(req.Source.Machine) > 256 || len(req.Source.Name) > 200 {
		writeRefusal(w, 422, "invalid_project", "The source is named in at most 200 bytes.")
		return
	}
	result, err := s.projectSync.Apply(r.Context(), req)
	switch {
	case errors.Is(err, psync.ErrSourceMismatch):
		writeRefusal(w, 409, "mirror_source_mismatch", err.Error())
	case errors.Is(err, psync.ErrCloneTarget):
		writeRefusal(w, 409, "clone_target_exists", err.Error())
	case errors.Is(err, psync.ErrCloneBusy):
		writeRefusal(w, 429, "clone_busy", err.Error())
	case errors.Is(err, domain.ErrMirrorCapacity):
		writeRefusal(w, 409, "mirror_capacity", err.Error())
	case errors.Is(err, psync.ErrInvalid):
		writeRefusal(w, 422, "invalid_project", err.Error())
	case errors.Is(err, psync.ErrNoCloneRoot):
		writeRefusal(w, 409, "clone_root_unavailable", err.Error())
	case err != nil:
		writeRefusal(w, 503, "mirror_store_unavailable", err.Error())
	default:
		writeJSON(w, map[string]any{"ok": true, "result": result})
	}
}

// mirrorLookup is the icon registry's view of the mirror.
func mirrorLookup(m *domain.Mirror) func(string) (icon.Grid, string, bool) {
	return func(cwd string) (icon.Grid, string, bool) {
		rec, ok := m.Lookup(cwd)
		if !ok {
			return icon.Grid{}, "", false
		}
		return rec.Icon, rec.Label, true
	}
}
