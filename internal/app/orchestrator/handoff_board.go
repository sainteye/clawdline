package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
	sb "github.com/sainteye/clawdline/internal/domain/sessionboard"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// handoffSessionBoards records the active memberships captured at admission
// and the receiver execution observed before the first keystroke.
type handoffSessionBoards struct {
	Project      string   `json:"project"`
	From         string   `json:"from"`
	Boards       []string `json:"boards"`
	Conversation string   `json:"conversation,omitempty"`
	PID          int      `json:"pid,omitempty"`
	ProcessStart int64    `json:"process_start,omitempty"`
}

func (b *Broker) captureHandoffSessionBoards(ctx context.Context, sender session.Session, project string) (*handoffSessionBoards, error) {
	source, ok := projects.ResolveScope(sender.CWD)
	if !ok {
		return nil, nil
	}
	destination, ok := projects.ResolveScope(project)
	if !ok || destination.ID != source.ID {
		return nil, nil
	}
	from := string(sender.Assistant) + ":" + sender.ConversationID
	boards, err := b.Store.ActiveSessionBoardIDs(ctx, source.ID, from)
	if err != nil || len(boards) == 0 {
		return nil, err
	}
	return &handoffSessionBoards{Project: source.ID, From: from, Boards: boards}, nil
}

func (b *Broker) pinHandoffSessionBoardReceiver(transfer *handoffSessionBoards, row session.Session, assistant string) bool {
	if string(row.Assistant) != assistant || !handoffSessionBoardScopeMatches(transfer, row) {
		return false
	}
	transfer.Conversation = row.ConversationID
	if nativeNoticeTarget(row) {
		return transfer.Conversation != ""
	}
	if b.ProcessStart == nil || row.PID <= 0 {
		return false
	}
	started := b.ProcessStart(row.PID)
	if started.IsZero() {
		return false
	}
	transfer.PID, transfer.ProcessStart = row.PID, started.UnixNano()
	return true
}

func handoffSessionBoardScopeMatches(transfer *handoffSessionBoards, row session.Session) bool {
	if row.Observation.Freshness != "" && row.Observation.Freshness != session.FreshnessCurrent {
		return false
	}
	scope, ok := projects.ResolveScope(row.CWD)
	return ok && scope.ID == transfer.Project
}

func (b *Broker) isHandoffSessionBoardReceiver(h Handoff, row session.Session, rd reading) bool {
	transfer := h.SessionBoards
	if string(row.Assistant) != h.Assistant || row.ConversationID == h.FromSession || h.TypeAttemptedAt == 0 ||
		row.Activity.At.Unix() < h.TypeAttemptedAt || !handoffSessionBoardScopeMatches(transfer, row) ||
		(transfer.Conversation != "" && row.ConversationID != transfer.Conversation) {
		return false
	}
	for _, other := range rd.sessions {
		if other.ID != row.ID && other.Assistant == row.Assistant && other.ConversationID == row.ConversationID {
			return false
		}
	}
	if transfer.PID == 0 {
		return transfer.Conversation != "" && nativeNoticeTarget(row)
	}
	if b.ProcessStart == nil || row.PID <= 0 || row.PID != transfer.PID || transfer.ProcessStart == 0 {
		return false
	}
	started := b.ProcessStart(row.PID)
	return !started.IsZero() && started.UnixNano() == transfer.ProcessStart
}

// tendHandoffBoards waits until the receiver has a conversation id. A typed
// handoff line alone is not evidence that an assistant received it. The
// handoff marker and every Board ownership change commit in one transaction;
// a later pass can therefore retry without transferring an item twice.
func (b *Broker) tendHandoffBoards(ctx context.Context, rd reading) int {
	rows, err := b.Store.PendingBoardHandoffs(ctx, handoffListLimit)
	if err != nil {
		log.Printf("broker: read delivered handoffs for Board transfer: %v", err)
		return 0
	}
	transferred := 0
	for _, row := range rows {
		h, err := decodeHandoff(row)
		if err != nil || h.Opened == nil {
			continue
		}
		receiver, found := rd.session(h.Opened.TerminalID)
		if !found || !receiver.IsAssistant() || receiver.ConversationID == "" || !receiver.Activity.Known() {
			continue
		}
		if (len(h.BoardItems) == 0 && h.SessionBoards == nil) || h.BoardTransferredAt != 0 {
			// A milestone handoff with no Board items still names its
			// receiver, so the ledger can join the two Sessions.
			if h.Milestone && h.Receiver == "" && receiver.ConversationID != h.FromSession {
				if _, err := b.updateHandoff(ctx, h.ID, "handoff.receiver_bound", func(x *Handoff) {
					x.Receiver = receiver.ConversationID
				}); err != nil {
					log.Printf("broker: record the receiver of handoff %s: %v", h.ID, err)
				}
			}
			continue
		}
		if h.SessionBoards != nil && !b.isHandoffSessionBoardReceiver(h, receiver, rd) {
			continue
		}
		if err := b.transferHandoffBoard(ctx, h, receiver); err != nil {
			log.Printf("broker: Board transfer for handoff %s: %v", h.ID, err)
			continue
		}
		transferred++
	}
	return transferred
}

func (b *Broker) transferHandoffBoard(ctx context.Context, h Handoff, row session.Session) error {
	receiver := row.ConversationID
	if receiver == "" || h.Opened == nil || receiver == h.FromSession {
		return errors.New("handoff receiver is not a distinct bound Session")
	}
	now := b.now()
	return b.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		_, err := tx.UpdateOpened(store.TableHandoffs, h.ID, func(o store.Opened) (*store.Opened, []store.Event, error) {
			current, err := decodeHandoff(o)
			if err != nil || current.State != HandoffDelivered || current.BoardTransferredAt != 0 {
				return nil, nil, err
			}
			for _, id := range current.BoardItems {
				item, err := tx.Item(id)
				if errors.Is(err, store.ErrNoWorkV2) {
					continue
				}
				if err != nil {
					return nil, nil, err
				}
				if item.OwnerSession != current.FromSession || item.Phase.Terminal() ||
					filepath.Clean(item.ProjectPath) != filepath.Clean(current.ProjectDir) {
					continue
				}
				old, err := tx.ActiveAssignment(id)
				if err != nil {
					return nil, nil, err
				}
				if old.ID == "" || old.SessionID != current.FromSession {
					continue
				}
				pending, err := tx.PendingAssignment(id)
				if err != nil {
					return nil, nil, err
				}
				if pending.ID != "" {
					// A separate reassignment is already in flight. Its
					// completion owns the next owner decision.
					continue
				}
				old.State, old.ReleasedAt, old.UpdatedAt = "released", now, now
				if err := tx.UpdateAssignment(old); err != nil {
					return nil, nil, err
				}
				nextAssignment := work.AssignmentV2{
					ID: NewUUID(), WorkID: id, Mode: "existing_session", SessionID: receiver,
					TerminalID: current.Opened.TerminalID, Assistant: current.Assistant, Model: current.Model,
					State: "active", HumanActor: "handoff:" + current.ID, CreatedAt: now, UpdatedAt: now,
					GatePreviewed: old.GatePreviewed, PlanningGate: old.PlanningGate,
					VerifyGate: old.VerifyGate, CycleBaseCommit: old.CycleBaseCommit,
				}
				if err := tx.CreateAssignment(nextAssignment); err != nil {
					return nil, nil, err
				}
				next := item
				next.OwnerSession, next.UpdatedAt = receiver, now
				if next.VerifyGate && (next.Phase == work.PhaseVerifying || next.Phase == work.PhaseMerging) {
					next.Phase = work.PhaseImplementing
					if err := tx.InvalidateWorkV2VerificationAuthorization(item, next, "handoff"); err != nil {
						return nil, nil, err
					}
				}
				payload, _ := json.Marshal(map[string]string{"handoff_id": current.ID,
					"from_session": current.FromSession, "to_session": receiver,
					"assignment_id": nextAssignment.ID})
				if err := tx.HandOverItem(item, next, "assignment.handed_off", "handoff:"+current.ID, string(payload)); err != nil {
					return nil, nil, err
				}
			}
			if transfer := current.SessionBoards; transfer != nil {
				label := row.Label
				if label == "" {
					label = string(row.Assistant)
				}
				actor := sb.Actor{ID: string(row.Assistant) + ":" + receiver, Label: label, Persona: row.Persona}
				if err := tx.TransferSessionBoards(transfer.Project, transfer.From, actor, transfer.Boards); err != nil {
					return nil, nil, err
				}
			}
			current.BoardTransferredAt = now.Unix()
			current.Receiver = receiver
			body, err := json.Marshal(current)
			if err != nil {
				return nil, nil, err
			}
			payload, _ := json.Marshal(map[string]string{"handoff": current.ID, "receiver": receiver})
			return &store.Opened{ID: o.ID, State: o.State, Record: body, CreatedAt: o.CreatedAt, UpdatedAt: now},
				[]store.Event{{Kind: "handoff.board_transferred", Subject: o.ID, Payload: payload}}, nil
		})
		return err
	})
}
