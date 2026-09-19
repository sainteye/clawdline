package projects

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The lifecycle read model is ProjectWorktreeLifecycleService's read half:
// snapshot and refresh. Preview and apply — the cleanup executor — are not
// ported, and no route here reaches them; the browser never had them either.

const (
	schemaVersion        = 1
	rowLimit             = 200
	dirtyPathLimit       = 500
	statusEntryLimit     = 5000
	changedPathLimit     = 1000
	projectCacheLimit    = 32
	localFreshness       = 300 * time.Second
	canonicalFreshness   = 6 * time.Hour
	refreshCoalesce      = 5 * time.Second
	observationBudget    = 120 * time.Second
	defaultGitTimeout    = 15 * time.Second
	storageTimeoutCap    = 5 * time.Second
	classActive          = "active_in_use"
	classLandedResidue   = "landed_identical_residue"
	classUnlanded        = "genuinely_unlanded"
	classMixed           = "mixed_conflicted"
	classTaskTemporary   = "task_owned_temporary"
	classPrunable        = "prunable_stale_metadata"
	classUnknownEvidence = "unknown_incomplete_evidence"
)

// classOrder is Classification.allCases.
var classOrder = []string{classActive, classLandedResidue, classUnlanded, classMixed,
	classTaskTemporary, classPrunable, classUnknownEvidence}

// Refusal is a typed refusal: Status and Code are the wire contract.
type Refusal struct {
	Status  int
	Code    string
	Message string
}

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

// Issue is one named gap in the evidence.
type Issue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Task is the part of one Swift task record the lifecycle reads. It arrives
// through the Tasks port; this package never opens the store itself.
type Task struct {
	ID            string
	State         string
	Title         string
	RootLabel     string
	RootSession   string
	ChildSession  string
	ChildTerminal string
	// Plan and Status are the record's own narrative: `plan`, and the newest
	// progress note or else the summary. Empty when the reader does not carry
	// them.
	Plan       string
	Status     string
	Created    *time.Time
	BriefedAt  *time.Time
	SpawnedAt  *time.Time
	FinishedAt *time.Time
	Landing    *TaskLanding
	Worktree   *TaskWorktree
}

type TaskLanding struct {
	State  string
	Target string
	Commit string
}

type TaskWorktree struct {
	Path   string
	Branch string
	Base   string
}

// terminal is Orchestrator.State.isTerminal. An unrecognised state is live.
func terminal(state string) bool {
	switch state {
	case "success", "failure", "timeout", "cancelled", "spawn_failed":
		return true
	}
	return false
}

type TaskEvidence struct {
	Authoritative bool
	Tasks         []Task
}

type LiveSession struct {
	TerminalID     string
	ConversationID string
	CWD            string
}

type LiveEvidence struct {
	Complete   bool
	ObservedAt time.Time
	Sessions   []LiveSession
}

// Ports is every dependency the lifecycle reads, as in the Swift service.
type Ports struct {
	// ManagedWorktreeRoots is every root a broker makes checkouts under (the
	// function of that name). The Swift service knew one, its own broker's;
	// a checkout under any of them is managed.
	ManagedWorktreeRoots []string
	ProjectDirectories   func() []string
	Tasks                func() TaskEvidence
	Live                 func() LiveEvidence
	Now                  func() time.Time
	StorageBytes         func(path string, timeout time.Duration) (int64, *Issue)
}

// DefaultStorageBytes is measureStorageBytes: `du -sk`, bounded.
func DefaultStorageBytes(path string, timeout time.Duration) (int64, *Issue) {
	unavailable := &Issue{Code: "storage_unavailable", Message: "The bounded disk-usage observation did not complete."}
	if timeout <= 0 {
		return 0, unavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/du", "-sk", path).Output()
	if err != nil {
		return 0, unavailable
	}
	token := strings.Fields(string(out))
	if len(token) == 0 {
		return 0, unavailable
	}
	kib, err := strconv.ParseInt(token[0], 10, 64)
	if err != nil || kib < 0 {
		return 0, unavailable
	}
	bytes := kib * 1024
	if bytes/1024 != kib || bytes > 9_007_199_254_740_991 {
		return 0, &Issue{Code: "storage_out_of_range", Message: "The observed disk usage is outside JSON's safe integer range."}
	}
	return bytes, nil
}

// Identity is ProjectIdentity.
type Identity struct {
	ID            string
	Label         string
	CanonicalPath string
}

// IdentityFor is projectIdentity(forDirectory:).
func IdentityFor(directory string) (Identity, bool) {
	canonical, ok := CanonicalProjectKey(directory)
	if !ok {
		return Identity{}, false
	}
	return Identity{ID: ProjectID(canonical), Label: filepath.Base(canonical), CanonicalPath: canonical}, true
}

type owner struct {
	kind           string // task, repository, foreign, unknown
	evidence       string
	taskID         string
	sessionID      string
	terminalID     string
	title          string
	taskState      string
	rootLabel      string
	landing        string
	recordedBase   string
	recordedBranch string
	purpose        string
	note           string
	currentStatus  string
	createdAt      *time.Time
	startedAt      *time.Time
	finishedAt     *time.Time
	originSession  string
	originTitle    string
}

func (o owner) nextOwner() string {
	switch o.kind {
	case "task":
		id := o.taskID
		if id == "" {
			id = "?"
		}
		if o.rootLabel != "" {
			return fmt.Sprintf("%s (the root that dispatched task %s)", o.rootLabel, id)
		}
		return "the root that dispatched task " + id
	case "repository":
		return "the repository owner working in the main checkout"
	case "foreign":
		return "the person or tool that registered this worktree"
	}
	return "the Project root: ownership evidence for this checkout is missing"
}

type dirtyEntry struct {
	path                     string
	staged, modified         bool
	untracked, conflicted    bool
	indexDiffersFromWorktree bool
	deleted                  bool
	worktreeMode             string
	worktreeBlob             string
}

type row struct {
	worktreeID      string
	path            string
	isMain          bool
	exists          bool
	locked          bool
	prunable        bool
	branch          string
	head            string
	owner           owner
	active          *bool
	activeEvidence  []string
	statusComplete  bool
	staged          *int64
	modified        *int64
	untracked       *int64
	ignored         int
	dirty           []dirtyEntry
	conflicted      bool
	aheadOfTarget   *int
	mergeBase       string
	identicalPaths  []string
	unlandedPaths   []string
	unknownPaths    []string
	errors          []Issue
	classifications []string
	blockers        []Issue
	actions         []string
	storageBytes    *int64
	storageObserved *time.Time
	storageError    *Issue
}

func (r row) base() string {
	if r.owner.recordedBase != "" {
		return r.owner.recordedBase
	}
	return r.mergeBase
}

type canonicalTarget struct {
	branch           string
	localRef         string
	localOID         string
	remoteRef        string
	remoteOID        string
	remoteObservedAt *time.Time
	published        *bool
	err              *Issue
}

type observation struct {
	project      Identity
	repositoryID string
	common       string
	mainPath     string
	observedAt   time.Time
	target       canonicalTarget
	rows         []row
	truncated    bool
	err          *Issue
}

// Lifecycle is the service. One lives as long as the daemon, because its
// cache is what a plain read answers from.
type Lifecycle struct {
	ports Ports

	stateMu  sync.Mutex
	cache    map[string]observation
	order    []string
	observe_ sync.Mutex
	deadline time.Time
}

func NewLifecycle(p Ports) *Lifecycle {
	if p.Now == nil {
		p.Now = time.Now
	}
	if p.StorageBytes == nil {
		p.StorageBytes = DefaultStorageBytes
	}
	if len(p.ManagedWorktreeRoots) == 0 {
		p.ManagedWorktreeRoots = ManagedWorktreeRoots("")
	}
	return &Lifecycle{ports: p, cache: map[string]observation{}}
}

// Resolve is resolveProject.
func (l *Lifecycle) Resolve(id string) (Identity, error) {
	if !IsProjectID(id) {
		return Identity{}, &Refusal{400, "bad_project_id",
			"A Project is named by its Board id: project- and 24 lowercase hex digits."}
	}
	seen := map[string]bool{}
	var dirs []string
	if l.ports.ProjectDirectories != nil {
		dirs = l.ports.ProjectDirectories()
	}
	for _, d := range dirs {
		if seen[d] {
			continue
		}
		seen[d] = true
		if identity, ok := IdentityFor(d); ok && identity.ID == id {
			return identity, nil
		}
	}
	return Identity{}, &Refusal{404, "project_not_found", "No Project known to this Mac has that id."}
}

// Snapshot is the cached reading, or an explicit not_observed one. It never
// runs git.
func (l *Lifecycle) Snapshot(id string) (*Snapshot, error) {
	project, err := l.Resolve(id)
	if err != nil {
		return nil, err
	}
	l.stateMu.Lock()
	cached, ok := l.cache[project.ID]
	l.stateMu.Unlock()
	if !ok {
		return notObserved(project), nil
	}
	return encode(cached, l.ports.Now()), nil
}

// Refresh runs one bounded local observation now and returns it, coalescing
// with one that finished inside the window. It never fetches.
func (l *Lifecycle) Refresh(id string) (*Snapshot, error) {
	project, err := l.Resolve(id)
	if err != nil {
		return nil, err
	}
	l.observe_.Lock()
	defer l.observe_.Unlock()
	now := l.ports.Now()
	l.stateMu.Lock()
	cached, had := l.cache[project.ID]
	l.stateMu.Unlock()
	if had && now.Sub(cached.observedAt) < refreshCoalesce && cached.err == nil {
		return encode(cached, now), nil
	}
	obs := l.observe(project)
	if obs.err != nil && had {
		// The last good rows are kept, and serve-time freshness reports them stale.
		kept := cached
		kept.err = obs.err
		obs = kept
	}
	l.store(obs)
	return encode(obs, now), nil
}

func (l *Lifecycle) store(obs observation) {
	l.stateMu.Lock()
	defer l.stateMu.Unlock()
	l.cache[obs.project.ID] = obs
	for i, id := range l.order {
		if id == obs.project.ID {
			l.order = append(l.order[:i], l.order[i+1:]...)
			break
		}
	}
	l.order = append(l.order, obs.project.ID)
	for len(l.order) > projectCacheLimit {
		delete(l.cache, l.order[0])
		l.order = l.order[1:]
	}
}

// ---------- git ----------

type gitAnswer struct {
	status int
	output string
	utf8   bool
}

func (l *Lifecycle) git(args []string, cwd string, timeout time.Duration) *gitAnswer {
	if !l.deadline.IsZero() {
		remaining := l.deadline.Sub(l.ports.Now())
		if remaining <= 0 {
			return nil
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "GIT_LITERAL_PATHSPECS=1", "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil
	}
	status := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil
		}
		status = exit.ExitCode()
	}
	return &gitAnswer{status: status, output: stdout.String(), utf8: utf8.Valid(stdout.Bytes())}
}

func (l *Lifecycle) gitText(args []string, cwd string, timeout time.Duration) (string, bool) {
	a := l.git(args, cwd, timeout)
	if a == nil || a.status != 0 || !a.utf8 {
		return "", false
	}
	return a.output, true
}

func isObjectID(v string) bool { return (len(v) == 40 || len(v) == 64) && lowerHex(v) }

func (l *Lifecycle) oid(revision, cwd string) string {
	text, ok := l.gitText([]string{"rev-parse", "--verify", "--quiet", "--end-of-options", revision + "^{commit}"},
		cwd, defaultGitTimeout)
	if !ok {
		return ""
	}
	v := strings.TrimSpace(text)
	if !isObjectID(v) {
		return ""
	}
	return v
}

// gitCommonDirectory is OrchestratorDraft.gitCommonDirectory(at:).
func (l *Lifecycle) gitCommonDirectory(cwd string) string {
	a := l.git([]string{"rev-parse", "--git-common-dir"}, cwd, defaultGitTimeout)
	if a == nil || a.status != 0 {
		return ""
	}
	raw := strings.TrimSpace(a.output)
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "/") {
		raw = filepath.Join(cwd, raw)
	}
	canonical := canonicalFilesystemPath(raw)
	if !isDirectory(canonical) {
		return ""
	}
	return canonical
}

// ---------- probe ----------

type registered struct {
	path, head, branch string
	locked, prunable   bool
}

func parseWorktreeList(text string) []registered {
	var out []registered
	var cur *registered
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, field := range strings.Split(text, "\x00") {
		switch {
		case field == "":
			flush()
		case strings.HasPrefix(field, "worktree "):
			flush()
			cur = &registered{path: strings.TrimPrefix(field, "worktree ")}
		case cur == nil:
		case strings.HasPrefix(field, "HEAD "):
			cur.head = strings.TrimPrefix(field, "HEAD ")
		case strings.HasPrefix(field, "branch refs/heads/"):
			cur.branch = strings.TrimPrefix(field, "branch refs/heads/")
		case field == "locked" || strings.HasPrefix(field, "locked "):
			cur.locked = true
		case field == "prunable" || strings.HasPrefix(field, "prunable "):
			cur.prunable = true
		}
	}
	flush()
	return out
}

// parseStatus is porcelain v2, NUL separated, renames off. ok is false when
// the output is not the shape this parser knows.
func parseStatus(text string, limit int) (entries []dirtyEntry, ignored int, truncated, ok bool) {
	tokens := strings.Split(text, "\x00")
	if len(tokens) > 0 && tokens[len(tokens)-1] == "" {
		tokens = tokens[:len(tokens)-1]
	}
	for i := 0; i < len(tokens); {
		token := tokens[i]
		i++
		if len(entries)+ignored >= limit {
			return entries, ignored, true, true
		}
		if token == "" {
			return nil, 0, false, false
		}
		switch kind := token[0]; kind {
		case '?':
			if len(token) < 2 {
				return nil, 0, false, false
			}
			entries = append(entries, dirtyEntry{path: token[2:], untracked: true})
		case '!':
			ignored++
		case '1', '2', 'u':
			fieldCount := 8
			if kind == '2' {
				fieldCount = 9
			} else if kind == 'u' {
				fieldCount = 10
			}
			parts := strings.SplitN(token, " ", fieldCount+1)
			if len(parts) != fieldCount+1 || len(parts[1]) != 2 {
				return nil, 0, false, false
			}
			xy := parts[1]
			if kind == '2' {
				i++
			}
			conflicted := kind == 'u'
			staged, modified := xy[0] != '.', xy[1] != '.'
			mode := parts[5]
			if conflicted {
				mode = parts[6]
			}
			entries = append(entries, dirtyEntry{path: parts[fieldCount], staged: staged, modified: modified,
				conflicted: conflicted, indexDiffersFromWorktree: conflicted || (staged && modified),
				deleted: xy[1] == 'D' || (xy[0] == 'D' && xy[1] == '.'), worktreeMode: mode})
		default:
			return nil, 0, false, false
		}
	}
	return entries, ignored, false, true
}

func (l *Lifecycle) observe(project Identity) observation {
	started := l.ports.Now()
	l.deadline = started.Add(observationBudget)
	defer func() { l.deadline = time.Time{} }()
	var tasks TaskEvidence
	if l.ports.Tasks != nil {
		tasks = l.ports.Tasks()
	}
	var live LiveEvidence
	if l.ports.Live != nil {
		live = l.ports.Live()
	}
	obs := observation{project: project, repositoryID: RepositoryID(project.CanonicalPath), observedAt: started}
	failed := func(code, message string) observation {
		issue := &Issue{code, message}
		obs.target = canonicalTarget{err: issue}
		obs.err = issue
		return obs
	}
	common := l.gitCommonDirectory(project.CanonicalPath)
	if common == "" {
		return failed("repository_unreadable", "Git could not name this Project's repository.")
	}
	listing, ok := l.gitText([]string{"worktree", "list", "--porcelain", "-z"}, project.CanonicalPath, defaultGitTimeout)
	if !ok {
		return failed("worktree_list_failed", "git worktree list did not answer.")
	}
	reg := parseWorktreeList(listing)
	if len(reg) == 0 {
		return failed("worktree_list_empty", "git worktree list named no checkout at all.")
	}
	obs.common, obs.mainPath = common, reg[0].path
	obs.target = l.canonicalTarget(project.CanonicalPath, common)
	obs.truncated = len(reg) > rowLimit
	if obs.truncated {
		reg = reg[:rowLimit]
	}
	budgetExceeded := false
	for offset, entry := range reg {
		if !l.ports.Now().Before(l.deadline) {
			budgetExceeded = true
			break
		}
		r := l.observeRow(entry, WorktreeID(common, entry.path), offset == 0, obs.target, tasks, live)
		if !l.ports.Now().Before(l.deadline) {
			budgetExceeded = true
			r.errors = append(r.errors, Issue{"observation_budget_exceeded",
				"The bounded observation ran out of time before this row was complete."})
		}
		l.classify(&r, obs.target, tasks.Authoritative)
		obs.rows = append(obs.rows, r)
	}
	if budgetExceeded {
		obs.err = &Issue{"observation_budget_exceeded", "The bounded observation stopped at its two-minute deadline."}
	}
	return obs
}

func (l *Lifecycle) canonicalTarget(cwd, common string) canonicalTarget {
	branch := ""
	if symbolic, ok := l.gitText([]string{"symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"}, cwd, defaultGitTimeout); ok {
		ref := strings.TrimSpace(symbolic)
		if rest, ok := strings.CutPrefix(ref, "refs/remotes/origin/"); ok {
			branch = rest
		}
	}
	if branch == "" {
		for _, candidate := range []string{"main", "master"} {
			if l.oid("refs/heads/"+candidate, cwd) != "" {
				branch = candidate
				break
			}
		}
	}
	localOID := ""
	if branch != "" {
		localOID = l.oid("refs/heads/"+branch, cwd)
	}
	if branch == "" || localOID == "" {
		t := canonicalTarget{branch: branch,
			err: &Issue{"target_unresolved", "No local main or master branch resolves to a commit."}}
		if branch != "" {
			t.localRef = "refs/heads/" + branch
		}
		return t
	}
	remoteRef := "refs/remotes/origin/" + branch
	remoteOID := l.oid(remoteRef, cwd)
	if remoteOID == "" {
		return canonicalTarget{branch: branch, localRef: "refs/heads/" + branch, localOID: localOID}
	}
	t := canonicalTarget{branch: branch, localRef: "refs/heads/" + branch, localOID: localOID,
		remoteRef: remoteRef, remoteOID: remoteOID}
	if st, err := os.Stat(filepath.Join(common, "FETCH_HEAD")); err == nil {
		at := st.ModTime()
		t.remoteObservedAt = &at
	}
	if a := l.git([]string{"merge-base", "--is-ancestor", localOID, remoteOID}, cwd, defaultGitTimeout); a != nil {
		published := a.status == 0
		t.published = &published
	}
	return t
}

func (l *Lifecycle) observeRow(entry registered, id string, isMain bool, target canonicalTarget,
	tasks TaskEvidence, live LiveEvidence) row {
	r := row{worktreeID: id, path: entry.path, isMain: isMain, exists: isDirectory(entry.path),
		locked: entry.locked, prunable: entry.prunable, branch: entry.branch,
		owner: l.resolveOwner(entry, isMain, tasks)}
	if isObjectID(entry.head) {
		r.head = entry.head
	}
	resolveActivity(&r, live)
	if !r.exists {
		r.errors = append(r.errors, Issue{"worktree_path_missing", "The registered checkout directory does not exist."})
		return r
	}
	observedAt := l.ports.Now()
	r.storageObserved = &observedAt
	timeout := storageTimeoutCap
	if !l.deadline.IsZero() {
		if left := l.deadline.Sub(observedAt); left < timeout {
			timeout = left
		}
	}
	if timeout > 0 {
		if bytes, issue := l.ports.StorageBytes(entry.path, timeout); issue != nil {
			r.storageError = issue
		} else {
			r.storageBytes = &bytes
		}
	} else {
		r.storageError = &Issue{"storage_observation_budget_exceeded",
			"The bounded observation ended before disk usage could be read."}
	}
	statusText, ok := l.gitText([]string{"status", "--porcelain=v2", "-z", "--no-renames",
		"--untracked-files=all", "--ignored=matching"}, entry.path, 30*time.Second)
	var entries []dirtyEntry
	var ignored int
	var truncated bool
	if ok {
		entries, ignored, truncated, ok = parseStatus(statusText, statusEntryLimit)
	}
	if !ok {
		r.errors = append(r.errors, Issue{"status_unreadable", "git status did not answer in a known shape."})
		return r
	}
	r.ignored = ignored
	r.dirty = entries
	for _, e := range entries {
		if e.conflicted {
			r.conflicted = true
		}
	}
	if !r.conflicted {
		r.conflicted = l.operationInProgress(entry.path)
	}
	if truncated || len(entries) > dirtyPathLimit {
		r.errors = append(r.errors, Issue{"status_truncated",
			fmt.Sprintf("More than %d changed paths; this row is not compared path by path.", dirtyPathLimit)})
	} else {
		r.statusComplete = true
		var staged, modified, untracked int64
		for _, e := range entries {
			if e.staged {
				staged++
			}
			if e.modified && !e.untracked {
				modified++
			}
			if e.untracked {
				untracked++
			}
		}
		r.staged, r.modified, r.untracked = &staged, &modified, &untracked
	}
	l.hashWorktreeFiles(&r)
	l.compare(&r, target)
	return r
}

func (l *Lifecycle) operationInProgress(path string) bool {
	text, ok := l.gitText([]string{"rev-parse", "--absolute-git-dir"}, path, defaultGitTimeout)
	dir := strings.TrimSpace(text)
	if !ok || dir == "" {
		return true
	}
	for _, name := range []string{"MERGE_HEAD", "rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

func (l *Lifecycle) hashWorktreeFiles(r *row) {
	var paths []string
	for i := range r.dirty {
		e := r.dirty[i]
		if e.deleted || e.conflicted {
			continue
		}
		st, err := os.Lstat(filepath.Join(r.path, e.path))
		if err != nil {
			r.dirty[i].deleted = true
			continue
		}
		if !st.Mode().IsRegular() {
			r.errors = append(r.errors, Issue{"unsupported_dirty_entry",
				"A changed path is not a regular file and cannot be preserved as verified bytes."})
			continue
		}
		mode := "100644"
		if st.Mode().Perm()&0o111 != 0 {
			mode = "100755"
		}
		r.dirty[i].worktreeMode = mode
		r.dirty[i].deleted = false
		paths = append(paths, e.path)
	}
	if len(paths) == 0 {
		return
	}
	text, ok := l.gitText(append([]string{"hash-object", "--"}, paths...), r.path, 60*time.Second)
	if !ok {
		r.errors = append(r.errors, Issue{"hash_failed", "git hash-object could not read the changed files."})
		return
	}
	hashes := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(hashes) != len(paths) {
		r.errors = append(r.errors, Issue{"hash_failed", "git hash-object answered for a different set of files."})
		return
	}
	byPath := map[string]string{}
	for i, p := range paths {
		byPath[p] = hashes[i]
	}
	for i := range r.dirty {
		if blob, ok := byPath[r.dirty[i].path]; ok {
			r.dirty[i].worktreeBlob = blob
		}
	}
}

func splitNUL(text string) []string {
	var out []string
	for _, s := range strings.Split(text, "\x00") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// compare is the final bytes of every path this checkout changed, committed
// or dirty, against the local target. Dirty bytes decide a path they touch.
func (l *Lifecycle) compare(r *row, target canonicalTarget) {
	if target.err != nil || target.localOID == "" {
		r.errors = append(r.errors, Issue{"comparison_unavailable", "There is no resolvable target to compare this checkout with."})
		return
	}
	if r.head == "" {
		r.errors = append(r.errors, Issue{"head_unresolved", "This checkout has no resolvable HEAD commit."})
		return
	}
	aheadText, ok := l.gitText([]string{"rev-list", "--count", target.localOID + ".." + r.head}, r.path, defaultGitTimeout)
	ahead, err := strconv.Atoi(strings.TrimSpace(aheadText))
	if !ok || err != nil {
		r.errors = append(r.errors, Issue{"ancestry_unreadable", "git could not count commits beyond the target."})
		return
	}
	r.aheadOfTarget = &ahead
	if mb, ok := l.gitText([]string{"merge-base", r.head, target.localOID}, r.path, defaultGitTimeout); ok {
		r.mergeBase = strings.TrimSpace(mb)
	}
	var committedChanged []string
	committedDiffering := map[string]bool{}
	if ahead > 0 {
		changed, ok := "", false
		if isObjectID(r.mergeBase) {
			changed, ok = l.gitText([]string{"diff", "--name-only", "-z", "--no-renames", r.mergeBase, r.head}, r.path, defaultGitTimeout)
		}
		if !ok {
			r.errors = append(r.errors, Issue{"branch_diff_unreadable", "git could not list what this branch changed."})
			return
		}
		committedChanged = splitNUL(changed)
		if len(committedChanged) > changedPathLimit {
			r.errors = append(r.errors, Issue{"branch_diff_truncated",
				fmt.Sprintf("The branch changed more than %d paths; it is not compared path by path.", changedPathLimit)})
			return
		}
		if len(committedChanged) > 0 {
			differing, ok := l.gitText(append([]string{"diff", "--name-only", "-z", "--no-renames", r.head, target.localOID, "--"},
				committedChanged...), r.path, 30*time.Second)
			if !ok {
				r.errors = append(r.errors, Issue{"target_diff_unreadable", "git could not compare the branch with its target."})
				return
			}
			for _, p := range splitNUL(differing) {
				committedDiffering[p] = true
			}
		}
	}
	type blob struct{ mode, blob string }
	targetEntries := map[string]blob{}
	if len(r.dirty) > 0 {
		args := []string{"ls-tree", "-z", "--full-tree", target.localOID, "--"}
		for _, e := range r.dirty {
			args = append(args, e.path)
		}
		listing, ok := l.gitText(args, r.path, 30*time.Second)
		if !ok {
			r.errors = append(r.errors, Issue{"target_tree_unreadable", "git could not read the target's version of the changed paths."})
			return
		}
		for _, record := range splitNUL(listing) {
			meta, name, found := strings.Cut(record, "\t")
			if !found {
				continue
			}
			fields := strings.Split(meta, " ")
			if len(fields) != 3 || fields[1] != "blob" {
				continue
			}
			targetEntries[name] = blob{fields[0], fields[2]}
		}
	}
	var identical, unlanded, unknown []string
	decided := map[string]bool{}
	for _, e := range r.dirty {
		decided[e.path] = true
		var answer *bool
		switch {
		case e.conflicted || e.indexDiffersFromWorktree:
		case e.deleted:
			_, present := targetEntries[e.path]
			v := !present
			answer = &v
		case e.worktreeBlob != "" && e.worktreeMode != "":
			t, present := targetEntries[e.path]
			v := present && t.blob == e.worktreeBlob && t.mode == e.worktreeMode
			answer = &v
		}
		switch {
		case answer == nil:
			unknown = append(unknown, e.path)
		case *answer:
			identical = append(identical, e.path)
		default:
			unlanded = append(unlanded, e.path)
		}
	}
	for _, p := range committedChanged {
		if decided[p] {
			continue
		}
		if committedDiffering[p] {
			unlanded = append(unlanded, p)
		} else {
			identical = append(identical, p)
		}
	}
	sort.Strings(identical)
	sort.Strings(unlanded)
	sort.Strings(unknown)
	r.identicalPaths, r.unlandedPaths, r.unknownPaths = identical, unlanded, unknown
}

func boundedNarrative(value string, limit int) string {
	compact := strings.Join(strings.Fields(value), " ")
	if compact == "" {
		return ""
	}
	runes := []rune(compact)
	if len(runes) <= limit {
		return compact
	}
	return string(runes[:limit-1]) + "…"
}

func lowerUUID(v string) string {
	if len(v) != 36 {
		return ""
	}
	for i, c := range v {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return ""
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return ""
			}
		}
	}
	return strings.ToLower(v)
}

// managedRelative is relativePath from the first managed root that holds
// path, which is already comparable.
func (l *Lifecycle) managedRelative(path string) (string, bool) {
	for _, root := range l.ports.ManagedWorktreeRoots {
		if root == "" {
			continue
		}
		if relative, inside := relativePath(comparablePath(root), path); inside {
			return relative, true
		}
	}
	return "", false
}

func (l *Lifecycle) resolveOwner(entry registered, isMain bool, tasks TaskEvidence) owner {
	if isMain {
		return owner{kind: "repository", evidence: "main_worktree"}
	}
	path := comparablePath(entry.path)
	relative, inside := l.managedRelative(path)
	var components []string
	if inside && relative != "" {
		components = strings.Split(relative, "/")
	}
	if len(components) != 2 || !isTaskID(components[1]) {
		return owner{kind: "foreign", evidence: "not_a_clawdline_managed_worktree"}
	}
	managedTaskID := components[1]
	if !tasks.Authoritative {
		return owner{kind: "unknown", evidence: "task_registry_unavailable"}
	}
	var matches []Task
	for _, t := range tasks.Tasks {
		if t.Worktree != nil && comparablePath(t.Worktree.Path) == path {
			matches = append(matches, t)
		}
	}
	if len(matches) == 0 {
		return owner{kind: "unknown", evidence: "managed_path_without_task_record"}
	}
	t := matches[0]
	if len(matches) != 1 || t.ID != managedTaskID || t.Worktree.Branch != worktreeBranch(t.ID) ||
		(entry.branch != "" && entry.branch != t.Worktree.Branch) {
		return owner{kind: "unknown", evidence: "identity_conflict", taskID: t.ID, title: t.Title,
			taskState: t.State, rootLabel: t.RootLabel}
	}
	landing := ""
	if t.Landing != nil {
		target, commit := t.Landing.Target, t.Landing.Commit
		if target == "" {
			target = "-"
		}
		if commit == "" {
			commit = "-"
		}
		landing = strings.Join([]string{t.Landing.State, target, commit}, "|")
	}
	started := t.BriefedAt
	if started == nil {
		started = t.SpawnedAt
	}
	return owner{kind: "task", evidence: "exact_task_worktree_record", taskID: t.ID,
		sessionID: lowerUUID(t.ChildSession), terminalID: t.ChildTerminal, title: t.Title,
		taskState: t.State, rootLabel: t.RootLabel, landing: landing,
		recordedBase: t.Worktree.Base, recordedBranch: t.Worktree.Branch,
		purpose:       boundedNarrative(t.Title, 240),
		note:          boundedNarrative(t.Plan, 600),
		currentStatus: boundedNarrative(t.Status, 300),
		createdAt:     t.Created, startedAt: started, finishedAt: t.FinishedAt,
		originSession: lowerUUID(t.RootSession),
		originTitle:   boundedNarrative(t.RootLabel, 120)}
}

func resolveActivity(r *row, live LiveEvidence) {
	var evidence []string
	if r.owner.kind == "task" && r.owner.taskState != "" && !terminal(r.owner.taskState) {
		evidence = append(evidence, "task_live")
	}
	if r.owner.evidence == "identity_conflict" && r.owner.taskState != "" && !terminal(r.owner.taskState) {
		evidence = append(evidence, "task_live")
	}
	root := comparablePath(r.path)
	for _, s := range live.Sessions {
		cwd := comparablePath(s.CWD)
		if cwd == root || strings.HasPrefix(cwd, root+"/") {
			evidence = append(evidence, "session:"+s.TerminalID)
		}
	}
	sort.Strings(evidence)
	r.activeEvidence = evidence
	switch {
	case len(evidence) > 0:
		v := true
		r.active = &v
	case live.Complete && !live.ObservedAt.IsZero():
		v := false
		r.active = &v
	default:
		r.active = nil
	}
}

func (l *Lifecycle) classify(r *row, target canonicalTarget, tasksAuthoritative bool) {
	classes := map[string]bool{}
	ownerLive := r.owner.taskState != "" && !terminal(r.owner.taskState)
	isActive := r.active != nil && *r.active
	if isActive {
		classes[classActive] = true
	}
	if r.active == nil {
		classes[classUnknownEvidence] = true
	}
	if !r.exists || r.prunable {
		if ownerLive || isActive {
			classes[classUnknownEvidence] = true
		} else {
			classes[classPrunable] = true
		}
	}
	var gaps []Issue
	for _, e := range r.errors {
		if e.Code != "worktree_path_missing" {
			gaps = append(gaps, e)
		}
	}
	if len(gaps) > 0 || (r.exists && !r.statusComplete) || len(r.unknownPaths) > 0 {
		classes[classUnknownEvidence] = true
	}
	if r.owner.kind == "foreign" || r.owner.kind == "unknown" {
		classes[classUnknownEvidence] = true
	}
	if r.conflicted || r.owner.evidence == "identity_conflict" {
		classes[classMixed] = true
	}
	if len(r.unlandedPaths) > 0 {
		classes[classUnlanded] = true
	}
	if len(r.identicalPaths) > 0 {
		classes[classLandedResidue] = true
	}
	if len(r.unlandedPaths) > 0 && len(r.identicalPaths) > 0 {
		classes[classMixed] = true
	}
	if r.isMain && len(r.dirty) > 0 {
		classes[classUnknownEvidence] = true
	}
	settled := r.owner.kind == "task" && r.owner.taskState != "" && terminal(r.owner.taskState)
	if !r.isMain && r.exists && settled && r.statusComplete && len(gaps) == 0 && len(r.unlandedPaths) == 0 &&
		len(r.unknownPaths) == 0 && !r.conflicted && r.aheadOfTarget != nil && *r.aheadOfTarget == 0 {
		if len(r.identicalPaths) == 0 && r.head != "" && r.head == r.owner.recordedBase {
			classes[classTaskTemporary] = true
		} else {
			classes[classLandedResidue] = true
		}
	}
	r.classifications = nil
	for _, c := range classOrder {
		if classes[c] {
			r.classifications = append(r.classifications, c)
		}
	}
	l.plan(r, target, tasksAuthoritative, classes)
}

// plan is the cleanup assessment. Its blockers are what the page shows; its
// actions only decide `eligible`, since nothing here can act on them.
func (l *Lifecycle) plan(r *row, target canonicalTarget, tasksAuthoritative bool, classes map[string]bool) {
	var blockers []Issue
	has := func(code string) bool {
		for _, b := range blockers {
			if b.Code == code {
				return true
			}
		}
		return false
	}
	if r.isMain {
		blockers = append(blockers, Issue{"main_worktree", "The main checkout is never cleaned up here."})
	}
	if r.active != nil && *r.active {
		blockers = append(blockers, Issue{"worktree_in_use",
			fmt.Sprintf("A live task or Session is using this checkout (%s).", strings.Join(r.activeEvidence, ", "))})
	}
	if r.active == nil {
		blockers = append(blockers, Issue{"live_inventory_incomplete", "The Session inventory is incomplete, so liveness cannot be ruled out."})
	}
	if !tasksAuthoritative {
		blockers = append(blockers, Issue{"task_registry_unavailable", "The task registry is not authoritative."})
	}
	switch r.owner.kind {
	case "foreign":
		blockers = append(blockers, Issue{"foreign_worktree", "This checkout was not created by Clawdline."})
	case "unknown":
		code := "owner_unknown"
		if r.owner.evidence == "identity_conflict" {
			code = "owner_identity_conflict"
		}
		blockers = append(blockers, Issue{code, fmt.Sprintf("Ownership of this checkout cannot be proved (%s).", r.owner.evidence)})
	}
	if r.locked {
		blockers = append(blockers, Issue{"worktree_locked", "git has this worktree locked."})
	}
	if r.ignored > 0 {
		blockers = append(blockers, Issue{"ignored_entries_present", "Ignored entries are not byte-pinned, so this checkout cannot be removed."})
	}
	for _, gap := range r.errors {
		if gap.Code != "worktree_path_missing" {
			blockers = append(blockers, gap)
		}
	}
	if classes[classMixed] {
		blockers = append(blockers, Issue{"mixed_or_conflicted", "Landed and unlanded work, a conflict, or an operation in progress share this checkout."})
	}
	if classes[classUnlanded] {
		blockers = append(blockers, Issue{"unlanded_work", fmt.Sprintf("%d path(s) are not in the target.", len(r.unlandedPaths))})
	}
	if classes[classUnknownEvidence] && !has("owner_unknown") && len(r.unknownPaths) > 0 {
		blockers = append(blockers, Issue{"evidence_incomplete", fmt.Sprintf("%d path(s) could not be compared.", len(r.unknownPaths))})
	}
	if strings.HasPrefix(r.owner.landing, "pending|") && target.branch != "" &&
		!strings.HasPrefix(r.owner.landing, "pending|-|") && !strings.HasPrefix(r.owner.landing, "pending|"+target.branch+"|") {
		blockers = append(blockers, Issue{"landing_target_mismatch",
			fmt.Sprintf("The landing record names a different target than %s.", target.branch)})
	}
	if target.remoteRef != "" && !r.isMain && classes[classLandedResidue] {
		fresh := target.remoteObservedAt != nil && l.ports.Now().Sub(*target.remoteObservedAt) <= canonicalFreshness
		if !fresh {
			blockers = append(blockers, Issue{"canonical_target_stale", "The canonical remote has not been fetched recently; refresh it before cleanup."})
		} else if target.published == nil || !*target.published {
			blockers = append(blockers, Issue{"landing_not_published", "The local target has commits the canonical remote does not."})
		}
	}
	_, managed := l.managedRelative(comparablePath(r.path))
	indexDiffers := false
	for _, e := range r.dirty {
		if e.indexDiffersFromWorktree {
			indexDiffers = true
		}
	}
	preservable := !r.isMain && r.exists && r.owner.kind != "foreign" && r.active != nil && !*r.active &&
		r.statusComplete && !r.conflicted && len(r.dirty) > 0 && len(r.errors) == 0 && !indexDiffers && managed
	var actions []string
	onlyDestructive := len(classes) > 0
	for c := range classes {
		if c != classLandedResidue && c != classTaskTemporary {
			onlyDestructive = false
		}
	}
	if onlyDestructive {
		if len(r.dirty) > 0 {
			actions = append(actions, "preserve_patch")
		}
		actions = append(actions, "remove_checkout")
		if r.branch != "" && r.branch == r.owner.recordedBranch && r.aheadOfTarget != nil && *r.aheadOfTarget == 0 {
			actions = append(actions, "delete_branch")
		}
	} else if len(classes) == 1 && classes[classPrunable] && r.owner.kind == "task" {
		actions = append(actions, "prune_metadata")
	}
	if classes[classPrunable] && r.owner.kind != "task" {
		ownerBlocked := false
		for _, b := range blockers {
			if b.Code == "foreign_worktree" || strings.HasPrefix(b.Code, "owner_") {
				ownerBlocked = true
			}
		}
		if !ownerBlocked {
			blockers = append(blockers, Issue{"owner_unknown", "Stale metadata without a task record is not pruned here."})
		}
	}
	switch {
	case len(blockers) == 0:
		r.actions = actions
	case classes[classUnlanded] && preservable && !has("worktree_in_use") && !has("live_inventory_incomplete") &&
		!has("mixed_or_conflicted") && !has("task_registry_unavailable") && !has("worktree_locked"):
		r.actions = []string{"preserve_patch"}
	default:
		r.actions = nil
	}
	r.blockers = blockers
}
