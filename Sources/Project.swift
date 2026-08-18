import Foundation

/// Which project a session is sitting in.
///
/// The tab title alone is the task Claude Code is working on — "investigate the webhook" — and
/// two projects can easily be working on tasks that read the same at a glance. The thing that
/// tells them apart is the repository, and a message sent to the wrong one is not recoverable
/// by reading it back.
struct ProjectInfo: Equatable {
    var name: String       // the repository's own folder name, not the session's subfolder
    var branch: String     // empty when detached or not a repository
    var dirty: Int         // files not committed, counting untracked
    /// "owner/repo" on GitHub, which is how claude-bestiary names its workflow-status file.
    var remote: String?
}

enum Project {

    /// Everything in one `git` invocation. Three separate ones per refresh is three processes
    /// for something that sits in a footer.
    static func info(cwd: String) -> ProjectInfo? {
        guard !cwd.isEmpty else { return nil }
        let script = "git -C \(shellQuoted(cwd)) rev-parse --show-toplevel"
            + " && git -C \(shellQuoted(cwd)) remote get-url origin"
            + " ; git -C \(shellQuoted(cwd)) status --porcelain=v2 --branch"
        guard let out = run("/bin/sh", ["-c", script]) else { return nil }
        return parse(out, fallbackPath: cwd)
    }

    /// The pure half, so the shape of git's output can be tested without a repository.
    ///
    /// `--porcelain=v2` is a documented, stable format — unlike the human one, which changes
    /// with git's mood and with the user's language settings.
    static func parse(_ output: String, fallbackPath: String) -> ProjectInfo {
        var name = URL(fileURLWithPath: fallbackPath).lastPathComponent
        var branch = ""
        var dirty = 0
        var remote: String?
        var sawToplevel = false

        for line in output.split(separator: "\n", omittingEmptySubsequences: false) {
            let text = String(line)
            if !sawToplevel, text.hasPrefix("/") {
                name = URL(fileURLWithPath: text).lastPathComponent
                sawToplevel = true
                continue
            }
            if let repo = githubRepo(text) { remote = repo; continue }
            if text.hasPrefix("# branch.head ") {
                let head = String(text.dropFirst("# branch.head ".count))
                branch = head == "(detached)" ? "" : head
                continue
            }
            if text.hasPrefix("# ") { continue }
            // 1/2 are tracked changes, u is a conflict, ? is untracked. All of them are work
            // that is not committed, which is the one question the number answers.
            if let first = text.first, "12u?".contains(first),
               text.count > 1, text.dropFirst().first == " " {
                dirty += 1
            }
        }
        return ProjectInfo(name: name, branch: branch, dirty: dirty, remote: remote)
    }

    /// "owner/repo" out of a remote URL, in either of the two forms git hands back.
    static func githubRepo(_ url: String) -> String? {
        guard url.contains("github.com") else { return nil }
        var tail = url
        if let r = tail.range(of: "github.com") { tail = String(tail[r.upperBound...]) }
        tail = tail.trimmingCharacters(in: CharacterSet(charactersIn: ":/"))
        if tail.hasSuffix(".git") { tail = String(tail.dropLast(4)) }
        let parts = tail.split(separator: "/")
        guard parts.count >= 2 else { return nil }
        return "\(parts[0])-\(parts[1])"
    }

    // MARK: - Plumbing

    /// A path going into a shell command line, the only way that is always right.
    ///
    /// Always quoted, never conditionally — unlike ``Drop/quoted(_:)``, which leaves a plain path
    /// alone so that what is pasted into a prompt reads like something a person typed. Nobody
    /// reads this one, so it can be the boring correct thing every time.
    static func shellQuoted(_ path: String) -> String {
        "'" + path.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    private static func run(_ launch: String, _ args: [String]) -> String? {
        let task = Process()
        task.executableURL = URL(fileURLWithPath: launch)
        task.arguments = args
        let pipe = Pipe()
        task.standardOutput = pipe
        task.standardError = FileHandle.nullDevice
        do { try task.run() } catch { return nil }
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        task.waitUntilExit()
        return String(data: data, encoding: .utf8)
    }
}
