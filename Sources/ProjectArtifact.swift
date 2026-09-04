import Foundation

extension RemoteServer {
    /// The addresses a project has, in the order somebody would want them.
    ///
    /// Nothing here is invented: every one of these is a URL some other tool already put in a
    /// file this app reads. Clawdline's contribution is that they are one list on a phone rather
    /// than separate facts on a Mac in another room.
    func linksPayload(cwd: String, sessionID: String) -> [[String: Any]] {
        var out: [[String: Any]] = []
        let registry = ProjectIcon.row(forCwd: cwd)
        let status = ProjectStatus.read(cwd: cwd, remote: Project.info(cwd: cwd)?.remote,
                                        registry: registry?["health"] as? [String: Any])

        let healthRows = status.healthComponents.isEmpty
            ? status.health.map { [$0] } ?? []
            : status.healthComponents
        out.append(contentsOf: healthRows.compactMap { $0.linkRow() })
        if let deploy = status.deploy, let url = deploy.url, !url.isEmpty {
            var row: [String: Any] = ["label": deploy.label, "url": url, "kind": "deploy",
                                      "state": deploy.state, "local": false]
            if deploy.state == "running" {
                row["startedAt"] = deploy.startedAt
                row["typicalSeconds"] = deploy.typicalSeconds
            }
            out.append(row)
        }
        let stack = DevStack.find(fromCwd: cwd).flatMap { spec in
            DevStack.isTrusted(spec) ? DevStack.read(spec) : nil
        }
        for process in stack?.processes ?? [] {
            let url = process.url ?? process.port.map { "http://127.0.0.1:\($0)" }
            guard let url, !url.isEmpty else { continue }
            let why = process.isUp ? nil : stack?.why(process)
            out.append(["label": process.name, "url": url, "kind": "server",
                        "state": process.isUp ? "ok" : "down", "local": true,
                        "why": why ?? ""])
        }
        if let backlog = status.backlog, let file = backlog.artifact, !file.isEmpty {
            out.append(["label": "backlog",
                        "url": "/v1/sessions/\(sessionID)/artifacts/backlog",
                        "kind": "artifact", "state": "", "local": false])
        }
        if let milestone = status.milestone?.linkRow(sessionID: sessionID) { out.append(milestone) }
        return out
    }

    /// Serve one path already selected by a typed project-status slot.
    ///
    /// The file must resolve to a regular `.html` file inside `cwd`, may not escape through a
    /// symlink, and is capped before it is loaded. HTML is useful for a visual checklist; script is
    /// not required and is prohibited so a repository artifact cannot inherit the paired page's
    /// authority.
    static func projectArtifactResponse(cwd: String, artifact: String?) -> Response {
        guard let artifact, !artifact.isEmpty else {
            return .error(404, "artifact_not_found", "No project artifact named that.")
        }
        let root = URL(fileURLWithPath: cwd, isDirectory: true)
            .standardizedFileURL.resolvingSymlinksInPath()
        let file = URL(fileURLWithPath: artifact)
            .standardizedFileURL.resolvingSymlinksInPath()
        let rootPrefix = root.path.hasSuffix("/") ? root.path : root.path + "/"
        guard file.path.hasPrefix(rootPrefix), file.pathExtension.lowercased() == "html",
              let values = try? file.resourceValues(forKeys: [.isRegularFileKey, .fileSizeKey]),
              values.isRegularFile == true else {
            return .error(404, "artifact_not_found", "No project artifact named that.")
        }
        let maximumBytes = 2 * 1024 * 1024
        guard (values.fileSize ?? maximumBytes + 1) <= maximumBytes else {
            return .error(413, "artifact_too_large", "That project artifact is too large.")
        }
        guard let data = try? Data(contentsOf: file, options: [.mappedIfSafe]) else {
            return .error(404, "artifact_not_found", "No project artifact named that.")
        }
        return Response(status: 200, headers: [
            "Content-Type": "text/html; charset=utf-8",
            "Cache-Control": "private, no-store",
            "Content-Security-Policy": "default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
            "X-Robots-Tag": "noindex, nofollow",
        ], body: data)
    }
}

// MARK: - Documents

/// The documents this Mac produces, and the only two places they may be read from.
///
/// **This is a closed pair of roots, not a file server.** The artifact route above is safe
/// because `kind` is a *slot* rather than a path: there is no string a caller can send that
/// names a third file. That property cannot survive a route whose whole purpose is to serve a
/// document the caller names, so what replaces it is a boundary — every byte that leaves here is
/// under one of two directories the server computed for itself, and a caller's string can only
/// choose within one.
///
/// **The two roots.**
///
/// - `<session cwd>/artifacts` — where the long working documents live. In this repository that
///   path is a symlink into a private sibling checkout, and it is the one symlink this code
///   follows. It is followed *first*, before any caller string exists: somebody put it there to
///   say "my documents live over there", so what it resolves to **becomes the root**, and
///   containment is judged against that. Every other symlink is an escape as far as this is
///   concerned, including one inside the root pointing back out.
/// - `<task directory>/artifacts` — a dispatched child's deliverables. Note which directory that
///   is. The task directory itself holds `task.json`, whose secret authenticates that child's
///   completion, and `result.json`, which by protocol repeats that secret back. **Neither is
///   reachable, and not because anything filters them out**: the root is the `artifacts`
///   subdirectory, so both are outside it, and `..` is refused before the filesystem is touched.
///   The redacted half of a child's result — its summary, its artifacts, its verification — is
///   already published by `GET /v1/orchestrator/tasks`, so the file itself has no reason to go.
///
/// **How far a hostile path gets: nowhere outside those two directories.** A caller's string is
/// refused before any filesystem call if it is absolute, holds an empty segment, or holds a
/// segment beginning with `.` — which covers `..`, `.` and every dotfile. Then the resolved path
/// must still begin with the resolved root, which is what refuses a symlink pointing out of it.
/// Then the file must be a regular file whose extension is one of three that carry no active
/// content, and under the size cap. The worst a name can do is read one of this project's own
/// markdown documents, which is what the route is for.
enum ProjectDocuments {
    /// Text, and nothing a browser will execute. HTML is deliberately absent: the two named
    /// slots above serve it under a CSP chosen for one known producer, and a directory anybody
    /// may drop a file into is not that.
    static let readableExtensions: Set<String> = ["md", "markdown", "txt"]
    static let maximumBytes = 2 * 1024 * 1024
    /// A listing is a menu, not a backup, and a walk that never ends is a denial of service
    /// against the queue this shares with `/links`.
    static let maximumListed = 200
    static let maximumWalked = 4000
    static let maximumDepth = 6
    /// Newest first, and only as far back as somebody would scroll.
    static let maximumTasksListed = 60

    struct Document {
        var path: String
        var bytes: Int
        var modified: Double
    }

    struct Located {
        var url: URL
        var path: String
        var bytes: Int
        var modified: Double
    }

    enum Refusal: String, Error {
        case notFound = "document_not_found"
        case tooLarge = "document_too_large"

        var status: Int { self == .tooLarge ? 413 : 404 }
        var message: String {
            switch self {
            case .notFound: return "No document named that."
            case .tooLarge: return "That document is too large to serve."
            }
        }
    }

    /// One spelling for a resolved path, because `resolvingSymlinksInPath()` does not have one.
    ///
    /// It strips a leading `/private` only when the receiver already carries it, so applying it
    /// to a temporary directory alternates: `/var/folders/x` resolves to `/private/var/folders/x`
    /// and that resolves back to `/var/folders/x`. A containment test comparing a root normalised
    /// once against a path normalised twice would then disagree with itself on exactly the
    /// directories the tests use. Resolve once, then pin the prefix.
    static func resolvedPath(_ url: URL) -> String {
        let path = url.resolvingSymlinksInPath().path
        return path.hasPrefix("/private/") ? String(path.dropFirst("/private".count)) : path
    }

    /// A root, with its one symlink followed and everything about it settled before a caller's
    /// string is looked at. `nil` when there is no such directory, which is the ordinary case.
    static func root(under directory: String) -> URL? {
        guard !directory.isEmpty else { return nil }
        let candidate = URL(fileURLWithPath: directory, isDirectory: true)
            .appendingPathComponent("artifacts", isDirectory: true)
        var isDirectory: ObjCBool = false
        let path = resolvedPath(candidate)
        guard FileManager.default.fileExists(atPath: path, isDirectory: &isDirectory),
              isDirectory.boolValue else { return nil }
        return URL(fileURLWithPath: path, isDirectory: true)
    }

    /// One document inside one root, or the reason it is not being served.
    static func file(in root: URL, at path: String) -> Result<Located, Refusal> {
        guard !path.isEmpty, path.count <= 512, !path.contains("\0"),
              !path.hasPrefix("/") else { return .failure(.notFound) }
        let segments = path.split(separator: "/", omittingEmptySubsequences: false)
        guard !segments.isEmpty, segments.count <= maximumDepth else { return .failure(.notFound) }
        for segment in segments {
            // `..`, `.` and every dotfile in one clause, before the filesystem is asked anything.
            guard !segment.isEmpty, !segment.hasPrefix(".") else { return .failure(.notFound) }
        }
        let relative = segments.joined(separator: "/")
        guard readableExtensions.contains(
                URL(fileURLWithPath: relative).pathExtension.lowercased())
        else { return .failure(.notFound) }
        var candidate = root
        for segment in segments { candidate.appendPathComponent(String(segment)) }
        let base = resolvedPath(root)
        let resolved = resolvedPath(candidate)
        guard resolved.hasPrefix(base.hasSuffix("/") ? base : base + "/") else {
            return .failure(.notFound)
        }
        let url = URL(fileURLWithPath: resolved)
        guard let values = try? url.resourceValues(
                forKeys: [.isRegularFileKey, .fileSizeKey, .contentModificationDateKey]),
              values.isRegularFile == true else { return .failure(.notFound) }
        let bytes = values.fileSize ?? maximumBytes + 1
        guard bytes <= maximumBytes else { return .failure(.tooLarge) }
        return .success(Located(
            url: url, path: relative, bytes: bytes,
            modified: values.contentModificationDate?.timeIntervalSince1970 ?? 0))
    }

    /// The path as it appears in a URL. `.urlPathAllowed` rather than `.alphanumerics`, so an
    /// ordinary document keeps an ordinary address: a name is split on `/` before it gets here,
    /// so no separator can survive the escaping, and `?` and `#` are not in that set.
    static func escaped(_ path: String) -> String {
        path.split(separator: "/").map {
            String($0).addingPercentEncoding(withAllowedCharacters: .urlPathAllowed)
                ?? String($0)
        }.joined(separator: "/")
    }

    /// What is in a root, newest first.
    ///
    /// Every candidate goes back through `file(in:at:)`, so a listing cannot offer a document the
    /// read would then refuse — which is the property that makes the list safe to hand to a page
    /// that turns each row into a link.
    static func documents(in root: URL) -> [Document] {
        let base = resolvedPath(root)
        let prefix = base.hasSuffix("/") ? base : base + "/"
        guard let walker = FileManager.default.enumerator(
                at: URL(fileURLWithPath: base, isDirectory: true),
                includingPropertiesForKeys: [.isRegularFileKey],
                options: [.skipsHiddenFiles, .skipsPackageDescendants]) else { return [] }
        var out: [Document] = []
        var walked = 0
        for case let entry as URL in walker {
            walked += 1
            if walked > maximumWalked { break }
            let resolved = resolvedPath(entry)
            guard resolved.hasPrefix(prefix) else { continue }
            guard case .success(let located) = file(in: root,
                                                    at: String(resolved.dropFirst(prefix.count)))
            else { continue }
            out.append(Document(path: located.path, bytes: located.bytes,
                                modified: located.modified))
        }
        out.sort { $0.modified == $1.modified ? $0.path < $1.path : $0.modified > $1.modified }
        return Array(out.prefix(maximumListed))
    }
}

extension RemoteServer {
    /// Whether this path is the documents route. Matched as a whole shape rather than a suffix,
    /// for the reason `isSlowReading` gives: a route recognised by `contains` is a route a
    /// deeper path can impersonate.
    static func isDocumentsReading(_ path: String) -> Bool {
        guard path.hasPrefix("/v1/sessions/") else { return false }
        let parts = path.dropFirst("/v1/sessions/".count)
            .split(separator: "/", omittingEmptySubsequences: false)
        return parts.count >= 2 && !parts[0].isEmpty && parts[1] == "documents"
    }

    /// The documents route: a listing at `/documents`, and the bytes below it.
    ///
    /// **Percent-decoding happens after the split, and that is deliberate.** A segment holding
    /// `%2F` decodes to something containing a separator, and it stays one segment here — but
    /// `ProjectDocuments.file(in:at:)` splits the joined path again and applies every segment
    /// rule to what it finds, so `..%2F..` is refused by the same clause that refuses `../..`.
    func documentsRoute(_ path: String) -> Response {
        var parts = path.dropFirst("/v1/sessions/".count)
            .split(separator: "/", omittingEmptySubsequences: false).map(String.init)
        let sessionID = parts.removeFirst()
        parts.removeFirst() // "documents"
        guard let session = self.session(withID: sessionID.removingPercentEncoding ?? sessionID),
              let cwd = Targets.workingDirectory(of: session) else {
            return .error(404, "document_not_found", "No document named that.")
        }
        if parts.isEmpty {
            return .json(["documents": documentsPayload(cwd: cwd, sessionID: sessionID)])
        }
        let decoded = parts.map { $0.removingPercentEncoding ?? $0 }
        switch decoded[0] {
        case "project":
            guard let root = ProjectDocuments.root(under: cwd) else {
                return .error(404, "document_not_found", "No document named that.")
            }
            return Self.documentResponse(root: root,
                                         path: decoded.dropFirst().joined(separator: "/"))
        case "task":
            guard decoded.count >= 3,
                  let directory = taskDocumentDirectory(id: decoded[1], cwd: cwd),
                  let root = ProjectDocuments.root(under: directory) else {
                return .error(404, "document_not_found", "No document named that.")
            }
            return Self.documentResponse(root: root,
                                         path: decoded.dropFirst(2).joined(separator: "/"))
        default:
            return .error(404, "document_not_found", "No document named that.")
        }
    }

    /// One document's bytes, or its typed refusal.
    static func documentResponse(root: URL, path: String) -> Response {
        switch ProjectDocuments.file(in: root, at: path) {
        case .failure(let refusal):
            return .error(refusal.status, refusal.rawValue, refusal.message)
        case .success(let located):
            guard let data = try? Data(contentsOf: located.url, options: [.mappedIfSafe]) else {
                return .error(404, "document_not_found", "No document named that.")
            }
            let text = located.url.pathExtension.lowercased() == "txt" ? "plain" : "markdown"
            return Response(status: 200, headers: [
                "Content-Type": "text/\(text); charset=utf-8",
                "Cache-Control": "private, no-store",
                // Without this a browser is free to decide a document full of angle brackets is
                // HTML, and the whole reason only three extensions are served is that none of
                // them is a program.
                "X-Content-Type-Options": "nosniff",
                "Content-Security-Policy": "default-src 'none'; script-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
                "X-Robots-Tag": "noindex, nofollow",
            ], body: data)
        }
    }

    /// Everything this project has written down, in the order somebody would want it.
    ///
    /// **A route rather than rows on `/links`**, for the reason `/links` itself gives about the
    /// event stream, and one more: a `/links` row is an address something already knows how to
    /// draw. Until the page has a document view, a row pointing here would be a control whose
    /// read the client cannot render — so the row belongs to the slice that draws the page, and
    /// this is the address it will point at.
    func documentsPayload(cwd: String, sessionID: String) -> [[String: Any]] {
        var out: [[String: Any]] = []
        func rows(_ documents: [ProjectDocuments.Document], under address: String,
                  source: String, task: [String: Any]?) {
            for document in documents {
                let escaped = ProjectDocuments.escaped(document.path)
                var row: [String: Any] = [
                    "source": source,
                    "path": document.path,
                    "label": document.path,
                    "bytes": document.bytes,
                    "modified": document.modified,
                    "url": "/v1/sessions/\(sessionID)/documents/\(address)/\(escaped)",
                ]
                if let task { row["task"] = task }
                out.append(row)
            }
        }
        if let root = ProjectDocuments.root(under: cwd) {
            rows(ProjectDocuments.documents(in: root), under: "project",
                 source: "project", task: nil)
        }
        for record in Self.documentTaskRecords(cwd: cwd) {
            guard let id = record["id"] as? String,
                  let directory = record["dir"] as? String,
                  let root = ProjectDocuments.root(under: directory) else { continue }
            rows(ProjectDocuments.documents(in: root), under: "task/\(id)", source: "task",
                 task: ["id": id, "title": record["title"] as? String ?? ""])
        }
        return Array(out.prefix(ProjectDocuments.maximumListed))
    }

    /// The tasks whose deliverables belong to the project this session is in.
    ///
    /// The registry is what authorises a task directory, not the filesystem: `/tmp/.clawdline`
    /// holds a directory per task and a caller naming one this Mac never dispatched gets the same
    /// 404 as a caller naming a file that does not exist.
    static func documentTaskRecords(cwd: String) -> [[String: Any]] {
        Orchestrator.records().filter { record in
            guard let id = record["id"] as? String, OrchestratorDraft.isTaskID(id) else {
                return false
            }
            if record["projectDir"] as? String == cwd { return true }
            return (record["worktree"] as? [String: Any])?["path"] as? String == cwd
        }.prefix(ProjectDocuments.maximumTasksListed).map { $0 }
    }

    /// One task's directory, once the registry agrees it is this project's task.
    func taskDocumentDirectory(id: String, cwd: String) -> String? {
        guard OrchestratorDraft.isTaskID(id) else { return nil }
        return Self.documentTaskRecords(cwd: cwd).first { $0["id"] as? String == id }?["dir"]
            as? String
    }
}
