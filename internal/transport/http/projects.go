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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/projects"
	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// The Projects page's routes: the start places, the Board store's Project
// catalog, and one Project's worktree lifecycle. Every rule is the Swift app's
// and lives in internal/adapters/projects; this file only wires the readers to
// this daemon's inventory and to the Swift store, and encodes.

// projectReaders are built on first use and kept, because each one caches: the
// places remember which folder stands for which directory, the catalog
// remembers its last good reading, and the lifecycle's plain read answers from
// the observation a refresh made.
type projectReaders struct {
	places    *projects.Places
	catalog   *projects.Catalog
	lifecycle *projects.Lifecycle
	// busy bounds queued plus running lifecycle work, ProjectWorktreeHTTP.depth.
	busy chan struct{}
}

var (
	projectOnce sync.Once
	projectRead *projectReaders
)

func (s *Server) projectReaders() *projectReaders {
	projectOnce.Do(func() {
		projectRead = &projectReaders{
			places:  projects.NewPlaces(s.icons.Label),
			catalog: projects.NewCatalog(),
			busy:    make(chan struct{}, 4),
		}
		projectRead.lifecycle = projects.NewLifecycle(projects.Ports{
			ProjectDirectories: func() []string {
				dirs := projects.RegistryPaths()
				for _, t := range s.swift.Read().Tasks {
					dirs = append(dirs, t.ProjectDir)
				}
				return dirs
			},
			Tasks: func() projects.TaskEvidence { return lifecycleTasks(s.swift.Read()) },
			Live:  func() projects.LiveEvidence { return s.liveEvidence(context.Background()) },
		})
	})
	return projectRead
}

func secondsPtr(v *swiftstore.Seconds) *time.Time {
	if v == nil {
		return nil
	}
	t := v.Time()
	return &t
}

func stringOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// lifecycleTasks hands the lifecycle the task records it reads.
func lifecycleTasks(snap swiftstore.Snapshot) projects.TaskEvidence {
	out := projects.TaskEvidence{Authoritative: snap.Known && !snap.Stale}
	for _, t := range snap.Tasks {
		row := projects.Task{
			ID: t.ID, State: t.State, Title: t.Title,
			RootLabel: stringOrEmpty(t.RootLabel), RootSession: stringOrEmpty(t.RootSession),
			ChildSession: stringOrEmpty(t.ChildSession), ChildTerminal: stringOrEmpty(t.ChildTerminal),
			Created:   secondsPtr(&t.Created),
			BriefedAt: secondsPtr(t.BriefedAt), SpawnedAt: secondsPtr(t.SpawnedAt),
			FinishedAt: secondsPtr(t.FinishedAt),
		}
		row.Plan = stringOrEmpty(t.Plan)
		// Orchestrator.Task: the newest progress note, or else the summary.
		if n := len(t.Progress); n > 0 {
			row.Status = t.Progress[n-1].Note
		} else {
			row.Status = t.Summary.Text
		}
		if t.Created == 0 {
			row.Created = nil
		}
		if t.Landing != nil {
			row.Landing = &projects.TaskLanding{State: t.Landing.State, Target: stringOrEmpty(t.Landing.Target),
				Commit: stringOrEmpty(t.Landing.Commit)}
		}
		if t.Worktree != nil {
			row.Worktree = &projects.TaskWorktree{Path: t.Worktree.Path, Branch: t.Worktree.Branch,
				Base: t.Worktree.Base}
		}
		out.Tasks = append(out.Tasks, row)
	}
	return out
}

// liveEvidence is the lifecycle's Live port: every assistant session's
// directory, and whether the reading was complete.
func (s *Server) liveEvidence(ctx context.Context) projects.LiveEvidence {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	inv := s.inventory.Read(ctx)
	out := projects.LiveEvidence{Complete: inv.Complete, ObservedAt: inv.ObservedAt}
	for _, item := range inv.Sessions {
		if item.IsAssistant() && item.CWD != "" {
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
	return out
}

func (s *Server) placesRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	readers := s.projectReaders()
	list := readers.places.List(s.liveDirectories(r.Context()), 40)
	out := contract.StartPlaceList{At: time.Now().Unix(), Assistants: installedAssistants(),
		Places: make([]contract.StartPlace, 0, len(list))}
	for _, p := range list {
		out.Places = append(out.Places, contract.StartPlace{ID: p.ID, Label: p.Label, Path: p.Path,
			At: p.At.Unix(), Icon: wireIcon(s.icons.For(p.Path))})
	}
	writeJSON(w, out)
}

// projectCatalogRoute is the catalog half of ProjectBoardIntegration's
// materializeReadModel: the store's Projects, each joined to the start place
// that resolves to it, start places first in their own order.
func (s *Server) projectCatalogRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	readers := s.projectReaders()
	board := readers.catalog.Read()
	catalog := contract.ProjectCatalog{
		SchemaVersion: 1,
		Projects:      []contract.CatalogProject{},
		Source:        contract.CatalogSource{Ingestion: contract.CatalogIngestion{Status: "unknown"}},
	}
	switch {
	case !board.Known:
		catalog.ReadState = contract.CatalogReadState{Status: contract.CatalogReadStatusError,
			Error: &contract.CatalogError{Code: "board_unavailable", Message: "The Board store could not be read."}}
		writeJSON(w, contract.ProjectCatalogAnswer{At: time.Now().Unix(), Catalog: catalog})
		return
	case board.Stale:
		catalog.ReadState = contract.CatalogReadState{Status: contract.CatalogReadStatusStale,
			ObservedAt: float64(board.ReadAt.UnixMilli()) / 1000,
			Error:      &contract.CatalogError{Code: "board_store_unreadable", Message: "The newest read failed; an earlier reading is shown."}}
	default:
		catalog.ReadState = contract.CatalogReadState{Status: contract.CatalogReadStatusReady,
			ObservedAt: float64(board.ReadAt.UnixMilli()) / 1000}
	}
	catalog.Available = true
	catalog.Enabled = board.Enabled
	catalog.Revision = board.Revision
	catalog.Mode = "standard"
	if board.Enabled {
		catalog.Mode = "board"
	}

	type presentation struct {
		label, path string
		order       int
	}
	byID := map[string]presentation{}
	for i, p := range readers.places.List(s.liveDirectories(r.Context()), 40) {
		canonical, ok := projects.CanonicalProjectKey(p.Path)
		if !ok {
			continue
		}
		id := projects.ProjectID(canonical)
		if _, seen := byID[id]; seen {
			continue
		}
		// StartPoints.label(for:) of the canonical path, as projectPresentations does.
		label := s.icons.Label(canonical)
		if label == "" {
			label = filepath.Base(canonical)
		}
		byID[id] = presentation{label: label, path: canonical, order: i}
	}
	for _, p := range board.Projects {
		row := contract.CatalogProject{ID: p.ID, Name: p.Name, ItemCount: int64(p.ItemCount)}
		if pres, ok := byID[p.ID]; ok {
			row.IsStartPoint = true
			row.Label = pres.label
			row.DisplayPath = pres.path
			row.Icon = wireIcon(s.icons.For(pres.path))
		}
		catalog.Projects = append(catalog.Projects, row)
	}
	sort.SliceStable(catalog.Projects, func(i, j int) bool {
		a, aok := byID[catalog.Projects[i].ID]
		b, bok := byID[catalog.Projects[j].ID]
		switch {
		case aok && bok:
			return a.order < b.order
		case aok != bok:
			return aok
		}
		return strings.ToLower(catalog.Projects[i].Name) < strings.ToLower(catalog.Projects[j].Name)
	})
	writeJSON(w, contract.ProjectCatalogAnswer{At: time.Now().Unix(), Catalog: catalog})
}

// projectsRoute is ProjectWorktreeHTTP's read and refresh, and nothing else:
// the cleanup preview and apply are not routes on this daemon.
func (s *Server) projectsRoute(w http.ResponseWriter, r *http.Request) {
	// The string the mux dispatched by, split before anything is decoded, so
	// this route and the gate's `machineScoped` read the same shape
	// (routePath, gate.go).
	parts := strings.Split(strings.TrimPrefix(routePath(r), "/v1/projects/"), "/")
	for i, part := range parts {
		parts[i] = decodeSegment(part)
	}
	if len(parts) < 2 || parts[0] == "" || parts[1] != "worktrees" || len(parts) > 3 ||
		(len(parts) == 3 && parts[2] != "refresh") {
		writeRefusal(w, http.StatusNotFound, "not_found", "No such route")
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
				"Worktree lifecycle work is already queued on this Mac; try again shortly.")
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
		writeRefusal(w, http.StatusInternalServerError, "worktree_lifecycle_failed", err.Error())
		return
	}
	writeJSON(w, struct {
		Snapshot *projects.Snapshot `json:"projectWorktreeLifecycle"`
	}{snap})
}
