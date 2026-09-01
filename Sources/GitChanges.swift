import Foundation

/// The part of `git status` the web page needs, kept separate from the process that obtains it.
/// Porcelain v2 is deliberately parsed here rather than scattered through the HTTP route: Git's
/// fixed fields and rename separator are where a small indexing mistake becomes the wrong file.
enum GitChanges {
    enum Kind: String, Equatable {
        case modified, added, deleted, renamed, untracked, conflict
    }

    struct File: Equatable {
        var path: String
        var from: String?
        var staged: Bool
        var unstaged: Bool
        var kind: Kind
        var additions: Int?
        var deletions: Int?
    }

    struct Status: Equatable {
        var branch = ""
        var head = ""
        var ahead = 0
        var behind = 0
        var files: [File] = []
    }

    struct Numstat: Equatable {
        var additions: Int?
        var deletions: Int?
    }

    enum ReadResult {
        case snapshot(Status)
        case notRepository
        case failed
    }

    /// Parse `git status --porcelain=v2 --branch`.
    static func parseStatus(_ text: String) -> Status {
        var out = Status()
        for raw in text.split(separator: "\n", omittingEmptySubsequences: false) {
            let line = String(raw)
            if line.hasPrefix("# branch.oid ") {
                let value = String(line.dropFirst("# branch.oid ".count))
                out.head = value == "(initial)" ? "" : value
            } else if line.hasPrefix("# branch.head ") {
                out.branch = String(line.dropFirst("# branch.head ".count))
            } else if line.hasPrefix("# branch.ab ") {
                let words = line.dropFirst("# branch.ab ".count).split(separator: " ")
                for word in words {
                    if word.hasPrefix("+") { out.ahead = Int(word.dropFirst()) ?? 0 }
                    if word.hasPrefix("-") { out.behind = Int(word.dropFirst()) ?? 0 }
                }
            } else if line.hasPrefix("1 ") {
                // `1 XY sub mH mI mW hH hI path`: only the path is allowed to contain spaces.
                let fields = line.split(separator: " ", maxSplits: 8,
                                        omittingEmptySubsequences: true)
                guard fields.count == 9 else { continue }
                let xy = String(fields[1])
                out.files.append(file(path: String(fields[8]), from: nil, xy: xy,
                                      kind: kind(for: xy)))
            } else if line.hasPrefix("2 ") {
                // A rename's last field is `new path<TAB>old path`; spaces in either path are
                // ordinary path characters, which is why the split stops before that field.
                let fields = line.split(separator: " ", maxSplits: 9,
                                        omittingEmptySubsequences: true)
                guard fields.count == 10 else { continue }
                let paths = fields[9].split(separator: "\t", maxSplits: 1,
                                            omittingEmptySubsequences: false)
                guard paths.count == 2 else { continue }
                out.files.append(file(path: String(paths[0]), from: String(paths[1]),
                                      xy: String(fields[1]), kind: .renamed))
            } else if line.hasPrefix("u ") {
                // `u XY sub m1 m2 m3 mW h1 h2 h3 path`.
                let fields = line.split(separator: " ", maxSplits: 10,
                                        omittingEmptySubsequences: true)
                guard fields.count == 11 else { continue }
                out.files.append(file(path: String(fields[10]), from: nil,
                                      xy: String(fields[1]), kind: .conflict))
            } else if line.hasPrefix("? ") {
                out.files.append(File(path: String(line.dropFirst(2)), from: nil,
                                      staged: false, unstaged: true, kind: .untracked,
                                      additions: nil, deletions: nil))
            }
        }
        return out
    }

    /// Parse `git diff --numstat`. A dash is Git's answer for a binary side of a diff; once one
    /// side is unknowable, both figures stay null rather than presenting half a measurement.
    static func parseNumstat(_ text: String) -> [String: Numstat] {
        var out: [String: Numstat] = [:]
        for raw in text.split(separator: "\n", omittingEmptySubsequences: true) {
            let fields = raw.split(separator: "\t", maxSplits: 2,
                                   omittingEmptySubsequences: false)
            guard fields.count == 3 else { continue }
            let additions = Int(fields[0])
            let deletions = Int(fields[1])
            let path = renamedDestination(String(fields[2]))
            let next = Numstat(additions: additions, deletions: deletions)
            out[path] = merge(out[path], next)
        }
        return out
    }

    /// Join the two numstat views onto the status rows. This is pure as well: tests can pin a
    /// partially staged file without making and mutating a repository on disk.
    static func assemble(status text: String, unstaged: String, staged: String) -> Status {
        var out = parseStatus(text)
        let worktree = parseNumstat(unstaged)
        let index = parseNumstat(staged)
        for i in out.files.indices {
            let path = out.files[i].path
            let counts = merge(worktree[path], index[path])
            out.files[i].additions = counts?.additions
            out.files[i].deletions = counts?.deletions
        }
        return out
    }

    static func payload(_ snapshot: Status) -> [String: Any] {
        ["git": [
            "branch": snapshot.branch,
            "head": snapshot.head,
            "ahead": snapshot.ahead,
            "behind": snapshot.behind,
            "clean": snapshot.files.isEmpty,
            "files": snapshot.files.map { file -> [String: Any] in
                ["path": file.path,
                 "from": file.from ?? NSNull(),
                 "staged": file.staged,
                 "unstaged": file.unstaged,
                 "kind": file.kind.rawValue,
                 "additions": file.additions ?? NSNull(),
                 "deletions": file.deletions ?? NSNull()]
            },
        ]]
    }

    /// Read only when somebody asks for the panel. All commands are lock-free and bounded: this
    /// endpoint must not leave an index lock behind or pin the server queue on a wedged checkout.
    static func read(cwd: String) -> ReadResult {
        guard let status = run(["status", "--porcelain=v2", "--branch"], cwd: cwd) else {
            return .notRepository
        }
        guard let unstaged = run(["diff", "--numstat"], cwd: cwd),
              let staged = run(["diff", "--cached", "--numstat"], cwd: cwd) else {
            return .failed
        }
        return .snapshot(assemble(status: status, unstaged: unstaged, staged: staged))
    }

    private static func file(path: String, from: String?, xy: String, kind: Kind) -> File {
        let states = Array(xy)
        let x = states.indices.contains(0) ? states[0] : "."
        let y = states.indices.contains(1) ? states[1] : "."
        return File(path: path, from: from, staged: x != ".", unstaged: y != ".",
                    kind: kind, additions: nil, deletions: nil)
    }

    private static func kind(for xy: String) -> Kind {
        if xy.contains("U") { return .conflict }
        if xy.contains("R") || xy.contains("C") { return .renamed }
        if xy.contains("A") { return .added }
        if xy.contains("D") { return .deleted }
        return .modified
    }

    private static func merge(_ first: Numstat?, _ second: Numstat?) -> Numstat? {
        guard let first else { return second }
        guard let second else { return first }
        guard let a = first.additions, let b = second.additions,
              let d = first.deletions, let e = second.deletions else {
            return Numstat(additions: nil, deletions: nil)
        }
        return Numstat(additions: a + b, deletions: d + e)
    }

    /// Numstat abbreviates renames as either `old => new` or `dir/{old => new}.ext`; status
    /// names the destination, so expand only that side before joining the two answers.
    private static func renamedDestination(_ path: String) -> String {
        guard let arrow = path.range(of: " => ") else { return path }
        let before = path[..<arrow.lowerBound]
        let after = path[arrow.upperBound...]
        if let open = before.lastIndex(of: "{"), let close = after.firstIndex(of: "}") {
            return String(before[..<open] + after[..<close] + after[after.index(after: close)...])
        }
        return String(after)
    }

    private static func run(_ arguments: [String], cwd: String,
                            timeout: TimeInterval = 5) -> String? {
        let task = Process()
        task.executableURL = URL(fileURLWithPath: "/usr/bin/git")
        task.arguments = arguments
        task.currentDirectoryURL = URL(fileURLWithPath: cwd)
        var environment = ProcessInfo.processInfo.environment
        environment["GIT_OPTIONAL_LOCKS"] = "0"
        // Read in one language, for the reason spelled out in ``ITerm/shell(_:_:timeout:)``: this
        // output is parsed. Git ships translations, and a Mac with them installed would rename
        // every word these readers match on. The porcelain formats used here are already stable
        // by contract; this makes that true of the whole invocation rather than of the flags
        // somebody remembered to pass.
        environment["LC_ALL"] = "C"
        environment["LANG"] = "C"
        task.environment = environment
        let pipe = Pipe()
        task.standardInput = FileHandle.nullDevice
        task.standardOutput = pipe
        task.standardError = FileHandle.nullDevice

        do { try task.run() } catch { return nil }
        let killer = DispatchWorkItem { if task.isRunning { task.terminate() } }
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + timeout,
                                                       execute: killer)
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        task.waitQuietly()
        killer.cancel()
        guard task.terminationStatus == 0 else { return nil }
        return String(data: data, encoding: .utf8)
    }
}
