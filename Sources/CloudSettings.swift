import Foundation

struct CloudSettingsLoginAttempt: Sendable {
    let userCode: String
    let verificationCompleteURL: URL
    private let waiter: @Sendable (@escaping @Sendable (CloudDeviceLoginPollState) async -> Void) async throws -> CloudDeviceLoginPollState

    init(
        userCode: String,
        verificationCompleteURL: URL,
        wait: @escaping @Sendable (@escaping @Sendable (CloudDeviceLoginPollState) async -> Void) async throws -> CloudDeviceLoginPollState
    ) {
        self.userCode = userCode
        self.verificationCompleteURL = verificationCompleteURL
        waiter = wait
    }

    func wait(
        onState: @escaping @Sendable (CloudDeviceLoginPollState) async -> Void
    ) async throws -> CloudDeviceLoginPollState {
        try await waiter(onState)
    }
}

struct CloudSettingsServices {
    let restoredIdentity: @MainActor () throws -> CloudMachineIdentity?
    let startLogin: @Sendable (CloudMachineMetadata) async throws -> CloudSettingsLoginAttempt
    let signOut: @MainActor () throws -> Void
    let openVerificationURL: @MainActor (URL) -> Bool

    init(
        restoredIdentity: @escaping @MainActor () throws -> CloudMachineIdentity?,
        startLogin: @escaping @Sendable (CloudMachineMetadata) async throws -> CloudSettingsLoginAttempt,
        signOut: @escaping @MainActor () throws -> Void,
        openVerificationURL: @escaping @MainActor (URL) -> Bool
    ) {
        self.restoredIdentity = restoredIdentity
        self.startLogin = startLogin
        self.signOut = signOut
        self.openVerificationURL = openVerificationURL
    }

    static func production(
        client: CloudAccountClient = CloudAccountClient(),
        openVerificationURL: @escaping @MainActor (URL) -> Bool
    ) -> CloudSettingsServices {
        CloudSettingsServices(
            restoredIdentity: { try client.restoredMachineIdentity() },
            startLogin: { metadata in
                let started = try await client.startDeviceLogin(metadata: metadata)
                return CloudSettingsLoginAttempt(
                    userCode: started.userCode,
                    verificationCompleteURL: started.verificationCompleteURL,
                    wait: { onState in
                        try await client.waitForDeviceLogin(started, onState: onState)
                    })
            },
            signOut: { try client.signOut() },
            openVerificationURL: openVerificationURL)
    }
}

extension CloudMachineMetadata {
    static func currentMac(
        bundle: Bundle = .main,
        processInfo: ProcessInfo = .processInfo
    ) -> CloudMachineMetadata {
        let version = bundle.infoDictionary?["CFBundleShortVersionString"] as? String
        let displayName = Host.current().localizedName ?? processInfo.hostName
        return CloudMachineMetadata(
            name: displayName.isEmpty ? "Mac" : displayName,
            platform: "macos",
            appVersion: version)
    }
}

@MainActor
final class CloudSettingsModel {
    enum ConnectionOrigin: Equatable { case restored, completed }

    enum Phase: Equatable {
        case signedOut
        case starting
        case code(userCode: String)
        case waiting(userCode: String)
        case slowDown(userCode: String, retryAfter: Int)
        case connected(identity: CloudMachineIdentity, origin: ConnectionOrigin)
        case denied
        case expired
        case cancelled
        case failed(message: String)
        case signOutFailed(identity: CloudMachineIdentity, message: String)
    }

    private(set) var phase: Phase = .signedOut
    var onChange: (() -> Void)?

    private let services: CloudSettingsServices
    private let metadata: CloudMachineMetadata
    private var pendingAttempt: CloudSettingsLoginAttempt?
    private var operation: Task<Void, Never>?
    private var generation: UInt64 = 0

    init(services: CloudSettingsServices, metadata: CloudMachineMetadata) {
        self.services = services
        self.metadata = metadata
        do {
            if let identity = try services.restoredIdentity() {
                phase = .connected(identity: identity, origin: .restored)
            }
        } catch {
            phase = .failed(message: Self.message(for: error))
        }
    }

    deinit { operation?.cancel() }

    func connect() {
        replaceOperation(with: .starting)
        let wantedGeneration = generation
        let start = services.startLogin
        let metadata = metadata
        operation = Task { [weak self] in
            do {
                let attempt = try await start(metadata)
                try Task.checkCancellation()
                self?.accept(attempt, generation: wantedGeneration)
            } catch is CancellationError {
                return
            } catch {
                guard !Task.isCancelled else { return }
                self?.publishFailure(error, generation: wantedGeneration)
            }
        }
    }

    func retry() { connect() }

    /// The complete verification URL is opened only on this explicit confirmation. Merely
    /// entering the code phase never launches a browser and never starts device-code polling.
    func confirmAndOpen() {
        guard case .code(let userCode) = phase, let attempt = pendingAttempt else { return }
        guard services.openVerificationURL(attempt.verificationCompleteURL) else {
            pendingAttempt = nil
            setPhase(.failed(message: "Clawdline could not open the GitHub verification page."))
            return
        }

        setPhase(.waiting(userCode: userCode))
        let wantedGeneration = generation
        operation = Task { [weak self] in
            do {
                let terminal = try await attempt.wait { [weak self] state in
                    await self?.accept(state, userCode: userCode,
                                       generation: wantedGeneration)
                }
                try Task.checkCancellation()
                self?.accept(terminal, userCode: userCode, generation: wantedGeneration)
            } catch is CancellationError {
                return
            } catch {
                guard !Task.isCancelled else { return }
                self?.publishFailure(error, generation: wantedGeneration)
            }
        }
    }

    func cancel() {
        guard isConnecting else { return }
        invalidateOperation()
        setPhase(.cancelled)
    }

    func close() { cancel() }

    /// The signed-out phase is deliberately published only after the credential store confirms
    /// removal. On a Keychain failure the prior identity remains visible and the action is retryable.
    func signOut() {
        let identity: CloudMachineIdentity
        switch phase {
        case .connected(let current, _), .signOutFailed(let current, _):
            identity = current
        default:
            return
        }

        do {
            try services.signOut()
            invalidateOperation()
            setPhase(.signedOut)
        } catch {
            setPhase(.signOutFailed(identity: identity, message: Self.message(for: error)))
        }
    }

    private var isConnecting: Bool {
        switch phase {
        case .starting, .code, .waiting, .slowDown:
            return true
        default:
            return false
        }
    }

    private func replaceOperation(with phase: Phase) {
        invalidateOperation()
        setPhase(phase)
    }

    private func invalidateOperation() {
        generation &+= 1
        operation?.cancel()
        operation = nil
        pendingAttempt = nil
    }

    private func accept(_ attempt: CloudSettingsLoginAttempt, generation wanted: UInt64) {
        guard wanted == generation, !Task.isCancelled else { return }
        operation = nil
        pendingAttempt = attempt
        setPhase(.code(userCode: attempt.userCode))
    }

    private func accept(
        _ state: CloudDeviceLoginPollState,
        userCode: String,
        generation wanted: UInt64
    ) {
        guard wanted == generation, !Task.isCancelled else { return }
        switch state {
        case .authorizationPending:
            setPhase(.waiting(userCode: userCode))
        case .slowDown(let retryAfter):
            setPhase(.slowDown(userCode: userCode, retryAfter: retryAfter))
        case .accessDenied:
            finish(with: .denied)
        case .expired:
            finish(with: .expired)
        case .complete(let identity):
            finish(with: .connected(identity: identity, origin: .completed))
        }
    }

    private func finish(with phase: Phase) {
        operation = nil
        pendingAttempt = nil
        setPhase(phase)
    }

    private func publishFailure(_ error: Error, generation wanted: UInt64) {
        guard wanted == generation, !Task.isCancelled else { return }
        finish(with: .failed(message: Self.message(for: error)))
    }

    private static func message(for error: Error) -> String {
        if let localized = error as? LocalizedError,
           let description = localized.errorDescription, !description.isEmpty {
            return description
        }
        return error.localizedDescription
    }

    private func setPhase(_ next: Phase) {
        guard next != phase else { return }
        phase = next
        onChange?()
    }
}
