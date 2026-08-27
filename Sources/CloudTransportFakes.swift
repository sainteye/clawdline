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
            Task { await self?.accept(connection) }
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

    func connect(url: URL, bearerToken: String) async throws -> any CloudTransportSocket {
        try await relay.connectInProcess(url: url, bearerToken: bearerToken)
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
