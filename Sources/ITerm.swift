import Foundation

/// Somewhere text can be sent.
struct TargetSession: Equatable, Identifiable {
    let backend: Backend
    let id: String          // iTerm2 session UUID, or tmux pane id
    let name: String        // tab title (Claude Code sets it to the current task)
    let tty: String         // /dev/ttysNNN
    let windowIndex: Int
    let tabIndex: Int
    let isClaude: Bool
    var cwd: String?

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

    // MARK: - Who is running Claude Code

    /// Pull the TTYs whose process name is exactly `claude`.
    /// Matching the whole command line also catches statusline scripts and Claude.app, so only the first token counts.
    private static func claudeTTYs() -> Set<String> {
        parseClaudeTTYs(shell("/bin/ps", ["-ax", "-o", "tty=,command="]))
    }

    /// Split out so it can be tested against fixed `ps` output rather than whatever
    /// happens to be running on the machine at the time.
    static func parseClaudeTTYs(_ psOutput: String) -> Set<String> {
        var set = Set<String>()
        for line in psOutput.split(separator: "\n") {
            let parts = line.split(separator: " ", maxSplits: 1, omittingEmptySubsequences: true)
            guard parts.count == 2 else { continue }
            let tty = String(parts[0])
            guard tty.hasPrefix("ttys") else { continue }
            let cmd = parts[1].trimmingCharacters(in: .whitespaces)
            let head = cmd.split(separator: " ").first.map(String.init) ?? ""
            if head == "claude" || head.hasSuffix("/claude") { set.insert(tty) }
        }
        return set
    }

    private static let pidLock = NSLock()
    private static var pidCache: (at: CFAbsoluteTime, map: [String: Int32])?

    /// tty → pid for the claude processes. Needed because the working directory is the only
    /// way into the transcript, and only the process knows it.
    ///
    /// Held for a couple of seconds. The scan is a full `ps` — 104 ms measured, by far the most
    /// expensive thing on this path — and the pane asks for it once a second while it is open,
    /// which put a tenth of a second of process listing behind every refresh. A session that
    /// starts or dies is picked up on the next expiry, which is what a status display needs.
    static func claudePIDs() -> [String: Int32] {
        pidLock.lock()
        if let c = pidCache, CFAbsoluteTimeGetCurrent() - c.at < 2 {
            defer { pidLock.unlock() }
            return c.map
        }
        pidLock.unlock()

        var map: [String: Int32] = [:]
        let out = shell("/bin/ps", ["-ax", "-o", "tty=,pid=,command="])
        for line in out.split(separator: "\n") {
            let parts = line.split(separator: " ", omittingEmptySubsequences: true)
            guard parts.count >= 3, parts[0].hasPrefix("ttys"), let pid = Int32(parts[1]) else { continue }
            let head = String(parts[2])
            if head == "claude" || head.hasSuffix("/claude") { map[String(parts[0])] = pid }
        }
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

    // MARK: - API

    static func snapshot() -> Targets.Snapshot {
        var snap = Targets.Snapshot()

        let listed = osa(["list"])
        guard listed["ok"] as? Bool == true, let rows = listed["sessions"] as? [[String: Any]] else {
            snap.error = listed["error"] as? String ?? L.t.cannotList
            return snap
        }

        let claude = claudeTTYs()
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
                isClaude: claude.contains(bare)
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

    /// Open a tab and start something in it.
    ///
    /// The one operation that is not "talk to a session that already exists", and the only reason
    /// it is here rather than being left to the person at the keyboard: from a phone there is no
    /// keyboard to go to. Returns the new session's id and tty, or the reason it did not happen.
    static func newTab(cwd: String, command: String) -> (id: String, tty: String)? {
        let res = osa(["newtab", cwd, command])
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
