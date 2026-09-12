import Foundation
import ClawdlineApplication

/// Descriptor-walked filesystem access beneath explicitly admitted roots. Every parent is opened
/// with `O_NOFOLLOW`; a rename is performed relative to the same parent descriptor, so a path
/// cannot be swapped into an escape between policy admission and the effect.
final class LinuxContainedFileSystemHost: FileSystemHost {
    let roots: [CanonicalProjectRoot]
    let maximumReadBytes: Int
    private let protectedOwner: (uid: UInt32, gid: UInt32)?
    var capabilities: Set<HostCapability> { [.files] }

    init(roots: [CanonicalProjectRoot], maximumReadBytes: Int = 16 * 1_048_576,
         protectedOwner: (uid: UInt32, gid: UInt32)? = nil) {
        self.roots = roots.sorted { $0.path.count > $1.path.count }
        self.maximumReadBytes = maximumReadBytes
        self.protectedOwner = protectedOwner
    }

    func contents(atPath path: String) throws -> Data? {
        try withParent(of: path) { parent, leaf in
            let descriptor = leaf.withCString {
                openat(parent, $0, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC)
            }
            if descriptor < 0, errno == ENOENT { return nil }
            guard descriptor >= 0 else { throw failure("The contained file could not be opened safely.") }
            defer { _ = close(descriptor) }
            var metadata = stat()
            guard fstat(descriptor, &metadata) == 0,
                  metadata.st_mode & S_IFMT == S_IFREG,
                  metadata.st_size >= 0,
                  metadata.st_size <= maximumReadBytes else {
                throw failure("The contained path is not a bounded regular file.")
            }
            try validateProtectedLeaf(metadata, operation: "read")
            var data = Data(count: Int(metadata.st_size))
            var offset = 0
            while offset < data.count {
                let remaining = data.count - offset
                let count = data.withUnsafeMutableBytes { raw -> Int in
                    read(descriptor, raw.baseAddress!.advanced(by: offset), remaining)
                }
                guard count >= 0 else { throw failure("The contained file could not be read.") }
                if count == 0 { break }
                offset += count
            }
            guard offset == data.count else {
                throw failure("The contained file changed while it was read.")
            }
            return data
        }
    }

    func writeAtomically(_ data: Data, toPath path: String) throws {
        guard data.count <= maximumReadBytes else {
            throw failure("The contained write exceeds its byte limit.")
        }
        try withParent(of: path) { parent, leaf in
            try refuseSymbolicLink(parent: parent, leaf: leaf)
            let temporary = ".clawdline-" + UUID().uuidString.lowercased() + ".tmp"
            let descriptor = temporary.withCString {
                openat(parent, $0, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC, 0o600)
            }
            guard descriptor >= 0 else { throw failure("The atomic write could not create its temporary file.") }
            var keepTemporary = true
            defer {
                _ = close(descriptor)
                if keepTemporary { _ = temporary.withCString { unlinkat(parent, $0, 0) } }
            }
            var temporaryMetadata = stat()
            guard fstat(descriptor, &temporaryMetadata) == 0 else {
                throw failure("The atomic write temporary file could not be inspected.")
            }
            try validateProtectedLeaf(temporaryMetadata, operation: "write")
            var offset = 0
            while offset < data.count {
                let count = data.withUnsafeBytes { raw -> Int in
                    write(descriptor, raw.baseAddress!.advanced(by: offset), data.count - offset)
                }
                guard count > 0 else { throw failure("The atomic write did not complete.") }
                offset += count
            }
            guard fsync(descriptor) == 0 else { throw failure("The atomic write could not be synchronized.") }
            let renamed = temporary.withCString { source in
                leaf.withCString { destination in renameat(parent, source, parent, destination) }
            }
            guard renamed == 0, fsync(parent) == 0 else {
                throw failure("The atomic write could not replace and synchronize its destination.")
            }
            keepTemporary = false
        }
    }

    func removeItem(atPath path: String) throws {
        try withParent(of: path) { parent, leaf in
            let descriptor = leaf.withCString {
                openat(parent, $0, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC)
            }
            if descriptor < 0, errno == ENOENT { return }
            guard descriptor >= 0 else {
                throw failure("The contained remove could not open its leaf safely.")
            }
            defer { _ = close(descriptor) }
            var opened = stat()
            guard fstat(descriptor, &opened) == 0,
                  opened.st_mode & S_IFMT == S_IFREG else {
                throw failure("A contained remove accepts only a regular file.")
            }
            try validateProtectedLeaf(opened, operation: "remove")
            var metadata = stat()
            let status = leaf.withCString { fstatat(parent, $0, &metadata, AT_SYMLINK_NOFOLLOW) }
            guard status == 0,
                  metadata.st_mode & S_IFMT == S_IFREG,
                  metadata.st_dev == opened.st_dev,
                  metadata.st_ino == opened.st_ino else {
                throw failure("A contained remove refuses a changed or linked leaf.")
            }
            guard leaf.withCString({ unlinkat(parent, $0, 0) }) == 0, fsync(parent) == 0 else {
                throw failure("The contained file could not be removed and synchronized.")
            }
        }
    }

    private func withParent<T>(of path: String, _ body: (Int32, String) throws -> T) throws -> T {
        guard let root = roots.first(where: { path.hasPrefix($0.path + "/") }) else {
            throw failure("The requested file is outside every admitted project root.")
        }
        let relative: String
        switch ProjectRootPolicy.relativePath(of: path, beneath: root) {
        case .success(let value): relative = value
        case .failure(let refusal):
            throw LinuxRuntimeFailure(code: .fileFailure, message: refusal.message)
        }
        let components = relative.split(separator: "/").map(String.init)
        guard let leaf = components.last else { throw failure("The contained path has no leaf.") }
        var current = open("/", O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
        guard current >= 0 else { throw failure("The filesystem root could not be opened safely.") }
        defer { _ = close(current) }
        let rootComponents = root.path.split(separator: "/").map(String.init)
        for (index, component) in rootComponents.enumerated() {
            let next = component.withCString {
                openat(current, $0, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
            }
            guard next >= 0 else { throw failure("An admitted-root component is missing or linked.") }
            _ = close(current)
            current = next
            if index == rootComponents.count - 1 {
                try validateProtectedDirectory(current)
            }
        }
        for component in components.dropLast() {
            let next = component.withCString {
                openat(current, $0, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
            }
            guard next >= 0 else { throw failure("A contained parent is missing or linked.") }
            _ = close(current)
            current = next
            try validateProtectedDirectory(current)
        }
        return try body(current, leaf)
    }

    private func refuseSymbolicLink(parent: Int32, leaf: String) throws {
        var metadata = stat()
        let status = leaf.withCString { fstatat(parent, $0, &metadata, AT_SYMLINK_NOFOLLOW) }
        if status != 0, errno == ENOENT { return }
        guard status == 0, metadata.st_mode & S_IFMT != S_IFLNK else {
            throw failure("An atomic contained write refuses a symbolic-link destination.")
        }
        guard metadata.st_mode & S_IFMT == S_IFREG else {
            throw failure("An atomic contained write replaces only a regular file.")
        }
        try validateProtectedLeaf(metadata, operation: "write")
    }

    private func validateProtectedDirectory(_ descriptor: Int32) throws {
        guard let protectedOwner else { return }
        var metadata = stat()
        guard fstat(descriptor, &metadata) == 0,
              metadata.st_mode & S_IFMT == S_IFDIR,
              metadata.st_uid == protectedOwner.uid,
              metadata.st_gid == protectedOwner.gid,
              metadata.st_mode & 0o077 == 0 else {
            throw failure("The protected directory chain has unsafe ownership or mode.")
        }
    }

    private func validateProtectedLeaf(_ metadata: stat, operation: String) throws {
        guard let protectedOwner else { return }
        guard metadata.st_mode & S_IFMT == S_IFREG,
              metadata.st_uid == protectedOwner.uid,
              metadata.st_gid == protectedOwner.gid,
              metadata.st_mode & 0o077 == 0,
              metadata.st_nlink == 1 else {
            throw failure("The protected secret cannot be admitted for \(operation).")
        }
    }

    private func failure(_ message: String) -> LinuxRuntimeFailure {
        LinuxRuntimeFailure(code: .fileFailure, message: message)
    }
}

/// One process-wide coordinator is shared by every store instance. The key includes the canonical
/// secret root and account, so independent Linux compositions cannot silently race the same file.
private final class LinuxSecretStoreCoordinator: @unchecked Sendable {
    static let shared = LinuxSecretStoreCoordinator()
    private let mapLock = NSLock()
    private var accountLocks: [String: NSLock] = [:]

    func withAccount<T>(_ key: String, _ body: () throws -> T) rethrows -> T {
        mapLock.lock()
        let lock: NSLock
        if let existing = accountLocks[key] {
            lock = existing
        } else {
            let created = NSLock()
            accountLocks[key] = created
            lock = created
        }
        mapLock.unlock()
        lock.lock(); defer { lock.unlock() }
        return try body()
    }
}

/// Protected local secret storage. Account names are a closed filename alphabet, and every
/// operation — including plain reads and sets — crosses the process-wide per-account coordinator.
/// Closed operations use private unlocked primitives, avoiding recursive locking while retaining
/// the `SecretStore` serialization contract across multiple store instances.
final class LinuxProtectedFileSecretStore: SecretStore, @unchecked Sendable {
    private let files: LinuxContainedFileSystemHost
    private let root: CanonicalProjectRoot
    private let coordinator = LinuxSecretStoreCoordinator.shared
    var capabilities: Set<HostCapability> { [.secrets] }

    init(root: CanonicalProjectRoot) {
        self.root = root
        self.files = LinuxContainedFileSystemHost(
            roots: [root], maximumReadBytes: 4096,
            protectedOwner: (uid: root.ownerUID, gid: root.ownerGID))
    }

    func data(for account: String) throws -> Data? {
        let path = try path(for: account)
        return try coordinator.withAccount(path) { try readUnlocked(path) }
    }

    func set(_ data: Data, for account: String) throws {
        let path = try path(for: account)
        try coordinator.withAccount(path) { try writeUnlocked(data, path) }
    }

    func loadOrCreate(_ account: String, create: @Sendable () throws -> Data) throws -> Data {
        let path = try path(for: account)
        return try coordinator.withAccount(path) {
            if let existing = try readUnlocked(path) { return existing }
            let made = try create()
            try writeUnlocked(made, path)
            return made
        }
    }

    func rotate(_ account: String, replace: @Sendable (Data?) throws -> Data) throws -> Data {
        let path = try path(for: account)
        return try coordinator.withAccount(path) {
            let made = try replace(try readUnlocked(path))
            try writeUnlocked(made, path)
            return made
        }
    }

    func remove(_ account: String) throws {
        let path = try path(for: account)
        try coordinator.withAccount(path) { try files.removeItem(atPath: path) }
    }

    private func readUnlocked(_ path: String) throws -> Data? {
        try files.contents(atPath: path)
    }

    private func writeUnlocked(_ data: Data, _ path: String) throws {
        try files.writeAtomically(data, toPath: path)
    }

    private func path(for account: String) throws -> String {
        guard let slug = SessionLaunchPolicy.opaqueCommandID(account) else {
            throw LinuxRuntimeFailure(code: .fileFailure,
                                      message: "A secret account must be a closed filename slug.")
        }
        return root.path + "/" + slug + ".secret"
    }
}
