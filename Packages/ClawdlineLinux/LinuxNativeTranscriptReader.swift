import Foundation
import ClawdlineApplication
#if canImport(Glibc)
import Glibc
#else
import Darwin
#endif

private func linuxNativeRead(_ descriptor: Int32, _ buffer: UnsafeMutableRawPointer?,
                             _ count: Int) -> Int {
    #if canImport(Glibc)
    return Glibc.read(descriptor, buffer, count)
    #else
    return Darwin.read(descriptor, buffer, count)
    #endif
}

enum LinuxNativeTranscriptReadOutcome: Equatable {
    case available(LinuxNativeTranscriptIdentity)
    case unavailable(String)
    case unknown(String)
    case limit(String)
    case busy(String)
}

struct LinuxNativeTranscriptIdentity: Equatable {
    enum Source: String, Equatable { case claudeRegistry, codexProcessFD }
    let assistant: Assistant
    let sessionID: String
    let source: Source
    let relativePath: String
    /// The process whose already-attested Session authority selected this transcript.
    let process: HostProcessIdentity
    /// The process that actually owns the writer. This differs only for a Claude parked job.
    let writerProcess: HostProcessIdentity
    let conversationMode: LinuxNativeTranscriptConversationMode
    let fileDevice: UInt64
    let fileInode: UInt64
    let fileSize: UInt64
    let fileModifiedNanoseconds: Int64
    let suffixOffset: UInt64
    let bytes: Data
}

/// Resolves native provider transcripts without guessing from cwd, dates, or recent files.
/// Every path walk begins at the configured provider HOME descriptor. Production uses procfs;
/// tests point both roots at synthetic directories and never inspect a real provider HOME.
final class LinuxNativeTranscriptReader {
    struct Limits: Equatable {
        var maximumFDs = 256
        var maximumRegistryEntries = 256
        var maximumProjectDirectories = 512
        var maximumTranscriptBytes = 8 * 1_048_576
        var maximumRegistryBytes = 256 * 1_024
        var deadline: TimeInterval = 1
    }

    struct Configuration {
        let providerHome: String
        let procRoot: String
        let serviceUID: UInt32
        let serviceGID: UInt32?
        let supplementaryGroups: Set<UInt32>?
        let clock: LinuxProcfs.Clock
        var limits = Limits()
        var monotonicNow: () -> TimeInterval = { ProcessInfo.processInfo.systemUptime }
        var beforePostValidation: (() -> Void)?
        /// Tests can model procfs ownership without requiring root. Production always uses fstat.
        var processOwner: ((pid_t) -> UInt32?)?
        /// Tests can inject credential snapshots; production parses `/proc/<pid>/status`.
        var processCredentials: ((pid_t) -> LinuxProcfs.Credentials?)?
        /// Tests can exercise the leaf-owner predicate without requiring chown privilege.
        /// Production always uses the UID returned by fstat.
        var fileOwner: ((String, UInt32) -> UInt32)?
        /// Test-only accounting hook; production leaves it nil.
        var registryOpenFileCount: ((Int) -> Void)?

        init(providerHome: String, procRoot: String = "/proc", serviceUID: UInt32,
             serviceGID: UInt32? = nil, supplementaryGroups: Set<UInt32>? = nil,
             clock: LinuxProcfs.Clock, limits: Limits = Limits()) {
            self.providerHome = providerHome
            self.procRoot = procRoot
            self.serviceUID = serviceUID
            self.serviceGID = serviceGID
            self.supplementaryGroups = supplementaryGroups
            self.clock = clock
            self.limits = limits
        }
    }

    private struct SecureFile {
        let descriptor: Int32
        let relativePath: String
        let metadata: stat
    }

    private enum SecureDirectoryResult {
        case opened(Int32)
        case missing
        case unsafe
    }

    private enum SecureOpenResult {
        case opened(SecureFile)
        case missing
        case unsafe
        case limit
    }

    private struct ClaudeRegistry: Decodable, Equatable {
        let pid: Int32
        let sessionId: String
        let cwd: String
        let procStart: String
        let peerProtocol: Int
        let kind: String?
        let jobId: String?
        let parkedJobId: String?
    }

    private struct RegistrySnapshot: Equatable {
        let names: [String]
        let bytes: [Data]
        let rows: [ClaudeRegistry]
    }

    private struct CodexMetadata: Decodable {
        struct Payload: Decodable {
            let id: String?
            let session_id: String?
            let thread_source: String?
            let cwd: String?
            let source: String?
        }
        let type: String
        let payload: Payload
    }

    private struct CodexBinding {
        let relative: String
        let sessionID: String
        let threadSource: String
        let cwd: String
        let descriptorName: String
        let metadata: stat
    }

    private enum DirectoryListing {
        case names([String])
        case limit
    }

    private let configuration: Configuration

    init(configuration: Configuration) {
        self.configuration = configuration
    }

    func read(assistant: Assistant, process expected: HostProcessIdentity, canonicalCWD: String)
        -> LinuxNativeTranscriptReadOutcome {
        let deadline = configuration.monotonicNow() + configuration.limits.deadline
        guard expected.pid > 0, let startToken = expected.startToken,
              !startToken.isEmpty else {
            return .unknown("provider process identity is incomplete")
        }
        guard let current = processIdentity(pid: expected.pid), current == expected else {
            return .unavailable("provider process incarnation is unavailable")
        }
        guard processCredentialsMatch(pid: expected.pid) else {
            return .unavailable("provider process owner is unavailable")
        }
        guard validCanonicalCWD(canonicalCWD) else {
            return .unknown("provider working directory is not canonical")
        }
        guard !expired(deadline) else { return .busy("native transcript identity deadline elapsed") }
        let home = openAbsoluteDirectory(configuration.providerHome)
        guard home >= 0 else { return .unavailable("provider HOME cannot be opened safely") }
        defer { _ = close(home) }
        guard validateDirectory(home) else {
            return .unknown("provider HOME ownership or mode is unsafe")
        }
        switch assistant {
        case .claude:
            return readClaude(home: home, expected: expected, canonicalCWD: canonicalCWD,
                              deadline: deadline)
        case .codex:
            return readCodex(home: home, expected: expected, canonicalCWD: canonicalCWD,
                             deadline: deadline)
        }
    }

    private func readClaude(home: Int32, expected: HostProcessIdentity, canonicalCWD: String,
                            deadline: TimeInterval)
        -> LinuxNativeTranscriptReadOutcome {
        let first: RegistrySnapshot
        switch registrySnapshot(home: home, deadline: deadline) {
        case .success(let value): first = value
        case .failure(let outcome): return outcome
        }
        let direct = zip(first.names, first.rows).compactMap { name, row in
            name == "\(expected.pid).json" && row.pid == expected.pid ? row : nil
        }
        guard direct.count == 1, let foreground = direct.first else {
            return direct.isEmpty
                ? .unavailable("Claude registry has no exact provider PID")
                : .unknown("Claude registry PID is ambiguous")
        }
        guard claudeRegistryBinds(foreground, process: expected, canonicalCWD: canonicalCWD) else {
            return .unknown("Claude registry process or working-directory identity is mismatched")
        }
        var bound = foreground
        var boundProcess = expected
        if let parked = foreground.parkedJobId {
            let backgrounds = zip(first.names, first.rows).compactMap { name, row in
                row.kind == "bg" && row.jobId == parked && name == "\(row.pid).json"
                    && row.pid != expected.pid && row.sessionId != foreground.sessionId ? row : nil
            }
            guard backgrounds.count == 1, let background = backgrounds.first else {
                return .unknown("Claude parked-background registry binding is ambiguous")
            }
            guard let identity = processIdentity(pid: background.pid) else {
                return .unavailable("Claude background process is unavailable")
            }
            guard processCredentialsMatch(pid: background.pid) else {
                return .unknown("Claude background process owner is mismatched")
            }
            guard claudeRegistryBinds(background, process: identity,
                                      canonicalCWD: canonicalCWD),
                  first.names.contains("\(background.pid).json") else {
                return .unknown("Claude background registry identity is mismatched")
            }
            bound = background
            boundProcess = identity
        }
        guard validIdentifier(bound.sessionId) else {
            return .unknown("Claude registry session identity is malformed")
        }
        let slug = projectSlug(canonicalCWD)
        guard !slug.isEmpty, safeComponent(slug) else {
            return .unknown("Claude project transcript slug is invalid")
        }
        let relative = ".claude/projects/\(slug)/\(bound.sessionId).jsonl"
        return finish(home: home, relative: relative, assistant: .claude,
                      sessionID: bound.sessionId, expected: expected,
                      additionalProcess: boundProcess == expected ? nil : boundProcess,
                      registry: first, deadline: deadline)
    }

    private func readCodex(home: Int32, expected: HostProcessIdentity, canonicalCWD: String,
                           deadline: TimeInterval)
        -> LinuxNativeTranscriptReadOutcome {
        let fdDirectory = configuration.procRoot + "/\(expected.pid)/fd"
        let fdRoot: Int32 = fdDirectory.withCString {
            open($0, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
        }
        guard fdRoot >= 0 else {
            return .unavailable("Codex process descriptors are unavailable")
        }
        // Keep this exact directory open through final validation. Looking it up by pathname
        // again would let PID reuse silently move validation to a different process incarnation.
        defer { _ = close(fdRoot) }
        let listing = listDirectory(fdRoot, maximumEntries: configuration.limits.maximumFDs)
        let names: [String]
        switch listing {
        case .names(let value): names = value
        case .limit: return .limit("Codex process descriptor limit exceeded")
        case nil: return .unavailable("Codex process descriptors are unavailable")
        }
        let numeric = names.filter { asciiDigits($0) }.sorted()
        var matches: [CodexBinding] = []
        for name in numeric {
            guard !expired(deadline) else {
                return .busy("native transcript identity deadline elapsed")
            }
            guard let target = descriptorTarget(directory: fdRoot, name: name),
                  target.hasPrefix(configuration.providerHome + "/"),
                  !target.hasSuffix(" (deleted)") else { continue }
            let relative = String(target.dropFirst(configuration.providerHome.count + 1))
            guard validCodexRolloutPath(relative) else {
                continue
            }
            let file: SecureFile
            switch openSecureResult(home: home, relative: relative, maximumBytes: Int.max) {
            case .opened(let value): file = value
            case .missing: continue
            case .unsafe: return .unknown("Codex transcript file evidence is unsafe")
            case .limit: return .limit("Codex transcript file byte limit exceeded")
            }
            var linkMetadata = stat()
            let bound = name.withCString { fstatat(fdRoot, $0, &linkMetadata, 0) } == 0
                && sameObject(file.metadata, linkMetadata)
            let prefix = bound ? readPrefix(file.descriptor, maximumBytes: 65_536) : nil
            _ = close(file.descriptor)
            guard bound, let prefix,
                  let line = prefix.split(separator: 0x0a, maxSplits: 1).first,
                  let metadata = try? JSONDecoder().decode(CodexMetadata.self, from: Data(line)),
                  metadata.type == "session_meta",
                  metadata.payload.thread_source == "user",
                  metadata.payload.source == nil || metadata.payload.source == "cli",
                  metadata.payload.cwd == canonicalCWD,
                  let threadSource = metadata.payload.thread_source,
                  let cwd = metadata.payload.cwd,
                  let sessionID = metadata.payload.session_id,
                  validIdentifier(sessionID) else { continue }
            matches.append(CodexBinding(relative: relative, sessionID: sessionID,
                                        threadSource: threadSource, cwd: cwd, descriptorName: name,
                                        metadata: file.metadata))
        }
        guard matches.count == 1, let match = matches.first else {
            return matches.isEmpty
                ? .unavailable("Codex has no exact transcript fd with thread_source identity")
                : .unknown("Codex transcript fd identity is ambiguous")
        }
        return finish(home: home, relative: match.relative, assistant: .codex,
                      sessionID: match.sessionID, expected: expected, additionalProcess: nil,
                      registry: nil, deadline: deadline, codexFDDirectory: fdRoot,
                      codexBinding: match)
    }

    private func finish(home: Int32, relative: String, assistant: Assistant,
                        sessionID: String, expected: HostProcessIdentity,
                        additionalProcess: HostProcessIdentity?, registry: RegistrySnapshot?,
                        deadline: TimeInterval, codexFDDirectory: Int32? = nil,
                        codexBinding: CodexBinding? = nil) -> LinuxNativeTranscriptReadOutcome {
        let file: SecureFile
        switch openSecureResult(home: home, relative: relative, maximumBytes: Int.max) {
        case .opened(let value): file = value
        case .missing: return .unavailable("native transcript is unavailable")
        case .unsafe: return .unknown("native transcript file evidence is unsafe")
        case .limit: return .limit("native transcript byte limit exceeded")
        }
        defer { _ = close(file.descriptor) }
        if let codexBinding, !sameMetadata(file.metadata, codexBinding.metadata) {
            return .busy("Codex transcript changed after descriptor selection")
        }
        if let codexBinding,
           !codexDescriptorIdentifies(file.descriptor, binding: codexBinding) {
            return .busy("Codex transcript identity changed during read")
        }
        guard let firstRead = readSuffix(
            file, maximumBytes: configuration.limits.maximumTranscriptBytes,
            deadline: deadline) else {
            return .busy("native transcript changed during bounded read")
        }
        let bytes = firstRead.bytes
        configuration.beforePostValidation?()
        guard !expired(deadline) else { return .busy("native transcript identity deadline elapsed") }
        var after = stat()
        guard fstat(file.descriptor, &after) == 0, sameMetadata(file.metadata, after) else {
            return .busy("native transcript metadata changed during read")
        }
        guard case .opened(let reopened) = openSecureResult(
            home: home, relative: relative, maximumBytes: Int.max) else {
            return .busy("native transcript path changed during read")
        }
        let postRead = readSuffix(
            reopened, maximumBytes: configuration.limits.maximumTranscriptBytes,
            deadline: deadline)
        let codexIdentityStable = codexBinding.map {
            codexDescriptorIdentifies(reopened.descriptor, binding: $0)
        } ?? true
        let pathStable = sameMetadata(file.metadata, reopened.metadata)
        _ = close(reopened.descriptor)
        guard pathStable else { return .busy("native transcript path changed during read") }
        guard postRead?.offset == firstRead.offset, postRead?.bytes == bytes else {
            return .busy("native transcript content changed during read")
        }
        guard codexIdentityStable else { return .busy("Codex transcript identity changed during read") }
        guard processIdentity(pid: expected.pid) == expected,
              processCredentialsMatch(pid: expected.pid),
              additionalProcess.map({ processIdentity(pid: $0.pid) == $0
                  && processCredentialsMatch(pid: $0.pid) }) ?? true else {
            return .busy("provider process identity changed during read")
        }
        if let registry {
            guard case .success(let second) = registrySnapshot(home: home, deadline: deadline),
                  second == registry else {
                return .busy("Claude registry changed during read")
            }
        }
        if let codexFDDirectory, let codexBinding {
            guard codexFDStillBinds(directory: codexFDDirectory, binding: codexBinding,
                                    metadata: file.metadata) else {
                return .busy("Codex process descriptor changed during read")
            }
        }
        return .available(LinuxNativeTranscriptIdentity(
            assistant: assistant, sessionID: sessionID, source: assistant == .claude
                ? .claudeRegistry : .codexProcessFD,
            relativePath: relative, process: expected,
            writerProcess: additionalProcess ?? expected, conversationMode: .root,
            fileDevice: UInt64(after.st_dev), fileInode: UInt64(after.st_ino),
            fileSize: UInt64(after.st_size), fileModifiedNanoseconds: modifiedNanoseconds(after),
            suffixOffset: firstRead.offset, bytes: bytes))
    }

    private func registrySnapshot(home: Int32, deadline: TimeInterval)
        -> Result<RegistrySnapshot, LinuxNativeTranscriptReadOutcome> {
        let directory: Int32
        switch openDirectoryResult(home: home, relative: ".claude/sessions") {
        case .opened(let value): directory = value
        case .missing:
            return .failure(.unavailable("Claude registry directory is unavailable"))
        case .unsafe:
            return .failure(.unknown("Claude registry directory evidence is unsafe"))
        }
        defer { _ = close(directory) }
        guard let listing = listDirectory(
            directory, maximumEntries: configuration.limits.maximumRegistryEntries) else {
            return .failure(.unavailable("Claude registry directory cannot be enumerated"))
        }
        guard case .names(let listed) = listing else {
            return .failure(.limit("Claude registry entry limit exceeded"))
        }
        let names = listed.filter { $0.hasSuffix(".json") }.sorted()
        var bytes: [Data] = []
        var rows: [ClaudeRegistry] = []
        var aggregateBytes = 0
        var openEntryDescriptors = 0
        for name in names {
            guard !expired(deadline) else {
                return .failure(.busy("native transcript identity deadline elapsed"))
            }
            guard safeComponent(name) else {
                return .failure(.unknown("Claude registry entry is unsafe"))
            }
            let file: SecureFile
            switch openSecureResult(
                parent: directory, name: name, relative: ".claude/sessions/" + name,
                maximumBytes: configuration.limits.maximumRegistryBytes) {
            case .opened(let value): file = value
            case .limit:
                return .failure(.limit("Claude registry entry byte limit exceeded"))
            case .missing:
                return .failure(.busy("Claude registry entry changed during read"))
            case .unsafe:
                return .failure(.unknown("Claude registry entry is unsafe"))
            }
            openEntryDescriptors += 1
            configuration.registryOpenFileCount?(openEntryDescriptors)
            let data = readExactly(file, deadline: deadline)
            _ = close(file.descriptor)
            openEntryDescriptors -= 1
            configuration.registryOpenFileCount?(openEntryDescriptors)
            guard let data,
                  !data.isEmpty,
                  let row = try? JSONDecoder().decode(ClaudeRegistry.self, from: data),
                  row.pid > 0,
                  validIdentifier(row.sessionId),
                  row.kind.map({ !$0.isEmpty }) ?? true,
                  row.jobId.map(validIdentifier) ?? true,
                  row.parkedJobId.map(validIdentifier) ?? true else {
                return .failure(.unknown("Claude registry entry is malformed"))
            }
            aggregateBytes += data.count
            guard aggregateBytes <= configuration.limits.maximumRegistryBytes else {
                return .failure(.limit("Claude registry byte limit exceeded"))
            }
            bytes.append(data); rows.append(row)
        }
        return .success(RegistrySnapshot(names: names, bytes: bytes, rows: rows))
    }

    private func openDirectoryResult(home: Int32, relative: String) -> SecureDirectoryResult {
        var current = dup(home)
        guard current >= 0 else { return .unsafe }
        for component in relative.split(separator: "/").map(String.init) {
            guard safeComponent(component) else { _ = close(current); return .unsafe }
            let next = component.withCString {
                openat(current, $0, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
            }
            let failure = errno
            _ = close(current)
            guard next >= 0 else { return failure == ENOENT ? .missing : .unsafe }
            guard validateDirectory(next) else { _ = close(next); return .unsafe }
            current = next
        }
        return .opened(current)
    }

    private func openAbsoluteDirectory(_ path: String) -> Int32 {
        guard path.hasPrefix("/"), !path.contains("//") else { return -1 }
        var current = open("/", O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
        guard current >= 0 else { return -1 }
        for component in path.split(separator: "/").map(String.init) {
            guard safeComponent(component) else { _ = close(current); return -1 }
            let next = component.withCString {
                openat(current, $0, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
            }
            _ = close(current)
            guard next >= 0 else { return -1 }
            current = next
        }
        return current
    }

    private func openSecureResult(home: Int32, relative: String,
                                  maximumBytes: Int) -> SecureOpenResult {
        let pieces = relative.split(separator: "/").map(String.init)
        guard let name = pieces.last, pieces.allSatisfy(safeComponent) else { return .unsafe }
        let parentPath = pieces.dropLast().joined(separator: "/")
        let parent: Int32
        switch openDirectoryResult(home: home, relative: parentPath) {
        case .opened(let value): parent = value
        case .missing: return .missing
        case .unsafe: return .unsafe
        }
        defer { _ = close(parent) }
        return openSecureResult(parent: parent, name: name, relative: relative,
                                maximumBytes: maximumBytes)
    }

    private func openSecureResult(parent: Int32, name: String, relative: String,
                                  maximumBytes: Int) -> SecureOpenResult {
        let descriptor = name.withCString {
            openat(parent, $0, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC)
        }
        if descriptor < 0 {
            let failure = errno
            if failure == ENOENT { return .missing }
            var linked = stat()
            let exists = name.withCString {
                fstatat(parent, $0, &linked, AT_SYMLINK_NOFOLLOW)
            } == 0
            return exists ? .unsafe : (failure == ENOENT ? .missing : .unsafe)
        }
        var metadata = stat()
        guard fstat(descriptor, &metadata) == 0 else {
            _ = close(descriptor); return .unsafe
        }
        let owner = configuration.fileOwner?(relative, UInt32(metadata.st_uid))
            ?? UInt32(metadata.st_uid)
        guard metadata.st_mode & S_IFMT == S_IFREG,
              owner == configuration.serviceUID,
              metadata.st_mode & 0o077 == 0,
              metadata.st_nlink == 1,
              metadata.st_size >= 0 else {
            _ = close(descriptor); return .unsafe
        }
        guard metadata.st_size <= maximumBytes else {
            _ = close(descriptor); return .limit
        }
        return .opened(SecureFile(
            descriptor: descriptor, relativePath: relative, metadata: metadata))
    }

    private func readExactly(_ file: SecureFile, deadline: TimeInterval) -> Data? {
        guard lseek(file.descriptor, 0, SEEK_SET) >= 0 else { return nil }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 16_384)
        while data.count < Int(file.metadata.st_size) {
            guard !expired(deadline) else { return nil }
            let count = buffer.withUnsafeMutableBytes {
                linuxNativeRead(file.descriptor, $0.baseAddress,
                                min($0.count, Int(file.metadata.st_size) - data.count))
            }
            if count < 0, errno == EINTR { continue }
            guard count > 0 else { return nil }
            data.append(contentsOf: buffer.prefix(count))
        }
        var extra: UInt8 = 0
        while true {
            let count = linuxNativeRead(file.descriptor, &extra, 1)
            if count < 0, errno == EINTR { continue }
            guard count == 0 else { return nil }
            break
        }
        return data
    }

    private func readSuffix(_ file: SecureFile, maximumBytes: Int, deadline: TimeInterval)
        -> (bytes: Data, offset: UInt64)? {
        guard maximumBytes >= 0, file.metadata.st_size >= 0 else { return nil }
        let byteCount = min(Int(file.metadata.st_size), maximumBytes)
        let offset = Int(file.metadata.st_size) - byteCount
        guard lseek(file.descriptor, off_t(offset), SEEK_SET) == off_t(offset) else { return nil }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 16_384)
        while data.count < byteCount {
            guard !expired(deadline) else { return nil }
            let count = buffer.withUnsafeMutableBytes {
                linuxNativeRead(file.descriptor, $0.baseAddress,
                                min($0.count, byteCount - data.count))
            }
            if count < 0, errno == EINTR { continue }
            guard count > 0 else { return nil }
            data.append(contentsOf: buffer.prefix(count))
        }
        var extra: UInt8 = 0
        while true {
            let count = linuxNativeRead(file.descriptor, &extra, 1)
            if count < 0, errno == EINTR { continue }
            guard count == 0 else { return nil }
            break
        }
        return (data, UInt64(offset))
    }

    private func readPrefix(_ descriptor: Int32, maximumBytes: Int) -> Data? {
        guard lseek(descriptor, 0, SEEK_SET) >= 0 else { return nil }
        var bytes = [UInt8](repeating: 0, count: maximumBytes)
        let count = bytes.withUnsafeMutableBytes {
            linuxNativeRead(descriptor, $0.baseAddress, $0.count)
        }
        guard count > 0 else { return nil }
        return Data(bytes.prefix(count))
    }

    private func processIdentity(pid: pid_t) -> HostProcessIdentity? {
        guard let text = try? String(contentsOfFile: configuration.procRoot + "/\(pid)/stat",
                                     encoding: .utf8) else { return nil }
        return LinuxProcfs.parseStat(text, pid: pid, clock: configuration.clock)
    }

    private func processOwner(pid: pid_t) -> UInt32? {
        if let injected = configuration.processOwner { return injected(pid) }
        let path = configuration.procRoot + "/\(pid)"
        let descriptor = path.withCString {
            open($0, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
        }
        guard descriptor >= 0 else { return nil }
        defer { _ = close(descriptor) }
        var metadata = stat()
        guard fstat(descriptor, &metadata) == 0 else { return nil }
        return metadata.st_uid
    }

    private func processCredentialsMatch(pid: pid_t) -> Bool {
        guard processOwner(pid: pid) == configuration.serviceUID else { return false }
        guard let serviceGID = configuration.serviceGID,
              let supplementaryGroups = configuration.supplementaryGroups else { return true }
        let credentials: LinuxProcfs.Credentials?
        if let injected = configuration.processCredentials {
            credentials = injected(pid)
        } else {
            let path = configuration.procRoot + "/\(pid)/status"
            credentials = (try? String(contentsOfFile: path, encoding: .utf8))
                .flatMap(LinuxProcfs.parseCredentials)
        }
        guard let credentials else { return false }
        return credentials.effectiveUID == configuration.serviceUID
            && credentials.effectiveGID == serviceGID
            && credentials.supplementaryGroups == supplementaryGroups
    }

    private func descriptorTarget(directory: Int32, name: String) -> String? {
        guard safeComponent(name) else { return nil }
        var bytes = [CChar](repeating: 0, count: 16_384)
        let count = name.withCString { readlinkat(directory, $0, &bytes, bytes.count - 1) }
        guard count > 0, count < bytes.count - 1 else { return nil }
        bytes[Int(count)] = 0
        return String(cString: bytes)
    }

    private func codexMetadata(in bytes: Data) -> CodexMetadata? {
        guard let line = bytes.split(separator: 0x0a, maxSplits: 1).first else { return nil }
        return try? JSONDecoder().decode(CodexMetadata.self, from: Data(line))
    }

    private func codexBytes(_ bytes: Data, matchSessionID: String,
                            threadSource: String, cwd: String? = nil) -> Bool {
        guard let metadata = codexMetadata(in: bytes), metadata.type == "session_meta",
              metadata.payload.session_id == matchSessionID,
              metadata.payload.thread_source == threadSource,
              metadata.payload.source == nil || metadata.payload.source == "cli",
              cwd.map({ metadata.payload.cwd == $0 }) ?? true else { return false }
        return true
    }

    private func codexDescriptorIdentifies(_ descriptor: Int32, binding: CodexBinding) -> Bool {
        guard let prefix = readPrefix(descriptor, maximumBytes: 65_536) else { return false }
        return codexBytes(prefix, matchSessionID: binding.sessionID,
                          threadSource: binding.threadSource, cwd: binding.cwd)
    }

    private func codexFDStillBinds(directory: Int32, binding: CodexBinding,
                                   metadata: stat) -> Bool {
        var linked = stat()
        let status = binding.descriptorName.withCString {
            fstatat(directory, $0, &linked, 0)
        }
        guard status == 0, sameObject(metadata, linked),
              sameObject(binding.metadata, linked),
              descriptorTarget(directory: directory, name: binding.descriptorName)
                == configuration.providerHome + "/" + binding.relative else { return false }
        return true
    }

    private func listDirectory(_ descriptor: Int32, maximumEntries: Int) -> DirectoryListing? {
        guard maximumEntries >= 0 else { return .limit }
        let duplicate = dup(descriptor)
        guard duplicate >= 0, let directory = fdopendir(duplicate) else {
            if duplicate >= 0 { _ = close(duplicate) }
            return nil
        }
        defer { closedir(directory) }
        var names: [String] = []
        while let entry = readdir(directory) {
            let name = withUnsafePointer(to: entry.pointee.d_name) { pointer -> String in
                pointer.withMemoryRebound(to: CChar.self,
                                          capacity: MemoryLayout.size(ofValue: entry.pointee.d_name)) {
                    String(cString: $0)
                }
            }
            if name != "." && name != ".." {
                guard names.count < maximumEntries else { return .limit }
                names.append(name)
            }
        }
        return .names(names)
    }

    private func validateDirectory(_ descriptor: Int32) -> Bool {
        var metadata = stat()
        return fstat(descriptor, &metadata) == 0
            && metadata.st_mode & S_IFMT == S_IFDIR
            && metadata.st_uid == configuration.serviceUID
            && metadata.st_mode & 0o022 == 0
    }

    private func sameObject(_ lhs: stat, _ rhs: stat) -> Bool {
        lhs.st_dev == rhs.st_dev && lhs.st_ino == rhs.st_ino
    }

    private func sameMetadata(_ lhs: stat, _ rhs: stat) -> Bool {
        sameObject(lhs, rhs) && lhs.st_mode == rhs.st_mode && lhs.st_uid == rhs.st_uid
            && lhs.st_gid == rhs.st_gid && lhs.st_nlink == rhs.st_nlink
            && lhs.st_size == rhs.st_size
    }

    private func modifiedNanoseconds(_ metadata: stat) -> Int64 {
        #if canImport(Glibc)
        return Int64(metadata.st_mtim.tv_sec) * 1_000_000_000 + Int64(metadata.st_mtim.tv_nsec)
        #else
        return Int64(metadata.st_mtimespec.tv_sec) * 1_000_000_000
            + Int64(metadata.st_mtimespec.tv_nsec)
        #endif
    }

    private func validCanonicalCWD(_ value: String) -> Bool {
        value.hasPrefix("/") && !value.contains("//") && !value.contains("\0")
            && URL(fileURLWithPath: value).standardizedFileURL.path == value
    }

    private func projectSlug(_ path: String) -> String {
        var out = ""
        out.reserveCapacity(path.utf16.count)
        for unit in path.utf16 {
            let ascii = (48...57).contains(unit) || (65...90).contains(unit)
                || (97...122).contains(unit)
            out.append(ascii ? Character(UnicodeScalar(UInt8(unit))) : "-")
        }
        return out
    }

    private func claudeRegistryBinds(_ row: ClaudeRegistry, process: HostProcessIdentity,
                                     canonicalCWD: String) -> Bool {
        guard row.peerProtocol == 1, row.cwd == canonicalCWD,
              let started = claudeProcessStart(row.procStart) else { return false }
        return abs(started.timeIntervalSince(process.processStart)) < 1
    }

    private func claudeProcessStart(_ value: String) -> Date? {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        formatter.dateFormat = "EEE MMM d HH:mm:ss yyyy"
        return formatter.date(from: value)
    }

    private func validCodexRolloutPath(_ relative: String) -> Bool {
        let parts = relative.split(separator: "/").map(String.init)
        guard parts.count == 6, parts[0] == ".codex", parts[1] == "sessions",
              parts[2].count == 4, parts[3].count == 2, parts[4].count == 2,
              parts[2...4].allSatisfy(asciiDigits),
              parts[5].hasPrefix("rollout-"), parts[5].hasSuffix(".jsonl"),
              parts[5].count > "rollout-.jsonl".count else { return false }
        guard let year = Int(parts[2]), let month = Int(parts[3]), let day = Int(parts[4]),
              (1...9_999).contains(year), (1...12).contains(month), (1...31).contains(day) else {
            return false
        }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(secondsFromGMT: 0)!
        guard let date = calendar.date(from: DateComponents(
            calendar: calendar, timeZone: calendar.timeZone,
            year: year, month: month, day: day)) else { return false }
        let components = calendar.dateComponents([.year, .month, .day], from: date)
        return components.year == year && components.month == month && components.day == day
    }

    private func asciiDigits(_ value: String) -> Bool {
        !value.isEmpty && value.utf8.allSatisfy { (48...57).contains($0) }
    }

    private func safeComponent(_ value: String) -> Bool {
        !value.isEmpty && value != "." && value != ".." && !value.contains("/")
            && !value.contains("\0") && value.utf8.count <= 255
    }

    private func validIdentifier(_ value: String) -> Bool {
        !value.isEmpty && value.utf8.count <= 256 && !value.contains("/") && !value.contains("\0")
    }

    private func expired(_ deadline: TimeInterval) -> Bool {
        configuration.monotonicNow() > deadline
    }
}

extension LinuxNativeTranscriptReadOutcome: Error {}
