package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	br "github.com/sainteye/clawdline/internal/domain/botrequest"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A request and a task that name no Bot request card keep the hash and the
// stored shape they had before cards existed (docs/bot-request-cards.md §8.1).
func TestBotRequestIDLeavesOldHashesAndRecordsAlone(t *testing.T) {
	_, _, _, q, _ := assistantBrokerFixture(t)
	body, _ := json.Marshal(struct {
		Command              pa.Command
		Fence                pa.Fence
		TurnID, Brief        string
		Claims               []string
		Title                string            `json:",omitempty"`
		TargetProject        pa.ProjectScope   `json:"target_project,omitzero"`
		Persona              string            `json:",omitempty"`
		BoardItemID          string            `json:"board_item_id,omitempty"`
		ContinueOnCompletion bool              `json:"continue_on_completion,omitempty"`
		Choice               pa.DispatchChoice `json:"choice,omitzero"`
	}{q.Command, q.Actor.Fence, q.TurnID, q.Brief, q.Claims, q.Title, q.TargetProject, q.Persona, q.BoardItemID, q.ContinueOnCompletion, q.Choice})
	sum := sha256.Sum256(body)
	if got := AssistantRequestHash(q); got != hex.EncodeToString(sum[:]) {
		t.Fatal("a request naming no card changed the hash of receipts written before cards")
	}
	q.BotRequestID = br.CardID("m", "bot", "c", "msg")
	if AssistantRequestHash(q) == hex.EncodeToString(sum[:]) {
		t.Fatal("the card a request names is not part of its hash")
	}
	raw, _ := json.Marshal(Record{ID: "t", WorkID: "w"})
	if strings.Contains(string(raw), "bot_request_id") {
		t.Fatalf("a task naming no card stores the field: %s", raw)
	}
	link, _ := json.Marshal(pa.WorkLink{ID: "t"})
	if strings.Contains(string(link), "bot_request_id") {
		t.Fatalf("a work link naming no card carries the field: %s", link)
	}
}

// Only the Bot dispatch door attaches a task to a card: a task file that
// names one is refused by name rather than attached or ignored.
func TestATaskFileCannotNameABotRequestCard(t *testing.T) {
	b, _, _, _, _ := assistantBrokerFixture(t)
	protocol := Protocol
	claims := []string{}
	d := draft{Claims: &claims, Protocol: &protocol, TaskID: "a1111111-1111-4111-8111-111111111111", Kind: "implementation", Assistant: "claude",
		ProjectDir: t.TempDir(), Title: "T", Instructions: "Do it", BotRequestID: json.RawMessage(`"` + br.CardID("m", "bot", "c", "msg") + `"`)}
	if _, err := b.admit(d.TaskID, d, false, false); err == nil || !strings.Contains(err.Error(), "bot_request_id") {
		t.Fatalf("a task file naming a card: %v", err)
	}
}
