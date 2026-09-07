import Foundation

extension Orchestrator {
    /// Git evidence the broker proved for one root Session's current delivered turn. The
    /// repository path is the canonical common Git directory derived from the watched process's
    /// cwd, never a request field; keeping it in the durable receipt makes repository identity a
    /// recorded fact rather than something reconstructed later from whichever cwd the tab has.
    struct SessionLanding: Equatable {
        let repositoryCommonDir: String
        let verificationOrigin: String
        let target: String
        let verifiedCommit: String
        let verifiedTargetCommit: String
        let landedAt: Date
    }

    /// One coherent watcher publication. Git is allowed to run only between two complete values
    /// of this shape; no identity, state or cwd field is sampled from a different generation.
    struct SessionLandingObservation: Equatable {
        let identity: SessionWorkIdentity
        let terminalState: SessionState
        let repositoryDirectory: String?
        let publicationGeneration: Int
        let publicationEpoch: String
    }

    private struct SessionLandingAssignmentSnapshot: Equatable {
        let id: String
        let state: String
        let projectDir: String
        let terminalID: String
        let assistant: Assistant
        let tty: String?
        let pid: Int32?
        let processStart: Double?
        let conversationID: String?
    }

    private struct SessionLandingChildSnapshot: Equatable {
        let id: String
        let state: String
        let assistant: Assistant
        let terminalID: String?
        let tty: String?
        let pid: Int32?
        let processStart: Date?
        let conversationID: String?
        let transcriptProven: Bool
    }

    /// Timing-only test seam: production leaves it nil. A route test changes one durable/live fact
    /// here, exactly after Git answered and before the second watcher/CAS pass.
    static var sessionLandingDidVerifyForTesting: (() -> Void)?
    /// Mutation seams used only to prove the new tests go red against the pre-correction behavior.
    static var sessionLandingSkipPostGitValidationForTesting = false
    static var sessionLandingCompareTargetTipForTesting = false

    static func resetSessionLandingTesting() {
        sessionLandingDidVerifyForTesting = nil
        sessionLandingSkipPostGitValidationForTesting = false
        sessionLandingCompareTargetTipForTesting = false
        RemoteServer.sessionLandingObservationForTesting = nil
    }

    static func sessionDeliveryDisposition(_ delivery: SessionDelivery) -> [String: Any] {
        var disposition: [String: Any] = [
            "scope": "session", "title": delivery.summary,
            "evidence": "authenticated_session_delivery",
            "receiptAt": Int(delivery.reportedAt.timeIntervalSince1970),
        ]
        if let landing = delivery.landing, isBrokerVerifiedSessionLanding(landing) {
            disposition["evidence"] = "broker_verified_target_landing"
            disposition["commit"] = landing.verifiedCommit
            disposition["target"] = landing.target
            disposition["targetCommit"] = landing.verifiedTargetCommit
            disposition["landedAt"] = Int(landing.landedAt.timeIntervalSince1970)
        }
        return disposition
    }

    static func isBrokerVerifiedSessionLanding(_ landing: SessionLanding) -> Bool {
        guard landing.verificationOrigin == "local_target_branch",
              landing.repositoryCommonDir.hasPrefix("/"),
              OrchestratorDraft.canonicalFilesystemPath(landing.repositoryCommonDir)
                == landing.repositoryCommonDir,
              landing.target.count > 0 && landing.target.count <= 200 else { return false }
        return [landing.verifiedCommit, landing.verifiedTargetCommit].allSatisfy { id in
            (id.count == 40 || id.count == 64)
                && id.allSatisfy { ("0"..."9").contains($0) || ("a"..."f").contains($0) }
        }
    }

    private static func landingAssignmentSnapshot(for terminalID: String,
                                                  assignments: [RootAssignment])
        -> [SessionLandingAssignmentSnapshot] {
        assignments.compactMap { assignment in
            guard let identity = assignment.identity, identity.terminalID == terminalID else {
                return nil
            }
            return SessionLandingAssignmentSnapshot(
                id: assignment.id, state: assignment.state.rawValue,
                projectDir: assignment.projectDir, terminalID: identity.terminalID,
                assistant: identity.assistant, tty: identity.tty, pid: identity.pid,
                processStart: identity.processStart, conversationID: identity.conversationID)
        }.sorted { $0.id < $1.id }
    }

    private static func landingChildSnapshot(for terminalID: String, tasks: [Task])
        -> [SessionLandingChildSnapshot] {
        tasks.compactMap { task in
            guard task.childTerminalId == terminalID else { return nil }
            return SessionLandingChildSnapshot(
                id: task.id, state: task.state.rawValue, assistant: task.assistant,
                terminalID: task.childTerminalId, tty: task.childTTY, pid: task.childPID,
                processStart: task.childProcStart, conversationID: task.childSessionId,
                transcriptProven: task.transcriptProven)
        }.sorted { $0.id < $1.id }
    }

    /// Upgrade this current root turn's delivery to a broker-verified target landing. The first
    /// watcher publication and every durable authorizer become the expected CAS value. After Git,
    /// a second coherent publication and the exact durable values must still match before writing.
    static func reportSessionLanding(observation: SessionLandingObservation,
                                     summary rawSummary: String,
                                     target rawTarget: String,
                                     commit rawCommit: String,
                                     reobserve: () -> SessionLandingObservation?,
                                     now: Date = Date()) -> Reply {
        load()
        guard case .working = observation.terminalState else {
            return .refused(409, "session_not_working",
                            "A root may report landing only while its current turn is working.")
        }
        let identity = observation.identity
        guard identity.assistant != nil, identity.pid != nil, identity.processStart != nil,
              identity.conversationID != nil else {
            return .refused(409, "session_unbound",
                            "The current assistant process and conversation could not be bound.")
        }
        let summary = rawSummary.trimmingCharacters(in: .whitespacesAndNewlines)
        let target = rawTarget.trimmingCharacters(in: .whitespacesAndNewlines)
        let commit = rawCommit.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !summary.isEmpty, summary.count <= sessionDeliverySummaryLimit,
              !summary.unicodeScalars.contains(where: { $0.value == 0 }),
              !target.isEmpty, target.count <= 200,
              !target.unicodeScalars.contains(where: { $0.value == 0 }),
              !commit.isEmpty, commit.count <= 200,
              !commit.unicodeScalars.contains(where: { $0.value == 0 }) else {
            return .refused(400, "bad_request",
                            "summary, target and commit must be non-empty bounded strings.")
        }

        guard let repositoryDirectory = observation.repositoryDirectory else {
            return .refused(409, "session_repository_unbound",
                            "The current Session has no process-bound working directory.")
        }

        lock.lock()
        let expectedChildren = landingChildSnapshot(
            for: identity.terminalID, tasks: Array(tasks.values))
        if expectedChildren.contains(where: { child in
            tasks[child.id].map { taskMatchesCurrentSession($0, identity: identity) } == true
        }) {
            lock.unlock()
            return .refused(409, "child_session",
                            "A Clawdline child reports through its task landing, not this route.")
        }
        let assignments = Array(rootAssignments.values)
        let expectedAssignments = landingAssignmentSnapshot(
            for: identity.terminalID, assignments: assignments)
        let expectedReceipt = sessionDeliveries[identity.terminalID]
        lock.unlock()

        guard let repositoryCommonDir = OrchestratorDraft.gitCommonDirectory(
                at: repositoryDirectory) else {
            return .refused(409, "session_repository_unbound",
                            "The current Session directory is not in a readable local Git repository.")
        }
        let matchingAssignments = assignments.filter { assignment in
            guard let stored = assignment.identity,
                  ![.failed, .inactive].contains(assignment.state) else { return false }
            return rootAssignmentIdentityMatches(stored, observed: identity)
        }
        guard matchingAssignments.count <= 1 else {
            return .refused(409, "session_repository_ambiguous",
                            "More than one durable root assignment matches this Session.")
        }
        if let assignment = matchingAssignments.first {
            guard let assignedCommonDir = OrchestratorDraft.gitCommonDirectory(
                    at: assignment.projectDir), assignedCommonDir == repositoryCommonDir else {
                return .refused(409, "session_repository_mismatch",
                                "The live Session repository differs from its durable root assignment.")
            }
        }
        guard let verification = OrchestratorDraft.verifyTargetLanding(
                gitDirectory: repositoryCommonDir, target: target, commit: commit) else {
            RemoteAuth.audit("orchestrator.session.landing", [
                "session": identity.terminalID, "ok": "0", "why": "target_not_verified",
                "target": target,
            ])
            return .refused(409, "unverified_landing",
                            "The commit must resolve in the Session repository and be contained "
                                + "by the named local target branch.")
        }
        let landing = SessionLanding(
            repositoryCommonDir: repositoryCommonDir,
            verificationOrigin: verification.origin, target: target,
            verifiedCommit: verification.commit,
            verifiedTargetCommit: verification.targetCommit, landedAt: now)

        sessionLandingDidVerifyForTesting?()
        if !sessionLandingSkipPostGitValidationForTesting {
            guard let current = reobserve() else {
                return .refused(409, "session_changed",
                                "The current Session disappeared while landing was verified.")
            }
            guard current.publicationEpoch == observation.publicationEpoch,
                  current.publicationGeneration == observation.publicationGeneration,
                  current.identity == identity,
                  case .working = current.terminalState else {
                return .refused(409, "session_changed",
                                "The Session identity, turn, or publication changed during Git verification.")
            }
            guard let currentDirectory = current.repositoryDirectory,
                  let currentCommonDir = OrchestratorDraft.gitCommonDirectory(at: currentDirectory),
                  currentCommonDir == repositoryCommonDir else {
                return .refused(409, "session_repository_changed",
                                "The Session repository changed during Git verification.")
            }
        }

        lock.lock()
        if !sessionLandingSkipPostGitValidationForTesting {
            let currentChildren = landingChildSnapshot(
                for: identity.terminalID, tasks: Array(tasks.values))
            guard currentChildren == expectedChildren else {
                lock.unlock()
                return .refused(409, "child_session",
                                "The Session's child binding changed while landing was verified.")
            }
            let currentAssignments = landingAssignmentSnapshot(
                for: identity.terminalID, assignments: Array(rootAssignments.values))
            guard currentAssignments == expectedAssignments else {
                lock.unlock()
                return .refused(409, "root_assignment_changed",
                                "The Session's durable Root Assignment changed during Git verification.")
            }
            guard sessionDeliveries[identity.terminalID] == expectedReceipt else {
                lock.unlock()
                return .refused(409, "receipt_changed",
                                "The Session delivery receipt changed during Git verification.")
            }
        }
        if let existing = sessionDeliveries[identity.terminalID],
           sessionDeliveryMatchesCurrentSession(existing, identity: identity) {
            if existing.settled {
                lock.unlock()
                return .refused(409, "receipt_stale",
                                "That delivery belongs to an already settled turn.")
            }
            if existing.summary != summary {
                lock.unlock()
                return .refused(status: 409, code: "landing_conflict",
                                message: "This turn already has a different delivery summary.",
                                extra: ["field": "summary"])
            }
            if let stored = existing.landing {
                let same = stored.repositoryCommonDir == landing.repositoryCommonDir
                    && stored.verificationOrigin == landing.verificationOrigin
                    && stored.target == landing.target
                    && stored.verifiedCommit == landing.verifiedCommit
                    && (!sessionLandingCompareTargetTipForTesting
                        || stored.verifiedTargetCommit == landing.verifiedTargetCommit)
                if same {
                    let disposition = sessionDeliveryDisposition(existing)
                    lock.unlock()
                    return .ok(["ok": true, "created": false,
                                "disposition": disposition])
                }
                lock.unlock()
                return .refused(status: 409, code: "landing_conflict",
                                message: "This turn already has different verified landing evidence.",
                                extra: ["field": "landing"])
            }
            var upgraded = existing
            upgraded.landing = landing
            sessionDeliveries[identity.terminalID] = upgraded
            let disposition = sessionDeliveryDisposition(upgraded)
            lock.unlock()
            save()
            RemoteAuth.audit("orchestrator.session.landing", [
                "session": identity.terminalID, "ok": "1", "target": target,
                "verified_commit": verification.commit,
                "verified_target_commit": verification.targetCommit,
            ])
            return .ok(["ok": true, "created": true, "disposition": disposition])
        }
        let made = SessionDelivery(identity: identity, summary: summary,
                                   reportedAt: now, settled: false, landing: landing)
        sessionDeliveries[identity.terminalID] = made
        let disposition = sessionDeliveryDisposition(made)
        lock.unlock()
        save()
        RemoteAuth.audit("orchestrator.session.landing", [
            "session": identity.terminalID, "ok": "1", "target": target,
            "verified_commit": verification.commit,
            "verified_target_commit": verification.targetCommit,
        ])
        announceDelivery(made)
        return .ok(["ok": true, "created": true, "disposition": disposition])
    }
}
