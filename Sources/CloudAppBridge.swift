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
    case board(body: Data)
    case timeline(body: Data)
    case send(session: String, text: String, images: [String])
    case answer(session: String, key: String)
    case start(place: String, assistant: String, model: String)
    case resume(place: String, session: String, assistant: String)
    case end(session: String, acceptLoss: Bool, closeabilityVersion: String?)
    case focus(session: String)
    case shellKill(session: String, shell: String)
    case scheduleCreate(body: Data)
    case scheduleUpdate(id: String, body: Data)
    case scheduleDelete(id: String)
    case scheduleRun(id: String)
    case snippetCreate(body: Data)
    case snippetUpdate(id: String, body: Data)
    case snippetDelete(id: String)
    case snippetOrder(body: Data)
    case scheduleWebhookBind(requestID: String, hookID: String, scheduleID: String,
                             replaceHookID: String?)
    case pushSubscribe(body: Data)
    case pushUnsubscribe(id: String)
    case pushTest(session: String)
    case voice(audio: String, rate: Int)
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
enum CloudTranscriptPriority: String, Equatable, Sendable {
    case background
    case foreground
}

enum CloudHeadlessRead: Equatable, Sendable {
    case board(session: String, request: String, project: String, item: String)
    case timeline(session: String, request: String, project: String, entry: String,
                  cursor: String, environment: String, category: String, upcoming: Bool)
    case transcript(session: String, limit: Int, priority: CloudTranscriptPriority)
    case info(session: String, parts: String)
    case agent(session: String, agent: String, limit: Int)
    case shell(session: String, shell: String, bytes: Int)
    case skills(session: String)
    case git(session: String)
    case screen(session: String)
    /// One image already referenced by a message in that session's transcript.
    ///
    /// The reference — id, media type, byte count, pixel size, expiry — has always crossed,
    /// because it travels inside the transcript body. What could not cross was the picture: the
    /// browser turns that reference into `<img src="/v1/artifacts/images/:id">`, a *same-origin
    /// relative* URL, and on this path the origin is the hosted console rather than this Mac. The
    /// console serves no such route, so every image in every transcript was a broken-image icon —
    /// the one failure in this transport that looked like the reader's own fault.
    case image(session: String, id: String)
    /// The inert text documents the existing authenticated local route offers for this Session.
    case documents(session: String)
    /// One document under the existing project or task root, correlated by an opaque request id.
    case document(session: String, request: String, scope: String, task: String, path: String)
    case places(session: String, request: String)
    case projectWorktrees(session: String, request: String, project: String)
    case pastSessions(session: String, request: String, place: String, assistant: String)
    case schedules(session: String, request: String)
    case snippets(session: String, request: String)
    case schedule(session: String, request: String, id: String)
    case pushKey(session: String, request: String)

    /// The session this read is about — also the channel its answer is published on.
    var session: String {
        switch self {
        case .board(let session, _, _, _): return session
        case .timeline(let session, _, _, _, _, _, _, _): return session
        case .transcript(let session, _, _): return session
        case .info(let session, _): return session
        case .agent(let session, _, _): return session
        case .shell(let session, _, _): return session
        case .skills(let session): return session
        case .git(let session): return session
        case .screen(let session): return session
        case .image(let session, _): return session
        case .documents(let session): return session
        case .document(let session, _, _, _, _): return session
        case .places(let session, _): return session
        case .projectWorktrees(let session, _, _): return session
        case .pastSessions(let session, _, _, _): return session
        case .schedules(let session, _): return session
        case .snippets(let session, _): return session
        case .schedule(let session, _, _): return session
        case .pushKey(let session, _): return session
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
        case .screen: return "screen"
        case .image(_, let id): return "image." + id
        case .documents: return "documents"
        case .document(_, let request, _, _, _): return "read:" + request
        case .board(_, let request, _, _), .timeline(_, let request, _, _, _, _, _, _),
             .places(_, let request), .projectWorktrees(_, let request, _),
             .pastSessions(_, let request, _, _), .schedule(_, let request, _),
             .schedules(_, let request), .snippets(_, let request),
             .pushKey(_, let request): return "read:" + request
        }
    }
}

struct CloudCommandResult: Equatable, Sendable {
    let status: Int
    let code: String?
    let body: Data

    init(status: Int, code: String?, body: Data = Data()) {
        self.status = status
        self.code = code
        self.body = body
    }
}

/// A read's answer, kept as the route's own bytes rather than a parsed object: a refusal is an
/// answer too, and forwarding the typed `{"error":{"code",…}}` unchanged is what lets a browser
/// on the cloud path branch on exactly the codes it already branches on over the tunnel.
struct CloudReadResult: Equatable, Sendable {
    let status: Int
    let body: Data
    /// What those bytes are, when they are not JSON.
    ///
    /// Image and document reads need it: their routes answer with bytes and say what those bytes
    /// are in a header, while an envelope has no headers. JSON reads leave this nil rather than
    /// restating `application/json` in a second place.
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
        if case .scheduleWebhookBind(let requestID, let hookID, let scheduleID,
                                     let replaceHookID) = command {
            return await ScheduleWebhookBindingCoordinator.shared.bind(
                requestID: requestID, hookID: hookID, scheduleID: scheduleID,
                replaceHookID: replaceHookID, sender: sender)
        }
        if case .voice(let audio, let rate) = command {
            return await CloudVoiceCommandRouter.shared.route(
                audio: audio, rate: rate, sender: sender, idempotencyKey: idempotencyKey)
        }
        let response = await server.routeVerifiedCloudCommand(
            command, sender: sender, idempotencyKey: idempotencyKey
        )
        let object = (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]
        let error = object?["error"] as? [String: Any]
        return CloudCommandResult(status: response.status, code: error?["code"] as? String,
                                  body: response.body)
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

/// Serializes the RemoteServer's full Cloud snapshots. Session observations are latest-value
/// state, except that an authoritative inventory is a deletion barrier which must be delivered
/// before a later incomplete observation. The pending suffix is therefore bounded to one such
/// barrier plus the newest incomplete reading instead of growing a Task chain.
final class CloudSnapshotPublicationQueue: @unchecked Sendable {
    typealias Work = @Sendable () async throws -> Void
    static let maximumPending = 3
    private struct Item { let authoritative: Bool?; let work: Work }
    private let lock = NSLock()
    private var pending: [Item] = []
    private var worker: Task<Void, Never>?
    private var generation: UInt64 = 0

    func enqueueSessions(_ payload: Data, work: @escaping Work) {
        let authoritative = Self.authoritative(payload)
        lock.lock()
        let item = Item(authoritative: authoritative, work: work)
        if authoritative {
            pending.removeAll { $0.authoritative != nil }
        } else {
            pending.removeAll { $0.authoritative == false }
        }
        pending.append(item)
        precondition(pending.count <= Self.maximumPending)
        startWorkerLocked()
        lock.unlock()
    }

    func enqueue(_ work: @escaping Work) {
        lock.lock()
        pending.removeAll { $0.authoritative == nil }
        pending.append(Item(authoritative: nil, work: work))
        precondition(pending.count <= Self.maximumPending)
        startWorkerLocked()
        lock.unlock()
    }

    @discardableResult
    func cancelAndReset() -> Task<Void, Never>? {
        lock.lock()
        generation &+= 1
        pending.removeAll()
        let previous = worker
        worker = nil
        lock.unlock()
        previous?.cancel()
        return previous
    }

    private func startWorkerLocked() {
        guard worker == nil else { return }
        let ownedGeneration = generation
        worker = Task { [weak self] in await self?.drain(generation: ownedGeneration) }
    }

    private func drain(generation ownedGeneration: UInt64) async {
        while !Task.isCancelled, let item = take(generation: ownedGeneration) {
            try? await item.work()
        }
    }

    private func take(generation ownedGeneration: UInt64) -> Item? {
        lock.lock()
        defer { lock.unlock() }
        guard generation == ownedGeneration, !Task.isCancelled else { return nil }
        guard !pending.isEmpty else { worker = nil; return nil }
        return pending.removeFirst()
    }

    private static func authoritative(_ payload: Data) -> Bool {
        guard let root = try? JSONSerialization.jsonObject(with: payload) as? [String: Any],
              let scan = root["scan"] as? [String: Any] else { return false }
        return scan["complete"] as? Bool == true
            || scan["emptyAuthoritative"] as? Bool == true
    }
}

/// Connects the app's existing full-snapshot and HTTP-command seams to CloudTransport.
///
/// Construction has no side effects. `start()` is the explicit attachment/configuration point,
/// and inbound commands have a second, separately injected gate whose default is always false.
actor CloudAppBridge {
    static let machineReplySession = "__clawdline_machine__"
    static let sessionInventoryID = "__clawdline_inventory_v1__"
    static let sessionInventoryLimit = 512
    typealias CommandGate = @Sendable () -> Bool
    typealias Milliseconds = @Sendable () -> UInt64
    typealias CommandResultObserver = @Sendable (CloudCommandResult) -> Void
    typealias DiagnosticLogger = @Sendable (String) -> Void
    typealias TransportReadyObserver = @Sendable (UInt64) -> Void

    private enum ReadLane: String { case foreground, background }
    private struct PendingRead: Sendable {
        let read: CloudHeadlessRead
        let sender: String
        let lifecycleGeneration: UInt64
        let admittedAt: UInt64
    }

    private let transport: any CloudTransporting
    private let identity: CloudAppIdentity
    private let sequencing: any CloudEnvelopeSequencing
    private let allowCloudCommands: CommandGate
    private let commandRouter: any CloudCommandRouting
    private let nowMilliseconds: Milliseconds
    private let commandResult: CommandResultObserver
    private let diagnostic: DiagnosticLogger

    private var commandTask: Task<Void, Never>?
    private var readyTask: Task<Void, Never>?
    private var connectTask: Task<Void, Error>?
    private var publicationTasks: [UUID: Task<Void, Error>] = [:]
    private var foregroundReadTask: Task<Void, Never>?
    private var backgroundReadTask: Task<Void, Never>?
    private var foregroundReadActive = false
    private var backgroundReadActive = false
    private var foregroundReads: [PendingRead] = []
    private var backgroundReads: [PendingRead] = []
    private var transportReady: TransportReadyObserver = { _ in }
    private var lifecycleGeneration: UInt64 = 0
    private var starting = false
    private var running = false
    private var publishedSessionIDs = Set<String>()
    private var publishedSessionRows: [String: Data] = [:]
    private var publishedSessionInventory: Data?

    init(
        transport: any CloudTransporting,
        identity: CloudAppIdentity,
        sequencing: any CloudEnvelopeSequencing,
        allowCloudCommands: @escaping CommandGate = { false },
        commandRouter: any CloudCommandRouting = RemoteServerCloudCommandRouter(),
        nowMilliseconds: @escaping Milliseconds = {
            UInt64(Date().timeIntervalSince1970 * 1_000)
        },
        commandResult: @escaping CommandResultObserver = { _ in },
        diagnostic: @escaping DiagnosticLogger = { _ in }
    ) {
        self.transport = transport
        self.identity = identity
        self.sequencing = sequencing
        self.allowCloudCommands = allowCloudCommands
        self.commandRouter = commandRouter
        self.nowMilliseconds = nowMilliseconds
        self.commandResult = commandResult
        self.diagnostic = diagnostic
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
                || foregroundReadTask != nil || backgroundReadTask != nil
                || !publicationTasks.isEmpty
        else { return }
        lifecycleGeneration &+= 1
        starting = false
        running = false
        let command = commandTask
        let ready = readyTask
        let connect = connectTask
        let publications = Array(publicationTasks.values)
        let readTasks = [foregroundReadTask, backgroundReadTask].compactMap { $0 }
        commandTask = nil
        readyTask = nil
        connectTask = nil
        publicationTasks.removeAll()
        foregroundReadTask = nil
        backgroundReadTask = nil
        foregroundReadActive = false
        backgroundReadActive = false
        foregroundReads.removeAll()
        backgroundReads.removeAll()
        connect?.cancel()
        command?.cancel()
        ready?.cancel()
        publications.forEach { $0.cancel() }
        readTasks.forEach { $0.cancel() }
        await transport.shutdown()
        _ = await connect?.result
        await command?.value
        await ready?.value
        for publication in publications {
            _ = await publication.result
        }
        for task in readTasks { await task.value }
        publishedSessionIDs.removeAll()
        publishedSessionRows.removeAll()
        publishedSessionInventory = nil
    }

    func isRunning() -> Bool { running }

    func setTransportReadyObserver(_ observer: @escaping TransportReadyObserver) {
        transportReady = observer
    }

    /// Accepts the exact JSON bytes produced for local SSE, then fans its complete session rows
    /// out by channel. A complete authoritative scan also sends tombstones for rows that vanished.
    func publishSessions(_ payload: Data, force: Bool = false) async throws {
        let ownedGeneration = lifecycleGeneration
        try await runPublication { [weak self] in
            guard let self else { throw CancellationError() }
            try await self.publishSessionsOwned(
                payload, force: force, lifecycleGeneration: ownedGeneration
            )
        }
    }

    private func publishSessionsOwned(
        _ payload: Data, force: Bool, lifecycleGeneration ownedGeneration: UInt64
    ) async throws {
        try requireActivePublication(lifecycleGeneration: ownedGeneration)
        guard let root = try? JSONSerialization.jsonObject(with: payload) as? [String: Any],
              let sessions = root["sessions"] as? [[String: Any]],
              let at = root["at"], let scan = root["scan"] as? [String: Any]
        else { throw CloudAppBridgeError.malformedSessions }
        let authoritative = (scan["complete"] as? Bool) == true
            || (scan["emptyAuthoritative"] as? Bool) == true
        let ids = sessions.compactMap { $0["id"] as? String }
        guard ids.count == sessions.count,
              ids.allSatisfy({ !$0.isEmpty && $0 != Self.sessionInventoryID }),
              Set(ids).count == ids.count,
              !authoritative || ids.count <= Self.sessionInventoryLimit else {
            throw CloudAppBridgeError.malformedSessions
        }

        let startedAt = nowMilliseconds()
        let current = Set(ids)
        var changed = 0
        var skipped = 0
        for (session, id) in zip(sessions, ids) {
            try requireActivePublication(lifecycleGeneration: ownedGeneration)
            let stable = try JSONSerialization.data(
                withJSONObject: session, options: [.sortedKeys, .withoutEscapingSlashes]
            )
            if !force, publishedSessionRows[id] == stable {
                skipped += 1
                continue
            }
            let full: [String: Any] = ["session": session, "at": at, "scan": scan]
            try await publishJSON(
                full, channel: sessionChannel(id), lifecycleGeneration: ownedGeneration
            )
            publishedSessionRows[id] = stable
            changed += 1
        }

        let removed = authoritative ? publishedSessionIDs.subtracting(current) : []
        if authoritative {
            for id in removed {
                try requireActivePublication(lifecycleGeneration: ownedGeneration)
                let tombstone: [String: Any] = [
                    "session": NSNull(), "deleted": true, "at": at, "scan": scan,
                ]
                try await publishJSON(
                    tombstone, channel: sessionChannel(id),
                    lifecycleGeneration: ownedGeneration
                )
                publishedSessionRows[id] = nil
            }
            try requireActivePublication(lifecycleGeneration: ownedGeneration)
            publishedSessionIDs = current
        } else {
            try requireActivePublication(lifecycleGeneration: ownedGeneration)
            publishedSessionIDs.formUnion(current)
        }
        var inventoryPublished = false
        if authoritative {
            let inventory: [String: Any] = [
                "inventory": ["version": 1, "sessions": current.sorted()] as [String: Any],
            ]
            let stable = try JSONSerialization.data(
                withJSONObject: inventory, options: [.sortedKeys, .withoutEscapingSlashes]
            )
            if force || stable != publishedSessionInventory {
                try await publishJSON(
                    inventory, channel: sessionChannel(Self.sessionInventoryID),
                    lifecycleGeneration: ownedGeneration
                )
                publishedSessionInventory = stable
                inventoryPublished = true
            }
        }
        diagnostic("cloud: sessions published rows=\(sessions.count) changed=\(changed) "
            + "skipped=\(skipped) tombstones=\(removed.count) inventory=\(inventoryPublished) "
            + "force=\(force) total_ms=\(Self.elapsedMilliseconds(from: startedAt, to: nowMilliseconds()))")
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
        _ plaintext: Data, channel: String, lifecycleGeneration ownedGeneration: UInt64,
        readTraceID: String? = nil, scheduledAt: UInt64? = nil
    ) async throws {
        try requireActivePublication(lifecycleGeneration: ownedGeneration)
        var stage = "task_wait"
        var startedAt = scheduledAt ?? nowMilliseconds()
        // Only a solicited read supplies this opaque trace. Never log payload, channel, keys,
        // envelope sequence, or raw errors. The transport may buffer during reconnect; neither
        // socket-send completion nor a relay ACK is observed at this interface.
        func trace(_ outcome: String) {
            guard let readTraceID else { return }
            diagnostic("cloud: publication id=\(readTraceID) publication_stage=\(stage) "
                + "duration_ms=\(Self.elapsedMilliseconds(from: startedAt, to: nowMilliseconds())) "
                + "outcome=\(outcome) socket_send=not_observed ack=not_observed")
        }
        trace("complete")
        do {
            stage = "sequence"; startedAt = nowMilliseconds(); trace("begin")
            let sequence = try await sequencing.nextSequence(sender: identity.deviceID)
            trace("complete")
            stage = "lifecycle_check"; startedAt = nowMilliseconds()
            try requireActivePublication(lifecycleGeneration: ownedGeneration)
            stage = "seal"; startedAt = nowMilliseconds(); trace("begin")
            let envelope = try CloudEnvelope.seal(
                plaintext, ch: channel, seq: sequence, ts: nowMilliseconds(),
                envelopeClass: .stream, keyID: identity.keyID, sender: identity.deviceID,
                masterSecret: identity.masterSecret, signingKey: identity.signingKey
            )
            trace("complete")
            stage = "lifecycle_check"; startedAt = nowMilliseconds()
            try requireActivePublication(lifecycleGeneration: ownedGeneration)
            stage = "transport_publish"; startedAt = nowMilliseconds(); trace("begin")
            try await transport.publish(envelope: envelope)
            trace("complete")
        } catch {
            trace("failed")
            throw error
        }
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
        // Registering this already-paired browser as a notification destination is read-level on
        // the direct route. It must not inherit the separate switch for typing into a session.
        let readLevelCommand = requestedType == "push-subscribe"
            || requestedType == "push-unsubscribe" || requestedType == "push-test"
        guard readLevelCommand || allowCloudCommands() else {
            commandResult(CloudCommandResult(status: 403, code: "cloud_commands_disabled"))
            return
        }
        guard let body = parsed, let type = requestedType else {
            commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
            return
        }

        let command: CloudHeadlessCommand
        var commandReply: (session: String, name: String)?
        switch type {
        case "send":
            // A page already open on the previous hosted build has no `request`. Keep accepting
            // that wire shape while the new one adds an execution answer; reloading must improve
            // delivery semantics, not become a prerequisite for sending at all.
            let sendKeys = Set(body.keys)
            guard inbound.commandClass == .ctl,
                  sendKeys == ["type", "session", "text", "images"]
                    || sendKeys == ["type", "session", "request", "text", "images"],
                  let session = body["session"] as? String, !session.isEmpty,
                  let text = body["text"] as? String, let images = body["images"] as? [String]
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .send(session: session, text: text, images: images)
            if sendKeys.contains("request") {
                guard let request = Self.requestName(body["request"]) else {
                    commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                    return
                }
                commandReply = (session, "action:" + request)
            }
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
        case "start":
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "place", "assistant", "model"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let place = body["place"] as? String, !place.isEmpty,
                  let assistant = body["assistant"] as? String,
                  let model = body["model"] as? String
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .start(place: place, assistant: assistant, model: model)
            commandReply = (session, "action:" + request)
        case "resume":
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "place", "past", "assistant"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let place = body["place"] as? String, !place.isEmpty,
                  let past = body["past"] as? String, !past.isEmpty,
                  let assistant = body["assistant"] as? String
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .resume(place: place, session: past, assistant: assistant)
            commandReply = (session, "action:" + request)
        case "end":
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "accept_loss",
                                     "expected_closeability_version"],
                  let session = body["session"] as? String, !session.isEmpty,
                  let request = Self.requestName(body["request"]),
                  let acceptLoss = body["accept_loss"] as? Bool,
                  let closeability = body["expected_closeability_version"] as? String
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .end(session: session, acceptLoss: acceptLoss,
                           closeabilityVersion: closeability.isEmpty ? nil : closeability)
            commandReply = (session, "action:" + request)
        case "focus":
            // Show on Mac: the same named route the direct page presses, behind the same write
            // switch, and answered so the viewer can say whether this Mac managed it.
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request"],
                  let session = body["session"] as? String, !session.isEmpty,
                  let request = Self.requestName(body["request"])
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .focus(session: session)
            commandReply = (session, "action:" + request)
        case "shell-kill":
            // Stop a background command: the same named route and write switch as the direct
            // page, answered so a refusal such as `unidentified` reaches the panel in its own word.
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "shell"],
                  let session = body["session"] as? String, !session.isEmpty,
                  let shell = body["shell"] as? String, !shell.isEmpty,
                  let request = Self.requestName(body["request"])
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .shellKill(session: session, shell: shell)
            commandReply = (session, "action:" + request)
        case "board-command":
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "command"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let object = body["command"] as? [String: Any],
                  let data = try? JSONSerialization.data(withJSONObject: object),
                  data.count <= 64 * 1024
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .board(body: data)
            commandReply = (session, "action:" + request)
        case "timeline-command":
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "command"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let object = body["command"] as? [String: Any],
                  let data = try? JSONSerialization.data(withJSONObject: object),
                  data.count <= 256 * 1024
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .timeline(body: data)
            commandReply = (session, "action:" + request)
        case "schedule-create", "schedule-update":
            let wanted: Set<String> = type == "schedule-create"
                ? ["type", "session", "request", "schedule"]
                : ["type", "session", "request", "id", "schedule"]
            guard inbound.commandClass == .ctl, Set(body.keys) == wanted,
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let schedule = body["schedule"] as? [String: Any],
                  JSONSerialization.isValidJSONObject(schedule),
                  let data = try? JSONSerialization.data(
                    withJSONObject: schedule, options: [.withoutEscapingSlashes])
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            if type == "schedule-create" {
                command = .scheduleCreate(body: data)
            } else {
                guard let id = body["id"] as? String, !id.isEmpty else {
                    commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                    return
                }
                command = .scheduleUpdate(id: id, body: data)
            }
            commandReply = (session, "action:" + request)
        case "schedule-delete", "schedule-run":
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "id"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let id = body["id"] as? String, !id.isEmpty
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = type == "schedule-delete" ? .scheduleDelete(id: id) : .scheduleRun(id: id)
            commandReply = (session, "action:" + request)
        case "snippet-create", "snippet-update":
            let wanted: Set<String> = type == "snippet-create"
                ? ["type", "session", "request", "snippet"]
                : ["type", "session", "request", "id", "snippet"]
            guard inbound.commandClass == .ctl, Set(body.keys) == wanted,
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let snippet = body["snippet"] as? [String: Any],
                  JSONSerialization.isValidJSONObject(snippet),
                  let data = try? JSONSerialization.data(
                    withJSONObject: snippet, options: [.withoutEscapingSlashes])
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            if type == "snippet-create" {
                command = .snippetCreate(body: data)
            } else {
                guard let id = body["id"] as? String, !id.isEmpty else {
                    commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                    return
                }
                command = .snippetUpdate(id: id, body: data)
            }
            commandReply = (session, "action:" + request)
        case "snippet-delete":
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "id"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let id = body["id"] as? String, !id.isEmpty
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .snippetDelete(id: id)
            commandReply = (session, "action:" + request)
        case "snippet-order":
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "ordering"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let ordering = body["ordering"] as? [String: Any],
                  JSONSerialization.isValidJSONObject(ordering),
                  let data = try? JSONSerialization.data(
                    withJSONObject: ordering, options: [.withoutEscapingSlashes])
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .snippetOrder(body: data)
            commandReply = (session, "action:" + request)
        case "schedule-webhook-bind-v1":
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "request_id", "hook_id", "schedule_id",
                                      "replace_hook_id"],
                  let requestID = body["request_id"] as? String,
                  UUID(uuidString: requestID) != nil, requestID == requestID.lowercased(),
                  let hookID = body["hook_id"] as? String,
                  let scheduleID = body["schedule_id"] as? String,
                  body["replace_hook_id"] is NSNull || body["replace_hook_id"] is String
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .scheduleWebhookBind(
                requestID: requestID, hookID: hookID, scheduleID: scheduleID,
                replaceHookID: body["replace_hook_id"] as? String)
            commandReply = (Self.machineReplySession, "action:" + requestID)
        case "push-subscribe":
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "subscription"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let subscription = body["subscription"] as? [String: Any],
                  JSONSerialization.isValidJSONObject(subscription),
                  let data = try? JSONSerialization.data(
                    withJSONObject: subscription, options: [.withoutEscapingSlashes])
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .pushSubscribe(body: data)
            commandReply = (session, "action:" + request)
        case "push-unsubscribe":
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "id"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let id = body["id"] as? String, !id.isEmpty
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .pushUnsubscribe(id: id)
            commandReply = (session, "action:" + request)
        case "push-test":
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "target"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let target = body["target"] as? String
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .pushTest(session: target)
            commandReply = (session, "action:" + request)
        case "voice":
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "audio", "rate"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let audio = body["audio"] as? String, !audio.isEmpty,
                  let rate = body["rate"] as? Int, rate == Int(RemoteServer.voiceRate)
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_command"))
                return
            }
            command = .voice(audio: audio, rate: rate)
            commandReply = (session, "action:" + request)
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
        if let reply = commandReply {
            await publishJSONAnswer(name: reply.name, session: reply.session,
                                    status: result.status, body: result.body,
                                    lifecycleGeneration: ownedGeneration)
        }
    }

    /// The reads a viewer may name. A closed set, checked before the write gate, so that adding
    /// another one is a deliberate edit here rather than a spelling that slipped past.
    ///
    /// This and the switch in `serveRead` are two lists that have to agree, which is why that
    /// switch ends in a `default` that refuses rather than in the last read: a word admitted here
    /// and unknown there fails closed instead of being parsed as whichever case happens to sit at
    /// the bottom. `every read type this bridge admits also parses` walks this set and asks each
    /// member for a well-formed body, so the two cannot come apart quietly.
    static let readTypes: Set<String> = [
        "transcript", "info", "agent", "shell", "skills", "git", "image",
        "documents", "document",
        "screen", "board", "timeline", "places", "project-worktrees", "past-sessions", "schedules",
        "snippets", "schedule", "push-key",
    ]

    private static func requestName(_ value: Any?) -> String? {
        guard let value = value as? String, !value.isEmpty, value.count <= 128,
              value.unicodeScalars.allSatisfy({ $0.value >= 0x20 && $0.value != 0x7f })
        else { return nil }
        return value
    }

    /// A cheap copy of the local route's lexical boundary, before any filesystem is touched.
    /// `ProjectDocuments.file` applies the same checks again against its resolved authoritative
    /// root; this one exists to keep malformed relay reads from reaching that router at all.
    private static func cloudDocumentPath(_ value: Any?) -> String? {
        guard let path = value as? String, !path.isEmpty, path.count <= 512,
              !path.hasPrefix("/"), !path.contains("\0"),
              path.unicodeScalars.allSatisfy({ $0.value >= 0x20 && $0.value != 0x7f })
        else { return nil }
        let parts = path.split(separator: "/", omittingEmptySubsequences: false)
        guard !parts.isEmpty, parts.count <= ProjectDocuments.maximumDepth,
              parts.allSatisfy({ !$0.isEmpty && !$0.hasPrefix(".") }),
              ProjectDocuments.readableExtensions.contains(
                  URL(fileURLWithPath: path).pathExtension.lowercased())
        else { return nil }
        return path
    }

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
            let keys = Set(body.keys)
            guard (keys == ["type", "session", "limit"]
                    || keys == ["type", "session", "limit", "priority"]),
                  let session = body["session"] as? String, !session.isEmpty,
                  let limit = body["limit"] as? Int, (1...1000).contains(limit)
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            let priority: CloudTranscriptPriority
            if let raw = body["priority"] as? String,
               let parsed = CloudTranscriptPriority(rawValue: raw) {
                priority = parsed
            } else if body["priority"] == nil {
                // A stale hosted tab can only be a person-driven reader: automatic refreshes and
                // agents learned to send `background` in the same release that added this field.
                priority = .foreground
            } else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .transcript(session: session, limit: limit, priority: priority)
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
        case "screen":
            guard Set(body.keys) == ["type", "session"],
                  let session = body["session"] as? String, !session.isEmpty
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .screen(session: session)
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
        case "documents":
            guard Set(body.keys) == ["type", "session"],
                  let session = body["session"] as? String, !session.isEmpty
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .documents(session: session)
        case "document":
            guard Set(body.keys) == ["type", "session", "request", "scope", "task", "path"],
                  let session = body["session"] as? String, !session.isEmpty,
                  let request = Self.requestName(body["request"]),
                  let scope = body["scope"] as? String,
                  let task = body["task"] as? String,
                  let path = Self.cloudDocumentPath(body["path"]),
                  (scope == "project" && task.isEmpty)
                    || (scope == "task" && OrchestratorDraft.isTaskID(task))
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .document(session: session, request: request, scope: scope,
                             task: task, path: path)
        case "board":
            guard Set(body.keys) == ["type", "session", "request", "project", "item"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let project = body["project"] as? String, project.count <= 200,
                  let item = body["item"] as? String, item.count <= 200
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .board(session: session, request: request, project: project, item: item)
        case "timeline":
            guard Set(body.keys) == ["type", "session", "request", "project", "entry",
                                      "cursor", "environment", "category", "upcoming"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let project = body["project"] as? String, project.count <= 200,
                  let entry = body["entry"] as? String, entry.count <= 200,
                  let cursor = body["cursor"] as? String, cursor.count <= 20,
                  let environment = body["environment"] as? String, environment.count <= 32,
                  let category = body["category"] as? String, category.count <= 32,
                  let upcoming = body["upcoming"] as? Bool
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .timeline(session: session, request: request, project: project, entry: entry,
                             cursor: cursor, environment: environment, category: category,
                             upcoming: upcoming)
        case "places":
            guard Set(body.keys) == ["type", "session", "request"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"])
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .places(session: session, request: request)
        case "project-worktrees":
            guard Set(body.keys) == ["type", "session", "request", "project"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let project = body["project"] as? String, !project.isEmpty
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .projectWorktrees(session: session, request: request, project: project)
        case "past-sessions":
            guard Set(body.keys) == ["type", "session", "request", "place", "assistant"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let place = body["place"] as? String, !place.isEmpty,
                  let assistant = body["assistant"] as? String
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .pastSessions(session: session, request: request,
                                 place: place, assistant: assistant)
        case "schedules":
            guard Set(body.keys) == ["type", "session", "request"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"])
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .schedules(session: session, request: request)
        case "snippets":
            guard Set(body.keys) == ["type", "session", "request"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"])
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .snippets(session: session, request: request)
        case "schedule":
            guard Set(body.keys) == ["type", "session", "request", "id"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let id = body["id"] as? String, !id.isEmpty
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .schedule(session: session, request: request, id: id)
        case "push-key":
            guard Set(body.keys) == ["type", "session", "request"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"])
            else {
                commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
                return
            }
            read = .pushKey(session: session, request: request)
        default:
            // `readTypes` admitted a word this switch does not know, which means the two lists
            // have come apart. Fail closed rather than reading it as whichever case sits last —
            // that is how a misspelled `info` would have become a transcript.
            commandResult(CloudCommandResult(status: 400, code: "malformed_read"))
            return
        }

        enqueueRead(read, sender: inbound.sender, lifecycleGeneration: ownedGeneration)
    }

    private func enqueueRead(
        _ read: CloudHeadlessRead, sender: String, lifecycleGeneration ownedGeneration: UInt64
    ) {
        let lane: ReadLane
        if case .transcript(_, _, .foreground) = read { lane = .foreground }
        else { lane = .background }
        let depth = lane == .foreground
            ? foregroundReads.count + (foregroundReadActive ? 1 : 0)
            : backgroundReads.count + (backgroundReadActive ? 1 : 0)
        let limit = lane == .foreground ? 4 : 16
        guard depth < limit else {
            commandResult(CloudCommandResult(status: 429, code: "cloud_read_busy"))
            diagnostic("cloud: read refused read=\(read.name) lane=\(lane.rawValue) "
                + "depth=\(depth) limit=\(limit) code=cloud_read_busy")
            Task { [weak self] in
                await self?.publishBusyRead(
                    read, lane: lane, limit: limit,
                    lifecycleGeneration: ownedGeneration
                )
            }
            return
        }
        let pending = PendingRead(
            read: read, sender: sender, lifecycleGeneration: ownedGeneration,
            admittedAt: nowMilliseconds()
        )
        if lane == .foreground {
            foregroundReads.append(pending)
            if foregroundReadTask == nil {
                foregroundReadTask = Task { [weak self] in
                    await self?.drainReads(.foreground, lifecycleGeneration: ownedGeneration)
                }
            }
        } else {
            backgroundReads.append(pending)
            if backgroundReadTask == nil {
                backgroundReadTask = Task { [weak self] in
                    await self?.drainReads(.background, lifecycleGeneration: ownedGeneration)
                }
            }
        }
        diagnostic("cloud: read admitted read=\(read.name) lane=\(lane.rawValue) "
            + "depth=\(depth + 1) limit=\(limit)")
    }

    private func publishBusyRead(
        _ read: CloudHeadlessRead, lane: ReadLane, limit: Int,
        lifecycleGeneration ownedGeneration: UInt64
    ) async {
        let payload: [String: Any] = [
            "read": read.name, "status": 429,
            "error": [
                "code": "cloud_read_busy",
                "message": "That Cloud read lane is full; retry shortly.",
                "lane": lane.rawValue, "limit": limit, "retry_after": 1,
            ] as [String: Any],
        ]
        guard running, lifecycleGeneration == ownedGeneration,
              let bytes = try? JSONSerialization.data(
                  withJSONObject: payload, options: [.withoutEscapingSlashes]
              )
        else { return }
        do {
            try await runPublication { [weak self] in
                guard let self else { throw CancellationError() }
                try await self.publish(
                    bytes, channel: transcriptChannel(read.session),
                    lifecycleGeneration: ownedGeneration
                )
            }
            diagnostic("cloud: read refusal delivered read=\(read.name) lane=\(lane.rawValue) "
                + "status=429 code=cloud_read_busy")
        } catch {
            commandResult(CloudCommandResult(status: 503, code: "read_answer_undeliverable"))
        }
    }

    private func drainReads(
        _ lane: ReadLane, lifecycleGeneration ownedGeneration: UInt64
    ) async {
        while running, lifecycleGeneration == ownedGeneration, !Task.isCancelled {
            let next: PendingRead?
            if lane == .foreground {
                next = foregroundReads.isEmpty ? nil : foregroundReads.removeFirst()
                foregroundReadActive = next != nil
            } else {
                next = backgroundReads.isEmpty ? nil : backgroundReads.removeFirst()
                backgroundReadActive = next != nil
            }
            guard let next else { break }
            await performRead(
                next.read, sender: next.sender,
                lifecycleGeneration: next.lifecycleGeneration, admittedAt: next.admittedAt
            )
            if lane == .foreground { foregroundReadActive = false }
            else { backgroundReadActive = false }
        }
        if lane == .foreground { foregroundReadTask = nil }
        else { backgroundReadTask = nil }
    }

    private func performRead(
        _ read: CloudHeadlessRead, sender: String,
        lifecycleGeneration ownedGeneration: UInt64, admittedAt: UInt64
    ) async {
        let traceID = String(UUID().uuidString.prefix(8)).lowercased()
        let receivedAt = nowMilliseconds()
        let safeSender = Self.channelSegment(sender)
        let safeSession = Self.channelSegment(read.session)
        let kind: String
        if case .board = read { kind = "board" } else { kind = "other" }
        diagnostic("cloud: read received id=\(traceID) read=\(read.name) "
            + "sender=\(safeSender) session=\(safeSession) kind=\(kind) "
            + "queue_ms=\(Self.elapsedMilliseconds(from: admittedAt, to: receivedAt))")
        let answer = await commandRouter.read(read, sender: sender)
        let routedAt = nowMilliseconds()
        let outcome = Self.outcome(of: read, answer: answer)
        diagnostic("cloud: read routed id=\(traceID) read=\(read.name) "
            + "route_ms=\(Self.elapsedMilliseconds(from: receivedAt, to: routedAt)) "
            + "status=\(outcome.status) bytes=\(answer.body.count)")
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
        let publishStartedAt = nowMilliseconds()
        do {
            try await runPublication { [weak self] in
                guard let self else { throw CancellationError() }
                try await self.publish(
                    bytes, channel: channel, lifecycleGeneration: ownedGeneration,
                    readTraceID: traceID, scheduledAt: publishStartedAt
                )
            }
            let deliveredAt = nowMilliseconds()
            diagnostic("cloud: read delivered id=\(traceID) read=\(read.name) "
                + "publish_ms=\(Self.elapsedMilliseconds(from: publishStartedAt, to: deliveredAt)) "
                + "total_ms=\(Self.elapsedMilliseconds(from: receivedAt, to: deliveredAt)) "
                + "status=\(outcome.status)")
        } catch {
            // The answer's own channel is the only way back to the asker, so a publication that
            // cannot leave has nothing to report with. It is recorded here and the viewer's read
            // ages out at its end, which is the honest end state for a bridge that is going down.
            commandResult(CloudCommandResult(status: 503, code: "read_answer_undeliverable"))
            let failedAt = nowMilliseconds()
            diagnostic("cloud: read delivery_failed id=\(traceID) read=\(read.name) "
                + "publish_ms=\(Self.elapsedMilliseconds(from: publishStartedAt, to: failedAt)) "
                + "total_ms=\(Self.elapsedMilliseconds(from: receivedAt, to: failedAt))")
        }
    }

    private static func elapsedMilliseconds(from start: UInt64, to end: UInt64) -> UInt64 {
        guard end >= start else { return 0 }
        return end - start
    }

    private func publishJSONAnswer(
        name: String, session: String, status: Int, body: Data,
        lifecycleGeneration ownedGeneration: UInt64
    ) async {
        let parsed = (try? JSONSerialization.jsonObject(with: body)) as? [String: Any]
        var payload: [String: Any] = ["read": name, "status": status]
        if (200..<300).contains(status), let parsed {
            payload["body"] = parsed
        } else {
            payload["error"] = (parsed?["error"] as? [String: Any])
                ?? ["code": "command_failed", "message": "This command could not be completed."]
        }
        guard running, lifecycleGeneration == ownedGeneration,
              JSONSerialization.isValidJSONObject(payload),
              let bytes = try? JSONSerialization.data(
                withJSONObject: payload, options: [.withoutEscapingSlashes])
        else { return }
        do {
            try await runPublication { [weak self] in
                guard let self else { throw CancellationError() }
                try await self.publish(bytes, channel: transcriptChannel(session),
                                       lifecycleGeneration: ownedGeneration)
            }
        } catch {
            commandResult(CloudCommandResult(status: 503, code: "command_answer_undeliverable"))
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
        if answer.status == 200, case .documents = read {
            return documentListingOutcome(answer: answer)
        }
        if answer.status == 200,
           case .document(_, _, let scope, let task, let path) = read {
            return documentOutcome(scope: scope, task: task, path: path, answer: answer)
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

    /// Strip the local HTTP address and duplicate label before the listing enters an envelope.
    /// The relative document path is retained because it is the requested identity; no resolved
    /// root or filesystem path is present in the route's response or the payload built here.
    private static func documentListingOutcome(answer: CloudReadResult) -> ReadOutcome {
        guard let object = (try? JSONSerialization.jsonObject(with: answer.body)) as? [String: Any],
              Set(object.keys) == ["documents"],
              let sourceRows = object["documents"] as? [[String: Any]],
              sourceRows.count <= ProjectDocuments.maximumListed
        else {
            return .refused(502, "document_listing_invalid",
                            "The Mac returned an invalid document listing.")
        }
        var rows: [[String: Any]] = []
        for row in sourceRows {
            guard let source = row["source"] as? String,
                  let path = cloudDocumentPath(row["path"]),
                  row["label"] as? String == path,
                  let bytes = row["bytes"] as? Int, (0...ProjectDocuments.maximumBytes).contains(bytes),
                  let modified = row["modified"] as? NSNumber,
                  modified.doubleValue.isFinite,
                  row["url"] is String
            else {
                return .refused(502, "document_listing_invalid",
                                "The Mac returned invalid document metadata.")
            }
            if source == "project" {
                guard Set(row.keys) == ["source", "path", "label", "bytes", "modified", "url"]
                else {
                    return .refused(502, "document_listing_invalid",
                                    "The Mac returned unexpected document metadata.")
                }
                rows.append(["scope": "project", "path": path, "bytes": bytes,
                             "modified": modified])
            } else if source == "task" {
                guard Set(row.keys)
                        == ["source", "path", "label", "bytes", "modified", "url", "task"],
                      let task = row["task"] as? [String: Any],
                      Set(task.keys) == ["id", "title"],
                      let id = task["id"] as? String, OrchestratorDraft.isTaskID(id),
                      let title = task["title"] as? String, title.count <= 300
                else {
                    return .refused(502, "document_listing_invalid",
                                    "The Mac returned invalid task document metadata.")
                }
                rows.append(["scope": "task", "task": id, "title": title, "path": path,
                             "bytes": bytes, "modified": modified])
            } else {
                return .refused(502, "document_listing_invalid",
                                "The Mac returned an unknown document scope.")
            }
        }
        return ReadOutcome(status: 200, code: nil, body: ["documents": rows], error: nil)
    }

    /// Put only inert UTF-8 text inside the encrypted answer, with a count the browser rechecks.
    private static func documentOutcome(
        scope: String, task: String, path: String, answer: CloudReadResult
    ) -> ReadOutcome {
        let allowed = ["text/markdown; charset=utf-8", "text/plain; charset=utf-8"]
        guard let mediaType = answer.contentType, allowed.contains(mediaType) else {
            return .refused(415, "document_media_type_unsupported",
                            "That file is not an inert Markdown or text document.")
        }
        guard answer.body.count <= ProjectDocuments.maximumBytes else {
            return .refused(413, "document_too_large",
                            "That document is too large to carry in one encrypted answer.",
                            ["byte_count": answer.body.count,
                             "limit_bytes": ProjectDocuments.maximumBytes])
        }
        guard String(data: answer.body, encoding: .utf8) != nil else {
            return .refused(415, "document_not_utf8",
                            "That document is not valid UTF-8 text.")
        }
        var body: [String: Any] = [
            "scope": scope, "path": path, "media_type": mediaType,
            "byte_count": answer.body.count, "data": answer.body.base64EncodedString(),
        ]
        if scope == "task" { body["task"] = task }
        return ReadOutcome(status: 200, code: nil, body: body, error: nil)
    }
}
