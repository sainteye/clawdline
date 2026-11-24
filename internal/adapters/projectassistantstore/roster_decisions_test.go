package projectassistantstore

import (
	"reflect"
	"strings"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func rosterDecision(fence pa.Fence, id, question string, anchor int64, expires time.Time) pa.PermissionOffer {
	return pa.PermissionOffer{
		ID: id, NativeRequestID: id, Kind: pa.DecisionOfferKind, Fence: fence,
		Decision: &pa.DecisionRequest{Question: question}, AnchorMessageSeq: anchor,
		State: pa.PermissionPending, ExpiresAt: expires,
		Options: []pa.PermissionOption{{ID: pa.DecisionOptionPrefix + "1", Label: "Yes", Kind: pa.AllowOnce}},
	}
}

// The icon, the list row and the chat all count what still needs the person.
// A question no turn owns is counted and shown; a question the person already
// wrote past, or one that expired, is no longer something to handle.
func TestRosterCountsAndShowsAnOpenDecisionWithoutATurn(t *testing.T) {
	e, _, current := viewFixture(t, 2, 0, false)
	now := time.Now()
	e.Permissions = append(e.Permissions,
		rosterDecision(current, "decision-open", "Which option should I use?", 2, now.Add(time.Hour)),
		// Asked before the person's message m001 (seq 1): writing past a
		// question does not answer it, so it still counts.
		rosterDecision(current, "decision-passed", "Old question?", 0, now.Add(time.Hour)),
		rosterDecision(current, "decision-expired", "Expired question?", 2, now.Add(-time.Minute)),
	)
	_, s, views, _ := restoreViews(t, e)
	q := rosterQuery()
	got := rosterRead(t, views, q)
	if len(got.Frame.Changes) != 1 || got.Frame.Changes[0].Summary == nil {
		t.Fatalf("no roster row: %+v", got.Frame)
	}
	row := *got.Frame.Changes[0].Summary
	if row.PendingPermissions != 2 || row.Status != "needsInput" || row.Activity != "Asks: Which option should I use?" {
		t.Fatalf("roster row hides or miscounts the open decision: pending=%d status=%q activity=%q", row.PendingPermissions, row.Status, row.Activity)
	}
	list, err := s.ListForReader(testContext, pa.ListBots{MachineID: "machine", PageSize: 1}, q.ReaderID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(list.Items[0], row) {
		t.Fatalf("list and roster disagree\nlist   %+v\nroster %+v", list.Items[0], row)
	}
}

// Both unread counts skip the same messages: quiet outputs are not news.
func TestRosterAndListUnreadSkipTheSameMessages(t *testing.T) {
	e, _, _ := viewFixture(t, 4, 0, false)
	e.Messages[2].Output = &pa.Output{ID: "out-1", ArtifactID: "artifact-1", Name: "Notes", Bytes: 1, SHA256: strings.Repeat("ab", 32), Quiet: true}
	e.Events[2].Message.Output = e.Messages[2].Output
	_, s, views, _ := restoreViews(t, e)
	for _, reader := range []string{"desktop-reader", "cloud-account"} {
		q := rosterQuery()
		q.ReaderID = reader
		row := *rosterRead(t, views, q).Frame.Changes[0].Summary
		list, err := s.ListForReader(testContext, pa.ListBots{MachineID: "machine", PageSize: 1}, reader)
		if err != nil {
			t.Fatal(err)
		}
		if row.UnreadCount != 2 || list.Items[0].UnreadCount != row.UnreadCount || row.ReadCursor.ReaderID != reader || list.Items[0].ReadCursor.ReaderID != reader {
			t.Fatalf("reader %s: roster unread %d, list unread %d (want 2)", reader, row.UnreadCount, list.Items[0].UnreadCount)
		}
	}
}

// A message that may not be about the questions leaves them open; a reply
// quoting the Bot message that asked them closes them.
func TestUnrelatedMessageKeepsQuestionsAndAQuotedReplyClosesThem(t *testing.T) {
	_, s, _ := testDB(t)
	b, p := ledgerBot(t, s), newLedgerPeer()
	r := ledgerEngine(t, s, b, p)
	input := recoverySubmission(t, s, b, "ask")
	input.Message.Text = "Plan the release"
	if _, err := r.Submit(testContext, input); err != nil {
		t.Fatal(err)
	}
	q := ledgerReceive(t, p)
	var offers []pa.PermissionOffer
	for _, question := range []string{"Ship today?", "Tag it 2.1?"} {
		offer, err := r.AskPerson(testContext, q.Turn.Fence, q.Turn.ID, pa.AskPersonRequest{Question: question, SuggestedReplies: []string{"Yes"}})
		if err != nil {
			t.Fatal(err)
		}
		offers = append(offers, offer)
	}
	p.results <- app.PromptResult{FinalText: "Two questions for you"}
	recoveryAwait(t, func() bool { return r.Usage().ActiveActors == 0 })
	ledgerSend(t, s, b, r, p, "unrelated", "Also, what is the weather")
	for _, offer := range offers {
		if got, err := s.Permission(testContext, b.Key, offer.ID); err != nil || got.State != pa.PermissionPending {
			t.Fatalf("an unrelated message closed %q: %s %v", offer.Decision.Question, got.State, err)
		}
	}
	if got := listSummary(t, s, b.Key); got.PendingPermissions != 2 || got.Status != "needsInput" {
		t.Fatalf("open questions hidden after an unrelated message: %+v", got)
	}
	data, err := s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	var asked string
	for _, m := range data.Messages {
		if m.Kind != pa.UserMessage && m.Text == "Two questions for you" {
			asked = m.ID
		}
	}
	if asked == "" {
		t.Fatal("no Bot message carried the questions")
	}
	reply := recoverySubmission(t, s, b, "quoted")
	reply.Message.Text, reply.Message.QuoteMessageID = "Yes to both", asked
	if _, err := r.Submit(testContext, reply); err != nil {
		t.Fatal(err)
	}
	ledgerReceive(t, p)
	p.results <- app.PromptResult{FinalText: "Thanks"}
	recoveryAwait(t, func() bool { return r.Usage().ActiveActors == 0 })
	for _, offer := range offers {
		if got, err := s.Permission(testContext, b.Key, offer.ID); err != nil || got.State != pa.PermissionCancelled {
			t.Fatalf("a quoted reply left %q %s %v", offer.Decision.Question, got.State, err)
		}
	}
	if got := listSummary(t, s, b.Key); got.PendingPermissions != 0 || got.Status == "needsInput" {
		t.Fatalf("answered questions still counted: %+v", got)
	}
}

// The person's next message after the Bot's only newest question answers it,
// in storage, so every count and the chat agree.
func TestPersonMessageClosesTheQuestionItWrotePast(t *testing.T) {
	_, s, _ := testDB(t)
	b, p := ledgerBot(t, s), newLedgerPeer()
	r := ledgerEngine(t, s, b, p)
	input := recoverySubmission(t, s, b, "ask")
	input.Message.Text = "Plan the release"
	if _, err := r.Submit(testContext, input); err != nil {
		t.Fatal(err)
	}
	q := ledgerReceive(t, p)
	offer, err := r.AskPerson(testContext, q.Turn.Fence, q.Turn.ID, pa.AskPersonRequest{Question: "Ship today?", SuggestedReplies: []string{"Yes"}})
	if err != nil {
		t.Fatal(err)
	}
	p.results <- app.PromptResult{FinalText: "Waiting for your answer"}
	recoveryAwait(t, func() bool { return r.Usage().ActiveActors == 0 })
	if got := listSummary(t, s, b.Key); got.PendingPermissions != 1 || got.Status != "needsInput" {
		t.Fatalf("open decision not shown: %+v", got)
	}
	ledgerSend(t, s, b, r, p, "reply", "Not today, wait for QA")
	closed, err := s.Permission(testContext, b.Key, offer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if closed.State != pa.PermissionCancelled {
		t.Fatalf("question stayed %s after the person wrote past it", closed.State)
	}
	if got := listSummary(t, s, b.Key); got.PendingPermissions != 0 || got.Status == "needsInput" {
		t.Fatalf("closed decision still counted: %+v", got)
	}
}
