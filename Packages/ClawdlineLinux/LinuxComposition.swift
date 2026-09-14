import Foundation
import ClawdlineApplication

enum LinuxCompositionError: Error, Equatable {
    case badArguments(String)
    case configuration(String)
    case secret(String)
    case runtime(LinuxRuntimeFailure)
    case cloudEnrollment(String)
    case cloudPairing(String)
    case internalFailure

    var code: String {
        switch self {
        case .badArguments: return "bad_arguments"
        case .configuration: return "invalid_configuration"
        case .secret: return "invalid_secret_file"
        case .runtime(let failure): return failure.code.rawValue
        case .cloudEnrollment: return "cloud_enrollment_failed"
        case .cloudPairing: return "cloud_pairing_failed"
        case .internalFailure: return "internal_failure"
        }
    }

    var message: String {
        switch self {
        case .badArguments(let message), .configuration(let message), .secret(let message),
             .cloudPairing(let message):
            return message
        case .runtime(let failure): return failure.message
        case .cloudEnrollment(let message): return message
        case .internalFailure:
            return "unexpected startup failure"
        }
    }
}

private enum LinuxProtectedInputKind {
    case configuration
    case secret

    var maximumBytes: Int {
        switch self {
        case .configuration: return 64 * 1024
        case .secret: return 4096
        }
    }

    func error(_ message: String) -> LinuxCompositionError {
        switch self {
        case .configuration: return .configuration(message)
        case .secret: return .secret(message)
        }
    }
}

/// Opens, validates, and reads a protected input through one descriptor. The pathname is never
/// reopened after validation, so replacing it cannot redirect the read to another inode.
private enum LinuxProtectedInput {
    static func load(from path: String, kind: LinuxProtectedInputKind) throws -> Data {
        guard path.hasPrefix("/") else {
            throw kind.error("protected input path must be absolute")
        }

        let descriptor = path.withCString {
            open($0, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC)
        }
        guard descriptor >= 0 else {
            throw kind.error("protected input cannot be opened safely")
        }
        defer { _ = close(descriptor) }

        var metadata = stat()
        guard fstat(descriptor, &metadata) == 0 else {
            throw kind.error("protected input metadata is unavailable")
        }
        guard metadata.st_mode & S_IFMT == S_IFREG else {
            throw kind.error("protected input must be a regular file, not a symlink or device")
        }

        let effectiveUID = geteuid()
        let mode = metadata.st_mode & 0o777
        switch kind {
        case .configuration:
            guard metadata.st_uid == effectiveUID || metadata.st_uid == 0 else {
                throw kind.error("config file owner must be the service user or root")
            }
            guard mode & 0o022 == 0 else {
                throw kind.error("config file must not be group- or world-writable")
            }
        case .secret:
            guard metadata.st_uid == effectiveUID else {
                throw kind.error("secret file owner must be the service user")
            }
            guard mode == 0o600 else {
                throw kind.error("secret file permissions must be exactly 0600")
            }
        }

        guard metadata.st_size > 0, metadata.st_size <= kind.maximumBytes else {
            throw kind.error("protected input must contain 1...\(kind.maximumBytes) bytes")
        }

        var buffer = [UInt8](repeating: 0, count: kind.maximumBytes + 1)
        var count = 0
        while count <= kind.maximumBytes {
            let result = buffer.withUnsafeMutableBytes { rawBuffer -> Int in
                let start = rawBuffer.baseAddress!.advanced(by: count)
                return read(descriptor, start, rawBuffer.count - count)
            }
            guard result >= 0 else {
                throw kind.error("protected input is unreadable")
            }
            if result == 0 { break }
            count += result
            if count > kind.maximumBytes {
                throw kind.error("protected input exceeds \(kind.maximumBytes) bytes")
            }
        }
        guard count > 0, count == metadata.st_size else {
            throw kind.error("protected input changed while it was being read")
        }
        return Data(buffer.prefix(count))
    }
}

struct LinuxListenConfiguration: Codable, Equatable {
    let host: String
    let port: Int
}

struct LinuxProviderExecutablesConfiguration: Codable, Equatable {
    let claude: String
    let codex: String
}

struct LinuxRuntimeConfiguration: Codable, Equatable {
    let uid: UInt32
    let gid: UInt32
    let projectRoots: [String]
    let tmuxExecutable: String
    let providers: LinuxProviderExecutablesConfiguration
    /// Protected, explicit effect gate. Reads remain available to paired viewers; every Relay
    /// mutation re-reads this value at the serialized effect boundary.
    let cloudCommandsEnabled: Bool?
    /// A display-only bounded label. It is not authority and never replaces the channel machine id.
    let displayName: String?
    /// Infrastructure presentation is explicit configuration, never inferred from hostname.
    let infrastructureProvider: String?

    init(uid: UInt32, gid: UInt32, projectRoots: [String], tmuxExecutable: String,
         providers: LinuxProviderExecutablesConfiguration,
         cloudCommandsEnabled: Bool? = false, displayName: String? = nil,
         infrastructureProvider: String? = nil) {
        self.uid = uid
        self.gid = gid
        self.projectRoots = projectRoots
        self.tmuxExecutable = tmuxExecutable
        self.providers = providers
        self.cloudCommandsEnabled = cloudCommandsEnabled
        self.displayName = displayName
        self.infrastructureProvider = infrastructureProvider
    }
}

struct LinuxDaemonConfiguration: Codable, Equatable {
    static let schemaVersion = 3
    static let readableSchemaVersions = 1...3

    let version: Int
    let listen: LinuxListenConfiguration
    let stateDirectory: String
    /// Version 1 derived this beneath `stateDirectory`. Version 2 names the volatile path
    /// explicitly so systemd's `RuntimeDirectory=` and durable `StateDirectory=` cannot be
    /// accidentally collapsed into one lifecycle.
    let runtimeDirectory: String?
    let secretFile: String
    let runtime: LinuxRuntimeConfiguration?

    init(version: Int, listen: LinuxListenConfiguration, stateDirectory: String,
         runtimeDirectory: String? = nil, secretFile: String,
         runtime: LinuxRuntimeConfiguration? = nil) {
        self.version = version
        self.listen = listen
        self.stateDirectory = stateDirectory
        self.runtimeDirectory = runtimeDirectory
        self.secretFile = secretFile
        self.runtime = runtime
    }

    static func load(from path: String) throws -> LinuxDaemonConfiguration {
        let bytes: Data
        do {
            bytes = try LinuxProtectedInput.load(from: path, kind: .configuration)
        } catch let error as LinuxCompositionError {
            throw error
        }

        let object: Any
        do {
            object = try JSONSerialization.jsonObject(with: bytes)
        } catch {
            throw LinuxCompositionError.configuration("config file is not valid JSON")
        }
        guard let fields = object as? [String: Any] else {
            throw LinuxCompositionError.configuration("config root must be an object")
        }
        let requiredFields = Set(["version", "listen", "stateDirectory", "secretFile"])
        let allowedFields = requiredFields.union(["runtime", "runtimeDirectory"])
        guard requiredFields.isSubset(of: Set(fields.keys)), Set(fields.keys).isSubset(of: allowedFields) else {
            throw LinuxCompositionError.configuration("config fields must be version, listen, stateDirectory, secretFile, and optional runtime")
        }
        guard let listen = fields["listen"] as? [String: Any],
              Set(listen.keys) == Set(["host", "port"]) else {
            throw LinuxCompositionError.configuration("listen fields must be exactly host and port")
        }

        let configuration: LinuxDaemonConfiguration
        do {
            configuration = try JSONDecoder().decode(LinuxDaemonConfiguration.self, from: bytes)
        } catch {
            throw LinuxCompositionError.configuration("config values have invalid types")
        }
        guard readableSchemaVersions.contains(configuration.version) else {
            throw LinuxCompositionError.configuration("unsupported config version \(configuration.version)")
        }
        guard configuration.version == 1 || fields["runtimeDirectory"] != nil else {
            throw LinuxCompositionError.configuration("config version 2 or later requires runtimeDirectory")
        }
        guard configuration.listen.host == "127.0.0.1" || configuration.listen.host == "::1" else {
            throw LinuxCompositionError.configuration("W3 accepts only a loopback listen host")
        }
        guard (1...65535).contains(configuration.listen.port) else {
            throw LinuxCompositionError.configuration("listen port must be between 1 and 65535")
        }
        guard configuration.stateDirectory.hasPrefix("/"), configuration.secretFile.hasPrefix("/"),
              configuration.runtimeDirectory.map({ $0.hasPrefix("/") }) ?? true else {
            throw LinuxCompositionError.configuration("stateDirectory, runtimeDirectory, and secretFile must be absolute")
        }
        guard configuration.stateDirectory != "/", configuration.secretFile != "/",
              configuration.runtimeDirectory != "/" else {
            throw LinuxCompositionError.configuration("stateDirectory, runtimeDirectory, and secretFile may not be the filesystem root")
        }
        if let runtimeDirectory = configuration.runtimeDirectory,
           ProjectRootPolicy.pathsOverlap(configuration.stateDirectory, runtimeDirectory) {
            throw LinuxCompositionError.configuration("durable state and volatile runtime directories must not overlap")
        }
        if let runtime = configuration.runtime {
            guard let runtimeObject = fields["runtime"] as? [String: Any],
                  Set(["uid", "gid", "projectRoots", "tmuxExecutable", "providers"])
                    .isSubset(of: Set(runtimeObject.keys)),
                  Set(runtimeObject.keys).isSubset(of: Set([
                    "uid", "gid", "projectRoots", "tmuxExecutable", "providers",
                    "cloudCommandsEnabled", "displayName", "infrastructureProvider",
                  ])),
                  let providers = runtimeObject["providers"] as? [String: Any],
                  Set(providers.keys) == Set(["claude", "codex"]) else {
                throw LinuxCompositionError.configuration("runtime fields or provider fields are not the exact supported shape")
            }
            guard runtime.uid > 0, runtime.gid > 0,
                  !runtime.projectRoots.isEmpty, runtime.projectRoots.count <= 32,
                  Set(runtime.projectRoots).count == runtime.projectRoots.count,
                  runtime.projectRoots.allSatisfy(ProjectRootPolicy.isLexicallySafeAbsolute),
                  ProjectRootPolicy.isLexicallySafeAbsolute(runtime.tmuxExecutable),
                  ProjectRootPolicy.isLexicallySafeAbsolute(runtime.providers.claude),
                  ProjectRootPolicy.isLexicallySafeAbsolute(runtime.providers.codex) else {
                throw LinuxCompositionError.configuration("runtime identity, roots, or executable paths are invalid")
            }
            guard Self.displayValue(runtime.displayName, maximumBytes: 80),
                  Self.displayValue(runtime.infrastructureProvider, maximumBytes: 32),
                  runtime.infrastructureProvider == nil
                    || runtime.infrastructureProvider == "aws" else {
                throw LinuxCompositionError.configuration(
                    "runtime displayName or infrastructureProvider is not a supported bounded value")
            }
        }
        return configuration
    }

    private static func displayValue(_ value: String?, maximumBytes: Int) -> Bool {
        guard let value else { return true }
        return !value.isEmpty && value.utf8.count <= maximumBytes
            && value.unicodeScalars.allSatisfy { $0.value >= 0x20 && $0.value != 0x7f }
    }
}

enum LinuxProtectedSecretFile {
    static let maximumBytes = 4096

    static func load(from path: String) throws -> Data {
        try LinuxProtectedInput.load(from: path, kind: .secret)
    }
}

struct LinuxUnsupportedCapability: Codable, Equatable {
    let capability: String
    let owner: String
    let refusal: String
}

struct LinuxCapabilityState: Codable, Equatable {
    let capability: String
    let compiled: Bool
    let configured: Bool
    let usable: Bool
    let owner: String?
    let refusal: String?
}

struct LinuxRuntimeIdentity: Codable, Equatable {
    static var current: LinuxRuntimeIdentity { LinuxRuntimeIdentity(
        service: "clawdline-daemon",
        executable: "ClawdlineLinux",
        identityKind: "diagnostic",
        buildIdentity: ProcessInfo.processInfo.environment["CLAWDLINE_BUILD_IDENTITY"] ?? "unknown",
        configurationSchemaVersion: LinuxDaemonConfiguration.schemaVersion,
        ready: false,
        readinessCode: "w4_runtime_not_configured",
        applicationRefusalCode: HostCapabilityUnavailable.code,
        supportedCapabilities: [],
        unsupportedCapabilities: HostCapability.allCases.map { capability in
            LinuxUnsupportedCapability(
                capability: capability.rawValue,
                owner: capability == .terminalITerm ? "Mac composition" : "Linux protected configuration",
                refusal: HostCapabilityUnavailable.code)
        },
        capabilityStates: HostCapability.allCases.map { capability in
            LinuxCapabilityState(
                capability: capability.rawValue,
                compiled: capability != .terminalITerm,
                configured: false, usable: false,
                owner: capability == .terminalITerm ? "Mac composition" : "Linux protected configuration",
                refusal: HostCapabilityUnavailable.code)
        },
        lifecycleStages: [TerminalEffectStage.accepted.rawValue,
                          TerminalEffectStage.executed.rawValue,
                          TerminalEffectStage.delivered.rawValue,
                          TerminalEffectStage.observed.rawValue],
        providers: Assistant.allCases.map { LinuxProviderCapability.unconfigured(provider: $0.rawValue) }
    ) }

    static func configured() -> LinuxRuntimeIdentity {
        let usable = HostCapability.allCases.filter { $0 != .terminalITerm }
        return LinuxRuntimeIdentity(
            service: "clawdline-daemon", executable: "ClawdlineLinux",
            identityKind: "configured_runtime",
            buildIdentity: ProcessInfo.processInfo.environment["CLAWDLINE_BUILD_IDENTITY"] ?? "unknown",
            configurationSchemaVersion: LinuxDaemonConfiguration.schemaVersion,
            ready: false,
            readinessCode: "w4_provider_authentication_not_proven",
            applicationRefusalCode: HostCapabilityUnavailable.code,
            supportedCapabilities: usable.map(\.rawValue),
            unsupportedCapabilities: [LinuxUnsupportedCapability(
                capability: HostCapability.terminalITerm.rawValue,
                owner: "Mac composition", refusal: HostCapabilityUnavailable.code)],
            capabilityStates: HostCapability.allCases.map { capability in
                let isUsable = capability != .terminalITerm
                return LinuxCapabilityState(
                    capability: capability.rawValue,
                    compiled: isUsable, configured: isUsable, usable: isUsable,
                    owner: isUsable ? nil : "Mac composition",
                    refusal: isUsable ? nil : HostCapabilityUnavailable.code)
            },
            lifecycleStages: TerminalEffectStage.allCases.map(\.rawValue),
            providers: Assistant.allCases.map {
                LinuxProviderCapability.configuredAuthenticationPending(provider: $0.rawValue)
            })
    }

    let service: String
    let executable: String
    let identityKind: String
    let buildIdentity: String
    let configurationSchemaVersion: Int
    let ready: Bool
    let readinessCode: String
    let applicationRefusalCode: String
    let supportedCapabilities: [String]
    let unsupportedCapabilities: [LinuxUnsupportedCapability]
    let capabilityStates: [LinuxCapabilityState]
    let lifecycleStages: [String]
    let providers: [LinuxProviderCapability]
}

struct LinuxBinaryReleaseContract: Codable, Equatable {
    let configurationSchemaVersion: Int
    let configurationReadableMinimum: Int
    let durableSchemaVersion: Int
    let durableReadableMinimum: Int
    let durableReadableMaximum: Int
    let protocolIdentity: String

    static let current = LinuxBinaryReleaseContract(
        configurationSchemaVersion: LinuxDaemonConfiguration.schemaVersion,
        configurationReadableMinimum: LinuxDaemonConfiguration.readableSchemaVersions.lowerBound,
        durableSchemaVersion: LinuxDurableState.schemaVersion,
        durableReadableMinimum: LinuxDurableState.minimumReadableSchemaVersion,
        durableReadableMaximum: LinuxDurableState.maximumReadableSchemaVersion,
        protocolIdentity: "clawdline-linux-local-health-v1")
}

struct LinuxProviderCapability: Codable, Equatable {
    let provider: String
    let executableConfigured: Bool
    let authenticated: Bool
    let usable: Bool
    let terminal: String?
    let lifecycle: [String]
    let credentialBoundary: String
    let owner: String
    let refusal: String

    static func unconfigured(provider: String) -> LinuxProviderCapability {
        LinuxProviderCapability(
            provider: provider, executableConfigured: false, authenticated: false,
            usable: false, terminal: nil, lifecycle: [],
            credentialBoundary: "landlock_seccomp_required",
            owner: "Linux protected configuration",
            refusal: HostCapabilityUnavailable.code)
    }

    static func configuredAuthenticationPending(provider: String) -> LinuxProviderCapability {
        LinuxProviderCapability(
            provider: provider, executableConfigured: true, authenticated: false,
            usable: false, terminal: nil, lifecycle: [],
            credentialBoundary: "landlock_seccomp_enforced",
            owner: "W4-2 real-provider authentication gate",
            refusal: HostCapabilityUnavailable.code)
    }
}

struct LinuxConfigurationReceipt: Codable {
    let configuration: String
    let secretBytes: Int
    let identity: LinuxRuntimeIdentity
}

struct LinuxCloudLoginInvitation: Codable, Equatable {
    let event: String
    let userCode: String
    let verificationURL: String
    let verificationCompleteURL: String
    let expiresInSeconds: Int
    let pollIntervalSeconds: Int

    init(event: String = "authorization_required", userCode: String,
         verificationURL: String, verificationCompleteURL: String,
         expiresInSeconds: Int, pollIntervalSeconds: Int) {
        self.event = event
        self.userCode = userCode
        self.verificationURL = verificationURL
        self.verificationCompleteURL = verificationCompleteURL
        self.expiresInSeconds = expiresInSeconds
        self.pollIntervalSeconds = pollIntervalSeconds
    }

    init(_ started: CloudDeviceLoginStart) {
        self.init(
            userCode: started.userCode,
            verificationURL: started.verificationURL.absoluteString,
            verificationCompleteURL: started.verificationCompleteURL.absoluteString,
            expiresInSeconds: started.expiresIn,
            pollIntervalSeconds: started.interval)
    }
}

struct LinuxCloudLoginReceipt: Codable, Equatable {
    let event: String
    let status: String
    let accountID: String
    let machineID: String
    let protectedIdentity: Bool
}

struct LinuxBrowserPairingInvitation: Codable, Equatable {
    let event: String
    let verificationURL: String
    let accountID: String
    let machineID: String
    let machineFingerprint: String
    let expiresAtMilliseconds: Int64
}

struct LinuxBrowserPairingReceipt: Codable, Equatable {
    let event: String
    let status: String
    let accountID: String
    let machineID: String
    let viewerDeviceID: String
    let viewerFingerprint: String
    let machineFingerprint: String
    let protectedIdentity: Bool
}

/// Runs the deployed-console compatibility pairing flow without a GUI. The invitation URL is
/// emitted once to the invoking terminal and is never written to the daemon log or a receipt.
/// Polling is bounded by the invitation expiry, a fixed maximum number of attempts and three
/// same-prepared-delivery recovery attempts. Request cancellation is cooperative, as URLSession's
/// async transport is; a noncooperative injected dependency can finish after its nominal timeout.
enum LinuxBrowserPairing {
    static let pollIntervalNanoseconds: UInt64 = 1_500_000_000
    static let requestTimeoutNanoseconds: UInt64 = 15_000_000_000
    static let maximumLifetimeMilliseconds: Int64 = 600_000
    static let maximumPollAttempts = 401
    static let maximumRecoveryAttempts = 3

    typealias Begin = @Sendable () async throws -> CloudCompatibilityPairingStart
    typealias Advance = @Sendable () async throws -> CloudCompatibilityPairingProgress
    typealias Sleep = @Sendable (UInt64) async throws -> Void

    static func execute(
        configuration: LinuxDaemonConfiguration,
        emit: @escaping @Sendable (Data) -> Void
    ) async throws -> Data {
        do {
            let runtimeConfiguration = configuration.runtime
            let layout = try LinuxRuntimeLayout.prepare(
                stateDirectory: configuration.stateDirectory,
                runtimeDirectory: configuration.runtimeDirectory,
                expectedUID: runtimeConfiguration?.uid,
                expectedGID: runtimeConfiguration?.gid)
            let secretRoot = CanonicalProjectRoot(
                path: layout.secrets, ownerUID: layout.uid, ownerGID: layout.gid)
            // Opening the durable runtime takes its writer locks. A running daemon therefore
            // refuses this command before either process can mutate the protected identity.
            let runtime = try LinuxDurableCloudRuntime(
                stateDirectory: layout.state, expectedUID: layout.uid,
                secrets: LinuxProtectedFileSecretStore(root: secretRoot))
            guard case .enrolled = try runtime.enrollmentState() else {
                throw LinuxCompositionError.cloudPairing(
                    "This machine must finish cloud-login before pairing a browser.")
            }
            let handover = CloudCompatibilityPairingMachineHandover(
                client: runtime.accountClient, authority: runtime.identityAuthority,
                nowMilliseconds: { Int64(Date().timeIntervalSince1970 * 1_000) })
            return try await execute(
                emit: emit,
                nowMilliseconds: { Int64(Date().timeIntervalSince1970 * 1_000) },
                sleep: { try await Task.sleep(nanoseconds: $0) },
                requestTimeoutNanoseconds: requestTimeoutNanoseconds,
                begin: { try await handover.begin() },
                advance: { try await handover.advance() })
        } catch let error as LinuxCompositionError {
            throw error
        } catch let error as LinuxRuntimeFailure {
            throw LinuxCompositionError.runtime(error)
        } catch let error as CloudDurableStoreFailure {
            throw pairingError(for: error)
        } catch let error as LocalizedError {
            throw LinuxCompositionError.cloudPairing(
                error.errorDescription ?? "Browser pairing failed.")
        } catch {
            throw LinuxCompositionError.cloudPairing("Browser pairing failed.")
        }
    }

    static func pairingError(for failure: CloudDurableStoreFailure) -> LinuxCompositionError {
        switch failure {
        case .writerLockHeld:
            return .cloudPairing(
                "Browser pairing needs exclusive durable state. Stop clawdline-daemon, retry "
                    + "pair-browser, then start the daemon again.")
        default:
            return .cloudPairing("Cloud durable pairing state was refused: \(failure).")
        }
    }

    static func execute(
        emit: @escaping @Sendable (Data) -> Void,
        nowMilliseconds: @escaping @Sendable () -> Int64,
        sleep: @escaping Sleep,
        requestTimeoutNanoseconds: UInt64 = requestTimeoutNanoseconds,
        begin: @escaping Begin,
        advance: @escaping Advance
    ) async throws -> Data {
        let started: CloudCompatibilityPairingStart
        do {
            started = try await request(
                timeoutNanoseconds: requestTimeoutNanoseconds, operation: begin)
        } catch is RequestTimeout {
            throw LinuxCompositionError.cloudPairing(
                "Cloud pairing start did not cooperatively finish within its request timeout.")
        }
        let now = nowMilliseconds()
        guard started.expiresAtMilliseconds > now,
              started.expiresAtMilliseconds - now <= maximumLifetimeMilliseconds,
              started.verificationURL.scheme == "https",
              started.verificationURL.host == "app.clawdline.com",
              started.verificationURL.query == nil,
              started.verificationURL.fragment?.hasPrefix("pair=") == true else {
            throw LinuxCompositionError.cloudPairing(
                "Cloud returned an unsafe browser-pairing invitation.")
        }
        emit(try LinuxComposition.encode(LinuxBrowserPairingInvitation(
            event: "browser_pairing_required",
            verificationURL: started.verificationURL.absoluteString,
            accountID: started.accountID, machineID: started.machineID,
            machineFingerprint: started.machineFingerprint,
            expiresAtMilliseconds: started.expiresAtMilliseconds)))

        var recoveryAttempts = 0
        for attempt in 0..<maximumPollAttempts {
            guard nowMilliseconds() < started.expiresAtMilliseconds else {
                throw LinuxCompositionError.cloudPairing(
                    "Browser pairing expired before the viewer completed it.")
            }
            let progress: CloudCompatibilityPairingProgress
            do {
                progress = try await request(
                    timeoutNanoseconds: requestTimeoutNanoseconds, operation: advance)
            } catch is RequestTimeout {
                recoveryAttempts += 1
                guard recoveryAttempts <= maximumRecoveryAttempts else {
                    throw LinuxCompositionError.cloudPairing(
                        "Cloud pairing could not reconcile an uncertain request after three attempts.")
                }
                try await sleep(pollIntervalNanoseconds)
                continue
            }
            switch progress {
            case .waiting:
                recoveryAttempts = 0
                guard attempt + 1 < maximumPollAttempts,
                      nowMilliseconds() < started.expiresAtMilliseconds else {
                    throw LinuxCompositionError.cloudPairing(
                        "Browser pairing expired before the viewer completed it.")
                }
                try await sleep(pollIntervalNanoseconds)
            case .retrying:
                recoveryAttempts += 1
                guard recoveryAttempts <= maximumRecoveryAttempts else {
                    throw LinuxCompositionError.cloudPairing(
                        "Cloud pairing could not reconcile its prepared handover after three attempts.")
                }
                try await sleep(pollIntervalNanoseconds)
            case .complete(let receipt):
                guard receipt.accountID == started.accountID,
                      receipt.machineID == started.machineID,
                      receipt.machineFingerprint == started.machineFingerprint else {
                    throw LinuxCompositionError.cloudPairing(
                        "Browser pairing completed for a different protected machine identity.")
                }
                return try LinuxComposition.encode(LinuxBrowserPairingReceipt(
                    event: "browser_pairing_complete", status: "paired",
                    accountID: receipt.accountID, machineID: receipt.machineID,
                    viewerDeviceID: receipt.viewerDeviceID,
                    viewerFingerprint: receipt.viewerFingerprint,
                    machineFingerprint: receipt.machineFingerprint,
                    protectedIdentity: true))
            }
        }
        throw LinuxCompositionError.cloudPairing(
            "Browser pairing exceeded its bounded polling window.")
    }

    private enum RequestTimeout: Error { case elapsed }

    private static func request<Value: Sendable>(
        timeoutNanoseconds: UInt64,
        operation: @escaping @Sendable () async throws -> Value
    ) async throws -> Value {
        try await withThrowingTaskGroup(of: Value.self) { group in
            defer { group.cancelAll() }
            group.addTask { try await operation() }
            group.addTask {
                try await Task.sleep(nanoseconds: timeoutNanoseconds)
                throw RequestTimeout.elapsed
            }
            guard let first = try await group.next() else {
                throw RequestTimeout.elapsed
            }
            return first
        }
    }
}

enum LinuxCloudEnrollmentOutcome: Equatable {
    case accessDenied
    case expired
    case complete(accountID: String, machineID: String)
}

enum LinuxCloudEnrollment {
    typealias Begin = @Sendable (CloudMachineMetadata) async throws
        -> (LinuxCloudLoginInvitation, @Sendable () async throws -> LinuxCloudEnrollmentOutcome)

    static func execute(
        configuration: LinuxDaemonConfiguration,
        emit: @Sendable (Data) -> Void,
        state: () throws -> LinuxDurableCloudRuntime.EnrollmentState,
        begin: Begin
    ) async throws -> Data {
        switch try state() {
        case .enrolled(let accountID, let machineID):
            return try LinuxComposition.encode(LinuxCloudLoginReceipt(
                event: "enrollment_complete", status: "already_enrolled",
                accountID: accountID, machineID: machineID, protectedIdentity: true))
        case .missing:
            break
        }

        let metadata = CloudMachineMetadata(
            name: configuration.runtime?.displayName ?? "Clawdline Linux",
            platform: "linux",
            appVersion: LinuxReleaseIdentity.current.packageVersion)
        let (invitation, waitForCompletion) = try await begin(metadata)
        emit(try LinuxComposition.encode(invitation))
        switch try await waitForCompletion() {
        case .accessDenied:
            throw LinuxCompositionError.cloudEnrollment("Cloud device authorization was denied.")
        case .expired:
            throw LinuxCompositionError.cloudEnrollment("Cloud device authorization expired.")
        case .complete(let accountID, let machineID):
            return try LinuxComposition.encode(LinuxCloudLoginReceipt(
                event: "enrollment_complete", status: "enrolled",
                accountID: accountID, machineID: machineID, protectedIdentity: true))
        }
    }

    static func execute(
        configuration: LinuxDaemonConfiguration,
        emit: @escaping @Sendable (Data) -> Void
    ) async throws -> Data {
        do {
            let runtimeConfiguration = configuration.runtime
            let layout = try LinuxRuntimeLayout.prepare(
                stateDirectory: configuration.stateDirectory,
                runtimeDirectory: configuration.runtimeDirectory,
                expectedUID: runtimeConfiguration?.uid,
                expectedGID: runtimeConfiguration?.gid)
            let secretRoot = CanonicalProjectRoot(
                path: layout.secrets, ownerUID: layout.uid, ownerGID: layout.gid)
            let runtime = try LinuxDurableCloudRuntime(
                stateDirectory: layout.state, expectedUID: layout.uid,
                secrets: LinuxProtectedFileSecretStore(root: secretRoot))
            return try await execute(
                configuration: configuration, emit: emit,
                state: { try runtime.enrollmentState() },
                begin: { metadata in
                    let started = try await runtime.startDeviceLogin(metadata: metadata)
                    return (LinuxCloudLoginInvitation(started), {
                        switch try await runtime.continueDeviceLogin(started) {
                        case .authorizationPending, .slowDown:
                            throw LinuxCompositionError.cloudEnrollment(
                                "Cloud device authorization ended in an invalid waiting state.")
                        case .accessDenied:
                            return .accessDenied
                        case .expired:
                            return .expired
                        case .complete(let identity):
                            return .complete(
                                accountID: identity.accountID, machineID: identity.machineID)
                        }
                    })
                })
        } catch let error as LinuxCompositionError {
            throw error
        } catch let error as LinuxRuntimeFailure {
            throw LinuxCompositionError.runtime(error)
        } catch let error as CloudExecutorIdentityError {
            throw LinuxCompositionError.cloudEnrollment(
                error.errorDescription ?? "Protected executor identity is unavailable.")
        } catch let error as CloudAccountError {
            throw LinuxCompositionError.cloudEnrollment(
                error.errorDescription ?? "Cloud account enrollment failed.")
        } catch let error as CloudDurableStoreFailure {
            throw LinuxCompositionError.cloudEnrollment(
                "Cloud durable enrollment state was refused: \(error).")
        } catch {
            throw LinuxCompositionError.cloudEnrollment("Cloud account enrollment failed.")
        }
    }
}

struct LinuxRuntimeCompositionReceipt: Codable {
    let configuration: String
    let uid: UInt32
    let gid: UInt32
    let umask: String
    let homeDirectory: String
    let runtimeDirectory: String
    let projectRoots: [String]
    let terminalLimits: LinuxTerminalLimitsReceipt
    let identity: LinuxRuntimeIdentity
}

struct LinuxTerminalLimitsReceipt: Codable {
    let total: Int
    let perChannel: Int
    let maximumInputBytes: Int
    let maximumInventory: Int
}

struct LinuxErrorEnvelope: Codable {
    struct Body: Codable {
        let code: String
        let message: String
    }

    let error: Body
}

enum LinuxCompositionCommand: Equatable {
    case health
    case releaseContract
    case configuredHealth(String)
    case checkConfig(String)
    case run(String)
    case daemon(String)
    case cloudLogin(String)
    case pairBrowser(String)

    static func parse(_ arguments: [String]) throws -> LinuxCompositionCommand {
        if arguments == ["health"] { return .health }
        if arguments == ["release-contract"] { return .releaseContract }
        guard arguments.count == 3, arguments[1] == "--config" else {
            throw LinuxCompositionError.badArguments("usage: ClawdlineLinux release-contract | health [--config /absolute/path] | check-config --config /absolute/path | cloud-login --config /absolute/path | pair-browser --config /absolute/path | run --config /absolute/path | daemon --config /absolute/path")
        }
        switch arguments[0] {
        case "health": return .configuredHealth(arguments[2])
        case "check-config": return .checkConfig(arguments[2])
        case "run": return .run(arguments[2])
        case "daemon": return .daemon(arguments[2])
        case "cloud-login": return .cloudLogin(arguments[2])
        case "pair-browser": return .pairBrowser(arguments[2])
        default: throw LinuxCompositionError.badArguments("unknown command \(arguments[0])")
        }
    }
}

enum LinuxComposition {
    static func executeAsync(
        arguments: [String], emit: @escaping @Sendable (Data) -> Void
    ) async throws -> Data {
        let command = try LinuxCompositionCommand.parse(arguments)
        if case .cloudLogin(let path) = command {
            let config = try LinuxDaemonConfiguration.load(from: path)
            return try await LinuxCloudEnrollment.execute(configuration: config, emit: emit)
        }
        if case .pairBrowser(let path) = command {
            let config = try LinuxDaemonConfiguration.load(from: path)
            return try await LinuxBrowserPairing.execute(configuration: config, emit: emit)
        }
        return try execute(command: command)
    }

    static func execute(arguments: [String]) throws -> Data {
        try execute(command: LinuxCompositionCommand.parse(arguments))
    }

    private static func execute(command: LinuxCompositionCommand) throws -> Data {
        switch command {
        case .health:
            return try encode(LinuxRuntimeIdentity.current)
        case .releaseContract:
            return try encode(LinuxBinaryReleaseContract.current)
        case .configuredHealth(let path):
            let config = try LinuxDaemonConfiguration.load(from: path)
            return try LinuxDaemonService.health(configuration: config)
        case .checkConfig(let path):
            let config = try LinuxDaemonConfiguration.load(from: path)
            let secret = try LinuxProtectedSecretFile.load(from: config.secretFile)
            return try encode(LinuxConfigurationReceipt(
                configuration: "accepted_not_started",
                secretBytes: secret.count,
                identity: .current
            ))
        case .run(let path):
            let config = try LinuxDaemonConfiguration.load(from: path)
            _ = try LinuxProtectedSecretFile.load(from: config.secretFile)
            do {
                let runtime = try LinuxProviderRuntime.compose(configuration: config)
                return try encode(runtime.compositionReceipt)
            } catch let failure as LinuxRuntimeFailure {
                throw LinuxCompositionError.runtime(failure)
            }
        case .daemon(let path):
            let config = try LinuxDaemonConfiguration.load(from: path)
            let secret = try LinuxProtectedSecretFile.load(from: config.secretFile)
            do {
                try LinuxDaemonService.run(configuration: config, authorization: secret)
                throw LinuxCompositionError.internalFailure
            } catch let failure as LinuxRuntimeFailure {
                throw LinuxCompositionError.runtime(failure)
            } catch let failure as LinuxDurableStateFailure {
                throw LinuxCompositionError.configuration("\(failure.code): \(failure.message)")
            }
        case .cloudLogin, .pairBrowser:
            throw LinuxCompositionError.internalFailure
        }
    }

    static func errorData(_ error: LinuxCompositionError) -> Data {
        let envelope = LinuxErrorEnvelope(error: .init(code: error.code, message: error.message))
        return (try? encode(envelope)) ?? Data("{\"error\":{\"code\":\"encoding_failed\"}}\n".utf8)
    }

    static func encode<T: Encodable>(_ value: T) throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        var bytes = try encoder.encode(value)
        bytes.append(0x0a)
        return bytes
    }
}
