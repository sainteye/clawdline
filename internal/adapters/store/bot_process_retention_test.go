package store

import (
	"fmt"
	"testing"
	"time"
)

// The tables carry the Bot store's columns (projectassistantstore/schema.go);
// this package cannot import it, so the rules are checked on the same shape.
func seedBotProcessBacklog(t *testing.T, st *Store) {
	t.Helper()
	for _, statement := range []string{
		`CREATE TABLE project_assistant_bots (machine TEXT NOT NULL, bot TEXT NOT NULL, version INTEGER NOT NULL, data BLOB NOT NULL, PRIMARY KEY(machine,bot))`,
		`CREATE TABLE project_assistant_events (seq INTEGER PRIMARY KEY AUTOINCREMENT, machine TEXT NOT NULL, bot TEXT NOT NULL, event_id TEXT NOT NULL, data BLOB NOT NULL, UNIQUE(machine,event_id))`,
		`CREATE TABLE project_assistant_roster_changes (seq INTEGER PRIMARY KEY AUTOINCREMENT, machine TEXT NOT NULL, bot TEXT NOT NULL, project TEXT NOT NULL, conversation TEXT NOT NULL, generation INTEGER NOT NULL, binding TEXT NOT NULL, removed INTEGER NOT NULL)`,
		`CREATE TABLE project_assistant_receipts (machine TEXT NOT NULL, bot TEXT NOT NULL, operation TEXT NOT NULL, command_key TEXT NOT NULL, receipt_id TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY(machine,bot,operation,command_key), UNIQUE(machine,bot,receipt_id))`,
		// "busy" is running turn "live"; "idle" has no active turn.
		`INSERT INTO project_assistant_bots VALUES('m','busy',1,'{"Conversation":{"active_turn_id":"live"}}'),('m','idle',1,'{"Conversation":{"active_turn_id":""}}')`,
	} {
		if _, err := st.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	event := func(bot, id, kind, turn, category string) {
		data := fmt.Sprintf(`{"kind":%q,"trace":{"turn_id":%q,"category":%q,"text":"x"}}`, kind, turn, category)
		if _, err := st.db.Exec(`INSERT INTO project_assistant_events(machine,bot,event_id,data) VALUES('m',?,?,?)`, bot, id, data); err != nil {
			t.Fatal(err)
		}
	}
	receipt := func(bot, key string) {
		if _, err := st.db.Exec(`INSERT INTO project_assistant_receipts VALUES('m',?,'send',?,?,'{}')`, bot, key, bot+key); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3*BotProcessPruneBatchLimit/2; i++ {
		event("idle", fmt.Sprintf("old-%d", i), "trace_updated", "done", "agent_message_chunk")
		receipt("idle", fmt.Sprintf("runtime-event-done-%d", i))
	}
	event("busy", "old-busy", "trace_updated", "earlier", "tool_call")
	event("busy", "live-1", "trace_updated", "live", "agent_message_chunk")
	event("idle", "delegated", "trace_updated", "done", "bot_request")
	event("idle", "reply", "message_created", "", "")
	receipt("busy", "runtime-event-live-1")
	receipt("busy", "runtime-event-earlier-1")
	receipt("busy", "runtime-event-live2-1") // a different turn whose ID starts like the live one
	receipt("idle", "person-send")
	for i := 0; i < 4; i++ {
		for _, bot := range []string{"busy", "idle"} {
			if _, err := st.db.Exec(`INSERT INTO project_assistant_roster_changes(machine,bot,project,conversation,generation,binding,removed) VALUES('m',?,'{}','c',1,'b',0)`, bot); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The machine's newest event is a process trace of a finished turn: it
	// stays so that no reader's frontier moves back.
	event("idle", "newest", "trace_updated", "done", "tool_call")
}

func TestOpenClearsFinishedBotTurnsProcess(t *testing.T) {
	st := openEventStore(t)
	seedBotProcessBacklog(t, st)
	out, err := clearBotProcessBacklog(st.db, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	ids := func(query string) []string {
		t.Helper()
		rows, err := st.db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			got = append(got, s)
		}
		return got
	}
	if got := fmt.Sprint(ids(`SELECT event_id FROM project_assistant_events ORDER BY seq`)); got != "[live-1 delegated reply newest]" {
		t.Fatalf("events left: %s", got)
	}
	if got := fmt.Sprint(ids(`SELECT command_key FROM project_assistant_receipts ORDER BY command_key`)); got != "[person-send runtime-event-live-1]" {
		t.Fatalf("receipts left: %s", got)
	}
	if got := fmt.Sprint(ids(`SELECT bot||':'||seq FROM project_assistant_roster_changes ORDER BY seq`)); got != "[busy:7 idle:8]" {
		t.Fatalf("roster rows left: %s", got)
	}
	if out.Deleted["process traces"] != int64(3*BotProcessPruneBatchLimit/2)+1 || out.Deleted["runtime event receipts"] != int64(3*BotProcessPruneBatchLimit/2)+2 || out.Deleted["roster journal rows"] != 6 {
		t.Fatalf("deleted: %+v", out.Deleted)
	}
	// A second Open finds nothing left to do.
	again, err := clearBotProcessBacklog(st.db, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	for what, n := range again.Deleted {
		if n != 0 {
			t.Fatalf("second cleanup deleted %d %s", n, what)
		}
	}
}

func TestOpenWithoutBotTablesHasNoProcessToClear(t *testing.T) {
	st := openEventStore(t)
	out, err := clearBotProcessBacklog(st.db, 0, true)
	if err != nil || len(out.Deleted) != 0 || out.Vacuumed {
		t.Fatalf("%+v %v", out, err)
	}
}

// Open stops at its budget and leaves the rest, and VACUUM, for later: the
// maintenance clock deletes the remainder and a later Open gives space back.
func TestOpenLeavesWhatItsBudgetCannotClearForLater(t *testing.T) {
	st := openEventStore(t)
	seedBotProcessBacklog(t, st)
	out, err := clearBotProcessBacklog(st.db, time.Nanosecond, true)
	if err != nil || out.Finished || out.Vacuumed {
		t.Fatalf("an exhausted budget: %+v %v", out, err)
	}
	st.maintainBotProcessBacklog()
	var traces int
	if err := st.db.QueryRow(`SELECT count(*) FROM project_assistant_events WHERE event_id LIKE 'old-%'`).Scan(&traces); err != nil || traces != 0 {
		t.Fatalf("finished-turn traces after maintenance = %d, %v", traces, err)
	}
	again, err := clearBotProcessBacklog(st.db, BotProcessOpenBudgetLimit, true)
	if err != nil || !again.Finished {
		t.Fatalf("the next Open: %+v %v", again, err)
	}
}
