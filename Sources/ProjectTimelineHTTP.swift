import Foundation

/// Authenticated local and encrypted Cloud requests enter the same Timeline transaction here.
enum ProjectTimelineHTTP {
    static let maximumResponseBytes = 2 * 1024 * 1024
    private static var storeForTesting: ProjectTimelineStore?

    struct PreparedRead {
        let viewer: [String: Any]
        let query: ProjectTimelineReadCache.Query

        func execute() -> RemoteServer.Response {
            var envelope = ProjectTimelineIntegration.read(query)
            guard var timeline = envelope["timeline"] as? [String: Any] else {
                return .error(503, "timeline_unavailable", "Timeline projection is unavailable.")
            }
            if timeline["available"] as? Bool == false {
                let error = timeline["error"] as? [String: Any] ?? [:]
                return .error(503, error["code"] as? String ?? "timeline_unavailable",
                              error["message"] as? String ?? "Timeline is unavailable.")
            }
            timeline["viewer"] = viewer; envelope["timeline"] = timeline
            return response(envelope)
        }
    }

    struct PreparedCommand {
        let body: [String: Any]
        let actor: String
        let machine: Bool
        let store: ProjectTimelineStore

        func execute() -> RemoteServer.Response {
            let reply = store.command(body, actor: actor, trusted: machine)
            if reply.status == 200 {
                if body["operation"] as? String == "set_enabled" {
                    ProjectTimelineIntegration.modeDidChange(enabled: store.enabled)
                } else { ProjectTimelineIntegration.didMutate() }
            }
            return response(reply.body, status: reply.status)
        }
    }

    enum Admission { case response(RemoteServer.Response); case read(PreparedRead); case command(PreparedCommand) }

    static func admit(_ request: RemoteServer.Request, machine: Bool,
                      permission: RemoteAuth.Verdict) -> Admission? {
        guard request.path == "/v1/timeline" else { return nil }
        let viewer = ProjectBoardHTTP.viewer(machine: machine, permission: permission,
            source: request.source, remoteWrite: Config.shared.remoteWrite)
        if request.method == "GET" {
            let allowed = Set(["project", "entry", "cursor", "environment", "category", "upcoming"])
            guard request.repeatedQueryKeys.isEmpty, Set(request.query.keys).isSubset(of: allowed),
                  request.query.values.allSatisfy({ !$0.isEmpty && $0.utf8.count <= 200 }) else {
                return .response(.error(400, "bad_timeline_query", "Unknown or repeated Timeline query field."))
            }
            let cursor: Int
            if let raw = request.query["cursor"] {
                guard let value = Int(raw), value >= 0, value <= ProjectTimelineStore.maximumEntries else {
                    return .response(.error(400, "invalid_timeline_cursor", "Timeline cursor is outside its bound."))
                }
                cursor = value
            } else { cursor = 0 }
            let environment = request.query["environment"] ?? "production"
            guard ["production", "staging", "preview", "development", "all"].contains(environment) else {
                return .response(.error(400, "invalid_timeline_environment", "Timeline environment is not supported."))
            }
            let category = request.query["category"]
            if let category, !["deploy", "server", "architecture", "feature", "operation"].contains(category) {
                return .response(.error(400, "invalid_timeline_category", "Timeline category is not supported."))
            }
            let upcoming = request.query["upcoming"].map { $0 == "true" || $0 == "1" } ?? false
            if let raw = request.query["upcoming"], !["true", "1", "false", "0"].contains(raw) {
                return .response(.error(400, "invalid_timeline_upcoming", "upcoming must be true or false."))
            }
            return .read(.init(viewer: viewer, query: .init(project: request.query["project"],
                entry: request.query["entry"], cursor: cursor, environment: environment,
                category: category, includeUpcoming: upcoming)))
        }
        guard request.method == "POST" else { return .response(.status(405)) }
        if case .http = request.source, !machine, !Config.shared.remoteWrite {
            return .response(.error(403, "write_disabled", "Remote writes are disabled on this Mac."))
        }
        let canWrite = viewer["canWrite"] as? Bool == true
        let canManage = viewer["canManage"] as? Bool == true
        guard canWrite else { return .response(.error(403, "forbidden", "This device may only read Timeline.")) }
        guard request.body.count <= 256 * 1024,
              let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any],
              let operation = body["operation"] as? String else {
            return .response(.error(400, "bad_request", "A bounded Timeline command is required."))
        }
        if operation == "set_enabled" && !canManage {
            return .response(.error(403, "forbidden", "Changing Timeline mode requires an administrative device."))
        }
        if operation == "ingest" && !machine {
            return .response(.error(403, "timeline_ingest_forbidden", "Only a local trusted producer may ingest Timeline evidence."))
        }
        return .command(.init(body: body, actor: viewer["id"] as? String ?? "machine",
                              machine: machine, store: storeForTesting ?? .shared))
    }

    static func response(_ envelope: [String: Any], status: Int = 200) -> RemoteServer.Response {
        let reply = RemoteServer.Response.json(envelope, status: status)
        guard !reply.body.isEmpty, reply.body.count <= maximumResponseBytes else {
            return .error(503, "timeline_response_too_large", "Select a narrower Timeline page; the response exceeds its byte budget.")
        }
        return reply
    }

    static func route(_ request: RemoteServer.Request, machine: Bool,
                      permission: RemoteAuth.Verdict) -> RemoteServer.Response? {
        guard let admission = admit(request, machine: machine, permission: permission) else { return nil }
        switch admission {
        case .response(let value): return value
        case .read(let value): return value.execute()
        case .command(let value): return value.execute()
        }
    }

    static func configureStoreForTesting(_ store: ProjectTimelineStore?) { storeForTesting = store }
}
