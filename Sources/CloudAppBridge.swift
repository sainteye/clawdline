import Foundation

/// The app-facing surface of `CloudTransport`. Keeping the concrete actor behind this protocol
/// makes the bridge testable without a relay, Keychain, or terminal process.
protocol CloudTransporting: Sendable {
    var commands: AsyncStream<CloudInboundCommand> { get }
    var readyGenerations: AsyncStream<UInt64> { get }
    func connect(role: CloudTransportRole) async throws
    func publish(envelope: CloudEnvelope) async throws
    func shutdown() async
}

extension CloudTransport: CloudTransporting {}

/// Sequence persistence belongs to configuration/pairing, not to this bridge. The injected
/// implementation must be durable: reusing a sender sequence after relaunch is a replay.
protocol CloudEnvelopeSequencing: Sendable {
    func nextSequence(sender: String) async throws -> UInt64
}

struct CloudAppIdentity: @unchecked Sendable {
    let machineID: String
    let deviceID: String
    let keyID: String
    let masterSecret: CloudMasterSecret
    let signingKey: CloudDeviceKeyPair
}

enum CloudHeadlessCommand: Equatable, Sendable {
    case send(session: String, text: String, images: [String])
    case answer(session: String, key: String)
}

/// The reads a paired viewer may ask this Mac for over the relay.
///
/// A separate type from `CloudHeadlessCommand` because the two are gated differently, and the
/// difference is the whole reason this exists. A command types into somebody's session and needs
/// the write switch; a read changes nothing, and on the direct path a paired device reads a
/// transcript with that switch off. The session rows this bridge already publishes cross without
/// consulting it either, so a viewer that can see every row and not the messages inside one is
/// showing less than the same device sees over the tunnel — for no reason anybody chose.
///
/// The vocabulary is closed on purpose: a viewer names one of these, never a route.
enum CloudHeadlessRead: Equatable, Sendable {
    case transcript(session: String, limit: Int)
    case info(session: String, parts: String)
    case agent(session: String, agent: String, limit: Int)
    case shell(session: String, shell: String, bytes: Int)
    case skills(session: String)
    case git(session: String)
    /// One image already referenced by a message in that session's transcript.
    ///
    /// The reference — id, media type, byte count, pixel size, expiry — has always crossed,
    /// because it travels inside the transcript body. What could not cross was the picture: the
    /// browser turns that reference into `<img src="/v1/artifacts/images/:id">`, a *same-origin
    /// relative* URL, and on this path the origin is the hosted console rather than this Mac. The
    /// console serves no such route, so every image in every transcript was a broken-image icon —
    /// the one failure in this transport that looked like the reader's own fault.
    case image(session: String, id: String)

    /// The session this read is about — also the channel its answer is published on.
    var session: String {
        switch self {
        case .transcript(let session, _): return session
        case .info(let session, _): return session
        case .agent(let session, _, _): return session
        case .shell(let session, _, _): return session
        case .skills(let session): return session
        case .git(let session): return session
        case .image(let session, _): return session
        }
    }

    /// The word the answer carries, so a viewer waiting for one read cannot accept another.
    ///
    /// The two halves of Info are separate names rather than one, because they are separate
    /// answers: the summary omits screen, Git and links/deploy, and a full request settled by a
    /// summary would be cached as complete while missing exactly those. A distinction that is
    /// only in the request and not in the answer is a distinction the receiver cannot make.
    ///
    /// **An agent and a shell put their id in the name, and there the distinction is sharper
    /// still.** A session has many of each and one answer channel between them, so a reader who
    /// opens two agents — the ordinary case, the strip lists them side by side — would have the
    /// first settled by the second's conversation, with nothing in either answer able to tell
    /// them apart. `skills` and `git` need no id: there is one of each per session, exactly as
    /// there is one transcript.
    ///
    /// An image's own id is part of its word for the same reason: a transcript holds many
    /// pictures, they are asked for together, and they come back on one channel in whatever
    /// order the disk gives them. A bare `"image"` would let the second answer settle the
    /// first tile.
    var name: String {
        switch self {
        case .transcript: return "transcript"
        case .info(_, let parts): return "info." + parts
        case .agent(_, let agent, _): return "agent:" + agent
        case .shell(_, let shell, _): return "shell:" + shell
        case .skills: return "skills"
        case .git: return "git"
        case .image(_, let id): return "image." + id
        }
    }
}

struct CloudCommandResult: Equatable, Sendable {
    let status: Int
    let code: String?
}

/// A read's answer, kept as the route's own bytes rather than a parsed object: a refusal is an
/// answer too, and forwarding the typed `{"error":{"code",…}}` unchanged is what lets a browser
/// on the cloud path branch on exactly the codes it already branches on over the tunnel.
struct CloudReadResult: Equatable, Sendable {
    let status: Int
    let body: Data
    /// What those bytes are, when they are not JSON.
    ///
    /// Only the image read needs it: its route answers with the PNG itself and says so in a
    /// header, and the bridge has to name the media type in a payload field because an envelope
    /// has no headers. Every other read forwards JSON and leaves this nil rather than restating
    /// `application/json` in a second place.
    let contentType: String?

    init(status: Int, body: Data, contentType: String? = nil) {
        self.status = status
        self.body = body
        self.contentType = contentType
    }
}

/// Both cloud and HTTP commands enter this door. The production implementation below converts a
/// typed command back into an in-process request so authentication, idempotency, menu safety,
/// image validation, audit, and the actual terminal operation remain one implementation.
protocol CloudCommandRouting: Sendable {
    func route(_ command: CloudHeadlessCommand, sender: String,
               idempotencyKey: String) async -> CloudCommandResult
    /// Reads enter through the same door for the same reason, minus the idempotency key: a GET
    /// that is retried is not a second anything.
    func read(_ read: CloudHeadlessRead, sender: String) async -> CloudReadResult
}

struct RemoteServerCloudCommandRouter: CloudCommandRouting, @unchecked Sendable {
    let server: RemoteServer

    init(server: RemoteServer = .shared) {
        self.server = server
    }

    func route(_ command: CloudHeadlessCommand, sender: String,
               idempotencyKey: String) async -> CloudCommandResult {
        let response = await server.routeVerifiedCloudCommand(
            command, sender: sender, idempotencyKey: idempotencyKey
        )
        let object = (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]
        let error = object?["error"] as? [String: Any]
        return CloudCommandResult(status: response.status, code: error?["code"] as? String)
    }

    func read(_ read: CloudHeadlessRead, sender: String) async -> CloudReadResult {
        let response = await server.routeVerifiedCloudRead(read, sender: sender)
        return CloudReadResult(status: response.status, body: response.body,
                               contentType: response.headers["Content-Type"])
    }
}

enum CloudAppBridgeError: Error, LocalizedError, Equatable {
    case alreadyRunning
    case notRunning
    case malformedSessions
    case malformedOrchestrator

    var errorDescription: String? {
        switch self {
        case .alreadyRunning: return "The cloud app bridge is already running."
        case .notRunning: return "The cloud app bridge has not been explicitly started."
        case .malformedSessions: return "The local session snapshot is malformed."
        case .malformedOrchestrator: return "The local orchestrator snapshot is malformed."
        }
    }
}

/// Connects the app's existing full-snapshot and HTTP-command seams to CloudTransport.
///
/// Construction has no side effects. `start()` is the explicit attachment/configuration point,
/// and inbound commands have a second, separately injected gate whose default is always false.
actor CloudAppBridge {
    typealias CommandGate = @Sendable () -> Bool
    typealias Milliseconds = @Sendable () -> UInt64
    typealias CommandResultObserver = @Sendable (CloudCommandResult) -> Void
    typealias TransportReadyObserver = @Sendable (UInt64) -> Void

    private let transport: any CloudTransporting
    private let identity: CloudAppIdentity
    private let sequencing: any CloudEnvelopeSequencing
    private let allowCloudCommands: CommandGate
    private let commandRouter: any CloudCommandRouting
    private let nowMilliseconds: Milliseconds
    private let commandResult: CommandResultObserver

    private var commandTask: Task<Void, Never>?
    private var readyTask: Task<Void, Never>?
    private var connectTask: Task<Void, Error>?
    private var publicationTasks: [UUID: Task<Void, Error>] = [:]
    private var transportReady: TransportReadyObserver = { _ in }
    private var lifecycleGeneration: UInt64 = 0
    private var starting = false
    private var running = false
    private var publishedSessionIDs = Set<String>()

    init(
        transport: any CloudTransporting,
        identity: CloudAppIdentity,
        sequencing: any CloudEnvelopeSequencing,
        allowCloudCommands: @escaping CommandGate = { false },
        commandRouter: any CloudCommandRouting = RemoteServerCloudCommandRouter(),
        nowMilliseconds: @escaping Milliseconds = {
            UInt64(Date().timeIntervalSince1970 * 1_000)
        },
        commandResult: @escaping CommandResultObserver = { _ in }
    ) {
        self.transport = transport
        self.identity = identity
        self.sequencing = sequencing
        self.allowCloudCommands = allowCloudCommands
        self.commandRouter = commandRouter
        self.nowMilliseconds = nowMilliseconds
        self.commandResult = commandResult
    }

    func start() async throws {
        guard !running, !starting else { throw CloudAppBridgeError.alreadyRunning }
        lifecycleGeneration &+= 1
        let ownedGeneration = lifecycleGeneration
        starting = true
        let transport = self.transport
        let connect = Task {
            try await transport.connect(role: .machine)
        }
        connectTask = connect
        do {
            try await withTaskCancellationHandler {
                try await connect.value
            } onCancel: {
                connect.cancel()
            }
            guard lifecycleGeneration == ownedGeneration, !Task.isCancelled else {
                if lifecycleGeneration == ownedGeneration { starting = false }
                await transport.shutdown()
                throw CancellationError()
            }
            connectTask = nil
            starting = false
            running = true

            let commandStream = transport.commands
            commandTask = Task { [weak self] in
                for await command in commandStream {
                    guard let self else { return }
                    await self.consume(command, lifecycleGeneration: ownedGeneration)
                }
                await self?.commandStreamFinished(lifecycleGeneration: ownedGeneration)
            }
            let readyStream = transport.readyGenerations
            readyTask = Task { [weak self] in
                for await generation in readyStream {
                    guard let self else { return }
                    await self.transportBecameReady(
                        generation, lifecycleGeneration: ownedGeneration
                    )
                }
            }
        } catch {
            if lifecycleGeneration == ownedGeneration {
                connectTask = nil
                starting = false
            }
            throw error
        }
    }

    func stop() async {
        guard running || starting || connectTask != nil || commandTask != nil || readyTask != nil
                || !publicationTasks.isEmpty
        else { return }
        lifecycleGeneration &+= 1
        starting = false
        running = false
        let command = commandTask
        let ready = readyTask
        let connect = connectTask
        let publications = Array(publicationTasks.values)
        commandTask = nil
        readyTask = nil
        connectTask = nil
        publicationTasks.removeAll()
        connect?.cancel()
        command?.cancel()
        ready?.cancel()
        publications.forEach { $0.cancel() }
        await transport.shutdown()
        _ = await connect?.result
        await command?.value
        await ready?.value
        for publication in publications {
            _ = await publication.result
        }
        publishedSessionIDs.removeAll()
    }

    func isRunning() -> Bool { running }

    func setTransportReadyObserver(_ observer: @escaping TransportReadyObserver) {
        transportReady = observer
    }

    /// Accepts the exact JSON bytes produced for local SSE, then fans its complete session rows
    /// out by channel. A complete authoritative scan also sends tombstones for rows that vanished.
    func publishSessions(_ payload: Data) async throws {
        let ownedGeneration = lifecycleGeneration
        try await runPublication { [weak self] in
            guard let self else { throw CancellationError() }
            try await self.publishSessionsOwned(
                payload, lifecycleGeneration: ownedGeneration
            )
        }
    }

    private func publishSessionsOwned(
        _ payload: Data, lifecycleGeneration ownedGeneration: UInt64
    ) async throws {
        try requireActivePublication(lifecycleGeneration: ownedGeneration)
        guard let root = try? JSONSerialization.jsonObject(with: payload) as? [String: Any],
              let sessions = root["sessions"] as? [[String: Any]],
              let at = root["at"], let scan = root["scan"] as? [String: Any]
        else { throw CloudAppBridgeError.malformedSessions }

        var current = Set<String>()
        for session in sessions {
            try requireActivePublication(lifecycleGeneration: ownedGeneration)
            guard let id = session["id"] as? String, !id.isEmpty else {
                throw CloudAppBridgeError.malformedSessions
            }
            current.insert(id)
            let full: [String: Any] = ["session": session, "at": at, "scan": scan]
            try await publishJSON(
                full, channel: sessionChannel(id), lifecycleGeneration: ownedGeneration
            )
        }

        let authoritative = (scan["complete"] as? Bool) == true
            || (scan["emptyAuthoritative"] as? Bool) == true
        if authoritative {
            for id in publishedSessionIDs.subtracting(current) {
                try requireActivePublication(lifecycleGeneration: ownedGeneration)
                let tombstone: [String: Any] = [
                    "session": NSNull(), "deleted": true, "at": at, "scan": scan,
                ]
                try await publishJSON(
                    tombstone, channel: sessionChannel(id),
                    lifecycleGeneration: ownedGeneration
                )
            }
            try requireActivePublication(lifecycleGeneration: ownedGeneration)
            publishedSessionIDs = current
        } else {
            try requireActivePublication(lifecycleGeneration: ownedGeneration)
            publishedSessionIDs.formUnion(current)
        }
    }

    /// The local SSE serializer has already made these bytes. Seal them unchanged so local and
    /// cloud viewers receive one `Orchestrator.records()` payload shape.
    func publishOrchestrator(_ payload: Data) async throws {
        let ownedGeneration = lifecycleGeneration
        try await runPublication { [weak self] in
            guard let self else { throw CancellationError() }
            try await self.publishOrchestratorOwned(
                payload, lifecycleGeneration: ownedGeneration
            )
        }
    }

    private func publishOrchestratorOwned(
        _ payload: Data, lifecycleGeneration ownedGeneration: UInt64
    ) async throws {
        try requireActivePublication(lifecycleGeneration: ownedGeneration)
        guard JSONSerialization.isValidJSONObject(
            (try? JSONSerialization.jsonObject(with: payload)) as Any
        ) else { throw CloudAppBridgeError.malformedOrchestrator }
        try await publish(
            payload, channel: "orch/" + Self.channelSegment(identity.machineID),
            lifecycleGeneration: ownedGeneration
        )
    }

    static func channelSegment(_ value: String) -> String {
        var allowed = CharacterSet.alphanumerics
        allowed.insert(charactersIn: "-_.!~*'()")
        return value.addingPercentEncoding(withAllowedCharacters: allowed) ?? ""
    }

    private func sessionChannel(_ sessionID: String) -> String {
        "s/" + Self.channelSegment(identity.machineID) + "/" + Self.channelSegment(sessionID)
    }

    /// Where a read's answer goes. `t/<machine>/<session>` is PROTOCOL §2's transcript channel and
    /// has been in the relay and in `cloud-crypto.js` all along with nothing publishing to it; the
    /// viewer already subscribes to it when a session is opened. `info` rides the same channel
    /// rather than a new prefix because a prefix is the one part of an envelope the relay reads,
    /// and adding one would need a relay this repository does not contain. The payload says which
    /// read it is, which costs the relay nothing: everything past `ch` is ciphertext to it.
    private func transcriptChannel(_ sessionID: String) -> String {
        "t/" + Self.channelSegment(identity.machineID) + "/" + Self.channelSegment(sessionID)
    }

    private func publishJSON(
        _ object: [String: Any], channel: String, lifecycleGeneration ownedGeneration: UInt64
    ) async throws {
        guard JSONSerialization.isValidJSONObject(object) else {
            throw CloudAppBridgeError.malformedSessions
        }
        let payload = try JSONSerialization.data(
            withJSONObject: object, options: [.withoutEscapingSlashes]
        )
        try await publish(
            payload, channel: channel, lifecycleGeneration: ownedGeneration
        )
    }

    private func publish(
        _ plaintext: Data, channel: String, lifecycleGeneration ownedGeneration: UInt64
    ) async throws {
        try requireActivePublication(lifecycleGeneration: ownedGeneration)
        let sequence = try await sequencing.nextSequence(sender: identity.deviceID)
        try requireActivePublication(lifecycleGeneration: ownedGeneration)
        let envelope = try CloudEnvelope.seal(
            plaintext, ch: channel, seq: sequence, ts: nowMilliseconds(),
            envelopeClass: .stream, keyID: identity.keyID, sender: identity.deviceID,
            masterSecret: identity.masterSecret, signingKey: identity.signingKey
        )
        try requireActivePublication(lifecycleGeneration: ownedGeneration)
        try await transport.publish(envelope: envelope)
    }

    private func runPublication(
        _ work: @escaping @Sendable () async throws -> Void
    ) async throws {
        guard running else { throw CloudAppBridgeError.notRunning }
        let identifier = UUID()
        let publication = Task {
            try await work()
        }
        publicationTasks[identifier] = publication
        do {
            try await withTaskCancellationHandler {
                try await publication.value
            } onCancel: {
                publication.cancel()
            }
            publicationTasks[identifier] = nil
        } catch {
            publicationTasks[identifier] = nil
            throw error
        }
    }

    private func requireActivePublication(
        lifecycleGeneration ownedGeneration: UInt64
    ) throws {
        guard running, lifecycleGeneration == ownedGeneration, !Task.isCancelled else {
            throw CancellationError()
        }
    }

    private func commandStreamFinished(lifecycleGeneration ownedGeneration: UInt64) {
        guard lifecycleGeneration == ownedGeneration else { return }
        running = false
        commandTask = nil
        readyTask?.cancel()
        readyTask = nil
    }

    private func transportBecameReady(
        _ generation: UInt64, lifecycleGeneration ownedGeneration: UInt64
    ) {
        guard running, lifecycleGeneration == ownedGeneration else { return }
        transportReady(generation)
    }

    private func consume(
        _ inbound: CloudInboundCommand, lifecycleGeneration ownedGeneration: UInt64
    ) async {
        guard running, lifecycleGeneration == ownedGeneration else { return }
        let wantedChannel = "ctl/" + Self.channelSegment(identity.machineID)
        guard inbound.channel == wantedChannel else {
            commandResult(CloudCommandResult(status: 409, code: "wrong_machine"))
            return
        }
        // Parsed before the write gate rather than after it, because the gate is not the same
        // question for a read: `Config.shared.remoteWrite` is "may a remote device type into a
        // session on this Mac", and a transcript read types into nothing. Everything that is not
        // a read — an unparseable body included — meets that gate exactly where it always did.
        let parsed = (try? JSONSerialization.jsonObject(with: inbound.plaintext)) as? [String: Any]
        let requestedType = parsed?["type"] as? String
        if let parsed, let requestedType, Self.readTypes.contains(requestedType) {
            await serveRead(requestedType, body: parsed, inbound: inbound,
                            lifecycleGeneration: ownedGeneration)
            return
        }
        guard allowCloudCommands() else {
            commandResult(CloudCommandResult(status: 403, code: "cloud_commands_disabled"))
            return
        }
        guard let body = parsed, let type = requestedType else {
            commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
            return
        }

        let command: CloudHeadlessCommand
        switch type {
        case "send":
            guard inbound.commandClass == .ctl, Set(body.keys) == ["type", "session", "text", "images"],
                  let session = body["session"] as? String, !session.isEmpty,
                  let text = body["text"] as? String, let images = body["images"] as? [String]
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .send(session: session, text: text, images: images)
        case "answer", "key":
            let allowedKeys: Set<String> = type == "answer"
                ? ["type", "session", "answer"] : ["type", "session", "key"]
            guard inbound.commandClass == .ctl, Set(body.keys) == allowedKeys,
                  let session = body["session"] as? String, !session.isEmpty,
                  let key = body[type == "answer" ? "answer" : "key"] as? String
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .answer(session: session, key: key)
        case "dispatch":
            // cloud-client.js sends only `{task}`. The local broker protocol requires a materialized
            // task.json plus task_id and secret, and no pinned wire shape says how those are carried
            // or where the file is authorized to be written. Refuse instead of inventing one.
            commandResult(CloudCommandResult(status: 409, code: "cloud_dispatch_unpinned"))
            return
        default:
            commandResult(CloudCommandResult(status: 400, code: "unknown_command"))
            return
        }

        let result = await commandRouter.route(
            command, sender: inbound.sender,
            idempotencyKey: "cloud:\(inbound.sender):\(inbound.sequence)"
        )
        commandResult(result)
    }

    /// The reads a viewer may name. A closed set, checked before the write gate, so that adding
    /// an eighth is a deliberate edit here rather than a spelling that slipped past.
    ///
    /// This and the switch in `serveRead` are two lists that have to agree, which is why that
    /// switch ends in a `default` that refuses rather than in the last read: a word admitted here
    /// and unknown there fails closed instead of being parsed as whichever case happens to sit at
    /// the bottom. `every read type this bridge admits also parses` walks this set and asks each
    /// member for a well-formed body, so the two cannot come apart quietly.
    static let readTypes: Set<String> = [
        "transcript", "info", "agent", "shell", "skills", "git", "image",
    ]

    /// The relay's per-account ciphertext cap, which every tier shares (`max_envelope_bytes`).
    static let cloudEnvelopeCiphertextLimit = 16 << 20

    /// The JSON around one image's base64: the two answer fields, the id, the media type, the
    /// byte count and the quoting. Measured at 176 bytes with every field at its maximum — the
    /// serialization itself, counted, not an allowance reasoned about — so a kibibyte here is
    /// slack rather than an estimate waiting to be tightened, the same shape as the relay's own
    /// 4 KiB frame allowance. At the ceiling the sealed answer leaves 848 bytes under the cap.
    static let cloudImageAnswerOverhead = 1 << 10

    /// The largest PNG this transport carries in one answer. **Derived, not chosen.**
    ///
    /// The relay caps the ciphertext of one envelope at 16 MiB, AES-GCM adds a 16-byte tag to the
    /// plaintext, base64 costs four bytes for every three, and the JSON above wraps it. Rounding
    /// the base64 budget down to a multiple of four keeps the encoded length exact rather than
    /// approximately right: 12,582,132 bytes, which is 780 bytes below the *store's* own 12 MiB
    /// ceiling on an encoded artifact. So all but the last kilobyte of what this Mac will ever
    /// hold does cross, and what does not is refused in a sentence instead of a broken icon.
    static var cloudImageMaxEncodedBytes: Int {
        if let forced = cloudImageMaxEncodedBytesForTesting { return forced }
        let budget = cloudEnvelopeCiphertextLimit - CloudEnvelope.tagByteCount
            - cloudImageAnswerOverhead
        return (budget / 4) * 3
    }

    /// A seam so the refusal above can be seen happening. The real ceiling sits 780 bytes under
    /// the largest artifact this Mac stores, which is the right number and an impossible fixture.
    static var cloudImageMaxEncodedBytesForTesting: Int?

    /// Answer one read: parse it strictly, route it through the door local HTTP already uses, and
    /// publish the answer on the session's own transcript channel.
    ///
    /// A refusal is published too. That is the whole point of the shape: a browser that asked for
    /// a transcript and is told `not_found` can say so, while a browser that is told nothing at all
    /// waits forever behind a skeleton — which is what this path did before, because nothing on
    /// this Mac had ever published a `t/` envelope.
    private func serveRead(
        _ type: String, body: [String: Any], inbound: CloudInboundCommand,
        lifecycleGeneration ownedGeneration: UInt64
    ) async {
        // A read rides the command channel and therefore its class, which is what the relay bills
        // and what `CloudEnvelope` pins. `dispatch` is a command class and never a read.
        guard inbound.commandClass == .ctl else {
            commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
            return
        }
        let read: CloudHeadlessRead
        switch type {
        case "transcript":
            guard Set(body.keys) == ["type", "session", "limit"],
                  let session = body["session"] as? String, !session.isEmpty,
                  let limit = body["limit"] as? Int, (1...1000).contains(limit)
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .transcript(session: session, limit: limit)
        case "info":
            guard Set(body.keys) == ["type", "session", "parts"],
                  let session = body["session"] as? String, !session.isEmpty,
                  let parts = body["parts"] as? String,
                  parts == "full" || parts == "summary"
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .info(session: session, parts: parts)
        case "agent":
            // An agent's window is a transcript's window, bounded where the route bounds it,
            // because an agent's file is read the same way a session's is — it is the same kind
            // of file, and `agentPayload` hands it to the same parser.
            guard Set(body.keys) == ["type", "session", "agent", "limit"],
                  let session = body["session"] as? String, !session.isEmpty,
                  let agent = body["agent"] as? String, !agent.isEmpty,
                  let limit = body["limit"] as? Int, (1...1000).contains(limit)
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .agent(session: session, agent: agent, limit: limit)
        case "shell":
            // Bytes and not entries: a command has no turns, so the only honest bound on its
            // output is how much of the tail to take. The range is the route's own — 1 KiB to
            // 1 MiB — so a cloud viewer can ask for exactly what a phone on the tunnel can and
            // no more, and the ceiling stays far inside one envelope.
            guard Set(body.keys) == ["type", "session", "shell", "bytes"],
                  let session = body["session"] as? String, !session.isEmpty,
                  let shell = body["shell"] as? String, !shell.isEmpty,
                  let bytes = body["bytes"] as? Int, ((1 << 10)...(1 << 20)).contains(bytes)
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .shell(session: session, shell: shell, bytes: bytes)
        case "skills":
            // No window on either of these two. `/skills` answers a menu whose length is how
            // many skills that assistant has, and `/git` answers file rows with counts and no
            // diff text at all, so neither has a size a caller could name.
            guard Set(body.keys) == ["type", "session"],
                  let session = body["session"] as? String, !session.isEmpty
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .skills(session: session)
        case "git":
            guard Set(body.keys) == ["type", "session"],
                  let session = body["session"] as? String, !session.isEmpty
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .git(session: session)
        case "image":
            // The id is the opaque one the transcript already published, and it is checked by the
            // store rather than here: this bridge knows what a read looks like, not what an
            // artifact id looks like. An id that is not one reaches the same 404 it reaches on the
            // direct path.
            guard Set(body.keys) == ["type", "session", "id"],
                  let session = body["session"] as? String, !session.isEmpty,
                  let id = body["id"] as? String, !id.isEmpty
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .image(session: session, id: id)
        default:
            // `readTypes` admitted a word this switch does not know, which means the two lists
            // have come apart. Fail closed rather than reading it as whichever case sits last —
            // that is how a misspelled `info` would have become a transcript.
            commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
            return
        }

        let answer = await commandRouter.read(read, sender: inbound.sender)
        let outcome = Self.outcome(of: read, answer: answer)
        commandResult(CloudCommandResult(status: outcome.status, code: outcome.code))
        guard running, lifecycleGeneration == ownedGeneration else { return }
        var payload: [String: Any] = ["read": read.name, "status": outcome.status]
        if let body = outcome.body {
            payload["body"] = body
        } else {
            // Whatever went wrong, the viewer gets a code it can branch on rather than silence.
            payload["error"] = outcome.error
        }
        guard JSONSerialization.isValidJSONObject(payload),
              let bytes = try? JSONSerialization.data(
                  withJSONObject: payload, options: [.withoutEscapingSlashes]
              )
        else {
            commandResult(CloudCommandResult(status: 500, code: "unserializable_read"))
            return
        }
        let channel = transcriptChannel(read.session)
        do {
            try await runPublication { [weak self] in
                guard let self else { throw CancellationError() }
                try await self.publish(
                    bytes, channel: channel, lifecycleGeneration: ownedGeneration
                )
            }
        } catch {
            // The answer's own channel is the only way back to the asker, so a publication that
            // cannot leave has nothing to report with. It is recorded here and the viewer's read
            // ages out at its end, which is the honest end state for a bridge that is going down.
            commandResult(CloudCommandResult(status: 503, code: "read_answer_undeliverable"))
        }
    }

    /// One answered read, resolved into the two things the payload can hold.
    ///
    /// It replaces the older pair of "parse the body, or lift its `error`" lines because a
    /// picture is neither: its route answers with the PNG and a header, and turning that into
    /// something an envelope can carry — or refusing it — is a decision, not a forward.
    private struct ReadOutcome {
        let status: Int
        let code: String?
        let body: [String: Any]?
        let error: [String: Any]?

        static func refused(_ status: Int, _ code: String, _ message: String,
                            _ extra: [String: Any] = [:]) -> ReadOutcome {
            var error: [String: Any] = ["code": code, "message": message]
            extra.forEach { error[$0.key] = $0.value }
            return ReadOutcome(status: status, code: code, body: nil, error: error)
        }
    }

    private static func outcome(of read: CloudHeadlessRead,
                                answer: CloudReadResult) -> ReadOutcome {
        if answer.status == 200, case .image(_, let id) = read {
            return imageOutcome(id: id, answer: answer)
        }
        let parsed = (try? JSONSerialization.jsonObject(with: answer.body)) as? [String: Any]
        if answer.status == 200, let parsed {
            return ReadOutcome(status: 200, code: nil, body: parsed, error: nil)
        }
        let error = (parsed?["error"] as? [String: Any])
            ?? ["code": "read_failed", "message": "This read could not be answered."]
        return ReadOutcome(status: answer.status, code: error["code"] as? String,
                           body: nil, error: error)
    }

    /// The picture itself, base64 in a payload field, or the sentence saying why it stayed home.
    ///
    /// **The bytes are the answer and there is nothing shorter to send.** A URL cannot be: this
    /// Mac is not reachable from the console, which is the entire reason a relay exists. A
    /// redemption ticket cannot be either — redeeming it would need the same envelope this one
    /// already is. So the only question left is how large a picture may be, and that is the
    /// relay's per-envelope cap arithmetic and nothing else.
    ///
    /// The size is checked after the route has read the file rather than before. It costs one
    /// discarded read of at most 12 MiB, in the 780-byte band where a stored artifact is too
    /// large to cross — and buying it back would mean this bridge asking the artifact store its
    /// own questions, which is a layer it does not otherwise know exists.
    private static func imageOutcome(id: String, answer: CloudReadResult) -> ReadOutcome {
        guard let mediaType = answer.contentType, mediaType == "image/png" else {
            return .refused(415, "image_media_type_unsupported",
                            "That artifact is not a PNG and does not cross this connection.")
        }
        guard !answer.body.isEmpty else {
            return .refused(502, "image_empty", "That image arrived with no bytes in it.")
        }
        let limit = cloudImageMaxEncodedBytes
        guard answer.body.count <= limit else {
            return .refused(413, "image_too_large_for_cloud",
                            "That image is larger than one cloud envelope can carry.",
                            ["byte_count": answer.body.count, "limit_bytes": limit])
        }
        return ReadOutcome(
            status: 200, code: nil,
            body: ["id": id, "media_type": mediaType, "byte_count": answer.body.count,
                   "data": answer.body.base64EncodedString()],
            error: nil)
    }
}
