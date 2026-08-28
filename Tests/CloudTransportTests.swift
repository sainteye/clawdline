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

private actor CloudTestReadyGenerations {
    private var values: [UInt64] = []

    func append(_ generation: UInt64) { values.append(generation) }
    func all() -> [UInt64] { values }
}

private final class CloudSuspendedHandshakeSocket: CloudTransportSocket, @unchecked Sendable {
    private let lock = NSLock()
    private let continuation: AsyncStream<String>.Continuation
    private var iterator: AsyncStream<String>.Iterator
    private var receiveStarted = false
    private var closed = false

    init() {
        var continuation: AsyncStream<String>.Continuation!
        let stream = AsyncStream<String> { continuation = $0 }
        self.continuation = continuation
        iterator = stream.makeAsyncIterator()
    }

    func send(text: String) async throws {}

    func receiveText() async throws -> String {
        markReceiveStarted()
        guard let text = await iterator.next() else { throw CloudTransportError.notConnected }
        return text
    }

    private func markReceiveStarted() {
        lock.lock()
        receiveStarted = true
        lock.unlock()
    }

    func close() {
        lock.lock()
        closed = true
        lock.unlock()
        continuation.finish()
    }

    func state() -> (receiveStarted: Bool, closed: Bool) {
        lock.lock()
        defer { lock.unlock() }
        return (receiveStarted, closed)
    }
}

private struct CloudSuspendedHandshakeConnector: CloudTransportSocketConnecting {
    let socket: CloudSuspendedHandshakeSocket

    func connect(url: URL, bearerToken: String) async throws -> any CloudTransportSocket {
        socket
    }
}

private final class CloudSuspendedConnectAttempt: @unchecked Sendable {
    private let lock = NSLock()
    private let continuation: AsyncStream<Void>.Continuation
    let stream: AsyncStream<Void>
    private var started = false
    private var cancelled = false

    init() {
        var continuation: AsyncStream<Void>.Continuation!
        stream = AsyncStream { continuation = $0 }
        self.continuation = continuation
    }

    func markStarted() {
        lock.lock()
        started = true
        lock.unlock()
    }

    func cancel() {
        lock.lock()
        cancelled = true
        lock.unlock()
        continuation.finish()
    }

    func state() -> (started: Bool, cancelled: Bool) {
        lock.lock()
        defer { lock.unlock() }
        return (started, cancelled)
    }
}

private struct CloudCancellationCooperativeSuspendedConnector: CloudTransportSocketConnecting {
    let attempt: CloudSuspendedConnectAttempt

    func connect(url: URL, bearerToken: String) async throws -> any CloudTransportSocket {
        attempt.markStarted()
        return try await withTaskCancellationHandler {
            var iterator = attempt.stream.makeAsyncIterator()
            _ = await iterator.next()
            throw CancellationError()
        } onCancel: {
            attempt.cancel()
        }
    }
}

private actor CloudSuspendedTokenProvider: CloudDeviceTokenProviding {
    private let token: CloudDeviceToken
    private var continuation: CheckedContinuation<CloudDeviceToken, Never>?
    private var started = false

    init(token: CloudDeviceToken) {
        self.token = token
    }

    func fetchDeviceToken() async throws -> CloudDeviceToken {
        started = true
        return await withCheckedContinuation { continuation = $0 }
    }

    func hasStarted() -> Bool { started }

    func release() {
        continuation?.resume(returning: token)
        continuation = nil
    }
}

private final class CloudConnectCompletion: @unchecked Sendable {
    private let lock = NSLock()
    private var value = false

    func finish() {
        lock.lock()
        value = true
        lock.unlock()
    }

    func finished() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return value
    }
}

/// A standalone end-to-end test against the loopback relay state machine. The injected in-memory
/// connector is the sandbox fallback for the fake's NWListener WebSocket front end.
func runCloudTransportTests() async throws -> Int {
    switch ProcessInfo.processInfo.environment["CLAWDLINE_CLOUD_TRANSPORT_CASE"] {
    case "owned-connect": return try await runCloudTransportOwnedConnectTests()
    case "connector-cancel": return try await runCloudTransportConnectorCancellationTests()
    case "connector-registration": return try await runCloudTransportConnectorRegistrationTests()
    case "ready-buffer": return try await runCloudTransportReadyBufferTests()
    default: break
    }
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
    let readyGenerations = CloudTestReadyGenerations()
    let readyTask = Task {
        for await generation in transport.readyGenerations {
            await readyGenerations.append(generation)
        }
    }

    try await transport.connect(role: .machine)
    let initialState = await transport.currentState()
    let initialHandshakes = await relay.completedHandshakes()
    let initialTokens = await relay.observedTokens()
    try require(initialState == .ready, "handshake reaches ready")
    try require(initialHandshakes == 1, "relay verifies challenge signature")
    try require(initialTokens == ["machine-token-1"], "Bearer token is presented on upgrade")
    try await waitUntil("initial ready generation is observable") {
        await readyGenerations.all() == [1]
    }
    checks += 1

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
    let readyGenerationsBeforeDrop = await readyGenerations.all().count
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
    try await waitUntil("reconnect ready generation is observable", timeout: 2) {
        await readyGenerations.all().count > readyGenerationsBeforeDrop
    }
    checks += 1

    await transport.shutdown()
    let shutdownState = await transport.currentState()
    try require(shutdownState == .shutDown, "shutdown reaches terminal state")
    let streamEnded = try await nextCommand(from: transport.commands, timeout: 0.5) == nil
    try require(streamEnded, "shutdown finishes inbound stream")
    await readyTask.value
    await relay.stop()

    checks += try await runCloudTransportOwnedConnectTests()
    checks += try await runCloudTransportConnectorCancellationTests()
    checks += try await runCloudTransportConnectorRegistrationTests()
    checks += try await runCloudTransportReadyBufferTests()
    return checks
}

private func runCloudTransportOwnedConnectTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudTransportTestFailure(description: message) }
    }

    let machineKey = CloudDeviceKeyPair()
    let master = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x43, count: 32))
    let tokenProvider = CloudTestTokenProvider(tokens: [
        CloudDeviceToken(value: "suspended-token", expiresAt: Date().addingTimeInterval(60))
    ])
    let socket = CloudSuspendedHandshakeSocket()
    let transport = CloudTransport(
        relayBaseURL: URL(string: "ws://suspended.invalid/v1/connect")!,
        tokenProvider: tokenProvider,
        keyProvider: CloudStaticTransportKeys(
            deviceKey: machineKey, masterSecrets: ["master-1": master], pairedDevices: [:]
        ),
        connector: CloudSuspendedHandshakeConnector(socket: socket)
    )
    let completion = CloudConnectCompletion()
    let connectTask = Task {
        do { try await transport.connect(role: .machine) } catch {}
        completion.finish()
    }
    try await waitUntil("initial challenge receive is suspended") {
        socket.state().receiveStarted
    }
    await transport.shutdown()
    try await waitUntil("shutdown terminates initial connect", timeout: 0.5) {
        completion.finished()
    }
    await connectTask.value
    try require(socket.state().closed,
                "shutdown closes the socket owned by an initial suspended handshake")
    let shutdownState = await transport.currentState()
    try require(shutdownState == .shutDown,
                "initial connect cancellation preserves terminal shutdown state")
    return checks
}

private func runCloudTransportConnectorCancellationTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudTransportTestFailure(description: message) }
    }

    let machineKey = CloudDeviceKeyPair()
    let master = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x45, count: 32))
    let attempt = CloudSuspendedConnectAttempt()
    let transport = CloudTransport(
        relayBaseURL: URL(string: "ws://suspended-connector.invalid/v1/connect")!,
        tokenProvider: CloudTestTokenProvider(tokens: [
            CloudDeviceToken(value: "connector-token", expiresAt: Date().addingTimeInterval(60))
        ]),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: machineKey, masterSecrets: ["master-1": master], pairedDevices: [:]
        ),
        connector: CloudCancellationCooperativeSuspendedConnector(attempt: attempt)
    )
    let completion = CloudConnectCompletion()
    let connectTask = Task {
        do { try await transport.connect(role: .machine) } catch {}
        completion.finish()
    }
    try await waitUntil("connector attempt is suspended") {
        attempt.state().started
    }
    await transport.shutdown()
    try await waitUntil("shutdown cancels and joins the connector attempt", timeout: 0.5) {
        completion.finished()
    }
    await connectTask.value
    try require(attempt.state().cancelled,
                "connector cancellation terminates suspension without an external resume")
    let state = await transport.currentState()
    try require(state == .shutDown,
                "connector cancellation preserves terminal shutdown state")
    return checks
}

private func runCloudTransportConnectorRegistrationTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudTransportTestFailure(description: message) }
    }

    let machineKey = CloudDeviceKeyPair()
    let master = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x46, count: 32))
    let tokenProvider = CloudSuspendedTokenProvider(token: CloudDeviceToken(
        value: "late-secret-after-shutdown", expiresAt: Date().addingTimeInterval(60)
    ))
    let attempt = CloudSuspendedConnectAttempt()
    let transport = CloudTransport(
        relayBaseURL: URL(string: "ws://registration-race.invalid/v1/connect")!,
        tokenProvider: tokenProvider,
        keyProvider: CloudStaticTransportKeys(
            deviceKey: machineKey, masterSecrets: ["master-1": master], pairedDevices: [:]
        ),
        connector: CloudCancellationCooperativeSuspendedConnector(attempt: attempt)
    )
    let completion = CloudConnectCompletion()
    let connectTask = Task {
        do { try await transport.connect(role: .machine) } catch {}
        completion.finish()
    }
    try await waitUntil("token fetch is suspended before connector registration") {
        await tokenProvider.hasStarted()
    }
    await transport.shutdown()
    await tokenProvider.release()
    try await waitUntil("post-shutdown connect attempt terminates", timeout: 0.5) {
        completion.finished() || attempt.state().started
    }
    let attemptState = attempt.state()
    if attemptState.started {
        // Failure cleanup only: the fixed path completes without starting or externally resuming
        // the connector. This prevents the pre-fix red binary from hanging after its assertion.
        attempt.cancel()
    }
    await connectTask.value
    let retainedToken = try reflectedCachedTokenValue(in: transport)
    try require(retainedToken == nil,
                "shutdown does not retain the late token after its fetch resumes")
    try require(!attemptState.started,
                "shutdown prevents a connector from being born after token fetch resumes")
    try require(completion.finished(),
                "post-token cancellation finishes connect without a test-side connector resume")
    let state = await transport.currentState()
    try require(state == .shutDown,
                "post-token cancellation preserves terminal shutdown state")
    return checks
}

private func reflectedCachedTokenValue(in transport: CloudTransport) throws -> String? {
    guard let storage = Mirror(reflecting: transport).children.first(where: {
        $0.label == "cachedToken"
    })?.value else {
        throw CloudTransportTestFailure(description: "CloudTransport cachedToken storage is missing")
    }
    let optional = Mirror(reflecting: storage)
    guard optional.displayStyle == .optional else {
        throw CloudTransportTestFailure(description: "CloudTransport cachedToken storage is not optional")
    }
    guard let value = optional.children.first?.value else { return nil }
    guard let token = value as? CloudDeviceToken else {
        throw CloudTransportTestFailure(description: "CloudTransport cachedToken has an unexpected type")
    }
    return token.value
}

private func runCloudTransportReadyBufferTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudTransportTestFailure(description: message) }
    }

    let machineKey = CloudDeviceKeyPair()
    let master = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x44, count: 32))
    let relay = CloudLoopbackRelay(
        account: "buffer-account", deviceID: "buffer-machine",
        devicePublicKey: machineKey.publicKeyRaw, allowedTokens: ["buffer-token"]
    )
    let transport = CloudTransport(
        relayBaseURL: URL(string: "ws://loopback.invalid/v1/connect")!,
        tokenProvider: CloudTestTokenProvider(tokens: [
            CloudDeviceToken(value: "buffer-token", expiresAt: Date().addingTimeInterval(3_600))
        ]),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: machineKey, masterSecrets: ["master-1": master], pairedDevices: [:]
        ),
        connector: CloudLoopbackSocketConnector(relay: relay),
        initialBackoff: 0.01,
        maximumBackoff: 0.02
    )
    try await transport.connect(role: .machine)
    for wantedHandshakes in 2...3 {
        await relay.dropConnections()
        try await waitUntil("burst reconnect generation \(wantedHandshakes)", timeout: 2) {
            let handshakes = await relay.completedHandshakes()
            let dropped = await transport.droppedReadyGenerationCount()
            return handshakes >= wantedHandshakes && dropped == wantedHandshakes - 1
        }
    }
    var iterator = transport.readyGenerations.makeAsyncIterator()
    let buffered = await iterator.next()
    try require(buffered == 3,
                "bounded ready buffer coalesces a burst to the current generation")
    let dropped = await transport.droppedReadyGenerationCount()
    try require(dropped == 2,
                "ready generation yield reports each superseded buffered generation")
    await transport.shutdown()
    let ended = await iterator.next()
    try require(ended == nil,
                "shutdown finishes a delayed ready-generation iterator")
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
