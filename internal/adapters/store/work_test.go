package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// The board's tables hold their invariants themselves: an item is in one
// place at a time, a move is never rewritten or removed, a closed board item
// says why, and a writer holding a stale copy writes nothing.
func TestTheBoardTablesRefuseWhatTheRulesDoNotHave(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Unix(1_789_700_000, 0)
	it := work.Item{ID: "0b0a0000-0000-4000-8000-000000000001", Project: "/p", Title: "t", CreatedAt: at,
		CreatedBy: "user", Place: work.PlaceBacklog, State: work.ItemPlanned, PlacedAt: at}
	move := func(trigger string) WorkMove {
		return MoveOf(it.ID, work.Change{From: work.PlaceNone, To: work.PlaceBacklog, State: work.ItemPlanned,
			Trigger: trigger, Actor: "user"}, at)
	}
	if err := s.WriteWork(ctx, func(tx *WorkTx) error { return tx.Create(it, move("created"), 0) }); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) error {
		_, err := s.db.ExecContext(ctx, q, args...)
		return err
	}
	// In two places at once.
	if err := exec(`INSERT INTO board_items (work_id, state, owner, commitment, cycle_since, evidence_at)
		VALUES (?, 'active', 'user', 'assigned', 0, 0)`, it.ID); err == nil || !strings.Contains(err.Error(), "work_in_two_places") {
		t.Fatalf("a second place answered %v", err)
	}
	// A move rewritten or removed.
	if err := exec(`UPDATE moves SET actor = 'somebody else'`); err == nil || !strings.Contains(err.Error(), "moves_append_only") {
		t.Fatalf("an UPDATE of a move answered %v", err)
	}
	if err := exec(`DELETE FROM moves`); err == nil || !strings.Contains(err.Error(), "moves_append_only") {
		t.Fatalf("a DELETE of a move answered %v", err)
	}
	// A closed board item without a reason, and a dropped one that says landed.
	if err := exec(`DELETE FROM backlog WHERE work_id = ?`, it.ID); err != nil {
		t.Fatal(err)
	}
	if err := exec(`INSERT INTO board_items (work_id, state, owner, commitment, cycle_since, evidence_at)
		VALUES (?, 'done', 'user', 'assigned', 0, 0)`, it.ID); err == nil {
		t.Fatal("a done item with no reason was stored")
	}
	if err := exec(`INSERT INTO board_items (work_id, state, owner, commitment, cycle_since, evidence_at, closed_reason, closed_at)
		VALUES (?, 'dropped', 'user', 'assigned', 0, 0, 'landed', 1)`, it.ID); err == nil {
		t.Fatal("a dropped item that says landed was stored")
	}
	if err := exec(`INSERT INTO board_items (work_id, state, owner, commitment, cycle_since, evidence_at)
		VALUES (?, 'active', '', 'assigned', 0, 0)`, it.ID); err == nil {
		t.Fatal("a board item with no owner was stored")
	}
	if err := exec(`INSERT INTO backlog (work_id, state) VALUES (?, 'planned')`, it.ID); err != nil {
		t.Fatal(err)
	}
	// A stale writer.
	held, err := s.WorkItem(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := held
	next.Rank = 2
	if err := s.WriteWork(ctx, func(tx *WorkTx) error { return tx.Put(held, next, move("ranked")) }); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteWork(ctx, func(tx *WorkTx) error { return tx.Put(held, next, move("ranked")) }); !errors.Is(err, ErrConflict) {
		t.Fatalf("a stale write answered %v", err)
	}
	now, _ := s.WorkItem(ctx, it.ID)
	if now.Version != 1 || now.Rank != 2 {
		t.Fatalf("after the stale write: %+v", now)
	}
	if m, _ := s.WorkMoves(ctx, it.ID, 0, 10); len(m) != 2 {
		t.Fatalf("moves %d", len(m))
	}
}

// Which tasks are bound to an item is answered from the index on the
// broker's rows, and a row nobody can decode as JSON is not bound to
// anything rather than refused.
func TestBoundTasksAreFoundByTheirWorkID(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Unix(1_789_700_000, 0)
	w := "0b0a0000-0000-4000-8000-000000000002"
	for i, rec := range []string{`{"task_id":"a","work_id":"` + w + `"}`, `{"task_id":"b"}`, `not json`} {
		id := string(rune('a' + i))
		if _, err := s.CreateBrokerTask(ctx, BrokerRow{ID: id, Project: "/p", Assistant: "claude", State: "briefed",
			CreatedAt: at, SecretHash: "h", Record: []byte(rec)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.WorkTasks(ctx, []string{w, "0b0a0000-0000-4000-8000-00000000ffff"})
	if err != nil || len(got[w]) != 1 || got[w][0].ID != "a" || len(got) != 1 {
		t.Fatalf("bound %v %v", got, err)
	}
	var plan string
	if err := s.db.QueryRowContext(ctx, `EXPLAIN QUERY PLAN SELECT id FROM broker_tasks
		WHERE json_valid(record) AND json_extract(record, '$.work_id') = ?`, w).Scan(new(int), new(int), new(int), &plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "broker_tasks_work") {
		t.Fatalf("the bound-task query does not use its index: %s", plan)
	}
}
