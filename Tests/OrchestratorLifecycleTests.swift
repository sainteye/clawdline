import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3



func runOrchestratorLifecycleTests() {
group("claims refusals do not spend the dispatch rate budget") {
    Orchestrator.forget()
    for _ in 0..<12 {
        guard let ticket = Orchestrator.takeDispatchRate() else {
            check("a workspace_busy retry can always reach the claims gate", false)
            break
        }
        Orchestrator.refundDispatchRate(ticket)
    }
    let budget = max(10, Config.shared.orchestratorMaxDescendants)
    let accepted = (0..<budget).compactMap { _ in Orchestrator.takeDispatchRate() }
    check("refunded retries leave the full dispatch budget available",
          accepted.count == budget && Orchestrator.takeDispatchRate() == nil)
    Orchestrator.forget()
}

group("serialized operations are acquired atomically and globally") {
    func task(_ id: String, _ state: Orchestrator.State, _ tokens: [String],
              created: TimeInterval, root: String? = nil) -> Orchestrator.Task {
        Orchestrator.Task(id: id, state: state, kind: "custom", title: "a task",
                          assistant: .claude, projectDir: "/tmp", timeoutMinutes: 30,
                          created: Date(timeIntervalSince1970: created),
                          rootSessionId: root, serialize: tokens,
                          secretHash: String(repeating: "0", count: 64))
    }
    let running = task("11111111-1111-1111-1111-111111111111", .briefed, ["build"],
                       created: 1, root: "root-a")
    let first = task("22222222-2222-2222-2222-222222222222", .queued, ["build"],
                     created: 2, root: "root-a")
    let second = task("33333333-3333-3333-3333-333333333333", .queued, ["build"],
                      created: 3, root: "root-b")
    let free = task("44444444-4444-4444-4444-444444444444", .queued, ["release"],
                    created: 4, root: "root-b")
    expect("a live holder blocks the first waiter",
           OrchestratorDraft.serializeBlockers(for: first, among: [running, first, second, free])
               .map(\.id), [running.id])
    expect("FIFO makes an older queued task a blocker",
           OrchestratorDraft.serializeBlockers(for: second, among: [running, first, second, free])
               .map(\.id), [running.id, first.id])
    expect("a different root does not create a different mutex namespace",
           OrchestratorDraft.serializeBlockers(for: second, among: [first, second]).map(\.id),
           [first.id])
    expect("a disjoint operation can start while build is held",
           OrchestratorDraft.serializeBlockers(for: free, among: [running, first, second, free])
               .map(\.id), [])

    let holdsA = task("55555555-5555-5555-5555-555555555555", .briefed, ["a"], created: 1)
    let both = task("66666666-6666-6666-6666-666666666666", .queued, ["a", "b"], created: 2)
    let crossed = task("77777777-7777-7777-7777-777777777777", .queued, ["b", "a"],
                       created: 3)
    expect("a multi-token waiter acquires nothing while any token is held",
           OrchestratorDraft.serializeBlockers(for: both, among: [holdsA, both, crossed]).map(\.id),
           [holdsA.id])
    expect("crossed token order queues behind the older atomic request without deadlock",
           OrchestratorDraft.serializeBlockers(for: crossed, among: [both, crossed]).map(\.id),
           [both.id])

    for terminal in [Orchestrator.State.success, .failure, .timeout, .cancelled, .spawnFailed] {
        let ended = task(running.id, terminal, ["build"], created: 1)
        expect("\(terminal.rawValue) releases its serialized operation",
               OrchestratorDraft.serializeBlockers(for: first, among: [ended, first]).map(\.id), [])
    }
    let ordinary = task("88888888-8888-8888-8888-888888888888", .queued, [], created: 5)
    expect("a task without serialize has zero scheduling behavior",
           OrchestratorDraft.serializeBlockers(for: ordinary, among: [running, first, ordinary])
               .map(\.id), [])
}

group("a serialized waiter survives a store round trip without a plaintext secret") {
    let secret = String(repeating: "c3", count: 32)
    let sealed = Orchestrator.sealQueuedSecret(secret)
    check("the queued secret is sealed rather than stored in plaintext",
          sealed != nil && sealed != secret && !sealed!.contains(secret))
    expect("the installation can reopen it after a restart",
           sealed.flatMap(Orchestrator.openQueuedSecret), secret)
    let keyData = try? Data(contentsOf: Orchestrator.archiveKeyURL)
    let keyText = keyData.flatMap { String(data: $0, encoding: .utf8) }
    check("the archive key is an independent 32-byte random key",
          keyText.flatMap { Data(base64Encoded: $0) }?.count == 32)
    let keyMode = (try? FileManager.default.attributesOfItem(
        atPath: Orchestrator.archiveKeyURL.path)[.posixPermissions] as? NSNumber)?.intValue
    expect("the archive key file is private", keyMode, 0o600)

    // A dispatch-capable child is told how to read this credential. Rotating it must therefore
    // have no effect on the separate at-rest capability.
    try? Data(RemoteAuth.newToken().utf8).write(to: Orchestrator.tokenURL, options: .atomic)
    expect("rotating the orchestrator token cannot decrypt or invalidate queued secrets",
           sealed.flatMap(Orchestrator.openQueuedSecret), secret)
    check("an invalid sealed value is refused",
          Orchestrator.openQueuedSecret("not a sealed secret") == nil)

    try? Data("not a key".utf8).write(to: Orchestrator.archiveKeyURL, options: .atomic)
    let resealed = Orchestrator.sealQueuedSecret(secret)
    let replacement = try? Data(contentsOf: Orchestrator.archiveKeyURL)
    check("an unparsable archive key is replaced with a fresh valid one",
          replacement != keyData
              && replacement.flatMap { String(data: $0, encoding: .utf8) }
                  .flatMap { Data(base64Encoded: $0) }?.count == 32)
    expect("sealing continues after archive-key recovery",
           resealed.flatMap(Orchestrator.openQueuedSecret), secret)

    var task = Orchestrator.Task(id: taskID, state: .queued, kind: "custom", title: "a task",
                                 assistant: .claude, projectDir: "/tmp", timeoutMinutes: 30,
                                 created: Date(), serialize: ["build"],
                                 secretHash: Orchestrator.hash(ofSecret: secret))
    task.queuedSecret = sealed
    let loaded = OrchestratorStore.task(from: OrchestratorStore.stored(task))
    check("restart state keeps the tokens and recoverable secret until the pump starts it",
          loaded?.serialize == ["build"] && loaded?.queuedSecret == sealed
              && loaded?.spawnedAt == nil && loaded?.briefedAt == nil)
}

group("a queued serialized task reports its blockers and cancels immediately") {
    Orchestrator.forget()
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before {
            try? before.write(to: store, options: .atomic)
        } else {
            try? FileManager.default.removeItem(at: store)
        }
        Orchestrator.forget()
    }
    let holderID = "99999999-9999-9999-9999-999999999999"
    let waiterID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
    func row(_ id: String, state: String, created: Double, sealed: String? = nil)
        -> [String: Any] {
        var out: [String: Any] = [
            "id": id, "state": state, "kind": "custom", "title": "a task",
            "assistant": "claude", "project_dir": "/tmp", "timeout_minutes": 30,
            "created": created, "secret_hash": String(repeating: "0", count: 64),
            "serialize": ["build"], "artifacts": [],
        ]
        if let sealed { out["queued_secret"] = sealed }
        return out
    }
    let rows = [
        row(holderID, state: "briefed", created: 1),
        row(waiterID, state: "queued", created: 2, sealed: "unused in this test"),
    ]
    let data = try! JSONSerialization.data(withJSONObject: ["version": 1, "tasks": rows])
    try! FileManager.default.createDirectory(at: store.deletingLastPathComponent(),
                                             withIntermediateDirectories: true)
    try! data.write(to: store, options: .atomic)

    expect("GET records identify the task currently blocking a serialized waiter",
           Orchestrator.record(id: waiterID)?["waiting_on"] as? [String], [holderID])
    _ = Orchestrator.cancel(taskID: waiterID)
    check("cancelling queued work is observable before cancel returns and opens no child",
          Orchestrator.record(id: waiterID)?["state"] as? String == "cancelled"
              && Orchestrator.record(id: waiterID)?["child"] == nil)
}

group("a queued task scans and types workspace warnings only when promoted") {
    Orchestrator.forget()
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        Orchestrator.workspaceOverlapObserverForTesting = nil
        Orchestrator.drainSerializePumpForTesting()
        if let before {
            try? before.write(to: store, options: .atomic)
        } else {
            try? FileManager.default.removeItem(at: store)
        }
        Orchestrator.forget()
    }
    let holderID = "a1000000-0000-0000-0000-000000000001"
    let activeID = "a2000000-0000-0000-0000-000000000002"
    let waiterID = "a3000000-0000-0000-0000-000000000003"
    let secret = String(repeating: "ab", count: 32)
    let rows: [[String: Any]] = [
        [
            "id": holderID, "state": "briefed", "kind": "custom", "title": "holder",
            "assistant": "claude", "project_dir": "/unrelated", "timeout_minutes": 30,
            "created": 1.0, "root_session": "root-holder",
            "secret_hash": String(repeating: "0", count: 64),
            "serialize": ["build"], "artifacts": [],
        ],
        [
            "id": activeID, "state": "briefed", "kind": "custom", "title": "active",
            "assistant": "claude", "project_dir": "/a/b", "timeout_minutes": 30,
            "created": 2.0, "root_session": "root-active",
            "secret_hash": String(repeating: "0", count: 64), "artifacts": [],
        ],
        [
            "id": waiterID, "state": "queued", "kind": "custom", "title": "waiter",
            "assistant": "claude", "project_dir": "/a/b/child", "timeout_minutes": 30,
            "created": 3.0, "root_session": "root-waiter",
            "secret_hash": Orchestrator.hash(ofSecret: secret),
            "serialize": ["build"],
            "queued_secret": Orchestrator.sealQueuedSecret(secret)!, "artifacts": [],
        ],
    ]
    let data = try! JSONSerialization.data(withJSONObject: ["version": 1, "tasks": rows])
    try! data.write(to: store, options: .atomic)
    Orchestrator.load()

    let observedLock = NSLock()
    var observed: [OrchestratorDraft.WorkspaceOverlapNotice] = []
    var observedOffMain = false
    Orchestrator.workspaceOverlapObserverForTesting = { task, overlaps in
        guard task.id == waiterID else { return }
        let notices = Orchestrator.workspaceOverlapNotices(newTask: task, overlaps: overlaps)
        observedLock.lock()
        observed = notices
        observedOffMain = !Thread.isMainThread
        observedLock.unlock()
    }
    check("the queued waiter has no dispatch-time workspace warning",
          OrchestratorDraft.workspaceOverlaps(
            for: OrchestratorStore.task(from: rows[2])!,
            among: [OrchestratorStore.task(from: rows[1])!]
          ).isEmpty)

    Orchestrator.finalize(holderID, as: .success, summary: "release")
    let warned = eventually {
        observedLock.lock(); defer { observedLock.unlock() }
        return observed.count == 2
    }
    Orchestrator.drainSerializePumpForTesting()
    observedLock.lock()
    let lines = observed.map(\.line)
    let offMain = observedOffMain
    observedLock.unlock()
    check("promotion runs the current overlap scan through typed-line notification",
          warned && lines.allSatisfy { $0.contains("workspace overlap") }
              && lines.contains { $0.contains(activeID.prefix(8)) })
    check("promotion and its following terminal spawn run off the main thread", offMain)
}

group("startup pumps a recoverable serialized waiter exactly once") {
    Orchestrator.forget()
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before {
            try? before.write(to: store, options: .atomic)
        } else {
            try? FileManager.default.removeItem(at: store)
        }
        Orchestrator.forget()
    }
    let id = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
    let secret = String(repeating: "d4", count: 32)
    let row: [String: Any] = [
        "id": id, "state": "queued", "kind": "custom", "title": "a task",
        "assistant": "claude", "project_dir": "/path-that-does-not-exist-clawdline-test",
        "timeout_minutes": 30, "created": Date().timeIntervalSince1970,
        "secret_hash": Orchestrator.hash(ofSecret: secret), "serialize": ["build"],
        "queued_secret": Orchestrator.sealQueuedSecret(secret)!, "artifacts": [],
    ]
    let data = try! JSONSerialization.data(withJSONObject: ["version": 1, "tasks": [row]])
    try! data.write(to: store, options: .atomic)
    Orchestrator.forget()

    Orchestrator.resumeAfterRestart()
    let pumped = eventually {
        Orchestrator.record(id: id)?["state"] as? String == "spawn_failed"
    }
    Orchestrator.drainSerializePumpForTesting()
    let record = Orchestrator.record(id: id)
    check("startup recovered and pumped the waiter rather than leaving it queued",
          pumped && record?["state"] as? String == "spawn_failed"
              && (record?["summary"] as? String)?.contains("not_found") == true)
    let saved = try! JSONSerialization.jsonObject(with: Data(contentsOf: store))
        as! [String: Any]
    let savedRow = (saved["tasks"] as! [[String: Any]]).first!
    check("crossing the startup pump deletes the recoverable secret before opening",
          savedRow["queued_secret"] == nil && savedRow["state"] as? String == "spawn_failed")
}

group("every terminal outcome really pumps the next serialized waiter") {
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        Orchestrator.drainSerializePumpForTesting()
        if let before {
            try? before.write(to: store, options: .atomic)
        } else {
            try? FileManager.default.removeItem(at: store)
        }
        Orchestrator.forget()
    }
    let outcomes: [Orchestrator.State] = [
        .success, .failure, .timeout, .cancelled, .spawnFailed,
    ]
    let holderIDs = [
        "c0000000-0000-0000-0000-000000000001",
        "c0000000-0000-0000-0000-000000000002",
        "c0000000-0000-0000-0000-000000000003",
        "c0000000-0000-0000-0000-000000000004",
        "c0000000-0000-0000-0000-000000000005",
    ]
    let waiterIDs = [
        "d0000000-0000-0000-0000-000000000001",
        "d0000000-0000-0000-0000-000000000002",
        "d0000000-0000-0000-0000-000000000003",
        "d0000000-0000-0000-0000-000000000004",
        "d0000000-0000-0000-0000-000000000005",
    ]

    for index in outcomes.indices {
        Orchestrator.forget()
        let holderID = holderIDs[index]
        let waiterID = waiterIDs[index]
        let secret = String(repeating: String(index + 1), count: 64)
        let rows: [[String: Any]] = [
            [
                "id": holderID, "state": "briefed", "kind": "custom", "title": "holder",
                "assistant": "claude", "project_dir": "/tmp", "timeout_minutes": 30,
                "created": 1.0, "secret_hash": String(repeating: "0", count: 64),
                "serialize": ["build"], "artifacts": [],
            ],
            [
                "id": waiterID, "state": "queued", "kind": "custom", "title": "waiter",
                "assistant": "claude",
                "project_dir": "/path-that-does-not-exist-clawdline-pump-test-\(index)",
                "timeout_minutes": 30, "created": 2.0,
                "secret_hash": Orchestrator.hash(ofSecret: secret),
                "serialize": ["build"], "claims": ["owned-output"],
                "queued_secret": Orchestrator.sealQueuedSecret(secret)!, "artifacts": [],
            ],
        ]
        let data = try! JSONSerialization.data(withJSONObject: ["version": 1, "tasks": rows])
        try! data.write(to: store, options: .atomic)
        Orchestrator.load()

        Orchestrator.finalize(holderID, as: outcomes[index], summary: "terminal test")
        let pumped = eventually {
            Orchestrator.record(id: waiterID)?["state"] as? String == "spawn_failed"
        }
        Orchestrator.drainSerializePumpForTesting()
        let holder = Orchestrator.record(id: holderID)
        let waiter = Orchestrator.record(id: waiterID)
        check("\(outcomes[index].rawValue) schedules the pump",
              pumped && holder?["state"] as? String == outcomes[index].rawValue)
        check("\(outcomes[index].rawValue) pump records the terminal tab-opening refusal",
              waiter?["state"] as? String == "spawn_failed"
                  && waiter?["finishedAt"] != nil
                  && (waiter?["summary"] as? String)?.contains("not_found") == true)
        let saved = try! JSONSerialization.jsonObject(with: Data(contentsOf: store))
            as! [String: Any]
        let savedWaiter = (saved["tasks"] as! [[String: Any]])
            .first { $0["id"] as? String == waiterID }
            .flatMap(OrchestratorStore.task(from:))!
        let retry = Orchestrator.Task(
            id: taskID, state: .queued, kind: "custom", title: "retry",
            assistant: .claude, projectDir: savedWaiter.projectDir, timeoutMinutes: 30,
            created: Date(), rootSessionId: "a-different-root", claims: ["owned-output"],
            secretHash: String(repeating: "0", count: 64))
        expect("a pump spawn failure releases its queued claim",
               OrchestratorDraft.claimsOverlaps(for: retry, among: [savedWaiter]).count, 0)
    }
}

group("a task secret is kept as a hash and compared as one") {
    let secret = String(repeating: "a1", count: 32)
    let stored = Orchestrator.hash(ofSecret: secret)
    expect("the stored form is a SHA-256 in hex", stored.count, 64)
    check("and it is not the secret", stored != secret)
    expect("the same secret hashes the same way twice",
           Orchestrator.hash(ofSecret: secret), stored)
    check("a different one does not",
          Orchestrator.hash(ofSecret: String(repeating: "b2", count: 32)) != stored)
    check("the child's secret verifies against what was kept",
          RemoteAuth.constantTimeEquals(stored, Orchestrator.hash(ofSecret: secret)))
    check("and somebody else's does not",
          !RemoteAuth.constantTimeEquals(stored,
                                         Orchestrator.hash(ofSecret: secret + "0")))

    // The other credential: the one that says a local process asked, which is a different claim
    // from "this device is paired" and is deliberately not the same string.
    check("no dispatch token is not the dispatch token", !Orchestrator.verifyDispatch(token: nil))
    check("nor is an empty one", !Orchestrator.verifyDispatch(token: ""))
    check("nor is a guess", !Orchestrator.verifyDispatch(token: String(repeating: "0", count: 44)))
    check("the minted one is", Orchestrator.verifyDispatch(token: Orchestrator.dispatchToken()))
}

group("what a child's turn cost, at the prices this app knows") {
    expect("Opus, in", Orchestrator.price(forModel: "claude-opus-5-20260201")?.input, 5)
    expect("Opus, out", Orchestrator.price(forModel: "claude-opus-5-20260201")?.output, 25)
    expect("Sonnet, in", Orchestrator.price(forModel: "claude-sonnet-4-5")?.input, 3)
    expect("Fable, out", Orchestrator.price(forModel: "claude-fable-5")?.output, 50)
    expect("Haiku, in", Orchestrator.price(forModel: "claude-haiku-4-5")?.input, 1)
    // Codex bills against a plan rather than per token, so there is no honest number to give.
    check("a model nobody has a price for", Orchestrator.price(forModel: "gpt-5.6-luna") == nil)
    check("and no model at all", Orchestrator.price(forModel: nil) == nil)

    func opus(input: Int = 0, output: Int = 0, cacheRead: Int = 0,
              cacheWrite: Int = 0) -> Orchestrator.Usage {
        var usage = Orchestrator.Usage()
        usage.model = "claude-opus-5-20260201"
        usage.input = input
        usage.output = output
        usage.cacheRead = cacheRead
        usage.cacheWrite = cacheWrite
        return usage
    }
    expect("a million input tokens is the input price", Orchestrator.cost(of: opus(input: 1_000_000)), 5)
    expect("a million output tokens is the output price",
           Orchestrator.cost(of: opus(output: 1_000_000)), 25)
    expect("a cache read is a tenth of an input token",
           Orchestrator.cost(of: opus(cacheRead: 1_000_000)), 0.5)
    expect("a cache write is an input token and a quarter",
           Orchestrator.cost(of: opus(cacheWrite: 1_000_000)), 6.25)
    expect("and the four are added up",
           Orchestrator.cost(of: opus(input: 1_000_000, output: 1_000_000,
                                      cacheRead: 1_000_000, cacheWrite: 1_000_000)),
           36.75)
    expect("the answer is money, so it stops at four places",
           Orchestrator.cost(of: opus(input: 100)), 0.0005)
    expect("and a single token rounds away rather than inventing a digit",
           Orchestrator.cost(of: opus(input: 1)), 0)
    var unpriced = Orchestrator.Usage()
    unpriced.model = "gpt-5.6-luna"
    unpriced.input = 1_000_000
    check("tokens nobody has a price for cost nothing that can be said",
          Orchestrator.cost(of: unpriced) == nil)
}

group("token accounting reads only a transcript proved for its task") {
    let fm = FileManager.default
    let dir = fm.temporaryDirectory
        .appendingPathComponent("clawdline-usage-identity-\(UUID().uuidString)", isDirectory: true)
    try! fm.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? fm.removeItem(at: dir) }

    let usage = #"{"type":"assistant","message":{"model":"claude-opus-5","usage":{"input_tokens":37,"output_tokens":5}}}"#
    func receipt(_ id: String) -> String {
        """
        {"type":"user","message":{"role":"user",\
        "content":"You are a Clawdline CHILD agent for task \(id)."}}
        """
    }
    let sibling = dir.appendingPathComponent("sibling.jsonl")
    try! Data((receipt("11111111-2222-3333-4444-555555555555") + "\n" + usage + "\n").utf8)
        .write(to: sibling)
    let own = dir.appendingPathComponent("own.jsonl")
    try! Data((receipt(taskID) + "\n" + usage + "\n").utf8).write(to: own)

    var task = Orchestrator.Task(id: taskID, state: .briefed, kind: "custom", title: "a task",
                                 assistant: .claude, projectDir: "/tmp", timeoutMinutes: 30,
                                 created: Date(), secretHash: String(repeating: "0", count: 64))
    task.transcriptPath = sibling.path
    check("a sibling's usage is not charged to an unproven task",
          Orchestrator.harvestUsage(task) == nil)
    task.transcriptPath = own.path
    expect("the same unpersisted identity may be proved from its own marker",
           Orchestrator.harvestUsage(task)?.input, 37)
}

group("orchestrator task states only move forward") {
    check("a briefed task cannot become spawning again",
          !Orchestrator.mayReplaceState(.briefed, with: .spawning))
    check("a terminal task cannot be resurrected",
          !Orchestrator.mayReplaceState(.success, with: .briefed))
    check("a spawning task may advance to briefed",
          Orchestrator.mayReplaceState(.spawning, with: .briefed))
    check("a spawning task may fail terminally",
          Orchestrator.mayReplaceState(.spawning, with: .spawnFailed))
    check("same-state field enrichment remains possible",
          Orchestrator.mayReplaceState(.briefed, with: .briefed))
}

group("an orchestrated Claude child keeps its process identity") {
    let started = Date(timeIntervalSince1970: 2_000)
    var task = Orchestrator.Task(id: taskID, state: .spawning, kind: "custom",
                                 title: "a task", assistant: .claude,
                                 projectDir: "/tmp", timeoutMinutes: 30,
                                 created: Date(), secretHash: String(repeating: "0", count: 64))
    task.childPID = 100
    task.childProcStart = started

    let stored = OrchestratorStore.stored(task)
    let loaded = OrchestratorStore.task(from: stored)
    check("the recorded process pair survives a store round trip",
          loaded?.childPID == 100 && loaded?.childProcStart == started)

    // These checks exercise the policy value after the polling caller assembles it; the separate
    // complete-pair check below covers the caller-side recording seam. Removing the pid/start
    // guard in `identityStep` must make this compound assertion fail.
    let same = Orchestrator.ChildObservation(pid: 100,
                                             procStart: started.addingTimeInterval(5))
    let foreignPID = Orchestrator.ChildObservation(pid: 200, procStart: started)
    let recycledPID = Orchestrator.ChildObservation(pid: 100,
                                                    procStart: started.addingTimeInterval(6))
    let missingStart = Orchestrator.ChildObservation(pid: 100, procStart: nil)
    var missingRecordedStart = task
    missingRecordedStart.childProcStart = nil
    check("a live process match is unchanged while a foreign or recycled process is refused",
          Orchestrator.identityStep(for: task, seeing: same) == .none
            && Orchestrator.identityStep(for: task, seeing: foreignPID)
                == .refuseForeignProcess(seen: 200)
            && Orchestrator.identityStep(for: task, seeing: recycledPID)
                == .refuseForeignProcess(seen: 100)
            && Orchestrator.identityStep(for: task, seeing: missingStart)
                == .refuseForeignProcess(seen: 100)
            && Orchestrator.identityStep(for: missingRecordedStart, seeing: same)
                == .refuseForeignProcess(seen: 100))

    var unrecorded = Orchestrator.Task(id: taskID, state: .spawning, kind: "custom",
                                       title: "a task", assistant: .claude,
                                       projectDir: "/tmp", timeoutMinutes: 30,
                                       created: Date(),
                                       secretHash: String(repeating: "0", count: 64))
    let incomplete = Orchestrator.ChildObservation(pid: 101, procStart: nil)
    let complete = Orchestrator.ChildObservation(pid: 101, procStart: started)
    // Removing either half of recordProcessIdentity's pair guard must fail this caller check.
    check("the polling caller records only a same-source complete process pair",
          !Orchestrator.recordProcessIdentity(from: incomplete, in: &unrecorded)
            && unrecorded.childPID == nil && unrecorded.childProcStart == nil
            && Orchestrator.recordProcessIdentity(from: complete, in: &unrecorded)
            && unrecorded.childPID == 101 && unrecorded.childProcStart == started)
}

group("an orchestrated child's cwd") {
    let projectDir = "/tmp/a project"
    let task = Orchestrator.Task(id: taskID, state: .spawning, kind: "custom",
                                 title: "a task", assistant: .claude,
                                 projectDir: projectDir, timeoutMinutes: 30,
                                 created: Date(), secretHash: String(repeating: "0", count: 64))
    expect("it is the dispatch project until isolation supplies another directory",
           Orchestrator.cwd(of: task), projectDir)
}

group("an orchestrated Claude child prefers registry identity") {
    let registryID = "8e29b3df-1af7-40af-996f-3f969782c064"
    let legacyID = "6a2b2ad0-3574-40eb-ba41-50c93c83c201"
    let projectDir = "/tmp/a project"
    var task = Orchestrator.Task(id: taskID, state: .spawning, kind: "custom",
                                 title: "a task", assistant: .claude,
                                 projectDir: projectDir, timeoutMinutes: 30,
                                 created: Date(), secretHash: String(repeating: "0", count: 64))
    let registryTranscript = URL(fileURLWithPath: "/tmp/registry.jsonl")
    let withRegistry = Orchestrator.ChildObservation(pid: 100, procStart: Date(),
                                                     registrySessionID: registryID,
                                                     registryTranscript: registryTranscript)
    let beforeTranscript = Orchestrator.ChildObservation(pid: 100, procStart: Date(),
                                                         registrySessionID: registryID)
    let withoutRegistry = Orchestrator.ChildObservation(pid: 100, procStart: Date())

    // Removing the validated-transcript requirement must make the middle assertion fail.
    check("registry is primary only after its named transcript is validated",
          Orchestrator.identityStep(for: task, seeing: withRegistry)
            == .useRegistry(sessionID: registryID, transcript: registryTranscript)
            && Orchestrator.identityStep(for: task, seeing: beforeTranscript) == .none
            && Orchestrator.identityStep(for: task, seeing: withoutRegistry) == .none)

    task.childSessionId = legacyID
    task.transcriptPath = "/tmp/legacy.jsonl"
    let mismatch = Orchestrator.identityComparison(registrySessionID: registryID,
                                                   registryTranscript: registryTranscript,
                                                   legacyTask: task)
    var agreeing = task
    agreeing.childSessionId = registryID
    agreeing.transcriptPath = nil

    // Removing the session-id mismatch guard must make the agreeing half of this check fail.
    check("the audit comparison carries both answers and ignores only a missing old path",
          mismatch == Orchestrator.IdentityComparison(
            registrySessionID: registryID,
            registryTranscriptPath: registryTranscript.path,
            legacySessionID: legacyID,
            legacyTranscriptPath: "/tmp/legacy.jsonl"
          ) && Orchestrator.identityComparison(registrySessionID: registryID,
                                               registryTranscript: registryTranscript,
                                               legacyTask: agreeing) == nil)

    var pinned = task
    pinned.state = .briefed
    pinned.childSessionId = legacyID
    pinned.transcriptPath = "/tmp/briefed.jsonl"
    pinned.transcriptProven = true
    // Removing adoptRegistryIdentity's briefed-and-proven guard must replace this caller's pair.
    check("a delivered identity is pinned at the registry adoption caller",
          !Orchestrator.adoptRegistryIdentity(sessionID: registryID,
                                              transcript: registryTranscript, in: &pinned)
            && pinned.childSessionId == legacyID
            && pinned.transcriptPath == "/tmp/briefed.jsonl"
            && pinned.transcriptProven)

    var control = task
    // Removing beginRegistryControl's equality guard must make the second sample run again.
    check("the transition control samples once per distinct registry answer",
          Orchestrator.beginRegistryControl(for: registryID, in: &control)
            && !Orchestrator.beginRegistryControl(for: registryID, in: &control)
            && Orchestrator.beginRegistryControl(for: legacyID, in: &control))
}

group("an orchestrated child only inherits identity from this spawn") {
    let spawnedAt = Date(timeIntervalSince1970: 2_000)
    let old = HookBridge.Note(event: .sessionStart, tty: "ttys004",
                              at: Date(timeIntervalSince1970: 1_000), session: "previous")
    let current = HookBridge.Note(event: .sessionStart, tty: "ttys004",
                                  at: Date(timeIntervalSince1970: 2_001), session: "current")
    let equal = HookBridge.Note(event: .sessionStart, tty: "ttys004",
                                at: spawnedAt, session: "same-second")

    expect("a note left on the tty before this spawn has no child identity",
           Orchestrator.childSessionID(from: old, spawnedAt: spawnedAt), nil)
    expect("a note emitted after this spawn supplies the child identity",
           Orchestrator.childSessionID(from: current, spawnedAt: spawnedAt), "current")
    expect("the integer-second equality boundary is accepted",
           Orchestrator.childSessionID(from: equal, spawnedAt: spawnedAt), "same-second")
}

group("an orchestrated child is briefed only at a real composer") {
    // This is the shape that triggered the bug: Claude's process is present and its banner is
    // readable, but MCP startup has not finished and no input row exists yet. SessionState calls
    // it idle because there is neither a menu nor a spinner; the orchestrator must not.
    let starting = """
    ╭──────────────────────────────────────╮
    │ ✻ Welcome to Claude Code             │
    │ MCP server serena: connecting…       │
    │ MCP server headroom: starting…       │
    ╰──────────────────────────────────────╯
    """
    expect("the shared screen state has the ambiguous old answer",
           SessionState.read(starting, assistant: .claude), .idle)
    check("but a startup banner is not an input-ready child",
          !Orchestrator.briefingInputReady(starting, assistant: .claude))
    expect("so the first message waits",
           Orchestrator.briefingDecision(screen: starting, assistant: .claude,
                                         transcript: nil, transcriptKnown: true,
                                         taskID: taskID, attempts: 0,
                                         secondsSinceAttempt: nil), .wait)

    let claudeReady = """
    ╭──────────────────────────────────────╮
    │ Welcome back                         │
    ╰──────────────────────────────────────╯

    ❯

      ? for shortcuts
    """
    check("Claude's empty composer is positive readiness",
          Orchestrator.briefingInputReady(claudeReady, assistant: .claude))
    expect("the first attempt may be sent there",
           Orchestrator.briefingDecision(screen: claudeReady, assistant: .claude,
                                         transcript: nil, transcriptKnown: false,
                                         taskID: taskID, attempts: 0,
                                         secondsSinceAttempt: nil), .send)

    // The shapes above are tidied. What iTerm2 actually hands back has U+00A0 after the caret,
    // and a brand-new session carries the suggestion rather than a bare caret — which is the one
    // a child is briefed at. Written with an escape because the character is invisible in a
    // diff, and a plain space here would put the bug back without failing anything.
    let nbsp = "\u{00A0}"
    check("the caret's real separator is U+00A0, not a space",
          Orchestrator.briefingInputReady("❯\(nbsp)   ", assistant: .claude))
    check("and a new session's suggestion is the composer a child is briefed at",
          Orchestrator.briefingInputReady("❯\(nbsp)Try \"edit a file to fix a bug\"",
                                          assistant: .claude))
    check("a draft already in the composer is not a fresh child",
          !Orchestrator.briefingInputReady("❯\(nbsp)/deploy", assistant: .claude))

    let codexReady = """
    › Ask Codex to do anything

      gpt-5.6-sol default · ~/code/clawdline
    """
    check("Codex's composer is recognised independently",
          Orchestrator.briefingInputReady(codexReady, assistant: .codex))
}

group("briefing delivery is verified and retries are bounded") {
    let ready = "❯\n\n  ? for shortcuts"
    let marker = "You are a Clawdline CHILD agent for task \(taskID). Read CHILD.md."
    let claudeReceipt = """
    {"type":"user","message":{"role":"user","content":"\(marker)"}}
    """
    expect("a named user turn is the receipt",
           Orchestrator.briefingDecision(screen: nil, assistant: .claude,
                                         transcript: claudeReceipt, transcriptKnown: true,
                                         taskID: taskID, attempts: 5,
                                         secondsSinceAttempt: 60), .accepted)

    expect("a receipt is given time to appear before any retry",
           Orchestrator.briefingDecision(screen: ready, assistant: .claude,
                                         transcript: nil, transcriptKnown: true,
                                         taskID: taskID, attempts: 1,
                                         secondsSinceAttempt: 5), .wait)
    expect("absence in an unidentified transcript can never justify a duplicate",
           Orchestrator.briefingDecision(screen: ready, assistant: .claude,
                                         transcript: nil, transcriptKnown: false,
                                         taskID: taskID, attempts: 1,
                                         secondsSinceAttempt: 60), .wait)
    expect("the attempt below the ceiling may retry after the receipt window",
           Orchestrator.briefingDecision(screen: ready, assistant: .claude,
                                         transcript: nil, transcriptKnown: true,
                                         taskID: taskID,
                                         attempts: Orchestrator.briefingAttemptLimit - 1,
                                         secondsSinceAttempt: 60), .send)
    expect("the ceiling stops another copy from being sent",
           Orchestrator.briefingDecision(screen: ready, assistant: .claude,
                                         transcript: nil, transcriptKnown: true,
                                         taskID: taskID,
                                         attempts: Orchestrator.briefingAttemptLimit,
                                         secondsSinceAttempt: 60), .exhausted)

    let codexReceipt = """
    {"type":"event_msg","payload":{"type":"item_completed","item":\
    {"type":"UserMessage","content":[{"type":"text","text":"\(marker)"}]}}}
    """
    check("Codex's rollout closes the same retry gate",
          Orchestrator.transcriptContainsBriefing(codexReceipt, assistant: .codex,
                                                  taskID: taskID))
}

group("the one line a child is given") {
    let secret = String(repeating: "c3", count: 32)
    let line = Orchestrator.firstLine(id: taskID, secret: secret)
    check("carries the secret, because nothing else ever will",
          line.contains("TASK_SECRET=" + secret))
    check("and names the file that says what to do",
          line.contains("/tmp/.clawdline/\(taskID)/CHILD.md"))
    check("and it is one line, because it is typed into a prompt", !line.contains("\n"))
}

group("a child briefing carries the whole of what a child needs, and none of what it must not") {
    // CHILD.md is the whole of a child's instructions: it is read once, by a session nobody is
    // watching, and anything not in it does not happen. The tree is one level deep, so every
    // child is a leaf and there is only one briefing to get right — the level a child stands on
    // is asserted in the group below this one.
    func task(depth: Int = 1) -> Orchestrator.Task {
        Orchestrator.Task(id: taskID, state: .briefed, kind: "custom", title: "a task",
                          assistant: .claude, projectDir: "/Users/me/code/thing",
                          timeoutMinutes: 30, created: Date(), depth: depth,
                          secretHash: String(repeating: "0", count: 64))
    }
    let brief = Orchestrator.childBrief(for: task())

    // The measured finding the dispatch teaching was split out over: 28,323 characters of it
    // went into every one of 206 direct children's briefings, and not one of the 206 ever
    // dispatched anything. None of it comes back now that nothing may dispatch at all.
    check("the dispatch recipe is in no briefing any more",
          !brief.contains("## Handing work on")
            && !brief.contains("cat > /tmp/.clawdline/$sub/task.json <<JSON"))
    check("nor a convenience summary of the credential",
          !brief.contains("orchestrator-token") && !brief.contains("X-Clawdline-Orchestrator"))
    check("nor the one field nothing else would tell a dispatcher",
          !brief.contains("parent_task"))
    check("and reporting says to use the file tool rather than a shell line",
          brief.contains("Write it with your file-writing tool, not with a shell command"))
    check("and it learns the narrow push opening without needing a skill",
          brief.contains("/v1/orchestrator/tasks/\(taskID)/notify")
              && brief.contains("The value of push is rarity")
              && brief.contains("Routine results belong in `result.json`")
              && brief.contains("at most 5 notifications")
              && brief.contains("at most 30 per hour"))
    check("and it is told where the answer will appear",
          brief.contains("result.json"))
    // The ask nothing was making: AGENTS.md, docs/dispatching.md and the dispatch policy all
    // require a first progress note, and the briefing — the only thing a child actually reads —
    // asked only for one when the work drifted. A note at minute three is what lets a wrong
    // direction be cancelled before it has spent a session.
    check("a child is asked for one progress note before it starts, not only when it drifts",
          brief.contains("within about three minutes of starting")
            && brief.contains("before you begin the work"))
    check("and told why, because a child that knows why will actually send it",
          brief.contains("cancelled at minute three instead of minute twenty-six")
            && brief.contains("18.5M and 16.5M tokens"))
    check("the at-rest archive key is never named in a child briefing",
          !brief.contains("orchestrator-archive-key") && !brief.contains("archive key"))
}

group("a child is the bottom of the tree, and no setting can put a level under it") {
    // The tree is one level deep as a structural fact rather than as a default, and the reason
    // is the shape of `config.json`: it is seeded once and never migrated, so a changed default
    // reaches only Macs that have never run this app. Every machine that has one already carries
    // `orchestrator_max_grandchildren: 3`, and if the floor were still read out of that key, all
    // of them would have gone on dispatching grandchildren after the default moved under them.
    // So the floor is a constant, the setting is gone from `Config`, and an old file that still
    // names the key is read by nothing.
    func child(depth: Int) -> Orchestrator.Task {
        Orchestrator.Task(id: taskID, state: .briefed, kind: "custom", title: "a task",
                          assistant: .claude, projectDir: "/Users/me/code/thing",
                          timeoutMinutes: 30, created: Date(), depth: depth,
                          secretHash: String(repeating: "0", count: 64))
    }

    expect("the floor is one level", Orchestrator.depthFloor, 1)
    check("a root's child is allowed to exist", Orchestrator.depthIsAllowed(1))
    check("and the task that child would have opened is not",
          !Orchestrator.depthIsAllowed(2))

    // The nail. A Mac that has run any earlier version of this app has the old key in its config
    // file saying `3`; this is that machine, and the answer has to be the same one.
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-config-\(UUID().uuidString)", isDirectory: true)
    try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let legacy = """
    {"orchestrator_max_grandchildren": 3, "orchestrator_max_children": 4}
    """
    try? Data(legacy.utf8).write(to: directory.appendingPathComponent("config.json"))
    let stale = Config(directoryForTesting: directory)
    expect("an old config naming the removed key still loads its neighbours",
           stale.orchestratorMaxChildren, 4)
    check("and the key it still names is read by nothing",
          !Mirror(reflecting: stale).children.contains {
              ($0.label ?? "").lowercased().contains("grandchild")
          })
    expect("the floor is unmoved by a file that says three", Orchestrator.depthFloor, 1)
    check("so a child on that Mac still may not open a task",
          !Orchestrator.depthIsAllowed(2))
    check("and the machine-wide ceiling is not quietly tightened by the key going away",
          stale.orchestratorMaxDescendants == 16
              && Config.shared.orchestratorMaxDescendants == 20,
          "\(stale.orchestratorMaxDescendants) and \(Config.shared.orchestratorMaxDescendants)")

    let brief = Orchestrator.childBrief(for: child(depth: 1))
    check("CHILD.md tells a child plainly that it is the bottom",
          brief.contains("You are the bottom of this tree: you cannot dispatch Clawdline tasks"))
    check("and sends it to its own assistant's subagents for anything parallel",
          brief.contains("subagent"))
    check("no child is pointed at a dispatch recipe any more",
          !brief.contains("DISPATCHING.md") && !brief.contains("child sessions of your own"))
    // Two sentences elsewhere in the briefing were written for a child that could dispatch, and
    // survived the level going away because neither says "dispatch" in a way a grep for the
    // recipe would find. The reading rule let a child open a task directory it had opened
    // itself, and the honesty rule asked it to account for sessions it can no longer start.
    check("and nothing else in the briefing assumes it has tasks of its own",
          !brief.contains("any you dispatched") && !brief.contains("sessions you dispatched"))
}

group("a codex child is briefed with channels it can actually reach") {
    // Measured on this machine (task be9a54c0): CODEX_SANDBOX_NETWORK_DISABLED=1, curl to
    // 127.0.0.1 exits 7 after 0 ms, example.com fails at DNS resolution, and no approval
    // prompt ever appears. 133 codex children were briefed to curl a progress note; 0 notes
    // arrived. The group above asserts the progress section is *present*; nothing asserted it
    // was *reachable* for the assistant being briefed, which is how the impossible ask stayed
    // green — so every reachability claim here is made against the codex briefing itself.
    func fixture(_ assistant: Assistant) -> Orchestrator.Task {
        Orchestrator.Task(id: taskID, state: .briefed, kind: "custom", title: "a task",
                          assistant: assistant, projectDir: "/Users/me/code/thing",
                          timeoutMinutes: 30, created: Date(), depth: 1,
                          secretHash: String(repeating: "0", count: 64))
    }
    let codex = Orchestrator.childBrief(for: fixture(.codex))
    let claude = Orchestrator.childBrief(for: fixture(.claude))

    check("a codex child is never handed a loopback recipe at all",
          !codex.contains("http://127.0.0.1") && !codex.contains("curl -s"))
    check("its progress channel is the file the broker collects",
          codex.contains("/tmp/.clawdline/\(taskID)/progress.json")
            && codex.contains("\"task_secret\"") && codex.contains("\"note\""))
    check("told as a whole-file replace, the write pattern result.json already proved",
          codex.contains("replacing the whole file"))
    check("the three-minute first note survives the channel change",
          codex.contains("within about three minutes of starting")
            && codex.contains("before you begin the work"))
    check("and it is told the network is off rather than left to discover it by trying",
          codex.contains("exit") && codex.contains("no approval prompt"))
    check("the push-notification recipe is gone with the network that carried it",
          !codex.contains("/v1/orchestrator/tasks/\(taskID)/notify"))
    check("the inflight self-check too, with the plan named as what it has instead",
          !codex.contains("/v1/orchestrator/tasks/\(taskID)/inflight")
            && codex.contains("the plan above"))
    check("and the optional completion announce: the file is the whole signal",
          !codex.contains("/v1/orchestrator/tasks/\(taskID)/complete"))

    check("a claude child keeps the HTTP fast path for all four",
          claude.contains("/v1/orchestrator/tasks/\(taskID)/progress")
            && claude.contains("/v1/orchestrator/tasks/\(taskID)/notify")
            && claude.contains("/v1/orchestrator/tasks/\(taskID)/inflight")
            && claude.contains("/v1/orchestrator/tasks/\(taskID)/complete"))
    check("and is told about the file fallback, not left to meet a sandbox the hard way",
          claude.contains("/tmp/.clawdline/\(taskID)/progress.json"))
}

group("the graph and this Mac's own rules reach every child's briefing") {
    // Two paragraphs a briefing grows, and they are governed differently: the plan comes from
    // whoever dispatched (per task), the policy from whoever owns this Mac (per machine). What
    // they have in common is that a child reading neither writes an essay instead of an answer.
    //
    // **The policy half was once delivered only to a child that could dispatch**, on the reading
    // that house rules are rules about handing work out. That reading is wrong, and the evidence
    // is behavioural: the sentence in this Mac's own file saying a Codex sandbox has no network
    // is what stops a Codex leaf spending a turn on a `curl` that cannot connect. The file
    // carries facts about the machine and not only rules about dispatching, so it goes to every
    // child — which, the tree being one level deep, is now the only kind there is.
    let policyFile = Orchestrator.policyURL
    let localPolicyFile = Orchestrator.localPolicyURL
    let policyBefore = try? Data(contentsOf: policyFile)
    let localPolicyBefore = try? Data(contentsOf: localPolicyFile)
    defer {
        if let policyBefore {
            try? policyBefore.write(to: policyFile, options: .atomic)
        } else {
            try? FileManager.default.removeItem(at: policyFile)
        }
        if let localPolicyBefore {
            try? localPolicyBefore.write(to: localPolicyFile, options: .atomic)
        } else {
            try? FileManager.default.removeItem(at: localPolicyFile)
        }
    }
    func fixture(plan: String?, assistant: Assistant = .claude) -> Orchestrator.Task {
        var task = Orchestrator.Task(id: taskID, state: .briefed, kind: "custom", title: "a task",
                                     assistant: assistant, projectDir: "/Users/me/code/thing",
                                     timeoutMinutes: 30, created: Date(), depth: 1,
                                     secretHash: String(repeating: "0", count: 64))
        task.plan = plan
        return task
    }
    func brief(plan: String?, assistant: Assistant = .claude) -> String {
        Orchestrator.childBrief(for: fixture(plan: plan, assistant: assistant))
    }
    try? FileManager.default.createDirectory(at: policyFile.deletingLastPathComponent(),
                                             withIntermediateDirectories: true)
    try? FileManager.default.removeItem(at: localPolicyFile)
    try? Data("Review runs on opus. Breadth before depth.".utf8).write(to: policyFile, options: .atomic)

    let both = brief(plan: "root → 3 searchers → this one joins them up")
    check("the plan is in the briefing, in the dispatcher's own words",
          both.contains("root → 3 searchers → this one joins them up"))
    check("above the rules, because it is what the rules are read in the light of",
          both.range(of: "## The plan this is part of")!.lowerBound
              < both.range(of: "## Rules")!.lowerBound)
    // The house rules used to travel in `DISPATCHING.md`, which only a dispatching child was
    // given. There is no dispatching child now, and deleting the section with the recipe would
    // have left this Mac unable to tell any child anything about itself.
    check("and this Mac's house rules are in the briefing of a child that cannot dispatch",
          both.contains("Review runs on opus."))
    check("named by path, both files, so a child can say where a rule it followed came from",
          both.contains(Orchestrator.policyURL.path)
            && both.contains(Orchestrator.localPolicyURL.path))
    check("a codex leaf is told them too, and it is the one the machine facts are written for",
          brief(plan: nil, assistant: .codex).contains("Review runs on opus."))

    // The nail this group carries. The machine-local file exists so facts about *this* Mac
    // survive edits and syncs of the shipped rules; composing it is worth nothing if the
    // composed text has no consumer, and the briefing is the consumer.
    try? Data("Codex children here have no network.".utf8)
        .write(to: localPolicyFile, options: .atomic)
    let composedBrief = brief(plan: nil)
    check("the machine-local file reaches the child too, last and under its own heading",
          composedBrief.contains(Orchestrator.localPolicyHeading)
            && composedBrief.contains("Codex children here have no network.")
            && composedBrief.range(of: "Review runs on opus.")!.lowerBound
                < composedBrief.range(of: Orchestrator.localPolicyHeading)!.lowerBound)
    try? FileManager.default.removeItem(at: localPolicyFile)

    let leaf = brief(plan: "root → 3 searchers → this one")
    check("a leaf is told the plan too — it is what makes its answer joinable",
          leaf.contains("root → 3 searchers → this one"))

    try? FileManager.default.removeItem(at: policyFile)
    check("no file at all is no paragraph, rather than an empty heading",
          !brief(plan: nil).contains("## What this Mac says"))
    check("and no plan is no paragraph either",
          !brief(plan: nil).contains("## The plan this is part of"))
    try? Data("   \n\n  ".utf8).write(to: policyFile, options: .atomic)
    check("a file of nothing but whitespace counts as nobody having said anything",
          Orchestrator.policy() == nil)

    // The starting rules used to be a Swift string literal holding an old draft of the file this
    // repository actually edits — and that literal is what a fresh install received, because
    // `ensurePolicyFile` writes it. Now it is the shipped resource, read at the point of use.
    let bundledOverride = Orchestrator.bundledPolicyURLOverrideForTesting
    defer { Orchestrator.bundledPolicyURLOverrideForTesting = bundledOverride }
    Orchestrator.bundledPolicyURLOverrideForTesting =
        URL(fileURLWithPath: "Resources/dispatch-policy.md")
    let shipped = (try? String(contentsOfFile: "Resources/dispatch-policy.md", encoding: .utf8))?
        .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    check("the starting rules are the file this repository ships, character for character",
          !shipped.isEmpty && Orchestrator.defaultPolicy == shipped)
    // Nothing below quotes the rules themselves. This file is the house's opinion and is meant to
    // be rewritten; a suite that pins its sentences goes red for a reason that has nothing to do
    // with the code, and the person who reworded a paragraph is left reading a compiler.
    // What is the code's business is that the document arrives whole and usable.
    let policyLines = Orchestrator.defaultPolicy.split(separator: "\n", omittingEmptySubsequences: false)
    check("it arrives as the whole document rather than a first paragraph of it",
          policyLines.count > 1 && policyLines.first?.hasPrefix("# ") == true
              && policyLines.filter { $0.hasPrefix("## ") }.count >= 2)
    // Not merely under the limit: what a briefing would actually carry is the same document,
    // unshortened. Shipping rules that trip the app's own truncation notice would be the house
    // arguing with itself, and asked this way the assertion quotes neither the rules nor the notice.
    check("and what a briefing carries is the whole of it, uncut",
          Orchestrator.defaultPolicy.count <= Orchestrator.policyLimit
              && Orchestrator.policy(reading: Orchestrator.defaultPolicy) == Orchestrator.defaultPolicy)

    // A missing resource means no house rules, which is exactly what an empty policy file has
    // always meant. Nothing is invented to fill the gap.
    Orchestrator.bundledPolicyURLOverrideForTesting =
        URL(fileURLWithPath: "/nowhere/dispatch-policy.md")
    check("a bundle with no policy resource in it means no starting rules at all",
          Orchestrator.defaultPolicy.isEmpty)
    // …and it must not leave an empty file behind, because `ensurePolicyFile` never overwrites:
    // a machine that once could not read the resource would keep the empty rules for good.
    try? FileManager.default.removeItem(at: policyFile)
    let answered = Orchestrator.ensurePolicyFile()
    check("nothing to write is not the same as writing nothing",
          answered == policyFile
            && !FileManager.default.fileExists(atPath: policyFile.path))
    Orchestrator.bundledPolicyURLOverrideForTesting =
        URL(fileURLWithPath: "Resources/dispatch-policy.md")
    _ = Orchestrator.ensurePolicyFile()
    check("with the resource there, a machine with no rules of its own starts with the shipped ones",
          (try? String(contentsOf: policyFile, encoding: .utf8)) == shipped)
    try? Data("mine, and nobody rewrites it".utf8).write(to: policyFile, options: .atomic)
    _ = Orchestrator.ensurePolicyFile()
    check("and a file that is already there is never overwritten",
          (try? String(contentsOf: policyFile, encoding: .utf8)) == "mine, and nobody rewrites it")

    // Cutting used to be silent and mid-word: the first policy long enough to hit the limit lost
    // its last rule, and the briefing read as though the file simply ended there.
    let long = (0..<400).map { "Paragraph \($0) of a policy somebody kept adding to." }
        .joined(separator: "\n\n")
    let cut = Orchestrator.policy(reading: long)
    check("an over-long policy is cut", (cut?.count ?? 0) < long.count)
    check("and says so, rather than looking like the file ended there",
          cut?.contains("This policy was cut here") == true)
    check("and the cut lands on a paragraph, not inside a word",
          cut?.components(separatedBy: "\n\n").dropLast().last?.hasSuffix("kept adding to.") == true)
    check("a policy inside the limit is handed over untouched",
          Orchestrator.policy(reading: "  short rules  ") == "short rules")
    check("and an empty one is still nobody having said anything",
          Orchestrator.policy(reading: "   \n  ") == nil)

    // The optional sibling is machine-local: it is read beside the base at dispatch, composed
    // last so it wins, and never allowed to be the casualty when the pair crosses the limit.
    let baseOnly = "Base paragraph one.\n\nBase paragraph two."
    check("no local policy is byte-identical to the old single-file answer",
          Orchestrator.policy(reading: baseOnly, local: nil)
            == Orchestrator.policy(reading: baseOnly))
    check("a whitespace-only local policy behaves as absent too",
          Orchestrator.policy(reading: baseOnly, local: "  \n\n ")
            == Orchestrator.policy(reading: baseOnly))

    let local = "This machine permits loopback progress notes."
    let composed = Orchestrator.policy(reading: baseOnly, local: local)
    let localHeading = Orchestrator.localPolicyHeading
    check("local rules follow the base under a visible precedence heading",
          composed?.range(of: baseOnly) != nil
            && composed?.range(of: localHeading) != nil
            && composed?.range(of: local) != nil
            && composed!.range(of: baseOnly)!.lowerBound
                < composed!.range(of: localHeading)!.lowerBound
            && composed!.range(of: localHeading)!.lowerBound
                < composed!.range(of: local)!.lowerBound)

    let longBase = (0..<400).map { "Base paragraph \($0) must yield before local facts." }
        .joined(separator: "\n\n")
    let protectedLocal = (0..<40).map { "Machine fact \($0) survives." }
        .joined(separator: "\n\n")
    let protected = Orchestrator.policy(reading: longBase, local: protectedLocal)
    check("when the pair is too long, the local policy survives whole and last",
          protected?.hasSuffix(protectedLocal) == true
            && protected?.contains("Base paragraph 0 must yield before local facts.") == true
            && protected?.contains("Base paragraph 399 must yield before local facts.") == false)
    check("cutting the base is announced rather than made to look complete",
          protected?.contains("This policy was cut here") == true)

    let oversizedLocal = (0..<500).map { "Oversized machine paragraph \($0)." }
        .joined(separator: "\n\n")
    let localCut = Orchestrator.policy(reading: nil, local: oversizedLocal)
    check("a local policy that alone exceeds the limit is cut without crashing",
          localCut?.contains(localHeading) == true
            && localCut?.contains("This policy was cut here") == true
            && localCut?.contains("Oversized machine paragraph 499.") == false)
    check("a local policy still reaches dispatch when there is no base",
          Orchestrator.policy(reading: nil, local: local)?.hasSuffix(local) == true)

    // Everything above this line asks *what survives*, and a reader that never spent the local
    // section's budget satisfies all of it: the local rules are there, the base is shorter, the
    // cut is announced — and the briefing is a thousand characters over the ceiling the limit
    // exists to hold. So one check asks *how long*. The notice is deliberately outside the
    // budget, here as in the single-file cutter, so the allowance is the limit plus one of them.
    let noticeRoom = 500
    check("the pair spends the budget rather than overrunning it",
          protected!.count <= Orchestrator.policyLimit + noticeRoom)

    // A base with no blank line inside the allowance has no paragraph boundary to cut at: a
    // policy written as one bulleted list, or one saved with CRLF, where Swift reads `\r\n` as a
    // single Character so a search for `\n\n` can never match. Rules broken mid-word are the
    // answer there; no rules at all is not, and that is the whole difference between `?? head`
    // and `?? ""` — measured on a 16,389-character bulleted base, 12,193 characters of rules
    // against 807.
    let unbrokenBase = (0..<600).map { "- rule \($0) has no blank line after it." }
        .joined(separator: "\n")
    let unbroken = Orchestrator.policy(reading: unbrokenBase, local: local)
    check("a base with no paragraph break is cut, not deleted",
          unbroken?.contains("- rule 0 has no blank line after it.") == true
            && unbroken?.hasSuffix(local) == true
            && unbroken!.count > Orchestrator.policyLimit / 2)

    try? Data(baseOnly.utf8).write(to: policyFile, options: .atomic)
    try? Data(local.utf8).write(to: localPolicyFile, options: .atomic)
    check("the file-reading half reads the optional local sibling fresh",
          Orchestrator.policy() == composed)
}
}
