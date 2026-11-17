//go:build darwin || linux

package browserlaunch

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// OrphanTestBrowser represents an observed test browser process that has lost its parent.
type OrphanTestBrowser struct {
	PID     int
	PPID    int
	Command string
	TempDir string
}

// ReapOrphanedTestBrowsers finds and terminates orphaned Chromium/Chrome for Testing
// processes running with disposable temporary profiles, returning the number of reaped processes.
func ReapOrphanedTestBrowsers() int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	orphans := findOrphanedTestBrowsers(ctx)
	if len(orphans) == 0 {
		return 0
	}

	reaped := 0
	for _, o := range orphans {
		proc, err := os.FindProcess(o.PID)
		if err != nil {
			continue
		}
		if err := proc.Signal(syscall.SIGTERM); err == nil {
			reaped++
			log.Printf("browser: reaped orphaned test browser (pid %d, ppid %d)", o.PID, o.PPID)
			if o.TempDir != "" && isDisposableTempProfile(o.TempDir) {
				go func(dir string) {
					time.Sleep(500 * time.Millisecond)
					_ = os.RemoveAll(dir)
				}(o.TempDir)
			}
		}
	}
	return reaped
}

// findOrphanedTestBrowsers lists processes and returns those matching orphaned test browsers.
func findOrphanedTestBrowsers(ctx context.Context) []OrphanTestBrowser {
	cmd := exec.CommandContext(ctx, "ps", "-axo", "pid,ppid,command")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseOrphanedTestBrowsers(out)
}

func parseOrphanedTestBrowsers(out []byte) []OrphanTestBrowser {
	var orphans []OrphanTestBrowser
	lines := bytes.Split(out, []byte("\n"))
	for _, line := range lines {
		fields := strings.Fields(string(line))
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil || pid <= 0 {
			continue
		}
		command := strings.Join(fields[2:], " ")
		// Must be orphaned: PPID == 1 (adopted by init / launchd)
		if ppid != 1 {
			continue
		}
		// Must be a test browser binary
		if !isTestBrowserCommand(command) {
			continue
		}
		// Extract user-data-dir
		tempDir := extractUserDataDir(command)
		if tempDir == "" || !isDisposableTempProfile(tempDir) {
			continue
		}
		orphans = append(orphans, OrphanTestBrowser{
			PID:     pid,
			PPID:    ppid,
			Command: command,
			TempDir: tempDir,
		})
	}
	return orphans
}

func isTestBrowserCommand(command string) bool {
	// Exclude normal user browsers
	if strings.Contains(command, "/Applications/Google Chrome.app") ||
		strings.Contains(command, "Google Chrome.app/Contents/MacOS/Google Chrome ") && !strings.Contains(command, "Chrome for Testing") {
		return false
	}
	return strings.Contains(command, "Google Chrome for Testing") ||
		strings.Contains(command, "chrome_crashpad_handler") && strings.Contains(command, "Chrome for Testing") ||
		strings.Contains(command, "headless_shell") ||
		strings.Contains(command, "ms-playwright/chromium")
}

func extractUserDataDir(command string) string {
	const flag = "--user-data-dir="
	idx := strings.Index(command, flag)
	if idx == -1 {
		return ""
	}
	sub := command[idx+len(flag):]
	end := strings.IndexAny(sub, " \t\r\n")
	if end == -1 {
		return sub
	}
	return sub[:end]
}

func isDisposableTempProfile(dir string) bool {
	clean := filepath.Clean(dir)
	tmp := os.TempDir()
	isTemp := strings.HasPrefix(clean, "/tmp") ||
		strings.HasPrefix(clean, "/private/tmp") ||
		strings.HasPrefix(clean, "/var/folders/") ||
		strings.HasPrefix(clean, filepath.Clean(tmp))

	if !isTemp {
		return false
	}

	base := filepath.Base(clean)
	dirName := filepath.Dir(clean)
	return strings.HasPrefix(base, "extproto-") ||
		strings.HasPrefix(base, "extpipe-") ||
		strings.HasPrefix(base, "worker-privacy-") ||
		strings.HasPrefix(base, "worker-folder-") ||
		strings.HasPrefix(base, "test-chrome-") ||
		strings.HasPrefix(base, "browser-profile-") ||
		strings.HasPrefix(base, "library-profile-") ||
		strings.HasPrefix(filepath.Base(dirName), "extproto-") ||
		strings.HasPrefix(filepath.Base(dirName), "extpipe-") ||
		strings.HasPrefix(filepath.Base(dirName), "worker-privacy-") ||
		strings.HasPrefix(filepath.Base(dirName), "worker-folder-") ||
		strings.HasPrefix(filepath.Base(dirName), "test-chrome-")
}
