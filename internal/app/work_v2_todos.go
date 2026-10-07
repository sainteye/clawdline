package app

import (
	"context"
	"fmt"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type NewDirectTodoV2 struct {
	SessionID string
	Text      string
	Actor     string
}

func (w *WorkSystemV2) CreateDirectTodo(ctx context.Context, n NewDirectTodoV2, file TodoV2Filer) (work.DirectTodoV2, error) {
	n.Text = strings.TrimSpace(n.Text)
	if n.SessionID == "" || n.Text == "" {
		return work.DirectTodoV2{}, workV2Error(http.StatusBadRequest, "todo_text_required", "Choose a Session and enter what it should do.")
	}
	if len(n.Text) > directTodoTextLimit {
		return work.DirectTodoV2{}, workV2Error(http.StatusRequestEntityTooLarge, "todo_too_large", "A direct to-do is at most 8 KiB.")
	}
	td := work.DirectTodoV2{ID: newWorkID(), SessionID: n.SessionID, Text: n.Text, CreatedBy: n.Actor,
		CreatedAt: w.now(), Version: 1}
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if err := tx.CreateDirectTodo(td); err != nil {
			return err
		}
		if file != nil {
			if k, ans, ok := file(td); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	return td, mapWorkV2Error(err)
}

// NewSessionTodosV2 is a Session writing its own to-dos, which it does only
// when the person asked it to. The conversation id is both the owner and the
// provenance: a row whose CreatedBy is its own SessionID was written by that
// Session, not sent by the person.
type NewSessionTodosV2 struct {
	SessionID string
	Texts     []string
}

type TodosV2Filer func([]work.DirectTodoV2) (store.ReceiptKey, store.ReceiptAnswer, bool)

// CreateSessionTodos writes the whole batch in one transaction or nothing.
// The rows count as already read: the Session wrote them, so there is no
// delivery for it to observe.
func (w *WorkSystemV2) CreateSessionTodos(ctx context.Context, n NewSessionTodosV2, file TodosV2Filer) ([]work.DirectTodoV2, error) {
	if n.SessionID == "" {
		return nil, workV2Error(http.StatusBadRequest, "session_required", "Name the Session the to-dos belong to.")
	}
	switch {
	case len(n.Texts) == 0:
		return nil, workV2Error(http.StatusBadRequest, "todos_required", "Send at least one to-do.")
	case len(n.Texts) > sessionTodoBatchLimit:
		return nil, workV2Error(http.StatusRequestEntityTooLarge, "too_many_todos",
			fmt.Sprintf("One call adds at most %d to-dos; nothing was added.", sessionTodoBatchLimit))
	}
	now := w.now()
	rows := make([]work.DirectTodoV2, 0, len(n.Texts))
	for i, text := range n.Texts {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, workV2Error(http.StatusBadRequest, "todo_text_required",
				fmt.Sprintf("To-do %d is empty; nothing was added.", i+1))
		}
		if len(text) > directTodoTextLimit {
			return nil, workV2Error(http.StatusRequestEntityTooLarge, "todo_too_large",
				fmt.Sprintf("To-do %d is over 8 KiB; nothing was added.", i+1))
		}
		rows = append(rows, work.DirectTodoV2{ID: newWorkID(), SessionID: n.SessionID, Text: text,
			CreatedBy: n.SessionID, CreatedAt: now, ReadAt: now, Version: 1})
	}
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if err := tx.CreateDirectTodos(rows); err != nil {
			return err
		}
		if file != nil {
			if k, ans, ok := file(rows); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	if err != nil {
		return nil, mapWorkV2Error(err)
	}
	return rows, nil
}

func (w *WorkSystemV2) DirectTodos(ctx context.Context, session string, includeCompleted, markRead bool) ([]work.DirectTodoV2, bool, error) {
	rows, truncated, err := w.Store.DirectTodosV2(ctx, session, includeCompleted, markRead, WorkV2PageSize)
	return rows, truncated, mapWorkV2Error(err)
}

type AddDirectTodoImageV2 struct {
	ExpectedVersion int64
	SessionID       string
	Title           string
	Data            []byte
	// MediaType is what Normalize wrote Data as: image/png or image/jpeg.
	MediaType string
	Width     int
	Height    int
	Position  int64
	Actor     string
}

type TodoImageV2Filer func(work.DirectTodoV2, work.DirectTodoImageV2) (store.ReceiptKey, store.ReceiptAnswer, bool)

func (w *WorkSystemV2) AddDirectTodoImage(ctx context.Context, id string, c AddDirectTodoImageV2,
	file TodoImageV2Filer) (work.DirectTodoV2, work.DirectTodoImageV2, error) {
	var out work.DirectTodoV2
	var image work.DirectTodoImageV2
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.DirectTodo(id)
		if err != nil {
			return err
		}
		if prev.SessionID != c.SessionID {
			return work.RefuseV2("todo_session_mismatch", "That to-do belongs to another Session.")
		}
		if !VersionHolds(c.ExpectedVersion, prev.Version) {
			return store.ErrConflict
		}
		if !prev.Open() || !prev.SentAt.IsZero() {
			return work.RefuseV2("todo_already_delivered", "Reference images can be added only before a to-do is sent or completed.")
		}
		c.Title = strings.TrimSpace(c.Title)
		if c.Title == "" {
			c.Title = "reference image"
		}
		if len(c.Title) > workV2TitleLimit {
			return work.RefuseV2("image_title_too_large", "A reference-image title is at most 240 bytes.")
		}
		if len(c.Data) == 0 || c.Width <= 0 || c.Height <= 0 || !referenceMediaType(c.MediaType) {
			return work.RefuseV2("invalid_image", "A reference image needs normalized PNG or JPEG pixels.")
		}
		image = work.DirectTodoImageV2{ID: newWorkID(), TodoID: id, Title: c.Title, MediaType: c.MediaType,
			ByteCount: int64(len(c.Data)), Width: c.Width, Height: c.Height, Position: c.Position,
			CreatedBy: c.Actor, CreatedAt: w.now()}
		if err := tx.AddDirectTodoImage(image, c.Data); err != nil {
			return err
		}
		if err := tx.PutDirectTodo(prev, prev); err != nil {
			return err
		}
		out = prev
		out.Version++
		if file != nil {
			if k, a, ok := file(out, image); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return out, image, mapWorkV2Error(err)
}

func (w *WorkSystemV2) DirectTodoImages(ctx context.Context, id string) ([]work.DirectTodoImageV2, error) {
	images, err := w.Store.DirectTodoV2Images(ctx, id)
	return images, mapWorkV2Error(err)
}

// DirectTodoResendAfter is how long an unread delivery is protected from a
// second one. Inside it a second Send is a double tap, or the same press from
// a second screen, and would type the words twice. Past it the Session has
// had its chance: a Session that never reads its queue — one that was busy,
// or does not follow the guide — used to leave the row unsendable for good.
const DirectTodoResendAfter = 2 * time.Minute

// CheckDirectTodoSend distinguishes a first delivery from a reminder. A
// delivery that has not been observed cannot be doubled until
// DirectTodoResendAfter has passed; once the Session has read its queue, the
// person may remind it again while the row is still open.
func CheckDirectTodoSend(td work.DirectTodoV2, session string, now time.Time) error {
	if td.SessionID != session {
		return workV2Error(http.StatusConflict, "todo_session_mismatch", "That to-do belongs to another Session.")
	}
	if !td.Open() {
		return workV2Error(http.StatusConflict, "todo_completed", "A completed to-do is not sent again.")
	}
	if !td.SentAt.IsZero() && td.ReadAt.IsZero() && now.Sub(td.SentAt) < DirectTodoResendAfter {
		return workV2Error(http.StatusConflict, "todo_awaiting_read", "This delivery was sent moments ago and the Session has not read it yet.")
	}
	return nil
}

// DirectTodoSendText is what Send types into a Session for a direct to-do:
// the person's words unchanged, then one line naming the row and the command
// that checks it off. Without that line the Session reads an ordinary message
// and, once the work is done, has no way to tell which row it finished
// (measured 2026-09-25: fixed, deployed and reported, with the to-do left
// open). Only trailing line breaks are dropped, so exactly one blank line
// separates the words from the line.
func DirectTodoSendText(id, text string) string {
	line := "(Clawdline to-do " + id + ". When it is done: clawdline todo done " + id + ")"
	text = strings.TrimRight(text, "\r\n")
	if strings.TrimSpace(text) == "" {
		return line
	}
	return text + "\n\n" + line
}

// OpenDeliveredTodos are this Session's direct to-dos that were sent or read
// and are not completed, oldest first: what a turn receipt reminds the
// Session to check off. It reads without marking anything read — a receipt
// is not the Session reading its queue. The answer holds at most
// reportOpenTodoLimit rows; truncated says there were more, or that the
// bounded read of open rows stopped before it saw them all.
func (w *WorkSystemV2) OpenDeliveredTodos(ctx context.Context, session string) ([]work.DirectTodoV2, bool, error) {
	rows, truncated, err := w.DirectTodos(ctx, session, false, false)
	if err != nil {
		return nil, false, err
	}
	out := make([]work.DirectTodoV2, 0, len(rows))
	for _, td := range rows {
		if !td.Open() || (td.SentAt.IsZero() && td.ReadAt.IsZero()) {
			continue
		}
		if len(out) == reportOpenTodoLimit {
			return out, true, nil
		}
		out = append(out, td)
	}
	return out, truncated, nil
}

// TodoPreview is a to-do's text cut to reportOpenTodoTextLimit characters,
// never inside one, with its line breaks folded so it prints as one line.
func TodoPreview(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= reportOpenTodoTextLimit {
		return text
	}
	r := []rune(text)
	return string(r[:reportOpenTodoTextLimit-1]) + "…"
}

func (w *WorkSystemV2) MarkDirectTodoSent(ctx context.Context, id, session string, at time.Time) (work.DirectTodoV2, error) {
	var out work.DirectTodoV2
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.DirectTodo(id)
		if err != nil {
			return err
		}
		if err := CheckDirectTodoSend(prev, session, at); err != nil {
			return err
		}
		next := prev
		next.SentAt, next.ReadAt = time.Unix(at.Unix(), 0), time.Time{}
		if err := tx.PutDirectTodo(prev, next); err != nil {
			return err
		}
		next.Version++
		out = next
		return nil
	})
	return out, mapWorkV2Error(err)
}

func (w *WorkSystemV2) CompleteDirectTodo(ctx context.Context, id, session, actor string, person bool, file TodoV2Filer) (work.DirectTodoV2, error) {
	var out work.DirectTodoV2
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.DirectTodo(id)
		if err != nil {
			return err
		}
		if prev.SessionID != session {
			return work.RefuseV2("todo_session_mismatch", "That to-do belongs to another Session.")
		}
		if !person && actor != session {
			return work.RefuseV2("not_todo_owner", "An Agent may complete only its own Session to-do.")
		}
		if !prev.CompletedAt.IsZero() {
			out = prev
			return nil
		}
		next := prev
		next.CompletedAt, next.CompletedBy = w.now(), actor
		if err := tx.PutDirectTodo(prev, next); err != nil {
			return err
		}
		next.Version++
		out = next
		if file != nil {
			if k, ans, ok := file(out); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

func (w *WorkSystemV2) ReopenDirectTodo(ctx context.Context, id, session string, file TodoV2Filer) (work.DirectTodoV2, error) {
	var out work.DirectTodoV2
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.DirectTodo(id)
		if err != nil {
			return err
		}
		if prev.SessionID != session {
			return work.RefuseV2("todo_session_mismatch", "That to-do belongs to another Session.")
		}
		if prev.CompletedAt.IsZero() {
			out = prev
			if file != nil {
				if k, ans, ok := file(out); ok {
					return tx.CompleteReceipt(k, ans)
				}
			}
			return nil
		}
		next := prev
		next.CompletedAt, next.CompletedBy = time.Time{}, ""
		if err := tx.PutDirectTodo(prev, next); err != nil {
			return err
		}
		next.Version++
		out = next
		if file != nil {
			if k, ans, ok := file(out); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

func (w *WorkSystemV2) DeleteDirectTodo(ctx context.Context, id, session string, file func() (store.ReceiptKey, store.ReceiptAnswer, bool)) error {
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		deleted, err := tx.DeleteDirectTodo(id, session)
		if err != nil {
			return err
		}
		if !deleted {
			return work.RefuseV2("todo_not_found", "No direct to-do with that id belongs to this Session.")
		}
		if file != nil {
			if k, ans, ok := file(); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	return mapWorkV2Error(err)
}
