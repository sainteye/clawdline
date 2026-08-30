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
    let commands: AsyncStream<CloudInboundCommand>
    let readyGenerations: AsyncStream<UInt64>
    private let commandContinuation: AsyncStream<CloudInboundCommand>.Continuation
    private let readyContinuation: AsyncStream<UInt64>.Continuation
    private let lock = NSLock()
    private var connectCount = 0
    private var shutdownCount = 0
    private var published: [CloudEnvelope] = []
    let connectError: Error?

    init(connectError: Error? = nil) {
        self.connectError = connectError
        var commandContinuation: AsyncStream<CloudInboundCommand>.Continuation!
        commands = AsyncStream { commandContinuation = $0 }
        self.commandContinuation = commandContinuation
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
        record(envelope)
    }

    func shutdown() async {
        countShutdown()
        commandContinuation.finish()
        readyContinuation.finish()
    }

    func deliver(_ command: CloudInboundCommand) { commandContinuation.yield(command) }
    func becameReady(_ generation: UInt64) { readyContinuation.yield(generation) }
    func connects() -> Int { lock.lock(); defer { lock.unlock() }; return connectCount }
    func shutdowns() -> Int { lock.lock(); defer { lock.unlock() }; return shutdownCount }
    func publishedEnvelopes() -> [CloudEnvelope] {
        lock.lock(); defer { lock.unlock() }; return published
    }
}

private actor LifecycleTestRouter: CloudCommandRouting {
    private var routed: [(CloudHeadlessCommand, String, String)] = []

    func route(_ command: CloudHeadlessCommand, sender: String,
               idempotencyKey: String) async -> CloudCommandResult {
        routed.append((command, sender, idempotencyKey))
        return CloudCommandResult(status: 200, code: nil)
    }

    func count() -> Int { routed.count }
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

    // MARK: - Durable sequencing

    static func testSequenceFile() async {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("cloud-sequence.json")

        let first = CloudSequenceFile(url: url, block: 4)
        var handed: [UInt64] = []
        for _ in 0..<6 { handed.append((try? await first.nextSequence(sender: "mac")) ?? .max) }
        check("the first sequence a machine hands out is zero", handed.first == 0)
        check("sequences advance by exactly one", handed == [0, 1, 2, 3, 4, 5], "\(handed)")
        let ceiling = (try? await first.reservedCeiling(sender: "mac")) ?? 0
        check("the ceiling written down is ahead of what has been handed out",
              ceiling > handed.last!, "ceiling \(ceiling) last \(handed.last!)")

        // A relaunch is a new instance over the same file, which is the whole point.
        let second = CloudSequenceFile(url: url, block: 4)
        let afterRelaunch = (try? await second.nextSequence(sender: "mac")) ?? .max
        check("a relaunch never repeats a sequence it may already have used",
              afterRelaunch >= ceiling, "\(afterRelaunch) vs ceiling \(ceiling)")
        check("a relaunch does not restart from zero", afterRelaunch != 0)

        let other = (try? await second.nextSequence(sender: "phone")) ?? .max
        check("a second sender keeps its own counter", other == 0, "\(other)")

        try? Data("{ not json".utf8).write(to: url)
        let corrupt = CloudSequenceFile(url: url, block: 4)
        var refused = false
        do { _ = try await corrupt.nextSequence(sender: "mac") } catch { refused = true }
        check("an unreadable sequence file refuses rather than replaying from zero", refused)
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
        identity: @escaping @MainActor () throws -> CloudMachineIdentity?,
        recorder: AttachRecorder,
        transports: LifecycleTestTransport,
        router: LifecycleTestRouter = LifecycleTestRouter(),
        results: LifecycleTestResults = LifecycleTestResults(),
        allowCommands: @escaping @Sendable () -> Bool = { false },
        appIdentityFails: Bool = false,
        sequenceDirectory: URL
    ) -> CloudBridgeLifecycle {
        let signing = CloudDeviceKeyPair()
        let master = try? CloudMasterSecret()
        let sequence = CloudSequenceFile(
            url: sequenceDirectory.appendingPathComponent("cloud-sequence.json"))
        return CloudBridgeLifecycle(services: CloudBridgeLifecycle.Services(
            restoredIdentity: identity,
            appIdentity: { machine in
                if appIdentityFails { throw ForcedLifecycleFailure() }
                guard let master else { throw ForcedLifecycleFailure() }
                return CloudAppIdentity(
                    machineID: machine.machineID, deviceID: machine.machineID,
                    keyID: CloudBridgeLifecycle.masterKeyID,
                    masterSecret: master, signingKey: signing)
            },
            makeTransport: { _, _, _ in transports },
            sequencing: { _ in sequence },
            attach: { recorder.attach($0) },
            allowCloudCommands: allowCommands,
            commandRouter: { router },
            commandResult: { results.record($0) },
            log: { _ in }))
    }

    @MainActor
    static func testAttachmentIsSingular() async {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let recorder = AttachRecorder()
        let transport = LifecycleTestTransport()
        var identity = CloudMachineIdentity(accountID: "acct", machineID: "mac-1")
        let lifecycle = makeLifecycle(
            identity: { identity }, recorder: recorder, transports: transport,
            sequenceDirectory: directory)

        lifecycle.apply()
        check("a restored Mac attaches a bridge without being asked twice",
              recorder.attachedCount == 1, "\(recorder.attaches.count)")
        check("the attached state names the account and machine",
              lifecycle.state == .attached(accountID: "acct", machineID: "mac-1"))
        let generation = lifecycle.generation

        lifecycle.apply()
        lifecycle.apply()
        check("re-applying with the same identity leaves the live bridge alone",
              lifecycle.generation == generation && recorder.attaches.count == 1)

        identity = CloudMachineIdentity(accountID: "acct", machineID: "mac-2")
        lifecycle.apply()
        check("a changed machine identity replaces the bridge",
              lifecycle.state == .attached(accountID: "acct", machineID: "mac-2"))
        check("replacing detaches the old bridge before attaching the new one",
              recorder.attaches.count == 3 && recorder.attaches[1] == nil
                  && recorder.attaches[2] != nil)

        lifecycle.signedOut()
        check("signing out detaches", lifecycle.state == .detached && recorder.detachedCount == 2)
        check("nothing is attached after signing out", lifecycle.attachedBridge == nil)
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
        check("a Mac that is not signed in attaches nothing and does not churn",
              quiet.attaches.isEmpty && lifecycle.state == .detached)

        let throwing = AttachRecorder()
        let unreadable = makeLifecycle(
            identity: { throw ForcedLifecycleFailure() }, recorder: throwing,
            transports: LifecycleTestTransport(), sequenceDirectory: directory)
        unreadable.apply()
        check("an unreadable credential store reports failed rather than attaching",
              unreadable.attachedBridge == nil)
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
        check("a Keychain failure leaves no half-built bridge attached",
              noKeys.attachedBridge == nil && broken.attachedCount == 0)
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
        let lifecycle = CloudBridgeLifecycle(services: CloudBridgeLifecycle.Services(
            restoredIdentity: { identity },
            appIdentity: { machine in
                guard let master else { throw ForcedLifecycleFailure() }
                return CloudAppIdentity(
                    machineID: machine.machineID, deviceID: machine.machineID,
                    keyID: CloudBridgeLifecycle.masterKeyID,
                    masterSecret: master, signingKey: signing)
            },
            makeTransport: { _, _, onTerminalFailure in
                refusal = onTerminalFailure
                return transport
            },
            sequencing: { _ in CloudSequenceFile(
                url: directory.appendingPathComponent("cloud-sequence.json")) },
            attach: { recorder.attach($0) },
            allowCloudCommands: { false },
            commandRouter: { LifecycleTestRouter() },
            commandResult: { _ in },
            log: { _ in }))

        lifecycle.apply()
        check("the revocation case starts from an attached bridge", recorder.attachedCount == 1)
        refusal?(.unauthorized)
        let detached = await eventually { lifecycle.attachedBridge == nil }
        check("a refused device token brings the bridge down", detached)
        check("the refusal is remembered rather than reported as a generic failure",
              lifecycle.state == .unauthorized(accountID: "acct", machineID: "mac-1"))

        lifecycle.apply()
        check("applying again does not walk back into a refusal loop",
              recorder.attachedCount == 1, "\(recorder.attaches.count)")

        lifecycle.retry()
        check("an explicit retry is allowed to try once more", recorder.attachedCount == 2)

        // A refusal belonging to a bridge that has already been replaced must not take the
        // current one down; that is the shape the RemoteServer lifecycle guards against too.
        let stale = refusal
        lifecycle.signedOut()
        lifecycle.retry()
        let before = lifecycle.generation
        stale?(.unauthorized)
        let unchanged = await eventually { lifecycle.generation == before }
        check("a stale refusal does not detach the bridge that replaced it",
              unchanged && lifecycle.attachedBridge != nil)
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
        await testNoCredentialAndFailures()
        await testRevocationStopsReconnecting()
        await testWriteGateAndCommandSeam()
        await testPairingCompleter()

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

/// A settable stand-in for `Config.shared.remoteWrite`, read from the bridge actor.
private final class LifecycleWriteGate: @unchecked Sendable {
    private let lock = NSLock()
    private var on = false
    func isOn() -> Bool { lock.lock(); defer { lock.unlock() }; return on }
    func turnOn() { lock.lock(); on = true; lock.unlock() }
}

func runCloudLifecycleTests(vectorsURL: URL) async throws -> Int {
    try await CloudLifecycleTests.run(vectorsURL: vectorsURL)
}
