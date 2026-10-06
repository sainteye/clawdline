package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/work"
)

type humanInterventionWire struct {
	ID                 string                         `json:"id"`
	SourceConversation string                         `json:"source_conversation"`
	SourceLabel        string                         `json:"source_label"`
	TargetConversation string                         `json:"target_conversation"`
	TargetSession      string                         `json:"target_session"`
	Kind               string                         `json:"kind"`
	Title              string                         `json:"title"`
	Summary            string                         `json:"summary"`
	Action             string                         `json:"action"`
	Reason             string                         `json:"reason"`
	Detail             string                         `json:"detail,omitempty"`
	Options            []work.HumanInterventionOption `json:"options"`
	DocumentURL        string                         `json:"document_url,omitempty"`
	CreatedAt          int64                          `json:"created_at"`
	ReadAt             *int64                         `json:"read_at"`
	ResolvedAt         *int64                         `json:"resolved_at"`
	Resolution         string                         `json:"resolution,omitempty"`
	Version            int64                          `json:"version"`
}

func humanInterventionOf(v work.HumanIntervention) humanInterventionWire {
	out := humanInterventionWire{ID: v.ID, SourceConversation: v.SourceConversation, SourceLabel: v.SourceLabel,
		TargetConversation: v.TargetConversation, TargetSession: v.TargetSession, Kind: v.Kind, Title: v.Title,
		Summary: v.Summary, Action: v.Action, Reason: v.Reason, Detail: v.Detail, Options: v.Options,
		DocumentURL: v.DocumentURL, CreatedAt: v.CreatedAt.Unix(), Resolution: v.Resolution, Version: v.Version}
	if out.Options == nil {
		out.Options = []work.HumanInterventionOption{}
	}
	if v.ReadAt != nil {
		at := v.ReadAt.Unix()
		out.ReadAt = &at
	}
	if v.ResolvedAt != nil {
		at := v.ResolvedAt.Unix()
		out.ResolvedAt = &at
	}
	return out
}

func (s *Server) humanInterventions() *app.HumanInterventions {
	return app.NewHumanInterventions(s.store)
}

// workV2HumanInterventions reads a target conversation and records only a
// person's explicit read or resolution. A draft sent to a composer is not an
// acknowledgement or authorization.
func (s *Server) workV2HumanInterventions(w http.ResponseWriter, r *http.Request, parts []string) {
	actor := ""
	if r.Method == http.MethodGet {
		verdict := accessOf(r).verdict
		if machineAuthed(r) || !verdict.Allowed || !verdict.Caps.Has(auth.Read) {
			writeRefusal(w, http.StatusForbidden, "forbidden", "A person with read access must open this Session.")
			return
		}
	} else {
		var ok bool
		actor, ok = requirePersonWorkV2(w, r)
		if !ok {
			return
		}
	}
	if len(parts) < 1 {
		writeNoSuchRoute(w, r)
		return
	}
	target := decodeSegment(parts[0])
	if !strings.HasPrefix(target, sessionTodoConversationPrefix) || !workID(strings.TrimPrefix(target, sessionTodoConversationPrefix)) {
		writeRefusal(w, http.StatusBadRequest, "conversation_id_malformed", "Name the target as conversation:<lowercase UUID>.")
		return
	}
	conversation := strings.TrimPrefix(target, sessionTodoConversationPrefix)
	if len(parts) == 1 && r.Method == http.MethodGet {
		rows, pruned, err := s.humanInterventions().List(r.Context(), conversation)
		if err != nil {
			s.writeWorkV2Error(w, err)
			return
		}
		out := make([]humanInterventionWire, 0, len(rows))
		for _, row := range rows {
			out = append(out, humanInterventionOf(row))
		}
		writeJSON(w, map[string]any{"ok": true, "rows": out, "pruned_resolved": pruned})
		return
	}
	if len(parts) == 3 && workID(parts[1]) && r.Method == http.MethodPost {
		action := parts[2]
		var body struct {
			ExpectedVersion int64  `json:"expected_version"`
			Resolution      string `json:"resolution"`
		}
		raw, ok := readWorkV2Body(w, r, &body)
		if !ok {
			return
		}
		k, ok := s.beginWorkV2Write(w, r, actor, raw)
		if !ok {
			return
		}
		answer := func(v work.HumanIntervention) []byte {
			b, _ := json.Marshal(map[string]any{"ok": true, "note": humanInterventionOf(v)})
			return b
		}
		_, response, err := s.humanInterventions().Update(r.Context(), parts[1], conversation, action, body.Resolution, body.ExpectedVersion, k, answer)
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(response)
		return
	}
	writeNoSuchRoute(w, r)
}

func (s *Server) agentCreateHumanIntervention(w http.ResponseWriter, r *http.Request) {
	var body app.CreateHumanIntervention
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, body.SourceConversation, raw)
	if !ok {
		return
	}
	release := func() { _ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k) }
	if s.broker == nil {
		release()
		writeRefusal(w, http.StatusServiceUnavailable, "session_unresolved", "The live Session registry is unavailable.")
		return
	}
	source, err := s.broker.LiveRootSession(r.Context(), body.SourceConversation)
	if err != nil {
		release()
		s.writeHumanInterventionIdentityError(w, err)
		return
	}
	target, err := s.actions().Find(r.Context(), body.TargetSession)
	if err != nil || target.ConversationID == "" {
		release()
		writeRefusal(w, http.StatusConflict, "target_session_unavailable", "The target Session has no unique live conversation.")
		return
	}
	identity, err := s.broker.WhoAmI(r.Context(), target.ConversationID)
	if err != nil || identity.TerminalID != target.ID {
		release()
		writeRefusal(w, http.StatusConflict, "target_session_ambiguous", "The target conversation does not resolve to that Session.")
		return
	}
	body.TargetConversation = target.ConversationID
	body.SourceLabel = source.Label
	if body.SourceLabel == "" {
		body.SourceLabel = source.ID
	}
	if body.DocumentURL != "" {
		line, exists := cloudLines.Load(s.cfg.Dir)
		if exists {
			if holder, ok := line.(CloudPairingLine); ok {
				if pairing := holder.Pairing(); pairing != nil {
					body.TargetMachine = pairing.MachineID
				}
			}
		}
	}
	answer := func(v work.HumanIntervention) []byte {
		b, _ := json.Marshal(map[string]any{"ok": true, "note": humanInterventionOf(v)})
		return b
	}
	_, response, err := s.humanInterventions().Create(r.Context(), body, k, answer)
	if err != nil {
		release()
		s.writeWorkV2Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(response)
}

func (s *Server) writeHumanInterventionIdentityError(w http.ResponseWriter, err error) {
	var refusal orchestrator.Refusal
	if errors.As(err, &refusal) {
		writeRefusal(w, refusal.Status, refusal.Code, refusal.Message)
		return
	}
	writeRefusal(w, http.StatusServiceUnavailable, "session_unresolved", "The source Session could not be verified.")
}
