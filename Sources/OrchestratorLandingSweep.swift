import Foundation

/// **The landing record's lock stops only ever being added to.**
///
/// `landed` had exactly one entrance and a person was standing in it: the record moves only when
/// somebody calls `POST /v1/orchestrator/tasks/:id/landing` with the machine credential, and
/// `POST /v1/orchestrator/sessions/:id/complete` deliberately does not close it. So the
/// closeability projection could add `pending_landing_owned` and `touched_claims_without_closure`
/// and never take one away. On 2026-09-06 one root's card read 「還有 21 項未了結」 — 12 pending
/// landings and 9 touched-claim rows, all from twelve children that had reached `success` months
/// of machine-time earlier and whose records nobody had ever closed.
///
/// This is the broker closing, by itself, what it can prove by itself. It proves two different
/// propositions and never lets the weaker one wear the stronger one's clothes:
///
/// - **Arm 1, ancestry.** The delivery's head is contained by `refs/heads/<target>` in the task's
///   own repository. That is the same question `tools/check-landing-records.py` asks and calls
///   *unrecorded landing*, and the same one `OrchestratorDraft.verifyTargetLanding` asks for the
///   HTTP route. The record closes through that exact verified path, so `verification_origin`,
///   `verified_commit`, `verified_target_commit` and `landed_at` are all real and
///   ``Orchestrator/isBrokerVerifiedTargetLanding(_:)`` stays true — this delivery reached the
///   target, and the two checks say so.
/// - **Arm 2, write-set containment.** A shared-checkout task with no delivery branch, whose every
///   declared claim is both unmodified in its project directory and identical to
///   `refs/heads/<target>`. Git cannot be asked about ancestry here — `check-landing-records.py`
///   correctly calls this class `undecidable` — so what is proved is narrower: **nothing this task
///   was allowed to write is outstanding.** The record closes with the target's own commit, a note
///   saying which of the two sentences it rests on, and **no verification fields at all**, so it
///   can never become the two-check `work_complete`.
///
/// **What it refuses, always.** A settled record (a settled state may never move to another one);
/// a task that is not terminal; a record with no named target; `abandoned`, which is a sentence
/// about somebody giving up and not a thing a machine can observe; and `nothing_to_land`, which
/// ``Orchestrator/nothingToLandAdmission(for:declaredWritePaths:)`` refuses for exactly these
/// tasks anyway. A repository git will not answer for is **skipped**, never read as "nothing
/// landed" — the failure mode `check-landing-records.py` names in `contained_commits`.
extension Orchestrator {

    // MARK: - What a pass may settle

    /// Which of the two propositions closed a record. They are different sentences and the record
    /// says which one it rests on, in its note and in whether it carries verification at all.
    enum LandingSweepArm: String, Equatable {
        /// The delivery reached the target branch.
        case ancestry
        /// Nothing of the task's declared write set is outstanding.
        case writeSet = "write_set"
    }

    /// A pending landing this pass may be able to settle, chosen from records alone before git has
    /// been asked anything. Nothing here stats a filesystem: that is the next phase's job.
    struct LandingSweepCandidate: Equatable {
        let taskID: String
        let projectDir: String
        let target: String
        /// The branch a delivery of this task would be on, whether or not one exists.
        let deliveryBranch: String
        /// The head the registry last recorded for that branch. The live ref wins over it; this is
        /// the only thing left once a merged branch has been deleted.
        let storedHead: String?
        /// The declared write set. Empty for an isolated task by design — the broker drops that
        /// lease — which is why an isolated task is never an arm-2 candidate.
        let claims: [String]
    }

    /// What one candidate can be asked, once the repository's live delivery refs are known.
    enum LandingSweepQuestion: Equatable {
        case ancestry(head: String)
        case writeSet(paths: [String])
        /// Neither question can be put to git for this row, and why.
        case unanswerable(String)
    }

    /// Why a pass left a record exactly as it found it, or which arm closed it.
    enum LandingSweepVerdict: Equatable {
        case closed(LandingSweepArm)
        case left(String)
    }

    struct LandingSweepOutcome: Equatable {
        let taskID: String
        let verdict: LandingSweepVerdict
    }

    /// What the checkout said about one task's declared write set. Two lists rather than one
    /// boolean, because *somebody is still editing it* and *it never matched the target* are
    /// different findings and a reader of the log needs to tell them apart.
    struct LandingSweepWriteSet: Equatable {
        /// Claimed paths git reported as modified, staged or untracked in the project directory.
        let outstanding: [String]
        /// Claimed paths whose content differs from the target branch.
        let differing: [String]

        var isSettled: Bool { outstanding.isEmpty && differing.isEmpty }
    }

    /// How many pending records one pass will look at. A cap rather than a whole registry, because
    /// arm 2 costs two short subprocesses per row and this runs on a timer; what a pass does not
    /// reach, the next one does.
    static let landingSweepLimit = 20

    /// How often a pass may run. Nothing here is urgent — the records it closes have been open for
    /// hours — and the cost it must not impose is a git subprocess storm on a five-second beat.
    static let landingSweepInterval: TimeInterval = 300

    // MARK: - The pure half

    /// Every pending landing this pass may be able to settle, oldest first.
    ///
    /// Pure, and given its tasks rather than reading the registry, so the whole selection table can
    /// be exercised without seeding one. The three refusals that matter are all here: a settled
    /// record is not a candidate at all, a task that has not finished owes nothing yet, and a
    /// record with no target names nothing to be contained by.
    static func landingSweepCandidates(_ tasks: [Task],
                                       limit: Int = landingSweepLimit) -> [LandingSweepCandidate] {
        tasks.filter { task in
            guard task.state.isTerminal, let landing = task.landing,
                  landing.state == .pending,
                  let target = landing.target, !target.isEmpty else { return false }
            // Something has to be askable. A task with neither a checkout nor a declared write set
            // leaves this Mac no evidence of what it wrote, and an absence is not a proof.
            return task.worktree != nil || !task.claims.isEmpty
        }
        .sorted { $0.created == $1.created ? $0.id < $1.id : $0.created < $1.created }
        .prefix(max(0, limit))
        .map { task in
            LandingSweepCandidate(
                taskID: task.id, projectDir: task.projectDir,
                target: task.landing?.target ?? "",
                deliveryBranch: task.worktree?.branch
                    ?? OrchestratorDraft.worktreeBranch(for: task.id) ?? "",
                storedHead: task.worktree?.head,
                claims: task.worktree == nil ? task.claims : [])
        }
    }

    /// A claimed path this sweep is willing to hand to git as a pathspec.
    ///
    /// Dispatch already refuses `..`, and this refuses it again rather than trusting a record
    /// written by an older build; `:` is pathspec magic, and a leading `-` is an option. A single
    /// unusable claim disqualifies the whole task: a write set checked in part is not a write set.
    static func landingSweepClaimsUsable(_ claims: [String]) -> Bool {
        guard !claims.isEmpty else { return false }
        return claims.allSatisfy { path in
            !path.isEmpty && !path.hasPrefix("-") && !path.hasPrefix(":") && path.count <= 500
                && !path.split(separator: "/", omittingEmptySubsequences: false).contains("..")
        }
    }

    /// Which question this candidate answers, given what the repository's refs say right now.
    ///
    /// **A live ref beats the stored head**, the way `Orchestrator.inflightRow` and
    /// `check-landing-records.py` both prefer it: the difference between reporting a delivery and
    /// reporting a memory of one. A delivery branch that exists at all makes this an ancestry
    /// question and not a write-set one — arm 2 is for the class that has no branch to ask about.
    static func landingSweepQuestion(for candidate: LandingSweepCandidate,
                                     liveHead: String?) -> LandingSweepQuestion {
        if let head = liveHead ?? candidate.storedHead, !head.isEmpty {
            return .ancestry(head: head)
        }
        guard landingSweepClaimsUsable(candidate.claims) else {
            return .unanswerable("no delivery branch, and no usable declared write set")
        }
        return .writeSet(paths: candidate.claims)
    }

    /// The record a proved candidate closes as. Fail-closed in both directions: an ancestry arm
    /// without a broker verification, or a write-set arm carrying one, returns nil rather than
    /// writing a record whose shape contradicts what was proved.
    static func landingSweepLanding(existing: Landing, arm: LandingSweepArm,
                                    verification: LandingVerification?,
                                    targetCommit: String, target: String,
                                    claimCount: Int, now: Date) -> Landing? {
        switch arm {
        case .ancestry:
            guard let verification, verification.origin == "local_target_branch",
                  isFullObjectID(verification.commit),
                  isFullObjectID(verification.targetCommit) else { return nil }
            return Landing(
                state: .landed, target: target, delivery: existing.delivery,
                ownerRootKey: existing.ownerRootKey, since: existing.since,
                commit: verification.commit,
                note: "Broker landing sweep: refs/heads/\(target) contains this delivery's head "
                    + "\(verification.commit.prefix(8)), verified in the task's own repository. "
                    + "That proves the delivery reached the target.",
                landedAt: now,
                verificationOrigin: verification.origin,
                verifiedCommit: verification.commit,
                verifiedTargetCommit: verification.targetCommit)
        case .writeSet:
            guard verification == nil, isFullObjectID(targetCommit) else { return nil }
            // No `landedAt` and no verification triple, deliberately. This arm never verified that
            // a commit of this task's reached the target — there is no commit of its own to check
            // — so a record that looked broker-verified would be the weaker sentence wearing the
            // stronger one's clothes, and `isBrokerVerifiedTargetLanding` would turn it into the
            // double check.
            return Landing(
                state: .landed, target: target, delivery: existing.delivery,
                ownerRootKey: existing.ownerRootKey, since: existing.since,
                commit: targetCommit,
                note: "Broker landing sweep: all \(claimCount) declared path(s) are unmodified in "
                    + "this task's project directory and identical to refs/heads/\(target) at "
                    + "\(targetCommit.prefix(8)). That proves nothing of this task's write set is "
                    + "outstanding. It is not proof that a delivery reached the target.",
                landedAt: nil)
        }
    }

    /// A full lowercase object id, the only shape a commit written here may take. The same test
    /// `isBrokerVerifiedTargetLanding` and `OrchestratorStore.landing(from:)` already apply.
    static func isFullObjectID(_ id: String) -> Bool {
        (id.count == 40 || id.count == 64)
            && id.allSatisfy { ("0"..."9").contains($0) || ("a"..."f").contains($0) }
    }

    // MARK: - The git half

    /// Every commit `refs/heads/<target>` contains, and that branch's own tip.
    ///
    /// One subprocess for a whole repository rather than a `merge-base --is-ancestor` per task —
    /// the shape `check-landing-records.py` measured at 1,196 commits in 9 ms. `nil` means git did
    /// not answer, and the caller then skips the repository rather than reading an empty set as
    /// "nothing has landed", which would make every delivery look outstanding.
    static func landingSweepTargetHistory(gitDirectory: String, target: String)
        -> (tip: String, contained: Set<String>)? {
        // `refs/heads/a..b` is a revision *range*, so an unchecked target would silently change
        // the question. This is the same gate `verifyTargetLanding` opens with.
        guard let format = OrchestratorDraft.git(["check-ref-format", "--branch", target],
                                                 cwd: "/", gitDirectory: gitDirectory),
              format.status == 0,
              let answer = OrchestratorDraft.git(["rev-list", "refs/heads/\(target)"],
                                                 cwd: "/", gitDirectory: gitDirectory),
              answer.status == 0 else { return nil }
        var contained: Set<String> = []
        var tip = ""
        for line in answer.output.split(separator: "\n") {
            let id = line.trimmingCharacters(in: .whitespaces)
            guard !id.isEmpty else { continue }
            if tip.isEmpty { tip = id }
            contained.insert(id)
        }
        guard isFullObjectID(tip) else { return nil }
        return (tip, contained)
    }

    /// Delivery branch name → the commit it points at now. One subprocess per repository. An
    /// unreadable answer is an empty map: every candidate then falls back to its stored head,
    /// which is the same fallback a deleted merged branch takes.
    static func landingSweepDeliveryHeads(gitDirectory: String) -> [String: String] {
        guard let answer = OrchestratorDraft.git(
                ["for-each-ref", "--format=%(refname:short) %(objectname)",
                 "refs/heads/clawdline/task/"], cwd: "/", gitDirectory: gitDirectory),
              answer.status == 0 else { return [:] }
        var heads: [String: String] = [:]
        for line in answer.output.split(separator: "\n") {
            let parts = line.split(separator: " ")
            guard parts.count == 2 else { continue }
            heads[String(parts[0])] = String(parts[1])
        }
        return heads
    }

    /// Whether every claimed path is unmodified in the checkout and identical to the target.
    ///
    /// Two questions and two subprocesses, both read-only — `GIT_OPTIONAL_LOCKS=0` is set on the
    /// one git seam this goes through, so a pass cannot take the index lock out from under
    /// somebody working in that tree. `nil` means git would not answer, which is never read as a
    /// settled write set.
    static func landingSweepWriteSet(projectDir: String, targetCommit: String,
                                     claims: [String]) -> LandingSweepWriteSet? {
        guard landingSweepClaimsUsable(claims), isFullObjectID(targetCommit) else { return nil }
        var isDirectory: ObjCBool = false
        guard FileManager.default.fileExists(atPath: projectDir, isDirectory: &isDirectory),
              isDirectory.boolValue else { return nil }
        guard let status = OrchestratorDraft.git(
                ["status", "--porcelain=v1", "--untracked-files=all", "--"] + claims,
                cwd: projectDir), status.status == 0,
              let diff = OrchestratorDraft.git(
                ["diff", "--name-only", "--no-ext-diff", targetCommit, "--"] + claims,
                cwd: projectDir), diff.status == 0 else { return nil }
        let outstanding = status.output.split(separator: "\n")
            .map { String($0.dropFirst(3)) }
            .filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
        let differing = diff.output.split(separator: "\n").map(String.init)
            .filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
        return LandingSweepWriteSet(outstanding: outstanding, differing: differing)
    }

    // MARK: - One pass

    private static var landingSweepInFlight = false
    private static var landingSweepLastPass: Date?

    /// Main thread, from ``beat(fromTimer:)``, and nowhere else.
    ///
    /// Everything expensive is behind two gates: a pass runs at most once every
    /// ``landingSweepInterval``, and never while one is still running. The work itself goes to
    /// ``worktreeQueue`` — the one utility queue this app already serializes bounded git
    /// subprocesses on — so nothing here ever asks git a question from the main queue, and nothing
    /// asks one while holding the registry lock. A `sync` back onto a queue this pass is already
    /// on is the shape that produced an exit-133 deadlock here; there is none.
    static func scheduleLandingSweep(now: Date = Date()) {
        guard !landingSweepInFlight else { return }
        if let last = landingSweepLastPass, now.timeIntervalSince(last) < landingSweepInterval {
            return
        }
        landingSweepLastPass = now
        landingSweepInFlight = true
        worktreeQueue.async {
            _ = landingSweepPass(now: Date())
            DispatchQueue.main.async { landingSweepInFlight = false }
        }
    }

    /// One pass, synchronous, off the main queue. Returns what it decided about every candidate it
    /// looked at, oldest first, which is what a test reads and what the audit log records.
    @discardableResult
    static func landingSweepPass(now: Date = Date(),
                                 limit: Int = landingSweepLimit) -> [LandingSweepOutcome] {
        load()
        lock.lock()
        let evidence = Array(tasks.values)
        lock.unlock()
        let candidates = landingSweepCandidates(evidence, limit: limit)
        guard !candidates.isEmpty else { return [] }
        let byID = Dictionary(evidence.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })

        // Repository identity is resolved once per checkout rather than once per task: for a
        // shared-checkout backlog — which is the whole class arm 2 exists for — that is one
        // resolution for twelve rows. It stays per task only where the answer genuinely depends on
        // the task, which is the worktree case `landingGitDirectory` validates against the task id.
        var identities: [String: String?] = [:]
        var histories: [String: (tip: String, contained: Set<String>)?] = [:]
        var deliveryHeads: [String: [String: String]] = [:]
        var out: [LandingSweepOutcome] = []

        for candidate in candidates {
            guard let task = byID[candidate.taskID], let existing = task.landing else { continue }
            let identityKey = task.isolation == .worktree
                ? "worktree:\(task.id)"
                : "shared:\(task.projectDir)|\(task.repositoryCommonDir ?? "")"
            let gitDirectory: String?
            if let cached = identities[identityKey] {
                gitDirectory = cached
            } else {
                gitDirectory = OrchestratorDraft.landingGitDirectory(for: task, among: evidence)
                identities[identityKey] = gitDirectory
            }
            guard let gitDirectory else {
                out.append(LandingSweepOutcome(
                    taskID: candidate.taskID,
                    verdict: .left("this Mac cannot resolve the task's repository")))
                continue
            }
            let historyKey = "\(gitDirectory)|\(candidate.target)"
            let history: (tip: String, contained: Set<String>)?
            if let cached = histories[historyKey] {
                history = cached
            } else {
                history = landingSweepTargetHistory(gitDirectory: gitDirectory,
                                                    target: candidate.target)
                histories[historyKey] = history
            }
            guard let history else {
                out.append(LandingSweepOutcome(
                    taskID: candidate.taskID,
                    verdict: .left("git would not answer for refs/heads/\(candidate.target) in "
                                   + "this repository, which is not the same as nothing having "
                                   + "landed")))
                continue
            }
            if deliveryHeads[gitDirectory] == nil {
                deliveryHeads[gitDirectory] = landingSweepDeliveryHeads(gitDirectory: gitDirectory)
            }
            let liveHead = deliveryHeads[gitDirectory]?[candidate.deliveryBranch]

            switch landingSweepQuestion(for: candidate, liveHead: liveHead) {
            case .unanswerable(let why):
                out.append(LandingSweepOutcome(taskID: candidate.taskID, verdict: .left(why)))
            case .ancestry(let head):
                guard history.contained.contains(head) else {
                    out.append(LandingSweepOutcome(
                        taskID: candidate.taskID,
                        verdict: .left("refs/heads/\(candidate.target) does not contain "
                                       + "\(head.prefix(8))")))
                    continue
                }
                guard let verification = OrchestratorDraft.verifyTargetLanding(
                        gitDirectory: gitDirectory, target: candidate.target, commit: head),
                      let landing = landingSweepLanding(
                        existing: existing, arm: .ancestry, verification: verification,
                        targetCommit: history.tip, target: candidate.target,
                        claimCount: candidate.claims.count, now: now) else {
                    out.append(LandingSweepOutcome(
                        taskID: candidate.taskID,
                        verdict: .left("the containment held and the verification did not, so "
                                       + "nothing was written")))
                    continue
                }
                out.append(LandingSweepOutcome(
                    taskID: candidate.taskID,
                    verdict: applyLandingSweep(task: task, expected: existing, landing: landing,
                                               arm: .ancestry)))
            case .writeSet(let paths):
                guard let answer = landingSweepWriteSet(projectDir: candidate.projectDir,
                                                        targetCommit: history.tip, claims: paths)
                else {
                    out.append(LandingSweepOutcome(
                        taskID: candidate.taskID,
                        verdict: .left("git would not answer for this task's declared write set "
                                       + "in \(candidate.projectDir)")))
                    continue
                }
                guard answer.isSettled else {
                    let why = answer.outstanding.isEmpty
                        ? "\(answer.differing.count) claimed path(s) differ from "
                            + "refs/heads/\(candidate.target)"
                        : "\(answer.outstanding.count) claimed path(s) are still uncommitted in "
                            + "the checkout"
                    out.append(LandingSweepOutcome(taskID: candidate.taskID, verdict: .left(why)))
                    continue
                }
                guard let landing = landingSweepLanding(
                        existing: existing, arm: .writeSet, verification: nil,
                        targetCommit: history.tip, target: candidate.target,
                        claimCount: paths.count, now: now) else {
                    out.append(LandingSweepOutcome(
                        taskID: candidate.taskID,
                        verdict: .left("the write set was settled and its record would not form")))
                    continue
                }
                out.append(LandingSweepOutcome(
                    taskID: candidate.taskID,
                    verdict: applyLandingSweep(task: task, expected: existing, landing: landing,
                                               arm: .writeSet)))
            }
        }
        return out
    }

    /// The write, under the lock, with the compare-and-swap the git subprocesses above make
    /// necessary: the record and the task state must both be exactly what this pass decided about,
    /// or the decision belonged to a world that has moved and nothing is written. A record that
    /// settled while git was running — the route, another pass, a person — is left alone, because
    /// a settled state may never move to another one.
    private static func applyLandingSweep(task: Task, expected: Landing, landing: Landing,
                                          arm: LandingSweepArm) -> LandingSweepVerdict {
        lock.lock()
        guard var current = tasks[task.id], current.state == task.state,
              current.landing == expected, expected.state == .pending else {
            lock.unlock()
            return .left("the record changed while git was being asked, so it was left alone")
        }
        current.landing = landing
        tasks[task.id] = current
        let settled = current
        lock.unlock()

        _ = save()
        RemoteServer.shared.broadcastOrchestrator()
        RemoteAuth.audit("orchestrator.landing.sweep", [
            "task": task.id, "arm": arm.rawValue, "target": landing.target ?? "",
            "commit": landing.commit ?? "",
            "verified": Orchestrator.isBrokerVerifiedTargetLanding(landing) ? "1" : "0",
        ])
        recordLandingInLedger(settled)
        return .closed(arm)
    }
}
