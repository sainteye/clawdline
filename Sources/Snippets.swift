import Foundation

/// The small, Mac-owned pieces of text a session may place in its composer.
///
/// One JSON file is the source of truth for one snippet. The browser never chooses a project
/// path: ``project(forCwd:registry:gitCommonDirectory:)`` resolves the same project the header's
/// mark represents, including folding an isolated Git worktree back into its checkout.
enum Snippets {
    enum Scope: String {
        case global
        case project
    }

    struct Record {
        let id: String
        var title: String
        var body: String
        var scope: Scope
        var project: String?
        var position: Int
        let createdAt: Int
        var updatedAt: Int

        var json: [String: Any] {
            var value: [String: Any] = [
                "id": id,
                "title": title,
                "body": body,
                "scope": scope.rawValue,
                "position": position,
                "created_at": createdAt,
                "updated_at": updatedAt,
            ]
            if let project { value["project"] = project }
            return value
        }
    }

    struct ProjectScope: Equatable {
        let key: String
        let label: String

        var json: [String: Any] { ["key": key, "label": label] }
    }

    enum Reply {
        case ok([String: Any])
        case refused(status: Int, code: String, message: String, extra: [String: Any])

        static func refused(_ status: Int, _ code: String, _ message: String,
                            extra: [String: Any] = [:]) -> Reply {
            .refused(status: status, code: code, message: message, extra: extra)
        }
    }

    private static let lock = NSLock()
    private static var writeTimes: [Date] = []
    static var directoryOverrideForTesting: URL?
    static var directory: URL {
        directoryOverrideForTesting
            ?? RemoteAuth.directory.appendingPathComponent("snippets", isDirectory: true)
    }

    private static let recordKeys: Set<String> = [
        "id", "title", "body", "scope", "position", "created_at", "updated_at",
    ]
    private static let writableKeys: Set<String> = ["title", "body", "scope", "project"]
    private static let orderKeys: Set<String> = ["scope", "project", "order"]

    // MARK: - Reading

    /// Every valid snippet on this Mac. A hand-written neighbor with a non-UUID filename has no
    /// id and is ignored; malformed UUID-named files are isolated rather than repaired on read.
    static func records() -> [[String: Any]] {
        lock.lock(); defer { lock.unlock() }
        return loadRecords().sorted(by: ordered).map(\.json)
    }

    /// Project rows precede global rows, while each group retains the person's explicit order.
    static func records(for project: ProjectScope) -> [[String: Any]] {
        lock.lock(); defer { lock.unlock() }
        let all = loadRecords()
        let local = all.filter { $0.scope == .project && $0.project == project.key }
            .sorted(by: ordered)
        let global = all.filter { $0.scope == .global }.sorted(by: ordered)
        return (local + global).map(\.json)
    }

    private static func loadRecords() -> [Record] {
        let manager = FileManager.default
        guard let files = try? manager.contentsOfDirectory(
            at: directory, includingPropertiesForKeys: nil,
            options: [.skipsHiddenFiles]) else { return [] }
        return files.compactMap { file in
            guard file.pathExtension == "json" else { return nil }
            let stem = file.deletingPathExtension().lastPathComponent
            guard UUID(uuidString: stem) != nil,
                  let data = try? Data(contentsOf: file),
                  let value = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
            else { return nil }
            return record(from: value, filenameID: stem)
        }
    }

    private static func ordered(_ lhs: Record, _ rhs: Record) -> Bool {
        if lhs.position != rhs.position { return lhs.position < rhs.position }
        if lhs.createdAt != rhs.createdAt { return lhs.createdAt < rhs.createdAt }
        return lhs.id < rhs.id
    }

    // MARK: - Scope resolution

    /// Resolve the project key used by snippets. Tests may supply the icon registry and Git
    /// answer, but production always reads the same registry and runs the same Git question the
    /// session header is based on.
    static func project(
        forCwd cwd: String,
        registry: [String: [String: Any]]? = nil,
        gitCommonDirectory: ((String) -> String?)? = nil
    ) -> ProjectScope {
        let normalizedCwd = normalized(cwd) ?? cwd
        let rows: [String: [String: Any]]
        if let registry {
            rows = registry
        } else {
            rows = Dictionary(uniqueKeysWithValues: ProjectIcon.knownPaths().map { path in
                (path, ProjectIcon.row(forCwd: path) ?? [:])
            })
        }

        // Put the dictionary key into a private copy of each row, then ask ProjectIcon itself to
        // choose. That reuses its exact/prefix and longest-match rule instead of keeping a second
        // almost-identical project matcher here.
        let tagged = Dictionary(uniqueKeysWithValues: rows.map { path, row -> (String, [String: Any]) in
            var copy = row
            copy["__clawdline_snippet_path"] = path
            return (path, copy)
        })
        if let matched = ProjectIcon.entry(forCwd: normalizedCwd, in: tagged),
           let rawPath = matched["__clawdline_snippet_path"] as? String,
           let key = normalized(rawPath) {
            return ProjectScope(key: key, label: projectLabel(row: matched, path: key))
        }

        let common: String?
        if let resolver = gitCommonDirectory { common = resolver(normalizedCwd) }
        else { common = Self.gitCommonDirectory(at: normalizedCwd) }
        if let common, let key = checkoutPath(fromGitCommonDirectory: common) {
            return ProjectScope(key: key, label: pathLabel(key))
        }
        return ProjectScope(key: normalizedCwd, label: pathLabel(normalizedCwd))
    }

    private static func projectLabel(row: [String: Any], path: String) -> String {
        if let label = row["label"] as? String, !label.isEmpty { return label }
        return pathLabel(path)
    }

    private static func pathLabel(_ path: String) -> String {
        let label = (path as NSString).lastPathComponent
        return label.isEmpty ? path : label
    }

    private static func checkoutPath(fromGitCommonDirectory value: String) -> String? {
        guard let common = normalized(value), common.hasSuffix("/.git") else { return nil }
        return String(common.dropLast("/.git".count))
    }

    private static func normalized(_ path: String?) -> String? {
        guard let path else { return nil }
        let trimmed = path.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        return (trimmed as NSString).standardizingPath
    }

    /// `--git-common-dir`, not `--show-toplevel`: in an isolated worktree the latter is the
    /// child's checkout and the former points to the repository it was cut from.
    private static func gitCommonDirectory(at cwd: String) -> String? {
        let task = Process()
        task.executableURL = URL(fileURLWithPath: "/usr/bin/git")
        task.arguments = ["rev-parse", "--path-format=absolute", "--git-common-dir"]
        task.currentDirectoryURL = URL(fileURLWithPath: cwd)
        var environment = ProcessInfo.processInfo.environment
        environment["GIT_OPTIONAL_LOCKS"] = "0"
        task.environment = environment
        let pipe = Pipe()
        task.standardInput = FileHandle.nullDevice
        task.standardOutput = pipe
        task.standardError = FileHandle.nullDevice
        do { try task.run() } catch { return nil }
        let killer = DispatchWorkItem { if task.isRunning { task.terminate() } }
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + 3, execute: killer)
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        task.waitQuietly()
        killer.cancel()
        guard task.terminationStatus == 0 else { return nil }
        return String(data: data, encoding: .utf8)
    }

    // MARK: - Writing

    static func create(from body: [String: Any], now: Date = Date()) -> Reply {
        lock.lock(); defer { lock.unlock() }
        guard Set(body.keys).isSubset(of: writableKeys),
              Set(body.keys).contains("title"), Set(body.keys).contains("body"),
              Set(body.keys).contains("scope") else {
            return refuse("snippet.created", id: nil, status: 400, code: "malformed_snippet",
                          message: "A snippet contains missing or unknown fields.")
        }
        let id = UUID().uuidString.lowercased()
        let existing = loadRecords()
        let stamp = Int(now.timeIntervalSince1970)
        let position = (existing.compactMap { candidate -> Int? in
            guard candidate.scope.rawValue == body["scope"] as? String,
                  candidate.project == normalized(body["project"] as? String) else { return nil }
            return candidate.position
        }.max() ?? 0) + 100
        let candidate: Record
        switch record(fromWritable: body, base: nil, id: id, position: position,
                      createdAt: stamp, updatedAt: stamp) {
        case .refused(let reply):
            audit("snippet.created", id: nil, ok: false, why: reply.code)
            return reply.value
        case .record(let record): candidate = record
        }
        if let limit = limitRefusal(for: candidate, among: existing) {
            audit("snippet.created", id: nil, ok: false, why: "snippet_limit_reached")
            return limit
        }
        guard takeWriteRate(at: now) else {
            return refuse("snippet.created", id: nil, status: 429, code: "rate_limited",
                          message: "This Mac has received ten snippet writes in ten minutes.")
        }
        guard write(candidate, replacing: nil) else {
            return refuse("snippet.created", id: id, status: 500, code: "write_failed",
                          message: "The snippet file could not be written.")
        }
        audit("snippet.created", id: id, ok: true)
        return .ok(candidate.json)
    }

    static func update(id rawID: String, from body: [String: Any], now: Date = Date()) -> Reply {
        lock.lock(); defer { lock.unlock() }
        guard let id = canonicalID(rawID) else {
            return refuse("snippet.updated", id: rawID, status: 404,
                          code: "snippet_not_found",
                          message: "No snippet named that exists on this Mac.")
        }
        guard !body.isEmpty, Set(body.keys).isSubset(of: writableKeys) else {
            return refuse("snippet.updated", id: rawID, status: 400, code: "malformed_snippet",
                          message: "A snippet change contains no fields or an unknown field.")
        }
        let file = directory.appendingPathComponent("\(id).json")
        guard let previous = try? Data(contentsOf: file),
              let object = try? JSONSerialization.jsonObject(with: previous) as? [String: Any],
              let current = record(from: object, filenameID: id) else {
            return refuse("snippet.updated", id: id, status: 404, code: "snippet_not_found",
                          message: "No snippet named that exists on this Mac.")
        }
        var proposed = body
        let requestedScope = (body["scope"] as? String).flatMap(Scope.init(rawValue:))
            ?? current.scope
        if body["title"] == nil { proposed["title"] = current.title }
        if body["body"] == nil { proposed["body"] = current.body }
        if body["scope"] == nil { proposed["scope"] = current.scope.rawValue }
        if body["project"] == nil, requestedScope == .project, let project = current.project {
            proposed["project"] = project
        }
        let changedScope = requestedScope != current.scope ||
            normalized(proposed["project"] as? String) != current.project
        let existing = loadRecords().filter { $0.id != id }
        let position = changedScope
            ? ((existing.filter { $0.scope == requestedScope
                    && $0.project == normalized(proposed["project"] as? String) }
                .map(\.position).max() ?? 0) + 100)
            : current.position
        let candidate: Record
        switch record(fromWritable: proposed, base: current, id: id, position: position,
                      createdAt: current.createdAt,
                      updatedAt: Int(now.timeIntervalSince1970)) {
        case .refused(let reply):
            audit("snippet.updated", id: id, ok: false, why: reply.code)
            return reply.value
        case .record(let record): candidate = record
        }
        if let limit = limitRefusal(for: candidate, among: existing) {
            audit("snippet.updated", id: id, ok: false, why: "snippet_limit_reached")
            return limit
        }
        guard takeWriteRate(at: now) else {
            return refuse("snippet.updated", id: id, status: 429, code: "rate_limited",
                          message: "This Mac has received ten snippet writes in ten minutes.")
        }
        guard write(candidate, replacing: previous) else {
            return refuse("snippet.updated", id: id, status: 500, code: "write_failed",
                          message: "The snippet change could not be written; the previous file remains.")
        }
        audit("snippet.updated", id: id, ok: true)
        return .ok(candidate.json)
    }

    static func delete(id rawID: String, now: Date = Date()) -> Reply {
        lock.lock(); defer { lock.unlock() }
        guard let id = canonicalID(rawID) else {
            return refuse("snippet.deleted", id: rawID, status: 404,
                          code: "snippet_not_found",
                          message: "No snippet named that exists on this Mac.")
        }
        let file = directory.appendingPathComponent("\(id).json")
        guard FileManager.default.fileExists(atPath: file.path) else {
            return refuse("snippet.deleted", id: id, status: 404,
                          code: "snippet_not_found",
                          message: "No snippet named that exists on this Mac.")
        }
        guard takeWriteRate(at: now) else {
            return refuse("snippet.deleted", id: id, status: 429, code: "rate_limited",
                          message: "This Mac has received ten snippet writes in ten minutes.")
        }
        do { try FileManager.default.removeItem(at: file) }
        catch {
            return refuse("snippet.deleted", id: id, status: 500, code: "delete_failed",
                          message: "The snippet file could not be removed.")
        }
        audit("snippet.deleted", id: id, ok: true)
        return .ok(["ok": true, "deleted": id])
    }

    static func reorder(from body: [String: Any], now: Date = Date()) -> Reply {
        lock.lock(); defer { lock.unlock() }
        guard Set(body.keys) == orderKeys.subtracting(body["project"] == nil ? ["project"] : []),
              let rawScope = body["scope"] as? String, let scope = Scope(rawValue: rawScope),
              let rawOrder = body["order"] as? [Any],
              rawOrder.allSatisfy({ $0 is String }) else {
            return refuse("snippet.ordered", id: nil, status: 400, code: "malformed_snippet",
                          message: "A snippet order must name one scope and its complete order.")
        }
        let hasProject = body.keys.contains("project")
        let project = normalized(body["project"] as? String)
        guard (scope == .project && hasProject && project != nil)
                || (scope == .global && !hasProject) else {
            return refuse("snippet.ordered", id: nil, status: 400,
                          code: "snippet_scope_mismatch",
                          message: "Project snippets need one project path; global snippets cannot carry one.")
        }
        let order = rawOrder.compactMap { $0 as? String }.compactMap(canonicalID)
        guard order.count == rawOrder.count, Set(order).count == order.count else {
            return refuse("snippet.ordered", id: nil, status: 400, code: "malformed_snippet",
                          message: "A snippet order must contain distinct snippet ids.")
        }
        let current = loadRecords().filter { $0.scope == scope && $0.project == project }
        guard Set(order) == Set(current.map(\.id)), order.count == current.count else {
            return refuse("snippet.ordered", id: nil, status: 400,
                          code: "snippet_scope_mismatch",
                          message: "That order must contain every snippet in this scope, and no others.")
        }
        guard takeWriteRate(at: now) else {
            return refuse("snippet.ordered", id: nil, status: 429, code: "rate_limited",
                          message: "This Mac has received ten snippet writes in ten minutes.")
        }
        let stamp = Int(now.timeIntervalSince1970)
        var byID = Dictionary(uniqueKeysWithValues: current.map { ($0.id, $0) })
        for (offset, id) in order.enumerated() {
            guard var record = byID[id],
                  let previous = try? Data(contentsOf: directory
                    .appendingPathComponent("\(id).json")) else {
                return refuse("snippet.ordered", id: id, status: 500, code: "write_failed",
                              message: "A snippet changed while its order was being saved.")
            }
            record.position = (offset + 1) * 100
            record.updatedAt = stamp
            guard write(record, replacing: previous) else {
                return refuse("snippet.ordered", id: id, status: 500, code: "write_failed",
                              message: "The snippet order could not be written.")
            }
            byID[id] = record
        }
        audit("snippet.ordered", id: nil, ok: true,
              extra: ["scope": scope.rawValue, "project": project ?? ""])
        var answer: [String: Any] = ["ok": true, "scope": scope.rawValue,
                                     "snippets": order.compactMap { byID[$0]?.json }]
        if let project { answer["project"] = project }
        return .ok(answer)
    }

    // MARK: - Validation and files

    private struct Refusal {
        let status: Int
        let code: String
        let message: String
        let extra: [String: Any]
        var value: Reply { .refused(status: status, code: code, message: message, extra: extra) }
    }

    private enum BuiltRecord {
        case record(Record)
        case refused(Refusal)
    }

    private static func record(fromWritable body: [String: Any], base: Record?, id: String,
                               position: Int, createdAt: Int, updatedAt: Int) -> BuiltRecord {
        guard let title = body["title"] as? String,
              let text = body["body"] as? String,
              let rawScope = body["scope"] as? String,
              let scope = Scope(rawValue: rawScope) else {
            return .refused(Refusal(status: 400, code: "malformed_snippet",
                                    message: "A snippet needs a title, body, and valid scope.", extra: [:]))
        }
        guard !title.isEmpty, !text.isEmpty else {
            return .refused(Refusal(status: 400, code: "malformed_snippet",
                                    message: "A snippet title and body cannot be empty.", extra: [:]))
        }
        guard title.count <= 60, text.count <= 4_000 else {
            return .refused(Refusal(status: 400, code: "snippet_too_long",
                                    message: "A snippet title may contain 60 characters and its body 4000.",
                                    extra: ["title_count": title.count, "body_count": text.count]))
        }
        let hasProject = body.keys.contains("project")
        let project = normalized(body["project"] as? String)
        guard (scope == .project && hasProject && project != nil)
                || (scope == .global && !hasProject) else {
            return .refused(Refusal(status: 400, code: "snippet_scope_mismatch",
                                    message: "Project snippets need one project path; global snippets cannot carry one.",
                                    extra: [:]))
        }
        return .record(Record(id: id, title: title, body: text, scope: scope,
                              project: project, position: position,
                              createdAt: base?.createdAt ?? createdAt, updatedAt: updatedAt))
    }

    private static func record(from object: [String: Any], filenameID: String) -> Record? {
        guard let canonical = canonicalID(filenameID), Set(object.keys) == recordKeys
                .union(object.keys.contains("project") ? ["project"] : []),
              let storedID = object["id"] as? String, canonicalID(storedID) == canonical,
              let title = object["title"] as? String, !title.isEmpty, title.count <= 60,
              let body = object["body"] as? String, !body.isEmpty, body.count <= 4_000,
              let rawScope = object["scope"] as? String, let scope = Scope(rawValue: rawScope),
              let position = integer(object["position"]),
              let createdAt = integer(object["created_at"]),
              let updatedAt = integer(object["updated_at"]) else { return nil }
        let hasProject = object.keys.contains("project")
        let project = normalized(object["project"] as? String)
        guard (scope == .project && hasProject && project != nil)
                || (scope == .global && !hasProject) else { return nil }
        return Record(id: canonical, title: title, body: body, scope: scope,
                      project: project, position: position,
                      createdAt: createdAt, updatedAt: updatedAt)
    }

    private static func integer(_ value: Any?) -> Int? {
        if let value = value as? Int { return value }
        if let value = value as? Double, value.isFinite,
           value.rounded() == value, value >= Double(Int.min), value <= Double(Int.max) {
            return Int(value)
        }
        return nil
    }

    private static func canonicalID(_ raw: String) -> String? {
        UUID(uuidString: raw)?.uuidString.lowercased()
    }

    private static func limitRefusal(for candidate: Record, among others: [Record]) -> Reply? {
        if others.count >= 100 {
            return .refused(409, "snippet_limit_reached",
                            "This Mac already has 100 snippets.",
                            extra: ["count": others.count, "limit": 100])
        }
        let count = others.filter { $0.scope == candidate.scope
            && $0.project == candidate.project }.count
        if count >= 50 {
            return .refused(409, "snippet_limit_reached",
                            "This scope already has 50 snippets.",
                            extra: ["count": count, "limit": 50])
        }
        return nil
    }

    private static func write(_ record: Record, replacing previous: Data?) -> Bool {
        let manager = FileManager.default
        let file = directory.appendingPathComponent("\(record.id).json")
        let staging = directory.appendingPathComponent(".\(record.id).json.new")
        do {
            if !manager.fileExists(atPath: directory.path) {
                try manager.createDirectory(at: directory, withIntermediateDirectories: true,
                                            attributes: [.posixPermissions: 0o700])
            }
            let data = try JSONSerialization.data(withJSONObject: record.json,
                                                  options: [.prettyPrinted, .sortedKeys,
                                                            .withoutEscapingSlashes])
            try data.write(to: staging, options: .atomic)
            try manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: staging.path)
            if previous == nil {
                try manager.moveItem(at: staging, to: file)
            } else {
                _ = try manager.replaceItemAt(file, withItemAt: staging)
                try manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
            }
        } catch {
            try? manager.removeItem(at: staging)
            return false
        }
        guard let data = try? Data(contentsOf: file),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              Self.record(from: object, filenameID: record.id) != nil else {
            if let previous {
                try? previous.write(to: file, options: .atomic)
                try? manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
            } else {
                try? manager.removeItem(at: file)
            }
            return false
        }
        return true
    }

    private static func takeWriteRate(at now: Date) -> Bool {
        writeTimes = writeTimes.filter { now.timeIntervalSince($0) < 600 }
        guard writeTimes.count < 10 else { return false }
        writeTimes.append(now)
        return true
    }

    @discardableResult
    private static func refuse(_ event: String, id: String?, status: Int, code: String,
                               message: String, extra: [String: Any] = [:]) -> Reply {
        audit(event, id: id, ok: false, why: code)
        return .refused(status: status, code: code, message: message, extra: extra)
    }

    private static func audit(_ event: String, id: String?, ok: Bool, why: String? = nil,
                              extra: [String: String] = [:]) {
        var fields = extra
        fields["ok"] = ok ? "1" : "0"
        if let id { fields["snippet"] = id }
        if let why { fields["why"] = why }
        RemoteAuth.audit(event, fields)
    }

    static func resetForTesting(directory newDirectory: URL? = nil) {
        lock.lock()
        writeTimes = []
        directoryOverrideForTesting = newDirectory
        lock.unlock()
    }
}
