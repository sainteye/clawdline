package http

import (
	"context"
	"log"
	"os"
	"runtime"
	"syscall"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/browserlaunch"
	"github.com/sainteye/clawdline/internal/domain/browserpolicy"
)

// Extension upkeep (docs/browser.md, "Updating the extension"). Chrome reads
// an unpacked extension's files when it starts; an extension cannot reload
// itself into new files (chrome.runtime.reload removed it in every measured
// case, 2026-11-14). So a new release only has to be in the stable folder:
//
//   - the person's usual Chrome picks it up at its next start, or at once
//     from the reload arrow in chrome://extensions;
//   - Clawdline's own test Chrome is relaunched here, once per release, when
//     its channel is quiet, since closing it closes only its own tabs.
const (
	browserUpkeepTick  = 30 * time.Second
	browserQuietPeriod = 2 * time.Minute
	browserStopWait    = 10 * time.Second
)

// StartBrowserUpkeep writes this build's extension into the stable folder and
// then watches the test Chrome until ctx ends.
func (s *Server) StartBrowserUpkeep(ctx context.Context) {
	if s.cfg.Dir == "" {
		return
	}
	if changed, err := browserlaunch.WriteStableExtension(s.cfg.Dir); err != nil {
		log.Printf("browser: the stable extension folder could not be written: %v", err)
	} else if changed {
		log.Printf("browser: the stable extension folder now holds %s", browserlaunch.EmbeddedVersion())
	}
	go func() {
		tick := time.NewTicker(browserUpkeepTick)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				s.relaunchStaleTestChrome(time.Now())
			}
		}
	}()
}

// relaunchStaleTestChrome restarts Clawdline's test Chrome when it runs an
// older extension than this build carries and nothing is using it. One
// attempt per release: a Chrome that comes back still old is left alone.
func (s *Server) relaunchStaleTestChrome(now time.Time) {
	want := browserlaunch.EmbeddedVersion()
	if want == "" || runtime.GOOS == "windows" {
		return
	}
	b, err := s.browsers().Channel(browserpolicy.Isolated)
	if err != nil || b == nil {
		return
	}
	st := b.Status()
	if !st.Connected || st.Host.Version == "" || st.Host.Version == want {
		return
	}
	s.browserMu.Lock()
	tried := s.browserRelaunched == want
	if !tried {
		s.browserRelaunched = want
	}
	s.browserMu.Unlock()
	if tried || !b.Quiet(now, browserQuietPeriod) {
		if !tried {
			// Not quiet yet: allow another attempt on a later tick.
			s.browserMu.Lock()
			s.browserRelaunched = ""
			s.browserMu.Unlock()
		}
		return
	}
	pid := browserlaunch.RunningPID(s.cfg.Dir)
	if pid <= 0 {
		return
	}
	process, err := os.FindProcess(pid)
	if err != nil || process.Signal(syscall.SIGTERM) != nil {
		log.Printf("browser: the test Chrome (pid %d) could not be asked to quit: %v", pid, err)
		return
	}
	for deadline := time.Now().Add(browserStopWait); browserlaunch.Running(s.cfg.Dir) && time.Now().Before(deadline); {
		time.Sleep(200 * time.Millisecond)
	}
	b.ForgetAll()
	if _, err := browserlaunch.Launch(browserlaunch.Options{Dir: s.cfg.Dir, Port: s.cfg.Port, Browser: findChromium()}); err != nil {
		log.Printf("browser: the test Chrome could not be started with extension %s: %v", want, err)
		return
	}
	log.Printf("browser: relaunched the test Chrome to load extension %s (was %s)", want, st.Host.Version)
}
