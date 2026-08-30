import Foundation

/// What is happening to a project right now: a deploy in flight, a backlog, a health check.
///
/// None of it is computed here. These are files that
/// [claude-bestiary](https://github.com/sainteye/claude-bestiary) already writes for the terminal
/// status line, under `~/.claude/statusline-cache/`. Reading them is what keeps the two agreeing,
/// and it means the expensive part — polling GitHub, parsing a backlog, pinging a health
/// endpoint — happens once for both rather than twice.
///
/// Read only, and absent is normal: without claude-bestiary none of these files exist, and the
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

    /// A finite outcome list, distinct from the backlog's unbounded inventory.
    ///
    /// `waitingOnUser` is deliberately its own count. Folding it into incomplete work makes a
    /// release look slow when it is actually waiting for a credential, spend approval or product
    /// decision that no agent is allowed to invent.
    struct Milestone {
        var total: Int
        var complete: Int
        var waitingOnUser: Int
        var artifact: String?

        func linkRow(sessionID: String) -> [String: Any]? {
            guard let artifact, !artifact.isEmpty else { return nil }
            let segmentCharacters = CharacterSet.alphanumerics
                .union(CharacterSet(charactersIn: "-._~"))
            guard let session = sessionID.addingPercentEncoding(
                withAllowedCharacters: segmentCharacters) else { return nil }
            var row: [String: Any] = [
                "label": "milestone",
                "url": "/v1/sessions/\(session)/artifacts/milestone",
                "kind": "artifact",
                "state": total > 0 && complete >= total ? "ok" : "running",
                "status": "\(complete)/\(total)",
                // Same-origin authenticated bytes work through localhost, a tunnel, or the Cloud
                // bridge. This is no longer a `file://` address that only the Mac can open.
                "local": false,
            ]
            if waitingOnUser > 0 { row["why"] = "\(waitingOnUser) waiting on you" }
            return row
        }
    }

    struct Health {
        var label: String
        var state: String        // ok | fail | …
        /// Where the check points. It lives in the registry rather than the cache file: the
        /// endpoint is a fact about the project, the result is a fact about this minute.
        var url: String?
        var kind: String?
        var reason: String?

        /// One row for the browser's Links sheet. The receipt's vocabulary stays in `status`;
        /// the older visual state is only the colour of its dot.
        func linkRow() -> [String: Any]? {
            guard let url, !url.isEmpty else { return nil }
            let visualState: String
            switch state {
            case "online": visualState = "ok"
            case "not_deployed": visualState = "down"
            case "unhealthy", "unreachable": visualState = "fail"
            default: visualState = state
            }
            var row: [String: Any] = [
                "label": label,
                "url": url,
                "kind": kind ?? "site",
                "state": visualState,
                "status": state.replacingOccurrences(of: "_", with: " "),
                "local": false,
            ]
            if state != "online", let reason, !reason.isEmpty {
                row["why"] = reason.replacingOccurrences(of: "_", with: " ")
            }
            return row
        }
    }

    struct Snapshot {
        var deploy: Deploy?
        var backlog: Backlog?
        var milestone: Milestone?
        var health: Health?
        /// The lossless component rows from a multi-surface producer. Older receipts have none,
        /// so `health` remains the compatible overall chip used by the Mac footer.
        var healthComponents: [Health] = []
        var isEmpty: Bool {
            deploy == nil && backlog == nil && milestone == nil
                && health == nil && healthComponents.isEmpty
        }
    }

    /// Where the status files live. `status_dir` in the config overrides it.
    ///
    /// The default is where claude-bestiary puts them, because that is what most people reading
    /// this will already have. It is a default and not the location: the format is documented
    /// in docs/project-status.md precisely so that anything can write these — a cron job, a git
    /// hook, a script of your own — and a producer that is not claude-bestiary should not have to
    /// pretend to be it just to be found.
    static var cacheDirectory: URL {
        Paths.resolve(Config.shared.statusDir)
            ?? FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent(".claude/statusline-cache")
    }

    /// claude-bestiary keys the per-project files by path with the separators turned into dashes,
    /// the same shape Claude Code uses for its own project folders.
    static func key(forPath path: String) -> String {
        path.replacingOccurrences(of: "/", with: "-")
    }

    static func read(cwd: String, remote: String?, registry: [String: Any]? = nil) -> Snapshot {
        var out = Snapshot()
        let dir = cacheDirectory
        if let repo = remote {
            out.deploy = deploy(json(dir.appendingPathComponent("ghrun-\(repo).json")))
        }
        let stem = key(forPath: cwd)
        out.backlog = backlog(json(dir.appendingPathComponent("backlog-\(stem).json")))
        out.milestone = milestone(json(dir.appendingPathComponent("milestone-\(stem).json")))
        let healthRegistry = registry?["health"] as? [String: Any] ?? registry
        let healthRow = json(dir.appendingPathComponent("health-\(stem).json"))
        out.health = health(healthRow, registry: healthRegistry)
        out.healthComponents = healthComponents(healthRow)
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

    static func milestone(_ row: [String: Any]?) -> Milestone? {
        guard let row,
              let total = row["total"] as? Int,
              let complete = row["complete"] as? Int,
              total >= 0, complete >= 0, complete <= total else { return nil }
        return Milestone(total: total,
                         complete: complete,
                         waitingOnUser: max(0, row["waiting_on_user"] as? Int ?? 0),
                         artifact: row["artifact"] as? String)
    }

    static func health(_ row: [String: Any]?, registry: [String: Any]? = nil) -> Health? {
        guard let row, let state = row["state"] as? String else { return nil }
        return Health(label: row["label"] as? String
                        ?? registry?["label"] as? String ?? "health",
                      state: state,
                      url: row["url"] as? String ?? registry?["url"] as? String,
                      kind: row["kind"] as? String,
                      reason: row["reason"] as? String)
    }

    static func healthComponents(_ row: [String: Any]?) -> [Health] {
        guard let components = row?["components"] as? [[String: Any]] else { return [] }
        return components.compactMap { health($0, registry: $0) }
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
