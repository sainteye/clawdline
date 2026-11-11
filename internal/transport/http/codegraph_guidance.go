package http

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/lane"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/codegraphguidance"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func guidanceConversation(item session.Session) string {
	if item.ConversationID == "" {
		return ""
	}
	return string(item.Assistant) + ":" + item.ConversationID
}
func guidanceWire(item session.Session, g store.CodegraphGuidance) *contract.CodegraphGuidance {
	return &contract.CodegraphGuidance{Conversation: item.ConversationID, Enabled: g.Enabled, Revision: g.Revision, Version: g.Version, State: g.State, Source: g.Source}
}
func (s *Server) guidanceDefault(ctx context.Context, item session.Session) store.CodegraphGuidance {
	var info contract.SessionInfo
	s.fillWorkflowInfo(ctx, item, &info)
	if info.Workflow != nil && info.Workflow.State == "unavailable" {
		return store.CodegraphGuidance{State: "unavailable"}
	}
	if info.Workflow != nil && info.Workflow.CodegraphEnabled != nil && *info.Workflow.CodegraphEnabled {
		return store.CodegraphGuidance{Enabled: true, Version: codegraphguidance.Version(), Language: "en", State: "launch", Source: "launch"}
	}
	return store.CodegraphGuidance{Version: codegraphguidance.Version(), Language: "en", State: "off", Source: "session"}
}
func (s *Server) fillCodegraphGuidance(ctx context.Context, item session.Session, info *contract.SessionInfo) {
	if s.store == nil || guidanceConversation(item) == "" {
		return
	}
	g, found, err := s.store.CodegraphGuidance(ctx, guidanceConversation(item))
	if err != nil {
		info.CodegraphGuidance = &contract.CodegraphGuidance{Conversation: item.ConversationID, State: "unavailable"}
		return
	}
	if !found {
		g = s.guidanceDefault(ctx, item)
	}
	info.CodegraphGuidance = guidanceWire(item, g)
}
func (s *Server) configureCodegraphGuidance(ctx context.Context, w http.ResponseWriter, r *http.Request, id string, raw []byte) {
	if r.Header.Get("Idempotency-Key") == "" {
		writeRefusal(w, 400, "idempotency_key_required", "This preference needs an Idempotency-Key.")
		return
	}
	var body contract.CodegraphGuidanceRequest
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 5 || (body.Language != "en" && body.Language != "zh-Hant") || body.Conversation == "" || body.Revision < 0 {
		writeRefusal(w, 400, "bad_request", "A CodeGraph preference needs the current conversation, revision, enabled, again and language.")
		return
	}
	for _, name := range []string{"conversation", "revision", "enabled", "again", "language"} {
		if value, ok := fields[name]; !ok || string(value) == "null" {
			writeRefusal(w, 400, "bad_request", "The CodeGraph preference is incomplete.")
			return
		}
	}
	if s.store == nil {
		writeRefusal(w, 503, "store_unavailable", "The conversation preference could not be stored; nothing was changed.")
		return
	}
	item, err := s.actions().Find(ctx, id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	if item.ConversationID != body.Conversation || guidanceConversation(item) == "" {
		writeRefusal(w, 409, "conversation_moved", "The Session now names a different conversation; refresh before changing its preference.")
		return
	}
	if !s.admitPinnedTarget(w, r, id) {
		return
	}
	if body.Enabled && !s.codegraphEnabled() {
		writeRefusal(w, 403, "codegraph_disabled", "Enable CodeGraph in this machine's integration settings to use the guidance.")
		return
	}
	previous := store.CodegraphGuidance{}
	if body.Revision == 0 {
		previous = s.guidanceDefault(ctx, item)
		if previous.State == "unavailable" {
			writeRefusal(w, 503, "store_unavailable", "The original launch context could not be verified; nothing was changed.")
			return
		}
	}
	g, err := s.store.ConfigureCodegraphGuidance(ctx, guidanceConversation(item), body.Revision, body.Enabled, body.Again || previous.Enabled, codegraphguidance.Version(), body.Language, false)
	if errors.Is(err, store.ErrCodegraphGuidanceChanged) {
		writeRefusal(w, 409, "codegraph_guidance_changed", "Another browser changed this preference; refresh before choosing again.")
		return
	}
	if err != nil {
		writeRefusal(w, 503, "store_unavailable", "The conversation preference could not be stored; nothing was changed.")
		return
	}
	writeJSON(w, guidanceWire(item, g))
}

// prepareCodegraphSend is only called by a person's receipted HTTP send. Broker
// notices, peer messages and background jobs never acquire these instructions.
func (s *Server) prepareCodegraphSend(ctx context.Context, id, expected, text string) (string, func(error), error) {
	noop := func(error) {}
	if s.store == nil {
		return "", noop, nil
	}
	item, err := s.actions().Find(ctx, id)
	if err != nil {
		return "", noop, err
	}
	if expected != "" && item.ConversationID != expected {
		return "", noop, app.Refusal{Code: "conversation_moved", Detail: "The Session now names a different conversation; refresh before sending.", Cause: terminal.Unsent{Why: "conversation changed"}}
	}
	conversation := guidanceConversation(item)
	if conversation == "" || strings.HasPrefix(strings.TrimSpace(text), "/") {
		return "", noop, nil
	}
	g, found, err := s.store.CodegraphGuidance(ctx, conversation)
	if err != nil {
		return "", noop, app.Refusal{Code: "store_unavailable", Detail: "The conversation preference could not be read; nothing was typed."}
	}
	if !found {
		g = s.guidanceDefault(ctx, item)
		if g.State == "unavailable" {
			return "", noop, app.Refusal{Code: "store_unavailable", Detail: "The original launch context could not be verified; nothing was typed."}
		}
		if g.Enabled {
			_, err = s.store.ConfigureCodegraphGuidance(ctx, conversation, 0, true, false, g.Version, "en", true)
			if err != nil {
				return "", noop, err
			}
		} else {
			return "", noop, nil
		}
	}
	if g.Enabled && !s.codegraphEnabled() {
		return "", noop, nil
	}
	g, claimed, err := s.store.ClaimCodegraphGuidance(ctx, conversation)
	if err != nil {
		return "", noop, app.Refusal{Code: "store_unavailable", Detail: "The conversation preference could not be claimed; nothing was typed."}
	}
	if !claimed {
		return "", noop, nil
	}
	suffix := codegraphguidance.Text(g.Language, g.Enabled)
	finish := func(sendErr error) {
		state := "provided"
		if sendErr != nil {
			state = "unknown"
			var unsent terminal.Unsent
			var busy lane.Busy
			var effectBusy terminal.EffectBusy
			if errors.As(sendErr, &unsent) || errors.As(sendErr, &busy) || errors.As(sendErr, &effectBusy) {
				state = "pending"
			}
		}
		// A lost client/deadline cannot discard the result of an already typed send.
		final := context.WithoutCancel(ctx)
		if err := s.store.FinishCodegraphGuidance(final, conversation, g.Revision, state); err != nil {
			log.Printf("codegraph guidance receipt: %v", err)
		}
	}
	return suffix, finish, nil
}
func writeGuidanceSendRefusal(w http.ResponseWriter, err error, suffix string) {
	if suffix == "" {
		writeActionRefusal(w, err)
		return
	}
	ref, ok := err.(app.Refusal)
	if !ok {
		writeRefusalAboutWithSource(w, 500, "internal", err.Error(), contract.Refusal{CodegraphSuffix: suffix}, false)
		return
	}
	writeRefusalAboutWithSource(w, actionStatus(ref.Code), ref.Code, ref.Detail, contract.Refusal{CodegraphSuffix: suffix}, refusalDetailFixed(ref))
}
