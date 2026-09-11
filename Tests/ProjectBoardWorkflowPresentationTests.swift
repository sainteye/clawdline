import AppKit
import Foundation

func runProjectBoardWorkflowPresentationTests() {
group("managed workflow metadata is presentation-only") {
    workflowBeginAssistanceDecoderProof()
    workflowBeginHintProducerProof()
    let metadata = #"{"authority":"clawdline_metadata_not_user_authorization","board_epoch":7,"content_reference":"terminal-request:0123456789abcdef01234567","conversation_id":"11111111-1111-4111-8111-111111111111","coverage":"managed_ingress","helper":"clawdline-board-workflow <conversation-id> <stable-idempotency-key>","input_kind":"text_and_image","mode_gap":null,"process_generation":"process-7","project_id":"project-0123456789abcdef01234567","provider":"codex","required_first_action":"begin","run_id":"run-0123456789abcdef0123456789abcdef","terminal_id":"%458","version":1}"#
    let wire = #"<clawdline-workflow version="1" authority="metadata-not-user">"#
        + "\n" + metadata + "\n</clawdline-workflow>"
    let image = "[Image #1]"
    func pathWire(_ value: String) -> String {
        var object = (try! JSONSerialization.jsonObject(with: Data(metadata.utf8))) as! [String: Any]
        object["helper_path"] = value
        let raw = String(data: try! JSONSerialization.data(withJSONObject: object), encoding: .utf8)!
        return wire.replacingOccurrences(of: metadata, with: raw)
    }
    check("native renderer folds the relocated installed helper path",
          Transcript.boardWorkflowPresentation(in: pathWire(
            "/Applications/A relocated.app/Contents/Resources/clawdline-board-workflow")) != nil)
    check("native renderer rejects relative malformed and oversized helper paths",
          ["clawdline-board-workflow", "/tmp/other", "/tmp/../clawdline-board-workflow",
           "/tmp/\nclawdline-board-workflow", "/" + String(repeating: "a", count: 4096)
            + "/clawdline-board-workflow"].allSatisfy {
                Transcript.boardWorkflowPresentation(in: pathWire($0)) == nil
            })
    let source = "保留 <script>alert(1)</script> 與原句\n\n" + wire + "\n" + image
    let found = Transcript.boardWorkflowPresentation(in: source)
    expect("the native reader preserves text around exact metadata",
           found?.text, "保留 <script>alert(1)</script> 與原句\n" + image)
    expect("the native reader retains raw agent metadata for disclosure", found?.raw, metadata)
    check("quoted and fenced lookalikes remain ordinary user text",
          Transcript.boardWorkflowPresentation(in: "> " + wire) == nil
            && Transcript.boardWorkflowPresentation(in: "```text\n" + wire + "\n```") == nil
            && Transcript.boardWorkflowPresentation(in: "~~~~\n``` still fenced\n" + wire
                + "\n~~~~") == nil)
    check("malformed and duplicated envelopes fail visible",
          Transcript.boardWorkflowPresentation(in: wire.replacingOccurrences(of: metadata,
              with: "{not-json}")) == nil
            && Transcript.boardWorkflowPresentation(in: wire + "\n" + wire) == nil
            && Transcript.boardWorkflowPresentation(in: wire.replacingOccurrences(
                of: #""board_epoch":7"#, with: #""board_epoch":true"#)) == nil
            && Transcript.boardWorkflowPresentation(in: wire.replacingOccurrences(
                of: #""board_epoch":7"#, with: #""board_epoch":1e100"#)) == nil)

    let mono = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    let collapsed = Transcript.render([
        Transcript.Entry(kind: .user, text: source, tool: nil, time: nil)
    ], size: 11.5, mono: mono).string
    check("native chat hides raw JSON behind a Board record fold",
          collapsed.contains("BOARD RECORD") && collapsed.contains("保留 <script>alert(1)</script>")
            && collapsed.contains(image) && !collapsed.contains(metadata))
    guard let key = found?.foldKey else { check("workflow fold key exists", false); return }
    let expanded = Transcript.render([
        Transcript.Entry(kind: .user, text: source, tool: nil, time: nil)
    ], size: 11.5, mono: mono, expanded: [key]).string
    check("native disclosure can recover the exact raw metadata", expanded.contains(metadata))
}
}

/// Optional begin assistance in a v1 envelope: legacy and new shapes fold, a template folds by
/// its structural v1 contract rather than by the producer's current text, and unknown keys and
/// malformed values stay visible prose.
private func workflowBeginAssistanceDecoderProof() {
    let legacy = #"{"authority":"clawdline_metadata_not_user_authorization","board_epoch":7,"content_reference":"terminal-request:0123456789abcdef01234567","conversation_id":"11111111-1111-4111-8111-111111111111","coverage":"managed_ingress","helper":"clawdline-board-workflow <conversation-id> <stable-idempotency-key>","input_kind":"text_and_image","mode_gap":null,"process_generation":"process-7","project_id":"project-0123456789abcdef01234567","provider":"codex","required_first_action":"begin","run_id":"run-0123456789abcdef0123456789abcdef","terminal_id":"%458","version":1}"#
    let base = (try? JSONSerialization.jsonObject(with: Data(legacy.utf8))) as? [String: Any] ?? [:]
    let run = base["run_id"] as? String ?? ""
    let template = ProjectBoardWorkflow.beginTemplate(runID: run)
    let item = "4f1c2d3e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
    func folds(_ changes: [String: Any]) -> Bool {
        var object = base
        for (key, value) in changes { object[key] = value }
        let raw = String(data: try! JSONSerialization.data(
            withJSONObject: object, options: [.sortedKeys]), encoding: .utf8) ?? ""
        return Transcript.boardWorkflowPresentation(
            in: #"<clawdline-workflow version="1" authority="metadata-not-user">"#
                + "\n" + raw + "\n</clawdline-workflow>") != nil
    }
    check("a legacy envelope without begin assistance still folds", folds([:]))
    check("an envelope carrying only the begin template folds", folds(["begin_template": template]))
    check("an envelope carrying the template and a prior item folds",
          folds(["begin_template": template, "previous_item": item]))

    // A later producer may reword placeholders, add a field and drop one; its envelopes must keep
    // folding after that producer is gone.
    var missingField: [String: Any] = template
    missingField.removeValue(forKey: "title")
    var future = missingField
    future["classification"] = "<existing_item|new_work|question|clarification>"
    future["item_id"] = "<exact item uuid>"
    future["type"] = "<task|feature|bug|refactor|coordination|epic>"
    future["handoff_id"] = "<handoff>"
    check("a future-shaped template with other placeholders, an extra key and no title folds",
          folds(["begin_template": future, "previous_item": item]))
    var extraField: [String: Any] = template
    extraField["summary"] = "added"
    var alreadyChosen: [String: Any] = template
    alreadyChosen["classification"] = "existing_item"
    // Valid under the structural contract; each was malformed while the decoder demanded the
    // producer's exact text.
    let nowValid: [(String, [String: Any])] = [
        ("missing an optional field", missingField), ("with an extra field", extraField),
        ("whose choice was already made", alreadyChosen),
    ]
    for (name, shape) in nowValid {
        check("a template \(name) folds", folds(["begin_template": shape]))
    }
    var widest: [String: Any] = ["operation": "begin", "run_id": run]
    for length in 1...13 { widest["k_" + String(repeating: "a", count: length)] = "<\(length)>" }
    widest[String(repeating: "z", count: 64)] = String(repeating: "界", count: 85) + "a"
    check("a template at the bounds folds: 16 keys, a 64-byte key, a 256-byte value",
          widest.count == 16 && folds(["begin_template": widest]))
    check("a template of only operation and run_id folds",
          folds(["begin_template": ["operation": "begin", "run_id": run]]))
    check("an unknown key beside begin assistance fails visible",
          !folds(["begin_template": template, "previous_item": item, "previous_item_title": "Fix it"]))

    var otherRun = template
    otherRun["run_id"] = "run-ffffffffffffffffffffffffffffffff"
    var noOperation = template
    noOperation.removeValue(forKey: "operation")
    var otherOperation = template
    otherOperation["operation"] = "progress"
    var seventeen = widest
    seventeen["one_more"] = "<17>"
    var longValue = template
    longValue["title"] = String(repeating: "界", count: 85) + "ab"
    var numericPhase: [String: Any] = template
    numericPhase["phase"] = 1
    let malformed: [(String, [String: Any])] = [
        ("an uppercase prior item", ["begin_template": template, "previous_item": item.uppercased()]),
        ("a human key as prior item", ["begin_template": template, "previous_item": "CLA-395"]),
        ("a numeric prior item", ["begin_template": template, "previous_item": 7]),
        ("a null prior item", ["begin_template": template, "previous_item": NSNull()]),
        ("an empty prior item", ["begin_template": template, "previous_item": ""]),
        ("a prior item without the template", ["previous_item": item]),
        ("a template for another run", ["begin_template": otherRun]),
        ("a template without operation", ["begin_template": noOperation]),
        ("a template for another operation", ["begin_template": otherOperation]),
        ("a template with 17 keys", ["begin_template": seventeen]),
        ("a template value of 257 UTF-8 bytes", ["begin_template": longValue]),
        ("a template with a non-string value", ["begin_template": numericPhase]),
        ("a template sent as a string", ["begin_template": "{\"operation\":\"begin\"}"]),
        ("a template as an array", ["begin_template": [template]]),
        ("a null template", ["begin_template": NSNull()]),
    ]
    for (name, changes) in malformed {
        check("\(name) fails visible", !folds(changes))
    }
    for spelling in ["itemId", "item-id", "item_id2", "", String(repeating: "z", count: 65), "title\n"] {
        var misspelled: [String: Any] = template
        misspelled[spelling] = "<x>"
        check("a template key spelled \(spelling.debugDescription) fails visible",
              !folds(["begin_template": misspelled]))
    }
}

/// The producer offers only a settled item from the same conversation, freezes it at admission so
/// a retry replays byte-identical text, and emits the exact line the web decoder fixture holds.
private func workflowBeginHintProducerProof() {
    let root = URL(fileURLWithPath: NSTemporaryDirectory(), isDirectory: true)
        .appendingPathComponent("clawdline-workflow-begin-hint-\(UUID().uuidString)", isDirectory: true)
    try? FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }
    let journal = root.appendingPathComponent("workflow.json")
    var boardEnabled = true
    let refusedItem = "0badbeef-0000-4000-8000-000000000000"
    var createdCount = 0
    func makeWorkflow(_ url: URL = journal) -> ProjectBoardWorkflow {
        ProjectBoardWorkflow(
            url: url, boardHeader: { (boardEnabled, 1, true) },
            ensureProject: { _, _ in (true, nil) },
            boardCommand: { body, _ in
                if body["operation"] as? String == "create" {
                    createdCount += 1
                    return ProjectBoardStore.Reply(status: 200, body: [
                        "itemId": String(format: "c0ffee%02d-0000-4000-8000-000000000000", createdCount)])
                }
                if body["operation"] as? String == "link", body["itemId"] as? String == refusedItem {
                    return ProjectBoardStore.Reply(status: 404, body: ["error": ["code": "item_not_found"]])
                }
                return ProjectBoardStore.Reply(status: 200, body: [:])
            }, autoStart: false, bundleResources: nil)
    }
    func hintIdentity(conversation: String = "11111111-1111-4111-8111-111111111111",
                      provider: String = "codex",
                      project: String = "project-0123456789abcdef01234567",
                      terminal: String = "%hint-1",
                      generation: String = "pid-100-start-200") -> ProjectBoardWorkflow.Identity {
        ProjectBoardWorkflow.Identity(
            terminalID: terminal, provider: provider, conversationID: conversation,
            projectID: project, projectPath: "/Users/me/code/\(project)",
            processGeneration: generation)
    }
    func admit(_ workflow: ProjectBoardWorkflow, _ identity: ProjectBoardWorkflow.Identity,
               _ request: String) -> (runID: String, wire: String, metadata: [String: Any])? {
        guard case .managed(let prepared) = workflow.prepareIngress(
            requestID: request, fingerprint: request + "-body", text: "turn " + request,
            imageCount: 0, identity: identity) else { return nil }
        let line = prepared.wireText.components(separatedBy: "\n").first { $0.hasPrefix("{") } ?? "{}"
        let metadata = (try? JSONSerialization.jsonObject(with: Data(line.utf8))) as? [String: Any] ?? [:]
        return (prepared.runID, prepared.wireText, metadata)
    }
    func begin(_ workflow: ProjectBoardWorkflow, _ identity: ProjectBoardWorkflow.Identity,
               _ runID: String, _ fields: [String: Any]) -> Int {
        _ = workflow.markDelivery(runID: runID, identity: identity, delivered: true)
        var body = fields
        body["operation"] = "begin"
        body["run_id"] = runID
        return workflow.record(body, requestID: "begin-\(runID)", fingerprint: "begin-\(runID)",
                               identity: identity).status
    }
    func hint(_ admitted: (runID: String, wire: String, metadata: [String: Any])?) -> String? {
        admitted?.metadata["previous_item"] as? String
    }

    let me = hintIdentity()
    var workflow = makeWorkflow()
    workflow.syncMode()
    let firstItem = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
    guard let first = admit(workflow, me, "hint-1") else {
        check("the begin hint fixture admits a managed run", false); return
    }
    check("a conversation with no settled item carries no prior-item hint",
          first.metadata["previous_item"] == nil)
    let template = first.metadata["begin_template"] as? [String: Any] ?? [:]
    expect("the begin template names this run", template["run_id"] as? String, first.runID)
    check("the producer's own hinted envelope folds in the native reader",
          Transcript.boardWorkflowPresentation(in: first.wire) != nil)
    _ = workflow.markDelivery(runID: first.runID, identity: me, delivered: true)
    expect("the template is refused as posted, because its classification is still the choice list",
           workflow.record(template, requestID: "template-as-is", fingerprint: "template-as-is",
                           identity: me).code, "workflow_begin_invalid")
    var filled = template
    filled["classification"] = "existing_item"
    filled["item_id"] = firstItem
    filled.removeValue(forKey: "title")
    filled.removeValue(forKey: "type")
    expect("filling the template's existing_item choice makes an ordinary begin",
           workflow.record(filled, requestID: "template-filled", fingerprint: "template-filled",
                           identity: me).status, 202)

    let others = [
        hintIdentity(conversation: "22222222-2222-4222-8222-222222222222", terminal: "%hint-2"),
        hintIdentity(provider: "claude", terminal: "%hint-3"),
        hintIdentity(project: "project-fedcba9876543210fedcba98", terminal: "%hint-4"),
    ]
    for (index, other) in others.enumerated() {
        guard let run = admit(workflow, other, "other-\(index)") else {
            check("another scope \(index) admits a managed run", false); continue
        }
        expect("another scope \(index) binds its own item", begin(workflow, other, run.runID, [
            "classification": "existing_item", "phase": "output",
            "item_id": "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbb\(index)"]), 202)
    }
    let resumed = hintIdentity(terminal: "%hint-resumed", generation: "pid-900-start-901")
    let second = admit(workflow, resumed, "hint-2")
    expect("only this conversation's settled item is offered, even to a resumed process",
           hint(second), firstItem)

    guard let secondRun = second else { check("the resumed conversation admits a run", false); return }
    let program = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
    let binding: [String: Any] = [
        "program_item_id": program, "program_key": "CLA-1", "plan_id": "plan",
        "plan_version": 1, "graph_id": "graph", "node_id": "node",
    ]
    expect("a Program binding begin is admitted and stays pending", begin(workflow, resumed, secondRun.runID, [
        "classification": "existing_item", "item_id": program, "phase": "output",
        "program_binding": binding]), 202)
    let third = admit(workflow, me, "hint-3")
    expect("a pending Program binding is not a settled item", hint(third), firstItem)
    guard let thirdRun = third else { check("hint-3 admits a run", false); return }
    expect("new work is admitted before its create settles", begin(workflow, me, thirdRun.runID, [
        "classification": "new_work", "title": "Fresh work", "type": "task", "phase": "planning"]), 202)
    let fourth = admit(workflow, me, "hint-4")
    expect("an unsettled new_work create is not a settled item", hint(fourth), firstItem)
    workflow.drainForTesting()
    let createdItem = "c0ffee01-0000-4000-8000-000000000000"
    let fifth = admit(workflow, me, "hint-5")
    expect("a settled new_work item becomes the prior item", hint(fifth), createdItem)

    guard let fourthRun = fourth, let fifthRun = fifth else {
        check("hint-4 and hint-5 admit runs", false); return
    }
    _ = begin(workflow, me, fourthRun.runID, [
        "classification": "existing_item", "item_id": refusedItem, "phase": "output"])
    _ = begin(workflow, me, fifthRun.runID, [
        "classification": "existing_item", "item_id": "CLA-395", "phase": "output"])
    workflow.drainForTesting()
    guard let sixth = admit(workflow, me, "hint-6") else { check("hint-6 admits a run", false); return }
    expect("a refused link and a human key are unresolved, so the last settled item stays",
           hint(sixth), createdItem)
    let hintedLine = sixth.wire.components(separatedBy: "\n").first { $0.hasPrefix("{") } ?? ""
    var legacyShape = sixth.metadata
    legacyShape.removeValue(forKey: "begin_template")
    legacyShape.removeValue(forKey: "previous_item")
    let legacyLine = String(data: try! JSONSerialization.data(
        withJSONObject: legacyShape, options: [.sortedKeys]), encoding: .utf8) ?? ""
    expect("the hint and template add a constant 337 bytes to a hinted envelope",
           hintedLine.utf8.count - legacyLine.utf8.count, 337)

    // A retry can reach the terminal again, so it must not re-derive from a journal where an
    // earlier run settled after this one was admitted.
    guard let earlier = admit(workflow, me, "hint-7"),
          let original = admit(workflow, me, "hint-8") else {
        check("hint-7 and hint-8 admit runs", false); return
    }
    let laterItem = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
    expect("the original send carried the item settled when it was admitted", hint(original), createdItem)
    expect("an earlier run can still begin after a later run was admitted",
           begin(workflow, me, earlier.runID, [
            "classification": "existing_item", "item_id": laterItem, "phase": "output"]), 202)
    expect("control: a run admitted now is offered the newly settled item",
           hint(admit(workflow, me, "hint-9")), laterItem)
    expect("an identical retry after another run settled replays byte-identical text",
           admit(workflow, me, "hint-8")?.wire, original.wire)
    workflow = makeWorkflow()
    expect("the frozen hint survives a journal reload",
           admit(workflow, me, "hint-8")?.wire, original.wire)

    var legacyState = try! JSONSerialization.jsonObject(
        with: Data(contentsOf: journal)) as? [String: Any] ?? [:]
    legacyState["runs"] = (legacyState["runs"] as? [[String: Any]] ?? []).map {
        (run: [String: Any]) -> [String: Any] in
        var legacyRun = run
        legacyRun.removeValue(forKey: "beginHint")
        return legacyRun
    }
    try! JSONSerialization.data(withJSONObject: legacyState, options: [.sortedKeys]).write(to: journal)
    workflow = makeWorkflow()
    let legacyReplay = admit(workflow, me, "hint-8")
    check("a run admitted before the hint existed replays its legacy envelope",
          legacyReplay?.runID == original.runID
            && legacyReplay?.metadata["begin_template"] == nil
            && legacyReplay?.metadata["previous_item"] == nil)

    boardEnabled = false
    workflow.syncMode()
    boardEnabled = true
    workflow.syncMode()
    let afterBoundary = admit(workflow, me, "hint-10")
    check("an OFF→ON boundary does not offer an earlier epoch's item",
          afterBoundary?.metadata["begin_template"] != nil
            && afterBoundary?.metadata["previous_item"] == nil)

    // One exact producer line, shared with `Tests/web-board.mjs`, holds the web decoder to the
    // bytes this producer really emits.
    let fixtureWorkflow = makeWorkflow(root.appendingPathComponent("fixture.json"))
    fixtureWorkflow.syncMode()
    let fixtureIdentity = ProjectBoardWorkflow.Identity(
        terminalID: "%workflow-fixture", provider: "codex",
        conversationID: "11111111-1111-4111-8111-111111111111",
        projectID: "project-0123456789abcdef01234567", projectPath: "/Users/me/code/fixture",
        processGeneration: "pid-100-start-200")
    if let seed = admit(fixtureWorkflow, fixtureIdentity, "fixture-send-1") {
        expect("the fixture seed binds its item", begin(fixtureWorkflow, fixtureIdentity, seed.runID, [
            "classification": "existing_item", "item_id": "4f1c2d3e-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
            "phase": "output"]), 202)
    }
    let produced = admit(fixtureWorkflow, fixtureIdentity, "fixture-send-2")?.wire
        .components(separatedBy: "\n").first { $0.hasPrefix("{") }
    let fixture = (try? String(contentsOfFile: "Tests/board-workflow-metadata-v1.json",
                               encoding: .utf8))?.trimmingCharacters(in: .whitespacesAndNewlines)
    expect("the shared web fixture is this producer's exact metadata line", produced, fixture)
}
