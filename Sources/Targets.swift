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
        // tmux first: when a pane and its host terminal both appear, the pane is the one that
        // can actually receive text, so it should win the tty.
        for pane in Tmux.panes() where pane.isClaude {
            seenTTYs.insert(pane.tty)
            snap.sessions.append(pane)
        }
        for pane in Tmux.panes() where !pane.isClaude {
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

    static func capture(_ session: TargetSession) -> String? {
        switch session.backend {
        case .iterm: return ITerm.capture(session.id)
        case .tmux:  return Tmux.capture(session.id)
        }
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

    static func reveal(_ session: TargetSession) {
        switch session.backend {
        case .iterm: ITerm.reveal(session.id)
        case .tmux:  Tmux.reveal(session.id)
        }
    }
}
