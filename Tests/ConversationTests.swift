import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - Picking a recorded conversation back up

/// What the resume route is allowed to put on a command line, and nothing else.
///
/// `--resume` takes an **optional** value, which is the whole reason this is exact rather than
/// merely shell-safe: a value the CLI cannot read as an id becomes a search term for, and the
/// session opens its own picker in a tab nobody is sitting at. So the failure being tested for
/// here is a session that never starts, not an injection — although it refuses those too.






/// The list itself, against a project folder written for the occasion.
///
/// `dir` and `open` are parameters for exactly this: the real one is `~/.claude/projects/<slug>`
/// and the real answer to "what is being written to" is the session list, neither of which a test
/// can describe. Everything else here is the code the app runs.




/// Half of what is in a project folder is not a conversation somebody had, and every judgement
/// about which half is read off a field rather than guessed from the contents.


/// The trap that made a fixed-size head the wrong shape for this.


/// The title read, and the whole-file half of it that cost nine seconds.

func runConversationTests() {
group("a conversation id is a UUID or it is nothing") {
    check("the shape both CLIs write",
          StartPoints.sessionName("105344fb-c769-4b37-b766-403b410897eb") != nil)
    expect("upper case is not that shape — Claude Code writes lower",
           StartPoints.sessionName("105344FB-c769-4b37-b766-403b410897eb"), nil)
    expect("nor is a prefix of one", StartPoints.sessionName("105344fb"), nil)
    expect("nor is the empty string, which `--resume` would read as no value at all",
           StartPoints.sessionName(""), nil)
    expect("nor is nothing", StartPoints.sessionName(nil), nil)
    expect("the groups have to be the right lengths",
           StartPoints.sessionName("105344fb-c769-4b37-b766-403b410897ebcd"), nil)
    expect("and be the right number of them",
           StartPoints.sessionName("105344fb-c769-4b37-b766-403b-10897eb"), nil)
    expect("g is not hex", StartPoints.sessionName("g05344fb-c769-4b37-b766-403b410897eb"), nil)
    // The three that would matter if any of the above were a length check rather than an
    // alphabet one. None of them is 36 characters, and each says a different thing if it got out.
    expect("a semicolon is not a conversation",
           StartPoints.sessionName("105344fb; rm -rf ~"), nil)
    expect("nor is a substitution", StartPoints.sessionName("$(id)"), nil)
    expect("nor is a flag", StartPoints.sessionName("--dangerously-skip-permissions"), nil)
}

group("the line that picks a conversation back up") {
    expect("Claude Code takes a flag, and it comes before everything else",
           StartPoints.itermLine(cwd: "/Users/me/code/thing",
                                 resume: "105344fb-c769-4b37-b766-403b410897eb"),
           "cd '/Users/me/code/thing' && " + Assistant.claude.dropInheritedIdentity
             + "claude --resume 105344fb-c769-4b37-b766-403b410897eb")
    // The route selects this spelling from the assistant segment rather than accepting a command.
    expect("Codex spells it as a subcommand",
           StartPoints.itermLine(cwd: "/a/b", assistant: .codex,
                                 resume: "105344fb-c769-4b37-b766-403b410897eb"),
           "cd '/a/b' && " + Assistant.codex.dropInheritedIdentity
             + "codex resume 105344fb-c769-4b37-b766-403b410897eb")
    expect("an id that is not one leaves no flag behind rather than a bare `--resume`",
           StartPoints.itermLine(cwd: "/a/b", resume: "not-an-id"),
           "cd '/a/b' && " + Assistant.claude.dropInheritedIdentity + "claude")
    expect("and the ordinary line is exactly what it was",
           StartPoints.itermLine(cwd: "/a/b"),
           "cd '/a/b' && " + Assistant.claude.dropInheritedIdentity + "claude")
    check("nothing a client sent is anywhere in it",
          !StartPoints.itermLine(cwd: "/a/b", resume: "$(id)").contains("$"))
}

group("Codex threads become conversations that can be picked back up") {
    let named = "11111111-1111-4111-8111-111111111111"
    let previewed = "22222222-2222-4222-8222-222222222222"
    let dispatched = "33333333-3333-4333-8333-333333333333"
    let response: [String: Any] = ["result": ["data": [
        ["id": named, "cwd": "/repo", "name": " Persisted Codex title ",
         "preview": "the opening request", "updatedAt": 3000],
        ["id": previewed, "cwd": "/repo", "name": NSNull(),
         "preview": "fallback opening\nand the rest", "updatedAt": 2000],
        ["id": dispatched, "cwd": "/repo", "name": NSNull(),
         "preview": Orchestrator.briefingOpening + " child", "updatedAt": 1000],
        ["id": "not-a-uuid", "cwd": "/repo", "name": "not a thread",
         "preview": "bad id", "updatedAt": 500],
        ["id": "44444444-4444-4444-8444-444444444444", "cwd": "/elsewhere",
         "name": "wrong project", "preview": "wrong cwd", "updatedAt": 400],
    ]]]

    let rows = CodexNaming.listedThreads(in: response, cwd: "/repo", open: [previewed])
    expect("named and recognisable threads survive, newest first", rows.map(\.id),
           [named, previewed])
    expect("Codex's persisted name wins", rows.first?.title, "Persisted Codex title")
    expect("an unnamed thread falls back to one line of its preview",
           rows.last?.title, "fallback opening")
    expect("a matching live thread is marked", rows.filter(\.live).map(\.id), [previewed])
    check("a dispatched child is not offered as somebody's earlier conversation",
          !rows.contains { $0.id == dispatched })
}

group("what has already been said in a place") {
    let root = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-past-\(getpid())", isDirectory: true)
    try? FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }

    func write(_ name: String, _ lines: [String], at: Date? = nil) {
        let url = root.appendingPathComponent(name)
        try? lines.joined(separator: "\n").write(to: url, atomically: true, encoding: .utf8)
        if let at {
            try? FileManager.default.setAttributes([.modificationDate: at], ofItemAtPath: url.path)
        }
    }

    let titled = "11111111-1111-4111-8111-111111111111.jsonl"
    let renamed = "22222222-2222-4222-8222-222222222222.jsonl"
    let untitled = "33333333-3333-4333-8333-333333333333.jsonl"
    let silent = "44444444-4444-4444-8444-444444444444.jsonl"

    write(titled, [#"{"type":"user","message":{"role":"user","content":"first thing said"}}"#,
                   #"{"type":"ai-title","aiTitle":"What Claude Code called it"}"#],
          at: Date(timeIntervalSince1970: 3000))
    write(renamed, [#"{"type":"ai-title","aiTitle":"What Claude Code called it"}"#,
                    #"{"customTitle":"What a person called it"}"#],
          at: Date(timeIntervalSince1970: 2000))
    write(untitled, [#"{"type":"user","isSidechain":true,"message":{"role":"user","content":"an agent's turn"}}"#,
                     #"{"type":"user","toolUseResult":{},"message":{"role":"user","content":"a tool's answer"}}"#,
                     #"{"type":"user","message":{"role":"user","content":[{"type":"text","text":"the opening line\nand more of it"}]}}"#],
          at: Date(timeIntervalSince1970: 1000))
    write(silent, [#"{"type":"summary","summary":"nobody typed anything"}"#],
          at: Date(timeIntervalSince1970: 500))
    // Not a conversation: a subagent's folder, and a file whose name is not an id.
    try? FileManager.default.createDirectory(
        at: root.appendingPathComponent("55555555-5555-4555-8555-555555555555.jsonl"),
        withIntermediateDirectories: true)
    write("notes.jsonl", [#"{"type":"ai-title","aiTitle":"not a session"}"#])

    let place = StartPoints.Place(id: "p", path: "/Users/me/code/thing", label: "thing", at: Date())
    let rows = StartPoints.past(in: place, dir: root, open: [])

    expect("the ones that can be named, newest first", rows.map(\.id), [
        "11111111-1111-4111-8111-111111111111",
        "22222222-2222-4222-8222-222222222222",
        "33333333-3333-4333-8333-333333333333",
    ])
    expect("Claude Code's own title", rows.first?.title, "What Claude Code called it")
    expect("a rename beats it", rows.dropFirst().first?.title, "What a person called it")
    expect("and with neither, the first thing a person typed — one line of it",
           rows.last?.title, "the opening line")
    check("a transcript nobody typed into is left out rather than shown untitled",
          !rows.contains { $0.id.hasPrefix("4444") })
    check("a subagent's folder is not a conversation",
          !rows.contains { $0.id.hasPrefix("5555") })
    check("and neither is a file that is not named after one",
          !rows.contains { $0.title == "not a session" })

    check("nothing is open, so nothing says it is", rows.allSatisfy { !$0.live })
    let busy = StartPoints.past(in: place, dir: root,
                                open: [root.appendingPathComponent(titled)
                                        .resolvingSymlinksInPath().path])
    expect("and the one being written to is the one that says so",
           busy.filter(\.live).map(\.id), ["11111111-1111-4111-8111-111111111111"])

    // The cap is what the reply pages against: `pastPayload` asks for one more than it sends and
    // says `more` when it got it, so a list that stops is one that says so rather than one that
    // quietly ends. That only works if the cap is exact.
    expect("the cap is a cap", StartPoints.past(in: place, limit: 2, dir: root, open: []).count, 2)
    expect("and asking for one more gets one more",
           StartPoints.past(in: place, limit: 3, dir: root, open: []).count, 3)

    expect("an id off the list resolves",
           StartPoints.past(withID: "22222222-2222-4222-8222-222222222222", in: place,
                            dir: root, open: [])?.title,
           "What a person called it")
    expect("one that was never handed out does not",
           StartPoints.past(withID: "99999999-9999-4999-8999-999999999999", in: place,
                            dir: root, open: []), nil)
}

group("an agent's turn is not the opening of a conversation") {
    expect("a sidechain is stepped over",
           StartPoints.front(inText: #"{"type":"user","isSidechain":true,"message":{"role":"user","content":"an agent"}}"#).opening,
           nil)
    expect("so is a tool's answer quoted back",
           StartPoints.front(inText: #"{"type":"user","toolUseResult":{},"message":{"role":"user","content":"a result"}}"#).opening,
           nil)
    expect("a long opening is cut where it can be read",
           StartPoints.front(inText: #"{"type":"user","message":{"role":"user","content":"aaaaaaaaaa"}}"#, limit: 4).opening,
           "aaaa\u{2026}")
    expect("and an empty one is not an opening at all",
           StartPoints.front(inText: #"{"type":"user","message":{"role":"user","content":"   "}}"#).opening,
           nil)
}

group("what is not a conversation somebody had") {
    func front(_ json: String) -> StartPoints.Front { StartPoints.front(inText: json) }

    let typed = #"{"type":"user","entrypoint":"cli","promptSource":"typed","message":{"role":"user","content":"where did the census go"}}"#
    check("somebody at a terminal is one", front(typed).isConversation)
    expect("and it is named by what they said", front(typed).opening, "where did the census go")

    // `claude -p "what is 2+2"` writes a transcript like everything else. Eleven of them were in
    // this repository's list under names like `Test` and `Hello`.
    let printed = #"{"type":"user","entrypoint":"sdk-cli","promptSource":"sdk","message":{"role":"user","content":"What is 2+2?"}}"#
    check("a -p run is not", !front(printed).isConversation)
    check("the entrypoint alone is enough",
          !front(#"{"type":"user","entrypoint":"sdk-cli","promptSource":"typed","message":{"role":"user","content":"x"}}"#).isConversation)
    check("and so is the prompt source alone",
          !front(#"{"type":"user","entrypoint":"cli","promptSource":"sdk","message":{"role":"user","content":"x"}}"#).isConversation)

    // Transcripts old enough not to record either field are still conversations. Absence is not
    // a refusal — the alternative is a list that empties itself on somebody's older machine.
    check("a transcript that records neither is still one",
          front(#"{"type":"user","message":{"role":"user","content":"x"}}"#).isConversation)

    // Its own id rather than the file's `taskID`: that one is declared further down, and a
    // global read before its initializer has run is not a test failure, it is a crash.
    let child = Orchestrator.firstLine(id: "0f8fad5b-d9cb-469f-a165-70867728950e", secret: "s")
    let briefed = "{\"type\":\"user\",\"entrypoint\":\"cli\",\"promptSource\":\"typed\",\"message\":"
        + "{\"role\":\"user\",\"content\":\""
        + child.replacingOccurrences(of: "\"", with: "\\\"")
        + "\"}}"
    check("a session this app dispatched is not one", !front(briefed).isConversation)
    check("and the words it is recognised by are the line a child is actually given",
          child.hasPrefix(Orchestrator.briefingOpening))

    // The turn has to *begin* with the briefing. This conversation's own transcript opens with a
    // question about that matching, containing those words — under a `contains` test it would
    // have filtered itself out of the list it was being read in.
    check("but a conversation that opens by asking about the briefing is still one",
          front(#"{"type":"user","entrypoint":"cli","message":{"role":"user","content":"why does the Clawdline CHILD agent for task matching live in Orchestrator?"}}"#)
            .isConversation)
}

group("the first turn is found however far in it sits") {
    let root = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-front-\(getpid())", isDirectory: true)
    try? FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }

    // A `file-history-snapshot` carries the contents of every file a session has edited, so in a
    // transcript of *this* app it contains the literal text `"type":"user"` — and it can be a
    // hundred and thirty kilobytes on its own. Four of the transcripts in this repository's own
    // folder open with one, including the conversation this test was written in. A reader that
    // narrowed by that substring before parsing, or that only ever looked at a fixed window off
    // the front, found no turn in any of them.
    let padding = String(repeating: #"{"type":"user","fake":1} "#, count: 9000)
    let url = root.appendingPathComponent("big.jsonl")
    try? ([
        #"{"type":"mode","mode":"default"}"#,
        #"{"type":"file-history-snapshot","files":{"a.swift":"\#(padding)"}}"#,
        #"{"type":"user","entrypoint":"cli","promptSource":"typed","message":{"role":"user","content":"the turn behind the snapshot"}}"#,
    ].joined(separator: "\n")).write(to: url, atomically: true, encoding: .utf8)

    let size = (try? FileManager.default.attributesOfItem(atPath: url.path))?[.size] as? Int ?? 0
    check("the fixture's opening record really is bigger than a 128KB window", size > 128 << 10)
    let found = StartPoints.front(ofFile: url)
    expect("and the turn behind it is still found", found.opening, "the turn behind the snapshot")
    expect("with what it says about itself", found.entrypoint, "cli")
    check("so it counts as a conversation", found.isConversation)

    // The other half of the same trap: that snapshot must not be mistaken for a turn.
    expect("a record that merely contains the words is not a turn",
           StartPoints.front(inRecord: ["type": "file-history-snapshot",
                                        "message": ["role": "user", "content": "hello"]])?.opening,
           nil)

    // And the answer to that turn, which is what names a conversation whose opening request was
    // one pasted screenshot and no words. Same row parser as the user side, so the same things
    // are excluded — and tool calls are not the assistant talking.
    let answered = [
        #"{"type":"user","message":{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"x"}},{"type":"text","text":"[Image #1]"}]}}"#,
        #"{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}"#,
        #"{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"我看到了：素材缺一條"}]}}"#,
        #"{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"再來查判準"}]}}"#,
    ].joined(separator: "\n")
    expect("a turn that opens by running a command is stepped over until it says something",
           Transcript.firstAssistantMessage(in: answered), "我看到了：素材缺一條")
    expect("the user side of the same transcript is unchanged",
           Transcript.firstUserMessage(in: answered), "[Image #1]")
    expect("a sidechain is not this conversation's answer",
           Transcript.firstAssistantMessage(in: #"{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"an agent"}]}}"#),
           nil)
    expect("and a conversation nobody has answered yet has none",
           Transcript.firstAssistantMessage(in: #"{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}"#),
           nil)
    let onDisk = root.appendingPathComponent("answered.jsonl")
    try? answered.write(to: onDisk, atomically: true, encoding: .utf8)
    expect("the bounded file reader agrees with the parser above",
           Transcript.firstAssistantMessage(of: onDisk), "我看到了：素材缺一條")
}

group("a rename is found wherever it was made") {
    let root = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-title-\(getpid())", isDirectory: true)
    try? FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }

    // Renamed early, then talked at for another megabyte. This is the case the whole-file scan
    // exists for, and the case a tail-only reader gets wrong — so it is the one that proves the
    // rewrite of that scan kept its answer.
    let filler = String(repeating: #"{"type":"assistant","message":{"role":"assistant","content":"..."}}"#
                        + "\n", count: 12_000)
    let url = root.appendingPathComponent("renamed.jsonl")
    try? ([#"{"type":"ai-title","aiTitle":"what Claude Code called it"}"#,
           #"{"customTitle":"what a person called it"}"#,
           filler].joined(separator: "\n")).write(to: url, atomically: true, encoding: .utf8)

    let size = (try? FileManager.default.attributesOfItem(atPath: url.path))?[.size] as? Int ?? 0
    check("the rename really is further back than the tail window", size > 512_000)
    expect("and it still wins over the title Claude Code wrote",
           Transcript.title(ofTranscript: url), "what a person called it")

    // A file with no rename in it at all is the ninety-nine per cent case, and the one the scan
    // spends the most on: proving a key is absent means reading every byte.
    // The title record last, which is where it is in a real transcript: Claude Code appends one
    // per turn, so the freshest is always near the end. Only a rename is ever looked for further
    // back than the tail — that asymmetry is what the case above proves, and this is its other
    // half.
    let plain = root.appendingPathComponent("plain.jsonl")
    try? ([filler, #"{"type":"ai-title","aiTitle":"never renamed"}"#]
            .joined(separator: "\n")).write(to: plain, atomically: true, encoding: .utf8)
    expect("with none, Claude Code's own title stands",
           Transcript.title(ofTranscript: plain), "never renamed")
}
}
