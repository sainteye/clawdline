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
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	adaptercloud "github.com/sainteye/clawdline-go/internal/adapters/cloud"
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

// MARK: pairing

// CloudPairingLine is the half of a Cloud link the pairing routes drive. It is
// separate from CloudLine so that a build with a link but no identity — the
// switch is on and `cloud login` has never run — answers the status route and
// refuses these, which is the honest pair of answers.
type CloudPairingLine interface {
	CloudLine
	Pairing() *cloudtransport.Pairing
	RotationCost() []string
	RotateSigningKey(ctx context.Context, confirm bool) (cloudtransport.RotationOutcome, error)
}

// cloudPairingRoute begins, reads or cancels this machine's one pairing.
//
//	GET    /v1/cloud/pairing   what the pairing in progress is doing
//	POST   /v1/cloud/pairing   show a fresh invitation
//	DELETE /v1/cloud/pairing   stop waiting for one
//
// **This machine's own token only, and that is not a formality.** The `POST`
// answers a link whose fragment carries a one-time secret that hands the
// account's master secret to whoever opens it. A route that a tunnelled phone
// or a Cloud viewer could reach would let a reader of this Mac's sessions mint
// itself a second, fully-paired browser.
func (s *Server) cloudPairingRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	pairing, ok := s.cloudPairing(w)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		writeJSON(w, pairing.State())
	case http.MethodPost:
		state, err := pairing.Begin(r.Context())
		if err != nil {
			writeCloudPairingError(w, err)
			return
		}
		writeJSON(w, state)
	case http.MethodDelete:
		writeJSON(w, pairing.Cancel())
	default:
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request",
			"A pairing is read with GET, started with POST and stopped with DELETE.")
	}
}

// cloudPairingOfferRoute finishes a pairing from a code the person carried.
//
// `POST /v1/cloud/pairing/offer` with `{"offer":"<fragment>"}`. It is the
// desktop path: a browser on another screen shows its own pairing code and
// there is no camera to point at this Mac. The cryptography is the invitation
// path's, exactly; only the way the offer arrived differs.
func (s *Server) cloudPairingOfferRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "A pairing code is offered with POST.")
		return
	}
	pairing, ok := s.cloudPairing(w)
	if !ok {
		return
	}
	var body struct {
		Offer string `json:"offer"`
	}
	// The bound is the Swift app's `maxOfferFragmentUTF8Bytes`: a closed offer
	// has bounded ids and fixed-size keys, so four KiB is generous for it and
	// still refuses attacker-sized text before any decoding.
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&body); err != nil {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That request body is not readable JSON.")
		return
	}
	offer := strings.TrimSpace(body.Offer)
	if offer == "" || len(offer) > 4096 {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "A pairing code is 1 to 4096 characters.")
		return
	}
	state, err := pairing.Complete(r.Context(), offer)
	if err != nil {
		writeCloudPairingError(w, err)
		return
	}
	writeJSON(w, state)
}

// cloudDeviceRoute throws a paired viewer out of this machine.
//
// `POST /v1/cloud/devices/revoke` with `{"device":"<id>"}`. It is local and it
// is immediate: the account's own revocation needs a browser session this
// daemon does not have, and would in any case reach this machine only at the
// next roster refresh.
func (s *Server) cloudDeviceRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "A viewer is revoked with POST.")
		return
	}
	pairing, ok := s.cloudPairing(w)
	if !ok {
		return
	}
	var body struct {
		Device string `json:"device"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&body); err != nil {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That request body is not readable JSON.")
		return
	}
	device := strings.TrimSpace(body.Device)
	if device == "" {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "Name the viewer to revoke.")
		return
	}
	changed, err := pairing.Revoke(device)
	if err != nil {
		writeCloudPairingError(w, err)
		return
	}
	writeJSON(w, map[string]any{"device": device, "revoked": changed})
}

// cloudRotateRoute replaces this machine's signing key.
//
//	GET  /v1/cloud/keys/rotate   what a rotation would cost
//	POST /v1/cloud/keys/rotate   do it, with `{"confirm": true}`
//
// The GET exists so that the question a person is asked has names in it. Every
// browser that pinned the old key stops being able to verify this machine, and
// "are you sure" is not a useful sentence unless it says which browsers.
func (s *Server) cloudRotateRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	line, ok := cloudLines.Load(s.cfg.Dir)
	if !ok {
		writeAuthRefusal(w, http.StatusConflict, "cloud_off", "The Cloud line is off in this app's settings.")
		return
	}
	holder, ok := line.(CloudPairingLine)
	if !ok {
		writeAuthRefusal(w, http.StatusConflict, "cloud_off", "This Cloud line cannot rotate a key.")
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		writeJSON(w, map[string]any{"repair": holder.RotationCost()})
	case http.MethodPost:
		var body struct {
			Confirm bool `json:"confirm"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&body)
		}
		outcome, err := holder.RotateSigningKey(r.Context(), body.Confirm)
		if errors.Is(err, cloudtransport.ErrRotationUnconfirmed) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":   map[string]any{"code": "confirm_required", "message": err.Error()},
				"repair":  outcome.Repair,
				"confirm": "post {\"confirm\": true} to rotate anyway",
			})
			return
		}
		if err != nil {
			writeCloudPairingError(w, err)
			return
		}
		writeJSON(w, outcome)
	default:
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request",
			"A rotation is previewed with GET and done with POST.")
	}
}

// cloudPairing answers the link's pairing, or writes the refusal that says why
// there is none.
func (s *Server) cloudPairing(w http.ResponseWriter) (*cloudtransport.Pairing, bool) {
	line, ok := cloudLines.Load(s.cfg.Dir)
	if !ok {
		writeAuthRefusal(w, http.StatusConflict, "cloud_off",
			"The Cloud line is off in this app's settings.")
		return nil, false
	}
	holder, ok := line.(CloudPairingLine)
	if !ok {
		writeAuthRefusal(w, http.StatusConflict, "cloud_off", "This Cloud line cannot pair.")
		return nil, false
	}
	pairing := holder.Pairing()
	if pairing == nil {
		writeAuthRefusal(w, http.StatusConflict, "cloud_not_signed_in",
			"This machine is not connected to a Clawdline Cloud account. Run `clawdline cloud login` first.")
		return nil, false
	}
	return pairing, true
}

// writeCloudPairingError turns the typed refusals into one sentence and one
// status, so that "wait" and "start again" are told apart by a caller that
// reads neither Go errors nor English.
func writeCloudPairingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cloudtransport.ErrPairingUnavailable):
		writeAuthRefusal(w, http.StatusConflict, "cloud_not_signed_in", err.Error())
	case errors.Is(err, adaptercloud.ErrPairingPending):
		writeAuthRefusal(w, http.StatusAccepted, "pairing_pending", err.Error())
	case errors.Is(err, adaptercloud.ErrInvitationGone):
		writeAuthRefusal(w, http.StatusConflict, "pairing_expired", err.Error())
	case errors.Is(err, adaptercloud.ErrPairingUnknown):
		writeAuthRefusal(w, http.StatusNotFound, "unknown_pairing", err.Error())
	case errors.Is(err, adaptercloud.ErrPairingRefused):
		writeAuthRefusal(w, http.StatusForbidden, "pairing_refused", err.Error())
	default:
		writeAuthRefusal(w, http.StatusBadRequest, "pairing_failed", err.Error())
	}
}
