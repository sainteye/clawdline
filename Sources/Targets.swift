import AppKit
import Foundation

/// Where a session lives, and therefore how text gets into it.
enum Backend: String {
    case iterm
    case tmux
}

/// The one list the panel works from, merged out of every backend.
///
/// The two rarely collide in practice. When Claude Code runs inside tmux, the iTerm2 session
/// that hosts it has `tmux` as its foreground process, not `claude` — so only the tmux pane
/// gets flagged, and the same session does not show up twice. The dedupe below is a belt for
/// the cases where it would.
enum Targets {

    struct Snapshot {
        var sessions: [TargetSession] = []
        var currentID: String?
        var error: String?

        /// The ones with an assistant in them, whichever assistant that is.
        var assistantSessions: [TargetSession] { sessions.filter { $0.isAssistant } }
    }

    static func snapshot() -> Snapshot {
        var snap = Snapshot()
        let iterm = ITerm.snapshot()
        snap.currentID = iterm.currentID

        var seenTTYs = Set<String>()
        // Asked once. Walking it twice was two `list-panes` subprocesses per scan for one answer
        // that cannot have changed between them.
        let panes = Tmux.panes()
        // tmux first: when a pane and its host terminal both appear, the pane is the one that
        // can actually receive text, so it should win the tty.
        for pane in panes where pane.isAssistant {
            seenTTYs.insert(pane.tty)
            snap.sessions.append(pane)
        }
        for pane in panes where !pane.isAssistant {
            guard !seenTTYs.contains(pane.tty) else { continue }
            seenTTYs.insert(pane.tty)
            snap.sessions.append(pane)
        }
        for session in iterm.sessions where !seenTTYs.contains(session.tty) {
            snap.sessions.append(session)
        }

        // Only complain about iTerm2 when it was the only possible source of targets.
        if let e = iterm.error, snap.sessions.isEmpty { snap.error = e }
        return snap
    }

    static func send(_ text: String, to session: TargetSession) -> String? {
        switch session.backend {
        case .iterm: return ITerm.send(text, to: session.id)
        case .tmux:  return Tmux.send(text, to: session.id)
        }
    }

    /// Send a prompt whose images go over as images rather than as paths.
    ///
    /// Claude Code turns an image on the system pasteboard into `[Image #3]` when it receives a
    /// Ctrl-V — the byte 0x16, as a keystroke. So each image is lent to the pasteboard, the byte
    /// is sent **outside** the bracketed paste (inside one it is just a character), and the
    /// pasteboard is handed back.
    ///
    /// Two conditions, both of them about not making things worse:
    ///
    /// - **Only into a Claude Code session.** In a shell, Ctrl-V is readline's quoted-insert and
    ///   would put a control character in the command line. `isClaude` already knows — and it is
    ///   Claude Code specifically rather than any assistant, because `[Image #3]` is its
    ///   convention and nothing says Codex reads the same byte the same way.
    /// - **Only when the image loads.** Anything that fails falls back to its path, which is
    ///   what this did before and is never wrong, only plainer.
    ///
    /// A pause between pieces because the other end is a program reading a tty: the paste and
    /// the keystroke arrive as bytes in order, but the clipboard is read on the far side when
    /// the keystroke is handled, and that is not the same instant it arrives.
    static func send(_ pieces: [Drop.Piece], to session: TargetSession) -> String? {
        let asPath = pieces.map { piece -> String in
            if case .image(let path) = piece { return Drop.quoted(path) }
            if case .text(let text) = piece { return text }
            return ""
        }.joined()

        let images = pieces.contains { if case .image = $0 { return true }; return false }
        guard images, session.isClaude, Config.shared.sendImagesAsPaste else {
            return send(asPath, to: session)
        }

        // Borrowed once for the whole send and handed back at the end, rather than around each
        // image: the pasteboard is one shared thing, and putting it back between two images only
        // to take it again is churn nobody benefits from.
        let pasteboard = NSPasteboard.general
        let saved = Drop.contents(of: pasteboard)
        var failed: [String] = []
        var problem: String?

        for piece in pieces {
            switch piece {
            case .text(let text):
                guard !text.isEmpty else { continue }
                problem = paste(text, to: session, submit: false)
            case .image(let path) where Drop.offer(path, on: pasteboard):
                problem = keystroke(0x16, to: session)
                // The bytes and the keystroke arrive in order, but the far side reads the
                // clipboard when it handles the key, and that is not the instant it arrives.
                usleep(250_000)
            case .image(let path):
                // Its bytes could not be read. The path still works and is only plainer, which
                // beats a sentence pointing at nothing.
                failed.append(path)
                problem = paste(Drop.quoted(path), to: session, submit: false)
            }
            if let problem { Drop.put(saved, on: pasteboard); return problem }
        }

        let err = submit(to: session)
        // After the Enter, not before: the last image is still being read on the other side.
        usleep(200_000)
        Drop.put(saved, on: pasteboard)
        if !failed.isEmpty { Log.write("send: \(failed.count) image(s) went as paths") }
        return err
    }

    private static func paste(_ text: String, to session: TargetSession, submit: Bool) -> String? {
        switch session.backend {
        case .iterm: return ITerm.send(text, to: session.id, submit: submit)
        case .tmux:  return Tmux.send(text, to: session.id, submit: submit)
        }
    }

    /// Answer a menu, or send the one escape sequence this app names as a key.
    ///
    /// Claude Code's `AskUserQuestion` picker takes a bare digit **outside a bracketed paste** and
    /// treats it as a selection — in a single-select it also confirms, in a multi-select it
    /// toggles and `Tab` moves on to the review. That is why this exists at all and why it is a
    /// key rather than a string: `send` wraps its text in a bracketed paste, and **the picker
    /// throws the whole paste away and then acts on the Return that follows it**.
    ///
    /// Codex's dialogs answer the same way, which was checked rather than assumed: `2` on its
    /// trust dialog picks "No, quit" and the process is gone before the next capture.
    ///
    /// **Allowlisted here rather than at the route**, because the danger is not that somebody
    /// answers the wrong question — it is that a byte channel into a tty is an escape-sequence
    /// channel into a tty. `1`–`9` and `Tab` answer a menu and can do nothing else; the only
    /// sequence admitted is back-tab, which Claude Code uses to cycle permission modes. Keeping
    /// it whole also matters: three HTTP round trips could interleave with somebody typing and
    /// turn an intended key into three unrelated bytes.
    static func answer(_ byte: UInt8, to session: TargetSession) -> String? {
        answer([byte], to: session)
    }

    static func answer(_ bytes: [UInt8], to session: TargetSession) -> String? {
        let digit = bytes.count == 1 && (0x31...0x39).contains(bytes[0])
        let menuKey = digit || (bytes.count == 1 && bytes[0] == 0x09)
        let backTab: [UInt8] = [0x1b, 0x5b, 0x5a]       // ESC [ Z
        guard menuKey || bytes == backTab else {
            return "That is not a key this can send."
        }
        guard digit else { return keystroke(bytes, to: session) }
        let want = Int(bytes[0] - 0x30)

        // **Which kind of picker is this, before anything is typed at it.** A dialog drawn without
        // numbers has numeric selection switched off in the same breath, so the digit is not
        // ignored politely — it falls through the dialog and is typed into the composer
        // underneath. Reading first costs one capture on a path somebody is waiting on anyway.
        //
        // A capture that fails, or a screen that no longer parses, takes the numbered path: that
        // is what this did before this branch existed, and a dialog the reader is looking at right
        // now is far more likely to be the common shape than a shape nothing could read.
        let seen = capture(session).flatMap {
            SessionState.menu($0, assistant: session.assistant ?? .claude, hookWaiting: true)
        }
        if let menu = seen, !menu.numbered { return highlight(row: want, of: menu, on: session) }
        if let failure = keystroke(bytes, to: session) { return failure }

        // **A multi-select has nothing to confirm, and confirming it undoes the tap.** Its digits
        // toggle a row where a single-select's move the highlight, so the Return that commits one
        // is, on the other, a second press of the same row. Measured the hard way: the caret starts
        // on the first option, so tapping the first option was the one tap that reliably did
        // nothing — toggled by the digit and toggled straight back by the confirmation. Its rows
        // are sent by the button under them, and that is ``submitMenu(on:)``'s job.
        if seen?.submit != nil { return nil }
        return confirmSelection(want, on: session)
    }

    /// Answer a picker that does not take digits, by moving its highlight onto the row and
    /// confirming there.
    ///
    /// Claude Code binds `j` and `k` to a select's next and previous row alongside the arrow keys,
    /// and `Return` still accepts — the `hideIndexes` flag turns off numeric selection and nothing
    /// else. Two plain letters are deliberately preferred over `ESC [ B`: this app has one place
    /// where bytes reach a tty and the argument for it is that none of them are escape sequences.
    /// A stray `j` in a composer is a letter somebody can see and delete.
    ///
    /// **Nothing is confirmed on faith.** The walk ends in ``confirmSelection(_:on:)`` like every
    /// other answer, so if the reading was wrong and this is not a picker at all, the highlight
    /// never lands, no Return is sent, and the failure is a button that did nothing rather than an
    /// answer nobody chose.
    private static func highlight(row want: Int, of menu: SessionState.Menu,
                                  on session: TargetSession) -> String? {
        guard let here = menu.selected else { return nil }
        let move = walk(from: here, to: want)
        for _ in 0..<move.times {
            if let failure = keystroke([move.key], to: session) { return failure }
        }
        return confirmSelection(want, on: session)
    }

    /// The keystroke that walks a highlight from one row to another, and how many of them.
    /// Split out from the sending so the arithmetic can be checked without a terminal.
    static func walk(from here: Int, to want: Int) -> (key: UInt8, times: Int) {
        (key: want >= here ? 0x6a : 0x6b, times: abs(want - here))   // j / k
    }

    /// Press the button a multi-select draws under its rows.
    ///
    /// **A multi-select does not answer on a digit.** Its numbers toggle rows, Return on a row
    /// toggles that row too, and the only thing that sends is Return while the caret is on the
    /// button below the list. So this is not a keystroke the phone can name — it is a short walk
    /// this end has to make, one row at a time, reading the screen after each step.
    ///
    /// **The step is Tab, and `j` was wrong.** `j` is bound to "next row" and looked like the
    /// obvious choice; measured on 2026-08-26 against a real dialog, it walked four rows and then
    /// typed `jjjj` into the question's own text box. The last row of an `AskUserQuestion` is
    /// `Type something`, an input, and a focused input passes only a named few keys through —
    /// `up`, `down`, `escape`, `tab`, `return` — and swallows everything else as typing. Tab is on
    /// that list, moves to the next row, lands on the button from the last one, **and does nothing
    /// once it is there**, so overshooting is not a failure mode it has. It was already the one
    /// non-digit key this app was allowed to send.
    ///
    /// The check still comes before each step rather than after, and the loop is bounded by the
    /// rows it can see plus two, so a dialog that stops responding costs a few captures and then
    /// gives up with the screen as it was.
    static func submitMenu(on session: TargetSession) -> String? {
        var steps = 0
        while true {
            guard let screen = capture(session),
                  let menu = SessionState.menu(screen, assistant: session.assistant ?? .claude,
                                               hookWaiting: true) else {
                return "Could not read that session's screen."
            }
            guard let submit = menu.submit else {
                return "That question has no Submit to press."
            }
            if submit.selected { return keystroke(13, to: session) }
            guard steps <= menu.options.count + 1 else {
                return "The highlight would not move onto Submit."
            }
            if let failure = keystroke([submitStep], to: session) { return failure }
            steps += 1
            Thread.sleep(forTimeInterval: 0.12)
        }
    }

    /// The key that walks a multi-select's focus toward its button — see ``submitMenu(on:)`` for
    /// why it is this one and not the row-navigation key it looks like it should be.
    static let submitStep: UInt8 = 0x09   // Tab

    /// Press Return, but only once the screen shows the digit landed where it was meant to.
    ///
    /// **A digit no longer confirms.** The comment above described what the picker used to do and
    /// measurement caught up with it: sending `3` moves the highlight to the third row and leaves
    /// the dialog open, sending `1` moves it back, and nothing is ever submitted. Someone
    /// answering from a phone saw the tap do nothing at all, pressed again, and the transcript
    /// recorded no answer — the question had to be finished at the keyboard.
    ///
    /// **Confirming blind would be worse than the bug.** If a picker ever stops taking digits,
    /// the highlight does not move and a bare Return submits whatever row it happens to be
    /// sitting on — a wrong answer sent silently, where today there is merely no answer. So the
    /// screen is read back first, and Return goes only when the highlight is on the row that was
    /// asked for. Anything else — a capture that fails, a shape that no longer parses, a
    /// highlight somewhere else — leaves the dialog exactly as it was.
    ///
    /// Two reads, because a terminal repaints on its own schedule and the first can arrive before
    /// the redraw. Both are cheap next to the round trip that has already happened.
    private static func confirmSelection(_ want: Int, on session: TargetSession) -> String? {
        for attempt in 0..<2 {
            Thread.sleep(forTimeInterval: attempt == 0 ? 0.12 : 0.25)
            guard let screen = capture(session) else { continue }
            // `hookWaiting` is true here because this path only exists behind a menu the app
            // already drew buttons for: the reader pressed one of them a moment ago. The gate
            // guards against calling a screen a menu unprompted, which is not this.
            guard let menu = SessionState.menu(screen, assistant: session.assistant ?? .claude,
                                               hookWaiting: true) else { continue }
            if menu.selected == want { return keystroke(13, to: session) }
        }
        return nil
    }

    /// Whether this session is showing a menu right now, which changes what typing into it means.
    ///
    /// A capture, so only ask when it matters. See the `/send` route: a session that is merely
    /// `waiting` is the common case and answering it with words is correct; a session showing a
    /// *picker* is the case where words are silently discarded and the Return confirms whatever
    /// happens to be highlighted.
    static func isChoosing(_ session: TargetSession) -> Bool {
        guard let screen = capture(session) else { return false }
        return SessionState.isChoosing(screen, assistant: session.assistant ?? .claude)
    }

    /// End a session and close the tab it was in.
    ///
    /// **Two steps, in this order, and the order is the whole of it.** `/exit` first, so Claude
    /// Code leaves the way it would if somebody typed it — flushing its transcript rather than
    /// being killed in the middle of writing one. Then the tab, once the process it was holding
    /// is actually gone.
    ///
    /// Closing straight away would work and would be worse: the session's own record of the
    /// conversation is the thing you would still want tomorrow, and it is being appended to right
    /// up to the moment the process ends.
    ///
    /// **This used to be a fixed pause, and the fixed pause is what broke.** It waited 1.2
    /// seconds and closed regardless — which is fine when the word lands at an idle prompt and
    /// wrong the moment it does not. A session in the middle of a tool call queues `/exit` and
    /// keeps working, so the tab still had a job in it when the close arrived, and iTerm2 does
    /// what a terminal should do about that: it puts up a sheet and asks. A sheet is modal.
    /// The Apple event never returns, `osascript` never exits, and because every remote request
    /// is answered on one serial queue, a phone that pressed End froze every page in the house
    /// until somebody walked to the Mac and clicked a button they could not see.
    ///
    /// So the pause is now an answer instead of a guess — see ``Farewell``. The ordinary case
    /// got faster too: `/exit` at an idle prompt is done in a few hundred milliseconds, and this
    /// no longer sits out the rest of the second and a bit.
    ///
    /// **It still ends the session.** A session that will not leave on the word is asked with a
    /// signal and then told, which is the same outcome the sheet was offering and none of the
    /// waiting. Worst case is a shade over five seconds rather than forever, and that number
    /// matters: this runs on the one queue that answers every remote request, so it is a ceiling
    /// on how long a phone can be left looking at a page that has stopped moving. This is
    /// documented as ending a session, not as saving one.
    static func end(_ session: TargetSession) -> String? {
        // Typed as a line, not as a keystroke: the word is text at a prompt, and `send` already
        // knows how to put text in front of an assistant and press Return. Which word depends on
        // which assistant — Claude Code leaves on `/exit`, Codex on `/quit`, and each refuses the
        // other's, which would leave the session open with the tab closing under it.
        let word = (session.assistant ?? .claude).quitLine
        if let failure = send(word, to: session) { return failure }
        waitToBeGone(session)
        switch session.backend {
        case .iterm: return ITerm.close(session.id)
        case .tmux:  return Tmux.close(session.id)
        }
    }

    /// What to do next while waiting for a session to finish leaving.
    ///
    /// Split out from the loop that runs it because this is the part with the decisions in it,
    /// and a decision that can only be exercised by ending somebody's real session is a decision
    /// with no tests. The loop below is three lines of sleeping; everything that could be wrong
    /// about *when to stop being polite* is here, and is checked against a clock that is passed
    /// in rather than one that has to pass.
    enum Farewell {
        enum Step: Equatable {
            /// Still leaving on its own. Look again in a moment.
            case wait
            /// Ask the process to go.
            case term(pid_t)
            /// Stop asking.
            case kill(pid_t)
            /// Nothing is holding the tab. Take it.
            case close
        }

        /// How long the word gets before anything harsher happens.
        ///
        /// Three seconds, and short on purpose. A session at its prompt reads `/exit` and is gone
        /// inside one; a session in the middle of a tool call has *queued* the word and will not
        /// read it until the tool returns, which is not a thing three more seconds fixes. Waiting
        /// longer would only be waiting — and this runs on the queue that answers every other
        /// request, so every second here is a second the page does not repaint.
        ///
        /// It can afford to be short because the next rung is not violence. `SIGTERM` is how a
        /// program is asked to leave; both assistants handle it and flush on the way out. The
        /// thing this replaced — closing the tab regardless — hung up the tty underneath them,
        /// which is less notice than any step below.
        static let polite: TimeInterval = 3
        /// After `SIGTERM`. Claude Code and Codex both handle it and leave; this is the room to.
        static let afterTerm: TimeInterval = 1.5
        /// After `SIGKILL`. Only the kernel's own bookkeeping happens in here.
        static let afterKill: TimeInterval = 1

        static func step(elapsed: TimeInterval, pid: pid_t?,
                         termed: Bool, killed: Bool) -> Step {
            // Gone is gone, at any point — including before the first sleep, which is the
            // common case and the reason this is faster than what it replaces.
            guard let pid else { return .close }
            if elapsed < polite { return .wait }
            if !termed { return .term(pid) }
            if elapsed < polite + afterTerm { return .wait }
            if !killed { return .kill(pid) }
            if elapsed < polite + afterTerm + afterKill { return .wait }
            // Past a `SIGKILL` and still on the tty means a process the kernel cannot reap
            // either, and no amount of further waiting changes that. Closing is what is left,
            // and it is what the old code did to every session unconditionally.
            return .close
        }
    }

    /// Block until nothing is running on that session's tty, or until it has been made so.
    ///
    /// The tty and not the session id, because this is a question about processes and iTerm2's
    /// idea of a session is not one. It works the same for a tmux pane, which is why it is here
    /// rather than in ``ITerm``: `kill-pane` does not put up a sheet, but it does send a `SIGHUP`
    /// to whatever is still running, and a transcript half-written is no better on that side.
    private static func waitToBeGone(_ session: TargetSession) {
        let started = Date()
        var termed = false, killed = false
        while true {
            let running = ITerm.assistant(onTTY: session.tty)
            let elapsed = Date().timeIntervalSince(started)
            switch Farewell.step(elapsed: elapsed, pid: running?.pid,
                                 termed: termed, killed: killed) {
            case .close:
                return
            case .wait:
                Thread.sleep(forTimeInterval: 0.2)
            case .term(let pid):
                termed = true
                kill(pid, SIGTERM)
                Thread.sleep(forTimeInterval: 0.2)
            case .kill(let pid):
                killed = true
                kill(pid, SIGKILL)
                Thread.sleep(forTimeInterval: 0.2)
            }
        }
    }

    private static func keystroke(_ bytes: [UInt8], to session: TargetSession) -> String? {
        switch session.backend {
        case .iterm: return ITerm.keystroke(bytes, to: session.id)
        case .tmux:  return Tmux.keystroke(bytes, to: session.id)
        }
    }

    private static func keystroke(_ byte: UInt8, to session: TargetSession) -> String? {
        keystroke([byte], to: session)
    }

    private static func submit(to session: TargetSession) -> String? {
        switch session.backend {
        case .iterm: return ITerm.submit(session.id)
        case .tmux:  return Tmux.submit(session.id)
        }
    }

    // Starting a session is deliberately **not** here. It used to be — `create(cwd:command:)`,
    // which ran whatever it was handed wherever it was pointed — and that made it a general
    // "run this there" primitive one hop from an HTTP route. Every other function in this file
    // acts on a session somebody already opened; that one created execution, which is a different
    // kind of thing and now lives in ``StartPoints`` with the policy that decides what may be
    // started and where. The new session is not in any snapshot yet either way: the caller gets
    // an id and waits for the next reading like everybody else, because returning something
    // half-filled would be a third kind of `TargetSession` that is true for about a second.

    static func capture(_ session: TargetSession) -> String? {
        switch session.backend {
        case .iterm: return ITerm.capture(session.id)
        case .tmux:  return Tmux.capture(session.id)
        }
    }

    /// What is visible now, without tmux scrollback. A current mode cannot be read from history:
    /// after somebody cycles, an older status line is still true text and a false current answer.
    static func visibleScreen(of session: TargetSession) -> String? {
        switch session.backend {
        case .iterm: return ITerm.capture(session.id)
        case .tmux:  return Tmux.capture(session.id, scrollback: 0)
        }
    }

    /// What each of these sessions is doing, keyed by session id.
    ///
    /// Batched per backend rather than session by session, because the cost here is round trips
    /// and not text: iTerm2 answers for all of them in one osascript run, and tmux is asked pane
    /// by pane only because `capture-pane` has no plural.
    ///
    /// A session that could not be read comes back as `.unknown`, which is why the map is filled
    /// in for every session asked about rather than only the ones that answered — a missing key
    /// and a session that is doing nothing must not look the same to the caller.
    static func states(of sessions: [TargetSession]) -> [String: SessionState] {
        reading(of: sessions).states
    }

    /// What was on each screen, keyed by session id: the state, and the menu if there was one.
    ///
    /// **The menu comes out of the same capture, which is the only reason it is affordable.**
    /// Reading a menu used to mean a second round trip to the terminal — `isChoosing(_ session:)`
    /// still is one, and is still right for the single question `/send` asks before it refuses.
    /// But the phone needs the options on *every* beat, for every waiting session, and paying an
    /// osascript per session per second for that would have been the most expensive thing in the
    /// app. These screens have already been fetched. Parsing them twice costs nothing.
    struct Reading {
        var states: [String: SessionState] = [:]
        /// Only the sessions actually showing one, so a missing key means "no menu" and never
        /// "not looked at" — every session asked about gets a state, and most get no menu.
        var menus: [String: SessionState.Menu] = [:]
    }

    /// `hookWaiting` is the sessions something outside the screen says are stopped on a question
    /// — a hook note, or Claude Code's own registry entry, which agree about what the fact means
    /// and only differ in where it came from. It opens one parsing gate and asserts nothing: see
    /// ``SessionState/menu(_:assistant:tailLines:hookWaiting:)``.
    static func reading(of sessions: [TargetSession], hookWaiting: Set<String> = []) -> Reading {
        var out = Reading()

        func note(_ session: TargetSession, _ screen: String?) {
            let assistant = session.assistant ?? .claude
            let gateOpen = hookWaiting.contains(session.id)
            out.states[session.id] = SessionState.read(screen, assistant: assistant,
                                                       hookWaiting: gateOpen)
            // Only when the screen says so. Opening the gate lets that same screen safely count a
            // flush-left caret as a selection; it never supplies `.waiting` in the screen's place.
            guard out.states[session.id] == .waiting, let screen else { return }
            out.menus[session.id] = SessionState.menu(Ansi.plain(screen), assistant: assistant,
                                                      hookWaiting: gateOpen)
        }

        let iterm = sessions.filter { $0.backend == .iterm }
        if !iterm.isEmpty {
            let tails = ITerm.tails(ids: iterm.map { $0.id })
            for session in iterm { note(session, tails[session.id]) }
        }
        // Only the visible pane: `-S -0` starts at the top of the screen rather than in the
        // scrollback, which is both cheaper and the right question — what is on screen *now*.
        for session in sessions where session.backend == .tmux {
            note(session, Tmux.capture(session.id, scrollback: 0))
        }
        return out
    }

    /// Where that session is working. tmux hands it over with the pane list; for iTerm2 it
    /// has to be asked of the process, so it is only looked up when something needs it.
    private static let cwdLock = NSLock()
    private static var cwdCache: [String: (at: CFAbsoluteTime, path: String)] = [:]

    /// Where a session is working.
    ///
    /// Remembered for a while, because a Claude Code session does not move: it is started in a
    /// directory and stays there. Asking again every second cost a process listing and an
    /// `lsof` for an answer that had not changed since the session began.
    static func workingDirectory(of session: TargetSession) -> String? {
        if let cwd = session.cwd, !cwd.isEmpty { return cwd }
        cwdLock.lock()
        if let hit = cwdCache[session.id], CFAbsoluteTimeGetCurrent() - hit.at < 20 {
            defer { cwdLock.unlock() }
            return hit.path
        }
        cwdLock.unlock()

        let bare = session.tty.replacingOccurrences(of: "/dev/", with: "")
        guard let pid = ITerm.assistantPIDs()[bare]?.pid,
              let path = ITerm.workingDirectory(ofPID: pid) else { return nil }
        cwdLock.lock()
        cwdCache[session.id] = (CFAbsoluteTimeGetCurrent(), path)
        cwdLock.unlock()
        return path
    }

    /// When the assistant in this session started. Used to tell its record from the records of
    /// every other session in the same project.
    ///
    /// Remembered for the same reason and the same while as the working directory: it is asked
    /// once per session on every move through the list, it costs a `ps` of its own on top of the
    /// one that finds the pid, and the answer it gives is a fact about a process that has already
    /// started. Held rather than kept forever, because a tab whose session is restarted keeps its
    /// id and gets a new start time.
    private static var startCache: [String: (at: CFAbsoluteTime, started: Date?)] = [:]

    /// The process running in this session, when there is one this can see.
    ///
    /// The tty is the only link between a pane and a process, which is why this exists at all
    /// and why it answers nothing for a session in a shell. Read off the same cached `ps` as
    /// everything else on this path.
    static func pid(of session: TargetSession) -> Int32? {
        let bare = session.tty.replacingOccurrences(of: "/dev/", with: "")
        return ITerm.assistantPIDs()[bare]?.pid
    }

    static func processStart(of session: TargetSession) -> Date? {
        cwdLock.lock()
        if let hit = startCache[session.id], let started = hit.started,
           CFAbsoluteTimeGetCurrent() - hit.at < 20 {
            defer { cwdLock.unlock() }
            return started
        }
        cwdLock.unlock()

        let bare = session.tty.replacingOccurrences(of: "/dev/", with: "")
        let started = ITerm.assistantPIDs()[bare].flatMap { ITerm.processStart(ofPID: $0.pid) }
        cwdLock.lock()
        // Absence is a startup observation, not a process fact. Caching it for twenty seconds
        // can cover the entire briefing window and silence every registry lookup in it.
        if started != nil { startCache[session.id] = (CFAbsoluteTimeGetCurrent(), started) }
        cwdLock.unlock()
        return started
    }

    /// The pid-keyed start cache, used for background sessions and callers that already fixed
    /// the foreground pid so the terminal cannot contribute a mismatched start time.
    ///
    /// Keyed by pid because that is all there is to key it by: the background session behind a
    /// parked tab is running under the daemon, on no terminal, and the only thing naming it is
    /// the number in its file's name. Without this the poll would pay a `ps` a second for every
    /// tab somebody has parked — which is the one cost that would have made following a park
    /// expensive, since everything else about it is a directory listing.
    private static var bgStartCache: [Int32: (at: CFAbsoluteTime, started: Date?)] = [:]

    static func processStart(ofPID pid: Int32) -> Date? {
        cwdLock.lock()
        if let hit = bgStartCache[pid], let started = hit.started,
           CFAbsoluteTimeGetCurrent() - hit.at < 20 {
            defer { cwdLock.unlock() }
            return started
        }
        cwdLock.unlock()

        let started = ITerm.processStart(ofPID: pid)
        cwdLock.lock()
        // Held for the same twenty seconds as a tab's, and for the same reason: a number can be
        // handed to a different process, and that is what the start time being cached is about.
        // Swept on the way past because this one is keyed by pid, so it grows with every
        // background session the machine has ever run rather than with the tabs that are open.
        let now = CFAbsoluteTimeGetCurrent()
        bgStartCache = bgStartCache.filter { now - $0.value.at < 20 }
        if started != nil { bgStartCache[pid] = (now, started) }
        cwdLock.unlock()
        return started
    }

    /// What Claude Code says about these sessions, and the process facts that decide which of
    /// its files is allowed to speak for which session. See ``SessionRegistry``.
    ///
    /// **The two halves cost very different amounts, which is why they are read in this order.**
    /// The pid comes out of one `ps` shared by every session and cached for a couple of seconds.
    /// The start time is a `ps` of its own per session, so it is only asked for the sessions a
    /// registry file was actually found for — which is also the whole of why nothing in here
    /// names Codex. A Codex session writes no file, so it drops out at the same line a Claude
    /// Code session with no registry at all drops out at.
    static func registry(of sessions: [TargetSession]) -> SessionRegistry.Reading {
        var pids: [String: Int32] = [:]
        var byID: [String: TargetSession] = [:]
        for session in sessions where session.isAssistant {
            byID[session.id] = session
            if let pid = pid(of: session) { pids[session.id] = pid }
        }
        guard !pids.isEmpty else { return SessionRegistry.Reading() }

        var out = SessionRegistry.Reading()
        out.entries = SessionRegistry.entries(pids: Array(pids.values))
        for (id, pid) in pids where out.entries[pid] != nil {
            guard let session = byID[id] else { continue }
            out.processes[id] = SessionRegistry.Process(pid: pid,
                                                        started: processStart(of: session))
        }
        // A parked tab's file is about a conversation that has left it, so the other half of the
        // reading is where that conversation went. Free unless somebody has parked something:
        // with no parked file in hand this looks at nothing and runs no `ps`.
        SessionRegistry.attachBackground(to: &out) { processStart(ofPID: $0) }
        return out
    }

    /// Put this session in front of you — or, with `activate: false`, merely make it the one its
    /// terminal is showing. tmux is always the second kind: selecting a pane moves nothing to the
    /// front, because tmux is not an application and has no front to move to.
    static func reveal(_ session: TargetSession, activate: Bool = true) {
        switch session.backend {
        case .iterm: ITerm.reveal(session.id, activate: activate)
        case .tmux:  Tmux.reveal(session.id)
        }
    }
}
