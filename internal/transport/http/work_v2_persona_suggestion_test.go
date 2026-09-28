package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func askPersona(t *testing.T, s *Server, item workV2ItemWire, key string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	path := "/v1/work/v2/items/" + item.ID + "/persona-suggestion"
	body := fmt.Sprintf(`{"expected_version":%d}`, item.Version)
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, path, body, key))
	return rec
}

func TestPersonaSuggestionRunsOnlyAfterTheExplicitPressAndReplays(t *testing.T) {
	s, _, project := sessionItemServer(t)
	item := personItem(t, s, project, "feature", "persona-suggestion-item")
	runs := 0
	s.suggestPersona = func(_ context.Context, request planner.PersonaRequest, provider string) (planner.PersonaSuggestion, error) {
		runs++
		if provider != "codex" || request.Kind != "feature" || request.Title != "Tidy the notes" ||
			request.Description != "Tidy them." || len(request.Candidates) != 42 {
			t.Fatalf("request=%+v provider=%q", request, provider)
		}
		return planner.PersonaSuggestion{Outcome: "recommend", PersonaID: "frontend"}, nil
	}

	// Reading the item — the Board's normal render path — never asks a model.
	read := httptest.NewRecorder()
	s.workV2Route(read, personWorkV2Request(http.MethodGet, "/v1/work/v2/items/"+item.ID, "", ""))
	if read.Code != http.StatusOK || runs != 0 {
		t.Fatalf("read=%d runs=%d", read.Code, runs)
	}
	first := askPersona(t, s, item, "suggest-once")
	again := askPersona(t, s, item, "suggest-once")
	if first.Code != http.StatusOK || again.Code != http.StatusOK || runs != 1 ||
		again.Header().Get("Idempotent-Replayed") != "true" || again.Body.String() != first.Body.String() {
		t.Fatalf("first=%d %s again=%d %s runs=%d", first.Code, first.Body, again.Code, again.Body, runs)
	}
	var answer contract.PersonaSuggestionReply
	if err := json.Unmarshal(first.Body.Bytes(), &answer); err != nil || !answer.OK ||
		answer.Outcome != contract.PersonaSuggestionOutcomeRecommend || answer.PersonaID != "frontend" || answer.Provider != "codex" {
		t.Fatalf("answer=%+v err=%v", answer, err)
	}
}

func TestPersonaSuggestionKeepsAmbiguityAndFailuresTyped(t *testing.T) {
	cases := []struct {
		name    string
		result  planner.PersonaSuggestion
		err     error
		status  int
		code    string
		outcome contract.PersonaSuggestionOutcome
	}{
		{name: "ambiguous", result: planner.PersonaSuggestion{Outcome: "ambiguous"}, status: 200,
			outcome: contract.PersonaSuggestionOutcomeAmbiguous},
		{name: "not installed", err: planner.ErrNoPlanner, status: 501, code: "no_persona_suggester"},
		{name: "out of quota", err: fmt.Errorf("%w: exhausted", planner.ErrOutOfQuota), status: 503,
			code: "persona_suggester_out_of_quota"},
		{name: "unusable", err: errors.New("malformed answer"), status: 502, code: "persona_suggestion_failed"},
		{name: "unknown persona", result: planner.PersonaSuggestion{Outcome: "recommend", PersonaID: "wizard"},
			status: 502, code: "persona_suggestion_failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _, project := sessionItemServer(t)
			item := personItem(t, s, project, "feature", "persona-suggestion-"+strings.ReplaceAll(c.name, " ", "-"))
			s.suggestPersona = func(context.Context, planner.PersonaRequest, string) (planner.PersonaSuggestion, error) {
				return c.result, c.err
			}
			rec := askPersona(t, s, item, "ask-"+c.name)
			if rec.Code != c.status {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			if c.code != "" {
				if got := codeOf(t, rec); got != c.code {
					t.Fatalf("code=%q want %q", got, c.code)
				}
				return
			}
			var answer contract.PersonaSuggestionReply
			if json.Unmarshal(rec.Body.Bytes(), &answer) != nil || answer.Outcome != c.outcome || answer.PersonaID != "" {
				t.Fatalf("answer=%s", rec.Body)
			}
		})
	}
}

func TestPersonaSuggestionFilesAPossiblySpentFailureButNotBusy(t *testing.T) {
	s, _, project := sessionItemServer(t)
	item := personItem(t, s, project, "feature", "persona-suggestion-receipts")
	runs := 0
	s.suggestPersona = func(context.Context, planner.PersonaRequest, string) (planner.PersonaSuggestion, error) {
		runs++
		return planner.PersonaSuggestion{}, errors.New("provider failed after starting")
	}
	first := askPersona(t, s, item, "spent-failure")
	s.suggestPersona = func(context.Context, planner.PersonaRequest, string) (planner.PersonaSuggestion, error) {
		runs++
		return planner.PersonaSuggestion{Outcome: "recommend", PersonaID: "backend"}, nil
	}
	again := askPersona(t, s, item, "spent-failure")
	if first.Code != 502 || again.Code != 502 || runs != 1 || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("failure=%d replay=%d runs=%d replayed=%q", first.Code, again.Code, runs, again.Header().Get("Idempotent-Replayed"))
	}

	s.intentQueued = int(CapacityLimit(capacity.IntentPlannerQueue))
	busy := askPersona(t, s, item, "busy-then-free")
	s.intentQueued = 0
	free := askPersona(t, s, item, "busy-then-free")
	if busy.Code != 429 || codeOf(t, busy) != "busy" || free.Code != 200 || runs != 2 {
		t.Fatalf("busy=%d %s free=%d %s runs=%d", busy.Code, busy.Body, free.Code, free.Body, runs)
	}
}

func TestPersonaSuggestionContextHasARegisteredUTF8SafeBound(t *testing.T) {
	if got := CapacityLimit(capacity.PersonaSuggestionContextBytes); got != personaSuggestionContextLimit {
		t.Fatalf("context limit route=%d register=%d", personaSuggestionContextLimit, got)
	}
	item := work.ItemV2{Kind: work.KindFeature, Title: "多語言角色", Description: strings.Repeat("界", 30_000)}
	request, err := personaSuggestionRequest(item)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(request)
	if len(raw) > personaSuggestionContextLimit || !utf8.ValidString(request.Description) || len(request.Description) >= len(item.Description) {
		t.Fatalf("bytes=%d valid=%v description=%d/%d", len(raw), utf8.ValidString(request.Description), len(request.Description), len(item.Description))
	}
}
