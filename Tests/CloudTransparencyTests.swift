import Foundation

// Failure-injection tests for Cloud error transparency, design `docs/cloud-error-transparency.md`
// §7 and §11. Every case reports each failing assertion by name and the suite fails once at the
// end, so one red run shows every test's own failing line instead of only the first.

struct CloudTransparencyTestFailure: Error, CustomStringConvertible {
    let description: String
}

final class CloudTransparencyChecks: @unchecked Sendable {
    private let lock = NSLock()
    private var total = 0
    private var failed: [String] = []

    func check(_ name: String, _ condition: Bool, _ detail: @autoclosure () -> String = "") {
        let text = condition ? "" : detail()
        lock.lock()
        total += 1
        if !condition { failed.append(text.isEmpty ? name : "\(name) — \(text)") }
        lock.unlock()
    }

    func finish(_ suite: String) throws -> Int {
        lock.lock()
        let failures = failed
        let count = total
        lock.unlock()
        for failure in failures { print("    ✗ \(suite): \(failure)") }
        guard failures.isEmpty else {
            throw CloudTransparencyTestFailure(
                description: "\(failures.count) of \(count) \(suite) checks failed:\n"
                    + failures.joined(separator: "\n"))
        }
        return count
    }
}

func cloudTransparencyEventually(
    timeout: TimeInterval = 2, _ condition: () async -> Bool
) async -> Bool {
    let deadline = Date().addingTimeInterval(timeout)
    repeat {
        if await condition() { return true }
        try? await Task.sleep(nanoseconds: 10_000_000)
    } while Date() < deadline
    return await condition()
}

final class CloudTransparencyRecorder<Value>: @unchecked Sendable {
    private let lock = NSLock()
    private var values: [Value] = []

    func append(_ value: Value) {
        lock.lock()
        values.append(value)
        lock.unlock()
    }

    func all() -> [Value] {
        lock.lock()
        defer { lock.unlock() }
        return values
    }
}

private actor CloudTransparencyTokens: CloudDeviceTokenProviding {
    func fetchDeviceToken() async throws -> CloudDeviceToken {
        CloudDeviceToken(value: "transparency-token", expiresAt: Date().addingTimeInterval(3_600))
    }
}

/// A key provider whose roster read fails, the way a refused Keychain read does.
private struct CloudTransparencyUnreadableRoster: CloudTransportKeyProviding {
    let deviceKey: CloudDeviceKeyPair
    let secret: CloudMasterSecret

    func deviceKeyPair() async throws -> CloudDeviceKeyPair { deviceKey }
    func masterSecret(for keyID: String) async throws -> CloudMasterSecret { secret }
    func pairedDevicePublicKeys() async -> [String: Data] { [:] }
    func pairedDeviceRoster() async -> CloudPairedDeviceRosterReading { .unreadable }
}

private struct CloudTransparencyLink {
    let transport: CloudTransport
    let relay: CloudLoopbackRelay
    let lines: CloudTransparencyRecorder<String>
    let drops: CloudTransparencyRecorder<CloudInboundDrop>
    let accepted: CloudTransparencyRecorder<CloudInboundCommand>
    let consumer: Task<Void, Never>?
}

private func cloudTransparencyLink(
    machineKey: CloudDeviceKeyPair, keys: any CloudTransportKeyProviding,
    replayWindow: CloudInboundReplayWindow = CloudInboundReplayWindow(),
    limits: CloudInboundCommandQueueLimits = CloudInboundCommandQueueLimits(),
    consume: Bool = true
) async throws -> CloudTransparencyLink {
    let relay = CloudLoopbackRelay(
        account: "transparency-account", deviceID: "machine-t",
        devicePublicKey: machineKey.publicKeyRaw, allowedTokens: ["transparency-token"])
    let lines = CloudTransparencyRecorder<String>()
    let drops = CloudTransparencyRecorder<CloudInboundDrop>()
    let accepted = CloudTransparencyRecorder<CloudInboundCommand>()
    let transport = CloudTransport(
        relayBaseURL: URL(string: "ws://loopback.invalid/v1/connect")!,
        tokenProvider: CloudTransparencyTokens(), keyProvider: keys,
        connector: CloudLoopbackSocketConnector(relay: relay),
        inboundQueueLimits: limits, replayWindow: replayWindow,
        logger: { lines.append($0) })
    await transport.setInboundDropHandler { drops.append($0) }
    try await transport.connect(role: .machine)
    var consumer: Task<Void, Never>?
    if consume {
        let commands = transport.commands
        consumer = Task {
            for await command in commands { accepted.append(command) }
        }
    }
    return CloudTransparencyLink(
        transport: transport, relay: relay, lines: lines, drops: drops, accepted: accepted,
        consumer: consumer)
}

private func cloudTransparencySeal(
    _ text: String, sequence: UInt64, sender: String, key: CloudDeviceKeyPair, keyID: String,
    master: CloudMasterSecret, channel: String = "ctl/machine-t",
    envelopeClass: CloudEnvelopeClass = .ctl
) throws -> CloudEnvelope {
    try CloudEnvelope.seal(
        Data(text.utf8), ch: channel, seq: sequence,
        ts: UInt64(Date().timeIntervalSince1970 * 1_000), envelopeClass: envelopeClass,
        keyID: keyID, sender: sender, masterSecret: master, signingKey: key)
}

// MARK: - Transport: drops, log line, replay window

func runCloudTransportTransparencyTests() async throws -> Int {
    let checks = CloudTransparencyChecks()
    let machineKey = CloudDeviceKeyPair()
    let viewerKey = CloudDeviceKeyPair()
    let strangerKey = CloudDeviceKeyPair()
    let master = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x71, count: 32))
    let otherMaster = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x72, count: 32))

    // T-M1: the relay delivers an envelope sealed under ms-1 while this Mac holds ms-2.
    do {
        let link = try await cloudTransparencyLink(
            machineKey: machineKey,
            keys: CloudStaticTransportKeys(
                deviceKey: machineKey, masterSecrets: ["ms-2": master],
                pairedDevices: ["viewer-m1": viewerKey.publicKeyRaw]))
        try await link.relay.send(envelope: cloudTransparencySeal(
            #"{"type":"answer","session":"s","answer":"1"}"#, sequence: 7, sender: "viewer-m1",
            key: viewerKey, keyID: "ms-1", master: master))
        let dropped = await cloudTransparencyEventually { link.drops.all().count == 1 }
        let drop = link.drops.all().first
        checks.check("T-M1 the drop owner hears the key mismatch", dropped,
                      "drops=\(link.drops.all())")
        checks.check("T-M1 the drop names key_id_mismatch with both key ids, sender and seq",
                      drop == CloudInboundDrop(
                        code: .keyIDMismatch, sender: "viewer-m1", sequence: 7, keyID: "ms-1",
                        expectedKeyID: "ms-2"), "\(String(describing: drop))")
        let expected = "refusal layer=mac_transport code=key_id_mismatch sender=viewer-m1 seq=7 "
            + "request=- type=- session=- status=- reply=notice key_id=ms-1 expected_key_id=ms-2"
        checks.check("T-M1 the log carries exactly one §2.3 line for it",
                      link.lines.all().filter { $0 == expected }.count == 1,
                      "lines=\(link.lines.all())")
        checks.check("T-M1 the old reason=invalid line is gone",
                      !link.lines.all().contains { $0.contains("reason=invalid") })
        // The assertion above is exact-line equality. Prove it goes red on a broken format.
        let broken = expected.replacingOccurrences(of: " seq=7 request=-", with: " request=- seq=7")
        checks.check("T-M1 guard: a field-order break fails the line assertion",
                     [broken].filter { $0 == expected }.isEmpty && broken != expected)
        checks.check("T-M1 CloudRefusalLog renders that exact line",
                     CloudRefusalLog.line(
                        layer: .macTransport, code: "key_id_mismatch", sender: "viewer-m1",
                        sequence: 7, request: nil, type: nil, session: nil, status: nil,
                        reply: "notice",
                        extras: [("key_id", "ms-1"), ("expected_key_id", "ms-2")]) == expected)
        let status = CloudStatus()
        status.recordDrop(drop ?? CloudInboundDrop(
            code: .keyIDMismatch, sender: "viewer-m1", sequence: 7, keyID: "ms-1",
            expectedKeyID: "ms-2"))
        let inbound = status.snapshot()["inbound"] as? [String: Any]
        let recent = (inbound?["recent_drops"] as? [[String: Any]])?.first
        checks.check("T-M1 the status snapshot counts it by reason",
                     (inbound?["dropped"] as? [String: Any])?["key_id_mismatch"] as? Int == 1,
                     "\(String(describing: inbound))")
        checks.check("T-M1 recent_drops holds the same ref and key ids",
                     recent?["sender"] as? String == "viewer-m1" && recent?["seq"] as? Int == 7
                        && recent?["key_id"] as? String == "ms-1"
                        && recent?["expected_key_id"] as? String == "ms-2",
                     "\(String(describing: recent))")
        await link.transport.shutdown()
        link.consumer?.cancel()
        await link.relay.stop()
    }

    // T-M2: an unpaired sender, a bad signature and an unreadable roster are three codes.
    do {
        let link = try await cloudTransparencyLink(
            machineKey: machineKey,
            keys: CloudStaticTransportKeys(
                deviceKey: machineKey, masterSecrets: ["ms-2": master],
                pairedDevices: ["viewer-m2": viewerKey.publicKeyRaw]))
        try await link.relay.send(envelope: cloudTransparencySeal(
            "stranger", sequence: 3, sender: "stranger", key: strangerKey, keyID: "ms-2",
            master: master))
        let genuine = try cloudTransparencySeal(
            "forged", sequence: 4, sender: "viewer-m2", key: viewerKey, keyID: "ms-2",
            master: master)
        let forged = try CloudEnvelope(
            ch: genuine.ch, seq: genuine.seq, ts: genuine.ts, envelopeClass: genuine.envelopeClass,
            keyID: genuine.keyID, nonce: genuine.nonce, ct: genuine.ct, sender: genuine.sender,
            sig: try strangerKey.signature(for: genuine.signingBytes).base64EncodedString())
        try await link.relay.send(envelope: forged)
        try await link.relay.send(envelope: cloudTransparencySeal(
            "stream", sequence: 5, sender: "viewer-m2", key: viewerKey, keyID: "ms-2",
            master: master, channel: "s/machine-t/session", envelopeClass: .stream))
        try await link.relay.send(envelope: cloudTransparencySeal(
            "other secret", sequence: 6, sender: "viewer-m2", key: viewerKey, keyID: "ms-2",
            master: otherMaster))
        // The relay delivers through the receive loop; the malformed frame below does not, so wait
        // for the four delivered drops before it to keep the order the assertion names.
        _ = await cloudTransparencyEventually { link.drops.all().count >= 4 }
        try await link.transport.handleAuthenticatedFrameForTesting(
            #"{"type":"envelope","envelope":{"sender":"viewer-x","seq":12,"key_id":"ms-2","v":9}}"#)
        _ = await cloudTransparencyEventually { link.drops.all().count >= 5 }
        let codes = link.drops.all().map(\.code.rawValue)
        checks.check("T-M2 unknown sender, bad signature, wrong channel, decrypt and shape are "
                        + "five distinct codes",
                     codes == ["unknown_sender", "bad_signature", "wrong_channel",
                               "decrypt_failed", "envelope_malformed"], "\(codes)")
        let lines = link.lines.all()
        for expected in [
            "refusal layer=mac_transport code=unknown_sender sender=stranger seq=3 request=- "
                + "type=- session=- status=- reply=notice roster_readable=true",
            "refusal layer=mac_transport code=bad_signature sender=viewer-m2 seq=4 request=- "
                + "type=- session=- status=- reply=notice",
            "refusal layer=mac_transport code=wrong_channel sender=viewer-m2 seq=5 request=- "
                + "type=- session=- status=- reply=notice",
            "refusal layer=mac_transport code=decrypt_failed sender=viewer-m2 seq=6 request=- "
                + "type=- session=- status=- reply=notice",
            "refusal layer=mac_transport code=envelope_malformed sender=viewer-x seq=12 "
                + "request=- type=- session=- status=- reply=notice",
        ] {
            checks.check("T-M2 log line: \(expected.split(separator: " ")[2])",
                         lines.contains(expected), "lines=\(lines)")
        }
        checks.check("T-M2 no drop line carries ciphertext",
                     !lines.contains { $0.contains(genuine.ct) || $0.contains(forged.sig) })
        await link.transport.shutdown()
        link.consumer?.cancel()
        await link.relay.stop()

        let unreadable = try await cloudTransparencyLink(
            machineKey: machineKey,
            keys: CloudTransparencyUnreadableRoster(deviceKey: machineKey, secret: master))
        try await unreadable.relay.send(envelope: cloudTransparencySeal(
            "roster", sequence: 8, sender: "viewer-m2", key: viewerKey, keyID: "ms-2",
            master: master))
        _ = await cloudTransparencyEventually { unreadable.drops.all().count == 1 }
        let rosterDrop = unreadable.drops.all().first
        checks.check("T-M2 an unreadable roster is its own code and says so",
                     rosterDrop?.code.rawValue == "roster_unreadable"
                        && rosterDrop?.rosterReadable == false,
                     "\(String(describing: rosterDrop))")
        checks.check("T-M2 roster_readable=false is on the log line",
                     unreadable.lines.all().contains(
                        "refusal layer=mac_transport code=roster_unreadable sender=viewer-m2 seq=8 "
                            + "request=- type=- session=- status=- reply=notice roster_readable=false"),
                     "lines=\(unreadable.lines.all())")
        let status = CloudStatus()
        if let rosterDrop { status.recordDrop(rosterDrop) }
        let identity = status.snapshot()["identity"] as? [String: Any]
        checks.check("T-M2 the status snapshot reports roster_readable:false",
                     identity?["roster_readable"] as? Bool == false, "\(String(describing: identity))")
        await unreadable.transport.shutdown()
        unreadable.consumer?.cancel()
        await unreadable.relay.stop()
    }

    // T-M3 and §11.8 at the transport: a repeated (sender, seq) is a replay naming the highest.
    do {
        let link = try await cloudTransparencyLink(
            machineKey: machineKey,
            keys: CloudStaticTransportKeys(
                deviceKey: machineKey, masterSecrets: ["ms-2": master],
                pairedDevices: ["viewer-m3": viewerKey.publicKeyRaw]))
        let ten = try cloudTransparencySeal(
            "ten", sequence: 10, sender: "viewer-m3", key: viewerKey, keyID: "ms-2", master: master)
        let five = try cloudTransparencySeal(
            "five", sequence: 5, sender: "viewer-m3", key: viewerKey, keyID: "ms-2", master: master)
        try await link.relay.send(envelope: ten)
        _ = await cloudTransparencyEventually { link.accepted.all().count == 1 }
        try await link.relay.send(envelope: five)
        _ = await cloudTransparencyEventually { link.accepted.all().count == 2 }
        try await link.relay.send(envelope: ten)
        _ = await cloudTransparencyEventually { link.drops.all().count == 1 }
        checks.check("§11.8 an older unseen sequence inside the window is accepted",
                     link.accepted.all().map(\.sequence) == [10, 5],
                     "\(link.accepted.all().map(\.sequence))")
        checks.check("T-M3 the repeated envelope is a replay naming highest_seq",
                     link.drops.all().first == CloudInboundDrop(
                        code: .replay, sender: "viewer-m3", sequence: 10, highestSequence: 10),
                     "\(link.drops.all())")
        checks.check("T-M3 its log line carries highest_seq",
                     link.lines.all().contains(
                        "refusal layer=mac_transport code=replay sender=viewer-m3 seq=10 request=- "
                            + "type=- session=- status=- reply=notice highest_seq=10"),
                     "lines=\(link.lines.all())")
        await link.transport.shutdown()
        link.consumer?.cancel()
        await link.relay.stop()
    }

    // §11.8 audit: a transport rebuilt in the same process shares the window, so an envelope the
    // first transport accepted is a replay at the second.
    do {
        let window = CloudInboundReplayWindow()
        let keys = CloudStaticTransportKeys(
            deviceKey: machineKey, masterSecrets: ["ms-2": master],
            pairedDevices: ["viewer-w": viewerKey.publicKeyRaw])
        let envelope = try cloudTransparencySeal(
            "once", sequence: 21, sender: "viewer-w", key: viewerKey, keyID: "ms-2", master: master)
        let first = try await cloudTransparencyLink(
            machineKey: machineKey, keys: keys, replayWindow: window)
        try await first.relay.send(envelope: envelope)
        _ = await cloudTransparencyEventually { first.accepted.all().count == 1 }
        await first.transport.shutdown()
        first.consumer?.cancel()
        await first.relay.stop()
        let second = try await cloudTransparencyLink(
            machineKey: machineKey, keys: keys, replayWindow: window)
        try await second.relay.send(envelope: envelope)
        _ = await cloudTransparencyEventually {
            second.drops.all().count == 1 || second.accepted.all().count == 1
        }
        checks.check("§11.8 a rebuilt transport on the process window refuses the accepted seq",
                     first.accepted.all().count == 1 && second.accepted.all().isEmpty
                        && second.drops.all().first?.code == .replay,
                     "accepted=\(second.accepted.all().count) drops=\(second.drops.all())")
        await second.transport.shutdown()
        second.consumer?.cancel()
        await second.relay.stop()
    }

    // §11.8: a claim is final. A sequence refused for capacity and sent again is a replay.
    do {
        let link = try await cloudTransparencyLink(
            machineKey: machineKey,
            keys: CloudStaticTransportKeys(
                deviceKey: machineKey, masterSecrets: ["ms-2": master],
                pairedDevices: ["viewer-c": viewerKey.publicKeyRaw]),
            limits: CloudInboundCommandQueueLimits(maximumCount: 1, maximumChargedBytes: 1 << 20),
            consume: false)
        let refusals = CloudTransparencyRecorder<CloudInboundAdmissionRefusal>()
        await link.transport.setInboundRefusalHandler { refusals.append($0) }
        let held = try cloudTransparencySeal(
            "held", sequence: 31, sender: "viewer-c", key: viewerKey, keyID: "ms-2", master: master)
        let refused = try cloudTransparencySeal(
            "refused", sequence: 32, sender: "viewer-c", key: viewerKey, keyID: "ms-2",
            master: master)
        try await link.relay.send(envelope: held)
        try await link.relay.send(envelope: refused)
        _ = await cloudTransparencyEventually { refusals.all().count == 1 }
        try await link.relay.send(envelope: refused)
        _ = await cloudTransparencyEventually {
            link.drops.all().count == 1 || refusals.all().count == 2
        }
        checks.check("§11.8 a capacity-refused sequence sent again is a replay, not a retry",
                     refusals.all().count == 1 && link.drops.all().first?.code == .replay,
                     "refusals=\(refusals.all().count) drops=\(link.drops.all())")
        checks.check("M4 plaintext over the ceiling is the terminal command_too_large",
                     CloudInboundAdmissionRefusalReason.plaintextCap.refusalCode
                        == "command_too_large"
                        && CloudInboundAdmissionRefusalReason.countCap.refusalCode
                            == "cloud_ingress_busy")
        await link.transport.shutdown()
        await link.relay.stop()
    }

    // §11.8 window arithmetic on the tracker itself.
    do {
        var tracker = CloudSequenceTracker(maximumSenders: 2)
        checks.check("§11.8 first sequence accepted",
                     tracker.claim(sender: "a", sequence: 10) == .accepted)
        checks.check("§11.8 out-of-order unseen sequence inside the window accepted",
                     tracker.claim(sender: "a", sequence: 5) == .accepted)
        checks.check("§11.8 duplicate inside the window refused naming the highest",
                     tracker.claim(sender: "a", sequence: 5) == .replay(highestSequence: 10))
        checks.check("§11.8 the highest itself refused when repeated",
                     tracker.claim(sender: "a", sequence: 10) == .replay(highestSequence: 10))
        checks.check("§11.8 a jump past the window is accepted",
                     tracker.claim(sender: "a", sequence: 5_000) == .accepted)
        checks.check("§11.8 a sequence older than the window is refused",
                     tracker.claim(sender: "a", sequence: 3_975) == .replay(highestSequence: 5_000))
        checks.check("§11.8 the oldest position still inside the window is accepted",
                     tracker.claim(sender: "a", sequence: 3_976) == .accepted)
        checks.check("§11.8 an unseen sequence just below a gap larger than the window is accepted",
                     tracker.claim(sender: "a", sequence: 4_999) == .accepted)
        checks.check("§11.8 positions before a large gap are forgotten, not accepted twice",
                     tracker.claim(sender: "a", sequence: 10) == .replay(highestSequence: 5_000))
        checks.check("§11.8 advancing by a few keeps the old highest recorded",
                     tracker.claim(sender: "a", sequence: 5_003) == .accepted
                        && tracker.claim(sender: "a", sequence: 5_000)
                            == .replay(highestSequence: 5_003)
                        && tracker.claim(sender: "a", sequence: 5_001) == .accepted
                        && tracker.claim(sender: "a", sequence: 4_999)
                            == .replay(highestSequence: 5_003))
        checks.check("§11.8 per-sender isolation: another sender's window is its own",
                     tracker.claim(sender: "b", sequence: 60) == .accepted
                        && tracker.claim(sender: "b", sequence: 59) == .accepted
                        && tracker.claim(sender: "b", sequence: 60) == .replay(highestSequence: 60))
        checks.check("§11.8 a sender beyond the tracked bound is refused, not tracked by eviction",
                     tracker.claim(sender: "c", sequence: 1) == .senderCapacity
                        && tracker.claim(sender: "a", sequence: 5_004) == .accepted)
    }

    return try checks.finish("CloudTransportTransparency")
}

// MARK: - Bridge fixtures

private let transparencyMachine = "Mac / 台灣"
private let transparencyCommandChannel = "ctl/Mac%20%2F%20%E5%8F%B0%E7%81%A3"
private let transparencyOrchChannel = "orch/Mac%20%2F%20%E5%8F%B0%E7%81%A3"

actor CloudTransparencyRouter: CloudCommandRouting {
    private var answer = CloudCommandResult(
        status: 200, code: nil, body: Data(#"{"ok":true}"#.utf8))
    private var commands: [String] = []

    func answerWith(_ result: CloudCommandResult) { answer = result }

    func route(_ command: CloudHeadlessCommand, sender: String,
               idempotencyKey: String) async -> CloudCommandResult {
        commands.append(String(describing: command))
        return answer
    }

    func read(_ read: CloudHeadlessRead, sender: String) async -> CloudReadResult {
        CloudReadResult(status: 200, body: Data(#"{"ok":true}"#.utf8))
    }

    func recorded() -> [String] { commands }
}

final class CloudTransparencyAuthority: @unchecked Sendable {
    private let lock = NSLock()
    private var value = CloudCommandEffectAuthorization(
        epochState: .ready, rosterAllowsSender: true, writeGateAllows: true)

    func set(_ next: CloudCommandEffectAuthorization) {
        lock.lock()
        value = next
        lock.unlock()
    }

    func current() -> CloudCommandEffectAuthorization {
        lock.lock()
        defer { lock.unlock() }
        return value
    }
}

/// A transport whose publications either fail or take `delayMilliseconds`, recorded without an
/// `AsyncStream`, so several publications may wait at once.
final class CloudTransparencyFailingTransport: CloudTransporting, @unchecked Sendable {
    nonisolated let commands: CloudInboundCommandStream
    nonisolated let readyGenerations: AsyncStream<UInt64>
    private let queue: CloudInboundCommandQueue
    private let readyContinuation: AsyncStream<UInt64>.Continuation
    private let delayMilliseconds: UInt64?
    private let lock = NSLock()
    private var sent: [CloudEnvelope] = []

    init(delayMilliseconds: UInt64? = nil) {
        self.delayMilliseconds = delayMilliseconds
        let queue = CloudInboundCommandQueue()
        self.queue = queue
        commands = queue.stream
        var continuation: AsyncStream<UInt64>.Continuation!
        readyGenerations = AsyncStream { continuation = $0 }
        readyContinuation = continuation
    }

    func connect(role: CloudTransportRole) async throws {}
    func sendExactPublishFrame(_ bytes: Data) async throws {
        guard let delayMilliseconds else { throw CloudTransportError.notConnected }
        try await Task.sleep(nanoseconds: delayMilliseconds * 1_000_000)
        let frame = try JSONDecoder().decode(CloudPublishFrame.self, from: bytes)
        lock.lock()
        sent.append(frame.envelope)
        lock.unlock()
    }

    func envelopes() -> [CloudEnvelope] {
        lock.lock()
        defer { lock.unlock() }
        return sent
    }
    func setInboundRefusalHandler(_ handler: CloudTransport.InboundRefusalHandler?) async {}
    func setTerminalAuthorizationHandler(
        _ handler: CloudTransport.TerminalAuthorizationHandler?
    ) async {}
    func shutdown() async {
        queue.finish()
        readyContinuation.finish()
    }

    func yield(_ text: String, sequence: UInt64) {
        _ = queue.admit(CloudInboundCommand(
            channel: transparencyCommandChannel, sequence: sequence, timestamp: 1,
            commandClass: .ctl, sender: "viewer", plaintext: Data(text.utf8)))
    }
}

private struct CloudTransparencyBridgeKeys {
    let signingKey = CloudDeviceKeyPair()
    let master: CloudMasterSecret

    init() throws {
        master = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x74, count: 32))
    }

    var identity: CloudAppIdentity {
        CloudAppIdentity(machineID: transparencyMachine, deviceID: "machine-device",
                         keyID: "ms-1", masterSecret: master, signingKey: signingKey)
    }

    func open(_ envelope: CloudEnvelope) -> [String: Any] {
        let key = signingKey.publicKeyRaw
        guard let bytes = try? envelope.open(
            masterSecret: master, publicKeyForSender: { $0 == "machine-device" ? key : nil }),
              let object = try? JSONSerialization.jsonObject(with: bytes) as? [String: Any]
        else { return [:] }
        return object
    }

    func plaintext(_ envelope: CloudEnvelope) -> String {
        let key = signingKey.publicKeyRaw
        guard let bytes = try? envelope.open(
            masterSecret: master, publicKeyForSender: { $0 == "machine-device" ? key : nil })
        else { return "" }
        return String(decoding: bytes, as: UTF8.self)
    }

    /// Answers by their `read` name, because lane and inline answers may interleave.
    func answers(_ envelopes: [CloudEnvelope]) -> [String: [String: Any]] {
        var byName: [String: [String: Any]] = [:]
        for envelope in envelopes where envelope.ch.hasPrefix("t/") {
            let object = open(envelope)
            if let name = object["read"] as? String { byName[name] = object }
        }
        return byName
    }

    func notices(_ envelopes: [CloudEnvelope]) -> [[String: Any]] {
        envelopes.filter { $0.ch == transparencyOrchChannel }
            .compactMap { open($0)["cloud_status"] as? [String: Any] }
    }
}

private func cloudTransparencyDurableRuntime(
    status: CloudStatus, limits: CloudSpoolLimits = CloudSpoolLimits()
) throws -> CloudDurableRuntime {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent(
        "clawdline-transparency-\(UUID().uuidString)", isDirectory: true)
    try FileManager.default.createDirectory(
        at: directory, withIntermediateDirectories: true,
        attributes: [.posixPermissions: 0o700])
    return try CloudDurableRuntime.open(
        directory: directory, runtime: .mac, metrics: CloudStatusSpoolMetrics(status: status),
        limits: limits)
}

private func transparencyNow() -> UInt64 { UInt64(Date().timeIntervalSince1970 * 1_000) }

private func transparencyError(_ answer: [String: Any]?) -> [String: Any] {
    answer?["error"] as? [String: Any] ?? [:]
}

// MARK: - Bridge: preflight, ledger, route, reply, notices, reads

func runCloudBridgeTransparencyTests() async throws -> Int {
    let checks = CloudTransparencyChecks()
    let keys = try CloudTransparencyBridgeKeys()

    // T-M1 and T-M3 end to end: a real transport under a real bridge, through the loopback relay.
    // The bridge installs the drop owner through `any CloudTransporting`, which is the seam a
    // fixture transport cannot exercise.
    do {
        let machineKey = CloudDeviceKeyPair()
        let viewerKey = CloudDeviceKeyPair()
        let master = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x75, count: 32))
        let relay = CloudLoopbackRelay(
            account: "transparency-account", deviceID: "machine-e2e",
            devicePublicKey: machineKey.publicKeyRaw, allowedTokens: ["transparency-token"])
        let lines = CloudTransparencyRecorder<String>()
        let transport = CloudTransport(
            relayBaseURL: URL(string: "ws://loopback.invalid/v1/connect")!,
            tokenProvider: CloudTransparencyTokens(),
            keyProvider: CloudStaticTransportKeys(
                deviceKey: machineKey, masterSecrets: ["ms-2": master],
                pairedDevices: ["viewer-e2e": viewerKey.publicKeyRaw]),
            connector: CloudLoopbackSocketConnector(relay: relay),
            inboundQueueLimits: CloudInboundCommandQueueLimits(),
            replayWindow: CloudInboundReplayWindow(), logger: { lines.append($0) })
        let status = CloudStatus()
        let bridge = CloudAppBridge(
            transport: transport,
            identity: CloudAppIdentity(machineID: "machine-e2e", deviceID: "machine-e2e",
                                       keyID: "ms-2", masterSecret: master, signingKey: machineKey),
            sequencing: CloudAppBridgeTestSequence(), commandRouter: CloudTransparencyRouter(),
            status: status, noticeIntervalMilliseconds: 50)
        try await bridge.start()
        try await relay.send(envelope: cloudTransparencySeal(
            "mismatch", sequence: 7, sender: "viewer-e2e", key: viewerKey, keyID: "ms-1",
            master: master, channel: "ctl/machine-e2e"))
        func notices() async -> [[String: Any]] {
            await relay.publishedEnvelopes().filter { $0.ch == "orch/machine-e2e" }.compactMap {
                let bytes = try? $0.open(masterSecret: master, publicKeyForSender: {
                    $0 == "machine-e2e" ? machineKey.publicKeyRaw : nil })
                return bytes.flatMap {
                    (try? JSONSerialization.jsonObject(with: $0)) as? [String: Any]
                }?["cloud_status"] as? [String: Any]
            }
        }
        let published = await cloudTransparencyEventually(timeout: 3) {
            await notices().contains { digest in
                (digest["recent_drops"] as? [[String: Any]] ?? []).contains {
                    $0["code"] as? String == "key_id_mismatch" && $0["seq"] as? Int == 7
                        && $0["key_id"] as? String == "ms-1"
                        && $0["expected_key_id"] as? String == "ms-2"
                }
            }
        }
        let afterMismatch = await notices()
        checks.check("T-M1 the key mismatch reaches orch/<machine> as a notice", published,
                     "notices=\(afterMismatch)")
        checks.check("T-M1 with a bridge attached the drop line says reply=notice",
                     lines.all().contains {
                        $0.hasPrefix("refusal layer=mac_transport code=key_id_mismatch sender=viewer-e2e seq=7 ")
                            && $0.contains(" reply=notice ")
                     }, "lines=\(lines.all())")
        let ten = try cloudTransparencySeal(
            "ten", sequence: 10, sender: "viewer-e2e", key: viewerKey, keyID: "ms-2",
            master: master, channel: "ctl/machine-e2e")
        try await relay.send(envelope: ten)
        try await relay.send(envelope: ten)
        let replayed = await cloudTransparencyEventually(timeout: 3) {
            await notices().contains { digest in
                (digest["recent_drops"] as? [[String: Any]] ?? []).contains {
                    $0["code"] as? String == "replay" && $0["highest_seq"] as? Int == 10
                }
            }
        }
        let afterReplay = await notices()
        checks.check("T-M3 the replay reaches the notice with highest_seq", replayed,
                     "notices=\(afterReplay)")
        checks.check("T-M1 the status snapshot counts both drops by code",
                     status.droppedCount(.keyIDMismatch) == 1 && status.droppedCount(.replay) == 1)
        await bridge.stop()
        await relay.stop()
    }

    // T-P1, P2, M4 reply locations, §11.4 request on answer and dispatch, T-F1, cloud.status and
    // diagnostics.report — one fixture bridge with a status owner.
    do {
        let transport = CloudAppBridgeTestTransport()
        let router = CloudTransparencyRouter()
        let gate = CloudAppBridgeTestGate()
        let results = CloudAppBridgeTestResults()
        let lines = CloudTransparencyRecorder<String>()
        let status = CloudStatus()
        let bridge = CloudAppBridge(
            transport: transport, identity: keys.identity, sequencing: CloudAppBridgeTestSequence(),
            allowCloudCommands: { gate.get() }, commandRouter: router,
            commandResult: { results.append($0) }, diagnostic: { lines.append($0) },
            status: status, noticeIntervalMilliseconds: 50)
        try await bridge.start()

        transport.yield(#"{"type":"send","session":"plain","request":"p1-disabled","text":"secret words","images":[]}"#,
                        sequence: 51)
        _ = await cloudTransparencyEventually { keys.answers(transport.envelopes())["action:p1-disabled"] != nil }
        gate.set(true)
        transport.yield(#"{"type":"erase","session":"__clawdline_machine__","request":"p1-unknown"}"#,
                        sequence: 52)
        transport.yield(#"{"type":"schedule-run","session":"__clawdline_machine__","request":"p1-malformed","id":""}"#,
                        sequence: 53)
        transport.yield(#"{"type":"places","session":"__clawdline_machine__","request":"p1-read","extra":1}"#,
                        sequence: 54)
        transport.yield("not json", sequence: 55)
        transport.yield(#"{"type":"send","session":"plain","request":"p1-wrong","text":"x","images":[]}"#,
                        sequence: 56, channel: "ctl/another-mac")
        transport.yield(#"{"type":"answer","session":"plain","answer":"1","request":"p1-answer"}"#,
                        sequence: 57)
        transport.yield(#"{"type":"dispatch","task":{"title":"t"},"request":"p1-dispatch"}"#,
                        sequence: 58, commandClass: .dispatch)
        _ = await cloudTransparencyEventually {
            let names = Set(keys.answers(transport.envelopes()).keys)
            return names.isSuperset(of: ["action:p1-unknown", "action:p1-malformed", "read:p1-read",
                                         "action:p1-answer", "action:p1-dispatch"])
        }
        let answers = keys.answers(transport.envelopes())
        func preflight(_ name: String, _ code: String, _ httpStatus: Int, _ seq: Int) -> Bool {
            let answer = answers[name]
            let error = transparencyError(answer)
            return answer?["status"] as? Int == httpStatus && error["code"] as? String == code
                && error["layer"] as? String == "mac_preflight" && error["seq"] as? Int == seq
        }
        checks.check("T-P1 writes disabled answers with layer and seq (P2)",
                     preflight("action:p1-disabled", "cloud_commands_disabled", 403, 51),
                     "\(String(describing: answers["action:p1-disabled"]))")
        checks.check("T-P1 an unknown command on the machine channel is answered",
                     preflight("action:p1-unknown", "unknown_command", 400, 52),
                     "\(String(describing: answers["action:p1-unknown"]))")
        checks.check("T-P1 a malformed command with a request is answered",
                     preflight("action:p1-malformed", "malformed_command", 400, 53),
                     "\(String(describing: answers["action:p1-malformed"]))")
        checks.check("T-P1 a malformed request-scoped read is answered",
                     preflight("read:p1-read", "malformed_read", 400, 54),
                     "\(String(describing: answers["read:p1-read"]))")
        checks.check("T-P1 dispatch with a request is answered on the machine channel",
                     preflight("action:p1-dispatch", "cloud_dispatch_unpinned", 409, 58),
                     "\(String(describing: answers["action:p1-dispatch"]))")
        let routedAnswers = await router.recorded()
        checks.check("§11.4 answer with a request is routed, not malformed, and answered",
                     answers["action:p1-answer"]?["status"] as? Int == 200
                        && routedAnswers.contains { $0.hasPrefix("answer(") },
                     "\(String(describing: answers["action:p1-answer"]))")
        checks.check("T-P1 the unanswerable refusals are notices",
                     await cloudTransparencyEventually {
                        let notices = status.snapshot()["notices"] as? [[String: Any]] ?? []
                        let pairs = notices.map { ($0["seq"] as? Int ?? -1, $0["code"] as? String ?? "") }
                        return pairs.contains { $0 == (55, "malformed_command") }
                            && pairs.contains { $0 == (56, "wrong_machine") }
                     }, "\(String(describing: status.snapshot()["notices"]))")
        checks.check("M4 wrong_machine publishes nothing on a channel the viewer never reads",
                     !transport.envelopes().contains {
                        keys.open($0)["read"] as? String == "action:p1-wrong"
                     })
        checks.check("T-P1 a notice was published into orch/<machine>",
                     await cloudTransparencyEventually {
                        keys.notices(transport.envelopes()).contains { digest in
                            (digest["recent_notices"] as? [[String: Any]] ?? []).contains {
                                $0["seq"] as? Int == 55 && $0["layer"] as? String == "mac_preflight"
                            }
                        }
                     }, "notices=\(keys.notices(transport.envelopes()))")
        checks.check("§2.3 the unknown command is one refusal line with its ref",
                     lines.all().contains(
                        "cloud: refusal layer=mac_preflight code=unknown_command sender=viewer seq=52 "
                            + "request=p1-unknown type=erase session=__clawdline_machine__ status=400 "
                            + "reply=published"), "lines=\(lines.all())")
        checks.check("§2.3 the unanswerable refusal says reply=notice",
                     lines.all().contains { $0.hasPrefix(
                        "cloud: refusal layer=mac_preflight code=wrong_machine sender=viewer seq=56 ")
                        && $0.hasSuffix(" status=409 reply=notice") }, "lines=\(lines.all())")

        // T-F1: the route answers busy.
        await router.answerWith(CloudCommandResult(
            status: 409, code: "busy",
            body: Data(#"{"error":{"code":"busy","message":"The terminal is busy."}}"#.utf8)))
        transport.yield(#"{"type":"send","session":"plain","request":"f1-busy","text":"x","images":[]}"#,
                        sequence: 61)
        _ = await cloudTransparencyEventually { keys.answers(transport.envelopes())["action:f1-busy"] != nil }
        let busy = transparencyError(keys.answers(transport.envelopes())["action:f1-busy"])
        checks.check("T-F1 a route refusal carries layer=mac_route and seq",
                     busy["code"] as? String == "busy" && busy["layer"] as? String == "mac_route"
                        && busy["seq"] as? Int == 61 && busy["message"] as? String != nil,
                     "\(busy)")
        checks.check("T-F1 the route refusal has its §2.3 line",
                     await cloudTransparencyEventually {
                        lines.all().contains(
                            "cloud: refusal layer=mac_route code=busy sender=viewer seq=61 "
                                + "request=f1-busy type=send session=plain status=409 reply=published")
                     }, "lines=\(lines.all())")
        await router.answerWith(CloudCommandResult(
            status: 200, code: nil, body: Data(#"{"ok":true}"#.utf8)))

        // §11.5 diagnostics.report, with remote writes switched off.
        gate.set(false)
        transport.yield(#"{"type":"diagnostics.report","session":"__clawdline_machine__","request":"dr-1","report":{"completeness":{"whole":true}}}"#,
                        sequence: 81)
        _ = await cloudTransparencyEventually { keys.answers(transport.envelopes())["action:dr-1"] != nil }
        let reportCommands = await router.recorded()
        checks.check("§11.5 diagnostics.report is read-level and routed as its own command",
                     keys.answers(transport.envelopes())["action:dr-1"]?["status"] as? Int == 200
                        && reportCommands.contains { $0.hasPrefix("diagnosticsReport(") },
                     "commands=\(reportCommands)")

        // diagnostics.events, still with remote writes switched off: the page sends it without a
        // press, so it must not need the switch for typing into a session.
        transport.yield(#"{"type":"diagnostics.events","session":"__clawdline_machine__","request":"ve-1","batch":{"v":1}}"#,
                        sequence: 82)
        transport.yield(#"{"type":"diagnostics.events","session":"__clawdline_machine__","request":"ve-2","batch":[1]}"#,
                        sequence: 83)
        _ = await cloudTransparencyEventually {
            let names = keys.answers(transport.envelopes())
            return names["action:ve-1"] != nil && names["action:ve-2"] != nil
        }
        let eventAnswers = keys.answers(transport.envelopes())
        let eventCommands = await router.recorded()
        checks.check("diagnostics.events is read-level and routed as its own command",
                     eventAnswers["action:ve-1"]?["status"] as? Int == 200
                        && eventCommands.contains { $0.hasPrefix("diagnosticsEvents(") },
                     "commands=\(eventCommands) answer=\(String(describing: eventAnswers["action:ve-1"]))")
        let notABatch = transparencyError(eventAnswers["action:ve-2"])
        checks.check("diagnostics.events whose batch is not an object is answered malformed_command",
                     eventAnswers["action:ve-2"]?["status"] as? Int == 400
                        && notABatch["code"] as? String == "malformed_command"
                        && notABatch["layer"] as? String == "mac_preflight",
                     "\(String(describing: eventAnswers["action:ve-2"]))")

        // §11.3 cloud.status.
        transport.yield(#"{"type":"cloud.status","session":"__clawdline_machine__","request":"cs-1"}"#,
                        sequence: 91)
        _ = await cloudTransparencyEventually { keys.answers(transport.envelopes())["read:cs-1"] != nil }
        let snapshot = keys.answers(transport.envelopes())["read:cs-1"]?["body"] as? [String: Any]
        let rows = snapshot?["commands"] as? [[String: Any]] ?? []
        checks.check("§11.3 cloud.status answers the full snapshot on read:<request>",
                     keys.answers(transport.envelopes())["read:cs-1"]?["status"] as? Int == 200
                        && snapshot?["clawdline_cloud_status"] as? Int == 1,
                     "\(String(describing: snapshot))")
        let busyRow = rows.first { $0["seq"] as? Int == 61 }
        checks.check("§11.3 a command row has the pinned shape with its refusal",
                     busyRow?["sender"] as? String == "viewer"
                        && busyRow?["request"] as? String == "f1-busy"
                        && busyRow?["type"] as? String == "send"
                        && busyRow?["session"] as? String == "plain"
                        && busyRow?["accepted_at_ms"] as? Int != nil
                        && busyRow?["executed_at_ms"] as? Int != nil
                        && busyRow?.keys.contains("delivered_at_ms") == true
                        && busyRow?.keys.contains("undeliverable") == true
                        && (busyRow?["refusal"] as? [String: Any])?["layer"] as? String == "mac_route",
                     "\(String(describing: busyRow))")
        let snapshotBytes = (try? JSONSerialization.data(withJSONObject: status.snapshot())) ?? Data()
        checks.check("§4.1 the snapshot holds no command text",
                     snapshotBytes.range(of: Data("secret words".utf8)) == nil
                        && !snapshotBytes.isEmpty)
        await bridge.stop()
    }

    // T-M4: ingress refusals answer every command that names a request; plaintext cap is terminal.
    do {
        let transport = CloudAppBridgeTestTransport()
        let gate = CloudAppBridgeTestGate()
        gate.set(true)
        let status = CloudStatus()
        let bridge = CloudAppBridge(
            transport: transport, identity: keys.identity, sequencing: CloudAppBridgeTestSequence(),
            allowCloudCommands: { gate.get() }, commandRouter: CloudTransparencyRouter(),
            status: status, noticeIntervalMilliseconds: 50)
        try await bridge.start()
        func refused(_ text: String, _ sequence: UInt64) -> CloudInboundCommand {
            CloudInboundCommand(channel: transparencyCommandChannel, sequence: sequence, timestamp: 1,
                                commandClass: .ctl, sender: "viewer", plaintext: Data(text.utf8))
        }
        await transport.refuse(refused(#"{"type":"key","session":"plain","key":"y","request":"m4-key"}"#, 101),
                               reason: .countCap)
        await transport.refuse(refused(#"{"type":"send","session":"plain","request":"m4-large","text":"x","images":[]}"#, 102),
                               reason: .plaintextCap)
        await transport.refuse(refused(#"{"type":"answer","session":"plain","answer":"2"}"#, 103),
                               reason: .countCap)
        _ = await cloudTransparencyEventually {
            let names = keys.answers(transport.envelopes()).keys
            return names.contains("action:m4-key") && names.contains("action:m4-large")
        }
        let answers = keys.answers(transport.envelopes())
        let key = transparencyError(answers["action:m4-key"])
        checks.check("T-M4 a key command refused at ingress is answered, not timed out",
                     answers["action:m4-key"]?["status"] as? Int == 429
                        && key["code"] as? String == "cloud_ingress_busy"
                        && key["layer"] as? String == "mac_transport" && key["seq"] as? Int == 101
                        && (key["detail"] as? [String: Any])?["lane"] as? String == "ingress",
                     "\(key)")
        let large = transparencyError(answers["action:m4-large"])
        checks.check("T-M4 plaintext over the ceiling answers the terminal command_too_large",
                     answers["action:m4-large"]?["status"] as? Int == 413
                        && large["code"] as? String == "command_too_large", "\(large)")
        checks.check("T-M4 an answer with no request is announced as a notice",
                     await cloudTransparencyEventually {
                        (status.snapshot()["notices"] as? [[String: Any]] ?? []).contains {
                            $0["seq"] as? Int == 103 && $0["code"] as? String == "cloud_ingress_busy"
                                && $0["layer"] as? String == "mac_transport"
                        }
                     }, "\(String(describing: status.snapshot()["notices"]))")
        await bridge.stop()
    }

    // T-M5: a full refusal lane counts the drop and announces it.
    do {
        let transport = CloudTransparencyFailingTransport(delayMilliseconds: 400)
        let lines = CloudTransparencyRecorder<String>()
        let status = CloudStatus()
        let bridge = CloudAppBridge(
            transport: transport, identity: keys.identity, sequencing: CloudAppBridgeTestSequence(),
            commandRouter: CloudTransparencyRouter(), diagnostic: { lines.append($0) },
            refusalPublications: CloudRefusalPublicationQueue(maximumCount: 1, deadlineMilliseconds: 2_000),
            status: status, noticeIntervalMilliseconds: 50)
        try await bridge.start()
        transport.yield(#"{"type":"send","session":"plain","request":"m5-a","text":"x","images":[]}"#,
                        sequence: 111)
        transport.yield(#"{"type":"send","session":"plain","request":"m5-b","text":"x","images":[]}"#,
                        sequence: 112)
        let counted = await cloudTransparencyEventually {
            (status.snapshot()["reply"] as? [String: Any])?["lane_dropped"] as? Int == 1
        }
        checks.check("T-M5 lane_dropped counts the refusal the lane could not take", counted,
                     "\(String(describing: status.snapshot()["reply"]))")
        checks.check("T-M5 the dropped refusal has a notice line",
                     lines.all().contains(
                        "cloud: refusal layer=mac_preflight code=cloud_commands_disabled sender=viewer "
                            + "seq=112 request=m5-b type=send session=plain status=403 reply=notice "
                            + "lane=dropped_full"), "lines=\(lines.all())")
        checks.check("T-M5 the notice is published",
                     await cloudTransparencyEventually(timeout: 3) {
                        keys.notices(transport.envelopes()).contains { digest in
                            (digest["recent_notices"] as? [[String: Any]] ?? []).contains {
                                $0["seq"] as? Int == 112
                            }
                        }
                     }, "notices=\(keys.notices(transport.envelopes()))")
        await bridge.stop()
    }

    // T-L1, T-L2, T-L3, T-L4 on the durable ledger.
    do {
        let transport = CloudAppBridgeTestTransport()
        let authority = CloudTransparencyAuthority()
        let lines = CloudTransparencyRecorder<String>()
        let status = CloudStatus()
        let runtime = try cloudTransparencyDurableRuntime(status: status)
        let bridge = CloudAppBridge(
            transport: transport, identity: keys.identity, sequencing: CloudAppBridgeTestSequence(),
            allowCloudCommands: { true },
            currentCommandEffectAuthority: { _, _ in authority.current() },
            commandRouter: CloudTransparencyRouter(), diagnostic: { lines.append($0) },
            durableRuntime: runtime, status: status, noticeIntervalMilliseconds: 50)
        try await bridge.start()
        func send(_ request: String, _ sequence: UInt64) async -> [String: Any]? {
            let before = transport.envelopes().count
            transport.yield(#"{"type":"send","session":"plain","request":"\#(request)","text":"ledger secret","images":[]}"#,
                            sequence: sequence, timestamp: transparencyNow(),
                            channel: transparencyCommandChannel)
            _ = await cloudTransparencyEventually { transport.envelopes().count > before }
            guard transport.envelopes().count > before else { return nil }
            let envelope = transport.envelopes()[before]
            return keys.open(envelope)
        }
        func acknowledge(_ kind: CloudOutboundTransportReceiptKind = .delivered) {
            if let last = transport.envelopes().last { transport.acknowledge(last, kind: kind) }
        }

        authority.set(CloudCommandEffectAuthorization(
            epochState: .ready, rosterAllowsSender: true, writeGateAllows: true))
        _ = await send("l4-ok", 121)
        acknowledge()
        checks.check("T-L4 the delivered reply is marked delivered",
                     await cloudTransparencyEventually {
                        status.commandRows().first { $0.sequence == 121 }?.deliveredAt != nil
                     }, "\(status.commandRows())")

        authority.set(CloudCommandEffectAuthorization(
            epochState: .uncertain, rosterAllowsSender: true, writeGateAllows: true,
            epochReason: "stability_period_incomplete", epochClearsInMilliseconds: 41_000))
        let clock = await send("l4-clock", 122)
        acknowledge()
        let clockError = transparencyError(clock)
        let clockDetail = clockError["detail"] as? [String: Any]
        checks.check("T-L1 a ledger refusal names layer, code and seq",
                     clockError["code"] as? String == "command_clock_uncertain"
                        && clockError["layer"] as? String == "mac_ledger"
                        && clockError["seq"] as? Int == 122, "\(clockError)")
        checks.check("T-L1 the durability sentence is no longer the only information",
                     clockError["message"] as? String
                        != "The command durability boundary is unavailable.", "\(clockError)")
        checks.check("T-L2 detail carries the guard's reason and clears_in_ms",
                     clockDetail?["reason"] as? String == "stability_period_incomplete"
                        && clockDetail?["clears_in_ms"] as? Int == 41_000,
                     "\(String(describing: clockDetail))")
        checks.check("T-L2 the refusal line carries the detail",
                     lines.all().contains(
                        "cloud: refusal layer=mac_ledger code=command_clock_uncertain sender=viewer "
                            + "seq=122 request=l4-clock type=send session=plain status=503 "
                            + "reply=published detail.clears_in_ms=41000 "
                            + "detail.reason=stability_period_incomplete"), "lines=\(lines.all())")

        authority.set(CloudCommandEffectAuthorization(
            epochState: .ready, rosterAllowsSender: false, writeGateAllows: false,
            rosterReadable: false))
        let unreadable = transparencyError(await send("l3-roster", 123))
        acknowledge()
        authority.set(CloudCommandEffectAuthorization(
            epochState: .ready, rosterAllowsSender: true, writeGateAllows: false))
        let disabled = transparencyError(await send("l3-writes", 124))
        acknowledge()
        authority.set(CloudCommandEffectAuthorization(
            epochState: .ready, rosterAllowsSender: false, writeGateAllows: true))
        let revoked = transparencyError(await send("l3-revoked", 125))
        acknowledge()
        checks.check("T-L3 an unreadable roster and disabled writes are two codes",
                     unreadable["code"] as? String == "command_roster_unreadable"
                        && disabled["code"] as? String == "command_writes_disabled"
                        && revoked["code"] as? String == "unknown_sender"
                        && revoked["layer"] as? String == "mac_ledger",
                     "\(unreadable) \(disabled) \(revoked)")

        authority.set(CloudCommandEffectAuthorization(
            epochState: .ready, rosterAllowsSender: true, writeGateAllows: true))
        _ = await send("l4-reject", 126)
        acknowledge(.peerRejected(CloudOutboundPeerRejection(
            code: .forbidden, field: nil, disposition: .terminal)))
        _ = await cloudTransparencyEventually {
            status.commandRows().first { $0.sequence == 126 }?.undeliverable != nil
        }
        let rows = status.commandRows()
        let ok = rows.first { $0.sequence == 121 }
        let refusedRow = rows.first { $0.sequence == 122 }
        let rejected = rows.first { $0.sequence == 126 }
        checks.check("T-L4 the successful command has every Mac step",
                     ok?.acceptedAt != nil && ok?.executedAt != nil && ok?.outcome == "succeeded"
                        && ok?.deliveredAt != nil && ok?.refusal == nil && ok?.type == "send"
                        && ok?.request == "l4-ok", "\(String(describing: ok))")
        checks.check("T-L4 the ledger-refused command never executed and names its refusal",
                     refusedRow?.acceptedAt != nil && refusedRow?.executedAt == nil
                        && refusedRow?.refusal?.layer == .macLedger
                        && refusedRow?.refusal?.code == "command_clock_uncertain",
                     "\(String(describing: refusedRow))")
        checks.check("T-L4 the command whose reply failed executed and is undeliverable",
                     rejected?.executedAt != nil && rejected?.undeliverable == "peer_rejected",
                     "\(String(describing: rejected))")
        let bytes = (try? JSONSerialization.data(withJSONObject: status.snapshot())) ?? Data()
        checks.check("T-L4 the snapshot never holds the command text",
                     !bytes.isEmpty && bytes.range(of: Data("ledger secret".utf8)) == nil)
        await bridge.stop()
    }

    // T-G1: a reply that cannot be handed over, and one whose receipt never comes.
    do {
        let transport = CloudTransparencyFailingTransport()
        let status = CloudStatus()
        let lines = CloudTransparencyRecorder<String>()
        let bridge = CloudAppBridge(
            transport: transport, identity: keys.identity, sequencing: CloudAppBridgeTestSequence(),
            allowCloudCommands: { true }, commandRouter: CloudTransparencyRouter(),
            diagnostic: { lines.append($0) }, status: status, noticeIntervalMilliseconds: 50)
        try await bridge.start()
        transport.yield(#"{"type":"send","session":"plain","request":"g1-fail","text":"x","images":[]}"#,
                        sequence: 131)
        let undeliverable = await cloudTransparencyEventually {
            (status.snapshot()["reply"] as? [String: Any])?["undeliverable"] as? Int == 1
        }
        let row = status.commandRows().first { $0.sequence == 131 }
        checks.check("T-G1 a reply that could not be published counts undeliverable", undeliverable,
                     "\(String(describing: status.snapshot()["reply"]))")
        checks.check("T-G1 the executed command is marked undeliverable, not lost",
                     row?.executedAt != nil && row?.undeliverable == "command_answer_undeliverable",
                     "\(String(describing: row))")
        checks.check("T-G1 the undeliverable answer has a notice line",
                     lines.all().contains { $0.hasPrefix(
                        "cloud: refusal layer=mac_reply code=command_answer_undeliverable sender=viewer "
                            + "seq=131 request=g1-fail type=send session=plain status=503 reply=notice") },
                     "lines=\(lines.all())")
        await bridge.stop()

        var limits = CloudSpoolLimits()
        limits.attemptWindow = .milliseconds(150)
        let expiringStatus = CloudStatus()
        let runtime = try cloudTransparencyDurableRuntime(status: expiringStatus, limits: limits)
        let silent = CloudAppBridgeTestTransport()
        let expiring = CloudAppBridge(
            transport: silent, identity: keys.identity, sequencing: CloudAppBridgeTestSequence(),
            allowCloudCommands: { true }, commandRouter: CloudTransparencyRouter(),
            durableRuntime: runtime, status: expiringStatus, noticeIntervalMilliseconds: 50)
        try await expiring.start()
        silent.yield(#"{"type":"send","session":"plain","request":"g1-expire","text":"x","images":[]}"#,
                     sequence: 132, timestamp: transparencyNow(), channel: transparencyCommandChannel)
        let expired = await cloudTransparencyEventually(timeout: 4) {
            (expiringStatus.snapshot()["reply"] as? [String: Any])?["expired_receipt"] as? Int == 1
        }
        checks.check("T-G1 a sent reply with no receipt counts expired_receipt", expired,
                     "\(String(describing: expiringStatus.snapshot()["reply"]))")
        checks.check("T-G1 the command whose receipt expired is marked receipt_expired",
                     expiringStatus.commandRows().first { $0.sequence == 132 }?.undeliverable
                        == "receipt_expired", "\(expiringStatus.commandRows())")
        await expiring.stop()
    }

    // §11.2 notice: merged into the orchestrator snapshot, status-only before one, coalesced.
    do {
        let transport = CloudAppBridgeTestTransport()
        let status = CloudStatus()
        let bridge = CloudAppBridge(
            transport: transport, identity: keys.identity, sequencing: CloudAppBridgeTestSequence(),
            commandRouter: CloudTransparencyRouter(), status: status,
            noticeIntervalMilliseconds: 800)
        try await bridge.start()
        status.recordDrop(CloudInboundDrop(
            code: .replay, sender: "web_n2", sequence: 9, highestSequence: 10))
        _ = await cloudTransparencyEventually { !keys.notices(transport.envelopes()).isEmpty }
        let firstAt = Date()
        let statusOnly = transport.envelopes().first { $0.ch == transparencyOrchChannel }
            .map { keys.open($0) } ?? [:]
        let drop = ((statusOnly["cloud_status"] as? [String: Any])?["recent_drops"]
            as? [[String: Any]])?.first
        checks.check("§11.2 before any orchestrator payload the notice is a status-only object",
                     Set(statusOnly.keys) == ["cloud_status"]
                        && (statusOnly["cloud_status"] as? [String: Any])?["v"] as? Int == 1
                        && drop?["code"] as? String == "replay" && drop?["highest_seq"] as? Int == 10
                        && drop?.keys.contains("key_id") == true,
                     "\(statusOnly)")
        for sequence in 11...13 {
            status.recordDrop(CloudInboundDrop(code: .replay, sender: "web_n2",
                                               sequence: UInt64(sequence), highestSequence: 13))
        }
        try? await Task.sleep(nanoseconds: 200_000_000)
        checks.check("§11.2 notices are coalesced inside the interval",
                     keys.notices(transport.envelopes()).count == 1,
                     "count=\(keys.notices(transport.envelopes()).count)")
        let second = await cloudTransparencyEventually(timeout: 3) {
            keys.notices(transport.envelopes()).count == 2
        }
        checks.check("§11.2 the coalesced changes are published once after the interval",
                     second && Date().timeIntervalSince(firstAt) >= 0.6,
                     "count=\(keys.notices(transport.envelopes()).count)")
        let snapshot = Data(#"{"tasks":[{"id":"t1"}],"app":{"version":"1"}}"#.utf8)
        try await bridge.publishOrchestrator(snapshot)
        let merged = transport.envelopes().last { $0.ch == transparencyOrchChannel }
        let mergedText = merged.map { keys.plaintext($0) } ?? ""
        let mergedObject = merged.map { keys.open($0) } ?? [:]
        checks.check("§11.2 the digest is merged into the orchestrator payload",
                     (mergedObject["tasks"] as? [[String: Any]])?.first?["id"] as? String == "t1"
                        && (mergedObject["cloud_status"] as? [String: Any])?["v"] as? Int == 1,
                     mergedText)
        checks.check("§11.2 the orchestrator bytes are kept, the digest spliced in",
                     mergedText.hasPrefix(#"{"tasks":[{"id":"t1"}],"app":{"version":"1"},"cloud_status":"#),
                     mergedText)
        await bridge.stop()
    }

    // §11.2 the 8 KiB cap, enforced by trimming the recent lists.
    do {
        let status = CloudStatus()
        // The widest values the envelope grammar allows: 128-byte senders and 64-byte key ids made
        // of `"`, which JSON must escape to two bytes each.
        let wide = String(repeating: "\"", count: 128)
        for sequence in 0..<20 {
            status.recordDrop(CloudInboundDrop(
                code: .keyIDMismatch, sender: wide, sequence: UInt64(sequence),
                keyID: String(repeating: "\"", count: 64),
                expectedKeyID: String(repeating: "\"", count: 64)))
            status.recordRefusal(
                sender: wide, sequence: UInt64(1_000 + sequence),
                request: String(repeating: "\"", count: 128), type: "send", session: "plain",
                layer: .macPreflight, code: "malformed_command", reply: .notice)
        }
        let digest = status.noticeDigest()
        let bytes = (try? JSONSerialization.data(withJSONObject: digest)) ?? Data()
        let drops = digest["recent_drops"] as? [[String: Any]] ?? []
        let notices = digest["recent_notices"] as? [[String: Any]] ?? []
        checks.check("§11.2 the serialized digest is at most 8 KiB",
                     !bytes.isEmpty && bytes.count <= 8 * 1024, "bytes=\(bytes.count)")
        checks.check("§11.2 the cap trimmed the recent lists rather than the counts",
                     digest["v"] as? Int == 1 && !drops.isEmpty && drops.count <= 10
                        && notices.count <= 10 && drops.count + notices.count < 20
                        && (digest["dropped"] as? [String: Any])?["key_id_mismatch"] as? Int == 20,
                     "drops=\(drops.count) notices=\(notices.count)")
        checks.check("§11.2 the newest rows survive the trim",
                     drops.first?["seq"] as? Int == 19, "\(String(describing: drops.first))")
    }

    // §4.2 the fixed file: same directory as DiagnosticReport, 0600, written on change.
    do {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(
            "clawdline-transparency-home-\(UUID().uuidString)", isDirectory: true)
        let url = CloudStatus.fileURL(root: root)
        checks.check("§4.2 cloud-status.json sits beside report.json",
                     url == DiagnosticReport.directory(root: root)
                        .appendingPathComponent("cloud-status.json"), url.path)
        let status = CloudStatus(fileURL: url, writeIntervalMilliseconds: 100)
        status.recordDrop(CloudInboundDrop(code: .replay, sender: "web_f", sequence: 1,
                                           highestSequence: 1))
        status.writeFileNow()
        let attributes = try? FileManager.default.attributesOfItem(atPath: url.path)
        let written = (try? Data(contentsOf: url))
            .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
        checks.check("§4.2 the file is written with mode 0600",
                     (attributes?[.posixPermissions] as? NSNumber)?.intValue == 0o600,
                     "\(String(describing: attributes?[.posixPermissions]))")
        checks.check("§4.2 the file holds the snapshot",
                     written?["clawdline_cloud_status"] as? Int == 1
                        && ((written?["inbound"] as? [String: Any])?["dropped"] as? [String: Any])?["replay"]
                            as? Int == 1, "\(String(describing: written))")
        status.enableFileWriting()
        status.recordDrop(CloudInboundDrop(code: .replay, sender: "web_f", sequence: 1,
                                           highestSequence: 1))
        checks.check("§4.2 a change is written without being asked",
                     await cloudTransparencyEventually {
                        let object = (try? Data(contentsOf: url))
                            .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
                        return ((object?["inbound"] as? [String: Any])?["dropped"] as? [String: Any])?["replay"]
                            as? Int == 2
                     })
        try? FileManager.default.removeItem(at: root)
    }

    // §11.5 the report route itself: the same store and words, with the sender as written_by.
    do {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(
            "clawdline-transparency-report-\(UUID().uuidString)", isDirectory: true)
        let audits = CloudTransparencyRecorder<String>()
        let written = CloudDiagnosticsReportRoute.route(
            body: Data(#"{"completeness":{"whole":true}}"#.utf8), sender: "web_report",
            root: root, audit: { event, fields in audits.append(event + ":" + (fields["ok"] ?? "")) })
        let file = (try? Data(contentsOf: DiagnosticReport.reportURL(root: root)))
            .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
        let receipt = (try? JSONSerialization.jsonObject(with: written.body)) as? [String: Any]
        checks.check("§11.5 a report is written to the fixed file with the envelope sender",
                     written.status == 200 && file?["written_by"] as? String == "web_report"
                        && receipt?["path"] as? String == DiagnosticReport.reportURL(root: root).path,
                     "\(written) \(String(describing: file))")
        let refused = CloudDiagnosticsReportRoute.route(
            body: Data("[1]".utf8), sender: "web_report", root: root, audit: { _, _ in })
        checks.check("§11.5 refusals use the route's own codes",
                     refused.status == 400 && refused.code == "report_not_json",
                     "\(refused)")
        checks.check("§11.5 the write is audited like the HTTP route",
                     audits.all() == ["diagnostics.report:1"], "\(audits.all())")
        try? FileManager.default.removeItem(at: root)
    }

    // diagnostics.events at its store: one fixed JSONL file beside report.json, one line per
    // batch stating its own completeness, bounded input, rotation, one counts-only log line, a
    // resent batch recognised from its id, and the batches the page really seals.
    do {
        let manager = FileManager.default
        let root = manager.temporaryDirectory.appendingPathComponent(
            "clawdline-viewer-events-\(UUID().uuidString)", isDirectory: true)
        // `rowTab: nil` leaves the key out of every row.
        func viewerBatch(_ id: String, rows: Int, data: [String: Any]? = nil, n: Any? = nil,
                         rowTab: Any? = "tab-1", nTo: Int? = nil, refused: Int = 0,
                         unflushed: Int = 0) -> Data {
            let kept = (0..<rows).map { index -> [String: Any] in
                var row: [String: Any] = [
                    "n": n ?? (index + 1), "at_ms": 1_789_400_000_000 + index,
                    "event": "cloud.receive.failed",
                    "data": data ?? ["code": "unknown_sender", "stage": "sender_key_lookup",
                                     "seq": 9, "realign": true, "senders_with_keys": ["mac-device"]]]
                row["tab"] = rowTab
                return row
            }
            let batch: [String: Any] = [
                "v": 1, "batch_id": id, "created_at_ms": 1_789_400_000_500,
                "device": "viewer-device", "tab": "tab-1", "web_build": "b0123",
                "rows": kept,
                "completeness": [
                    "n_from": 1, "n_to": nTo ?? rows, "rows": rows, "dropped_rate_limited": 47,
                    "dropped_overflow": 0, "dropped_refused": refused,
                    "dropped_unflushed": unflushed, "storage_errors": 0,
                    "counting_since_ms": 1_789_400_000_000,
                    "rate_limited": [["key": "cloud.receive.failed|unknown_sender",
                                      "event": "cloud.receive.failed", "dropped": 47,
                                      "first_at_ms": 1, "last_at_ms": 2, "sample_n": 3]
                                     as [String: Any]],
                    "limits": ["rows": 100, "per_key_per_window": 3],
                ] as [String: Any],
            ]
            return (try? JSONSerialization.data(withJSONObject: batch)) ?? Data()
        }
        let audits = CloudTransparencyRecorder<String>()
        let logs = CloudTransparencyRecorder<String>()
        func send(_ body: Data) -> CloudCommandResult {
            CloudViewerEventsRoute.route(
                body: body, sender: "viewer-device", root: root,
                audit: { event, fields in audits.append(event + ":" + (fields["ok"] ?? "")) },
                log: { logs.append($0) })
        }
        let url = CloudViewerEventLog.fileURL(root: root)
        func lines() -> [[String: Any]] {
            ((try? String(contentsOf: url, encoding: .utf8)) ?? "").split(separator: "\n").compactMap {
                (try? JSONSerialization.jsonObject(with: Data($0.utf8))) as? [String: Any]
            }
        }
        let firstBody = viewerBatch("b-1", rows: 3)
        let first = send(firstBody)
        let receipt = (try? JSONSerialization.jsonObject(with: first.body)) as? [String: Any]
        let line = lines().first
        checks.check("viewer events land in cloud-viewer-events.jsonl beside report.json",
                     url == DiagnosticReport.directory(root: root)
                        .appendingPathComponent("cloud-viewer-events.jsonl"), url.path)
        checks.check("an accepted batch answers a receipt naming its batch and its rows",
                     first.status == 200 && receipt?["batch_id"] as? String == "b-1"
                        && receipt?["rows"] as? Int == 3 && receipt?["path"] as? String == url.path
                        && receipt?["duplicate"] as? Bool == false,
                     "\(first) \(String(describing: receipt))")
        checks.check("the line names the envelope sender and states its own completeness",
                     lines().count == 1 && line?["device"] as? String == "viewer-device"
                        && line?["row_count"] as? Int == 3
                        && line?["completeness_consistent"] as? Bool == true
                        && (line?["completeness"] as? [String: Any])?["dropped_rate_limited"] as? Int == 47
                        && ((line?["rows"] as? [[String: Any]])?.first?["data"] as? [String: Any])?["stage"]
                            as? String == "sender_key_lookup",
                     "\(String(describing: line))")
        let mode = (try? manager.attributesOfItem(atPath: url.path))?[.posixPermissions] as? NSNumber
        checks.check("the viewer events file is 0600", mode?.intValue == 0o600, "\(String(describing: mode))")
        checks.check("an accepted batch writes one counts-only Clawdline.log line",
                     logs.all().count == 1
                        && logs.all()[0].hasPrefix("cloud viewer events: appended rows=3 dropped_rate_limited=47 dropped_overflow=0 dropped_refused=0 dropped_unflushed=0 storage_errors=0 consistent=1 ")
                        && !logs.all()[0].contains("unknown_sender"),
                     "\(logs.all())")
        _ = send(viewerBatch("b-2", rows: 1))
        checks.check("a second batch appends a second line", lines().count == 2, "\(lines().count)")

        let huge = send(Data(repeating: 0x20, count: CloudViewerEventLog.maxBatchBytes + 1))
        let nested = send(viewerBatch("b-3", rows: 1, data: ["kept": ["secret_value": "TOPSECRET"]]))
        let boolean = send(viewerBatch("b-4", rows: 1, n: true))
        checks.check("an oversized batch is refused by name before it is parsed",
                     huge.status == 413 && huge.code == "viewer_events_too_large", "\(huge)")
        let nestedMessage = String(data: nested.body, encoding: .utf8) ?? ""
        checks.check("a nested value is malformed at its path, and the refusal repeats no value",
                     nested.status == 400 && nested.code == "viewer_events_malformed"
                        && nestedMessage.contains("rows[0].data.kept")
                        && !nestedMessage.contains("TOPSECRET"),
                     nestedMessage)
        checks.check("a boolean is not a row number",
                     boolean.status == 400 && boolean.code == "viewer_events_malformed"
                        && (String(data: boolean.body, encoding: .utf8) ?? "").contains("rows[0].n"),
                     "\(boolean)")
        checks.check("refused batches write nothing and log only their code",
                     lines().count == 2
                        && logs.all().contains("cloud viewer events: refused code=viewer_events_too_large received_bytes=\(CloudViewerEventLog.maxBatchBytes + 1)")
                        && audits.all() == ["diagnostics.events:1", "diagnostics.events:1",
                                            "diagnostics.events:0", "diagnostics.events:0",
                                            "diagnostics.events:0"],
                     "\(audits.all()) \(logs.all())")
        checks.check("viewer events never touch report.json or previous.json",
                     !manager.fileExists(atPath: DiagnosticReport.reportURL(root: root).path)
                        && !manager.fileExists(atPath: DiagnosticReport.previousURL(root: root).path))

        // After the audit and log counts above, which a resend would change.
        let size = ((try? manager.attributesOfItem(atPath: url.path))?[.size] as? NSNumber)?.intValue ?? 0
        let resent = send(firstBody)
        let resentReceipt = (try? JSONSerialization.jsonObject(with: resent.body)) as? [String: Any]
        let resentSize = ((try? manager.attributesOfItem(atPath: url.path))?[.size] as? NSNumber)?.intValue ?? 0
        checks.check("a batch resent after its receipt was lost answers a duplicate receipt naming its rows and appends nothing",
                     resent.status == 200 && resentReceipt?["duplicate"] as? Bool == true
                        && resentReceipt?["batch_id"] as? String == "b-1"
                        && resentReceipt?["rows"] as? Int == 3 && resentReceipt?["line_bytes"] as? Int == 0
                        && lines().count == 2 && resentSize == size
                        && logs.all().last?.hasPrefix("cloud viewer events: duplicate rows=3 ") == true,
                     "\(resent) \(String(describing: resentReceipt)) lines=\(lines().count) \(logs.all())")

        let rolled = CloudViewerEventLog.append(viewerBatch("b-5", rows: 1), device: "viewer-device",
                                                root: root, maxFileBytes: size + 16)
        let rotatedURL = CloudViewerEventLog.rotatedURL(root: root)
        let rotatedLines = ((try? String(contentsOf: rotatedURL, encoding: .utf8)) ?? "").split(separator: "\n")
        checks.check("a line that would pass the bound rotates the file to .1 first",
                     (try? rolled.get())?.rotated == true && lines().count == 1
                        && lines().first?["batch_id"] as? String == "b-5" && rotatedLines.count == 2,
                     "\(rolled) rotated=\(rotatedLines.count) current=\(lines().count)")
        let rotatedResend = CloudViewerEventLog.append(viewerBatch("b-2", rows: 1), device: "viewer-device",
                                                       root: root)
        let rotatedAfter = ((try? String(contentsOf: rotatedURL, encoding: .utf8)) ?? "").split(separator: "\n")
        let onlyRotated = rotatedLines.contains { $0.contains(#""batch_id":"b-2""#) }
        checks.check("a batch whose line has rotated to .1 is still a duplicate: the id index survives rotation",
                     (try? rotatedResend.get())?.duplicate == true && onlyRotated
                        && lines().count == 1 && rotatedAfter.count == 2,
                     "\(rotatedResend) rotated=\(rotatedAfter.count) current=\(lines().count)")

        let tabless = send(viewerBatch("b-6", rows: 1, rowTab: nil))
        let longTab = send(viewerBatch("b-7", rows: 1, rowTab: String(repeating: "t", count: 129)))
        checks.check("a row without its tab, or with a tab over 128 characters, is malformed at its path",
                     tabless.status == 400 && tabless.code == "viewer_events_malformed"
                        && (String(data: tabless.body, encoding: .utf8) ?? "").contains("rows[0].keys")
                        && longTab.code == "viewer_events_malformed"
                        && (String(data: longTab.body, encoding: .utf8) ?? "").contains("rows[0].tab")
                        && lines().count == 1,
                     "\(tabless) \(longTab)")
        let unflushed = send(viewerBatch("b-8", rows: 2, nTo: 3, refused: 4, unflushed: 1))
        let unflushedLine = lines().last
        checks.check("unflushed rows consume their n and refused rows do not: n 1…3, 2 rows, 1 unflushed, 4 refused is consistent",
                     unflushed.status == 200 && lines().count == 2
                        && unflushedLine?["batch_id"] as? String == "b-8"
                        && unflushedLine?["completeness_consistent"] as? Bool == true
                        && logs.all().last?.contains("dropped_refused=4 dropped_unflushed=1 storage_errors=0 consistent=1 ") == true,
                     "\(unflushed) \(String(describing: unflushedLine)) \(logs.all().last ?? "")")
        try? manager.removeItem(at: root)

        let boundedRoot = manager.temporaryDirectory.appendingPathComponent(
            "clawdline-viewer-events-bounded-\(UUID().uuidString)", isDirectory: true)
        func appendBounded(_ id: String) -> CloudViewerEventLog.Receipt? {
            try? CloudViewerEventLog.append(viewerBatch(id, rows: 1), device: "viewer-device",
                                            root: boundedRoot, rememberedBatches: 2).get()
        }
        let firstFive = ["c-1", "c-2", "c-3", "c-4", "c-5"].map { appendBounded($0) }
        let indexURL = CloudViewerEventLog.batchIndexURL(root: boundedRoot)
        let indexLines = ((try? String(contentsOf: indexURL, encoding: .utf8)) ?? "").split(separator: "\n")
        let indexMode = (try? manager.attributesOfItem(atPath: indexURL.path))?[.posixPermissions] as? NSNumber
        let newestAgain = appendBounded("c-5")
        let oldestAgain = appendBounded("c-1")
        let fiveAppended = firstFive.allSatisfy { $0?.duplicate == false }
        checks.check("the remembered batch ids are bounded: 5 ids with 2 remembered leave at most 4 lines, the newest is still a duplicate, the oldest appends again",
                     fiveAppended && !indexLines.isEmpty
                        && indexLines.count <= 4 && indexMode?.intValue == 0o600
                        && newestAgain?.duplicate == true
                        && oldestAgain?.duplicate == false && (oldestAgain?.lineBytes ?? 0) > 0,
                     "index=\(indexLines) mode=\(String(describing: indexMode)) c-5=\(String(describing: newestAgain)) c-1=\(String(describing: oldestAgain))")
        try? manager.removeItem(at: boundedRoot)

        // The page→Mac contract: batches the page's real `ViewerEventLog.takeBatch` sealed, written
        // by `Tests/web-cloud-failures.mjs`, re-encoded exactly as `CloudAppBridge` hands them to
        // this store. A missing fixture fails: a contract check that skips says nothing either way.
        let contractPath = "Tests/cloud-viewer-events-contract-batches.json"
        let contract = (try? Data(contentsOf: URL(fileURLWithPath: contractPath)))
            .flatMap { (try? JSONSerialization.jsonObject(with: $0)) as? [String: Any] }
        let contractBatches = contract?["batches"] as? [[String: Any]] ?? []
        checks.check("page→Mac contract: \(contractPath) is v1 from Tests/web-cloud-failures.mjs and holds batches",
                     contract?["v"] as? Int == 1
                        && contract?["generated_by"] as? String == "Tests/web-cloud-failures.mjs"
                        && !contractBatches.isEmpty,
                     "cwd=\(manager.currentDirectoryPath) keys=\(contract.map { Array($0.keys).sorted() } ?? []) batches=\(contractBatches.count)")
        var shapeFailures: [String] = []
        var appendFailures: [String] = []
        for (index, batch) in contractBatches.enumerated() {
            let rowCount = (batch["rows"] as? [Any])?.count ?? -1
            guard JSONSerialization.isValidJSONObject(batch),
                  let encoded = try? JSONSerialization.data(
                    withJSONObject: batch, options: [.withoutEscapingSlashes]) else {
                shapeFailures.append("batch \(index) cannot be encoded as CloudAppBridge does")
                continue
            }
            if let problem = CloudViewerEventLog.problem(in: batch) {
                shapeFailures.append("batch \(index) is malformed at \(problem)")
            }
            if encoded.count > CloudViewerEventLog.maxBatchBytes {
                shapeFailures.append("batch \(index) is \(encoded.count) bytes")
            }
            let contractRoot = manager.temporaryDirectory.appendingPathComponent(
                "clawdline-viewer-events-contract-\(UUID().uuidString)", isDirectory: true)
            switch CloudViewerEventLog.append(encoded, device: "contract-device", root: contractRoot) {
            case .success(let stored):
                if !stored.consistent || stored.duplicate || stored.rows != rowCount {
                    appendFailures.append("batch \(index) consistent=\(stored.consistent) duplicate=\(stored.duplicate) rows=\(stored.rows) of \(rowCount)")
                }
            case .failure(let refusal):
                appendFailures.append("batch \(index) refused \(refusal.code): \(refusal.message)")
            }
            try? manager.removeItem(at: contractRoot)
        }
        checks.check("page→Mac contract: every batch the page sealed is viewer events v1 within maxBatchBytes",
                     !contractBatches.isEmpty && shapeFailures.isEmpty, "\(shapeFailures)")
        checks.check("page→Mac contract: every batch the page sealed appends once, consistent, with its row count",
                     !contractBatches.isEmpty && appendFailures.isEmpty, "\(appendFailures)")
        let contractRows = contractBatches.flatMap { $0["rows"] as? [[String: Any]] ?? [] }
        func holdsNonASCII(_ value: Any) -> Bool {
            if let text = value as? String { return text.unicodeScalars.contains { $0.value > 0x7F } }
            if let list = value as? [Any] { return list.contains { holdsNonASCII($0) } }
            if let object = value as? [String: Any] {
                return object.contains { holdsNonASCII($0.key) || holdsNonASCII($0.value) }
            }
            return false
        }
        let fullBatch = contractBatches.contains { ($0["rows"] as? [Any])?.count == 100 }
        let widestData = contractRows.contains { ($0["data"] as? [String: Any])?.count == 48 }
        let truncatedRow = contractRows.contains { row in
            guard let flag = (row["data"] as? [String: Any])?["row_truncated"] as? NSNumber else {
                return false
            }
            return CFGetTypeID(flag) == CFBooleanGetTypeID() && flag.boolValue
        }
        let nonASCII = holdsNonASCII(contractBatches)
        checks.check("page→Mac contract: the fixture is worst-case, not trivially small",
                     fullBatch && widestData && truncatedRow && nonASCII,
                     "100_rows=\(fullBatch) 48_keys=\(widestData) row_truncated=\(truncatedRow) non_ascii=\(nonASCII)")
    }

    // L2 at its source: the real guard's reason and countdown.
    do {
        let wall = Date(timeIntervalSince1970: 1_789_290_000)
        let authority = CloudCommandEpochAuthority(wall: { wall }, kernelNanoseconds: { _ in 5_000_000_000 })
        let before = authority.effectAuthorization(
            rosterAllowsSender: true, writeGateAllows: true, rosterReadable: true)
        authority.acceptAuthenticatedServerDate(wall)
        let window = authority.effectAuthorization(
            rosterAllowsSender: true, writeGateAllows: true, rosterReadable: true)
        checks.check("T-L2 no server time yet is its own reason with no countdown",
                     before.epochState == .uncertain && before.epochReason == "awaiting_server_time"
                        && before.epochClearsInMilliseconds == nil,
                     "\(before)")
        checks.check("T-L2 a running stability window names itself and counts down",
                     window.epochReason == "stability_period_incomplete"
                        && window.epochClearsInMilliseconds == 60_000,
                     "\(window)")
    }

    return try checks.finish("CloudBridgeTransparency")
}
