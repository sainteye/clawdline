import AppKit
import Foundation

/// A repository with a `main` and two delivery branches on it, so the queue's git half has
/// something real to read: `repositoryBranches` only ever looks under `refs/heads/clawdline/task/`.
func makeLandingQueueRepository() -> (url: URL, base: String) {
    let url = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-landing-queue-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
    expect("the landing-queue fixture repository initializes",
           testGit(["init", "-q", "-b", "main"], cwd: url).status, 0)
    try! Data("ceiling=1\n".utf8).write(to: url.appendingPathComponent("guard.txt"))
    _ = testGit(["add", "guard.txt"], cwd: url)
    expect("the landing-queue fixture commits its base", testGit([
        "-c", "user.name=Clawdline Tests", "-c", "user.email=tests@clawdline.invalid",
        "commit", "-qm", "base",
    ], cwd: url).status, 0)
    return (url, testGit(["rev-parse", "HEAD"], cwd: url).output)
}

/// One delivery branch that changes exactly the named files, left unmerged.
@discardableResult
func makeLandingQueueDelivery(in repository: URL, taskID: String, base: String,
                              writing files: [String]) -> String {
    let branch = "clawdline/task/\(taskID)"
    _ = testGit(["checkout", "-q", "-b", branch, base], cwd: repository)
    for file in files {
        let path = repository.appendingPathComponent(file)
        try? FileManager.default.createDirectory(at: path.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true)
        try! Data("\(taskID) wrote \(file)\n".utf8).write(to: path)
        _ = testGit(["add", file], cwd: repository)
    }
    _ = testGit(["-c", "user.name=Clawdline Tests", "-c", "user.email=tests@clawdline.invalid",
                 "commit", "-qm", "delivery \(taskID)"], cwd: repository)
    let head = testGit(["rev-parse", "HEAD"], cwd: repository).output
    _ = testGit(["checkout", "-q", "main"], cwd: repository)
    return head
}

/// The one spelling of the root digest in this suite. `rootKeyDigest` is one of the thirty-seven
/// statics `d2f25d29` moved from `Orchestrator` to `OrchestratorDraft` after this branch's base,
/// and that move produces no merge conflict and no warning — only a compiler error on the merged
/// tree. One call site is one line to repair.
func landingQueueDigest(_ rootKey: String) -> String { OrchestratorDraft.rootKeyDigest(rootKey) }

func landingQueueTask(id: String, title: String, state: Orchestrator.State, root: String,
                      label: String?, projectDir: String, created: TimeInterval,
                      claims: [String] = [], landing: Orchestrator.Landing? = nil,
                      worktree: Orchestrator.Worktree? = nil) -> Orchestrator.Task {
    Orchestrator.Task(
        id: id, state: state, kind: "custom", title: title, assistant: .claude,
        projectDir: projectDir, timeoutMinutes: 60,
        created: Date(timeIntervalSince1970: created), rootSessionId: root, rootLabel: label,
        claims: claims, claimsDeclared: !claims.isEmpty, landing: landing,
        isolation: worktree == nil ? .none : .worktree, worktree: worktree,
        secretHash: Orchestrator.hash(ofSecret: String(repeating: "b2", count: 32)))
}

func landingQueueWorktree(taskID: String, base: String, repository: String)
    -> Orchestrator.Worktree {
    Orchestrator.Worktree(path: repository + "/wt-" + taskID,
                          branch: "clawdline/task/\(taskID)", base: base,
                          repository: repository, cwd: repository + "/wt-" + taskID)
}

/// Give one group its own registry, its own queue store and its own repository, and put the
/// process back exactly as it was afterwards.
func withLandingQueueFixture(_ body: (URL, String) -> Void) {
    let repository = makeLandingQueueRepository()
    let queueStore = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-landing-queue-store-\(UUID().uuidString).json")
    let previousStore = Orchestrator.storeURLOverrideForTesting
    Orchestrator.storeURLOverrideForTesting = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-landing-queue-registry-\(UUID().uuidString).json")
    OrchestratorLandingQueue.storeURLOverrideForTesting = queueStore
    OrchestratorLandingQueue.forget()
    Orchestrator.forget()
    defer {
        Orchestrator.forget()
        OrchestratorLandingQueue.forget()
        OrchestratorLandingQueue.storeURLOverrideForTesting = nil
        try? FileManager.default.removeItem(at: Orchestrator.storeURL)
        Orchestrator.storeURLOverrideForTesting = previousStore
        try? FileManager.default.removeItem(at: queueStore)
        try? FileManager.default.removeItem(at: repository.url)
    }
    body(repository.url, repository.base)
}

func landingQueueBody(_ reply: Orchestrator.Reply) -> [String: Any]? {
    guard case .ok(let body) = reply else { return nil }
    return body
}

func landingQueueRefusal(_ reply: Orchestrator.Reply) -> (status: Int, code: String)? {
    guard case .refused(let status, let code, _, _) = reply else { return nil }
    return (status, code)
}

func landingQueueRows(_ reply: Orchestrator.Reply) -> [[String: Any]] {
    landingQueueBody(reply)?["queue"] as? [[String: Any]] ?? []
}

func landingQueueRow(_ reply: Orchestrator.Reply, digest: String) -> [String: Any]? {
    landingQueueRows(reply).first { $0["root_key"] as? String == digest }
}

func runOrchestratorLandingQueueTests() {
group("the landing queue derives its own membership from work nobody wrote down") {
    withLandingQueueFixture { repository, base in
        let now = Date(timeIntervalSince1970: 5_000)
        // Failure 1, exactly as it happened: two roots working in one checkout whose children were
        // touching the most contended files, and a hand-maintained queue that did not contain them.
        // Nothing below ever adds an entry — the only calls are dispatch-shaped ones.
        let working = "230CF496-0000-4000-8000-000000000001"
        let delivered = "A1EE5C53-0000-4000-8000-000000000002"
        Orchestrator.holdScheduleTaskForTesting(landingQueueTask(
            id: "11111111-1111-4111-8111-111111111111", title: "still writing the guard",
            state: .briefed, root: working, label: "root that was invisible",
            projectDir: repository.path, created: 1_000,
            claims: ["tools/check-architecture-boundaries.sh"]))
        let deliveryHead = makeLandingQueueDelivery(
            in: repository, taskID: "22222222-2222-4222-8222-222222222222", base: base,
            writing: ["Sources/Orchestrator.swift"])
        Orchestrator.holdScheduleTaskForTesting(landingQueueTask(
            id: "22222222-2222-4222-8222-222222222222", title: "delivered and unlanded",
            state: .success, root: delivered, label: "root that had already finished",
            projectDir: repository.path, created: 2_000,
            worktree: landingQueueWorktree(taskID: "22222222-2222-4222-8222-222222222222",
                                           base: base, repository: repository.path)))

        let reply = OrchestratorLandingQueue.queueReply(project: repository.path, now: now)
        let rows = landingQueueRows(reply)
        expect("both roots are in the queue although nobody added either", rows.count, 2)
        let workingDigest = landingQueueDigest(working)
        let deliveredDigest = landingQueueDigest(delivered)
        check("the root that was only working is a member",
              landingQueueRow(reply, digest: workingDigest)?["reasons"] as? [String]
                  == ["live_work"],
              "got \(String(describing: landingQueueRow(reply, digest: workingDigest)?["reasons"]))")
        check("the root with an unlanded delivery is a member",
              landingQueueRow(reply, digest: deliveredDigest)?["reasons"] as? [String]
                  == ["unlanded_delivery"],
              "got \(String(describing: landingQueueRow(reply, digest: deliveredDigest)?["reasons"]))")
        let deliveryTasks = landingQueueRow(reply, digest: deliveredDigest)?["tasks"]
            as? [[String: Any]] ?? []
        check("the delivery's branch and head come from the repository, not from a memory of it",
              (deliveryTasks.first?["delivery"] as? [String: Any])?["head"] as? String
                  == deliveryHead,
              "got \(String(describing: (deliveryTasks.first?["delivery"] as? [String: Any])?["head"]))")
        check("nobody has been placed yet, and unplaced entries still have rows",
              rows.allSatisfy { $0["placement"] as? String == "unplaced" && $0["position"] is NSNull })
        expect("exactly one entry holds the slot",
               rows.filter { $0["holder"] as? Bool == true }.count, 1)

        // A root that declared its landing pending stays visible with no branch at all — the
        // declared half of membership, which the derived half must not duplicate or drop.
        Orchestrator.holdScheduleTaskForTesting(landingQueueTask(
            id: "33333333-3333-4333-8333-333333333333", title: "landed in the shared tree",
            state: .success, root: "shared-tree-root", label: "shared tree",
            projectDir: repository.path, created: 3_000, claims: ["docs/api.md"],
            landing: Orchestrator.Landing(state: .pending, target: "main", delivery: nil,
                                          ownerRootKey: "abcd1234",
                                          since: Date(timeIntervalSince1970: 3_500),
                                          commit: nil, note: nil)))
        expect("a declared pending landing is a third member",
               landingQueueRows(OrchestratorLandingQueue.queueReply(project: repository.path,
                                                                    now: now)).count, 3)

        // And membership leaves by itself. `landed` is the root's own answer; nothing clears a
        // list, because there is no list.
        Orchestrator.mutateTaskForTesting("22222222-2222-4222-8222-222222222222") {
            $0.landing = Orchestrator.Landing(
                state: .landed, target: "main", delivery: nil, ownerRootKey: "abcd1234",
                since: Date(timeIntervalSince1970: 2_500), commit: deliveryHead, note: nil,
                landedAt: Date(timeIntervalSince1970: 4_000))
        }
        let after = OrchestratorLandingQueue.queueReply(project: repository.path, now: now)
        check("a landed delivery leaves the queue with nobody removing it",
              landingQueueRow(after, digest: deliveredDigest) == nil)
        expect("the queue is exactly the two lines still outstanding",
               landingQueueRows(after).count, 2)

        check("a directory outside a repository is a typed refusal, not an empty queue",
              landingQueueRefusal(OrchestratorLandingQueue.queueReply(project: "/nowhere/at/all",
                                                                      now: now))?.code
                  == "bad_request")
    }
}

group("a coordinator sets position and cannot set membership") {
    withLandingQueueFixture { repository, _ in
        let now = Date(timeIntervalSince1970: 6_000)
        let first = "order-root-one"
        let second = "order-root-two"
        let third = "order-root-three"
        for (index, root) in [first, second, third].enumerated() {
            Orchestrator.holdScheduleTaskForTesting(landingQueueTask(
                id: "4444444\(index)-4444-4444-8444-44444444444\(index)",
                title: "line \(index)", state: .briefed, root: root, label: "line \(index)",
                projectDir: repository.path, created: Double(1_000 * (index + 1)),
                claims: ["tools/check-architecture-boundaries.sh"]))
        }
        let digests = [first, second, third].map(landingQueueDigest)

        let placed = OrchestratorLandingQueue.setOrder(
            project: repository.path, keys: [digests[2], digests[0]], ifGeneration: nil,
            setBy: "clawdfather", now: now)
        let rows = landingQueueRows(placed)
        expect("an accepted order answers with the whole queue", rows.count, 3)
        expect("the coordinator's first choice is position 1",
               rows.first?["root_key"] as? String, digests[2])
        expect("the coordinator's second choice is position 2",
               rows.dropFirst().first?["position"] as? Int, 2)
        // The property the whole design exists for: an order that forgot somebody does not
        // shorten the queue. The forgotten line is last, and it is labelled rather than silent.
        expect("the line nobody placed is still in the queue, at the end",
               rows.last?["root_key"] as? String, digests[1])
        check("and it is labelled unplaced rather than given an invented position",
              rows.last?["placement"] as? String == "unplaced"
                  && rows.last?["position"] is NSNull)
        check("the order names who it has not placed",
              (landingQueueBody(placed)?["order"] as? [String: Any])?["unplaced"] as? [String]
                  == [digests[1]])
        expect("the stored order has a generation",
               (landingQueueBody(placed)?["order"] as? [String: Any])?["generation"] as? Int, 1)

        check("an order cannot invent a member",
              landingQueueRefusal(OrchestratorLandingQueue.setOrder(
                  project: repository.path, keys: [digests[0], "deadbeef"], ifGeneration: nil,
                  setBy: nil, now: now))?.code == "not_queued")
        check("a repeated key has no position",
              landingQueueRefusal(OrchestratorLandingQueue.setOrder(
                  project: repository.path, keys: [digests[0], digests[0]], ifGeneration: nil,
                  setBy: nil, now: now))?.code == "duplicate_entry")
        check("an order written against a generation that has moved is refused",
              landingQueueRefusal(OrchestratorLandingQueue.setOrder(
                  project: repository.path, keys: [digests[0]], ifGeneration: 0, setBy: nil,
                  now: now))?.code == "stale_order")
        check("and the refusal hands back the generation to retry against",
              {
                  guard case .refused(_, _, _, let extra) = OrchestratorLandingQueue.setOrder(
                      project: repository.path, keys: [digests[0]], ifGeneration: 0, setBy: nil,
                      now: now) else { return false }
                  return extra["generation"] as? Int == 1
              }())

        // An ordered line that lands leaves the queue; its position does not resurrect it, and
        // the order says so rather than quietly holding a name for something that is gone.
        Orchestrator.mutateTaskForTesting("44444442-4444-4444-8444-444444444442") {
            $0.state = .success
            $0.landing = Orchestrator.Landing(
                state: .landed, target: "main", delivery: nil, ownerRootKey: "abcd1234",
                since: Date(timeIntervalSince1970: 5_000), commit: String(repeating: "a", count: 40),
                note: nil, landedAt: Date(timeIntervalSince1970: 5_500))
        }
        let afterLanding = OrchestratorLandingQueue.queueReply(project: repository.path, now: now)
        check("a stored position does not keep a landed line in the queue",
              landingQueueRow(afterLanding, digest: digests[2]) == nil)
        check("the order reports the position it is still holding as stale",
              (landingQueueBody(afterLanding)?["order"] as? [String: Any])?["stale"] as? [String]
                  == [digests[2]])
        expect("and the line behind it is now position 1",
               landingQueueRows(afterLanding).first?["position"] as? Int, 1)
    }
}

group("two entries writing one path are told they are writing one path") {
    withLandingQueueFixture { repository, base in
        let now = Date(timeIntervalSince1970: 7_000)
        // Failure 4: three lines needed to change the same line of the same file, two upward and
        // one downward, and the constraint was found by a line that tripped over it. Two of these
        // declare the path and one only writes it on its branch, which is the case a queue built
        // out of `claims` alone cannot see.
        Orchestrator.holdScheduleTaskForTesting(landingQueueTask(
            id: "55555555-5555-4555-8555-555555555551", title: "raise the ceiling",
            state: .briefed, root: "ceiling-up", label: "up", projectDir: repository.path,
            created: 1_000, claims: ["tools/check-architecture-boundaries.sh", "docs/api.md"]))
        Orchestrator.holdScheduleTaskForTesting(landingQueueTask(
            id: "55555555-5555-4555-8555-555555555552", title: "lower the ceiling",
            state: .briefed, root: "ceiling-down", label: "down", projectDir: repository.path,
            created: 2_000, claims: ["tools/check-architecture-boundaries.sh"]))
        makeLandingQueueDelivery(in: repository, taskID: "55555555-5555-4555-8555-555555555553",
                                 base: base,
                                 writing: ["tools/check-architecture-boundaries.sh"])
        Orchestrator.holdScheduleTaskForTesting(landingQueueTask(
            id: "55555555-5555-4555-8555-555555555553", title: "declared nothing, wrote it anyway",
            state: .success, root: "ceiling-silent", label: "silent", projectDir: repository.path,
            created: 3_000,
            worktree: landingQueueWorktree(taskID: "55555555-5555-4555-8555-555555555553",
                                           base: base, repository: repository.path)))

        let reply = OrchestratorLandingQueue.queueReply(project: repository.path, now: now)
        let contended = landingQueueBody(reply)?["contended_paths"] as? [[String: Any]] ?? []
        expect("only the shared path is reported", contended.count, 1)
        expect("and it is the one all three write",
               contended.first?["path"] as? String, "tools/check-architecture-boundaries.sh")
        let holders = (contended.first?["entries"] as? [[String: Any]] ?? [])
            .compactMap { $0["root_key"] as? String }
        expect("every entry on that path is named", holders.count, 3)
        check("including the one that declared nothing and only its branch diff shows it",
              holders.contains(landingQueueDigest("ceiling-silent")))
        let sources = (contended.first?["entries"] as? [[String: Any]] ?? [])
            .compactMap { $0["source"] as? String }
        check("a declared path reads as claims and a branch-only one as its diff",
              Set(sources) == ["claims", "delivery_diff"],
              "got \(sources)")
        check("a path only one entry writes is not contention",
              !contended.contains { $0["path"] as? String == "docs/api.md" })
    }
}

group("the landing slot is handed on by the broker, once, and re-armed by a re-order") {
    withLandingQueueFixture { repository, _ in
        let now = Date(timeIntervalSince1970: 8_000)
        let ahead = "slot-root-ahead"
        let behind = "slot-root-behind"
        Orchestrator.holdScheduleTaskForTesting(landingQueueTask(
            id: "66666666-6666-4666-8666-666666666661", title: "in front", state: .briefed,
            root: ahead, label: "in front", projectDir: repository.path, created: 1_000,
            claims: ["Sources/Orchestrator.swift", "docs/landing.md"]))
        Orchestrator.holdScheduleTaskForTesting(landingQueueTask(
            id: "66666666-6666-4666-8666-666666666662", title: "next", state: .briefed,
            root: behind, label: "next", projectDir: repository.path, created: 2_000,
            claims: ["Sources/Orchestrator.swift"]))
        var typed: [(session: String, text: String)] = []
        let deliver: (String, String) -> String? = { session, text in
            typed.append((session, text)); return nil
        }

        // A refusal writes no receipt, which is what makes the retry below a real delivery rather
        // than an `already_notified` covering for a message that never arrived.
        check("a delivery this side could not complete is a typed refusal, not a receipt",
              landingQueueRefusal(OrchestratorLandingQueue.advance(
                  project: repository.path, now: now,
                  deliver: { _, _ in "No session named that." }))?.code
                  == "handoff_delivery_failed")
        check("a busy holder is refused before anything is typed",
              landingQueueRefusal(OrchestratorLandingQueue.advance(
                  project: repository.path, now: now,
                  readiness: { _ in "That session is showing a permission prompt." },
                  deliver: { _, _ in "should not be reached" }))?.code == "holder_busy")

        let first = OrchestratorLandingQueue.advance(project: repository.path, now: now,
                                                     deliver: deliver)
        check("the front of the queue is told the slot is theirs",
              landingQueueBody(first)?["delivered"] as? Bool == true)
        expect("and it is typed into that root's own session", typed.first?.session, ahead)
        expect("exactly one message was sent", typed.count, 1)
        // Failure 3: a wait whose `paths` correctly listed four files and whose prose foregrounded
        // one. The notice prints the whole set and then says which of the two is authoritative.
        check("the notice prints every path rather than summarising them",
              typed.first?.text.contains("Sources/Orchestrator.swift") == true)
        check("including the path the entry shares with the line behind it",
              typed.first?.text.contains("docs/landing.md") == true)
        check("and it names the route as the authority over its own prose",
              typed.first?.text.contains("GET /v1/orchestrator/landing-queue") == true)

        let again = OrchestratorLandingQueue.advance(project: repository.path, now: now,
                                                     deliver: deliver)
        check("a second advance does not type the same paragraph twice",
              landingQueueBody(again)?["delivered"] as? Bool == false
                  && landingQueueBody(again)?["reason"] as? String == "already_notified")
        expect("nothing further was sent", typed.count, 1)

        // The holder is arithmetic: the line in front lands, and the next one is at the front
        // without anybody writing that down.
        Orchestrator.mutateTaskForTesting("66666666-6666-4666-8666-666666666661") {
            $0.state = .success
            $0.landing = Orchestrator.Landing(
                state: .landed, target: "main", delivery: nil, ownerRootKey: "abcd1234",
                since: Date(timeIntervalSince1970: 7_000),
                commit: String(repeating: "b", count: 40), note: nil,
                landedAt: Date(timeIntervalSince1970: 7_500))
        }
        let handed = OrchestratorLandingQueue.advance(project: repository.path, now: now,
                                                      deliver: deliver)
        check("the next line is reached without a human relay",
              landingQueueBody(handed)?["delivered"] as? Bool == true)
        expect("and it is the line that was behind", typed.last?.session, behind)
        check("the notice says who it is after",
              typed.last?.text.contains(landingQueueDigest(ahead)) == true)

        let reordered = OrchestratorLandingQueue.setOrder(
            project: repository.path, keys: [landingQueueDigest(behind)],
            ifGeneration: nil, setBy: "clawdfather", now: now)
        check("re-ordering is accepted", landingQueueBody(reordered) != nil)
        let rearmed = OrchestratorLandingQueue.advance(project: repository.path, now: now,
                                                       deliver: deliver)
        check("a re-ordered queue is a new instruction even to the same holder",
              landingQueueBody(rearmed)?["delivered"] as? Bool == true)
        expect("so the holder hears it again", typed.count, 3)

        // The receipt is checked before anything is delivered, so a transport that has since
        // broken cannot turn an answered handoff back into an unanswered one.
        check("with a receipt standing, a broken transport is not even reached",
              landingQueueBody(OrchestratorLandingQueue.advance(
                  project: repository.path, now: now,
                  deliver: { _, _ in "No session named that." }))?["reason"] as? String
                  == "already_notified")
        expect("and still nothing more was typed", typed.count, 3)

        Orchestrator.forget()
        check("an empty repository has no slot to hand on",
              landingQueueRefusal(OrchestratorLandingQueue.advance(
                  project: repository.path, now: now, deliver: deliver))?.code == "queue_empty")
    }
}

group("the landing-time write set survives the isolation that empties the edit-time lease") {
    withLandingQueueFixture { repository, _ in
        let now = Date(timeIntervalSince1970: 9_000)
        var task = landingQueueTask(
            id: "77777777-7777-4777-8777-777777777777", title: "isolated and lossy",
            state: .briefed, root: "isolated-root", label: "isolated", projectDir: repository.path,
            created: 1_000,
            claims: ["Sources/Orchestrator.swift", "tools/check-architecture-boundaries.sh"])
        task.isolation = .worktree

        let warnings = OrchestratorLandingQueue.retainLandingPaths(&task)
        expect("the edit-time lease is still emptied for an isolated child", task.claims.count, 0)
        check("and the caller still gets the warning it always got",
              warnings.first?["code"] as? String == "claims_ignored_for_worktree")
        expect("the landing-time write set is kept",
               OrchestratorLandingQueue.retainedLandingPaths()[task.id]?.count, 2)

        Orchestrator.holdScheduleTaskForTesting(task)
        let row = landingQueueRow(
            OrchestratorLandingQueue.queueReply(project: repository.path, now: now),
            digest: landingQueueDigest("isolated-root"))
        check("so the queue can still say what this delivery will write",
              row?["paths"] as? [String]
                  == ["Sources/Orchestrator.swift", "tools/check-architecture-boundaries.sh"],
              "got \(String(describing: row?["paths"]))")

        // It survives the process, which is the whole difference from the dispatch response that
        // carried this list once and was thrown away.
        OrchestratorLandingQueue.forget()
        expect("and it is on disk rather than in one HTTP answer",
               OrchestratorLandingQueue.retainedLandingPaths()[task.id]?.count, 2)

        var shared = landingQueueTask(
            id: "77777777-7777-4777-8777-777777777778", title: "not isolated", state: .briefed,
            root: "shared-root", label: "shared", projectDir: repository.path, created: 2_000,
            claims: ["docs/api.md"])
        expect("a task in the shared checkout keeps its lease untouched",
               OrchestratorLandingQueue.retainLandingPaths(&shared).count, 0)
        check("and nothing is retained for it, because its claims already say so",
              OrchestratorLandingQueue.retainedLandingPaths()[shared.id] == nil)

        // `[]` is a sentence, not a blank: the shipped guide says it positively declares the task
        // read-only. These are the two rows that used to read identically — an isolated delivery
        // that writes two files, and a review that writes none.
        var isolated: [String: Any] = [:]
        OrchestratorLandingQueue.projectWriteSet(of: task, into: &isolated)
        var readOnly: [String: Any] = [:]
        var review = landingQueueTask(
            id: "77777777-7777-4777-8777-777777777779", title: "reads and judges", state: .briefed,
            root: "review-root", label: "review", projectDir: repository.path, created: 3_000)
        review.claimsDeclared = true
        OrchestratorLandingQueue.projectWriteSet(of: review, into: &readOnly)
        check("both still read as an empty lease, which is the contract nothing here changes",
              isolated["claims"] as? [String] == [] && readOnly["claims"] as? [String] == [])
        check("the erased lease says what it will write when it lands",
              isolated["landing_paths"] as? [String]
                  == ["Sources/Orchestrator.swift", "tools/check-architecture-boundaries.sh"],
              "got \(String(describing: isolated["landing_paths"]))")
        check("and the review, which really writes nothing, carries no such list",
              readOnly["landing_paths"] == nil)
        check("whether a write set was declared at all is now readable from outside",
              isolated["claims_declared"] as? Bool == true
                  && readOnly["claims_declared"] as? Bool == true)
        var undeclared: [String: Any] = [:]
        OrchestratorLandingQueue.projectWriteSet(of: landingQueueTask(
            id: "77777777-7777-4777-8777-77777777777a", title: "said nothing", state: .briefed,
            root: "silent-root", label: nil, projectDir: repository.path, created: 4_000),
            into: &undeclared)
        check("a task that declared nothing is told apart from one that declared emptiness",
              undeclared["claims_declared"] as? Bool == false && undeclared["claims"] == nil)
        check("and an inflight row still carries the key it has always carried",
              Orchestrator.inflightRow(review, visibility: .live,
                                       branches: Orchestrator.RepositoryBranches(),
                                       now: now)["claims"] as? [String] == [])
    }
}
}
