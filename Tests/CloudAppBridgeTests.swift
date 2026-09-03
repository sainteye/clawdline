import Foundation

private struct CloudAppBridgeTestFailure: Error, CustomStringConvertible {
    let description: String
}

private actor CloudAppBridgeTestSequence: CloudEnvelopeSequencing {
    private var value: UInt64 = 0

    func nextSequence(sender: String) async throws -> UInt64 {
        value += 1
        return value
    }
}

private struct CloudAppBridgeTestTokenProvider: CloudDeviceTokenProviding {
    let token: CloudDeviceToken

    func fetchDeviceToken() async throws -> CloudDeviceToken { token }
}

private final class CloudAppBridgeTestTransport: CloudTransporting, @unchecked Sendable {
    nonisolated let commands: AsyncStream<CloudInboundCommand>
    nonisolated let readyGenerations: AsyncStream<UInt64>

    private let lock = NSLock()
    private var continuation: AsyncStream<CloudInboundCommand>.Continuation!
    private var readyContinuation: AsyncStream<UInt64>.Continuation!
    private var sent: [CloudEnvelope] = []
    private var connected = false
    private var stopped = false
    private var connectStarted = false
    private var connectFinished = false
    private var connectContinuation: CheckedContinuation<Void, Never>?
    private var readyGeneration: UInt64 = 0
    private let suspendConnect: Bool
    private let suspendPublication: Bool
    private let publicationContinuation: AsyncStream<Void>.Continuation
    private let publicationStream: AsyncStream<Void>
    private var publicationStarted = false
    private var publicationCancelled = false

    init(suspendConnect: Bool = false, suspendPublication: Bool = false) {
        self.suspendConnect = suspendConnect
        self.suspendPublication = suspendPublication
        var continuation: AsyncStream<CloudInboundCommand>.Continuation!
        commands = AsyncStream { continuation = $0 }
        self.continuation = continuation
        var readyContinuation: AsyncStream<UInt64>.Continuation!
        readyGenerations = AsyncStream { readyContinuation = $0 }
        self.readyContinuation = readyContinuation
        var publicationContinuation: AsyncStream<Void>.Continuation!
        publicationStream = AsyncStream { publicationContinuation = $0 }
        self.publicationContinuation = publicationContinuation
    }

    func connect(role: CloudTransportRole) async throws {
        if suspendConnect {
            await withCheckedContinuation { continuation in
                lock.lock()
                connectStarted = true
                connectContinuation = continuation
                lock.unlock()
            }
            markConnectFinished()
        }
        if Task.isCancelled { throw CancellationError() }
        setConnected(role == .machine)
        signalReady()
    }

    private func setConnected(_ value: Bool) {
        lock.lock()
        connected = value
        lock.unlock()
    }

    func publish(envelope: CloudEnvelope) async throws {
        if suspendPublication {
            markPublicationStarted()
            await withTaskCancellationHandler {
                var iterator = publicationStream.makeAsyncIterator()
                _ = await iterator.next()
            } onCancel: {
                self.cancelPublication()
            }
            if Task.isCancelled { throw CancellationError() }
        }
        append(envelope)
    }

    private func markPublicationStarted() {
        lock.lock()
        publicationStarted = true
        lock.unlock()
    }

    private func cancelPublication() {
        lock.lock()
        publicationCancelled = true
        lock.unlock()
        publicationContinuation.finish()
    }

    private func append(_ envelope: CloudEnvelope) {
        lock.lock()
        sent.append(envelope)
        lock.unlock()
    }

    func shutdown() async {
        markStopped()
        continuation.finish()
        readyContinuation.finish()
        let suspended = takeConnectContinuation()
        if let suspended {
            Task {
                try? await Task.sleep(nanoseconds: 75_000_000)
                suspended.resume()
            }
        }
    }

    private func markStopped() {
        lock.lock()
        stopped = true
        connected = false
        lock.unlock()
    }

    private func markConnectFinished() {
        lock.lock()
        connectFinished = true
        lock.unlock()
    }

    private func takeConnectContinuation() -> CheckedContinuation<Void, Never>? {
        lock.lock()
        defer { lock.unlock() }
        let suspended = connectContinuation
        connectContinuation = nil
        return suspended
    }

    func signalReady() {
        lock.lock()
        readyGeneration += 1
        let generation = readyGeneration
        lock.unlock()
        readyContinuation.yield(generation)
    }

    func yield(_ plaintext: String, sequence: UInt64, commandClass: CloudEnvelopeClass = .ctl,
               channel: String = "ctl/Mac%20%2F%20%E5%8F%B0%E7%81%A3") {
        continuation.yield(CloudInboundCommand(
            channel: channel, sequence: sequence, timestamp: 1,
            commandClass: commandClass, sender: "viewer", plaintext: Data(plaintext.utf8)
        ))
    }

    func envelopes() -> [CloudEnvelope] {
        lock.lock()
        defer { lock.unlock() }
        return sent
    }

    func state() -> (
        connected: Bool, stopped: Bool, connectStarted: Bool, connectFinished: Bool,
        publicationStarted: Bool, publicationCancelled: Bool
    ) {
        lock.lock()
        defer { lock.unlock() }
        return (
            connected, stopped, connectStarted, connectFinished,
            publicationStarted, publicationCancelled
        )
    }
}

private actor CloudAppBridgeTestRouter: CloudCommandRouting {
    struct Call: Equatable {
        let command: CloudHeadlessCommand
        let sender: String
        let idempotencyKey: String
    }

    struct ReadCall: Equatable {
        let read: CloudHeadlessRead
        let sender: String
    }

    private var calls: [Call] = []
    private var reads: [ReadCall] = []
    private var readAnswer = CloudReadResult(status: 200, body: Data(#"{"ok":true}"#.utf8))

    func route(_ command: CloudHeadlessCommand, sender: String,
               idempotencyKey: String) async -> CloudCommandResult {
        calls.append(Call(command: command, sender: sender, idempotencyKey: idempotencyKey))
        return CloudCommandResult(status: 200, code: nil)
    }

    func read(_ read: CloudHeadlessRead, sender: String) async -> CloudReadResult {
        reads.append(ReadCall(read: read, sender: sender))
        return readAnswer
    }

    func answerReadsWith(_ answer: CloudReadResult) { readAnswer = answer }

    func recorded() -> [Call] { calls }
    func recordedReads() -> [ReadCall] { reads }
}

private final class CloudAppBridgeTestGate: @unchecked Sendable {
    private let lock = NSLock()
    private var value = false

    func set(_ value: Bool) {
        lock.lock()
        self.value = value
        lock.unlock()
    }

    func get() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return value
    }
}

private final class CloudAppBridgeCompletion: @unchecked Sendable {
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

private final class CloudAppBridgeTestResults: @unchecked Sendable {
    private let lock = NSLock()
    private var values: [CloudCommandResult] = []

    func append(_ value: CloudCommandResult) {
        lock.lock()
        values.append(value)
        lock.unlock()
    }

    func all() -> [CloudCommandResult] {
        lock.lock()
        defer { lock.unlock() }
        return values
    }
}

private func runCloudAppBridgeBaseTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }

    let signingKey = CloudDeviceKeyPair()
    let masterSecret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x51, count: 32))
    let transport = CloudAppBridgeTestTransport()
    let sequence = CloudAppBridgeTestSequence()
    let router = CloudAppBridgeTestRouter()
    let gate = CloudAppBridgeTestGate()
    let results = CloudAppBridgeTestResults()
    let bridge = CloudAppBridge(
        transport: transport,
        identity: CloudAppIdentity(
            machineID: "Mac / 台灣", deviceID: "machine-device", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey
        ),
        sequencing: sequence,
        allowCloudCommands: { gate.get() },
        commandRouter: router,
        nowMilliseconds: { 1_787_740_000_000 },
        commandResult: { results.append($0) }
    )

    try require(transport.state().connected == false, "the bridge is inert before start")
    try await bridge.start()
    try require(transport.state().connected, "explicit start connects the machine transport")

    let sessionPayload: [String: Any] = [
        "sessions": [
            ["id": "session/一|?", "label": "First", "state": "working"],
            ["id": "plain", "label": "Second", "state": "idle"],
        ],
        "at": 123,
        "scan": ["generation": 4, "complete": true, "emptyAuthoritative": false],
    ]
    try await bridge.publishSessions(JSONSerialization.data(withJSONObject: sessionPayload))
    let orchestratorPayload = try JSONSerialization.data(withJSONObject: [
        "tasks": [["id": "task-one", "state": "working"]], "at": 124,
    ], options: [.sortedKeys])
    try await bridge.publishOrchestrator(orchestratorPayload)

    let published = transport.envelopes()
    try require(published.count == 3, "two full sessions and one orchestrator snapshot publish")
    try require(
        published.map(\.ch).contains("s/Mac%20%2F%20%E5%8F%B0%E7%81%A3/session%2F%E4%B8%80%7C%3F"),
        "machine and session channel segments use encodeURIComponent-compatible encoding"
    )
    try require(published.map(\.ch).contains("orch/Mac%20%2F%20%E5%8F%B0%E7%81%A3"),
                "the orchestrator uses the encoded machine channel")
    try require(published.allSatisfy { $0.envelopeClass == .stream },
                "all app snapshots use the stream class")

    let firstSession = try published.first { $0.ch.hasSuffix("session%2F%E4%B8%80%7C%3F") }!
        .open(masterSecret: masterSecret, publicKeyForSender: {
            $0 == "machine-device" ? signingKey.publicKeyRaw : nil
        })
    let firstObject = try JSONSerialization.jsonObject(with: firstSession) as! [String: Any]
    let firstRow = firstObject["session"] as? [String: Any]
    try require(firstRow?["label"] as? String == "First",
                "the channel contains the full locally serialized session row")
    let orch = try published.first { $0.ch.hasPrefix("orch/") }!
        .open(masterSecret: masterSecret, publicKeyForSender: {
            $0 == "machine-device" ? signingKey.publicKeyRaw : nil
        })
    try require(orch == orchestratorPayload, "orchestrator publication keeps the local payload bytes")

    let incompleteEmpty: [String: Any] = [
        "sessions": [], "at": 125,
        "scan": ["generation": 5, "complete": false, "emptyAuthoritative": false],
    ]
    try await bridge.publishSessions(JSONSerialization.data(withJSONObject: incompleteEmpty))
    try require(transport.envelopes().count == 3,
                "an incomplete nonauthoritative scan never tombstones known sessions")

    let oneRemaining: [String: Any] = [
        "sessions": [["id": "session/一|?", "label": "First", "state": "idle"]],
        "at": 126,
        "scan": ["generation": 6, "complete": true, "emptyAuthoritative": false],
    ]
    try await bridge.publishSessions(JSONSerialization.data(withJSONObject: oneRemaining))
    let afterRemoval = transport.envelopes()
    let plainFrames = afterRemoval.filter { $0.ch.hasSuffix("/plain") }
    let removedPlain = try plainFrames.last?.open(
        masterSecret: masterSecret,
        publicKeyForSender: { $0 == "machine-device" ? signingKey.publicKeyRaw : nil }
    )
    let removedObject: [String: Any]? = removedPlain.flatMap {
        (try? JSONSerialization.jsonObject(with: $0)) as? [String: Any]
    }
    try require(plainFrames.count == 2 && removedObject?["deleted"] as? Bool == true,
                "a complete A+B to A scan publishes a B tombstone")

    let authoritativeEmpty: [String: Any] = [
        "sessions": [], "at": 127,
        "scan": ["generation": 7, "complete": false, "emptyAuthoritative": true],
    ]
    try await bridge.publishSessions(JSONSerialization.data(withJSONObject: authoritativeEmpty))
    let firstFrames = transport.envelopes().filter {
        $0.ch.hasSuffix("session%2F%E4%B8%80%7C%3F")
    }
    let removedFirst = try firstFrames.last?.open(
        masterSecret: masterSecret,
        publicKeyForSender: { $0 == "machine-device" ? signingKey.publicKeyRaw : nil }
    )
    let removedFirstObject: [String: Any]? = removedFirst.flatMap {
        (try? JSONSerialization.jsonObject(with: $0)) as? [String: Any]
    }
    try require(firstFrames.count == 3 && removedFirstObject?["deleted"] as? Bool == true,
                "independent authoritative-empty evidence tombstones the last session")

    transport.yield(#"{"type":"send","session":"plain","text":"hello","images":[]}"#,
                    sequence: 10)
    try await waitForCloudAppBridge("default-off command refusal") {
        results.all().contains { $0.code == "cloud_commands_disabled" }
    }
    let defaultOffCalls = await router.recorded()
    try require(defaultOffCalls.isEmpty, "default-off commands never enter the broker")

    gate.set(true)
    transport.yield(#"{"type":"answer","session":"plain","answer":"2"}"#, sequence: 11)
    try await waitForCloudAppBridge("allowed command routing") {
        await router.recorded().count == 1
    }
    let routed = await router.recorded()
    try require(routed.first?.command == .answer(session: "plain", key: "2"),
                "answer uses the shared typed broker seam")
    try require(routed.first?.idempotencyKey == "cloud:viewer:11",
                "verified sender and sequence supply HTTP-equivalent idempotency")

    transport.yield("not json", sequence: 12)
    transport.yield(#"{"type":"erase","session":"plain"}"#, sequence: 13)
    transport.yield(#"{"type":"dispatch","task":{"title":"not pinned"}}"#,
                    sequence: 14, commandClass: .dispatch)
    try await waitForCloudAppBridge("typed command refusals") {
        let codes = results.all().compactMap(\.code)
        return codes.contains("malformed_command") && codes.contains("unknown_command")
            && codes.contains("cloud_dispatch_unpinned")
    }
    let refusedCalls = await router.recorded()
    try require(refusedCalls.count == 1,
                "malformed, unknown, and unpinned dispatch commands never reach execution")

    await bridge.stop()
    try require(transport.state().stopped, "bridge shutdown stops the transport")
    let bridgeRunning = await bridge.isRunning()
    try require(bridgeRunning == false, "bridge lifecycle ends after stream shutdown")

    return checks
}

private func runCloudAppBridgeLifecycleTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }

    let signingKey = CloudDeviceKeyPair()
    let masterSecret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x52, count: 32))
    RemoteServer.cloudSnapshotDataForTesting = try cloudAppBridgeTestSnapshots(sessionID: nil)
    defer { RemoteServer.cloudSnapshotDataForTesting = nil }
    func makeBridge(_ transport: CloudAppBridgeTestTransport,
                    _ router: CloudAppBridgeTestRouter) -> CloudAppBridge {
        CloudAppBridge(
            transport: transport,
            identity: CloudAppIdentity(
                machineID: "lifecycle", deviceID: "machine-device", keyID: "ms-1",
                masterSecret: masterSecret, signingKey: signingKey
            ),
            sequencing: CloudAppBridgeTestSequence(),
            allowCloudCommands: { true },
            commandRouter: router
        )
    }

    var detachedTransport: CloudAppBridgeTestTransport? = CloudAppBridgeTestTransport(
        suspendConnect: true
    )
    let detachedRouter = CloudAppBridgeTestRouter()
    var detachedBridge: CloudAppBridge? = makeBridge(detachedTransport!, detachedRouter)
    weak var detachedTransportReference = detachedTransport
    weak var detachedBridgeReference = detachedBridge
    await attachCloudBridgeForTest(detachedBridge)
    try await waitForCloudAppBridge("suspended attach starts connecting") {
        detachedTransport!.state().connectStarted
    }
    await attachCloudBridgeForTest(nil)
    await RemoteServer.shared.awaitCloudBridgeLifecycle()
    try require(detachedTransport!.state().stopped,
                "detach closes a suspended bridge transport")
    try require(detachedTransport!.state().connectFinished,
                "detach waits for the suspended connect lifecycle to finish")
    let detachedRunning = await detachedBridge!.isRunning()
    try require(detachedRunning == false,
                "detached suspended bridge stays stopped")
    detachedTransport!.signalReady()
    detachedTransport!.yield(
        #"{"type":"send","session":"stale","text":"no","images":[]}"#,
        sequence: 40, channel: "ctl/lifecycle"
    )
    try await Task.sleep(nanoseconds: 30_000_000)
    let detachedCalls = await detachedRouter.recorded()
    try require(detachedCalls.isEmpty,
                "detach during connect leaves no stale command consumer")
    try require(detachedTransport!.envelopes().isEmpty,
                "detach leaves stale transport publications empty")
    detachedBridge = nil
    detachedTransport = nil
    try require(detachedBridgeReference == nil && detachedTransportReference == nil,
                "completed detach releases the old bridge and transport")

    var replacedTransport: CloudAppBridgeTestTransport? = CloudAppBridgeTestTransport(
        suspendConnect: true
    )
    let replacedRouter = CloudAppBridgeTestRouter()
    var replacedBridge: CloudAppBridge? = makeBridge(replacedTransport!, replacedRouter)
    weak var replacedTransportReference = replacedTransport
    weak var replacedBridgeReference = replacedBridge
    await attachCloudBridgeForTest(replacedBridge)
    try await waitForCloudAppBridge("replacement candidate starts connecting") {
        replacedTransport!.state().connectStarted
    }
    let activeTransport = CloudAppBridgeTestTransport()
    let activeRouter = CloudAppBridgeTestRouter()
    let activeBridge = makeBridge(activeTransport, activeRouter)
    await attachCloudBridgeForTest(activeBridge)
    try await waitForCloudAppBridge("replacement starts after stopping stale bridge") {
        activeTransport.state().connected
    }
    try require(replacedTransport!.state().stopped
                && replacedTransport!.state().connectFinished,
                "replacement starts only after the old connect lifecycle terminates")
    let replacedRunning = await replacedBridge!.isRunning()
    try require(replacedRunning == false,
                "replaced suspended bridge cannot revive")
    replacedTransport!.signalReady()
    replacedTransport!.yield(
        #"{"type":"send","session":"stale","text":"no","images":[]}"#,
        sequence: 41, channel: "ctl/lifecycle"
    )
    try await Task.sleep(nanoseconds: 30_000_000)
    let replacedCalls = await replacedRouter.recorded()
    try require(replacedCalls.isEmpty,
                "replacement leaves no stale command consumer")
    try require(replacedTransport!.envelopes().isEmpty,
                "replacement leaves stale transport publications empty")
    replacedBridge = nil
    replacedTransport = nil
    try require(replacedBridgeReference == nil && replacedTransportReference == nil,
                "completed replacement releases the old bridge and transport")
    await attachCloudBridgeForTest(nil)
    await RemoteServer.shared.awaitCloudBridgeLifecycle()
    try require(activeTransport.state().stopped,
                "replacement cleanup stops active transport")
    return checks
}

private func runCloudAppBridgeTransitiveLifecycleTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }

    let signingKey = CloudDeviceKeyPair()
    let masterSecret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x56, count: 32))
    RemoteServer.cloudSnapshotDataForTesting = try cloudAppBridgeTestSnapshots(sessionID: nil)
    defer { RemoteServer.cloudSnapshotDataForTesting = nil }
    func makeBridge(_ transport: CloudAppBridgeTestTransport,
                    _ router: CloudAppBridgeTestRouter) -> CloudAppBridge {
        CloudAppBridge(
            transport: transport,
            identity: CloudAppIdentity(
                machineID: "transitive", deviceID: "machine-device", keyID: "ms-1",
                masterSecret: masterSecret, signingKey: signingKey
            ),
            sequencing: CloudAppBridgeTestSequence(), allowCloudCommands: { true },
            commandRouter: router
        )
    }

    var transportA: CloudAppBridgeTestTransport? = CloudAppBridgeTestTransport(suspendConnect: true)
    let routerA = CloudAppBridgeTestRouter()
    var bridgeA: CloudAppBridge? = makeBridge(transportA!, routerA)
    weak var transportAReference = transportA
    weak var bridgeAReference = bridgeA
    await attachCloudBridgeForTest(bridgeA)
    try await waitForCloudAppBridge("transitive A starts its suspended connect") {
        transportA!.state().connectStarted
    }

    var transportB: CloudAppBridgeTestTransport? = CloudAppBridgeTestTransport()
    let routerB = CloudAppBridgeTestRouter()
    var bridgeB: CloudAppBridge? = makeBridge(transportB!, routerB)
    weak var transportBReference = transportB
    weak var bridgeBReference = bridgeB
    await attachCloudBridgeForTest(bridgeB)

    let transportC = CloudAppBridgeTestTransport()
    let bridgeC = makeBridge(transportC, CloudAppBridgeTestRouter())
    await attachCloudBridgeForTest(bridgeC)
    let lifecycleCompletion = CloudAppBridgeCompletion()
    let lifecycleTask = Task {
        await RemoteServer.shared.awaitCloudBridgeLifecycle()
        lifecycleCompletion.finish()
    }
    try await Task.sleep(nanoseconds: 30_000_000)
    try require(transportC.state().connected == false,
                "C cannot connect while transitive A teardown is still blocked")
    try require(lifecycleCompletion.finished() == false,
                "latest lifecycle await includes every predecessor teardown")

    await lifecycleTask.value
    try require(transportA!.state().stopped && transportA!.state().connectFinished,
                "transitive lifecycle terminates A's suspended connect")
    try require(transportB!.state().connected == false,
                "cancelled intermediate B never starts")
    try require(transportC.state().connected,
                "C starts after the complete predecessor lifecycle chain")
    try require(transportA!.envelopes().isEmpty && transportB!.envelopes().isEmpty,
                "transitive teardown emits no stale publication")
    transportA!.yield(
        #"{"type":"send","session":"stale-a","text":"no","images":[]}"#,
        sequence: 60, channel: "ctl/transitive"
    )
    transportB!.yield(
        #"{"type":"send","session":"stale-b","text":"no","images":[]}"#,
        sequence: 61, channel: "ctl/transitive"
    )
    try await Task.sleep(nanoseconds: 30_000_000)
    let callsA = await routerA.recorded()
    let callsB = await routerB.recorded()
    try require(callsA.isEmpty && callsB.isEmpty,
                "transitive teardown leaves no stale command consumer")

    bridgeA = nil
    transportA = nil
    bridgeB = nil
    transportB = nil
    try require(bridgeAReference == nil && transportAReference == nil
                && bridgeBReference == nil && transportBReference == nil,
                "latest lifecycle completion releases A and B ownership")
    await attachCloudBridgeForTest(nil)
    await RemoteServer.shared.awaitCloudBridgeLifecycle()
    return checks
}

private func runCloudAppBridgePublicationLifecycleTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }

    let signingKey = CloudDeviceKeyPair()
    let masterSecret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x57, count: 32))
    RemoteServer.cloudSnapshotDataForTesting = try cloudAppBridgeTestSnapshots(
        sessionID: "publication-lifecycle"
    )
    defer { RemoteServer.cloudSnapshotDataForTesting = nil }
    func makeBridge(_ transport: CloudAppBridgeTestTransport) -> CloudAppBridge {
        CloudAppBridge(
            transport: transport,
            identity: CloudAppIdentity(
                machineID: "publication", deviceID: "machine-device", keyID: "ms-1",
                masterSecret: masterSecret, signingKey: signingKey
            ),
            sequencing: CloudAppBridgeTestSequence()
        )
    }

    var transportA: CloudAppBridgeTestTransport? = CloudAppBridgeTestTransport(
        suspendPublication: true
    )
    var bridgeA: CloudAppBridge? = makeBridge(transportA!)
    weak var transportAReference = transportA
    weak var bridgeAReference = bridgeA
    await attachCloudBridgeForTest(bridgeA)
    try await waitForCloudAppBridge("A publication suspends after entry") {
        transportA!.state().publicationStarted
    }

    let transportB = CloudAppBridgeTestTransport()
    let bridgeB = makeBridge(transportB)
    await attachCloudBridgeForTest(bridgeB)
    await RemoteServer.shared.awaitCloudBridgeLifecycle()
    try require(transportA!.state().publicationCancelled,
                "replacement cancels an in-flight bridge-owned publication")
    try await waitForCloudAppBridge("B fresh publications bypass A's invalidated tail") {
        let channels = transportB.envelopes().map(\.ch)
        return channels.filter { $0.hasPrefix("s/publication/") }.count == 1
            && channels.filter { $0 == "orch/publication" }.count == 1
    }
    try require(transportA!.envelopes().isEmpty,
                "cancelled A publication emits no stale envelope")
    bridgeA = nil
    transportA = nil
    try require(bridgeAReference == nil && transportAReference == nil,
                "publication lifecycle completion releases A and its transport")
    await attachCloudBridgeForTest(nil)
    await RemoteServer.shared.awaitCloudBridgeLifecycle()
    return checks
}

private func runCloudAppBridgeABATests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }

    await attachCloudBridgeForTest(nil)
    await RemoteServer.shared.awaitCloudBridgeLifecycle()
    let before = await RemoteServer.shared.cloudLifecycleStateForTesting(bridge: nil)
    let signingKey = CloudDeviceKeyPair()
    let masterSecret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x54, count: 32))
    let transport = CloudAppBridgeTestTransport()
    let bridge = CloudAppBridge(
        transport: transport,
        identity: CloudAppIdentity(
            machineID: "aba", deviceID: "machine-device", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey
        ),
        sequencing: CloudAppBridgeTestSequence()
    )

    let mainEntered = DispatchSemaphore(value: 0)
    let backgroundAttached = DispatchSemaphore(value: 0)
    let mainFinished = DispatchSemaphore(value: 0)
    DispatchQueue.main.async {
        mainEntered.signal()
        _ = backgroundAttached.wait(timeout: .now() + 2)
        RemoteServer.shared.attachCloudBridge(nil)
        mainFinished.signal()
    }
    await waitForCloudAppBridgeSemaphore(mainEntered)
    DispatchQueue.global(qos: .userInitiated).async {
        RemoteServer.shared.attachCloudBridge(bridge)
        backgroundAttached.signal()
    }
    await waitForCloudAppBridgeSemaphore(mainFinished)
    await RemoteServer.shared.awaitCloudBridgeLifecycle()
    await MainActor.run {}

    let after = await RemoteServer.shared.cloudLifecycleStateForTesting(bridge: nil)
    let observerPresent = await MainActor.run {
        SessionWatch.shared.observers["remote"] != nil
    }
    try require(after.bridgeMatches, "ABA final bridge reflects the newest detach request")
    try require(after.generation == before.generation + 2,
                "ABA lifecycle generation includes both ordered requests")
    try require(observerPresent == false,
                "ABA final SessionWatch observer reflects the newest detach request")
    try require(transport.state().connected == false,
                "ABA newest detach leaves no superseded bridge lifecycle active")
    return checks
}

private func runCloudAppBridgeReconnectTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }

    let signingKey = CloudDeviceKeyPair()
    let masterSecret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x53, count: 32))
    let transport = CloudAppBridgeTestTransport()
    let bridge = CloudAppBridge(
        transport: transport,
        identity: CloudAppIdentity(
            machineID: "reconnect", deviceID: "machine-device", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey
        ),
        sequencing: CloudAppBridgeTestSequence()
    )
    RemoteServer.cloudSnapshotDataForTesting = try cloudAppBridgeTestSnapshots(
        sessionID: "cloud-reconnect"
    )
    defer { RemoteServer.cloudSnapshotDataForTesting = nil }

    await attachCloudBridgeForTest(bridge)
    try await waitForCloudAppBridge("initial ready generation publishes both snapshots") {
        let channels = transport.envelopes().map(\.ch)
        return channels.filter { $0.hasPrefix("s/reconnect/") }.count == 1
            && channels.filter { $0 == "orch/reconnect" }.count == 1
    }
    transport.signalReady()
    try await waitForCloudAppBridge("reconnect republishes without a local observation") {
        let channels = transport.envelopes().map(\.ch)
        return channels.filter { $0.hasPrefix("s/reconnect/") }.count == 2
            && channels.filter { $0 == "orch/reconnect" }.count == 2
    }
    try await Task.sleep(nanoseconds: 30_000_000)
    let channels = transport.envelopes().map(\.ch)
    try require(channels.filter { $0.hasPrefix("s/reconnect/") }.count == 2,
                "one reconnect generation has one session observer publication")
    try require(channels.filter { $0 == "orch/reconnect" }.count == 2,
                "one reconnect generation has one orchestrator publication")
    await attachCloudBridgeForTest(nil)
    try await waitForCloudAppBridge("reconnect test shutdown completes") {
        transport.state().stopped
    }
    return checks
}

private func runCloudAppBridgeConcreteReconnectTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }

    let signingKey = CloudDeviceKeyPair()
    let masterSecret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x55, count: 32))
    let relay = CloudLoopbackRelay(
        account: "bridge-account", deviceID: "bridge-machine",
        devicePublicKey: signingKey.publicKeyRaw, allowedTokens: ["bridge-token"]
    )
    let transport = CloudTransport(
        relayBaseURL: URL(string: "ws://loopback.invalid/v1/connect")!,
        tokenProvider: CloudAppBridgeTestTokenProvider(token: CloudDeviceToken(
            value: "bridge-token", expiresAt: Date().addingTimeInterval(3_600)
        )),
        keyProvider: CloudStaticTransportKeys(
            deviceKey: signingKey, masterSecrets: ["ms-1": masterSecret], pairedDevices: [:]
        ),
        connector: CloudLoopbackSocketConnector(relay: relay),
        initialBackoff: 0.01,
        maximumBackoff: 0.02
    )
    let bridge = CloudAppBridge(
        transport: transport,
        identity: CloudAppIdentity(
            machineID: "concrete", deviceID: "bridge-machine", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey
        ),
        sequencing: CloudAppBridgeTestSequence()
    )
    RemoteServer.cloudSnapshotDataForTesting = try cloudAppBridgeTestSnapshots(
        sessionID: "concrete-reconnect"
    )
    defer { RemoteServer.cloudSnapshotDataForTesting = nil }

    await attachCloudBridgeForTest(bridge)
    await RemoteServer.shared.awaitCloudBridgeLifecycle()
    try await waitForCloudAppBridge("concrete initial ready publishes fresh snapshots") {
        let channels = await relay.publishedEnvelopes().map(\.ch)
        return channels.filter { $0.hasPrefix("s/concrete/") }.count == 1
            && channels.filter { $0 == "orch/concrete" }.count == 1
    }
    await relay.dropConnections()
    try await waitForCloudAppBridge("concrete reconnect republishes fresh snapshots") {
        let channels = await relay.publishedEnvelopes().map(\.ch)
        return channels.filter { $0.hasPrefix("s/concrete/") }.count == 2
            && channels.filter { $0 == "orch/concrete" }.count == 2
    }
    let handshakes = await relay.completedHandshakes()
    try require(handshakes >= 2,
                "concrete CloudTransport reconnect completes a second signed handshake")
    let channels = await relay.publishedEnvelopes().map(\.ch)
    try require(channels.filter { $0.hasPrefix("s/concrete/") }.count == 2
                && channels.filter { $0 == "orch/concrete" }.count == 2,
                "concrete Transport to Bridge to RemoteServer path publishes one fresh pair per ready generation")
    await attachCloudBridgeForTest(nil)
    await RemoteServer.shared.awaitCloudBridgeLifecycle()
    let state = await transport.currentState()
    try require(state == .shutDown,
                "concrete reconnect path shutdown owns the active transport lifecycle")
    await relay.stop()
    return checks
}

/// What a Cloud viewer is actually given, which no other check in this file looks at.
///
/// Every suite above hands the bridge `RemoteServer.cloudSnapshotDataForTesting`, so the bytes
/// they publish are the test's own and would go on passing with the Mac publishing nothing at
/// all. The body a real Mac sends comes from `RemoteServer.orchestratorSnapshot()`, and this is
/// the only place that reads it.
///
/// **Failures are collected rather than thrown at the first one, and that is deliberate.**
/// `CLAWDLINE_TEST_GROUPS` exits at `Tests/CloudTestRunner.swift:80`, before the `Task` at `:109`
/// that starts the twelve Cloud suites, so nothing in this file can be selected and every red
/// here costs a whole-tree compile. Stopping at the first failure would have made proving these
/// six checks a six-compile battery on a machine that allows one compile at a time.
private func runCloudAppBridgeSnapshotTests() async throws -> Int {
    var checks = 0
    var problems: [String] = []
    func require(_ condition: @autoclosure () -> Bool, _ message: String) {
        checks += 1
        if !condition() { problems.append(message) }
    }

    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-cloud-snapshot-\(UUID().uuidString)", isDirectory: true)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    let previousScheduleDirectory = Orchestrator.scheduleDirectoryOverrideForTesting
    defer {
        Orchestrator.scheduleDirectoryOverrideForTesting = previousScheduleDirectory
        try? FileManager.default.removeItem(at: directory)
    }
    Orchestrator.scheduleDirectoryOverrideForTesting = directory

    let bare = await MainActor.run { RemoteServer.orchestratorSnapshot() }
    require((bare["schedules"] as? [[String: Any]])?.isEmpty == true,
            "a Mac with no schedule files publishes the field holding an empty list — which is "
                + "what lets a viewer tell it apart from a Mac that publishes no field at all")

    let id = "cccccccc-dddd-4eee-8fff-aaaaaaaaaaaa"
    let source: [String: Any] = [
        "clawdline_schedule": 1, "schedule_id": id, "title": "nightly cloud read",
        "when": ["at": "01:30", "days": "daily"],
        "task": ["assistant": "codex", "project_dir": "/tmp",
                 "instructions": "do the nightly work"],
        "enabled": true,
    ]
    try JSONSerialization.data(withJSONObject: source)
        .write(to: directory.appendingPathComponent("\(id).json"))
    try Data("not json".utf8).write(to: directory.appendingPathComponent("broken.json"))

    let snapshot = await MainActor.run { RemoteServer.orchestratorSnapshot() }
    let published = snapshot["schedules"] as? [[String: Any]] ?? []
    require(published.contains {
        $0["id"] as? String == id && $0["title"] as? String == "nightly cloud read"
    }, "the snapshot a Cloud viewer receives carries the schedules this Mac actually has")
    require(published.contains {
        $0["file"] as? String == "broken.json" && $0["state"] as? String == "invalid"
    }, "and the unreadable ones, so a Cloud viewer is not shown a shorter list than the Mac's")
    require(snapshot["tasks"] is [[String: Any]],
            "the task list the snapshot already carried is still there beside them")

    let listing = await MainActor.run {
        RemoteServer.shared.route(remoteRequest(
            "GET", "/v1/orchestrator/schedules",
            headers: ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]))
    }
    let listed = ((try? JSONSerialization.jsonObject(with: listing.body)) as? [String: Any])?[
        "schedules"] as? [[String: Any]] ?? []
    func names(_ rows: [[String: Any]]) -> [String] {
        rows.map { ($0["id"] as? String) ?? ($0["file"] as? String) ?? "" }
    }
    require(!listed.isEmpty && names(listed) == names(published),
            "the cloud snapshot and GET /v1/orchestrator/schedules answer one inventory, "
                + "so a phone away from the Mac and a browser on its network read the same list")

    let health = await MainActor.run {
        RemoteServer.shared.route(remoteRequest("GET", "/v1/health"))
    }
    let healthBody = (try? JSONSerialization.jsonObject(with: health.body)) as? [String: Any] ?? [:]
    let stamp = snapshot["app"] as? [String: Any]
    require(stamp?["build"] as? Int == healthBody["build"] as? Int
                && stamp?["version"] as? String == healthBody["version"] as? String
                && stamp?["protocol"] as? Int == healthBody["protocol"] as? Int,
            "the build stamp on the snapshot is the reading /v1/health answers on the direct "
                + "path, which is the comparison Build.saw makes to raise the stale banner")
    require(stamp != nil && stamp?["write"] == nil && stamp?["instance"] == nil,
            "and carries neither this Mac's write switch nor its instance id into a viewer's "
                + "hello, because a viewer's write state is its own device capability")

    if !problems.isEmpty {
        throw CloudAppBridgeTestFailure(description: problems.joined(separator: " ✗ "))
    }
    return checks
}

func runCloudAppBridgeTests() async throws -> Int {
    switch ProcessInfo.processInfo.environment["CLAWDLINE_CLOUD_BRIDGE_CASE"] {
    case "base": return try await runCloudAppBridgeBaseTests()
    case "lifecycle": return try await runCloudAppBridgeLifecycleTests()
    case "transitive-lifecycle": return try await runCloudAppBridgeTransitiveLifecycleTests()
    case "publication-lifecycle": return try await runCloudAppBridgePublicationLifecycleTests()
    case "reconnect": return try await runCloudAppBridgeReconnectTests()
    case "concrete-reconnect": return try await runCloudAppBridgeConcreteReconnectTests()
    case "aba": return try await runCloudAppBridgeABATests()
    case "reads": return try await runCloudAppBridgeReadTests()
    case "images": return try await runCloudAppBridgeImageTests()
    case "snapshot": return try await runCloudAppBridgeSnapshotTests()
    default:
        let base = try await runCloudAppBridgeBaseTests()
        let lifecycle = try await runCloudAppBridgeLifecycleTests()
        let transitiveLifecycle = try await runCloudAppBridgeTransitiveLifecycleTests()
        let publicationLifecycle = try await runCloudAppBridgePublicationLifecycleTests()
        let reconnect = try await runCloudAppBridgeReconnectTests()
        let concreteReconnect = try await runCloudAppBridgeConcreteReconnectTests()
        let aba = try await runCloudAppBridgeABATests()
        let reads = try await runCloudAppBridgeReadTests()
        let images = try await runCloudAppBridgeImageTests()
        let snapshot = try await runCloudAppBridgeSnapshotTests()
        return base + lifecycle + transitiveLifecycle + publicationLifecycle
            + reconnect + concreteReconnect + aba + reads + images + snapshot
    }
}

private func waitForCloudAppBridgeSemaphore(_ semaphore: DispatchSemaphore) async {
    await withCheckedContinuation { continuation in
        DispatchQueue.global(qos: .userInitiated).async {
            semaphore.wait()
            continuation.resume()
        }
    }
}

private func attachCloudBridgeForTest(_ bridge: CloudAppBridge?) async {
    await MainActor.run {
        RemoteServer.shared.attachCloudBridge(bridge)
    }
}

private func cloudAppBridgeTestSnapshots(
    sessionID: String?
) throws -> (sessions: Data, orchestrator: Data) {
    let rows: [[String: Any]] = sessionID.map {
        [["id": $0, "label": "Reconnect", "state": "idle"]]
    } ?? []
    return (
        try JSONSerialization.data(withJSONObject: [
            "sessions": rows, "at": 200,
            "scan": ["generation": 1, "complete": true, "emptyAuthoritative": rows.isEmpty],
        ]),
        try JSONSerialization.data(withJSONObject: ["tasks": [], "at": 201])
    )
}

private func waitForCloudAppBridge(
    _ description: String,
    timeout: TimeInterval = 3,
    condition: @escaping () async -> Bool
) async throws {
    let deadline = Date().addingTimeInterval(timeout)
    while Date() < deadline {
        if await condition() { return }
        try await Task.sleep(nanoseconds: 10_000_000)
    }
    throw CloudAppBridgeTestFailure(description: "timed out: \(description)")
}

#if CLOUD_APP_BRIDGE_STANDALONE
@main
private enum CloudAppBridgeTestMain {
    static func main() async throws {
        let count = try await runCloudAppBridgeTests()
        print("\(count) CloudAppBridge checks passed")
    }
}
#endif

/// The reads a browser on the cloud path could not make.
///
/// The whole round trip, minus a relay: what the bridge accepts on the command channel, which
/// door it sends it through, and what it publishes back. Nothing had ever published a `t/`
/// envelope, so the two things being proved here are that one now exists and that a refusal
/// becomes one too — a viewer that is told nothing waits behind a skeleton forever, which is
/// exactly what a phone on this path used to do.
private func runCloudAppBridgeReadTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }

    let signingKey = CloudDeviceKeyPair()
    let masterSecret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x53, count: 32))
    let transport = CloudAppBridgeTestTransport()
    let router = CloudAppBridgeTestRouter()
    let gate = CloudAppBridgeTestGate()
    let results = CloudAppBridgeTestResults()
    let bridge = CloudAppBridge(
        transport: transport,
        identity: CloudAppIdentity(
            machineID: "Mac / 台灣", deviceID: "machine-device", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey
        ),
        sequencing: CloudAppBridgeTestSequence(),
        allowCloudCommands: { gate.get() },
        commandRouter: router,
        nowMilliseconds: { 1_787_740_000_000 },
        commandResult: { results.append($0) }
    )
    try await bridge.start()

    func opened(_ envelope: CloudEnvelope) throws -> [String: Any] {
        let clear = try envelope.open(masterSecret: masterSecret, publicKeyForSender: {
            $0 == "machine-device" ? signingKey.publicKeyRaw : nil
        })
        return (try JSONSerialization.jsonObject(with: clear)) as? [String: Any] ?? [:]
    }

    // The write switch is off for the whole of this suite. A transcript read is still answered:
    // on the direct path a paired device reads one with that switch off, and the session rows
    // this bridge publishes cross without consulting it either.
    try require(gate.get() == false, "the write switch is off for every read below")
    await router.answerReadsWith(CloudReadResult(
        status: 200, body: Data(#"{"messages":[{"role":"user"}],"revision":"7"}"#.utf8)
    ))
    transport.yield(#"{"type":"transcript","session":"plain","limit":200}"#, sequence: 20)
    try await waitForCloudAppBridge("a transcript read with the write switch off") {
        await router.recordedReads().count == 1
    }
    let transcriptReads = await router.recordedReads()
    try require(transcriptReads.first?.read == .transcript(session: "plain", limit: 200),
                "the transcript read reaches the broker with its own window")
    try require(transcriptReads.first?.sender == "viewer",
                "a read carries the verified sender the envelope was signed by")
    try require(!results.all().contains(where: { $0.code == "cloud_commands_disabled" }),
                "the write switch does not refuse a read")

    try await waitForCloudAppBridge("the transcript answer to be published") {
        !transport.envelopes().isEmpty
    }
    let transcriptEnvelope = transport.envelopes()[0]
    try require(
        transcriptEnvelope.ch == "t/Mac%20%2F%20%E5%8F%B0%E7%81%A3/plain",
        "the answer goes on the session's own transcript channel, which the viewer subscribes to"
    )
    try require(transcriptEnvelope.envelopeClass == .stream,
                "an answer is a stream envelope; only a viewer publishes ctl")
    let transcriptPayload = try opened(transcriptEnvelope)
    try require(transcriptPayload["read"] as? String == "transcript",
                "the answer names the read it answers")
    try require(transcriptPayload["status"] as? Int == 200, "the answer carries the route's status")
    try require((transcriptPayload["body"] as? [String: Any])?["revision"] as? String == "7",
                "the answer carries the route's own body, unchanged")

    // Full and summary Info are two answers, not one. A summary omits screen, Git and
    // links/deploy, so a full request settled by a summary would be cached as complete.
    await router.answerReadsWith(CloudReadResult(
        status: 200, body: Data(#"{"info":{"session":{"id":"plain"}}}"#.utf8)
    ))
    transport.yield(#"{"type":"info","session":"plain","parts":"summary"}"#, sequence: 21)
    try await waitForCloudAppBridge("the summary Info answer") {
        transport.envelopes().count == 2
    }
    let summaryReads = await router.recordedReads()
    try require(summaryReads.last?.read == .info(session: "plain", parts: "summary"),
                "the summary tier reaches the broker as the tier that was asked for")
    let summaryPayload = try opened(transport.envelopes()[1])
    try require(summaryPayload["read"] as? String == "info.summary",
                "the summary answer is named apart from the full one")

    // A refusal is published too, and it is the route's own typed code rather than a new one.
    await router.answerReadsWith(CloudReadResult(
        status: 404,
        body: Data(#"{"error":{"code":"not_found","message":"No session named that"}}"#.utf8)
    ))
    transport.yield(#"{"type":"info","session":"gone","parts":"full"}"#, sequence: 22)
    try await waitForCloudAppBridge("the refused Info answer") {
        transport.envelopes().count == 3
    }
    let refusal = try opened(transport.envelopes()[2])
    try require(refusal["read"] as? String == "info.full",
                "a refusal names the read it refuses, so the right waiter hears it")
    try require(refusal["status"] as? Int == 404, "a refusal carries the route's status")
    try require((refusal["error"] as? [String: Any])?["code"] as? String == "not_found",
                "a refused read crosses as a typed code rather than as an empty view")
    try require(!refusal.keys.contains("body"),
                "a refusal publishes no body to be mistaken for one")
    try require(results.all().contains(where: { $0.code == "not_found" }),
                "the refusal is observable at the bridge as well as on the channel")

    // And the other four, which were `cloud_read_unavailable` — a typed refusal, and not an
    // answer. The Git panel first, because it is the one the person opens most and the one whose
    // page already branches on a code of the Mac's.
    await router.answerReadsWith(CloudReadResult(
        status: 200, body: Data(#"{"git":{"branch":"main","clean":true,"files":[]}}"#.utf8)
    ))
    transport.yield(#"{"type":"git","session":"plain"}"#, sequence: 23)
    try await waitForCloudAppBridge("the Git answer") { transport.envelopes().count == 4 }
    let gitReads = await router.recordedReads()
    try require(gitReads.last?.read == .git(session: "plain"),
                "the Git panel reaches the broker as a read of its own, with no window to name")
    let gitPayload = try opened(transport.envelopes()[3])
    try require(gitPayload["read"] as? String == "git", "the Git answer names itself")
    try require(((gitPayload["body"] as? [String: Any])?["git"] as? [String: Any])?["branch"]
                    as? String == "main",
                "and carries the route's own body, which is what lets git-panel.js keep its shape")

    // A refusal the Git panel already has a sentence for. `not_a_repo` is not a cloud word and is
    // not translated into one: the page branches on it over the tunnel and branches on it here.
    await router.answerReadsWith(CloudReadResult(
        status: 404,
        body: Data(#"{"error":{"code":"not_a_repo","message":"Not inside a Git repository"}}"#.utf8)
    ))
    transport.yield(#"{"type":"git","session":"plain"}"#, sequence: 24)
    try await waitForCloudAppBridge("the refused Git answer") { transport.envelopes().count == 5 }
    let notARepo = try opened(transport.envelopes()[4])
    try require((notARepo["error"] as? [String: Any])?["code"] as? String == "not_a_repo",
                "a session outside a repository crosses as the route's own code")

    await router.answerReadsWith(CloudReadResult(
        status: 200, body: Data(#"{"skills":[{"name":"/run"}]}"#.utf8)
    ))
    transport.yield(#"{"type":"skills","session":"plain"}"#, sequence: 25)
    try await waitForCloudAppBridge("the skills answer") { transport.envelopes().count == 6 }
    let skillsRead = await router.recordedReads().last?.read
    try require(skillsRead == .skills(session: "plain"),
                "the composer's menu is a read of the session and nothing else")
    let skillsName = try opened(transport.envelopes()[5])["read"] as? String
    try require(skillsName == "skills", "the skills answer names itself")

    await router.answerReadsWith(CloudReadResult(
        status: 200, body: Data(#"{"text":"building","ended":false}"#.utf8)
    ))
    transport.yield(#"{"type":"shell","session":"plain","shell":"sh-9","bytes":65536}"#,
                    sequence: 26)
    try await waitForCloudAppBridge("the shell answer") { transport.envelopes().count == 7 }
    let shellRead = await router.recordedReads().last?.read
    try require(shellRead == .shell(session: "plain", shell: "sh-9", bytes: 65536),
                "a command's tail reaches the broker with the bound it was asked for")
    let shellName = try opened(transport.envelopes()[6])["read"] as? String
    try require(shellName == "shell:sh-9", "and its answer names the command, not the kind")

    // The decisive one, and the reason `name` carries an id at all. Two agents in one session
    // answer on that session's single channel; a viewer waiting on both can only tell the answers
    // apart by the name, so the name has to be the agent and not the word "agent".
    await router.answerReadsWith(CloudReadResult(
        status: 200, body: Data(#"{"agent":{"id":"a-1"},"entries":[]}"#.utf8)
    ))
    transport.yield(#"{"type":"agent","session":"plain","agent":"a-1","limit":200}"#, sequence: 27)
    try await waitForCloudAppBridge("the first agent answer") { transport.envelopes().count == 8 }
    transport.yield(#"{"type":"agent","session":"plain","agent":"a-2","limit":200}"#, sequence: 28)
    try await waitForCloudAppBridge("the second agent answer") { transport.envelopes().count == 9 }
    let agentNames = try [8, 9].map { try opened(transport.envelopes()[$0 - 1])["read"] as? String }
    try require(agentNames == ["agent:a-1", "agent:a-2"],
                "two agents in one session are two names, so neither settles the other's waiter")
    let secondAgentRead = await router.recordedReads().last?.read
    try require(secondAgentRead == .agent(session: "plain", agent: "a-2", limit: 200),
                "and the second reaches the broker as its own agent with its own window")

    // The two lists that have to agree: `readTypes` admits a word, and the switch in `serveRead`
    // has to know it. A member of the set with no case would be refused by the `default` rather
    // than read as the last one, and this is what would say so.
    let wellFormed: [String: String] = [
        "transcript": #"{"type":"transcript","session":"typed","limit":200}"#,
        "info": #"{"type":"info","session":"typed","parts":"full"}"#,
        "agent": #"{"type":"agent","session":"typed","agent":"a","limit":200}"#,
        "shell": #"{"type":"shell","session":"typed","shell":"s","bytes":1024}"#,
        "skills": #"{"type":"skills","session":"typed"}"#,
        "git": #"{"type":"git","session":"typed"}"#,
        // The stub answers every read with the same JSON body and no `Content-Type`, so this one
        // is refused 415 by `imageOutcome` rather than answered. That is the right fixture here:
        // this table asks whether a word admitted by `readTypes` reaches its own case, and a read
        // that is parsed and then refused has answered that question. The picture's own answer,
        // its byte bound and its refusals are checked further down with a real PNG.
        "image": #"{"type":"image","session":"typed","id":"img-1"}"#,
    ]
    try require(Set(wellFormed.keys) == CloudAppBridge.readTypes,
                "every read type this bridge admits has a body written for it here")
    let readsBeforeTyped = await router.recordedReads().count
    let envelopesBeforeTyped = transport.envelopes().count
    var typedSequence: UInt64 = 60
    for type in CloudAppBridge.readTypes.sorted() {
        transport.yield(wellFormed[type]!, sequence: typedSequence)
        typedSequence += 1
    }
    try await waitForCloudAppBridge("every admitted read type to parse and route") {
        await router.recordedReads().count == readsBeforeTyped + wellFormed.count
    }
    // Drained before the malformed table below counts envelopes: an answer still in flight would
    // arrive during that count and be read as a malformed read having published one.
    try await waitForCloudAppBridge("every admitted read type to be answered") {
        transport.envelopes().count == envelopesBeforeTyped + wellFormed.count
    }
    let typedReads = await router.recordedReads().suffix(wellFormed.count)
    let typedNames = Set(typedReads.map(\.read.name))
    try require(typedNames
                    == ["transcript", "info.full", "agent:a", "shell:s", "skills", "git",
                        "image.img-1"],
                "and each parses into the read it names rather than into the switch's last case")

    // Strictness, in the same shape the commands already have: an exact key set, a bounded
    // window, a session that is really there, a tier that is one of two, and the command class
    // the relay bills. None of them may reach the broker.
    let readsBeforeMalformed = await router.recordedReads().count
    let envelopesBeforeMalformed = transport.envelopes().count
    let malformed = [
        #"{"type":"transcript","session":"plain"}"#,
        #"{"type":"transcript","session":"plain","limit":200,"extra":1}"#,
        #"{"type":"transcript","session":"plain","limit":0}"#,
        #"{"type":"transcript","session":"plain","limit":1001}"#,
        #"{"type":"transcript","session":"plain","limit":1.5}"#,
        #"{"type":"transcript","session":"","limit":200}"#,
        #"{"type":"info","session":"plain","parts":"everything"}"#,
        #"{"type":"info","session":"plain"}"#,
        // An agent is a transcript with a name on it, so it is refused everywhere a transcript
        // is and once more besides: without the name there is nothing to read and nothing to
        // publish the answer under.
        #"{"type":"agent","session":"plain","limit":200}"#,
        #"{"type":"agent","session":"plain","agent":"","limit":200}"#,
        #"{"type":"agent","session":"plain","agent":"a-1"}"#,
        #"{"type":"agent","session":"plain","agent":"a-1","limit":0}"#,
        #"{"type":"agent","session":"plain","agent":"a-1","limit":1001}"#,
        #"{"type":"agent","session":"plain","agent":"a-1","limit":200,"parts":"full"}"#,
        // A command's tail is bounded where its route bounds it — 1 KiB to 1 MiB — so a viewer
        // cannot ask this Mac for more of a build log than a phone on the tunnel can.
        #"{"type":"shell","session":"plain","shell":"sh-9"}"#,
        #"{"type":"shell","session":"plain","shell":"","bytes":65536}"#,
        #"{"type":"shell","session":"plain","bytes":65536}"#,
        #"{"type":"shell","session":"plain","shell":"sh-9","bytes":1023}"#,
        #"{"type":"shell","session":"plain","shell":"sh-9","bytes":1048577}"#,
        #"{"type":"shell","session":"plain","shell":"sh-9","bytes":65536.5}"#,
        // Neither of the last two takes a window, and a field they do not read is a field
        // somebody believed they read.
        #"{"type":"skills","session":"plain","limit":200}"#,
        #"{"type":"skills","session":""}"#,
        #"{"type":"git","session":"plain","parts":"summary"}"#,
        #"{"type":"git","session":""}"#,
    ]
    var sequence: UInt64 = 30
    for body in malformed {
        transport.yield(body, sequence: sequence)
        sequence += 1
    }
    transport.yield(#"{"type":"transcript","session":"plain","limit":200}"#,
                    sequence: sequence, commandClass: .dispatch)
    try await waitForCloudAppBridge("every malformed read to be refused") {
        results.all().filter { $0.code == "malformed_read" }.count == malformed.count + 1
    }
    let readsAfterMalformed = await router.recordedReads().count
    try require(readsAfterMalformed == readsBeforeMalformed,
                "no malformed read reaches the broker")
    // The count this asks about is its own delta, not a total. A literal here — it was 15 — spells
    // out every publication above it, so adding one admitted read type turns a check about
    // refusals red for a reason that has nothing to do with refusals.
    try require(transport.envelopes().count == envelopesBeforeMalformed,
                "a refused-before-routing read publishes nothing, having no answer to publish")

    await bridge.stop()

    // What a viewer may actually reach. The path and the query are built from the closed enum
    // rather than sent, so this is the whole surface a paired browser can ask for.
    let transcriptRequest = RemoteServer.Request(
        verifiedCloudRead: .transcript(session: "session/一|?", limit: 50), sender: "viewer"
    )
    try require(transcriptRequest.method == "GET", "a read is a GET and carries no body")
    try require(transcriptRequest.path == "/v1/sessions/session%2F%E4%B8%80%7C%3F/transcript",
                "the session id is encoded the same way the command door encodes it")
    try require(transcriptRequest.query == ["limit": "50"],
                "the window travels as the query the direct path uses")
    try require(transcriptRequest.headers["idempotency-key"] == nil,
                "a read mints no idempotency key, because a retried GET is not a second anything")
    let fullInfoRequest = RemoteServer.Request(
        verifiedCloudRead: .info(session: "plain", parts: "full"), sender: "viewer"
    )
    try require(fullInfoRequest.path == "/v1/sessions/plain/info" && fullInfoRequest.query.isEmpty,
                "full Info is the bare route, exactly as the direct path asks for it")
    let summaryInfoRequest = RemoteServer.Request(
        verifiedCloudRead: .info(session: "plain", parts: "summary"), sender: "viewer"
    )
    try require(summaryInfoRequest.query == ["parts": "summary"],
                "the summary tier is the same query string the local client sends")
    try require(RemoteServer.isTranscriptReading(transcriptRequest.path),
                "a cloud transcript read is classified into the transcript lane")
    try require(RemoteServer.isSlowReading(fullInfoRequest.path),
                "a cloud Info read is classified into the bounded reading lane")

    // The other four, built from the same closed enum. Their ids are escaped the way the direct
    // path's own call sites escape them, so an id holding a slash is one path segment on both.
    let agentRequest = RemoteServer.Request(
        verifiedCloudRead: .agent(session: "plain", agent: "bg/一", limit: 200), sender: "viewer"
    )
    try require(agentRequest.path == "/v1/sessions/plain/agents/bg%2F%E4%B8%80",
                "an agent id is one path segment however it is spelled")
    try require(agentRequest.query == ["limit": "200"],
                "and travels with the window the direct path asks for")
    let shellRequest = RemoteServer.Request(
        verifiedCloudRead: .shell(session: "plain", shell: "sh-9", bytes: 65536), sender: "viewer"
    )
    try require(shellRequest.path == "/v1/sessions/plain/shells/sh-9"
                    && shellRequest.query == ["bytes": "65536"],
                "a command's tail names its bound in the query the route reads it from")
    let skillsRequest = RemoteServer.Request(
        verifiedCloudRead: .skills(session: "plain"), sender: "viewer"
    )
    try require(skillsRequest.path == "/v1/sessions/plain/skills" && skillsRequest.query.isEmpty,
                "skills is the bare route, exactly as the composer asks for it")
    let gitRequest = RemoteServer.Request(
        verifiedCloudRead: .git(session: "plain"), sender: "viewer"
    )
    try require(gitRequest.path == "/v1/sessions/plain/git" && gitRequest.query.isEmpty,
                "and so is the Git panel")
    try require(gitRequest.method == "GET" && gitRequest.headers["idempotency-key"] == nil,
                "none of the four mints an idempotency key, because none of them makes anything happen")

    // Where they queue, which is the shared queue — and that is not this door's decision, it is
    // the direct path's. Both lane predicates refuse these four paths over HTTP too, so sending
    // them to a lane here would be a second policy nobody measured.
    for path in [agentRequest.path, shellRequest.path, skillsRequest.path, gitRequest.path] {
        try require(!RemoteServer.isTranscriptReading(path) && !RemoteServer.isSlowReading(path),
                    "\(path) is not a lane read here, because it is not one on the direct path")
    }

    return checks
}

/// A picture crossing the transport, and one that could not.
///
/// Row 19 of the Cloud enumeration, and the only one whose failure was in the transport itself:
/// `transcript-images.js` builds `<img src="/v1/artifacts/images/:id">`, which is same-origin and
/// relative, and on this path the origin is a hosted console with no such route. So every image in
/// every transcript was a broken-image icon — the worst answer available, because it looks like
/// the reader's own fault.
///
/// What is proved here, in order: the bound is arithmetic rather than a preference; the real route
/// answers a cloud image read with the stored bytes; those bytes reach the session's own channel
/// as base64 under the picture's own name; and a picture over the bound is published as a typed
/// refusal carrying both numbers, with the bytes left at home.
private func runCloudAppBridgeImageTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }

    // The bound, which is the only genuinely new decision in this feature. The relay caps one
    // envelope's ciphertext at 16 MiB on every tier; AES-GCM adds a tag, base64 costs four bytes
    // for three, and the JSON around it is measured rather than guessed. Everything else follows.
    let ceiling = CloudAppBridge.cloudImageMaxEncodedBytes
    try require(ceiling == 12_582_132,
                "the image ceiling is derived from the relay's envelope cap, not chosen")
    let sealed = ((ceiling + 2) / 3) * 4 + CloudAppBridge.cloudImageAnswerOverhead
        + CloudEnvelope.tagByteCount
    try require(sealed <= CloudAppBridge.cloudEnvelopeCiphertextLimit,
                "a picture at the ceiling still seals inside one envelope")
    let storeCeiling = SessionImageArtifactStore.productionPolicy.maxEncodedBytes
    try require(storeCeiling - ceiling == 780,
                "and it sits 780 bytes under the store's own, so all but the last kilobyte of "
                + "what this Mac will ever hold does cross")

    // The Mac side with nothing faked: a real artifact in the real store, read through the real
    // route by the Request the closed enum builds.
    let directory = isolatedTestSessionImagesDirectory
    try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    let source = directory.appendingPathComponent("cloud-image-source.png")
    guard let fixture = exactPixelPNG(width: 6, height: 4, rgba: (200, 40, 90, 255)) else {
        throw CloudAppBridgeTestFailure(description: "the PNG fixture encodes")
    }
    try fixture.write(to: source)
    guard let stored = try SessionImageArtifactStore().importPaths([source.path]).first else {
        throw CloudAppBridgeTestFailure(description: "the PNG fixture is stored as an artifact")
    }
    let served = await RemoteServer.shared.routeVerifiedCloudRead(
        .image(session: "plain", id: stored.artifact.id), sender: "viewer")
    try require(served.status == 200, "a stored artifact answers a verified cloud image read")
    try require(served.headers["Content-Type"] == "image/png",
                "the route names the media type, which is the only place the bridge learns it")
    try require(served.body.starts(with: [0x89, 0x50, 0x4e, 0x47])
                    && served.body.count == stored.artifact.byteCount,
                "the answer is the stored PNG itself and all of it")
    let absent = await RemoteServer.shared.routeVerifiedCloudRead(
        .image(session: "plain", id: "99999999-8888-4777-8666-555555555555"), sender: "viewer")
    try require(absent.status == 404,
                "an id nothing stored meets the same typed refusal the direct path meets")

    let signingKey = CloudDeviceKeyPair()
    let masterSecret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x69, count: 32))
    let transport = CloudAppBridgeTestTransport()
    let router = CloudAppBridgeTestRouter()
    let gate = CloudAppBridgeTestGate()
    let results = CloudAppBridgeTestResults()
    let bridge = CloudAppBridge(
        transport: transport,
        identity: CloudAppIdentity(
            machineID: "Mac / 台灣", deviceID: "machine-device", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey
        ),
        sequencing: CloudAppBridgeTestSequence(),
        allowCloudCommands: { gate.get() },
        commandRouter: router,
        nowMilliseconds: { 1_787_740_000_000 },
        commandResult: { results.append($0) }
    )
    try await bridge.start()
    defer { CloudAppBridge.cloudImageMaxEncodedBytesForTesting = nil }

    func opened(_ envelope: CloudEnvelope) throws -> [String: Any] {
        let clear = try envelope.open(masterSecret: masterSecret, publicKeyForSender: {
            $0 == "machine-device" ? signingKey.publicKeyRaw : nil
        })
        return (try JSONSerialization.jsonObject(with: clear)) as? [String: Any] ?? [:]
    }

    // A picture crossing. The write switch is off throughout, exactly as it is for the other two
    // reads: looking at a picture somebody already sent types into nothing.
    try require(gate.get() == false, "the write switch is off for every image read below")
    let first = "11111111-2222-4333-8444-555555555555"
    let second = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    await router.answerReadsWith(CloudReadResult(
        status: 200, body: fixture, contentType: "image/png"))
    transport.yield(#"{"type":"image","session":"plain","id":"\#(first)"}"#, sequence: 40)
    try await waitForCloudAppBridge("the picture to be published") {
        !transport.envelopes().isEmpty
    }
    let imageReads = await router.recordedReads()
    try require(imageReads.first?.read == .image(session: "plain", id: first),
                "the image read reaches the broker naming the artifact the transcript named")
    let carried = transport.envelopes()[0]
    try require(carried.ch == "t/Mac%20%2F%20%E5%8F%B0%E7%81%A3/plain",
                "a picture comes back on the session's own channel, which the viewer is on")
    let payload = try opened(carried)
    try require(payload["read"] as? String == "image." + first,
                "the answer is named for this picture, so a transcript's images cannot cross-settle")
    try require(payload["status"] as? Int == 200, "a picture that crossed says 200")
    let body = payload["body"] as? [String: Any]
    try require(body?["media_type"] as? String == "image/png",
                "the media type crosses in a payload field, an envelope having no headers")
    try require(body?["byte_count"] as? Int == fixture.count,
                "the count crosses beside the bytes so a truncated answer is visible as one")
    try require(Data(base64Encoded: (body?["data"] as? String) ?? "") == fixture,
                "and the bytes themselves are the picture, base64 and unchanged")

    // Two pictures asked for together come back under two names. A transcript holds up to six per
    // message across two hundred messages, and they are answered in whatever order the disk gives.
    transport.yield(#"{"type":"image","session":"plain","id":"\#(second)"}"#, sequence: 41)
    try await waitForCloudAppBridge("the second picture") { transport.envelopes().count == 2 }
    let secondPayload = try opened(transport.envelopes()[1])
    try require(secondPayload["read"] as? String == "image." + second,
                "the second picture answers under its own name rather than settling the first")

    // The refusal, which is the half that matters more: whatever the bound is, a picture that
    // cannot cross says so. The ceiling is forced here because the real one sits 780 bytes under
    // the largest artifact this Mac stores — the right number, and an impossible fixture.
    CloudAppBridge.cloudImageMaxEncodedBytesForTesting = fixture.count - 1
    transport.yield(#"{"type":"image","session":"plain","id":"\#(first)"}"#, sequence: 42)
    try await waitForCloudAppBridge("the refusal to be published") {
        transport.envelopes().count == 3
    }
    let refusal = try opened(transport.envelopes()[2])
    try require(refusal["status"] as? Int == 413, "an oversized picture is refused, not dropped")
    let error = refusal["error"] as? [String: Any]
    try require(error?["code"] as? String == "image_too_large_for_cloud",
                "in a code of its own, so the tile can name the size rather than say 'expired'")
    try require(error?["byte_count"] as? Int == fixture.count
                    && error?["limit_bytes"] as? Int == fixture.count - 1,
                "and carrying both numbers, so the page writes its own sentence")
    try require(!refusal.keys.contains("body"),
                "the bytes it refused to carry are not carried anyway")
    try require(results.all().contains(where: { $0.code == "image_too_large_for_cloud" }),
                "the refusal is observable at the bridge as well as on the channel")
    CloudAppBridge.cloudImageMaxEncodedBytesForTesting = nil

    // A route that answered with something that is not a PNG never becomes a blob URL.
    await router.answerReadsWith(CloudReadResult(
        status: 200, body: Data("<svg/>".utf8), contentType: "image/svg+xml"))
    transport.yield(#"{"type":"image","session":"plain","id":"\#(second)"}"#, sequence: 43)
    try await waitForCloudAppBridge("the wrong media type to be refused") {
        transport.envelopes().count == 4
    }
    let wrongType = try opened(transport.envelopes()[3])
    try require(wrongType["status"] as? Int == 415
                    && (wrongType["error"] as? [String: Any])?["code"] as? String
                        == "image_media_type_unsupported",
                "only a PNG crosses, because only a PNG is what this store writes")

    // The route's own refusal is forwarded rather than restated, so an expired picture reads the
    // same word on both transports.
    await router.answerReadsWith(CloudReadResult(
        status: 410,
        body: Data(#"{"error":{"code":"artifact_expired","message":"gone"}}"#.utf8),
        contentType: "application/json; charset=utf-8"))
    transport.yield(#"{"type":"image","session":"plain","id":"\#(first)"}"#, sequence: 44)
    try await waitForCloudAppBridge("the expired picture") { transport.envelopes().count == 5 }
    let expired = try opened(transport.envelopes()[4])
    try require(expired["status"] as? Int == 410
                    && (expired["error"] as? [String: Any])?["code"] as? String
                        == "artifact_expired",
                "expiry crosses as the route's own code, which the tile already has words for")

    // Strictness, in the shape the other reads already have.
    let before = await router.recordedReads().count
    let malformed = [
        #"{"type":"image","session":"plain"}"#,
        #"{"type":"image","session":"plain","id":""}"#,
        #"{"type":"image","session":"","id":"\#(first)"}"#,
        #"{"type":"image","session":"plain","id":"\#(first)","extra":1}"#,
        #"{"type":"image","session":"plain","id":7}"#,
    ]
    var sequence: UInt64 = 50
    for line in malformed {
        transport.yield(line, sequence: sequence)
        sequence += 1
    }
    try await waitForCloudAppBridge("every malformed image read to be refused") {
        results.all().filter { $0.code == "malformed_read" }.count == malformed.count
    }
    let after = await router.recordedReads().count
    try require(after == before, "no malformed image read reaches the broker")
    try require(transport.envelopes().count == 5,
                "and none of them publishes an answer, having none to publish")

    await bridge.stop()

    // What a viewer may reach, built from the closed enum rather than sent.
    let request = RemoteServer.Request(
        verifiedCloudRead: .image(session: "plain", id: "a/../b"), sender: "viewer")
    try require(request.method == "GET" && request.query.isEmpty,
                "an image read is the bare artifact route, as the direct path asks for it")
    try require(request.path == "/v1/artifacts/images/a%2F..%2Fb",
                "an id that is not an opaque name cannot become a path segment of its own")
    try require(!RemoteServer.isTranscriptReading(request.path)
                    && !RemoteServer.isSlowReading(request.path),
                "a picture queues where the direct path's own <img> queues, not in a read lane")

    return checks
}
