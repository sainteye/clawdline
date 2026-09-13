import Foundation
import ClawdlineApplication

/// Ubuntu's production ownership leaf for the shared Application ledger, spool, account client
/// and protected executor identity. It exposes bootstrap but no publish door: W0-E remains a
/// candidate until the separate contract/cutover authority says otherwise.
final class LinuxDurableCloudRuntime {
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
    case stoppingAuthorization
    case unauthorized
    case failed
    case stopped
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
    let status: LinuxRelayRuntimeStatus

    init(owner: LinuxRelayRuntimeOwner, status: LinuxRelayRuntimeStatus,
         diagnostic: @escaping @Sendable (String) -> Void) {
        self.owner = owner
        self.status = status
        self.diagnostic = diagnostic
    }

    func start() {
        lock.lock()
        guard startTask == nil else { lock.unlock(); return }
        let owner = owner
        let diagnostic = diagnostic
        startTask = Task {
            do { try await owner.start() }
            catch is CancellationError { diagnostic("linux cloud: Relay start cancelled") }
            catch { diagnostic("linux cloud: Relay start failed code=relay_start_failed") }
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
}

/// Exactly one instance is composed by `LinuxDaemonService`. It owns the transport generation,
/// the shared durable outbound drain and the three bounded receive streams; every command effect
/// still enters `LinuxDaemonIngressOwner`, whose lock and ledger remain the sole Linux authority.
actor LinuxRelayRuntimeOwner {
    private let machine: CloudMachineIdentity
    private let identityAuthority: CloudExecutorIdentityAuthority
    private let transport: any CloudTransporting
    private let outbound: CloudDurableOutboundComposition
    private let ingress: LinuxDaemonIngressOwner
    private let commandsEnabled: @Sendable () -> Bool
    private let diagnostic: @Sendable (String) -> Void
    private let stateObserver: @Sendable (LinuxRelayRuntimeState) -> Void
    private var commandTask: Task<Void, Never>?
    private var readyTask: Task<Void, Never>?
    private var receiptTask: Task<Void, Never>?
    private var transportStopped = false
    private(set) var state: LinuxRelayRuntimeState = .idle {
        didSet { stateObserver(state) }
    }
    private(set) var authenticatedGeneration: UInt64 = 0
    private var lifecycleGeneration: UInt64 = 0

    init(
        machine: CloudMachineIdentity,
        identityAuthority: CloudExecutorIdentityAuthority,
        transport: any CloudTransporting,
        outbound: CloudDurableOutboundComposition,
        ingress: LinuxDaemonIngressOwner,
        commandsEnabled: @escaping @Sendable () -> Bool,
        diagnostic: @escaping @Sendable (String) -> Void = { _ in },
        stateObserver: @escaping @Sendable (LinuxRelayRuntimeState) -> Void = { _ in }
    ) {
        self.machine = machine
        self.identityAuthority = identityAuthority
        self.transport = transport
        self.outbound = outbound
        self.ingress = ingress
        self.commandsEnabled = commandsEnabled
        self.diagnostic = diagnostic
        self.stateObserver = stateObserver
    }

    func start() async throws {
        guard state == .idle else { throw CloudTransportError.alreadyConnected }
        state = .starting
        await transport.setTerminalAuthorizationHandler { [weak self] _ in
            Task { await self?.authorizationRefused() }
        }
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
        do {
            try await transport.connect(role: .machine)
            guard lifecycleGeneration == owned, state == .starting else {
                throw CancellationError()
            }
            state = .running
        } catch {
            if lifecycleGeneration == owned, state == .starting { state = .failed }
            await stopStreamsAndTransport()
            throw error
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
        let request: LinuxIngressRequest
        do { request = try JSONDecoder().decode(LinuxIngressRequest.self, from: inbound.plaintext) }
        catch {
            diagnostic("linux cloud: authenticated command refused code=malformed_command")
            return
        }
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
        } catch let failure as LinuxDurableStateFailure {
            diagnostic("linux cloud: command refused code=\(failure.code)")
        } catch {
            diagnostic("linux cloud: command failed before durable response")
        }
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
