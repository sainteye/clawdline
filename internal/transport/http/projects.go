package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/icon"
)

// The Projects page's routes: the start places, the Project catalog, and one
// Project's worktree lifecycle. Every rule is the Swift app's and lives in
// internal/adapters/projects; this file only wires the readers to this
// daemon's inventory and its own task records, and encodes.

// projectReaders are built on first use and kept, because each one caches: the
// places remember which folder stands for which directory, and the
// lifecycle's plain read answers from the observation a refresh made.
type projectReaders struct {
	places    *projects.Places
	repos     *projects.RepoLookup
	setups    *setupLookup
	registry  *projects.PlaceRegistry
	lifecycle *projects.Lifecycle
	// busy bounds queued plus running lifecycle work, ProjectWorktreeHTTP.depth.
	busy chan struct{}
}

// projectReaderSets holds one set of readers per state directory, as
// scheduleBooks does: what they leave off the list depends on where this
// daemon's broker keeps its checkouts, and that is under its state directory.
var (
	projectMu         sync.Mutex
	projectReaderSets = map[string]*projectReaders{}
)

func (s *Server) projectReaders() *projectReaders {
	projectMu.Lock()
	defer projectMu.Unlock()
	if r, ok := projectReaderSets[s.cfg.Dir]; ok {
		return r
	}
	own := ""
	if s.broker != nil {
		own = s.broker.WorktreeRoot()
	}
	managed := projects.ManagedWorktreeRoots(own)
	registry := projects.OpenPlaceRegistry(s.cfg.Dir, swiftDirs()...)
	registry.SetLimit(CapacityLimit(capacity.PlacesRegistered))
	places := projects.NewPlaces(s.icons.Label, managed)
	places.MachineStateDir = s.cfg.Dir
	places.Registered = registry.List
	if s.cfg.Dir != "" {
		places.Hidden = registry.Hidden
	}
	r := &projectReaders{
		places:   places,
		repos:    projects.NewRepoLookup(projects.OriginRepo),
		setups:   newSetupLookup(projectSetup),
		registry: registry,
		busy:     make(chan struct{}, 4),
	}
	r.lifecycle = projects.NewLifecycle(projects.Ports{
		ManagedWorktreeRoots: managed,
		ProjectDirectories: func() []string {
			dirs := projects.RegistryPaths()
			for _, place := range places.List(s.liveDirectories(context.Background()), 40) {
				dirs = append(dirs, place.Path)
			}
			return append(dirs, s.taskProjectDirs()...)
		},
		Tasks: func() projects.TaskEvidence { return s.lifecycleEvidence() },
		Live:  func() projects.LiveEvidence { return s.liveEvidence(context.Background()) },
	})
	projectReaderSets[s.cfg.Dir] = r
	return r
}

// liveEvidence is the lifecycle's Live port: every assistant session's
// directory, and whether the reading was complete.
func (s *Server) liveEvidence(ctx context.Context) projects.LiveEvidence {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	inv := s.reading(ctx)
	out := projects.LiveEvidence{Complete: inv.Complete, ObservedAt: inv.ObservedAt}
	for _, item := range inv.Sessions {
		if item.IsAssistant() && item.CWD != "" && !projects.IsMachineWorkspace(s.cfg.Dir, item.CWD) {
			out.Sessions = append(out.Sessions, projects.LiveSession{TerminalID: item.ID,
				ConversationID: item.ConversationID, CWD: item.CWD})
		}
	}
	return out
}

func (s *Server) liveDirectories(ctx context.Context) []string {
	var cwds []string
	for _, l := range s.liveEvidence(ctx).Sessions {
		cwds = append(cwds, l.CWD)
	}
	return cwds
}

// installedAssistants is Assistant.available, approximated by where the two
// commands are found. Availability is quota, which this daemon does not read.
func installedAssistants() []contract.StartAssistant {
	found := func(name string) bool {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
		home, _ := os.UserHomeDir()
		for _, dir := range []string{home + "/.local/bin", home + "/.claude/local", "/opt/homebrew/bin", "/usr/local/bin"} {
			if st, err := os.Stat(filepath.Join(dir, name)); err == nil && !st.IsDir() {
				return true
			}
		}
		return false
	}
	out := []contract.StartAssistant{}
	for _, a := range []struct{ id, label string }{{"claude", "Claude Code"}, {"codex", "Codex"}} {
		if found(a.id) {
			out = append(out, contract.StartAssistant{ID: a.id, Label: a.label,
				Availability: contract.StartAvailabilityUnknown})
		}
	}
	out = append(out, antigravityStartAssistant(antigravityResolver().Snapshot(context.Background())))
	return out
}

var antigravityResolver = sync.OnceValue(func() *antigravity.Resolver {
	return antigravity.NewResolver(antigravity.DefaultCandidates(), nil)
})

func antigravityStartAssistant(info antigravity.Installation) contract.StartAssistant {
	state := contract.StartIntegrationStateMissing
	if info.Compatible {
		state = contract.StartIntegrationStateDisabled
	}
	if info.Installed && info.Reason == "provider_cli_incompatible" {
		state = contract.StartIntegrationStateIncompatible
	}
	var installed *bool
	if info.InstalledKnown {
		installed = &info.Installed
	}
	return contract.StartAssistant{ID: projects.AssistantAntigravity, Label: "Antigravity",
		Availability: contract.StartAvailabilityUnknown, Installed: installed,
		Version: info.Version, AuthState: contract.StartAuthState(info.AuthState),
		IntegrationState: state, Capabilities: &contract.StartCapabilities{}}
}

func (s *Server) placesRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost || r.Method == http.MethodDelete || r.URL.Query().Get("registered") == "1" {
		s.registeredPlacesRoute(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	if err := s.icons.Refresh(); err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "icon_store_unavailable", "Project icons could not be read.")
		return
	}
	readers := s.projectReaders()
	list := readers.places.List(s.liveDirectories(r.Context()), 40)
	repos := readers.repos.Resolve(list)
	keys := make([]setupLookupKey, 0, len(list))
	icons := make(map[string]icon.Grid, len(list))
	sources := make(map[string]icon.Source, len(list))
	for _, p := range list {
		grid, source := s.icons.Resolve(p.Path)
		icons[p.Path] = grid
		sources[p.Path] = source
		keys = append(keys, setupLookupKey{path: p.Path, repo: repos[p.Path], icon: source})
	}
	setups := readers.setups.Resolve(keys)
	out := contract.StartPlaceList{At: time.Now().Unix(), Assistants: s.availableAssistants(),
		Places: make([]contract.StartPlace, 0, len(list))}
	for _, p := range list {
		// `repo` is what a schedule moving here from another machine finds its
		// project by (docs/schedules.md): the id is a digest of this path.
		repo := repos[p.Path]
		grid := icons[p.Path]
		var projectID string
		if canonical, ok := projects.CanonicalProjectKey(p.Path); ok {
			projectID = projects.ProjectID(canonical)
		}
		out.Places = append(out.Places, contract.StartPlace{ID: p.ID, Label: p.Label, Path: p.Path,
			At: p.At.Unix(), Icon: wireIcon(grid), Repo: repo, ProjectID: projectID,
			Setup: setups[setupLookupKey{path: p.Path, repo: repo, icon: sources[p.Path]}]})
	}
	writeJSON(w, out)
}

// registeredPlacesRoute keeps the CLI's explicit list in the same daemon
// whose start sheet reads GET /v1/places. A write never silently falls back to
// a second state directory when the daemon cannot be reached.
func (s *Server) registeredPlacesRoute(w http.ResponseWriter, r *http.Request) {
	if !machineAuthed(r) && !((r.Method == http.MethodDelete || r.Method == http.MethodPost) && maySend(r)) {
		writeRefusal(w, http.StatusForbidden, "machine_required", "Only this machine may change its registered Projects.")
		return
	}
	registry := s.projectReaders().registry
	var rows []projects.RegisteredPlace
	var err error
	switch r.Method {
	case http.MethodGet:
		rows, err = registry.List()
	case http.MethodPost, http.MethodDelete:
		var body struct {
			Paths []string `json:"paths"`
		}
		dec := json.NewDecoder(r.Body)
		if dec.Decode(&body) != nil || dec.Decode(new(any)) != io.EOF || len(body.Paths) == 0 {
			writeRefusal(w, http.StatusBadRequest, "invalid_project_paths", "Supply one JSON object with paths to existing directories.")
			return
		}
		if r.Method == http.MethodPost {
			rows, err = registry.Add(body.Paths, time.Now())
		} else {
			rows, err = registry.Remove(body.Paths)
		}
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET, POST or DELETE only")
		return
	}
	if err != nil {
		writeRefusal(w, http.StatusUnprocessableEntity, "project_registry_failed", err.Error())
		return
	}
	writeJSON(w, struct {
		Registered bool                       `json:"registered"`
		Places     []projects.RegisteredPlace `json:"places"`
	}{true, rows})
}

// projectCatalogRoute is the catalog half of ProjectBoardIntegration's
// materializeReadModel: the store's Projects, each joined to the start place
// that resolves to it, start places first in their own order.
func (s *Server) projectCatalogRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	if err := s.icons.Refresh(); err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "icon_store_unavailable", "Project icons could not be read.")
		return
	}
	// The catalog's projects were the retired Swift app's Board store, which
	// is no longer read (docs/cutover.md §7): known, and it adds no projects.
	// This is the answer that store gave when it was switched off (cutover
	// B1).
	catalog := contract.ProjectCatalog{
		SchemaVersion: 1,
		Projects:      []contract.CatalogProject{},
		Source:        contract.CatalogSource{Ingestion: contract.CatalogIngestion{Status: "unknown"}},
		ReadState:     contract.CatalogReadState{Status: contract.CatalogReadStatusReady},
		Available:     true,
		Mode:          "standard",
	}
	writeJSON(w, contract.ProjectCatalogAnswer{At: time.Now().Unix(), Catalog: catalog})
}

// projectsRoute reads worktrees, refreshes the observation, and removes one
// freshly verified landed checkout on a person's explicit request.
func (s *Server) projectsRoute(w http.ResponseWriter, r *http.Request) {
	// The string the mux dispatched by, split before anything is decoded, so
	// this route and the gate's `machineScoped` read the same shape
	// (routePath, gate.go).
	parts := strings.Split(strings.TrimPrefix(routePath(r), "/v1/projects/"), "/")
	for i, part := range parts {
		parts[i] = decodeSegment(part)
	}
	if len(parts) >= 2 && parts[0] != "" && parts[1] == "files" && len(parts) <= 3 {
		id := ""
		if len(parts) == 3 {
			id = parts[2]
		}
		s.projectFilesRoute(w, r, parts[0], id)
		return
	}
	if len(parts) >= 2 && parts[0] != "" && parts[1] == "tree" &&
		(len(parts) == 2 || len(parts) == 3 && parts[2] == "file") {
		s.projectTreeRoute(w, r, parts[0], len(parts) == 3)
		return
	}
	if len(parts) >= 2 && parts[0] != "" && parts[1] == "memory" && len(parts) <= 3 {
		name := ""
		if len(parts) == 3 {
			if parts[2] == "" {
				writeNoSuchRoute(w, r)
				return
			}
			name = parts[2]
		}
		s.projectMemoryRoute(w, r, parts[0], name)
		return
	}
	if len(parts) == 3 && parts[0] != "" && parts[1] == "memory-groups" && parts[2] != "" {
		s.projectMemoryGroupRoute(w, r, parts[0], parts[2])
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "antigravity-consent" {
		s.antigravityProjectConsentRoute(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "unify" {
		s.projectUnifyRoute(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[0] != "" && parts[1] == "worktrees" && strings.HasPrefix(parts[2], "wt-") {
		if r.Method != http.MethodDelete {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if !maySend(r) {
			writeRefusal(w, http.StatusForbidden, "send_required", "Deleting a worktree requires send access to this machine.")
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength > 0 {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "Worktree removal takes no query or body.")
			return
		}
		readers := s.projectReaders()
		select {
		case readers.busy <- struct{}{}:
		default:
			writeRefusal(w, http.StatusTooManyRequests, "worktree_lifecycle_busy", "Worktree lifecycle work is already queued on this machine; try again shortly.")
			return
		}
		snap, err := readers.lifecycle.Remove(parts[0], parts[2])
		<-readers.busy
		var refusal *projects.Refusal
		if errors.As(err, &refusal) {
			writeRefusal(w, refusal.Status, refusal.Code, refusal.Message)
			return
		}
		if err != nil {
			writeRawRefusal(w, http.StatusInternalServerError, "worktree_remove_failed", err.Error())
			return
		}
		writeJSON(w, struct {
			Snapshot *projects.Snapshot `json:"projectWorktreeLifecycle"`
		}{snap})
		return
	}
	if len(parts) < 2 || parts[0] == "" || parts[1] != "worktrees" || len(parts) > 3 ||
		(len(parts) == 3 && parts[2] != "refresh") {
		writeNoSuchRoute(w, r)
		return
	}
	refresh := len(parts) == 3
	want := http.MethodGet
	if refresh {
		want = http.MethodPost
	}
	if r.Method != want {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", want+" only")
		return
	}
	if r.URL.RawQuery != "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Worktree lifecycle routes take no query fields.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024+1))
	if err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The body could not be read.")
		return
	}
	if len(body) > 16*1024 {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "body_too_large", "A worktree lifecycle body is at most 16 KiB.")
		return
	}
	if !refresh && len(body) > 0 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "This route takes no body.")
		return
	}
	if refresh && len(body) > 0 {
		var fields map[string]json.RawMessage
		if json.Unmarshal(body, &fields) != nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "The body must be one JSON object.")
			return
		}
		if len(fields) > 0 {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "A refresh takes an empty body.")
			return
		}
	}
	readers := s.projectReaders()
	var snap *projects.Snapshot
	if refresh {
		select {
		case readers.busy <- struct{}{}:
		default:
			writeRefusal(w, http.StatusTooManyRequests, "worktree_lifecycle_busy",
				"Worktree lifecycle work is already queued on this machine; try again shortly.")
			return
		}
		snap, err = readers.lifecycle.Refresh(parts[0])
		<-readers.busy
	} else {
		snap, err = readers.lifecycle.Snapshot(parts[0])
	}
	var refusal *projects.Refusal
	if errors.As(err, &refusal) {
		writeRefusal(w, refusal.Status, refusal.Code, refusal.Message)
		return
	}
	if err != nil {
		writeRawRefusal(w, http.StatusInternalServerError, "worktree_lifecycle_failed", err.Error())
		return
	}
	writeJSON(w, struct {
		Snapshot *projects.Snapshot `json:"projectWorktreeLifecycle"`
	}{snap})
}
