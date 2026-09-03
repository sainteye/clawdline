import Foundation

/// The registry's serializer, and only that.
///
/// `Orchestrator` owns the collections and the lock; this namespace owns the translation between
/// those domain values and the `[String: Any]` rows that reach disk. Nothing here reads or writes
/// shared state, and nothing here takes a lock — `load(force:)` and `save()` stay behind in
/// `Orchestrator` because assigning the collections is exactly the ownership this file does not
/// have.
///
/// It is a separate namespace rather than an `extension Orchestrator` in another file on purpose:
/// an extension would move the text without moving the dependency, and the boundary is the point.
/// Writing `Orchestrator.Task` and `Orchestrator.Landing` in full is that boundary becoming
/// visible at every crossing.
///
/// **These functions decode records written by older builds.** Every key string, default,
/// legacy-shape branch and optional-versus-absent distinction is a compatibility contract with
/// stores already on disk; tidying one is a data-loss bug, not a cleanup.
enum OrchestratorStore {

    static func landingRecord(_ landing: Orchestrator.Landing) -> [String: Any] {
        var out: [String: Any] = [
            "state": landing.state.rawValue,
            "owner_root_key": landing.ownerRootKey,
            "since": Int(landing.since.timeIntervalSince1970),
        ]
        if let target = landing.target { out["target"] = target }
        if let delivery = landing.delivery { out["delivery"] = delivery }
        if let commit = landing.commit { out["commit"] = commit }
        if let note = landing.note { out["note"] = note }
        if let landedAt = landing.landedAt {
            out["landed_at"] = Int(landedAt.timeIntervalSince1970)
        }
        if let origin = landing.verificationOrigin { out["verification_origin"] = origin }
        if let commit = landing.verifiedCommit { out["verified_commit"] = commit }
        if let targetCommit = landing.verifiedTargetCommit {
            out["verified_target_commit"] = targetCommit
        }
        return out
    }

    static func stored(_ landing: Orchestrator.Landing) -> [String: Any] {
        var out = landingRecord(landing)
        out["since"] = landing.since.timeIntervalSince1970
        if let landedAt = landing.landedAt {
            out["landed_at"] = landedAt.timeIntervalSince1970
        }
        return out
    }

    /// A progress note on the wire — whole seconds, like every other time in a record.
    static func progressRecord(_ note: Orchestrator.ProgressNote) -> [String: Any] {
        ["note": note.note, "at": Int(note.at.timeIntervalSince1970)]
    }

    static func stored(_ note: Orchestrator.ProgressNote) -> [String: Any] {
        ["note": note.note, "at": note.at.timeIntervalSince1970]
    }

    /// Notes back off disk. A row that lost its text or its clock is dropped rather than
    /// resurrected with a guess, and the kept-count is applied again on the way in so an older
    /// store written before the cap cannot reintroduce an unbounded list.
    static func progress(from raw: Any?) -> [Orchestrator.ProgressNote] {
        guard let rows = raw as? [[String: Any]] else { return [] }
        let notes = rows.compactMap { row -> Orchestrator.ProgressNote? in
            guard let text = row["note"] as? String,
                  !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  text.count <= Orchestrator.progressLimit,
                  let at = row["at"] as? Double else { return nil }
            return Orchestrator.ProgressNote(note: text, at: Date(timeIntervalSince1970: at))
        }
        return notes.count > Orchestrator.progressKept ? Array(notes.suffix(Orchestrator.progressKept)) : notes
    }

    static func stored(_ task: Orchestrator.Task) -> [String: Any] {
        var out: [String: Any] = [
            "id": task.id,
            "state": task.state.rawValue,
            "kind": task.kind,
            "title": task.title,
            "assistant": task.assistant.rawValue,
            "project_dir": task.projectDir,
            "timeout_minutes": task.timeoutMinutes,
            "created": task.created.timeIntervalSince1970,
            "depth": task.depth,
            "secret_hash": task.secretHash,
            "notify_count": task.notifyCount,
            "artifacts": task.artifacts,
        ]
        if let at = task.spawnedAt { out["spawned_at"] = at.timeIntervalSince1970 }
        if let at = task.briefedAt { out["briefed_at"] = at.timeIntervalSince1970 }
        if let at = task.finishedAt { out["finished_at"] = at.timeIntervalSince1970 }
        if let at = task.resultVerifiedAt {
            out["result_verified_at"] = at.timeIntervalSince1970
        }
        if let v = task.rootSessionId { out["root_session"] = v }
        if let v = task.rootAssistant { out["root_assistant"] = v.rawValue }
        if let v = task.rootLabel { out["root_label"] = v }
        if let v = task.repositoryCommonDir ?? task.worktree?.repositoryCommonDir {
            out["repository_common_dir"] = v
        }
        if let v = task.parentTaskId { out["parent_task"] = v }
        if let v = task.respawnOf {
            out["respawn_of"] = v
            out["respawn_generation"] = task.respawnGeneration
        }
        if let v = task.model { out["model"] = v }
        if let v = task.reasoningEffort { out["reasoning_effort"] = v.rawValue }
        out["permission"] = task.permission.rawValue
        if let v = task.plan { out["plan"] = v }
        if let v = task.graph { out["graph"] = Orchestrator.storedPlanningGraph(v) }
        if let v = task.scheduleID {
            out["schedule_id"] = v
            out["schedule_close_tab"] = task.scheduleCloseTab.rawValue
            out["schedule_notify_failure"] = task.scheduleNotifyFailure
        }
        if !task.serialize.isEmpty { out["serialize"] = task.serialize }
        if task.claimsDeclared { out["claims"] = task.claims }
        if !task.claimKeys.isEmpty { out["claim_keys"] = task.claimKeys }
        if !task.releasedClaims.isEmpty {
            out["released_claims"] = task.releasedClaims.map {
                ["path": $0.path, "released_at": $0.releasedAt.timeIntervalSince1970]
                    as [String: Any]
            }
        }
        if !task.untouchedClaims.isEmpty { out["untouched_claims"] = task.untouchedClaims }
        if let landing = task.landing { out["landing"] = stored(landing) }
        if !task.progress.isEmpty { out["progress"] = task.progress.map(stored) }
        if let v = task.progressFileNote { out["progress_file_note"] = v }
        if let worktree = task.worktree {
            out["isolation"] = Orchestrator.Isolation.worktree.rawValue
            var storedWorktree: [String: Any] = [
                "path": worktree.path, "branch": worktree.branch, "base": worktree.base,
                "repository": worktree.repository, "cwd": worktree.cwd,
                "base_dirty": worktree.baseDirty, "requested_base": worktree.requestedBase,
            ]
            if let head = worktree.head { storedWorktree["head"] = head }
            if let commits = worktree.commits { storedWorktree["commits"] = commits }
            if let dirty = worktree.dirty { storedWorktree["dirty"] = dirty }
            out["worktree"] = storedWorktree
        }
        if let v = task.queuedSecret { out["queued_secret"] = v }
        if let v = task.attachSessionId { out["attach_session"] = v }
        if let v = task.childTerminalId { out["child_terminal"] = v }
        if let v = task.childBackend { out["child_backend"] = v.rawValue }
        if task.childTaskRootAccess { out["child_task_root_access"] = true }
        if let v = task.childTTY { out["child_tty"] = v }
        if let v = task.childPID { out["child_pid"] = Int(v) }
        if let v = task.childProcStart { out["child_proc_start"] = v.timeIntervalSince1970 }
        if let v = task.childSessionId { out["child_session"] = v }
        if let at = task.closeAt { out["close_at"] = at.timeIntervalSince1970 }
        if let v = task.terminalIntervention {
            out["terminal_intervention"] = ["kind": v.kind.rawValue, "message": v.message]
        }
        if let at = task.workCleanupAt { out["work_cleanup_at"] = at.timeIntervalSince1970 }
        if let at = task.buildCleanupAt { out["build_cleanup_at"] = at.timeIntervalSince1970 }
        if let v = task.transcriptPath { out["transcript"] = v }
        if task.transcriptProven { out["transcript_proven"] = true }; if let receipt = task.executorReceipt { out["executor"] = Orchestrator.stored(receipt) }
        if let v = task.summary { out["summary"] = v }
        if let delivery = task.completionDelivery {
            out["completion_delivery"] = stored(delivery)
        }
        if let verification = task.verification {
            out["verification"] = Orchestrator.verificationRecord(verification)
        }
        if let review = task.review { out["review"] = Orchestrator.reviewRecord(review) }
        if let usage = task.usage {
            var counts: [String: Any] = ["input": usage.input, "output": usage.output,
                                         "cache_read": usage.cacheRead,
                                         "cache_write": usage.cacheWrite, "total": usage.total]
            if let model = usage.model { counts["model"] = model }
            if let cost = usage.costUsd { counts["cost_usd"] = cost }
            out["usage"] = counts
        }
        return out
    }

    static func stored(_ delivery: Orchestrator.CompletionDelivery) -> [String: Any] {
        var out: [String: Any] = [
            "notice_id": delivery.noticeID,
            "created_at": delivery.created.timeIntervalSince1970,
            "state": delivery.state.rawValue,
            "attempts": delivery.attempts,
            "legacy_reconciled": delivery.legacyReconciled,
        ]
        if let at = delivery.nextRetryAt { out["next_retry_at"] = at.timeIntervalSince1970 }
        if let at = delivery.lastAttemptAt { out["last_attempt_at"] = at.timeIntervalSince1970 }
        if let at = delivery.transportDeliveredAt {
            out["transport_delivered_at"] = at.timeIntervalSince1970
        }
        if let at = delivery.observedAt { out["observed_at"] = at.timeIntervalSince1970 }
        if let at = delivery.acknowledgedAt {
            out["acknowledged_at"] = at.timeIntervalSince1970
        }
        if let failure = delivery.lastError {
            out["last_error"] = [
                "code": failure.code.rawValue, "message": failure.message,
                "at": failure.at.timeIntervalSince1970,
            ] as [String: Any]
        }
        if let at = delivery.deadLetterAt { out["dead_letter_at"] = at.timeIntervalSince1970 }
        return out
    }

    static func completionDelivery(from obj: [String: Any]) -> Orchestrator.CompletionDelivery? {
        func date(_ key: String) -> Date? {
            guard let value = obj[key] as? Double, value.isFinite, value > 0 else { return nil }
            return Date(timeIntervalSince1970: value)
        }
        guard let noticeID = obj["notice_id"] as? String,
              UUID(uuidString: noticeID) != nil,
              let created = date("created_at"),
              let rawState = obj["state"] as? String,
              let state = Orchestrator.CompletionDeliveryState(rawValue: rawState),
              let attempts = obj["attempts"] as? Int, (0...1_000).contains(attempts)
        else { return nil }
        var failure: Orchestrator.CompletionFailure?
        if let row = obj["last_error"] as? [String: Any] {
            guard let rawCode = row["code"] as? String,
                  let code = Orchestrator.CompletionFailureCode(rawValue: rawCode),
                  let message = row["message"] as? String, !message.isEmpty,
                  message.count <= 1_000,
                  let rawAt = row["at"] as? Double, rawAt.isFinite, rawAt > 0 else { return nil }
            failure = Orchestrator.CompletionFailure(code: code, message: message,
                                        at: Date(timeIntervalSince1970: rawAt))
        }
        let next = date("next_retry_at")
        let last = date("last_attempt_at")
        let transported = date("transport_delivered_at")
        let observed = date("observed_at")
        let acknowledged = date("acknowledged_at")
        let dead = date("dead_letter_at")
        guard (state != .acknowledged || (observed != nil && acknowledged != nil && next == nil)),
              (state != .deadLetter || (dead != nil && next == nil)),
              (state != .delivered || transported != nil),
              (obj["next_retry_at"] == nil || next != nil),
              (obj["last_attempt_at"] == nil || last != nil),
              (obj["transport_delivered_at"] == nil || transported != nil),
              (obj["observed_at"] == nil || observed != nil),
              (obj["acknowledged_at"] == nil || acknowledged != nil),
              (obj["dead_letter_at"] == nil || dead != nil),
              (obj["last_error"] == nil || failure != nil) else { return nil }
        return Orchestrator.CompletionDelivery(
            noticeID: noticeID.lowercased(), created: created, state: state,
            attempts: attempts, nextRetryAt: next, lastAttemptAt: last,
            transportDeliveredAt: transported, observedAt: observed,
            acknowledgedAt: acknowledged, lastError: failure, deadLetterAt: dead,
            legacyReconciled: obj["legacy_reconciled"] as? Bool ?? false)
    }

    static func stored(_ envelope: Orchestrator.HandoffEnvelope) -> [String: Any] {
        var out: [String: Any] = [
            "handoff_id": envelope.id,
            "project_dir": envelope.projectDir,
            "created": envelope.created.timeIntervalSince1970,
            "state": envelope.state.rawValue,
        ]
        if let title = envelope.title { out["title"] = title }
        if let from = envelope.fromSession { out["from_session"] = from }
        return out
    }

    static func stored(_ assignment: Orchestrator.RootAssignment) -> [String: Any] {
        var out: [String: Any] = [
            "id": assignment.id, "request_id": assignment.requestID,
            "request_digest": assignment.requestDigest,
            "assistant": assignment.assistant.rawValue, "model": assignment.model,
            "project_dir": assignment.projectDir, "label": assignment.label,
            "objective": assignment.objective, "scope": assignment.scope,
            "constraints": assignment.constraints,
            "relevant_references": assignment.relevantReferences,
            "acceptance": assignment.acceptance,
            "project_approved": assignment.projectApproved,
            "created": assignment.created.timeIntervalSince1970,
            "state": assignment.state.rawValue,
            "inject_attempts": assignment.injectAttempts,
            "answered_trust_menu": assignment.answeredTrustMenu,
        ]
        if let identity = assignment.identity {
            var row: [String: Any] = ["terminal_id": identity.terminalID,
                                      "assistant": identity.assistant.rawValue]
            if let tty = identity.tty { row["tty"] = tty }
            if let pid = identity.pid { row["pid"] = Int(pid) }
            if let start = identity.processStart { row["process_start"] = start }
            if let conversation = identity.conversationID { row["conversation_id"] = conversation }
            out["identity"] = row
        }
        if let language = assignment.language { out["language"] = ["tag": language.tag, "name": language.name] }
        func put(_ key: String, _ value: Date?) {
            if let value { out[key] = value.timeIntervalSince1970 }
        }
        put("terminal_opened_at", assignment.terminalOpenedAt)
        put("prompt_ready_at", assignment.promptReadyAt)
        put("prompt_timeout_started_at", assignment.promptTimeoutStartedAt)
        put("briefed_at", assignment.briefedAt)
        put("active_at", assignment.activeAt)
        put("ended_at", assignment.endedAt)
        put("last_inject_at", assignment.lastInjectAt)
        put("missing_observed_at", assignment.missingObservedAt)
        if let blocker = assignment.blocker { out["blocker"] = blocker }
        if let failure = assignment.failure { out["failure"] = failure }
        if let reconciliation = assignment.reconciliation {
            out["reconciliation"] = reconciliation
        }
        if let transition = assignment.reportedTransition {
            out["reported_transition"] = transition
        }
        if let generation = assignment.missingGeneration {
            out["missing_generation"] = generation
        }
        if let epoch = assignment.missingEpoch { out["missing_epoch"] = epoch }
        return out
    }

    static func stored(_ wait: Orchestrator.CoordinationWait) -> [String: Any] {
        [
            "id": wait.id, "repository": wait.repository, "paths": wait.paths,
            "owner_session_id": wait.ownerSessionID,
            "release_condition": wait.releaseCondition,
            "created": wait.created.timeIntervalSince1970,
            "waiters": wait.waiters.map { waiter -> [String: Any] in
                var row: [String: Any] = [
                    "session_id": waiter.sessionID, "reason": waiter.reason,
                    "created": waiter.created.timeIntervalSince1970,
                ]
                if let at = waiter.requestDeliveredAt {
                    row["request_delivered_at"] = at.timeIntervalSince1970
                }
                if let at = waiter.releaseDeliveredAt {
                    row["release_delivered_at"] = at.timeIntervalSince1970
                }
                return row
            },
        ]
    }

    static func stored(_ delivery: Orchestrator.SessionDelivery) -> [String: Any] {
        var out: [String: Any] = [
            "terminal_id": delivery.identity.terminalID,
            "tty": delivery.identity.tty,
            "summary": delivery.summary,
            "reported_at": delivery.reportedAt.timeIntervalSince1970,
            "settled": delivery.settled,
        ]
        if let assistant = delivery.identity.assistant { out["assistant"] = assistant.rawValue }
        if let pid = delivery.identity.pid { out["pid"] = Int(pid) }
        if let start = delivery.identity.processStart {
            out["process_start"] = start.timeIntervalSince1970
        }
        if let conversation = delivery.identity.conversationID {
            out["conversation_id"] = conversation
        }
        return out
    }

    static func sessionDelivery(from obj: [String: Any]) -> Orchestrator.SessionDelivery? {
        guard let terminalID = obj["terminal_id"] as? String, !terminalID.isEmpty,
              terminalID.count <= 512,
              let assistantName = obj["assistant"] as? String,
              let assistant = Assistant(rawValue: assistantName),
              let tty = obj["tty"] as? String, !tty.isEmpty, tty.count <= 512,
              let pidValue = obj["pid"] as? Int, let pid = Int32(exactly: pidValue),
              let processStart = obj["process_start"] as? Double,
              let conversation = obj["conversation_id"] as? String,
              !conversation.isEmpty, conversation.count <= 512,
              let summary = obj["summary"] as? String,
              !summary.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              summary.count <= Orchestrator.sessionDeliverySummaryLimit,
              !summary.unicodeScalars.contains(where: { $0.value == 0 }),
              let reportedAt = obj["reported_at"] as? Double,
              let settled = obj["settled"] as? Bool else { return nil }
        let identity = Orchestrator.SessionWorkIdentity(
            terminalID: terminalID, assistant: assistant, tty: tty, pid: pid,
            processStart: Date(timeIntervalSince1970: processStart),
            conversationID: conversation)
        return Orchestrator.SessionDelivery(identity: identity, summary: summary,
                               reportedAt: Date(timeIntervalSince1970: reportedAt),
                               settled: settled)
    }

    static func stored(_ selfState: Orchestrator.SessionSelfState) -> [String: Any] {
        var out: [String: Any] = [
            "terminal_id": selfState.identity.terminalID,
            "tty": selfState.identity.tty,
            "claim_settled": selfState.claimSettled,
        ]
        if let assistant = selfState.identity.assistant { out["assistant"] = assistant.rawValue }
        if let pid = selfState.identity.pid { out["pid"] = Int(pid) }
        if let start = selfState.identity.processStart {
            out["process_start"] = start.timeIntervalSince1970
        }
        if let conversation = selfState.identity.conversationID {
            out["conversation_id"] = conversation
        }
        if let claim = selfState.claim { out["claim"] = claim.rawValue }
        if let note = selfState.note { out["note"] = note }
        if let movedBy = selfState.movedBy { out["moved_by"] = movedBy }
        if let personNeeded = selfState.personNeeded { out["person_needed"] = personNeeded }
        if let reportedAt = selfState.claimReportedAt {
            out["claim_reported_at"] = reportedAt.timeIntervalSince1970
        }
        if let debt = selfState.owed {
            var owed: [String: Any] = ["note": debt.note,
                                       "person_needed": debt.personNeeded,
                                       "since": debt.since.timeIntervalSince1970]
            if let movedBy = debt.movedBy { owed["moved_by"] = movedBy }
            out["owed"] = owed
        }
        return out
    }

    static func sessionSelfState(from obj: [String: Any]) -> Orchestrator.SessionSelfState? {
        guard let terminalID = obj["terminal_id"] as? String, !terminalID.isEmpty,
              terminalID.count <= 512,
              let assistantName = obj["assistant"] as? String,
              let assistant = Assistant(rawValue: assistantName),
              let tty = obj["tty"] as? String, !tty.isEmpty, tty.count <= 512,
              let pidValue = obj["pid"] as? Int, let pid = Int32(exactly: pidValue),
              let processStart = obj["process_start"] as? Double,
              let conversation = obj["conversation_id"] as? String,
              !conversation.isEmpty, conversation.count <= 512,
              let claimSettled = obj["claim_settled"] as? Bool else { return nil }
        var claim: Orchestrator.SessionWorkState?
        if let rawClaim = obj["claim"] as? String {
            // Only the two declarable states survive a reload; anything else in the store is a
            // record this code has no business believing.
            guard let parsed = Orchestrator.SessionWorkState(rawValue: rawClaim),
                  parsed == .ready || parsed == .holding else { return nil }
            claim = parsed
        }
        var owed: Orchestrator.OwedDebt?
        if let rawOwed = obj["owed"] as? [String: Any] {
            guard let note = rawOwed["note"] as? String, !note.isEmpty,
                  note.count <= Orchestrator.sessionSelfNoteLimit,
                  let personNeeded = rawOwed["person_needed"] as? Bool,
                  let since = rawOwed["since"] as? Double else { return nil }
            owed = Orchestrator.OwedDebt(note: note, movedBy: rawOwed["moved_by"] as? String,
                            personNeeded: personNeeded,
                            since: Date(timeIntervalSince1970: since))
        }
        guard claim != nil || owed != nil else { return nil }
        let identity = Orchestrator.SessionWorkIdentity(
            terminalID: terminalID, assistant: assistant, tty: tty, pid: pid,
            processStart: Date(timeIntervalSince1970: processStart),
            conversationID: conversation)
        return Orchestrator.SessionSelfState(
            identity: identity, claim: claim, note: obj["note"] as? String,
            movedBy: obj["moved_by"] as? String,
            personNeeded: obj["person_needed"] as? Bool,
            claimReportedAt: (obj["claim_reported_at"] as? Double)
                .map { Date(timeIntervalSince1970: $0) },
            claimSettled: claimSettled, owed: owed)
    }

    static func coordinationWait(from obj: [String: Any]) -> Orchestrator.CoordinationWait? {
        guard let id = obj["id"] as? String, OrchestratorDraft.isTaskID(id),
              let repositoryRaw = obj["repository"] as? String,
              let repository = Orchestrator.canonicalCoordinationRepository(repositoryRaw),
              let pathsRaw = obj["paths"] as? [String],
              let paths = Orchestrator.canonicalCoordinationPaths(pathsRaw, repository: repository),
              let owner = Orchestrator.boundedCoordinationText(obj["owner_session_id"], limit: 512),
              let condition = Orchestrator.boundedCoordinationText(obj["release_condition"], limit: 1_000),
              let created = obj["created"] as? Double else { return nil }
        var seenWaiters = Set<String>()
        let waiters: [Orchestrator.CoordinationWaiter] = (obj["waiters"] as? [[String: Any]] ?? []).compactMap {
            row in
            guard let session = Orchestrator.boundedCoordinationText(row["session_id"], limit: 512),
                  seenWaiters.insert(session).inserted,
                  let reason = Orchestrator.boundedCoordinationText(row["reason"], limit: 1_000),
                  let made = row["created"] as? Double else { return nil }
            return Orchestrator.CoordinationWaiter(
                sessionID: session, reason: reason, created: Date(timeIntervalSince1970: made),
                requestDeliveredAt: (row["request_delivered_at"] as? Double)
                    .map(Date.init(timeIntervalSince1970:)),
                releaseDeliveredAt: (row["release_delivered_at"] as? Double)
                    .map(Date.init(timeIntervalSince1970:)))
        }
        guard !waiters.isEmpty else { return nil }
        return Orchestrator.CoordinationWait(id: id, repository: repository, paths: paths,
                                ownerSessionID: owner, releaseCondition: condition,
                                created: Date(timeIntervalSince1970: created), waiters: waiters)
    }

    static func landing(from obj: [String: Any]) -> Orchestrator.Landing? {
        guard let state = (obj["state"] as? String).flatMap(Orchestrator.LandingState.init(rawValue:)),
              let owner = obj["owner_root_key"] as? String, owner.count == 8,
              owner.allSatisfy({ ("0"..."9").contains($0) || ("a"..."f").contains($0) }),
              let since = obj["since"] as? Double else { return nil }
        func text(_ key: String, limit: Int) -> String? {
            guard let value = obj[key] as? String,
                  !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  value.count <= limit else { return nil }
            return value
        }
        let target = text("target", limit: 200)
        let delivery = text("delivery", limit: 500)
        let commit = text("commit", limit: 200)
        let note = text("note", limit: 500)
        let landedAt = (obj["landed_at"] as? Double).map(Date.init(timeIntervalSince1970:))
        let verificationOrigin = text("verification_origin", limit: 100)
        let verifiedCommit = text("verified_commit", limit: 200)
        let verifiedTargetCommit = text("verified_target_commit", limit: 200)
        let verificationValues: [String?] = [verificationOrigin, verifiedCommit,
                                              verifiedTargetCommit]
        let hasAnyVerification = verificationValues.contains { $0 != nil }
        let hasCompleteVerification = verificationOrigin == "local_target_branch"
            && verifiedCommit == commit
            && [verifiedCommit, verifiedTargetCommit].allSatisfy { value in
                guard let value else { return false }
                return (value.count == 40 || value.count == 64)
                    && value.allSatisfy {
                        ("0"..."9").contains($0) || ("a"..."f").contains($0)
                    }
            }
        guard (obj["target"] == nil || target != nil),
              (obj["delivery"] == nil || delivery != nil),
              (obj["commit"] == nil || commit != nil),
              (obj["note"] == nil || note != nil),
              (obj["landed_at"] == nil || landedAt != nil),
              (obj["verification_origin"] == nil || verificationOrigin != nil),
              (obj["verified_commit"] == nil || verifiedCommit != nil),
              (obj["verified_target_commit"] == nil || verifiedTargetCommit != nil),
              (landedAt == nil || state == .landed),
              (state == .landed) == (commit != nil),
              !hasAnyVerification || (state == .landed && target != nil
                                      && hasCompleteVerification) else { return nil }
        return Orchestrator.Landing(state: state, target: target, delivery: delivery,
                       ownerRootKey: owner, since: Date(timeIntervalSince1970: since),
                       commit: commit, note: note, landedAt: landedAt,
                       verificationOrigin: verificationOrigin,
                       verifiedCommit: verifiedCommit,
                       verifiedTargetCommit: verifiedTargetCommit)
    }

    static func handoff(from obj: [String: Any]) -> Orchestrator.HandoffEnvelope? {
        guard let id = obj["handoff_id"] as? String, OrchestratorDraft.isTaskID(id),
              let projectDir = obj["project_dir"] as? String, StartPoints.usable(projectDir),
              let created = obj["created"] as? Double,
              let state = (obj["state"] as? String).flatMap(Orchestrator.HandoffState.init(rawValue:))
        else { return nil }
        let title = (obj["title"] as? String).flatMap { $0.count <= 200 ? $0 : nil }
        let from = (obj["from_session"] as? String).flatMap { $0.count <= 200 ? $0 : nil }
        return Orchestrator.HandoffEnvelope(id: id, projectDir: projectDir, title: title,
                               fromSession: from, created: Date(timeIntervalSince1970: created),
                               state: state)
    }

    static func rootAssignment(from obj: [String: Any]) -> Orchestrator.RootAssignment? {
        func text(_ key: String, _ limit: Int = 8_192) -> String? {
            guard let value = obj[key] as? String, !value.isEmpty,
                  value.utf8.count <= limit else { return nil }
            return value
        }
        guard let id = text("id", 64), OrchestratorDraft.isTaskID(id),
              let requestID = text("request_id", 64), OrchestratorDraft.isTaskID(requestID),
              let digest = text("request_digest", 128), digest.count == 64,
              let assistant = text("assistant", 16).flatMap(Assistant.init(rawValue:)),
              let model = text("model", 64),
              model == "default" || StartPoints.modelName(model) == model,
              let projectDir = text("project_dir", 4_096), StartPoints.usable(projectDir),
              let label = text("label", 200),
              let objective = text("objective"), let scope = text("scope"),
              let constraints = text("constraints"),
              let references = text("relevant_references"),
              let acceptance = text("acceptance"),
              let created = obj["created"] as? Double,
              let state = text("state", 32).flatMap(Orchestrator.RootAssignmentState.init(rawValue:)) else {
            return nil
        }
        var assignment = Orchestrator.RootAssignment(
            id: id, requestID: requestID, requestDigest: digest, assistant: assistant,
            model: model, projectDir: projectDir, label: label, objective: objective,
            scope: scope, constraints: constraints, relevantReferences: references,
            acceptance: acceptance, projectApproved: obj["project_approved"] as? Bool ?? false,
            created: Date(timeIntervalSince1970: created), state: state, language: nil)
        if let row = obj["language"] as? [String: Any],
           let tag = row["tag"] as? String, !tag.isEmpty, tag.utf8.count <= 64,
           let name = row["name"] as? String, !name.isEmpty, name.utf8.count <= 200,
           !tag.contains("\0"), !name.contains("\0"),
           L.catalog.contains(where: { $0.tag == tag }) {
            assignment.language = Orchestrator.RootAssignmentLanguage(tag: tag, name: name)
        }
        if let row = obj["identity"] as? [String: Any],
           let terminal = row["terminal_id"] as? String,
           let identityAssistant = (row["assistant"] as? String).flatMap(Assistant.init(rawValue:)),
           identityAssistant == assistant {
            assignment.identity = Orchestrator.RootAssignmentIdentity(
                terminalID: terminal, assistant: identityAssistant, tty: row["tty"] as? String,
                pid: (row["pid"] as? Int).map(Int32.init),
                processStart: row["process_start"] as? Double,
                conversationID: row["conversation_id"] as? String)
        }
        func date(_ key: String) -> Date? {
            (obj[key] as? Double).map(Date.init(timeIntervalSince1970:))
        }
        assignment.terminalOpenedAt = date("terminal_opened_at")
        assignment.promptReadyAt = date("prompt_ready_at")
        assignment.promptTimeoutStartedAt = date("prompt_timeout_started_at")
        assignment.briefedAt = date("briefed_at")
        assignment.activeAt = date("active_at")
        assignment.endedAt = date("ended_at")
        assignment.lastInjectAt = date("last_inject_at")
        assignment.missingObservedAt = date("missing_observed_at")
        assignment.missingGeneration = obj["missing_generation"] as? Int
        assignment.missingEpoch = obj["missing_epoch"] as? String
        assignment.injectAttempts = min(max(obj["inject_attempts"] as? Int ?? 0, 0),
                                        Orchestrator.briefingAttemptLimit)
        assignment.answeredTrustMenu = obj["answered_trust_menu"] as? Bool ?? false
        assignment.blocker = obj["blocker"] as? String
        assignment.failure = obj["failure"] as? String
        assignment.reconciliation = obj["reconciliation"] as? String
        assignment.reportedTransition = obj["reported_transition"] as? String
        return assignment
    }

    static func task(from obj: [String: Any]) -> Orchestrator.Task? {
        guard let id = obj["id"] as? String, OrchestratorDraft.isTaskID(id),
              let state = (obj["state"] as? String).flatMap(Orchestrator.State.init(rawValue:)),
              let assistant = (obj["assistant"] as? String).flatMap(Assistant.init(rawValue:)),
              let projectDir = obj["project_dir"] as? String,
              let created = obj["created"] as? Double,
              let secretHash = obj["secret_hash"] as? String else { return nil }
        var task = Orchestrator.Task(id: id, state: state,
                        kind: obj["kind"] as? String ?? "custom",
                        title: obj["title"] as? String ?? "task",
                        assistant: assistant, projectDir: projectDir,
                        timeoutMinutes: obj["timeout_minutes"] as? Int ?? 30,
                        created: Date(timeIntervalSince1970: created),
                        secretHash: secretHash)
        task.spawnedAt = (obj["spawned_at"] as? Double).map(Date.init(timeIntervalSince1970:))
        task.briefedAt = (obj["briefed_at"] as? Double).map(Date.init(timeIntervalSince1970:))
        task.finishedAt = (obj["finished_at"] as? Double).map(Date.init(timeIntervalSince1970:))
        task.resultVerifiedAt = (obj["result_verified_at"] as? Double)
            .map(Date.init(timeIntervalSince1970:))
        task.rootSessionId = obj["root_session"] as? String
        task.rootAssistant = (obj["root_assistant"] as? String).flatMap(Assistant.init(rawValue:))
        task.rootLabel = obj["root_label"] as? String
        if let common = obj["repository_common_dir"] as? String {
            guard StartPoints.usable(common) else { return nil }
            task.repositoryCommonDir = OrchestratorDraft.canonicalFilesystemPath(common)
        }
        task.parentTaskId = obj["parent_task"] as? String
        // A chain position is only meaningful beside the task it descends from, so a row missing
        // one is missing both: a generation with no origin would count against a cap for a chain
        // nothing can name.
        task.respawnOf = (obj["respawn_of"] as? String)
            .flatMap { OrchestratorDraft.isTaskID($0) ? $0 : nil }
        task.respawnGeneration = task.respawnOf == nil
            ? 0
            : min(max(obj["respawn_generation"] as? Int ?? 1, 1), Orchestrator.respawnLimit)
        task.model = StartPoints.modelName(obj["model"] as? String)
        task.reasoningEffort = assistant == .codex
            ? (obj["reasoning_effort"] as? String).flatMap(ReasoningEffort.init(rawValue:))
            : nil
        task.permission = (obj["permission"] as? String).flatMap(Permission.init(rawValue:)) ?? .ask
        task.plan = obj["plan"] as? String
        if let rawGraph = obj["graph"] {
            task.graph = Orchestrator.planningGraph(from: rawGraph).graph
        }
        task.scheduleID = (obj["schedule_id"] as? String)
            .flatMap { OrchestratorDraft.isTaskID($0) ? $0 : nil }
        task.scheduleCloseTab = (obj["schedule_close_tab"] as? String)
            .flatMap(Orchestrator.ScheduleCloseTab.init(rawValue:)) ?? .onSuccess
        task.scheduleNotifyFailure = obj["schedule_notify_failure"] as? Bool ?? true
        task.serialize = (obj["serialize"] as? [String] ?? []).filter {
            StartPoints.modelName($0) == $0
        }
        let rawClaims = obj["claims"] as? [String]
        task.claims = (rawClaims ?? []).filter { path in
            !path.isEmpty && path.count <= 1_024 && !path.hasPrefix("/")
                && !path.split(separator: "/", omittingEmptySubsequences: false)
                    .contains(where: { $0 == ".." })
                && !path.unicodeScalars.contains(where: { $0.value == 0 })
        }
        task.claimsDeclared = rawClaims.map { $0.count == task.claims.count } ?? false
        let storedClaimKeys = (obj["claim_keys"] as? [String] ?? []).filter {
            $0.hasPrefix("/")
        }
        task.claimKeys = storedClaimKeys.count == task.claims.count
            ? storedClaimKeys
            : OrchestratorDraft.freezeClaims(task.claims, projectDir: task.projectDir)
        task.releasedClaims = (obj["released_claims"] as? [[String: Any]] ?? []).compactMap { row in
            guard let path = row["path"] as? String, path.hasPrefix("/"),
                  let releasedAt = row["released_at"] as? Double else { return nil }
            return Orchestrator.ReleasedClaim(path: path, releasedAt: Date(timeIntervalSince1970: releasedAt))
        }
        task.untouchedClaims = (obj["untouched_claims"] as? [String] ?? []).filter {
            task.claims.contains($0)
        }
        if let rawLanding = obj["landing"] as? [String: Any] {
            task.landing = landing(from: rawLanding)
            if task.landing == nil {
                Log.write("orchestrator: ignored invalid landing record for task \(task.id)")
            }
        }
        task.progress = progress(from: obj["progress"])
        task.progressFileNote = (obj["progress_file_note"] as? String).flatMap {
            !$0.isEmpty && $0.count <= Orchestrator.progressLimit ? $0 : nil
        }
        task.isolation = (obj["isolation"] as? String).flatMap(Orchestrator.Isolation.init(rawValue:)) ?? .none
        if task.isolation == .worktree {
            guard let raw = obj["worktree"] as? [String: Any],
                  let path = raw["path"] as? String, StartPoints.usable(path),
                  let branch = raw["branch"] as? String,
                  branch == OrchestratorDraft.worktreeBranch(for: task.id),
                  let base = raw["base"] as? String, !base.isEmpty,
                  let repository = raw["repository"] as? String, StartPoints.usable(repository),
                  path == OrchestratorDraft.worktreePath(project: repository, taskID: task.id),
                  let cwd = raw["cwd"] as? String, StartPoints.usable(cwd),
                  OrchestratorDraft.relativePath(from: path, to: cwd) != nil else { return nil }
            let requestedBase = raw["requested_base"] as? String ?? "HEAD"
            guard OrchestratorDraft.validIsolationBase(requestedBase),
                  (base.count == 40 || base.count == 64),
                  base.allSatisfy({ ("0"..."9").contains($0) || ("a"..."f").contains($0) })
            else { return nil }
            var worktree = Orchestrator.Worktree(path: path, branch: branch, base: base,
                                    repository: repository, cwd: cwd)
            if let common = raw["repository_common_dir"] as? String {
                guard StartPoints.usable(common) else { return nil }
                worktree.repositoryCommonDir = OrchestratorDraft.canonicalFilesystemPath(common)
            }
            worktree.head = raw["head"] as? String
            worktree.commits = raw["commits"] as? Int
            worktree.dirty = raw["dirty"] as? Bool
            worktree.baseDirty = raw["base_dirty"] as? Int ?? 0
            worktree.requestedBase = requestedBase
            task.worktree = worktree
            // Migrate the old nested receipt into the task-wide source of truth on the next save.
            if task.repositoryCommonDir == nil {
                task.repositoryCommonDir = worktree.repositoryCommonDir
            }
        }
        task.queuedSecret = obj["queued_secret"] as? String
        task.attachSessionId = (obj["attach_session"] as? String).flatMap {
            !$0.isEmpty && $0.count <= 512 ? $0 : nil
        }
        // A registry written before tasks had a depth holds only tasks a root dispatched, which
        // is exactly what 1 means.
        task.depth = (obj["depth"] as? Int).map { min(max($0, 1), 9) } ?? 1
        task.childTerminalId = obj["child_terminal"] as? String
        task.childBackend = (obj["child_backend"] as? String).flatMap(Backend.init(rawValue:))
        task.childTaskRootAccess = obj["child_task_root_access"] as? Bool == true
        task.childTTY = obj["child_tty"] as? String
        task.childPID = (obj["child_pid"] as? Int).flatMap(Int32.init(exactly:))
        task.childProcStart = (obj["child_proc_start"] as? Double)
            .map(Date.init(timeIntervalSince1970:))
        task.childSessionId = obj["child_session"] as? String
        task.closeAt = (obj["close_at"] as? Double).map(Date.init(timeIntervalSince1970:))
        if let raw = obj["terminal_intervention"] as? [String: Any],
           let kind = (raw["kind"] as? String).flatMap(Orchestrator.TerminalInterventionKind.init(rawValue:)),
           let message = raw["message"] as? String, !message.isEmpty {
            task.terminalIntervention = Orchestrator.TerminalIntervention(kind: kind, message: message)
        } else if let legacy = obj["terminal_intervention"] as? String, !legacy.isEmpty {
            // Compatibility for the short-lived crash-recovery store that persisted only prose.
            // New rows never infer type from text; this branch exists solely to retain its reason.
            let modal = task.childBackend == .iterm && legacy.contains("iTerm2 needs attention")
            task.terminalIntervention = Orchestrator.TerminalIntervention(
                kind: modal ? .iTermModal : .terminal, message: legacy)
        }
        task.workCleanupAt = (obj["work_cleanup_at"] as? Double)
            .map(Date.init(timeIntervalSince1970:))
        task.buildCleanupAt = (obj["build_cleanup_at"] as? Double)
            .map(Date.init(timeIntervalSince1970:))
        task.transcriptPath = obj["transcript"] as? String
        task.transcriptProven = obj["transcript_proven"] as? Bool == true && task.childSessionId != nil && task.transcriptPath != nil
        if let raw = obj["executor"] as? [String: Any] {
            task.executorReceipt = Orchestrator.executorReceipt(from: raw)
        }
        task.notifyCount = min(max(obj["notify_count"] as? Int ?? 0, 0), Orchestrator.notifyTaskLimit)
        if task.transcriptProven, task.assistant == .claude,
           let path = task.transcriptPath, let sessionID = task.childSessionId,
           URL(fileURLWithPath: path).deletingPathExtension().lastPathComponent != sessionID {
            // A proof belongs to the path/session pair, not merely to the row. A malformed pair
            // is retained for diagnosis but cannot participate in cascade ownership; usage may
            // still independently re-prove the path from its task marker.
            task.transcriptProven = false
        }
        task.summary = obj["summary"] as? String
        if let rawDelivery = obj["completion_delivery"] as? [String: Any] {
            task.completionDelivery = completionDelivery(from: rawDelivery)
            if task.completionDelivery == nil {
                Log.write("orchestrator: ignored invalid completion delivery for task \(task.id)")
            }
        }
        task.artifacts = obj["artifacts"] as? [String] ?? []
        task.verification = Orchestrator.verification(from: obj["verification"])
        task.review = Orchestrator.review(from: obj["review"])
        if let counts = obj["usage"] as? [String: Any] {
            var usage = Orchestrator.Usage()
            usage.input = counts["input"] as? Int ?? 0
            usage.output = counts["output"] as? Int ?? 0
            usage.cacheRead = counts["cache_read"] as? Int ?? 0
            usage.cacheWrite = counts["cache_write"] as? Int ?? 0
            usage.total = counts["total"] as? Int ?? 0
            usage.model = counts["model"] as? String
            usage.costUsd = counts["cost_usd"] as? Double
            task.usage = usage
        }
        return task
    }

}
