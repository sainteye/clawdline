package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// The to-do table keeps what the rules decided and nothing else: one row per
// fact, written only through a transaction that holds the task, refused when
// a writer's copy is stale, and refused by the file itself in a state the
// rules do not have.
func TestATodoIsWrittenOnceAndOnlyFromWhatWasRead(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Unix(1_789_700_000, 0)
	id := "70d0f001-0000-4000-8000-000000000001"
	todo := work.Todo{ID: work.TodoID(work.OriginDispatch, id), Origin: work.OriginDispatch, Task: id,
		Owner: "conv", OwnerAssistant: "claude", State: work.TodoStateOpen, Reason: work.ReasonDispatched,
		CreatedAt: at, UpdatedAt: at}
	put := func(next work.Todo, prev *TodoRow) error {
		_, _, err := s.UpdateBrokerTask(ctx, id, func(tx *Tx, _ BrokerRow) (*BrokerWrite, error) {
			return nil, tx.PutTodo(next, prev, Event{Kind: "todo.test", Subject: next.ID})
		})
		return err
	}
	if _, err := s.CreateBrokerTaskTx(ctx, BrokerRow{ID: id, Project: "/p", Assistant: "claude", State: "briefed",
		CreatedAt: at, SecretHash: "h", Record: []byte(`{}`)}, nil, nil,
		func(tx *Tx) error { return tx.PutTodo(todo, nil) }); err != nil {
		t.Fatal(err)
	}
	// The same first write again is refused: one to-do per fact.
	if err := put(todo, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("a second first write answered %v", err)
	}
	held, err := s.Todo(ctx, todo.ID)
	if err != nil || held.Version != 0 || held.State != work.TodoStateOpen || !held.CreatedAt.Equal(at) {
		t.Fatalf("held %+v %v", held, err)
	}
	done := held.Todo
	done.State, done.Reason, done.ClosedAt = work.TodoStateDone, work.ReasonLanded, at.Add(time.Hour)
	if err := put(done, &held); err != nil {
		t.Fatal(err)
	}
	// A writer holding the copy from before that is told so, and writes nothing.
	stale := held.Todo
	stale.State, stale.Reason = work.TodoStateHandedOff, work.ReasonOwnerGone
	if err := put(stale, &held); !errors.Is(err, ErrConflict) {
		t.Fatalf("a stale write answered %v", err)
	}
	now, _ := s.Todo(ctx, todo.ID)
	if now.State != work.TodoStateDone || now.Version != 1 || !now.ClosedAt.Equal(at.Add(time.Hour)) {
		t.Fatalf("after the stale write: %+v", now)
	}
	// A state the rules do not have is refused by the file.
	bogus := now.Todo
	bogus.State = "archived"
	if err := put(bogus, &now); err == nil || errors.Is(err, ErrConflict) {
		t.Fatalf("an unknown state answered %v", err)
	}

	counts, err := s.TodoCounts(ctx, "conv")
	if err != nil || counts[work.TodoStateDone] != 1 || len(counts) != 1 {
		t.Fatalf("counts %v %v", counts, err)
	}
	if owed, err := s.OutstandingTodos(ctx); err != nil || len(owed) != 0 {
		t.Fatalf("outstanding %v %v", owed, err)
	}
	if missing, err := s.TasksWithoutTodo(ctx, work.OriginDispatch); err != nil || len(missing) != 0 {
		t.Fatalf("tasks without a to-do: %v %v", missing, err)
	}
	// Reading from inside a write would wait on the one connection for ever.
	_, _, err = s.UpdateBrokerTask(ctx, id, func(*Tx, BrokerRow) (*BrokerWrite, error) {
		_, err := s.Todo(ctx, todo.ID)
		return nil, err
	})
	if !errors.Is(err, ErrNestedWrite) {
		t.Fatalf("a nested read answered %v", err)
	}
}
