import Foundation
#if canImport(ClawdlineApplication)
import ClawdlineApplication // W3-1 correction: real cross-module import, see Sources/HostPorts.swift
#endif

// The shape of a Root Assignment: the states it moves through, the request that opens one, the
// executor identity it is bound to, and the pure decisions taken from those values — trust, the
// next step, which rows may be cleaned up, and what an audit receipt says. `OrchestratorTaskShape`
// is the same file for a `Task`; this is its opposite number for the other kind of session the
// broker keeps records about.
//
// Chosen by measuring rather than by `// MARK:`. On the base this was cut from, this block
// referenced no `private` symbol left behind and acquired `Orchestrator.lock` zero times, which
// is not true of the `// MARK: - Independent feature roots` heading it used to sit under: the
// forty-one lines that remain there — `reportRootAssignmentTransition` and `cwd(of:)` — take
// the lock twice and read `private static var rootAssignments`, so they stayed.
//
// It moves as an `extension`, which renames nothing. 84 branches on this machine have a base that
// predates this cut and a commit touching `Orchestrator.swift`; a new `enum` would have renamed
// every spelling here and each of those 84 would have merged cleanly and then failed to compile.
extension Orchestrator {

    enum RootAssignmentState: String {
        case accepted, terminalOpened = "terminal_opened", promptReady = "prompt_ready"
        case blocked, briefed, active, failed, inactive
    }

    struct RootAssignmentDraft: Equatable {
        let requestID: String
        let assistant: Assistant
        /// `default` is deliberately a value rather than an omitted option: model selection is
        /// part of the caller's closed request even when it delegates the concrete model.
        let model, projectDir, label: String
        let objective, scope, constraints, relevantReferences, acceptance: String
    }

    /// Resolved once at acceptance; both values are stored to keep briefing bytes stable.
    struct RootAssignmentLanguage: Equatable { let tag, name: String }
    enum RootAssignmentDraftOutcome: Equatable {
        case ok(RootAssignmentDraft), bad(String)
        var isBad: Bool { if case .bad = self { return true }; return false }
    }

    struct RootAssignmentIdentity: Equatable {
        var terminalID: String
        var assistant: Assistant
        var tty: String?
        var pid: Int32?
        /// Seconds since the Unix epoch. Keeping the wire-sized scalar here makes equality exact.
        var processStart: Double?
        var conversationID: String?
    }

    enum RootAssignmentReconciliation: Equatable {
        case wait(String), rebind(RootAssignmentIdentity)
        case fail(String), inactive(String)
    }

    enum RootAssignmentTrustDecision: Equatable {
        case none, block, accept(row: Int)
    }

    static func rootAssignmentTrustDecision(projectApproved: Bool,
                                            menu: SessionState.Menu?,
                                            answeredTrustMenu: Bool = false)
        -> RootAssignmentTrustDecision {
        guard let menu,
              let choices = SessionClosePolicy.startupTrustChoices(in: menu) else { return .none }
        guard projectApproved else { return .block }
        return answeredTrustMenu ? .none : .accept(row: choices.accept)
    }

    /// No retry delay: a Root Assignment is typed at most once, so nothing about the record, the
    /// terminal or the clock can license a second send (see the step decision). `observed` and
    /// `sendFailed` only name the failure a deadline writes.
    struct RootAssignmentDeliveryEvidence: Equatable {
        let recorded: Bool
        /// When the assistant's own record says that user turn happened — the delivery itself,
        /// never the beat that got round to reading it. `nil` is a record carrying the turn
        /// without a timestamp: a delivery whose moment is simply unknown.
        let recordedAt: Date?
        /// The end of the pre-brief window this delivery had to land inside, kept beside the
        /// event so the comparison cannot quietly become "when did the broker look".
        let deadline: Date?
        /// This beat read a candidate record of the conversation, receipt or not. A receipt is
        /// itself an observation.
        let observed: Bool
        /// The terminal reported that typing the one counted attempt failed (`injectFailure`).
        let sendFailed: Bool

        init(recorded: Bool, recordedAt: Date? = nil, deadline: Date? = nil,
             observed: Bool = false, sendFailed: Bool = false) {
            self.recorded = recorded; self.recordedAt = recordedAt; self.deadline = deadline
            self.observed = observed || recorded; self.sendFailed = sendFailed
        }

        /// A prompt that reached the assistant inside the window, however late it was observed.
        /// An undated receipt counts: the record carries the turn, and refusing it would restore
        /// the false negative this field exists to remove.
        var deliveredInWindow: Bool {
            guard recorded else { return false }
            guard let recordedAt, let deadline else { return true }
            return recordedAt <= deadline
        }
    }

    /// Where this record's pre-brief window opened, and whether `now` is past its deadline. One
    /// reading for the lifecycle half of a beat and its delivery half, so the two cannot disagree.
    static func rootAssignmentPromptWindow(_ assignment: RootAssignment, now: Date)
        -> (openedAt: Date, timedOut: Bool) {
        let openedAt = rootAssignmentPromptTimeoutAnchor(
            terminalOpenedAt: assignment.terminalOpenedAt ?? assignment.created,
            trustResumedAt: assignment.promptTimeoutStartedAt)
        return (openedAt, rootAssignmentPromptTimedOut(
            state: assignment.state, openedAt: openedAt, now: now,
            briefed: assignment.briefedAt != nil))
    }

    enum RootAssignmentStepDecision: Equatable {
        case wait, activate, block, promptReady, inspectDelivery, briefed, inject, answerTrust(row: Int)
        case fail(String)
    }

    /// The lifecycle choice is pure; terminal capture, transcript reads, persistence and typing
    /// happen only after this answer. Keeping the whole branch table here makes timeout, trust,
    /// receipt, the single send and the typed deadline failures executable without opening
    /// somebody's terminal.
    static func rootAssignmentStepDecision(
        state: RootAssignmentState, promptTimedOut: Bool,
        trust: RootAssignmentTrustDecision, answeredTrustMenu: Bool,
        inputReady: Bool, delivery: RootAssignmentDeliveryEvidence?, injectAttempts: Int
    ) -> RootAssignmentStepDecision {
        guard ![.accepted, .failed, .inactive, .active].contains(state) else { return .wait }
        if state == .briefed { return .activate }
        switch trust {
        case .block: return .block
        case .accept(let row): return answeredTrustMenu ? .wait : .answerTrust(row: row)
        case .none: break
        }
        if state == .promptReady {
            // Once a prompt has been sent, its exact transcript receipt is authoritative even if
            // the assistant is now working and the broker first observes it after the deadline.
            // Composer readiness decides only when the one send may happen; it never gates
            // observation.
            guard let delivery else { return .inspectDelivery }
            if delivery.deliveredInWindow { return .briefed }
            if promptTimedOut {
                return .fail(rootAssignmentDeadlineFailure(delivery,
                                                           injectAttempts: injectAttempts))
            }
            // At most one send. A missing receipt cannot tell a prompt that never arrived from one
            // that arrived unrecognised, and an empty composer is exactly what a Root that has
            // finished its first turn looks like: 8cd9479d's Root ended that turn at 07:33:32.122Z
            // and was typed the same briefing again 1.5 seconds later. Once an attempt is counted,
            // only a receipt or the deadline moves this record — a send the terminal refused
            // included, because the refusal can follow text or an Enter that already reached the
            // tab, and a failed record could no longer take the receipt that proves it did.
            guard injectAttempts == 0 else { return .wait }
            return inputReady ? .inject : .wait
        }
        if promptTimedOut { return .fail("prompt_timeout") }
        guard inputReady else { return .wait }
        if state == .terminalOpened || state == .blocked { return .promptReady }
        return .wait
    }

    /// The typed failure a prompt-ready record takes at its deadline: what the broker knows about
    /// its one attempt, not only that time ran out. Nothing typed, or a turn recorded after the
    /// window closed, is `prompt_timeout`. A counted attempt the terminal refused is
    /// `delivery_failed`. An unrefused one is `delivery_unconfirmed` when this beat read the
    /// conversation's record and it holds no receipt — which still cannot tell a turn never
    /// recorded from one recorded where the broker did not read — and `delivery_unobserved` when
    /// no record could be read. A crash between the durable count and the keystrokes also leaves
    /// an unrefused attempt, so it ends in one of those two.
    static func rootAssignmentDeadlineFailure(_ delivery: RootAssignmentDeliveryEvidence,
                                              injectAttempts: Int) -> String {
        guard injectAttempts > 0, !delivery.recorded else { return "prompt_timeout" }
        if delivery.sendFailed { return "delivery_failed" }
        return delivery.observed ? "delivery_unconfirmed" : "delivery_unobserved"
    }

    struct RootAssignment {
        let id, requestID, requestDigest: String
        let assistant: Assistant
        let model, projectDir, label: String
        let objective, scope, constraints, relevantReferences, acceptance: String
        let projectApproved: Bool
        let created: Date
        var state: RootAssignmentState
        /// `nil` keeps a legacy record's old briefing bytes and transcript receipt.
        var language: RootAssignmentLanguage?
        var identity: RootAssignmentIdentity?
        var terminalOpenedAt, promptReadyAt: Date?
        /// Reset only when a person clears workspace trust, so that human wait consumes none of
        /// the ordinary terminal-open-to-briefing window without falsifying terminalOpenedAt.
        var promptTimeoutStartedAt: Date?
        var briefedAt, activeAt, endedAt: Date?
        var injectAttempts = 0
        var lastInjectAt, missingObservedAt: Date?
        /// The terminal's error for the one counted attempt, when it reported one. Kept for the
        /// deadline's typed failure; it never withdraws the count or licenses another send.
        var injectFailure: String?
        var answeredTrustMenu = false
        var blocker, failure, reconciliation: String?
        /// The durable at-most-once receipt for the last blocked/failed/inactive audit event.
        var reportedTransition: String?
        var missingGeneration: Int?
        var missingEpoch: String?
    }

    struct RootAssignmentTransitionNotice: Equatable {
        let receipt: String
        let event: String
        let reason: String
    }

    struct RootAssignmentCleanupCandidate: Equatable {
        let id: String
        let state: RootAssignmentState
        let created: Date
    }

    static func rootAssignmentCleanupIDs(
        _ rows: [RootAssignmentCleanupCandidate], retaining limit: Int = 200
    ) -> [String] {
        rows.filter { [.failed, .inactive].contains($0.state) }
            .sorted { $0.created > $1.created }
            .dropFirst(limit).map(\.id)
    }

    static func rootAssignmentTransitionNotice(state: RootAssignmentState,
                                               blocker: String?, failure: String?)
        -> RootAssignmentTransitionNotice? {
        let reason: String
        let event: String
        switch state {
        case .blocked:
            guard let blocker else { return nil }
            reason = blocker; event = "root_assignment.blocked"
        case .failed:
            guard let failure else { return nil }
            reason = failure; event = "root_assignment.failed"
        case .inactive:
            guard let failure else { return nil }
            reason = failure; event = "root_assignment.inactive"
        default:
            return nil
        }
        return RootAssignmentTransitionNotice(
            receipt: "\(state.rawValue)|\(reason)", event: event, reason: reason)
    }
}
