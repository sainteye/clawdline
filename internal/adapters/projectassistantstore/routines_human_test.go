package projectassistantstore

import (
	"fmt"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"strings"
	"testing"
)

func TestHumanRoutinePagesBindScopeAuthorityAndIncarnation(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "human-pages")
	for i := 0; i < 3; i++ {
		d := routineFixture(b)
		d.ID = fmt.Sprint("routine-", i)
		if _, err := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, d.ID), ExpectedVersion: int64(i + 1), Routine: &d, Receipt: pa.Receipt{ID: d.ID, State: pa.EffectDelivered, ResourceID: d.ID}}); err != nil {
			t.Fatal(err)
		}
	}
	views := newTestViews(t, db)
	q := app.RoutineQuery{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID, PageSize: 1, AuthorizationBinding: "reader/grant/source"}
	page, err := views.ReadRoutines(testContext, q)
	if err != nil || len(page.Definitions) != 1 || page.NextCursor == "" {
		t.Fatal(page, err)
	}
	ids := []string{page.Definitions[0].ID}
	q.Cursor = page.NextCursor
	for {
		page, err = views.ReadRoutines(testContext, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range page.Definitions {
			ids = append(ids, d.ID)
		}
		if page.NextCursor == "" {
			break
		}
		q.Cursor = page.NextCursor
	}
	if strings.Join(ids, ",") != "routine-2,routine-1,routine-0" {
		t.Fatal("keyset lost/repeated rows", ids)
	}
	for _, change := range []func(*app.RoutineQuery){func(q *app.RoutineQuery) { q.AuthorizationBinding = "other-reader" }, func(q *app.RoutineQuery) { q.RoutineID = "routine-1" }, func(q *app.RoutineQuery) { q.Cursor += "x" }} {
		bad := q
		change(&bad)
		_, err = views.ReadRoutines(testContext, bad)
		requireCode(t, err, pa.CursorExpired)
	}
	restarted := newTestViews(t, db)
	_, err = restarted.ReadRoutines(testContext, q)
	requireCode(t, err, pa.CursorExpired)
	bad := q
	bad.Project.CanonicalID = "other-project"
	_, err = views.ReadRoutines(testContext, bad)
	requireCode(t, err, pa.ScopeDenied)
	bad = q
	bad.ConversationID = "other-chat"
	_, err = views.ReadRoutines(testContext, bad)
	requireCode(t, err, pa.NotFound)
	q.Cursor = ""
	if _, err = db.Exec(`UPDATE project_assistant_routines SET data=? WHERE machine=? AND bot=? AND id=?`, strings.Repeat("x", cloudops.ReadPlaintextBytesLimit+1), b.Key.MachineID, b.Key.BotID, "routine-2"); err != nil {
		t.Fatal(err)
	}
	_, err = views.ReadRoutines(testContext, q)
	requireCode(t, err, pa.CapacityFull)
	if _, err = db.Exec(`DROP TABLE project_assistant_routines`); err != nil {
		t.Fatal(err)
	}
	q.Cursor = ""
	if _, err = views.ReadRoutines(testContext, q); err == nil {
		t.Fatal("unknown storage became an empty list")
	}
}

func TestHumanRoutinePageByteBudgetRetainsAnHonestPrefix(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "human-page-bytes")
	for i := 0; i < pa.MaxPageRows; i++ {
		d := routineFixture(b)
		d.ID = fmt.Sprintf("large-%03d", i)
		d.Instruction = strings.Repeat("\x01", pa.MaxRoutineInstructionBytes)
		if _, err := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, d.ID), ExpectedVersion: int64(i + 1), Routine: &d, Receipt: pa.Receipt{ID: d.ID, State: pa.EffectDelivered, ResourceID: d.ID}}); err != nil {
			t.Fatal(err)
		}
	}
	views := newTestViews(t, db)
	q := app.RoutineQuery{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID, PageSize: pa.MaxPageRows, AuthorizationBinding: "reader/grant"}
	seen := map[string]bool{}
	pages := 0
	for {
		page, err := views.ReadRoutines(testContext, q)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if pages == 1 && (len(page.Definitions) == 0 || len(page.Definitions) >= pa.MaxPageRows || page.NextCursor == "") {
			t.Fatal("byte budget lost its honest prefix", len(page.Definitions))
		}
		for _, d := range page.Definitions {
			if seen[d.ID] || len(d.Instruction) != pa.MaxRoutineInstructionBytes {
				t.Fatal("page repeated/truncated a definition")
			}
			seen[d.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		q.Cursor = page.NextCursor
	}
	if len(seen) != pa.MaxPageRows {
		t.Fatal("byte pages lost rows", len(seen))
	}
}
