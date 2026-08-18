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

    /// Start a session somewhere, and hand back the one it became.
    ///
    /// iTerm2 when it is running, tmux otherwise — the same order everything else here uses, and
    /// for the same reason: a tab you can see beats a pane you have to go and find.
    ///
    /// The new session is not in any snapshot yet, so the caller gets an id and has to wait for
    /// the next reading like everybody else. Returning something half-filled here would be a
    /// third kind of `TargetSession` that is true for about a second.
    static func create(cwd: String, command: String) -> (id: String, backend: Backend)? {
        if let made = ITerm.newTab(cwd: cwd, command: command) { return (made.id, .iterm) }
        if let pane = Tmux.newWindow(cwd: cwd, command: command) { return (pane, .tmux) }
        return nil
    }

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
        var out: [String: SessionState] = [:]

        let iterm = sessions.filter { $0.backend == .iterm }
        if !iterm.isEmpty {
            let tails = ITerm.tails(ids: iterm.map { $0.id })
            for session in iterm { out[session.id] = SessionState.read(tails[session.id]) }
        }
        // Only the visible pane: `-S -0` starts at the top of the screen rather than in the
        // scrollback, which is both cheaper and the right question — what is on screen *now*.
        for session in sessions where session.backend == .tmux {
            out[session.id] = SessionState.read(Tmux.capture(session.id, scrollback: 0))
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
