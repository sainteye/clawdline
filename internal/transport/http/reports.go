package http

import (
	"net"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/turnreport"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

// GET /reports/<id>: a turn's status report (`clawdline report`), for a
// browser on this machine.
//
// **This is the one place the daemon answers an HTML page it did not ship.**
// internal/adapters/documents keeps HTML out of what it shows, because a
// document there can be any file in a directory anybody may write to. This
// route answers only what `clawdline report` wrote into this daemon's own
// state directory, and only to this machine:
//
//   - The id is one directory name of one fixed shape; nothing from the
//     request is ever joined to a path but that (turnreport.ReadStored), and a
//     link, a page without the generator's mark, or anything else is not found.
//   - A Cloud viewer's request, answered in process, is refused: the report
//     never travels over the Cloud line.
//   - So is any request whose Host is not a loopback name, which is how the
//     tunnel's requests arrive, and any that did not come over a loopback
//     socket.
//   - The gate in front still wants a device's credential, as for every other
//     route that names a repository's contents.
//   - The answer carries its own Content-Security-Policy: a sandbox with
//     scripts and nothing else, no loads from anywhere, no framing. The page's
//     own policy names its script by hash and both apply, so only that script
//     runs.
func (s *Server) reportRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "a report is read with GET")
		return
	}
	if cloud.ViaCloud(r.Context()) {
		writeRefusal(w, http.StatusForbidden, "report_not_over_cloud",
			"a turn's report is opened on this machine, never through Clawdline Cloud")
		return
	}
	if !loopbackHost(r.Host) || !loopbackPeer(r.RemoteAddr) || r.Header.Get("Cf-Connecting-Ip") != "" {
		writeRefusal(w, http.StatusForbidden, "report_local_only",
			"a turn's report is opened from a browser on this machine, at 127.0.0.1")
		return
	}
	id := decodeSegment(strings.TrimPrefix(routePath(r), "/reports/"))
	page, err := turnreport.ReadStored(s.cfg.Dir, id)
	if err != nil {
		writeRefusal(w, http.StatusNotFound, "report_not_found", "No report has that address.")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", reportPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "private, no-store")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(page)
	}
}

// reportPolicy sandboxes the page into an origin of its own with scripts on,
// and lets it load nothing. Inline script is allowed here so that a report
// written by an older or newer `clawdline report` still runs; the page's own
// meta policy narrows that to its one script's hash.
const reportPolicy = "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline'; " +
	"style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// loopbackHost is a Host header naming this machine by a loopback name, with
// any port. A tunnel's name, or a configured remote hostname, is not one.
func loopbackHost(header string) bool {
	host := strings.ToLower(strings.TrimSpace(header))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// loopbackPeer is a connection from this machine. An in-process request has
// no peer at all, and is not one.
func loopbackPeer(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
