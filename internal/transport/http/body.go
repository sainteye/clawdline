package http

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/sainteye/clawdline/internal/domain/capacity"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/projectsync"
)

// Every request body this daemon reads itself has a size it is refused past
// (docs/limits.md N27). Before, the broker's routes, the coordinator, a
// session's close and the retired dispatch read `json.NewDecoder(r.Body)` with
// no bound at all, and the auth routes cut a body at 64 KiB and then read what
// was left as `{}`.
//
// The bound is decided here, once, by the route the mux will actually run —
// the same lookup it dispatches by — so a route added tomorrow is bounded by
// default rather than by somebody remembering. A route whose handler has a
// larger bound of its own is named in bodyLimits; a handler's own smaller bound
// still applies inside this one. The body is read here, up to its bound, and
// handed on whole: past the bound the request is refused with 413
// `body_too_large` and the handler never runs, so no handler ever sees part of
// a body, and a chunked body without a length is refused exactly as one that
// declared it.
//
// The one route not bounded here is the fallback: the proxy streams a request
// to the Swift app, which has a bound of its own, and the standalone page reads
// no body.

const (
	// commandBodyLimit is every owned route without a bound of its own named
	// below. The largest body among them is a relayed message: 100,000
	// characters, which a client escaping everything outside ASCII writes in
	// twelve bytes each.
	commandBodyLimit = 2 << 20
	// authBodyLimit is the Swift app's reading of an auth or push body.
	authBodyLimit = 64 << 10
	// terminalBodyLimit is one terminal request: the largest is a 1 MiB
	// paste, which JSON writes in up to six bytes a byte (`\u0001`), plus
	// room for the request's other fields (`terminal.body_bytes`).
	terminalBodyLimit = 6<<20 + 4<<10
)

// bodyLimits are the routes whose bound is not commandBodyLimit, by the
// pattern they are registered under.
var bodyLimits = map[string]int64{
	"/v1/auth/":         authBodyLimit,
	"/v1/push/":         authBodyLimit,
	"/v1/sessions/":     sendBodyLimit,
	"/v1/voice":         voiceBodyLimit,
	"/v1/voice/preview": capacity.MaximumWhisperPreviewBodyBytes,
	"/v1/terminals/":    terminalBodyLimit,
	// One project's settings with contents (project_sync.go).
	"/v1/project-sync/": projectsync.MaxEntryBytes,
}

// botBodyPattern is the one pattern every Bot route is registered under, so
// the routes that carry pictures are told apart by their path.
const botBodyPattern = "/v1/project-assistants/"

// bodyLimitFor is the bound for a request the mux dispatches under pattern,
// and false for the fallback, which is not bounded here.
func bodyLimitFor(pattern string, r *http.Request) (int64, bool) {
	if pattern == "/" {
		return 0, false
	}
	if pattern == botBodyPattern && botPictureRoute(r) {
		return capacity.MaximumBotSendBodyBytes, true
	}
	if n, ok := bodyLimits[pattern]; ok {
		return n, true
	}
	return commandBodyLimit, true
}

// botPictureRoute is a Bot send or queue edit, the two writes whose body
// carries pictures as data: URLs (capacity bots.send_body_bytes), told apart
// by the same parse the Bot handler dispatches by. Every other Bot route keeps
// commandBodyLimit.
func botPictureRoute(r *http.Request) bool {
	route, err := paRoute(r)
	return err == nil && r.Method == http.MethodPost && (route.action == "send" || route.action == "queue-edit")
}

// boundBodies reads each request's body to its route's bound before the mux
// runs the route.
func boundBodies(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Body == http.NoBody {
			mux.ServeHTTP(w, r)
			return
		}
		_, pattern := mux.Handler(r)
		limit, bounded := bodyLimitFor(pattern, r)
		if !bounded {
			mux.ServeHTTP(w, r)
			return
		}
		tooLarge := writeBodyTooLarge
		if pattern == botBodyPattern {
			tooLarge = writeBotBodyTooLarge
		}
		if r.ContentLength > limit {
			tooLarge(w, limit)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
		if err != nil {
			writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "The request body could not be read.")
			return
		}
		if int64(len(body)) > limit {
			tooLarge(w, limit)
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		mux.ServeHTTP(w, r)
	})
}

func writeBodyTooLarge(w http.ResponseWriter, limit int64) {
	// The connection is not reused: what is left of the body is unread.
	w.Header().Set("Connection", "close")
	writeAuthRefusal(w, http.StatusRequestEntityTooLarge, "body_too_large",
		fmt.Sprintf("This route reads a body of at most %d bytes.", limit))
}

// writeBotBodyTooLarge answers a Bot route in the Bot error shape, so a page
// reads `pa_capacity_full` field `body` as a body too large for this route, a
// 413 it should not retry, never a busy 429.
func writeBotBodyTooLarge(w http.ResponseWriter, _ int64) {
	w.Header().Set("Connection", "close")
	w.Header().Set("Cache-Control", "no-store")
	paWriteHTTPFailure(w, paHTTPFailure(pa.CapacityFull, paBodyTooLargeField))
}
