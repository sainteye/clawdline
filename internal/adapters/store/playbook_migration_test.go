package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/snippet"
)

func TestPlaybookUpgradePreservesDefinitionsSourcesAndRunState(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	source := snippet.SourceRef{Kind: "skill", FileID: strings.Repeat("a", 32), PlaceID: "place", Scope: "project", Name: "Review", Version: "one"}
	raw, _ := json.Marshal(source)
	const id = "00000000-0000-4000-8000-000000000041"
	for _, statement := range []string{
		`INSERT INTO snippets (id,title,body,scope,project,position,created_at,updated_at) VALUES ('` + id + `','Saved title','Saved message','workflow','/project',700,11,22)`,
		`ALTER TABLE playbook_sources RENAME TO workflow_sources`,
	} {
		if _, err := st.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.db.Exec(`INSERT INTO workflow_sources VALUES (?,?)`, id, string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE snippets SET scope='workflow', body=replace(body,'target process.','target workflow.') WHERE id=?`, snippet.DefaultZeroReviewID); err != nil {
		t.Fatal(err)
	}
	for key, body := range map[string]string{
		"old-create":       `{"id":"saved","scope":"workflow"}`,
		"old-order":        `{"ok":true,"snippets":[{"id":"saved","scope":"workflow"}]}`,
		"ordinary-snippet": `{"id":"ordinary","scope":"global"}`,
	} {
		if _, err := st.db.Exec(`INSERT INTO request_receipts (scope,actor,key,digest,state,status,body,created_at) VALUES ('snippets','agent',?,'same-request','complete',200,?,1)`, key, body); err != nil {
			t.Fatal(err)
		}
	}
	message := "Keep this authored example: [Clawdline-Workflow example]"
	oldText := message + "\n\n[Clawdline-Workflow " + id + " Saved%20title]"
	if _, err := st.db.Exec(`INSERT INTO session_direct_todos (id,session_id,text,created_by,created_at,sent_at,read_at,completed_at,completed_by,version) VALUES ('run','session',?,'agent',10,20,30,40,'person',7)`, oldText); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO session_direct_todos (id,session_id,text,created_by,created_at) VALUES ('ordinary','session',?,'agent',10)`, "An example\n\n[Clawdline-Workflow invalid name]"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		st, err = Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := st.Snippets(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows {
			if row.ID == snippet.DefaultZeroReviewID && !snippet.IsDefaultZeroReview(row) {
				t.Fatal("untouched starter lost localization")
			}
			if row.ID != id {
				continue
			}
			found = true
			if row.Scope != snippet.Playbook || row.Project != "/project" || row.Title != "Saved title" || row.Body != "Saved message" || row.Position != 700 || row.CreatedAt != 11 || row.UpdatedAt != 22 || row.Source == nil || *row.Source != source {
				t.Fatalf("upgrade lost definition: %+v", row)
			}
		}
		if !found {
			t.Fatal("saved definition disappeared")
		}
		for _, key := range []string{"old-create", "old-order", "ordinary-snippet"} {
			claim, err := st.LookupReceipt(context.Background(), ReceiptKey{Scope: "snippets", Actor: "agent", Key: key})
			if err != nil {
				t.Fatal(err)
			}
			want := ReceiptExpired
			if key == "ordinary-snippet" {
				want = ReceiptReplay
			}
			if claim.Outcome != want {
				t.Fatalf("receipt %s: %s, want %s", key, claim.Outcome, want)
			}
		}
		var text, completedBy string
		var sent, read, completed, version int
		if err := st.db.QueryRow(`SELECT text,sent_at,read_at,completed_at,completed_by,version FROM session_direct_todos WHERE id='run'`).Scan(&text, &sent, &read, &completed, &completedBy, &version); err != nil {
			t.Fatal(err)
		}
		if text != message+"\n\n[Clawdline-Playbook "+id+" Saved%20title]" || sent != 20 || read != 30 || completed != 40 || completedBy != "person" || version != 7 {
			t.Fatalf("run state changed: %q %d %d %d %s %d", text, sent, read, completed, completedBy, version)
		}
		if err := st.db.QueryRow(`SELECT text FROM session_direct_todos WHERE id='ordinary'`).Scan(&text); err != nil {
			t.Fatal(err)
		}
		if text != "An example\n\n[Clawdline-Workflow invalid name]" {
			t.Fatal("ordinary authored text was changed")
		}
		var oldTables, oldRows int
		st.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='workflow_sources'`).Scan(&oldTables)
		st.db.QueryRow(`SELECT COUNT(*) FROM snippets WHERE scope='workflow'`).Scan(&oldRows)
		if oldTables != 0 || oldRows != 0 {
			t.Fatal("retired definition names remain")
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlaybookDefinitionUpgradeRollsBackOnConflictingSources(t *testing.T) {
	st, _ := openSnippetStore(t)
	for _, statement := range []string{
		`INSERT INTO snippets (id,title,body,scope,project,position,created_at,updated_at) VALUES ('saved','Title','Message','workflow','',100,1,1)`,
		`CREATE TABLE workflow_sources (snippet_id TEXT PRIMARY KEY,source_json TEXT NOT NULL)`,
		`INSERT INTO workflow_sources VALUES ('saved','old source')`,
		`INSERT INTO playbook_sources VALUES ('saved','new source')`,
	} {
		if _, err := st.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := openSnippets(st.db); err == nil {
		t.Fatal("conflicting attribution was silently overwritten")
	}
	var scope, source string
	if err := st.db.QueryRow(`SELECT scope FROM snippets WHERE id='saved'`).Scan(&scope); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT source_json FROM workflow_sources WHERE snippet_id='saved'`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if scope != "workflow" || source != "old source" {
		t.Fatal("failed upgrade changed existing data")
	}
}
