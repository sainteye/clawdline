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
}
