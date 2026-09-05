import Foundation

/// The store codec, asked the only question it exists to answer: does a value survive the trip to
/// `[String: Any]` and back unchanged, and does a row an older build wrote still decode?
///
/// Round-tripping through the encoder alone would be a weaker test than it looks — a field the
/// encoder forgot and the decoder never reads round-trips perfectly. So each record type is
/// compared **field by field** against the value that went in, and the fields are listed here
/// rather than derived, so that adding one to the model and not to the codec leaves a gap this
/// file has to be edited to close.
///
/// The legacy fixtures are the other half. These functions read stores written months ago; a
/// branch that looks like dead tidy-up is a compatibility contract with bytes already on disk.

// MARK: - Rendering one field

private let absent = "«nil»"

private func fieldText(_ value: String?) -> String { value ?? absent }

private func stamp(_ date: Date) -> String { String(date.timeIntervalSince1970) }

private func stampText(_ date: Date?) -> String { date.map(stamp) ?? absent }

private func fieldText<T>(_ value: T?) -> String { value.map { "\($0)" } ?? absent }

/// Compare two field listings produced by the same describing function, one check per field, so a
/// failure names the field rather than the record.
private func compareStoreFields(_ label: String,
                                _ got: [(String, String)],
                                _ want: [(String, String)]) {
    for (index, field) in want.enumerated() {
        let observed = index < got.count ? got[index].1 : "«record did not decode»"
        expect("\(label) keeps \(field.0)", observed, field.1)
    }
}

// MARK: - What each record is made of

private func landingFields(_ landing: Orchestrator.Landing?) -> [(String, String)] {
    guard let landing else { return [] }
    return [("state", landing.state.rawValue),
            ("target", fieldText(landing.target)),
            ("delivery", fieldText(landing.delivery)),
            ("owner_root_key", landing.ownerRootKey),
            ("since", stamp(landing.since)),
            ("commit", fieldText(landing.commit)),
            ("note", fieldText(landing.note)),
            ("landed_at", stampText(landing.landedAt)),
            ("verification_origin", fieldText(landing.verificationOrigin)),
            ("verified_commit", fieldText(landing.verifiedCommit)),
            ("verified_target_commit", fieldText(landing.verifiedTargetCommit))]
}

private func progressFields(_ notes: [Orchestrator.ProgressNote]) -> [(String, String)] {
    [("count", String(notes.count)),
     ("notes", notes.map { "\($0.note)@\(stamp($0.at))" }.joined(separator: "|"))]
}

private func completionFields(_ delivery: Orchestrator.CompletionDelivery?) -> [(String, String)] {
    guard let delivery else { return [] }
    return [("notice_id", delivery.noticeID),
            ("created_at", stamp(delivery.created)),
            ("state", delivery.state.rawValue),
            ("attempts", String(delivery.attempts)),
            ("next_retry_at", stampText(delivery.nextRetryAt)),
            ("last_attempt_at", stampText(delivery.lastAttemptAt)),
            ("transport_delivered_at", stampText(delivery.transportDeliveredAt)),
            ("observed_at", stampText(delivery.observedAt)),
            ("acknowledged_at", stampText(delivery.acknowledgedAt)),
            ("last_error.code", fieldText(delivery.lastError?.code.rawValue)),
            ("last_error.message", fieldText(delivery.lastError?.message)),
            ("last_error.at", stampText(delivery.lastError?.at)),
            ("dead_letter_at", stampText(delivery.deadLetterAt)),
            ("legacy_reconciled", String(delivery.legacyReconciled))]
}

private func handoffFields(_ envelope: Orchestrator.HandoffEnvelope?) -> [(String, String)] {
    guard let envelope else { return [] }
    return [("handoff_id", envelope.id),
            ("project_dir", envelope.projectDir),
            ("title", fieldText(envelope.title)),
            ("from_session", fieldText(envelope.fromSession)),
            ("coordinator_plain_handoff", String(envelope.coordinatorPlainHandoff)),
            ("created", stamp(envelope.created)),
            ("state", envelope.state.rawValue)]
}

private func assignmentFields(_ row: Orchestrator.RootAssignment?) -> [(String, String)] {
    guard let row else { return [] }
    return [("id", row.id),
            ("request_id", row.requestID),
            ("request_digest", row.requestDigest),
            ("assistant", row.assistant.rawValue),
            ("model", row.model),
            ("project_dir", row.projectDir),
            ("label", row.label),
            ("objective", row.objective),
            ("scope", row.scope),
            ("constraints", row.constraints),
            ("relevant_references", row.relevantReferences),
            ("acceptance", row.acceptance),
            ("project_approved", String(row.projectApproved)),
            ("created", stamp(row.created)),
            ("state", row.state.rawValue),
            ("language.tag", fieldText(row.language?.tag)),
            ("language.name", fieldText(row.language?.name)),
            ("identity.terminal_id", fieldText(row.identity?.terminalID)),
            ("identity.assistant", fieldText(row.identity?.assistant.rawValue)),
            ("identity.tty", fieldText(row.identity?.tty)),
            ("identity.pid", fieldText(row.identity?.pid)),
            ("identity.process_start", fieldText(row.identity?.processStart)),
            ("identity.conversation_id", fieldText(row.identity?.conversationID)),
            ("terminal_opened_at", stampText(row.terminalOpenedAt)),
            ("prompt_ready_at", stampText(row.promptReadyAt)),
            ("prompt_timeout_started_at", stampText(row.promptTimeoutStartedAt)),
            ("briefed_at", stampText(row.briefedAt)),
            ("active_at", stampText(row.activeAt)),
            ("ended_at", stampText(row.endedAt)),
            ("inject_attempts", String(row.injectAttempts)),
            ("last_inject_at", stampText(row.lastInjectAt)),
            ("missing_observed_at", stampText(row.missingObservedAt)),
            ("answered_trust_menu", String(row.answeredTrustMenu)),
            ("blocker", fieldText(row.blocker)),
            ("failure", fieldText(row.failure)),
            ("reconciliation", fieldText(row.reconciliation)),
            ("reported_transition", fieldText(row.reportedTransition)),
            ("missing_generation", fieldText(row.missingGeneration)),
            ("missing_epoch", fieldText(row.missingEpoch))]
}

private func waitFields(_ wait: Orchestrator.CoordinationWait?) -> [(String, String)] {
    guard let wait else { return [] }
    return [("id", wait.id),
            ("repository", wait.repository),
            ("paths", wait.paths.joined(separator: "|")),
            ("owner_session_id", wait.ownerSessionID),
            ("release_condition", wait.releaseCondition),
            ("created", stamp(wait.created)),
            ("waiter_count", String(wait.waiters.count)),
            ("waiters", wait.waiters.map {
                "\($0.sessionID)/\($0.reason)/\(stamp($0.created))"
                    + "/\(stampText($0.requestDeliveredAt))/\(stampText($0.releaseDeliveredAt))"
            }.joined(separator: "|"))]
}

private func identityFields(_ identity: Orchestrator.SessionWorkIdentity) -> [(String, String)] {
    [("terminal_id", identity.terminalID),
     ("assistant", fieldText(identity.assistant?.rawValue)),
     ("tty", identity.tty),
     ("pid", fieldText(identity.pid)),
     ("process_start", stampText(identity.processStart)),
     ("conversation_id", fieldText(identity.conversationID))]
}

private func sessionDeliveryFields(_ delivery: Orchestrator.SessionDelivery?)
    -> [(String, String)] {
    guard let delivery else { return [] }
    return identityFields(delivery.identity) + [
        ("summary", delivery.summary),
        ("reported_at", stamp(delivery.reportedAt)),
        ("settled", String(delivery.settled)),
    ]
}

private func selfStateFields(_ state: Orchestrator.SessionSelfState?) -> [(String, String)] {
    guard let state else { return [] }
    return identityFields(state.identity) + [
        ("claim", fieldText(state.claim?.rawValue)),
        ("note", fieldText(state.note)),
        ("moved_by", fieldText(state.movedBy)),
        ("person_needed", fieldText(state.personNeeded)),
        ("claim_reported_at", stampText(state.claimReportedAt)),
        ("claim_settled", String(state.claimSettled)),
        ("owed.note", fieldText(state.owed?.note)),
        ("owed.moved_by", fieldText(state.owed?.movedBy)),
        ("owed.person_needed", fieldText(state.owed?.personNeeded)),
        ("owed.since", stampText(state.owed?.since)),
    ]
}

private func worktreeText(_ worktree: Orchestrator.Worktree?) -> String {
    guard let worktree else { return absent }
    return [worktree.path, worktree.branch, worktree.base, worktree.repository,
            fieldText(worktree.repositoryCommonDir), worktree.cwd, fieldText(worktree.head),
            fieldText(worktree.commits), fieldText(worktree.dirty), String(worktree.baseDirty),
            worktree.requestedBase].joined(separator: "/")
}

private func usageText(_ usage: Orchestrator.Usage?) -> String {
    guard let usage else { return absent }
    return "\(usage.input)/\(usage.output)/\(usage.cacheRead)/\(usage.cacheWrite)/\(usage.total)"
        + "/\(fieldText(usage.model))/\(fieldText(usage.costUsd))"
}

private func graphText(_ graph: Orchestrator.PlanningGraph?) -> String {
    guard let graph else { return absent }
    return [graph.id, graph.destination, graph.currentNode,
            graph.nodes.map { "\($0.id):\($0.title):\($0.kind.rawValue)"
                + ":\($0.dependsOn.joined(separator: ","))"
                + ":\($0.acceptance.joined(separator: ","))" }.joined(separator: ";"),
            graph.unknowns.joined(separator: ","),
            graph.outOfScope.joined(separator: ",")].joined(separator: "/")
}

private func reviewText(_ review: Orchestrator.ReviewReceipt?) -> String {
    guard let review else { return absent }
    return review.verdict.rawValue + "/" + review.axes.map { axis in
        "\(axis.axis.rawValue):\(axis.status.rawValue):"
            + axis.findings.map { "\($0.id)|\($0.severity.rawValue)|\($0.summary)"
                + "|\($0.evidence.joined(separator: "^"))" }.joined(separator: "+")
    }.joined(separator: ";")
}

private func taskFields(_ task: Orchestrator.Task?) -> [(String, String)] {
    guard let task else { return [] }
    return [("id", task.id),
            ("state", task.state.rawValue),
            ("kind", task.kind),
            ("title", task.title),
            ("assistant", task.assistant.rawValue),
            ("project_dir", task.projectDir),
            ("timeout_minutes", String(task.timeoutMinutes)),
            ("created", stamp(task.created)),
            ("depth", String(task.depth)),
            ("secret_hash", task.secretHash),
            ("notify_count", String(task.notifyCount)),
            ("artifacts", task.artifacts.joined(separator: "|")),
            ("spawned_at", stampText(task.spawnedAt)),
            ("briefed_at", stampText(task.briefedAt)),
            ("finished_at", stampText(task.finishedAt)),
            ("result_verified_at", stampText(task.resultVerifiedAt)),
            ("root_session", fieldText(task.rootSessionId)),
            ("root_assistant", fieldText(task.rootAssistant?.rawValue)),
            ("root_label", fieldText(task.rootLabel)),
            ("repository_common_dir", fieldText(task.repositoryCommonDir)),
            ("parent_task", fieldText(task.parentTaskId)),
            ("respawn_of", fieldText(task.respawnOf)),
            ("respawn_generation", String(task.respawnGeneration)),
            ("model", fieldText(task.model)),
            ("reasoning_effort", fieldText(task.reasoningEffort?.rawValue)),
            ("permission", task.permission.rawValue),
            ("plan", fieldText(task.plan)),
            ("graph", graphText(task.graph)),
            ("schedule_id", fieldText(task.scheduleID)),
            ("schedule_close_tab", task.scheduleCloseTab.rawValue),
            ("schedule_notify_failure", String(task.scheduleNotifyFailure)),
            ("serialize", task.serialize.joined(separator: "|")),
            ("claims", task.claims.joined(separator: "|")),
            ("claims_declared", String(task.claimsDeclared)),
            ("claim_keys", task.claimKeys.joined(separator: "|")),
            ("released_claims", task.releasedClaims
                .map { "\($0.path)@\(stamp($0.releasedAt))" }.joined(separator: "|")),
            ("untouched_claims", task.untouchedClaims.joined(separator: "|")),
            ("landing", landingFields(task.landing).map { "\($0.0)=\($0.1)" }
                .joined(separator: ",")),
            ("progress", progressFields(task.progress).map { "\($0.0)=\($0.1)" }
                .joined(separator: ",")),
            ("progress_file_note", fieldText(task.progressFileNote)),
            ("isolation", task.isolation.rawValue),
            ("worktree", worktreeText(task.worktree)),
            ("queued_secret", fieldText(task.queuedSecret)),
            ("attach_session", fieldText(task.attachSessionId)),
            ("child_terminal", fieldText(task.childTerminalId)),
            ("child_backend", fieldText(task.childBackend?.rawValue)),
            ("child_task_root_access", String(task.childTaskRootAccess)),
            ("child_tty", fieldText(task.childTTY)),
            ("child_pid", fieldText(task.childPID)),
            ("child_proc_start", stampText(task.childProcStart)),
            ("child_session", fieldText(task.childSessionId)),
            ("close_at", stampText(task.closeAt)),
            ("terminal_intervention", task.terminalIntervention
                .map { "\($0.kind.rawValue)/\($0.message)" } ?? absent),
            ("work_cleanup_at", stampText(task.workCleanupAt)),
            ("build_cleanup_at", stampText(task.buildCleanupAt)),
            ("transcript", fieldText(task.transcriptPath)),
            ("transcript_proven", String(task.transcriptProven)),
            ("executor", fieldText(task.executorReceipt.map {
                "\($0.status.rawValue)/\($0.provenance)/\($0.inventoryGeneration)"
                    + "/\($0.inventoryEpoch)/\($0.mover)"
            })),
            ("summary", fieldText(task.summary)),
            ("completion_delivery", completionFields(task.completionDelivery)
                .map { "\($0.0)=\($0.1)" }.joined(separator: ",")),
            ("verification", task.verification
                .map { "\($0.runs)/\($0.seconds)/\($0.last.rawValue)/\($0.scope)" } ?? absent),
            ("review", reviewText(task.review)),
            ("usage", usageText(task.usage))]
}

// MARK: - The suite

func runOrchestratorStoreTests() {
    // Fixed rather than generated: a fixture that changes between runs cannot be quoted in a
    // failure report, and `isTaskID` wants lower-case hex.
    let taskID = "11111111-2222-3333-4444-555555555555"
    let otherID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
    let graphID = "99999999-8888-7777-6666-555555555555"
    let sessionID = "0123abcd-4567-89ef-0123-456789abcdef"
    let commit = String(repeating: "a", count: 40)
    let targetCommit = String(repeating: "b", count: 40)
    let base = String(repeating: "c", count: 40)
    let digest = String(repeating: "d", count: 64)
    let epoch = Date(timeIntervalSince1970: 1_700_000_000)
    func later(_ seconds: Double) -> Date { epoch.addingTimeInterval(seconds) }

    group("the landing codec keeps every field, and its verification stays fail-closed") {
        let full = Orchestrator.Landing(
            state: .landed, target: "main", delivery: "branch", ownerRootKey: "0123abcd",
            since: epoch, commit: commit, note: "landed by root", landedAt: later(60),
            verificationOrigin: "local_target_branch", verifiedCommit: commit,
            verifiedTargetCommit: targetCommit)
        let minimal = Orchestrator.Landing(
            state: .pending, target: nil, delivery: nil, ownerRootKey: "beefcafe",
            since: epoch, commit: nil, note: nil)
        for (label, value) in [("a verified landing", full), ("a bare pending landing", minimal)] {
            let row = OrchestratorStore.stored(value)
            compareStoreFields(label, landingFields(OrchestratorStore.landing(from: row)),
                               landingFields(value))
        }

        // The wire record and the stored record are deliberately different clocks: whole seconds
        // for a reader, full precision for the store that has to reproduce them.
        let wire = OrchestratorStore.landingRecord(full)
        check("the wire record rounds its clocks to whole seconds",
              wire["since"] as? Int == Int(epoch.timeIntervalSince1970)
                  && wire["landed_at"] as? Int == Int(later(60).timeIntervalSince1970))
        check("and the stored record keeps the full precision the decoder reads",
              OrchestratorStore.stored(full)["since"] as? Double == epoch.timeIntervalSince1970)

        // The three verification fields are one fact. A row carrying part of it is a row an older
        // build wrote before the fact existed, and it must not be read as evidence.
        var partial = OrchestratorStore.stored(full)
        partial.removeValue(forKey: "verified_target_commit")
        check("a half-written verification refuses rather than half-decoding",
              OrchestratorStore.landing(from: partial) == nil)
        var withoutVerification = OrchestratorStore.stored(full)
        for key in ["verification_origin", "verified_commit", "verified_target_commit"] {
            withoutVerification.removeValue(forKey: key)
        }
        let legacy = OrchestratorStore.landing(from: withoutVerification)
        check("a legacy landed row with no verification at all still decodes",
              legacy?.state == .landed && legacy?.commit == commit
                  && legacy?.verificationOrigin == nil)
    }

    group("the progress codec keeps its bound and re-applies the kept cap on the way in") {
        let notes = (0..<3).map {
            Orchestrator.ProgressNote(note: "note \($0)", at: later(Double($0)))
        }
        let rows = notes.map(OrchestratorStore.stored)
        compareStoreFields("a short progress list",
                           progressFields(OrchestratorStore.progress(from: rows)),
                           progressFields(notes))
        let single = [Orchestrator.ProgressNote(note: "x", at: epoch)]
        compareStoreFields("a one-note progress list",
                           progressFields(OrchestratorStore.progress(from: single.map(
                               OrchestratorStore.stored))),
                           progressFields(single))

        let wire = OrchestratorStore.progressRecord(notes[0])
        check("the wire note rounds its clock to whole seconds",
              wire["at"] as? Int == Int(epoch.timeIntervalSince1970)
                  && wire["note"] as? String == "note 0")

        // A store written before the cap existed can hold an unbounded list, so the cap is
        // applied again on the way in rather than only on the way out.
        let overflowing = (0..<12).map {
            OrchestratorStore.stored(
                Orchestrator.ProgressNote(note: "note \($0)", at: later(Double($0))))
        }
        let capped = OrchestratorStore.progress(from: overflowing)
        expect("an older unbounded list is cut to the kept count",
               capped.count, Orchestrator.progressKept)
        expect("and it keeps the newest, not the oldest",
               capped.first?.note ?? "", "note 7")
        check("a row that lost its text or its clock is dropped, not guessed at",
              OrchestratorStore.progress(from: [["at": epoch.timeIntervalSince1970],
                                                ["note": "orphan"],
                                                ["note": "   ", "at": 1.0]]).isEmpty)
        check("and a note longer than the limit is dropped rather than truncated",
              OrchestratorStore.progress(from: [[
                  "note": String(repeating: "x", count: Orchestrator.progressLimit + 1),
                  "at": epoch.timeIntervalSince1970]]).isEmpty)
        check("progress that is not a list of rows is an empty list",
              OrchestratorStore.progress(from: "not rows").isEmpty
                  && OrchestratorStore.progress(from: nil).isEmpty)
    }

    group("the completion delivery codec keeps every clock and its legacy flag") {
        let full = Orchestrator.CompletionDelivery(
            noticeID: sessionID, created: epoch, state: .acknowledged, attempts: 3,
            nextRetryAt: nil, lastAttemptAt: later(10), transportDeliveredAt: later(20),
            observedAt: later(30), acknowledgedAt: later(40),
            lastError: Orchestrator.CompletionFailure(
                code: .itermModal, message: "a modal was in the way", at: later(5)),
            deadLetterAt: nil, legacyReconciled: true)
        let minimal = Orchestrator.CompletionDelivery(
            noticeID: sessionID, created: epoch, state: .pending, attempts: 0, nextRetryAt: nil)
        for (label, value) in [("an acknowledged delivery", full),
                               ("a pending delivery", minimal)] {
            let row = OrchestratorStore.stored(value)
            compareStoreFields(label,
                               completionFields(OrchestratorStore.completionDelivery(from: row)),
                               completionFields(value))
        }
        check("a decoded envelope is persisted by construction, whatever the sender believed",
              OrchestratorStore.completionDelivery(
                from: OrchestratorStore.stored(minimal))?.persisted == true)

        // `legacy_reconciled` postdates the envelope, so its absence is false rather than a
        // refusal — the shape every row written before reconciliation existed has.
        var legacy = OrchestratorStore.stored(full)
        legacy.removeValue(forKey: "legacy_reconciled")
        check("a row written before the reconciliation flag decodes as unreconciled",
              OrchestratorStore.completionDelivery(from: legacy)?.legacyReconciled == false)
        var acknowledgedWithoutClock = OrchestratorStore.stored(full)
        acknowledgedWithoutClock.removeValue(forKey: "acknowledged_at")
        check("an acknowledged state with no acknowledgement clock is refused",
              OrchestratorStore.completionDelivery(from: acknowledgedWithoutClock) == nil)
        var damagedError = OrchestratorStore.stored(full)
        damagedError["last_error"] = ["code": "not_a_code", "message": "x", "at": 1.0]
        check("an unreadable failure refuses the whole envelope rather than losing it",
              OrchestratorStore.completionDelivery(from: damagedError) == nil)
    }

    group("the handoff envelope codec keeps its two optional halves") {
        let full = Orchestrator.HandoffEnvelope(
            id: taskID, projectDir: "/clawdline-fixture/repo", title: "continue the line",
            fromSession: "sender-session", coordinatorPlainHandoff: true,
            created: epoch, state: .delivered)
        let minimal = Orchestrator.HandoffEnvelope(
            id: otherID, projectDir: "/clawdline-fixture/repo", title: nil, fromSession: nil,
            coordinatorPlainHandoff: false, created: epoch, state: .opening)
        for (label, value) in [("a delivered handoff", full), ("an opening handoff", minimal)] {
            let row = OrchestratorStore.stored(value)
            compareStoreFields(label, handoffFields(OrchestratorStore.handoff(from: row)),
                               handoffFields(value))
        }
        // Both optional halves are bounded rather than validated: an over-long value from an
        // older, unbounded writer drops that field instead of dropping the envelope.
        var oversized = OrchestratorStore.stored(full)
        oversized["title"] = String(repeating: "t", count: 201)
        oversized["from_session"] = String(repeating: "s", count: 201)
        let bounded = OrchestratorStore.handoff(from: oversized)
        check("an over-long title or sender is dropped, and the envelope survives",
              bounded?.id == taskID && bounded?.title == nil && bounded?.fromSession == nil)
        var noProject = OrchestratorStore.stored(full)
        noProject["project_dir"] = "relative/path"
        check("an unusable project directory refuses the envelope",
              OrchestratorStore.handoff(from: noProject) == nil)
    }

    group("the root assignment codec keeps every field a legacy row may not have") {
        var full = Orchestrator.RootAssignment(
            id: taskID, requestID: otherID, requestDigest: digest, assistant: .claude,
            model: "opus", projectDir: "/clawdline-fixture/repo", label: "Feature Root",
            objective: "objective", scope: "scope", constraints: "constraints",
            relevantReferences: "references", acceptance: "acceptance", projectApproved: true,
            created: epoch, state: .active,
            language: Orchestrator.RootAssignmentLanguage(tag: "zh-Hant",
                                                          name: "Traditional Chinese"))
        full.identity = Orchestrator.RootAssignmentIdentity(
            terminalID: "terminal-1", assistant: .claude, tty: "/dev/ttys001", pid: 4_242,
            processStart: 1_700_000_000, conversationID: sessionID)
        full.terminalOpenedAt = later(1)
        full.promptReadyAt = later(2)
        full.promptTimeoutStartedAt = later(3)
        full.briefedAt = later(4)
        full.activeAt = later(5)
        full.endedAt = later(6)
        full.lastInjectAt = later(7)
        full.missingObservedAt = later(8)
        full.injectAttempts = 2
        full.answeredTrustMenu = true
        full.blocker = "workspace_trust_required"
        full.failure = "no"
        full.reconciliation = "rebound"
        full.reportedTransition = "root_assignment.blocked"
        full.missingGeneration = 7
        full.missingEpoch = "epoch-1"
        let minimal = Orchestrator.RootAssignment(
            id: otherID, requestID: taskID, requestDigest: digest, assistant: .codex,
            model: "default", projectDir: "/clawdline-fixture/repo", label: "Bare Root",
            objective: "objective", scope: "scope", constraints: "constraints",
            relevantReferences: "references", acceptance: "acceptance", projectApproved: false,
            created: epoch, state: .accepted, language: nil)
        for (label, value) in [("a fully briefed assignment", full),
                               ("a bare accepted assignment", minimal)] {
            let row = OrchestratorStore.stored(value)
            compareStoreFields(label,
                               assignmentFields(OrchestratorStore.rootAssignment(from: row)),
                               assignmentFields(value))
        }

        // Three fields postdate the record. Their absence is the legacy shape, and each has a
        // documented default rather than a refusal.
        var legacy = OrchestratorStore.stored(full)
        for key in ["language", "project_approved", "answered_trust_menu"] {
            legacy.removeValue(forKey: key)
        }
        let decoded = OrchestratorStore.rootAssignment(from: legacy)
        check("a row written before language, trust and approval keeps its old bytes",
              decoded?.language == nil && decoded?.projectApproved == false
                  && decoded?.answeredTrustMenu == false && decoded?.id == taskID)
        var crossedIdentity = OrchestratorStore.stored(full)
        crossedIdentity["identity"] = ["terminal_id": "terminal-1", "assistant": "codex"]
        check("an identity naming another assistant is dropped, not believed",
              OrchestratorStore.rootAssignment(from: crossedIdentity)?.identity == nil)
        var overAttempted = OrchestratorStore.stored(full)
        overAttempted["inject_attempts"] = 9_999
        expect("inject attempts are clamped to the briefing limit on the way in",
               OrchestratorStore.rootAssignment(from: overAttempted)?.injectAttempts,
               Orchestrator.briefingAttemptLimit)
        var foreignLanguage = OrchestratorStore.stored(full)
        foreignLanguage["language"] = ["tag": "xx-Fake", "name": "Nowhere"]
        check("a language tag this build does not ship is dropped, and the row survives",
              OrchestratorStore.rootAssignment(from: foreignLanguage)?.language == nil)
    }

    group("the coordination wait codec keeps its canonical paths and waiter receipts") {
        let full = Orchestrator.CoordinationWait(
            id: taskID, repository: "/clawdline-fixture/repo",
            paths: ["Sources/Orchestrator.swift", "Sources/OrchestratorStore.swift"],
            ownerSessionID: "owner-session", releaseCondition: "after the codec lands",
            created: epoch,
            waiters: [
                Orchestrator.CoordinationWaiter(
                    sessionID: "waiter-a", reason: "needs the same file", created: later(1),
                    requestDeliveredAt: later(2), releaseDeliveredAt: later(3)),
                Orchestrator.CoordinationWaiter(
                    sessionID: "waiter-b", reason: "queued behind it", created: later(4)),
            ])
        let minimal = Orchestrator.CoordinationWait(
            id: otherID, repository: "/clawdline-fixture/repo", paths: ["README.md"],
            ownerSessionID: "owner-session", releaseCondition: "soon", created: epoch,
            waiters: [Orchestrator.CoordinationWaiter(
                sessionID: "waiter-c", reason: "why", created: epoch)])
        for (label, value) in [("a wait with two waiters", full),
                               ("a wait with one waiter", minimal)] {
            let row = OrchestratorStore.stored(value)
            compareStoreFields(label,
                               waitFields(OrchestratorStore.coordinationWait(from: row)),
                               waitFields(value))
        }
        var duplicated = OrchestratorStore.stored(full)
        let waiterRows = duplicated["waiters"] as? [[String: Any]] ?? []
        duplicated["waiters"] = waiterRows + [waiterRows[0]]
        expect("a session that appears twice in the waiter list is kept once",
               OrchestratorStore.coordinationWait(from: duplicated)?.waiters.count, 2)
        var empty = OrchestratorStore.stored(full)
        empty["waiters"] = [[String: Any]]()
        check("a wait nobody is waiting on is not a wait",
              OrchestratorStore.coordinationWait(from: empty) == nil)
        var escaping = OrchestratorStore.stored(full)
        escaping["paths"] = ["../outside/the/repository"]
        check("a path that leaves the repository refuses the whole record",
              OrchestratorStore.coordinationWait(from: escaping) == nil)
    }

    group("the session delivery codec keeps the exact process it is bound to") {
        let identity = Orchestrator.SessionWorkIdentity(
            terminalID: "terminal-1", assistant: .claude, tty: "/dev/ttys001", pid: 4_242,
            processStart: epoch, conversationID: sessionID)
        let full = Orchestrator.SessionDelivery(
            identity: identity, summary: String(repeating: "s", count: 500),
            reportedAt: later(9), settled: true)
        let minimal = Orchestrator.SessionDelivery(
            identity: identity, summary: "x", reportedAt: epoch, settled: false)
        for (label, value) in [("a settled delivery", full), ("an unsettled delivery", minimal)] {
            let row = OrchestratorStore.stored(value)
            compareStoreFields(label,
                               sessionDeliveryFields(
                                   OrchestratorStore.sessionDelivery(from: row)),
                               sessionDeliveryFields(value))
        }
        // Every identity field is required. A record that has lost one cannot be re-bound to a
        // later process occupying the same terminal, so it fails closed.
        for key in ["assistant", "pid", "process_start", "conversation_id", "tty"] {
            var damaged = OrchestratorStore.stored(full)
            damaged.removeValue(forKey: key)
            check("a delivery missing \(key) fails closed",
                  OrchestratorStore.sessionDelivery(from: damaged) == nil)
        }
        var oversized = OrchestratorStore.stored(full)
        oversized["summary"] = String(repeating: "s", count: 501)
        check("a summary past its limit refuses the record",
              OrchestratorStore.sessionDelivery(from: oversized) == nil)
    }

    group("the session self-state codec keeps both halves and refuses a third claim") {
        let identity = Orchestrator.SessionWorkIdentity(
            terminalID: "terminal-2", assistant: .codex, tty: "/dev/ttys002", pid: 91,
            processStart: epoch, conversationID: sessionID)
        let full = Orchestrator.SessionSelfState(
            identity: identity, claim: .holding, note: "waiting on the review",
            movedBy: "root", personNeeded: true, claimReportedAt: later(11), claimSettled: true,
            owed: Orchestrator.OwedDebt(note: "somebody owes an answer", movedBy: "root",
                                        personNeeded: false, since: later(12)))
        let minimal = Orchestrator.SessionSelfState(
            identity: identity, claim: .ready, note: nil, movedBy: nil, personNeeded: nil,
            claimReportedAt: nil, claimSettled: false, owed: nil)
        for (label, value) in [("a holding session with a debt", full),
                               ("a bare ready claim", minimal)] {
            let row = OrchestratorStore.stored(value)
            compareStoreFields(label,
                               selfStateFields(OrchestratorStore.sessionSelfState(from: row)),
                               selfStateFields(value))
        }
        let owedOnly = Orchestrator.SessionSelfState(
            identity: identity, claim: nil, note: nil, movedBy: nil, personNeeded: nil,
            claimReportedAt: nil, claimSettled: false,
            owed: Orchestrator.OwedDebt(note: "debt with no claim", movedBy: nil,
                                        personNeeded: true, since: epoch))
        compareStoreFields("a record that is only a debt",
                           selfStateFields(OrchestratorStore.sessionSelfState(
                               from: OrchestratorStore.stored(owedOnly))),
                           selfStateFields(owedOnly))
        // Only the two declarable states survive a reload. A row holding any other is a record
        // this code has no business believing, whatever wrote it.
        for claim in ["working", "waiting_you", "work_complete", "unknown"] {
            var foreign = OrchestratorStore.stored(full)
            foreign["claim"] = claim
            check("a stored claim of \(claim) refuses the record",
                  OrchestratorStore.sessionSelfState(from: foreign) == nil)
        }
        var neither = OrchestratorStore.stored(minimal)
        neither.removeValue(forKey: "claim")
        check("a record with neither a claim nor a debt is nothing to keep",
              OrchestratorStore.sessionSelfState(from: neither) == nil)
    }

    group("the task codec keeps every field across a full and a minimal record") {
        let repository = "/clawdline-fixture/monorepo"
        let worktreePath = OrchestratorDraft.worktreePath(project: repository, taskID: taskID) ?? ""
        var worktree = Orchestrator.Worktree(
            path: worktreePath, branch: OrchestratorDraft.worktreeBranch(for: taskID) ?? "",
            base: base, repository: repository, cwd: worktreePath + "/sub")
        worktree.head = commit
        worktree.commits = 3
        worktree.dirty = true
        worktree.baseDirty = 2
        worktree.requestedBase = "main"

        var full = Orchestrator.Task(
            id: taskID, state: .success, kind: "custom", title: "a task",
            assistant: .claude, projectDir: "/clawdline-fixture/monorepo",
            timeoutMinutes: 180, created: epoch, secretHash: digest)
        full.model = "opus"
        full.permission = .full
        full.projectDir = repository
        full.repositoryCommonDir = "/clawdline-fixture/monorepo/.git"
        full.spawnedAt = later(1)
        full.briefedAt = later(2)
        full.finishedAt = later(3)
        full.resultVerifiedAt = later(4)
        full.rootSessionId = "root-session"
        full.rootAssistant = .codex
        full.rootLabel = "root label"
        full.depth = 2
        full.parentTaskId = otherID
        full.respawnOf = otherID
        full.respawnGeneration = 1
        full.plan = "the plan"
        full.graph = Orchestrator.planningGraph(from: [
            "id": graphID, "destination": "a boundary that is real",
            "current_node": "cut1",
            "nodes": [["id": "cut1", "title": "extract", "kind": "delivery",
                       "depends_on": [String](), "acceptance": ["only pure functions moved"]],
                      ["id": "review", "title": "review", "kind": "review",
                       "depends_on": ["cut1"], "acceptance": ["evidence for every finding"]]],
            "unknowns": ["whether a helper reaches state"],
            "out_of_scope": ["cut 2"],
        ] as [String: Any]).graph
        full.scheduleID = otherID
        full.scheduleCloseTab = .always
        full.scheduleNotifyFailure = false
        full.serialize = ["opus"]
        full.claims = ["Sources/Orchestrator.swift", "Sources/OrchestratorStore.swift"]
        full.claimsDeclared = true
        full.claimKeys = OrchestratorDraft.freezeClaims(full.claims, projectDir: repository)
        full.releasedClaims = [Orchestrator.ReleasedClaim(path: full.claimKeys[0],
                                                          releasedAt: later(5))]
        full.untouchedClaims = ["Sources/Orchestrator.swift"]
        full.landing = Orchestrator.Landing(
            state: .landed, target: "main", delivery: "branch", ownerRootKey: "0123abcd",
            since: epoch, commit: commit, note: "landed", landedAt: later(6),
            verificationOrigin: "local_target_branch", verifiedCommit: commit,
            verifiedTargetCommit: targetCommit)
        full.progress = [Orchestrator.ProgressNote(note: "first", at: later(7)),
                         Orchestrator.ProgressNote(note: "second", at: later(8))]
        full.progressFileNote = "from the file channel"
        full.isolation = .worktree
        full.worktree = worktree
        full.queuedSecret = "sealed"
        full.attachSessionId = "attached-session"
        full.childTerminalId = "terminal-3"
        full.childBackend = .iterm
        full.childTaskRootAccess = true
        full.childTTY = "/dev/ttys003"
        full.childPID = 777
        full.childProcStart = later(9)
        full.childSessionId = sessionID
        full.transcriptPath = "/clawdline-fixture/transcripts/\(sessionID).jsonl"
        full.transcriptProven = true
        full.closeAt = later(10)
        full.terminalIntervention = Orchestrator.TerminalIntervention(
            kind: .iTermModal, message: "iTerm2 needs attention")
        full.workCleanupAt = later(11)
        full.buildCleanupAt = later(12)
        full.summary = "done"
        full.artifacts = ["artifacts/cut1-report.md"]
        full.notifyCount = 2
        full.completionDelivery = Orchestrator.CompletionDelivery(
            noticeID: sessionID, created: epoch, state: .delivered, attempts: 1,
            nextRetryAt: later(13), lastAttemptAt: later(14), transportDeliveredAt: later(15))
        full.verification = Orchestrator.Verification(
            runs: 2, seconds: 940, last: .pass, scope: "swift suite")
        full.review = Orchestrator.ReviewReceipt(
            verdict: .changesRequired,
            axes: Orchestrator.ReviewAxisName.allCases.map { name in
                Orchestrator.ReviewAxis(
                    axis: name, status: .findings,
                    findings: [Orchestrator.ReviewFinding(
                        id: "f-\(name.rawValue)", severity: .minor,
                        summary: "a finding", evidence: ["Sources/OrchestratorStore.swift:1"])])
            })
        var usage = Orchestrator.Usage()
        usage.input = 10; usage.output = 20; usage.cacheRead = 30
        usage.cacheWrite = 40; usage.total = 100
        usage.model = "opus"; usage.costUsd = 1.25
        full.usage = usage

        let minimal = Orchestrator.Task(
            id: otherID, state: .queued, kind: "custom", title: "task",
            assistant: .codex, projectDir: "/clawdline-fixture/monorepo",
            timeoutMinutes: 30, created: epoch, secretHash: digest)

        for (label, value) in [("a fully populated task", full), ("a bare queued task", minimal)] {
            let row = OrchestratorStore.stored(value)
            compareStoreFields(label, taskFields(OrchestratorStore.task(from: row)),
                               taskFields(value))
        }
        check("the graph fixture is a graph, so the field above is not comparing two nils",
              full.graph != nil && full.review != nil && full.worktree != nil)
        // The worktree's own common directory is deliberately not one of the stored worktree
        // fields: `stored(_:)` promotes it to the task-wide receipt, which is the identity that
        // survives the checkout being disposed. The legacy group below covers the migration.
        var promoted = full
        promoted.repositoryCommonDir = nil
        promoted.worktree?.repositoryCommonDir = "/clawdline-fixture/monorepo/.git"
        let carried = OrchestratorStore.task(from: OrchestratorStore.stored(promoted))
        check("a worktree common directory is stored once, on the task",
              carried?.repositoryCommonDir == "/clawdline-fixture/monorepo/.git"
                  && carried?.worktree?.repositoryCommonDir == nil)
    }

    group("the task codec still reads the shapes older builds wrote") {
        let bare = Orchestrator.Task(
            id: taskID, state: .briefed, kind: "custom", title: "task",
            assistant: .claude, projectDir: "/clawdline-fixture/monorepo",
            timeoutMinutes: 30, created: epoch, secretHash: digest)

        // The short-lived crash-recovery store persisted only prose. The branch that reads it is
        // the only place a kind is inferred from text, and it exists to retain that reason.
        var proseIntervention = OrchestratorStore.stored(bare)
        proseIntervention["child_backend"] = "iterm"
        proseIntervention["terminal_intervention"] = "iTerm2 needs attention"
        let modal = OrchestratorStore.task(from: proseIntervention)
        check("a prose intervention from an iTerm2 tab decodes as the modal kind",
              modal?.terminalIntervention?.kind == .iTermModal
                  && modal?.terminalIntervention?.message == "iTerm2 needs attention")
        var proseOnTerminal = OrchestratorStore.stored(bare)
        proseOnTerminal["terminal_intervention"] = "something else went wrong"
        check("and the same prose on any other tab stays the plain terminal kind",
              OrchestratorStore.task(from: proseOnTerminal)?.terminalIntervention?.kind
                  == .terminal)

        var noDepth = OrchestratorStore.stored(bare)
        noDepth.removeValue(forKey: "depth")
        expect("a registry written before depth existed holds only what a root dispatched",
               OrchestratorStore.task(from: noDepth)?.depth, 1)

        var noIsolation = OrchestratorStore.stored(bare)
        noIsolation.removeValue(forKey: "isolation")
        let shared = OrchestratorStore.task(from: noIsolation)
        check("a row written before isolation is a shared checkout with no worktree",
              shared?.isolation == Orchestrator.Isolation.none && shared?.worktree == nil)

        var orphanGeneration = OrchestratorStore.stored(bare)
        orphanGeneration["respawn_generation"] = 4
        let orphan = OrchestratorStore.task(from: orphanGeneration)
        check("a chain position with no origin is dropped rather than counted",
              orphan?.respawnOf == nil && orphan?.respawnGeneration == 0)

        var defaults = OrchestratorStore.stored(bare)
        for key in ["kind", "title", "timeout_minutes", "permission", "artifacts",
                    "notify_count"] {
            defaults.removeValue(forKey: key)
        }
        let defaulted = OrchestratorStore.task(from: defaults)
        check("the fields a row may simply not have keep their documented defaults",
              defaulted?.kind == "custom" && defaulted?.title == "task"
                  && defaulted?.timeoutMinutes == 30
                  && defaulted?.permission == Permission.ask
                  && defaulted?.artifacts.isEmpty == true && defaulted?.notifyCount == 0)

        // Claim keys are frozen at registration. A row whose key list no longer matches its
        // claims is one an older build wrote before they were persisted, and it is re-frozen.
        var claimed = OrchestratorStore.stored(bare)
        claimed["claims"] = ["Sources/Orchestrator.swift"]
        claimed.removeValue(forKey: "claim_keys")
        expect("claim keys absent from a row are frozen again from its claims",
               OrchestratorStore.task(from: claimed)?.claimKeys,
               OrchestratorDraft.freezeClaims(["Sources/Orchestrator.swift"],
                                         projectDir: "/clawdline-fixture/monorepo"))
        var undeclared = OrchestratorStore.stored(bare)
        undeclared.removeValue(forKey: "claims")
        let unknownWriteSet = OrchestratorStore.task(from: undeclared)
        check("a row with no claims key means the write set is unknown, not empty",
              unknownWriteSet?.claimsDeclared == false && unknownWriteSet?.claims.isEmpty == true)

        // The worktree's common directory moved to the task; the old nested spelling is migrated
        // on the way in so the next save writes the new one.
        let repository = "/clawdline-fixture/monorepo"
        var isolated = bare
        isolated.isolation = .worktree
        var worktree = Orchestrator.Worktree(
            path: OrchestratorDraft.worktreePath(project: repository, taskID: taskID) ?? "",
            branch: OrchestratorDraft.worktreeBranch(for: taskID) ?? "", base: base,
            repository: repository,
            cwd: OrchestratorDraft.worktreePath(project: repository, taskID: taskID) ?? "")
        worktree.repositoryCommonDir = "/clawdline-fixture/monorepo/.git"
        isolated.worktree = worktree
        var nested = OrchestratorStore.stored(isolated)
        nested.removeValue(forKey: "repository_common_dir")
        var nestedWorktree = nested["worktree"] as? [String: Any] ?? [:]
        nestedWorktree["repository_common_dir"] = "/clawdline-fixture/monorepo/.git"
        nested["worktree"] = nestedWorktree
        expect("the old nested common directory is migrated to the task-wide receipt",
               OrchestratorStore.task(from: nested)?.repositoryCommonDir,
               "/clawdline-fixture/monorepo/.git")

        var strangerBranch = OrchestratorStore.stored(isolated)
        var strangerWorktree = strangerBranch["worktree"] as? [String: Any] ?? [:]
        strangerWorktree["branch"] = "clawdline/task/\(otherID)"
        strangerBranch["worktree"] = strangerWorktree
        check("a worktree naming another task's branch refuses the whole task",
              OrchestratorStore.task(from: strangerBranch) == nil)

        var damagedID = OrchestratorStore.stored(bare)
        damagedID["id"] = "not-a-task-id"
        check("a row whose id is not a task id is not a task",
              OrchestratorStore.task(from: damagedID) == nil)
    }
}
