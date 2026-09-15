import Foundation
#if canImport(CryptoKit)
import CryptoKit
#elseif canImport(Crypto)
import Crypto
#else
#error("Cloud relay runtime requires CryptoKit or swift-crypto")
#endif
#if canImport(ClawdlineApplication)
import ClawdlineApplication // SwiftPM owns the durable Cloud state machines in the Application target.
#endif

// The transport/outbound owner is shared by the Mac and Linux compositions. SwiftPM compiles
// these declarations once in ClawdlineApplication; the flat compatibility suite compiles the
// same bytes together with the Mac-only bridge below.
#if !SWIFT_PACKAGE || CLAWDLINE_APPLICATION_TARGET

/// The app-facing surface of `CloudTransport`. Keeping the concrete actor behind this protocol
/// makes the bridge testable without a relay, Keychain, or terminal process.
public protocol CloudTransporting: Sendable {
    var commands: CloudInboundCommandStream { get }
    var readyGenerations: AsyncStream<UInt64> { get }
    var outboundReceipts: AsyncStream<CloudOutboundTransportReceipt> { get }
    func connect(role: CloudTransportRole) async throws
    func sendExactPublishFrame(_ bytes: Data) async throws
    func outboundReceiptIngestionSnapshot() async -> CloudOutboundReceiptIngestionSnapshot
    func setInboundRefusalHandler(_ handler: CloudTransport.InboundRefusalHandler?) async
    func setInboundDropHandler(_ handler: CloudTransport.InboundDropHandler?) async
    func setConnectionObserver(_ observer: CloudTransport.ConnectionObserver?) async
    func setTerminalAuthorizationHandler(
        _ handler: CloudTransport.TerminalAuthorizationHandler?
    ) async
    func shutdown() async
}

public extension CloudTransporting {
    var outboundReceipts: AsyncStream<CloudOutboundTransportReceipt> {
        AsyncStream { $0.finish() }
    }

    func outboundReceiptIngestionSnapshot() async -> CloudOutboundReceiptIngestionSnapshot {
        CloudOutboundReceiptIngestionSnapshot(enqueued: 0, dropped: 0, terminated: 0)
    }

    /// Fixtures that never drop an envelope have nothing to report.
    func setInboundDropHandler(_: CloudTransport.InboundDropHandler?) async {}

    func setConnectionObserver(_: CloudTransport.ConnectionObserver?) async {}
}

extension CloudTransport: CloudTransporting {}

public enum CloudOutboundTransportReceiptKind: Equatable, Sendable {
    case delivered
    case viewerOffline
    case peerRejected(CloudOutboundPeerRejection)
}

public enum CloudPublishErrorCode: String, CaseIterable, Equatable, Sendable {
    case badRequest = "bad_request"
    case tooLarge = "too_large"
    case forbidden
    case rateLimited = "rate_limited"
    case clockSkew = "clock_skew"
    case unavailable
    case `internal`
    case legacyRefused = "legacy_refused"
    case unknown
}

public enum CloudPublishErrorField: String, CaseIterable, Equatable, Sendable {
    case version = "v"
    case channel = "ch"
    case sequence = "seq"
    case timestamp = "ts"
    case envelopeClass = "class"
    case keyID = "key_id"
    case nonce
    case ciphertext = "ct"
    case sender
    case signature = "sig"
    case unknown
}

public enum CloudPublishErrorDisposition: String, Equatable, Sendable {
    case terminal
    case retryNewAttempt = "retry_new_attempt"
}

public struct CloudOutboundPeerRejection: Equatable, Sendable {
    public let code: CloudPublishErrorCode
    public let field: CloudPublishErrorField?
    public let disposition: CloudPublishErrorDisposition

    public init(code: CloudPublishErrorCode, field: CloudPublishErrorField?,
                disposition: CloudPublishErrorDisposition) {
        self.code = code
        self.field = field
        self.disposition = disposition
    }
}

public struct CloudOutboundTransportReceipt: Equatable, Sendable {
    public let channel: String
    public let sequence: Int64
    public let kind: CloudOutboundTransportReceiptKind

    public init(channel: String, sequence: Int64, kind: CloudOutboundTransportReceiptKind) {
        self.channel = channel
        self.sequence = sequence
        self.kind = kind
    }
}

public enum CloudDurableOutboundError: Error, Equatable, Sendable {
    case stopped
}

public enum CloudDurableOutboundFailureClass: String, Equatable, Sendable {
    case capacity
    case integrity
    case permissions
    case durableIO = "durable_io"
    case transport
    case other
}

public struct CloudOutboundReceiptIngestionSnapshot: Equatable, Sendable {
    public let enqueued: UInt64
    public let dropped: UInt64
    public let terminated: UInt64

    public init(enqueued: UInt64, dropped: UInt64, terminated: UInt64) {
        self.enqueued = enqueued
        self.dropped = dropped
        self.terminated = terminated
    }
}

public struct CloudCommandEffectAuthorization: Equatable, Sendable {
    public let epochState: CloudEpochGuardState
    public let rosterAllowsSender: Bool
    public let writeGateAllows: Bool
    /// False when the roster could not be read at all, which `rosterAllowsSender` alone cannot
    /// tell apart from a sender that is not in it.
    public let rosterReadable: Bool
    /// The clock guard's own reason while uncertain, in snake_case, and how long until its
    /// stability window would close if nothing else happens. Both are only reported, never used.
    public let epochReason: String?
    public let epochClearsInMilliseconds: UInt64?

    public var permitsEffect: Bool { rosterAllowsSender && writeGateAllows }

    public init(epochState: CloudEpochGuardState, rosterAllowsSender: Bool,
                writeGateAllows: Bool, rosterReadable: Bool = true,
                epochReason: String? = nil, epochClearsInMilliseconds: UInt64? = nil) {
        self.epochState = epochState
        self.rosterAllowsSender = rosterAllowsSender
        self.writeGateAllows = writeGateAllows
        self.rosterReadable = rosterReadable
        self.epochReason = epochReason
        self.epochClearsInMilliseconds = epochClearsInMilliseconds
    }
}

struct CloudPublishFrame: Codable, Equatable, Sendable {
    let type: String
    let envelope: CloudEnvelope

    init(envelope: CloudEnvelope) {
        type = "publish"
        self.envelope = envelope
    }
}

/// Sequence persistence belongs to configuration/pairing, not to this bridge. The injected
/// implementation must be durable: reusing a sender sequence after relaunch is a replay.
public protocol CloudEnvelopeSequencing: Sendable {
    func nextSequence(sender: String) async throws -> UInt64
}

public struct CloudAppIdentity: @unchecked Sendable {
    public let machineID: String
    public let deviceID: String
    public let keyID: String
    public let masterSecret: CloudMasterSecret
    public let signingKey: CloudDeviceKeyPair

    public init(machineID: String, deviceID: String, keyID: String,
                masterSecret: CloudMasterSecret, signingKey: CloudDeviceKeyPair) {
        self.machineID = machineID
        self.deviceID = deviceID
        self.keyID = keyID
        self.masterSecret = masterSecret
        self.signingKey = signingKey
    }
}

#endif

#if !SWIFT_PACKAGE || !CLAWDLINE_APPLICATION_TARGET

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
    /// A browser's diagnostic report, as `POST /v1/diagnostics/report` takes it (design §11.5).
    case diagnosticsReport(body: Data)
    /// A batch of a paired browser's own receive-failure rows, appended to a fixed JSONL file.
    case diagnosticsEvents(body: Data)
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
    /// The Project worktree lifecycle read model and its separately command-classified refresh.
    case projectWorktreeLifecycle(session: String, request: String, project: String)
    case projectWorktreeLifecycleRefresh(session: String, request: String, project: String)
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
        case .projectWorktreeLifecycle(let session, _, _),
             .projectWorktreeLifecycleRefresh(let session, _, _): return session
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
             .projectWorktreeLifecycle(_, let request, _), .projectWorktreeLifecycleRefresh(_, let request, _),
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
        if case .diagnosticsReport(let body) = command {
            return CloudDiagnosticsReportRoute.route(body: body, sender: sender)
        }
        if case .diagnosticsEvents(let body) = command {
            return CloudViewerEventsRoute.route(body: body, sender: sender)
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
    case sequenceExhausted
    case unsupportedOutboundChannel

    var errorDescription: String? {
        switch self {
        case .alreadyRunning: return "The cloud app bridge is already running."
        case .notRunning: return "The cloud app bridge has not been explicitly started."
        case .malformedSessions: return "The local session snapshot is malformed."
        case .malformedOrchestrator: return "The local orchestrator snapshot is malformed."
        case .sequenceExhausted: return "The durable Cloud sequence space is exhausted."
        case .unsupportedOutboundChannel: return "The Cloud outbound channel is not supported."
        }
    }
}

private struct CloudAppBridgeCompatibilityPublishFrame: Encodable {
    let type = "publish"
    let envelope: CloudEnvelope
}

/// Serializes the RemoteServer's full Cloud snapshots. Session observations are latest-value
/// state, except that an authoritative inventory is a deletion barrier which must be delivered
/// before a later incomplete observation. The pending suffix is therefore bounded to one such
/// barrier plus the newest incomplete reading instead of growing a Task chain.
final class CloudSnapshotPublicationQueue: @unchecked Sendable {
    enum Kind: String, Sendable { case sessionsAuthoritative = "sessions_authoritative"
        case sessionsIncremental = "sessions_incremental", orchestrator }
    enum Outcome: String, Sendable { case complete, failed, cancelled }
    struct Observation: Sendable {
        let id: String
        let kind: Kind
        let outcome: Outcome
        let queueMilliseconds: UInt64
        let workMilliseconds: UInt64
        let pendingAtEnqueue: Int
        let replacedAtEnqueue: Int
    }
    typealias Work = @Sendable (_ publicationID: String) async throws -> Void
    typealias Observer = @Sendable (Observation) -> Void
    typealias Clock = @Sendable () -> UInt64
    static let maximumPending = 3
    private struct Item {
        let id: String
        let authoritative: Bool?
        let kind: Kind
        let enqueuedAt: UInt64
        let pendingAtEnqueue: Int
        let replacedAtEnqueue: Int
        let work: Work
    }
    private let lock = NSLock()
    private let clock: Clock
    private let observer: Observer
    private var pending: [Item] = []
    private var worker: Task<Void, Never>?
    private var generation: UInt64 = 0

    init(clock: @escaping Clock = {
        DispatchTime.now().uptimeNanoseconds / 1_000_000
    }, observer: @escaping Observer = { _ in }) {
        self.clock = clock
        self.observer = observer
    }

    func enqueueSessions(_ payload: Data, work: @escaping Work) {
        let authoritative = Self.authoritative(payload)
        lock.lock()
        let before = pending.count
        if authoritative {
            pending.removeAll { $0.authoritative != nil }
        } else {
            pending.removeAll { $0.authoritative == false }
        }
        let replaced = before - pending.count
        let item = Item(
            id: Self.publicationID(), authoritative: authoritative,
            kind: authoritative ? .sessionsAuthoritative : .sessionsIncremental,
            enqueuedAt: clock(), pendingAtEnqueue: pending.count + 1,
            replacedAtEnqueue: replaced, work: work)
        pending.append(item)
        precondition(pending.count <= Self.maximumPending)
        startWorkerLocked()
        lock.unlock()
    }

    func enqueue(_ work: @escaping @Sendable () async throws -> Void) {
        lock.lock()
        let before = pending.count
        pending.removeAll { $0.authoritative == nil }
        let replaced = before - pending.count
        pending.append(Item(
            id: Self.publicationID(), authoritative: nil, kind: .orchestrator,
            enqueuedAt: clock(), pendingAtEnqueue: pending.count + 1,
            replacedAtEnqueue: replaced, work: { _ in try await work() }))
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
            let started = clock()
            let outcome: Outcome
            do {
                try await item.work(item.id)
                outcome = .complete
            } catch is CancellationError {
                outcome = .cancelled
            } catch {
                outcome = .failed
            }
            observer(Observation(
                id: item.id, kind: item.kind, outcome: outcome,
                queueMilliseconds: Self.elapsed(item.enqueuedAt, started),
                workMilliseconds: Self.elapsed(started, clock()),
                pendingAtEnqueue: item.pendingAtEnqueue,
                replacedAtEnqueue: item.replacedAtEnqueue))
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

    private static func publicationID() -> String {
        String(UUID().uuidString.lowercased().replacingOccurrences(of: "-", with: "").prefix(12))
    }

    private static func elapsed(_ start: UInt64, _ end: UInt64) -> UInt64 {
        end >= start ? end - start : 0
    }
}

struct CloudRefusalPublicationQueueMetrics: Equatable, Sendable {
    let maximumCount: Int
    let deadlineMilliseconds: UInt64
    let currentCount: Int
    let peakCount: Int
    let admittedTotal: UInt64
    let completedTotal: UInt64
    let timedOutTotal: UInt64
    let droppedFullTotal: UInt64
    let cancelledTotal: UInt64
}

/// A refusal has to answer on the encrypted request channel without turning overload into more
/// unbounded work. Offering is synchronous and constant-time; one worker publishes serially with
/// a deadline, so neither the transport receive loop nor the command consumer waits for I/O.
final class CloudRefusalPublicationQueue: @unchecked Sendable {
    typealias Work = @Sendable () async -> Void
    enum Outcome: String, Equatable, Sendable {
        case completed, timedOut = "timed_out", droppedFull = "dropped_full", cancelled
    }
    typealias Observer = @Sendable (Outcome, CloudRefusalPublicationQueueMetrics) -> Void

    static let defaultMaximumOutstanding = 8
    static let defaultDeadlineMilliseconds: UInt64 = 1_000

    private let lock = NSLock()
    private let maximumCount: Int
    private let deadlineMilliseconds: UInt64
    private let observer: Observer
    private var pending: [Work] = []
    private var worker: Task<Void, Never>?
    private var timedOutWork: [UUID: Task<Void, Never>] = [:]
    private var generation: UInt64 = 0
    private var active = false
    private var peakCount = 0
    private var admittedTotal: UInt64 = 0
    private var completedTotal: UInt64 = 0
    private var timedOutTotal: UInt64 = 0
    private var droppedFullTotal: UInt64 = 0
    private var cancelledTotal: UInt64 = 0

    init(
        maximumCount: Int = CloudRefusalPublicationQueue.defaultMaximumOutstanding,
        deadlineMilliseconds: UInt64 = CloudRefusalPublicationQueue.defaultDeadlineMilliseconds,
        observer: @escaping Observer = { _, _ in }
    ) {
        precondition(maximumCount > 0)
        precondition((1...60_000).contains(deadlineMilliseconds))
        self.maximumCount = maximumCount
        self.deadlineMilliseconds = deadlineMilliseconds
        self.observer = observer
    }

    /// `false` means the lane was full and `work` will never run; the caller owns saying so.
    @discardableResult
    func enqueue(_ work: @escaping Work) -> Bool {
        lock.lock()
        let current = currentCountLocked()
        guard current < maximumCount else {
            droppedFullTotal = Self.addingClamped(droppedFullTotal, 1)
            let metrics = snapshotLocked()
            lock.unlock()
            observer(.droppedFull, metrics)
            return false
        }
        pending.append(work)
        admittedTotal = Self.addingClamped(admittedTotal, 1)
        peakCount = max(peakCount, current + 1)
        startWorkerLocked()
        lock.unlock()
        return true
    }

    func snapshot() -> CloudRefusalPublicationQueueMetrics {
        lock.lock()
        let metrics = snapshotLocked()
        lock.unlock()
        return metrics
    }

    @discardableResult
    func cancelAndReset() -> Task<Void, Never>? {
        lock.lock()
        generation &+= 1
        let cancelled = pending.count + (active ? 1 : 0)
        pending.removeAll()
        active = false
        cancelledTotal = Self.addingClamped(cancelledTotal, UInt64(cancelled))
        let previous = worker
        worker = nil
        // Timed-out work may ignore cancellation. Keep it charged across a stop/start cycle until
        // its task really exits, otherwise repeated lifecycle resets could accumulate unbounded
        // zombie publications behind an apparently empty lane.
        let timedOut = Array(timedOutWork.values)
        let metrics = snapshotLocked()
        lock.unlock()
        previous?.cancel()
        timedOut.forEach { $0.cancel() }
        if cancelled > 0 { observer(.cancelled, metrics) }
        return previous
    }

    private func startWorkerLocked() {
        guard worker == nil else { return }
        let ownedGeneration = generation
        worker = Task { [weak self] in await self?.drain(generation: ownedGeneration) }
    }

    private func drain(generation ownedGeneration: UInt64) async {
        while !Task.isCancelled, let work = take(generation: ownedGeneration) {
            let result = await Self.run(work, deadlineMilliseconds: deadlineMilliseconds)
            guard let completion = complete(result, generation: ownedGeneration) else { return }
            observer(result.outcome, completion.metrics)
            if let id = completion.timedOutID, let task = result.lingeringWork {
                Task { [weak self] in
                    await task.value
                    self?.releaseTimedOutWork(id)
                }
            }
        }
    }

    private func take(generation ownedGeneration: UInt64) -> Work? {
        lock.lock()
        defer { lock.unlock() }
        guard generation == ownedGeneration, !Task.isCancelled else { return nil }
        guard !pending.isEmpty else { worker = nil; return nil }
        active = true
        return pending.removeFirst()
    }

    private func complete(
        _ result: RunResult, generation ownedGeneration: UInt64
    ) -> Completion? {
        lock.lock()
        defer { lock.unlock() }
        guard generation == ownedGeneration else { return nil }
        active = false
        var timedOutID: UUID?
        switch result.outcome {
        case .completed: completedTotal = Self.addingClamped(completedTotal, 1)
        case .timedOut:
            timedOutTotal = Self.addingClamped(timedOutTotal, 1)
            if let task = result.lingeringWork {
                let id = UUID()
                timedOutWork[id] = task
                timedOutID = id
            }
        case .cancelled: cancelledTotal = Self.addingClamped(cancelledTotal, 1)
        case .droppedFull: break
        }
        return Completion(metrics: snapshotLocked(), timedOutID: timedOutID)
    }

    private func releaseTimedOutWork(_ id: UUID) {
        lock.lock()
        timedOutWork[id] = nil
        startWorkerLocked()
        lock.unlock()
    }

    private func snapshotLocked() -> CloudRefusalPublicationQueueMetrics {
        CloudRefusalPublicationQueueMetrics(
            maximumCount: maximumCount,
            deadlineMilliseconds: deadlineMilliseconds,
            currentCount: currentCountLocked(),
            peakCount: peakCount,
            admittedTotal: admittedTotal,
            completedTotal: completedTotal,
            timedOutTotal: timedOutTotal,
            droppedFullTotal: droppedFullTotal,
            cancelledTotal: cancelledTotal
        )
    }

    private func currentCountLocked() -> Int {
        pending.count + (active ? 1 : 0) + timedOutWork.count
    }

    private struct Completion {
        let metrics: CloudRefusalPublicationQueueMetrics
        let timedOutID: UUID?
    }

    private struct RunResult {
        let outcome: Outcome
        let lingeringWork: Task<Void, Never>?
    }

    private final class DeadlineRace: @unchecked Sendable {
        private let lock = NSLock()
        private var outcome: Outcome?
        private var continuation: CheckedContinuation<Outcome, Never>?

        func wait() async -> Outcome {
            await withCheckedContinuation { continuation in
                lock.lock()
                if let outcome {
                    lock.unlock()
                    continuation.resume(returning: outcome)
                } else {
                    self.continuation = continuation
                    lock.unlock()
                }
            }
        }

        func resolve(_ outcome: Outcome) {
            lock.lock()
            guard self.outcome == nil else { lock.unlock(); return }
            self.outcome = outcome
            let continuation = self.continuation
            self.continuation = nil
            lock.unlock()
            continuation?.resume(returning: outcome)
        }
    }

    private static func run(
        _ work: @escaping Work, deadlineMilliseconds: UInt64
    ) async -> RunResult {
        let race = DeadlineRace()
        let workTask = Task {
            await work()
            race.resolve(.completed)
        }
        let deadlineTask = Task {
            do {
                try await Task.sleep(nanoseconds: deadlineMilliseconds * 1_000_000)
                race.resolve(.timedOut)
            } catch {}
        }
        let outcome = await withTaskCancellationHandler {
            await race.wait()
        } onCancel: {
            race.resolve(.cancelled)
        }
        deadlineTask.cancel()
        if outcome != .completed { workTask.cancel() }
        return RunResult(
            outcome: outcome,
            lingeringWork: outcome == .timedOut ? workTask : nil
        )
    }

    private static func addingClamped(_ value: UInt64, _ amount: UInt64) -> UInt64 {
        let (sum, overflow) = value.addingReportingOverflow(amount)
        return overflow ? .max : sum
    }
}

#endif

#if !SWIFT_PACKAGE || CLAWDLINE_APPLICATION_TARGET

/// Production's sole outbound sequence, pending, reconnect and publication owner. The logical
/// bytes and global sequence are committed first, the exact publish frame is sealed second, and
/// `CloudOutboundSpool` commits `sent` before this adapter asks the transport to write a byte.
/// The W0-E contract remains candidate-only: this composes the already-deployed legacy-v1
/// envelope producer and does not opt into candidate authority or raise a client floor.
public actor CloudDurableOutboundComposition {
    public typealias EnqueueStageObserver = @Sendable (
        _ stage: String, _ durationMilliseconds: UInt64, _ outcome: String,
        _ logicalRecordBytes: Int, _ sealedFrameBytes: Int
    ) -> Void

    private let spool: CloudOutboundSpool
    private let transport: any CloudTransporting
    private var identity: CloudAppIdentity
    private var identitiesByKeyID: [String: CloudAppIdentity]
    private let nowMilliseconds: @Sendable () -> UInt64
    private let diagnostic: @Sendable (String) -> Void
    private let deadlineFailureRetryDelay: Duration
    private var attemptDeadlineTask: Task<Void, Never>?
    private var attemptDeadlineGeneration: UInt64 = 0
    private var drainWorker: Task<Void, Never>?
    private var drainRequested = false
    private var reconnectRequested = false
    private var drainGeneration: UInt64 = 0
    private var stopped = false
    private var stoppingDrainWorker: Task<Void, Never>?
    private var stoppingDeadlineTask: Task<Void, Never>?
    private var structuralFailure: CloudDurableOutboundFailureClass?
    private var transientFailureCount = 0

    public init(spool: CloudOutboundSpool, transport: any CloudTransporting,
         identity: CloudAppIdentity, nowMilliseconds: @escaping @Sendable () -> UInt64,
         diagnostic: @escaping @Sendable (String) -> Void = { _ in },
         deadlineFailureRetryDelay: Duration = .seconds(1)) {
        self.spool = spool
        self.transport = transport
        self.identity = identity
        identitiesByKeyID = [identity.keyID: identity]
        self.nowMilliseconds = nowMilliseconds
        self.diagnostic = diagnostic
        self.deadlineFailureRetryDelay = deadlineFailureRetryDelay
    }

    /// A ready transport generation has already authenticated these freshly read protected
    /// epochs. New frames use the current key; uncertain sent rows retain their exact bytes, and
    /// retry-new-attempt can still open an older in-process frame with its original key.
    public func replaceIdentity(_ identity: CloudAppIdentity) {
        guard !stopped else { return }
        self.identity = identity
        identitiesByKeyID[identity.keyID] = identity
    }

    /// Producer completion is the durable seal, not a socket acknowledgement. Once the exact
    /// frame is committed ready, this method only wakes the independently owned drain worker and
    /// returns; a slow socket can no longer suspend snapshot/read producers behind itself.
    /// The answer is the spool sequence the frame was sealed under, which is also the sequence a
    /// relay receipt for it will carry.
    @discardableResult
    public func enqueue(
        _ plaintext: Data, channel: String, logicalID: String,
        observe: EnqueueStageObserver? = nil
    ) async throws -> Int64 {
        guard !stopped else { throw CloudDurableOutboundError.stopped }
        let spoolChannel = try Self.spoolChannel(channel)
        let identity = identity
        let payloadDigest = SHA256.hash(data: plaintext).map { String(format: "%02x", $0) }.joined()
        // Durable logical metadata carries the quota-relevant size and a one-way content identity,
        // never plaintext or a base64-equivalent copy. Exact content exists only inside the
        // authenticated encrypted frame.
        let logicalRecord: CloudJSONValue = .object([
            "v": .int(2), "ch": .string(channel), "class": .string("stream"),
            "key_id": .string(identity.keyID), "logical_id": .string(logicalID),
            "payload_bytes": .int(Int64(plaintext.count)),
            "payload_sha256": .string(payloadDigest), "sender": .string(identity.deviceID),
        ])
        let logicalRecordBytes = CloudOutboundSpool.chargedBytes(of: logicalRecord)
        var stageStarted = nowMilliseconds()
        let sequence: Int64
        do {
            if spoolChannel.isLatestValue {
                sequence = try await spool.reserveLatestValue(
                    channel: spoolChannel, logicalID: logicalID, ownerID: nil,
                    recipient: channel, record: logicalRecord)
            } else {
                sequence = try await spool.reserve(
                    channel: spoolChannel, logicalID: logicalID, ownerID: nil,
                    recipient: channel, record: logicalRecord)
            }
            observe?("durable_reserve", Self.elapsed(stageStarted, nowMilliseconds()), "complete",
                     logicalRecordBytes, 0)
        } catch {
            observe?("durable_reserve", Self.elapsed(stageStarted, nowMilliseconds()), "failed",
                     logicalRecordBytes, 0)
            recordImmediateFailure(error, stage: "durable_reserve")
            throw error
        }
        guard sequence >= 0 else { throw CloudRelayRuntimeError.sequenceExhausted }
        stageStarted = nowMilliseconds()
        let envelope: CloudEnvelope
        do {
            envelope = try CloudEnvelope.seal(
                plaintext, ch: channel, seq: UInt64(sequence), ts: nowMilliseconds(),
                envelopeClass: .stream, keyID: identity.keyID, sender: identity.deviceID,
                masterSecret: identity.masterSecret, signingKey: identity.signingKey)
            observe?("envelope_seal", Self.elapsed(stageStarted, nowMilliseconds()), "complete",
                     logicalRecordBytes, 0)
        } catch {
            observe?("envelope_seal", Self.elapsed(stageStarted, nowMilliseconds()), "failed",
                     logicalRecordBytes, 0)
            throw error
        }
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        let exactFrame = try encoder.encode(CloudPublishFrame(envelope: envelope))
        stageStarted = nowMilliseconds()
        do {
            try await spool.seal(seq: sequence, sealedEnvelope: exactFrame)
            observe?("spool_seal", Self.elapsed(stageStarted, nowMilliseconds()), "complete",
                     logicalRecordBytes, exactFrame.count)
        } catch {
            observe?("spool_seal", Self.elapsed(stageStarted, nowMilliseconds()), "failed",
                     logicalRecordBytes, exactFrame.count)
            recordImmediateFailure(error, stage: "spool_seal")
            // The reservation is durable even when sealing fails. Its stale deadline now has an
            // explicit wake owner, so it cannot block higher ready work forever without traffic.
            requestDrain(reconnect: false)
            throw error
        }
        requestDrain(reconnect: false)
        return sequence
    }

    public func requestDrain(reconnect: Bool) {
        guard !stopped else { return }
        drainRequested = true
        reconnectRequested = reconnectRequested || reconnect
        guard drainWorker == nil else { return }
        drainGeneration &+= 1
        let generation = drainGeneration
        drainWorker = Task { [weak self] in
            await self?.runDrainWorker(generation: generation)
        }
    }

    public func settle(_ receipt: CloudOutboundTransportReceipt) async throws {
        let channel = try Self.spoolChannel(receipt.channel)
        let kind: CloudSpoolSettleKind
        switch receipt.kind {
        case .delivered: kind = .delivered
        case .viewerOffline: kind = .viewerOffline
        case .peerRejected: kind = .peerError
        }
        let retrySource = await spool.row(seq: receipt.sequence)
        let disposition = try await spool.settle(
            seq: receipt.sequence, channel: channel, fullChannel: receipt.channel, kind)
        requestDrain(reconnect: false)
        if case .peerRejected(let rejection) = receipt.kind {
            let field = rejection.field?.rawValue ?? "none"
            diagnostic("cloud: outbound peer_rejection code=\(rejection.code.rawValue) "
                + "field=\(field) "
                + "disposition=\(rejection.disposition.rawValue)")
            if rejection.disposition == .retryNewAttempt,
               disposition == .settled(.rejected), let retrySource {
                try await retryRejectedRow(retrySource)
            }
        }
    }

    public func metricsSnapshot() async -> CloudOutboundWindowSnapshot {
        await spool.outboundWindowSnapshot()
    }

    public func persistentFailureState() -> CloudDurableOutboundFailureClass? { structuralFailure }

    /// Phase one is deliberately non-joining: CloudAppBridge must close the transport socket
    /// before it waits for a production URLSession send that does not observe Task cancellation.
    public func beginStop() {
        guard !stopped else { return }
        stopped = true
        drainGeneration &+= 1
        drainRequested = false
        reconnectRequested = false
        let worker = drainWorker
        drainWorker = nil
        worker?.cancel()
        attemptDeadlineGeneration &+= 1
        let deadline = attemptDeadlineTask
        attemptDeadlineTask = nil
        deadline?.cancel()
        stoppingDrainWorker = worker
        stoppingDeadlineTask = deadline
    }

    public func finishStop() async {
        await stoppingDrainWorker?.value
        await stoppingDeadlineTask?.value
        stoppingDrainWorker = nil
        stoppingDeadlineTask = nil
    }

    public func stop(unblocking: @Sendable () async -> Void) async {
        beginStop()
        await unblocking()
        await finishStop()
    }

    /// Test/helper convenience for cancellation-aware transports. Production uses the explicit
    /// begin/transport-shutdown/finish sequence above.
    public func stop() async {
        beginStop()
        await finishStop()
    }

    private func runDrainWorker(generation ownedGeneration: UInt64) async {
        while !Task.isCancelled, ownedGeneration == drainGeneration, !stopped {
            guard drainRequested else { break }
            let reconnect = reconnectRequested
            drainRequested = false
            reconnectRequested = false
            do {
                if reconnect {
                    let sent = await spool.rowsSnapshot()
                        .filter { $0.state == .sent }
                        .map(\.seq).sorted()
                    for sequence in sent {
                        try Task.checkCancellation()
                        _ = try await measuredSend(preparationStage: "reconnect_replay_lookup") {
                            try await self.spool.resendPersisted(seq: sequence, via: $0)
                        }
                    }
                }
                while !Task.isCancelled {
                    let disposition = try await measuredSend(preparationStage: "spool_mark_sent") {
                        try await self.spool.sendNext(via: $0)
                    }
                    guard case .sent = disposition else { break }
                }
                structuralFailure = nil
                transientFailureCount = 0
                await scheduleAttemptDeadline()
            } catch is CancellationError {
                break
            } catch {
                // `sendNext` commits sent before the socket call. A failed transport therefore
                // consumes its durable window slot. A store failure can instead leave the row
                // ready. The same single wake owner retries either shape without a busy loop.
                let failure = Self.failureDisposition(error)
                let retry = failure.retryable ? "scheduled" : "permanent"
                diagnostic("cloud: durable outbound drain failed failure=\(failure.kind.rawValue) "
                    + "retry=\(retry)")
                if failure.retryable {
                    transientFailureCount = min(transientFailureCount + 1, 8)
                    await scheduleAttemptDeadline(
                        notBefore: Self.scaledRetryDelay(
                            deadlineFailureRetryDelay, failures: transientFailureCount),
                        forceWake: true)
                } else {
                    structuralFailure = failure.kind
                }
            }
        }
        if ownedGeneration == drainGeneration { drainWorker = nil }
    }

    private func measuredSend(
        preparationStage: String,
        _ operation: (@escaping @Sendable (Data) async throws -> Void) async throws
            -> CloudSpoolSendDisposition
    ) async throws -> CloudSpoolSendDisposition {
        let before = await spool.outboundWindowSnapshot()
        let persistenceStarted = nowMilliseconds()
        let disposition = try await operation { [self, transport, diagnostic, nowMilliseconds] bytes in
            let start = nowMilliseconds()
            diagnostic("cloud: durable outbound stage=\(preparationStage) "
                + "duration_ms=\(Self.elapsed(persistenceStarted, start)) outcome=complete "
                + "frame_bytes=\(bytes.count) ready_rows=\(before.readyWaitingRows) "
                + "window_rows=\(before.currentRows) window_bytes=\(before.currentBytes) "
                + "stored_rows=\(before.storedRows) "
                + "stored_charged_bytes=\(before.storedChargedBytes) "
                + "terminal_rows=\(before.terminalRows)")
            // The deadline owner must wake before a socket is allowed to stall indefinitely.
            // `sendNext` has already committed this exact frame as sent when it invokes us.
            await self.scheduleAttemptDeadline()
            do {
                try await transport.sendExactPublishFrame(bytes)
                diagnostic("cloud: durable outbound stage=socket_write "
                    + "duration_ms=\(Self.elapsed(start, nowMilliseconds())) outcome=complete "
                    + "frame_bytes=\(bytes.count)")
            } catch {
                diagnostic("cloud: durable outbound stage=socket_write "
                    + "duration_ms=\(Self.elapsed(start, nowMilliseconds())) outcome=failed "
                    + "frame_bytes=\(bytes.count)")
                throw error
            }
        }
        let after = await spool.outboundWindowSnapshot()
        if !Self.isWriteDisposition(disposition) {
            diagnostic("cloud: durable outbound stage=drain_observation ready_age_ms="
                + "\(after.oldestReadyAgeMilliseconds) outcome=\(Self.dispositionName(disposition)) "
                + "receipt_wait_ms=\(after.oldestReceiptWaitMilliseconds) "
                + "ready_rows=\(after.readyWaitingRows) ready_bytes=\(after.readyWaitingBytes) "
                + "window_rows=\(after.currentRows) window_bytes=\(after.currentBytes) "
                + "stored_rows=\(after.storedRows) "
                + "stored_charged_bytes=\(after.storedChargedBytes) "
                + "terminal_rows=\(after.terminalRows) "
                + "process_window_peak_rows=\(after.processPeakRows) "
                + "process_window_peak_bytes=\(after.processPeakBytes) "
                + "window_refusal_attempts=\(after.windowAdmissionRefusalAttempts) "
                + "writes_started=\(after.socketWritesStarted) "
                + "writes_completed=\(after.socketWritesCompleted) "
                + "writes_failed=\(after.socketWritesFailed) "
                + "budget=\(after.budgetStatus)")
        }
        return disposition
    }

    private func scheduleAttemptDeadline(
        notBefore minimumDelay: Duration = .zero, forceWake: Bool = false
    ) async {
        guard !stopped else { return }
        attemptDeadlineGeneration &+= 1
        let generation = attemptDeadlineGeneration
        attemptDeadlineTask?.cancel()
        attemptDeadlineTask = nil
        let deadlineDelay = await spool.nextMaintenanceWakeDelay()
        guard !stopped else { return }
        guard deadlineDelay != nil || forceWake else { return }
        let nextDelay = deadlineDelay ?? .zero
        let delay = max(nextDelay, minimumDelay)
        let nanoseconds = Self.nanoseconds(delay)
        attemptDeadlineTask = Task { [weak self] in
            do { try await Task.sleep(nanoseconds: nanoseconds) }
            catch { return }
            await self?.attemptDeadlineFired(generation: generation)
        }
    }

    private func attemptDeadlineFired(generation: UInt64) async {
        guard !stopped, generation == attemptDeadlineGeneration, !Task.isCancelled else { return }
        attemptDeadlineTask = nil
        do {
            _ = try await spool.expireAttemptDeadlines()
            requestDrain(reconnect: false)
            await scheduleAttemptDeadline()
        } catch {
            let failure = Self.failureDisposition(error)
            let retry = failure.retryable ? "scheduled" : "permanent"
            diagnostic("cloud: durable outbound deadline failed failure=\(failure.kind.rawValue) "
                + "retry=\(retry)")
            if failure.retryable {
                transientFailureCount = min(transientFailureCount + 1, 8)
                await scheduleAttemptDeadline(
                    notBefore: Self.scaledRetryDelay(
                        deadlineFailureRetryDelay, failures: transientFailureCount),
                    forceWake: true)
            } else {
                structuralFailure = failure.kind
            }
        }
    }

    private func retryRejectedRow(_ row: CloudSpoolRow) async throws {
        guard let bytes = row.sealedEnvelopeBytes else { return }
        let frame = try JSONDecoder().decode(CloudPublishFrame.self, from: bytes)
        guard frame.envelope.ch == row.recipient, Int64(frame.envelope.seq) == row.seq else {
            throw CloudOutboundSpoolError.settlementCorrelationMismatch(seq: row.seq)
        }
        guard let sealingIdentity = identitiesByKeyID[frame.envelope.keyID] else { return }
        let deviceID = sealingIdentity.deviceID
        let publicKey = sealingIdentity.signingKey.publicKeyRaw
        let plaintext = try frame.envelope.open(
            masterSecret: sealingIdentity.masterSecret,
            publicKeyForSender: { sender in
                sender == deviceID ? publicKey : nil
            })
        try await enqueue(plaintext, channel: row.recipient, logicalID: row.logicalID)
    }

    private func recordImmediateFailure(_ error: Error, stage: String) {
        let failure = Self.failureDisposition(error)
        if !failure.retryable { structuralFailure = failure.kind }
        let retry = failure.retryable ? "caller" : "permanent"
        diagnostic("cloud: durable outbound stage=\(stage) failure=\(failure.kind.rawValue) "
            + "retry=\(retry)")
    }

    private static func nanoseconds(_ duration: Duration) -> UInt64 {
        let parts = duration.components
        guard parts.seconds >= 0 else { return 0 }
        let seconds = UInt64(parts.seconds)
        let nanos = UInt64(max(0, parts.attoseconds / 1_000_000_000))
        let (whole, overflow) = seconds.multipliedReportingOverflow(by: 1_000_000_000)
        if overflow { return .max }
        return whole.addingReportingOverflow(nanos).overflow ? .max : whole + nanos
    }

    private static func elapsed(_ start: UInt64, _ end: UInt64) -> UInt64 {
        end >= start ? end - start : 0
    }

    private static func dispositionName(_ disposition: CloudSpoolSendDisposition) -> String {
        switch disposition {
        case .idle: return "idle"
        case .sent: return "sent"
        case .resent: return "resent"
        case .blockedAwaitingAcknowledgement: return "receipt_wait"
        case .blockedAwaitingSeal: return "seal_wait"
        case .blockedWindowFull: return "window_full"
        }
    }

    private static func isWriteDisposition(_ disposition: CloudSpoolSendDisposition) -> Bool {
        switch disposition {
        case .sent, .resent: return true
        default: return false
        }
    }

    private static func failureDisposition(
        _ error: Error
    ) -> (kind: CloudDurableOutboundFailureClass, retryable: Bool) {
        if error is CloudTransportError { return (.transport, true) }
        if error is CloudOutboundSpoolError { return (.integrity, false) }
        guard let durable = error as? CloudDurableStoreFailure else { return (.other, false) }
        switch durable {
        case .oversized: return (.capacity, false)
        case .unsafePath, .unsafePermissions, .invalidMachineIdentity, .writerLockHeld,
             .symlink, .nonRegular, .multipleLinks, .wrongOwner:
            return (.permissions, false)
        case .unreadable, .corrupt, .unknownVersion, .sequenceExhausted:
            return (.integrity, false)
        case .persist, .fileFsync, .rename, .directoryFsync, .recovery:
            return (.durableIO, true)
        }
    }

    private static func scaledRetryDelay(_ base: Duration, failures: Int) -> Duration {
        let exponent = max(0, min(failures - 1, 6))
        let baseNanoseconds = nanoseconds(base)
        let multiplier = UInt64(1) << UInt64(exponent)
        let (scaled, overflow) = baseNanoseconds.multipliedReportingOverflow(by: multiplier)
        return .nanoseconds(Int64(min(overflow ? UInt64.max : scaled, 60_000_000_000)))
    }

    private static func spoolChannel(_ channel: String) throws -> CloudSpoolChannel {
        guard let prefix = channel.split(separator: "/", maxSplits: 1).first,
              let parsed = CloudSpoolChannel(rawValue: String(prefix)) else {
            throw CloudRelayRuntimeError.unsupportedOutboundChannel
        }
        return parsed
    }
}

public enum CloudRelayRuntimeError: Error, LocalizedError, Equatable, Sendable {
    case sequenceExhausted
    case unsupportedOutboundChannel

    public var errorDescription: String? {
        switch self {
        case .sequenceExhausted: return "The durable Cloud sequence space is exhausted."
        case .unsupportedOutboundChannel: return "The Cloud outbound channel is not supported."
        }
    }
}

#endif

#if !SWIFT_PACKAGE || !CLAWDLINE_APPLICATION_TARGET

/// One published Session whose transcript signature a running Cloud bridge wants to know.
///
/// `binding` is the row's own spelling of what selects the transcript file — assistant, tty,
/// conversation id and working directory. A source may use it to skip re-resolving a file whose
/// selecting facts have not moved; it is never shown to anybody.
struct CloudTranscriptSubject: Equatable, Sendable {
    let sessionID: String
    let binding: String
}

/// `signature` is the value the Mac's `transcript` read answer carries for that Session's file at
/// the moment of the report, or nil once it is no longer known.
typealias CloudTranscriptSignatureReport = @Sendable (_ sessionID: String, _ signature: String?)
    -> Void

/// Where a running Cloud bridge learns each published Session's transcript signature.
///
/// The bridge is the only demand. It names its published Sessions from inside a running
/// publication and calls `stop` when its lifecycle ends, so a source never watches a file while
/// Cloud is disabled or the bridge is stopped. Reports may arrive on any thread, in order, and may
/// repeat a value the bridge already holds.
protocol CloudTranscriptSignatureSource: AnyObject, Sendable {
    func track(_ subjects: [CloudTranscriptSubject], report: @escaping CloudTranscriptSignatureReport)
    func stop()
}

/// Connects the app's existing full-snapshot and HTTP-command seams to CloudTransport.
///
/// Construction has no side effects. `start()` is the explicit attachment/configuration point,
/// and inbound commands have a second, separately injected gate whose default is always false.
actor CloudAppBridge {
    static let machineReplySession = "__clawdline_machine__"
    static let sessionInventoryID = "__clawdline_inventory_v1__"
    static let sessionInventoryLimit = 512
    /// The Cloud-only row field naming the current transcript signature (docs/cloud.md).
    static let transcriptSignatureField = "transcript_signature"
    /// A transcript-driven republication pass runs at most once per this many milliseconds, so a
    /// Session at most once per second however fast its transcript is written. Scan publications
    /// are not held to it: they already follow the SessionWatch cadence.
    static let transcriptRepublicationIntervalMilliseconds: UInt64 = 1_000
    /// The hosted console calls a machine current for five minutes after its newest `s/` or
    /// `orch/` envelope (`MACHINE_INVENTORY_FRESH_MS`). With unchanged rows skipped, an idle Mac
    /// may publish neither, so an authoritative scan re-sends the small inventory marker once no
    /// Session-channel frame has gone out for three minutes. The scan cadence (20 s) and the
    /// measured 12–20 s outbound latency still land it inside the five.
    static let sessionPresenceIntervalMilliseconds: UInt64 = 180_000
    /// Row fields that move with every SessionWatch reading whether or not anything a viewer reads
    /// did, so they never make a row different. A row that differs elsewhere is still published
    /// whole, these values included. Each one was checked against every reader in
    /// `Resources/web/app/js`: none reads them (the viewer's closeability gate reads `state`,
    /// `reasons`, `mover`, `attestation_id`, `version` and `source.freshness`, which stay).
    static let freshnessOnlySessionRowFields: [[String]] = [
        ["closeability", "observed_at"],
        ["closeability", "session_generation"],
        ["closeability", "source", "observed_at"],
    ]
    typealias CommandGate = @Sendable () -> Bool
    typealias CommandEffectAuthority = @Sendable (_ sender: String, _ requiresWriteGate: Bool) async
        -> CloudCommandEffectAuthorization
    typealias Milliseconds = @Sendable () -> UInt64
    typealias CommandResultObserver = @Sendable (CloudCommandResult) -> Void
    typealias DiagnosticLogger = @Sendable (String) -> Void
    typealias TransportReadyObserver = @Sendable (UInt64) -> Void

    private enum ReadLane: String { case foreground, background }
    private struct PendingRead: Sendable {
        let read: CloudHeadlessRead
        let reference: CommandReference
        let lifecycleGeneration: UInt64
        let admittedAt: UInt64
    }
    private struct ReadRefusal {
        let layer: CloudRefusalLayer
        let status: Int
        let code: String
        let message: String
        var detail: [String: Any] = [:]
    }
    /// Who asked, as far as this Mac can tell: the envelope's `(sender, seq)` always, and the
    /// request, type and session once a plaintext names them safely. Every refusal carries it.
    struct CommandReference: Sendable {
        let sender: String
        let sequence: UInt64
        var request: String?
        var type: String?
        var session: String?

        init(sender: String, sequence: UInt64, request: String? = nil, type: String? = nil,
             session: String? = nil) {
            self.sender = sender
            self.sequence = sequence
            self.request = request
            self.type = type
            self.session = session
        }

        init(_ inbound: CloudInboundCommand, body: [String: Any]?) {
            sender = inbound.sender
            sequence = inbound.sequence
            request = CloudStatus.identifier(
                CloudAppBridge.requestName(body?["request"])
                    ?? CloudAppBridge.requestName(body?["request_id"]))
            type = CloudStatus.identifier(body?["type"] as? String)
            session = CloudStatus.identifier(body?["session"] as? String)
        }
    }
    private struct RoutedCommand {
        let result: CloudCommandResult
        let layer: CloudRefusalLayer
        let executed: Bool
        var detail: [String: Any] = [:]
    }

    private let transport: any CloudTransporting
    private let identity: CloudAppIdentity
    private let sequencing: any CloudEnvelopeSequencing
    private let allowCloudCommands: CommandGate
    private let currentCommandEffectAuthority: CommandEffectAuthority
    private let commandRouter: any CloudCommandRouting
    private let nowMilliseconds: Milliseconds
    private let commandResult: CommandResultObserver
    private let diagnostic: DiagnosticLogger
    private let refusalPublications: CloudRefusalPublicationQueue
    private let commandLedger: CloudCommandLedger?
    private let durableOutbound: CloudDurableOutboundComposition?
    private let status: CloudStatus
    /// Notices are published only for a status owner the composition supplied. Fixtures that pass
    /// none still record into a private owner, and their `orch/` bytes stay exactly as given.
    private let noticesEnabled: Bool
    private let noticeIntervalMilliseconds: UInt64
    private var noticeTask: Task<Void, Never>?
    private var lastNoticeAt: UInt64?
    /// The newest orchestrator snapshot, so a notice can be republished inside it (§11.2).
    private var lastOrchestratorPayload: Data?

    private var commandTask: Task<Void, Never>?
    private var readyTask: Task<Void, Never>?
    private var outboundReceiptTask: Task<Void, Never>?
    private var connectTask: Task<Void, Error>?
    private var publicationTasks: [UUID: Task<Void, Error>] = [:]
    private var foregroundReadTask: Task<Void, Never>?
    private var backgroundReadTask: Task<Void, Never>?
    private var lifecycleRefreshTasks: [UUID: Task<Void, Never>] = [:]
    private var foregroundReadActive = false
    private var backgroundReadActive = false
    private var foregroundReads: [PendingRead] = []
    private var backgroundReads: [PendingRead] = []
    private var transportReady: TransportReadyObserver = { _ in }
    private var lifecycleGeneration: UInt64 = 0
    private var starting = false
    private var running = false
    private var publishedSessionIDs = Set<String>()
    /// What each published row was compared as (`cloudSessionRowIdentity`), not its full bytes.
    private var publishedSessionRows: [String: Data] = [:]
    private var publishedSessionInventory: Data?
    /// The newest complete row the Mac handed over for each published Session, whether or not it
    /// was published, and that scan's `at`/`scan`. A transcript republication sends these.
    private var latestSessionRows: [String: [String: Any]] = [:]
    private var latestSessionEnvelope: (at: Any, scan: [String: Any])?
    private var lastSessionFrameAt: UInt64?
    /// Session-channel publications run one at a time, whoever started them, so a transcript
    /// republication can never interleave with a scan's rows or land after its tombstone.
    private var sessionPublicationTail: Task<Void, Never>?
    private let transcriptSignatures: (any CloudTranscriptSignatureSource)?
    private let transcriptRepublicationIntervalMilliseconds: UInt64
    private let waitMilliseconds: @Sendable (UInt64) async throws -> Void
    private var knownTranscriptSignatures: [String: String] = [:]
    private var pendingTranscriptSessions = Set<String>()
    private var transcriptRepublicationTask: Task<Void, Never>?
    private var lastTranscriptRepublicationAt: UInt64?
    private var transcriptReports: AsyncStream<(String, String?)>.Continuation?
    private var transcriptReportTask: Task<Void, Never>?

    init(
        transport: any CloudTransporting,
        identity: CloudAppIdentity,
        sequencing: any CloudEnvelopeSequencing,
        allowCloudCommands: @escaping CommandGate = { false },
        currentCommandEffectAuthority: CommandEffectAuthority? = nil,
        commandRouter: any CloudCommandRouting = RemoteServerCloudCommandRouter(),
        nowMilliseconds: @escaping Milliseconds = {
            UInt64(Date().timeIntervalSince1970 * 1_000)
        },
        commandResult: @escaping CommandResultObserver = { _ in },
        diagnostic: @escaping DiagnosticLogger = { _ in },
        refusalPublications: CloudRefusalPublicationQueue? = nil,
        durableRuntime: CloudDurableRuntime? = nil,
        status: CloudStatus? = nil,
        noticeIntervalMilliseconds: UInt64 = 5_000,
        transcriptSignatures: (any CloudTranscriptSignatureSource)? = nil,
        transcriptRepublicationIntervalMilliseconds: UInt64 =
            CloudAppBridge.transcriptRepublicationIntervalMilliseconds,
        waitMilliseconds: @escaping @Sendable (UInt64) async throws -> Void = {
            try await Task.sleep(nanoseconds: $0 * 1_000_000)
        }
    ) {
        self.transcriptSignatures = transcriptSignatures
        self.transcriptRepublicationIntervalMilliseconds = transcriptRepublicationIntervalMilliseconds
        self.waitMilliseconds = waitMilliseconds
        self.status = status ?? CloudStatus()
        noticesEnabled = status != nil
        self.noticeIntervalMilliseconds = noticeIntervalMilliseconds
        self.transport = transport
        self.identity = identity
        self.sequencing = sequencing
        self.allowCloudCommands = allowCloudCommands
        self.currentCommandEffectAuthority = currentCommandEffectAuthority ?? { _, requiresWriteGate in
            CloudCommandEffectAuthorization(
                epochState: .ready, rosterAllowsSender: true,
                writeGateAllows: !requiresWriteGate || allowCloudCommands())
        }
        self.commandRouter = commandRouter
        self.nowMilliseconds = nowMilliseconds
        self.commandResult = commandResult
        self.diagnostic = diagnostic
        commandLedger = durableRuntime?.ledger
        durableOutbound = durableRuntime.map {
            CloudDurableOutboundComposition(
                spool: $0.spool, transport: transport, identity: identity,
                nowMilliseconds: nowMilliseconds, diagnostic: diagnostic)
        }
        self.refusalPublications = refusalPublications ?? CloudRefusalPublicationQueue(
            observer: { outcome, metrics in
                diagnostic("cloud: refusal reply lane outcome=\(outcome.rawValue) "
                    + "current=\(metrics.currentCount) peak=\(metrics.peakCount) "
                    + "admitted=\(metrics.admittedTotal) completed=\(metrics.completedTotal) "
                    + "timed_out=\(metrics.timedOutTotal) dropped_full=\(metrics.droppedFullTotal) "
                    + "cancelled=\(metrics.cancelledTotal) "
                    + "deadline_ms=\(metrics.deadlineMilliseconds)")
            }
        )
    }

    func start() async throws {
        guard !running, !starting else { throw CloudAppBridgeError.alreadyRunning }
        lifecycleGeneration &+= 1
        let ownedGeneration = lifecycleGeneration
        starting = true
        let transport = self.transport
        let refusalPublications = self.refusalPublications
        let status = self.status
        let diagnostic = self.diagnostic
        await transport.setInboundRefusalHandler {
            [weak self, refusalPublications, status, diagnostic] refusal in
            let admitted = refusalPublications.enqueue { [weak self] in
                await self?.consumeInboundRefusal(
                    refusal, lifecycleGeneration: ownedGeneration
                )
            }
            guard !admitted else { return }
            // The lane is full, so no answer will be published. Say so in the log and the notice
            // from here: nothing else will ever see this refusal.
            let command = refusal.command
            status.recordLaneDropped()
            status.recordRefusal(
                sender: command.sender, sequence: command.sequence, request: nil, type: nil,
                session: nil, layer: .macTransport, code: refusal.reason.refusalCode, reply: .notice)
            diagnostic("cloud: " + Self.laneDropLine(refusal))
        }
        await transport.setInboundDropHandler { [status] drop in status.recordDrop(drop) }
        await transport.setConnectionObserver { [status] event in status.record(event) }
        status.setKeyID(identity.keyID)
        if noticesEnabled {
            status.enableFileWriting()
            status.setNoticeObserver { [weak self] in
                Task { await self?.noticeRequested(lifecycleGeneration: ownedGeneration) }
            }
        }
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
            if let durableOutbound {
                let receipts = transport.outboundReceipts
                outboundReceiptTask = Task { [weak self] in
                    for await receipt in receipts {
                        guard let self else { return }
                        await self.consumeOutboundReceipt(
                            receipt, durableOutbound: durableOutbound,
                            lifecycleGeneration: ownedGeneration)
                    }
                }
            }
        } catch {
            if lifecycleGeneration == ownedGeneration {
                connectTask = nil
                starting = false
                await transport.setInboundRefusalHandler(nil)
                await transport.setInboundDropHandler(nil)
                await transport.setConnectionObserver(nil)
                if noticesEnabled { status.setNoticeObserver(nil) }
            }
            throw error
        }
    }

    func stop() async {
        guard running || starting || connectTask != nil || commandTask != nil || readyTask != nil
                || outboundReceiptTask != nil
                || foregroundReadTask != nil || backgroundReadTask != nil
                || !lifecycleRefreshTasks.isEmpty || !publicationTasks.isEmpty
                || transcriptReportTask != nil || transcriptRepublicationTask != nil
        else { return }
        await transport.setInboundRefusalHandler(nil)
        await transport.setInboundDropHandler(nil)
        await transport.setConnectionObserver(nil)
        if noticesEnabled { status.setNoticeObserver(nil) }
        noticeTask?.cancel()
        noticeTask = nil
        let transcriptWork = stopTranscriptSignatures()
        lifecycleGeneration &+= 1
        starting = false
        running = false
        let command = commandTask
        let ready = readyTask
        let outboundReceipts = outboundReceiptTask
        let connect = connectTask
        let publications = Array(publicationTasks.values)
        let refusalPublication = refusalPublications.cancelAndReset()
        let readTasks = [foregroundReadTask, backgroundReadTask].compactMap { $0 }
            + Array(lifecycleRefreshTasks.values)
        commandTask = nil
        readyTask = nil
        outboundReceiptTask = nil
        connectTask = nil
        publicationTasks.removeAll()
        lifecycleRefreshTasks.removeAll()
        foregroundReadTask = nil
        backgroundReadTask = nil
        foregroundReadActive = false
        backgroundReadActive = false
        foregroundReads.removeAll()
        backgroundReads.removeAll()
        connect?.cancel()
        command?.cancel()
        ready?.cancel()
        outboundReceipts?.cancel()
        publications.forEach { $0.cancel() }
        readTasks.forEach { $0.cancel() }
        // Cancellation alone does not unblock Foundation's production WebSocket send. Establish
        // the stop fence first, close the socket next, and only then join the drain/deadline tasks.
        if let durableOutbound {
            await durableOutbound.stop { [transport] in await transport.shutdown() }
        } else {
            await transport.shutdown()
        }
        _ = await connect?.result
        await command?.value
        await ready?.value
        await outboundReceipts?.value
        for publication in publications {
            _ = await publication.result
        }
        await refusalPublication?.value
        for task in readTasks { await task.value }
        for task in transcriptWork { await task.value }
        publishedSessionIDs.removeAll()
        publishedSessionRows.removeAll()
        publishedSessionInventory = nil
        latestSessionRows.removeAll()
        latestSessionEnvelope = nil
        lastSessionFrameAt = nil
        sessionPublicationTail = nil
    }

    func isRunning() -> Bool { running }

    func refusalPublicationMetrics() -> CloudRefusalPublicationQueueMetrics {
        refusalPublications.snapshot()
    }

    func setTransportReadyObserver(_ observer: @escaping TransportReadyObserver) {
        transportReady = observer
    }

    /// Accepts the exact JSON bytes produced for local SSE, then fans its complete session rows
    /// out by channel. A complete authoritative scan also sends tombstones for rows that vanished.
    ///
    /// A row goes out only when it differs from the last one published for that Session outside
    /// `freshnessOnlySessionRowFields`; `transcript_signature`, added here and never present in the
    /// local bytes, counts as a difference like any other field.
    func publishSessions(_ payload: Data, force: Bool = false,
                         publicationID: String? = nil) async throws {
        let ownedGeneration = lifecycleGeneration
        try await runSessionPublication { [weak self] in
            guard let self else { throw CancellationError() }
            try await self.publishSessionsOwned(
                payload, force: force, publicationID: publicationID,
                lifecycleGeneration: ownedGeneration
            )
        }
    }

    /// The bytes a Cloud row is compared as: sorted keys, with the freshness-only fields removed.
    static func cloudSessionRowIdentity(_ row: [String: Any]) throws -> Data {
        func removing(_ path: ArraySlice<String>, from object: [String: Any]) -> [String: Any] {
            guard let key = path.first else { return object }
            var copy = object
            if path.count == 1 {
                copy.removeValue(forKey: key)
            } else if let child = copy[key] as? [String: Any] {
                copy[key] = removing(path.dropFirst(), from: child)
            }
            return copy
        }
        let stripped = freshnessOnlySessionRowFields.reduce(row) { removing($1[...], from: $0) }
        return try JSONSerialization.data(
            withJSONObject: stripped, options: [.sortedKeys, .withoutEscapingSlashes])
    }

    /// The facts in a row that select its transcript file (`Transcript.record(of:)`).
    static func transcriptBinding(_ row: [String: Any]) -> String {
        ["assistant", "tty", "sessionId", "cwd"]
            .map { (row[$0] as? String) ?? "-" }
            .joined(separator: "\u{1}")
    }

    private func runSessionPublication(
        _ work: @escaping @Sendable () async throws -> Void
    ) async throws {
        guard running else { throw CloudAppBridgeError.notRunning }
        let previous = sessionPublicationTail
        let turn = Task<Void, Error> {
            await previous?.value
            try await work()
        }
        sessionPublicationTail = Task { _ = await turn.result }
        try await runPublication {
            try await withTaskCancellationHandler {
                try await turn.value
            } onCancel: {
                turn.cancel()
            }
        }
    }

    /// Publish one Session row unless everything a viewer reads is what was last published.
    private func publishSessionRow(
        _ session: [String: Any], id: String, force: Bool,
        lifecycleGeneration ownedGeneration: UInt64
    ) async throws -> Bool {
        guard let envelope = latestSessionEnvelope else { return false }
        var row = session
        if let signature = knownTranscriptSignatures[id] {
            row[Self.transcriptSignatureField] = signature
        } else {
            row.removeValue(forKey: Self.transcriptSignatureField)
        }
        let identity = try Self.cloudSessionRowIdentity(row)
        if !force, publishedSessionRows[id] == identity { return false }
        let full: [String: Any] = ["session": row, "at": envelope.at, "scan": envelope.scan]
        try await publishJSON(
            full, channel: sessionChannel(id), lifecycleGeneration: ownedGeneration
        )
        publishedSessionRows[id] = identity
        lastSessionFrameAt = nowMilliseconds()
        return true
    }

    private func publishSessionsOwned(
        _ payload: Data, force: Bool, publicationID: String?,
        lifecycleGeneration ownedGeneration: UInt64
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
        latestSessionEnvelope = (at, scan)
        for (session, id) in zip(sessions, ids) {
            try requireActivePublication(lifecycleGeneration: ownedGeneration)
            latestSessionRows[id] = session
            if try await publishSessionRow(
                session, id: id, force: force, lifecycleGeneration: ownedGeneration
            ) {
                changed += 1
            } else {
                skipped += 1
            }
        }

        let removed = authoritative ? publishedSessionIDs.subtracting(current) : []
        if authoritative {
            for id in removed {
                try requireActivePublication(lifecycleGeneration: ownedGeneration)
                latestSessionRows[id] = nil
                knownTranscriptSignatures[id] = nil
                pendingTranscriptSessions.remove(id)
                let tombstone: [String: Any] = [
                    "session": NSNull(), "deleted": true, "at": at, "scan": scan,
                ]
                try await publishJSON(
                    tombstone, channel: sessionChannel(id),
                    lifecycleGeneration: ownedGeneration
                )
                publishedSessionRows[id] = nil
                lastSessionFrameAt = nowMilliseconds()
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
            let now = nowMilliseconds()
            let presenceDue = lastSessionFrameAt.map {
                now < $0 || now - $0 >= Self.sessionPresenceIntervalMilliseconds
            } ?? true
            if force || stable != publishedSessionInventory || presenceDue {
                try await publishJSON(
                    inventory, channel: sessionChannel(Self.sessionInventoryID),
                    lifecycleGeneration: ownedGeneration
                )
                publishedSessionInventory = stable
                lastSessionFrameAt = nowMilliseconds()
                inventoryPublished = true
            }
        }
        trackTranscriptSignatures(lifecycleGeneration: ownedGeneration)
        diagnostic("cloud: sessions published id=\(publicationID ?? "direct") "
            + "rows=\(sessions.count) changed=\(changed) "
            + "skipped=\(skipped) tombstones=\(removed.count) inventory=\(inventoryPublished) "
            + "force=\(force) total_ms=\(Self.elapsedMilliseconds(from: startedAt, to: nowMilliseconds()))")
    }

    // MARK: - Transcript signatures

    /// Name every published Session to the signature source, and start the one ordered consumer
    /// of its reports the first time. Only a running publication reaches here.
    private func trackTranscriptSignatures(lifecycleGeneration ownedGeneration: UInt64) {
        guard let transcriptSignatures, running, lifecycleGeneration == ownedGeneration else {
            return
        }
        if transcriptReports == nil {
            // Unbounded is bounded by its producer: one report per watched Session per watch
            // debounce, consumed with dictionary work only.
            var made: AsyncStream<(String, String?)>.Continuation!
            let reports = AsyncStream<(String, String?)> { made = $0 }
            transcriptReports = made
            transcriptReportTask = Task { [weak self] in
                for await (sessionID, signature) in reports {
                    guard let self else { return }
                    await self.transcriptSignatureObserved(
                        sessionID, signature: signature, lifecycleGeneration: ownedGeneration)
                }
            }
        }
        guard let continuation = transcriptReports else { return }
        let subjects = publishedSessionIDs.sorted().compactMap { id in
            latestSessionRows[id].map {
                CloudTranscriptSubject(sessionID: id, binding: Self.transcriptBinding($0))
            }
        }
        transcriptSignatures.track(subjects) { sessionID, signature in
            continuation.yield((sessionID, signature))
        }
    }

    private func transcriptSignatureObserved(
        _ sessionID: String, signature: String?, lifecycleGeneration ownedGeneration: UInt64
    ) {
        guard running, lifecycleGeneration == ownedGeneration else { return }
        let current = signature.flatMap { $0.isEmpty ? nil : $0 }
        guard knownTranscriptSignatures[sessionID] != current else { return }
        knownTranscriptSignatures[sessionID] = current
        guard latestSessionRows[sessionID] != nil else { return }
        pendingTranscriptSessions.insert(sessionID)
        scheduleTranscriptRepublication(lifecycleGeneration: ownedGeneration)
    }

    /// At once when the previous pass is older than the interval, otherwise when it has passed.
    /// Everything that changes meanwhile rides that one pass.
    private func scheduleTranscriptRepublication(lifecycleGeneration ownedGeneration: UInt64) {
        guard running, lifecycleGeneration == ownedGeneration, transcriptRepublicationTask == nil,
              !pendingTranscriptSessions.isEmpty else { return }
        let interval = transcriptRepublicationIntervalMilliseconds
        let now = nowMilliseconds()
        let elapsed = lastTranscriptRepublicationAt.map { now >= $0 ? now - $0 : 0 }
        let wait = elapsed.map { $0 >= interval ? 0 : interval - $0 } ?? 0
        let waitMilliseconds = self.waitMilliseconds
        transcriptRepublicationTask = Task { [weak self] in
            if wait > 0 {
                do { try await waitMilliseconds(wait) } catch { return }
            }
            await self?.republishTranscriptRows(lifecycleGeneration: ownedGeneration)
        }
    }

    private func republishTranscriptRows(lifecycleGeneration ownedGeneration: UInt64) async {
        transcriptRepublicationTask = nil
        guard running, lifecycleGeneration == ownedGeneration,
              !pendingTranscriptSessions.isEmpty else { return }
        let sessionIDs = pendingTranscriptSessions.sorted()
        pendingTranscriptSessions.removeAll()
        lastTranscriptRepublicationAt = nowMilliseconds()
        do {
            try await runSessionPublication { [weak self] in
                guard let self else { throw CancellationError() }
                try await self.republishTranscriptRowsOwned(
                    sessionIDs, lifecycleGeneration: ownedGeneration)
            }
        } catch {
            if running, lifecycleGeneration == ownedGeneration {
                diagnostic("cloud: transcript signature publication failed")
            }
        }
        scheduleTranscriptRepublication(lifecycleGeneration: ownedGeneration)
    }

    private func republishTranscriptRowsOwned(
        _ sessionIDs: [String], lifecycleGeneration ownedGeneration: UInt64
    ) async throws {
        var changed = 0
        for id in sessionIDs {
            try requireActivePublication(lifecycleGeneration: ownedGeneration)
            // A tombstone or a newer scan may have run first; only a still-published row goes.
            guard publishedSessionIDs.contains(id), let session = latestSessionRows[id] else {
                continue
            }
            if try await publishSessionRow(
                session, id: id, force: false, lifecycleGeneration: ownedGeneration
            ) {
                changed += 1
            }
        }
        diagnostic("cloud: transcript signatures published rows=\(sessionIDs.count) "
            + "changed=\(changed)")
    }

    /// End the demand: no watch outlives a stopped bridge. Returns the tasks `stop()` joins.
    private func stopTranscriptSignatures() -> [Task<Void, Never>] {
        transcriptSignatures?.stop()
        transcriptReports?.finish()
        transcriptReports = nil
        let tasks = [transcriptReportTask, transcriptRepublicationTask].compactMap { $0 }
        tasks.forEach { $0.cancel() }
        transcriptReportTask = nil
        transcriptRepublicationTask = nil
        knownTranscriptSignatures.removeAll()
        pendingTranscriptSessions.removeAll()
        lastTranscriptRepublicationAt = nil
        return tasks
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
        guard let object = (try? JSONSerialization.jsonObject(with: payload)) as? [String: Any],
              JSONSerialization.isValidJSONObject(object)
        else { throw CloudAppBridgeError.malformedOrchestrator }
        guard noticesEnabled else {
            try await publish(
                payload, channel: "orch/" + Self.channelSegment(identity.machineID),
                lifecycleGeneration: ownedGeneration
            )
            return
        }
        lastOrchestratorPayload = payload
        try await publishOrchestratorWithNotice(lifecycleGeneration: ownedGeneration)
    }

    /// The last orchestrator snapshot with the current `cloud_status` digest spliced in as one
    /// more top-level key, or a status-only object when no snapshot has arrived yet (§11.2). The
    /// snapshot's own bytes are kept: the digest is inserted before its closing brace rather than
    /// re-serializing a document that can be a megabyte.
    private func publishOrchestratorWithNotice(
        lifecycleGeneration ownedGeneration: UInt64
    ) async throws {
        status.clearNoticeRequest()
        lastNoticeAt = nowMilliseconds()
        let digest = try JSONSerialization.data(
            withJSONObject: status.noticeDigest(), options: [.withoutEscapingSlashes])
        let merged = Self.splice(cloudStatus: digest, into: lastOrchestratorPayload)
        try await publish(
            merged, channel: "orch/" + Self.channelSegment(identity.machineID),
            lifecycleGeneration: ownedGeneration
        )
    }

    static func splice(cloudStatus digest: Data, into payload: Data?) -> Data {
        let key = Data(#""cloud_status":"#.utf8)
        guard let payload,
              let object = (try? JSONSerialization.jsonObject(with: payload)) as? [String: Any],
              object["cloud_status"] == nil,
              let close = payload.lastIndex(where: { !Self.isJSONWhitespace($0) }),
              payload[close] == UInt8(ascii: "}")
        else {
            if let payload,
               var object = (try? JSONSerialization.jsonObject(with: payload)) as? [String: Any],
               let status = try? JSONSerialization.jsonObject(with: digest) {
                object["cloud_status"] = status
                if let bytes = try? JSONSerialization.data(
                    withJSONObject: object, options: [.withoutEscapingSlashes]) {
                    return bytes
                }
            }
            return Data("{".utf8) + key + digest + Data("}".utf8)
        }
        var merged = Data(payload[payload.startIndex..<close])
        if !object.isEmpty { merged.append(UInt8(ascii: ",")) }
        merged.append(key)
        merged.append(digest)
        merged.append(payload[close...])
        return merged
    }

    private static func isJSONWhitespace(_ byte: UInt8) -> Bool {
        byte == 0x20 || byte == 0x0a || byte == 0x0d || byte == 0x09
    }

    /// Called at most once per announcement cycle (see `CloudStatus.setNoticeObserver`). Publishes
    /// now when the last notice is older than the interval, otherwise once the interval has passed.
    private func noticeRequested(lifecycleGeneration ownedGeneration: UInt64) {
        guard noticesEnabled, running, lifecycleGeneration == ownedGeneration,
              noticeTask == nil else { return }
        let now = nowMilliseconds()
        let elapsed = lastNoticeAt.map { now >= $0 ? now - $0 : 0 }
        let wait = elapsed.map { $0 >= noticeIntervalMilliseconds ? 0 : noticeIntervalMilliseconds - $0 } ?? 0
        noticeTask = Task { [weak self] in
            if wait > 0 {
                do { try await Task.sleep(nanoseconds: wait * 1_000_000) } catch { return }
            }
            await self?.publishNotice(lifecycleGeneration: ownedGeneration)
        }
    }

    private func publishNotice(lifecycleGeneration ownedGeneration: UInt64) async {
        noticeTask = nil
        guard running, lifecycleGeneration == ownedGeneration else { return }
        do {
            try await runPublication { [weak self] in
                guard let self else { throw CancellationError() }
                try await self.publishOrchestratorWithNotice(lifecycleGeneration: ownedGeneration)
            }
        } catch {
            diagnostic("cloud: notice publication failed")
        }
    }

    private static func laneDropLine(_ refusal: CloudInboundAdmissionRefusal) -> String {
        CloudRefusalLog.line(
            layer: .macTransport, code: refusal.reason.refusalCode,
            sender: refusal.command.sender, sequence: refusal.command.sequence,
            request: nil, type: nil, session: nil, status: refusal.reason.refusalStatus,
            reply: "notice", extras: [("detail.reason", refusal.reason.rawValue),
                                      ("lane", "dropped_full")])
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
        readTraceID: String? = nil, scheduledAt: UInt64? = nil,
        replyFor reference: CommandReference? = nil
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
            if let durableOutbound {
                stage = "durable_enqueue"; startedAt = nowMilliseconds(); trace("begin")
                let stageObserver: CloudDurableOutboundComposition.EnqueueStageObserver?
                if let publicationID = readTraceID {
                    stageObserver = { [diagnostic, nowMilliseconds] part, duration, outcome,
                                      rowBytes, frameBytes in
                        diagnostic("cloud: publication id=\(publicationID) publication_stage=\(part) "
                            + "duration_ms=\(duration) outcome=\(outcome) "
                            + "row_bytes=\(rowBytes) frame_bytes=\(frameBytes) "
                            + "observed_at_ms=\(nowMilliseconds()) socket_send=not_observed "
                            + "ack=not_observed")
                    }
                } else {
                    stageObserver = nil
                }
                let spoolSequence = try await durableOutbound.enqueue(
                    plaintext, channel: channel,
                    logicalID: readTraceID ?? UUID().uuidString.lowercased(),
                    observe: stageObserver)
                if let publicationID = readTraceID {
                    let snapshot = await durableOutbound.metricsSnapshot()
                    diagnostic("cloud: publication id=\(publicationID) "
                        + "publication_stage=durable_state duration_ms=0 outcome=observed "
                        + "stored_rows=\(snapshot.storedRows) "
                        + "stored_charged_bytes=\(snapshot.storedChargedBytes) "
                        + "terminal_rows=\(snapshot.terminalRows) "
                        + "ready_rows=\(snapshot.readyWaitingRows) "
                        + "window_rows=\(snapshot.currentRows) socket_send=not_observed "
                        + "ack=not_observed")
                }
                if let reference {
                    status.recordReplySealed(sender: reference.sender, sequence: reference.sequence,
                                             spoolSequence: spoolSequence)
                }
                trace("complete")
                return
            }
            // Fixture-only compatibility. Production construction always supplies
            // `durableOutbound`; this path lets narrow bridge fakes observe legacy envelopes
            // without becoming a silent production fallback.
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
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
            try await transport.sendExactPublishFrame(
                try encoder.encode(CloudAppBridgeCompatibilityPublishFrame(envelope: envelope)))
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
        _ = stopTranscriptSignatures()
    }

    private func transportBecameReady(
        _ generation: UInt64, lifecycleGeneration ownedGeneration: UInt64
    ) async {
        guard running, lifecycleGeneration == ownedGeneration else { return }
        if let durableOutbound {
            await durableOutbound.requestDrain(reconnect: true)
        }
        transportReady(generation)
    }

    private func consumeOutboundReceipt(
        _ receipt: CloudOutboundTransportReceipt,
        durableOutbound: CloudDurableOutboundComposition,
        lifecycleGeneration ownedGeneration: UInt64
    ) async {
        guard running, lifecycleGeneration == ownedGeneration else { return }
        switch receipt.kind {
        case .delivered:
            status.recordReplyReceipt(spoolSequence: receipt.sequence, delivered: true)
        case .peerRejected:
            status.recordReplyReceipt(spoolSequence: receipt.sequence, delivered: false)
        case .viewerOffline:
            break
        }
        do { try await durableOutbound.settle(receipt) }
        catch { diagnostic("cloud: correlated outbound receipt could not settle durably") }
    }

    private func consume(
        _ inbound: CloudInboundCommand, lifecycleGeneration ownedGeneration: UInt64
    ) async {
        // Parsed before the write gate rather than after it, because the gate is not the same
        // question for a read: `Config.shared.remoteWrite` is "may a remote device type into a
        // session on this Mac", and a transcript read types into nothing. Everything that is not
        // a read — an unparseable body included — meets that gate exactly where it always did.
        let parsed = (try? JSONSerialization.jsonObject(with: inbound.plaintext)) as? [String: Any]
        let requestedType = parsed?["type"] as? String
        var reference = CommandReference(inbound, body: parsed)
        guard running, lifecycleGeneration == ownedGeneration else {
            diagnostic("cloud: " + CloudRefusalLog.line(
                layer: .macPreflight, code: "bridge_stopped", sender: inbound.sender,
                sequence: inbound.sequence, request: reference.request, type: reference.type,
                session: reference.session, status: nil, reply: "silent:bridge_stopped"))
            return
        }
        status.recordInboundAccepted()
        let wantedChannel = "ctl/" + Self.channelSegment(identity.machineID)
        guard inbound.channel == wantedChannel else {
            // The viewer listens on the channel it addressed, which is not one this Mac publishes;
            // an answer on this Mac's own channel would be read by nobody. The notice is the reply.
            await refuse(
                reference, layer: .macPreflight, status: 409, code: "wrong_machine",
                message: "This Cloud request addresses another Mac.", replyTo: nil,
                viaLane: false, lifecycleGeneration: ownedGeneration)
            return
        }
        if requestedType == "cloud.status" {
            await serveCloudStatus(parsed ?? [:], inbound: inbound, reference: reference,
                                   lifecycleGeneration: ownedGeneration)
            return
        }
        if let parsed, let requestedType, Self.readTypes.contains(requestedType) {
            // One legacy spelling performs a local refresh before returning its snapshot. The v2
            // catalog classifies that operation as a command, and this check makes the
            // classification an admission boundary rather than documentation: it cannot bypass
            // the same remote-write switch that protects every other effect.
            if CloudV2ReadCatalog.requiresWriteGate(for: requestedType) {
                status.recordCommand(
                    sender: reference.sender, sequence: reference.sequence,
                    request: reference.request, type: reference.type, session: reference.session)
                let authority = await currentCommandEffectAuthority(inbound.sender, true)
                if let denied = Self.refreshAuthorizationRefusal(authority) {
                    await refuse(
                        reference, layer: .macPreflight, status: denied.status,
                        code: denied.code, message: denied.message, detail: denied.detail,
                        replyTo: Self.readRefusalReply(type: requestedType, body: parsed),
                        viaLane: false, lifecycleGeneration: ownedGeneration)
                    return
                }
            }
            await serveRead(requestedType, body: parsed, inbound: inbound,
                            lifecycleGeneration: ownedGeneration)
            return
        }
        // Registering this already-paired browser as a notification destination is read-level on
        // the direct route, and so is handing over a diagnostic report. Neither may inherit the
        // separate switch for typing into a session.
        let readLevelCommand = Self.readLevelCommandTypes.contains(requestedType ?? "")
        status.recordCommand(
            sender: reference.sender, sequence: reference.sequence, request: reference.request,
            type: reference.type, session: reference.session)
        func malformed() async {
            await enqueueCommandRefusal(
                reference, body: parsed, type: requestedType, commandClass: inbound.commandClass,
                status: 400, code: "malformed_command", message: "This Cloud command is malformed.",
                lifecycleGeneration: ownedGeneration)
        }
        guard readLevelCommand || allowCloudCommands() else {
            await enqueueCommandRefusal(
                reference, body: parsed, type: requestedType, commandClass: inbound.commandClass,
                status: 403, code: "cloud_commands_disabled",
                message: "Cloud commands are disabled on this Mac.",
                lifecycleGeneration: ownedGeneration)
            return
        }
        guard let body = parsed, let type = requestedType else {
            await malformed()
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
                await malformed()
                return
            }
            command = .send(session: session, text: text, images: images)
            if sendKeys.contains("request") {
                guard let request = Self.requestName(body["request"]) else {
                    await malformed()
                    return
                }
                commandReply = (session, "action:" + request)
            }
        case "answer", "key":
            // `request` is optional (§11.4): an older page sends none, and a newer one names the
            // channel an answer or a refusal can reach it on.
            let allowedKeys: Set<String> = type == "answer"
                ? ["type", "session", "answer"] : ["type", "session", "key"]
            let keys = Set(body.keys)
            guard inbound.commandClass == .ctl,
                  keys == allowedKeys || keys == allowedKeys.union(["request"]),
                  let session = body["session"] as? String, !session.isEmpty,
                  let key = body[type == "answer" ? "answer" : "key"] as? String
            else {
                await malformed()
                return
            }
            command = .answer(session: session, key: key)
            if keys.contains("request") {
                guard let request = Self.requestName(body["request"]) else {
                    await malformed()
                    return
                }
                commandReply = (session, "action:" + request)
            }
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
                await malformed()
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
                await malformed()
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
                await malformed()
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
                await malformed()
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
                await enqueueCommandRefusal(
                    reference, body: body, type: type, commandClass: inbound.commandClass,
                    status: 400, code: "malformed_command",
                    message: "The shell-kill command is malformed.",
                    lifecycleGeneration: ownedGeneration)
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
                await malformed()
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
                await malformed()
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
                await malformed()
                return
            }
            if type == "schedule-create" {
                command = .scheduleCreate(body: data)
            } else {
                guard let id = body["id"] as? String, !id.isEmpty else {
                    await malformed()
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
                await malformed()
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
                await malformed()
                return
            }
            if type == "snippet-create" {
                command = .snippetCreate(body: data)
            } else {
                guard let id = body["id"] as? String, !id.isEmpty else {
                    await malformed()
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
                await malformed()
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
                await malformed()
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
                await malformed()
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
                await malformed()
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
                await malformed()
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
                await malformed()
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
                await malformed()
                return
            }
            command = .voice(audio: audio, rate: rate)
            commandReply = (session, "action:" + request)
        case "diagnostics.report":
            // §11.5: the same store and refusal words as `POST /v1/diagnostics/report`. A `report`
            // that is not an object still reaches that store, which names it `report_not_json`.
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "report"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let report = body["report"],
                  let data = try? JSONSerialization.data(
                    withJSONObject: report, options: [.fragmentsAllowed, .withoutEscapingSlashes])
            else {
                await malformed()
                return
            }
            command = .diagnosticsReport(body: data)
            commandReply = (session, "action:" + request)
        case "diagnostics.events":
            // A paired browser's receive-failure rows, sent without a press. Only the envelope of
            // the command is checked here; `CloudViewerEventLog` owns the batch's shape and bounds.
            guard inbound.commandClass == .ctl,
                  Set(body.keys) == ["type", "session", "request", "batch"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let batch = body["batch"] as? [String: Any],
                  JSONSerialization.isValidJSONObject(batch),
                  let data = try? JSONSerialization.data(
                    withJSONObject: batch, options: [.withoutEscapingSlashes])
            else {
                await malformed()
                return
            }
            command = .diagnosticsEvents(body: data)
            commandReply = (session, "action:" + request)
        case "dispatch":
            // cloud-client.js sends only `{task}`. The local broker protocol requires a materialized
            // task.json plus task_id and secret, and no pinned wire shape says how those are carried
            // or where the file is authorized to be written. Refuse instead of inventing one.
            await refuse(
                reference, layer: .macPreflight, status: 409, code: "cloud_dispatch_unpinned",
                message: "Cloud dispatch has no pinned wire shape on this Mac.",
                replyTo: Self.commandRefusalReply(
                    body: body, type: type, commandClass: inbound.commandClass),
                viaLane: false, lifecycleGeneration: ownedGeneration)
            return
        default:
            await refuse(
                reference, layer: .macPreflight, status: 400, code: "unknown_command",
                message: "This Mac does not know that Cloud command.",
                replyTo: Self.commandRefusalReply(
                    body: body, type: type, commandClass: inbound.commandClass),
                viaLane: false, lifecycleGeneration: ownedGeneration)
            return
        }
        if let reply = commandReply {
            reference.request = CloudStatus.identifier(String(reply.name.dropFirst("action:".count)))
                ?? reference.request
        }

        let routed: RoutedCommand
        if let commandLedger {
            routed = await routeDurably(
                command, body: body, inbound: inbound, ledger: commandLedger,
                requiresWriteGate: !readLevelCommand)
        } else {
            // Fixture-only compatibility. `CloudBridgeLifecycle.Services.production()` always
            // supplies a durable runtime, so no production effect reaches this branch.
            routed = RoutedCommand(
                result: await commandRouter.route(
                    command, sender: inbound.sender,
                    idempotencyKey: inbound.idempotencyKey),
                layer: .macRoute, executed: true)
        }
        let result = routed.result
        commandResult(result)
        let succeeded = (200..<300).contains(result.status)
        let code = result.code ?? (succeeded ? "succeeded" : "command_failed")
        if routed.executed {
            status.recordExecuted(sender: inbound.sender, sequence: inbound.sequence,
                                  outcome: succeeded ? "succeeded" : code)
        }
        guard let reply = commandReply else {
            if !succeeded {
                noteRefusal(reference, layer: routed.layer, code: code, status: result.status,
                            reply: .notice, detail: routed.detail)
            }
            return
        }
        let delivered = await publishJSONAnswer(
            name: reply.name, session: reply.session, status: result.status, body: result.body,
            layer: routed.layer, reference: reference, detail: routed.detail,
            lifecycleGeneration: ownedGeneration)
        if !succeeded {
            noteRefusal(reference, layer: routed.layer, code: code, status: result.status,
                        reply: delivered ? .published : .notice, detail: routed.detail)
        }
    }

    private func routeDurably(
        _ command: CloudHeadlessCommand, body: [String: Any], inbound: CloudInboundCommand,
        ledger: CloudCommandLedger, requiresWriteGate: Bool
    ) async -> RoutedCommand {
        let digest = Data(SHA256.hash(data: inbound.plaintext))
        let logicalRequestID = (body["request_id"] as? String)
            ?? (body["request"] as? String) ?? inbound.idempotencyKey
        let timestampSeconds = TimeInterval(inbound.timestamp) / 1_000
        let deadline = Date(timeIntervalSince1970: timestampSeconds + 300)
        let continuous = DispatchTime.now().uptimeNanoseconds
        let effectLimit = continuous.addingReportingOverflow(60_000_000_000)
        let request = CloudCommandLedgerRequest(
            viewerSender: inbound.sender, requestID: logicalRequestID,
            requestSHA256: digest, rawReplyKey: Data(), replyKeyID: identity.keyID,
            recipientDeviceID: inbound.sender, deadlineAt: deadline,
            effectNotAfterContinuous: effectLimit.overflow ? .max : effectLimit.partialValue)
        var effectAuthority: CloudCommandEffectAuthorization?
        do {
            let reservationAuthority = await currentCommandEffectAuthority(
                inbound.sender, requiresWriteGate)
            switch try await ledger.reserve(request, epochState: reservationAuthority.epochState) {
            case .cached(let outcome):
                let code = outcome.code == .succeeded ? nil : outcome.code.rawValue
                return RoutedCommand(
                    result: CloudCommandResult(
                        status: outcome.status, code: Self.routeCode(outcome.payload) ?? code,
                        body: outcome.payload ?? Data()),
                    layer: .macRoute, executed: false)
            case .reserved:
                break
            }
            // The durable reserve may suspend on disk. Re-read every revocable authority at the
            // point of no return instead of reusing admission-time facts.
            let authority = await currentCommandEffectAuthority(inbound.sender, requiresWriteGate)
            effectAuthority = authority
            try await ledger.beginEffect(
                request, epochState: authority.epochState,
                latestRosterAndGateAllow: authority.permitsEffect)
        } catch let error as CloudCommandLedgerError {
            return Self.ledgerRefusal(error, authority: effectAuthority)
        } catch {
            return RoutedCommand(
                result: Self.durabilityFailure(code: "command_reservation_not_durable"),
                layer: .macLedger, executed: false)
        }

        let result = await commandRouter.route(
            command, sender: inbound.sender, idempotencyKey: inbound.idempotencyKey)
        let outcome = CloudCommandNormalizedOutcome(
            code: Self.normalizedOutcomeCode(status: result.status),
            status: result.status, payload: result.body)
        do {
            try await ledger.complete(request, outcome: outcome)
            return RoutedCommand(result: result, layer: .macRoute, executed: true)
        } catch {
            // The effect crossed its point of no return but its outcome did not. Never publish
            // the undurable result; a duplicate observes `outcomeUnknown` after restart.
            return RoutedCommand(
                result: Self.durabilityFailure(code: "command_outcome_unknown"),
                layer: .macLedger, executed: true)
        }
    }

    private static func routeCode(_ payload: Data?) -> String? {
        guard let payload,
              let object = (try? JSONSerialization.jsonObject(with: payload)) as? [String: Any]
        else { return nil }
        return (object["error"] as? [String: Any])?["code"] as? String
    }

    private static func normalizedOutcomeCode(status: Int) -> CloudCommandOutcomeCode {
        switch status {
        case 200..<300: return .succeeded
        case 429: return .rateLimited
        case 503: return .unavailable
        case 500...599: return .internalError
        default: return .failed
        }
    }

    /// One ledger refusal with its own code and words (L1). `gateUnavailable` is told apart with
    /// the same effect-time reading that refused it: an unreadable roster, a sender that is not in
    /// the roster, and remote writes switched off are three different things to do about it.
    private static func ledgerRefusal(
        _ error: CloudCommandLedgerError, authority: CloudCommandEffectAuthorization?
    ) -> RoutedCommand {
        var detail: [String: Any] = [:]
        let refused: CloudCommandResult
        switch error {
        case .idempotencyCapacity:
            refused = durabilityFailure(code: "command_ledger_capacity", status: 429)
        case .idempotencyConflict:
            refused = durabilityFailure(code: "command_idempotency_conflict", status: 409)
        case .outcomeUnknown:
            refused = durabilityFailure(code: "command_outcome_unknown")
        case .deadlineExpired:
            refused = durabilityFailure(code: "command_deadline_expired", status: 408)
        case .epochUncertain:
            if let reason = authority?.epochReason { detail["reason"] = reason }
            if let clears = authority?.epochClearsInMilliseconds { detail["clears_in_ms"] = clears }
            refused = durabilityFailure(code: "command_clock_uncertain", detail: detail)
        case .gateUnavailable:
            if authority?.rosterReadable == false {
                refused = durabilityFailure(code: "command_roster_unreadable")
            } else if authority?.rosterAllowsSender == false {
                refused = durabilityFailure(code: "unknown_sender", status: 403)
            } else if authority?.writeGateAllows == false {
                refused = durabilityFailure(code: "command_writes_disabled", status: 403)
            } else {
                refused = durabilityFailure(code: "command_gate_unavailable")
            }
        case .reservationReleased:
            refused = durabilityFailure(code: "command_reservation_released", status: 429)
        case .durabilityUnavailable, .invalidTransition, .corrupt, .notFound:
            refused = durabilityFailure(code: "command_ledger_unavailable")
        }
        return RoutedCommand(result: refused, layer: .macLedger, executed: false, detail: detail)
    }

    private static let ledgerMessages: [String: String] = [
        "command_ledger_capacity": "This Mac's command ledger is full; try again later.",
        "command_idempotency_conflict": "That request id was already used for a different command.",
        "command_outcome_unknown": "This Mac cannot tell whether that command ran.",
        "command_deadline_expired": "That command arrived after its deadline.",
        "command_clock_uncertain": "This Mac is still confirming the time; try again shortly.",
        "command_roster_unreadable": "This Mac could not read its paired devices.",
        "unknown_sender": "This Mac does not recognise this device.",
        "command_writes_disabled": "Cloud commands are disabled on this Mac.",
        "command_gate_unavailable": "This Mac could not confirm the command is allowed.",
        "command_reservation_released": "That command was released before it ran; try again.",
        "command_ledger_unavailable": "This Mac's command ledger is unavailable.",
        "command_reservation_not_durable": "This Mac could not record that command durably.",
    ]

    private static func durabilityFailure(
        code: String, status: Int = 503, detail: [String: Any] = [:]
    ) -> CloudCommandResult {
        var error: [String: Any] = [
            "code": code,
            "message": ledgerMessages[code] ?? "The command durability boundary is unavailable.",
        ]
        let filtered = CloudErrorContract.detail(code: code, detail)
        if !filtered.isEmpty { error["detail"] = filtered }
        let bytes = (try? JSONSerialization.data(
            withJSONObject: ["error": error], options: [.withoutEscapingSlashes])) ?? Data()
        return CloudCommandResult(status: status, code: code, body: bytes)
    }

    /// The reads a viewer may name. Cloud v2 owns the exhaustive classification catalog, so a new
    /// bridge spelling cannot be admitted without deciding whether it is replicated, an
    /// effect-free live query, a command, or unsupported.
    ///
    /// This and the switch in `serveRead` are two lists that have to agree, which is why that
    /// switch ends in a `default` that refuses rather than in the last read: a word admitted here
    /// and unknown there fails closed instead of being parsed as whichever case happens to sit at
    /// the bottom. `every read type this bridge admits also parses` walks this set and asks each
    /// member for a well-formed body, so the two cannot come apart quietly.
    static let readTypes: Set<String> = CloudV2ReadCatalog.readTypeNames

    /// Commands a paired device may send with remote writes switched off.
    static let readLevelCommandTypes: Set<String> = [
        "push-subscribe", "push-unsubscribe", "push-test", "diagnostics.report",
        "diagnostics.events",
    ]

    fileprivate static func requestName(_ value: Any?) -> String? {
        guard let value = value as? String, !value.isEmpty, value.count <= 128,
              value.unicodeScalars.allSatisfy({ $0.value >= 0x20 && $0.value != 0x7f })
        else { return nil }
        return value
    }

    /// Name the answer channel only from a closed command vocabulary and the same lexical
    /// identity fields the command parser consumes. This lets a preflight refusal settle the
    /// browser's request without turning a malformed body into an arbitrary transcript publish.
    private static func commandRefusalReply(
        body: [String: Any]?, type: String?, commandClass: CloudEnvelopeClass
    ) -> (session: String, name: String)? {
        guard let body, let type else { return nil }
        if type == "dispatch" {
            // A dispatch names no Session; with a request it is answered where every machine
            // request is. Any `session` it carries must already be that channel.
            guard let request = requestName(body["request"]) else { return nil }
            if let session = body["session"], session as? String != machineReplySession {
                return nil
            }
            return (machineReplySession, "action:" + request)
        }
        guard commandClass == .ctl else { return nil }
        if type == "schedule-webhook-bind-v1" {
            guard let request = body["request_id"] as? String,
                  UUID(uuidString: request) != nil, request == request.lowercased()
            else { return nil }
            return (machineReplySession, "action:" + request)
        }
        guard let request = requestName(body["request"]),
              let session = body["session"] as? String,
              !session.isEmpty, session.count <= 256,
              session.unicodeScalars.allSatisfy({ $0.value >= 0x20 && $0.value != 0x7f })
        else { return nil }
        let sessionCommands: Set<String> = ["send", "answer", "key", "end", "focus", "shell-kill"]
        if sessionCommands.contains(type) { return (session, "action:" + request) }
        // Every machine command, and any word this Mac does not know, is answered only on the one
        // machine reply channel, so neither a malformed nor an unknown body can name a Session.
        guard session == machineReplySession else { return nil }
        return (session, "action:" + request)
    }

    /// Where a refused read is answered: only a request-scoped read that names its request (P1).
    /// A Session read without one — a transcript, an agent, a picture — is announced as a notice
    /// instead, because a malformed body cannot be trusted to name the waiter it would settle.
    private static func readRefusalReply(
        type: String, body: [String: Any]
    ) -> (session: String, name: String)? {
        let requestReads: Set<String> = [
            "document", "board", "timeline", "places", "project-worktrees",
            "project-worktree-lifecycle", "project-worktree-lifecycle-refresh", "past-sessions",
            "schedules", "snippets", "schedule", "push-key", "cloud.status",
        ]
        guard requestReads.contains(type),
              let request = requestName(body["request"]),
              let session = body["session"] as? String, !session.isEmpty, session.count <= 256,
              session.unicodeScalars.allSatisfy({ $0.value >= 0x20 && $0.value != 0x7f }),
              type == "document" || session == machineReplySession
        else { return nil }
        return (session, "read:" + request)
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
        lifecycleGeneration ownedGeneration: UInt64, refusal: ReadRefusal? = nil
    ) async {
        let reference = CommandReference(inbound, body: body)
        // A read that cannot be parsed is still answered where its fields safely say it would
        // have been. An ingress refusal keeps its own code: the body was never the problem.
        func malformedRead() async {
            await refuse(
                reference, layer: refusal?.layer ?? .macPreflight,
                status: refusal?.status ?? 400, code: refusal?.code ?? "malformed_read",
                message: refusal?.message ?? "This Cloud read is malformed.",
                detail: refusal?.detail ?? [:],
                replyTo: Self.readRefusalReply(type: type, body: body),
                viaLane: refusal == nil, lifecycleGeneration: ownedGeneration)
        }
        // A read rides the command channel and therefore its class, which is what the relay bills
        // and what `CloudEnvelope` pins. `dispatch` is a command class and never a read.
        guard inbound.commandClass == .ctl else {
            await malformedRead()
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
                await malformedRead()
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
                await malformedRead()
                return
            }
            read = .transcript(session: session, limit: limit, priority: priority)
        case "info":
            guard Set(body.keys) == ["type", "session", "parts"],
                  let session = body["session"] as? String, !session.isEmpty,
                  let parts = body["parts"] as? String,
                  parts == "full" || parts == "summary"
            else {
                await malformedRead()
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
                await malformedRead()
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
                await malformedRead()
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
                await malformedRead()
                return
            }
            read = .skills(session: session)
        case "git":
            guard Set(body.keys) == ["type", "session"],
                  let session = body["session"] as? String, !session.isEmpty
            else {
                await malformedRead()
                return
            }
            read = .git(session: session)
        case "screen":
            guard Set(body.keys) == ["type", "session"],
                  let session = body["session"] as? String, !session.isEmpty
            else {
                await malformedRead()
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
                await malformedRead()
                return
            }
            read = .image(session: session, id: id)
        case "documents":
            guard Set(body.keys) == ["type", "session"],
                  let session = body["session"] as? String, !session.isEmpty
            else {
                await malformedRead()
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
                await malformedRead()
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
                await malformedRead()
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
                await malformedRead()
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
                await malformedRead()
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
                await malformedRead()
                return
            }
            read = .projectWorktrees(session: session, request: request, project: project)
        case "project-worktree-lifecycle", "project-worktree-lifecycle-refresh":
            // The same exact key set as the Portfolio worktree join, but a Board Project id rather
            // than a path: `ProjectWorktreeHTTP` refuses any other shape before the service runs.
            guard Set(body.keys) == ["type", "session", "request", "project"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let project = body["project"] as? String, !project.isEmpty, project.count <= 200
            else {
                await malformedRead()
                return
            }
            read = type == "project-worktree-lifecycle"
                ? .projectWorktreeLifecycle(session: session, request: request, project: project)
                : .projectWorktreeLifecycleRefresh(session: session, request: request, project: project)
        case "past-sessions":
            guard Set(body.keys) == ["type", "session", "request", "place", "assistant"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"]),
                  let place = body["place"] as? String, !place.isEmpty,
                  let assistant = body["assistant"] as? String
            else {
                await malformedRead()
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
                await malformedRead()
                return
            }
            read = .schedules(session: session, request: request)
        case "snippets":
            guard Set(body.keys) == ["type", "session", "request"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"])
            else {
                await malformedRead()
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
                await malformedRead()
                return
            }
            read = .schedule(session: session, request: request, id: id)
        case "push-key":
            guard Set(body.keys) == ["type", "session", "request"],
                  let session = body["session"] as? String,
                  session == Self.machineReplySession,
                  let request = Self.requestName(body["request"])
            else {
                await malformedRead()
                return
            }
            read = .pushKey(session: session, request: request)
        default:
            // `readTypes` admitted a word this switch does not know, which means the two lists
            // have come apart. Fail closed rather than reading it as whichever case sits last —
            // that is how a misspelled `info` would have become a transcript.
            await malformedRead()
            return
        }

        if let refusal {
            await refuse(
                reference, layer: refusal.layer, status: refusal.status, code: refusal.code,
                message: refusal.message, detail: refusal.detail,
                replyTo: (read.session, read.name), viaLane: false,
                lifecycleGeneration: ownedGeneration)
        } else {
            await enqueueRead(read, reference: reference, lifecycleGeneration: ownedGeneration)
        }
    }

    /// `cloud.status` (§11.3): the whole snapshot, answered on the machine reply channel. It reads
    /// memory this bridge already owns, so it skips the read lanes rather than waiting in one.
    private func serveCloudStatus(
        _ body: [String: Any], inbound: CloudInboundCommand, reference: CommandReference,
        lifecycleGeneration ownedGeneration: UInt64
    ) async {
        guard inbound.commandClass == .ctl,
              Set(body.keys) == ["type", "session", "request"],
              let session = body["session"] as? String,
              session == Self.machineReplySession,
              let request = Self.requestName(body["request"])
        else {
            await refuse(
                reference, layer: .macPreflight, status: 400, code: "malformed_read",
                message: "This Cloud read is malformed.",
                replyTo: Self.readRefusalReply(type: "cloud.status", body: body),
                viaLane: true, lifecycleGeneration: ownedGeneration)
            return
        }
        let payload: [String: Any] = [
            "read": "read:" + request, "status": 200, "body": status.snapshot(),
        ]
        commandResult(CloudCommandResult(status: 200, code: nil))
        _ = await publishPayload(payload, session: session, reference: nil,
                                 lifecycleGeneration: ownedGeneration)
    }

    private func enqueueRead(
        _ read: CloudHeadlessRead, reference: CommandReference,
        lifecycleGeneration ownedGeneration: UInt64
    ) async {
        if case .projectWorktreeLifecycleRefresh = read {
            let limit = 4
            let depth = lifecycleRefreshTasks.count
            guard depth < limit else {
                await enqueueReadRefusal(
                    read, reference: reference, status: 429, code: "cloud_read_busy",
                    message: "That Cloud read lane is full; retry shortly.",
                    extra: ["lane": ReadLane.background.rawValue, "limit": limit,
                            "retry_after": 1], lifecycleGeneration: ownedGeneration
                )
                return
            }
            let id = UUID()
            let admittedAt = nowMilliseconds()
            lifecycleRefreshTasks[id] = Task { [weak self] in
                guard let self else { return }
                // Admission may wait behind other lifecycle work. Re-read roster, guarded clock,
                // and the write gate immediately before the process-running cache mutation.
                let authority = await self.currentCommandEffectAuthority(reference.sender, true)
                if let denied = Self.refreshAuthorizationRefusal(authority) {
                    await self.refuse(
                        reference, layer: .macPreflight, status: denied.status,
                        code: denied.code, message: denied.message, detail: denied.detail,
                        replyTo: (read.session, read.name), viaLane: false,
                        lifecycleGeneration: ownedGeneration)
                } else {
                    await self.performRead(
                        read, reference: reference, lifecycleGeneration: ownedGeneration,
                        admittedAt: admittedAt)
                }
                await self.finishLifecycleRefresh(id, lifecycleGeneration: ownedGeneration)
            }
            diagnostic("cloud: read admitted read=\(read.name) lane=lifecycle-refresh "
                + "depth=\(depth + 1) limit=\(limit)")
            return
        }
        let lane: ReadLane
        if case .transcript(_, _, .foreground) = read { lane = .foreground }
        else { lane = .background }
        let depth = lane == .foreground
            ? foregroundReads.count + (foregroundReadActive ? 1 : 0)
            : backgroundReads.count + (backgroundReadActive ? 1 : 0)
        let limit = lane == .foreground ? 4 : 16
        guard depth < limit else {
            diagnostic("cloud: read refused read=\(read.name) lane=\(lane.rawValue) "
                + "depth=\(depth) limit=\(limit) code=cloud_read_busy")
            await enqueueReadRefusal(
                read, reference: reference, status: 429, code: "cloud_read_busy",
                message: "That Cloud read lane is full; retry shortly.",
                extra: ["lane": lane.rawValue, "limit": limit, "retry_after": 1],
                lifecycleGeneration: ownedGeneration
            )
            return
        }
        let pending = PendingRead(
            read: read, reference: reference, lifecycleGeneration: ownedGeneration,
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

    private func finishLifecycleRefresh(_ id: UUID, lifecycleGeneration ownedGeneration: UInt64) {
        guard lifecycleGeneration == ownedGeneration else { return }
        lifecycleRefreshTasks[id] = nil
    }

    /// `lane`, `limit` and `retry_after` stay top-level on this answer, where they always were
    /// (§11.1); the browser copies them into its own `detail`.
    private func enqueueReadRefusal(
        _ read: CloudHeadlessRead, reference: CommandReference, status httpStatus: Int,
        code: String, message: String, extra: [String: Any] = [:],
        lifecycleGeneration ownedGeneration: UInt64
    ) async {
        await refuse(
            reference, layer: .macPreflight, status: httpStatus, code: code,
            message: message, extra: extra, replyTo: (read.session, read.name),
            viaLane: true, lifecycleGeneration: ownedGeneration)
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
                next.read, reference: next.reference,
                lifecycleGeneration: next.lifecycleGeneration, admittedAt: next.admittedAt
            )
            if lane == .foreground { foregroundReadActive = false }
            else { backgroundReadActive = false }
        }
        if lane == .foreground { foregroundReadTask = nil }
        else { backgroundReadTask = nil }
    }

    private func performRead(
        _ read: CloudHeadlessRead, reference: CommandReference,
        lifecycleGeneration ownedGeneration: UInt64, admittedAt: UInt64
    ) async {
        let sender = reference.sender
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
        let failed = !(200..<300).contains(outcome.status)
        guard running, lifecycleGeneration == ownedGeneration else {
            if failed {
                diagnostic("cloud: " + CloudRefusalLog.line(
                    layer: .macRoute, code: outcome.code ?? "read_failed", sender: sender,
                    sequence: reference.sequence, request: reference.request,
                    type: reference.type, session: reference.session, status: outcome.status,
                    reply: "silent:bridge_stopped"))
            }
            return
        }
        var payload: [String: Any] = ["read": read.name, "status": outcome.status]
        if let body = outcome.body {
            payload["body"] = body
        } else {
            // Whatever went wrong, the viewer gets a code it can branch on rather than silence.
            let code = outcome.code ?? "read_failed"
            payload["error"] = CloudErrorContract.error(
                outcome.error ?? [:], code: code, layer: .macRoute, sequence: reference.sequence)
        }
        guard JSONSerialization.isValidJSONObject(payload),
              let bytes = try? JSONSerialization.data(
                  withJSONObject: payload, options: [.withoutEscapingSlashes])
        else {
            await refuse(
                reference, layer: .macRoute, status: 500, code: "unserializable_read",
                message: "This read's answer could not be serialized.",
                replyTo: (read.session, read.name), viaLane: false,
                lifecycleGeneration: ownedGeneration)
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
            if failed {
                noteRefusal(reference, layer: .macRoute, code: outcome.code ?? "read_failed",
                            status: outcome.status, reply: .published)
            }
        } catch {
            // The answer's own channel is the only way back to the asker, so a publication that
            // cannot leave is recorded and announced in the notice instead.
            commandResult(CloudCommandResult(status: 503, code: "read_answer_undeliverable"))
            let failedAt = nowMilliseconds()
            diagnostic("cloud: read delivery_failed id=\(traceID) read=\(read.name) "
                + "publish_ms=\(Self.elapsedMilliseconds(from: publishStartedAt, to: failedAt)) "
                + "total_ms=\(Self.elapsedMilliseconds(from: receivedAt, to: failedAt))")
            status.recordUndeliverable(sender: sender, sequence: reference.sequence,
                                       reason: "read_answer_undeliverable")
            noteRefusal(reference, layer: .macReply, code: "read_answer_undeliverable",
                        status: 503, reply: .notice)
        }
    }

    private static func elapsedMilliseconds(from start: UInt64, to end: UInt64) -> UInt64 {
        guard end >= start else { return 0 }
        return end - start
    }

    /// Publishes a command's answer. A non-2xx answer's `error` gains `layer`, `seq` and a
    /// whitelisted `detail` (§11.1). `false` means it could not be handed to the outbound owner,
    /// in which case the refusal is recorded as undeliverable and announced as a notice.
    @discardableResult
    private func publishJSONAnswer(
        name: String, session: String, status httpStatus: Int, body: Data,
        layer: CloudRefusalLayer, reference: CommandReference, detail: [String: Any] = [:],
        lifecycleGeneration ownedGeneration: UInt64
    ) async -> Bool {
        let parsed = (try? JSONSerialization.jsonObject(with: body)) as? [String: Any]
        var payload: [String: Any] = ["read": name, "status": httpStatus]
        if (200..<300).contains(httpStatus), let parsed {
            payload["body"] = parsed
        } else {
            let base = (parsed?["error"] as? [String: Any])
                ?? ["code": "command_failed", "message": "This command could not be completed."]
            let code = base["code"] as? String ?? "command_failed"
            var candidate = detail
            if let routeDetail = base["detail"] as? [String: Any] {
                candidate.merge(routeDetail) { current, _ in current }
            }
            var error = base
            error.removeValue(forKey: "detail")
            payload["error"] = CloudErrorContract.error(
                error, code: code, layer: layer, sequence: reference.sequence, detail: candidate)
        }
        let delivered = await publishPayload(
            payload, session: session, reference: reference, lifecycleGeneration: ownedGeneration)
        if !delivered {
            commandResult(CloudCommandResult(status: 503, code: "command_answer_undeliverable"))
            status.recordUndeliverable(sender: reference.sender, sequence: reference.sequence,
                                       reason: "command_answer_undeliverable")
            if (200..<300).contains(httpStatus) {
                noteRefusal(reference, layer: .macReply, code: "command_answer_undeliverable",
                            status: 503, reply: .notice)
            }
        }
        return delivered
    }

    /// `true` once the payload is sealed into the outbound owner. That is not a relay receipt;
    /// a later receipt for the same spool row marks the command `delivered`.
    private func publishPayload(
        _ payload: [String: Any], session: String, reference: CommandReference?,
        lifecycleGeneration ownedGeneration: UInt64
    ) async -> Bool {
        guard running, lifecycleGeneration == ownedGeneration,
              JSONSerialization.isValidJSONObject(payload),
              let bytes = try? JSONSerialization.data(
                withJSONObject: payload, options: [.withoutEscapingSlashes])
        else { return false }
        do {
            try await runPublication { [weak self] in
                guard let self else { throw CancellationError() }
                try await self.publish(bytes, channel: transcriptChannel(session),
                                       lifecycleGeneration: ownedGeneration,
                                       replyFor: reference)
            }
            return true
        } catch {
            return false
        }
    }

    /// A preflight command refusal: answered through the bounded refusal lane on the action channel
    /// its identity safely names, or announced as a notice when it names none.
    private func enqueueCommandRefusal(
        _ reference: CommandReference, body: [String: Any]?, type: String?,
        commandClass: CloudEnvelopeClass, status httpStatus: Int, code: String, message: String,
        lifecycleGeneration ownedGeneration: UInt64
    ) async {
        await refuse(
            reference, layer: .macPreflight, status: httpStatus, code: code, message: message,
            replyTo: Self.commandRefusalReply(body: body, type: type, commandClass: commandClass),
            viaLane: true, lifecycleGeneration: ownedGeneration)
    }

    /// Every refusal the bridge decides goes through here: the observer result, the status
    /// snapshot, one §2.3 log line, and either a published answer or a notice. Nothing that
    /// reaches this function is silent.
    private func refuse(
        _ reference: CommandReference, layer: CloudRefusalLayer, status httpStatus: Int,
        code: String, message: String, detail: [String: Any] = [:], extra: [String: Any] = [:],
        replyTo: (session: String, name: String)?, viaLane: Bool,
        lifecycleGeneration ownedGeneration: UInt64
    ) async {
        var base: [String: Any] = ["code": code, "message": message]
        extra.forEach { base[$0.key] = $0.value }
        let error = CloudErrorContract.error(
            base, code: code, layer: layer, sequence: reference.sequence, detail: detail)
        let bytes = (try? JSONSerialization.data(
            withJSONObject: ["error": error], options: [.withoutEscapingSlashes])) ?? Data()
        commandResult(CloudCommandResult(status: httpStatus, code: code, body: bytes))
        guard let replyTo else {
            noteRefusal(reference, layer: layer, code: code, status: httpStatus, reply: .notice,
                        detail: detail)
            return
        }
        let payload: [String: Any] = ["read": replyTo.name, "status": httpStatus, "error": error]
        guard viaLane else {
            await deliverRefusal(payload, to: replyTo.session, reference: reference, layer: layer,
                                 code: code, status: httpStatus, detail: detail,
                                 lifecycleGeneration: ownedGeneration)
            return
        }
        let admitted = refusalPublications.enqueue { [weak self] in
            await self?.deliverRefusal(
                payload, to: replyTo.session, reference: reference, layer: layer, code: code,
                status: httpStatus, detail: detail, lifecycleGeneration: ownedGeneration)
        }
        if !admitted {
            status.recordLaneDropped()
            noteRefusal(reference, layer: layer, code: code, status: httpStatus, reply: .notice,
                        detail: detail, extras: [("lane", "dropped_full")])
        }
    }

    private func deliverRefusal(
        _ payload: [String: Any], to session: String, reference: CommandReference,
        layer: CloudRefusalLayer, code: String, status httpStatus: Int, detail: [String: Any],
        lifecycleGeneration ownedGeneration: UInt64
    ) async {
        let delivered = await publishPayload(
            payload, session: session, reference: reference, lifecycleGeneration: ownedGeneration)
        if !delivered {
            status.recordUndeliverable(sender: reference.sender, sequence: reference.sequence,
                                       reason: "refusal_undeliverable")
        }
        noteRefusal(reference, layer: layer, code: code, status: httpStatus,
                    reply: delivered ? .published : .notice, detail: detail)
    }

    /// Records one refusal in the status snapshot and writes its one log line.
    private func noteRefusal(
        _ reference: CommandReference, layer: CloudRefusalLayer, code: String, status httpStatus: Int,
        reply: CloudStatus.Reply, detail: [String: Any] = [:], extras: [(String, String?)] = []
    ) {
        status.recordRefusal(
            sender: reference.sender, sequence: reference.sequence, request: reference.request,
            type: reference.type, session: reference.session, layer: layer, code: code, reply: reply)
        var fields = extras
        for key in detail.keys.sorted() {
            fields.append(("detail." + key, detail[key].map { "\($0)" }))
        }
        diagnostic("cloud: " + CloudRefusalLog.line(
            layer: layer, code: code, sender: reference.sender, sequence: reference.sequence,
            request: reference.request, type: reference.type, session: reference.session,
            status: httpStatus, reply: reply == .published ? "published" : "notice",
            extras: fields))
    }

    /// A command rejected before routing still has two observers: local diagnostics and the
    /// request-scoped Cloud caller. It runs inside the refusal lane, so it publishes directly.
    private func consumeInboundRefusal(
        _ refusal: CloudInboundAdmissionRefusal,
        lifecycleGeneration ownedGeneration: UInt64
    ) async {
        guard running, lifecycleGeneration == ownedGeneration else { return }
        let inbound = refusal.command
        let parsed = (try? JSONSerialization.jsonObject(with: inbound.plaintext)) as? [String: Any]
        let requestedType = parsed?["type"] as? String
        let reference = CommandReference(inbound, body: parsed)
        let wantedChannel = "ctl/" + Self.channelSegment(identity.machineID)
        if inbound.channel != wantedChannel {
            await refuse(
                reference, layer: .macPreflight, status: 409, code: "wrong_machine",
                message: "This Cloud request addresses another Mac.", replyTo: nil,
                viaLane: false, lifecycleGeneration: ownedGeneration)
            return
        }
        let limit = refusal.reason == .chargedByteCap
            ? refusal.metrics.maximumChargedBytes : refusal.metrics.maximumCount
        let busyDetail: [String: Any] = refusal.reason == .plaintextCap
            ? [:] : ["lane": "ingress", "limit": limit, "retry_after": 1]
        let code = refusal.reason.refusalCode
        let message = refusal.reason == .plaintextCap
            ? "This command is larger than this Mac accepts over Cloud."
            : "This request was not accepted because Cloud ingress is full; try again shortly."
        if let parsed, let requestedType,
           Self.readTypes.contains(requestedType) || requestedType == "cloud.status" {
            if requestedType == "cloud.status" {
                await refuse(
                    reference, layer: .macTransport, status: refusal.reason.refusalStatus,
                    code: code, message: message, detail: busyDetail,
                    replyTo: Self.readRefusalReply(type: requestedType, body: parsed),
                    viaLane: false, lifecycleGeneration: ownedGeneration)
                return
            }
            if CloudV2ReadCatalog.requiresWriteGate(for: requestedType), !allowCloudCommands() {
                await refuse(
                    reference, layer: .macPreflight, status: 403,
                    code: "cloud_commands_disabled",
                    message: "Cloud commands are disabled on this Mac.",
                    replyTo: Self.readRefusalReply(type: requestedType, body: parsed),
                    viaLane: false, lifecycleGeneration: ownedGeneration)
                return
            }
            await serveRead(
                requestedType, body: parsed, inbound: inbound,
                lifecycleGeneration: ownedGeneration,
                refusal: ReadRefusal(
                    layer: .macTransport, status: refusal.reason.refusalStatus, code: code,
                    message: message, detail: busyDetail)
            )
            return
        }
        let readLevelCommand = Self.readLevelCommandTypes.contains(requestedType ?? "")
        let reply = Self.commandRefusalReply(
            body: parsed, type: requestedType, commandClass: inbound.commandClass)
        status.recordCommand(
            sender: reference.sender, sequence: reference.sequence, request: reference.request,
            type: reference.type, session: reference.session)
        if !readLevelCommand && !allowCloudCommands() {
            await refuse(
                reference, layer: .macPreflight, status: 403, code: "cloud_commands_disabled",
                message: "Cloud commands are disabled on this Mac.", replyTo: reply,
                viaLane: false, lifecycleGeneration: ownedGeneration)
            return
        }
        await refuse(
            reference, layer: .macTransport, status: refusal.reason.refusalStatus, code: code,
            message: message, detail: busyDetail, replyTo: reply, viaLane: false,
            lifecycleGeneration: ownedGeneration)
    }

    private static func refreshAuthorizationRefusal(
        _ authority: CloudCommandEffectAuthorization
    ) -> (status: Int, code: String, message: String, detail: [String: Any])? {
        if authority.epochState != .ready {
            var detail: [String: Any] = [:]
            if let reason = authority.epochReason { detail["reason"] = reason }
            if let clears = authority.epochClearsInMilliseconds {
                detail["clears_in_ms"] = clears
            }
            return (503, "command_clock_uncertain",
                    "This Mac is still confirming the time; try again shortly.", detail)
        }
        if !authority.rosterReadable {
            return (503, "command_roster_unreadable",
                    "This Mac could not read its paired devices.", [:])
        }
        if !authority.rosterAllowsSender {
            return (403, "unknown_sender", "This Mac does not recognise this device.", [:])
        }
        if !authority.writeGateAllows {
            return (403, "cloud_commands_disabled",
                    "Cloud commands are disabled on this Mac.", [:])
        }
        return nil
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

#endif
