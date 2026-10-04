// Package updatecheck answers whether this machine trails the cloud's latest
// build (docs/updates.md).
//
// On 2026-10-04 app.clawdline.com served BUILD.json stamp 4b7c3f8d while a
// machine's daemon served 08b4277b, and nothing said so: the console showed
// "讀取失敗" beside features that daemon predated. This package reads both
// stamps and says which is later.
//
// "Latest" is what the hosted console serves at <origin>/BUILD.json. "Running"
// is the BUILD.json in the web dist this daemon serves, falling back to the
// binary's own vcs.revision. A request never waits on the network: Checker
// answers from its last check, and a background loop refreshes it.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/contract"
)

// Bounds, registered in internal/domain/capacity.
const (
	// RefreshSecondsLimit is how old the last answer gets before the
	// background loop asks the cloud again.
	RefreshSecondsLimit = 1800
	// FetchTimeoutSecondsLimit is how long one ask of the cloud may take.
	FetchTimeoutSecondsLimit = 10
	// maxBuildBodyBytes caps a BUILD.json read, from the cloud or from disk.
	// The real one is under 100 bytes.
	maxBuildBodyBytes = 4096
)

// Environment switches.
const (
	URLEnv   = "CLAWDLINE_NEXT_UPDATE_URL"
	CheckEnv = "CLAWDLINE_NEXT_UPDATE_CHECK"
)

// SourceURL is the BUILD.json this machine compares itself against.
func SourceURL() string {
	if v := strings.TrimSpace(os.Getenv(URLEnv)); v != "" {
		return v
	}
	return strings.TrimRight(cloud.DefaultAppOrigin, "/") + "/BUILD.json"
}

// Disabled says the check was turned off with CLAWDLINE_NEXT_UPDATE_CHECK=off.
func Disabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(CheckEnv)), "off")
}

// buildFile is BUILD.json. committed_at is absent in files written before
// 2026-10-04, and that is not an error.
type buildFile struct {
	Stamp       string `json:"stamp"`
	CommittedAt string `json:"committed_at,omitempty"`
}

// ParseBuild reads one BUILD.json body.
func ParseBuild(body []byte) (contract.BuildStamp, error) {
	var f buildFile
	if err := json.Unmarshal(body, &f); err != nil {
		return contract.BuildStamp{}, fmt.Errorf("BUILD.json is not JSON: %w", err)
	}
	f.Stamp = strings.TrimSpace(f.Stamp)
	if f.Stamp == "" {
		return contract.BuildStamp{}, errors.New("BUILD.json has no stamp")
	}
	return contract.BuildStamp{Stamp: f.Stamp, CommittedAt: strings.TrimSpace(f.CommittedAt)}, nil
}

// Running is this daemon's own build: the served dist's BUILD.json when there
// is one, else the binary's vcs.revision (with no commit time).
func Running(webRoot string) contract.BuildStamp {
	if webRoot != "" {
		if f, err := os.Open(filepath.Join(webRoot, "BUILD.json")); err == nil {
			body, rerr := io.ReadAll(io.LimitReader(f, maxBuildBodyBytes))
			f.Close()
			if rerr == nil {
				if b, err := ParseBuild(body); err == nil {
					return b
				}
			}
		}
	}
	return contract.BuildStamp{Stamp: vcsRevision()}
}

func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return ""
}

// Fetch asks url for its BUILD.json, within FetchTimeoutSecondsLimit and
// maxBuildBodyBytes.
func Fetch(ctx context.Context, client *http.Client, url string) (contract.BuildStamp, error) {
	ctx, cancel := context.WithTimeout(ctx, FetchTimeoutSecondsLimit*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return contract.BuildStamp{}, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return contract.BuildStamp{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBuildBodyBytes+1))
	if err != nil {
		return contract.BuildStamp{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return contract.BuildStamp{}, fmt.Errorf("%s answered %d", url, resp.StatusCode)
	}
	if len(body) > maxBuildBodyBytes {
		return contract.BuildStamp{}, fmt.Errorf("%s answered more than %d bytes", url, maxBuildBodyBytes)
	}
	return ParseBuild(body)
}

// Compare decides the state from two builds. reason is set only for unknown.
func Compare(running, latest contract.BuildStamp) (state contract.UpdateState, reason string) {
	switch {
	case running.Stamp == "":
		return contract.UpdateStateUnknown, "this build does not know its own commit"
	case latest.Stamp == "":
		return contract.UpdateStateUnknown, "the cloud's latest build has not been read yet"
	case sameCommit(running.Stamp, latest.Stamp):
		return contract.UpdateStateCurrent, ""
	}
	rt, rerr := time.Parse(time.RFC3339, running.CommittedAt)
	lt, lerr := time.Parse(time.RFC3339, latest.CommittedAt)
	if rerr != nil || lerr != nil {
		return contract.UpdateStateDiffers, ""
	}
	switch {
	case lt.After(rt):
		return contract.UpdateStateUpdateAvailable, ""
	case rt.After(lt):
		return contract.UpdateStateAhead, ""
	}
	// Two commits at the same second: neither is known to be later.
	return contract.UpdateStateDiffers, ""
}

// sameCommit lets an abbreviated stamp (at least seven characters) match the
// full one, so a hand-written BUILD.json with a short hash still compares.
func sameCommit(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if len(a) > len(b) {
		a, b = b, a
	}
	return len(a) >= 7 && strings.HasPrefix(b, a)
}

// Checker holds the last answer and refreshes it in the background.
type Checker struct {
	URL      string
	Running  func() contract.BuildStamp
	Client   *http.Client
	Disabled bool
	Now      func() time.Time

	mu        sync.Mutex
	latest    contract.BuildStamp
	checkedAt time.Time
	lastErr   string
}

// New is the daemon's checker, configured from the environment.
func New(webRoot string) *Checker {
	return &Checker{
		URL:      SourceURL(),
		Running:  func() contract.BuildStamp { return Running(webRoot) },
		Disabled: Disabled(),
	}
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Refresh asks the cloud once. A failure keeps the last good answer and
// records the error beside it.
func (c *Checker) Refresh(ctx context.Context) {
	if c.Disabled {
		return
	}
	b, err := Fetch(ctx, c.Client, c.URL)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.lastErr = err.Error()
		return
	}
	c.latest, c.checkedAt, c.lastErr = b, c.now().UTC(), ""
}

// Run refreshes now and every RefreshSecondsLimit until ctx ends.
func (c *Checker) Run(ctx context.Context) {
	if c.Disabled {
		return
	}
	c.Refresh(ctx)
	t := time.NewTicker(RefreshSecondsLimit * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.Refresh(ctx)
		}
	}
}

// Status is the answer from what is held; it never touches the network.
func (c *Checker) Status() contract.UpdateStatus {
	running := contract.BuildStamp{}
	if c.Running != nil {
		running = c.Running()
	}
	out := contract.UpdateStatus{Running: running, SourceURL: c.URL}
	if c.Disabled {
		out.State, out.Reason = contract.UpdateStateUnknown, "the update check is off ("+CheckEnv+"=off)"
		return out
	}
	c.mu.Lock()
	out.Latest, out.Error = c.latest, c.lastErr
	if !c.checkedAt.IsZero() {
		out.CheckedAt = c.checkedAt.Format(time.RFC3339)
	}
	c.mu.Unlock()
	out.State, out.Reason = Compare(out.Running, out.Latest)
	if out.State == contract.UpdateStateUnknown && out.Latest.Stamp == "" && out.Error != "" {
		out.Reason = "the cloud's latest build could not be read: " + out.Error
	}
	return out
}
