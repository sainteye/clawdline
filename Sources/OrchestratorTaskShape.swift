import Foundation

// The shape of a task: its states, its landing obligations, its worktree and the records a
// root keeps about it. Eighteen nested types and no stored state of its own, which is why it
// moves as an extension and every `Orchestrator.Task` spelling stays exactly as it was.
extension Orchestrator {

    enum State: String {
        case queued, spawning, briefed
        case success, failure, timeout, cancelled
        case spawnFailed = "spawn_failed"

        var isTerminal: Bool {
            switch self {
            case .queued, .spawning, .briefed: return false
            case .success, .failure, .timeout, .cancelled, .spawnFailed: return true
            }
        }
    }

    enum Isolation: String {
        case none, worktree
    }

    /// **The obligation's four spellings, three of which are final.**
    ///
    /// `nothingToLand` is the one a read-only delivery needs, and it was added because five
    /// children on this Mac had no honest state at all: they audited, wrote nothing to any
    /// repository, and shipped an artifact under somebody else's commit. `landed` wants a target
    /// and a commit they do not have; `abandoned` says the work was given up, which is a false
    /// sentence about an audit that was read and acted on. So they sat in the Projects page's
    /// "done, never landed" block for ever, and a block that cannot be emptied is one nobody
    /// reads.
    ///
    /// It is as final as the other two — see the settled check in
    /// ``Orchestrator/updateLanding(taskID:secret:orchestratorToken:raw:now:)`` — and it is
    /// admissible only where this Mac holds no durable evidence that the task wrote anything;
    /// ``Orchestrator/nothingToLandAdmission(for:)`` is the one place that rule is written down.
    enum LandingState: String, CaseIterable {
        case pending, landed, abandoned
        case nothingToLand = "nothing_to_land"

        /// Whether this obligation is closed. A settled state may never move to another one.
        var isSettled: Bool { self != .pending }
    }

    /// Whether a task may be closed as having had nothing to land, and if not, the stored fact
    /// that says otherwise.
    ///
    /// **This is a refusal built out of evidence, not a proof of innocence.** A task that ran in
    /// the shared checkout leaves no record here of what it wrote — `git status` in that tree is
    /// the only witness, and it belongs to nobody in particular — so what this can do is refuse
    /// every case the registry *can* see: a declared claim, commits on the delivery branch, a
    /// dirty checkout, counts it does not have, and an obligation whose target a root has already
    /// named. The remaining assertion is the caller's, which is why the route accepts it only
    /// from the machine credential.
    ///
    /// **`declaredWritePaths` is passed in and not read off the task**, because for an isolated
    /// task `claims` is empty by design — the broker drops the lease and hands the list back as
    /// `claims_ignored_for_worktree`, which
    /// ``OrchestratorLandingQueue/landingPaths(of:retainedPaths:)`` is the one place that knows
    /// how to recover. Reading `claims` here would have made every worktree child look like it
    /// had declared nothing, which is the one spelling that positively means "writes nothing".
    /// It is also a store behind its own lock, and this runs inside the registry's.
    static func nothingToLandAdmission(for task: Task,
                                       declaredWritePaths: [String]) -> NothingToLandAdmission {
        if !declaredWritePaths.isEmpty {
            return .refused("this task declared \(declaredWritePaths.count) path(s) to write")
        }
        if let target = task.landing?.target, !target.isEmpty {
            return .refused("its landing obligation already names the target \(target)")
        }
        if let worktree = task.worktree {
            guard let commits = worktree.commits, let dirty = worktree.dirty else {
                return .refused("this Mac has no commit count for its checkout, and an unknown "
                                + "count is not permission")
            }
            if commits > 0 { return .refused("its branch carries \(commits) commit(s)") }
            if dirty { return .refused("its checkout has uncommitted changes") }
        }
        return .admitted
    }

    enum NothingToLandAdmission: Equatable {
        case admitted
        /// The stored fact that contradicts "there was nothing to land".
        case refused(String)

        var isAdmitted: Bool { self == .admitted }
    }

    /// The root-owned obligation after a child has delivered. This is deliberately observational:
    /// it never extends a claim or participates in dispatch arbitration.
    struct Landing: Equatable {
        let state: LandingState
        let target: String?
        let delivery: String?
        let ownerRootKey: String
        let since: Date
        let commit: String?
        let note: String?
        /// When the target commit was verified. `since` remains when the obligation first opened.
        let landedAt: Date?

        /// Durable broker evidence captured only after resolving both objects in the task's
        /// project repository and proving the commit is contained by the named local target.
        /// Their absence on legacy rows is intentional fail-closed compatibility.
        let verificationOrigin: String?
        let verifiedCommit: String?
        let verifiedTargetCommit: String?

        init(state: LandingState, target: String?, delivery: String?, ownerRootKey: String,
             since: Date, commit: String?, note: String?, landedAt: Date? = nil,
             verificationOrigin: String? = nil, verifiedCommit: String? = nil,
             verifiedTargetCommit: String? = nil) {
            self.state = state
            self.target = target
            self.delivery = delivery
            self.ownerRootKey = ownerRootKey
            self.since = since
            self.commit = commit
            self.note = note
            self.landedAt = landedAt
            self.verificationOrigin = verificationOrigin
            self.verifiedCommit = verifiedCommit
            self.verifiedTargetCommit = verifiedTargetCommit
        }
    }

    struct LandingVerification: Equatable {
        let origin: String
        let commit: String
        let targetCommit: String
    }

    /// What a resent `landed` record says about the settled one it is aimed at.
    ///
    /// The distinction exists because one of these three was being answered as though it were
    /// another. A resend carrying a *different* commit was swallowed by the idempotent early
    /// return in ``updateLanding(taskID:secret:orchestratorToken:raw:now:)`` and the caller was
    /// told `ok: true`; one record on this machine names another task's commit permanently
    /// because the root that wrote it tried to correct it and was told it had worked.
    enum LandingResend: Equatable {
        /// Nothing supplied contradicts the record. Replaying it applies no write, so answering
        /// `ok` is honest — and this is the door a landing recorded before the ledger wiring
        /// existed uses to reach its row.
        case replay
        /// The same claim with different evidence or annotation. A write: it is applied, through
        /// the same verification the first landing passed, or it is refused. It is never `ok`
        /// without one of those.
        case correction(field: String, stored: String, requested: String)
        /// A *different* claim about a settled obligation — a landing aimed at another target is
        /// not a correction of this one. Refused, naming both values.
        case conflict(field: String, stored: String, requested: String)
    }

    /// Read a `landed` resend against the record it is aimed at, without asking git anything.
    ///
    /// **Absent is not a disagreement.** A caller replaying a receipt onto its ledger row sends
    /// the state and little else, and a field it did not mention is not a write it is asking for.
    ///
    /// **Two spellings of one commit are one claim.** The stored value is the 40-character object
    /// the broker resolved; a caller holding the abbreviation git printed for it is naming the
    /// same commit, so a hex prefix of at least git's own seven characters counts as agreement.
    /// Anything that is not hex is not compared here at all — `HEAD`, a branch name, or the free
    /// text a legacy row may hold — because guessing whether two revision expressions name one
    /// object is exactly the reading with no subject this whole change is about. Those fall to
    /// `correction`, where git resolves them and the answer is proved rather than assumed.
    static func landingResend(existing: Landing,
                              requested fields: [String: String]) -> LandingResend {
        if let target = fields["target"], let stored = existing.target, target != stored {
            return .conflict(field: "target", stored: stored, requested: target)
        }
        if let commit = fields["commit"], !namesSameCommit(existing.commit, commit) {
            return .correction(field: "commit", stored: existing.commit ?? "", requested: commit)
        }
        if let target = fields["target"], existing.target == nil {
            return .correction(field: "target", stored: "", requested: target)
        }
        for (name, stored) in [("delivery", existing.delivery), ("note", existing.note)] {
            guard let value = fields[name], value != stored else { continue }
            return .correction(field: name, stored: stored ?? "", requested: value)
        }
        return .replay
    }

    /// Whether two pieces of commit text are certainly one commit. Certainly, not probably: a
    /// `false` here costs a git resolution, and a `true` skips one.
    static func namesSameCommit(_ stored: String?, _ requested: String) -> Bool {
        guard let stored, !stored.isEmpty, !requested.isEmpty else { return false }
        if stored == requested { return true }
        let a = stored.lowercased(), b = requested.lowercased()
        let hex = { (text: String) in
            text.allSatisfy { ("0"..."9").contains($0) || ("a"..."f").contains($0) }
        }
        guard hex(a), hex(b), min(a.count, b.count) >= 7 else { return false }
        return a.hasPrefix(b) || b.hasPrefix(a)
    }

    static func isBrokerVerifiedTargetLanding(_ landing: Landing) -> Bool {
        guard landing.state == .landed, landing.landedAt != nil,
              landing.verificationOrigin == "local_target_branch",
              let commit = landing.commit, !commit.isEmpty,
              landing.verifiedCommit == commit,
              let targetCommit = landing.verifiedTargetCommit, !targetCommit.isEmpty,
              landing.target?.isEmpty == false else { return false }
        return [commit, targetCommit].allSatisfy { id in
            (id.count == 40 || id.count == 64)
                && id.allSatisfy { ("0"..."9").contains($0) || ("a"..."f").contains($0) }
        }
    }

    /// The checkout is disposable; the branch is the delivery. Repository and cwd are internal
    /// facts needed to operate a monorepo worktree and are stored beside the six public facts.
    struct Worktree {
        var path: String
        var branch: String
        var base: String
        var repository: String
        /// Canonical common Git directory shared by every checkout of this repository. Unlike
        /// `repository`, which may itself be a linked worktree used at dispatch time, this
        /// identity remains usable after that checkout is disposed.
        var repositoryCommonDir: String? = nil
        var cwd: String
        var head: String? = nil
        var commits: Int? = nil
        var dirty: Bool? = nil
        var baseDirty = 0
        var requestedBase = "HEAD"
    }

    enum WorktreeDisposal: Equatable {
        case removeAll, removeTreeKeepBranch, keepEverything
    }

    /// The deletion policy is pure and fail-safe. Missing git facts are not permission to erase.
    static func worktreeDisposal(commits: Int?, dirty: Bool?, headOnBranch: Bool?,
                                 branchExists: Bool) -> WorktreeDisposal {
        guard branchExists, let commits, let dirty, !dirty else { return .keepEverything }
        if commits == 0 {
            return headOnBranch == true ? .removeAll : .keepEverything
        }
        return .removeTreeKeepBranch
    }

    /// Whether an ending may reclaim this task's checkout as one nothing was ever done in.
    ///
    /// ``worktreeDisposal(commits:dirty:headOnBranch:branchExists:)`` already refuses to erase
    /// commits or dirty bytes. This is the guard for the window it cannot see: a child working in
    /// the directory right now that has not written a byte yet. Measured on 2026-08-28 — one
    /// child had committed a minute in and kept everything; its sibling had not, and lost the
    /// checkout *and* the delivery branch while its tab was still `working` inside the deleted
    /// directory.
    ///
    /// **Liveness cannot answer this question**, which is why neither the tab nor the child
    /// process appears below. A session that never reached a prompt is also a live assistant in a
    /// live tab — that *is* the case this reclaim exists for. What separates the two readings is
    /// whether this task's own first message was ever put in front of that session.
    ///
    /// A checkout kept by mistake costs a directory until `cleanup` sweeps it a day later. A
    /// checkout deleted by mistake costs the work inside it, and this app has no copy.
    static func reclaimsEmptyWorktree(_ task: Task, outcome: State) -> Bool {
        guard !childWasSpokenTo(task) else { return false }
        if task.childTerminalId == nil { return true }
        return outcome == .spawnFailed && task.briefedAt == nil
    }

    /// Any receipt that this task's first message and a child ever met, in either direction:
    /// the briefing was accepted, its marker was proved in a transcript, the child answered with
    /// a note, or the line was typed at a composer this app had already seen was ready.
    static func childWasSpokenTo(_ task: Task) -> Bool {
        task.briefedAt != nil
            || task.transcriptProven
            || !task.progress.isEmpty
            || task.progressFileNote != nil
            || task.injectAttempts > 0
    }

    /// A stale value copy may add fields, but it may never move a task backwards or resurrect it.
    /// Internal rather than private so the invariant has a pure unit test.
    static func mayReplaceState(_ current: State, with candidate: State) -> Bool {
        if current == candidate { return true }
        if current.isTerminal { return false }
        if candidate.isTerminal { return true }
        switch (current, candidate) {
        case (.queued, .spawning), (.queued, .briefed), (.spawning, .briefed):
            return true
        default:
            return false
        }
    }

    struct Usage {
        var input = 0
        var output = 0
        var cacheRead = 0
        var cacheWrite = 0
        var total = 0
        var model: String?
        var costUsd: Double?
    }

    struct Verification: Equatable {
        enum Last: String {
            case pass, fail, skipped
        }

        let runs: Int
        let seconds: Int
        let last: Last
        let scope: String
    }

    /// One claim path given back early through `claims/release`, and when — see
    /// `Orchestrator.releaseClaims`. `Task.claimKeys` stays the full original reservation;
    /// `Task.activeClaimKeys` is what this subtracts from it.
    struct ReleasedClaim: Equatable {
        let path: String
        let releasedAt: Date
    }

    /// Durable state for the terminal completion postman. A terminal task outcome and this
    /// envelope are written in the same orchestrator-store snapshot before any terminal API is
    /// called. `delivered` means only that the terminal transport accepted the line; observation
    /// and acknowledgement remain separate facts until the root explicitly ACKs `noticeID`.
    enum CompletionDeliveryState: String, Equatable {
        case pending, delivered, acknowledged
        case deadLetter = "dead_letter"
    }

    enum CompletionFailureCode: String, Equatable {
        case rootMissing = "root_missing"
        case conversationAmbiguous = "conversation_ambiguous"
        case rootChoosing = "root_choosing"
        case itermModal = "iterm_modal"
        case terminalTimeout = "terminal_timeout"
        case identityStale = "identity_stale"
        case transportFailed = "transport_failed"
        case acknowledgementTimeout = "acknowledgement_timeout"
    }

    struct CompletionFailure: Equatable {
        let code: CompletionFailureCode
        let message: String
        let at: Date
    }

    struct CompletionDelivery: Equatable {
        let noticeID: String
        let created: Date
        var state: CompletionDeliveryState
        var attempts: Int
        var nextRetryAt: Date?
        var lastAttemptAt: Date? = nil
        var transportDeliveredAt: Date? = nil
        var observedAt: Date? = nil
        var acknowledgedAt: Date? = nil
        var lastError: CompletionFailure? = nil
        var deadLetterAt: Date? = nil
        var legacyReconciled = false
        /// Process-local eligibility barrier. New envelopes stay false until the exact registry
        /// snapshot containing them reaches disk; decoded envelopes default true.
        var persisted: Bool

        init(noticeID: String, created: Date, state: CompletionDeliveryState,
             attempts: Int, nextRetryAt: Date?, lastAttemptAt: Date? = nil,
             transportDeliveredAt: Date? = nil, observedAt: Date? = nil,
             acknowledgedAt: Date? = nil, lastError: CompletionFailure? = nil,
             deadLetterAt: Date? = nil, legacyReconciled: Bool = false,
             persisted: Bool = true) {
            self.noticeID = noticeID
            self.created = created
            self.state = state
            self.attempts = attempts
            self.nextRetryAt = nextRetryAt
            self.lastAttemptAt = lastAttemptAt
            self.transportDeliveredAt = transportDeliveredAt
            self.observedAt = observedAt
            self.acknowledgedAt = acknowledgedAt
            self.lastError = lastError
            self.deadLetterAt = deadLetterAt
            self.legacyReconciled = legacyReconciled
            self.persisted = persisted
        }
    }

    enum CompletionTransportResult: Equatable {
        case delivered
        case failed(CompletionFailureCode, String)
    }

    struct Task {
        let id: String
        var state: State
        var kind: String
        var title: String
        var assistant: Assistant
        /// The model the child was started on, when the task named one. Nil means whatever that
        /// assistant defaults to, which is the answer for most tasks and all older records.
        var model: String?
        var reasoningEffort: ReasoningEffort?
        /// How far the child may go before stopping to ask — what was actually used, after this
        /// Mac's ceiling was applied to what the task asked for.
        var permission = Permission.ask
        var projectDir: String
        /// Canonical common Git directory captured at dispatch for every task whose project is
        /// inside a repository. This is the durable landing identity; projectDir may name a
        /// disposable checkout even when isolation itself was not declared.
        var repositoryCommonDir: String? = nil
        var timeoutMinutes: Int
        var created: Date
        var spawnedAt: Date?
        var briefedAt: Date?
        var finishedAt: Date?
        /// Set only after `result.json` passed the task-secret check. A terminal state reported by
        /// `/complete` may exist without it, which is why this is not derived from `finishedAt`.
        var resultVerifiedAt: Date?
        var rootSessionId: String?
        /// Which assistant owns `rootSessionId`. Older registry rows predate the field and are
        /// Claude roots, so nil retains that spelling rather than making them unresolvable.
        var rootAssistant: Assistant?
        var rootLabel: String?
        /// How far from the person at the keyboard this task is: `1` for one a human's session
        /// dispatched, `2` for one dispatched by a child of theirs. Stored rather than derived —
        /// the parent may be over and gone by the time anybody asks, and the answer should not
        /// change when it goes.
        var depth = 1
        /// The task whose child dispatched this one, when it said so. Nil at depth 1, and nil at
        /// depth 2 when the parent was recognised by session id instead.
        var parentTaskId: String?
        /// The `spawn_failed` task this one was retried from, when it was. Nil for every
        /// ordinary dispatch. Recorded so the chain is visible in the registry rather than being
        /// three unrelated-looking tasks with the same title.
        var respawnOf: String?
        /// How far down a respawn chain this task sits: `0` for an original, `1` for the retry of
        /// one, `2` for the retry of that. It is a description of where this task came from and
        /// nothing more: ``Orchestrator/respawnLimit`` is enforced over the whole family
        /// descending from one original, by ``Orchestrator/respawnFamily(of:)``, because a depth
        /// held on the retried task is a number no respawn ever updates.
        var respawnGeneration = 0
        /// The whole graph this task is one node of, in the dispatcher's own words. Carried into
        /// the briefing so a leaf knows what its output feeds — which is the difference between
        /// a usable answer and an essay.
        var plan: String?
        var graph: PlanningGraph?
        /// Durable work identity is independent of this execution attempt's kind.
        var workItemID: String? = nil
        var workPhase: String? = nil
        /// Present only for work created from a schedule file. The public registry exposes the
        /// id; the two policy values stay internal so an edit to the source file cannot rewrite
        /// what should happen to a task already in flight.
        var scheduleID: String?
        var scheduleCloseTab = ScheduleCloseTab.onSuccess
        var scheduleNotifyFailure = true
        /// This task opened an independently owned Root Session rather than a child tab. The task
        /// receipt still bounds its first piece of work, but the Session owns its tab and may
        /// dispatch children of its own.
        var sessionRoot = false
        /// Machine-global operation names acquired together when this task leaves `queued`.
        /// A queued task holds none; a spawning or briefed task holds every name in this list.
        var serialize: [String] = []
        /// Paths this task may write, relative to `projectDir`. Unlike `serialize`, these are
        /// reserved from registration (including while queued) until every terminal outcome.
        var claims: [String] = []
        /// Whether `claims` was present in task.json. An empty declared set means read-only;
        /// absence means the write set is unknown, so L1 must retain its directory warning.
        var claimsDeclared = false
        /// Absolute, canonical-root comparison keys frozen when the lease is registered. These
        /// are persisted because a claim must not change identity as its target comes into being.
        var claimKeys: [String] = []
        /// Claim keys given back early through `claims/release`, each with when it happened.
        var releasedClaims: [ReleasedClaim] = []
        /// Claim keys still actually held: `claimKeys` minus anything already given back. This
        /// is what dispatch-time arbitration and L1's disjoint-claims silence compare against;
        /// `claimKeys` itself remains the full original reservation for the record and the audit.
        var activeClaimKeys: [String] {
            guard !releasedClaims.isEmpty else { return claimKeys }
            let released = Set(releasedClaims.map(\.path))
            return claimKeys.filter { !released.contains($0) }
        }
        /// Claimed paths the terminal-state audit found untouched — see
        /// `Orchestrator.untouchedClaims`. Purely observational, computed once at finalize and
        /// persisted so a later read shows the same verdict rather than re-checking a
        /// filesystem that has since moved on.
        var untouchedClaims: [String] = []
        /// A root's durable declaration that delivered work is waiting to land, has landed, or
        /// was abandoned. This is a signpost for other roots, never a write-path lease.
        var landing: Landing?
        /// What this session has said it is doing since it was briefed, oldest first, newest
        /// ``Orchestrator/progressKept`` kept. The title is fixed at dispatch; this is not.
        var progress: [ProgressNote] = []
        /// The last note collected from the task directory's `progress.json` — the file half
        /// of the progress channel, for a child whose sandbox cannot reach loopback. Persisted
        /// so a beat cannot replay the file's old sentence after a newer note arrived over
        /// HTTP, and a restart cannot replay it either.
        var progressFileNote: String?
        var isolation = Isolation.none
        var worktree: Worktree?
        /// The existing standing Session this task was delivered into. Nil normally means
        /// Clawdline opened `childTerminalId` for this task and owns that tab's lifecycle;
        /// `sessionRoot` is the explicit independently-owned exception.
        var attachSessionId: String?
        var childTerminalId: String?
        var childBackend: Backend?
        /// Whether this task's terminal was actually launched with access to the whole task
        /// root. Persisted because depth settings can change while the tab remains standing.
        var childTaskRootAccess = false
        var childTTY: String?
        var childPID: Int32?
        var childProcStart: Date?
        var childSessionId: String?
        var transcriptPath: String?
        /// The task marker was observed in `transcriptPath`. Persisted because that fact does not
        /// expire when Claude rotates the file or the file is temporarily unavailable.
        var transcriptProven = false; var executorReceipt: ExecutorReceipt?
        var secretHash: String
        /// Agent-authored pushes already accepted for this task. Unlike the process-wide hourly
        /// brake, this survives a restart: a task gets five messages for its whole lifetime, not
        /// five every time the app is rebuilt.
        var notifyCount = 0
        /// The task secret encrypted with a key local to this installation. It exists only while
        /// a serialized task waits, so a restart can resume the queue without storing plaintext.
        var queuedSecret: String?
        var summary: String?
        var artifacts: [String] = []
        var usage: Usage?
        var verification: Verification?
        var review: ReviewReceipt?
        var injectAttempts = 0
        /// The most recent time the first message was handed to the terminal. In memory only:
        /// a process restart loses the plaintext secret and fails every spawning task anyway.
        var lastInjectAt: Date?
        /// The registry answer already sampled by the temporary legacy comparison. In memory
        /// only, so a restart may compare once more without imposing per-beat transcript I/O.
        var registryControlSessionID: String?
        /// Whether the one menu decision this task is allowed to make has already been made —
        /// either the default was taken on a tab this app opened, or the menu was recognised as
        /// somebody else's and left alone. See ``Orchestrator/menuStep(task:choosing:)``.
        var answeredMenu = false
        /// When the child's terminal was last seen in a reading — the difference between a child
        /// that finished and one whose tab was closed under it.
        var lastSeenChild: Date?
        /// When the child's terminal is due to be closed, once it has reported.
        ///
        /// **Written down, because three minutes is longer than this app stays running.**
        /// It used to live in memory only, on the principle that a tab should be closed on the
        /// strength of what this process saw rather than what a previous one believed — and the
        /// principle is right, but the deadline was never the belief. `./build.sh` replaces the
        /// app several times an hour; every child that reported inside the last three minutes
        /// lost its deadline with the process, and nothing ever set one again. Of the eighteen
        /// tabs left standing since the linger was written, seventeen had the app restart inside
        /// their three minutes and the eighteenth had not run out yet — Claude Code's tabs and
        /// Codex's alike, which is why this was not the assistant-specific fault it looked like.
        ///
        /// What crosses the restart is the deadline and nothing else. Whether that tab is still
        /// the child's is asked again, of a reading this process took — see ``closeStep``.
        var closeAt: Date?
        /// The idempotent completion envelope. Nil on live tasks, manual-poll tasks and legacy
        /// terminal rows which have not yet passed bounded reconciliation.
        var completionDelivery: CompletionDelivery?
        /// Why an automatic terminal cleanup is deliberately still pending. The kind is durable:
        /// an iTerm modal may be tried once after a fresh list proves recovery, while a process
        /// scan/still-running/tmux failure must stay stopped for a person instead of sending the
        /// quit word and signals again on every five-second beat.
        var terminalIntervention: TerminalIntervention?
        /// When the task-owned heavyweight `work/` directory may be reclaimed. Nil means either
        /// there is nothing left to do or the `-1` setting leaves it to the 24-hour root sweep.
        var workCleanupAt: Date?
        /// When the isolated checkout's build output may be reclaimed. Nil for every task without
        /// a worktree of its own: the build directory of a shared checkout belongs to the person
        /// working in it, and this deadline must never be able to name it.
        ///
        /// Separate from ``workCleanupAt`` because the two directories are on opposite sides of
        /// the repository line — `work/` is Clawdline's own scratch under `/tmp`, this is a
        /// gitignored directory inside somebody's checkout — and because whole-worktree disposal
        /// is not allowed to be the only thing that frees it. `.build/` regenerates from the
        /// source; the source and the branch are the delivery and are never touched here.
        var buildCleanupAt: Date?

        var dir: URL { Orchestrator.root.appendingPathComponent(id, isDirectory: true) }
    }

    enum TerminalInterventionKind: String {
        case iTermModal = "iterm_modal"
        case terminal = "terminal"
    }

    struct TerminalIntervention: Equatable {
        let kind: TerminalInterventionKind
        let message: String
    }

    static func scheduledCloseAt(policy: ScheduleCloseTab, outcome: State,
                                 now: Date, hasChild: Bool, linger: TimeInterval = 180,
                                 briefed: Bool = true) -> Date? {
        guard hasChild else { return nil }
        switch policy {
        case .always: return now
        case .onSuccess: return outcome == .success ? now : nil
        case .never: return nil
        }
    }

    /// An explicitly retained task owns an independent Root Session from its first turn. A task
    /// with a conditional close policy starts as a child and may be promoted after finalization.
    static func opensRootSession(scheduleCloseTab: ScheduleCloseTab?, childLinger: Int) -> Bool {
        if let scheduleCloseTab { return scheduleCloseTab == .never }
        return childLinger < 0
    }

    /// Keep the task title as protocol data; the schedule marker belongs only to Session naming.
    static func sessionTitle(taskTitle: String, scheduled: Bool) -> String {
        guard scheduled, !taskTitle.hasPrefix("[Task]") else { return taskTitle }
        return "[Task] \(taskTitle)"
    }

    static func automaticCloseAt(for task: Task, outcome: State, now: Date = Date(),
                                 childLinger: Int) -> Date? {
        guard task.attachSessionId == nil, !task.sessionRoot else { return nil }
        if task.scheduleID != nil {
            return scheduledCloseAt(policy: task.scheduleCloseTab, outcome: outcome, now: now,
                                    hasChild: task.childTerminalId != nil,
                                    linger: TimeInterval(childLinger),
                                    briefed: task.briefedAt != nil)
        }
        guard task.childTerminalId != nil, childLinger >= 0 else { return nil }
        if outcome == .success || outcome == .failure {
            return now.addingTimeInterval(TimeInterval(childLinger))
        }
        return outcome == .spawnFailed && task.briefedAt == nil ? now : nil
    }

    static func retainedTaskOwnsRootSession(_ task: Task) -> Bool {
        task.state != .cancelled && task.attachSessionId == nil
            && task.childTerminalId != nil && task.closeAt == nil
    }

    // MARK: - What a finished line no longer needs

    /// When a reclaimable directory falls due, from one grace setting and one ending.
    ///
    /// Shared by `work/` and by the isolated checkout's build output so the two settings cannot
    /// drift into meaning different things: `0` and every success go now, a positive number is
    /// minutes of diagnostic grace, and `-1` hands the directory to the 24-hour sweep.
    static func reclaimDeadline(minutes: Int, outcome: State, now: Date = Date()) -> Date? {
        if outcome == .success || minutes == 0 { return now }
        if minutes > 0 { return now.addingTimeInterval(TimeInterval(minutes * 60)) }
        return nil
    }

    /// The four places the reclaim passes act on, and the only spelling of them those passes read.
    ///
    /// Production answers this Mac's directories. The test binary installs temporary roots once at
    /// launch (`configureTestIsolation`), so a reclaim pass reached from any test — including every
    /// test that runs `cleanup()` — can only touch a fixture. Everything written against this value
    /// is outside B-SUITE-WALKS-LIVE-WORKTREES by construction rather than by review.
    struct ReclaimRoots: Equatable {
        /// Every checkout the broker created: `~/Library/Application Support/Clawdline/worktrees`.
        var worktrees: URL
        /// Task protocol directories: `/tmp/.clawdline`.
        var tasks: URL
        /// Scratch contract v1's owned root, `${CLAWDLINE_SCRATCH_ROOT:-/tmp/clawdline-scratch}`
        /// (`docs/scratch.md`). The raw spelling, so a relative value is refused rather than
        /// resolved against this process's working directory.
        var scratch: String
        /// Where a removed landed checkout's verified delta is kept:
        /// `~/Library/Application Support/Clawdline/reclaimed-checkouts`.
        var preserved: URL
    }

    static var reclaimRootsOverrideForTesting: ReclaimRoots?

    static var reclaimRoots: ReclaimRoots {
        if let override = reclaimRootsOverrideForTesting { return override }
        let scratch = ProcessInfo.processInfo.environment["CLAWDLINE_SCRATCH_ROOT"] ?? ""
        return ReclaimRoots(
            worktrees: OrchestratorDraft.worktreeRoot, tasks: root,
            scratch: scratch.isEmpty ? "/tmp/clawdline-scratch" : scratch,
            preserved: FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(
                "Library/Application Support/Clawdline/reclaimed-checkouts", isDirectory: true))
    }

    /// Untracked directories a tool writes into whatever checkout it is started in. `git status`
    /// counts them, and they hold nothing anybody delivered, so they do not make a checkout dirty
    /// **for disposal** and are not part of a preserved delta. Nothing else changes meaning:
    /// `Worktree.dirty` and every other reader still count them.
    ///
    /// A measured list rather than a pattern, and each name has to earn its place. Read-only on
    /// 2026-09-11 across the 49 checkouts then under the worktree root: `.serena/` — Serena's
    /// per-project cache and generated `project.yml` — was untracked in 24 of them, was the only
    /// thing making 13 of the 37 dirty ones dirty, and was the only untracked dot-directory in any.
    static let worktreeToolNoiseDirectories: Set<String> = [".serena"]

    /// Directory names the build deadline reclaims beside `.build`, inside a checkout the task
    /// owns: installed JavaScript packages and a Python virtual environment. Re-measured
    /// 2026-09-11: 13 `node_modules` held 3,492 MB of the 5,167 MB under the worktree root.
    static let dependencyDirectoryNames: Set<String> = ["node_modules", ".venv"]

    /// One `git status --porcelain=v1 -z` record: its two-letter code, its path, and for a rename
    /// or copy the path it came from.
    struct WorktreeStatusEntry: Equatable {
        let code: String
        let path: String
        var original: String? = nil
        var untracked: Bool { code == "??" }
    }

    /// Parse `git status --porcelain=v1 -z`. `nil` when the bytes are not that shape: a status
    /// nobody could read is not a clean one.
    static func worktreeStatusEntries(_ output: String) -> [WorktreeStatusEntry]? {
        var fields = output.components(separatedBy: "\0")
        if fields.last == "" { fields.removeLast() }
        var entries: [WorktreeStatusEntry] = []
        var index = 0
        while index < fields.count {
            let bytes = Array(fields[index].utf8)
            index += 1
            guard bytes.count >= 4, bytes[2] == 0x20 else { return nil }
            let code = String(decoding: bytes[0..<2], as: UTF8.self)
            var entry = WorktreeStatusEntry(code: code,
                                            path: String(decoding: bytes[3...], as: UTF8.self))
            if code.contains("R") || code.contains("C") {
                guard index < fields.count, !fields[index].isEmpty else { return nil }
                entry.original = fields[index]
                index += 1
            }
            entries.append(entry)
        }
        return entries
    }

    /// Whether a status record is only tool noise: untracked, inside a listed directory at the
    /// checkout's top level. A regular file that happens to share the name is not a directory a
    /// tool wrote, and stays dirty.
    static func isWorktreeToolNoise(_ entry: WorktreeStatusEntry) -> Bool {
        guard entry.untracked else { return false }
        return worktreeToolNoiseDirectories.contains { entry.path.hasPrefix($0 + "/") }
    }

    /// Whether a finished task's own checkout may now be preserved and removed because its
    /// delivery landed. Pure: ``OrchestratorDraft/disposeLandedWorktree(_:taskID:roots:now:)``
    /// still proves every git fact before it removes anything.
    ///
    /// The owner is the task's recorded process. A Root Session keeps working in its checkout
    /// after its task ends, and a tab left open is still somebody's working directory. A task that
    /// never recorded a process is `unknown` here rather than gone, because this is the reclaim
    /// whose mistake costs work: a checkout deleted by mistake is work this app has no other copy
    /// of, and one kept by mistake is a directory.
    static func landedCheckoutDecision(state: State, landing: Landing?,
                                       owner: OwnedStorage.ProcessStatus, graceMinutes: Int,
                                       now: Date) -> OwnedStorage.Decision {
        guard graceMinutes >= 0 else { return .init(state: .held, why: "grace_disabled", eligibleAt: nil) }
        guard state.isTerminal else { return .init(state: .held, why: "task_not_terminal", eligibleAt: nil) }
        guard let landing, landing.state == .landed else {
            return .init(state: .held, why: "not_landed", eligibleAt: nil)
        }
        guard let landedAt = landing.landedAt else {
            return .init(state: .unknown, why: "landed_at_missing", eligibleAt: nil)
        }
        if owner == .absent { return .init(state: .unknown, why: "owner_unrecorded", eligibleAt: nil) }
        return ownerGoneDecision(owner, due: landedAt.addingTimeInterval(TimeInterval(graceMinutes * 60)),
                                 now: now, why: "landed")
    }

    /// Whether a `work/` that exists with no reclaim deadline outstanding may go now.
    ///
    /// Two histories arrive with the same facts. A task whose `work/` was reclaimed and then
    /// written again — a Root Session keeps working after its first task ends, and one measured
    /// on 2026-09-11 wrote a 496 MB deploy copy and a credential copy there — and a task that
    /// finished before deadlines existed and never received one (what B-RECLAIM-HAS-NO-BACKFILL
    /// recorded). Both take the grace the first reclaim would have used, counted from when the
    /// task settled, and neither is removed while the task's recorded process runs. A task that
    /// never recorded a process has nobody who could be writing there, which is the one place
    /// `.absent` counts as gone.
    static func afterFinishWorkDecision(state: State, workCleanupAt: Date?, settledAt: Date,
                                        owner: OwnedStorage.ProcessStatus, graceMinutes: Int,
                                        now: Date) -> OwnedStorage.Decision {
        guard state.isTerminal else { return .init(state: .held, why: "task_not_terminal", eligibleAt: nil) }
        guard workCleanupAt == nil else {
            return .init(state: .held, why: "deadline_pending", eligibleAt: workCleanupAt)
        }
        guard let due = reclaimDeadline(minutes: graceMinutes, outcome: state, now: settledAt) else {
            return .init(state: .held, why: "grace_disabled", eligibleAt: nil)
        }
        return ownerGoneDecision(owner, due: due, now: now, why: "after_finish")
    }

    /// `.build` on its own deadline: the deadline says when, and the task's process says whether.
    /// It is the gate the dependency directories beside it use, because a Session still building in
    /// its checkout — a Root Session's task is terminal from its first report — would otherwise
    /// lose its build output mid-build while its `node_modules` waited. A live or unreadable owner
    /// keeps the deadline outstanding, so the beat asks again and the directory goes on the first
    /// beat after that process has gone.
    static func buildDeadlineDecision(state: State, buildCleanupAt: Date?,
                                      owner: OwnedStorage.ProcessStatus,
                                      now: Date) -> OwnedStorage.Decision {
        guard state.isTerminal else { return .init(state: .held, why: "task_not_terminal", eligibleAt: nil) }
        guard let due = buildCleanupAt else {
            return .init(state: .held, why: "no_deadline", eligibleAt: nil)
        }
        return ownerGoneDecision(owner, due: due, now: now, why: "build_deadline")
    }

    /// Dependency directories fall due on `.build`'s deadline and wait, as `.build` now does, for
    /// the task's process to be gone: a reinstall costs minutes, and a Session still running in the
    /// checkout may be using them. A live owner is not refused, only deferred — the six-hourly
    /// pass asks again with no deadline outstanding, which is also how a task that never received
    /// a build deadline gets its `.build` and dependencies reclaimed.
    static func dependencyReclaimDecision(state: State, buildCleanupAt: Date?, settledAt: Date,
                                          owner: OwnedStorage.ProcessStatus, graceMinutes: Int,
                                          now: Date) -> OwnedStorage.Decision {
        guard state.isTerminal else { return .init(state: .held, why: "task_not_terminal", eligibleAt: nil) }
        guard buildCleanupAt == nil else {
            return .init(state: .held, why: "deadline_pending", eligibleAt: buildCleanupAt)
        }
        guard let due = reclaimDeadline(minutes: graceMinutes, outcome: state, now: settledAt) else {
            return .init(state: .held, why: "grace_disabled", eligibleAt: nil)
        }
        return ownerGoneDecision(owner, due: due, now: now, why: "build_deadline")
    }

    private static func ownerGoneDecision(_ owner: OwnedStorage.ProcessStatus, due: Date, now: Date,
                                          why: String) -> OwnedStorage.Decision {
        switch owner {
        case .unreadable: return .init(state: .unknown, why: "owner_unreadable", eligibleAt: nil)
        case .alive: return .init(state: .held, why: "owner_alive", eligibleAt: nil)
        case .absent, .dead, .reused: break
        }
        guard due <= now else { return .init(state: .held, why: "grace", eligibleAt: due) }
        return .init(state: .releasable, why: why, eligibleAt: due)
    }

    /// One task as the sweep sees it. `settledAt` is `finishedAt ?? created` — when the task
    /// stopped being live, which is the clock both windows read; `created` is what the count
    /// orders by, because that is what it has always ordered by. `ownerLive` says the task's
    /// recorded process is still running, or could not be ruled out.
    struct TaskRetentionCandidate: Equatable {
        let id: String
        let terminal: Bool
        let landingPending: Bool
        let created: Date
        let settledAt: Date
        var ownerLive = false
    }

    /// The two limits, answered separately so that a caller — and a test — can say which one
    /// fired. They are deliberately not the same window. `directories` is heavyweight working
    /// space under `/tmp/.clawdline`; `records` is the registry row, which is small and is the
    /// only durable evidence the usage Feature classifier has, so it is worth keeping for far
    /// longer than the directory it names. Three settings decide it:
    /// `orchestrator_task_dir_retention_hours` for the first, and
    /// `orchestrator_task_record_limit` (a valve on file size) together with
    /// `orchestrator_task_record_retention_days` (the retention policy) for the second.
    ///
    /// Either record limit fires alone: the count drops what is past `recordLimit` however recent
    /// it is, and the age drops what is past `recordDays` however few records there are. A
    /// pending landing is exempt from both, unconditionally, and a task that is not terminal is
    /// never aged out by either clock.
    ///
    /// **A finished task whose recorded process is still running keeps its directory, and the
    /// record that names it.** A Root Session's task is terminal from its first report while the
    /// Session goes on writing; before this, the day-old sweep deleted that directory — `work/`
    /// included — under the live Session, while the after-finish `work/` rule waited for the
    /// same process to be gone. Both now say the same thing. The record is held with it so the
    /// directory is never left with nothing to sweep it. **That holds whichever limit reaches the
    /// row, the count included**: `taskRetentionOwnerQuestions` names every row whose owner has to
    /// be known, from the same reach this answers from.
    static func taskRetentionSweep(_ rows: [TaskRetentionCandidate], now: Date = Date(),
                                   directoryHours: Int, recordLimit: Int, recordDays: Int)
        -> (directories: [String], records: [String]) {
        let reach = taskRetentionReach(rows, now: now, directoryHours: directoryHours,
                                       recordLimit: recordLimit, recordDays: recordDays)
        return (rows.filter { reach.directories.contains($0.id) && !$0.ownerLive }.map(\.id),
                rows.filter {
                    reach.records.contains($0.id) && !($0.terminal && $0.ownerLive)
                }.map(\.id))
    }

    /// Every terminal row either limit reaches before any owner is consulted — the directory's
    /// hours, the record's age, **or the record count**, which reaches a task that finished an
    /// hour ago as readily as one that finished last year. These are the rows whose owner the
    /// sweep has to know. Asking only the rows past the directory cutoff is how a Root Session
    /// that finished an hour ago, still working, could lose its registry row to the count.
    static func taskRetentionOwnerQuestions(_ rows: [TaskRetentionCandidate], now: Date,
                                            directoryHours: Int, recordLimit: Int, recordDays: Int)
        -> Set<String> {
        let reach = taskRetentionReach(rows, now: now, directoryHours: directoryHours,
                                       recordLimit: recordLimit, recordDays: recordDays)
        return Set(rows.filter {
            $0.terminal && (reach.directories.contains($0.id) || reach.records.contains($0.id))
        }.map(\.id))
    }

    /// What each limit reaches with no owner consulted. The sweep and the owner questions both
    /// answer from this one reading, so which rows are asked about cannot drift from which rows go.
    private static func taskRetentionReach(_ rows: [TaskRetentionCandidate], now: Date,
                                           directoryHours: Int, recordLimit: Int, recordDays: Int)
        -> (directories: Set<String>, records: Set<String>) {
        let directoryCutoff = now.addingTimeInterval(-Double(directoryHours) * 3600)
        let recordCutoff = now.addingTimeInterval(-Double(recordDays) * 86_400)
        let overCount = Set(rows.sorted { $0.created > $1.created }
                                .dropFirst(recordLimit).map(\.id))
        return (Set(rows.filter {
                    $0.terminal && !$0.landingPending && $0.settledAt < directoryCutoff
                }.map(\.id)),
                Set(rows.filter {
                    !$0.landingPending
                        && (overCount.contains($0.id) || ($0.terminal && $0.settledAt < recordCutoff))
                }.map(\.id)))
    }

    /// One registry row as the sweep sees it.
    static func retentionCandidate(_ task: Task, ownerLive: Bool = false) -> TaskRetentionCandidate {
        TaskRetentionCandidate(id: task.id, terminal: task.state.isTerminal,
                               landingPending: task.landing?.state == .pending,
                               created: task.created, settledAt: task.finishedAt ?? task.created,
                               ownerLive: ownerLive)
    }
}
