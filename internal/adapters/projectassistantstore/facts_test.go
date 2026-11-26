package projectassistantstore

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// insertStoredAggregate writes a as persist would, without its validation,
// so a test can store a deliberately partial Bot.
func insertStoredAggregate(t *testing.T, db *sql.DB, a aggregate) {
	t.Helper()
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := encodeFacts(a)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(testContext, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	version := max(a.Bot.Version, 1)
	if _, err = tx.ExecContext(testContext, `INSERT INTO project_assistant_bots(machine,bot,version,data) VALUES(?,?,?,?)`, a.Bot.Key.MachineID, a.Bot.Key.BotID, version, data); err != nil {
		t.Fatal(err)
	}
	if err = persistFacts(testContext, tx, a.Bot.Key, facts, true); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// splitExpectation is what schema 17 must make of each Bot row: the row
// without its five collections, and one member row for each element.
type splitExpectation struct {
	heads   map[string]any
	members map[string]any
}

func expectSplit(t *testing.T, db *sql.DB) splitExpectation {
	t.Helper()
	out := splitExpectation{heads: map[string]any{}, members: map[string]any{}}
	rows, err := db.Query(`SELECT machine,bot,CAST(data AS TEXT) FROM project_assistant_bots`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var machine, bot, raw string
		if err = rows.Scan(&machine, &bot, &raw); err != nil {
			t.Fatal(err)
		}
		var head map[string]json.RawMessage
		if err = json.Unmarshal([]byte(raw), &head); err != nil {
			t.Fatal(err)
		}
		for _, c := range factCollections {
			value := head[c]
			delete(head, c)
			if c == factMessages {
				var list []map[string]any
				_ = json.Unmarshal(value, &list)
				for _, m := range list {
					out.members[machine+"/"+bot+"/"+c+"/"+m["id"].(string)+"@"+jsonNumber(m["seq"])] = m
				}
				continue
			}
			var set map[string]any
			_ = json.Unmarshal(value, &set)
			for k, v := range set {
				out.members[machine+"/"+bot+"/"+c+"/"+k+"@0"] = v
			}
		}
		b, _ := json.Marshal(head)
		var v any
		_ = json.Unmarshal(b, &v)
		out.heads[machine+"/"+bot] = v
	}
	return out
}

func jsonNumber(v any) string { b, _ := json.Marshal(v); return string(b) }

func requireSplit(t *testing.T, db *sql.DB, want splitExpectation) {
	t.Helper()
	got := splitExpectation{heads: map[string]any{}, members: map[string]any{}}
	rows, err := db.Query(`SELECT machine,bot,CAST(data AS TEXT) FROM project_assistant_bots`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var machine, bot, raw string
		var v any
		if err = rows.Scan(&machine, &bot, &raw); err != nil || json.Unmarshal([]byte(raw), &v) != nil {
			t.Fatal(err)
		}
		got.heads[machine+"/"+bot] = v
	}
	rows.Close()
	rows, err = db.Query(`SELECT machine,bot,collection,member,ord,CAST(data AS TEXT) FROM project_assistant_bot_facts`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var machine, bot, collection, member, raw string
		var ord int64
		var v any
		if err = rows.Scan(&machine, &bot, &collection, &member, &ord, &raw); err != nil || json.Unmarshal([]byte(raw), &v) != nil {
			t.Fatal(err)
		}
		got.members[machine+"/"+bot+"/"+collection+"/"+member+"@"+jsonNumber(ord)] = v
	}
	rows.Close()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema 17 split = %#v\nwant %#v", got, want)
	}
}

// inlineLayout rewrites the database as schema 16 stored it: every Bot's
// collections inside its row and no member rows.
func inlineLayout(t *testing.T, db *sql.DB) {
	t.Helper()
	member := func(c string) string {
		if c == factMessages {
			return `coalesce((SELECT json_group_array(json(x.v)) FROM (SELECT CAST(f.data AS TEXT) v FROM project_assistant_bot_facts f WHERE f.machine=b.machine AND f.bot=b.bot AND f.collection='Messages' ORDER BY f.ord) x),json('[]'))`
		}
		return `coalesce((SELECT json_group_object(f.member,json(CAST(f.data AS TEXT))) FROM project_assistant_bot_facts f WHERE f.machine=b.machine AND f.bot=b.bot AND f.collection='` + c + `'),json('{}'))`
	}
	set := `json_set(CAST(b.data AS TEXT)`
	for _, c := range factCollections {
		set += `,'$.` + c + `',` + member(c)
	}
	if _, err := db.Exec(`UPDATE project_assistant_bots AS b SET data=CAST(` + set + `) AS BLOB); DELETE FROM project_assistant_bot_facts; UPDATE project_assistant_schema SET version=16`); err != nil {
		t.Fatal(err)
	}
}

func TestSchema17MovesInlineCollectionsToMemberRows(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "inline")
	populate(t, s, b, pa.EffectUnknown)
	before, err := s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Messages) == 0 || len(before.Turns) == 0 || len(before.Permissions) == 0 || len(before.WorkLinks) == 0 {
		t.Fatalf("fixture lacks a collection: %+v", before)
	}
	inlineLayout(t, db)
	if _, err = s.Bot(testContext, b.Key); pa.Code(err) != pa.Invalid {
		t.Fatalf("an inline row was read before migration: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err = (Installer{}).Install(testContext, db); err != nil {
			t.Fatal(err)
		}
	}
	after, err := s.Export(testContext, b.Key)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("migration changed the Bot's history: %v", err)
	}
	var inline int
	if err = db.QueryRow(`SELECT count(*) FROM project_assistant_bots WHERE json_type(CAST(data AS TEXT),'$.Messages') IS NOT NULL`).Scan(&inline); err != nil || inline != 0 {
		t.Fatalf("inline rows after migration = %d, %v", inline, err)
	}
}

func TestSchema17RefusesARepeatedMemberAndKeepsTheRow(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "repeated")
	populate(t, s, b, pa.EffectUnknown)
	inlineLayout(t, db)
	if _, err := db.Exec(`UPDATE project_assistant_bots SET data=json_insert(CAST(data AS TEXT),'$.Messages[#]',json_extract(CAST(data AS TEXT),'$.Messages[0]'))`); err != nil {
		t.Fatal(err)
	}
	before := databaseFacts(t, db)
	if err := (Installer{}).Install(testContext, db); err == nil {
		t.Fatal("a repeated message ID was migrated")
	}
	if !reflect.DeepEqual(before, databaseFacts(t, db)) {
		t.Fatal("a refused migration changed the database")
	}
}
