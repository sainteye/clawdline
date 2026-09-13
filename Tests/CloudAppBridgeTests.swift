import Foundation
#if CLOUD_APP_BRIDGE_STANDALONE
import AppKit
#endif
struct CloudAppBridgeTestFailure: Error, CustomStringConvertible {
    let description: String
}
actor CloudAppBridgeTestSequence: CloudEnvelopeSequencing {
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
final class CloudAppBridgeTestTransport: CloudTransporting, @unchecked Sendable {
    nonisolated let commands: CloudInboundCommandStream
    nonisolated let readyGenerations: AsyncStream<UInt64>
    nonisolated let outboundReceipts: AsyncStream<CloudOutboundTransportReceipt>
    private let lock = NSLock()
    private let commandQueue: CloudInboundCommandQueue
    private var refusalHandler: CloudTransport.InboundRefusalHandler?
    private var readyContinuation: AsyncStream<UInt64>.Continuation!
    private var receiptContinuation: AsyncStream<CloudOutboundTransportReceipt>.Continuation!
    private var sent: [CloudEnvelope] = []
    private var connected = false
    private var stopped = false
    private var connectStarted = false
    private var connectFinished = false
    private var connectContinuation: CheckedContinuation<Void, Never>?
    private var readyGeneration: UInt64 = 0
    private let suspendConnect: Bool
    private let suspendPublication: Bool
    private let publicationRequiresShutdown: Bool
    private let publicationContinuation: AsyncStream<Void>.Continuation
    private let publicationStream: AsyncStream<Void>
    private var publicationStarts = 0
    private var publicationCancelled = false
    init(
        suspendConnect: Bool = false, suspendPublication: Bool = false,
        publicationRequiresShutdown: Bool = false
    ) {
        self.suspendConnect = suspendConnect
        self.suspendPublication = suspendPublication
        self.publicationRequiresShutdown = publicationRequiresShutdown
        let commandQueue = CloudInboundCommandQueue(limits: CloudInboundCommandQueueLimits(
            maximumCount: 10_000, maximumChargedBytes: Int.max
        ))
        self.commandQueue = commandQueue
        commands = commandQueue.stream
        var readyContinuation: AsyncStream<UInt64>.Continuation!
        readyGenerations = AsyncStream { readyContinuation = $0 }
        self.readyContinuation = readyContinuation
        var receiptContinuation: AsyncStream<CloudOutboundTransportReceipt>.Continuation!
        outboundReceipts = AsyncStream { receiptContinuation = $0 }
        self.receiptContinuation = receiptContinuation
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
                self.observePublicationCancellation()
            }
            if Task.isCancelled { throw CancellationError() }
        }
        append(envelope)
    }
    func sendExactPublishFrame(_ bytes: Data) async throws {
        let frame = try JSONDecoder().decode(CloudPublishFrame.self, from: bytes)
        try await publish(envelope: frame.envelope)
    }
    private func markPublicationStarted() {
        lock.lock()
        publicationStarts += 1
        lock.unlock()
    }
    private func observePublicationCancellation() {
        lock.lock()
        publicationCancelled = true
        let release = !publicationRequiresShutdown
        lock.unlock()
        if release { publicationContinuation.finish() }
    }
    private func append(_ envelope: CloudEnvelope) {
        lock.lock()
        sent.append(envelope)
        lock.unlock()
    }
    func shutdown() async {
        markStopped()
        publicationContinuation.finish()
        commandQueue.finish()
        readyContinuation.finish()
        receiptContinuation.finish()
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
    func releasePublications(_ count: Int = 1) { for _ in 0..<count { publicationContinuation.yield(()) } }
    func yield(_ plaintext: String, sequence: UInt64, timestamp: UInt64 = 1,
               commandClass: CloudEnvelopeClass = .ctl,
               channel: String = "ctl/Mac%20%2F%20%E5%8F%B0%E7%81%A3") {
        _ = commandQueue.admit(CloudInboundCommand(
            channel: channel, sequence: sequence, timestamp: timestamp,
            commandClass: commandClass, sender: "viewer", plaintext: Data(plaintext.utf8)
        ))
    }
    func acknowledge(_ envelope: CloudEnvelope,
                     kind: CloudOutboundTransportReceiptKind = .delivered) {
        receiptContinuation.yield(CloudOutboundTransportReceipt(
            channel: envelope.ch, sequence: Int64(envelope.seq), kind: kind))
    }
    func setInboundRefusalHandler(_ handler: CloudTransport.InboundRefusalHandler?) async {
        replaceRefusalHandler(handler)
    }
    func setTerminalAuthorizationHandler(
        _ handler: CloudTransport.TerminalAuthorizationHandler?
    ) async {}
    private func replaceRefusalHandler(_ handler: CloudTransport.InboundRefusalHandler?) {
        lock.lock()
        refusalHandler = handler
        lock.unlock()
    }
    func refuse(_ command: CloudInboundCommand, reason: CloudInboundAdmissionRefusalReason) async {
        let handler = currentRefusalHandler()
        let metrics = commandQueue.snapshot()
        if let handler {
            handler(CloudInboundAdmissionRefusal(
                command: command, reason: reason, metrics: metrics
            ))
        }
    }
    private func currentRefusalHandler() -> CloudTransport.InboundRefusalHandler? {
        lock.lock()
        defer { lock.unlock() }
        let handler = refusalHandler
        return handler
    }
    func envelopes() -> [CloudEnvelope] {
        lock.lock()
        defer { lock.unlock() }
        return sent
    }
    func state() -> (
        connected: Bool, stopped: Bool, connectStarted: Bool, connectFinished: Bool,
        publicationStarts: Int, publicationCancelled: Bool
    ) {
        lock.lock()
        defer { lock.unlock() }
        return (connected, stopped, connectStarted, connectFinished,
                publicationStarts, publicationCancelled)
    }
}
actor CloudAppBridgeTestRouter: CloudCommandRouting {
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
    private var blockedReadName: String?, blockedReadContinuation: CheckedContinuation<Void, Never>?
    func route(_ command: CloudHeadlessCommand, sender: String,
               idempotencyKey: String) async -> CloudCommandResult {
        calls.append(Call(command: command, sender: sender, idempotencyKey: idempotencyKey))
        return CloudCommandResult(status: 200, code: nil,
                                  body: Data(#"{"ok":true,"id":"opened"}"#.utf8))
    }
    func read(_ read: CloudHeadlessRead, sender: String) async -> CloudReadResult {
        reads.append(ReadCall(read: read, sender: sender))
        if read.name == blockedReadName { await withCheckedContinuation { blockedReadContinuation = $0 } }
        return readAnswer
    }
    func answerReadsWith(_ answer: CloudReadResult) { readAnswer = answer }
    func blockRead(named name: String) { blockedReadName = name }
    func releaseBlockedRead() { blockedReadName = nil; blockedReadContinuation?.resume(); blockedReadContinuation = nil }
    func recorded() -> [Call] { calls }
    func recordedReads() -> [ReadCall] { reads }
}
final class CloudAppBridgeTestGate: @unchecked Sendable {
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

final class CloudAppBridgeTestResults: @unchecked Sendable {
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
    var unchangedSessionPayload = sessionPayload; unchangedSessionPayload["at"] = 125
    try await bridge.publishSessions(JSONSerialization.data(withJSONObject: unchangedSessionPayload))

    let published = transport.envelopes()
    try require(published.count == 4,
                "an unchanged scan republishes nothing beside two rows, inventory and orchestrator")
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
    try require(transport.envelopes().count == 4,
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

    transport.yield(#"{"type":"send","session":"plain","request":"send-off","text":"hello","images":[]}"#,
                    sequence: 10)
    try await waitForCloudAppBridge("default-off command refusal") {
        results.all().contains { $0.code == "cloud_commands_disabled" }
    }
    let defaultOffCalls = await router.recorded()
    try require(defaultOffCalls.isEmpty, "default-off commands never enter the broker")

    let overloadPlaintext = Data(
        #"{"type":"send","session":"plain","request":"ingress-full","text":"later","images":[]}"#.utf8
    )
    let overloadCommand = CloudInboundCommand(
        channel: "ctl/Mac%20%2F%20%E5%8F%B0%E7%81%A3", sequence: 99, timestamp: 1,
        commandClass: .ctl, sender: "viewer", plaintext: overloadPlaintext
    )
    gate.set(true)
    let envelopesBeforeOverload = transport.envelopes().count
    await transport.refuse(overloadCommand, reason: .countCap)
    try await waitForCloudAppBridge("typed ingress overload answer") {
        transport.envelopes().count == envelopesBeforeOverload + 1
    }
    let overloadEnvelope = transport.envelopes().last!
    let overloadBytes = try overloadEnvelope.open(
        masterSecret: masterSecret,
        publicKeyForSender: { $0 == "machine-device" ? signingKey.publicKeyRaw : nil }
    )
    let overloadAnswer = try JSONSerialization.jsonObject(with: overloadBytes) as! [String: Any]
    try require(overloadAnswer["read"] as? String == "action:ingress-full"
                    && overloadAnswer["status"] as? Int == 429,
                "an identifiable pre-admission refusal reaches its request-scoped encrypted answer")
    let overloadError = overloadAnswer["error"] as? [String: Any]
    try require(overloadError?["code"] as? String == "cloud_ingress_busy",
                "the encrypted overload answer carries the stable typed code")
    let overloadRouterCalls = await router.recorded()
    try require(overloadRouterCalls.isEmpty,
                "a command refused before admission never reaches the effect router")

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

    let envelopesBeforeSend = transport.envelopes().count
    transport.yield(#"{"type":"send","session":"plain","request":"send-1","text":"hello","images":[]}"#,
                    sequence: 12)
    try await waitForCloudAppBridge("send execution answer") {
        transport.envelopes().count == envelopesBeforeSend + 1
    }
    let sendAnswer = try transport.envelopes().last?.open(
        masterSecret: masterSecret,
        publicKeyForSender: { $0 == "machine-device" ? signingKey.publicKeyRaw : nil }
    )
    let sendPayload = sendAnswer.flatMap {
        (try? JSONSerialization.jsonObject(with: $0)) as? [String: Any]
    }
    let callsAfterSend = await router.recorded()

    let envelopesBeforeStart = transport.envelopes().count
    transport.yield(#"{"type":"start","session":"__clawdline_machine__","request":"r-1","place":"portfolio","assistant":"claude","model":"sonnet"}"#,
                    sequence: 13)
    try await waitForCloudAppBridge("start command answer") {
        transport.envelopes().count == envelopesBeforeStart + 1
    }
    let startCalls = await router.recorded()
    let startAnswer = try transport.envelopes().last?.open(
        masterSecret: masterSecret,
        publicKeyForSender: { $0 == "machine-device" ? signingKey.publicKeyRaw : nil }
    )
    let startPayload = startAnswer.flatMap {
        (try? JSONSerialization.jsonObject(with: $0)) as? [String: Any]
    }

    let featureBodies = [
        #"{"type":"end","session":"%306","request":"end-1","accept_loss":true,"expected_closeability_version":"close-v1"}"#,
        #"{"type":"resume","session":"__clawdline_machine__","request":"resume-1","place":"portfolio","past":"past/session|一","assistant":"codex"}"#,
        #"{"type":"schedule-create","session":"__clawdline_machine__","request":"create-1","schedule":{"title":"Morning"}}"#,
        #"{"type":"schedule-update","session":"__clawdline_machine__","request":"update-1","id":"morning","schedule":{"title":"Later"}}"#,
        #"{"type":"schedule-delete","session":"__clawdline_machine__","request":"delete-1","id":"morning"}"#,
        #"{"type":"schedule-run","session":"__clawdline_machine__","request":"run-1","id":"morning"}"#,
        #"{"type":"snippet-create","session":"__clawdline_machine__","request":"snippet-create-1","snippet":{"title":"Deploy","body":"commit, push","scope":"global"}}"#,
        #"{"type":"snippet-update","session":"__clawdline_machine__","request":"snippet-update-1","id":"snippet/one","snippet":{"title":"Ship"}}"#,
        #"{"type":"snippet-delete","session":"__clawdline_machine__","request":"snippet-delete-1","id":"snippet/one"}"#,
        #"{"type":"snippet-order","session":"__clawdline_machine__","request":"snippet-order-1","ordering":{"scope":"global","order":["two","one"]}}"#,
        #"{"type":"push-subscribe","session":"__clawdline_machine__","request":"push-1","subscription":{"endpoint":"https://push.example/one","keys":{"p256dh":"key","auth":"auth"}}}"#,
        #"{"type":"push-unsubscribe","session":"__clawdline_machine__","request":"push-2","id":"subscription-1"}"#,
        #"{"type":"push-test","session":"__clawdline_machine__","request":"push-3","target":"plain"}"#,
        #"{"type":"voice","session":"__clawdline_machine__","request":"voice-1","audio":"AAEC","rate":16000}"#,
    ]
    let featureCallsBefore = await router.recorded().count
    let featureEnvelopesBefore = transport.envelopes().count
    for (offset, body) in featureBodies.enumerated() {
        transport.yield(body, sequence: UInt64(20 + offset))
    }
    try await waitForCloudAppBridge("cloud controls to parse and answer") {
        await router.recorded().count == featureCallsBefore + featureBodies.count
            && transport.envelopes().count == featureEnvelopesBefore + featureBodies.count
    }
    let featureCommands = Array((await router.recorded()).suffix(featureBodies.count)).map(\.command)
    let parsedSnippetCreate = featureCommands.contains { command in
        guard case .snippetCreate(let data) = command,
              let body = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
        else { return false }
        return body["title"] as? String == "Deploy" && body["scope"] as? String == "global"
    }
    let parsedSnippetUpdate = featureCommands.contains { command in
        guard case .snippetUpdate(let id, let data) = command,
              let body = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
        else { return false }
        return id == "snippet/one" && body["title"] as? String == "Ship"
    }
    let parsedSnippetOrder = featureCommands.contains { command in
        guard case .snippetOrder(let data) = command,
              let body = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
        else { return false }
        return body["scope"] as? String == "global"
            && body["order"] as? [String] == ["two", "one"]
    }

    let scheduleRunRoute = CloudLocalRoute(command: .scheduleRun(id: "morning/one"))
    try require(scheduleRunRoute.method == "POST"
                    && scheduleRunRoute.path
                        == "/v1/orchestrator/schedules/morning%2Fone/run",
                "the closed Run now command maps only to the named local schedule route")

    let callsBeforeMalformedSchedules = await router.recorded().count
    let malformedSchedules = [
        #"{"type":"schedule-run","session":"__clawdline_machine__","request":"bad-run-1","id":""}"#,
        #"{"type":"schedule-run","session":"__clawdline_machine__","request":"bad-run-2","id":"morning","path":"/v1/orchestrator/tasks"}"#,
    ]
    for (offset, body) in malformedSchedules.enumerated() {
        transport.yield(body, sequence: UInt64(50 + offset))
    }
    try await waitForCloudAppBridge("malformed schedule-run commands to be refused") {
        results.all().filter { $0.code == "malformed_command" }.count
            >= malformedSchedules.count
    }
    let callsAfterMalformedSchedules = await router.recorded().count
    try require(callsAfterMalformedSchedules == callsBeforeMalformedSchedules,
                "a malformed Run now command cannot choose or reach a local route")

    let callsBeforeMalformedSnippets = await router.recorded().count
    let malformedSnippets = [
        #"{"type":"snippet-create","session":"__clawdline_machine__","request":"bad-1"}"#,
        #"{"type":"snippet-update","session":"__clawdline_machine__","request":"bad-2","id":"","snippet":{"title":"x"}}"#,
        #"{"type":"snippet-delete","session":"__clawdline_machine__","request":"bad-3","id":"snippet","extra":true}"#,
        #"{"type":"snippet-order","session":"__clawdline_machine__","request":"bad-4","ordering":[]}"#,
    ]
    for (offset, body) in malformedSnippets.enumerated() {
        transport.yield(body, sequence: UInt64(60 + offset))
    }
    try await waitForCloudAppBridge("malformed snippet commands to be refused") {
        results.all().filter { $0.code == "malformed_command" }.count
            >= malformedSnippets.count
    }
    let callsAfterMalformedSnippets = await router.recorded().count
    try require(callsAfterMalformedSnippets == callsBeforeMalformedSnippets,
                "no malformed snippet mutation reaches the local router")

    transport.yield("not json", sequence: 14)
    transport.yield(#"{"type":"erase","session":"plain"}"#, sequence: 15)
    transport.yield(#"{"type":"dispatch","task":{"title":"not pinned"}}"#,
                    sequence: 16, commandClass: .dispatch)
    try await waitForCloudAppBridge("typed command refusals") {
        let codes = results.all().compactMap(\.code)
        return codes.contains("malformed_command") && codes.contains("unknown_command")
            && codes.contains("cloud_dispatch_unpinned")
    }
    let refusedCalls = await router.recorded()
    try require(refusedCalls.count == 3 + featureBodies.count
                    && callsAfterSend.last?.command
                        == .send(session: "plain", text: "hello", images: [])
                    && sendPayload?["read"] as? String == "action:send-1"
                    && featureCommands.contains(.end(session: "%306", acceptLoss: true,
                                                     closeabilityVersion: "close-v1"))
                    && featureCommands.contains(.resume(place: "portfolio",
                                                        session: "past/session|一",
                                                        assistant: "codex"))
                    && featureCommands.contains(.scheduleDelete(id: "morning"))
                    && featureCommands.contains(.scheduleRun(id: "morning"))
                    && parsedSnippetCreate && parsedSnippetUpdate
                    && featureCommands.contains(.snippetDelete(id: "snippet/one"))
                    && parsedSnippetOrder
                    && featureCommands.contains(.pushUnsubscribe(id: "subscription-1"))
                    && featureCommands.contains(.pushTest(session: "plain"))
                    && featureCommands.contains(.voice(audio: "AAEC", rate: 16000))
                    && startCalls.last?.command
                        == .start(place: "portfolio", assistant: "claude", model: "sonnet")
                    && startPayload?["read"] as? String == "action:r-1"
                    && (startPayload?["body"] as? [String: Any])?["id"] as? String == "opened",
                "only admitted commands reach execution, and start returns its new session id")

    await bridge.stop()
    try require(transport.state().stopped, "bridge shutdown stops the transport")
    let bridgeRunning = await bridge.isRunning()
    try require(bridgeRunning == false, "bridge lifecycle ends after stream shutdown")

    return checks
}

/// R-1 correction: ingress refusals reuse the normal gates and safe read identity, while every
/// refusal publication enters one bounded, deadline-governed lane instead of blocking its caller.
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
        return channels.filter { $0.hasPrefix("s/reconnect/") }.count == 2
            && channels.filter { $0 == "orch/reconnect" }.count == 1
    }
    transport.signalReady()
    try await waitForCloudAppBridge("reconnect republishes without a local observation") {
        let channels = transport.envelopes().map(\.ch)
        return channels.filter { $0.hasPrefix("s/reconnect/") }.count == 4
            && channels.filter { $0 == "orch/reconnect" }.count == 2
    }
    try await Task.sleep(nanoseconds: 30_000_000)
    let channels = transport.envelopes().map(\.ch)
    try require(channels.filter { $0.hasPrefix("s/reconnect/") }.count == 4,
                "each reconnect republishes one row and its authoritative inventory")
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
        return channels.filter { $0.hasPrefix("s/concrete/") }.count == 2
            && channels.filter { $0 == "orch/concrete" }.count == 1
    }
    await relay.dropConnections()
    try await waitForCloudAppBridge("concrete reconnect republishes fresh snapshots") {
        let channels = await relay.publishedEnvelopes().map(\.ch)
        return channels.filter { $0.hasPrefix("s/concrete/") }.count == 4
            && channels.filter { $0 == "orch/concrete" }.count == 2
    }
    let handshakes = await relay.completedHandshakes()
    try require(handshakes >= 2,
                "concrete CloudTransport reconnect completes a second signed handshake")
    let channels = await relay.publishedEnvelopes().map(\.ch)
    try require(channels.filter { $0.hasPrefix("s/concrete/") }.count == 4
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

/// Exercises the production `RemoteServer.orchestratorSnapshot()` bytes rather than the other
/// suites' injected snapshots. It collects all failures because this Cloud suite is expensive and
/// cannot be selected through `CLAWDLINE_TEST_GROUPS`.
///
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
    case "durable": return try await runCloudAppBridgeDurableCompositionTests()
    case "lifecycle": return try await runCloudAppBridgeLifecycleTests()
    case "transitive-lifecycle": return try await runCloudAppBridgeTransitiveLifecycleTests()
    case "publication-lifecycle": return try await runCloudAppBridgePublicationLifecycleTests()
    case "reconnect": return try await runCloudAppBridgeReconnectTests()
    case "concrete-reconnect": return try await runCloudAppBridgeConcreteReconnectTests()
    case "aba": return try await runCloudAppBridgeABATests()
    case "reads": return try await runCloudAppBridgeReadTests()
    case "images": return try await runCloudAppBridgeImageTests()
    case "documents": return try await runCloudAppBridgeDocumentTests()
    case "snapshot": return try await runCloudAppBridgeSnapshotTests()
    case "refusals": return try await runCloudCommandRefusalTests()
    case "ingress-refusals": return try await runCloudAppBridgeIngressRefusalTests()
    case "transparency": return try await runCloudBridgeTransparencyTests()
    default:
        let base = try await runCloudAppBridgeBaseTests()
        let durable = try await runCloudAppBridgeDurableCompositionTests()
        let lifecycle = try await runCloudAppBridgeLifecycleTests()
        let transitiveLifecycle = try await runCloudAppBridgeTransitiveLifecycleTests()
        let publicationLifecycle = try await runCloudAppBridgePublicationLifecycleTests()
        let reconnect = try await runCloudAppBridgeReconnectTests()
        let concreteReconnect = try await runCloudAppBridgeConcreteReconnectTests()
        let aba = try await runCloudAppBridgeABATests()
        let reads = try await runCloudAppBridgeReadTests()
        let images = try await runCloudAppBridgeImageTests()
        let documents = try await runCloudAppBridgeDocumentTests()
        let snapshot = try await runCloudAppBridgeSnapshotTests()
        let refusals = try await runCloudCommandRefusalTests()
        let ingressRefusals = try await runCloudAppBridgeIngressRefusalTests()
        let transparency = try await runCloudBridgeTransparencyTests()
        return base + durable + lifecycle + transitiveLifecycle + publicationLifecycle
            + reconnect + concreteReconnect + aba + reads + images + documents + snapshot
            + refusals + ingressRefusals + transparency
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

func attachCloudBridgeForTest(_ bridge: CloudAppBridge?) async {
    await MainActor.run {
        RemoteServer.shared.attachCloudBridge(bridge)
    }
}

func cloudAppBridgeTestSnapshots(
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

func waitForCloudAppBridge(
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
// The complete suite normally receives these fixtures from its shared test files. Keep the
// standalone focused runner self-contained so compiling one group does not require compiling all
// unrelated test bodies merely to satisfy names in groups it will not execute.
private let isolatedTestSessionImagesDirectory = FileManager.default.temporaryDirectory
    .appendingPathComponent("clawdline-cloud-bridge-images-\(UUID().uuidString)",
                            isDirectory: true)

private func remoteRequest(_ method: String, _ target: String,
                           headers: [String: String] = [:]) -> RemoteServer.Request {
    var head = "\(method) \(target) HTTP/1.1\r\nHost: 127.0.0.1:\(Config.shared.remotePort)\r\n"
    for (key, value) in headers.sorted(by: { $0.key < $1.key }) {
        head += "\(key): \(value)\r\n"
    }
    return RemoteServer.Request(head: Data((head + "\r\n").utf8))!
}

private func exactPixelPNG(width: Int, height: Int,
                           rgba: (UInt8, UInt8, UInt8, UInt8)) -> Data? {
    guard width > 0, height > 0,
          let rep = NSBitmapImageRep(
            bitmapDataPlanes: nil, pixelsWide: width, pixelsHigh: height,
            bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
            colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0),
          let pixels = rep.bitmapData else { return nil }
    for y in 0..<height {
        let row = pixels.advanced(by: y * rep.bytesPerRow)
        for x in 0..<width {
            let pixel = row.advanced(by: x * 4)
            pixel[0] = rgba.0; pixel[1] = rgba.1; pixel[2] = rgba.2; pixel[3] = rgba.3
        }
    }
    return rep.representation(using: .png, properties: [:])
}

#if !CLOUD_INGRESS_COMBINED_STANDALONE
@main
private enum CloudAppBridgeTestMain {
    static func main() async throws {
        let count = try await runCloudAppBridgeTests()
        print("\(count) CloudAppBridge checks passed")
    }
}
#endif
#endif

/// The encrypted round trip for every read a Cloud browser can ask this Mac to perform.
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

    try require(gate.get() == false, "the write switch is off for every read below")
    await router.answerReadsWith(CloudReadResult(
        status: 200, body: Data(#"{"messages":[{"role":"user"}],"revision":"7"}"#.utf8)
    ))
    transport.yield(
        #"{"type":"transcript","session":"plain","limit":200,"priority":"foreground"}"#,
        sequence: 20)
    try await waitForCloudAppBridge("a transcript read with the write switch off") {
        await router.recordedReads().count == 1
    }
    let transcriptReads = await router.recordedReads()
    try require(
        transcriptReads.first?.read
            == .transcript(session: "plain", limit: 200, priority: .foreground),
        "the transcript read reaches the broker with its own window and interactive priority")
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

    let wellFormed: [String: String] = [
        "transcript": #"{"type":"transcript","session":"typed","limit":200}"#,
        "info": #"{"type":"info","session":"typed","parts":"full"}"#,
        "agent": #"{"type":"agent","session":"typed","agent":"a","limit":200}"#,
        "shell": #"{"type":"shell","session":"typed","shell":"s","bytes":1024}"#,
        "skills": #"{"type":"skills","session":"typed"}"#,
        "git": #"{"type":"git","session":"typed"}"#,
        "screen": #"{"type":"screen","session":"typed"}"#,
        "image": #"{"type":"image","session":"typed","id":"img-1"}"#,
        "documents": #"{"type":"documents","session":"typed"}"#,
        "document": #"{"type":"document","session":"typed","request":"p-doc","scope":"project","task":"","path":"notes.md"}"#,
        "board": #"{"type":"board","session":"__clawdline_machine__","request":"p-board","project":"project-1","item":"item-1"}"#,
        "timeline": #"{"type":"timeline","session":"__clawdline_machine__","request":"p-timeline","project":"project-1","entry":"entry-1","cursor":"40","environment":"production","category":"feature","upcoming":true}"#,
        "places": #"{"type":"places","session":"__clawdline_machine__","request":"p-1"}"#,
        "project-worktrees": #"{"type":"project-worktrees","session":"__clawdline_machine__","request":"p-2","project":"/code/app"}"#,
        "past-sessions": #"{"type":"past-sessions","session":"__clawdline_machine__","request":"p-3","place":"portfolio","assistant":"claude"}"#,
        "schedules": #"{"type":"schedules","session":"__clawdline_machine__","request":"p-list"}"#,
        "snippets": #"{"type":"snippets","session":"__clawdline_machine__","request":"p-snippets"}"#,
        "schedule": #"{"type":"schedule","session":"__clawdline_machine__","request":"p-4","id":"morning"}"#,
        "push-key": #"{"type":"push-key","session":"__clawdline_machine__","request":"p-5"}"#,
        "project-worktree-lifecycle": #"{"type":"project-worktree-lifecycle","session":"__clawdline_machine__","request":"p-6","project":"project-0123456789abcdef01234567"}"#, "project-worktree-lifecycle-refresh": #"{"type":"project-worktree-lifecycle-refresh","session":"__clawdline_machine__","request":"p-7","project":"project-0123456789abcdef01234567"}"#,
    ]
    try require(Set(wellFormed.keys) == CloudAppBridge.readTypes, "every read type this bridge admits has a body written for it here")
    var typedSequence: UInt64 = 60
    for type in CloudAppBridge.readTypes.sorted() {
        let readsBeforeType = await router.recordedReads().count
        let envelopesBeforeType = transport.envelopes().count
        transport.yield(wellFormed[type]!, sequence: typedSequence)
        typedSequence += 1
        try await waitForCloudAppBridge("the \(type) read type to parse and route") {
            await router.recordedReads().count == readsBeforeType + 1 }
        try await waitForCloudAppBridge("the \(type) read type to be answered") {
            transport.envelopes().count == envelopesBeforeType + 1 }
    }
    let typedReads = await router.recordedReads().suffix(wellFormed.count)
    let typedNames = Set(typedReads.map(\.read.name))
    try require(
        typedNames
            == ["transcript", "info.full", "agent:a", "shell:s", "skills", "git", "screen",
                "image.img-1", "documents", "read:p-doc", "read:p-1", "read:p-2", "read:p-3", "read:p-4",
                "read:p-5", "read:p-list", "read:p-snippets", "read:p-board", "read:p-timeline", "read:p-6", "read:p-7"]
            && typedReads.contains(CloudAppBridgeTestRouter.ReadCall(
                read: .transcript(session: "typed", limit: 200, priority: .foreground),
                sender: "viewer")),
        "each read parses into its own case, and an old interactive Cloud tab stays foreground")
    let readsBeforeMalformed = await router.recordedReads().count
    let envelopesBeforeMalformed = transport.envelopes().count
    let malformed = [
        #"{"type":"transcript","session":"plain"}"#,
        #"{"type":"transcript","session":"plain","limit":200,"extra":1}"#,
        #"{"type":"transcript","session":"plain","limit":0}"#,
        #"{"type":"transcript","session":"plain","limit":1001}"#,
        #"{"type":"transcript","session":"plain","limit":1.5}"#,
        #"{"type":"transcript","session":"","limit":200}"#,
        #"{"type":"transcript","session":"plain","limit":200,"priority":"urgent"}"#,
        #"{"type":"transcript","session":"plain","limit":200,"priority":1}"#,
        #"{"type":"info","session":"plain","parts":"everything"}"#,
        #"{"type":"info","session":"plain"}"#,
        #"{"type":"agent","session":"plain","limit":200}"#,
        #"{"type":"agent","session":"plain","agent":"","limit":200}"#,
        #"{"type":"agent","session":"plain","agent":"a-1"}"#,
        #"{"type":"agent","session":"plain","agent":"a-1","limit":0}"#,
        #"{"type":"agent","session":"plain","agent":"a-1","limit":1001}"#,
        #"{"type":"agent","session":"plain","agent":"a-1","limit":200,"parts":"full"}"#,
        #"{"type":"shell","session":"plain","shell":"sh-9"}"#,
        #"{"type":"shell","session":"plain","shell":"","bytes":65536}"#,
        #"{"type":"shell","session":"plain","bytes":65536}"#,
        #"{"type":"shell","session":"plain","shell":"sh-9","bytes":1023}"#,
        #"{"type":"shell","session":"plain","shell":"sh-9","bytes":1048577}"#,
        #"{"type":"shell","session":"plain","shell":"sh-9","bytes":65536.5}"#,
        #"{"type":"skills","session":"plain","limit":200}"#,
        #"{"type":"skills","session":""}"#,
        #"{"type":"git","session":"plain","parts":"summary"}"#,
        #"{"type":"git","session":""}"#,
        #"{"type":"screen"}"#,
        #"{"type":"screen","session":"plain","extra":true}"#,
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
    try require(transport.envelopes().count == envelopesBeforeMalformed,
                "a refused-before-routing read publishes nothing, having no answer to publish")

    let readsBeforeBlockedLane = await router.recordedReads().count
    await router.blockRead(named: "git")
    transport.yield(#"{"type":"git","session":"plain"}"#, sequence: 500)
    try await waitForCloudAppBridge("background read enters its lane") {
        await router.recordedReads().count == readsBeforeBlockedLane + 1 }
    transport.yield(#"{"type":"transcript","session":"plain","limit":200,"priority":"foreground"}"#, sequence: 501)
    try await waitForCloudAppBridge("foreground bypasses blocked background") {
        await router.recordedReads().count == readsBeforeBlockedLane + 2 }
    let laneReads = await router.recordedReads().suffix(2)
    await router.releaseBlockedRead()
    try await waitForCloudAppBridge("both lanes finish before capacity checks") {
        transport.envelopes().count == envelopesBeforeMalformed + 2
    }

    func proveBusy(
        _ name: String, limit: Int, sequence start: UInt64,
        body: (Int) -> String
    ) async throws -> (payload: [String: Any], channel: String) {
        let beforeReads = await router.recordedReads().count
        let beforeEnvelopes = transport.envelopes().count
        let beforeBusy = results.all().filter { $0.code == "cloud_read_busy" }.count
        await router.blockRead(named: name)
        for index in 0...limit { transport.yield(body(index), sequence: start + UInt64(index)) }
        try await waitForCloudAppBridge("\(name) lane reaches its bound") {
            await router.recordedReads().count == beforeReads + 1
                && results.all().filter { $0.code == "cloud_read_busy" }.count == beforeBusy + 1
                && transport.envelopes().count == beforeEnvelopes + 1
        }
        let busyEnvelope = transport.envelopes()[beforeEnvelopes]
        let busyPayload = try opened(busyEnvelope)
        await router.releaseBlockedRead()
        try await waitForCloudAppBridge("\(name) lane drains") {
            await router.recordedReads().count == beforeReads + limit
                && transport.envelopes().count == beforeEnvelopes + limit + 1
        }
        transport.yield(body(limit + 1), sequence: start + UInt64(limit + 1))
        try await waitForCloudAppBridge("\(name) lane recovers") {
            await router.recordedReads().count == beforeReads + limit + 1
                && transport.envelopes().count == beforeEnvelopes + limit + 2
        }
        return (busyPayload, busyEnvelope.ch)
    }
    let backgroundBusy = try await proveBusy("git", limit: 16, sequence: 600) {
        #"{"type":"git","session":"bg-\#($0)"}"#
    }
    let foregroundBusy = try await proveBusy("transcript", limit: 4, sequence: 700) {
        #"{"type":"transcript","session":"fg-\#($0)","limit":200,"priority":"foreground"}"#
    }

    func inventoryData(_ ids: [String]) throws -> Data {
        try JSONSerialization.data(withJSONObject: [
            "sessions": ids.map { ["id": $0] }, "at": 800,
            "scan": ["generation": 8, "complete": true, "emptyAuthoritative": false],
        ])
    }
    let maximumIDs = (0..<512).map { "inventory-\($0)" }
    try await bridge.publishSessions(inventoryData(maximumIDs))
    let sentinel = try transport.envelopes().last { $0.ch.hasSuffix("/__clawdline_inventory_v1__") }!
        .open(masterSecret: masterSecret, publicKeyForSender: {
            $0 == "machine-device" ? signingKey.publicKeyRaw : nil
        })
    let sentinelObject = try JSONSerialization.jsonObject(with: sentinel) as? [String: Any]
    let sentinelInventory = sentinelObject?["inventory"] as? [String: Any]
    var malformedInventories = 0
    for ids in [maximumIDs + ["inventory-512"], ["same", "same"],
                [CloudAppBridge.sessionInventoryID]] {
        do { try await bridge.publishSessions(inventoryData(ids)) }
        catch CloudAppBridgeError.malformedSessions { malformedInventories += 1 }
    }
    guard sentinelInventory?["version"] as? Int == 1,
          sentinelInventory?["sessions"] as? [String] == maximumIDs.sorted(),
          malformedInventories == 3 else {
        throw CloudAppBridgeTestFailure(description:
            "Swift opens the 512-id sentinel and rejects 513, duplicate and reserved ids")
    }
    await bridge.stop()
    let transcriptRequest = RemoteServer.Request(
        verifiedCloudRead: .transcript(
            session: "session/一|?", limit: 50, priority: .foreground), sender: "viewer"
    )
    try require(transcriptRequest.method == "GET"
                    && laneReads.map(\.read.name) == ["git", "transcript"]
                    && backgroundBusy.channel.hasSuffix("/bg-16")
                    && foregroundBusy.channel.hasSuffix("/fg-4")
                    && [backgroundBusy.payload, foregroundBusy.payload].allSatisfy {
                        $0["read"] as? String != nil && $0["status"] as? Int == 429
                            && ($0["error"] as? [String: Any])?["code"] as? String
                                == "cloud_read_busy"
                    },
                "reads are GETs; foreground bypasses background; both bounded lanes return "
                    + "encrypted typed 429 on the refused Session channel and recover")
    try require(transcriptRequest.path == "/v1/sessions/session%2F%E4%B8%80%7C%3F/transcript",
                "the session id is encoded the same way the command door encodes it")
    try require(transcriptRequest.query == ["limit": "50", "priority": "foreground"],
                "the window and interactive lane travel as the query the direct path uses")
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
    let screenRequest = RemoteServer.Request(
        verifiedCloudRead: .screen(session: "session/一|?"), sender: "viewer"
    )
    let scheduleListRequest = RemoteServer.Request(
        verifiedCloudRead: .schedules(session: CloudAppBridge.machineReplySession,
                                      request: "fresh"), sender: "viewer"
    )
    let snippetListRequest = RemoteServer.Request(
        verifiedCloudRead: .snippets(session: CloudAppBridge.machineReplySession,
                                     request: "fresh-snippets"), sender: "viewer"
    )
    let closeRequest = RemoteServer.Request(
        verifiedCloud: .end(session: "%306", acceptLoss: true,
                            closeabilityVersion: "close-v1"),
        sender: "viewer", idempotencyKey: "end-key"
    )
    let resumeRequest = RemoteServer.Request(
        verifiedCloud: .resume(place: "project/one", session: "past/session|一",
                               assistant: "codex"),
        sender: "viewer", idempotencyKey: "resume-key"
    )
    let snippetCreateRequest = RemoteServer.Request(
        verifiedCloud: .snippetCreate(body: Data(
            #"{"title":"Deploy","body":"commit","scope":"global"}"#.utf8)),
        sender: "viewer", idempotencyKey: "snippet-create-key"
    )
    let snippetUpdateRequest = RemoteServer.Request(
        verifiedCloud: .snippetUpdate(
            id: "snippet/一", body: Data(#"{"title":"Ship"}"#.utf8)),
        sender: "viewer", idempotencyKey: "snippet-update-key"
    )
    let snippetDeleteRequest = RemoteServer.Request(
        verifiedCloud: .snippetDelete(id: "snippet/一"),
        sender: "viewer", idempotencyKey: "snippet-delete-key"
    )
    let snippetOrderRequest = RemoteServer.Request(
        verifiedCloud: .snippetOrder(body: Data(
            #"{"scope":"global","order":["two","one"]}"#.utf8)),
        sender: "viewer", idempotencyKey: "snippet-order-key"
    )
    try require(gitRequest.method == "GET" && gitRequest.headers["idempotency-key"] == nil
                    && scheduleListRequest.path == "/v1/orchestrator/schedules"
                    && snippetListRequest.path == "/v1/snippets"
                    && screenRequest.path == "/v1/sessions/session%2F%E4%B8%80%7C%3F/screen"
                    && screenRequest.query.isEmpty
                    && closeRequest.path == "/v1/sessions/%25306/end"
                    && ((try? JSONSerialization.jsonObject(with: closeRequest.body))
                        as? [String: Any])?["expected_closeability_version"] as? String == "close-v1"
                    && resumeRequest.path
                        == "/v1/places/project%2Fone/resume/codex/past%2Fsession%7C%E4%B8%80",
                "reads mint no key, while live screen, fresh schedules, snippets, Session close, "
                    + "and Resume map to the exact local routes")
    try require(snippetCreateRequest.method == "POST"
                    && snippetCreateRequest.path == "/v1/snippets"
                    && snippetCreateRequest.headers["idempotency-key"] == "snippet-create-key"
                    && snippetUpdateRequest.method == "PATCH"
                    && snippetUpdateRequest.path == "/v1/snippets/snippet%2F%E4%B8%80"
                    && snippetDeleteRequest.method == "DELETE"
                    && snippetDeleteRequest.path == "/v1/snippets/snippet%2F%E4%B8%80"
                    && snippetOrderRequest.method == "POST"
                    && snippetOrderRequest.path == "/v1/snippets/order"
                    && laneReads.map(\.read.name) == ["git", "transcript"],
                "Cloud mutations map locally, and foreground reads bypass background work")

    // Their local routes keep the direct path's classification. The Cloud bridge's separate
    // background worker is an upstream admission boundary, not a claim that these HTTP paths
    // belong to the local transcript or slow-read coordinator.
    for path in [agentRequest.path, shellRequest.path, skillsRequest.path] {
        try require(!RemoteServer.isTranscriptReading(path) && !RemoteServer.isSlowReading(path),
                    "\(path) is not a lane read here, because it is not one on the direct path")
    }
    try require(RemoteServer.isProjectReading(gitRequest.path),
                "Git uses the same uncached bounded project lane through Cloud and local HTTP")

    return checks
}

/// A picture crossing the transport, and one that could not.
///
/// Encrypted Cloud image reads, including their exact payload bound and typed refusals.
private func runCloudAppBridgeImageTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }

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

    transport.yield(#"{"type":"image","session":"plain","id":"\#(second)"}"#, sequence: 41)
    try await waitForCloudAppBridge("the second picture") { transport.envelopes().count == 2 }
    let secondPayload = try opened(transport.envelopes()[1])
    try require(secondPayload["read"] as? String == "image." + second,
                "the second picture answers under its own name rather than settling the first")

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

/// Project/task documents cross only as closed encrypted reads through the existing local router.
private func runCloudAppBridgeDocumentTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }

    let signingKey = CloudDeviceKeyPair()
    let masterSecret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x73, count: 32))
    let transport = CloudAppBridgeTestTransport()
    let router = CloudAppBridgeTestRouter()
    let results = CloudAppBridgeTestResults()
    let bridge = CloudAppBridge(
        transport: transport,
        identity: CloudAppIdentity(
            machineID: "Mac / 台灣", deviceID: "machine-device", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey
        ),
        sequencing: CloudAppBridgeTestSequence(),
        allowCloudCommands: { false },
        commandRouter: router,
        nowMilliseconds: { 1_788_000_000_000 },
        commandResult: { results.append($0) }
    )
    try await bridge.start()

    func opened(_ envelope: CloudEnvelope) throws -> [String: Any] {
        let clear = try envelope.open(masterSecret: masterSecret, publicKeyForSender: {
            $0 == "machine-device" ? signingKey.publicKeyRaw : nil
        })
        return (try JSONSerialization.jsonObject(with: clear)) as? [String: Any] ?? [:]
    }

    let taskID = "9e475a3b-a6dc-437a-8630-2fc78375e76d"
    await router.answerReadsWith(CloudReadResult(status: 200, body: Data(#"{"documents":[{"source":"task","path":"feature-review.md","label":"feature-review.md","bytes":3258,"modified":1788800000,"url":"/v1/sessions/plain/documents/task/9e475a3b-a6dc-437a-8630-2fc78375e76d/feature-review.md","task":{"id":"9e475a3b-a6dc-437a-8630-2fc78375e76d","title":"Cloud review"}}]}"#.utf8)))
    transport.yield(#"{"type":"documents","session":"plain"}"#, sequence: 80)
    try await waitForCloudAppBridge("the document listing") {
        transport.envelopes().count == 1
    }
    let listRead = await router.recordedReads().last?.read
    try require(listRead == .documents(session: "plain"),
                "the list reaches the existing session document route as a closed read")
    let listPayload = try opened(transport.envelopes()[0])
    let listRows = ((listPayload["body"] as? [String: Any])?["documents"] as? [[String: Any]]) ?? []
    try require(listRows.count == 1 && listRows[0]["scope"] as? String == "task",
                "the local listing becomes typed Cloud document metadata")
    try require(listRows[0]["path"] as? String == "feature-review.md"
                    && listRows[0]["task"] as? String == taskID,
                "the encrypted row retains only the identity needed to ask for its bytes")
    try require(listRows[0]["url"] == nil && listRows[0]["label"] == nil,
                "the local HTTP route and its duplicate label do not cross the relay")

    let markdown = Data("# Review\n\n<script>alert(1)</script>\n".utf8)
    await router.answerReadsWith(CloudReadResult(
        status: 200, body: markdown, contentType: "text/markdown; charset=utf-8"))
    transport.yield(#"{"type":"document","session":"plain","request":"doc-1","scope":"task","task":"9e475a3b-a6dc-437a-8630-2fc78375e76d","path":"feature-review.md"}"#, sequence: 81)
    try await waitForCloudAppBridge("the document bytes") { transport.envelopes().count == 2 }
    let documentRead = await router.recordedReads().last?.read
    try require(documentRead == .document(
        session: "plain", request: "doc-1", scope: "task", task: taskID,
        path: "feature-review.md"),
        "the byte read carries the exact session, root, task and relative path")
    let documentPayload = try opened(transport.envelopes()[1])
    try require(documentPayload["read"] as? String == "read:doc-1",
                "the answer is correlated by an opaque request id rather than a path")
    let documentBody = documentPayload["body"] as? [String: Any]
    try require(documentBody?["media_type"] as? String == "text/markdown; charset=utf-8",
                "the inert text media type crosses beside the bytes")
    try require(documentBody?["byte_count"] as? Int == markdown.count,
                "the exact byte count crosses so truncation is observable")
    try require(Data(base64Encoded: documentBody?["data"] as? String ?? "") == markdown,
                "the document bytes cross base64 and unchanged inside the encrypted envelope")

    await router.answerReadsWith(CloudReadResult(
        status: 200, body: Data("<html>".utf8), contentType: "text/html"))
    transport.yield(#"{"type":"document","session":"plain","request":"doc-2","scope":"project","task":"","path":"notes.md"}"#, sequence: 82)
    try await waitForCloudAppBridge("the executable media type refusal") {
        transport.envelopes().count == 3
    }
    let mediaRefusal = try opened(transport.envelopes()[2])
    try require(mediaRefusal["status"] as? Int == 415
                    && (mediaRefusal["error"] as? [String: Any])?["code"] as? String
                        == "document_media_type_unsupported",
                "an executable media type is refused before it can reach the renderer")

    let tooLarge = Data(repeating: 0x61, count: ProjectDocuments.maximumBytes + 1)
    await router.answerReadsWith(CloudReadResult(
        status: 200, body: tooLarge, contentType: "text/plain; charset=utf-8"))
    transport.yield(#"{"type":"document","session":"plain","request":"doc-3","scope":"project","task":"","path":"notes.txt"}"#, sequence: 83)
    try await waitForCloudAppBridge("the oversized document refusal") {
        transport.envelopes().count == 4
    }
    let sizeRefusal = try opened(transport.envelopes()[3])
    try require(sizeRefusal["status"] as? Int == 413
                    && (sizeRefusal["error"] as? [String: Any])?["code"] as? String
                        == "document_too_large",
                "a route that violates the authoritative two MiB cap cannot create an envelope")
    try require(sizeRefusal["body"] == nil,
                "the refused bytes are not carried in the refusal")

    let beforeMalformed = await router.recordedReads().count
    let malformed = [
        #"{"type":"document","session":"plain","scope":"project","task":"","path":"a.md"}"#,
        #"{"type":"document","session":"plain","request":"r","scope":"elsewhere","task":"","path":"a.md"}"#,
        #"{"type":"document","session":"plain","request":"r","scope":"task","task":"","path":"a.md"}"#,
        #"{"type":"document","session":"plain","request":"r","scope":"project","task":"9e475a3b-a6dc-437a-8630-2fc78375e76d","path":"a.md"}"#,
        #"{"type":"document","session":"plain","request":"r","scope":"project","task":"","path":"../a.md"}"#,
        #"{"type":"document","session":"plain","request":"r","scope":"project","task":"","path":"a.html"}"#,
        #"{"type":"document","session":"plain","request":"r","scope":"project","task":"","path":"a/b/c/d/e/f/g.md"}"#,
        #"{"type":"documents","session":"plain","path":"a.md"}"#,
    ]
    var sequence: UInt64 = 90
    for body in malformed {
        transport.yield(body, sequence: sequence)
        sequence += 1
    }
    try await waitForCloudAppBridge("all malformed document reads") {
        results.all().filter { $0.code == "malformed_read" }.count == malformed.count
    }
    let afterMalformed = await router.recordedReads().count
    try require(afterMalformed == beforeMalformed,
                "no malformed scope or path reaches the local router")
    // P1: the six that name a request are answered on it; the two that do not stay notices.
    try await waitForCloudAppBridge("malformed reads naming a request") { transport.envelopes().count == 10 }
    try require(transport.envelopes().suffix(6).map { try? opened($0) }.allSatisfy {
        let error = $0?["error"] as? [String: Any]
        return $0?["read"] as? String == "read:r" && error?["code"] as? String == "malformed_read"
            && error?["layer"] as? String == "mac_preflight"
    }, "a malformed read answers only on the request it names, never an uncorrelated one")

    await bridge.stop()

    let listingRequest = RemoteServer.Request(
        verifiedCloudRead: .documents(session: "session/一|?"), sender: "viewer")
    try require(listingRequest.path
                    == "/v1/sessions/session%2F%E4%B8%80%7C%3F/documents",
                "the listing maps to the existing authenticated route")
    let taskRequest = RemoteServer.Request(verifiedCloudRead: .document(
        session: "plain", request: "ignored-locally", scope: "task", task: taskID,
        path: "nested/two words?.md"), sender: "viewer")
    try require(taskRequest.path == "/v1/sessions/plain/documents/task/\(taskID)/nested/two%20words%3F.md",
                "the task root and every document segment are encoded without accepting a route")
    let projectRequest = RemoteServer.Request(verifiedCloudRead: .document(
        session: "plain", request: "ignored-locally", scope: "project", task: "",
        path: "notes.txt"), sender: "viewer")
    try require(projectRequest.path == "/v1/sessions/plain/documents/project/notes.txt",
                "project scope can choose only the existing project root")
    try require(taskRequest.query.isEmpty && taskRequest.body.isEmpty,
                "the local document read is a bare GET with no credential or filesystem path")

    return checks
}
