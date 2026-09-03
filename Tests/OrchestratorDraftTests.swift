import Foundation

/// The admission layer, asked the two questions it exists to answer: *is this refused, and with
/// which typed code*, and *what did a body decode into*.
///
/// Every function here is pure over its arguments — no registry, no lock, no terminal — so each
/// group is a table of inputs beside the answer each one is owed. A refusal is compared by
/// `status` and `code` rather than by prose, because those two are the wire contract and the
/// message is not; where a message carries a fact a caller acts on, that fact is checked on its
/// own.
///
/// **The rows that produce *no* refusal matter as much as the ones that do.** A guard that
/// refuses everything passes a table listing only refusals, and three of the four functions below
/// exist precisely to leave the other modes alone.
///
/// `draft(from:)` reads files a root wrote, so its defaults, truncations and absent-versus-empty
/// distinctions are a compatibility contract with task directories already on disk. They are
/// listed here field by field rather than round-tripped, because a field the parser forgets and
/// nothing reads round-trips perfectly.

// MARK: - Reading one answer

/// A refusal flattened to the two facts a caller acts on, or `"none"` when the function declined
/// to refuse. These four functions only ever return `.refused` or nil, so `"ok"` is a shape that
/// should never appear and is spelled out rather than folded into one of the other two.
private func refusalAnswer(_ reply: Orchestrator.Reply?) -> String {
    guard let reply else { return "none" }
    guard case .refused(let status, let code, _, _) = reply else { return "ok" }
    return "\(status) \(code)"
}

private func refusalMessage(_ reply: Orchestrator.Reply?) -> String {
    guard case .refused(_, _, let message, _)? = reply else { return "«no refusal»" }
    return message
}

private func refusalExtra(_ reply: Orchestrator.Reply?) -> [String: Any] {
    guard case .refused(_, _, _, let extra)? = reply else { return [:] }
    return extra
}

/// What a draft outcome is, in one comparable string: the refusal text, or `ok`.
private func draftAnswer(_ outcome: OrchestratorDraft.DraftOutcome) -> String {
    switch outcome {
    case .ok: return "ok"
    case .bad(let why): return why
    }
}

private func draftValue(_ outcome: OrchestratorDraft.DraftOutcome) -> OrchestratorDraft.Draft? {
    guard case .ok(let made) = outcome else { return nil }
    return made
}

// MARK: - What a draft is made of

private let absentField = "«nil»"

private func text(_ value: String?) -> String { value ?? absentField }

/// Listed rather than derived, so a field added to `Draft` and not to the parser leaves a gap
/// this file has to be edited to close.
private func draftFields(_ draft: OrchestratorDraft.Draft?) -> [(String, String)] {
    guard let draft else { return [] }
    return [("id", draft.id),
            ("kind", draft.kind),
            ("assistant", draft.assistant.rawValue),
            ("model", text(draft.model)),
            ("reasoning_effort", text(draft.reasoningEffort?.rawValue)),
            ("permission", text(draft.permission?.rawValue)),
            ("project_dir", draft.projectDir),
            ("title", draft.title),
            ("instructions", draft.instructions),
            ("timeout_minutes", String(draft.timeoutMinutes)),
            ("root.session_id", text(draft.rootSessionId)),
            ("root.assistant", text(draft.rootAssistant?.rawValue)),
            ("root.label", text(draft.rootLabel)),
            ("root.parent_task", text(draft.parentTaskId)),
            ("root.poll_only", String(draft.pollOnly)),
            ("plan", text(draft.plan)),
            ("graph.id", text(draft.graph?.id)),
            ("graph.current_node", text(draft.graph?.currentNode)),
            ("serialize", draft.serialize.joined(separator: "|")),
            ("claims", draft.claims.joined(separator: "|")),
            ("claims_declared", String(draft.claimsDeclared)),
            ("isolation", draft.isolation.rawValue),
            ("isolation_base", text(draft.isolationBase)),
            ("ignore_quota", String(draft.ignoreQuota)),
            ("attach_session", text(draft.attachSessionId))]
}

private func compareDraftFields(_ label: String,
                               _ got: [(String, String)],
                               _ want: [(String, String)]) {
    for (index, field) in want.enumerated() {
        let observed = index < got.count ? got[index].1 : "«the body did not decode»"
        expect("\(label) reads \(field.0)", observed, field.1)
    }
}

// MARK: - The suite

func runOrchestratorDraftTests() {

// Fixed rather than generated: a fixture that changes between runs cannot be quoted in a failure
// report, and `isTaskID` wants lower-case hex.
// Hex letters on purpose: an all-digit id makes `.uppercased()` the identity, and the two rows
// below that are about case would pass without testing anything.
let taskID = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
let parentID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
let graphID = "99999999-8888-7777-6666-555555555555"
// Every `draft(from:)` row below passes this stub instead of touching the filesystem, so the
// only project_dir facts under test are the ones the parser itself decides.
let everyPathIsADirectory: (String) -> Bool = { _ in true }

group("a root id that is really a terminal id is refused, and only when the evidence agrees") {
    func evidence(_ source: String, terminal: String, canonical: String,
                  _ assistant: Assistant = .claude) -> OrchestratorDraft.RootIdentityEvidence {
        OrchestratorDraft.RootIdentityEvidence(
            source: source, terminalID: terminal, canonicalSessionID: canonical,
            assistant: assistant)
    }
    let watched = evidence("active_terminal", terminal: "terminal-1", canonical: "conversation-1")
    let bound = evidence("coordinator_binding", terminal: "terminal-1",
                         canonical: "conversation-1")
    let refused = "422 root_identity_is_terminal"

    let identityRows: [(String, String?, [OrchestratorDraft.RootIdentityEvidence], String)] = [
        ("a detached dispatch claims nothing", nil, [watched], "none"),
        ("an empty claim is not a claim", "", [watched], "none"),
        ("no evidence is not evidence of a swap", "terminal-1", [], "none"),
        ("one watched terminal proves the swap", "terminal-1", [watched], refused),
        ("two sources agreeing on one tuple still prove it", "terminal-1", [watched, bound],
         refused),
        ("two canonical ids for one terminal are inconclusive", "terminal-1",
         [watched, evidence("other", terminal: "terminal-1", canonical: "conversation-2")],
         "none"),
        ("one canonical id under two assistants is inconclusive", "terminal-1",
         [watched, evidence("other", terminal: "terminal-1", canonical: "conversation-1", .codex)],
         "none"),
        ("an id that is already canonical is left alone", "conversation-1",
         [evidence("active_terminal", terminal: "terminal-1", canonical: "conversation-1")],
         "none"),
        ("evidence about another terminal says nothing about this one", "terminal-9", [watched],
         "none"),
    ]
    for (label, claimed, evidence, want) in identityRows {
        expect(label, refusalAnswer(
            OrchestratorDraft.rootIdentityRefusal(claimed: claimed, evidence: evidence)), want)
    }

    // The correction is only useful if the caller can act on it without a second request, so the
    // three identity facts and the provenance are checked rather than the prose around them.
    let extra = refusalExtra(OrchestratorDraft.rootIdentityRefusal(
        claimed: "terminal-1", evidence: [bound, watched]))
    expect("the refusal echoes what was supplied",
           extra["supplied_root_session_id"] as? String, "terminal-1")
    expect("and names the conversation id to resend with",
           extra["canonical_root_session_id"] as? String, "conversation-1")
    expect("and the assistant that was observed rather than the one claimed",
           extra["canonical_root_assistant"] as? String, "claude")
    expect("and lists its sources in a stable order",
           extra["evidence"] as? [String], ["active_terminal", "coordinator_binding"])
}

group("the dispatch door sorts owned children from detached automation before anything opens") {
    // Owner requirement: only a nameless owner on the owned route is refused.
    let ownerRows: [(String, String?, Bool, String)] = [
        ("an owned child with no owner is refused", nil, false, "422 root_session_required"),
        ("detached automation is allowed to be ownerless", nil, true, "none"),
        ("a named owner passes", "conversation-1", false, "none"),
        ("a named owner that also polls is not this function's business", "conversation-1", true,
         "none"),
    ]
    for (label, sessionID, pollOnly, want) in ownerRows {
        expect(label, refusalAnswer(OrchestratorDraft.rootSessionRequirementRefusal(
            sessionID: sessionID, pollOnly: pollOnly)), want)
    }

    // Route selection: each route accepts exactly one shape and names the other one.
    let ownedRoute = "422 detached_route_required"
    let detachedRoute = "422 detached_task_required"
    let doorRows: [(String, String?, Bool, Bool, String)] = [
        ("the owned route takes a named owner", "conversation-1", false, false, "none"),
        ("the owned route takes an unresolved owner and refuses it later", nil, false, false,
         "none"),
        ("the owned route never takes poll-only", "conversation-1", true, false, ownedRoute),
        ("not even from a caller with no owner at all", nil, true, false, ownedRoute),
        ("the detached route takes exactly ownerless poll-only", nil, true, true, "none"),
        ("the detached route refuses a named owner", "conversation-1", true, true, detachedRoute),
        ("and refuses an owner who forgot to say poll-only", "conversation-1", false, true,
         detachedRoute),
        ("and refuses a caller who is neither", nil, false, true, detachedRoute),
    ]
    for (label, sessionID, pollOnly, detached, want) in doorRows {
        expect(label, refusalAnswer(OrchestratorDraft.dispatchDoorRefusal(
            sessionID: sessionID, pollOnly: pollOnly,
            allowDetachedAutomation: detached)), want)
    }

    // Both halves of the owner tuple, or neither: an omitted assistant must not inherit a default.
    let assistantRows: [(String, String?, Bool, Assistant?, String)] = [
        ("a named owner with no assistant is refused", "conversation-1", false, nil,
         "422 root_assistant_required"),
        ("a named owner that says claude passes", "conversation-1", false, .claude, "none"),
        ("a named owner that says codex passes", "conversation-1", false, .codex, "none"),
        ("detached automation is ownerless on purpose", nil, true, nil, "none"),
        ("and stays ownerless even when it names an assistant", nil, true, .claude, "none"),
        ("an unresolved owner is the other function's refusal", nil, false, nil, "none"),
        ("a poll-only caller with an owner is the door's refusal", "conversation-1", true, nil,
         "none"),
        ("and remains so when it names an assistant", "conversation-1", true, .codex, "none"),
    ]
    for (label, sessionID, pollOnly, assistant, want) in assistantRows {
        expect(label, refusalAnswer(OrchestratorDraft.rootAssistantRequirementRefusal(
            sessionID: sessionID, pollOnly: pollOnly, assistant: assistant)), want)
    }

    // A typed code tells a program what happened; the message is what tells a person where to go
    // next, and each of these three points at a different door.
    check("the owner refusal sends the caller to whoami",
          refusalMessage(OrchestratorDraft.rootSessionRequirementRefusal(
            sessionID: nil, pollOnly: false)).contains("GET /v1/orchestrator/whoami"))
    check("the owned-route refusal names the detached route",
          refusalMessage(OrchestratorDraft.dispatchDoorRefusal(
            sessionID: nil, pollOnly: true, allowDetachedAutomation: false))
            .contains("POST /v1/orchestrator/detached-tasks"))
    check("the detached-route refusal names the owned route",
          refusalMessage(OrchestratorDraft.dispatchDoorRefusal(
            sessionID: "conversation-1", pollOnly: true, allowDetachedAutomation: true))
            .contains("POST /v1/orchestrator/tasks"))
}

group("a task.json is read whole, or refused naming the field that was wrong") {
    /// The body every row below starts from, so each row differs from a valid dispatch in exactly
    /// one place and the refusal it gets can only be about that place.
    func body(_ changes: [String: Any?] = [:]) -> [String: Any] {
        var obj: [String: Any] = [
            "clawdline_protocol": 1,
            "task_id": taskID,
            "assistant": "claude",
            "project_dir": "/Users/me/code/clawdline",
            "instructions": "do the thing",
        ]
        for (key, value) in changes {
            if let value { obj[key] = value } else { obj.removeValue(forKey: key) }
        }
        return obj
    }

    let rows: [(String, [String: Any])] = [
        ("clawdline_protocol must be 1", body(["clawdline_protocol": nil])),
        ("clawdline_protocol must be 1", body(["clawdline_protocol": 2])),
        ("task_id must be a lowercase UUID and match the dispatch", body(["task_id": parentID])),
        ("task_id must be a lowercase UUID and match the dispatch",
         body(["task_id": taskID.uppercased()])),
        ("assistant must be claude or codex", body(["assistant": nil])),
        ("assistant must be claude or codex", body(["assistant": "gemini"])),
        ("project_dir must be an absolute path to a directory", body(["project_dir": nil])),
        ("project_dir must be an absolute path to a directory", body(["project_dir": "code"])),
        ("instructions must be non-empty and at most 16 KiB", body(["instructions": nil])),
        ("instructions must be non-empty and at most 16 KiB", body(["instructions": ""])),
        ("instructions must be non-empty and at most 16 KiB",
         body(["instructions": String(repeating: "x", count: 16_385)])),
        ("model must be a model name: lower-case letters, digits, . _ -, "
            + "at most 64 characters", body(["model": "Opus 5"])),
        ("reasoning_effort is only valid when assistant is codex",
         body(["reasoning_effort": "high"])),
        ("reasoning_effort must be one of: high, xhigh",
         body(["assistant": "codex", "reasoning_effort": "medium"])),
        ("plan must be at most 4 KiB",
         body(["plan": String(repeating: "p", count: 4_097)])),
        ("graph must be an object", body(["graph": "not an object"])),
        ("isolation must be one of: none, worktree", body(["isolation": "sandbox"])),
        ("isolation_base is only valid when isolation is worktree",
         body(["isolation_base": "HEAD"])),
        ("isolation_base must be a 1–200 character Git revision using letters, digits, "
            + ". _ / - or ~; it cannot begin with - or contain ..",
         body(["isolation": "worktree", "isolation_base": "--upload-pack=evil"])),
        ("serialize must be an array of at most 4 tokens", body(["serialize": "build"])),
        ("serialize must contain at most 4 tokens",
         body(["serialize": ["a", "b", "c", "d", "e"]])),
        ("serialize[0] must be a string", body(["serialize": [7]])),
        ("serialize[0] must be 1–64 lower-case letters, digits, . _ -, and not begin with -",
         body(["serialize": ["Build"]])),
        ("serialize[1] duplicates build", body(["serialize": ["build", "build"]])),
        ("claims must be an array of 0–32 relative POSIX paths", body(["claims": "Sources"])),
        ("claims must contain 0–32 paths",
         body(["claims": (0..<33).map { "file\($0).swift" }])),
        ("claims[0] must be a string", body(["claims": [7]])),
        ("claims[0] must be 1–1024 characters", body(["claims": [""]])),
        ("claims[0] must be relative to project_dir", body(["claims": ["/etc/passwd"]])),
        ("claims[0] must not contain a .. component", body(["claims": ["Sources/../etc"]])),
        ("claims[0] must be a POSIX path without NUL", body(["claims": ["Sources/a\u{0}b"]])),
        ("claims[1] duplicates a.swift", body(["claims": ["a.swift", "a.swift"]])),
        ("permission_mode must be one of: ask, edits, full",
         body(["permission_mode": "yolo"])),
        ("attach_session must be a non-empty session id of at most 512 characters",
         body(["attach_session": ""])),
        ("attach_session must be a non-empty session id of at most 512 characters",
         body(["attach_session": String(repeating: "s", count: 513)])),
        ("timeout_minutes must be 1…240", body(["timeout_minutes": 0])),
        ("timeout_minutes must be 1…240", body(["timeout_minutes": 241])),
        ("root.assistant must be claude or codex",
         body(["root": ["session_id": "conversation-1", "assistant": "gemini"]])),
        ("root.poll_only must be true or false",
         body(["root": ["session_id": NSNull(),
                        "poll_only": "yes"] as [String: Any]])),
        ("root.poll_only is only valid when root.session_id is null",
         body(["root": ["session_id": "conversation-1",
                        "poll_only": true] as [String: Any]])),
    ]
    for (want, obj) in rows {
        expect("a body refused with: \(want)",
               draftAnswer(OrchestratorDraft.draft(from: obj, expecting: taskID,
                                                   isDirectory: everyPathIsADirectory)),
               want)
    }

    // The one refusal that cannot come from the body alone: a project_dir that parses and is not
    // there. The stub is what makes the other forty rows filesystem-free, so it is worth one row
    // proving the stub is consulted at all.
    expect("a project_dir that is not a directory is refused by the filesystem seam",
           draftAnswer(OrchestratorDraft.draft(from: body(), expecting: taskID,
                                               isDirectory: { _ in false })),
           "project_dir must be an absolute path to a directory")
}

group("a fully populated task.json and a minimal one both arrive intact") {
    let full: [String: Any] = [
        "clawdline_protocol": 1,
        "task_id": taskID,
        "kind": "review",
        "assistant": "codex",
        "model": "gpt-5.1-codex",
        "reasoning_effort": "xhigh",
        "permission_mode": "full",
        "project_dir": "/Users/me/code/clawdline",
        "title": "Refactor: extract the task-draft parsing",
        "instructions": "read the block and move it",
        "timeout_minutes": 180,
        "plan": "  cut, then review  ",
        "graph": [
            "id": graphID,
            "destination": "the block owns its own file",
            "current_node": "cut",
            "nodes": [["id": "cut", "title": "Extract the block", "kind": "delivery",
                       "depends_on": [String](),
                       "acceptance": ["only pure declarations move"]] as [String: Any]],
            "unknowns": ["whether a helper reaches shared state"],
            "out_of_scope": ["the lock and its collections"],
        ] as [String: Any],
        "serialize": ["build", "suite"],
        "claims": ["Sources/Orchestrator.swift", "Tests/main.swift"],
        "isolation": "worktree",
        "isolation_base": "d97d0afb",
        "ignore_quota": true,
        "attach_session": "session-7",
        "root": ["session_id": "conversation-1", "assistant": "claude",
                 "label": "clawdline root", "parent_task": parentID] as [String: Any],
    ]
    compareDraftFields(
        "a fully populated body",
        draftFields(draftValue(OrchestratorDraft.draft(from: full, expecting: taskID,
                                                       isDirectory: everyPathIsADirectory))),
        [("id", taskID),
         ("kind", "review"),
         ("assistant", "codex"),
         ("model", "gpt-5.1-codex"),
         ("reasoning_effort", "xhigh"),
         ("permission", "full"),
         ("project_dir", "/Users/me/code/clawdline"),
         ("title", "Refactor: extract the task-draft parsing"),
         ("instructions", "read the block and move it"),
         ("timeout_minutes", "180"),
         ("root.session_id", "conversation-1"),
         ("root.assistant", "claude"),
         ("root.label", "clawdline root"),
         ("root.parent_task", parentID),
         ("root.poll_only", "false"),
         ("plan", "cut, then review"),
         ("graph.id", graphID),
         ("graph.current_node", "cut"),
         ("serialize", "build|suite"),
         ("claims", "Sources/Orchestrator.swift|Tests/main.swift"),
         ("claims_declared", "true"),
         ("isolation", "worktree"),
         ("isolation_base", "d97d0afb"),
         ("ignore_quota", "true"),
         ("attach_session", "session-7")])

    // The five keys above are the whole of what a task.json must carry. Everything else has a
    // default, and those defaults are the contract with every root that omits a field.
    let minimal: [String: Any] = [
        "clawdline_protocol": 1,
        "task_id": taskID,
        "assistant": "claude",
        "project_dir": "/Users/me/code/clawdline",
        "instructions": "do the thing",
    ]
    compareDraftFields(
        "a minimal body",
        draftFields(draftValue(OrchestratorDraft.draft(from: minimal, expecting: taskID,
                                                       isDirectory: everyPathIsADirectory))),
        [("id", taskID),
         ("kind", "custom"),
         ("assistant", "claude"),
         ("model", absentField),
         ("reasoning_effort", absentField),
         ("permission", absentField),
         ("project_dir", "/Users/me/code/clawdline"),
         ("title", "task"),
         ("instructions", "do the thing"),
         ("timeout_minutes", "30"),
         ("root.session_id", absentField),
         ("root.assistant", absentField),
         ("root.label", absentField),
         ("root.parent_task", absentField),
         ("root.poll_only", "false"),
         ("plan", absentField),
         ("graph.id", absentField),
         ("graph.current_node", absentField),
         ("serialize", ""),
         ("claims", ""),
         ("claims_declared", "false"),
         ("isolation", "none"),
         ("isolation_base", absentField),
         ("ignore_quota", "false"),
         ("attach_session", absentField)])
}

group("the shapes an older root still writes are the shapes this still reads") {
    func made(_ changes: [String: Any?]) -> OrchestratorDraft.Draft? {
        var obj: [String: Any] = [
            "clawdline_protocol": 1,
            "task_id": taskID,
            "assistant": "claude",
            "project_dir": "/Users/me/code/clawdline",
            "instructions": "do the thing",
        ]
        for (key, value) in changes {
            if let value { obj[key] = value } else { obj.removeValue(forKey: key) }
        }
        return draftValue(OrchestratorDraft.draft(from: obj, expecting: taskID,
                                                  isDirectory: everyPathIsADirectory))
    }

    // A task file written before `root` existed decodes as an ownerless one rather than failing.
    expect("a body with no root object at all still decodes",
           text(made([:])?.rootSessionId), absentField)
    expect("and is not turned into a poll-only dispatch by the omission",
           made([:])?.pollOnly, false)
    // `root.assistant` is explicitly nullable on the wire, and a null is not a bad value.
    expect("an explicit null root.assistant is absent, not invalid",
           text(made(["root": ["session_id": "conversation-1",
                               "assistant": NSNull()] as [String: Any]])?
               .rootAssistant?.rawValue),
           absentField)
    expect("an empty root.session_id is absent rather than an owner named the empty string",
           text(made(["root": ["session_id": ""]])?.rootSessionId), absentField)
    expect("a root label longer than the field is truncated rather than refused",
           made(["root": ["session_id": "conversation-1",
                          "label": String(repeating: "L", count: 200)]])?.rootLabel?.count,
           120)
    expect("a parent_task that is not a task id is dropped rather than trusted",
           text(made(["root": ["session_id": "conversation-1",
                               "parent_task": "not-a-task"]])?.parentTaskId),
           absentField)
    expect("and one that is a task id survives",
           text(made(["root": ["session_id": "conversation-1",
                               "parent_task": parentID]])?.parentTaskId),
           parentID)

    // Kind and title are free text a root chose, so they are bounded here rather than refused.
    expect("an empty kind falls back to custom", made(["kind": ""])?.kind, "custom")
    expect("a kind longer than the field is truncated",
           made(["kind": String(repeating: "k", count: 60)])?.kind.count, 40)
    expect("a title longer than the field is truncated",
           made(["title": String(repeating: "t", count: 300)])?.title.count, 200)

    // A plan that is only whitespace is not a plan; the distinction reaches the briefing.
    expect("a whitespace-only plan is no plan", text(made(["plan": "   \n  "])?.plan),
           absentField)
    expect("and a plan with whitespace around it keeps only its text",
           text(made(["plan": "  cut first  "])?.plan), "cut first")

    // Absent claims and an empty claims array are deliberately different facts: the second one
    // positively says the task writes nothing.
    expect("absent claims leave the declaration unmade", made([:])?.claimsDeclared, false)
    expect("an empty claims array is a declaration that this task writes nothing",
           made(["claims": [String]()])?.claimsDeclared, true)
    expect("and it still carries no paths", made(["claims": [String]()])?.claims.count, 0)

    // `ignore_quota` is an opt-in, so anything that is not a true boolean leaves it off.
    expect("ignore_quota defaults to off", made([:])?.ignoreQuota, false)
    expect("ignore_quota is honoured when it is a real boolean",
           made(["ignore_quota": true])?.ignoreQuota, true)
    expect("and a non-boolean ignore_quota is off rather than a refusal",
           made(["ignore_quota": "true"])?.ignoreQuota, false)
}

group("a task id is a lowercase UUID, and a task secret is 32 bytes of lower-case hex") {
    let idRows: [(String, String, Bool)] = [
        ("a lowercase UUID is a task id", taskID, true),
        ("an upper-case one is not", taskID.uppercased(), false),
        ("nor is one character short", String(taskID.dropLast()), false),
        ("nor one character long", taskID + "1", false),
        // Both of these are the right length on purpose: a row that is also the wrong length
        // would go red for the length rule and prove nothing about the alphabet.
        ("nor one carrying a non-hex letter", "g" + String(taskID.dropFirst()), false),
        ("nor one carrying a path separator", String(taskID.dropLast()) + "/", false),
        ("nor the empty string", "", false),
        // Not a defect being fixed here: the check is length plus alphabet, and thirty-six
        // dashes satisfy both. It is written down so that a later tightening is a deliberate
        // change to a wire contract rather than a silent one.
        ("and thirty-six dashes pass it, which is what the alphabet says",
         String(repeating: "-", count: 36), true),
    ]
    for (label, id, want) in idRows {
        expect(label, OrchestratorDraft.isTaskID(id), want)
    }

    let secret = String(repeating: "ab", count: 32)
    expect("64 lower-case hex characters are a task secret",
           OrchestratorDraft.isTaskSecret(secret), true)
    expect("63 are not", OrchestratorDraft.isTaskSecret(String(secret.dropLast())), false)
    expect("nor are 64 with an upper-case letter in them",
           OrchestratorDraft.isTaskSecret(secret.uppercased()), false)
    expect("nor 64 characters outside hex",
           OrchestratorDraft.isTaskSecret(String(repeating: "z", count: 64)), false)
}

group("claims is required as a present field, which is the one shape every dispatcher can answer") {
    func answer(declared: Bool, written: Bool) -> String {
        refusalAnswer(OrchestratorDraft.claimsRequirementRefusal(
            declared: declared, writtenForThisDispatch: written))
    }
    // Four rows are the whole rule, and only one of them refuses: an absent field in a body
    // somebody is holding the answer for. It is the row the warning was already firing on.
    let rows: [(String, Bool, Bool, String)] = [
        ("a body written now with no claims field is refused", false, true,
         "422 claims_required"),
        ("a declared write set passes, and an empty declaration is a declaration", true, true,
         "none"),
        ("a stored schedule template is not refused for a field its editor cannot write",
         false, false, "none"),
        ("and neither is a respawn of a body that was admitted once already", true, false, "none"),
    ]
    for (label, declared, written, want) in rows {
        expect(label, answer(declared: declared, written: written), want)
    }

    // A refusal that only says no teaches the caller to route around it. Each of these is an
    // answer a dispatcher always has: the files, the directories when the files are undecided,
    // and the empty array when the task genuinely writes nothing.
    let message = refusalMessage(OrchestratorDraft.claimsRequirementRefusal(
        declared: false, writtenForThisDispatch: true))
    check("the message names the paths a task may write", message.contains("relative paths"))
    check("and the directories to fall back on", message.contains("directories"))
    check("and the empty array that keeps a read-only task sayable",
          message.contains("\"claims\": []"))
    // The two sentences that stop this becoming the field it replaced: an isolated dispatcher is
    // told its list is kept rather than dropped, and nobody is asked for paths a guard writes.
    check("it tells an isolated caller the same list becomes its landing write set",
          message.contains("landing write set"))
    check("and it asks nobody to declare what a repository guard rewrites",
          message.contains("ratcheted line count"))
}

}
