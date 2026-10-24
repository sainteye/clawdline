package http

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/antigravityproject"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

const antigravityConsentBodyBytesLimit = capacity.AntigravityConsentRequestBytesLimit

// The place resolves on this machine. A browser supplies only the approved
// revision/digests, never a Project path, helper, actor or operation identity.
// The manager's SQLite phase receipts own recovery; do not wrap this in a
// second pending receipt that could strand a prepared operation on restart.
func (s *Server) antigravityProjectConsentRoute(w http.ResponseWriter, r *http.Request, place string) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeRefusal(w, 405, "method_not_allowed", "Read Antigravity consent with GET or change it with POST.")
		return
	}
	if r.URL.RawQuery != "" {
		writeRefusal(w, 400, "bad_request", "Antigravity consent takes no query fields.")
		return
	}
	if r.Method == http.MethodPost && (!maySend(r) || actorOf(r) == "") {
		writeRefusal(w, 403, "forbidden", "Changing Antigravity consent requires send access to this machine.")
		return
	}
	project, found := s.workV2Project(r.Context(), place)
	if !found {
		writeRefusal(w, 404, "project_not_found", "Choose a Project that this machine currently lists.")
		return
	}
	owner := s.antigravityProjectOwner.Load()
	if owner == nil {
		writeRefusal(w, 503, "antigravity_project_unavailable", "Antigravity consent is unavailable on this machine. Read its status again before changing it.")
		return
	}
	if r.Method == http.MethodGet {
		// Read even unknown-length bodies through a bounded reader; no chunked
		// request body may be mistaken for an empty read.
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) != 0 {
			writeRefusal(w, 400, "bad_request", "An Antigravity consent read takes no body.")
			return
		}
		out, err := owner.Status(r.Context(), project.Path)
		if err != nil {
			writeAntigravityConsentRefusal(w, err)
			return
		}
		writeJSON(w, antigravityConsentWire(out))
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) == 0 || len(key) > store.AntigravityConsentIdentityBytesLimit || strings.ContainsFunc(key, func(c rune) bool { return c < 33 || c > 126 }) {
		writeRefusal(w, 400, "bad_request", "Antigravity consent needs an Idempotency-Key of at most 128 visible ASCII bytes.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, antigravityConsentBodyBytesLimit+1))
	if err != nil || len(body) > antigravityConsentBodyBytesLimit {
		writeRefusal(w, 413, "antigravity_consent_body_too_large", "An Antigravity consent change is at most 4096 bytes.")
		return
	}
	var input contract.AntigravityConsentChange
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil || dec.Decode(new(any)) != io.EOF || input.Revision < 0 || input.Scope != antigravityproject.Scope || (input.Action != contract.AntigravityConsentChangeActionEnable && input.Action != contract.AntigravityConsentChangeActionDisable) {
		writeRefusal(w, 400, "bad_request", "Approve the displayed Antigravity consent scope and revision.")
		return
	}
	// Go's zero integer cannot distinguish an approved revision zero from a
	// missing/null required field. Require its actual presence on the wire.
	var required struct {
		Revision *int64 `json:"revision"`
	}
	if json.Unmarshal(body, &required) != nil || required.Revision == nil {
		writeRefusal(w, 400, "bad_request", "Approve the displayed Antigravity consent scope and revision.")
		return
	}
	// Hash authenticated identity into the bounded private receipt namespace.
	// Route scopes the key, but action/body do not: changed input under the same
	// key conflicts rather than becoming a new operation.
	actor := requestDigest([]byte(actorOf(r)))
	receiptKey := requestDigest([]byte(routePath(r)), []byte(key))
	canonical, _ := json.Marshal(input)
	operation := requestDigest([]byte(r.Method), []byte(routePath(r)), canonical)
	switch input.Action {
	case contract.AntigravityConsentChangeActionEnable:
		p := input.Preview
		if p == nil || (p.BeforeDigest != "absent" && !antigravityConsentDigest(p.BeforeDigest)) || !antigravityConsentDigest(p.HelperDigest) || !antigravityConsentDigest(p.ManagedDigest) {
			writeRefusal(w, 400, "bad_request", "Approve the displayed Antigravity consent scope and revision.")
			return
		}
		// The coordinator validates this exact approved plan against the current
		// image, catalog and files, including recovery after a lost answer.
		_, err = owner.Enable(r.Context(), antigravityproject.Plan{ProjectRoot: project.Path, Workspace: project.Path, Revision: input.Revision, BeforeDigest: p.BeforeDigest, HelperDigest: p.HelperDigest, ManagedDigest: p.ManagedDigest}, actor, receiptKey, operation)
	case contract.AntigravityConsentChangeActionDisable:
		if input.Preview != nil {
			writeRefusal(w, 400, "bad_request", "Approve the displayed Antigravity consent scope and revision.")
			return
		}
		_, err = owner.Disable(r.Context(), project.Path, input.Revision, actor, receiptKey, operation)
	}
	if err != nil {
		writeAntigravityConsentRefusal(w, err)
		return
	}
	// Historical command answers never become the displayed current state.
	out, err := owner.Status(r.Context(), project.Path)
	if err != nil {
		writeAntigravityConsentRefusal(w, err)
		return
	}
	writeJSON(w, antigravityConsentWire(out))
}
func antigravityConsentDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && strings.ToLower(s) == s
}
func antigravityConsentWire(s antigravityproject.Snapshot) contract.AntigravityConsentStatus {
	out := contract.AntigravityConsentStatus{Phase: contract.AntigravityConsentStatusPhase(s.Consent.Phase), Revision: s.Consent.Revision, Scope: antigravityproject.Scope, Hooks: contract.AntigravityConsentStatusHooks(s.Hooks), Reason: s.Reason}
	if p := s.Preview; p != nil {
		out.Preview = &contract.AntigravityConsentPreview{BeforeDigest: p.BeforeDigest, HelperDigest: p.HelperDigest, ManagedDigest: p.ManagedDigest}
	}
	return out
}
func writeAntigravityConsentRefusal(w http.ResponseWriter, err error) {
	code, status := "antigravity_project_unavailable", 503
	var project antigravityproject.Error
	var consent store.AntigravityConsentError
	if errors.As(err, &project) {
		code = string(project)
		status = 409
	}
	if errors.As(err, &consent) {
		code = string(consent)
		status = 409
		switch consent {
		case store.AntigravityConsentBusy, store.AntigravityConsentUnreadable, store.AntigravityConsentWriteFailed:
			status = 503
		case store.AntigravityConsentCapacity:
			status = 429
		}
	}
	// Even a refused write may have prepared, installed or revoked. Readers must
	// refresh before describing effects, then retry only the same reviewed input.
	writeRefusal(w, status, code, "Read Antigravity consent again to check its current state. If an answer was lost, retry only the same approved change with the same key.")
}
