import Foundation
#if canImport(ClawdlineCore)
import ClawdlineCore
#endif

/// Filesystem evidence gathered by a host adapter without following a symbolic link.  The
/// Application layer decides what that evidence authorizes; Linux gathers it with `lstat` and
/// `realpath`, while tests supply fixed values.
public struct ProjectRootEvidence: Equatable {
    public let requestedPath: String
    public let canonicalPath: String
    public let isDirectory: Bool
    public let containsSymbolicLink: Bool
    public let ownerUID: UInt32
    public let ownerGID: UInt32

    public init(requestedPath: String, canonicalPath: String, isDirectory: Bool,
                containsSymbolicLink: Bool, ownerUID: UInt32, ownerGID: UInt32) {
        self.requestedPath = requestedPath
        self.canonicalPath = canonicalPath
        self.isDirectory = isDirectory
        self.containsSymbolicLink = containsSymbolicLink
        self.ownerUID = ownerUID
        self.ownerGID = ownerGID
    }
}

public protocol ProjectRootInspecting {
    func inspectProjectRoot(at path: String) throws -> ProjectRootEvidence
}

public struct CanonicalProjectRoot: Equatable, Hashable {
    public let path: String
    public let ownerUID: UInt32
    public let ownerGID: UInt32

    public init(path: String, ownerUID: UInt32, ownerGID: UInt32) {
        self.path = path
        self.ownerUID = ownerUID
        self.ownerGID = ownerGID
    }
}

public struct ProjectRootRefusal: Error, Equatable {
    public enum Code: String, Equatable {
        case invalidPath = "invalid_project_path"
        case unavailable = "project_root_unavailable"
        case symbolicLink = "project_root_symlink"
        case notAllowed = "project_root_not_allowed"
        case wrongOwner = "project_root_wrong_owner"
        case pathEscape = "project_path_escape"
    }

    public let code: Code
    public let message: String

    public init(code: Code, message: String) {
        self.code = code
        self.message = message
    }
}

/// The one policy deciding whether a path is an executable project root.  Callers pass only
/// canonical roots from protected configuration; a request must name one of those roots exactly.
/// A path that merely resolves into an allowed root is refused because resolving a symlink is not
/// the same permission as naming the root that was registered.
public struct ProjectRootPolicy {
    public let allowedCanonicalRoots: Set<String>
    public let expectedUID: UInt32
    public let expectedGID: UInt32

    public init(allowedCanonicalRoots: Set<String>, expectedUID: UInt32, expectedGID: UInt32) {
        self.allowedCanonicalRoots = allowedCanonicalRoots
        self.expectedUID = expectedUID
        self.expectedGID = expectedGID
    }

    public func admit(_ requestedPath: String, inspector: any ProjectRootInspecting)
        -> Result<CanonicalProjectRoot, ProjectRootRefusal> {
        guard Self.isLexicallySafeAbsolute(requestedPath) else {
            return .failure(ProjectRootRefusal(
                code: .invalidPath,
                message: "A project root must be one canonical absolute path without traversal."))
        }

        let evidence: ProjectRootEvidence
        do {
            evidence = try inspector.inspectProjectRoot(at: requestedPath)
        } catch {
            return .failure(ProjectRootRefusal(
                code: .unavailable,
                message: "The project root could not be inspected without following links."))
        }
        guard evidence.requestedPath == requestedPath else {
            return .failure(ProjectRootRefusal(code: .unavailable,
                                               message: "The project-root evidence names a different path."))
        }
        guard !evidence.containsSymbolicLink, evidence.canonicalPath == requestedPath else {
            return .failure(ProjectRootRefusal(
                code: .symbolicLink,
                message: "A project root containing or resolving through a symbolic link is refused."))
        }
        guard evidence.isDirectory else {
            return .failure(ProjectRootRefusal(code: .unavailable,
                                               message: "The project root is not a directory."))
        }
        guard allowedCanonicalRoots.contains(evidence.canonicalPath) else {
            return .failure(ProjectRootRefusal(code: .notAllowed,
                                               message: "That canonical project root is not registered."))
        }
        guard evidence.ownerUID == expectedUID, evidence.ownerGID == expectedGID else {
            return .failure(ProjectRootRefusal(
                code: .wrongOwner,
                message: "The project root is not owned by the configured service uid and gid."))
        }
        return .success(CanonicalProjectRoot(path: evidence.canonicalPath,
                                             ownerUID: evidence.ownerUID,
                                             ownerGID: evidence.ownerGID))
    }

    /// Pure lexical screening used before an adapter is allowed to inspect the filesystem.
    public static func isLexicallySafeAbsolute(_ path: String) -> Bool {
        guard path.hasPrefix("/"), path != "/", !path.hasSuffix("/"), path.count <= 4096 else {
            return false
        }
        guard !path.unicodeScalars.contains(where: { $0.value < 0x20 || $0.value == 0x7f }) else {
            return false
        }
        let components = path.split(separator: "/", omittingEmptySubsequences: false)
        guard components.first == "" else { return false }
        return components.dropFirst().allSatisfy { !$0.isEmpty && $0 != "." && $0 != ".." }
    }

    /// True when either canonical path is the other path or an ancestor of it. Reserved daemon
    /// trees use this in both directions so an admitted workspace can neither contain control
    /// state nor sit anywhere below it.
    public static func pathsOverlap(_ first: String, _ second: String) -> Bool {
        first == second || first.hasPrefix(second + "/") || second.hasPrefix(first + "/")
    }

    /// A relative name beneath an admitted root.  The adapter still walks every component with
    /// `O_NOFOLLOW`; this check prevents traversal from ever reaching that effect boundary.
    public static func relativePath(of absolutePath: String,
                                    beneath root: CanonicalProjectRoot)
        -> Result<String, ProjectRootRefusal> {
        guard isLexicallySafeAbsolute(absolutePath) else {
            return .failure(ProjectRootRefusal(code: .invalidPath,
                                               message: "The path is not a canonical absolute path."))
        }
        let prefix = root.path + "/"
        guard absolutePath.hasPrefix(prefix) else {
            return .failure(ProjectRootRefusal(code: .pathEscape,
                                               message: "The path escapes its admitted project root."))
        }
        let relative = String(absolutePath.dropFirst(prefix.count))
        guard !relative.isEmpty else {
            return .failure(ProjectRootRefusal(code: .invalidPath,
                                               message: "The operation requires a path below the project root."))
        }
        return .success(relative)
    }
}

// MARK: - Headless task and read vocabulary

/// The closed Application vocabulary accepted by a headless host.  A host adapter may translate
/// these cases onto a local transport, but it may not translate an arbitrary route or filesystem
/// operation.  The four legacy terminal cases stay here while existing W4-2 clients migrate; the
/// task cases add result/receipt ownership without creating another terminal executor.
public enum HeadlessApplicationOperation: String, Codable, CaseIterable, Sendable {
    case create, send, observe, close
    case taskCreate = "task_create"
    case taskRead = "task_read"
    case taskMessage = "task_message"
    case taskResult = "task_result"
    case taskAcknowledge = "task_acknowledge"
    case taskClose = "task_close"
    case boardRead = "board_read"
    case sessionRead = "session_read"
    case documentsRead = "documents_read"
    case documentRead = "document_read"

    public var isRead: Bool {
        switch self {
        case .taskRead, .boardRead, .sessionRead, .documentsRead, .documentRead:
            return true
        default:
            return false
        }
    }

    /// Only the task's own publication lane consumes the task secret.  Local transport
    /// authentication is a separate prerequisite and is deliberately not represented here.
    public var requiresTaskSecret: Bool {
        switch self {
        case .taskMessage, .taskResult:
            return true
        default:
            return false
        }
    }
}

/// A document caller chooses one of two computed roots, never an absolute pathname.
public enum HeadlessDocumentScope: String, Codable, CaseIterable, Sendable {
    case project
    case task
}

/// Pure lexical policy shared by direct and relayed headless reads before a host opens anything.
/// The host still has to walk descriptors, refuse links/devices and re-check size on the opened
/// descriptor; this policy alone is not filesystem containment.
public enum HeadlessDocumentPathPolicy {
    public static let readableExtensions: Set<String> = ["md", "markdown", "txt"]
    public static let maximumBytes = 2 * 1024 * 1024
    public static let maximumDepth = 6
    public static let maximumPathBytes = 512
    public static let maximumListed = 200
    public static let maximumWalked = 4_000

    public static func relativePath(_ value: String) -> String? {
        guard !value.isEmpty, value.utf8.count <= maximumPathBytes,
              !value.hasPrefix("/"), !value.unicodeScalars.contains(where: {
                  $0.value < 0x20 || $0.value == 0x7f
              }) else { return nil }
        let segments = value.split(separator: "/", omittingEmptySubsequences: false)
        guard !segments.isEmpty, segments.count <= maximumDepth,
              segments.allSatisfy({ !$0.isEmpty && !$0.hasPrefix(".") }) else { return nil }
        let normalized = segments.joined(separator: "/")
        guard readableExtensions.contains(
            URL(fileURLWithPath: normalized).pathExtension.lowercased()) else { return nil }
        return normalized
    }
}
