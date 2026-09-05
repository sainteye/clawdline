import Foundation

// **Whether the landing a surface shows is the one that is true now**, and the state a delivery
// that wrote nothing has.
//
// The defect these two groups were written for is one defect. `usage_intervals.landing_state` is
// a point-in-time copy taken when a task reaches a terminal state, and a landing closes *after*
// the work ends — so the field is almost always absent at sampling time, and the only thing that
// ever filled it in was the backfill import on launch. Measured over this Mac's own store on
// 2026-09-05: of the tasks whose landing closed before the last launch, 79 of 79 carried the
// copy; of those closed after it, 0 of 6 did. Two of the second group were sitting in the
// Projects page's 「做完了，沒有落地」 block while the broker held a verified `landed` for each.
//
// They live in a file of their own because `Tests/UsageLedgerTests.swift` and
// `Tests/OrchestratorLandingTests.swift` are both within a hundred lines of the 2,000-line
// stop-growth limit, and a test trimmed to fit under a wall leaves the next person standing at
// it.

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
          !Orchestrator.nothingToLandAdmission(for: named).isAdmitted)
    check("a dirty checkout is evidence too",
          !Orchestrator.nothingToLandAdmission(for: Orchestrator.Task(
            id: "20202020-3030-4040-5050-606060606065", state: .success, kind: "review",
            title: "dirty", assistant: .claude, projectDir: repository.url.path,
            timeoutMinutes: 30, created: Date(timeIntervalSince1970: 1),
            worktree: checkout(commits: 0, dirty: true), secretHash: "")).isAdmitted)
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

}
