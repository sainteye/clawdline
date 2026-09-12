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

// MARK: - The order the broker derives for itself

/// One terminal delivery whose root declared where it lands: the ordinary ready-candidate shape.
/// `target: nil` is the entry that declared an obligation and no branch for it, and
/// `landing: false` is the one that declared nothing at all — the two shapes that must never be
/// read as `main`.
func landingQueueCandidateTask(id: String, root: String, repository: String,
                               created: TimeInterval, target: String?, landing: Bool = true,
                               claims: [String] = [], base: String = "base0",
                               isolated: Bool = true) -> Orchestrator.Task {
    landingQueueTask(
        id: id, title: "delivered \(id.prefix(8))", state: .success, root: root, label: root,
        projectDir: repository, created: created, claims: claims,
        landing: landing
            ? Orchestrator.Landing(state: .pending, target: target, delivery: nil,
                                   ownerRootKey: "abcd1234",
                                   since: Date(timeIntervalSince1970: created),
                                   commit: nil, note: nil)
            : nil,
        worktree: isolated ? landingQueueWorktree(taskID: id, base: base, repository: repository)
                           : nil)
}

func landingQueueBranch(_ id: String) -> String { "clawdline/task/\(id)" }

/// The queue's two pure functions against a described repository: no git, no registry, no
/// subprocess, so the same state really can be read twice and compared.
func landingQueueEntries(_ tasks: [Orchestrator.Task], repository: String,
                         heads: [String: String] = [:], known: Bool = true,
                         order: OrchestratorLandingQueue.Order = OrchestratorLandingQueue.Order(),
                         now: TimeInterval = 20_000) -> [OrchestratorLandingQueue.Entry] {
    let members = OrchestratorLandingQueue.members(
        tasks: tasks, repository: repository,
        branches: Orchestrator.RepositoryBranches(heads: heads, merged: [], known: known),
        retainedPaths: [:], deliveryPaths: [:], now: Date(timeIntervalSince1970: now))
    return OrchestratorLandingQueue.entries(members: members, order: order)
}

func landingQueueEntry(_ entries: [OrchestratorLandingQueue.Entry], _ digest: String)
    -> OrchestratorLandingQueue.Entry? {
    entries.first { $0.member.digest == digest }
}

/// What a second reader must see: the derived answer, and nothing that was measured at read time.
func landingQueueDerived(_ entries: [OrchestratorLandingQueue.Entry])
    -> [(digest: String, target: String?, order: Int?, of: Int?, turn: Bool)] {
    entries.map { ($0.member.digest, $0.member.target, $0.candidateOrder, $0.candidateGroup,
                   $0.turn) }
}

func landingQueueSameDerived(_ first: [OrchestratorLandingQueue.Entry],
                             _ second: [OrchestratorLandingQueue.Entry]) -> Bool {
    let left = landingQueueDerived(first), right = landingQueueDerived(second)
    return left.count == right.count && zip(left, right).allSatisfy {
        $0.0.digest == $0.1.digest && $0.0.target == $0.1.target && $0.0.order == $0.1.order
            && $0.0.of == $0.1.of && $0.0.turn == $0.1.turn
    }
}

group("the landing queue orders the ready candidates in one repository and target") {
    let repository = "/described/repository"
    let elsewhere = "/described/other-repository"
    let firstID = "aa000000-0000-4000-8000-00000000000a"
    let secondID = "bb000000-0000-4000-8000-00000000000b"
    // Two lines that are genuinely ready and share no path. Before this, the only thing said
    // about them was "unplaced, oldest first", which is an accident of two timestamps rather than
    // an answer, and the two roots asked each other instead.
    let first = landingQueueCandidateTask(
        id: firstID, root: "candidate-one", repository: repository, created: 1_000,
        target: "main", claims: ["Sources/First.swift"])
    let second = landingQueueCandidateTask(
        id: secondID, root: "candidate-two", repository: repository, created: 2_000,
        target: "main", claims: ["Sources/Second.swift"])
    let heads = [landingQueueBranch(firstID): "commit1", landingQueueBranch(secondID): "commit2"]
    let firstDigest = landingQueueDigest("candidate-one")
    let secondDigest = landingQueueDigest("candidate-two")

    let rows = landingQueueEntries([first, second], repository: repository, heads: heads)
    expect("both lines are in the queue", rows.count, 2)
    check("both are ready candidates",
          rows.allSatisfy { $0.member.isCandidate && $0.member.notCandidate == nil })
    expect("the older contributing work takes the turn",
           landingQueueEntry(rows, firstDigest)?.candidateOrder, 1)
    expect("and the other is second on the same target",
           landingQueueEntry(rows, secondDigest)?.candidateOrder, 2)
    check("both are told how many candidates they are one of",
          rows.allSatisfy { $0.candidateGroup == 2 })
    expect("exactly one entry has the shared-checkout turn", rows.filter(\.turn).count, 1)
    check("and it is the one the order put first",
          landingQueueEntry(rows, firstDigest)?.turn == true)
    // The second is not being told to wait for anything but the ref: no path is shared, so the
    // whole of preparation is open to it while the first holds the turn.
    check("the line behind shares no path with the one in front, so it may prepare",
          OrchestratorLandingQueue.contendedPaths(rows).isEmpty)

    // Deterministic means two readers of one state get one answer. The registry hands tasks back
    // in whatever order it holds them, so the reversed arm is the one that would catch a key that
    // was really just input order.
    check("reading the same state twice gives the same answer",
          landingQueueSameDerived(rows, landingQueueEntries([first, second],
                                                            repository: repository, heads: heads)))
    check("and so does reading it with the tasks in the other order",
          landingQueueSameDerived(rows, landingQueueEntries([second, first],
                                                            repository: repository, heads: heads)))

    // Same two lines, two repositories. Nothing compares them, and each holds its own turn —
    // which is the property that keeps unrelated work from waiting on this queue at all.
    let away = landingQueueCandidateTask(
        id: secondID, root: "candidate-two", repository: elsewhere, created: 2_000,
        target: "main", claims: ["Sources/Second.swift"])
    let here = landingQueueEntries([first, away], repository: repository, heads: heads)
    let there = landingQueueEntries([first, away], repository: elsewhere, heads: heads)
    expect("a queue read names only its own repository's lines", here.count, 1)
    expect("and the other repository names only the other", there.count, 1)
    check("each is first on its own target, because they are never ordered against each other",
          landingQueueEntry(here, firstDigest)?.candidateOrder == 1
              && landingQueueEntry(here, firstDigest)?.candidateGroup == 1
              && landingQueueEntry(there, secondDigest)?.candidateOrder == 1
              && landingQueueEntry(there, secondDigest)?.candidateGroup == 1)

    // Clawdfather's rule, and the whole entry is what it applies to: the delivery below is still
    // terminal and still unlanded, and one live sibling under the same root is enough.
    let sibling = landingQueueTask(
        id: "aa000000-0000-4000-8000-00000000000c", title: "still writing", state: .briefed,
        root: "candidate-one", label: "candidate-one", projectDir: repository, created: 3_000,
        claims: ["Sources/Third.swift"])
    let withLive = landingQueueEntries([first, second, sibling], repository: repository,
                                       heads: heads)
    expect("a line with one live task is still in the queue", withLive.count, 2)
    expect("but it is not a candidate", landingQueueEntry(withLive, firstDigest)?.member.notCandidate,
           OrchestratorLandingQueue.NotCandidate.liveWork)
    check("it holds no position and no turn",
          landingQueueEntry(withLive, firstDigest)?.candidateOrder == nil
              && landingQueueEntry(withLive, firstDigest)?.turn == false)
    check("and the line that is ready is first rather than second behind it",
          landingQueueEntry(withLive, secondDigest)?.candidateOrder == 1
              && landingQueueEntry(withLive, secondDigest)?.candidateGroup == 1
              && landingQueueEntry(withLive, secondDigest)?.turn == true)

    // An explicit order wins by being the first component of the key, not by a branch: the
    // coordinator names the younger line and the clock stops deciding.
    var placed = OrchestratorLandingQueue.Order()
    placed.keys = [secondDigest]
    placed.generation = 4
    let coordinated = landingQueueEntries([first, second], repository: repository, heads: heads,
                                          order: placed)
    expect("a coordinator's placed line takes the turn",
           landingQueueEntry(coordinated, secondDigest)?.candidateOrder, 1)
    expect("and the automatic answer falls in behind it",
           landingQueueEntry(coordinated, firstDigest)?.candidateOrder, 2)
    check("the coordinator's own fields are untouched by the derivation",
          placed.generation == 4 && placed.keys == [secondDigest])
}

group("a landing candidate the broker cannot prove stays visible, unordered and unguessed") {
    let repository = "/described/repository"
    let readyID = "cc000000-0000-4000-8000-00000000000c"
    let namelessID = "dd000000-0000-4000-8000-00000000000d"
    let silentID = "ee000000-0000-4000-8000-00000000000e"
    let goneID = "ff000000-0000-4000-8000-00000000000f"
    let ready = landingQueueCandidateTask(
        id: readyID, root: "provable", repository: repository, created: 1_000, target: "main",
        claims: ["Sources/Ready.swift"])
    // An obligation with no target: `Orchestrator.Landing.target` is optional and this is what
    // that costs. It is not an entry landing on `main`.
    let nameless = landingQueueCandidateTask(
        id: namelessID, root: "no-target", repository: repository, created: 2_000, target: nil,
        claims: ["Sources/Nameless.swift"])
    // A delivery nobody declared a landing for at all: unmerged, outstanding, and silent about
    // where it goes.
    let silent = landingQueueCandidateTask(
        id: silentID, root: "no-obligation", repository: repository, created: 3_000, target: nil,
        landing: false, claims: ["Sources/Silent.swift"])
    // A declared landing whose branch this repository's git did not answer for: what would land
    // cannot be named, so the entry is not ready however terminal its tasks are.
    let gone = landingQueueCandidateTask(
        id: goneID, root: "branch-gone", repository: repository, created: 4_000, target: "main",
        claims: ["Sources/Gone.swift"])
    let heads = [landingQueueBranch(readyID): "commit1", landingQueueBranch(namelessID): "commit2",
                 landingQueueBranch(silentID): "commit3"]
    let tasks = [ready, nameless, silent, gone]
    let rows = landingQueueEntries(tasks, repository: repository, heads: heads)
    func entry(_ root: String) -> OrchestratorLandingQueue.Entry? {
        landingQueueEntry(rows, landingQueueDigest(root))
    }

    expect("every line is still in the queue", rows.count, 4)
    expect("an obligation with no target is reported rather than placed",
           entry("no-target")?.member.notCandidate,
           OrchestratorLandingQueue.NotCandidate.targetUnreadable)
    check("and its target is empty rather than main",
          entry("no-target")?.member.target == nil)
    expect("a delivery that declared no landing at all is the same answer",
           entry("no-obligation")?.member.notCandidate,
           OrchestratorLandingQueue.NotCandidate.targetUnreadable)
    check("it is not guessed onto main either", entry("no-obligation")?.member.target == nil)
    expect("a delivery whose branch git did not answer for cannot be identified",
           entry("branch-gone")?.member.notCandidate,
           OrchestratorLandingQueue.NotCandidate.deliveryUnreadable)
    check("though its declared target is still reported, because that much was read",
          entry("branch-gone")?.member.target == "main")
    check("none of the three holds a position or a turn",
          ["no-target", "no-obligation", "branch-gone"].allSatisfy {
              entry($0)?.candidateOrder == nil && entry($0)?.candidateGroup == nil
                  && entry($0)?.turn == false
          })
    // And they do not consume a place: the one provable line is 1 of 1, not 1 of 4.
    check("the line that can be proved is ordered against the candidates and nobody else",
          entry("provable")?.candidateOrder == 1 && entry("provable")?.candidateGroup == 1
              && entry("provable")?.turn == true)

    // Two obligations under one root naming two branches. A queue that picked one would be
    // choosing a target on somebody's behalf, which is the whole thing this refuses to do.
    let split = landingQueueCandidateTask(
        id: "cc000000-0000-4000-8000-00000000001c", root: "provable", repository: repository,
        created: 5_000, target: "release", claims: ["Sources/Split.swift"])
    let divided = landingQueueEntries([ready, split], repository: repository,
                                      heads: heads.merging(
                                        [landingQueueBranch("cc000000-0000-4000-8000-00000000001c"):
                                            "commit4"], uniquingKeysWith: { first, _ in first }))
    expect("one root naming two targets is one entry", divided.count, 1)
    expect("and it is unordered rather than assigned to either",
           landingQueueEntry(divided, landingQueueDigest("provable"))?.member.notCandidate,
           OrchestratorLandingQueue.NotCandidate.targetUnreadable)
    check("with no target of its own",
          landingQueueEntry(divided, landingQueueDigest("provable"))?.member.target == nil)

    // git said nothing at all. Membership keeps its fail-safe direction — every line stays
    // visible — and ordering takes the opposite one, because the two mistakes cost different
    // amounts.
    let blind = landingQueueEntries(tasks, repository: repository, heads: [:], known: false)
    expect("an unreadable repository still shows every line", blind.count, 4)
    check("and orders none of them",
          blind.allSatisfy {
              $0.member.notCandidate == OrchestratorLandingQueue.NotCandidate.repositoryUnreadable
                  && $0.candidateOrder == nil && !$0.turn
          })
}

func landingQueueCandidateRecord(_ reply: Orchestrator.Reply, digest: String) -> [String: Any]? {
    landingQueueRow(reply, digest: digest)?["candidate"] as? [String: Any]
}

func landingQueueDerivedRecord(_ reply: Orchestrator.Reply) -> [String: Any] {
    (landingQueueBody(reply)?["order"] as? [String: Any])?["derived"] as? [String: Any] ?? [:]
}

group("the queue's answer says why an entry is a ready candidate or is not") {
    withLandingQueueFixture { repository, base in
        let now = Date(timeIntervalSince1970: 21_000)
        let firstID = "a1000000-0000-4000-8000-00000000000a"
        let secondID = "a2000000-0000-4000-8000-00000000000b"
        makeLandingQueueDelivery(in: repository, taskID: firstID, base: base,
                                 writing: ["Sources/First.swift"])
        makeLandingQueueDelivery(in: repository, taskID: secondID, base: base,
                                 writing: ["Sources/Second.swift"])
        Orchestrator.holdScheduleTaskForTesting(landingQueueCandidateTask(
            id: firstID, root: "answer-one", repository: repository.path, created: 1_000,
            target: "main", claims: ["Sources/First.swift"], base: base))
        Orchestrator.holdScheduleTaskForTesting(landingQueueCandidateTask(
            id: secondID, root: "answer-two", repository: repository.path, created: 2_000,
            target: "main", claims: ["Sources/Second.swift"], base: base))
        Orchestrator.holdScheduleTaskForTesting(landingQueueTask(
            id: "a3000000-0000-4000-8000-00000000000c", title: "children still out",
            state: .briefed, root: "answer-live", label: "answer-live",
            projectDir: repository.path, created: 3_000, claims: ["Sources/Third.swift"]))
        let first = landingQueueDigest("answer-one")
        let second = landingQueueDigest("answer-two")
        let live = landingQueueDigest("answer-live")

        let reply = OrchestratorLandingQueue.queueReply(project: repository.path, now: now)
        expect("every line is answered for", landingQueueRows(reply).count, 3)
        check("every row carries the whole candidate shape, present whether or not it has values",
              landingQueueRows(reply).allSatisfy { row in
                  let candidate = row["candidate"] as? [String: Any] ?? [:]
                  return Set(candidate.keys) == ["ready", "target", "order", "of", "turn", "why"]
              })
        let holder = landingQueueCandidateRecord(reply, digest: first) ?? [:]
        check("a ready candidate says it is one, on the target it read",
              holder["ready"] as? Bool == true && holder["target"] as? String == "main"
                  && holder["why"] is NSNull,
              "got \(holder)")
        check("with its place among the candidates sharing that target",
              holder["order"] as? Int == 1 && holder["of"] as? Int == 2
                  && holder["turn"] as? Bool == true)
        let behind = landingQueueCandidateRecord(reply, digest: second) ?? [:]
        check("the line behind is a candidate too, and knows it does not have the turn",
              behind["ready"] as? Bool == true && behind["order"] as? Int == 2
                  && behind["turn"] as? Bool == false)
        let working = landingQueueCandidateRecord(reply, digest: live) ?? [:]
        check("a line with live work says which fact stopped it, not nothing",
              working["ready"] as? Bool == false && working["why"] as? String == "live_work"
                  && working["order"] is NSNull && working["turn"] as? Bool == false,
              "got \(working)")

        let derived = landingQueueDerivedRecord(reply)
        expect("the order names the key it used",
               derived["basis"] as? String, "coordinator_order_then_oldest_work_then_digest")
        let targets = derived["targets"] as? [[String: Any]] ?? []
        expect("one row per target", targets.count, 1)
        check("naming the target, its order, and who holds the turn on it",
              targets.first?["target"] as? String == "main"
                  && targets.first?["keys"] as? [String] == [first, second]
                  && targets.first?["turn"] as? String == first,
              "got \(targets)")
        let unordered = derived["unordered"] as? [[String: Any]] ?? []
        check("and the lines it would not place, with the reason",
              unordered.count == 1 && unordered.first?["root_key"] as? String == live
                  && unordered.first?["why"] as? String == "live_work",
              "got \(unordered)")

        // The decision this slice rests on, measured rather than asserted: the derived answer is
        // read, so a read writes nothing. A stored order would have to move `generation`, and a
        // generation that moves on a GET re-arms a slot notice nobody asked for.
        check("nothing about this answer reached the store",
              !FileManager.default.fileExists(atPath: OrchestratorLandingQueue.storeURL.path))
        expect("so the coordinator's generation is still untouched",
               (landingQueueBody(reply)?["order"] as? [String: Any])?["generation"] as? Int, 0)

        let placed = OrchestratorLandingQueue.setOrder(
            project: repository.path, keys: [second, first], ifGeneration: nil,
            setBy: "clawdfather", now: now)
        check("an explicit order still wins over the automatic one",
              (landingQueueCandidateRecord(placed, digest: second) ?? [:])["order"] as? Int == 1
                  && (landingQueueCandidateRecord(placed, digest: first) ?? [:])["order"] as? Int
                      == 2)
        let stored = try? Data(contentsOf: OrchestratorLandingQueue.storeURL)
        let again = OrchestratorLandingQueue.queueReply(project: repository.path, now: now)
        let third = OrchestratorLandingQueue.queueReply(project: repository.path, now: now)
        check("and reading twice more moves neither the generation nor a byte of the store",
              (landingQueueBody(again)?["order"] as? [String: Any])?["generation"] as? Int == 1
                  && (landingQueueBody(third)?["order"] as? [String: Any])?["generation"] as? Int
                      == 1
                  && (try? Data(contentsOf: OrchestratorLandingQueue.storeURL)) == stored)
        func derivedKeys(_ reply: Orchestrator.Reply) -> [String]? {
            (landingQueueDerivedRecord(reply)["targets"] as? [[String: Any]])?
                .first?["keys"] as? [String]
        }
        check("with the same answer both times",
              derivedKeys(again) == [second, first] && derivedKeys(third) == [second, first],
              "got \(String(describing: derivedKeys(again))) then "
                  + "\(String(describing: derivedKeys(third)))")
    }
}

group("the slot notice says the turn is the shared checkout and not approval to land") {
    withLandingQueueFixture { repository, base in
        let now = Date(timeIntervalSince1970: 22_000)
        let aheadID = "b1000000-0000-4000-8000-00000000000a"
        let behindID = "b2000000-0000-4000-8000-00000000000b"
        makeLandingQueueDelivery(in: repository, taskID: aheadID, base: base,
                                 writing: ["Sources/Ahead.swift"])
        makeLandingQueueDelivery(in: repository, taskID: behindID, base: base,
                                 writing: ["Sources/Behind.swift"])
        Orchestrator.holdScheduleTaskForTesting(landingQueueCandidateTask(
            id: aheadID, root: "notice-ahead", repository: repository.path, created: 1_000,
            target: "main", claims: ["Sources/Ahead.swift", "docs/landing.md"], base: base))
        Orchestrator.holdScheduleTaskForTesting(landingQueueCandidateTask(
            id: behindID, root: "notice-behind", repository: repository.path, created: 2_000,
            target: "main", claims: ["Sources/Behind.swift"], base: base))
        var typed: [String] = []
        let delivered = OrchestratorLandingQueue.advance(
            project: repository.path, now: now,
            deliver: { _, text in typed.append(text); return nil })
        check("the front of the queue is reached", landingQueueBody(delivered)?["delivered"]
                  as? Bool == true)
        let notice = typed.first ?? ""
        // The failure this is for: a line whose review has just finished and whose correction has
        // not been dispatched is terminal-looking, so being called forward must not read as
        // "land it now".
        check("the notice says what the slot is",
              notice.contains("shared-checkout turn and not approval to land"),
              "got \(notice)")
        check("and that it is not a review, a passing suite, or an agreement",
              notice.contains("says nothing about review, tests, or anybody having agreed"))
        check("it names the ref update and the shared checkout as what the turn is for",
              notice.contains("update a ref on this repository and target")
                  && notice.contains("stage or commit here"))
        check("and it asks for the three re-reads before anything is staged",
              notice.contains("Re-read HEAD, git status and the index in your own checkout"))
        check("the holder is told it is a ready candidate and where it sits",
              notice.contains("Ready candidate for main: 1 of 2 on that target"),
              "got \(notice)")
        // The discipline this function already had, unchanged: the whole write set, and the route
        // rather than the prose as the authority.
        check("every path is still printed rather than summarised",
              notice.contains("Sources/Ahead.swift") && notice.contains("docs/landing.md"))
        check("and the route is still named as the authority over this sentence",
              notice.contains("GET /v1/orchestrator/landing-queue"))

        // A holder that is not a candidate is told so in the same message, because the two
        // questions — may I use the checkout, is this line ready — have two answers and a reader
        // holding one of them must not read it as the other.
        Orchestrator.holdScheduleTaskForTesting(landingQueueTask(
            id: "b3000000-0000-4000-8000-00000000000c", title: "still writing", state: .briefed,
            root: "notice-ahead", label: "notice-ahead", projectDir: repository.path,
            created: 3_000, claims: ["Sources/Live.swift"]))
        _ = OrchestratorLandingQueue.setOrder(
            project: repository.path, keys: [landingQueueDigest("notice-ahead")],
            ifGeneration: nil, setBy: "clawdfather", now: now)
        _ = OrchestratorLandingQueue.advance(
            project: repository.path, now: now,
            deliver: { _, text in typed.append(text); return nil })
        check("a holder that is not a ready candidate is told which fact stopped it",
              typed.last?.contains("not a ready candidate (live_work)") == true,
              "got \(String(describing: typed.last))")
        check("and that it still holds the checkout turn it was handed",
              typed.last?.contains("shared-checkout turn and not approval to land") == true)
    }
}

// MARK: - The inventory a dispatch has to have read

/// A described repository: no git, no registry, no subprocess. The three sections and the digest
/// are pure functions of these four inputs, which is what makes the volatility questions below
/// answerable at all — a test that had to run git could not hold "nothing meaningful changed"
/// still while it changed the clock.
func inventoryRows(_ tasks: [Orchestrator.Task],
                   repository: String,
                   heads: [String: String] = [:], merged: Set<String> = [],
                   known: Bool = true,
                   retained: [String: [String]] = [:],
                   now: TimeInterval = 10_000) -> [OrchestratorInventory.Row] {
    OrchestratorInventory.rows(
        repository: repository, tasks: tasks,
        branches: Orchestrator.RepositoryBranches(heads: heads, merged: merged, known: known),
        retainedPaths: retained, now: Date(timeIntervalSince1970: now))
}

func inventorySection(_ rows: [OrchestratorInventory.Row],
                      _ section: OrchestratorInventory.Section) -> [[String: Any]] {
    rows.filter { $0.section == section }.map { $0.payload }
}

/// One settled worktree task, described down to the three facts the droppable ladder reads.
func inventorySettled(id: String, repository: String, branch: String, base: String,
                      head: String?, dirty: Bool?, path: String,
                      landing: Orchestrator.Landing? = nil) -> Orchestrator.Task {
    var task = landingQueueTask(id: id, title: "settled \(id.prefix(8))", state: .success,
                                root: "settled-root", label: "settled",
                                projectDir: repository, created: 1_000, landing: landing)
    var worktree = Orchestrator.Worktree(path: path, branch: branch, base: base,
                                         repository: repository, cwd: path)
    worktree.head = head
    worktree.dirty = dirty
    task.isolation = .worktree
    task.worktree = worktree
    return task
}

group("the inventory answers three sections and every row names an action a route accepts") {
    let repository = "/described/repository"
    let onDisk = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-inventory-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: onDisk, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: onDisk) }

    var working = landingQueueTask(
        id: "aaaaaaaa-0000-4000-8000-000000000001", title: "still writing the guard",
        state: .briefed, root: "live-root", label: "the live line", projectDir: repository,
        created: 1_000, claims: ["tools/check-architecture-boundaries.sh", "Sources/Drop.swift"])
    working.worktree = nil

    var delivered = landingQueueTask(
        id: "bbbbbbbb-0000-4000-8000-000000000002", title: "delivered, never landed",
        state: .success, root: "delivered-root", label: "the finished line",
        projectDir: repository, created: 2_000,
        worktree: landingQueueWorktree(taskID: "bbbbbbbb-0000-4000-8000-000000000002",
                                       base: "base0", repository: repository))
    delivered.worktree?.head = "commit1"
    delivered.worktree?.dirty = false

    // A read-only audit whose root opened the obligation and never closed it: no claims, no
    // worktree, no target. This is the row `nothing_to_land` exists for.
    let audited = landingQueueTask(
        id: "cccccccc-0000-4000-8000-000000000003", title: "read and judged, wrote nothing",
        state: .success, root: "audit-root", label: "the audit", projectDir: repository,
        created: 3_000,
        landing: Orchestrator.Landing(state: .pending, target: nil, delivery: nil,
                                      ownerRootKey: "abcd1234",
                                      since: Date(timeIntervalSince1970: 3_100),
                                      commit: nil, note: nil))

    let rows = inventoryRows(
        [working, delivered, audited], repository: repository,
        heads: ["clawdline/task/bbbbbbbb-0000-4000-8000-000000000002": "commit1"])
    let live = inventorySection(rows, .live)
    let unlanded = inventorySection(rows, .unlanded)

    expect("the live line is the only live row", live.count, 1)
    expect("and it is named", live.first?["task"] as? String,
           "aaaaaaaa-0000-4000-8000-000000000001")
    expect("a live row asks the caller to coordinate rather than to dispatch over it",
           live.first?["do"] as? String, "coordinate_or_take_over")
    check("and carries the paths it holds",
          (live.first?["claims"] as? [String] ?? []).sorted()
              == ["Sources/Drop.swift", "tools/check-architecture-boundaries.sh"],
          "got \(String(describing: live.first?["claims"]))")

    expect("both finished lines are unlanded", unlanded.count, 2)
    let deliveredRow = unlanded.first { $0["task"] as? String
        == "bbbbbbbb-0000-4000-8000-000000000002" }
    expect("a delivery whose branch carries commits asks a person to decide",
           deliveredRow?["do"] as? String, "land_or_abandon")
    check("and says which stored fact took the cheaper close off the table",
          (deliveredRow?["why"] as? String ?? "").contains("commit"),
          "got \(String(describing: deliveredRow?["why"]))")
    let auditRow = unlanded.first { $0["task"] as? String
        == "cccccccc-0000-4000-8000-000000000003" }
    expect("an audit that wrote nothing is offered the close its route would admit",
           auditRow?["do"] as? String, "nothing_to_land")

    // The standard the `do` vocabulary is held to: it is `nothing_to_land` here exactly when
    // `POST /v1/orchestrator/tasks/:id/landing` would take it, asked with the route's own
    // predicate rather than with a second copy of it.
    for row in unlanded {
        let id = row["task"] as? String ?? ""
        let task = [working, delivered, audited].first { $0.id == id }
        let admitted = task.map {
            Orchestrator.nothingToLandAdmission(for: $0, declaredWritePaths: $0.claims).isAdmitted
        } ?? false
        check("row \(id.prefix(8)) advises only what the landing route would accept",
              (row["do"] as? String == "nothing_to_land") == admitted)
    }

    // Droppable: the three shapes, and the three refusals in front of them.
    let mergedTask = inventorySettled(
        id: "dddddddd-0000-4000-8000-000000000004", repository: repository,
        branch: "clawdline/task/dddddddd-0000-4000-8000-000000000004", base: "base0",
        head: "commit9", dirty: false, path: onDisk.path)
    let emptyTask = inventorySettled(
        id: "eeeeeeee-0000-4000-8000-000000000005", repository: repository,
        branch: "clawdline/task/eeeeeeee-0000-4000-8000-000000000005", base: "base0",
        head: "base0", dirty: false, path: onDisk.path)
    let orphanTask = inventorySettled(
        id: "ffffffff-0000-4000-8000-000000000006", repository: repository,
        branch: "clawdline/task/ffffffff-0000-4000-8000-000000000006", base: "base0",
        head: "commit7", dirty: false, path: onDisk.path)
    let dirtyTask = inventorySettled(
        id: "11111111-0000-4000-8000-000000000007", repository: repository,
        branch: "clawdline/task/11111111-0000-4000-8000-000000000007", base: "base0",
        head: "commit8", dirty: true, path: onDisk.path)
    let unreadTask = inventorySettled(
        id: "22222222-0000-4000-8000-000000000008", repository: repository,
        branch: "clawdline/task/22222222-0000-4000-8000-000000000008", base: "base0",
        head: "commit8", dirty: nil, path: onDisk.path)
    let recordedTask = inventorySettled(
        id: "33333333-0000-4000-8000-000000000009", repository: repository,
        branch: "clawdline/task/33333333-0000-4000-8000-000000000009", base: "base0",
        head: "commit8", dirty: false, path: onDisk.path,
        landing: Orchestrator.Landing(state: .landed, target: "main", delivery: nil,
                                      ownerRootKey: "abcd1234",
                                      since: Date(timeIntervalSince1970: 4_000),
                                      commit: "commit8", note: nil,
                                      landedAt: Date(timeIntervalSince1970: 4_100)))
    let settled = [mergedTask, emptyTask, orphanTask, dirtyTask, unreadTask, recordedTask]
    // Every branch but the orphan's exists; every branch but the recorded landing's is contained
    // by HEAD. That pair is what separates "git says the commits are in the tree" from "a root
    // wrote down that they are", which is the whole of the last check in this group.
    var heads: [String: String] = [:]
    var merged: Set<String> = []
    for task in settled {
        guard let worktree = task.worktree,
              task.id != "ffffffff-0000-4000-8000-000000000006" else { continue }
        heads[worktree.branch] = worktree.head ?? worktree.base
        if task.id != "33333333-0000-4000-8000-000000000009" { merged.insert(worktree.branch) }
    }
    let droppable = inventorySection(
        inventoryRows(settled, repository: repository, heads: heads, merged: merged), .droppable)
    func why(_ id: String) -> String? {
        (droppable.first { ($0["task"] as? String)?.hasPrefix(id) == true })?["why"] as? String
    }
    expect("a merged branch with commits of its own is disposable", why("dddddddd"),
           "merged_and_clean")
    expect("a branch that never received a commit is told apart from a landing", why("eeeeeeee"),
           "branch_empty")
    expect("a finished task whose branch git cannot find leaves a checkout to reclaim",
           why("ffffffff"), "checkout_orphaned")
    check("a dirty checkout is never named disposable", why("11111111") == nil)
    check("and neither is one this Mac has no dirty reading for", why("22222222") == nil)
    // The asymmetry that matters: a record says the obligation is closed, `branch_unmerged` says
    // this repository's HEAD does not contain the commits, and only one of those two can be
    // checked against the tree. Deleting on the strength of the other costs the work.
    check("a recorded landing does not make an unmerged branch safe to remove",
          why("33333333") == nil)
    check("every droppable row names disposal and nothing else",
          droppable.allSatisfy { $0["do"] as? String == "dispose" })

    check("git having said nothing empties this section rather than filling it",
          inventorySection(inventoryRows(settled, repository: repository, known: false),
                           .droppable).isEmpty)
}

group("the inventory generation moves on what changes a decision and on nothing else") {
    let repository = "/described/repository"
    let live = landingQueueTask(
        id: "aaaaaaaa-1111-4000-8000-000000000001", title: "the title it was dispatched with",
        state: .briefed, root: "live-root", label: "a label", projectDir: repository,
        created: 1_000, claims: ["docs/api.md"])
    func generation(_ tasks: [Orchestrator.Task], repository: String = "/described/repository",
                    heads: [String: String] = [:], merged: Set<String> = [],
                    now: TimeInterval = 10_000) -> String {
        OrchestratorInventory.generation(
            repository: repository,
            rows: inventoryRows(tasks, repository: repository, heads: heads, merged: merged,
                                now: now))
    }

    let sealed = generation([live])
    check("a generation is sixteen hex characters", sealed.count == 16
              && sealed.allSatisfy { "0123456789abcdef".contains($0) }, "got \(sealed)")
    expect("reading twice gives the same receipt", generation([live]), sealed)
    // The exclusion the whole mechanism rests on: a digest that ate the clock would expire while
    // its holder was composing the request it belongs to.
    expect("an hour of wall clock does not move it",
           generation([live], now: 13_600), sealed)

    var renamed = live
    renamed.title = "somebody rewrote the title"
    renamed.rootLabel = "and the label"
    expect("nor does prose somebody rewrote", generation([renamed]), sealed)

    var promoted = live
    promoted.state = .spawning
    expect("nor does a task moving through queued, spawning and briefed",
           generation([promoted]), sealed)

    var moreClaims = live
    moreClaims.claims = ["docs/api.md", "Sources/RemoteServer.swift"]
    check("but the write set a caller is deciding against does move it",
          generation([moreClaims]) != sealed)

    var finished = live
    finished.state = .success
    finished.isolation = .worktree
    finished.worktree = landingQueueWorktree(taskID: live.id, base: "base0",
                                             repository: repository)
    let branch = "clawdline/task/\(live.id)"
    check("and so does a row changing section, which is how a task finishing is noticed",
          generation([finished], heads: [branch: "commit1"]) != sealed)

    var head = finished
    head.worktree?.head = "commit1"
    check("a child committing again does not move it — the verdict is what a dispatcher reads",
          generation([head], heads: [branch: "commit2"])
              == generation([finished], heads: [branch: "commit1"]))

    check("two repositories with identical work do not share a receipt",
          generation([live], repository: "/described/other") != sealed)
    check("an empty repository still has a generation, and it is its own",
          generation([]).count == 16 && generation([]) != sealed)
}

group("a dispatch that did not read the inventory is refused and handed the inventory") {
    withLandingQueueFixture { repository, base in
        let now = Date(timeIntervalSince1970: 12_000)
        Orchestrator.holdScheduleTaskForTesting(landingQueueTask(
            id: "44444444-4444-4444-8444-444444444441", title: "somebody is already on this",
            state: .briefed, root: "occupying-root", label: "the line already here",
            projectDir: repository.path, created: 1_000, claims: ["Sources/RemoteServer.swift"]))

        guard let read = OrchestratorInventory.inventory(project: repository.path, now: now),
              let generation = read.payload["generation"] as? String else {
            check("the inventory answers for the fixture repository", false)
            return
        }
        expect("which is the row a dispatcher would have collided with",
               (read.payload["live"] as? [[String: Any]])?.count, 1)

        // A task.json that names no Root: `POST /v1/orchestrator/tasks` refuses that at a door
        // of its own, well before a terminal starter can run — so the green arm below proves the
        // inventory let the body through without this test opening a tab on somebody's Mac.
        let taskID = UUID().uuidString.lowercased()
        let directory = Orchestrator.root.appendingPathComponent(taskID, isDirectory: true)
        try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        try! JSONSerialization.data(withJSONObject: [
            "clawdline_protocol": 1, "task_id": taskID, "kind": "code", "assistant": "claude",
            "project_dir": repository.path, "title": "the work nobody looked before starting",
            "instructions": "do the work", "timeout_minutes": 30,
        ]).write(to: directory.appendingPathComponent("task.json"), options: .atomic)

        let secret = String(repeating: "a1", count: 32)
        let token = Orchestrator.dispatchToken()
        func post(_ generation: String?) -> RemoteServer.Response {
            var body = "{\"task_id\":\"\(taskID)\",\"secret\":\"\(secret)\""
            if let generation { body += ",\"inventory_generation\":\"\(generation)\"" }
            return RemoteServer.shared.route(remoteRequest(
                "POST", "/v1/orchestrator/tasks",
                headers: ["Content-Type": "application/json",
                          "X-Clawdline-Orchestrator": token],
                body: body + "}"))
        }

        let unread = post(nil)
        expect("a dispatch carrying no receipt is refused", unread.status, 409)
        expect("and typed as the read it skipped", remoteErrorCode(unread), "stale_inventory")
        let refusal = ((try? JSONSerialization.jsonObject(with: unread.body)) as? [String: Any])?[
            "error"] as? [String: Any] ?? [:]
        expect("the refusal carries the generation it wanted",
               refusal["inventory_generation"] as? String, generation)
        let handed = refusal["inventory"] as? [String: Any] ?? [:]
        check("and the whole inventory with it, so one round trip is enough to recover",
              handed["live"] is [[String: Any]] && handed["unlanded"] is [[String: Any]]
                  && handed["droppable"] is [[String: Any]]
                  && handed["generation"] as? String == generation)
        check("a refused dispatch registers no task", Orchestrator.record(id: taskID) == nil)

        expect("a receipt from another tree is refused the same way",
               remoteErrorCode(post("0000000000000000")), "stale_inventory")

        // The differential. The only thing that changes between this arm and the first is the
        // value of `inventory_generation`; what comes back is this task's own next refusal,
        // which is not this one.
        let looked = post(generation)
        check("with the current receipt the dispatch is no longer refused for not looking",
              remoteErrorCode(looked) != "stale_inventory",
              "got \(remoteErrorCode(looked))")
        expect("and reaches the door it was always going to be judged at",
               remoteErrorCode(looked), "root_session_required")
        check("which still opened nothing", Orchestrator.record(id: taskID) == nil)

        // A receipt goes stale when the answer moves, which is the whole of what it is for.
        Orchestrator.holdScheduleTaskForTesting(landingQueueTask(
            id: "44444444-4444-4444-8444-444444444442", title: "and now a second line",
            state: .briefed, root: "second-root", label: "arrived since you looked",
            projectDir: repository.path, created: 2_000, claims: ["docs/api.md"]))
        expect("a receipt taken before another line appeared no longer opens the door",
               remoteErrorCode(post(generation)), "stale_inventory")

        // Resending a task the registry already holds stays idempotent: a retry after a timeout
        // must not become a refusal because the repository moved between the two sends.
        var held = landingQueueTask(
            id: "44444444-4444-4444-8444-444444444443", title: "already dispatched",
            state: .briefed, root: "occupying-root", label: nil, projectDir: repository.path,
            created: 3_000, claims: ["docs/api.md"])
        held.secretHash = Orchestrator.hash(ofSecret: secret)
        Orchestrator.holdScheduleTaskForTesting(held)
        check("a task the registry already holds has no inventory opinion at all",
              OrchestratorInventory.dispatchAdmission(taskID: held.id, supplied: nil,
                                                      now: now) == nil)
        check("and neither has one whose task.json this Mac cannot read",
              OrchestratorInventory.dispatchAdmission(
                  taskID: UUID().uuidString.lowercased(), supplied: nil, now: now) == nil)
    }
}


group("an isolated child is answered about the repository it was cut from, not its own checkout") {
    // **A reading with no subject.** `git rev-parse --show-toplevel` run inside a linked worktree
    // answers with that disposable checkout, while every task in the registry is filed under the
    // repository it was cut from — so the filter in `inflightRecords` kept nothing and the child
    // was told `200`, its own path as `repository`, and an empty board. `CHILD.md` sends every
    // child through this door before it starts work, and an empty board is exactly what "nobody
    // else is working here" looks like. The briefing warns about the call failing; nothing warned
    // about it succeeding emptily.
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    let fixture = makeLandingRepository()
    let base = OrchestratorDraft.canonicalFilesystemPath(fixture.url.path)
    let checkout = fixture.url.deletingLastPathComponent()
        .appendingPathComponent("clawdline-child-\(UUID().uuidString)", isDirectory: true)
    defer {
        _ = testGit(["worktree", "remove", "--force", checkout.path], cwd: fixture.url)
        try? FileManager.default.removeItem(at: fixture.url)
    }
    expect("the fixture cuts a linked worktree the way an isolated dispatch does",
           testGit(["worktree", "add", "-q", "-b", "clawdline/task/child", checkout.path, "main"],
                   cwd: fixture.url).status, 0)
    let childCheckout = OrchestratorDraft.canonicalFilesystemPath(checkout.path)
    // The fact the defect rests on, measured here rather than assumed: git in a linked worktree
    // really does answer with the checkout.
    check("git inside the checkout says the checkout, which is why this cannot be asked of git alone",
          OrchestratorDraft.canonicalFilesystemPath(
            testGit(["rev-parse", "--show-toplevel"], cwd: checkout).output) == childCheckout
              && childCheckout != base)

    let peerID = "77777777-1111-2222-3333-444444444444"
    let childID = "88888888-1111-2222-3333-444444444444"
    let secret = String(repeating: "d4", count: 32)
    let peer = Orchestrator.Task(
        id: peerID, state: .briefed, kind: "custom", title: "the line already open here",
        assistant: .claude, projectDir: base, timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 100), rootSessionId: "root-peer",
        rootLabel: "root of the other line", claims: ["Sources/RemoteServer.swift"],
        claimsDeclared: true, secretHash: Orchestrator.hash(ofSecret: secret))
    Orchestrator.holdScheduleTaskForTesting(peer)
    var child = Orchestrator.Task(
        id: childID, state: .briefed, kind: "code", title: "the isolated child asking",
        assistant: .claude, projectDir: base, timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 200), rootSessionId: "root-child",
        rootLabel: "root of this line", claims: [], claimsDeclared: true,
        secretHash: Orchestrator.hash(ofSecret: secret))
    child.isolation = .worktree
    child.worktree = Orchestrator.Worktree(
        path: childCheckout, branch: "clawdline/task/child", base: fixture.commit,
        repository: base, cwd: childCheckout)
    Orchestrator.holdScheduleTaskForTesting(child)

    func board(_ reply: Orchestrator.Reply) -> (repository: String, ids: [String]) {
        guard case .ok(let body) = reply else { return ("", []) }
        let rows = body["inflight"] as? [[String: Any]] ?? []
        return (body["repository"] as? String ?? "", rows.compactMap { $0["id"] as? String })
    }
    let fromCheckout = board(Orchestrator.inflightReply(taskID: childID, secret: secret,
                                                        now: Date(timeIntervalSince1970: 300)))
    let fromRepository = board(Orchestrator.inflightReply(taskID: peerID, secret: secret,
                                                          now: Date(timeIntervalSince1970: 300)))
    // The pair differs in exactly one thing: where the reader is standing.
    expect("one question asked from the linked checkout and from the repository has one answer",
           Orchestrator.inflightRepository(childCheckout), Orchestrator.inflightRepository(base))
    expect("and that answer is the repository, which is where the registry files its work",
           Orchestrator.inflightRepository(childCheckout), base)
    expect("so an isolated child's own door names the repository it was cut from",
           fromCheckout.repository, base)
    expect("and the board it is given is not empty: the line already open here is on it",
           fromCheckout.ids, [peerID])
    expect("a task standing in the repository itself was always answered, and still is",
           fromRepository.ids, [childID])
}
}
