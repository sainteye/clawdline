package store

import (
	"database/sql"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/snippet"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// The retired spellings are confined to this one-way upgrade. Live readers,
// writers, routes and catalogs accept only the Playbook vocabulary.
func migratePlaybookDefinitions(tx *sql.Tx) error {
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='workflow_sources')`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		if _, err := tx.Exec(`INSERT INTO playbook_sources (snippet_id, source_json) SELECT snippet_id, source_json FROM workflow_sources`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE workflow_sources`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE snippets SET scope='playbook' WHERE scope='workflow'`); err != nil {
		return err
	}
	// Keep the retry key and digest as a tombstone rather than replaying an
	// obsolete wire shape or allowing the same create to execute again.
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='request_receipts')`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		if _, err := tx.Exec(`UPDATE request_receipts SET state='expired', status=0, body=NULL
		WHERE scope='snippets' AND state='complete' AND EXISTS (
		 SELECT 1 FROM json_tree(CASE WHEN json_valid(CAST(request_receipts.body AS TEXT))
		 THEN CAST(request_receipts.body AS TEXT) ELSE '{}' END)
		 WHERE key='scope' AND value='workflow')`); err != nil {
			return err
		}
	}
	// Recognize only the untouched shipped example; edited messages remain
	// authored data. Keep its identity and edit timestamps during the upgrade.
	oldBody := strings.Replace(snippet.DefaultZeroReviewBody, "target process.", "target workflow.", 1)
	_, err := tx.Exec(`UPDATE snippets SET body=? WHERE id=? AND scope='playbook' AND project='' AND title=? AND body=?`,
		snippet.DefaultZeroReviewBody, snippet.DefaultZeroReviewID, snippet.DefaultZeroReviewTitle, oldBody)
	return err
}

func migratePlaybookRuns(db *sql.DB) error {
	const oldMarker = "\n\n[Clawdline-Workflow "
	const marker = "\n\n[Clawdline-Playbook "
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id, text FROM session_direct_todos WHERE instr(text, ?) > 0`, oldMarker)
	if err != nil {
		return err
	}
	type edit struct{ id, text string }
	var edits []edit
	for rows.Next() {
		var id, text string
		if err := rows.Scan(&id, &text); err != nil {
			rows.Close()
			return err
		}
		at := strings.LastIndex(text, oldMarker)
		next := text[:at] + marker + text[at+len(oldMarker):]
		if _, ok := work.PlaybookRunTitle(next); ok {
			edits = append(edits, edit{id, next})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, e := range edits {
		// Metadata conversion preserves the message, receipts, completion and
		// version. In particular, it never sends or reopens a to-do.
		if _, err := tx.Exec(`UPDATE session_direct_todos SET text=? WHERE id=?`, e.text, e.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
