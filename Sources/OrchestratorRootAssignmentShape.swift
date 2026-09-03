import Foundation

// The shape of a Root Assignment: the states it moves through, the request that opens one, the
// executor identity it is bound to, and the pure decisions taken from those values — trust, the
// next step, which rows may be cleaned up, and what an audit receipt says. `OrchestratorTaskShape`
// is the same file for a `Task`; this is its opposite number for the other kind of session the
// broker keeps records about.
//
// Chosen by measuring rather than by `// MARK:`. On the base this was cut from, this block
// referenced no `private` symbol left behind and acquired `Orchestrator.lock` zero times, which
// is not true of the `// MARK: - Independent feature roots` heading it used to sit under: the
// forty lines that remain there — `reportRootAssignmentTransition` and `cwd(of:)` — take the lock
// twice and read `private static var rootAssignments`, so they stayed.
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

    struct RootAssignmentDeliveryEvidence: Equatable {
        let transcriptKnown: Bool
        let recorded: Bool
        /// When the assistant's own record says that user turn happened — the delivery itself,
        /// never the beat that got round to reading it. `nil` is a record carrying the turn
        /// without a timestamp: a delivery whose moment is simply unknown.
        let recordedAt: Date?
        /// The end of the pre-brief window this delivery had to land inside, kept beside the
        /// event so the comparison cannot quietly become "when did the broker look".
        let deadline: Date?
        let retryDelayElapsed: Bool

        init(transcriptKnown: Bool, recorded: Bool, recordedAt: Date? = nil,
             deadline: Date? = nil, retryDelayElapsed: Bool) {
            self.transcriptKnown = transcriptKnown; self.recorded = recorded
            self.recordedAt = recordedAt; self.deadline = deadline
            self.retryDelayElapsed = retryDelayElapsed
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

    enum RootAssignmentStepDecision: Equatable {
        case wait, activate, block, promptReady, inspectDelivery, briefed, inject, answerTrust(row: Int)
        case fail(String)
    }

    /// The lifecycle choice is pure; terminal capture, transcript reads, persistence and typing
    /// happen only after this answer. Keeping the whole branch table here makes timeout, trust,
    /// receipt and retry failure injection executable without opening somebody's terminal.
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
            // Composer readiness only decides whether a retry is safe; it never gates observation.
            guard let delivery else { return .inspectDelivery }
            if delivery.deliveredInWindow { return .briefed }
            if promptTimedOut { return .fail("prompt_timeout") }
            guard inputReady else { return .wait }
            if injectAttempts > 0 && !delivery.transcriptKnown { return .wait }
            guard delivery.retryDelayElapsed else { return .wait }
            return injectAttempts >= briefingAttemptLimit
                ? .fail("delivery_unconfirmed") : .inject
        }
        if promptTimedOut { return .fail("prompt_timeout") }
        guard inputReady else { return .wait }
        if state == .terminalOpened || state == .blocked { return .promptReady }
        return .wait
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
