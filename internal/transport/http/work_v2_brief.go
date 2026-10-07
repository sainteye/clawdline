package http

import (
	"context"
	"fmt"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/work"
	"strings"
)

func workV2PhaseInstruction(id string) string {
	return "Move the item through its phases yourself as the work happens: `clawdline item phase " + id +
		" implementing`, then verifying, merging (--verification), deploying (--commit --target --remote) and done " +
		"(--deployment or --no-deployment-reason); once the work has landed, `clawdline item finish " + id +
		"` takes it the rest of the way in one command. `clawdline guide board` says what each one needs."
}

// workV2StepsInstruction says when an owner breaks its item into steps, in
// every brief an owner receives: the daemon already took steps from their
// owner, yet no brief or command named that, so complex work was never broken
// down unless the person wrote the list themselves; and a simple change must
// not grow a ceremony of steps (2026-09-26).
func workV2StepsInstruction(id string) string {
	return "If this item has no steps and the work is multi-stage — several changes verified separately, or more than " +
		"one part of the system — first break it into its ordered steps with `clawdline item step-add " + id +
		" \"…\" \"…\"` and complete each with `clawdline item step-done` once it is verified; a single straightforward " +
		"change takes no steps."
}

// workV2EpicInstruction is the procedure an Epic's owner follows before it
// may enter implementing (work.EpicPlanGate): an Epic is large, so its plan is
// written onto the item and a child reviews it before code is written. The
// daemon refuses `phase implementing` until both are recorded.
func workV2EpicInstruction(id string) string {
	return "This is an Epic: before any code, (1) plan it carefully and write the plan onto the item with " +
		"`clawdline item doc " + id + " --role plan --title \"Plan\" --body-file <file>`; (2) dispatch a read-only child " +
		"to review the plan critically with `clawdline dispatch --kind plan_review --work-id " + id + " --title \"Review the plan\" --claims \"\"`; " +
		"(3) record its review with `clawdline item doc " + id + " --role plan_review --title \"Plan review\" " +
		"--reference <task id> --body-file <file>` — what it found and what the plan changed — and if it found real " +
		"problems, revise the plan (a new plan document) and have that reviewed again, at most twice in all; (4) break the work into steps with " +
		"`clawdline item step-add " + id + "`; (5) only then `clawdline item phase " + id + " implementing`, which the " +
		"daemon refuses until a review newer than the latest plan, or a second review, is recorded. Plan verification in order: " +
		"each implementation child uses focused tests for its own changes; the Epic owner then integrates all affected components " +
		"into one runnable candidate and checks the required accounts, data, browser and API paths. Only after that candidate " +
		"works should you dispatch a real end-to-end test. Do not send an end-to-end verifier to a mock UI, disconnected " +
		"branches or incomplete APIs; resolve setup failures with the implementation owner and rerun the affected paths before " +
		"the final end-to-end round. Before browser-based verification, prove the chosen worker can open the target URL with " +
		"an authorized browser or equivalent local automation, and has the required test accounts, fixtures and app/origin " +
		"permissions. Name that browser route in the brief; `--permission-mode full` alone does not grant browser access. " +
		"If the preflight fails, fix access or choose an equivalent harness before dispatching; this is not an end-to-end attempt. " +
		"Plan one comprehensive end-to-end round per Epic, not one per child or revision. After a defect fix, rerun only the " +
		"affected scenarios; repeat the comprehensive round only when the acceptance scope or integration boundary materially " +
		"changes, and record why. Do not repeatedly send a verifier into the same blocker. During planning, decide whether " +
		"the Epic changes a human-facing interface, user journey, or product policy. If it does, before merging dispatch an independent " +
		"read-only UX/product reviewer with `clawdline dispatch --kind review --work-id " + id + " --title \"UX review\" --claims \"\" --persona ux-architect`; " +
		"brief it to inspect the integrated desktop and mobile experience, accessibility, workflow, and product fit, require evidence for " +
		"each finding and tell it to mark unverified and say why when evidence is unavailable, then resolve every blocking finding. If the " +
		"Epic has no such impact, record why in the plan instead of adding review ceremony. This specialist review complements rather than " +
		"replaces the verification gate. `clawdline guide epic` has the whole procedure."
}

// workV2EpicChildrenInstruction is the exception an Epic's owner holds to
// "do not create Board items" (work-system-v2 §6.5): once its reviewed plan
// has taken it into implementing, it may break the Epic into Feature and
// Issue items and hand them to other Sessions, and it stays responsible for
// the whole. The daemon refuses a child before implementing
// (epic_not_planned) and the Epic's done while a child is open
// (epic_children_open).
func workV2EpicChildrenInstruction(id string) string {
	return "After the reviewed plan, when parts of this Epic are better done by other Sessions, you may break it into " +
		"Feature and Issue items and assign them: `clawdline item child " + id + " --kind feature|issue --title \"…\" " +
		"[--step \"…\"]… --description-file <file> [--assign-terminal <terminal id> | --assign-new]` (terminal ids are in the " +
		"session address book, `clawdline guide send`; without an --assign flag the item waits for the person), and " +
		"`clawdline item assign <child id> --terminal <id> | --new` to hand one to another Session. You remain responsible " +
		"for the Epic: follow every child to done, integrate their work, and move the Epic to done only when all its " +
		"children are closed — the daemon refuses done while one is open. For each independently opened Feature Root, " +
		"also track its Session after Board completion: identify it by the Epic parent relation, ask its owner to audit " +
		"closeability and finish its own obligations, then verify that the Session was closed or record the exact blocker " +
		"and next owner. Once the exact Session is idle, its Board items and to-dos are complete, " +
		"and closeability is safe, use the supported Session close action and confirm it vanished from fresh inventory. " +
		"A Session report is not a close; do not force-close unknown or blocked work or use a raw terminal action. " +
		"Create no other Board items."
}

// workV2CreateRule is what an owner is told about creating Board items: an
// Epic's owner may create the Epic's children; every other owner creates none.
func workV2CreateRule(id string, kind work.Kind) string {
	if kind == work.KindEpic {
		return workV2EpicChildrenInstruction(id)
	}
	return "Do not create Board items."
}

// workV2RootConstraints is the Constraints of the Root Assignment a new
// Session is opened with for an item.
func workV2RootConstraints(id string, kind work.Kind) string {
	return "Own only this Board item. " + workV2CreateRule(id, kind)
}

// workV2ParentNote is what the owner of an Epic's child is told about the
// Epic it belongs to: the Epic's owner broke it out and follows it to done.
func workV2ParentNote(parent work.ItemV2) string {
	return fmt.Sprintf("This item is part of Epic %s: %s; that Epic's owner Session created it and follows it to done, "+
		"so keep its phase and steps current.", parent.ID, parent.Title)
}

func workV2FeatureInstruction(item work.ItemV2) string {
	id := item.ID
	// A Refactor follows a Feature's planning rules; only the noun differs.
	noun := "Feature"
	if item.Kind == work.KindRefactor {
		noun = "Refactor"
	}
	if !item.ReviewRequired {
		return "This " + noun + "'s captured planning gate is on, and the person has not checked Needs independent review. " +
			"Concise acceptance criteria and focused tests suffice; move to implementing without writing a plan for review " +
			"and do not dispatch a plan_review child. Do not record a risk assessment: whether a Feature is reviewed is the " +
			"person's switch, read when the item asks to enter implementing. If the person checks it before then, the phase " +
			"route asks for a plan and its review."
	}
	return "This " + noun + "'s captured planning gate is on, and the person checked Needs independent review. " +
		"Write a plan with `clawdline item doc " + id + " --role plan --title \"Plan\"`, have an independent `plan_review` " +
		"child review it, and record the review before moving to implementing. " +
		"After revising a reviewed plan, record an `other` document titled `Review boundary assessment` with " +
		"new_risk_boundary=false and a reason only when the change stays within the reviewed boundary; otherwise request a focused fresh review."
}

// workV2KindSteps is the planning/steps instruction captured for this cycle.
func workV2KindSteps(item work.ItemV2) string {
	if item.Kind == work.KindEpic && item.PlanningGate {
		return workV2EpicInstruction(item.ID)
	}
	if item.Kind.FeatureLike() && item.PlanningGate {
		return workV2FeatureInstruction(item) + " " + workV2StepsInstruction(item.ID)
	}
	if (item.Kind == work.KindEpic || item.Kind.FeatureLike()) && item.HasGateSnapshot() && !item.PlanningGate {
		return "This cycle captured planning_gate off, so no planning document or independent review is forced before implementing. " +
			workV2StepsInstruction(item.ID)
	}
	return workV2StepsInstruction(item.ID)
}

func workV2GateModeInstruction(item work.ItemV2) string {
	if !item.HasGateSnapshot() {
		return "The gate mode is captured only when assignment succeeds."
	}
	return fmt.Sprintf("This cycle captured planning_gate=%t and verify_gate=%t; later global setting changes do not alter it.",
		item.PlanningGate, item.VerifyGate)
}

func workV2AcceptanceBrief(item work.ItemV2) string {
	if item.AcceptanceCriteria == "" {
		return ""
	}
	return "Acceptance criteria:\n" + item.AcceptanceCriteria + "\n\n"
}

func workV2AgentAcceptanceInstruction(item work.ItemV2) string {
	if strings.TrimSpace(item.AcceptanceCriteria) != "" || !item.GateNeedsAcceptance() {
		return ""
	}
	return "The person does not need to fill acceptance criteria. Write observable Markdown criteria " +
		"from the item goal with `clawdline item acceptance " + item.ID + " --body-file <file>` before crossing its gate. "
}

// workV2AssignmentBrief is what an existing Session is sent when it is
// assigned an item.
func workV2AssignmentBriefForItem(item work.ItemV2) string {
	return workV2AcceptanceBrief(item) + fmt.Sprintf("Clawdline assigned you Board item %s: %s. Read its description and reference images, then own it through implementation, verification, Merge, and deployment. Use the work-system v2 Agent API to update it. %s %s %s %s %s", item.ID, item.Title,
		workV2CreateRule(item.ID, item.Kind), workV2GateModeInstruction(item)+" "+workV2AgentAcceptanceInstruction(item), workV2KindSteps(item),
		workV2PhaseInstruction(item.ID), workV2CompletionReportInstruction)
}

// workV2ReassignmentBrief is what an existing Session is sent when the person
// moves an item to it from another Session: the item is mid-flight, so it is
// told to continue from what is recorded rather than start over.
func workV2ReassignmentBriefForItem(item work.ItemV2) string {
	return workV2AcceptanceBrief(item) + fmt.Sprintf("Clawdline reassigned Board item %s: %s to you. %s %s %s %s %s %s", item.ID, item.Title,
		workV2TakeoverNote(item.Phase), workV2CreateRule(item.ID, item.Kind), workV2GateModeInstruction(item)+" "+workV2AgentAcceptanceInstruction(item),
		workV2KindSteps(item), workV2PhaseInstruction(item.ID),
		workV2CompletionReportInstruction)
}

// workV2TakeoverNote says what a Session taking an item over from another one
// must read first. The previous Session is not named by id: it may have run
// out of tokens or been closed, and the item's record is what carries over.
func workV2TakeoverNote(phase work.Phase) string {
	return fmt.Sprintf("Another Session owned it before and no longer does; the item is in phase %s, and its steps, "+
		"documents and history are kept. Read them, and look for work the previous Session left in this Project "+
		"(its branch or worktree) before starting over, then continue from where it stopped.", phase)
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
func workV2ReleasedNotice(id, title string) string {
	return fmt.Sprintf("Clawdline moved Board item %s: %s from this Session to another Session. It is no longer yours: "+
		"do not change its phase, steps or documents, and do not land it. If you have uncommitted work for it, commit it "+
		"on your branch so the new owner can find it; then stop working on this item.", id, title)
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
	s.tellFormerOwner(ctx, previous, workV2ReleasedNotice(id, title))
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
func workV2RootAssignmentAcceptanceForItem(item work.ItemV2) string {
	process := "After reading the objective and scope, choose a short Session name that states your actual task, " +
		"then run `clawdline item name " + item.ID + " \"<task name>\"` once. This changes your Session's name, " +
		"not the Board item's title. Implement, verify, merge, and deploy according to the item's deployment policy. " +
		workV2GateModeInstruction(item) + " " + workV2AgentAcceptanceInstruction(item) + workV2KindSteps(item) + " " +
		workV2PhaseInstruction(item.ID) + " " + workV2CompletionReportInstruction
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

const workV2CompletionReportInstruction = "When substantial investigation was needed to find a non-obvious cause, add a user-readable completion_report before done; a straightforward fix does not require one."

// repo is the repository the commit is looked for in: the item's Project, or
// the catalog Project the request named, resolved by the caller.
