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
    enum LandingState: String {
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
    static func nothingToLandAdmission(for task: Task) -> NothingToLandAdmission {
        if !task.claimKeys.isEmpty {
            return .refused("this task claimed \(task.claimKeys.count) path(s) to write")
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
        /// Present only for work created from a schedule file. The public registry exposes the
        /// id; the two policy values stay internal so an edit to the source file cannot rewrite
        /// what should happen to a task already in flight.
        var scheduleID: String?
        var scheduleCloseTab = ScheduleCloseTab.onSuccess
        var scheduleNotifyFailure = true
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
        /// The existing standing Session this task was delivered into. Nil means Clawdline
        /// opened `childTerminalId` for this task and therefore owns that tab's lifecycle.
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
}
