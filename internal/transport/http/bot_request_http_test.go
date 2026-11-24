package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/auth"
	br "github.com/sainteye/clawdline/internal/domain/botrequest"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func botRequestServer(t *testing.T) (*Server, br.Card) {
	t.Helper()
	p := &pane{s: session.Session{ID: "pane-br", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
		ConversationID: "10000000-0000-4000-8000-0000000000b1", State: session.StateIdle}}
	s := paneServer(t, p)
	auth := br.Authority{Bot: "lead", ConversationID: "conv", TurnID: "turn", MessageID: "msg"}
	c, err := br.NewCard(br.CardID("machine", "lead", "conv", "msg"), "project", "/project", "lead", auth,
		br.CreateInput{Title: "weekly post", Kind: "publishing"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c, _, err = s.store.CreateBotRequest(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

func botRequestDo(s *Server, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.botRequestRoute(rec, req)
	return rec
}

func TestBotRequestRoutesReadAndThePersonActs(t *testing.T) {
	s, c := botRequestServer(t)
	base := "/v1/bot-request/cards/" + c.ID

	rec := botRequestDo(s, personWorkV2Request(http.MethodGet, "/v1/bot-request/cards?state=open", "", ""))
	var page BotRequestPage
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &page) != nil || len(page.Cards) != 1 {
		t.Fatalf("list %d %s", rec.Code, rec.Body)
	}
	if v := page.Cards[0]; v.Done != 0 || v.Total != 3 || v.ProgressState != br.ProgressInProgress || v.UpdatedAt == "" {
		t.Fatalf("row %+v", v)
	}

	// The person completes with open entries: allowed for the person.
	body := `{"expected_version":` + strconv.FormatInt(c.Version, 10) + `,"reason":"published by hand"}`
	rec = botRequestDo(s, personWorkV2Request(http.MethodPost, base+"/complete", body, "done-1"))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"state":"done"`) {
		t.Fatalf("complete %d %s", rec.Code, rec.Body)
	}
	// A retry with the same key answers the card and writes nothing.
	rec = botRequestDo(s, personWorkV2Request(http.MethodPost, base+"/complete", body, "done-1"))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"replayed":true`) {
		t.Fatalf("retry %d %s", rec.Code, rec.Body)
	}
	effects, err := s.store.Effects(context.Background(), store.EffectBotRequestDone, br.NotifyKey(c.ID, 1))
	if err != nil || len(effects) != 1 {
		t.Fatalf("notices %d %v", len(effects), err)
	}
	// A stale version is a typed conflict that names the current version.
	rec = botRequestDo(s, personWorkV2Request(http.MethodPost, base+"/reopen", body, "reopen-stale"))
	var ref botRequestRefusal
	if rec.Code != http.StatusConflict || json.Unmarshal(rec.Body.Bytes(), &ref) != nil || ref.Error != br.CodeVersionConflict || ref.Version != c.Version+1 {
		t.Fatalf("stale %d %s", rec.Code, rec.Body)
	}
	rec = botRequestDo(s, personWorkV2Request(http.MethodPost, base+"/reopen", `{"expected_version":`+strconv.FormatInt(ref.Version, 10)+`}`, "reopen-1"))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"cycle":2`) {
		t.Fatalf("reopen %d %s", rec.Code, rec.Body)
	}

	rec = botRequestDo(s, personWorkV2Request(http.MethodGet, base, "", ""))
	var view BotRequestView
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &view) != nil || len(view.Events) != 3 {
		t.Fatalf("read %d %s", rec.Code, rec.Body)
	}
}

func TestBotRequestRoutesRefuseWhatIsNotThePersons(t *testing.T) {
	s, c := botRequestServer(t)
	base := "/v1/bot-request/cards/" + c.ID
	body := `{"expected_version":1}`

	session := httptest.NewRequest(http.MethodPost, base+"/cancel", strings.NewReader(body))
	session.Header.Set("Idempotency-Key", "k")
	session = session.WithContext(context.WithValue(session.Context(), accessKey{}, access{machine: true,
		verdict: auth.Verdict{Allowed: true, Caps: auth.NewCaps(auth.Read, auth.Send)}}))
	if rec := botRequestDo(s, session); rec.Code != http.StatusForbidden {
		t.Fatalf("a Session wrote a card: %d %s", rec.Code, rec.Body)
	}
	reader := httptest.NewRequest(http.MethodPost, base+"/cancel", strings.NewReader(body))
	reader.Header.Set("Idempotency-Key", "k")
	reader = reader.WithContext(context.WithValue(reader.Context(), accessKey{}, access{verdict: auth.Verdict{
		Allowed: true, Device: "viewer", Caps: auth.NewCaps(auth.Read)}}))
	if rec := botRequestDo(s, reader); rec.Code != http.StatusForbidden {
		t.Fatalf("a read-only device wrote a card: %d", rec.Code)
	}
	if rec := botRequestDo(s, personWorkV2Request(http.MethodPost, base+"/cancel", body, "")); rec.Code != http.StatusBadRequest {
		t.Fatalf("no idempotency key: %d", rec.Code)
	}
	if rec := botRequestDo(s, personWorkV2Request(http.MethodGet, "/v1/bot-request/cards/missing", "", "")); rec.Code != http.StatusNotFound {
		t.Fatalf("missing card: %d %s", rec.Code, rec.Body)
	}
	// The person may not record a verdict; there is no such person route,
	// and a check with a bad state is a typed refusal.
	rec := botRequestDo(s, personWorkV2Request(http.MethodPost, base+"/check", `{"expected_version":1,"entries":[{"label":"draft","state":"maybe"}]}`, "bad"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), br.CodeInvalid) {
		t.Fatalf("bad check %d %s", rec.Code, rec.Body)
	}
	rec = botRequestDo(s, personWorkV2Request(http.MethodPost, base+"/verdict", body, "v"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("verdict route %d", rec.Code)
	}
	// A handoff to a Bot outside the chain is the person's to make.
	rec = botRequestDo(s, personWorkV2Request(http.MethodPost, base+"/handoff", `{"expected_version":1,"to_bot":"reviewer","role":"reviewer","note":"please read"}`, "h"))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"bot":"reviewer"`) {
		t.Fatalf("handoff %d %s", rec.Code, rec.Body)
	}
}
