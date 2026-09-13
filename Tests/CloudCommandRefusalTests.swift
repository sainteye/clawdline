import Foundation
func runCloudAppBridgePublicationLifecycleTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }
    func sessions(_ ids: [String], _ generation: Int, _ complete: Bool) throws -> Data { try JSONSerialization.data(withJSONObject: ["sessions": ids.map { ["id": $0] }, "at": generation, "scan": ["generation": generation, "complete": complete, "emptyAuthoritative": complete && ids.isEmpty]]) }
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
        transportA!.state().publicationStarts > 0
    }
    RemoteServer.shared.enqueueCloudSessionsForTesting(try sessions(["obsolete"], 2, false))
    RemoteServer.shared.enqueueCloudOrchestratorForTesting(
        Data(#"{"tasks":[{"id":"obsolete"}]}"#.utf8))
    RemoteServer.shared.enqueueCloudSessionsForTesting(try sessions([], 3, true))
    RemoteServer.shared.enqueueCloudOrchestratorForTesting(
        Data(#"{"tasks":[{"id":"latest"}]}"#.utf8))
    RemoteServer.shared.enqueueCloudSessionsForTesting(try sessions(["latest"], 4, false))
    _ = await RemoteServer.shared.cloudLifecycleStateForTesting(bridge: bridgeA)
    for _ in 0..<12 {
        if transportA!.envelopes().contains(where: { $0.ch.hasSuffix("/latest") }) { break }
        let startsBeforeStep = transportA!.state().publicationStarts
        transportA!.releasePublications()
        try await waitForCloudAppBridge("one bounded publication step advances") {
            transportA!.envelopes().contains { $0.ch.hasSuffix("/latest") }
                || transportA!.state().publicationStarts > startsBeforeStep
        }
    }
    try await waitForCloudAppBridge("the bounded queue publishes its latest Session") {
        transportA!.envelopes().contains { $0.ch.hasSuffix("/latest") }
    }
    let bounded = transportA!.envelopes()
    try require(!bounded.contains { $0.ch.hasSuffix("/obsolete") },
                "interleaved publications cannot preserve an obsolete Session FIFO entry")
    let orchFrames = bounded.filter { $0.ch == "orch/publication" }
    let orchBytes = try orchFrames.last?.open(masterSecret: masterSecret, publicKeyForSender: {
        $0 == "machine-device" ? signingKey.publicKeyRaw : nil })
    let orchObject = orchBytes.flatMap { (try? JSONSerialization.jsonObject(with: $0)) as? [String: Any] }
    let orchTasks = orchObject?["tasks"] as? [[String: Any]]
    try require(orchFrames.count == 1 && orchTasks?.first?["id"] as? String == "latest",
                "interleaved publications retain only the latest pending orchestrator snapshot")
    let seedFrames = bounded.filter { $0.ch.hasSuffix("/publication-lifecycle") }
    let deletion = try seedFrames.last?.open(masterSecret: masterSecret,
        publicKeyForSender: { $0 == "machine-device" ? signingKey.publicKeyRaw : nil })
    let deletionObject = deletion.flatMap { (try? JSONSerialization.jsonObject(with: $0)) as? [String: Any] }
    try require(seedFrames.count == 2 && deletionObject?["deleted"] as? Bool == true,
                "the authoritative deletion barrier survives a newer incomplete snapshot")
    let inventory = bounded.indices.filter { bounded[$0].ch.hasSuffix("/__clawdline_inventory_v1__") }
    let latest = bounded.firstIndex { $0.ch.hasSuffix("/latest") }
    try require(inventory.count == 2 && latest != nil && inventory.last! < latest!,
                "the authoritative inventory is delivered before the latest incomplete row")
    let startsBeforeCancellation = transportA!.state().publicationStarts
    RemoteServer.shared.enqueueCloudSessionsForTesting(try sessions(["cancelled"], 5, false))
    try await waitForCloudAppBridge("the replacement target enters bridge publication") {
        transportA!.state().publicationStarts > startsBeforeCancellation
    }
    let transportB = CloudAppBridgeTestTransport()
    let bridgeB = makeBridge(transportB)
    await attachCloudBridgeForTest(bridgeB)
    await RemoteServer.shared.awaitCloudBridgeLifecycle()
    try require(transportA!.state().publicationCancelled,
                "replacement cancels an in-flight bridge-owned publication")
    try await waitForCloudAppBridge("B fresh publications bypass A's invalidated tail") {
        let channels = transportB.envelopes().map(\.ch)
        return channels.filter { $0.hasPrefix("s/publication/") }.count == 2
            && channels.filter { $0 == "orch/publication" }.count == 1
    }
    try require(!transportA!.envelopes().contains { $0.ch.hasSuffix("/cancelled") },
                "cancelled A publication emits no stale envelope")
    bridgeA = nil
    transportA = nil
    try require(bridgeAReference == nil && transportAReference == nil,
                "publication lifecycle completion releases A and its transport")
    await attachCloudBridgeForTest(nil)
    await RemoteServer.shared.awaitCloudBridgeLifecycle()
    return checks
}
func runCloudAppBridgeIngressRefusalTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }

    let signingKey = CloudDeviceKeyPair()
    let masterSecret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x73, count: 32))
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
        allowCloudCommands: { gate.get() }, commandRouter: router,
        commandResult: { results.append($0) }
    )
    try await bridge.start()

    func refused(
        _ plaintext: String, sequence: UInt64,
        channel: String = "ctl/Mac%20%2F%20%E5%8F%B0%E7%81%A3"
    ) -> CloudInboundCommand {
        CloudInboundCommand(
            channel: channel, sequence: sequence, timestamp: 1,
            commandClass: .ctl, sender: "viewer", plaintext: Data(plaintext.utf8)
        )
    }

    func opened(_ envelope: CloudEnvelope) throws -> [String: Any] {
        let bytes = try envelope.open(
            masterSecret: masterSecret,
            publicKeyForSender: { $0 == "machine-device" ? signingKey.publicKeyRaw : nil }
        )
        return try JSONSerialization.jsonObject(with: bytes) as? [String: Any] ?? [:]
    }

    await transport.refuse(refused(
        #"{"type":"info","session":"plain","parts":"full"}"#, sequence: 1
    ), reason: .countCap)
    try await waitForCloudAppBridge("ingress-refused read answer") {
        transport.envelopes().count == 1
    }
    let readAnswer = try opened(transport.envelopes()[0])
    try require(readAnswer["read"] as? String == "info.full"
                    && readAnswer["status"] as? Int == 429
                    && (readAnswer["error"] as? [String: Any])?["code"] as? String
                        == "cloud_ingress_busy",
                "a safely identifiable read receives the existing encrypted read-refusal shape")

    await transport.refuse(refused(
        #"{"type":"send","session":"plain","request":"write-off","text":"x","images":[]}"#,
        sequence: 2
    ), reason: .countCap)
    try await waitForCloudAppBridge("write-gated ingress refusal") {
        transport.envelopes().count == 2
    }
    let gatedAnswer = try opened(transport.envelopes()[1])
    try require(gatedAnswer["status"] as? Int == 403
                    && (gatedAnswer["error"] as? [String: Any])?["code"] as? String
                        == "cloud_commands_disabled",
                "ingress refusal applies the normal remote-write gate before reporting capacity")

    gate.set(true)
    await transport.refuse(refused(
        #"{"type":"send","session":"plain","request":"wrong-mac","text":"x","images":[]}"#,
        sequence: 3, channel: "ctl/another-machine"
    ), reason: .countCap)
    try await waitForCloudAppBridge("wrong-machine ingress refusal") {
        results.all().contains { $0.code == "wrong_machine" }
    }
    // The viewer reads the channel it addressed, and this Mac cannot publish there, so the
    // refusal is a notice rather than an answer nobody would read.
    try require(transport.envelopes().count == 2
                    && results.all().filter { $0.code == "cloud_ingress_busy" }.count == 1,
                "ingress refusal applies the exact machine-channel gate before reporting capacity")

    await transport.refuse(refused(
        #"{"type":"send","session":"plain","request":"new-request","text":"x","images":[]}"#,
        sequence: 4
    ), reason: .countCap)
    try await waitForCloudAppBridge("terminal capacity refusal") {
        transport.envelopes().count == 3
    }
    let capacityAnswer = try opened(transport.envelopes()[2])
    let capacityError = capacityAnswer["error"] as? [String: Any]
    try require(capacityAnswer["status"] as? Int == 429
                    && capacityError?["code"] as? String == "cloud_ingress_busy"
                    && !(capacityError?["message"] as? String ?? "").contains("same request"),
                "capacity refusal is terminal and never instructs an impossible same-envelope retry")
    let refusalRouteCalls = await router.recorded()
    let refusalReadCalls = await router.recordedReads()
    try require(refusalRouteCalls.isEmpty && refusalReadCalls.isEmpty,
                "no pre-admission refusal reaches command effects or read routing")
    try await waitForCloudAppBridge("refusal lane completion accounting") {
        await bridge.refusalPublicationMetrics().completedTotal == 4
    }
    let normalMetrics = await bridge.refusalPublicationMetrics()
    try require(normalMetrics.admittedTotal == 4 && normalMetrics.completedTotal == 4
                    && normalMetrics.timedOutTotal == 0 && normalMetrics.droppedFullTotal == 0,
                "the refusal lane accounts every completed encrypted answer")
    await bridge.stop()

    let boundedTransport = CloudAppBridgeTestTransport(suspendPublication: true)
    let boundedResults = CloudAppBridgeTestResults()
    let boundedLane = CloudRefusalPublicationQueue(maximumCount: 2, deadlineMilliseconds: 200)
    let boundedBridge = CloudAppBridge(
        transport: boundedTransport,
        identity: CloudAppIdentity(
            machineID: "Mac / 台灣", deviceID: "machine-device", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey
        ),
        sequencing: CloudAppBridgeTestSequence(),
        commandResult: { boundedResults.append($0) },
        refusalPublications: boundedLane
    )
    try await boundedBridge.start()
    for sequence in 10...12 {
        boundedTransport.yield(
            #"{"type":"send","session":"plain","request":"bounded-\#(sequence)","text":"x","images":[]}"#,
            sequence: UInt64(sequence)
        )
    }
    try await waitForCloudAppBridge("nonblocking preflight refusal admission") {
        boundedResults.all().filter { $0.code == "cloud_commands_disabled" }.count == 3
            && boundedTransport.state().publicationStarts == 1
    }
    let saturatedLane = await boundedBridge.refusalPublicationMetrics()
    try require(saturatedLane.maximumCount == 2 && saturatedLane.peakCount == 2
                    && saturatedLane.admittedTotal == 2 && saturatedLane.droppedFullTotal == 1,
                "preflight 403 publication is bounded while the command consumer keeps draining")
    try await waitForCloudAppBridge("refusal publication deadline accounting") {
        let metrics = await boundedBridge.refusalPublicationMetrics()
        return metrics.currentCount == 0 && metrics.timedOutTotal == 1
            && metrics.completedTotal == 1
    }
    let deadlineMetrics = await boundedBridge.refusalPublicationMetrics()
    try require(deadlineMetrics.deadlineMilliseconds == 200
                    && deadlineMetrics.timedOutTotal == 1
                    && deadlineMetrics.completedTotal == 1,
                "a stalled refusal publication reaches its explicit deadline and the lane recovers")
    await boundedBridge.stop()

    return checks
}


func runCloudCommandRefusalTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }
    let key = CloudDeviceKeyPair()
    let secret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x52, count: 32))
    let transport = CloudAppBridgeTestTransport()
    let router = CloudAppBridgeTestRouter()
    let gate = CloudAppBridgeTestGate()
    let results = CloudAppBridgeTestResults()
    let bridge = CloudAppBridge(
        transport: transport,
        identity: CloudAppIdentity(machineID: "Mac / 台灣", deviceID: "machine-device", keyID: "ms-1",
                                   masterSecret: secret, signingKey: key),
        sequencing: CloudAppBridgeTestSequence(),
        allowCloudCommands: { gate.get() }, commandRouter: router,
        commandResult: { results.append($0) })
    try await bridge.start()

    transport.yield(#"{"type":"shell-kill","session":"s-1","request":"disabled-1","shell":"p-1"}"#,
                    sequence: 1)
    try await waitForCloudAppBridge("disabled command answer") { transport.envelopes().count == 1 }
    gate.set(true)
    transport.yield(#"{"type":"shell-kill","session":"s-1","request":"malformed-1"}"#,
                    sequence: 2)
    try await waitForCloudAppBridge("malformed command answer") { transport.envelopes().count == 2 }

    let payloads = try transport.envelopes().map { envelope -> [String: Any] in
        let data = try envelope.open(masterSecret: secret, publicKeyForSender: {
            $0 == "machine-device" ? key.publicKeyRaw : nil
        })
        return try JSONSerialization.jsonObject(with: data) as? [String: Any] ?? [:]
    }
    try require(payloads.map { $0["read"] as? String } ==
                    ["action:disabled-1", "action:malformed-1"],
                "403 and safely identifiable 400 refusals publish on the original action channels")
    try require(payloads.map { $0["status"] as? Int } == [403, 400],
                "the published answers preserve refusal status")
    try require(payloads.compactMap { ($0["error"] as? [String: Any])?["code"] as? String } ==
                    ["cloud_commands_disabled", "malformed_command"],
                "the published answers preserve typed refusal codes")
    let routed = await router.recorded()
    try require(routed.isEmpty, "preflight refusals never reach the command router")

    let readsBefore = await router.recordedReads().count
    await router.blockRead(named: "git")
    transport.yield(#"{"type":"git","session":"blocked"}"#, sequence: 3)
    try await waitForCloudAppBridge("background read enters") {
        await router.recordedReads().count == readsBefore + 1
    }
    transport.yield(#"{"type":"project-worktree-lifecycle-refresh","session":"__clawdline_machine__","request":"refresh-1","project":"project-0123456789abcdef01234567"}"#,
                    sequence: 4)
    try await waitForCloudAppBridge("lifecycle refresh bypasses blocked background reads") {
        await router.recordedReads().count == readsBefore + 2
    }
    let independent = await router.recordedReads().last?.read.name
    try require(independent == "read:refresh-1",
                "a lifecycle refresh has an independent bounded lane")
    await router.releaseBlockedRead()
    await bridge.stop()
    return checks
}
