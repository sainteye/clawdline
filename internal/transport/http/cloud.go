package http

// `/v1/cloud/status` — what the line to app.clawdline.com is doing.
//
// **Only this machine's own token reads it.** It names the relay, the control
// plane, the account and machine ids, this machine's key fingerprint and the
// account's enrolled viewers; a paired phone has no use for any of that and a
// tunnel is exactly where it should not go. It is the same rule
// `/v1/diagnostics` keeps, for the same reason.
//
// The link is registered rather than held on the Server, because the daemon
// builds the link *after* the handler exists — the link answers Cloud requests
// by dispatching into that handler, so one of the two has to come second. The
// registry is keyed by state directory, which is how this package already
// holds the gate and the settings file: a test server with a directory of its
// own gets a link of its own.

import (
	"net/http"
	"sync"

	cloudtransport "github.com/sainteye/clawdline-go/internal/transport/cloud"
)

// CloudLine is the half of a Cloud link this route reads. It is an interface so
// that a test can answer a fixed status without opening a settings file, a key
// store or a socket.
type CloudLine interface {
	Status() cloudtransport.Status
}

var cloudLines sync.Map // dir -> CloudLine

// SetCloudLine registers the link that answers for a state directory. Passing
// nil unregisters it, which is what a daemon whose switch is off does.
func SetCloudLine(dir string, line CloudLine) {
	if line == nil {
		cloudLines.Delete(dir)
		return
	}
	cloudLines.Store(dir, line)
}

// cloudStatusRoute answers what the Cloud line is doing.
//
// A daemon with no link registered answers `enabled: false` rather than 404.
// The difference matters to the settings page: a route that is not there means
// "this build has no Cloud", and a route answering `enabled: false` means "this
// build has Cloud and you have not turned it on", and those send a person to
// two different places.
func (s *Server) cloudStatusRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "The cloud status is read with GET.")
		return
	}
	if !requireLocal(w, r) {
		return
	}
	line, ok := cloudLines.Load(s.cfg.Dir)
	if !ok {
		writeJSON(w, cloudtransport.Status{Enabled: false, State: cloudtransport.StateOff})
		return
	}
	writeJSON(w, line.(CloudLine).Status())
}

// CloudCredentials hands the Cloud link the two credentials the gate judges an
// in-process request by: this machine's own device token, and the orchestrator
// token that the machine-scoped routes want instead.
//
// They are returned rather than reached for, because the gate's store is this
// package's and the link is not. Neither is logged, and the orchestrator token
// being unusable is not an error here: the link carries what it has, and a
// machine-scoped route then refuses for itself with its own sentence.
func (s *Server) CloudCredentials() (local string, machine string, err error) {
	g := s.gate()
	if g.err != nil {
		return "", "", g.err
	}
	local, err = g.auth.LocalToken()
	if err != nil {
		return "", "", err
	}
	if token, err := g.files.MachineToken(); err == nil {
		machine = token
	}
	return local, machine, nil
}
