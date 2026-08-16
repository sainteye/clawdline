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

    /// A short label for display. Claude Code prefixes its title with ✳, which is a useful marker,
    /// so keep it; iTerm appends " (job name)", which helps nobody pick a tab, so drop it.
    var label: String {
        var s = name.trimmingCharacters(in: .whitespacesAndNewlines)
        if s.hasSuffix(")"), let open = s.lastIndex(of: "("), open > s.startIndex {
            let before = s.index(before: open)
            if s[before] == " " { s = String(s[s.startIndex..<before]) }
        }
        s = s.trimmingCharacters(in: .whitespaces)
        return s.isEmpty ? "⌘\(windowIndex + 1)-\(tabIndex + 1)" : s
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

    /// tty → pid for the claude processes. Needed because the working directory is the only
    /// way into the transcript, and only the process knows it.
    static func claudePIDs() -> [String: Int32] {
        var map: [String: Int32] = [:]
        let out = shell("/bin/ps", ["-ax", "-o", "tty=,pid=,command="])
        for line in out.split(separator: "\n") {
            let parts = line.split(separator: " ", omittingEmptySubsequences: true)
            guard parts.count >= 3, parts[0].hasPrefix("ttys"), let pid = Int32(parts[1]) else { continue }
            let head = String(parts[2])
            if head == "claude" || head.hasSuffix("/claude") { map[String(parts[0])] = pid }
        }
        return map
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

    /// What that session currently shows. iTerm2 exposes the visible screen and no more,
    /// so this is a snapshot of the window rather than a transcript.
    static func capture(_ sessionID: String) -> String? {
        let res = osa(["capture", sessionID])
        guard res["ok"] as? Bool == true else { return nil }
        return res["text"] as? String
    }

    /// Bring a session to the front (used by the menu bar).
    static func reveal(_ sessionID: String) {
        _ = osa(["reveal", sessionID])
    }
}
