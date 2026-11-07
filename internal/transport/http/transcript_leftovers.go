package http

import (
	"context"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// attachLeftovers puts what each completion notice's delivery left undone on
// the notice as the transcript is served.
//
// The line typed into the root carries only the count (notice.go: the list
// belongs in result.json, not in a line typed at a session), so a card that
// says "it left two things" had nothing behind it to open. The list is read
// here, from the task's stored result, rather than added to the typed line:
// the root's context stays the size it was, and a card typed before this
// existed opens too. Cloud reads the transcript through this route, so the
// phone gets the same list.
//
// A notice is matched to its task by notice_id as well as task id, so a line
// that merely looks like a notice cannot borrow another task's list. Anything
// that cannot be read leaves the card as it was: the count without the list.
func (s *Server) attachLeftovers(ctx context.Context, entries []contract.TranscriptEntry) {
	if s.broker == nil {
		return
	}
	read := map[string][]contract.TranscriptNoticeLeftover{}
	for i := range entries {
		n := entries[i].Notice
		if n == nil || n.Kind != "task_finished" || n.Leftovers <= 0 || n.Task == nil || n.Task.ID == "" || n.NoticeID == "" {
			continue
		}
		key := n.Task.ID + "\x00" + n.NoticeID
		items, seen := read[key]
		if !seen {
			items = s.leftoverItems(ctx, n.Task.ID, n.NoticeID)
			read[key] = items
		}
		n.LeftoverItems = items
	}
}

func (s *Server) leftoverItems(ctx context.Context, taskID, noticeID string) []contract.TranscriptNoticeLeftover {
	r, _, err := s.broker.Record(ctx, taskID)
	if err != nil || r.Notice == nil || r.Notice.ID != noticeID || r.Result == nil {
		return nil
	}
	// Read again rather than trusted from the record, as a proposal from a
	// leftover is (app/proposals.go): every string stays within its bound.
	list, err := work.ParseLeftovers(r.Result.Leftovers)
	if err != nil {
		return nil
	}
	out := make([]contract.TranscriptNoticeLeftover, 0, len(list))
	for _, lo := range list {
		out = append(out, contract.TranscriptNoticeLeftover{Title: lo.Title, Why: lo.Why})
	}
	return out
}
