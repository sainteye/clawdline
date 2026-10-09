package git

import (
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// NotRepoAgeLimit is how long a directory git said is not a repository is
// answered that way without asking git again.
//
// A session sitting in a directory with no repository is asked about every
// time its Git panel opens, and over Cloud each ask was a pinned read that
// spawned `git status` to learn the same thing. A minute is short against
// somebody running `git init` and opening the panel again, and long against
// a panel reopened while they read.
const NotRepoAgeLimit = 60 * time.Second

// NotRepoRowsLimit is how many such directories are remembered. Past it the
// one remembered longest ago is let go; a miss is one `git status` behind the
// answer.
const NotRepoRowsLimit = 256

// NotRepos remembers the directories git last answered "not a repository" for.
// Only that answer is kept: a timeout, a missing directory or a refused one
// is asked again every time, because each of those can change on its own.
type NotRepos struct {
	mu   sync.Mutex
	age  time.Duration
	rows int
	now  func() time.Time
	seen map[string]time.Time
	// evicted and expired count what the two limits let go.
	evicted, expired int64
}

// NewNotRepos is the cache with the given bounds; nonpositive ones are the
// shipped limits.
func NewNotRepos(age time.Duration, rows int) *NotRepos {
	if age <= 0 {
		age = NotRepoAgeLimit
	}
	if rows <= 0 {
		rows = NotRepoRowsLimit
	}
	return &NotRepos{age: age, rows: rows, now: time.Now, seen: map[string]time.Time{}}
}

// Known is whether cwd was answered "not a repository" within the age limit.
func (n *NotRepos) Known(cwd string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	at, ok := n.seen[cwd]
	if !ok {
		return false
	}
	if since := n.now().Sub(at); since < 0 || since >= n.age {
		delete(n.seen, cwd)
		n.expired++
		return false
	}
	return true
}

// Remember records that git answered "not a repository" for cwd now.
func (n *NotRepos) Remember(cwd string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := n.now()
	for dir, at := range n.seen {
		if now.Sub(at) >= n.age {
			delete(n.seen, dir)
			n.expired++
		}
	}
	if _, ok := n.seen[cwd]; !ok && len(n.seen) >= n.rows {
		oldest, first := "", true
		for dir, at := range n.seen {
			if first || at.Before(n.seen[oldest]) {
				oldest, first = dir, false
			}
		}
		delete(n.seen, oldest)
		n.evicted++
	}
	n.seen[cwd] = now
}

// Forget drops cwd, for an answer from git that says otherwise.
func (n *NotRepos) Forget(cwd string) {
	n.mu.Lock()
	delete(n.seen, cwd)
	n.mu.Unlock()
}

// AgeReading is the capacity row for NotRepoAgeLimit: Used is the oldest
// remembered answer's age.
func (n *NotRepos) AgeReading() capacity.Reading {
	n.mu.Lock()
	defer n.mu.Unlock()
	reading := capacity.Reading{Known: true, WindowSeconds: int64(n.age / time.Second),
		Counters: capacity.Counters{Expired: n.expired}}
	now, oldest := n.now(), time.Time{}
	for _, at := range n.seen {
		if oldest.IsZero() || at.Before(oldest) {
			oldest = at
		}
	}
	if !oldest.IsZero() {
		reading.OldestAt = oldest
		if age := now.Sub(oldest); age > 0 {
			reading.Used = int64(age / time.Second)
		}
	}
	return reading
}

// RowsReading is the capacity row for NotRepoRowsLimit.
func (n *NotRepos) RowsReading() capacity.Reading {
	n.mu.Lock()
	defer n.mu.Unlock()
	return capacity.Reading{Known: true, Used: int64(len(n.seen)),
		Counters: capacity.Counters{Evicted: n.evicted}}
}
