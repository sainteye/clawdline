package projectsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/icon"
	domain "github.com/sainteye/clawdline/internal/domain/projectsync"
)

// MaxCloneJobs is how many clones may run at once. Registered in
// internal/domain/capacity.
const MaxCloneJobs = 2

// Checkout is one project root this machine knows, as its places list names it.
type Checkout struct {
	Path  string
	Label string
}

// Refusals an apply answers by name.
var (
	ErrSourceMismatch = errors.New("this project already mirrors another machine")
	ErrCloneTarget    = errors.New("the clone destination already exists and is not this repository")
	ErrCloneBusy      = errors.New("this machine is already cloning as many repositories as it will at once")
	ErrNotOffered     = errors.New("this machine does not offer that repository")
	ErrInvalid        = errors.New("the project settings are not valid")
	ErrNoCloneRoot    = errors.New("this machine has no place to clone into")
)

// Apply states.
const (
	StateApplied   = "applied"
	StateUnchanged = "unchanged"
	StateMissing   = "missing"
	StateCloning   = "cloning"
	StateFailed    = "clone_failed"
)

// ApplyRequest is one project arriving at a mirror.
type ApplyRequest struct {
	Source        domain.Source `json:"source"`
	Project       domain.Entry  `json:"project"`
	Clone         bool          `json:"clone"`
	ReplaceSource bool          `json:"replace_source"`
}

// ApplyResult is what one apply did.
type ApplyResult struct {
	Repo     string        `json:"repo"`
	State    string        `json:"state"`
	Path     string        `json:"path,omitempty"`
	Revision string        `json:"revision"`
	Written  []string      `json:"written"`
	Deleted  []string      `json:"deleted"`
	Kept     []domain.Kept `json:"kept"`
}

// Job is a clone in progress, or one that failed and is kept until the next
// apply for the same repository says otherwise.
type Job struct {
	Repo    string        `json:"repo"`
	State   string        `json:"state"`
	Dest    string        `json:"dest"`
	Source  domain.Source `json:"source"`
	Started int64         `json:"started"`
	Error   string        `json:"error,omitempty"`
	request ApplyRequest
}

// MirrorState is a mirror's own account of itself.
type MirrorState struct {
	CloneRoot string          `json:"clone_root"`
	Projects  []domain.Record `json:"projects"`
	Clones    []Job           `json:"clones"`
}

// Service joins the domain's policy to this machine's checkouts.
type Service struct {
	// Checkouts are the project roots this machine knows, canonical.
	Checkouts func(ctx context.Context) []Checkout
	// Icon is the mark this machine draws for a path.
	Icon func(path string) icon.Grid
	// Register adds a checkout to this machine's places.
	Register func(path string) error
	// CloneRoot is where a missing repository is cloned.
	CloneRoot func() string
	Mirror    *domain.Mirror
	Now       func() time.Time

	mu    sync.Mutex
	apply sync.Mutex
	jobs  map[string]*Job
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Manifest is what this machine offers: every project with a portable origin
// that is not itself a mirror here. Contents are left out.
func (s *Service) Manifest(ctx context.Context) domain.Manifest {
	out := domain.Manifest{Version: domain.Version, At: s.now().Unix(), Projects: []domain.Entry{}, Skipped: []domain.Skipped{}}
	seen := map[string]bool{}
	for _, c := range s.Checkouts(ctx) {
		entry, skip := s.describe(ctx, c, false)
		if skip == "" && seen[entry.Repo] {
			skip = domain.SkipDuplicate
		}
		if skip == "" && len(out.Projects) >= domain.MaxManifestProjects {
			skip = domain.SkipManifestFull
		}
		if skip != "" {
			out.Skipped = append(out.Skipped, domain.Skipped{Label: c.Label, Path: c.Path, Reason: skip})
			continue
		}
		seen[entry.Repo] = true
		out.Projects = append(out.Projects, entry)
	}
	sort.Slice(out.Projects, func(i, j int) bool { return out.Projects[i].Repo < out.Projects[j].Repo })
	out.Revision = domain.ManifestRevision(out.Projects)
	return out
}

// Entry is one offered project with its files' contents.
func (s *Service) Entry(ctx context.Context, repo string) (domain.Entry, error) {
	for _, c := range s.Checkouts(ctx) {
		url, err := Origin(ctx, c.Path)
		if err != nil {
			continue
		}
		if r, err := domain.Repo(url); err != nil || r != repo {
			continue
		}
		entry, skip := s.describe(ctx, c, true)
		if skip != "" {
			continue
		}
		return entry, nil
	}
	return domain.Entry{}, ErrNotOffered
}

func (s *Service) describe(ctx context.Context, c Checkout, withContent bool) (domain.Entry, string) {
	if s.Mirror != nil && s.Mirror.Mirrored(c.Path) {
		return domain.Entry{}, domain.SkipMirrored
	}
	url, err := Origin(ctx, c.Path)
	if errors.Is(err, ErrNoRemote) {
		return domain.Entry{}, domain.SkipNoRemote
	}
	if err != nil {
		return domain.Entry{}, domain.SkipUnreadable
	}
	repo, err := domain.Repo(url)
	if err != nil {
		return domain.Entry{}, domain.SkipLocalRemote
	}
	paths, err := Untracked(ctx, c.Path)
	if err != nil {
		return domain.Entry{}, domain.SkipUnreadable
	}
	e := domain.Entry{Repo: repo, CloneURL: url, Label: c.Label, Icon: s.Icon(c.Path), Files: []domain.File{}}
	if len(e.Label) > 200 {
		e.Label = e.Label[:200]
	}
	total := 0
	for _, p := range paths {
		if len(e.Files) >= domain.MaxProjectFiles {
			break
		}
		data, err := Read(c.Path, p)
		if err != nil || data == nil {
			continue
		}
		total += len(data)
		if withContent && total > domain.MaxEntryBytes/2 {
			// Base64 grows a body by a third; stop well before the bound.
			break
		}
		f := domain.File{Path: p, SHA256: domain.Sum(data), Size: int64(len(data))}
		if withContent {
			f.Content = data
		}
		e.Files = append(e.Files, f)
	}
	e.Revision = domain.EntryRevision(e)
	return e, ""
}

// State is the mirror's records and clones.
func (s *Service) State() MirrorState {
	out := MirrorState{Projects: []domain.Record{}, Clones: []Job{}}
	if s.CloneRoot != nil {
		out.CloneRoot = s.CloneRoot()
	}
	if s.Mirror != nil {
		out.Projects = s.Mirror.Records()
	}
	s.mu.Lock()
	for _, j := range s.jobs {
		out.Clones = append(out.Clones, *j)
	}
	s.mu.Unlock()
	sort.Slice(out.Clones, func(i, j int) bool { return out.Clones[i].Repo < out.Clones[j].Repo })
	return out
}

// Detach makes a mirrored project this machine's own again.
func (s *Service) Detach(repo string) (bool, error) {
	s.apply.Lock()
	defer s.apply.Unlock()
	return s.Mirror.Remove(repo)
}

func sameSource(a, b domain.Source) bool {
	if a.Machine != "" || b.Machine != "" {
		return a.Machine == b.Machine
	}
	return a.Name == b.Name
}

// Apply writes one project into this machine's mirror.
func (s *Service) Apply(ctx context.Context, req ApplyRequest) (ApplyResult, error) {
	e := req.Project
	if err := domain.Check(e, true); err != nil {
		return ApplyResult{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	// The revision is recomputed, never trusted: it is what a later apply is
	// compared against.
	e.Revision = domain.EntryRevision(e)
	s.apply.Lock()
	defer s.apply.Unlock()
	prev, had := s.Mirror.Get(e.Repo)
	if had && !req.ReplaceSource && !sameSource(prev.Source, req.Source) {
		return ApplyResult{}, fmt.Errorf("%w (%s)", ErrSourceMismatch, prev.Source.Name)
	}
	root := s.locate(ctx, e.Repo, prev.Path)
	if root == "" {
		if !req.Clone {
			return ApplyResult{Repo: e.Repo, State: StateMissing, Revision: e.Revision}, nil
		}
		return s.startClone(req, e)
	}
	s.forgetJob(e.Repo)
	return s.applyAt(ctx, root, req.Source, e, prev)
}

func (s *Service) applyAt(ctx context.Context, root string, source domain.Source, e domain.Entry, prev domain.Record) (ApplyResult, error) {
	here := map[string]domain.Here{}
	look := func(p string) {
		if _, done := here[p]; done {
			return
		}
		h := domain.Here{}
		if tracked, err := Tracked(ctx, root, p); err != nil || tracked {
			h.Tracked = tracked
			h.Unsafe = err != nil
		}
		if !h.Tracked && !h.Unsafe {
			data, err := Read(root, p)
			switch {
			case err != nil:
				h.Unsafe = true
			case data != nil:
				h.SHA256 = domain.Sum(data)
			}
		}
		here[p] = h
	}
	for _, f := range e.Files {
		look(f.Path)
	}
	owned := map[string]string{}
	if prev.Path == root {
		owned = prev.Files
	}
	for p := range owned {
		look(p)
	}
	steps := domain.Plan(e, owned, here)
	contents := map[string][]byte{}
	for _, f := range e.Files {
		contents[f.Path] = f.Content
	}
	result := ApplyResult{Repo: e.Repo, Path: root, Revision: e.Revision, Written: []string{}, Deleted: []string{}, Kept: steps.Kept}
	if result.Kept == nil {
		result.Kept = []domain.Kept{}
	}
	for _, p := range steps.Write {
		if err := Write(root, p, contents[p]); err != nil {
			delete(steps.Owned, p)
			result.Kept = append(result.Kept, domain.Kept{Path: p, Reason: domain.KeepUnsafe})
			continue
		}
		result.Written = append(result.Written, p)
	}
	for _, p := range steps.Delete {
		if err := Remove(root, p); err != nil {
			result.Kept = append(result.Kept, domain.Kept{Path: p, Reason: domain.KeepUnsafe})
			continue
		}
		result.Deleted = append(result.Deleted, p)
	}
	record := domain.Record{Repo: e.Repo, Path: root, Source: source, Revision: e.Revision, Label: e.Label,
		Icon: e.Icon, Files: steps.Owned, AppliedAt: s.now().Unix()}
	unchanged := prev.Path == root && prev.Revision == e.Revision && sameSource(prev.Source, source) &&
		len(result.Written) == 0 && len(result.Deleted) == 0
	if unchanged {
		result.State = StateUnchanged
		record.AppliedAt = prev.AppliedAt
	} else {
		result.State = StateApplied
	}
	if err := s.Mirror.Put(record); err != nil {
		return ApplyResult{}, err
	}
	if s.Register != nil {
		// Best effort: the mirror is recorded either way, and a place list that
		// could not be written still finds the checkout by its sessions.
		_ = s.Register(root)
	}
	return result, nil
}

// locate is the checkout of repo here: the one the mirror last used when it
// still is that repository, then any known checkout, then the clone
// destination if something already put it there.
func (s *Service) locate(ctx context.Context, repo, last string) string {
	matches := func(dir string) bool {
		if dir == "" {
			return false
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return false
		}
		url, err := Origin(ctx, dir)
		if err != nil {
			return false
		}
		r, err := domain.Repo(url)
		return err == nil && r == repo
	}
	if matches(last) {
		return last
	}
	for _, c := range s.Checkouts(ctx) {
		if matches(c.Path) {
			return c.Path
		}
	}
	if dest := s.cloneDest(repo); matches(dest) {
		return dest
	}
	return ""
}

func (s *Service) cloneDest(repo string) string {
	if s.CloneRoot == nil {
		return ""
	}
	root := s.CloneRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, path.Base(repo))
}

func (s *Service) forgetJob(repo string) {
	s.mu.Lock()
	delete(s.jobs, repo)
	s.mu.Unlock()
}

func (s *Service) startClone(req ApplyRequest, e domain.Entry) (ApplyResult, error) {
	dest := s.cloneDest(e.Repo)
	if dest == "" || e.CloneURL == "" {
		return ApplyResult{}, ErrNoCloneRoot
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobs == nil {
		s.jobs = map[string]*Job{}
	}
	if j, ok := s.jobs[e.Repo]; ok && j.State == StateCloning {
		return ApplyResult{Repo: e.Repo, State: StateCloning, Path: j.Dest, Revision: e.Revision}, nil
	}
	if _, err := os.Lstat(dest); err == nil {
		return ApplyResult{}, fmt.Errorf("%w: %s", ErrCloneTarget, dest)
	}
	running := 0
	for _, j := range s.jobs {
		if j.State == StateCloning {
			running++
		}
	}
	if running >= MaxCloneJobs {
		return ApplyResult{}, ErrCloneBusy
	}
	req.Project = e
	job := &Job{Repo: e.Repo, State: StateCloning, Dest: dest, Source: req.Source, Started: s.now().Unix(), request: req}
	s.jobs[e.Repo] = job
	go s.runClone(job)
	return ApplyResult{Repo: e.Repo, State: StateCloning, Path: dest, Revision: e.Revision,
		Written: []string{}, Deleted: []string{}, Kept: []domain.Kept{}}, nil
}

func (s *Service) runClone(job *Job) {
	err := Clone(context.Background(), job.request.Project.CloneURL, job.Dest)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		s.apply.Lock()
		prev, _ := s.Mirror.Get(job.Repo)
		_, err = s.applyAt(ctx, job.Dest, job.request.Source, job.request.Project, prev)
		s.apply.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		delete(s.jobs, job.Repo)
		return
	}
	job.State = StateFailed
	job.Error = strings.TrimSpace(err.Error())
}
