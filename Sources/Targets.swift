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

        var claudeSessions: [TargetSession] { sessions.filter { $0.isClaude } }
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
        for pane in panes where pane.isClaude {
            seenTTYs.insert(pane.tty)
            snap.sessions.append(pane)
        }
        for pane in panes where !pane.isClaude {
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
    ///   would put a control character in the command line. `isClaude` already knows.
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

    /// Answer a menu with the one byte its picker reads.
    ///
    /// Claude Code's `AskUserQuestion` picker takes a bare digit **outside a bracketed paste** and
    /// treats it as a selection — in a single-select it also confirms, in a multi-select it
    /// toggles and `Tab` moves on to the review. That is why this exists at all and why it is a
    /// byte rather than a string: `send` wraps its text in a bracketed paste, and **the picker
    /// throws the whole paste away and then acts on the Return that follows it**.
    ///
    /// **Allowlisted here rather than at the route**, because the danger is not that somebody
    /// answers the wrong question — it is that a byte channel into a tty is an escape-sequence
    /// channel into a tty. `1`–`9` and `Tab` answer a menu and can do nothing else; `ESC` alone
    /// cancels, and three bytes of an arrow key sent as three round trips arrive as one.
    static func answer(_ byte: UInt8, to session: TargetSession) -> String? {
        guard (0x31...0x39).contains(byte) || byte == 0x09 else {
            return "That is not a key this can send."
        }
        return keystroke(byte, to: session)
    }

    /// Whether this session is showing a menu right now, which changes what typing into it means.
    ///
    /// A capture, so only ask when it matters. See the `/send` route: a session that is merely
    /// `waiting` is the common case and answering it with words is correct; a session showing a
    /// *picker* is the case where words are silently discarded and the Return confirms whatever
    /// happens to be highlighted.
    static func isChoosing(_ session: TargetSession) -> Bool {
        guard let screen = capture(session) else { return false }
        return SessionState.isChoosing(screen)
    }

    /// End a session and close the tab it was in.
    ///
    /// **Two steps, in this order, and the order is the whole of it.** `/exit` first, so Claude
    /// Code leaves the way it would if somebody typed it — flushing its transcript rather than
    /// being killed in the middle of writing one. Then the tab, after a pause long enough for it
    /// to have gone.
    ///
    /// Closing straight away would work and would be worse: the session's own record of the
    /// conversation is the thing you would still want tomorrow, and it is being appended to right
    /// up to the moment the process ends.
    ///
    /// **The pause is not a guarantee.** If Claude Code is mid-answer it will not have finished
    /// leaving, and the tab closes under it — the same outcome as closing the tab by hand, which
    /// is what the person asking for this would otherwise do. Saying so is the point: this is
    /// documented as ending a session, not as saving one.
    static func end(_ session: TargetSession) -> String? {
        // Typed as a line, not as a keystroke: `/exit` is text at a prompt, and `send` already
        // knows how to put text in front of Claude Code and press Return.
        if let failure = send("/exit", to: session) { return failure }
        Thread.sleep(forTimeInterval: 1.2)
        switch session.backend {
        case .iterm: return ITerm.close(session.id)
        case .tmux:  return Tmux.close(session.id)
        }
    }

    private static func keystroke(_ byte: UInt8, to session: TargetSession) -> String? {
        switch session.backend {
        case .iterm: return ITerm.keystroke(byte, to: session.id)
        case .tmux:  return Tmux.keystroke(byte, to: session.id)
        }
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

    static func reading(of sessions: [TargetSession]) -> Reading {
        var out = Reading()

        func note(_ id: String, _ screen: String?) {
            out.states[id] = SessionState.read(screen)
            // Only when the state says so. `read` has already decided a menu is up — asking the
            // parser again on an idle screen would be work done to be told nothing, once per
            // session per beat, which is the shape of cost this whole file is careful about.
            guard out.states[id] == .waiting, let screen else { return }
            out.menus[id] = SessionState.menu(Ansi.plain(screen))
        }

        let iterm = sessions.filter { $0.backend == .iterm }
        if !iterm.isEmpty {
            let tails = ITerm.tails(ids: iterm.map { $0.id })
            for session in iterm { note(session.id, tails[session.id]) }
        }
        // Only the visible pane: `-S -0` starts at the top of the screen rather than in the
        // scrollback, which is both cheaper and the right question — what is on screen *now*.
        for session in sessions where session.backend == .tmux {
            note(session.id, Tmux.capture(session.id, scrollback: 0))
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
        guard let pid = ITerm.claudePIDs()[bare],
              let path = ITerm.workingDirectory(ofPID: pid) else { return nil }
        cwdLock.lock()
        cwdCache[session.id] = (CFAbsoluteTimeGetCurrent(), path)
        cwdLock.unlock()
        return path
    }

    /// When the Claude Code process in this session started. Used to tell its transcript from
    /// the transcripts of every other session in the same project.
    ///
    /// Remembered for the same reason and the same while as the working directory: it is asked
    /// once per session on every move through the list, it costs a `ps` of its own on top of the
    /// one that finds the pid, and the answer it gives is a fact about a process that has already
    /// started. Held rather than kept forever, because a tab whose session is restarted keeps its
    /// id and gets a new start time.
    private static var startCache: [String: (at: CFAbsoluteTime, started: Date?)] = [:]

    static func processStart(of session: TargetSession) -> Date? {
        cwdLock.lock()
        if let hit = startCache[session.id], CFAbsoluteTimeGetCurrent() - hit.at < 20 {
            defer { cwdLock.unlock() }
            return hit.started
        }
        cwdLock.unlock()

        let bare = session.tty.replacingOccurrences(of: "/dev/", with: "")
        let started = ITerm.claudePIDs()[bare].flatMap { ITerm.processStart(ofPID: $0) }
        cwdLock.lock()
        startCache[session.id] = (CFAbsoluteTimeGetCurrent(), started)
        cwdLock.unlock()
        return started
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
