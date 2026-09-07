import Foundation

/// The closed Cloud command/read vocabulary translated into the local HTTP router's shape.
///
/// This is a transport adapter, not a second router: the resulting request still passes through
/// `RemoteServer.dispatch`, so permission, idempotency, queueing, validation and response encoding
/// remain the same implementation used by a paired browser on the Mac's network.
struct CloudLocalRoute: Sendable {
    let method: String
    let path: String
    let query: [String: String]
    let body: Data

    init(command: CloudHeadlessCommand) {
        method = "POST"
        query = [:]
        var route: String
        let object: [String: Any]
        switch command {
        case .send(let session, let text, let images):
            route = "/v1/sessions/\(Self.segment(session))/send"
            object = ["text": text, "images": images]
        case .answer(let session, let key):
            route = "/v1/sessions/\(Self.segment(session))/key"
            object = ["key": key]
        case .start(let place, let assistant, let model):
            route = "/v1/places/\(Self.segment(place))/start"
            if !assistant.isEmpty || !model.isEmpty {
                route += "/" + Self.segment(assistant.isEmpty ? "claude" : assistant)
            }
            if !model.isEmpty { route += "/" + Self.segment(model) }
            object = [:]
        case .resume(let place, let session, let assistant):
            route = "/v1/places/\(Self.segment(place))/resume/"
            if !assistant.isEmpty { route += Self.segment(assistant) + "/" }
            route += Self.segment(session)
            object = [:]
        }
        path = route
        body = (try? JSONSerialization.data(withJSONObject: object,
                                             options: [.withoutEscapingSlashes])) ?? Data()
    }

    init(read: CloudHeadlessRead) {
        method = "GET"
        body = Data()
        var route: String
        var parameters: [String: String] = [:]
        switch read {
        case .transcript(let session, let limit):
            route = "/v1/sessions/\(Self.segment(session))/transcript"
            parameters = ["limit": String(limit)]
        case .info(let session, let parts):
            route = "/v1/sessions/\(Self.segment(session))/info"
            if parts == "summary" { parameters = ["parts": "summary"] }
        case .agent(let session, let agent, let limit):
            route = "/v1/sessions/\(Self.segment(session))/agents/\(Self.segment(agent))"
            parameters = ["limit": String(limit)]
        case .shell(let session, let shell, let bytes):
            route = "/v1/sessions/\(Self.segment(session))/shells/\(Self.segment(shell))"
            parameters = ["bytes": String(bytes)]
        case .skills(let session):
            route = "/v1/sessions/\(Self.segment(session))/skills"
        case .git(let session):
            route = "/v1/sessions/\(Self.segment(session))/git"
        case .image(_, let id):
            route = "/v1/artifacts/images/\(Self.segment(id))"
        case .places:
            route = "/v1/places"
        case .projectWorktrees(_, _, let project):
            route = "/v1/orchestrator/usage/project-worktrees"
            parameters = ["project": project]
        case .pastSessions(_, _, let place, let assistant):
            route = "/v1/places/\(Self.segment(place))/sessions"
            if !assistant.isEmpty { route += "/" + Self.segment(assistant) }
        }
        path = route
        query = parameters
    }

    private static func segment(_ value: String) -> String {
        CloudAppBridge.channelSegment(value)
    }
}
