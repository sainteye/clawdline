import Foundation
import ClawdlineApplication

enum LinuxRuntimeFailureCode: String, Codable {
    case requiresNonRoot = "requires_non_root"
    case identityMismatch = "service_identity_mismatch"
    case unsafePath = "unsafe_runtime_path"
    case commandUnavailable = "command_unavailable"
    case capabilityUnavailable = "capability_unavailable"
    case commandFailed = "terminal_command_failed"
    case commandTimeout = "terminal_timeout"
    case outputLimit = "terminal_output_limit"
    case inventoryLimit = "terminal_capacity"
    case malformedReply = "malformed_terminal_reply"
    case processIdentity = "process_identity_changed"
    case fileFailure = "contained_file_failure"
}

struct LinuxRuntimeFailure: Error, Codable, Equatable {
    let code: LinuxRuntimeFailureCode
    let message: String
}

enum LinuxEffectCertainty: String, Codable, Equatable {
    case noEffect = "no_effect"
    case compensated
    case partial
    case unknown
}

enum LinuxEffectCheckpoint: String, Codable, Equatable {
    case none
    case sessionCreated = "session_created"
    case textPasted = "text_pasted"
    case submitRequested = "submit_requested"
}

/// A lower adapter may know more than `Error` can ordinarily carry about an effect that happened
/// before failure. The lifecycle layer turns this into its public progress-bearing failure receipt.
struct LinuxTerminalEffectFailure: Error, Equatable {
    let failure: LinuxRuntimeFailure
    let certainty: LinuxEffectCertainty
    let checkpoint: LinuxEffectCheckpoint
    let sessionID: String?
    let tty: String?
}

struct LinuxCommandReceipt {
    let status: Int32
    let stdout: Data
    let stderr: Data
}

enum LinuxSendEffectResult: Equatable {
    case submitted
    case pastedNotSubmitted
}

/// Bounded subprocess execution for the Linux leaves. Provider input is accepted only as stdin;
/// the runner never logs argv, environment, stdin, stdout, or stderr.
final class LinuxCommandRunner {
    let environment: [String: String]

    init(environment: [String: String]) {
        self.environment = environment
    }

    func run(executable: String, arguments: [String], input: Data? = nil,
             timeout: TimeInterval, maximumOutputBytes: Int = 1_048_576) throws -> LinuxCommandReceipt {
        guard executable.hasPrefix("/"), timeout > 0, timeout <= 30,
              maximumOutputBytes > 0, maximumOutputBytes <= 4 * 1_048_576 else {
            throw LinuxRuntimeFailure(code: .commandUnavailable,
                                      message: "The bounded command configuration is invalid.")
        }
        let process = Process()
        process.executableURL = URL(fileURLWithPath: executable)
        process.arguments = arguments
        process.environment = environment
        let stdout = Pipe()
        let stderr = Pipe()
        process.standardOutput = stdout
        process.standardError = stderr
        let stdin = input.map { _ in Pipe() }
        if let stdin { process.standardInput = stdin }
        do { try process.run() } catch {
            throw LinuxRuntimeFailure(code: .commandUnavailable,
                                      message: "The configured runtime command could not be executed.")
        }
        // The parent must not retain the child's pipe ends; otherwise EOF can never be evidence
        // that the direct child closed them. Every parent-owned descriptor is nonblocking, so a
        // child that refuses stdin or fills either output stream cannot stop the deadline loop.
        try? stdout.fileHandleForWriting.close()
        try? stderr.fileHandleForWriting.close()
        if let stdin { try? stdin.fileHandleForReading.close() }
        let outFD = stdout.fileHandleForReading.fileDescriptor
        let errFD = stderr.fileHandleForReading.fileDescriptor
        let inFD = stdin?.fileHandleForWriting.fileDescriptor
        for descriptor in [outFD, errFD] + (inFD.map { [$0] } ?? []) {
            let flags = fcntl(descriptor, F_GETFL)
            if flags >= 0 { _ = fcntl(descriptor, F_SETFL, flags | O_NONBLOCK) }
        }
        defer {
            try? stdout.fileHandleForReading.close()
            try? stderr.fileHandleForReading.close()
            if let stdin { try? stdin.fileHandleForWriting.close() }
        }

        let deadline = ProcessInfo.processInfo.systemUptime + timeout
        var out = Data()
        var err = Data()
        var aggregateOutputBytes = 0
        var inputOffset = 0
        var inputClosed = input == nil
        var outputExceeded = false
        var timedOut = false

        func drain(_ descriptor: Int32, standardError: Bool) {
            var buffer = [UInt8](repeating: 0, count: 16_384)
            while !outputExceeded {
                let count = buffer.withUnsafeMutableBytes {
                    read(descriptor, $0.baseAddress, $0.count)
                }
                if count > 0 {
                    guard aggregateOutputBytes + count <= maximumOutputBytes else {
                        outputExceeded = true
                        return
                    }
                    aggregateOutputBytes += count
                    if standardError { err.append(contentsOf: buffer.prefix(count)) }
                    else { out.append(contentsOf: buffer.prefix(count)) }
                } else if count == 0 || errno == EAGAIN || errno == EWOULDBLOCK {
                    return
                } else if errno != EINTR {
                    return
                }
            }
        }

        while true {
            drain(outFD, standardError: false)
            drain(errFD, standardError: true)
            if outputExceeded { break }

            if !inputClosed, let input, let inFD {
                if inputOffset == input.count {
                    try? stdin?.fileHandleForWriting.close()
                    inputClosed = true
                } else {
                    let written = input.withUnsafeBytes { raw -> Int in
                        write(inFD, raw.baseAddress!.advanced(by: inputOffset),
                              input.count - inputOffset)
                    }
                    if written > 0 {
                        inputOffset += written
                    } else if written < 0, errno == EPIPE {
                        try? stdin?.fileHandleForWriting.close()
                        inputClosed = true
                    }
                }
            }

            if !process.isRunning {
                // Drain bytes the direct child wrote immediately before exit, then close rather
                // than waiting on a descendant that inherited either pipe.
                drain(outFD, standardError: false)
                drain(errFD, standardError: true)
                break
            }
            let remaining = deadline - ProcessInfo.processInfo.systemUptime
            if remaining <= 0 {
                timedOut = true
                break
            }
            var descriptors = [
                pollfd(fd: outFD, events: Int16(POLLIN), revents: 0),
                pollfd(fd: errFD, events: Int16(POLLIN), revents: 0),
            ]
            if !inputClosed, let inFD {
                descriptors.append(pollfd(fd: inFD, events: Int16(POLLOUT), revents: 0))
            }
            _ = poll(&descriptors, nfds_t(descriptors.count),
                     Int32(min(20, max(1, remaining * 1_000))))
        }

        if !inputClosed { try? stdin?.fileHandleForWriting.close() }
        if outputExceeded || timedOut {
            terminateAndReap(process)
            throw LinuxRuntimeFailure(
                code: outputExceeded ? .outputLimit : .commandTimeout,
                message: outputExceeded
                    ? "The terminal command exceeded its bounded aggregate output limit."
                    : "The terminal command exceeded its bounded deadline.")
        }
        return LinuxCommandReceipt(status: process.terminationStatus, stdout: out, stderr: err)
    }

    /// Neither timeout handling nor output-ceiling handling is allowed to introduce a new
    /// unbounded wait. Foundation reaps the direct child as `isRunning` is sampled; descendants
    /// that retained a pipe are disconnected by the caller's deferred close above.
    private func terminateAndReap(_ process: Process) {
        if process.isRunning { process.terminate() }
        var deadline = ProcessInfo.processInfo.systemUptime + 0.2
        while process.isRunning, ProcessInfo.processInfo.systemUptime < deadline { usleep(10_000) }
        if process.isRunning { _ = kill(process.processIdentifier, SIGKILL) }
        deadline = ProcessInfo.processInfo.systemUptime + 0.5
        while process.isRunning, ProcessInfo.processInfo.systemUptime < deadline { usleep(10_000) }
    }
}

struct LinuxRuntimeLayout: Equatable {
    let state: String
    let home: String
    let runtime: String
    let projects: String
    let secrets: String
    let temporary: String
    let uid: UInt32
    let gid: UInt32

    static func prepare(stateDirectory: String, expectedUID: UInt32? = nil,
                        expectedGID: UInt32? = nil) throws -> LinuxRuntimeLayout {
        let uid = geteuid()
        let gid = getegid()
        guard uid != 0 else {
            throw LinuxRuntimeFailure(code: .requiresNonRoot,
                                      message: "ClawdlineLinux refuses to run provider sessions as root.")
        }
        guard expectedUID.map({ $0 == uid }) ?? true,
              expectedGID.map({ $0 == gid }) ?? true else {
            throw LinuxRuntimeFailure(code: .identityMismatch,
                                      message: "The configured service uid or gid does not match this process.")
        }
        guard ProjectRootPolicy.isLexicallySafeAbsolute(stateDirectory) else {
            throw LinuxRuntimeFailure(code: .unsafePath,
                                      message: "The state directory must be one canonical absolute path.")
        }

        _ = umask(0o077)
        let layout = LinuxRuntimeLayout(
            state: stateDirectory,
            home: stateDirectory + "/home",
            runtime: stateDirectory + "/runtime",
            // Project bytes are deliberately a sibling of daemon control state. A root beneath
            // `state` would make an ancestor registration a direct path to secrets and sockets.
            projects: stateDirectory + "-projects",
            secrets: stateDirectory + "/secrets",
            temporary: stateDirectory + "/tmp",
            uid: uid,
            gid: gid)
        for path in [layout.state, layout.home, layout.runtime, layout.projects,
                     layout.secrets, layout.temporary] {
            try FileManager.default.createDirectory(atPath: path,
                                                    withIntermediateDirectories: true,
                                                    attributes: [.posixPermissions: 0o700])
            var metadata = stat()
            let pathEvidence = try LinuxProjectRootInspector().inspectProjectRoot(at: path)
            guard lstat(path, &metadata) == 0,
                  metadata.st_mode & S_IFMT == S_IFDIR,
                  metadata.st_uid == uid, metadata.st_gid == gid,
                  metadata.st_mode & 0o077 == 0,
                  !pathEvidence.containsSymbolicLink,
                  pathEvidence.canonicalPath == path else {
                throw LinuxRuntimeFailure(code: .unsafePath,
                                          message: "A runtime directory is linked, shared, or owned by another identity.")
            }
        }
        return layout
    }
}

/// Linux evidence for Application's canonical allowlist policy. Every existing component is
/// checked with `lstat`; resolving to the same destination does not excuse a symlink component.
struct LinuxProjectRootInspector: ProjectRootInspecting {
    func inspectProjectRoot(at path: String) throws -> ProjectRootEvidence {
        guard ProjectRootPolicy.isLexicallySafeAbsolute(path) else {
            throw LinuxRuntimeFailure(code: .unsafePath, message: "invalid project root")
        }
        var cursor = ""
        var linked = false
        for component in path.split(separator: "/") {
            cursor += "/" + component
            var componentMetadata = stat()
            guard lstat(cursor, &componentMetadata) == 0 else {
                throw LinuxRuntimeFailure(code: .unsafePath, message: "project root is unavailable")
            }
            if componentMetadata.st_mode & S_IFMT == S_IFLNK { linked = true }
        }
        let canonical = URL(fileURLWithPath: path).resolvingSymlinksInPath().path
        var metadata = stat()
        guard lstat(path, &metadata) == 0 else {
            throw LinuxRuntimeFailure(code: .unsafePath, message: "project root metadata is unavailable")
        }
        return ProjectRootEvidence(
            requestedPath: path, canonicalPath: canonical,
            isDirectory: metadata.st_mode & S_IFMT == S_IFDIR,
            containsSymbolicLink: linked, ownerUID: metadata.st_uid, ownerGID: metadata.st_gid)
    }
}

/// Reads Linux's stable process-start field. The token is compared again immediately before a
/// process-group signal, so a reused PID cannot inherit the authority of the process it replaced.
enum LinuxProcfs {
    struct Credentials: Equatable {
        let effectiveUID: UInt32
        let effectiveGID: UInt32
        let supplementaryGroups: Set<UInt32>
    }

    struct Row: Equatable {
        let identity: HostProcessIdentity
        let assistant: Assistant?
        let tty: String?
        let credentials: Credentials?
    }

    static func parseStat(_ text: String, pid: pid_t) -> HostProcessIdentity? {
        guard let close = text.lastIndex(of: ")") else { return nil }
        let fields = text[text.index(after: close)...].split(separator: " ")
        // Suffix starts at field 3 (`state`), so pgrp is index 2 and starttime is index 19.
        guard fields.count > 19, let group = pid_t(fields[2]), let ticks = UInt64(fields[19]) else {
            return nil
        }
        return HostProcessIdentity(pid: pid,
                                   processStart: Date(timeIntervalSince1970: TimeInterval(ticks)),
                                   startToken: String(ticks), processGroupID: group)
    }

    static func row(pid: pid_t) -> Row? {
        guard let statText = try? String(contentsOfFile: "/proc/\(pid)/stat", encoding: .utf8),
              let identity = parseStat(statText, pid: pid) else { return nil }
        let cmdline = (try? Data(contentsOf: URL(fileURLWithPath: "/proc/\(pid)/cmdline"))) ?? Data()
        let arguments = String(decoding: cmdline, as: UTF8.self).split(separator: "\0").map(String.init)
        let names = arguments.prefix(4).map { URL(fileURLWithPath: $0).lastPathComponent.lowercased() }
        let assistant: Assistant?
        if names.contains(where: { $0 == "claude" || $0.contains("claude") }) { assistant = .claude }
        else if names.contains(where: { $0 == "codex" || $0.contains("codex") }) { assistant = .codex }
        else { assistant = nil }

        let tty = try? FileManager.default.destinationOfSymbolicLink(atPath: "/proc/\(pid)/fd/0")
        let status = try? String(contentsOfFile: "/proc/\(pid)/status", encoding: .utf8)
        let credentials = status.flatMap(parseCredentials)
        return Row(identity: identity, assistant: assistant, tty: tty, credentials: credentials)
    }

    static func parseCredentials(_ status: String) -> Credentials? {
        let lines = status.split(separator: "\n")
        func values(_ prefix: String) -> [UInt32]? {
            guard let line = lines.first(where: { $0.hasPrefix(prefix) }) else { return nil }
            let parsed = line.split(whereSeparator: { $0 == " " || $0 == "\t" })
                .dropFirst().compactMap { UInt32($0) }
            return parsed
        }
        // `/proc/<pid>/status` spells real/effective/saved/fs ids in that order. Authority is
        // exercised with the effective id; the complete supplementary set is retained too.
        guard let uids = values("Uid:"), uids.count == 4,
              let gids = values("Gid:"), gids.count == 4,
              let groups = values("Groups:") else { return nil }
        return Credentials(effectiveUID: uids[1], effectiveGID: gids[1],
                           supplementaryGroups: Set(groups))
    }

    static func currentSupplementaryGroups() throws -> Set<UInt32> {
        let count = getgroups(0, nil)
        guard count >= 0 else {
            throw LinuxRuntimeFailure(code: .identityMismatch,
                                      message: "The service supplementary groups are unavailable.")
        }
        guard count > 0 else { return [] }
        var groups = [gid_t](repeating: 0, count: Int(count))
        let readCount = getgroups(count, &groups)
        guard readCount == count else {
            throw LinuxRuntimeFailure(code: .identityMismatch,
                                      message: "The service supplementary groups changed during composition.")
        }
        return Set(groups.map { UInt32($0) })
    }
}

struct LinuxProcessHost: ProcessHost {
    let serviceUID: UInt32
    let serviceGID: UInt32
    let supplementaryGroups: Set<UInt32>
    var capabilities: Set<HostCapability> { [.processObservation, .processSignal] }

    func observeAssistant(onTTY tty: String) throws -> TerminalProcessObservation {
        let started = ProcessInfo.processInfo.systemUptime
        guard let names = try? FileManager.default.contentsOfDirectory(atPath: "/proc") else {
            return TerminalProcessObservation(assistant: nil, processIdentity: nil,
                                              error: "procfs could not be enumerated")
        }
        var inspected = 0
        for name in names.sorted() {
            guard let pid = pid_t(name) else { continue }
            inspected += 1
            guard inspected <= 16_384,
                  ProcessInfo.processInfo.systemUptime - started <= 1 else {
                return TerminalProcessObservation(assistant: nil, processIdentity: nil,
                                                  error: "procfs observation exceeded its bound")
            }
            guard let row = LinuxProcfs.row(pid: pid), credentialsMatch(row.credentials),
                  row.tty == tty, let assistant = row.assistant else { continue }
            return TerminalProcessObservation(assistant: assistant,
                                              processIdentity: row.identity, error: nil)
        }
        return TerminalProcessObservation(assistant: nil, processIdentity: nil, error: nil)
    }

    func signal(_ identity: HostProcessIdentity, _ signal: HostProcessSignal) throws {
        guard let current = LinuxProcfs.row(pid: identity.pid) else {
            throw HostProcessIdentityChanged(identity: identity, reason: .processGone)
        }
        guard credentialsMatch(current.credentials), current.identity == identity,
              let group = identity.processGroupID, group > 0 else {
            throw HostProcessIdentityChanged(identity: identity, reason: .pidReused)
        }
        let value = signal == .terminate ? SIGTERM : SIGKILL
        guard kill(-group, value) == 0 else {
            if errno == ESRCH {
                throw HostProcessIdentityChanged(identity: identity, reason: .processGone)
            }
            throw LinuxRuntimeFailure(code: .processIdentity,
                                      message: "The owned provider process group could not be signalled.")
        }
    }

    func credentialsMatch(_ credentials: LinuxProcfs.Credentials?) -> Bool {
        guard let credentials else { return false }
        // Provider processes inherit the daemon's complete supplementary-group set. Landlock is
        // the filesystem authority boundary; this exact-set check prevents a credential-changing
        // process from retaining signal authority unnoticed.
        return credentials.effectiveUID == serviceUID
            && credentials.effectiveGID == serviceGID
            && credentials.supplementaryGroups == supplementaryGroups
    }
}

struct LinuxHostClock: HostClock {
    func monotonicNow() -> TimeInterval { ProcessInfo.processInfo.systemUptime }
    func sleep(for seconds: TimeInterval) { usleep(useconds_t(max(0, seconds) * 1_000_000)) }
}

struct LinuxIdentityHost: IdentityHost {
    func newIdentifier() -> String { UUID().uuidString.lowercased() }
}

struct LinuxExecutableDescriptor: Equatable {
    let path: String
    let device: UInt64
    let inode: UInt64
    let ownerUID: UInt32
    let ownerGID: UInt32
    let mode: UInt32

    static func open(path: String, serviceUID: UInt32,
                     reservedRoots: [String]) throws -> LinuxExecutableDescriptor {
        guard ProjectRootPolicy.isLexicallySafeAbsolute(path),
              !reservedRoots.contains(where: { ProjectRootPolicy.pathsOverlap(path, $0) }) else {
            throw unavailable("A configured executable overlaps daemon control state.")
        }
        var cursor = ""
        for component in path.split(separator: "/") {
            cursor += "/" + component
            var componentMetadata = stat()
            guard lstat(cursor, &componentMetadata) == 0,
                  componentMetadata.st_mode & S_IFMT != S_IFLNK else {
                throw unavailable("A configured executable is missing or traverses a symbolic link.")
            }
        }
        var metadata = stat()
        guard lstat(path, &metadata) == 0,
              metadata.st_mode & S_IFMT == S_IFREG,
              (metadata.st_uid == 0 || metadata.st_uid == serviceUID),
              metadata.st_mode & 0o022 == 0,
              access(path, X_OK) == 0 else {
            throw unavailable("A configured executable is not an owned, immutable executable file.")
        }
        return LinuxExecutableDescriptor(
            path: path, device: UInt64(metadata.st_dev), inode: UInt64(metadata.st_ino),
            ownerUID: metadata.st_uid, ownerGID: metadata.st_gid,
            mode: UInt32(metadata.st_mode))
    }

    func revalidate() throws {
        var metadata = stat()
        guard lstat(path, &metadata) == 0,
              metadata.st_mode & S_IFMT == S_IFREG,
              UInt64(metadata.st_dev) == device, UInt64(metadata.st_ino) == inode,
              metadata.st_uid == ownerUID, metadata.st_gid == ownerGID,
              UInt32(metadata.st_mode) == mode, metadata.st_mode & 0o022 == 0,
              access(path, X_OK) == 0 else {
            throw Self.unavailable("A configured executable changed after composition.")
        }
    }

    private static func unavailable(_ message: String) -> LinuxRuntimeFailure {
        LinuxRuntimeFailure(code: .capabilityUnavailable, message: message)
    }
}

struct LinuxProviderSandboxSpec: Codable, Equatable {
    let writableRoots: [String]
    let executable: String
    let arguments: [String]
}

/// The provider enters a Landlock filesystem allowlist before `execve`, then a seccomp filter
/// denies AF_UNIX socket creation and process-inspection syscalls. Thus the same-uid provider can
/// use its project/HOME/tmp and Internet sockets but cannot open daemon secrets, write outside the
/// admitted trees, connect to the tmux control socket, or ptrace the daemon. Both restrictions are
/// inherited by every descendant and cannot be relaxed after `no_new_privs`.
enum LinuxProviderSandbox {
    static let command = "__clawdline_sandbox_provider_v1"

    static var isSupported: Bool {
        #if os(Linux) && arch(x86_64)
        return landlockABIVersion() >= 3
        #else
        return false
        #endif
    }

    static func launch(encodedSpec: String) -> Never {
        do {
            guard let bytes = Data(base64Encoded: encodedSpec) else {
                throw LinuxRuntimeFailure(code: .unsafePath,
                                          message: "The provider sandbox receipt was malformed.")
            }
            let spec = try JSONDecoder().decode(LinuxProviderSandboxSpec.self, from: bytes)
            try install(spec)
            try exec(spec)
        } catch let failure as LinuxRuntimeFailure {
            let message = "clawdline provider sandbox: \(failure.code.rawValue)\n"
            FileHandle.standardError.write(Data(message.utf8))
        } catch {
            FileHandle.standardError.write(Data("clawdline provider sandbox: internal_failure\n".utf8))
        }
        exit(126)
    }

    static func validate(_ spec: LinuxProviderSandboxSpec) throws {
        guard isSupported, !spec.writableRoots.isEmpty,
              spec.writableRoots.count <= 34,
              spec.writableRoots.allSatisfy(ProjectRootPolicy.isLexicallySafeAbsolute),
              ProjectRootPolicy.isLexicallySafeAbsolute(spec.executable),
              spec.arguments.count <= 128,
              spec.arguments.allSatisfy({ $0.utf8.count <= 16_384 && !$0.contains("\0") }) else {
            throw LinuxRuntimeFailure(
                code: .capabilityUnavailable,
                message: "The kernel cannot enforce the configured provider sandbox.")
        }
    }

    private static func exec(_ spec: LinuxProviderSandboxSpec) throws {
        #if os(Linux)
        let argumentStrings = [spec.executable] + spec.arguments
        let environmentStrings = ProcessInfo.processInfo.environment.keys.sorted().map {
            $0 + "=" + ProcessInfo.processInfo.environment[$0]!
        }
        let argumentPointers = argumentStrings.map { value in
            value.withCString { strdup($0) }
        }
        let environmentPointers = environmentStrings.map { value in
            value.withCString { strdup($0) }
        }
        defer {
            argumentPointers.forEach { free($0) }
            environmentPointers.forEach { free($0) }
        }
        var argv = argumentPointers + [nil]
        var environment = environmentPointers + [nil]
        let result = spec.executable.withCString { path in
            execve(path, &argv, &environment)
        }
        guard result == 0 else {
            throw LinuxRuntimeFailure(code: .commandUnavailable,
                                      message: "The sandboxed provider could not be executed.")
        }
        #else
        throw LinuxRuntimeFailure(code: .capabilityUnavailable,
                                  message: "Provider containment is Linux-only.")
        #endif
    }

    private static func install(_ spec: LinuxProviderSandboxSpec) throws {
        try validate(spec)
        #if os(Linux) && arch(x86_64)
        guard linuxPrctl(38, 1, 0, 0, 0) == 0 else { // PR_SET_NO_NEW_PRIVS
            throw LinuxRuntimeFailure(code: .capabilityUnavailable,
                                      message: "Provider no_new_privs could not be enforced.")
        }
        let abi = landlockABIVersion()
        let read: UInt64 = landlockExecute | landlockReadFile | landlockReadDirectory
        var handled = read | landlockWriteFile | landlockRemoveDirectory | landlockRemoveFile
            | landlockMakeCharacter | landlockMakeDirectory | landlockMakeRegular
            | landlockMakeSocket | landlockMakeFIFO | landlockMakeBlock | landlockMakeSymbolicLink
        if abi >= 2 { handled |= landlockRefer }
        if abi >= 3 { handled |= landlockTruncate }
        var ruleset = LinuxLandlockRulesetAttribute(handledAccessFS: handled)
        let rulesetFD = withUnsafePointer(to: &ruleset) {
            linuxSyscall(444, UInt(bitPattern: $0),
                         UInt(MemoryLayout<LinuxLandlockRulesetAttribute>.size), 0, 0, 0, 0)
        }
        guard rulesetFD >= 0 else {
            throw LinuxRuntimeFailure(code: .capabilityUnavailable,
                                      message: "A provider Landlock ruleset could not be created.")
        }
        defer { _ = close(Int32(rulesetFD)) }

        var entries: [(String, UInt64)] = spec.writableRoots.map { ($0, handled) }
        entries.append((spec.executable, read))
        for path in ["/usr", "/bin", "/lib", "/lib64", "/etc/ssl", "/etc/ca-certificates",
                     "/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf", "/etc/gai.conf",
                     "/etc/ld.so.cache", "/etc/passwd", "/etc/group", "/etc/localtime",
                     "/etc/ca-certificates.conf",
                     "/dev/null", "/dev/urandom", "/dev/random", "/dev/tty"] {
            if FileManager.default.fileExists(atPath: path) { entries.append((path, read)) }
        }
        for (path, access) in entries {
            let parent = open(path, linuxOpenPath | O_CLOEXEC)
            guard parent >= 0 else {
                throw LinuxRuntimeFailure(code: .capabilityUnavailable,
                                          message: "A provider sandbox root could not be opened.")
            }
            var rule = LinuxLandlockPathBeneathAttribute(
                allowedAccess: access, parentFD: parent, reserved: 0)
            let added = withUnsafePointer(to: &rule) {
                linuxSyscall(445, UInt(rulesetFD), 1, UInt(bitPattern: $0), 0, 0, 0)
            }
            _ = close(parent)
            guard added == 0 else {
                throw LinuxRuntimeFailure(code: .capabilityUnavailable,
                                          message: "A provider sandbox root could not be admitted.")
            }
        }
        guard linuxSyscall(446, UInt(rulesetFD), 0, 0, 0, 0, 0) == 0 else {
            throw LinuxRuntimeFailure(code: .capabilityUnavailable,
                                      message: "Provider filesystem containment could not be activated.")
        }
        try installSeccomp()
        #else
        throw LinuxRuntimeFailure(code: .capabilityUnavailable,
                                  message: "Provider containment requires Ubuntu amd64.")
        #endif
    }

    #if os(Linux) && arch(x86_64)
    private static let linuxOpenPath: Int32 = 0x200000
    private static let landlockExecute: UInt64 = 1 << 0
    private static let landlockWriteFile: UInt64 = 1 << 1
    private static let landlockReadFile: UInt64 = 1 << 2
    private static let landlockReadDirectory: UInt64 = 1 << 3
    private static let landlockRemoveDirectory: UInt64 = 1 << 4
    private static let landlockRemoveFile: UInt64 = 1 << 5
    private static let landlockMakeCharacter: UInt64 = 1 << 6
    private static let landlockMakeDirectory: UInt64 = 1 << 7
    private static let landlockMakeRegular: UInt64 = 1 << 8
    private static let landlockMakeSocket: UInt64 = 1 << 9
    private static let landlockMakeFIFO: UInt64 = 1 << 10
    private static let landlockMakeBlock: UInt64 = 1 << 11
    private static let landlockMakeSymbolicLink: UInt64 = 1 << 12
    private static let landlockRefer: UInt64 = 1 << 13
    private static let landlockTruncate: UInt64 = 1 << 14

    private struct LinuxLandlockRulesetAttribute { var handledAccessFS: UInt64 }
    private struct LinuxLandlockPathBeneathAttribute {
        var allowedAccess: UInt64
        var parentFD: Int32
        var reserved: UInt32
    }
    private struct LinuxSockFilter {
        var code: UInt16
        var jumpTrue: UInt8
        var jumpFalse: UInt8
        var value: UInt32
    }
    private struct LinuxSockProgram {
        var length: UInt16
        var filters: UnsafeMutablePointer<LinuxSockFilter>?
    }

    private static func landlockABIVersion() -> Int {
        linuxSyscall(444, 0, 0, 1, 0, 0, 0)
    }

    private static func installSeccomp() throws {
        let load: UInt16 = 0x20
        let equal: UInt16 = 0x15
        let value: UInt16 = 0x06
        let deny = UInt32(0x0005_0000 | UInt32(EPERM))
        let allow: UInt32 = 0x7fff_0000
        let killProcess: UInt32 = 0x8000_0000
        var filters = [
            LinuxSockFilter(code: load, jumpTrue: 0, jumpFalse: 0, value: 4),
            LinuxSockFilter(code: equal, jumpTrue: 1, jumpFalse: 0, value: 0xc000_003e),
            LinuxSockFilter(code: value, jumpTrue: 0, jumpFalse: 0, value: killProcess),
            LinuxSockFilter(code: load, jumpTrue: 0, jumpFalse: 0, value: 0),
        ]
        // Same-uid process inspection and descriptor theft cannot be part of provider authority.
        for syscallNumber: UInt32 in [101, 310, 311, 438] { // ptrace, process_vm_*, pidfd_getfd
            filters.append(LinuxSockFilter(code: equal, jumpTrue: 0, jumpFalse: 1,
                                           value: syscallNumber))
            filters.append(LinuxSockFilter(code: value, jumpTrue: 0, jumpFalse: 0, value: deny))
        }
        // AF_UNIX is deliberately unavailable to providers. They receive no inherited socket;
        // without one they cannot address the daemon/tmux control paths even though Internet
        // sockets remain available for model APIs.
        for syscallNumber: UInt32 in [41, 53] { // socket, socketpair on Linux amd/amd64
            filters.append(LinuxSockFilter(code: equal, jumpTrue: 0, jumpFalse: 4,
                                           value: syscallNumber))
            filters.append(LinuxSockFilter(code: load, jumpTrue: 0, jumpFalse: 0, value: 16))
            filters.append(LinuxSockFilter(code: equal, jumpTrue: 0, jumpFalse: 1,
                                           value: UInt32(AF_UNIX)))
            filters.append(LinuxSockFilter(code: value, jumpTrue: 0, jumpFalse: 0, value: deny))
            filters.append(LinuxSockFilter(code: value, jumpTrue: 0, jumpFalse: 0, value: allow))
        }
        filters.append(LinuxSockFilter(code: value, jumpTrue: 0, jumpFalse: 0, value: allow))
        let installed = filters.withUnsafeMutableBufferPointer { buffer -> Int32 in
            var program = LinuxSockProgram(length: UInt16(buffer.count), filters: buffer.baseAddress)
            return withUnsafePointer(to: &program) {
                linuxPrctl(22, 2, UInt(bitPattern: $0), 0, 0) // PR_SET_SECCOMP/FILTER
            }
        }
        guard installed == 0 else {
            throw LinuxRuntimeFailure(code: .capabilityUnavailable,
                                      message: "Provider socket/process containment could not be activated.")
        }
    }

    @_silgen_name("syscall")
    private static func linuxSyscall(_ number: Int, _ first: UInt, _ second: UInt,
                                     _ third: UInt, _ fourth: UInt, _ fifth: UInt,
                                     _ sixth: UInt) -> Int
    @_silgen_name("prctl")
    private static func linuxPrctl(_ option: Int32, _ second: UInt, _ third: UInt,
                                   _ fourth: UInt, _ fifth: UInt) -> Int32
    #endif
}

/// A tmux server on one explicit socket owns every PTY. No default socket or inherited TMUX value
/// can redirect these calls into somebody else's server.
final class LinuxTmuxTerminalHost: TerminalHost {
    let tmuxExecutable: LinuxExecutableDescriptor
    let socketPath: String
    let providerExecutables: [Assistant: LinuxExecutableDescriptor]
    let sandboxExecutable: LinuxExecutableDescriptor
    let writableRoots: [String]
    let providerEnvironment: [String: String]
    let runner: LinuxCommandRunner
    let limits: TerminalWorkLimits
    var failSubmitAfterPasteForTesting = false

    var capabilities: Set<HostCapability> { [.terminalTmux] }

    init(tmuxExecutable: LinuxExecutableDescriptor, socketPath: String,
         providerExecutables: [Assistant: LinuxExecutableDescriptor],
         sandboxExecutable: LinuxExecutableDescriptor, writableRoots: [String],
         providerEnvironment: [String: String], hostEnvironment: [String: String],
         limits: TerminalWorkLimits = .production) {
        self.tmuxExecutable = tmuxExecutable
        self.socketPath = socketPath
        self.providerExecutables = providerExecutables
        self.sandboxExecutable = sandboxExecutable
        self.writableRoots = writableRoots
        self.providerEnvironment = providerEnvironment
        self.runner = LinuxCommandRunner(environment: hostEnvironment)
        self.limits = limits
    }

    func inventory() throws -> TerminalInventory {
        let format = ["#{pane_id}", "#{session_name}", "#{pane_title}", "#{pane_tty}",
                      "#{window_index}", "#{pane_index}", "#{pane_current_path}"]
            .joined(separator: "\u{1f}")
        let receipt = try tmux(["list-panes", "-a", "-F", format], operation: .enumerate)
        if receipt.status != 0 {
            let error = String(decoding: receipt.stderr, as: UTF8.self)
            if error.contains("no server running") || error.contains("error connecting") {
                return TerminalInventory()
            }
            return TerminalInventory(error: "tmux inventory was unavailable", isComplete: false)
        }
        let lines = String(decoding: receipt.stdout, as: UTF8.self)
            .split(separator: "\n", omittingEmptySubsequences: true)
        guard lines.count <= limits.maximumInventory else {
            throw LinuxRuntimeFailure(code: .inventoryLimit,
                                      message: "The tmux inventory exceeded \(limits.maximumInventory) panes.")
        }
        var sessions: [TargetSession] = []
        for line in lines {
            let fields = line.split(separator: "\u{1f}", omittingEmptySubsequences: false)
            guard fields.count == 7, Self.paneID(String(fields[0])) != nil,
                  let window = Int(fields[4]), let pane = Int(fields[5]) else {
                return TerminalInventory(sessions: sessions, error: "tmux returned a malformed pane row",
                                         isComplete: false)
            }
            let sessionName = String(fields[1])
            let assistant: Assistant? = sessionName.hasPrefix("clawdline-claude-") ? .claude
                : (sessionName.hasPrefix("clawdline-codex-") ? .codex : nil)
            sessions.append(TargetSession(backend: .tmux, id: String(fields[0]),
                                          name: String(fields[2]), tty: String(fields[3]),
                                          windowIndex: window, tabIndex: pane,
                                          assistant: assistant, cwd: String(fields[6])))
        }
        return TerminalInventory(sessions: sessions)
    }

    func create(_ request: TerminalCreateRequest) throws -> TerminalCreated {
        guard case .managedProvider(let plan) = request else {
            throw LinuxRuntimeFailure(code: .commandFailed,
                                      message: "Linux accepts only a policy-admitted provider launch.")
        }
        guard plan.terminalMode != .iTermTab else {
            throw HostCapabilityUnavailable(capability: .terminalITerm,
                                            operation: "start provider session")
        }
        guard let executable = providerExecutables[plan.assistant] else {
            throw LinuxRuntimeFailure(code: .commandUnavailable,
                                      message: "The admitted provider has no configured executable.")
        }
        try executable.revalidate()
        try sandboxExecutable.revalidate()
        let suffix = plan.commandID.replacingOccurrences(of: ".", with: "-")
            .replacingOccurrences(of: "_", with: "-")
        let sessionName = "clawdline-\(plan.assistant.rawValue)-" + String(suffix.prefix(32))
        let envArguments = providerEnvironment.keys.sorted().flatMap { key in
            [key + "=" + providerEnvironment[key]!]
        }
        let sandbox = LinuxProviderSandboxSpec(
            writableRoots: writableRoots, executable: executable.path,
            arguments: plan.arguments)
        let encodedSandbox = try JSONEncoder().encode(sandbox).base64EncodedString()
        let commandWords = ["/usr/bin/env", "-i"] + envArguments
            + [sandboxExecutable.path, LinuxProviderSandbox.command, encodedSandbox]
        let command = "exec " + commandWords.map(SessionLaunchPolicy.shellQuoted).joined(separator: " ")
        let format = ["#{pane_id}", "#{pane_pid}", "#{pane_tty}"].joined(separator: "\u{1f}")
        let receipt: LinuxCommandReceipt
        do {
            receipt = try tmux(["new-session", "-d", "-s", sessionName, "-c", plan.projectRoot,
                                "-P", "-F", format, command], operation: .create)
        } catch let failure as LinuxRuntimeFailure {
            throw LinuxTerminalEffectFailure(
                failure: failure, certainty: .unknown, checkpoint: .none,
                sessionID: nil, tty: nil)
        }
        guard receipt.status == 0 else {
            throw LinuxTerminalEffectFailure(
                failure: LinuxRuntimeFailure(
                    code: .commandFailed,
                    message: "tmux refused the admitted provider launch."),
                certainty: .noEffect, checkpoint: .none, sessionID: nil, tty: nil)
        }
        let fields = String(decoding: receipt.stdout, as: UTF8.self)
            .trimmingCharacters(in: .whitespacesAndNewlines)
            .split(separator: "\u{1f}", omittingEmptySubsequences: false)
        guard fields.count == 3, let pane = Self.paneID(String(fields[0])),
              let pid = pid_t(fields[1]), !fields[2].isEmpty,
              let identity = LinuxProcfs.row(pid: pid)?.identity else {
            let removed = (try? tmux(["kill-session", "-t", "=" + sessionName],
                                     operation: .close).status) == 0
            throw LinuxTerminalEffectFailure(
                failure: LinuxRuntimeFailure(
                    code: .malformedReply,
                    message: "tmux created no identity-bearing PTY receipt."),
                certainty: removed ? .compensated : .unknown,
                checkpoint: .sessionCreated, sessionID: nil, tty: nil)
        }
        return TerminalCreated(
            id: pane, backend: .tmux, tty: String(fields[2]),
            attachCommand: "tmux -S " + SessionLaunchPolicy.shellQuoted(socketPath)
                + " attach -t " + SessionLaunchPolicy.shellQuoted(sessionName),
            processIdentity: identity)
    }

    func sendLine(_ text: String, to session: TargetSession) throws -> String? {
        switch try sendLineEffect(text, to: session) {
        case .submitted: return nil
        case .pastedNotSubmitted: return "pasted, but Enter did not land"
        }
    }

    func sendLineEffect(_ text: String, to session: TargetSession) throws -> LinuxSendEffectResult {
        try require(session)
        let bytes = Data(text.utf8)
        try ProviderLifecyclePolicy.validateInput(bytes, limits: limits)
        let buffer = "clawdline-" + UUID().uuidString.lowercased()
        let loaded: LinuxCommandReceipt
        do {
            loaded = try tmux(["load-buffer", "-b", buffer, "-"], input: bytes, operation: .send)
        } catch let failure as LinuxRuntimeFailure {
            throw LinuxTerminalEffectFailure(
                failure: failure, certainty: .noEffect, checkpoint: .none,
                sessionID: session.id, tty: session.tty)
        }
        guard loaded.status == 0 else {
            throw LinuxTerminalEffectFailure(
                failure: LinuxRuntimeFailure(
                    code: .commandFailed,
                    message: "tmux would not accept the bounded input"),
                certainty: .noEffect, checkpoint: .none,
                sessionID: session.id, tty: session.tty)
        }
        defer { _ = try? tmux(["delete-buffer", "-b", buffer], operation: .send) }
        let pasted: LinuxCommandReceipt
        do {
            pasted = try tmux(["paste-buffer", "-d", "-b", buffer, "-t", session.id],
                              operation: .send)
        } catch let failure as LinuxRuntimeFailure {
            throw LinuxTerminalEffectFailure(
                failure: failure, certainty: .unknown, checkpoint: .none,
                sessionID: session.id, tty: session.tty)
        }
        guard pasted.status == 0 else {
            throw LinuxTerminalEffectFailure(
                failure: LinuxRuntimeFailure(code: .commandFailed,
                                             message: "The addressed tmux pane is gone."),
                certainty: .noEffect, checkpoint: .none,
                sessionID: session.id, tty: session.tty)
        }
        if failSubmitAfterPasteForTesting { return .pastedNotSubmitted }
        let submitted: LinuxCommandReceipt
        do {
            submitted = try tmux(["send-keys", "-t", session.id, "Enter"], operation: .send)
        } catch let failure as LinuxRuntimeFailure {
            throw LinuxTerminalEffectFailure(
                failure: failure, certainty: .unknown, checkpoint: .textPasted,
                sessionID: session.id, tty: session.tty)
        }
        return submitted.status == 0 ? .submitted : .pastedNotSubmitted
    }

    /// Compensate only the exact pane just created: its pane id, tty, pid, start token and process
    /// group must all still match before the kill, and absence is read back afterward.
    func compensateCreated(_ created: TerminalCreated) throws -> Bool {
        guard let expected = created.processIdentity else { return false }
        let format = ["#{pane_pid}", "#{pane_tty}"].joined(separator: "\u{1f}")
        let observed = try tmux(["display-message", "-p", "-t", created.id, format],
                                operation: .close)
        let fields = String(decoding: observed.stdout, as: UTF8.self)
            .trimmingCharacters(in: .whitespacesAndNewlines)
            .split(separator: "\u{1f}", omittingEmptySubsequences: false)
        guard observed.status == 0, fields.count == 2,
              pid_t(fields[0]) == expected.pid, String(fields[1]) == created.tty,
              LinuxProcfs.row(pid: expected.pid)?.identity == expected else { return false }
        guard try tmux(["kill-pane", "-t", created.id], operation: .close).status == 0 else {
            return false
        }
        let readBack = try tmux(["display-message", "-p", "-t", created.id, "#{pane_id}"],
                                operation: .close)
        return readBack.status != 0
    }

    func close(_ session: TargetSession) throws -> String? {
        try require(session)
        return try tmux(["kill-pane", "-t", session.id], operation: .close).status == 0
            ? nil : "that tmux pane is gone"
    }

    func capture(_ session: TargetSession) throws -> String? {
        try require(session)
        let receipt = try tmux(["capture-pane", "-p", "-e", "-t", session.id, "-S", "-200"],
                               operation: .observe)
        return receipt.status == 0 ? String(decoding: receipt.stdout, as: UTF8.self) : nil
    }

    func reveal(_ session: TargetSession, activate: Bool) throws {
        try require(session)
        // Headless Linux has no GUI activation. Selecting the pane is still a real tmux effect;
        // asking to activate a window returns the Mac-only capability refusal.
        guard !activate else {
            throw HostCapabilityUnavailable(capability: .terminalITerm, operation: "activate terminal")
        }
        let receipt = try tmux(["select-pane", "-t", session.id], operation: .observe)
        guard receipt.status == 0 else {
            throw LinuxRuntimeFailure(code: .commandFailed, message: "that tmux pane is gone")
        }
    }

    func interrupt(_ bytes: [UInt8], to session: TargetSession) throws -> String? {
        try require(session)
        try ProviderLifecyclePolicy.validateInput(Data(bytes), limits: limits)
        guard bytes.count <= 3 else {
            throw TerminalLifecycleFailure(code: .invalidCommand,
                                           message: "A terminal control key is at most three bytes.")
        }
        let hex = bytes.map { String(format: "%02x", $0) }
        return try tmux(["send-keys", "-t", session.id, "-H"] + hex, operation: .interrupt).status == 0
            ? nil : "that tmux pane is gone"
    }

    func resize(_ session: TargetSession, columns: Int, rows: Int) throws {
        try require(session)
        guard (20...500).contains(columns), (5...300).contains(rows) else {
            throw TerminalLifecycleFailure(code: .invalidCommand,
                                           message: "Terminal dimensions are outside 20...500 by 5...300.")
        }
        let resized = try tmux(["resize-pane", "-t", session.id, "-x", String(columns),
                                "-y", String(rows)], operation: .resize)
        guard resized.status == 0 else {
            throw LinuxRuntimeFailure(code: .commandFailed, message: "that tmux pane could not be resized")
        }
        let observed = try tmux(["display-message", "-p", "-t", session.id,
                                 "#{pane_width}x#{pane_height}"], operation: .resize)
        let value = String(decoding: observed.stdout, as: UTF8.self)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard observed.status == 0, value == "\(columns)x\(rows)" else {
            throw LinuxRuntimeFailure(code: .commandFailed,
                                      message: "tmux did not report the requested PTY dimensions.")
        }
    }

    private func require(_ session: TargetSession) throws {
        guard session.backend == .tmux, Self.paneID(session.id) != nil else {
            throw HostCapabilityUnavailable(capability: .terminal(session.backend),
                                            operation: "address terminal")
        }
    }

    private func tmux(_ arguments: [String], input: Data? = nil,
                      operation: TerminalEffectOperation) throws -> LinuxCommandReceipt {
        try tmuxExecutable.revalidate()
        return try runner.run(executable: tmuxExecutable.path,
                       arguments: ["-S", socketPath] + arguments,
                       input: input, timeout: ProviderLifecyclePolicy.timeout(for: operation))
    }

    static func paneID(_ raw: String) -> String? {
        guard raw.first == "%", raw.count <= 16,
              raw.dropFirst().allSatisfy({ $0.isNumber }) else { return nil }
        return raw
    }
}
