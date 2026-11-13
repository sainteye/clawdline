package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/app/browserbridge"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/productcopy"
)

// The browser bridge (internal/app/browserbridge, docs/browser.md).
//
//   - POST /v1/browser/commands       an agent's command; machine token, live Session
//   - POST /v1/browser/host/poll      the native messaging host's long poll; machine token
//   - POST /v1/browser/host/results   the host's answers; machine token
//   - GET  /v1/browser/status         machine token, or a person who may read
//   - GET  /v1/browser/audit          machine token, or a person who may read
//   - POST /v1/browser/stop           {stopped}; the machine may only stop, a person may also resume
//   - POST /v1/browser/control        {tab, person}; a person who may send
//   - POST /v1/browser/person         {op, tab, args}; the person's own view and input
//   - POST /v1/browser/mute           {conversation, muted}; one Session's new-site notification, a person who may send
//   - GET  /v1/browser/activity       ?conversation=; a Session's recent actions with a picture of each, a person who may read
//   - GET  /v1/browser/screenshot     ?tab=N[&quality=Q]; the same view as a read, which Cloud carries
//   - GET/POST /v1/browser/settings   ordered automatic selection and new-site notifications; writing needs a person who may send
//   - GET  /v1/browser/setup          the setup guide's read (browser_launch.go)
//   - POST /v1/browser/launch         start Clawdline's own Chromium; a person who may send
func (s *Server) browserRoutes() []route {
	return []route{
		{Route{"*", "/v1/browser/reminder"}, s.browserReminderRoute},
		{Route{"*", "/v1/browser/commands"}, s.browserCommandRoute},
		{Route{"*", "/v1/browser/host/poll"}, s.browserHostPollRoute},
		{Route{"*", "/v1/browser/host/results"}, s.browserHostResultsRoute},
		{Route{"*", "/v1/browser/status"}, s.browserStatusRoute},
		{Route{"*", "/v1/browser/audit"}, s.browserAuditRoute},
		{Route{"*", "/v1/browser/stop"}, s.browserStopRoute},
		{Route{"*", "/v1/browser/control"}, s.browserControlRoute},
		{Route{"*", "/v1/browser/person"}, s.browserPersonRoute},
		{Route{"*", "/v1/browser/screenshot"}, s.browserScreenshotRoute},
		{Route{"*", "/v1/browser/mute"}, s.browserMuteRoute},
		{Route{"*", "/v1/browser/activity"}, s.browserActivityRoute},
		{Route{"*", "/v1/browser/settings"}, s.browserSettingsRoute},
		{Route{"*", "/v1/browser/setup"}, s.browserSetupRoute},
		{Route{"*", "/v1/browser/prepare"}, s.browserPrepareRoute},
		{Route{"*", "/v1/browser/launch"}, s.browserLaunchRoute},
	}
}

func (s *Server) browser() *browserbridge.Bridge {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	if s.browserProfiles != nil {
		return s.browserProfiles.Legacy()
	}
	if s.browserBridge == nil {
		if s.cfg.Dir == "" {
			s.browserBridge = browserbridge.New(s.announceBrowserSite)
		} else {
			b, err := browserbridge.Open(s.cfg.Dir, s.announceBrowserSite)
			if err != nil {
				log.Printf("browser: the audit journal could not be read: %v", err)
			}
			s.browserBridge = b
		}
	}
	if err := s.browserBridge.SetMode(s.browserMode()); err != nil {
		s.browserBridge.SetEnabled(false)
	}
	return s.browserBridge
}

// setBrowserAwareSettings serializes permission changes with bridge refreshes.
// The config remains the only persisted source of the enabled setting.
func (s *Server) setBrowserAwareSettings(f *nextconfig.File, changes map[string]any) (nextconfig.Values, error) {
	if _, ok := changes["browser_policy"]; ok {
		return s.setBrowserPolicySettings(f, changes)
	}
	if _, ok := changes["browser_mode"]; ok {
		return s.setBrowserPolicySettings(f, changes)
	}
	if _, ok := changes["browser_enabled"]; ok {
		return s.setBrowserPolicySettings(f, changes)
	}
	return f.Set(changes)
}

func (s *Server) browserMode() string {
	values, err := nextconfig.Open(s.cfg.Dir).Read()
	if err != nil {
		return nextconfig.BrowserModeNative
	}
	return values.BrowserMode()
}

func (s *Server) browserSetting(key string) bool {
	values, err := nextconfig.Open(s.cfg.Dir).Read()
	if err != nil {
		return false
	}
	on, _ := values.Bool(key)
	return on
}

// announceBrowserSite sends the person's chosen notification: once for each
// site a Session starts operating. It never blocks the command that caused it.
func (s *Server) announceBrowserSite(n browserbridge.NewSite) {
	if s.broker == nil || !s.browserSetting("browser_notify_new_site") {
		return
	}
	go func() {
		ctx := context.Background()
		label, sessionID := "Session", ""
		if live, err := s.broker.LiveSession(ctx, n.Conversation); err == nil {
			sessionID = live.ID
			if live.Label != "" {
				label = live.Label
			}
		}
		language := s.productLanguage()
		title := productcopy.Format(language, "browser.new_site.title", map[string]string{"label": label})
		body := productcopy.Format(language, "browser.new_site.body", map[string]string{"site": n.Site})
		_, _ = s.broker.MachineNotify(ctx, title, body, sessionID)
	}()
}

func readBrowserBody(w http.ResponseWriter, r *http.Request, limit int64, into any) bool {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil || int64(len(raw)) > limit {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "browser_bad_request", "The browser request is too large or unreadable.")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(into) != nil {
		writeRefusal(w, http.StatusBadRequest, "browser_bad_request", "The browser request is not valid JSON for this route.")
		return false
	}
	return true
}

func (s *Server) browserCommandRoute(w http.ResponseWriter, r *http.Request) {
	if !machineAuthed(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "Only an agent on this machine may send browser commands.")
		return
	}
	var in struct {
		Conversation string          `json:"conversation"`
		Mode         string          `json:"browser_mode,omitempty"`
		Explicit     bool            `json:"explicit,omitempty"`
		Op           string          `json:"op"`
		Args         json.RawMessage `json:"args"`
	}
	if !readBrowserBody(w, r, browserbridge.MaxArgBytes+4096, &in) {
		return
	}
	if s.broker == nil {
		writeRefusal(w, http.StatusServiceUnavailable, "session_unresolved", "The live Session registry is unavailable.")
		return
	}
	if _, err := s.broker.LiveSession(r.Context(), in.Conversation); err != nil {
		writeRefusal(w, http.StatusForbidden, "session_unresolved", "Browser commands must come from a live Session on this machine.")
		return
	}
	policy, err := s.browserPolicy()
	if err != nil {
		writeBrowserError(w, browserbridge.StateWriteFailed)
		return
	}
	value, err := s.browsers().DoSession(r.Context(), in.Conversation, in.Op, in.Args, policy, in.Mode, in.Explicit)
	if err != nil {
		writeBrowserError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"ok": true, "value": value})
}

func writeBrowserError(w http.ResponseWriter, err error) {
	if refusal, ok := browserbridge.IsRefusal(err); ok {
		switch refusal {
		case browserbridge.SetupRequired:
			writeRefusal(w, http.StatusConflict, string(refusal), "This browser's extension setup is not confirmed. Open its setup instructions in Settings, Browser.")
		case browserbridge.NativeTools:
			writeRefusal(w, http.StatusConflict, string(refusal), "Use this agent's native browser tools. If they are unavailable, consult the browser priority and automatic-selection switches before choosing another browser.")
		case browserbridge.SessionChoicesFull:
			writeRefusal(w, http.StatusTooManyRequests, string(refusal), "The saved Session browser-choice limit has been reached. Existing Sessions and tabs are retained; no new choice was saved.")
		case browserbridge.SessionBusy:
			writeRefusal(w, http.StatusConflict, string(refusal), "This Session still has a browser operation in progress. Wait for its result before choosing another browser.")
		case browserbridge.WrongMode:
			writeRefusal(w, http.StatusConflict, string(refusal), "This Chrome profile is not the selected browser mode.")
		case browserbridge.Closing:
			writeRefusal(w, http.StatusConflict, string(refusal), "This tab is closing. Wait for its result before taking over or sending another command.")
		case browserbridge.Disabled:
			writeRefusal(w, http.StatusForbidden, string(refusal), "Automatic selection is off for this browser. Use Settings, Browser, or honor the person's explicit request for this Session after setup is complete.")
		case browserbridge.StateWriteFailed:
			writeRefusal(w, http.StatusServiceUnavailable, string(refusal), "The browser control state could not be saved. Keep Clawdline running and retry before restarting. Browser use remains paused.")
		case browserbridge.SecretInputFailed:
			writeRefusal(w, http.StatusUnprocessableEntity, string(refusal), "Secret input could not be confirmed. Check the field before trying again.")
		case browserbridge.HostMissing:
			writeRefusal(w, http.StatusServiceUnavailable, string(refusal), "The Clawdline Chrome extension is not connected. Ask the person to install it or open Chrome; continue another way meanwhile.")
		case browserbridge.Stopped:
			writeRefusal(w, http.StatusConflict, string(refusal), "The person stopped the Clawdline browser channel. Do not retry until they resume it.")
		case browserbridge.PersonControlling:
			writeRefusal(w, http.StatusConflict, string(refusal), "The person has taken over this tab. Wait until they hand it back.")
		case browserbridge.OtherSessionsTab:
			writeRefusal(w, http.StatusConflict, string(refusal), "That tab belongs to another Session. Open your own tab.")
		case browserbridge.QueueFull:
			writeRefusal(w, http.StatusTooManyRequests, string(refusal), "Too many browser commands are waiting. Try again shortly.")
		case browserbridge.TimedOut:
			writeRefusal(w, http.StatusGatewayTimeout, string(refusal), "The extension did not answer in time. The command may or may not have run; read the page before repeating it.")
		case browserbridge.NotTakenOver:
			writeRefusal(w, http.StatusConflict, string(refusal), "Take over the tab before typing or clicking in it.")
		case browserbridge.OtherDebugger:
			writeRefusal(w, http.StatusConflict, string(refusal), "Another program is already controlling this tab, so Clawdline cannot show or operate it. Close the other tool's control of the tab, or open a new tab.")
		case browserbridge.NoTab:
			writeRefusal(w, http.StatusConflict, string(refusal), "This Session has no tab yet. Open one first.")
		default:
			writeRefusal(w, http.StatusBadRequest, string(refusal), "The browser command or its arguments are not valid.")
		}
		return
	}
	var ext *browserbridge.ExtensionError
	if errors.As(err, &ext) {
		writeRawRefusal(w, http.StatusUnprocessableEntity, "browser_page_error", ext.Message)
		return
	}
	writeRawRefusal(w, http.StatusInternalServerError, "internal", err.Error())
}

func (s *Server) browserHostPollRoute(w http.ResponseWriter, r *http.Request) {
	if !machineAuthed(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "Only the browser host on this machine may poll.")
		return
	}
	var info browserbridge.HostInfo
	if !readBrowserBody(w, r, 4096, &info) {
		return
	}
	profiles := s.browsers()
	if profiles.StateError() {
		writeBrowserError(w, browserbridge.StateWriteFailed)
		return
	}
	bridge, err := profiles.Channel(info.Mode)
	if err != nil || !bridge.AcceptsHost(info) {
		writeBrowserError(w, browserbridge.WrongMode)
		return
	}
	commands := bridge.Poll(r.Context(), info)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"commands": commands})
}

func refuseAll(commands []browserbridge.Command) []browserbridge.Result {
	out := make([]browserbridge.Result, 0, len(commands))
	for _, c := range commands {
		out = append(out, browserbridge.Result{ID: c.ID, Error: string(browserbridge.Disabled)})
	}
	return out
}

func (s *Server) browserHostResultsRoute(w http.ResponseWriter, r *http.Request) {
	if !machineAuthed(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "Only the browser host on this machine may answer.")
		return
	}
	var in struct {
		Mode    string                 `json:"browser_mode,omitempty"`
		Results []browserbridge.Result `json:"results"`
	}
	if !readBrowserBody(w, r, browserbridge.MaxResultBytes, &in) {
		return
	}
	if err := s.browsers().Deliver(in.Mode, in.Results); err != nil {
		writeBrowserError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func mayReadBrowser(r *http.Request) bool {
	if machineAuthed(r) {
		return true
	}
	v := accessOf(r).verdict
	return v.Allowed && v.Caps.Has(auth.Read)
}

func (s *Server) browserStatusRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	if !mayReadBrowser(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This needs a device that may read.")
		return
	}
	for key, values := range r.URL.Query() {
		if key != "conversation" || len(values) != 1 || values[0] == "" {
			writeBrowserError(w, browserbridge.BadRequest)
			return
		}
	}
	conversation := r.URL.Query().Get("conversation")
	if len(conversation) > 128 {
		writeBrowserError(w, browserbridge.BadRequest)
		return
	}
	policy, err := s.browserPolicy()
	if err != nil {
		writeBrowserError(w, browserbridge.StateWriteFailed)
		return
	}
	mode, st, profiles := s.browsers().SelectedStatus(conversation, policy)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"ok": true, "mode": mode, "enabled": mode != "native" && profiles[mode].Ready, "browser_policy": policy, "profiles": profiles, "notify_new_site": s.browserSetting("browser_notify_new_site"), "status": st})
}

func (s *Server) browserAuditRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	if !mayReadBrowser(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This needs a device that may read.")
		return
	}
	limit := 200
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= browserbridge.MaxAuditRows {
		limit = v
	}
	q := r.URL.Query()
	filter := browserbridge.AuditFilter{Conversation: q.Get("conversation"), Site: q.Get("site")}
	if len(filter.Conversation) > 128 || len(filter.Site) > 253 {
		writeRefusal(w, http.StatusBadRequest, "browser_bad_request", "The Session or site to filter by is too long.")
		return
	}
	if raw := q.Get("since"); raw != "" {
		since, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || since < 0 {
			writeRefusal(w, http.StatusBadRequest, "browser_bad_request", "since is a Unix time in seconds.")
			return
		}
		filter.Since = since
	}
	rows, pruned := s.browsers().Find(filter, limit)
	w.Header().Set("Cache-Control", "no-store")
	saved := ""
	if err := s.browsers().JournalErr(); err != nil {
		saved = "The audit could not be written to disk; recent rows are kept only until the daemon restarts."
	}
	writeJSON(w, map[string]any{"ok": true, "rows": rows, "pruned": pruned, "journal_error": saved})
}

func (s *Server) browserStopRoute(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Stopped bool `json:"stopped"`
	}
	if !readBrowserBody(w, r, 256, &in) {
		return
	}
	if machineAuthed(r) {
		if !in.Stopped {
			writeRefusal(w, http.StatusForbidden, "browser_resume_person_only", "Only the person may resume browser use.")
			return
		}
	} else if !maySend(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This needs a device that may send.")
		return
	}
	if err := s.browsers().SetStopped(in.Stopped); err != nil {
		writeBrowserError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "stopped": in.Stopped})
}

func (s *Server) browserControlRoute(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode   string `json:"browser_mode"`
		Tab    int    `json:"tab"`
		Person bool   `json:"person"`
	}
	if !readBrowserBody(w, r, 256, &in) {
		return
	}
	if machineAuthed(r) || !maySend(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "Only a person's device that may send can take over a tab.")
		return
	}
	if in.Tab <= 0 {
		writeRefusal(w, http.StatusBadRequest, "browser_bad_request", "Name the tab to take over or hand back.")
		return
	}
	if err := s.browsers().SetPersonControl(in.Mode, in.Tab, in.Person); err != nil {
		writeBrowserError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "tab": in.Tab, "person": in.Person})
}

// browserPersonRoute is the live panel's: a screenshot needs a device that may
// read; a click, typing, a key or a scroll needs one that may send, and a tab
// the person has taken over.
func (s *Server) browserPersonRoute(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode string          `json:"browser_mode"`
		Op   string          `json:"op"`
		Tab  int             `json:"tab"`
		Args json.RawMessage `json:"args"`
	}
	if !readBrowserBody(w, r, browserbridge.MaxArgBytes+4096, &in) {
		return
	}
	if machineAuthed(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This is the person's own view of a tab; agents use clawdline browser.")
		return
	}
	if in.Op == "screenshot" {
		if !mayReadBrowser(r) {
			writeRefusal(w, http.StatusForbidden, "forbidden", "This needs a device that may read.")
			return
		}
	} else if !maySend(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This needs a device that may send.")
		return
	}
	value, err := s.browsers().PersonDo(r.Context(), in.Mode, in.Op, in.Tab, in.Args)
	if err != nil {
		writeBrowserError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"ok": true, "value": value})
}

// browserScreenshotRoute is the person's view as a GET, so Clawdline Cloud can
// carry it as a read word (chrome-tab-screenshot) for a device that may read.
func (s *Server) browserScreenshotRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	if machineAuthed(r) || !mayReadBrowser(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This needs a person's device that may read.")
		return
	}
	tab, err := strconv.Atoi(r.URL.Query().Get("tab"))
	if err != nil || tab <= 0 {
		writeRefusal(w, http.StatusBadRequest, "browser_bad_request", "Name the tab to look at.")
		return
	}
	args := json.RawMessage(`{}`)
	if q, err := strconv.Atoi(r.URL.Query().Get("quality")); err == nil && q >= 20 && q <= 80 {
		args = json.RawMessage(`{"quality":` + strconv.Itoa(q) + `}`)
	}
	value, err := s.browsers().PersonDo(r.Context(), r.URL.Query().Get("browser_mode"), "screenshot", tab, args)
	if err != nil {
		writeBrowserError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"ok": true, "value": value})
}

func (s *Server) browserMuteRoute(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Conversation string `json:"conversation"`
		Muted        bool   `json:"muted"`
	}
	if !readBrowserBody(w, r, 512, &in) {
		return
	}
	if machineAuthed(r) || !maySend(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "Only a person's device that may send can change a Session's notifications.")
		return
	}
	if in.Conversation == "" || len(in.Conversation) > 128 {
		writeRefusal(w, http.StatusBadRequest, "browser_bad_request", "Name the Session's conversation.")
		return
	}
	if err := s.browsers().SetMuted(in.Conversation, in.Muted); err != nil {
		writeBrowserError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "conversation": in.Conversation, "muted": in.Muted})
}

// browserActivityRoute is the panel's list of what the Session did, with the
// small picture of the page after each action. The pictures are kept in
// memory only (browserbridge.MaxActivityThumbs).
func (s *Server) browserActivityRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	if machineAuthed(r) || !mayReadBrowser(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This needs a person's device that may read.")
		return
	}
	conversation := r.URL.Query().Get("conversation")
	if conversation == "" || len(conversation) > 128 {
		writeRefusal(w, http.StatusBadRequest, "browser_bad_request", "Name the Session's conversation.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"ok": true, "rows": s.browsers().RecentActivity(conversation, browserbridge.MaxActivityThumbs)})
}

// browserSettingsRoute carries only browser priority, automatic switches and
// notification preference to a sending person device, including through Cloud.
// Agent commands use the separate Session choice route and cannot write policy.
func (s *Server) browserSettingsRoute(w http.ResponseWriter, r *http.Request) {
	if machineAuthed(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "Only the person may read or change the browser settings.")
		return
	}
	f := s.settingsFile()
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if !mayReadBrowser(r) {
			writeRefusal(w, http.StatusForbidden, "forbidden", "This needs a device that may read.")
			return
		}
	case http.MethodPost:
		if !maySend(r) {
			writeRefusal(w, http.StatusForbidden, "forbidden", "This device may read, and not change the browser settings.")
			return
		}
		if contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || contentType != "application/json" {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "A browser settings write is application/json.")
			return
		}
		var body map[string]json.RawMessage
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
		if decoder.Decode(&body) != nil || body == nil || decoder.Decode(new(any)) != io.EOF {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "That body is not a browser settings change.")
			return
		}
		changes := make(map[string]any, len(body))
		for name, raw := range body {
			if name != "browser_policy" && name != "browser_mode" && name != "browser_enabled" && name != "browser_notify_new_site" {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "Not a browser setting key: "+name)
				return
			}
			if name == "browser_policy" {
				policy, err := nextconfig.DecodeBrowserPolicy(raw)
				if err != nil {
					writeRefusal(w, http.StatusBadRequest, "browser_bad_request", "Choose each browser once in priority order with an automatic selection switch.")
					return
				}
				changes[name] = policy
				continue
			}
			if name == "browser_mode" {
				var mode string
				if json.Unmarshal(raw, &mode) != nil || (mode != nextconfig.BrowserModeNative && mode != nextconfig.BrowserModeUserChrome && mode != nextconfig.BrowserModeIsolated) {
					writeRefusal(w, http.StatusBadRequest, "browser_bad_request", "Choose native, user_chrome or isolated browser mode.")
					return
				}
				changes[name] = mode
				continue
			}
			var on bool
			if string(raw) != "true" && string(raw) != "false" || json.Unmarshal(raw, &on) != nil {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "A browser setting must be a boolean.")
				return
			}
			changes[name] = on
		}
		if len(changes) != 0 {
			if _, err := s.setBrowserAwareSettings(f, changes); err != nil {
				if _, ok := browserbridge.IsRefusal(err); ok {
					writeBrowserError(w, err)
				} else {
					writeSettingsFailure(w, f, err)
				}
				return
			}
		}
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST")
		return
	}
	values, err := f.Read()
	if err != nil {
		writeSettingsFailure(w, f, err)
		return
	}
	enabled, _ := values.Bool("browser_enabled")
	notify, _ := values.Bool("browser_notify_new_site")
	w.Header().Set("Cache-Control", "no-store")
	policy, err := values.BrowserPolicy()
	if err != nil {
		writeSettingsFailure(w, f, err)
		return
	}
	writeJSON(w, map[string]any{"browser_policy": policy, "browser_mode": values.BrowserMode(), "browser_enabled": enabled, "browser_notify_new_site": notify})
}
