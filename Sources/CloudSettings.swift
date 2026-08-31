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
    let readRestoredIdentity: @MainActor (
        @escaping @MainActor (Result<CloudMachineIdentity?, Error>) -> Void
    ) -> Void
    let startLogin: @Sendable (CloudMachineMetadata) async throws -> CloudSettingsLoginAttempt
    /// Atomic and main-safe. The model calls this before cancelling any Task, closing the only
    /// gap in which an ignored cancellation could persist under the old epoch.
    let reserveCredentialInvalidation: @MainActor () -> CloudCredentialGeneration
    /// Removing the credential is a Keychain *write*, so it has the same shape as the identity
    /// read: hand it over, get an answer back on the main queue. There is deliberately no
    /// synchronous spelling of this — a `() throws -> Void` is callable straight from a button
    /// action, and that is exactly the call that froze Settings on a locked Keychain.
    let signOut: @MainActor (CloudCredentialGeneration,
        @escaping @MainActor (CloudKeychainWriteOutcome) -> Void
    ) -> Void
    /// Abandon any sign-in still in flight. It touches no Keychain and cannot block, which is
    /// why it is separate from `signOut`: it has to be safe to call from a button.
    let abandonPendingLogins: @MainActor (CloudCredentialGeneration,
        @escaping @MainActor (CloudKeychainWriteOutcome) -> Void
    ) -> Void
    let openVerificationURL: @MainActor (URL) -> Bool

    init(
        restoredIdentity: @escaping @MainActor () throws -> CloudMachineIdentity?,
        startLogin: @escaping @Sendable (CloudMachineMetadata) async throws -> CloudSettingsLoginAttempt,
        reserveCredentialInvalidation: @escaping @MainActor () -> CloudCredentialGeneration = {
            CloudCredentialGeneration(0)
        },
        signOut: @escaping @MainActor (CloudCredentialGeneration,
            @escaping @MainActor (CloudKeychainWriteOutcome) -> Void
        ) -> Void = { _, completion in completion(.succeeded) },
        abandonPendingLogins: @escaping @MainActor (CloudCredentialGeneration,
            @escaping @MainActor (CloudKeychainWriteOutcome) -> Void
        ) -> Void = { _, completion in completion(.succeeded) },
        openVerificationURL: @escaping @MainActor (URL) -> Bool
    ) {
        self.readRestoredIdentity = { completion in
            completion(Result { try restoredIdentity() })
        }
        self.startLogin = startLogin
        self.reserveCredentialInvalidation = reserveCredentialInvalidation
        self.signOut = signOut
        self.abandonPendingLogins = abandonPendingLogins
        self.openVerificationURL = openVerificationURL
    }

    static func production(
        client: CloudAccountClient = CloudAccountClient(),
        openVerificationURL: @escaping @MainActor (URL) -> Bool
    ) -> CloudSettingsServices {
        let identityReader = CloudKeychainReader<CloudMachineIdentity?>(
            label: "clawdline.cloud.settings-identity"
        ) {
            try client.restoredMachineIdentity()
        }
        return CloudSettingsServices(
            readRestoredIdentity: { completion in
                identityReader.read(
                    onTimeout: { seconds in
                        completion(.failure(CloudKeyError.operationTimedOut(seconds: seconds)))
                    },
                    completion: completion)
            },
            startLogin: { metadata in
                let started = try await client.startDeviceLogin(metadata: metadata)
                return CloudSettingsLoginAttempt(
                    userCode: started.userCode,
                    verificationCompleteURL: started.verificationCompleteURL,
                    wait: { onState in
                        try await client.waitForDeviceLogin(started, onState: onState)
                    })
            },
            reserveCredentialInvalidation: { client.reservePendingLoginInvalidation() },
            signOut: { invalidation, completion in
                CloudKeychainWriter(label: "clawdline.cloud.settings-sign-out") {
                    try client.signOut(reservedAt: invalidation)
                }.write(completion: completion)
            },
            abandonPendingLogins: { invalidation, completion in
                CloudKeychainWriter(label: "clawdline.cloud.settings-cancel-login") {
                    try client.persistPendingLoginInvalidation(invalidation)
                }.write(completion: completion)
            },
            openVerificationURL: openVerificationURL)
    }

    init(
        readRestoredIdentity: @escaping @MainActor (
            @escaping @MainActor (Result<CloudMachineIdentity?, Error>) -> Void
        ) -> Void,
        startLogin: @escaping @Sendable (CloudMachineMetadata) async throws -> CloudSettingsLoginAttempt,
        reserveCredentialInvalidation: @escaping @MainActor () -> CloudCredentialGeneration,
        signOut: @escaping @MainActor (CloudCredentialGeneration,
            @escaping @MainActor (CloudKeychainWriteOutcome) -> Void
        ) -> Void,
        abandonPendingLogins: @escaping @MainActor (CloudCredentialGeneration,
            @escaping @MainActor (CloudKeychainWriteOutcome) -> Void
        ) -> Void,
        openVerificationURL: @escaping @MainActor (URL) -> Bool
    ) {
        self.readRestoredIdentity = readRestoredIdentity
        self.startLogin = startLogin
        self.reserveCredentialInvalidation = reserveCredentialInvalidation
        self.signOut = signOut
        self.abandonPendingLogins = abandonPendingLogins
        self.openVerificationURL = openVerificationURL
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
    private enum DeferredSignOutFailure {
        case cleanup(message: String)
        case invalidation(CloudCredentialGeneration, message: String)
    }
    enum ConnectionOrigin: Equatable { case restored, completed }

    enum Phase: Equatable {
        case restoring
        case restorationReconciliation(message: String)
        case signedOut
        case starting
        case code(userCode: String)
        case waiting(userCode: String)
        case slowDown(userCode: String, retryAfter: Int)
        case connected(identity: CloudMachineIdentity, origin: ConnectionOrigin)
        /// The removal has been handed to the Keychain and has not answered yet. It is its own
        /// phase because the alternative is a button that looks idle while a locked Keychain
        /// decides — which reads as nothing having happened.
        case signingOut(identity: CloudMachineIdentity)
        /// The bound elapsed, but the synchronous Security mutation is still running. The
        /// process-local credential and bridge are already detached by reservation; durable
        /// invalidation/removal remain unknown until a retained terminal result reconciles them.
        /// Retry starts another removal without silencing the first.
        case signOutReconciliation(identity: CloudMachineIdentity, message: String)
        /// Durable invalidation succeeded, so the credential and bridge are already unusable;
        /// only stale Keychain bytes remain. Cleanup failure is visible and retryable without
        /// reconnecting the bridge.
        case signOutCleanupPending(identity: CloudMachineIdentity, message: String)
        case cleaningUpSignOut(identity: CloudMachineIdentity)
        /// The atomic process floor is already raised, so this process must detach. Persistence
        /// failed, however, and a restart could read the old durable epoch. Retry owns the exact
        /// reservation rather than claiming either connected or signed out.
        case signOutInvalidationPending(identity: CloudMachineIdentity, message: String)
        case denied
        case expired
        case cancelled
        case cancelling
        case cancellationReconciliation(message: String)
        case cancellationFailed(message: String)
        case failed(message: String)
        case signOutFailed(identity: CloudMachineIdentity, message: String)
    }

    private(set) var phase: Phase = .restoring
    var onChange: (() -> Void)?
    /// Set once, by the app, so that signing in or out takes effect without a relaunch.
    ///
    /// Deliberately not `onChange`: that one belongs to whichever window is on screen, and the
    /// cloud bridge has to follow this state whether or not Settings is open. It is a type
    /// property because the model is rebuilt with the window and the bridge is not.
    /// Carries the model's credential truth so production can detach directly on `false`
    /// instead of launching another identity read that may disagree with the reserved epoch.
    static var onConnectionChange: (@MainActor (Bool) -> Void)?

    private let services: CloudSettingsServices
    private let metadata: CloudMachineMetadata
    private var pendingAttempt: CloudSettingsLoginAttempt?
    private var operation: Task<Void, Never>?
    private var generation: UInt64 = 0
    private var nextSignOutAttempt: UInt64 = 0
    private var latestSignOutAttempt: UInt64 = 0
    private var activeSignOutAttempts: Set<UInt64> = []
    private var pendingSignOutInvalidation: CloudCredentialGeneration?
    private var deferredSignOutFailure: DeferredSignOutFailure?
    private var nextCancellationAttempt: UInt64 = 0
    private var latestCancellationAttempt: UInt64 = 0
    private var activeCancellationAttempts: Set<UInt64> = []
    private var pendingCancellationInvalidation: CloudCredentialGeneration?
    private var deferredCancellationFailureMessage: String?

    init(services: CloudSettingsServices, metadata: CloudMachineMetadata) {
        self.services = services
        self.metadata = metadata
        let wantedGeneration = generation
        services.readRestoredIdentity { [weak self] result in
            self?.acceptRestoredIdentity(result, generation: wantedGeneration)
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

    func retry() {
        switch phase {
        case .cancellationReconciliation, .cancellationFailed:
            startCancellationInvalidation()
        default:
            connect()
        }
    }

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
        let invalidation = services.reserveCredentialInvalidation()
        // Cancel the UI-owned Task at once, then persist the already-reserved invalidation off
        // main. The operation's terminal answer remains observed after a timeout.
        invalidateOperation()
        startCancellationInvalidation(invalidation)
    }

    func close() { cancel() }

    var connectedIdentity: CloudMachineIdentity? {
        switch phase {
        case .connected(let identity, _): return identity
        default: return nil
        }
    }

    /// The signed-out phase is deliberately published only after the credential store confirms
    /// removal. On a Keychain failure the prior identity remains visible and the action is retryable.
    ///
    /// Nothing here waits. The removal goes to a bounded writer and the answer comes back
    /// through ``acceptSignOut(_:identity:generation:)``, so a locked Keychain produces a
    /// sentence rather than a frozen window — and, because the writer is bounded, a sentence
    /// even when the Keychain never answers at all.
    func signOut() {
        let identity: CloudMachineIdentity
        let cleanupOnly: Bool
        switch phase {
        case .connected(let current, _), .signOutReconciliation(let current, _),
             .signOutFailed(let current, _):
            identity = current
            cleanupOnly = false
        case .signOutInvalidationPending(let current, _):
            identity = current
            cleanupOnly = false
        case .signOutCleanupPending(let current, _):
            identity = current
            cleanupOnly = true
        default:
            return
        }

        let invalidation = pendingSignOutInvalidation
            ?? services.reserveCredentialInvalidation()
        pendingSignOutInvalidation = nil
        invalidateOperation()
        nextSignOutAttempt &+= 1
        let attempt = nextSignOutAttempt
        latestSignOutAttempt = attempt
        activeSignOutAttempts.insert(attempt)
        if cleanupOnly {
            setPhase(.cleaningUpSignOut(identity: identity))
        } else {
            setPhase(.signingOut(identity: identity))
        }
        services.signOut(invalidation) { [weak self] outcome in
            self?.acceptSignOut(
                outcome, identity: identity, invalidation: invalidation,
                attempt: attempt, cleanupOnly: cleanupOnly)
        }
    }

    /// Every attempt addresses the same credential. A success from any still-observed attempt is
    /// therefore authoritative even after retry; discarding it would leave the model and bridge
    /// connected after the store had removed their credential.
    private func acceptSignOut(
        _ outcome: CloudKeychainWriteOutcome,
        identity: CloudMachineIdentity,
        invalidation: CloudCredentialGeneration,
        attempt: UInt64,
        cleanupOnly: Bool
    ) {
        guard activeSignOutAttempts.contains(attempt) else { return }
        switch outcome {
        case .succeeded:
            activeSignOutAttempts.removeAll()
            pendingSignOutInvalidation = nil
            deferredSignOutFailure = nil
            setPhase(.signedOut)
        case .timedOut:
            guard attempt == latestSignOutAttempt else { return }
            if cleanupOnly {
                setPhase(.signOutCleanupPending(identity: identity, message: outcome.message))
            } else {
                setPhase(.signOutReconciliation(identity: identity, message: outcome.message))
            }
        case .failed:
            activeSignOutAttempts.remove(attempt)
            if case .failed(let error) = outcome,
               case .credentialCleanupPending = error as? CloudAccountError {
                deferredSignOutFailure = .cleanup(message: outcome.message)
            } else if case .failed(let error) = outcome,
               case .credentialInvalidationFailed = error as? CloudAccountError {
                deferredSignOutFailure = .invalidation(
                    invalidation, message: outcome.message)
            } else if cleanupOnly {
                deferredSignOutFailure = .cleanup(message: outcome.message)
            }
            guard activeSignOutAttempts.isEmpty else {
                if attempt == latestSignOutAttempt {
                    setPhase(.signOutReconciliation(
                        identity: identity,
                        message: "A retry failed, but an earlier Keychain removal is still "
                            + "running; its terminal result remains authoritative."))
                }
                return
            }
            if publishDeferredSignOutFailure(identity: identity) { return }
            guard attempt == latestSignOutAttempt else { return }
            if activeSignOutAttempts.isEmpty {
                setPhase(.signOutFailed(identity: identity, message: outcome.message))
            }
        }
    }

    @discardableResult
    private func publishDeferredSignOutFailure(identity: CloudMachineIdentity) -> Bool {
        guard let deferredSignOutFailure else { return false }
        switch deferredSignOutFailure {
        case .cleanup(let message):
            pendingSignOutInvalidation = nil
            setPhase(.signOutCleanupPending(identity: identity, message: message))
        case .invalidation(let invalidation, let message):
            pendingSignOutInvalidation = invalidation
            setPhase(.signOutInvalidationPending(identity: identity, message: message))
        }
        return true
    }

    private func startCancellationInvalidation(
        _ reserved: CloudCredentialGeneration? = nil
    ) {
        let invalidation = reserved ?? pendingCancellationInvalidation
            ?? services.reserveCredentialInvalidation()
        pendingCancellationInvalidation = nil
        nextCancellationAttempt &+= 1
        let attempt = nextCancellationAttempt
        latestCancellationAttempt = attempt
        activeCancellationAttempts.insert(attempt)
        setPhase(.cancelling)
        services.abandonPendingLogins(invalidation) { [weak self] outcome in
            self?.acceptCancellationInvalidation(
                outcome, invalidation: invalidation, attempt: attempt)
        }
    }

    private func acceptCancellationInvalidation(
        _ outcome: CloudKeychainWriteOutcome,
        invalidation: CloudCredentialGeneration,
        attempt: UInt64
    ) {
        guard activeCancellationAttempts.contains(attempt) else { return }
        switch outcome {
        case .succeeded:
            activeCancellationAttempts.removeAll()
            pendingCancellationInvalidation = nil
            deferredCancellationFailureMessage = nil
            setPhase(.cancelled)
        case .timedOut:
            guard attempt == latestCancellationAttempt else { return }
            setPhase(.cancellationReconciliation(message: outcome.message))
        case .failed:
            activeCancellationAttempts.remove(attempt)
            if case .failed(let error) = outcome,
               case .credentialInvalidationFailed = error as? CloudAccountError {
                pendingCancellationInvalidation = invalidation
                deferredCancellationFailureMessage = outcome.message
            }
            guard activeCancellationAttempts.isEmpty else {
                if attempt == latestCancellationAttempt {
                    setPhase(.cancellationReconciliation(
                        message: "A retry failed, but an earlier invalidation is still running; "
                            + "its terminal result remains authoritative."))
                }
                return
            }
            if let deferredCancellationFailureMessage {
                setPhase(.cancellationFailed(message: deferredCancellationFailureMessage))
                return
            }
            guard attempt == latestCancellationAttempt else { return }
            if activeCancellationAttempts.isEmpty {
                setPhase(.cancellationFailed(message: outcome.message))
            }
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

    private func acceptRestoredIdentity(
        _ result: Result<CloudMachineIdentity?, Error>, generation wanted: UInt64
    ) {
        guard wanted == generation else { return }
        switch result {
        case .success(let identity?): setPhase(.connected(identity: identity, origin: .restored))
        case .success(nil): setPhase(.signedOut)
        case .failure(let error):
            if case .operationTimedOut = error as? CloudKeyError {
                setPhase(.restorationReconciliation(message: Self.message(for: error)))
            } else {
                setPhase(.failed(message: Self.message(for: error)))
            }
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
        let wasConnected = Self.isConnected(phase)
        phase = next
        onChange?()
        // Only the transitions that change whether this Mac has a usable credential. The code,
        // waiting and slow-down phases are a login in progress and mean nothing to the bridge.
        if wasConnected != Self.isConnected(next) {
            Self.onConnectionChange?(Self.isConnected(next))
        }
    }

    /// Reservation raises the process-local admission floor before any persistence or removal.
    /// Therefore every sign-out phase is already unusable by local auth and must detach the
    /// bridge, even while durable invalidation/removal is still reconciling.
    private static func isConnected(_ phase: Phase) -> Bool {
        switch phase {
        case .connected: return true
        default: return false
        }
    }
}
