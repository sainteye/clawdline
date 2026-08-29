import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - Dev stacks

func runDevStackTests() {
group("devstack: a project describes its stack") {
    let json = """
    {"version": 1, "name": "cairn",
     "status": "make stack-status",
     "restart": "make stack-restart P={process}",
     "logs": "make stack-logs P={process} N={lines}"}
    """
    let spec = DevStack.parse(Data(json.utf8), root: "/tmp/cairn")
    check("it parses", spec != nil)
    expect("name", spec?.name, "cairn")
    expect("root", spec?.root, "/tmp/cairn")
    expect("status command", spec?.status, "make stack-status")
    check("absent commands stay absent", spec?.up == nil)
}

group("devstack: a file with nothing but a name still works") {
    // The contract's floor. Everything but `name` is optional, and a reader that refused this
    // would be telling adopters to write more before they can see anything at all.
    let spec = DevStack.parse(Data("{}".utf8), root: "/tmp/whatever")
    check("it parses", spec != nil)
    expect("name falls back to the directory", spec?.name, "whatever")
    check("nothing is declared", spec?.declared.isEmpty == true)
    check("no commands", spec?.status == nil && spec?.up == nil)
}

group("devstack: tier 0 declares ports and nothing else") {
    let json = """
    {"name": "app", "up": "make dev",
     "processes": [{"name": "api", "port": 8002}, {"name": "web", "port": 3001}]}
    """
    let spec = DevStack.parse(Data(json.utf8), root: "/tmp/app")
    expect("two processes declared", spec?.declared.count, 2)
    expect("first port", spec?.declared.first?.port, 8002)
    check("no status command is fine", spec?.status == nil)
}

group("devstack: garbage is refused, not guessed at") {
    check("not JSON", DevStack.parse(Data("nope".utf8), root: "/tmp/x") == nil)
    check("JSON but not an object", DevStack.parse(Data("[1,2]".utf8), root: "/tmp/x") == nil)
}

group("devstack: the fingerprint follows the bytes") {
    let a = DevStack.fingerprint(Data("{\"name\":\"a\"}".utf8))
    let b = DevStack.fingerprint(Data("{\"name\":\"a\"}".utf8))
    let c = DevStack.fingerprint(Data("{\"name\":\"b\"}".utf8))
    check("same bytes, same fingerprint", a == b)
    // Trust is tied to this. If an edit did not change it, adding a command to a trusted file
    // would run without ever being agreed to.
    check("different bytes, different fingerprint", a != c)
}

group("devstack: the state document") {
    let json = """
    {"state": "partial", "updated_at": 1786949616,
     "processes": [
       {"name": "api", "state": "healthy", "port": 8002, "pid": 7970},
       {"name": "build-web", "state": "exited", "exit_code": 1,
        "error": "Cannot find module next/font/google"},
       {"name": "web", "state": "healthy", "pid": 8243}]}
    """
    let s = DevStack.parseState(Data(json.utf8))
    check("it parses", s != nil)
    expect("verdict", s?.state, "partial")
    expect("three processes", s?.processes.count, 3)
    expect("two are up", s?.upCount, 2)
    expect("the broken one is named", s?.brokenNames, ["build-web"])
    expect("its error travels with it", s?.processes[1].error,
           "Cannot find module next/font/google")
    // `completed` is a one-shot that finished on purpose. Drawing it as a failure would put a
    // red dot on every successful build, which trains people to ignore red dots.
    let done = DevStack.parseState(Data("""
    {"processes": [{"name": "build", "state": "completed"}]}
    """.utf8))
    expect("completed counts as up", done?.upCount, 1)
    expect("and the verdict is derived", done?.state, "running")
}

group("devstack: a state document missing its verdict still lights the dot") {
    // These files are written by shell scripts. One that forgot a key should degrade, not vanish.
    let s = DevStack.parseState(Data("""
    {"processes": [{"name": "api", "state": "exited", "exit_code": 1}]}
    """.utf8))
    expect("derived from the processes", s?.state, "partial")
}

group("devstack: the line worth showing out of a process's dying words") {
    // Every fixture here is real. These are the shapes three projects on this machine handed
    // back on the morning the row was found to be unreadable — one of them a JSON envelope, one
    // a success message under a red mark, one a tail that stopped in the middle of a brace.

    // Sixty characters of bookkeeping in front of the answer. Cut at seventy, as the row used
    // to, and it stopped two characters into the part that meant anything.
    let wrapped = DevStack.parseState(Data(#"""
    {"processes": [{"name": "build-web", "state": "exited", "exit_code": 127,
      "error": "{\"level\":\"error\",\"process\":\"build-web\",\"replica\":0,\"message\":\"bash: npm: command not found\"}"}]}
    """#.utf8))
    expect("the sentence, not the envelope around it",
           wrapped?.processes.first?.reason, "bash: npm: command not found")

    // A web server prints request logs until the moment it dies, so the last line of the tail is
    // routinely a success. Offering that as the explanation for a crash is worse than silence.
    let noisy = DevStack.parseState(Data(#"""
    {"processes": [{"name": "api", "state": "exited", "exit_code": 1,
      "error": "{\"level\":\"error\",\"message\":\"ERROR: address already in use\"}\n{\"level\":\"info\",\"message\":\"INFO: GET /health 200 OK\"}"}]}
    """#.utf8))
    expect("the failure, not the last thing it happened to print",
           noisy?.processes.first?.reason, "ERROR: address already in use")

    // And when nothing in the tail announces itself, say nothing. An exit code with no
    // explanation is a smaller claim than a wrong explanation.
    let quiet = DevStack.parseState(Data(#"""
    {"processes": [{"name": "api", "state": "exited", "exit_code": 1,
      "error": "{\"level\":\"info\",\"message\":\"INFO: GET /health 200 OK\"}"}]}
    """#.utf8))
    check("a tail with nothing loud in it explains nothing rather than guessing",
          quiet?.processes.first?.reason == nil)

    // The tail is cut to a byte budget by whoever wrote it, so it often ends mid-envelope.
    let cut = DevStack.parseState(Data(#"""
    {"processes": [{"name": "tunnel", "state": "exited", "exit_code": 1,
      "error": "{\"level\":\"error\",\"message\":\"ERR failed to accept incoming stream\"}\n{\"level\":"}]}
    """#.utf8))
    expect("half an envelope is dropped, not shown",
           cut?.processes.first?.reason, "ERR failed to accept incoming stream")

    // process-compose files everything written to stderr under `level: error`, and cloudflared
    // writes its startup chatter there. Believing the envelope about a line that says `INF` in
    // its own text offers a connection notice as the reason a tunnel died — the same wrong
    // answer as trusting the last line, arrived at from the other direction.
    let chatter = DevStack.parseState(Data(#"""
    {"processes": [{"name": "tunnel", "state": "exited", "exit_code": 1,
      "error": "{\"level\":\"error\",\"message\":\"2026-08-22T13:03:55Z ERR failed to accept QUIC stream\"}\n{\"level\":\"error\",\"message\":\"2026-08-22T13:03:55Z INF Tunnel connection curve preferences\"}"}]}
    """#.utf8))
    expect("a line that calls itself INF is not the reason, whatever the envelope says",
           chatter?.processes.first?.reason, "2026-08-22T13:03:55Z ERR failed to accept QUIC stream")
    check("and the text is what settles it",
          StackLog.declaresCalm("2026-08-22T13:03:55Z INF Tunnel connection")
            && !StackLog.declaresCalm("bash: npm: command not found"))
    check("a line leading with a real severity is not calm for mentioning info later",
          !StackLog.declaresCalm("ERROR: could not read info file"))

    // cloudflared, uvicorn and next all colour their own output, and it survives into the tail.
    let coloured = DevStack.parseState(Data(#"""
    {"processes": [{"name": "web", "state": "exited", "exit_code": 1,
      "error": "\u001b[31mERROR\u001b[0m  build failed"}]}
    """#.utf8))
    expect("colour codes come off", coloured?.processes.first?.reason, "ERROR  build failed")

    check("and a process that left nothing behind has no reason to give",
          DevStack.parseState(Data(#"{"processes":[{"name":"web","state":"exited","exit_code":1}]}"#.utf8))?
            .processes.first?.reason == nil)
}

group("devstack: which broken process the row names") {
    // atrium, as its status command printed it. `blog` is first alphabetically and says nothing
    // — it exits silently because the build it waits on never completed. `build-blog` is the one
    // that actually failed, and the only entry in the whole document that says why. The row
    // named `blog` and drew no explanation at all, which is how three stacks stayed broken.
    let atrium = DevStack.parseState(Data(#"""
    {"state": "partial", "processes": [
      {"name": "api", "state": "healthy", "port": 8004},
      {"name": "blog", "state": "exited", "port": 4324, "exit_code": 1},
      {"name": "build-blog", "state": "exited", "exit_code": 127,
       "error": "{\"level\":\"error\",\"message\":\"bash: npm: command not found\"}"},
      {"name": "web", "state": "exited", "port": 3004, "exit_code": 1}]}
    """#.utf8))
    expect("three are down", atrium?.brokenNames.count, 3)
    expect("and the one named is the cause, not the first casualty",
           atrium?.rootCause?.name, "build-blog")

    // What the phone is told about a row it can actually see. `build-blog` listens on nothing,
    // so it never appears in a list of addresses; `blog` does, and answers for both.
    expect("a silent casualty is told what took it down",
           atrium.flatMap { s in s.processes.first { $0.name == "blog" }.flatMap(s.why) },
           "build-blog: bash: npm: command not found")
    expect("and the cause speaks for itself",
           atrium.flatMap { s in s.processes.first { $0.name == "build-blog" }.flatMap(s.why) },
           "bash: npm: command not found")

    // 127 is a shell saying "command not found" and is almost always a cause; 1 is the code
    // everything uses for everything, and says nothing about who started it.
    let codes = DevStack.parseState(Data(#"""
    {"processes": [{"name": "web", "state": "exited", "exit_code": 1},
                   {"name": "build", "state": "exited", "exit_code": 127}]}
    """#.utf8))
    expect("a specific exit code outranks a generic one", codes?.rootCause?.name, "build")

    // A tie keeps the document's own order, so the row does not reshuffle itself every read.
    let tied = DevStack.parseState(Data(#"""
    {"processes": [{"name": "a", "state": "exited", "exit_code": 1},
                   {"name": "b", "state": "exited", "exit_code": 1}]}
    """#.utf8))
    expect("nothing to choose between them keeps the first", tied?.rootCause?.name, "a")
    check("and a stack with nothing down names nobody",
          DevStack.parseState(Data(#"{"processes":[{"name":"api","state":"healthy"}]}"#.utf8))?
            .rootCause == nil)
}

group("devstack: commands substitute rather than concatenate") {
    // Where the process name goes is the project's decision: `make stack-restart P=api` and
    // `overmind restart api` do not put it in the same place.
    expect("make style", DevStack.expand("make stack-restart P={process}", process: "api"),
           "make stack-restart P='api'")
    expect("bare argument", DevStack.expand("overmind restart {process}", process: "web"),
           "overmind restart 'web'")
    expect("line counts too",
           DevStack.expand("make logs P={process} N={lines}", process: "api", lines: 50),
           "make logs P='api' N=50")
    // "Restart everything" drops the whole word, so no `P=` is left dangling to be read as an
    // empty process name.
    expect("no process drops the word",
           DevStack.expand("make stack-restart P={process}", process: nil),
           "make stack-restart")
    expect("quoting survives a hostile name",
           DevStack.expand("x {process}", process: "a'b"), "x 'a'\\''b'")
}

group("devstack: the documented example files") {
    // docs/devstack.md tells other people how to write these. The examples beside it are parsed
    // here with the same code the app uses, so the page cannot quietly stop being true.
    func load(_ name: String) -> Data? {
        try? Data(contentsOf: URL(fileURLWithPath: "docs/examples/" + name))
    }
    let tier1 = load("devstack.json").flatMap { DevStack.parse($0, root: "/Users/you/code/atrium") }
    check("the tier 1 example is there", tier1 != nil)
    expect("it names itself", tier1?.name, "atrium")
    check("it can be asked for state", tier1?.status != nil)
    check("and restarted per process", tier1?.restart?.contains("{process}") == true)

    let tier0 = load("devstack-tier0.json").flatMap { DevStack.parse($0, root: "/tmp/myapp") }
    check("the tier 0 example is there", tier0 != nil)
    expect("it declares its ports", tier0?.declared.count, 2)
    check("without a status command", tier0?.status == nil)
    // The rung that makes this a format rather than an integration: no supervisor, still visible.
    if let tier0 {
        expect("so it is read by probing", DevStack.probeDeclared(tier0).processes.count, 2)
    } else {
        check("so it is read by probing", false, "the tier 0 example was unavailable")
    }

    let state = load("devstack-state.json").flatMap { DevStack.parseState($0) }
    check("the state example is there", state != nil)
    expect("its verdict", state?.state, "partial")
    expect("four of five up", state?.upCount, 4)
    expect("and it names what broke", state?.brokenNames, ["build-web"])
    // The one thing in a stack that is a place rather than a number. The row turns it into a
    // link, so confirming a site is really up is a click and not a retyped address.
    expect("the tunnel carries the address it serves",
           state?.processes.first { $0.name == "tunnel" }?.url, "https://dev.example.com")
}

group("stack logs: unwrapping the supervisor's envelope") {
    let line = #"{"level":"error","process":"api","replica":0,"message":"INFO: GET /health 200"}"#
    expect("the line the server actually printed", StackLog.unwrap(line), "INFO: GET /health 200")
    // A project whose `logs` is plain `tail` must not come out empty for not being JSON.
    expect("plain lines pass through", StackLog.unwrap("just a line"), "just a line")
    expect("so does something JSON-shaped but not an envelope",
           StackLog.unwrap(#"{"a":1}"#), "")
    expect("and a brace that starts a sentence", StackLog.unwrap("{not json"), "{not json")
}

group("stack logs: the pipe a line came down, kept for the post-mortem") {
    // Worthless for colouring — process-compose calls everything written to stderr an error, and
    // believing it paints a healthy stack red. Worth a great deal in the tail of a process that
    // has *already exited*, where "the last thing it wrote to stderr" is a much better guess at
    // what killed it than "the last thing it wrote".
    let e = StackLog.envelope(#"{"level":"error","process":"api","message":"bash: npm: command not found"}"#)
    expect("the message", e.message, "bash: npm: command not found")
    expect("and the level it was logged at", e.level, "error")
    check("a line that was never wrapped has no level",
          StackLog.envelope("just a line").level == nil)
}

group("stack logs: severity comes from the text, not the pipe") {
    // process-compose labels everything on stderr as level=error, and uvicorn, next, mkdocs and
    // cloudflared all log their ordinary progress there. Believing that field paints a healthy
    // stack entirely red, after which nobody reads the colour again.
    check("an INFO line is not an error", StackLog.level(of: "INFO: GET /health 200") == .normal)
    check("an ERROR line is", StackLog.level(of: "ERROR - could not connect") == .error)
    check("a warning is a warning", StackLog.level(of: "WARNING: deprecated") == .warning)
    // A request path is not a severity.
    check("a path containing the word does not count",
          StackLog.level(of: "GET /api/v1/error-report 200") == .normal)
}

group("stack logs: the three shapes a logs command can hand back") {
    // tail over files, with banners.
    let files = """
    ==> logs/stack/api.log <==
    {"level":"info","process":"api","message":"listening on 8002"}
    ==> logs/stack/web.log <==
    {"level":"info","process":"web","message":"ready"}
    """
    let a = StackLog.entries(files)
    expect("one entry per line", a.count, 2)
    expect("named by the file, without path or suffix", a.first?.process, "api")
    expect("and unwrapped", a.first?.message, "listening on 8002")

    // process-compose's own multi-process prefix.
    let prefixed = "[web\t]  ✓ Ready in 244ms\n[api\t]  INFO: started"
    let b = StackLog.entries(prefixed)
    expect("two entries", b.count, 2)
    expect("first is web", b.first?.process, "web")
    expect("with the bracket gone", b.first?.message, "✓ Ready in 244ms")

    // A single process: clean lines, no prefix, no envelope. Nothing to strip.
    let plain = StackLog.entries("INFO: one\nINFO: two")
    expect("both kept", plain.count, 2)
    expect("verbatim", plain.first?.message, "INFO: one")
    expect("with no process to name", plain.first?.process, "")
}

group("stack logs: the timestamp every line starts with") {
    // Dimming it is most of what makes a wall of log readable — it is the same eighteen
    // characters on every row and the least informative part of each.
    let line = "2026-08-18 10:39:28 INFO    cairn.access GET /health → 200"
    expect("date and time, up to the following space",
           StackLog.timestampLength(line), 20)
    expect("a bare clock too", StackLog.timestampLength("10:39:28 INFO x"), 9)
    expect("nothing to dim when there is no stamp",
           StackLog.timestampLength("INFO: 127.0.0.1 - GET /"), 0)
    // A version number is not a time.
    expect("and not a version", StackLog.timestampLength("v1.120.0 released"), 0)
}

group("devstack: port probing") {
    // Port 1 needs root to bind, so nothing on a normal machine is listening there.
    check("nothing answers on port 1", !DevStack.isListening(port: 1))
    check("out of range is not listening", !DevStack.isListening(port: 0))
    check("also out of range", !DevStack.isListening(port: 70000))
}

group("the label a tab shows, without the animation on the front") {
    // Claude Code used to put a fixed ✳ in front of the title, which was worth keeping. 2.1.228
    // cycles half circles through it instead, so the same tab reads differently four times a
    // second — and a label that changes on its own is noise on every surface that draws one.
    expect("a half circle goes", TargetSession.withoutStatusGlyph("◐ IG 設定指引改進"), "IG 設定指引改進")
    expect("and so does the next frame", TargetSession.withoutStatusGlyph("◑ 評估動態島實現機制"), "評估動態島實現機制")
    expect("braille too", TargetSession.withoutStatusGlyph("⠐ 設計基本問題"), "設計基本問題")
    expect("and the ✳ it used to be", TargetSession.withoutStatusGlyph("✳ investigate the webhook"),
           "investigate the webhook")

    // What must survive. A title is allowed to start with punctuation, and a marker is only a
    // marker when it stands alone in front of one.
    expect("a title that starts with a quote is a title",
           TargetSession.withoutStatusGlyph("\"why\" is the question"), "\"why\" is the question")
    expect("a glyph with no space after it is part of the word",
           TargetSession.withoutStatusGlyph("◐IG"), "◐IG")
    expect("a letter is never a marker", TargetSession.withoutStatusGlyph("a b"), "a b")
    expect("a digit is never a marker", TargetSession.withoutStatusGlyph("1 b"), "1 b")
    expect("an ordinary title is untouched",
           TargetSession.withoutStatusGlyph("fix the retry backoff"), "fix the retry backoff")
    expect("and nothing is taken off something too short to have a marker",
           TargetSession.withoutStatusGlyph("◐ "), "◐ ")
}

group("the machinery Claude Code injects into your turns") {
    // These arrive tagged, attributed to the user, and never appear on screen — so a transcript
    // is the first place anybody sees them, sitting under the word "you" above the one sentence
    // they actually typed.
    let noisy = """
    <task-notification>
    <task-id>be95b5m2o</task-id>
    <status>completed</status>
    </task-notification>
    確認要部署上正式站嗎？
    """
    expect("the block goes and the sentence stays",
           Transcript.withoutMachineBlocks(noisy).trimmingCharacters(in: .whitespacesAndNewlines),
           "確認要部署上正式站嗎？")
    expect("a turn that was nothing but machinery comes back empty",
           Transcript.withoutMachineBlocks("<system-reminder>be careful</system-reminder>")
               .trimmingCharacters(in: .whitespacesAndNewlines), "")
    expect("several in one turn",
           Transcript.withoutMachineBlocks("a<system-reminder>x</system-reminder>b<command-name>y</command-name>c"),
           "abc")
    // Opened and never closed is a truncated record, and everything after it is the block.
    expect("an unclosed block takes the rest with it",
           Transcript.withoutMachineBlocks("keep me<task-notification>cut from here"), "keep me")
    // The rule is a list of observed tags, not a rule about angle brackets.
    expect("somebody quoting xml typed that themselves",
           Transcript.withoutMachineBlocks("why does <thing>this</thing> fail?"),
           "why does <thing>this</thing> fail?")
    expect("ordinary text is untouched",
           Transcript.withoutMachineBlocks("deploy it"), "deploy it")
}

group("a slash command is the one piece of that machinery somebody typed") {
    // The three rows one `/model` leaves behind. `withoutMachineBlocks` empties all three, and an
    // empty entry is dropped — so a session whose whole history so far is one switch read in the
    // pane as a session that had said nothing at all, which is the one thing a pane must not do.
    let rows = [
        #"{"type":"user","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>Caveat: x</local-command-caveat>"}}"#,
        #"{"type":"user","timestamp":"2026-08-24T03:24:21.355Z","message":{"role":"user","content":"<command-name>/model</command-name>\n<command-message>model</command-message>\n<command-args>fable</command-args>"}}"#,
        #"{"type":"user","message":{"role":"user","content":"<local-command-stdout>Set model to \u001b[1mFable 5\u001b[22m and saved as your default for new sessions</local-command-stdout>"}}"#,
    ].joined(separator: "\n")
    let entries = Transcript.parse(rows)
    expect("the command and what it printed, and not the caveat between them", entries.count, 2)
    expect("what somebody typed is theirs", entries.first?.kind, Transcript.Entry.Kind.user)
    expect("said as one line", entries.first?.text, "/model fable")
    check("and stamped like any turn", entries.first?.time != nil)
    expect("what it printed is the machine answering", entries.last?.kind, Transcript.Entry.Kind.toolResult)
    expect("with the terminal's bold taken out", entries.last?.text,
           "Set model to Fable 5 and saved as your default for new sessions")

    expect("a command with nothing after it is just the command",
           Transcript.slashCommand(in: "<command-name>/recap</command-name><command-args></command-args>"), "/recap")
    expect("an argument comes with it",
           Transcript.slashCommand(in: "<command-name>/code-review</command-name><command-args>high</command-args>"), "/code-review high")
    expect("a name written without its slash is given one",
           Transcript.slashCommand(in: "<command-name>model</command-name>"), "/model")
    expect("the picker's own label for the row is not typed by anybody",
           Transcript.slashCommand(in: "<command-name>/model</command-name><command-message>model</command-message>"), "/model")
    check("ordinary prose is not a command", Transcript.slashCommand(in: "run /model for me") == nil)

    // A command whose expansion arrives in the same block keeps both: the line that was typed,
    // and whatever of the turn was not machinery.
    expect("the line typed leads what is left of the turn",
           Transcript.parse(#"{"type":"user","message":{"role":"user","content":"<command-name>/deploy</command-name>now please"}}"#).first?.text,
           "/deploy\nnow please")

    check("a command that printed nothing has no result",
          Transcript.commandOutput(in: "<local-command-stdout></local-command-stdout>") == nil)
    expect("stderr counts as printed too",
           Transcript.commandOutput(in: "<local-command-stderr>no such model</local-command-stderr>"), "no such model")
    check("and a turn with no command in it prints nothing",
          Transcript.commandOutput(in: "deploy it") == nil)
}

group("the remote server refuses the right cross-origin requests") {
    // DNS rebinding: the page keeps calling itself evil.com while the address behind the name
    // becomes 127.0.0.1, so origin checks stop working — and Host is the one thing that does not
    // change through any of it.
    func ok(_ host: String) -> Bool {
        RemoteServer.isAllowedHost(host, port: 7717, hostname: "clawd.example.com")
    }
    check("loopback with a port", ok("127.0.0.1:7717"))
    check("localhost", ok("localhost:7717"))
    check("ipv6 loopback in brackets", ok("[::1]:7717"))
    check("the configured hostname", ok("clawd.example.com"))
    check("and it is not case sensitive", ok("Clawd.Example.COM"))
    check("a quick tunnel, whose name cannot be known in advance",
          ok("denied-franchise-william-jade.trycloudflare.com"))
    check("a rebound host is refused", !ok("evil.com"))
    check("and one that merely ends in something familiar", !ok("evil-trycloudflare.com"))
    check("and one pretending to be the configured one", !ok("clawd.example.com.evil.com"))

    // The distinction that cost a bug: typing the address into a bar that was on another page is
    // cross-site, and is not an attack.
    func sub(_ h: [String: String]) -> Bool { RemoteServer.isCrossSiteSubresource(h) }
    check("a script fetching from another site is refused",
          sub(["sec-fetch-site": "cross-site", "sec-fetch-mode": "cors"]))
    check("with no mode at all, still refused",
          sub(["sec-fetch-site": "cross-site"]))
    check("somebody typing the address is not",
          !sub(["sec-fetch-site": "cross-site", "sec-fetch-mode": "navigate",
                "sec-fetch-dest": "document"]))
    check("the page asking for its own things is not",
          !sub(["sec-fetch-site": "same-origin", "sec-fetch-mode": "cors"]))
    check("and a script that is not a browser at all is left to the token check",
          !sub([:]))
}

group("a tool result is what a command printed, not how it wanted to be coloured") {
    // Observed on a phone: four hundred characters of escape codes with no spaces in them, which
    // pushed the page sideways and left a screen of black with one line of noise in the middle.
    let noisy = "\u{1B}[38;2;47;107;94m\u{1B}[48;2;47;107;94m▪\u{1B}[0msample-app — ~/code/sample-app"
    expect("the colours go and the sentence stays",
           Ansi.plain(noisy), "▪sample-app — ~/code/sample-app")
    expect("text with no escapes in it is untouched",
           Ansi.plain("total 95904"), "total 95904")
}
}
