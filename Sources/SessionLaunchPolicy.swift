import Foundation
#if canImport(ClawdlineCore)
import ClawdlineCore
#endif

public enum ProviderTerminalMode: String, Equatable {
    case iTermTab
    case tmuxWindow
    case tmuxDetachedSession

    public var backend: Backend {
        switch self {
        case .iTermTab: return .iterm
        case .tmuxWindow, .tmuxDetachedSession: return .tmux
        }
    }
}

public struct ProviderLaunchRequest: Equatable {
    public let commandID: String
    public let projectRoot: String
    public let assistant: Assistant
    public let model: String?
    public let reasoningEffort: ReasoningEffort?
    public let permission: Permission
    public let additionalDirectory: String?
    public let resumeSessionID: String?
    public let terminalMode: ProviderTerminalMode

    public init(commandID: String, projectRoot: String, assistant: Assistant,
                model: String? = nil, reasoningEffort: ReasoningEffort? = nil,
                permission: Permission = .ask, additionalDirectory: String? = nil,
                resumeSessionID: String? = nil, terminalMode: ProviderTerminalMode) {
        self.commandID = commandID
        self.projectRoot = projectRoot
        self.assistant = assistant
        self.model = model
        self.reasoningEffort = reasoningEffort
        self.permission = permission
        self.additionalDirectory = additionalDirectory
        self.resumeSessionID = resumeSessionID
        self.terminalMode = terminalMode
    }
}

/// A structured provider invocation.  Linux consumes `executableName` and `arguments` directly;
/// the Mac facade consumes `shellCommand` because its established iTerm/tmux adapters type one
/// line into a login shell.  Both values are produced from the same closed policy.
public struct ProviderLaunchPlan: Equatable {
    public let commandID: String
    public let projectRoot: String
    public let assistant: Assistant
    public let terminalMode: ProviderTerminalMode
    public let executableName: String
    public let arguments: [String]
    public let inheritedEnvironmentKeysToRemove: [String]

    public var shellCommand: String {
        let prefix = inheritedEnvironmentKeysToRemove.isEmpty
            ? ""
            : "env " + inheritedEnvironmentKeysToRemove.map { "-u " + $0 }.joined(separator: " ") + " "
        return prefix + ([executableName] + arguments).joined(separator: " ")
    }

    public var shellLine: String {
        "cd " + SessionLaunchPolicy.shellQuoted(projectRoot) + " && " + shellCommand
    }
}

public enum SessionLaunchRefusal: Error, Equatable {
    case invalid(String)
    case capabilityUnavailable(HostCapabilityUnavailable)

    public var code: String {
        switch self {
        case .invalid: return "invalid_launch"
        case .capabilityUnavailable: return HostCapabilityUnavailable.code
        }
    }

    public var message: String {
        switch self {
        case .invalid(let message): return message
        case .capabilityUnavailable(let unavailable): return unavailable.message
        }
    }
}

/// Shared admission for every provider start route.  It is pure: capability and input checks
/// finish before a `TerminalHost.create` call can be made.
public enum SessionLaunchPolicy {
    public static func admit(_ request: ProviderLaunchRequest,
                             terminalCapabilities: Set<HostCapability>)
        -> Result<ProviderLaunchPlan, SessionLaunchRefusal> {
        guard opaqueCommandID(request.commandID) != nil else {
            return .failure(.invalid("The launch command id is not a closed opaque identifier."))
        }
        guard request.projectRoot.hasPrefix("/"),
              !request.projectRoot.unicodeScalars.contains(where: {
                  $0.value < 0x20 || $0.value == 0x7f
              }) else {
            return .failure(.invalid("The launch project root is not a safe absolute path."))
        }
        guard request.model == nil || modelName(request.model) != nil else {
            return .failure(.invalid("The provider model is not an allowlisted slug."))
        }
        guard request.additionalDirectory == nil || extraDirectory(request.additionalDirectory) != nil else {
            return .failure(.invalid("The provider additional directory is not an allowlisted path."))
        }
        guard request.resumeSessionID == nil || sessionID(request.resumeSessionID) != nil else {
            return .failure(.invalid("The provider resume id is not a lowercase UUID."))
        }

        let required = HostCapability.terminal(request.terminalMode.backend)
        guard terminalCapabilities.contains(required) else {
            return .failure(.capabilityUnavailable(HostCapabilityUnavailable(
                capability: required, operation: "start provider session")))
        }

        var arguments: [String] = []
        if let resume = request.resumeSessionID {
            switch request.assistant {
            case .claude: arguments += ["--resume", resume]
            case .codex: arguments += ["resume", resume]
            }
        }
        if let model = request.model { arguments += ["--model", model] }
        if request.assistant == .codex, let effort = request.reasoningEffort {
            arguments += ["--config", "model_reasoning_effort=" + effort.rawValue]
        }
        if let extra = request.additionalDirectory { arguments += ["--add-dir", extra] }
        arguments += permissionArguments(request.permission, assistant: request.assistant)

        return .success(ProviderLaunchPlan(
            commandID: request.commandID,
            projectRoot: request.projectRoot,
            assistant: request.assistant,
            terminalMode: request.terminalMode,
            executableName: request.assistant.rawValue,
            arguments: arguments,
            inheritedEnvironmentKeysToRemove: inheritedIdentityKeys(for: request.assistant)
        ))
    }

    public static func modelName(_ raw: String?) -> String? {
        guard let raw, !raw.isEmpty, raw.count <= 64, !raw.hasPrefix("-") else { return nil }
        return raw.allSatisfy {
            ("a"..."z").contains($0) || ("0"..."9").contains($0)
                || $0 == "." || $0 == "_" || $0 == "-"
        } ? raw : nil
    }

    public static func extraDirectory(_ raw: String?) -> String? {
        guard let raw, raw.hasPrefix("/"), raw.count <= 256, !raw.contains("..") else { return nil }
        return raw.allSatisfy {
            ("a"..."z").contains($0) || ("A"..."Z").contains($0) || ("0"..."9").contains($0)
                || $0 == "." || $0 == "_" || $0 == "-" || $0 == "/"
        } ? raw : nil
    }

    public static func sessionID(_ raw: String?) -> String? {
        guard let raw, raw.count == 36 else { return nil }
        let groups = raw.split(separator: "-", omittingEmptySubsequences: false)
        guard groups.map(\.count) == [8, 4, 4, 4, 12] else { return nil }
        return groups.joined().allSatisfy {
            ("0"..."9").contains($0) || ("a"..."f").contains($0)
        } ? raw : nil
    }

    public static func opaqueCommandID(_ raw: String) -> String? {
        guard !raw.isEmpty, raw.count <= 64, !raw.hasPrefix("-") else { return nil }
        return raw.allSatisfy {
            ("a"..."z").contains($0) || ("0"..."9").contains($0)
                || $0 == "." || $0 == "_" || $0 == "-"
        } ? raw : nil
    }

    public static func shellQuoted(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    public static func inheritedIdentityKeys(for assistant: Assistant) -> [String] {
        switch assistant {
        case .claude:
            return ["CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION",
                    "CLAUDE_PID", "CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN",
                    "CLAUDE_CODE_BRIDGE_SESSION_ID", "CLAUDE_EFFORT", "AI_AGENT"]
        case .codex:
            return ["CODEX_THREAD_ID", "CODEX_SESSION_ID",
                    "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED"]
        }
    }

    private static func permissionArguments(_ permission: Permission,
                                            assistant: Assistant) -> [String] {
        switch (permission, assistant) {
        case (.ask, _): return []
        case (.edits, .claude): return ["--permission-mode", "acceptEdits"]
        case (.edits, .codex): return ["--ask-for-approval", "on-request", "--sandbox", "workspace-write"]
        case (.full, .claude): return ["--permission-mode", "bypassPermissions"]
        case (.full, .codex): return ["--ask-for-approval", "never", "--sandbox", "workspace-write"]
        }
    }
}

public enum TerminalMenuAnswer: Equatable {
    case digit(Int)
    case key([UInt8])
}

/// Parses the complete menu-answer byte request and checks its backend capability in one place.
/// No composition may reinterpret an arbitrary byte array as a terminal key channel.
public enum TerminalMenuAnswerPolicy {
    public static let tab: [UInt8] = [0x09]
    public static let backTab: [UInt8] = [0x1b, 0x5b, 0x5a]

    public static func admit(_ bytes: [UInt8], backend: Backend,
                             terminalCapabilities: Set<HostCapability>)
        -> Result<TerminalMenuAnswer, SessionLaunchRefusal> {
        let capability = HostCapability.terminal(backend)
        guard terminalCapabilities.contains(capability) else {
            return .failure(.capabilityUnavailable(HostCapabilityUnavailable(
                capability: capability, operation: "answer terminal menu")))
        }
        if bytes.count == 1, (0x31...0x39).contains(bytes[0]) {
            return .success(.digit(Int(bytes[0] - 0x30)))
        }
        if bytes == tab || bytes == backTab { return .success(.key(bytes)) }
        return .failure(.invalid("That is not a key this can send."))
    }
}
