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
/// - **The slot handoff is the broker's to make.** The holder is the first entry still in the
///   queue, so a slot completing hands over by arithmetic; ``advance(project:now:readiness:
///   deliver:)`` is what turns that into a message the next line actually receives, once.
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
    enum Placement: String {
        case ordered, unplaced
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

        var sortedPaths: [String] { paths.keys.sorted() }
    }

    /// A member with a coordinator's answer attached.
    struct Entry: Equatable {
        let member: Member
        /// 1-based, and nil for an entry nobody has placed. Nil is a position too: it means the
        /// queue knows about this line and the coordinator has not yet said where it goes.
        let position: Int?
        let placement: Placement
        /// The one entry that may land now. Derived, so a slot completing moves it with no write.
        let holder: Bool
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
                head: branch.flatMap { branches.heads[$0] } ?? task.worktree?.head)
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
            return Member(
                rootKey: key, digest: rootKeyDigest(key),
                label: sorted.compactMap { $0.task.rootLabel }.first,
                sessionID: root?.rootSessionId,
                reasons: reasons,
                since: sorted.first?.member.since ?? now,
                tasks: sorted.map(\.member), paths: paths)
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

    // MARK: - Position

    /// The queue as a reader sees it: the coordinator's order first, then everyone it has not
    /// placed, and exactly one holder.
    ///
    /// **An unplaced entry is still an entry.** That is the whole design: an order can be
    /// incomplete, out of date, or written by somebody who did not know about a line, and none of
    /// those can remove a row. The cost is that `position` is nullable and the reader has to
    /// notice; the benefit is that there is no state in which the queue is quietly short.
    static func entries(members: [Member], order: Order) -> [Entry] {
        let placement = Dictionary(order.keys.enumerated().map { ($0.element, $0.offset) },
                                   uniquingKeysWith: { first, _ in first })
        let ordered = members.filter { placement[$0.digest] != nil }
            .sorted { (placement[$0.digest] ?? 0) < (placement[$1.digest] ?? 0) }
        let unplaced = members.filter { placement[$0.digest] == nil }
        var out: [Entry] = []
        for (index, member) in ordered.enumerated() {
            out.append(Entry(member: member, position: index + 1, placement: .ordered,
                             holder: index == 0))
        }
        for (index, member) in unplaced.enumerated() {
            out.append(Entry(member: member, position: nil, placement: .unplaced,
                             holder: ordered.isEmpty && index == 0))
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
        Orchestrator.lock.lock()
        let tasks = Array(Orchestrator.tasks.values)
        Orchestrator.lock.unlock()
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
            "position": entry.position as Any? ?? NSNull(),
            "placement": entry.placement.rawValue,
            "holder": entry.holder,
            "reasons": entry.member.reasons.map(\.rawValue),
            "since": Int(entry.member.since.timeIntervalSince1970),
            "age_seconds": ageSeconds(since: entry.member.since, now: now),
            "paths": entry.member.sortedPaths,
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

    static func orderRecord(_ order: Order, stale: [String], unplaced: [String]) -> [String: Any] {
        ["keys": order.keys,
         "generation": order.generation,
         "updated": order.updated.map { Int($0.timeIntervalSince1970) } as Any? ?? NSNull(),
         "set_by": order.setBy as Any? ?? NSNull(),
         "stale": stale,
         "unplaced": unplaced]
    }

    // MARK: - Routes

    /// `GET /v1/orchestrator/landing-queue`.
    static func queueReply(project: String, now: Date = Date()) -> Orchestrator.Reply {
        guard let repository = resolveRepository(project) else {
            return .refused(400, "bad_request",
                            "project must be an absolute path inside a Git repository.")
        }
        let state = snapshot(repository: repository, now: now)
        let unplaced = state.entries.filter { $0.placement == .unplaced }.map(\.member.digest)
        return .ok([
            "repository": repository,
            "queue": state.entries.map { entryRecord($0, now: now) },
            "order": orderRecord(state.order, stale: state.stale, unplaced: unplaced),
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
        let unplaced = after.entries.filter { $0.placement == .unplaced }.map(\.member.digest)
        return .ok(["ok": true,
                    "repository": repository,
                    "queue": after.entries.map { entryRecord($0, now: now) },
                    "order": orderRecord(after.order, stale: after.stale, unplaced: unplaced),
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
        body += "This message is a copy of "
            + "GET /v1/orchestrator/landing-queue?project=\(repository), and that route is the "
            + "authority — read it rather than this sentence before you integrate."
        return body
    }

    /// `POST /v1/orchestrator/landing-queue/advance`.
    ///
    /// The holder is derived, so nothing here decides who is next: the previous holder left the
    /// queue when its landing was recorded and the arithmetic moved on without being told. What
    /// this route adds is the one thing arithmetic cannot do, which is reach the session that is
    /// now at the front. The receipt makes it idempotent — a second call after a successful
    /// delivery answers `already_notified` rather than typing the same paragraph twice.
    static func advance(project: String, now: Date = Date(),
                        readiness: (String) -> String? = { _ in nil },
                        deliver: (String, String) -> String?) -> Orchestrator.Reply {
        guard let repository = resolveRepository(project) else {
            return .refused(400, "bad_request",
                            "project must be an absolute path inside a Git repository.")
        }
        let state = snapshot(repository: repository, now: now)
        guard let holder = state.entries.first(where: \.holder) else {
            return .refused(409, "queue_empty",
                            "Nothing is outstanding in this repository, so there is no slot to "
                                + "hand on.")
        }
        let unplaced = state.entries.filter { $0.placement == .unplaced }.map(\.member.digest)
        func answer(_ delivered: Bool, _ note: String) -> Orchestrator.Reply {
            .ok(["ok": true, "repository": repository, "delivered": delivered, "reason": note,
                 "holder": entryRecord(holder, now: now),
                 "queue": state.entries.map { entryRecord($0, now: now) },
                 "order": orderRecord(state.order, stale: state.stale, unplaced: unplaced),
                 "contended_paths": state.contended.map(contentionRecord),
                 "at": Int(now.timeIntervalSince1970)])
        }
        if state.order.notifiedDigest == holder.member.digest,
           state.order.notifiedGeneration == state.order.generation {
            return answer(false, "already_notified")
        }
        guard let session = holder.member.sessionID else {
            return .refused(status: 409, code: "holder_unreachable",
                            message: "The entry at the front of the queue has no Clawdline "
                                + "session to type into, so the slot cannot be handed on "
                                + "automatically.",
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
