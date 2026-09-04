import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

@discardableResult
func testGit(_ arguments: [String], cwd: URL) -> (status: Int32, output: String) {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/git")
    process.arguments = arguments
    process.currentDirectoryURL = cwd
    let pipe = Pipe()
    process.standardOutput = pipe
    process.standardError = pipe
    do { try process.run() } catch { return (-1, "\(error)") }
    let data = pipe.fileHandleForReading.readDataToEndOfFile()
    process.waitQuietly()
    return (process.terminationStatus,
            String(decoding: data, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines))
}

func makeLandingRepository() -> (url: URL, commit: String) {
    let url = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-landing-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
    expect("the landing fixture repository initializes",
           testGit(["init", "-q", "-b", "main"], cwd: url).status, 0)
    try! Data("verified\n".utf8).write(to: url.appendingPathComponent("receipt.txt"))
    expect("the landing fixture stages its commit",
           testGit(["add", "receipt.txt"], cwd: url).status, 0)
    expect("the landing fixture commits", testGit([
        "-c", "user.name=Clawdline Tests", "-c", "user.email=tests@clawdline.invalid",
        "commit", "-qm", "verified target",
    ], cwd: url).status, 0)
    let commit = testGit(["rev-parse", "HEAD"], cwd: url).output
    return (url, commit)
}



func runOrchestratorLandingTests() {
group("landing records enforce the root-owned state machine and keep idempotent receipts") {
    let repository = makeLandingRepository()
    defer { try? FileManager.default.removeItem(at: repository.url) }
    expect("the fixture has a second named local target",
           testGit(["branch", "release/main"], cwd: repository.url).status, 0)
    expect("the fixture opens an unrelated history",
           testGit(["switch", "-q", "--orphan", "unrelated"], cwd: repository.url).status, 0)
    expect("the unrelated history commits",
           testGit(["-c", "user.name=Clawdline Tests", "-c",
                    "user.email=tests@clawdline.invalid", "commit", "--allow-empty", "-qm",
                    "unrelated"], cwd: repository.url).status, 0)
    let unrelatedCommit = testGit(["rev-parse", "HEAD"], cwd: repository.url).output
    expect("the fixture returns to main",
           testGit(["switch", "-q", "main"], cwd: repository.url).status, 0)
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    let id = "10101010-2020-3030-4040-505050505050"
    let secret = String(repeating: "a1", count: 32)
    let root = "landing-root-session"
    var made = Orchestrator.Task(
        id: id, state: .briefed, kind: "custom", title: "landing state machine",
        assistant: .codex, projectDir: repository.url.path, timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 1), rootSessionId: root,
        claims: ["Sources/Landing.swift"], claimsDeclared: true,
        secretHash: Orchestrator.hash(ofSecret: secret))
    made.claimKeys = OrchestratorDraft.freezeClaims(made.claims, projectDir: made.projectDir)
    Orchestrator.holdScheduleTaskForTesting(made)

    func landing(_ reply: Orchestrator.Reply) -> [String: Any]? {
        guard case .ok(let body) = reply,
              let task = body["task"] as? [String: Any] else { return nil }
        return task["landing"] as? [String: Any]
    }

    let first = Orchestrator.updateLanding(
        taskID: id, secret: secret,
        raw: ["state": "pending", "target": "main", "delivery": "review/landing",
              "note": "awaiting a safe shared tree"],
        now: Date(timeIntervalSince1970: 10))
    let pending = landing(first)
    check("pending is accepted before final landing",
          pending?["state"] as? String == "pending"
              && pending?["target"] as? String == "main"
              && pending?["delivery"] as? String == "review/landing"
              && pending?["owner_root_key"] as? String == OrchestratorDraft.rootKeyDigest(root)
              && pending?["since"] as? Int == 10)

    let repeated = Orchestrator.updateLanding(
        taskID: id, secret: secret,
        raw: ["state": "pending", "target": "release/main", "delivery": "review/final"],
        now: Date(timeIntervalSince1970: 99))
    check("repeating pending preserves since while filling mutable receipt fields",
          landing(repeated)?["since"] as? Int == 10
              && landing(repeated)?["target"] as? String == "release/main"
              && landing(repeated)?["delivery"] as? String == "review/final"
              && landing(repeated)?["note"] as? String == "awaiting a safe shared tree")

    let supplemented = Orchestrator.updateLanding(
        taskID: id, secret: secret,
        raw: ["state": "pending", "note": "review says safe to land"],
        now: Date(timeIntervalSince1970: 100))
    check("repeating pending may add a later note without restarting its age",
          landing(supplemented)?["since"] as? Int == 10
              && landing(supplemented)?["note"] as? String == "review says safe to land")

    if case .refused(let status, let code, _, _) = Orchestrator.updateLanding(
        taskID: id, secret: "", orchestratorToken: Orchestrator.dispatchToken(),
        raw: ["state": "landed", "commit": repository.commit],
        now: Date(timeIntervalSince1970: 20)) {
        check("a live child cannot be declared landed", status == 409 && code == "not_terminal")
    } else {
        check("a live child cannot be declared landed", false, "it answered ok")
    }

    Orchestrator.finalize(id, as: .success, summary: "delivered")
    if case .refused(let status, let code, _, _) = Orchestrator.updateLanding(
        taskID: id, secret: secret,
        raw: ["state": "landed", "commit": repository.commit],
        now: Date(timeIntervalSince1970: 25)) {
        check("the child task secret cannot assert a landed target",
              status == 403 && code == "forbidden")
    } else {
        check("the child task secret cannot assert a landed target", false, "it answered ok")
    }
    if case .refused(let status, let code, _, _) = Orchestrator.updateLanding(
        taskID: id, secret: "", orchestratorToken: Orchestrator.dispatchToken(),
        raw: ["state": "landed", "commit": "not-a-commit"],
        now: Date(timeIntervalSince1970: 26)) {
        check("arbitrary non-empty commit text is not landing evidence",
              status == 409 && code == "unverified_landing")
    } else {
        check("arbitrary non-empty commit text is not landing evidence", false, "it answered ok")
    }
    if case .refused(let status, let code, _, _) = Orchestrator.updateLanding(
        taskID: id, secret: "", orchestratorToken: Orchestrator.dispatchToken(),
        raw: ["state": "landed", "commit": unrelatedCommit],
        now: Date(timeIntervalSince1970: 27)) {
        check("a real commit outside the named target is not landing evidence",
              status == 409 && code == "unverified_landing")
    } else {
        check("a real commit outside the named target is not landing evidence", false,
              "it answered ok")
    }
    let landedReply = Orchestrator.updateLanding(
        taskID: id, secret: "", orchestratorToken: Orchestrator.dispatchToken(),
        raw: ["state": "landed", "commit": repository.commit],
        now: Date(timeIntervalSince1970: 30))
    let landed = landing(landedReply)
    check("pending advances to landed only after the task is terminal and records its commit",
          landed?["state"] as? String == "landed"
              && landed?["commit"] as? String == repository.commit
              && landed?["since"] as? Int == 10
              && landed?["landed_at"] as? Int == 30
              && landed?["verification_origin"] as? String == "local_target_branch"
              && landed?["verified_commit"] as? String == repository.commit
              && landed?["verified_target_commit"] as? String == repository.commit)

    let landedAgain = Orchestrator.updateLanding(
        taskID: id, secret: "", orchestratorToken: Orchestrator.dispatchToken(),
        raw: ["state": "landed", "commit": "changed"],
        now: Date(timeIntervalSince1970: 40))
    check("repeating landed cannot rewrite its commit receipt",
          landing(landedAgain)?["commit"] as? String == repository.commit)
    if case .refused(let status, let code, _, _) = Orchestrator.updateLanding(
        taskID: id, secret: secret, raw: ["state": "pending"]) {
        check("landed cannot move back to pending", status == 409 && code == "invalid_transition")
    } else {
        check("landed cannot move back to pending", false, "it answered ok")
    }

    let abandonedID = "20202020-3030-4040-5050-606060606060"
    let abandonedSecret = String(repeating: "f6", count: 32)
    var abandonedTask = Orchestrator.Task(
        id: abandonedID, state: .briefed, kind: "custom", title: "abandoned delivery",
        assistant: .codex, projectDir: repository.url.path, timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 1), rootSessionId: root,
        claims: ["Sources/Abandoned.swift"], claimsDeclared: true,
        secretHash: Orchestrator.hash(ofSecret: abandonedSecret))
    abandonedTask.claimKeys = OrchestratorDraft.freezeClaims(
        abandonedTask.claims, projectDir: abandonedTask.projectDir)
    Orchestrator.holdScheduleTaskForTesting(abandonedTask)
    _ = Orchestrator.updateLanding(
        taskID: abandonedID, secret: abandonedSecret, raw: ["state": "pending"],
        now: Date(timeIntervalSince1970: 50))
    if case .refused(let status, let code, _, _) = Orchestrator.updateLanding(
        taskID: abandonedID, secret: abandonedSecret, raw: ["state": "abandoned"]) {
        check("a live child cannot be declared abandoned", status == 409 && code == "not_terminal")
    } else {
        check("a live child cannot be declared abandoned", false, "it answered ok")
    }
    Orchestrator.finalize(abandonedID, as: .success, summary: "delivered but declined")
    let abandonedReply = Orchestrator.updateLanding(
        taskID: abandonedID, secret: abandonedSecret,
        raw: ["state": "abandoned", "note": "superseded elsewhere"],
        now: Date(timeIntervalSince1970: 60))
    check("pending advances to abandoned after the task is terminal",
          landing(abandonedReply)?["state"] as? String == "abandoned")
    for state in ["pending", "landed"] {
        var raw: [String: Any] = ["state": state]
        if state == "landed" { raw["commit"] = repository.commit }
        if case .refused(let status, let code, _, _) = Orchestrator.updateLanding(
            taskID: abandonedID,
            secret: state == "landed" ? "" : abandonedSecret,
            orchestratorToken: state == "landed" ? Orchestrator.dispatchToken() : nil,
            raw: raw) {
            check("abandoned cannot move to \(state)",
                  status == 409 && code == "invalid_transition")
        } else {
            check("abandoned cannot move to \(state)", false, "it answered ok")
        }
    }
}

group("landing verification survives a disposed worktree but refuses a repository mismatch") {
    let repository = makeLandingRepository()
    let otherRepository = makeLandingRepository()
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        try? FileManager.default.removeItem(at: repository.url)
        try? FileManager.default.removeItem(at: otherRepository.url)
        Orchestrator.forget()
    }
    Orchestrator.forget()

    func isolatedTask(_ id: String, projectDir: String,
                      durableRepository: URL, secret: String,
                      worktreeRepository: String? = nil) -> Orchestrator.Task {
        let repositoryPath = worktreeRepository ?? durableRepository.path
        let worktreePath = OrchestratorDraft.worktreePath(
            project: repositoryPath, taskID: id)!
        var task = Orchestrator.Task(
            id: id, state: .success, kind: "custom", title: "durable landing repository",
            assistant: .codex, projectDir: projectDir, timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: 1), rootSessionId: "landing-repository-root",
            rootAssistant: .codex, claims: ["Sources/Landing.swift"], claimsDeclared: true,
            secretHash: Orchestrator.hash(ofSecret: secret))
        task.finishedAt = Date(timeIntervalSince1970: 2)
        task.repositoryCommonDir = durableRepository
            .appendingPathComponent(".git", isDirectory: true).path
        task.isolation = .worktree
        var worktree = Orchestrator.Worktree(
            path: worktreePath, branch: "clawdline/task/\(id)", base: repository.commit,
            repository: repositoryPath, cwd: worktreePath)
        worktree.repositoryCommonDir = nil
        task.worktree = worktree
        return task
    }

    let disposedID = "31313131-4141-5151-6161-717171717171"
    let disposedSecret = String(repeating: "a7", count: 32)
    let deletedRepositoryPath = repository.url.deletingLastPathComponent()
        .appendingPathComponent("deleted-repository-\(UUID().uuidString)", isDirectory: true).path
    let disposedPath = OrchestratorDraft.worktreePath(
        project: deletedRepositoryPath, taskID: disposedID)!
    check("the landing fixture's isolated cwd is genuinely absent",
          !FileManager.default.fileExists(atPath: disposedPath))
    Orchestrator.holdScheduleTaskForTesting(isolatedTask(
        disposedID, projectDir: disposedPath,
        durableRepository: repository.url, secret: disposedSecret,
        worktreeRepository: deletedRepositoryPath))
    Orchestrator.saveForTesting()
    let storedRoot = (try? JSONSerialization.jsonObject(with: Data(contentsOf: store)))
        as? [String: Any]
    let storedDisposed = (storedRoot?["tasks"] as? [[String: Any]])?
        .first { $0["id"] as? String == disposedID }
    check("repository identity is persisted at task scope, not only inside worktree metadata",
          storedDisposed?["repository_common_dir"] as? String
                == repository.url.appendingPathComponent(".git", isDirectory: true).path
            && (storedDisposed?["worktree"] as? [String: Any])?["repository_common_dir"] == nil)
    Orchestrator.forget()
    _ = Orchestrator.updateLanding(
        taskID: disposedID, secret: disposedSecret,
        raw: ["state": "pending", "target": "main"])
    let recovered = Orchestrator.updateLanding(
        taskID: disposedID, secret: "", orchestratorToken: Orchestrator.dispatchToken(),
        raw: ["state": "landed", "target": "main", "commit": repository.commit])
    if case .ok(let body) = recovered,
       let row = (body["task"] as? [String: Any])?["landing"] as? [String: Any] {
        check("a disposed worktree closes through its persisted canonical repository receipt",
              row["state"] as? String == "landed"
                && row["verified_commit"] as? String == repository.commit)
    } else {
        check("a disposed worktree closes through its persisted canonical repository receipt",
              false, "\(recovered)")
    }

    let mismatchID = "32323232-4242-5252-6262-727272727272"
    let mismatchSecret = String(repeating: "b8", count: 32)
    Orchestrator.holdScheduleTaskForTesting(isolatedTask(
        mismatchID, projectDir: repository.url.path,
        durableRepository: otherRepository.url, secret: mismatchSecret))
    _ = Orchestrator.updateLanding(
        taskID: mismatchID, secret: mismatchSecret,
        raw: ["state": "pending", "target": "main"])
    if case .refused(let status, let code, _, _) = Orchestrator.updateLanding(
        taskID: mismatchID, secret: "", orchestratorToken: Orchestrator.dispatchToken(),
        raw: ["state": "landed", "target": "main", "commit": otherRepository.commit]) {
        check("a readable task cwd from another repository fails closed",
              status == 409 && code == "unverified_landing")
    } else {
        check("a readable task cwd from another repository fails closed", false,
              "the mismatched repository was accepted")
    }

    let movedID = "33333333-4343-5353-6363-737373737373"
    let movedSecret = String(repeating: "ba", count: 32)
    var moved = Orchestrator.Task(
        id: movedID, state: .success, kind: "custom", title: "moved repository receipt",
        assistant: .codex, projectDir: repository.url.path, timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 1), finishedAt: Date(timeIntervalSince1970: 2),
        rootSessionId: "moved-repository-root", rootAssistant: .codex,
        claims: ["Sources/Landing.swift"], claimsDeclared: true,
        secretHash: Orchestrator.hash(ofSecret: movedSecret))
    moved.repositoryCommonDir = repository.url.deletingLastPathComponent()
        .appendingPathComponent("old-location/.git", isDirectory: true).path
    Orchestrator.holdScheduleTaskForTesting(moved)
    _ = Orchestrator.updateLanding(
        taskID: movedID, secret: movedSecret,
        raw: ["state": "pending", "target": "main"])
    let movedReply = Orchestrator.updateLanding(
        taskID: movedID, secret: "", orchestratorToken: Orchestrator.dispatchToken(),
        raw: ["state": "landed", "target": "main", "commit": repository.commit])
    if case .ok = movedReply {
        check("a stale stored path falls back to independently derived same-repository evidence",
              true)
    } else {
        check("a stale stored path falls back to independently derived same-repository evidence",
              false, "\(movedReply)")
    }

    let unboundID = "34343434-4444-5454-6464-747474747474"
    let unboundSecret = String(repeating: "cb", count: 32)
    var unbound = Orchestrator.Task(
        id: unboundID, state: .success, kind: "custom", title: "unbound stale receipt",
        assistant: .codex, projectDir: "/tmp/unowned-deleted-project-\(UUID().uuidString)",
        timeoutMinutes: 30, created: Date(timeIntervalSince1970: 1),
        finishedAt: Date(timeIntervalSince1970: 2), rootSessionId: "unbound-root",
        rootAssistant: .codex, claims: ["Sources/Landing.swift"], claimsDeclared: true,
        secretHash: Orchestrator.hash(ofSecret: unboundSecret))
    unbound.repositoryCommonDir = moved.repositoryCommonDir
    Orchestrator.holdScheduleTaskForTesting(unbound)
    _ = Orchestrator.updateLanding(
        taskID: unboundID, secret: unboundSecret,
        raw: ["state": "pending", "target": "main"])
    if case .refused(let status, let code, _, _) = Orchestrator.updateLanding(
        taskID: unboundID, secret: "", orchestratorToken: Orchestrator.dispatchToken(),
        raw: ["state": "landed", "target": "main", "commit": repository.commit]) {
        check("a stale stored path without independent repository evidence fails closed",
              status == 409 && code == "unverified_landing")
    } else {
        check("a stale stored path without independent repository evidence fails closed", false)
    }

    // These are the two production row ids that exposed the legacy shape: isolation was absent
    // (therefore `.none`) and project_dir named a broker-owned worktree that was later disposed.
    // A separate retained worktree receipt for the same broker repository slug is bounded,
    // authoritative local evidence; no basename search or caller-supplied repository participates.
    let staleProjectDir = OrchestratorDraft.worktreePath(
        project: repository.url.path,
        taskID: "69b79f6d-8b70-4c43-bdb2-3d6590d79113")!
    check("the legacy non-isolated project_dir is genuinely disposed",
          !FileManager.default.fileExists(atPath: staleProjectDir))
    let evidenceID = "71717171-8181-9191-a1a1-b1b1b1b1b1b1"
    var evidence = isolatedTask(
        evidenceID, projectDir: repository.url.path,
        durableRepository: repository.url, secret: String(repeating: "c9", count: 32))
    evidence.state = .briefed
    evidence.landing = nil
    Orchestrator.holdScheduleTaskForTesting(evidence)
    for (id, byte) in [
        ("1a060155-998c-42c5-b9e0-cb05d5bf7893", "d1"),
        ("1cb35b50-2613-43f2-b4d8-3ab2f806d15d", "e2"),
    ] {
        let secret = String(repeating: byte, count: 32)
        var legacy = Orchestrator.Task(
            id: id, state: .success, kind: "custom", title: "legacy pending row",
            assistant: .codex, projectDir: staleProjectDir, timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: 1), finishedAt: Date(timeIntervalSince1970: 2),
            rootSessionId: "legacy-landing-root", rootAssistant: .codex,
            claims: ["Sources/Landing.swift"], claimsDeclared: true,
            secretHash: Orchestrator.hash(ofSecret: secret))
        legacy.isolation = .none
        Orchestrator.holdScheduleTaskForTesting(legacy)
        _ = Orchestrator.updateLanding(
            taskID: id, secret: secret, raw: ["state": "pending", "target": "main"])
        let reply = Orchestrator.updateLanding(
            taskID: id, secret: "", orchestratorToken: Orchestrator.dispatchToken(),
            raw: ["state": "landed", "target": "main", "commit": repository.commit])
        if case .ok(let body) = reply,
           let row = (body["task"] as? [String: Any])?["landing"] as? [String: Any] {
            check("legacy pending row \(id) closes from bounded same-repository evidence",
                  row["state"] as? String == "landed"
                    && row["verified_commit"] as? String == repository.commit)
        } else {
            check("legacy pending row \(id) closes from bounded same-repository evidence",
                  false, "\(reply)")
        }
    }
}

group("landing routes accept a matching task or machine credential and list pending obligations") {
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
    let firstID = "11111111-2222-3333-4444-555555555555"
    let secondID = "66666666-7777-8888-9999-aaaaaaaaaaaa"
    let thirdID = "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
    let firstSecret = String(repeating: "b2", count: 32)
    let secondSecret = String(repeating: "c3", count: 32)
    let thirdSecret = String(repeating: "d4", count: 32)
    func fixture(_ id: String, secret: String, title: String) -> Orchestrator.Task {
        var task = Orchestrator.Task(
            id: id, state: .success, kind: "custom", title: title, assistant: .claude,
            projectDir: repository.url.path, timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: 1), rootSessionId: "root-\(id)",
            rootLabel: "owner \(title)", claims: ["Sources/\(title).swift"],
            claimsDeclared: true, secretHash: Orchestrator.hash(ofSecret: secret))
        task.claimKeys = OrchestratorDraft.freezeClaims(task.claims, projectDir: task.projectDir)
        return task
    }
    Orchestrator.holdScheduleTaskForTesting(fixture(firstID, secret: firstSecret, title: "pending"))
    Orchestrator.holdScheduleTaskForTesting(fixture(secondID, secret: secondSecret, title: "landed"))
    Orchestrator.holdScheduleTaskForTesting(fixture(thirdID, secret: thirdSecret, title: "abandoned"))
    let path = "/v1/orchestrator/tasks/\(firstID)/landing"

    let missing = RemoteServer.shared.route(remoteRequest(
        "POST", path, body: "{\"state\":\"pending\"}"))
    expect("a missing task secret reaches the landing handler and is forbidden", missing.status, 403)
    let wrong = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: ["X-Clawdline-Task-Secret": String(repeating: "e5", count: 32)],
        body: "{\"state\":\"pending\"}"))
    expect("a wrong task secret is forbidden", wrong.status, 403)
    let another = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: ["X-Clawdline-Task-Secret": secondSecret],
        body: "{\"state\":\"pending\"}"))
    expect("another task's real secret is still forbidden", another.status, 403)
    let wrongMachineToken = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: ["X-Clawdline-Orchestrator": "not-the-machine-token"],
        body: "{\"state\":\"pending\"}"))
    expect("a wrong orchestrator token is forbidden", wrongMachineToken.status, 403)

    let machine = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()],
        body: "{\"state\":\"pending\",\"note\":\"accepted after handoff\"}"))
    expect("the orchestrator token may update a landing after handoff", machine.status, 200)

    let childCannotLand = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: ["X-Clawdline-Task-Secret": firstSecret],
        body: "{\"state\":\"landed\",\"target\":\"main\",\"commit\":\"\(repository.commit)\"}"))
    expect("a task secret cannot assert landed over HTTP", childCannotLand.status, 403)

    let unknown = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: ["X-Clawdline-Task-Secret": firstSecret],
        body: "{\"state\":\"pending\",\"surprise\":true}"))
    expect("unknown landing fields are a bad request", unknown.status, 400)
    expect("with the ordinary typed code", remoteErrorCode(unknown), "bad_request")

    if case .refused(let status, let code, _, _) = Orchestrator.updateLanding(
        taskID: firstID, secret: firstSecret, raw: ["state": "mystery"]) {
        check("an unknown landing state is a bad request",
              status == 400 && code == "bad_request")
    } else { check("an unknown landing state is a bad request", false) }
    if case .refused(let status, let code, _, _) = Orchestrator.updateLanding(
        taskID: firstID, secret: firstSecret,
        raw: ["state": "pending", "commit": "abc123"]) {
        check("commit is rejected for a non-landed state",
              status == 400 && code == "bad_request")
    } else { check("commit is rejected for a non-landed state", false) }
    for (field, limit) in [("target", 200), ("delivery", 500),
                           ("commit", 200), ("note", 500)] {
        let state = field == "commit" ? "landed" : "pending"
        if case .refused(let status, let code, _, _) = Orchestrator.updateLanding(
            taskID: firstID, secret: field == "commit" ? "" : firstSecret,
            orchestratorToken: field == "commit" ? Orchestrator.dispatchToken() : nil,
            raw: ["state": state, field: String(repeating: "x", count: limit + 1)]) {
            check("\(field) enforces its documented length limit",
                  status == 400 && code == "bad_request")
        } else { check("\(field) enforces its documented length limit", false) }
    }
    if case .refused(let status, let code, _, _) = Orchestrator.updateLanding(
        taskID: "00000000-0000-0000-0000-000000000000", secret: firstSecret,
        raw: ["state": "pending"]) {
        check("an unknown landing task is not found", status == 404 && code == "not_found")
    } else { check("an unknown landing task is not found", false) }

    let accepted = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: ["X-Clawdline-Task-Secret": firstSecret],
        body: "{\"state\":\"pending\",\"target\":\"main\",\"note\":\"waiting\"}"))
    expect("the matching task secret updates its landing record", accepted.status, 200)
    // Replace the route fixture so the list assertions below have an exact, injected clock;
    // route authentication itself was the fact the call above exercised.
    Orchestrator.holdScheduleTaskForTesting(
        fixture(firstID, secret: firstSecret, title: "pending"))
    _ = Orchestrator.updateLanding(
        taskID: firstID, secret: firstSecret,
        raw: ["state": "pending", "target": "main", "note": "waiting"],
        now: Date(timeIntervalSince1970: 1))
    _ = Orchestrator.updateLanding(
        taskID: secondID, secret: "", orchestratorToken: Orchestrator.dispatchToken(),
        raw: ["state": "landed", "target": "main", "commit": repository.commit],
        now: Date(timeIntervalSince1970: 20))
    _ = Orchestrator.updateLanding(
        taskID: thirdID, secret: thirdSecret,
        raw: ["state": "abandoned", "target": "main"],
        now: Date(timeIntervalSince1970: 30))

    let firstLanding = Orchestrator.record(id: firstID)?["landing"] as? [String: Any]
    let pendingSince = firstLanding?["since"] as? Int ?? 0
    let pending = Orchestrator.landingRecords(
        now: Date(timeIntervalSince1970: TimeInterval(pendingSince + 50)))
    check("the registry lists only pending landing obligations",
          pending.count == 1 && pending.first?["id"] as? String == firstID)
    check("a pending row carries owner, claims, target and the shared non-negative age formula",
          pending.first?["title"] as? String == "pending"
              && pending.first?["root_label"] as? String == "owner pending"
              && pending.first?["root_key"] as? String
                  == OrchestratorDraft.rootKeyDigest("root-\(firstID)")
              && pending.first?["paths"] as? [String] == ["Sources/pending.swift"]
              && pending.first?["target"] as? String == "main"
              && pending.first?["age_seconds"] as? Int == 50)
    expect("the age formula clamps a clock rollback to zero",
           Orchestrator.landingRecords(
                now: Date(timeIntervalSince1970: TimeInterval(pendingSince - 1)))
                .first?["age_seconds"] as? Int,
           0)

    Orchestrator.saveForTesting()
    Orchestrator.forget()
    let reloaded = Orchestrator.record(id: firstID)?["landing"] as? [String: Any]
    check("landing survives a registry save/load round trip",
          reloaded?["state"] as? String == "pending"
              && reloaded?["owner_root_key"] as? String
                  == OrchestratorDraft.rootKeyDigest("root-\(firstID)"))
    let reloadedLanded = Orchestrator.record(id: secondID)?["landing"] as? [String: Any]
    check("verified landing evidence survives a registry save/load round trip",
          reloadedLanded?["landed_at"] as? Int == 20
              && reloadedLanded?["verification_origin"] as? String == "local_target_branch"
              && reloadedLanded?["verified_commit"] as? String == repository.commit
              && reloadedLanded?["verified_target_commit"] as? String == repository.commit)

    let anonymous = RemoteServer.shared.route(remoteRequest("GET", "/v1/orchestrator/landings"))
    expect("an anonymous landing-registry read stops at the door", anonymous.status, 401)
    let phone = RemoteAuth.addDevice(name: "landing route reader", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: phone.id) }
    let listed = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/landings",
        headers: ["Authorization": "Bearer \(phone.token)"]))
    expect("a paired reader may query pending landing obligations", listed.status, 200)
    let listedJSON = (try? JSONSerialization.jsonObject(with: listed.body)) as? [String: Any]
    expect("the GET route returns the same one pending row",
           (listedJSON?["landings"] as? [[String: Any]])?.count, 1)
}

group("pending landing ownership is one fail-closed observation across list and Bearings") {
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        RemoteServer.coordinatorSessionsForTesting = nil
        RemoteServer.coordinatorObservationEvidenceForTesting = nil
        RemoteServer.coordinatorObservationUnavailableForTesting = false
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    let observedAt = Date(timeIntervalSince1970: 1_800_001_000)
    let processStart = Date(timeIntervalSince1970: 1_800_000_000)

    func pendingTask(_ id: String, state: Orchestrator.State,
                     root: String, title: String) -> Orchestrator.Task {
        var task = Orchestrator.Task(
            id: id, state: state, kind: "custom", title: title, assistant: .codex,
            projectDir: "/tmp", timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: 1),
            finishedAt: state.isTerminal ? Date(timeIntervalSince1970: 2) : nil,
            rootSessionId: root, rootAssistant: .codex,
            rootLabel: title + " owner", claims: ["Sources/\(title).swift"],
            claimsDeclared: true, secretHash: String(repeating: "0", count: 64))
        task.landing = Orchestrator.Landing(
            state: .pending, target: "main", delivery: nil,
            ownerRootKey: OrchestratorDraft.rootKeyDigest(root),
            since: Date(timeIntervalSince1970: 10), commit: nil, note: nil)
        return task
    }

    let liveObservedID = "41414141-5151-6161-7171-818181818181"
    var liveObserved = pendingTask(
        liveObservedID, state: .briefed, root: "root-live-observed", title: "live-observed")
    liveObserved.childTerminalId = "EXECUTOR-WORKING"
    liveObserved.childTTY = "/dev/ttys301"
    liveObserved.childPID = 1_301
    liveObserved.childProcStart = processStart
    liveObserved.childSessionId = "executor-working-conversation"
    liveObserved.transcriptProven = true
    Orchestrator.holdScheduleTaskForTesting(liveObserved)

    let liveUnobservedID = "42424242-5252-6262-7272-828282828282"
    Orchestrator.holdScheduleTaskForTesting(pendingTask(
        liveUnobservedID, state: .briefed,
        root: "root-live-unobserved", title: "live-unobserved"))

    let terminalWorkingID = "43434343-5353-6363-7373-838383838383"
    Orchestrator.holdScheduleTaskForTesting(pendingTask(
        terminalWorkingID, state: .success,
        root: "owner-working-conversation", title: "terminal-working"))

    let ownerReadyID = "44444444-5454-6464-7474-848484848484"
    Orchestrator.holdScheduleTaskForTesting(pendingTask(
        ownerReadyID, state: .success,
        root: "owner-ready-conversation", title: "owner-ready"))

    let absentID = "45454545-5555-6565-7575-858585858585"
    Orchestrator.holdScheduleTaskForTesting(pendingTask(
        absentID, state: .success, root: "owner-absent-conversation", title: "owner-absent"))

    let missingAssistantID = "46464646-5656-6666-7676-868686868686"
    var missingAssistant = pendingTask(
        missingAssistantID, state: .success,
        root: "legacy-codex-owner-conversation", title: "missing-root-assistant")
    missingAssistant.rootAssistant = nil
    Orchestrator.holdScheduleTaskForTesting(missingAssistant)

    RemoteServer.coordinatorSessionsForTesting = [
        coordinatorFixture(
            "EXECUTOR-WORKING", tty: "/dev/ttys301", pid: 1_301,
            processStart: processStart, conversation: "executor-working-conversation",
            workState: .working),
        coordinatorFixture(
            "OWNER-WORKING", tty: "/dev/ttys302", pid: 1_302,
            processStart: processStart, conversation: "owner-working-conversation",
            workState: .working),
        coordinatorFixture(
            "OWNER-READY", tty: "/dev/ttys303", pid: 1_303,
            processStart: processStart, conversation: "owner-ready-conversation",
            workState: .ready),
        coordinatorFixture(
            "LEGACY-CODEX-OWNER", tty: "/dev/ttys304", pid: 1_304,
            processStart: processStart, conversation: "legacy-codex-owner-conversation",
            workState: .working),
    ]
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: observedAt, generation: 77, complete: true)
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    func json(_ path: String) -> [String: Any]? {
        let response = RemoteServer.shared.route(remoteRequest("GET", path, headers: auth))
        return (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]
    }
    func rows(_ payload: [String: Any]?) -> [[String: Any]] {
        payload?["landings"] as? [[String: Any]] ?? []
    }
    func ownership(_ id: String, in rows: [[String: Any]]) -> [String: Any]? {
        rows.first { $0["id"] as? String == id }?["ownership"] as? [String: Any]
    }
    let listed = json("/v1/orchestrator/landings")
    let listedRows = rows(listed)
    expect("an exact live child is observed working", ownership(liveObservedID, in: listedRows)?["status"] as? String,
           "observed_working")
    expect("live child evidence names the executor", ownership(liveObservedID, in: listedRows)?["subject"] as? String,
           "executor")
    expect("a live registry row without an observed executor stays explicitly live",
           ownership(liveUnobservedID, in: listedRows)?["status"] as? String,
           "task_still_live")
    check("terminal success remains pending and attributes current root work without reviving the child",
          ownership(terminalWorkingID, in: listedRows)?["status"] as? String
                == "observed_working"
            && ownership(terminalWorkingID, in: listedRows)?["subject"] as? String == "root"
            && ownership(terminalWorkingID, in: listedRows)?["task_state"] as? String == "success")
    expect("a ready owner is distinguished from active work",
           ownership(ownerReadyID, in: listedRows)?["status"] as? String,
           "observed_ready_or_holding")
    expect("absence under one complete inventory is not-observed, not dead",
           ownership(absentID, in: listedRows)?["status"] as? String, "not_observed")
    check("a live Codex-root legacy row with no assistant fails closed instead of inventing Claude",
          ownership(missingAssistantID, in: listedRows)?["status"] as? String == "unknown"
            && ownership(missingAssistantID, in: listedRows)?["reason"] as? String
                == "root_identity_missing"
            && ownership(missingAssistantID, in: listedRows)?["root_assistant"] is NSNull)
    check("the list carries closed source freshness without process or transcript secrets",
          ((listed?["sources"] as? [String: Any])?["sessions"] as? [String: Any])?["freshness"] as? String
                == "current"
            && listedRows.allSatisfy { row in
                guard let owner = row["ownership"] as? [String: Any] else { return false }
                let closedKeys: Set<String> = [
                    "version", "status", "subject", "reason", "task_id", "task_state",
                    "root_key", "root_assistant", "observed_work_state", "evidence",
                ]
                guard Set(owner.keys) == closedKeys,
                      let evidence = owner["evidence"] as? [String: Any],
                      Set(evidence.keys) == ["sessions", "tasks", "landings"] else {
                    return false
                }
                return owner["task_id"] as? String == row["id"] as? String
                    && owner["root_key"] as? String == row["root_key"] as? String
                    && owner["root_session_id"] == nil
                    && owner["pid"] == nil && owner["tty"] == nil
                    && owner["transcript"] == nil && owner["token"] == nil
            })

    let coordinator = json("/v1/orchestrator/coordinator")
    let bearingsRows = ((coordinator?["bearings"] as? [String: Any])?["pending_landings"]
        as? [[String: Any]]) ?? []
    check("the same absent subject has the same identity and status in Bearings",
          ownership(absentID, in: listedRows)?["status"] as? String
                == ownership(absentID, in: bearingsRows)?["status"] as? String
            && bearingsRows.first { $0["id"] as? String == absentID }?["root_key"] as? String
                == listedRows.first { $0["id"] as? String == absentID }?["root_key"] as? String)

    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: observedAt, generation: 78, complete: false)
    let staleListed = rows(json("/v1/orchestrator/landings"))
    expect("absence under an incomplete inventory fails closed as unknown",
           ownership(absentID, in: staleListed)?["status"] as? String, "unknown")
    check("stale evidence can never be rendered offline or dead",
          !["offline", "dead"].contains(
            ownership(absentID, in: staleListed)?["status"] as? String ?? ""))
    expect("even a matching session under an incomplete inventory stays unknown",
           ownership(liveObservedID, in: staleListed)?["status"] as? String, "unknown")

    RemoteServer.coordinatorSessionsForTesting = nil
    RemoteServer.coordinatorObservationEvidenceForTesting = nil
    RemoteServer.coordinatorObservationUnavailableForTesting = true
    let unavailable = json("/v1/orchestrator/landings")
    let unavailableRows = rows(unavailable)
    check("a failed live observation still returns every registry landing with unknown ownership",
          unavailableRows.count == 6
            && unavailableRows.allSatisfy {
                ($0["ownership"] as? [String: Any])?["status"] as? String == "unknown"
            })
    expect("unavailable observation is not rendered as a complete absence",
           ((unavailable?["sources"] as? [String: Any])?["sessions"]
                as? [String: Any])?["freshness"] as? String,
           "missing")
}

group("published coordinator inventory is a direct fail-closed read") {
    let source = (try? String(contentsOfFile: "Sources/RemoteServer.swift",
                              encoding: .utf8)) ?? ""
    let observation = sourceSlice(
        source, from: "func coordinatorObservation()",
        through: "static func attachCoordinator")
    check("coordinator projection reads one publication without a main hop or semaphore",
          observation.contains("publishedInventory()")
            && !observation.contains("DispatchQueue.main")
            && !observation.contains("DispatchSemaphore")
            && !observation.contains("onMain("), observation)

    expect("a never-observed production publication renders as missing evidence",
           RemoteServer.coordinatorSessionsFresh(complete: false, observedAt: nil), true)
    expect("an observed incomplete production publication renders as stale evidence",
           RemoteServer.coordinatorSessionsFresh(complete: false, observedAt: Date()), false)

    RemoteServer.coordinatorObservationUnavailableForTesting = true
    defer { RemoteServer.coordinatorObservationUnavailableForTesting = false }
    let response = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/coordinator",
        headers: ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]))
    expect("missing publication evidence still returns the durable coordinator surface",
           response.status, 200)
    let payload = (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]
    let sources = (payload?["bearings"] as? [String: Any])?["sources"] as? [String: Any]
    expect("missing publication evidence is typed rather than rendered as an empty scan",
           (sources?["sessions"] as? [String: Any])?["freshness"] as? String, "missing")
}
group("cleanup retains pending landing obligations beyond the ordinary registry cap") {
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    let pendingID = "30303030-4040-5050-6060-707070707070"
    let ordinaryOldID = "40404040-5050-6060-7070-808080808080"
    let now = Date()
    // Two past whatever the registry cap currently is, read from the setting rather than pinned:
    // the literal 202 here was written against a literal 200 in `cleanup()`, and once the cap
    // became `orchestrator_task_record_limit` a fixture of 202 rows stopped reaching it at all —
    // the group stayed green while testing nothing.
    let cap = Config.shared.orchestratorTaskRecordLimit
    for index in 0..<(cap + 2) {
        let id = index == 0 ? pendingID
            : (index == 1 ? ordinaryOldID
               : String(format: "50505050-6060-7070-8080-%012d", index))
        var task = Orchestrator.Task(
            id: id, state: .success, kind: "custom", title: "retention \(index)",
            assistant: .codex, projectDir: "/repo", timeoutMinutes: 30,
            created: now.addingTimeInterval(TimeInterval(index)),
            finishedAt: now, rootSessionId: "cleanup-root",
            secretHash: String(repeating: "0", count: 64))
        if index == 0 {
            task.landing = Orchestrator.Landing(
                state: .pending, target: "main", delivery: nil,
                ownerRootKey: "12345678", since: now, commit: nil, note: nil)
        }
        Orchestrator.holdScheduleTaskForTesting(task)
    }
    Orchestrator.cleanup()
    check("a pending landing survives even when older than the newest \(cap) tasks",
          Orchestrator.record(id: pendingID) != nil)
    check("the ordinary cap still removes an old settled record",
          Orchestrator.record(id: ordinaryOldID) == nil)
}

group("work visibility decides from git, not from anybody's memory") {
    typealias V = Orchestrator.WorkVisibility
    func visible(_ state: Orchestrator.State, landing: Orchestrator.Landing? = nil,
                 isolated: Bool = true, exists: Bool? = true, merged: Bool? = false) -> V {
        Orchestrator.workVisibility(state: state, landing: landing, isolated: isolated,
                                    branchExists: exists, branchMerged: merged)
    }
    func landing(_ state: Orchestrator.LandingState) -> Orchestrator.Landing {
        Orchestrator.Landing(state: state, target: "main", delivery: nil,
                             ownerRootKey: "00000000", since: Date(timeIntervalSince1970: 1),
                             commit: state == .landed ? "abc123" : nil, note: nil)
    }

    expect("a briefed task is live whatever its branch says",
           visible(.briefed, exists: false, merged: true), V.live)
    expect("a queued task is live before any branch exists",
           visible(.queued, isolated: false, exists: nil, merged: nil), V.live)

    expect("a finished delivery whose branch is unmerged stays visible",
           visible(.success), V.unmerged)
    expect("a task whose session died leaves its branch behind, and it stays visible",
           visible(.failure), V.unmerged)
    expect("a timed-out task is the same case", visible(.timeout), V.unmerged)

    expect("a merged branch settles", visible(.success, merged: true), V.settled)
    expect("a branch that disposal removed settles",
           visible(.success, exists: false, merged: false), V.settled)

    // Unknown git facts keep it visible: showing a merged delivery costs a glance, hiding an
    // unmerged one costs a day. Same direction as worktreeDisposal's fail-safe.
    expect("an unreadable repository does not erase a delivery",
           visible(.success, exists: nil, merged: nil), V.unmerged)

    expect("a root that said landed settles it even with commits on the branch",
           visible(.success, landing: landing(.landed)), V.settled)
    expect("a root that said abandoned settles it too",
           visible(.success, landing: landing(.abandoned)), V.settled)
    expect("a declared pending landing is visible with no worktree at all",
           visible(.success, landing: landing(.pending), isolated: false,
                   exists: nil, merged: nil), V.unmerged)

    // The boundary between the derived half and the declared half: a finished non-isolated task
    // left its edits in the shared tree, where git status already shows them.
    expect("a finished non-isolated task with no landing settles",
           visible(.success, isolated: false, exists: nil, merged: nil), V.settled)
    expect("a live non-isolated task is still live",
           visible(.spawning, isolated: false, exists: nil, merged: nil), V.live)
}

group("session work state is a closed broker projection, never an idle guess") {
    typealias W = Orchestrator.SessionWorkState
    expect("the public work-state vocabulary is closed",
           W.allCases.map(\.rawValue), [
            "ready", "working", "holding", "waiting_you", "waiting_session", "unknown",
            "milestone_complete", "work_complete",
           ])
    func landing(_ state: Orchestrator.LandingState) -> Orchestrator.Landing {
        let commit = String(repeating: "a", count: 40)
        return Orchestrator.Landing(
            state: state, target: "main", delivery: "delivery-branch",
            ownerRootKey: "00000000", since: Date(timeIntervalSince1970: 10),
            commit: state == .landed ? commit : nil, note: nil,
            landedAt: state == .landed ? Date(timeIntervalSince1970: 20) : nil,
            verificationOrigin: state == .landed ? "local_target_branch" : nil,
            verifiedCommit: state == .landed ? commit : nil,
            verifiedTargetCommit: state == .landed ? String(repeating: "b", count: 40) : nil)
    }
    func task(_ state: Orchestrator.State, landing: Orchestrator.Landing? = nil,
              finished: Bool = true) -> Orchestrator.Task {
        var task = Orchestrator.Task(
            id: "12345678-1234-4234-8234-123456789abc", state: state,
            kind: "custom", title: "delivery <phase>", assistant: .codex,
            projectDir: "/repo", timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: 1), secretHash: String(repeating: "0", count: 64))
        task.finishedAt = finished ? Date(timeIntervalSince1970: 15) : nil
        task.landing = landing
        return task
    }
    func project(_ terminal: SessionState, task: Orchestrator.Task? = nil,
                 waiting: Bool = false, handoff: Bool = false,
                 knownReady: Bool = false, delivered: Bool = false,
                 claim: W? = nil) -> W {
        Orchestrator.projectSessionWorkState(
            terminalState: terminal, task: task, hasCoordinationWait: waiting,
            hasOpenHandoff: handoff, assignmentKnownAbsent: knownReady,
            hasSessionDelivery: delivered, selfClaim: claim)
    }

    expect("an explicitly assignment-free prompt is ready",
           project(.idle, knownReady: true), .ready)
    expect("idle without evidence is the quiet absence, not a demand",
           project(.idle), .unknown)
    // The gate this vocabulary exists to fix: an idle assistant session can now reach ready —
    // through its own authenticated declaration, shown as stated rather than proven.
    expect("a declared ready claim makes an idle assistant session ready",
           project(.idle, claim: .ready), .ready)
    expect("a declared holding claim is the only entrance to holding",
           project(.idle, claim: .holding), .holding)
    expect("a self claim can never produce a check state",
           project(.idle, claim: .workComplete), .unknown)
    expect("nor the single check",
           project(.idle, claim: .milestoneComplete), .unknown)
    expect("a question on screen outranks any claim",
           project(.waiting, claim: .ready), .waitingYou)
    expect("current activity outranks any claim",
           project(.working("busy"), claim: .holding), .working)
    expect("a finished failure receipt outranks a ready claim",
           project(.idle, task: task(.failure), claim: .ready), .unknown)
    expect("a delivered turn outranks a ready claim",
           project(.idle, delivered: true, claim: .ready), .milestoneComplete)
    expect("a child mid-task may still declare its own quiet hold",
           project(.idle, task: task(.briefed, finished: false), claim: .holding), .holding)
    expect("a live task without a claim stays the quiet absence",
           project(.idle, task: task(.briefed, finished: false)), .unknown)
    expect("current terminal activity is working", project(.working("building")), .working)
    expect("a human question is the only waiting-human state",
           project(.waiting, waiting: true), .waitingYou)
    expect("a peer or owed wait is waiting-session without asking the human",
           project(.idle, waiting: true), .waitingSession)
    expect("an idle root with an active child is waiting-session",
           Orchestrator.projectSessionWorkState(
            terminalState: .idle, task: nil, hasCoordinationWait: false,
            hasOpenHandoff: false, assignmentKnownAbsent: false,
            hasOutstandingChild: true), .waitingSession)
    expect("a root working beside its child still reads as working",
           Orchestrator.projectSessionWorkState(
            terminalState: .working("parallel work"), task: nil,
            hasCoordinationWait: false, hasOpenHandoff: false,
            assignmentKnownAbsent: false, hasOutstandingChild: true), .working)
    expect("an unreadable terminal outranks a delivered milestone",
           project(.unknown, task: task(.success)), .unknown)
    expect("renewed activity outranks an older delivery receipt",
           project(.working("follow-up"), task: task(.success, landing: landing(.landed))),
           .working)
    expect("authenticated task success is exactly a milestone",
           project(.idle, task: task(.success)), .milestoneComplete)
    expect("a pending landing cannot produce the double check",
           project(.idle, task: task(.success, landing: landing(.pending))),
           .milestoneComplete)
    expect("a malformed success without its finished receipt fails closed",
           project(.idle, task: task(.success, finished: false)), .unknown)
    expect("a broker-verified target landing closes exactly that task scope",
           project(.idle, task: task(.success, landing: landing(.landed))), .workComplete)
    expect("an open handoff keeps landed delivery at a milestone",
           project(.idle, task: task(.success, landing: landing(.landed)), handoff: true),
           .milestoneComplete)
    expect("a durable peer wait outranks broker closure",
           project(.idle, task: task(.success, landing: landing(.landed)), waiting: true),
           .waitingSession)
    expect("failure is never completion, and never a demand either",
           project(.idle, task: task(.failure)), .unknown)
}

group("a session's own declaration is bounded, typed, and can never mint a check") {
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    let started = Date(timeIntervalSince1970: 700)
    let identity = Orchestrator.SessionWorkIdentity(
        terminalID: "SELF-TAB", assistant: .claude, tty: "/dev/ttys20", pid: 2000,
        processStart: started, conversationID: "conversation-self")

    if case .refused(let status, let code, _, _) = Orchestrator.declareSessionState(
        identity: identity, terminalState: .idle, claim: "ready", note: nil, movedBy: nil,
        personNeeded: nil, owed: nil, clearOwed: false) {
        expect("declaring from an idle prompt is refused like delivery is", status, 409)
        expect("with the same typed reason", code, "session_not_working")
    } else { check("an idle declaration must be refused", false) }

    if case .refused(let status, let code, _, _) = Orchestrator.declareSessionState(
        identity: identity, terminalState: .working("wrapping"), claim: "work_complete",
        note: nil, movedBy: nil, personNeeded: nil, owed: nil, clearOwed: false) {
        expect("the check states are refused by name", status, 403)
        expect("with a boundary-naming code", code, "self_completion_refused")
    } else { check("a self-declared check must be refused", false) }

    if case .refused(_, let code, _, _) = Orchestrator.declareSessionState(
        identity: identity, terminalState: .working("wrapping"), claim: "holding",
        note: nil, movedBy: nil, personNeeded: nil, owed: nil, clearOwed: false) {
        expect("holding without its evidence is refused, never defaulted",
               code, "holding_needs_evidence")
    } else { check("an evidence-free holding must be refused", false) }

    guard case .ok = Orchestrator.declareSessionState(
        identity: identity, terminalState: .working("wrapping"), claim: "ready",
        note: "fix landed; can take new work", movedBy: nil, personNeeded: nil,
        owed: ["note": "the schedules design is still your call"], clearOwed: false,
        now: Date(timeIntervalSince1970: 800)) else {
        check("a bound working session can declare ready with a debt", false); return
    }
    let projected = Orchestrator.sessionWorkProjection(identity: identity, terminalState: .idle)
    expect("the idle prompt now reads ready", projected.state, .ready)
    expect("and says who said so", projected.provenance, "self")
    expect("in the session's own words", projected.note, "fix landed; can take new work")
    expect("the debt rides beside the claim",
           projected.owed?["note"] as? String, "the schedules design is still your call")
    expect("a debt defaults to needing a person", projected.owed?["person_needed"] as? Bool, true)

    var reused = identity
    reused.pid = 2001
    expect("a later process in the same terminal cannot borrow the claim",
           Orchestrator.sessionWorkProjection(identity: reused, terminalState: .idle).state,
           .unknown)

    // Redeclaring the same debt must keep its first clock: age is the debt's whole risk.
    _ = Orchestrator.declareSessionState(
        identity: identity, terminalState: .working("still"), claim: nil, note: nil,
        movedBy: nil, personNeeded: nil,
        owed: ["note": "the schedules design is still your call"], clearOwed: false,
        now: Date(timeIntervalSince1970: 5_000))
    expect("the same debt keeps its original since",
           Orchestrator.sessionWorkProjection(identity: identity, terminalState: .idle)
               .owed?["since"] as? Int, 800)

    Orchestrator.noteSessionStateChange(terminalID: identity.terminalID, to: .idle)
    Orchestrator.noteSessionStateChange(terminalID: identity.terminalID,
                                        to: .working("next turn"))
    let afterTurn = Orchestrator.sessionWorkProjection(identity: identity, terminalState: .idle)
    expect("the next observed turn consumes the claim", afterTurn.state, .unknown)
    expect("but the debt survives the turn — its failure mode is being forgotten",
           afterTurn.owed?["note"] as? String, "the schedules design is still your call")

    Orchestrator.saveForTesting()
    Orchestrator.forget()
    expect("the surviving debt also survives a restart",
           Orchestrator.sessionWorkProjection(identity: identity, terminalState: .idle)
               .owed?["note"] as? String, "the schedules design is still your call")

    _ = Orchestrator.declareSessionState(
        identity: identity, terminalState: .working("clearing"), claim: nil, note: nil,
        movedBy: nil, personNeeded: nil, owed: nil, clearOwed: true)
    check("an explicit clear is the one thing that pays the debt",
          Orchestrator.sessionWorkProjection(identity: identity, terminalState: .idle)
              .owed == nil)
}

    runSessionCloseabilityTests()

group("session completion receipts are bound to the current process, not a reusable terminal") {
    let started = Date(timeIntervalSince1970: 100)
    let identity = Orchestrator.SessionWorkIdentity(
        terminalID: "TAB", assistant: .codex, tty: "/dev/ttys7", pid: 700,
        processStart: started, conversationID: "conversation-current")
    var task = Orchestrator.Task(
        id: "abababab-1234-4234-8234-abcdefabcdef", state: .success,
        kind: "custom", title: "bound child", assistant: .codex,
        projectDir: "/repo", timeoutMinutes: 30, created: Date(timeIntervalSince1970: 1),
        finishedAt: Date(timeIntervalSince1970: 200),
        secretHash: String(repeating: "0", count: 64))
    task.childTerminalId = "TAB"
    task.childTTY = "/dev/ttys7"
    task.childPID = 700
    task.childProcStart = started
    task.childSessionId = "conversation-current"
    task.transcriptProven = true

    check("the exact assistant, process, conversation and child marker bind the receipt",
          Orchestrator.taskMatchesCurrentSession(task, identity: identity))
    var stale = identity
    stale.pid = 701
    check("a reused terminal with a later process cannot borrow the old check",
          !Orchestrator.taskMatchesCurrentSession(task, identity: stale))
    stale = identity
    stale.processStart = started.addingTimeInterval(30)
    check("a recycled pid with another start time also fails closed",
          !Orchestrator.taskMatchesCurrentSession(task, identity: stale))
    stale = identity
    stale.conversationID = "conversation-later"
    check("another rollout or conversation in the same terminal cannot borrow it",
          !Orchestrator.taskMatchesCurrentSession(task, identity: stale))
    stale = identity
    stale.assistant = .claude
    check("another assistant in the same terminal cannot borrow it",
          !Orchestrator.taskMatchesCurrentSession(task, identity: stale))
    var unproved = task
    unproved.transcriptProven = false
    check("legacy child identity without task-marker proof fails closed",
          !Orchestrator.taskMatchesCurrentSession(unproved, identity: identity))
    expect("the projection selector returns the one exact process-bound task",
           Orchestrator.taskForCurrentSession([task, unproved], identity: identity)?.id, task.id)
    check("duplicate exact receipts are contradictory and fail closed",
          Orchestrator.taskForCurrentSession([task, task], identity: identity) == nil)
    check("a stale process has no selectable completion receipt",
          Orchestrator.taskForCurrentSession([task], identity: stale) == nil)

    check("handoff source may be the exact terminal namespace",
          Orchestrator.handoffSource("TAB", matches: identity))
    check("handoff source may separately be the process-bound conversation namespace",
          Orchestrator.handoffSource("conversation-current", matches: identity))
    check("a stale terminal-like prefix is not guessed",
          !Orchestrator.handoffSource("TAB-old", matches: identity))
    check("a stale conversation is not guessed",
          !Orchestrator.handoffSource("conversation-later", matches: identity))
}

group("a root session can report one delivered turn without becoming a child task") {
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    let started = Date(timeIntervalSince1970: 300)
    let rootConversation = "12121212-3434-4567-8899-abcdefabcdef"
    let identity = Orchestrator.SessionWorkIdentity(
        terminalID: "ROOT-TAB", assistant: .codex, tty: "/dev/ttys9", pid: 900,
        processStart: started, conversationID: rootConversation)
    let reported = Orchestrator.reportSessionDelivery(
        identity: identity, terminalState: .working("wrapping up"),
        summary: "Implemented and committed the requested change.",
        now: Date(timeIntervalSince1970: 400))
    guard case .ok(let payload) = reported,
          let disposition = payload["disposition"] as? [String: Any] else {
        check("a bound working root can report delivery", false); return
    }
    expect("the root receipt is session-scoped", disposition["scope"] as? String, "session")
    expect("with explicit self-reported delivery evidence",
           disposition["evidence"] as? String, "authenticated_session_delivery")
    expect("and the authored summary", disposition["title"] as? String,
           "Implemented and committed the requested change.")
    if case .ok(let retry) = Orchestrator.reportSessionDelivery(
        identity: identity, terminalState: .working("still wrapping up"),
        summary: "Implemented and committed the requested change.",
        now: Date(timeIntervalSince1970: 401)) {
        expect("an identical in-turn report is idempotent", retry["created"] as? Bool, false)
        expect("and preserves the original receipt time",
               (retry["disposition"] as? [String: Any])?["receiptAt"] as? Int, 400)
    } else { check("an identical root report can be retried", false) }

    let unrelatedChild = Orchestrator.Task(
        id: "cdcdcdcd-1111-4222-8333-444444444444", state: .briefed, kind: "custom",
        title: "another root's child", assistant: .claude, projectDir: "/repo",
        timeoutMinutes: 30, created: Date(timeIntervalSince1970: 399),
        rootSessionId: "34343434-5656-4789-8abc-defabcdefabc", rootAssistant: .codex,
        secretHash: String(repeating: "0", count: 64))
    Orchestrator.holdScheduleTaskForTesting(unrelatedChild)

    let idle = Orchestrator.sessionWorkProjection(identity: identity, terminalState: .idle)
    expect("another root's top-level child does not turn this root into waiting",
           idle.state, .milestoneComplete)
    expect("the public projection keeps the session scope",
           idle.disposition?["scope"] as? String, "session")
    Orchestrator.saveForTesting()
    Orchestrator.forget()
    expect("the root receipt survives an app restart",
           Orchestrator.sessionWorkProjection(identity: identity, terminalState: .idle).state,
           .milestoneComplete)

    var reused = identity
    reused.pid = 901
    expect("a later process in the same terminal cannot borrow the root receipt",
           Orchestrator.sessionWorkProjection(identity: reused, terminalState: .idle).state,
           .unknown)

    Orchestrator.noteSessionStateChange(terminalID: identity.terminalID, to: .idle)
    Orchestrator.noteSessionStateChange(terminalID: identity.terminalID,
                                        to: .working("new request"))
    expect("starting the next observed turn consumes the old root receipt",
           Orchestrator.sessionWorkProjection(identity: identity, terminalState: .idle).state,
           .unknown)

    let child = Orchestrator.Task(
        id: "dededede-1111-4222-8333-444444444444", state: .briefed, kind: "custom",
        title: "live child", assistant: .claude, projectDir: "/repo", timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 450), rootSessionId: rootConversation,
        rootAssistant: .codex, secretHash: String(repeating: "0", count: 64))
    Orchestrator.holdScheduleTaskForTesting(child)
    expect("the broker projects an idle root with its live child as waiting-session",
           Orchestrator.sessionWorkProjection(identity: identity, terminalState: .idle).state,
           .waitingSession)

    let childIdentity = Orchestrator.SessionWorkIdentity(
        terminalID: "CHILD-TAB", assistant: .claude, tty: "/dev/ttys10", pid: 1000,
        processStart: Date(timeIntervalSince1970: 500), conversationID: "child-conversation")
    var ownTask = Orchestrator.Task(
        id: "efefefef-1111-4222-8333-444444444444", state: .briefed, kind: "custom",
        title: "this process is a child", assistant: .claude, projectDir: "/repo",
        timeoutMinutes: 30, created: Date(timeIntervalSince1970: 490),
        secretHash: String(repeating: "0", count: 64))
    ownTask.childTerminalId = childIdentity.terminalID
    ownTask.childTTY = childIdentity.tty
    ownTask.childPID = childIdentity.pid
    ownTask.childProcStart = childIdentity.processStart
    ownTask.childSessionId = childIdentity.conversationID
    ownTask.transcriptProven = true
    Orchestrator.holdScheduleTaskForTesting(ownTask)
    if case .refused(let status, let code, _, _) = Orchestrator.reportSessionDelivery(
        identity: childIdentity, terminalState: .working("done"), summary: "done") {
        expect("a child cannot use the root completion route", status, 409)
        expect("and is sent back to its task result", code, "child_session")
    } else { check("a child report is refused", false) }

    if case .refused(let status, let code, _, _) = Orchestrator.reportSessionDelivery(
        identity: identity, terminalState: .idle, summary: "too late") {
        expect("a report outside its active turn is a conflict", status, 409)
        expect("and says the session is not working", code, "session_not_working")
    } else { check("an idle root cannot mint a fresh check", false) }

    var incomplete = identity
    incomplete.conversationID = nil
    if case .refused(let status, let code, _, _) = Orchestrator.reportSessionDelivery(
        identity: incomplete, terminalState: .working("done"), summary: "done") {
        expect("an unbound root report is refused", status, 409)
        expect("with the process-binding code", code, "session_unbound")
    } else { check("an unbound process cannot report delivery", false) }

    let path = "/v1/orchestrator/sessions/ROOT-TAB/complete"
    let anonymous = RemoteServer.shared.route(remoteRequest(
        "POST", path, body: "{\"summary\":\"done\"}"))
    expect("the root completion route needs the machine-local credential",
           anonymous.status, 401)
}

group("only broker-verified target landing evidence can produce the double check") {
    let legacy = Orchestrator.Landing(
        state: .landed, target: "main", delivery: "delivery",
        ownerRootKey: "00000000", since: Date(timeIntervalSince1970: 10),
        commit: "abc123", note: nil, landedAt: Date(timeIntervalSince1970: 20))
    check("a legacy landed row with only arbitrary commit text fails closed",
          !Orchestrator.isBrokerVerifiedTargetLanding(legacy))
    let verified = Orchestrator.Landing(
        state: .landed, target: "main", delivery: "delivery",
        ownerRootKey: "00000000", since: Date(timeIntervalSince1970: 10),
        commit: String(repeating: "a", count: 40), note: nil,
        landedAt: Date(timeIntervalSince1970: 20),
        verificationOrigin: "local_target_branch",
        verifiedCommit: String(repeating: "a", count: 40),
        verifiedTargetCommit: String(repeating: "b", count: 40))
    check("the explicit broker-verified local target receipt is accepted",
          Orchestrator.isBrokerVerifiedTargetLanding(verified))
}

// The last box of the delivery loop had never once been `done` on this machine: 23 landing nodes,
// none of them with a task, because landing belongs to the root and a root does not dispatch
// itself — while 66 landing receipts sat on the delivery tasks beside them. These read the receipt
// where the root writes it, and say what still must not count as one.
group("a landing node reads the receipt the root actually wrote, on the delivery beside it") {
    func node(_ id: String, _ kind: Orchestrator.GraphNodeKind,
              _ dependsOn: [String]) -> Orchestrator.GraphNode {
        Orchestrator.GraphNode(id: id, title: id, kind: kind, dependsOn: dependsOn,
                               acceptance: ["\(id) is done"])
    }
    func graph(_ id: String, _ nodes: [Orchestrator.GraphNode],
               current: String) -> Orchestrator.PlanningGraph {
        Orchestrator.PlanningGraph(id: id, destination: "Landed on main", currentNode: current,
                                   nodes: nodes, unknowns: [], outOfScope: [])
    }
    func landedReceipt(_ commit: String) -> Orchestrator.Landing {
        Orchestrator.Landing(state: .landed, target: "main", delivery: "branch",
                             ownerRootKey: "00000000", since: Date(timeIntervalSince1970: 1),
                             commit: commit, note: nil,
                             landedAt: Date(timeIntervalSince1970: 2),
                             verificationOrigin: "local_target_branch",
                             verifiedCommit: commit, verifiedTargetCommit: commit)
    }
    // The same receipt with the broker's three verification fields left off. A row shaped like this
    // decodes on purpose — `OrchestratorStoreTests` pins "a legacy landed row with no verification
    // at all still decodes", because an older build wrote landed rows before the fact existed — and
    // decoding is not the same question as counting as evidence.
    func unverifiedReceipt(_ commit: String) -> Orchestrator.Landing {
        Orchestrator.Landing(state: .landed, target: "main", delivery: "branch",
                             ownerRootKey: "00000000", since: Date(timeIntervalSince1970: 1),
                             commit: commit, note: nil,
                             landedAt: Date(timeIntervalSince1970: 2))
    }
    var serial = 0
    func hold(_ nodeID: String, in planningGraph: Orchestrator.PlanningGraph,
              state: Orchestrator.State = .success,
              landing: Orchestrator.Landing? = nil,
              review: Orchestrator.ReviewReceipt? = nil,
              verification: Orchestrator.Verification? = nil) {
        serial += 1
        let hex = String(format: "%012x", serial)
        var task = Orchestrator.Task(
            id: "00000000-0000-4000-8000-\(hex)", state: state, kind: nodeID, title: nodeID,
            assistant: .claude, projectDir: "/repo", timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: TimeInterval(serial)),
            graph: graph(planningGraph.id, planningGraph.nodes, current: nodeID),
            secretHash: String(repeating: "0", count: 64))
        task.landing = landing
        task.review = review
        task.verification = verification
        Orchestrator.holdScheduleTaskForTesting(task)
    }
    func state(of nodeID: String, in planningGraph: Orchestrator.PlanningGraph) -> String {
        let record = Orchestrator.planningGraphRecord(planningGraph,
                                                      taskIndex: Orchestrator.graphTaskIndex())
        let rows = record["nodes"] as? [[String: Any]] ?? []
        return rows.first(where: { $0["id"] as? String == nodeID })?["state"] as? String ?? ""
    }

    // The ordinary shape, and the one every graph on this machine was stuck in: delivery, review,
    // verification, landing. Nothing depends on the landing node directly, so the receipt has to
    // be found through two nodes that never carry one.
    Orchestrator.forget()
    let ordinaryNodes = [node("build", .delivery, []), node("review", .review, ["build"]),
                         node("verify", .verification, ["review"]),
                         node("land", .landing, ["verify"])]
    let ordinary = graph("11111111-1111-4111-8111-111111111111", ordinaryNodes, current: "land")
    hold("build", in: ordinary)
    check("a delivery with no landing receipt leaves the landing node where it was",
          state(of: "land", in: ordinary) == "blocked")
    Orchestrator.forget()
    hold("build", in: ordinary, landing: landedReceipt(String(repeating: "a", count: 40)))
    check("the landing node is done once the delivery it lands carries a landed receipt",
          state(of: "land", in: ordinary) == "done")
    check("finding it through a review and a verification needs neither to have passed",
          state(of: "review", in: ordinary) == "ready"
              && state(of: "verify", in: ordinary) == "blocked")

    // Verification reached `Sources/` on 2026-08-27 (`verifiedCommit`) and the graph did not exist
    // until 2026-08-31 (`PlanningGraph`), and a landing node resolves its producers only through
    // the graph task index — so no row a graph node can reach was written by a build that could
    // omit it. Requiring it here therefore refuses nothing any store on any machine holds, while
    // leaving the decoder's legacy path exactly where it is.
    Orchestrator.forget()
    hold("build", in: ordinary, landing: unverifiedReceipt(String(repeating: "a", count: 40)))
    check("a landed receipt the broker never verified is not evidence for a landing node",
          state(of: "land", in: ordinary) == "blocked")

    // What a landed receipt on the delivery must not be allowed to say.
    Orchestrator.forget()
    let pairNodes = [node("swift-core", .delivery, []), node("web-surface", .delivery, []),
                     node("land", .landing, ["swift-core", "web-surface"])]
    let pair = graph("22222222-2222-4222-8222-222222222222", pairNodes, current: "land")
    hold("swift-core", in: pair, landing: landedReceipt(String(repeating: "b", count: 40)))
    check("a second declared delivery that never ran is not landed by the first one's receipt",
          state(of: "land", in: pair) == "blocked")
    hold("web-surface", in: pair)
    check("a second delivery that ran and was never landed leaves the node dispatchable, not done",
          state(of: "land", in: pair) == "ready")
    hold("web-surface", in: pair, landing: landedReceipt(String(repeating: "c", count: 40)))
    check("both deliveries landed is what closes a graph that declared two",
          state(of: "land", in: pair) == "done")

    // A correction is contingent — a review demands one or it does not — so an undispatched
    // correction node is nothing to land. Treating it as missing evidence would reproduce the
    // original bug one node further along, in every graph whose review found nothing.
    Orchestrator.forget()
    let repairNodes = [node("build", .delivery, []), node("review", .review, ["build"]),
                       node("correct", .correction, ["review"]),
                       node("land", .landing, ["correct"])]
    let repair = graph("33333333-3333-4333-8333-333333333333", repairNodes, current: "land")
    hold("build", in: repair, landing: landedReceipt(String(repeating: "d", count: 40)))
    check("a correction the review never demanded does not hold the landing node open",
          state(of: "land", in: repair) == "done")
    hold("correct", in: repair)
    check("a correction that did run and was not landed takes the node back off done",
          state(of: "land", in: repair) == "ready")

    // Two graphs in the record reach their landing node through a review and nothing else. An
    // "every producer landed" rule is vacuously true there, and would report a landing that has
    // no evidence at all behind it.
    Orchestrator.forget()
    let emptyNodes = [node("review", .review, []), node("land", .landing, ["review"])]
    let empty = graph("44444444-4444-4444-8444-444444444444", emptyNodes, current: "land")
    hold("review", in: empty)
    check("a landing node with nothing that produces bytes under it is not done",
          state(of: "land", in: empty) == "blocked")

    // Three landed receipts in the record belong to tasks that timed out or failed after a root
    // had already integrated their work. The receipt is the assertion that the bytes are on the
    // target; the producing task's own exit is a different fact.
    Orchestrator.forget()
    hold("build", in: ordinary, state: .timeout,
         landing: landedReceipt(String(repeating: "e", count: 40)))
    check("a landed receipt still counts when the task that produced it timed out",
          state(of: "land", in: ordinary) == "done" && state(of: "build", in: ordinary) == "failed")

    // The published control sheet and the admission gate read the same derivation, so a landed
    // graph refuses a second landing dispatch instead of inviting one.
    var completeCode = ""
    if case .refused(_, let code, _, _) = Orchestrator.graphAdmissionRefusal(
        graph(ordinary.id, ordinaryNodes, current: "land"),
        taskID: "55555555-5555-4555-8555-555555555555") {
        completeCode = code
    }
    expect("admission calls a landed landing node complete rather than dispatchable",
           completeCode, "graph_node_complete")

    // The review threshold is not what this change touches, and this is the check that says so.
    Orchestrator.forget()
    let findings = Orchestrator.ReviewAxisName.allCases.map {
        Orchestrator.ReviewAxis(
            axis: $0, status: .findings,
            findings: [Orchestrator.ReviewFinding(id: "f1", severity: .minor,
                                                  summary: "one finding", evidence: ["line 1"])])
    }
    hold("build", in: ordinary, landing: landedReceipt(String(repeating: "f", count: 40)))
    hold("review", in: ordinary,
         review: Orchestrator.ReviewReceipt(verdict: .changesRequired, axes: findings))
    check("a changes_required review is still failed beneath a landed landing node",
          state(of: "review", in: ordinary) == "failed"
              && state(of: "land", in: ordinary) == "done")
    Orchestrator.forget()
}

group("the in-flight list answers what a worktree hides") {
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    let repository = "/repo"
    func fixture(_ id: String, title: String, state: Orchestrator.State, secret: String,
                 branch: String?, created: TimeInterval) -> Orchestrator.Task {
        var made = Orchestrator.Task(
            id: id, state: state, kind: "custom", title: title, assistant: .claude,
            projectDir: repository, timeoutMinutes: 30,
            created: Date(timeIntervalSince1970: created),
            rootSessionId: "root-\(id)", rootLabel: "root of \(title)",
            claims: ["Sources/\(title).swift"], claimsDeclared: true,
            secretHash: Orchestrator.hash(ofSecret: secret))
        if state.isTerminal { made.finishedAt = Date(timeIntervalSince1970: created + 60) }
        if let branch {
            made.isolation = .worktree
            made.worktree = Orchestrator.Worktree(
                path: "/worktrees/\(id)", branch: branch, base: "base000",
                repository: repository, cwd: "/worktrees/\(id)", head: "stale00",
                commits: 2, dirty: false)
        }
        return made
    }

    let liveID = "aaaaaaaa-1111-2222-3333-444444444444"
    let deadID = "bbbbbbbb-1111-2222-3333-444444444444"
    let doneID = "cccccccc-1111-2222-3333-444444444444"
    let goneID = "dddddddd-1111-2222-3333-444444444444"
    let otherID = "eeeeeeee-1111-2222-3333-444444444444"
    let secret = String(repeating: "b2", count: 32)

    var live = fixture(liveID, title: "live", state: .briefed, secret: secret,
                       branch: "clawdline/task/aaaa", created: 100)
    live.progress = [Orchestrator.ProgressNote(note: "the real problem is in Settings",
                                               at: Date(timeIntervalSince1970: 150))]
    Orchestrator.holdScheduleTaskForTesting(live)
    // Its session ended without reporting: terminal, but its branch is still there.
    Orchestrator.holdScheduleTaskForTesting(
        fixture(deadID, title: "orphaned", state: .failure, secret: secret,
                branch: "clawdline/task/bbbb", created: 90))
    // Delivered and merged — nothing outstanding.
    Orchestrator.holdScheduleTaskForTesting(
        fixture(doneID, title: "merged", state: .success, secret: secret,
                branch: "clawdline/task/cccc", created: 80))
    // Its branch was disposed of.
    Orchestrator.holdScheduleTaskForTesting(
        fixture(goneID, title: "disposed", state: .success, secret: secret,
                branch: "clawdline/task/dddd", created: 70))
    var elsewhere = fixture(otherID, title: "elsewhere", state: .briefed, secret: secret,
                            branch: nil, created: 60)
    elsewhere.projectDir = "/another-repo"
    Orchestrator.holdScheduleTaskForTesting(elsewhere)

    var branches = Orchestrator.RepositoryBranches()
    branches.known = true
    branches.heads = ["clawdline/task/aaaa": "head0aa", "clawdline/task/bbbb": "head0bb",
                      "clawdline/task/cccc": "head0cc"]
    branches.merged = ["clawdline/task/cccc"]

    let rows = Orchestrator.inflightRecords(repository: repository,
                                            now: Date(timeIntervalSince1970: 400),
                                            branches: branches)
    let ids = rows.compactMap { $0["id"] as? String }
    expect("only outstanding work is listed, newest first", ids, [liveID, deadID])
    expect("a live session says so", rows.first?["visibility"] as? String, "live")
    expect("a finished-but-unmerged delivery says so",
           rows.last?["visibility"] as? String, "unmerged")
    expect("and carries the state that made it terminal", rows.last?["state"] as? String, "failure")

    let liveWorktree = rows.first?["worktree"] as? [String: Any]
    expect("the branch is where the code lives",
           liveWorktree?["branch"] as? String, "clawdline/task/aaaa")
    expect("and the head is what the ref says now, not what was last recorded",
           liveWorktree?["head"] as? String, "head0aa")
    expect("the row says the branch is really there", liveWorktree?["branch_exists"] as? Bool, true)
    expect("and that nobody has merged it", liveWorktree?["merged"] as? Bool, false)
    expect("claims come along", rows.first?["claims"] as? [String], ["Sources/live.swift"])
    expect("so does who has it", rows.first?["root_label"] as? String, "root of live")
    expect("and the age, on the one shared formula",
           rows.first?["age_seconds"] as? Int, 300)
    let notes = rows.first?["progress"] as? [[String: Any]]
    expect("what the session said about itself is the row's best evidence",
           notes?.first?["note"] as? String, "the real problem is in Settings")

    expect("a task in another repository is not this repository's business",
           ids.contains(otherID), false)

    // Unreadable git: every branch fact is unknown, and nothing is erased on that basis.
    let blind = Orchestrator.inflightRecords(repository: repository,
                                             now: Date(timeIntervalSince1970: 400),
                                             branches: Orchestrator.RepositoryBranches())
    expect("when git cannot be asked, every delivery stays visible",
           blind.compactMap { $0["id"] as? String }.sorted(),
           [liveID, deadID, doneID, goneID].sorted())

    expect("a task is left out of its own answer",
           Orchestrator.inflightRecords(repository: repository,
                                        now: Date(timeIntervalSince1970: 400),
                                        branches: branches, excluding: liveID)
               .compactMap { $0["id"] as? String },
           [deadID])

    // The task's own door: its secret, and no repository parameter to get wrong.
    let wrongSecret = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/tasks/\(liveID)/inflight",
        headers: ["X-Clawdline-Task-Secret": String(repeating: "c3", count: 32)]))
    expect("a wrong task secret is forbidden at the in-flight door", wrongSecret.status, 403)
    let noSecret = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/tasks/\(liveID)/inflight"))
    expect("and so is no secret at all", noSecret.status, 403)
    let missing = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/tasks/00000000-0000-0000-0000-000000000000/inflight",
        headers: ["X-Clawdline-Task-Secret": secret]))
    expect("an unknown task is not found rather than forbidden", missing.status, 404)
    let notARepo = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/tasks/\(liveID)/inflight",
        headers: ["X-Clawdline-Task-Secret": secret]))
    check("a task whose tree is not a repository is told so rather than given an empty list",
          notARepo.status == 409 && remoteErrorCode(notARepo) == "not_a_repository")

    let anonymous = RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/inflight?project=/repo"))
    expect("the repository-wide listing stops at the door without a device", anonymous.status, 401)
    let phone = RemoteAuth.addDevice(name: "inflight reader", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: phone.id) }
    let noProject = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/inflight",
        headers: ["Authorization": "Bearer \(phone.token)"]))
    check("a listing with no project is a bad request",
          noProject.status == 400 && remoteErrorCode(noProject) == "bad_request")
    let relative = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/inflight?project=repo",
        headers: ["Authorization": "Bearer \(phone.token)"]))
    expect("and so is a project that is not an absolute path", relative.status, 400)
}

group("a session can say what it is doing without stopping to write a report") {
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()

    let id = "12121212-3434-5656-7878-909090909090"
    let secret = String(repeating: "d4", count: 32)
    let made = Orchestrator.Task(
        id: id, state: .briefed, kind: "custom", title: "the title fixed at dispatch",
        assistant: .claude, projectDir: "/repo", timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 1), rootSessionId: "progress-root",
        secretHash: Orchestrator.hash(ofSecret: secret))
    Orchestrator.holdScheduleTaskForTesting(made)

    func notes(_ reply: Orchestrator.Reply) -> [[String: Any]]? {
        guard case .ok(let body) = reply, let task = body["task"] as? [String: Any]
        else { return nil }
        return task["progress"] as? [[String: Any]]
    }

    if case .refused(let status, let code, _, _) = Orchestrator.recordProgress(
        taskID: id, secret: String(repeating: "0f", count: 32), note: "no") {
        expect("a wrong secret cannot write into somebody else's record", status, 403)
        expect("with the ordinary typed code", code, "forbidden")
    } else { check("a wrong secret is refused", false) }

    if case .refused(let status, _, _, _) = Orchestrator.recordProgress(
        taskID: id, secret: secret, note: "   ") {
        expect("an empty note is a bad request", status, 400)
    } else { check("an empty note is refused", false) }

    let long = String(repeating: "x", count: Orchestrator.progressLimit + 1)
    if case .refused(let status, _, _, _) = Orchestrator.recordProgress(
        taskID: id, secret: secret, note: long) {
        expect("a note past the limit is a bad request", status, 400)
    } else { check("an over-long note is refused", false) }

    let first = Orchestrator.recordProgress(taskID: id, secret: secret,
                                            note: "the fixture needs rewriting too",
                                            now: Date(timeIntervalSince1970: 10))
    expect("a sentence is accepted", notes(first)?.count, 1)
    expect("with the time it was said", notes(first)?.first?["at"] as? Int, 10)

    let repeated = Orchestrator.recordProgress(taskID: id, secret: secret,
                                               note: "the fixture needs rewriting too",
                                               now: Date(timeIntervalSince1970: 11))
    expect("the same sentence twice is a loop, not news", notes(repeated)?.count, 1)

    for step in 1...Orchestrator.progressKept + 2 {
        _ = Orchestrator.recordProgress(taskID: id, secret: secret, note: "step \(step)",
                                        now: Date(timeIntervalSince1970: TimeInterval(20 + step)))
    }
    let kept = Orchestrator.record(id: id)?["progress"] as? [[String: Any]]
    expect("only the newest few are kept", kept?.count, Orchestrator.progressKept)
    expect("and they are the newest", kept?.last?["note"] as? String,
           "step \(Orchestrator.progressKept + 2)")
    expect("oldest first", kept?.first?["note"] as? String, "step 3")

    Orchestrator.saveForTesting()
    Orchestrator.forget()
    let reloaded = Orchestrator.record(id: id)?["progress"] as? [[String: Any]]
    expect("progress survives a registry save/load round trip",
           reloaded?.count, Orchestrator.progressKept)
    expect("with its text intact", reloaded?.last?["note"] as? String,
           "step \(Orchestrator.progressKept + 2)")

    // A finished task's story is its summary. Recording into it would be a second answer to a
    // question already answered, and a live-work list that shows dead work is the failure this
    // whole change exists to avoid.
    Orchestrator.finalize(id, as: .success, summary: "done")
    if case .refused(let status, let code, _, _) = Orchestrator.recordProgress(
        taskID: id, secret: secret, note: "still going") {
        expect("a terminal task cannot record progress", status, 409)
        expect("and is told why", code, "not_live")
    } else { check("a terminal task is refused", false) }

    let path = "/v1/orchestrator/tasks/\(id)/progress"
    let unauthenticated = RemoteServer.shared.route(remoteRequest(
        "POST", path, body: "{\"note\":\"hello\"}"))
    expect("the route reaches the handler without a paired device and is forbidden there",
           unauthenticated.status, 403)
}

group("a sandboxed child's progress arrives as a file, the way its result does") {
    // Measured on this machine (task be9a54c0): a Codex child's sandbox sets
    // CODEX_SANDBOX_NETWORK_DISABLED=1, a curl to 127.0.0.1 exits 7 after 0 ms, DNS itself is
    // off, and no approval prompt ever appears. 133 codex children were briefed to send a
    // progress note over HTTP and 0 notes ever arrived; result.json always worked, because it
    // is a file the broker picks up. So progress gets the same shape: the child replaces
    // progress.json in its own task directory, and the watch beat collects it.
    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    let id = UUID().uuidString.lowercased()
    let directory = Orchestrator.root.appendingPathComponent(id, isDirectory: true)
    defer {
        try? manager.removeItem(at: directory)
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    Orchestrator.forget()
    try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
    let secret = String(repeating: "c3", count: 32)
    var made = Orchestrator.Task(
        id: id, state: .briefed, kind: "custom", title: "a codex child at work",
        assistant: .codex, projectDir: "/repo", timeoutMinutes: 30, created: Date(),
        secretHash: Orchestrator.hash(ofSecret: secret))
    made.briefedAt = Date()
    Orchestrator.holdScheduleTaskForTesting(made)

    let file = directory.appendingPathComponent("progress.json")
    func say(_ note: String, secret: String = secret) {
        try! JSONSerialization.data(withJSONObject: ["task_secret": secret, "note": note])
            .write(to: file, options: .atomic)
    }
    func notes() -> [String] {
        ((Orchestrator.record(id: id)?["progress"] as? [[String: Any]]) ?? [])
            .compactMap { $0["note"] as? String }
    }

    say("reading the fixture first", secret: String(repeating: "0f", count: 32))
    Orchestrator.beat(fromTimer: true)
    expect("a file with the wrong secret writes nothing into the record", notes(), [])

    say("reading the fixture first")
    Orchestrator.beat(fromTimer: true)
    expect("the watch beat collects a valid note with no network call from the child",
           notes(), ["reading the fixture first"])

    Orchestrator.beat(fromTimer: true)
    expect("an unchanged file is not collected twice",
           notes(), ["reading the fixture first"])

    say("the real problem is in the parser")
    Orchestrator.beat(fromTimer: true)
    expect("replacing the file appends the new sentence",
           notes(), ["reading the fixture first", "the real problem is in the parser"])

    // The same sentence through both channels is one piece of news, whichever lands first.
    _ = Orchestrator.recordProgress(taskID: id, secret: secret,
                                    note: "moving to the second half")
    say("moving to the second half")
    Orchestrator.beat(fromTimer: true)
    expect("the same sentence through both channels is recorded once",
           notes(), ["reading the fixture first", "the real problem is in the parser",
                     "moving to the second half"])

    // The replay trap the collected-marker exists for: the file says A, HTTP has since said
    // B, and the next beat still finds A sitting in the file.
    say("checking the fixture again")
    Orchestrator.beat(fromTimer: true)
    _ = Orchestrator.recordProgress(taskID: id, secret: secret, note: "writing the fix")
    Orchestrator.beat(fromTimer: true)
    expect("a beat cannot replay an already-collected file note over a newer HTTP note",
           Array(notes().suffix(2)), ["checking the fixture again", "writing the fix"])

    say(String(repeating: "x", count: Orchestrator.progressLimit + 1))
    Orchestrator.beat(fromTimer: true)
    expect("a note past the limit is ignored, like the route refuses it",
           notes().last, "writing the fix")

    try! Data("{\"task_secret\": \"".utf8).write(to: file)
    Orchestrator.beat(fromTimer: true)
    say("recovered from the half-written file")
    Orchestrator.beat(fromTimer: true)
    expect("a half-written file fails to parse and is simply read again",
           notes().last, "recovered from the half-written file")

    // The restart shape of the same replay trap: the file still holds its last sentence, a
    // newer HTTP note has landed since, and the collected-marker must come back off disk —
    // consecutive dedupe alone cannot save this one.
    _ = Orchestrator.recordProgress(taskID: id, secret: secret, note: "wrapping up")
    Orchestrator.saveForTesting()
    Orchestrator.forget()
    check("the task survives the reload", Orchestrator.record(id: id) != nil)
    Orchestrator.beat(fromTimer: true)
    expect("a restart does not replay a file note the registry already collected",
           notes().last, "wrapping up")
    expect("and nothing was double-collected across the reload",
           notes().count, Orchestrator.progressKept)

    Orchestrator.finalize(id, as: .success, summary: "done")
    say("still going")
    Orchestrator.beat(fromTimer: true)
    check("a finished task's story is its summary; the file is left uncollected",
          !notes().contains("still going"))
}

group("terminal notices remind a root about unrecorded claimed work exactly where needed") {
    var task = Orchestrator.Task(
        id: "cdcdcdcd-efef-0101-2323-454545454545", state: .success, kind: "custom",
        title: "shared-tree delivery", assistant: .codex, projectDir: "/repo",
        timeoutMinutes: 30, created: Date(timeIntervalSince1970: 1),
        claims: ["Sources", "Tests"], claimsDeclared: true,
        secretHash: String(repeating: "0", count: 64))
    let reminder = Orchestrator.landingNotice(for: task)
    check("claimed terminal work with no landing record produces the root reminder",
          !reminder.isEmpty && reminder.contains("landing"))
    for audience in [ClawdlineMessage.Audience.root, .parent] {
        let body = Orchestrator.taskFinishedNotice(for: task, audience: audience)?.body ?? ""
        check("the \(audience.rawValue) completion line appends the reminder once",
              body.hasSuffix(reminder)
                  && body.components(separatedBy: reminder).count - 1 == 1)
    }

    task.untouchedClaims = task.claims
    check("all claims judged untouched suppress the reminder",
          Orchestrator.landingNotice(for: task).isEmpty)
    task.untouchedClaims = ["Tests"]
    check("one touched claim is enough to keep the reminder",
          !Orchestrator.landingNotice(for: task).isEmpty)
    task.landing = Orchestrator.Landing(
        state: .pending, target: "main", delivery: nil, ownerRootKey: "12345678",
        since: Date(timeIntervalSince1970: 2), commit: nil, note: nil)
    check("any existing landing record suppresses the automatic reminder",
          Orchestrator.landingNotice(for: task).isEmpty)
}

group("the terminal claims audit tells touched from untouched from gone") {
    let dir = "/repo"
    var made = Orchestrator.Task(id: taskID, state: .success, kind: "custom", title: "audited",
                                 assistant: .claude, projectDir: dir, timeoutMinutes: 30,
                                 created: Date(timeIntervalSince1970: 1),
                                 claims: ["Sources/touched.swift", "Sources/untouched.swift",
                                          "Sources/gone.swift"],
                                 claimsDeclared: true, secretHash: String(repeating: "0", count: 64))
    made.claimKeys = OrchestratorDraft.freezeClaims(made.claims, projectDir: dir)
    made.spawnedAt = Date(timeIntervalSince1970: 10)
    func mtime(_ path: String) -> Date? {
        switch path {
        case "\(dir)/Sources/touched.swift": return Date(timeIntervalSince1970: 20)
        case "\(dir)/Sources/untouched.swift": return Date(timeIntervalSince1970: 5)
        default: return nil
        }
    }
    let untouched = Orchestrator.untouchedClaims(made, mtime: mtime)
    expect("a file modified after spawn is touched; one before it, or missing, is not",
           untouched.sorted(), ["Sources/gone.swift", "Sources/untouched.swift"])
    var exactlyAtSpawn = made
    exactlyAtSpawn.claims = ["Sources/touched.swift"]
    exactlyAtSpawn.claimKeys = OrchestratorDraft.freezeClaims(exactlyAtSpawn.claims,
                                                              projectDir: dir)
    check("an mtime exactly at spawnedAt counts as touched",
          Orchestrator.untouchedClaims(exactlyAtSpawn,
                                       mtime: { _ in Date(timeIntervalSince1970: 10) }).isEmpty)
    var neverSpawned = made
    neverSpawned.spawnedAt = nil
    check("a task that never spawned has no baseline, so nothing is judged",
          Orchestrator.untouchedClaims(neverSpawned, mtime: mtime).isEmpty)
    var readOnly = made
    readOnly.claims = []
    readOnly.claimKeys = []
    check("a read-only task has nothing to audit",
          Orchestrator.untouchedClaims(readOnly, mtime: mtime).isEmpty)

    var silent = made
    silent.untouchedClaims = []
    check("the finish-line reminder is silent when nothing is untouched",
          Orchestrator.untouchedClaimsNotice(for: silent).isEmpty)
    var flagged = made
    flagged.untouchedClaims = ["Sources/untouched.swift", "Sources/gone.swift"]
    let notice = Orchestrator.untouchedClaimsNotice(for: flagged)
    check("and names the paths plus a hint to declare narrower when it is not",
          notice.contains("2") && notice.contains("Sources/untouched.swift")
              && notice.contains("Sources/gone.swift") && notice.contains("narrower"))

    var manyUntouched = made
    manyUntouched.untouchedClaims = ["Sources/a.swift", "Sources/b.swift", "Sources/c.swift",
                                     "Sources/d.swift", "Sources/e.swift"]
    let truncated = Orchestrator.untouchedClaimsNotice(for: manyUntouched)
    check("the finish-line reminder lists at most three paths and folds the rest into a count, "
          + "so a wide claims declaration cannot type tens of KB into a live tab",
          truncated.contains("5 claimed path(s)")
              && truncated.contains("Sources/a.swift") && truncated.contains("Sources/b.swift")
              && truncated.contains("Sources/c.swift")
              && !truncated.contains("Sources/d.swift") && !truncated.contains("Sources/e.swift")
              && truncated.contains("and 2 more"))

    let ambiguousEndings: [Orchestrator.State] = [.timeout, .spawnFailed, .cancelled]
    for outcome in ambiguousEndings {
        var ended = made
        ended.state = outcome
        expect("\(outcome.rawValue) never produces untouched_claims — its child's actual ending "
               + "is ambiguous, so it gets no \"claim narrower\" advice either",
               Orchestrator.untouchedClaims(ended, mtime: mtime), [])
    }
    var failed = made
    failed.state = .failure
    expect("failure is judged exactly like success",
           Orchestrator.untouchedClaims(failed, mtime: mtime).sorted(),
           ["Sources/gone.swift", "Sources/untouched.swift"])
}

group("finalize runs the claims audit and writes it into the record") {
    defer { Orchestrator.forget() }
    Orchestrator.forget()
    let fm = FileManager.default
    let dir = "/private/tmp/clawdline-audit-\(UUID().uuidString)"
    try! fm.createDirectory(at: URL(fileURLWithPath: dir).appendingPathComponent("Sources"),
                            withIntermediateDirectories: true)
    defer { try? fm.removeItem(atPath: dir) }
    let touchedPath = "\(dir)/Sources/touched.swift"
    let untouchedPath = "\(dir)/Sources/untouched.swift"
    fm.createFile(atPath: touchedPath, contents: Data("old".utf8))
    fm.createFile(atPath: untouchedPath, contents: Data("old".utf8))
    let spawnedAt = Date()
    try! fm.setAttributes([.modificationDate: spawnedAt.addingTimeInterval(-60)],
                          ofItemAtPath: untouchedPath)
    let auditTaskID = "22222222-3333-4444-5555-666666666666"
    var made = Orchestrator.Task(id: auditTaskID, state: .briefed, kind: "custom",
                                 title: "real audit", assistant: .claude, projectDir: dir,
                                 timeoutMinutes: 30, created: spawnedAt,
                                 claims: ["Sources/touched.swift", "Sources/untouched.swift",
                                          "Sources/missing.swift"],
                                 claimsDeclared: true, secretHash: String(repeating: "0", count: 64))
    made.claimKeys = OrchestratorDraft.freezeClaims(made.claims, projectDir: dir)
    made.spawnedAt = spawnedAt
    Orchestrator.holdScheduleTaskForTesting(made)
    try! Data("new".utf8).write(to: URL(fileURLWithPath: touchedPath))

    Orchestrator.finalize(auditTaskID, as: .success, summary: "done")
    let record = Orchestrator.record(id: auditTaskID)
    let untouched = (record?["untouched_claims"] as? [String] ?? []).sorted()
    expect("the record lists exactly what the child never touched",
           untouched, ["Sources/missing.swift", "Sources/untouched.swift"])
}

group("a directory-shaped claim is judged by recursively walking its subtree, not its own mtime") {
    let fm = FileManager.default
    let dir = "/private/tmp/clawdline-dir-audit-\(UUID().uuidString)"
    let sourcesDir = URL(fileURLWithPath: dir).appendingPathComponent("Sources")
    try! fm.createDirectory(at: sourcesDir, withIntermediateDirectories: true)
    defer { try? fm.removeItem(atPath: dir) }
    let filePath = sourcesDir.appendingPathComponent("api.md").path
    fm.createFile(atPath: filePath, contents: Data("old".utf8))
    let spawnedAt = Date()
    // The directory's own mtime is left *before* spawnedAt, matching exactly what a plain
    // `stat` on the declared directory sees when nothing is added, removed, or renamed inside
    // it — the case the review's own experiment found a single-stat audit gets wrong.
    try! fm.setAttributes([.modificationDate: spawnedAt.addingTimeInterval(-60)],
                          ofItemAtPath: sourcesDir.path)
    // In-place append, the same shape as `printf 'b' >> docs/api.md`: rewrites the file's
    // content without renaming or replacing it, so the file's own mtime moves but the
    // directory's never does.
    let handle = try! FileHandle(forWritingTo: URL(fileURLWithPath: filePath))
    handle.seekToEndOfFile()
    handle.write(Data("new".utf8))
    handle.closeFile()

    var made = Orchestrator.Task(id: "33333333-4444-5555-6666-777777777777", state: .success,
                                 kind: "custom", title: "directory audit", assistant: .claude,
                                 projectDir: dir, timeoutMinutes: 30, created: spawnedAt,
                                 claims: ["Sources"], claimsDeclared: true,
                                 secretHash: String(repeating: "0", count: 64))
    made.claimKeys = OrchestratorDraft.freezeClaims(made.claims, projectDir: dir)
    made.spawnedAt = spawnedAt
    expect("a file modified in place inside a claimed directory counts the whole claim as "
          + "touched, even though the directory's own mtime never moved",
           Orchestrator.untouchedClaims(made), [])
}
}
