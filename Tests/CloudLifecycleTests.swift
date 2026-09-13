import Foundation
import CryptoKit

private struct CloudLifecycleTestFailure: Error, CustomStringConvertible {
    let failures: [String]
    let checks: Int

    var description: String {
        "CloudLifecycleTests: \(failures.count)/\(checks) failed — "
            + failures.joined(separator: "; ")
    }
}

private struct ForcedLifecycleFailure: Error, LocalizedError {
    var errorDescription: String? { "forced lifecycle failure" }
}

/// A transport with no relay behind it. Commands and ready generations are pushed by the test,
/// which is the only way to drive the bridge's two inbound streams deterministically.
private final class LifecycleTestTransport: CloudTransporting, @unchecked Sendable {
    let commands: CloudInboundCommandStream
    let readyGenerations: AsyncStream<UInt64>
    private let commandQueue: CloudInboundCommandQueue
    private let readyContinuation: AsyncStream<UInt64>.Continuation
    private let lock = NSLock()
    private var connectCount = 0
    private var shutdownCount = 0
    private var published: [CloudEnvelope] = []
    let connectError: Error?
    let publicationEffect: (@Sendable () throws -> Void)?
    let buffersPublication: Bool

    init(connectError: Error? = nil, buffersPublication: Bool = false,
         publicationEffect: (@Sendable () throws -> Void)? = nil) {
        self.connectError = connectError
        self.buffersPublication = buffersPublication
        self.publicationEffect = publicationEffect
        let commandQueue = CloudInboundCommandQueue(limits: CloudInboundCommandQueueLimits(
            maximumCount: 10_000, maximumChargedBytes: Int.max
        ))
        self.commandQueue = commandQueue
        commands = commandQueue.stream
        var readyContinuation: AsyncStream<UInt64>.Continuation!
        readyGenerations = AsyncStream(bufferingPolicy: .bufferingNewest(1)) { readyContinuation = $0 }
        self.readyContinuation = readyContinuation
    }

    // The counters are bumped through synchronous helpers: NSLock is not usable across an
    // await, and taking it inside an `async` body is the shape Swift 6 rejects outright.
    private func countConnect() { lock.lock(); connectCount += 1; lock.unlock() }
    private func countShutdown() { lock.lock(); shutdownCount += 1; lock.unlock() }
    private func record(_ envelope: CloudEnvelope) {
        lock.lock(); published.append(envelope); lock.unlock()
    }

    func connect(role: CloudTransportRole) async throws {
        countConnect()
        if let connectError { throw connectError }
    }

    func publish(envelope: CloudEnvelope) async throws {
        try publicationEffect?()
        if buffersPublication { return } // Models CloudTransport's reconnect buffer, no socket send.
        record(envelope)
    }

    func sendExactPublishFrame(_ bytes: Data) async throws {
        let frame = try JSONDecoder().decode(CloudPublishFrame.self, from: bytes)
        try await publish(envelope: frame.envelope)
    }

    func shutdown() async {
        countShutdown()
        commandQueue.finish()
        readyContinuation.finish()
    }

    func deliver(_ command: CloudInboundCommand) { _ = commandQueue.admit(command) }
    func setInboundRefusalHandler(_ handler: CloudTransport.InboundRefusalHandler?) async {}
    func setTerminalAuthorizationHandler(
        _ handler: CloudTransport.TerminalAuthorizationHandler?
    ) async {}
    func becameReady(_ generation: UInt64) { readyContinuation.yield(generation) }
    func connects() -> Int { lock.lock(); defer { lock.unlock() }; return connectCount }
    func shutdowns() -> Int { lock.lock(); defer { lock.unlock() }; return shutdownCount }
    func publishedEnvelopes() -> [CloudEnvelope] {
        lock.lock(); defer { lock.unlock() }; return published
    }
}

/// Deterministic stage time: no wall-clock sleep and no credentials/content in diagnostics.
private final class LifecyclePublicationClock: @unchecked Sendable {
    private let lock = NSLock()
    private var value: UInt64 = 10_000
    private var lines: [String] = []
    func now() -> UInt64 { lock.lock(); defer { lock.unlock() }; return value }
    func advance(_ amount: UInt64) { lock.lock(); value += amount; lock.unlock() }
    func record(_ line: String) { lock.lock(); lines.append(line); lock.unlock() }
    func logs() -> [String] { lock.lock(); defer { lock.unlock() }; return lines }
}

private actor LifecycleTimedSequence: CloudEnvelopeSequencing {
    let clock: LifecyclePublicationClock
    let gate: LifecycleSequenceCancellationGate?
    init(_ clock: LifecyclePublicationClock, gate: LifecycleSequenceCancellationGate? = nil) {
        self.clock = clock; self.gate = gate
    }
    func nextSequence(sender: String) async throws -> UInt64 {
        clock.advance(1_200)
        if let gate {
            await withTaskCancellationHandler {
                await withCheckedContinuation { gate.install($0) }
            } onCancel: { gate.release() }
        }
        return 1
    }
}

/// Lifecycle tests keep a deliberately in-memory compatibility sequencer. Production
/// composition never calls this seam: `CloudDurableOutboundComposition` owns sequence
/// reservation through the durable spool.
private actor LifecycleTestSequence: CloudEnvelopeSequencing {
    private var next: UInt64 = 0

    init(url _: URL? = nil, block _: UInt64 = 0) {}

    func nextSequence(sender _: String) async throws -> UInt64 {
        defer { next &+= 1 }
        return next
    }
}

private final class LifecycleSequenceCancellationGate: @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<Void, Never>?
    private var released = false
    func install(_ next: CheckedContinuation<Void, Never>) {
        lock.lock()
        if released { lock.unlock(); next.resume(); return }
        continuation = next; lock.unlock()
    }
    func waiting() -> Bool { lock.lock(); defer { lock.unlock() }; return continuation != nil }
    func release() {
        lock.lock(); released = true
        let next = continuation; continuation = nil; lock.unlock()
        next?.resume()
    }
}

private actor LifecycleTestRouter: CloudCommandRouting {
    private var routed: [(CloudHeadlessCommand, String, String)] = []
    private var reads: [(CloudHeadlessRead, String)] = []

    func route(_ command: CloudHeadlessCommand, sender: String,
               idempotencyKey: String) async -> CloudCommandResult {
        routed.append((command, sender, idempotencyKey))
        return CloudCommandResult(status: 200, code: nil)
    }

    /// This suite drives commands only; a read that reached it would be a lifecycle change
    /// nobody asked for, so it is recorded rather than silently answered as a success.
    func read(_ read: CloudHeadlessRead, sender: String) async -> CloudReadResult {
        reads.append((read, sender))
        return CloudReadResult(status: 200, body: Data("{}".utf8))
    }

    func count() -> Int { routed.count }
    func readCount() -> Int { reads.count }
    func lastIdempotencyKey() -> String? { routed.last?.2 }
}

private final class LifecycleTestResults: @unchecked Sendable {
    private let lock = NSLock()
    private var values: [CloudCommandResult] = []
    func record(_ result: CloudCommandResult) {
        lock.lock()
        values.append(result)
        lock.unlock()
    }
    func all() -> [CloudCommandResult] {
        lock.lock()
        defer { lock.unlock() }
        return values
    }
}

private final class LifecycleIdentityBox: @unchecked Sendable {
    private let lock = NSLock()
    private var identity: CloudMachineIdentity?
    init(_ identity: CloudMachineIdentity?) { self.identity = identity }
    func get() -> CloudMachineIdentity? {
        lock.lock(); defer { lock.unlock() }; return identity
    }
    func set(_ identity: CloudMachineIdentity?) {
        lock.lock(); self.identity = identity; lock.unlock()
    }
}

private final class LifecycleOneShotDurableFault: @unchecked Sendable {
    private let lock = NSLock()
    private var fired = false

    func failFirstPersist(_ point: CloudDurableStoreFaultPoint) throws {
        lock.lock()
        defer { lock.unlock() }
        guard point == .persist, !fired else { return }
        fired = true
        throw CloudDurableStoreFailure.persist
    }

    var didFire: Bool {
        lock.lock()
        defer { lock.unlock() }
        return fired
    }
}

private final class LifecycleReadGate: @unchecked Sendable {
    let entered = DispatchSemaphore(value: 0)
    let release = DispatchSemaphore(value: 0)
    private let lock = NSLock()
    private var finished = false

    func wait() {
        entered.signal()
        release.wait()
        lock.lock(); finished = true; lock.unlock()
    }

    func hasFinished() -> Bool {
        lock.lock(); defer { lock.unlock() }; return finished
    }

    func awaitEntry() -> Bool {
        entered.wait(timeout: .now() + 1) == .success
    }
}

private final class LifecycleBlockingKeyStore: CloudKeyStoring, @unchecked Sendable {
    let coordinator = CloudKeyStoreCoordinator()
    private let condition = NSCondition()
    private var entered = false
    private var released = false

    func data(for account: String) throws -> Data? {
        condition.lock()
        entered = true
        condition.broadcast()
        let deadline = Date().addingTimeInterval(3)
        while !released && condition.wait(until: deadline) {}
        condition.unlock()
        return nil
    }

    func set(_ data: Data, for account: String) throws {}
    func remove(_ account: String) throws {}

    func awaitEntry() -> Bool {
        condition.lock()
        let deadline = Date().addingTimeInterval(2)
        while !entered && condition.wait(until: deadline) {}
        let result = entered
        condition.unlock()
        return result
    }

    func release() {
        condition.lock(); released = true; condition.broadcast(); condition.unlock()
    }
}

/// Empty protected identity authority for production-composition tests. Using the default
/// Keychain-backed authority here makes the test depend on whether this Mac is already enrolled:
/// an enrolled developer machine bypasses the injected hanging key store entirely.
private final class LifecycleEmptySecretStore: SecretStore, @unchecked Sendable {
    let capabilities: Set<HostCapability> = [.secrets]
    private let lock = NSLock()
    private var values: [String: Data] = [:]

    func data(for account: String) throws -> Data? {
        lock.lock(); defer { lock.unlock() }
        return values[account]
    }

    func set(_ data: Data, for account: String) throws {
        lock.lock(); values[account] = data; lock.unlock()
    }

    func loadOrCreate(_ account: String, create: @Sendable () throws -> Data) throws -> Data {
        lock.lock(); defer { lock.unlock() }
        if let value = values[account] { return value }
        let value = try create()
        values[account] = value
        return value
    }

    func rotate(_ account: String, replace: @Sendable (Data?) throws -> Data) throws -> Data {
        lock.lock(); defer { lock.unlock() }
        let value = try replace(values[account])
        values[account] = value
        return value
    }

    func remove(_ account: String) throws {
        lock.lock(); values.removeValue(forKey: account); lock.unlock()
    }
}

private struct LifecycleUnusedHTTPTransport: CloudAccountHTTPTransport {
    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        throw ForcedLifecycleFailure()
    }
}

private struct LifecycleTestTokenProvider: CloudDeviceTokenProviding, Sendable {
    let error: Error?

    func fetchDeviceToken() async throws -> CloudDeviceToken {
        if let error { throw error }
        return CloudDeviceToken(value: "token", expiresAt: Date().addingTimeInterval(300))
    }
}

/// Stands in for `RemoteServer.attachCloudBridge`, including the half that matters here: it
/// stops whatever it replaces. A recorder that only appended would have let "signing out shuts
/// the transport down" pass against a lifecycle that never shut anything down.
@MainActor
private final class AttachRecorder {
    private(set) var attaches: [CloudAppBridge?] = []
    var startAttached = false
    private var live: CloudAppBridge?

    func attach(_ bridge: CloudAppBridge?) {
        attaches.append(bridge)
        let previous = live
        live = bridge
        Task {
            if let previous { await previous.stop() }
            guard let bridge, self.startAttached else { return }
            try? await bridge.start()
        }
    }

    var attachedCount: Int { attaches.filter { $0 != nil }.count }
    var detachedCount: Int { attaches.filter { $0 == nil }.count }
}

@MainActor
private final class ScheduleWebhookWorkerRecorder {
    private(set) var identities: [CloudMachineIdentity?] = []
    func apply(_ identity: CloudMachineIdentity?, _ unauthorized: @escaping @Sendable () -> Void) {
        identities.append(identity)
    }
}

private struct CloudLifecycleTests {
    static var checks = 0
    static var failures: [String] = []

    static func check(_ name: String, _ condition: @autoclosure () -> Bool,
                      _ detail: @autoclosure () -> String = "") {
        checks += 1
        if !condition() {
            let extra = detail()
            failures.append(extra.isEmpty ? name : "\(name) — \(extra)")
        }
    }

    static func eventually(_ predicate: () async -> Bool) async -> Bool {
        for _ in 0..<500 {
            if await predicate() { return true }
            try? await Task.sleep(nanoseconds: 2_000_000)
        }
        return await predicate()
    }

    static func temporaryDirectory() -> URL {
        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent("clawdline-cloud-lifecycle-\(UUID().uuidString)",
                                    isDirectory: true)
        try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    // MARK: - Test-only compatibility sequencing

    static func testSequenceFile() async {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("cloud-sequence.json")

        let first = LifecycleTestSequence(url: url, block: 4)
        var handed: [UInt64] = []
        for _ in 0..<6 { handed.append((try? await first.nextSequence(sender: "mac")) ?? .max) }
        check("the first sequence a machine hands out is zero", handed.first == 0)
        check("sequences advance by exactly one", handed == [0, 1, 2, 3, 4, 5], "\(handed)")
        check("the compatibility seam is not a persisted production authority",
              CloudContractCandidateAuthority.w0E.authority == "candidate"
                  && !CloudContractCandidateAuthority.w0E.permitsEmission(wireVersion: 2))
    }

    // MARK: - Pinned viewer devices

    static func testPairedDeviceStore() {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("cloud-devices.json")
        let store = CloudPairedDeviceStore(url: url)
        let key = Data((0..<32).map { UInt8($0) })
        let replacement = Data((0..<32).map { UInt8(0x40 + $0) })

        check("an empty store lists nothing",
              ((try? store.devices(accountID: "acct")) ?? [nil].compactMap { _ in nil }).isEmpty)
        try? store.pin(CloudPairedDevice(deviceID: "viewer-1", signingKey: key,
                                         pairedAtMilliseconds: 10), accountID: "acct")
        let pinned = (try? store.devices(accountID: "acct")) ?? []
        check("a pinned viewer reads back with its key",
              pinned.count == 1 && pinned.first?.deviceID == "viewer-1"
                  && pinned.first?.signingKey == key)

        try? store.pin(CloudPairedDevice(deviceID: "viewer-1", signingKey: replacement,
                                         pairedAtMilliseconds: 20), accountID: "acct")
        let replaced = (try? store.devices(accountID: "acct")) ?? []
        check("re-pairing the same device replaces its row rather than adding a second",
              replaced.count == 1 && replaced.first?.signingKey == replacement)

        check("another account inherits nothing",
              ((try? store.devices(accountID: "other")) ?? [CloudPairedDevice(
                  deviceID: "x", signingKey: key, pairedAtMilliseconds: 0)]).isEmpty)

        let attributes = try? FileManager.default.attributesOfItem(atPath: url.path)
        let permissions = (attributes?[.posixPermissions] as? NSNumber)?.intValue ?? 0
        check("the pinned list is readable only by its owner", permissions == 0o600,
              String(permissions, radix: 8))

        let migration = try? store.beginProtectedMigration(accountID: "acct")
        check("protected migration preserves the exact legacy roster in its fence",
              migration == replaced)
        check("protected migration removes the pathname a pre-W5 image authorizes",
              !FileManager.default.fileExists(atPath: url.path))
        let restartedMigration = try? CloudPairedDeviceStore(url: url)
            .beginProtectedMigration(accountID: "acct")
        check("protected migration resumes from the same fence after restart",
              restartedMigration == replaced)
        try? store.finishProtectedMigration()
        check("the migration fence is removed only by explicit completion",
              !FileManager.default.fileExists(
                atPath: url.appendingPathExtension("w5-protected-migration").path))

        try? store.forget(deviceID: "viewer-1", accountID: "acct")
        check("forgetting a viewer removes it",
              ((try? store.devices(accountID: "acct")) ?? [CloudPairedDevice(
                  deviceID: "x", signingKey: key, pairedAtMilliseconds: 0)]).isEmpty)

        try? Data("not json at all".utf8).write(to: url)
        var refused = false
        do { _ = try store.devices(accountID: "acct") } catch { refused = true }
        check("an unreadable pinned list refuses rather than answering empty", refused)
    }

    // MARK: - Key handover

    private struct HandoverFixture {
        let offer: CloudPairingOffer
        let handover: CloudPairingHandover
        let viewerEphemeralPrivate: Data
        let machineEphemeralPrivate: Data
        let now: Int64
    }

    private static func makeFixture() throws -> HandoverFixture {
        let viewerEphemeral = Data((0..<32).map { UInt8(0x10 + $0) })
        let machineEphemeral = Data((0..<32).map { UInt8(0x50 + $0) })
        let viewerSigning = try Curve25519.Signing.PrivateKey(
            rawRepresentation: Data((0..<32).map { UInt8(0x80 + $0) }))
        let machineSigning = try Curve25519.Signing.PrivateKey(
            rawRepresentation: Data((0..<32).map { UInt8(0xb0 + $0) }))
        let now: Int64 = 1_787_817_600_000
        let offer = CloudPairingOffer(
            pairingID: "pairing-test-01",
            claimNonce: Data((0..<32).map { UInt8(0x01 + $0) }).base64EncodedString(),
            pairingNonce: Data((0..<32).map { UInt8(0x21 + $0) }).base64EncodedString(),
            accountID: "account-test-01",
            viewerDeviceID: "viewer-test-01",
            viewerSigningKey: viewerSigning.publicKey.rawRepresentation.base64EncodedString(),
            viewerEphemeralKey: try CloudHandover.x25519PublicKey(
                forPrivateKeyRaw: viewerEphemeral).base64EncodedString(),
            viewerFingerprint: try CloudPairing.ed25519Fingerprint(
                publicKeyRaw: viewerSigning.publicKey.rawRepresentation),
            expiresAt: now + 300_000)
        let handover = CloudPairingHandover(
            accountID: offer.accountID,
            machineID: "mac-test-01",
            machineSigningKey: machineSigning.publicKey.rawRepresentation.base64EncodedString(),
            machineFingerprint: try CloudPairing.ed25519Fingerprint(
                publicKeyRaw: machineSigning.publicKey.rawRepresentation),
            keyID: CloudBridgeLifecycle.masterKeyID,
            masterSecret: Data((0..<32).map { UInt8(0xa0 + $0) }).base64EncodedString())
        return HandoverFixture(
            offer: offer, handover: handover,
            viewerEphemeralPrivate: viewerEphemeral,
            machineEphemeralPrivate: machineEphemeral, now: now)
    }

    static func testHandover() {
        guard let fixture = try? makeFixture() else {
            check("the handover fixture builds", false)
            return
        }
        let nonce = Data(repeating: 0, count: 11) + Data([0x2a])
        guard let wrapper = try? CloudHandover.seal(
            fixture.handover, for: fixture.offer, machineDeviceID: fixture.handover.machineID,
            machineEphemeralPrivateKey: fixture.machineEphemeralPrivate,
            nonce: nonce, nowMilliseconds: fixture.now)
        else {
            check("the machine can seal a handover for a viewer offer", false)
            return
        }
        check("the sealed handover is a grant", wrapper.phase == .grant)
        check("the wrapper carries the machine's ephemeral key",
              wrapper.ephemeralKey == (try? CloudHandover.x25519PublicKey(
                  forPrivateKeyRaw: fixture.machineEphemeralPrivate).base64EncodedString()))

        let opened = try? CloudHandover.open(
            wrapper, for: fixture.offer,
            viewerEphemeralPrivateKey: fixture.viewerEphemeralPrivate,
            senderDeviceID: fixture.handover.machineID, nowMilliseconds: fixture.now)
        check("the viewer opens exactly what the machine sealed", opened == fixture.handover)

        var expired = false
        do {
            _ = try CloudHandover.seal(
                fixture.handover, for: fixture.offer,
                machineDeviceID: fixture.handover.machineID,
                machineEphemeralPrivateKey: fixture.machineEphemeralPrivate,
                nonce: nonce, nowMilliseconds: fixture.offer.expiresAt + 1)
        } catch { expired = true }
        check("an expired offer is refused before anything is sealed for it", expired)

        var wrongAccount = false
        var foreign = fixture.handover
        foreign.accountID = "account-somebody-else"
        do {
            _ = try CloudHandover.seal(
                foreign, for: fixture.offer, machineDeviceID: fixture.handover.machineID,
                machineEphemeralPrivateKey: fixture.machineEphemeralPrivate,
                nonce: nonce, nowMilliseconds: fixture.now)
        } catch { wrongAccount = true }
        check("a handover for another account is never sealed into this offer", wrongAccount)

        var wrongSender = false
        do {
            _ = try CloudHandover.open(
                wrapper, for: fixture.offer,
                viewerEphemeralPrivateKey: fixture.viewerEphemeralPrivate,
                senderDeviceID: "some-other-mac", nowMilliseconds: fixture.now)
        } catch { wrongSender = true }
        check("a slot written by a different device than the wrapper claims is refused",
              wrongSender)

        var tampered = wrapper
        var ciphertext = Data(base64Encoded: wrapper.ciphertext) ?? Data()
        ciphertext[0] ^= 0xFF
        tampered.ciphertext = ciphertext.base64EncodedString()
        var authFailed = false
        do {
            _ = try CloudHandover.open(
                tampered, for: fixture.offer,
                viewerEphemeralPrivateKey: fixture.viewerEphemeralPrivate,
                senderDeviceID: fixture.handover.machineID, nowMilliseconds: fixture.now)
        } catch { authFailed = true }
        check("a flipped ciphertext bit fails authentication rather than decoding", authFailed)

        var relabelled = wrapper
        relabelled.senderDeviceID = "another-mac"
        var aadFailed = false
        do {
            _ = try CloudHandover.open(
                relabelled, for: fixture.offer,
                viewerEphemeralPrivateKey: fixture.viewerEphemeralPrivate,
                senderDeviceID: "another-mac", nowMilliseconds: fixture.now)
        } catch { aadFailed = true }
        check("rewriting the sender in the wrapper breaks the authenticated data", aadFailed)

        var badFingerprint = fixture.offer
        badFingerprint.viewerFingerprint = "AAAA-BBBB-CCCC-DDDD"
        var fingerprintRefused = false
        do {
            _ = try CloudHandover.seal(
                fixture.handover, for: badFingerprint,
                machineDeviceID: fixture.handover.machineID,
                machineEphemeralPrivateKey: fixture.machineEphemeralPrivate,
                nonce: nonce, nowMilliseconds: fixture.now)
        } catch { fingerprintRefused = true }
        check("an offer whose fingerprint does not match its key is refused",
              fingerprintRefused)

        let fragment = try? CloudHandover.encodeOfferFragment(
            fixture.offer, nowMilliseconds: fixture.now)
        let decoded = fragment.flatMap {
            try? CloudHandover.decodeOfferFragment($0, nowMilliseconds: fixture.now)
        }
        check("an offer fragment round-trips", decoded == fixture.offer)

        var badFragment = false
        do {
            _ = try CloudHandover.decodeOfferFragment(
                (fragment ?? "") + "=", nowMilliseconds: fixture.now)
        } catch { badFragment = true }
        check("a fragment that is not canonical base64url is refused", badFragment)
    }

    /// The cross-runtime half. `Tests/protocol-vectors.json` is generated by a third
    /// implementation and read by the JavaScript viewer as well, so agreeing with it here is
    /// what makes "the browser can open what this Mac seals" a measured claim.
    static func testHandoverVector(vectorsURL: URL) {
        guard let data = try? Data(contentsOf: vectorsURL),
              let root = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              let vector = root["pairing_handover"] as? [String: Any],
              let fragment = vector["offer_fragment"] as? String,
              let expectedOffer = vector["offer"] as? String,
              let expectedHandover = vector["handover"] as? String,
              let viewerPrivate = (vector["viewer_ephemeral_private_key"] as? String)
                  .flatMap({ Data(base64Encoded: $0) }),
              let sender = vector["sender_device_id"] as? String,
              let now = vector["now_milliseconds"] as? Int64 ?? (vector["now_milliseconds"] as? NSNumber)?.int64Value,
              let wrapperFields = vector["wrapper"] as? [String: Any]
        else {
            check("the protocol vector file carries a pairing handover", false)
            return
        }

        let offer = try? CloudHandover.decodeOfferFragment(fragment, nowMilliseconds: now)
        check("the vector's offer fragment decodes", offer != nil)
        guard let offer else { return }
        check("the vector's offer is the canonical JSON it says it is",
              String(decoding: CloudCanonicalJSON.canonicalData(offer.cloudJSONValue),
                     as: UTF8.self) == expectedOffer)

        guard let phase = (wrapperFields["phase"] as? String).flatMap(CloudPairingPhase.init),
              let pairingID = wrapperFields["pairing_id"] as? String,
              let senderDeviceID = wrapperFields["sender_device_id"] as? String,
              let ephemeralKey = wrapperFields["ephemeral_key"] as? String,
              let nonce = wrapperFields["nonce"] as? String,
              let ciphertext = wrapperFields["ct"] as? String
        else {
            check("the vector's wrapper has its seven members", false)
            return
        }
        let wrapper = CloudPairingWrapper(
            phase: phase, pairingID: pairingID, senderDeviceID: senderDeviceID,
            ephemeralKey: ephemeralKey, nonce: nonce, ciphertext: ciphertext)
        let opened = try? CloudHandover.open(
            wrapper, for: offer, viewerEphemeralPrivateKey: viewerPrivate,
            senderDeviceID: sender, nowMilliseconds: now)
        check("the vector's sealed handover opens against this implementation", opened != nil)
        if let opened {
            check("the opened handover is the canonical payload the vector records",
                  String(decoding: CloudCanonicalJSON.canonicalData(opened.cloudJSONValue),
                         as: UTF8.self) == expectedHandover)
        }
    }

    // MARK: - Token supervision

    static func testSupervisedTokenProvider() async {
        let results = LifecycleTestResults()
        func provider(_ error: Error?) -> CloudSupervisedDeviceTokenProvider {
            CloudSupervisedDeviceTokenProvider(
                inner: LifecycleTestTokenProvider(error: error),
                onTerminalFailure: { _ in results.record(CloudCommandResult(status: 403, code: "revoked")) })
        }

        _ = try? await provider(nil).fetchDeviceToken()
        check("a healthy token fetch reports nothing terminal", results.all().isEmpty)

        var rethrown: CloudTransportError?
        do { _ = try await provider(CloudTransportError.unauthorized).fetchDeviceToken() }
        catch let error as CloudTransportError { rethrown = error }
        catch { rethrown = nil }
        check("a refusal is reported to the lifecycle", results.all().count == 1)
        check("a refusal is still thrown to the transport", rethrown == .unauthorized)

        _ = try? await provider(CloudTransportError.invalidTokenResponse).fetchDeviceToken()
        check("an ordinary token failure is left to the transport's own retry",
              results.all().count == 1)
    }

    // MARK: - The key provider

    static func testKeyProvider() async {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = CloudPairedDeviceStore(
            url: directory.appendingPathComponent("cloud-devices.json"))
        let signing = CloudDeviceKeyPair()
        guard let master = try? CloudMasterSecret() else {
            check("a master secret can be made for the key provider", false)
            return
        }
        let provider = CloudLifecycleKeyProvider(
            deviceKey: signing, masterSecrets: [CloudBridgeLifecycle.masterKeyID: master],
            accountID: "acct", pairedDevices: store)

        let viewerKey = Data((0..<32).map { UInt8(0x30 + $0) })
        try? store.pin(CloudPairedDevice(deviceID: "viewer-1", signingKey: viewerKey,
                                         pairedAtMilliseconds: 1), accountID: "acct")
        let pinned = await provider.pairedDevicePublicKeys()
        check("the transport verifies senders against the pinned list",
              pinned == ["viewer-1": viewerKey])

        try? store.forget(deviceID: "viewer-1", accountID: "acct")
        let afterForget = await provider.pairedDevicePublicKeys()
        check("unpinning a viewer takes effect without rebuilding the transport",
              afterForget.isEmpty)

        let known = try? await provider.masterSecret(for: CloudBridgeLifecycle.masterKeyID)
        check("the account content key is served by its key id", known == master)
        var refusedKey = false
        do { _ = try await provider.masterSecret(for: "ms-99") } catch { refusedKey = true }
        check("an unknown content key id is refused", refusedKey)
    }

    // MARK: - The bridge lifecycle

    @MainActor
    private static func makeLifecycle(
        identity: @escaping @Sendable () throws -> CloudMachineIdentity?,
        recorder: AttachRecorder,
        transports: LifecycleTestTransport,
        router: LifecycleTestRouter = LifecycleTestRouter(),
        results: LifecycleTestResults = LifecycleTestResults(),
        allowCommands: @escaping @Sendable () -> Bool = { false },
        appIdentityFails: Bool = false,
        webhookWorker: ScheduleWebhookWorkerRecorder? = nil,
        durableRuntime: (@MainActor (CloudMachineIdentity) throws -> CloudDurableRuntime)? = nil,
        sequenceDirectory: URL
    ) -> CloudBridgeLifecycle {
        let signing = CloudDeviceKeyPair()
        let master = try? CloudMasterSecret()
        let sequence = LifecycleTestSequence(
            url: sequenceDirectory.appendingPathComponent("cloud-sequence.json"))
        let identityReader = CloudKeychainReader<CloudBridgeLifecycle.RestoredIdentity?> {
            guard let machine = try identity() else { return nil }
            if appIdentityFails { throw ForcedLifecycleFailure() }
            guard let master else { throw ForcedLifecycleFailure() }
            return CloudBridgeLifecycle.RestoredIdentity(
                machine: machine,
                app: CloudAppIdentity(
                    machineID: machine.machineID, deviceID: machine.machineID,
                    keyID: CloudBridgeLifecycle.masterKeyID,
                    masterSecret: master, signingKey: signing))
        }
        return CloudBridgeLifecycle(services: CloudBridgeLifecycle.Services(
            identityReader: identityReader,
            makeTransport: { _, _, _ in transports },
            sequencing: { _ in sequence },
            durableRuntime: durableRuntime,
            attach: { recorder.attach($0) },
            allowCloudCommands: allowCommands,
            commandRouter: { router },
            commandResult: { results.record($0) },
            log: { _ in },
            scheduleWebhooks: { identity, unauthorized in
                webhookWorker?.apply(identity, unauthorized)
            }))
    }

    @MainActor
    static func testMacLifecycleComposesSharedDurability() async {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let recorder = AttachRecorder()
        recorder.startAttached = true
        let transport = LifecycleTestTransport()
        let machine = CloudMachineIdentity(accountID: "acct", machineID: "mac-durable")
        let lifecycle = makeLifecycle(
            identity: { machine }, recorder: recorder, transports: transport,
            durableRuntime: { identity in
                try CloudDurableRuntime.open(
                    directory: directory.appendingPathComponent(identity.machineID), runtime: .mac)
            }, sequenceDirectory: directory)
        lifecycle.apply()
        let attached = await eventually {
            recorder.attachedCount == 1 && transport.connects() == 1
        }
        check("Mac lifecycle opens the shared ledger and spool before attachment", attached)
        check("candidate authority cannot cut over, emit, or raise a floor",
              CloudContractCandidateAuthority.w0E.authority == "candidate"
                  && CloudContractCandidateAuthority.w0E.cutoverRequired
                  && !CloudContractCandidateAuthority.w0E.permitsNormativeCutover
                  && !CloudContractCandidateAuthority.w0E.permitsEmission(wireVersion: 1)
                  && !CloudContractCandidateAuthority.w0E.permitsClientFloorRaise(to: "2"))
        lifecycle.signedOut()
        let detached = await eventually {
            recorder.detachedCount == 1 && transport.shutdowns() == 1
        }
        check("detaching releases the durable Mac composition with its bridge", detached)

        let refusedRecorder = AttachRecorder()
        let refused = makeLifecycle(
            identity: { machine }, recorder: refusedRecorder,
            transports: LifecycleTestTransport(),
            durableRuntime: { _ in throw CloudDurableStoreFailure.recovery },
            sequenceDirectory: directory)
        refused.apply()
        let failedClosed = await eventually {
            if case .failed = refused.state { return true }
            return false
        }
        check("a durable-store startup failure prevents Mac bridge attachment",
              failedClosed && refusedRecorder.attachedCount == 0)
    }

    @MainActor
    static func testAttachmentIsSingular() async {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let recorder = AttachRecorder()
        let transport = LifecycleTestTransport()
        let identity = LifecycleIdentityBox(
            CloudMachineIdentity(accountID: "acct", machineID: "mac-1"))
        let webhookWorker = ScheduleWebhookWorkerRecorder()
        let lifecycle = makeLifecycle(
            identity: { identity.get() }, recorder: recorder, transports: transport,
            webhookWorker: webhookWorker,
            sequenceDirectory: directory)

        lifecycle.apply()
        let firstAttached = await eventually { recorder.attachedCount == 1 }
        check("a restored Mac attaches a bridge without being asked twice",
              firstAttached, "\(recorder.attaches.count)")
        check("the attached state names the account and machine",
              lifecycle.state == .attached(accountID: "acct", machineID: "mac-1"))
        check("a restored signed-in identity starts exactly one webhook worker",
              webhookWorker.identities.compactMap { $0 }.map(\.machineID) == ["mac-1"])
        let generation = lifecycle.generation

        lifecycle.apply()
        lifecycle.apply()
        let refreshResolved = await eventually { lifecycle.identityKnowledge == .resolved }
        check("re-applying with the same identity leaves the live bridge alone",
              refreshResolved && lifecycle.generation == generation && recorder.attaches.count == 1)

        identity.set(CloudMachineIdentity(accountID: "acct", machineID: "mac-2"))
        lifecycle.apply()
        let changedIdentityAttached = await eventually {
            lifecycle.state == .attached(accountID: "acct", machineID: "mac-2")
        }
        check("a changed machine identity replaces the bridge",
              changedIdentityAttached)
        check("replacing detaches the old bridge before attaching the new one",
              recorder.attaches.count == 3 && recorder.attaches[1] == nil
                  && recorder.attaches[2] != nil)
        check("identity replacement stops then starts the lifecycle-owned webhook worker",
              webhookWorker.identities.count == 3 && webhookWorker.identities[1] == nil
                  && webhookWorker.identities[2]?.machineID == "mac-2")

        lifecycle.signedOut()
        check("signing out detaches", lifecycle.state == .detached && recorder.detachedCount == 2)
        check("nothing is attached after signing out", lifecycle.attachedBridge == nil)
        check("sign-out stops webhook polling", webhookWorker.identities.last! == nil)
    }

    @MainActor
    static func testNoCredentialAndFailures() async {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }

        let quiet = AttachRecorder()
        let lifecycle = makeLifecycle(
            identity: { nil }, recorder: quiet, transports: LifecycleTestTransport(),
            sequenceDirectory: directory)
        lifecycle.apply()
        lifecycle.apply()
        let signedOutResolved = await eventually { lifecycle.identityKnowledge == .resolved }
        check("a Mac that is not signed in attaches nothing and does not churn",
              signedOutResolved && quiet.attaches.isEmpty && lifecycle.state == .detached)

        let throwing = AttachRecorder()
        let unreadable = makeLifecycle(
            identity: { throw ForcedLifecycleFailure() }, recorder: throwing,
            transports: LifecycleTestTransport(), sequenceDirectory: directory)
        unreadable.apply()
        let unreadableResolved = await eventually { unreadable.identityKnowledge == .resolved }
        check("an unreadable credential store reports failed rather than attaching",
              unreadableResolved && unreadable.attachedBridge == nil)
        if case .failed = unreadable.state {
            check("the failure carries a reason", true)
        } else {
            check("the failure carries a reason", false, "\(unreadable.state)")
        }

        let broken = AttachRecorder()
        let noKeys = makeLifecycle(
            identity: { CloudMachineIdentity(accountID: "acct", machineID: "mac-1") },
            recorder: broken, transports: LifecycleTestTransport(),
            appIdentityFails: true, sequenceDirectory: directory)
        noKeys.apply()
        let keyFailureResolved = await eventually { noKeys.identityKnowledge == .resolved }
        check("a Keychain failure leaves no half-built bridge attached",
              keyFailureResolved && noKeys.attachedBridge == nil && broken.attachedCount == 0)
    }

    @MainActor
    static func testSignOutInvalidatesInFlightIdentity() async {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let recorder = AttachRecorder()
        let gate = LifecycleReadGate()
        let machine = CloudMachineIdentity(accountID: "acct", machineID: "mac-1")
        let signing = CloudDeviceKeyPair()
        guard let master = try? CloudMasterSecret() else {
            check("sign-out invalidation fixture builds", false)
            return
        }
        let reader = CloudKeychainReader<CloudBridgeLifecycle.RestoredIdentity?> {
            gate.wait()
            return CloudBridgeLifecycle.RestoredIdentity(
                machine: machine,
                app: CloudAppIdentity(
                    machineID: machine.machineID, deviceID: machine.machineID,
                    keyID: CloudBridgeLifecycle.masterKeyID,
                    masterSecret: master, signingKey: signing))
        }
        let lifecycle = CloudBridgeLifecycle(services: CloudBridgeLifecycle.Services(
            identityReader: reader,
            makeTransport: { _, _, _ in LifecycleTestTransport() },
            sequencing: { _ in LifecycleTestSequence(
                url: directory.appendingPathComponent("cloud-sequence.json")) },
            attach: { recorder.attach($0) },
            allowCloudCommands: { false },
            commandRouter: { LifecycleTestRouter() },
            commandResult: { _ in },
            log: { _ in }))

        lifecycle.apply()
        let started = gate.awaitEntry()
        lifecycle.signedOut()
        gate.release.signal()
        let returned = await eventually { gate.hasFinished() }
        try? await Task.sleep(nanoseconds: 20_000_000)
        check("sign-out invalidates an identity already returning",
              started && returned && recorder.attachedCount == 0
                  && lifecycle.attachedBridge == nil && lifecycle.state == .detached)
    }

    @MainActor
    static func testIdentityReadTimeoutRetainsTerminalReconciliation() async {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let recorder = AttachRecorder()
        let gate = LifecycleReadGate()
        let machine = CloudMachineIdentity(accountID: "acct", machineID: "mac-timeout")
        let signing = CloudDeviceKeyPair()
        guard let master = try? CloudMasterSecret() else {
            check("identity-timeout fixture builds", false)
            return
        }
        let reader = CloudKeychainReader<CloudBridgeLifecycle.RestoredIdentity?>(
            timeoutSeconds: 1
        ) {
            gate.wait()
            return CloudBridgeLifecycle.RestoredIdentity(
                machine: machine,
                app: CloudAppIdentity(
                    machineID: machine.machineID, deviceID: machine.machineID,
                    keyID: CloudBridgeLifecycle.masterKeyID,
                    masterSecret: master, signingKey: signing))
        }
        let lifecycle = CloudBridgeLifecycle(services: CloudBridgeLifecycle.Services(
            identityReader: reader,
            makeTransport: { _, _, _ in LifecycleTestTransport() },
            sequencing: { _ in LifecycleTestSequence(
                url: directory.appendingPathComponent("cloud-sequence.json")) },
            attach: { recorder.attach($0) },
            allowCloudCommands: { false },
            commandRouter: { LifecycleTestRouter() },
            commandResult: { _ in },
            log: { _ in }))

        lifecycle.apply()
        _ = gate.awaitEntry()
        for _ in 0..<1_000 where lifecycle.identityReadTimeoutSeconds == nil {
            try? await Task.sleep(nanoseconds: 2_000_000)
        }
        check("bridge read timeout remains observable unknown progress",
              lifecycle.identityReadTimeoutSeconds == 1
                  && lifecycle.identityKnowledge == .reading && recorder.attachedCount == 0)
        gate.release.signal()
        let attached = await eventually { recorder.attachedCount == 1 }
        check("bridge read retains late terminal identity and reconciles attachment",
              attached && lifecycle.identityReadTimeoutSeconds == nil
                  && lifecycle.state == .attached(accountID: "acct", machineID: "mac-timeout"))

        let timeoutAfterSignOutRecorder = AttachRecorder()
        let timeoutAfterSignOutGate = LifecycleReadGate()
        let timeoutAfterSignOutReader = CloudKeychainReader<CloudBridgeLifecycle.RestoredIdentity?>(
            timeoutSeconds: 1
        ) {
            timeoutAfterSignOutGate.wait()
            return CloudBridgeLifecycle.RestoredIdentity(
                machine: machine,
                app: CloudAppIdentity(
                    machineID: machine.machineID, deviceID: machine.machineID,
                    keyID: CloudBridgeLifecycle.masterKeyID,
                    masterSecret: master, signingKey: signing))
        }
        let timeoutAfterSignOut = CloudBridgeLifecycle(services: .init(
            identityReader: timeoutAfterSignOutReader,
            makeTransport: { _, _, _ in LifecycleTestTransport() },
            sequencing: { _ in LifecycleTestSequence(
                url: directory.appendingPathComponent("cloud-timeout-after-signout.json")) },
            attach: { timeoutAfterSignOutRecorder.attach($0) },
            allowCloudCommands: { false },
            commandRouter: { LifecycleTestRouter() },
            commandResult: { _ in },
            log: { _ in }))
        timeoutAfterSignOut.apply()
        _ = timeoutAfterSignOutGate.awaitEntry()
        timeoutAfterSignOut.signedOut()
        try? await Task.sleep(nanoseconds: 1_100_000_000)
        check("an invalidated read cannot publish its timeout after signedOut",
              timeoutAfterSignOut.identityReadTimeoutSeconds == nil
                  && timeoutAfterSignOut.identityKnowledge == .resolved)
        timeoutAfterSignOutGate.release.signal()
        try? await Task.sleep(nanoseconds: 20_000_000)
        check("the same invalidated read's late terminal remains detached",
              timeoutAfterSignOutRecorder.attachedCount == 0
                  && timeoutAfterSignOut.state == .detached)

        let signOutAfterTimeoutRecorder = AttachRecorder()
        let signOutAfterTimeoutGate = LifecycleReadGate()
        let signOutAfterTimeoutReader = CloudKeychainReader<CloudBridgeLifecycle.RestoredIdentity?>(
            timeoutSeconds: 1
        ) {
            signOutAfterTimeoutGate.wait()
            return CloudBridgeLifecycle.RestoredIdentity(
                machine: machine,
                app: CloudAppIdentity(
                    machineID: machine.machineID, deviceID: machine.machineID,
                    keyID: CloudBridgeLifecycle.masterKeyID,
                    masterSecret: master, signingKey: signing))
        }
        let signOutAfterTimeout = CloudBridgeLifecycle(services: .init(
            identityReader: signOutAfterTimeoutReader,
            makeTransport: { _, _, _ in LifecycleTestTransport() },
            sequencing: { _ in LifecycleTestSequence(
                url: directory.appendingPathComponent("cloud-signout-after-timeout.json")) },
            attach: { signOutAfterTimeoutRecorder.attach($0) },
            allowCloudCommands: { false },
            commandRouter: { LifecycleTestRouter() },
            commandResult: { _ in },
            log: { _ in }))
        signOutAfterTimeout.apply()
        _ = signOutAfterTimeoutGate.awaitEntry()
        for _ in 0..<1_000 where signOutAfterTimeout.identityReadTimeoutSeconds == nil {
            try? await Task.sleep(nanoseconds: 2_000_000)
        }
        check("the current read publishes timeout progress before signedOut",
              signOutAfterTimeout.identityReadTimeoutSeconds == 1
                  && signOutAfterTimeout.identityKnowledge == .reading)
        signOutAfterTimeout.signedOut()
        check("signedOut clears an already-published timeout and resolves identity knowledge",
              signOutAfterTimeout.identityReadTimeoutSeconds == nil
                  && signOutAfterTimeout.identityKnowledge == .resolved)
        signOutAfterTimeoutGate.release.signal()
        try? await Task.sleep(nanoseconds: 20_000_000)
        check("late terminal after a cleared timeout cannot reattach",
              signOutAfterTimeoutRecorder.attachedCount == 0
                  && signOutAfterTimeout.state == .detached)
    }

    @MainActor
    static func testRevocationStopsReconnecting() async {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let recorder = AttachRecorder()
        let transport = LifecycleTestTransport()
        let identity = CloudMachineIdentity(accountID: "acct", machineID: "mac-1")
        var refusal: (@Sendable (CloudTransportError) -> Void)?
        let signing = CloudDeviceKeyPair()
        let master = try? CloudMasterSecret()
        let identityReader = CloudKeychainReader<CloudBridgeLifecycle.RestoredIdentity?> {
            guard let master else { throw ForcedLifecycleFailure() }
            return CloudBridgeLifecycle.RestoredIdentity(
                machine: identity,
                app: CloudAppIdentity(
                    machineID: identity.machineID, deviceID: identity.machineID,
                    keyID: CloudBridgeLifecycle.masterKeyID,
                    masterSecret: master, signingKey: signing))
        }
        let lifecycle = CloudBridgeLifecycle(services: CloudBridgeLifecycle.Services(
            identityReader: identityReader,
            makeTransport: { _, _, onTerminalFailure in
                refusal = onTerminalFailure
                return transport
            },
            sequencing: { _ in LifecycleTestSequence(
                url: directory.appendingPathComponent("cloud-sequence.json")) },
            attach: { recorder.attach($0) },
            allowCloudCommands: { false },
            commandRouter: { LifecycleTestRouter() },
            commandResult: { _ in },
            log: { _ in }))

        lifecycle.apply()
        let initiallyAttached = await eventually { recorder.attachedCount == 1 }
        check("the revocation case starts from an attached bridge", initiallyAttached)
        refusal?(.unauthorized)
        let detached = await eventually { lifecycle.attachedBridge == nil }
        check("a refused device token brings the bridge down", detached)
        check("the refusal is remembered rather than reported as a generic failure",
              lifecycle.state == .unauthorized(accountID: "acct", machineID: "mac-1"))

        lifecycle.apply()
        let refusalRefreshResolved = await eventually {
            lifecycle.identityKnowledge == .resolved
        }
        check("applying again does not walk back into a refusal loop",
              refusalRefreshResolved && recorder.attachedCount == 1,
              "\(recorder.attaches.count)")

        lifecycle.retry()
        let retried = await eventually { recorder.attachedCount == 2 }
        check("an explicit retry is allowed to try once more", retried)

        // A refusal belonging to a bridge that has already been replaced must not take the
        // current one down; that is the shape the RemoteServer lifecycle guards against too.
        let stale = refusal
        lifecycle.signedOut()
        lifecycle.retry()
        let replacementAttached = await eventually { lifecycle.attachedBridge != nil }
        let before = lifecycle.generation
        stale?(.unauthorized)
        let unchanged = await eventually { lifecycle.generation == before }
        check("a stale refusal does not detach the bridge that replaced it",
              replacementAttached && unchanged && lifecycle.attachedBridge != nil)
    }

    @MainActor
    static func testWriteGateAndCommandSeam() async {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let recorder = AttachRecorder()
        recorder.startAttached = true
        let transport = LifecycleTestTransport()
        let router = LifecycleTestRouter()
        let results = LifecycleTestResults()
        let writes = LifecycleWriteGate()
        let lifecycle = makeLifecycle(
            identity: { CloudMachineIdentity(accountID: "acct", machineID: "mac-1") },
            recorder: recorder, transports: transport, router: router, results: results,
            allowCommands: { writes.isOn() }, sequenceDirectory: directory)

        lifecycle.apply()
        let running = await eventually {
            guard let bridge = lifecycle.attachedBridge else { return false }
            return await bridge.isRunning()
        }
        check("the attached bridge starts its transport", running)
        check("starting the bridge connects exactly once", transport.connects() == 1)

        func command(_ body: [String: Any], channel: String = "ctl/mac-1",
                     sequence: UInt64) -> CloudInboundCommand {
            CloudInboundCommand(
                channel: channel, sequence: sequence, timestamp: 1,
                commandClass: .ctl, sender: "viewer-1",
                plaintext: (try? JSONSerialization.data(withJSONObject: body)) ?? Data())
        }

        transport.deliver(command(
            ["type": "send", "session": "s1", "text": "hello", "images": []], sequence: 1))
        let refusedWhileOff = await eventually { results.all().count == 1 }
        check("a cloud command is refused while sending is switched off", refusedWhileOff)
        check("the refusal names the write gate rather than a transport error",
              results.all().last?.code == "cloud_commands_disabled"
                  && results.all().last?.status == 403,
              "\(String(describing: results.all().last))")
        let routedWhileOff = await router.count()
        check("nothing reached the command router while sending was off", routedWhileOff == 0)

        writes.turnOn()
        transport.deliver(command(
            ["type": "send", "session": "s1", "text": "hello", "images": []], sequence: 2))
        let routed = await eventually { await router.count() == 1 }
        check("an allowed command reaches the verified command seam", routed)
        let key = await router.lastIdempotencyKey()
        check("the command carries a sender-and-sequence idempotency key",
              key == "cloud:viewer-1:2", key ?? "nil")

        transport.deliver(command(
            ["type": "send", "session": "s1", "text": "hello", "images": []],
            channel: "ctl/another-mac", sequence: 3))
        let wrongMachine = await eventually {
            results.all().contains { $0.code == "wrong_machine" }
        }
        check("a command addressed to another Mac is refused here too", wrongMachine)

        // Several relay reconnects must not become several bridges.
        let generation = lifecycle.generation
        transport.becameReady(1)
        transport.becameReady(2)
        transport.becameReady(3)
        try? await Task.sleep(nanoseconds: 20_000_000)
        check("reconnecting the relay does not build a second bridge",
              lifecycle.generation == generation && recorder.attachedCount == 1)

        lifecycle.signedOut()
        let stopped = await eventually { transport.shutdowns() >= 1 }
        check("signing out shuts the transport down", stopped)
    }

    // MARK: - The Mac's half of pairing, end to end

    static func testReadPublicationTiming() async {
        for mode in ["sent", "failed", "buffered", "cancelled"] {
            let failSend = mode == "failed", cancelled = mode == "cancelled"
            let buffered = mode == "buffered"
            let gate = cancelled ? LifecycleSequenceCancellationGate() : nil
            let clock = LifecyclePublicationClock()
            let transport = LifecycleTestTransport(buffersPublication: buffered, publicationEffect: {
                clock.advance(3_400)
                if failSend { throw ForcedLifecycleFailure() }
            })
            guard let secret = try? CloudMasterSecret() else {
                check("publication timing fixture creates its secret", false); return
            }
            let bridge = CloudAppBridge(transport: transport,
                identity: CloudAppIdentity(machineID: "mac-timing", deviceID: "mac-timing",
                    keyID: "ms-1", masterSecret: secret, signingKey: CloudDeviceKeyPair()),
                sequencing: LifecycleTimedSequence(clock, gate: gate), commandRouter: LifecycleTestRouter(),
                nowMilliseconds: { clock.now() }, diagnostic: { clock.record($0) })
            do { try await bridge.start() } catch {
                check("publication timing fixture starts", false); return
            }
            let body: [String: Any] = ["type": "board", "session": "__clawdline_machine__",
                "request": "timing-request", "project": "private-project", "item": "private-item"]
            transport.deliver(CloudInboundCommand(channel: "ctl/mac-timing", sequence: 1,
                timestamp: 10_000, commandClass: .ctl, sender: "viewer-timing",
                plaintext: (try? JSONSerialization.data(withJSONObject: body)) ?? Data()))
            if let gate {
                let waiting = await eventually { gate.waiting() }
                check("cancellation happens while the exact sequence call is suspended", waiting)
                await bridge.stop()
            }
            let finished = await eventually {
                clock.logs().contains { $0.contains(failSend || cancelled ? "read delivery_failed" : "read delivered") }
            }
            check("publication diagnostic fixture finishes on both success and failure", finished)
            let logs = clock.logs()
            check("Board reads identify their type and queue wait without a payload",
                logs.contains { $0.contains("kind=board") && $0.contains("queue_ms=0") })
            let stages = logs.filter { $0.contains("publication_stage=") }
            let expected = cancelled ? [("task_wait", 0), ("sequence", 1200), ("lifecycle_check", 0)]
                : [("task_wait", 0), ("sequence", 1200), ("seal", 0), ("transport_publish", 3400)]
            for (stage, duration) in expected {
                let outcome = (stage == "transport_publish" && failSend) || stage == "lifecycle_check" ? "failed" : "complete"
                check("\(stage) duration is separately measured (\(mode))",
                    stages.contains { $0.contains("publication_stage=\(stage) ")
                        && $0.contains("duration_ms=\(duration) ") && $0.contains("outcome=\(outcome)") })
            }
            if cancelled {
                check("a completed sequence is not falsely classified as failed on cancellation",
                    !stages.contains { $0.contains("publication_stage=sequence ") && $0.contains("outcome=failed") })
                check("cancellation never reaches sealing or transport publication",
                    !stages.contains { $0.contains("publication_stage=seal ") || $0.contains("publication_stage=transport_publish ") })
            } else {
                check("transport return never claims socket-send completion or relay acknowledgement (\(mode))",
                    stages.contains { $0.contains("publication_stage=transport_publish ")
                        && $0.contains("socket_send=not_observed ack=not_observed") })
                check("buffered returns cannot masquerade as a completed socket send",
                    !stages.contains { $0.contains("publication_stage=transport_send ") })
            }
            let ids = Set(stages.compactMap { $0.split(separator: " ").first { $0.hasPrefix("id=") } })
            check("every publication stage correlates to one opaque trace", !stages.isEmpty && ids.count == 1)
            check("stage diagnostics never expose Project/item or plaintext",
                !logs.joined().contains("private-project") && !logs.joined().contains("private-item"))
            check("instrumentation leaves publication outcome unchanged",
                transport.publishedEnvelopes().count == (failSend || cancelled || buffered ? 0 : 1))
            await bridge.stop()
        }
    }

    static func testPairingCompleter() async {
        guard let fixture = try? makeFixture() else {
            check("the completer fixture builds", false)
            return
        }
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = CloudPairedDeviceStore(
            url: directory.appendingPathComponent("cloud-devices.json"))
        let identity = CloudMachineIdentity(
            accountID: fixture.offer.accountID, machineID: "mac-test-01")
        let signing = CloudDeviceKeyPair()
        guard let master = try? CloudMasterSecret(),
              let fragment = try? CloudHandover.encodeOfferFragment(
                  fixture.offer, nowMilliseconds: fixture.now)
        else {
            check("the completer fixture has keys and a fragment", false)
            return
        }
        let delivered = LifecycleDeliveryRecorder()

        func completer(
            identity: CloudMachineIdentity?,
            echo: String? = nil
        ) -> CloudPairingCompleter {
            CloudPairingCompleter(
                restoredIdentity: { identity },
                deviceKeyPair: { signing },
                masterSecret: { master },
                deliver: { pairingID, blob in
                    delivered.record(pairingID: pairingID, blob: blob)
                    return CloudPairingDelivery(
                        fingerprint: echo ?? fixture.offer.viewerFingerprint)
                },
                pin: { device, account in try store.pin(device, accountID: account) },
                nowMilliseconds: { fixture.now },
                // Deterministic here on purpose: the ephemeral key and nonce are the two values
                // the browser has to be able to reproduce nothing about, and fixing them is what
                // lets this test open the exact bytes that went to the control plane.
                randomBytes: { count in
                    count == 12
                        ? Data(repeating: 0, count: 11) + Data([0x2a])
                        : fixture.machineEphemeralPrivate
                })
        }

        let outcome = try? await completer(identity: identity).complete(offerFragment: fragment)
        check("the Mac completes a pairing from the code a person carried", outcome != nil)
        check("the outcome names the viewer it paired",
              outcome?.viewerDeviceID == fixture.offer.viewerDeviceID)
        check("and both fingerprints a person compares",
              outcome?.viewerFingerprint == fixture.offer.viewerFingerprint
                  && outcome?.machineFingerprint == (try? CloudPairing.ed25519Fingerprint(
                      publicKeyRaw: signing.publicKeyRaw)))
        check("the pairing id the blob was delivered under is the offer's",
              delivered.lastPairingID() == fixture.offer.pairingID)
        let pinned = (try? store.devices(accountID: identity.accountID)) ?? []
        check("the viewer's signing key is pinned locally, not read back from the cloud",
              pinned.count == 1 && pinned.first?.deviceID == fixture.offer.viewerDeviceID)

        // The bytes that actually went to `POST /v1/pairing/complete`, opened the way the
        // browser opens them. This is the seam a comment cannot check.
        let wireWrapper = delivered.lastBlobBytes().flatMap {
            try? CloudPairing.decodeWrapper($0)
        }
        check("what was delivered is a decodable pairing wrapper", wireWrapper != nil)
        if let wireWrapper {
            let opened = try? CloudHandover.open(
                wireWrapper, for: fixture.offer,
                viewerEphemeralPrivateKey: fixture.viewerEphemeralPrivate,
                senderDeviceID: identity.machineID, nowMilliseconds: fixture.now)
            check("the delivered blob opens to this account's key material",
                  opened?.accountID == identity.accountID
                      && opened?.masterSecret == master.rawRepresentation.base64EncodedString())
        }

        var notSignedIn = false
        do { _ = try await completer(identity: nil).complete(offerFragment: fragment) }
        catch { notSignedIn = (error as? CloudPairingCompleter.Failure) == .notSignedIn }
        check("a Mac that is not signed in refuses to pair anything", notSignedIn)

        var wrongAccount = false
        let other = CloudMachineIdentity(accountID: "somebody-else", machineID: "mac-test-01")
        do { _ = try await completer(identity: other).complete(offerFragment: fragment) }
        catch { wrongAccount = (error as? CloudPairingCompleter.Failure) == .wrongAccount }
        check("a code from another account is refused before anything is sealed", wrongAccount)

        try? store.forget(deviceID: fixture.offer.viewerDeviceID,
                          accountID: identity.accountID)
        var notEchoed = false
        do {
            _ = try await completer(identity: identity, echo: "AAAA-BBBB-CCCC-DDDD")
                .complete(offerFragment: fragment)
        } catch {
            notEchoed = (error as? CloudPairingCompleter.Failure) == .fingerprintNotEchoed
        }
        check("a fingerprint the service does not echo back is refused", notEchoed)
        check("and nothing is pinned when the delivery is not trusted",
              ((try? store.devices(accountID: identity.accountID)) ?? []).isEmpty)

        // Follow the actual Settings dependency factory into CloudKeys.loadOrCreate. A
        // synchronous store that never answers must return control to the pairing task on both
        // timeout and cancellation; running it off-main by itself would not prove either claim.
        func productionCompleter(
            keyStore: LifecycleBlockingKeyStore,
            timeout: Int
        ) throws -> CloudPairingCompleter {
            let credentialStore = CloudInMemoryKeyStore()
            try credentialStore.set(
                JSONEncoder().encode(CloudMachineCredential(
                    accountID: fixture.offer.accountID,
                    machineID: "mac-test-01",
                    secret: "fixture-machine-secret")),
                for: CloudAccountClient.machineCredentialAccount)
            let client = CloudAccountClient(
                apiBaseURL: URL(string: "https://api.example.test")!,
                transport: LifecycleUnusedHTTPTransport(),
                credentialStore: credentialStore,
                deviceKeyLoader: { signing })
            return CloudPairingCompleter.production(
                client: client,
                keys: CloudKeys(store: keyStore),
                identityAuthority: CloudExecutorIdentityAuthority(
                    store: LifecycleEmptySecretStore()),
                keychainTimeoutSeconds: timeout,
                nowMilliseconds: { fixture.now })
        }

        let timeoutStore = LifecycleBlockingKeyStore()
        let timeoutCompleter = try? productionCompleter(keyStore: timeoutStore, timeout: 1)
        let timeoutTask = Task {
            try await timeoutCompleter?.complete(offerFragment: fragment)
        }
        check("production pairing reaches the hanging load-or-create Keychain seam",
              timeoutStore.awaitEntry())
        var pairingTimedOut = false
        do { _ = try await timeoutTask.value }
        catch {
            pairingTimedOut = (error as? CloudPairingCompleter.Failure)
                == .keychainTimedOut(seconds: 1)
        }
        timeoutStore.release()
        check("a hanging production key load returns a bounded error to the pairing UI",
              pairingTimedOut)

        let cancellationStore = LifecycleBlockingKeyStore()
        let cancellationCompleter = try? productionCompleter(
            keyStore: cancellationStore, timeout: 60)
        let cancellationTask = Task {
            try await cancellationCompleter?.complete(offerFragment: fragment)
        }
        check("the cancellable production pairing reaches the same load-or-create seam",
              cancellationStore.awaitEntry())
        cancellationTask.cancel()
        var pairingCancelled = false
        do { _ = try await cancellationTask.value }
        catch is CancellationError { pairingCancelled = true }
        catch {}
        cancellationStore.release()
        check("closing pairing cancels the production await and returns to the UI",
              pairingCancelled)
    }

    @MainActor
    static func testBrowserPairingWorkflow() async {
        guard let fixture = try? makeFixture() else {
            check("the browser workflow fixture builds", false)
            return
        }
        let outcome = CloudPairingCompleter.Outcome(
            viewerDeviceID: fixture.offer.viewerDeviceID,
            viewerFingerprint: fixture.offer.viewerFingerprint,
            deliveredFingerprint: fixture.offer.viewerFingerprint,
            machineFingerprint: "MAC1-MAC2-MAC3-MAC4")

        let bounds = LifecycleBrowserPairingRecorder(offer: fixture.offer, outcome: outcome)
        let bounded = CloudBrowserPairingWorkflow(services: .init(
            nowMilliseconds: { fixture.now },
            decode: { fragment, now in try bounds.decode(fragment: fragment, now: now) },
            complete: { fragment in try await bounds.complete(fragment: fragment) }))

        var emptyRefused = false
        do { _ = try bounded.preview(raw: " \n ") }
        catch {
            emptyRefused = (error as? CloudBrowserPairingWorkflow.InputFailure) == .emptyOffer
        }
        check("an empty browser offer is refused before decoding", emptyRefused)

        let below = String(repeating: "a", count:
            CloudBrowserPairingWorkflow.maxOfferFragmentUTF8Bytes - 1)
        let at = String(repeating: "b", count:
            CloudBrowserPairingWorkflow.maxOfferFragmentUTF8Bytes)
        _ = try? bounded.preview(raw: below)
        _ = try? bounded.preview(raw: at)
        let decodeCountAtBoundary = bounds.decodeCount()
        var aboveRefused = false
        do { _ = try bounded.preview(raw: at + "c") }
        catch {
            aboveRefused = (error as? CloudBrowserPairingWorkflow.InputFailure)
                == .offerTooLarge(maxBytes:
                    CloudBrowserPairingWorkflow.maxOfferFragmentUTF8Bytes)
        }
        check("browser offers below and at the byte ceiling reach the decoder",
              decodeCountAtBoundary == 2)
        check("a browser offer one byte above the ceiling is refused", aboveRefused)
        check("an oversized browser offer is refused before decoding",
              bounds.decodeCount() == decodeCountAtBoundary)

        let consent = LifecycleBrowserPairingRecorder(offer: fixture.offer, outcome: outcome)
        let workflow = CloudBrowserPairingWorkflow(services: .init(
            nowMilliseconds: { fixture.now },
            decode: { fragment, now in try consent.decode(fragment: fragment, now: now) },
            complete: { fragment in try await consent.complete(fragment: fragment) }))
        let firstPreview = try? workflow.preview(raw: "  carried-offer  ")
        check("the preview exposes the decoded fingerprint, not raw Cloud text",
              firstPreview?.viewerFingerprint == fixture.offer.viewerFingerprint
                  && firstPreview?.fragment == "carried-offer")
        workflow.cancel()
        await Task.yield()
        check("cancelling before consent never calls the completer", consent.completeCount() == 0)

        let confirmed = try? workflow.preview(raw: "carried-offer")
        if let confirmed {
            workflow.confirm(confirmed)
            workflow.confirm(confirmed)
        }
        let completedOnce = await eventually {
            if case .succeeded = workflow.phase { return consent.completeCount() == 1 }
            return false
        }
        check("explicit confirmation completes exactly the previewed offer once", completedOnce)

        let delayed = LifecycleBrowserPairingRecorder(
            offer: fixture.offer, outcome: outcome, suspendCompletion: true)
        let cancellable = CloudBrowserPairingWorkflow(services: .init(
            nowMilliseconds: { fixture.now },
            decode: { fragment, now in try delayed.decode(fragment: fragment, now: now) },
            complete: { fragment in try await delayed.complete(fragment: fragment) }))
        if let preview = try? cancellable.preview(raw: "delayed-offer") {
            cancellable.confirm(preview)
        }
        let delayedReached = await eventually { delayed.completeCount() == 1 }
        check("the confirmed browser handover reaches its owned async operation", delayedReached)
        cancellable.cancel()
        delayed.release()
        try? await Task.sleep(nanoseconds: 20_000_000)
        check("a late completion cannot replace a cancelled browser attempt",
              cancellable.phase == .cancelled)

        let teardown = LifecycleBrowserPairingRecorder(
            offer: fixture.offer, outcome: outcome, suspendCompletion: true)
        var owned: CloudBrowserPairingWorkflow? = CloudBrowserPairingWorkflow(services: .init(
            nowMilliseconds: { fixture.now },
            decode: { fragment, now in try teardown.decode(fragment: fragment, now: now) },
            complete: { fragment in try await teardown.complete(fragment: fragment) }))
        weak var released = owned
        if let workflow = owned,
           let preview = try? workflow.preview(raw: "teardown-offer") {
            workflow.confirm(preview)
        }
        let teardownReached = await eventually { teardown.completeCount() == 1 }
        check("the teardown fixture reaches its owned async operation", teardownReached)
        owned = nil
        check("dropping the browser workflow releases its task owner", released == nil)
        teardown.release()
    }

    @MainActor
    static func run(vectorsURL: URL) async throws -> Int {
        await testSequenceFile()
        testPairedDeviceStore()
        testHandover()
        testHandoverVector(vectorsURL: vectorsURL)
        await testSupervisedTokenProvider()
        await testKeyProvider()
        await testAttachmentIsSingular()
        await testMacLifecycleComposesSharedDurability()
        await testNoCredentialAndFailures()
        await testSignOutInvalidatesInFlightIdentity()
        await testIdentityReadTimeoutRetainsTerminalReconciliation()
        await testRevocationStopsReconnecting()
        await testWriteGateAndCommandSeam()
        await testReadPublicationTiming()
        await testPairingCompleter()
        await testBrowserPairingWorkflow()

        guard failures.isEmpty else {
            throw CloudLifecycleTestFailure(failures: failures, checks: checks)
        }
        return checks
    }
}

private final class LifecycleDeliveryRecorder: @unchecked Sendable {
    private let lock = NSLock()
    private var pairingID: String?
    private var blob: CloudOpaquePairingBlob?

    func record(pairingID: String, blob: CloudOpaquePairingBlob) {
        lock.lock()
        self.pairingID = pairingID
        self.blob = blob
        lock.unlock()
    }

    func lastPairingID() -> String? {
        lock.lock()
        defer { lock.unlock() }
        return pairingID
    }

    /// The blob is opaque by design and prints redacted; the tests need its bytes, and asking
    /// for them through the same base64 the wire carries is how they get them without widening
    /// the type's own surface.
    func lastBlobBytes() -> Data? {
        lock.lock()
        defer { lock.unlock() }
        guard let blob else { return nil }
        return Data(base64Encoded: blob.wireBase64ForTesting)
    }
}

private final class LifecycleBrowserPairingRecorder: @unchecked Sendable {
    private let lock = NSLock()
    private let offer: CloudPairingOffer
    private let outcome: CloudPairingCompleter.Outcome
    private let suspendCompletion: Bool
    private var decoded: [String] = []
    private var completed: [String] = []
    private var continuation: CheckedContinuation<CloudPairingCompleter.Outcome, Error>?

    init(
        offer: CloudPairingOffer,
        outcome: CloudPairingCompleter.Outcome,
        suspendCompletion: Bool = false
    ) {
        self.offer = offer
        self.outcome = outcome
        self.suspendCompletion = suspendCompletion
    }

    func decode(fragment: String, now: Int64) throws -> CloudPairingOffer {
        lock.lock()
        decoded.append(fragment)
        lock.unlock()
        return offer
    }

    func complete(fragment: String) async throws -> CloudPairingCompleter.Outcome {
        lock.lock()
        completed.append(fragment)
        let shouldSuspend = suspendCompletion
        lock.unlock()
        guard shouldSuspend else { return outcome }
        return try await withCheckedThrowingContinuation { continuation in
            lock.lock()
            self.continuation = continuation
            lock.unlock()
        }
    }

    func decodeCount() -> Int {
        lock.lock()
        defer { lock.unlock() }
        return decoded.count
    }

    func completeCount() -> Int {
        lock.lock()
        defer { lock.unlock() }
        return completed.count
    }

    func release() {
        lock.lock()
        let continuation = self.continuation
        self.continuation = nil
        lock.unlock()
        continuation?.resume(returning: outcome)
    }
}

/// A settable stand-in for `Config.shared.remoteWrite`, read from the bridge actor.
private final class LifecycleWriteGate: @unchecked Sendable {
    private let lock = NSLock()
    private var on = false
    func isOn() -> Bool { lock.lock(); defer { lock.unlock() }; return on }
    func turnOn() { lock.lock(); on = true; lock.unlock() }
}

/// Normally allows every check. Once armed, the reservation check succeeds and the immediately
/// following effect-time check observes revocation, pinning the second authorization read.
private final class LifecycleEffectAuthority: @unchecked Sendable {
    private let lock = NSLock()
    private var remainingAllowedChecks: Int?

    func armRevocationAfterReservation() {
        lock.lock(); remainingAllowedChecks = 1; lock.unlock()
    }

    func current(requiresWriteGate: Bool) -> CloudCommandEffectAuthorization {
        lock.lock(); defer { lock.unlock() }
        guard let remainingAllowedChecks else {
            return CloudCommandEffectAuthorization(
                epochState: .ready, rosterAllowsSender: true, writeGateAllows: true)
        }
        let allow = remainingAllowedChecks > 0
        self.remainingAllowedChecks = max(0, remainingAllowedChecks - 1)
        return CloudCommandEffectAuthorization(
            epochState: .ready, rosterAllowsSender: allow,
            writeGateAllows: !requiresWriteGate || allow)
    }
}

func runCloudLifecycleTests(vectorsURL: URL) async throws -> Int {
    try await CloudLifecycleTests.run(vectorsURL: vectorsURL)
}

func runCloudAppBridgeDurableCompositionTests() async throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudAppBridgeTestFailure(description: message) }
    }

    let directory = FileManager.default.temporaryDirectory.appendingPathComponent(
        "clawdline-bridge-durable-\(UUID().uuidString)", isDirectory: true)
    try FileManager.default.createDirectory(
        at: directory, withIntermediateDirectories: true,
        attributes: [.posixPermissions: 0o700])
    let signingKey = CloudDeviceKeyPair()
    let masterSecret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x61, count: 32))
    let commandChannel = "ctl/durable-mac"
    let transport = CloudAppBridgeTestTransport()
    let router = CloudAppBridgeTestRouter()
    let results = CloudAppBridgeTestResults()
    let effectAuthority = LifecycleEffectAuthority()
    var runtime: CloudDurableRuntime? = try CloudDurableRuntime.open(
        directory: directory, runtime: .mac)
    var bridge: CloudAppBridge? = CloudAppBridge(
        transport: transport,
        identity: CloudAppIdentity(
            machineID: "durable-mac", deviceID: "durable-device", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey),
        sequencing: CloudAppBridgeTestSequence(), allowCloudCommands: { true },
        currentCommandEffectAuthority: { _, requiresWriteGate in
            effectAuthority.current(requiresWriteGate: requiresWriteGate)
        },
        commandRouter: router,
        nowMilliseconds: { UInt64(Date().timeIntervalSince1970 * 1_000) },
        commandResult: { results.append($0) }, durableRuntime: runtime)
    try await bridge!.start()

    let now = UInt64(Date().timeIntervalSince1970 * 1_000)
    let command = #"{"type":"send","session":"plain","request":"durable-1","text":"hello","images":[]}"#
    transport.yield(command, sequence: 1, timestamp: now, channel: commandChannel)
    try await waitForCloudAppBridge("durable command outcome and reply") {
        results.all().count == 1 && transport.envelopes().count == 1
    }
    let durableRow = try await runtime!.ledger.row(for: CloudCommandLedgerKey(
        viewerSender: "viewer", requestID: "durable-1"))
    try require(durableRow?.state == .completed
                    && durableRow?.normalizedOutcome?.status == 200,
                "the normalized command outcome is durable before reply")
    let durableFiles = (FileManager.default.enumerator(
        at: directory, includingPropertiesForKeys: [.isRegularFileKey]))?
        .compactMap { $0 as? URL }
        .compactMap { try? Data(contentsOf: $0) } ?? []
    try require(durableFiles.allSatisfy { $0.range(of: Data("hello".utf8)) == nil },
                "durable ledger and spool files contain no plaintext-equivalent command payload")
    let firstEffectCount = await router.recorded().count
    try require(firstEffectCount == 1,
                "the first durable identity reaches effect exactly once")

    let firstReply = transport.envelopes()[0]
    transport.acknowledge(firstReply)
    try await waitForCloudAppBridge("first durable reply acknowledgement") {
        await runtime!.spool.row(seq: Int64(firstReply.seq))?.state == .acked
    }
    transport.yield(command, sequence: 2, timestamp: now, channel: commandChannel)
    try await waitForCloudAppBridge("durable duplicate replay") {
        results.all().count == 2 && transport.envelopes().count == 2
    }
    let duplicateEffectCount = await router.recorded().count
    try require(duplicateEffectCount == 1,
                "an exact duplicate replays without repeating the effect")

    let secondReply = transport.envelopes()[1]
    transport.acknowledge(secondReply)
    try await waitForCloudAppBridge("second durable reply acknowledgement") {
        await runtime!.spool.row(seq: Int64(secondReply.seq))?.state == .acked
    }
    let conflict = #"{"type":"send","session":"plain","request":"durable-1","text":"changed","images":[]}"#
    transport.yield(conflict, sequence: 3, timestamp: now, channel: commandChannel)
    try await waitForCloudAppBridge("durable digest conflict") {
        results.all().contains { $0.code == "command_idempotency_conflict" }
    }
    let conflictEffectCount = await router.recorded().count
    try require(conflictEffectCount == 1,
                "a changed digest fails before another effect")

    effectAuthority.armRevocationAfterReservation()
    let revoked = #"{"type":"send","session":"plain","request":"durable-revoked","text":"no-effect","images":[]}"#
    transport.yield(revoked, sequence: 4, timestamp: now, channel: commandChannel)
    try await waitForCloudAppBridge("effect-time authority revocation") {
        results.all().contains { $0.code == "unknown_sender" }
    }
    let revokedEffectCount = await router.recorded().count
    let revokedRow = try await runtime!.ledger.row(for: CloudCommandLedgerKey(
        viewerSender: "viewer", requestID: "durable-revoked"))
    try require(revokedEffectCount == 1 && revokedRow == nil,
                "effect-time roster/write revocation removes reserved state before any effect")

    await bridge!.stop()
    bridge = nil
    runtime = nil

    let deadlineDirectory = FileManager.default.temporaryDirectory.appendingPathComponent(
        "clawdline-bridge-deadline-\(UUID().uuidString)", isDirectory: true)
    try FileManager.default.createDirectory(
        at: deadlineDirectory, withIntermediateDirectories: true,
        attributes: [.posixPermissions: 0o700])
    defer { try? FileManager.default.removeItem(at: deadlineDirectory) }
    var deadlineLimits = CloudSpoolLimits()
    deadlineLimits.attemptWindow = .milliseconds(20)
    let deadlineRuntime = try CloudDurableRuntime.open(
        directory: deadlineDirectory, runtime: .mac, limits: deadlineLimits)
    let deadlineTransport = CloudAppBridgeTestTransport(suspendPublication: true)
    let deadlineComposition = CloudDurableOutboundComposition(
        spool: deadlineRuntime.spool, transport: deadlineTransport,
        identity: CloudAppIdentity(
            machineID: "durable-mac", deviceID: "durable-device", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey),
        nowMilliseconds: { now })
    try await deadlineComposition.enqueue(
        Data("deadline-payload".utf8), channel: "s/account/viewer", logicalID: "deadline")
    try await waitForCloudAppBridge("autonomous attempt deadline wake") {
        await deadlineRuntime.spool.row(seq: 0)?.state == .burned
    }
    let deadlineRow = await deadlineRuntime.spool.row(seq: 0)
    try require(deadlineRow?.burnReason == .attemptCapExpired,
                "a socket-stalled sent head burns at its attempt deadline without another outbound event")
    await deadlineComposition.stop()

    let retryDirectory = FileManager.default.temporaryDirectory.appendingPathComponent(
        "clawdline-bridge-deadline-retry-\(UUID().uuidString)", isDirectory: true)
    try FileManager.default.createDirectory(
        at: retryDirectory, withIntermediateDirectories: true,
        attributes: [.posixPermissions: 0o700])
    defer { try? FileManager.default.removeItem(at: retryDirectory) }
    var retryLimits = CloudSpoolLimits()
    retryLimits.attemptWindow = .milliseconds(20)
    let retryStore = try CloudFileSpoolStore(url: retryDirectory.appendingPathComponent("spool.json"))
    let retrySpool = try CloudOutboundSpool(
        store: retryStore, clock: CloudSystemSpoolClock(), metrics: CloudNoopSpoolMetrics(),
        runtime: .mac, limits: retryLimits)
    let retryTransport = CloudAppBridgeTestTransport()
    let retryComposition = CloudDurableOutboundComposition(
        spool: retrySpool, transport: retryTransport,
        identity: CloudAppIdentity(
            machineID: "durable-mac", deviceID: "durable-device", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey),
        nowMilliseconds: { now }, deadlineFailureRetryDelay: .milliseconds(20))
    try await retryComposition.enqueue(
        Data("deadline-retry".utf8), channel: "s/account/viewer", logicalID: "deadline-retry")
    let retryFault = LifecycleOneShotDurableFault()
    retryStore.faultInjection = { try retryFault.failFirstPersist($0) }
    try await waitForCloudAppBridge("deadline wake retries after one persistence failure") {
        await retrySpool.row(seq: 0)?.state == .burned
    }
    try require(retryFault.didFire,
                "the retry fixture reaches the injected deadline persistence failure")
    await retryComposition.stop()

    checks += try await runCloudOutboundCorrectionTests()

    let stalledDirectory = FileManager.default.temporaryDirectory.appendingPathComponent(
        "clawdline-bridge-stalled-drain-\(UUID().uuidString)", isDirectory: true)
    try FileManager.default.createDirectory(
        at: stalledDirectory, withIntermediateDirectories: true,
        attributes: [.posixPermissions: 0o700])
    defer { try? FileManager.default.removeItem(at: stalledDirectory) }
    let stalledRuntime = try CloudDurableRuntime.open(
        directory: stalledDirectory, runtime: .mac)
    let stalledTransport = CloudAppBridgeTestTransport(suspendPublication: true)
    let stalledComposition = CloudDurableOutboundComposition(
        spool: stalledRuntime.spool, transport: stalledTransport,
        identity: CloudAppIdentity(
            machineID: "durable-mac", deviceID: "durable-device", keyID: "ms-1",
            masterSecret: masterSecret, signingKey: signingKey),
        nowMilliseconds: { now })
    try await stalledComposition.enqueue(
        Data("stalled-first".utf8), channel: "s/account/viewer", logicalID: "stalled-0")
    try await waitForCloudAppBridge("independent drain reaches stalled socket") {
        stalledTransport.state().publicationStarts == 1
    }
    for index in 1...3 {
        try await stalledComposition.enqueue(
            Data("stalled-\(index)".utf8), channel: "s/account/viewer",
            logicalID: "stalled-\(index)")
    }
    let stalledMetrics = await stalledComposition.metricsSnapshot()
    try require(stalledMetrics.currentRows == 1
                    && stalledMetrics.readyWaitingRows == 1
                    && stalledTransport.state().publicationStarts == 1,
                "latest-value enqueue coalesces backlog behind one independently owned stalled writer")
    await stalledComposition.stop()
    try require(stalledTransport.state().publicationCancelled,
                "stop cancels and joins the independently owned drain worker")

    let windowDirectory = FileManager.default.temporaryDirectory.appendingPathComponent(
        "clawdline-bridge-window-\(UUID().uuidString)", isDirectory: true)
    try FileManager.default.createDirectory(at: windowDirectory, withIntermediateDirectories: true,
                                            attributes: [.posixPermissions: 0o700])
    defer { try? FileManager.default.removeItem(at: windowDirectory) }
    var windowLimits = CloudSpoolLimits(); windowLimits.outboundWindowRowCap = 2
    let windowRuntime = try CloudDurableRuntime.open(
        directory: windowDirectory, runtime: .mac, limits: windowLimits)
    let windowTransport = CloudAppBridgeTestTransport()
    let windowComposition = CloudDurableOutboundComposition(
        spool: windowRuntime.spool, transport: windowTransport,
        identity: CloudAppIdentity(machineID: "durable-mac", deviceID: "durable-device",
            keyID: "ms-1", masterSecret: masterSecret, signingKey: signingKey),
        nowMilliseconds: { now })
    for index in 0..<3 {
        try await windowComposition.enqueue(Data("window-\(index)".utf8),
            channel: index == 1 ? "t/account/viewer" : "s/account/viewer", logicalID: "window-\(index)")
    }
    try await waitForCloudAppBridge("bounded outbound window fills without ACK") {
        windowTransport.envelopes().count == 2
    }
    let fullWindow = await windowComposition.metricsSnapshot()
    try require(fullWindow.currentRows == 2 && fullWindow.readyWaitingRows == 1
                    && fullWindow.processPeakRows == 2
                    && fullWindow.windowAdmissionRefusalAttempts >= 1
                    && fullWindow.currentBytes <= fullWindow.maximumBytes
                    && fullWindow.processPeakBytes <= fullWindow.maximumBytes,
                "the durable send window exposes current peak wait and refusal at its row bound")
    let secondWindowFrame = windowTransport.envelopes()[1]
    try await windowComposition.settle(CloudOutboundTransportReceipt(
        channel: secondWindowFrame.ch, sequence: Int64(secondWindowFrame.seq), kind: .delivered))
    try await waitForCloudAppBridge("out-of-order receipt replenishes one window slot") {
        windowTransport.envelopes().count == 3
    }
    let olderWindowState = await windowRuntime.spool.row(seq: 0)?.state
    try require(olderWindowState == .sent,
                "out-of-order settlement does not alter the older sent row")
    await windowComposition.requestDrain(reconnect: true)
    try await waitForCloudAppBridge("production reconnect replays the sent snapshot once") {
        windowTransport.envelopes().count == 5
    }
    let replayed = windowTransport.envelopes().suffix(2).map(\.seq)
    try require(Array(replayed) == [0, 2],
                "production reconnect snapshots sent rows once and replays them in sequence order")

    let firstWindowFrame = windowTransport.envelopes()[0]
    try await windowComposition.settle(CloudOutboundTransportReceipt(
        channel: firstWindowFrame.ch, sequence: Int64(firstWindowFrame.seq),
        kind: .peerRejected(CloudOutboundPeerRejection(
            code: .unknown, field: .unknown, disposition: .terminal))))
    await windowComposition.requestDrain(reconnect: true)
    try await waitForCloudAppBridge("terminally rejected bytes never replay") {
        windowTransport.envelopes().count == 6
    }
    let terminallyRejectedState = await windowRuntime.spool.row(seq: 0)?.state
    try require(windowTransport.envelopes().last?.seq == 2
                    && terminallyRejectedState == .rejected,
                "a definitive refusal settles only its correlated row and excludes it from replay")

    let retrySourceFrame = windowTransport.envelopes()[2]
    try await windowComposition.settle(CloudOutboundTransportReceipt(
        channel: retrySourceFrame.ch, sequence: Int64(retrySourceFrame.seq),
        kind: .peerRejected(CloudOutboundPeerRejection(
            code: .clockSkew, field: .timestamp, disposition: .retryNewAttempt))))
    try await waitForCloudAppBridge("retryable refusal creates a fresh attempt") {
        windowTransport.envelopes().contains { $0.seq == 3 }
    }
    let retryFrame = windowTransport.envelopes().first { $0.seq == 3 }
    let retrySourceState = await windowRuntime.spool.row(seq: 2)?.state
    try require(retryFrame?.ct != retrySourceFrame.ct && retrySourceState == .rejected,
                "retryable refusal terminally settles old bytes and reseals a new sequence")
    await windowComposition.stop()
    try? FileManager.default.removeItem(at: directory)
    return checks
}
