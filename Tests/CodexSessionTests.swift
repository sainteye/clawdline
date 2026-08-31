import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - Codex

// Every screen in this section is a real capture off a real Codex TUI, not a sketch of one.
// The point of these tests is that the shapes are what was actually drawn — a fixture somebody
// wrote from memory would agree with the parser and with nothing else.



























// The defect: after an iTerm2 restart eleven of fifteen rows read `Default` — the profile name
// iTerm2 reports for a tab nobody has titled. `osascript` confirmed the tab titles really did
// say `Default (python)`, and every one of those sessions had its name in Claude Code's own
// files the whole time. So the terminal is not a place a name is kept, and nothing below may
// read one from it.


// The reading itself, which until this group had no assertion anywhere behind it. `lookForTesting`
// replaces the *whole* of it, so with every call site stubbed, gutting `SessionNaming.look` to
// `return .none` left the suite at exactly the same passing count — the headline claim of the type,
// *more than one source*, was the one part of it nothing touched. These go through
// `SessionNaming.Sources`, which stubs the three files and runs the real body over them.


// The second half of the brief, and the one that outlives this incident: one source going quiet
// must not blank a name. The rung above was added because a source disappeared; a rung that
// disappears the same way has bought nothing.

func runCodexSessionTests() {
group("Codex's live line is the one with a clock in it") {
    let working = """
    • Running sleep 25 now.

    • Working (10s • esc to interrupt) · 1 background terminal running · /ps to view · /stop to close

    › Ask Codex to do anything

      gpt-5.6-sol default · ~/code/clawdline
    """
    expect("the working line is found and cut at the bracket",
           Activity.codex(working), "Working (10s • esc to interrupt)")

    // Codex prefixes everything it says with the same bullet, so the glyph proves nothing.
    let quiet = """
    • Ran printf '%s\\n' hello > notes.txt
      └ (no output)

    • Created notes.txt containing hello.

    › Ask Codex to do anything
    """
    check("a sentence with the same bullet is not a live line", Activity.codex(quiet) == nil)

    expect("an approval being reviewed is still work",
           Activity.codex("• Reviewing approval request (3s • esc to interrupt)"),
           "Reviewing approval request (3s • esc to interrupt)")

    // The two live lines are not interchangeable, and reading one with the other's parser is
    // how a session comes back idle while it is plainly busy.
    check("Claude Code's parser does not read Codex's line",
          Activity.parse(working) == nil)
    check("and Codex's does not read Claude Code's",
          Activity.codex("✳ Generating… (21s · thinking)") == nil)
    expect("each read by the right one",
           Activity.parse("✳ Generating… (21s · thinking)", assistant: .claude),
           "Generating… (21s · thinking)")
}

group("Codex's dialogs are read by what is under them, not by the margin") {
    // The trust dialog, captured on a first run in an untrusted directory. Note the caret in
    // column zero — exactly where Codex also draws the composer's.
    let trust = """
    › Ask Codex to do anything

      ? for shortcuts
    > You are in /private/tmp/scratch/probe

      Do you trust the contents of this directory? Working with untrusted contents comes with
      injection. Trusting the directory allows project-local config, hooks, and exec policies.

    › 1. Yes, continue
      2. No, quit

      Press enter to continue
    """
    let menu = SessionState.menu(trust, assistant: .codex)
    expect("both options are read", menu?.options.count, 2)
    expect("the caret's row is the selection", menu?.selected, 1)
    expect("and the words come with it", menu?.options.first?.label, "Yes, continue")
    check("so the session reads as waiting",
          SessionState.read(trust, assistant: .codex) == .waiting)

    // The model picker, where the caret is on the first of six and the question sits above it.
    let models = """
      Select Model and Effort
      Access legacy models by running codex -m <model_name> or in your config.toml

    › 1. gpt-5.6-sol (current)  Latest frontier agentic coding model.
      2. gpt-5.6-terra          Balanced agentic coding model for everyday work.
      3. gpt-5.6-luna           Fast and affordable agentic coding model.

      Press enter to confirm or esc to go back
    """
    expect("a longer picker is read the same way",
           SessionState.menu(models, assistant: .codex)?.options.count, 3)

    // **The one that matters.** A message that begins with a numbered list echoes with the same
    // caret in the same column. What tells them apart is that the composer is still underneath
    // it — a dialog takes the composer away.
    let echoed = """
    › 1. rename the field
      2. update the callers
      3. run the tests

    • Working (4s • esc to interrupt)

    › Ask Codex to do anything

      gpt-5.6-sol default · ~/code/clawdline
    """
    check("a numbered list somebody sent is not a dialog",
          SessionState.menu(echoed, assistant: .codex) == nil)
    check("and the session reads as working, which is what it is",
          SessionState.read(echoed, assistant: .codex) == .working("Working (4s • esc to interrupt)"))

    // Arrowing down moves the caret off the first row; the rows above it are still the menu.
    let moved = """
      Select Model and Effort

      1. gpt-5.6-sol
    › 2. gpt-5.6-terra
      3. gpt-5.6-luna
    """
    let picked = SessionState.menu(moved, assistant: .codex)
    expect("the rows above the caret are still options", picked?.options.count, 3)
    expect("and the caret says which is selected", picked?.selected, 2)

    // An idle screen is not a menu, however many carets are on it.
    let idle = """
    › Run the shell command: sleep 25

    • Ran sleep 25

    › Ask Codex to do anything
    """
    check("an idle Codex session is idle", SessionState.read(idle, assistant: .codex) == .idle)
}

group("a rollout says where its session is working") {
    let head = """
    {"timestamp":"2026-08-23T15:30:26.612Z","ordinal":0,"type":"session_meta","payload":\
    {"session_id":"01a02f3e-8f2c-7011-bb6d-49d2aaabd2a8","cwd":"/Users/me/code/thing",\
    "originator":"codex-tui","cli_version":"0.149.0"}}
    """
    expect("the working directory comes out", Codex.head(inText: head)?.cwd, "/Users/me/code/thing")
    expect("and the session's own id", Codex.head(inText: head)?.id,
           "01a02f3e-8f2c-7011-bb6d-49d2aaabd2a8")
    check("a line with no cwd is not a head", Codex.head(inText: "{\"type\":\"event_msg\"}") == nil)

    // A Codex process writes more than one rollout: its own conversation, and one per subagent
    // it sends off. Same directory, same minute, same everything — except this.
    let mine = """
    {"type":"session_meta","payload":{"session_id":"a","cwd":"/w","originator":"codex-tui",\
    "source":"cli","thread_source":"user"}}
    """
    let theirs = """
    {"type":"session_meta","payload":{"session_id":"b","cwd":"/w","originator":"codex-tui",\
    "source":{"subagent":{"other":"guardian"}},"thread_source":"subagent"}}
    """
    check("a person's thread says so", Codex.head(inText: mine)?.isUser == true)
    check("and a subagent's says otherwise", Codex.head(inText: theirs)?.isUser == false)
    check("a terminal rollout is an interactive CLI session",
          Codex.head(inText: mine)?.isInteractiveCLI == true)
    let desktop = """
    {"type":"session_meta","payload":{"session_id":"c","cwd":"/Users/me/Documents/Codex/new-chat",\
    "originator":"codex_work_desktop","source":"vscode","thread_source":"user"}}
    """
    check("a Desktop conversation is a person but not a CLI project",
          Codex.head(inText: desktop)?.isUser == true
            && Codex.head(inText: desktop)?.isInteractiveCLI == false)
    // The field is newer than the format. Absent means nobody wrote it down, which is not the
    // same as "this is a subagent" — and reading it that way would empty the pane for anyone
    // whose sessions predate it.
    check("a rollout that does not say counts as a person's",
          Codex.head(inText: head)?.isUser == true)
    check("and an old rollout remains a CLI session",
          Codex.head(inText: head)?.isInteractiveCLI == true)

    let growing = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-growing-head-\(UUID().uuidString).jsonl")
    defer { try? FileManager.default.removeItem(at: growing) }
    try! Data().write(to: growing)
    _ = Codex.head(of: growing)
    try! Data((head + "\n").utf8).write(to: growing)
    expect("a newly appended session_meta is reconsidered after an earlier nil",
           Codex.head(of: growing)?.id, "01a02f3e-8f2c-7011-bb6d-49d2aaabd2a8")
    expect("the unified parent identity reads the rollout's session id",
           Transcript.sessionID(in: growing, assistant: .codex),
           "01a02f3e-8f2c-7011-bb6d-49d2aaabd2a8")

    let unfinished = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-unfinished-head-\(UUID().uuidString).jsonl")
    defer { try? FileManager.default.removeItem(at: unfinished) }
    try! Data(#"{"type":"session_meta","payload":{"cwd":"/w"}}"#.utf8)
        .write(to: unfinished)
    check("a rollout head with no id cannot identify a parent",
          Transcript.sessionID(in: unfinished, assistant: .codex) == nil)

    check("Claude root identity is absent when neither registry nor hook names a transcript",
          Transcript.namedClaudeSessionID(registry: nil, hook: nil) == nil)
    expect("Claude root identity prefers the process registry over a legacy hook",
           Transcript.namedClaudeSessionID(registry: "current", hook: "stale"), "current")
    check("an empty legacy hook cannot turn heuristic transcript ranking into identity",
          Transcript.namedClaudeSessionID(registry: nil, hook: "") == nil)
}

group("the rollout a Codex process is holding open") {
    // What `lsof -p` actually answers with: a hundred files, nearly all of them SQLite.
    let open = [
        "/Users/me/.codex/state_5.sqlite",
        "/Users/me/.codex/thread-writer-locks/01a02f68-ccd4.lock",
        "/Users/me/code/thing/.git/index",
        "/Users/me/.codex/sessions/2026/08/24/rollout-2026-08-24T00-16-32-01a02f68-ccd4.jsonl",
    ]
    check("a rollout is picked out of it by shape", Codex.isRollout(open[3]))
    check("a lock file beside it is not one", !Codex.isRollout(open[1]))
    check("nor is a jsonl somewhere else",
          !Codex.isRollout("/Users/me/.claude/projects/-Users-me/rollout-x.jsonl"))
    check("and the name has to be a rollout's",
          !Codex.isRollout("/Users/me/.codex/sessions/2026/08/24/notes.jsonl"))
}

group("a new Codex process never borrows an existing conversation") {
    let fm = FileManager.default
    let base = fm.temporaryDirectory.appendingPathComponent("clawdline-codex-\(UUID().uuidString)")
    let day = base.appendingPathComponent("sessions/2026/08/24", isDirectory: true)
    let file = day.appendingPathComponent("rollout-2026-08-24T00-16-32-existing.jsonl")
    let oldHome = Config.shared.codexHome
    defer {
        Config.shared.codexHome = oldHome
        try? fm.removeItem(at: base)
    }
    try! fm.createDirectory(at: day, withIntermediateDirectories: true)
    try! Data("""
    {"type":"session_meta","payload":{"session_id":"existing","cwd":"/w",\
    "originator":"codex-tui","thread_source":"user"}}
    """.utf8).write(to: file)
    Config.shared.codexHome = base.path

    check("the clock remains a fallback when there is no process to ask",
          Codex.locate(cwd: "/w", startedAt: Date(), days: 3) == file)
    check("a known process with no rollout returns nothing instead of the fallback",
          Codex.locate(cwd: "/w", startedAt: Date(),
                       pid: Int32(ProcessInfo.processInfo.processIdentifier), days: 3) == nil)
}

group("a rollout reads as the same entries a transcript does") {
    func line(_ item: String) -> String {
        "{\"timestamp\":\"2026-08-23T15:43:06.283Z\",\"type\":\"event_msg\",\"payload\":"
            + "{\"type\":\"item_completed\",\"item\":" + item + "}}"
    }
    let rollout = [
        line("{\"type\":\"UserMessage\",\"content\":[{\"type\":\"text\",\"text\":\"fix the tests\"}]}"),
        line("{\"type\":\"Reasoning\",\"summary_text\":[],\"raw_content\":[]}"),
        line("{\"type\":\"AgentMessage\",\"phase\":\"commentary\","
             + "\"content\":[{\"type\":\"Text\",\"text\":\"Looking now.\"}]}"),
        line("{\"type\":\"CommandExecution\",\"command\":[\"/bin/zsh\",\"-lc\",\"./test.sh\"],"
             + "\"aggregated_output\":\"1281 checks passed\\nfine\",\"exit_code\":0}"),
        line("{\"type\":\"FileChange\",\"changes\":{\"/Users/me/a/Thing.swift\":{\"type\":\"update\"}}}"),
        // Not an item_completed at all — the same conversation, as it went to the model.
        "{\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"developer\","
            + "\"content\":[{\"type\":\"input_text\",\"text\":\"<skills_instructions>…\"}]}}",
    ].joined(separator: "\n")

    let entries = Codex.parse(rollout)
    expect("the machinery is left out and the conversation is not", entries.count, 5)
    expect("the person spoke first", entries[0].kind, Transcript.Entry.Kind.user)
    expect("and what they said", entries[0].text, "fix the tests")
    expect("thinking with nothing in it is not a row",
           entries.filter { $0.text.contains("Reasoning") }.count, 0)
    expect("the assistant answered", entries[1].kind, Transcript.Entry.Kind.assistant)
    expect("a command is a tool call", entries[2].tool, "shell")
    expect("with the login shell taken off the front", entries[2].text, "./test.sh")
    expect("and its first line of output under it", entries[3].kind,
           Transcript.Entry.Kind.toolResult)
    expect("which is the first line and not all of it", entries[3].text, "1281 checks passed")
    expect("an edit names the file", entries[4].text, "Thing.swift")
    expect("the timestamp is read", entries[0].time?.timeIntervalSince1970,
           1787499786.283)

    let fileChangeEntries = Codex.entries(ofItem: [
        "type": "FileChange",
        "changes": [
            "/Users/me/a/Added.swift": ["type": "add", "content": "let added = true\n"],
            "/Users/me/a/Deleted.swift": ["type": "delete", "content": "let gone = true\n"],
            "/Users/me/a/Thing.swift": [
                "type": "update", "move_path": "/Users/me/a/Renamed.swift",
                "unified_diff": "@@ -1,2 +1,2 @@\n-let old = true\n+let new = true\n keep\n",
            ],
        ],
    ], at: nil)
    expect("one Codex file-change item stays one transcript entry", fileChangeEntries.count, 1)
    expect("the structured file-change entry remains an edit", fileChangeEntries.first?.tool,
           "edit")
    expect("every changed file survives the rollout parser",
           fileChangeEntries.first?.fileChanges.count, 3)
    expect("changed files have a stable path order",
           fileChangeEntries.first?.fileChanges.map(\.path),
           ["/Users/me/a/Added.swift", "/Users/me/a/Deleted.swift", "/Users/me/a/Thing.swift"])
    expect("an added file keeps its complete content",
           fileChangeEntries.first?.fileChanges[0].content, "let added = true\n")
    expect("a deleted file keeps its complete content",
           fileChangeEntries.first?.fileChanges[1].content, "let gone = true\n")
    expect("an update keeps the unified diff",
           fileChangeEntries.first?.fileChanges[2].unifiedDiff,
           "@@ -1,2 +1,2 @@\n-let old = true\n+let new = true\n keep\n")
    expect("a rename keeps its destination",
           fileChangeEntries.first?.fileChanges[2].movePath, "/Users/me/a/Renamed.swift")
    let fileChangeRow = RemoteServer.transcriptRows(fileChangeEntries).first
    let fileChangeWire = fileChangeRow?["fileChanges"] as? [[String: Any]]
    expect("structured file changes cross the transcript wire", fileChangeWire?.count, 3)
    expect("the wire keeps the diff under an explicit key",
           fileChangeWire?.last?["unifiedDiff"] as? String,
           "@@ -1,2 +1,2 @@\n-let old = true\n+let new = true\n keep\n")
    check("absent change fields stay absent rather than becoming null",
          fileChangeWire?.first?["unifiedDiff"] == nil
            && fileChangeWire?.last?["content"] == nil)

    let planInput = #"const p = [{step:"Inspect <unsafe>",status:"completed"},{step:"Implement cards",status:"in_progress"},{step:"Verify",status:"pending"}]; const r = await tools.update_plan({explanation:"Now",plan:p}); text(r)"#
    let planRowObject: [String: Any] = [
        "timestamp": "2026-08-30T15:31:02.125Z", "type": "response_item",
        "payload": ["type": "custom_tool_call", "name": "exec", "input": planInput],
    ]
    let planRow = String(decoding: try! JSONSerialization.data(withJSONObject: planRowObject),
                         as: UTF8.self)
    let planEntries = Codex.parse(planRow)
    expect("an exec-wrapped update_plan becomes one transcript entry", planEntries.count, 1)
    expect("the plan keeps all literal steps", planEntries.first?.plan.count, 3)
    expect("Codex's snake-case status joins the app-server wire spelling",
           planEntries.first?.plan.map(\.status), ["completed", "inProgress", "pending"])
    expect("the plan keeps authored step text inertly",
           planEntries.first?.plan.first?.step, "Inspect <unsafe>")
    let planWire = RemoteServer.transcriptRows(planEntries).first?["plan"] as? [[String: Any]]
    expect("structured plans cross the transcript wire", planWire?.count, 3)
    expect("the wire uses the documented inProgress spelling",
           planWire?[1]["status"] as? String, "inProgress")
    let computedPlan = planInput.replacingOccurrences(
        of: #"plan:p"#, with: #"plan:makePlan()"#)
    let computedRowObject: [String: Any] = [
        "type": "response_item",
        "payload": ["type": "custom_tool_call", "name": "exec", "input": computedPlan],
    ]
    let computedRow = String(decoding: try! JSONSerialization.data(withJSONObject: computedRowObject),
                             as: UTF8.self)
    expect("computed update_plan input is rejected instead of executed",
           Codex.parse(computedRow).count, 0)

    let calledEntries = Codex.entries(ofItem: [
        "type": "McpToolCall", "server": "browser", "tool": "connect",
        "arguments": ["title": "Connect <preview>"], "status": "completed",
        "duration": ["secs": 1, "nanos": 250_000_000],
        "result": ["isError": false,
                   "content": [["type": "text", "text": "Connected\npage two"]]],
    ], at: nil)
    expect("a titled MCP call stays one rich activity", calledEntries.count, 1)
    expect("the activity is classified as Called", calledEntries.first?.activity?.kind, "called")
    expect("Called uses the title Codex supplied", calledEntries.first?.activity?.title,
           "Connect <preview>")
    expect("Called keeps status and duration",
           calledEntries.first?.activity?.durationMilliseconds, 1_250)
    expect("Called keeps bounded multiline result detail",
           calledEntries.first?.activity?.result, "Connected\npage two")

    let exploredEntries = Codex.entries(ofItem: [
        "type": "CommandExecution", "command": ["/bin/zsh", "-lc", "rg title Sources"],
        "parsed_cmd": [
            ["type": "search", "cmd": "rg title Sources", "query": "title", "path": "Sources"],
            ["type": "read", "cmd": "sed -n 1,20p Sources/Codex.swift",
             "name": "Codex.swift", "path": "Sources/Codex.swift"],
        ], "status": "completed", "duration": ["secs": 0, "nanos": 90_000_000],
    ], at: nil)
    expect("read and search command actions stay one Explored activity", exploredEntries.count, 1)
    expect("the activity is classified as Explored", exploredEntries.first?.activity?.kind,
           "explored")
    expect("Explored retains both typed actions",
           exploredEntries.first?.activity?.actions.map(\.kind), ["search", "read"])
    expect("Explored retains the query and path",
           exploredEntries.first?.activity?.actions.first?.query, "title")
    let unknownCommand = Codex.entries(ofItem: [
        "type": "CommandExecution", "command": ["/bin/zsh", "-lc", "make magic"],
        "parsed_cmd": [["type": "unknown", "cmd": "make magic"]],
        "aggregated_output": "done", "exit_code": 0,
    ], at: nil)
    expect("unknown command actions keep the ordinary shell fallback", unknownCommand.count, 2)
    check("the shell fallback does not invent activity metadata",
          unknownCommand.first?.tool == "shell" && unknownCommand.first?.activity == nil)
    let calledWire = RemoteServer.transcriptRows(calledEntries).first?["activity"] as? [String: Any]
    expect("rich activity crosses the transcript wire", calledWire?["kind"] as? String, "called")
    check("ordinary entries omit both new optional wire fields",
          RemoteServer.transcriptRows([entries[0]]).first?["plan"] == nil
            && RemoteServer.transcriptRows([entries[0]]).first?["activity"] == nil)

    expect("a truncated line is skipped rather than fatal", Codex.parse("{\"type\":").count, 0)
    expect("and so is an item nobody has taught this about",
           Codex.parse(line("{\"type\":\"SomethingNew\",\"content\":\"…\"}")).count, 0)

    let drop = Drop.directory.appendingPathComponent(
        "clawdline-20260827-120000-000-AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE.png").path
    let imageTurn = Codex.entries(ofItem: [
        "type": "UserMessage", "content": [["type": "text", "text": "look" + drop]],
    ], at: Date(timeIntervalSince1970: 200))
    expect("a Codex drop-cache path remains one user turn", imageTurn.count, 1)
    expect("the Codex transport path is removed from authored text", imageTurn.first?.text, "look")
    expect("the Codex transport path becomes canonical image metadata",
           imageTurn.first?.imageCount, 1)
    let codexImageOnly = Codex.entries(ofItem: [
        "type": "UserMessage", "content": [["type": "text", "text": drop]],
    ], at: Date(timeIntervalSince1970: 201))
    expect("a Codex image-only turn is retained", codexImageOnly.count, 1)
    expect("a Codex image-only turn has a visible attachment marker",
           codexImageOnly.first?.text, "[Image #1]")
    expect("a Codex image-only turn exposes one image", codexImageOnly.first?.imageCount, 1)
    let authoredCases = [
        "改一下 Resources/web/app/js/view/waits.js",
        "see docs/notes",
        "keep spaces and 'quotes'",
        "check https://example.com/x",
    ]
    for authored in authoredCases {
        let entries = Codex.entries(ofItem: [
            "type": "UserMessage", "content": authored + drop,
        ], at: Date(timeIntervalSince1970: 202))
        expect("Codex drop removal preserves authored text: \(authored)",
               entries.first?.text, authored)
        expect("Codex drop removal counts its image: \(authored)",
               entries.first?.imageCount, 1)
    }
    let adjacent = Codex.entries(ofItem: [
        "type": "UserMessage", "content": "two" + drop + drop,
    ], at: Date(timeIntervalSince1970: 203))
    expect("adjacent Codex drop paths preserve preceding text", adjacent.first?.text, "two")
    expect("adjacent Codex drop paths are counted separately", adjacent.first?.imageCount, 2)
    let spacedDirectory = "/Users/Test Person/Library/Caches/com.tsunamiworks.clawdline/drops"
    let spacedDrop = spacedDirectory + "/clawdline-20260827-120000-000-"
        + "AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE.png"
    let quoted = Transcript.canonicalDropPaths(
        in: "keep 'quotes' and docs/notes'" + spacedDrop + "'",
        directory: spacedDirectory)
    expect("the real regex preserves spaces, quotes, and authored paths", quoted.text,
           "keep 'quotes' and docs/notes")
    expect("the real regex recognizes a shell-quoted drop path", quoted.imageCount, 1)
    let quotedAdjacent = Transcript.canonicalDropPaths(
        in: "two'" + spacedDrop + "''" + spacedDrop + "'",
        directory: spacedDirectory)
    expect("the real regex preserves text before adjacent quoted paths", quotedAdjacent.text,
           "two")
    expect("the real regex counts adjacent quoted paths", quotedAdjacent.imageCount, 2)
    let webRow = RemoteServer.transcriptRows(imageTurn).first
    expect("the Web transcript row carries canonical image metadata",
           webRow?["imageCount"] as? Int, 1)
}

group("a Codex session can be named from its first request") {
    func completed(_ item: String) -> String {
        "{\"type\":\"event_msg\",\"payload\":{\"type\":\"item_completed\",\"item\":"
            + item + "}}"
    }
    let rollout = [
        completed("{\"type\":\"AgentMessage\",\"content\":[{\"text\":\"Hello\"}]}"),
        completed("{\"type\":\"UserMessage\",\"content\":[{\"text\":\"修正登入逾時問題\"}]}"),
        completed("{\"type\":\"UserMessage\",\"content\":[{\"text\":\"and add tests\"}]}"),
    ].joined(separator: "\n")
    expect("the first human request is selected",
           Codex.firstUserMessage(in: rollout), "修正登入逾時問題")

    let prompt = CodexNaming.prompt(for: "修正登入逾時問題")
    check("the request is marked as untrusted data", prompt.contains("untrusted data"))
    check("the title turn is explicitly told not to use tools", prompt.contains("do not use tools"))
    check("the request is delimited", prompt.contains("<request>\n修正登入逾時問題\n</request>"))
    expect("long requests are bounded",
           CodexNaming.prompt(for: String(repeating: "x", count: 5_000), limit: 10)
                .components(separatedBy: "<request>\n").last?
                .components(separatedBy: "\n</request>").first,
           String(repeating: "x", count: 10))

    expect("markdown and a label are removed from a model title",
           CodexNaming.cleanTitle("# Title:  Fix login timeout.\nExtra explanation"),
           "Fix login timeout")
    expect("Chinese wrappers and punctuation are removed",
           CodexNaming.cleanTitle("「修正登入逾時問題。」"), "修正登入逾時問題")
    expect("a quoted title is unwrapped",
           CodexNaming.cleanTitle("“Codex Session 自動命名”"), "Codex Session 自動命名")
    check("an empty answer is refused", CodexNaming.cleanTitle("\n  \n") == nil)

    let named: [String: Any] = ["result": ["thread": ["name": " Bug bash "]]]
    let unnamed: [String: Any] = ["result": ["thread": ["name": NSNull()]]]
    expect("a persisted thread name is recognised", CodexNaming.threadName(in: named), "Bug bash")
    check("a null persisted name is unnamed", CodexNaming.threadName(in: unnamed) == nil)

    let configRoot = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-naming-provider-\(UUID().uuidString)", isDirectory: true)
    defer { try? FileManager.default.removeItem(at: configRoot) }
    let config = Config(directoryForTesting: configRoot)
    expect("automatic naming starts off", config.automaticNamingSelection, "off")
    config.automaticNamingSelection = Assistant.claude.rawValue
    check("choosing Claude enables naming", config.codexAutoName)
    expect("Claude is the selected naming assistant", config.automaticNamingAssistant, .claude)
    check("the provider choice is saved", config.save())
    let reloaded = Config(directoryForTesting: configRoot)
    expect("the naming provider survives a reload", reloaded.automaticNamingAssistant, .claude)
    expect("the enabled provider is the settings selection", reloaded.automaticNamingSelection,
           Assistant.claude.rawValue)
    reloaded.automaticNamingSelection = "off"
    check("off disables naming", !reloaded.codexAutoName)
    expect("turning naming off remembers the provider", reloaded.automaticNamingAssistant, .claude)

    let start = Date(timeIntervalSince1970: 100)
    let provisional = CodexNameObservation.observe("the opening request", previous: nil,
                                                   now: start, settleAfter: 6)
    check("the first native thread name is not assumed final", !provisional.settled)
    let stillProvisional = CodexNameObservation.observe(
        "the opening request", previous: provisional.observation,
        now: start.addingTimeInterval(3), settleAfter: 6)
    check("the same early name still waits for Codex's refinement", !stillProvisional.settled)
    let refined = CodexNameObservation.observe(
        "A concise title", previous: stillProvisional.observation,
        now: start.addingTimeInterval(4), settleAfter: 6)
    check("a changed native name restarts the settling window", !refined.settled)
    let settled = CodexNameObservation.observe(
        "A concise title", previous: refined.observation,
        now: start.addingTimeInterval(10), settleAfter: 6)
    check("an unchanged refined name eventually settles", settled.settled)

    let claudeEnvelope = #"{"structured_output":{"title":"修正登入逾時"}}"#
    expect("Claude's schema-validated title is read from its print envelope",
           CodexNaming.title(inClaudeOutput: claudeEnvelope), "修正登入逾時")
    let claudeArguments = CodexNaming.claudeArguments(system: "name it", schema: "{}")
    check("Claude naming is not persisted as another session",
          claudeArguments.contains("--no-session-persistence"))
    let toolsFlag = claudeArguments.firstIndex(of: "--tools")
    check("Claude naming cannot use tools", toolsFlag.map {
        claudeArguments.indices.contains($0 + 1) && claudeArguments[$0 + 1].isEmpty
    } ?? false)
    // What used to sit here was `CodexNaming.displayLabel(threadName:terminalLabel:)` and two
    // assertions pinning "an absent Codex name keeps the terminal tab label". Both are gone with
    // it: the function had no caller left in `Sources/` once naming stopped reading tab titles,
    // and a test holding a removed contract in place is a contract pointing the wrong way.
    // Where a Codex tab lands with no name of its own is asserted in
    // `a session's name never comes from its terminal's tab title`.
}

group("an unnamed Claude conversation gets a durable model fallback") {
    let first = #"{"type":"user","message":{"content":[{"type":"text","text":"修正登入逾時問題"}]}}"#
    let second = #"{"type":"user","message":{"content":[{"type":"text","text":"順便更新文件"}]}}"#
    let assistant = #"{"type":"assistant","message":{"content":[{"type":"text","text":"I will inspect it."}]}}"#
    expect("Claude naming uses the first authored request",
           Transcript.firstUserMessage(in: [assistant, first, second].joined(separator: "\n")),
           "修正登入逾時問題")

    check("Claude waits until the first turn is idle before paying for a fallback",
          !CodexNaming.shouldGenerateClaudeTitle(systemTitle: nil,
                                                 request: "修正登入逾時問題",
                                                 state: .working("Working")))
    check("Claude does not replace a title its own system supplied",
          !CodexNaming.shouldGenerateClaudeTitle(systemTitle: "Claude's title",
                                                 request: "修正登入逾時問題",
                                                 state: .idle))
    check("an idle unnamed Claude conversation with a request is eligible",
          CodexNaming.shouldGenerateClaudeTitle(systemTitle: nil,
                                                request: "修正登入逾時問題",
                                                state: .idle))

    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-claude-auto-title-\(UUID().uuidString)",
                                isDirectory: true)
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let config = Config(directoryForTesting: directory)
    expect("the generated fallback is stored by conversation id",
           config.setAutomaticSessionTitle("修正登入逾時", sessionID: "claude-one",
                                           terminalID: "terminal-one"),
           "修正登入逾時")
    check("an automatic fallback never enters the human-title rung",
          config.sessionTitle(sessionID: "claude-one", terminalID: "terminal-one") == nil)
    expect("the automatic rung can read it independently",
           config.automaticSessionTitle(sessionID: "claude-one"), "修正登入逾時")
    config.setAutomaticSessionTitle("第二段對話", sessionID: "claude-two",
                                    terminalID: "terminal-one")
    expect("reusing a terminal does not erase the earlier conversation's fallback",
           config.automaticSessionTitle(sessionID: "claude-one"), "修正登入逾時")
    check("the automatic fallback is saved", config.save())
    let loaded = Config(directoryForTesting: directory)
    expect("the fallback survives an app restart",
           loaded.automaticSessionTitle(sessionID: "claude-one"), "修正登入逾時")
    loaded.setSessionTitle("我的登入修復", sessionID: "claude-one",
                           terminalID: "terminal-one")
    expect("a person can still name the same conversation",
           loaded.sessionTitle(sessionID: "claude-one", terminalID: "terminal-one"),
           "我的登入修復")
    loaded.setSessionTitle("", sessionID: "claude-one", terminalID: "terminal-one")
    expect("clearing that choice reveals the generated fallback again",
           loaded.automaticSessionTitle(sessionID: "claude-one"), "修正登入逾時")

    let target = TargetSession(backend: .iterm, id: "terminal-claude-fallback",
                               name: "Default (claude)", tty: "/dev/ttys098",
                               windowIndex: 0, tabIndex: 0, assistant: .claude)
    CodexNaming.shared.rememberClaudeForTesting("模型產生的名稱",
                                                sessionID: "claude-display",
                                                targetID: target.id)
    expect("Clawdline draws the fallback instead of Claude's derived handle",
           CodexNaming.shared.title(for: target), "模型產生的名稱")
    expect("Claude's own later title still outranks the fallback",
           TargetSession.preferredDisplayLabel(
               manualTitle: nil, orchestratorTitle: nil,
               conversationTitle: "Claude 系統名稱",
               threadName: CodexNaming.shared.title(for: target),
               handle: "yoga-astro-44", coordinate: target.coordinate),
           "Claude 系統名稱")
    CodexNaming.shared.forget(target: target)
}

group("a Codex npm shim starts with Finder's cold PATH") {
    let root = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-codex-launch-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }

    let node = root.appendingPathComponent("node")
    let codex = root.appendingPathComponent("codex")
    try! Data("#!/bin/sh\nprintf 'codex started'\n".utf8).write(to: node)
    try! Data("#!/usr/bin/env node\n".utf8).write(to: codex)
    try! FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: node.path)
    try! FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: codex.path)

    func run(environment: [String: String]) -> (status: Int32, output: String) {
        let process = Process()
        process.executableURL = codex
        process.environment = environment
        let output = Pipe()
        process.standardOutput = output
        process.standardError = output
        do { try process.run() } catch { return (-1, error.localizedDescription) }
        let data = output.fileHandleForReading.readDataToEndOfFile()
        process.waitQuietly()
        return (process.terminationStatus, String(decoding: data, as: UTF8.self))
    }

    let finder = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin"]
    check("the unprepared npm shim genuinely cannot find node", run(environment: finder).status != 0)
    let prepared = run(environment: CodexNaming.processEnvironment(for: codex,
                                                                    inherited: finder))
    expect("the naming launch boundary supplies the shim's interpreter", prepared.status, 0)
    expect("the intended Codex shim ran", prepared.output, "codex started")
}

group("a canonical Codex npm shim resolves to its native vendor executable") {
    let root = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-codex-package-\(UUID().uuidString)", isDirectory: true)
    defer { try? FileManager.default.removeItem(at: root) }
    let nodeRoot = root.appendingPathComponent("versions/node/v24.1.0", isDirectory: true)
    let package = nodeRoot.appendingPathComponent("lib/node_modules/@openai/codex",
                                                   isDirectory: true)
    let script = package.appendingPathComponent("bin/codex.js")
    #if arch(arm64)
    let vendor = "codex-darwin-arm64/vendor/aarch64-apple-darwin/bin/codex"
    #else
    let vendor = "codex-darwin-x64/vendor/x86_64-apple-darwin/bin/codex"
    #endif
    let native = package.appendingPathComponent("node_modules/@openai")
        .appendingPathComponent(vendor)
    let shim = nodeRoot.appendingPathComponent("bin/codex")
    try! FileManager.default.createDirectory(at: script.deletingLastPathComponent(),
                                             withIntermediateDirectories: true)
    try! FileManager.default.createDirectory(at: native.deletingLastPathComponent(),
                                             withIntermediateDirectories: true)
    try! Data("#!/usr/bin/env node\n".utf8).write(to: script)
    try! Data("#!/bin/sh\n".utf8).write(to: native)
    try! FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)
    try! FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: native.path)
    try! FileManager.default.createDirectory(at: shim.deletingLastPathComponent(),
                                             withIntermediateDirectories: true)
    try! FileManager.default.createSymbolicLink(
        atPath: shim.path,
        withDestinationPath: "../lib/node_modules/@openai/codex/bin/codex.js")

    expect("the architecture-matched native binary is preferred",
           CodexNaming.preferredExecutable(for: shim), native)
    expect("an unrelated executable keeps its process-bound identity",
           CodexNaming.preferredExecutable(for: URL(fileURLWithPath: "/bin/sh")),
           URL(fileURLWithPath: "/bin/sh"))
}

group("the fields of a rollout, one at a time") {
    expect("a login shell is unwrapped",
           Codex.command(["/bin/zsh", "-lc", "ls -la"]), "ls -la")
    expect("bash counts too", Codex.command(["/bin/bash", "-c", "make"]), "make")
    expect("anything else is left as it was",
           Codex.command(["git", "status", "--short"]), "git status --short")
    expect("a failure with no output says so",
           Codex.outcome(of: ["exit_code": 3, "aggregated_output": ""]), "exit 3")
    expect("and a success with no output says nothing",
           Codex.outcome(of: ["exit_code": 0, "aggregated_output": ""]), "")
    expect("an MCP call shows the title its plugin wrote",
           Codex.arguments(["title": "open the run page", "code": "await browser.open()"]),
           "open the run page")
    expect("four files are three and a count",
           Codex.changed(["/a/one.swift": 1, "/b/two.swift": 1, "/c/three.swift": 1,
                          "/d/four.swift": 1]),
           "one.swift, two.swift, three.swift +1")
}

group("a person-named session title outranks every automatic label") {
    expect("a person's title outranks the orchestrator task",
           TargetSession.preferredDisplayLabel(
               manualTitle: "My release room", orchestratorTitle: "automatic handoff",
               conversationTitle: "Ledger reader layer", threadName: "Codex thread",
               handle: "clawdline-cb", coordinate: "⌘1-1"),
           "My release room")
    expect("the orchestrator task still outranks what the conversation calls itself",
           TargetSession.preferredDisplayLabel(
               manualTitle: nil, orchestratorTitle: "automatic handoff",
               conversationTitle: "Ledger reader layer", threadName: "Codex thread",
               handle: "clawdline-cb", coordinate: "⌘1-1"),
           "automatic handoff")
    expect("the conversation's own title outranks Codex metadata and the derived handle",
           TargetSession.preferredDisplayLabel(
               manualTitle: nil, orchestratorTitle: nil,
               conversationTitle: "Ledger reader layer", threadName: "Codex thread",
               handle: "clawdline-cb", coordinate: "⌘1-1"),
           "Ledger reader layer")
    expect("Codex metadata still outranks the derived handle",
           TargetSession.preferredDisplayLabel(
               manualTitle: nil, orchestratorTitle: nil, conversationTitle: nil,
               threadName: "Codex thread", handle: "clawdline-cb", coordinate: "⌘1-1"),
           "Codex thread")
    // `clawdline-cb` is not a description of anything, which is exactly why it sits here: every
    // registry file read on 2026-08-28 said `nameSource: "derived"`. It still names the project
    // and the conversation, and a coordinate names neither.
    expect("a derived handle is preferred to a coordinate",
           TargetSession.preferredDisplayLabel(
               manualTitle: nil, orchestratorTitle: nil, conversationTitle: nil,
               threadName: nil, handle: "clawdline-cb", coordinate: "⌘1-1"),
           "clawdline-cb")
    expect("and with nothing to go on, where the tab is",
           TargetSession.preferredDisplayLabel(
               manualTitle: nil, orchestratorTitle: nil, conversationTitle: nil,
               threadName: nil, handle: nil, coordinate: "⌘1-1"),
           "⌘1-1")
    expect("a source that answers with blanks is a source that did not answer",
           TargetSession.preferredDisplayLabel(
               manualTitle: "   ", orchestratorTitle: " \n ", conversationTitle: "",
               threadName: nil, handle: nil, coordinate: "⌘1-1"),
           "⌘1-1")
    // Clearing, through the store rather than by passing `nil` in by hand. Handing the function a
    // literal `nil` restates the check above it and proves nothing about what happens when a
    // person empties the box: the interesting half is that `Config` stops answering.
    let clearingDir = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-title-clearing-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: clearingDir, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: clearingDir) }
    let store = Config(directoryForTesting: clearingDir)
    func labelForStoredTitle() -> String {
        TargetSession.preferredDisplayLabel(
            manualTitle: store.sessionTitle(sessionID: nil, terminalID: "terminal-clearing"),
            orchestratorTitle: "automatic handoff", conversationTitle: nil,
            threadName: "Codex thread", handle: nil, coordinate: "⌘1-1")
    }
    store.setSessionTitle("Release room", sessionID: nil, terminalID: "terminal-clearing")
    expect("a stored title is what the display prefers", labelForStoredTitle(), "Release room")
    store.setSessionTitle("   \n  ", sessionID: nil, terminalID: "terminal-clearing")
    expect("clearing a person's title restores the automatic label",
           labelForStoredTitle(), "automatic handoff")
}

group("a session's name never comes from its terminal's tab title") {
    defer { SessionNaming.lookForTesting = noSessionNames; SessionNaming.forgetForTesting() }
    func tab(_ title: String, id: String = UUID().uuidString,
             assistant: Assistant? = .claude, tab index: Int = 0) -> TargetSession {
        TargetSession(backend: .iterm, id: id, name: title, tty: "/dev/ttys900",
                      windowIndex: 0, tabIndex: index, assistant: assistant)
    }

    // Each of these is a tab title that used to decide a row. None of them may now.
    SessionNaming.forgetForTesting()
    SessionNaming.lookForTesting = { _ in
        SessionNaming.Name(title: "Ledger reader layer", handle: "clawdline-cb")
    }
    for hostile in ["Default (python)", "WRONG", "◐ somebody else's task", ""] {
        SessionNaming.forgetForTesting()
        expect("a tab titled \(hostile.isEmpty ? "nothing" : hostile) still shows its own name",
               tab(hostile).displayLabel, "Ledger reader layer")
    }

    // And with nothing to go on the answer is where the tab is, never what it is called: a
    // profile name is the same on every tab that has one, so it tells two rows apart from
    // nothing at all.
    SessionNaming.forgetForTesting()
    SessionNaming.lookForTesting = { _ in .none }
    expect("a tab nothing can name says where it is, not what its profile is called",
           tab("Default (python)", tab: 4).displayLabel, "⌘1-5")

    // A tab with no assistant in it has no conversation to name — and must not go on wearing the
    // name of the one that has left.
    SessionNaming.forgetForTesting()
    SessionNaming.lookForTesting = { _ in SessionNaming.Name(title: "gone", handle: nil) }
    expect("a shell is not called after the session that used to be in it",
           tab("Default (-zsh)", assistant: nil, tab: 2).displayLabel, "⌘1-3")

    // Codex, which is the half of this that got worse. `SessionNaming` answers for Claude only,
    // and Codex keeps its name in memory that a restart empties — so an unnamed Codex tab now
    // shows a coordinate where it used to show the terminal's title. That is the rule applied
    // evenly and not an oversight, and this case is here because the group above it is all
    // Claude and a shell: an edit that handed `terminalLabel` back to Codex alone would have
    // gone through every assertion in this file untouched.
    SessionNaming.forgetForTesting()
    SessionNaming.lookForTesting = { _ in SessionNaming.Name(title: "not this one", handle: nil) }
    expect("a Codex tab with no name of its own says where it is, not what the tab is called",
           tab("Default (node)", assistant: .codex, tab: 6).displayLabel, "⌘1-7")
}

group("a name is read from what proves identity, and from nothing else") {
    let root = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-naming-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }

    let now = Date()
    let mine = "11111111-1111-4111-8111-111111111111"
    let stranger = "99999999-9999-4999-8999-999999999999"
    func write(_ id: String, _ title: String, created: Date, modified: Date) {
        let url = root.appendingPathComponent("\(id).jsonl")
        try! #"{"type":"ai-title","aiTitle":"\#(title)"}"#
            .write(to: url, atomically: true, encoding: .utf8)
        try! FileManager.default.setAttributes([.creationDate: created,
                                                .modificationDate: modified],
                                               ofItemAtPath: url.path)
    }
    // The stranger is the project's most recently written transcript, which is exactly what
    // `Transcript.locate` returns when it has been given nothing to identify a session by. Its
    // title is descriptive and plausible and belongs to somebody else — the failure that is worse
    // than the one this feature was written to fix, because `Default` on eleven rows announced
    // itself and this does not.
    write(stranger, "somebody else's conversation",
          created: now.addingTimeInterval(-3600), modified: now)
    write(mine, "Ledger reader layer",
          created: now.addingTimeInterval(-10), modified: now.addingTimeInterval(-5))

    let target = TargetSession(backend: .iterm, id: UUID().uuidString, name: "Default (python)",
                               tty: "/dev/ttys902", windowIndex: 0, tabIndex: 0, assistant: .claude)
    func registryEntry(sessionID: String?, name: String?) -> SessionRegistry.Entry? {
        var row: [String: Any] = ["pid": 4242, "peerProtocol": SessionRegistry.protocolVersion]
        if let sessionID { row["sessionId"] = sessionID }
        if let name { row["name"] = name }
        guard let data = try? JSONSerialization.data(withJSONObject: row) else { return nil }
        return SessionRegistry.parse(data)
    }
    func sources(registry: SessionRegistry.Entry? = nil, hook: String? = nil,
                 transcripts: URL? = root) -> SessionNaming.Sources {
        SessionNaming.Sources(registryEntry: { _ in registry }, hookSessionID: { _ in hook },
                              transcripts: { _ in transcripts })
    }

    // Route one: the registry names the conversation, so the file name is known and nothing is
    // ranked. No start time at all here — that is the point, either source alone is enough.
    expect("the registry's session id picks the transcript by name",
           SessionNaming.look(at: target, startedAt: nil,
                              sources: sources(registry: registryEntry(sessionID: mine,
                                                                       name: "clawdline-cb"))),
           SessionNaming.Name(title: "Ledger reader layer", handle: "clawdline-cb"))

    // Route two: no registry file — the switch is off, or this Claude Code predates it — and a
    // `SessionStart` hook answers the same question. This was the third source the doc comment
    // claimed and the code did not read; `Transcript.record(of:)` had been asking both all along.
    expect("a hook's session id answers when there is no registry file",
           SessionNaming.look(at: target, startedAt: nil, sources: sources(hook: mine)).title,
           "Ledger reader layer")

    // Route three: no id from anywhere, but the process start time is measurable, and a session's
    // own transcript is created when the session starts. The stranger's file is newer by
    // modification and an hour older by creation, so only this route tells them apart.
    expect("with no id, the process start time still finds the right one",
           SessionNaming.look(at: target, startedAt: now.addingTimeInterval(-10),
                              sources: sources(registry: registryEntry(sessionID: nil,
                                                                       name: "clawdline-cb"))).title,
           "Ledger reader layer")

    // And the guard. Every source of identity is quiet: no registry id, no hook, no start time.
    // `Transcript.locate` would have one line left — whichever file was written last — and would
    // hand this tab the stranger's name. Remove the `sessionID != nil || startedAt != nil` guard
    // in `look` and this assertion reads `somebody else's conversation`.
    let blind = SessionNaming.look(at: target, startedAt: nil,
                                   sources: sources(registry: registryEntry(sessionID: nil,
                                                                            name: "clawdline-cb")))
    check("nothing that proves identity means no title, not the newest stranger's",
          blind.title == nil, "got \(blind.title ?? "nil")")
    // The blank is not a blank row: the handle is still a fact about this session, and it is what
    // the rung below the title is for.
    expect("but the handle beside it is still answered", blind.handle, "clawdline-cb")
    expect("and with no transcript directory either, the handle is the whole answer",
           SessionNaming.look(at: target, startedAt: now.addingTimeInterval(-10),
                              sources: sources(registry: registryEntry(sessionID: mine,
                                                                       name: "clawdline-cb"),
                                               transcripts: nil)),
           SessionNaming.Name(title: nil, handle: "clawdline-cb"))
}

group("a name survives its source going quiet") {
    defer { SessionNaming.lookForTesting = noSessionNames; SessionNaming.forgetForTesting() }
    let session = TargetSession(backend: .iterm, id: UUID().uuidString, name: "Default (python)",
                                tty: "/dev/ttys901", windowIndex: 0, tabIndex: 0,
                                assistant: .claude)
    SessionNaming.forgetForTesting()
    SessionNaming.lookForTesting = { _ in
        SessionNaming.Name(title: "Ledger reader layer", handle: "clawdline-cb")
    }
    expect("first look", session.displayLabel, "Ledger reader layer")

    SessionNaming.lookForTesting = { _ in .none }
    SessionNaming.expireForTesting()
    expect("a look that finds nothing at all leaves the name where it was",
           session.displayLabel, "Ledger reader layer")

    // The half that a whole-record merge would get wrong: the registry answered and the
    // transcript did not, so replacing the descriptive name with `clawdline-cb` would be this
    // fix reintroducing its own bug one rung down.
    SessionNaming.lookForTesting = { _ in SessionNaming.Name(title: nil, handle: "clawdline-cb") }
    SessionNaming.expireForTesting()
    expect("a handle arriving without a title does not demote the title",
           session.displayLabel, "Ledger reader layer")

    // A newer answer is still an answer.
    SessionNaming.lookForTesting = { _ in SessionNaming.Name(title: "renamed", handle: nil) }
    SessionNaming.expireForTesting()
    expect("a fresh title replaces the remembered one", session.displayLabel, "renamed")

    // A closed tab is never asked about again, so the only thing that can drop its name is a
    // reading of what is still on screen. `Orchestrator.pruneClosedHandoffTitles(visible:)` is
    // next door for the same reason, and states the sharper half: a terminal id is reusable, so
    // a name left under a closed tab's id is a name waiting to be handed to a later session.
    SessionNaming.forget(closedFrom: [session.id])
    SessionNaming.lookForTesting = { _ in .none }
    SessionNaming.expireForTesting()
    expect("a visible tab keeps its remembered name", session.displayLabel, "renamed")
    SessionNaming.forget(closedFrom: [])
    check("a closed tab's reusable id does not carry its name into a later session",
          SessionNaming.title(of: session) == nil)
}

group("a remembered name belongs to a conversation, not to a tab") {
    let started = Date(timeIntervalSince1970: 1_787_900_000)
    let now = Date(timeIntervalSince1970: 1_787_900_500)
    let remembered = SessionNaming.Remembered(
        at: Date(timeIntervalSince1970: 1_787_900_100), startedAt: started,
        name: SessionNaming.Name(title: "Ledger reader layer", handle: "clawdline-cb"))
    check("a start time that has not moved keeps the name",
          SessionNaming.reconcile(remembered: remembered, found: .none,
                                  startedAt: started, now: now)?.name.title
              == "Ledger reader layer")
    check("a start time nothing could measure is not evidence of a change",
          SessionNaming.reconcile(remembered: remembered, found: .none,
                                  startedAt: nil, now: now)?.name.title == "Ledger reader layer")
    check("but that look still leaves the last start time to compare against",
          SessionNaming.reconcile(remembered: remembered, found: .none,
                                  startedAt: nil, now: now)?.startedAt == started)
    check("a different process in the same tab is a different conversation",
          SessionNaming.reconcile(remembered: remembered, found: .none,
                                  startedAt: started.addingTimeInterval(600), now: now) == nil)
    check("and it is named by what is there now, not by what was",
          SessionNaming.reconcile(remembered: remembered,
                                  found: SessionNaming.Name(title: nil, handle: "clawdline-fa"),
                                  startedAt: started.addingTimeInterval(600),
                                  now: now)?.name == SessionNaming.Name(title: nil,
                                                                        handle: "clawdline-fa"))
    check("nothing remembered and nothing found is nothing kept",
          SessionNaming.reconcile(remembered: nil, found: .none,
                                  startedAt: started, now: now) == nil)

    // The mirror of the case above it, which had no test and is the one that carries a name
    // across a conversation change. A remembered look that could not measure a start time has no
    // baseline under it — and `reconcile` only ever fills the baseline in once, so reading "we
    // still cannot compare" as "still the same conversation" would keep that name for as long as
    // the tab is open. A measurement arriving where there was none is a new baseline.
    let baseless = SessionNaming.Remembered(
        at: Date(timeIntervalSince1970: 1_787_900_100), startedAt: nil,
        name: SessionNaming.Name(title: "Ledger reader layer", handle: "clawdline-cb"))
    check("a name remembered with no start time under it is not carried onto a measured one",
          SessionNaming.continues(baseless, startedAt: started) == false)
    check("so a look that finally measures one starts the record over",
          SessionNaming.reconcile(remembered: baseless, found: .none,
                                  startedAt: started, now: now) == nil)
    check("and what it does find is what the tab is called, on its own",
          SessionNaming.reconcile(remembered: baseless,
                                  found: SessionNaming.Name(title: nil, handle: "clawdline-fa"),
                                  startedAt: started, now: now)?.name
              == SessionNaming.Name(title: nil, handle: "clawdline-fa"))
    // Still nothing measured on either side is still nothing that happened: that is the quiet
    // source the rule above is about, and it must not blank the row.
    check("but two looks that both failed to measure are not a change",
          SessionNaming.reconcile(remembered: baseless, found: .none,
                                  startedAt: nil, now: now)?.name.title == "Ledger reader layer")
}

group("session titles are normalized, persisted and bounded") {
    expect("line breaks and controls become one ordinary space",
           Config.normalizedSessionTitle("  first\nsecond\u{0007}\tthird  "),
           "first second third")
    check("only whitespace clears a title",
          Config.normalizedSessionTitle(" \n\t ") == nil)
    expect("the length boundary is accepted",
           Config.normalizedSessionTitle(String(repeating: "x", count: Config.sessionTitleLimit))?.count,
           Config.sessionTitleLimit)

    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-session-titles-\(UUID().uuidString)",
                                isDirectory: true)
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let config = Config(directoryForTesting: directory)
    let now = Date()
    expect("setting returns the normalized title",
           config.setSessionTitle("  release\nroom  ", sessionID: "claude-one",
                                  terminalID: "terminal-one", now: now),
           "release room")
    expect("the stable Claude id is preferred after a terminal changes",
           config.sessionTitle(sessionID: "claude-one", terminalID: "terminal-two"),
           "release room")
    expect("the terminal id remains a fallback when there is no hook id",
           config.sessionTitle(sessionID: nil, terminalID: "terminal-one"),
           "release room")
    config.save()
    let loaded = Config(directoryForTesting: directory)
    expect("a saved title makes a config round trip",
           loaded.sessionTitle(sessionID: "claude-one", terminalID: "terminal-two"),
           "release room")
    // Overlong before cleared, because the two used to be the same outcome: the removal ran first
    // and only the append was bounded, so a title too long to store took the stored one with it —
    // a name silently lost to a request that was answered `400`.
    let tooLong = String(repeating: "x", count: Config.sessionTitleLimit + 1)
    check("an overlong title is refused without taking the stored one with it",
          loaded.setSessionTitle(tooLong, sessionID: "claude-one",
                                 terminalID: "terminal-one", now: now) == nil
              && loaded.sessionTitle(sessionID: "claude-one",
                                     terminalID: "terminal-one") == "release room")
    check("clearing removes both lookup keys",
          loaded.setSessionTitle(" \n ", sessionID: "claude-one",
                                 terminalID: "terminal-one", now: now) == nil
              && loaded.sessionTitle(sessionID: "claude-one", terminalID: "terminal-one") == nil)

    let rows: [[String: Any]] = (0..<(Config.sessionTitleCapacity + 3)).map { index in
        ["title": "title \(index)", "terminal_id": "terminal-\(index)",
         "updated_at": now.addingTimeInterval(Double(index)).timeIntervalSince1970]
    }
    let file = directory.appendingPathComponent("config.json")
    let data = try! JSONSerialization.data(withJSONObject: ["session_titles": rows])
    try! data.write(to: file)
    let bounded = Config(directoryForTesting: directory)
    check("loading keeps only the newest bounded set",
          bounded.sessionTitle(sessionID: nil, terminalID: "terminal-0") == nil
              && bounded.sessionTitle(sessionID: nil,
                                      terminalID: "terminal-\(Config.sessionTitleCapacity + 2)")
                  == "title \(Config.sessionTitleCapacity + 2)")

    let stale = [["title": "old", "terminal_id": "old-terminal",
                  "updated_at": now.addingTimeInterval(-Config.sessionTitleLifetime - 1)
                    .timeIntervalSince1970]]
    try! JSONSerialization.data(withJSONObject: ["session_titles": stale]).write(to: file)
    let cleaned = Config(directoryForTesting: directory)
    check("expired closed-session titles are discarded",
          cleaned.sessionTitle(sessionID: nil, terminalID: "old-terminal") == nil)
}

group("renaming never changes the terminal label used to locate transcripts") {
    let target = TargetSession(backend: .iterm, id: "terminal-name-test",
                               name: "Claude Code", tty: "/dev/ttys099",
                               windowIndex: 0, tabIndex: 0, assistant: .claude)
    let display = TargetSession.preferredDisplayLabel(
        manualTitle: "Human title", orchestratorTitle: nil, conversationTitle: nil,
        threadName: nil, handle: nil, coordinate: target.coordinate)
    expect("the display calculation can change independently", display, "Human title")

    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-title-transcript-\(UUID().uuidString)",
                                isDirectory: true)
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let transcript = directory.appendingPathComponent("matching.jsonl")
    try! Data("{\"customTitle\":\"Claude Code\"}\n".utf8).write(to: transcript)
    // A second conversation in the same project, named what this session is *displayed* as. It is
    // what makes the check below a regression rather than a restatement: comparing `target.label`
    // with a copy of itself passes for every possible edit to this repository, and matching on the
    // terminal title passed before any of this existed. What has to stay true is that the two
    // strings have come apart and that the lookup is still given the terminal one — because the
    // day a rename feeds back into `label`, or a caller hands `displayLabel` to `locate`, this
    // session gets attached to somebody else's file, confidently.
    let impostor = directory.appendingPathComponent("named-like-the-display.jsonl")
    try! Data("{\"customTitle\":\"Human title\"}\n".utf8).write(to: impostor)
    expect("transcript matching still receives the unchanged terminal title",
           Transcript.locate(in: directory, tabTitle: target.label), transcript)
    check("the display title and the terminal label really have come apart",
          display != target.label)
    expect("and looking one up by the display title would land on the other conversation",
           Transcript.locate(in: directory, tabTitle: display), impostor)
}

group("a person's session title belongs to the conversation, not to the tab") {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-title-conversation-\(UUID().uuidString)",
                                isDirectory: true)
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let config = Config(directoryForTesting: directory)
    let now = Date()
    let started = Date(timeIntervalSince1970: 1_700_000_000)
    let restarted = started.addingTimeInterval(3_600)

    // The failure this exists for: iTerm2 keeps a tab's session UUID when the assistant inside it
    // exits, so a name stored against the terminal alone was handed to the next conversation for
    // up to ninety days — and, being a person's name, it outranked the task title of a tab this
    // app had opened for a dispatch.
    config.setSessionTitle("Release room", sessionID: "claude-one", terminalID: "terminal-one",
                           startedAt: started, now: now)
    expect("the conversation that chose the name still reads it",
           config.sessionTitle(sessionID: "claude-one", terminalID: "terminal-one",
                               conversationStart: { started }),
           "Release room")
    check("the next conversation in that tab does not inherit it",
          config.sessionTitle(sessionID: "claude-two", terminalID: "terminal-one",
                              conversationStart: { restarted }) == nil)
    expect("and a resumed conversation keeps it wherever it is resumed",
           config.sessionTitle(sessionID: "claude-one", terminalID: "terminal-two",
                               conversationStart: { nil }),
           "Release room")

    // Codex and a Claude without hooks have no conversation id to offer, so the fallback has to
    // stand on the process instead of on the tab.
    config.setSessionTitle("Night shift", sessionID: nil, terminalID: "terminal-codex",
                           startedAt: started, now: now)
    expect("a session with no conversation id is matched by the process in its tab",
           config.sessionTitle(sessionID: nil, terminalID: "terminal-codex",
                               conversationStart: { started.addingTimeInterval(2) }),
           "Night shift")
    check("the next assistant started in that tab is not that session",
          config.sessionTitle(sessionID: nil, terminalID: "terminal-codex",
                              conversationStart: { restarted }) == nil)
    check("and a tab with nothing running in it cannot claim an assistant's name",
          config.sessionTitle(sessionID: nil, terminalID: "terminal-codex",
                              conversationStart: { nil }) == nil)

    // A name can be chosen in the moment before the hook note arrives, so a row carrying no
    // conversation id is still this conversation's when the process agrees.
    config.setSessionTitle("Before the hook arrived", sessionID: nil, terminalID: "terminal-late",
                           startedAt: started, now: now)
    expect("a name chosen before the hook note arrived still belongs to that process",
           config.sessionTitle(sessionID: "claude-late", terminalID: "terminal-late",
                               conversationStart: { started }),
           "Before the hook arrived")

    check("two readings of one process may disagree by a second or two",
          Config.sameConversation(started, started.addingTimeInterval(2)))
    check("but not by an hour", !Config.sameConversation(started, restarted))
    check("nothing running is the same fact on both sides", Config.sameConversation(nil, nil))
    check("and it is not the same fact as a running process",
          !Config.sameConversation(nil, started))
    check("nor the other way round", !Config.sameConversation(started, nil))

    config.save()
    let loaded = Config(directoryForTesting: directory)
    expect("the credential survives a config round trip",
           loaded.sessionTitle(sessionID: nil, terminalID: "terminal-codex",
                               conversationStart: { started }),
           "Night shift")
    check("and it is still the credential after loading, not just a stored field",
          loaded.sessionTitle(sessionID: nil, terminalID: "terminal-codex",
                              conversationStart: { restarted }) == nil)
}

group("the newest of a local name and a terminal /rename wins") {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-title-freshness-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let config = Config(directoryForTesting: directory)
    let now = Date()

    // 1. A local name chosen when the transcript had no `/rename` yet; the terminal gets one
    // afterward. The newer utterance — the terminal's — has to win.
    config.setSessionTitle("Web-chosen name", sessionID: "claude-fresh", terminalID: "terminal-fresh",
                           seenCustomTitle: nil, seenTranscriptPath: "/tmp/fake-fresh.jsonl", now: now)
    var reads = 0
    expect("a later /rename in the terminal takes the label away from the local name",
           config.sessionTitle(sessionID: "claude-fresh", terminalID: "terminal-fresh",
                               currentCustomTitle: { path in
                                   reads += 1
                                   check("it is asked about the transcript this row recorded",
                                         path == "/tmp/fake-fresh.jsonl")
                                   return "Renamed from the terminal"
                               }),
           nil)
    expect("one read decided it", reads, 1)
    // 4. Superseded, not merely out-voted: the row is gone, so a second read never asks the
    // transcript again — there is nothing left to compare.
    expect("the superseded row does not answer a second time",
           config.sessionTitle(sessionID: "claude-fresh", terminalID: "terminal-fresh",
                               currentCustomTitle: { _ in reads += 1; return "Renamed from the terminal" }),
           nil)
    expect("and was not asked, because there was nothing left standing to ask about", reads, 1)

    // 2. The guard against over-correction this line exists for: an unchanged customTitle is not
    // itself a rename. A stored value read back identical must never evict the local name — the
    // naive version of this feature is "any customTitle beats the local name", and that is wrong.
    config.setSessionTitle("Stays local", sessionID: "claude-stable", terminalID: "terminal-stable",
                           seenCustomTitle: "Same as ever", seenTranscriptPath: "/tmp/fake-stable.jsonl",
                           now: now)
    expect("no change in the transcript means the local name still wins",
           config.sessionTitle(sessionID: "claude-stable", terminalID: "terminal-stable",
                               currentCustomTitle: { _ in "Same as ever" }),
           "Stays local")
    expect("and it survives being asked more than once",
           config.sessionTitle(sessionID: "claude-stable", terminalID: "terminal-stable",
                               currentCustomTitle: { _ in "Same as ever" }),
           "Stays local")

    // 3. A local name taken after a customTitle already existed: the baseline is non-nil, and as
    // long as nothing has changed since, the local name still wins — the old rename is not itself
    // treated as a supersession just because it predates the local name.
    config.setSessionTitle("Chosen after an old rename", sessionID: "claude-after",
                           terminalID: "terminal-after", seenCustomTitle: "An old rename",
                           seenTranscriptPath: "/tmp/fake-after.jsonl", now: now)
    expect("a pre-existing rename is the baseline, not a later change",
           config.sessionTitle(sessionID: "claude-after", terminalID: "terminal-after",
                               currentCustomTitle: { _ in "An old rename" }),
           "Chosen after an old rename")

    // No baseline at all — the transcript could not be resolved when the name was set, the shape
    // a non-Claude session or a not-yet-written transcript takes — is not evidence of anything,
    // so the local name is never evicted for want of something to compare it against.
    config.setSessionTitle("No baseline to compare", sessionID: "claude-none",
                           terminalID: "terminal-none", now: now)
    expect("with nothing to compare against, the local name always wins",
           config.sessionTitle(sessionID: "claude-none", terminalID: "terminal-none",
                               currentCustomTitle: { _ in "Anything at all" }),
           "No baseline to compare")

    // 5. Clearing is untouched by any of this: it still returns to the automatic label.
    config.setSessionTitle("", sessionID: "claude-stable", terminalID: "terminal-stable", now: now)
    check("clearing still returns to the automatic label",
          config.sessionTitle(sessionID: "claude-stable", terminalID: "terminal-stable",
                              currentCustomTitle: { _ in "Same as ever" }) == nil)

    // The baseline round-trips through a save, so a rename that happens while the app is closed
    // is still caught on the first read after it reopens.
    config.setSessionTitle("Round-trips too", sessionID: "claude-persist", terminalID: "terminal-persist",
                           seenCustomTitle: "before restart", seenTranscriptPath: "/tmp/fake-persist.jsonl",
                           now: now)
    config.save()
    let loaded = Config(directoryForTesting: directory)
    expect("an unchanged transcript still wins after a restart",
           loaded.sessionTitle(sessionID: "claude-persist", terminalID: "terminal-persist",
                               currentCustomTitle: { _ in "before restart" }),
           "Round-trips too")
    expect("and a rename typed while the app was closed is noticed on the first read back",
           loaded.sessionTitle(sessionID: "claude-persist", terminalID: "terminal-persist",
                               currentCustomTitle: { _ in "renamed while closed" }),
           nil)

    // 6. Every existing title-precedence test above this one must still pass unmodified — the
    // suite run this change was verified against is the proof, not a restatement here.
}

group("a config write that did not happen is not reported as saved") {
    let writable = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-title-durable-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: writable, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: writable) }
    let stored = Config(directoryForTesting: writable)
    stored.setSessionTitle("Release room", sessionID: nil, terminalID: "terminal-durable")
    check("a write that landed says so", stored.save())

    // A real failure, not a stub: the settings directory is a regular file, so `createDirectory`
    // and the write both fail the way they would on a full or read-only disk. `local_applied` is
    // what the whole downstream design leans on — a busy Claude is deliberately not queued
    // because the local name is said to be durable — so it may not be a constant.
    let blocked = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-title-blocked-\(UUID().uuidString)")
    try! Data("this is a file where a directory should be\n".utf8).write(to: blocked)
    defer { try? FileManager.default.removeItem(at: blocked) }
    let broken = Config(directoryForTesting: blocked)
    broken.setSessionTitle("Release room", sessionID: nil, terminalID: "terminal-blocked")
    check("a write that could not happen says that instead", !broken.save())
    expect("and the name is still in use, which is why the route answers 200",
           broken.sessionTitle(sessionID: nil, terminalID: "terminal-blocked"),
           "Release room")
}

group("clearing a title takes the Codex name off Clawdline's surfaces too") {
    let target = TargetSession(backend: .iterm, id: "terminal-codex-clear",
                               name: "codex", tty: "/dev/ttys077",
                               windowIndex: 0, tabIndex: 3, assistant: .codex)
    // Through the label rather than only through the cache, and with no orchestrator title in
    // the way: a task title would answer this expression whether the cache had been cleared or
    // not, which is a check that passes for every possible edit to this repository.
    func label() -> String {
        TargetSession.preferredDisplayLabel(
            manualTitle: nil, orchestratorTitle: nil, conversationTitle: nil,
            threadName: CodexNaming.shared.title(for: target), handle: nil,
            coordinate: target.coordinate)
    }
    CodexNaming.shared.rememberForTesting("Release room", threadID: "thread-clear",
                                          targetID: target.id)
    expect("the name a person typed is what the label draws", label(), "Release room")
    CodexNaming.shared.forget(target: target)
    check("clearing drops it from the display cache",
          CodexNaming.shared.title(for: target) == nil)
    // The half that stays: the thread keeps the name in Codex's own metadata, because
    // `thread/name/set` has no undo and this app does not know what Codex would have called it.
    // What has to come back here is the automatic label, not the name that was just cleared —
    // and the automatic label is no longer the tab's title, which for this tab reads `codex`.
    expect("so the label falls back to where the tab is", label(), target.coordinate)
    check("and not to what the terminal calls itself", label() != target.label)
}

group("resuming a known conversation carries its title onto the new terminal") {
    let conversationID = "11111111-1111-4111-8111-111111111111"
    let title = "Restore session titles"
    let target = TargetSession(backend: .iterm, id: "terminal-resumed-title",
                               name: "Default", tty: "/dev/ttys078",
                               windowIndex: 0, tabIndex: 11, assistant: .codex)
    _ = StartPoints.resumed(.started(id: target.id, backend: target.backend),
                            conversationID: conversationID, title: title, assistant: .codex)
    expect("the new row has the selected title before another inventory read",
           CodexNaming.shared.title(for: target), title)
    check("a display-only resume hint cannot become the id used by /rename",
          CodexNaming.shared.threadID(for: target) == nil)
    CodexNaming.shared.forget(target: target)

    let refused = TargetSession(backend: .iterm, id: "terminal-refused-resume",
                                name: "Default", tty: "/dev/ttys080",
                                windowIndex: 0, tabIndex: 13, assistant: .codex)
    _ = StartPoints.resumed(
        .refused(status: 502, code: "terminal_io_failed", message: "no tab", app: nil),
        conversationID: conversationID, title: title, assistant: .codex)
    check("a resume that opened no terminal seeds no title",
          CodexNaming.shared.title(for: refused) == nil)

    let absent = TargetSession(backend: .iterm, id: "terminal-titleless-resume",
                               name: "Default", tty: "/dev/ttys081",
                               windowIndex: 0, tabIndex: 14, assistant: .codex)
    _ = StartPoints.resumed(.started(id: absent.id, backend: absent.backend),
                            conversationID: conversationID, title: nil, assistant: .codex)
    check("an authorized resume with no title does not invent one",
          CodexNaming.shared.title(for: absent) == nil)

    let long = TargetSession(backend: .iterm, id: "terminal-long-resume",
                             name: "Default", tty: "/dev/ttys082",
                             windowIndex: 0, tabIndex: 15, assistant: .codex)
    _ = StartPoints.resumed(.started(id: long.id, backend: long.backend),
                            conversationID: conversationID,
                            title: String(repeating: "x", count: 200), assistant: .codex)
    expect("a history preview is bounded before becoming a session label",
           CodexNaming.shared.title(for: long)?.count, 80)
    CodexNaming.shared.forget(target: long)

    let claude = TargetSession(backend: .iterm, id: "terminal-resumed-claude-title",
                               name: "Default", tty: "/dev/ttys079",
                               windowIndex: 0, tabIndex: 12, assistant: .claude)
    _ = StartPoints.resumed(.started(id: claude.id, backend: claude.backend),
                            conversationID: conversationID, title: title, assistant: .claude)
    expect("Claude receives the same display-only resume hint",
           CodexNaming.shared.title(for: claude), title)
    CodexNaming.shared.forget(target: claude)
    check("clearing a Claude title removes its resume hint from the shared cache",
          CodexNaming.shared.title(for: claude) == nil)
}

group("a rename is not typed into a session that is showing a menu") {
    var looks = 0
    let idle = SessionTitleSync.action(assistant: .claude, state: .idle,
                                       clearing: false, codexThreadID: nil)
    // The cached `idle` this starts from can be twenty seconds old while the app is in the
    // background, which is the state the Mac is in whenever somebody renames from a phone. A
    // slash command sent to a menu confirms whichever row is highlighted; `/send` pays for the
    // same capture and its comment records what that cost the last time nobody paid it.
    expect("a screen with a menu on it is busy, whatever the cached state said",
           SessionTitleSync.confirmed(idle, showingMenu: { looks += 1; return true }), .busy)
    expect("a screen with no menu on it still receives the rename",
           SessionTitleSync.confirmed(idle, showingMenu: { looks += 1; return false }),
           .renameClaude)
    expect("both of those read the screen", looks, 2)

    let quiet: [SessionTitleSync.Action] = [.localOnly, .busy, .unavailable,
                                            .renameCodex(threadID: "thread-one")]
    for action in quiet {
        expect("an action that types nothing is returned unchanged",
               SessionTitleSync.confirmed(action, showingMenu: { looks += 1; return true }),
               action)
    }
    expect("and none of them paid for a screen capture", looks, 2)
}

group("session-title downstream synchronization never interrupts Claude") {
    expect("an idle Claude session receives its rename",
           SessionTitleSync.action(assistant: .claude, state: .idle,
                                   clearing: false, codexThreadID: nil),
           .renameClaude)
    expect("a working Claude session stays local",
           SessionTitleSync.action(assistant: .claude, state: .working("turn"),
                                   clearing: false, codexThreadID: nil),
           .busy)
    expect("a waiting Claude session is never answered by a rename",
           SessionTitleSync.action(assistant: .claude, state: .waiting,
                                   clearing: false, codexThreadID: nil),
           .busy)
    // A screen this app could not read is not a session it may type into. Same answer as `busy`,
    // and the reason it is asserted separately is that the two are different facts — `docs/api.md`
    // has to describe this one as well, or `busy` becomes the word for "we did not look".
    expect("a session whose screen could not be read is not typed into either",
           SessionTitleSync.action(assistant: .claude, state: .unknown,
                                   clearing: false, codexThreadID: nil),
           .busy)
    expect("Codex metadata may be renamed without waiting for idle",
           SessionTitleSync.action(assistant: .codex, state: .working("turn"),
                                   clearing: false, codexThreadID: "thread-one"),
           .renameCodex(threadID: "thread-one"))
    expect("a Codex session with no thread to name says so rather than reporting success",
           SessionTitleSync.action(assistant: .codex, state: .idle,
                                   clearing: false, codexThreadID: nil),
           .unavailable)
    expect("a plain shell keeps the name locally and claims nothing downstream",
           SessionTitleSync.action(assistant: nil, state: .idle,
                                   clearing: false, codexThreadID: nil),
           .localOnly)
    expect("clearing is local-only for every assistant",
           SessionTitleSync.action(assistant: .claude, state: .idle,
                                   clearing: true, codexThreadID: nil),
           .localOnly)
}

group("which assistant a process name stands for") {
    expect("a bare name", Assistant.named("codex"), .codex)
    expect("a path to one", Assistant.named("/opt/homebrew/bin/claude"), .claude)
    check("a longer name is somebody else's program", Assistant.named("codexctl") == nil)
    check("and so is a directory that merely contains one", Assistant.named("/Users/me/.codex") == nil)
    expect("each leaves on its own word", Assistant.claude.quitLine, "/exit")
    expect("and they are not the same word", Assistant.codex.quitLine, "/quit")
}

group("assistant product marks load at row size") {
    for assistant in Assistant.allCases {
        let image = assistant.logoImage(height: 11)
        check("\(assistant.rawValue) has a vector mark", image != nil)
        expectClose("\(assistant.rawValue) mark is square", image?.size.width ?? 0, 11)
        expectClose("\(assistant.rawValue) mark has the requested height", image?.size.height ?? 0, 11)
    }
}

group("the line a new tab is given names the assistant") {
    // Every line this app types now opens with `env -u …` between the `&&` and the program
    // name. What that prefix is exactly is pinned in "a new tab is not handed the identity of
    // whatever launched the terminal"; here it is composed, so these stay assertions about
    // the thing they were written for.
    expect("Claude Code, as it always was",
           StartPoints.itermLine(cwd: "/Users/me/code/thing"),
           "cd '/Users/me/code/thing' && " + Assistant.claude.dropInheritedIdentity + "claude")
    expect("Codex, by the same route",
           StartPoints.itermLine(cwd: "/Users/me/code/thing", assistant: .codex),
           "cd '/Users/me/code/thing' && " + Assistant.codex.dropInheritedIdentity + "codex")
    // The quoting is the same quoting, which is the point of it being one function.
    expect("and a directory with a quote in it survives",
           StartPoints.itermLine(cwd: "/Users/me/it's", assistant: .codex),
           "cd '/Users/me/it'\\''s' && " + Assistant.codex.dropInheritedIdentity + "codex")
}
}
