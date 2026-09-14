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

private final class CloudInboundRefusalRecorder: @unchecked Sendable {
    private let lock = NSLock()
    private var values: [CloudInboundAdmissionRefusal] = []

    func append(_ refusal: CloudInboundAdmissionRefusal) {
        lock.lock()
        values.append(refusal)
        lock.unlock()
    }

    func all() -> [CloudInboundAdmissionRefusal] {
        lock.lock()
        defer { lock.unlock() }
        return values
    }
}

private final class CloudTerminalAuthorizationRecorder: @unchecked Sendable {
    private let lock = NSLock()
    private var values: [CloudTransportError] = []

    func append(_ error: CloudTransportError) {
        lock.lock()
        values.append(error)
        lock.unlock()
    }

    func all() -> [CloudTransportError] {
        lock.lock()
        defer { lock.unlock() }
        return values
    }
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

private final class CloudOpeningBudgetClock: CloudTransportClock, @unchecked Sendable {
    private struct Waiter {
        let deadline: TimeInterval
        let continuation: CheckedContinuation<Void, Error>
    }
    private let lock = NSLock()
    private var monotonic: TimeInterval
    private var waiters: [UUID: Waiter] = [:]

    init(monotonic: TimeInterval = 100) { self.monotonic = monotonic }

    func now() async -> Date { Date(timeIntervalSince1970: 1_800_000_000) }
    func monotonicNow() async -> TimeInterval { currentMonotonic() }
    func jitterUnit() async -> Double { 0.5 }
    func sleep(for seconds: TimeInterval) async throws {
        try await Task.sleep(nanoseconds: UInt64(max(0, seconds) * 1_000_000_000))
    }
    func waitUntilMonotonic(_ deadline: TimeInterval) async throws {
        let id = UUID()
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
                lock.lock()
                if Task.isCancelled {
                    lock.unlock(); continuation.resume(throwing: CancellationError()); return
                }
                if monotonic >= deadline {
                    lock.unlock(); continuation.resume(); return
                }
                waiters[id] = Waiter(deadline: deadline, continuation: continuation)
                lock.unlock()
            }
        } onCancel: { [weak self] in self?.cancel(id) }
    }
    func advance(by seconds: TimeInterval) {
        lock.lock()
        monotonic += seconds
        let due = waiters.filter { $0.value.deadline <= monotonic }
        for id in due.keys { waiters.removeValue(forKey: id) }
        lock.unlock()
        for waiter in due.values { waiter.continuation.resume() }
    }
    private func currentMonotonic() -> TimeInterval {
        lock.lock(); defer { lock.unlock() }; return monotonic
    }
    private func cancel(_ id: UUID) {
        lock.lock(); let waiter = waiters.removeValue(forKey: id); lock.unlock()
        waiter?.continuation.resume(throwing: CancellationError())
    }
}

private actor CloudAdvancingTokenProvider: CloudDeviceTokenProviding {
    let clock: CloudOpeningBudgetClock
    let advance: TimeInterval
    let token: CloudDeviceToken
    init(clock: CloudOpeningBudgetClock, advance: TimeInterval, token: CloudDeviceToken) {
        self.clock = clock; self.advance = advance; self.token = token
    }
    func fetchDeviceToken() async throws -> CloudDeviceToken {
        clock.advance(by: advance)
        return token
    }
}

private actor CloudCancellableBudgetTokenProvider: CloudDeviceTokenProviding {
    let token: CloudDeviceToken
    private var started = false
    private var cancelled = false
    init(token: CloudDeviceToken) { self.token = token }
    func fetchDeviceToken() async throws -> CloudDeviceToken {
        started = true
        do {
            try await Task.sleep(nanoseconds: 3_600_000_000_000)
            return token
        } catch {
            cancelled = true
            throw error
        }
    }
    func snapshot() -> (started: Bool, cancelled: Bool) { (started, cancelled) }
}

private final class CloudOpeningBudgetConnector: CloudTransportSocketConnecting,
    @unchecked Sendable {
    private let lock = NSLock()
    private let base: CloudReconnectProbeConnector
    private var budgets: [TimeInterval] = []
    init(probe: CloudReconnectSocketProbe) {
        base = CloudReconnectProbeConnector(probe: probe, behavior: .stayConnected)
    }
    func connect(url: URL, bearerToken: String) async throws -> CloudEstablishedTransportSocket {
        try await connect(url: url, bearerToken: bearerToken, openingTimeout: 15)
    }
    func connect(
        url: URL, bearerToken: String, openingTimeout: TimeInterval
    ) async throws -> CloudEstablishedTransportSocket {
        lock.lock(); budgets.append(openingTimeout); lock.unlock()
        return try await base.connect(url: url, bearerToken: bearerToken)
    }
    func observedBudgets() -> [TimeInterval] {
        lock.lock(); defer { lock.unlock() }; return budgets
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
    case "inbound-budget": return try await runCloudTransportInboundBudgetTests()
    case "transparency": return try await runCloudTransportTransparencyTests()
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
    let snapshotReceiptTask = Task {
        try await nextOutboundReceipt(from: transport.outboundReceipts)
    }
    try await transport.publish(envelope: snapshot)
    try await waitUntil("relay receives published snapshot") {
        await relay.publishedEnvelopes().contains(snapshot)
    }
    checks += 1
    let snapshotReceipt = try await snapshotReceiptTask.value
    try require(snapshotReceipt == CloudOutboundTransportReceipt(
        channel: snapshot.ch, sequence: Int64(snapshot.seq), kind: .delivered),
        "an authenticated ack exposes the correlated channel and sequence")

    let rejectionTask = Task {
        try await nextOutboundReceipt(from: transport.outboundReceipts)
    }
    try await transport.handleAuthenticatedFrameForTesting(
        "{\"type\":\"publish_error\",\"ch\":\"s/machine-1/session-1\",\"seq\":1,"
            + "\"code\":\"clock_skew\",\"field\":\"ts\"}")
    let rejection = try await rejectionTask.value
    try require(rejection == CloudOutboundTransportReceipt(
        channel: snapshot.ch, sequence: 1,
        kind: .peerRejected(CloudOutboundPeerRejection(
            code: .clockSkew, field: .timestamp, disposition: .retryNewAttempt))),
        "publish_error exposes closed code/field and retry-new-attempt disposition")
    let futureRejectionTask = Task {
        try await nextOutboundReceipt(from: transport.outboundReceipts)
    }
    try await transport.handleAuthenticatedFrameForTesting(
        "{\"type\":\"publish_error\",\"ch\":\"s/machine-1/session-1\",\"seq\":1,"
            + "\"code\":\"future_code\",\"field\":\"future_field\"}")
    let futureRejection = try await futureRejectionTask.value
    try require(futureRejection?.kind == .peerRejected(CloudOutboundPeerRejection(
        code: .unknown, field: .unknown, disposition: .terminal)),
        "unknown publish_error code is terminal and cannot opt an old client into replay")
    try await transport.handleAuthenticatedFrameForTesting(
        "{\"type\":\"error\",\"code\":\"malformed_envelope\",\"message\":\"redacted\"}")
    try await transport.handleAuthenticatedFrameForTesting("{\"type\":\"future_frame\"}")
    let stateAfterNonfatalFrames = await transport.currentState()
    try require(stateAfterNonfatalFrames == .ready,
                "uncorrelated error and unknown authenticated frame do not discard the socket")

    // The exact receipt assertion above independently proves authenticated parsing. Drive the
    // shared actor-isolated offer seam on a fresh, unopened transport so neither an iterator nor
    // socket/handshake scheduling can become the quantity under test.
    let receiptTransport = CloudTransport(
        relayBaseURL: relayURL,
        tokenProvider: CloudTestTokenProvider(tokens: [
            CloudDeviceToken(value: "unused-receipt-token", expiresAt: Date().addingTimeInterval(3_600))
        ]),
        keyProvider: keys,
        connector: CloudSuspendedHandshakeConnector(socket: CloudSuspendedHandshakeSocket()),
        logger: { logs.append($0) })
    for offset in 0..<257 {
        await receiptTransport.offerOutboundReceiptForTesting(CloudOutboundTransportReceipt(
            channel: snapshot.ch, sequence: Int64(10_000 + offset), kind: .delivered))
    }
    let receiptIngestion = await receiptTransport.outboundReceiptIngestionSnapshot()
    try require(receiptIngestion.enqueued == 256 && receiptIngestion.dropped == 1
                    && receiptIngestion.terminated == 0,
                "receipt ingestion retains 256 oldest observations and exposes one typed drop; "
                    + "observed enqueued=\(receiptIngestion.enqueued) "
                    + "dropped=\(receiptIngestion.dropped) "
                    + "terminated=\(receiptIngestion.terminated)")
    let connectedTransportState = await transport.currentState()
    try require(connectedTransportState == .ready,
                "receipt overflow is isolated from the authenticated connection")
    await receiptTransport.shutdown()

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
    let dropLogs = logs.lines().filter { $0.hasPrefix("refusal layer=mac_transport ") }
    try require(dropLogs.count == 2, "each rejected envelope has one refusal line")
    try require(dropLogs.first?.contains("code=unknown_sender sender=unknown-device seq=2 ") == true
                    && dropLogs.last?.contains("code=bad_signature sender=viewer-device seq=3 ") == true,
                "each drop line names its own code and the envelope's outside reference")
    try require(dropLogs.allSatisfy { !$0.contains(command.ct) && !$0.contains(forged.ct) },
                "drop logs contain no envelope content")

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
    do {
        try await transport.publish(envelope: queuedSnapshot)
    } catch let error as CloudTransportError {
        try require(error == .notConnected,
                    "a reconnecting transport refuses instead of owning a pending frame")
    }
    try await waitUntil("reconnect completes before the durable owner retries", timeout: 2) {
        await relay.completedHandshakes() > handshakesBeforeDrop
    }
    checks += 1
    try await transport.publish(envelope: queuedSnapshot)
    try await waitUntil("the caller retry publishes after ready", timeout: 2) {
        await relay.publishedEnvelopes().contains(queuedSnapshot)
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
    checks += try await runCloudTransportInboundBudgetTests()
    checks += try await runCloudTransportTransparencyTests()
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
    try await Task.sleep(nanoseconds: 30_000_000)
    let unexpectedSockets = unexpectedProbe.snapshot()
    let unexpectedSleeps = await unexpectedClock.recordedSleeps()
    await unexpectedTransport.shutdown()
    try require(
        unexpectedSockets.opened == 1 && unexpectedSockets.live == 1
            && unexpectedSockets.peak == 1 && unexpectedSleeps.isEmpty,
        "an unsupported authenticated frame is ignored without entering reconnect"
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
    let actualSocket = CloudURLSessionSocket(session: actualSession, task: actualTask)
    let maximumMessageSize = actualTask.maximumMessageSize
    actualSocket.close()
    try require(maximumMessageSize == 32 * 1024 * 1024,
                "the production WebSocket admits every frame the Cloud relay may forward")
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
        connector: connector,
        initialBackoff: 0.01,
        maximumBackoff: 0.01,
        logger: { logs.append($0) }
    )
    let terminal = CloudTerminalAuthorizationRecorder()
    await transport.setTerminalAuthorizationHandler { error in
        terminal.append(error)
    }
    try await transport.connect(role: .machine)
    try await Task.sleep(nanoseconds: 200_000_000)
    let tokens = connector.observedTokens()
    let terminalErrors = terminal.all()
    let finalState = await transport.currentState()
    try require(terminalErrors == [.unauthorized],
                "unauthorized reconnect reaches the terminal owner; tokens=\(tokens) "
                    + "state=\(finalState) logs=\(logs.lines())")
    try require(tokens == ["cached-refused-token", "cached-refused-token"],
                "an unauthorized WebSocket upgrade is terminal and never reconnects")
    try require(finalState == .idle,
                "terminal authorization refusal leaves no reconnecting transport")
    try require(!logs.lines().contains { $0.contains("reconnect waiting reason=unauthorized") },
                "terminal authorization refusal never enters reconnect backoff")
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
        connector: CloudReconnectProbeConnector(probe: probe, behavior: .stayConnected)
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
    do {
        try await transport.publish(envelope: queued)
        throw CloudTransportTestFailure(
            description: "a connecting transport unexpectedly accepted a pending frame")
    } catch let error as CloudTransportError {
        try require(error == .notConnected,
                    "a connecting transport refuses a frame instead of buffering it")
    }
    await tokenProvider.release()
    try await connectTask.value
    let state = await transport.currentState()
    try require(state == .ready,
                "refusing a pre-ready frame does not poison later connection readiness")
    let socketIsNil = try reflectedSocketIsNil(in: transport)
    try require(!socketIsNil,
                "the authenticated socket remains installed after pending ownership is refused")
    try require(probe.snapshot().live == 1,
                "only the live authenticated socket remains owned")
    await transport.shutdown()
    return checks
}

private func runCloudTransportTimeoutTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudTransportTestFailure(description: message) }
    }

    let responseURL = URL(string: "https://api.invalid/v1/cloud/device-token")!
    for status in [200, 500] {
        var early = CloudBoundedHTTPAccumulator(maximumBytes: 8)
        let declaredLarge = HTTPURLResponse(
            url: responseURL, statusCode: status, httpVersion: "HTTP/1.1",
            headerFields: ["Content-Length": "9"])!
        try require(!early.accept(declaredLarge),
                    "a declared oversized \(status) token response is refused before streaming")

        var streamed = CloudBoundedHTTPAccumulator(maximumBytes: 8)
        let unknownLength = HTTPURLResponse(
            url: responseURL, statusCode: status, httpVersion: "HTTP/1.1",
            headerFields: nil)!
        try require(streamed.accept(unknownLength),
                    "an unknown-length \(status) response enters the bounded accumulator")
        try require(streamed.append(Data(repeating: 0x61, count: 4))
                        && streamed.append(Data(repeating: 0x62, count: 4)),
                    "a trickled \(status) response may fill the exact byte ceiling")
        try require(!streamed.append(Data([0x63])),
                    "a trickled \(status) response fails on the first byte over the ceiling")
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

    let budgetToken = CloudDeviceToken(
        value: "opening-budget-token", expiresAt: Date(timeIntervalSince1970: 1_900_000_000))
    let budgetClock = CloudOpeningBudgetClock()
    let budgetProbe = CloudReconnectSocketProbe()
    let budgetConnector = CloudOpeningBudgetConnector(probe: budgetProbe)
    let budgetTransport = CloudTransport(
        relayBaseURL: URL(string: "ws://opening-budget.invalid/v1/connect")!,
        tokenProvider: CloudAdvancingTokenProvider(
            clock: budgetClock, advance: 6, token: budgetToken),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: CloudDeviceKeyPair(), masterSecrets: [:], pairedDevices: [:]),
        clock: budgetClock, connector: budgetConnector, openingTimeout: 15)
    try await budgetTransport.connect(role: .machine)
    guard let passedBudget = budgetConnector.observedBudgets().first else {
        throw CloudTransportTestFailure(description: "connector did not receive its remaining budget")
    }
    checks += 1
    try require(abs(passedBudget - 9) < 0.001,
                "token acquisition consumes the same absolute 15-second opening budget")
    await budgetTransport.shutdown()

    let expiredClock = CloudOpeningBudgetClock()
    let expiredConnector = CloudOpeningBudgetConnector(probe: CloudReconnectSocketProbe())
    let expiredTransport = CloudTransport(
        relayBaseURL: URL(string: "ws://expired-budget.invalid/v1/connect")!,
        tokenProvider: CloudAdvancingTokenProvider(
            clock: expiredClock, advance: 15, token: budgetToken),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: CloudDeviceKeyPair(), masterSecrets: [:], pairedDevices: [:]),
        clock: expiredClock, connector: expiredConnector, openingTimeout: 15)
    do {
        try await expiredTransport.connect(role: .machine)
        throw CloudTransportTestFailure(description: "an exhausted token budget opened a socket")
    } catch let error as CloudTransportError {
        try require(error == .connectionTimedOut,
                    "an exhausted token budget fails before opening the connector")
    }
    try require(expiredConnector.observedBudgets().isEmpty,
                "no connector starts after the absolute opening deadline")
    await expiredTransport.shutdown()

    let stalledClock = CloudOpeningBudgetClock()
    let stalledToken = CloudCancellableBudgetTokenProvider(token: budgetToken)
    let stalledConnector = CloudOpeningBudgetConnector(probe: CloudReconnectSocketProbe())
    let stalledTransport = CloudTransport(
        relayBaseURL: URL(string: "ws://stalled-token-budget.invalid/v1/connect")!,
        tokenProvider: stalledToken,
        keyProvider: CloudStaticTransportKeys(
            deviceKey: CloudDeviceKeyPair(), masterSecrets: [:], pairedDevices: [:]),
        clock: stalledClock, connector: stalledConnector, openingTimeout: 15)
    let stalledConnect = Task { try await stalledTransport.connect(role: .machine) }
    try await waitUntil("bounded token acquisition starts") {
        await stalledToken.snapshot().started
    }
    stalledClock.advance(by: 15)
    do {
        try await stalledConnect.value
        throw CloudTransportTestFailure(description: "a stalled token fetch exceeded its budget")
    } catch let error as CloudTransportError {
        try require(error == .connectionTimedOut,
                    "a stalled token fetch terminates at the shared opening deadline")
    }
    try await waitUntil("deadline cancellation reaches the token request") {
        await stalledToken.snapshot().cancelled
    }
    try require(stalledConnector.observedBudgets().isEmpty,
                "a token timeout never starts a relay connector")
    await stalledTransport.shutdown()

    let cancelledClock = CloudOpeningBudgetClock()
    let cancelledToken = CloudCancellableBudgetTokenProvider(token: budgetToken)
    let cancelledTransport = CloudTransport(
        relayBaseURL: URL(string: "ws://cancelled-token-budget.invalid/v1/connect")!,
        tokenProvider: cancelledToken,
        keyProvider: CloudStaticTransportKeys(
            deviceKey: CloudDeviceKeyPair(), masterSecrets: [:], pairedDevices: [:]),
        clock: cancelledClock,
        connector: CloudOpeningBudgetConnector(probe: CloudReconnectSocketProbe()),
        openingTimeout: 15)
    let cancelledConnect = Task { try await cancelledTransport.connect(role: .machine) }
    try await waitUntil("cancellable token acquisition starts") {
        await cancelledToken.snapshot().started
    }
    cancelledConnect.cancel()
    do {
        try await cancelledConnect.value
        throw CloudTransportTestFailure(description: "a cancelled opening attempt completed")
    } catch is CancellationError {
        checks += 1
    }
    try await waitUntil("caller cancellation reaches token acquisition") {
        await cancelledToken.snapshot().cancelled
    }
    await cancelledTransport.shutdown()

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

/// R-1: authenticated commands have one explicit count/charged-byte owner. A suspended consumer
/// fills but cannot grow it; overload does not advance replay identity or fence later requests;
/// reconnect does not evict admitted work; and shutdown closes admission without false metrics.
private func runCloudTransportInboundBudgetTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudTransportTestFailure(description: message) }
    }

    func command(sequence: UInt64, bytes: Int) -> CloudInboundCommand {
        CloudInboundCommand(
            channel: "ctl/budget-machine", sequence: sequence, timestamp: 1,
            commandClass: .ctl, sender: "budget-viewer",
            plaintext: Data(repeating: UInt8(sequence % 251), count: bytes)
        )
    }

    // Representative byte-overflow proof, with the count limit deliberately out of the way.
    let firstByteCommand = command(sequence: 1, bytes: 32)
    let byteQueue = CloudInboundCommandQueue(limits: CloudInboundCommandQueueLimits(
        maximumCount: 8,
        maximumChargedBytes: firstByteCommand.ingressChargedBytes + 1
    ))
    let firstByteAdmission = byteQueue.admit(firstByteCommand)
    if case .success = firstByteAdmission {
        checks += 1
    } else {
        throw CloudTransportTestFailure(
            description: "the first command fits the charged-byte budget"
        )
    }
    let byteOverflow = byteQueue.admit(command(sequence: 2, bytes: 2))
    if case .failure(.chargedByteCap) = byteOverflow {
        checks += 1
    } else {
        throw CloudTransportTestFailure(
            description: "charged-byte overflow is refused independently of count"
        )
    }
    let byteMetrics = byteQueue.snapshot()
    try require(byteMetrics.currentCount == 1
                    && byteMetrics.currentChargedBytes == firstByteCommand.ingressChargedBytes,
                "byte refusal leaves exact current count and charge unchanged")
    try require(byteMetrics.refusalTotals[.chargedByteCap] == 1
                    && byteMetrics.peakChargedBytes == firstByteCommand.ingressChargedBytes,
                "byte refusal and peak are explicit observability")

    let plaintextQueue = CloudInboundCommandQueue(limits: CloudInboundCommandQueueLimits(
        maximumCount: 8, maximumChargedBytes: 1_024, maximumPlaintextBytes: 1
    ))
    let plaintextOverflow = plaintextQueue.admit(command(sequence: 3, bytes: 2))
    if case .failure(.plaintextCap) = plaintextOverflow {
        checks += 1
    } else {
        throw CloudTransportTestFailure(
            description: "one plaintext is refused at the actual post-decrypt admission boundary"
        )
    }
    let plaintextMetrics = plaintextQueue.snapshot()
    try require(plaintextMetrics.maximumPlaintextBytes == 1
                    && plaintextMetrics.currentCount == 0
                    && plaintextMetrics.admittedTotal == 0
                    && plaintextMetrics.refusalTotals[.plaintextCap] == 1,
                "plaintext refusal has an explicit limit and cannot inflate accepted metrics")
    plaintextQueue.finish()

    byteQueue.finish()
    let postFinishAdmission = byteQueue.admit(command(sequence: 4, bytes: 1))
    if case .failure(.finished) = postFinishAdmission {
        checks += 1
    } else {
        throw CloudTransportTestFailure(
            description: "a finished ingress owner rejects rather than accepting unreachable work"
        )
    }
    let finishedMetrics = byteQueue.snapshot()
    try require(finishedMetrics.admittedTotal == 1
                    && finishedMetrics.refusalTotals[.finished] == 1,
                "post-finish admission preserves accepted accounting and records a typed refusal")

    let machineKey = CloudDeviceKeyPair()
    let viewerKey = CloudDeviceKeyPair()
    let master = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x65, count: 32))
    let relay = CloudLoopbackRelay(
        account: "budget-account", deviceID: "budget-machine",
        devicePublicKey: machineKey.publicKeyRaw, allowedTokens: ["budget-token"]
    )
    let logs = CloudTestLog()
    let transport = CloudTransport(
        relayBaseURL: URL(string: "ws://loopback.invalid/v1/connect")!,
        tokenProvider: CloudTestTokenProvider(tokens: [
            CloudDeviceToken(value: "budget-token", expiresAt: Date().addingTimeInterval(3_600))
        ]),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: machineKey, masterSecrets: ["master-1": master],
            pairedDevices: ["budget-viewer": viewerKey.publicKeyRaw]
        ),
        connector: CloudLoopbackSocketConnector(relay: relay),
        initialBackoff: 0.01,
        maximumBackoff: 0.02,
        inboundQueueLimits: CloudInboundCommandQueueLimits(
            maximumCount: 2, maximumChargedBytes: 1_024 * 1_024
        ),
        logger: { logs.append($0) }
    )
    let refusals = CloudInboundRefusalRecorder()
    await transport.setInboundRefusalHandler { refusal in
        refusals.append(refusal)
    }
    try await transport.connect(role: .machine)

    func envelope(sequence: UInt64, label: String) throws -> CloudEnvelope {
        try CloudEnvelope.seal(
            Data(label.utf8), ch: "ctl/budget-machine", seq: sequence,
            ts: millisecondsNow(), envelopeClass: .ctl, keyID: "master-1",
            sender: "budget-viewer", masterSecret: master, signingKey: viewerKey
        )
    }
    let first = try envelope(sequence: 1, label: "one")
    let second = try envelope(sequence: 2, label: "two")
    let refused = try envelope(sequence: 3, label: "three")
    try await relay.send(envelope: first)
    try await relay.send(envelope: second)
    try await relay.send(envelope: refused)
    try await waitUntil("suspended consumer reaches count refusal") {
        let metrics = await transport.inboundQueueMetrics()
        let observed = refusals.all()
        return metrics.currentCount == 2 && observed.count == 1
    }
    let saturated = await transport.inboundQueueMetrics()
    try require(saturated.currentCount == 2 && saturated.peakCount == 2,
                "a suspended consumer stays at the explicit count ceiling")
    try require(saturated.refusalTotals[.countCap] == 1,
                "count overflow is terminal for only the refused authenticated sequence")
    let refusalSnapshot = refusals.all()
    try require(refusalSnapshot.map(\.command.idempotencyKey)
                    == ["cloud:budget-viewer:3"],
                "the refused sequence is reported once without installing a successor fence")

    // Other actor lanes remain usable while the command consumer is deliberately absent.
    let outbound = try CloudEnvelope.seal(
        Data("still-publishes".utf8), ch: "s/budget-machine/session", seq: 1,
        ts: millisecondsNow(), envelopeClass: .stream, keyID: "master-1",
        sender: "budget-machine", masterSecret: master, signingKey: machineKey
    )
    try await transport.publish(envelope: outbound)
    try await waitUntil("outbound lane publishes while ingress is saturated") {
        await relay.publishedEnvelopes().contains(outbound)
    }
    checks += 1

    let handshakes = await relay.completedHandshakes()
    await relay.dropConnections()
    try await waitUntil("reconnect preserves saturated command occupancy", timeout: 2) {
        let newHandshakes = await relay.completedHandshakes()
        let metrics = await transport.inboundQueueMetrics()
        return newHandshakes > handshakes && metrics.currentCount == 2
    }
    checks += 1

    var iterator = transport.commands.makeAsyncIterator()
    let drainedFirst = await iterator.next()
    try require(drainedFirst?.sequence == 1,
                "reconnect and suspension conserve the oldest admitted command")
    let later = try envelope(sequence: 4, label: "four")
    try await relay.send(envelope: later)
    try await waitUntil("capacity drain admits a later request") {
        await transport.inboundQueueMetrics().currentCount == 2
    }
    let drainedSecond = await iterator.next()
    let admittedLater = await iterator.next()
    try require([drainedSecond?.sequence, admittedLater?.sequence] == [2, 4]
                    && admittedLater?.idempotencyKey == "cloud:budget-viewer:4",
                "capacity drain admits a higher sequence and preserves FIFO without a retry fence")
    try await relay.send(envelope: refused)
    try await waitUntil("terminally refused predecessor is stale after a higher admission") {
        await transport.droppedInboundCount() == 1
    }
    checks += 1
    let recovered = await transport.inboundQueueMetrics()
    try require(recovered.currentCount == 0 && recovered.admittedTotal == 3
                    && recovered.deliveredTotal == 3 && recovered.droppedInvalidTotal == 1,
                "recovery accounts admitted, delivered and stale-replay stages without loss")
    try require(logs.lines().contains {
                    $0.hasPrefix("refusal layer=mac_transport code=replay sender=budget-viewer seq=3 ")
                }
                    && logs.lines().allSatisfy { !$0.contains(refused.ct) && !$0.contains(later.ct) },
                "the stale predecessor is one refusal line with its reference and without content")

    await transport.shutdown()
    await relay.stop()
    return checks
}

private final class CloudOutboundCorrectionStore: CloudSpoolStore, @unchecked Sendable {
    enum Refusal: Equatable { case none, firstSeal, everySent }
    private let lock = NSLock()
    private var state = CloudSpoolPersistedState()
    private let refusal: Refusal
    private var didRefuseSeal = false
    private var refusalCount = 0

    init(refusal: Refusal) { self.refusal = refusal }

    func load() throws -> CloudSpoolPersistedState {
        lock.lock(); defer { lock.unlock() }
        return state
    }

    func commit(_ candidate: CloudSpoolPersistedState) throws {
        lock.lock(); defer { lock.unlock() }
        if refusal == .firstSeal, !didRefuseSeal,
           candidate.rows.contains(where: { $0.state == .ready }) {
            didRefuseSeal = true
            refusalCount += 1
            throw CloudDurableStoreFailure.persist
        }
        if refusal == .everySent, candidate.rows.contains(where: { $0.state == .sent }) {
            refusalCount += 1
            throw CloudDurableStoreFailure.oversized
        }
        state = candidate
    }

    func snapshot() -> (state: CloudSpoolPersistedState, refusals: Int) {
        lock.lock(); defer { lock.unlock() }
        return (state, refusalCount)
    }
}

func runCloudOutboundCorrectionTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudTransportTestFailure(description: message) }
    }
    let signingKey = CloudDeviceKeyPair()
    let masterSecret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x61, count: 32))

    var shutdownLimits = CloudSpoolLimits()
    shutdownLimits.attemptWindow = .milliseconds(20)
    let shutdownStore = CloudOutboundCorrectionStore(refusal: .none)
    let shutdownSpool = try CloudOutboundSpool(
        store: shutdownStore, clock: CloudSystemSpoolClock(), metrics: CloudNoopSpoolMetrics(),
        runtime: .mac, limits: shutdownLimits)
    let shutdownTransport = CloudAppBridgeTestTransport(
        suspendPublication: true, publicationRequiresShutdown: true)
    let shutdownComposition = CloudDurableOutboundComposition(
        spool: shutdownSpool, transport: shutdownTransport,
        identity: CloudAppIdentity(
            machineID: "durable-mac", deviceID: "durable-device", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey),
        nowMilliseconds: { UInt64(Date().timeIntervalSince1970 * 1_000) })
    try await shutdownComposition.enqueue(
        Data("shutdown-unblocks-send".utf8), channel: "t/account/viewer", logicalID: "shutdown")
    try await waitForCloudAppBridge("production-shaped send reaches uncancellable suspension") {
        shutdownTransport.state().publicationStarts == 1
    }
    await shutdownComposition.stop { await shutdownTransport.shutdown() }
    try await Task.sleep(nanoseconds: 30_000_000)
    try require(shutdownTransport.state().stopped
                    && shutdownTransport.state().publicationCancelled,
                "socket shutdown happens before joining a production-shaped suspended send")
    try require(shutdownStore.snapshot().state.rows.first?.state == .sent,
                "no attempt deadline task mutates durable state after stop returns")
    do {
        try await shutdownComposition.enqueue(
            Data("after-stop".utf8), channel: "t/account/viewer", logicalID: "after-stop")
        throw CloudTransportTestFailure(description: "enqueue after stop unexpectedly succeeded")
    } catch CloudDurableOutboundError.stopped {
        checks += 1
    }

    var reservationLimits = CloudSpoolLimits()
    reservationLimits.staleReservedAfter = .milliseconds(20)
    reservationLimits.gcInterval = .milliseconds(5)
    let reservationStore = CloudOutboundCorrectionStore(refusal: .firstSeal)
    let reservationSpool = try CloudOutboundSpool(
        store: reservationStore, clock: CloudSystemSpoolClock(), metrics: CloudNoopSpoolMetrics(),
        runtime: .mac, limits: reservationLimits)
    let reservationComposition = CloudDurableOutboundComposition(
        spool: reservationSpool, transport: CloudAppBridgeTestTransport(),
        identity: CloudAppIdentity(
            machineID: "durable-mac", deviceID: "durable-device", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey),
        nowMilliseconds: { UInt64(Date().timeIntervalSince1970 * 1_000) })
    do {
        try await reservationComposition.enqueue(
            Data("seal-fails".utf8), channel: "t/account/viewer", logicalID: "seal-fails")
        throw CloudTransportTestFailure(description: "injected seal failure did not fire")
    } catch CloudDurableStoreFailure.persist {}
    try await waitForCloudAppBridge("failed reservation has an autonomous stale wake owner") {
        await reservationSpool.row(seq: 0)?.burnReason == .staleReservation
    }
    checks += 1
    await reservationComposition.stop()

    let capacityStore = CloudOutboundCorrectionStore(refusal: .everySent)
    let capacitySpool = try CloudOutboundSpool(
        store: capacityStore, clock: CloudSystemSpoolClock(), metrics: CloudNoopSpoolMetrics())
    let capacityComposition = CloudDurableOutboundComposition(
        spool: capacitySpool, transport: CloudAppBridgeTestTransport(),
        identity: CloudAppIdentity(
            machineID: "durable-mac", deviceID: "durable-device", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey),
        nowMilliseconds: { UInt64(Date().timeIntervalSince1970 * 1_000) },
        deadlineFailureRetryDelay: .milliseconds(5))
    try await capacityComposition.enqueue(
        Data("capacity".utf8), channel: "t/account/viewer", logicalID: "capacity")
    try await waitForCloudAppBridge("structural capacity failure is visible") {
        await capacityComposition.persistentFailureState() == .capacity
    }
    try await Task.sleep(nanoseconds: 30_000_000)
    try require(capacityStore.snapshot().refusals == 1,
                "persistent capacity refusal is not retried by a fixed 1 Hz full-store loop")
    await capacityComposition.stop()

    let capDirectory = FileManager.default.temporaryDirectory.appendingPathComponent(
        "clawdline-window-cap-\(UUID().uuidString)", isDirectory: true)
    try FileManager.default.createDirectory(
        at: capDirectory, withIntermediateDirectories: true,
        attributes: [.posixPermissions: 0o700])
    defer { try? FileManager.default.removeItem(at: capDirectory) }
    let capFile = capDirectory.appendingPathComponent("spool.json")
    let capStore = try CloudFileSpoolStore(url: capFile)
    let capSpool = try CloudOutboundSpool(
        store: capStore, clock: CloudSystemSpoolClock(), metrics: CloudNoopSpoolMetrics())
    let mebibyteFrame = Data(repeating: 0x61, count: 1_048_576)
    for index in 0..<4 {
        let seq = try await capSpool.reserve(
            channel: .t, logicalID: "cap-\(index)", ownerID: nil,
            recipient: "viewer", record: .int(Int64(index)))
        try await capSpool.seal(seq: seq, sealedEnvelope: mebibyteFrame)
        _ = try await capSpool.sendNext { _ in }
    }
    let capCandidate = try await capSpool.reserve(
        channel: .t, logicalID: "cap-over", ownerID: nil,
        recipient: "viewer", record: .int(9))
    try await capSpool.seal(seq: capCandidate, sealedEnvelope: Data([0x62]))
    let capBlocked = try await capSpool.sendNext { _ in }
    let durableSize = try FileManager.default.attributesOfItem(atPath: capFile.path)[.size] as? Int
    try require(capBlocked == .blockedWindowFull(currentRows: 4, currentBytes: 4_194_304),
                "one byte beyond the default raw-frame cap is typed window refusal")
    try require(durableSize.map { $0 < CloudFileSpoolStore.maximumBytes } == true,
                "the full base64-and-metadata file reaches the typed cap below its ceiling")

    func strictRecord(channel: String, logicalID: String) -> CloudJSONValue {
        .object([
            "v": .int(2), "ch": .string(channel), "class": .string("stream"),
            "key_id": .string("key-1"), "logical_id": .string(logicalID),
            "payload_bytes": .int(7),
            "payload_sha256": .string(String(repeating: "a", count: 64)),
            "sender": .string("device-1"),
        ])
    }
    func strictFrame(seq: Int64, channel: String, timestamp: Int64) throws -> Data {
        try JSONSerialization.data(withJSONObject: [
            "type": "publish",
            "envelope": [
                "v": 1, "ch": channel, "seq": seq, "ts": timestamp,
                "class": "stream", "key_id": "key-1",
                "nonce": Data(repeating: 1, count: 12).base64EncodedString(),
                "ct": Data(repeating: 2, count: 16).base64EncodedString(),
                "sender": "device-1",
                "sig": Data(repeating: 3, count: 64).base64EncodedString(),
            ],
        ], options: [.sortedKeys, .withoutEscapingSlashes])
    }
    let freshnessStore = CloudOutboundCorrectionStore(refusal: .none)
    let freshnessSpool = try CloudOutboundSpool(
        store: freshnessStore, clock: CloudSystemSpoolClock(), metrics: CloudNoopSpoolMetrics(),
        strictPersistedFrameValidation: true)
    let freshnessNow = Int64(Date().timeIntervalSince1970 * 1_000)
    let staleTimestamp = freshnessNow - 241_000
    let staleTranscript = try await freshnessSpool.reserve(
        channel: .t, logicalID: "stale-t", ownerID: nil, recipient: "t/viewer",
        record: strictRecord(channel: "t/viewer", logicalID: "stale-t"))
    try await freshnessSpool.seal(
        seq: staleTranscript,
        sealedEnvelope: strictFrame(
            seq: staleTranscript, channel: "t/viewer", timestamp: staleTimestamp))
    let staleControl = try await freshnessSpool.reserve(
        channel: .ctl, logicalID: "stale-ctl", ownerID: "viewer", recipient: "ctl/machine",
        record: strictRecord(channel: "ctl/machine", logicalID: "stale-ctl"))
    try await freshnessSpool.seal(
        seq: staleControl,
        sealedEnvelope: strictFrame(
            seq: staleControl, channel: "ctl/machine", timestamp: staleTimestamp))
    let fresh = try await freshnessSpool.reserve(
        channel: .t, logicalID: "fresh-t", ownerID: nil, recipient: "t/viewer",
        record: strictRecord(channel: "t/viewer", logicalID: "fresh-t"))
    try await freshnessSpool.seal(
        seq: fresh,
        sealedEnvelope: strictFrame(seq: fresh, channel: "t/viewer", timestamp: freshnessNow))
    let freshDisposition = try await freshnessSpool.sendNext { _ in }
    let freshnessState = freshnessStore.snapshot().state
    try require(freshDisposition == .sent(seq: fresh),
                "live selection removes stale predecessors and sends the next fresh row immediately")
    try require(!freshnessState.rows.contains { $0.seq == staleTranscript },
                "never-sent transcript freshness has zero terminal retention")
    try require(!freshnessState.rows.contains { $0.seq == staleControl },
                "never-sent control freshness has zero terminal retention")
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
    from stream: CloudInboundCommandStream,
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

private func nextOutboundReceipt(
    from stream: AsyncStream<CloudOutboundTransportReceipt>,
    timeout: TimeInterval = 1
) async throws -> CloudOutboundTransportReceipt? {
    try await withThrowingTaskGroup(of: CloudOutboundTransportReceipt?.self) { group in
        group.addTask {
            var iterator = stream.makeAsyncIterator()
            return await iterator.next()
        }
        group.addTask {
            try await Task.sleep(nanoseconds: UInt64(timeout * 1_000_000_000))
            throw CloudTransportTestFailure(description: "timed out waiting for outbound receipt")
        }
        let result = try await group.next()!
        group.cancelAll()
        return result
    }
}

#if CLOUD_TRANSPORT_STANDALONE && !CLOUD_INGRESS_COMBINED_STANDALONE
@main
private struct CloudTransportStandaloneTests {
    static func main() async throws {
        let count = try await runCloudTransportTests()
        print("\(count) CloudTransport checks passed")
    }
}
#endif
