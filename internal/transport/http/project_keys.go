package http

import (
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// ProjectKeyCacheLimit is how many working directories' Project keys the
// session list remembers. A machine runs a few dozen Sessions in far fewer
// directories; past the limit the answer read longest ago is let go and read
// again when a row next names that directory.
const ProjectKeyCacheLimit = 256

// projectKeyFresh is how long a remembered answer stands. The claim path asks
// the filesystem every time; a repository created or a worktree linked under a
// running Session is seen by the list within this long, so the dialog and the
// claim disagree for no longer than that.
const projectKeyFresh = 30 * time.Second

// projectKeyCache answers which Project a Session's working directory belongs
// to with the very function the claim path uses (sessionProjectKey), held per
// directory so a list built every two seconds does not walk the filesystem for
// every row each time.
type projectKeyCache struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]projectKeyEntry
	evicted int64
	lastAt  time.Time
}

type projectKeyEntry struct {
	key  string
	ok   bool
	read time.Time
}

// sessionProjectKey is the Project a working directory belongs to, as both the
// claim (sessionInProject) and the session list's `project_key` decide it.
func sessionProjectKey(cwd string) (string, bool) {
	return projects.CanonicalProjectKey(cwd)
}

// key is sessionProjectKey(cwd), remembered for projectKeyFresh.
func (c *projectKeyCache) key(cwd string) (string, bool) {
	// This optional list decoration must not inspect an unrelated process's
	// personal directory just because that process is visible in inventory.
	if cwd == "" || !projects.MayInspectInBackground(cwd) {
		return "", false
	}
	c.mu.Lock()
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	at := now()
	if e, hit := c.entries[cwd]; hit && at.Sub(e.read) < projectKeyFresh {
		c.mu.Unlock()
		return e.key, e.ok
	}
	c.mu.Unlock()
	// Read outside the lock: a slow filesystem holds up only this row.
	key, ok := sessionProjectKey(cwd)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]projectKeyEntry{}
	}
	if _, held := c.entries[cwd]; !held && len(c.entries) >= ProjectKeyCacheLimit {
		oldest, first := "", true
		var oldestAt time.Time
		for dir, e := range c.entries {
			if first || e.read.Before(oldestAt) {
				oldest, oldestAt, first = dir, e.read, false
			}
		}
		delete(c.entries, oldest)
		c.evicted++
		c.lastAt = at
	}
	c.entries[cwd] = projectKeyEntry{key: key, ok: ok, read: at}
	return key, ok
}

// Reading is the cache's row in the capacity register.
func (c *projectKeyCache) Reading() capacity.Reading {
	c.mu.Lock()
	defer c.mu.Unlock()
	return capacity.Reading{Known: true, Used: int64(len(c.entries)),
		Counters: capacity.Counters{Evicted: c.evicted, LastActionAt: c.lastAt}}
}
