import Foundation

// iTerm2 and tmux drawing the same windows.
//
// Under `tmux -CC` iTerm2 renders each tmux window as one of its own tabs while tmux keeps the
// pty. Every decision that follows from that is a pure function here, because the situation it is
// about cannot be set up in a test: reproducing it means opening a real control-mode session on
// somebody's desktop, and while one is open this app cannot close any session at all — which is
// the very defect these seams exist to remove.

func runTmuxCoexistenceTests() {

/// A tmux that writes down what it was asked and answers in the shapes the real one uses.
///
/// `/bin/echo` and `/bin/sleep` already stand in for tmux elsewhere in the suite, and they are
/// enough when the question is *what did this send* or *does a hung call get reaped*. Neither can
/// answer *how many processes did one reading cost*, which is the whole of what this file's
/// batching group is about — so this one records every invocation and replies to a batched script
/// the way tmux 3.6a replied to it on this Mac: a marker per pane, an empty id for a pane it
/// cannot find, and no capture at all under that one.
struct FakeTmux {
    let root: URL
    var binary: String { root.appendingPathComponent("tmux").path }
    /// One line per invocation, arguments joined by spaces, oldest first.
    var calls: [String] {
        let text = (try? String(contentsOf: root.appendingPathComponent("calls"),
                                encoding: .utf8)) ?? ""
        return text.split(separator: "\n").map(String.init)
    }
    func cleanup() { try? FileManager.default.removeItem(at: root) }
}

func makeFakeTmux(dead: [String] = []) -> FakeTmux {
    let root = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-fake-tmux-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    // `awk` rather than a second language: the script has to run on whatever CI hands it, and it
    // must not touch stdin unless it was given some — `run` only opens that pipe for the calls
    // that need it, so a `cat` on any other path would block on the suite's own stdin.
    let shell = """
    #!/bin/sh
    dir=$(dirname "$0")
    printf '%s\\n' "$*" >> "$dir/calls"
    dead="\(dead.joined(separator: " "))"
    case "$1" in
      source-file)
        cat > "$dir/script"
        awk -v dead=" $dead " '
          function alive(t) { return index(dead, " " t " ") == 0 }
          { target = ""; for (i = 1; i <= NF; i++) if ($i == "-t") target = $(i + 1) }
          /^display-message/ {
            printf "%cclawdline-pane%c%s%c\\n", 1, 1, (alive(target) ? target : ""), 1
          }
          /^capture-pane/ { if (alive(target)) { printf "screen of %s\\nsecond line\\n", target } }
        ' "$dir/script"
        ;;
      capture-pane)
        printf 'one pane at a time: %s\\n' "$*"
        ;;
      new-session|new-window)
        printf '%%7\\n'
        ;;
    esac
    exit 0
    """
    let binary = root.appendingPathComponent("tmux")
    try! Data(shell.utf8).write(to: binary)
    try! FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: binary.path)
    return FakeTmux(root: root)
}

func tmuxPane(_ id: String) -> TargetSession {
    TargetSession(backend: .tmux, id: id, name: "pane \(id)", tty: "/dev/ttys900",
                  windowIndex: 0, tabIndex: 0, assistant: .claude, cwd: "/tmp")
}

group("a tmux control-mode client is what makes a pty-less iTerm2 row explicable") {
    let sep = "\u{1}"
    // The flags string measured on this Mac against tmux 3.6a, from iTerm2's own `-CC` client,
    // and the window count that rides along in the same `list-clients` read:
    // `flags=attached,focused,control-mode,UTF-8 session=work windows=2`.
    let live = ["/dev/ttys006", "attached,focused,control-mode,wait-exit,pause-after=0,UTF-8",
                "work", "2"].joined(separator: sep)
    let clients = Tmux.parseControlModeClients(live)
    expect("the live control-mode client is found", clients.count, 1)
    expect("its gateway tty is kept", clients.first?.tty, "/dev/ttys006")
    expect("and the session it is attached to", clients.first?.session, "work")
    expect("and how many windows that session holds", clients.first?.sessionWindows, 2)

    let ordinary = ["/dev/ttys011", "attached,focused,UTF-8", "work", "2"].joined(separator: sep)
    expect("an ordinary attached client is not one", Tmux.parseControlModeClients(ordinary).count, 0)

    // `#{client_flags}` is an open vocabulary tmux adds to between releases. A flag that merely
    // contains these letters is not this flag, so the list is split and compared whole.
    let lookalike = ["/dev/ttys012", "attached,control-mode-pending,UTF-8", "s", "1"]
        .joined(separator: sep)
    expect("a flag that only contains the word is not the flag",
           Tmux.parseControlModeClients(lookalike).count, 0)

    let mixed = [live,
                 ordinary,
                 ["/dev/ttys013", "attached,control-mode,UTF-8", "other", "1"]
                     .joined(separator: sep)]
        .joined(separator: "\n")
    let both = Tmux.parseControlModeClients(mixed)
    expect("two control-mode clients on one server are both reported", both.count, 2)
    expect("and they are told apart by session",
           both.map(\.session).sorted(), ["other", "work"])

    expect("a malformed line is skipped", Tmux.parseControlModeClients("only-one-field").count, 0)
    expect("empty input yields nothing", Tmux.parseControlModeClients("").count, 0)
    // tmux 3.0 and older have no `#{client_session}`; the flags still decide, and the session
    // simply reads empty rather than the row vanishing.
    let noSession = ["/dev/ttys014", "attached,control-mode"].joined(separator: sep)
    expect("a client without a session field is still a client",
           Tmux.parseControlModeClients(noSession).count, 1)
    expect("its session is empty rather than invented",
           Tmux.parseControlModeClients(noSession).first?.session, "")
    // `?? nil` flattens the `Int??` that optional-chaining a missing field produces, so that
    // "no client" and "a client tmux would not size" cannot pass for each other here.
    func windowCount(_ line: String) -> Int? {
        Tmux.parseControlModeClients(line).first?.sessionWindows ?? nil
    }
    expect("a window count it did not answer is missing rather than zero",
           windowCount(noSession), nil)
    expect("and so is one it answered with something that is not a number",
           windowCount(["/dev/ttys015", "attached,control-mode", "work", ""]
                        .joined(separator: sep)), nil)
    expect("while the count it did answer is the number", windowCount(live), 2)

    // **A tmux this app cannot find has not said there is no control-mode client.** `binary` is
    // `Config.shared.tmuxPath` plus four fixed install paths, and a `-CC` session starts from
    // whatever tmux is on the person's PATH — so `nil` there is a question that was never asked.
    let noTmux = Tmux.controlModeObservationWithoutBinary()
    check("a tmux this app cannot find has not answered", !noTmux.isComplete)
    check("and the reason sends you to tmuxPath rather than to tmux",
          noTmux.error?.message.contains("tmuxPath") == true, noTmux.error?.message ?? "nil")
    expect("so it reaches the attribution as a source that could not be asked",
           ITerm.ptylessSecondSource(noTmux),
           .unavailable(noTmux.error?.message ?? ""))
    expect("while a reading that came back is tmux answering",
           ITerm.ptylessSecondSource(Tmux.ControlModeObservation(clients: [], error: nil)),
           .tmux(controlModeClients: []))

    // The socket Clawdline asks is an assumption, and an assumption nobody wrote down is one
    // whose victim has nothing to read. `tmux -L work -CC` still refuses every close.
    let interface = (try? String(contentsOfFile: "docs/interface.md", encoding: .utf8)) ?? ""
    check("the interface note says which tmux socket this asks",
          interface.contains("Clawdline only ever asks tmux's default socket"),
          "docs/interface.md read \(interface.count) characters")
}

group("an iTerm2 row with no pty is attributed rather than dropped") {
    func row(_ id: String, tty: String, ptyless: Bool = false) -> [String: Any] {
        var out: [String: Any] = ["id": id, "name": "tab " + id, "tty": tty,
                                  "profile": "Default", "win": 0, "tab": 0]
        if ptyless { out["ptyless"] = true }
        return out
    }
    func client(_ tty: String, session: String = "work",
                windows: Int? = 1) -> Tmux.ControlModeClient {
        Tmux.ControlModeClient(tty: tty, session: session, sessionWindows: windows)
    }

    // What one `-CC` session looked like on this Mac: ordinary rows, one drawn for tmux with an
    // empty tty — plus the gateway itself, which is an ordinary row with a real pty and must
    // keep behaving as one. It is also the row that proves the client is iTerm2's.
    let rows = [row("A", tty: "/dev/ttys006"),
                row("B", tty: "", ptyless: true),
                row("C", tty: "/dev/ttys011")]
    expect("the pty-less row is counted", ITerm.ptylessRowCount(rows), 1)
    expect("and is kept out of the iterm-backend rows",
           ITerm.attributableRows(rows).compactMap { $0["id"] as? String }, ["A", "C"])
    check("the tmux gateway keeps its own row",
          ITerm.attributableRows(rows).contains { $0["tty"] as? String == "/dev/ttys006" })
    // Zero is the answer the ordinary scan gets, and it is what keeps `snapshot` from asking
    // tmux anything at all on a Mac with no control-mode session open.
    expect("an ordinary scan counts none of them",
           ITerm.ptylessRowCount([row("A", tty: "/dev/ttys006")]), 0)
    expect("the ptys of one reading are what a client is matched against",
           ITerm.rowTTYs(rows).sorted(), ["ttys006", "ttys011"])

    // The marker is one string key shared across two languages. If either side renames it the
    // rows silently go back to being dropped, so the agreement is checked rather than assumed.
    let script = (try? String(contentsOfFile: "Resources/iterm.js", encoding: .utf8)) ?? ""
    check("iterm.js writes the marker key Swift reads here",
          script.contains("row.ptyless = true"),
          "Resources/iterm.js read \(script.count) characters")
    expect("and it writes it on both list paths",
           script.components(separatedBy: "row.ptyless = true").count - 1, 2)

    let none = ITerm.ptylessRowAttribution(rows: [row("A", tty: "/dev/ttys006")],
                                           secondSource: .tmux(controlModeClients: []))
    check("no pty-less rows lowers nothing", !none.lowersConfidence)
    check("and says nothing", none.error == nil)

    let drawn = ITerm.ptylessRowAttribution(
        rows: rows, secondSource: .tmux(controlModeClients: [client("/dev/ttys006")]))
    expect("a client speaking over this reading's own gateway pty explains its row",
           drawn.attributedToTmux, 1)
    expect("and leaves none unexplained", drawn.unexplained, 0)
    check("so one -CC session no longer makes the inventory incomplete", !drawn.lowersConfidence)
    check("and there is nothing to report", drawn.error == nil)

    // **The ceiling.** A control-mode client draws one tab per window of its session, and
    // `#{session_windows}` says how many that is. Without it, one client anywhere excused any
    // number of pty-less rows — in the only situation this code ever runs in.
    let twoDrawn = [row("A", tty: "/dev/ttys006"),
                    row("B", tty: "", ptyless: true),
                    row("C", tty: "", ptyless: true)]
    let sized = ITerm.ptylessRowAttribution(
        rows: twoDrawn, secondSource: .tmux(controlModeClients: [client("/dev/ttys006",
                                                                       windows: 2)]))
    expect("a client drawing two windows explains two rows", sized.attributedToTmux, 2)
    check("and the inventory keeps its confidence", !sized.lowersConfidence)

    let overflow = ITerm.ptylessRowAttribution(
        rows: twoDrawn + [row("D", tty: "", ptyless: true)],
        secondSource: .tmux(controlModeClients: [client("/dev/ttys006", windows: 2)]))
    expect("a third pty-less row is more than that client draws", overflow.unexplained, 1)
    expect("the two it does draw are still attributed", overflow.attributedToTmux, 2)
    check("so one control-mode client no longer excuses any number of rows",
          overflow.lowersConfidence)
    check("and the reason names the ceiling it went past",
          overflow.error?.contains("2 windows") == true, overflow.error ?? "nil")

    expect("two clients watching one session are watching the same windows",
           ITerm.drawnWindowCeiling([client("/dev/ttys006", session: "work", windows: 2),
                                     client("/dev/ttys011", session: "work", windows: 2)]), 2)
    expect("two clients on different sessions add up",
           ITerm.drawnWindowCeiling([client("/dev/ttys006", session: "work", windows: 2),
                                     client("/dev/ttys011", session: "reading", windows: 3)]), 5)
    expect("a client tmux would not size is worth one window rather than all of them",
           ITerm.drawnWindowCeiling([client("/dev/ttys006", windows: nil)]), 1)
    expect("and two unsized clients tmux named no session for stay apart",
           ITerm.drawnWindowCeiling([client("/dev/ttys006", session: "", windows: nil),
                                     client("/dev/ttys011", session: "", windows: nil)]), 2)

    // **The identity.** `control-mode` is a protocol and not a name: `tmux -C attach` from a
    // script carries the same flag, measured on this Mac in one line. What is iTerm2's is the
    // gateway pty, and it is in the same reading being attributed.
    let stranger = ITerm.ptylessRowAttribution(
        rows: rows, secondSource: .tmux(controlModeClients: [client("/dev/ttys099", windows: 4)]))
    expect("a control-mode client on a pty this reading never saw explains nothing",
           stranger.attributedToTmux, 0)
    check("so the row it does not account for is unexplained", stranger.lowersConfidence)
    check("and the reason says the client was not one of iTerm2's",
          stranger.error?.contains("speaking to a pty iTerm2 named") == true,
          stranger.error ?? "nil")
    expect("an empty client tty matches nothing rather than everything",
           ITerm.controlModeClientsDrawnByITerm2([client("", windows: 2)],
                                                 rowTTYs: ITerm.rowTTYs(rows)).count, 0)
    expect("and a bare tty is the same pty as a /dev one",
           ITerm.controlModeClientsDrawnByITerm2([client("ttys006")],
                                                 rowTTYs: ITerm.rowTTYs(rows)).count, 1)

    // The other half of a second source: when it disagrees, the row is exactly the unreadable
    // one the original guard was written for.
    let anomaly = ITerm.ptylessRowAttribution(rows: rows,
                                              secondSource: .tmux(controlModeClients: []))
    expect("tmux disagreeing attributes nothing", anomaly.attributedToTmux, 0)
    expect("the row stays unexplained", anomaly.unexplained, 1)
    check("and the inventory loses confidence", anomaly.lowersConfidence)
    check("the reason names the missing agreement",
          anomaly.error?.contains("no control-mode client") == true, anomaly.error ?? "nil")
    check("one row is not described in the plural",
          anomaly.error?.contains("1 iTerm2 session had") == true, anomaly.error ?? "nil")

    // A source that could not answer has not agreed — and is not the same fact as a source that
    // answered no.
    let quiet = ITerm.ptylessRowAttribution(
        rows: rows, secondSource: .unavailable("tmux did not finish within 5 seconds"))
    expect("a tmux that never answered attributes nothing", quiet.attributedToTmux, 0)
    check("and still lowers confidence", quiet.lowersConfidence)
    check("but says so differently from a tmux that said no",
          quiet.error?.contains("could not be asked") == true, quiet.error ?? "nil")
    check("carrying the reason it could not be asked",
          quiet.error?.contains("5 seconds") == true, quiet.error ?? "nil")
    check("and the two answers are not the same string",
          quiet.error != ITerm.ptylessRowAttribution(
            rows: rows, secondSource: .tmux(controlModeClients: [])).error)
}

group("revealing a tmux pane brings iTerm2 forward only when tmux says it drew it") {
    let drawing = Tmux.ControlModeClient(tty: "/dev/ttys006", session: "work", sessionWindows: 1)
    let elsewhere = Tmux.ControlModeClient(tty: "/dev/ttys020", session: "reading",
                                           sessionWindows: 1)

    check("a pane in the session a control-mode client is attached to activates iTerm2",
          Tmux.shouldActivateITerm(paneSession: "work", controlModeClients: [drawing]))
    check("a pane in another session does not, even while -CC is running elsewhere",
          !Tmux.shouldActivateITerm(paneSession: "ghostty", controlModeClients: [drawing]))
    check("with several clients each session still answers for itself",
          Tmux.shouldActivateITerm(paneSession: "reading",
                                   controlModeClients: [drawing, elsewhere]))
    check("no control-mode client at all is a no",
          !Tmux.shouldActivateITerm(paneSession: "work", controlModeClients: []))
    // `display-message` failing leaves the pane's session unknown, and an unknown answer must not
    // take somebody's keyboard away from the box they were typing into.
    check("an unknown pane session is a no",
          !Tmux.shouldActivateITerm(paneSession: nil, controlModeClients: [drawing]))
    check("and so is an empty one",
          !Tmux.shouldActivateITerm(paneSession: "", controlModeClients: [drawing]))
    // A client tmux reports without a session — the old-tmux shape above — must not match a pane
    // whose session simply could not be read.
    check("an empty session on the client side matches nothing either",
          !Tmux.shouldActivateITerm(
            paneSession: nil,
            controlModeClients: [Tmux.ControlModeClient(tty: "/dev/ttys014", session: "",
                                                        sessionWindows: nil)]))

    // **Being in the right session is not being iTerm2.** The production rule filters the client
    // list against iTerm2's own ptys first, because a `tmux -C attach` in a script sits in the
    // same session with the same flag, and bringing iTerm2 forward for it takes the keyboard
    // away from whatever the person was typing into.
    let itermPTYs: Set<String> = ["ttys006"]
    check("a control-mode client speaking over an iTerm2 pty may bring iTerm2 forward",
          Tmux.shouldActivateITerm(
            paneSession: "work",
            controlModeClients: ITerm.controlModeClientsDrawnByITerm2([drawing],
                                                                      rowTTYs: itermPTYs)))
    check("a control-mode client that is not iTerm2's may not, however right its session is",
          !Tmux.shouldActivateITerm(
            paneSession: "work",
            controlModeClients: ITerm.controlModeClientsDrawnByITerm2(
                [Tmux.ControlModeClient(tty: "/dev/ttys077", session: "work", sessionWindows: 1)],
                rowTTYs: itermPTYs)))
    check("and an iTerm2 that would not list its sessions proves nothing",
          !Tmux.shouldActivateITerm(
            paneSession: "work",
            controlModeClients: ITerm.controlModeClientsDrawnByITerm2([drawing], rowTTYs: [])))

    // **The tail leaves the caller's thread.** Three of `Targets.reveal`'s four call sites are on
    // the main thread — the island's menu and its click, and `revealTarget()` — and the tail is
    // three round trips with a deadline each. `/bin/echo` stands in for tmux so `select-pane` and
    // `select-window` answer without a server; what is being proved is where the rest goes.
    defer {
        Tmux.binaryForTesting = nil
        Tmux.revealActivationDispatchForTesting = nil
    }
    Tmux.binaryForTesting = "/bin/echo"
    var handedOn: [() -> Void] = []
    Tmux.revealActivationDispatchForTesting = { handedOn.append($0) }
    let failure = Tmux.reveal("%1")
    check("selecting the pane is still answered for on the spot", failure == nil)
    expect("and the activation tail is handed to another thread", handedOn.count, 1)
    handedOn.removeAll()
    _ = Tmux.reveal("%1", activate: false)
    expect("the prompt bar walking its list hands on nothing at all", handedOn.count, 0)
}

group("one tmux subprocess answers for every pane in a reading") {
    let marker = Tmux.batchedCaptureMarker

    // **The script.** One marker and one capture per pane, in that order, so the answers can be
    // told apart afterwards even though `capture-pane` has no plural.
    let script = Tmux.batchedCaptureScript(["%1", "%2"], scrollback: 0)
    let lines = script.split(separator: "\n").map(String.init)
    expect("one marker and one capture per pane", lines.count, 4)
    check("each marker is aimed at its own pane", lines[0].hasPrefix("display-message -p -t %1 "),
          lines[0])
    check("and each capture at the same one", lines[1].hasSuffix("-t %1"), lines[1])
    check("the capture starts at the top of the screen", lines[1].contains("-S -0"), lines[1])
    check("and keeps the colours, which is what Activity strips again",
          lines[1].contains("-e"), lines[1])
    check("the id is asked of tmux rather than written out", lines[0].contains("#{pane_id}"),
          lines[0])

    // **The trap, kept as a test because it costs an hour to rediscover.** `display-message` runs
    // its argument through `strftime`, so a marker built out of `%0` arrives as `0` — measured on
    // tmux 3.6a. Nothing inside a marker format may be a `%`.
    for line in lines where line.hasPrefix("display-message") {
        let format = line.drop(while: { $0 != "\"" })
        check("no marker format carries a % for strftime to eat",
              !format.contains(Character("%")), String(format))
    }

    expect("a word that is not a pane id never reaches the script",
           Tmux.batchedCaptureScript(["%1", "; kill-server", "%2x", "%", "", "kill-server"],
                                     scrollback: 0).split(separator: "\n").count, 2)
    expect("and nothing to ask about is no script at all",
           Tmux.batchedCaptureScript([], scrollback: 0), "")
    check("a pane id is what tmux hands out", Tmux.isPaneID("%12"))
    check("and a bare percent is not", !Tmux.isPaneID("%"))
    check("nor a number without one", !Tmux.isPaneID("12"))
    check("nor an id with anything else in it", !Tmux.isPaneID("%1a"))

    // **The parsing**, which is where the bugs live, exercised with no tmux server running.
    let answered = "\(marker)%1\(marker)\nfirst\nsecond\n\(marker)%2\(marker)\nonly\n"
    let screens = Tmux.parseBatchedCapture(answered)
    expect("every pane that answered is keyed by its own id", screens.count, 2)
    expect("and keeps its own screen", screens["%1"], "first\nsecond\n")
    expect("including the last, which has no marker after it to close it", screens["%2"], "only\n")

    // What a pane that has gone away actually leaves behind, measured on tmux 3.6a: the marker
    // resolves to an empty id and the capture prints nothing, while `source-file` carries on
    // through the rest of the script. That is the whole reason this is not a `;`-separated
    // argument list, which stops dead at the first error and loses every pane after it.
    let withDead = "\(marker)%1\(marker)\nalive\n\(marker)\(marker)\n\(marker)%3\(marker)\nlater\n"
    let salvaged = Tmux.parseBatchedCapture(withDead)
    expect("a pane that would not answer costs one key, not the reading", salvaged.count, 2)
    expect("the pane after it still answers", salvaged["%3"], "later\n")
    check("and the one with no id is not a session called nothing", salvaged[""] == nil)

    // A live pane prints its whole height, blank rows included, so a marker with nothing under it
    // is a failure rather than a blank screen — and the two must not arrive as the same answer.
    check("a marker with no screen under it is no answer",
          Tmux.parseBatchedCapture("\(marker)%4\(marker)\n")["%4"] == nil)
    expect("a line that merely looks like a marker is content",
           Tmux.parseBatchedCapture("\(marker)%5\(marker)\nclawdline-pane %6\n")["%5"],
           "clawdline-pane %6\n")
    expect("the first answer for an id wins",
           Tmux.parseBatchedCapture("\(marker)%7\(marker)\nreal\n\(marker)%7\(marker)\nlate\n")["%7"],
           "real\n")
    expect("empty output is no answers", Tmux.parseBatchedCapture("").count, 0)

    // **The point of all of it.** A reading is one round trip to every terminal, it runs every
    // 1.2 s while the panel is open, and one in progress suppresses the next — so ten panes used
    // to be ten process spawns per beat. Measured on tmux 3.6a with ten real panes, 2026-09-02:
    // one process, median 3.45 ms, against ten processes and 32.01 ms.
    let fake = makeFakeTmux(dead: ["%4"])
    defer {
        fake.cleanup()
        Tmux.binaryForTesting = nil
    }
    Tmux.binaryForTesting = fake.binary
    let reading = Targets.reading(of: (1...10).map { tmuxPane("%\($0)") })
    expect("ten tmux panes cost one tmux invocation", fake.calls.count, 1)
    check("and that invocation is the batched one",
          fake.calls.first?.hasPrefix("source-file -") == true, fake.calls.first ?? "nothing")
    expect("every pane asked about gets a state", reading.states.count, 10)
    expect("the pane that would not answer is unknown rather than idle",
           reading.states["%4"], .unknown)
    expect("its neighbour before it was read", reading.states["%3"], .idle)
    expect("and its neighbour after it, which the old shape would have lost",
           reading.states["%5"], .idle)
}

group("Clawdline starts a tmux server rather than telling a phone to go and run one") {
    let fake = makeFakeTmux()
    defer {
        fake.cleanup()
        Tmux.binaryForTesting = nil
    }
    Tmux.binaryForTesting = fake.binary

    let made = Tmux.newSessionResult(cwd: "/tmp/x", command: "env -u ANTHROPIC_API_KEY claude")
    expect("the pane the new server made comes back", try? made.get(), "%7")
    let started = fake.calls
    expect("a server, a line, and the Return that runs it", started.count, 3)
    check("the server is started detached and under a name somebody can attach to",
          started[0].hasPrefix("new-session -d -s clawdline "), started[0])
    check("in the directory that was asked for", started[0].hasSuffix("-c /tmp/x"), started[0])

    // **Not `new-session -d '<assistant>'`, and this is the measurement that decided it.** A pane
    // started that way is run by `sh -c` with the *server's* environment, and a server this app
    // starts inherits the app's — a Finder-launched app has no login shell behind it and so no
    // PATH worth reading. Measured on this Mac, 2026-09-02, with a Finder-shaped environment:
    // that spelling draws `zsh:1: command not found: claude`, and so does every later
    // `new-window 'claude …'` on the same server. A pane created with no command gets an
    // interactive login shell, which reads the file the person's PATH is actually set in.
    check("the assistant is not handed to tmux as the pane's command",
          !started[0].contains("claude"), started[0])
    check("it is typed at the login shell instead",
          started[1] == "send-keys -t %7 -l env -u ANTHROPIC_API_KEY claude", started[1])
    check("and Return is its own keypress", started[2] == "send-keys -t %7 Enter", started[2])

    // The ordinary window path takes the same shape for the same reason: the second session
    // started on a server Clawdline made would otherwise be the one that says command not found.
    let window = Tmux.newWindowResult(cwd: "", command: "codex")
    expect("an ordinary new window answers with its pane too", try? window.get(), "%7")
    let opened = Array(fake.calls.dropFirst(3))
    expect("and costs the same three steps", opened.count, 3)
    check("no working directory is passed when there is none",
          opened[0] == "new-window -P -F #{pane_id}", opened[0])
    check("the assistant is not the window's command either",
          !opened[0].contains("codex"), opened[0])
    check("it is typed too", opened[1] == "send-keys -t %7 -l codex", opened[1])

    check("the sentence a person needs names the session they would attach to",
          Tmux.attachCommand.contains(Tmux.startedSessionName), Tmux.attachCommand)
    // The socket that sentence is about is the default one, which is the assumption this app
    // makes everywhere and the one `docs/interface.md` is where it is written down.
    let interface = (try? String(contentsOfFile: "docs/interface.md", encoding: .utf8)) ?? ""
    check("and the interface note says what a detached session looks like and how to reach it",
          interface.contains(Tmux.attachCommand),
          "docs/interface.md read \(interface.count) characters")
}

group("a screen read to decide something is the screen, not its history") {
    defer { Tmux.binaryForTesting = nil }
    // `/bin/echo` answers with the arguments it was given, so what a capture *asked for* is
    // readable without a tmux server — which is the only thing in question here.
    Tmux.binaryForTesting = "/bin/echo"
    let pane = tmuxPane("%9")

    let decided = Targets.capture(pane) ?? ""
    check("what a decision reads starts at the top of the screen",
          decided.contains("-S -0"), decided)
    check("and never in the scrollback", !decided.contains("-S -200"), decided)
    expect("which is the same question `visibleScreen` already asked",
           Targets.visibleScreen(of: pane), decided)

    // The one caller that is showing a person a terminal rather than deciding from it keeps the
    // history — and is now the only one paying for it.
    let shown = Targets.screenWithHistory(of: pane) ?? ""
    check("what a person is shown carries the scrollback", shown.contains("-S -200"), shown)
    check("and the two are not the same reading", shown != decided)
    check("a caller may say how much of it it wants",
          (Targets.screenWithHistory(of: pane, lines: 40) ?? "").contains("-S -40"))

    // **Why it matters, in the one place a tail window does not save it.** `SessionState.menu`
    // reads the last thirty non-empty lines and `Activity.parse` the last twenty-five, so history
    // is inert for them. `briefingInputReady` looks for a bare composer *anywhere* in the text it
    // is handed, so a `❯` that scrolled away is a session that says it is ready to be briefed.
    let stale = (0..<60).map { "output line \($0)" }.joined(separator: "\n")
    check("a composer left in the scrollback would say a starting session is ready",
          Orchestrator.briefingInputReady("❯\n" + stale, assistant: .claude))
    check("while the screen alone says what is true",
          !Orchestrator.briefingInputReady(stale, assistant: .claude))
}

}
