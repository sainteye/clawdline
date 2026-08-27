import Foundation

private struct CloudTransportTestFailure: Error, CustomStringConvertible {
    let description: String
}

private actor CloudTestTokenProvider: CloudDeviceTokenProviding {
    private let tokens: [CloudDeviceToken]
    private var index = 0

    init(tokens: [CloudDeviceToken]) {
        self.tokens = tokens
    }

    func fetchDeviceToken() async throws -> CloudDeviceToken {
        guard !tokens.isEmpty else { throw CloudTransportError.invalidTokenResponse }
        let selected = tokens[min(index, tokens.count - 1)]
        index += 1
        return selected
    }

    func fetchCount() -> Int { index }
}

private final class CloudTestLog: @unchecked Sendable {
    private let lock = NSLock()
    private var values: [String] = []

    func append(_ line: String) {
        lock.lock()
        values.append(line)
        lock.unlock()
    }

    func lines() -> [String] {
        lock.lock()
        defer { lock.unlock() }
        return values
    }
}

/// A standalone end-to-end test against the loopback relay state machine. The injected in-memory
/// connector is the sandbox fallback for the fake's NWListener WebSocket front end.
func runCloudTransportTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudTransportTestFailure(description: message) }
    }

    let machineKey = CloudDeviceKeyPair()
    let viewerKey = CloudDeviceKeyPair()
    let unknownKey = CloudDeviceKeyPair()
    let master = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x42, count: 32))
    let tokenProvider = CloudTestTokenProvider(tokens: [
        CloudDeviceToken(value: "machine-token-1", expiresAt: Date().addingTimeInterval(0.8)),
        CloudDeviceToken(value: "machine-token-2", expiresAt: Date().addingTimeInterval(60))
    ])
    let relay = CloudLoopbackRelay(
        account: "account-test",
        deviceID: "machine-device",
        devicePublicKey: machineKey.publicKeyRaw,
        allowedTokens: ["machine-token-1", "machine-token-2"]
    )
    let relayURL = URL(string: "ws://loopback.invalid/v1/connect")!
    let logs = CloudTestLog()
    let keys = CloudStaticTransportKeys(
        deviceKey: machineKey,
        masterSecrets: ["master-1": master],
        pairedDevices: ["viewer-device": viewerKey.publicKeyRaw]
    )
    let transport = CloudTransport(
        relayBaseURL: relayURL,
        tokenProvider: tokenProvider,
        keyProvider: keys,
        connector: CloudLoopbackSocketConnector(relay: relay),
        refreshAhead: 0.2,
        initialBackoff: 0.02,
        maximumBackoff: 0.08,
        logger: { logs.append($0) }
    )

    try await transport.connect(role: .machine)
    let initialState = await transport.currentState()
    let initialHandshakes = await relay.completedHandshakes()
    let initialTokens = await relay.observedTokens()
    try require(initialState == .ready, "handshake reaches ready")
    try require(initialHandshakes == 1, "relay verifies challenge signature")
    try require(initialTokens == ["machine-token-1"], "Bearer token is presented on upgrade")

    let snapshot = try CloudEnvelope.seal(
        Data("snapshot-one".utf8),
        ch: "s/machine-1/session-1",
        seq: 1,
        ts: millisecondsNow(),
        envelopeClass: .stream,
        keyID: "master-1",
        sender: "machine-device",
        masterSecret: master,
        signingKey: machineKey
    )
    try await transport.publish(envelope: snapshot)
    try await waitUntil("relay receives published snapshot") {
        await relay.publishedEnvelopes().contains(snapshot)
    }
    checks += 1

    let command = try CloudEnvelope.seal(
        Data("{\"type\":\"answer\",\"value\":\"yes\"}".utf8),
        ch: "ctl/machine-1",
        seq: 1,
        ts: millisecondsNow(),
        envelopeClass: .ctl,
        keyID: "master-1",
        sender: "viewer-device",
        masterSecret: master,
        signingKey: viewerKey
    )
    let commandTask = Task { try await nextCommand(from: transport.commands) }
    try await relay.send(envelope: command)
    let received = try await commandTask.value
    try require(received?.plaintext == Data("{\"type\":\"answer\",\"value\":\"yes\"}".utf8), "verified command is opened")
    try require(received?.sender == "viewer-device", "verified command retains sender")
    try require(received?.commandClass == .ctl, "verified command retains class")

    let unknown = try CloudEnvelope.seal(
        Data("unknown".utf8),
        ch: "ctl/machine-1",
        seq: 2,
        ts: millisecondsNow(),
        envelopeClass: .ctl,
        keyID: "master-1",
        sender: "unknown-device",
        masterSecret: master,
        signingKey: unknownKey
    )
    try await relay.send(envelope: unknown)
    let forged = try forgedSignature(command, sequence: 3)
    try await relay.send(envelope: forged)
    try await waitUntil("unknown and forged senders are dropped") {
        await transport.droppedInboundCount() == 2
    }
    checks += 1
    let dropLogs = logs.lines().filter { $0.contains("dropped inbound envelope") }
    try require(dropLogs.count == 2, "each rejected envelope has one counted log")
    try require(dropLogs.allSatisfy { !$0.contains("unknown-device") && !$0.contains(command.ct) }, "drop logs contain no envelope content")

    try await relay.send(envelope: command)
    try await waitUntil("replayed command is dropped") {
        await transport.droppedInboundCount() == 3
    }
    checks += 1

    try await waitUntil("token refresh reconnects before expiry", timeout: 2) {
        let fetches = await tokenProvider.fetchCount()
        let completed = await relay.completedHandshakes()
        return fetches >= 2 && completed >= 2
    }
    checks += 1
    let refreshedTokens = await relay.observedTokens()
    try require(refreshedTokens.contains("machine-token-2"), "refresh uses a new device token")

    let handshakesBeforeDrop = await relay.completedHandshakes()
    await relay.dropConnections()
    try await waitUntil("transport enters reconnect") {
        await transport.currentState() == .reconnecting
    }
    let queuedSnapshot = try CloudEnvelope.seal(
        Data("snapshot-after-drop".utf8),
        ch: "s/machine-1/session-2",
        seq: 2,
        ts: millisecondsNow(),
        envelopeClass: .stream,
        keyID: "master-1",
        sender: "machine-device",
        masterSecret: master,
        signingKey: machineKey
    )
    try await transport.publish(envelope: queuedSnapshot)
    try await waitUntil("reconnect completes and queued snapshot publishes", timeout: 2) {
        let completed = await relay.completedHandshakes()
        let published = await relay.publishedEnvelopes()
        return completed > handshakesBeforeDrop && published.contains(queuedSnapshot)
    }
    checks += 1

    await transport.shutdown()
    let shutdownState = await transport.currentState()
    try require(shutdownState == .shutDown, "shutdown reaches terminal state")
    let streamEnded = try await nextCommand(from: transport.commands, timeout: 0.5) == nil
    try require(streamEnded, "shutdown finishes inbound stream")
    await relay.stop()

    return checks
}

private func forgedSignature(_ source: CloudEnvelope, sequence: UInt64) throws -> CloudEnvelope {
    var object = try JSONSerialization.jsonObject(with: source.encodeJSON()) as! [String: Any]
    object["seq"] = sequence
    var signature = Data(base64Encoded: source.sig)!
    signature[signature.startIndex] ^= 0x80
    object["sig"] = signature.base64EncodedString()
    return try CloudEnvelope.decodeJSON(JSONSerialization.data(withJSONObject: object))
}

private func millisecondsNow() -> UInt64 {
    UInt64(Date().timeIntervalSince1970 * 1000)
}

private func waitUntil(
    _ description: String,
    timeout: TimeInterval = 1,
    condition: @escaping () async -> Bool
) async throws {
    let deadline = Date().addingTimeInterval(timeout)
    while Date() < deadline {
        if await condition() { return }
        try await Task.sleep(nanoseconds: 10_000_000)
    }
    throw CloudTransportTestFailure(description: "timed out: \(description)")
}

private func nextCommand(
    from stream: AsyncStream<CloudInboundCommand>,
    timeout: TimeInterval = 1
) async throws -> CloudInboundCommand? {
    try await withThrowingTaskGroup(of: CloudInboundCommand?.self) { group in
        group.addTask {
            var iterator = stream.makeAsyncIterator()
            return await iterator.next()
        }
        group.addTask {
            try await Task.sleep(nanoseconds: UInt64(timeout * 1_000_000_000))
            throw CloudTransportTestFailure(description: "timed out waiting for inbound command")
        }
        let result = try await group.next()!
        group.cancelAll()
        return result
    }
}

#if CLOUD_TRANSPORT_STANDALONE
@main
private struct CloudTransportStandaloneTests {
    static func main() async throws {
        let count = try await runCloudTransportTests()
        print("\(count) CloudTransport checks passed")
    }
}
#endif
