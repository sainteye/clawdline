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
        query = [:]
        var routeMethod = "POST"
        var route: String
        var object: [String: Any] = [:]
        var encodedBody: Data?
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
        case .scheduleCreate(let data):
            route = "/v1/orchestrator/schedules"
            encodedBody = data
        case .scheduleUpdate(let id, let data):
            routeMethod = "PATCH"
            route = "/v1/orchestrator/schedules/\(Self.segment(id))"
            encodedBody = data
        case .scheduleDelete(let id):
            routeMethod = "DELETE"
            route = "/v1/orchestrator/schedules/\(Self.segment(id))"
        case .pushSubscribe(let data):
            route = "/v1/push/subscribe"
            encodedBody = data
        case .pushUnsubscribe(let id):
            route = "/v1/push/unsubscribe"
            object = ["id": id]
        case .pushTest(let session):
            route = "/v1/push/test"
            object = session.isEmpty ? [:] : ["session_id": session]
        case .voice(let audio, let rate):
            route = "/v1/voice"
            object = ["audio": audio, "rate": rate]
        }
        method = routeMethod
        path = route
        body = encodedBody ?? (try? JSONSerialization.data(
            withJSONObject: object, options: [.withoutEscapingSlashes])) ?? Data()
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
        case .schedule(_, _, let id):
            route = "/v1/orchestrator/schedules/\(Self.segment(id))"
        case .pushKey:
            route = "/v1/push/key"
        }
        path = route
        query = parameters
    }

    private static func segment(_ value: String) -> String {
        CloudAppBridge.channelSegment(value)
    }
}

/// Dictation is the one local route that deliberately does not run through `dispatch`: Whisper
/// takes seconds, so the HTTP server hands it to a private queue before answering its socket. A
/// relay command has no socket to hand over. This actor provides the same bounded asynchronous
/// door, while the actual recognizer remains `Whisper.transcribe` — shared with both the menu bar
/// and the local web route, and globally serialized there so two transports cannot start two
/// whisper processes on one Mac.
actor CloudVoiceCommandRouter {
    static let shared = CloudVoiceCommandRouter()

    private var active = 0
    private var remembered: [String: (at: Date, result: CloudCommandResult)] = [:]

    func route(audio: String, rate: Int, sender: String,
               idempotencyKey: String) async -> CloudCommandResult {
        let now = Date()
        remembered = remembered.filter { now.timeIntervalSince($0.value.at) < 600 }
        if let seen = remembered[idempotencyKey] { return seen.result }

        let samples: Data
        switch RemoteServer.voiceSamples(from: ["audio": audio, "rate": rate]) {
        case .samples(let data):
            samples = data
        case .refused(let response):
            let result = Self.commandResult(response)
            remembered[idempotencyKey] = (now, result)
            return result
        }

        guard active < RemoteServer.voiceDepth else {
            return Self.failure(429, "busy",
                "Two recordings are already waiting to be transcribed on this Mac. "
                    + "Try again in a moment.")
        }
        active += 1
        let result = await Task.detached(priority: .userInitiated) {
            Self.transcribe(samples, sender: "cloud:\(sender)")
        }.value
        active -= 1
        if result.status == 200 { remembered[idempotencyKey] = (Date(), result) }
        return result
    }

    private static func transcribe(_ samples: Data, sender: String) -> CloudCommandResult {
        switch Whisper.status(binary: Config.shared.whisperBinary,
                              model: Config.shared.whisperModel) {
        case .noBinary:
            return failure(503, "no_whisper",
                "This Mac has no whisper-cli, so there is nothing here to read a recording with. "
                    + "See docs/whisper.md.", ["reason": "no_binary"])
        case .noModel:
            return failure(503, "no_whisper",
                "whisper-cli is installed on this Mac and has no model to read with. "
                    + "See docs/whisper.md.", ["reason": "no_model"])
        case .ready:
            break
        }

        let started = Date()
        let heard = Whisper.transcribe(samples, rate: RemoteServer.voiceRate, vocabulary: [],
                                       language: Config.shared.voiceLanguage)
        let milliseconds = Int(Date().timeIntervalSince(started) * 1_000)
        let text = heard.map {
            Whisper.applyVocabulary($0,
                terms: Voice.alwaysExpected + Config.shared.voiceVocabulary)
        } ?? ""
        let seconds = Double(samples.count) / (RemoteServer.voiceRate * 2)
        RemoteAuth.audit("voice.transcribe", ["device": sender,
            "seconds": String(format: "%.1f", seconds), "ms": "\(milliseconds)",
            "chars": "\(text.count)", "ok": heard == nil ? "0" : "1"])
        return success(["text": text, "ms": milliseconds])
    }

    private static func commandResult(_ response: RemoteServer.Response) -> CloudCommandResult {
        let object = (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]
        let error = object?["error"] as? [String: Any]
        return CloudCommandResult(status: response.status, code: error?["code"] as? String,
                                  body: response.body)
    }

    private static func success(_ body: [String: Any]) -> CloudCommandResult {
        let data = (try? JSONSerialization.data(
            withJSONObject: body, options: [.withoutEscapingSlashes])) ?? Data()
        return CloudCommandResult(status: 200, code: nil, body: data)
    }

    private static func failure(_ status: Int, _ code: String, _ message: String,
                                _ extra: [String: Any] = [:]) -> CloudCommandResult {
        var error: [String: Any] = ["code": code, "message": message]
        extra.forEach { error[$0.key] = $0.value }
        let data = (try? JSONSerialization.data(
            withJSONObject: ["error": error], options: [.withoutEscapingSlashes])) ?? Data()
        return CloudCommandResult(status: status, code: code, body: data)
    }
}
