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

/// The four shapes a tmux that is *not* answering normally takes, because every one of them is a
/// case this file's production code decides something from and none of them could be reached with
/// a fake that always exits 0.
///
/// - `silent` is a tmux too old for `source-file -` (3.3): it swallows the script and prints
///   nothing, which is the only signal the per-pane fallback has to go on.
/// - `hanging` never finishes, so `run` reaps it and returns a `.timeout` — the failure that used
///   to be read as a version floor.
/// - `refusing` is tmux's own words on stderr with a non-zero exit, and `only` narrows that to one
///   subcommand, which is how a pane that will not be typed into is set up.
func makeFakeTmux(dead: [String] = [], silent: Bool = false, hanging: Bool = false,
                  refusing: String? = nil, only refusedCommand: String? = nil) -> FakeTmux {
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
    silent="\(silent ? "1" : "")"
    hanging="\(hanging ? "1" : "")"
    refusal="\(refusing ?? "")"
    refused_command="\(refusedCommand ?? "")"
    if [ -n "$refusal" ] && { [ -z "$refused_command" ] || [ "$1" = "$refused_command" ]; }; then
      printf '%s\\n' "$refusal" >&2
      exit 1
    fi
    if [ -n "$hanging" ]; then
      sleep 5
      exit 0
    fi
    case "$1" in
      source-file)
        cat > "$dir/script"
        if [ -n "$silent" ]; then exit 0; fi
        awk -v dead=" $dead " '
          function alive(t) { return index(dead, " " t " ") == 0 }
          { target = ""; for (i = 1; i <= NF; i++) if ($i == "-t") target = $(i + 1) }
          /^display-message/ { printf "%cclawdline-pane%c%s%cclawdline-pane%c\\n", 1, 1, (alive(target) ? target : ""), 1, 1 }
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

group("a tmux -CC reveal names the tab, because iTerm2 does not follow tmux's selection") {
    // **The measurement this group exists for cannot be made in here.** Whether iTerm2 moves its
    // selected tab when tmux's active window changes is a fact about two running applications on
    // somebody's desktop, and the answer — it does not, on tmux 3.6a, not after two minutes and
    // not with iTerm2 already frontmost, while tmux did send `%session-window-changed` for every
    // one of those calls — was taken by hand and written down in `docs/interface.md`. What can be
    // proved here is everything that follows from it: the pane id arrives in the spelling
    // `iterm.js` compares against, the tab is asked for before the application, and a tab that
    // cannot be found still leaves somebody with their terminal in front of them.

    // The conversion, which is one line of production code and three ways to be silently wrong.
    // `iterm.js` compares with `!==` against a session variable, so every one of these mistakes
    // arrives as *iTerm2 is drawing no tab for that pane* — indistinguishable from the true
    // absence, and answered by raising the application blind, which is the old broken behaviour
    // wearing the new code's clothes.
    expect("a pane id is what iTerm2 spells without the marker", ITerm.tmuxMirrorPaneNumber("%65"),
           "65")
    expect("including the first pane a server ever made", ITerm.tmuxMirrorPaneNumber("%0"), "0")
    expect("a window id is not a pane id, however alike the numbers look",
           ITerm.tmuxMirrorPaneNumber("@65"), nil)
    expect("a bare number is not one either", ITerm.tmuxMirrorPaneNumber("65"), nil)
    expect("nor a marker with nothing after it", ITerm.tmuxMirrorPaneNumber("%"), nil)
    expect("nor anything that is not all digits", ITerm.tmuxMirrorPaneNumber("%6a"), nil)
    expect("and an empty id is refused rather than sent as an empty argument",
           ITerm.tmuxMirrorPaneNumber(""), nil)

    // **The order, and the fallback.** Selecting the tab already brings iTerm2 with it, so a
    // second `activate` behind a successful select is an Apple Event spent on something done —
    // and, more to the point, raising the application is what the tail used to do *instead* of
    // selecting. If this ever goes back to calling both, or to calling only the second, the tab
    // stops being named and the reveal lands wherever the person was last looking.
    var revealed: [String] = []
    var raised = 0
    let landed = Tmux.bringITerm2ToPane("%65",
                                        revealMirrorTab: { revealed.append($0); return nil },
                                        bringForward: { raised += 1; return nil })
    check("a pane whose tab was found is landed on", landed)
    expect("the tab is asked for by pane id", revealed, ["%65"])
    expect("and the application is not raised a second time behind it", raised, 0)

    revealed.removeAll()
    raised = 0
    let fellBack = Tmux.bringITerm2ToPane(
        "%65",
        revealMirrorTab: {
            revealed.append($0)
            return TerminalFailure(kind: .io, message: "gone")
        },
        bringForward: { raised += 1; return nil })
    check("a tab that cannot be found is not reported as landed on", !fellBack)
    expect("it was still asked for first", revealed, ["%65"])
    expect("and the window comes forward anyway rather than the press doing nothing", raised, 1)

    // The same cross-language agreement the `ptyless` marker gets above, for the three strings
    // this path is built on. Swift sends the command word; the two variable names and the role
    // value live only in `iterm.js`, and renaming one there breaks the mapping *silently* —
    // `revealtmux` would answer "no such tab" forever and every reveal would quietly fall back to
    // raising the application, which is the bug this group is about, restored and invisible.
    let script = (try? String(contentsOfFile: "Resources/iterm.js", encoding: .utf8)) ?? ""
    check("iterm.js answers the command Swift sends",
          script.contains("cmd === \"revealtmux\""),
          "Resources/iterm.js read \(script.count) characters")
    // **Both of these are matched inside the call and not as a bare word**, because the block of
    // prose above `revealtmux` names all three strings too — a first cut of this check passed
    // happily while the code below it asked for `session.tmuxPaneNumber`, which is the rename it
    // was written to catch. A guard that cannot go red for the thing it guards is worse than none.
    check("and matches on the variable that carries the tmux pane",
          script.contains("variableOf(s, \"session.tmuxWindowPane\")"))
    check("and refuses any row iTerm2 does not call a mirrored tmux window",
          script.contains("variableOf(s, \"session.tmuxRole\") !== \"client\""))
}

group("a tmux failure says whether there is no server, or only that nobody could ask") {
    // Every message here is tmux 3.6a's own, taken on this Mac on 2026-09-02 from three private
    // `-L` sockets — one whose server had been killed, one that had never existed, and one that
    // existed and would not open. `list-sessions`, `list-panes -a`, `list-clients` and
    // `source-file -` were each asked, and all four answer with the same three sentences.
    func io(_ message: String) -> TerminalFailure {
        TerminalFailure(kind: .io, message: message)
    }
    let socket = "/private/tmp/tmux-501/clawdline-probe"
    let killed = "no server running on \(socket)"
    let neverUsed = "error connecting to \(socket) (No such file or directory)"
    let shut = "error connecting to \(socket) (Permission denied)"

    expect("a server that was killed leaves its socket behind and says so",
           Tmux.serverAnswer(io(killed)), .noServer)
    expect("a socket that was never made says something else entirely and means the same",
           Tmux.serverAnswer(io(neverUsed)), .noServer)
    expect("the spelling tmux uses while a server is starting is an answer too",
           Tmux.serverAnswer(io("failed to connect to server")), .noServer)
    expect("a socket that exists and will not open proves nothing",
           Tmux.serverAnswer(io(shut)), .unreachable)
    expect("and a tmux that never finished proves nothing either",
           Tmux.serverAnswer(TerminalFailure(kind: .timeout,
                                             message: "tmux did not finish within 15 seconds")),
           .unreachable)
    expect("a sentence about the command is not a sentence about the server",
           Tmux.serverAnswer(io("can't find pane: %99")), .reached)
    expect("nor is one nobody here has seen before",
           Tmux.serverAnswer(io("unknown command: sorcery")), .reached)
    expect("and a command that did not fail at all is a reading",
           Tmux.serverAnswer(nil), .reached)

    // **The two that have to be told apart share every word but one.** That is the whole reason
    // this is not a third substring: `error connecting to <path> (…)` is one sentence with
    // `strerror(errno)` inside it, and only the errno says whether anything is there.
    let sentence = "error connecting to \(socket) ("
    check("the socket that is missing and the socket that will not open say the same sentence",
          neverUsed.hasPrefix(sentence) && shut.hasPrefix(sentence), "\(neverUsed) / \(shut)")
    check("so the errno inside it is what decides, and it decides differently",
          Tmux.serverAnswer(io(neverUsed)) != Tmux.serverAnswer(io(shut)))
    check("read in the case tmux writes it, which is the C library's",
          neverUsed.contains("(No such file or directory)"), neverUsed)

    // **What it is for.** `StartPoints.tmuxReach()` starts a detached server only for an
    // inventory that is *complete* and empty, so the Mac this feature was written for — tmux
    // installed, never run, no socket file at all — has to reach it as complete. It did not:
    // the one message it gives is the one message this did not know.
    let fresh = makeFakeTmux(refusing: neverUsed)
    defer {
        fresh.cleanup()
        Tmux.binaryForTesting = nil
    }
    Tmux.binaryForTesting = fresh.binary
    let freshPanes = Tmux.paneObservation()
    check("a Mac where tmux has never run has a complete pane inventory", freshPanes.isComplete)
    check("holding nothing", freshPanes.sessions.isEmpty)
    check("and a complete control-mode answer beside it",
          Tmux.controlModeObservation().isComplete)

    let unreadable = makeFakeTmux(refusing: shut)
    defer { unreadable.cleanup() }
    Tmux.binaryForTesting = unreadable.binary
    let shutPanes = Tmux.paneObservation()
    check("a socket that will not open leaves the inventory incomplete", !shutPanes.isComplete)
    check("and keeps tmux's own words for why",
          shutPanes.error?.message.contains("Permission denied") == true,
          shutPanes.error?.message ?? "nothing")
    check("with the control-mode answer holding the same doubt",
          !Tmux.controlModeObservation().isComplete)
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

    // **A deadline is not a version.** The fallback below is entered on the *absence* of the
    // marker, and a wedged tmux is absent in exactly the same way — so the reading this group
    // exists to make cheap would have gone from one bounded call to one per pane, on the path
    // where a reading in progress suppresses the next one rather than queueing it.
    let wedged = makeFakeTmux(hanging: true)
    defer { wedged.cleanup() }
    Tmux.binaryForTesting = wedged.binary
    // **The deadline has to outlast starting a shell, not just outlast the hang.** The fake
    // records the call on its third line and only then sleeps, so a deadline that fires before
    // `/bin/sh` gets that far kills it with nothing written — and the check below then reads 0
    // and reports the fallback defect, which did not happen. That is a false red, and a check
    // that can go falsely red costs exactly what one that cannot go red costs: it makes "it went
    // red" mean nothing. Reported from a full-suite run on 2026-09-03 while the machine was
    // compiling continuously; three focused runs of the same group were green.
    //
    // Measured on this Mac the same day, spawn to recorded line: 21-29 ms idle, 27-38 ms under
    // eight busy cores. Two seconds is fifty times the worst of those and still well inside the
    // fake's five-second sleep, so the deadline is what fires and the timeout path is still what
    // is being exercised. The suite pays that two seconds once.
    Tmux.subprocessTimeoutForTesting = 2.0
    let timedOut = Tmux.capture(panes: (1...10).map { "%\($0)" }, scrollback: 0)
    Tmux.subprocessTimeoutForTesting = nil
    expect("a reading that ran out of time answers for no pane", timedOut.count, 0)
    // Said as three cases rather than one, so the next person to see this red is told which of
    // them happened instead of going to look for a product defect that is not there.
    check("and costs one deadline rather than one per pane", wedged.calls.count == 1,
          wedged.calls.count == 0
            ? "the fake tmux recorded nothing: the deadline fired before the shell could start, "
              + "so this says nothing about the fallback — the fixture was starved, not the code"
            : "\(wedged.calls.count) invocations: the batch fell back to reading pane by pane")

    // The same for a socket nobody could reach. Asking again pane by pane cannot answer what the
    // batch could not, and on the Mac where tmux has never run it is one failure per pane.
    let noSocket = makeFakeTmux(
        refusing: "error connecting to /private/tmp/tmux-501/default (No such file or directory)")
    defer { noSocket.cleanup() }
    Tmux.binaryForTesting = noSocket.binary
    expect("a socket that was never made answers for no pane",
           Tmux.capture(panes: ["%1", "%2"], scrollback: 0).count, 0)
    expect("and is asked exactly once", noSocket.calls.count, 1)

    // **The branch that exists so an old tmux does not go blind, run at last.** `source-file -`
    // wants tmux 3.3; an older one takes the script and prints nothing at all, and pane by pane
    // is what keeps the whole backend readable. Nothing had ever executed it.
    let old = makeFakeTmux(silent: true)
    defer { old.cleanup() }
    Tmux.binaryForTesting = old.binary
    let oneAtATime = Tmux.capture(panes: ["%1", "%2"], scrollback: 0)
    expect("a tmux too old for the script costs one try and then one call per pane",
           old.calls.count, 3)
    expect("every pane still answers", oneAtATime.count, 2)
    expect("out of the per-pane path rather than the batched one", oneAtATime["%1"],
           "one pane at a time: capture-pane -p -e -J -S -0 -t %1\n")
    check("which asks for the screen this reading was told to ask for",
          old.calls.last == "capture-pane -p -e -J -S -0 -t %2", old.calls.last ?? "nothing")
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
    let calls = fake.calls
    // Read by position rather than subscripted: a run where the step is missing is the run this
    // group exists to fail, and it has to report that rather than trap on the way there.
    func started(_ index: Int) -> String { index < calls.count ? calls[index] : "" }
    expect("a server, a line, and the Return that runs it", calls.count, 3)
    check("the server is started detached and under a name somebody can attach to",
          started(0).hasPrefix("new-session -d -s clawdline "), started(0))
    check("in the directory that was asked for", started(0).hasSuffix("-c /tmp/x"), started(0))

    // **Not `new-session -d '<assistant>'`, and this is the measurement that decided it.** A pane
    // started that way is run by `sh -c` with the *server's* environment, and a server this app
    // starts inherits the app's — a Finder-launched app has no login shell behind it and so no
    // PATH worth reading. Measured on this Mac, 2026-09-02, with a Finder-shaped environment:
    // that spelling draws `zsh:1: command not found: claude`, and so does every later
    // `new-window 'claude …'` on the same server. A pane created with no command gets an
    // interactive login shell, which reads the file the person's PATH is actually set in.
    check("the assistant is not handed to tmux as the pane's command",
          !started(0).contains("claude"), started(0))
    check("it is typed at the login shell instead",
          started(1) == "send-keys -t %7 -l env -u ANTHROPIC_API_KEY claude", started(1))
    check("and Return is its own keypress", started(2) == "send-keys -t %7 Enter", started(2))

    // The ordinary window path takes the same shape for the same reason: the second session
    // started on a server Clawdline made would otherwise be the one that says command not found.
    let window = Tmux.newWindowResult(cwd: "", command: "codex")
    expect("an ordinary new window answers with its pane too", try? window.get(), "%7")
    let after = Array(fake.calls.dropFirst(calls.count))
    func opened(_ index: Int) -> String { index < after.count ? after[index] : "" }
    expect("and costs the same three steps", after.count, 3)
    check("no working directory is passed when there is none",
          opened(0) == "new-window -P -F #{pane_id}", opened(0))
    check("the assistant is not the window's command either",
          !opened(0).contains("codex"), opened(0))
    check("it is typed too", opened(1) == "send-keys -t %7 -l codex", opened(1))

    check("the sentence a person needs names the session they would attach to",
          Tmux.attachCommand.contains(Tmux.startedSessionName), Tmux.attachCommand)
    // The socket that sentence is about is the default one, which is the assumption this app
    // makes everywhere and the one `docs/interface.md` is where it is written down.
    let interface = (try? String(contentsOfFile: "docs/interface.md", encoding: .utf8)) ?? ""
    check("and the interface note says what a detached session looks like and how to reach it",
          interface.contains(Tmux.attachCommand),
          "docs/interface.md read \(interface.count) characters")

    // **Typing is not running, and only one of those is proved here.** `send-keys` reports that
    // tmux delivered the keystrokes, never that the shell ran them: an rc that flushes pending
    // input discards the line while both calls exit 0 — measured against a `.zshrc` calling
    // `tcsetattr(0, TCSAFLUSH, …)`. There is deliberately no check for it on this path, because
    // an rc that merely sleeps *keeps* the line, so within a start's deadline a swallowed line
    // and a slow shell are the same silence. The page has to say so rather than let "the session
    // started" carry a promise the two calls above cannot make.
    check("the page says typing the line is not the same as the shell running it",
          interface.contains("Typing is not running"),
          "docs/interface.md read \(interface.count) characters")
    check("and names the state a task really ends in when the shell swallows it",
          interface.contains(Orchestrator.State.spawnFailed.rawValue),
          "docs/interface.md read \(interface.count) characters")

    // The half of the claim that is true, which nothing had ever exercised: a pane tmux will not
    // type into is a failure and not a session. The fake refuses `send-keys` alone, so the pane
    // is really made first — which is the case the sentence is about.
    let wontType = makeFakeTmux(refusing: "can't find pane: %7", only: "send-keys")
    defer { wontType.cleanup() }
    Tmux.binaryForTesting = wontType.binary
    let refusal: String
    switch Tmux.newSessionResult(cwd: "/tmp/x", command: "claude") {
    case .success(let id): refusal = "a pane id: \(id)"
    case .failure(let problem): refusal = problem.message
    }
    check("a pane tmux would not type into comes back as a failure, in tmux's own words",
          refusal.contains("can't find pane"), refusal)
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

    // **`docs/interface.md` names that depth in prose, and nothing connected the two.** The
    // sentence is true today, and a slice two days ago came within one edit of making it a lie:
    // the number lives in a default argument nobody reading the page would think to look at. So
    // the page is compared with what the default actually asks tmux for, the same way this file
    // already compares it with `Tmux.attachCommand` rather than with a transcribed string.
    let arguments = shown.split(separator: " ").map(String.init)
    let depth = arguments.firstIndex(of: "-S").flatMap { flag -> String? in
        let value = arguments.index(after: flag)
        // `-S -200` counts backwards from the bottom of the screen; the page says how many lines
        // that is, so the sign goes.
        return value < arguments.endIndex ? String(arguments[value].dropFirst()) : nil
    } ?? ""
    check("the default history depth is readable out of what the capture asked for",
          Int(depth) != nil, "asked: \(shown)")
    let interfaceNote = (try? String(contentsOfFile: "docs/interface.md", encoding: .utf8)) ?? ""
    check("and docs/interface.md names that depth rather than one of its own",
          !depth.isEmpty && interfaceNote.contains("plus \(depth) lines of history"),
          "asked for \(depth) lines; docs/interface.md read \(interfaceNote.count) characters")

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


/// Somewhere for a live-screen test to keep its FIFOs, deleted with the group.
func makeScreenDirectory() -> URL {
    let root = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-live-screens-\(UUID().uuidString)", isDirectory: true)
    try? FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    return root
}

/// What the `changed` callback saw, across the queues it is called from.
final class ScreenEvents: @unchecked Sendable {
    private let lock = NSLock()
    private var seen: [(String, String)] = []
    func record(_ id: String, _ revision: String) {
        lock.lock(); seen.append((id, revision)); lock.unlock()
    }
    var all: [(String, String)] {
        lock.lock(); defer { lock.unlock() }; return seen
    }
    var count: Int { all.count }
}

/// A capture that can be changed from the test while the service is running.
final class ScreenSource: @unchecked Sendable {
    private let lock = NSLock()
    private var text: String?
    private var taken = 0
    init(_ initial: String?) { text = initial }
    func set(_ value: String?) { lock.lock(); text = value; lock.unlock() }
    var captures: Int { lock.lock(); defer { lock.unlock() }; return taken }
    func read() -> String? {
        lock.lock(); defer { lock.unlock() }
        taken += 1
        return text
    }
}

group("the pipe a live screen needs is one tmux command, and taking it off is another") {
    let fake = makeFakeTmux()
    defer { fake.cleanup(); Tmux.binaryForTesting = nil }
    Tmux.binaryForTesting = fake.binary

    check("a pipe is attached with the command it is to run",
          Tmux.pipe("%4", into: "cat > /tmp/x.fifo"))
    expect("as one invocation naming the pane and nothing else", fake.calls.last,
           "pipe-pane -t %4 cat > /tmp/x.fifo")
    check("and taken off with the same subcommand and no command at all", Tmux.unpipe("%4"))
    expect("which is tmux's own way of saying it", fake.calls.last, "pipe-pane -t %4")

    // The same closed vocabulary `batchedCaptureScript` keeps, and for a sharper reason: this one
    // hands tmux a *shell line*, so a word that is not an id tmux gave out has no business here.
    let before = fake.calls.count
    check("a word that is not a pane id never reaches tmux", !Tmux.pipe("; kill-server", into: "cat"))
    check("nor a bare percent", !Tmux.pipe("%", into: "cat"))
    check("nor when it is being taken off again", !Tmux.unpipe("2; kill-server"))
    check("an empty command is not a pipe", !Tmux.pipe("%4", into: ""))
    expect("so none of the four reached the terminal at all", fake.calls.count, before)

    // **Where the pipe writes, and the one path shape that is refused.** The command becomes a
    // shell line inside somebody's multiplexer; the app's own directories have spaces in them, so
    // the path is quoted, and a path that could close that quote is no command at all.
    expect("the target is the FIFO, quoted", PaneSignal.pipeCommand(writingTo: "/a b/1.fifo"),
           "cat > '/a b/1.fifo'")
    check("a path that could close the quote is refused rather than escaped",
          PaneSignal.pipeCommand(writingTo: "/a'b/1.fifo") == nil)
    check("and so is nothing at all", PaneSignal.pipeCommand(writingTo: "") == nil)

    // **The FIFO's name is the ownership record**, so it has to survive the round trip: a file
    // left behind by a crashed app is the only thing that says which pane to take a pipe off.
    let directory = makeScreenDirectory()
    defer { try? FileManager.default.removeItem(at: directory) }
    let signal = PaneSignal(directory: directory, queue: DispatchQueue(label: "test.signal"))
    expect("a pane id becomes a file name", signal.fifoURL(for: "%12")?.lastPathComponent,
           "12.fifo")
    expect("and reads back as the pane it came from",
           PaneSignal.paneID(ofFIFONamed: "12.fifo"), "%12")
    check("a file this class did not make names no pane",
          PaneSignal.paneID(ofFIFONamed: "notes.txt") == nil)
    check("and neither does one that only looks like it",
          PaneSignal.paneID(ofFIFONamed: "12a.fifo") == nil)

    // **What tmux will and will not say about a pipe.** `#{pane_pipe}` is a boolean: it answers
    // *is something piped* and never *is it ours*, which is why the state route reports a pane it
    // cannot account for rather than calling it a leak.
    let sep = "\u{1}"
    let state = Tmux.parsePipedPanes(
        "%1\(sep)1\n%2\(sep)0\n%3\(sep)1\n")
    expect("every pane tmux listed is keyed by its own id", state.count, 3)
    expect("a piped pane says so", state["%1"], true)
    expect("and one with nothing attached says that instead", state["%2"], false)
    check("a pane tmux did not list is absent rather than false", state["%9"] == nil)
    // Missing, unreadable and not-piped are three states and must not share one spelling: a row
    // this cannot read is dropped, so "no answer" never arrives looking like "no pipe".
    let messy = Tmux.parsePipedPanes("%1\(sep)yes\n%2\n\(sep)1\nnonsense\n%3\(sep)1\n")
    expect("only the rows that answered in tmux's own vocabulary survive", messy.count, 1)
    expect("and the good row is still read", messy["%3"], true)
    _ = Tmux.pipedPanes()
    check("the state reading asks for every pane on the server in one command",
          fake.calls.last == "list-panes -a -F \(Tmux.pipeStateFormat)", fake.calls.last ?? "none")
}

group("the coalescing window is what stops a signal becoming a nine-hertz sampler") {
    // Measured on a working Claude Code pane: writes every 107 ms median, 9.3 a second, no quiet
    // second in 139. So the window is not an optimisation — without it this shape captures on
    // every redraw, which is the one way it is worse than sampling.
    var window = ScreenCoalescer(window: 0.15)
    expect("the first signal for a pane is captured immediately", window.signal("%1", at: 0), .now)
    window.captured("%1", at: 0)
    expect("a redraw inside the window is scheduled for the end of it",
           window.signal("%1", at: 0.107), .at(0.15))
    expect("and every further redraw joins the one already scheduled",
           window.signal("%1", at: 0.12), .waiting)
    check("which is a state the caller can ask about", window.isScheduled("%1"))
    window.captured("%1", at: 0.15)
    check("and stops being one once that capture has happened", !window.isScheduled("%1"))
    // Asked of the coalescer that has just captured `%1`, and inside the window that capture
    // opened. A fresh coalescer would answer `.now` for any pane and prove nothing about whether
    // the two share a clock; this one answers `.now` only because they do not.
    expect("panes do not share a window", window.signal("%2", at: 0.16), .now)
    expect("a redraw a whole window later is captured immediately again",
           window.signal("%1", at: 0.31), .now)

    // **The window is measured from the last capture, not from the last signal.** That asymmetry
    // is the point of the whole shape: a burst is bounded at one capture per window, while a
    // keystroke on a session that has been quiet is captured at once.
    var quiet = ScreenCoalescer(window: 0.15)
    quiet.captured("%2", at: 5)
    expect("a change after a long silence does not wait for anything",
           quiet.signal("%2", at: 90), .now)

    // The arithmetic the comment claims, run: ten redraws at the measured 107 ms interval.
    var burst = ScreenCoalescer(window: 0.15)
    var captures = 0
    var clock = 0.0
    var due: Double?
    for _ in 0..<10 {
        if let at = due, at <= clock { burst.captured("%3", at: at); captures += 1; due = nil }
        switch burst.signal("%3", at: clock) {
        case .now: burst.captured("%3", at: clock); captures += 1
        case .at(let at): due = at
        case .waiting: break
        }
        clock += 0.107
    }
    if let at = due { burst.captured("%3", at: at); captures += 1 }
    check("ten redraws in a second cost fewer captures than redraws", captures < 10, "\(captures)")
    check("and no more than one per window", Double(captures) <= 1.07 / 0.15 + 1, "\(captures)")

    // A pane the window has forgotten is a pane nobody is watching any more, and its next signal
    // must not be answered against a stale last-capture time.
    burst.forget("%3")
    expect("a forgotten pane starts again", burst.signal("%3", at: clock), .now)
}

group("a screen nobody is watching costs nothing, and a signal is what buys the next one") {
    let fake = makeFakeTmux()
    let directory = makeScreenDirectory()
    defer {
        fake.cleanup()
        Tmux.binaryForTesting = nil
        try? FileManager.default.removeItem(at: directory)
    }
    Tmux.binaryForTesting = fake.binary

    let events = ScreenEvents()
    let source = ScreenSource("first screen\n")
    let screens = LiveScreens(directory: directory, capture: { _ in source.read() },
                              changed: { events.record($0, $1) })
    defer { screens.stop() }

    // **Nothing has been watched, so nothing has been done.** No lease, no pipe, no capture.
    expect("an unwatched Mac has attached no pipe at all", fake.calls.count, 0)
    expect("and taken no captures", source.captures, 0)

    let pane = tmuxPane("%1")
    let first = screens.read(pane)
    expect("the first read names the backend before it says anything about the screen",
           first.backend, Backend.tmux)
    expect("and what that backend can promise", first.channel, LiveScreenChannel.signalled)
    check("and a lease that starts now", first.watchingUntil > Date())
    screens.settleForTesting()

    check("reading is what attaches the pipe",
          fake.calls.contains { $0.hasPrefix("pipe-pane -t %1 cat > ") },
          fake.calls.joined(separator: " | "))
    expect("and it costs exactly one capture", source.captures, 1)
    check("which the watcher is told about once", eventually { events.count == 1 })
    let held = screens.read(pane)
    expect("the second read is answered out of that capture", held.text, "first screen\n")
    expect("and its revision is the one the event carried", held.revision, events.all.last?.1)

    // **Zero captures while nothing moves, and this is the half that separates C from sampling.**
    // A sampler asks again on every beat; this asks only when the pane has said something.
    for _ in 0..<5 { _ = screens.read(pane) }
    screens.settleForTesting()
    expect("reading again while the pane is quiet asks the terminal nothing", source.captures, 1)
    expect("and tells the watcher nothing", events.count, 1)

    // **The signal, exercised through a real FIFO.** This is what tmux's `pipe-pane` does: bytes
    // arrive on the far end of the pipe, and their arrival — not their content — is the message.
    source.set("second screen\n")
    let fifo = directory.appendingPathComponent("1.fifo")
    check("the pipe target is a FIFO on disk", FileManager.default.fileExists(atPath: fifo.path))
    let writer = FileHandle(forWritingAtPath: fifo.path)
    check("which something can write to, the way the pane's own output would", writer != nil)
    writer?.write(Data("some bytes the pane drew".utf8))
    check("a pane that moved reaches the watcher", eventually { events.count == 2 })
    expect("having cost one more capture", source.captures, 2)
    expect("and the screen the watcher can now fetch is the new one",
           screens.read(pane).text, "second screen\n")

    // **The route never waits on a terminal, and that is provable rather than probable.** A
    // capture held open by a semaphore is a wedged tmux in slow motion: the read still answers,
    // with no screen yet and a lease already running, and the screen arrives on the event.
    let slowDirectory = makeScreenDirectory()
    defer { try? FileManager.default.removeItem(at: slowDirectory) }
    let gate = DispatchSemaphore(value: 0)
    let slowEvents = ScreenEvents()
    let slow = LiveScreens(directory: slowDirectory, capture: { _ in gate.wait(); return "late\n" },
                           changed: { slowEvents.record($0, $1) })
    let asked = Date()
    let waiting = slow.read(tmuxPane("%6"))
    let waited = Date().timeIntervalSince(asked)
    check("a read whose capture has not finished answers anyway", waiting.text == nil)
    check("without waiting for it", waited < 1, "waited \(waited)s")
    expect("and says so rather than pretending the screen is empty",
           waiting.payload["pending"] as? Bool, true)
    gate.signal()
    check("and the screen follows on the event", eventually { slowEvents.count == 1 })
    slow.stop()

    // **The 21%.** Ten samples a second of a working session came back byte-identical to the last
    // one 23 times in 113. A capture that changed nothing is not a change.
    let quiet = events.count
    writer?.write(Data("more bytes, same screen".utf8))
    check("a capture whose bytes did not change costs a capture",
          eventually { source.captures == 3 })
    screens.settleForTesting()
    expect("and tells the watcher nothing at all", events.count, quiet)
    writer?.closeFile()
}

group("no pipe-pane survives a watcher that stopped watching") {
    let fake = makeFakeTmux()
    let directory = makeScreenDirectory()
    defer {
        fake.cleanup()
        Tmux.binaryForTesting = nil
        try? FileManager.default.removeItem(at: directory)
    }
    Tmux.binaryForTesting = fake.binary

    let screens = LiveScreens(directory: directory, capture: { _ in "screen\n" }, changed: { _, _ in })
    defer { screens.stop() }
    let pane = tmuxPane("%2")
    _ = screens.read(pane)
    screens.settleForTesting()
    let fifo = directory.appendingPathComponent("2.fifo")
    check("watching attached a pipe", FileManager.default.fileExists(atPath: fifo.path))

    // The lease is what "somebody is still looking" means, and nothing else is. Move the clock
    // past it rather than waiting for it, because what is being proved is the rule and not the
    // number.
    screens.sweep(now: Date().addingTimeInterval(LiveScreen.leaseSeconds + 1))
    screens.settleForTesting()
    expect("a lease nobody renewed takes the pipe off", fake.calls.last, "pipe-pane -t %2")
    check("and the FIFO with it", !FileManager.default.fileExists(atPath: fifo.path))
    check("so nothing is left claiming to be watched",
          (screens.inventory()["attached"] as? [String])?.isEmpty == true)

    // **The other half: a pipe this app attached in a previous life.** The FIFO left in its own
    // directory is the record, so recovery needs no second file to fall out of step with.
    let orphan = directory.appendingPathComponent("7.fifo")
    FileManager.default.createFile(atPath: orphan.path, contents: Data())
    let stranger = directory.appendingPathComponent("notes.txt")
    FileManager.default.createFile(atPath: stranger.path, contents: Data())
    let taken = screens.reclaim()
    // This fake tmux answers `list-panes` with nothing, which is a server that has no such pane —
    // so nothing is unpiped, and the record is forgotten anyway. That is the measured case: the
    // pipe target dies on the pane's first write once the reading end is gone.
    expect("a pane tmux no longer has is not sent a command", taken.count, 0)
    check("but its record is cleared", !FileManager.default.fileExists(atPath: orphan.path))
    check("and a file this app did not make is left alone",
          FileManager.default.fileExists(atPath: stranger.path))
}

group("an iTerm2 screen says what it cannot do rather than doing less silently") {
    let fake = makeFakeTmux()
    let directory = makeScreenDirectory()
    defer {
        fake.cleanup()
        Tmux.binaryForTesting = nil
        try? FileManager.default.removeItem(at: directory)
    }
    Tmux.binaryForTesting = fake.binary

    expect("tmux can say a pane moved", LiveScreens.channel(for: .tmux),
           LiveScreenChannel.signalled)
    // iTerm2's scripting interface publishes no byte stream, so there is no signal to have — this
    // is not a slower version of the same thing, it is a different one.
    expect("iTerm2 cannot, and the type says so", LiveScreens.channel(for: .iterm),
           LiveScreenChannel.onDemand)

    let screens = LiveScreens(directory: directory, capture: { _ in "iterm screen\n" },
                              changed: { _, _ in })
    defer { screens.stop() }
    let session = TargetSession(backend: .iterm, id: "ABC-1", name: "tab", tty: "/dev/ttys901",
                                windowIndex: 0, tabIndex: 0, assistant: .claude, cwd: "/tmp")
    _ = screens.read(session)
    screens.settleForTesting()
    check("no pipe is attached to a backend that has none",
          !fake.calls.contains { $0.hasPrefix("pipe-pane") }, fake.calls.joined(separator: " | "))
    check("and no FIFO is made for it",
          ((try? FileManager.default.contentsOfDirectory(atPath: directory.path)) ?? []).isEmpty)

    let reading = screens.read(session)
    let payload = reading.payload
    expect("the payload names the backend it is looking at", payload["backend"] as? String,
           "iterm")
    expect("and that this one has to be asked", payload["channel"] as? String, "on-demand")
    // **The interface has to be able to say it, so the payload has to carry it.** A view that
    // draws a four-millisecond live screen and a sampled one with the same chrome repeats a defect
    // this repository already had.
    expect("with the fastest it may ask, which is the Mac's number and not the phone's",
           payload["askAgainAfterMs"] as? Int, Int(LiveScreen.onDemandFloor * 1000))

    let piped = LiveScreens(directory: directory, capture: { _ in "tmux screen\n" },
                            changed: { _, _ in })
    defer { piped.stop() }
    let tmuxReading = piped.read(tmuxPane("%3"))
    piped.settleForTesting()
    let tmuxPayload = piped.read(tmuxPane("%3")).payload
    expect("a signalled backend says that instead", tmuxPayload["channel"] as? String, "signalled")
    check("and never asks the phone to poll it", tmuxPayload["askAgainAfterMs"] == nil)
    expect("both readings are about the session they were asked about",
           tmuxReading.sessionID, "%3")
    expect("and the screen carries how many lines actually came back, not how many were asked for",
           tmuxPayload["lines"] as? Int, 1)
}

}
