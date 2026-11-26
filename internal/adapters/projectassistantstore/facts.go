package projectassistantstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A Bot's growing collections are stored one member per row. The Bot row in
// project_assistant_bots keeps only its fixed-size state, so a read of one
// page, one turn or the Bot's settings no longer parses its whole history.
//
// Members are keyed by their own identity: a message by its ID with its
// position in ord, a turn, binding, permission or work link by its map key.
// persist writes only the members that changed. Messages are reassembled in
// ord order; validateAggregate still requires positions 1..n.
const (
	factMessages    = "Messages"
	factTurns       = "Turns"
	factBindings    = "Bindings"
	factPermissions = "Permissions"
	factWorkLinks   = "WorkLinks"
)

var factCollections = []string{factMessages, factTurns, factBindings, factPermissions, factWorkLinks}

const botFactsSchema = `CREATE TABLE IF NOT EXISTS project_assistant_bot_facts (machine TEXT NOT NULL, bot TEXT NOT NULL,
 collection TEXT NOT NULL CHECK(collection IN ('Messages','Turns','Bindings','Permissions','WorkLinks')),
 member TEXT NOT NULL CHECK(member<>''), ord INTEGER NOT NULL, data BLOB NOT NULL, PRIMARY KEY(machine,bot,collection,member))`

const botFactsOrderIndex = `CREATE INDEX IF NOT EXISTS project_assistant_bot_fact_order ON project_assistant_bot_facts(machine,bot,collection,ord)`

// A permission or question card is found by its ID without knowing its Bot.
const botFactsMemberIndex = `CREATE INDEX IF NOT EXISTS project_assistant_bot_fact_member ON project_assistant_bot_facts(machine,collection,member)`

type factKey struct{ collection, member string }

type factRow struct {
	ord  int64
	data []byte
}

func encodeFacts(a aggregate) (map[factKey]factRow, error) {
	out := make(map[factKey]factRow, len(a.Messages)+len(a.Turns)+len(a.Bindings)+len(a.Permissions)+len(a.WorkLinks))
	add := func(collection, member string, ord int64, v any) error {
		if member == "" {
			return failure(pa.Invalid, "fact_member")
		}
		k := factKey{collection, member}
		if _, dup := out[k]; dup {
			return failure(pa.Invalid, "fact_member")
		}
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		out[k] = factRow{ord: ord, data: data}
		return nil
	}
	for _, m := range a.Messages {
		if err := add(factMessages, m.ID, m.Seq, m); err != nil {
			return nil, err
		}
	}
	for k, v := range a.Turns {
		if err := add(factTurns, k, 0, v); err != nil {
			return nil, err
		}
	}
	for k, v := range a.Bindings {
		if err := add(factBindings, k, 0, v); err != nil {
			return nil, err
		}
	}
	for k, v := range a.Permissions {
		if err := add(factPermissions, k, 0, v); err != nil {
			return nil, err
		}
	}
	for k, v := range a.WorkLinks {
		if err := add(factWorkLinks, k, 0, v); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// persistFacts makes the stored members of key equal to next. It reads the
// stored bytes back and writes only the members that differ.
func persistFacts(ctx context.Context, tx *sql.Tx, key pa.BotKey, next map[factKey]factRow, fresh bool) error {
	stored := map[factKey]factRow{}
	if !fresh {
		rows, err := tx.QueryContext(ctx, `SELECT collection,member,ord,data FROM project_assistant_bot_facts WHERE machine=? AND bot=?`, key.MachineID, key.BotID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var k factKey
			var r factRow
			if err = rows.Scan(&k.collection, &k.member, &r.ord, &r.data); err != nil {
				rows.Close()
				return err
			}
			stored[k] = r
		}
		if err = rows.Close(); err != nil {
			return err
		}
	}
	for k := range stored {
		if _, ok := next[k]; ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM project_assistant_bot_facts WHERE machine=? AND bot=? AND collection=? AND member=?`, key.MachineID, key.BotID, k.collection, k.member); err != nil {
			return err
		}
	}
	// A stable order keeps the statements of one commit reproducible.
	keys := make([]factKey, 0, len(next))
	for k := range next {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].collection != keys[j].collection {
			return keys[i].collection < keys[j].collection
		}
		return keys[i].member < keys[j].member
	})
	for _, k := range keys {
		r := next[k]
		if old, ok := stored[k]; ok && old.ord == r.ord && bytes.Equal(old.data, r.data) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_assistant_bot_facts(machine,bot,collection,member,ord,data) VALUES(?,?,?,?,?,?)
 ON CONFLICT(machine,bot,collection,member) DO UPDATE SET ord=excluded.ord,data=excluded.data`, key.MachineID, key.BotID, k.collection, k.member, r.ord, r.data); err != nil {
			return err
		}
	}
	return nil
}

type factReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// loadFacts fills the collections of a from its stored members.
func loadFacts(ctx context.Context, r factReader, key pa.BotKey, a *aggregate) error {
	rows, err := r.QueryContext(ctx, `SELECT collection,member,data FROM project_assistant_bot_facts WHERE machine=? AND bot=? ORDER BY collection,ord,member`, key.MachineID, key.BotID)
	if err != nil {
		return err
	}
	defer rows.Close()
	a.Turns, a.Bindings, a.Permissions, a.WorkLinks = map[string]pa.Turn{}, map[string]pa.ExecutionBinding{}, map[string]pa.PermissionOffer{}, map[string]pa.WorkLink{}
	a.Messages = nil
	for rows.Next() {
		var collection, member string
		var data []byte
		if err = rows.Scan(&collection, &member, &data); err != nil {
			return err
		}
		switch collection {
		case factMessages:
			var m pa.Message
			err = json.Unmarshal(data, &m)
			a.Messages = append(a.Messages, m)
		case factTurns:
			var v pa.Turn
			err = json.Unmarshal(data, &v)
			a.Turns[member] = v
		case factBindings:
			var v pa.ExecutionBinding
			err = json.Unmarshal(data, &v)
			a.Bindings[member] = v
		case factPermissions:
			var v pa.PermissionOffer
			err = json.Unmarshal(data, &v)
			a.Permissions[member] = v
		case factWorkLinks:
			var v pa.WorkLink
			err = json.Unmarshal(data, &v)
			a.WorkLinks[member] = v
		default:
			err = fmt.Errorf("unknown Bot fact collection %q", collection)
		}
		if err != nil {
			return err
		}
	}
	return rows.Err()
}

// inlineCollections names the members a Bot row held before schema 17.
type inlineCollections struct {
	Messages, Turns, Bindings, Permissions, WorkLinks json.RawMessage
}

// decodeAggregate turns a stored Bot row and its members into an aggregate.
// A row that still holds its collections inline is refused: reading it as
// empty would hide its history.
func decodeAggregate(ctx context.Context, r factReader, key pa.BotKey, data []byte) (aggregate, error) {
	a, err := decodeHead(data)
	if err != nil {
		return a, err
	}
	return a, loadFacts(ctx, r, key, &a)
}

// decodeHead decodes the Bot row alone; its collections stay empty.
func decodeHead(data []byte) (aggregate, error) {
	var a aggregate
	if err := json.Unmarshal(data, &a); err != nil {
		return a, err
	}
	var inline inlineCollections
	if err := json.Unmarshal(data, &inline); err != nil {
		return a, err
	}
	for _, raw := range [][]byte{inline.Messages, inline.Turns, inline.Bindings, inline.Permissions, inline.WorkLinks} {
		if len(raw) != 0 && string(raw) != "null" {
			return a, failure(pa.Invalid, "bot_inline_facts")
		}
	}
	// Workers saved by earlier versions can carry their owner's sidebar role.
	if a.BackgroundOwner != nil {
		a.Bot.Lead = false
	}
	return a, nil
}

// factRowsSQL selects one collection's members for the Bot named by the two
// given SQL expressions, as json_each would list them: key, value and type.
// A message's key is its array position, seq-1.
func factRowsSQL(collection, machine, bot string) string {
	key := "member"
	if collection == factMessages {
		key = "ord-1"
	}
	return `(SELECT ` + key + ` key,CAST(data AS TEXT) value,'object' type,ord,member FROM project_assistant_bot_facts WHERE machine=` + machine + ` AND bot=` + bot + ` AND collection='` + collection + `')`
}

// splitBotFacts moves every Bot row's inline collections to member rows.
func splitBotFacts(ctx context.Context, tx *sql.Tx) error {
	for _, c := range factCollections {
		member, ord, kind := "m.key", "0", "object"
		if c == factMessages {
			member, ord, kind = "json_extract(m.value,'$.id')", "json_extract(m.value,'$.seq')", "array"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_assistant_bot_facts(machine,bot,collection,member,ord,data)
 SELECT b.machine,b.bot,'`+c+`',`+member+`,`+ord+`,CAST(m.value AS BLOB)
 FROM project_assistant_bots b,json_each(CAST(b.data AS TEXT),'$.`+c+`') m
 WHERE json_type(CAST(b.data AS TEXT),'$.`+c+`')='`+kind+`'`); err != nil {
			return fmt.Errorf("split Bot %s: %w", c, err)
		}
		// A collection of another JSON type has no members to move, but it
		// is not legal either; keep the row unreadable rather than empty.
		var odd int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_bots
 WHERE coalesce(json_type(CAST(data AS TEXT),'$.`+c+`'),'null') NOT IN ('null','`+kind+`')`).Scan(&odd); err != nil {
			return err
		}
		if odd != 0 {
			return fmt.Errorf("split Bot %s: %d rows hold another JSON type", c, odd)
		}
	}
	// Rows without an inline collection keep their exact bytes.
	_, err := tx.ExecContext(ctx, `UPDATE project_assistant_bots SET data=CAST(json_remove(CAST(data AS TEXT),'$.Messages','$.Turns','$.Bindings','$.Permissions','$.WorkLinks') AS BLOB)
 WHERE json_type(CAST(data AS TEXT),'$.Messages') IS NOT NULL OR json_type(CAST(data AS TEXT),'$.Turns') IS NOT NULL OR json_type(CAST(data AS TEXT),'$.Bindings') IS NOT NULL
 OR json_type(CAST(data AS TEXT),'$.Permissions') IS NOT NULL OR json_type(CAST(data AS TEXT),'$.WorkLinks') IS NOT NULL`)
	return err
}

// factCTEsSQL defines fact_messages, fact_turns, fact_bindings,
// fact_permissions and fact_work_links for one Bot, each listing its members
// with json_each's key, value and type columns.
func factCTEsSQL(machine, bot string) string {
	names := map[string]string{factMessages: "fact_messages", factTurns: "fact_turns", factBindings: "fact_bindings", factPermissions: "fact_permissions", factWorkLinks: "fact_work_links"}
	out := ""
	for _, c := range factCollections {
		out += ", " + names[c] + " AS NOT MATERIALIZED " + factRowsSQL(c, machine, bot)
	}
	return out
}

// factJoinSQL joins one collection's members to the Bot row aliased bot,
// with json_each's key, value and type columns.
func factJoinSQL(collection, alias, bot string) string {
	return `JOIN (SELECT machine fact_machine,bot fact_bot,member key,CAST(data AS TEXT) value,'object' type FROM project_assistant_bot_facts WHERE collection='` +
		collection + `') ` + alias + ` ON ` + alias + `.fact_machine=` + bot + `.machine AND ` + alias + `.fact_bot=` + bot + `.bot`
}

// factValueSQL is one member's stored JSON for the Bot row aliased bot, or NULL.
func factValueSQL(collection, bot, member string) string {
	return `(SELECT CAST(f.data AS TEXT) FROM project_assistant_bot_facts f WHERE f.machine=` + bot + `.machine AND f.bot=` + bot + `.bot AND f.collection='` + collection + `' AND f.member=` + member + `)`
}
