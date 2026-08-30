import Foundation

enum CloudTransportRole: String, Sendable {
    case machine
}

enum CloudTransportState: Equatable, Sendable {
    case idle
    case connecting
    case ready
    case reconnecting
    case shutDown
}

enum CloudTransportError: Error, LocalizedError, Equatable {
    case alreadyConnected
    case invalidRelayURL
    case invalidTokenResponse
    /// The control plane refused this device's credential outright — 401, or the 403 a revoked
    /// machine or device gets from `POST /v1/tokens/device`. Separate from
    /// `invalidTokenResponse` because retrying it is pointless: the reconnect loop here treats
    /// every failure as transient, so somebody above has to be able to tell the two apart.
    case unauthorized
    case notConnected
    case unexpectedFrame(String)
    case relay(String, String)

    var errorDescription: String? {
        switch self {
        case .alreadyConnected:
            return "CloudTransport is already connected."
        case .invalidRelayURL:
            return "The cloud relay URL is invalid."
        case .invalidTokenResponse:
            return "The device-token response is invalid."
        case .unauthorized:
            return "Clawdline Cloud refused this device's credential."
        case .notConnected:
            return "CloudTransport is not connected."
        case .unexpectedFrame(let type):
            return "The relay sent an unexpected \(type) frame."
        case .relay(let code, let message):
            return "The relay refused the request (\(code)): \(message)"
        }
    }
}

struct CloudDeviceToken: Equatable, Sendable {
    let value: String
    let expiresAt: Date
    let relayURL: URL?

    init(value: String, expiresAt: Date, relayURL: URL? = nil) {
        self.value = value
        self.expiresAt = expiresAt
        self.relayURL = relayURL
    }
}

protocol CloudDeviceTokenProviding: Sendable {
    func fetchDeviceToken() async throws -> CloudDeviceToken
}

/// The concrete `POST /v1/tokens/device` client. Authentication is deliberately a closure:
/// device-code login owns that credential, while this component owns token lifetime and refresh.
struct CloudAPIDeviceTokenProvider: CloudDeviceTokenProviding, Sendable {
    typealias AuthorizationHeaderProvider = @Sendable () async throws -> String

    let apiBaseURL: URL
    let session: URLSession
    let authorizationHeader: AuthorizationHeaderProvider

    init(
        apiBaseURL: URL,
        session: URLSession = .shared,
        authorizationHeader: @escaping AuthorizationHeaderProvider
    ) {
        self.apiBaseURL = apiBaseURL
        self.session = session
        self.authorizationHeader = authorizationHeader
    }

    func fetchDeviceToken() async throws -> CloudDeviceToken {
        let url = apiBaseURL.appendingPathComponent("v1/tokens/device")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue(try await authorizationHeader(), forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        let (data, response) = try await session.data(for: request)
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
        guard wire.tokenType == "Bearer", !wire.token.isEmpty else {
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
            relayURL: URL(string: wire.relayURL)
        )
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

protocol CloudTransportKeyProviding: Sendable {
    func deviceKeyPair() async throws -> CloudDeviceKeyPair
    func masterSecret(for keyID: String) async throws -> CloudMasterSecret
    func pairedDevicePublicKeys() async -> [String: Data]
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
}

protocol CloudTransportClock: Sendable {
    func now() async -> Date
    func sleep(for seconds: TimeInterval) async throws
    func jitterUnit() async -> Double
}

struct CloudSystemTransportClock: CloudTransportClock, Sendable {
    func now() async -> Date { Date() }

    func sleep(for seconds: TimeInterval) async throws {
        if seconds <= 0 { return }
        try await Task.sleep(nanoseconds: UInt64(seconds * 1_000_000_000))
    }

    func jitterUnit() async -> Double { Double.random(in: 0...1) }
}

struct CloudInboundCommand: Equatable, Sendable {
    let channel: String
    let sequence: UInt64
    let timestamp: UInt64
    let commandClass: CloudEnvelopeClass
    let sender: String
    let plaintext: Data
}

protocol CloudTransportSocket: AnyObject, Sendable {
    func send(text: String) async throws
    func receiveText() async throws -> String
    func close()
}

protocol CloudTransportSocketConnecting: Sendable {
    /// Implementations may suspend while establishing a socket, but must terminate promptly when
    /// the calling task is cancelled. `CloudTransport` owns that task and cancels/joins it during
    /// shutdown, before any socket exists that could otherwise be closed.
    func connect(url: URL, bearerToken: String) async throws -> any CloudTransportSocket
}

struct CloudURLSessionSocketConnector: CloudTransportSocketConnecting, Sendable {
    let session: URLSession

    init(session: URLSession = .shared) {
        self.session = session
    }

    func connect(url: URL, bearerToken: String) async throws -> any CloudTransportSocket {
        var request = URLRequest(url: url)
        request.setValue("Bearer \(bearerToken)", forHTTPHeaderField: "Authorization")
        let task = session.webSocketTask(with: request)
        task.resume()
        return CloudURLSessionSocket(session: session, task: task)
    }
}

private final class CloudURLSessionSocket: CloudTransportSocket, @unchecked Sendable {
    private let session: URLSession
    private let task: URLSessionWebSocketTask

    init(session: URLSession, task: URLSessionWebSocketTask) {
        self.session = session
        self.task = task
    }

    func send(text: String) async throws {
        try await task.send(.string(text))
    }

    func receiveText() async throws -> String {
        switch try await task.receive() {
        case .string(let text):
            return text
        case .data:
            throw CloudTransportError.unexpectedFrame("binary")
        @unknown default:
            throw CloudTransportError.unexpectedFrame("unknown")
        }
    }

    func close() {
        task.cancel(with: .goingAway, reason: nil)
        _ = session
    }
}

/// The machine-side cloud link. It has no app dependencies: credentials, keys, clock, socket,
/// and logging are all injected. Failed inbound envelopes never expose ciphertext or plaintext.
actor CloudTransport {
    typealias Logger = @Sendable (String) -> Void

    nonisolated let commands: AsyncStream<CloudInboundCommand>
    /// Emits once after every successful initial or reconnect handshake. Snapshot owners use the
    /// monotonically increasing value to force a fresh full publication for the new relay state.
    /// The buffer coalesces to the newest value: an unconsumed older generation describes relay
    /// state that has already been superseded, while the current generation forces the same full
    /// snapshot refresh without allowing reconnect bursts to grow memory without bound.
    nonisolated let readyGenerations: AsyncStream<UInt64>

    private let relayBaseURL: URL
    private let tokenProvider: any CloudDeviceTokenProviding
    private let keyProvider: any CloudTransportKeyProviding
    private let clock: any CloudTransportClock
    private let connector: any CloudTransportSocketConnecting
    private let logger: Logger
    private let refreshAhead: TimeInterval
    private let initialBackoff: TimeInterval
    private let maximumBackoff: TimeInterval
    private let commandContinuation: AsyncStream<CloudInboundCommand>.Continuation
    private let readyContinuation: AsyncStream<UInt64>.Continuation

    private var state: CloudTransportState = .idle
    private var role: CloudTransportRole?
    private var connectorTask: Task<any CloudTransportSocket, Error>?
    private var connectingSocket: (any CloudTransportSocket)?
    private var socket: (any CloudTransportSocket)?
    private var cachedToken: CloudDeviceToken?
    private var receiveTask: Task<Void, Never>?
    private var refreshTask: Task<Void, Never>?
    private var generation = 0
    private var droppedInbound = 0
    private var droppedReadyGenerations = 0
    private var sequenceTracker = CloudSequenceTracker()
    private var pendingByChannel: [String: CloudEnvelope] = [:]

    init(
        relayBaseURL: URL,
        tokenProvider: any CloudDeviceTokenProviding,
        keyProvider: any CloudTransportKeyProviding,
        clock: any CloudTransportClock = CloudSystemTransportClock(),
        connector: any CloudTransportSocketConnecting = CloudURLSessionSocketConnector(),
        refreshAhead: TimeInterval = 60,
        initialBackoff: TimeInterval = 0.25,
        maximumBackoff: TimeInterval = 30,
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
        self.logger = logger
        var continuation: AsyncStream<CloudInboundCommand>.Continuation!
        commands = AsyncStream { continuation = $0 }
        commandContinuation = continuation
        var readyContinuation: AsyncStream<UInt64>.Continuation!
        readyGenerations = AsyncStream(bufferingPolicy: .bufferingNewest(1)) {
            readyContinuation = $0
        }
        self.readyContinuation = readyContinuation
    }

    func connect(role: CloudTransportRole = .machine) async throws {
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
            }
            throw error
        }
    }

    /// Snapshot producers may call this while a reconnect is in flight. Only the newest envelope
    /// per channel is retained, matching the relay's last-snapshot semantics.
    func publish(envelope: CloudEnvelope) async throws {
        guard state != .idle, state != .shutDown else {
            throw CloudTransportError.notConnected
        }
        guard let socket else {
            pendingByChannel[envelope.ch] = envelope
            return
        }
        do {
            try await sendPublish(envelope, over: socket)
        } catch {
            pendingByChannel[envelope.ch] = envelope
            socket.close()
            throw error
        }
    }

    func currentState() -> CloudTransportState { state }
    func droppedInboundCount() -> Int { droppedInbound }
    func droppedReadyGenerationCount() -> Int { droppedReadyGenerations }

    func shutdown() async {
        guard state != .shutDown else { return }
        state = .shutDown
        refreshTask?.cancel()
        refreshTask = nil
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
        pendingByChannel.removeAll()
        commandContinuation.finish()
        readyContinuation.finish()
        _ = await connector?.result
        if let task { await task.value }
    }

    private func establish(role: CloudTransportRole) async throws -> (socket: any CloudTransportSocket, generation: Int) {
        let token = try await validToken()
        // `validToken()` may suspend while another actor turn completes shutdown. From this check
        // through `connectorTask` registration there is no suspension, so a terminal transport
        // cannot create a connector after shutdown has already passed its cancel/join boundary.
        try Task.checkCancellation()
        guard state != .shutDown else { throw CancellationError() }
        let url = try connectURL(role: role)
        let connector = self.connector
        let attempt = Task {
            try await connector.connect(url: url, bearerToken: token.value)
        }
        connectorTask = attempt
        let newSocket: any CloudTransportSocket
        do {
            newSocket = try await withTaskCancellationHandler {
                try await attempt.value
            } onCancel: {
                attempt.cancel()
            }
        } catch {
            connectorTask = nil
            throw error
        }
        connectorTask = nil
        guard state != .shutDown, !Task.isCancelled else {
            newSocket.close()
            throw CancellationError()
        }
        connectingSocket = newSocket
        defer {
            if connectingSocket === newSocket { connectingSocket = nil }
        }

        do {
            let challengeText = try await newSocket.receiveText()
            let challenge = try decodeChallenge(challengeText)
            let key = try await keyProvider.deviceKeyPair()
            let signed = [challenge.context, challenge.account, challenge.device, challenge.challenge]
                .joined(separator: "|")
            let signature = try key.signature(for: Data(signed.utf8)).base64EncodedString()
            try await newSocket.send(text: try encode(HelloFrame(sig: signature)))

            let readyText = try await newSocket.receiveText()
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
            socket = newSocket
            state = .ready
            scheduleRefresh(token: token, generation: currentGeneration)
            try await flushPending(over: newSocket)
            switch readyContinuation.yield(UInt64(currentGeneration)) {
            case .dropped:
                droppedReadyGenerations += 1
            case .enqueued, .terminated:
                break
            @unknown default:
                break
            }
            return (newSocket, currentGeneration)
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

    private func receiveAndReconnect(socket initialSocket: any CloudTransportSocket, generation initialGeneration: Int) async {
        var activeSocket = initialSocket
        var activeGeneration = initialGeneration
        var backoff = initialBackoff

        while state != .shutDown, !Task.isCancelled {
            do {
                let text = try await activeSocket.receiveText()
                try await handle(text)
                backoff = initialBackoff
                continue
            } catch is CancellationError {
                return
            } catch {
                if state == .shutDown || Task.isCancelled { return }
                if generation == activeGeneration {
                    self.socket = nil
                    refreshTask?.cancel()
                    refreshTask = nil
                    state = .reconnecting
                }
            }

            while state != .shutDown, !Task.isCancelled {
                let jitter = 0.75 + (await clock.jitterUnit() * 0.5)
                do {
                    try await clock.sleep(for: min(maximumBackoff, backoff) * jitter)
                    guard let role else { return }
                    let established = try await establish(role: role)
                    activeSocket = established.socket
                    activeGeneration = established.generation
                    backoff = initialBackoff
                    break
                } catch is CancellationError {
                    return
                } catch {
                    if state == .shutDown || Task.isCancelled { return }
                    state = .reconnecting
                    backoff = min(maximumBackoff, backoff * 2)
                }
            }
        }
    }

    private func handle(_ text: String) async throws {
        let data = Data(text.utf8)
        let header = try JSONDecoder().decode(FrameHeader.self, from: data)
        switch header.type {
        case "envelope":
            do {
                let frame = try JSONDecoder().decode(EnvelopeFrame.self, from: data)
                try await acceptInbound(frame.envelope)
            } catch {
                dropInbound(reason: reason(for: error))
            }
        case "ack", "subscriptions", "pong":
            return
        case "ping":
            guard let socket else { throw CloudTransportError.notConnected }
            try await socket.send(text: "{\"type\":\"pong\"}")
        case "error":
            let frame = try JSONDecoder().decode(ErrorFrame.self, from: data)
            logger("CloudTransport relay error code=\(frame.code)")
        default:
            throw CloudTransportError.unexpectedFrame(header.type)
        }
    }

    private func acceptInbound(_ envelope: CloudEnvelope) async throws {
        guard envelope.envelopeClass == .ctl || envelope.envelopeClass == .dispatch,
              envelope.ch.hasPrefix("ctl/") else {
            throw CloudTransportError.unexpectedFrame("non-command-envelope")
        }
        let paired = await keyProvider.pairedDevicePublicKeys()
        guard paired[envelope.sender] != nil else { throw CloudEnvelopeError.unknownSender }
        let secret = try await keyProvider.masterSecret(for: envelope.keyID)
        let plaintext = try envelope.open(
            masterSecret: secret,
            publicKeyForSender: { paired[$0] }
        )
        guard sequenceTracker.accept(sender: envelope.sender, sequence: envelope.seq) else {
            throw CloudEnvelopeError.replay
        }
        commandContinuation.yield(CloudInboundCommand(
            channel: envelope.ch,
            sequence: envelope.seq,
            timestamp: envelope.ts,
            commandClass: envelope.envelopeClass,
            sender: envelope.sender,
            plaintext: plaintext
        ))
    }

    private func dropInbound(reason: String) {
        droppedInbound += 1
        logger("CloudTransport dropped inbound envelope reason=\(reason) count=\(droppedInbound)")
    }

    private func reason(for error: Error) -> String {
        guard let envelopeError = error as? CloudEnvelopeError else { return "invalid" }
        switch envelopeError {
        case .unknownSender: return "unknown_sender"
        case .badSignature: return "bad_signature"
        case .replay: return "replay"
        default: return "invalid"
        }
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

    private func refreshToken(generation expectedGeneration: Int) {
        guard state == .ready, generation == expectedGeneration else { return }
        cachedToken = nil
        socket?.close()
    }

    private func flushPending(over socket: any CloudTransportSocket) async throws {
        let pending = pendingByChannel.values.sorted { $0.ch < $1.ch }
        pendingByChannel.removeAll()
        do {
            for envelope in pending { try await sendPublish(envelope, over: socket) }
        } catch {
            for envelope in pending { pendingByChannel[envelope.ch] = envelope }
            throw error
        }
    }

    private func sendPublish(_ envelope: CloudEnvelope, over socket: any CloudTransportSocket) async throws {
        try await socket.send(text: try encode(PublishFrame(envelope: envelope)))
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

    private struct PublishFrame: Encodable {
        let type = "publish"
        let envelope: CloudEnvelope
    }

    private struct EnvelopeFrame: Decodable {
        let type: String
        let envelope: CloudEnvelope
    }

    private struct ErrorFrame: Decodable {
        let type: String
        let code: String
        let message: String
    }
}
