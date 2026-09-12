import Foundation
import Dispatch
#if canImport(ClawdlineCore)
import ClawdlineCore
#endif
#if canImport(Darwin)
import Darwin
#elseif canImport(Glibc)
import Glibc
#endif

/// The public W0-E bytes are an additive-reader candidate, not production authority. Keeping
/// every pin in one value prevents a caller from validating one digest and silently promoting it.
public struct CloudContractCandidateAuthority: Equatable, Sendable {
    public static let w0E = CloudContractCandidateAuthority(
        publicCommit: "38eb822575e3c309a776a9e3e2874c7062d8fb75",
        packageTree: "3ee391a4af73f9688510c19106d38ba325227051",
        sourceSHA256: "47c21a3d096813f940026943004791087ea89d628f6123942dd59c02f71a2f3d",
        packageSHA256: "6ccccea5f05b603fd9a583940f6f7a3fd5a8735ac6d5ce6ef7246620b59b7d6f",
        authority: "candidate", cutoverRequired: true)

    public let publicCommit: String
    public let packageTree: String
    public let sourceSHA256: String
    public let packageSHA256: String
    public let authority: String
    public let cutoverRequired: Bool

    public var permitsNormativeCutover: Bool { authority != "candidate" && !cutoverRequired }
    public func permitsEmission(wireVersion: Int) -> Bool {
        wireVersion == 1 && permitsNormativeCutover
    }
    public func permitsClientFloorRaise(to _: String) -> Bool { permitsNormativeCutover }
}

public enum CloudDurableStoreFaultPoint: String, CaseIterable, Sendable {
    case persist
    case fileFsync = "file_fsync"
    case rename
    case directoryFsync = "directory_fsync"
    case recovery
}

public enum CloudDurableStoreFailure: Error, Equatable, Sendable {
    case unsafePath
    case unsafePermissions
    case invalidMachineIdentity
    case writerLockHeld
    case unreadable
    case corrupt
    case unknownVersion(Int)
    case oversized
    case symlink
    case nonRegular
    case multipleLinks
    case wrongOwner
    case persist
    case fileFsync
    case rename
    case directoryFsync
    case recovery
    case sequenceExhausted
}

private struct CloudDurableFileMetadata {
    let size: Int64
    let uid: UInt32
    let links: UInt64
    let mode: mode_t
}

/// One descriptor-checked, single-writer authority file. The previous generation remains under a
/// sibling name until the new file and directory entry are fsynced. If the final directory fsync
/// fails, the candidate is preserved in quarantine and the old generation is restored before the
/// error reaches the application layer.
private final class CloudDurableFile: @unchecked Sendable {
    private static let maximumRecoveryDirectoryEntries = 128
    private static let maximumLegacyCandidates = 64
    let url: URL
    let maximumBytes: Int
    let expectedUID: UInt32
    private let directory: URL
    private let quarantineDirectory: URL
    private let lockURL: URL
    private let previousURL: URL
    private let temporaryURL: URL
    private let token = UUID().uuidString.lowercased()
    /// The inode is only a stable rendezvous name. Ownership is the kernel lock retained by this
    /// descriptor, so a crash releases authority without asking a later process to guess whether
    /// the token left on disk is stale.
    private var writerLockDescriptor: Int32 = -1

    init(url: URL, maximumBytes: Int, expectedUID: UInt32) throws {
        guard url.isFileURL, url.path.hasPrefix("/"), !url.pathComponents.contains(".."),
              url.lastPathComponent.first != ".", maximumBytes > 0 else {
            throw CloudDurableStoreFailure.unsafePath
        }
        self.url = url
        self.maximumBytes = maximumBytes
        self.expectedUID = expectedUID
        directory = url.deletingLastPathComponent()
        quarantineDirectory = directory.appendingPathComponent("quarantine", isDirectory: true)
        lockURL = directory.appendingPathComponent(".\(url.lastPathComponent).writer.lock")
        previousURL = directory.appendingPathComponent(".\(url.lastPathComponent).previous")
        temporaryURL = directory.appendingPathComponent(".\(url.lastPathComponent).writing")
        try Self.prepareDirectory(directory, expectedUID: expectedUID)
        try Self.prepareDirectory(quarantineDirectory, expectedUID: expectedUID)
        try acquireWriterLock()
        do {
            try recoverOrphanCandidates()
        } catch {
            releaseWriterLock()
            throw error
        }
    }

    deinit { releaseWriterLock() }

    func load() throws -> Data? {
        if Self.pathExists(previousURL.path) {
            try recoverInterruptedCommit()
        }
        guard Self.pathExists(url.path) else { return nil }
        do {
            return try secureRead(url)
        } catch let failure as CloudDurableStoreFailure {
            try quarantine(url, reason: "load")
            throw failure
        }
    }

    func quarantineCurrent() throws {
        guard Self.pathExists(url.path) else { return }
        try quarantine(url, reason: "decode")
    }

    func commit(
        _ bytes: Data,
        fault: ((CloudDurableStoreFaultPoint) throws -> Void)?
    ) throws {
        guard bytes.count <= maximumBytes else { throw CloudDurableStoreFailure.oversized }
        if Self.pathExists(url.path) { _ = try secureRead(url) }
        if Self.pathExists(previousURL.path) { try recoverInterruptedCommit() }

        let temporary = temporaryURL
        guard !Self.pathExists(temporary.path) else {
            throw CloudDurableStoreFailure.recovery
        }
        var movedPrevious = false
        do {
            try fault?(.persist)
            try writeNew(bytes, to: temporary, fault: fault)
            if Self.pathExists(url.path) {
                try Self.rename(url.path, previousURL.path)
                movedPrevious = true
                try syncDirectory()
            }
            try fault?(.rename)
            try Self.rename(temporary.path, url.path)
            do {
                try fault?(.directoryFsync)
                try syncDirectory()
            } catch {
                try preserveFailedCandidateAndRestore(movedPrevious: movedPrevious)
                if error is CloudDurableStoreFailure { throw error }
                throw CloudDurableStoreFailure.directoryFsync
            }
            if movedPrevious {
                // The final directory fsync above is the commit point. Cleanup cannot turn an
                // already durable candidate into a reported failure (and thereby leave RAM on
                // the old snapshot while disk holds the new one). A surviving previous name is
                // recognized and quarantined by the next open.
                if unlink(previousURL.path) == 0 { try? syncDirectory() }
            }
        } catch {
            _ = unlink(temporary.path)
            if movedPrevious, !Self.pathExists(url.path), Self.pathExists(previousURL.path) {
                do {
                    try Self.rename(previousURL.path, url.path)
                    try syncDirectory()
                } catch {
                    throw CloudDurableStoreFailure.recovery
                }
            }
            throw error
        }
    }

    private func acquireWriterLock() throws {
        let descriptor = lockURL.path.withCString {
            open($0, O_RDWR | O_CREAT | O_NOFOLLOW | O_CLOEXEC, 0o600)
        }
        guard descriptor >= 0 else { throw CloudDurableStoreFailure.persist }
        var ownsKernelLock = false
        do {
            var opened = stat()
            guard fstat(descriptor, &opened) == 0 else {
                throw CloudDurableStoreFailure.unreadable
            }
            guard opened.st_mode & S_IFMT == S_IFREG else {
                throw CloudDurableStoreFailure.nonRegular
            }
            guard opened.st_nlink == 1 else { throw CloudDurableStoreFailure.multipleLinks }
            guard opened.st_uid == expectedUID else { throw CloudDurableStoreFailure.wrongOwner }
            guard opened.st_mode & 0o777 == 0o600 else {
                throw CloudDurableStoreFailure.unsafePermissions
            }

            guard flock(descriptor, LOCK_EX | LOCK_NB) == 0 else {
                if errno == EWOULDBLOCK || errno == EAGAIN {
                    throw CloudDurableStoreFailure.writerLockHeld
                }
                throw CloudDurableStoreFailure.persist
            }
            ownsKernelLock = true

            // Hold the opened inode still across the pathname check. An attacker cannot swap a
            // checked lock out for a fresh inode and let two writers each own a different lock.
            var named = stat()
            guard lstat(lockURL.path, &named) == 0,
                  named.st_mode & S_IFMT == S_IFREG,
                  named.st_dev == opened.st_dev, named.st_ino == opened.st_ino,
                  named.st_nlink == opened.st_nlink else {
                throw CloudDurableStoreFailure.unsafePath
            }

            guard ftruncate(descriptor, 0) == 0,
                  lseek(descriptor, 0, SEEK_SET) == 0 else {
                throw CloudDurableStoreFailure.persist
            }
            try Self.writeAll(Data(token.utf8), descriptor: descriptor)
            guard fsync(descriptor) == 0 else { throw CloudDurableStoreFailure.fileFsync }
            try syncDirectory()
            writerLockDescriptor = descriptor
        } catch {
            if ownsKernelLock { _ = flock(descriptor, LOCK_UN) }
            _ = close(descriptor)
            throw error
        }
    }

    private func releaseWriterLock() {
        guard writerLockDescriptor >= 0 else { return }
        let descriptor = writerLockDescriptor
        writerLockDescriptor = -1
        _ = flock(descriptor, LOCK_UN)
        _ = close(descriptor)
    }

    private func secureRead(_ source: URL) throws -> Data {
        var before = stat()
        guard lstat(source.path, &before) == 0 else {
            throw CloudDurableStoreFailure.unreadable
        }
        guard before.st_mode & S_IFMT != S_IFLNK else { throw CloudDurableStoreFailure.symlink }
        let descriptor = source.path.withCString {
            open($0, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC)
        }
        guard descriptor >= 0 else { throw CloudDurableStoreFailure.unreadable }
        defer { _ = close(descriptor) }
        var info = stat()
        guard fstat(descriptor, &info) == 0 else { throw CloudDurableStoreFailure.unreadable }
        let metadata = CloudDurableFileMetadata(
            size: Int64(info.st_size), uid: info.st_uid, links: UInt64(info.st_nlink),
            mode: info.st_mode)
        guard metadata.mode & S_IFMT == S_IFREG else { throw CloudDurableStoreFailure.nonRegular }
        guard metadata.links == 1 else { throw CloudDurableStoreFailure.multipleLinks }
        guard metadata.uid == expectedUID else { throw CloudDurableStoreFailure.wrongOwner }
        guard metadata.mode & 0o777 == 0o600 else {
            throw CloudDurableStoreFailure.unsafePermissions
        }
        guard metadata.size >= 0, metadata.size <= maximumBytes else {
            throw CloudDurableStoreFailure.oversized
        }
        var output = Data(count: Int(metadata.size))
        var offset = 0
        while offset < output.count {
            let count = output.withUnsafeMutableBytes { buffer -> Int in
                read(descriptor, buffer.baseAddress!.advanced(by: offset), buffer.count - offset)
            }
            guard count > 0 else { throw CloudDurableStoreFailure.unreadable }
            offset += count
        }
        var after = stat()
        guard fstat(descriptor, &after) == 0,
              after.st_dev == info.st_dev, after.st_ino == info.st_ino,
              after.st_size == info.st_size, after.st_nlink == info.st_nlink else {
            throw CloudDurableStoreFailure.unreadable
        }
        return output
    }

    private func writeNew(
        _ bytes: Data, to target: URL,
        fault: ((CloudDurableStoreFaultPoint) throws -> Void)?
    ) throws {
        let descriptor = target.path.withCString {
            open($0, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC, 0o600)
        }
        guard descriptor >= 0 else { throw CloudDurableStoreFailure.persist }
        var closed = false
        defer { if !closed { _ = close(descriptor) } }
        do {
            try Self.writeAll(bytes, descriptor: descriptor)
            try fault?(.fileFsync)
            guard fsync(descriptor) == 0 else { throw CloudDurableStoreFailure.fileFsync }
            guard close(descriptor) == 0 else { throw CloudDurableStoreFailure.persist }
            closed = true
        } catch {
            throw error is CloudDurableStoreFailure ? error : CloudDurableStoreFailure.persist
        }
    }

    private func preserveFailedCandidateAndRestore(movedPrevious: Bool) throws {
        if Self.pathExists(url.path) { try quarantine(url, reason: "uncommitted") }
        if movedPrevious, Self.pathExists(previousURL.path) {
            try Self.rename(previousURL.path, url.path)
        }
        try syncDirectory()
    }

    private func recoverInterruptedCommit() throws {
        if !Self.pathExists(url.path) {
            do {
                try Self.rename(previousURL.path, url.path)
                try syncDirectory()
            } catch {
                throw CloudDurableStoreFailure.recovery
            }
            return
        }
        // Both names mean the candidate rename reached the filesystem. The current name is the
        // only candidate callers could have observed; retain the older generation in quarantine.
        do {
            try quarantine(previousURL, reason: "superseded")
            try syncDirectory()
        } catch {
            throw CloudDurableStoreFailure.recovery
        }
    }

    /// W5-1 originally used an unbounded UUID candidate name. Recovery recognizes those legacy
    /// names once, but caps the directory work; all new commits use one fixed candidate, so future
    /// crash residue is intrinsically bounded to one file per authority.
    private func recoverOrphanCandidates() throws {
        let legacyPrefix = ".\(url.lastPathComponent).writing-"
        guard let stream = directory.path.withCString({ opendir($0) }) else {
            throw CloudDurableStoreFailure.recovery
        }
        defer { _ = closedir(stream) }
        let directoryDescriptor = dirfd(stream)
        guard directoryDescriptor >= 0 else { throw CloudDurableStoreFailure.recovery }

        var scanned = 0
        var names: [String] = []
        while let entry = readdir(stream) {
            let name = withUnsafePointer(to: entry.pointee.d_name) { pointer in
                pointer.withMemoryRebound(
                    to: CChar.self,
                    capacity: MemoryLayout.size(ofValue: entry.pointee.d_name)
                ) { String(cString: $0) }
            }
            if name == "." || name == ".." { continue }
            scanned += 1
            guard scanned <= Self.maximumRecoveryDirectoryEntries else {
                throw CloudDurableStoreFailure.recovery
            }
            if name == temporaryURL.lastPathComponent || name.hasPrefix(legacyPrefix) {
                names.append(name)
                guard names.count <= Self.maximumLegacyCandidates else {
                    throw CloudDurableStoreFailure.recovery
                }
            }
        }
        for name in names.sorted() {
            try validateAndUnlinkCandidate(name, directoryDescriptor: directoryDescriptor)
        }
        if !names.isEmpty { try syncDirectory() }
    }

    private func validateAndUnlinkCandidate(
        _ name: String, directoryDescriptor: Int32
    ) throws {
        var before = stat()
        let statResult = name.withCString {
            fstatat(directoryDescriptor, $0, &before, AT_SYMLINK_NOFOLLOW)
        }
        guard statResult == 0 else { throw CloudDurableStoreFailure.recovery }
        guard before.st_mode & S_IFMT != S_IFLNK else { throw CloudDurableStoreFailure.symlink }
        guard before.st_mode & S_IFMT == S_IFREG else {
            throw CloudDurableStoreFailure.nonRegular
        }
        guard before.st_nlink == 1 else { throw CloudDurableStoreFailure.multipleLinks }
        guard before.st_uid == expectedUID else { throw CloudDurableStoreFailure.wrongOwner }
        guard before.st_mode & 0o777 == 0o600 else {
            throw CloudDurableStoreFailure.unsafePermissions
        }
        guard before.st_size >= 0, before.st_size <= maximumBytes else {
            throw CloudDurableStoreFailure.oversized
        }

        let descriptor = name.withCString {
            openat(directoryDescriptor, $0, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC)
        }
        guard descriptor >= 0 else { throw CloudDurableStoreFailure.recovery }
        defer { _ = close(descriptor) }
        var opened = stat()
        guard fstat(descriptor, &opened) == 0,
              opened.st_dev == before.st_dev, opened.st_ino == before.st_ino,
              opened.st_size == before.st_size, opened.st_nlink == before.st_nlink else {
            throw CloudDurableStoreFailure.recovery
        }
        var current = stat()
        let currentResult = name.withCString {
            fstatat(directoryDescriptor, $0, &current, AT_SYMLINK_NOFOLLOW)
        }
        guard currentResult == 0,
              current.st_dev == opened.st_dev, current.st_ino == opened.st_ino,
              current.st_size == opened.st_size, current.st_nlink == opened.st_nlink else {
            throw CloudDurableStoreFailure.recovery
        }
        let unlinkResult = name.withCString { unlinkat(directoryDescriptor, $0, 0) }
        guard unlinkResult == 0 else { throw CloudDurableStoreFailure.recovery }
    }

    private func quarantine(_ source: URL, reason: String) throws {
        let name = "\(url.lastPathComponent).\(reason).\(UUID().uuidString.lowercased())"
        let destination = quarantineDirectory.appendingPathComponent(name)
        do {
            try Self.rename(source.path, destination.path)
            try syncDirectory(directory)
            try syncDirectory(quarantineDirectory)
        } catch {
            throw CloudDurableStoreFailure.recovery
        }
    }

    private func syncDirectory(_ target: URL? = nil) throws {
        let directoryURL = target ?? directory
        let descriptor = directoryURL.path.withCString {
            open($0, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
        }
        guard descriptor >= 0 else { throw CloudDurableStoreFailure.directoryFsync }
        defer { _ = close(descriptor) }
        guard fsync(descriptor) == 0 else { throw CloudDurableStoreFailure.directoryFsync }
    }

    private static func prepareDirectory(_ url: URL, expectedUID: UInt32) throws {
        let target = url.standardizedFileURL
        guard target.isFileURL, target.path.hasPrefix("/") else {
            throw CloudDurableStoreFailure.unsafePath
        }
        var missing: [URL] = []
        var cursor = target
        while true {
            var info = stat()
            if lstat(cursor.path, &info) == 0 {
                guard info.st_mode & S_IFMT == S_IFDIR else {
                    throw CloudDurableStoreFailure.unsafePath
                }
                break
            }
            guard errno == ENOENT, cursor.path != "/" else {
                throw CloudDurableStoreFailure.unsafePath
            }
            missing.append(cursor)
            cursor.deleteLastPathComponent()
        }

        for component in missing.reversed() {
            let made = component.path.withCString { mkdir($0, 0o700) }
            guard made == 0 else { throw CloudDurableStoreFailure.unsafePath }
            guard chmod(component.path, 0o700) == 0 else {
                throw CloudDurableStoreFailure.unsafePermissions
            }
            try validatePrivateDirectory(component, expectedUID: expectedUID)
            // The new child inode and the parent's new name both reach stable storage before an
            // authority file inside the child may be published to RAM.
            try syncDirectoryDescriptor(component)
            try syncDirectoryDescriptor(component.deletingLastPathComponent())
        }
        try validatePrivateDirectory(target, expectedUID: expectedUID)
    }

    private static func validatePrivateDirectory(_ url: URL, expectedUID: UInt32) throws {
        var info = stat()
        guard lstat(url.path, &info) == 0,
              info.st_mode & S_IFMT == S_IFDIR,
              info.st_uid == expectedUID,
              info.st_nlink >= 1 else { throw CloudDurableStoreFailure.unsafePath }
        guard info.st_mode & 0o777 == 0o700 else {
            throw CloudDurableStoreFailure.unsafePermissions
        }
    }

    private static func syncDirectoryDescriptor(_ url: URL) throws {
        let descriptor = url.path.withCString {
            open($0, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
        }
        guard descriptor >= 0 else { throw CloudDurableStoreFailure.directoryFsync }
        defer { _ = close(descriptor) }
        guard fsync(descriptor) == 0 else { throw CloudDurableStoreFailure.directoryFsync }
    }

    private static func pathExists(_ path: String) -> Bool {
        var info = stat()
        return lstat(path, &info) == 0
    }

    private static func rename(_ source: String, _ destination: String) throws {
        guard source.withCString({ sourcePointer in
            destination.withCString { destinationPointer in
                #if canImport(Darwin)
                Darwin.rename(sourcePointer, destinationPointer)
                #else
                Glibc.rename(sourcePointer, destinationPointer)
                #endif
            }
        }) == 0 else { throw CloudDurableStoreFailure.rename }
    }

    private static func writeAll(_ bytes: Data, descriptor: Int32) throws {
        var offset = 0
        while offset < bytes.count {
            let count = bytes.withUnsafeBytes { buffer -> Int in
                write(descriptor, buffer.baseAddress!.advanced(by: offset), buffer.count - offset)
            }
            guard count > 0 else { throw CloudDurableStoreFailure.persist }
            offset += count
        }
    }
}

/// Compatibility fence for the former Mac `cloud-sequence.json` authority. It keeps the old
/// schema readable by the previous binary and durably raises that binary's starting ceiling before
/// the new spool returns a reservation. This is intentionally a live rollback fence, not a second
/// allocator: only `CloudOutboundSpool` returns sequence numbers in the new image.
public final class CloudLegacySequenceFence: CloudSpoolSequenceFence, @unchecked Sendable {
    public static let maximumBytes = 1 * 1024 * 1024
    private let file: CloudDurableFile
    private let sender: String
    private let block: Int64
    private let lock = NSLock()
    private var senders: [String: Int64]

    public init(
        url: URL, sender: String, expectedUID: UInt32 = geteuid(), block: Int64 = 64
    ) throws {
        guard !sender.isEmpty, block > 0 else { throw CloudDurableStoreFailure.corrupt }
        self.sender = sender
        self.block = block
        senders = [:]
        file = try CloudDurableFile(
            url: url, maximumBytes: Self.maximumBytes, expectedUID: expectedUID)
        guard let bytes = try file.load() else {
            return
        }
        let value: CloudJSONValue
        do { value = try CloudCanonicalJSON.parseStrict(bytes) }
        catch {
            try file.quarantineCurrent()
            throw CloudDurableStoreFailure.corrupt
        }
        guard case .object(let root) = value,
              Set(root.keys) == ["v", "senders"],
              case .int(let version)? = root["v"], version == 1,
              case .object(let encodedSenders)? = root["senders"] else {
            try file.quarantineCurrent()
            throw CloudDurableStoreFailure.corrupt
        }
        var parsed: [String: Int64] = [:]
        for (identity, entry) in encodedSenders {
            guard case .int(let ceiling) = entry, ceiling >= 0,
                  ceiling <= CloudCanonicalJSON.maximumSafeInteger else {
                try file.quarantineCurrent()
                throw CloudDurableStoreFailure.corrupt
            }
            parsed[identity] = ceiling
        }
        senders = parsed
    }

    public var reservedCeiling: Int64 {
        lock.lock(); defer { lock.unlock() }
        return senders[sender] ?? 0
    }

    public func prepareToReserve(sequence: Int64) throws {
        lock.lock(); defer { lock.unlock() }
        guard sequence >= 0, sequence < CloudCanonicalJSON.maximumSafeInteger else {
            throw CloudDurableStoreFailure.sequenceExhausted
        }
        let current = senders[sender] ?? 0
        guard sequence >= current else { return }
        let (unclampedCandidate, overflow) = sequence.addingReportingOverflow(block)
        let candidate = overflow
            ? CloudCanonicalJSON.maximumSafeInteger
            : min(unclampedCandidate, CloudCanonicalJSON.maximumSafeInteger)
        guard candidate > sequence else { throw CloudDurableStoreFailure.sequenceExhausted }
        var next = senders
        next[sender] = candidate
        let encoded = next.mapValues { CloudJSONValue.int($0) }
        let bytes = CloudCanonicalJSON.canonicalData(.object([
            "v": .int(1), "senders": .object(encoded),
        ]))
        try file.commit(bytes, fault: nil)
        senders = next
    }
}

/// Read-only compatibility used by migration inspection and narrow tests. Production retains the
/// `CloudLegacySequenceFence` instance for the full durable runtime lifetime.
public enum CloudLegacySequenceMigration {
    public static let maximumBytes = CloudLegacySequenceFence.maximumBytes

    public static func reservedCeiling(
        at url: URL, sender: String, expectedUID: UInt32 = geteuid()
    ) throws -> Int64? {
        guard FileManager.default.fileExists(atPath: url.path) else { return nil }
        return try CloudLegacySequenceFence(
            url: url, sender: sender, expectedUID: expectedUID).reservedCeiling
    }
}

private struct CloudLedgerFileEnvelope: Codable {
    static let currentVersion = 2
    let schemaVersion: Int
    let minimumReaderVersion: Int
    let recordKind: String
    let generation: UInt64
    let rows: [CloudCommandLedgerRow]

    init(generation: UInt64, rows: [CloudCommandLedgerRow]) {
        schemaVersion = Self.currentVersion
        minimumReaderVersion = 1
        recordKind = "cloud_command_ledger"
        self.generation = generation
        self.rows = rows
    }

    private enum CodingKeys: String, CodingKey {
        case schemaVersion, minimumReaderVersion, recordKind, generation, rows
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        schemaVersion = try values.decode(Int.self, forKey: .schemaVersion)
        if schemaVersion == Self.currentVersion {
            minimumReaderVersion = try values.decode(Int.self, forKey: .minimumReaderVersion)
            recordKind = try values.decode(String.self, forKey: .recordKind)
            generation = try values.decode(UInt64.self, forKey: .generation)
            rows = try values.decode([CloudCommandLedgerRow].self, forKey: .rows)
        } else {
            // Defaults exist only in the explicitly named additive v1 migration.
            minimumReaderVersion = try values.decodeIfPresent(
                Int.self, forKey: .minimumReaderVersion) ?? 1
            recordKind = try values.decodeIfPresent(String.self, forKey: .recordKind)
                ?? "cloud_command_ledger"
            generation = try values.decodeIfPresent(UInt64.self, forKey: .generation) ?? 0
            rows = try values.decodeIfPresent([CloudCommandLedgerRow].self, forKey: .rows) ?? []
        }
    }
}

/// Durable JSON stays inside the same integer-only domain as the strict parser. `Date`'s default
/// Codable representation is a fractional Double, which made a freshly written authority fail
/// its own strict read on the next process start. Milliseconds preserve the timing precision this
/// state machine uses while remaining far below the shared safe-integer ceiling.
private func cloudDurableJSONEncoder() -> JSONEncoder {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
    encoder.dateEncodingStrategy = .custom { date, target in
        let milliseconds = date.timeIntervalSinceReferenceDate * 1_000
        guard milliseconds.isFinite,
              milliseconds >= Double(CloudCanonicalJSON.minimumSafeInteger),
              milliseconds <= Double(CloudCanonicalJSON.maximumSafeInteger) else {
            throw CloudDurableStoreFailure.persist
        }
        var value = target.singleValueContainer()
        try value.encode(Int64(milliseconds.rounded()))
    }
    return encoder
}

private func cloudDurableJSONDecoder() -> JSONDecoder {
    let decoder = JSONDecoder()
    decoder.dateDecodingStrategy = .custom { source in
        let value = try source.singleValueContainer().decode(Int64.self)
        return Date(timeIntervalSinceReferenceDate: Double(value) / 1_000)
    }
    return decoder
}

/// Schema v2 was released briefly with Foundation's default Date representation: seconds since
/// 2001 as a JSON number. This decoder is never used as a general fallback. A caller must also
/// prove that decoding and re-encoding produces the exact installed bytes before treating them as
/// that legacy product format.
private func cloudLegacySchemaTwoDecoder() -> JSONDecoder {
    JSONDecoder()
}

private func cloudLegacySchemaTwoEncoder() -> JSONEncoder {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
    return encoder
}

private func decodeExactLegacySchemaTwo<T: Codable>(
    _ type: T.Type, from bytes: Data, schemaVersion: (T) -> Int
) throws -> T {
    let decoded = try cloudLegacySchemaTwoDecoder().decode(type, from: bytes)
    guard schemaVersion(decoded) == 2,
          try cloudLegacySchemaTwoEncoder().encode(decoded) == bytes else {
        throw CloudDurableStoreFailure.corrupt
    }
    return decoded
}

public final class CloudFileCommandLedgerStore: CloudCommandLedgerStore, @unchecked Sendable {
    public static let maximumBytes = 16 * 1024 * 1024
    public var faultInjection: (@Sendable (CloudDurableStoreFaultPoint) throws -> Void)?
    private let file: CloudDurableFile
    private let lock = NSLock()
    private var rows: [CloudCommandLedgerKey: CloudCommandLedgerRow]?
    private var generation: UInt64 = 0

    public init(url: URL, expectedUID: UInt32 = geteuid()) throws {
        file = try CloudDurableFile(
            url: url, maximumBytes: Self.maximumBytes, expectedUID: expectedUID)
    }

    public func transaction<T>(
        _ body: (inout [CloudCommandLedgerKey: CloudCommandLedgerRow]) throws -> T
    ) throws -> T {
        lock.lock()
        defer { lock.unlock() }
        try loadIfNeeded()
        let original = rows ?? [:]
        var candidate = original
        let result = try body(&candidate)
        guard candidate != original else { return result }
        try persist(candidate)
        rows = candidate
        return result
    }

    public func persistedBytes() throws -> Data {
        lock.lock()
        defer { lock.unlock() }
        try loadIfNeeded()
        return try encode(rows ?? [:], generation: generation)
    }

    private func loadIfNeeded() throws {
        guard rows == nil else { return }
        guard let bytes = try file.load() else { rows = [:]; return }
        let envelope: CloudLedgerFileEnvelope
        let rewritesLegacyDateRepresentation: Bool
        do {
            do {
                let value = try CloudCanonicalJSON.parseStrict(bytes)
                guard case .object(let root) = value,
                      case .int(let schema)? = root["schemaVersion"] else {
                    throw CloudDurableStoreFailure.corrupt
                }
                if schema == CloudLedgerFileEnvelope.currentVersion {
                    guard Set(root.keys) == ["schemaVersion", "minimumReaderVersion", "recordKind",
                                             "generation", "rows"],
                          Self.validCurrentSchema(root) else {
                        throw CloudDurableStoreFailure.corrupt
                    }
                }
                envelope = try cloudDurableJSONDecoder().decode(
                    CloudLedgerFileEnvelope.self, from: bytes)
                rewritesLegacyDateRepresentation = false
            } catch {
                envelope = try decodeExactLegacySchemaTwo(
                    CloudLedgerFileEnvelope.self, from: bytes, schemaVersion: \CloudLedgerFileEnvelope.schemaVersion)
                rewritesLegacyDateRepresentation = true
            }
        }
        catch { try file.quarantineCurrent(); throw CloudDurableStoreFailure.corrupt }
        guard (1...CloudLedgerFileEnvelope.currentVersion).contains(envelope.schemaVersion) else {
            try file.quarantineCurrent()
            throw CloudDurableStoreFailure.unknownVersion(envelope.schemaVersion)
        }
        guard envelope.minimumReaderVersion >= 1,
              envelope.minimumReaderVersion <= CloudLedgerFileEnvelope.currentVersion,
              envelope.recordKind == "cloud_command_ledger",
              envelope.generation < UInt64.max,
              envelope.rows.allSatisfy(Self.validRow),
              Set(envelope.rows.map(\.key)).count == envelope.rows.count else {
            try file.quarantineCurrent()
            throw CloudDurableStoreFailure.corrupt
        }
        let loadedRows = Dictionary(uniqueKeysWithValues: envelope.rows.map { ($0.key, $0) })
        generation = envelope.generation
        if envelope.schemaVersion < CloudLedgerFileEnvelope.currentVersion
            || rewritesLegacyDateRepresentation {
            try faultInjection?(.recovery)
            try persist(loadedRows)
        }
        rows = loadedRows
    }

    private func persist(_ rows: [CloudCommandLedgerKey: CloudCommandLedgerRow]) throws {
        guard generation < UInt64.max else { throw CloudDurableStoreFailure.corrupt }
        let next = generation + 1
        let bytes = try encode(rows, generation: next)
        try file.commit(bytes, fault: faultInjection)
        generation = next
    }

    private func encode(
        _ rows: [CloudCommandLedgerKey: CloudCommandLedgerRow], generation: UInt64
    ) throws -> Data {
        let ordered = rows.values.sorted {
            if $0.key.viewerSender != $1.key.viewerSender {
                return $0.key.viewerSender < $1.key.viewerSender
            }
            return $0.key.requestID < $1.key.requestID
        }
        let encoder = cloudDurableJSONEncoder()
        do { return try encoder.encode(CloudLedgerFileEnvelope(generation: generation, rows: ordered)) }
        catch { throw CloudDurableStoreFailure.persist }
    }

    private static func validRow(_ row: CloudCommandLedgerRow) -> Bool {
        guard !row.key.viewerSender.isEmpty, !row.key.requestID.isEmpty,
              !row.requestSHA256.isEmpty, row.requestSHA256.count <= 64,
              !row.replyKeyID.isEmpty, !row.recipientDeviceID.isEmpty,
              row.deadlineAt.timeIntervalSinceReferenceDate.isFinite,
              row.createdAt.timeIntervalSinceReferenceDate.isFinite,
              row.expiresAt.timeIntervalSinceReferenceDate.isFinite,
              row.expiresAt >= row.createdAt else { return false }
        switch row.state {
        case .reserved:
            return row.normalizedOutcome == nil && row.effectStartedAt == nil
                && row.completedAt == nil && row.effectNotAfterContinuous != nil
        case .inProgress:
            return row.normalizedOutcome == nil && row.effectStartedAt != nil
                && row.completedAt == nil && row.effectNotAfterContinuous == nil
        case .completed:
            return row.normalizedOutcome != nil && row.effectStartedAt != nil
                && row.completedAt != nil && row.effectNotAfterContinuous == nil
        }
    }

    private static func validCurrentSchema(_ root: [String: CloudJSONValue]) -> Bool {
        guard case .array(let rows)? = root["rows"] else { return false }
        let required = Set([
            "key", "requestSHA256", "replyKeyID", "recipientDeviceID", "deadlineAt", "state",
            "createdAt", "expiresAt", "retainedElapsedMilliseconds", "elapsedBootID",
        ])
        for value in rows {
            guard case .object(let row) = value,
                  required.isSubset(of: Set(row.keys)),
                  case .object(let key)? = row["key"],
                  Set(key.keys) == ["viewerSender", "requestID"] else { return false }
            if let outcome = row["normalizedOutcome"] {
                guard case .object(let fields) = outcome,
                      Set(["code", "status"]).isSubset(of: Set(fields.keys)) else { return false }
            }
        }
        return true
    }
}

private struct CloudSpoolFileRow: Codable {
    let seq: Int64
    let channel: CloudSpoolChannel
    let logicalID: String
    let ownerID: String?
    let recipient: String
    let logicalRecordCanonicalBytes: Data
    let chargedBytes: Int
    let sealedEnvelopeBytes: Data?
    let state: CloudSpoolRowState
    let reservedAt: Date
    let reservedAtNanoseconds: Int64
    let firstSentNanoseconds: Int64?
    let attemptNotAfterNanoseconds: Int64?
    let attemptOutcome: CloudSpoolAttemptOutcome?
    let burnReason: CloudSpoolBurnReason?
    let logicalTombstoneNanoseconds: Int64?

    init(_ row: CloudSpoolRow) {
        seq = row.seq; channel = row.channel; logicalID = row.logicalID; ownerID = row.ownerID
        recipient = row.recipient; logicalRecordCanonicalBytes = row.logicalRecordCanonicalBytes
        chargedBytes = row.chargedBytes; sealedEnvelopeBytes = row.sealedEnvelopeBytes
        state = row.state; reservedAt = row.reservedAt
        reservedAtNanoseconds = row.reservedAtContinuous.cloudNanoseconds
        firstSentNanoseconds = row.firstSentContinuous?.cloudNanoseconds
        attemptNotAfterNanoseconds = row.attemptNotAfterContinuous?.cloudNanoseconds
        attemptOutcome = row.attemptOutcome; burnReason = row.burnReason
        logicalTombstoneNanoseconds = row.logicalTombstoneContinuous?.cloudNanoseconds
    }

    var row: CloudSpoolRow {
        CloudSpoolRow(
            seq: seq, channel: channel, logicalID: logicalID, ownerID: ownerID,
            recipient: recipient, logicalRecordCanonicalBytes: logicalRecordCanonicalBytes,
            chargedBytes: chargedBytes, sealedEnvelopeBytes: sealedEnvelopeBytes, state: state,
            reservedAt: reservedAt, reservedAtContinuous: .nanoseconds(reservedAtNanoseconds),
            firstSentContinuous: firstSentNanoseconds.map(Duration.nanoseconds),
            attemptNotAfterContinuous: attemptNotAfterNanoseconds.map(Duration.nanoseconds),
            attemptOutcome: attemptOutcome, burnReason: burnReason,
            logicalTombstoneContinuous: logicalTombstoneNanoseconds.map(Duration.nanoseconds))
    }
}

private struct CloudSpoolFileEnvelope: Codable {
    static let currentVersion = 2
    let schemaVersion: Int
    let minimumReaderVersion: Int
    let recordKind: String
    let generation: UInt64
    let nextSeq: Int64
    let rows: [CloudSpoolFileRow]

    init(generation: UInt64, state: CloudSpoolPersistedState) {
        schemaVersion = Self.currentVersion; minimumReaderVersion = 1
        recordKind = "cloud_outbound_spool"; self.generation = generation
        nextSeq = state.nextSeq; rows = state.rows.map(CloudSpoolFileRow.init)
    }

    private enum CodingKeys: String, CodingKey {
        case schemaVersion, minimumReaderVersion, recordKind, generation, nextSeq, rows
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        schemaVersion = try values.decode(Int.self, forKey: .schemaVersion)
        if schemaVersion == Self.currentVersion {
            minimumReaderVersion = try values.decode(Int.self, forKey: .minimumReaderVersion)
            recordKind = try values.decode(String.self, forKey: .recordKind)
            generation = try values.decode(UInt64.self, forKey: .generation)
            nextSeq = try values.decode(Int64.self, forKey: .nextSeq)
            rows = try values.decode([CloudSpoolFileRow].self, forKey: .rows)
        } else {
            minimumReaderVersion = try values.decodeIfPresent(
                Int.self, forKey: .minimumReaderVersion) ?? 1
            recordKind = try values.decodeIfPresent(String.self, forKey: .recordKind)
                ?? "cloud_outbound_spool"
            generation = try values.decodeIfPresent(UInt64.self, forKey: .generation) ?? 0
            nextSeq = try values.decodeIfPresent(Int64.self, forKey: .nextSeq) ?? 0
            rows = try values.decodeIfPresent([CloudSpoolFileRow].self, forKey: .rows) ?? []
        }
    }
}

public final class CloudFileSpoolStore: CloudSpoolStore, @unchecked Sendable {
    public static let maximumBytes = 32 * 1024 * 1024
    public var faultInjection: (@Sendable (CloudDurableStoreFaultPoint) throws -> Void)?
    private let file: CloudDurableFile
    private let lock = NSLock()
    private var state: CloudSpoolPersistedState?
    private var generation: UInt64 = 0

    public init(url: URL, expectedUID: UInt32 = geteuid()) throws {
        file = try CloudDurableFile(
            url: url, maximumBytes: Self.maximumBytes, expectedUID: expectedUID)
    }

    public func load() throws -> CloudSpoolPersistedState {
        lock.lock(); defer { lock.unlock() }
        try loadIfNeeded()
        return state ?? CloudSpoolPersistedState()
    }

    public func commit(_ candidate: CloudSpoolPersistedState) throws {
        lock.lock(); defer { lock.unlock() }
        try loadIfNeeded()
        if state == candidate { return }
        try persist(candidate)
        state = candidate
    }

    private func loadIfNeeded() throws {
        guard state == nil else { return }
        guard let bytes = try file.load() else { state = CloudSpoolPersistedState(); return }
        let envelope: CloudSpoolFileEnvelope
        let rewritesLegacyDateRepresentation: Bool
        do {
            do {
                let value = try CloudCanonicalJSON.parseStrict(bytes)
                guard case .object(let root) = value,
                      case .int(let schema)? = root["schemaVersion"] else {
                    throw CloudDurableStoreFailure.corrupt
                }
                if schema == CloudSpoolFileEnvelope.currentVersion {
                    guard Set(root.keys) == ["schemaVersion", "minimumReaderVersion", "recordKind",
                                             "generation", "nextSeq", "rows"],
                          Self.validCurrentSchema(root) else {
                        throw CloudDurableStoreFailure.corrupt
                    }
                }
                envelope = try cloudDurableJSONDecoder().decode(
                    CloudSpoolFileEnvelope.self, from: bytes)
                rewritesLegacyDateRepresentation = false
            } catch {
                envelope = try decodeExactLegacySchemaTwo(
                    CloudSpoolFileEnvelope.self, from: bytes, schemaVersion: \CloudSpoolFileEnvelope.schemaVersion)
                rewritesLegacyDateRepresentation = true
            }
        }
        catch { try file.quarantineCurrent(); throw CloudDurableStoreFailure.corrupt }
        guard (1...CloudSpoolFileEnvelope.currentVersion).contains(envelope.schemaVersion) else {
            try file.quarantineCurrent()
            throw CloudDurableStoreFailure.unknownVersion(envelope.schemaVersion)
        }
        let rowSequences = envelope.rows.map(\.seq)
        guard envelope.minimumReaderVersion >= 1,
              envelope.minimumReaderVersion <= CloudSpoolFileEnvelope.currentVersion,
              envelope.recordKind == "cloud_outbound_spool",
              envelope.generation < UInt64.max,
              envelope.nextSeq >= 0,
              envelope.nextSeq <= CloudCanonicalJSON.maximumSafeInteger,
              envelope.rows.allSatisfy(Self.validRow),
              rowSequences.allSatisfy({ $0 >= 0 && $0 < envelope.nextSeq }),
              Set(rowSequences).count == rowSequences.count,
              rowSequences == rowSequences.sorted() else {
            try file.quarantineCurrent(); throw CloudDurableStoreFailure.corrupt
        }
        let loadedState = CloudSpoolPersistedState(
            nextSeq: envelope.nextSeq, rows: envelope.rows.map(\.row))
        generation = envelope.generation
        if envelope.schemaVersion < CloudSpoolFileEnvelope.currentVersion
            || rewritesLegacyDateRepresentation {
            try faultInjection?(.recovery)
            try persist(loadedState)
        }
        state = loadedState
    }

    private func persist(_ state: CloudSpoolPersistedState) throws {
        guard generation < UInt64.max, state.nextSeq >= 0,
              state.nextSeq <= CloudCanonicalJSON.maximumSafeInteger else {
            throw CloudDurableStoreFailure.corrupt
        }
        let next = generation + 1
        let encoder = cloudDurableJSONEncoder()
        let bytes: Data
        do { bytes = try encoder.encode(CloudSpoolFileEnvelope(generation: next, state: state)) }
        catch { throw CloudDurableStoreFailure.persist }
        try file.commit(bytes, fault: faultInjection)
        generation = next
    }

    private static func validRow(_ row: CloudSpoolFileRow) -> Bool {
        guard row.seq >= 0, !row.logicalID.isEmpty, !row.recipient.isEmpty,
              row.chargedBytes >= 0,
              row.reservedAt.timeIntervalSinceReferenceDate.isFinite else { return false }
        guard let logical = try? CloudCanonicalJSON.parseStrict(row.logicalRecordCanonicalBytes),
              CloudCanonicalJSON.canonicalData(logical) == row.logicalRecordCanonicalBytes,
              CloudCanonicalJSON.chargedBytes(record: logical) == row.chargedBytes else {
            return false
        }
        switch row.state {
        case .reserved:
            return row.sealedEnvelopeBytes == nil && row.firstSentNanoseconds == nil
                && row.attemptNotAfterNanoseconds == nil && row.attemptOutcome == nil
                && row.burnReason == nil && row.logicalTombstoneNanoseconds == nil
        case .ready:
            return row.sealedEnvelopeBytes?.isEmpty == false && row.firstSentNanoseconds == nil
                && row.attemptNotAfterNanoseconds == nil && row.attemptOutcome == nil
                && row.burnReason == nil && row.logicalTombstoneNanoseconds == nil
        case .sent:
            guard row.sealedEnvelopeBytes?.isEmpty == false,
                  let first = row.firstSentNanoseconds,
                  let deadline = row.attemptNotAfterNanoseconds else { return false }
            return deadline >= first && row.attemptOutcome == nil && row.burnReason == nil
                && row.logicalTombstoneNanoseconds == nil
        case .acked, .rejected:
            // Terminal rows written before frame compaction may still carry the last frame;
            // current writers deliberately clear it after the final send. Accept both durable
            // representations, but never an explicitly empty frame.
            return row.sealedEnvelopeBytes?.isEmpty != true
                && row.firstSentNanoseconds != nil && row.attemptNotAfterNanoseconds != nil
                && row.burnReason == nil
        case .burned:
            return row.burnReason != nil
        }
    }

    private static func validCurrentSchema(_ root: [String: CloudJSONValue]) -> Bool {
        guard case .array(let rows)? = root["rows"] else { return false }
        let required = Set([
            "seq", "channel", "logicalID", "recipient", "logicalRecordCanonicalBytes",
            "chargedBytes", "state", "reservedAt", "reservedAtNanoseconds",
        ])
        return rows.allSatisfy { value in
            guard case .object(let row) = value else { return false }
            return required.isSubset(of: Set(row.keys))
        }
    }
}

public struct CloudSystemSpoolClock: CloudSpoolClock {
    public init() {}
    public var continuousNow: Duration {
        .nanoseconds(Int64(clamping: DispatchTime.now().uptimeNanoseconds))
    }
    public var wallNow: Date { Date() }
}

public final class CloudNoopSpoolMetrics: CloudSpoolMetrics, @unchecked Sendable {
    public init() {}
    public func recordRows(_: Int, runtime _: CloudSpoolRuntime) {}
    public func recordBytes(_: Int, runtime _: CloudSpoolRuntime) {}
    public func recordAdmissionRefusal(runtime _: CloudSpoolRuntime,
                                      reason _: CloudSpoolRefusalReason,
                                      scope _: CloudSpoolRefusalScope) {}
    public func recordOccupancyAlert(runtime _: CloudSpoolRuntime,
                                     dimension _: CloudSpoolOccupancyDimension,
                                     level _: CloudSpoolOccupancyAlert) {}
    public func recordLateSettleTelemetry(runtime _: CloudSpoolRuntime) {}
    public func recordStateStoreGC(store _: CloudStateStore, reason _: CloudStateStoreGCReason) {}
    public func recordStateStoreCorrupt(store _: CloudStateStore) {}
}

/// A server machine id is network identity, never a path component. Production accepts one
/// canonical ASCII grammar and encodes its exact UTF-8 bytes as lowercase hex under a domain
/// prefix. The mapping is injective even on case-insensitive and normalization-insensitive filesystems.
///
/// **The grammar is the control plane's, not a guess at it.** `api/src/lib/ids.ts` mints
/// `mac_<uuid>` — a typed prefix, an underscore, then a lowercase UUID — so an id that omitted
/// `_` admitted nothing this Mac is ever issued. It refused every real identity while the fixture
/// ids in the tests (`machine-a`, `machine-b`) kept passing, and the bridge detached on every
/// launch with no automatic retry. Separator characters are not what makes this safe in any case:
/// the hex encoding below is total over bytes, and the first/last and length bounds are what keep
/// the component from being a relative path or an overlong name.
public enum CloudMachineFilesystemNamespace {
    public static func component(for serverMachineID: String) throws -> String {
        let bytes = Array(serverMachineID.utf8)
        guard !bytes.isEmpty, bytes.count <= 128,
              bytes.first.map(isLowercaseLetterOrDigit) == true,
              bytes.last.map(isLowercaseLetterOrDigit) == true,
              bytes.allSatisfy({ isLowercaseLetterOrDigit($0) || isSeparator($0) }) else {
            throw CloudDurableStoreFailure.invalidMachineIdentity
        }
        return "machine-v1-" + bytes.map { String(format: "%02x", $0) }.joined()
    }

    private static func isLowercaseLetterOrDigit(_ byte: UInt8) -> Bool {
        (0x61...0x7a).contains(byte) || (0x30...0x39).contains(byte)
    }

    /// `-` and `_`, the two the control plane's own ids are built from.
    private static func isSeparator(_ byte: UInt8) -> Bool {
        byte == 0x2d || byte == 0x5f
    }
}

public struct CloudDurableRuntime: Sendable {
    public let ledger: CloudCommandLedger
    public let spool: CloudOutboundSpool

    public static func open(
        directory: URL, expectedUID: UInt32 = geteuid(), runtime: CloudSpoolRuntime?,
        metrics: CloudSpoolMetrics = CloudNoopSpoolMetrics(), minimumNextSequence: Int64 = 0,
        sequenceFence: any CloudSpoolSequenceFence = CloudNoopSpoolSequenceFence(),
        limits: CloudSpoolLimits = CloudSpoolLimits(),
        strictPersistedFrameValidation: Bool = false
    ) throws -> CloudDurableRuntime {
        guard minimumNextSequence >= 0,
              minimumNextSequence <= CloudCanonicalJSON.maximumSafeInteger else {
            throw CloudDurableStoreFailure.corrupt
        }
        let ledgerStore = try CloudFileCommandLedgerStore(
            url: directory.appendingPathComponent("command-ledger.json"), expectedUID: expectedUID)
        let spoolStore = try CloudFileSpoolStore(
            url: directory.appendingPathComponent("outbound-spool.json"), expectedUID: expectedUID)
        var spoolState = try spoolStore.load()
        if spoolState.nextSeq < minimumNextSequence {
            spoolState.nextSeq = minimumNextSequence
            try spoolStore.commit(spoolState)
        }
        let clocks = CloudCommandLedgerClocks(
            wallNow: { Date() },
            continuousNow: { DispatchTime.now().uptimeNanoseconds },
            bootID: { CloudDurableProcessIdentity.value })
        return CloudDurableRuntime(
            ledger: try CloudCommandLedger(store: ledgerStore, clocks: clocks),
            spool: try CloudOutboundSpool(
                store: spoolStore, clock: CloudSystemSpoolClock(), metrics: metrics,
                runtime: runtime, limits: limits, sequenceFence: sequenceFence,
                strictPersistedFrameValidation: strictPersistedFrameValidation))
    }
}

private enum CloudDurableProcessIdentity {
    static let value = UUID().uuidString.lowercased()
}

private extension Duration {
    var cloudNanoseconds: Int64 {
        let value = components
        let seconds = value.seconds.multipliedReportingOverflow(by: 1_000_000_000)
        if seconds.overflow { return value.seconds < 0 ? .min : .max }
        let nanos = value.attoseconds / 1_000_000_000
        let (sum, overflow) = seconds.partialValue.addingReportingOverflow(nanos)
        return overflow ? (seconds.partialValue < 0 ? .min : .max) : sum
    }
}
