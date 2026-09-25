package http

import (
	"context"
	"log"
	"sort"
	"sync/atomic"

	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// boardTitles answers, for one assistant's conversations, the label of the
// Root Assignment a Board item opened each of them with. Only a `new_session`
// assignment names a conversation; one given to a session that already had
// its own identity does not (store.WorkV2RootAssignmentsForSessions).
//
// The resume picker asks it for one place's history and the live session
// list for everything on screen, so the two say the same name for the same
// conversation. projectPath "" asks by conversation alone. known are Root
// Assignments already read; any other is read by id. A conversation whose
// assignment could not be read is left out and the first error is returned
// beside what the others said.
func (s *Server) boardTitles(ctx context.Context, projectPath, assistant string, conversations []string,
	known []orchestrator.RootAssignment) (map[string]string, error) {
	out := map[string]string{}
	if s.store == nil || s.broker == nil || len(conversations) == 0 {
		return out, nil
	}
	ids, err := s.store.WorkV2RootAssignmentsForSessions(ctx, projectPath, assistant, conversations)
	if err != nil {
		return out, err
	}
	labels := make(map[string]string, len(known))
	for _, a := range known {
		labels[a.ID] = a.Label
	}
	var first error
	for conversation, id := range ids {
		label, ok := labels[id]
		if !ok {
			a, err := s.broker.RootAssignmentByID(ctx, id)
			if err != nil {
				if first == nil {
					first = err
				}
				continue
			}
			label = a.Label
			labels[id] = label
		}
		if label != "" {
			out[conversation] = label
		}
	}
	return out, first
}

// boardTitleErrorLogged keeps an unreadable Board from writing one line per
// poll of the session list.
var boardTitleErrorLogged atomic.Bool

// ownBoardTitles is boardTitles for every conversation on screen: one read
// per assistant, whatever the number of rows. A row with no conversation id
// asks nothing. A Board that cannot be read costs the rows this rung only —
// they keep the names they had before it existed — so it is not counted as
// this daemon's records missing, which would hold every row's closing.
func (s *Server) ownBoardTitles(ctx context.Context, lives []swiftstore.Live, known []orchestrator.RootAssignment) []swiftstore.BoardTitle {
	byAssistant := map[string][]string{}
	seen := map[string]bool{}
	for _, l := range lives {
		key := l.Assistant + "\x01" + l.ConversationID
		if l.Assistant == "" || l.ConversationID == "" || seen[key] {
			continue
		}
		seen[key] = true
		byAssistant[l.Assistant] = append(byAssistant[l.Assistant], l.ConversationID)
	}
	out := []swiftstore.BoardTitle{}
	for assistant, conversations := range byAssistant {
		titles, err := s.boardTitles(ctx, "", assistant, conversations, known)
		if err != nil && boardTitleErrorLogged.CompareAndSwap(false, true) {
			log.Printf("sessions: Board titles for %s: %v", assistant, err)
		}
		for conversation, label := range titles {
			out = append(out, swiftstore.BoardTitle{Assistant: assistant, ConversationID: conversation, Label: label})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Assistant != out[j].Assistant {
			return out[i].Assistant < out[j].Assistant
		}
		return out[i].ConversationID < out[j].ConversationID
	})
	return out
}
