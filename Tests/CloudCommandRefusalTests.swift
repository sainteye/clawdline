import Foundation

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
