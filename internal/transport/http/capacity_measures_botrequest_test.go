package http

import (
	"context"
	"testing"
	"time"

	br "github.com/sainteye/clawdline/internal/domain/botrequest"
)

// The botrequest rows read the store's current values: the first page of
// cards, the live receipts, and the longest merged_into walk.
func TestBotRequestCapacityReadsStoredValues(t *testing.T) {
	s := capacityServer(t)
	ctx := context.Background()
	now := time.Now()
	var ids []string
	for _, m := range []string{"m1", "m2", "m3"} {
		auth := br.Authority{Bot: "lead", ConversationID: "c", TurnID: "t-" + m, MessageID: m}
		c, err := br.NewCard(br.CardID("machine", "lead", "c", m), "p", "/p", "lead", auth, br.CreateInput{Title: m, Checklist: []string{"a"}}, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.store.CreateBotRequest(ctx, c); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	db := s.store.ProjectAssistantDatabase()
	// m1 merged into m2, m2 merged into m3: two hops from m1.
	for _, pair := range [][2]string{{ids[0], ids[1]}, {ids[1], ids[2]}} {
		if _, err := db.Exec(`UPDATE bot_request_cards SET state = 'merged', merged_into = ? WHERE id = ?`, pair[1], pair[0]); err != nil {
			t.Fatal(err)
		}
	}
	for i, at := range []time.Time{now, now.Add(-time.Hour), now.Add(-48 * time.Hour)} {
		if _, err := db.Exec(`INSERT INTO bot_request_receipts (actor, key, card_id, at) VALUES ('bot:lead', ?, ?, ?)`,
			"k"+string(rune('0'+i)), ids[2], at.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	measures := s.capacityMeasures()
	for name, want := range map[string]int64{"botrequest.page_rows": 3, "botrequest.merge_hops": 2, "botrequest.receipts": 2} {
		r := measures[name]()
		if !r.Known || r.Used != want {
			t.Errorf("%s = %+v, want used %d", name, r, want)
		}
	}
}
