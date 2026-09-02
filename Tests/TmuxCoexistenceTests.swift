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
    // The flags string measured on this Mac against tmux 3.6a, from iTerm2's own `-CC` client.
    let live = ["/dev/ttys006", "attached,focused,control-mode,wait-exit,pause-after=0,UTF-8",
                "work"].joined(separator: sep)
    let clients = Tmux.parseControlModeClients(live)
    expect("the live control-mode client is found", clients.count, 1)
    expect("its gateway tty is kept", clients.first?.tty, "/dev/ttys006")
    expect("and the session it is attached to", clients.first?.session, "work")

    let ordinary = ["/dev/ttys011", "attached,focused,UTF-8", "work"].joined(separator: sep)
    expect("an ordinary attached client is not one", Tmux.parseControlModeClients(ordinary).count, 0)

    // `#{client_flags}` is an open vocabulary tmux adds to between releases. A flag that merely
    // contains these letters is not this flag, so the list is split and compared whole.
    let lookalike = ["/dev/ttys012", "attached,control-mode-pending,UTF-8", "s"]
        .joined(separator: sep)
    expect("a flag that only contains the word is not the flag",
           Tmux.parseControlModeClients(lookalike).count, 0)

    let mixed = [live,
                 ordinary,
                 ["/dev/ttys013", "attached,control-mode,UTF-8", "other"].joined(separator: sep)]
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
}

group("an iTerm2 row with no pty is attributed rather than dropped") {
    func row(_ id: String, tty: String, ptyless: Bool = false) -> [String: Any] {
        var out: [String: Any] = ["id": id, "name": "tab " + id, "tty": tty,
                                  "profile": "Default", "win": 0, "tab": 0]
        if ptyless { out["ptyless"] = true }
        return out
    }

    // What one `-CC` session looked like on this Mac: sixteen rows, fifteen of them ordinary, one
    // drawn for tmux with an empty tty — plus the gateway itself, which is an ordinary row with a
    // real pty and must keep behaving as one.
    let rows = [row("A", tty: "/dev/ttys006"),
                row("B", tty: "", ptyless: true),
                row("C", tty: "/dev/ttys011")]
    expect("the pty-less row is counted", ITerm.ptylessRowCount(rows), 1)
    expect("and is kept out of the iterm-backend rows",
           ITerm.attributableRows(rows).compactMap { $0["id"] as? String }, ["A", "C"])
    check("the tmux gateway keeps its own row",
          ITerm.attributableRows(rows).contains { $0["tty"] as? String == "/dev/ttys006" })
    expect("a scan with no such row asks nothing of tmux",
           ITerm.ptylessRowCount([row("A", tty: "/dev/ttys006")]), 0)

    // The marker is one string key shared across two languages. If either side renames it the
    // rows silently go back to being dropped, so the agreement is checked rather than assumed.
    let script = (try? String(contentsOfFile: "Resources/iterm.js", encoding: .utf8)) ?? ""
    check("iterm.js writes the marker key Swift reads here",
          script.contains("row.ptyless = true"),
          "Resources/iterm.js read \(script.count) characters")
    expect("and it writes it on both list paths",
           script.components(separatedBy: "row.ptyless = true").count - 1, 2)

    let none = ITerm.ptylessRowAttribution(rows: 0, secondSource: .tmux(controlModeClients: 0))
    check("no pty-less rows lowers nothing", !none.lowersConfidence)
    check("and says nothing", none.error == nil)

    let drawn = ITerm.ptylessRowAttribution(rows: 2, secondSource: .tmux(controlModeClients: 1))
    expect("tmux agreeing attributes every such row", drawn.attributedToTmux, 2)
    expect("and leaves none unexplained", drawn.unexplained, 0)
    check("so one -CC session no longer makes the inventory incomplete", !drawn.lowersConfidence)
    check("and there is nothing to report", drawn.error == nil)

    // The other half of a second source: when it disagrees, the row is exactly the unreadable
    // one the original guard was written for.
    let anomaly = ITerm.ptylessRowAttribution(rows: 1, secondSource: .tmux(controlModeClients: 0))
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
        rows: 3, secondSource: .unavailable("tmux did not finish within 15 seconds"))
    expect("a tmux that never answered attributes nothing", quiet.attributedToTmux, 0)
    check("and still lowers confidence", quiet.lowersConfidence)
    check("but says so differently from a tmux that said no",
          quiet.error?.contains("could not be asked") == true, quiet.error ?? "nil")
    check("carrying the reason it could not be asked",
          quiet.error?.contains("15 seconds") == true, quiet.error ?? "nil")
    check("and the two answers are not the same string",
          quiet.error != ITerm.ptylessRowAttribution(
            rows: 3, secondSource: .tmux(controlModeClients: 0)).error)
}

group("revealing a tmux pane brings iTerm2 forward only when tmux says it drew it") {
    let drawing = Tmux.ControlModeClient(tty: "/dev/ttys006", session: "work")
    let elsewhere = Tmux.ControlModeClient(tty: "/dev/ttys020", session: "reading")

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
            controlModeClients: [Tmux.ControlModeClient(tty: "/dev/ttys014", session: "")]))
}

}
