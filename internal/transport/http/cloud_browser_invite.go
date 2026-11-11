package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// CloudBrowserInviteLine is a Cloud link that can invite browsers.
type CloudBrowserInviteLine interface {
	BrowserInvites() *cloudtransport.BrowserInvites
}

// maxBrowserInviteBody bounds every invitation request body: an expiry
// word, an id and a bool.
const maxBrowserInviteBody = 1 << 10

// cloudBrowserInvitesRoute starts and lists browser invitations.
//
//	GET  /v1/cloud/browser-invites   {"invites":[…],"browsers":[…]}: live and recent invitations, and every invited browser
//	POST /v1/cloud/browser-invites   {"expiry":"8h"|"7d"|"never"} → one invitation with its code
//
// This machine's own token only: the answer carries the code that admits a
// browser with operate permission. A paired Cloud viewer reaches it through
// the encrypted `browser-invite-*` words, which an invited browser may not use.
func (s *Server) cloudBrowserInvitesRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	invites, ok := s.cloudBrowserInvites(w)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		browsers, err := invites.InvitedBrowsers()
		if err != nil {
			writeAuthRefusal(w, http.StatusServiceUnavailable, "pinned_unreadable",
				"This machine could not read its paired devices.")
			return
		}
		writeJSON(w, map[string]any{"invites": invites.List(), "browsers": browsers})
	case http.MethodPost:
		var body struct {
			Expiry string `json:"expiry"`
		}
		if !decodeInviteBody(w, r, &body) {
			return
		}
		view, err := invites.Start(r.Context(), body.Expiry)
		if err != nil {
			writeBrowserInviteError(w, err)
			return
		}
		writeJSON(w, view)
	default:
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request",
			"Browser invitations are listed with GET and started with POST.")
	}
}

// cloudBrowserInviteRoute is one invitation.
//
//	GET  /v1/cloud/browser-invites/{id}
//	POST /v1/cloud/browser-invites/{id}/decision  {"approve":true|false}
//	POST /v1/cloud/browser-invites/{id}/cancel
//	POST /v1/cloud/browser-invites/devices/{device}/revoke
func (s *Server) cloudBrowserInviteRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	invites, ok := s.cloudBrowserInvites(w)
	if !ok {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/v1/cloud/browser-invites/")
	parts := strings.Split(rest, "/")
	switch {
	case len(parts) == 3 && parts[0] == "devices" && parts[2] == "revoke" && r.Method == http.MethodPost:
		if parts[1] == "" || len(parts[1]) > 128 {
			writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That is not a device id.")
			return
		}
		changed, err := invites.RevokeInvited(parts[1])
		if err != nil && !changed {
			writeBrowserInviteError(w, err)
			return
		}
		out := map[string]any{"device_id": parts[1], "revoked": true, "changed": changed}
		if err != nil {
			// Refused here; Cloud was not told. The refusal is what counts.
			out["cloud_error"] = "cloud_not_told"
		}
		writeJSON(w, out)
	case len(parts) == 1 && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		if !domaincloud.ValidInviteID(parts[0]) {
			writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That is not an invitation id.")
			return
		}
		view, err := invites.Status(parts[0])
		if err != nil {
			writeBrowserInviteError(w, err)
			return
		}
		writeJSON(w, view)
	case len(parts) == 2 && parts[1] == "decision" && r.Method == http.MethodPost:
		var body struct {
			Approve *bool `json:"approve"`
		}
		if !decodeInviteBody(w, r, &body) {
			return
		}
		if body.Approve == nil || !domaincloud.ValidInviteID(parts[0]) {
			writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "A decision says approve true or false.")
			return
		}
		view, err := invites.Decide(parts[0], *body.Approve)
		if err != nil {
			writeBrowserInviteError(w, err)
			return
		}
		writeJSON(w, view)
	case len(parts) == 2 && parts[1] == "cancel" && r.Method == http.MethodPost:
		if !domaincloud.ValidInviteID(parts[0]) {
			writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That is not an invitation id.")
			return
		}
		view, err := invites.Cancel(r.Context(), parts[0])
		if err != nil {
			writeBrowserInviteError(w, err)
			return
		}
		writeJSON(w, view)
	default:
		writeAuthRefusal(w, http.StatusNotFound, "not_found", "There is no such browser invitation route.")
	}
}

func decodeInviteBody(w http.ResponseWriter, r *http.Request, out any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBrowserInviteBody+1))
	if err != nil || len(raw) > maxBrowserInviteBody {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That request body could not be read.")
		return false
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That request body is not what this route reads.")
		return false
	}
	return true
}

func (s *Server) cloudBrowserInvites(w http.ResponseWriter) (*cloudtransport.BrowserInvites, bool) {
	line, ok := cloudLines.Load(s.cfg.Dir)
	if !ok {
		writeAuthRefusal(w, http.StatusConflict, "cloud_off", "The Cloud line is off in this app's settings.")
		return nil, false
	}
	holder, ok := line.(CloudBrowserInviteLine)
	if !ok || holder.BrowserInvites() == nil {
		writeAuthRefusal(w, http.StatusConflict, "cloud_not_signed_in",
			"This machine is not connected to a Clawdline Cloud account. Run `clawdline cloud login` first.")
		return nil, false
	}
	return holder.BrowserInvites(), true
}

func writeBrowserInviteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cloudtransport.ErrPairingUnavailable):
		writeAuthRefusal(w, http.StatusConflict, "cloud_not_signed_in", err.Error())
	case errors.Is(err, cloudtransport.ErrInviteExpiry):
		writeAuthRefusal(w, http.StatusBadRequest, "invite_expiry", err.Error())
	case errors.Is(err, cloudtransport.ErrInviteNoRoom):
		writeAuthRefusal(w, http.StatusConflict, "invite_limit", err.Error())
	case errors.Is(err, cloudtransport.ErrInviteUnknown):
		writeAuthRefusal(w, http.StatusNotFound, "unknown_invite", err.Error())
	case errors.Is(err, cloudtransport.ErrInviteNotWaiting):
		writeAuthRefusal(w, http.StatusConflict, "invite_not_waiting", err.Error())
	case errors.Is(err, adaptercloud.ErrInviteUnusable):
		writeAuthRefusal(w, http.StatusGone, "invite_gone", err.Error())
	default:
		writeAuthRefusal(w, http.StatusBadGateway, "invite_failed", "The browser invitation could not be completed.")
	}
}
