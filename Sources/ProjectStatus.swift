import Foundation

/// What is happening to a project right now: a deploy in flight, a backlog, a health check.
///
/// None of it is computed here. These are files that
/// [claude-tools](https://github.com/sainteye/claude-tools) already writes for the terminal
/// status line, under `~/.claude/statusline-cache/`. Reading them is what keeps the two agreeing,
/// and it means the expensive part — polling GitHub, parsing a backlog, pinging a health
/// endpoint — happens once for both rather than twice.
///
/// Read only, and absent is normal: without claude-tools none of these files exist, and the
/// footer simply has less to say.
enum ProjectStatus {

    struct Deploy {
        var label: String        // "deploy"
        var state: String        // running | ok | fail
        var startedAt: Double
        var typicalSeconds: Double
        var url: String?
        /// 0…1 while running, by elapsed time against how long this workflow usually takes.
        func progress(now: Double) -> Double {
            guard typicalSeconds > 0 else { return 0 }
            return min(1, max(0, (now - startedAt) / typicalSeconds))
        }
        func elapsed(now: Double) -> Int { max(0, Int(now - startedAt)) }
    }

    struct Backlog {
        var total: Int
        var now: Int             // the lane that is asking for attention
        var artifact: String?    // a local file to open
    }

    struct Health {
        var label: String
        var state: String        // ok | fail | …
    }

    struct Snapshot {
        var deploy: Deploy?
        var backlog: Backlog?
        var health: Health?
        var isEmpty: Bool { deploy == nil && backlog == nil && health == nil }
    }

    static var cacheDirectory: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".claude/statusline-cache")
    }

    /// claude-tools keys the per-project files by path with the separators turned into dashes,
    /// the same shape Claude Code uses for its own project folders.
    static func key(forPath path: String) -> String {
        path.replacingOccurrences(of: "/", with: "-")
    }

    static func read(cwd: String, remote: String?) -> Snapshot {
        var out = Snapshot()
        let dir = cacheDirectory
        if let repo = remote {
            out.deploy = deploy(json(dir.appendingPathComponent("ghrun-\(repo).json")))
        }
        let stem = key(forPath: cwd)
        out.backlog = backlog(json(dir.appendingPathComponent("backlog-\(stem).json")))
        out.health = health(json(dir.appendingPathComponent("health-\(stem).json")))
        return out
    }

    // MARK: - Parsing
    //
    // Every field is optional on the way in. These files belong to another program and their
    // shape can change; a footer that throws away the whole line because one key moved is worse
    // than one that shows the parts it still recognises.

    static func json(_ url: URL) -> [String: Any]? {
        guard let data = try? Data(contentsOf: url) else { return nil }
        return (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
    }

    static func deploy(_ row: [String: Any]?) -> Deploy? {
        guard let row, let state = row["state"] as? String else { return nil }
        return Deploy(label: row["label"] as? String ?? "deploy",
                      state: state,
                      startedAt: row["started_at"] as? Double ?? 0,
                      typicalSeconds: row["typical_seconds"] as? Double ?? 0,
                      url: row["url"] as? String)
    }

    static func backlog(_ row: [String: Any]?) -> Backlog? {
        guard let row, let total = row["total"] as? Int else { return nil }
        let lanes = row["lanes"] as? [String: Any]
        return Backlog(total: total,
                       now: lanes?["now"] as? Int ?? 0,
                       artifact: row["artifact"] as? String)
    }

    static func health(_ row: [String: Any]?) -> Health? {
        guard let row, let state = row["state"] as? String else { return nil }
        return Health(label: row["label"] as? String ?? "health", state: state)
    }

    // MARK: - Display

    /// "3m 20s", the way a wait is read out loud.
    static func duration(_ seconds: Int) -> String {
        if seconds < 60 { return "\(seconds)s" }
        let m = seconds / 60, s = seconds % 60
        if m < 60 { return s == 0 ? "\(m)m" : "\(m)m \(s)s" }
        return "\(m / 60)h \(m % 60)m"
    }

    /// A bar drawn out of block characters, so it lines up in any font and costs no layout.
    static func bar(_ progress: Double, width: Int = 8) -> String {
        let filled = max(0, min(width, Int((progress * Double(width)).rounded())))
        return String(repeating: "▰", count: filled) + String(repeating: "▱", count: width - filled)
    }
}
