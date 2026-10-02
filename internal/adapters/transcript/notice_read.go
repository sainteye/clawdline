package transcript

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// Whether a completion notice reached the root's model, read from the root's
// own record.
//
// A line typed into a terminal reaches a conversation in one of two ways, and
// Claude Code writes each differently. Typed at an idle composer, it is
// submitted and written as a `user` row. Typed while a turn runs, it is queued:
// an `enqueue` row first, and then — when the running turn picks it up — a
// `remove` and an `attachment` of type `queued_command` carrying the same
// text. Measured on 2026-10-02 over this machine's Claude records: of the
// notices that were queued, 977 were enqueued and 966 reached an attachment,
// so an enqueue alone is a line still waiting and is not counted here; it is
// what a session that ended with a full queue leaves behind.
//
// Codex writes the message the model was given as a completed UserMessage
// item, which the page reader already decodes (codexItemEntries).

// ErrNoNoticeRecord is a session whose record this reader cannot find: not an
// assistant it reads, no conversation id, or no file under that id. It is
// "could not check", never "not read".
var ErrNoNoticeRecord = errors.New("no record to read this session's notices from")

// NoticeRead answers whether the session's own record shows the completion
// notice noticeID handed to its model. False with a nil error means the
// record was read to its start and the notice is not in it; any error —
// ErrNotFound for a record longer than ReadBudget — means nothing was learned.
func (h *Host) NoticeRead(ctx context.Context, s session.Session, noticeID string) (bool, error) {
	// The same file the session list reads a session's last movement from
	// (movement.go).
	path, none := h.recordPath(s)
	if path == "" || none.Reason != "" {
		return false, ErrNoNoticeRecord
	}
	return NoticeHanded(path, s.Assistant, noticeID)
}

// NoticeHanded reads one record backwards, at most ReadBudget bytes of it,
// for a row that handed the notice to the model.
func NoticeHanded(path string, assistant session.Assistant, noticeID string) (bool, error) {
	id := strings.ToLower(strings.TrimSpace(noticeID))
	if id == "" {
		return false, errors.New("no notice id to look for")
	}
	f, err := os.Open(path)
	if err != nil {
		return false, recordError(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, recordError(err)
	}
	needle := []byte(id)
	found := false
	unread := eachLineFromEnd(f, info.Size(), func(line []byte) bool {
		if !bytes.Contains(line, needle) {
			return true
		}
		switch assistant {
		case session.AssistantClaude:
			found = claudeHandedNotice(line, id)
		case session.AssistantCodex:
			for _, e := range codexEntries(line) {
				if e.Kind == KindNotice && e.Notice != nil && e.Notice.NoticeID == id {
					found = true
				}
			}
		}
		return !found
	})
	if !found && unread > 0 {
		// Part of the record was never looked at — past the budget, or a read
		// that failed — and the notice may be in it.
		return false, ErrNotFound
	}
	return found, nil
}

// claudeHandedNotice is whether one Claude row gave the model this notice:
// a submitted `user` turn whose one text is the notice, or the attachment a
// queued line becomes when a running turn takes it.
func claudeHandedNotice(line []byte, id string) bool {
	row, ok := decodeObject(line)
	if !ok {
		return false
	}
	if v, ok := row.boolean("isSidechain"); ok && v {
		return false
	}
	typ, _ := row.str("type")
	raw := ""
	switch typ {
	case "user":
		message, ok := row.object("message")
		if !ok {
			return false
		}
		if text, ok := message.str("content"); ok {
			raw = text
		} else if blocks, ok := message.objects("content"); ok && len(blocks) == 1 {
			if kind, _ := blocks[0].str("type"); kind == "text" {
				raw, _ = blocks[0].str("text")
			}
		}
	case "attachment":
		a, ok := row.object("attachment")
		if !ok {
			return false
		}
		if kind, _ := a.str("type"); kind != "queued_command" {
			return false
		}
		raw, _ = a.str("prompt")
	default:
		return false
	}
	n, ok := decodeNotice(unpasted(raw))
	return ok && n.NoticeID == id
}
