import Foundation
import Network

/// Fresh project/subprocess routes are intentionally kept out of `RemoteServer`'s serial owner.
extension RemoteServer {
    static func isSlowReading(_ path: String) -> Bool {
        if path == "/v1/places" || path == "/v1/screens" { return true }
        guard path.hasPrefix("/v1/sessions/") else { return false }
        let rest = path.dropFirst("/v1/sessions/".count)
        let parts = rest.split(separator: "/", omittingEmptySubsequences: false)
        guard parts.count == 2, !parts[0].isEmpty else { return false }
        return parts[1] == "info" || parts[1] == "live"
    }

    static func isProjectReading(_ path: String) -> Bool {
        guard path.hasPrefix("/v1/sessions/") else { return false }
        let rest = path.dropFirst("/v1/sessions/".count)
        let parts = rest.split(separator: "/", omittingEmptySubsequences: false)
        guard !parts.isEmpty, !parts[0].isEmpty else { return false }
        if parts.count == 2 { return parts[1] == "git" || parts[1] == "links" }
        return parts.count == 3 && parts[1] == "artifacts" && !parts[2].isEmpty
    }

    func readSlowly(_ request: Request, on connection: NWConnection) {
        let id = ObjectIdentifier(connection)
        let token = startSlowReading(request) { [weak self] response in
            guard let self else { connection.cancel(); return }
            pendingFreshWaiters.removeValue(forKey: id)
            send(response, on: connection)
        }
        if let token { pendingFreshWaiters[id] = token }
    }

    func readProject(_ request: Request, on connection: NWConnection) {
        let id = ObjectIdentifier(connection)
        let token = startProjectReading(request) { [weak self] response in
            guard let self else { connection.cancel(); return }
            pendingProjectReads.removeValue(forKey: id)
            send(response, on: connection)
        }
        if let token { pendingProjectReads[id] = token }
    }

    @discardableResult
    func startProjectReading(_ request: Request, deliver: @escaping (Response) -> Void)
        -> ProjectReadCoordinator.Token? {
        if let refusal = slowReadingRefusal(request) {
            deliver(withCachePolicy(refusal))
            return nil
        }
        return projectReads.start(
            work: { self.route(request) },
            deliver: { outcome in deliver(self.projectReadResponse(outcome)) })
    }

    func projectReadResponse(_ outcome: ProjectReadCoordinator.Outcome) -> Response {
        let response: Response
        switch outcome {
        case .response(let answer):
            response = answer
        case .capacity(let limit, let current):
            response = .error(429, "project_read_busy",
                "This Mac already has \(current) project reads active or queued. Try again after they drain.",
                extra: ["limit": limit, "current": current, "retry_after": 1])
        case .queueTimeout(let seconds):
            response = .error(503, "project_read_queue_timeout",
                "That project read could not start inside its queue deadline.",
                extra: ["deadline_seconds": seconds, "retry_after": 1])
        case .requestTimeout(let seconds):
            response = .error(504, "project_read_timeout",
                "That project did not answer inside the request deadline.",
                extra: ["deadline_seconds": seconds, "retry_after": 1])
        }
        var decorated = withCachePolicy(response)
        if case .response = outcome { return decorated }
        decorated.headers["Retry-After"] = "1"
        return decorated
    }
}
