package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/release"
)

// Base is where the manifest of channel (or of one version) lives:
// <base>/manifest.json and <base>/manifest.sig.json.
func (e Env) Base(ctx context.Context, channel, version string) (string, error) {
	if version != "" {
		if _, err := release.ParseVersion(version); err != nil {
			return "", err
		}
		if e.ReleaseURL != "" {
			i := strings.LastIndex(e.ReleaseURL, "/")
			if i < 0 {
				return "", fail(CodeReleaseUnreachable, "%s=%s has no path to put a version in", ReleaseURLEnv, e.ReleaseURL)
			}
			return e.ReleaseURL[:i+1] + version, nil
		}
		return DefaultDownloadRoot + "/" + version, nil
	}
	if e.ReleaseURL != "" {
		return e.ReleaseURL, nil
	}
	if channel != ChannelBeta {
		return DefaultStableBase, nil
	}
	tag, err := e.newestTag(ctx)
	if err != nil {
		return "", err
	}
	return DefaultDownloadRoot + "/" + tag, nil
}

// newestTag is the newest published (not draft) release of any kind, from
// the GitHub Releases API: the beta channel's pointer.
func (e Env) newestTag(ctx context.Context) (string, error) {
	api := e.ReleasesAPI
	if api == "" {
		api = DefaultReleasesAPI
	}
	body, err := e.get(ctx, api, maxReleaseListBytes)
	if err != nil {
		return "", err
	}
	var list []struct {
		Tag   string `json:"tag_name"`
		Draft bool   `json:"draft"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return "", fail(CodeReleaseUnreachable, "the release list is not JSON: %v", err)
	}
	var best release.Version
	tag := ""
	for _, r := range list {
		if r.Draft {
			continue
		}
		v, err := release.ParseVersion(r.Tag)
		if err != nil {
			continue
		}
		if tag == "" || release.Compare(v, best) > 0 {
			best, tag = v, r.Tag
		}
	}
	if tag == "" {
		return "", fail(CodeNoReleasePublished, "no release is published yet")
	}
	return tag, nil
}

// get reads url within FetchTimeoutSecondsLimit and limit bytes. A 404 is
// no_release_published; any other failure is release_unreachable.
func (e Env) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, FetchTimeoutSecondsLimit*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fail(CodeReleaseUnreachable, "%v", err)
	}
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := e.client().Do(req)
	if err != nil {
		return nil, fail(CodeReleaseUnreachable, "%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fail(CodeNoReleasePublished, "%s answered 404", url)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fail(CodeReleaseUnreachable, "%s answered %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fail(CodeReleaseUnreachable, "reading %s: %v", url, err)
	}
	if int64(len(body)) > limit {
		return nil, &release.Error{Code: release.CodeManifestMalformed, Detail: fmt.Sprintf("%s is longer than %d bytes", url, limit)}
	}
	return body, nil
}

// Fetch reads and verifies the manifest at base. Nothing of it is believed
// until release.Open has checked its signature.
func (e Env) Fetch(ctx context.Context, base string) (release.Manifest, error) {
	m, err := e.get(ctx, base+"/manifest.json", release.MaxManifestBytes())
	if err != nil {
		return release.Manifest{}, err
	}
	s, err := e.get(ctx, base+"/manifest.sig.json", release.MaxSignatureBytes())
	if err != nil {
		if ue, ok := err.(*Error); ok && ue.Code == CodeNoReleasePublished {
			return release.Manifest{}, &release.Error{Code: release.CodeManifestUnsigned, Detail: "the release has a manifest and no manifest.sig.json"}
		}
		return release.Manifest{}, err
	}
	return release.Open(m, s, e.Keys)
}

// Checker holds the last release check. A request reads it and never waits
// on the network.
type Checker struct {
	Env     Env
	Channel string
	// Jitter is the random part of the wait; nil uses math/rand.
	Jitter func() time.Duration

	mu        sync.Mutex
	latest    *release.Manifest
	base      string
	checkedAt time.Time
	lastErr   error
}

// Check is one reading of the checker.
type Check struct {
	Latest    *release.Manifest
	Base      string
	CheckedAt time.Time
	Err       error
}

// Refresh reads the channel's manifest once. A failure keeps the last good
// manifest and records the error beside it.
func (c *Checker) Refresh(ctx context.Context) Check {
	base, err := c.Env.Base(ctx, c.Channel, "")
	var m release.Manifest
	if err == nil {
		m, err = c.Env.Fetch(ctx, base)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if base != "" {
		c.base = base
	}
	if err != nil {
		c.lastErr = err
		// A release taken down is not the last good one any more.
		if ue, ok := err.(*Error); ok && ue.Code == CodeNoReleasePublished {
			c.latest = nil
		}
	} else {
		c.latest, c.checkedAt, c.lastErr = &m, c.Env.now(), nil
	}
	return c.snapshot()
}

// Last is the last check, without asking anything.
func (c *Checker) Last() Check {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshot()
}

func (c *Checker) snapshot() Check {
	out := Check{Base: c.base, CheckedAt: c.checkedAt, Err: c.lastErr}
	if c.latest != nil {
		m := *c.latest
		out.Latest = &m
	}
	return out
}

// Run checks now and then every CheckIntervalSecondsLimit plus up to
// CheckJitterSecondsLimit, until ctx ends, calling after with each check.
func (c *Checker) Run(ctx context.Context, after func(context.Context, Check)) {
	for {
		got := c.Refresh(ctx)
		if after != nil {
			after(ctx, got)
		}
		wait := CheckIntervalSecondsLimit * time.Second
		if c.Jitter != nil {
			wait += c.Jitter()
		} else {
			wait += time.Duration(rand.Int64N(CheckJitterSecondsLimit)) * time.Second
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
