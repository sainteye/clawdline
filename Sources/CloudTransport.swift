import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
#if os(Linux)
import NIOCore
import NIOHTTP1
import NIOPosix
import NIOSSL
import NIOWebSocket
#endif
#if canImport(ClawdlineApplication) && !CLAWDLINE_APPLICATION_TARGET
import ClawdlineApplication
#endif

// The token and reconnect identity contract belongs to ClawdlineApplication. The flat test
// graph compiles it from Sources; the separated Mac SwiftPM target imports that one definition.
#if !SWIFT_PACKAGE || CLAWDLINE_APPLICATION_TARGET

public enum CloudTransportRole: String, Sendable {
    case machine
}

public enum CloudTransportState: Equatable, Sendable {
    case idle
    case connecting
    case ready
    case reconnecting
    case shutDown
}

public enum CloudTransportError: Error, LocalizedError, Equatable, Sendable {
    case alreadyConnected
    case invalidRelayURL
    case invalidTokenResponse
    /// The control plane refused this device's credential outright — 401, or the 403 a revoked
    /// machine or device gets from `POST /v1/tokens/device`. Separate from
    /// `invalidTokenResponse` because retrying it is pointless: the reconnect loop here treats
    /// every failure as transient, so somebody above has to be able to tell the two apart.
    case unauthorized
    case upgradeRefused(statusCode: Int)
    case connectionTimedOut
    case challengeTimedOut
    case readyTimedOut
    case authenticationTimedOut
    case receiveTimedOut
    case connectionFailed(String)
    /// This transport closed its own socket because the device token is being rotated, and the
    /// reconnect that follows is the rotation finishing rather than anything going wrong. It is
    /// a separate case because the socket close arrives back through `receive` as an ordinary
    /// URLSession error and is otherwise indistinguishable from a relay that dropped us: on
    /// 2026-09-03 a healthy Mac logged `reason=connection_failed` every four minutes — 300-second
    /// token TTL less the 60-second `refreshAhead` — and that line was read as "the Mac cannot
    /// reach the relay" while it was in fact connected and publishing throughout.
    case tokenRotated
    case notConnected
    case unexpectedFrame(String)
    case relay(String, String)

    public var errorDescription: String? {
        switch self {
        case .alreadyConnected:
            return "CloudTransport is already connected."
        case .invalidRelayURL:
            return "The cloud relay URL is invalid."
        case .invalidTokenResponse:
            return "The device-token response is invalid."
        case .unauthorized:
            return "Clawdline Cloud refused this device's credential."
        case .upgradeRefused(let statusCode):
            return "The cloud relay refused the WebSocket upgrade (HTTP \(statusCode))."
        case .connectionTimedOut:
            return "The cloud relay WebSocket did not open before its deadline."
        case .challengeTimedOut:
            return "The cloud relay did not send its authentication challenge before the deadline."
        case .readyTimedOut:
            return "The cloud relay did not confirm authentication before the deadline."
        case .authenticationTimedOut:
            return "The cloud relay authentication handshake did not finish before its deadline."
        case .receiveTimedOut:
            return "The cloud relay stopped sending frames before its receive deadline."
        case .connectionFailed(let reason):
            return "The cloud relay connection failed: \(reason)"
        case .tokenRotated:
            return "The cloud relay connection is being rebuilt with a fresh device token."
        case .notConnected:
            return "CloudTransport is not connected."
        case .unexpectedFrame(let type):
            return "The relay sent an unexpected \(type) frame."
        case .relay(let code, let message):
            return "The relay refused the request (\(code)): \(message)"
        }
    }
}

public struct CloudDeviceToken: Equatable, Sendable {
    public let value: String
    public let expiresAt: Date
    public let relayURL: URL?
    /// Parsed only from the pinned HTTPS response's Date header. Consumers use it to calibrate
    /// effect-time clock authority; absence stays fail closed.
    public let authenticatedServerDate: Date?

    public init(value: String, expiresAt: Date, relayURL: URL? = nil,
         authenticatedServerDate: Date? = nil) {
        self.value = value
        self.expiresAt = expiresAt
        self.relayURL = relayURL
        self.authenticatedServerDate = authenticatedServerDate
    }
}

public protocol CloudDeviceTokenProviding: Sendable {
    func fetchDeviceToken() async throws -> CloudDeviceToken
}

/// The concrete `POST /v1/tokens/device` client. Authentication is deliberately a closure:
/// device-code login owns that credential, while this component owns token lifetime and refresh.
public struct CloudAPIDeviceTokenProvider: CloudDeviceTokenProviding, Sendable {
    public typealias AuthorizationHeaderProvider = @Sendable () async throws -> String

    public let apiBaseURL: URL
    public let session: URLSession
    public let authorizationHeader: AuthorizationHeaderProvider

    public init(
        apiBaseURL: URL,
        session: URLSession = .shared,
        authorizationHeader: @escaping AuthorizationHeaderProvider
    ) {
        self.apiBaseURL = apiBaseURL
        self.session = session
        self.authorizationHeader = authorizationHeader
    }

    public func fetchDeviceToken() async throws -> CloudDeviceToken {
        let url = apiBaseURL.appendingPathComponent("v1/tokens/device")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue(try await authorizationHeader(), forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        var configuration = session.configuration
        configuration.timeoutIntervalForRequest = 15
        configuration.timeoutIntervalForResource = 15
        let (data, response) = try await CloudBoundedTokenHTTPClient(
            configuration: configuration, maximumBytes: 64 * 1024).fetch(request)
        guard let http = response as? HTTPURLResponse else {
            throw CloudTransportError.invalidTokenResponse
        }
        // 401 is "that credential is not one of ours" and 403 is `ApiError.forbidden("revoked")`.
        // Both are answers rather than outages, and the difference decides whether anything
        // above this should keep reconnecting.
        guard http.statusCode != 401, http.statusCode != 403 else {
            throw CloudTransportError.unauthorized
        }
        guard (200..<300).contains(http.statusCode) else {
            throw CloudTransportError.invalidTokenResponse
        }
        let wire = try JSONDecoder().decode(DeviceTokenResponse.self, from: data)
        guard wire.tokenType == "Bearer", !wire.token.isEmpty,
              wire.token.utf8.count <= 16 * 1024,
              wire.relayURL.utf8.count <= 2_048,
              let relayURL = URL(string: wire.relayURL),
              relayURL.scheme?.lowercased() == "wss", relayURL.host?.isEmpty == false,
              relayURL.user == nil, relayURL.password == nil, relayURL.fragment == nil else {
            throw CloudTransportError.invalidTokenResponse
        }
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let expiresAt = formatter.date(from: wire.expiresAt)
            ?? ISO8601DateFormatter().date(from: wire.expiresAt)
        guard let expiresAt else { throw CloudTransportError.invalidTokenResponse }
        return CloudDeviceToken(
            value: wire.token,
            expiresAt: expiresAt,
            relayURL: relayURL,
            authenticatedServerDate: Self.parseHTTPDate(
                http.value(forHTTPHeaderField: "Date"))
        )
    }

    private static func parseHTTPDate(_ value: String?) -> Date? {
        guard let value else { return nil }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        formatter.dateFormat = "EEE',' dd MMM yyyy HH':'mm':'ss z"
        return formatter.date(from: value)
    }

    private struct DeviceTokenResponse: Decodable {
        let token: String
        let tokenType: String
        let expiresAt: String
        let relayURL: String

        enum CodingKeys: String, CodingKey {
            case token
            case tokenType = "token_type"
            case expiresAt = "expires_at"
            case relayURL = "relay_url"
        }
    }
}

struct CloudBoundedHTTPAccumulator {
    let maximumBytes: Int
    private(set) var response: URLResponse?
    private(set) var bytes = Data()

    mutating func accept(_ response: URLResponse) -> Bool {
        let length = response.expectedContentLength
        guard length < 0 || length <= Int64(maximumBytes) else { return false }
        self.response = response
        return true
    }

    mutating func append(_ data: Data) -> Bool {
        guard data.count <= maximumBytes, bytes.count <= maximumBytes - data.count else {
            return false
        }
        bytes.append(data)
        return true
    }
}

private final class CloudBoundedTokenHTTPClient: NSObject, URLSessionDataDelegate,
    @unchecked Sendable {
    private let configuration: URLSessionConfiguration
    private let lock = NSLock()
    private var continuation: CheckedContinuation<(Data, URLResponse), Error>?
    private var accumulator: CloudBoundedHTTPAccumulator
    private var task: URLSessionDataTask?
    private var ownedSession: URLSession?
    private var finished = false

    init(configuration: URLSessionConfiguration, maximumBytes: Int) {
        self.configuration = configuration
        accumulator = CloudBoundedHTTPAccumulator(maximumBytes: maximumBytes)
    }

    func fetch(_ request: URLRequest) async throws -> (Data, URLResponse) {
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                lock.lock()
                if finished || Task.isCancelled {
                    lock.unlock()
                    continuation.resume(throwing: CancellationError())
                    return
                }
                self.continuation = continuation
                let session = URLSession(configuration: configuration, delegate: self,
                                         delegateQueue: nil)
                let task = session.dataTask(with: request)
                ownedSession = session
                self.task = task
                lock.unlock()
                task.resume()
            }
        } onCancel: { [weak self] in self?.cancel() }
    }

    func urlSession(
        _ session: URLSession, dataTask: URLSessionDataTask,
        didReceive response: URLResponse,
        completionHandler: @escaping (URLSession.ResponseDisposition) -> Void
    ) {
        lock.lock(); let accepted = accumulator.accept(response); lock.unlock()
        guard accepted else {
            completionHandler(.cancel)
            finish(.failure(CloudTransportError.invalidTokenResponse))
            return
        }
        completionHandler(.allow)
    }

    func urlSession(_ session: URLSession, dataTask: URLSessionDataTask,
                    didReceive data: Data) {
        lock.lock()
        guard !finished, accumulator.append(data) else {
            lock.unlock()
            dataTask.cancel()
            finish(.failure(CloudTransportError.invalidTokenResponse))
            return
        }
        lock.unlock()
    }

    func urlSession(_ session: URLSession, task: URLSessionTask,
                    didCompleteWithError error: Error?) {
        if let error { finish(.failure(error)); return }
        lock.lock()
        let response = accumulator.response
        let bytes = accumulator.bytes
        lock.unlock()
        guard let response else {
            finish(.failure(CloudTransportError.invalidTokenResponse)); return
        }
        finish(.success((bytes, response)))
    }

    private func cancel() {
        lock.lock(); let task = task; lock.unlock()
        task?.cancel()
        finish(.failure(CancellationError()))
    }

    private func finish(_ result: Result<(Data, URLResponse), Error>) {
        lock.lock()
        guard !finished else { lock.unlock(); return }
        finished = true
        let continuation = continuation
        let session = ownedSession
        self.continuation = nil
        self.task = nil
        ownedSession = nil
        lock.unlock()
        continuation?.resume(with: result)
        session?.invalidateAndCancel()
    }
}

/// The immutable identity a socket challenge and every reconnect must prove before a signing key
/// is released. Key/revocation epochs are supplied by the protected identity authority; sequence
/// ownership deliberately remains in the W5-1 ledger/spool.
public struct CloudExecutorTransportBinding: Equatable, Sendable {
    public let accountID: String
    public let machineID: String
    public let deviceID: String
    public let signingKeyFingerprint: String
    public let identityGeneration: UInt64
    public let keyEpoch: UInt64
    public let revocationEpoch: UInt64

    public init(accountID: String, machineID: String, deviceID: String,
                signingKeyFingerprint: String, identityGeneration: UInt64,
                keyEpoch: UInt64, revocationEpoch: UInt64) {
        self.accountID = accountID
        self.machineID = machineID
        self.deviceID = deviceID
        self.signingKeyFingerprint = signingKeyFingerprint
        self.identityGeneration = identityGeneration
        self.keyEpoch = keyEpoch
        self.revocationEpoch = revocationEpoch
    }
}

public struct CloudExecutorReconnectProof: Equatable, Sendable {
    public let accountID: String
    public let machineID: String
    public let deviceID: String
    public let identityGeneration: UInt64
    public let keyEpoch: UInt64
    public let revocationEpoch: UInt64
    public let durableLedgerOpened: Bool
    public let durableSpoolOpened: Bool

    public init(accountID: String, machineID: String, deviceID: String,
                identityGeneration: UInt64, keyEpoch: UInt64,
                revocationEpoch: UInt64, durableLedgerOpened: Bool,
                durableSpoolOpened: Bool) {
        self.accountID = accountID
        self.machineID = machineID
        self.deviceID = deviceID
        self.identityGeneration = identityGeneration
        self.keyEpoch = keyEpoch
        self.revocationEpoch = revocationEpoch
        self.durableLedgerOpened = durableLedgerOpened
        self.durableSpoolOpened = durableSpoolOpened
    }
}

public enum CloudExecutorReconnectDisposition: Equatable, Sendable {
    case resumeFromDurableLedgerAndSpool
}
#endif

// URLSession WebSocket, authenticated framing and bounded ingress are host-neutral Foundation
// runtime. SwiftPM owns them in ClawdlineApplication so Mac and Linux compose the same transport;
// the flat compatibility suite still compiles these exact source bytes directly.
#if !SWIFT_PACKAGE || CLAWDLINE_APPLICATION_TARGET

/// The Mac's Cloud layers, as named in design `cloud-error-transparency.md` §1. A closed set: a
/// refusal names exactly one of these, and a viewer decides nothing from anything else.
public enum CloudRefusalLayer: String, CaseIterable, Sendable {
    case macTransport = "mac_transport"
    case macPreflight = "mac_preflight"
    case macLedger = "mac_ledger"
    case macRoute = "mac_route"
    case macReply = "mac_reply"
}

/// Why an inbound envelope was dropped before it became a command. `reason=invalid` used to cover
/// the first five of these, so a key mismatch and a malformed frame read the same in the log.
public enum CloudInboundDropCode: String, CaseIterable, Sendable {
    case envelopeMalformed = "envelope_malformed"
    case wrongChannel = "wrong_channel"
    case unknownSender = "unknown_sender"
    /// The roster could not be read, so no sender could be recognised. Today's Keychain failure
    /// used to become an empty roster and read exactly like an unpaired device.
    case rosterUnreadable = "roster_unreadable"
    case keyIDMismatch = "key_id_mismatch"
    /// This Mac could not read its own master secret, so no key id could be compared.
    case keyUnreadable = "key_unreadable"
    case badSignature = "bad_signature"
    case decryptFailed = "decrypt_failed"
    case replay
    /// The replay owner tracks a bounded number of senders and cannot decide for one more.
    case replayWindowFull = "replay_window_full"
}

/// One dropped envelope, carrying only what the envelope's outside already said: device id,
/// sequence and key ids. Nothing here came from ciphertext.
public struct CloudInboundDrop: Equatable, Sendable {
    public let code: CloudInboundDropCode
    public let sender: String?
    public let sequence: UInt64?
    public let keyID: String?
    public let expectedKeyID: String?
    public let highestSequence: UInt64?
    /// `nil` when the roster was not consulted before this drop was decided.
    public let rosterReadable: Bool?

    public init(code: CloudInboundDropCode, sender: String? = nil, sequence: UInt64? = nil,
                keyID: String? = nil, expectedKeyID: String? = nil,
                highestSequence: UInt64? = nil, rosterReadable: Bool? = nil) {
        self.code = code
        self.sender = sender
        self.sequence = sequence
        self.keyID = keyID
        self.expectedKeyID = expectedKeyID
        self.highestSequence = highestSequence
        self.rosterReadable = rosterReadable
    }
}

/// What the transport learned about its own connection, for a status reader. Emitted from actor
/// turns that already happen; the observer must record and return.
public enum CloudTransportConnectionEvent: Equatable, Sendable {
    case ready(generation: UInt64, tokenExpiresAtMilliseconds: UInt64, refreshAheadMilliseconds: UInt64)
    case reconnecting(reason: String)
    case stopped(reason: String)
    /// The roster as the last inbound envelope read it, reported only when it changed.
    case roster(readable: Bool, deviceIDs: [String])
}

/// A roster read that can say it failed. `pairedDevicePublicKeys()` answers an empty dictionary
/// for both "nobody is paired" and "the Keychain refused", and those two need different words.
public enum CloudPairedDeviceRosterReading: Equatable, Sendable {
    case readable([String: Data])
    case unreadable
}

/// The one spelling of a Cloud refusal in the Mac log (design §2.3):
///
/// `refusal layer=… code=… sender=… seq=… request=… type=… session=… status=… reply=… <extras>`
///
/// Callers prefix it with `cloud: `. A value that is absent is `-`; every value is reduced to a
/// safe token so a device-supplied string cannot forge a second field. `reply` is `published`,
/// `notice`, or `silent:<why>` — a `silent:` line is a path nothing answers yet.
public enum CloudRefusalLog {
    public static func line(
        layer: CloudRefusalLayer, code: String, sender: String?, sequence: UInt64?,
        request: String?, type: String?, session: String?, status: Int?, reply: String,
        extras: [(String, String?)] = []
    ) -> String {
        var fields: [(String, String?)] = [
            ("layer", layer.rawValue), ("code", code), ("sender", sender),
            ("seq", sequence.map(String.init)), ("request", request), ("type", type),
            ("session", session), ("status", status.map(String.init)), ("reply", reply),
        ]
        fields.append(contentsOf: extras)
        return "refusal " + fields.map { "\($0.0)=\(token($0.1))" }.joined(separator: " ")
    }

    public static func token(_ value: String?) -> String {
        guard let value, !value.isEmpty else { return "-" }
        var allowed = CharacterSet.alphanumerics
        allowed.insert(charactersIn: "-_.~:")
        let bounded = String(value.prefix(128))
        return bounded.addingPercentEncoding(withAllowedCharacters: allowed) ?? "-"
    }
}

public protocol CloudTransportKeyProviding: Sendable {
    func deviceKeyPair() async throws -> CloudDeviceKeyPair
    func masterSecret(for keyID: String) async throws -> CloudMasterSecret
    func pairedDevicePublicKeys() async -> [String: Data]
    /// `nil` is retained only for deterministic pre-W5 fixtures. Production returns the current
    /// protected binding on every initial connection and reconnect.
    func transportBinding() async throws -> CloudExecutorTransportBinding?
    /// Production re-reads the protected epoch tuple before every initial/reconnect signature.
    /// The lifecycle opens both durable owners before it permits transport construction.
    func admitReconnect(_ binding: CloudExecutorTransportBinding) async throws
    /// The roster, able to say it could not be read. Defaults to `pairedDevicePublicKeys()`.
    func pairedDeviceRoster() async -> CloudPairedDeviceRosterReading
    /// The key id this Mac currently opens envelopes with, when it can be read.
    func currentKeyID() async -> String?
}

extension CloudTransportKeyProviding {
    func transportBinding() async throws -> CloudExecutorTransportBinding? { nil }
    func admitReconnect(_: CloudExecutorTransportBinding) async throws {}
}

public extension CloudTransportKeyProviding {
    func pairedDeviceRoster() async -> CloudPairedDeviceRosterReading {
        .readable(await pairedDevicePublicKeys())
    }

    func currentKeyID() async -> String? { nil }
}

/// Production adapter shared by both hosts. Every method reads the protected authority again;
/// no socket generation retains a stale roster, key epoch or revocation epoch as current truth.
public struct CloudExecutorIdentityTransportKeys: CloudTransportKeyProviding, Sendable {
    public let authority: CloudExecutorIdentityAuthority

    public init(authority: CloudExecutorIdentityAuthority) { self.authority = authority }

    public func deviceKeyPair() async throws -> CloudDeviceKeyPair {
        try authority.transportMaterial().deviceKey
    }

    public func masterSecret(for keyID: String) async throws -> CloudMasterSecret {
        let material = try authority.transportMaterial()
        guard material.keyID == keyID else {
            throw CloudTransportError.unexpectedFrame("unknown-key")
        }
        return material.masterSecret
    }

    public func pairedDevicePublicKeys() async -> [String: Data] {
        (try? authority.transportMaterial().pairedDevicePublicKeys) ?? [:]
    }

    public func pairedDeviceRoster() async -> CloudPairedDeviceRosterReading {
        guard let material = try? authority.transportMaterial() else { return .unreadable }
        return .readable(material.pairedDevicePublicKeys)
    }

    public func currentKeyID() async -> String? {
        try? authority.transportMaterial().keyID
    }

    public func transportBinding() async throws -> CloudExecutorTransportBinding? {
        try authority.transportMaterial().binding
    }

    public func admitReconnect(_ binding: CloudExecutorTransportBinding) async throws {
        _ = try authority.verifyReconnect(CloudExecutorReconnectProof(
            accountID: binding.accountID, machineID: binding.machineID,
            deviceID: binding.deviceID, identityGeneration: binding.identityGeneration,
            keyEpoch: binding.keyEpoch, revocationEpoch: binding.revocationEpoch,
            durableLedgerOpened: true, durableSpoolOpened: true))
    }
}

struct CloudStaticTransportKeys: CloudTransportKeyProviding, Sendable {
    let deviceKey: CloudDeviceKeyPair
    let masterSecrets: [String: CloudMasterSecret]
    let pairedDevices: [String: Data]

    func deviceKeyPair() async throws -> CloudDeviceKeyPair { deviceKey }

    func masterSecret(for keyID: String) async throws -> CloudMasterSecret {
        guard let secret = masterSecrets[keyID] else {
            throw CloudTransportError.unexpectedFrame("unknown-key")
        }
        return secret
    }

    func pairedDevicePublicKeys() async -> [String: Data] { pairedDevices }

    /// The expected key id is only nameable when exactly one secret is held.
    func currentKeyID() async -> String? {
        masterSecrets.count == 1 ? masterSecrets.keys.first : nil
    }
}

public protocol CloudTransportClock: Sendable {
    func now() async -> Date
    func monotonicNow() async -> TimeInterval
    func waitUntilMonotonic(_ deadline: TimeInterval) async throws
    func sleep(for seconds: TimeInterval) async throws
    func jitterUnit() async -> Double
}

public extension CloudTransportClock {
    func monotonicNow() async -> TimeInterval { ProcessInfo.processInfo.systemUptime }
    func waitUntilMonotonic(_ deadline: TimeInterval) async throws {
        let remaining = deadline - ProcessInfo.processInfo.systemUptime
        if remaining <= 0 { return }
        try await Task.sleep(nanoseconds: UInt64(remaining * 1_000_000_000))
    }
}

struct CloudSystemTransportClock: CloudTransportClock, Sendable {
    func now() async -> Date { Date() }
    func monotonicNow() async -> TimeInterval { ProcessInfo.processInfo.systemUptime }

    func sleep(for seconds: TimeInterval) async throws {
        if seconds <= 0 { return }
        try await Task.sleep(nanoseconds: UInt64(seconds * 1_000_000_000))
    }

    func jitterUnit() async -> Double { Double.random(in: 0...1) }
}

public struct CloudInboundCommand: Equatable, Sendable {
    public let channel: String
    public let sequence: UInt64
    public let timestamp: UInt64
    public let commandClass: CloudEnvelopeClass
    public let sender: String
    public let plaintext: Data

    /// HTTP-equivalent idempotency identity for an admitted command. Capacity refusal is terminal
    /// for its sequence; a later request receives a new sequence and therefore a new identity.
    public var idempotencyKey: String { "cloud:\(sender):\(sequence)" }
    public var isDispatchCommand: Bool { commandClass == .dispatch }

    public init(channel: String, sequence: UInt64, timestamp: UInt64,
                sender: String, plaintext: Data, isDispatch: Bool = false) {
        self.channel = channel
        self.sequence = sequence
        self.timestamp = timestamp
        commandClass = isDispatch ? .dispatch : .ctl
        self.sender = sender
        self.plaintext = plaintext
    }

    init(channel: String, sequence: UInt64, timestamp: UInt64,
         commandClass: CloudEnvelopeClass, sender: String, plaintext: Data) {
        self.channel = channel
        self.sequence = sequence
        self.timestamp = timestamp
        self.commandClass = commandClass
        self.sender = sender
        self.plaintext = plaintext
    }

    /// Retained variable-width bytes charged to the ingress owner. Scalar fields have fixed
    /// storage; channel, sender and plaintext are the command's variable memory debt.
    var ingressChargedBytes: Int {
        let (names, namesOverflow) = channel.utf8.count.addingReportingOverflow(sender.utf8.count)
        let (total, totalOverflow) = names.addingReportingOverflow(plaintext.count)
        return namesOverflow || totalOverflow ? Int.max : total
    }
}

struct CloudInboundCommandQueueLimits: Equatable, Sendable {
    /// Reuses the already approved application-wide terminal admission depth from Plan-v4.
    static let defaultMaximumCount = 8
    /// One 32 MiB retained-variable-width budget across the pending FIFO.
    static let defaultMaximumChargedBytes = 32 * 1024 * 1024
    /// Fail closed at the relay's existing per-envelope content ceiling after authenticated
    /// decrypt. The retained charge still includes this plaintext plus channel and sender bytes.
    static let defaultMaximumPlaintextBytes = 16 * 1024 * 1024

    let maximumCount: Int
    let maximumChargedBytes: Int
    let maximumPlaintextBytes: Int

    init(
        maximumCount: Int = Self.defaultMaximumCount,
        maximumChargedBytes: Int = Self.defaultMaximumChargedBytes,
        maximumPlaintextBytes: Int = Self.defaultMaximumPlaintextBytes
    ) {
        precondition(maximumCount > 0)
        precondition(maximumChargedBytes > 0)
        precondition(maximumPlaintextBytes > 0)
        self.maximumCount = maximumCount
        self.maximumChargedBytes = maximumChargedBytes
        self.maximumPlaintextBytes = maximumPlaintextBytes
    }
}

public enum CloudInboundAdmissionRefusalReason: String, CaseIterable, Error, Hashable, Sendable {
    case countCap = "count_cap"
    case chargedByteCap = "charged_byte_cap"
    case plaintextCap = "plaintext_cap"
    case finished = "finished"

    /// The code a viewer is told. A single plaintext over the ceiling will never fit, so it is a
    /// terminal `command_too_large` rather than a busy answer that invites the same retry.
    public var refusalCode: String {
        switch self {
        case .countCap, .chargedByteCap: return "cloud_ingress_busy"
        case .plaintextCap: return "command_too_large"
        case .finished: return "cloud_ingress_closed"
        }
    }

    public var refusalStatus: Int {
        switch self {
        case .countCap, .chargedByteCap: return 429
        case .plaintextCap: return 413
        case .finished: return 503
        }
    }
}

public struct CloudInboundCommandQueueMetrics: Equatable, Sendable {
    public let maximumCount: Int
    public let maximumChargedBytes: Int
    public let maximumPlaintextBytes: Int
    public let currentCount: Int
    public let currentChargedBytes: Int
    public let peakCount: Int
    public let peakChargedBytes: Int
    public let admittedTotal: UInt64
    public let deliveredTotal: UInt64
    public let droppedInvalidTotal: UInt64
    public let refusalTotals: [CloudInboundAdmissionRefusalReason: UInt64]
    public let refusedChargedBytes: UInt64
    public let oldestWaitMilliseconds: UInt64
}

public struct CloudInboundAdmissionRefusal: Sendable {
    public let command: CloudInboundCommand
    public let reason: CloudInboundAdmissionRefusalReason
    public let metrics: CloudInboundCommandQueueMetrics
}

/// A single-consumer command sequence backed by the explicit ingress owner below. This replaces
/// AsyncStream's opaque, formerly unbounded buffer so dequeue is observable and charged bytes can
/// be released at the exact handoff boundary.
public struct CloudInboundCommandStream: AsyncSequence, Sendable {
    public typealias Element = CloudInboundCommand

    public struct AsyncIterator: AsyncIteratorProtocol {
        fileprivate let owner: CloudInboundCommandQueue

        public mutating func next() async -> CloudInboundCommand? {
            await owner.next()
        }
    }

    fileprivate let owner: CloudInboundCommandQueue

    public func makeAsyncIterator() -> AsyncIterator { AsyncIterator(owner: owner) }
}

/// The one owner of Cloud command ingress count, charged bytes, peaks, drops and refusals.
/// Operations inspect at most `maximumCount` rows, so admission/dequeue work is bounded too.
final class CloudInboundCommandQueue: @unchecked Sendable {
    private struct Pending {
        let command: CloudInboundCommand
        let chargedBytes: Int
        let admittedAtMilliseconds: UInt64
    }

    private let lock = NSLock()
    private let limits: CloudInboundCommandQueueLimits
    private let nowMilliseconds: @Sendable () -> UInt64
    private var pending: [Pending] = []
    private var waiters: [UUID: CheckedContinuation<CloudInboundCommand?, Never>] = [:]
    private var waiterOrder: [UUID] = []
    private var cancelledWaiters: Set<UUID> = []
    private var finished = false
    private var currentChargedBytes = 0
    private var peakCount = 0
    private var peakChargedBytes = 0
    private var admittedTotal: UInt64 = 0
    private var deliveredTotal: UInt64 = 0
    private var droppedInvalidTotal: UInt64 = 0
    private var refusalTotals: [CloudInboundAdmissionRefusalReason: UInt64] = [:]
    private var refusedChargedBytes: UInt64 = 0

    init(
        limits: CloudInboundCommandQueueLimits = CloudInboundCommandQueueLimits(),
        nowMilliseconds: @escaping @Sendable () -> UInt64 = {
            UInt64(ProcessInfo.processInfo.systemUptime * 1_000)
        }
    ) {
        self.limits = limits
        self.nowMilliseconds = nowMilliseconds
    }

    var stream: CloudInboundCommandStream { CloudInboundCommandStream(owner: self) }

    func admit(_ command: CloudInboundCommand) -> Result<Void, CloudInboundAdmissionRefusalReason> {
        let chargedBytes = command.ingressChargedBytes
        var resumed: CheckedContinuation<CloudInboundCommand?, Never>?
        lock.lock()
        let refusal: CloudInboundAdmissionRefusalReason?
        if finished {
            refusal = .finished
        } else if command.plaintext.count > limits.maximumPlaintextBytes {
            refusal = .plaintextCap
        } else if pending.count >= limits.maximumCount {
            refusal = .countCap
        } else {
            let (candidateBytes, overflow) = currentChargedBytes.addingReportingOverflow(chargedBytes)
            refusal = overflow || candidateBytes > limits.maximumChargedBytes
                ? .chargedByteCap : nil
        }
        if let refusal {
            refusalTotals[refusal] = Self.addingClamped(refusalTotals[refusal] ?? 0, 1)
            refusedChargedBytes = Self.addingClamped(
                refusedChargedBytes, UInt64(clamping: chargedBytes)
            )
            lock.unlock()
            return .failure(refusal)
        }

        admittedTotal = Self.addingClamped(admittedTotal, 1)
        peakCount = max(peakCount, pending.count + 1)
        let candidatePeakBytes = currentChargedBytes.addingReportingOverflow(chargedBytes)
        if !candidatePeakBytes.overflow {
            peakChargedBytes = max(peakChargedBytes, candidatePeakBytes.partialValue)
        }
        while let id = waiterOrder.first {
            waiterOrder.removeFirst()
            if let continuation = waiters.removeValue(forKey: id) {
                deliveredTotal = Self.addingClamped(deliveredTotal, 1)
                resumed = continuation
                break
            }
        }
        if resumed == nil {
            pending.append(Pending(
                command: command, chargedBytes: chargedBytes,
                admittedAtMilliseconds: nowMilliseconds()
            ))
            currentChargedBytes += chargedBytes
        }
        lock.unlock()
        resumed?.resume(returning: command)
        return .success(())
    }

    func recordInvalidDrop() -> UInt64 {
        lock.lock()
        droppedInvalidTotal = Self.addingClamped(droppedInvalidTotal, 1)
        let total = droppedInvalidTotal
        lock.unlock()
        return total
    }

    func snapshot() -> CloudInboundCommandQueueMetrics {
        lock.lock()
        let now = nowMilliseconds()
        let oldest = pending.first.map {
            now >= $0.admittedAtMilliseconds ? now - $0.admittedAtMilliseconds : 0
        } ?? 0
        let result = CloudInboundCommandQueueMetrics(
            maximumCount: limits.maximumCount,
            maximumChargedBytes: limits.maximumChargedBytes,
            maximumPlaintextBytes: limits.maximumPlaintextBytes,
            currentCount: pending.count,
            currentChargedBytes: currentChargedBytes,
            peakCount: peakCount,
            peakChargedBytes: peakChargedBytes,
            admittedTotal: admittedTotal,
            deliveredTotal: deliveredTotal,
            droppedInvalidTotal: droppedInvalidTotal,
            refusalTotals: refusalTotals,
            refusedChargedBytes: refusedChargedBytes,
            oldestWaitMilliseconds: oldest
        )
        lock.unlock()
        return result
    }

    func finish() {
        lock.lock()
        finished = true
        let continuations = waiterOrder.compactMap { waiters.removeValue(forKey: $0) }
        waiterOrder.removeAll()
        cancelledWaiters.removeAll()
        lock.unlock()
        for continuation in continuations { continuation.resume(returning: nil) }
    }

    fileprivate func next() async -> CloudInboundCommand? {
        if Task.isCancelled { return nil }
        let id = UUID()
        return await withTaskCancellationHandler {
            await withCheckedContinuation { continuation in
                registerWaiter(id: id, continuation: continuation)
            }
        } onCancel: {
            self.cancelWaiter(id: id)
        }
    }

    private func registerWaiter(
        id: UUID, continuation: CheckedContinuation<CloudInboundCommand?, Never>
    ) {
        lock.lock()
        if cancelledWaiters.remove(id) != nil {
            lock.unlock()
            continuation.resume(returning: nil)
            return
        }
        if !pending.isEmpty {
            let item = pending.removeFirst()
            currentChargedBytes -= item.chargedBytes
            deliveredTotal = Self.addingClamped(deliveredTotal, 1)
            lock.unlock()
            continuation.resume(returning: item.command)
            return
        }
        if finished {
            lock.unlock()
            continuation.resume(returning: nil)
            return
        }
        // Production has one bridge consumer. Refuse a competing iterator instead of allowing an
        // internal misuse to turn continuation waiters into a second unbounded queue.
        if !waiterOrder.isEmpty {
            lock.unlock()
            continuation.resume(returning: nil)
            return
        }
        waiters[id] = continuation
        waiterOrder.append(id)
        lock.unlock()
    }

    private func cancelWaiter(id: UUID) {
        lock.lock()
        if let continuation = waiters.removeValue(forKey: id) {
            waiterOrder.removeAll { $0 == id }
            lock.unlock()
            continuation.resume(returning: nil)
        } else {
            cancelledWaiters.insert(id)
            lock.unlock()
        }
    }

    private static func addingClamped(_ value: UInt64, _ amount: UInt64) -> UInt64 {
        let (sum, overflow) = value.addingReportingOverflow(amount)
        return overflow ? .max : sum
    }
}

/// Narrow deterministic source for host-composition tests. It exposes the same bounded queue as
/// production without making its counters or mutation surface part of the runtime API.
public final class CloudInboundCommandSource: @unchecked Sendable {
    private let queue: CloudInboundCommandQueue
    public let stream: CloudInboundCommandStream

    public init() {
        let queue = CloudInboundCommandQueue()
        self.queue = queue
        stream = queue.stream
    }

    @discardableResult
    public func yield(_ command: CloudInboundCommand) -> Bool {
        if case .success = queue.admit(command) { return true }
        return false
    }

    public func finish() { queue.finish() }
}

public protocol CloudTransportSocket: AnyObject, Sendable {
    func send(text: String) async throws
    func receiveText() async throws -> String
    func close()
}

/// A connector can own a started URLSession task internally, but it only exports this value after
/// the HTTP upgrade has completed. Authentication is a later state, represented separately below.
public struct CloudEstablishedTransportSocket: Sendable {
    fileprivate let raw: any CloudTransportSocket

    public init(_ raw: any CloudTransportSocket) {
        self.raw = raw
    }

    fileprivate func send(text: String) async throws { try await raw.send(text: text) }
    fileprivate func receiveText() async throws -> String { try await raw.receiveText() }
    fileprivate func close() { raw.close() }
}

/// This value can only be created after challenge/hello/ready validation. Keeping it distinct from
/// an established socket prevents a resumed task or a completed HTTP upgrade from being stored as
/// the transport's authenticated connection.
private struct CloudAuthenticatedTransportSocket: Sendable {
    let established: CloudEstablishedTransportSocket

    func send(text: String) async throws { try await established.send(text: text) }
    func receiveText() async throws -> String { try await established.receiveText() }
    func close() { established.close() }
}

public protocol CloudTransportSocketConnecting: Sendable {
    /// Implementations may suspend while establishing a socket, but must terminate promptly when
    /// the calling task is cancelled. `CloudTransport` owns that task and cancels/joins it during
    /// shutdown, before any socket exists that could otherwise be closed.
    func connect(url: URL, bearerToken: String) async throws -> CloudEstablishedTransportSocket
    func connect(
        url: URL, bearerToken: String, openingTimeout: TimeInterval
    ) async throws -> CloudEstablishedTransportSocket
}

public extension CloudTransportSocketConnecting {
    func connect(
        url: URL, bearerToken: String, openingTimeout: TimeInterval
    ) async throws -> CloudEstablishedTransportSocket {
        try await connect(url: url, bearerToken: bearerToken)
    }
}

public protocol CloudStartedTransportSocket: CloudTransportSocket {
    func resume()
}

public protocol CloudURLSessionSocketStarting: Sendable {
    func start(
        request: URLRequest,
        configuration: URLSessionConfiguration,
        observer: CloudWebSocketOpenObserver
    ) -> any CloudStartedTransportSocket
}

struct CloudFoundationURLSessionSocketStarter: CloudURLSessionSocketStarting, Sendable {
    func start(
        request: URLRequest,
        configuration: URLSessionConfiguration,
        observer: CloudWebSocketOpenObserver
    ) -> any CloudStartedTransportSocket {
        let session = URLSession(
            configuration: configuration, delegate: observer, delegateQueue: nil
        )
        let task = session.webSocketTask(with: request)
        return CloudURLSessionSocket(session: session, task: task)
    }
}

struct CloudURLSessionSocketConnector: CloudTransportSocketConnecting, Sendable {
    let openingTimeout: TimeInterval
    let connectionLifetime: TimeInterval
    private let starter: any CloudURLSessionSocketStarting

    init(
        openingTimeout: TimeInterval = 15,
        connectionLifetime: TimeInterval = 7 * 24 * 60 * 60,
        starter: any CloudURLSessionSocketStarting = CloudFoundationURLSessionSocketStarter()
    ) {
        self.openingTimeout = max(0.01, openingTimeout)
        self.connectionLifetime = max(60, connectionLifetime)
        self.starter = starter
    }

    func connect(url: URL, bearerToken: String) async throws -> CloudEstablishedTransportSocket {
        try await connect(url: url, bearerToken: bearerToken, openingTimeout: openingTimeout)
    }

    func connect(
        url: URL, bearerToken: String, openingTimeout remainingTimeout: TimeInterval
    ) async throws -> CloudEstablishedTransportSocket {
        let openingTimeout = min(self.openingTimeout, remainingTimeout)
        guard openingTimeout > 0 else { throw CloudTransportError.connectionTimedOut }
        var request = URLRequest(url: url)
        request.setValue("Bearer \(bearerToken)", forHTTPHeaderField: "Authorization")
        let observer = CloudWebSocketOpenObserver()
        let configuration = URLSessionConfiguration.ephemeral
#if !canImport(FoundationNetworking)
        configuration.waitsForConnectivity = false
#endif
        configuration.timeoutIntervalForRequest = openingTimeout
        configuration.timeoutIntervalForResource = connectionLifetime
        let started = starter.start(
            request: request, configuration: configuration, observer: observer
        )
        started.resume()
        do {
            try await withTaskCancellationHandler {
                try await observer.waitUntilOpen(timeout: openingTimeout)
            } onCancel: {
                started.close()
                observer.cancel()
            }
            try Task.checkCancellation()
            return CloudEstablishedTransportSocket(started)
        } catch {
            if error is CancellationError { throw error }
            started.close()
            if let transportError = error as? CloudTransportError { throw transportError }
            throw CloudTransportError.connectionFailed(error.localizedDescription)
        }
    }
}

#if os(Linux)
func cloudNIOAddHTTPClientUpgradeHandlers(
    to channel: any Channel,
    upgrade: NIOHTTPClientUpgradeSendableConfiguration
) -> EventLoopFuture<Void> {
    channel.pipeline.addHTTPClientHandlers(
        leftOverBytesStrategy: .forwardBytes, withClientUpgrade: upgrade)
}

/// FoundationNetworking's WebSocket implementation on Swift 6.1.3/Noble returns
/// NSURLErrorUnsupportedURL before issuing an upgrade. Linux therefore uses the pinned NIO stack;
/// macOS continues to use URLSession and never links this implementation.
struct CloudNIOLinuxSocketConnector: CloudTransportSocketConnecting, Sendable {
    let openingTimeout: TimeInterval

    init(openingTimeout: TimeInterval = 15) {
        self.openingTimeout = max(0.01, openingTimeout)
    }

    func connect(url: URL, bearerToken: String) async throws -> CloudEstablishedTransportSocket {
        try await connect(url: url, bearerToken: bearerToken, openingTimeout: openingTimeout)
    }

    func connect(
        url: URL, bearerToken: String, openingTimeout remainingTimeout: TimeInterval
    ) async throws -> CloudEstablishedTransportSocket {
        let openingTimeout = min(self.openingTimeout, remainingTimeout)
        guard openingTimeout > 0 else { throw CloudTransportError.connectionTimedOut }
        guard url.scheme?.lowercased() == "wss", let host = url.host, !host.isEmpty else {
            throw CloudTransportError.invalidRelayURL
        }
        let port = url.port ?? 443
        let path = (url.path.isEmpty ? "/" : url.path)
            + (url.query.map { "?" + $0 } ?? "")
        let connection = CloudNIOConnectionBox()
        let promiseBox = CloudNIOPromiseBox()
        let group = MultiThreadedEventLoopGroup.singleton
        let timeout = group.next().scheduleTask(
            in: .milliseconds(Int64(openingTimeout * 1_000))) {
                connection.close()
                promiseBox.fail(CloudTransportError.connectionTimedOut)
            }
        let bootstrap = ClientBootstrap(group: group)
            .channelOption(ChannelOptions.socketOption(.so_reuseaddr), value: 1)
            .connectTimeout(.milliseconds(Int64(openingTimeout * 1_000)))
            .channelInitializer { channel in
                do {
                    var tls = TLSConfiguration.makeClientConfiguration()
                    tls.certificateVerification = .fullVerification
                    let context = try NIOSSLContext(configuration: tls)
                    let tlsHandler = try NIOSSLClientHandler(context: context, serverHostname: host)
                    let http = CloudNIOHTTPUpgradeHandler(
                        host: port == 443 ? host : "\(host):\(port)", path: path,
                        bearerToken: bearerToken, promise: promiseBox)
                    let upgrader = NIOWebSocketClientUpgrader(
                        maxFrameSize: 32 * 1024 * 1024,
                        upgradePipelineHandler: { channel, _ in
                            let pipe = CloudNIOTextPipe(channel: channel)
                            return channel.pipeline.addHandlers([
                                NIOWebSocketFrameAggregator(
                                    minNonFinalFragmentSize: 1,
                                    maxAccumulatedFrameCount: 1_024,
                                    maxAccumulatedFrameSize: 32 * 1024 * 1024),
                                CloudNIOFrameHandler(pipe: pipe)
                            ]).map {
                                if !promiseBox.succeed(CloudEstablishedTransportSocket(pipe)) {
                                    pipe.close()
                                }
                            }
                        })
                    let upgrade: NIOHTTPClientUpgradeSendableConfiguration = (
                        upgraders: [upgrader],
                        completionHandler: { context in
                            context.pipeline.syncOperations.removeHandler(http, promise: nil)
                        })
                    return channel.pipeline.addHandler(tlsHandler).flatMap {
                        cloudNIOAddHTTPClientUpgradeHandlers(to: channel, upgrade: upgrade)
                    }.flatMap {
                        channel.pipeline.addHandler(http)
                    }
                } catch {
                    return channel.eventLoop.makeFailedFuture(error)
                }
            }

        let connectFuture = bootstrap.connect(host: host, port: port)
        connectFuture.whenSuccess { channel in connection.set(channel) }
        connectFuture.whenFailure { error in promiseBox.fail(error) }
        do {
            return try await withTaskCancellationHandler {
                let result = try await promiseBox.value()
                timeout.cancel()
                return result
            } onCancel: {
                timeout.cancel()
                connection.close()
                promiseBox.fail(CancellationError())
            }
        } catch {
            timeout.cancel()
            connection.close()
            if error is CancellationError { throw error }
            if let error = error as? CloudTransportError { throw error }
            throw CloudTransportError.connectionFailed(String(describing: error))
        }
    }
}

final class CloudNIOConnectionBox: @unchecked Sendable {
    private let lock = NSLock()
    private var channel: Channel?
    private var closed = false
    func set(_ channel: Channel) {
        lock.lock()
        if closed { lock.unlock(); channel.close(promise: nil); return }
        self.channel = channel
        lock.unlock()
    }
    func close() {
        lock.lock(); closed = true; let channel = channel; self.channel = nil; lock.unlock()
        channel?.close(promise: nil)
    }
}

final class CloudNIOPromiseBox: @unchecked Sendable {
    private let lock = NSLock()
    private var result: Result<CloudEstablishedTransportSocket, Error>?
    private var continuation: CheckedContinuation<CloudEstablishedTransportSocket, Error>?

    func value() async throws -> CloudEstablishedTransportSocket {
        try await withCheckedThrowingContinuation { continuation in
            lock.lock()
            if let result { lock.unlock(); continuation.resume(with: result) }
            else { self.continuation = continuation; lock.unlock() }
        }
    }
    @discardableResult func succeed(_ socket: CloudEstablishedTransportSocket) -> Bool {
        finish(.success(socket))
    }
    @discardableResult func fail(_ error: Error) -> Bool { finish(.failure(error)) }
    private func finish(_ result: Result<CloudEstablishedTransportSocket, Error>) -> Bool {
        lock.lock()
        guard self.result == nil else { lock.unlock(); return false }
        self.result = result
        let continuation = continuation
        self.continuation = nil
        lock.unlock()
        continuation?.resume(with: result)
        return true
    }
}

final class CloudNIOHTTPUpgradeHandler: ChannelInboundHandler,
    RemovableChannelHandler, @unchecked Sendable {
    typealias InboundIn = HTTPClientResponsePart
    typealias OutboundOut = HTTPClientRequestPart
    private let host: String
    private let path: String
    private let bearerToken: String
    private let promise: CloudNIOPromiseBox

    init(host: String, path: String, bearerToken: String, promise: CloudNIOPromiseBox) {
        self.host = host; self.path = path; self.bearerToken = bearerToken; self.promise = promise
    }
    func channelActive(context: ChannelHandlerContext) {
        var headers = HTTPHeaders()
        headers.add(name: "Host", value: host)
        headers.add(name: "Authorization", value: "Bearer " + bearerToken)
        context.write(wrapOutboundOut(.head(HTTPRequestHead(
            version: .http1_1, method: .GET, uri: path, headers: headers))), promise: nil)
        context.writeAndFlush(wrapOutboundOut(.end(nil)), promise: nil)
    }
    func channelRead(context: ChannelHandlerContext, data: NIOAny) {
        if case .head(let head) = unwrapInboundIn(data), head.status.code != 101 {
            let code = Int(head.status.code)
            promise.fail(code == 401 || code == 403
                ? CloudTransportError.unauthorized
                : CloudTransportError.upgradeRefused(statusCode: code))
            context.close(promise: nil)
        }
    }
    func errorCaught(context: ChannelHandlerContext, error: Error) {
        promise.fail(error)
        context.close(promise: nil)
    }
    func channelInactive(context: ChannelHandlerContext) {
        promise.fail(CloudTransportError.connectionFailed("the WebSocket upgrade closed"))
        context.fireChannelInactive()
    }
}

private final class CloudNIOTextInbox: @unchecked Sendable {
    private struct BufferedText {
        let text: String
        let bytes: Int
    }

    private let maximumBufferedTexts: Int
    private let maximumBufferedBytes: Int
    private let lock = NSLock()
    private var buffered: [BufferedText] = []
    private var bufferedBytes = 0
    private var waiter: CheckedContinuation<String, Error>?
    private var terminalError: Error?
    private var isFinished = false

    init(maximumBufferedTexts: Int, maximumBufferedBytes: Int) {
        self.maximumBufferedTexts = max(1, maximumBufferedTexts)
        self.maximumBufferedBytes = max(1, maximumBufferedBytes)
    }

    /// Returns false only when accepting this text would exceed the bounded in-memory backlog.
    /// A waiting receiver takes the value directly and therefore consumes no buffer budget.
    func offer(_ text: String) -> Bool {
        let bytes = text.utf8.count
        var waiting: CheckedContinuation<String, Error>?
        lock.lock()
        guard !isFinished else { lock.unlock(); return false }
        if let waiter {
            waiting = waiter
            self.waiter = nil
        } else {
            guard buffered.count < maximumBufferedTexts,
                  bytes <= maximumBufferedBytes,
                  bufferedBytes <= maximumBufferedBytes - bytes else {
                lock.unlock()
                return false
            }
            buffered.append(BufferedText(text: text, bytes: bytes))
            bufferedBytes += bytes
        }
        lock.unlock()
        waiting?.resume(returning: text)
        return true
    }

    func finish(throwing error: Error? = nil) {
        var waiting: CheckedContinuation<String, Error>?
        lock.lock()
        guard !isFinished else { lock.unlock(); return }
        isFinished = true
        terminalError = error
        if buffered.isEmpty {
            waiting = waiter
            waiter = nil
        }
        lock.unlock()
        if let waiting {
            waiting.resume(throwing: error ?? CloudTransportError.connectionFailed(
                "the WebSocket closed"))
        }
    }

    func next() async throws -> String {
        try await withCheckedThrowingContinuation { continuation in
            var result: Result<String, Error>?
            lock.lock()
            if !buffered.isEmpty {
                let first = buffered.removeFirst()
                bufferedBytes -= first.bytes
                result = .success(first.text)
            } else if isFinished {
                result = .failure(terminalError ?? CloudTransportError.connectionFailed(
                    "the WebSocket closed"))
            } else if waiter != nil {
                result = .failure(CloudTransportError.connectionFailed(
                    "concurrent inbound receive is unsupported"))
            } else {
                waiter = continuation
            }
            lock.unlock()
            if let result { continuation.resume(with: result) }
        }
    }
}

final class CloudNIOTextPipe: CloudTransportSocket, @unchecked Sendable {
    let channel: Channel
    private let inbox: CloudNIOTextInbox
    private let closeLock = NSLock()
    private var didBeginClose = false
    init(
        channel: Channel,
        maximumBufferedTexts: Int = 64,
        maximumBufferedBytes: Int = 32 * 1024 * 1024
    ) {
        self.channel = channel
        inbox = CloudNIOTextInbox(
            maximumBufferedTexts: maximumBufferedTexts,
            maximumBufferedBytes: maximumBufferedBytes)
    }
    func send(text: String) async throws {
        let channel = self.channel
        try await channel.eventLoop.submit {
            var bytes = channel.allocator.buffer(capacity: text.utf8.count)
            bytes.writeString(text)
            return channel.writeAndFlush(WebSocketFrame(fin: true, opcode: .text, data: bytes))
        }.flatMap { $0 }.get()
    }
    func receiveText() async throws -> String { try await inbox.next() }
    func offer(_ text: String) -> Bool { inbox.offer(text) }
    func fail(_ error: Error) { inbox.finish(throwing: error) }
    func finishPeerClose() {
        closeLock.lock(); didBeginClose = true; closeLock.unlock()
        inbox.finish()
    }
    func close() {
        closeLock.lock()
        guard !didBeginClose else { closeLock.unlock(); return }
        didBeginClose = true
        closeLock.unlock()
        inbox.finish()
        let channel = self.channel
        channel.eventLoop.execute {
            let empty = channel.allocator.buffer(capacity: 0)
            channel.writeAndFlush(WebSocketFrame(
                fin: true, opcode: .connectionClose, data: empty)).whenComplete { _ in
                    channel.close(promise: nil)
                }
            channel.eventLoop.scheduleTask(in: .seconds(1)) {
                if channel.isActive { channel.close(promise: nil) }
            }
        }
    }
}

final class CloudNIOFrameHandler: ChannelInboundHandler, @unchecked Sendable {
    typealias InboundIn = WebSocketFrame
    private let pipe: CloudNIOTextPipe
    init(pipe: CloudNIOTextPipe) { self.pipe = pipe }
    func channelRead(context: ChannelHandlerContext, data: NIOAny) {
        var frame = unwrapInboundIn(data)
        switch frame.opcode {
        case .text:
            var bytes = frame.unmaskedData
            guard let text = bytes.readString(length: bytes.readableBytes) else {
                pipe.fail(CloudTransportError.unexpectedFrame("text"))
                context.close(promise: nil); return
            }
            if !pipe.offer(text) {
                pipe.fail(CloudTransportError.connectionFailed("the inbound text buffer overflowed"))
                context.close(promise: nil)
            }
        case .ping:
            context.writeAndFlush(NIOAny(WebSocketFrame(
                fin: true, opcode: .pong, data: frame.unmaskedData)), promise: nil)
        case .pong:
            break
        case .connectionClose:
            pipe.finishPeerClose()
            let flushed = context.eventLoop.makePromise(of: Void.self)
            flushed.futureResult.whenComplete { _ in context.close(promise: nil) }
            context.writeAndFlush(NIOAny(WebSocketFrame(
                fin: true, opcode: .connectionClose, data: frame.unmaskedData)), promise: flushed)
        default:
            pipe.fail(CloudTransportError.unexpectedFrame(String(describing: frame.opcode)))
            context.close(promise: nil)
        }
    }
    func errorCaught(context: ChannelHandlerContext, error: Error) {
        pipe.fail(error)
        context.close(promise: nil)
    }
    func channelInactive(context: ChannelHandlerContext) { pipe.finishPeerClose() }
}
#endif

/// URLSession's WebSocket task is only *started* after `resume()`. The delegate callback is the
/// first evidence that the HTTP upgrade completed, and task completion is where a 401/403 lives.
/// This one-shot observer converts those callbacks into a bounded async result.
public final class CloudWebSocketOpenObserver: NSObject, URLSessionWebSocketDelegate, @unchecked Sendable {
    private let lock = NSLock()
    private var outcome: Result<Void, Error>?
    private var continuation: CheckedContinuation<Void, Error>?

    func waitUntilOpen(timeout: TimeInterval) async throws {
        try await withThrowingTaskGroup(of: Void.self) { group in
            group.addTask { [self] in try await waitForDelegate() }
            group.addTask {
                try await Task.sleep(nanoseconds: UInt64(max(0, timeout) * 1_000_000_000))
                throw CloudTransportError.connectionTimedOut
            }
            do {
                _ = try await group.next()
                group.cancelAll()
            } catch {
                group.cancelAll()
                throw error
            }
        }
    }

    public func opened() { finish(.success(())) }

    public func failed(statusCode: Int?, error: Error?) {
        if statusCode == 401 || statusCode == 403 {
            finish(.failure(CloudTransportError.unauthorized))
        } else if let statusCode {
            finish(.failure(CloudTransportError.upgradeRefused(statusCode: statusCode)))
        } else {
            finish(.failure(CloudTransportError.connectionFailed(
                error?.localizedDescription ?? "the WebSocket task ended before opening"
            )))
        }
    }

    public func cancel() { finish(.failure(CancellationError())) }

    public func urlSession(
        _ session: URLSession, webSocketTask: URLSessionWebSocketTask,
        didOpenWithProtocol protocol: String?
    ) {
        opened()
    }

    public func urlSession(
        _ session: URLSession, webSocketTask: URLSessionWebSocketTask,
        didCloseWith closeCode: URLSessionWebSocketTask.CloseCode, reason: Data?
    ) {
        finish(.failure(CloudTransportError.connectionFailed(
            "the WebSocket closed before opening (code \(closeCode.rawValue))"
        )))
    }

    public func urlSession(
        _ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?
    ) {
        failed(statusCode: (task.response as? HTTPURLResponse)?.statusCode, error: error)
    }

    private func waitForDelegate() async throws {
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { next in
                lock.lock()
                if let outcome {
                    lock.unlock()
                    next.resume(with: outcome)
                } else {
                    continuation = next
                    lock.unlock()
                }
            }
        } onCancel: {
            cancel()
        }
    }

    private func finish(_ result: Result<Void, Error>) {
        lock.lock()
        guard outcome == nil else {
            lock.unlock()
            return
        }
        outcome = result
        let next = continuation
        continuation = nil
        lock.unlock()
        next?.resume(with: result)
    }
}

final class CloudURLSessionSocket: CloudStartedTransportSocket, @unchecked Sendable {
    /// Foundation otherwise stops buffering at 1 MiB. A relay envelope may carry 16 MiB of
    /// ciphertext, whose base64 and JSON wrapper make the WebSocket text frame about 22.4 MiB.
    /// Keep this at the relay platform's 32 MiB frame ceiling so the Mac can receive everything
    /// the relay is allowed to forward, including longer voice recordings.
    private static let maximumMessageSize = 32 * 1024 * 1024

    private let resumeTask: @Sendable () -> Void
    private let sendText: @Sendable (String) async throws -> Void
    private let receiveTextFrame: @Sendable () async throws -> String
    private let cancelTask: @Sendable () -> Void
    private let invalidateSession: @Sendable () -> Void

    init(session: URLSession, task: URLSessionWebSocketTask) {
        task.maximumMessageSize = Self.maximumMessageSize
        resumeTask = { task.resume() }
        sendText = { text in try await task.send(.string(text)) }
        receiveTextFrame = {
            switch try await task.receive() {
            case .string(let text):
                return text
            case .data:
                throw CloudTransportError.unexpectedFrame("binary")
            @unknown default:
                throw CloudTransportError.unexpectedFrame("unknown")
            }
        }
        cancelTask = { task.cancel(with: .goingAway, reason: nil) }
        invalidateSession = { session.invalidateAndCancel() }
    }

    init(
        resume: @escaping @Sendable () -> Void,
        send: @escaping @Sendable (String) async throws -> Void,
        receive: @escaping @Sendable () async throws -> String,
        cancel: @escaping @Sendable () -> Void,
        invalidate: @escaping @Sendable () -> Void
    ) {
        resumeTask = resume
        sendText = send
        receiveTextFrame = receive
        cancelTask = cancel
        invalidateSession = invalidate
    }

    func resume() { resumeTask() }

    func send(text: String) async throws {
        try await sendText(text)
    }

    func receiveText() async throws -> String {
        try await receiveTextFrame()
    }

    func close() {
        cancelTask()
        invalidateSession()
    }
}

/// The machine-side cloud link. It has no app dependencies: credentials, keys, clock, socket,
/// and logging are all injected. Failed inbound envelopes never expose ciphertext or plaintext.
public actor CloudTransport {
    public typealias Logger = @Sendable (String) -> Void
    /// Called inline from the sole receive loop, so implementations must only offer the refusal to
    /// a bounded lane and return. Publication and any other suspension belong to that lane.
    public typealias InboundRefusalHandler = @Sendable (CloudInboundAdmissionRefusal) -> Void
    /// Called inline from the receive loop for every envelope dropped before admission. The
    /// owner records the drop and returns; with no owner the drop is logged `reply=silent:`.
    public typealias InboundDropHandler = @Sendable (CloudInboundDrop) -> Void
    /// Records a connection fact and returns; it runs on this actor.
    public typealias ConnectionObserver = @Sendable (CloudTransportConnectionEvent) -> Void
    /// A positive credential/revocation refusal is terminal until an explicit identity change.
    /// The callback must enqueue lifecycle work and return; it runs on the receive owner.
    public typealias TerminalAuthorizationHandler = @Sendable (CloudTransportError) -> Void

    public nonisolated let commands: CloudInboundCommandStream
    /// Correlated replies read from the authenticated socket. Pending ownership stays in the
    /// Application spool; transport publishes observations and retains no outbound queue.
    public nonisolated let outboundReceipts: AsyncStream<CloudOutboundTransportReceipt>
    /// Emits once after every successful initial or reconnect handshake. Snapshot owners use the
    /// monotonically increasing value to force a fresh full publication for the new relay state.
    /// The buffer coalesces to the newest value: an unconsumed older generation describes relay
    /// state that has already been superseded, while the current generation forces the same full
    /// snapshot refresh without allowing reconnect bursts to grow memory without bound.
    public nonisolated let readyGenerations: AsyncStream<UInt64>

    private let relayBaseURL: URL
    private let tokenProvider: any CloudDeviceTokenProviding
    private let keyProvider: any CloudTransportKeyProviding
    private let clock: any CloudTransportClock
    private let connector: any CloudTransportSocketConnecting
    private let logger: Logger
    private let refreshAhead: TimeInterval
    private let initialBackoff: TimeInterval
    private let maximumBackoff: TimeInterval
    private let backoffResetAfter: TimeInterval
    private let openingTimeout: TimeInterval
    private let authenticationTimeout: TimeInterval
    private let receiveTimeout: TimeInterval
    private let keepaliveInterval: TimeInterval
    private let commandQueue: CloudInboundCommandQueue
    private let readyContinuation: AsyncStream<UInt64>.Continuation
    private let outboundReceiptContinuation: AsyncStream<CloudOutboundTransportReceipt>.Continuation

    private var state: CloudTransportState = .idle
    private var role: CloudTransportRole?
    private var connectorTask: Task<CloudEstablishedTransportSocket, Error>?
    private var connectingSocket: CloudEstablishedTransportSocket?
    private var socket: CloudAuthenticatedTransportSocket?
    private var cachedToken: CloudDeviceToken?
    private var receiveTask: Task<Void, Never>?
    private var refreshTask: Task<Void, Never>?
    private var keepaliveTask: Task<Void, Never>?
    private var generation = 0
    private var droppedReadyGenerations = 0
    private let replayWindow: CloudInboundReplayWindow
    private var outboundReceiptEnqueued: UInt64 = 0
    private var outboundReceiptDropped: UInt64 = 0
    private var outboundReceiptTerminated: UInt64 = 0
    private var inboundRefusalHandler: InboundRefusalHandler?
    private var inboundDropHandler: InboundDropHandler?
    private var connectionObserver: ConnectionObserver?
    private var lastReportedRoster: (readable: Bool, deviceIDs: [String])?
    private var terminalAuthorizationHandler: TerminalAuthorizationHandler?
    /// The generation whose socket `refreshToken` closed on purpose. Read once, by the reconnect
    /// loop, so the retry it triggers is named for what caused it rather than for how it arrived.
    private var rotatingGeneration: Int?

    /// Host-neutral production constructor. Test clocks and socket connectors remain internal;
    /// public host compositions can only construct the real URLSession WebSocket transport.
    public static func production(
        relayBaseURL: URL,
        tokenProvider: any CloudDeviceTokenProviding,
        keyProvider: any CloudTransportKeyProviding,
        terminalAuthorizationHandler: TerminalAuthorizationHandler? = nil,
        logger: @escaping Logger = { _ in }
    ) -> CloudTransport {
#if os(Linux)
        let connector: any CloudTransportSocketConnecting = CloudNIOLinuxSocketConnector()
#else
        let connector: any CloudTransportSocketConnecting = CloudURLSessionSocketConnector()
#endif
        return CloudTransport(
            relayBaseURL: relayBaseURL, tokenProvider: tokenProvider,
            keyProvider: keyProvider, connector: connector,
            replayWindow: .process,
            terminalAuthorizationHandler: terminalAuthorizationHandler,
            logger: logger)
    }

    init(
        relayBaseURL: URL,
        tokenProvider: any CloudDeviceTokenProviding,
        keyProvider: any CloudTransportKeyProviding,
        clock: any CloudTransportClock = CloudSystemTransportClock(),
        connector: any CloudTransportSocketConnecting = CloudURLSessionSocketConnector(),
        refreshAhead: TimeInterval = 60,
        initialBackoff: TimeInterval = 0.25,
        maximumBackoff: TimeInterval = 30,
        backoffResetAfter: TimeInterval = 30,
        openingTimeout: TimeInterval = 15,
        authenticationTimeout: TimeInterval = 15,
        receiveTimeout: TimeInterval = 90,
        keepaliveInterval: TimeInterval = 30,
        inboundQueueLimits: CloudInboundCommandQueueLimits = CloudInboundCommandQueueLimits(),
        replayWindow: CloudInboundReplayWindow = CloudInboundReplayWindow(),
        terminalAuthorizationHandler: TerminalAuthorizationHandler? = nil,
        logger: @escaping Logger = { _ in }
    ) {
        self.relayBaseURL = relayBaseURL
        self.tokenProvider = tokenProvider
        self.keyProvider = keyProvider
        self.clock = clock
        self.connector = connector
        self.refreshAhead = max(0, refreshAhead)
        self.initialBackoff = max(0.01, initialBackoff)
        self.maximumBackoff = max(initialBackoff, maximumBackoff)
        self.backoffResetAfter = max(0.01, backoffResetAfter)
        self.openingTimeout = max(0.01, openingTimeout)
        self.authenticationTimeout = max(0.01, authenticationTimeout)
        self.receiveTimeout = max(0.01, receiveTimeout)
        self.keepaliveInterval = min(
            max(0.01, keepaliveInterval), max(0.01, receiveTimeout / 2))
        self.terminalAuthorizationHandler = terminalAuthorizationHandler
        self.logger = logger
        self.replayWindow = replayWindow
        let commandQueue = CloudInboundCommandQueue(limits: inboundQueueLimits)
        self.commandQueue = commandQueue
        commands = commandQueue.stream
        var readyContinuation: AsyncStream<UInt64>.Continuation!
        readyGenerations = AsyncStream(bufferingPolicy: .bufferingNewest(1)) {
            readyContinuation = $0
        }
        self.readyContinuation = readyContinuation
        var outboundReceiptContinuation: AsyncStream<CloudOutboundTransportReceipt>.Continuation!
        // Settlement observations are bounded independently of the socket receive loop. Keep the
        // oldest observations because they are most likely to release the global head; overflow is
        // explicitly counted and later head progress is still bounded by the spool attempt timer.
        outboundReceipts = AsyncStream(bufferingPolicy: .bufferingOldest(256)) {
            outboundReceiptContinuation = $0
        }
        self.outboundReceiptContinuation = outboundReceiptContinuation
    }

    public func connect(role: CloudTransportRole = .machine) async throws {
        guard state == .idle else { throw CloudTransportError.alreadyConnected }
        self.role = role
        state = .connecting
        do {
            let established = try await establish(role: role)
            receiveTask = Task { [weak self] in
                await self?.receiveAndReconnect(socket: established.socket, generation: established.generation)
            }
        } catch {
            if state != .shutDown {
                state = .idle
                self.role = nil
                logger("CloudTransport initial connection failed reason=\(failureCode(for: error))")
                if isTerminalAuthorizationFailure(error) {
                    terminalAuthorizationHandler?(normalizedTerminalAuthorizationFailure(error))
                }
            }
            throw error
        }
    }

    /// Compatibility for direct transport tests. Production hands the exact bytes sealed in the
    /// durable spool to `sendExactPublishFrame(_:)`.
    func publish(envelope: CloudEnvelope) async throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        try await sendExactPublishFrame(try encoder.encode(CloudPublishFrame(envelope: envelope)))
    }

    /// One exact-byte socket operation. A reconnecting transport refuses; it never keeps a
    /// second pending dictionary or chooses a sequence. The spool retains and retries the bytes.
    public func sendExactPublishFrame(_ bytes: Data) async throws {
        guard state != .idle, state != .shutDown else {
            throw CloudTransportError.notConnected
        }
        guard let socket else { throw CloudTransportError.notConnected }
        guard let text = String(data: bytes, encoding: .utf8), Data(text.utf8) == bytes else {
            throw CloudTransportError.unexpectedFrame("non-utf8-publish")
        }
        do {
            try await socket.send(text: text)
        } catch {
            socket.close()
            throw error
        }
    }

    public func currentState() -> CloudTransportState { state }
    func droppedInboundCount() -> Int {
        Int(clamping: commandQueue.snapshot().droppedInvalidTotal)
    }
    func droppedReadyGenerationCount() -> Int { droppedReadyGenerations }
    func inboundQueueMetrics() -> CloudInboundCommandQueueMetrics { commandQueue.snapshot() }

    public func setInboundRefusalHandler(_ handler: InboundRefusalHandler?) {
        inboundRefusalHandler = handler
    }

    /// `async` on purpose. `CloudTransporting` gives fixtures a no-op `async` default, and in an
    /// async context Swift prefers an `async` overload over a synchronous actor method, so a
    /// synchronous spelling here was silently passed over and no drop owner was ever installed.
    public func setInboundDropHandler(_ handler: InboundDropHandler?) async {
        inboundDropHandler = handler
    }

    public func setConnectionObserver(_ observer: ConnectionObserver?) async {
        connectionObserver = observer
        lastReportedRoster = nil
    }

    public func setTerminalAuthorizationHandler(_ handler: TerminalAuthorizationHandler?) {
        terminalAuthorizationHandler = handler
        logger("CloudTransport terminal authorization owner="
            + (handler == nil ? "cleared" : "installed"))
    }

    public func shutdown() async {
        guard state != .shutDown else { return }
        state = .shutDown
        refreshTask?.cancel()
        refreshTask = nil
        keepaliveTask?.cancel()
        keepaliveTask = nil
        receiveTask?.cancel()
        let task = receiveTask
        receiveTask = nil
        connectorTask?.cancel()
        let connector = connectorTask
        connectorTask = nil
        connectingSocket?.close()
        connectingSocket = nil
        socket?.close()
        socket = nil
        cachedToken = nil
        rotatingGeneration = nil
        connectionObserver?(.stopped(reason: "shutdown"))
        inboundRefusalHandler = nil
        inboundDropHandler = nil
        connectionObserver = nil
        terminalAuthorizationHandler = nil
        commandQueue.finish()
        readyContinuation.finish()
        outboundReceiptContinuation.finish()
        _ = await connector?.result
        if let task { await task.value }
    }

    private func establish(role: CloudTransportRole) async throws -> (socket: CloudAuthenticatedTransportSocket, generation: Int) {
        let deadline = await clock.monotonicNow() + openingTimeout
        let token = try await withinOpeningDeadline(deadline) { try await self.validToken() }
        // `validToken()` may suspend while another actor turn completes shutdown. From this check
        // through `connectorTask` registration there is no suspension, so a terminal transport
        // cannot create a connector after shutdown has already passed its cancel/join boundary.
        try Task.checkCancellation()
        guard state != .shutDown else { throw CancellationError() }
        let url = try connectURL(role: role)
        let connector = self.connector
        let connectorBudget = try await remainingOpeningBudget(deadline)
        let attempt = Task {
            try await connector.connect(
                url: url, bearerToken: token.value, openingTimeout: connectorBudget)
        }
        connectorTask = attempt
        let newSocket: CloudEstablishedTransportSocket
        do {
            newSocket = try await withTaskCancellationHandler {
                try await attempt.value
            } onCancel: {
                attempt.cancel()
            }
        } catch {
            connectorTask = nil
            if let transportError = error as? CloudTransportError,
               transportError == .unauthorized {
                cachedToken = nil
            }
            throw error
        }
        connectorTask = nil
        guard state != .shutDown, !Task.isCancelled else {
            newSocket.close()
            throw CancellationError()
        }
        connectingSocket = newSocket
        defer { connectingSocket = nil }

        do {
            let challengeText = try await boundedReceive(
                from: newSocket, timeout: authenticationTimeout,
                timeoutError: .challengeTimedOut
            )
            let challenge = try decodeChallenge(challengeText)
            let binding = try await keyProvider.transportBinding()
            let key = try await keyProvider.deviceKeyPair()
            if let binding {
                guard challenge.account == binding.accountID,
                      challenge.device == binding.deviceID,
                      binding.machineID == binding.deviceID,
                      binding.signingKeyFingerprint == key.pairingFingerprint else {
                    throw CloudTransportError.unexpectedFrame("identity-binding")
                }
                try await keyProvider.admitReconnect(binding)
            }
            let signed = [challenge.context, challenge.account, challenge.device, challenge.challenge]
                .joined(separator: "|")
            let signature = try key.signature(for: Data(signed.utf8)).base64EncodedString()
            try await newSocket.send(text: try encode(HelloFrame(sig: signature)))

            let readyText = try await boundedReceive(
                from: newSocket, timeout: authenticationTimeout,
                timeoutError: .readyTimedOut
            )
            let header = try JSONDecoder().decode(FrameHeader.self, from: Data(readyText.utf8))
            if header.type == "error" {
                let relayError = try JSONDecoder().decode(ErrorFrame.self, from: Data(readyText.utf8))
                throw CloudTransportError.relay(relayError.code, relayError.message)
            }
            guard header.type == "ready" else {
                throw CloudTransportError.unexpectedFrame(header.type)
            }
            let ready = try JSONDecoder().decode(ReadyFrame.self, from: Data(readyText.utf8))
            guard ready.v == 1, ready.role == role.rawValue,
                  ready.account == challenge.account, ready.device == challenge.device else {
                throw CloudTransportError.unexpectedFrame("ready")
            }
            guard state != .shutDown, !Task.isCancelled else {
                throw CancellationError()
            }

            generation += 1
            let currentGeneration = generation
            let authenticated = CloudAuthenticatedTransportSocket(established: newSocket)
            socket = authenticated
            state = .ready
            scheduleRefresh(token: token, generation: currentGeneration)
            scheduleKeepalive(generation: currentGeneration)
            connectionObserver?(.ready(
                generation: UInt64(currentGeneration),
                tokenExpiresAtMilliseconds: Self.milliseconds(token.expiresAt),
                refreshAheadMilliseconds: UInt64(refreshAhead * 1_000)))
            switch readyContinuation.yield(UInt64(currentGeneration)) {
            case .dropped:
                droppedReadyGenerations += 1
            case .enqueued, .terminated:
                break
            @unknown default:
                break
            }
            return (authenticated, currentGeneration)
        } catch {
            newSocket.close()
            throw error
        }
    }

    private func validToken() async throws -> CloudDeviceToken {
        let now = await clock.now()
        try Task.checkCancellation()
        guard state != .shutDown else { throw CancellationError() }
        if let cachedToken, cachedToken.expiresAt.timeIntervalSince(now) > refreshAhead {
            return cachedToken
        }
        let token = try await tokenProvider.fetchDeviceToken()
        try Task.checkCancellation()
        guard state != .shutDown else { throw CancellationError() }
        guard !token.value.isEmpty, token.expiresAt > now else {
            throw CloudTransportError.invalidTokenResponse
        }
        cachedToken = token
        return token
    }

    private func remainingOpeningBudget(_ deadline: TimeInterval) async throws -> TimeInterval {
        let remaining = deadline - (await clock.monotonicNow())
        guard remaining > 0 else { throw CloudTransportError.connectionTimedOut }
        return remaining
    }

    private func withinOpeningDeadline<T: Sendable>(
        _ deadline: TimeInterval,
        operation: @escaping @Sendable () async throws -> T
    ) async throws -> T {
        _ = try await remainingOpeningBudget(deadline)
        return try await withThrowingTaskGroup(of: T.self) { group in
            group.addTask { try await operation() }
            group.addTask { [clock] in
                try await clock.waitUntilMonotonic(deadline)
                throw CloudTransportError.connectionTimedOut
            }
            do {
                guard let value = try await group.next() else {
                    throw CloudTransportError.connectionTimedOut
                }
                group.cancelAll()
                return value
            } catch {
                group.cancelAll()
                throw error
            }
        }
    }

    private func connectURL(role: CloudTransportRole) throws -> URL {
        guard var components = URLComponents(url: relayBaseURL, resolvingAgainstBaseURL: false) else {
            throw CloudTransportError.invalidRelayURL
        }
        if components.path.isEmpty || components.path == "/" {
            components.path = "/v1/connect"
        }
        guard components.path == "/v1/connect" else {
            throw CloudTransportError.invalidRelayURL
        }
        components.queryItems = [URLQueryItem(name: "role", value: role.rawValue)]
        guard let url = components.url else { throw CloudTransportError.invalidRelayURL }
        return url
    }

    private func receiveAndReconnect(socket initialSocket: CloudAuthenticatedTransportSocket, generation initialGeneration: Int) async {
        var activeSocket = initialSocket
        var activeGeneration = initialGeneration
        var backoff = initialBackoff
        var connectedAt = await clock.now()

        while state != .shutDown, !Task.isCancelled {
            do {
                let text = try await boundedReceive(
                    from: activeSocket, timeout: receiveTimeout,
                    timeoutError: .receiveTimedOut
                )
                try await handle(text)
                continue
            } catch is CancellationError {
                return
            } catch {
                if state == .shutDown || Task.isCancelled { return }
                let tokenExpired = isAuthenticatedTokenExpiry(error)
                if !tokenExpired, isTerminalAuthorizationFailure(error) {
                    let terminalHandler = terminalAuthorizationHandler
                    activeSocket.close()
                    if generation == activeGeneration {
                        self.socket = nil
                        refreshTask?.cancel()
                        refreshTask = nil
                        keepaliveTask?.cancel()
                        keepaliveTask = nil
                    }
                    state = .idle
                    role = nil
                    let failure = normalizedTerminalAuthorizationFailure(error)
                    logger("CloudTransport terminal authorization refusal reason="
                        + "\(failureCode(for: failure)) owner="
                        + (terminalHandler == nil ? "missing" : "installed"))
                    connectionObserver?(.stopped(reason: failureCode(for: failure)))
                    terminalHandler?(failure)
                    return
                }
                if tokenExpired {
                    // This frame can only arrive after the signed challenge completed. In the
                    // Relay protocol an in-band `unauthorized` means that socket's device token
                    // expired; signature and credential refusals happen during `establish`, while
                    // revocation is `forbidden`/`revoked`. Discard the cached credential and let
                    // the ordinary reconnect path fetch a new one. Opening 401/403 and every
                    // revocation code remain terminal.
                    cachedToken = nil
                    rotatingGeneration = activeGeneration
                }
                if (await clock.now()).timeIntervalSince(connectedAt) >= backoffResetAfter {
                    backoff = initialBackoff
                }
                activeSocket.close()
                if generation == activeGeneration {
                    self.socket = nil
                    refreshTask?.cancel()
                    refreshTask = nil
                    keepaliveTask?.cancel()
                    keepaliveTask = nil
                    state = .reconnecting
                }
                var retryError = error
                // A socket this transport closed itself comes back as an ordinary receive error,
                // so the reason has to be carried rather than inferred from the error.
                if rotatingGeneration == activeGeneration {
                    rotatingGeneration = nil
                    retryError = CloudTransportError.tokenRotated
                }

                while state != .shutDown, !Task.isCancelled {
                    let jitter = 0.75 + (await clock.jitterUnit() * 0.5)
                    let delay = min(maximumBackoff, backoff) * jitter
                    logger("CloudTransport reconnect waiting reason=\(failureCode(for: retryError)) retry_in_ms=\(Int(delay * 1_000))")
                    connectionObserver?(.reconnecting(reason: failureCode(for: retryError)))
                    do {
                        try await clock.sleep(for: delay)
                        backoff = min(maximumBackoff, backoff * 2)
                        guard let role else { return }
                        let established = try await establish(role: role)
                        activeSocket = established.socket
                        activeGeneration = established.generation
                        connectedAt = await clock.now()
                        break
                    } catch is CancellationError {
                        return
                    } catch {
                        if state == .shutDown || Task.isCancelled { return }
                        if isTerminalAuthorizationFailure(error) {
                            let terminalHandler = terminalAuthorizationHandler
                            state = .idle
                            role = nil
                            let failure = normalizedTerminalAuthorizationFailure(error)
                            logger("CloudTransport terminal authorization refusal reason="
                                + "\(failureCode(for: failure)) owner="
                                + (terminalHandler == nil ? "missing" : "installed"))
                            connectionObserver?(.stopped(reason: failureCode(for: failure)))
                            terminalHandler?(failure)
                            return
                        }
                        state = .reconnecting
                        retryError = error
                    }
                }
            }
        }
    }

    private func boundedReceive(
        from socket: CloudEstablishedTransportSocket, timeout: TimeInterval,
        timeoutError: CloudTransportError
    ) async throws -> String {
        try await withThrowingTaskGroup(of: String.self) { group in
            group.addTask { try await socket.receiveText() }
            group.addTask {
                try await Task.sleep(nanoseconds: UInt64(timeout * 1_000_000_000))
                throw timeoutError
            }
            do {
                let text = try await group.next()!
                group.cancelAll()
                return text
            } catch {
                socket.close()
                group.cancelAll()
                throw normalizedConnectionError(error)
            }
        }
    }

    private func boundedReceive(
        from socket: CloudAuthenticatedTransportSocket, timeout: TimeInterval,
        timeoutError: CloudTransportError
    ) async throws -> String {
        try await boundedReceive(
            from: socket.established, timeout: timeout, timeoutError: timeoutError
        )
    }

    private func normalizedConnectionError(_ error: Error) -> Error {
        if error is CancellationError { return error }
        if let transportError = error as? CloudTransportError { return transportError }
        return CloudTransportError.connectionFailed(error.localizedDescription)
    }

    private func failureCode(for error: Error) -> String {
        guard let transportError = error as? CloudTransportError else {
            return "connection_failed"
        }
        switch transportError {
        case .unauthorized: return "unauthorized"
        case .upgradeRefused(let status): return "upgrade_refused_\(status)"
        case .connectionTimedOut: return "connection_timeout"
        case .challengeTimedOut: return "challenge_timeout"
        case .readyTimedOut: return "ready_timeout"
        case .authenticationTimedOut: return "authentication_timeout"
        case .receiveTimedOut: return "receive_timeout"
        case .connectionFailed: return "connection_failed"
        case .tokenRotated: return "token_rotation"
        case .alreadyConnected: return "already_connected"
        case .invalidRelayURL: return "invalid_relay_url"
        case .invalidTokenResponse: return "invalid_token_response"
        case .notConnected: return "not_connected"
        case .unexpectedFrame: return "unexpected_frame"
        case .relay(let code, _): return "relay_\(code)"
        }
    }

    private func isTerminalAuthorizationFailure(_ error: Error) -> Bool {
        guard let failure = error as? CloudTransportError else { return false }
        switch failure {
        case .unauthorized:
            return true
        case .upgradeRefused(let statusCode):
            return statusCode == 401 || statusCode == 403
        case .relay(let code, _):
            return Self.terminalAuthorizationCodes.contains(code.lowercased())
        default:
            return false
        }
    }

    private func isAuthenticatedTokenExpiry(_ error: Error) -> Bool {
        guard case .relay(let rawCode, _) = error as? CloudTransportError else {
            return false
        }
        let code = rawCode.lowercased()
        return code == "token_expired" || code == "unauthorized"
    }

    private func normalizedTerminalAuthorizationFailure(
        _ error: Error
    ) -> CloudTransportError {
        guard let failure = error as? CloudTransportError else { return .unauthorized }
        switch failure {
        case .upgradeRefused(let statusCode) where statusCode == 401 || statusCode == 403:
            return .unauthorized
        default:
            return failure
        }
    }

    private static let terminalAuthorizationCodes: Set<String> = [
        "unauthorized", "forbidden", "revoked", "device_revoked", "account_revoked",
    ]

    private func handle(_ text: String) async throws {
        let data = Data(text.utf8)
        guard let header = try? JSONDecoder().decode(FrameHeader.self, from: data) else {
            logger("CloudTransport ignored inbound frame reason=malformed_header")
            return
        }
        switch header.type {
        case "envelope":
            let frame: EnvelopeFrame
            do {
                frame = try JSONDecoder().decode(EnvelopeFrame.self, from: data)
            } catch {
                let outside = Self.outsideOfMalformedEnvelope(data)
                dropInbound(CloudInboundDrop(
                    code: .envelopeMalformed, sender: outside.sender,
                    sequence: outside.sequence, keyID: outside.keyID))
                return
            }
            await acceptInbound(frame.envelope)
        case "ack":
            guard let frame = try? JSONDecoder().decode(AckFrame.self, from: data),
                  frame.seq >= 0, frame.fanout >= 0, !frame.ch.isEmpty else {
                logger("CloudTransport ignored inbound frame reason=malformed_ack")
                return
            }
            let kind: CloudOutboundTransportReceiptKind
            switch frame.status {
            case "delivered": kind = .delivered
            case "viewer_offline": kind = .viewerOffline
            case "rejected", "refused":
                kind = .peerRejected(CloudOutboundPeerRejection(
                    code: .legacyRefused, field: nil, disposition: .terminal))
            default:
                logger("CloudTransport ignored inbound frame reason=unknown_ack_status")
                return
            }
            offerOutboundReceipt(CloudOutboundTransportReceipt(
                channel: frame.ch, sequence: frame.seq, kind: kind))
            return
        case "publish_error":
            guard let frame = try? JSONDecoder().decode(PublishErrorFrame.self, from: data),
                  frame.seq >= 0, !frame.ch.isEmpty else {
                logger("CloudTransport ignored inbound frame reason=malformed_publish_error")
                return
            }
            let code = Self.publishErrorCode(frame.code)
            offerOutboundReceipt(CloudOutboundTransportReceipt(
                channel: frame.ch, sequence: frame.seq,
                kind: .peerRejected(CloudOutboundPeerRejection(
                    code: code, field: Self.publishErrorField(frame.field),
                    disposition: Self.publishErrorDisposition(code)))))
            return
        case "subscriptions", "pong":
            return
        case "ping":
            guard let socket else { throw CloudTransportError.notConnected }
            try await socket.send(text: "{\"type\":\"pong\"}")
        case "error":
            guard let frame = try? JSONDecoder().decode(ErrorFrame.self, from: data) else {
                logger("CloudTransport ignored inbound frame reason=malformed_error")
                return
            }
            let failure = CloudTransportError.relay(frame.code, frame.message)
            if isAuthenticatedTokenExpiry(failure)
                || isTerminalAuthorizationFailure(failure) {
                throw failure
            }
            logger("CloudTransport uncorrelated relay error code="
                + "\(Self.publishErrorCode(frame.code).rawValue)")
        default:
            // The socket is already authenticated. A future or irrelevant nonfatal frame cannot
            // discard the very connection carrying correlated durable settlement.
            logger("CloudTransport ignored inbound frame reason=unsupported_type")
        }
    }

    private static func publishErrorCode(_ raw: String) -> CloudPublishErrorCode {
        CloudPublishErrorCode(rawValue: raw) ?? .unknown
    }

    private static func publishErrorField(_ raw: String?) -> CloudPublishErrorField? {
        guard let raw else { return nil }
        return CloudPublishErrorField(rawValue: raw) ?? .unknown
    }

    private static func publishErrorDisposition(
        _ code: CloudPublishErrorCode
    ) -> CloudPublishErrorDisposition {
        switch code {
        case .clockSkew, .unavailable, .internal: return .retryNewAttempt
        case .badRequest, .tooLarge, .forbidden, .rateLimited, .legacyRefused, .unknown:
            return .terminal
        }
    }

    /// Deterministic parser seam: it exercises the same authenticated-frame handling used by the
    /// receive loop without requiring a relay fake to grow a second raw-frame API.
    func handleAuthenticatedFrameForTesting(_ text: String) async throws {
        try await handle(text)
    }

    private func offerOutboundReceipt(_ receipt: CloudOutboundTransportReceipt) {
        let disposition = outboundReceiptContinuation.yield(receipt)
        switch disposition {
        case .enqueued:
            outboundReceiptEnqueued &+= 1
        case .dropped:
            outboundReceiptDropped &+= 1
            logger("CloudTransport outbound receipt dropped reason=bounded_oldest_full")
        case .terminated:
            outboundReceiptTerminated &+= 1
        @unknown default:
            outboundReceiptDropped &+= 1
        }
    }

    /// Deterministic test seam for the bounded owner used by the authenticated ACK parser above.
    /// It exposes no socket, persistence, or production admission path.
    func offerOutboundReceiptForTesting(_ receipt: CloudOutboundTransportReceipt) {
        offerOutboundReceipt(receipt)
    }

    public func outboundReceiptIngestionSnapshot() async -> CloudOutboundReceiptIngestionSnapshot {
        CloudOutboundReceiptIngestionSnapshot(
            enqueued: outboundReceiptEnqueued,
            dropped: outboundReceiptDropped,
            terminated: outboundReceiptTerminated)
    }

    /// Every exit from here is either an admission, a typed refusal handed to the refusal owner,
    /// or a typed drop. The order is the order of what each check needs: the outside of the
    /// envelope, the roster, this Mac's key, the signature and plaintext, and only then the replay
    /// window — so an unauthenticated envelope can never consume a sequence.
    private func acceptInbound(_ envelope: CloudEnvelope) async {
        let sender = envelope.sender
        let sequence = envelope.seq
        guard envelope.envelopeClass == .ctl || envelope.envelopeClass == .dispatch,
              envelope.ch.hasPrefix("ctl/") else {
            dropInbound(CloudInboundDrop(
                code: .wrongChannel, sender: sender, sequence: sequence))
            return
        }
        let paired: [String: Data]
        switch await keyProvider.pairedDeviceRoster() {
        case .readable(let keys):
            paired = keys
            reportRoster(readable: true, deviceIDs: keys.keys.sorted())
        case .unreadable:
            reportRoster(readable: false, deviceIDs: [])
            dropInbound(CloudInboundDrop(
                code: .rosterUnreadable, sender: sender, sequence: sequence, rosterReadable: false))
            return
        }
        guard paired[sender] != nil else {
            dropInbound(CloudInboundDrop(
                code: .unknownSender, sender: sender, sequence: sequence, rosterReadable: true))
            return
        }
        let secret: CloudMasterSecret
        do {
            secret = try await keyProvider.masterSecret(for: envelope.keyID)
        } catch {
            let expected = await keyProvider.currentKeyID()
            let unknownKey = (error as? CloudTransportError) == .unexpectedFrame("unknown-key")
            let mismatch = unknownKey || (expected != nil && expected != envelope.keyID)
            dropInbound(CloudInboundDrop(
                code: mismatch ? .keyIDMismatch : .keyUnreadable, sender: sender,
                sequence: sequence, keyID: envelope.keyID, expectedKeyID: expected))
            return
        }
        let plaintext: Data
        do {
            plaintext = try envelope.open(
                masterSecret: secret,
                publicKeyForSender: { paired[$0] }
            )
        } catch CloudEnvelopeError.unknownSender {
            dropInbound(CloudInboundDrop(
                code: .unknownSender, sender: sender, sequence: sequence, rosterReadable: true))
            return
        } catch CloudEnvelopeError.badSignature {
            dropInbound(CloudInboundDrop(code: .badSignature, sender: sender, sequence: sequence))
            return
        } catch {
            dropInbound(CloudInboundDrop(code: .decryptFailed, sender: sender, sequence: sequence))
            return
        }
        switch replayWindow.claim(sender: sender, sequence: sequence) {
        case .accepted:
            break
        case .replay(let highest):
            dropInbound(CloudInboundDrop(
                code: .replay, sender: sender, sequence: sequence, highestSequence: highest))
            return
        case .senderCapacity:
            dropInbound(CloudInboundDrop(
                code: .replayWindowFull, sender: sender, sequence: sequence))
            return
        }
        let command = CloudInboundCommand(
            channel: envelope.ch,
            sequence: sequence,
            timestamp: envelope.ts,
            commandClass: envelope.envelopeClass,
            sender: sender,
            plaintext: plaintext
        )
        // The claim above is final: a capacity refusal is terminal for this sequence, so the same
        // envelope sent again is a replay rather than a second chance to execute.
        if case .failure(let reason) = commandQueue.admit(command) {
            refuseInbound(command, reason: reason)
        }
    }

    private func dropInbound(_ drop: CloudInboundDrop) {
        _ = commandQueue.recordInvalidDrop()
        let reply = inboundDropHandler == nil ? "silent:no_status_owner" : "notice"
        logger(CloudRefusalLog.line(
            layer: .macTransport, code: drop.code.rawValue, sender: drop.sender,
            sequence: drop.sequence, request: nil, type: nil, session: nil, status: nil,
            reply: reply, extras: Self.logExtras(for: drop)))
        inboundDropHandler?(drop)
    }

    static func logExtras(for drop: CloudInboundDrop) -> [(String, String?)] {
        var extras: [(String, String?)] = []
        switch drop.code {
        case .keyIDMismatch, .keyUnreadable:
            extras.append(("key_id", drop.keyID))
            extras.append(("expected_key_id", drop.expectedKeyID))
        case .replay:
            extras.append(("highest_seq", drop.highestSequence.map(String.init)))
        default:
            break
        }
        if let readable = drop.rosterReadable {
            extras.append(("roster_readable", readable ? "true" : "false"))
        }
        return extras
    }

    private func refuseInbound(
        _ command: CloudInboundCommand, reason: CloudInboundAdmissionRefusalReason
    ) {
        let metrics = commandQueue.snapshot()
        guard let inboundRefusalHandler else {
            logger(CloudRefusalLog.line(
                layer: .macTransport, code: reason.refusalCode, sender: command.sender,
                sequence: command.sequence, request: nil, type: nil, session: nil,
                status: reason.refusalStatus,
                reply: reason == .finished ? "silent:shutdown" : "silent:no_reply_owner",
                extras: [("detail.reason", reason.rawValue)]))
            return
        }
        inboundRefusalHandler(CloudInboundAdmissionRefusal(
            command: command, reason: reason, metrics: metrics
        ))
    }

    private func reportRoster(readable: Bool, deviceIDs: [String]) {
        guard let connectionObserver else { return }
        if let last = lastReportedRoster, last.readable == readable, last.deviceIDs == deviceIDs {
            return
        }
        lastReportedRoster = (readable, deviceIDs)
        connectionObserver(.roster(readable: readable, deviceIDs: deviceIDs))
    }

    /// What a malformed frame's outside still says, kept only when each value has the shape the
    /// envelope contract allows. It names the drop; it is never used to route anything.
    static func outsideOfMalformedEnvelope(
        _ data: Data
    ) -> (sender: String?, sequence: UInt64?, keyID: String?) {
        guard let frame = try? JSONDecoder().decode(MalformedEnvelopeOutside.self, from: data)
        else { return (nil, nil, nil) }
        func token(_ value: String?, maximum: Int) -> String? {
            guard let value, !value.isEmpty, value.utf8.count <= maximum,
                  value.utf8.allSatisfy({ $0 >= 0x21 && $0 <= 0x7e && $0 != 0x2f && $0 != 0x7c })
            else { return nil }
            return value
        }
        let sequence = frame.envelope?.seq.flatMap {
            $0 <= CloudEnvelope.maximumRelayInteger ? $0 : nil
        }
        return (token(frame.envelope?.sender, maximum: 128), sequence,
                token(frame.envelope?.keyID, maximum: 64))
    }

    private struct MalformedEnvelopeOutside: Decodable {
        struct Outside: Decodable {
            let sender: String?
            let seq: UInt64?
            let keyID: String?

            enum CodingKeys: String, CodingKey {
                case sender, seq
                case keyID = "key_id"
            }

            init(from decoder: Decoder) throws {
                let values = try decoder.container(keyedBy: CodingKeys.self)
                sender = try? values.decodeIfPresent(String.self, forKey: .sender)
                seq = try? values.decodeIfPresent(UInt64.self, forKey: .seq)
                keyID = try? values.decodeIfPresent(String.self, forKey: .keyID)
            }
        }

        let envelope: Outside?

        enum CodingKeys: String, CodingKey { case envelope }

        init(from decoder: Decoder) throws {
            let values = try decoder.container(keyedBy: CodingKeys.self)
            envelope = try? values.decodeIfPresent(Outside.self, forKey: .envelope)
        }
    }

    static func milliseconds(_ date: Date) -> UInt64 {
        let value = date.timeIntervalSince1970 * 1_000
        guard value.isFinite, value > 0 else { return 0 }
        return value >= Double(UInt64.max) ? .max : UInt64(value)
    }

    private func scheduleRefresh(token: CloudDeviceToken, generation: Int) {
        refreshTask?.cancel()
        refreshTask = Task { [weak self, clock, refreshAhead] in
            guard let self else { return }
            let now = await clock.now()
            let delay = max(0, token.expiresAt.timeIntervalSince(now) - refreshAhead)
            do {
                try await clock.sleep(for: delay)
                await self.refreshToken(generation: generation)
            } catch {
                return
            }
        }
    }

    private func scheduleKeepalive(generation expectedGeneration: Int) {
        keepaliveTask?.cancel()
        let interval = keepaliveInterval
        keepaliveTask = Task { [weak self] in
            while !Task.isCancelled {
                do {
                    try await Task.sleep(
                        nanoseconds: UInt64(interval * 1_000_000_000))
                } catch {
                    return
                }
                guard let self, await self.sendKeepalive(generation: expectedGeneration) else {
                    return
                }
            }
        }
    }

    private func sendKeepalive(generation expectedGeneration: Int) async -> Bool {
        guard state == .ready, generation == expectedGeneration, let socket else {
            return false
        }
        do {
            try await socket.send(text: "{\"type\":\"ping\"}")
            return true
        } catch {
            logger("CloudTransport keepalive send failed reason=\(failureCode(for: error))")
            socket.close()
            return false
        }
    }

    private func refreshToken(generation expectedGeneration: Int) {
        guard state == .ready, generation == expectedGeneration else { return }
        cachedToken = nil
        rotatingGeneration = expectedGeneration
        keepaliveTask?.cancel()
        keepaliveTask = nil
        socket?.close()
    }

    private func decodeChallenge(_ text: String) throws -> ChallengeFrame {
        let data = Data(text.utf8)
        let header = try JSONDecoder().decode(FrameHeader.self, from: data)
        if header.type == "error" {
            let frame = try JSONDecoder().decode(ErrorFrame.self, from: data)
            throw CloudTransportError.relay(frame.code, frame.message)
        }
        guard header.type == "challenge" else {
            throw CloudTransportError.unexpectedFrame(header.type)
        }
        let frame = try JSONDecoder().decode(ChallengeFrame.self, from: data)
        guard frame.v == 1, frame.context == "clawdline-challenge-v1",
              !frame.account.isEmpty, !frame.device.isEmpty, frame.expiresInMS > 0,
              let challenge = Data(base64Encoded: frame.challenge), challenge.count == 32 else {
            throw CloudTransportError.unexpectedFrame("challenge")
        }
        return frame
    }

    private func encode<T: Encodable>(_ value: T) throws -> String {
        let data = try JSONEncoder().encode(value)
        guard let text = String(data: data, encoding: .utf8) else {
            throw CloudTransportError.unexpectedFrame("encoding")
        }
        return text
    }

    private struct FrameHeader: Decodable { let type: String }

    private struct ChallengeFrame: Decodable {
        let type: String
        let v: Int
        let context: String
        let account: String
        let device: String
        let challenge: String
        let expiresInMS: Int

        enum CodingKeys: String, CodingKey {
            case type, v, context, account, device, challenge
            case expiresInMS = "expires_in_ms"
        }
    }

    private struct HelloFrame: Encodable {
        let type = "hello"
        let sig: String
    }

    private struct ReadyFrame: Decodable {
        let type: String
        let v: Int
        let account: String
        let device: String
        let role: String
    }

    private struct EnvelopeFrame: Decodable {
        let type: String
        let envelope: CloudEnvelope
    }

    private struct AckFrame: Decodable {
        let type: String
        let ch: String
        let seq: Int64
        let fanout: Int
        let status: String
    }

    private struct ErrorFrame: Decodable {
        let type: String
        let code: String
        let message: String
    }

    private struct PublishErrorFrame: Decodable {
        let type: String
        let ch: String
        let seq: Int64
        let code: String
        let field: String?
    }
}
#endif
