package graphify

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	BuildBodyBytesLimit        = 4096
	BuildJobsLimit             = 16
	BuildSecondsLimit          = 600
	BuildPollMillisecondsLimit = 1000
	BuildLogBytesLimit         = 64 << 10
	RuntimeBytesLimit          = 512 << 20
	BuildRawBytesLimit         = 128 << 20
	BuildWorkersLimit          = 1
)

type BuildStatus struct {
	State     string `json:"state"`
	Phase     string `json:"phase"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
	StartedAt int64  `json:"started_at"`
	UpdatedAt int64  `json:"updated_at"`
	Error     string `json:"error"`
}

type BuildError string

func (e BuildError) Error() string { return string(e) }

// Engine receives a private source snapshot and output directory. Tests replace
// it without installing a parser, starting a model, or changing real settings.
type Engine func(context.Context, string, string, func(string)) error
type buildJob struct {
	BuildStatus
	key    string
	cancel context.CancelFunc
	done   chan struct{}
}
type Builder struct {
	mu      sync.Mutex
	jobs    map[string]*buildJob
	active  string
	cache   string
	engine  Engine
	allowed func() bool
	closed  bool
}

func NewBuilder(cache string, allowed func() bool, engine Engine) *Builder {
	b := &Builder{cache: cache, allowed: allowed, jobs: map[string]*buildJob{}}
	if engine == nil {
		b.engine = b.extract
	} else {
		b.engine = engine
	}
	return b
}

func (b *Builder) Status(root string) BuildStatus {
	if abs, err := filepath.Abs(root); err == nil {
		if resolved, e := filepath.EvalSymlinks(abs); e == nil {
			root = resolved
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if job := b.jobs[root]; job != nil {
		return job.BuildStatus
	}
	return BuildStatus{State: "idle", Phase: "idle"}
}

func (b *Builder) Counts() (active, held int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.active != "" {
		active = 1
	}
	return active, int64(len(b.jobs))
}

// Start never queues behind another build and never retries a held request key.
// One machine-wide slot bounds parser preparation as well as AST extraction.
func (b *Builder) Start(root, key string) (BuildStatus, error) {
	if key == "" {
		return BuildStatus{}, BuildError("request_key_required")
	}
	if b.allowed != nil && !b.allowed() {
		return BuildStatus{}, BuildError("disabled")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return BuildStatus{}, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return BuildStatus{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return BuildStatus{}, BuildError("unavailable")
	}
	if job := b.jobs[abs]; job != nil && (job.key == key || job.State == "running") {
		return job.BuildStatus, nil
	}
	if b.active != "" {
		return BuildStatus{}, BuildError("busy")
	}
	if _, err = os.Lstat(filepath.Join(abs, "graphify-out")); !os.IsNotExist(err) {
		return BuildStatus{}, BuildError("index_exists")
	}
	// Terminal receipts expire after the same bounded operation window. A new
	// click still cannot overwrite an index made by an expired request.
	for name, job := range b.jobs {
		if job.State != "running" && time.Now().Unix()-job.UpdatedAt > BuildSecondsLimit {
			delete(b.jobs, name)
		}
	}
	if _, found := b.jobs[abs]; !found && len(b.jobs) >= BuildJobsLimit {
		return BuildStatus{}, BuildError("history_full")
	}
	now := time.Now().Unix()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(BuildSecondsLimit)*time.Second)
	job := &buildJob{key: key, cancel: cancel, done: make(chan struct{}), BuildStatus: BuildStatus{State: "running", Phase: "scanning", StartedAt: now, UpdatedAt: now}}
	b.jobs[abs] = job
	b.active = abs
	go b.run(ctx, abs, job)
	return job.BuildStatus, nil
}

// Close revokes new admissions, cancels the exact active job, and joins cleanup
// within the owner's shutdown deadline. It never deletes a completed index.
func (b *Builder) Close(ctx context.Context) error {
	b.mu.Lock()
	b.closed = true
	var done <-chan struct{}
	if job := b.jobs[b.active]; job != nil {
		job.cancel()
		done = job.done
	}
	b.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *Builder) change(job *buildJob, edit func(*BuildStatus)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	edit(&job.BuildStatus)
	job.UpdatedAt = time.Now().Unix()
}

func (b *Builder) run(ctx context.Context, root string, job *buildJob) {
	cancel := job.cancel
	defer cancel()
	defer close(job.done)
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(time.Duration(BuildPollMillisecondsLimit) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if b.allowed != nil && !b.allowed() {
					cancel()
					return
				}
			}
		}
	}()
	err := b.build(ctx, root, job)
	if err != nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	job.UpdatedAt = time.Now().Unix()
	b.active = ""
	if err == nil {
		job.State = "succeeded"
		job.Phase = "complete"
		return
	}
	job.State = "failed"
	job.Phase = "failed"
	job.Error = "failed"
	var typed BuildError
	if errors.As(err, &typed) {
		job.Error = string(typed)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		job.Error = "timeout"
	}
	if errors.Is(err, context.Canceled) {
		job.Error = "disabled"
		if b.closed {
			job.Error = "stopped"
		}
	}
}

type buildSource struct {
	Hash string `json:"ast_hash"`
}

func (b *Builder) build(ctx context.Context, root string, job *buildJob) error {
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, name := range []string{".graphifyrc", ".graphifyignore"} {
		if _, e := r.Lstat(name); !os.IsNotExist(e) {
			return BuildError("custom_inputs")
		}
	}
	if err = os.MkdirAll(b.cache, 0700); err != nil {
		return err
	}
	scratch, err := os.MkdirTemp(b.cache, "build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	input := filepath.Join(scratch, "source")
	output := filepath.Join(scratch, "result")
	for _, p := range []string{input, output} {
		if err = os.Mkdir(p, 0700); err != nil {
			return err
		}
	}
	manifest := map[string]buildSource{}
	entries := 0
	var total int64
	err = fs.WalkDir(r.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > MaxScanEntries {
			return BuildError("source_limit")
		}
		if d.IsDir() {
			if name != "." && (excludedDirectory(d.Name()) || strings.HasPrefix(d.Name(), ".clawdline-graphify-")) {
				return fs.SkipDir
			}
			return nil
		}
		if !sourceName(name) {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return BuildError("unsafe_source")
		}
		f, e := openProbeFile(r, name)
		if e != nil {
			return e
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			return BuildError("unsafe_source")
		}
		if st.Size() > MaxFileBytes || st.Size() > MaxTotalBytes-total {
			return BuildError("source_limit")
		}
		data, e := io.ReadAll(io.LimitReader(f, min(MaxFileBytes, MaxTotalBytes-total)+1))
		if e != nil {
			return e
		}
		if int64(len(data)) > MaxFileBytes || int64(len(data)) > MaxTotalBytes-total {
			return BuildError("source_limit")
		}
		total += int64(len(data))
		sum := md5.Sum(data)
		manifest[name] = buildSource{hex.EncodeToString(sum[:])}
		dest := filepath.Join(input, filepath.FromSlash(name))
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return e
		}
		if e = os.WriteFile(dest, data, 0600); e != nil {
			return e
		}
		b.change(job, func(s *BuildStatus) { s.Files = len(manifest); s.Bytes = total })
		return nil
	})
	if err != nil {
		return err
	}
	if len(manifest) == 0 {
		return BuildError("no_sources")
	}
	phase := func(name string) { b.change(job, func(s *BuildStatus) { s.Phase = name }) }
	if err = b.engine(ctx, input, output, phase); err != nil {
		return err
	}
	phase("checking")
	graphPath := filepath.Join(output, "graph.json")
	f, err := os.Open(graphPath)
	if err != nil {
		return fmt.Errorf("%w: graph read", BuildError("parser_failed"))
	}
	raw, err := io.ReadAll(io.LimitReader(f, BuildRawBytesLimit+1))
	f.Close()
	if err != nil {
		return err
	}
	if len(raw) > BuildRawBytesLimit {
		return BuildError("graph_limit")
	}
	// Keep native identities/relations, but never persist executable upstream
	// HTML or enormous attributes that the built-in viewer does not consume.
	var graph struct {
		Graph map[string]any               `json:"graph"`
		Nodes []map[string]json.RawMessage `json:"nodes"`
		Links []map[string]json.RawMessage `json:"links"`
	}
	if json.Unmarshal(raw, &graph) != nil || graph.Graph["schema_version"] != float64(1) || graph.Nodes == nil || graph.Links == nil {
		return fmt.Errorf("%w: graph envelope", BuildError("parser_failed"))
	}
	for i, n := range graph.Nodes {
		keep := map[string]json.RawMessage{}
		for _, k := range []string{"id", "label", "source_file", "source_location", "community"} {
			if v, ok := n[k]; ok {
				keep[k] = v
			}
		}
		graph.Nodes[i] = keep
	}
	for i, l := range graph.Links {
		keep := map[string]json.RawMessage{}
		for _, k := range []string{"source", "target", "relation", "confidence"} {
			if v, ok := l[k]; ok {
				keep[k] = v
			}
		}
		graph.Links[i] = keep
	}
	unverifiedSources := 0
	known := map[string]bool{}
	for _, n := range graph.Nodes {
		var id, file string
		if json.Unmarshal(n["id"], &id) != nil || id == "" || known[id] {
			return fmt.Errorf("%w: node identities", BuildError("parser_failed"))
		}
		known[id] = true
		for _, key := range []string{"label", "source_location"} {
			if raw, ok := n[key]; ok {
				var text string
				if json.Unmarshal(raw, &text) != nil {
					return fmt.Errorf("%w: node text", BuildError("parser_failed"))
				}
			}
		}
		if v, ok := n["source_file"]; ok {
			if json.Unmarshal(v, &file) != nil {
				return fmt.Errorf("%w: source format", BuildError("parser_failed"))
			}
			if file != "" {
				if _, ok := manifest[file]; !ok {
					// Upstream module/dependency nodes may name an unresolved import,
					// not a file in this snapshot. Keep the reference, never claim its
					// source was hashed, and never pass an arbitrary path to a reader.
					if filepath.IsAbs(file) || strings.Contains(file, "\\") || path.Clean(file) != file || file == ".." || strings.HasPrefix(file, "../") {
						return fmt.Errorf("%w: unsafe source reference", BuildError("parser_failed"))
					}
					n["source_file"] = json.RawMessage(`""`)
					delete(n, "source_location")
					unverifiedSources++
				}
			}
		}
	}
	for _, l := range graph.Links {
		var a, z string
		if json.Unmarshal(l["source"], &a) != nil || json.Unmarshal(l["target"], &z) != nil || !known[a] || !known[z] {
			return fmt.Errorf("%w: link identities", BuildError("parser_failed"))
		}
		for _, key := range []string{"relation", "confidence"} {
			if raw, ok := l[key]; ok {
				var text string
				if json.Unmarshal(raw, &text) != nil {
					return fmt.Errorf("%w: link text", BuildError("parser_failed"))
				}
			}
		}
	}
	graph.Graph = map[string]any{"schema_version": 1, "graphify_version": "0.9.97", "clawdline_code_only": true, "unverified_sources": unverifiedSources}
	compact, err := json.Marshal(graph)
	if err != nil {
		return err
	}
	if int64(len(compact)) > MaxMetadataBytes {
		return BuildError("graph_limit")
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if int64(len(manifestRaw)) > MaxMetadataBytes {
		return BuildError("source_limit")
	}
	// Construct an entire candidate beside the target. Kernel no-replace rename
	// publishes it without exposing a partial index or replacing user content.
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	stage := ".clawdline-graphify-" + hex.EncodeToString(nonce[:])
	if err = r.Mkdir(stage, 0700); err != nil {
		return err
	}
	defer r.RemoveAll(stage)
	for name, data := range map[string][]byte{"graph.json": compact, "manifest.json": manifestRaw, ".graphify_root": []byte(root), ".gitignore": []byte("*\n")} {
		p := filepath.Join(stage, name)
		out, e := r.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = out.Write(data)
		if e == nil {
			e = out.Sync()
		}
		closeErr := out.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	// The graph's identities and sources were validated against the immutable
	// snapshot. Verify live hashes/inventory again just before publication.
	if err = verifyBuildSources(ctx, r, manifest); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if b.allowed != nil && !b.allowed() {
		return BuildError("disabled")
	}
	phase("publishing")
	if err = publishDirectory(r, stage, "graphify-out"); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return BuildError("index_exists")
		}
		return err
	}
	return nil
}

func verifyBuildSources(ctx context.Context, r *os.Root, manifest map[string]buildSource) error {
	for _, name := range []string{".graphifyrc", ".graphifyignore"} {
		if _, err := r.Lstat(name); !os.IsNotExist(err) {
			return BuildError("source_changed")
		}
	}
	entries := 0
	seen := 0
	var total int64
	err := fs.WalkDir(r.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > MaxScanEntries {
			return BuildError("source_limit")
		}
		if d.IsDir() {
			if name != "." && (excludedDirectory(d.Name()) || strings.HasPrefix(d.Name(), ".clawdline-graphify-")) {
				return fs.SkipDir
			}
			return nil
		}
		if !sourceName(name) {
			return nil
		}
		row, ok := manifest[name]
		if !ok || d.Type()&os.ModeSymlink != 0 {
			return BuildError("source_changed")
		}
		f, err := openProbeFile(r, name)
		if err != nil {
			return BuildError("source_changed")
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() {
			return BuildError("source_changed")
		}
		if st.Size() > MaxFileBytes || st.Size() > MaxTotalBytes-total {
			return BuildError("source_limit")
		}
		data, err := io.ReadAll(io.LimitReader(f, min(MaxFileBytes, MaxTotalBytes-total)+1))
		if err != nil {
			return err
		}
		total += int64(len(data))
		if int64(len(data)) > MaxFileBytes || total > MaxTotalBytes {
			return BuildError("source_limit")
		}
		sum := md5.Sum(data)
		if hex.EncodeToString(sum[:]) != row.Hash {
			return BuildError("source_changed")
		}
		seen++
		return nil
	})
	if err != nil {
		return err
	}
	if seen != len(manifest) {
		return BuildError("source_changed")
	}
	return nil
}
