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

struct CloudCommandResult: Equatable, Sendable {
    let status: Int
    let code: String?
}

/// Both cloud and HTTP commands enter this door. The production implementation below converts a
/// typed command back into an in-process request so authentication, idempotency, menu safety,
/// image validation, audit, and the actual terminal operation remain one implementation.
protocol CloudCommandRouting: Sendable {
    func route(_ command: CloudHeadlessCommand, sender: String,
               idempotencyKey: String) async -> CloudCommandResult
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
        guard allowCloudCommands() else {
            commandResult(CloudCommandResult(status: 403, code: "cloud_commands_disabled"))
            return
        }
        let wantedChannel = "ctl/" + Self.channelSegment(identity.machineID)
        guard inbound.channel == wantedChannel else {
            commandResult(CloudCommandResult(status: 409, code: "wrong_machine"))
            return
        }
        guard let object = try? JSONSerialization.jsonObject(with: inbound.plaintext),
              let body = object as? [String: Any], let type = body["type"] as? String
        else {
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
}
