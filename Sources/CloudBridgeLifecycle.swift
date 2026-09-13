import Foundation
#if canImport(ClawdlineApplication)
import ClawdlineApplication // W3-1 correction: real cross-module import, see Sources/HostPorts.swift
#endif

/// Production must never allocate a sequence outside `CloudOutboundSpool`. This non-owning
/// compatibility value exists only because narrow bridge fixtures still exercise the old
/// envelope-level seam; production injects `CloudDurableRuntime` and this door is unreachable.
struct CloudDurableOnlySequencing: CloudEnvelopeSequencing {
    func nextSequence(sender _: String) async throws -> UInt64 {
        throw CloudAppBridgeError.sequenceExhausted
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
    private let deviceKey: CloudDeviceKeyPair?
    private let masterSecrets: [String: CloudMasterSecret]
    private let accountID: String?
    private let pairedDevices: CloudPairedDeviceStore?
    private let identityAuthority: CloudExecutorIdentityAuthority?

    init(deviceKey: CloudDeviceKeyPair, masterSecrets: [String: CloudMasterSecret],
         accountID: String, pairedDevices: CloudPairedDeviceStore) {
        self.deviceKey = deviceKey
        self.masterSecrets = masterSecrets
        self.accountID = accountID
        self.pairedDevices = pairedDevices
        identityAuthority = nil
    }

    init(identityAuthority: CloudExecutorIdentityAuthority) {
        deviceKey = nil
        masterSecrets = [:]
        accountID = nil
        pairedDevices = nil
        self.identityAuthority = identityAuthority
    }

    func deviceKeyPair() async throws -> CloudDeviceKeyPair {
        if let identityAuthority { return try identityAuthority.transportMaterial().deviceKey }
        guard let deviceKey else { throw CloudTransportError.unexpectedFrame("identity-unavailable") }
        return deviceKey
    }

    func masterSecret(for keyID: String) async throws -> CloudMasterSecret {
        if let identityAuthority {
            let material = try identityAuthority.transportMaterial()
            guard material.keyID == keyID else {
                throw CloudTransportError.unexpectedFrame("unknown-key")
            }
            return material.masterSecret
        }
        guard let secret = masterSecrets[keyID] else {
            throw CloudTransportError.unexpectedFrame("unknown-key")
        }
        return secret
    }

    func pairedDevicePublicKeys() async -> [String: Data] {
        if let identityAuthority {
            return (try? identityAuthority.transportMaterial().pairedDevicePublicKeys) ?? [:]
        }
        guard let pairedDevices, let accountID,
              let devices = try? pairedDevices.devices(accountID: accountID) else { return [:] }
        var keys: [String: Data] = [:]
        for device in devices { keys[device.deviceID] = device.signingKey }
        return keys
    }

    /// The same read as `pairedDevicePublicKeys()`, except that a Keychain refusal is reported as
    /// unreadable instead of becoming the empty roster an unpaired Mac also has.
    func pairedDeviceRoster() async -> CloudPairedDeviceRosterReading {
        if let identityAuthority {
            guard let material = try? identityAuthority.transportMaterial() else {
                return .unreadable
            }
            return .readable(material.pairedDevicePublicKeys)
        }
        guard let pairedDevices, let accountID,
              let devices = try? pairedDevices.devices(accountID: accountID) else {
            return .unreadable
        }
        var keys: [String: Data] = [:]
        for device in devices { keys[device.deviceID] = device.signingKey }
        return .readable(keys)
    }

    func currentKeyID() async -> String? {
        if let identityAuthority { return try? identityAuthority.transportMaterial().keyID }
        return masterSecrets.count == 1 ? masterSecrets.keys.first : nil
    }

    func transportBinding() async throws -> CloudExecutorTransportBinding? {
        try identityAuthority?.transportMaterial().binding
    }

    func admitReconnect(_ binding: CloudExecutorTransportBinding) async throws {
        guard let identityAuthority else { return }
        _ = try identityAuthority.verifyReconnect(CloudExecutorReconnectProof(
            accountID: binding.accountID, machineID: binding.machineID,
            deviceID: binding.deviceID, identityGeneration: binding.identityGeneration,
            keyEpoch: binding.keyEpoch, revocationEpoch: binding.revocationEpoch,
            durableLedgerOpened: true, durableSpoolOpened: true))
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
    let onAuthenticatedServerDate: @Sendable (Date) -> Void

    init(
        inner: any CloudDeviceTokenProviding,
        onTerminalFailure: @escaping @Sendable (CloudTransportError) -> Void,
        onAuthenticatedServerDate: @escaping @Sendable (Date) -> Void = { _ in }
    ) {
        self.inner = inner
        self.onTerminalFailure = onTerminalFailure
        self.onAuthenticatedServerDate = onAuthenticatedServerDate
    }

    func fetchDeviceToken() async throws -> CloudDeviceToken {
        do {
            let token = try await inner.fetchDeviceToken()
            if let serverDate = token.authenticatedServerDate {
                onAuthenticatedServerDate(serverDate)
            }
            return token
        } catch let error as CloudTransportError where error == .unauthorized {
            onTerminalFailure(error)
            throw error
        }
    }
}

/// Thread-safe owner of the real host epoch guard. Only a Date header from a successful pinned
/// HTTPS device-token response establishes calibration; every command effect observes the guard
/// again after durable reservation.
///
/// Both halves of that wiring live here — the token provider that feeds the guard and the
/// effect-time authorization that reads it — so `Services.production()` and the tests compose
/// the same code. Tests replace only the two leaf readings, the wall clock and the kernel clock.
///
/// The device token rotates every four minutes and every rotation or reconnect carries a server
/// date, so a date is offered, never forced: it calibrates only a guard that has no live
/// calibration. Re-arming on each one refused every command for the minute after each rotation.
///
/// The guard's continuous clock is `CLOCK_MONOTONIC_RAW` (`mach_continuous_time`), which keeps
/// counting while the Mac sleeps. `ProcessInfo.systemUptime` and `CLOCK_UPTIME_RAW` stop, so the
/// guard read every wake as a forward wall jump and refused commands until the next token fetch
/// plus a full window; measured on this Mac on 2026-09-13 they stood 60,262 s behind
/// wall time since boot, while `CLOCK_MONOTONIC_RAW` stood within 6 s of it over 913,711 s.
/// The ledger's own `continuousNow` and `effectNotAfterContinuous` stay on
/// `DispatchTime.uptimeNanoseconds`: those values are compared only with each other, and none of
/// this guard's readings leaves it — the ledger receives `.ready` or `.uncertain`, nothing more.
final class CloudCommandEpochAuthority: @unchecked Sendable {
    typealias KernelNanoseconds = (clockid_t) -> UInt64

    private static let bootID = UUID().uuidString.lowercased()
    private let lock = NSLock()
    private let guardState: EpochGuard

    init(
        wall: @escaping () -> Date = { Date() },
        kernelNanoseconds: @escaping KernelNanoseconds = { clock_gettime_nsec_np($0) }
    ) {
        guardState = EpochGuard(clock: CloudClock(
            wall: wall,
            continuous: { TimeInterval(kernelNanoseconds(CLOCK_MONOTONIC_RAW)) / 1_000_000_000 },
            bootID: { CloudCommandEpochAuthority.bootID }))
    }

    func supervisedTokenProvider(
        inner: any CloudDeviceTokenProviding,
        onTerminalFailure: @escaping @Sendable (CloudTransportError) -> Void
    ) -> CloudSupervisedDeviceTokenProvider {
        CloudSupervisedDeviceTokenProvider(
            inner: inner,
            onTerminalFailure: onTerminalFailure,
            onAuthenticatedServerDate: { [self] date in
                acceptAuthenticatedServerDate(date)
            })
    }

    func acceptAuthenticatedServerDate(_ date: Date) {
        lock.lock(); defer { lock.unlock() }
        _ = guardState.offerServerDate(date)
    }

    func current() -> CloudEpochGuardState { detail().state }

    /// The guard's state with the words a refusal and the status snapshot need. The ledger still
    /// receives only `.ready` or `.uncertain`; the reason and countdown are reported beside it.
    func detail() -> CloudClockGuardDetail {
        lock.lock(); defer { lock.unlock() }
        switch guardState.observe().state {
        case .ready:
            return CloudClockGuardDetail(state: .ready, reason: nil, clearsInMilliseconds: nil)
        case .uncertain(let reason):
            let remaining = guardState.stabilityRemaining()
            return CloudClockGuardDetail(
                state: .uncertain,
                reason: Self.reasonName(reason, calibrated: remaining != nil),
                clearsInMilliseconds: remaining.map { UInt64(($0 * 1_000).rounded(.up)) })
        }
    }

    func effectAuthorization(
        rosterAllowsSender: Bool, writeGateAllows: Bool, rosterReadable: Bool = true
    ) -> CloudCommandEffectAuthorization {
        let clock = detail()
        return CloudCommandEffectAuthorization(
            epochState: clock.state,
            rosterAllowsSender: rosterAllowsSender,
            writeGateAllows: writeGateAllows,
            rosterReadable: rosterReadable,
            epochReason: clock.reason,
            epochClearsInMilliseconds: clock.clearsInMilliseconds)
    }

    /// `stability_period_incomplete` with no live calibration means no server time has been
    /// accepted yet, which is a different wait from a window that is counting down.
    static func reasonName(_ reason: EpochGuardUncertaintyReason, calibrated: Bool) -> String {
        switch reason {
        case .stabilityPeriodIncomplete:
            return calibrated ? "stability_period_incomplete" : "awaiting_server_time"
        case .serverSampleTooFar: return "server_sample_too_far"
        case .wallRollback: return "wall_rollback"
        case .forwardJump: return "forward_jump"
        case .bootIDChanged: return "boot_id_changed"
        case .continuousWentBackwards: return "continuous_went_backwards"
        }
    }
}

/// One reading of the command clock guard for refusals and the Cloud status snapshot.
struct CloudClockGuardDetail: Equatable, Sendable {
    let state: CloudEpochGuardState
    let reason: String?
    let clearsInMilliseconds: UInt64?
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

    static func acceptsTimeout(generation: UInt64, tracker: Tracker) -> Bool {
        tracker.inFlightGeneration == generation && tracker.knowledge == .reading
    }

    /// Sign-out is an authoritative foreground event. It invalidates an answer already on its
    /// way back without pretending the blocking operation itself can be cancelled.
    static func resolveWithoutRead(_ tracker: inout Tracker) {
        tracker.latestGeneration &+= 1
        tracker.refreshPending = false
        tracker.knowledge = .resolved
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
    struct RestoredIdentity: Sendable {
        let machine: CloudMachineIdentity
        let app: CloudAppIdentity
    }

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
        var identityReader: CloudKeychainReader<RestoredIdentity?>
        var makeTransport: @MainActor (
            CloudMachineIdentity, CloudAppIdentity,
            @escaping @Sendable (CloudTransportError) -> Void
        ) throws -> any CloudTransporting
        var sequencing: @MainActor (CloudMachineIdentity) -> any CloudEnvelopeSequencing
        /// Production's fail-closed command ledger and outbound spool. Tests may omit it to use
        /// envelope-observing fakes; the production factory below always supplies it.
        var durableRuntime: (@MainActor (CloudMachineIdentity) throws -> CloudDurableRuntime)? = nil
        var attach: @MainActor (CloudAppBridge?) -> Void
        var allowCloudCommands: @Sendable () -> Bool
        /// Re-reads clock epoch, the current paired roster and the current write gate at the
        /// durable command's effect point. Production always supplies this closure.
        var commandEffectAuthority: @MainActor (
            CloudMachineIdentity
        ) -> CloudAppBridge.CommandEffectAuthority = { _ in
            { _, _ in CloudCommandEffectAuthorization(
                epochState: .uncertain, rosterAllowsSender: false, writeGateAllows: false) }
        }
        /// The one door a cloud command enters the app through. Production hands back
        /// `RemoteServerCloudCommandRouter`, so authentication, idempotency, image validation
        /// and audit stay the local HTTP route's single implementation rather than a copy.
        var commandRouter: @MainActor () -> any CloudCommandRouting
        var commandResult: @Sendable (CloudCommandResult) -> Void
        var log: @MainActor (String) -> Void
        /// Bridge diagnostics are emitted off the main actor after routing or publication.
        /// Tests leave this silent; production uses the thread-safe file logger.
        var diagnostic: @Sendable (String) -> Void = { _ in }
        /// Lifecycle-owned companion to the relay bridge. Tests omit it; production starts one
        /// machine-credential webhook poller for exactly the restored signed-in identity.
        var scheduleWebhooks: @MainActor (
            CloudMachineIdentity?, @escaping @Sendable () -> Void
        ) -> Void = { _, _ in }
        /// The process-lifetime status owner every bridge this lifecycle builds records into.
        /// Tests omit it; production passes `CloudStatus.shared`, which also writes the file.
        var status: CloudStatus? = nil
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
    private var identityRead = CloudIdentityReadPolicy.Tracker()
    private(set) var identityReadTimeoutSeconds: Int?

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
        services.identityReader.read(
            onTimeout: { [weak self] seconds in
                guard let self else { return }
                guard CloudIdentityReadPolicy.acceptsTimeout(
                    generation: generation, tracker: self.identityRead) else { return }
                self.identityReadTimeoutSeconds = seconds
                self.services.log("cloud: Keychain identity read timed out; state remains unknown while reconciliation continues")
                self.onChange?()
            },
            completion: { [weak self] result in
                self?.finishedIdentityRead(result, generation: generation)
            })
    }

    private func finishedIdentityRead(
        _ result: Result<RestoredIdentity?, Error>, generation: UInt64
    ) {
        let wasReading = identityRead.knowledge == .reading
        switch CloudIdentityReadPolicy.complete(generation: generation, tracker: &identityRead) {
        case .restart(let next):
            identityReadTimeoutSeconds = nil
            startIdentityRead(generation: next)
            return
        case .discard:
            if wasReading && identityRead.knowledge != .reading { onChange?() }
            return
        case .accept:
            identityReadTimeoutSeconds = nil
            break
        }

        if wasReading { onChange?() }
        switch result {
        case .failure(let error):
            detach()
            set(.failed(reason: Self.message(for: error)))
            services.log("cloud: the identity material could not be read — \(Self.message(for: error))")
            return
        case .success(nil):
            detach()
            set(.detached)
            return
        case .success(let identity?):
            apply(identity: identity)
        }
    }

    private func apply(identity restored: RestoredIdentity) {
        let identity = restored.machine
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
            // Durable owners are admission prerequisites, not peers of the socket. Opening them
            // first means `CloudLifecycleKeyProvider.admitReconnect` can truthfully prove both
            // flags before any initial or reconnect challenge is signed.
            let durableRuntime = try services.durableRuntime?(identity)
            let transport = try services.makeTransport(identity, restored.app) { [weak self] error in
                Task { @MainActor [weak self] in
                    self?.authorizationRefused(error, for: identity, generation: owned)
                }
            }
            let bridge = CloudAppBridge(
                transport: transport,
                identity: restored.app,
                sequencing: services.sequencing(identity),
                allowCloudCommands: services.allowCloudCommands,
                currentCommandEffectAuthority: services.commandEffectAuthority(identity),
                commandRouter: services.commandRouter(),
                commandResult: services.commandResult,
                diagnostic: services.diagnostic,
                durableRuntime: durableRuntime,
                status: services.status)
            attachedBridge = bridge
            services.attach(bridge)
            services.scheduleWebhooks(identity) { [weak self] in
                Task { @MainActor [weak self] in
                    self?.scheduleWebhookAuthorizationRefused(
                        identity: identity, generation: owned)
                }
            }
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
        identityReadTimeoutSeconds = nil
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

    private func scheduleWebhookAuthorizationRefused(
        identity: CloudMachineIdentity, generation owned: UInt64
    ) {
        guard owned == generation, case .attached(let account, let machine) = state,
              account == identity.accountID, machine == identity.machineID else { return }
        detach()
        set(.unauthorized(accountID: identity.accountID, machineID: identity.machineID))
        services.log("cloud: schedule webhook polling stopped after machine authorization refusal")
    }

    private func detach() {
        guard attachedBridge != nil else { return }
        attachedBridge = nil
        generation &+= 1
        services.attach(nil)
        services.scheduleWebhooks(nil, {})
    }

    private func set(_ next: State) {
        guard next != state else { return }
        state = next
        services.status?.setBridgeState(Self.statusLabel(next))
        onChange?()
    }

    static func statusLabel(_ state: State) -> String {
        switch state {
        case .detached: return "detached"
        case .attached: return "attached"
        case .unauthorized: return "unauthorized"
        case .failed: return "failed"
        }
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
        identityAuthority: CloudExecutorIdentityAuthority = CloudExecutorIdentityAuthority(
            store: CloudKeychainStore()),
        legacyPairedDevices: CloudPairedDeviceStore = CloudPairedDeviceStore(),
        relayBaseURL: URL = CloudBridgeLifecycle.defaultRelayURL
    ) -> CloudBridgeLifecycle.Services {
        let epochAuthority = CloudCommandEpochAuthority()
        let status = CloudStatus.shared
        status.setClockProvider { epochAuthority.detail() }
        let identityReader = CloudKeychainReader<CloudBridgeLifecycle.RestoredIdentity?>(
            label: "clawdline.cloud.bridge-identity"
        ) {
            guard let identity = try client.restoredMachineIdentity() else { return nil }
            _ = try CloudMachineFilesystemNamespace.component(for: identity.machineID)
            switch identityAuthority.readiness(
                expectedAccountID: identity.accountID,
                expectedMachineID: identity.machineID) {
            case .ready:
                break
            case .blocked(.protectedStateMissing):
                let imported = try legacyPairedDevices.beginProtectedMigration(
                    accountID: identity.accountID).map {
                    CloudExecutorPairedDevice(
                        deviceID: $0.deviceID, signingKey: $0.signingKey,
                        fingerprint: try CloudPairing.ed25519Fingerprint(
                            publicKeyRaw: $0.signingKey),
                        pairedAtMilliseconds: $0.pairedAtMilliseconds,
                        identityGeneration: 1,
                        capabilities: CloudExecutorIdentityAuthority.defaultCapabilities)
                }
                _ = try identityAuthority.provision(
                    accountID: identity.accountID, machineID: identity.machineID,
                    deviceKey: keys.loadOrCreateDeviceKeyPair(),
                    masterSecret: keys.loadOrCreateMasterSecret(),
                    importedPairedDevices: imported)
            case .blocked(let error):
                throw error
            }
            // A pre-W5 image must never regain authorization from its stale JSON roster. If a
            // crash lands between the protected commit and this removal, the next apply repeats
            // only the removal because provision is already durable and idempotent.
            try legacyPairedDevices.removeAll()
            let material = try identityAuthority.transportMaterial()
            return CloudBridgeLifecycle.RestoredIdentity(
                machine: identity,
                app: CloudAppIdentity(
                    machineID: identity.machineID,
                    // The control plane mints `dev == mid` for `role=machine`
                    // (`api/src/services/tokens.ts`: `deviceId = machine._id`), so the envelope
                    // sender a viewer pins is the machine id and not a second identifier.
                    deviceID: identity.machineID,
                    keyID: material.keyID,
                    masterSecret: material.masterSecret,
                    signingKey: material.deviceKey))
        }
        return CloudBridgeLifecycle.Services(
            identityReader: identityReader,
            makeTransport: { identity, app, onTerminalFailure in
                CloudTransport.production(
                    relayBaseURL: relayBaseURL,
                    tokenProvider: epochAuthority.supervisedTokenProvider(
                        inner: client.deviceTokenProvider(),
                        onTerminalFailure: onTerminalFailure),
                    keyProvider: CloudLifecycleKeyProvider(
                        identityAuthority: identityAuthority),
                    logger: { Log.write("cloud: \($0)") })
            },
            // This compatibility seam owns no counter. The production bridge receives the
            // durable runtime below, so all sequence allocation belongs to its spool.
            sequencing: { _ in CloudDurableOnlySequencing() },
            durableRuntime: { identity in
                let legacyFence = try CloudLegacySequenceFence(
                    url: RemoteAuth.directory.appendingPathComponent("cloud-sequence.json"),
                    sender: identity.machineID)
                let component = try CloudMachineFilesystemNamespace.component(
                    for: identity.machineID)
                return try CloudDurableRuntime.open(
                    directory: RemoteAuth.directory
                        .appendingPathComponent("cloud-runtime", isDirectory: true)
                        .appendingPathComponent(component, isDirectory: true),
                    runtime: .mac, metrics: CloudStatusSpoolMetrics(status: status),
                    minimumNextSequence: legacyFence.reservedCeiling,
                    sequenceFence: legacyFence, strictPersistedFrameValidation: true)
            },
            attach: { RemoteServer.shared.attachCloudBridge($0) },
            // The same gate the local server uses. A Mac that will not accept a message from
            // the browser on its own network does not accept one from the relay either.
            allowCloudCommands: { Config.shared.remoteWrite },
            commandEffectAuthority: { identity in
                { sender, requiresWriteGate in
                    // Read once, and keep "could not read" apart from "not in it": the refusal
                    // names them differently (`command_roster_unreadable`, `unknown_sender`).
                    let roster = try? identityAuthority.snapshot()
                    let rosterAllows = roster?.pairedDevices.contains {
                        $0.deviceID == sender
                    } == true
                    return epochAuthority.effectAuthorization(
                        rosterAllowsSender: rosterAllows,
                        writeGateAllows: !requiresWriteGate || Config.shared.remoteWrite,
                        rosterReadable: roster != nil)
                }
            },
            commandRouter: { RemoteServerCloudCommandRouter() },
            // Refusals are logged by the bridge itself, one `cloud: refusal layer=… code=…` line
            // each, because only the bridge knows the layer, the reference and whether the viewer
            // was answered. This observer used to write a second, reference-free line.
            commandResult: { _ in },
            log: { Log.write($0) },
            diagnostic: { Log.write($0) },
            scheduleWebhooks: { identity, onUnauthorized in
                Task {
                    if let identity {
                        await ScheduleWebhookRuntime.shared.start(
                            identity: identity, onUnauthorized: onUnauthorized)
                    } else {
                        await ScheduleWebhookRuntime.shared.stop()
                    }
                }
            },
            status: status)
    }
}
