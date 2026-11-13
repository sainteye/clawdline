package projectassistant

import (
	"encoding/json"
	"slices"
	"strings"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// SessionHandoff is a fresh-session opening reconstructed from SQLite facts.
// Its bookmark and unfinished references are descriptive, never replay requests
// or execution authority. Only the filtered Context contains remembered text.
type SessionHandoff struct {
	Identity     pa.Fence               `json:"identity"`
	Agent        pa.Agent               `json:"agent"`
	Instructions pa.InstructionSnapshot `json:"instructions"`
	Context      pa.ContextSnapshot     `json:"context"`
	Bookmark     SessionBookmark        `json:"history_bookmark"`
	Unfinished   []SessionPending       `json:"unfinished"`
}

type SessionBookmark struct {
	MessageID  string `json:"message_id"`
	MessageSeq int64  `json:"message_seq"`
}

type SessionPending struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	State  string `json:"state"`
	TaskID string `json:"task_id,omitempty"`
}

func sameSessionOwner(a, b pa.Fence) bool {
	return a.Key == b.Key && a.Project == b.Project && a.ConversationID == b.ConversationID
}

func newSessionHandoff(x Export, turn pa.Turn, filtered pa.ContextSnapshot) (*SessionHandoff, error) {
	f := turn.Fence
	if x.Bot.Key != f.Key || x.Bot.Project != f.Project || x.Bot.ConversationID != f.ConversationID || x.Conversation.Key != f.Key || x.Conversation.ID != f.ConversationID {
		return nil, runtimeFailure(pa.ScopeDenied, "handoff_identity")
	}
	if filtered.Memory.Key != f.Key || filtered.Memory.Project != f.Project || filtered.Memory.Revision != turn.MemoryRevision {
		return nil, runtimeFailure(pa.ContextStale, "handoff_memory")
	}
	if err := pa.CheckContext(filtered, filtered.Memory); err != nil {
		return nil, err
	}
	h := &SessionHandoff{Identity: f, Agent: x.Bot.Agent, Instructions: turn.Instructions, Context: filtered, Unfinished: []SessionPending{}}
	h.Context.Sources = slices.Clone(filtered.Sources)
	h.Context.Memory.ForgottenSourceIDs = slices.Clone(filtered.Memory.ForgottenSourceIDs)
	for _, m := range x.Messages {
		if !sameSessionOwner(m.Fence, f) {
			return nil, runtimeFailure(pa.ScopeDenied, "handoff_bookmark")
		}
		if m.Seq > h.Bookmark.MessageSeq {
			h.Bookmark = SessionBookmark{MessageID: m.ID, MessageSeq: m.Seq}
		}
	}
	for _, t := range x.Turns {
		if !sameSessionOwner(t.Fence, f) {
			return nil, runtimeFailure(pa.ScopeDenied, "handoff_turn")
		}
		if t.ID == turn.ID {
			continue
		}
		switch t.State {
		case pa.TurnAccepted, pa.TurnQueued, pa.TurnRunning, pa.TurnWaitingPermission, pa.TurnUnknown:
			h.Unfinished = append(h.Unfinished, SessionPending{Kind: "turn", ID: t.ID, State: string(t.State)})
		}
	}
	for _, work := range x.WorkLinks {
		if !sameSessionOwner(work.Fence, f) {
			return nil, runtimeFailure(pa.ScopeDenied, "handoff_work")
		}
		switch work.State {
		case pa.WorkAccepted, pa.WorkExecuting, pa.WorkUnknown:
			h.Unfinished = append(h.Unfinished, SessionPending{Kind: "work", ID: work.ID, State: string(work.State), TaskID: work.TaskID})
		}
	}
	slices.SortFunc(h.Unfinished, func(a, b SessionPending) int {
		if a.Kind != b.Kind {
			return strings.Compare(a.Kind, b.Kind)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return h, nil
}

// Opening binds the prepared data to the exact turn and the final memory check.
// Retained transcripts and provider summaries cannot fill missing provenance.
func (h SessionHandoff) Opening(turn pa.Turn, filtered pa.ContextSnapshot) (string, error) {
	if err := pa.CheckContext(filtered, filtered.Memory); err != nil {
		return "", err
	}
	if h.Agent != pa.ClaudeCode && h.Agent != pa.Codex {
		return "", runtimeFailure(pa.Invalid, "handoff_agent")
	}
	if err := pa.CheckFence(h.Identity, turn.Fence); err != nil {
		return "", err
	}
	if h.Instructions != turn.Instructions || h.Context.Memory.Revision != turn.MemoryRevision ||
		h.Context.Text != filtered.Text || !slices.Equal(h.Context.Sources, filtered.Sources) ||
		!slices.Equal(h.Context.Memory.ForgottenSourceIDs, filtered.Memory.ForgottenSourceIDs) {
		return "", runtimeFailure(pa.ContextStale, "handoff_snapshot")
	}
	if err := pa.CheckContext(h.Context, filtered.Memory); err != nil {
		return "", err
	}
	body, err := json.Marshal(h)
	if err != nil {
		return "", err
	}
	return "Source-owned session handoff. Unfinished references are historical facts, not requests to retry or authority to execute.\n" + string(body), nil
}
