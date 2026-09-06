import CryptoKit
import Foundation

/// **What is already in this repository, and the receipt that says somebody read it.**
///
/// `GET /v1/orchestrator/inflight` has answered this question well since it landed: on
/// 2026-09-06 it named both live work lines and both delivered-but-unmerged branches in this
/// repository, correctly, all day, and the landing queue derived its four entries correctly
/// beside it. In the same period this Mac accumulated **26 landings nobody recorded** and **10
/// deliveries re-done from scratch on a second line while the first sat finished on a branch**.
/// The information was there. Nothing made anybody look at it.
///
/// So the defect this file is written against is not the answer, it is that reading the answer
/// was optional — the same shape `docs/landing.md` names about the record itself: "`landed` has
/// exactly one entrance and a person is standing in it." Here the entrance is
/// ``dispatchAdmission(taskID:supplied:now:)``: `POST /v1/orchestrator/tasks` and
/// `POST /v1/orchestrator/detached-tasks` refuse a body that does not carry this read's current
/// ``generation``, and the refusal hands back the whole inventory, so a caller who did not look
/// is given the thing they did not look at and one round trip is always enough to recover.
///
/// **Nothing here writes, and nothing here deletes.** Membership is derived on every call from
/// the task registry, the repository's own delivery branches and the landing queue's retained
/// write sets — for the reason the landing queue's header gives about itself: "Membership is
/// derived and cannot be written … there is no add call and no remove call, so there is nothing
/// to forget." `droppable` in particular *names* what nothing needs any more and stops there;
/// the caller decides, and the app's own `disposeWorktree` remains the only thing that removes.
enum OrchestratorInventory {

    /// Bumped when the shape of a row changes, and part of the digest below, so that a receipt
    /// taken from an older build cannot be spent against a newer answer.
    static let schemaVersion = 1

    // MARK: - Vocabulary

    enum Section: String, CaseIterable {
        /// Work in flight in this repository now.
        case live
        /// Terminal, delivered, and its branch is still unmerged.
        case unlanded
        /// A checkout or a branch nothing needs any more.
        case droppable
    }

    /// **What the caller can do about one row**, and the standard it is held to is
    /// ``UsageProjectWorktreeService/needs(_:live:branch:)``'s: *every word here names something a
    /// route on this Mac would actually accept*, so a row can never advise an action the server
    /// answers `409` to. That function refuses to invent a fourth word for a settled obligation
    /// for exactly this reason, and the same rule is what keeps this list short.
    ///
    /// `dispose` is the one word that names no route, and it is deliberate rather than an
    /// oversight: **there is no dispose route**, because nothing in this app deletes a checkout on
    /// a reader's behalf. It names what a person or a root may do with `git worktree remove` and
    /// `git branch -d`, having read the row's `why`.
    enum Action: String {
        /// Somebody is on it. Two routes accept this: `POST /v1/orchestrator/waits` registers a
        /// file wait on the session that holds the paths, and `POST /v1/orchestrator/handoffs`
        /// takes the line over whole. Dispatching a second task onto the same paths is the thing
        /// this word exists to stop.
        case coordinateOrTakeOver = "coordinate_or_take_over"
        /// `POST /v1/orchestrator/tasks/:id/landing` with `landed` or `abandoned`. Something in
        /// this delivery wrote, so a person decides which.
        case landOrAbandon = "land_or_abandon"
        /// `POST /v1/orchestrator/tasks/:id/landing` with `nothing_to_land`, and it is only ever
        /// written here when ``Orchestrator/nothingToLandAdmission(for:declaredWritePaths:)`` — the
        /// same predicate that route admits by — says the route would take it.
        case nothingToLand = "nothing_to_land"
        /// Named, never done. See the note on this type.
        case dispose
    }

    /// Why a checkout or a branch is in `droppable`. Each is a git fact plus a stored fact, never
    /// an inference, and the fail-safe direction is the one
    /// ``Orchestrator/worktreeDisposal(commits:dirty:headOnBranch:branchExists:)`` already takes:
    /// **a missing fact is not permission**. An unknown dirty flag, a repository git could not be
    /// asked about, or a branch this side cannot see the shape of keeps the row out of here.
    enum Droppable: String {
        /// The branch carries commits of its own and this repository's `HEAD` already contains
        /// them, and the checkout has no uncommitted changes. The delivery is in the tree.
        case mergedAndClean = "merged_and_clean"
        /// The branch is contained by `HEAD` and still points at the commit it was cut from, so
        /// nothing was ever committed on it. `git worktree add -b` makes such a branch an
        /// ancestor of `HEAD` the moment it exists; 12 of this Mac's 75 merged delivery branches
        /// on 2026-09-06 were this and not a landing.
        case branchEmpty = "branch_empty"
        /// The task is over, git cannot find its delivery branch at all, and the checkout is
        /// still on disk. There is no delivery left to lose and a directory left to reclaim.
        case checkoutOrphaned = "checkout_orphaned"
    }

    // MARK: - One row

    /// **One row, with the fields the digest reads kept apart from the fields it must not.**
    ///
    /// That separation is the whole reason this is a type rather than a dictionary. `generation`
    /// has to be stable while nothing meaningful changed — a digest that ate `age_seconds` would
    /// make every dispatch race a clock and turn a mechanism into a nuisance — so the sealed
    /// half is written down here, once, and `payload` is free to carry anything a reader wants.
    struct Row {
        // Sealed.
        let section: Section
        /// The task this row is about. A row that no task owns has never occurred in this
        /// version — see the boundary note on ``rows(repository:tasks:branches:retainedPaths:now:)``
        /// — and the field is a `String` so that adding one later does not reshape the digest.
        let id: String
        /// The delivery branch, or `""` where the row has none. Empty rather than optional for
        /// the same reason: one canonical spelling per line.
        let branch: String
        let action: Action
        /// The verdict's own word: a ``Droppable`` reason, or `""` where the section is the
        /// reason. Sealed, because it is what changed when a row changes meaning without moving.
        let why: String
        /// The paths this row will write, repository-relative. Sealed for `live` rows because a
        /// caller about to dispatch is deciding against exactly this list.
        let paths: [String]

        // Not sealed. Everything a clock, a commit or a rename moves.
        let payload: [String: Any]
    }

    // MARK: - Deriving the three sections

    /// Every row for one repository, from facts that are all already published somewhere else.
    ///
    /// Pure on purpose, the way ``OrchestratorLandingQueue/members(tasks:repository:branches:retainedPaths:deliveryPaths:now:)``
    /// is: the two things that need a subprocess or a store arrive as parameters, so the whole
    /// shape can be exercised against a described repository rather than a real one.
    ///
    /// **`live` and `unlanded` are exactly `GET /v1/orchestrator/inflight` split in two.** They
    /// read the same ``Orchestrator/workVisibility(state:landing:isolated:branchExists:branchMerged:)``,
    /// which means a row cannot be in this answer and missing from that one; a second projection
    /// of the same records that could disagree with the first is the failure this repository
    /// already had three screens of.
    ///
    /// **A boundary, said out loud.** Only tasks the registry still holds produce rows. A
    /// checkout whose task record the newest-200 sweep has evicted is invisible here, and so is a
    /// delivery branch nobody's record owns — `Orchestrator.cleanupOrphanWorktrees` owns that
    /// case, with the same fail-safe rules and a modification-time floor this read has no honest
    /// way to apply. What that costs is stated rather than hidden: this answer is complete for
    /// work the registry remembers and silent about work it has forgotten.
    static func rows(repository: String, tasks: [Orchestrator.Task],
                     branches: Orchestrator.RepositoryBranches,
                     retainedPaths: [String: [String]], now: Date) -> [Row] {
        let prefix = repository.hasSuffix("/") ? repository : repository + "/"
        var out: [Row] = []
        for task in tasks.sorted(by: { $0.id < $1.id }) {
            let home = task.worktree?.repository ?? task.projectDir
            guard home == repository || home.hasPrefix(prefix) else { continue }
            let branch = task.worktree?.branch
            let exists = branch.flatMap { branches.known ? branches.heads[$0] != nil : nil }
            let merged = branch.flatMap { branches.known ? branches.merged.contains($0) : nil }
            let visibility = Orchestrator.workVisibility(
                state: task.state, landing: task.landing, isolated: task.worktree != nil,
                branchExists: exists, branchMerged: merged)
            let paths = OrchestratorLandingQueue.landingPaths(of: task,
                                                              retainedPaths: retainedPaths)
            switch visibility {
            case .live:
                out.append(liveRow(task, paths: paths, now: now))
            case .unmerged:
                out.append(unlandedRow(task, paths: paths, now: now))
            case .settled:
                if let row = droppableRow(task, exists: exists, merged: merged, now: now) {
                    out.append(row)
                }
            }
        }
        return out
    }

    private static func liveRow(_ task: Orchestrator.Task, paths: [String], now: Date) -> Row {
        var payload: [String: Any] = [
            "task": task.id,
            "title": task.title,
            "state": task.state.rawValue,
            "assistant": task.assistant.rawValue,
            "claims": paths,
            "age_seconds": OrchestratorDraft.ageSeconds(since: task.created, now: now),
            "do": Action.coordinateOrTakeOver.rawValue,
        ]
        payload["root_label"] = task.rootLabel as Any? ?? NSNull()
        payload["root_key"] = task.rootSessionId.map(OrchestratorDraft.rootKeyDigest)
            as Any? ?? NSNull()
        payload["branch"] = task.worktree?.branch as Any? ?? NSNull()
        return Row(section: .live, id: task.id, branch: task.worktree?.branch ?? "",
                   action: .coordinateOrTakeOver, why: "", paths: paths, payload: payload)
    }

    /// **The landing action this row's own route would admit**, asked with the route's predicate
    /// rather than with a second copy of it. `nothing_to_land` appears only where
    /// ``Orchestrator/nothingToLandAdmission(for:declaredWritePaths:)`` admits it; everywhere else
    /// something wrote, and a person decides between `landed` and `abandoned`.
    private static func unlandedRow(_ task: Orchestrator.Task, paths: [String],
                                    now: Date) -> Row {
        let admission = Orchestrator.nothingToLandAdmission(for: task, declaredWritePaths: paths)
        let action: Action = admission.isAdmitted ? .nothingToLand : .landOrAbandon
        var why = ""
        if case .refused(let reason) = admission { why = reason }
        var payload: [String: Any] = [
            "task": task.id,
            "title": task.title,
            "state": task.state.rawValue,
            "claims": paths,
            "age_seconds": OrchestratorDraft.ageSeconds(since: task.created, now: now),
            "do": action.rawValue,
        ]
        payload["branch"] = task.worktree?.branch as Any? ?? NSNull()
        payload["head"] = task.worktree?.head as Any? ?? NSNull()
        payload["landing"] = task.landing?.state.rawValue as Any? ?? NSNull()
        // Only present when the answer is `land_or_abandon`, and it is the stored fact that made
        // it so — the reader is told why the cheaper close is not on offer.
        payload["why"] = why.isEmpty ? NSNull() : why
        return Row(section: .unlanded, id: task.id, branch: task.worktree?.branch ?? "",
                   action: action, why: why, paths: paths, payload: payload)
    }

    /// A settled task whose checkout or branch nothing needs any more, or nil where something
    /// still might.
    ///
    /// **Three refusals before any verdict, and each one is a cost that is not symmetric.** A
    /// checkout kept by mistake costs a directory; a branch named as disposable and then deleted
    /// costs the work on it, and this app has no copy.
    ///
    /// 1. **git must have answered.** With `branches.known == false` every branch fact is unknown
    ///    and nothing is droppable, the same direction
    ///    ``Orchestrator/workVisibility(state:landing:isolated:branchExists:branchMerged:)`` takes
    ///    with visibility.
    /// 2. **The checkout must be known-clean.** `dirty == nil` is not permission; it is this Mac
    ///    having no reading.
    /// 3. **An unmerged branch is never droppable, whatever anybody recorded.** A root's
    ///    `landing: landed` settles the *obligation*, and a settled obligation is why this task
    ///    reached here at all — but `branch_unmerged` says this repository's `HEAD` does not
    ///    contain those commits, and a record cannot make that false. Same for `abandoned`: a
    ///    given-up obligation on an unmerged branch is debris worth looking at and is not
    ///    something to name as safe to remove.
    private static func droppableRow(_ task: Orchestrator.Task, exists: Bool?, merged: Bool?,
                                     now: Date) -> Row? {
        guard let worktree = task.worktree, let exists, worktree.dirty == false else { return nil }
        let onDisk = FileManager.default.fileExists(atPath: worktree.path)
        let why: Droppable
        if !exists {
            guard onDisk else { return nil }
            why = .checkoutOrphaned
        } else if merged == true {
            let head = worktree.head
            why = (head != nil && head == worktree.base) ? .branchEmpty : .mergedAndClean
        } else {
            return nil
        }
        let payload: [String: Any] = [
            "task": task.id,
            "title": task.title,
            "branch": worktree.branch,
            "path": onDisk ? worktree.path : NSNull(),
            "branch_exists": exists,
            "why": why.rawValue,
            "age_seconds": OrchestratorDraft.ageSeconds(since: task.created, now: now),
            "do": Action.dispose.rawValue,
        ]
        return Row(section: .droppable, id: task.id, branch: worktree.branch,
                   action: .dispose, why: why.rawValue, paths: [], payload: payload)
    }

    // MARK: - The receipt

    /// **What is in the digest, and what is deliberately out.**
    ///
    /// In: the schema version, the repository, and for every row its section, its task, its
    /// branch, its `why` and its `do`, plus the write set of a `live` row. That is the identity
    /// of each line of work and the verdict about it — which is precisely the set of facts a
    /// caller who "looked at what is already here" would have had to read.
    ///
    /// Out, and each for a reason:
    ///
    /// * **`age_seconds`, `created`, `since` and `at`.** A clock in the digest means the receipt
    ///   expires while the caller is composing the request it belongs to, and the whole mechanism
    ///   becomes a retry loop. This is the exclusion the brief for this feature named first.
    /// * **A task's `state`.** `queued` -> `spawning` -> `briefed` -> `working` changes nothing a
    ///   dispatcher decides. What *does* change a decision is the row moving between sections,
    ///   and the section is sealed, so a task finishing still moves the generation.
    /// * **`head`, `dirty` and commit counts.** A child committing does not change whether
    ///   somebody else should dispatch onto those paths. Where these facts do change the answer —
    ///   an empty branch, a clean checkout — they have already decided a `why`, and the `why` is
    ///   sealed.
    /// * **`title` and `root_label`.** Prose somebody may rewrite; not identity.
    /// * **`overlaps`.** It is the caller's question, not the repository's state: two callers
    ///   asking with different `claims` must get the same generation or no receipt is
    ///   transferable between them.
    ///
    /// Sixteen hex characters, from SHA-256 over a canonical, sorted rendering. Long enough that
    /// nobody arrives at one by accident, short enough to type into a `-d` body — this is a
    /// receipt saying a read happened, not a credential.
    static func generation(repository: String, rows: [Row]) -> String {
        var lines = ["clawdline-inventory-v\(schemaVersion)", "repository=\(repository)"]
        lines += rows.map { row in
            [row.section.rawValue, row.id, row.branch, row.why, row.action.rawValue,
             row.paths.sorted().joined(separator: ",")].joined(separator: "\t")
        }.sorted()
        let digest = SHA256.hash(data: Data(lines.joined(separator: "\n").utf8))
        return String(digest.map { String(format: "%02x", $0) }.joined().prefix(16))
    }

    // MARK: - The answer

    /// The whole payload, generation included. Separate from ``reply(project:claims:now:)`` so the
    /// refusal below can hand back the identical bytes a `GET` would have answered.
    static func payload(repository: String, rows: [Row], claims: [String],
                        now: Date) -> [String: Any] {
        let wanted = Set(claims.filter { !$0.isEmpty })
        func section(_ which: Section) -> [[String: Any]] {
            rows.filter { $0.section == which }.map { row -> [String: Any] in
                var out = row.payload
                // The caller-relative half, and the one field in this payload that depends on who
                // is asking. Absent when nobody asked, rather than an empty array, because an
                // empty overlap and no question are different answers.
                if !wanted.isEmpty, which == .live {
                    out["overlaps"] = row.paths.filter(wanted.contains).sorted()
                }
                return out
            }
        }
        return [
            "schema_version": schemaVersion,
            "repository": repository,
            "generation": generation(repository: repository, rows: rows),
            "live": section(.live),
            "unlanded": section(.unlanded),
            "droppable": section(.droppable),
            // The digest's own contract, published beside the number it produces, so a caller
            // reading this does not have to read this file to know what will move it.
            "digest": ["sealed": ["section", "task", "branch", "why", "do", "claims"],
                       "excluded": ["age_seconds", "created", "state", "head", "dirty",
                                    "title", "root_label", "overlaps", "at"]],
            "at": Int(now.timeIntervalSince1970),
        ]
    }

    /// Everything the read needs from this Mac, in one place, so that both routes below and every
    /// test take the same path to it.
    static func inventory(project: String, claims: [String] = [], now: Date = Date())
        -> (repository: String, rows: [Row], payload: [String: Any])? {
        guard !project.isEmpty, project.hasPrefix("/"),
              let repository = Orchestrator.inflightRepository(project) else { return nil }
        let branches = Orchestrator.repositoryBranches(in: repository)
        Orchestrator.load()
        Orchestrator.lock.lock()
        let held = Array(Orchestrator.tasks.values)
        Orchestrator.lock.unlock()
        // Read outside the registry lock: the landing queue keeps its own, and taking one inside
        // the other is the shape that produced this repository's `exit 133`.
        let retained = OrchestratorLandingQueue.retainedLandingPaths()
        let derived = rows(repository: repository, tasks: held, branches: branches,
                           retainedPaths: retained, now: now)
        return (repository, derived,
                payload(repository: repository, rows: derived, claims: claims, now: now))
    }

    /// `GET /v1/orchestrator/inventory?project=<dir>&claims=<a,b,c>`.
    ///
    /// `project` is any directory and the repository containing it is resolved on this side,
    /// exactly as `inflight` and `landing-queue` do. `claims` is optional and is the caller's own
    /// write set: where it is given, every `live` row also carries the intersection.
    static func reply(project: String, claims: [String] = [],
                      now: Date = Date()) -> Orchestrator.Reply {
        guard let answer = inventory(project: project, claims: claims, now: now) else {
            return .refused(400, "bad_request",
                            "project must be an absolute path inside a Git repository.")
        }
        return .ok(answer.payload)
    }

    // MARK: - The door

    /// **The refusal that makes the read above compulsory**, or nil where the dispatch may go on.
    ///
    /// `POST /v1/orchestrator/tasks` and `POST /v1/orchestrator/detached-tasks` carry
    /// `inventory_generation`; missing or not equal to the current digest is `409 stale_inventory`
    /// with the whole inventory in the error body, so the caller who did not look is handed
    /// exactly what they did not look at and needs no second request to find it.
    ///
    /// **`POST /v1/orchestrator/handoffs` and `POST /v1/orchestrator/root-assignments` do not call
    /// this, and the asymmetry is the point rather than an omission.** A handoff continues a line
    /// of work whose subject is already named — the sender's REFERENCES, VERIFICATION and OPEN
    /// THREADS travel with it, so the receiver is not choosing what to work on and cannot collide
    /// with a line it has not been told about. A Root Assignment opens an ordinary independent
    /// Root, and that Root runs this read itself before *it* dispatches; requiring a receipt from
    /// the launching session would seal the launcher's view of a repository the new Root has not
    /// started work in yet. Both would be a receipt about the wrong moment.
    ///
    /// Three cases pass through with no opinion, each because refusing would be worse than
    /// allowing:
    ///
    /// * **A task the registry already holds.** Resending the same `task_id` is documented as
    ///   idempotent and is how a caller recovers from a timeout; a retry must not become a
    ///   refusal because the answer moved between the first send and the second.
    /// * **A task file this Mac cannot read or parse.** ``Orchestrator/dispatch(taskID:secret:schedule:requireRootSession:allowDetachedAutomation:respawn:)``
    ///   answers `422 bad_task` for those, with the reason; a `409` here would replace a precise
    ///   complaint with a vague one.
    /// * **A project directory that is in no Git repository.** There is no inventory to read, so
    ///   there is no receipt anybody could produce, and requiring one would make such a task
    ///   undispatchable rather than careful.
    static func dispatchAdmission(taskID: String, supplied: Any?,
                                  now: Date = Date()) -> Orchestrator.Reply? {
        guard OrchestratorDraft.isTaskID(taskID), Orchestrator.held(taskID) == nil,
              let project = projectDirectory(ofTask: taskID),
              let answer = inventory(project: project, now: now) else { return nil }
        let current = generation(repository: answer.repository, rows: answer.rows)
        if let offered = supplied as? String, offered == current { return nil }
        let what = supplied == nil ? "carried no inventory_generation"
            : "carried an inventory_generation this repository has moved past"
        return .refused(
            status: 409, code: "stale_inventory",
            message: "This dispatch \(what). GET /v1/orchestrator/inventory?project="
                + "\(answer.repository) answers \(current); the whole of it is in this error, so "
                + "read it and resend with that value.",
            extra: ["inventory_generation": current, "inventory": answer.payload])
    }

    /// The project directory a task was written for, read from the same `task.json` dispatch is
    /// about to read. Deliberately not the parsed draft: this needs one field, before any of the
    /// validation whose refusals belong to `dispatch`.
    private static func projectDirectory(ofTask taskID: String) -> String? {
        let file = Orchestrator.root.appendingPathComponent(taskID, isDirectory: true)
            .appendingPathComponent("task.json")
        guard let data = try? Data(contentsOf: file),
              let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              let dir = obj["project_dir"] as? String, dir.hasPrefix("/") else { return nil }
        return dir
    }

    /// `claims=a,b,c` on the read above. Split here rather than in the router so the one place
    /// that decides what an empty value means is beside the code that consumes it.
    static func parseClaims(_ raw: String?) -> [String] {
        (raw ?? "").split(separator: ",").map {
            $0.trimmingCharacters(in: .whitespacesAndNewlines)
        }.filter { !$0.isEmpty }
    }
}
