import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - Handing work to another session

/// A lowercase UUID, which is the only shape a task id is ever allowed to have.
let taskID = "0f8fad5b-d9cb-469f-a165-70867728950e"







/// A tab this app opens is a new session, and nothing in its environment may say otherwise.
///
/// iTerm2 launched once from a shell inside a Claude Code session keeps that session's identity
/// variables for as long as it runs and hands them to every tab afterwards. Measured on
/// 2026-08-28: `ps -Ewww` on the iTerm2 process held `CLAUDE_PID`, `CLAUDE_CODE_SESSION_ID` and
/// `CLAUDE_CODE_CHILD_SESSION=1` from a session in another window, and the children dispatched
/// into it wrote neither a `~/.claude/sessions/<pid>.json` nor a transcript, because the CLI had
/// been told it was already somebody's nested session. Nothing downstream could then prove a
/// briefing had arrived, and the spawn-failure path that follows deletes an uncommitted checkout.
///
/// So this is not a cosmetic test about a command line. It is the one place the list itself is
/// spelled out; every other assertion in this file composes the prefix from the property, and a
/// list that quietly emptied would leave all of those passing.

func runOrchestratorDispatchTests() {
group("a task.json is read before a terminal is opened for it") {
    // Everything here is the file a *root* session wrote, which is to say a file this app did not
    // write and cannot trust. `draft` is the whole of the reading, and it is pure — the directory
    // check is handed in — so the refusals can be exercised without a filesystem to arrange.
    func file(_ overrides: [String: Any] = [:]) -> [String: Any] {
        var obj: [String: Any] = ["clawdline_protocol": 1,
                                  "task_id": taskID,
                                  "kind": "image",
                                  "assistant": "codex",
                                  "project_dir": "/Users/me/code/thing",
                                  "title": "draw the project",
                                  "instructions": "Draw it, in ink.",
                                  "timeout_minutes": 45,
                                  "root": ["session_id": "abc", "label": "notebook"]]
        for (key, value) in overrides { obj[key] = value }
        return obj
    }
    func read(_ obj: [String: Any], expecting: String = taskID) -> Orchestrator.DraftOutcome {
        Orchestrator.draft(from: obj, expecting: expecting, isDirectory: { _ in true })
    }
    func made(_ obj: [String: Any]) -> Orchestrator.Draft? {
        if case .ok(let draft) = read(obj) { return draft }
        return nil
    }
    func refused(_ obj: [String: Any], expecting: String = taskID) -> Bool {
        if case .bad = read(obj, expecting: expecting) { return true }
        return false
    }
    func refusal(_ obj: [String: Any]) -> String? {
        if case .bad(let why) = read(obj) { return why }
        return nil
    }

    expect("a whole task is taken as written", made(file())?.assistant, .codex)
    check("a task with no model named runs on whatever that assistant defaults to",
          made(file())?.model == nil)
    expect("a task may name machine-global operations to serialize",
           made(file(["serialize": ["build", "db.migration"]]))?.serialize,
           ["build", "db.migration"])
    expect("an absent serialize field has no scheduling effect",
           made(file())?.serialize, [])
    expect("a task may reserve relative write paths from dispatch",
           made(file(["claims": ["Sources", "docs/api.md"]]))?.claims,
           ["Sources", "docs/api.md"])
    check("an empty claims array is a positive read-only declaration",
          made(file(["claims": []]))?.claims == []
              && made(file(["claims": []]))?.claimsDeclared == true)
    check("an absent claims field preserves an unknown write set",
          made(file())?.claims == [] && made(file())?.claimsDeclared == false)
    expect("an absent isolation field means the shared working tree",
           made(file())?.isolation, Orchestrator.Isolation.none)
    expect("a task may ask for a worktree",
           made(file(["isolation": "worktree"]))?.isolation, .worktree)
    expect("an explicit none is the default written out",
           made(file(["isolation": "none"]))?.isolation, Orchestrator.Isolation.none)
    check("an isolation nobody defined is refused rather than rounded down",
          refused(file(["isolation": "container"])))
    check("isolation is a string enum, not a truthy switch",
          refused(file(["isolation": true])))
    check("a base without worktree isolation is refused rather than ignored",
          refused(file(["isolation_base": "main"])))
    expect("a worktree may name a relative commit expression",
           made(file(["isolation": "worktree", "isolation_base": "HEAD~3"]))?.isolationBase,
           "HEAD~3")
    check("a base cannot turn into another git flag",
          refused(file(["isolation": "worktree", "isolation_base": "--force"])))
    check("a revision range is not one commit-shaped base",
          refused(file(["isolation": "worktree", "isolation_base": "a..b"])))
    check("base validation rejects whitespace, controls and over-long values together",
          refused(file(["isolation": "worktree", "isolation_base": "feature branch"]))
              && refused(file(["isolation": "worktree", "isolation_base": "main\nnext"]))
              && refused(file(["isolation": "worktree",
                               "isolation_base": String(repeating: "a", count: 201)])))
    expect("a full ref is still one valid revision name",
           made(file(["isolation": "worktree",
                      "isolation_base": "refs/heads/feature/x"]))?.isolationBase,
           "refs/heads/feature/x")
    expect("one that names a model carries it", made(file(["model": "haiku"]))?.model, "haiku")
    expect("a Codex task may ask for high reasoning",
           made(file(["reasoning_effort": "high"]))?.reasoningEffort, .high)
    expect("a Codex task may ask for extra-high reasoning",
           made(file(["reasoning_effort": "xhigh"]))?.reasoningEffort, .xhigh)
    check("omitting reasoning effort inherits Codex and user defaults",
          made(file())?.reasoningEffort == nil)
    for invalid in ["", "low", "medium", "max", "ultra", "HIGH"] {
        let why = refusal(file(["reasoning_effort": invalid])) ?? ""
        check("reasoning effort \(invalid.isEmpty ? "empty" : invalid) is refused by name",
              why.contains("reasoning_effort"))
    }
    check("a non-string reasoning effort is refused by name",
          refusal(file(["reasoning_effort": 1]))?.contains("reasoning_effort") == true)
    check("reasoning effort is Codex-only and the refusal names the field",
          refusal(file(["assistant": "claude", "reasoning_effort": "high"]))?
              .contains("reasoning_effort") == true)
    expect("and one that names how far it may go carries that",
           made(file(["permission_mode": "edits"]))?.permission, .edits)
    check("`auto` is not one of them — on Haiku that flag quietly means `manual`",
          refused(file(["permission_mode": "auto"])))
    check("a permission nobody defined is refused rather than rounded down",
          refused(file(["permission_mode": "whatever"])))
    check("and saying nothing is not the same as saying ask — the ceiling decides that later",
          made(file())?.permission == nil)
    check("and one that names something that is not a model is refused, not quietly ignored",
          refused(file(["model": "haiku; rm -rf /"])))
    expect("the graph a task is one node of comes through whole",
           made(file(["plan": "root → two leaves → this one"]))?.plan,
           "root → two leaves → this one")
    check("a plan of nothing but whitespace is no plan",
          made(file(["plan": "   \n  "]))?.plan == nil)
    check("and one past four KiB is refused rather than cut",
          refused(file(["plan": String(repeating: "p", count: 4097)])))

    func graph(_ overrides: [String: Any] = [:]) -> [String: Any] {
        var value: [String: Any] = [
            "id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
            "destination": "A reviewed change is landed on main.",
            "current_node": "build",
            "nodes": [
                ["id": "build", "title": "Build the slice", "kind": "delivery",
                 "depends_on": [],
                 "acceptance": ["The focused contract passes."]],
                ["id": "review", "title": "Review the slice", "kind": "review",
                 "depends_on": ["build"],
                 "acceptance": ["All three review axes have a verdict."]],
            ],
            "unknowns": ["Whether a migration is necessary."],
            "out_of_scope": ["Changing the terminal transport."],
        ]
        for (key, valueOverride) in overrides { value[key] = valueOverride }
        return value
    }
    let typed = made(file(["graph": graph()]))?.graph
    check("a typed graph carries destination, fog and scope without replacing the legacy plan",
          typed?.destination == "A reviewed change is landed on main."
              && typed?.currentNode == "build"
              && typed?.unknowns == ["Whether a migration is necessary."]
              && typed?.outOfScope == ["Changing the terminal transport."])
    expect("a typed graph keeps decision and delivery order as explicit dependencies",
           typed?.nodes.last?.dependsOn ?? [], ["build"])
    check("the current graph node must name a declared node",
          refused(file(["graph": graph(["current_node": "missing"])])))
    let cyclicNodes: [[String: Any]] = [
        ["id": "build", "title": "Build", "kind": "delivery",
         "depends_on": ["review"], "acceptance": ["Built"]],
        ["id": "review", "title": "Review", "kind": "review",
         "depends_on": ["build"], "acceptance": ["Reviewed"]],
    ]
    check("a graph cycle is refused before a terminal opens",
          refused(file(["graph": graph(["nodes": cyclicNodes])])))
    check("a graph node kind is a closed vocabulary",
          refused(file(["graph": graph(["nodes": [[
              "id": "build", "title": "Build", "kind": "guess",
              "depends_on": [], "acceptance": ["Built"],
          ]]])])))
    expect("with its own timeout", made(file())?.timeoutMinutes, 45)
    expect("its kind", made(file())?.kind, "image")
    expect("its title", made(file())?.title, "draw the project")
    expect("and who asked for it", made(file())?.rootSessionId, "abc")
    expect("and which assistant the root session is running",
           made(file(["root": ["session_id": "abc", "assistant": "codex"]]))?.rootAssistant,
           .codex)
    check("an older task with no root assistant remains a legacy Claude row",
          made(file())?.rootAssistant == nil)
    let explicitNullRoot = made(file(["root": ["session_id": "abc",
                                                  "assistant": NSNull()]]))
    check("an explicit null root assistant has the same compatibility meaning as absence",
          explicitNullRoot != nil && explicitNullRoot?.rootAssistant == nil)
    check("a root assistant outside the closed set is refused",
          refused(file(["root": ["session_id": "abc", "assistant": "emacs"]])))
    // The one identification a child can always make correctly: it has known its own task id
    // since its first message, whereas its session id is something this app works out later.
    let parent = "9f2b6a7e-5d0c-4123-8d4e-3f9a21bc8765"
    expect("and, when it is a child asking, the task it hangs under",
           made(file(["root": ["session_id": "abc", "parent_task": parent]]))?.parentTaskId, parent)
    check("a parent that is a path is no parent at all",
          made(file(["root": ["parent_task": "../../etc/passwd"]]))?.parentTaskId == nil)
    check("nor is one nobody wrote",
          made(file(["root": ["session_id": "abc"]]))?.parentTaskId == nil)
    expect("a file with no kind is a custom one", made(file(["kind": ""]))?.kind, "custom")
    expect("and one with no timeout gets the default",
           made(file(["timeout_minutes": NSNull()]))?.timeoutMinutes, 30)
    expect("a title longer than the field is cut, not refused",
           made(file(["title": String(repeating: "t", count: 400)]))?.title.count, 200)

    // The refusals, one reason at a time.
    check("a protocol nobody has written yet", refused(file(["clawdline_protocol": 2])))
    check("and a missing one", refused(file(["clawdline_protocol": NSNull()])))
    check("an assistant this app cannot start", refused(file(["assistant": "emacs"])))
    check("a task_id that is a path", refused(file(["task_id": "../../etc/passwd"]),
                                             expecting: "../../etc/passwd"))
    check("a task_id with a separator in it, at the right length",
          refused(file(["task_id": "0f8fad5b-d9cb-469f-a165-7086772895/e"]),
                  expecting: "0f8fad5b-d9cb-469f-a165-7086772895/e"))
    check("a task_id that does not match the dispatch",
          refused(file(["task_id": "11111111-2222-3333-4444-555555555555"])))
    check("instructions nobody wrote", refused(file(["instructions": ""])))
    check("instructions past 16 KiB",
          refused(file(["instructions": String(repeating: "x", count: 16_385)])))
    check("a project_dir that is not a directory",
          {
              if case .bad = Orchestrator.draft(from: file(), expecting: taskID,
                                                isDirectory: { _ in false }) { return true }
              return false
          }())
    check("a project_dir that is not a path at all", refused(file(["project_dir": "thing"])))
    check("a timeout past four hours", refused(file(["timeout_minutes": 999])))
    check("and one of zero minutes", refused(file(["timeout_minutes": 0])))
    check("serialize must be an array", refused(file(["serialize": "build"])))
    check("serialize accepts at most four tokens",
          refused(file(["serialize": ["a", "b", "c", "d", "e"]])))
    check("serialize names the index of a non-string token",
          refused(file(["serialize": ["build", 3]])))
    check("serialize rejects an empty token", refused(file(["serialize": [""]])))
    check("serialize rejects a token longer than 64 characters",
          refused(file(["serialize": [String(repeating: "a", count: 65)]])))
    check("serialize uses the same closed alphabet as model",
          refused(file(["serialize": ["Build", "release now", "$(id)"]])))
    check("serialize tokens may not begin with a dash",
          refused(file(["serialize": ["-build"]])))
    check("serialize tokens may not repeat",
          refused(file(["serialize": ["build", "build"]])))
    let everyProblem = refusal(file(["serialize": ["build", 3, "Build", "build"]])) ?? ""
    check("serialize reports every invalid item in one bad_task message",
          everyProblem.contains("serialize[1]") && everyProblem.contains("serialize[2]")
              && everyProblem.contains("serialize[3]"))
    check("claims must be an array", refused(file(["claims": "Sources"])))
    check("claims range errors describe the accepted zero-through-thirty-two range",
          refusal(file(["claims": "Sources"]))?.contains("0–32") == true
              && refusal(file(["claims": (0...32).map { "path-\($0)" }]))?
                  .contains("0–32") == true)
    check("claims accepts an empty declaration for read-only work",
          !refused(file(["claims": []])))
    check("claims accepts at most thirty-two paths",
          refused(file(["claims": (0...32).map { "path-\($0)" }])))
    check("claims names the index of a non-string path",
          refused(file(["claims": ["Sources", 3]])))
    check("claims rejects an empty path", refused(file(["claims": [""]])))
    check("claims rejects a path longer than 1024 characters",
          refused(file(["claims": [String(repeating: "a", count: 1_025)]])))
    check("claims rejects NUL because it is not a POSIX pathname character",
          refused(file(["claims": ["Sources\u{0}hidden"]])))
    check("claims paths are relative to project_dir",
          refused(file(["claims": ["/Users/me/code/thing/Sources"]])))
    check("claims rejects dot-dot only as a complete path component",
          refused(file(["claims": ["Sources/../docs"]]))
              && !refused(file(["claims": ["Sources/..cache/file"]])))
    check("claims rejects duplicate declarations",
          refused(file(["claims": ["Sources", "Sources"]])))
    let everyClaimProblem = refusal(file(["claims": ["ok", 3, "/absolute", "a/../b", "ok"]])) ?? ""
    check("claims reports every invalid item in one bad_task message",
          everyClaimProblem.contains("claims[1]") && everyClaimProblem.contains("claims[2]")
              && everyClaimProblem.contains("claims[3]")
              && everyClaimProblem.contains("claims[4]"))

    // Q2's dispatch-gate override — see docs/orchestrator.md's assistant_exhausted section.
    check("an absent ignore_quota leaves the exhausted gate standing", made(file())?.ignoreQuota == false)
    check("ignore_quota: true is read as the override it is",
          made(file(["ignore_quota": true]))?.ignoreQuota == true)
    check("ignore_quota: false is the same as absent",
          made(file(["ignore_quota": false]))?.ignoreQuota == false)
    check("a non-bool ignore_quota is not an error — it is simply not true",
          made(file(["ignore_quota": "yes"]))?.ignoreQuota == false)
    // Review receipts keep independent axes independent.
    let reviewPassRows: [[String: Any]] = [
        ["axis": "specification", "status": "pass", "findings": []],
        ["axis": "repository_invariants", "status": "pass", "findings": []],
        ["axis": "runtime_failure_behavior", "status": "pass", "findings": []],
    ]
    let safe = Orchestrator.review(from: ["verdict": "safe_to_land", "axes": reviewPassRows])
    check("SAFE TO LAND requires all three named axes",
          safe?.verdict == .safeToLand && safe?.axes.count == 3)

    var findingsAxes = reviewPassRows
    findingsAxes[1] = [
        "axis": "repository_invariants", "status": "findings",
        "findings": [
            ["id": "R1", "severity": "blocking",
             "summary": "The shared index can be swept into the commit.",
             "evidence": ["AGENTS.md: use named staging paths"]]
        ],
    ]
    let changes = Orchestrator.review(
        from: ["verdict": "changes_required", "axes": findingsAxes])
    check("a finding stays attached to its axis with typed severity and evidence",
          changes?.verdict == .changesRequired
              && changes?.axes[1].findings.first?.severity == .blocking
              && changes?.axes[1].findings.first?.evidence.count == 1)
    var hiddenFindingAxes = reviewPassRows
    hiddenFindingAxes[0] = [
        "axis": "specification", "status": "pass",
        "findings": [
            ["id": "S1", "severity": "minor", "summary": "Hidden",
             "evidence": ["x"]]
        ],
    ]
    check("a pass axis cannot hide findings",
          Orchestrator.review(from: ["verdict": "safe_to_land",
                                     "axes": hiddenFindingAxes]) == nil)
    check("a safe verdict cannot omit an axis",
          Orchestrator.review(from: ["verdict": "safe_to_land",
                                     "axes": Array(reviewPassRows.dropLast())]) == nil)
    check("changes_required must carry at least one concrete finding",
          Orchestrator.review(from: ["verdict": "changes_required", "axes": reviewPassRows]) == nil)

    // The graph frontier is derived from durable task and review receipts.
    Orchestrator.forget()
    let graphID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
    let reviewTaskID = "11111111-2222-3333-4444-555555555555"
    let verificationTaskID = "12121212-3434-5656-7878-909090909090"
    let landingTaskID = "22222222-3333-4444-5555-666666666666"
    let nodes = [
        Orchestrator.GraphNode(id: "build", title: "Build", kind: .delivery,
                               dependsOn: [], acceptance: ["Built"]),
        Orchestrator.GraphNode(id: "review", title: "Review", kind: .review,
                               dependsOn: ["build"], acceptance: ["Reviewed"]),
        Orchestrator.GraphNode(id: "verify", title: "Verify", kind: .verification,
                               dependsOn: ["review"], acceptance: ["Focused proof passes"]),
        Orchestrator.GraphNode(id: "land", title: "Land", kind: .landing,
                               dependsOn: ["verify"], acceptance: ["Landed"]),
    ]
    func graph(_ current: String) -> Orchestrator.PlanningGraph {
        Orchestrator.PlanningGraph(id: graphID, destination: "Landed on main",
                                   currentNode: current, nodes: nodes,
                                   unknowns: [], outOfScope: [])
    }
    let build = Orchestrator.Task(id: taskID, state: .success, kind: "implementation",
                                 title: "Build", assistant: .codex, projectDir: "/repo",
                                 timeoutMinutes: 30, created: Date(timeIntervalSince1970: 1),
                                 graph: graph("build"),
                                 secretHash: String(repeating: "0", count: 64))
    Orchestrator.holdScheduleTaskForTesting(build)
    let conflictingGraph = Orchestrator.PlanningGraph(
        id: graphID, destination: "A different destination", currentNode: "review",
        nodes: nodes, unknowns: [], outOfScope: [])
    let conflictingAdmission = Orchestrator.graphAdmissionRefusal(
        conflictingGraph, taskID: reviewTaskID)
    var conflictingCode = ""
    if case .refused(_, let code, _, _) = conflictingAdmission { conflictingCode = code }
    var completedCode = ""
    if case .refused(_, let code, _, _) = Orchestrator.graphAdmissionRefusal(
        graph("build"), taskID: "33333333-4444-5555-6666-777777777777") {
        completedCode = code
    }
    let reserved = Orchestrator.graphAdmissionRefusal(
        graph("review"), taskID: reviewTaskID, reserve: true) == nil
    var reservedCode = ""
    if case .refused(_, let code, _, _) = Orchestrator.graphAdmissionRefusal(
        graph("review"), taskID: "44444444-5555-6666-7777-888888888888", reserve: true) {
        reservedCode = code
    }
    Orchestrator.releaseGraphAdmission(Orchestrator.graphAdmissionKey(graph("review")))
    var plannedState = ""
    if case .refused(_, _, _, let extra) = Orchestrator.graphAdmissionRefusal(
        graph("land"), taskID: landingTaskID) {
        plannedState = (extra["blocking_nodes"] as? [[String: Any]])?.first?["state"]
            as? String ?? ""
    }
    check("a completed dependency makes only its direct successor dispatchable",
          Orchestrator.graphAdmissionRefusal(graph("review"), taskID: reviewTaskID) == nil
              && conflictingCode == "graph_definition_conflict"
              && completedCode == "graph_node_complete"
              && reserved && reservedCode == "graph_node_active"
              && plannedState == "blocked")
    let controlSheet = Orchestrator.graphRecords().first
    let projected = controlSheet?["nodes"] as? [[String: Any]]
    let graphResponse = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/graphs",
        headers: ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]))
    let graphBody = (try? JSONSerialization.jsonObject(with: graphResponse.body)) as? [String: Any]
    let publicGraphs = graphBody?["graphs"] as? [[String: Any]]
    check("the control sheet exposes a computed frontier, not a caller-supplied ready flag",
          controlSheet?["frontier"] as? [String] == ["review"]
              && projected?.first(where: { $0["id"] as? String == "build" })?["state"] as? String
                    == "done"
              && projected?.first(where: { $0["id"] as? String == "review" })?["state"] as? String
                    == "ready"
              && graphResponse.status == 200
              && publicGraphs?.first?["frontier"] as? [String] == ["review"])

    var reviewTask = build
    reviewTask = Orchestrator.Task(id: reviewTaskID, state: .success, kind: "review",
                                   title: "Review", assistant: .claude, projectDir: "/repo",
                                   timeoutMinutes: 30, created: Date(timeIntervalSince1970: 2),
                                   graph: graph("review"),
                                   secretHash: String(repeating: "1", count: 64))
    Orchestrator.holdScheduleTaskForTesting(reviewTask)
    if case .refused(let status, let code, _, let extra) = Orchestrator.graphAdmissionRefusal(
        graph("verify"), taskID: verificationTaskID) {
        check("a successful review task without a typed review receipt fails closed",
              status == 409 && code == "graph_dependency_failed"
                  && (extra["blocking_nodes"] as? [[String: Any]])?.first?["state"] as? String
                        == "failed")
    } else {
        check("a successful review task without a typed review receipt fails closed", false)
    }
    let passAxes = Orchestrator.ReviewAxisName.allCases.map {
        Orchestrator.ReviewAxis(axis: $0, status: .pass, findings: [])
    }
    reviewTask.review = Orchestrator.ReviewReceipt(verdict: .safeToLand, axes: passAxes)
    Orchestrator.holdScheduleTaskForTesting(reviewTask)
    var verificationTask = Orchestrator.Task(
        id: verificationTaskID, state: .success, kind: "verification", title: "Verify",
        assistant: .codex, projectDir: "/repo", timeoutMinutes: 30,
        created: Date(timeIntervalSince1970: 3), graph: graph("verify"),
        secretHash: String(repeating: "2", count: 64))
    let reviewAdvances = Orchestrator.graphAdmissionRefusal(
        graph("verify"), taskID: verificationTaskID) == nil
    Orchestrator.holdScheduleTaskForTesting(verificationTask)
    var missingProofCode = ""
    if case .refused(_, let code, _, _) = Orchestrator.graphAdmissionRefusal(
        graph("land"), taskID: landingTaskID) { missingProofCode = code }
    verificationTask.verification = Orchestrator.Verification(
        runs: 1, seconds: 2, last: .pass, scope: "focused planning graph proof")
    Orchestrator.holdScheduleTaskForTesting(verificationTask)
    let restoredReview = Orchestrator.task(from: Orchestrator.stored(reviewTask))
    var damagedReview = Orchestrator.stored(reviewTask)
    var damagedGraph = damagedReview["graph"] as? [String: Any] ?? [:]
    damagedGraph["future_field"] = true
    damagedReview["graph"] = damagedGraph
    let restoredWithoutGraph = Orchestrator.task(from: damagedReview)
    check("review and verification receipts advance their exact successor onto the frontier",
          Orchestrator.graphAdmissionRefusal(graph("land"), taskID: landingTaskID) == nil
              && reviewAdvances && missingProofCode == "graph_dependency_failed"
              && restoredReview?.graph == graph("review")
              && restoredReview?.review?.verdict == .safeToLand
              && restoredReview?.review?.axes.count == 3
              && restoredWithoutGraph?.id == reviewTaskID
              && restoredWithoutGraph?.graph == nil
              && restoredWithoutGraph?.review?.verdict == .safeToLand)
    Orchestrator.forget()
}

group("the assistant-quota dispatch gate names the override and the age the same way "
    + "workspace_busy already does") {
    let now = Date(timeIntervalSince1970: 1_787_745_138)
    let codexExhausted = AssistantQuota(
        assistant: .codex, installed: true, loggedIn: true, plan: "prolite",
        availability: .exhausted, source: .observed,
        observedAt: Int(now.timeIntervalSince1970) - 1_708,
        resetsAt: Int(now.timeIntervalSince1970) + 3_600 * 24 * 5,
        detail: "7d 100%; resets in 5d0h", windows: [])

    let reply = Orchestrator.assistantExhaustedReply(codexExhausted, now: now)
    guard case .refused(let status, let code, let message, let extra) = reply else {
        check("assistantExhaustedReply refuses", false); return
    }
    expect("the 409 the Q1 design settled on", status, 409)
    expect("with its own code", code, "assistant_exhausted")
    check("the message tells a stuck root exactly how to get past this",
          message.contains("ignore_quota"))
    expect("the assistant that was named", extra["assistant"] as? String, "codex")
    expect("its availability", extra["availability"] as? String, "exhausted")
    // The exact formula the B-task's ClaimsOverlap.warning(for:now:) and workspaceBusyExtra
    // already use: max(0, Int(now - observed)), an integer number of seconds.
    expect("age_seconds uses the one formula this app already agreed on",
           extra["age_seconds"] as? Int, 1_708)
    expect("retry_after is capped at an hour even though the reset is five days out",
           extra["retry_after"] as? Int, 3_600)
    let alternatives = extra["alternatives"] as? [[String: Any]]
    expect("the other assistant is offered by name — the whole value of refusing this way",
           alternatives?.map { $0["id"] as? String ?? "" }, ["claude"])
    check("it carries its own availability, not just its id",
          alternatives?.first?["availability"] is String)

    let overridden = Orchestrator.assistantOverrideWarning(codexExhausted, timeoutMinutes: 40, now: now)
    expect("an override is a warning, not a refusal — its own code says which one",
           overridden["code"] as? String, "assistant_exhausted")
    check("and it says the override actually happened",
          (overridden["message"] as? String)?.contains("ignore_quota") == true)
    check("with a five-day reset against a forty-minute timeout, it warns the window will not "
        + "reset before this task's timeout",
          (overridden["message"] as? String)?.contains("will not reset before this task's timeout")
              == true)

    var soonExhausted = codexExhausted
    soonExhausted.resetsAt = Int(now.timeIntervalSince1970) + 60
    let overriddenSoon = Orchestrator.assistantOverrideWarning(soonExhausted, timeoutMinutes: 40, now: now)
    check("but not when the task's own timeout comfortably outlasts the reset",
          (overriddenSoon["message"] as? String)?.contains("will not reset") == false)

    let codexLow = AssistantQuota(
        assistant: .codex, installed: true, loggedIn: true, plan: "prolite",
        availability: .low, source: .observed,
        observedAt: Int(now.timeIntervalSince1970) - 180,
        resetsAt: Int(now.timeIntervalSince1970) + 3_600 * 24 * 5,
        detail: "7d 92%", windows: [])
    let low = Orchestrator.assistantLowWarning(codexLow, now: now)
    expect("low warns under its own code, never assistant_exhausted's",
           low["code"] as? String, "assistant_low")
    expect("naming which assistant", low["assistant"] as? String, "codex")
    expect("with the same age formula as everywhere else", low["age_seconds"] as? Int, 180)
    check("the message says a long task might not finish, not that it will be refused",
          (low["message"] as? String)?.contains("may not finish") == true)
}

group("a model name is a name, not a fragment of a command line") {
    // The one string a dispatched task puts on a command line. `StartPoints`' header promises the
    // command is a literal; this is the exception, and it holds only because the alphabet is
    // closed. Every refusal below is a character a shell would read.
    func ok(_ raw: String) -> Bool { StartPoints.modelName(raw) == raw }
    check("a bare alias is a name", ok("haiku"))
    check("so is a dated id", ok("claude-opus-5-20260201"))
    check("and one with a dot in it", ok("gpt-5.1-codex"))
    check("and one with an underscore", ok("some_model.v2-3"))

    check("a space is not", StartPoints.modelName("haiku extra") == nil)
    check("nor a semicolon", StartPoints.modelName("haiku;id") == nil)
    check("nor a substitution", StartPoints.modelName("$(id)") == nil)
    check("nor a backtick", StartPoints.modelName("`id`") == nil)
    check("nor a quote", StartPoints.modelName("\"haiku\"") == nil)
    check("nor a newline", StartPoints.modelName("haiku\nid") == nil)
    check("nor an ampersand", StartPoints.modelName("haiku&&id") == nil)
    check("nor a second flag, which is the one that would not look wrong",
          StartPoints.modelName("--dangerously-skip-permissions") == nil)
    check("nor upper case, which no slug either CLI takes uses",
          StartPoints.modelName("Haiku") == nil)
    check("nor nothing at all",
          StartPoints.modelName("") == nil && StartPoints.modelName(nil) == nil)
    check("64 characters is a name", StartPoints.modelName(String(repeating: "a", count: 64)) != nil)
    check("65 is not", StartPoints.modelName(String(repeating: "a", count: 65)) == nil)

    // Every line this app types now opens with `env -u …` between the `&&` and the program
    // name. What that prefix is exactly is pinned in "a new tab is not handed the identity of
    // whatever launched the terminal"; here it is composed, so these stay assertions about
    // the thing they were written for.
    let claudeRuns = Assistant.claude.dropInheritedIdentity
    let codexRuns = Assistant.codex.dropInheritedIdentity
    expect("the flag is written once", Assistant.claude.command(model: "haiku"),
           claudeRuns + "claude --model haiku")
    check("and the permission flags land after it, not instead of it",
          Assistant.claude.command(model: "haiku", permission: .edits)
              .hasPrefix(claudeRuns + "claude --model haiku "))
    expect("and not written at all when nothing was named", Assistant.claude.command(model: nil),
           claudeRuns + "claude")
    expect("Codex reasoning effort follows the model and precedes reach and permission",
           Assistant.codex.command(model: "gpt-5.6-sol", reasoningEffort: .xhigh,
                                   permission: .edits, addDir: "/tmp/.clawdline"),
           codexRuns + "codex --model gpt-5.6-sol --config model_reasoning_effort=xhigh "
             + "--add-dir /tmp/.clawdline --ask-for-approval on-request --sandbox workspace-write")
    expect("omitted reasoning effort adds no Codex override",
           Assistant.codex.command(model: "gpt-5.6-sol"),
           codexRuns + "codex --model gpt-5.6-sol")
    expect("StartPoints carries the typed override to the same command builder",
           StartPoints.itermLine(cwd: "/tmp/x", assistant: .codex,
                                 model: "gpt-5.6-sol", reasoningEffort: .high),
           "cd '/tmp/x' && " + codexRuns
             + "codex --model gpt-5.6-sol --config model_reasoning_effort=high")
    check("the line a tab is opened with is still one command",
          StartPoints.itermLine(cwd: "/tmp/x", assistant: .codex, model: "gpt-5.1-codex")
              .hasSuffix("&& " + codexRuns + "codex --model gpt-5.1-codex"))
    check("and a refused name reaches that line as no flag rather than as an argument",
          StartPoints.itermLine(cwd: "/tmp/x", assistant: .claude, model: "haiku; id")
              .hasSuffix("&& " + claudeRuns + "claude"))
}

group("a new tab is not handed the identity of whatever launched the terminal") {
    expect("Claude Code's line drops the session identity before it names the program",
           Assistant.claude.command(model: nil),
           "env -u CLAUDECODE -u CLAUDE_CODE_SESSION_ID -u CLAUDE_CODE_CHILD_SESSION "
             + "-u CLAUDE_PID -u CLAUDE_CODE_MESSAGING_SOCKET -u CLAUDE_CODE_MESSAGING_TOKEN "
             + "-u CLAUDE_CODE_BRIDGE_SESSION_ID -u CLAUDE_EFFORT -u AI_AGENT claude")
    expect("and Codex's drops the rollout id it exports, under both its spellings",
           Assistant.codex.command(model: nil),
           "env -u CODEX_THREAD_ID -u CODEX_SESSION_ID -u CODEX_SANDBOX "
             + "-u CODEX_SANDBOX_NETWORK_DISABLED codex")

    // The three read off the terminal that caused this. They are the ones that did the damage,
    // and a future version renaming one of them has to come back through this test.
    for measured in ["CLAUDE_PID", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION"] {
        check("\(measured), read off the polluted iTerm2 itself, is dropped",
              Assistant.claude.inheritedIdentityVariables.contains(measured))
    }
    // And what a session exports about the *installation* stays, because it is still true in the
    // tab being opened: `claude` typed at a prompt really is the `cli` entrypoint, the exec path
    // names the program rather than a conversation, and the subagent cap is a number this Mac
    // sets in its own settings.
    for kept in ["CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_EXECPATH",
                 "CLAUDE_CODE_MAX_SUBAGENTS_PER_SESSION"] {
        check("\(kept) is not swept up with them",
              !Assistant.claude.inheritedIdentityVariables.contains(kept))
    }
    // The two that belonged to neither column, and were therefore in neither — while the comment
    // beside the list claimed to have enumerated everything a session exports. Both are dropped
    // now, and they are here by name because the argument for each is a sentence rather than the
    // rule: `CLAUDE_EFFORT` is one conversation's chosen reasoning effort and this app has no
    // other source of one for Claude, so inheriting it would let whoever launched the terminal
    // decide how hard a child thinks; `AI_AGENT` is the only name here that a Codex tab does not
    // overwrite, so it would be a false statement about which assistant is running for the whole
    // life of that session.
    for unclassified in ["CLAUDE_EFFORT", "AI_AGENT"] {
        check("\(unclassified), which is neither an id nor an installation fact, is dropped",
              Assistant.claude.inheritedIdentityVariables.contains(unclassified))
    }
    // The claim the comment makes about that list is that it is exhaustive against a measured
    // session — twelve exported names, three kept. A future version adding a thirteenth has to
    // come back through here, which is the whole reason the count is written down.
    expect("the dropped set and the kept set account for every name a 2.1.250 session exports",
           Assistant.claude.inheritedIdentityVariables.count + 3, 12)

    // `env` execs the program, so a tty still lists `claude --model haiku` with no wrapper in
    // front of it — which is what `Assistant.reading(ofPS:)` reads a session out of. Checked on
    // a real process rather than assumed: `env -u CLAUDECODE sleep 3` lists as `sleep 3`.
    check("the program is the last word of the prefix, so `ps` reads as it always did",
          Assistant.claude.command(model: "haiku").hasSuffix(" claude --model haiku"))

    // Every route that types a command builds it here, so there is no second one left to forget.
    check("the line an iTerm2 tab is opened with carries it",
          StartPoints.itermLine(cwd: "/a/b").hasPrefix("cd '/a/b' && env -u "))
    check("and so does the line that picks a conversation back up",
          StartPoints.itermLine(cwd: "/a/b", assistant: .codex,
                                resume: "105344fb-c769-4b37-b766-403b410897eb")
              .hasPrefix("cd '/a/b' && env -u CODEX_THREAD_ID "))

    // The prefix is part of a line this app promises holds nothing a shell reads. A name that is
    // anything but an environment name would be the way that promise breaks.
    for assistant in Assistant.allCases {
        check("\(assistant.rawValue) drops plain environment names and nothing else",
              assistant.inheritedIdentityVariables.allSatisfy { name in
                  !name.isEmpty && name.allSatisfy {
                      ("A"..."Z").contains($0) || ("0"..."9").contains($0) || $0 == "_"
                  }
              })
        check("\(assistant.rawValue) names each of them once",
              Set(assistant.inheritedIdentityVariables).count
                  == assistant.inheritedIdentityVariables.count)
    }
}

group("a child row resolves only to its current parent session") {
    let wanted = "01a03d2c-c646-7521-b80b-7bc73cc1987e"
    let current = "01a03d7e-1111-7222-a333-444444444444"
    func target(_ id: String, _ assistant: Assistant, tty: String = "/dev/ttys7")
        -> TargetSession {
        TargetSession(backend: .iterm, id: id, name: "root", tty: tty,
                      windowIndex: 0, tabIndex: 0, assistant: assistant)
    }
    func task(root: String? = wanted, assistant: Assistant? = .codex) -> Orchestrator.Task {
        Orchestrator.Task(id: taskID, state: .briefed, kind: "custom", title: "a task",
                          assistant: .claude, projectDir: "/tmp", timeoutMinutes: 30,
                          created: Date(), rootSessionId: root, rootAssistant: assistant,
                          secretHash: String(repeating: "0", count: 64))
    }
    func resolve(_ task: Orchestrator.Task, parent: String? = nil,
                 targets: [TargetSession], identities: [String: String]) -> String? {
        Orchestrator.rootTerminalID(for: task, parentTerminalID: parent, among: targets,
                                    sessionID: { identities[$0.id] })
    }
    func resolveTarget(root: String = wanted, assistant: Assistant? = .codex,
                       resolution: Orchestrator.RootResolution = .task,
                       targets: [TargetSession], identities: [String: String]) -> TargetSession? {
        Orchestrator.target(forRootSession: root, assistant: assistant,
                            resolution: resolution, among: targets,
                            sessionID: { identities[$0.id] })
    }

    let codex = target("CODEX-TAB", .codex)
    expect("a Codex rollout id mounts the child under that Codex terminal",
           resolve(task(), targets: [codex], identities: [codex.id: wanted]), codex.id)
    check("a null parent id never matches an unrelated terminal",
          resolve(task(root: nil), targets: [codex], identities: [codex.id: wanted]) == nil)
    check("a stale rollout id is not accepted from the current Codex process",
          resolve(task(), targets: [codex], identities: [codex.id: current]) == nil)
    check("the production task resolver does not accept a terminal id shortcut",
          resolveTarget(root: codex.id, targets: [codex],
                        identities: [codex.id: current]) == nil)
    expect("the production task resolver accepts the process-bound conversation identity",
           resolveTarget(targets: [codex], identities: [codex.id: wanted]), codex)
    expect("handoff compatibility may address the watched terminal id directly",
           resolveTarget(root: codex.id, assistant: nil, resolution: .handoff,
                         targets: [codex], identities: [:]), codex)

    let terminalBinding = Orchestrator.canonicalRootSession(
        codex.id, assistant: .codex, among: [codex], sessionID: { _ in wanted })
    expect("dispatch canonicalizes a terminal id to the process-bound conversation",
           terminalBinding.sessionID, wanted)
    check("a resolved dispatch emits no root warning", terminalBinding.warning == nil)
    let conversationBinding = Orchestrator.canonicalRootSession(
        wanted, assistant: .codex, among: [codex], sessionID: { _ in wanted })
    expect("the conversation namespace canonicalizes to the same durable key",
           conversationBinding.sessionID, wanted)
    let duplicateCodex = target("CODEX-TAB-2", .codex, tty: "/dev/ttys8")
    let duplicateRows = [codex, duplicateCodex]
    let duplicateIdentities = [codex.id: wanted, duplicateCodex.id: wanted]
    let ambiguousBinding = Orchestrator.canonicalRootSession(
        wanted, assistant: .codex, among: duplicateRows,
        sessionID: { duplicateIdentities[$0.id] })
    expect("dispatch names duplicate same-assistant conversation rows honestly",
           ambiguousBinding.warning?["code"] as? String, "conversation_ambiguous")
    var dispatchAmbiguity: (Int, String)?
    if let warning = ambiguousBinding.warning,
       case .refused(let status, let code, _, _) = Orchestrator.rootBindingRefusal(warning) {
        dispatchAmbiguity = (status, code)
    }
    check("the HTTP dispatch boundary returns the same ambiguity before registration",
          dispatchAmbiguity?.0 == 409 && dispatchAmbiguity?.1 == "conversation_ambiguous")
    check("the canonical resolver never deduplicates two processes into one conversation",
          Orchestrator.target(
            forRootSession: wanted, assistant: .codex, resolution: .task,
            among: duplicateRows, sessionID: { duplicateIdentities[$0.id] }) == nil)
    // These five production consumers intentionally have no typed HTTP response to carry an
    // ambiguity. Pin each one to the shared resolver's fail-closed answer instead of adding five
    // parallel identity algorithms or invasive delivery seams merely for the test.
    check("handoff receipt resolution sends to neither duplicate conversation owner",
          Orchestrator.target(
            forRootSession: wanted, assistant: nil, resolution: .handoff,
            among: duplicateRows, sessionID: { duplicateIdentities[$0.id] }) == nil)
    check("root notification resolution sends to neither duplicate conversation owner",
          Orchestrator.target(
            forRootSession: wanted, assistant: .codex, resolution: .task,
            among: duplicateRows, sessionID: { duplicateIdentities[$0.id] }) == nil)
    check("workspace overlap resolution sends to neither duplicate conversation owner",
          Orchestrator.target(
            forRootSession: wanted, assistant: .codex, resolution: .task,
            among: duplicateRows, sessionID: { duplicateIdentities[$0.id] }) == nil)
    check("batch URL resolution names neither duplicate conversation terminal",
          Orchestrator.target(
            forRootSession: wanted, assistant: nil, resolution: .task,
            among: duplicateRows, sessionID: { duplicateIdentities[$0.id] }) == nil)
    check("rootTerminalID leaves a duplicate conversation task unmounted",
          Orchestrator.rootTerminalID(
            for: task(), parentTerminalID: nil, among: duplicateRows,
            sessionID: { duplicateIdentities[$0.id] }) == nil)
    var whoamiAmbiguous = false
    if case .ambiguous = RemoteServer.sessionWhoAmI(
        conversationID: wanted, among: duplicateRows,
        identityPass: { duplicateIdentities }) { whoamiAmbiguous = true }
    check("whoami calls the same two-row subject conversation_ambiguous", whoamiAmbiguous)
    let duplicateTask = task()
    var completionAmbiguity: Orchestrator.CompletionFailureCode?
    if case .refused(.failed(let code, _)) = Orchestrator.completionRecipient(
        duplicateTask, targets: duplicateRows,
        identity: { candidate in
            Orchestrator.SessionWorkIdentity(
                terminalID: candidate.id, assistant: candidate.assistant, tty: candidate.tty,
                pid: candidate.id == codex.id ? 701 : 702,
                processStart: Date(timeIntervalSince1970: candidate.id == codex.id ? 701 : 702),
                conversationID: duplicateIdentities[candidate.id])
        }) { completionAmbiguity = code }
    expect("completion names that same subject with the same typed ambiguity",
           completionAmbiguity?.rawValue, "conversation_ambiguous")
    let unresolvedBinding = Orchestrator.canonicalRootSession(
        "MISSING", assistant: .codex, among: [codex], sessionID: { _ in wanted })
    expect("an unresolved root spelling is retained for compatibility",
           unresolvedBinding.sessionID, "MISSING")
    expect("and dispatch reports the typed orphan-risk warning",
           unresolvedBinding.warning?["code"] as? String, "root_unresolved")

    // Same terminal and tty, but a different assistant now occupies them. Even a stale identity
    // source claiming the old id cannot move this new process under the Codex root.
    let reused = target(codex.id, .claude, tty: codex.tty)
    check("terminal and tty reuse cannot cross the assistant boundary",
          resolve(task(), targets: [reused], identities: [reused.id: wanted]) == nil)
    check("the production resolver applies the assistant boundary to conversation identity",
          resolveTarget(targets: [reused], identities: [reused.id: wanted]) == nil)

    let claude = target("CLAUDE-TAB", .claude)
    expect("a pre-root.assistant registry row keeps its Claude behaviour",
           resolve(task(assistant: nil), targets: [claude], identities: [claude.id: wanted]),
           claude.id)
    expect("a declared parent task keeps the direct depth-2 mapping",
           resolve(task(root: current), parent: "PARENT-CHILD-TAB", targets: [codex],
                   identities: [codex.id: wanted]),
           "PARENT-CHILD-TAB")
    expect("root cancellation reads the same process-bound identity seam",
           Orchestrator.rootIdentity(of: codex, sessionID: { candidate in
               candidate.id == codex.id ? wanted : nil
           }), wanted)

    let stored = Orchestrator.stored(task())
    expect("the root assistant survives the durable registry",
           Orchestrator.task(from: stored)?.rootAssistant, .codex)
    let publicRoot = Orchestrator.recordForTesting(task())["root"] as? [String: Any]
    expect("the public record says which assistant owns its root id",
           publicRoot?["assistant"] as? String, "codex")
}

group("a public session id and task root use one process-bound identity") {
    let staleHook = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let currentRollout = "11111111-2222-4333-8444-555555555555"
    let codex = TargetSession(backend: .iterm, id: "CODEX-TAB", name: "root",
                              tty: "/dev/ttys7", windowIndex: 0, tabIndex: 0,
                              assistant: .codex)

    let exposed = RemoteServer.sessionIdentity(assistant: codex.assistant,
                                                processBound: currentRollout)
    expect("a Codex row exposes the rollout proved for its current process, not a tty's old hook",
           exposed, currentRollout)

    let task = Orchestrator.Task(id: taskID, state: .briefed, kind: "custom", title: "a task",
                                 assistant: .claude, projectDir: "/tmp", timeoutMinutes: 30,
                                 created: Date(), rootSessionId: currentRollout,
                                 rootAssistant: .codex,
                                 secretHash: String(repeating: "0", count: 64))
    expect("the public identity mounts a task carrying that same verified conversation",
           Orchestrator.rootTerminalID(for: task, parentTerminalID: nil, among: [codex],
                                       sessionID: { _ in exposed }), codex.id)

    var staleTask = task
    staleTask.rootSessionId = staleHook
    check("the stale tty hook remains unable to mount a Codex task",
          Orchestrator.rootTerminalID(for: staleTask, parentTerminalID: nil, among: [codex],
                                      sessionID: { _ in currentRollout }) == nil)
    check("a stale hook alone is not published as a Codex process identity",
          RemoteServer.sessionIdentity(assistant: .codex, processBound: nil) == nil)
    expect("a Claude row likewise prefers its process-validated registry or transcript identity",
           RemoteServer.sessionIdentity(assistant: .claude, processBound: currentRollout),
           currentRollout)
    check("an ordinary shell cannot inherit an old assistant hook",
          RemoteServer.sessionIdentity(assistant: nil, processBound: currentRollout) == nil)
}

group("a task keeps its per-dispatch Codex reasoning effort") {
    var task = Orchestrator.Task(id: taskID, state: .briefed, kind: "custom", title: "reason",
                                 assistant: .codex, projectDir: "/tmp", timeoutMinutes: 30,
                                 created: Date(), secretHash: String(repeating: "0", count: 64))
    task.reasoningEffort = .xhigh
    let stored = Orchestrator.stored(task)
    expect("the durable row names reasoning effort",
           stored["reasoning_effort"] as? String, "xhigh")
    expect("reasoning effort survives a registry reload",
           Orchestrator.task(from: stored)?.reasoningEffort, .xhigh)
    expect("the public task record exposes reasoning effort",
           Orchestrator.recordForTesting(task)["reasoning_effort"] as? String, "xhigh")

    task.state = .queued
    task.model = "gpt-5.6-sol"
    task.permission = .edits
    var startedWith: (Assistant, String?, ReasoningEffort?, Permission)?
    let spawned = Orchestrator.spawn(task) { _, assistant, model, effort, permission, _ in
        startedWith = (assistant, model, effort, permission)
        return .started(id: "reasoning-child", backend: .iterm)
    }
    expect("spawn carries reasoning effort into the terminal starter",
           startedWith?.2, .xhigh)
    check("and keeps the other typed launch choices beside it",
          startedWith?.0 == .codex && startedWith?.1 == "gpt-5.6-sol"
            && startedWith?.3 == .edits && spawned.state == .spawning)

    var inherited = task
    inherited.reasoningEffort = nil
    check("an inherited default writes no durable or public field",
          Orchestrator.stored(inherited)["reasoning_effort"] == nil
            && Orchestrator.recordForTesting(inherited)["reasoning_effort"] == nil)
}

group("how far a child may go is this Mac's answer, not the asking session's") {
    // The setting is a *ceiling* and a default at once: a task that says nothing gets it, and one
    // that asks for more is given it instead. What this pins down is the ordering — that more
    // never wins — and that the words are a closed list rather than a flag somebody assembled.
    check("the three escalate in the order they are written",
          Permission.ask < Permission.edits && Permission.edits < Permission.full)
    check("a word that is not one of them is not a permission",
          Permission(rawValue: "yolo") == nil)
    // Dropped because it is model-dependent, not because it is broken: `--permission-mode auto`
    // selects auto mode on Sonnet and Opus, and produces `manual` — everything asked — on Haiku,
    // silently. A word a task fills in has to mean the same thing to every session it can name.
    check("nor is `auto`, which means one thing on Opus and the opposite on Haiku",
          Permission(rawValue: "auto") == nil)

    func granted(asked: Permission?, ceiling: Permission) -> Permission {
        min(asked ?? ceiling, ceiling)
    }
    expect("asking for nothing takes the ceiling", granted(asked: nil, ceiling: .edits), .edits)
    expect("asking for less than the ceiling is honoured",
           granted(asked: .ask, ceiling: .full), .ask)
    expect("asking for more than the ceiling is not",
           granted(asked: .full, ceiling: .edits), .edits)
    expect("and a Mac that says ask means ask, whatever a task wants",
           granted(asked: .full, ceiling: .ask), .ask)

    // The flags themselves. `ask` is each CLI's own default, so it is spelled by adding nothing —
    // which is what keeps a task that says nothing from changing the command line at all.
    expect("the quiet mode adds nothing to either command line",
           Permission.ask.flags(for: .claude) + Permission.ask.flags(for: .codex), "")
    check("and the other two do, differently, because the two CLIs spell it differently",
          !Permission.edits.flags(for: .claude).isEmpty
              && !Permission.edits.flags(for: .codex).isEmpty
              && Permission.edits.flags(for: .claude) != Permission.edits.flags(for: .codex))
    // The exact strings, because these were read off a status line one at a time and getting one
    // wrong is silent: the session starts, the flag is ignored, and the mode is `manual`.
    expect("files-without-asking is spelled acceptEdits for Claude Code",
           Permission.edits.flags(for: .claude), "--permission-mode acceptEdits")
    check("a bare command is still what an unasked-for permission produces",
          Assistant.claude.command(model: nil, permission: .ask)
              == Assistant.claude.dropInheritedIdentity + "claude")
}

group("the directory a child is given reach over is a path, not a fragment of one") {
    // The second string a dispatch puts on a command line, and the one that decides whether a
    // child can read its own briefing without asking. Same shape of rule as `modelName`: a closed
    // alphabet rather than an escaping pass, so there is nothing in it for a shell to read.
    func ok(_ raw: String) -> Bool { StartPoints.extraDir(raw) == raw }
    check("the task directory is one", ok("/tmp/.clawdline/0f8fad5b-d9cb-469f-a165-70867728950e"))
    check("so is the parent it sits in", ok("/tmp/.clawdline"))
    check("and a plain project path", ok("/Users/me/code/thing"))

    check("a relative path is not — there is no cwd to resolve it against here",
          StartPoints.extraDir("tmp/.clawdline") == nil)
    check("nor one that walks upwards", StartPoints.extraDir("/tmp/../etc") == nil)
    check("nor one with a space", StartPoints.extraDir("/tmp/two words") == nil)
    check("nor one with a semicolon", StartPoints.extraDir("/tmp/x;id") == nil)
    check("nor a substitution", StartPoints.extraDir("/tmp/$(id)") == nil)
    check("nor a quote", StartPoints.extraDir("/tmp/\"x\"") == nil)
    check("nor a newline", StartPoints.extraDir("/tmp/x\nid") == nil)
    check("nor nothing at all", StartPoints.extraDir("") == nil && StartPoints.extraDir(nil) == nil)
    check("nor a path past 256 characters",
          StartPoints.extraDir("/tmp/" + String(repeating: "a", count: 256)) == nil)

    // Both CLIs spell it the same, which is the only reason the flag is not a switch.
    let claudeRuns = Assistant.claude.dropInheritedIdentity
    expect("claude is given it by name", Assistant.claude.command(model: nil, addDir: "/tmp/.clawdline"),
           claudeRuns + "claude --add-dir /tmp/.clawdline")
    expect("and so is codex", Assistant.codex.command(model: nil, addDir: "/tmp/.clawdline"),
           Assistant.codex.dropInheritedIdentity + "codex --add-dir /tmp/.clawdline")
    expect("with the model in front of it when there is one",
           Assistant.claude.command(model: "haiku", addDir: "/tmp/.clawdline"),
           claudeRuns + "claude --model haiku --add-dir /tmp/.clawdline")
    check("a refused path reaches the line as no flag rather than as an argument",
          StartPoints.itermLine(cwd: "/tmp/x", assistant: .claude, addDir: "/tmp/a b")
              .hasSuffix("&& " + claudeRuns + "claude"))
    expect("and nothing at all is still the bare command",
           Assistant.claude.command(model: nil, addDir: nil), claudeRuns + "claude")
}

group("a task id is the name of a directory, so it may not be a path") {
    check("a lowercase UUID is one", Orchestrator.isTaskID(taskID))
    check("in upper case it is not", !Orchestrator.isTaskID(taskID.uppercased()))
    check("nor is a walk upwards", !Orchestrator.isTaskID("../../etc/passwd"))
    check("nor is one with a slash at the right length",
          !Orchestrator.isTaskID("0f8fad5b-d9cb-469f-a165-7086772895/e"))
    check("nor a letter that is not hex",
          !Orchestrator.isTaskID("0f8fad5b-d9cb-469f-a165-70867728950g"))
    check("nor one character too few", !Orchestrator.isTaskID(String(taskID.dropLast())))
    check("nor nothing at all", !Orchestrator.isTaskID(""))
}

group("a worktree is named by repository and task, without accepting a path as a ref") {
    expect("the branch is the complete task id in Clawdline's namespace",
           Orchestrator.worktreeBranch(for: taskID), "clawdline/task/\(taskID)")
    check("branch naming fails closed on anything that is not a task id",
          Orchestrator.worktreeBranch(for: "../../main") == nil)

    let first = Orchestrator.worktreePath(project: "/Users/me/code/two words/專案..", taskID: taskID)
    let second = Orchestrator.worktreePath(project: "/Users/other/code/專案..", taskID: taskID)
    check("the checkout path lives under Application Support and contains the task id",
          first?.contains("/Library/Application Support/Clawdline/worktrees/") == true
              && first?.hasSuffix("/\(taskID)") == true
              && first.map(StartPoints.usable) == true)
    let firstRepo = first.map { URL(fileURLWithPath: $0).deletingLastPathComponent().lastPathComponent }
    let secondRepo = second.map { URL(fileURLWithPath: $0).deletingLastPathComponent().lastPathComponent }
    check("a hostile-looking Unicode repository name becomes only an ASCII slug plus hash",
          firstRepo?.allSatisfy {
              ("a"..."z").contains($0) || ("0"..."9").contains($0) || $0 == "-"
          } == true && firstRepo?.contains("..") == false)
    check("equal basenames at different full paths receive different repository slugs",
          firstRepo != secondRepo)
    if let first {
        check("worktree storage never becomes a remote-start place",
              !StartPoints.isDurablePlace(first))
    }
}

group("worktree cleanup chooses data preservation before disk reclamation") {
    expect("an untouched checkout and its branch may both go",
           Orchestrator.worktreeDisposal(commits: 0, dirty: false,
                                         headOnBranch: true, branchExists: true), .removeAll)
    expect("a clean committed checkout goes but its branch stays",
           Orchestrator.worktreeDisposal(commits: 2, dirty: false,
                                         headOnBranch: true, branchExists: true),
           .removeTreeKeepBranch)
    expect("a committed dirty checkout is the only copy and stays",
           Orchestrator.worktreeDisposal(commits: 2, dirty: true,
                                         headOnBranch: true, branchExists: true), .keepEverything)
    expect("an uncommitted dirty checkout is the only copy and stays",
           Orchestrator.worktreeDisposal(commits: 0, dirty: true,
                                         headOnBranch: true, branchExists: true), .keepEverything)
    expect("zero commits does not authorize deleting a checkout whose HEAD moved",
           Orchestrator.worktreeDisposal(commits: 0, dirty: false,
                                         headOnBranch: false, branchExists: true), .keepEverything)
    expect("a missing branch is never recreated or cleaned around",
           Orchestrator.worktreeDisposal(commits: 0, dirty: false,
                                         headOnBranch: true, branchExists: false), .keepEverything)
    expect("an unreadable git fact fails safe",
           Orchestrator.worktreeDisposal(commits: nil, dirty: nil,
                                         headOnBranch: nil, branchExists: true), .keepEverything)
}

group("worktree task records, briefings and shared-tree coordination stay distinct") {
    func task(_ id: String = taskID, isolated: Bool) -> Orchestrator.Task {
        var made = Orchestrator.Task(id: id, state: .briefed, kind: "custom", title: "edit it",
                                     assistant: .codex, projectDir: "/repo/packages/app",
                                     timeoutMinutes: 30, created: Date(),
                                     secretHash: String(repeating: "0", count: 64))
        if isolated {
            made.isolation = .worktree
            made.spawnedAt = Date()
            let path = Orchestrator.worktreePath(project: "/repo", taskID: id)!
            made.worktree = Orchestrator.Worktree(
                path: path,
                branch: "clawdline/task/\(id)", base: String(repeating: "a", count: 40),
                repository: "/repo", cwd: path + "/packages/app")
        }
        return made
    }

    let shared = task(isolated: false)
    let isolated = task(isolated: true)
    let sharedRecord = Orchestrator.recordForTesting(shared)
    let isolatedRecord = Orchestrator.recordForTesting(isolated)
    check("a shared-tree record has no worktree key at all", sharedRecord["worktree"] == nil)
    let worktree = isolatedRecord["worktree"] as? [String: Any]
    check("an isolated record reports its path, branch and immutable base",
          worktree?["path"] as? String == isolated.worktree?.path
              && worktree?["branch"] as? String == isolated.worktree?.branch
              && worktree?["base"] as? String == isolated.worktree?.base)
    expect("projectDir remains the repository-facing path",
           isolatedRecord["projectDir"] as? String, "/repo/packages/app")
    var queuedIsolated = isolated
    queuedIsolated.state = .queued
    queuedIsolated.spawnedAt = nil
    let queuedRecord = Orchestrator.recordForTesting(queuedIsolated)
    check("a queued isolated record names the mode without publishing a candidate base receipt",
          queuedRecord["isolation"] as? String == "worktree"
              && queuedRecord["worktree"] == nil)

    let restored = Orchestrator.task(from: Orchestrator.stored(isolated))
    check("the registry round-trips all worktree identity and observed facts",
          restored?.isolation == .worktree && restored?.worktree?.path == isolated.worktree?.path
              && restored?.worktree?.branch == isolated.worktree?.branch
              && restored?.worktree?.base == isolated.worktree?.base
              && restored?.worktree?.repository == isolated.worktree?.repository
              && restored?.worktree?.cwd == isolated.worktree?.cwd)
    let tmpFixture = URL(fileURLWithPath: "/tmp", isDirectory: true)
        .appendingPathComponent("clawdline-worktree-round-trip-\(UUID().uuidString)")
    let tmpRepository = tmpFixture.appendingPathComponent("repository", isDirectory: true)
    let tmpAlias = tmpFixture.appendingPathComponent("repository-link", isDirectory: true)
    let manager = FileManager.default
    let fixtureReady = (try? manager.createDirectory(at: tmpRepository,
                                                      withIntermediateDirectories: true)) != nil
        && (try? manager.createSymbolicLink(at: tmpAlias,
                                            withDestinationURL: tmpRepository)) != nil
    defer { try? manager.removeItem(at: tmpFixture) }
    var tmpTask = task(isolated: false)
    tmpTask.isolation = .worktree
    let reportedTmpRepository = tmpAlias.path
    let canonicalTmpRepository = tmpAlias.standardizedFileURL.resolvingSymlinksInPath().path
    let tmpPath = Orchestrator.worktreePath(project: reportedTmpRepository, taskID: taskID)!
    tmpTask.worktree = Orchestrator.Worktree(
        path: tmpPath, branch: "clawdline/task/\(taskID)",
        base: String(repeating: "b", count: 40), repository: canonicalTmpRepository, cwd: tmpPath)
    let restoredTmp = Orchestrator.task(from: Orchestrator.stored(tmpTask))
    check("a symlinked /tmp repository task survives a registry restart with one canonical slug",
          fixtureReady && restoredTmp?.id == tmpTask.id
              && restoredTmp?.worktree?.repository == canonicalTmpRepository
              && restoredTmp?.worktree?.path == tmpTask.worktree?.path)
    var legacy = Orchestrator.stored(shared)
    legacy.removeValue(forKey: "isolation")
    legacy.removeValue(forKey: "worktree")
    check("a registry row from before isolation remains a shared-tree task",
          Orchestrator.task(from: legacy)?.isolation == Orchestrator.Isolation.none
              && Orchestrator.task(from: legacy)?.worktree == nil)

    let sharedBrief = Orchestrator.childBrief(for: shared)
    let isolatedBrief = Orchestrator.childBrief(for: isolated)
    check("a shared-tree child keeps the existing briefing without worktree rules",
          !sharedBrief.contains("## Your isolated checkout")
              && !sharedBrief.contains("only on this branch"))
    check("an isolated child is told its branch, base and the rules it shares with every isolate",
          isolatedBrief.contains(isolated.worktree!.branch)
              && isolatedBrief.contains(isolated.worktree!.base)
              && isolatedBrief.contains("Do not push")
              && isolatedBrief.contains("gitignore"))
    // The delivery rule is not the same for both assistants, and this pair is why. A linked
    // worktree keeps its git metadata in the base repository, outside what codex may write, so
    // telling a codex child to commit produces a `failure` with the work done — twice in one week
    // before the briefing was split. The old single assertion asserted "only on this branch"
    // against a codex fixture, which locked the defect in.
    check("a codex isolate is told to leave the bytes for root, not to commit",
          isolatedBrief.contains("Do not commit")
              && !isolatedBrief.contains("only on this branch"))
    var isolatedClaude = isolated
    isolatedClaude.assistant = .claude
    let claudeBrief = Orchestrator.childBrief(for: isolatedClaude)
    check("a claude isolate keeps the commit-on-your-branch delivery rule",
          claudeBrief.contains("only on this branch")
              && !claudeBrief.contains("Do not commit"))
    check("the work-inside line names the effective monorepo cwd, not projectDir",
          isolatedBrief.contains("Work inside \(isolated.worktree!.cwd)."))

    expect("transcript discovery uses the isolated cwd",
           Orchestrator.cwd(of: isolated), isolated.worktree?.cwd)
    expect("and shared-tree discovery still uses projectDir",
           Orchestrator.cwd(of: shared), shared.projectDir)

    var claiming = isolated
    claiming.claims = ["Sources/Parser.swift"]
    claiming.claimsDeclared = true
    claiming.claimKeys = Orchestrator.freezeClaims(claiming.claims,
                                                    projectDir: claiming.projectDir)
    let claimWarnings = Orchestrator.prepareClaimsForIsolation(&claiming)
    check("workspace-local claims are discarded with one typed warning",
          claiming.claims.isEmpty && claiming.claimKeys.isEmpty
              && claimWarnings.count == 1
              && claimWarnings.first?["code"] as? String == "claims_ignored_for_worktree")
    claiming.state = .success
    check("a worktree task cannot emit the shared-tree landing reminder after claims are cleared",
          Orchestrator.landingNotice(for: claiming).isEmpty)

    let other = task("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", isolated: false)
    expect("an isolated task never receives a shared-project L1 overlap warning",
           Orchestrator.workspaceOverlaps(for: isolated, among: [other]).count, 0)
    var serialized = isolated
    serialized.serialize = ["build"]
    check("serialize and isolation retain both independent controls",
          serialized.isolation == .worktree && serialized.serialize == ["build"])
}

group("workspace overlap is a path-component relationship") {
    expect("the same directory is shared",
           Orchestrator.sharedWorkspaceDirectory("/a/b", "/a/b"), "/a/b")
    expect("a child is the shared part of an ancestor and descendant",
           Orchestrator.sharedWorkspaceDirectory("/a/b", "/a/b/c"), "/a/b/c")
    expect("and order does not change that answer",
           Orchestrator.sharedWorkspaceDirectory("/a/b/c", "/a/b"), "/a/b/c")
    expect("a string prefix that ends mid-component is not overlap",
           Orchestrator.sharedWorkspaceDirectory("/a/b", "/a/bc"), nil)
    expect("case follows the repository's exact resolved-path comparisons",
           Orchestrator.sharedWorkspaceDirectory("/a/B", "/a/b"), nil)
    expect("standard path components are compared after dot segments are removed",
           Orchestrator.sharedWorkspaceDirectory("/a/b/../c", "/a/c/d"), "/a/c/d")
    expect("a trailing slash is the same directory",
           Orchestrator.sharedWorkspaceDirectory("/a/b/", "/a/b"), "/a/b")
}

group("workspace overlap follows the whole dispatch tree") {
    let firstRoot = "11111111-2222-3333-4444-555555555555"
    let secondRoot = "66666666-7777-8888-9999-aaaaaaaaaaaa"
    func task(_ id: String, dir: String, root: String?, state: Orchestrator.State = .briefed,
              created: TimeInterval = 1, claims: [String]? = nil) -> Orchestrator.Task {
        var made = Orchestrator.Task(id: id, state: state, kind: "custom", title: "a task",
                                     assistant: .claude, projectDir: dir, timeoutMinutes: 30,
                                     created: Date(timeIntervalSince1970: created),
                                     rootSessionId: root,
                                     secretHash: String(repeating: "0", count: 64))
        if let claims {
            made.claims = claims
            made.claimsDeclared = true
            made.claimKeys = Orchestrator.freezeClaims(claims, projectDir: dir)
        }
        return made
    }

    let otherID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
    let other = task(otherID, dir: "/a/b/c", root: secondRoot)
    expect("the same known root is quiet",
           Orchestrator.workspaceOverlaps(for: task(taskID, dir: "/a/b", root: secondRoot),
                                          among: [other]).count, 0)
    let newcomer = task(taskID, dir: "/a/b/c/d", root: firstRoot)
    let across = Orchestrator.workspaceOverlaps(for: newcomer, among: [other])
    expect("different roots warn", across.count, 1)
    let declaredNewcomer = task(taskID, dir: "/a/b", root: firstRoot,
                                claims: ["Sources"])
    let declaredOther = task(otherID, dir: "/a/b/c", root: secondRoot,
                             claims: ["Tests"])
    let disjointOverlaps = Orchestrator.workspaceOverlaps(for: declaredNewcomer,
                                                           among: [declaredOther])
    expect("disjoint declarations silence the directory warning",
           disjointOverlaps.count, 0)
    check("neither side gets a typed line for a disjoint declared pair",
          Orchestrator.workspaceOverlapNotices(newTask: declaredNewcomer,
                                               overlaps: disjointOverlaps).isEmpty)
    let intersectingUnknown = task(otherID, dir: "/a/b/c", root: nil,
                                   claims: ["Sources"])
    let intersectingNewcomer = task(taskID, dir: "/a/b", root: firstRoot,
                                    claims: ["c/Sources"])
    expect("intersecting declarations with an unknown root still warn",
           Orchestrator.workspaceOverlaps(for: intersectingNewcomer,
                                          among: [intersectingUnknown]).count, 1)
    let readOnlyNewcomer = task(taskID, dir: "/a/b", root: firstRoot, claims: [])
    let readOnlyOther = task(otherID, dir: "/a/b/c", root: secondRoot, claims: [])
    expect("a declaration paired with read-only is quiet",
           Orchestrator.workspaceOverlaps(for: declaredNewcomer,
                                          among: [readOnlyOther]).count, 0)
    expect("two read-only declarations are quiet",
           Orchestrator.workspaceOverlaps(for: readOnlyNewcomer,
                                          among: [readOnlyOther]).count, 0)
    expect("a declaration paired with an absent field still warns",
           Orchestrator.workspaceOverlaps(for: declaredNewcomer, among: [other]).count, 1)
    let absentOverlaps = Orchestrator.workspaceOverlaps(for: declaredNewcomer, among: [other])
    let absentNotices = Orchestrator.workspaceOverlapNotices(newTask: declaredNewcomer,
                                                              overlaps: absentOverlaps)
    check("both sides get typed lines when one claims field is absent",
          absentNotices.count == 2
              && absentNotices.contains {
                  $0.rootSessionID == firstRoot && $0.taskID == declaredNewcomer.id
              }
              && absentNotices.contains {
                  $0.rootSessionID == secondRoot && $0.taskID == other.id
              })
    expect("two absent fields still warn",
           Orchestrator.workspaceOverlaps(for: newcomer, among: [other]).count, 1)
    let mixedDisjoint = task("10101010-2020-3030-4040-505050505050",
                             dir: "/a/b/c", root: secondRoot, claims: ["Tests"])
    let mixedSameRoot = task("20202020-3030-4040-5050-606060606060",
                             dir: "/a/b", root: firstRoot, claims: ["Sources"])
    let mixed = Orchestrator.workspaceOverlaps(
        for: declaredNewcomer, among: [mixedDisjoint, other, mixedSameRoot]
    )
    check("silence is decided per pair within one dispatch",
          mixed.map { $0.task.id } == [other.id])
    let mixedNotices = Orchestrator.workspaceOverlapNotices(newTask: declaredNewcomer,
                                                             overlaps: mixed)
    check("typed lines contain only the warning pair from a mixed dispatch",
          mixedNotices.count == 2
              && mixedNotices.allSatisfy {
                  $0.taskID != mixedDisjoint.id && $0.taskID != mixedSameRoot.id
                      && !$0.line.contains(mixedDisjoint.id.prefix(8))
                      && !$0.line.contains(mixedSameRoot.id.prefix(8))
              }
              && mixedNotices.contains { $0.taskID == declaredNewcomer.id }
              && mixedNotices.contains { $0.taskID == other.id })
    expect("different roots in unrelated directories remain quiet",
           Orchestrator.workspaceOverlaps(
               for: task(taskID, dir: "/unrelated", root: firstRoot), among: [other]
           ).count, 0)
    expect("the overlap records the shared descendant directory", across.first?.sharedDir,
           "/a/b/c/d")
    let warning = across.first?.warning(for: taskID)
    check("the wire warning names its code, opposing task and shared directory consistently",
          warning?["code"] as? String == "workspace_overlap"
              && warning?["task"] as? String == otherID
              && warning?["dir"] as? String == "/a/b/c/d"
              && (warning?["message"] as? String)?.contains("at /a/b/c/d") == true)

    let unknown = task("bbbbbbbb-cccc-dddd-eeee-ffffffffffff",
                       dir: "/a/b/child", root: nil)
    expect("an existing task with unknown root warns",
           Orchestrator.workspaceOverlaps(for: task(taskID, dir: "/a/b", root: firstRoot),
                                          among: [unknown]).count, 1)
    expect("an unknown new root warns against a known root",
           Orchestrator.workspaceOverlaps(for: task(taskID, dir: "/a/b", root: nil),
                                          among: [other]).count, 1)
    expect("and an unknown new root warns even against another unknown root",
           Orchestrator.workspaceOverlaps(for: task(taskID, dir: "/a/b", root: nil),
                                          among: [unknown]).count, 1)

    var parent = task("12121212-3434-5656-7878-909090909090",
                      dir: "/a/b", root: firstRoot)
    parent.depth = 1
    var grandchild = task("23232323-4545-6767-8989-010101010101",
                          dir: "/a/b/child", root: nil)
    grandchild.depth = 2
    grandchild.parentTaskId = parent.id
    var sibling = task("34343434-5656-7878-9090-121212121212",
                       dir: "/a/b/sibling", root: nil)
    sibling.depth = 2
    sibling.parentTaskId = parent.id
    expect("a grandchild inherits its parent's root and does not warn about its tree",
           Orchestrator.workspaceOverlaps(for: grandchild,
                                          among: [parent, sibling]).count, 0)

    let finished = task("cccccccc-dddd-eeee-ffff-000000000000",
                        dir: "/a/b", root: secondRoot, state: .success)
    let spawnFailed = task("45454545-6767-8989-0101-232323232323",
                           dir: "/a/b", root: secondRoot, state: .spawnFailed)
    let scanningNewcomer = task(taskID, dir: "/a/b", root: firstRoot)
    expect("all terminal existing tasks are outside the dispatch-time scan",
           Orchestrator.workspaceOverlaps(for: scanningNewcomer,
                                          among: [finished, spawnFailed]).count, 0)
    let failedNewcomer = task(taskID, dir: "/a/b", root: firstRoot, state: .spawnFailed)
    expect("a terminal new task does not report stale overlaps",
           Orchestrator.workspaceOverlaps(for: failedNewcomer, among: [other]).count, 0)
    let queued = task("dddddddd-eeee-ffff-0000-111111111111",
                      dir: "/a/b/queued", root: secondRoot, state: .queued, created: 2)
    let spawning = task("eeeeeeee-ffff-0000-1111-222222222222",
                        dir: "/a/b/spawning", root: secondRoot, state: .spawning, created: 1)
    expect("queued tasks are absent from L1 while spawning tasks remain active",
           Orchestrator.workspaceOverlaps(for: scanningNewcomer,
                                          among: [queued, spawning]).count, 1)
    let queuedNewcomer = task(taskID, dir: "/a/b", root: firstRoot, state: .queued)
    expect("a queued newcomer does not warn before it can touch the workspace",
           Orchestrator.workspaceOverlaps(for: queuedNewcomer, among: [other]).count, 0)
    expect("overlaps are sorted by creation time",
           Orchestrator.workspaceOverlaps(for: scanningNewcomer,
                                          among: [queued, spawning]).first?.task.id,
           spawning.id)
    let sameTimeLaterID = task("ffffffff-ffff-ffff-ffff-ffffffffffff",
                               dir: "/a/b/later-id", root: secondRoot, created: 1)
    expect("equal creation times are sorted by task id",
           Orchestrator.workspaceOverlaps(for: scanningNewcomer,
                                          among: [sameTimeLaterID, spawning]).first?.task.id,
           spawning.id)

    let quietReply = Orchestrator.dispatchPayload(record: ["id": taskID],
                                                  taskID: taskID, overlaps: [])
    check("a dispatch without overlap omits warnings rather than returning an empty array",
          quietReply["warnings"] == nil)
    let warnedReply = Orchestrator.dispatchPayload(record: ["id": taskID],
                                                   taskID: taskID, overlaps: across)
    expect("a dispatch with overlap includes one warning",
           (warnedReply["warnings"] as? [[String: Any]])?.count, 1)

    let third = task("56565656-7878-9090-1212-343434343434",
                     dir: "/a/b/third", root: nil)
    let notificationOverlaps = Orchestrator.workspaceOverlaps(for: scanningNewcomer,
                                                               among: [other, third])
    let notices = Orchestrator.workspaceOverlapNotices(newTask: scanningNewcomer,
                                                       overlaps: notificationOverlaps)
    let newSide = notices.filter { $0.rootSessionID == firstRoot }
    expect("the new task's root receives one aggregate line", newSide.count, 1)
    check("the aggregate line names every overlap",
          newSide.first?.line.contains(other.id.prefix(8)) == true
              && newSide.first?.line.contains(third.id.prefix(8)) == true)
    let decodedOverlap = newSide.first.flatMap { ClawdlineMessage.decode($0.line) }
    if case let .workspaceOverlap(task, audience, rows)? = decodedOverlap?.event {
        check("workspace delivery is a typed aggregate notice",
              task.id == scanningNewcomer.id && audience == .root && rows.count == 2
                  && Set(rows.map(\.path)) == Set(["/a/b/c", "/a/b/third"]))
    } else {
        check("workspace delivery is a typed aggregate notice", false)
    }
    expect("an identifiable opposing root receives its own line",
           notices.filter { $0.rootSessionID == secondRoot }.count, 1)
    expect("a null opposing root is quietly skipped", notices.count, 2)
    expect("no overlap produces no notification decision",
           Orchestrator.workspaceOverlapNotices(newTask: scanningNewcomer,
                                                overlaps: []).count, 0)
}

group("declared write claims are reserved across dispatch trees") {
    let firstRoot = "11111111-2222-3333-4444-555555555555"
    let secondRoot = "66666666-7777-8888-9999-aaaaaaaaaaaa"
    func task(_ id: String, dir: String = "/repo", root: String? = firstRoot,
              state: Orchestrator.State = .queued, claims: [String],
              serialize: [String] = [], created: TimeInterval = 1,
              title: String = "claimed work", rootLabel: String? = "root label")
        -> Orchestrator.Task {
        var made = Orchestrator.Task(id: id, state: state, kind: "custom", title: title,
                                     assistant: .claude, projectDir: dir, timeoutMinutes: 30,
                                     created: Date(timeIntervalSince1970: created),
                                     rootSessionId: root, rootLabel: rootLabel,
                                     serialize: serialize, claims: claims,
                                     claimsDeclared: true,
                                     secretHash: String(repeating: "0", count: 64))
        made.claimKeys = Orchestrator.freezeClaims(claims, projectDir: dir)
        return made
    }

    let holderID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
    let holder = task(holderID, root: secondRoot, claims: ["Sources"])
    let exact = task(taskID, root: firstRoot, claims: ["Sources"])
    let equal = Orchestrator.claimsOverlaps(for: exact, among: [holder])
    expect("equal absolute claims conflict", equal.first?.paths, ["/repo/Sources"])
    expect("a directory claim covers a descendant file",
           Orchestrator.claimsOverlaps(
               for: task(taskID, root: firstRoot, claims: ["Sources/Orchestrator.swift"]),
               among: [holder]
           ).first?.paths, ["/repo/Sources/Orchestrator.swift"])
    expect("the ancestor relationship is symmetric",
           Orchestrator.claimsOverlaps(
               for: task(taskID, root: firstRoot, claims: ["Sources"]),
               among: [task(holderID, root: secondRoot,
                            claims: ["Sources/Orchestrator.swift"])]
           ).first?.paths, ["/repo/Sources/Orchestrator.swift"])
    expect("a string prefix ending mid-component is not a claim conflict",
           Orchestrator.claimsOverlaps(
               for: task(taskID, root: firstRoot, claims: ["a/b"]),
               among: [task(holderID, root: secondRoot, claims: ["a/bc"])]
           ).count, 0)
    expect("claim path case follows L1's exact resolved spelling",
           Orchestrator.claimsOverlaps(
               for: task(taskID, root: firstRoot, claims: ["Sources"]),
               among: [task(holderID, root: secondRoot, claims: ["sources"])]
           ).count, 0)

    let nested = task(holderID, dir: "/repo", root: secondRoot,
                      claims: ["packages/app/Sources"])
    expect("claims are absolutized before nested project directories are compared",
           Orchestrator.claimsOverlaps(
               for: task(taskID, dir: "/repo/packages/app", root: firstRoot,
                         claims: ["Sources/file.swift"]),
               among: [nested]
           ).first?.paths, ["/repo/packages/app/Sources/file.swift"])

    check("a cross-root overlap is a blocker",
          equal.count == 1 && equal.first?.sameRoot == false)
    let busy = equal.first.map {
        Orchestrator.workspaceBusyExtra($0, now: Date(timeIntervalSince1970: 61))
    } ?? [:]
    check("workspace_busy context names the blocker, title, root, creation and paths",
          busy["blocking_task"] as? String == holderID
              && busy["title"] as? String == "claimed work"
              && busy["root_label"] as? String == "root label"
              && busy["created"] as? Int == 1
              && busy["conflict_paths"] as? [String] == ["/repo/Sources"]
              && busy["retry_after"] as? Int == 60)
    // Both fixtures default to the same `root label` — the two-roots-same-label case a real
    // dispatch hit tonight — so this also proves root_key distinguishes what the label cannot.
    check("the 409 is context-sufficient: real age, and root_key rather than the collidable label",
          busy["age_seconds"] as? Int == 60
              && busy["root_key"] as? String == Orchestrator.rootKeyDigest(secondRoot)
              && Orchestrator.rootKeyDigest(secondRoot) != Orchestrator.rootKeyDigest("root label")
              && (busy["root_key"] as? String)?.count == 8)
    // Pinned against the function's own output would pass no matter what the function
    // computes — this is the actual SHA-256 of the literal string below, truncated to 8 hex
    // characters (`printf '%s' clawdline-root-key-fixture | shasum -a 256 | cut -c1-8`), so a
    // change to the algorithm or its inputs breaks this test rather than sailing through it.
    expect("rootKeyDigest is a stable SHA-256 prefix, pinned to a literal expectation",
           Orchestrator.rootKeyDigest("clawdline-root-key-fixture"), "237b1f8e")
    let clockRolledBack = equal.first.map {
        Orchestrator.workspaceBusyExtra($0, now: Date(timeIntervalSince1970: 0))
    } ?? [:]
    check("age_seconds never goes negative, even against a clock that rolled back before the "
          + "blocking task's own created time",
          clockRolledBack["age_seconds"] as? Int == 0)

    let sibling = task(holderID, root: firstRoot, claims: ["Sources"])
    let sameRoot = Orchestrator.claimsOverlaps(for: exact, among: [sibling])
    check("the same root keeps authority over its own overlapping graph",
          sameRoot.count == 1 && sameRoot.first?.sameRoot == true)
    let warned = Orchestrator.dispatchPayload(record: ["id": taskID], taskID: taskID,
                                              overlaps: [], claimsOverlaps: sameRoot)
    let warning = (warned["warnings"] as? [[String: Any]])?.first
    check("same-root overlap is admitted with a claims_overlap warning",
          warning?["code"] as? String == "claims_overlap"
              && warning?["task"] as? String == holderID
              && warning?["paths"] as? [String] == ["/repo/Sources"])
    let warnedAt = Orchestrator.dispatchPayload(record: ["id": taskID], taskID: taskID,
                                                overlaps: [], claimsOverlaps: sameRoot,
                                                now: Date(timeIntervalSince1970: 31))
    let timedWarning = (warnedAt["warnings"] as? [[String: Any]])?.first
    check("the claims_overlap warning is context-sufficient too",
          timedWarning?["age_seconds"] as? Int == 30
              && timedWarning?["root_key"] as? String == Orchestrator.rootKeyDigest(firstRoot))

    var parent = task("12121212-3434-5656-7878-909090909090",
                      root: firstRoot, state: .briefed, claims: ["elsewhere"])
    parent.depth = 1
    var child = task(taskID, root: nil, claims: ["Sources"])
    child.depth = 2
    child.parentTaskId = parent.id
    var otherChild = task(holderID, root: nil, claims: ["Sources"], rootLabel: nil)
    otherChild.depth = 2
    otherChild.parentTaskId = parent.id
    let inherited = Orchestrator.claimsOverlaps(for: child, among: [parent, otherChild])
    check("descendants inherit root identity and label for claims just as they do for L1",
          inherited.allSatisfy(\.sameRoot) && inherited.first?.rootLabel == "root label")

    var unknown = task("89898989-8989-8989-8989-898989898989", root: nil,
                       claims: ["Sources"])
    unknown.depth = 2
    unknown.parentTaskId = "99999999-9999-9999-9999-999999999999"
    let unknownRoot = Orchestrator.claimsOverlaps(for: unknown, among: [holder])
    let unknownReply = Orchestrator.dispatchPayload(record: ["id": taskID], taskID: taskID,
                                                    overlaps: [], claimsOverlaps: unknownRoot)
    let unknownWarning = (unknownReply["warnings"] as? [[String: Any]])?.first
    check("an unresolved root degrades to a typed warning instead of a hard blocker",
          unknownRoot.first?.blocks == false
              && unknownWarning?["code"] as? String == "claims_overlap_unknown_root")
    check("even inside an unknown-root pair, a resolvable blocker still reports its own root_key",
          unknownWarning?["root_key"] as? String == Orchestrator.rootKeyDigest(secondRoot))
    let unknownHolder = Orchestrator.claimsOverlaps(for: exact, among: [unknown])
    check("an unresolved blocking side also lacks hard-block authority",
          unknownHolder.first?.blocks == false
              && unknownHolder.first?.warning(for: taskID)["code"] as? String
                  == "claims_overlap_unknown_root")
    check("and when the blocker's own root cannot resolve, root_key is honestly absent",
          unknownHolder.first?.rootKey == nil
              && (unknownHolder.first?.warning(for: taskID)["root_key"] as? String) == nil)

    let terminals: [Orchestrator.State] = [.success, .failure, .timeout, .cancelled, .spawnFailed]
    for terminal in terminals {
        let ended = task(holderID, root: secondRoot, state: terminal, claims: ["Sources"])
        expect("\(terminal.rawValue) releases every claim",
               Orchestrator.claimsOverlaps(for: exact, among: [ended]).count, 0)
    }
    let timedOut = task(holderID, root: secondRoot, state: .timeout, claims: ["Sources"])
    check("a timeout completion line says the claims are free while the tab may still write",
          Orchestrator.timeoutClaimNotice(for: timedOut)
              .contains("claims released; child tab may still be writing"))
    let queuedHolder = task(holderID, root: secondRoot, claims: ["Sources"],
                            serialize: ["build"])
    expect("a serialized task reserves claims for its whole queued wait",
           Orchestrator.claimsOverlaps(for: exact, among: [queuedHolder]).count, 1)
    expect("a candidate combining claims and serialize still checks claims while queued",
           Orchestrator.claimsOverlaps(
               for: task(taskID, root: firstRoot, claims: ["Sources"], serialize: ["build"]),
               among: [queuedHolder]
           ).count, 1)
    let readOnly = task(taskID, root: firstRoot, claims: [])
    expect("a read-only task cannot conflict with a live lease",
           Orchestrator.claimsOverlaps(
               for: readOnly, among: [holder]
           ).count, 0)
    expect("a live lease cannot conflict with a read-only task either",
           Orchestrator.claimsOverlaps(for: exact, among: [readOnly]).count, 0)
    check("a read-only task holds no lease keys while retaining its declaration",
          readOnly.claimsDeclared && readOnly.claimKeys.isEmpty)

    let roundTrip = Orchestrator.task(from: Orchestrator.stored(exact))
    check("claims and their frozen comparison keys survive the registry",
          roundTrip?.claims == ["Sources"] && roundTrip?.claimKeys == ["/repo/Sources"])
    let readOnlyRoundTrip = Orchestrator.task(from: Orchestrator.stored(readOnly))
    var absent = Orchestrator.Task(id: "abababab-cdcd-efef-0101-232323232323",
                                   state: .briefed, kind: "custom", title: "unknown scope",
                                   assistant: .claude, projectDir: "/repo", timeoutMinutes: 30,
                                   created: Date(timeIntervalSince1970: 2),
                                   rootSessionId: firstRoot,
                                   secretHash: String(repeating: "0", count: 64))
    absent.claims = []
    let absentRoundTrip = Orchestrator.task(from: Orchestrator.stored(absent))
    check("the registry round-trip preserves empty versus absent claims",
          readOnlyRoundTrip?.claimsDeclared == true && readOnlyRoundTrip?.claims == []
              && absentRoundTrip?.claimsDeclared == false && absentRoundTrip?.claims == [])
    var damagedRecord = Orchestrator.stored(exact)
    damagedRecord["claims"] = ["/invalid-absolute-claim"]
    let damagedRoundTrip = Orchestrator.task(from: damagedRecord)
    check("a stored declaration filtered down to nothing becomes unknown, not read-only",
          damagedRoundTrip?.claims == [] && damagedRoundTrip?.claimsDeclared == false)
    let readOnlyRecord = Orchestrator.recordForTesting(readOnly)
    let absentRecord = Orchestrator.recordForTesting(absent)
    check("GET records show an empty array while omitting an absent declaration",
          readOnlyRecord["claims"] as? [String] == [] && absentRecord["claims"] == nil)

    let fm = FileManager.default
    let physicalRoot = "/private/tmp/clawdline-claim-freeze-\(UUID().uuidString)"
    let physicalURL = URL(fileURLWithPath: physicalRoot, isDirectory: true)
    try! fm.createDirectory(at: physicalURL.appendingPathComponent("link", isDirectory: true),
                            withIntermediateDirectories: true)
    defer { try? fm.removeItem(at: physicalURL) }
    let claim = "link/created-later.txt"
    let beforeCreation = Orchestrator.freezeClaims([claim], projectDir: physicalRoot)
    fm.createFile(atPath: physicalURL.appendingPathComponent(claim).path,
                  contents: Data("made later".utf8))
    let afterCreation = Orchestrator.freezeClaims([claim], projectDir: physicalRoot)
    expect("a frozen claim key does not change when its target file is created",
           afterCreation, beforeCreation)
    expect("literal joining normalises dot and empty relative components",
           Orchestrator.freezeClaims(["./link//created-later.txt"], projectDir: physicalRoot),
           beforeCreation)

    let tmpSpelling = physicalRoot.replacingOccurrences(of: "/private/tmp/", with: "/tmp/")
    let tmpHolder = task(holderID, dir: physicalRoot, root: secondRoot,
                         claims: ["future/output.txt"])
    let tmpCandidate = task(taskID, dir: tmpSpelling, root: firstRoot,
                            claims: ["future/output.txt"])
    check("/tmp and /private/tmp project roots reserve one canonical namespace",
          Orchestrator.claimsOverlaps(for: tmpCandidate, among: [tmpHolder]).first?.blocks == true)
}

group("a released claim key no longer counts as held") {
    let dir = "/repo"
    let releasedAt = Date(timeIntervalSince1970: 5)
    var full = Orchestrator.Task(id: "d0d0d0d0-d0d0-d0d0-d0d0-d0d0d0d0d0d0", state: .briefed,
                                 kind: "custom", title: "releaser", assistant: .claude,
                                 projectDir: dir, timeoutMinutes: 30,
                                 created: Date(timeIntervalSince1970: 1),
                                 rootSessionId: "root-full",
                                 claims: ["Sources/A.swift", "Sources/B.swift"],
                                 claimsDeclared: true, secretHash: String(repeating: "0", count: 64))
    full.claimKeys = Orchestrator.freezeClaims(full.claims, projectDir: dir)
    check("before any release, both claims are active",
          full.activeClaimKeys == ["\(dir)/Sources/A.swift", "\(dir)/Sources/B.swift"])
    var partiallyReleased = full
    partiallyReleased.releasedClaims = [Orchestrator.ReleasedClaim(path: "\(dir)/Sources/A.swift",
                                                                    releasedAt: releasedAt)]
    check("a partial release narrows active keys to what remains held",
          partiallyReleased.activeClaimKeys == ["\(dir)/Sources/B.swift"])
    var fullyReleased = full
    fullyReleased.releasedClaims = full.claimKeys.map {
        Orchestrator.ReleasedClaim(path: $0, releasedAt: releasedAt)
    }
    check("a full release leaves nothing active", fullyReleased.activeClaimKeys.isEmpty)

    var withUntouched = partiallyReleased
    withUntouched.state = .success
    withUntouched.untouchedClaims = ["Sources/B.swift"]
    let roundTrip = Orchestrator.task(from: Orchestrator.stored(withUntouched))
    expect("released claims — path and when — survive a store/load round trip",
           roundTrip?.releasedClaims, withUntouched.releasedClaims)
    expect("untouched claims survive a store/load round trip",
           roundTrip?.untouchedClaims, withUntouched.untouchedClaims)
    check("the round-tripped task still reports the same active claim keys",
          roundTrip?.activeClaimKeys == ["\(dir)/Sources/B.swift"])

    var candidate = Orchestrator.Task(id: "e0e0e0e0-e0e0-e0e0-e0e0-e0e0e0e0e0e0", state: .queued,
                                      kind: "custom", title: "candidate", assistant: .claude,
                                      projectDir: dir, timeoutMinutes: 30,
                                      created: Date(timeIntervalSince1970: 2),
                                      rootSessionId: "root-candidate",
                                      claims: ["Sources/A.swift"], claimsDeclared: true,
                                      secretHash: String(repeating: "0", count: 64))
    candidate.claimKeys = Orchestrator.freezeClaims(candidate.claims, projectDir: dir)
    check("a still-held claim keeps blocking a cross-root candidate",
          Orchestrator.claimsOverlaps(for: candidate, among: [full]).first?.blocks == true)
    check("the same candidate is free once its path was released",
          Orchestrator.claimsOverlaps(for: candidate, among: [partiallyReleased]).isEmpty)
}

group("the claims/release route frees paths early, guards terminal tasks, and is idempotent") {
    defer { Orchestrator.forget() }
    Orchestrator.forget()
    let dir = "/repo"
    func fresh(state: Orchestrator.State) -> Orchestrator.Task {
        var made = Orchestrator.Task(id: taskID, state: state, kind: "custom", title: "releaser",
                                     assistant: .claude, projectDir: dir, timeoutMinutes: 30,
                                     created: Date(timeIntervalSince1970: 1),
                                     claims: ["Sources/A.swift", "Sources/B.swift"],
                                     claimsDeclared: true,
                                     secretHash: String(repeating: "0", count: 64))
        made.claimKeys = Orchestrator.freezeClaims(made.claims, projectDir: dir)
        return made
    }

    if case .refused(let status, let code, _, _) = Orchestrator.releaseClaims(
        taskID: "00000000-0000-0000-0000-000000000000", paths: []) {
        check("releasing an unknown task is a 404", status == 404 && code == "not_found")
    } else {
        check("releasing an unknown task is a 404", false, "it answered ok")
    }

    Orchestrator.holdScheduleTaskForTesting(fresh(state: .success))
    if case .refused(let status, let code, _, _) = Orchestrator.releaseClaims(
        taskID: taskID, paths: []) {
        check("a terminal task refuses release; its claims are already gone",
              status == 409 && code == "already_done")
    } else {
        check("a terminal task refuses release; its claims are already gone", false, "it answered ok")
    }

    func releasedPaths(_ reply: Orchestrator.Reply) -> [String] {
        guard case .ok(let body) = reply,
              let record = body["task"] as? [String: Any],
              let rows = record["released_claims"] as? [[String: Any]] else { return [] }
        return rows.compactMap { $0["path"] as? String }.sorted()
    }

    Orchestrator.holdScheduleTaskForTesting(fresh(state: .briefed))
    let partial = Orchestrator.releaseClaims(taskID: taskID, paths: ["Sources/A.swift"],
                                             now: Date(timeIntervalSince1970: 21))
    check("a partial release frees exactly the named path",
          releasedPaths(partial) == ["\(dir)/Sources/A.swift"])
    guard case .ok(let partialBody) = partial,
          let partialRecord = partialBody["task"] as? [String: Any],
          let partialRow = (partialRecord["released_claims"] as? [[String: Any]])?.first else {
        check("the release row carries when it happened", false)
        Orchestrator.forget()
        // Nothing further can be asked of a release that did not come back as expected.
        return
    }
    expect("and records exactly when", partialRow["released_at"] as? Int, 21)

    let reReleased = Orchestrator.releaseClaims(taskID: taskID, paths: ["Sources/A.swift"],
                                                now: Date(timeIntervalSince1970: 99))
    check("releasing the same path again is a no-op, not a second timestamp",
          releasedPaths(reReleased) == ["\(dir)/Sources/A.swift"]
              && (Orchestrator.record(id: taskID)?["released_claims"] as? [[String: Any]])?.count == 1)

    let full = Orchestrator.releaseClaims(taskID: taskID, paths: [])
    check("an empty paths array releases everything still held",
          releasedPaths(full) == ["\(dir)/Sources/A.swift", "\(dir)/Sources/B.swift"])
}

group("release compares ancestor and descendant paths like arbitration does, validates paths, "
      + "and refuses a queued task") {
    defer { Orchestrator.forget() }
    let dir = "/repo"
    func fresh(claims: [String], state: Orchestrator.State = .briefed) -> Orchestrator.Task {
        var made = Orchestrator.Task(id: taskID, state: state, kind: "custom",
                                     title: "ancestor releaser", assistant: .claude,
                                     projectDir: dir, timeoutMinutes: 30,
                                     created: Date(timeIntervalSince1970: 1),
                                     claims: claims, claimsDeclared: true,
                                     secretHash: String(repeating: "0", count: 64))
        made.claimKeys = Orchestrator.freezeClaims(made.claims, projectDir: dir)
        return made
    }
    func releasedPaths(_ reply: Orchestrator.Reply) -> [String] {
        guard case .ok(let body) = reply,
              let record = body["task"] as? [String: Any],
              let rows = record["released_claims"] as? [[String: Any]] else { return [] }
        return rows.compactMap { $0["path"] as? String }.sorted()
    }

    Orchestrator.forget()
    Orchestrator.holdScheduleTaskForTesting(fresh(claims: ["Sources/A.swift", "Sources/B.swift"]))
    let ancestor = Orchestrator.releaseClaims(taskID: taskID, paths: ["Sources"])
    check("releasing an ancestor spelling frees every declared claim key under it",
          releasedPaths(ancestor) == ["\(dir)/Sources/A.swift", "\(dir)/Sources/B.swift"])

    Orchestrator.forget()
    Orchestrator.holdScheduleTaskForTesting(fresh(claims: ["Sources"]))
    let descendant = Orchestrator.releaseClaims(taskID: taskID,
                                                paths: ["Sources/Orchestrator.swift"])
    check("releasing a descendant spelling frees the whole directory-shaped claim it lives under, "
          + "because that claim is one atomic reservation",
          releasedPaths(descendant) == ["\(dir)/Sources"])

    Orchestrator.forget()
    Orchestrator.holdScheduleTaskForTesting(fresh(claims: ["Sources/A.swift"]))
    let unrelated = Orchestrator.releaseClaims(taskID: taskID, paths: ["Tests/main.swift"])
    if case .ok = unrelated {
        check("a completely unrelated path is silently a no-op, not an error",
              releasedPaths(unrelated).isEmpty)
    } else {
        check("a completely unrelated path is silently a no-op, not an error", false, "it refused")
    }

    Orchestrator.forget()
    Orchestrator.holdScheduleTaskForTesting(fresh(claims: ["Sources/A.swift"]))
    if case .refused(let status, let code, let message, _) = Orchestrator.releaseClaims(
        taskID: taskID, paths: ["Sources/../../etc/passwd"]) {
        check("a .. component in a release path is refused, not silently frozen into a claim key",
              status == 400 && code == "bad_request" && message.contains(".."))
    } else {
        check("a .. component in a release path is refused, not silently frozen into a claim key",
              false, "it answered ok")
    }

    Orchestrator.forget()
    Orchestrator.holdScheduleTaskForTesting(fresh(claims: ["Sources/A.swift"], state: .queued))
    if case .refused(let status, let code, let message, _) = Orchestrator.releaseClaims(
        taskID: taskID, paths: []) {
        check("a queued task cannot release its claims early — it has not started writing yet",
              status == 409 && code == "not_started" && message.lowercased().contains("cancel"))
    } else {
        check("a queued task cannot release its claims early — it has not started writing yet",
              false, "it answered ok")
    }
}

group("the claims/release HTTP route is orchestrator-token gated") {
    defer { Orchestrator.forget() }
    Orchestrator.forget()
    let dir = "/repo"
    let releaseTaskID = "01234567-89ab-cdef-0123-456789abcdef"
    var made = Orchestrator.Task(id: releaseTaskID, state: .briefed, kind: "custom",
                                 title: "http releaser", assistant: .claude, projectDir: dir,
                                 timeoutMinutes: 30, created: Date(timeIntervalSince1970: 1),
                                 claims: ["Sources/A.swift"], claimsDeclared: true,
                                 secretHash: String(repeating: "0", count: 64))
    made.claimKeys = Orchestrator.freezeClaims(made.claims, projectDir: dir)
    Orchestrator.holdScheduleTaskForTesting(made)

    let anonymous = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/tasks/\(releaseTaskID)/claims/release",
        body: "{\"paths\":[\"Sources/A.swift\"]}"))
    expect("no credential at all stops at the door", anonymous.status, 401)

    let phone = RemoteAuth.addDevice(name: "a phone", caps: [.read, .send])
    let paired = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/tasks/\(releaseTaskID)/claims/release",
        headers: ["Authorization": "Bearer \(phone.token)"],
        body: "{\"paths\":[\"Sources/A.swift\"]}"))
    expect("a paired device cannot substitute for the orchestrator token", paired.status, 403)

    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let authed = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/tasks/\(releaseTaskID)/claims/release", headers: auth,
        body: "{\"paths\":[\"Sources/A.swift\"]}"))
    expect("the orchestrator token reaches the route", authed.status, 200)
    let body = (try? JSONSerialization.jsonObject(with: authed.body)) as? [String: Any]
    let record = body?["task"] as? [String: Any]
    let released = record?["released_claims"] as? [[String: Any]]
    check("the HTTP body reflects the release",
          released?.first?["path"] as? String == "\(dir)/Sources/A.swift")
}
}
