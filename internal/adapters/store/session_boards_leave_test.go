package store

import (
	"context"
	"errors"
	"testing"
	"time"

	sb "github.com/sainteye/clawdline/internal/domain/sessionboard"
)

// A Lead who is the board's only remaining member can leave; leaving archives
// the board instead of stranding it until the background sweep notices.
func TestSessionBoardLastMemberLeaveArchives(t *testing.T) {
	ctx := context.Background()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	lead := sb.Actor{ID: "codex:lead", Label: "Lead"}
	member := sb.Actor{ID: "claude:member", Label: "Member"}
	now := time.Now()
	const project = "project-one"
	change := func(actor sb.Actor, cmd sb.Command, key string, want error) sb.Result {
		t.Helper()
		got, err := st.ChangeSessionBoard(ctx, project, actor, sb.Actor{}, cmd, key, now)
		if !errors.Is(err, want) {
			t.Fatalf("%s by %s: got %v, want %v", cmd.Action, actor.ID, err, want)
		}
		return got
	}
	id := change(lead, sb.Command{Action: "create", Name: "Solo", Text: "One Session"}, "create", nil).ID
	change(member, sb.Command{Action: "join", Board: id}, "", nil)
	// With another member present the Lead must transfer first.
	change(lead, sb.Command{Action: "leave", Board: id}, "", sb.ErrDenied)
	change(member, sb.Command{Action: "leave", Board: id}, "", nil)

	change(lead, sb.Command{Action: "leave", Board: id}, "", nil)
	active, err := st.ActiveSessionBoardIDs(ctx, project, lead.ID)
	if err != nil || len(active) != 0 {
		t.Fatalf("the last member still has an active board: %v, %v", active, err)
	}
	// A repeated leave is the same outcome, and the history stays readable.
	change(lead, sb.Command{Action: "leave", Board: id}, "", nil)
	page, err := st.SessionBoardPage(ctx, project, lead.ID, id, 0, 0)
	if err != nil || page.Board.ArchivedAt == 0 {
		t.Fatalf("archived board page: %+v %v", page.Board, err)
	}
	change(lead, sb.Command{Action: "post", Board: id, Text: "late"}, "late", sb.ErrClosed)
}
