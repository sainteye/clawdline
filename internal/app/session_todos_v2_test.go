package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

const ownTodoSession = "30000000-0000-4000-8000-000000000003"

// Nine rows written in the same second come back in the order the Session
// gave them, carry the Session as their author, and are already read.
func TestASessionsOwnTodosKeepTheirOrderAndProvenance(t *testing.T) {
	w := newWorkV2Test(t)
	texts := make([]string, 9)
	for i := range texts {
		// Reverse-sorted text, so a sort by text or by a random id cannot pass.
		texts[i] = fmt.Sprintf("  step %d of the cleanup  ", 9-i)
	}
	created, err := w.CreateSessionTodos(context.Background(), NewSessionTodosV2{SessionID: ownTodoSession, Texts: texts}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 9 {
		t.Fatalf("created %d rows", len(created))
	}
	rows, _, err := w.DirectTodos(context.Background(), ownTodoSession, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 9 {
		t.Fatalf("read %d rows", len(rows))
	}
	for i, td := range rows {
		want := strings.TrimSpace(texts[i])
		if td.Text != want || td.ID != created[i].ID {
			t.Fatalf("row %d = %q (%s), want %q (%s)", i, td.Text, td.ID, want, created[i].ID)
		}
		if td.CreatedBy != ownTodoSession {
			t.Fatalf("row %d created_by = %q", i, td.CreatedBy)
		}
		if td.ReadAt.IsZero() || !td.ReadAt.Equal(td.CreatedAt) || !td.SentAt.IsZero() {
			t.Fatalf("row %d receipts: read %v created %v sent %v", i, td.ReadAt, td.CreatedAt, td.SentAt)
		}
	}
	// The Agent's own read keeps the same order.
	agent, _, err := w.DirectTodos(context.Background(), ownTodoSession, false, true)
	if err != nil || len(agent) != 9 || agent[0].ID != created[0].ID || agent[8].ID != created[8].ID {
		t.Fatalf("agent read: %v %+v", err, agent)
	}
}

// A batch that would cross the 500 open rows is refused whole: not the rows
// that fit, and not a partial prefix.
func TestASessionTodoBatchPastTheOpenLimitWritesNothing(t *testing.T) {
	w := newWorkV2Test(t)
	ctx := context.Background()
	for len(texts(t, w)) < store.DirectTodoV2Limit-3 {
		batch := make([]string, 0, sessionTodoBatchLimit)
		for n := len(texts(t, w)); len(batch) < sessionTodoBatchLimit && n+len(batch) < store.DirectTodoV2Limit-3; {
			batch = append(batch, fmt.Sprintf("row %d", n+len(batch)))
		}
		if _, err := w.CreateSessionTodos(ctx, NewSessionTodosV2{SessionID: ownTodoSession, Texts: batch}, nil); err != nil {
			t.Fatal(err)
		}
	}
	before := len(texts(t, w))
	_, err := w.CreateSessionTodos(ctx, NewSessionTodosV2{SessionID: ownTodoSession,
		Texts: []string{"a", "b", "c", "d"}}, nil)
	var we *WorkError
	if !errors.As(err, &we) || we.Code != "direct_todos_full" {
		t.Fatalf("over the limit: %v", err)
	}
	if after := len(texts(t, w)); after != before {
		t.Fatalf("a refused batch wrote %d rows", after-before)
	}
	if _, err := w.CreateSessionTodos(ctx, NewSessionTodosV2{SessionID: ownTodoSession,
		Texts: []string{"a", "b", "c"}}, nil); err != nil {
		t.Fatalf("exactly to the limit: %v", err)
	}
}

// Shape refusals name the row and write nothing, including the rows before it.
func TestASessionTodoBatchWithABadRowWritesNothing(t *testing.T) {
	w := newWorkV2Test(t)
	ctx := context.Background()
	for _, c := range []struct {
		name  string
		texts []string
		code  string
	}{
		{"empty batch", nil, "todos_required"},
		{"blank row", []string{"first", "   "}, "todo_text_required"},
		{"oversized row", []string{"first", strings.Repeat("x", directTodoTextLimit+1)}, "todo_too_large"},
		{"twenty-one rows", make21(), "too_many_todos"},
	} {
		_, err := w.CreateSessionTodos(ctx, NewSessionTodosV2{SessionID: ownTodoSession, Texts: c.texts}, nil)
		var we *WorkError
		if !errors.As(err, &we) || we.Code != c.code {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	if n := len(texts(t, w)); n != 0 {
		t.Fatalf("refused batches wrote %d rows", n)
	}
	twenty := make21()[:sessionTodoBatchLimit]
	if _, err := w.CreateSessionTodos(ctx, NewSessionTodosV2{SessionID: ownTodoSession, Texts: twenty}, nil); err != nil {
		t.Fatalf("twenty rows: %v", err)
	}
}

func make21() []string {
	out := make([]string, sessionTodoBatchLimit+1)
	for i := range out {
		out[i] = fmt.Sprintf("row %d", i)
	}
	return out
}

func texts(t *testing.T, w *WorkSystemV2) []string {
	t.Helper()
	rows, _, err := w.Store.DirectTodosV2(context.Background(), ownTodoSession, false, false, store.DirectTodoV2Limit+10)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Text)
	}
	return out
}
