package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func decisionItemBody(t *testing.T, conversation string, via map[string]string, extra map[string]any) string {
	t.Helper()
	body := map[string]any{"session_id": conversation, "via": via, "kind": "feature",
		"title": "Shared event stream follow-ups", "description": "Atlas takes these.",
		"steps": []string{"closed EventSource reuse", "memory retry rereads memory"}}
	for k, v := range extra {
		body[k] = v
	}
	b, _ := json.Marshal(body)
	return string(b)
}

// A Bot chat has no run; the person's answer to its own question card, found
// by the daemon, is what lets it create a Board item. A card the daemon does
// not admit writes nothing, and a Session's run path is untouched.
func TestABotCreatesAnItemOnThePersonsAnswerToItsQuestionCard(t *testing.T) {
	s, p, _ := sessionItemServer(t)
	s.cfg.Dir = t.TempDir()
	answered := time.Now().Add(-time.Minute).Truncate(time.Second)
	var asked []string
	decisionAuthorityByServer.Store(s, decisionAuthorityFunc(func(_ context.Context, conversation, offerID string) (pa.DecisionAuthority, error) {
		asked = append(asked, conversation+"/"+offerID)
		switch {
		case offerID == "card-answered" && conversation == "bot-native":
			return pa.DecisionAuthority{OfferID: offerID, BotID: "xavia", ConversationID: "chat-1", NativeSessionID: conversation,
				CanonicalID: p.s.CWD, Question: "Open it?", Answer: "Yes, open it for Atlas", AnsweredAt: answered}, nil
		case offerID == "card-answered":
			return pa.DecisionAuthority{}, &pa.DecisionRefusal{Code: pa.DecisionOtherConversation}
		case offerID == "card-pending":
			return pa.DecisionAuthority{}, &pa.DecisionRefusal{Code: pa.DecisionUnanswered}
		}
		return pa.DecisionAuthority{}, &pa.DecisionRefusal{Code: pa.DecisionUnknown}
	}))
	t.Cleanup(func() { decisionAuthorityByServer.Delete(s) })
	post := func(body, key string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", body, key))
		return rec
	}
	openItems := func() int {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, httptest.NewRequest(http.MethodGet, "/v1/work/v2/items?status=open", nil))
		return strings.Count(rec.Body.String(), `"title":"Shared event stream follow-ups"`)
	}

	// Off, the default: the card is ignored exactly as before the setting
	// existed, so even an answered card creates nothing.
	if rec := post(decisionItemBody(t, "bot-native", map[string]string{"decision": "card-answered"}, nil), "off"); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), `"error":"run_unknown"`) {
		t.Fatalf("an answered card with the setting off: %d %s", rec.Code, rec.Body)
	}
	if len(asked) != 0 || openItems() != 0 {
		t.Fatalf("the setting off read cards %v or created items", asked)
	}
	if err := os.WriteFile(s.settingsFile().Path(), []byte(`{"bot_decision_items":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// (b) Without an answered card of this Bot chat, nothing is created.
	for _, c := range []struct {
		conversation, card, code string
		status                   int
	}{
		{"bot-native", "card-pending", pa.DecisionUnanswered, http.StatusForbidden},
		{"bot-native", "card-made-up", pa.DecisionUnknown, http.StatusForbidden},
		{"other-bot-native", "card-answered", pa.DecisionOtherConversation, http.StatusForbidden},
	} {
		rec := post(decisionItemBody(t, c.conversation, map[string]string{"decision": c.card}, nil), "refused-"+c.card+c.conversation)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), `"error":"`+c.code+`"`) {
			t.Fatalf("%s from %s: %d %s", c.card, c.conversation, rec.Code, rec.Body)
		}
	}
	if rec := post(decisionItemBody(t, "bot-native", map[string]string{"decision": "card-answered", "run": "10000000-0000-4000-8000-000000000009"}, nil), "both"); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "via_ambiguous") {
		t.Fatalf("a card and a run together: %d %s", rec.Code, rec.Body)
	}
	if rec := post(decisionItemBody(t, "bot-native", map[string]string{"decision": "card-answered"}, map[string]any{"assign": map[string]string{"mode": "self"}}), "self"); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "invalid_assignment") {
		t.Fatalf("a card with an assignment: %d %s", rec.Code, rec.Body)
	}
	if rec := post(decisionItemBody(t, "bot-native", map[string]string{}, nil), "no-authority"); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "run_unknown") {
		t.Fatalf("neither card nor run: %d %s", rec.Code, rec.Body)
	}
	if n := openItems(); n != 0 {
		t.Fatalf("a refused request created %d items", n)
	}

	// (a) The answered card creates the item, unassigned, with the card as its source.
	rec := post(decisionItemBody(t, "bot-native", map[string]string{"decision": "card-answered"}, nil), "created")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created struct {
		Assigned        bool           `json:"assigned"`
		AssignmentState string         `json:"assignment_state"`
		Item            workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	it := created.Item
	if created.Assigned || created.AssignmentState != "not_requested" || it.OwnerSession != nil ||
		it.CreatedBy != work.ActorViaBotDecision+"card-answered" || len(it.Steps) != 2 {
		t.Fatalf("item: %s", rec.Body)
	}
	if v := it.CreatedVia; v == nil || v.DecisionID != "card-answered" || v.BotID != "xavia" || v.Run != "card-answered" ||
		v.SessionID != "bot-native" || v.At != answered.Unix() || v.Excerpt != "Yes, open it for Atlas" {
		t.Fatalf("created_via: %+v", it.CreatedVia)
	}
	if replay := post(decisionItemBody(t, "bot-native", map[string]string{"decision": "card-answered"}, nil), "created"); replay.Code != http.StatusCreated || replay.Body.String() != rec.Body.String() {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body)
	}
	if n := openItems(); n != 1 {
		t.Fatalf("open items after one create and its replay = %d", n)
	}

	// (c) A Session's run path never consults a question card.
	asked = nil
	run := issueTestRun(t, s, p, "Make a Board item for the notes")
	if rec := post(sessionItemBody(t, p.s.ConversationID, run, "", "issue"), "session-run"); rec.Code != http.StatusCreated ||
		!strings.Contains(rec.Body.String(), `"created_by":"`+work.ActorViaSession+run+`"`) || strings.Contains(rec.Body.String(), "decision_id") {
		t.Fatalf("session create: %d %s", rec.Code, rec.Body)
	}
	if len(asked) != 0 {
		t.Fatalf("the Session path read question cards: %v", asked)
	}
}
