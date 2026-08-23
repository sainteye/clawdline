import Foundation

/// Somewhere text can be sent.
struct TargetSession: Equatable, Identifiable {
    let backend: Backend
    let id: String          // iTerm2 session UUID, or tmux pane id
    let name: String        // tab title (Claude Code sets it to the current task)
    let tty: String         // /dev/ttysNNN
    let windowIndex: Int
    let tabIndex: Int
    /// Which assistant is running here, or nothing when it is an ordinary shell.
    ///
    /// This was `isClaude`, a boolean, for as long as there was only one thing it could be
    /// about. It is still asked as one — see ``isAssistant`` — everywhere the question is
    /// "can I send work to this", because that answer has not changed; what changed is that
    /// how to read its screen, where to find its record and what word ends it are now three
    /// answers rather than three assumptions. See ``Assistant``.
    let assistant: Assistant?
    var cwd: String?

    /// Somewhere work can be sent, as opposed to a shell somebody left open.
    var isAssistant: Bool { assistant != nil }

    /// Kept because Claude Code genuinely is a special case in two places — the Ctrl-V paste
    /// that turns a clipboard image into `[Image #3]`, and the transcripts under `~/.claude`.
    /// Everywhere else that used to ask this wanted ``isAssistant`` and now says so.
    var isClaude: Bool { assistant == .claude }

    /// A short label for display: the task, and only the task.
    ///
    /// Two things get taken off. iTerm appends " (job name)", which helps nobody pick a tab. And
    /// Claude Code puts a status glyph on the front — which used to be a fixed ✳ and was worth
    /// keeping as a marker, and is now **a frame of an animation**: 2.1.228 cycles half circles
    /// through the title, so the same tab reads `◐ …`, `◑ …`, `◒ …` one after another. A label
    /// that changes four times a second is not a label, it is noise on every surface that draws
    /// one — and the thing it was standing in for is now answered properly by ``SessionState``.
    var label: String {
        var s = name.trimmingCharacters(in: .whitespacesAndNewlines)
        if s.hasSuffix(")"), let open = s.lastIndex(of: "("), open > s.startIndex {
            let before = s.index(before: open)
            if s[before] == " " { s = String(s[s.startIndex..<before]) }
        }
        s = TargetSession.withoutStatusGlyph(s)
        s = s.trimmingCharacters(in: .whitespaces)
        return s.isEmpty ? "⌘\(windowIndex + 1)-\(tabIndex + 1)" : s
    }

    /// Strip a leading status glyph and the space after it.
    ///
    /// Recognised by *shape* rather than by a list of characters, for the same reason everything
    /// else here is: the glyphs changed once already and the list would have to be chased. What
    /// does not change is that it is one non-alphanumeric mark, on its own, in front of a title —
    /// so a single leading character from the symbol, punctuation or Braille blocks goes, and one
    /// that is followed by anything other than a space stays, because that is a title starting
    /// with a bullet rather than a marker in front of one.
    static func withoutStatusGlyph(_ title: String) -> String {
        var chars = Array(title)
        guard chars.count > 2, chars[1] == " " else { return title }
        guard let scalar = chars[0].unicodeScalars.first, chars[0].unicodeScalars.count == 1,
              !chars[0].isLetter, !chars[0].isNumber else { return title }
        // Braille (⠀–⣿), geometric shapes (◀–◿), dingbats and misc symbols — the blocks a
        // terminal spinner is drawn from. A quotation mark or a bracket is not one of them.
        let v = scalar.value
        let isMarker = (0x2800...0x28FF).contains(v)   // Braille
            || (0x25A0...0x25FF).contains(v)           // geometric shapes, incl. ◐◑◒◓
            || (0x2600...0x27BF).contains(v)           // misc symbols and dingbats, incl. ✳
            || (0x2B00...0x2BFF).contains(v)           // misc symbols and arrows
            || (0x1F300...0x1FAFF).contains(v)         // emoji
        guard isMarker else { return title }
        chars.removeFirst(2)
        return String(chars)
    }
}

enum ITerm {

    // MARK: - Subprocesses

    private static func shell(_ path: String, _ args: [String]) -> String {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: path)
        p.arguments = args
        let out = Pipe()
        p.standardOutput = out
        p.standardError = Pipe()
        do { try p.run() } catch { return "" }
        let data = out.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        return String(data: data, encoding: .utf8) ?? ""
    }

    private static var scriptPath: String? {
        Bundle.main.url(forResource: "iterm", withExtension: "js")?.path
    }

    @discardableResult
    private static func osa(_ args: [String]) -> [String: Any] {
        guard let script = scriptPath else {
            return ["ok": false, "error": L.t.scriptMissing]
        }
        let raw = shell("/usr/bin/osascript", ["-l", "JavaScript", script] + args)
        guard let data = raw.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
            return ["ok": false, "error": trimmed.isEmpty ? L.t.itermSilent : trimmed]
        }
        return obj
    }

    // MARK: - Who is running an assistant

    private static let pidLock = NSLock()
    private static var pidCache: (at: CFAbsoluteTime, map: [String: Assistant.Running])?

    /// tty → what is running on it. The one process listing everything else on this path shares.
    ///
    /// Needed because the working directory is the only way into a session's record, and only
    /// the process knows it. The parsing is ``Assistant/reading(ofPS:)``, which is where the
    /// mistakes live and where the tests are.
    ///
    /// Held for a couple of seconds. The scan is a full `ps` — 104 ms measured, by far the most
    /// expensive thing on this path — and the pane asks for it once a second while it is open,
    /// which put a tenth of a second of process listing behind every refresh. A session that
    /// starts or dies is picked up on the next expiry, which is what a status display needs.
    static func assistantPIDs() -> [String: Assistant.Running] {
        pidLock.lock()
        if let c = pidCache, CFAbsoluteTimeGetCurrent() - c.at < 2 {
            defer { pidLock.unlock() }
            return c.map
        }
        pidLock.unlock()

        let map = Assistant.reading(ofPS: shell("/bin/ps", ["-ax", "-o", "tty=,pid=,command="]))
        pidLock.lock()
        pidCache = (CFAbsoluteTimeGetCurrent(), map)
        pidLock.unlock()
        return map
    }

    /// When a process started, from how long it has been running.
    ///
    /// `etime` rather than `lstart`: the latter is a formatted date that changes with the
    /// machine's locale, and parsing a localised date to find a file is a way to work on your
    /// machine and nowhere else.
    static func processStart(ofPID pid: Int32) -> Date? {
        let out = shell("/bin/ps", ["-o", "etime=", "-p", "\(pid)"])
        guard let seconds = parseElapsed(out) else { return nil }
        return Date(timeIntervalSinceNow: -seconds)
    }

    /// `[[dd-]hh:]mm:ss` → seconds.
    static func parseElapsed(_ text: String) -> TimeInterval? {
        var body = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !body.isEmpty else { return nil }
        var days = 0.0
        if let dash = body.firstIndex(of: "-") {
            days = Double(body[body.startIndex..<dash]) ?? 0
            body = String(body[body.index(after: dash)...])
        }
        let parts = body.split(separator: ":").map { Double($0) ?? 0 }
        guard !parts.isEmpty, parts.count <= 3 else { return nil }
        let padded = Array(repeating: 0.0, count: 3 - parts.count) + parts
        return days * 86_400 + padded[0] * 3_600 + padded[1] * 60 + padded[2]
    }

    /// `lsof` rather than the PWD in the environment: an environment variable is whatever it
    /// was at launch, and it splits on spaces, which paths are allowed to contain.
    static func workingDirectory(ofPID pid: Int32) -> String? {
        let out = shell("/usr/sbin/lsof", ["-a", "-p", "\(pid)", "-d", "cwd", "-Fn"])
        for line in out.split(separator: "\n") where line.hasPrefix("n/") {
            return String(line.dropFirst())
        }
        return nil
    }

    /// Every file that process has open, by path.
    ///
    /// The same tool and the same argument as the working directory above, without the `-d cwd`
    /// that narrows it to one. It exists because **a Codex session holds its own rollout open**,
    /// which turns "which of these files belongs to that session" from a guess about clocks into
    /// a fact about a file descriptor — see ``Codex/locate(cwd:startedAt:pid:days:)``.
    ///
    /// `-Fn` so the answer is one path per line with an `n` in front of it, rather than a table
    /// whose columns a path with a space in it walks straight through.
    static func openFiles(ofPID pid: Int32) -> [String] {
        shell("/usr/sbin/lsof", ["-p", "\(pid)", "-Fn"])
            .split(separator: "\n")
            .filter { $0.hasPrefix("n/") }
            .map { String($0.dropFirst()) }
    }

    // MARK: - API

    static func snapshot() -> Targets.Snapshot {
        var snap = Targets.Snapshot()

        let listed = osa(["list"])
        guard listed["ok"] as? Bool == true, let rows = listed["sessions"] as? [[String: Any]] else {
            snap.error = listed["error"] as? String ?? L.t.cannotList
            return snap
        }

        let running = assistantPIDs()
        snap.sessions = rows.map { row in
            let tty = row["tty"] as? String ?? ""
            let bare = tty.replacingOccurrences(of: "/dev/", with: "")
            return TargetSession(
                backend: .iterm,
                id: (row["id"] as? String ?? "").uppercased(),
                name: row["name"] as? String ?? "",
                tty: tty,
                windowIndex: row["win"] as? Int ?? 0,
                tabIndex: row["tab"] as? Int ?? 0,
                assistant: running[bare]?.assistant
            )
        }

        let cur = osa(["current"])
        if cur["ok"] as? Bool == true { snap.currentID = (cur["id"] as? String)?.uppercased() }

        return snap
    }

    /// Send text to a session. nil means it worked; anything else is the reason it did not.
    static func send(_ text: String, to sessionID: String, submit: Bool = true) -> String? {
        let res = osa(["send", sessionID, text, submit ? "1" : "0"])
        if res["ok"] as? Bool == true { return nil }
        return res["error"] as? String ?? L.t.sendFailed
    }

    /// One byte, as a keypress rather than as text — see the `key` command in iterm.js.
    /// Close a session's tab. See the `close` command in `iterm.js` for why it is the session
    /// and not the tab.
    static func close(_ sessionID: String) -> String? {
        let res = osa(["close", sessionID])
        if res["ok"] as? Bool == true { return nil }
        return res["error"] as? String ?? "that session is gone"
    }

    static func keystroke(_ byte: UInt8, to sessionID: String) -> String? {
        let res = osa(["key", sessionID, String(byte)])
        if res["ok"] as? Bool == true { return nil }
        return res["error"] as? String ?? L.t.sendFailed
    }

    static func submit(_ sessionID: String) -> String? { keystroke(13, to: sessionID) }

    /// What that session currently shows. iTerm2 exposes the visible screen and no more,
    /// so this is a snapshot of the window rather than a transcript.
    static func capture(_ sessionID: String) -> String? {
        let res = osa(["capture", sessionID])
        guard res["ok"] as? Bool == true else { return nil }
        return res["text"] as? String
    }

    /// The tail of several sessions' screens, keyed by session id, in one round trip.
    ///
    /// Asking `capture` per session would be a process and an Apple event bridge each, once a
    /// second, for as long as the list is open. The tail is enough because everything read off a
    /// screen here — the live line, a menu waiting for an answer — is drawn at the bottom of it.
    static func tails(ids: [String], lines: Int = 60) -> [String: String] {
        guard !ids.isEmpty else { return [:] }
        let res = osa(["tails", ids.joined(separator: ","), String(lines)])
        guard res["ok"] as? Bool == true, let rows = res["tails"] as? [String: String] else {
            return [:]
        }
        var out: [String: String] = [:]
        for (id, text) in rows { out[id.uppercased()] = text }
        return out
    }

    /// Open a tab and type one line into it.
    ///
    /// The one operation that is not "talk to a session that already exists", and the only reason
    /// it is here rather than being left to the person at the keyboard: from a phone there is no
    /// keyboard to go to. Returns the new session's id and tty, or nil if it did not happen.
    ///
    /// **The line arrives built and quoted.** It used to arrive as a directory and a command and
    /// be assembled inside `iterm.js`, which meant the string that actually ran only existed on
    /// the far side of an `osascript` call — so the quoting could only be exercised by executing
    /// it, which is the one thing a test must not do. It is built by
    /// ``StartPoints/itermLine(cwd:)`` now, where it is an ordinary value.
    ///
    /// Nothing here brings iTerm2 forward: the tab is made and written into, and the app stays
    /// wherever it was. Whoever asked for this is not at the keyboard.
    static func newTab(line: String) -> (id: String, tty: String)? {
        let res = osa(["newtab", line])
        guard res["ok"] as? Bool == true,
              let id = res["id"] as? String, !id.isEmpty else { return nil }
        return (id, res["tty"] as? String ?? "")
    }

    /// Select a session's window and tab.
    ///
    /// `activate: false` stops short of bringing iTerm2 forward, which is what the prompt bar
    /// wants while it is open: the tab underneath should follow the target you are pointing at,
    /// but the keyboard has to stay in the box you are typing into.
    static func reveal(_ sessionID: String, activate: Bool = true) {
        _ = osa(["reveal", sessionID, activate ? "1" : "0"])
    }
}
