package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
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
//   - GET  /v1/browser/screenshot     ?tab=N[&quality=Q]; the same view as a read, which Cloud carries
func (s *Server) browserRoutes() []route {
	return []route{
		{Route{"*", "/v1/browser/commands"}, s.browserCommandRoute},
		{Route{"*", "/v1/browser/host/poll"}, s.browserHostPollRoute},
		{Route{"*", "/v1/browser/host/results"}, s.browserHostResultsRoute},
		{Route{"*", "/v1/browser/status"}, s.browserStatusRoute},
		{Route{"*", "/v1/browser/audit"}, s.browserAuditRoute},
		{Route{"*", "/v1/browser/stop"}, s.browserStopRoute},
		{Route{"*", "/v1/browser/control"}, s.browserControlRoute},
		{Route{"*", "/v1/browser/person"}, s.browserPersonRoute},
		{Route{"*", "/v1/browser/screenshot"}, s.browserScreenshotRoute},
	}
}

func (s *Server) browser() *browserbridge.Bridge {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
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
	return s.browserBridge
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
		Op           string          `json:"op"`
		Args         json.RawMessage `json:"args"`
	}
	if !readBrowserBody(w, r, browserbridge.MaxArgBytes+4096, &in) {
		return
	}
	if !s.browserSetting("browser_enabled") {
		writeRefusal(w, http.StatusForbidden, "browser_disabled", "Browser use is turned off on this machine. The person can turn it on in Settings, Browser.")
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
	value, err := s.browser().Do(r.Context(), in.Conversation, in.Op, in.Args)
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
		case browserbridge.HostMissing:
			writeRefusal(w, http.StatusServiceUnavailable, string(refusal), "The Clawdline Chrome extension is not connected. Ask the person to install it or open Chrome; continue another way meanwhile.")
		case browserbridge.Stopped:
			writeRefusal(w, http.StatusConflict, string(refusal), "The person stopped all browser use. Do not retry until they resume it.")
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
	commands := s.browser().Poll(r.Context(), info)
	if !s.browserSetting("browser_enabled") && len(commands) > 0 {
		// The setting was turned off while commands waited; none of them runs.
		s.browser().Deliver(refuseAll(commands))
		commands = []browserbridge.Command{}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"commands": commands})
}

func refuseAll(commands []browserbridge.Command) []browserbridge.Result {
	out := make([]browserbridge.Result, 0, len(commands))
	for _, c := range commands {
		out = append(out, browserbridge.Result{ID: c.ID, Error: string(browserbridge.Stopped)})
	}
	return out
}

func (s *Server) browserHostResultsRoute(w http.ResponseWriter, r *http.Request) {
	if !machineAuthed(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "Only the browser host on this machine may answer.")
		return
	}
	var in struct {
		Results []browserbridge.Result `json:"results"`
	}
	if !readBrowserBody(w, r, browserbridge.MaxResultBytes, &in) {
		return
	}
	s.browser().Deliver(in.Results)
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
	st := s.browser().Status()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"ok": true, "enabled": s.browserSetting("browser_enabled"),
		"notify_new_site": s.browserSetting("browser_notify_new_site"), "status": st})
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
	rows, pruned := s.browser().Records(r.URL.Query().Get("conversation"), limit)
	w.Header().Set("Cache-Control", "no-store")
	saved := ""
	if err := s.browser().JournalErr(); err != nil {
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
	s.browser().SetStopped(in.Stopped)
	writeJSON(w, map[string]any{"ok": true, "stopped": in.Stopped})
}

func (s *Server) browserControlRoute(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Tab    int  `json:"tab"`
		Person bool `json:"person"`
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
	s.browser().SetPersonControl(in.Tab, in.Person)
	writeJSON(w, map[string]any{"ok": true, "tab": in.Tab, "person": in.Person})
}

// browserPersonRoute is the live panel's: a screenshot needs a device that may
// read; a click, typing, a key or a scroll needs one that may send, and a tab
// the person has taken over.
func (s *Server) browserPersonRoute(w http.ResponseWriter, r *http.Request) {
	var in struct {
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
	value, err := s.browser().PersonDo(r.Context(), in.Op, in.Tab, in.Args)
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
	value, err := s.browser().PersonDo(r.Context(), "screenshot", tab, args)
	if err != nil {
		writeBrowserError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"ok": true, "value": value})
}
