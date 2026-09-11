import Foundation
#if canImport(ClawdlineCore)
import ClawdlineCore
#endif

public enum TerminalEffectOperation: String, Codable, Equatable, CaseIterable {
    case create, send, observe, interrupt, resize, close, enumerate
}

/// These are distinct receipts, not aliases for one success bit.  A terminal command may be
/// accepted but never execute, execute but fail before delivery, or be delivered to a tty without
/// evidence that a provider observed it.
public enum TerminalEffectStage: String, Codable, Equatable, CaseIterable {
    case accepted, executed, delivered, observed

    fileprivate var rank: Int {
        switch self {
        case .accepted: return 0
        case .executed: return 1
        case .delivered: return 2
        case .observed: return 3
        }
    }
}

public struct TerminalEffectProgress: Codable, Equatable {
    public let commandID: String
    public let operation: TerminalEffectOperation
    public let channel: String
    public private(set) var stages: [TerminalEffectStage]

    public init(commandID: String, operation: TerminalEffectOperation, channel: String) {
        self.commandID = commandID
        self.operation = operation
        self.channel = channel
        self.stages = [.accepted]
    }

    public mutating func advance(to stage: TerminalEffectStage) throws {
        guard let previous = stages.last, stage.rank == previous.rank + 1 else {
            throw TerminalLifecycleFailure(
                code: .invalidTransition,
                message: "Terminal effect stages must advance exactly once in order.")
        }
        stages.append(stage)
    }
}

public struct TerminalWorkLimits: Equatable {
    public let total: Int
    public let perChannel: Int
    public let maximumInputBytes: Int
    public let maximumInventory: Int

    public init(total: Int = 8, perChannel: Int = 2,
                maximumInputBytes: Int = 65_536, maximumInventory: Int = 64) {
        self.total = total
        self.perChannel = perChannel
        self.maximumInputBytes = maximumInputBytes
        self.maximumInventory = maximumInventory
    }

    public static let production = TerminalWorkLimits()
}

public enum TerminalLifecycleFailureCode: String, Codable, Equatable {
    case totalCapacity = "terminal_capacity"
    case channelCapacity = "terminal_channel_capacity"
    case duplicateCommand = "duplicate_terminal_command"
    case invalidCommand = "invalid_terminal_command"
    case invalidTransition = "invalid_terminal_transition"
    case timeout = "terminal_timeout"
    case outputLimit = "terminal_output_limit"
    case maintenance = "terminal_maintenance"
    case identityChanged = "process_identity_changed"
    case environment = "invalid_provider_environment"
}

public struct TerminalLifecycleFailure: Error, Codable, Equatable {
    public let code: TerminalLifecycleFailureCode
    public let message: String

    public init(code: TerminalLifecycleFailureCode, message: String) {
        self.code = code
        self.message = message
    }
}

public enum ProviderEnvironmentPolicy {
    public static let allowedKeys: Set<String> = ["HOME", "PATH", "LANG", "LC_ALL", "TERM", "TMPDIR"]

    /// Construct, never filter, so a newly inherited credential cannot silently join the child.
    public static func closed(home: String, temporaryDirectory: String,
                              executableSearchPath: String = "/usr/local/bin:/usr/bin:/bin")
        -> Result<[String: String], TerminalLifecycleFailure> {
        guard ProjectRootPolicy.isLexicallySafeAbsolute(home),
              ProjectRootPolicy.isLexicallySafeAbsolute(temporaryDirectory),
              !executableSearchPath.isEmpty, executableSearchPath.utf8.count <= 1024,
              !executableSearchPath.contains("\n") else {
            return .failure(TerminalLifecycleFailure(
                code: .environment,
                message: "Provider HOME, TMPDIR, and PATH must be bounded canonical values."))
        }
        return .success([
            "HOME": home,
            "PATH": executableSearchPath,
            "LANG": "C.UTF-8",
            "LC_ALL": "C.UTF-8",
            "TERM": "xterm-256color",
            "TMPDIR": temporaryDirectory,
        ])
    }
}

public enum ProviderLifecyclePolicy {
    public static func timeout(for operation: TerminalEffectOperation) -> TimeInterval {
        switch operation {
        case .create: return 10
        case .send, .interrupt, .resize, .close: return 5
        case .observe, .enumerate: return 3
        }
    }

    public static func validateInput(_ data: Data, limits: TerminalWorkLimits = .production) throws {
        guard !data.isEmpty, data.count <= limits.maximumInputBytes else {
            throw TerminalLifecycleFailure(
                code: .invalidCommand,
                message: "Terminal input must contain 1...\(limits.maximumInputBytes) bytes.")
        }
    }
}
