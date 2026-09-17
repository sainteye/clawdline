package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sainteye/clawdline-go/internal/adapters/devices"
	"github.com/sainteye/clawdline-go/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/config"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/auth"
)

// The gate is in front of every route, and it is the Swift app's
// RemoteServer.dispatch preamble, rule for rule: the two refusals about who is
// allowed to be asking at all, then a token for everything that is not on the
// open list, then the Origin check for anything that changes something.
//
// There is no exception for loopback. Through a tunnel every request comes
// from 127.0.0.1, and a web page the person is visiting can reach a local port
// too — with a DNS rebinding, as same-origin.

const (
	// sessionCookie is this daemon's cookie. Not `clawdline`, which is the
	// Swift app's: a browser keeps one cookie per name per host whatever the
	// port, so sharing the name would have each app overwrite the other's and
	// sign the person out of whichever they used last.
	sessionCookie = "clawdline-next"
	// machineHeader carries the orchestrator credential, as in the Swift app.
	machineHeader = "X-Clawdline-Orchestrator"
)

// gate holds what the checks read. One per state directory per process,
// because the authority behind it is the only writer of that directory's
// device file.
type gate struct {
	files *devices.Files
	auth  *auth.Authority
	// err is why the gate could not open. Every route behind it is then
	// refused; an unreadable device file is never an empty one.
	err error
	// hostname is `remote_hostname` from this app's config.json, read when the
	// daemon starts: the one name besides loopback and a quick tunnel that the
	// Host and Origin checks accept.
	hostname string
	port     int

	machineWarned sync.Once
}

var (
	gates  sync.Map // dir -> *gate
	gateMu sync.Mutex
)

func (s *Server) gate() *gate {
	if g, ok := gates.Load(s.cfg.Dir); ok {
		return g.(*gate)
	}
	gateMu.Lock()
	defer gateMu.Unlock()
	if g, ok := gates.Load(s.cfg.Dir); ok {
		return g.(*gate)
	}
	g := openGate(s.cfg)
	gates.Store(s.cfg.Dir, g)
	return g
}

// AuthReady opens the gate and says whether it opened. The daemon asks before
// it listens, so a device file that cannot be read stops it at startup rather
// than answering every request with a refusal.
func (s *Server) AuthReady() error { return s.gate().err }

// swiftDirs is the Swift app's directory by every name it may go by. Nothing
// under it is opened, read or accepted here.
func swiftDirs() []string {
	out := []string{swiftstore.Dir()}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, ".config", "clawdline"))
	}
	return out
}

func openGate(cfg config.Config) *gate {
	g := &gate{port: cfg.Port}
	files, err := devices.Open(cfg.Dir, swiftDirs()...)
	if err != nil {
		g.err = fmt.Errorf("the device store at %s could not be opened: %w", cfg.Dir, err)
		log.Printf("auth: %v", g.err)
		return g
	}
	g.files = files
	a, err := auth.New(files, auth.Options{})
	if err != nil {
		g.err = fmt.Errorf("the device store at %s could not be read: %w", cfg.Dir, err)
		log.Printf("auth: %v", g.err)
		return g
	}
	// This machine's own token exists from the moment the daemon starts, so a
	// script or the shell finds a key already there rather than a 401.
	if _, err := a.LocalToken(); err != nil {
		g.err = fmt.Errorf("the local token could not be written: %w", err)
		log.Printf("auth: %v", g.err)
		return g
	}
	g.auth = a
	if _, err := files.MachineToken(); err != nil {
		g.machineWarned.Do(func() {
			log.Printf("auth: orchestrator token unusable, machine authentication is refused: %v", err)
		})
	}
	if v, err := nextconfig.Open(cfg.Dir).Read(); err == nil {
		if h, ok := v.String("remote_hostname"); ok {
			g.hostname = strings.ToLower(strings.TrimSpace(h))
		}
	}
	// Paths only. Neither token is ever written to the log.
	log.Printf("auth: local token at %s, orchestrator token at %s, audit at %s",
		files.LocalTokenPath(), files.MachineTokenPath(), files.AuditPath())
	return g
}

// access is what the gate learned about a request, for the handlers behind it.
type access struct {
	machine bool
	verdict auth.Verdict
}

type accessKey struct{}

func accessOf(r *http.Request) access {
	a, _ := r.Context().Value(accessKey{}).(access)
	return a
}

// maySend is the Swift writeGate's capability half: a device that may type
// into a session. The machine token is not a way round it, as it is not in the
// Swift app — it opens the orchestrator's routes, not a session's.
func maySend(r *http.Request) bool {
	v := accessOf(r).verdict
	return v.Allowed && v.Caps.Has(auth.Send)
}

// machineAuthed is whether the request carried this daemon's orchestrator
// credential on a route that accepts it.
func machineAuthed(r *http.Request) bool { return accessOf(r).machine }

func (g *gate) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Before anything else, and before authentication: these are about
		// who is allowed to be asking at all, not about who they are.
		if msg := g.crossOriginRefusal(r); msg != "" {
			writeAuthRefusal(w, http.StatusForbidden, "forbidden", msg)
			return
		}
		p := cleanPath(r.URL.Path)
		machine := machineScoped(p) && g.verifyMachine(r.Header.Get(machineHeader))
		verdict := g.permission(r)
		if !openPath(p) && !machine && !taskSecretRoute(r.Method, p) && !verdict.Allowed {
			if g.err != nil {
				writeAuthRefusal(w, http.StatusServiceUnavailable, "store_unavailable",
					"The device store could not be read, so nothing behind the gate is answered.")
				return
			}
			writeAuthRefusal(w, http.StatusUnauthorized, "unauthorized", "This needs a paired device.")
			return
		}
		// A cookie is sent whether or not the page asking wanted it sent, so a
		// change must also come from our own page. Reads are exempt: they are
		// already gated by the token.
		if r.Method != http.MethodGet && r.Header.Get("Origin") != "" && !g.isOurs(r.Header.Get("Origin")) {
			writeAuthRefusal(w, http.StatusForbidden, "forbidden", "That request did not come from this page.")
			return
		}
		if status, code, msg := writePolicy(r.Method, p, machine, verdict); status != 0 {
			writeAuthRefusal(w, status, code, msg)
			return
		}
		// The credential stops here. Nothing behind the gate needs it, and the
		// proxy behind it would otherwise hand this daemon's token to the Swift
		// app.
		stripCredentials(r)
		ctx := context.WithValue(r.Context(), accessKey{}, access{machine: machine, verdict: verdict})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// openPath is the list that needs no token, and each entry is on it for a
// reason rather than for convenience: a person cannot sign in through a page
// that will not load, cannot pair with a machine they cannot ask, and a
// browser fetches its icon before it knows who anybody is. None of these
// answers names a session, a repository, a path or a credential.
//
// `/assets/` and `/strings/` are this console's bundle, which Vite writes where
// the Swift page keeps `/app/`: the same files for everybody, and the door has
// to be drawn before anybody is let through it.
func openPath(p string) bool {
	switch p {
	case "/", "/index.html", "/manifest.webmanifest", "/hero-orchestration.webp",
		"/v1/health", "/v1/strings", "/sw.js", "/favicon.ico":
		return true
	}
	for _, prefix := range []string{"/v1/auth/", "/app/", "/assets/", "/strings/"} {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	for _, prefix := range []string{"/splash-", "/icon-", "/project-"} {
		if strings.HasPrefix(p, prefix) && strings.HasSuffix(p, ".png") {
			return true
		}
	}
	return false
}

// machineScoped is where the orchestrator credential is accepted in place of a
// device: the orchestrator's own routes, the board, and the worktree reads.
// The `/v1/next/` names are this daemon's shadows of the orchestrator's routes
// and are treated as the routes they shadow.
func machineScoped(p string) bool {
	if strings.HasPrefix(p, "/v1/orchestrator/") {
		return true
	}
	switch p {
	case "/v1/board", "/v1/next/board", "/v1/next/schedules", "/v1/next/coordinator":
		return true
	}
	if strings.HasPrefix(p, "/v1/projects/") {
		parts := strings.Split(strings.TrimPrefix(p, "/v1/projects/"), "/")
		return len(parts) >= 2 && parts[1] == "worktrees"
	}
	return false
}

// taskSecretRoute is where a child reports with its own task secret, which
// the handler checks. No such handler exists on this daemon yet — children
// write result.json — so today these paths reach settleRoute's not_found.
func taskSecretRoute(method, p string) bool {
	if !strings.HasPrefix(p, "/v1/orchestrator/tasks/") {
		return false
	}
	switch method {
	case http.MethodPost:
		return strings.HasSuffix(p, "/complete") || strings.HasSuffix(p, "/notify") ||
			strings.HasSuffix(p, "/landing") || strings.HasSuffix(p, "/progress")
	case http.MethodGet:
		return strings.HasSuffix(p, "/inflight")
	}
	return false
}

// writePolicy is the capability each changing route needs, for the routes
// whose handlers this gate does not own. Session actions check it themselves
// (actions.go), as do dispatch and settle (dispatch.go), each with the Swift
// app's sentence.
func writePolicy(method, p string, machine bool, v auth.Verdict) (int, string, string) {
	if method == http.MethodGet || method == http.MethodHead {
		return 0, "", ""
	}
	send := v.Allowed && v.Caps.Has(auth.Send)
	switch {
	case p == "/v1/orchestrator/schedules" || p == "/v1/next/schedules":
		// Two doors, as in the Swift app: a device that may send, or this
		// machine's orchestrator.
		if !machine && !send {
			return http.StatusForbidden, "forbidden", "This device may read, and not send."
		}
	case p == "/v1/board" || p == "/v1/next/board":
		if !machine && !send {
			return http.StatusForbidden, "forbidden", "This device may only read the board."
		}
	case p == "/v1/next/coordinator":
		if !machine {
			return http.StatusForbidden, "forbidden", "Moving the machine coordinator needs the orchestrator token."
		}
	case p == "/v1/settings":
		// The Swift app has no such route: its settings are a native window.
		// Here they are the hotkey, a global keyboard grab, so only this
		// machine's own token changes them.
		if !(v.Allowed && v.Local) {
			return http.StatusForbidden, "forbidden", "Only this Mac's own token may change its settings."
		}
	case p == "/v1/orchestrator/tasks" || strings.HasPrefix(p, "/v1/orchestrator/tasks/"):
		// dispatch.go, and the task-secret routes.
	case strings.HasPrefix(p, "/v1/orchestrator/"):
		if !machine {
			return http.StatusForbidden, "forbidden", "That needs the orchestrator token."
		}
	}
	return 0, "", ""
}

// crossOriginRefusal is the Host check and the Sec-Fetch-Site check, which
// come before authentication because they are about a browser being made to
// ask on somebody else's behalf. "" means neither applies.
func (g *gate) crossOriginRefusal(r *http.Request) string {
	if isCrossSiteSubresource(r.Header) {
		return "Cross-site requests are not answered."
	}
	if !isAllowedHost(r.Host, g.hostname) {
		return "Wrong host."
	}
	return ""
}

// isCrossSiteSubresource is a cross-site request that is not somebody
// following a link. Typing the address into a bar showing another page is
// cross-site too, and says navigate/document, which a script's fetch cannot.
// Absent headers mean it is not a browser, and the token is what it needs.
func isCrossSiteSubresource(h http.Header) bool {
	if h.Get("Sec-Fetch-Site") != "cross-site" {
		return false
	}
	return !(h.Get("Sec-Fetch-Mode") == "navigate" && h.Get("Sec-Fetch-Dest") == "document")
}

// isAllowedHost is the DNS-rebinding defence: rebinding changes what a name
// resolves to, never the Host header, so a Host this daemon does not answer
// to is refused before anything else is looked at. A quick tunnel's name is
// generated per run, so its whole suffix is allowed; rebinding needs the
// attacker to control that DNS answer, and those answers are Cloudflare's.
func isAllowedHost(header, hostname string) bool {
	host := strings.ToLower(strings.TrimSpace(header))
	if strings.HasPrefix(host, "[") {
		end := strings.Index(host, "]")
		if end < 0 {
			return false
		}
		host = host[1:end]
	} else if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	if hostname != "" && host == hostname {
		return true
	}
	return strings.HasSuffix(host, ".trycloudflare.com")
}

// isOurs is whether an Origin is a page this daemon served. As in the Swift
// app it compares the host and not the port.
func (g *gate) isOurs(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	if host == "127.0.0.1" || host == "localhost" {
		return true
	}
	if g.hostname != "" && host == g.hostname {
		return true
	}
	return strings.HasSuffix(host, ".trycloudflare.com")
}

// bearer is the token a request carries: the header, which a script uses, or
// the cookie, which exists because EventSource cannot set a header at all.
func bearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		return strings.TrimSpace(c.Value)
	}
	return ""
}

func (g *gate) permission(r *http.Request) auth.Verdict {
	if g.auth == nil {
		return auth.Verdict{}
	}
	return g.auth.Verify(bearer(r))
}

func (g *gate) verifyMachine(presented string) bool {
	if g.files == nil || presented == "" {
		return false
	}
	expected, err := g.files.MachineToken()
	if err != nil {
		g.machineWarned.Do(func() {
			log.Printf("auth: orchestrator token unusable, machine authentication is refused: %v", err)
		})
		return false
	}
	return auth.VerifySecret(expected, presented)
}

// stripCredentials removes this daemon's credentials from a request that has
// been let through: the Authorization header when it is a bearer, the
// orchestrator header, and this daemon's cookie. Other cookies are left, so
// the proxy still carries the Swift app's own.
func stripCredentials(r *http.Request) {
	if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		r.Header.Del("Authorization")
	}
	r.Header.Del(machineHeader)
	cookies := r.Cookies()
	if len(cookies) == 0 {
		return
	}
	var kept []string
	for _, c := range cookies {
		if c.Name != sessionCookie {
			kept = append(kept, c.Name+"="+c.Value)
		}
	}
	r.Header.Del("Cookie")
	if len(kept) > 0 {
		r.Header.Set("Cookie", strings.Join(kept, "; "))
	}
}

// cleanPath is the path the decision is made on: dot segments and repeated
// slashes resolved, a trailing slash kept. The mux redirects an unclean path
// anyway; deciding on the clean one means the open list cannot be walked out
// of with `/app/../`.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	c := path.Clean("/" + p)
	if strings.HasSuffix(p, "/") && c != "/" {
		c += "/"
	}
	return c
}

// writeAuthRefusal sends the Swift app's error envelope, which is what its
// page reads: `{"error":{"code","message","request_id"}}`.
func writeAuthRefusal(w http.ResponseWriter, status int, code, message string) {
	writeAuthError(w, status, contract.AuthError{Code: code, Message: message})
}

func writeAuthError(w http.ResponseWriter, status int, e contract.AuthError) {
	e.RequestID = requestID()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(contract.AuthRefusal{Error: e})
}
