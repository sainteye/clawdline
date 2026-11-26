package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// In a Bot chat `item add --decision <card>` names the person's answered
// question card instead of reading a run: no run is asked for, and the
// daemon, not the command, decides whether the card authorizes the item.
func TestItemAddInABotChatNamesTheAnsweredQuestionCard(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 201, createdItem })
	var out, errs bytes.Buffer
	code := sessionItem(&out, &errs, b, "add", itemFlags{project: "p1", kind: "feature", title: "Ship it",
		description: "The person agreed on the card.", decision: "card-1", steps: []string{"one", "two"}}, nil, thinConversation, "", envOf(nil))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 1 || seen[0].Method != http.MethodPost || seen[0].EscapedPath != "/v1/work/v2/agent/items" {
		t.Fatalf("requests = %+v", seen)
	}
	var body struct {
		SessionID string            `json:"session_id"`
		Via       map[string]string `json:"via"`
		Assign    any               `json:"assign"`
	}
	if err := json.Unmarshal(seen[0].Body, &body); err != nil || body.SessionID != thinConversation ||
		body.Via["decision"] != "card-1" || body.Via["run"] != "" || len(body.Via) != 1 || body.Assign != nil {
		t.Fatalf("body = %s", seen[0].Body)
	}

	// A card is the whole authority: it takes no run and no assignment, and
	// nothing is asked of the daemon.
	for _, f := range []itemFlags{
		{project: "p1", kind: "feature", title: "x", description: "d", decision: "card-1", run: itemRun},
		{project: "p1", kind: "feature", title: "x", description: "d", decision: "card-1", assign: assignFlags{self: true}},
	} {
		s, b := newStandIn(t, func(r *http.Request) (int, string) { return 201, createdItem })
		errs.Reset()
		if code := sessionItem(&out, &errs, b, "add", f, nil, thinConversation, "", envOf(nil)); code != 2 ||
			!strings.Contains(errs.String(), "--decision") || len(s.requests()) != 0 {
			t.Fatalf("exit %d, %d requests, stderr %q", code, len(s.requests()), errs.String())
		}
	}

	// Without a card the run path is unchanged, and its refusal now names the card.
	s, b = newStandIn(t, func(r *http.Request) (int, string) {
		return 404, `{"error":"no_run","detail":"Nobody has sent this session a message through Clawdline."}`
	})
	errs.Reset()
	if code := sessionItem(&out, &errs, b, "add", itemFlags{project: "p1", kind: "issue", title: "x", description: "d"},
		nil, thinConversation, "", envOf(nil)); code != 1 || len(s.requests()) != 1 ||
		s.requests()[0].Method != http.MethodGet || !strings.Contains(errs.String(), "--decision <question card id>") {
		t.Fatalf("exit %d, requests %+v, stderr %q", code, s.requests(), errs.String())
	}
}
