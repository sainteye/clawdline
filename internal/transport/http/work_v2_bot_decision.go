package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// A Bot chat has no run: nobody types into it through the send route, so
// `clawdline item add` from a Bot found no message to relay and answered
// no_run. What a Bot chat does have is the person's answer to its own
// ask_person question card, pressed or typed on the console's answer route,
// which no Bot tool can write. POST /v1/work/v2/agent/items with
// {"via":{"decision":"<card id>"}} rests the item on that answer. The daemon
// finds the card itself, in this machine's Bot chats, and admits it only when
// it is an ask_person question of that Bot's own, the person answered it in
// the last day, and session_id is the current native session of the Bot chat
// that asked it (pa.CheckDecisionAuthority). The item arrives unassigned in
// the Bot's Project and records the card as its source; one card backs at
// most as many items as one message does.

// decisionAuthorityByServer holds a test's stand-in for the Bot host lookup.
var decisionAuthorityByServer sync.Map // *Server -> decisionAuthorityFunc

type decisionAuthorityFunc func(ctx context.Context, conversation, offerID string) (pa.DecisionAuthority, error)

func (s *Server) decisionAuthority(ctx context.Context, conversation, offerID string) (pa.DecisionAuthority, error) {
	if f, ok := decisionAuthorityByServer.Load(s); ok {
		return f.(decisionAuthorityFunc)(ctx, conversation, offerID)
	}
	p := &s.projectAssistants
	p.mu.RLock()
	h := p.host
	p.mu.RUnlock()
	if h == nil {
		return pa.DecisionAuthority{}, &pa.DecisionRefusal{Code: pa.DecisionUnknown}
	}
	return h.DecisionAuthority(ctx, conversation, offerID, work.RelayWindow)
}

// decisionRefusals are the typed answers to a card that does not authorize
// the write.
var decisionRefusals = map[string]string{
	pa.DecisionUnknown: "No Bot on this machine asked a question card with that id; nothing was created. " +
		"Ask the person with ask_person, and name the card once they answer it.",
	pa.DecisionUnanswered: "The person has not answered that question card; nothing was created. " +
		"Wait for their answer, or file a proposal with `clawdline item propose`.",
	pa.DecisionNotThePersons: "That card carries another Bot's question, and its answer went to that Bot; nothing was created.",
	pa.DecisionOtherConversation: "That question card belongs to another Bot chat, or to an earlier session of this one; nothing was created. " +
		"Only the Bot chat that asked it, in its current session, creates items on its answer.",
	pa.DecisionExpired: "The person answered that question card more than a day ago; nothing was created. Ask again.",
}

func (s *Server) agentCreateDecisionItem(w http.ResponseWriter, r *http.Request, k store.ReceiptKey, refuse func(error),
	decision, conversation, projectID string, n app.NewSessionItemV2) {
	conversation = strings.TrimSpace(conversation)
	auth, err := s.decisionAuthority(r.Context(), conversation, decision)
	if err != nil {
		var refused *pa.DecisionRefusal
		if errors.As(err, &refused) {
			refuse(&app.WorkError{Status: http.StatusForbidden, Code: refused.Code, Message: decisionRefusals[refused.Code]})
			return
		}
		refuse(&app.WorkError{Status: http.StatusServiceUnavailable, Code: "store_unavailable",
			Message: "The Bot's question cards could not be read; nothing was created. Retry."})
		return
	}
	catalog := s.workV2Projects(r.Context())
	project, ok := catalog.resolve(projectID, auth.CanonicalID)
	if !ok {
		refuse(&app.WorkError{Status: http.StatusUnprocessableEntity, Code: "project_not_found", Message: workV2ProjectNotFound})
		return
	}
	n.Run = work.Run{ID: auth.OfferID, Session: conversation, Principal: "bot_decision:" + auth.BotID,
		At: auth.AnsweredAt, Excerpt: app.RunExcerptOf(auth.Answer)}
	n.SessionID, n.SessionProject = conversation, auth.CanonicalID
	n.Decision, n.BotID = auth.OfferID, auth.BotID
	n.ProjectID, n.ProjectPath = project.ID, project.Path
	var answer []byte
	_, err = s.workV2().CreateFromSession(r.Context(), n, func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		answer, _ = json.Marshal(map[string]any{"ok": true, "assigned": false,
			"assignment_state": "not_requested", "item": s.workV2ItemOf(catalog, v)})
		return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
	})
	if err != nil {
		refuse(err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(answer)
}
