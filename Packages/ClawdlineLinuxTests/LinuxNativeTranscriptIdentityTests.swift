import Foundation
import XCTest
@testable import ClawdlineApplication
@testable import ClawdlineLinux
#if canImport(Glibc)
import Glibc
#else
import Darwin
#endif

private final class NativeIdentityFixture {
    let root: URL
    let home: URL
    let proc: URL
    let uid = geteuid()
    let gid = getegid()
    let clock = LinuxProcfs.Clock(
        bootTime: Date(timeIntervalSince1970: 1_700_000_000), ticksPerSecond: 100)
    let canonicalCWD = "/project"
    private var processes: [Int32: HostProcessIdentity] = [:]

    init() throws {
        // `/var` is a symlink on macOS and Foundation leaves it unresolved. Production's
        // O_NOFOLLOW walk intentionally rejects it, so canonicalize only the system temp root.
        let temporary = FileManager.default.temporaryDirectory.path
        let resolved = try XCTUnwrap(temporary.withCString { realpath($0, nil) })
        defer { free(resolved) }
        root = URL(fileURLWithPath: String(cString: resolved))
            .appendingPathComponent("clawdline-native-identity-\(UUID().uuidString)")
        home = root.appendingPathComponent("home")
        proc = root.appendingPathComponent("proc")
        try directory(home.path)
        try directory(proc.path)
        try file(proc.appendingPathComponent("stat").path, "btime 1700000000\n")
    }

    deinit { try? FileManager.default.removeItem(at: root) }

    func directory(_ path: String) throws {
        try FileManager.default.createDirectory(atPath: path, withIntermediateDirectories: true)
        XCTAssertEqual(chmod(path, 0o700), 0)
    }

    func file(_ path: String, _ contents: String, mode: mode_t = 0o600) throws {
        try directory(URL(fileURLWithPath: path).deletingLastPathComponent().path)
        try Data(contents.utf8).write(to: URL(fileURLWithPath: path))
        XCTAssertEqual(chmod(path, mode), 0)
    }

    func process(_ pid: Int32, ticks: UInt64 = 500, group: Int32? = nil) throws
        -> HostProcessIdentity {
        let fields = ["S", "1", String(group ?? pid)]
            + Array(repeating: "0", count: 16) + [String(ticks)]
        try file(proc.appendingPathComponent("\(pid)/stat").path,
                 "\(pid) (provider name) " + fields.joined(separator: " ") + "\n")
        let identity = try XCTUnwrap(LinuxProcfs.parseStat(
            "\(pid) (provider name) " + fields.joined(separator: " "),
            pid: pid, clock: clock))
        processes[pid] = identity
        return identity
    }

    func claudeRegistry(name: String, pid: Int32, session: String,
                        kind: String? = nil, job: String? = nil,
                        parked: String? = nil, cwd: String? = nil,
                        peerProtocol: Int = 1, procStart: String? = nil) throws {
        let identity = try XCTUnwrap(processes[pid] ?? processes.values.first)
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        formatter.dateFormat = "EEE MMM d HH:mm:ss yyyy"
        var row: [String: Any] = [
            "pid": pid, "sessionId": session, "cwd": cwd ?? canonicalCWD,
            "procStart": procStart ?? formatter.string(from: identity.processStart),
            "peerProtocol": peerProtocol,
        ]
        if let kind { row["kind"] = kind }
        if let job { row["jobId"] = job }
        if let parked { row["parkedJobId"] = parked }
        let data = try JSONSerialization.data(withJSONObject: row, options: [.sortedKeys])
        let path = home.appendingPathComponent(".claude/sessions/\(name).json").path
        try directory(URL(fileURLWithPath: path).deletingLastPathComponent().path)
        try data.write(to: URL(fileURLWithPath: path)); XCTAssertEqual(chmod(path, 0o600), 0)
    }

    func claudeTranscript(project: String = "-project", session: String,
                          text: String = "{\"type\":\"assistant\"}\n") throws -> String {
        let relative = ".claude/projects/\(project)/\(session).jsonl"
        try file(home.appendingPathComponent(relative).path, text)
        return relative
    }

    func codexTranscript(pid: Int32, fd: Int, session: String,
                         threadSource: String? = "user", suffix: String = "a",
                         cwd: String? = nil, source: String = "cli") throws -> String {
        let relative = ".codex/sessions/2026/09/16/rollout-\(suffix).jsonl"
        let path = home.appendingPathComponent(relative).path
        var payload: [String: Any] = [
            "session_id": session, "cwd": cwd ?? canonicalCWD, "source": source,
        ]
        if let threadSource { payload["thread_source"] = threadSource }
        let header = try JSONSerialization.data(
            withJSONObject: ["type": "session_meta", "payload": payload], options: [.sortedKeys])
        try file(path, String(decoding: header, as: UTF8.self) + "\n{\"type\":\"response_item\"}\n")
        let fdDirectory = proc.appendingPathComponent("\(pid)/fd").path
        try directory(fdDirectory)
        XCTAssertEqual(symlink(path, fdDirectory + "/\(fd)"), 0)
        return relative
    }

    func reader(limits: LinuxNativeTranscriptReader.Limits = .init(),
                serviceUID: UInt32? = nil,
                serviceGID: UInt32? = nil,
                supplementaryGroups: Set<UInt32>? = nil,
                beforePost: (() -> Void)? = nil,
                processOwner: ((pid_t) -> UInt32?)? = nil,
                processCredentials: ((pid_t) -> LinuxProcfs.Credentials?)? = nil,
                fileOwner: ((String, UInt32) -> UInt32)? = nil,
                registryOpenFileCount: ((Int) -> Void)? = nil,
                monotonic: @escaping () -> TimeInterval = { 0 }) -> LinuxNativeTranscriptReader {
        var configuration = LinuxNativeTranscriptReader.Configuration(
            providerHome: home.path, procRoot: proc.path, serviceUID: serviceUID ?? uid,
            serviceGID: serviceGID, supplementaryGroups: supplementaryGroups,
            clock: clock, limits: limits)
        configuration.monotonicNow = monotonic
        configuration.beforePostValidation = beforePost
        configuration.processOwner = processOwner
        configuration.processCredentials = processCredentials
        configuration.fileOwner = fileOwner
        configuration.registryOpenFileCount = registryOpenFileCount
        return LinuxNativeTranscriptReader(configuration: configuration)
    }
}

extension LinuxRuntimeContractTests {
    func testNativeRuntimeRetainsExactProcessAcrossABAObservation() throws {
        let fixture = try NativeIdentityFixture()
        let conversation = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
        let processA = try fixture.process(301)
        let processB = HostProcessIdentity(
            pid: 302, processStart: processA.processStart.addingTimeInterval(1),
            startToken: "600", processGroupID: 302)
        try fixture.claudeRegistry(name: "301", pid: 301, session: conversation)
        _ = try fixture.claudeTranscript(
            session: conversation,
            text: #"{"type":"assistant","message":{"content":[{"type":"text","text":"retained A"}]}}"# + "\n")
        let baseIdentity: LinuxNativeTranscriptIdentity
        guard case .available(let identity) = fixture.reader().read(
            assistant: .claude, process: processA, canonicalCWD: "/project") else {
            return XCTFail("the exact A observation must select its synthetic native history")
        }
        baseIdentity = identity
        let session = TargetSession(
            backend: .tmux, id: "%aba", name: "ABA", tty: "/dev/pts/9",
            windowIndex: 0, tabIndex: 0, assistant: .claude, cwd: "/project")
        let incarnation = String(repeating: "a", count: 64)
        let before = LinuxCloudSessionIdentity(
            session: session, incarnation: incarnation, processIdentity: processA)
        let interveningB = LinuxCloudSessionIdentity(
            session: session, incarnation: incarnation, processIdentity: processB)
        let after = LinuxCloudSessionIdentity(
            session: session, incarnation: incarnation, processIdentity: processA)
        var readerWasCalled = false
        var cloudObservations: [HostProcessIdentity] = []
        var readerProcesses: [HostProcessIdentity] = []
        let parsed = try LinuxProviderRuntime.nativeTranscript(
            sessionID: session.id, expectedIncarnation: incarnation,
            canonicalCWD: "/project", limit: 20,
            cloudSessionIdentity: {
                let value = cloudObservations.isEmpty
                    ? before : (readerWasCalled ? after : interveningB)
                cloudObservations.append(try XCTUnwrap(value.processIdentity))
                return value
            },
            readNative: { assistant, process, cwd in
                readerWasCalled = true
                readerProcesses.append(process)
                // A mutation that performs an independent pre-reader observation receives B.
                // Preserve valid bytes/evidence so the production post-check, not a fixture
                // short-cut, must reject the eventual A -> B -> A sequence.
                return .available(LinuxNativeTranscriptIdentity(
                    assistant: assistant, sessionID: baseIdentity.sessionID,
                    source: baseIdentity.source, relativePath: baseIdentity.relativePath,
                    process: process, writerProcess: process,
                    conversationMode: baseIdentity.conversationMode,
                    fileDevice: baseIdentity.fileDevice, fileInode: baseIdentity.fileInode,
                    fileSize: baseIdentity.fileSize,
                    fileModifiedNanoseconds: baseIdentity.fileModifiedNanoseconds,
                    suffixOffset: baseIdentity.suffixOffset, bytes: baseIdentity.bytes))
            })
        XCTAssertEqual(parsed.entries.map(\.text), ["retained A"])
        XCTAssertEqual(cloudObservations, [processA, processA])
        XCTAssertEqual(readerProcesses, [processA],
                       "the process that minted the retained incarnation reaches the reader")
    }

    func testNativeFixtureUsesCanonicalSystemTemporaryDirectory() throws {
        let fixture = try NativeIdentityFixture()
        let temporary = FileManager.default.temporaryDirectory.path
        let resolved = try XCTUnwrap(temporary.withCString { realpath($0, nil) })
        defer { free(resolved) }
        XCTAssertEqual(fixture.root.deletingLastPathComponent().path,
                       String(cString: resolved))
    }

    func testNativeProcessStartConvertsBootRelativeTicksToUnixDate() throws {
        let fixture = try NativeIdentityFixture()
        let identity = try fixture.process(101, ticks: 250)
        XCTAssertEqual(identity.startToken, "250")
        XCTAssertEqual(identity.processStart.timeIntervalSince1970, 1_700_000_002.5,
                       accuracy: 0.0001)
    }

    func testNativeClaudeIdentityUsesExactPIDRegistryAndDescriptorWalk() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(102)
        let reread = try String(contentsOf: fixture.proc.appendingPathComponent("102/stat"),
                                encoding: .utf8)
        XCTAssertEqual(LinuxProcfs.parseStat(reread, pid: 102, clock: fixture.clock), process)
        try fixture.claudeRegistry(name: "102", pid: 102, session: "claude-session")
        let relative = try fixture.claudeTranscript(session: "claude-session")
        let outcome = fixture.reader().read(assistant: .claude, process: process, canonicalCWD: "/project")
        guard case .available(let identity) = outcome else {
            return XCTFail("an exact registry and transcript must resolve: \(outcome)")
        }
        XCTAssertEqual(identity.sessionID, "claude-session")
        XCTAssertEqual(identity.relativePath, relative)
        XCTAssertEqual(identity.source, .claudeRegistry)
        var metadata = stat()
        XCTAssertEqual(lstat(fixture.home.appendingPathComponent(relative).path, &metadata), 0)
        XCTAssertEqual(identity.fileDevice, UInt64(metadata.st_dev))
        XCTAssertEqual(identity.fileInode, UInt64(metadata.st_ino))
        XCTAssertEqual(identity.fileSize, UInt64(metadata.st_size))
        XCTAssertGreaterThan(identity.fileModifiedNanoseconds, 0)
        XCTAssertEqual(identity.suffixOffset, 0)
    }

    func testNativeClaudeRequiresProtocolCWDAndProcessStart() throws {
        for mismatch in ["protocol", "cwd", "start"] {
            let fixture = try NativeIdentityFixture()
            let process = try fixture.process(121)
            try fixture.claudeRegistry(
                name: "121", pid: 121, session: "identity",
                cwd: mismatch == "cwd" ? "/other" : nil,
                peerProtocol: mismatch == "protocol" ? 2 : 1,
                procStart: mismatch == "start" ? "Tue Sep 16 01:02:03 2026" : nil)
            _ = try fixture.claudeTranscript(session: "identity")
            XCTAssertEqual(
                fixture.reader().read(
                    assistant: .claude, process: process, canonicalCWD: "/project"),
                .unknown("Claude registry process or working-directory identity is mismatched"),
                mismatch)
        }
    }

    func testNativeClaudeParkedProcessBindsExactBackgroundJob() throws {
        let fixture = try NativeIdentityFixture()
        let foreground = try fixture.process(103)
        _ = try fixture.process(104, ticks: 600)
        try fixture.claudeRegistry(name: "103", pid: 103, session: "parked-session",
                                   kind: "interactive", parked: "job-a")
        try fixture.claudeRegistry(name: "104", pid: 104, session: "background-session",
                                   kind: "bg", job: "job-a")
        _ = try fixture.claudeTranscript(session: "background-session")
        guard case .available(let identity) = fixture.reader().read(
            assistant: .claude, process: foreground, canonicalCWD: "/project") else {
            return XCTFail("one exact parked/background pair must resolve")
        }
        XCTAssertEqual(identity.sessionID, "background-session")
        XCTAssertEqual(identity.process, foreground)
        XCTAssertEqual(identity.writerProcess.pid, 104)
    }

    func testNativeClaudeParkedWriterMustBeUniqueOwnedAndLive() throws {
        let fixture = try NativeIdentityFixture()
        let foreground = try fixture.process(110)
        _ = try fixture.process(111)
        _ = try fixture.process(112)
        try fixture.claudeRegistry(name: "110", pid: 110, session: "parked",
                                   kind: "interactive", parked: "job-z")
        try fixture.claudeRegistry(name: "111", pid: 111, session: "writer-a",
                                   kind: "bg", job: "job-z")
        try fixture.claudeRegistry(name: "112", pid: 112, session: "writer-b",
                                   kind: "bg", job: "job-z")
        _ = try fixture.claudeTranscript(session: "writer-a")
        XCTAssertEqual(fixture.reader().read(assistant: .claude, process: foreground, canonicalCWD: "/project"),
                       .unknown("Claude parked-background registry binding is ambiguous"))

        let single = try NativeIdentityFixture()
        let authority = try single.process(113)
        _ = try single.process(114)
        try single.claudeRegistry(name: "113", pid: 113, session: "parked",
                                  kind: "interactive", parked: "job-owner")
        try single.claudeRegistry(name: "114", pid: 114, session: "writer",
                                  kind: "bg", job: "job-owner")
        _ = try single.claudeTranscript(session: "writer")
        let wrongOwner = single.reader(processOwner: { pid in
            pid == 114 ? single.uid &+ 1 : single.uid
        })
        XCTAssertEqual(wrongOwner.read(assistant: .claude, process: authority, canonicalCWD: "/project"),
                       .unknown("Claude background process owner is mismatched"))

        var writerOwnerReads = 0
        let ownerChangesAfterSelection = single.reader(processOwner: { pid in
            guard pid == 114 else { return single.uid }
            writerOwnerReads += 1
            return writerOwnerReads == 1 ? single.uid : single.uid &+ 1
        })
        XCTAssertEqual(ownerChangesAfterSelection.read(
            assistant: .claude, process: authority, canonicalCWD: "/project"),
            .busy("provider process identity changed during read"))

        var writerCredentialReads = 0
        let credentialsChangeAfterSelection = single.reader(
            serviceGID: single.gid, supplementaryGroups: [single.gid],
            processCredentials: { pid in
                guard pid == 114 else {
                    return .init(effectiveUID: single.uid, effectiveGID: single.gid,
                                 supplementaryGroups: [single.gid])
                }
                writerCredentialReads += 1
                return .init(effectiveUID: single.uid,
                             effectiveGID: writerCredentialReads == 1
                                 ? single.gid : single.gid &+ 1,
                             supplementaryGroups: [single.gid])
            })
        XCTAssertEqual(credentialsChangeAfterSelection.read(
            assistant: .claude, process: authority, canonicalCWD: "/project"),
            .busy("provider process identity changed during read"))
    }

    func testNativeClaudeParkedWriterIncarnationCannotBeReused() throws {
        let fixture = try NativeIdentityFixture()
        let authority = try fixture.process(119)
        _ = try fixture.process(120, ticks: 600)
        try fixture.claudeRegistry(name: "119", pid: 119, session: "parked",
                                   kind: "interactive", parked: "job-reuse")
        try fixture.claudeRegistry(name: "120", pid: 120, session: "writer",
                                   kind: "bg", job: "job-reuse")
        _ = try fixture.claudeTranscript(session: "writer")
        let reader = fixture.reader(beforePost: {
            _ = try? fixture.process(120, ticks: 601)
        })
        XCTAssertEqual(reader.read(assistant: .claude, process: authority, canonicalCWD: "/project"),
                       .busy("provider process identity changed during read"))
    }

    func testNativeClaudeParkedWriterCannotReuseForegroundPIDOrSession() throws {
        let selfBound = try NativeIdentityFixture()
        let selfProcess = try selfBound.process(214)
        try selfBound.claudeRegistry(
            name: "214", pid: 214, session: "foreground",
            kind: "bg", job: "job-self", parked: "job-self")
        _ = try selfBound.claudeTranscript(session: "foreground")
        XCTAssertEqual(selfBound.reader().read(
            assistant: .claude, process: selfProcess, canonicalCWD: "/project"),
            .unknown("Claude parked-background registry binding is ambiguous"))

        let reusedSession = try NativeIdentityFixture()
        let foreground = try reusedSession.process(215)
        _ = try reusedSession.process(216)
        try reusedSession.claudeRegistry(
            name: "215", pid: 215, session: "same-session",
            kind: "interactive", parked: "job-same")
        try reusedSession.claudeRegistry(
            name: "216", pid: 216, session: "same-session", kind: "bg", job: "job-same")
        _ = try reusedSession.claudeTranscript(session: "same-session")
        XCTAssertEqual(reusedSession.reader().read(
            assistant: .claude, process: foreground, canonicalCWD: "/project"),
            .unknown("Claude parked-background registry binding is ambiguous"))
    }

    func testNativeClaudeRejectsEmptyMalformedAndUnsafeRegistryEntries() throws {
        let empty = try NativeIdentityFixture()
        let emptyProcess = try empty.process(115)
        try empty.directory(empty.home.appendingPathComponent(".claude/sessions").path)
        XCTAssertEqual(empty.reader().read(assistant: .claude, process: emptyProcess, canonicalCWD: "/project"),
                       .unavailable("Claude registry has no exact provider PID"))

        for contents in ["", #"{"pid":116,"sessionId":""}"#,
                         #"{"pid":0,"sessionId":"session"}"#] {
            let malformed = try NativeIdentityFixture()
            let process = try malformed.process(116)
            try malformed.file(malformed.home.appendingPathComponent(
                ".claude/sessions/bad.json").path, contents)
            XCTAssertEqual(malformed.reader().read(assistant: .claude, process: process, canonicalCWD: "/project"),
                           .unknown("Claude registry entry is malformed"))
        }

        let unsafe = try NativeIdentityFixture()
        let process = try unsafe.process(117)
        try unsafe.claudeRegistry(name: "117", pid: 117, session: "unsafe")
        XCTAssertEqual(chmod(unsafe.home.appendingPathComponent(
            ".claude/sessions/117.json").path, 0o640), 0)
        XCTAssertEqual(unsafe.reader().read(assistant: .claude, process: process, canonicalCWD: "/project"),
                       .unknown("Claude registry entry is unsafe"))
    }

    func testNativeClaudeRequiresTheExactPIDRegistryFilename() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(105)
        try fixture.claudeRegistry(name: "a", pid: 105, session: "first")
        try fixture.claudeRegistry(name: "b", pid: 105, session: "second")
        _ = try fixture.claudeTranscript(session: "first")
        _ = try fixture.claudeTranscript(project: "other", session: "second")
        XCTAssertEqual(fixture.reader().read(assistant: .claude, process: process, canonicalCWD: "/project"),
                       .unavailable("Claude registry has no exact provider PID"))
    }

    func testNativeClaudeRevalidatesRegistryAndProcessAfterRead() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(106)
        try fixture.claudeRegistry(name: "106", pid: 106, session: "stable")
        _ = try fixture.claudeTranscript(session: "stable")
        let registry = fixture.home.appendingPathComponent(".claude/sessions/106.json").path
        let reader = fixture.reader(beforePost: {
            try? Data(#"{"pid":106,"sessionId":"changed"}"#.utf8)
                .write(to: URL(fileURLWithPath: registry))
            _ = chmod(registry, 0o600)
        })
        XCTAssertEqual(reader.read(assistant: .claude, process: process, canonicalCWD: "/project"),
                       .busy("Claude registry changed during read"))
    }

    func testNativeSecureOpenRejectsSymlinkHardlinkAndSharedMode() throws {
        for unsafe in ["symlink", "hardlink", "mode", "directory", "owner"] {
            let fixture = try NativeIdentityFixture()
            let process = try fixture.process(107)
            try fixture.claudeRegistry(name: "107", pid: 107, session: "unsafe")
            let relative = try fixture.claudeTranscript(session: "unsafe")
            let path = fixture.home.appendingPathComponent(relative).path
            if unsafe == "symlink" {
                let target = fixture.root.appendingPathComponent("target.jsonl").path
                try fixture.file(target, "{}\n")
                XCTAssertEqual(unlink(path), 0); XCTAssertEqual(symlink(target, path), 0)
            } else if unsafe == "hardlink" {
                XCTAssertEqual(link(path, fixture.root.appendingPathComponent("alias").path), 0)
            } else if unsafe == "mode" {
                XCTAssertEqual(chmod(path, 0o640), 0)
            } else if unsafe == "directory" {
                XCTAssertEqual(unlink(path), 0)
                try fixture.directory(path)
            }
            let reader = fixture.reader(fileOwner: unsafe == "owner" ? { relative, owner in
                relative.hasSuffix("/unsafe.jsonl") ? owner &+ 1 : owner
            } : nil)
            XCTAssertEqual(reader.read(assistant: .claude, process: process, canonicalCWD: "/project"),
                           .unknown("native transcript file evidence is unsafe"), unsafe)
        }
    }

    func testNativeTranscriptPostFstatDetectsReplacement() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(108)
        try fixture.claudeRegistry(name: "108", pid: 108, session: "replace")
        let relative = try fixture.claudeTranscript(session: "replace")
        let path = fixture.home.appendingPathComponent(relative).path
        let reader = fixture.reader(beforePost: {
            let next = path + ".next"
            try? Data("different\n".utf8).write(to: URL(fileURLWithPath: next))
            _ = chmod(next, 0o600); _ = rename(next, path)
        })
        XCTAssertEqual(reader.read(assistant: .claude, process: process, canonicalCWD: "/project"),
                       .busy("native transcript metadata changed during read"))
    }

    func testNativeTranscriptRejectsSameSizeInPlaceMutation() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(118)
        try fixture.claudeRegistry(name: "118", pid: 118, session: "same-size")
        let relative = try fixture.claudeTranscript(session: "same-size", text: "AAAA\n")
        let path = fixture.home.appendingPathComponent(relative).path
        let reader = fixture.reader(beforePost: {
            try? Data("BBBB\n".utf8).write(to: URL(fileURLWithPath: path))
            _ = chmod(path, 0o600)
        })
        XCTAssertEqual(reader.read(assistant: .claude, process: process, canonicalCWD: "/project"),
                       .busy("native transcript content changed during read"))
    }

    func testNativeReaderRevalidatesProcessIncarnationAfterRead() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(109)
        try fixture.claudeRegistry(name: "109", pid: 109, session: "process-change")
        _ = try fixture.claudeTranscript(session: "process-change")
        let reader = fixture.reader(beforePost: { _ = try? fixture.process(109, ticks: 501) })
        XCTAssertEqual(reader.read(assistant: .claude, process: process, canonicalCWD: "/project"),
                       .busy("provider process identity changed during read"))
    }

    func testNativeCodexRequiresExactFDInodeAndThreadSource() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(201)
        let relative = try fixture.codexTranscript(pid: 201, fd: 7, session: "codex-session")
        guard case .available(let identity) = fixture.reader().read(
            assistant: .codex, process: process, canonicalCWD: "/project") else {
            return XCTFail("an exact Codex fd/inode must resolve")
        }
        XCTAssertEqual(identity.sessionID, "codex-session")
        XCTAssertEqual(identity.relativePath, relative)
        XCTAssertEqual(identity.source, .codexProcessFD)
    }

    func testNativeCodexMissingThreadSourceDoesNotFallBackToDateOrCWD() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(202)
        _ = try fixture.codexTranscript(pid: 202, fd: 8, session: "looks-right",
                                        threadSource: nil)
        try fixture.file(fixture.home.appendingPathComponent(
            ".codex/sessions/2026/09/16/newest.jsonl").path,
            #"{"type":"session_meta","payload":{"session_id":"newest","thread_source":"cli"}}"# + "\n")
        XCTAssertEqual(fixture.reader().read(assistant: .codex, process: process, canonicalCWD: "/project"),
                       .unavailable("Codex has no exact transcript fd with thread_source identity"))
    }

    func testNativeCodexRequiresUserCLIAndExactCanonicalCWD() throws {
        for mismatch in ["thread", "source", "cwd"] {
            let fixture = try NativeIdentityFixture()
            let process = try fixture.process(211)
            _ = try fixture.codexTranscript(
                pid: 211, fd: 21, session: "identity-\(mismatch)",
                threadSource: mismatch == "thread" ? "subagent" : "user",
                cwd: mismatch == "cwd" ? "/other" : nil,
                source: mismatch == "source" ? "vscode" : "cli")
            XCTAssertEqual(
                fixture.reader().read(
                    assistant: .codex, process: process, canonicalCWD: "/project"),
                .unavailable("Codex has no exact transcript fd with thread_source identity"),
                mismatch)
        }
    }

    func testNativeTranscriptReadsBoundedTailWithRealOffsetAndParserOwns413() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(212)
        let conversation = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
        try fixture.claudeRegistry(name: "212", pid: 212, session: conversation)
        let relative = ".claude/projects/-project/\(conversation).jsonl"
        let path = fixture.home.appendingPathComponent(relative).path
        try fixture.directory(URL(fileURLWithPath: path).deletingLastPathComponent().path)
        try Data(repeating: 0x78, count: LinuxNativeTranscriptWire.maximumSuffixBytes + 1)
            .write(to: URL(fileURLWithPath: path))
        XCTAssertEqual(chmod(path, 0o600), 0)
        let outcome = fixture.reader().read(
            assistant: .claude, process: process, canonicalCWD: "/project")
        guard case .available(let identity) = outcome else {
            return XCTFail("an oversized stable file must return its bounded suffix: \(outcome)")
        }
        XCTAssertEqual(identity.fileSize,
                       UInt64(LinuxNativeTranscriptWire.maximumSuffixBytes + 1))
        XCTAssertEqual(identity.suffixOffset, 1)
        XCTAssertEqual(identity.bytes.count, LinuxNativeTranscriptWire.maximumSuffixBytes)
        let evidence = LinuxNativeTranscriptEvidence(
            assistant: .claude, conversationMode: identity.conversationMode,
            providerConversationID: identity.sessionID,
            terminalIncarnation: String(repeating: "a", count: 64),
            fileDevice: identity.fileDevice, fileInode: identity.fileInode,
            fileSize: identity.fileSize,
            fileModifiedNanoseconds: identity.fileModifiedNanoseconds,
            suffixOffset: identity.suffixOffset)
        XCTAssertThrowsError(try LinuxNativeTranscriptWire.parse(
            bytes: identity.bytes, evidence: evidence, limit: 200)) { error in
            XCTAssertEqual(error as? LinuxNativeTranscriptWireError, .suffixLimitExceeded)
        }
    }

    func testNativeCodexKeepsHeaderIdentityWhileReturningOnlyBoundedTail() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(213)
        let relative = try fixture.codexTranscript(
            pid: 213, fd: 23, session: "bounded-codex", suffix: "bounded")
        let path = fixture.home.appendingPathComponent(relative)
        let handle = try FileHandle(forWritingTo: path)
        try handle.seekToEnd()
        try handle.write(contentsOf: Data(("{\"type\":\"event_msg\",\"payload\":{\"type\":\"agent_message\","
            + "\"message\":\"tail\"}}\n").utf8))
        try handle.close()
        var limits = LinuxNativeTranscriptReader.Limits()
        limits.maximumTranscriptBytes = 64
        let outcome = fixture.reader(limits: limits).read(
            assistant: .codex, process: process, canonicalCWD: "/project")
        guard case .available(let identity) = outcome else {
            return XCTFail("the held Codex header must bind its bounded suffix: \(outcome)")
        }
        XCTAssertGreaterThan(identity.suffixOffset, 0)
        XCTAssertEqual(identity.bytes.count, 64)
        XCTAssertEqual(identity.suffixOffset + UInt64(identity.bytes.count), identity.fileSize)
    }

    func testNativeCodexRefusesMultipleMatchingFDsInsteadOfTakingFirst() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(203)
        _ = try fixture.codexTranscript(pid: 203, fd: 9, session: "one", suffix: "one")
        _ = try fixture.codexTranscript(pid: 203, fd: 10, session: "two", suffix: "two")
        XCTAssertEqual(fixture.reader().read(assistant: .codex, process: process, canonicalCWD: "/project"),
                       .unknown("Codex transcript fd identity is ambiguous"))
    }

    func testNativeCodexRevalidatesDescriptorBindingAfterRead() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(205)
        let relative = try fixture.codexTranscript(pid: 205, fd: 11, session: "fd-change")
        let descriptor = fixture.proc.appendingPathComponent("205/fd/11").path
        let replacement = fixture.proc.appendingPathComponent("205/fd/12").path
        let transcript = fixture.home.appendingPathComponent(relative).path
        let reader = fixture.reader(beforePost: {
            _ = symlink(transcript, replacement)
            _ = unlink(descriptor)
        })
        XCTAssertEqual(reader.read(assistant: .codex, process: process, canonicalCWD: "/project"),
                       .busy("Codex process descriptor changed during read"))
    }

    func testNativeCodexRejectsFDReuseWithDifferentTranscriptIdentity() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(206)
        _ = try fixture.codexTranscript(pid: 206, fd: 13, session: "original",
                                        suffix: "original")
        let replacement = try fixture.codexTranscript(pid: 206, fd: 14,
                                                       session: "replaced", suffix: "replacement")
        XCTAssertEqual(unlink(fixture.proc.appendingPathComponent("206/fd/14").path), 0)
        let descriptor = fixture.proc.appendingPathComponent("206/fd/13").path
        let replacementPath = fixture.home.appendingPathComponent(replacement).path
        let reader = fixture.reader(beforePost: {
            _ = unlink(descriptor)
            _ = symlink(replacementPath, descriptor)
        })
        XCTAssertEqual(reader.read(assistant: .codex, process: process, canonicalCWD: "/project"),
                       .busy("Codex process descriptor changed during read"))
    }

    func testNativeReaderRejectsUnsafeIntermediateDirectoryAndUsesOnlyCanonicalProjectSlug() throws {
        let unsafe = try NativeIdentityFixture()
        let process = try unsafe.process(207)
        try unsafe.claudeRegistry(name: "207", pid: 207, session: "intermediate")
        _ = try unsafe.claudeTranscript(session: "intermediate")
        XCTAssertEqual(chmod(unsafe.home.appendingPathComponent(
            ".claude/projects").path, 0o770), 0)
        XCTAssertEqual(unsafe.reader().read(assistant: .claude, process: process, canonicalCWD: "/project"),
                       .unknown("native transcript file evidence is unsafe"))

        let limited = try NativeIdentityFixture()
        let limitedProcess = try limited.process(208)
        try limited.claudeRegistry(name: "208", pid: 208, session: "limited")
        _ = try limited.claudeTranscript(project: "-project", session: "limited")
        try limited.directory(limited.home.appendingPathComponent(
            ".claude/projects/two").path)
        var limits = LinuxNativeTranscriptReader.Limits()
        limits.maximumProjectDirectories = 1
        guard case .available(let identity) = limited.reader(limits: limits).read(
            assistant: .claude, process: limitedProcess, canonicalCWD: "/project") else {
            return XCTFail("unrelated project directories must not enter identity selection")
        }
        XCTAssertEqual(identity.relativePath, ".claude/projects/-project/limited.jsonl")
    }

    func testNativeClaudeRegistryEnumerationClosesEachEntryWithinBudget() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(209)
        for index in 0..<300 {
            try fixture.claudeRegistry(name: index == 299 ? "209" : String(format: "%03d", index),
                                       pid: index == 299 ? 209 : Int32(1_000 + index),
                                       session: index == 299 ? "selected" : "other-\(index)")
        }
        _ = try fixture.claudeTranscript(session: "selected")
        var limits = LinuxNativeTranscriptReader.Limits()
        limits.maximumRegistryEntries = 300
        limits.maximumRegistryBytes = 512 * 1_024
        var maximumOpen = 0
        guard case .available(let identity) = fixture.reader(
            limits: limits,
            registryOpenFileCount: { maximumOpen = max(maximumOpen, $0) }).read(
            assistant: .claude, process: process, canonicalCWD: "/project") else {
            return XCTFail("registry entries must be closed during enumeration")
        }
        XCTAssertEqual(identity.sessionID, "selected")
        XCTAssertEqual(maximumOpen, 1)
    }

    func testNativeClaudeRegistryEntryHasAnIndependentSizeBound() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(217)
        let path = fixture.home.appendingPathComponent(".claude/sessions/217.json").path
        try fixture.file(path, String(repeating: "x", count: 257))
        var limits = LinuxNativeTranscriptReader.Limits()
        limits.maximumRegistryBytes = 256
        XCTAssertEqual(fixture.reader(limits: limits).read(
            assistant: .claude, process: process, canonicalCWD: "/project"),
            .limit("Claude registry entry byte limit exceeded"))
    }

    func testNativeCodexPathRequiresASCIIDigitsAndRealCalendarRanges() throws {
        for (index, relative) in [
            ".codex/sessions/2026/13/16/rollout-invalid-month.jsonl",
            ".codex/sessions/2026/02/31/rollout-impossible-date.jsonl",
            ".codex/sessions/２０２６/09/16/rollout-unicode-year.jsonl",
        ].enumerated() {
            let fixture = try NativeIdentityFixture()
            let pid = Int32(218 + index)
            let process = try fixture.process(pid)
            let path = fixture.home.appendingPathComponent(relative).path
            let header = #"{"type":"session_meta","payload":{"session_id":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee","cwd":"/project","source":"cli","thread_source":"user"}}"#
            try fixture.file(path, header + "\n")
            try fixture.directory(fixture.proc.appendingPathComponent("\(pid)/fd").path)
            XCTAssertEqual(symlink(path, fixture.proc.appendingPathComponent("\(pid)/fd/7").path), 0)
            XCTAssertEqual(fixture.reader().read(
                assistant: .codex, process: process, canonicalCWD: "/project"),
                .unavailable("Codex has no exact transcript fd with thread_source identity"),
                relative)
        }
    }

    func testNativeObservationReaderParserAndWireJoinForBothProviders() throws {
        let conversation = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
        let incarnation = String(repeating: "a", count: 64)

        let claude = try NativeIdentityFixture()
        let claudeProcess = try claude.process(220)
        try claude.claudeRegistry(name: "220", pid: 220, session: conversation)
        _ = try claude.claudeTranscript(
            session: conversation,
            text: #"{"type":"assistant","message":{"content":[{"type":"text","text":"joined claude"}]}}"# + "\n")
        let claudeIdentity: LinuxNativeTranscriptIdentity
        guard case .available(let value) = claude.reader().read(
            assistant: .claude, process: claudeProcess, canonicalCWD: "/project") else {
            return XCTFail("the exact synthetic proc observation must select Claude history")
        }
        claudeIdentity = value
        let claudeEvidence = LinuxNativeTranscriptEvidence(
            assistant: .claude, conversationMode: claudeIdentity.conversationMode,
            providerConversationID: claudeIdentity.sessionID,
            terminalIncarnation: incarnation, fileDevice: claudeIdentity.fileDevice,
            fileInode: claudeIdentity.fileInode, fileSize: claudeIdentity.fileSize,
            fileModifiedNanoseconds: claudeIdentity.fileModifiedNanoseconds,
            suffixOffset: claudeIdentity.suffixOffset)
        let claudeBody = LinuxNativeTranscriptWire.webBody(try LinuxNativeTranscriptWire.parse(
            bytes: claudeIdentity.bytes, evidence: claudeEvidence, limit: 20))
        XCTAssertEqual((claudeBody["entries"] as? [[String: Any]])?.first?["text"] as? String,
                       "joined claude")

        let codex = try NativeIdentityFixture()
        let codexProcess = try codex.process(221)
        let relative = try codex.codexTranscript(pid: 221, fd: 7, session: conversation)
        let path = codex.home.appendingPathComponent(relative)
        let handle = try FileHandle(forWritingTo: path)
        try handle.seekToEnd()
        try handle.write(contentsOf: Data(#"{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","content":[{"type":"output_text","text":"joined codex"}]}}}"#.utf8))
        try handle.write(contentsOf: Data("\n".utf8))
        try handle.close()
        let codexIdentity: LinuxNativeTranscriptIdentity
        guard case .available(let value) = codex.reader().read(
            assistant: .codex, process: codexProcess, canonicalCWD: "/project") else {
            return XCTFail("the exact synthetic proc/fd observation must select Codex history")
        }
        codexIdentity = value
        let codexEvidence = LinuxNativeTranscriptEvidence(
            assistant: .codex, conversationMode: codexIdentity.conversationMode,
            providerConversationID: codexIdentity.sessionID,
            terminalIncarnation: incarnation, fileDevice: codexIdentity.fileDevice,
            fileInode: codexIdentity.fileInode, fileSize: codexIdentity.fileSize,
            fileModifiedNanoseconds: codexIdentity.fileModifiedNanoseconds,
            suffixOffset: codexIdentity.suffixOffset)
        let codexBody = LinuxNativeTranscriptWire.webBody(try LinuxNativeTranscriptWire.parse(
            bytes: codexIdentity.bytes, evidence: codexEvidence, limit: 20))
        XCTAssertEqual((codexBody["entries"] as? [[String: Any]])?.first?["text"] as? String,
                       "joined codex")
    }

    func testNativeDeadlineStartsPerReadAndResetsForRepeatedReads() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(210)
        try fixture.claudeRegistry(name: "210", pid: 210, session: "deadline")
        _ = try fixture.claudeTranscript(session: "deadline")
        var now: TimeInterval = 10
        let reader = fixture.reader(monotonic: { now })
        now = 10_000 // Delay before the first read must not consume its budget.
        guard case .available = reader.read(assistant: .claude, process: process, canonicalCWD: "/project") else {
            return XCTFail("a delayed first read needs a fresh deadline")
        }
        now = 20_000 // Every later read receives its own full budget too.
        guard case .available = reader.read(assistant: .claude, process: process, canonicalCWD: "/project") else {
            return XCTFail("a repeated read needs a fresh deadline")
        }
    }

    func testNativeReaderReturnsTypedLimitAndBusyOutcomes() throws {
        let fixture = try NativeIdentityFixture()
        let process = try fixture.process(204)
        _ = try fixture.codexTranscript(pid: 204, fd: 1, session: "one")
        _ = try fixture.codexTranscript(pid: 204, fd: 2, session: "two", suffix: "two")
        var limits = LinuxNativeTranscriptReader.Limits(); limits.maximumFDs = 1
        XCTAssertEqual(fixture.reader(limits: limits).read(assistant: .codex, process: process, canonicalCWD: "/project"),
                       .limit("Codex process descriptor limit exceeded"))
        var now: TimeInterval = 0
        let busy = fixture.reader(monotonic: { defer { now += 2 }; return now })
        XCTAssertEqual(busy.read(assistant: .codex, process: process, canonicalCWD: "/project"),
                       .busy("native transcript identity deadline elapsed"))
    }
}
