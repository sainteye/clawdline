package http

import (
	"context"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/work"
	"github.com/sainteye/clawdline/internal/productcopy"
)

func workV2PhaseInstruction(id string, language ...string) string {
	return workV2Instruction(language, "phase", map[string]string{"id": id})
}

// workV2StepsInstruction says when an owner breaks its item into steps, in
// every brief an owner receives: the daemon already took steps from their
// owner, yet no brief or command named that, so complex work was never broken
// down unless the person wrote the list themselves; and a simple change must
// not grow a ceremony of steps (2026-09-26).
func workV2StepsInstruction(id string, language ...string) string {
	return workV2Instruction(language, "steps", map[string]string{"id": id})
}

// workV2EpicInstruction is the procedure an Epic's owner follows before it
// may enter implementing (work.PlanningGate): an Epic is large, so its plan is
// written onto the item and a child reviews it before code is written. The
// daemon refuses `phase implementing` until both are recorded.
func workV2EpicInstruction(id string, language ...string) string {
	return workV2Instruction(language, "epic", map[string]string{"id": id})
}

// workV2EpicChildrenInstruction is the exception an Epic's owner holds to
// "do not create Board items" (work-system-v2 §6.5): once its reviewed plan
// has taken it into implementing, it may break the Epic into Feature and
// Issue items and hand them to other Sessions, and it stays responsible for
// the whole. The daemon refuses a child before implementing
// (epic_not_planned) and the Epic's done while a child is open
// (epic_children_open).
func workV2EpicChildrenInstruction(id string, language ...string) string {
	return workV2Instruction(language, "children", map[string]string{"id": id})
}

// workV2CreateRule is what an owner is told about creating Board items: an
// Epic's owner may create the Epic's children; every other owner creates none.
func workV2CreateRule(id string, kind work.Kind, language ...string) string {
	if kind == work.KindEpic {
		return workV2EpicChildrenInstruction(id, language...)
	}
	return workV2Instruction(language, "no_create", nil)
}

// workV2RootConstraints is the Constraints of the Root Assignment a new
// Session is opened with for an item.
func workV2RootConstraints(id string, kind work.Kind, language ...string) string {
	return workV2Instruction(language, "own", nil) + workV2CreateRule(id, kind, language...)
}

// workV2ParentNote is what the owner of an Epic's child is told about the
// Epic it belongs to: the Epic's owner broke it out and follows it to done.
func workV2ParentNote(parent work.ItemV2, language ...string) string {
	return workV2Instruction(language, "parent", map[string]string{"id": parent.ID, "title": parent.Title})
}

func workV2FeatureInstruction(item work.ItemV2, language ...string) string {
	noun := "Feature"
	if item.Kind == work.KindRefactor {
		noun = "Refactor"
	}
	key := "feature_simple"
	if item.ReviewRequired {
		key = "feature_review"
	}
	return workV2Instruction(language, key, map[string]string{"id": item.ID, "kind": noun})
}

// workV2KindSteps is the planning/steps instruction captured for this cycle.
func workV2KindSteps(item work.ItemV2, language ...string) string {
	if item.Kind == work.KindEpic && item.PlanningGate {
		return workV2EpicInstruction(item.ID, language...)
	}
	if item.Kind.FeatureLike() && item.PlanningGate {
		return workV2FeatureInstruction(item, language...) + " " + workV2StepsInstruction(item.ID, language...)
	}
	if (item.Kind == work.KindEpic || item.Kind.FeatureLike()) && item.HasGateSnapshot() && !item.PlanningGate {
		return workV2Instruction(language, "planning_off", nil) + workV2StepsInstruction(item.ID, language...)
	}
	return workV2StepsInstruction(item.ID, language...)
}

func workV2GateModeInstruction(item work.ItemV2, language ...string) string {
	if !item.HasGateSnapshot() {
		return workV2Instruction(language, "gate_pending", nil)
	}
	return workV2Instruction(language, "gate", map[string]string{"planning": strconv.FormatBool(item.PlanningGate), "verify": strconv.FormatBool(item.VerifyGate)})
}

func workV2AcceptanceBrief(item work.ItemV2, language ...string) string {
	if item.AcceptanceCriteria == "" {
		return ""
	}
	return workV2Instruction(language, "acceptance", nil) + item.AcceptanceCriteria + "\n\n"
}

func workV2AgentAcceptanceInstruction(item work.ItemV2, language ...string) string {
	if strings.TrimSpace(item.AcceptanceCriteria) != "" || !item.GateNeedsAcceptance() {
		return ""
	}
	return workV2Instruction(language, "write_acceptance", map[string]string{"id": item.ID})
}

// workV2AssignmentBrief is what an existing Session is sent when it is
// assigned an item.
func workV2AssignmentBriefForItem(item work.ItemV2, language ...string) string {
	return workV2LanguageInstruction(language) + workV2AcceptanceBrief(item, language...) +
		workV2Instruction(language, "assign", map[string]string{"id": item.ID, "title": item.Title}) + " " +
		workV2CreateRule(item.ID, item.Kind, language...) + " " + workV2GateModeInstruction(item, language...) + " " +
		workV2AgentAcceptanceInstruction(item, language...) + workV2KindSteps(item, language...) + " " +
		workV2PhaseInstruction(item.ID, language...) + " " + workV2Instruction(language, "completion", nil)
}

// workV2ReassignmentBrief is what an existing Session is sent when the person
// moves an item to it from another Session: the item is mid-flight, so it is
// told to continue from what is recorded rather than start over.
func workV2ReassignmentBriefForItem(item work.ItemV2, language ...string) string {
	return workV2LanguageInstruction(language) + workV2AcceptanceBrief(item, language...) +
		workV2Instruction(language, "reassign", map[string]string{"id": item.ID, "title": item.Title}) + " " +
		workV2TakeoverNote(item.Phase, language...) + " " + workV2CreateRule(item.ID, item.Kind, language...) + " " +
		workV2GateModeInstruction(item, language...) + " " + workV2AgentAcceptanceInstruction(item, language...) +
		workV2KindSteps(item, language...) + " " + workV2PhaseInstruction(item.ID, language...) + " " + workV2Instruction(language, "completion", nil)
}

// workV2TakeoverNote says what a Session taking an item over from another one
// must read first. The previous Session is not named by id: it may have run
// out of tokens or been closed, and the item's record is what carries over.
func workV2TakeoverNote(phase work.Phase, language ...string) string {
	return workV2Instruction(language, "takeover", map[string]string{"phase": string(phase)})
}

// workV2HandoffInput is what the Board adds to the handoff pack when an
// in-flight item (implementing through deploying) is taken over by a new
// Session; nil otherwise. It is read from the store and the previous owner's
// transcript file only: the previous owner is never asked and never waited
// on, so an owner stopped by its quota or gone altogether changes nothing
// but what the pack can say about its last message.
func (s *Server) workV2HandoffInput(ctx context.Context, item app.WorkV2View, previous work.AssignmentV2) *orchestrator.HandoffInput {
	switch item.Item.Phase {
	case work.PhaseImplementing, work.PhaseVerifying, work.PhaseMerging, work.PhaseDeploying:
	default:
		return nil
	}
	if previous.ID == "" {
		return nil
	}
	in := &orchestrator.HandoffInput{ItemID: item.Item.ID, Title: item.Item.Title, Phase: string(item.Item.Phase),
		PreviousSession: previous.SessionID}
	for _, step := range item.Steps {
		if !step.Done {
			in.OpenSteps = append(in.OpenSteps, step.Title)
		}
	}
	in.LastMessage, in.LastMessageUnread = s.lastAssistantMessage(ctx, previous)
	return in
}

// lastAssistantMessage is the previous owner's last assistant message, or why
// it could not be read.
func (s *Server) lastAssistantMessage(ctx context.Context, previous work.AssignmentV2) (string, string) {
	if previous.TerminalID == "" {
		return "", "the assignment names no terminal"
	}
	sess, err := s.actions().Find(ctx, previous.TerminalID)
	if err != nil {
		return "", "its terminal " + previous.TerminalID + " is gone: " + err.Error()
	}
	if sess.ConversationID != previous.SessionID {
		return "", "its terminal " + previous.TerminalID + " now holds another conversation"
	}
	page, err := s.sessionTail(sess)
	if err != nil {
		return "", "its transcript could not be read: " + err.Error()
	}
	last := ""
	for _, entry := range page.Entries {
		if entry.Kind == transcript.KindAssistant && strings.TrimSpace(entry.Text) != "" {
			last = strings.TrimSpace(entry.Text)
		}
	}
	if last == "" {
		return "", "its transcript tail holds no assistant message"
	}
	return last, ""
}

// workV2ReleasedNotice is what the Session an item was moved away from is
// sent, so a Session that is still running stops working on it.
func workV2ReleasedNotice(id, title string, language ...string) string {
	return workV2Instruction(language, "released", map[string]string{"id": id, "title": title})
}

// workV2ActiveOwner is the item's active assignment held by its owner, or the
// zero assignment when nobody holds it.
func workV2ActiveOwner(item app.WorkV2View) work.AssignmentV2 {
	for _, a := range item.Assignments {
		if a.State == "active" && a.SessionID != "" && a.SessionID == item.Item.OwnerSession {
			return a
		}
	}
	return work.AssignmentV2{}
}

// tellReleasedOwner sends the courtesy notice to the Session an item was just
// moved away from. The assignment record is the durable fact; the notice is
// typed only when the terminal still holds that conversation and reads idle,
// so a recycled terminal never receives another Session's work.
func (s *Server) tellReleasedOwner(ctx context.Context, previous work.AssignmentV2, id, title string) {
	s.tellFormerOwner(ctx, previous, workV2ReleasedNotice(id, title, s.workV2AgentLanguage()))
}

func (s *Server) tellFormerOwner(ctx context.Context, previous work.AssignmentV2, notice string) {
	if previous.TerminalID == "" {
		return
	}
	sess, err := s.actions().Find(ctx, previous.TerminalID)
	if err != nil || sess.ConversationID != previous.SessionID {
		return
	}
	_, _ = s.actions().SendIfIdle(ctx, sess.ID, notice)
}

// workV2RootAssignmentAcceptance is the acceptance of the Root Assignment a
// new Session is opened with for an item.
func workV2RootAssignmentAcceptanceForItem(item work.ItemV2, language ...string) string {
	process := workV2LanguageInstruction(language) + workV2Instruction(language, "root_process", map[string]string{"id": item.ID}) +
		workV2GateModeInstruction(item, language...) + " " + workV2AgentAcceptanceInstruction(item, language...) +
		workV2KindSteps(item, language...) + " " + workV2PhaseInstruction(item.ID, language...) + " " + workV2Instruction(language, "completion", nil)
	if item.AcceptanceCriteria == "" {
		return process
	}
	return item.AcceptanceCriteria + "\n\n" + process
}

// Compatibility composers preserve the pre-snapshot helper surface used by
// focused instruction tests. Runtime assignment paths use the item-aware
// forms above so acceptance and the captured mode are exact.
func legacyWorkV2BriefItem(id, title string, kind work.Kind, phase work.Phase) work.ItemV2 {
	return work.ItemV2{ID: id, Title: title, Kind: kind, Phase: phase, Cycle: 1, GateSnapshotCycle: 1,
		PlanningGate: kind == work.KindEpic}
}

func workV2AssignmentBrief(id, title string, kind work.Kind) string {
	return workV2AssignmentBriefForItem(legacyWorkV2BriefItem(id, title, kind, work.PhaseAssigned))
}

func workV2ReassignmentBrief(id, title string, kind work.Kind, phase work.Phase) string {
	return workV2ReassignmentBriefForItem(legacyWorkV2BriefItem(id, title, kind, phase))
}

func workV2RootAssignmentAcceptance(id string, kind work.Kind) string {
	return workV2RootAssignmentAcceptanceForItem(legacyWorkV2BriefItem(id, "", kind, work.PhaseAssigned))
}

// repo is the repository the commit is looked for in: the item's Project, or
// the catalog Project the request named, resolved by the caller.

// A missing language keeps compatibility composers in English. Runtime paths
// read the daemon's Agent preference, independently of browser/product language.
func workV2Instruction(language []string, key string, values map[string]string) string {
	tag := "en"
	if len(language) != 0 {
		tag = language[0]
	}
	return productcopy.BoardInstruction(tag, key, values)
}

func (s *Server) workV2AgentLanguage() string {
	if s.broker == nil {
		return "en"
	}
	return s.broker.DisplayLanguage()
}

func workV2LanguageInstruction(language []string) string {
	if len(language) == 0 {
		return ""
	}
	tag := projects.CodexBoardLanguage(language[0])
	if tag == "" {
		return ""
	}
	return workV2Instruction(language, "language", map[string]string{"language": tag}) + "\n\n"
}

func workV2ReminderBrief(item work.ItemV2, language string) string {
	return workV2LanguageInstruction([]string{language}) + productcopy.BoardInstruction(language, "remind", map[string]string{"id": item.ID, "title": item.Title})
}
