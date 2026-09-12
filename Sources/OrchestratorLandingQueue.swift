import Foundation

/// Who is waiting to land in one repository, in what order, and what they will collide over.
///
/// **Why this is a broker primitive and not a coordinator's list.** On 2026-09-03 one session
/// coordinated seven lines landing into one shared checkout and kept the order in messages. It
/// worked, and it failed in four measurable ways: two roots were working in that checkout and
/// were not in the list at all (found by accident while scanning for orphaned commits); one slot
/// was filed under the wrong line and both lines spent time correcting it; a wait's prose
/// foregrounded one of its four `paths` and the waiting line believed the conflict surface was one
/// file for three hours; and the one real ordering constraint — three lines changing
/// `orchestrator_ceiling`, two upward and one downward, where putting the downward one in the
/// middle costs an extra re-measure — was discovered by one of the lines rather than by the queue.
///
/// Each of those is a property of *where the answer was kept*, so each is answered here by moving
/// the answer rather than by asking people to be more careful:
///
/// - **Membership is derived and cannot be written.** ``members(tasks:repository:branches:
///   retainedPaths:deliveryPaths:now:)`` reads the task registry through the same
///   ``Orchestrator/workVisibility(state:landing:isolated:branchExists:branchMerged:)`` the
///   inflight list uses. There is no add call and no remove call, so there is nothing to forget.
/// - **Position is stored, and only position is stored.** A coordinator sets an order over root
///   keys; an entry the order does not mention is still an entry, at the end, marked `unplaced`.
///   An order naming somebody who has left is reported as stale rather than reviving them.
/// - **Contention is computed.** Two entries writing the same path are reported as such, from the
///   landing-time write sets and from the delivery branches' own diffs.
/// - **The slot handoff is the broker's to make, and there is one slot per repository.** The turn
///   belongs to the first of this repository's ready candidates, so a slot completing hands over by
///   arithmetic; ``advance(project:now:readiness:deliver:)`` is what turns that into a message the
///   next line actually receives, once. One repository is one shared index, one staging area and
///   one `HEAD`, so two candidates on two target branches of it are not two slots; only different
///   repositories land in parallel. The older ``Entry/holder`` flag — the first entry in the queue,
///   ready or not — stays as display and is named as compatibility where it is published.
///
/// **Where a root can still be absent, said out loud because the point of this file is that it
/// cannot happen.** Three gaps remain and none of them is closed by anything here:
/// a person working in the checkout with no Clawdline task at all is invisible to the broker
/// entirely; a task whose `project_dir` was declared outside the repository lands in another
/// queue; and a terminal delivery whose root never declared `landing: pending` can be evicted by
/// the registry's newest-200 cleanup, which pending landings are exempt from and it is not. The
/// first is the real one, and it is a boundary of the broker rather than of this queue.
enum OrchestratorLandingQueue {

    // MARK: - Vocabulary

    /// Why an entry is in the queue. Derived every time, never stored.
    enum Reason: String {
        /// A session is still working on it. This is the reason failure 1 needs: both black holes
        /// were roots whose children were mid-flight, so a queue that admitted only delivered work
        /// would have kept exactly the two entries it lost.
        case liveWork = "live_work"
        /// Terminal, and its delivery is still on a branch nobody has merged.
        case unlandedDelivery = "unlanded_delivery"
        /// The root declared the obligation itself through `POST /v1/orchestrator/tasks/:id/landing`.
        case pendingLanding = "pending_landing"
    }

    /// Whether a coordinator has said where this entry goes.
    ///
    /// A coordinator's sentence about rows, and **not landing authority**: it is the first
    /// component of the derived key — so an explicit order does decide the turn — but `ordered`
    /// by itself says only that somebody placed this line, including a line that is still
    /// working. ``Entry/turn`` is the field that says who may use the shared checkout.
    enum Placement: String {
        case ordered, unplaced
    }

    /// Why an entry is **not** a ready candidate, and therefore holds no derived position.
    ///
    /// Every value names a fact that was read — the registry's, or git's — rather than a judgement
    /// about the work. The set is closed because the alternative to a reason is a guess: an entry
    /// whose target this side cannot read is not an entry landing on `main`, it is an entry whose
    /// target this side cannot read, and those two are the same row only until somebody lands on
    /// the wrong branch.
    enum NotCandidate: String {
        /// A contributing task is still live. One is enough: however much of the rest of the line
        /// has finished, a root whose child is mid-flight is not ready to take the shared checkout.
        case liveWork = "live_work"
        /// No contributing task named a target, or two named different ones. **Never defaulted to
        /// `main`.** A landing record's `target` is optional, so an entry that is only an unmerged
        /// delivery ordinarily has none, and that is this value rather than an assumption.
        case targetUnreadable = "target_unreadable"
        /// A contributing delivery names a branch this repository's git did not answer for, so
        /// what would land cannot be named. Candidate identity is part of being ready.
        case deliveryUnreadable = "delivery_unreadable"
        /// git answered nothing about this repository at all
        /// (``Orchestrator/RepositoryBranches/known`` is false), so every branch fact an order
        /// would rest on is unknown. Membership stays visible — that fail-safe is
        /// ``Orchestrator/workVisibility(state:landing:isolated:branchExists:branchMerged:)``'s
        /// and is not changed here — while ordering fails closed, which is the opposite direction
        /// on purpose: showing a line costs a glance, ordering an unreadable one costs a landing.
        case repositoryUnreadable = "repository_unreadable"
    }

    /// Which evidence put a path in an entry's write set. `both` is not a third source; it is the
    /// two agreeing, which is the ordinary case for a delivery that did what it said it would.
    enum PathSource: String {
        case claims
        case deliveryDiff = "delivery_diff"
        case both

        func joined(with other: PathSource) -> PathSource {
            self == other ? self : .both
        }
    }

    /// One task's contribution to an entry.
    struct MemberTask: Equatable {
        let id: String
        let title: String
        let state: String
        let visibility: Orchestrator.WorkVisibility
        let reason: Reason
        let since: Date
        let branch: String?
        let base: String?
        let head: String?
        /// The branch this task's landing obligation names, when it declared one. Optional at the
        /// source (`Orchestrator.Landing.target`), optional here, and never filled in.
        let target: String?
    }

    /// One line of work waiting to land: a root, in a repository, with everything it holds.
    ///
    /// The key is ``Orchestrator/rootKey(of:among:)``'s canonical answer — a live root's session
    /// id, or `task:<id>` for a task that resolves back to itself — and it never leaves this
    /// process. What callers see is ``digest``, the same eight hex characters a claims conflict
    /// and a landing row already publish, so the three can be compared without a conversation id
    /// going out over the wire.
    struct Member: Equatable {
        let rootKey: String
        let digest: String
        let label: String?
        let sessionID: String?
        let reasons: [Reason]
        let since: Date
        let tasks: [MemberTask]
        /// Path → where the evidence came from. Relative to the repository, exactly as declared.
        let paths: [String: PathSource]
        /// The one branch every contributing obligation names, or nil when none did or they
        /// disagreed. Nil is never `main`; see ``NotCandidate/targetUnreadable``.
        let target: String?
        /// Nil when this entry is a ready candidate, otherwise the one reason it is not.
        let notCandidate: NotCandidate?

        var sortedPaths: [String] { paths.keys.sorted() }
        /// Terminal, still holding something to land, and provable: repository, target and what
        /// would land were all read.
        var isCandidate: Bool { notCandidate == nil }
    }

    /// A member with a coordinator's answer attached.
    struct Entry: Equatable {
        let member: Member
        /// 1-based, and nil for an entry nobody has placed. Nil is a position too: it means the
        /// queue knows about this line and the coordinator has not yet said where it goes.
        let position: Int?
        let placement: Placement
        /// **Compatibility display, not landing authority.** The first entry still in the queue,
        /// which is the arithmetic this file published before ``turn`` existed: it can be an entry
        /// whose own row says `ready: false`, so a reader that treats it as the turn can call a
        /// line forward while it is still working. The field stays because readers have it and the
        /// row is worth seeing; ``turn`` is what says who may use the shared checkout.
        let holder: Bool
        /// 1-based among the ready candidates sharing this entry's **repository**, and nil for an
        /// entry that is not one. Target is part of the key and not a group: a repository has one
        /// shared index, one staging area and one set of refs, so two candidates on two branches
        /// of it are not parallel. This is the broker's own answer and it is derived on every read:
        /// nothing about it is stored, so it cannot move ``Order/generation`` and cannot re-arm a
        /// notice nobody asked for.
        let candidateOrder: Int?
        /// How many ready candidates this repository holds, so `order` can be read as "1 of 2"
        /// rather than as a rank against an unknown field.
        let candidateGroup: Int?
        /// This entry is first among **the repository's** ready candidates: it has the one
        /// shared-checkout turn, and ``member``'s ``Member/target`` says which branch that turn is
        /// for. This is the landing-slot authority, and ``holder`` and ``placement`` beside it are
        /// compatibility display. **It is still not approval to land** — see ``slotNotice(
        /// repository:entry:total:previous:contended:)``, which has to say so in words.
        let turn: Bool
    }

    /// One path two or more entries will write when they land.
    struct Contention: Equatable {
        let path: String
        /// Digest → why that entry is on this path, in queue order.
        let entries: [(digest: String, source: PathSource)]

        static func == (lhs: Contention, rhs: Contention) -> Bool {
            lhs.path == rhs.path
                && lhs.entries.count == rhs.entries.count
                && zip(lhs.entries, rhs.entries).allSatisfy {
                    $0.0.digest == $0.1.digest && $0.0.source == $0.1.source
                }
        }
    }

    /// The stored half: an order over digests, and the receipt for the last handoff typed out.
    struct Order: Equatable {
        var keys: [String] = []
        /// Bumped by every accepted `order` write. A handoff notice is owed again when this moves,
        /// because a re-ordered queue is a different instruction even to the same holder.
        var generation: Int = 0
        var updated: Date?
        var setBy: String?
        var notifiedDigest: String?
        var notifiedAt: Date?
        var notifiedGeneration: Int?
    }

    // MARK: - Derived membership

    /// Every root with outstanding work in one repository, oldest first.
    ///
    /// Pure on purpose: the two facts that need a subprocess — which delivery branches exist, and
    /// what each one changed — arrive as parameters, so the whole shape can be exercised against
    /// a described repository. `retainedPaths` is the landing-time write set kept by
    /// ``retainLandingPaths(_:)`` for isolated tasks, whose edit-time `claims` are deliberately
    /// empty; see that function for why those are two different questions.
    static func members(tasks: [Orchestrator.Task], repository: String,
                        branches: Orchestrator.RepositoryBranches,
                        retainedPaths: [String: [String]],
                        deliveryPaths: [String: [String]],
                        now: Date) -> [Member] {
        let indexed = Dictionary(tasks.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let prefix = repository.hasSuffix("/") ? repository : repository + "/"
        var byRoot: [String: [(task: Orchestrator.Task, member: MemberTask)]] = [:]
        for task in tasks {
            let home = task.worktree?.repository ?? task.projectDir
            guard home == repository || home.hasPrefix(prefix) else { continue }
            let branch = task.worktree?.branch
            let visibility = Orchestrator.workVisibility(
                state: task.state, landing: task.landing, isolated: task.worktree != nil,
                branchExists: branch.flatMap { branches.known ? branches.heads[$0] != nil : nil },
                branchMerged: branch.flatMap { branches.known ? branches.merged.contains($0) : nil })
            guard visibility != .settled else { continue }
            let reason: Reason
            if task.landing?.state == .pending { reason = .pendingLanding }
            else if visibility == .live { reason = .liveWork }
            else { reason = .unlandedDelivery }
            let member = MemberTask(
                id: task.id, title: task.title, state: task.state.rawValue,
                visibility: visibility, reason: reason,
                since: task.landing?.since ?? task.created,
                branch: branch, base: task.worktree?.base,
                head: branch.flatMap { branches.heads[$0] } ?? task.worktree?.head,
                target: task.landing?.target)
            byRoot[rootKey(of: task, among: indexed), default: []]
                .append((task, member))
        }
        return byRoot.map { key, rows in
            let sorted = rows.sorted { $0.member.since < $1.member.since }
            var paths: [String: PathSource] = [:]
            for row in sorted {
                for path in landingPaths(of: row.task, retainedPaths: retainedPaths) {
                    paths[path] = paths[path]?.joined(with: .claims) ?? .claims
                }
                guard let branch = row.member.branch else { continue }
                for path in deliveryPaths[branch] ?? [] {
                    paths[path] = paths[path]?.joined(with: .deliveryDiff) ?? .deliveryDiff
                }
            }
            var reasons: [Reason] = []
            for reason in [Reason.pendingLanding, .unlandedDelivery, .liveWork]
            where sorted.contains(where: { $0.member.reason == reason }) {
                reasons.append(reason)
            }
            let root = sorted.first { $0.task.rootSessionId != nil }?.task
            let memberTasks = sorted.map(\.member)
            let ready = candidacy(reasons: reasons, tasks: memberTasks, branches: branches)
            return Member(
                rootKey: key, digest: rootKeyDigest(key),
                label: sorted.compactMap { $0.task.rootLabel }.first,
                sessionID: root?.rootSessionId,
                reasons: reasons,
                since: sorted.first?.member.since ?? now,
                tasks: memberTasks, paths: paths,
                target: ready.target, notCandidate: ready.notCandidate)
        }.sorted { first, second in
            first.since == second.since ? first.digest < second.digest : first.since < second.since
        }
    }

    /// What one task will write into the target tree when it lands.
    ///
    /// For a task in the shared checkout this is its `claims`, which is also its edit-time lease.
    /// For an isolated one the lease is empty by design and this is the list the dispatch response
    /// handed back as `claims_ignored_for_worktree` and nobody kept — see ``retainLandingPaths(_:)``.
    static func landingPaths(of task: Orchestrator.Task,
                             retainedPaths: [String: [String]]) -> [String] {
        task.claims.isEmpty ? (retainedPaths[task.id] ?? []) : task.claims
    }

    // MARK: - Ready candidates

    /// Whether one entry is a **ready candidate**, and the target it would land on.
    ///
    /// A ready candidate is an entry every contributing task of which is terminal, which still has
    /// something to land, and whose repository, target and candidate identity were all *read*. The
    /// first two are the vocabulary this file already had — ``Reason/unlandedDelivery`` or
    /// ``Reason/pendingLanding`` present and ``Reason/liveWork`` absent. The third is the half that
    /// has to be said out loud, because it is the half that fails closed: **terminal tasks alone
    /// are not readiness.** An entry the broker cannot place is left visible and unordered with the
    /// reason attached, never given a position by guessing.
    ///
    /// The checks are not mutually exclusive and the first that applies is the one reported. The
    /// order is deliberate: an unreadable repository makes every answer below it unprovable rather
    /// than false, so it is reported first; a live task is the registry's own answer and disqualifies
    /// the entry whatever git says; and of the two remaining facts the target is asked for first,
    /// because it is the one whose absence used to read as `main`.
    static func candidacy(reasons: [Reason], tasks: [MemberTask],
                          branches: Orchestrator.RepositoryBranches)
        -> (target: String?, notCandidate: NotCandidate?) {
        let declared = Set(tasks.compactMap(\.target)
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty })
        // Two obligations naming two branches is not a target this side may choose between.
        let target = declared.count == 1 ? declared.first : nil
        if !branches.known { return (target, .repositoryUnreadable) }
        if reasons.contains(.liveWork) { return (target, .liveWork) }
        guard target != nil else { return (nil, .targetUnreadable) }
        let unreadable = tasks.contains { task in
            guard let branch = task.branch else { return false }
            return branches.heads[branch] == nil
        }
        return (target, unreadable ? .deliveryUnreadable : nil)
    }

    // MARK: - Position

    /// The queue as a reader sees it: the coordinator's order first, then everyone it has not
    /// placed, and exactly one holder.
    ///
    /// **An unplaced entry is still an entry.** That is the whole design: an order can be
    /// incomplete, out of date, or written by somebody who did not know about a line, and none of
    /// those can remove a row. The cost is that `position` is nullable and the reader has to
    /// notice; the benefit is that there is no state in which the queue is quietly short.
    ///
    /// **`turn` is the landing authority and `holder` is kept as display.** They are two
    /// different answers and only one of them may move a ref: `holder` is the arithmetic this file
    /// published first — the first entry still in the queue, which can be an entry whose own row
    /// says `ready: false` — and `turn` is who, among the entries in **this repository** that are
    /// actually ready, has the single shared-checkout turn. Publishing both as peers is what let
    /// two fields disagree about one index, so the disagreement is now named rather than left for
    /// a reader to arbitrate: `holder` and `position` stay visible, `turn` decides, and an entry
    /// that holds the legacy slot without being a candidate is told why in the same row.
    static func entries(members: [Member], order: Order) -> [Entry] {
        let placement = Dictionary(order.keys.enumerated().map { ($0.element, $0.offset) },
                                   uniquingKeysWith: { first, _ in first })
        let ordered = members.filter { placement[$0.digest] != nil }
            .sorted { (placement[$0.digest] ?? 0) < (placement[$1.digest] ?? 0) }
        let unplaced = members.filter { placement[$0.digest] == nil }
        let derived = candidateOrder(members: members, placement: placement)
        func row(_ member: Member, position: Int?, placement: Placement, holder: Bool) -> Entry {
            let seat = derived[member.digest]
            return Entry(member: member, position: position, placement: placement, holder: holder,
                         candidateOrder: seat?.order, candidateGroup: seat?.of,
                         turn: seat?.order == 1)
        }
        var out: [Entry] = []
        for (index, member) in ordered.enumerated() {
            out.append(row(member, position: index + 1, placement: .ordered, holder: index == 0))
        }
        for (index, member) in unplaced.enumerated() {
            out.append(row(member, position: nil, placement: .unplaced,
                           holder: ordered.isEmpty && index == 0))
        }
        return out
    }

    /// The broker's own order over ready candidates: digest → where it sits among **the
    /// repository's** ready candidates, 1-based.
    ///
    /// **Why a key rather than a judgement.** Two roots reading this at the same moment have to get
    /// the same answer or the whole thing is worse than nothing, so the key is made of values that
    /// do not move while anybody is reading: a coordinator's stored placement, the oldest
    /// contributing task's clock — which is already this queue's published sort, so the derived
    /// order and the row order agree instead of contradicting each other — and the digest, which is
    /// the identity this route already publishes, as the last tie-break. Nothing in it is measured
    /// at read time, so the same queue state answers the same way for as long as it lasts.
    ///
    /// **An explicit coordinator order always wins**, and it wins by being the first component of
    /// the key rather than by a branch somewhere below: a placed candidate sorts ahead of every
    /// unplaced one, in the coordinator's own sequence. A coordinator who has said nothing costs
    /// nothing, because `Int.max` is every unplaced entry's first component and the clock decides.
    ///
    /// **One turn per repository, because a repository is what serializes.** An earlier version of
    /// this function grouped the candidates by target and gave each group its own first place, on
    /// the reasoning that two branches are two refs. Two branches are two refs and **one** index:
    /// a candidate for `main` and a candidate for `release` in the same checkout stage into the
    /// same `.git/index`, commit with the same `HEAD`, and leave each other's half-staged files
    /// behind — which is the failure the whole slot exists to prevent, reintroduced one level down.
    /// So target is part of the key and never a group: it is part of a candidate's identity, it
    /// sorts deterministically, and the answer says which target the single turn is for.
    ///
    /// Entries in different repositories never meet here at all —
    /// ``members(tasks:repository:branches:retainedPaths:deliveryPaths:now:)`` filters by
    /// repository before this is reached — and those are the only genuinely parallel landings.
    static func candidateOrder(members: [Member], placement: [String: Int])
        -> [String: (order: Int, of: Int)] {
        let candidates = members.filter(\.isCandidate).sorted { first, second in
            let left = placement[first.digest] ?? Int.max
            let right = placement[second.digest] ?? Int.max
            if left != right { return left < right }
            if first.since != second.since { return first.since < second.since }
            if first.target != second.target { return (first.target ?? "") < (second.target ?? "") }
            return first.digest < second.digest
        }
        var out: [String: (order: Int, of: Int)] = [:]
        for (index, candidate) in candidates.enumerated() {
            out[candidate.digest] = (order: index + 1, of: candidates.count)
        }
        return out
    }

    /// Digests the stored order names that are no longer in the queue. They are reported, never
    /// acted on: an order is a coordinator's sentence about members and cannot create one.
    static func staleOrderKeys(members: [Member], order: Order) -> [String] {
        let live = Set(members.map(\.digest))
        return order.keys.filter { !live.contains($0) }
    }

    // MARK: - Contention

    /// Every path more than one entry writes, in queue order, with the queue's own order applied
    /// so a reader sees who is in front.
    ///
    /// This is failure 4 in one call. Three lines changing `orchestrator_ceiling` show up as one
    /// row naming all three, and the row exists whether or not anybody thought to look — which is
    /// the difference between the constraint being discovered by a line that tripped over it and
    /// the constraint being visible before the order was set.
    static func contendedPaths(_ entries: [Entry]) -> [Contention] {
        var byPath: [String: [(digest: String, source: PathSource)]] = [:]
        for entry in entries {
            for path in entry.member.sortedPaths {
                guard let source = entry.member.paths[path] else { continue }
                byPath[path, default: []].append((entry.member.digest, source))
            }
        }
        return byPath.filter { $0.value.count > 1 }
            .map { Contention(path: $0.key, entries: $0.value) }
            .sorted { $0.path < $1.path }
    }

    // MARK: - The landing-time write set, kept rather than handed back and dropped

    /// ``Orchestrator/prepareClaimsForIsolation(_:)`` with the discarded list kept.
    ///
    /// **`claims` answers two questions and isolation only ends one of them.** The first is "who
    /// may edit this path in the shared checkout", and for an isolated child the honest answer is
    /// nobody-here: it writes its own checkout at a different spelling, so retaining the lease
    /// would block useful work in the base tree for no reason. That is what the source comment on
    /// `prepareClaimsForIsolation` says and it is right. The second question is "who will write
    /// this path when the work lands", and isolation does not end that one — it *defers* it, to
    /// exactly the moment this queue exists for. Emptying one field answered both.
    ///
    /// So the semantics are split rather than overloaded: `claims` stays the edit-time lease and
    /// keeps being emptied, and the landing-time write set is kept here, where landing lives. The
    /// alternative — a second field on `Orchestrator.Task` with its own store codec — is the
    /// better long-term home and is not this change: it moves a shared record and a sealed codec
    /// test, and this file can be that record's producer either way.
    ///
    /// The bug this closes is worse than a lost list. `claimsDeclared` is not cleared alongside
    /// `claims`, so an isolated task with nine declared paths is persisted as *declared and
    /// empty* — the one spelling that positively means "this task writes nothing".
    @discardableResult
    static func retainLandingPaths(_ task: inout Orchestrator.Task) -> [[String: Any]] {
        let declared = task.claims
        let warnings = prepareClaimsForIsolation(&task)
        guard !warnings.isEmpty, !declared.isEmpty else { return warnings }
        storeLock.lock()
        loadLocked()
        landingPathsByTask[task.id] = declared
        saveLocked()
        storeLock.unlock()
        return warnings
    }

    /// The write-set half of every task projection, in one place because it was wrong in two.
    ///
    /// **`claims: []` is not a blank, it is a sentence.** `Resources/skill-guides/clawdline.md`
    /// ships the definition — "`[]` positively declares the task read-only" — so a task that
    /// declared nine write paths and then had them erased by isolation does not read as unknown
    /// to anybody applying that definition. It reads as a promise to write nothing, which is a
    /// statement, and a false one. On the machine this was found on, 22 of 40 tasks were isolated,
    /// so more than half of every `claims`-shaped input was that sentence.
    ///
    /// Two rows from one root, one hour apart, read the same through this projection: an isolated
    /// delivery that went on to write twenty-six files, and a review that genuinely wrote nothing.
    /// The precise claim matters, because the first version of it was too strong — `isolation` was
    /// always readable beside `claims`, so the pair *could* be told apart by anybody who thought to
    /// read both. What was true is narrower and worse: **the field lies when it is read alone**,
    /// and a field that lies alone will be read alone.
    ///
    /// **So the repair is a choke point rather than a rule.** Both projections now leave through
    /// this one function, which means the broker cannot emit an erased `claims` without emitting
    /// what replaced it in the same breath — there is no call site left where somebody could
    /// forget. What it cannot do is stop a *reader* from copying `claims` alone into some third
    /// place; nothing here reaches that far, and saying otherwise would be the same overclaim
    /// again. It emits three things rather than one:
    ///
    /// - `claims`, unchanged: the edit-time lease, still emptied for an isolated child.
    /// - `claims_declared`, new: whether a write set was declared at dispatch at all. Without it,
    ///   an empty `claims` cannot be told from an absent one from the outside.
    /// - `landing_paths`, new and present only when there is something to say: what this delivery
    ///   will write when it lands, kept by ``retainLandingPaths(_:)``.
    ///
    /// `alwaysClaims` keeps the two callers' existing contracts, which differ on purpose: a task
    /// record omits `claims` entirely when nothing was declared, because there absence means "the
    /// write set is unknown" and an empty array would mean the opposite; an inflight row has
    /// always carried the key.
    static func projectWriteSet(of task: Orchestrator.Task, into out: inout [String: Any],
                                alwaysClaims: Bool = false) {
        if task.claimsDeclared || alwaysClaims { out["claims"] = task.claims }
        out["claims_declared"] = task.claimsDeclared
        let retained = retainedLandingPaths()[task.id] ?? []
        if !retained.isEmpty { out["landing_paths"] = retained }
    }

    // MARK: - The store

    static var storeURLOverrideForTesting: URL?
    static var storeURL: URL {
        storeURLOverrideForTesting
            ?? RemoteAuth.directory.appendingPathComponent("landing-queue.json")
    }

    /// This file's own lock, and deliberately not ``Orchestrator/lock``.
    ///
    /// ``retainLandingPaths(_:)`` is called from the middle of a dispatch, above the point where
    /// dispatch takes the registry lock. Reaching for that lock here would put a second acquisition
    /// inside a region that is about to acquire it, which is the shape that produced this
    /// repository's `exit 133`.
    private static let storeLock = NSLock()
    private static var loaded = false
    private static var landingPathsByTask: [String: [String]] = [:]
    private static var queues: [String: Order] = [:]

    /// Drop everything in memory so the next read comes off disk. For tests, and for the same
    /// reason ``Orchestrator/forget()`` exists.
    static func forget() {
        storeLock.lock()
        loaded = false
        landingPathsByTask = [:]
        queues = [:]
        storeLock.unlock()
    }

    private static func loadLocked() {
        guard !loaded else { return }
        loaded = true
        guard let data = try? Data(contentsOf: storeURL),
              let raw = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { return }
        for (taskID, paths) in raw["landing_paths"] as? [String: [String]] ?? [:] {
            landingPathsByTask[taskID] = paths.filter { !$0.isEmpty }
        }
        for (repository, row) in raw["queues"] as? [String: [String: Any]] ?? [:] {
            var order = Order()
            order.keys = (row["order"] as? [String] ?? []).filter { !$0.isEmpty }
            order.generation = row["generation"] as? Int ?? 0
            order.updated = (row["updated"] as? Double).map(Date.init(timeIntervalSince1970:))
            order.setBy = row["set_by"] as? String
            order.notifiedDigest = row["notified_digest"] as? String
            order.notifiedAt = (row["notified_at"] as? Double)
                .map(Date.init(timeIntervalSince1970:))
            order.notifiedGeneration = row["notified_generation"] as? Int
            queues[repository] = order
        }
    }

    @discardableResult
    private static func saveLocked() -> Bool {
        var rows: [String: Any] = [:]
        for (repository, order) in queues {
            var row: [String: Any] = ["order": order.keys, "generation": order.generation]
            if let updated = order.updated { row["updated"] = updated.timeIntervalSince1970 }
            if let setBy = order.setBy { row["set_by"] = setBy }
            if let digest = order.notifiedDigest { row["notified_digest"] = digest }
            if let at = order.notifiedAt { row["notified_at"] = at.timeIntervalSince1970 }
            if let generation = order.notifiedGeneration {
                row["notified_generation"] = generation
            }
            rows[repository] = row
        }
        let payload: [String: Any] = ["version": 1, "landing_paths": landingPathsByTask,
                                      "queues": rows]
        guard let data = try? JSONSerialization.data(withJSONObject: payload,
                                                     options: [.sortedKeys]) else { return false }
        do {
            try FileManager.default.createDirectory(at: storeURL.deletingLastPathComponent(),
                                                    withIntermediateDirectories: true)
            try data.write(to: storeURL, options: .atomic)
            // Every time rather than at creation, for the reason `Orchestrator.save` gives: an
            // atomic replace carries the replaced file's metadata forward.
            try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                                  ofItemAtPath: storeURL.path)
            return true
        } catch {
            Log.write("landing queue: could not persist the store — \(error)")
            return false
        }
    }

    static func order(for repository: String) -> Order {
        storeLock.lock(); defer { storeLock.unlock() }
        loadLocked()
        return queues[repository] ?? Order()
    }

    static func retainedLandingPaths() -> [String: [String]] {
        storeLock.lock(); defer { storeLock.unlock() }
        loadLocked()
        return landingPathsByTask
    }

    // MARK: - Reading the repository

    /// What each delivery branch changed against its own base, branch name → repository-relative
    /// paths. One `git diff` per live branch, which is the same order of subprocesses
    /// ``Orchestrator/repositoryBranches(in:)`` already pays for a queue read.
    static func deliveryDiffs(repository: String, members: [Member],
                              branches: Orchestrator.RepositoryBranches) -> [String: [String]] {
        var out: [String: [String]] = [:]
        for member in members {
            for task in member.tasks {
                guard let branch = task.branch, let base = task.base,
                      branches.heads[branch] != nil, out[branch] == nil,
                      let changed = gitOutput(["diff", "--name-only", "\(base)...\(branch)"],
                                              cwd: repository) else { continue }
                out[branch] = changed.split(whereSeparator: \.isNewline).map(String.init)
                    .filter { !$0.isEmpty }
            }
        }
        return out
    }

    /// The whole answer for one repository, assembled from the registry and from git.
    static func snapshot(repository: String, now: Date)
        -> (entries: [Entry], order: Order, contended: [Contention], stale: [String]) {
        let branches = Orchestrator.repositoryBranches(in: repository)
        Orchestrator.load()
        let tasks = OrchestratorRegistry.withTaskRecords { $0.taskValues() }
        let retained = retainedLandingPaths()
        let first = members(tasks: tasks, repository: repository, branches: branches,
                            retainedPaths: retained, deliveryPaths: [:], now: now)
        let diffs = deliveryDiffs(repository: repository, members: first, branches: branches)
        let full = members(tasks: tasks, repository: repository, branches: branches,
                           retainedPaths: retained, deliveryPaths: diffs, now: now)
        let stored = order(for: repository)
        let rows = entries(members: full, order: stored)
        return (rows, stored, contendedPaths(rows), staleOrderKeys(members: full, order: stored))
    }

    // MARK: - Projection

    static func entryRecord(_ entry: Entry, now: Date) -> [String: Any] {
        var row: [String: Any] = [
            "root_key": entry.member.digest,
            "root_label": entry.member.label as Any? ?? NSNull(),
            // `position`, `placement` and `holder` are compatibility display and are named as such
            // in `order.derived.compatibility`. `candidate.turn` below is the landing authority:
            // these three can say `holder: true` about a line that is still working, and a reader
            // that acted on that would be the second session in one index.
            "position": entry.position as Any? ?? NSNull(),
            "placement": entry.placement.rawValue,
            "holder": entry.holder,
            "reasons": entry.member.reasons.map(\.rawValue),
            "since": Int(entry.member.since.timeIntervalSince1970),
            "age_seconds": ageSeconds(since: entry.member.since, now: now),
            "paths": entry.member.sortedPaths,
            // Every key is present whether or not it has a value, for the reason the rest of this
            // row's nullable fields are: a reader deciding whether it may touch the shared
            // checkout must be able to tell "not a candidate, and here is why" from "this broker
            // does not answer that question", and an absent key cannot say either.
            "candidate": [
                "ready": entry.member.isCandidate,
                "target": entry.member.target as Any? ?? NSNull(),
                "order": entry.candidateOrder as Any? ?? NSNull(),
                "of": entry.candidateGroup as Any? ?? NSNull(),
                "turn": entry.turn,
                "why": entry.member.notCandidate?.rawValue as Any? ?? NSNull(),
            ] as [String: Any],
        ]
        row["tasks"] = entry.member.tasks.map { task -> [String: Any] in
            var out: [String: Any] = ["id": task.id, "title": task.title, "state": task.state,
                                      "visibility": task.visibility.rawValue,
                                      "reason": task.reason.rawValue]
            if let branch = task.branch {
                var delivery: [String: Any] = ["branch": branch]
                if let base = task.base { delivery["base"] = base }
                if let head = task.head { delivery["head"] = head }
                out["delivery"] = delivery
            }
            return out
        }
        return row
    }

    static func contentionRecord(_ contention: Contention) -> [String: Any] {
        ["path": contention.path,
         "entries": contention.entries.map { ["root_key": $0.digest, "source": $0.source.rawValue] }]
    }

    /// The stored order, and beside it the one the broker derived for itself.
    ///
    /// `keys`, `generation`, `updated`, `set_by`, `stale` and `unplaced` are the coordinator's
    /// half and are unchanged. `derived` is this side's own answer and is a *reading*, not a
    /// write: it names the key it used, this repository's ready candidates in that order, the one
    /// entry holding the turn and the target that turn is for, and every entry it refused to place
    /// with the reason. Nothing in it moves `generation`, because nothing in it is stored — a
    /// generation that moved on a `GET` would re-arm a slot notice on every read.
    ///
    /// `authority` and `compatibility` are published rather than left to a doc page, because the
    /// defect they answer was a reader picking the wrong field: two fields that can disagree about
    /// one shared index must say in the response which of them a landing may rest on.
    static func orderRecord(_ order: Order, stale: [String], entries: [Entry]) -> [String: Any] {
        let groups = candidateGroups(entries)
        return ["keys": order.keys,
                "generation": order.generation,
                "updated": order.updated.map { Int($0.timeIntervalSince1970) } as Any? ?? NSNull(),
                "set_by": order.setBy as Any? ?? NSNull(),
                "stale": stale,
                "unplaced": entries.filter { $0.placement == .unplaced }.map(\.member.digest),
                "derived": [
                    "basis": candidateOrderBasis,
                    "keys": groups.keys,
                    "turn": groups.turn?.digest as Any? ?? NSNull(),
                    "turn_target": groups.turn?.target as Any? ?? NSNull(),
                    "authority": derivedTurnAuthority,
                    "compatibility": legacySlotFields,
                    "unordered": groups.unordered.map { ["root_key": $0.digest, "why": $0.why] },
                ] as [String: Any]]
    }

    /// The field a landing may rest on, named in the response beside the fields that may not.
    static let derivedTurnAuthority = "queue[].candidate.turn"

    /// The two fields that predate the derived turn and are now display only. They are still
    /// published — a reader that has them keeps working and the rows are worth seeing — and a
    /// landing that rests on either can be the second one in a shared index.
    static let legacySlotFields = ["queue[].holder", "queue[].position"]

    /// The one place the derived key is written down for a reader, so the route's answer and
    /// ``candidateOrder(members:placement:)`` cannot drift into describing different sorts.
    static let candidateOrderBasis = "coordinator_order_then_oldest_work_then_target_then_digest"

    /// The derived order as a reader sees it: this repository's ready candidates in that order,
    /// the one digest holding the turn with the target it is for, and every entry the broker would
    /// not place with the reason it would not.
    ///
    /// There is deliberately no per-target row. A reader given one list per target reads each list
    /// as a queue with a front, which is the reading that put two lines in one index; the target a
    /// turn is for is named once, beside the turn.
    static func candidateGroups(_ entries: [Entry])
        -> (keys: [String], turn: (digest: String, target: String)?,
            unordered: [(digest: String, why: String)]) {
        var ordered: [(order: Int, digest: String, target: String)] = []
        var unordered: [(digest: String, why: String)] = []
        for entry in entries {
            guard let order = entry.candidateOrder, let target = entry.member.target else {
                unordered.append((entry.member.digest,
                                  entry.member.notCandidate?.rawValue ?? "unordered"))
                continue
            }
            ordered.append((order, entry.member.digest, target))
        }
        ordered.sort { $0.order < $1.order }
        let turn = ordered.first.map { (digest: $0.digest, target: $0.target) }
        return (ordered.map(\.digest), turn, unordered)
    }

    // MARK: - Routes

    /// `GET /v1/orchestrator/landing-queue`.
    static func queueReply(project: String, now: Date = Date()) -> Orchestrator.Reply {
        guard let repository = resolveRepository(project) else {
            return .refused(400, "bad_request",
                            "project must be an absolute path inside a Git repository.")
        }
        let state = snapshot(repository: repository, now: now)
        return .ok([
            "repository": repository,
            "queue": state.entries.map { entryRecord($0, now: now) },
            "order": orderRecord(state.order, stale: state.stale, entries: state.entries),
            "contended_paths": state.contended.map(contentionRecord),
            "at": Int(now.timeIntervalSince1970),
        ])
    }

    /// `POST /v1/orchestrator/landing-queue/order`.
    ///
    /// The write is exactly one sentence: these entries, in this order. It cannot add a member,
    /// it cannot remove one, and it refuses a digest that is not currently in the queue rather
    /// than storing a name for something that might come back — an order that can name absent
    /// work is an order that can quietly be about the wrong tree.
    static func setOrder(project: String, keys: [String], ifGeneration: Int?, setBy: String?,
                         now: Date = Date()) -> Orchestrator.Reply {
        guard let repository = resolveRepository(project) else {
            return .refused(400, "bad_request",
                            "project must be an absolute path inside a Git repository.")
        }
        guard keys.allSatisfy({ !$0.isEmpty && $0.count <= 64 }) else {
            return .refused(400, "bad_request", "Every order entry is a non-empty root key.")
        }
        guard Set(keys).count == keys.count else {
            return .refused(400, "duplicate_entry",
                            "An order names each entry once; a repeated root key has no position.")
        }
        let state = snapshot(repository: repository, now: now)
        let live = Set(state.entries.map(\.member.digest))
        if let missing = keys.first(where: { !live.contains($0) }) {
            return .refused(status: 409, code: "not_queued",
                            message: "\(missing) is not in this repository's landing queue. "
                                + "Membership is derived from outstanding work; an order cannot "
                                + "create it.",
                            extra: ["queued": Array(live).sorted()])
        }
        if let expected = ifGeneration, expected != state.order.generation {
            return .refused(status: 409, code: "stale_order",
                            message: "The order moved to generation \(state.order.generation) "
                                + "while this one was being written; re-read and re-send.",
                            extra: ["generation": state.order.generation])
        }
        storeLock.lock()
        loadLocked()
        var order = queues[repository] ?? Order()
        order.keys = keys
        order.generation += 1
        order.updated = now
        order.setBy = setBy
        queues[repository] = order
        saveLocked()
        storeLock.unlock()
        RemoteAuth.audit("orchestrator.landing-queue.order", [
            "repository": repository, "entries": String(keys.count),
            "generation": String(order.generation),
        ])
        let after = snapshot(repository: repository, now: now)
        return .ok(["ok": true,
                    "repository": repository,
                    "queue": after.entries.map { entryRecord($0, now: now) },
                    "order": orderRecord(after.order, stale: after.stale, entries: after.entries),
                    "contended_paths": after.contended.map(contentionRecord),
                    "at": Int(now.timeIntervalSince1970)])
    }

    /// The message the broker types into the next slot's session.
    ///
    /// **It quotes the structured answer and then says the route wins.** Failure 3 was a correct
    /// `paths` field and a message about it that foregrounded one of the four; three hours later
    /// the waiting line discovered the other three by attempting the merge. Prose summarising a
    /// list is the defect, so this prose does not summarise: it prints every path, and it names
    /// the route that will still be right after this message has aged.
    ///
    /// **And it says what the slot is, because the slot is a smaller thing than it reads as.**
    /// Holding it is permission to use the shared checkout — to update a ref on this repository
    /// and target, and to stage and commit here — and it is nothing else. There is one of it per
    /// repository, which is why the sentence names the target the turn is *for* rather than
    /// offering a turn *on* that target: a second branch is not a second index. It is not a review, not
    /// a passing suite, and nobody's approval to land. The failure that needs saying out loud is
    /// concrete: a line whose review has just finished and whose correction has not been
    /// dispatched is terminal-looking, so it can be called forward while the thing it would land
    /// is not the thing anybody agreed to; and the queue's own answer is a *reading*, taken before
    /// this message was typed, of a shared checkout that other roots are still writing in. So the
    /// notice also asks for the re-reads that cost seconds — `HEAD`, `git status` and the index
    /// in the reader's own checkout — rather than assuming the sentence it is holding is still
    /// true.
    static func slotNotice(repository: String, entry: Entry, total: Int,
                           previous: String?, contended: [Contention]) -> String {
        let position = entry.position.map(String.init) ?? "unplaced"
        var body = "[Clawdline landing queue] Repo: \(repository). The landing slot is yours: "
            + "position \(position) of \(total)"
        if let previous { body += ", after \(previous)" }
        body += ". "
        let deliveries = entry.member.tasks.compactMap { task -> String? in
            guard let branch = task.branch else { return nil }
            return task.head.map { "\(branch) at \($0)" } ?? branch
        }
        if !deliveries.isEmpty {
            body += "Your deliveries: \(deliveries.joined(separator: ", ")). "
        }
        let paths = entry.member.sortedPaths
        body += paths.isEmpty
            ? "This entry declares no landing paths. "
            : "Paths this entry writes when it lands: \(paths.joined(separator: ", ")). "
        let shared = contended.filter { row in
            row.entries.contains { $0.digest == entry.member.digest }
        }
        if !shared.isEmpty {
            let rows = shared.map { row -> String in
                let others = row.entries.map(\.digest).filter { $0 != entry.member.digest }
                return "\(row.path) with \(others.joined(separator: ", "))"
            }
            body += "Also written by entries still in the queue: \(rows.joined(separator: "; ")). "
        }
        if let target = entry.member.target, let order = entry.candidateOrder {
            body += "Ready candidate \(order) of \(entry.candidateGroup ?? order) in this "
                + "repository, and the turn is for \(target). "
        } else if let why = entry.member.notCandidate?.rawValue {
            // `advance` never reaches here: it selects the entry holding the derived turn and
            // refuses `no_ready_candidate` when there is none. A caller that composes a notice for
            // some other entry is told the entry holds no turn rather than handed a sentence that
            // is silent about it.
            body += "This entry is not a ready candidate (\(why)), so the queue has given it no "
                + "turn in this repository. "
        }
        body += "Holding the slot is the shared-checkout turn and not approval to land: it says "
            + "you may update a ref on this repository and target and stage or commit here, and "
            + "it says nothing about review, tests, or anybody having agreed to this landing. "
            + "Re-read HEAD, git status and the index in your own checkout before you stage or "
            + "commit anything. "
        body += "This message is a copy of "
            + "GET /v1/orchestrator/landing-queue?project=\(repository), and that route is the "
            + "authority — read it rather than this sentence before you integrate."
        return body
    }

    /// `POST /v1/orchestrator/landing-queue/advance`.
    ///
    /// The turn is derived, so nothing here decides who is next: the previous holder left the
    /// queue when its landing was recorded and the arithmetic moved on without being told. What
    /// this route adds is the one thing arithmetic cannot do, which is reach the session that now
    /// holds it. The receipt makes it idempotent — a second call after a successful delivery
    /// answers `already_notified` rather than typing the same paragraph twice.
    ///
    /// **It types into ``Entry/turn`` and never into ``Entry/holder``.** The legacy holder is the
    /// first entry still in the queue, ready or not, so selecting it could type "the landing slot
    /// is yours" into a session whose own row says `ready: false` — a line whose child is still
    /// out, or whose delivery branch this side could not read. Those are exactly the entries that
    /// must not be called forward, so when no entry in this repository holds the turn the route
    /// refuses `no_ready_candidate`: nothing is typed, no receipt is written, and the legacy holder
    /// is never notified as a fallback.
    static func advance(project: String, now: Date = Date(),
                        readiness: (String) -> String? = { _ in nil },
                        deliver: (String, String) -> String?) -> Orchestrator.Reply {
        guard let repository = resolveRepository(project) else {
            return .refused(400, "bad_request",
                            "project must be an absolute path inside a Git repository.")
        }
        let state = snapshot(repository: repository, now: now)
        guard !state.entries.isEmpty else {
            return .refused(409, "queue_empty",
                            "Nothing is outstanding in this repository, so there is no slot to "
                                + "hand on.")
        }
        guard let holder = state.entries.first(where: \.turn) else {
            let groups = candidateGroups(state.entries)
            return .refused(status: 409, code: "no_ready_candidate",
                            message: "No entry in this repository is a ready candidate, so there "
                                + "is nobody this slot may be handed to. Nothing was typed and no "
                                + "receipt was written; the entry holding the legacy `holder` flag "
                                + "is display and is never notified in its place.",
                            extra: ["unordered": groups.unordered.map {
                                ["root_key": $0.digest, "why": $0.why]
                            }])
        }
        func answer(_ delivered: Bool, _ note: String) -> Orchestrator.Reply {
            .ok(["ok": true, "repository": repository, "delivered": delivered, "reason": note,
                 "holder": entryRecord(holder, now: now),
                 "queue": state.entries.map { entryRecord($0, now: now) },
                 "order": orderRecord(state.order, stale: state.stale, entries: state.entries),
                 "contended_paths": state.contended.map(contentionRecord),
                 "at": Int(now.timeIntervalSince1970)])
        }
        if state.order.notifiedDigest == holder.member.digest,
           state.order.notifiedGeneration == state.order.generation {
            return answer(false, "already_notified")
        }
        guard let session = holder.member.sessionID else {
            return .refused(status: 409, code: "holder_unreachable",
                            message: "The ready candidate holding this repository's turn has no "
                                + "Clawdline session to type into, so the slot cannot be handed "
                                + "on automatically.",
                            extra: ["holder": entryRecord(holder, now: now)])
        }
        if let blocked = readiness(session) {
            return .refused(status: 409, code: "holder_busy",
                            message: blocked + " Nothing was sent and no receipt was written; "
                                + "retry this advance.",
                            extra: ["holder": entryRecord(holder, now: now)])
        }
        let previous = state.order.notifiedDigest
        let text = slotNotice(repository: repository, entry: holder,
                              total: state.entries.count, previous: previous,
                              contended: state.contended)
        if let problem = deliver(session, text) {
            RemoteAuth.audit("orchestrator.landing-queue.advance", [
                "repository": repository, "holder": holder.member.digest,
                "result": "delivery_failed",
            ])
            return .refused(status: 502, code: "handoff_delivery_failed",
                            message: problem,
                            extra: ["holder": entryRecord(holder, now: now)])
        }
        storeLock.lock()
        loadLocked()
        var order = queues[repository] ?? Order()
        order.notifiedDigest = holder.member.digest
        order.notifiedAt = now
        order.notifiedGeneration = order.generation
        queues[repository] = order
        saveLocked()
        storeLock.unlock()
        RemoteAuth.audit("orchestrator.landing-queue.advance", [
            "repository": repository, "holder": holder.member.digest, "result": "delivered",
        ])
        return answer(true, "delivered")
    }

    // MARK: - Five names that are moving out of `Orchestrator`, in one place

    /// `d2f25d29` moved thirty-seven statics — `git`, `rootKey`, `rootKeyDigest`, `ageSeconds`
    /// and `prepareClaimsForIsolation` among them — from `Orchestrator` into `OrchestratorDraft`,
    /// after this branch's base. **The two edits touch no common line, so `git merge` says nothing
    /// at all** and the first thing that mentions it is `swiftc`: `type 'Orchestrator' has no
    /// member 'isTaskID'`. That has already cost one line a green child and a red merge, and it is
    /// invisible in a staged diff because the only difference from the code around it is a type
    /// name — which is the one thing a reader does not compare.
    ///
    /// So every use in this file goes through here. On a tree that already has the extraction, the
    /// repair is `Orchestrator` → `OrchestratorDraft` on these five lines and nothing else.
    /// Returns output only on a zero status, so the `GitAnswer` type stays out of this file
    /// entirely — it moved with the function, and a type name in a signature is one more thing a
    /// merge would not mention.
    static func gitOutput(_ arguments: [String], cwd: String) -> String? {
        guard let answer = OrchestratorDraft.git(arguments, cwd: cwd), answer.status == 0
        else { return nil }
        return answer.output
    }

    static func rootKey(of task: Orchestrator.Task,
                        among existing: [String: Orchestrator.Task]) -> String {
        OrchestratorDraft.rootKey(of: task, among: existing)
    }

    static func rootKeyDigest(_ canonicalRootKey: String) -> String {
        OrchestratorDraft.rootKeyDigest(canonicalRootKey)
    }

    static func ageSeconds(since: Date, now: Date) -> Int {
        OrchestratorDraft.ageSeconds(since: since, now: now)
    }

    static func prepareClaimsForIsolation(_ task: inout Orchestrator.Task) -> [[String: Any]] {
        OrchestratorDraft.prepareClaimsForIsolation(&task)
    }

    private static func resolveRepository(_ project: String) -> String? {
        guard !project.isEmpty, project.hasPrefix("/") else { return nil }
        return Orchestrator.inflightRepository(project)
    }
}
