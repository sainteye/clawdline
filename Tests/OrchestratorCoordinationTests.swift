import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3
// Words sent into Claude Code's permission picker are discarded, and the Return that follows
// answers whichever row is highlighted. Every other typing path in this app already refuses that;
// these two were the ones still sending into it — and worse, recording the loss as a delivery.

// A3: the owner's half of a file wait. Two groups, both named for the side of the relationship
// they are about, so this block can be told apart from the delivery-and-guard work landing on
// the same file from another branch.
func runOrchestratorCoordinationTests() {
runRootAssignmentCoordinationTests()
group("a handoff envelope is validated without reading its letter") {
    let id = "7c1e9b02-4d55-4a80-9c3e-1f6b2a09d431"
    let base: [String: Any] = ["handoff_id": id, "project_dir": "/tmp"]
    func draft(_ changes: [String: Any] = [:], packageReady: Bool = true)
        -> Orchestrator.HandoffDraftOutcome {
        var request = base
        for (key, value) in changes { request[key] = value }
        return Orchestrator.handoffDraft(from: request,
            isDirectory: { $0 == "/tmp" }, packageIsReady: { _ in packageReady })
    }

    guard case .ok(let defaults) = draft() else {
        check("the smallest valid envelope is accepted", false); return
    }
    expect("the default receiver is Claude", defaults.assistant, .claude)
    expect("and no absent field is invented", defaults.title, nil)
    expect("nor is a sender invented", defaults.fromSession, nil)
    check("a lowercase UUID is required",
          draft(["handoff_id": id.uppercased()]).isBad)
    check("and the task UUID rule does not admit a path",
          draft(["handoff_id": "../../../../tmp/handoff-letter-123456"]).isBad)
    check("a project must be absolute", draft(["project_dir": "tmp"]).isBad)
    check("and must exist as a directory",
          Orchestrator.handoffDraft(from: base, isDirectory: { _ in false },
                                    packageIsReady: { _ in true }).isBad)
    check("the receiver is a closed choice", draft(["assistant": "other"]).isBad)
    check("a shell-shaped model is refused", draft(["model": "opus; touch /tmp/x"]).isBad)
    check("an empty model is refused too", draft(["model": ""]).isBad)
    check("a title may be exactly 200 characters",
          !draft(["title": String(repeating: "t", count: 200)]).isBad)
    check("but not 201", draft(["title": String(repeating: "t", count: 201)]).isBad)
    check("a sender name is free-form at its boundary",
          !draft(["from_session": String(repeating: "會", count: 200)]).isBad)
    check("but has the same boundary",
          draft(["from_session": String(repeating: "會", count: 201)]).isBad)
    check("the package directory and non-empty regular handoff.md are mandatory",
          draft(packageReady: false).isBad)

    expect("the injected line is the canonical protocol sentence, byte for byte",
           Orchestrator.handoffLine(id: id),
           "You are picking up a Clawdline handoff. Read /tmp/.clawdline/handoffs/"
             + id
             + "/handoff.md before anything else and follow it: walk its REFERENCES, answer its "
             + "VERIFICATION questions from those sources, say plainly what you could not reach, "
             + "then continue from OPEN THREADS.")
    let line = Orchestrator.handoffLine(id: id)
    let claudeReceipt = """
    {"type":"user","message":{"role":"user","content":"\(line)"}}
    """
    check("Claude's exact first user turn confirms delivery",
          Orchestrator.transcriptContainsHandoff(claudeReceipt, assistant: .claude,
                                                 handoffID: id))
    let wrappedClaudeReceipt = """
    {"type":"user","message":{"role":"user","content":"already queued\\n\(line)"}}
    """
    check("a canonical line inside a longer first user turn confirms delivery",
          Orchestrator.transcriptContainsHandoff(wrappedClaudeReceipt, assistant: .claude,
                                                 handoffID: id))
    let codexReceipt = """
    {"type":"event_msg","payload":{"type":"item_completed","item":\
    {"type":"UserMessage","content":[{"type":"text","text":"\(line)"}]}}}
    """
    check("Codex's exact first user turn confirms delivery too",
          Orchestrator.transcriptContainsHandoff(codexReceipt, assistant: .codex,
                                                 handoffID: id))
    check("a merely similar turn is not a receipt",
          !Orchestrator.transcriptContainsHandoff(
              claudeReceipt.replacingOccurrences(of: "OPEN THREADS.", with: "OPEN THREADS"),
              assistant: .claude, handoffID: id))
    check("a missing handoff transcript never authorises a retry",
          !Orchestrator.handoffRetryAllowed(attempts: 1, transcriptKnown: false))
    check("the first handoff send does not require a transcript that cannot exist yet",
          Orchestrator.handoffRetryAllowed(attempts: 0, transcriptKnown: false))
    expect("a titled receipt names the line and receiver",
           Orchestrator.handoffReceipt(id: id, title: "Cloud planning line",
                                       assistant: .codex, projectDir: "/tmp", delivered: true).body,
           "[clawdline] handoff 7c1e9b02 (Cloud planning line) picked up by codex in /tmp")
    expect("an untitled failure receipt does not grow empty brackets",
           Orchestrator.handoffReceipt(id: id, title: nil, assistant: .claude,
                                       projectDir: "/tmp", delivered: false).body,
           "[clawdline] handoff 7c1e9b02 opened a tab but the first line never landed — type it in by hand")
    let semanticReceipt = Orchestrator.handoffReceipt(
        id: id, title: "Cloud planning line", assistant: .codex,
        projectDir: #"/tmp/<repo>&\"quoted\""#, delivered: true)
    let semanticReceiptIsTyped: Bool
    if case .handoffReceipt(_, _, .codex, _, .pickedUp) = semanticReceipt.event {
        semanticReceiptIsTyped = true
    } else {
        semanticReceiptIsTyped = false
    }
    check("a handoff receipt carries one typed kind with an outcome state",
          ClawdlineMessage.decode(ClawdlineMessage.encode(semanticReceipt)) == semanticReceipt
            && semanticReceiptIsTyped)
}

group("handoff envelopes survive restart and terminal ones are swept as one unit") {
    Orchestrator.forget()
    let handoffBase = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-handoffs-\(UUID().uuidString)", isDirectory: true)
    Orchestrator.handoffRootOverrideForTesting = handoffBase.appendingPathComponent(
        "handoffs", isDirectory: true)
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    let id = UUID().uuidString.lowercased()
    let opening = UUID().uuidString.lowercased()
    let directory = Orchestrator.handoffRoot.appendingPathComponent(id, isDirectory: true)
    defer {
        try? FileManager.default.removeItem(at: directory)
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.handoffRootOverrideForTesting = nil
        try? FileManager.default.removeItem(at: handoffBase)
        Orchestrator.forget()
    }
    try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    try? Data("the app must never persist these words".utf8)
        .write(to: directory.appendingPathComponent("handoff.md"), options: .atomic)
    let old = Date().addingTimeInterval(-25 * 3600).timeIntervalSince1970
    let rows: [[String: Any]] = [
        ["handoff_id": id, "project_dir": "/tmp", "title": "Old line",
         "from_session": "sender", "created": old, "state": "delivered"],
        ["handoff_id": opening, "project_dir": "/tmp", "created": old, "state": "opening"],
    ]
    // The label rows are a separate durable record with their own key, and the second one has no
    // envelope at all: what a label is bound to is a tab, not a letter.
    let orphanLabel = UUID().uuidString.lowercased()
    let liveStart = 1_788_397_479.0
    let labelRows: [[String: Any]] = [
        ["handoff_id": id, "label": "Old line",
         "identity": ["terminal_id": "%live", "assistant": "claude", "tty": "/dev/ttys021",
                      "pid": 4242, "process_start": liveStart,
                      "conversation_id": "a9618310-4114-4906-aeea-6a7f226db119"]],
        ["handoff_id": orphanLabel, "label": "A tab that closed",
         "identity": ["terminal_id": "%gone", "assistant": "claude", "pid": 4243,
                      "process_start": liveStart]],
    ]
    let data = try! JSONSerialization.data(withJSONObject: ["version": 1, "tasks": [],
                                                              "handoffs": rows,
                                                              "handoff_labels": labelRows])
    try? FileManager.default.createDirectory(at: store.deletingLastPathComponent(),
                                             withIntermediateDirectories: true)
    try! data.write(to: store, options: .atomic)
    Orchestrator.load(force: true)
    let loaded = Orchestrator.handoffRecord(id: id)
    expect("the envelope comes back across a reload", loaded?["state"] as? String, "delivered")
    check("only envelope fields came back",
          Set(loaded?.keys.map { $0 } ?? []) == Set(["handoff_id", "project_dir", "title",
                                                    "from_session", "created", "state"]))
    Orchestrator.saveForTesting()
    let saved = (try? String(contentsOf: store, encoding: .utf8)) ?? ""
    check("the registry did not read and remember the letter",
          !saved.contains("the app must never persist these words"))

    let restoredLabel = Orchestrator.handoffLabelForTesting(id)
    expect("a durable handoff label comes back off disk with its job title",
           restoredLabel?.label, "Old line")
    check("and with the whole process tuple it was bound to",
          restoredLabel?.identity == Orchestrator.RootAssignmentIdentity(
            terminalID: "%live", assistant: .claude, tty: "/dev/ttys021", pid: 4242,
            processStart: liveStart,
            conversationID: "a9618310-4114-4906-aeea-6a7f226db119"))
    expect("so the tab wears the job title again in a process that never opened it",
           Orchestrator.title(forTerminal: "%live"), "Old line")
    let liveIdentity = Orchestrator.SessionWorkIdentity(
        terminalID: "%live", assistant: .claude, tty: "/dev/ttys021", pid: 4242,
        processStart: Date(timeIntervalSince1970: liveStart),
        conversationID: "a9618310-4114-4906-aeea-6a7f226db119")
    Orchestrator.pruneClosedHandoffTitles(visible: ["%live"], identities: [liveIdentity])

    Orchestrator.cleanup()
    check("a day-old terminal envelope is removed", Orchestrator.handoffRecord(id: id) == nil)
    check("its package goes with it", !FileManager.default.fileExists(atPath: directory.path))
    check("an old opening envelope is retained", Orchestrator.handoffRecord(id: opening) != nil)
    // The two halves of the lifetime decision, taken together so neither can pass alone: the
    // envelope's 24-hour clock does not reach the label, and the reading of this machine does.
    check("the label of a tab that is still open outlives the envelope it came from",
          Orchestrator.handoffLabelForTesting(id)?.label == "Old line")
    check("while the label of a tab no reading can find is reclaimed",
          Orchestrator.handoffLabelForTesting(orphanLabel) == nil)
    Orchestrator.forget()
    Orchestrator.load(force: true)
    check("the removal itself survives restart", Orchestrator.handoffRecord(id: id) == nil)
    check("and so does the label that outlived it",
          Orchestrator.handoffLabelForTesting(id)?.label == "Old line")

    func reload(labels: [[String: Any]]) {
        let rewritten = try! JSONSerialization.data(
            withJSONObject: ["version": 1, "tasks": [], "handoffs": [],
                             "handoff_labels": labels])
        try! rewritten.write(to: store, options: .atomic)
        Orchestrator.forget()
        Orchestrator.load(force: true)
    }

    // Two unsuppressed labels reaching one terminal id are two answers to a question that has
    // one, and dictionary iteration order is not a tie-break. The projection refuses instead,
    // the way `rootAssignmentSessionProjection` refuses on `matches.count == 1`.
    let twin = UUID().uuidString.lowercased()
    reload(labels: [
        ["handoff_id": id, "label": "Old line",
         "identity": ["terminal_id": "%shared", "assistant": "claude"]],
        ["handoff_id": twin, "label": "A second job on the same tab",
         "identity": ["terminal_id": "%shared", "assistant": "claude"]],
        ["handoff_id": orphanLabel, "label": "The only label on its own tab",
         "identity": ["terminal_id": "%alone", "assistant": "claude"]],
    ])
    check("two unsuppressed labels on one terminal id name that tab nothing at all",
          Orchestrator.title(forTerminal: "%shared") == nil)
    expect("while a tab exactly one label names still wears it",
           Orchestrator.title(forTerminal: "%alone"), "The only label on its own tab")

    // `load()` is the boot path, so a stored value too large to be a process id costs this label
    // its process rather than costing the app its start.
    reload(labels: [
        ["handoff_id": id, "label": "Old line",
         "identity": ["terminal_id": "%live", "assistant": "claude", "tty": "/dev/ttys021",
                      "pid": 4_294_967_296, "process_start": liveStart]],
    ])
    expect("a pid too large to be one does not stop the registry loading",
           Orchestrator.handoffLabelForTesting(id)?.label, "Old line")
    check("and that label simply comes back with no process bound to it",
          Orchestrator.handoffLabelForTesting(id)?.identity.pid == nil)
    check("the fields either side of it are untouched",
          Orchestrator.handoffLabelForTesting(id)?.identity.processStart == liveStart
            && Orchestrator.handoffLabelForTesting(id)?.identity.tty == "/dev/ttys021")
}

group("handoff registration opens once and shares the dispatch brake") {
    Orchestrator.forget()
    let handoffBase = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-handoffs-\(UUID().uuidString)", isDirectory: true)
    Orchestrator.handoffRootOverrideForTesting = handoffBase.appendingPathComponent(
        "handoffs", isDirectory: true)
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    var made: [URL] = []
    defer {
        for directory in made { try? FileManager.default.removeItem(at: directory) }
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Orchestrator.handoffRootOverrideForTesting = nil
        try? FileManager.default.removeItem(at: handoffBase)
        Orchestrator.forget()
    }
    func envelope(_ id: String) -> [String: Any] {
        let directory = Orchestrator.handoffRoot.appendingPathComponent(id, isDirectory: true)
        made.append(directory)
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try? Data("# handoff\n".utf8)
            .write(to: directory.appendingPathComponent("handoff.md"), options: .atomic)
        return ["handoff_id": id, "project_dir": "/tmp", "assistant": "codex"]
    }
    let id = UUID().uuidString.lowercased()
    var opens = 0
    var grantedDirectory: String?
    let first = Orchestrator.handoff(envelope(id)) { _, _, _, addDir in
        opens += 1
        grantedDirectory = addDir
        return .started(id: "%handoff", backend: .tmux)
    }
    guard case .ok(let payload) = first,
          let handoff = payload["handoff"] as? [String: Any] else {
        check("a valid handoff opens", false); return
    }
    expect("the synchronous answer is opening", handoff["state"] as? String, "opening")
    expect("it names the selected assistant", handoff["assistant"] as? String, "codex")
    expect("and the terminal that was opened",
           (handoff["opened"] as? [String: Any])?["terminalId"] as? String, "%handoff")
    expect("the receiver can read precisely its package directory", grantedDirectory,
           Orchestrator.handoffRoot.appendingPathComponent(id).path)
    expect("an unnamed tab gets the documented fallback label",
           Orchestrator.title(forTerminal: "%handoff"), "handoff \(id.prefix(8))")
    let parentMode = (try? FileManager.default.attributesOfItem(
        atPath: Orchestrator.handoffRoot.deletingLastPathComponent().path)[.posixPermissions])
        as? NSNumber
    let rootMode = (try? FileManager.default.attributesOfItem(
        atPath: Orchestrator.handoffRoot.path)[.posixPermissions]) as? NSNumber
    expect("the app secures the handoff parent directory", parentMode?.intValue, 0o700)
    expect("and secures the handoff root directory", rootMode?.intValue, 0o700)
    Orchestrator.settleHandoff(id, delivered: true, assistant: .codex, why: nil)
    Orchestrator.pruneClosedHandoffTitles(visible: ["%handoff"])
    expect("a visible handed-off root keeps its title",
           Orchestrator.title(forTerminal: "%handoff"), "handoff \(id.prefix(8))")
    Orchestrator.pruneClosedHandoffTitles(visible: [])
    check("a closed handed-off root forgets its title",
          Orchestrator.title(forTerminal: "%handoff") == nil)

    _ = Orchestrator.handoff(envelope(id)) { _, _, _, _ in
        opens += 1
        return .refused(status: 500, code: "internal", message: "should not run", app: nil)
    }
    expect("an in-process retry does not open another tab", opens, 1)
    Orchestrator.forget()
    Orchestrator.load(force: true)
    _ = Orchestrator.handoff(envelope(id)) { _, _, _, _ in
        opens += 1
        return .refused(status: 500, code: "internal", message: "should not run", app: nil)
    }
    expect("nor does the same id after a registry reload", opens, 1)

    // The durable label, from the tab opening to the restart that used to lose it. The fallback
    // above — `handoff <first eight>` — is deliberately not part of this: it says less than the
    // name the conversation generates for itself, so it stays a fact about this process only.
    Orchestrator.forget()
    let titledID = UUID().uuidString.lowercased()
    var titledRequest = envelope(titledID)
    titledRequest["title"] = "接手成為 Clawdfather"
    titledRequest["assistant"] = "claude"
    _ = Orchestrator.handoff(titledRequest) { _, _, _, _ in .started(id: "%titled", backend: .tmux) }
    let untitledID = UUID().uuidString.lowercased()
    _ = Orchestrator.handoff(envelope(untitledID)) { _, _, _, _ in
        .started(id: "%untitled", backend: .tmux)
    }
    check("an untitled handoff stores no durable placeholder",
          Orchestrator.handoffLabelForTesting(untitledID) == nil)
    expect("a named one is bound to the exact tab it was delivered into",
           Orchestrator.handoffLabelForTesting(titledID)?.identity.terminalID, "%titled")
    check("and it holds no process tuple yet, because nothing has looked at that tab",
          Orchestrator.handoffLabelForTesting(titledID)?.identity.pid == nil)
    let receivingSnapshot = SessionWatch.IdentitySnapshot(
        targets: [], generation: 7, complete: true, observedAt: Date(), epoch: "handoff-label")
    let receiver = Orchestrator.SessionWorkIdentity(
        terminalID: "%titled", assistant: .claude, tty: "/dev/ttys031", pid: 4711,
        processStart: Date(timeIntervalSince1970: 1_788_397_479),
        conversationID: "f7e2716a-0000-4000-8000-000000000001")
    check("an incomplete reading of the machine is never allowed to seed one",
          !Orchestrator.adoptHandoffLabelIdentitiesForTesting(
            snapshot: SessionWatch.IdentitySnapshot(
                targets: [], generation: 6, complete: false, observedAt: Date(),
                epoch: "handoff-label"),
            identities: [receiver]))
    check("so the record is still the terminal id it was opened with",
          Orchestrator.handoffLabelForTesting(titledID)?.identity.pid == nil)
    // A Claude tab has a pid before its transcript has a name, so the first reading binds a record
    // that is bound and still incomplete — which is the only state in which the equality guard,
    // rather than the completeness one, is what stops the next beat writing the store again.
    let halfNamed = Orchestrator.SessionWorkIdentity(
        terminalID: "%titled", assistant: .claude, tty: "/dev/ttys031", pid: 4711,
        processStart: Date(timeIntervalSince1970: 1_788_397_479), conversationID: nil)
    check("the first complete inventory binds the label to the process now in that tab",
          Orchestrator.adoptHandoffLabelIdentitiesForTesting(snapshot: receivingSnapshot,
                                                             identities: [halfNamed]))
    check("reading that same half-named tab again writes nothing",
          !Orchestrator.adoptHandoffLabelIdentitiesForTesting(snapshot: receivingSnapshot,
                                                              identities: [halfNamed]))
    check("the conversation arriving later completes the record",
          Orchestrator.adoptHandoffLabelIdentitiesForTesting(snapshot: receivingSnapshot,
                                                             identities: [receiver]))
    check("after which a further reading of the same process changes nothing",
          !Orchestrator.adoptHandoffLabelIdentitiesForTesting(snapshot: receivingSnapshot,
                                                              identities: [receiver]))
    Orchestrator.saveForTesting()
    Orchestrator.forget()
    Orchestrator.load(force: true)
    expect("a restart gives the handed-off tab its job title back",
           Orchestrator.title(forTerminal: "%titled"), "接手成為 Clawdfather")
    check("while an untitled one is left to whatever the conversation calls itself",
          Orchestrator.title(forTerminal: "%untitled") == nil)
    Orchestrator.pruneClosedHandoffTitles(visible: ["%titled"], identities: [receiver])
    let sameProcessKept = Orchestrator.title(forTerminal: "%titled")
    var reusedTab = receiver
    reusedTab.pid = 909
    Orchestrator.pruneClosedHandoffTitles(visible: ["%titled"], identities: [reusedTab])
    let reusedForgot = Orchestrator.title(forTerminal: "%titled") == nil
    Orchestrator.pruneClosedHandoffTitles(visible: ["%titled"], identities: [receiver])
    var otherConversation = receiver
    otherConversation.conversationID = "11111111-2222-3333-4444-555555555555"
    Orchestrator.pruneClosedHandoffTitles(visible: ["%titled"], identities: [otherConversation])
    let otherConversationForgot = Orchestrator.title(forTerminal: "%titled") == nil
    expect("the same terminal running the same process keeps the restored title",
           sameProcessKept, "接手成為 Clawdfather")
    check("a reused terminal id does not inherit a stranger's job name", reusedForgot)
    check("nor does a different conversation in the same tab", otherConversationForgot)
    check("and the durable record is still there to give it back when the process returns",
          Orchestrator.handoffLabelForTesting(titledID)?.label == "接手成為 Clawdfather")

    // An absence only means something once the reading is finished. Production's only caller
    // passes an array that is empty rather than nil until the first scan publishes, and what is
    // suppressed here is exactly what `cleanup()` deletes.
    Orchestrator.pruneClosedHandoffTitles(visible: ["%titled"], identities: [receiver])
    Orchestrator.pruneClosedHandoffTitles(visible: ["%titled"], identities: [],
                                          inventoryComplete: false)
    expect("an unfinished reading of this Mac leaves a durable label alone",
           Orchestrator.title(forTerminal: "%titled"), "接手成為 Clawdfather")
    Orchestrator.pruneClosedHandoffTitles(visible: ["%titled"], identities: [],
                                          inventoryComplete: true)
    check("while a finished reading that finds no assistant anywhere does suppress it",
          Orchestrator.title(forTerminal: "%titled") == nil)

    // The five steps of the rebinding this feature exists to refuse, in the order one beat walks
    // them. A Claude tab has a pid before its transcript has a name, so a record that is bound to
    // a process and still missing its conversation id is an ordinary state — and "is a field
    // missing?" is not the same question as "is this bound to anything yet?".
    let rebindID = UUID().uuidString.lowercased()
    var rebindRequest = envelope(rebindID)
    rebindRequest["title"] = "not this stranger's job"
    rebindRequest["assistant"] = "claude"
    _ = Orchestrator.handoff(rebindRequest) { _, _, _, _ in
        .started(id: "%rebind", backend: .tmux)
    }
    let firstProcess = Orchestrator.SessionWorkIdentity(
        terminalID: "%rebind", assistant: .claude, tty: "/dev/ttys041", pid: 100,
        processStart: Date(timeIntervalSince1970: 1_788_398_100), conversationID: nil)
    check("a first complete reading binds the label to the process in that tab",
          Orchestrator.adoptHandoffLabelIdentitiesForTesting(snapshot: receivingSnapshot,
                                                             identities: [firstProcess]))
    // That process ends and another opens in the same reusable tab. One beat sees both halves:
    // the prune stops showing the label, which is right, and the adoption directly after it must
    // not hand the record to whatever is in the tab now.
    let strangerInThatTab = Orchestrator.SessionWorkIdentity(
        terminalID: "%rebind", assistant: .claude, tty: "/dev/ttys041", pid: 200,
        processStart: Date(timeIntervalSince1970: 1_788_398_200),
        conversationID: "cccccccc-0000-4000-8000-000000000002")
    Orchestrator.pruneClosedHandoffTitles(visible: ["%rebind"], identities: [strangerInThatTab])
    check("a bound record does not follow a new process into the tab it was opened in",
          !Orchestrator.adoptHandoffLabelIdentitiesForTesting(snapshot: receivingSnapshot,
                                                              identities: [strangerInThatTab]))
    expect("so it still names the process this handoff was delivered to",
           Orchestrator.handoffLabelForTesting(rebindID)?.identity.pid, 100)
    Orchestrator.saveForTesting()
    Orchestrator.forget()
    Orchestrator.load(force: true)
    Orchestrator.pruneClosedHandoffTitles(visible: ["%rebind"], identities: [strangerInThatTab])
    check("so the next beat leaves the stranger's tab unnamed rather than giving it this job",
          Orchestrator.title(forTerminal: "%rebind") == nil)
    // What a bound record may still accept from the process it is bound to is the one field it
    // is missing.
    let firstProcessNamed = Orchestrator.SessionWorkIdentity(
        terminalID: "%rebind", assistant: .claude, tty: "/dev/ttys041", pid: 100,
        processStart: Date(timeIntervalSince1970: 1_788_398_100),
        conversationID: "cccccccc-0000-4000-8000-000000000001")
    check("while the process it is bound to may still fill in its conversation id",
          Orchestrator.adoptHandoffLabelIdentitiesForTesting(snapshot: receivingSnapshot,
                                                             identities: [firstProcessNamed]))
    expect("completing the record it already had rather than replacing it",
           Orchestrator.handoffLabelForTesting(rebindID)?.identity.conversationID,
           "cccccccc-0000-4000-8000-000000000001")

    Orchestrator.forget()
    for _ in 0..<max(10, Config.shared.orchestratorMaxDescendants) {
        let next = UUID().uuidString.lowercased()
        _ = Orchestrator.handoff(envelope(next)) { _, _, _, _ in
            .refused(status: 409, code: "terminal_closed", message: "closed", app: "iTerm2")
        }
    }
    check("terminal refusals still spend the shared ticket", Orchestrator.takeDispatchRate() == nil)
}

group("coordination waits are durable broker relationships") {
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

    let firstWaiter = "WAIT-A"
    let secondWaiter = "WAIT-B"
    let owner = "OWNER"
    let created = Date(timeIntervalSince1970: 1_800_000_000)
    func request(waiter: String) -> [String: Any] {
        [
            "repository": "/Users/me/code/clawdline/",
            "paths": ["Sources/Foo.swift", "./Sources/Foo.swift"],
            "owner_session_id": owner,
            "waiter_session_id": waiter,
            "reason": "the owner is editing the same file",
            "release_condition": "the path is committed or explicitly released",
        ]
    }

    var requests: [(String, String)] = []
    func deliverRequest(_ target: String, _ text: String) -> String? {
        requests.append((target, text))
        return nil
    }
    let first = Orchestrator.registerCoordinationWait(
        request(waiter: firstWaiter), now: created, deliver: deliverRequest)
    guard case .ok(let firstPayload) = first,
          let wait = firstPayload["wait"] as? [String: Any],
          let waitID = wait["id"] as? String else {
        check("a valid coordination wait registers", false); return
    }
    expect("the broker delivers the request to the owner", requests.first?.0, owner)
    let deliveredRequestBody = requests.first.flatMap { ClawdlineMessage.decode($0.1)?.body }
    check("the delivered request body identifies the waiter and release condition",
          deliveredRequestBody?.contains(firstWaiter) == true
              && deliveredRequestBody?.contains("committed or explicitly released") == true)
    check("the owner receives a strict semantic file-wait request envelope",
          requests.first.flatMap { ClawdlineMessage.decode($0.1) }.map {
              if case let .fileWaitRequest(id, repository, paths, waiter, reason, condition)
                    = $0.event {
                  return id == waitID && repository == "/Users/me/code/clawdline"
                    && paths == ["Sources/Foo.swift"] && waiter == firstWaiter
                    && reason == "the owner is editing the same file"
                    && condition == "the path is committed or explicitly released"
              }
              return false
          } == true)
    expect("canonical duplicate paths are stored once",
           wait["paths"] as? [String], ["Sources/Foo.swift"])

    let duplicate = Orchestrator.registerCoordinationWait(
        request(waiter: firstWaiter), now: created.addingTimeInterval(5),
        deliver: deliverRequest)
    guard case .ok(let duplicatePayload) = duplicate else {
        check("a duplicate wait is accepted idempotently", false); return
    }
    expect("a duplicate reports that it reused the relationship",
           duplicatePayload["deduplicated"] as? Bool, true)
    expect("and does not type the same request twice", requests.count, 1)

    _ = Orchestrator.registerCoordinationWait(
        request(waiter: secondWaiter), now: created.addingTimeInterval(10),
        deliver: deliverRequest)
    expect("a second waiter is delivered independently", requests.count, 2)
    expect("both waiters share one release group",
           Orchestrator.coordinationWaitRecords().count, 1)
    expect("the first waiter's session overlay names one wait",
           Orchestrator.coordination(forTerminal: firstWaiter).waitingOn.count, 1)
    expect("the owner's overlay counts both blocked sessions",
           Orchestrator.coordination(forTerminal: owner).waitedOnBy.count, 2)

    Orchestrator.forget()
    expect("the unresolved wait survives an app-memory reset",
           Orchestrator.coordination(forTerminal: firstWaiter).waitingOn.count, 1)

    var releases: [String] = []
    var releaseWires: [String] = []
    let partial = Orchestrator.releaseCoordinationWait(
        id: waitID, ownerSessionID: owner, commit: "abc123", note: nil,
        deliver: { target, text in
            releases.append(target)
            releaseWires.append(text)
            check("a release notice body names the commit",
                  ClawdlineMessage.decode(text)?.body.contains("abc123") == true)
            return target == secondWaiter ? "terminal closed" : nil
        })
    guard case .refused(let status, let code, _, let extra) = partial else {
        check("a partial fan-out is reported", false); return
    }
    expect("a partial fan-out is an upstream failure", status, 502)
    expect("with a stable coordination delivery code", code, "release_incomplete")
    expect("the response counts the waiter still pending", extra["pending"] as? Int, 1)
    expect("a notified waiter is no longer shown as blocked",
           Orchestrator.coordination(forTerminal: firstWaiter).waitingOn.count, 0)
    expect("the failed waiter remains visibly blocked",
           Orchestrator.coordination(forTerminal: secondWaiter).waitingOn.count, 1)
    check("a waiter receives a strict semantic release with the Git safety instruction",
          releaseWires.first.flatMap(ClawdlineMessage.decode).map {
              guard $0.body.hasSuffix(
                "Re-check HEAD, status and diff before editing or integrating."),
                    case let .fileWaitRelease(id, repository, paths, commit, note) = $0.event
              else { return false }
              return id == waitID && repository == "/Users/me/code/clawdline"
                && paths == ["Sources/Foo.swift"] && commit == "abc123" && note == nil
          } == true)

    let retry = Orchestrator.releaseCoordinationWait(
        id: waitID, ownerSessionID: owner, commit: "abc123", note: "tree rechecked",
        deliver: { target, text in releases.append(target); releaseWires.append(text); return nil })
    guard case .ok(let releasedPayload) = retry else {
        check("the pending release can be retried", false); return
    }
    expect("retry sends only to the waiter that missed the first notice",
           releases, [firstWaiter, secondWaiter, secondWaiter])
    expect("the completed release reports both waiters", releasedPayload["released"] as? Int, 2)
    check("a retry keeps its note as typed data",
          releaseWires.last.flatMap(ClawdlineMessage.decode).map {
              if case let .fileWaitRelease(_, _, _, _, note) = $0.event {
                  return note == "tree rechecked"
              }
              return false
          } == true)
    check("a fully released relationship leaves no active registry row",
          Orchestrator.coordinationWaitRecords().isEmpty)
}

group("coordination wait routes keep their credential and ownership boundaries") {
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
    let owner = "ROUTE-OWNER", waiter = "ROUTE-WAITER"
    let made = Orchestrator.registerCoordinationWait([
        "repository": "/Users/me/code/clawdline", "paths": ["Sources/Foo.swift"],
        "owner_session_id": owner, "waiter_session_id": waiter, "reason": "overlap",
        "release_condition": "explicit release",
    ], deliver: { _, _ in nil })
    guard case .ok(let payload) = made,
          let row = payload["wait"] as? [String: Any],
          let id = row["id"] as? String else {
        check("a route fixture registers", false); return
    }

    let phone = RemoteAuth.addDevice(name: "wait route reader", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: phone.id) }
    let anonymous = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/waits"))
    expect("an anonymous wait-registry read stops at the door", anonymous.status, 401)
    let listed = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/waits",
        headers: ["Authorization": "Bearer \(phone.token)"]))
    expect("a paired reader may see the relationships Session rows expose", listed.status, 200)
    let listedJSON = (try? JSONSerialization.jsonObject(with: listed.body)) as? [String: Any]
    expect("the registry GET includes the active group",
           (listedJSON?["waits"] as? [[String: Any]])?.count, 1)

    let pairedWrite = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/waits",
        headers: ["Authorization": "Bearer \(phone.token)"], body: "{}"))
    expect("a paired device cannot register an agent coordination wait", pairedWrite.status, 403)
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let wrongOwner = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/waits/\(id)/release", headers: auth,
        body: "{\"owner_session_id\":\"SOMEBODY-ELSE\"}"))
    expect("a release must name the persisted owner", wrongOwner.status, 403)
    expect("with the ownership-specific code", remoteErrorCode(wrongOwner), "wrong_owner")
    let cancelled = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/waits/\(id)/cancel", headers: auth,
        body: "{\"waiter_session_id\":\"\(waiter)\"}"))
    expect("the registered waiter may cancel only its membership", cancelled.status, 200)
    check("cancelling the sole waiter removes the group",
          Orchestrator.coordinationWaitRecords().isEmpty)
}

group("session whoami is a machine-only exact-conversation bridge") {
    let path = "/v1/orchestrator/whoami"
    let conversation = "11111111-2222-4333-8444-555555555555"
    let replacement = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    func target(_ id: String, assistant: Assistant? = .codex, name: String = "same label")
        -> TargetSession {
        TargetSession(backend: .iterm, id: id, name: name, tty: "/dev/ttys099",
                      windowIndex: 0, tabIndex: 0, assistant: assistant, cwd: "/same/cwd")
    }
    defer {
        RemoteServer.sessionPayloadForTesting = nil
        RemoteServer.sessionConversationIDForTesting = nil
        RemoteServer.sessionIdentityPassDidFinishForTesting = nil
    }
    let anonymous = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)"))
    expect("anonymous whoami stops at authentication", anonymous.status, 401)

    let phone = RemoteAuth.addDevice(name: "whoami route phone", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: phone.id) }
    let paired = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)",
        headers: ["Authorization": "Bearer \(phone.token)"]))
    expect("a paired device cannot resolve a process identity", paired.status, 403)
    expect("the paired-device refusal names the credential boundary",
           remoteErrorCode(paired), "forbidden")

    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let missing = RemoteServer.shared.route(remoteRequest("GET", path, headers: auth))
    expect("whoami requires exactly one conversation id", missing.status, 400)
    expect("the missing input is typed", remoteErrorCode(missing), "conversation_id_required")
    let malformed = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=not-a-conversation", headers: auth))
    expect("whoami rejects a malformed conversation id", malformed.status, 400)
    expect("the malformed input is typed", remoteErrorCode(malformed),
           "conversation_id_malformed")
    let duplicate = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)&conversation_id=\(conversation)",
        headers: auth))
    expect("whoami rejects a repeated conversation_id", duplicate.status, 400)
    expect("the duplicate query uses the closed-query code", remoteErrorCode(duplicate),
           "conversation_id_required")
    let extra = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)&unknown=1", headers: auth))
    expect("whoami rejects an unknown extra query key", extra.status, 400)
    expect("the extra-key refusal uses the closed-query code", remoteErrorCode(extra),
           "conversation_id_required")
    let empty = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=", headers: auth))
    expect("whoami rejects an empty conversation_id", empty.status, 400)
    expect("the empty-value refusal uses the closed-query code", remoteErrorCode(empty),
           "conversation_id_required")
    let uppercase = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(replacement.uppercased())", headers: auth))
    expect("whoami rejects an uppercase UUID", uppercase.status, 400)
    expect("uppercase is malformed rather than normalized", remoteErrorCode(uppercase),
           "conversation_id_malformed")
    let trailingSlash = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)/?conversation_id=\(conversation)", headers: auth))
    expect("a trailing slash does not widen the exact route", trailingSlash.status, 404)
    let extraSegment = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)/extra?conversation_id=\(conversation)", headers: auth))
    expect("an extra path segment does not widen the exact route", extraSegment.status, 404)

    // Resume/rebind fixture: the stale terminal is absent; the same process-bound conversation
    // now belongs to one newly observed terminal. The route must return that new address.
    let oldTerminal = "22222222-3333-4444-8555-666666666666"
    let newTerminal = "33333333-4444-4555-8666-777777777777"
    RemoteServer.sessionPayloadForTesting = ([target(newTerminal)], [:])
    RemoteServer.sessionConversationIDForTesting = { _ in conversation }
    let rebound = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("a resumed conversation resolves successfully", rebound.status, 200)
    let reboundBody = (try? JSONSerialization.jsonObject(with: rebound.body)) as? [String: Any]
    expect("the old absent terminal is never returned",
           reboundBody?["terminal_id"] as? String, newTerminal)
    expect("the canonical conversation comes from the process-bound match",
           reboundBody?["conversation_id"] as? String, conversation)
    expect("the response names the assistant", reboundBody?["assistant"] as? String, "codex")
    expect("the response schema is closed at five public keys",
           Set(reboundBody?.keys.map { $0 } ?? []),
           Set(["conversation_id", "terminal_id", "assistant", "provenance", "at"]))
    check("the response leaks no label, cwd, pid or tty",
          Set(["label", "cwd", "pid", "tty"]).isDisjoint(
            with: Set(reboundBody?.keys.map { $0 } ?? [])))
    let provenance = reboundBody?["provenance"] as? [String: Any]
    check("the response identifies its registry and consistency boundary",
          provenance?["source"] as? String == "live_session_registry"
            && provenance?["consistency"] as? String == "single_snapshot_revalidated")

    // Mutation 1: accepting the terminal namespace would trust the stale iTerm environment.
    // It is a valid UUID on purpose, so only the namespace boundary can reject it.
    let staleEnvironment = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(oldTerminal)", headers: auth))
    expect("a stale terminal environment id is not accepted as a conversation",
           staleEnvironment.status, 404)
    expect("the stale namespace fails as an exact conversation miss",
           remoteErrorCode(staleEnvironment), "conversation_not_found")

    // Mutation 2: label, cwd and state are identical. Only the exact process-bound value may
    // select the second row; a presentation/ranking resolver would pick the first.
    let lookalike = target("44444444-5555-4666-8777-888888888888")
    let exact = target("55555555-6666-4777-8888-999999999999")
    RemoteServer.sessionPayloadForTesting = ([lookalike, exact],
                                             [lookalike.id: .idle, exact.id: .idle])
    RemoteServer.sessionConversationIDForTesting = { candidate in
        candidate.id == exact.id ? conversation : replacement
    }
    let exactOnly = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    let exactBody = (try? JSONSerialization.jsonObject(with: exactOnly.body)) as? [String: Any]
    expect("same label, cwd and state cannot outrank exact identity",
           exactBody?["terminal_id"] as? String, exact.id)

    // Mutation 3: the reader observes the requested binding, then its replacement. Returning the
    // first terminal without the second pass would make this 200 instead of a typed retry.
    RemoteServer.sessionPayloadForTesting = ([exact], [exact.id: .working("whoami")])
    var reads = 0
    RemoteServer.sessionConversationIDForTesting = { _ in
        reads += 1
        return reads == 1 ? conversation : replacement
    }
    let moved = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("a binding moved between resolution passes is refused", moved.status, 409)
    expect("the moved binding asks the caller to retry", remoteErrorCode(moved),
           "session_identity_stale")

    // Mutation 4a/4b: first-match selection and guessed fallbacks are both forbidden.
    RemoteServer.sessionPayloadForTesting = ([lookalike, exact], [:])
    RemoteServer.sessionConversationIDForTesting = { _ in conversation }
    let ambiguous = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("duplicate process-bound conversation identity fails closed", ambiguous.status, 409)
    expect("the duplicate refusal is typed", remoteErrorCode(ambiguous),
           "conversation_ambiguous")
    RemoteServer.sessionConversationIDForTesting = { _ in replacement }
    let nonexistent = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("a nonexistent conversation is never guessed", nonexistent.status, 404)
    expect("the nonexistent refusal is typed", remoteErrorCode(nonexistent),
           "conversation_not_found")

    let shell = target("66666666-7777-4888-8999-aaaaaaaaaaaa", assistant: nil)
    RemoteServer.sessionPayloadForTesting = ([shell], [:])
    RemoteServer.sessionConversationIDForTesting = { _ in conversation }
    let unsupported = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("a plain-shell row cannot manufacture an assistant identity", unsupported.status, 404)
    expect("the shell remains an exact conversation miss", remoteErrorCode(unsupported),
           "conversation_not_found")

    if let source = try? String(contentsOfFile: "Sources/RemoteServer.swift", encoding: .utf8),
       let protocolPage = try? String(
            contentsOfFile: "docs/clawdline-protocol.html", encoding: .utf8) {
        check("the source and living protocol page both name the whoami route",
              source.contains(#"case ("GET", "/v1/orchestrator/whoami")"#)
                && protocolPage.contains("GET /v1/orchestrator/whoami"))
    } else {
        check("the source and protocol page are readable for the whoami guard", false)
    }
}

group("the session index a wait needs is the dispatch credential's own door") {
    // `POST /v1/orchestrator/waits` takes two terminal-neutral session ids, and the credential
    // that may call it is refused by `GET /v1/sessions` — so before this route the one way in
    // was open and the ids it takes were not readable. This is that door, and it is not the
    // paired-device one: a phone already reads the whole session list next door.
    Orchestrator.forget()
    defer { Orchestrator.forget() }
    let phone = RemoteAuth.addDevice(name: "a phone that may read sessions", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: phone.id) }

    let anonymous = RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/sessions"))
    expect("no credential is stopped at the door", anonymous.status, 401)
    expect("by the ordinary unauthorised word", remoteErrorCode(anonymous), "unauthorized")

    let paired = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/sessions",
        headers: ["Authorization": "Bearer \(phone.token)"]))
    expect("a paired device gets in the door and no further", paired.status, 403)
    expect("and it is a refusal about the credential", remoteErrorCode(paired), "forbidden")

    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let listed = RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/sessions", headers: auth))
    expect("the credential that registers a wait may read the ids it takes", listed.status, 200)
    let body = (try? JSONSerialization.jsonObject(with: listed.body)) as? [String: Any]
    check("the answer carries a session list", body?["sessions"] is [[String: Any]])
    check("stamped like every other orchestrator read", body?["at"] is Int)
    // **The emptiness is installed, not assumed.** This read went through
    // `SessionWatch.publishedInventory()`, which answers with whatever the process has published —
    // and until 2026-09-03 nothing in a test process ever had, so "nothing has read a terminal
    // here" held by accident. It stopped holding when this machine's sessions moved to tmux, which
    // a test process can read with an ordinary subprocess where iTerm2 needed a running app: the
    // group then saw two live sessions on `main` and four an hour later, failing on a number that
    // is a fact about the Mac rather than about the route. What the assertion is for survives
    // intact — an empty inventory publishes as `[]`, an answer, not a refusal and not an absent
    // key — so the inventory it is about is now the one this line puts there.
    SessionWatch.shared.installPublicationForTesting(
        targets: [], states: [:], identities: [:], complete: true,
        observedAt: Date(timeIntervalSince1970: 1_800_000_000))
    let empty = RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/sessions", headers: auth))
    let emptyBody = (try? JSONSerialization.jsonObject(with: empty.body)) as? [String: Any]
    expect("no reading yet is an empty list rather than a missing one",
           (emptyBody?["sessions"] as? [[String: Any]])?.count, 0)

    // The other half of the same sentence: the wait route resolves its waiter against the same
    // population this route publishes, so an id that is not in the index is refused there.
    let unknown = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/waits", headers: auth,
        body: "{\"waiter_session_id\":\"NOT-IN-THE-INDEX\"}"))
    expect("a waiter the index does not list cannot register a wait", unknown.status, 404)
    expect("and says which half was not found", remoteErrorCode(unknown), "waiter_not_found")
}

group("session-message relay has a machine-only, idempotent door") {
    let path = "/v1/orchestrator/messages"
    let body = #"{"from_session":"NO-SOURCE","to_session":"NO-TARGET","text":"status"}"#
    let anonymous = RemoteServer.shared.route(remoteRequest("POST", path, body: body))
    expect("an anonymous relay stops at authentication", anonymous.status, 401)

    let phone = RemoteAuth.addDevice(name: "a phone that may send prompts", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: phone.id) }
    let paired = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: ["Authorization": "Bearer \(phone.token)"], body: body))
    expect("a paired device cannot claim to be an assistant session", paired.status, 403)
    expect("the refusal names the machine credential boundary", remoteErrorCode(paired),
           "forbidden")

    let machine = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let unkeyed = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: machine, body: body))
    expect("a machine relay still requires an idempotency key", unkeyed.status, 400)

    var keyed = machine
    keyed["Idempotency-Key"] = UUID().uuidString
    let malformed = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: keyed, body: #"{"from_session":"NO-SOURCE","text":"status"}"#))
    expect("the relay body is a closed schema", malformed.status, 400)

    keyed["Idempotency-Key"] = UUID().uuidString
    let unresolved = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: keyed, body: body))
    expect("a claimed source must resolve to one current assistant session",
           unresolved.status, 404)
    expect("the source refusal is typed", remoteErrorCode(unresolved), "source_not_found")

    let server = (try? String(contentsOfFile: "Sources/RemoteServer.swift",
                              encoding: .utf8)) ?? ""
    let route = server.components(separatedBy:
        #"case ("POST", "/v1/orchestrator/messages"):"#)
        .dropFirst().first?.components(separatedBy: "\n        // A root's explicit").first ?? ""
    check("the relay route types only the closed encoded envelope",
          route.contains("ClawdlineSessionMessage.encode(message)")
            && route.contains("Targets.send(wire, to: target)")
            && !route.contains("Targets.send(text, to: target)"))
}

group("the wait session index says what a wait must name, and nothing off the screen") {
    // The exposure line, asserted field by field. Everything here is something a caller has to
    // know before it can write a wait down; everything a session says or shows stays out.
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

    defer { SessionNaming.lookForTesting = noSessionNames; SessionNaming.forgetForTesting() }
    SessionNaming.forgetForTesting()
    SessionNaming.lookForTesting = { _ in
        SessionNaming.Name(title: "fix the webhook", handle: nil)
    }
    let claude = TargetSession(backend: .iterm, id: "SESSION-A", name: "Default (python)",
                               tty: "/dev/ttys004", windowIndex: 0, tabIndex: 0,
                               assistant: .claude, cwd: "/Users/me/code/clawdline")
    let codex = TargetSession(backend: .tmux, id: "%codex", name: "envelope work",
                              tty: "/dev/ttys005", windowIndex: 0, tabIndex: 1,
                              assistant: .codex, cwd: "/Users/me/code/clawdline")
    let shell = TargetSession(backend: .iterm, id: "JUST-A-SHELL", name: "-zsh",
                              tty: "/dev/ttys006", windowIndex: 1, tabIndex: 0,
                              assistant: nil, cwd: "/Users/me")
    let homeless = TargetSession(backend: .iterm, id: "NO-CWD", name: "somewhere",
                                 tty: "/dev/ttys999", windowIndex: 2, tabIndex: 0,
                                 assistant: .claude, cwd: nil)
    let states: [String: SessionState] = ["SESSION-A": .working("editing RemoteServer.swift"),
                                          "%codex": .waiting]
    let observedAt = Date(timeIntervalSince1970: 1_800_000_000)
    func published(_ session: TargetSession, pid: Int32) -> SessionWatch.PublishedIdentity {
        SessionWatch.PublishedIdentity(
            assistant: session.assistant!, tty: session.tty, pid: pid,
            processStart: observedAt.addingTimeInterval(-60), conversationID: nil,
            workingDirectory: session.cwd, recordURL: nil, observedAt: observedAt,
            provenance: "coordination_serializer_fixture", conversationSource: .test,
            conversationObservedAt: observedAt)
    }
    let publishedIdentities = [
        claude.id: published(claude, pid: 4_001),
        codex.id: published(codex, pid: 4_002),
        homeless.id: published(homeless, pid: 4_003),
    ]
    let publishedLabels = [
        claude.id: "fix the webhook",
        codex.id: codex.coordinate,
        homeless.id: homeless.coordinate,
    ]

    let rows = RemoteServer.coordinationSessionRows([claude, codex, shell, homeless],
                                                    states: states,
                                                    publishedIdentities: publishedIdentities,
                                                    publishedLabels: publishedLabels)
    expect("a shell prompt is not an address a wait can be delivered to", rows.count, 3)
    check("and it is the one without an assistant in it",
          !rows.contains { $0["id"] as? String == "JUST-A-SHELL" })

    guard let first = rows.first else { check("the assistant sessions are listed", false); return }
    expect("the row is keyed by the terminal-neutral id", first["id"] as? String, "SESSION-A")
    expect("it names which assistant is in there", first["assistant"] as? String, "claude")
    expect("and the checkout the wait is about", first["cwd"] as? String,
           "/Users/me/code/clawdline")
    expect("the label is the conversation's own, not the tab's",
           first["label"] as? String, "fix the webhook")
    check("a tab titled Default does not put Default on a wait",
          first["label"] as? String != claude.label)
    expect("and the terminal state, so a caller knows whether anybody is home",
           first["state"] as? String, "working")
    expect("and every row has exactly one closed work-state projection",
           rows.compactMap { $0["work_state"] as? String },
           ["working", "waiting_you", "unknown"])
    expect("a session showing a question reads as waiting", rows[1]["state"] as? String, "waiting")
    check("a session nothing has read yet is unknown rather than idle",
          rows[2]["state"] as? String == "unknown")
    check("a session with no known checkout omits cwd rather than sending an empty one",
          rows[2]["cwd"] == nil)

    // What must never appear. Each of these is either the screen or the transcript: `line` is
    // what the assistant is doing, `menu` is the question it is asking, `agents` and `shells`
    // are what it has out, and `sessionId` is the name of its transcript file.
    for row in rows {
        for leaked in ["line", "menu", "agents", "shells", "sessionId", "icon", "coordination"] {
            check("the index does not carry \(leaked)", row[leaked] == nil,
                  "\(leaked) reached the dispatch credential")
        }
    }
    check("the working line does not arrive under another name",
          !rows.contains { row in
              row.values.contains { ($0 as? String) == "editing RemoteServer.swift" }
          })

    // Every id the index publishes is one the wait routes can find, because both go through the
    // same lookup. A rename on either side stops here rather than in somebody's dispatch.
    for row in rows {
        let id = row["id"] as? String ?? ""
        check("the index's id is one a wait route resolves",
              RemoteServer.session(withID: id, among: [claude, codex, shell, homeless]) != nil,
              "\(id) is published and cannot be found")
    }

    // A tab this app opened for a task carries the task id, which is the one address that needs
    // no label matching. The same credential already reads the whole record next door.
    let openedFor = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
    let row: [String: Any] = [
        "id": openedFor, "state": "briefed", "kind": "custom", "title": "the envelope work",
        "assistant": "claude", "project_dir": "/Users/me/code/clawdline",
        "timeout_minutes": 30, "created": Date().timeIntervalSince1970,
        "secret_hash": Orchestrator.hash(ofSecret: "x"), "artifacts": [],
        "child_terminal": "SESSION-A", "child_tty": "/dev/ttys004",
    ]
    let data = try! JSONSerialization.data(withJSONObject: ["version": 1, "tasks": [row]])
    try! data.write(to: store, options: .atomic)
    Orchestrator.forget()
    Orchestrator.load(force: true)
    let named = RemoteServer.coordinationSessionRows(
        [claude, codex], states: states, publishedIdentities: publishedIdentities,
        publishedLabels: [claude.id: "the envelope work", codex.id: codex.coordinate])
    expect("a tab Clawdline opened for a task says which task", named.first?["taskId"] as? String,
           openedFor)
    expect("and is called by the title the dispatcher gave it",
           named.first?["label"] as? String, "the envelope work")
    check("a tab a person opened themselves carries no task id", named[1]["taskId"] == nil)
}

group("attached follow-up tasks are single-flight broker work in a standing session") {
    func session(_ id: String, _ assistant: Assistant?) -> TargetSession {
        TargetSession(backend: .iterm, id: id, name: "standing", tty: "/dev/ttys055",
                      windowIndex: 0, tabIndex: 0, assistant: assistant, cwd: "/tmp")
    }
    let standing = session("STANDING", .codex)
    let shell = session("SHELL", nil)
    let existing = Orchestrator.Task(
        id: "11111111-2222-4333-8444-555555555555", state: .briefed, kind: "code",
        title: "already attached", assistant: .codex, projectDir: "/tmp",
        timeoutMinutes: 30, created: Date(), attachSessionId: standing.id,
        childTerminalId: standing.id, secretHash: String(repeating: "0", count: 64))
    let role = Orchestrator.Role(taskID: existing.id, depth: 2, title: existing.title,
                                 deadline: nil, live: false, taskRootAccess: true)
    let leafRole = Orchestrator.Role(taskID: existing.id, depth: 2, title: existing.title,
                                     deadline: nil, live: false, taskRootAccess: false)
    var durableGrant = existing
    durableGrant.childTaskRootAccess = true
    check("the launch-time task-root grant survives a registry round trip",
          OrchestratorStore.task(from: OrchestratorStore.stored(durableGrant))?.childTaskRootAccess == true)

    func refusal(_ decision: OrchestratorDraft.AttachmentDecision) -> (Int, String)? {
        guard case .refused(let status, let code, _) = decision else { return nil }
        return (status, code)
    }
    expect("an unknown attachment is typed 404 before registration",
           refusal(OrchestratorDraft.attachmentDecision(
            sessionID: "UNKNOWN", assistant: .codex, sessions: [standing], states: [:],
            tasks: [], roles: [:], isChoosing: { _ in false }))?.1,
           "attach_session_not_found")
    expect("a plain shell cannot receive a child briefing",
           refusal(OrchestratorDraft.attachmentDecision(
            sessionID: shell.id, assistant: .codex, sessions: [shell], states: [:],
            tasks: [], roles: [:], isChoosing: { _ in false }))?.1,
           "attach_unsupported")
    expect("a session Clawdline never opened for a task cannot be attached to",
           refusal(OrchestratorDraft.attachmentDecision(
            sessionID: standing.id, assistant: .codex, sessions: [standing], states: [:],
            tasks: [], roles: [:], isChoosing: { _ in false }))?.1,
           "attach_not_managed")
    expect("and a person's own session is refused before the assistant is even compared",
           refusal(OrchestratorDraft.attachmentDecision(
            sessionID: standing.id, assistant: .claude, sessions: [standing], states: [:],
            tasks: [], roles: [:], isChoosing: { _ in false }))?.1,
           "attach_not_managed")
    expect("a managed leaf without task-root access cannot host a follow-up",
           refusal(OrchestratorDraft.attachmentDecision(
            sessionID: standing.id, assistant: .codex, sessions: [standing], states: [:],
            tasks: [], roles: [standing.id: leafRole], isChoosing: { _ in false }))?.1,
           "attach_not_managed")
    expect("the task assistant must match the standing assistant",
           refusal(OrchestratorDraft.attachmentDecision(
            sessionID: standing.id, assistant: .claude, sessions: [standing], states: [:],
            tasks: [], roles: [standing.id: role], isChoosing: { _ in false }))?.1,
           "attach_assistant_mismatch")
    expect("one live task occupies a standing session",
           refusal(OrchestratorDraft.attachmentDecision(
            sessionID: standing.id, assistant: .codex, sessions: [standing], states: [:],
            tasks: [existing], roles: [standing.id: role], isChoosing: { _ in false }))?.1,
           "attach_session_occupied")
    check("but not that task's own second resolution, which is how the pump promotes it",
          refusal(OrchestratorDraft.attachmentDecision(
            sessionID: standing.id, assistant: .codex, sessions: [standing], states: [:],
            tasks: [existing], roles: [standing.id: role], isChoosing: { _ in false },
            excluding: existing.id)) == nil)
    expect("a cached waiting state plus the narrow menu proof refuses before typing",
           refusal(OrchestratorDraft.attachmentDecision(
            sessionID: standing.id, assistant: .codex, sessions: [standing],
            states: [standing.id: .waiting], tasks: [], roles: [standing.id: role],
            isChoosing: { _ in true }))?.1,
           "attach_session_busy")
    check("waiting without the menu proof is accepted",
          refusal(OrchestratorDraft.attachmentDecision(
            sessionID: standing.id, assistant: .codex, sessions: [standing],
            states: [standing.id: .waiting], tasks: [], roles: [standing.id: role],
            isChoosing: { _ in false })) == nil)
    if case .accepted(_, let depth) = OrchestratorDraft.attachmentDecision(
        sessionID: standing.id, assistant: .codex, sessions: [standing], states: [:],
        tasks: [], roles: [standing.id: role], isChoosing: { _ in false }) {
        expect("attachment keeps the standing session's existing depth", depth, 2)
    } else {
        check("a valid standing session is accepted", false)
    }

    var holder = existing
    holder.rootSessionId = "root-a"
    holder.claims = ["Sources/Feature.swift"]
    holder.claimsDeclared = true
    holder.claimKeys = OrchestratorDraft.freezeClaims(holder.claims, projectDir: holder.projectDir)
    var candidate = Orchestrator.Task(
        id: "66666666-7777-4888-8999-aaaaaaaaaaaa", state: .queued, kind: "code",
        title: "conflicting follow-up", assistant: .codex, projectDir: "/tmp",
        timeoutMinutes: 30, created: Date(), rootSessionId: "root-b",
        attachSessionId: "OTHER", secretHash: String(repeating: "0", count: 64))
    candidate.claims = holder.claims
    candidate.claimsDeclared = true
    candidate.claimKeys = OrchestratorDraft.freezeClaims(candidate.claims,
                                                     projectDir: candidate.projectDir)
    check("an attached live task reserves claims through the ordinary workspace gate",
          OrchestratorDraft.claimsOverlaps(for: candidate, among: [holder]).first?.blocks == true)

    let manager = FileManager.default
    let store = Orchestrator.storeURL
    let storeBefore = try? Data(contentsOf: store)
    let lingerBefore = Config.shared.orchestratorChildLinger
    defer {
        Config.shared.orchestratorChildLinger = lingerBefore
        if let storeBefore { try? storeBefore.write(to: store, options: .atomic) }
        else { try? manager.removeItem(at: store) }
        Orchestrator.forget()
    }
    for linger in [-1, 0, 180] {
        Orchestrator.forget()
        Config.shared.orchestratorChildLinger = linger
        let attached = Orchestrator.Task(
            id: UUID().uuidString.lowercased(), state: .briefed, kind: "code",
            title: "attached completion", assistant: .codex, projectDir: "/tmp",
            timeoutMinutes: 30, created: Date(), attachSessionId: standing.id,
            childTerminalId: standing.id, secretHash: String(repeating: "0", count: 64))
        Orchestrator.holdScheduleTaskForTesting(attached)
        Orchestrator.finalize(attached.id, as: .success, summary: "done")
        check("attached completion never schedules its tab to close at linger \(linger)",
              Orchestrator.closeAtForTesting(attached.id) == nil)
        let record = Orchestrator.record(id: attached.id)
        check("the task record marks the follow-up as attached",
              record?["attached"] as? Bool == true
                && record?["attachSession"] as? String == standing.id)
    }
    let brief = Orchestrator.childBrief(for: existing)
    check("an attached briefing names the standing-session lifecycle",
          brief.contains("standing session") && brief.contains("does not end this session"))
}

group("a file-wait notice is not typed into a session that is showing a picker") {
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

    let owner = "BUSY-OWNER", waiter = "BUSY-WAITER"
    let body: [String: Any] = [
        "repository": "/Users/me/code/clawdline", "paths": ["Sources/Foo.swift"],
        "owner_session_id": owner, "waiter_session_id": waiter,
        "reason": "the owner is mid-edit", "release_condition": "committed",
    ]
    var attempts: [String] = []
    let blocked = Orchestrator.registerCoordinationWait(
        body, readiness: { _ in "that session is showing a menu" },
        deliver: { target, _ in attempts.append(target); return nil })
    guard case .refused(let status, let code, _, _) = blocked else {
        check("registering against a session showing a picker is refused", false); return
    }
    expect("a session that cannot be typed into has not failed to receive anything", status, 409)
    expect("with a code that says the owner is busy rather than unreachable", code, "owner_busy")
    check("nothing was typed into the picker", attempts.isEmpty)

    func waiterRow() -> [String: Any] {
        (Orchestrator.coordinationWaitRecords().first?["waiters"] as? [[String: Any]])?
            .first(where: { $0["sessionId"] as? String == waiter }) ?? [:]
    }
    check("the wait is still durable so the waiter is not silently unblocked",
          Orchestrator.coordinationWaitRecords().count == 1
              && waiterRow()["sessionId"] as? String == waiter)
    check("and the request is recorded as still owed, not as delivered",
          waiterRow()["requestDeliveredAt"] == nil)
    expect("the waiter's own overlay still names the wait",
           Orchestrator.coordination(forTerminal: waiter).waitingOn.count, 1)

    // The bug this pins: a blocked send that had been receipted as delivered would be deduplicated
    // away here, and the owner would never hear about this waiter again.
    let retry = Orchestrator.registerCoordinationWait(
        body, readiness: { _ in nil },
        deliver: { target, _ in attempts.append(target); return nil })
    guard case .ok(let retried) = retry else {
        check("the same registration can be retried once the picker is gone", false); return
    }
    expect("the retry reuses the relationship rather than making a second one",
           retried["deduplicated"] as? Bool, true)
    expect("and this time the owner is told", attempts, [owner])
    check("and the delivery is receipted", waiterRow()["requestDeliveredAt"] != nil)

    // Release takes the same guard, but keeps the existing partial-fan-out code: from the owner's
    // side "one waiter did not get it, retry" is the same situation whether the message failed to
    // send or was never sent at all.
    guard let waitID = Orchestrator.coordinationWaitRecords().first?["id"] as? String else {
        check("the group has an id to release", false); return
    }
    var releases: [String] = []
    let held = Orchestrator.releaseCoordinationWait(
        id: waitID, ownerSessionID: owner, commit: "beef123", note: nil,
        readiness: { _ in "that session is showing a menu" },
        deliver: { target, _ in releases.append(target); return nil })
    guard case .refused(let heldStatus, let heldCode, _, let extra) = held else {
        check("a release nobody could be told about is not a completed release", false); return
    }
    expect("an undeliverable release is the existing incomplete answer", heldStatus, 502)
    expect("with the existing code", heldCode, "release_incomplete")
    expect("and it counts the waiter still owed a notice", extra["pending"] as? Int, 1)
    check("nothing was typed into the waiter's picker", releases.isEmpty)
    check("and no release receipt was written",
          waiterRow()["sessionId"] as? String == waiter
              && waiterRow()["releaseDeliveredAt"] == nil)
    expect("so the waiter is still shown as blocked",
           Orchestrator.coordination(forTerminal: waiter).waitingOn.count, 1)

    let completed = Orchestrator.releaseCoordinationWait(
        id: waitID, ownerSessionID: owner, commit: "beef123", note: nil,
        readiness: { _ in nil },
        deliver: { target, _ in releases.append(target); return nil })
    guard case .ok = completed else {
        check("the release completes once the waiter can be typed into", false); return
    }
    expect("the retry reaches the waiter that was skipped", releases, [waiter])
    check("and the fully released group leaves the registry",
          Orchestrator.coordinationWaitRecords().isEmpty)
}

group("A3 owner visibility: a session row says when peers are parked on it") {
    // The overlay the broker hands the row, built by hand. `coordination(forTerminal:)` is
    // already held by the broker group above; what is unheld is the *drawing* rule, and it has
    // three cases — blocked, blocking, and both at once — that no running app makes easy to see.
    func wait(id: String, owner: String, waiter: String?, condition: String) -> [String: Any] {
        var row: [String: Any] = [
            "id": id, "repository": "/Users/me/code/clawdline",
            "paths": ["Sources/Foo.swift"], "ownerSessionId": owner,
            "releaseCondition": condition, "createdAt": 1_800_000_000,
            "reason": "the same file",
        ]
        if let waiter { row["waiterSessionId"] = waiter }
        return row
    }
    let names = ["OWNER": "docs pass before the release", "WAIT-A": "update the Artifact",
                 "WAIT-B": "rewrite the importer"]
    func said(_ coordination: Orchestrator.Coordination) -> String? {
        PromptController.coordinationWaitSaid(coordination) { names[$0] }
    }
    func owed(_ n: Int) -> String {
        n == 1 ? L.t.sessionWaitedOnByOne
               : L.t.sessionWaitedOnByMany.replacingOccurrences(of: "{n}", with: "\(n)")
    }

    let quiet = said(Orchestrator.Coordination(waitingOn: [], waitedOnBy: []))
    check("a session in no relationship says nothing at all", quiet == nil)

    // The side that already worked, kept exactly as it was. The owner half is an addition, not
    // a redesign: a waiter's row still names the peer to go and ask, and still counts the rest.
    let blocked = said(Orchestrator.Coordination(waitingOn: [
        wait(id: "one", owner: "OWNER", waiter: nil, condition: "the docs are committed"),
    ], waitedOnBy: []))
    expect("a blocked session still names its owner and the release condition",
           blocked, "⏳ docs pass before the release · the docs are committed")
    let blockedTwice = said(Orchestrator.Coordination(waitingOn: [
        wait(id: "one", owner: "OWNER", waiter: nil, condition: "the docs are committed"),
        wait(id: "two", owner: "WAIT-B", waiter: nil, condition: "the importer lands"),
    ], waitedOnBy: []))
    expect("and still counts the owners it did not have room to name",
           blockedTwice, "⏳ docs pass before the release · the docs are committed  +1")

    // **The bug this is about.** The registry knew, the API said so, and both lists drew the
    // owner's row exactly as they draw a session in no relationship at all.
    let blocking = said(Orchestrator.Coordination(waitingOn: [], waitedOnBy: [
        wait(id: "one", owner: "OWNER", waiter: "WAIT-A", condition: "the docs are committed"),
        wait(id: "one", owner: "OWNER", waiter: "WAIT-B", condition: "the docs are committed"),
    ]))
    check("an owner's row is no longer identical to an unrelated one", blocking != quiet)
    expect("it counts the sessions parked on this one and says what would free them",
           blocking, "⏳ \(owed(2)) · the docs are committed")
    check("the count is the app's own words, not a session label",
          blocking?.contains(owed(2)) == true && blocking?.contains("update the Artifact") == false)

    // A count and a `+N` would be the same sessions twice, so the count is the `+N`: one waiter
    // reads as one, and the condition beside it belongs to the group that row came from.
    let blockingOnce = said(Orchestrator.Coordination(waitingOn: [], waitedOnBy: [
        wait(id: "one", owner: "OWNER", waiter: "WAIT-A", condition: "the docs are committed"),
    ]))
    expect("a single waiter is counted, not suffixed",
           blockingOnce, "⏳ \(owed(1)) · the docs are committed")

    // **One is the ordinary case, not the edge of it.** Most of the time a single session is
    // parked on you, and this sentence has a verb in it: with one plural key the German row read
    // "1 warten auf dich" — the count an owner sees most often, in the one shape the language
    // does not allow. Named rather than derived, because which languages inflect here is a fact
    // about those languages and not something the count can be asked.
    // Read in German on purpose. In English the two sentences are the same string once the 1
    // is in it, so an English-only check here passes whichever key the rule reaches for — the
    // exact shape of a test that cannot fail.
    let onceInGerman = PromptController.coordinationWaitSaid(
        Orchestrator.Coordination(waitingOn: [], waitedOnBy: [
            wait(id: "one", owner: "OWNER", waiter: "WAIT-A", condition: "the docs are committed"),
        ]), copy: German()) { names[$0] }
    expect("one waiter reads as the singular sentence in a language that inflects",
           onceInGerman, "⏳ \(German().sessionWaitedOnByOne) · the docs are committed")
    let twiceInGerman = PromptController.coordinationWaitSaid(
        Orchestrator.Coordination(waitingOn: [], waitedOnBy: [
            wait(id: "one", owner: "OWNER", waiter: "WAIT-A", condition: "the docs are committed"),
            wait(id: "one", owner: "OWNER", waiter: "WAIT-B", condition: "the docs are committed"),
        ]), copy: German()) { names[$0] }
    check("and two waiters reach for the other sentence, not the same one",
          twiceInGerman?.contains(German().sessionWaitedOnByOne) == false
              && twiceInGerman?.contains("2") == true)
    let inflecting = ["de", "fr", "es", "it", "ru", "hi"]
    let uninflected = L.catalog.filter { inflecting.contains($0.tag) }.filter {
        $0.copy.sessionWaitedOnByOne
            == $0.copy.sessionWaitedOnByMany.replacingOccurrences(of: "{n}", with: "1")
    }.map { $0.tag }.sorted()
    check("a language whose verb agrees with the count says one differently from many",
          uninflected.isEmpty, uninflected.joined(separator: ", "))
    // The plural keeps the hole. A translation that dropped it would draw a row that says
    // somebody is stuck without ever saying how many.
    let holeless = L.catalog.filter { !$0.copy.sessionWaitedOnByMany.contains("{n}") }
        .map { $0.tag }.sorted()
    check("every language keeps the hole the count goes in", holeless.isEmpty,
          holeless.joined(separator: ", "))

    // Both at once — A waits on B while C waits on A. Waiting leads, because that is the rule
    // the API already states (`state` is `waiting_on_session` whenever `waitingOn` is not empty)
    // and because the peer to go and ask is the more useful half. The owed count still goes on
    // the end: dropping it here would put this row back in the state this whole change is about.
    let both = said(Orchestrator.Coordination(waitingOn: [
        wait(id: "one", owner: "OWNER", waiter: nil, condition: "the docs are committed"),
    ], waitedOnBy: [
        wait(id: "two", owner: "ME", waiter: "WAIT-B", condition: "the importer lands"),
    ]))
    expect("a session on both sides leads with who it is waiting for, then what it owes",
           both, "⏳ docs pass before the release · the docs are committed  ·  \(owed(1))")

    // A relationship that outlived a tab. The label lookup answers nil, and the row falls back
    // to the id rather than to silence — an unresolved wait is exactly the one worth seeing.
    let gone = said(Orchestrator.Coordination(waitingOn: [
        wait(id: "one", owner: "CLOSED-TAB", waiter: nil, condition: "the branch is pushed"),
    ], waitedOnBy: []))
    expect("a wait on a session this Mac cannot see still draws",
           gone, "⏳ CLOSED-TAB · the branch is pushed")
}

group("A3 owner visibility: the page draws the same two sides as the Mac") {
    // The page is JavaScript with no test runner in this repo, so it is held the way
    // `transcript.js` is held one group over: by reading the source and pinning the shape of it.
    let js = (try? String(contentsOfFile: "Resources/web/app/js/view/list.js",
                          encoding: .utf8)) ?? ""
    let derive = (try? String(contentsOfFile: "Resources/web/app/js/view/derive.js",
                              encoding: .utf8)) ?? ""
    let mock = (try? String(contentsOfFile: "Resources/web/app/js/net/mock.js",
                            encoding: .utf8)) ?? ""
    let fallback = (try? String(contentsOfFile: "Resources/web/app/js/core/i18n.js",
                                encoding: .utf8)) ?? ""
    check("the list source was read", js.contains("function fillRow"))

    check("the row reads the owner's half of the overlay, not only the waiter's",
          js.contains("coordination.waitedOnBy"))
    check("the owed count is a translated string with a number filled into it",
          js.contains("T.sessionWaitedOnByMany") && js.contains("fill(T.sessionWaitedOnByMany")
              && js.contains("T.sessionWaitedOnByOne"))
    check("and it is escaped like every other value on the row",
          js.contains("esc(peerText)") || js.contains("esc(ownedText)") ||
              (js.contains(#"sessionStatusGlyphHTML("⏳", peerText)"#) &&
               derive.contains(#"session-status-label">' + attr(copy)"#)))
    // The cache key decides whether the row is redrawn at all. A row that gains a waiter while
    // the page is open and keeps the shape it had is a row that never says so.
    check("the redraw key changes when the owner's half changes",
          js.contains("waitedOnBy") && js.contains("+cw"))

    // The name and a value, not the name anywhere in the file: this used to pass on the comment
    // one line above the entry, which is the exact shape of a check that cannot fail.
    check("the page carries an English fallback for the new string",
          fallback.contains("sessionWaitedOnByOne: \"")
              && fallback.contains("sessionWaitedOnByMany: \""))
    let sent = ((try? JSONSerialization.jsonObject(
        with: RemoteServer.shared.route(remoteRequest("GET", "/v1/strings")).body))
        as? [String: Any]) ?? [:]
    expect("and the server sends the translated one",
           sent["sessionWaitedOnByMany"] as? String, L.t.sessionWaitedOnByMany)
    expect("and the singular one beside it",
           sent["sessionWaitedOnByOne"] as? String, L.t.sessionWaitedOnByOne)

    // `?mock=1` is how this row is looked at without two real terminals parked on each other.
    check("the mock list has an owner with waiters on it",
          mock.contains("has_waiters") && mock.contains("waiterSessionId"))
}

group("handing off is the other thing a paired device may not do") {
    Orchestrator.forget()
    defer { Orchestrator.forget() }
    let phone = RemoteAuth.addDevice(name: "a phone that may send", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: phone.id) }
    let body = "{\"handoff_id\":\"\(taskID)\"}"

    let anonymous = RemoteServer.shared.route(
        remoteRequest("POST", "/v1/orchestrator/handoffs", body: body))
    expect("a handoff with no credential is stopped at the door", anonymous.status, 401)
    expect("by ordinary unauthorised auth", remoteErrorCode(anonymous), "unauthorized")
    let paired = RemoteServer.shared.route(
        remoteRequest("POST", "/v1/orchestrator/handoffs",
                      headers: ["Authorization": "Bearer \(phone.token)",
                                "Idempotency-Key": UUID().uuidString], body: body))
    expect("a paired device cannot hand work to a new tab", paired.status, 403)
    expect("the refusal is the orchestrator credential", remoteErrorCode(paired), "forbidden")
}

group("a handoff request is parsed before the shared switch, and no further") {
    Orchestrator.forget()
    let enabled = Config.shared.orchestratorEnabled
    defer { Config.shared.orchestratorEnabled = enabled; Orchestrator.forget() }
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    Config.shared.orchestratorEnabled = false
    let malformed = RemoteServer.shared.route(
        remoteRequest("POST", "/v1/orchestrator/handoffs", headers: auth, body: "no"))
    expect("a non-JSON body is a request refusal first", malformed.status, 400)
    expect("and uses the existing envelope word", remoteErrorCode(malformed), "bad_request")
    let missing = RemoteServer.shared.route(
        remoteRequest("POST", "/v1/orchestrator/handoffs", headers: auth, body: "{}"))
    expect("a missing id is the same request refusal", missing.status, 400)
    let disabled = RemoteServer.shared.route(
        remoteRequest("POST", "/v1/orchestrator/handoffs", headers: auth,
                      body: "{\"handoff_id\":\"\(taskID)\"}"))
    expect("only a shaped request reaches the shared switch", disabled.status, 403)
    expect("and gets the dispatch switch's code", remoteErrorCode(disabled), "orchestrator_disabled")
}

group("dispatching is the one thing a paired device may not do") {
    // The whole point of the second credential. A phone with `send` can already type into a
    // session; opening a *new* one from a task file somebody else wrote is a different power, and
    // it is behind a `0600` file no page can read.
    Orchestrator.forget()
    defer { Orchestrator.forget() }
    let phone = RemoteAuth.addDevice(name: "a phone that may send", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: phone.id) }

    let body = "{\"task_id\":\"\(taskID)\",\"secret\":\"\(String(repeating: "a1", count: 32))\"}"
    let anonymous = RemoteServer.shared.route(
        remoteRequest("POST", "/v1/orchestrator/tasks", body: body))
    expect("nothing at all is turned away at the door", anonymous.status, 401)
    expect("and says so", remoteErrorCode(anonymous), "unauthorized")

    let paired = RemoteServer.shared.route(
        remoteRequest("POST", "/v1/orchestrator/tasks",
                      headers: ["Authorization": "Bearer \(phone.token)",
                                "Idempotency-Key": UUID().uuidString],
                      body: body))
    expect("a paired device gets in the door and no further", paired.status, 403)
    expect("and it is a refusal about the credential, not the task",
           remoteErrorCode(paired), "forbidden")

    let reading = RemoteServer.shared.route(remoteRequest("GET", "/v1/orchestrator/tasks"))
    expect("reading the list needs a credential too", reading.status, 401)
    expect("and it is the ordinary one", remoteErrorCode(reading), "unauthorized")
}

group("finishing a task takes that task's secret and nothing else") {
    // The completion route is the one place a *child* speaks, and a child was never given a
    // device token — so it is exempt from the door and gated on the secret alone. The task is put
    // into the store rather than dispatched, because dispatching opens a terminal tab.
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
    let secret = String(repeating: "a1", count: 32)
    let row: [String: Any] = ["id": taskID, "state": "briefed", "kind": "custom",
                              "title": "a task", "assistant": "codex", "project_dir": "/tmp",
                              "timeout_minutes": 30, "created": Date().timeIntervalSince1970,
                              "secret_hash": Orchestrator.hash(ofSecret: secret),
                              "artifacts": []]
    let stored = (try? JSONSerialization.data(withJSONObject: ["version": 1, "tasks": [row]])) ?? Data()
    try? FileManager.default.createDirectory(at: store.deletingLastPathComponent(),
                                             withIntermediateDirectories: true)
    try? stored.write(to: store, options: .atomic)

    func finish(_ id: String, secret: String?) -> RemoteServer.Response {
        var headers: [String: String] = [:]
        if let secret { headers["X-Clawdline-Task-Secret"] = secret }
        return RemoteServer.shared.route(
            remoteRequest("POST", "/v1/orchestrator/tasks/\(id)/complete", headers: headers,
                          body: "{\"status\":\"success\",\"summary\":\"drew it\"}"))
    }

    let wrong = finish(taskID, secret: String(repeating: "b2", count: 32))
    expect("another task's secret is not this task's", wrong.status, 403)
    expect("and it is a plain refusal", remoteErrorCode(wrong), "forbidden")
    let silent = finish(taskID, secret: nil)
    expect("no secret at all is the same refusal", silent.status, 403)
    let unknown = finish("11111111-2222-3333-4444-555555555555", secret: secret)
    expect("a task nobody registered is a 404", unknown.status, 404)
    expect("and says nothing else about it", remoteErrorCode(unknown), "not_found")
}

group("the agent-notification preference defaults on, including for old config files") {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-agent-notify-config-\(UUID().uuidString)",
                                isDirectory: true)
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }

    let absent = Config(directoryForTesting: directory)
    expect("agent notifications default on", absent.orchestratorAgentNotify, true)

    try! Data("{}".utf8).write(to: directory.appendingPathComponent("config.json"))
    let missing = Config(directoryForTesting: directory)
    expect("an old config with no agent-notify key keeps them on",
           missing.orchestratorAgentNotify, true)

    try! Data("{\"orchestrator_agent_notify\":false}".utf8)
        .write(to: directory.appendingPathComponent("config.json"))
    let disabled = Config(directoryForTesting: directory)
    expect("the explicit off value is loaded", disabled.orchestratorAgentNotify, false)
    disabled.orchestratorAgentNotify = true
    disabled.save()
    let written = (try? Data(contentsOf: disabled.fileURL))
        .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
    expect("saving uses the orchestrator_agent_notify key",
           written?["orchestrator_agent_notify"] as? Bool, true)
}

group("smart notifications are an explicit quota-spending preference") {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-smart-notify-config-\(UUID().uuidString)",
                                isDirectory: true)
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }

    let absent = Config(directoryForTesting: directory)
    expect("smart notifications default off", absent.smartNotifications, false)

    try! Data("{}".utf8).write(to: directory.appendingPathComponent("config.json"))
    let missing = Config(directoryForTesting: directory)
    expect("an old config does not silently start spending assistant quota",
           missing.smartNotifications, false)

    try! Data("{\"smart_notifications\":true}".utf8)
        .write(to: directory.appendingPathComponent("config.json"))
    let enabled = Config(directoryForTesting: directory)
    expect("the explicit on value is loaded", enabled.smartNotifications, true)
    enabled.save()
    let written = (try? Data(contentsOf: enabled.fileURL))
        .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
    expect("saving uses the smart_notifications key",
           written?["smart_notifications"] as? Bool, true)
}

group("an agent notification is narrow, scarce and audited") {
    Orchestrator.forget()
    let agentNotifyWasEnabled = Config.shared.orchestratorAgentNotify
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        Config.shared.orchestratorAgentNotify = agentNotifyWasEnabled
        if let before {
            try? before.write(to: store, options: .atomic)
        } else {
            try? FileManager.default.removeItem(at: store)
        }
        Orchestrator.forget()
    }
    let secret = String(repeating: "d4", count: 32)
    func row(_ id: String, state: String = "briefed", finishedAgo: TimeInterval? = nil)
        -> [String: Any] {
        var value: [String: Any] = [
            "id": id, "state": state, "kind": "custom", "title": "daily weather",
            "assistant": "codex", "project_dir": "/tmp", "timeout_minutes": 30,
            "created": Date().addingTimeInterval(-120).timeIntervalSince1970,
            "secret_hash": Orchestrator.hash(ofSecret: secret), "artifacts": [],
        ]
        if let finishedAgo {
            value["finished_at"] = Date().addingTimeInterval(-finishedAgo).timeIntervalSince1970
        }
        return value
    }
    func install(_ rows: [[String: Any]]) {
        let data = (try? JSONSerialization.data(withJSONObject: ["version": 1, "tasks": rows]))
            ?? Data()
        try? FileManager.default.createDirectory(at: store.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true)
        try? data.write(to: store, options: .atomic)
        Orchestrator.forget()
    }
    func taskNotify(_ id: String, secret presented: String = secret,
                    secretInHeader: Bool = false,
                    title: String = "forecast", body: String = "sunny")
        -> RemoteServer.Response {
        var obj = ["title": title, "body": body]
        var headers: [String: String] = [:]
        if secretInHeader {
            headers["X-Clawdline-Task-Secret"] = presented
        } else {
            obj["secret"] = presented
        }
        let data = try! JSONSerialization.data(withJSONObject: obj)
        return RemoteServer.shared.route(remoteRequest(
            "POST", "/v1/orchestrator/tasks/\(id)/notify",
            headers: headers,
            body: String(decoding: data, as: UTF8.self)))
    }

    install([row(taskID)])
    var displayedTitle = ""
    var displayedTag: String?
    var displayedIcon: String?
    let delivered = WebPush.Delivery(sent: 1, failed: 0)
    Orchestrator.agentPushForTesting = { title, _, _, tag, icon in
        displayedTitle = title
        displayedTag = tag
        displayedIcon = icon
        return delivered
    }
    var disabledTaskPushes = 0
    Orchestrator.agentPushForTesting = { _, _, _, _, _ in
        disabledTaskPushes += 1
        return delivered
    }
    Config.shared.orchestratorAgentNotify = false
    let disabledTask = taskNotify(taskID)
    expect("the task route names a disabled agent-notification preference",
           disabledTask.status, 409)
    expect("the task route returns its dedicated disabled code",
           remoteErrorCode(disabledTask), "agent_notify_disabled")
    check("the task refusal says who disabled it and where to re-enable it",
          remoteErrorMessage(disabledTask).contains("user")
              && remoteErrorMessage(disabledTask).contains("Settings → Remote"))
    expect("the task route does not attempt delivery while disabled", disabledTaskPushes, 0)
    Config.shared.orchestratorAgentNotify = true
    Orchestrator.agentPushForTesting = { title, _, _, tag, icon in
        displayedTitle = title
        displayedTag = tag
        displayedIcon = icon
        return delivered
    }
    for index in 1...5 {
        expect("a disabled refusal did not spend task allowance \(index)",
               taskNotify(taskID).status, 200)
    }
    expect("only the sixth delivery after a disabled refusal spends past the task allowance",
           taskNotify(taskID).status, 429)
    let wrong = taskNotify(taskID, secret: String(repeating: "e5", count: 32))
    expect("the task route rejects a different task's secret", wrong.status, 403)
    expect("with the complete route's error semantics", remoteErrorCode(wrong), "forbidden")
    let rejectedAudit = RemoteAuth.recentAudit(limit: 5).first {
        $0["event"] as? String == "orchestrator.notify"
            && $0["result"] as? String == "bad_secret"
    }
    check("an unverified notification never writes caller-controlled content",
          rejectedAudit?["title"] == nil && rejectedAudit?["body"] == nil)
    check("an unverified notification records only a source and short attempt hash",
          rejectedAudit?["source"] as? String == "task_secret"
              && (rejectedAudit?["attempt"] as? String)?.count == 12)
    _ = taskNotify(taskID, secret: String(repeating: "e5", count: 32))
    _ = taskNotify(taskID, secret: String(repeating: "e5", count: 32))
    let flooded = taskNotify(taskID, secret: String(repeating: "e5", count: 32))
    expect("the fourth unverified notification attempt is throttled", flooded.status, 429)
    expect("with the ordinary rate-limited code", remoteErrorCode(flooded), "rate_limited")

    Orchestrator.forget()
    let unknownID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    for index in 1...3 {
        expect("an unknown task gets the complete route's 404 before throttling — \(index)",
               taskNotify(unknownID).status, 404)
    }
    expect("unknown-task probes share the unverified-attempt throttle",
           taskNotify(unknownID).status, 429)

    Orchestrator.forget()
    install([row(taskID)])
    Orchestrator.agentPushForTesting = { title, _, _, tag, icon in
        displayedTitle = title
        displayedTag = tag
        displayedIcon = icon
        return delivered
    }
    expect("the task secret is accepted in the same header as complete",
           taskNotify(taskID, secretInHeader: true).status, 200)

    Orchestrator.forget()
    install([row(taskID)])
    let unsubscribed = taskNotify(taskID)
    expect("a task with no push subscription is told synchronously", unsubscribed.status, 409)
    expect("with the push test route's existing code", remoteErrorCode(unsubscribed),
           "not_subscribed")
    let noSubscriptionAudit = RemoteAuth.recentAudit(limit: 5).first {
        $0["event"] as? String == "orchestrator.notify"
            && $0["result"] as? String == "not_subscribed"
    }
    expect("the no-subscription audit records no false success",
           noSubscriptionAudit?["sent"] as? String, "0")
    Orchestrator.agentPushForTesting = { title, _, _, tag, icon in
        displayedTitle = title
        displayedTag = tag
        displayedIcon = icon
        return delivered
    }
    for index in 1...5 {
        expect("each of the task's five notifications is accepted — \(index)",
               taskNotify(taskID).status, 200)
    }
    expect("the task name prefixes the agent's notification title", displayedTitle,
           "daily weather: forecast")
    expect("task content notifications have one stable push topic", displayedTag,
           "agent-task-\(taskID)")
    check("task content notifications carry their project icon", displayedIcon != nil)
    Orchestrator.forget()
    Orchestrator.agentPushForTesting = { _, _, _, _, _ in delivered }
    let sixth = taskNotify(taskID)
    expect("a sixth notification from one task is refused", sixth.status, 429)
    expect("with a limit distinct from the machine brake", remoteErrorCode(sixth), "notify_limit")
    let acceptedAudit = RemoteAuth.recentAudit(limit: 20).first {
        $0["event"] as? String == "orchestrator.notify"
            && $0["task_id"] as? String == taskID
            && $0["title"] as? String == "forecast"
            && $0["result"] as? String == "sent"
    }
    expect("the audit names the delivered result", acceptedAudit?["result"] as? String, "sent")
    expect("and records the accepted subscriptions", acceptedAudit?["sent"] as? String, "1")
    expect("and records failed subscriptions", acceptedAudit?["failed"] as? String, "0")

    let recent = "11111111-2222-3333-4444-555555555555"
    let expired = "22222222-3333-4444-5555-666666666666"
    install([row(recent, state: "success", finishedAgo: 59),
             row(expired, state: "success", finishedAgo: 61)])
    Orchestrator.agentPushForTesting = { _, _, _, _, _ in delivered }
    expect("a child may still speak just after reporting", taskNotify(recent).status, 200)
    let late = taskNotify(expired)
    expect("the task secret's notification opening closes after sixty seconds", late.status, 409)
    expect("and says that the opening expired", remoteErrorCode(late), "notify_expired")

    let scheduled = "33333333-4444-5555-6666-777777777777"
    var scheduledRow = row(scheduled)
    scheduledRow["title"] = "weather worker"
    scheduledRow["schedule_id"] = "44444444-5555-6666-7777-888888888888"
    scheduledRow["root_label"] = "morning weather"
    install([scheduledRow])
    Orchestrator.agentPushForTesting = { title, _, _, tag, icon in
        displayedTitle = title
        displayedTag = tag
        displayedIcon = icon
        return delivered
    }
    expect("a scheduled task may notify", taskNotify(scheduled).status, 200)
    expect("and its schedule name is the visible source", displayedTitle,
           "morning weather: forecast")

    let failing = "55555555-6666-7777-8888-999999999999"
    install([row(failing)])
    Orchestrator.agentPushForTesting = { _, _, _, _, _ in
        WebPush.Delivery(sent: 0, failed: 2)
    }
    let upstreamFailure = taskNotify(failing)
    expect("push-service refusals are not reported as success", upstreamFailure.status, 502)
    expect("push-service refusals use a distinct code", remoteErrorCode(upstreamFailure),
           "push_failed")
    let upstreamObject = (try? JSONSerialization.jsonObject(with: upstreamFailure.body))
        as? [String: Any]
    let upstreamError = upstreamObject?["error"] as? [String: Any]
    expect("the failed response reports accepted subscriptions", upstreamError?["sent"] as? Int, 0)
    expect("the failed response reports refused subscriptions", upstreamError?["failed"] as? Int, 2)
    let failedAudit = RemoteAuth.recentAudit(limit: 10).first {
        $0["event"] as? String == "orchestrator.notify"
            && $0["result"] as? String == "failed"
    }
    expect("a failed delivery audit records zero successes", failedAudit?["sent"] as? String, "0")
    expect("a failed delivery audit records every refusal", failedAudit?["failed"] as? String, "2")

    Orchestrator.forget()
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    func rootNotify(title: String, body: String) -> RemoteServer.Response {
        let data = try! JSONSerialization.data(withJSONObject: ["title": title, "body": body])
        return RemoteServer.shared.route(remoteRequest(
            "POST", "/v1/orchestrator/notify", headers: auth,
            body: String(decoding: data, as: UTF8.self)))
    }
    var disabledRootPushes = 0
    Orchestrator.agentPushForTesting = { _, _, _, _, _ in
        disabledRootPushes += 1
        return delivered
    }
    Config.shared.orchestratorAgentNotify = false
    let disabledRoot = rootNotify(title: "forecast", body: "sunny")
    expect("the root route names a disabled agent-notification preference",
           disabledRoot.status, 409)
    expect("the root route returns its dedicated disabled code",
           remoteErrorCode(disabledRoot), "agent_notify_disabled")
    check("the root refusal says who disabled it and where to re-enable it",
          remoteErrorMessage(disabledRoot).contains("user")
              && remoteErrorMessage(disabledRoot).contains("Settings → Remote"))
    expect("the root route does not attempt delivery while disabled", disabledRootPushes, 0)
    Config.shared.orchestratorAgentNotify = true
    Orchestrator.agentPushForTesting = { _, _, _, _, _ in delivered }
    for index in 1...30 {
        expect("a disabled refusal did not spend machine allowance \(index)",
               rootNotify(title: "after refusal \(index)", body: "body").status, 200)
    }
    let afterDisabledCrowded = rootNotify(title: "after refusal 31", body: "body")
    expect("only the thirty-first delivery after a disabled refusal is rate-limited",
           afterDisabledCrowded.status, 429)
    Orchestrator.forget()
    let anonymousRoot = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/notify", body: "{\"title\":\"x\",\"body\":\"y\"}"))
    expect("a root notification without any credential stops at the door",
           anonymousRoot.status, 401)
    let phone = RemoteAuth.addDevice(name: "a phone", caps: [.read, .send])
    let pairedRoot = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/notify",
        headers: ["Authorization": "Bearer \(phone.token)"],
        body: "{\"title\":\"x\",\"body\":\"y\"}"))
    expect("a paired device cannot substitute for the orchestrator token", pairedRoot.status, 403)
    RemoteAuth.revoke(id: phone.id)
    Orchestrator.agentPushForTesting = { title, _, _, tag, icon in
        displayedTitle = title
        displayedTag = tag
        displayedIcon = icon
        return delivered
    }
    expect("the root route accepts the exact title and body boundaries",
           rootNotify(title: String(repeating: "t", count: 80),
                      body: String(repeating: "b", count: 500)).status, 200)
    expect("the root source prefixes its notification title", displayedTitle,
           "Clawdline: " + String(repeating: "t", count: 80))
    expect("root content notifications have one stable push topic", displayedTag, "agent-root")
    check("root content notifications do not invent a project icon", displayedIcon == nil)
    let rootAudit = RemoteAuth.recentAudit(limit: 10).first {
        $0["event"] as? String == "orchestrator.notify"
            && $0["root"] as? String == "1"
            && $0["result"] as? String == "sent"
    }
    expect("a root audit names its source", rootAudit?["root"] as? String, "1")
    expect("an 81-character title is rejected",
           rootNotify(title: String(repeating: "t", count: 81), body: "body").status, 400)
    expect("a 501-character body is rejected",
           rootNotify(title: "title", body: String(repeating: "b", count: 501)).status, 400)
    expect("a blank title is rejected", rootNotify(title: "   ", body: "body").status, 400)
    expect("a blank body is rejected", rootNotify(title: "title", body: "\n").status, 400)

    Orchestrator.forget()
    Orchestrator.agentPushForTesting = { _, _, _, _, _ in delivered }
    for index in 1...30 {
        expect("the machine accepts notification \(index) in the hour",
               rootNotify(title: "message \(index)", body: "body").status, 200)
    }
    let crowded = rootNotify(title: "message 31", body: "body")
    expect("the thirty-first notification in an hour is refused", crowded.status, 429)
    expect("by the agent-notification brake", remoteErrorCode(crowded), "rate_limited")

    Orchestrator.forget()
    Orchestrator.agentPushForTesting = { _, _, _, _, _ in delivered }
    let start = Date(timeIntervalSince1970: 1_800_000_000)
    for index in 0..<30 {
        if case .refused = Orchestrator.agentNotify(title: "window \(index)", body: "body",
                                                     now: start) {
            check("the sliding-window setup accepts thirty messages", false)
        }
    }
    if case .ok = Orchestrator.agentNotify(title: "after one hour", body: "body",
                                           now: start.addingTimeInterval(3600)) {
        check("the sliding hour expires records at its boundary", true)
    } else {
        check("the sliding hour expires records at its boundary", false)
    }

    Orchestrator.forget()
    let dispatchBudget = max(10, Config.shared.orchestratorMaxDescendants)
    for _ in 0..<dispatchBudget { _ = Orchestrator.takeDispatchRate() }
    Orchestrator.agentPushForTesting = { _, _, _, _, _ in delivered }
    if case .ok = Orchestrator.agentNotify(title: "separate", body: "body") {
        check("dispatch saturation does not consume notification capacity", true)
    } else {
        check("dispatch saturation does not consume notification capacity", false)
    }

    Orchestrator.forget()
    Orchestrator.agentPushForTesting = { _, _, _, _, _ in delivered }
    for index in 0..<30 {
        _ = Orchestrator.agentNotify(title: "notify \(index)", body: "body")
    }
    check("notification saturation does not consume dispatch capacity",
          Orchestrator.takeDispatchRate() != nil)

    Orchestrator.forget()
    install([row(taskID)])
    Orchestrator.agentPushForTesting = { _, _, _, _, _ in delivered }
    expect("a task-route delivery enters the shared hourly window", taskNotify(taskID).status, 200)
    for index in 1..<30 {
        _ = Orchestrator.agentNotify(title: "root after task \(index)", body: "body")
    }
    if case .refused(let status, let code, _, _) =
        Orchestrator.agentNotify(title: "shared thirty-one", body: "body") {
        check("the task route consumes one hourly notification slot",
              status == 429 && code == "rate_limited")
    } else {
        check("the task route consumes one hourly notification slot", false)
    }
}
}
