package projectassistantstore

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func putFact(t *testing.T, db *sql.DB, bot pa.Bot, collection, member string, ord int, data string) {
	t.Helper()
	if _, err := db.Exec(`INSERT OR REPLACE INTO project_assistant_bot_facts(machine,bot,collection,member,ord,data) VALUES(?,?,?,?,?,?)`,
		bot.Key.MachineID, bot.Key.BotID, collection, member, ord, []byte(data)); err != nil {
		t.Fatal(err)
	}
}

func putNotice(t *testing.T, db *sql.DB, bot pa.Bot, card string, version int, turn string) {
	t.Helper()
	key := pa.BotRequestHandoffKey(card, int64(version))
	if _, err := db.Exec(`INSERT INTO project_assistant_receipts(machine,bot,operation,command_key,receipt_id,data) VALUES(?,?,?,?,?,?)`,
		bot.Key.MachineID, bot.Key.BotID, "send", key, "receipt-"+key, []byte(fmt.Sprintf(`{"state":"accepted","resource_id":%q}`, turn))); err != nil {
		t.Fatal(err)
	}
}

func TestAHeldCardIsStalledOnlyOnEvidenceThatNothingIsWorkingOnIt(t *testing.T) {
	db, s, _ := testDB(t)
	holder := seed(t, s, "holder")
	busy := seed(t, s, "busy")
	m := holder.Key.MachineID

	// both: the notice turn ended unknown and the work it dispatched failed.
	putNotice(t, db, holder, "both", 2, "notice-old")
	putNotice(t, db, holder, "both", 3, "notice-both")
	putFact(t, db, holder, factTurns, "notice-old", 1, `{"state":"succeeded"}`)
	putFact(t, db, holder, factTurns, "notice-both", 2, `{"state":"unknown"}`)
	putFact(t, db, holder, factWorkLinks, "w-both", 1, `{"bot_request_id":"both","state":"failed","turn_id":"notice-both","finished_at":"2026-11-27T05:07:30Z"}`)
	// work: a later turn's work failed after the notice turn succeeded.
	putNotice(t, db, holder, "work", 1, "notice-work")
	putFact(t, db, holder, factTurns, "notice-work", 3, `{"state":"succeeded"}`)
	putFact(t, db, holder, factWorkLinks, "w-work-1", 2, `{"bot_request_id":"work","state":"completed","turn_id":"x","finished_at":"2026-11-27T01:00:00Z"}`)
	putFact(t, db, holder, factWorkLinks, "w-work-2", 3, `{"bot_request_id":"work","state":"failed","turn_id":"y","finished_at":"2026-11-27T02:00:00Z"}`)
	// turn: the notice turn failed and dispatched nothing.
	putNotice(t, db, holder, "turn", 1, "notice-turn")
	putFact(t, db, holder, factTurns, "notice-turn", 4, `{"state":"failed"}`)
	// recovered: the latest work completed, so an earlier failure is history.
	putFact(t, db, holder, factWorkLinks, "w-rec-1", 4, `{"bot_request_id":"recovered","state":"failed","finished_at":"2026-11-27T01:00:00Z"}`)
	putFact(t, db, holder, factWorkLinks, "w-rec-2", 5, `{"bot_request_id":"recovered","state":"completed","finished_at":"2026-11-27T03:00:00Z"}`)
	// running: one of its works is still executing.
	putFact(t, db, holder, factWorkLinks, "w-run-1", 6, `{"bot_request_id":"running","state":"failed","finished_at":"2026-11-27T03:00:00Z"}`)
	putFact(t, db, holder, factWorkLinks, "w-run-2", 7, `{"bot_request_id":"running","state":"executing","started_at":"2026-11-27T02:00:00Z"}`)
	// quiet: no notice turn and no work is no evidence of anything.
	// held: its holder has a queued turn, so it may still act.
	putFact(t, db, busy, factTurns, "queued", 1, `{"state":"queued"}`)
	putFact(t, db, busy, factWorkLinks, "w-held", 1, `{"bot_request_id":"held","state":"failed"}`)
	// restart: queued notices wait behind a turn a restart interrupted, which
	// only the person's next message releases, so they are not in flight.
	stuck := seed(t, s, "stuck")
	putNotice(t, db, stuck, "restart", 1, "interrupted")
	putFact(t, db, stuck, factTurns, "interrupted", 1, `{"state":"unknown","failure":{"code":"execution_effect_unknown","field":"restart"}}`)
	putFact(t, db, stuck, factTurns, "report", 2, `{"state":"queued","origin":"host_notice"}`)
	putFact(t, db, stuck, factWorkLinks, "w-restart", 1, `{"bot_request_id":"restart","state":"failed","turn_id":"interrupted"}`)

	holds := []BotRequestHold{{"both", holder.Key.BotID}, {"work", holder.Key.BotID}, {"turn", holder.Key.BotID},
		{"recovered", holder.Key.BotID}, {"running", holder.Key.BotID}, {"quiet", holder.Key.BotID}, {"held", busy.Key.BotID}, {"restart", stuck.Key.BotID}}
	got, err := s.BotRequestStalls(testContext, m, holds)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"both": BotRequestStallTurnAndWork, "work": BotRequestStallWorkFailed, "turn": BotRequestStallTurnUnsettled, "restart": BotRequestStallTurnAndWork}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stalls = %v, want %v", got, want)
	}
	if other, err := s.BotRequestStalls(testContext, "another-machine", holds); err != nil || len(other) != 0 {
		t.Fatalf("another machine's cards = %v, %v; want none", other, err)
	}
}
