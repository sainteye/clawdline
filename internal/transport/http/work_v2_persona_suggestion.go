package http

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/persona"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// One deliberately requested classifier turn reads the closed persona
// catalog and the part of the Board item that fits beside it.
const personaSuggestionContextLimit = 16 << 10

// workV2PersonaSuggestion is POST /v1/work/v2/items/<id>/persona-suggestion.
// Merely opening, reading or rendering an item cannot enter this path. The
// person's Board-AI consent names the one provider whose account may be used.
func (s *Server) workV2PersonaSuggestion(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "An AI role suggestion is requested with POST.")
		return
	}
	actor, ok := requirePersonWorkV2(w, r)
	if !ok {
		return
	}
	var body contract.PersonaSuggestionRequest
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}

	provider := boardViewer(r).NarrativeProvider
	settings, err := s.boardSettings(r.Context())
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "board_settings_unavailable",
			"The Board AI consent setting could not be read, so no model turn was started.")
		return
	}
	if provider == "" || settings.NarrativeConsent == nil || *settings.NarrativeConsent != provider {
		writeRefusal(w, http.StatusForbidden, "ai_consent_required",
			"Enable AI reading summaries in Settings before sending a Board item's type, title and description to the AI provider.")
		return
	}
	view, err := s.workV2().Item(r.Context(), id)
	if err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	if body.ExpectedVersion != view.Item.Version {
		writeRefusal(w, http.StatusConflict, "version_conflict", "The item changed; reread it before asking AI to classify it.")
		return
	}
	request, err := personaSuggestionRequest(view.Item)
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "persona_catalog_unavailable",
			"The role catalog does not fit inside one bounded classification turn, so no model turn was started.")
		return
	}
	k, ok := workKey(w, r, actor)
	if !ok {
		return
	}
	digest := requestDigest([]byte(r.Method), []byte(routePath(r)), raw)
	// 429 has not started a turn and 501 means there is no installed provider;
	// either may succeed on a later retry. Once a provider ran, its success or
	// failure is filed so a lost response cannot spend a second turn.
	s.receipted(w, r, k, digest, func(status int) bool {
		return status != http.StatusTooManyRequests && status != http.StatusNotImplemented
	}, func(w http.ResponseWriter) {
		s.runPersonaSuggestion(w, r, id, body.ExpectedVersion, provider, request)
	})
}

func (s *Server) runPersonaSuggestion(w http.ResponseWriter, r *http.Request, id string, version int64,
	provider string, request planner.PersonaRequest) {
	if !s.enterIntent() {
		writeRefusal(w, http.StatusTooManyRequests, "busy",
			"This machine is already using both small-turn slots. Try again in a moment.")
		return
	}
	defer s.leaveIntent()
	s.intentRun.Lock()
	defer s.intentRun.Unlock()

	started := time.Now()
	turn, cancel := context.WithTimeout(r.Context(), time.Duration(CapacityLimit(capacity.IntentPlannerSeconds))*time.Second)
	suggestion, err := s.personaSuggester()(turn, request, provider)
	cancel()
	if err == nil && !validPersonaSuggestion(suggestion, request.Candidates) {
		err = errors.New("persona suggester returned a result outside the closed catalog")
	}
	ms := time.Since(started).Milliseconds()
	device := accessOf(r).verdict.Device
	switch {
	case errors.Is(err, planner.ErrNoPlanner):
		log.Printf("audit work.persona_suggestion device=%s provider=%s ms=%d ok=0 why=no_planner", device, provider, ms)
		writeRefusal(w, http.StatusNotImplemented, "no_persona_suggester",
			"The AI provider selected for Board reading is not installed on this machine.")
		return
	case errors.Is(err, planner.ErrOutOfQuota):
		log.Printf("audit work.persona_suggestion device=%s provider=%s ms=%d ok=0 why=out_of_quota", device, provider, ms)
		writeRefusal(w, http.StatusServiceUnavailable, "persona_suggester_out_of_quota",
			"The AI provider selected for Board reading has no usage left. The current role choice was not changed.")
		return
	case err != nil:
		log.Printf("audit work.persona_suggestion device=%s provider=%s ms=%d ok=0 why=failed", device, provider, ms)
		writeRefusal(w, http.StatusBadGateway, "persona_suggestion_failed",
			"The AI provider did not return a role from this machine's catalog. The current role choice was not changed.")
		return
	}

	// The turn classified exactly one version. If the person edited the item
	// while it ran, its answer is not silently applied to the newer words.
	latest, latestErr := s.workV2().Item(r.Context(), id)
	if latestErr != nil || latest.Item.Version != version {
		log.Printf("audit work.persona_suggestion device=%s provider=%s ms=%d ok=0 why=version_changed", device, provider, ms)
		writeRefusal(w, http.StatusConflict, "version_conflict",
			"The item changed while AI was classifying it. The current role choice was not changed.")
		return
	}
	outcome := contract.PersonaSuggestionOutcome(suggestion.Outcome)
	log.Printf("audit work.persona_suggestion device=%s provider=%s ms=%d outcome=%s ok=1", device, provider, ms, outcome)
	writeJSON(w, contract.PersonaSuggestionReply{OK: true, Outcome: outcome,
		PersonaID: suggestion.PersonaID, Provider: provider})
}

func validPersonaSuggestion(suggestion planner.PersonaSuggestion, candidates []planner.PersonaCandidate) bool {
	if suggestion.Outcome == "ambiguous" {
		return suggestion.PersonaID == ""
	}
	if suggestion.Outcome != "recommend" || suggestion.PersonaID == "" {
		return false
	}
	for _, candidate := range candidates {
		if candidate.ID == suggestion.PersonaID {
			return true
		}
	}
	return false
}

func (s *Server) personaSuggester() func(context.Context, planner.PersonaRequest, string) (planner.PersonaSuggestion, error) {
	if s.suggestPersona != nil {
		return s.suggestPersona
	}
	p := planner.New()
	p.Timeout = time.Duration(CapacityLimit(capacity.IntentPlannerSeconds)) * time.Second
	return p.SuggestPersona
}

func personaSuggestionRequest(item work.ItemV2) (planner.PersonaRequest, error) {
	candidates := make([]planner.PersonaCandidate, 0, len(persona.All()))
	for _, p := range persona.All() {
		candidates = append(candidates, planner.PersonaCandidate{ID: p.ID, Name: p.Name.En, Summary: p.Summary.En})
	}
	request := planner.PersonaRequest{Kind: string(item.Kind), Title: item.Title,
		Description: item.Description, Candidates: candidates}
	limit := int(CapacityLimit(capacity.PersonaSuggestionContextBytes))
	for {
		raw, err := json.Marshal(request)
		if err != nil {
			return planner.PersonaRequest{}, err
		}
		if len(raw) <= limit {
			return request, nil
		}
		if request.Description == "" {
			return planner.PersonaRequest{}, errors.New("persona catalog exceeds suggestion context")
		}
		next := len(request.Description) - (len(raw) - limit) - 16
		if next < 0 {
			next = 0
		}
		request.Description = capBytes(request.Description, next)
	}
}
