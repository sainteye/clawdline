import Foundation
import ClawdlineApplication

struct LinuxDocumentIdentity: Codable, Equatable {
    let authority: String
    let projectRoot: String
    let sessionID: String
    let taskID: String
    let scope: HeadlessDocumentScope
    let relativePath: String
}

struct LinuxDocumentEntry: Codable, Equatable {
    let identity: LinuxDocumentIdentity
    let bytes: Int
}

struct LinuxDocumentsSnapshot: Codable, Equatable {
    let authority: String
    let projectRoot: String
    let sessionID: String
    let taskID: String
    let documents: [LinuxDocumentEntry]
    let truncated: Bool
}

struct LinuxDocumentPayload: Codable, Equatable {
    let identity: LinuxDocumentIdentity
    let bytes: Int
    let sha256: String
    let bodyBase64: String
}

/// Reads inert task/project documents through the identity already accepted by the daemon.
/// Caller strings only select beneath one of two computed roots. Every content read holds one
/// no-follow descriptor from metadata validation through EOF and refuses multiple-link inodes.
final class LinuxDocumentReader {
    private let tasksDirectory: String
    private let expectedUID: UInt32
    var afterRootOpenForTesting: ((Int32) throws -> Void)?

    init(tasksDirectory: String, expectedUID: UInt32) {
        self.tasksDirectory = tasksDirectory
        self.expectedUID = expectedUID
    }

    func document(projectRoot: String, sessionID: String, taskID: String,
                  scope: HeadlessDocumentScope, relativePath rawPath: String)
        throws -> LinuxDocumentPayload {
        guard let relativePath = HeadlessDocumentPathPolicy.relativePath(rawPath) else {
            throw failure("document_not_found", "No document named that.")
        }
        let root = rootPath(projectRoot: projectRoot, taskID: taskID, scope: scope)
        let descriptor = try openFile(root: root, relativePath: relativePath)
        defer { _ = close(descriptor) }
        var metadata = stat()
        guard fstat(descriptor, &metadata) == 0,
              metadata.st_mode & S_IFMT == S_IFREG,
              metadata.st_uid == expectedUID,
              metadata.st_nlink == 1 else {
            throw failure("document_not_found", "No document named that.")
        }
        guard metadata.st_size >= 0,
              metadata.st_size <= HeadlessDocumentPathPolicy.maximumBytes else {
            throw failure("document_too_large", "That document is too large to serve.")
        }
        var bytes = Data(count: Int(metadata.st_size))
        var offset = 0
        while offset < bytes.count {
            let remaining = bytes.count - offset
            let count = bytes.withUnsafeMutableBytes { buffer -> Int in
                read(descriptor, buffer.baseAddress!.advanced(by: offset), remaining)
            }
            if count < 0, errno == EINTR { continue }
            guard count > 0 else {
                throw failure("document_unreadable", "The document changed while it was read.")
            }
            offset += count
        }
        var extra: UInt8 = 0
        while true {
            let extraCount = read(descriptor, &extra, 1)
            if extraCount < 0, errno == EINTR { continue }
            guard extraCount == 0 else {
                throw failure("document_unreadable", "The document changed while it was read.")
            }
            break
        }
        let identity = LinuxDocumentIdentity(
            authority: "linux_daemon_local", projectRoot: projectRoot,
            sessionID: sessionID, taskID: taskID, scope: scope,
            relativePath: relativePath)
        return LinuxDocumentPayload(
            identity: identity, bytes: bytes.count, sha256: LinuxSHA256.hex(bytes),
            bodyBase64: bytes.base64EncodedString())
    }

    func list(projectRoot: String, sessionID: String, taskID: String,
              scope: HeadlessDocumentScope) throws -> LinuxDocumentsSnapshot {
        let root = rootPath(projectRoot: projectRoot, taskID: taskID, scope: scope)
        let rootDescriptor = try openDirectory(root)
        defer { _ = close(rootDescriptor) }
        try afterRootOpenForTesting?(rootDescriptor)
        var walked = 0
        var documents: [LinuxDocumentEntry] = []
        var walkTruncated = false
        try walkDirectory(
            rootDescriptor, prefix: "", projectRoot: projectRoot,
            sessionID: sessionID, taskID: taskID, scope: scope,
            walked: &walked, documents: &documents,
            walkTruncated: &walkTruncated)
        documents.sort { $0.identity.relativePath < $1.identity.relativePath }
        let truncated = walkTruncated
            || documents.count > HeadlessDocumentPathPolicy.maximumListed
        if documents.count > HeadlessDocumentPathPolicy.maximumListed {
            documents.removeLast(documents.count - HeadlessDocumentPathPolicy.maximumListed)
        }
        return LinuxDocumentsSnapshot(
            authority: "linux_daemon_local", projectRoot: projectRoot,
            sessionID: sessionID, taskID: taskID, documents: documents,
            truncated: truncated)
    }

    private func walkDirectory(
        _ directory: Int32, prefix: String, projectRoot: String,
        sessionID: String, taskID: String, scope: HeadlessDocumentScope,
        walked: inout Int, documents: inout [LinuxDocumentEntry],
        walkTruncated: inout Bool
    ) throws {
        for name in try directoryNames(directory) {
            guard walked < HeadlessDocumentPathPolicy.maximumWalked else {
                walkTruncated = true
                return
            }
            walked += 1
            let relative = prefix.isEmpty ? name : prefix + "/" + name
            let descriptor = name.withCString {
                openat(directory, $0, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC)
            }
            guard descriptor >= 0 else { continue }
            defer { _ = close(descriptor) }
            var metadata = stat()
            guard fstat(descriptor, &metadata) == 0,
                  metadata.st_uid == expectedUID else { continue }
            switch metadata.st_mode & S_IFMT {
            case S_IFDIR:
                guard metadata.st_mode & 0o022 == 0 else { continue }
                let depth = relative.split(separator: "/").count
                if depth < HeadlessDocumentPathPolicy.maximumDepth {
                    try walkDirectory(
                        descriptor, prefix: relative, projectRoot: projectRoot,
                        sessionID: sessionID, taskID: taskID, scope: scope,
                        walked: &walked, documents: &documents,
                        walkTruncated: &walkTruncated)
                    if walkTruncated { return }
                }
            case S_IFREG:
                guard metadata.st_nlink == 1, metadata.st_size >= 0,
                      metadata.st_size <= HeadlessDocumentPathPolicy.maximumBytes,
                      HeadlessDocumentPathPolicy.relativePath(relative) != nil else { continue }
                documents.append(.init(
                    identity: LinuxDocumentIdentity(
                        authority: "linux_daemon_local", projectRoot: projectRoot,
                        sessionID: sessionID, taskID: taskID, scope: scope,
                        relativePath: relative),
                    bytes: Int(metadata.st_size)))
            default:
                continue
            }
        }
    }

    private func directoryNames(_ descriptor: Int32) throws -> [String] {
        let duplicate = dup(descriptor)
        guard duplicate >= 0 else {
            throw failure("document_unreadable", "A document directory cannot be held safely.")
        }
        guard let stream = fdopendir(duplicate) else {
            _ = close(duplicate)
            throw failure("document_unreadable", "A document directory cannot be enumerated safely.")
        }
        defer { _ = closedir(stream) }
        var names: [String] = []
        errno = 0
        while let entry = readdir(stream) {
            let name = withUnsafePointer(to: &entry.pointee.d_name) { pointer in
                pointer.withMemoryRebound(
                    to: CChar.self,
                    capacity: MemoryLayout.size(ofValue: entry.pointee.d_name)
                ) { String(cString: $0) }
            }
            if name != ".", name != "..", !name.hasPrefix(".") {
                names.append(name)
            }
            errno = 0
        }
        guard errno == 0 else {
            throw failure("document_unreadable", "A document directory changed during enumeration.")
        }
        return names.sorted()
    }

    private func rootPath(projectRoot: String, taskID: String,
                          scope: HeadlessDocumentScope) -> String {
        switch scope {
        case .project: return projectRoot + "/artifacts"
        case .task: return tasksDirectory + "/" + taskID + "/artifacts"
        }
    }

    private func openDirectory(_ path: String) throws -> Int32 {
        guard ProjectRootPolicy.isLexicallySafeAbsolute(path) else {
            throw failure("document_not_found", "No document root is available.")
        }
        var current = open("/", O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
        guard current >= 0 else {
            throw failure("document_unreadable", "The document root cannot be opened safely.")
        }
        do {
            for component in path.split(separator: "/").map(String.init) {
                let next = component.withCString {
                    openat(current, $0, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
                }
                guard next >= 0 else {
                    throw failure("document_not_found", "No document root is available.")
                }
                _ = close(current)
                current = next
            }
            var metadata = stat()
            guard fstat(current, &metadata) == 0,
                  metadata.st_mode & S_IFMT == S_IFDIR,
                  metadata.st_uid == expectedUID,
                  metadata.st_mode & 0o022 == 0 else {
                throw failure("document_not_found", "No document root is available.")
            }
            return current
        } catch {
            _ = close(current)
            throw error
        }
    }

    private func openFile(root: String, relativePath: String) throws -> Int32 {
        var parent = try openDirectory(root)
        do {
            let components = relativePath.split(separator: "/").map(String.init)
            for component in components.dropLast() {
                let next = component.withCString {
                    openat(parent, $0, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
                }
                guard next >= 0 else {
                    throw failure("document_not_found", "No document named that.")
                }
                _ = close(parent)
                parent = next
            }
            guard let leaf = components.last else {
                throw failure("document_not_found", "No document named that.")
            }
            let descriptor = leaf.withCString {
                openat(parent, $0, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC)
            }
            guard descriptor >= 0 else {
                throw failure("document_not_found", "No document named that.")
            }
            _ = close(parent)
            return descriptor
        } catch {
            _ = close(parent)
            throw error
        }
    }

    private func failure(_ code: String, _ message: String) -> LinuxDurableStateFailure {
        LinuxDurableStateFailure(code: code, message: message)
    }
}
