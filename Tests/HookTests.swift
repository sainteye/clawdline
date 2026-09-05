import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - Claude Code hooks

private func hookSession(_ id: String, tty: String) -> TargetSession {
    TargetSession(backend: .iterm, id: id, name: "x", tty: tty,
                  windowIndex: 0, tabIndex: 0, assistant: .claude)
}









// ---------------------------------------------------------------- real captured fixtures
// Everything below was copied out of a live run: cloudflared 2026.6.1 on macOS 15,
// `cloudflared tunnel --url http://127.0.0.1:7717`, stderr.

let banner = "2026-08-18T09:31:17Z INF |  https://denied-franchise-william-jade.trycloudflare.com                                   |"
let bannerTitle = "2026-08-18T09:31:17Z INF |  Your quick Tunnel has been created! Visit it at (it may take some time to be reachable):  |"
let terms = "2026-08-18T09:31:14Z INF Thank you for trying Cloudflare Tunnel. Doing so, without a Cloudflare account, is a quick way to experiment and try it out. However, be aware that these account-less Tunnels have no uptime guarantee, are subject to the Cloudflare Online Services Terms of Use (https://www.cloudflare.com/website-terms/), and Cloudflare reserves the right to investigate your use of Tunnels for violations of such terms. If you intend to use Tunnels in production you should use a pre-created named tunnel by following: https://developers.cloudflare.com/cloudflare-one/connections/connect-apps"
let requesting = "2026-08-18T09:31:14Z INF Requesting new quick Tunnel on trycloudflare.com..."
let registered = "2026-08-18T09:31:18Z INF Registered tunnel connection connIndex=0 connection=da4d66ef-9c21-4b23-82c8-bc14bb60cc8e event=0 ip=2606:4700:a8::5 location=tpe01 protocol=quic"
let registered1 = "2026-08-18T09:32:00Z INF Registered tunnel connection connIndex=1 connection=7807387d-9b2f-4400-856b-48a74c4fd921 event=0 ip=2606:4700:a8::3 location=tpe01 protocol=quic"
let curve = "2026-08-18T09:31:17Z INF Tunnel connection curve preferences: [X25519MLKEM768 CurveID(65074) CurveP256] connIndex=0 event=0 ip=2606:4700:a8::5"
let metrics = "2026-08-18T09:31:17Z INF Starting metrics server on 127.0.0.1:20241/metrics"
let settings = "2026-08-18T09:31:17Z INF Settings: map[cred-file:/Users/x/.cloudflared/471a0799.json ha-connections:1 protocol:quic url:http://127.0.0.1:7717]"
let precheck = "2026-08-18T09:31:17Z INF precheck component=\"DNS Resolution\" details=\"DNS Resolved successfully\" run_id=f12384d3 status=pass target=region1.v2.argotunnel.com"
let badName = "error parsing tunnel ID: clawdline-no-such-tunnel is neither the ID nor the name of any of your tunnels"
let startingNamed = "2026-08-18T09:31:59Z INF Starting tunnel tunnelID=471a0799-077f-42b2-9d7e-21da0f069d07"

// Not captured — cloudflared's own shutdown wording, which is the trap this guards against.
let unregistered = "2026-08-18T09:35:02Z INF Unregistered tunnel connection connIndex=0 event=0"

















private func hookTarget(_ id: String, title: String = "fix the webhook",
                        tty: String = "/dev/ttys004", cwd: String? = nil) -> TargetSession {
    TargetSession(backend: .iterm, id: id, name: title, tty: tty,
                  windowIndex: 0, tabIndex: 0, assistant: .claude, cwd: cwd)
}

func runHookTests() {
group("hooks: reading a note") {
    let good = Data(#"{"event":"Stop","tty":"ttys004","at":1787039500,"session":"3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34"}"#.utf8)
    let note = HookBridge.parse(good)
    check("a whole note reads", note != nil)
    expect("its event", note?.event, .stop)
    expect("an old note gets the safe legacy meaning", note?.kind, .stop)
    expect("its tty", note?.tty, "ttys004")
    expect("its session id", note?.session, "3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34")

    // The script leaves the session out when it could not find one, and that is a note worth
    // keeping: the tty is what a reading is keyed on, and the id is only ever a shortcut.
    let noSession = HookBridge.parse(Data(#"{"event":"Stop","tty":"ttys004","at":1}"#.utf8))
    check("a note with no session id is still a note", noSession != nil)
    expect("and says so", noSession?.session, nil)

    let asked = Data(#"{"event":"PreToolUse","kind":"ask_user_question","tty":"ttys004","at":2,"tool_input":{"questions":[{"header":"Deploy","question":"Ship this build?","multiSelect":false,"options":[{"label":"Yes","description":"Deploy now"},{"label":"Not yet","description":"Keep testing"}]}]}}"#.utf8)
    let question = HookBridge.parse(asked)
    expect("a matched tool note carries its meaning", question?.kind, .askUserQuestion)
    expect("and the complete question", question?.questions.first?.text, "Ship this build?")
    expect("with unclipped option labels", question?.questions.first?.options.map(\.label),
           ["Yes", "Not yet"])
    expect("which become the existing numbered menu", question?.menu?.options.map(\.number),
           [1, 2])

    // An event this version does not know about is not an error to report; it is a note from a
    // newer script, and the right answer is to ignore it rather than to draw something wrong.
    check("an unknown event is dropped",
          HookBridge.parse(Data(#"{"event":"PreCompact","tty":"ttys004","at":1}"#.utf8)) == nil)
    check("half a file is dropped", HookBridge.parse(Data(#"{"event":"Stop","tt"#.utf8)) == nil)
    check("no tty is dropped",
          HookBridge.parse(Data(#"{"event":"Stop","tty":"","at":1}"#.utf8)) == nil)
}

group("hooks: what a note is allowed to change") {
    let now = Date()
    let one = hookSession("A", tty: "/dev/ttys004")
    let two = hookSession("B", tty: "/dev/ttys009")
    let sessions = [one, two]
    func notes(_ event: HookBridge.Event, ago: TimeInterval = 1) -> [String: HookBridge.Note] {
        ["ttys004": HookBridge.Note(event: event, tty: "ttys004",
                                    at: now.addingTimeInterval(-ago), session: nil)]
    }
    func note(_ kind: HookBridge.Kind, event: HookBridge.Event) -> [String: HookBridge.Note] {
        ["ttys004": HookBridge.Note(event: event, kind: kind, tty: "ttys004",
                                    at: now.addingTimeInterval(-1), session: nil)]
    }

    // Nothing installed is the state every reading has to be right in, so it is the first check.
    expect("with no notes, the screen is the whole answer",
           HookBridge.merge([:], into: ["A": .waiting], sessions: sessions, now: now)["A"],
           .waiting)
    expect("a session nobody left a note for is untouched",
           HookBridge.merge(notes(.stop), into: ["B": .working("x")], sessions: sessions,
                            now: now)["B"],
           .working("x"))

    // The screen remains the authority for look-only, turn-boundary and opening input notes.
    expect("a question on screen outranks a Stop",
           HookBridge.merge(notes(.stop), into: ["A": .waiting], sessions: sessions, now: now)["A"],
           .waiting)
    expect("and a prompt going in leaves it alone",
           HookBridge.merge(notes(.userPromptSubmit), into: ["A": .waiting],
                            sessions: sessions, now: now)["A"],
           .waiting)

    // Opening input events only gate the screen parser. In particular, auto mode emits both
    // permission kinds for approvals it handles itself without ever showing a dialog. The
    // closing event can still beat a picker that remains in one stale capture.
    expect("AskUserQuestion does not replace an idle screen",
           HookBridge.merge(note(.askUserQuestion, event: .preToolUse), into: ["A": .idle],
                            sessions: sessions, now: now)["A"],
           .idle)
    expect("nor a screen that could not be read",
           HookBridge.merge(note(.askUserQuestion, event: .preToolUse), into: ["A": .unknown],
                            sessions: sessions, now: now)["A"],
           .unknown)
    expect("PostToolUse retracts a picker left in the capture",
           HookBridge.merge(note(.askUserQuestionDone, event: .postToolUse),
                            into: ["A": .waiting], sessions: sessions, now: now)["A"],
           .idle)
    expect("an auto-approved PermissionRequest keeps the working screen",
           HookBridge.merge(note(.permissionRequest, event: .permissionRequest),
                            into: ["A": .working("Reviewing approval request")],
                            sessions: sessions, now: now)["A"],
           .working("Reviewing approval request"))
    expect("an auto-approved permission notification keeps the idle screen",
           HookBridge.merge(note(.permissionPrompt, event: .notification), into: ["A": .idle],
                            sessions: sessions, now: now)["A"],
           .idle)
    expect("but idle_prompt still only asks for a screen reading",
           HookBridge.merge(note(.idlePrompt, event: .notification), into: ["A": .idle],
                            sessions: sessions, now: now)["A"],
           .idle)

    // **No note asserts that a session is working**, and this is the narrowing that measuring
    // produced: Claude Code draws its live line about two seconds after Return and draws nothing
    // at all while plain text comes back, so a claim short enough to be safe covers almost none
    // of a turn — and a long one cannot be retracted when somebody presses Esc, because
    // cancelling fires no hook. `SessionWatch.nudge` looks twice instead.
    expect("a submitted prompt claims nothing about an idle screen",
           HookBridge.merge(notes(.userPromptSubmit), into: ["A": .idle],
                            sessions: sessions, now: now)["A"],
           .idle)
    expect("nor about one that could not be read at all",
           HookBridge.merge(notes(.userPromptSubmit), into: ["A": .unknown],
                            sessions: sessions, now: now)["A"],
           .unknown)
    expect("and it never touches the live line",
           HookBridge.merge(notes(.userPromptSubmit), into: ["A": .working("Generating… (4s)")],
                            sessions: sessions, now: now)["A"],
           .working("Generating… (4s)"))

    // The stale spinner: Claude Code does not always erase its live line when a fast turn ends.
    expect("a fresh Stop beats a spinner left on the screen",
           HookBridge.merge(notes(.stop), into: ["A": .working("Generating… (4s)")],
                            sessions: sessions, now: now)["A"],
           .idle)
    // …and only for as long as a leftover line could plausibly still be one. Past that, anything
    // running started with a prompt of its own, and calling it idle is the worst answer here.
    expect("an old one does not",
           HookBridge.merge(notes(.stop, ago: HookBridge.stopWindow + 5),
                            into: ["A": .working("Generating… (4s)")],
                            sessions: sessions, now: now)["A"],
           .working("Generating… (4s)"))

    // These two only ask us to look. The reading that followed is the answer.
    expect("a notification changes no state by itself",
           HookBridge.merge(notes(.notification), into: ["A": .idle], sessions: sessions,
                            now: now)["A"],
           .idle)
    expect("nor does a session starting",
           HookBridge.merge(notes(.sessionStart), into: ["A": .unknown], sessions: sessions,
                            now: now)["A"],
           .unknown)
}

group("hooks: editing somebody else's settings file") {
    // What has to survive: a hook that belongs to a plugin, and every key that is not `hooks`.
    let theirs: [String: Any] = [
        "model": "opus",
        "hooks": [
            "PreToolUse": [["matcher": "Write",
                            "hooks": [["type": "command", "command": "/opt/theirs/check.sh"]]]],
            "Stop": [["hooks": [["type": "command", "command": "/opt/theirs/done.sh"]]]],
        ],
    ]
    let after = HookBridge.adding("/Users/x/.config/clawdline/hook.sh", to: theirs)
    let hooks = after["hooks"] as? [String: Any] ?? [:]
    expect("everything outside hooks is left alone", after["model"] as? String, "opus")
    expect("an event we share keeps its entry and gains the matched one",
           (hooks["PreToolUse"] as? [[String: Any]])?.count, 2)
    expect("an event we share keeps theirs and gains ours",
           (hooks["Stop"] as? [[String: Any]])?.count, 2)
    for registration in HookBridge.Registration.all {
        check("\(registration.event.rawValue)/\(registration.matcher ?? "all") is wired up",
              (hooks[registration.event.rawValue] as? [[String: Any]] ?? []).contains { group in
                  (group["matcher"] as? String) == registration.matcher &&
                  (group["hooks"] as? [[String: Any]] ?? []).contains(where: HookBridge.isOurs)
              })
    }
    let notifications = hooks["Notification"] as? [[String: Any]] ?? []
    // One event, three meanings, each filtered on its own. `agent_needs_input` is Claude Code's
    // own word for somebody having to answer something now — registered to find out whether it
    // arrives when a picker opens, which is the signal `PreToolUse` was expected to give and
    // measurably does not.
    expect("Notification is split per meaning, not registered once",
           Set(notifications.compactMap { $0["matcher"] as? String }),
           Set(["permission_prompt", "idle_prompt", "agent_needs_input"]))

    // Pressing Install twice is a thing people do.
    let twice = HookBridge.adding("/Users/x/.config/clawdline/hook.sh", to: after)
    expect("installing twice leaves one of ours",
           ((twice["hooks"] as? [String: Any])?["Stop"] as? [[String: Any]])?.count, 2)

    // A matcher group can contain several owners. Taking our handler out must leave both the
    // group metadata and the neighbouring command exactly where the user put them.
    var mixed = after
    var mixedHooks = mixed["hooks"] as? [String: Any] ?? [:]
    var stopGroups = mixedHooks["Stop"] as? [[String: Any]] ?? []
    var ourStop = stopGroups.removeLast()
    var handlers = ourStop["hooks"] as? [[String: Any]] ?? []
    handlers.append(["type": "command", "command": "/opt/theirs/same-group.sh"])
    ourStop["hooks"] = handlers
    ourStop["once"] = true
    stopGroups.append(ourStop)
    mixedHooks["Stop"] = stopGroups
    mixed["hooks"] = mixedHooks
    let unmixed = HookBridge.removing(from: mixed)
    let remainingStop = ((unmixed["hooks"] as? [String: Any])?["Stop"] as? [[String: Any]]) ?? []
    check("removing ours preserves another handler in the same group",
          remainingStop.contains { group in
              group["once"] as? Bool == true &&
              (group["hooks"] as? [[String: Any]] ?? []).contains {
                  $0["command"] as? String == "/opt/theirs/same-group.sh"
              }
          })

    let back = HookBridge.removing(from: twice)
    let left = back["hooks"] as? [String: Any] ?? [:]
    expect("removing puts the file back", NSDictionary(dictionary: left),
           NSDictionary(dictionary: theirs["hooks"] as? [String: Any] ?? [:]))
    expect("and leaves the rest of it alone", back["model"] as? String, "opus")

    // A file that had no hooks of its own should read afterwards as though nothing happened.
    // The real path, because that is what marks an entry as ours — see `isOurs`.
    let bare = HookBridge.removing(
        from: HookBridge.adding("/Users/x/.config/clawdline/hook.sh", to: ["model": "opus"]))
    check("an event left empty goes away rather than staying as []", bare["hooks"] == nil)

    // A path with a space in it is a home directory somebody actually has.
    let spaced = HookBridge.adding("/Users/a b/.config/clawdline/hook.sh", to: [:])
    let command = (((spaced["hooks"] as? [String: Any])?["Stop"] as? [[String: Any]])?
        .first?["hooks"] as? [[String: Any]])?.first?["command"] as? String
    expect("the path is quoted", command,
           "'/Users/a b/.config/clawdline/hook.sh' Stop stop")
}

group("hooks: the script itself") {
    // The one piece of this that is not Swift, and the piece with the most ways to be quietly
    // wrong: it writes nothing on stdout, it always exits 0, and it names its note after the
    // terminal the session is on — which is the only string this app and Claude Code both know.
    let script = "Resources/clawdline-hook.sh"
    guard FileManager.default.fileExists(atPath: script) else {
        check("Resources/clawdline-hook.sh is there", false)
        return
    }
    let dir = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-hook-test-\(getpid())")
    try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: dir) }

    func run(_ event: String, kind: String? = nil, payload: String,
             into: URL? = dir) -> (out: String, code: Int32) {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/bin/sh")
        p.arguments = [script, event] + (kind.map { [$0] } ?? [])
        var env = ProcessInfo.processInfo.environment
        if let into { env["CLAWDLINE_HOOK_DIR"] = into.path } else { env["CLAWDLINE_HOOK_DIR"] = "/nowhere/at/all" }
        p.environment = env
        let stdin = Pipe(), stdout = Pipe()
        p.standardInput = stdin
        p.standardOutput = stdout
        p.standardError = Pipe()
        try? p.run()
        stdin.fileHandleForWriting.write(Data(payload.utf8))
        try? stdin.fileHandleForWriting.close()
        let out = String(data: stdout.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
        p.waitUntilExit()
        return (out, p.terminationStatus)
    }

    // A prompt with a quote and a backslash in it, because that is what breaks a reader that
    // tries to parse JSON with string operations. The session id is a uuid and cannot.
    let payload = #"{"session_id":"3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34","cwd":"/tmp","hook_event_name":"Stop","prompt":"a \" quote and a \\ backslash"}"#
    let first = run("Stop", payload: payload)
    expect("it exits 0", first.code, 0)
    expect("and says nothing on stdout — that is read back as instructions", first.out, "")

    // **Every assertion below runs whether or not a note was written**, and that is not
    // fussiness. The script writes nothing when it cannot find a tty above itself, which is the
    // normal case on a build machine and never the case here — so a group that only asserted
    // "when there is a note" counted six more checks locally than in CI, and the count the
    // README carries is checked against the run. A test whose *number* of assertions depends on
    // the machine is a test that turns the guard around it into a coin toss.
    func notes(in dir: URL) -> [String] {
        ((try? FileManager.default.contentsOfDirectory(atPath: dir.path)) ?? [])
            .filter { $0.hasSuffix(".json") }
    }
    let written = notes(in: dir)
    let note = written.first.flatMap {
        HookBridge.parse((try? Data(contentsOf: dir.appendingPathComponent($0))) ?? Data())
    }
    let noTTY = written.isEmpty      // no controlling terminal above this process

    check("it wrote a note, or found no terminal to name one after", noTTY || note != nil)
    check("named after the tty it found",
          noTTY || written.first == "\(note?.tty ?? "?").json")
    check("carrying the event it was told", noTTY || note?.event == .stop)
    check("and the session id, cut out of a payload with quotes in it",
          noTTY || note?.session == "3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34")

    // Overwritten, never appended: a session has one note and it is the newest one.
    _ = run("Notification", payload: payload)
    let after = notes(in: dir)
    check("a second note replaces the first", noTTY || after.count == 1)
    check("with the newer event", noTTY || written.first.flatMap {
        HookBridge.parse((try? Data(contentsOf: dir.appendingPathComponent($0))) ?? Data())?.event
    } == .notification)

    // Every way this can be asked to do nothing, it has to do nothing quietly. A hook that
    // exits non-zero is making a decision about somebody's turn.
    expect("no event argument, no complaint", run("", payload: payload).code, 0)
    expect("empty stdin, no complaint", run("Stop", payload: "").code, 0)
    expect("no directory to write into, no complaint",
           run("Stop", payload: payload, into: nil).code, 0)

    // Pin the tty through the script's own per-session cache so this half is deterministic even
    // on a CI process with no controlling terminal.
    let structured = dir.appendingPathComponent("structured", isDirectory: true)
    try? FileManager.default.createDirectory(at: structured, withIntermediateDirectories: true)
    let session = "4f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a35"
    try? Data("ttys777".utf8).write(
        to: structured.appendingPathComponent(".tty-\(session)"), options: .atomic)
    let askPayload = #"{"session_id":"4f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a35","hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{"questions":[{"header":"Choice","question":"Which path?","options":[{"label":"Keep the full label — \"quoted\"","description":"first"},{"label":"另一條路","description":"second"}],"multiSelect":false}]}}"#
    let askRun = run("PreToolUse", kind: "ask_user_question", payload: askPayload,
                     into: structured)
    let askFile = structured.appendingPathComponent("ttys777.json")
    let askData = (try? Data(contentsOf: askFile)) ?? Data()
    let askNote = HookBridge.parse(askData)
    expect("a structured question still writes nothing to stdout", askRun.out, "")
    expect("and exits 0", askRun.code, 0)
    expect("tool_input survives as a complete question in the note",
           askNote?.questions.first?.text, "Which path?")
    expect("including labels JSON has to escape",
           askNote?.questions.first?.options.map(\.label),
           ["Keep the full label — \"quoted\"", "另一條路"])
    check("the note has a hard size ceiling", askData.count < 34 * 1024)

    // idle_prompt is a nudge, not a state transition. It must not replace a still-open question,
    // or the one-minute notification would make precisely the question disappear again.
    _ = run("Notification", kind: "idle_prompt", payload: askPayload, into: structured)
    expect("idle_prompt does not erase an authoritative question",
           HookBridge.parse((try? Data(contentsOf: askFile)) ?? Data())?.kind,
           .askUserQuestion)

    let huge = String(repeating: "x", count: 40_000)
    let hugePayload = "{\"session_id\":\"\(session)\",\"tool_input\":{\"questions\":[{\"question\":\"Q\",\"options\":[{\"label\":\"A\",\"description\":\"\(huge)\"},{\"label\":\"B\"}]}]}}"
    _ = run("PreToolUse", kind: "ask_user_question", payload: hugePayload, into: structured)
    let capped = (try? Data(contentsOf: askFile)) ?? Data()
    check("oversized tool_input is omitted rather than growing the note", capped.count < 34 * 1024)
    check("an omitted oversized input does not become partial JSON",
          HookBridge.parse(capped)?.questions.isEmpty == true)
}

group("quickURL") {
    expect("the banner line", RemoteTunnel.quickURL(in: banner),
           "https://denied-franchise-william-jade.trycloudflare.com")
    check("the terms-of-use line has URLs but not ours", RemoteTunnel.quickURL(in: terms) == nil,
          "got \(String(describing: RemoteTunnel.quickURL(in: terms)))")
    check("the banner title", RemoteTunnel.quickURL(in: bannerTitle) == nil)
    check("the requesting line", RemoteTunnel.quickURL(in: requesting) == nil)
    check("a registration line", RemoteTunnel.quickURL(in: registered) == nil)
    check("empty", RemoteTunnel.quickURL(in: "") == nil)
    expect("a trailing slash is dropped",
           RemoteTunnel.quickURL(in: "INF |  https://a-b-c.trycloudflare.com/  |"),
           "https://a-b-c.trycloudflare.com")
    check("a bare apex is not an address",
          RemoteTunnel.quickURL(in: "https://.trycloudflare.com") == nil)
    expect("ours wins even when another URL comes first",
           RemoteTunnel.quickURL(in: "see https://www.cloudflare.com/x/ then https://q-w-e.trycloudflare.com |"),
           "https://q-w-e.trycloudflare.com")
}

group("registeredConnection") {
    expect("the registration line", RemoteTunnel.registeredConnection(in: registered), "tpe01")
    expect("the second connection", RemoteTunnel.registeredConnection(in: registered1), "tpe01")
    check("unregistered is not registered",
          RemoteTunnel.registeredConnection(in: unregistered) == nil,
          "got \(String(describing: RemoteTunnel.registeredConnection(in: unregistered)))")
    check("the curve line mentions a connection and is not one",
          RemoteTunnel.registeredConnection(in: curve) == nil)
    check("the banner", RemoteTunnel.registeredConnection(in: banner) == nil)
    check("the metrics line", RemoteTunnel.registeredConnection(in: metrics) == nil)
    check("the settings line", RemoteTunnel.registeredConnection(in: settings) == nil)
    check("a precheck row", RemoteTunnel.registeredConnection(in: precheck) == nil)
    expect("no location field still counts as up",
           RemoteTunnel.registeredConnection(in: "2026-01-01T00:00:00Z INF Registered tunnel connection connIndex=0"),
           "?")
    expect("the older wording",
           RemoteTunnel.registeredConnection(in: "2021-01-01T00:00:00Z INF Connection 9c1 registered connIndex=1 location=SIN"),
           "SIN")
}

group("complaint") {
    expect("a bare stderr line is the whole message", RemoteTunnel.complaint(in: badName), badName)
    check("an INF line is not a complaint", RemoteTunnel.complaint(in: registered) == nil)
    check("the banner is not a complaint", RemoteTunnel.complaint(in: banner) == nil)
    check("Starting tunnel is not a complaint", RemoteTunnel.complaint(in: startingNamed) == nil)
    expect("an ERR line loses its timestamp",
           RemoteTunnel.complaint(in: "2026-08-18T09:31:14Z ERR Couldn't start tunnel error=\"Unauthorized\""),
           "Couldn't start tunnel error=\"Unauthorized\"")
    expect("an FTL line too",
           RemoteTunnel.complaint(in: "2026-08-18T09:31:14Z FTL no credentials file"),
           "no credentials file")
    check("blank", RemoteTunnel.complaint(in: "   ") == nil)
    check("a WRN line is not worth reporting", RemoteTunnel.complaint(in: "2026-08-18T09:31:17Z WRN slow") == nil)
    check("long lines are cut", (RemoteTunnel.complaint(in: String(repeating: "x", count: 500)) ?? "").count == 200)
}

group("redacted") {
    expect("the random half goes",
           RemoteTunnel.redacted("https://denied-franchise-william-jade.trycloudflare.com"),
           "https://….trycloudflare.com")
    expect("a named hostname keeps its zone",
           RemoteTunnel.redacted("https://clawd.example.com"), "https://….example.com")
    expect("nothing to redact", RemoteTunnel.redacted("nonsense"), "nonsense")
}

group("backoff") {
    expect("first try", RemoteTunnel.backoff(1), 1)
    expect("second", RemoteTunnel.backoff(2), 2)
    expect("third", RemoteTunnel.backoff(3), 4)
    expect("sixth", RemoteTunnel.backoff(6), 32)
    expect("capped", RemoteTunnel.backoff(20), 60)
    expect("nonsense is still a wait", RemoteTunnel.backoff(0), 1)
}

group("arguments") {
    // `--config` is the one that is not decoration: without it cloudflared reads
    // ~/.cloudflared/config.yml, whose `tunnel:` key overrides the name on the command line and
    // whose ingress list 404s any hostname it has never heard of. Both were observed live.
    let ours = RemoteTunnel.configURL.path
    expect("quick",
           RemoteTunnel.arguments(for: RemoteTunnel.Plan(mode: .quick, port: 7717)),
           ["tunnel", "--config", ours, "--no-autoupdate", "--metrics", "127.0.0.1:0",
            "--grace-period", "2s", "--url", "http://127.0.0.1:7717"])
    expect("named puts its flags before run",
           RemoteTunnel.arguments(for: RemoteTunnel.Plan(mode: .named, name: "clawd")),
           ["tunnel", "--config", ours, "--no-autoupdate", "--metrics", "127.0.0.1:0",
            "--grace-period", "2s", "run", "clawd"])
    expect("a non-default port rides along",
           RemoteTunnel.arguments(for: RemoteTunnel.Plan(mode: .quick, port: 9000)).last,
           "http://127.0.0.1:9000")
}

group("TunnelMode") {
    expect("off", TunnelMode(configured: "off"), .off)
    expect("quick", TunnelMode(configured: "quick"), .quick)
    expect("named", TunnelMode(configured: " Named "), .named)
    expect("a typo is off", TunnelMode(configured: "quik"), .off)
    expect("empty is off", TunnelMode(configured: ""), .off)
}

group("the tunnel refuses, and every reason is a pure function of its inputs") {
    func why(_ mode: TunnelMode, auth: Bool = true, server: Bool = true,
             name: String = "clawd", host: String = "clawd.example.com") -> String? {
        RemoteTunnel.refusal(mode: mode, authConfigured: auth, serverOn: server,
                             name: name, hostname: host)
    }
    // The interlock this file exists for: reachable from the internet must be a decision somebody
    // made, not something one config key did.
    check("no paired device refuses", why(.quick, auth: false)?.contains("paired device") == true)
    check("and says so for a named tunnel too", why(.named, auth: false) != nil)
    check("no local server refuses", why(.quick, server: false)?.contains("local server") == true)
    check("a named tunnel with no name refuses",
          why(.named, name: "")?.contains("remote_tunnel_name") == true)
    check("a named tunnel with no hostname refuses",
          why(.named, host: "")?.contains("remote_hostname") == true)
    check("a quick tunnel needs neither", why(.quick, name: "", host: "") == nil)
    check("off never refuses, it is simply off", why(.off, auth: false, server: false) == nil)
    check("everything in place is allowed", why(.quick) == nil && why(.named) == nil)
}

group("state hook: a reading is not an event") {
    let one = hookTarget("A")
    let two = hookTarget("B", tty: "/dev/ttys009")
    let sessions = [one, two]
    func changes(_ old: [String: SessionState],
                 _ new: [String: SessionState]) -> [StateHook.Change] {
        StateHook.transitions(from: old, to: new, sessions: sessions)
    }

    // The moment the whole file exists for: something that was running has stopped to ask.
    let real = changes(["A": .working("Cogitating… (7s)")], ["A": .waiting])
    expect("working → waiting is one change", real.count, 1)
    expect("and it says exactly what happened", real.first,
           StateHook.Change(session: one, from: .working("Cogitating… (7s)"), to: .waiting))

    expect("the same state twice is nothing", changes(["A": .idle], ["A": .idle]).count, 0)

    // The live line carries its own clock, so two readings of a busy session are never equal and
    // are never a change either. Anything keyed on `==` here would fire once a second, forever.
    expect("a ticking live line is not a change",
           changes(["A": .working("Cogitating… (7s)")],
                   ["A": .working("Cogitating… (8s)")]).count, 0)
    expect("nor is losing the line while staying busy",
           changes(["A": .working("Cogitating… (7s)")], ["A": .working("")]).count, 0)

    // `unknown` is the absence of an answer, not a state — iTerm2 being slow to reply must not
    // arrive as a session stopping and starting again.
    expect("going unknown is not a change",
           changes(["A": .working("x")], ["A": .unknown]).count, 0)
    expect("coming back from unknown is not one either",
           changes(["A": .unknown], ["A": .idle]).count, 0)
    expect("and unknown to unknown is certainly not",
           changes(["A": .unknown], ["A": .unknown]).count, 0)

    // Ten sessions on the first reading after launch are not ten state changes.
    expect("a session seen for the first time is not a change",
           changes([:], ["A": .waiting, "B": .idle]).count, 0)
    expect("a session that has gone away is not one",
           changes(["A": .working("x")], [:]).count, 0)

    let both = changes(["A": .working("x"), "B": .idle], ["A": .idle, "B": .waiting])
    expect("two sessions changing is two changes", both.count, 2)
    expect("in the order the sessions are listed in", both.map { $0.session.id }, ["A", "B"])

    // The session list is the authority on what exists; a reading keyed to something not in it
    // belongs to a tab that closed between the two.
    expect("a reading for a session nobody is watching is ignored",
           StateHook.transitions(from: ["Z": .idle], to: ["Z": .waiting],
                                 sessions: sessions).count, 0)
}

group("state hook: the words a hook reads") {
    // Spelled out rather than derived, because these are somebody else's API: renaming a case in
    // Swift must not quietly rename a string a shell script compares against.
    expect("working", StateHook.name(.working("x")), "working")
    expect("waiting", StateHook.name(.waiting), "waiting")
    expect("idle", StateHook.name(.idle), "idle")
    expect("unknown", StateHook.name(.unknown), "unknown")
}

group("state hook: what a hook is told") {
    // A hook is told the display label, and a display label comes from the conversation's own
    // record rather than from the tab it is sitting in — so the record is what is stubbed here,
    // and the tab is given the worst title it could have.
    defer { SessionNaming.lookForTesting = noSessionNames; SessionNaming.forgetForTesting() }
    SessionNaming.forgetForTesting()
    SessionNaming.lookForTesting = { _ in
        SessionNaming.Name(title: "fix the webhook", handle: nil)
    }
    let session = hookTarget("A9F3", title: "Default (python)",
                             cwd: "/Users/x/code/clawdline")
    let env = StateHook.environment(
        for: StateHook.Change(session: session, from: .working("Cogitating… (7s)"), to: .waiting),
        claudeSession: "3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34")

    expect("the event has a name of its own", env["CLAWDLINE_EVENT"], "state_changed")
    expect("the state", env["CLAWDLINE_STATE"], "waiting")
    expect("the one it came from", env["CLAWDLINE_PREV_STATE"], "working")
    expect("the session id", env["CLAWDLINE_SESSION_ID"], "A9F3")
    expect("the tty", env["CLAWDLINE_TTY"], "/dev/ttys004")
    // The display label, not the tab title: a title is a place a name is shown, and a tab whose
    // title has been emptied still reads `Default` on every row that trusted it.
    expect("the label as a person reads it", env["CLAWDLINE_LABEL"], "fix the webhook")
    check("and nothing the terminal is called reaches the hook",
          env["CLAWDLINE_LABEL"] != session.label && env["CLAWDLINE_LABEL"] != session.name)
    expect("where it is working", env["CLAWDLINE_CWD"], "/Users/x/code/clawdline")
    expect("and Claude Code's own id, which names the transcript",
           env["CLAWDLINE_CLAUDE_SESSION"], "3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34")
    check("a session that is not working carries no live line", env["CLAWDLINE_LINE"] == nil)

    let obj = (try? JSONSerialization.jsonObject(
        with: Data((env["CLAWDLINE_EVENT_JSON"] ?? "").utf8))) as? [String: Any]
    check("the whole thing arrives as one object too", obj != nil)
    expect("with the prefix off the keys", obj?["state"] as? String, "waiting")
    expect("and the underscores kept", obj?["session_id"] as? String, "A9F3")
    expect("the same fields as the variables, no more", obj?.count, env.count - 1)
    check("and it does not contain itself", obj?["event_json"] == nil)

    let busy = StateHook.environment(
        for: StateHook.Change(session: session, from: .idle, to: .working("Cogitating… (7s)")))
    expect("a working session carries the live line", busy["CLAWDLINE_LINE"], "Cogitating… (7s)")
    expect("and where it came from", busy["CLAWDLINE_PREV_STATE"], "idle")
    check("with nothing invented for a session no hook told us about",
          busy["CLAWDLINE_CLAUDE_SESSION"] == nil)

    // Claude Code draws no live line at all while plain text is coming back, and an empty string
    // would read as one that had been erased rather than one that was never there.
    let quiet = StateHook.environment(
        for: StateHook.Change(session: session, from: .idle, to: .working("   ")))
    check("a blank line is omitted rather than sent empty", quiet["CLAWDLINE_LINE"] == nil)

    let bare = StateHook.environment(
        for: StateHook.Change(session: hookTarget("B"), from: .waiting, to: .idle))
    check("a session whose directory is not known omits it", bare["CLAWDLINE_CWD"] == nil)
    expect("and everything else is still there", bare["CLAWDLINE_STATE"], "idle")
    expect("including the one it left", bare["CLAWDLINE_PREV_STATE"], "waiting")
}

group("push notifications identify the session and its project") {
    defer { SessionNaming.lookForTesting = noSessionNames; SessionNaming.forgetForTesting() }
    SessionNaming.forgetForTesting()
    SessionNaming.lookForTesting = { _ in
        SessionNaming.Name(title: "fix the webhook", handle: nil)
    }
    // A notification reaching a phone saying `Default` is the same defect one surface further
    // out, so the tab here is titled the way the eleven on 2026-08-28 were.
    let session = hookTarget("A9F3", title: "Default (python)")
    let waiting = StateHook.pushMessage(
        for: session, project: "clawdline", event: "is waiting for an answer")

    expect("the session's own name is the title", waiting.title, "fix the webhook")
    expect("the project and event are the body", waiting.body,
           "clawdline is waiting for an answer")

    let deploy = StateHook.pushMessage(
        for: session, project: "clawdline", event: "deploy failed")
    expect("deploy notifications use the same informative shape", deploy,
           StateHook.PushMessage(title: "fix the webhook", body: "clawdline deploy failed"))
}

group("a notification goes to whoever is actually blocked") {
    func role(depth: Int, live: Bool = true, deadline: Date? = nil) -> Orchestrator.Role {
        Orchestrator.Role(taskID: "t", depth: depth, title: "a task",
                          deadline: deadline, live: live)
    }

    // A person's own session behaves exactly as it always has. This is the case that must not
    // move: it is the one notification in the app that earns an interruption.
    // Routed against `L.t` rather than against English, so the test says what it means — which
    // string this event picks — on a machine in any language.
    expect("a root that is waiting still says so",
           StateHook.pushDecision(role: nil, minutesLeft: nil),
           .send(L.t.pushWaiting))

    // There used to be a second event on this path, and the acceptance for removing it is a
    // negative: no reachable code turns a stopped turn into a notification. A pure function
    // cannot assert its own absence, so the file is read — and this goes red the moment anybody
    // brings the clock back.
    let hook = try! String(contentsOfFile: "Sources/StateHook.swift", encoding: .utf8)
    check("nothing here times a turn any more",
          !hook.contains("FinishTracker") && !hook.contains("finishThreshold"))
    check("and no push is gated on a turn merely stopping",
          !hook.contains("pushOnFinish") && !hook.contains("sendFinishedPush"))

    // The one below a root that is *more* urgent than a root, because nobody is on that tab.
    expect("a child that is waiting says which kind of session it is",
           StateHook.pushDecision(role: role(depth: 1), minutesLeft: nil),
           .send(L.t.pushChildWaiting(minutes: nil)))
    expect("and carries the clock when there is one",
           StateHook.pushDecision(role: role(depth: 1), minutesLeft: 12),
           .send(L.t.pushChildWaiting(minutes: 12)))
    check("which is a different sentence from a root's, not a politer one",
          L.t.pushChildWaiting(minutes: nil) != L.t.pushWaiting)

    let en = English()
    expect("in English, the clock is on the end", en.pushChildWaiting(minutes: 12),
           "has a child session waiting — 12 min left")
    expect("and absent rather than blank when there is none", en.pushChildWaiting(minutes: nil),
           "has a child session waiting")
    expect("a tab whose task is over has nobody behind it",
           StateHook.pushDecision(role: role(depth: 1, live: false), minutesLeft: 5),
           .silent)

    let now = Date(timeIntervalSince1970: 1_000_000)
    expect("minutes left are whole minutes",
           StateHook.minutesLeft(for: role(depth: 1, deadline: now.addingTimeInterval(750)),
                                 now: now), 12)
    check("a task with no clock yet has no number",
          StateHook.minutesLeft(for: role(depth: 1), now: now) == nil)
    check("and neither does one that has already run out",
          StateHook.minutesLeft(for: role(depth: 1, deadline: now.addingTimeInterval(-60)),
                                now: now) == nil)
    check("nor one with less than a minute to go — 0 reads as a number somebody forgot",
          StateHook.minutesLeft(for: role(depth: 1, deadline: now.addingTimeInterval(20)),
                                now: now) == nil)
}

group("a fan-out is one sentence, whatever it cost") {
    expect("the root's own name is the title, so it is clear whose work came back",
           Orchestrator.batchMessage(project: "clawdline", label: "ship the parser",
                                     done: 5, failed: 0).title,
           "ship the parser")
    expect("the project and the count are the body",
           Orchestrator.batchMessage(project: "clawdline", label: nil, done: 5, failed: 0).body,
           "clawdline " + L.t.pushBatchDone(done: 5, failed: 0))

    let copy = English()
    expect("in English, a clean batch is a count", copy.pushBatchDone(done: 5, failed: 0),
           "finished 5 tasks")
    expect("a batch that lost some says how many", copy.pushBatchDone(done: 5, failed: 2),
           "finished 5 tasks, 2 failed")
    expect("and one task is one task", copy.pushBatchDone(done: 1, failed: 0), "finished 1 task")
    expect("and with no root label the project stands in for it",
           Orchestrator.batchMessage(project: "clawdline", label: nil, done: 1, failed: 0).title,
           "clawdline")

    // The project of a finished fan-out has to come off a path: every tab it ran in is closed
    // by the time there is anything to say.
    expect("a directory names its own project", StateHook.projectName(forDirectory: "/x/y/parser"),
           "parser")
    expect("and a path with nothing on the end falls back",
           StateHook.projectName(forDirectory: "/", fallback: "Clawdline"), "Clawdline")

    // Which preference the announcement reads, checked in the source because the gate sits inside
    // a private function two layers under the beat. A test that cannot reach the branch can still
    // require the branch to name the right key, and this one goes red if it names the old one.
    let orchestrator = try! String(contentsOfFile: "Sources/Orchestrator.swift", encoding: .utf8)
    let announcement = orchestrator
        .components(separatedBy: "private static func announce(_ batch: Batch, rootKey key: String)")
        .last?.components(separatedBy: "let project = batch.projectDir").first ?? ""
    check("the fan-out push is gated on push_on_fanout",
          announcement.contains("Config.shared.pushOnFanout"))
    check("and on nothing that names a turn stopping",
          !announcement.contains("pushOnFinish"))

    // The rename has to carry the answer somebody already gave. `push_on_finish` covered both the
    // turn-stopped push and this one, so an off there was an off about fan-outs too.
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-fanout-config-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let file = directory.appendingPathComponent("config.json")

    let fresh = Config(directoryForTesting: directory)
    expect("both push preferences default on", fresh.pushOnDelivery && fresh.pushOnFanout, true)

    try! Data("{\"push_on_finish\":false}".utf8).write(to: file)
    let inherited = Config(directoryForTesting: directory)
    expect("a config carrying only the old key keeps its answer about fan-outs",
           inherited.pushOnFanout, false)
    expect("and says nothing about the delivery push, which is a new question",
           inherited.pushOnDelivery, true)

    try! Data("{\"push_on_finish\":false,\"push_on_fanout\":true}".utf8).write(to: file)
    expect("an explicit new answer outranks the old key",
           Config(directoryForTesting: directory).pushOnFanout, true)

    // What the app writes, with nothing on disk to merge with — which is the only way to ask
    // "does it write this key" rather than "is this key in the file". `Config.save` deliberately
    // passes through keys it does not know, so an old `push_on_finish` in somebody's file is left
    // where it is; the migration reads it once and it is never a second source of truth.
    try! FileManager.default.removeItem(at: file)
    let rewriting = Config(directoryForTesting: directory)
    rewriting.pushOnFanout = false
    rewriting.save()
    let written = (try? Data(contentsOf: file))
        .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
    expect("saving uses the new key", written?["push_on_fanout"] as? Bool, false)
    expect("and the delivery preference beside it", written?["push_on_delivery"] as? Bool, true)
    check("the app writes no push_on_finish of its own", written?["push_on_finish"] == nil)
}

group("a delivered turn is the finish a phone hears about") {
    // The wording first, because it is pure: the session's name, the project, and a phrase that
    // says the work is over and the next move is the reader's.
    let plain = Orchestrator.deliveryMessage(project: "clawdline", label: "ship the parser",
                                             summary: "Landed the resume path and its tests.",
                                             smart: false)
    expect("the session's own name is the title", plain.title, "ship the parser")
    expect("the project and the event are the body", plain.body,
           "clawdline " + L.t.pushDelivered)
    expect("with no name to use, the project stands in for it",
           Orchestrator.deliveryMessage(project: "clawdline", label: nil, summary: "x",
                                        smart: false).title,
           "clawdline")
    check("and in English the phrase says a delivery, never that a turn stopped",
          !English().pushDelivered.contains("run") && !English().pushDelivered.contains("turn"))

    // `smart_notifications` spends nothing on this path: the sentence already exists and the
    // assistant wrote it about its own delivery.
    let smart = Orchestrator.deliveryMessage(project: "clawdline", label: nil,
                                             summary: "Landed the resume path and its tests.",
                                             smart: true)
    expect("the receipt's own summary is carried verbatim", smart.body,
           "clawdline · Landed the resume path and its tests.")
    check("which is a different body from the generic one", smart.body != plain.body)

    // And the event itself: one receipt, one push, however many times a root says it again.
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    let deliveryPreference = Config.shared.pushOnDelivery
    let smartPreference = Config.shared.smartNotifications
    defer {
        if let before { try? before.write(to: store, options: .atomic) }
        else { try? FileManager.default.removeItem(at: store) }
        Config.shared.pushOnDelivery = deliveryPreference
        Config.shared.smartNotifications = smartPreference
        Orchestrator.forget()
    }
    Orchestrator.forget()
    Config.shared.pushOnDelivery = true
    Config.shared.smartNotifications = false
    var pushed: [StateHook.PushMessage] = []
    Orchestrator.sessionDeliveryPushForTesting = { pushed.append($0) }

    let identity = Orchestrator.SessionWorkIdentity(
        terminalID: "DELIVERY-TAB", assistant: .claude, tty: "/dev/ttys11", pid: 1100,
        processStart: Date(timeIntervalSince1970: 600), conversationID: "delivery-conversation")
    // How many pushes *this* report produced, rather than how many have happened so far. A running
    // total makes every check after the first divergence red for somebody else's reason, and then
    // one mutation cannot tell you which of them are load-bearing.
    func pushes(reporting summary: String, while state: SessionState,
                at seconds: TimeInterval) -> Int {
        let already = pushed.count
        _ = Orchestrator.reportSessionDelivery(identity: identity, terminalState: state,
                                               summary: summary,
                                               now: Date(timeIntervalSince1970: seconds))
        return pushed.count - already
    }

    let delivered = "Landed the resume path and its tests."
    expect("a first receipt buzzes once",
           pushes(reporting: delivered, while: .working("wrapping up"), at: 700), 1)
    expect("carrying the generic phrase, because smart notifications are off",
           pushed.first?.body, "Clawdline " + L.t.pushDelivered)
    expect("the same report again is the same delivery, and says nothing",
           pushes(reporting: delivered, while: .working("still wrapping up"), at: 701), 0)
    expect("a report from outside its own turn never reaches a phone",
           pushes(reporting: "too late", while: .idle, at: 702), 0)
    expect("a genuinely new receipt is a new event",
           pushes(reporting: "Also fixed the stale snapshot.",
                  while: .working("one more thing"), at: 703), 1)

    Config.shared.pushOnDelivery = false
    expect("and the switch is a switch",
           pushes(reporting: "Wrote the migration down as well.",
                  while: .working("and another"), at: 704), 0)
}

group("smart notifications describe the completed work, not its machinery") {
    let entries = [
        Transcript.Entry(kind: .user, text: "old request", tool: nil, time: nil),
        Transcript.Entry(kind: .assistant, text: "old answer", tool: nil, time: nil),
        Transcript.Entry(kind: .user, text: "fix reconnect delivery", tool: nil, time: nil),
        Transcript.Entry(kind: .tool, text: "Tests/main.swift", tool: "Read", time: nil),
        Transcript.Entry(kind: .toolResult, text: "thousands of noisy bytes", tool: nil,
                         time: nil),
        Transcript.Entry(kind: .assistant,
                         text: "Fixed SSE resume and added a lost-event regression test.",
                         tool: nil, time: nil),
    ]
    expect("only the last request and answer become summary material",
           SmartNotification.context(from: entries),
           SmartNotification.Context(
            request: "fix reconnect delivery",
            outcome: "Fixed SSE resume and added a lost-event regression test."))
    check("a tool result cannot stand in for a completed answer",
          SmartNotification.context(from: Array(entries.dropLast())) == nil)

    let tasks = [
        SmartNotification.TaskLine(title: "resume", state: "success",
                                   summary: "Added SSE replay."),
        SmartNotification.TaskLine(title: "receipts", state: "failure",
                                   summary: "The terminal bridge stayed blocked."),
    ]
    let source = SmartNotification.source(for: tasks) ?? ""
    check("a fan-out names each task, state and authored summary",
          source.contains("resume") && source.contains("success")
            && source.contains("Added SSE replay.") && source.contains("failure")
            && source.contains("The terminal bridge stayed blocked."))

    let envelope = #"{"structured_output":{"summary":"  修好 SSE 重連\n並補上測試。  "}}"#
    expect("structured model output becomes one lock-screen sentence",
           SmartNotification.summary(fromClaudeOutput: envelope), "修好 SSE 重連 並補上測試。")
    check("an empty model answer is a failure, so the ordinary notice can take over",
          SmartNotification.summary(fromClaudeOutput:
            #"{"structured_output":{"summary":"   "}}"#) == nil)
    expect("the project remains visible beside the generated account",
           SmartNotification.body(project: "clawdline", summary: "修好重連。"),
           "clawdline · 修好重連。")
}

group("a headless assistant runs in one directory, not a new one per call") {
    // What this guards is not the scratch directory — that is deleted, and always was. It is the
    // folder Claude Code writes under ~/.claude/projects, named after the working directory and
    // outside anything the spawner can remove. A per-call cwd is one permanent folder per call:
    // 97 from notifications and 22 from the planner by 2026-08-28, against 27 real projects.
    let root = URL(fileURLWithPath: "/tmp/scratch-root", isDirectory: true)
    expect("the working directory carries no per-call component",
           Scratch.directory(for: "smart-notification", in: root).path,
           "/tmp/scratch-root/clawdline-smart-notification")
    check("so every run names the same folder under ~/.claude/projects",
          StartPoints.slug(of: Scratch.directory(for: "smart-notification", in: root).path)
            == StartPoints.slug(of: Scratch.directory(for: "smart-notification", in: root).path))
    let notifierFirst = SmartNotification.scratchDirectory
    let notifierSecond = SmartNotification.scratchDirectory
    check("the notifier's own working directory is that stable one",
          notifierFirst == notifierSecond)
    let plannerFirst = Planner.scratchDirectory
    let plannerSecond = Planner.scratchDirectory
    check("and so is the planner's, which had left 22 folders of its own",
          plannerFirst == plannerSecond)
    check("both stay under the temporary root, where the start list never offers them",
          !StartPoints.isDurablePlace(notifierFirst.path)
            && !StartPoints.isDurablePlace(plannerFirst.path))
    check("two purposes still do not share a directory",
          Scratch.directory(for: "smart-notification", in: root)
            != Scratch.directory(for: "plan-run", in: root))

    // The one thing the per-call directory was really protecting: two runs must not write into
    // one sink. Two Clawdline processes overlap whenever build.sh restarts the app.
    let first = Scratch.file("output", extension: "json", in: root)
    let second = Scratch.file("output", extension: "json", in: root)
    check("each run still gets a sink no other run is holding", first != second)
    check("and both of them are inside the one directory",
          first.deletingLastPathComponent().path == root.path
            && second.deletingLastPathComponent().path == root.path)
    expect("a sink a crash left behind still says what it was",
           Scratch.file("output", extension: "json", in: root,
                        id: UUID(uuidString: "11111111-2222-4333-8444-555555555555")!)
             .lastPathComponent,
           "output-11111111-2222-4333-8444-555555555555.json")
}

group("a burst of finishes becomes one push, not one each") {
    let base = Date(timeIntervalSince1970: 1_000_000)
    expect("a lone finish waits only for its transcript to settle",
           SmartNotification.batchReadyAt(oldest: base, newest: base),
           base.addingTimeInterval(0.35))
    expect("a steady stream cannot postpone the batch past the cap",
           SmartNotification.batchReadyAt(oldest: base, newest: base.addingTimeInterval(5)),
           base.addingTimeInterval(1.0))
    check("the queue outlasts the worst observed burst (44 finishes in 120 ms)",
          SmartNotification.maxQueued >= 44)

    let combined = SmartNotification.coalescedSource([
        (project: "clawdline", source: #"{"request":"fix reconnect","outcome":"Fixed SSE resume."}"#),
        (project: "parser", source: nil),
    ]) ?? ""
    check("each member keeps its project name so one sentence can tell them apart",
          combined.contains("clawdline") && combined.contains("parser"))
    check("a member's conversation rides along as data",
          combined.contains("Fixed SSE resume."))
    check("an unreadable member still counts as a finish",
          SmartNotification.coalescedSource([(project: "solo", source: nil)]) != nil)

    expect("in English a coalesced push says how many",
           English().pushCoalesced(count: 11), "11 jobs finished together")
    expect("in Traditional Chinese too",
           TraditionalChinese().pushCoalesced(count: 11), "11 件工作同時完成")
    expect("the ordinary wording survives coalescing, head first",
           SmartNotification.coalescedBody(["a · one", "b · two"]), "a · one / b · two")
}

group("smart notification health is visible where the switch is") {
    var health = SmartNotification.Health()
    check("before anything resolves there is no verdict", health.lastResolvedWasSuccess == nil)
    health.attempt(44)
    health.failure(.queueFull, at: Date(timeIntervalSince1970: 100))
    check("a failure is the last word until something works",
          health.lastResolvedWasSuccess == false)
    health.success(1, at: Date(timeIntervalSince1970: 200))
    check("a later success takes the verdict back", health.lastResolvedWasSuccess == true)
    expect("attempts are counted per finish, drops included", health.attempts, 44)
    expect("successes only when a model sentence was delivered", health.successes, 1)

    let copy = English()
    var failing = SmartNotification.Health()
    failing.attempt(8)
    failing.failure(.timedOut, at: Date(timeIntervalSince1970: 0))
    let line = SmartNotification.healthLine(failing, copy: copy)
    check("a timing-out model is said in words, not folded into a generic failure",
          line.contains(copy.settingsSmartTimeout(seconds: Int(SmartNotification.timeout))))
    check("the counts stay beside the reason",
          line.contains(copy.settingsSmartHealth(attempts: 8, successes: 0)))
    expect("before the first attempt the row says exactly that",
           SmartNotification.healthLine(SmartNotification.Health(), copy: copy),
           copy.settingsSmartHealthIdle)
    check("the timeout words name the number of seconds a person would raise",
          copy.settingsSmartTimeout(seconds: 30).contains("30"))
    check("the deadline clears the slowest measured run (12.9 s) with headroom",
          SmartNotification.timeout >= 26)
    expect("in Traditional Chinese the timeout is words a person can act on",
           TraditionalChinese().settingsSmartTimeout(seconds: 30),
           "Claude 超過 30 秒沒完成，被中止")
}

group("a push payload keeps the beginning of a sentence that does not fit") {
    let beginning = "KEEP THE FORECAST: "
    let ending = " THROW THIS TAIL AWAY"
    let body = beginning + String(repeating: "天", count: 2_000) + ending
    let made = WebPush.notificationPayload(title: "weather", body: body, url: "/",
                                           tag: nil,
                                           icon: "/project-64-a-very-long-decoration.png")
    check("the final plaintext stays inside the RFC 8291 ceiling",
          (made?.count ?? Int.max) <= WebPush.maxPayload)
    let obj = made.flatMap {
        try? JSONSerialization.jsonObject(with: $0) as? [String: Any]
    }
    let shortened = obj?["body"] as? String ?? ""
    check("truncation keeps the head", shortened.hasPrefix(beginning))
    check("and discards the tail", !shortened.hasSuffix(ending))
}

group("a notification's deep link carries a session id a browser can read back") {
    // The id this Mac actually watches. `Sources/Tmux.swift` calls `%12` "stable for the life of
    // the pane", and `tmux list-panes -a -F '#{pane_id}'` prints `%141` here.
    expect("a tmux pane id arrives with its per-cent encoded",
           WebPush.sessionURL(forSessionID: "%141"), "/#session=%25141")
    // What the fragment used to say, and the whole of the bug: `decodeURIComponent("%141")` does
    // not throw — `%14` is a complete escape — so the page went looking for a session whose id is
    // U+0014 followed by "1", found none, and stopped on the list.
    check("which is not the raw id the page could not find",
          WebPush.sessionURL(forSessionID: "%141") != "/#session=%141")
    // The control group: the ids that were never broken must not start moving now. An iTerm
    // session id is `w0t0p0:<UUID>`, and every character of it is either unreserved or a colon.
    expect("an iTerm session id is untouched by the encoding",
           WebPush.sessionURL(forSessionID: "w0t0p0:1234-ABCD"),
           "/#session=w0t0p0%3A1234-ABCD")
    expect("and its unreserved characters stay themselves",
           WebPush.sessionURL(forSessionID: "a-z_0.9~Q"), "/#session=a-z_0.9~Q")

    // Why the allowed set is unreserved rather than `.urlFragmentAllowed`, which permits all
    // three of these: the reader is `/(?:^|[#&])session=([^&]*)/`, so an id carrying one of them
    // would be cut in half by the character that was let through.
    for cutter in ["&", "=", "#"] {
        let made = WebPush.sessionURL(forSessionID: "a\(cutter)b")
        check("a session id containing \(cutter) does not carry it into the fragment",
              !made.dropFirst("/#session=".count).contains(cutter))
    }

    // What the web app does with it, spelled out here because the two halves live in different
    // languages and only agreeing makes either of them right.
    let written = WebPush.sessionURL(forSessionID: "%141")
    let fragment = String(written.dropFirst("/#session=".count))
    expect("and the fragment decodes back to exactly the pane it named",
           fragment.removingPercentEncoding, "%141")
}

group("push-service receipts distinguish acceptance from refusal") {
    check("a 201 is an accepted push receipt", WebPush.serviceAccepted(status: 201))
    check("a service-side 4xx is a failed push receipt", !WebPush.serviceAccepted(status: 403))
    check("a service-side 5xx is a failed push receipt", !WebPush.serviceAccepted(status: 503))
}

group("a project mark small enough to be a URL") {
    let cells: [[NSColor?]] = [
        [nil, ProjectIcon.color(hex: "#D97757"), nil],
        [ProjectIcon.color(hex: "#141416"), nil, ProjectIcon.color(hex: "#0E0E11")],
    ]
    let packed = RemoteIcon.pack(cells)
    check("a grid packs", packed != nil)

    let back = packed.flatMap(RemoteIcon.unpack)
    check("and comes back the same shape", back?.count == 2 && back?.first?.count == 3)
    expect("with the holes still holes", back?[0][0] == nil && back?[1][1] == nil, true)
    expect("and the colours to the byte", back.map { ProjectIcon.hex($0[0][1]!) }, "#D97757")
    expect("including the ones that are nearly the ground", back.map { ProjectIcon.hex($0[1][2]!) },
           "#0E0E11")

    // The whole reason for the format: it has to survive being a path component.
    let text = packed ?? ""
    check("the packing is URL-safe", !text.contains("+") && !text.contains("/")
          && !text.contains("="))
    check("and a 7x4 registry drawing stays short", (RemoteIcon.pack(
        Array(repeating: Array(repeating: NSColor.red as NSColor?, count: 7), count: 4)) ?? "")
        .count < 200)

    // Everything below arrives from outside as a string somebody could have typed.
    check("a truncated body is refused", RemoteIcon.unpack(String(text.dropLast(4))) == nil)
    check("nonsense is refused", RemoteIcon.unpack("not-base64-at-all!!") == nil)
    check("an empty string is refused", RemoteIcon.unpack("") == nil)
    check("a grid claiming to be enormous is refused",
          RemoteIcon.unpack(WebPush.base64url(Data([255, 255, 0, 0, 0, 0]))) == nil)
    check("a header with no cells behind it is refused",
          RemoteIcon.unpack(WebPush.base64url(Data([2, 2]))) == nil)
    check("a grid with no rows is refused", RemoteIcon.pack([]) == nil)
    check("a grid too wide to draw is refused",
          RemoteIcon.pack([Array(repeating: NSColor.red as NSColor?, count: 33)]) == nil)
}

group("state hook: finding the program") {
    // A GUI app has launchd's PATH, not a login shell's, so a bare name has to be looked for in
    // more places than PATH names — otherwise everything installed by Homebrew is unreachable.
    expect("an absolute path is taken as one", StateHook.resolve("/bin/sh"), "/bin/sh")
    expect("one that is not there resolves to nothing", StateHook.resolve("/nope/nope"), nil)
    // A slash means a path, so this is not hunted for in /usr/bin under the same name.
    expect("a relative path is not searched for", StateHook.resolve("./nope"), nil)
    check("a bare name is found", StateHook.resolve("sh")?.hasSuffix("/sh") == true)
    expect("a bare name that exists nowhere is nothing",
           StateHook.resolve("clawdline-no-such-program"), nil)
}

group("a hook written as #!/usr/bin/env node has to be able to find node") {
    // An app launched from Finder gets the launchd PATH, which has no Homebrew in it — so the
    // shebang fails for a reason that has nothing to do with the script.
    let launchd = "/usr/bin:/bin:/usr/sbin:/sbin"
    let fixed = StateHook.usefulPath(launchd)
    check("homebrew goes in front", fixed.hasPrefix("/opt/homebrew/bin:"))
    check("and /usr/local with it", fixed.contains("/usr/local/bin"))
    check("what was there is still there", fixed.hasSuffix(launchd))
    // Somebody's ordering is somebody's ordering: only what is genuinely absent goes in front,
    // and what was already there keeps its place rather than being hoisted.
    let mine = "/opt/homebrew/bin:/usr/bin:/bin"
    check("a directory already listed is not listed twice",
          StateHook.usefulPath(mine).split(separator: ":").filter { $0 == "/opt/homebrew/bin" }.count == 1)
    check("and the original ordering survives at the end",
          StateHook.usefulPath(mine).hasSuffix(mine))
    let complete = "/opt/homebrew/bin:/usr/local/bin:" + NSHomeDirectory() + "/.local/bin:"
        + NSHomeDirectory() + "/.bun/bin:/usr/bin"
    expect("nothing missing means nothing is touched at all",
           StateHook.usefulPath(complete), complete)
    check("an empty PATH still gets somewhere to look",
          StateHook.usefulPath("").contains("/usr/bin"))
    check("and so does a missing one", StateHook.usefulPath(nil).contains("/usr/bin"))
}

group("pairing tells a client apart from a sentence") {
    // Wrong and lapsed used to be the same code with different English in them, so a page could
    // only tell them apart by reading the message — the one part of an error nobody should ever
    // branch on. They are different things to do about: try again, versus start again.
    let entry = RemoteAuth.beginPairing(name: "a test")
    if case .wrongCode(let left) = RemoteAuth.confirmPairing(id: entry.id, code: "000000") {
        expect("a wrong code says how many are left", left, 4)
    } else {
        check("a wrong code is a wrong code", false)
    }
    // Five and it is gone, and gone reads as expired rather than as another wrong guess.
    for _ in 0..<3 { _ = RemoteAuth.confirmPairing(id: entry.id, code: "000000") }
    expect("the fifth wrong code ends it",
           RemoteAuth.confirmPairing(id: entry.id, code: "000000"), .expired)
    expect("and it stays ended",
           RemoteAuth.confirmPairing(id: entry.id, code: entry.code), .expired)

    // Only one pairing is ever live: two codes would be two chances to guess, and the person at
    // the Mac is looking at one window.
    let first = RemoteAuth.beginPairing(name: "first")
    let second = RemoteAuth.beginPairing(name: "second")
    expect("a new request replaces the old one",
           RemoteAuth.confirmPairing(id: first.id, code: first.code), .expired)
    if case .paired(let token) = RemoteAuth.confirmPairing(id: second.id, code: second.code) {
        check("and the newest one is the one that works", token.count >= 40)
        RemoteAuth.revoke(id: second.id)
    } else {
        check("and the newest one is the one that works", false)
    }
}

group("the cloudflared config we write, rather than the one that was already there") {
    // Only what cloudflared will act on: the file leads with a comment explaining why it exists,
    // and that comment says the words "ingress" and "tunnel:" out loud.
    func settings(_ yaml: String) -> String {
        yaml.split(separator: "\n", omittingEmptySubsequences: false)
            .filter { !$0.trimmingCharacters(in: .whitespaces).hasPrefix("#") }
            .joined(separator: "\n")
    }
    let quick = settings(RemoteTunnel.configFile(for: RemoteTunnel.Plan(mode: .quick, port: 7717)))
    check("a quick tunnel gets no ingress at all — its address cannot be known in advance",
          !quick.contains("ingress"))
    check("and no tunnel name to be overridden by", !quick.contains("tunnel:"))
    // Not literally empty: cloudflared logs an error about an empty configuration file, in the
    // one place somebody looks when a tunnel will not come up.
    check("but it is not empty either", quick.contains("no-autoupdate: true"))

    let named = settings(RemoteTunnel.configFile(
        for: RemoteTunnel.Plan(mode: .named, name: "clawd", hostname: "clawd.example.com",
                               port: 7717),
        credentials: "/Users/x/.cloudflared/abc.json"))
    check("a named tunnel names itself", named.contains("tunnel: clawd"))
    check("and says where its credentials are",
          named.contains("credentials-file: /Users/x/.cloudflared/abc.json"))
    check("and maps the hostname at the port", named.contains("hostname: clawd.example.com"))
    check("to the loopback address and nothing else",
          named.contains("service: http://127.0.0.1:7717"))
    // A stream that is meant never to end, versus a proxy that answers for anybody.
    check("the event stream is given room", named.contains("connectTimeout: 30s"))
    check("and everything else is refused", named.contains("service: http_status:404"))
    check("credentials are left out when they could not be found",
          !settings(RemoteTunnel.configFile(for: RemoteTunnel.Plan(mode: .named, name: "c",
                                                                    hostname: "h", port: 1),
                                            credentials: nil)).contains("credentials-file"))
}

group("which language to answer a browser in") {
    // A browser may write its preferences in any order and let `q` say what it means, and several
    // do — so the order on the wire is not the order of preference.
    expect("plain order", L.preferences(in: "zh-TW,zh,en"), ["zh-TW", "zh", "en"])
    expect("quality wins over position",
           L.preferences(in: "en;q=0.5,zh-TW;q=0.9,ja"), ["ja", "zh-TW", "en"])
    expect("a missing q is 1.0, the specification's default",
           L.preferences(in: "de,en;q=0.9"), ["de", "en"])
    expect("ties keep the order they were written in",
           L.preferences(in: "fr;q=0.8,it;q=0.8"), ["fr", "it"])
    expect("the wildcard is not a language", L.preferences(in: "*"), [])
    expect("and neither is nothing at all", L.preferences(in: ""), [])
    expect("whitespace is not part of a tag",
           L.preferences(in: "zh-TW, en;q=0.7"), ["zh-TW", "en"])

    // The catalog's ordering is what stops zh-Hans falling into the Traditional bucket.
    check("a Taiwanese browser gets Traditional",
          L.copy(preferring: ["zh-TW"]).settingsRemote == TraditionalChinese().settingsRemote)
    check("and a mainland one does not",
          L.copy(preferring: ["zh-CN"]).settingsRemote == SimplifiedChinese().settingsRemote)
    check("a language nobody here speaks falls back to English",
          L.copy(preferring: ["is-IS"]).settingsRemote == English().settingsRemote)
    check("and so does an empty list", L.copy(preferring: []).settingsRemote == English().settingsRemote)
}

group("a cache stamp has to see through a symlink") {
    // The bug this exists for: ~/.claude/project-icons.json is normally a link into a checkout,
    // and neither attributesOfItem(atPath:) nor URL.resourceValues(forKeys:) follows one. Both
    // describe the link — 64 bytes, and the modification time of the day it was made — so the
    // stamp never changed, the registry could be rewritten all afternoon, and only relaunching
    // the app picked it up.
    let dir = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-stamp-\(getpid())")
    let fm = FileManager.default
    try? fm.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? fm.removeItem(at: dir) }

    let real = dir.appendingPathComponent("real.json")
    let link = dir.appendingPathComponent("link.json")
    try? Data("first".utf8).write(to: real)
    try? fm.createSymbolicLink(at: link, withDestinationURL: real)

    let before = Paths.stamp(of: link)
    check("a link and its target stamp the same", before == Paths.stamp(of: real))

    // Enough of a change to move the size, which is the half that does not depend on a clock.
    try? Data("second, and appreciably longer than the first".utf8).write(to: real)
    check("writing through the target changes the link's stamp", Paths.stamp(of: link) != before)
    check("and it still agrees with the target", Paths.stamp(of: link) == Paths.stamp(of: real))

    check("a file that is not there stamps as nothing rather than crashing",
          Paths.stamp(of: dir.appendingPathComponent("absent.json")) == "0-0")
}

group("telling two devices with the same name apart") {
    // Pressing "Open in a browser" twice mints two devices called the same thing, and two
    // identical rows is a list nobody can act on. What separates them is not a better name —
    // two browsers deserve the same name — it is when each was last used.
    let now = Date()
    func ago(_ seconds: TimeInterval) -> String? {
        DeviceChips.ago(now.addingTimeInterval(-seconds), now: now)
    }
    expect("just now", ago(5), "now")
    expect("under an hour is minutes", ago(125), "2m")
    expect("under a day is hours", ago(3 * 3600 + 100), "3h")
    expect("beyond that is days", ago(4 * 86_400), "4d")
    expect("a clock that ran backwards is not negative", ago(-30), "now")
    // The one that matters: a device minted and never used has no time at all, and that is
    // exactly the one it is safe to take away.
    check("never seen says nothing", DeviceChips.ago(nil) == nil)
}

group("a top-level state from the wrong vocabulary") {
    // The two `state` keys mean different things, and the first project outside this repository
    // to write one of these sent `healthy` — a process word — at the top. Taken literally it put
    // a healthy stack under a mark that said nobody had agreed to run it.
    func read(_ json: String) -> DevStack.State? { DevStack.parseState(Data(json.utf8)) }
    let processes = """
    "processes": [{"name": "api", "port": 1, "state": "healthy"},
                  {"name": "web", "port": 2, "state": "healthy"}]
    """
    expect("a process word at the top is not believed",
           read("{\"state\": \"healthy\", \(processes)}")?.state, "running")
    expect("and neither is anything else unrecognised",
           read("{\"state\": \"lovely\", \(processes)}")?.state, "running")
    expect("one of the four is taken as written",
           read("{\"state\": \"partial\", \(processes)}")?.state, "partial")
    expect("unknown is one of the four, and cannot be derived",
           read("{\"state\": \"unknown\", \(processes)}")?.state, "unknown")
    expect("no top-level state at all derives one",
           read("{\(processes)}")?.state, "running")
    expect("and a process that is down makes it partial",
           read("{\"processes\": [{\"name\": \"a\", \"state\": \"healthy\"}, "
                + "{\"name\": \"b\", \"state\": \"exited\"}]}")?.state, "partial")
}
}
