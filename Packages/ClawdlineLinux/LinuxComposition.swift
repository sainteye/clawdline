import Foundation
import ClawdlineApplication

enum LinuxCompositionError: Error, Equatable {
    case badArguments(String)
    case configuration(String)
    case secret(String)
    case runtime(LinuxRuntimeFailure)
    case internalFailure

    var code: String {
        switch self {
        case .badArguments: return "bad_arguments"
        case .configuration: return "invalid_configuration"
        case .secret: return "invalid_secret_file"
        case .runtime(let failure): return failure.code.rawValue
        case .internalFailure: return "internal_failure"
        }
    }

    var message: String {
        switch self {
        case .badArguments(let message), .configuration(let message), .secret(let message):
            return message
        case .runtime(let failure): return failure.message
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

    init(uid: UInt32, gid: UInt32, projectRoots: [String], tmuxExecutable: String,
         providers: LinuxProviderExecutablesConfiguration,
         cloudCommandsEnabled: Bool? = false) {
        self.uid = uid
        self.gid = gid
        self.projectRoots = projectRoots
        self.tmuxExecutable = tmuxExecutable
        self.providers = providers
        self.cloudCommandsEnabled = cloudCommandsEnabled
    }
}

struct LinuxDaemonConfiguration: Codable, Equatable {
    static let schemaVersion = 2
    static let readableSchemaVersions = 1...2

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
            throw LinuxCompositionError.configuration("config version 2 requires runtimeDirectory")
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
                    "cloudCommandsEnabled",
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
        }
        return configuration
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

enum LinuxCompositionCommand {
    case health
    case releaseContract
    case configuredHealth(String)
    case checkConfig(String)
    case run(String)
    case daemon(String)

    static func parse(_ arguments: [String]) throws -> LinuxCompositionCommand {
        if arguments == ["health"] { return .health }
        if arguments == ["release-contract"] { return .releaseContract }
        guard arguments.count == 3, arguments[1] == "--config" else {
            throw LinuxCompositionError.badArguments("usage: ClawdlineLinux release-contract | health [--config /absolute/path] | check-config --config /absolute/path | run --config /absolute/path | daemon --config /absolute/path")
        }
        switch arguments[0] {
        case "health": return .configuredHealth(arguments[2])
        case "check-config": return .checkConfig(arguments[2])
        case "run": return .run(arguments[2])
        case "daemon": return .daemon(arguments[2])
        default: throw LinuxCompositionError.badArguments("unknown command \(arguments[0])")
        }
    }
}

enum LinuxComposition {
    static func execute(arguments: [String]) throws -> Data {
        switch try LinuxCompositionCommand.parse(arguments) {
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
        }
    }

    static func errorData(_ error: LinuxCompositionError) -> Data {
        let envelope = LinuxErrorEnvelope(error: .init(code: error.code, message: error.message))
        return (try? encode(envelope)) ?? Data("{\"error\":{\"code\":\"encoding_failed\"}}\n".utf8)
    }

    private static func encode<T: Encodable>(_ value: T) throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        var bytes = try encoder.encode(value)
        bytes.append(0x0a)
        return bytes
    }
}
