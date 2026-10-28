package http

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/adapters/release/updater"
	"github.com/sainteye/clawdline/internal/contract"
)

// A dev build (`clawdline dev install`) is named v1.0.7-dev.<commit> and sorts
// before v1.0.7, so the release reads as available whether the dev build is
// twelve commits past it or five commits short of it. The version panel and
// the session list's strip need to tell those apart — "go back to v1.0.7"
// drops work, "update to v1.0.7" gains it — and only git can: release
// versions say nothing about ancestry. This is that reading.

// devStandingMemo holds one answer, and is read without waiting for git.
//
// **The request never computes it.** The first version ran git, and listed
// every Project, inside GET /v1/update under this lock. Listing Projects reads
// every live Session, which can take minutes on a busy machine, and every read
// of the route queued behind it: on 2026-10-15 GET /v1/update answered nothing
// for over two minutes while /v1/health answered in a millisecond, and
// `clawdline dev install`, which follows the switch by reading that route, hung
// at "switching". So a read answers what is held for the pair of commits —
// nothing yet, an unknown, or the standing — and starts one reading in the
// background when there is none running and none known.
//
// Two commits' histories never change, so a known answer stands until the
// running build or the latest release is another commit. An unknown one is
// asked again by the next read: right after a start the daemon has not listed
// its Projects yet, and the first reading of a dev build installed a moment
// before found none that held its commits.
type devStandingMemo struct {
	mu      sync.Mutex
	dev     string
	latest  string
	answer  *contract.UpdateDevStanding
	reading bool
}

// withDevStanding adds `dev` to a release install's status while it runs a
// dev build and the latest release names its commit, once it is known.
func (s *Server) withDevStanding(st contract.UpdateStatus) contract.UpdateStatus {
	v, err := release.ParseVersion(st.Running.Version)
	if err != nil || !v.IsDev() || st.Latest.Stamp == "" || st.Running.Stamp == "" {
		return st
	}
	dev, latest := st.Running.Stamp, st.Latest.Stamp
	m := &s.devStanding
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dev != dev || m.latest != latest {
		m.dev, m.latest, m.answer = dev, latest, nil
	}
	if m.answer != nil {
		answer := *m.answer
		st.Dev = &answer
	}
	known := m.answer != nil && m.answer.Relation != contract.UpdateDevRelationUnknown
	if !known && !m.reading {
		m.reading = true
		go s.readDevStanding(dev, latest)
	}
	return st
}

// readDevStanding is one background reading, kept only if the pair it was for
// is still the one asked about.
func (s *Server) readDevStanding(dev, latest string) {
	ctx, cancel := context.WithTimeout(context.Background(), updater.FetchTimeoutSecondsLimit*time.Second)
	defer cancel()
	answer := devStandingIn(ctx, s.placeDirectories(ctx), dev, latest)
	if answer.Relation == contract.UpdateDevRelationUnknown {
		log.Printf("update: how dev build %.7s stands against release %.7s is not known: %s", dev, latest, answer.Reason)
	}
	m := &s.devStanding
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reading = false
	if m.dev == dev && m.latest == latest {
		m.answer = &answer
	}
}

// placeDirectories is the directory of every Place a dev build's commit is
// looked for in, leaving out personal folders and unchosen aliases. A Dropbox
// folder listed as a Place answered no git command for twenty seconds on
// 2026-10-15; a git process started there can make a macOS prompt name Clawdline.
func (s *Server) placeDirectories(ctx context.Context) []string {
	var dirs []string
	for _, place := range s.squadListedPlaces(ctx) {
		if !projects.MayInspectInBackground(place.Path) {
			continue
		}
		dirs = append(dirs, place.Path)
	}
	return dirs
}

func gitIn(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// devStandingIn is how dev stands against latest in a repository, among dirs,
// that holds both commits. It does not fetch: the dev build was built from a
// checkout here, and a release commit that checkout has never seen is
// reported unknown rather than waited for.
//
// **Every directory is asked at once.** Asked one after another, the first
// version spent its whole time on a Place in a cloud-storage folder whose git
// never answered, and never reached the clawdline checkout listed after it.
// The first directory that holds both commits is used and the others are
// stopped.
func devStandingIn(ctx context.Context, dirs []string, dev, latest string) contract.UpdateDevStanding {
	unknown := func(why string) contract.UpdateDevStanding {
		return contract.UpdateDevStanding{Relation: contract.UpdateDevRelationUnknown, Reason: why}
	}
	if dev == latest {
		return contract.UpdateDevStanding{Relation: contract.UpdateDevRelationSame}
	}
	ask, stop := context.WithCancel(ctx)
	defer stop()
	// Unbuffered: a directory found after the first waits on stop and leaves.
	holders := make(chan string)
	var wg sync.WaitGroup
	for _, dir := range dirs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := gitIn(ask, dir, "cat-file", "-e", dev+"^{commit}"); err != nil {
				return
			}
			if _, err := gitIn(ask, dir, "cat-file", "-e", latest+"^{commit}"); err != nil {
				return
			}
			select {
			case holders <- dir:
			case <-ask.Done():
			}
		}()
	}
	go func() { wg.Wait(); close(holders) }()
	repo, found := <-holders
	stop()
	if !found {
		why := fmt.Sprintf("no Project on this machine holds both the dev build's commit and the release's (%d looked in)", len(dirs))
		if err := ctx.Err(); err != nil {
			why += "; the reading ran out of time: " + err.Error()
		}
		return unknown(why)
	}
	// Left is the release's own commits, right the dev build's.
	out, err := gitIn(ctx, repo, "rev-list", "--left-right", "--count", latest+"..."+dev)
	if err != nil {
		return unknown("git rev-list did not answer in " + repo + ": " + err.Error())
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return unknown("git rev-list answered " + strconv.Quote(out) + " in " + repo)
	}
	behind, err1 := strconv.ParseInt(fields[0], 10, 64)
	ahead, err2 := strconv.ParseInt(fields[1], 10, 64)
	if err1 != nil || err2 != nil {
		return unknown("git rev-list answered " + strconv.Quote(out) + " in " + repo)
	}
	standing := contract.UpdateDevStanding{Ahead: ahead, Behind: behind}
	switch {
	case ahead == 0 && behind == 0:
		standing.Relation = contract.UpdateDevRelationSame
	case behind == 0:
		standing.Relation = contract.UpdateDevRelationAhead
	case ahead == 0:
		standing.Relation = contract.UpdateDevRelationBehind
	default:
		standing.Relation = contract.UpdateDevRelationDiverged
	}
	return standing
}
