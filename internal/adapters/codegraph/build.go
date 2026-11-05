package codegraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
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

type Engine func(context.Context, string, string, func(string)) error
type buildJob struct {
	BuildStatus
	key    string
	cancel context.CancelFunc
	done   chan struct{}
}
type Builder struct {
	mu            sync.Mutex
	runtimeMu     sync.Mutex
	servicesWG    sync.WaitGroup
	jobs          map[string]*buildJob
	services      map[string]*service
	active, cache string
	engine        Engine
	native        bool
	allowed       func() bool
	closed        bool
	ctx           context.Context
	cancel        context.CancelFunc
	done          chan struct{}
}

func NewBuilder(cache string, allowed func() bool, engine Engine) *Builder {
	ctx, cancel := context.WithCancel(context.Background())
	b := &Builder{cache: cache, allowed: allowed, jobs: map[string]*buildJob{}, services: map[string]*service{}, ctx: ctx, cancel: cancel, done: make(chan struct{}), native: engine == nil}
	b.engine = engine
	if engine == nil {
		b.engine = b.extract
	}
	go b.maintain()
	return b
}
func (b *Builder) enabled() bool { return b.allowed == nil || b.allowed() }
func (b *Builder) maintain() {
	defer close(b.done)
	ticker := time.NewTicker(time.Duration(BuildPollMillisecondsLimit) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			enabled := b.enabled()
			now := time.Now().Unix()
			b.mu.Lock()
			if !enabled {
				if job := b.jobs[b.active]; job != nil {
					job.cancel()
				}
			}
			var expired []*service
			for root, s := range b.services {
				if !enabled || now-s.lastUsed.Load() > ServiceIdleSecondsLimit || s.exited() {
					delete(b.services, root)
					expired = append(expired, s)
				}
			}
			b.mu.Unlock()
			for _, s := range expired {
				s.stop()
			}
		}
	}
}
func (b *Builder) Status(root string) BuildStatus {
	if resolved, err := canonical(root); err == nil {
		root = resolved
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if job := b.jobs[root]; job != nil {
		return job.BuildStatus
	}
	return BuildStatus{State: "idle", Phase: "idle"}
}
func (b *Builder) Counts() (int64, int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var active int64
	if b.active != "" {
		active = 1
	}
	return active, int64(len(b.jobs))
}
func (b *Builder) ServiceCount() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return int64(len(b.services))
}
func (b *Builder) change(job *buildJob, edit func(*BuildStatus)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	edit(&job.BuildStatus)
	job.UpdatedAt = time.Now().Unix()
}
func (b *Builder) Start(root, key string) (BuildStatus, error) {
	if key == "" {
		return BuildStatus{}, BuildError("request_key_required")
	}
	if !b.enabled() {
		return BuildStatus{}, BuildError("disabled")
	}
	root, err := canonical(root)
	if err != nil {
		return BuildStatus{}, err
	}
	if _, err := os.Lstat(filepath.Join(root, ".codegraph")); !os.IsNotExist(err) {
		if _, _, err := readManifest(root); err != nil {
			return BuildStatus{}, BuildError("index_exists")
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return BuildStatus{}, BuildError("unavailable")
	}
	if job := b.jobs[root]; job != nil && (job.key == key || job.State == "running") {
		return job.BuildStatus, nil
	}
	if b.active != "" {
		return BuildStatus{}, BuildError("busy")
	}
	now := time.Now().Unix()
	for name, job := range b.jobs {
		if job.State != "running" && now-job.UpdatedAt > BuildSecondsLimit {
			delete(b.jobs, name)
		}
	}
	if _, ok := b.jobs[root]; !ok && len(b.jobs) >= BuildJobsLimit {
		return BuildStatus{}, BuildError("history_full")
	}
	ctx, cancel := context.WithTimeout(b.ctx, time.Duration(BuildSecondsLimit)*time.Second)
	job := &buildJob{key: key, cancel: cancel, done: make(chan struct{}), BuildStatus: BuildStatus{State: "running", Phase: "scanning", StartedAt: now, UpdatedAt: now}}
	b.jobs[root] = job
	b.active = root
	go b.run(ctx, root, job)
	return job.BuildStatus, nil
}
func (b *Builder) run(ctx context.Context, root string, job *buildJob) {
	defer close(job.done)
	defer job.cancel()
	err := b.build(ctx, root, job)
	if err != nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.active = ""
	job.UpdatedAt = time.Now().Unix()
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
func (b *Builder) build(ctx context.Context, root string, job *buildJob) error {
	if _, _, err := readManifest(root); err == nil {
		if !b.native {
			return nil
		}
		if _, err = b.runtime(ctx, true); err != nil {
			return err
		}
		b.change(job, func(s *BuildStatus) { s.Phase = "starting" })
		s, err := b.ensureService(ctx, root)
		if err != nil {
			return err
		}
		var state serviceStatus
		return s.call(ctx, "sync", "", &state)
	}
	if err := os.MkdirAll(b.cache, 0700); err != nil {
		return err
	}
	// Source snapshots live outside every watched project. A vendor scanner
	// must never rediscover the snapshot while opening the published index.
	stage, err := os.MkdirTemp("", "clawdline-codegraph-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	resolvedStage, err := canonical(stage)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, resolvedStage)
	if (err != nil && strings.EqualFold(filepath.VolumeName(root), filepath.VolumeName(resolvedStage))) || (err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))))) {
		return BuildError("unsafe_source")
	}
	input, output := filepath.Join(stage, "input"), filepath.Join(stage, "output")
	if err = os.MkdirAll(input, 0700); err != nil {
		return err
	}
	if err = os.MkdirAll(output, 0700); err != nil {
		return err
	}
	sources, reading, err := scan(ctx, root, input)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return BuildError("no_sources")
	}
	b.change(job, func(s *BuildStatus) { s.Files = len(sources); s.Bytes = reading.Bytes })
	phase := func(name string) { b.change(job, func(s *BuildStatus) { s.Phase = name }) }
	if err = b.engine(ctx, input, output, phase); err != nil {
		return err
	}
	phase("checking")
	current, _, err := scan(ctx, root, "")
	if err != nil {
		return err
	}
	if !sameSources(sources, current) {
		return BuildError("source_changed")
	}
	if !b.enabled() {
		return BuildError("disabled")
	}
	native := filepath.Join(output, "native")
	if err = validateStore(ctx, native); err != nil {
		return err
	}
	var info struct {
		Configuration string `json:"configuration"`
	}
	if raw, err := os.ReadFile(filepath.Join(native, "clawdline-build.json")); err == nil {
		_ = json.Unmarshal(raw, &info)
	}
	rawSources, _ := json.Marshal(sources)
	hash := sha256.Sum256(rawSources)
	m := manifest{Schema: 1, Root: root, Version: Version, Sources: sources, Generation: hex.EncodeToString(hash[:]), Configuration: info.Configuration}
	raw, _ := json.Marshal(m)
	if len(raw) > MaxMetadataBytes {
		return BuildError("source_limit")
	}
	if err = os.WriteFile(filepath.Join(native, "clawdline.json"), raw, 0600); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(native, ".gitignore"), []byte("*\n"), 0600); err != nil {
		return err
	}
	if !b.enabled() || ctx.Err() != nil {
		return BuildError("disabled")
	}
	// Publication never replaces an index created by another process.
	publishStage, err := os.MkdirTemp(root, ".clawdline-codegraph-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(publishStage)
	candidate := filepath.Join(publishStage, "native")
	if err = os.Rename(native, candidate); err != nil {
		if err = copyNative(ctx, native, candidate); err != nil {
			return err
		}
	}
	if !b.enabled() || ctx.Err() != nil {
		return BuildError("disabled")
	}
	directory, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	err = publishDirectory(directory, filepath.ToSlash(filepath.Join(filepath.Base(publishStage), "native")), ".codegraph")
	directory.Close()
	if err != nil {
		return BuildError("index_exists")
	}
	if b.native {
		phase("starting")
		_, err = b.ensureService(ctx, root)
	}
	return err
}

// The cache and Project can be on different volumes. Copy only the closed,
// bounded native output into a same-volume publication candidate in that case.
func copyNative(ctx context.Context, source, destination string) error {
	var total int64
	entries := 0
	return filepath.WalkDir(source, func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		entries++
		if entries > MaxScanEntries {
			return BuildError("runtime_limit")
		}
		relative, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return BuildError("unsafe_source")
		}
		if st.Size() > StoreBytesLimit-total {
			return BuildError("runtime_limit")
		}
		input, err := os.Open(name)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		count, err := io.Copy(output, io.LimitReader(input, StoreBytesLimit-total+1))
		closeErr := output.Close()
		total += count
		if total > StoreBytesLimit {
			return BuildError("runtime_limit")
		}
		if err != nil {
			return err
		}
		return closeErr
	})
}
func sameSources(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for n, h := range a {
		if b[n] != h {
			return false
		}
	}
	return true
}
func (b *Builder) Close(ctx context.Context) error {
	b.mu.Lock()
	b.closed = true
	b.cancel()
	var jobDone <-chan struct{}
	if job := b.jobs[b.active]; job != nil {
		job.cancel()
		jobDone = job.done
	}
	services := b.services
	b.services = map[string]*service{}
	b.mu.Unlock()
	for _, s := range services {
		s.stop()
	}
	select {
	case <-b.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if jobDone != nil {
		select {
		case <-jobDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for _, s := range services {
		if err := s.join(ctx); err != nil {
			return err
		}
	}
	joined := make(chan struct{})
	go func() { b.servicesWG.Wait(); close(joined) }()
	select {
	case <-joined:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
