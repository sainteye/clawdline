package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	sb "github.com/sainteye/clawdline/internal/domain/sessionboard"
)

func TestSessionBoardLifecycle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	lead := sb.Actor{ID: "codex:lead", Label: "Lead", Persona: "architect"}
	member := sb.Actor{ID: "claude:member", Label: "Member", Persona: "code-reviewer"}
	person := sb.Actor{Label: "Person"}
	now := time.Now()
	const project = "project-one"
	change := func(actor, target sb.Actor, cmd sb.Command, key string, want error) sb.Result {
		t.Helper()
		got, err := st.ChangeSessionBoard(ctx, project, actor, target, cmd, key, now)
		if !errors.Is(err, want) {
			t.Fatalf("%s: got %v, want %v", cmd.Action, err, want)
		}
		return got
	}
	create := sb.Command{Action: "create", Name: "Acceptance", Text: "Share one build"}
	id := change(lead, sb.Actor{}, create, "create", nil).ID
	if got := change(lead, sb.Actor{}, create, "create", nil); got.ID != id {
		t.Fatal("replayed create returned a different board")
	}
	create.Text = "A different purpose"
	change(lead, sb.Actor{}, create, "create", sb.ErrReplay)
	post := sb.Command{Action: "post", Board: id, Text: "Pinned build"}
	change(member, sb.Actor{}, post, "post", sb.ErrDenied)
	change(lead, sb.Actor{}, post, "lead-post", nil)
	list, err := st.SessionBoards(ctx, project, member.ID)
	if err != nil || len(list) != 1 || len(list[0].Members) != 0 || list[0].Purpose == "" || list[0].Latest != 0 {
		t.Fatalf("discovery returned %v, %v", list, err)
	}
	if _, err := st.SessionBoardPage(ctx, project, member.ID, id, 0, 0); !errors.Is(err, sb.ErrDenied) {
		t.Fatal(err)
	}
	if _, err := st.ChangeSessionBoard(ctx, "other", member, sb.Actor{}, sb.Command{Action: "join", Board: id}, "", now); !errors.Is(err, sb.ErrMissing) {
		t.Fatal(err)
	}
	change(member, sb.Actor{}, sb.Command{Action: "join", Board: id}, "", nil)
	change(member, sb.Actor{}, sb.Command{Action: "summary", Board: id, Text: "Summary"}, "", sb.ErrDenied)
	change(lead, sb.Actor{}, sb.Command{Action: "summary", Board: id, Text: "Summary"}, "", nil)
	post.ReplyTo = 1
	post.Text = "Reply"
	if got := change(member, sb.Actor{}, post, "post", nil); got.Seq != 2 {
		t.Fatal(got)
	}
	if got := change(member, sb.Actor{}, post, "post", nil); got.Seq != 2 {
		t.Fatal(got)
	}
	change(lead, member, sb.Command{Action: "transfer", Board: id, Target: member.ID}, "", nil)
	change(lead, sb.Actor{}, sb.Command{Action: "leave", Board: id}, "", nil)
	change(lead, sb.Actor{}, sb.Command{Action: "archive", Board: id}, "", sb.ErrDenied)
	change(member, sb.Actor{}, sb.Command{Action: "archive", Board: id}, "", nil)
	change(member, sb.Actor{}, post, "new-post", sb.ErrClosed)
	if got := change(member, sb.Actor{}, post, "post", nil); got.Seq != 2 {
		t.Fatal(got)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	page, err := st.SessionBoardPage(ctx, project, member.ID, id, 1, 0)
	if err != nil || page.Board.ArchivedAt == 0 || page.Board.Lead != member.ID ||
		page.Board.Summary != "Summary" || len(page.Board.Members) != 1 || page.Board.Members[0] != member ||
		len(page.Posts) != 1 || page.Posts[0].ReplyTo != 1 || page.Posts[0].Author != member {
		t.Fatalf("reopened board: %+v %v", page, err)
	}
	if err := st.pruneSessionBoards(ctx, now.AddDate(0, 0, sb.MaxArchiveDays-1)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SessionBoardPage(ctx, "", person.ID, id, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := st.pruneSessionBoards(ctx, now.AddDate(0, 0, sb.MaxArchiveDays)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SessionBoardPage(ctx, "", person.ID, id, 0, 0); !errors.Is(err, sb.ErrMissing) {
		t.Fatal(err)
	}
	if n, err := st.SessionBoardOccupancy(ctx, "members"); n != 0 || err != nil {
		t.Fatalf("archive cleanup left %d members: %v", n, err)
	}
}

func TestSessionBoardAddsRolesToAnExistingStore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lead := sb.Actor{ID: "codex:lead", Label: "Lead"}
	now := time.Now()
	board, err := st.ChangeSessionBoard(ctx, "project", lead, sb.Actor{}, sb.Command{Action: "create", Name: "Review", Text: "Share decisions"}, "create", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"session_board_members", "session_board_posts"} {
		if _, err := st.db.Exec("ALTER TABLE " + table + " DROP COLUMN persona"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	id := board.ID
	lead.Persona = "architect"
	if _, err := st.ChangeSessionBoard(ctx, "project", lead, sb.Actor{}, sb.Command{Action: "join", Board: id}, "", now); err != nil {
		t.Fatal(err)
	}
	page, err := st.SessionBoardPage(ctx, "project", lead.ID, id, 0, 0)
	if err != nil || len(page.Board.Members) != 1 || page.Board.Members[0] != lead {
		t.Fatalf("migrated board: %+v %v", page, err)
	}
}

func TestSessionBoardMigratesReplyRecipients(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	lead := sb.Actor{ID: "codex:lead", Label: "Lead"}
	now := time.Now()
	change := func(actor sb.Actor, cmd sb.Command, key string) sb.Result {
		t.Helper()
		out, err := st.ChangeSessionBoard(ctx, "project", actor, sb.Actor{}, cmd, key, now)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	board := change(lead, sb.Command{Action: "create", Name: "Review", Text: "Topic"}, "create").ID
	original := change(lead, sb.Command{Action: "post", Board: board, Text: "Original"}, "original")
	reply := change(sb.Actor{}, sb.Command{Action: "post", Board: board, Text: "Reply", ReplyTo: original.Seq}, "reply")
	if _, err := st.db.Exec("ALTER TABLE session_board_posts DROP COLUMN reply_actor"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec("DELETE FROM session_board_posts WHERE board=? AND seq=?", board, original.Seq); err != nil {
		t.Fatal(err)
	}
	inbox, err := st.SessionBoardInbox(ctx, "project", lead.ID, board, 0, 0)
	if err != nil || len(inbox.Posts) != 1 || inbox.Posts[0].Seq != reply.Seq {
		t.Fatalf("migrated inbox: %+v, %v", inbox, err)
	}
}

func TestSessionBoardConcurrentPostAndArchive(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	lead := sb.Actor{ID: "codex:lead", Label: "Lead"}
	now := time.Now()
	b, err := st.ChangeSessionBoard(ctx, "project", lead, sb.Actor{}, sb.Command{Action: "create", Name: "Room", Text: "Purpose"}, "create", now)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errs := make(chan error, 3)
	post := sb.Command{Action: "post", Board: b.ID, Text: "Only saved once"}
	for _, c := range []sb.Command{post, post, {Action: "archive", Board: b.ID}} {
		group.Go(func() {
			_, err := st.ChangeSessionBoard(ctx, "project", lead, sb.Actor{}, c, "same-key", now)
			errs <- err
		})
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, sb.ErrClosed) {
			t.Fatal(err)
		}
	}
	page, err := st.SessionBoardPage(ctx, "project", lead.ID, b.ID, 0, 0)
	if err != nil || page.Board.ArchivedAt == 0 || len(page.Posts) > 1 || page.Board.Latest != int64(len(page.Posts)) {
		t.Fatalf("archive raced with post: %+v %v", page, err)
	}
	if _, err := st.ChangeSessionBoard(ctx, "project", lead, sb.Actor{}, post, "new-key", now); !errors.Is(err, sb.ErrClosed) {
		t.Fatal(err)
	}
}

func TestSessionBoardRetainsOnlyNewestPostsAndKeepsCounting(t *testing.T) {
	ctx := t.Context()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	const project = "project"
	lead := sb.Actor{ID: "codex:lead", Label: "Lead"}
	member := sb.Actor{ID: "claude:member", Label: "Member"}
	now := time.Now()
	change := func(actor sb.Actor, cmd sb.Command, key string, want error) sb.Result {
		t.Helper()
		result, err := st.ChangeSessionBoard(ctx, project, actor, sb.Actor{}, cmd, key, now)
		if !errors.Is(err, want) {
			t.Fatalf("change %q (%s): got %v, want %v", key, cmd.Action, err, want)
		}
		return result
	}
	board := change(lead, sb.Command{Action: "create", Name: "Review", Text: "Topic"}, "create", nil).ID
	change(member, sb.Command{Action: "join", Board: board}, "", nil)
	post := sb.Command{Action: "post", Board: board, Text: "Discussion"}
	for seq := int64(1); seq <= sb.MaxPosts; seq++ {
		if got := change(lead, post, fmt.Sprint(seq), nil).Seq; got != seq {
			t.Fatalf("first window sequence = %d, want %d", got, seq)
		}
	}
	reply := sb.Command{Action: "post", Board: board, Text: "Reply while the oldest post is still visible", ReplyTo: 1}
	replySeq := change(member, reply, "reply", nil).Seq
	if replySeq != sb.MaxPosts+1 {
		t.Fatalf("first new sequence = %d", replySeq)
	}
	if got := change(member, reply, "reply", nil).Seq; got != replySeq {
		t.Fatalf("retry after original was removed = %d, want %d", got, replySeq)
	}
	reply.Text = "Changed retry"
	change(member, reply, "reply", sb.ErrReplay)
	change(member, reply, "late-reply", sb.ErrInvalid)
	change(lead, post, "1", sb.ErrPostExpired)

	var group sync.WaitGroup
	for _, key := range []string{"next-a", "next-b"} {
		group.Go(func() {
			if _, err := st.ChangeSessionBoard(ctx, project, lead, sb.Actor{}, post, key, now); err != nil {
				t.Errorf("concurrent post %s: %v", key, err)
			}
		})
	}
	group.Wait()
	var seqs []int64
	cursor := int64(1) // An evicted post is still a valid pagination boundary.
	for {
		page, err := st.SessionBoardPage(ctx, project, lead.ID, board, cursor, 0)
		if err != nil || page.Board.Latest != replySeq+2 || (page.More && len(page.Posts) == 0) {
			t.Fatalf("thread after %d: %+v, %v", cursor, page, err)
		}
		for _, p := range page.Posts {
			seqs = append(seqs, p.Seq)
		}
		if !page.More {
			break
		}
		cursor = seqs[len(seqs)-1]
	}
	if len(seqs) != sb.MaxPosts {
		t.Fatalf("retained %d posts", len(seqs))
	}
	for i, seq := range seqs {
		if seq != int64(i+4) {
			t.Fatalf("retained post %d has sequence %d", i, seq)
		}
	}
	if n, err := st.SessionBoardOccupancy(ctx, "posts"); err != nil || n != sb.MaxPosts {
		t.Fatalf("retention occupancy = %d, %v", n, err)
	}
	inbox, err := st.SessionBoardInbox(ctx, project, lead.ID, board, sb.MaxPosts, 0)
	if err != nil || len(inbox.Posts) != 1 || inbox.Posts[0].Seq != replySeq || inbox.Posts[0].ReplyTo != 1 {
		t.Fatalf("retained reply to removed original: %+v, %v", inbox, err)
	}
}

func TestSessionBoardInboxPagesOnlyOtherAuthorsDirectReplies(t *testing.T) {
	ctx := t.Context()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	const project = "project"
	lead := sb.Actor{ID: "codex:lead", Label: "Lead"}
	member := sb.Actor{ID: "claude:member", Label: "Member"}
	person := sb.Actor{Label: "Person"}
	now := time.Now()
	change := func(actor, target sb.Actor, cmd sb.Command, key string) sb.Result {
		t.Helper()
		out, err := st.ChangeSessionBoard(ctx, project, actor, target, cmd, key, now)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	board := change(lead, sb.Actor{}, sb.Command{Action: "create", Name: "Review", Text: "Topic"}, "create").ID
	other := change(lead, sb.Actor{}, sb.Command{Action: "create", Name: "Other", Text: "Other topic"}, "other").ID
	change(member, sb.Actor{}, sb.Command{Action: "join", Board: board}, "")
	post := func(actor sb.Actor, id string, replyTo int64, text string) int64 {
		return change(actor, sb.Actor{}, sb.Command{Action: "post", Board: id, Text: text, ReplyTo: replyTo}, text).Seq
	}
	mine := post(lead, board, 0, "Mine")
	theirs := post(member, board, 0, "Theirs")
	post(lead, board, mine, "Self-reply")
	first := post(person, board, mine, "Person reply")
	post(person, board, theirs, "Someone else's reply")
	post(lead, other, 0, "Other board original")
	post(person, other, mine, "Other board reply")
	last := first
	for i := range sb.MaxPageRows {
		last = post(member, board, mine, fmt.Sprintf("Reply %d", i))
	}

	page, err := st.SessionBoardInbox(ctx, project, lead.ID, board, 0, 0)
	if err != nil || !page.More || len(page.Posts) != sb.MaxPageRows || page.Posts[len(page.Posts)-1].Seq != last {
		t.Fatalf("newest replies: %+v, %v", page, err)
	}
	for _, reply := range page.Posts {
		if reply.Author != member || reply.ReplyTo != mine {
			t.Fatalf("unrelated reply: %+v", reply)
		}
	}
	earlier, err := st.SessionBoardInbox(ctx, "", lead.ID, board, 0, page.Posts[0].Seq)
	if err != nil || earlier.More || len(earlier.Posts) != 1 || earlier.Posts[0].Seq != first || earlier.Posts[0].ReplyTo != mine {
		t.Fatalf("older replies: %+v, %v", earlier, err)
	}
	newer, err := st.SessionBoardInbox(ctx, project, lead.ID, board, first, 0)
	if err != nil || !slices.Equal(newer.Posts, page.Posts) || newer.More {
		t.Fatalf("newer replies: %+v, %v", newer, err)
	}
	if _, err := st.SessionBoardInbox(ctx, "another project", lead.ID, board, 0, 0); !errors.Is(err, sb.ErrMissing) {
		t.Fatalf("cross-project inbox: %v", err)
	}
	change(lead, member, sb.Command{Action: "transfer", Board: board, Target: member.ID}, "")
	change(lead, sb.Actor{}, sb.Command{Action: "leave", Board: board}, "")
	if _, err := st.SessionBoardInbox(ctx, project, lead.ID, board, 0, 0); !errors.Is(err, sb.ErrDenied) {
		t.Fatalf("left member still has an inbox: %v", err)
	}
}

func TestSessionBoardsShowRecentActivityBeforeArchives(t *testing.T) {
	ctx := t.Context()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lead := sb.Actor{ID: "codex:lead", Label: "Lead"}
	now := time.Unix(1000, 0)
	change := func(cmd sb.Command, key string, delta time.Duration) string {
		t.Helper()
		out, err := st.ChangeSessionBoard(ctx, "project", lead, sb.Actor{}, cmd, key, now.Add(delta))
		if err != nil {
			t.Fatal(err)
		}
		return out.ID
	}
	old := change(sb.Command{Action: "create", Name: "Alpha", Text: "Old board"}, "old", 0)
	newer := change(sb.Command{Action: "create", Name: "Zulu", Text: "New board"}, "newer", time.Second)
	archive := change(sb.Command{Action: "create", Name: "Latest", Text: "Archived board"}, "archive", 2*time.Second)
	change(sb.Command{Action: "archive", Board: archive}, "", 3*time.Second)
	check := func(want []string) {
		t.Helper()
		boards, err := st.SessionBoards(ctx, "project", lead.ID)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, board := range boards {
			got = append(got, board.ID)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("board order = %v, want %v", got, want)
		}
	}
	check([]string{newer, old, archive})
	change(sb.Command{Action: "post", Board: old, Text: "Recent work"}, "post", 4*time.Second)
	check([]string{old, newer, archive})
}

func TestArchiveInactiveSessionBoardRechecksTheRosterAndHandoff(t *testing.T) {
	ctx := t.Context()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Unix(1000, 0)
	lead := sb.Actor{ID: "codex:lead", Label: "Lead"}
	newMember := sb.Actor{ID: "claude:joined", Label: "New member"}
	change := func(actor sb.Actor, cmd sb.Command) string {
		t.Helper()
		out, err := st.ChangeSessionBoard(ctx, "project", actor, sb.Actor{}, cmd, "test", now)
		if err != nil {
			t.Fatal(err)
		}
		return out.ID
	}
	id := change(lead, sb.Command{Action: "create", Name: "Stale", Text: "Shared work"})
	read := func() sb.Board {
		t.Helper()
		page, err := st.SessionBoardPage(ctx, "", "", id, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		return page.Board
	}
	tryArchive := func(witness sb.Board, archived bool) {
		t.Helper()
		if err := st.ArchiveInactiveSessionBoard(ctx, witness, now); err != nil {
			t.Fatal(err)
		}
		if got := read().ArchivedAt > 0; got != archived {
			t.Fatalf("board archived = %t, want %t", got, archived)
		}
	}
	original := read()
	change(newMember, sb.Command{Action: "join", Board: id})
	tryArchive(original, false)
	beforePost := read()
	change(lead, sb.Command{Action: "post", Board: id, Text: "Do not race this post"})
	tryArchive(beforePost, false)
	const handoffID = "00000000-0000-4000-8000-000000000001"
	record := json.RawMessage(`{"session_boards":{"boards":["` + id + `"]}}`)
	if err := st.CreateOpened(ctx, TableHandoffs, Opened{ID: handoffID, State: "delivered", Record: record, CreatedAt: now, UpdatedAt: now}, nil); err != nil {
		t.Fatal(err)
	}
	witness := read()
	tryArchive(witness, false)
	if _, err := st.UpdateOpened(ctx, TableHandoffs, handoffID, func(h Opened) (*Opened, []Event, error) {
		h.Record = json.RawMessage(`{"board_transferred_at":1000,"session_boards":{"boards":["` + id + `"]}}`)
		return &h, nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	tryArchive(witness, true)
}
