package http

import (
	"net/http"
	"strings"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// A local human-manager ingress only. Cloud's closed source decoder does not
// expose this operation, and a native/machine bearer cannot call it.
func (s *Server) botNetworkConsentRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a := accessOf(r)
	if cloudtransport.ViaCloud(r.Context()) || cloudtransport.CloudViewerOf(r.Context()) != "" || a.machine || a.gate == nil || a.gate != s.gate() {
		paWriteHTTPFailure(w, paHTTPFailure(pa.ScopeDenied, "local_owner"))
		return
	}
	v := a.gate.auth.Revalidate(a.verdict)
	if !v.Allowed || !v.Local {
		paWriteHTTPFailure(w, paHTTPFailure(pa.ScopeDenied, "local_owner"))
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/bot-network/consents/"), "/")
	if r.Method != http.MethodPost || len(parts) != 2 || parts[1] != "sync" || !paHTTPSafeBotID(parts[0]) || r.URL.RawQuery != "" || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		paWriteHTTPFailure(w, paHTTPFailure(pa.Invalid, "bot_cloud_consent"))
		return
	}
	s.projectAssistants.mu.RLock()
	n := s.projectAssistants.network
	s.projectAssistants.mu.RUnlock()
	if n == nil {
		paWriteHTTPFailure(w, paHTTPFailure(pa.Unsupported, "bot_cloud_unconfigured"))
		return
	}
	_, authority, e := n.active(r.Context())
	if e == nil {
		e = authority.SyncConsent(r.Context(), parts[0])
	}
	if e != nil {
		paWriteHTTPFailure(w, e)
		return
	}
	writeJSON(w, map[string]any{"synced": true, "authority_id": parts[0]})
}
