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
        let menuKey = bytes.count == 1
            && ((0x31...0x39).contains(bytes[0]) || bytes[0] == 0x09)
        let backTab: [UInt8] = [0x1b, 0x5b, 0x5a]       // ESC [ Z
        guard menuKey || bytes == backTab else {
            return "That is not a key this can send."
        }
        return keystroke(bytes, to: session)
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
        if let hit = startCache[session.id], CFAbsoluteTimeGetCurrent() - hit.at < 20 {
            defer { cwdLock.unlock() }
            return hit.started
        }
        cwdLock.unlock()

        let bare = session.tty.replacingOccurrences(of: "/dev/", with: "")
        let started = ITerm.assistantPIDs()[bare].flatMap { ITerm.processStart(ofPID: $0.pid) }
        cwdLock.lock()
        startCache[session.id] = (CFAbsoluteTimeGetCurrent(), started)
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
