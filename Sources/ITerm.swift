import Foundation

/// Somewhere text can be sent.
struct TargetSession: Equatable, Identifiable {
    let id: String          // iTerm2's session UUID
    let name: String        // tab title (Claude Code sets it to the current task)
    let tty: String         // /dev/ttysNNN
    let windowIndex: Int
    let tabIndex: Int
    let isClaude: Bool

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
        var set = Set<String>()
        for line in shell("/bin/ps", ["-ax", "-o", "tty=,command="]).split(separator: "\n") {
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

    // MARK: - API

    struct Snapshot {
        var sessions: [TargetSession] = []
        var currentID: String?
        var error: String?

        var claudeSessions: [TargetSession] { sessions.filter { $0.isClaude } }
    }

    static func snapshot() -> Snapshot {
        var snap = Snapshot()

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

    /// Bring a session to the front (used by the menu bar).
    static func reveal(_ sessionID: String) {
        _ = osa(["reveal", sessionID])
    }
}
