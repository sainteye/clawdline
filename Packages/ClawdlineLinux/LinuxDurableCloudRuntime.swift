import Foundation
import ClawdlineApplication

/// Ubuntu's production ownership leaf for the shared Application ledger, spool, account client
/// and protected executor identity. It exposes bootstrap but no publish door: W0-E remains a
/// candidate until the separate contract/cutover authority says otherwise.
final class LinuxDurableCloudRuntime {
    enum EnrollmentState: Equatable {
        case missing
        case enrolled(accountID: String, machineID: String)
    }

    struct Readiness: Codable, Equatable {
        let durable: Bool
        let authority: String
        let cutoverRequired: Bool
        let emissionEnabled: Bool
        let code: String
    }

    let runtime: CloudDurableRuntime
    let accountClient: CloudAccountClient
    let keys: CloudKeys
    let identityAuthority: CloudExecutorIdentityAuthority
    let candidateAuthority = CloudContractCandidateAuthority.w0E

    init(stateDirectory: String, expectedUID: UInt32, secrets: any SecretStore) throws {
        guard ProjectRootPolicy.isLexicallySafeAbsolute(stateDirectory) else {
            throw LinuxDurableStateFailure(
                code: "unsafe_cloud_state_path",
                message: "The Cloud durable state path must be canonical and absolute.")
        }
        runtime = try CloudDurableRuntime.open(
            directory: URL(fileURLWithPath: stateDirectory, isDirectory: true)
                .appendingPathComponent("cloud", isDirectory: true),
            // W0-E accepts only mac|viewer. Linux metrics therefore remain typed-unavailable
            // until an additive accepted label exists; reporting Ubuntu as Mac is forbidden.
            expectedUID: expectedUID, runtime: nil,
            strictPersistedFrameValidation: true)
        let keyStore = CloudSecretStoreKeyAdapter(store: secrets)
        let keys = CloudKeys(store: keyStore)
        self.keys = keys
        let authority = CloudExecutorIdentityAuthority(store: secrets)
        identityAuthority = authority
        accountClient = CloudAccountClient(
            apiBaseURL: URL(string: "https://api.clawdline.com")!,
            transport: CloudAccountURLSessionTransport(),
            credentialStore: keyStore,
            deviceKeyLoader: {
                switch authority.readiness() {
                case .ready:
                    return try authority.transportMaterial().deviceKey
                case .blocked(.protectedStateMissing):
                    // The legacy item is only the explicit pre-enrollment bootstrap source.
                    return try keys.loadOrCreateDeviceKeyPair()
                case .blocked(let error):
                    throw error
                }
            })
    }

    var readiness: Readiness {
        let code: String
        switch identityAuthority.readiness() {
        case .ready:
            code = "w0e_contract_cutover_required"
        case .blocked(let error):
            switch error {
            case .protectedStateMissing: code = "w5_executor_identity_missing"
            case .protectedStateFutureVersion: code = "w5_executor_identity_future_version"
            case .protectedStateCorrupt: code = "w5_executor_identity_corrupt"
            case .identityMismatch: code = "w5_executor_identity_wrong_owner"
            default: code = "w5_executor_identity_unavailable"
            }
        }
        return Readiness(
            durable: true, authority: candidateAuthority.authority,
            cutoverRequired: candidateAuthority.cutoverRequired,
            emissionEnabled: candidateAuthority.permitsEmission(wireVersion: 1), code: code)
    }

    /// Enrollment is a three-way protected-state decision, not "try login if restore returned
    /// nil". A retained credential without its executor identity (or the reverse) is corruption
    /// and must never be repaired by silently creating a second machine identity.
    func enrollmentState() throws -> EnrollmentState {
        let restored = try accountClient.restoredMachineIdentity()
        switch identityAuthority.readiness() {
        case .ready(let snapshot):
            guard let restored,
                  restored.accountID == snapshot.accountID,
                  restored.machineID == snapshot.machineID else {
                throw CloudExecutorIdentityError.identityMismatch
            }
            return .enrolled(accountID: snapshot.accountID, machineID: snapshot.machineID)
        case .blocked(.protectedStateMissing):
            guard restored == nil else { throw CloudExecutorIdentityError.identityMismatch }
            return .missing
        case .blocked(let error):
            throw error
        }
    }

    func startDeviceLogin(metadata: CloudMachineMetadata) async throws -> CloudDeviceLoginStart {
        try await accountClient.startDeviceLogin(metadata: metadata)
    }

    /// The completed account owner and the keys generated at login-start are committed as one
    /// protected executor identity before this method reports completion.
    func continueDeviceLogin(_ started: CloudDeviceLoginStart) async throws
        -> CloudDeviceLoginPollState {
        let state = try await accountClient.waitForDeviceLogin(started)
        if case .complete(let owner) = state {
            switch identityAuthority.readiness(
                expectedAccountID: owner.accountID, expectedMachineID: owner.machineID) {
            case .ready:
                break
            case .blocked(.protectedStateMissing):
                _ = try identityAuthority.provision(
                    accountID: owner.accountID, machineID: owner.machineID,
                    deviceKey: keys.loadOrCreateDeviceKeyPair(),
                    masterSecret: keys.loadOrCreateMasterSecret())
            case .blocked(let error):
                throw error
            }
        }
        return state
    }

    func restoreExecutorIdentity() throws -> CloudExecutorIdentitySnapshot? {
        guard let owner = try accountClient.restoredMachineIdentity() else { return nil }
        let snapshot = try identityAuthority.snapshot()
        guard snapshot.accountID == owner.accountID, snapshot.machineID == owner.machineID else {
            throw CloudExecutorIdentityError.identityMismatch
        }
        return snapshot
    }

    /// Constructs the daemon's one authenticated Relay owner from the already-open ledger/spool.
    /// Missing enrollment is a normal detached state; corrupt or mismatched protected identity is
    /// a startup failure and never falls back to memory or a second store.
    func makeRelayOwner(
        ingress: LinuxDaemonIngressOwner,
        commandsEnabled: @escaping @Sendable () -> Bool,
        presentation: LinuxRelayMachinePresentation,
        places: [LinuxRelayPlace],
        inventory: @escaping @Sendable () -> TerminalInventory,
        observations: @escaping @Sendable (TerminalInventory) async
            -> [String: TerminalSessionPresentation.Observation] = { _ in [:] },
        screenCapture: @escaping @Sendable (TargetSession) async throws -> String?,
        relayBaseURL: URL = URL(string: "wss://relay.clawdline.com/v1/connect")!,
        diagnostic: @escaping @Sendable (String) -> Void = { _ in },
        stateObserver: @escaping @Sendable (LinuxRelayRuntimeState) -> Void = { _ in }
    ) throws -> LinuxRelayRuntimeOwner? {
        guard let owner = try accountClient.restoredMachineIdentity() else { return nil }
        let material = try identityAuthority.transportMaterial()
        guard material.binding.accountID == owner.accountID,
              material.binding.machineID == owner.machineID,
              material.binding.deviceID == owner.machineID else {
            throw CloudExecutorIdentityError.identityMismatch
        }
        let transport = CloudTransport.production(
            relayBaseURL: relayBaseURL, tokenProvider: accountClient.deviceTokenProvider(),
            keyProvider: CloudExecutorIdentityTransportKeys(authority: identityAuthority),
            logger: diagnostic)
        let identity = Self.appIdentity(material)
        let relay = LinuxRelayRuntimeOwner(
            machine: owner, identityAuthority: identityAuthority,
            transport: transport,
            outbound: CloudDurableOutboundComposition(
                spool: runtime.spool, transport: transport, identity: identity,
                nowMilliseconds: {
                    UInt64(max(0, Date().timeIntervalSince1970 * 1_000))
                }, diagnostic: diagnostic),
            ingress: ingress, commandsEnabled: commandsEnabled, diagnostic: diagnostic,
            presentation: presentation, places: places, inventory: inventory,
            observations: observations, screenCapture: screenCapture,
            stateObserver: stateObserver)
        return relay
    }

    fileprivate static func appIdentity(
        _ material: CloudExecutorTransportMaterial
    ) -> CloudAppIdentity {
        CloudAppIdentity(
            machineID: material.binding.machineID,
            deviceID: material.binding.deviceID,
            keyID: material.keyID,
            masterSecret: material.masterSecret,
            signingKey: material.deviceKey)
    }
}

enum LinuxRelayRuntimeState: Equatable, Sendable {
    case idle
    case starting
    case running
    case degraded
    case stoppingAuthorization
    case unauthorized
    case failed
    case stopped
}

struct LinuxRelayMachinePresentation: Equatable, Sendable {
    let displayName: String
    let provider: String?
}

struct LinuxRelayPlace: Equatable, Sendable {
    let id: String
    let label: String
    let path: String
}

/// Synchronous projection used by health/log code without creating a second runtime owner.
final class LinuxRelayRuntimeStatus: @unchecked Sendable {
    private let lock = NSLock()
    private var value: LinuxRelayRuntimeState = .idle

    func record(_ state: LinuxRelayRuntimeState) {
        lock.lock(); value = state; lock.unlock()
    }

    var state: LinuxRelayRuntimeState {
        lock.lock(); defer { lock.unlock() }; return value
    }

    var readinessCode: String {
        switch state {
        case .idle: return "w5_linux_relay_idle"
        case .starting: return "w5_linux_relay_starting"
        case .running: return "w5_linux_relay_running_candidate_cutover_required"
        case .degraded: return "w5_linux_relay_degraded_retrying"
        case .stoppingAuthorization: return "w5_linux_relay_stopping_unauthorized"
        case .unauthorized: return "w5_linux_relay_unauthorized"
        case .failed: return "w5_linux_relay_failed"
        case .stopped: return "w5_linux_relay_stopped"
        }
    }
}

/// The daemon-lifetime strong owner for the one Relay actor and its start/stop task.
final class LinuxRelayRuntimeSupervisor: @unchecked Sendable {
    private let owner: LinuxRelayRuntimeOwner
    private let diagnostic: @Sendable (String) -> Void
    private let lock = NSLock()
    private var startTask: Task<Void, Never>?
    private let sleep: @Sendable (TimeInterval) async throws -> Void
    let status: LinuxRelayRuntimeStatus

    init(owner: LinuxRelayRuntimeOwner, status: LinuxRelayRuntimeStatus,
         diagnostic: @escaping @Sendable (String) -> Void,
         sleep: @escaping @Sendable (TimeInterval) async throws -> Void = {
             try await Task.sleep(nanoseconds: UInt64($0 * 1_000_000_000))
         }) {
        self.owner = owner
        self.status = status
        self.diagnostic = diagnostic
        self.sleep = sleep
    }

    func start() {
        lock.lock()
        guard startTask == nil else { lock.unlock(); return }
        let owner = owner
        let diagnostic = diagnostic
        let sleep = sleep
        startTask = Task {
            var delay = 0.25
            while !Task.isCancelled {
                do { try await owner.startOrRetry(); return }
                catch is CancellationError {
                    diagnostic("linux cloud: Relay start cancelled")
                    return
                } catch let error as CloudTransportError where error == .unauthorized {
                    return
                } catch {
                    diagnostic("linux cloud: Relay unavailable; retrying with bounded backoff")
                    do { try await sleep(delay) } catch { return }
                    delay = min(30, delay * 2)
                }
            }
        }
        lock.unlock()
    }

    func stopAndWait() {
        lock.lock(); let started = startTask; startTask = nil; lock.unlock()
        let owner = owner
        let completed = DispatchSemaphore(value: 0)
        Task.detached {
            await owner.stop()
            _ = await started?.value
            completed.signal()
        }
        completed.wait()
    }

    func publishInventory(_ inventory: TerminalInventory) {
        Task { await owner.publishInventory(inventory) }
    }
}

/// Exactly one instance is composed by `LinuxDaemonService`. It owns the transport generation,
/// the shared durable outbound drain and the three bounded receive streams; every command effect
/// still enters `LinuxDaemonIngressOwner`, whose lock and ledger remain the sole Linux authority.
actor LinuxRelayRuntimeOwner {
    /// The hosted console expires machine discovery after five minutes. The daemon's existing
    /// ten-second observation cadence calls `publishInventory`; refreshing at four minutes leaves
    /// a bounded scheduling margin without turning a descriptor into transport authority.
    static let descriptorHeartbeatIntervalSeconds: TimeInterval = 240

    /// The machine-session words a browser may send this executor, and the only ones it answers
    /// with an effect. Published in the descriptor as `commands` so the hosted console can route
    /// by them; every other word is refused as `unknown_command` (`adaptBrowserCommand`).
    static let browserCommandTypes = ["info", "places", "screen", "send", "start", "transcript"]

    private let machine: CloudMachineIdentity
    private let identityAuthority: CloudExecutorIdentityAuthority
    private let transport: any CloudTransporting
    private let outbound: CloudDurableOutboundComposition
    private let ingress: LinuxDaemonIngressOwner
    private let commandsEnabled: @Sendable () -> Bool
    private let diagnostic: @Sendable (String) -> Void
    private let stateObserver: @Sendable (LinuxRelayRuntimeState) -> Void
    private let presentation: LinuxRelayMachinePresentation
    private let places: [LinuxRelayPlace]
    private let inventory: @Sendable () -> TerminalInventory
    private let observations: @Sendable (TerminalInventory) async
        -> [String: TerminalSessionPresentation.Observation]
    private let screenCapture: @Sendable (TargetSession) async throws -> String?
    private let monotonicNow: @Sendable () -> TimeInterval
    private var commandTask: Task<Void, Never>?
    private var readyTask: Task<Void, Never>?
    private var receiptTask: Task<Void, Never>?
    private var transportStopped = false
    private var streamsInstalled = false
    private(set) var state: LinuxRelayRuntimeState = .idle {
        didSet { stateObserver(state) }
    }
    private(set) var authenticatedGeneration: UInt64 = 0
    private var lifecycleGeneration: UInt64 = 0
    private var lastInventoryDigest: String?
    private var publishedInventoryRows: [String: Data] = [:]
    private var inventoryPublicationInFlight = false
    private var pendingInventoryPublication: (snapshot: TerminalInventory, force: Bool)?
    private var lastDescriptorPublishedAt: TimeInterval?

    init(
        machine: CloudMachineIdentity,
        identityAuthority: CloudExecutorIdentityAuthority,
        transport: any CloudTransporting,
        outbound: CloudDurableOutboundComposition,
        ingress: LinuxDaemonIngressOwner,
        commandsEnabled: @escaping @Sendable () -> Bool,
        diagnostic: @escaping @Sendable (String) -> Void = { _ in },
        presentation: LinuxRelayMachinePresentation = .init(
            displayName: "Clawdline Linux", provider: nil),
        places: [LinuxRelayPlace] = [],
        inventory: @escaping @Sendable () -> TerminalInventory = { TerminalInventory() },
        observations: @escaping @Sendable (TerminalInventory) async
            -> [String: TerminalSessionPresentation.Observation] = { _ in [:] },
        screenCapture: @escaping @Sendable (TargetSession) async throws -> String? = { _ in nil },
        stateObserver: @escaping @Sendable (LinuxRelayRuntimeState) -> Void = { _ in },
        monotonicNow: @escaping @Sendable () -> TimeInterval = {
            ProcessInfo.processInfo.systemUptime
        }
    ) {
        self.machine = machine
        self.identityAuthority = identityAuthority
        self.transport = transport
        self.outbound = outbound
        self.ingress = ingress
        self.commandsEnabled = commandsEnabled
        self.diagnostic = diagnostic
        self.presentation = presentation
        self.places = places
        self.inventory = inventory
        self.observations = observations
        self.screenCapture = screenCapture
        self.stateObserver = stateObserver
        self.monotonicNow = monotonicNow
    }

    func startOrRetry() async throws {
        guard state == .idle || state == .degraded else {
            throw CloudTransportError.alreadyConnected
        }
        state = .starting
        await transport.setTerminalAuthorizationHandler { [weak self] _ in
            Task { await self?.authorizationRefused() }
        }
        if !streamsInstalled {
            streamsInstalled = true
            lifecycleGeneration &+= 1
            let owned = lifecycleGeneration
            let commandStream = transport.commands
            let readyStream = transport.readyGenerations
            let receipts = transport.outboundReceipts
            commandTask = Task { [weak self] in
                for await command in commandStream {
                    guard let self else { return }
                    await self.consume(command, generation: owned)
                }
            }
            readyTask = Task { [weak self] in
                for await generation in readyStream {
                    guard let self else { return }
                    await self.ready(generation, lifecycleGeneration: owned)
                }
            }
            receiptTask = Task { [weak self] in
                for await receipt in receipts {
                    guard let self else { return }
                    await self.settle(receipt, lifecycleGeneration: owned)
                }
            }
        }
        let owned = lifecycleGeneration
        do {
            try await transport.connect(role: .machine)
            guard lifecycleGeneration == owned, state == .starting else {
                throw CancellationError()
            }
            state = .running
        } catch {
            if lifecycleGeneration == owned, state == .starting {
                if Self.isTerminalAuthorization(error) {
                    await authorizationRefused()
                } else {
                    state = .degraded
                }
            }
            if Self.isTerminalAuthorization(error) { throw CloudTransportError.unauthorized }
            throw error
        }
    }

    func start() async throws { try await startOrRetry() }

    static func isTerminalAuthorization(_ error: Error) -> Bool {
        guard let error = error as? CloudTransportError else { return false }
        switch error {
        case .unauthorized: return true
        case .upgradeRefused(let status): return status == 401 || status == 403
        case .relay(let code, _):
            return ["unauthorized", "forbidden", "revoked", "device_revoked",
                    "account_revoked"].contains(code.lowercased())
        default: return false
        }
    }

    func stop() async {
        guard state != .stopped else { return }
        state = .stopped
        lifecycleGeneration &+= 1
        await stopStreamsAndTransport()
    }

    func authorizationRefused() async {
        guard state != .stoppingAuthorization, state != .unauthorized,
              state != .stopped else { return }
        state = .stoppingAuthorization
        lifecycleGeneration &+= 1
        diagnostic("linux cloud: device credential refused; Relay owner stopped without reconnect")
        await stopStreamsAndTransport()
        state = .unauthorized
    }

    func publish(_ bytes: Data, channel: String, logicalID: String) async throws {
        guard state == .running else { throw CloudDurableOutboundError.stopped }
        try await outbound.enqueue(bytes, channel: channel, logicalID: logicalID)
    }

    private func ready(_ generation: UInt64, lifecycleGeneration owned: UInt64) async {
        guard lifecycleGeneration == owned, state == .starting || state == .running else { return }
        do {
            let material = try identityAuthority.transportMaterial()
            guard material.binding.accountID == machine.accountID,
                  material.binding.machineID == machine.machineID,
                  material.binding.deviceID == machine.machineID else {
                throw CloudExecutorIdentityError.identityMismatch
            }
            await outbound.replaceIdentity(LinuxDurableCloudRuntime.appIdentity(material))
            await outbound.requestDrain(reconnect: true)
            authenticatedGeneration = generation
            if !places.isEmpty {
                try await publishDescriptor()
                await publishInventory(inventory(), force: true)
            }
            diagnostic("linux cloud: authenticated Relay ready generation=\(generation)")
        } catch {
            diagnostic("linux cloud: protected identity changed incompatibly; Relay stopped")
            // This callback is running inside `readyTask`. Scheduling the terminal transition on
            // a fresh task lets this consumer return before stop joins the three stream tasks.
            Task { [weak self] in await self?.authorizationRefused() }
        }
    }

    private func settle(
        _ receipt: CloudOutboundTransportReceipt, lifecycleGeneration owned: UInt64
    ) async {
        guard lifecycleGeneration == owned, state == .starting || state == .running else { return }
        do { try await outbound.settle(receipt) }
        catch { diagnostic("linux cloud: correlated outbound receipt could not settle durably") }
    }

    private func consume(_ inbound: CloudInboundCommand, generation owned: UInt64) async {
        guard lifecycleGeneration == owned, state == .starting || state == .running else { return }
        let wantedChannel = "ctl/" + channelSegment(machine.machineID)
        guard inbound.channel == wantedChannel else {
            diagnostic("linux cloud: authenticated command refused code=wrong_machine")
            return
        }
        let now = UInt64(max(0, Date().timeIntervalSince1970 * 1_000))
        guard inbound.timestamp <= now + 60_000, now <= inbound.timestamp + 300_000 else {
            diagnostic("linux cloud: authenticated command refused code=command_expired")
            return
        }
        let command: LinuxBrowserCommand
        do { command = try adaptBrowserCommand(inbound) }
        catch let failure as LinuxDurableStateFailure {
            diagnostic("linux cloud: authenticated command refused code=\(failure.code)")
            if let refusal = browserRefusalIdentity(inbound.plaintext) {
                try? await outbound.enqueue(
                    try refusalPayload(read: refusal.read, status: Self.status(for: failure.code),
                                       code: failure.code, message: failure.message),
                    channel: refusal.channel, logicalID: refusal.read)
            }
            return
        } catch {
            diagnostic("linux cloud: authenticated command refused code=malformed_command")
            return
        }
        switch command {
        case .places(let requestID):
            do {
                guard try isAuthorized(sender: inbound.sender, requiresWrite: false) else {
                    throw LinuxDurableStateFailure(code: "cloud_read_refused", message: "Paired read refused.")
                }
                try await outbound.enqueue(
                    try placesPayload(requestID: requestID), channel: machineReplyChannel(),
                    logicalID: "read:" + requestID)
            } catch {
                try? await outbound.enqueue(
                    try refusalPayload(read: "read:" + requestID, status: 403,
                                       code: "cloud_read_refused",
                                       message: "The paired read is not authorized."),
                    channel: machineReplyChannel(), logicalID: "read:" + requestID)
                diagnostic("linux cloud: authenticated places read refused")
            }
            return
        case .start(let requestID, let request):
            do {
                let response = try ingress.performCloud(
                    request, sender: inbound.sender, sequence: inbound.sequence,
                    effectAuthorization: { [identityAuthority, commandsEnabled, machine] write in
                        let snapshot = try identityAuthority.snapshot()
                        guard snapshot.accountID == machine.accountID,
                              snapshot.machineID == machine.machineID,
                              snapshot.deviceID == machine.machineID,
                              !snapshot.revokedDeviceIDs.contains(inbound.sender),
                              snapshot.pairedDevices.contains(where: {
                                  $0.deviceID == inbound.sender
                              }) else { return false }
                        return !write || commandsEnabled()
                    })
                try await outbound.enqueue(
                    try actionPayload(response: response, requestID: requestID, request: request),
                    channel: machineReplyChannel(), logicalID: "action:" + requestID)
                await publishInventory(inventory(), force: true)
            } catch let failure as LinuxDurableStateFailure {
                try? await outbound.enqueue(
                    try refusalPayload(read: "action:" + requestID,
                                       status: Self.status(for: failure.code),
                                       code: failure.code, message: failure.message),
                    channel: machineReplyChannel(), logicalID: "action:" + requestID)
                diagnostic("linux cloud: command refused code=\(failure.code)")
            } catch {
                try? await outbound.enqueue(
                    try refusalPayload(read: "action:" + requestID, status: 500,
                                       code: "internal_failure",
                                       message: "The command could not be completed."),
                    channel: machineReplyChannel(), logicalID: "action:" + requestID)
                diagnostic("linux cloud: command failed before durable response")
            }
            return
        case .transcript(let sessionID):
            await performBrowserSession(
                requestID: browserReadCommandID(inbound), sessionID: sessionID, operation: .observe,
                text: nil, sender: inbound.sender, sequence: inbound.sequence,
                readName: "transcript"
            ) { receipt in
                let text = TerminalSessionPresentation.plain(receipt.output ?? "")
                return ["entries": text.isEmpty ? [] : [["role": "assistant", "text": text]],
                        "signature": LinuxSHA256.hex(Data(text.utf8))]
            }
            return
        case .screen(let sessionID):
            await performBrowserScreen(sessionID: sessionID, sender: inbound.sender)
            return
        case .info(let sessionID, let parts):
            await performBrowserInfo(sessionID: sessionID, parts: parts, sender: inbound.sender)
            return
        case .send(let requestID, let sessionID, let text):
            await performBrowserSession(
                requestID: requestID, sessionID: sessionID, operation: .send,
                text: text, sender: inbound.sender, sequence: inbound.sequence,
                readName: "action:" + requestID
            ) { _ in
                let now = Int(Date().timeIntervalSince1970 * 1_000)
                return ["ok": true, "accepted_at": now, "at": now,
                        "optimistic_settlement": "action_receipt"]
            }
            await publishInventory(inventory(), force: true)
            return
        case .native(let request):
            do {
                let response = try ingress.performCloud(
                    request, sender: inbound.sender, sequence: inbound.sequence,
                    effectAuthorization: { [identityAuthority, commandsEnabled, machine] write in
                        let snapshot = try identityAuthority.snapshot()
                        guard snapshot.accountID == machine.accountID,
                              snapshot.machineID == machine.machineID,
                              snapshot.deviceID == machine.machineID,
                              !snapshot.revokedDeviceIDs.contains(inbound.sender),
                              snapshot.pairedDevices.contains(where: {
                                  $0.deviceID == inbound.sender
                              }) else { return false }
                        return !write || commandsEnabled()
                    })
                let session = request.sessionID ?? request.taskID
                try await outbound.enqueue(
                    response,
                    channel: "t/" + channelSegment(machine.machineID) + "/" + channelSegment(session),
                    logicalID: inbound.idempotencyKey)
                if request.operation == .create || request.operation == .send
                    || request.operation == .close {
                    await publishInventory(inventory(), force: true)
                }
            } catch let failure as LinuxDurableStateFailure {
                diagnostic("linux cloud: command refused code=\(failure.code)")
            } catch {
                diagnostic("linux cloud: command failed before durable response")
            }
            return
        }
    }

    private func performBrowserSession(
        requestID: String, sessionID: String, operation: LinuxIngressOperation,
        text: String?, sender: String, sequence: UInt64, readName: String,
        body: (LinuxLifecycleReceipt) -> [String: Any]
    ) async {
        let channel = "t/" + channelSegment(machine.machineID) + "/" + channelSegment(sessionID)
        do {
            guard try isAuthorized(sender: sender, requiresWrite: operation == .send) else {
                throw LinuxDurableStateFailure(
                    code: operation == .send ? "cloud_effect_refused" : "cloud_read_refused",
                    message: "The paired viewer is not authorized for this Session operation.")
            }
            let authorize: (_ requiresWrite: Bool) throws -> Bool = {
                [identityAuthority, commandsEnabled, machine] write in
                    let snapshot = try identityAuthority.snapshot()
                    guard snapshot.accountID == machine.accountID,
                          snapshot.machineID == machine.machineID,
                          snapshot.deviceID == machine.machineID,
                          !snapshot.revokedDeviceIDs.contains(sender),
                          snapshot.pairedDevices.contains(where: { $0.deviceID == sender }) else {
                        return false
                    }
                    return !write || commandsEnabled()
            }
            let receipt: LinuxLifecycleReceipt
            if operation == .observe {
                receipt = try ingress.observeCloudSession(
                    sessionID, commandID: requestID,
                    effectAuthorization: { try authorize(false) })
            } else {
                let taskID = try ingress.taskIDForCloudSession(sessionID)
                let response = try ingress.performCloud(
                    LinuxIngressRequest(
                        operation: operation, commandID: requestID, taskID: taskID,
                        sessionID: sessionID, text: text),
                    sender: sender, sequence: sequence,
                    effectAuthorization: authorize)
                let decoded = try JSONDecoder().decode(LinuxIngressResponse.self, from: response)
                guard let value = decoded.receipt else {
                    throw LinuxDurableStateFailure(code: "missing_receipt",
                                                   message: "The Session operation produced no receipt.")
                }
                receipt = value
            }
            try await outbound.enqueue(
                try Self.json(["read": readName, "status": 200, "body": body(receipt)]),
                channel: channel, logicalID: readName)
        } catch let failure as LinuxDurableStateFailure {
            try? await outbound.enqueue(
                try refusalPayload(read: readName, status: Self.status(for: failure.code),
                                   code: failure.code, message: failure.message),
                channel: channel, logicalID: readName)
            diagnostic("linux cloud: Session command refused code=\(failure.code)")
        } catch {
            try? await outbound.enqueue(
                try refusalPayload(read: readName, status: 500, code: "internal_failure",
                                   message: "The Session operation could not be completed."),
                channel: channel, logicalID: readName)
            diagnostic("linux cloud: Session command failed before durable response")
        }
    }

    /// The terminal renderer is the sole styled capture consumer. Durable/native `.observe`
    /// continues through `TerminalHost.capture` as plain text; this read revalidates viewer and
    /// terminal incarnation on both sides of the bounded styled capture.
    private func performBrowserScreen(sessionID: String, sender: String) async {
        let readName = "screen"
        let channel = "t/" + channelSegment(machine.machineID) + "/" + channelSegment(sessionID)
        do {
            guard try isAuthorized(sender: sender, requiresWrite: false) else {
                throw LinuxDurableStateFailure(
                    code: "cloud_read_refused",
                    message: "The paired viewer is not authorized to read this Session.")
            }
            let authorize: () throws -> Bool = { [identityAuthority, machine] in
                let snapshot = try identityAuthority.snapshot()
                return snapshot.accountID == machine.accountID
                    && snapshot.machineID == machine.machineID
                    && snapshot.deviceID == machine.machineID
                    && !snapshot.revokedDeviceIDs.contains(sender)
                    && snapshot.pairedDevices.contains(where: { $0.deviceID == sender })
            }
            let before = try ingress.infoForCloudSession(sessionID, effectAuthorization: authorize)
            guard let text = try await screenCapture(before) else {
                throw LinuxDurableStateFailure(
                    code: "session_observe_failed", message: "The PTY could not be captured.")
            }
            let after = try ingress.infoForCloudSession(sessionID, effectAuthorization: authorize)
            guard before == after else {
                throw LinuxDurableStateFailure(
                    code: "session_identity_incomplete",
                    message: "The Linux Session identity changed during capture.")
            }
            let body: [String: Any] = ["screen": [
                "id": sessionID, "backend": "tmux", "channel": "on-demand",
                "revision": LinuxSHA256.hex(Data(text.utf8)), "readable": true,
                "pending": false, "text": text,
                "lines": TerminalSessionPresentation.lineCount(text),
                "askAgainAfterMs": 1_000,
            ]]
            try await outbound.enqueue(
                try Self.json(["read": readName, "status": 200, "body": body]),
                channel: channel, logicalID: readName)
        } catch let failure as LinuxDurableStateFailure {
            try? await outbound.enqueue(
                try refusalPayload(read: readName, status: Self.status(for: failure.code),
                                   code: failure.code, message: failure.message),
                channel: channel, logicalID: readName)
            diagnostic("linux cloud: Session screen refused code=\(failure.code)")
        } catch {
            try? await outbound.enqueue(
                try refusalPayload(read: readName, status: 500, code: "internal_failure",
                                   message: "The Session screen could not be read."),
                channel: channel, logicalID: readName)
            diagnostic("linux cloud: Session screen failed before durable response")
        }
    }

    /// Answer the hosted console's Info card without pretending the Linux daemon owns the Mac's
    /// transcript-derived usage, provider quota, Git or deploy readers. Viewer membership is
    /// checked first, then the durable task-terminal edge proves that an unrelated tmux pane in
    /// the current inventory cannot be read by naming its id. The terminal inventory contributes
    /// only display metadata already published in the Session row; cwd crosses only when it is an
    /// exact configured place.
    private func performBrowserInfo(sessionID: String, parts: String, sender: String) async {
        let readName = "info." + parts
        let channel = "t/" + channelSegment(machine.machineID) + "/" + channelSegment(sessionID)
        do {
            guard try isAuthorized(sender: sender, requiresWrite: false) else {
                throw LinuxDurableStateFailure(
                    code: "cloud_read_refused",
                    message: "The paired viewer is not authorized to read this Session.")
            }
            let row = try ingress.infoForCloudSession(sessionID) {
                try isAuthorized(sender: sender, requiresWrite: false)
            }
            var session: [String: Any] = ["id": row.id, "title": row.name]
            if let assistant = row.assistant { session["assistant"] = assistant.rawValue }
            if let cwd = row.cwd, places.contains(where: { $0.path == cwd }) {
                session["cwd"] = cwd
            }
            var info: [String: Any] = [
                "session": session,
                "limits": ["windows": []],
                "models": [],
            ]
            if parts == "full" {
                info["deploy"] = []
                info["links"] = []
            }
            try await outbound.enqueue(
                try Self.json(["read": readName, "status": 200, "body": ["info": info]]),
                channel: channel, logicalID: readName)
        } catch let failure as LinuxDurableStateFailure {
            try? await outbound.enqueue(
                try refusalPayload(read: readName, status: Self.status(for: failure.code),
                                   code: failure.code, message: failure.message),
                channel: channel, logicalID: readName)
            diagnostic("linux cloud: Session Info refused code=\(failure.code)")
        } catch {
            try? await outbound.enqueue(
                try refusalPayload(read: readName, status: 500, code: "internal_failure",
                                   message: "The Session Info could not be read."),
                channel: channel, logicalID: readName)
            diagnostic("linux cloud: Session Info failed before durable response")
        }
    }

    private func browserReadCommandID(_ inbound: CloudInboundCommand) -> String {
        LinuxSHA256.hex(Data(
            "browser-read:\(inbound.sender):\(inbound.channel):\(inbound.sequence)".utf8))
    }

    func publishInventory(_ snapshot: TerminalInventory, force: Bool = false) async {
        guard state == .running, !places.isEmpty else { return }
        if inventoryPublicationInFlight {
            let inheritedForce = pendingInventoryPublication?.force ?? false
            pendingInventoryPublication = (snapshot, force || inheritedForce)
            return
        }
        inventoryPublicationInFlight = true
        defer { inventoryPublicationInFlight = false }
        var next: (snapshot: TerminalInventory, force: Bool)? = (snapshot, force)
        while let current = next {
            pendingInventoryPublication = nil
            // Production supplies this async closure from a detached terminal sampler. The actor
            // remains available to commands while tmux spends up to its three-second bound.
            let observed = await observations(current.snapshot)
            await publishInventory(
                current.snapshot, observed: observed, force: current.force)
            next = pendingInventoryPublication
        }
    }

    private func publishInventory(
        _ snapshot: TerminalInventory,
        observed: [String: TerminalSessionPresentation.Observation],
        force: Bool
    ) async {
        guard state == .running, !places.isEmpty else { return }
        await publishDescriptorIfDue()
        let allowed = Set(places.map(\.path))
        var rowsByID: [String: TargetSession] = [:]
        for row in snapshot.assistantSessions where rowsByID[row.id] == nil {
            rowsByID[row.id] = row
        }
        let complete = snapshot.isComplete
        let retainedIDs: Set<String>
        if complete {
            retainedIDs = Set(rowsByID.keys)
            guard retainedIDs.count <= 512 else { return }
        } else {
            let union = Set(rowsByID.keys).union(publishedInventoryRows.keys)
            // A churned partial scan can exceed the publication ceiling even though the last
            // accepted roster did not. Preserve and downgrade that roster instead of returning
            // early and leaving its old working/waiting claims visible.
            retainedIDs = union.count <= 512 ? union : Set(publishedInventoryRows.keys)
        }
        var encodedRows: [String: Data] = [:]
        do {
            for id in retainedIDs.sorted() {
                let presentation = complete ? (observed[id] ?? .unknown) : .unknown
                if let row = rowsByID[id] {
                    var session: [String: Any] = [
                        "id": row.id, "name": row.name, "label": row.name,
                        "assistant": row.assistant?.rawValue ?? "", "backend": "tmux",
                        "window": row.windowIndex, "tab": row.tabIndex,
                        "tty": row.tty.replacingOccurrences(of: "/dev/", with: ""),
                        "isClaude": row.isClaude,
                        "state": presentation.state, "work_state": presentation.workState,
                    ]
                    if let line = presentation.line { session["line"] = line }
                    if let cwd = row.cwd, allowed.contains(cwd) { session["cwd"] = cwd }
                    encodedRows[id] = try Self.json(["session": session])
                } else if let prior = publishedInventoryRows[id],
                          var envelope = try JSONSerialization.jsonObject(with: prior)
                            as? [String: Any],
                          var session = envelope["session"] as? [String: Any] {
                    session["state"] = TerminalSessionPresentation.Observation.unknown.state
                    session["work_state"] = TerminalSessionPresentation.Observation.unknown.workState
                    session.removeValue(forKey: "line")
                    envelope["session"] = session
                    encodedRows[id] = try Self.json(envelope)
                }
            }
        } catch { diagnostic("linux cloud: inventory encoding failed"); return }
        let digest = encodedRows.keys.sorted().map { id in
            id + ":" + LinuxSHA256.hex(encodedRows[id] ?? Data())
        }.joined(separator: "\n")
        guard force || digest != lastInventoryDigest else { return }
        do {
            for id in encodedRows.keys.sorted() {
                guard force || publishedInventoryRows[id] != encodedRows[id] else { continue }
                try await outbound.enqueue(
                    encodedRows[id]!,
                    channel: "s/" + channelSegment(machine.machineID) + "/" + channelSegment(id),
                    logicalID: "linux-session:" + id)
            }
            for id in Set(publishedInventoryRows.keys).subtracting(encodedRows.keys).sorted() {
                try await outbound.enqueue(
                    try Self.json(["deleted": true]),
                    channel: "s/" + channelSegment(machine.machineID) + "/" + channelSegment(id),
                    logicalID: "linux-session:" + id)
            }
            try await outbound.enqueue(
                try Self.json(["inventory": ["version": 1,
                                              "sessions": encodedRows.keys.sorted()]]),
                channel: "s/" + channelSegment(machine.machineID) + "/"
                    + channelSegment("__clawdline_inventory_v1__"),
                logicalID: "linux-session-inventory")
            publishedInventoryRows = encodedRows
            lastInventoryDigest = digest
        } catch { diagnostic("linux cloud: inventory publication failed") }
    }

    private enum LinuxBrowserCommand {
        case places(String)
        case start(String, LinuxIngressRequest)
        case transcript(String)
        case screen(String)
        case info(String, String)
        case send(String, String, String)
        case native(LinuxIngressRequest)
    }

    private func adaptBrowserCommand(_ inbound: CloudInboundCommand) throws -> LinuxBrowserCommand {
        if let native = try? JSONDecoder().decode(LinuxIngressRequest.self, from: inbound.plaintext) {
            return .native(native)
        }
        guard let body = try JSONSerialization.jsonObject(with: inbound.plaintext) as? [String: Any],
              let type = body["type"] as? String else {
            throw LinuxDurableStateFailure(code: "malformed_command", message: "Browser command is malformed.")
        }
        // A word this executor does not implement. It used to fall through to the `start` shape check
        // and be dropped as malformed with no reply, so a browser asking for snippets, schedules,
        // push or the Board waited out its whole read timeout. `browserRefusalIdentity` answers it.
        guard Self.browserCommandTypes.contains(type) else {
            throw LinuxDurableStateFailure(code: "unknown_command",
                                           message: "This machine does not know that Cloud command.")
        }
        if type == "transcript" {
            guard Set(body.keys) == ["type", "session", "limit", "priority"],
                  let session = body["session"] as? String,
                  let limit = body["limit"] as? Int, (1...200).contains(limit),
                  let priority = body["priority"] as? String,
                  ["foreground", "background"].contains(priority) else {
                throw LinuxDurableStateFailure(code: "malformed_read",
                                               message: "Transcript command is malformed.")
            }
            return .transcript(session)
        }
        if type == "screen" {
            guard Set(body.keys) == ["type", "session"],
                  let session = body["session"] as? String else {
                throw LinuxDurableStateFailure(code: "malformed_command",
                                               message: "Screen command is malformed.")
            }
            return .screen(session)
        }
        if type == "info" {
            guard Set(body.keys) == ["type", "session", "parts"],
                  let session = body["session"] as? String, !session.isEmpty,
                  let parts = body["parts"] as? String,
                  parts == "full" || parts == "summary" else {
                throw LinuxDurableStateFailure(code: "malformed_read",
                                               message: "Info command is malformed.")
            }
            return .info(session, parts)
        }
        guard let request = body["request"] as? String,
              request.utf8.count > 0, request.utf8.count <= 128,
              SessionLaunchPolicy.opaqueCommandID(request) == request else {
            throw LinuxDurableStateFailure(code: "malformed_command", message: "Browser command is malformed.")
        }
        if type == "places" {
            guard Set(body.keys) == ["type", "session", "request"],
                  body["session"] as? String == "__clawdline_machine__" else {
                throw LinuxDurableStateFailure(code: "malformed_command", message: "Places command is malformed.")
            }
            return .places(request)
        }
        if type == "send" {
            guard Set(body.keys) == ["type", "session", "request", "text", "images"],
                  let session = body["session"] as? String,
                  let text = body["text"] as? String,
                  !text.isEmpty, text.utf8.count <= 65_536,
                  let images = body["images"] as? [Any], images.isEmpty else {
                throw LinuxDurableStateFailure(code: "malformed_command",
                                               message: "Send command is malformed or carries unsupported images.")
            }
            return .send(request, session, text)
        }
        guard type == "start",
              Set(body.keys) == ["type", "session", "request", "place", "assistant", "model"],
              body["session"] as? String == "__clawdline_machine__",
              let placeID = body["place"] as? String,
              let assistantName = body["assistant"] as? String,
              let model = body["model"] as? String else {
            throw LinuxDurableStateFailure(code: "malformed_command", message: "Start command is malformed.")
        }
        guard let place = places.first(where: { $0.id == placeID }) else {
            throw LinuxDurableStateFailure(code: "place_not_found", message: "No configured place has that id.")
        }
        let selectedAssistant: Assistant? = assistantName.isEmpty
            ? .claude : Assistant(rawValue: assistantName)
        guard let assistant = selectedAssistant else {
            throw LinuxDurableStateFailure(code: "assistant_not_found", message: "No assistant has that id.")
        }
        guard model.isEmpty || ["haiku", "sonnet", "opus"].contains(model) else {
            throw LinuxDurableStateFailure(code: "model_not_found", message: "No model has that id.")
        }
        let id = LinuxSHA256.hex(Data("browser:\(inbound.sender):\(inbound.sequence)".utf8))
        return .start(request, LinuxIngressRequest(
            operation: .create, commandID: id, taskID: id,
            projectRoot: place.path, assistant: assistant,
            model: model.isEmpty ? nil : model))
    }

    private func isAuthorized(sender: String, requiresWrite: Bool) throws -> Bool {
        let snapshot = try identityAuthority.snapshot()
        guard snapshot.accountID == machine.accountID,
              snapshot.machineID == machine.machineID,
              snapshot.deviceID == machine.machineID,
              !snapshot.revokedDeviceIDs.contains(sender),
              snapshot.pairedDevices.contains(where: { $0.deviceID == sender }) else { return false }
        return !requiresWrite || commandsEnabled()
    }

    private func publishDescriptor() async throws {
        var descriptor: [String: Any] = ["name": presentation.displayName,
                                         "platform": "linux",
                                         "commands": Self.browserCommandTypes]
        if let provider = presentation.provider { descriptor["provider"] = provider }
        let payload: [String: Any] = ["v": 1, "machine": descriptor,
                                      "at": Int(Date().timeIntervalSince1970)]
        try await outbound.enqueue(
            try Self.json(payload), channel: "orch/" + channelSegment(machine.machineID),
            logicalID: "linux-machine-descriptor")
        let publishedAt = monotonicNow()
        if publishedAt.isFinite { lastDescriptorPublishedAt = publishedAt }
    }

    /// A heartbeat is only eligible after an authenticated ready generation and while the sole
    /// Relay owner remains running. Delivery still travels through the signed durable outbound
    /// path, so a queued frame cannot make an offline machine appear present until Relay actually
    /// authenticates and forwards it.
    private func publishDescriptorIfDue() async {
        guard authenticatedGeneration > 0 else { return }
        let now = monotonicNow()
        guard now.isFinite else { return }
        if let last = lastDescriptorPublishedAt,
           now >= last,
           now - last < Self.descriptorHeartbeatIntervalSeconds {
            return
        }
        do {
            try await publishDescriptor()
        } catch {
            diagnostic("linux cloud: machine presence heartbeat publication failed")
        }
    }

    private func placesPayload(requestID: String) throws -> Data {
        try Self.json(["read": "read:" + requestID, "status": 200, "body": [
            "places": places.map { ["id": $0.id, "label": $0.label, "path": $0.path] },
            "assistants": Assistant.allCases.map {
                ["id": $0.rawValue, "label": $0.rawValue, "availability": "available"]
            }
        ]])
    }

    private func actionPayload(
        response: Data, requestID: String, request: LinuxIngressRequest
    ) throws -> Data {
        let decoded = try JSONDecoder().decode(LinuxIngressResponse.self, from: response)
        guard let receipt = decoded.receipt, let id = receipt.sessionID else {
            throw LinuxDurableStateFailure(code: "missing_receipt", message: "Start receipt is incomplete.")
        }
        return try Self.json(["read": "action:" + requestID, "status": 200, "body": [
            "ok": true, "id": id, "tty": receipt.tty ?? "",
            "attach": receipt.attachCommand ?? "", "backend": "tmux",
            "assistant": request.assistant ?? Assistant.claude.rawValue,
            "model": request.model ?? "",
            "place": places.first(where: { $0.path == request.projectRoot })?.id ?? "",
            "cwd": request.projectRoot ?? ""
        ]])
    }

    /// Where a refused machine-session command is answered: only one that names the machine reply
    /// session and a well-formed request id, whatever its word. `places` is a read. Every other word
    /// is answered `action:` — `start` because it is one, and a word this executor does not implement
    /// because it cannot know whether the browser waits on `read:` or `action:`; that is the Mac's
    /// `commandRefusalReply` rule, and the hosted console lets a refusal settle either spelling of
    /// the same request id.
    private func browserRefusalIdentity(_ plaintext: Data)
        -> (read: String, type: String, channel: String)? {
        guard let body = try? JSONSerialization.jsonObject(with: plaintext) as? [String: Any],
              let type = body["type"] as? String,
              let session = body["session"] as? String,
              !session.isEmpty else { return nil }
        let channel = "t/" + channelSegment(machine.machineID) + "/" + channelSegment(session)
        if type == "transcript" || type == "screen" {
            return (type, type, channel)
        }
        if type == "info", let parts = body["parts"] as? String,
           parts == "full" || parts == "summary" {
            return ("info." + parts, type, channel)
        }
        guard let request = body["request"] as? String,
              SessionLaunchPolicy.opaqueCommandID(request) == request else { return nil }
        if session == "__clawdline_machine__" {
            return ((type == "places" ? "read:" : "action:") + request, type, channel)
        }
        guard type == "send" else { return nil }
        return ("action:" + request, type, channel)
    }

    private func refusalPayload(read: String, status: Int, code: String,
                                message: String) throws -> Data {
        try Self.json(["read": read, "status": status,
                       "error": ["code": code, "message": message]])
    }

    private static func status(for code: String) -> Int {
        if code.contains("unauthorized") || code.contains("refused") { return 403 }
        if code.contains("not_found") { return 404 }
        if code == "ingress_closed" || code == "inventory_incomplete"
            || code == "session_identity_incomplete" || code == "session_observe_failed" {
            return 503
        }
        return 400
    }

    private func machineReplyChannel() -> String {
        "t/" + channelSegment(machine.machineID) + "/__clawdline_machine__"
    }

    private static func json(_ value: Any) throws -> Data {
        try JSONSerialization.data(withJSONObject: value, options: [.sortedKeys, .withoutEscapingSlashes])
    }

    private func stopStreamsAndTransport() async {
        guard !transportStopped else { return }
        transportStopped = true
        let command = commandTask
        let ready = readyTask
        let receipts = receiptTask
        commandTask = nil
        readyTask = nil
        receiptTask = nil
        command?.cancel()
        ready?.cancel()
        receipts?.cancel()
        await transport.setTerminalAuthorizationHandler(nil)
        await outbound.beginStop()
        await transport.shutdown()
        await command?.value
        await ready?.value
        await receipts?.value
        await outbound.finishStop()
    }

    private func channelSegment(_ value: String) -> String {
        var allowed = CharacterSet.alphanumerics
        allowed.insert(charactersIn: "-_.!~*'()")
        return value.addingPercentEncoding(withAllowedCharacters: allowed) ?? ""
    }

}
