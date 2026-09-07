import Foundation

// **Whether the landing a surface shows is the one that is true now**, and the state a delivery
// that wrote nothing has.
//
// The defect these two groups were written for is one defect. `usage_intervals.landing_state` is
// a point-in-time copy taken when a task reaches a terminal state, and a landing closes *after*
// the work ends — so the field is almost always absent at sampling time, and the only thing that
// ever filled it in was the backfill import on launch. Measured over this Mac's own store at
// 11:06 on 2026-09-05: of the tasks whose landing had closed before that launch, 79 of 79 carried
// the copy; of the 6 closed after it, 0 did. Two of those were sitting in the Projects page's
// 「做完了，沒有落地」 block while the broker held a verified `landed` for each — and when the app
// restarted at 11:16 with no landing record changing, both read `landed` and the count fell by two.
//
// They live in a file of their own because `Tests/UsageLedgerTests.swift` and
// `Tests/OrchestratorLandingTests.swift` are both within a hundred lines of the 2,000-line
// stop-growth limit, and a test trimmed to fit under a wall leaves the next person standing at
// it.


// **The lock that only ever tightened.** `landed` had one entrance and a person standing in it, so
// the closeability projection could add `pending_landing_owned` and never take one away — twelve
// of them from one root, on one card, on 2026-09-06. These two groups are the broker closing what
// it can prove by itself, and the second is the larger half: every case where it must not.
//
// The fixture is a real repository, not a fake git. The whole question this feature answers is
// what `git rev-list`, `git status` and `git diff` say about a checkout, and a stub that answers
// for them would be a test of the stub.

func makeSweepRepository() -> (url: URL, base: String, delivery: String, main: String) {
    let url = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-sweep-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(
        at: url.appendingPathComponent("Sources", isDirectory: true),
        withIntermediateDirectories: true)
    func commit(_ message: String) {
        expect("the sweep fixture stages \(message)", testGit(["add", "-A"], cwd: url).status, 0)
        expect("the sweep fixture commits \(message)", testGit([
            "-c", "user.name=Clawdline Tests", "-c", "user.email=tests@clawdline.invalid",
            "commit", "-qm", message,
        ], cwd: url).status, 0)
    }
    func write(_ relative: String, _ text: String) {
        try! Data(text.utf8).write(to: url.appendingPathComponent(relative))
    }
    try! FileManager.default.createDirectory(
        at: url.appendingPathComponent("docs", isDirectory: true),
        withIntermediateDirectories: true)
    expect("the sweep fixture repository initializes",
           testGit(["init", "-q", "-b", "main"], cwd: url).status, 0)
    write("Sources/Kept.swift", "kept\n")
    write("Sources/Dirty.swift", "dirty\n")
    write("Sources/Differs.swift", "one\n")
    // Only in the target. The checkout deletes it below, so it is the one claim that resolves
    // through `ls-tree` rather than through `ls-files` — the second of F1's two paths, and the
    // reason a landed deletion is not read as a claim that names nothing.
    write("Sources/Removed.swift", "removed\n")
    // A directory claim, clean and identical to the target on both sides, so that the arm-2 path
    // is exercised for a claim that is not a file. `landingSweepClaimsUsable` accepted one from
    // the first day; nothing had ever run a pass with one.
    write("docs/kept.md", "kept\n")
    commit("the target's own history")
    let base = testGit(["rev-parse", "HEAD"], cwd: url).output

    // A delivery branch with a commit of its own, merged into the target. This is arm 1's whole
    // question: `refs/heads/main` contains that head.
    let deliveryBranch = "clawdline/task/\(sweepAncestryTask)"
    expect("the sweep fixture opens a delivery branch",
           testGit(["switch", "-q", "-c", deliveryBranch], cwd: url).status, 0)
    write("Sources/Delivered.swift", "delivered\n")
    commit("the delivery")
    let delivery = testGit(["rev-parse", "HEAD"], cwd: url).output
    expect("the sweep fixture returns to the target",
           testGit(["switch", "-q", "main"], cwd: url).status, 0)
    expect("the sweep fixture merges the delivery", testGit([
        "-c", "user.name=Clawdline Tests", "-c", "user.email=tests@clawdline.invalid",
        "merge", "-q", "--no-ff", "-m", "land the delivery", deliveryBranch,
    ], cwd: url).status, 0)
    let mainTip = testGit(["rev-parse", "HEAD"], cwd: url).output

    // The checkout the shared-tree tasks are judged in sits on a branch of its own, so that one
    // claimed path can be committed-and-clean while still differing from the target — which is a
    // different finding from a path somebody is still editing, and the sweep keeps them apart.
    expect("the sweep fixture opens the checkout's own branch",
           testGit(["switch", "-q", "-c", "side"], cwd: url).status, 0)
    write("Sources/Differs.swift", "two\n")
    // **`Sources/Dirty.swift` is committed here to something else, and put back below.**
    //
    // It used to be committed as the target's own `dirty\n` and then edited to `somebody is
    // still typing\n`, which made it *both* uncommitted and different from the target — so no
    // test in this file could tell `outstanding` and `differing` apart, and the mutation that
    // deleted the `outstanding.isEmpty` conjunct from `isSettled` survived the whole suite. The
    // shape that needs `outstanding` on its own is a path somebody is halfway through saving back
    // to exactly what the target has, which is what this is.
    write("Sources/Dirty.swift", "somebody is still typing\n")
    expect("the sweep fixture removes a path the target still carries",
           testGit(["rm", "-q", "Sources/Removed.swift"], cwd: url).status, 0)
    commit("a claimed path that never reached the target")
    write("Sources/Dirty.swift", "dirty\n")
    return (url, base, delivery, mainTip)
}

let sweepAncestryTask = "31313131-4141-5151-6161-717171717171"
let sweepWriteSetTask = "31313131-4141-5151-6161-717171717172"
let sweepDirtyTask = "31313131-4141-5151-6161-717171717173"
let sweepDifferingTask = "31313131-4141-5151-6161-717171717174"
let sweepUnreadableTask = "31313131-4141-5151-6161-717171717175"
let sweepSettledTask = "31313131-4141-5151-6161-717171717176"
let sweepLiveTask = "31313131-4141-5151-6161-717171717177"
let sweepUntargetedTask = "31313131-4141-5151-6161-717171717178"
let sweepDirectoryTask = "31313131-4141-5151-6161-717171717179"
let sweepMissingClaimTask = "31313131-4141-5151-6161-71717171717a"
let sweepDeletedClaimTask = "31313131-4141-5151-6161-71717171717b"
let sweepWarningClaimTask = "31313131-4141-5151-6161-71717171717c"
let sweepDirtyEmptyTask = "31313131-4141-5151-6161-71717171717d"
let sweepUnreadableRefsTask = "31313131-4141-5151-6161-71717171717e"
let sweepDeletedBranchTask = "31313131-4141-5151-6161-71717171717f"
let sweepUnreadableWriteSetTask = "41414141-5151-6161-7171-818181818181"

func runLandingCurrencyTests() {

group("a read-only delivery gets a terminal state, and one that wrote may not use it") {
    let repository = makeLandingRepository()
    defer { try? FileManager.default.removeItem(at: repository.url) }
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    let secret = String(repeating: "b2", count: 32)
    // Held terminal rather than finalized: `finalize` would reclaim a checkout and write a usage
    // row, and neither is what these assertions are about.
    func hold(_ id: String, state: Orchestrator.State = .success, claims: [String] = [],
              worktree: Orchestrator.Worktree? = nil) {
        var task = Orchestrator.Task(
            id: id, state: state, kind: "review", title: "an audit that wrote nothing",
            assistant: .claude, projectDir: repository.url.path, timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: 1), rootSessionId: "landing-currency-root",
            claims: claims, claimsDeclared: !claims.isEmpty, worktree: worktree,
            secretHash: Orchestrator.hash(ofSecret: secret))
        task.claimKeys = OrchestratorDraft.freezeClaims(claims, projectDir: task.projectDir)
        Orchestrator.holdScheduleTaskForTesting(task)
    }
    func checkout(commits: Int?, dirty: Bool?) -> Orchestrator.Worktree {
        var worktree = Orchestrator.Worktree(
            path: repository.url.appendingPathComponent("audit").path,
            branch: "clawdline/task/audit", base: "HEAD",
            repository: repository.url.path,
            cwd: repository.url.appendingPathComponent("audit").path)
        worktree.commits = commits
        worktree.dirty = dirty
        return worktree
    }
    func settle(_ id: String, raw: [String: Any], asMachine: Bool = true) -> Orchestrator.Reply {
        Orchestrator.updateLanding(
            taskID: id, secret: asMachine ? "" : secret,
            orchestratorToken: asMachine ? Orchestrator.dispatchToken() : nil,
            raw: raw, now: Date(timeIntervalSince1970: 50))
    }
    func refusal(_ reply: Orchestrator.Reply) -> (status: Int, code: String)? {
        if case .refused(let status, let code, _, _) = reply { return (status, code) }
        return nil
    }
    func landingState(_ reply: Orchestrator.Reply) -> String? {
        guard case .ok(let body) = reply, let task = body["task"] as? [String: Any],
              let landing = task["landing"] as? [String: Any] else { return nil }
        return landing["state"] as? String
    }

    // The five children this state was added for: `claims: []`, no checkout, an artifact that
    // shipped under somebody else's commit, and no landing record that could say so.
    let audit = "20202020-3030-4040-5050-606060606060"
    hold(audit, state: .briefed)
    expect("a live task cannot settle its obligation either",
           refusal(settle(audit, raw: ["state": "nothing_to_land"]))?.code, "not_terminal")
    hold(audit)
    expect("the child's own secret may not assert what a repository holds",
           refusal(settle(audit, raw: ["state": "nothing_to_land"], asMachine: false))?.status, 403)
    expect("a branch to land on is the thing this state says did not exist",
           refusal(settle(audit, raw: ["state": "nothing_to_land", "target": "main"]))?.code,
           "bad_request")
    expect("and a commit is refused for the same reason",
           refusal(settle(audit, raw: ["state": "nothing_to_land",
                                       "commit": repository.commit]))?.code, "bad_request")
    expect("a read-only delivery settles",
           landingState(settle(audit, raw: ["state": "nothing_to_land",
                                            "note": "artifacts/inventory.md, 883 lines"])),
           "nothing_to_land")
    expect("and is as final as the other two",
           refusal(settle(audit, raw: ["state": "landed", "target": "main",
                                       "commit": repository.commit]))?.code, "invalid_transition")
    expect("in both directions", refusal(settle(audit, raw: ["state": "pending"]))?.code,
           "invalid_transition")

    // The evidence gate. Every refusal below is a stored fact, and an unknown count is not one of
    // them: a checkout this Mac never counted may hold anything.
    let claimed = "20202020-3030-4040-5050-606060606061"
    hold(claimed, claims: ["Sources/Wrote.swift"])
    expect("a task that claimed a path to write may not say it wrote nothing",
           refusal(settle(claimed, raw: ["state": "nothing_to_land"]))?.code, "wrote_to_repository")
    let committed = "20202020-3030-4040-5050-606060606062"
    hold(committed, worktree: checkout(commits: 3, dirty: false))
    expect("nor one whose branch carries commits",
           refusal(settle(committed, raw: ["state": "nothing_to_land"]))?.status, 409)
    let uncounted = "20202020-3030-4040-5050-606060606063"
    hold(uncounted, worktree: checkout(commits: nil, dirty: nil))
    expect("and an uncounted checkout is a refusal, because unknown is not permission",
           refusal(settle(uncounted, raw: ["state": "nothing_to_land"]))?.code,
           "wrote_to_repository")

    // **The list an isolated task declared is not in `claims`.** The broker drops that lease and
    // hands the paths back as `claims_ignored_for_worktree`, so a worktree child that declared
    // nine paths is persisted as declared-and-empty — the one spelling that positively means
    // "writes nothing". Reading `claims` here would have admitted every one of them.
    let isolated = "20202020-3030-4040-5050-606060606066"
    var isolatedTask = Orchestrator.Task(
        id: isolated, state: .success, kind: "custom", title: "a worktree child that declared",
        assistant: .claude, projectDir: repository.url.path, timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 1), claims: ["Sources/Isolated.swift"],
        claimsDeclared: true, worktree: checkout(commits: 0, dirty: false),
        secretHash: Orchestrator.hash(ofSecret: secret))
    isolatedTask.isolation = .worktree
    OrchestratorLandingQueue.retainLandingPaths(&isolatedTask)
    Orchestrator.holdScheduleTaskForTesting(isolatedTask)
    check("the broker really does store it declared-and-empty",
          isolatedTask.claims.isEmpty && isolatedTask.claimsDeclared)
    expect("and its declared write set still refuses the state, from where landing kept it",
           refusal(settle(isolated, raw: ["state": "nothing_to_land"]))?.code,
           "wrote_to_repository")

    // The predicate itself, which the Projects read consults so that a row can never advise a
    // close this route would refuse.
    var named = Orchestrator.Task(
        id: "20202020-3030-4040-5050-606060606064", state: .success, kind: "review",
        title: "a delivery a root already named a branch for", assistant: .claude,
        projectDir: repository.url.path, timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 1), secretHash: "")
    named.landing = Orchestrator.Landing(
        state: .pending, target: "main", delivery: nil, ownerRootKey: "0123abcd",
        since: Date(timeIntervalSince1970: 2), commit: nil, note: nil)
    check("an obligation whose target is already named contradicts having nothing to land",
          !Orchestrator.nothingToLandAdmission(for: named, declaredWritePaths: []).isAdmitted)
    check("a dirty checkout is evidence too",
          !Orchestrator.nothingToLandAdmission(for: Orchestrator.Task(
            id: "20202020-3030-4040-5050-606060606065", state: .success, kind: "review",
            title: "dirty", assistant: .claude, projectDir: repository.url.path,
            timeoutMinutes: 30, created: Date(timeIntervalSince1970: 1),
            worktree: checkout(commits: 0, dirty: true), secretHash: ""),
            declaredWritePaths: []).isAdmitted)
}

group("a worktree's landing state is the one that is true now, and its row says what the work was") {
    let at = ISO8601DateFormatter().date(from: "2026-09-05T09:30:00Z")!
    let closedSinceLaunch = "6769836c-94f6-431b-ae10-ec4bd1ac4034"
    let swept = "b8954b67-3039-42fd-837a-28be6d2f8655"
    let readOnly = "3dc4cce4-1111-4222-8333-444444444444"
    let wrote = "01f57a72-5555-4666-8777-888888888888"
    let rows = [
        // The shape this whole slice exists for: the row was collected when the task ended, the
        // landing closed six hours later, and nothing has restarted the app since.
        worktreeRow("closed-since-launch", at: at, worktree: closedSinceLaunch,
                    task: closedSinceLaunch),
        // The control arm: its landing closed before the last launch, so the backfill copied the
        // word onto the row — and the registry has since swept the task.
        worktreeRow("swept", at: at, worktree: swept, task: swept, landing: "landed"),
        worktreeRow("read-only", at: at, worktree: readOnly, task: readOnly),
        worktreeRow("wrote", at: at, worktree: wrote, task: wrote),
    ]
    let features = ["closed-since-launch": acceptedFeature("f1", "Clawdfather — handoff 18bde7c3"),
                    "swept": acceptedFeature("f2", "Clawdfather"),
                    "read-only": acceptedFeature("f3", "Clawdfather — handoff 18bde7c3"),
                    "wrote": acceptedFeature("f4", "Clawdfather — handoff 18bde7c3")]
    let records = [
        closedSinceLaunch: UsageLedger.LiveTaskRecord(
            landingState: "landed", title: "一輪 correction：handoff sender contract 的八個 finding"),
        readOnly: UsageLedger.LiveTaskRecord(
            title: "How expensive is true streaming?", nothingToLand: true),
        wrote: UsageLedger.LiveTaskRecord(title: "The landing queue", nothingToLand: false),
    ]
    let payload = UsageProjectWorktreeService(
        rows: { rows }, acceptedFeatures: { features }, liveTaskRecords: { records })
        .read(.init(project: "widget", timezoneID: "UTC"), now: at).payload ?? [:]
    let worktrees = payload["worktrees"] as? [[String: Any]] ?? []
    func row(_ id: String) -> [String: Any] {
        worktrees.first { $0["id"] as? String == id } ?? [:]
    }

    let current = row(closedSinceLaunch)
    // Red on this tree before the join landed: `delivered`, with `landingStates: []`, while the
    // broker held `landing.state = landed` verified against `refs/heads/main`.
    expect("a landing closed since the last launch reaches the read without a restart",
           current["outcome"] as? String, "landed")
    expect("through the state that is true now", current["landingStates"] as? [String], ["landed"])
    expect("while the row keeps the nothing it froze",
           current["storedLandingStates"] as? [String], [])
    expect("and the answer says which of the two it rests on",
           current["landingBasis"] as? String, "live")
    expect("a row whose task the registry has swept still answers from its own copy",
           row(swept)["landingBasis"] as? String, "stored")
    expect("and that copy is still evidence", row(swept)["outcome"] as? String, "landed")

    expect("the card says what the work was, not only which root owned it",
           current["work"] as? String, "一輪 correction：handoff sender contract 的八個 finding")
    expect("under the label that names the work line",
           (current["features"] as? [[String: Any]])?.first?["label"] as? String,
           "Clawdfather — handoff 18bde7c3")
    expect("a swept task has no title anywhere, and empty is what that says",
           row(swept)["work"] as? String, nil)

    expect("a settled row needs nothing", current["needs"] as? String, nil)
    expect("a delivery this Mac has no record of writing can be closed as nothing to land",
           row(readOnly)["needs"] as? String, "nothing_to_land")
    expect("one that wrote is a person's decision", row(wrote)["needs"] as? String,
           "land_or_abandon")
    expect("and one whose task is gone has nothing left to close",
           row(swept)["needs"] as? String, nil)

    let settled = [worktreeRow("settled", at: at, worktree: readOnly, task: readOnly,
                               landing: "nothing_to_land")]
    let closed = UsageProjectWorktreeService(
        rows: { settled }, acceptedFeatures: { ["settled": acceptedFeature("f5", "Reviews")] })
        .read(.init(project: "widget", timezoneID: "UTC"), now: at).payload ?? [:]
    expect("a settled read-only delivery leaves the done-never-landed block",
           ((closed["worktrees"] as? [[String: Any]])?.first?["outcome"]) as? String,
           "nothing_to_land")
    check("and the ladder puts it beside landed rather than above delivered",
          UsageProjectWorktreeService.Outcome.landed.rank
            < UsageProjectWorktreeService.Outcome.nothingToLand.rank
            && UsageProjectWorktreeService.Outcome.nothingToLand.rank
                < UsageProjectWorktreeService.Outcome.delivered.rank)
}


group("the broker closes by ancestry what git already proves, and keeps the receipt real") {
    let repository = makeSweepRepository()
    defer { try? FileManager.default.removeItem(at: repository.url) }
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    let opened = Date(timeIntervalSince1970: 1_000)
    func pending(_ target: String) -> Orchestrator.Landing {
        Orchestrator.Landing(state: .pending, target: target, delivery: "the delivery",
                             ownerRootKey: "0123abcd", since: opened, commit: nil,
                             note: "awaiting a safe shared tree")
    }
    func hold(_ id: String, claims: [String] = [], target: String? = "main",
              state: Orchestrator.State = .success, isolated: Bool = false) {
        var task = Orchestrator.Task(
            id: id, state: state, kind: "custom", title: "a delivery nobody closed",
            assistant: .claude, projectDir: repository.url.path, timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: 1), rootSessionId: "sweep-root",
            claims: claims, claimsDeclared: !claims.isEmpty,
            secretHash: String(repeating: "0", count: 64))
        task.finishedAt = state.isTerminal ? Date(timeIntervalSince1970: 900) : nil
        task.resultVerifiedAt = task.finishedAt
        task.summary = state.isTerminal ? "delivered" : nil
        task.claimKeys = OrchestratorDraft.freezeClaims(claims, projectDir: task.projectDir)
        if isolated {
            task.isolation = .worktree
            task.worktree = Orchestrator.Worktree(
                path: OrchestratorDraft.worktreePath(project: repository.url.path, taskID: id)!,
                branch: OrchestratorDraft.worktreeBranch(for: id)!, base: repository.base,
                repository: repository.url.path, cwd: repository.url.path,
                head: repository.delivery, commits: 1, dirty: false)
        }
        task.landing = target.map(pending)
        Orchestrator.holdScheduleTaskForTesting(task)
    }
    func settled(_ id: String) -> Orchestrator.Landing? { Orchestrator.held(id)?.landing }

    hold(sweepAncestryTask, isolated: true)
    hold(sweepWriteSetTask, claims: ["Sources/Kept.swift"])
    hold(sweepDirectoryTask, claims: ["docs"])
    expect("the empty delivery branch exists at its base", testGit([
        "branch", "clawdline/task/\(sweepDirtyEmptyTask)", repository.base,
    ], cwd: repository.url).status, 0)
    var dirtyEmpty = Orchestrator.Task(
        id: sweepDirtyEmptyTask, state: .success, kind: "custom",
        title: "dirty bytes on a zero-commit delivery branch",
        assistant: .claude, projectDir: repository.url.path, timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 2), rootSessionId: "sweep-root",
        claims: [], claimsDeclared: true,
        secretHash: String(repeating: "0", count: 64))
    dirtyEmpty.finishedAt = Date(timeIntervalSince1970: 900)
    dirtyEmpty.resultVerifiedAt = dirtyEmpty.finishedAt
    dirtyEmpty.summary = "delivered as dirty worktree bytes"
    dirtyEmpty.isolation = .worktree
    dirtyEmpty.worktree = Orchestrator.Worktree(
        path: OrchestratorDraft.worktreePath(
            project: repository.url.path, taskID: sweepDirtyEmptyTask)!,
        branch: OrchestratorDraft.worktreeBranch(for: sweepDirtyEmptyTask)!,
        base: repository.base, repository: repository.url.path, cwd: repository.url.path,
        head: repository.base, commits: 0, dirty: true)
    dirtyEmpty.landing = pending("main")
    Orchestrator.holdScheduleTaskForTesting(dirtyEmpty)
    Orchestrator.forgetLandingSweepObservations()

    // **Two passes, because arm 2 waits and arm 1 does not.** What arm 2 proves is true at an
    // instant and the record it writes is permanent, so the same write set has to come back clear
    // twice, at least `landingSweepStabilityWindow` apart. Arm 1's proposition is git's own
    // containment and does not move, so it closes on the first pass.
    let opening = Orchestrator.landingSweepPass(now: Date(timeIntervalSince1970: 5_000))
    func windowReason(_ id: String) -> String? {
        if case .left(let reason) = opening.first(where: { $0.taskID == id })?.verdict {
            return reason
        }
        return nil
    }
    expect("the merged delivery closes on ancestry on the first pass",
           opening.first { $0.taskID == sweepAncestryTask }?.verdict, .closed(.ancestry))
    check("a dirty zero-commit worktree is not closed merely because main contains its base",
          Orchestrator.held(sweepDirtyEmptyTask)?.landing?.state == .pending)
    check("and the pass does not claim ancestry proved those dirty delivery bytes landed",
          opening.first { $0.taskID == sweepDirtyEmptyTask }?.verdict != .closed(.ancestry))

    // The pure admission matrix keeps each stored fact independently observable. These are also
    // the mutation witnesses: deleting any one gate below makes its named row choose ancestry.
    func isolatedQuestion(base: String = repository.base,
                          head: String? = repository.delivery,
                          commits: Int? = 1, dirty: Bool? = false,
                          deliveryRef: Orchestrator.LandingSweepDeliveryRefObservation = .absent,
                          claims: [String] = []) -> Orchestrator.LandingSweepQuestion {
        Orchestrator.landingSweepQuestion(
            for: Orchestrator.LandingSweepCandidate(
                taskID: sweepDirtyEmptyTask, projectDir: repository.url.path, target: "main",
                deliveryBranch: "clawdline/task/\(sweepDirtyEmptyTask)",
                deliveryEvidence: .isolated(
                    base: base, head: head, commits: commits, dirty: dirty),
                claims: claims),
            deliveryRef: deliveryRef)
    }
    func isUnanswerable(_ question: Orchestrator.LandingSweepQuestion) -> Bool {
        if case .unanswerable = question { return true }
        return false
    }
    expect("clean nonempty isolated evidence admits the actual delivery head",
           isolatedQuestion(), .ancestry(head: repository.delivery))
    expect("dirty nonempty isolated evidence is unanswerable",
           isolatedQuestion(dirty: true),
           .unanswerable("isolated delivery worktree still has dirty bytes"))
    expect("clean empty isolated evidence is unanswerable",
           isolatedQuestion(head: repository.base, commits: 0),
           .unanswerable("isolated delivery branch has no commits beyond its base"))
    check("dirty empty isolated evidence is unanswerable too",
          isUnanswerable(isolatedQuestion(
            head: repository.base, commits: 0, dirty: true)))
    expect("an unknown isolated commit count is not permission",
           isolatedQuestion(commits: nil),
           .unanswerable("isolated delivery commit count is unknown"))
    expect("an unknown isolated dirty state is not permission",
           isolatedQuestion(dirty: nil),
           .unanswerable("isolated delivery dirty state is unknown"))
    expect("a missing isolated base is not permission",
           isolatedQuestion(base: ""),
           .unanswerable("isolated delivery base is unknown"))
    expect("a missing isolated recorded head is not permission",
           isolatedQuestion(head: nil),
           .unanswerable("isolated delivery head is unknown"))
    expect("a clean nonempty receipt whose head is still base is contradictory",
           isolatedQuestion(head: repository.base),
           .unanswerable("isolated delivery head is still its base"))
    expect("a live head that disagrees with the receipt is contradictory",
           isolatedQuestion(deliveryRef: .present(repository.main)),
           .unanswerable("isolated delivery live head contradicts its recorded head"))
    expect("an isolated row with unusable evidence never falls back to its claims",
           isolatedQuestion(head: nil, claims: ["Sources/Kept.swift"]),
           .unanswerable("isolated delivery head is unknown"))
    check("a write set clear once is a window that has opened, not a record",
          windowReason(sweepWriteSetTask)?.contains("a second reading") == true)
    check("and nothing was written for it",
          Orchestrator.held(sweepWriteSetTask)?.landing?.state == .pending)

    // Too soon: the same clear answer, inside the window rather than after it.
    let early = Orchestrator.landingSweepPass(now: Date(timeIntervalSince1970: 5_100))
    if case .left(let reason) = early.first(where: { $0.taskID == sweepWriteSetTask })?.verdict {
        check("a second reading inside the window says how much of it is left",
              reason.contains("short of the"))
    } else {
        check("a second reading inside the window does not close the record", false)
    }
    check("and still nothing was written",
          Orchestrator.held(sweepWriteSetTask)?.landing?.state == .pending)

    let pass = Orchestrator.landingSweepPass(now: Date(timeIntervalSince1970: 5_400))
    func verdict(_ id: String) -> Orchestrator.LandingSweepVerdict? {
        pass.first { $0.taskID == id }?.verdict
    }
    expect("and the shared checkout's write set closes on its own proposition, once the window "
           + "has held", verdict(sweepWriteSetTask), .closed(.writeSet))
    expect("a claim naming a directory is a write set like any other",
           verdict(sweepDirectoryTask), .closed(.writeSet))

    // Arm 1: the same verified path the HTTP route takes, so the record is the two-check kind.
    let ancestry = settled(sweepAncestryTask)
    expect("the ancestry record is landed", ancestry?.state, .landed)
    expect("on the commit git resolved, not the one a caller typed",
           ancestry?.commit, repository.delivery)
    expect("with the broker's own verification origin",
           ancestry?.verificationOrigin, "local_target_branch")
    expect("naming the target commit it was contained by",
           ancestry?.verifiedTargetCommit, repository.main)
    check("and it reads as the two-check landing",
          ancestry.map(Orchestrator.isBrokerVerifiedTargetLanding) == true)
    check("which is what the Session vocabulary calls work_complete",
          Orchestrator.projectSessionWorkState(
            terminalState: .idle, task: Orchestrator.held(sweepAncestryTask),
            hasCoordinationWait: false, hasOpenHandoff: false,
            assignmentKnownAbsent: false) == .workComplete)
    check("and its note says which of the two propositions it rests on",
          ancestry?.note?.contains("reached the target") == true)

    // Arm 2: the narrower sentence, and it may never be dressed as the wider one.
    let writeSet = settled(sweepWriteSetTask)
    expect("the write-set record is landed too", writeSet?.state, .landed)
    expect("on the target's own head, because it has no commit of its own",
           writeSet?.commit, repository.main)
    check("carrying no verification field at all",
          writeSet?.verificationOrigin == nil && writeSet?.verifiedCommit == nil
              && writeSet?.verifiedTargetCommit == nil && writeSet?.landedAt == nil)
    check("so it can never become the two-check landing",
          writeSet.map(Orchestrator.isBrokerVerifiedTargetLanding) == false)
    check("and stays the one-check milestone in the Session vocabulary",
          Orchestrator.projectSessionWorkState(
            terminalState: .idle, task: Orchestrator.held(sweepWriteSetTask),
            hasCoordinationWait: false, hasOpenHandoff: false,
            assignmentKnownAbsent: false) == .milestoneComplete)
    check("its note says out loud that this is not the other proposition",
          writeSet?.note?.contains("not proof that a delivery reached the target") == true)
    check("and it keeps the obligation's own clock rather than inventing one",
          writeSet?.since == opened && writeSet?.ownerRootKey == "0123abcd")
    // The record is permanent and its proposition was not, so it says when it was true — both
    // times, in the past tense.
    check("its note names both instants the write set was read at",
          writeSet?.note?.contains(Orchestrator.landingSweepInstant(
            Date(timeIntervalSince1970: 5_000))) == true
              && writeSet?.note?.contains(Orchestrator.landingSweepInstant(
                Date(timeIntervalSince1970: 5_400))) == true)
    check("and it does not claim in the present tense what it measured in the past",
          writeSet?.note?.contains("were unmodified") == true)
    // The route has a person standing in it who can write the sentence again; nothing here can,
    // so the note the record was opened with travels with the closure instead of being lost.
    check("both arms keep the note the record was opened with",
          writeSet?.note?.contains("awaiting a safe shared tree") == true
              && ancestry?.note?.contains("awaiting a safe shared tree") == true)
    check("and neither note can grow past what the store will decode",
          [writeSet?.note, ancestry?.note].allSatisfy { ($0?.count ?? 0) <= 500 })

    // Both records have to survive the codec, which fails closed on any verification-shaped field
    // whose origin and triple are not exactly right.
    for (name, record) in [("the ancestry record", ancestry), ("the write-set record", writeSet)] {
        guard let record else { check("\(name) exists", false); continue }
        expect("\(name) round-trips through the store",
               OrchestratorStore.landing(from: OrchestratorStore.stored(record)), record)
    }

    // A settled record is never rewritten, and the cheapest proof is that a later pass does not
    // even consider one: the projection's obligations fall away with it.
    let second = Orchestrator.landingSweepPass(now: Date(timeIntervalSince1970: 6_000))
    check("a settled obligation is not a candidate for the next pass",
          !second.contains { $0.taskID == sweepAncestryTask || $0.taskID == sweepWriteSetTask })
    expect("and the record it wrote is still the one it wrote",
           settled(sweepAncestryTask), ancestry)
}

group("delivery-ref observation keeps absent separate from unreadable at the production git seam") {
    let repository = makeSweepRepository()
    defer { try? FileManager.default.removeItem(at: repository.url) }
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        Orchestrator.landingSweepDeliveryRefsGitOverrideForTesting = nil
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    let opened = Date(timeIntervalSince1970: 1_000)
    func hold(_ id: String, storedHead: String) {
        var task = Orchestrator.Task(
            id: id, state: .success, kind: "custom", title: "a clean committed delivery",
            assistant: .claude, projectDir: repository.url.path, timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: id == sweepUnreadableRefsTask ? 1 : 2),
            rootSessionId: "sweep-root", claims: [], claimsDeclared: true,
            secretHash: String(repeating: "0", count: 64))
        task.finishedAt = Date(timeIntervalSince1970: 900)
        task.resultVerifiedAt = task.finishedAt
        task.summary = "delivered"
        task.isolation = .worktree
        task.worktree = Orchestrator.Worktree(
            path: OrchestratorDraft.worktreePath(project: repository.url.path, taskID: id)!,
            branch: OrchestratorDraft.worktreeBranch(for: id)!, base: repository.base,
            repository: repository.url.path, cwd: repository.url.path,
            head: storedHead, commits: 1, dirty: false)
        task.landing = Orchestrator.Landing(
            state: .pending, target: "main", delivery: "the delivery",
            ownerRootKey: "0123abcd", since: opened, commit: nil, note: nil)
        Orchestrator.holdScheduleTaskForTesting(task)
    }
    func holdSharedCheckout() {
        var task = Orchestrator.Task(
            id: sweepUnreadableWriteSetTask, state: .success, kind: "custom",
            title: "a shared-checkout delivery", assistant: .claude,
            projectDir: repository.url.path, timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: 3), rootSessionId: "sweep-root",
            claims: ["Sources/Kept.swift"], claimsDeclared: true,
            secretHash: String(repeating: "0", count: 64))
        task.finishedAt = Date(timeIntervalSince1970: 900)
        task.resultVerifiedAt = task.finishedAt
        task.summary = "delivered"
        task.claimKeys = OrchestratorDraft.freezeClaims(task.claims, projectDir: task.projectDir)
        task.landing = Orchestrator.Landing(
            state: .pending, target: "main", delivery: "the delivery",
            ownerRootKey: "0123abcd", since: opened, commit: nil, note: nil)
        Orchestrator.holdScheduleTaskForTesting(task)
    }

    let unreadableBranch = "clawdline/task/\(sweepUnreadableRefsTask)"
    let actualLiveHead = testGit(["rev-parse", "HEAD"], cwd: repository.url).output
    expect("the failure fixture has a live head different from its stored merged head",
           testGit(["branch", unreadableBranch, actualLiveHead], cwd: repository.url).status, 0)
    check("and the actual live head is neither the stored head nor contained by main",
          actualLiveHead != repository.delivery
              && testGit(["merge-base", "--is-ancestor", actualLiveHead, "main"],
                         cwd: repository.url).status != 0)
    hold(sweepUnreadableRefsTask, storedHead: repository.delivery)
    hold(sweepDeletedBranchTask, storedHead: repository.delivery)
    holdSharedCheckout()
    Orchestrator.forgetLandingSweepObservations()
    let affected = [
        sweepUnreadableRefsTask, sweepDeletedBranchTask, sweepUnreadableWriteSetTask,
    ]

    let gitDirectory = OrchestratorDraft.gitCommonDirectory(at: repository.url.path)!
    let successfulScan = Orchestrator.landingSweepDeliveryRefs(gitDirectory: gitDirectory)
    expect("a successful scan reports the moved live ref as present",
           successfulScan.observation(for: unreadableBranch), .present(actualLiveHead))
    expect("and confirms a deleted delivery branch is absent rather than unreadable",
           successfulScan.observation(
            for: "clawdline/task/\(sweepDeletedBranchTask)"), .absent)
    func reason(_ id: String, in outcomes: [Orchestrator.LandingSweepOutcome]) -> String? {
        if case .left(let reason) = outcomes.first(where: { $0.taskID == id })?.verdict {
            return reason
        }
        return nil
    }

    Orchestrator.landingSweepDeliveryRefsGitOverrideForTesting = { _ in nil }
    let unlaunchable = Orchestrator.landingSweepPass(now: Date(timeIntervalSince1970: 4_800))
    expect("the unlaunchable scan produced one outcome for each affected record",
           unlaunchable.map(\.taskID).sorted(),
           affected.sorted())
    check("an unlaunchable ref scan leaves every affected record pending",
          affected.allSatisfy {
            Orchestrator.held($0)?.landing?.state == .pending
          })
    check("and says command launch was unanswerable rather than branch absence",
          unlaunchable.allSatisfy {
            if case .left(let why) = $0.verdict {
                return why.contains("could not launch the delivery-ref scan")
            }
            return false
          })

    Orchestrator.landingSweepDeliveryRefsGitOverrideForTesting = { _ in
        OrchestratorDraft.GitAnswer(output: "not-a-ref-record", status: 0)
    }
    let malformed = Orchestrator.landingSweepPass(now: Date(timeIntervalSince1970: 4_900))
    expect("the malformed scan produced one outcome for each affected record",
           malformed.map(\.taskID).sorted(),
           affected.sorted())
    check("malformed ref output leaves every affected record pending",
          affected.allSatisfy {
            Orchestrator.held($0)?.landing?.state == .pending
          })
    check("and is typed as malformed rather than confirmed absence",
          malformed.allSatisfy {
            if case .left(let why) = $0.verdict {
                return why.contains("delivery-ref scan returned malformed output")
            }
            return false
          })

    var undecodableRefBytes = Data("\(unreadableBranch) \(actualLiveHead)\n"
        .utf8)
    undecodableRefBytes.append(contentsOf: "clawdline/task/".utf8)
    undecodableRefBytes.append(0xff)
    undecodableRefBytes.append(contentsOf: "rogue \(repository.delivery)\n".utf8)
    let undecodableAnswer = OrchestratorDraft.GitAnswer(
        outputData: undecodableRefBytes, status: 0)
    check("the byte fixture crosses the production decoder as unreadable",
          undecodableAnswer.output.isEmpty && !undecodableAnswer.outputIsUTF8)
    Orchestrator.landingSweepDeliveryRefsGitOverrideForTesting = { _ in undecodableAnswer }
    let undecodable = Orchestrator.landingSweepPass(now: Date(timeIntervalSince1970: 4_950))
    expect("the undecodable byte scan produced one outcome for each affected record",
           undecodable.map(\.taskID).sorted(),
           affected.sorted())
    check("status-zero stdout with invalid UTF-8 leaves every affected record pending",
          affected.allSatisfy {
            Orchestrator.held($0)?.landing?.state == .pending
          })
    check("and stays typed as unreadable rather than confirmed absence",
          undecodable.allSatisfy {
            if case .left(let why) = $0.verdict {
                return why.contains("delivery-ref scan output is not valid UTF-8")
            }
            return false
          })

    Orchestrator.landingSweepDeliveryRefsGitOverrideForTesting = { _ in
        OrchestratorDraft.GitAnswer(output: "temporary ref backend failure", status: 1)
    }
    let failed = Orchestrator.landingSweepPass(now: Date(timeIntervalSince1970: 5_000))
    expect("the failed scan produced one outcome for each affected record",
           failed.map(\.taskID).sorted(),
           affected.sorted())
    check("a failed ref scan is typed unanswerable at the pass seam",
          reason(sweepUnreadableRefsTask, in: failed)?.contains(
            "delivery-ref scan exited with status 1") == true)
    check("stored B cannot close while the unseen live ref is C, even though target contains B",
          Orchestrator.held(sweepUnreadableRefsTask)?.landing?.state == .pending)
    check("the same unreadable scan cannot masquerade as a deleted branch",
          Orchestrator.held(sweepDeletedBranchTask)?.landing?.state == .pending)
    check("nor can it fall through to the shared checkout's usable write set",
          reason(sweepUnreadableWriteSetTask, in: failed)?.contains(
            "delivery-ref scan exited with status 1") == true
              && Orchestrator.held(sweepUnreadableWriteSetTask)?.landing?.state == .pending)

    Orchestrator.landingSweepDeliveryRefsGitOverrideForTesting = nil
    let answered = Orchestrator.landingSweepPass(now: Date(timeIntervalSince1970: 5_300))
    expect("the successfully confirmed-deleted branch closes from its clean stored head",
           answered.first { $0.taskID == sweepDeletedBranchTask }?.verdict,
           .closed(.ancestry))
    expect("that closure names the stored merged delivery commit",
           Orchestrator.held(sweepDeletedBranchTask)?.landing?.verifiedCommit,
           repository.delivery)
    check("while a successful present ref must still agree with the stored head",
          reason(sweepUnreadableRefsTask, in: answered)?.contains(
            "live head contradicts its recorded head") == true
              && Orchestrator.held(sweepUnreadableRefsTask)?.landing?.state == .pending)
}

group("a sweep that cannot prove it leaves the record exactly as it found it") {
    let repository = makeSweepRepository()
    defer { try? FileManager.default.removeItem(at: repository.url) }
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    let opened = Date(timeIntervalSince1970: 1_000)
    func landing(_ state: Orchestrator.LandingState, target: String?) -> Orchestrator.Landing {
        Orchestrator.Landing(state: state, target: target, delivery: nil,
                             ownerRootKey: "0123abcd", since: opened,
                             commit: state == .landed ? String(repeating: "a", count: 40) : nil,
                             note: "a root wrote this")
    }
    func hold(_ id: String, claims: [String], landingState: Orchestrator.LandingState = .pending,
              target: String? = "main", state: Orchestrator.State = .success) {
        var task = Orchestrator.Task(
            id: id, state: state, kind: "custom", title: "a delivery nobody closed",
            assistant: .claude, projectDir: repository.url.path, timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: 1), rootSessionId: "sweep-root",
            claims: claims, claimsDeclared: true,
            secretHash: String(repeating: "0", count: 64))
        task.finishedAt = state.isTerminal ? Date(timeIntervalSince1970: 900) : nil
        task.claimKeys = OrchestratorDraft.freezeClaims(claims, projectDir: task.projectDir)
        task.landing = landing(landingState, target: target)
        Orchestrator.holdScheduleTaskForTesting(task)
    }

    hold(sweepDirtyTask, claims: ["Sources/Dirty.swift"])
    hold(sweepDifferingTask, claims: ["Sources/Differs.swift"])
    hold(sweepUnreadableTask, claims: ["Sources/Kept.swift"], target: "no-such-branch")
    hold(sweepSettledTask, claims: ["Sources/Kept.swift"], landingState: .landed)
    hold(sweepLiveTask, claims: ["Sources/Kept.swift"], state: .briefed)
    hold(sweepUntargetedTask, claims: ["Sources/Kept.swift"], target: nil)
    // The class the whole feature was nearly wrong about: a claim that matches no file. `git
    // status` and `git diff` answer *exit 0, nothing* for it, which is the answer they give for a
    // path that is clean, and three of the twelve real records this shipped for were exactly this
    // shape — three filenames joined by spaces into one path that exists nowhere.
    hold(sweepMissingClaimTask, claims: ["Sources/Gone.swift"])
    // Its control: a claim absent from the checkout and present in the target, which is what a
    // landed deletion looks like from here. It must not be read as the row above.
    hold(sweepDeletedClaimTask, claims: ["Sources/Removed.swift"])
    // The same class, and the one shape of it git talks about. A pathspec naming a directory that
    // does not exist makes git print `warning: could not open directory …` **and exit 0**, and
    // while the two streams shared a pipe that line became a claimed path called
    // `ning: could not open directory …` — the accident that was the only thing separating this
    // row from the one above it.
    hold(sweepWarningClaimTask, claims: ["Sources/nope/one.md"])
    Orchestrator.forgetLandingSweepObservations()

    let held = [sweepDirtyTask, sweepDifferingTask, sweepUnreadableTask, sweepSettledTask,
                sweepLiveTask, sweepUntargetedTask, sweepMissingClaimTask, sweepDeletedClaimTask,
                sweepWarningClaimTask].compactMap { Orchestrator.held($0) }
    expect("every row in this fixture was seeded", held.count, 9)
    let candidates = Orchestrator.landingSweepCandidates(held).map(\.taskID).sorted()
    expect("a settled record, a live task and a record with no target are not candidates at all",
           candidates, [sweepDirtyTask, sweepDifferingTask, sweepUnreadableTask,
                        sweepMissingClaimTask, sweepDeletedClaimTask,
                        sweepWarningClaimTask].sorted())

    let pass = Orchestrator.landingSweepPass(now: Date(timeIntervalSince1970: 5_000))
    func why(_ id: String) -> String? {
        if case .left(let reason) = pass.first(where: { $0.taskID == id })?.verdict {
            return reason
        }
        return nil
    }
    check("a claimed path somebody is still editing is not a settled write set",
          why(sweepDirtyTask)?.contains("uncommitted") == true)
    check("a claimed path that never reached the target is a different finding, and also not one",
          why(sweepDifferingTask)?.contains("differ from refs/heads/main") == true)
    check("and a target git will not answer for is skipped, never read as nothing having landed",
          why(sweepUnreadableTask)?.contains("refs/heads/no-such-branch") == true
              && why(sweepUnreadableTask)?.contains("not the same as nothing having landed")
                  == true)
    check("a claim that matches no file is unanswerable, not a settled write set",
          why(sweepMissingClaimTask)?.contains("unanswerable") == true
              && why(sweepMissingClaimTask)?.contains("Sources/Gone.swift") == true)
    check("while a claim the target still carries resolves, and gets a finding about content",
          why(sweepDeletedClaimTask)?.contains("unanswerable") == false
              && why(sweepDeletedClaimTask)?.contains("differ from refs/heads/main") == true)
    check("a warning git prints about a missing directory is not a claimed path",
          why(sweepWarningClaimTask)?.contains("unanswerable") == true
              && why(sweepWarningClaimTask)?.contains("could not open directory") == false)
    check("nothing this pass could not prove was written",
          [sweepDirtyTask, sweepDifferingTask, sweepUnreadableTask, sweepLiveTask,
           sweepUntargetedTask, sweepMissingClaimTask, sweepDeletedClaimTask,
           sweepWarningClaimTask].allSatisfy { Orchestrator.held($0)?.landing?.state == .pending })

    // **The seam itself, on the one command that produces the line.** git writes this to stderr
    // and exits 0, so a `0` status is not what tells the two apart; keeping the streams apart is.
    let noisy = OrchestratorDraft.git(
        ["status", "--porcelain=v1", "--untracked-files=all", "--", "Sources/nope/one.md"],
        cwd: repository.url.path, separateStandardError: true)
    expect("git answers a missing directory with a zero status", noisy?.status, 0)
    check("its warning is on the stream git wrote it to",
          noisy?.errorOutput.contains("could not open directory") == true)
    check("and not in the output a porcelain reader parses",
          noisy?.output.contains("warning") == false && noisy?.output.isEmpty == true)
    // And the reader is fail-closed whatever the seam does, because a caller can still be handed
    // a merged stream: a line that is not `XY <path>` means git did not answer.
    check("a diagnostic line is not a porcelain record",
          Orchestrator.landingSweepPorcelainPath(
            "warning: could not open directory 'a/nope/': No such file or directory") == nil
              && Orchestrator.landingSweepPorcelainPath("fatal: bad revision 'x'") == nil
              && Orchestrator.landingSweepPorcelainPath("MM") == nil)
    expect("while a record keeps its path exactly",
           Orchestrator.landingSweepPorcelainPath(" M Sources/Dirty.swift"),
           "Sources/Dirty.swift")
    expect("including the untracked one, whose two characters are not letters",
           Orchestrator.landingSweepPorcelainPath("?? docs/new.md"), "docs/new.md")

    // **The two conditions in `isSettled` are two conditions.** Until this fixture was corrected,
    // the one dirty path was also different from the target, so no test could tell them apart and
    // a mutation deleting `outstanding.isEmpty` survived the whole suite. The shape that needs
    // `outstanding` on its own is a path being saved back to exactly what the target holds.
    let editing = Orchestrator.landingSweepWriteSet(
        projectDir: repository.url.path, targetCommit: repository.main,
        claims: ["Sources/Dirty.swift"])
    expect("a path somebody is still editing is outstanding", editing?.outstanding,
           ["Sources/Dirty.swift"])
    expect("and identical to the target all the same, so `differing` is empty",
           editing?.differing, [])
    check("which is not a settled write set, and only `outstanding` says so",
          editing?.isSettled == false)
    expect("a claim that matches nothing is neither outstanding nor differing — it is unresolved",
           Orchestrator.landingSweepWriteSet(
            projectDir: repository.url.path, targetCommit: repository.main,
            claims: ["Sources/Gone.swift"]),
           Orchestrator.LandingSweepWriteSet(outstanding: [], differing: [],
                                             unresolved: ["Sources/Gone.swift"]))

    // The compare-and-swap, driven directly, because a pass cannot race itself in one thread.
    let stale = landing(.pending, target: "main")
    let elsewhere = Orchestrator.Landing(state: .pending, target: "main", delivery: nil,
                                         ownerRootKey: "0123abcd", since: opened, commit: nil,
                                         note: "a different sentence entirely")
    let wouldClose = Orchestrator.landingSweepLanding(
        existing: stale, arm: .writeSet, verification: nil, targetCommit: repository.main,
        target: "main", claimCount: 1, now: Date(timeIntervalSince1970: 5_400),
        observed: (Date(timeIntervalSince1970: 5_000), Date(timeIntervalSince1970: 5_400)))
    check("the record this pass decided about was one it could form", wouldClose != nil)
    if let wouldClose {
        expect("a record that changed while git was being asked is left exactly as it is",
               Orchestrator.applyLandingSweep(task: Orchestrator.held(sweepDirtyTask)!,
                                              expected: elsewhere, landing: wouldClose,
                                              arm: .writeSet).verdict,
               .left("the record changed while git was being asked, so it was left alone"))
    }
    check("and it is still the record a root wrote",
          Orchestrator.held(sweepDirtyTask)?.landing == stale)

    // The cap is a cap on one pass, not on the registry: what a pass does not reach, the next one
    // does. Pure, so this costs no subprocess at all.
    let many = (0..<25).map { index -> Orchestrator.Task in
        var task = Orchestrator.Task(
            id: String(format: "31313131-4141-5151-6161-7171717171%02x", index + 32),
            state: .success, kind: "custom", title: "a delivery nobody closed",
            assistant: .claude, projectDir: repository.url.path, timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: TimeInterval(100 + index)),
            rootSessionId: "sweep-root", claims: ["Sources/Kept.swift"], claimsDeclared: true,
            secretHash: String(repeating: "0", count: 64))
        task.landing = landing(.pending, target: "main")
        return task
    }
    let capped = Orchestrator.landingSweepCandidates(many)
    expect("one pass looks at twenty records and no more", capped.count, 20)
    expect("and they are the oldest twenty, so nothing at the back is starved",
           capped.map(\.taskID), many.prefix(20).map(\.id))
    expect("and the one record that was already settled is byte for byte what a root wrote",
           Orchestrator.held(sweepSettledTask)?.landing, landing(.landed, target: "main"))

    // The two shapes the writer itself refuses, so that a caller cannot assemble the wrong record
    // even if the phases above were ever wired up the wrong way round.
    let open = landing(.pending, target: "main")
    let real = Orchestrator.LandingVerification(
        origin: "local_target_branch", commit: String(repeating: "c", count: 40),
        targetCommit: String(repeating: "d", count: 40))
    check("an ancestry arm with no verification writes nothing",
          Orchestrator.landingSweepLanding(
            existing: open, arm: .ancestry, verification: nil,
            targetCommit: String(repeating: "d", count: 40), target: "main", claimCount: 0,
            now: Date(timeIntervalSince1970: 5_000)) == nil)
    check("and a write-set arm carrying one writes nothing either",
          Orchestrator.landingSweepLanding(
            existing: open, arm: .writeSet, verification: real,
            targetCommit: String(repeating: "d", count: 40), target: "main", claimCount: 1,
            now: Date(timeIntervalSince1970: 5_000)) == nil)
    check("a pathspec that could mean something else to git disqualifies the whole write set",
          !Orchestrator.landingSweepClaimsUsable(["Sources/A.swift", "../elsewhere"])
              && !Orchestrator.landingSweepClaimsUsable([":(glob)Sources/*"])
              && !Orchestrator.landingSweepClaimsUsable([])
              && Orchestrator.landingSweepClaimsUsable(["Sources/A.swift", "docs"]))
    check("a live delivery ref that contradicts the isolated receipt is not chosen",
          Orchestrator.landingSweepQuestion(
            for: Orchestrator.LandingSweepCandidate(
                taskID: sweepDirtyTask, projectDir: repository.url.path, target: "main",
                deliveryBranch: "clawdline/task/\(sweepDirtyTask)",
                deliveryEvidence: .isolated(
                    base: repository.base, head: "stale", commits: 1, dirty: false),
                claims: ["Sources/Dirty.swift"]),
            deliveryRef: .present(repository.delivery))
              == .unanswerable("isolated delivery live head contradicts its recorded head"))
    check("and a candidate with neither a branch nor a usable write set is asked nothing",
          Orchestrator.landingSweepQuestion(
            for: Orchestrator.LandingSweepCandidate(
                taskID: sweepDirtyTask, projectDir: repository.url.path, target: "main",
                deliveryBranch: "clawdline/task/\(sweepDirtyTask)",
                deliveryEvidence: .sharedCheckout, claims: []),
            deliveryRef: .absent)
              == .unanswerable("no delivery branch, and no usable declared write set"))
}


group("a settled landing is replayed, corrected against the same target, or refused — never ok in vain") {
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    let fixture = makeLandingRepository()
    defer { try? FileManager.default.removeItem(at: fixture.url) }
    try! Data("second\n".utf8).write(to: fixture.url.appendingPathComponent("second.txt"))
    expect("the fixture stages a second commit on the same target",
           testGit(["add", "second.txt"], cwd: fixture.url).status, 0)
    expect("and commits it", testGit([
        "-c", "user.name=Clawdline Tests", "-c", "user.email=tests@clawdline.invalid",
        "commit", "-qm", "the commit that actually landed",
    ], cwd: fixture.url).status, 0)
    let landedCommit = testGit(["rev-parse", "HEAD"], cwd: fixture.url).output
    expect("the fixture opens a second named target, which is a different claim and not a spelling",
           testGit(["branch", "release/main"], cwd: fixture.url).status, 0)

    let id = "30303030-4040-5050-6060-707070707070"
    var task = Orchestrator.Task(
        id: id, state: .success, kind: "custom", title: "a record naming the wrong commit",
        assistant: .claude, projectDir: fixture.url.path, timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 1), rootSessionId: "landing-correction-root",
        claims: ["Sources/Landing.swift"], claimsDeclared: true,
        secretHash: Orchestrator.hash(ofSecret: String(repeating: "e5", count: 32)))
    task.claimKeys = OrchestratorDraft.freezeClaims(task.claims, projectDir: task.projectDir)
    Orchestrator.holdScheduleTaskForTesting(task)
    func settle(_ raw: [String: Any], at seconds: TimeInterval) -> Orchestrator.Reply {
        Orchestrator.updateLanding(taskID: id, secret: "",
                                   orchestratorToken: Orchestrator.dispatchToken(), raw: raw,
                                   now: Date(timeIntervalSince1970: seconds))
    }
    func landing(_ reply: Orchestrator.Reply) -> [String: Any]? {
        guard case .ok(let body) = reply, let task = body["task"] as? [String: Any] else {
            return nil
        }
        return task["landing"] as? [String: Any]
    }
    func stored() -> Orchestrator.Landing? { Orchestrator.held(id)?.landing }
    func refusal(_ reply: Orchestrator.Reply) -> (status: Int, code: String, extra: [String: Any])? {
        if case .refused(let status, let code, _, let extra) = reply {
            return (status, code, extra)
        }
        return nil
    }

    // The record that names the wrong commit: the first commit, when the work is on the second.
    let first = settle(["state": "landed", "target": "main", "commit": fixture.commit], at: 10)
    check("the mistaken record is written the ordinary way",
          landing(first)?["commit"] as? String == fixture.commit
              && landing(first)?["landed_at"] as? Int == 10)

    // Three resends. They differ from each other in exactly one field, and each field decides a
    // different answer: same commit replays, a different commit on the same target is a
    // correction, a different target is a different claim.
    let replayed = settle(["state": "landed", "commit": String(fixture.commit.prefix(8))], at: 20)
    check("an abbreviation of the recorded commit is the same claim, replayed, changing nothing",
          landing(replayed)?["commit"] as? String == fixture.commit
              && landing(replayed)?["landed_at"] as? Int == 10)
    let wrongTarget = refusal(settle(["state": "landed", "target": "release/main",
                                      "commit": fixture.commit], at: 21))
    check("a settled landing aimed at another target is refused, naming both values",
          wrongTarget?.status == 409 && wrongTarget?.code == "landing_conflict"
              && wrongTarget?.extra["field"] as? String == "target"
              && wrongTarget?.extra["stored"] as? String == "main"
              && wrongTarget?.extra["requested"] as? String == "release/main")
    check("and it changed nothing", stored()?.commit == fixture.commit)
    let unprovable = refusal(settle(["state": "landed", "commit": String(repeating: "9", count: 40)],
                                    at: 22))
    check("a correction the broker cannot prove is refused by the same gate as a first landing",
          unprovable?.status == 409 && unprovable?.code == "unverified_landing")
    check("and it changed nothing either", stored()?.commit == fixture.commit)

    // The correction itself: same target, a commit the broker resolves and proves contained.
    let corrected = settle(["state": "landed", "commit": landedCommit], at: 30)
    check("a verified same-target correction is applied rather than swallowed",
          landing(corrected)?["commit"] as? String == landedCommit
              && landing(corrected)?["verified_commit"] as? String == landedCommit
              && landing(corrected)?["target"] as? String == "main"
              && landing(corrected)?["since"] as? Int == 10
              && landing(corrected)?["landed_at"] as? Int == 30)
    var correctedFrom: [String: Any]?
    if case .ok(let body) = corrected { correctedFrom = body["corrected_from"] as? [String: Any] }
    check("and the record it replaced is handed back rather than dropped in silence",
          correctedFrom?["commit"] as? String == fixture.commit
              && correctedFrom?["landed_at"] as? Int == 10)
    check("a replay of the corrected record is a replay again",
          landing(settle(["state": "landed", "commit": landedCommit], at: 31))?["landed_at"]
              as? Int == 30)

    // The reading itself, as a pure function: one field changes, one answer changes.
    let record = Orchestrator.Landing(
        state: .landed, target: "main", delivery: "clawdline/task/x", ownerRootKey: "abcdef01",
        since: Date(timeIntervalSince1970: 1), commit: landedCommit, note: "landed by root",
        landedAt: Date(timeIntervalSince1970: 2), verificationOrigin: "local_target_branch",
        verifiedCommit: landedCommit, verifiedTargetCommit: landedCommit)
    let asRecorded = ["target": "main", "commit": landedCommit, "delivery": "clawdline/task/x",
                      "note": "landed by root"]
    expect("a resend that contradicts nothing is a replay",
           Orchestrator.landingResend(existing: record, requested: asRecorded), .replay)
    var otherCommit = asRecorded
    otherCommit["commit"] = String(repeating: "a", count: 40)
    check("changing only the commit makes it a correction",
          Orchestrator.landingResend(existing: record, requested: otherCommit)
              == .correction(field: "commit", stored: landedCommit,
                             requested: String(repeating: "a", count: 40)))
    var otherTarget = asRecorded
    otherTarget["target"] = "release/main"
    check("changing only the target makes it a conflict",
          Orchestrator.landingResend(existing: record, requested: otherTarget)
              == .conflict(field: "target", stored: "main", requested: "release/main"))
    var otherNote = asRecorded
    otherNote["note"] = "landed by somebody else"
    check("and changing only the note is still a write, so it is not a replay",
          Orchestrator.landingResend(existing: record, requested: otherNote)
              == .correction(field: "note", stored: "landed by root",
                             requested: "landed by somebody else"))
    expect("a resend that says nothing at all contradicts nothing",
           Orchestrator.landingResend(existing: record, requested: [:]), .replay)
}
}
