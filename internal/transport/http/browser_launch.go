package http

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/browserlaunch"
)

// The console's browser setup guide (web/console/src/session/BrowserSetup.tsx),
// which Cloud carries as chrome-setup and chrome-launch, so a phone can set up
// browser use without a terminal.
//
//   - GET  /v1/browser/setup    whether a Chromium was found and where, the profile, whether the extension is connected
//   - POST /v1/browser/launch   start Clawdline's own Chromium (internal/adapters/browserlaunch)

// findChromium is the Chromium the daemon would start; a test replaces it.
var findChromium = func() string { return browserlaunch.FindChromium("", os.Getenv) }

func (s *Server) browserSetupRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	if !mayReadBrowser(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This needs a device that may read.")
		return
	}
	chromium := findChromium()
	profile := ""
	running := false
	if s.cfg.Dir != "" {
		profile = browserlaunch.ProfileDir(s.cfg.Dir)
		running = browserlaunch.Running(s.cfg.Dir)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{
		"ok": true, "mode": s.browserMode(), "chromium_found": chromium != "", "chromium": chromium, "profile": profile,
		"running": running, "connected": s.browser().SetupConnected(),
		"enabled": s.browserSetting("browser_enabled"), "notify_new_site": s.browserSetting("browser_notify_new_site"),
		"install_command": browserlaunch.InstallCommand,
	})
}

func (s *Server) browserLaunchRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	// The body says nothing; an empty one and `{}` are both a press.
	raw, err := io.ReadAll(io.LimitReader(r.Body, settingsRequestBodyLimit+1))
	if body := strings.TrimSpace(string(raw)); err != nil || (body != "" && body != "{}") {
		writeRefusal(w, http.StatusBadRequest, "browser_bad_request", "The browser request is not valid JSON for this route.")
		return
	}
	if machineAuthed(r) || !maySend(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "Only a person's device that may send can open Clawdline's Chrome.")
		return
	}
	if s.browserMode() == "user_chrome" {
		writeRefusal(w, http.StatusConflict, "browser_user_chrome_setup_required", "Connect your usual Chrome extension separately before using this mode.")
		return
	}
	if s.cfg.Dir == "" {
		writeRefusal(w, http.StatusServiceUnavailable, "browser_launch_failed", "Clawdline's Chrome could not be started on this machine.")
		return
	}
	started, err := browserlaunch.Launch(browserlaunch.Options{Dir: s.cfg.Dir, Port: s.cfg.Port, Browser: findChromium()})
	if errors.Is(err, browserlaunch.ErrChromiumMissing) {
		writeRefusal(w, http.StatusConflict, "browser_chromium_missing", "No Chromium was found on this machine. Install one with: npx playwright install chromium")
		return
	}
	if err != nil {
		writeRawRefusal(w, http.StatusInternalServerError, "browser_launch_failed", "Clawdline's Chrome could not be started: "+err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"ok": true, "pid": started.PID, "browser": started.Browser, "profile": started.Profile,
		"extension_id": started.ExtensionID, "install_command": browserlaunch.InstallCommand})
}

// browserPrepareRoute writes only Clawdline-owned files after an explicit
// press by a sending device. It never registers a host or opens Chrome.
func (s *Server) browserPrepareRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	if machineAuthed(r) || !maySend(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "Only a person's device that may send can prepare the extension.")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, settingsRequestBodyLimit+1))
	if body := strings.TrimSpace(string(raw)); err != nil || (body != "" && body != "{}") {
		writeRefusal(w, http.StatusBadRequest, "browser_bad_request", "The browser request is not valid JSON for this route.")
		return
	}
	if s.browserMode() != "user_chrome" {
		writeRefusal(w, http.StatusConflict, "browser_wrong_mode", "Select your usual Chrome mode before preparing its extension.")
		return
	}
	receipt, err := browserlaunch.PrepareUserChrome(s.cfg.Dir)
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "browser_prepare_failed", "The extension files could not be prepared.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, receipt)
}
