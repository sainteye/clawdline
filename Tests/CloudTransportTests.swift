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

    func connect(url: URL, bearerToken: String) async throws -> CloudEstablishedTransportSocket {
        CloudEstablishedTransportSocket(socket)
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

    func connect(url: URL, bearerToken: String) async throws -> CloudEstablishedTransportSocket {
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

private final class CloudConnectResult: @unchecked Sendable {
    private let lock = NSLock()
    private var result: Result<Void, Error>?

    func finish(_ result: Result<Void, Error>) {
        lock.lock()
        self.result = result
        lock.unlock()
    }

    func snapshot() -> Result<Void, Error>? {
        lock.lock()
        defer { lock.unlock() }
        return result
    }
}

private final class CloudURLSessionSocketLifecycleProbe: @unchecked Sendable {
    private let lock = NSLock()
    private var resumes = 0
    private var cancellations = 0
    private var invalidations = 0

    func didResume() { lock.lock(); resumes += 1; lock.unlock() }
    func didCancel() { lock.lock(); cancellations += 1; lock.unlock() }
    func didInvalidate() { lock.lock(); invalidations += 1; lock.unlock() }

    func snapshot() -> (resumes: Int, cancellations: Int, invalidations: Int) {
        lock.lock()
        defer { lock.unlock() }
        return (resumes, cancellations, invalidations)
    }
}

private final class CloudURLSessionInvalidationProbe: NSObject, URLSessionDelegate,
    @unchecked Sendable
{
    private let lock = NSLock()
    private var invalidated = false

    func urlSession(_ session: URLSession, didBecomeInvalidWithError error: Error?) {
        lock.lock()
        invalidated = true
        lock.unlock()
    }

    func didInvalidate() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return invalidated
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
    case "reconnect-ownership": return try await runCloudTransportReconnectOwnershipTests()
    case "opening": return try await runCloudTransportOpeningTests()
    case "urlsession": return try await runCloudTransportURLSessionConnectorTests()
    case "unauthorized-upgrade": return try await runCloudTransportUnauthorizedUpgradeTests()
    case "flush-invariant": return try await runCloudTransportFlushInvariantTests()
    case "timeouts": return try await runCloudTransportTimeoutTests()
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

    // The socket this transport closes for the rotation comes back through `receive` as an
    // ordinary error, so without a carried reason the rotation is logged as the relay failing.
    // On 2026-09-03 that produced `reason=connection_failed` once per token lifetime on a Mac
    // that was connected and publishing throughout, and the line was read as an outage.
    let retryLines = logs.lines().filter { $0.contains("CloudTransport reconnect waiting") }
    try require(retryLines.contains { $0.contains("reason=token_rotation") },
                "the reconnect a token rotation causes is logged as a rotation")
    try require(!retryLines.contains { $0.contains("reason=connection_failed") },
                "a rotation this transport asked for is not reported as a failed connection")

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
    checks += try await runCloudTransportReconnectOwnershipTests()
    checks += try await runCloudTransportOpeningTests()
    checks += try await runCloudTransportURLSessionConnectorTests()
    checks += try await runCloudTransportUnauthorizedUpgradeTests()
    checks += try await runCloudTransportFlushInvariantTests()
    checks += try await runCloudTransportTimeoutTests()
    return checks
}

private func runCloudTransportReconnectOwnershipTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() {
            throw CloudTransportTestFailure(
                description: "\(checks - 1) checks passed before: \(message)"
            )
        }
    }

    let probe = CloudReconnectSocketProbe()
    let clock = CloudReconnectProbeClock()
    let logs = CloudTestLog()
    let transport = CloudTransport(
        relayBaseURL: URL(string: "ws://reconnect-probe.invalid/v1/connect")!,
        tokenProvider: CloudTestTokenProvider(tokens: [
            CloudDeviceToken(value: "probe-token", expiresAt: Date().addingTimeInterval(3_600))
        ]),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: CloudDeviceKeyPair(), masterSecrets: [:], pairedDevices: [:]
        ),
        clock: clock,
        connector: CloudReconnectProbeConnector(probe: probe),
        initialBackoff: 1,
        maximumBackoff: 8,
        logger: { logs.append($0) }
    )
    try await transport.connect(role: .machine)
    try await waitUntil("three reconnect delays are observable") {
        await clock.recordedSleeps().count >= 3
    }
    let sockets = probe.snapshot()
    let sleeps = await clock.recordedSleeps()
    await transport.shutdown()
    let afterTeardown = probe.snapshot()
    try require(afterTeardown.live == 0 && afterTeardown.closed == afterTeardown.opened,
                "teardown disposes every socket created across reconnect generations")
    try require(sockets.peak == 1,
                "a reconnect closes its predecessor before opening a successor")
    try require(sockets.opened == 3 && sockets.closed == 3 && sockets.live == 0,
                "every failed reconnect generation is explicitly disposed")
    try require(sleeps.prefix(3).elementsEqual([1, 2, 4]),
                "short-lived successful handshakes do not reset exponential backoff")
    try require(logs.lines().prefix(3).elementsEqual([
        "CloudTransport reconnect waiting reason=not_connected retry_in_ms=1000",
        "CloudTransport reconnect waiting reason=not_connected retry_in_ms=2000",
        "CloudTransport reconnect waiting reason=not_connected retry_in_ms=4000",
    ]), "every retry logs a typed reason and its backoff rhythm")

    let activeProbe = CloudReconnectSocketProbe()
    let activeTransport = CloudTransport(
        relayBaseURL: URL(string: "ws://teardown-probe.invalid/v1/connect")!,
        tokenProvider: CloudTestTokenProvider(tokens: [
            CloudDeviceToken(value: "teardown-token", expiresAt: Date().addingTimeInterval(3_600))
        ]),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: CloudDeviceKeyPair(), masterSecrets: [:], pairedDevices: [:]
        ),
        connector: CloudReconnectProbeConnector(
            probe: activeProbe, dropsAfterAuthentication: false
        )
    )
    try await activeTransport.connect(role: .machine)
    try require(activeProbe.snapshot().live == 1,
                "the teardown probe observes the authenticated live socket")
    await activeTransport.shutdown()
    try require(activeProbe.snapshot().live == 0,
                "shutdown closes a healthy authenticated socket")

    let unexpectedProbe = CloudReconnectSocketProbe()
    let unexpectedClock = CloudReconnectProbeClock()
    let unexpectedTransport = CloudTransport(
        relayBaseURL: URL(string: "ws://unexpected-frame.invalid/v1/connect")!,
        tokenProvider: CloudTestTokenProvider(tokens: [
            CloudDeviceToken(value: "unexpected-token", expiresAt: Date().addingTimeInterval(3_600))
        ]),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: CloudDeviceKeyPair(), masterSecrets: [:], pairedDevices: [:]
        ),
        clock: unexpectedClock,
        connector: CloudReconnectProbeConnector(
            probe: unexpectedProbe, behavior: .unexpectedFrame
        ),
        initialBackoff: 1,
        maximumBackoff: 8
    )
    try await unexpectedTransport.connect(role: .machine)
    try await waitUntil("unexpected frames drive three reconnect attempts") {
        await unexpectedClock.recordedSleeps().count >= 3
    }
    let unexpectedSockets = unexpectedProbe.snapshot()
    await unexpectedTransport.shutdown()
    try require(
        unexpectedSockets.opened == 3 && unexpectedSockets.closed == 3
            && unexpectedSockets.live == 0 && unexpectedSockets.peak == 1,
        "a frame rejected by handle disposes every authenticated predecessor"
    )

    let handledProbe = CloudReconnectSocketProbe()
    let handledClock = CloudReconnectProbeClock()
    let handledTransport = CloudTransport(
        relayBaseURL: URL(string: "ws://handled-frame.invalid/v1/connect")!,
        tokenProvider: CloudTestTokenProvider(tokens: [
            CloudDeviceToken(value: "handled-token", expiresAt: Date().addingTimeInterval(3_600))
        ]),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: CloudDeviceKeyPair(), masterSecrets: [:], pairedDevices: [:]
        ),
        clock: handledClock,
        connector: CloudReconnectProbeConnector(
            probe: handledProbe, behavior: .handledFrameThenDisconnect
        ),
        initialBackoff: 1,
        maximumBackoff: 8
    )
    try await handledTransport.connect(role: .machine)
    try await waitUntil("handled frames drive three reconnect attempts") {
        await handledClock.recordedSleeps().count >= 3
    }
    let handledSleeps = await handledClock.recordedSleeps()
    await handledTransport.shutdown()
    try require(handledSleeps.prefix(3).elementsEqual([1, 2, 4]),
                "a short-lived connection cannot reset backoff merely by handling a frame")

    let stableProbe = CloudReconnectSocketProbe()
    let stableClock = CloudReconnectProbeClock()
    let stableTransport = CloudTransport(
        relayBaseURL: URL(string: "ws://stable-connection.invalid/v1/connect")!,
        tokenProvider: CloudTestTokenProvider(tokens: [
            CloudDeviceToken(value: "stable-token", expiresAt: Date().addingTimeInterval(3_600))
        ]),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: CloudDeviceKeyPair(), masterSecrets: [:], pairedDevices: [:]
        ),
        clock: stableClock,
        connector: CloudReconnectSequenceConnector(
            probe: stableProbe,
            behaviors: [
                .disconnectAfterAuthentication, .handledFrameThenDisconnect, .stayConnected,
            ],
            onHandledFrame: { await stableClock.advance(by: 31) }
        ),
        initialBackoff: 1,
        maximumBackoff: 8,
        backoffResetAfter: 30
    )
    try await stableTransport.connect(role: .machine)
    try await waitUntil("a stable connection reaches its next reconnect") {
        await stableClock.recordedSleeps().count >= 2
    }
    let stableSleeps = await stableClock.recordedSleeps()
    await stableTransport.shutdown()
    try require(stableSleeps.prefix(2).elementsEqual([1, 1]),
                "a connection resets backoff only after lasting beyond the stability threshold")
    return checks
}

private func runCloudTransportOpeningTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudTransportTestFailure(description: message) }
    }

    let observer = CloudWebSocketOpenObserver()
    let pending = CloudConnectResult()
    let wait = Task {
        do {
            try await observer.waitUntilOpen(timeout: 1)
            pending.finish(.success(()))
        } catch {
            pending.finish(.failure(error))
        }
    }
    try await Task.sleep(nanoseconds: 20_000_000)
    try require(pending.snapshot() == nil,
                "a started WebSocket does not report established before didOpen")
    observer.opened()
    await wait.value
    if case .success? = pending.snapshot() {
        checks += 1
    } else {
        throw CloudTransportTestFailure(description: "didOpen completes the established socket wait")
    }

    let refused = CloudWebSocketOpenObserver()
    let refusalTask = Task { try await refused.waitUntilOpen(timeout: 1) }
    refused.failed(statusCode: 401, error: nil)
    do {
        try await refusalTask.value
        throw CloudTransportTestFailure(description: "a 401 upgrade must not produce a socket")
    } catch let error as CloudTransportError {
        try require(error == .unauthorized,
                    "a 401 upgrade surfaces as the typed unauthorized refusal")
    }

    let stalled = CloudWebSocketOpenObserver()
    do {
        try await stalled.waitUntilOpen(timeout: 0.02)
        throw CloudTransportTestFailure(description: "a stalled upgrade must time out")
    } catch let error as CloudTransportError {
        try require(error == .connectionTimedOut,
                    "the WebSocket opening wait has a typed deadline")
    }
    return checks
}

private func runCloudTransportURLSessionConnectorTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() {
            throw CloudTransportTestFailure(
                description: "\(checks - 1) checks passed before: \(message)"
            )
        }
    }

    let starter = CloudURLSessionConnectorProbe(outcome: .opens)
    let openingTimeout: TimeInterval = 0.02
    let connector = CloudURLSessionSocketConnector(
        openingTimeout: openingTimeout, starter: starter
    )
    let transport = CloudTransport(
        relayBaseURL: URL(string: "ws://urlsession-probe.invalid/v1/connect")!,
        tokenProvider: CloudTestTokenProvider(tokens: [
            CloudDeviceToken(value: "urlsession-token", expiresAt: Date().addingTimeInterval(3_600))
        ]),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: CloudDeviceKeyPair(), masterSecrets: [:], pairedDevices: [:]
        ),
        connector: connector,
        receiveTimeout: 1
    )
    try await transport.connect(role: .machine)
    let configuration = starter.snapshot()
    try require(configuration.authorization == "Bearer urlsession-token",
                "the production connector applies its bearer token")
    try require(!configuration.waitsForConnectivity,
                "the production connector fails a blocked opening attempt promptly")
    try require(configuration.requestTimeout == openingTimeout,
                "the URLSession request timeout matches only the opening deadline")
    try require(configuration.resourceTimeout > openingTimeout,
                "the URLSession resource lifetime is independent of the opening deadline")
    try await Task.sleep(nanoseconds: 60_000_000)
    try require(starter.snapshot().socketsClosed == 0,
                "an authenticated URLSession socket outlives its opening deadline")
    let longLivedState = await transport.currentState()
    try require(longLivedState == .ready,
                "the authenticated URLSession-backed transport remains ready")
    await transport.shutdown()

    let lifecycle = CloudURLSessionSocketLifecycleProbe()
    let socket = CloudURLSessionSocket(
        resume: { lifecycle.didResume() },
        send: { _ in },
        receive: { throw CloudTransportError.notConnected },
        cancel: { lifecycle.didCancel() },
        invalidate: { lifecycle.didInvalidate() }
    )
    socket.resume()
    socket.close()
    let lifecycleState = lifecycle.snapshot()
    try require(lifecycleState.resumes == 1,
                "the owned URLSession socket starts its task exactly once")
    try require(lifecycleState.cancellations == 1,
                "closing the owned URLSession socket cancels its task")
    let invalidationProbe = CloudURLSessionInvalidationProbe()
    let actualSession = URLSession(
        configuration: .ephemeral, delegate: invalidationProbe, delegateQueue: nil
    )
    let actualTask = actualSession.webSocketTask(
        with: URL(string: "ws://urlsession-invalidation.invalid/v1/connect")!
    )
    CloudURLSessionSocket(session: actualSession, task: actualTask).close()
    try await waitUntil("owned URLSession invalidates") { invalidationProbe.didInvalidate() }
    try require(invalidationProbe.didInvalidate(),
                "closing the owned URLSession socket invalidates its private session")

    let stalledStarter = CloudURLSessionConnectorProbe(outcome: .stalls)
    do {
        _ = try await CloudURLSessionSocketConnector(
            openingTimeout: 0.02, starter: stalledStarter
        ).connect(url: URL(string: "ws://stalled.invalid/v1/connect")!, bearerToken: "stalled")
        throw CloudTransportTestFailure(description: "a stalled connector unexpectedly opened")
    } catch let error as CloudTransportError {
        try require(error == .connectionTimedOut,
                    "the production connector preserves its typed opening timeout")
    }
    try require(stalledStarter.snapshot().closed,
                "an opening failure disposes the started URLSession socket")

    let unauthorizedStarter = CloudURLSessionConnectorProbe(outcome: .unauthorized)
    do {
        _ = try await CloudURLSessionSocketConnector(
            openingTimeout: 1, starter: unauthorizedStarter
        ).connect(url: URL(string: "ws://unauthorized.invalid/v1/connect")!, bearerToken: "refused")
        throw CloudTransportTestFailure(description: "an unauthorized connector unexpectedly opened")
    } catch let error as CloudTransportError {
        try require(error == .unauthorized,
                    "the production connector preserves a typed unauthorized upgrade")
    }
    try require(unauthorizedStarter.snapshot().closed,
                "an upgrade refusal disposes the started URLSession socket")

    let cancelledStarter = CloudURLSessionConnectorProbe(outcome: .stalls)
    let cancelledConnector = CloudURLSessionSocketConnector(
        openingTimeout: 1, starter: cancelledStarter
    )
    let cancelledConnect = Task {
        try await cancelledConnector.connect(
            url: URL(string: "ws://cancelled.invalid/v1/connect")!, bearerToken: "cancelled"
        )
    }
    try await waitUntil("production connector task starts") {
        cancelledStarter.snapshot().resumed
    }
    cancelledConnect.cancel()
    do {
        _ = try await cancelledConnect.value
        throw CloudTransportTestFailure(description: "a cancelled connector unexpectedly opened")
    } catch is CancellationError {
        checks += 1
    }
    try require(cancelledStarter.snapshot().closed,
                "cancelling a connection attempt disposes its started URLSession socket")
    return checks
}

private func runCloudTransportUnauthorizedUpgradeTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() {
            throw CloudTransportTestFailure(
                description: "\(checks - 1) checks passed before: \(message)"
            )
        }
    }

    let probe = CloudReconnectSocketProbe()
    let connector = CloudUnauthorizedReconnectConnector(probe: probe)
    let clock = CloudReconnectProbeClock()
    let logs = CloudTestLog()
    let transport = CloudTransport(
        relayBaseURL: URL(string: "ws://unauthorized-reconnect.invalid/v1/connect")!,
        tokenProvider: CloudTestTokenProvider(tokens: [
            CloudDeviceToken(value: "cached-refused-token", expiresAt: Date().addingTimeInterval(3_600)),
            CloudDeviceToken(value: "refreshed-token", expiresAt: Date().addingTimeInterval(3_600)),
        ]),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: CloudDeviceKeyPair(), masterSecrets: [:], pairedDevices: [:]
        ),
        clock: clock,
        connector: connector,
        initialBackoff: 1,
        maximumBackoff: 8,
        logger: { logs.append($0) }
    )
    try await transport.connect(role: .machine)
    try await waitUntil("unauthorized reconnect fetches a replacement token") {
        connector.observedTokens().count >= 3
    }
    let tokens = connector.observedTokens()
    try require(logs.lines().contains { $0.contains("reason=unauthorized") },
                "an unauthorized upgrade keeps its typed reconnect reason")
    try require(tokens.prefix(3).elementsEqual([
        "cached-refused-token", "cached-refused-token", "refreshed-token",
    ]), "an unauthorized upgrade invalidates the cached token before retrying")
    await transport.shutdown()
    return checks
}

private func runCloudTransportFlushInvariantTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() {
            throw CloudTransportTestFailure(
                description: "\(checks - 1) checks passed before: \(message)"
            )
        }
    }

    let machineKey = CloudDeviceKeyPair()
    let master = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x47, count: 32))
    let tokenProvider = CloudSuspendedTokenProvider(token: CloudDeviceToken(
        value: "flush-token", expiresAt: Date().addingTimeInterval(3_600)
    ))
    let probe = CloudReconnectSocketProbe()
    let transport = CloudTransport(
        relayBaseURL: URL(string: "ws://flush-failure.invalid/v1/connect")!,
        tokenProvider: tokenProvider,
        keyProvider: CloudStaticTransportKeys(
            deviceKey: machineKey,
            masterSecrets: ["master-1": master],
            pairedDevices: [:]
        ),
        connector: CloudReconnectProbeConnector(
            probe: probe, behavior: .failPendingPublish
        )
    )
    let connectTask = Task { try await transport.connect(role: .machine) }
    try await waitUntil("token fetch suspends before the flush probe connects") {
        await tokenProvider.hasStarted()
    }
    let queued = try CloudEnvelope.seal(
        Data("queued-before-connect".utf8),
        ch: "s/flush/session",
        seq: 1,
        ts: millisecondsNow(),
        envelopeClass: .stream,
        keyID: "master-1",
        sender: "flush-machine",
        masterSecret: master,
        signingKey: machineKey
    )
    try await transport.publish(envelope: queued)
    await tokenProvider.release()
    do {
        try await connectTask.value
        throw CloudTransportTestFailure(description: "a failing pending flush unexpectedly connected")
    } catch let error as CloudTransportError {
        try require(error == .notConnected,
                    "a pending flush reports the socket's typed send failure")
    }
    let state = await transport.currentState()
    try require(state == .idle,
                "a failed initial pending flush restores the transport to idle")
    let socketIsNil = try reflectedSocketIsNil(in: transport)
    try require(socketIsNil,
                "a failed pending flush cannot leave a closed authenticated socket installed")
    try require(probe.snapshot().live == 0,
                "a failed pending flush disposes the candidate socket")
    await transport.shutdown()
    return checks
}

private func runCloudTransportTimeoutTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudTransportTestFailure(description: message) }
    }

    let suspended = CloudSuspendedHandshakeSocket()
    let handshakeTransport = CloudTransport(
        relayBaseURL: URL(string: "ws://handshake-timeout.invalid/v1/connect")!,
        tokenProvider: CloudTestTokenProvider(tokens: [
            CloudDeviceToken(value: "handshake-token", expiresAt: Date().addingTimeInterval(60))
        ]),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: CloudDeviceKeyPair(), masterSecrets: [:], pairedDevices: [:]
        ),
        connector: CloudSuspendedHandshakeConnector(socket: suspended),
        authenticationTimeout: 0.02
    )
    do {
        try await handshakeTransport.connect(role: .machine)
        throw CloudTransportTestFailure(description: "a suspended authentication must time out")
    } catch let error as CloudTransportError {
        try require(error == .authenticationTimedOut,
                    "challenge and ready waits have a typed authentication deadline")
    }
    try require(suspended.state().closed,
                "authentication timeout closes its established socket")
    await handshakeTransport.shutdown()

    let receiveProbe = CloudReconnectSocketProbe()
    let receiveLogs = CloudTestLog()
    let receiveTransport = CloudTransport(
        relayBaseURL: URL(string: "ws://receive-timeout.invalid/v1/connect")!,
        tokenProvider: CloudTestTokenProvider(tokens: [
            CloudDeviceToken(value: "receive-token", expiresAt: Date().addingTimeInterval(3_600))
        ]),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: CloudDeviceKeyPair(), masterSecrets: [:], pairedDevices: [:]
        ),
        connector: CloudReconnectProbeConnector(
            probe: receiveProbe, dropsAfterAuthentication: false
        ),
        initialBackoff: 1,
        receiveTimeout: 0.02,
        logger: { receiveLogs.append($0) }
    )
    try await receiveTransport.connect(role: .machine)
    try await waitUntil("receive timeout reaches reconnect backoff") {
        receiveLogs.lines().contains {
            $0.contains("reason=receive_timeout") && $0.contains("retry_in_ms=")
        }
    }
    try require(receiveProbe.snapshot().live == 0,
                "receive timeout closes the socket before reconnect backoff")
    await receiveTransport.shutdown()
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

private func reflectedSocketIsNil(in transport: CloudTransport) throws -> Bool {
    guard let storage = Mirror(reflecting: transport).children.first(where: {
        $0.label == "socket"
    })?.value else {
        throw CloudTransportTestFailure(description: "CloudTransport socket storage is missing")
    }
    let optional = Mirror(reflecting: storage)
    guard optional.displayStyle == .optional else {
        throw CloudTransportTestFailure(description: "CloudTransport socket storage is not optional")
    }
    return optional.children.isEmpty
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
