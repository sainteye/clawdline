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
