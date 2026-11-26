package projectassistanttest

import (
	"context"
	"database/sql"
)

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// MoveInlineCollections lets a test keep writing a Bot's messages, turns,
// bindings, permissions or work links inline with json_set. It replaces each
// inline collection's stored member rows with the inline value and removes it
// from the Bot row, as the store's schema 17 migration does.
func MoveInlineCollections(ctx context.Context, db execer, machine, bot string) error {
	for _, c := range []string{"Messages", "Turns", "Bindings", "Permissions", "WorkLinks"} {
		member, ord := "m.key", "0"
		if c == "Messages" {
			member, ord = "json_extract(m.value,'$.id')", "json_extract(m.value,'$.seq')"
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM project_assistant_bot_facts WHERE machine=?1 AND bot=?2 AND collection=?3
 AND EXISTS(SELECT 1 FROM project_assistant_bots b WHERE b.machine=?1 AND b.bot=?2 AND json_type(CAST(b.data AS TEXT),'$.'||?3) IS NOT NULL)`, machine, bot, c); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO project_assistant_bot_facts(machine,bot,collection,member,ord,data)
 SELECT b.machine,b.bot,'`+c+`',`+member+`,`+ord+`,CAST(m.value AS BLOB)
 FROM project_assistant_bots b,json_each(CAST(b.data AS TEXT),'$.`+c+`') m WHERE b.machine=? AND b.bot=?`, machine, bot); err != nil {
			return err
		}
	}
	_, err := db.ExecContext(ctx, `UPDATE project_assistant_bots SET data=CAST(json_remove(CAST(data AS TEXT),'$.Messages','$.Turns','$.Bindings','$.Permissions','$.WorkLinks') AS BLOB) WHERE machine=? AND bot=?`, machine, bot)
	return err
}
