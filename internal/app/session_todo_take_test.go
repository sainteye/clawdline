package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func takeRefusal(t *testing.T, err error) string {
	t.Helper()
	var we *WorkError
	if !errors.As(err, &we) {
		t.Fatalf("not a typed refusal: %v", err)
	}
	return we.Code
}

// A root Session under session_todo_auto_take takes the person's unsent
// to-dos one at a time and oldest first; nothing it may not work on is taken.
func TestASessionTakesThePersonsTodosOneAtATimeInOrder(t *testing.T) {
	ctx := context.Background()
	w := newWorkV2Test(t)
	person := func(text string) string {
		td, err := w.CreateDirectTodo(ctx, NewDirectTodoV2{SessionID: ownTodoSession, Text: text, Actor: "person"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return td.ID
	}
	first, second, sent := person("first"), person("second"), person("already sent")
	if _, err := w.MarkDirectTodoSent(ctx, sent, ownTodoSession, time.Now()); err != nil {
		t.Fatal(err)
	}
	own, err := w.CreateSessionTodos(ctx, NewSessionTodosV2{SessionID: ownTodoSession, Texts: []string{"my own step"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := w.CreateDirectTodo(ctx, NewDirectTodoV2{SessionID: "30000000-0000-4000-8000-000000000009", Text: "elsewhere", Actor: "person"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	for name, c := range map[string]struct{ id, code string }{
		"out of order":      {second, "todo_not_next"},
		"sent by person":    {sent, "todo_already_sent"},
		"session's own row": {own[0].ID, "todo_session_created"},
		"other session":     {other.ID, "todo_session_mismatch"},
	} {
		if _, err := w.TakeDirectTodo(ctx, c.id, ownTodoSession, nil); takeRefusal(t, err) != c.code {
			t.Fatalf("%s: %v, want %s", name, err, c.code)
		}
	}

	taken, err := w.TakeDirectTodo(ctx, first, ownTodoSession, nil)
	if err != nil || taken.TakenAt.IsZero() || taken.ReadAt.IsZero() || !taken.SentAt.IsZero() {
		t.Fatalf("take = %+v, %v", taken, err)
	}
	again, err := w.TakeDirectTodo(ctx, first, ownTodoSession, nil)
	if err != nil || again.Version != taken.Version {
		t.Fatalf("a second take of the same row changed it: %+v, %v", again, err)
	}
	if _, err := w.TakeDirectTodo(ctx, second, ownTodoSession, nil); takeRefusal(t, err) != "todo_take_pending" {
		t.Fatalf("second take while first open: %v", err)
	}
	if _, err := w.CompleteDirectTodo(ctx, first, ownTodoSession, ownTodoSession, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := w.TakeDirectTodo(ctx, first, ownTodoSession, nil); takeRefusal(t, err) != "todo_completed" {
		t.Fatalf("taking a completed row: %v", err)
	}
	next, err := w.TakeDirectTodo(ctx, second, ownTodoSession, nil)
	if err != nil || next.TakenAt.IsZero() {
		t.Fatalf("next take = %+v, %v", next, err)
	}
	// The taken mark is stored, not only answered.
	rows, _, err := w.DirectTodos(ctx, ownTodoSession, true, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, td := range rows {
		if (td.ID == first || td.ID == second) != !td.TakenAt.IsZero() {
			t.Fatalf("row %s (%q) taken_at %v", td.ID, td.Text, td.TakenAt)
		}
	}
}
