package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/auth"
)

// The /v1/auth/ routes. The gate lets every one of them through without a
// token, as the Swift app does — nobody can pair with a machine they cannot
// ask — so each route below decides for itself who may call it. The ones
// that manage devices or read a pairing code take this machine's own token and
// nothing else.

const (
	cookieLifetime = "31536000"
	browserName    = "Browser on this Mac"
	// streamPing keeps an idle pairing stream from being closed by whatever
	// sits between it and its reader.
	streamPing = 15 * time.Second
)

func (s *Server) authRoute(w http.ResponseWriter, r *http.Request) {
	g := s.gate()
	if g.auth == nil {
		writeAuthRefusal(w, http.StatusServiceUnavailable, "store_unavailable",
			"The device store could not be read, so nobody can be let in.")
		return
	}
	// The string the mux dispatched by, so that this route's own names are
	// read the way the gate read them (routePath, gate.go).
	p := routePath(r)
	post := r.Method == http.MethodPost
	switch {
	case post && p == "/v1/auth/pair":
		g.beginPairing(w, r)
	case post && p == "/v1/auth/pair/confirm":
		g.confirmPairing(w, r)
	case post && p == "/v1/auth/adopt":
		g.adopt(w, r)
	case post && p == "/v1/auth/password":
		g.exchangePassword(w, r)
	case post && p == "/v1/auth/logout":
		g.logout(w, r)
	case r.Method == http.MethodGet && p == "/v1/auth/open":
		openPage(w)
	case r.Method == http.MethodGet && p == "/v1/auth/pairings":
		g.pairings(w, r)
	case p == "/v1/auth/devices" || strings.HasPrefix(p, "/v1/auth/devices/"):
		g.devicesRoute(w, r, strings.TrimPrefix(strings.TrimPrefix(p, "/v1/auth/devices"), "/"))
	default:
		writeAuthRefusal(w, http.StatusNotFound, "not_found", "No such route")
	}
}

// readBody is the Swift app's reading of a body: a JSON object or nothing.
// A body that is not one is an empty object, and the route then says which
// field it is missing.
func readBody(r *http.Request) map[string]any {
	data, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if json.Unmarshal(data, &out) != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func stringField(body map[string]any, key string) (string, bool) {
	v, ok := body[key].(string)
	return v, ok
}

// beginPairing opens a pairing. The code is not in this response, and that is
// the whole security property: the person who can finish it is the person
// who can see this machine.
func (g *gate) beginPairing(w http.ResponseWriter, r *http.Request) {
	body := readBody(r)
	name, ok := stringField(body, "name")
	if !ok {
		name = auth.DefaultName
	}
	started, err := g.auth.BeginPairing(name)
	if errors.Is(err, auth.ErrRateLimited) {
		writeAuthRefusal(w, http.StatusTooManyRequests, "rate_limited",
			"Too many pairing attempts. Try again in a few minutes.")
		return
	}
	if errors.Is(err, auth.ErrPairingLocked) {
		writeAuthRefusal(w, http.StatusTooManyRequests, "rate_limited",
			"Too many wrong codes. Pairing is closed for now.")
		return
	}
	if err != nil {
		log.Printf("auth: a pairing could not be opened: %v", err)
		writeAuthRefusal(w, http.StatusInternalServerError, "internal", "The pairing could not be opened.")
		return
	}
	writeAuthJSON(w, contract.PairStarted{PairingID: started.ID, Expires: started.Expires.Unix()})
}

func (g *gate) confirmPairing(w http.ResponseWriter, r *http.Request) {
	body := readBody(r)
	id, ok1 := stringField(body, "pairing_id")
	code, ok2 := stringField(body, "code")
	if !ok1 || !ok2 {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That needs a pairing_id and a code.")
		return
	}
	result, err := g.auth.ConfirmPairing(id, code)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	switch result.Kind {
	case auth.Paired:
		signedIn(w, r, result.Token)
	case auth.WrongCode:
		// A code of its own, and the count in the body, so a page can say
		// "two tries left" without reading the sentence or counting.
		writeAuthError(w, http.StatusForbidden, contract.AuthError{
			Code:      "wrong_code",
			Message:   fmt.Sprintf("That code is not right. %d tries left.", result.Left),
			TriesLeft: int64(result.Left),
		})
	default:
		writeAuthRefusal(w, http.StatusForbidden, "expired", "That pairing has expired. Start again.")
	}
}

// adopt turns a token a page was handed into the cookie it can keep. It exists
// because EventSource cannot set a header. Nothing is granted: an unknown
// token is refused as it would be anywhere else.
func (g *gate) adopt(w http.ResponseWriter, r *http.Request) {
	token, ok := stringField(readBody(r), "token")
	if !ok || !g.auth.Verify(token).Allowed {
		writeAuthRefusal(w, http.StatusUnauthorized, "unauthorized", "That token is not one of ours.")
		return
	}
	signedIn(w, r, token)
}

func (g *gate) exchangePassword(w http.ResponseWriter, r *http.Request) {
	body := readBody(r)
	password, ok := stringField(body, "password")
	if !ok {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That needs a password.")
		return
	}
	name, ok := stringField(body, "name")
	if !ok {
		name = auth.DefaultName
	}
	token, ok, err := g.auth.Exchange(r.Context(), password, name)
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		writeAuthRefusal(w, http.StatusTooManyRequests, "rate_limited",
			"Too many wrong passwords. Try again later.")
		return
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The caller has gone; there is nobody to answer.
		return
	case err != nil:
		writeStoreFailure(w, err)
		return
	}
	if !ok {
		writeAuthRefusal(w, http.StatusUnauthorized, "unauthorized", "That is not the password.")
		return
	}
	signedIn(w, r, token)
}

// logout clears the cookie and revokes the device that held it: a browser that
// once had the token may still have it written down, so signing out means the
// key goes too.
//
// This machine's own token is the exception, which the Swift app does not
// make. The shell's window and every script hold it, and a sign-out in one
// window taking it from all of them is not what the button means.
func (g *gate) logout(w http.ResponseWriter, r *http.Request) {
	if v := accessOf(r).verdict; v.Allowed && !v.Local {
		if err := g.auth.Revoke(v.Device); err != nil && !errors.Is(err, auth.ErrNotFound) {
			writeStoreFailure(w, err)
			return
		}
	}
	w.Header().Set("Set-Cookie", sessionCookie+"=; Path=/; Max-Age=0; HttpOnly; SameSite=Strict")
	writeAuthJSON(w, contract.AuthOK{OK: true})
}

// signedIn sends the token twice: in the body for a script that will keep it,
// and in an HttpOnly cookie for a page, because the event stream cannot carry
// a header. Secure only when the request came through HTTPS — which through a
// tunnel it did, and at the desk it did not; set always, the cookie would be
// dropped on http://127.0.0.1 and nothing would work there.
func signedIn(w http.ResponseWriter, r *http.Request, token string) {
	cookie := sessionCookie + "=" + token + "; Path=/; Max-Age=" + cookieLifetime + "; HttpOnly; SameSite=Strict"
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		cookie += "; Secure"
	}
	w.Header().Set("Set-Cookie", cookie)
	writeAuthJSON(w, contract.SignedIn{OK: true, Token: token})
}

// requireLocal answers for a caller that is not this machine's own token.
func requireLocal(w http.ResponseWriter, r *http.Request) bool {
	v := accessOf(r).verdict
	if !v.Allowed {
		writeAuthRefusal(w, http.StatusUnauthorized, "unauthorized", "This needs a paired device.")
		return false
	}
	if !v.Local {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "Only this Mac's own token may manage devices.")
		return false
	}
	return true
}

// pairings is the one place a pairing code leaves this daemon: a server-sent
// stream only this machine's own token may open.
//
// A stream rather than a long poll. A pairing lives two minutes and the code
// has to be on a screen within a second of the request: a stream delivers it
// the moment it exists, over one connection, and a new pairing that replaces
// the old simply arrives as the next event. A long poll would need a cursor
// and a re-arm between answers, and a pairing that landed in that gap would be
// the one nobody saw. This daemon already speaks server-sent events, and so do
// curl, URLSession and a twenty-line Go reader.
func (g *gate) pairings(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAuthRefusal(w, http.StatusInternalServerError, "internal", "This connection cannot stream.")
		return
	}
	holder := accessOf(r).verdict.Device
	notices, cancel := g.auth.Watch()
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": pairings\n\n")
	flusher.Flush()

	ping := time.NewTicker(streamPing)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case n := <-notices:
			// Asked again before every code: a token rotated while this stream
			// was open no longer hears anything.
			if d, ok := g.auth.Holds(holder); !ok || !d.Local {
				return
			}
			data, err := json.Marshal(contract.PairingNotice{
				PairingID: n.ID, Name: n.Name, Code: n.Code, Expires: n.Expires.Unix(),
			})
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "event: pairing\ndata: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// devicesRoute is the device list and what can be done to it, for this
// machine's own token only. A paired device cannot list, revoke or grant.
func (g *gate) devicesRoute(w http.ResponseWriter, r *http.Request, rest string) {
	if !requireLocal(w, r) {
		return
	}
	get, post := r.Method == http.MethodGet, r.Method == http.MethodPost
	// Split first, then decode each segment: the id of a device is a name, and
	// a name is one segment (routePath, gate.go).
	parts := strings.Split(rest, "/")
	for i, part := range parts {
		parts[i] = decodeSegment(part)
	}
	rest = strings.Join(parts, "/")
	switch {
	case get && rest == "":
		g.listDevices(w)
	case post && rest == "revoke-all":
		count, err := g.auth.RevokeAll()
		if err != nil {
			writeStoreFailure(w, err)
			return
		}
		writeAuthJSON(w, contract.RevokedAll{OK: true, Count: int64(count)})
	case post && rest == "browser":
		g.browserDevice(w, r)
	case post && rest == "password":
		password, ok := stringField(readBody(r), "password")
		if !ok {
			writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That needs a password.")
			return
		}
		if err := g.auth.SetPassword(password); err != nil {
			writeStoreFailure(w, err)
			return
		}
		writeAuthJSON(w, contract.AuthOK{OK: true})
	case post && len(parts) == 2 && parts[0] != "" && parts[1] == "revoke":
		writeDeviceChange(w, g.auth.Revoke(parts[0]))
	case post && len(parts) == 2 && parts[0] != "" && parts[1] == "caps":
		raw, ok := readBody(r)["caps"].([]any)
		if !ok {
			writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That needs caps.")
			return
		}
		var caps auth.Caps
		for _, item := range raw {
			name, _ := item.(string)
			c, ok := auth.ParseCapability(name)
			if !ok {
				writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "caps may be read and send.")
				return
			}
			caps = append(caps, c)
		}
		_, err := g.auth.SetCapabilities(parts[0], caps)
		writeDeviceChange(w, err)
	default:
		writeAuthRefusal(w, http.StatusNotFound, "not_found", "No such route")
	}
}

func (g *gate) listDevices(w http.ResponseWriter) {
	out := contract.DeviceList{
		Configured: g.auth.IsConfigured(),
		Password:   g.auth.HasPassword(),
		Devices:    []contract.PairedDevice{},
	}
	for _, d := range g.auth.Devices() {
		caps := make([]string, 0, len(d.Caps))
		for _, c := range d.Caps {
			caps = append(caps, string(c))
		}
		row := contract.PairedDevice{ID: d.ID, Name: d.Name, Caps: caps, Created: d.Created.Unix()}
		if !d.LastSeen.IsZero() {
			row.LastSeen = d.LastSeen.Unix()
		}
		out.Devices = append(out.Devices, row)
	}
	writeAuthJSON(w, out)
}

// browserDevice is `clawdline open`: a device of its own rather than this
// machine's token, because they are not the same thing — the local token may
// administer, and a browser tab starts with less. Sending is granted only when
// the person at this machine asked for it.
func (g *gate) browserDevice(w http.ResponseWriter, r *http.Request) {
	caps := auth.NewCaps(auth.Read)
	if send, _ := readBody(r)["send"].(bool); send {
		caps = auth.NewCaps(auth.Read, auth.Send)
	}
	id, token, err := g.auth.AddDevice(browserName, caps, false)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	// The fragment, not the query: a browser never sends a fragment to the
	// server and never writes it into a log or a Referer. /v1/auth/open trades
	// it for the cookie and takes it out of the address bar.
	writeAuthJSON(w, contract.BrowserDevice{
		ID:    id,
		Token: token,
		URL:   fmt.Sprintf("http://127.0.0.1:%d/v1/auth/open#t=%s", g.port, token),
	})
}

func writeDeviceChange(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		writeAuthJSON(w, contract.AuthOK{OK: true})
	case errors.Is(err, auth.ErrNotFound):
		writeAuthRefusal(w, http.StatusNotFound, "not_found", "No paired device has that id.")
	case errors.Is(err, auth.ErrLocalDevice):
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "This Mac's own token is not managed here.")
	case errors.Is(err, auth.ErrBadCaps):
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "caps may be read and send.")
	default:
		writeStoreFailure(w, err)
	}
}

func writeStoreFailure(w http.ResponseWriter, err error) {
	log.Printf("auth: the device store could not be written: %v", err)
	writeAuthRefusal(w, http.StatusServiceUnavailable, "store_unavailable",
		"The device store could not be written; nothing was changed.")
}

func writeAuthJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// openPage is where `clawdline open` lands. The console this daemon serves
// does not read a `#t=` fragment yet — the Swift page does, in net/fetch.js
// adoptToken — so this stop does exactly that and nothing else: take the token
// out of the fragment, take the fragment out of the address bar before the
// request goes out, trade the token for the cookie, and go to the console.
// The regular expression and the order are adoptToken's.
func openPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = io.WriteString(w, `<!doctype html>
<html><head><meta charset="utf-8"><meta name="color-scheme" content="dark light"><title>clawdline</title></head>
<body><script>
(function () {
  var match = /(?:^|[#&])t=([^&]+)/.exec(location.hash || "");
  var token = match ? decodeURIComponent(match[1]) : "";
  try { history.replaceState(null, "", location.pathname + location.search); } catch (e) { location.hash = ""; }
  function home() { location.replace("/"); }
  if (!token) { home(); return; }
  fetch("/v1/auth/adopt", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token: token }),
    credentials: "same-origin"
  }).then(home, home);
})();
</script></body></html>
`)
}
