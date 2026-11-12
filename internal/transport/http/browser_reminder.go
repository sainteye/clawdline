package http

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/work"
	"github.com/sainteye/clawdline/internal/productcopy"
)

// browserReminderRoute saves an explicitly requested reminder on the same Session.
// It uses the existing durable Note and idempotency boundaries; it starts no browser.
func (s *Server) browserReminderRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	actor, ok := requirePersonWorkV2(w, r)
	if !ok {
		return
	}
	var body struct {
		Conversation string `json:"conversation"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	if s.broker == nil {
		writeRefusal(w, http.StatusServiceUnavailable, "session_unresolved", "The live Session registry is unavailable.")
		return
	}
	live, err := s.broker.LiveSession(r.Context(), body.Conversation)
	if err != nil {
		s.writeHumanInterventionIdentityError(w, err)
		return
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	lang := s.productLanguage()
	note := app.CreateHumanIntervention{SourceConversation: body.Conversation, SourceLabel: "Clawdline", TargetConversation: body.Conversation, TargetSession: live.ID,
		Kind: "browser", Title: productcopy.Format(lang, "browser.reminder.title", nil), Summary: productcopy.Format(lang, "browser.reminder.summary", nil), Action: productcopy.Format(lang, "browser.reminder.action", nil), Reason: productcopy.Format(lang, "browser.reminder.reason", nil), RequestedByPerson: true}
	_, response, err := s.humanInterventions().Create(r.Context(), note, k, func(v work.HumanIntervention) []byte {
		b, _ := json.Marshal(map[string]any{"ok": true, "note": humanInterventionOf(v)})
		return b
	})
	if err != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		s.writeWorkV2Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(response)
}
