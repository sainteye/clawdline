import Foundation

/// Durable outbound sequence numbers for `CloudAppBridge`.
///
/// `CloudEnvelopeSequencing` says the implementation must be durable because "reusing a sender
/// sequence after relaunch is a replay". Both ends enforce that: `CloudSequenceTracker` on the
/// Mac and `sequenceBySender` in `cloud-client.js` refuse an envelope whose `seq` did not
/// advance, so a counter that restarts at zero does not merely repeat itself — it makes this
/// Mac silent on every channel until it has climbed back past whatever the viewer already saw.
///
/// So what is persisted is a **ceiling**, written before the first number under it is handed
/// out. A crash therefore skips the rest of a block; it can never repeat one. The block exists
/// only so that publishing a snapshot is not one `fsync` per envelope.
actor CloudSequenceFile: CloudEnvelopeSequencing {
    enum Failure: Error, LocalizedError, Equatable {
        case unreadable
        case unwritable

        var errorDescription: String? {
            switch self {
            case .unreadable: return "The cloud sequence file could not be read."
            case .unwritable: return "The cloud sequence file could not be written."
            }
        }
    }

    private let url: URL
    private let block: UInt64
    private var reserved: [String: UInt64] = [:]
    private var next: [String: UInt64] = [:]
    private var loaded = false

    init(url: URL, block: UInt64 = 64) {
        self.url = url
        self.block = max(1, block)
    }

    init(block: UInt64 = 64) {
        self.init(url: RemoteAuth.directory.appendingPathComponent("cloud-sequence.json"),
                  block: block)
    }

    func nextSequence(sender: String) async throws -> UInt64 {
        try loadIfNeeded()
        let value = next[sender] ?? reserved[sender] ?? 0
        if value >= (reserved[sender] ?? 0) {
            let ceiling = value &+ block
            reserved[sender] = ceiling
            try persist()
        }
        next[sender] = value &+ 1
        // Read back rather than trusting the local copy: `persist()` is the only thing allowed
        // to have moved the ceiling, and a value handed out above one that was never written is
        // exactly the reuse this type exists to prevent.
        guard let ceiling = reserved[sender], value < ceiling else { throw Failure.unwritable }
        return value
    }

    /// The ceiling currently promised on disk. Tests use it to prove a relaunch cannot reuse.
    func reservedCeiling(sender: String) throws -> UInt64 {
        try loadIfNeeded()
        return reserved[sender] ?? 0
    }

    private func loadIfNeeded() throws {
        guard !loaded else { return }
        guard FileManager.default.fileExists(atPath: url.path) else {
            loaded = true
            return
        }
        guard let data = try? Data(contentsOf: url) else { throw Failure.unreadable }
        guard let value = try? CloudCanonicalJSON.parseStrict(data),
              case .object(let root) = value,
              case .int(let version)? = root["v"], version == 1,
              case .object(let senders)? = root["senders"]
        else {
            // Deliberately not "start again from zero". A file we cannot read is a high-water
            // mark we do not know, and inventing one is the replay. Refusing keeps this Mac
            // quiet on the cloud until somebody looks, which is the recoverable half.
            throw Failure.unreadable
        }
        var restored: [String: UInt64] = [:]
        for (sender, entry) in senders {
            guard case .int(let ceiling) = entry, ceiling >= 0 else { throw Failure.unreadable }
            restored[sender] = UInt64(ceiling)
        }
        reserved = restored
        next = [:]
        loaded = true
    }

    private func persist() throws {
        var senders: [String: CloudJSONValue] = [:]
        for (sender, ceiling) in reserved {
            guard ceiling <= UInt64(Int64.max) else { throw Failure.unwritable }
            senders[sender] = .int(Int64(ceiling))
        }
        let body = CloudCanonicalJSON.canonicalData(.object([
            "v": .int(1), "senders": .object(senders),
        ]))
        do {
            try FileManager.default.createDirectory(
                at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            try body.write(to: url, options: [.atomic])
            try FileManager.default.setAttributes(
                [.posixPermissions: 0o600], ofItemAtPath: url.path)
        } catch {
            throw Failure.unwritable
        }
    }
}

/// The transport's view of this Mac's keys.
///
/// Not `CloudStaticTransportKeys`, and the difference is the third method. A static dictionary
/// freezes the pinned viewer list at the moment the bridge was built, so a viewer unpinned
/// afterwards would keep being accepted until the app restarted. Reading the store on every
/// inbound command is what makes "revocation stops routing, effective immediately" true on this
/// side of the wire as well as on the relay's.
struct CloudLifecycleKeyProvider: CloudTransportKeyProviding, Sendable {
    let deviceKey: CloudDeviceKeyPair
    let masterSecrets: [String: CloudMasterSecret]
    let accountID: String
    let pairedDevices: CloudPairedDeviceStore

    func deviceKeyPair() async throws -> CloudDeviceKeyPair { deviceKey }

    func masterSecret(for keyID: String) async throws -> CloudMasterSecret {
        guard let secret = masterSecrets[keyID] else {
            throw CloudTransportError.unexpectedFrame("unknown-key")
        }
        return secret
    }

    func pairedDevicePublicKeys() async -> [String: Data] {
        guard let devices = try? pairedDevices.devices(accountID: accountID) else { return [:] }
        var keys: [String: Data] = [:]
        for device in devices { keys[device.deviceID] = device.signingKey }
        return keys
    }
}

/// Every device-token fetch, initial and reconnect, passes through here.
///
/// `CloudTransport` reconnects forever with capped backoff and reports nothing after the first
/// `connect()`: read `receiveAndReconnect`, whose inner loop treats every error the same way and
/// only doubles the delay. That is right for a flaky network and wrong for a revoked machine,
/// which would knock on the relay's door every thirty seconds for as long as the app is open. A
/// revoked machine is exactly what `POST /v1/tokens/device` answers 403 to, so the token fetch
/// is the one place that distinction is visible, and it is visible on every attempt rather than
/// only on the first.
struct CloudSupervisedDeviceTokenProvider: CloudDeviceTokenProviding, Sendable {
    let inner: any CloudDeviceTokenProviding
    let onTerminalFailure: @Sendable (CloudTransportError) -> Void

    func fetchDeviceToken() async throws -> CloudDeviceToken {
        do {
            return try await inner.fetchDeviceToken()
        } catch let error as CloudTransportError where error == .unauthorized {
            onTerminalFailure(error)
            throw error
        }
    }
}

/// The small state machine between a main-actor lifecycle request and a potentially blocking
/// credential read. It is deliberately free of Dispatch and Keychain types so the ordering can
/// be compiled and mutation-tested without substituting a second implementation for production.
struct CloudIdentityReadPolicy {
    enum Knowledge: Equatable {
        /// No read has finished yet; callers must not interpret this as a proved sign-out.
        case unknown
        /// A read is running away from the main actor. The bridge keeps its last proved state.
        case reading
        /// The latest requested read has been applied, or sign-out supplied the answer directly.
        case resolved
    }

    enum RequestAction: Equatable {
        case start(generation: UInt64)
        case coalesced
    }

    enum CompletionAction: Equatable {
        case accept
        case restart(generation: UInt64)
        case discard
    }

    struct Tracker: Equatable {
        fileprivate(set) var latestGeneration: UInt64 = 0
        fileprivate(set) var inFlightGeneration: UInt64?
        fileprivate(set) var refreshPending = false
        fileprivate(set) var knowledge: Knowledge = .unknown
    }

    static func request(_ tracker: inout Tracker) -> RequestAction {
        tracker.latestGeneration &+= 1
        tracker.knowledge = .reading
        guard tracker.inFlightGeneration == nil else {
            tracker.refreshPending = true
            return .coalesced
        }
        tracker.inFlightGeneration = tracker.latestGeneration
        return .start(generation: tracker.latestGeneration)
    }

    static func complete(generation: UInt64,
                         tracker: inout Tracker) -> CompletionAction {
        guard tracker.inFlightGeneration == generation else { return .discard }
        tracker.inFlightGeneration = nil
        if tracker.refreshPending {
            tracker.refreshPending = false
            tracker.inFlightGeneration = tracker.latestGeneration
            return .restart(generation: tracker.latestGeneration)
        }
        tracker.knowledge = .resolved
        return generation == tracker.latestGeneration ? .accept : .discard
    }

    /// Sign-out is an authoritative foreground event. It invalidates an answer already on its
    /// way back without pretending the blocking operation itself can be cancelled.
    static func resolveWithoutRead(_ tracker: inout Tracker) {
        tracker.latestGeneration &+= 1
        tracker.refreshPending = false
        tracker.knowledge = .resolved
    }
}

/// The executable concurrency seam for credential I/O. Tests run this exact type with a blocking
/// operation: `read` must return to the main thread immediately, operations must stay serial, and
/// completion must cross back to the main queue before lifecycle state is touched.
final class CloudIdentityReader<Value: Sendable>: @unchecked Sendable {
    private let queue: DispatchQueue

    init(label: String = "clawdline.cloud.identity") {
        queue = DispatchQueue(label: label, qos: .utility)
    }

    func read(
        _ operation: @escaping @Sendable () throws -> Value,
        completion: @escaping @MainActor (Result<Value, Error>) -> Void
    ) {
        queue.async {
            let result = Result { try operation() }
            DispatchQueue.main.async { completion(result) }
        }
    }
}

/// Owns exactly one `CloudAppBridge` for the running app.
///
/// Everything under it already existed and none of it was ever built: `CloudAppBridge` had no
/// constructor call anywhere in `Sources/`, and `RemoteServer.attachCloudBridge` had no caller,
/// so a Mac signed in to Clawdline Cloud published nothing and accepted nothing. This is that
/// missing wire, and its whole job is to be idempotent — `apply()` is called on launch, on every
/// config change and on every Cloud sign-in state change, and only an actual change of identity
/// may replace a live bridge.
@MainActor
final class CloudBridgeLifecycle {
    enum State: Equatable {
        case detached
        case attached(accountID: String, machineID: String)
        /// The control plane refused this machine's device token with 401/403. Reconnecting on a
        /// timer would be a refusal loop, so the bridge comes down and stays down until the
        /// identity changes or somebody asks again.
        case unauthorized(accountID: String, machineID: String)
        case failed(reason: String)
    }

    struct Services {
        var restoredIdentity: @Sendable () throws -> CloudMachineIdentity?
        var appIdentity: @MainActor (CloudMachineIdentity) throws -> CloudAppIdentity
        var makeTransport: @MainActor (
            CloudMachineIdentity, CloudAppIdentity,
            @escaping @Sendable (CloudTransportError) -> Void
        ) throws -> any CloudTransporting
        var sequencing: @MainActor (CloudMachineIdentity) -> any CloudEnvelopeSequencing
        var attach: @MainActor (CloudAppBridge?) -> Void
        var allowCloudCommands: @Sendable () -> Bool
        /// The one door a cloud command enters the app through. Production hands back
        /// `RemoteServerCloudCommandRouter`, so authentication, idempotency, image validation
        /// and audit stay the local HTTP route's single implementation rather than a copy.
        var commandRouter: @MainActor () -> any CloudCommandRouting
        var commandResult: @Sendable (CloudCommandResult) -> Void
        var log: @MainActor (String) -> Void
    }

    /// `nonisolated` because `Services.production()` is not on the main actor and these are
    /// two immutable Sendable values, not state this class owns.
    nonisolated static let masterKeyID = "ms-1"
    nonisolated static let defaultRelayURL = URL(string: "wss://relay.clawdline.com/v1/connect")!

    static let shared = CloudBridgeLifecycle(services: .production())

    private(set) var state: State = .detached
    private(set) var attachedBridge: CloudAppBridge?
    /// Bumped by every attach and every detach. Tests read it to tell "left alone" from
    /// "torn down and rebuilt with the same identity", which `state` alone cannot show.
    private(set) var generation: UInt64 = 0
    var onChange: (() -> Void)?

    private let services: Services
    private let identityReader = CloudIdentityReader<CloudMachineIdentity?>()
    private var identityRead = CloudIdentityReadPolicy.Tracker()

    /// This is separate from `state`: while a refresh is blocked in Keychain, an attached bridge
    /// remains honestly attached, while a launch-time `.detached` is not yet proof of sign-out.
    var identityKnowledge: CloudIdentityReadPolicy.Knowledge { identityRead.knowledge }

    init(services: Services) {
        self.services = services
    }

    /// Bring the bridge into line with what the credential store says. Safe to call whenever
    /// anything might have changed. The Keychain operation never runs on the main actor.
    func apply() {
        let wasReading = identityRead.knowledge == .reading
        switch CloudIdentityReadPolicy.request(&identityRead) {
        case .start(let generation):
            if !wasReading { onChange?() }
            startIdentityRead(generation: generation)
        case .coalesced:
            if !wasReading { onChange?() }
        }
    }

    private func startIdentityRead(generation: UInt64) {
        let restore = services.restoredIdentity
        identityReader.read(restore) { [weak self] result in
            self?.finishedIdentityRead(result, generation: generation)
        }
    }

    private func finishedIdentityRead(
        _ result: Result<CloudMachineIdentity?, Error>, generation: UInt64
    ) {
        let wasReading = identityRead.knowledge == .reading
        switch CloudIdentityReadPolicy.complete(generation: generation, tracker: &identityRead) {
        case .restart(let next):
            startIdentityRead(generation: next)
            return
        case .discard:
            if wasReading && identityRead.knowledge != .reading { onChange?() }
            return
        case .accept:
            break
        }

        if wasReading { onChange?() }
        switch result {
        case .failure(let error):
            detach()
            set(.failed(reason: Self.message(for: error)))
            services.log("cloud: the machine credential could not be read — \(Self.message(for: error))")
            return
        case .success(nil):
            detach()
            set(.detached)
            return
        case .success(let identity?):
            apply(identity: identity)
        }
    }

    private func apply(identity: CloudMachineIdentity) {
        switch state {
        case .attached(let account, let machine)
            where account == identity.accountID && machine == identity.machineID:
            // One lifecycle. Replacing a healthy bridge on an unrelated config change would
            // drop the socket, re-handshake and republish every snapshot for nothing.
            return
        case .unauthorized(let account, let machine)
            where account == identity.accountID && machine == identity.machineID:
            return
        default:
            break
        }

        detach()
        // The generation is claimed *before* the transport is built, so the refusal observer
        // closes over the attachment it belongs to. Comparing identities instead was wrong in a
        // way only the third case shows: sign out and sign back in to the same account and the
        // superseded transport's refusal still matched, and took the new bridge down with it.
        generation &+= 1
        let owned = generation
        do {
            let app = try services.appIdentity(identity)
            let transport = try services.makeTransport(identity, app) { [weak self] error in
                Task { @MainActor [weak self] in
                    self?.authorizationRefused(error, for: identity, generation: owned)
                }
            }
            let bridge = CloudAppBridge(
                transport: transport,
                identity: app,
                sequencing: services.sequencing(identity),
                allowCloudCommands: services.allowCloudCommands,
                commandRouter: services.commandRouter(),
                commandResult: services.commandResult)
            attachedBridge = bridge
            services.attach(bridge)
            set(.attached(accountID: identity.accountID, machineID: identity.machineID))
            services.log("cloud: bridge attached for machine \(identity.machineID)")
        } catch {
            set(.failed(reason: Self.message(for: error)))
            services.log("cloud: the bridge could not be built — \(Self.message(for: error))")
        }
    }

    /// Clear a refusal or a build failure and try once more. Sign-in state changes call
    /// `apply()`; this is for the person who has just fixed something.
    func retry() {
        switch state {
        case .unauthorized, .failed:
            set(.detached)
            apply()
        case .detached, .attached:
            apply()
        }
    }

    /// Called when the Cloud credential has been removed. Detaching here rather than waiting
    /// for the next `apply()` keeps the socket's lifetime inside the account's.
    func signedOut() {
        let wasReading = identityRead.knowledge == .reading
        CloudIdentityReadPolicy.resolveWithoutRead(&identityRead)
        detach()
        set(.detached)
        if wasReading { onChange?() }
    }

    private func authorizationRefused(
        _ error: CloudTransportError, for identity: CloudMachineIdentity, generation owned: UInt64
    ) {
        // A refusal that arrives after the bridge has already been replaced belongs to the
        // bridge that is gone, and must not take the new one down with it — including when the
        // replacement carries the same account and machine, which is what signing out and back
        // in produces.
        guard owned == generation, case .attached(let account, let machine) = state,
              account == identity.accountID, machine == identity.machineID else { return }
        detach()
        set(.unauthorized(accountID: identity.accountID, machineID: identity.machineID))
        services.log("cloud: the control plane refused this machine's device token — \(error)")
    }

    private func detach() {
        guard attachedBridge != nil else { return }
        attachedBridge = nil
        generation &+= 1
        services.attach(nil)
    }

    private func set(_ next: State) {
        guard next != state else { return }
        state = next
        onChange?()
    }

    private static func message(for error: Error) -> String {
        if let localized = error as? LocalizedError, let description = localized.errorDescription,
           !description.isEmpty {
            return description
        }
        return error.localizedDescription
    }
}

extension CloudBridgeLifecycle.Services {
    /// The real thing. Nothing here has a side effect until `apply()` runs it.
    static func production(
        client: CloudAccountClient = CloudAccountClient(),
        keys: CloudKeys = CloudKeys(),
        pairedDevices: CloudPairedDeviceStore = CloudPairedDeviceStore(),
        sequenceFile: CloudSequenceFile = CloudSequenceFile(),
        relayBaseURL: URL = CloudBridgeLifecycle.defaultRelayURL
    ) -> CloudBridgeLifecycle.Services {
        CloudBridgeLifecycle.Services(
            restoredIdentity: { try client.restoredMachineIdentity() },
            appIdentity: { identity in
                CloudAppIdentity(
                    machineID: identity.machineID,
                    // The control plane mints `dev == mid` for `role=machine`
                    // (`api/src/services/tokens.ts`: `deviceId = machine._id`), so the envelope
                    // sender a viewer pins is the machine id and not a second identifier.
                    deviceID: identity.machineID,
                    keyID: CloudBridgeLifecycle.masterKeyID,
                    masterSecret: try keys.loadOrCreateMasterSecret(),
                    signingKey: try keys.loadOrCreateDeviceKeyPair())
            },
            makeTransport: { identity, app, onTerminalFailure in
                CloudTransport(
                    relayBaseURL: relayBaseURL,
                    tokenProvider: CloudSupervisedDeviceTokenProvider(
                        inner: client.deviceTokenProvider(),
                        onTerminalFailure: onTerminalFailure),
                    keyProvider: CloudLifecycleKeyProvider(
                        deviceKey: app.signingKey,
                        masterSecrets: [app.keyID: app.masterSecret],
                        accountID: identity.accountID,
                        pairedDevices: pairedDevices),
                    logger: { Log.write("cloud: \($0)") })
            },
            sequencing: { _ in sequenceFile },
            attach: { RemoteServer.shared.attachCloudBridge($0) },
            // The same gate the local server uses. A Mac that will not accept a message from
            // the browser on its own network does not accept one from the relay either.
            allowCloudCommands: { Config.shared.remoteWrite },
            commandRouter: { RemoteServerCloudCommandRouter() },
            // Refusals are the interesting half and the only half that is logged: a refused
            // cloud command is either a write gate doing its job or a viewer talking to the
            // wrong Mac, and both are things somebody reading the log wants to see.
            commandResult: { result in
                guard result.status < 200 || result.status >= 300 else { return }
                Log.write("cloud: command refused \(result.status) \(result.code ?? "-")")
            },
            log: { Log.write($0) })
    }
}
