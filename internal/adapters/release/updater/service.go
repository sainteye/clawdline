package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/install"
)

// serviceCommandTimeout bounds one systemctl, systemd-run or launchctl.
const serviceCommandTimeout = 30 * time.Second

// unitName is the systemd unit without its suffix.
func unitName(s install.ServiceFile) string { return strings.TrimSuffix(s.Name, ".service") }

// SupervisorLabel is the supervisor's launchd label or systemd unit.
func SupervisorLabel(s install.ServiceFile) string {
	if s.Supervisor == "systemd" {
		return unitName(s) + "-update"
	}
	return s.Name + ".update"
}

// supervisorArgs is the old release's `update finish`, which supervises.
func (e Env) supervisorArgs(from string) []string {
	return []string{filepath.Join(e.Layout.ReleaseDir(from), "clawdline"), "update", "finish",
		"--state", e.StateDir, "--root", e.Layout.Root}
}

// startSupervisor starts `update finish` from the old release, outside the
// service's own job, so that restarting the service does not stop it and a
// supervisor that dies is started again.
func (e Env) startSupervisor(ctx context.Context, s install.ServiceFile, from string) error {
	ctx, cancel := context.WithTimeout(ctx, serviceCommandTimeout)
	defer cancel()
	args := e.supervisorArgs(from)
	label := SupervisorLabel(s)
	switch s.Supervisor {
	case "systemd":
		// A unit of the same name left failed from an earlier update would
		// refuse the new one.
		_, _ = e.Run(ctx, "systemctl", "--user", "reset-failed", label+".service")
		run := []string{"--user", "--unit", label, "--collect", "-p", "Restart=on-failure", "-p", "RestartSec=5"}
		for _, k := range sortedKeys(e.SupervisorEnv) {
			run = append(run, "--setenv="+k+"="+e.SupervisorEnv[k])
		}
		if _, err := e.Run(ctx, "systemd-run", append(run, args...)...); err != nil {
			return fail(CodeSupervisorFailed, "%v", err)
		}
		return nil
	case "launchd":
		plist := filepath.Join(e.UpdateDir(), label+".plist")
		if err := os.WriteFile(plist, []byte(e.supervisorPlist(label, args)), 0o600); err != nil {
			return fail(CodeSupervisorFailed, "%v", err)
		}
		// One left loaded from an earlier update is replaced.
		_, _ = e.Run(ctx, "launchctl", "bootout", s.Domain+"/"+label)
		if _, err := e.Run(ctx, "launchctl", "bootstrap", s.Domain, plist); err != nil {
			return fail(CodeSupervisorFailed, "%v", err)
		}
		return nil
	}
	return fail(CodeNoService, "service.json names supervisor %q", s.Supervisor)
}

// supervisorPlist is a one-shot LaunchAgent: started at load, started again
// whenever it exits unsuccessfully, left alone once it exits 0.
func (e Env) supervisorPlist(label string, args []string) string {
	var b strings.Builder
	esc := html.EscapeString
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + esc(label) + `</string>
  <key>ProgramArguments</key>
  <array>
`)
	for _, a := range args {
		b.WriteString("    <string>" + esc(a) + "</string>\n")
	}
	b.WriteString(`  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key>
  <dict><key>SuccessfulExit</key><false/></dict>
  <key>ThrottleInterval</key><integer>5</integer>
  <key>StandardOutPath</key><string>` + esc(filepath.Join(e.UpdateDir(), "supervisor.log")) + `</string>
  <key>StandardErrorPath</key><string>` + esc(filepath.Join(e.UpdateDir(), "supervisor.log")) + `</string>
`)
	if len(e.SupervisorEnv) > 0 {
		b.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
		for _, k := range sortedKeys(e.SupervisorEnv) {
			b.WriteString("    <key>" + esc(k) + "</key><string>" + esc(e.SupervisorEnv[k]) + "</string>\n")
		}
		b.WriteString("  </dict>\n")
	}
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

// endSupervisor unloads the supervisor job once the update is settled.
// Under launchd this stops the calling process, so it is the last thing
// `update finish` does.
func (e Env) endSupervisor(ctx context.Context, s install.ServiceFile) {
	if s.Supervisor != "launchd" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, serviceCommandTimeout)
	defer cancel()
	label := SupervisorLabel(s)
	os.Remove(filepath.Join(e.UpdateDir(), label+".plist"))
	_, _ = e.Run(ctx, "launchctl", "bootout", s.Domain+"/"+label)
}

// restartService restarts the daemon's service, whichever binary `current`
// now names.
func (e Env) restartService(ctx context.Context, s install.ServiceFile) error {
	ctx, cancel := context.WithTimeout(ctx, serviceCommandTimeout)
	defer cancel()
	var err error
	switch s.Supervisor {
	case "systemd":
		_, err = e.Run(ctx, "systemctl", "--user", "restart", s.Name)
	case "launchd":
		_, err = e.Run(ctx, "launchctl", "kickstart", "-k", s.Domain+"/"+s.Name)
	default:
		err = fmt.Errorf("unknown supervisor %q", s.Supervisor)
	}
	return err
}

// waitHealthy asks the daemon on s.Port until it serves its console and
// names commit, for at most HealthWaitSecondsLimit.
func (e Env) waitHealthy(ctx context.Context, s install.ServiceFile, want Served) error {
	token, _ := os.ReadFile(filepath.Join(e.StateDir, "orchestrator-token"))
	check := e.Health
	if check == nil {
		check = e.httpHealth
	}
	wait := HealthWaitSecondsLimit * time.Second
	if e.HealthWait > 0 && e.HealthWait < wait {
		wait = e.HealthWait
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	poll := e.Poll
	if poll <= 0 {
		poll = time.Second
	}
	last := errors.New("no answer yet")
	for {
		if err := check(ctx, s.Port, strings.TrimSpace(string(token)), want); err == nil {
			return nil
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return fail(CodeHealthTimeout, "the daemon on port %d did not come up as %s within %s: %v",
				s.Port, want, wait, last)
		case <-time.After(poll):
		}
	}
}

// Served is the release a restarted daemon must be serving.
type Served struct {
	Commit  string
	Version string
}

func (s Served) String() string {
	if s.Version != "" {
		return s.Version + " (" + short(s.Commit) + ")"
	}
	return short(s.Commit)
}

// httpHealth is GET / answering 200 and the token-authenticated
// /BUILD.json naming the release. /v1/health alone is not enough: it answers
// before the console is served. The version is compared too: two releases
// built from one commit (a release candidate and its final) differ only there.
func (e Env) httpHealth(ctx context.Context, port int, token string, want Served) error {
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	c := &http.Client{Timeout: 5 * time.Second}
	ask := func(path string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return nil, err
		}
		if token != "" {
			req.Header.Set("X-Clawdline-Orchestrator", token)
		}
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s answered %d", path, resp.StatusCode)
		}
		return body, nil
	}
	if _, err := ask("/"); err != nil {
		return err
	}
	body, err := ask("/BUILD.json")
	if err != nil {
		return err
	}
	var b struct {
		Stamp   string `json:"stamp"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return fmt.Errorf("BUILD.json: %v", err)
	}
	// A release whose BUILD.json could not be read is known only by its
	// console answering.
	if want.Commit != "" && !strings.EqualFold(strings.TrimSpace(b.Stamp), want.Commit) {
		return fmt.Errorf("the daemon serves %s, not %s", short(b.Stamp), want)
	}
	// A release built before BUILD.json carried its version is known by its
	// commit alone.
	if want.Version != "" && b.Version != "" && b.Version != want.Version {
		return fmt.Errorf("the daemon serves %s, not %s", b.Version, want)
	}
	return nil
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
