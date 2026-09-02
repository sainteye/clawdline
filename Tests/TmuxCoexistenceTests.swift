import Foundation

// iTerm2 and tmux drawing the same windows.
//
// Under `tmux -CC` iTerm2 renders each tmux window as one of its own tabs while tmux keeps the
// pty. Every decision that follows from that is a pure function here, because the situation it is
// about cannot be set up in a test: reproducing it means opening a real control-mode session on
// somebody's desktop, and while one is open this app cannot close any session at all — which is
// the very defect these seams exist to remove.

func runTmuxCoexistenceTests() {

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

}
