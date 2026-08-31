import CryptoKit
import Foundation
import Network

enum CloudLoopbackRelayError: Error, LocalizedError {
    case didNotStart
    case noReadyMachine
    case invalidFrame

    var errorDescription: String? {
        switch self {
        case .didNotStart: return "The loopback relay did not start."
        case .noReadyMachine: return "The loopback relay has no authenticated machine."
        case .invalidFrame: return "The loopback relay received an invalid frame."
        }
    }
}

/// A reconnect probe whose relay side drops every authenticated receive without closing the
/// client half. The transport therefore has to dispose of each predecessor itself; dropping its
/// reference is observable as more than one live socket. The probe is intentionally independent
/// of URLSession and localhost so the ownership invariant is deterministic in sandboxes.
final class CloudReconnectSocketProbe: @unchecked Sendable {
    private let lock = NSLock()
    private var live = 0
    private var peak = 0
    private var opened = 0
    private var closed = 0

    fileprivate func didOpen() {
        lock.lock()
        opened += 1
        live += 1
        peak = max(peak, live)
        lock.unlock()
    }

    fileprivate func didClose() {
        lock.lock()
        closed += 1
        live -= 1
        lock.unlock()
    }

    func snapshot() -> (opened: Int, live: Int, peak: Int, closed: Int) {
        lock.lock()
        defer { lock.unlock() }
        return (opened, live, peak, closed)
    }
}

struct CloudReconnectProbeConnector: CloudTransportSocketConnecting, Sendable {
    let probe: CloudReconnectSocketProbe
    let behavior: CloudReconnectProbeBehavior
    let onHandledFrame: @Sendable () async -> Void

    init(probe: CloudReconnectSocketProbe, dropsAfterAuthentication: Bool = true) {
        self.probe = probe
        behavior = dropsAfterAuthentication ? .disconnectAfterAuthentication : .stayConnected
        onHandledFrame = {}
    }

    init(
        probe: CloudReconnectSocketProbe,
        behavior: CloudReconnectProbeBehavior,
        onHandledFrame: @escaping @Sendable () async -> Void = {}
    ) {
        self.probe = probe
        self.behavior = behavior
        self.onHandledFrame = onHandledFrame
    }

    func connect(url: URL, bearerToken: String) async throws -> CloudEstablishedTransportSocket {
        CloudEstablishedTransportSocket(CloudReconnectProbeSocket(
            probe: probe, behavior: behavior, onHandledFrame: onHandledFrame
        ))
    }
}

enum CloudReconnectProbeBehavior: Equatable, Sendable {
    case disconnectAfterAuthentication
    case handledFrameThenDisconnect
    case unexpectedFrame
    case failPendingPublish
    case stayConnected
}

final class CloudReconnectSequenceConnector: CloudTransportSocketConnecting, @unchecked Sendable {
    private let lock = NSLock()
    private let probe: CloudReconnectSocketProbe
    private let behaviors: [CloudReconnectProbeBehavior]
    private let onHandledFrame: @Sendable () async -> Void
    private var index = 0

    init(
        probe: CloudReconnectSocketProbe,
        behaviors: [CloudReconnectProbeBehavior],
        onHandledFrame: @escaping @Sendable () async -> Void = {}
    ) {
        self.probe = probe
        self.behaviors = behaviors
        self.onHandledFrame = onHandledFrame
    }

    func connect(url: URL, bearerToken: String) async throws -> CloudEstablishedTransportSocket {
        let behavior = nextBehavior()
        return CloudEstablishedTransportSocket(CloudReconnectProbeSocket(
            probe: probe, behavior: behavior, onHandledFrame: onHandledFrame
        ))
    }

    private func nextBehavior() -> CloudReconnectProbeBehavior {
        lock.lock()
        defer { lock.unlock() }
        let behavior = behaviors[min(index, behaviors.count - 1)]
        index += 1
        return behavior
    }
}

final class CloudUnauthorizedReconnectConnector: CloudTransportSocketConnecting,
    @unchecked Sendable
{
    private let lock = NSLock()
    private let probe: CloudReconnectSocketProbe
    private var attempts = 0
    private var tokens: [String] = []

    init(probe: CloudReconnectSocketProbe) {
        self.probe = probe
    }

    func connect(url: URL, bearerToken: String) async throws -> CloudEstablishedTransportSocket {
        let attempt = recordAttempt(token: bearerToken)
        if attempt == 2 { throw CloudTransportError.unauthorized }
        let behavior: CloudReconnectProbeBehavior = attempt == 1
            ? .disconnectAfterAuthentication : .stayConnected
        return CloudEstablishedTransportSocket(CloudReconnectProbeSocket(
            probe: probe, behavior: behavior
        ))
    }

    private func recordAttempt(token: String) -> Int {
        lock.lock()
        defer { lock.unlock() }
        attempts += 1
        tokens.append(token)
        return attempts
    }

    func observedTokens() -> [String] {
        lock.lock()
        defer { lock.unlock() }
        return tokens
    }
}

private final class CloudReconnectProbeSocket: CloudTransportSocket, @unchecked Sendable {
    private let probe: CloudReconnectSocketProbe
    private let lock = NSLock()
    private let continuation: AsyncThrowingStream<String, Error>.Continuation
    private var iterator: AsyncThrowingStream<String, Error>.Iterator
    private var didSendHello = false
    private var isClosed = false
    private let behavior: CloudReconnectProbeBehavior
    private let onHandledFrame: @Sendable () async -> Void
    private var receivedFrameCount = 0

    init(
        probe: CloudReconnectSocketProbe,
        behavior: CloudReconnectProbeBehavior,
        onHandledFrame: @escaping @Sendable () async -> Void = {}
    ) {
        self.probe = probe
        self.behavior = behavior
        self.onHandledFrame = onHandledFrame
        var continuation: AsyncThrowingStream<String, Error>.Continuation!
        let stream = AsyncThrowingStream<String, Error> { continuation = $0 }
        self.continuation = continuation
        iterator = stream.makeAsyncIterator()
        probe.didOpen()
        let challenge = Data(repeating: 0x5a, count: 32).base64EncodedString()
        continuation.yield("{\"type\":\"challenge\",\"v\":1,\"context\":\"clawdline-challenge-v1\",\"account\":\"probe-account\",\"device\":\"probe-device\",\"challenge\":\"\(challenge)\",\"expires_in_ms\":15000}")
    }

    func send(text: String) async throws {
        let shouldReady = claimHello()
        guard shouldReady else {
            if behavior == .failPendingPublish { throw CloudTransportError.notConnected }
            return
        }
        continuation.yield("{\"type\":\"ready\",\"v\":1,\"account\":\"probe-account\",\"device\":\"probe-device\",\"role\":\"machine\",\"connected_at\":0,\"token_expires_at\":3600000}")
        switch behavior {
        case .disconnectAfterAuthentication, .failPendingPublish:
            continuation.finish(throwing: CloudTransportError.notConnected)
        case .handledFrameThenDisconnect:
            continuation.yield("{\"type\":\"pong\"}")
            continuation.finish(throwing: CloudTransportError.notConnected)
        case .unexpectedFrame:
            continuation.yield("{\"type\":\"bogus\"}")
        case .stayConnected:
            break
        }
    }

    private func claimHello() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        let shouldReady = !didSendHello && !isClosed
        didSendHello = true
        return shouldReady
    }

    func receiveText() async throws -> String {
        guard let text = try await iterator.next() else { throw CloudTransportError.notConnected }
        receivedFrameCount += 1
        if receivedFrameCount > 2, behavior == .handledFrameThenDisconnect {
            await onHandledFrame()
        }
        return text
    }

    func close() {
        lock.lock()
        guard !isClosed else {
            lock.unlock()
            return
        }
        isClosed = true
        lock.unlock()
        continuation.finish()
        probe.didClose()
    }
}

actor CloudReconnectProbeClock: CloudTransportClock {
    private var sleeps: [TimeInterval] = []
    private var date = Date()

    func now() -> Date { date }
    func jitterUnit() -> Double { 0.5 }

    func advance(by seconds: TimeInterval) { date = date.addingTimeInterval(seconds) }

    func sleep(for seconds: TimeInterval) async throws {
        // Token refresh shares this clock. Keep that long-lived timer suspended without letting
        // it masquerade as a reconnect delay in the probe's reading.
        if seconds > 100 {
            try await Task.sleep(nanoseconds: 3_600_000_000_000)
            return
        }
        sleeps.append(seconds)
        if sleeps.count >= 3 {
            try await Task.sleep(nanoseconds: 3_600_000_000_000)
        }
    }

    func recordedSleeps() -> [TimeInterval] { sleeps }
}

enum CloudURLSessionConnectorProbeOutcome: Sendable {
    case opens
    case unauthorized
    case stalls
}

final class CloudURLSessionConnectorProbe: CloudURLSessionSocketStarting, @unchecked Sendable {
    struct Snapshot {
        let authorization: String?
        let waitsForConnectivity: Bool
        let requestTimeout: TimeInterval
        let resourceTimeout: TimeInterval
        let resumed: Bool
        let closed: Bool
        let socketsClosed: Int
    }

    private let lock = NSLock()
    private let outcome: CloudURLSessionConnectorProbeOutcome
    private var authorization: String?
    private var waitsForConnectivity = true
    private var requestTimeout: TimeInterval = 0
    private var resourceTimeout: TimeInterval = 0
    private var socket: CloudURLSessionConnectorProbeSocket?
    private var socketsClosed = 0

    init(outcome: CloudURLSessionConnectorProbeOutcome) {
        self.outcome = outcome
    }

    func start(
        request: URLRequest,
        configuration: URLSessionConfiguration,
        observer: CloudWebSocketOpenObserver
    ) -> any CloudStartedTransportSocket {
        let socket = CloudURLSessionConnectorProbeSocket(
            observer: observer, outcome: outcome,
            resourceTimeout: configuration.timeoutIntervalForResource,
            onClose: { [weak self] in self?.didCloseSocket() }
        )
        lock.lock()
        authorization = request.value(forHTTPHeaderField: "Authorization")
        waitsForConnectivity = configuration.waitsForConnectivity
        requestTimeout = configuration.timeoutIntervalForRequest
        resourceTimeout = configuration.timeoutIntervalForResource
        self.socket = socket
        lock.unlock()
        return socket
    }

    func snapshot() -> Snapshot {
        lock.lock()
        let authorization = authorization
        let waitsForConnectivity = waitsForConnectivity
        let requestTimeout = requestTimeout
        let resourceTimeout = resourceTimeout
        let socketsClosed = socketsClosed
        let socket = socket
        lock.unlock()
        let lifecycle = socket?.snapshot() ?? (resumed: false, closed: false)
        return Snapshot(
            authorization: authorization,
            waitsForConnectivity: waitsForConnectivity,
            requestTimeout: requestTimeout,
            resourceTimeout: resourceTimeout,
            resumed: lifecycle.resumed,
            closed: lifecycle.closed,
            socketsClosed: socketsClosed
        )
    }

    private func didCloseSocket() {
        lock.lock()
        socketsClosed += 1
        lock.unlock()
    }

    func close() {
        lock.lock()
        let socket = socket
        lock.unlock()
        socket?.close()
    }
}

private final class CloudURLSessionConnectorProbeSocket: CloudStartedTransportSocket,
    @unchecked Sendable
{
    private let lock = NSLock()
    private let observer: CloudWebSocketOpenObserver
    private let outcome: CloudURLSessionConnectorProbeOutcome
    private let resourceTimeout: TimeInterval
    private let onClose: @Sendable () -> Void
    private let continuation: AsyncThrowingStream<String, Error>.Continuation
    private var iterator: AsyncThrowingStream<String, Error>.Iterator
    private var expiryTask: Task<Void, Never>?
    private var resumed = false
    private var closed = false
    private var didSendHello = false

    init(
        observer: CloudWebSocketOpenObserver,
        outcome: CloudURLSessionConnectorProbeOutcome,
        resourceTimeout: TimeInterval,
        onClose: @escaping @Sendable () -> Void
    ) {
        self.observer = observer
        self.outcome = outcome
        self.resourceTimeout = resourceTimeout
        self.onClose = onClose
        var continuation: AsyncThrowingStream<String, Error>.Continuation!
        let stream = AsyncThrowingStream<String, Error> { continuation = $0 }
        self.continuation = continuation
        iterator = stream.makeAsyncIterator()
    }

    func resume() {
        lock.lock()
        resumed = true
        lock.unlock()
        switch outcome {
        case .opens:
            observer.opened()
            let challenge = Data(repeating: 0x5b, count: 32).base64EncodedString()
            continuation.yield("{\"type\":\"challenge\",\"v\":1,\"context\":\"clawdline-challenge-v1\",\"account\":\"urlsession-probe\",\"device\":\"probe-device\",\"challenge\":\"\(challenge)\",\"expires_in_ms\":15000}")
            expiryTask = Task { [weak self] in
                guard let self else { return }
                try? await Task.sleep(
                    nanoseconds: UInt64(max(0.001, resourceTimeout) * 1_000_000_000)
                )
                if !Task.isCancelled { self.close() }
            }
        case .unauthorized:
            observer.failed(statusCode: 401, error: nil)
        case .stalls:
            break
        }
    }

    func send(text: String) async throws {
        let shouldReady = claimHello()
        if shouldReady {
            continuation.yield("{\"type\":\"ready\",\"v\":1,\"account\":\"urlsession-probe\",\"device\":\"probe-device\",\"role\":\"machine\",\"connected_at\":0,\"token_expires_at\":3600000}")
        }
    }

    private func claimHello() -> Bool {
        lock.lock()
        let shouldReady = !didSendHello && !closed
        didSendHello = true
        lock.unlock()
        return shouldReady
    }

    func receiveText() async throws -> String {
        guard let text = try await iterator.next() else { throw CloudTransportError.notConnected }
        return text
    }

    func close() {
        lock.lock()
        guard !closed else {
            lock.unlock()
            return
        }
        closed = true
        let expiryTask = expiryTask
        lock.unlock()
        expiryTask?.cancel()
        continuation.finish()
        onClose()
    }

    func snapshot() -> (resumed: Bool, closed: Bool) {
        lock.lock()
        defer { lock.unlock() }
        return (resumed, closed)
    }
}

/// A deliberately small, in-process WebSocket relay for transport tests. It binds only to
/// 127.0.0.1, validates the Bearer upgrade and real relay challenge signature, and implements
/// challenge/hello/ready, publish/ack, envelope delivery, and forced disconnects.
///
/// TLS is intentionally outside this fake's boundary: production accepts `wss://`; the loopback
/// listener exposes `ws://127.0.0.1` so tests need neither a trusted test CA nor a TLS bypass.
actor CloudLoopbackRelay {
    private struct Client {
        let wire: CloudLoopbackWireConnection
        var buffer = Data()
        var upgraded = false
        var authenticated = false
        var challenge = ""
        var token = ""
    }

    private struct MemoryClient {
        let socket: CloudLoopbackMemorySocket
        var authenticated = false
        var challenge: String
    }

    let account: String
    let deviceID: String
    let devicePublicKey: Data

    private let queue = DispatchQueue(label: "app.clawdline.cloud.loopback-relay")
    private var listener: NWListener?
    private var clients: [UUID: Client] = [:]
    private var memoryClients: [UUID: MemoryClient] = [:]
    private var allowedTokens: Set<String>
    private var published: [CloudEnvelope] = []
    private var handshakes = 0
    private var tokensSeen: [String] = []

    init(
        account: String,
        deviceID: String,
        devicePublicKey: Data,
        allowedTokens: Set<String>
    ) {
        self.account = account
        self.deviceID = deviceID
        self.devicePublicKey = devicePublicKey
        self.allowedTokens = allowedTokens
    }

    func start() async throws -> URL {
        if let url = relayURL() { return url }
        let parameters = NWParameters.tcp
        parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: .any)
        let listener = try NWListener(using: parameters)
        self.listener = listener
        listener.newConnectionHandler = { [weak self] connection in
            guard let relay = self else {
                connection.cancel()
                return
            }
            Task { await relay.accept(connection) }
        }
        return try await withCheckedThrowingContinuation { continuation in
            let gate = CloudLoopbackContinuationGate()
            listener.stateUpdateHandler = { state in
                switch state {
                case .ready:
                    guard gate.claim() else { return }
                    guard let port = listener.port, port.rawValue != 0,
                          let url = URL(string: "ws://127.0.0.1:\(port.rawValue)/v1/connect") else {
                        continuation.resume(throwing: CloudLoopbackRelayError.didNotStart)
                        return
                    }
                    continuation.resume(returning: url)
                case .failed(let error):
                    guard gate.claim() else { return }
                    continuation.resume(throwing: error)
                case .cancelled:
                    guard gate.claim() else { return }
                    continuation.resume(throwing: CloudLoopbackRelayError.didNotStart)
                default:
                    break
                }
            }
            listener.start(queue: queue)
        }
    }

    func stop() {
        listener?.cancel()
        listener = nil
        for client in clients.values { client.wire.cancel() }
        clients.removeAll()
        for client in memoryClients.values { client.socket.serverClose() }
        memoryClients.removeAll()
    }

    func allowToken(_ token: String) {
        allowedTokens.insert(token)
    }

    func publishedEnvelopes() -> [CloudEnvelope] { published }
    func completedHandshakes() -> Int { handshakes }
    func observedTokens() -> [String] { tokensSeen }

    func send(envelope: CloudEnvelope) throws {
        let object: [String: Any] = [
            "type": "envelope",
            "envelope": try jsonObject(envelope)
        ]
        if let client = clients.values.first(where: { $0.authenticated }) {
            try sendJSON(object, to: client.wire)
            return
        }
        if let client = memoryClients.values.first(where: { $0.authenticated }) {
            try sendJSON(object, to: client.socket)
            return
        }
        throw CloudLoopbackRelayError.noReadyMachine
    }

    func dropConnections() {
        for client in clients.values {
            client.wire.sendClose(code: 1012, reason: "test reconnect")
        }
        for client in memoryClients.values { client.socket.serverClose() }
        memoryClients.removeAll()
    }

    fileprivate func connectInProcess(url: URL, bearerToken: String) throws -> CloudLoopbackMemorySocket {
        guard url.path == "/v1/connect",
              URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems
                == [URLQueryItem(name: "role", value: "machine")],
              allowedTokens.contains(bearerToken) else {
            throw CloudLoopbackRelayError.invalidFrame
        }
        let id = UUID()
        let challengeBytes = Data((0..<32).map { UInt8(($0 + handshakes) & 0xff) })
        let challenge = challengeBytes.base64EncodedString()
        let socket = CloudLoopbackMemorySocket(id: id, relay: self)
        memoryClients[id] = MemoryClient(socket: socket, challenge: challenge)
        tokensSeen.append(bearerToken)
        try sendJSON([
            "type": "challenge",
            "v": 1,
            "context": "clawdline-challenge-v1",
            "account": account,
            "device": deviceID,
            "challenge": challenge,
            "expires_in_ms": 15_000
        ], to: socket)
        return socket
    }

    fileprivate func receivedInProcess(_ text: String, from id: UUID) throws {
        guard var client = memoryClients[id],
              let object = try JSONSerialization.jsonObject(with: Data(text.utf8)) as? [String: Any],
              let type = object["type"] as? String else {
            throw CloudLoopbackRelayError.invalidFrame
        }
        if !client.authenticated {
            guard type == "hello", let signatureText = object["sig"] as? String,
                  let signature = Data(base64Encoded: signatureText) else {
                throw CloudLoopbackRelayError.invalidFrame
            }
            let signingText = ["clawdline-challenge-v1", account, deviceID, client.challenge]
                .joined(separator: "|")
            let key = try Curve25519.Signing.PublicKey(rawRepresentation: devicePublicKey)
            guard key.isValidSignature(signature, for: Data(signingText.utf8)) else {
                client.socket.serverClose()
                memoryClients.removeValue(forKey: id)
                return
            }
            client.authenticated = true
            memoryClients[id] = client
            handshakes += 1
            try sendJSON([
                "type": "ready",
                "v": 1,
                "account": account,
                "device": deviceID,
                "role": "machine",
                "connected_at": Int(Date().timeIntervalSince1970 * 1000),
                "token_expires_at": Int(Date().addingTimeInterval(3600).timeIntervalSince1970 * 1000)
            ], to: client.socket)
            return
        }

        switch type {
        case "publish":
            guard let envelopeObject = object["envelope"] else {
                throw CloudLoopbackRelayError.invalidFrame
            }
            let data = try JSONSerialization.data(withJSONObject: envelopeObject)
            let envelope = try CloudEnvelope.decodeJSON(data)
            published.append(envelope)
            try sendJSON([
                "type": "ack",
                "ch": envelope.ch,
                "seq": envelope.seq,
                "fanout": 0,
                "status": "delivered"
            ], to: client.socket)
        case "ping":
            try sendJSON(["type": "pong"], to: client.socket)
        case "pong":
            return
        default:
            try sendJSON([
                "type": "error",
                "code": "bad_request",
                "message": "unknown frame type"
            ], to: client.socket)
        }
    }

    fileprivate func closedInProcess(_ id: UUID) {
        memoryClients.removeValue(forKey: id)
    }

    private func relayURL() -> URL? {
        guard let port = listener?.port, port.rawValue != 0 else { return nil }
        return URL(string: "ws://127.0.0.1:\(port.rawValue)/v1/connect")
    }

    private func accept(_ connection: NWConnection) {
        let id = UUID()
        let wire = CloudLoopbackWireConnection(id: id, connection: connection, relay: self)
        clients[id] = Client(wire: wire)
        wire.start(on: queue)
    }

    fileprivate func received(_ data: Data, from id: UUID) {
        guard var client = clients[id] else { return }
        client.buffer.append(data)
        clients[id] = client
        do {
            if !client.upgraded {
                try processUpgrade(for: id)
            }
            try processWebSocketFrames(for: id)
        } catch {
            clients[id]?.wire.sendClose(code: 1002, reason: "invalid frame")
        }
    }

    fileprivate func closed(_ id: UUID) {
        clients.removeValue(forKey: id)
    }

    private func processUpgrade(for id: UUID) throws {
        guard var client = clients[id],
              let end = client.buffer.range(of: Data("\r\n\r\n".utf8)) else { return }
        let requestData = client.buffer[..<end.upperBound]
        client.buffer.removeSubrange(..<end.upperBound)
        guard let request = String(data: requestData, encoding: .utf8) else {
            throw CloudLoopbackRelayError.invalidFrame
        }
        let lines = request.components(separatedBy: "\r\n")
        let requestParts = lines.first?.split(separator: " ") ?? []
        guard requestParts.count >= 2,
              requestParts[0] == "GET",
              String(requestParts[1]) == "/v1/connect?role=machine" else {
            throw CloudLoopbackRelayError.invalidFrame
        }
        var headers: [String: String] = [:]
        for line in lines.dropFirst() {
            guard let colon = line.firstIndex(of: ":") else { continue }
            let name = line[..<colon].trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
            let value = line[line.index(after: colon)...].trimmingCharacters(in: .whitespacesAndNewlines)
            headers[name] = value
        }
        guard let authorization = headers["authorization"], authorization.hasPrefix("Bearer "),
              let webSocketKey = headers["sec-websocket-key"] else {
            throw CloudLoopbackRelayError.invalidFrame
        }
        let token = String(authorization.dropFirst("Bearer ".count))
        guard allowedTokens.contains(token) else {
            client.wire.sendRaw(Data("HTTP/1.1 401 Unauthorized\r\nContent-Length: 0\r\n\r\n".utf8))
            client.wire.cancel()
            return
        }
        let acceptSource = Data((webSocketKey + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").utf8)
        let accept = Data(Insecure.SHA1.hash(data: acceptSource)).base64EncodedString()
        let response = [
            "HTTP/1.1 101 Switching Protocols",
            "Upgrade: websocket",
            "Connection: Upgrade",
            "Sec-WebSocket-Accept: \(accept)",
            "",
            ""
        ].joined(separator: "\r\n")
        client.upgraded = true
        client.token = token
        let challengeBytes = Data((0..<32).map { UInt8(($0 + handshakes) & 0xff) })
        client.challenge = challengeBytes.base64EncodedString()
        clients[id] = client
        tokensSeen.append(token)
        client.wire.sendRaw(Data(response.utf8))
        try sendJSON([
            "type": "challenge",
            "v": 1,
            "context": "clawdline-challenge-v1",
            "account": account,
            "device": deviceID,
            "challenge": client.challenge,
            "expires_in_ms": 15_000
        ], to: client.wire)
    }

    private func processWebSocketFrames(for id: UUID) throws {
        guard var client = clients[id], client.upgraded else { return }
        while let frame = try CloudLoopbackWebSocketFrame.parse(from: client.buffer) {
            client.buffer.removeFirst(frame.consumed)
            switch frame.opcode {
            case 0x1:
                guard let text = String(data: frame.payload, encoding: .utf8) else {
                    throw CloudLoopbackRelayError.invalidFrame
                }
                clients[id] = client
                try process(text: text, for: id)
                guard let refreshed = clients[id] else { return }
                client = refreshed
            case 0x8:
                client.wire.cancel()
                clients.removeValue(forKey: id)
                return
            case 0x9:
                client.wire.sendPong(frame.payload)
            case 0xA:
                break
            default:
                throw CloudLoopbackRelayError.invalidFrame
            }
        }
        clients[id] = client
    }

    private func process(text: String, for id: UUID) throws {
        guard var client = clients[id],
              let object = try JSONSerialization.jsonObject(with: Data(text.utf8)) as? [String: Any],
              let type = object["type"] as? String else {
            throw CloudLoopbackRelayError.invalidFrame
        }
        if !client.authenticated {
            guard type == "hello", let signatureText = object["sig"] as? String,
                  let signature = Data(base64Encoded: signatureText) else {
                throw CloudLoopbackRelayError.invalidFrame
            }
            let signingText = ["clawdline-challenge-v1", account, deviceID, client.challenge]
                .joined(separator: "|")
            let key = try Curve25519.Signing.PublicKey(rawRepresentation: devicePublicKey)
            guard key.isValidSignature(signature, for: Data(signingText.utf8)) else {
                client.wire.sendClose(code: 4401, reason: "bad challenge signature")
                return
            }
            client.authenticated = true
            clients[id] = client
            handshakes += 1
            try sendJSON([
                "type": "ready",
                "v": 1,
                "account": account,
                "device": deviceID,
                "role": "machine",
                "connected_at": Int(Date().timeIntervalSince1970 * 1000),
                "token_expires_at": Int(Date().addingTimeInterval(3600).timeIntervalSince1970 * 1000)
            ], to: client.wire)
            return
        }

        switch type {
        case "publish":
            guard let envelopeObject = object["envelope"] else {
                throw CloudLoopbackRelayError.invalidFrame
            }
            let data = try JSONSerialization.data(withJSONObject: envelopeObject)
            let envelope = try CloudEnvelope.decodeJSON(data)
            published.append(envelope)
            try sendJSON([
                "type": "ack",
                "ch": envelope.ch,
                "seq": envelope.seq,
                "fanout": 0,
                "status": "delivered"
            ], to: client.wire)
        case "ping":
            try sendJSON(["type": "pong"], to: client.wire)
        case "pong":
            return
        default:
            try sendJSON([
                "type": "error",
                "code": "bad_request",
                "message": "unknown frame type"
            ], to: client.wire)
        }
    }

    private func jsonObject(_ envelope: CloudEnvelope) throws -> Any {
        try JSONSerialization.jsonObject(with: envelope.encodeJSON())
    }

    private func sendJSON(_ object: [String: Any], to wire: CloudLoopbackWireConnection) throws {
        let data = try JSONSerialization.data(withJSONObject: object, options: [.sortedKeys])
        guard let text = String(data: data, encoding: .utf8) else {
            throw CloudLoopbackRelayError.invalidFrame
        }
        wire.sendText(text)
    }

    private func sendJSON(_ object: [String: Any], to socket: CloudLoopbackMemorySocket) throws {
        let data = try JSONSerialization.data(withJSONObject: object, options: [.sortedKeys])
        guard let text = String(data: data, encoding: .utf8) else {
            throw CloudLoopbackRelayError.invalidFrame
        }
        socket.serverSend(text)
    }
}

private final class CloudLoopbackContinuationGate: @unchecked Sendable {
    private let lock = NSLock()
    private var finished = false

    func claim() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        guard !finished else { return false }
        finished = true
        return true
    }
}

/// Sandbox-friendly adapter for the same fake relay state machine. It exists because some CI and
/// agent sandboxes refuse localhost bind; production still uses `CloudURLSessionSocketConnector`.
struct CloudLoopbackSocketConnector: CloudTransportSocketConnecting, Sendable {
    let relay: CloudLoopbackRelay

    func connect(url: URL, bearerToken: String) async throws -> CloudEstablishedTransportSocket {
        CloudEstablishedTransportSocket(
            try await relay.connectInProcess(url: url, bearerToken: bearerToken)
        )
    }
}

private final class CloudLoopbackMemorySocket: CloudTransportSocket, @unchecked Sendable {
    let id: UUID
    private weak var relay: CloudLoopbackRelay?
    private let continuation: AsyncStream<String>.Continuation
    private var iterator: AsyncStream<String>.Iterator

    init(id: UUID, relay: CloudLoopbackRelay) {
        self.id = id
        self.relay = relay
        var continuation: AsyncStream<String>.Continuation!
        let stream = AsyncStream<String> { continuation = $0 }
        self.continuation = continuation
        iterator = stream.makeAsyncIterator()
    }

    func send(text: String) async throws {
        guard let relay else { throw CloudTransportError.notConnected }
        try await relay.receivedInProcess(text, from: id)
    }

    func receiveText() async throws -> String {
        guard let text = await iterator.next() else { throw CloudTransportError.notConnected }
        return text
    }

    func close() {
        continuation.finish()
        Task { await relay?.closedInProcess(id) }
    }

    func serverSend(_ text: String) {
        continuation.yield(text)
    }

    func serverClose() {
        continuation.finish()
    }
}

private final class CloudLoopbackWireConnection: @unchecked Sendable {
    let id: UUID
    private let connection: NWConnection
    private weak var relay: CloudLoopbackRelay?

    init(id: UUID, connection: NWConnection, relay: CloudLoopbackRelay) {
        self.id = id
        self.connection = connection
        self.relay = relay
    }

    func start(on queue: DispatchQueue) {
        connection.stateUpdateHandler = { [weak self] state in
            guard let self else { return }
            switch state {
            case .ready:
                self.receiveNext()
            case .failed, .cancelled:
                Task { await self.relay?.closed(self.id) }
            default:
                break
            }
        }
        connection.start(queue: queue)
    }

    func sendRaw(_ data: Data) {
        connection.send(content: data, completion: .contentProcessed { _ in })
    }

    func sendText(_ text: String) {
        sendRaw(CloudLoopbackWebSocketFrame.encode(opcode: 0x1, payload: Data(text.utf8)))
    }

    func sendPong(_ payload: Data) {
        sendRaw(CloudLoopbackWebSocketFrame.encode(opcode: 0xA, payload: payload))
    }

    func sendClose(code: UInt16, reason: String) {
        var payload = Data([UInt8(code >> 8), UInt8(code & 0xff)])
        payload.append(Data(reason.utf8))
        connection.send(
            content: CloudLoopbackWebSocketFrame.encode(opcode: 0x8, payload: payload),
            completion: .contentProcessed { [weak self] _ in self?.connection.cancel() }
        )
    }

    func cancel() {
        connection.cancel()
    }

    private func receiveNext() {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 1_048_576) { [weak self] data, _, complete, _ in
            guard let self else { return }
            Task {
                if let data, !data.isEmpty { await self.relay?.received(data, from: self.id) }
                if complete {
                    await self.relay?.closed(self.id)
                } else {
                    self.receiveNext()
                }
            }
        }
    }
}

private enum CloudLoopbackWebSocketFrame {
    struct Parsed {
        let opcode: UInt8
        let payload: Data
        let consumed: Int
    }

    static func parse(from data: Data) throws -> Parsed? {
        guard data.count >= 2 else { return nil }
        let bytes = [UInt8](data)
        guard bytes[0] & 0x80 != 0 else { throw CloudLoopbackRelayError.invalidFrame }
        let opcode = bytes[0] & 0x0f
        let masked = bytes[1] & 0x80 != 0
        var length = UInt64(bytes[1] & 0x7f)
        var cursor = 2
        if length == 126 {
            guard bytes.count >= 4 else { return nil }
            length = (UInt64(bytes[2]) << 8) | UInt64(bytes[3])
            cursor = 4
        } else if length == 127 {
            guard bytes.count >= 10 else { return nil }
            length = 0
            for byte in bytes[2..<10] { length = (length << 8) | UInt64(byte) }
            cursor = 10
        }
        let maskBytes = masked ? 4 : 0
        guard length <= 1_048_576,
              UInt64(bytes.count) >= UInt64(cursor + maskBytes) + length else { return nil }
        let payloadStart = cursor + maskBytes
        var payload = Data(bytes[payloadStart..<(payloadStart + Int(length))])
        if masked {
            let mask = Array(bytes[cursor..<(cursor + 4)])
            for offset in payload.indices {
                payload[offset] ^= mask[offset % 4]
            }
        }
        return Parsed(opcode: opcode, payload: payload, consumed: payloadStart + Int(length))
    }

    static func encode(opcode: UInt8, payload: Data) -> Data {
        var frame = Data([0x80 | opcode])
        if payload.count < 126 {
            frame.append(UInt8(payload.count))
        } else if payload.count <= Int(UInt16.max) {
            frame.append(126)
            frame.append(UInt8((payload.count >> 8) & 0xff))
            frame.append(UInt8(payload.count & 0xff))
        } else {
            frame.append(127)
            let count = UInt64(payload.count)
            for shift in stride(from: 56, through: 0, by: -8) {
                frame.append(UInt8((count >> UInt64(shift)) & 0xff))
            }
        }
        frame.append(payload)
        return frame
    }
}
