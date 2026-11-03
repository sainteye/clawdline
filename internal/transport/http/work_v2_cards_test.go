package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestBoardCardsWithholdDetailsWithoutChangingSearchOrOneItemReads(t *testing.T) {
	s, pane, v := workV2AssignmentServer(t, session.StateWorking)
	owned, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", pane.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	description := strings.Repeat("界", 240) + " search-at-the-end"
	v, err = s.workV2().Edit(context.Background(), v.Item.ID, app.EditWorkV2{
		Description: &description, Actor: "local", Person: true, ExpectedVersion: owned.Item.Version,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	document := strings.TrimSpace(strings.Repeat("A complete document stays on the detail read.\n", 250))
	v, err = s.workV2().AddDocument(context.Background(), v.Item.ID, app.AddDocumentV2{
		ExpectedVersion: v.Item.Version, SessionID: pane.s.ConversationID, Role: "spec", Title: "Spec", Body: document,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.workV2().AddStep(context.Background(), v.Item.ID, app.AddStepV2{
		ExpectedVersion: v.Item.Version, SessionID: pane.s.ConversationID, Title: "Keep the complete step", Position: 0,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	read := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRecorder()
		s.workV2Route(r, httptest.NewRequest(http.MethodGet, path, nil))
		if r.Code != http.StatusOK {
			t.Fatalf("read %s: %d %s", path, r.Code, r.Body)
		}
		return r
	}
	legacy := read("/v1/work/v2/items?status=open")
	preview := read("/v1/work/v2/items?status=open&view=cards-v1&q=search-at-the-end")
	var cards struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &cards); err != nil {
		t.Fatal(err)
	}
	if len(cards.Rows) != 1 {
		t.Fatalf("full-source search returned %d cards", len(cards.Rows))
	}
	card := cards.Rows[0]
	if card["summary"] != true || card["id"] != v.Item.ID || card["title"] != v.Item.Title || card["acceptance_present"] != true {
		t.Fatalf("card lost identity or gate presentation: %v", card)
	}
	text, _ := card["description"].(string)
	if !utf8.ValidString(text) || len([]rune(text)) != 200 {
		t.Fatalf("description preview is not bounded UTF-8: %q", text)
	}
	for _, field := range []string{"documents", "steps", "images", "assignments", "events", "landings", "acceptance_criteria"} {
		if _, exists := card[field]; exists {
			t.Errorf("card transferred detail field %s", field)
		}
	}
	if preview.Body.Len()*2 >= legacy.Body.Len() {
		t.Fatalf("card still carries most detail bytes: %d / %d", preview.Body.Len(), legacy.Body.Len())
	}
	full := read("/v1/work/v2/items/" + v.Item.ID)
	var detail struct {
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(full.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Item.Description != description || len(detail.Item.Documents) != 1 || detail.Item.Documents[0].Body != document || len(detail.Item.Steps) != 1 {
		t.Fatal("one-item read lost retained detail")
	}
	if !strings.Contains(legacy.Body.String(), "acceptance_criteria") || !strings.Contains(legacy.Body.String(), "Keep the complete step") {
		t.Fatal("legacy list changed before the viewer opted in")
	}
}
