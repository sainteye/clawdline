// Package doctor produces content-free support evidence. It never reads credentials,
// starts an assistant conversation, or contacts a provider.
package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/providerquota"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

const (
	HistoryRowsLimit    = 24
	HistoryBytesLimit   = 32 << 10
	HistorySecondsLimit = 3 * 24 * 60 * 60
	VersionSecondsLimit = 3
	VersionBytesLimit   = 4096
	ReportBytesLimit    = 8192
)

type Provider struct {
	ID          string   `json:"id"`
	Source      string   `json:"source"`
	State       string   `json:"state"`
	Problem     string   `json:"problem,omitempty"`
	ObservedAt  *int64   `json:"observed_at"`
	UsedPercent *float64 `json:"used_percent"`
	Stale       bool     `json:"stale"`
}
type Event struct {
	At       int64    `json:"at"`
	Provider Provider `json:"provider"`
}
type Tool struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Version string `json:"version,omitempty"`
}
type Coverage struct {
	LostUnknown bool   `json:"lost_unknown"`
	State       string `json:"state"`
	Dropped     int64  `json:"dropped"`
	From        *int64 `json:"from,omitempty"`
	Through     *int64 `json:"through,omitempty"`
}
type Report struct {
	Schema    string     `json:"schema"`
	At        int64      `json:"at"`
	Version   string     `json:"version"`
	OS        string     `json:"os"`
	Arch      string     `json:"arch"`
	Tools     []Tool     `json:"tools"`
	Providers []Provider `json:"providers"`
	History   []Event    `json:"history"`
	Coverage  Coverage   `json:"coverage"`
	Gaps      []string   `json:"gaps"`
}
type journal struct {
	LostUnknown bool    `json:"lost_unknown"`
	Events      []Event `json:"events"`
	Dropped     int64   `json:"dropped"`
	Through     int64   `json:"through"`
}

// Recorder is bounded local evidence, not an upload queue. Its failure cannot
// replace the quota response. Unchanged observations do not rewrite the file.
type Recorder struct {
	mu     sync.Mutex
	dir    string
	loaded bool
	data   journal
	state  string
}

func New(dir string) *Recorder {
	if dir == "" {
		return &Recorder{state: "not_recorded"}
	}
	return &Recorder{dir: filepath.Join(dir, "doctor"), state: "not_recorded"}
}

func problem(p string) string {
	switch p {
	case "", "no_record", "no_reading", "unreadable", "too_old", "not_signed_in", "reauth_required", "invalid_response", "unavailable", "rate_limited":
		return p
	default:
		return "unavailable"
	}
}
func Project(row providerquota.Row) (Provider, bool) {
	var source string
	switch row.ID {
	case "claude":
		source = "claude_statusline"
	case "codex":
		source = "codex_rollout"
	case "gemini", "grok":
		source = "provider_api"
	default:
		return Provider{}, false
	}
	p := Provider{ID: row.ID, Source: source, State: "unknown", Problem: problem(row.Problem), ObservedAt: row.ObservedAt, Stale: row.Stale}
	for _, w := range row.Windows {
		if math.IsNaN(w.UsedPercent) || math.IsInf(w.UsedPercent, 0) || w.UsedPercent < 0 || w.UsedPercent > 100 {
			continue
		}
		if p.UsedPercent == nil || *p.UsedPercent < w.UsedPercent {
			v := w.UsedPercent
			p.UsedPercent = &v
		}
	}
	if p.UsedPercent != nil {
		p.State = "ok"
	}
	if p.Stale {
		p.State = "stale"
	}
	if p.Problem != "" {
		p.State = "failed"
	}
	return p, true
}
func (r *Recorder) load() {
	if r.dir == "" {
		return
	}
	if r.loaded {
		return
	}
	r.loaded = true
	f, err := os.Open(filepath.Join(r.dir, "history.json"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		r.state = "unreadable"
		r.data.LostUnknown = true
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, HistoryBytesLimit+1))
	var j journal
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err != nil || len(b) > HistoryBytesLimit || dec.Decode(&j) != nil || dec.Decode(&struct{}{}) != io.EOF || len(j.Events) > HistoryRowsLimit || j.Dropped < 0 {
		r.state = "unreadable"
		r.data.LostUnknown = true
		return
	}
	for _, e := range j.Events {
		p, ok := Project(providerquota.Row{ID: e.Provider.ID, Problem: e.Provider.Problem})
		if !ok || p.Source != e.Provider.Source || problem(e.Provider.Problem) != e.Provider.Problem || e.At <= 0 ||
			(e.Provider.State != "ok" && e.Provider.State != "stale" && e.Provider.State != "failed" && e.Provider.State != "unknown") ||
			(e.Provider.UsedPercent != nil && (*e.Provider.UsedPercent < 0 || *e.Provider.UsedPercent > 100)) {
			r.state = "unreadable"
			r.data.LostUnknown = true
			return
		}
	}
	r.data = j
	r.state = "ok"
}
func (r *Recorder) prune(now int64) {
	kept := r.data.Events[:0]
	for _, e := range r.data.Events {
		if now-e.At > HistorySecondsLimit {
			r.data.Dropped++
		} else {
			kept = append(kept, e)
		}
	}
	r.data.Events = kept
	if n := len(kept) - HistoryRowsLimit; n > 0 {
		r.data.Dropped += int64(n)
		r.data.Events = kept[n:]
	}
}
func (r *Recorder) Record(s providerquota.Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	changed := false
	for _, row := range s.Providers {
		p, ok := Project(row)
		if !ok {
			continue
		}
		var last *Provider
		for i := len(r.data.Events) - 1; i >= 0; i-- {
			if r.data.Events[i].Provider.ID == p.ID {
				last = &r.data.Events[i].Provider
				break
			}
		}
		a, _ := json.Marshal(last)
		b, _ := json.Marshal(p)
		if string(a) == string(b) {
			continue
		}
		r.data.Events = append(r.data.Events, Event{At: s.At, Provider: p})
		changed = true
	}
	before := len(r.data.Events)
	r.prune(s.At)
	if !changed && before == len(r.data.Events) {
		return
	}
	r.data.Through = s.At
	if err := r.save(); err != nil {
		r.state = "write_failed"
	} else {
		r.state = "ok"
	}
}
func (r *Recorder) save() error {
	if r.dir == "" {
		return errors.New("history_unconfigured")
	}
	b, err := json.Marshal(r.data)
	if err != nil || len(b) > HistoryBytesLimit {
		return errors.New("history_too_large")
	}
	if err = os.MkdirAll(r.dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(r.dir, ".history-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, filepath.Join(r.dir, "history.json"))
}
func (r *Recorder) Evidence(now time.Time) ([]Event, Coverage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	before := len(r.data.Events)
	r.prune(now.Unix())
	if before != len(r.data.Events) {
		if err := r.save(); err != nil {
			r.state = "write_failed"
		}
	}
	h := append([]Event{}, r.data.Events...)
	c := Coverage{State: r.state, Dropped: r.data.Dropped, LostUnknown: r.data.LostUnknown}
	if len(h) > 0 {
		v := h[0].At
		c.From = &v
	}
	if r.data.Through > 0 {
		v := r.data.Through
		c.Through = &v
	}
	return h, c
}

// Reading measures retained local evidence without probing CLIs or rewriting it.
func (r *Recorder) Reading(bytes bool) capacity.Reading {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	if r.state == "unreadable" {
		return capacity.Unmeasured("Doctor history is unreadable")
	}
	out := capacity.Reading{Known: true, Used: int64(len(r.data.Events)), Failing: r.state == "write_failed", Note: "retained local Doctor transitions"}
	if len(r.data.Events) > 0 {
		out.OldestAt = time.Unix(r.data.Events[0].At, 0)
	}
	if !bytes {
		return out
	}
	info, err := os.Stat(filepath.Join(r.dir, "history.json"))
	if errors.Is(err, os.ErrNotExist) {
		out.Used = 0
		return out
	}
	if err != nil {
		return capacity.Unmeasured("Doctor history size is unreadable")
	}
	out.Used = info.Size()
	return out
}

var versionPattern = regexp.MustCompile(`^(?:v|codex-cli )?([0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?)(?: \(Claude Code\))?$`)
var buildPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]{0,95}$`)

func SafeBuild(v string) string {
	if buildPattern.MatchString(v) {
		return v
	}
	return "unknown"
}

type limitedOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	room := VersionBytesLimit - b.Len()
	if len(p) > room {
		b.overflow = true
		p = p[:room]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

// Probe runs only the fixed --version argument, never a shell or a prompt.
// Only a recognized version survives; neither stdout nor stderr is exported.
func Probe(ctx context.Context, id string) Tool {
	t := Tool{ID: id, State: "not_found"}
	path, err := exec.LookPath(id)
	if err != nil {
		return t
	}
	ctx, cancel := context.WithTimeout(ctx, VersionSecondsLimit*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	cmd.WaitDelay = VersionSecondsLimit * time.Second
	var out limitedOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	err = cmd.Run()
	t.State = "failed"
	if ctx.Err() != nil {
		t.State = "timeout"
		return t
	}
	if err != nil || out.overflow {
		return t
	}
	m := versionPattern.FindStringSubmatch(strings.TrimSpace(out.String()))
	if len(m) != 2 {
		t.State = "unrecognized"
		return t
	}
	t.State, t.Version = "ok", m[1]
	return t
}
func Build(ctx context.Context, version string, snapshot providerquota.Snapshot, recorder *Recorder) Report {
	r := Report{Schema: "clawdline.doctor/1", At: time.Now().Unix(), Version: SafeBuild(version), OS: runtime.GOOS, Arch: runtime.GOARCH,
		Tools: []Tool{}, Providers: []Provider{}, Gaps: []string{"claude_auth_not_inspected", "upstream_claude_usage_not_observed", "history_only_when_quota_is_read", "browser_render_not_observed"}}
	var tools [2]Tool
	var wg sync.WaitGroup
	for i, id := range []string{"claude", "codex"} {
		wg.Add(1)
		go func(i int, id string) { defer wg.Done(); tools[i] = Probe(ctx, id) }(i, id)
	}
	wg.Wait()
	r.Tools = tools[:]
	for _, p := range snapshot.Providers {
		if v, ok := Project(p); ok {
			r.Providers = append(r.Providers, v)
		}
	}
	r.History, r.Coverage = recorder.Evidence(time.Now())
	return r
}
