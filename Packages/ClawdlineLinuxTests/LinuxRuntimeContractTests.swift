import Foundation
import XCTest
@testable import ClawdlineApplication
@testable import ClawdlineLinux

private final class LockedStrings: @unchecked Sendable {
    private let lock = NSLock()
    private var values: [String] = []

    func append(_ value: String) {
        lock.lock()
        values.append(value)
        lock.unlock()
    }

    func snapshot() -> [String] {
        lock.lock()
        defer { lock.unlock() }
        return values
    }
}

private final class LockedBool: @unchecked Sendable {
    private let lock = NSLock()
    private var value: Bool
    init(_ value: Bool) { self.value = value }
    func get() -> Bool { lock.lock(); defer { lock.unlock() }; return value }
    func set(_ next: Bool) { lock.lock(); value = next; lock.unlock() }
}

private enum DeniedSecretStoreFailure: Error { case denied }

private struct DeniedSecretStore: SecretStore {
    let capabilities: Set<HostCapability> = [.secrets]
    func data(for _: String) throws -> Data? { throw DeniedSecretStoreFailure.denied }
    func set(_: Data, for _: String) throws { throw DeniedSecretStoreFailure.denied }
    func loadOrCreate(_: String, create _: @Sendable () throws -> Data) throws -> Data {
        throw DeniedSecretStoreFailure.denied
    }
    func rotate(_: String, replace _: @Sendable (Data?) throws -> Data) throws -> Data {
        throw DeniedSecretStoreFailure.denied
    }
    func remove(_: String) throws { throw DeniedSecretStoreFailure.denied }
}

private final class TestMemorySecretStore: SecretStore, @unchecked Sendable {
    let capabilities: Set<HostCapability> = [.secrets]
    private let lock = NSLock()
    private var values: [String: Data] = [:]

    func data(for account: String) throws -> Data? {
        lock.lock(); defer { lock.unlock() }
        return values[account]
    }
    func set(_ data: Data, for account: String) throws {
        lock.lock(); values[account] = data; lock.unlock()
    }
    func loadOrCreate(_ account: String, create: @Sendable () throws -> Data) throws -> Data {
        lock.lock(); defer { lock.unlock() }
        if let value = values[account] { return value }
        let value = try create(); values[account] = value; return value
    }
    func rotate(_ account: String, replace: @Sendable (Data?) throws -> Data) throws -> Data {
        lock.lock(); defer { lock.unlock() }
        let value = try replace(values[account]); values[account] = value; return value
    }
    func remove(_ account: String) throws {
        lock.lock(); values.removeValue(forKey: account); lock.unlock()
    }
}

private final class FakeLinuxLifecycleRuntime: LinuxLifecyclePerforming {
    private let callsLock = NSLock()
    private var storedCalls: [String] = []
    var calls: [String] { callsLock.lock(); defer { callsLock.unlock() }; return storedCalls }
    var sessionID = "%durable"

    private func record(_ call: String) {
        callsLock.lock(); storedCalls.append(call); callsLock.unlock()
    }

    private func receipt(commandID: String, operation: TerminalEffectOperation,
                         channel: String, sessionID: String?, observed: Bool) throws
        -> LinuxLifecycleReceipt {
        var progress = TerminalEffectProgress(commandID: commandID,
                                              operation: operation, channel: channel)
        try progress.advance(to: .executed)
        try progress.advance(to: .delivered)
        if observed { try progress.advance(to: .observed) }
        return LinuxLifecycleReceipt(progress: progress, sessionID: sessionID,
                                     tty: "/dev/pts/42", attachCommand: nil,
                                     output: operation == .observe ? "screen" : nil)
    }

    func create(commandID: String, projectRoot: String, assistant: Assistant,
                model: String?, reasoningEffort: ReasoningEffort?, permission: Permission,
                additionalDirectory: String?, resumeSessionID: String?) throws
        -> LinuxLifecycleReceipt {
        record("create:\(commandID)")
        return try receipt(commandID: commandID, operation: .create,
                           channel: projectRoot, sessionID: sessionID, observed: true)
    }

    func send(commandID: String, sessionID: String, text: String) throws
        -> LinuxLifecycleReceipt {
        record("send:\(commandID):\(text)")
        return try receipt(commandID: commandID, operation: .send,
                           channel: sessionID, sessionID: sessionID, observed: false)
    }

    func observe(commandID: String, sessionID: String) throws -> LinuxLifecycleReceipt {
        record("observe:\(commandID)")
        return try receipt(commandID: commandID, operation: .observe,
                           channel: sessionID, sessionID: sessionID, observed: true)
    }

    func close(commandID: String, sessionID: String) throws -> LinuxLifecycleReceipt {
        record("close:\(commandID)")
        return try receipt(commandID: commandID, operation: .close,
                           channel: sessionID, sessionID: sessionID, observed: true)
    }
}

private final class LinuxRelayTestTransport: CloudTransporting, @unchecked Sendable {
    let source: CloudInboundCommandSource
    let commands: CloudInboundCommandStream
    let readyGenerations: AsyncStream<UInt64>
    let outboundReceipts: AsyncStream<CloudOutboundTransportReceipt>
    private let readyContinuation: AsyncStream<UInt64>.Continuation
    private let receiptContinuation: AsyncStream<CloudOutboundTransportReceipt>.Continuation
    private let lock = NSLock()
    private var writes: [Data] = []
    private var shutdownCount = 0
    private var connectFailures: [CloudTransportError]
    private var connectCount = 0

    init(connectFailures: [CloudTransportError] = []) {
        self.connectFailures = connectFailures
        let source = CloudInboundCommandSource()
        self.source = source
        commands = source.stream
        var ready: AsyncStream<UInt64>.Continuation!
        readyGenerations = AsyncStream(bufferingPolicy: .bufferingNewest(1)) { ready = $0 }
        readyContinuation = ready
        var receipts: AsyncStream<CloudOutboundTransportReceipt>.Continuation!
        outboundReceipts = AsyncStream { receipts = $0 }
        receiptContinuation = receipts
    }

    func connect(role: CloudTransportRole) async throws {
        XCTAssertEqual(role, .machine)
        lock.lock()
        connectCount += 1
        let failure = connectFailures.isEmpty ? nil : connectFailures.removeFirst()
        lock.unlock()
        if let failure { throw failure }
        readyContinuation.yield(1)
    }

    func sendExactPublishFrame(_ bytes: Data) async throws {
        lock.lock(); writes.append(bytes); lock.unlock()
    }

    func setInboundRefusalHandler(_ handler: CloudTransport.InboundRefusalHandler?) async {}
    func setTerminalAuthorizationHandler(
        _ handler: CloudTransport.TerminalAuthorizationHandler?
    ) async {}

    func shutdown() async {
        lock.lock(); shutdownCount += 1; lock.unlock()
        source.finish()
        readyContinuation.finish()
        receiptContinuation.finish()
    }

    @discardableResult
    func deliver(_ command: CloudInboundCommand) -> Bool { source.yield(command) }
    func ready(_ generation: UInt64) { readyContinuation.yield(generation) }
    func receipt(_ value: CloudOutboundTransportReceipt) { receiptContinuation.yield(value) }
    func writtenFrames() -> [Data] { lock.lock(); defer { lock.unlock() }; return writes }
    func shutdowns() -> Int { lock.lock(); defer { lock.unlock() }; return shutdownCount }
    func connects() -> Int { lock.lock(); defer { lock.unlock() }; return connectCount }
}

private struct SchemaTwoIngressSeal: Encodable {
    let authorization: String?
    let operation: LinuxIngressOperation
    let commandID: String
    let taskID: String
    let sessionID: String?
    let projectRoot: String?
    let assistant: String?
    let text: String?
    let acknowledge: Bool
    let authorizeRecovery: Bool

    init(_ request: LinuxIngressRequest) {
        authorization = nil
        operation = request.operation
        commandID = request.commandID
        taskID = request.taskID
        sessionID = request.sessionID
        projectRoot = request.projectRoot
        assistant = request.assistant
        text = request.text
        acknowledge = request.acknowledge
        authorizeRecovery = false
    }

    func bytes() throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return try encoder.encode(self)
    }
}

final class LinuxRuntimeContractTests: XCTestCase {
    func testProjectRootPolicyRejectsTraversalAndSymlink() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-root-\(UUID().uuidString)")
        let root = scratch.appendingPathComponent("project")
        let linked = scratch.appendingPathComponent("linked")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o700])
        try FileManager.default.createSymbolicLink(at: linked, withDestinationURL: root)
        var metadata = stat()
        XCTAssertEqual(lstat(root.path, &metadata), 0)
        let policy = ProjectRootPolicy(allowedCanonicalRoots: [root.path],
                                       expectedUID: metadata.st_uid, expectedGID: metadata.st_gid)
        #if os(Linux)
        let inspector: any ProjectRootInspecting = LinuxProjectRootInspector()
        #else
        // macOS rewrites its public `/var` alias inconsistently between URL canonicalization and
        // lstat. The shared policy is still exercised here with fixed host evidence; Ubuntu uses
        // the real descriptor/lstat inspector in this test and the lifecycle test below.
        let inspector: any ProjectRootInspecting = FixedProjectRootInspector(
            canonical: root.path, linked: linked.path,
            uid: metadata.st_uid, gid: metadata.st_gid)
        #endif
        guard case .success = policy.admit(root.path, inspector: inspector) else {
            return XCTFail("a canonical owned root should be admitted")
        }
        guard case .failure(let linkedRefusal) = policy.admit(linked.path, inspector: inspector) else {
            return XCTFail("a symlink root should be refused")
        }
        XCTAssertEqual(linkedRefusal.code, .symbolicLink)
        XCTAssertNil(try? ProjectRootPolicy.relativePath(
            of: root.appendingPathComponent("../escape").path,
            beneath: CanonicalProjectRoot(path: root.path, ownerUID: metadata.st_uid,
                                          ownerGID: metadata.st_gid)).get())
        XCTAssertTrue(ProjectRootPolicy.pathsOverlap("/srv/clawdline", "/srv/clawdline/secrets"))
        XCTAssertTrue(ProjectRootPolicy.pathsOverlap("/srv", "/srv/clawdline"))
        XCTAssertFalse(ProjectRootPolicy.pathsOverlap("/srv/projects", "/srv/clawdline"))
    }

    func testProcIdentityUsesExactStartTokenAndGroup() throws {
        var fields = Array(repeating: "0", count: 20)
        fields[0] = "S"
        fields[2] = "4242"
        fields[19] = "998877"
        let identity = try XCTUnwrap(LinuxProcfs.parseStat("71 (claude) " + fields.joined(separator: " "),
                                                           pid: 71))
        XCTAssertEqual(identity.pid, 71)
        XCTAssertEqual(identity.processGroupID, 4242)
        XCTAssertEqual(identity.startToken, "998877")

        fields[19] = "998878" // representative PID-reuse mutation
        let replacement = try XCTUnwrap(LinuxProcfs.parseStat(
            "71 (claude) " + fields.joined(separator: " "), pid: 71))
        XCTAssertNotEqual(identity, replacement)

        let credentials = try XCTUnwrap(LinuxProcfs.parseCredentials("""
        Name:\tclaude
        Uid:\t1000\t1001\t1002\t1003
        Gid:\t2000\t2001\t2002\t2003
        Groups:\t2001 3000
        """))
        XCTAssertEqual(credentials.effectiveUID, 1001)
        XCTAssertEqual(credentials.effectiveGID, 2001)
        XCTAssertEqual(credentials.supplementaryGroups, [2001, 3000])
        let host = LinuxProcessHost(serviceUID: 1001, serviceGID: 2001,
                                    supplementaryGroups: [2001, 3000])
        XCTAssertTrue(host.credentialsMatch(credentials))
        XCTAssertFalse(host.credentialsMatch(.init(
            effectiveUID: 1001, effectiveGID: 2999,
            supplementaryGroups: [2001, 3000]))) // changed effective GID
        XCTAssertFalse(host.credentialsMatch(.init(
            effectiveUID: 1001, effectiveGID: 2001,
            supplementaryGroups: [2001, 3999]))) // changed supplementary group
        let sameTickDifferentPID = HostProcessIdentity(
            pid: 72, processStart: identity.processStart,
            startToken: identity.startToken, processGroupID: identity.processGroupID)
        XCTAssertNotEqual(identity, sameTickDifferentPID)
        let changedGroup = HostProcessIdentity(
            pid: identity.pid, processStart: identity.processStart,
            startToken: identity.startToken, processGroupID: 4343)
        XCTAssertNotEqual(identity, changedGroup)
    }

    func testClosedProviderEnvironmentCannotLeakInheritedCredentials() throws {
        setenv("CLAUDE_CODE_MESSAGING_TOKEN", "must-not-cross", 1)
        setenv("OPENAI_API_KEY", "must-not-cross", 1)
        defer {
            unsetenv("CLAUDE_CODE_MESSAGING_TOKEN")
            unsetenv("OPENAI_API_KEY")
        }
        let environment = try ProviderEnvironmentPolicy.closed(
            home: "/var/lib/clawdline/home", temporaryDirectory: "/var/lib/clawdline/tmp").get()
        XCTAssertEqual(Set(environment.keys), ProviderEnvironmentPolicy.allowedKeys)
        XCTAssertNil(environment["CLAUDE_CODE_MESSAGING_TOKEN"])
        XCTAssertNil(environment["OPENAI_API_KEY"])
        XCTAssertFalse(environment.values.contains("must-not-cross"))
    }

    func testUnavailableCapabilityRefusesBeforeLaunchAndMenuEffects() throws {
        let request = ProviderLaunchRequest(
            commandID: "contract-1", projectRoot: "/var/lib/clawdline/projects/demo",
            assistant: .codex, terminalMode: .tmuxDetachedSession)
        guard case .failure(.capabilityUnavailable(let launch)) = SessionLaunchPolicy.admit(
            request, terminalCapabilities: []) else {
            return XCTFail("missing tmux must be a typed refusal")
        }
        XCTAssertEqual(launch.capability, .terminalTmux)
        guard case .failure(.capabilityUnavailable(let answer)) = TerminalMenuAnswerPolicy.admit(
            [0x31], backend: .tmux, terminalCapabilities: []) else {
            return XCTFail("missing tmux must refuse menu input before a key effect")
        }
        XCTAssertEqual(answer.capability, .terminalTmux)
        XCTAssertEqual(HostCapabilityUnavailable.code, "capability_unavailable")
    }

    func testMenuAllowlistAndLifecycleStagesStayClosed() throws {
        XCTAssertEqual(try TerminalMenuAnswerPolicy.admit(
            [0x31], backend: .tmux, terminalCapabilities: [.terminalTmux]).get(), .digit(1))
        XCTAssertEqual(try TerminalMenuAnswerPolicy.admit(
            TerminalMenuAnswerPolicy.backTab, backend: .tmux,
            terminalCapabilities: [.terminalTmux]).get(), .key([0x1b, 0x5b, 0x5a]))
        XCTAssertThrowsError(try TerminalMenuAnswerPolicy.admit(
            [0x1b, 0x5b, 0x41], backend: .tmux,
            terminalCapabilities: [.terminalTmux]).get())

        var progress = TerminalEffectProgress(commandID: "stages-1", operation: .send,
                                              channel: "%1")
        XCTAssertThrowsError(try progress.advance(to: .observed))
        try progress.advance(to: .executed)
        try progress.advance(to: .delivered)
        try progress.advance(to: .observed)
        XCTAssertEqual(progress.stages, [.accepted, .executed, .delivered, .observed])
    }

    func testSharedSchedulerSerializesSameChannelSendCloseNestedAndMaintenance() throws {
        let owner = TerminalCommandScheduler(
            label: "linux-scheduler-\(UUID().uuidString)",
            limits: TerminalWorkLimits(total: 3, perChannel: 2,
                                       maximumInputBytes: 32, maximumInventory: 4))
        let firstEntered = DispatchSemaphore(value: 0)
        let releaseFirst = DispatchSemaphore(value: 0)
        let closeFinished = DispatchSemaphore(value: 0)
        let group = DispatchGroup()
        let order = LockedStrings()

        group.enter()
        DispatchQueue.global().async {
            defer { group.leave() }
            _ = try? owner.run(commandID: "send-one", channel: "%1", operation: .send) {
                order.append("send-preflight")
                firstEntered.signal()
                releaseFirst.wait()
                order.append("send-effect")
            }
        }
        XCTAssertEqual(firstEntered.wait(timeout: .now() + 1), .success)
        group.enter()
        DispatchQueue.global().async {
            defer { group.leave() }
            _ = try? owner.run(commandID: "close-one", channel: "%1", operation: .close) {
                order.append("close-preflight")
                closeFinished.signal()
            }
        }
        XCTAssertEqual(closeFinished.wait(timeout: .now() + 0.05), .timedOut,
                       "same-channel close preflight must wait behind send")
        XCTAssertEqual(owner.drainSnapshot().outstanding, 2)
        owner.setRestartMaintenance(active: true, requestID: "restart-one")
        XCTAssertThrowsError(try owner.run(
            commandID: "observe-refused", channel: "%2", operation: .observe) {}) {
            XCTAssertEqual(($0 as? TerminalLifecycleFailure)?.code, .maintenance)
        }
        releaseFirst.signal()
        XCTAssertEqual(group.wait(timeout: .now() + 2), .success)
        XCTAssertEqual(order.snapshot(), ["send-preflight", "send-effect", "close-preflight"])
        XCTAssertEqual(owner.drainSnapshot().outstanding, 0)
        owner.setRestartMaintenance(active: false, requestID: "wrong-restart")
        XCTAssertNotNil(owner.maintenanceRefusal(), "stale reopen must not clear maintenance")
        owner.setRestartMaintenance(active: false, requestID: "restart-one")

        try owner.run(commandID: "outer", channel: "%1", operation: .close) {
            try owner.run(commandID: "nested", channel: "%1", operation: .enumerate) {
                XCTAssertTrue(owner.isCurrentlyOnWorkerQueue)
                XCTAssertEqual(owner.drainSnapshot().outstanding, 1)
            }
        }
        XCTAssertEqual(owner.drainSnapshot().outstanding, 0)
    }

    func testCommandRunnerEnforcesAggregateOutputAndStdinDeadline() throws {
        let runner = LinuxCommandRunner(environment: ["PATH": "/usr/bin:/bin"])
        func expect(_ code: LinuxRuntimeFailureCode, script: String,
                    input: Data? = nil, timeout: TimeInterval = 1) {
            XCTAssertThrowsError(try runner.run(
                executable: "/bin/sh", arguments: ["-c", script], input: input,
                timeout: timeout, maximumOutputBytes: 1024)) {
                XCTAssertEqual(($0 as? LinuxRuntimeFailure)?.code, code)
            }
        }
        expect(.outputLimit, script: "i=0; while [ $i -lt 300 ]; do printf 12345678; i=$((i+1)); done")
        expect(.outputLimit, script: "i=0; while [ $i -lt 300 ]; do printf 12345678 >&2; i=$((i+1)); done")
        expect(.outputLimit, script: "i=0; while [ $i -lt 80 ]; do printf 12345678; printf 12345678 >&2; i=$((i+1)); done")
        let started = ProcessInfo.processInfo.systemUptime
        expect(.commandTimeout, script: "sleep 5",
               input: Data(repeating: 0x61, count: 65_536), timeout: 0.1)
        XCTAssertLessThan(ProcessInfo.processInfo.systemUptime - started, 1,
                          "a child that refuses stdin must remain deadline bounded")
    }

    func testSecretCoordinatorSerializesMixedOperationsAcrossInstances() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-secrets-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(at: scratch, withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o700])
        var metadata = stat()
        XCTAssertEqual(lstat(scratch.path, &metadata), 0)
        let root = CanonicalProjectRoot(path: scratch.path,
                                        ownerUID: metadata.st_uid, ownerGID: metadata.st_gid)
        let first = LinuxProtectedFileSecretStore(root: root)
        let second = LinuxProtectedFileSecretStore(root: root)
        try first.set(Data("initial".utf8), for: "account")
        let rotateEntered = DispatchSemaphore(value: 0)
        let releaseRotate = DispatchSemaphore(value: 0)
        let setFinished = DispatchSemaphore(value: 0)
        let group = DispatchGroup()
        group.enter()
        DispatchQueue.global().async {
            defer { group.leave() }
            _ = try? first.rotate("account") { old in
                XCTAssertEqual(old, Data("initial".utf8))
                rotateEntered.signal()
                releaseRotate.wait()
                return Data("rotated".utf8)
            }
        }
        XCTAssertEqual(rotateEntered.wait(timeout: .now() + 1), .success)
        group.enter()
        DispatchQueue.global().async {
            defer { group.leave() }
            try? second.set(Data("set-after-rotate".utf8), for: "account")
            setFinished.signal()
        }
        XCTAssertEqual(setFinished.wait(timeout: .now() + 0.05), .timedOut,
                       "plain set must share the closed-operation account coordinator")
        releaseRotate.signal()
        XCTAssertEqual(group.wait(timeout: .now() + 2), .success)
        XCTAssertEqual(try first.data(for: "account"), Data("set-after-rotate".utf8))
    }

    func testHealthSeparatesCompiledConfiguredUsableAndAuthenticated() throws {
        let health = LinuxRuntimeIdentity.current
        XCTAssertFalse(health.ready)
        XCTAssertEqual(health.readinessCode, "w4_runtime_not_configured")
        XCTAssertTrue(health.supportedCapabilities.isEmpty)
        XCTAssertTrue(health.capabilityStates.contains {
            $0.capability == HostCapability.terminalTmux.rawValue
                && $0.compiled && !$0.configured && !$0.usable
        })
        XCTAssertTrue(health.providers.allSatisfy {
            !$0.executableConfigured && !$0.authenticated && !$0.usable && $0.lifecycle.isEmpty
        })
    }

    func testW51LinuxComposesSharedDurabilityUnderCandidateAuthority() async throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-cloud-runtime-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(
            at: scratch, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        var metadata = stat()
        XCTAssertEqual(lstat(scratch.path, &metadata), 0)
        let secretURL = scratch.appendingPathComponent("secrets", isDirectory: true)
        try FileManager.default.createDirectory(
            at: secretURL, withIntermediateDirectories: false,
            attributes: [.posixPermissions: 0o700])
        let secretRoot = CanonicalProjectRoot(
            path: secretURL.path, ownerUID: metadata.st_uid, ownerGID: metadata.st_gid)
        let secrets = LinuxProtectedFileSecretStore(root: secretRoot)

        let linux = try LinuxDurableCloudRuntime(
            stateDirectory: scratch.path, expectedUID: metadata.st_uid, secrets: secrets)
        XCTAssertTrue(linux.readiness.durable)
        XCTAssertEqual(linux.readiness.authority, "candidate")
        XCTAssertTrue(linux.readiness.cutoverRequired)
        XCTAssertFalse(linux.readiness.emissionEnabled)
        XCTAssertEqual(linux.readiness.code, "w5_executor_identity_missing")
        XCTAssertEqual(linux.candidateAuthority.publicCommit,
                       "38eb822575e3c309a776a9e3e2874c7062d8fb75")
        XCTAssertEqual(linux.candidateAuthority.packageTree,
                       "3ee391a4af73f9688510c19106d38ba325227051")
        XCTAssertFalse(linux.candidateAuthority.permitsNormativeCutover)
        XCTAssertFalse(linux.candidateAuthority.permitsEmission(wireVersion: 1))
        XCTAssertFalse(linux.candidateAuthority.permitsEmission(wireVersion: 2))
        XCTAssertFalse(linux.candidateAuthority.permitsClientFloorRaise(to: "2"))
        let ledgerRows = try await linux.runtime.ledger.rowCount()
        let spoolRows = await linux.runtime.spool.rowsSnapshot()
        let metricRuntime = await linux.runtime.spool.runtime
        XCTAssertEqual(ledgerRows, 0)
        XCTAssertTrue(spoolRows.isEmpty)
        XCTAssertNil(metricRuntime, "Linux must suppress unsupported spool metrics, not label them mac")

        XCTAssertThrowsError(try LinuxDurableCloudRuntime(
            stateDirectory: scratch.path, expectedUID: metadata.st_uid, secrets: secrets)) {
            XCTAssertEqual($0 as? CloudDurableStoreFailure, .writerLockHeld)
        }
    }

    func testW54CloudLoginStreamsOnlyPublicInvitationThenProtectedReceipt() async throws {
        let config = LinuxDaemonConfiguration(
            version: 2,
            listen: LinuxListenConfiguration(host: "127.0.0.1", port: 7717),
            stateDirectory: "/var/lib/clawdline",
            runtimeDirectory: "/run/clawdline",
            secretFile: "/var/lib/clawdline/daemon.secret")
        let invitation = LinuxCloudLoginInvitation(
            userCode: "ABCD-EFGH",
            verificationURL: "https://api.clawdline.com/device",
            verificationCompleteURL: "https://api.clawdline.com/device?user_code=ABCD-EFGH",
            expiresInSeconds: 600,
            pollIntervalSeconds: 5)
        let emitted = LockedStrings()
        let receipt = try await LinuxCloudEnrollment.execute(
            configuration: config,
            emit: { emitted.append(String(decoding: $0, as: UTF8.self)) },
            state: { .missing },
            begin: { metadata in
                XCTAssertEqual(metadata.platform, "linux")
                XCTAssertTrue(metadata.name.hasPrefix("Clawdline Linux "))
                return (invitation, {
                    .complete(accountID: "usr_alpha", machineID: "machine_alpha")
                })
            })
        let publicOutput = emitted.snapshot().joined()
        XCTAssertTrue(publicOutput.contains("ABCD-EFGH"))
        XCTAssertTrue(publicOutput.contains("authorization_required"))
        XCTAssertFalse(publicOutput.contains("deviceCode"))
        XCTAssertFalse(publicOutput.contains("device_code"))
        XCTAssertFalse(publicOutput.contains("machine_credential"))
        XCTAssertFalse(publicOutput.contains("private"))
        let finalOutput = String(decoding: receipt, as: UTF8.self)
        XCTAssertTrue(finalOutput.contains("enrollment_complete"))
        XCTAssertTrue(finalOutput.contains("protectedIdentity"))
        XCTAssertFalse(finalOutput.contains("credential"))
    }

    func testW54CloudLoginIsIdempotentAndDoesNotOpenAnotherAuthorization() async throws {
        let config = LinuxDaemonConfiguration(
            version: 2,
            listen: LinuxListenConfiguration(host: "127.0.0.1", port: 7717),
            stateDirectory: "/var/lib/clawdline",
            runtimeDirectory: "/run/clawdline",
            secretFile: "/var/lib/clawdline/daemon.secret")
        let began = LockedBool(false)
        let receipt = try await LinuxCloudEnrollment.execute(
            configuration: config,
            emit: { _ in XCTFail("an enrolled machine must not emit a new invitation") },
            state: { .enrolled(accountID: "usr_alpha", machineID: "machine_alpha") },
            begin: { _ in
                began.set(true)
                throw LinuxCompositionError.internalFailure
            })
        XCTAssertFalse(began.get())
        XCTAssertTrue(String(decoding: receipt, as: UTF8.self).contains("already_enrolled"))
    }

    func testW54CloudLoginCommandHasAnExplicitProtectedConfigBoundary() throws {
        XCTAssertEqual(
            try LinuxCompositionCommand.parse([
                "cloud-login", "--config", "/etc/clawdline/daemon.json"
            ]),
            .cloudLogin("/etc/clawdline/daemon.json"))
        XCTAssertThrowsError(try LinuxCompositionCommand.parse(["cloud-login"]))
    }

    func testW54LinuxRelayOwnsDurableReplayRotationIngressAndStop() async throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-w54-relay-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(
            at: scratch, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        let project = scratch.appendingPathComponent("project", isDirectory: true)
        try FileManager.default.createDirectory(
            at: project, withIntermediateDirectories: false,
            attributes: [.posixPermissions: 0o700])

        let secretStore = TestMemorySecretStore()
        let authority = CloudExecutorIdentityAuthority(store: secretStore)
        let machineKey = try CloudDeviceKeyPair(
            privateKeyRaw: Data(repeating: 0x41, count: 32))
        let viewerKey = try CloudDeviceKeyPair(
            privateKeyRaw: Data(repeating: 0x42, count: 32))
        let master = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x43, count: 32))
        let initial = try authority.provision(
            accountID: "account-w54", machineID: "machine-w54",
            deviceKey: machineKey, masterSecret: master,
            importedPairedDevices: [CloudExecutorPairedDevice(
                deviceID: "viewer-w54", signingKey: viewerKey.publicKeyRaw,
                fingerprint: viewerKey.pairingFingerprint,
                pairedAtMilliseconds: 1, identityGeneration: 1,
                capabilities: CloudExecutorIdentityAuthority.defaultCapabilities)])
        let durable = try CloudDurableRuntime.open(
            directory: scratch.appendingPathComponent("cloud", isDirectory: true),
            runtime: nil, strictPersistedFrameValidation: true)
        let transport = LinuxRelayTestTransport(connectFailures: [.connectionFailed("transient")])
        let appIdentity = CloudAppIdentity(
            machineID: initial.machineID, deviceID: initial.deviceID,
            keyID: initial.keyID, masterSecret: master, signingKey: machineKey)
        let outbound = CloudDurableOutboundComposition(
            spool: durable.spool, transport: transport, identity: appIdentity,
            nowMilliseconds: { UInt64(max(0, Date().timeIntervalSince1970 * 1_000)) })
        let ingressStore = try LinuxDurableStateStore(
            stateDirectory: scratch.appendingPathComponent("daemon", isDirectory: true).path)
        let lifecycle = FakeLinuxLifecycleRuntime()
        let startup = try LinuxStartupReconciler.reconcile(
            store: ingressStore, inventory: .complete([]))
        let ingress = LinuxDaemonIngressOwner(store: ingressStore, runtime: lifecycle)
        ingress.completeStartup(startup)
        let gate = LockedBool(true)
        let relayStatus = LinuxRelayRuntimeStatus()
        let relayDiagnostics = LockedStrings()
        let relay = LinuxRelayRuntimeOwner(
            machine: CloudMachineIdentity(accountID: initial.accountID,
                                          machineID: initial.machineID),
            identityAuthority: authority, transport: transport, outbound: outbound,
            ingress: ingress, commandsEnabled: { gate.get() },
            diagnostic: { relayDiagnostics.append($0) },
            stateObserver: { relayStatus.record($0) })

        func waitUntil(_ message: String, _ predicate: () async -> Bool) async throws {
            for _ in 0..<200 {
                if await predicate() { return }
                try await Task.sleep(nanoseconds: 5_000_000)
            }
            XCTFail(message)
            throw LinuxDurableStateFailure(code: "test_timeout", message: message)
        }

        func waitUntilDynamic(
            _ message: () -> String, _ predicate: () async -> Bool
        ) async throws {
            for _ in 0..<200 {
                if await predicate() { return }
                try await Task.sleep(nanoseconds: 5_000_000)
            }
            let current = message()
            XCTFail(current)
            throw LinuxDurableStateFailure(code: "test_timeout", message: current)
        }

        let retryDelays = LockedStrings()
        let supervisor = LinuxRelayRuntimeSupervisor(
            owner: relay, status: relayStatus, diagnostic: { relayDiagnostics.append($0) },
            sleep: { retryDelays.append(String($0)) })
        supervisor.start()
        try await waitUntil("transient initial connection was not retried") {
            await relay.state == .running
        }
        let runningState = await relay.state
        XCTAssertEqual(runningState, .running)
        XCTAssertEqual(transport.connects(), 2)
        XCTAssertEqual(retryDelays.snapshot(), ["0.25"])
        XCTAssertEqual(transport.shutdowns(), 0,
                       "transient initial failure must not terminate the durable owner")
        try await waitUntil("initial authenticated ready generation was not observed") {
            await relay.authenticatedGeneration == 1
        }

        // Initial publication is sent once; an authenticated ready generation replays the exact
        // uncertain frame rather than resealing or allocating another sequence.
        try await relay.publish(
            Data("snapshot".utf8), channel: "s/machine-w54/session",
            logicalID: "snapshot-1")
        try await waitUntil("initial durable publish did not reach the transport") {
            transport.writtenFrames().count == 1
        }
        let firstFrame = try XCTUnwrap(transport.writtenFrames().first)
        transport.ready(2)
        try await waitUntil("reconnect did not replay the uncertain exact frame") {
            transport.writtenFrames().count == 2
        }
        XCTAssertEqual(transport.writtenFrames()[1], firstFrame)

        let first = try JSONDecoder().decode(CloudPublishFrame.self, from: firstFrame)
        transport.receipt(CloudOutboundTransportReceipt(
            channel: first.envelope.ch, sequence: Int64(first.envelope.seq),
            kind: .peerRejected(CloudOutboundPeerRejection(
                code: .forbidden, field: nil, disposition: .terminal))))
        try await waitUntil("terminal publish refusal was not settled") {
            await durable.spool.row(seq: Int64(first.envelope.seq))?.state == .rejected
        }
        XCTAssertEqual(transport.writtenFrames().count, 2,
                       "terminal refusal must not enter a reconnect/retry loop")

        try await relay.publish(
            Data("retry".utf8), channel: "t/machine-w54/session",
            logicalID: "retry-1")
        try await waitUntil("retry source did not publish") { transport.writtenFrames().count == 3 }
        let retrySource = try JSONDecoder().decode(
            CloudPublishFrame.self, from: transport.writtenFrames()[2])
        transport.receipt(CloudOutboundTransportReceipt(
            channel: retrySource.envelope.ch, sequence: Int64(retrySource.envelope.seq),
            kind: .peerRejected(CloudOutboundPeerRejection(
                code: .unavailable, field: .ciphertext,
                disposition: .retryNewAttempt))))
        try await waitUntil("typed retry refusal did not create a new durable attempt") {
            transport.writtenFrames().count == 4
        }
        let retryAttempt = try JSONDecoder().decode(
            CloudPublishFrame.self, from: transport.writtenFrames()[3])
        XCTAssertGreaterThan(retryAttempt.envelope.seq, retrySource.envelope.seq)
        transport.receipt(CloudOutboundTransportReceipt(
            channel: retryAttempt.envelope.ch, sequence: Int64(retryAttempt.envelope.seq),
            kind: .delivered))
        try await waitUntil("retry attempt delivery did not settle before ingress") {
            await durable.spool.row(seq: Int64(retryAttempt.envelope.seq))?.state == .acked
        }

        // A reconnect after protected key rotation adopts the freshly read key for new frames.
        let rotatedKey = try CloudDeviceKeyPair(
            privateKeyRaw: Data(repeating: 0x51, count: 32))
        let rotatedMaster = try CloudMasterSecret(
            rawRepresentation: Data(repeating: 0x52, count: 32))
        _ = try authority.rotateKeys(
            deviceKey: rotatedKey, masterSecret: rotatedMaster, keyID: "master-v2",
            expectedGeneration: authority.snapshot().identityGeneration)
        transport.ready(3)
        try await waitUntil("rotated reconnect did not refresh protected identity") {
            await relay.authenticatedGeneration == 3
        }
        try await relay.publish(
            Data("rotated".utf8), channel: "s/machine-w54/rotated",
            logicalID: "rotated-1")
        try await waitUntil("rotated frame did not publish") { transport.writtenFrames().count >= 5 }
        let rotatedFrame = try JSONDecoder().decode(
            CloudPublishFrame.self, from: transport.writtenFrames().last!)
        XCTAssertEqual(rotatedFrame.envelope.keyID, "master-v2")

        // Cloud sender+sequence, not caller JSON, is the stable idempotency identity.
        let create = LinuxIngressRequest(
            operation: .create, commandID: "caller-controlled", taskID: "task-w54",
            projectRoot: project.path, assistant: .claude)
        let createBytes = try JSONEncoder().encode(create)
        let timestamp = UInt64(Date().timeIntervalSince1970 * 1_000)
        let inbound = CloudInboundCommand(
            channel: "ctl/machine-w54", sequence: 77, timestamp: timestamp,
            sender: "viewer-w54", plaintext: createBytes, isDispatch: true)
        XCTAssertEqual(transport.shutdowns(), 0)
        XCTAssertTrue(transport.deliver(inbound))
        XCTAssertTrue(transport.deliver(inbound))
        let cloudCommandID = LinuxSHA256.hex(Data("cloud:viewer-w54:77".utf8))
        try await waitUntilDynamic({
            "serialized cloud ingress did not execute; diagnostics=\(relayDiagnostics.snapshot())"
        }) {
            lifecycle.calls.filter { $0 == "create:\(cloudCommandID)" }.count == 1
        }

        // The gate is read after durable reservation and releases the row before any effect.
        gate.set(false)
        let send = LinuxIngressRequest(
            operation: .send, commandID: "ignored", taskID: "task-w54",
            sessionID: lifecycle.sessionID, text: "must-not-run")
        transport.deliver(CloudInboundCommand(
            channel: "ctl/machine-w54", sequence: 78, timestamp: timestamp,
            sender: "viewer-w54", plaintext: try JSONEncoder().encode(send)))
        try await Task.sleep(nanoseconds: 30_000_000)
        XCTAssertFalse(lifecycle.calls.contains { $0.contains("must-not-run") })
        XCTAssertNil(ingressStore.load().state?.commands.first {
            $0.id == LinuxSHA256.hex(Data("cloud:viewer-w54:78".utf8))
        })

        let refusedTask = "task-w54-refused"
        let taskCreate = LinuxIngressRequest(
            operation: .taskCreate, commandID: "ignored", taskID: refusedTask,
            projectRoot: project.path, assistant: .codex, taskSecret: "refused-secret",
            title: "Must not exist", claims: [])
        transport.deliver(CloudInboundCommand(
            channel: "ctl/machine-w54", sequence: 79, timestamp: timestamp,
            sender: "viewer-w54", plaintext: try JSONEncoder().encode(taskCreate)))
        try await Task.sleep(nanoseconds: 30_000_000)
        XCTAssertFalse(ingressStore.load().state?.tasks.contains {
            $0.id == refusedTask
        } ?? true, "preflight refusal must not create durable task authority")
        XCTAssertFalse(FileManager.default.fileExists(
            atPath: ingressStore.tasksDirectory + "/" + refusedTask),
            "preflight refusal must happen before creating a task artifact directory")

        gate.set(true)
        _ = try authority.revokeDevice(
            "viewer-w54", expectedGeneration: authority.snapshot().identityGeneration)
        transport.deliver(CloudInboundCommand(
            channel: "ctl/machine-w54", sequence: 80, timestamp: timestamp,
            sender: "viewer-w54", plaintext: try JSONEncoder().encode(send)))
        try await Task.sleep(nanoseconds: 30_000_000)
        XCTAssertFalse(lifecycle.calls.contains { $0.contains("must-not-run") },
                       "effect-time roster revocation must fail before the terminal effect")

        try secretStore.set(Data("not-canonical".utf8),
                            for: CloudExecutorIdentityAuthority.protectedAccount)
        transport.ready(4)
        try await waitUntil("protected identity mismatch did not complete terminal shutdown") {
            relayStatus.state == .unauthorized
        }
        XCTAssertEqual(transport.shutdowns(), 1,
                       "identity mismatch must finish transport shutdown without self-joining")

        await relay.stop()
        let stoppedState = await relay.state
        XCTAssertEqual(stoppedState, .stopped)
        XCTAssertEqual(transport.shutdowns(), 1)
        do {
            try await relay.publish(Data("late".utf8), channel: "s/machine-w54/late",
                                    logicalID: "late")
            XCTFail("stopped Relay owner accepted a publication")
        } catch {
            XCTAssertEqual(error as? CloudDurableOutboundError, .stopped)
        }
    }

    func testLinuxRelayPublishesBoundedDiscoveryAndAdaptsClosedBrowserCommands() async throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-linux-discovery-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: scratch) }
        let project = scratch.appendingPathComponent("reaver", isDirectory: true)
        try FileManager.default.createDirectory(
            at: project, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        let secrets = TestMemorySecretStore()
        let authority = CloudExecutorIdentityAuthority(store: secrets)
        let machineKey = try CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 0x61, count: 32))
        let viewerKey = try CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 0x62, count: 32))
        let master = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x63, count: 32))
        let identity = try authority.provision(
            accountID: "account-discovery", machineID: "machine-linux",
            deviceKey: machineKey, masterSecret: master,
            importedPairedDevices: [CloudExecutorPairedDevice(
                deviceID: "viewer-linux", signingKey: viewerKey.publicKeyRaw,
                fingerprint: viewerKey.pairingFingerprint, pairedAtMilliseconds: 1,
                identityGeneration: 1,
                capabilities: CloudExecutorIdentityAuthority.defaultCapabilities)])
        let durable = try CloudDurableRuntime.open(
            directory: scratch.appendingPathComponent("cloud"), runtime: nil,
            strictPersistedFrameValidation: true)
        let transport = LinuxRelayTestTransport()
        let outbound = CloudDurableOutboundComposition(
            spool: durable.spool, transport: transport,
            identity: CloudAppIdentity(
                machineID: identity.machineID, deviceID: identity.deviceID,
                keyID: identity.keyID, masterSecret: master, signingKey: machineKey),
            nowMilliseconds: { UInt64(Date().timeIntervalSince1970 * 1_000) })
        let ingressStore = try LinuxDurableStateStore(
            stateDirectory: scratch.appendingPathComponent("daemon").path)
        let lifecycle = FakeLinuxLifecycleRuntime()
        let startup = try LinuxStartupReconciler.reconcile(
            store: ingressStore, inventory: .complete([]))
        let ingress = LinuxDaemonIngressOwner(store: ingressStore, runtime: lifecycle)
        ingress.completeStartup(startup)
        let gate = LockedBool(true)
        let row = TargetSession(
            backend: .tmux, id: "%7", name: "Linux work", tty: "/dev/pts/7",
            windowIndex: 0, tabIndex: 0, assistant: .codex, cwd: project.path)
        let relay = LinuxRelayRuntimeOwner(
            machine: CloudMachineIdentity(accountID: identity.accountID,
                                          machineID: identity.machineID),
            identityAuthority: authority, transport: transport, outbound: outbound,
            ingress: ingress, commandsEnabled: { gate.get() },
            presentation: LinuxRelayMachinePresentation(
                displayName: "AWS worker", provider: "aws"),
            places: [LinuxRelayPlace(id: "reaver", label: "reaver", path: project.path)],
            inventory: { TerminalInventory(sessions: [row]) })
        try await relay.start()
        var initialFrames: [CloudPublishFrame] = []
        for wanted in 1...3 {
            for _ in 0..<200 where transport.writtenFrames().count < wanted {
                try await Task.sleep(nanoseconds: 5_000_000)
            }
            let frame = try JSONDecoder().decode(
                CloudPublishFrame.self, from: transport.writtenFrames()[wanted - 1])
            initialFrames.append(frame)
            transport.receipt(CloudOutboundTransportReceipt(
                channel: frame.envelope.ch, sequence: Int64(frame.envelope.seq), kind: .delivered))
        }
        XCTAssertEqual(initialFrames.count, 3)
        func plaintexts(_ frames: [CloudPublishFrame]) throws -> [[String: Any]] {
            try frames.map { frame in
                let envelope = frame.envelope
                let clear = try envelope.open(
                    masterSecret: master,
                    publicKeyForSender: { $0 == identity.machineID ? machineKey.publicKeyRaw : nil })
                return try XCTUnwrap(JSONSerialization.jsonObject(with: clear) as? [String: Any])
            }
        }
        let initial = try plaintexts(initialFrames)
        XCTAssertTrue(initial.contains { $0["platform"] as? String == "linux"
            && $0["provider"] as? String == "aws" && $0["label"] as? String == "AWS worker" })
        XCTAssertTrue(initial.contains {
            ($0["session"] as? [String: Any])?["cwd"] as? String == project.path
        })
        XCTAssertTrue(initial.contains {
            (($0["inventory"] as? [String: Any])?["sessions"] as? [String]) == ["%7"]
        })

        let now = UInt64(Date().timeIntervalSince1970 * 1_000)
        let places = try JSONSerialization.data(withJSONObject: [
            "type": "places", "session": "__clawdline_machine__", "request": "places-1"
        ])
        XCTAssertTrue(transport.deliver(CloudInboundCommand(
            channel: "ctl/machine-linux", sequence: 9, timestamp: now,
            sender: "viewer-linux", plaintext: places)))
        for _ in 0..<200 where transport.writtenFrames().count < 4 {
            try await Task.sleep(nanoseconds: 5_000_000)
        }
        let placesFrame = try JSONDecoder().decode(
            CloudPublishFrame.self, from: transport.writtenFrames()[3])
        let placesAnswer = try XCTUnwrap(try plaintexts([placesFrame]).first)
        XCTAssertEqual(placesAnswer["read"] as? String, "read:places-1")
        transport.receipt(CloudOutboundTransportReceipt(
            channel: placesFrame.envelope.ch, sequence: Int64(placesFrame.envelope.seq),
            kind: .delivered))
        let start = try JSONSerialization.data(withJSONObject: [
            "type": "start", "session": "__clawdline_machine__", "request": "start-1",
            "place": "reaver", "assistant": "codex", "model": ""
        ])
        XCTAssertTrue(transport.deliver(CloudInboundCommand(
            channel: "ctl/machine-linux", sequence: 10, timestamp: now,
            sender: "viewer-linux", plaintext: start)))
        for _ in 0..<200 where lifecycle.calls.filter({ $0.hasPrefix("create:") }).isEmpty {
            try await Task.sleep(nanoseconds: 5_000_000)
        }
        XCTAssertEqual(lifecycle.calls.filter { $0.hasPrefix("create:") }.count, 1)
        let before = lifecycle.calls.count
        let arbitrary = try JSONSerialization.data(withJSONObject: [
            "type": "start", "session": "__clawdline_machine__", "request": "start-2",
            "place": "/etc", "assistant": "codex", "model": ""
        ])
        XCTAssertTrue(transport.deliver(CloudInboundCommand(
            channel: "ctl/machine-linux", sequence: 11, timestamp: now,
            sender: "viewer-linux", plaintext: arbitrary)))
        try await Task.sleep(nanoseconds: 30_000_000)
        XCTAssertEqual(lifecycle.calls.count, before, "the browser cannot supply an arbitrary path")
        await relay.stop()
    }

    func testW52ProtectedExecutorIdentityPairingRotationAndReconnect() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-w52-identity-\(UUID().uuidString)")
        let secretURL = scratch.appendingPathComponent("secrets", isDirectory: true)
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(
            at: secretURL, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        var metadata = stat()
        XCTAssertEqual(lstat(secretURL.path, &metadata), 0)
        let secrets = LinuxProtectedFileSecretStore(root: CanonicalProjectRoot(
            path: secretURL.path, ownerUID: metadata.st_uid, ownerGID: metadata.st_gid))
        let authority = CloudExecutorIdentityAuthority(store: secrets)
        XCTAssertEqual(authority.readiness(), .blocked(.protectedStateMissing))
        XCTAssertEqual(
            CloudExecutorIdentityAuthority(store: DeniedSecretStore()).readiness(),
            .blocked(.protectedStateUnavailable))

        let machineKey = try CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 0x17, count: 32))
        let master = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x29, count: 32))
        let initial = try authority.provision(
            accountID: "account-w52", machineID: "machine-w52",
            deviceKey: machineKey, masterSecret: master)
        XCTAssertEqual(initial.identityGeneration, 1)
        XCTAssertEqual(initial.keyEpoch, 1)
        XCTAssertEqual(initial.machineFingerprint, machineKey.pairingFingerprint)
        let memory = CloudExecutorIdentityAuthority(store: TestMemorySecretStore())
        XCTAssertEqual(
            try memory.provision(accountID: "account-w52", machineID: "machine-w52",
                                 deviceKey: machineKey, masterSecret: master),
            initial, "Mac/Linux protected adapters expose one Application identity contract")

        let viewerSigning = try CloudDeviceKeyPair(
            privateKeyRaw: Data(repeating: 0x35, count: 32))
        let viewerEphemeralPrivate = Data(repeating: 0x41, count: 32)
        let viewerEphemeral = try CloudPairing.x25519PublicKey(
            privateKeyRaw: viewerEphemeralPrivate)
        let claimNonce = Data(repeating: 0x53, count: 32)
        let pairingNonce = Data(repeating: 0x67, count: 32)
        func offer(_ pairingID: String, deviceID: String = "viewer-w52") -> Data {
            CloudCanonicalJSON.canonicalData(.object([
                "v": .int(1), "type": .string("pairing_offer"),
                "pairing_id": .string(pairingID),
                "claim_nonce": .base64(claimNonce),
                "pairing_nonce": .base64(pairingNonce),
                "account_id": .string("account-w52"),
                "viewer_device_id": .string(deviceID),
                "viewer_signing_key": .base64(viewerSigning.publicKeyRaw),
                "viewer_ephemeral_key": .base64(viewerEphemeral),
                "viewer_fingerprint": .string(viewerSigning.pairingFingerprint),
                "expires_at": .int(101_000),
            ]))
        }
        let offerBytes = offer("pairing-w52")
        let prepared = try authority.prepareHandover(
            offerBytes: offerBytes, nowMilliseconds: 100_000,
            randomBytes: { count in Data(repeating: count == 12 ? 0x79 : 0x73, count: count) })
        let retry = try CloudExecutorIdentityAuthority(store: secrets).prepareHandover(
            offerBytes: offerBytes, nowMilliseconds: 100_100,
            randomBytes: { count in Data(repeating: 0xff, count: count) })
        XCTAssertEqual(retry.wrapperBytes, prepared.wrapperBytes)
        XCTAssertFalse(retry.alreadyCommitted)
        XCTAssertThrowsError(try authority.prepareHandover(
            offerBytes: offer("second-claimant"), nowMilliseconds: 100_100)) {
            XCTAssertEqual($0 as? CloudExecutorIdentityError, .pairingClaimed)
        }

        let wrapper = try CloudPairing.decodeWrapper(prepared.wrapperBytes)
        let shared = try CloudPairing.x25519SharedSecret(
            privateKeyRaw: viewerEphemeralPrivate,
            peerPublicKeyRaw: try CloudPairing.decodeCanonicalBase64(
                wrapper.ephemeralKey, field: "ephemeral_key", expectedLength: 32))
        let derived = try CloudPairing.derive(
            sharedSecretRaw: shared, pairingNonce: pairingNonce,
            pairingID: "pairing-w52", claimNonce: claimNonce, phase: .grant)
        let clear = try CloudPairing.open(
            wrapper, phaseKey: derived.phaseKey,
            viewerEphemeralKey: viewerEphemeral.base64EncodedString(),
            machineEphemeralKey: wrapper.ephemeralKey)
        guard case .object(let handover) = try CloudCanonicalJSON.parseStrict(clear) else {
            return XCTFail("handover must be canonical JSON")
        }
        XCTAssertEqual(handover["account_id"], .string("account-w52"))
        XCTAssertEqual(handover["machine_id"], .string("machine-w52"))
        XCTAssertEqual(handover.count, 8, "W0-E keeps the accepted v1 handover wire unchanged")
        XCTAssertEqual(initial.keyEpoch, 1)
        XCTAssertEqual(initial.revocationEpoch, 0)
        XCTAssertThrowsError(try authority.commitPreparedHandover(
            pairingID: "pairing-w52", claimNonce: claimNonce.base64EncodedString(),
            deliveredFingerprint: "wrong", nowMilliseconds: 100_200)) {
            XCTAssertEqual($0 as? CloudExecutorIdentityError, .pairingFingerprintMismatch)
        }
        XCTAssertThrowsError(try authority.commitPreparedHandover(
            pairingID: "pairing-w52", claimNonce: claimNonce.base64EncodedString(),
            deliveredFingerprint: viewerSigning.pairingFingerprint,
            nowMilliseconds: 101_001)) {
            XCTAssertEqual($0 as? CloudExecutorIdentityError, .pairingExpired)
        }
        let paired = try authority.commitPreparedHandover(
            pairingID: "pairing-w52", claimNonce: claimNonce.base64EncodedString(),
            deliveredFingerprint: viewerSigning.pairingFingerprint,
            nowMilliseconds: 100_200)
        XCTAssertEqual(paired.pairedDevices.map(\.deviceID), ["viewer-w52"])
        let completedRetry = try CloudExecutorIdentityAuthority(store: secrets).prepareHandover(
            offerBytes: offerBytes, nowMilliseconds: 100_300)
        XCTAssertTrue(completedRetry.alreadyCommitted)
        XCTAssertEqual(completedRetry.wrapperBytes, prepared.wrapperBytes)

        let revoked = try authority.revokeDevice(
            "viewer-w52", expectedGeneration: paired.identityGeneration)
        XCTAssertEqual(revoked.revokedDeviceIDs, ["viewer-w52"])
        XCTAssertTrue(try authority.transportMaterial().pairedDevicePublicKeys.isEmpty)
        XCTAssertThrowsError(try authority.prepareHandover(
            offerBytes: offerBytes, nowMilliseconds: 100_400)) {
            XCTAssertEqual($0 as? CloudExecutorIdentityError, .revokedIdentity)
        }
        let replacementKey = try CloudDeviceKeyPair(
            privateKeyRaw: Data(repeating: 0x7f, count: 32))
        let replacementMaster = try CloudMasterSecret(
            rawRepresentation: Data(repeating: 0x83, count: 32))
        let transition = try authority.rotateKeys(
            deviceKey: replacementKey, masterSecret: replacementMaster,
            keyID: "master-v2", expectedGeneration: revoked.identityGeneration)
        XCTAssertEqual(transition.keyEpoch, 2)
        XCTAssertEqual(try authority.transportMaterial().deviceKey, replacementKey)
        let current = try authority.snapshot()
        let proof = CloudExecutorReconnectProof(
            accountID: current.accountID, machineID: current.machineID,
            deviceID: current.deviceID, identityGeneration: current.identityGeneration,
            keyEpoch: current.keyEpoch, revocationEpoch: current.revocationEpoch,
            durableLedgerOpened: true, durableSpoolOpened: true)
        XCTAssertEqual(try authority.verifyReconnect(proof), .resumeFromDurableLedgerAndSpool)
        let stale = CloudExecutorReconnectProof(
            accountID: current.accountID, machineID: current.machineID,
            deviceID: current.deviceID, identityGeneration: revoked.identityGeneration,
            keyEpoch: 1, revocationEpoch: current.revocationEpoch,
            durableLedgerOpened: true, durableSpoolOpened: true)
        XCTAssertThrowsError(try authority.verifyReconnect(stale)) {
            XCTAssertEqual($0 as? CloudExecutorIdentityError, .invalidReconnectProof)
        }
        let noSpool = CloudExecutorReconnectProof(
            accountID: current.accountID, machineID: current.machineID,
            deviceID: current.deviceID, identityGeneration: current.identityGeneration,
            keyEpoch: current.keyEpoch, revocationEpoch: current.revocationEpoch,
            durableLedgerOpened: true, durableSpoolOpened: false)
        XCTAssertThrowsError(try authority.verifyReconnect(noSpool)) {
            XCTAssertEqual($0 as? CloudExecutorIdentityError, .durableResumeUnavailable)
        }

        let protectedLeaf = secretURL.appendingPathComponent("executor-identity-v1.secret")
        let goodBytes = try XCTUnwrap(secrets.data(
            for: CloudExecutorIdentityAuthority.protectedAccount))
        XCTAssertLessThanOrEqual(
            goodBytes.count, CloudExecutorIdentityAuthority.maximumProtectedStateBytes)
        try secrets.set(Data("{}".utf8), for: CloudExecutorIdentityAuthority.protectedAccount)
        XCTAssertEqual(authority.readiness(), .blocked(.protectedStateCorrupt))
        try Data(repeating: 0x78,
                 count: CloudExecutorIdentityAuthority.maximumProtectedStateBytes + 1)
            .write(to: protectedLeaf)
        XCTAssertEqual(authority.readiness(), .blocked(.protectedStateUnavailable))
        guard case .object(var future) = try CloudCanonicalJSON.parseStrict(goodBytes) else {
            return XCTFail("stored identity must be an object")
        }
        future["v"] = .int(2)
        try secrets.set(CloudCanonicalJSON.canonicalData(.object(future)),
                        for: CloudExecutorIdentityAuthority.protectedAccount)
        XCTAssertEqual(authority.readiness(), .blocked(.protectedStateFutureVersion(2)))
        try secrets.set(goodBytes, for: CloudExecutorIdentityAuthority.protectedAccount)
        XCTAssertThrowsError(try authority.provision(
            accountID: "wrong-owner", machineID: "machine-w52",
            deviceKey: replacementKey, masterSecret: replacementMaster)) {
            XCTAssertEqual($0 as? CloudExecutorIdentityError, .identityMismatch)
        }
        let linked = scratch.appendingPathComponent("identity-hardlink")
        try FileManager.default.linkItem(
            at: secretURL.appendingPathComponent("executor-identity-v1.secret"), to: linked)
        XCTAssertEqual(authority.readiness(), .blocked(.protectedStateUnavailable))
        XCTAssertThrowsError(try secrets.set(
            Data("replacement-must-not-land".utf8),
            for: CloudExecutorIdentityAuthority.protectedAccount))
        XCTAssertThrowsError(try secrets.remove(CloudExecutorIdentityAuthority.protectedAccount))
        XCTAssertEqual(try Data(contentsOf: linked), goodBytes,
                       "write/remove refuse a multiply linked protected leaf without changing it")
        try FileManager.default.removeItem(at: linked)
        XCTAssertEqual(authority.readiness(), .ready(current))

        try FileManager.default.setAttributes(
            [.posixPermissions: 0o640], ofItemAtPath: protectedLeaf.path)
        XCTAssertEqual(authority.readiness(), .blocked(.protectedStateUnavailable))
        XCTAssertThrowsError(try secrets.set(
            Data("mode-must-not-land".utf8),
            for: CloudExecutorIdentityAuthority.protectedAccount))
        XCTAssertThrowsError(try secrets.remove(CloudExecutorIdentityAuthority.protectedAccount))
        XCTAssertEqual(try Data(contentsOf: protectedLeaf), goodBytes)
        try FileManager.default.setAttributes(
            [.posixPermissions: 0o600], ofItemAtPath: protectedLeaf.path)

        try FileManager.default.setAttributes(
            [.posixPermissions: 0o750], ofItemAtPath: secretURL.path)
        XCTAssertEqual(authority.readiness(), .blocked(.protectedStateUnavailable))
        XCTAssertThrowsError(try secrets.set(
            Data("directory-mode-must-not-land".utf8),
            for: CloudExecutorIdentityAuthority.protectedAccount))
        XCTAssertThrowsError(try secrets.remove(CloudExecutorIdentityAuthority.protectedAccount))
        XCTAssertEqual(try Data(contentsOf: protectedLeaf), goodBytes)
        try FileManager.default.setAttributes(
            [.posixPermissions: 0o700], ofItemAtPath: secretURL.path)
        XCTAssertEqual(authority.readiness(), .ready(current))

        let wrongOwnerStore = LinuxProtectedFileSecretStore(root: CanonicalProjectRoot(
            path: secretURL.path, ownerUID: .max, ownerGID: metadata.st_gid))
        XCTAssertThrowsError(try wrongOwnerStore.data(
            for: CloudExecutorIdentityAuthority.protectedAccount))
        XCTAssertThrowsError(try wrongOwnerStore.set(
            Data("owner-must-not-land".utf8),
            for: CloudExecutorIdentityAuthority.protectedAccount))
        XCTAssertThrowsError(try wrongOwnerStore.remove(
            CloudExecutorIdentityAuthority.protectedAccount))
        XCTAssertEqual(try Data(contentsOf: protectedLeaf), goodBytes)

        let savedLeaf = secretURL.appendingPathComponent("identity-saved-for-type-check")
        try FileManager.default.moveItem(at: protectedLeaf, to: savedLeaf)
        try FileManager.default.createDirectory(
            at: protectedLeaf, withIntermediateDirectories: false,
            attributes: [.posixPermissions: 0o700])
        XCTAssertEqual(authority.readiness(), .blocked(.protectedStateUnavailable))
        XCTAssertThrowsError(try secrets.set(
            Data("type-must-not-land".utf8),
            for: CloudExecutorIdentityAuthority.protectedAccount))
        XCTAssertThrowsError(try secrets.remove(
            CloudExecutorIdentityAuthority.protectedAccount))
        try FileManager.default.removeItem(at: protectedLeaf)
        try FileManager.default.moveItem(at: savedLeaf, to: protectedLeaf)
        XCTAssertEqual(try Data(contentsOf: protectedLeaf), goodBytes,
                       "non-regular protected leaf is refused without replacing saved bytes")
        XCTAssertFalse(String(describing: try authority.transportMaterial())
            .contains(replacementMaster.rawRepresentation.base64EncodedString()))
    }

    #if os(Linux)
    func testRealTmuxProviderLifecycleOnLinux() throws {
        let tmux = try XCTUnwrap(ProcessInfo.processInfo.environment["CLAWDLINE_TEST_TMUX"],
                                 "Linux contract requires its pinned tmux executable")
        let linuxExecutable = try XCTUnwrap(
            ProcessInfo.processInfo.environment["CLAWDLINE_TEST_LINUX_EXECUTABLE"],
            "Linux contract requires the exact sandbox launcher executable")
        XCTAssertNotEqual(geteuid(), 0, "the provider lifecycle contract must run non-root")
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-runtime-\(UUID().uuidString)")
        let state = scratch.appendingPathComponent("state")
        let project = scratch.appendingPathComponent("project")
        let provider = scratch.appendingPathComponent("claude-fixture")
        let outside = scratch.appendingPathComponent("outside-provider-write")
        let machineSecret = state.appendingPathComponent("secrets/machine.secret")
        let controlSocket = state.appendingPathComponent("runtime/clawdline.sock")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(at: project, withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o700])
        let quotedSecret = SessionLaunchPolicy.shellQuoted(machineSecret.path)
        let quotedSocket = SessionLaunchPolicy.shellQuoted(controlSocket.path)
        let quotedOutside = SessionLaunchPolicy.shellQuoted(outside.path)
        let script = """
        #!/bin/sh
        if /bin/cat \(quotedSecret) >/dev/null 2>&1; then
          printf 'SECRET-READABLE\\n'
        else
          printf 'SECRET-DENIED\\n'
        fi
        if /usr/bin/python3 -c "import socket; s=socket.socket(socket.AF_UNIX); s.connect(\(quotedSocket))" >/dev/null 2>&1; then
          printf 'SOCKET-CONNECTED\\n'
        else
          printf 'SOCKET-DENIED\\n'
        fi
        if printf escaped > \(quotedOutside) 2>/dev/null; then
          printf 'OUTSIDE-WRITABLE\\n'
        else
          printf 'OUTSIDE-DENIED\\n'
        fi
        printf 'READY\\n'
        while IFS= read -r line; do
          if [ "$line" = /exit ]; then exit 0; fi
          printf 'ECHO:%s\\n' "$line"
        done
        """
        try Data(script.utf8).write(to: provider)
        try FileManager.default.setAttributes([.posixPermissions: 0o700],
                                              ofItemAtPath: provider.path)
        let runtimeConfig = LinuxRuntimeConfiguration(
            uid: geteuid(), gid: getegid(), projectRoots: [project.path],
            tmuxExecutable: tmux,
            providers: LinuxProviderExecutablesConfiguration(
                claude: provider.path, codex: provider.path))
        let config = LinuxDaemonConfiguration(
            version: 1, listen: LinuxListenConfiguration(host: "127.0.0.1", port: 7718),
            stateDirectory: state.path,
            secretFile: scratch.appendingPathComponent("unused.secret").path,
            runtime: runtimeConfig)
        let runtime = try LinuxProviderRuntime.compose(
            configuration: config, sandboxExecutablePath: linuxExecutable)
        XCTAssertEqual(runtime.compositionReceipt.configuration,
                       "runtime_adapters_configured_provider_auth_pending")
        XCTAssertFalse(runtime.compositionReceipt.identity.ready)
        XCTAssertTrue(runtime.compositionReceipt.identity.providers.allSatisfy {
            $0.executableConfigured && !$0.authenticated && !$0.usable && $0.lifecycle.isEmpty
        })
        try Data("machine-secret-must-not-cross".utf8).write(to: machineSecret)
        try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                              ofItemAtPath: machineSecret.path)
        let created = try runtime.create(commandID: "create-1", projectRoot: project.path,
                                         assistant: .claude)
        let pane = try XCTUnwrap(created.sessionID)
        XCTAssertEqual(created.progress.stages, [.accepted, .executed, .delivered, .observed])
        var observed = ""
        for attempt in 0..<40 where !observed.contains("READY") {
            usleep(50_000)
            observed = try runtime.observe(commandID: "observe-\(attempt)", sessionID: pane).output ?? ""
        }
        XCTAssertTrue(observed.contains("SECRET-DENIED"))
        XCTAssertTrue(observed.contains("SOCKET-DENIED"))
        XCTAssertTrue(observed.contains("OUTSIDE-DENIED"))
        XCTAssertFalse(FileManager.default.fileExists(atPath: outside.path))

        _ = try runtime.send(commandID: "send-1", sessionID: pane, text: "hello")
        for attempt in 40..<80 where !observed.contains("ECHO:hello") {
            usleep(50_000)
            observed = try runtime.observe(commandID: "observe-\(attempt)", sessionID: pane).output ?? ""
        }
        XCTAssertTrue(observed.contains("ECHO:hello"))

        runtime.terminal.failSubmitAfterPasteForTesting = true
        XCTAssertThrowsError(try runtime.send(
            commandID: "partial-send", sessionID: pane, text: "partial")) {
            let receipt = $0 as? LinuxLifecycleFailure
            XCTAssertEqual(receipt?.certainty, .partial)
            XCTAssertEqual(receipt?.lastConfirmedEffect, .textPasted)
            XCTAssertEqual(receipt?.progress.stages, [.accepted, .executed, .delivered])
            XCTAssertEqual(receipt?.reconciliationRequired, true)
        }
        runtime.terminal.failSubmitAfterPasteForTesting = false
        _ = try runtime.interrupt(commandID: "clear-partial", sessionID: pane)

        runtime.failPostCreateReadinessForTesting = true
        XCTAssertThrowsError(try runtime.create(
            commandID: "post-create-failure", projectRoot: project.path, assistant: .claude)) {
            let receipt = $0 as? LinuxLifecycleFailure
            XCTAssertEqual(receipt?.certainty, .compensated)
            XCTAssertEqual(receipt?.lastConfirmedEffect, .sessionCreated)
            XCTAssertEqual(receipt?.reconciliationRequired, false)
        }
        runtime.failPostCreateReadinessForTesting = false

        let resized = try runtime.resize(commandID: "resize-1", sessionID: pane,
                                         columns: 90, rows: 25)
        XCTAssertEqual(resized.output, "90x25")
        let enumerated = try runtime.enumerate(commandID: "enumerate-1")
        XCTAssertTrue(enumerated.1.contains(where: { $0.id == pane }))
        let closed = try runtime.close(commandID: "close-1", sessionID: pane)
        XCTAssertEqual(closed.progress.stages, [.accepted, .executed, .delivered, .observed])
        XCTAssertFalse(try runtime.enumerate(commandID: "enumerate-2").1
            .contains(where: { $0.id == pane }))
    }

    func testComposeRefusesReservedRootsAndMissingExecutablesBeforeEffect() throws {
        let tmux = try XCTUnwrap(ProcessInfo.processInfo.environment["CLAWDLINE_TEST_TMUX"])
        let linuxExecutable = try XCTUnwrap(
            ProcessInfo.processInfo.environment["CLAWDLINE_TEST_LINUX_EXECUTABLE"])
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-capability-\(UUID().uuidString)")
        let project = scratch.appendingPathComponent("project")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(at: project, withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o700])
        func configuration(state: URL, roots: [String], provider: String) -> LinuxDaemonConfiguration {
            LinuxDaemonConfiguration(
                version: 1, listen: .init(host: "127.0.0.1", port: 7718),
                stateDirectory: state.path,
                secretFile: scratch.appendingPathComponent("unused.secret").path,
                runtime: LinuxRuntimeConfiguration(
                    uid: geteuid(), gid: getegid(), projectRoots: roots,
                    tmuxExecutable: tmux,
                    providers: .init(claude: provider, codex: provider)))
        }
        let firstState = scratch.appendingPathComponent("first-state")
        XCTAssertThrowsError(try LinuxProviderRuntime.compose(
            configuration: configuration(
                state: firstState, roots: [firstState.appendingPathComponent("secrets").path],
                provider: linuxExecutable),
            sandboxExecutablePath: linuxExecutable)) {
            XCTAssertEqual(($0 as? LinuxRuntimeFailure)?.code, .unsafePath)
        }
        let secondState = scratch.appendingPathComponent("second-state")
        XCTAssertThrowsError(try LinuxProviderRuntime.compose(
            configuration: configuration(
                state: secondState, roots: [project.path],
                provider: scratch.appendingPathComponent("missing-provider").path),
            sandboxExecutablePath: linuxExecutable)) {
            XCTAssertEqual(($0 as? LinuxRuntimeFailure)?.code, .capabilityUnavailable)
        }
    }
    #endif

    func testW42StartupReconciliationPreservesTerminalQueueAndAcknowledgedTruth() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-reconcile-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(
            at: scratch, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        let store = try LinuxDurableStateStore(stateDirectory: scratch.path)
        let sealed = Data("recoverable-command".utf8)
        try store.save(LinuxDurableState(
            terminals: [
                .init(id: "%present", taskID: "working", state: .present,
                      lastObservedAt: nil, evidenceDigest: nil),
                .init(id: "%gone", taskID: "done", state: .present,
                      lastObservedAt: nil, evidenceDigest: "terminal-evidence"),
            ],
            tasks: [
                .init(id: "done", terminalID: "%gone", state: .complete,
                      resultDigest: "result-digest", acknowledgedEvidence: ["ack-digest"]),
                .init(id: "working", terminalID: "%present", state: .working,
                      resultDigest: nil, acknowledgedEvidence: []),
                .init(id: "queued-safe", terminalID: nil, state: .queued,
                      resultDigest: nil, acknowledgedEvidence: []),
                .init(id: "queued-unsafe", terminalID: nil, state: .queued,
                      resultDigest: nil, acknowledgedEvidence: []),
            ],
            queue: [
                .init(id: "q1", taskID: "queued-safe", commandID: "c-safe",
                      state: .queued, payloadDigest: LinuxSHA256.hex(sealed),
                      sealedPayloadRecoverable: true,
                      sealedPayloadBase64: sealed.base64EncodedString()),
                .init(id: "q2", taskID: "queued-unsafe", commandID: "c-unsafe",
                      state: .queued, payloadDigest: "missing-seal",
                      sealedPayloadRecoverable: false),
            ],
            commands: [
                .init(id: "ack", taskID: "done", terminalID: "%gone",
                      operation: "send", stage: .acknowledged, outcome: .succeeded,
                      evidenceDigest: "ack-digest", acknowledgedAt: "2026-09-12T00:00:00Z"),
                .init(id: "accepted", taskID: "queued-safe", terminalID: nil,
                      operation: "send", stage: .accepted, outcome: .pending,
                      evidenceDigest: nil, acknowledgedAt: nil),
                .init(id: "delivered", taskID: "working", terminalID: "%present",
                      operation: "send", stage: .delivered, outcome: .pending,
                      evidenceDigest: nil, acknowledgedAt: nil),
                .init(id: "c-safe", taskID: "queued-safe", terminalID: nil,
                      operation: "send", stage: .accepted, outcome: .pending,
                      evidenceDigest: nil, acknowledgedAt: nil),
                .init(id: "c-unsafe", taskID: "queued-unsafe", terminalID: nil,
                      operation: "send", stage: .accepted, outcome: .pending,
                      evidenceDigest: nil, acknowledgedAt: nil),
            ]))

        let receipt = try LinuxStartupReconciler.reconcile(
            store: store, inventory: .complete(["%present", "%discovered"]),
            now: "2026-09-12T01:00:00Z")
        XCTAssertTrue(receipt.authoritative)
        XCTAssertEqual(receipt.status, "complete")
        XCTAssertEqual(receipt.terminalPresent, 2)
        XCTAssertEqual(receipt.terminalMissing, 1)
        XCTAssertEqual(receipt.taskTerminal, 1)
        XCTAssertEqual(receipt.taskReconciling, 1)
        XCTAssertEqual(receipt.taskUnknown, 1)
        XCTAssertEqual(receipt.queueRecoverable, 1)
        XCTAssertEqual(receipt.queueUnknown, 1)
        XCTAssertEqual(receipt.commandSucceeded, 1)
        XCTAssertEqual(receipt.commandInterrupted, 3)
        XCTAssertEqual(receipt.commandUnknown, 1)

        let persisted = try XCTUnwrap(store.load().state)
        XCTAssertEqual(persisted.tasks.first { $0.id == "done" }?.resultDigest,
                       "result-digest")
        XCTAssertEqual(persisted.tasks.first { $0.id == "done" }?.acknowledgedEvidence,
                       ["ack-digest"])
        XCTAssertEqual(persisted.commands.first { $0.id == "ack" }?.outcome, .succeeded)
        XCTAssertEqual(persisted.commands.first { $0.id == "accepted" }?.outcome, .interrupted)
        XCTAssertEqual(persisted.commands.first { $0.id == "delivered" }?.outcome, .unknown)
    }

    func testW42IncompleteInventoryAndPartialStateNeverInventAbsenceOrEmptyAuthority() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-partial-state-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(
            at: scratch, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        let store = try LinuxDurableStateStore(stateDirectory: scratch.path)
        try store.save(LinuxDurableState(terminals: [
            .init(id: "%1", taskID: nil, state: .present,
                  lastObservedAt: nil, evidenceDigest: nil),
        ]))
        let incomplete = try LinuxStartupReconciler.reconcile(
            store: store, inventory: .incomplete("tmux timeout"),
            now: "2026-09-12T02:00:00Z")
        XCTAssertEqual(incomplete.status, "inventory_incomplete")
        XCTAssertEqual(incomplete.terminalUnknown, 1)
        XCTAssertEqual(incomplete.terminalMissing, 0)

        try Data("{\"schemaVersion\":2,\"terminals\":[".utf8)
            .write(to: URL(fileURLWithPath: store.statePath), options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                              ofItemAtPath: store.statePath)
        let corrupt = store.load()
        XCTAssertFalse(corrupt.authoritative)
        XCTAssertNil(corrupt.state)
        XCTAssertEqual(corrupt.disposition, .quarantined)
        let preserved = try XCTUnwrap(corrupt.preservedOriginal)
        XCTAssertTrue(FileManager.default.fileExists(atPath: preserved))
        XCTAssertFalse(FileManager.default.fileExists(atPath: store.statePath))
        let secondTick = try LinuxStartupReconciler.reconcile(
            store: store, inventory: .complete([]), now: "2026-09-12T02:00:10Z")
        XCTAssertFalse(secondTick.authoritative)
        XCTAssertEqual(secondTick.stateDisposition, .recoveryRequired)
        let restartedStore = try LinuxDurableStateStore(stateDirectory: scratch.path)
        XCTAssertEqual(restartedStore.load().disposition, .recoveryRequired)
        XCTAssertThrowsError(try restartedStore.authorizeEmptyRecovery(authorization: "implicit"))
        try restartedStore.authorizeEmptyRecovery(
            authorization: LinuxDurableStateStore.recoveryAuthorization)
        XCTAssertEqual(restartedStore.load().disposition, .initialized)

        // Simulate power loss after the recovery obligation's file+directory fsync but before
        // the canonical corrupt inode is renamed into quarantine. A fresh process must prefer
        // the obligation over ENOENT/initialization logic and preserve the canonical bytes.
        let crashBytes = Data("still-corrupt-after-marker".utf8)
        try crashBytes.write(to: URL(fileURLWithPath: store.statePath), options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                              ofItemAtPath: store.statePath)
        let planned = store.quarantineDirectory + "/runtime-state.planned-crash.json"
        let obligation = """
        {"schemaVersion":1,"preservedOriginal":"\(planned)","reason":"simulated marker-before-rename power loss"}
        """
        try Data(obligation.utf8).write(
            to: URL(fileURLWithPath: store.recoveryObligationPath), options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                              ofItemAtPath: store.recoveryObligationPath)
        let afterPowerLoss = try LinuxDurableStateStore(stateDirectory: scratch.path)
        let blocked = afterPowerLoss.load()
        XCTAssertEqual(blocked.disposition, .recoveryRequired)
        XCTAssertEqual(blocked.preservedOriginal, store.statePath)
        XCTAssertEqual(try Data(contentsOf: URL(fileURLWithPath: store.statePath)), crashBytes)
        try afterPowerLoss.authorizeEmptyRecovery(
            authorization: LinuxDurableStateStore.recoveryAuthorization)
        XCTAssertEqual(afterPowerLoss.load().disposition, .initialized)
    }

    func testW42UnknownStateVersionIsQuarantinedAndVersionOneMigratesAdditively() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-state-migration-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(
            at: scratch, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        let store = try LinuxDurableStateStore(stateDirectory: scratch.path)
        let legacy = """
        {"schemaVersion":1,"minimumReaderVersion":2,"terminals":[{"id":"%legacy","taskID":"legacy-task","state":"present","lastObservedAt":null,"evidenceDigest":"old-evidence"}],"tasks":[{"id":"legacy-task","terminalID":"%legacy","state":"complete","resultDigest":"old-result"},{"id":"queued","terminalID":null,"state":"queued","resultDigest":null}],"queue":[{"id":"legacy-queue","taskID":"queued","commandID":"legacy-command","payloadDigest":"old-payload"}],"commands":[{"id":"legacy-command","taskID":"queued","terminalID":null,"operation":"send","stage":"accepted","outcome":"pending","evidenceDigest":null,"acknowledgedAt":null}]}
        """
        try Data(legacy.utf8).write(to: URL(fileURLWithPath: store.statePath))
        try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                              ofItemAtPath: store.statePath)
        let migrated = try LinuxStartupReconciler.reconcile(
            store: store, inventory: .complete(["%legacy"]),
            now: "2026-09-12T03:00:00Z")
        XCTAssertEqual(migrated.stateDisposition, .migrated)
        let persisted = try XCTUnwrap(store.load().state)
        XCTAssertEqual(persisted.schemaVersion, LinuxDurableState.schemaVersion)
        XCTAssertEqual(persisted.minimumReaderVersion, 2)
        XCTAssertEqual(persisted.terminals.first?.evidenceDigest, "old-evidence")
        XCTAssertEqual(persisted.tasks.first?.resultDigest, "old-result")
        XCTAssertEqual(persisted.tasks.first?.acknowledgedEvidence, [])
        XCTAssertEqual(persisted.queue.first?.state, .unknown)
        XCTAssertFalse(persisted.queue.first?.sealedPayloadRecoverable ?? true)

        let future = "{\"schemaVersion\":999,\"terminals\":[],\"tasks\":[],\"queue\":[],\"commands\":[]}"
        try Data(future.utf8).write(to: URL(fileURLWithPath: store.statePath), options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                              ofItemAtPath: store.statePath)
        let refused = store.load()
        XCTAssertFalse(refused.authoritative)
        XCTAssertEqual(refused.disposition, .quarantined)
        XCTAssertTrue(refused.reason?.contains("schema 999") == true)
        XCTAssertEqual(store.load().disposition, .recoveryRequired)
    }

    func testW42SemanticValidationRejectsDuplicateDanglingAndContradictoryRecords() throws {
        let cases = [
            #"{"schemaVersion":2,"minimumReaderVersion":1,"daemonEpoch":0,"terminals":[{"id":"%1","taskID":null,"state":"present"},{"id":"%1","taskID":null,"state":"missing"}],"tasks":[],"queue":[],"commands":[]}"#,
            #"{"schemaVersion":2,"minimumReaderVersion":1,"daemonEpoch":0,"terminals":[],"tasks":[{"id":"task","terminalID":"%missing","state":"working","resultDigest":null,"acknowledgedEvidence":[]}],"queue":[],"commands":[]}"#,
            #"{"schemaVersion":2,"minimumReaderVersion":1,"daemonEpoch":0,"terminals":[],"tasks":[{"id":"task","terminalID":null,"state":"working","resultDigest":null,"acknowledgedEvidence":[]}],"queue":[],"commands":[{"id":"command","taskID":"task","terminalID":null,"operation":"send","stage":"accepted","outcome":"succeeded","evidenceDigest":"evidence","acknowledgedAt":null}]}"#,
            #"{"schemaVersion":2,"minimumReaderVersion":1,"daemonEpoch":0,"terminals":[],"tasks":[{"id":"task","terminalID":null,"state":"working","resultDigest":null,"acknowledgedEvidence":[]}],"queue":[],"commands":[{"id":"command","taskID":"task","terminalID":null,"operation":"send","stage":"acknowledged","outcome":"pending","evidenceDigest":null,"acknowledgedAt":"now"}]}"#,
            #"{"schemaVersion":2,"minimumReaderVersion":1,"daemonEpoch":0,"terminals":[],"tasks":[{"id":"task","terminalID":null,"state":"queued","resultDigest":null,"acknowledgedEvidence":[]}],"queue":[{"id":"queue","taskID":"task","commandID":"command","state":"queued","payloadDigest":"wrong","sealedPayloadRecoverable":true,"sealedPayloadBase64":"c2VhbGVk"}],"commands":[{"id":"command","taskID":"task","terminalID":null,"operation":"send","stage":"accepted","outcome":"pending","evidenceDigest":null,"acknowledgedAt":null}]}"#,
        ]
        for source in cases {
            let scratch = canonicalTemporaryDirectory()
                .appendingPathComponent("clawdline-invalid-state-\(UUID().uuidString)")
            defer { try? FileManager.default.removeItem(at: scratch) }
            try FileManager.default.createDirectory(at: scratch, withIntermediateDirectories: true,
                                                    attributes: [.posixPermissions: 0o700])
            let store = try LinuxDurableStateStore(stateDirectory: scratch.path)
            try Data(source.utf8).write(to: URL(fileURLWithPath: store.statePath))
            try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                                  ofItemAtPath: store.statePath)
            let refused = store.load()
            XCTAssertFalse(refused.authoritative)
            XCTAssertEqual(refused.disposition, .quarantined)
            XCTAssertEqual(store.load().disposition, .recoveryRequired)
        }
        XCTAssertEqual(LinuxSHA256.hex(Data("abc".utf8)),
                       "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
    }

    func testW42PeriodicObservationDoesNotReplayStartupTransition() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-epoch-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(at: scratch, withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o700])
        let store = try LinuxDurableStateStore(stateDirectory: scratch.path)
        try store.save(LinuxDurableState())
        let startup = try LinuxStartupReconciler.reconcile(
            store: store, inventory: .complete([]), now: "2026-09-12T04:00:00Z")
        XCTAssertEqual(startup.daemonEpoch, 1)
        var live = try XCTUnwrap(store.load().state)
        live.tasks.append(.init(id: "live", terminalID: nil, state: .working,
                                resultDigest: nil, acknowledgedEvidence: []))
        live.commands.append(.init(id: "live-command", taskID: "live", terminalID: nil,
                                   operation: "send", stage: .accepted, outcome: .pending,
                                   evidenceDigest: nil, acknowledgedAt: nil))
        try store.save(live)

        let tick = try LinuxStartupReconciler.observe(
            store: store, inventory: .complete([]), now: "2026-09-12T04:00:10Z")
        XCTAssertEqual(tick.daemonEpoch, 1)
        let afterTick = try XCTUnwrap(store.load().state)
        XCTAssertEqual(afterTick.tasks.first?.state, .working)
        XCTAssertEqual(afterTick.commands.first?.outcome, .pending)

        let restarted = try LinuxStartupReconciler.reconcile(
            store: store, inventory: .complete([]), now: "2026-09-12T04:01:00Z")
        XCTAssertEqual(restarted.daemonEpoch, 2)
        let afterRestart = try XCTUnwrap(store.load().state)
        XCTAssertEqual(afterRestart.tasks.first?.state, .reconciling)
        XCTAssertEqual(afterRestart.commands.first?.outcome, .interrupted)
    }

    func testW42SerializedIngressSealsBeforeEffectAndPersistsBeforeResponse() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-ledger-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(at: scratch, withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o700])
        let store = try LinuxDurableStateStore(stateDirectory: scratch.path)
        let runtime = FakeLinuxLifecycleRuntime()
        let startup = try LinuxStartupReconciler.reconcile(store: store, inventory: .complete([]))
        let owner = LinuxDaemonIngressOwner(store: store, runtime: runtime)
        owner.completeStartup(startup)
        let create = LinuxIngressRequest(
            operation: .create, commandID: "create", taskID: "task",
            projectRoot: "/srv/project", assistant: .claude)
        _ = try owner.perform(create)
        XCTAssertEqual(runtime.calls, ["create:create"])

        var injected = false
        owner.faultInjection = { point in
            if point == .afterEffectBeforeReceipt, !injected {
                injected = true
                throw LinuxIngressFaultPoint.afterEffectBeforeReceipt
            }
        }
        let send = LinuxIngressRequest(
            operation: .send, commandID: "send", taskID: "task",
            sessionID: runtime.sessionID, text: "continue")
        XCTAssertThrowsError(try owner.perform(send))
        var interrupted = try XCTUnwrap(store.load().state)
        XCTAssertEqual(interrupted.commands.first { $0.id == "send" }?.stage, .accepted)
        XCTAssertEqual(interrupted.queue.first { $0.commandID == "send" }?.state, .queued)

        let restart = try LinuxStartupReconciler.reconcile(
            store: store, inventory: .complete([runtime.sessionID]))
        XCTAssertEqual(restart.daemonEpoch, 2)
        interrupted = try XCTUnwrap(store.load().state)
        XCTAssertEqual(interrupted.commands.first { $0.id == "send" }?.outcome, .interrupted)
        let restartedOwner = LinuxDaemonIngressOwner(store: store, runtime: runtime)
        restartedOwner.completeStartup(restart)
        XCTAssertThrowsError(try restartedOwner.perform(send))
        let recoveredSend = LinuxIngressRequest(
            operation: .send, commandID: "send", taskID: "task",
            sessionID: runtime.sessionID, text: "continue", authorizeRecovery: true)
        _ = try restartedOwner.perform(recoveredSend)
        XCTAssertEqual(runtime.calls.filter { $0.hasPrefix("send:") }.count, 2)

        var responseFault = false
        restartedOwner.faultInjection = { point in
            if point == .afterReceiptBeforeResponse, !responseFault {
                responseFault = true
                throw LinuxIngressFaultPoint.afterReceiptBeforeResponse
            }
        }
        let close = LinuxIngressRequest(
            operation: .close, commandID: "close", taskID: "task",
            sessionID: runtime.sessionID)
        XCTAssertThrowsError(try restartedOwner.perform(close))
        restartedOwner.faultInjection = nil
        let replayedResponse = try restartedOwner.perform(close)
        XCTAssertFalse(replayedResponse.isEmpty)
        XCTAssertEqual(runtime.calls.filter { $0.hasPrefix("close:") }.count, 1)
        let final = try XCTUnwrap(store.load().state)
        XCTAssertEqual(final.tasks.first { $0.id == "task" }?.state, .complete)
        XCTAssertEqual(final.terminals.first { $0.id == runtime.sessionID }?.state, .missing)
        XCTAssertEqual(final.commands.first { $0.id == "close" }?.outcome, .succeeded)
    }

    func testW43SchemaTwoSealsReplaySucceededAndInterruptedCommandsAfterMigration() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-w43-schema-two-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(
            at: scratch, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        let store = try LinuxDurableStateStore(stateDirectory: scratch.path)
        let runtime = FakeLinuxLifecycleRuntime()
        let succeeded = LinuxIngressRequest(
            operation: .send, commandID: "schema-two-succeeded", taskID: "legacy-task",
            sessionID: runtime.sessionID, text: "already delivered")
        let interrupted = LinuxIngressRequest(
            operation: .send, commandID: "schema-two-interrupted", taskID: "legacy-task",
            sessionID: runtime.sessionID, text: "resume me")
        let succeededSeal = try SchemaTwoIngressSeal(succeeded).bytes()
        let interruptedSeal = try SchemaTwoIngressSeal(interrupted).bytes()
        let cached = Data("schema-two-cached-response".utf8)
        let legacy = LinuxDurableState(
            schemaVersion: 2, minimumReaderVersion: 1,
            terminals: [.init(id: runtime.sessionID, taskID: "legacy-task", state: .present,
                              lastObservedAt: nil, evidenceDigest: nil)],
            tasks: [.init(id: "legacy-task", terminalID: runtime.sessionID, state: .working,
                          resultDigest: nil, acknowledgedEvidence: [])],
            queue: [
                .init(id: "queue-succeeded", taskID: "legacy-task",
                      commandID: succeeded.commandID, state: .complete,
                      payloadDigest: LinuxSHA256.hex(succeededSeal),
                      sealedPayloadRecoverable: true,
                      sealedPayloadBase64: succeededSeal.base64EncodedString()),
                .init(id: "queue-interrupted", taskID: "legacy-task",
                      commandID: interrupted.commandID, state: .queued,
                      payloadDigest: LinuxSHA256.hex(interruptedSeal),
                      sealedPayloadRecoverable: true,
                      sealedPayloadBase64: interruptedSeal.base64EncodedString()),
            ],
            commands: [
                .init(id: succeeded.commandID, taskID: "legacy-task",
                      terminalID: runtime.sessionID, operation: "send", stage: .delivered,
                      outcome: .succeeded, evidenceDigest: LinuxSHA256.hex(cached),
                      acknowledgedAt: nil, responseBase64: cached.base64EncodedString()),
                .init(id: interrupted.commandID, taskID: "legacy-task",
                      terminalID: runtime.sessionID, operation: "send", stage: .accepted,
                      outcome: .pending, evidenceDigest: nil, acknowledgedAt: nil),
            ])
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        try encoder.encode(legacy).write(to: URL(fileURLWithPath: store.legacyStatePath))
        try FileManager.default.setAttributes(
            [.posixPermissions: 0o600], ofItemAtPath: store.legacyStatePath)

        let restart = try LinuxStartupReconciler.reconcile(
            store: store, inventory: .complete([runtime.sessionID]))
        XCTAssertEqual(restart.stateDisposition, .migrated)
        let owner = LinuxDaemonIngressOwner(store: store, runtime: runtime)
        owner.completeStartup(restart)
        XCTAssertEqual(try succeeded.sealedBytes(), succeededSeal)
        XCTAssertEqual(try owner.perform(succeeded), cached)
        XCTAssertEqual(runtime.calls, [])

        let recovery = LinuxIngressRequest(
            operation: .send, commandID: interrupted.commandID, taskID: "legacy-task",
            sessionID: runtime.sessionID, text: "resume me", authorizeRecovery: true)
        XCTAssertEqual(try recovery.sealedBytes(), interruptedSeal)
        _ = try owner.perform(recovery)
        XCTAssertEqual(runtime.calls, ["send:schema-two-interrupted:resume me"])
    }

    func testW43AcknowledgedTaskSurvivesIncompleteInventoryReconciliation() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-w43-ack-incomplete-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(
            at: scratch, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        let store = try LinuxDurableStateStore(stateDirectory: scratch.path)
        let result = Data(#"{"status":"success"}"#.utf8)
        let digest = LinuxSHA256.hex(result)
        try store.publishTaskResult(result, taskID: "acknowledged-task", expectedDigest: digest)
        try store.save(LinuxDurableState(
            terminals: [.init(id: "%acknowledged", taskID: "acknowledged-task",
                              state: .present, lastObservedAt: nil, evidenceDigest: nil)],
            tasks: [.init(
                id: "acknowledged-task", terminalID: "%acknowledged", state: .acknowledged,
                resultDigest: digest, acknowledgedEvidence: [], projectRoot: "/srv/project",
                title: "Acknowledged", claims: [], secretDigest: LinuxSHA256.hex(Data("secret".utf8)),
                createdAt: "2026-09-12T00:00:00Z", resultByteCount: result.count,
                resultPublishedAt: "2026-09-12T00:01:00Z",
                resultAcknowledgedAt: "2026-09-12T00:02:00Z")]))

        let receipt = try LinuxStartupReconciler.reconcile(
            store: store, inventory: .incomplete("tmux inventory unavailable"))
        XCTAssertEqual(receipt.status, "inventory_incomplete")
        let persisted = try XCTUnwrap(store.load().state?.tasks.first)
        XCTAssertEqual(persisted.state, .acknowledged)
        XCTAssertEqual(persisted.resultAcknowledgedAt, "2026-09-12T00:02:00Z")
    }

    func testW43CompleteTaskBoardSessionAndDocumentLifecycleAcrossRestart() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-w43-flow-\(UUID().uuidString)")
        let stateRoot = scratch.appendingPathComponent("state")
        let project = scratch.appendingPathComponent("project")
        let projectArtifacts = project.appendingPathComponent("artifacts")
        defer { try? FileManager.default.removeItem(at: scratch) }
        for directory in [scratch, stateRoot, project, projectArtifacts] {
            try FileManager.default.createDirectory(
                at: directory, withIntermediateDirectories: true,
                attributes: [.posixPermissions: 0o700])
        }
        try Data("project document".utf8).write(
            to: projectArtifacts.appendingPathComponent("plan.md"))

        let store = try LinuxDurableStateStore(stateDirectory: stateRoot.path)
        let runtime = FakeLinuxLifecycleRuntime()
        let startup = try LinuxStartupReconciler.reconcile(store: store,
                                                            inventory: .complete([]))
        let owner = LinuxDaemonIngressOwner(store: store, runtime: runtime)
        owner.completeStartup(startup)
        let secret = "task-secret-never-persisted"
        let create = LinuxIngressRequest(
            operation: .taskCreate, commandID: "w43-create", taskID: "w43-task",
            projectRoot: project.path, assistant: .codex, taskSecret: secret,
            title: "Ubuntu lifecycle", claims: ["Sources/Linux.swift"])
        let createBytes = try owner.perform(create)
        let createReply = try JSONDecoder().decode(LinuxIngressResponse.self, from: createBytes)
        XCTAssertEqual(createReply.task?.sessionID, runtime.sessionID)
        XCTAssertEqual(createReply.task?.state, .working)
        XCTAssertFalse(String(data: try Data(contentsOf: URL(fileURLWithPath: store.statePath)),
                              encoding: .utf8)?.contains(secret) ?? true)

        let message = LinuxIngressRequest(
            operation: .taskMessage, commandID: "w43-message", taskID: "w43-task",
            sessionID: runtime.sessionID, projectRoot: project.path,
            text: "continue", taskSecret: secret)
        let messageReply = try owner.perform(message)
        XCTAssertEqual(runtime.calls.filter { $0.hasPrefix("send:w43-message") }.count, 1)
        XCTAssertEqual(try owner.perform(message), messageReply)
        XCTAssertEqual(runtime.calls.filter { $0.hasPrefix("send:w43-message") }.count, 1)
        XCTAssertThrowsError(try owner.perform(LinuxIngressRequest(
            operation: .taskMessage, commandID: "w43-message", taskID: "w43-task",
            sessionID: runtime.sessionID, projectRoot: project.path,
            text: "different", taskSecret: secret))) {
            XCTAssertEqual(($0 as? LinuxDurableStateFailure)?.code, "command_id_conflict")
        }
        _ = try owner.perform(LinuxIngressRequest(
            operation: .observe, commandID: "w43-observe", taskID: "w43-task",
            sessionID: runtime.sessionID))

        let result = Data(#"{"status":"success","summary":"ubuntu"}"#.utf8)
        let resultDigest = LinuxSHA256.hex(result)
        let resultRequest = LinuxIngressRequest(
            operation: .taskResult, commandID: "w43-result", taskID: "w43-task",
            sessionID: runtime.sessionID, projectRoot: project.path,
            taskSecret: secret, resultBase64: result.base64EncodedString(),
            resultDigest: resultDigest)
        let publishedBytes = try owner.perform(resultRequest)
        let published = try JSONDecoder().decode(LinuxIngressResponse.self,
                                                 from: publishedBytes)
        XCTAssertEqual(published.resultReceipt?.resultDigest, resultDigest)
        XCTAssertNil(published.resultReceipt?.acknowledgedAt)
        XCTAssertEqual(try owner.perform(LinuxIngressRequest(
            operation: .taskResult, commandID: "w43-result", taskID: "w43-task",
            sessionID: runtime.sessionID, projectRoot: project.path,
            taskSecret: secret, resultBase64: result.base64EncodedString(),
            resultDigest: resultDigest)), publishedBytes)
        let resultPath = store.tasksDirectory + "/w43-task/result.json"
        XCTAssertEqual(try Data(contentsOf: URL(fileURLWithPath: resultPath)), result)
        XCTAssertFalse(String(
            data: try Data(contentsOf: URL(fileURLWithPath: store.statePath)),
            encoding: .utf8)?.contains(result.base64EncodedString()) ?? true,
            "published result bytes must not be duplicated in global task authority")

        let taskArtifacts = URL(fileURLWithPath:
            try store.prepareTaskArtifacts(taskID: "w43-task"))
        try Data("task document".utf8).write(
            to: taskArtifacts.appendingPathComponent("delivery.txt"))
        let stateBeforeReads = try Data(contentsOf: URL(fileURLWithPath: store.statePath))
        let taskRead = try owner.perform(LinuxIngressRequest(
            operation: .taskRead, commandID: "read-task", taskID: "w43-task",
            sessionID: runtime.sessionID, projectRoot: project.path))
        let taskReply = try JSONDecoder().decode(LinuxIngressResponse.self, from: taskRead)
        XCTAssertEqual(taskReply.task?.resultBase64, result.base64EncodedString())
        let boardRead = try owner.perform(LinuxIngressRequest(
            operation: .boardRead, commandID: "read-board", taskID: "w43-task",
            sessionID: runtime.sessionID, projectRoot: project.path))
        let boardReply = try JSONDecoder().decode(LinuxIngressResponse.self, from: boardRead)
        XCTAssertEqual(boardReply.board?.authority, "linux_task_projection")
        XCTAssertEqual(boardReply.board?.canWrite, false)
        XCTAssertEqual(boardReply.board?.canManage, false)
        XCTAssertEqual(boardReply.board?.items.map(\.taskID), ["w43-task"])
        let sessionRead = try owner.perform(LinuxIngressRequest(
            operation: .sessionRead, commandID: "read-session", taskID: "w43-task",
            sessionID: runtime.sessionID, projectRoot: project.path))
        XCTAssertEqual(try JSONDecoder().decode(LinuxIngressResponse.self,
                                                from: sessionRead).session?.taskID,
                       "w43-task")
        let projectDocument = try owner.perform(LinuxIngressRequest(
            operation: .documentRead, commandID: "read-project-document",
            taskID: "w43-task", sessionID: runtime.sessionID,
            projectRoot: project.path, documentScope: .project,
            relativePath: "plan.md"))
        let projectDocumentReply = try JSONDecoder().decode(
            LinuxIngressResponse.self, from: projectDocument)
        XCTAssertEqual(projectDocumentReply.document?.identity.projectRoot, project.path)
        XCTAssertEqual(projectDocumentReply.document?.identity.sessionID, runtime.sessionID)
        XCTAssertEqual(projectDocumentReply.document?.identity.taskID, "w43-task")
        XCTAssertEqual(projectDocumentReply.document?.identity.scope, .project)
        XCTAssertEqual(Data(base64Encoded: projectDocumentReply.document?.bodyBase64 ?? ""),
                       Data("project document".utf8))
        let taskDocuments = try owner.perform(LinuxIngressRequest(
            operation: .documentsRead, commandID: "list-task-documents",
            taskID: "w43-task", sessionID: runtime.sessionID,
            projectRoot: project.path, documentScope: .task))
        XCTAssertEqual(try JSONDecoder().decode(LinuxIngressResponse.self,
                                                from: taskDocuments)
            .documents?.documents.map(\.identity.relativePath), ["delivery.txt"])
        XCTAssertEqual(try Data(contentsOf: URL(fileURLWithPath: store.statePath)),
                       stateBeforeReads, "read capabilities must not mutate task authority")

        XCTAssertThrowsError(try owner.perform(LinuxIngressRequest(
            operation: .taskRead, commandID: "wrong-project", taskID: "w43-task",
            sessionID: runtime.sessionID, projectRoot: scratch.path))) {
            XCTAssertEqual(($0 as? LinuxDurableStateFailure)?.code, "read_not_found")
        }
        XCTAssertThrowsError(try owner.perform(LinuxIngressRequest(
            operation: .taskMessage, commandID: "bad-auth", taskID: "w43-task",
            sessionID: runtime.sessionID, projectRoot: project.path,
            text: "secret", taskSecret: "wrong"))) {
            XCTAssertEqual(($0 as? LinuxDurableStateFailure)?.code, "task_unauthorized")
        }
        XCTAssertThrowsError(try owner.perform(LinuxIngressRequest(
            operation: .taskMessage, commandID: message.commandID, taskID: "w43-task",
            sessionID: runtime.sessionID, projectRoot: project.path,
            text: "continue", taskSecret: "wrong"))) {
            XCTAssertEqual(($0 as? LinuxDurableStateFailure)?.code, "task_unauthorized")
        }
        XCTAssertThrowsError(try owner.perform(LinuxIngressRequest(
            operation: .taskMessage, commandID: "missing-auth", taskID: "missing-task",
            sessionID: runtime.sessionID, projectRoot: project.path,
            text: "secret", taskSecret: "wrong"))) {
            XCTAssertEqual(($0 as? LinuxDurableStateFailure)?.code, "task_unauthorized")
        }
        XCTAssertThrowsError(try owner.perform(LinuxIngressRequest(
            operation: .boardRead, commandID: "read-cannot-write", taskID: "w43-task",
            sessionID: runtime.sessionID, projectRoot: project.path, text: "mutate"))) {
            XCTAssertEqual(($0 as? LinuxDurableStateFailure)?.code, "invalid_ingress")
        }
        XCTAssertEqual(try Data(contentsOf: URL(fileURLWithPath: store.statePath)),
                       stateBeforeReads)

        let restart = try LinuxStartupReconciler.reconcile(
            store: store, inventory: .complete([runtime.sessionID]))
        XCTAssertEqual(restart.daemonEpoch, 2)
        let restartedOwner = LinuxDaemonIngressOwner(store: store, runtime: runtime)
        restartedOwner.completeStartup(restart)
        XCTAssertEqual(try restartedOwner.perform(resultRequest), publishedBytes)
        _ = try restartedOwner.perform(LinuxIngressRequest(
            operation: .taskMessage, commandID: "w43-continue", taskID: "w43-task",
            sessionID: runtime.sessionID, projectRoot: project.path,
            text: "after restart", taskSecret: secret))
        let acknowledged = try restartedOwner.perform(LinuxIngressRequest(
            operation: .taskAcknowledge, commandID: "w43-ack", taskID: "w43-task",
            sessionID: runtime.sessionID, projectRoot: project.path,
            resultDigest: resultDigest))
        XCTAssertNotNil(try JSONDecoder().decode(LinuxIngressResponse.self,
                                                 from: acknowledged)
            .resultReceipt?.acknowledgedAt)
        _ = try restartedOwner.perform(LinuxIngressRequest(
            operation: .taskClose, commandID: "w43-close", taskID: "w43-task",
            sessionID: runtime.sessionID, projectRoot: project.path))
        let final = try XCTUnwrap(store.load().state)
        XCTAssertEqual(final.tasks.first?.state, .complete)
        XCTAssertNotNil(final.tasks.first?.resultAcknowledgedAt)
        XCTAssertEqual(final.tasks.first?.messages.count, 2)
        XCTAssertEqual(final.terminals.first?.state, .missing)

        try Data("tampered result".utf8).write(
            to: URL(fileURLWithPath: resultPath), options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                              ofItemAtPath: resultPath)
        let corruptedResult = try LinuxStartupReconciler.reconcile(
            store: store, inventory: .complete([]))
        XCTAssertFalse(corruptedResult.authoritative)
        XCTAssertEqual(corruptedResult.status, "state_non_authoritative")
        XCTAssertEqual(corruptedResult.stateDisposition, .recoveryRequired)
    }

    func testW43ResultReplayAcknowledgeAndCloseRevalidatePinnedBytes() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-w43-result-revalidate-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: scratch) }

        func publishedFixture(_ name: String) throws
            -> (LinuxDurableStateStore, LinuxDaemonIngressOwner, LinuxIngressRequest, String, String) {
            let stateRoot = scratch.appendingPathComponent(name + "-state")
            let project = scratch.appendingPathComponent(name + "-project")
            for directory in [stateRoot, project] {
                try FileManager.default.createDirectory(
                    at: directory, withIntermediateDirectories: true,
                    attributes: [.posixPermissions: 0o700])
            }
            let store = try LinuxDurableStateStore(stateDirectory: stateRoot.path)
            let runtime = FakeLinuxLifecycleRuntime()
            let startup = try LinuxStartupReconciler.reconcile(store: store,
                                                                inventory: .complete([]))
            let owner = LinuxDaemonIngressOwner(store: store, runtime: runtime)
            owner.completeStartup(startup)
            let taskID = name + "-task"
            let secret = name + "-secret"
            _ = try owner.perform(LinuxIngressRequest(
                operation: .taskCreate, commandID: name + "-create", taskID: taskID,
                projectRoot: project.path, assistant: .codex, taskSecret: secret,
                title: name, claims: []))
            let result = Data(#"{"status":"success"}"#.utf8)
            let digest = LinuxSHA256.hex(result)
            let request = LinuxIngressRequest(
                operation: .taskResult, commandID: name + "-result", taskID: taskID,
                sessionID: runtime.sessionID, projectRoot: project.path,
                taskSecret: secret, resultBase64: result.base64EncodedString(),
                resultDigest: digest)
            _ = try owner.perform(request)
            return (store, owner, request, digest,
                    store.tasksDirectory + "/" + taskID + "/result.json")
        }

        do {
            let (store, owner, request, _, path) = try publishedFixture("tampered-replay")
            try Data("tampered".utf8).write(to: URL(fileURLWithPath: path), options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: path)
            XCTAssertThrowsError(try owner.perform(request)) {
                XCTAssertEqual(($0 as? LinuxDurableStateFailure)?.code,
                               "task_result_non_authoritative")
            }
            XCTAssertEqual(store.load().disposition, .recoveryRequired)
        }
        do {
            let (store, owner, request, digest, path) = try publishedFixture("missing-ack")
            try FileManager.default.removeItem(atPath: path)
            XCTAssertThrowsError(try owner.perform(LinuxIngressRequest(
                operation: .taskAcknowledge, commandID: "missing-ack-command",
                taskID: request.taskID, sessionID: request.sessionID,
                projectRoot: request.projectRoot, resultDigest: digest))) {
                XCTAssertEqual(($0 as? LinuxDurableStateFailure)?.code,
                               "task_result_non_authoritative")
            }
            XCTAssertEqual(store.load().disposition, .recoveryRequired)
        }
        do {
            let (store, owner, request, digest, path) = try publishedFixture("linked-close")
            _ = try owner.perform(LinuxIngressRequest(
                operation: .taskAcknowledge, commandID: "linked-close-ack",
                taskID: request.taskID, sessionID: request.sessionID,
                projectRoot: request.projectRoot, resultDigest: digest))
            let outside = scratch.appendingPathComponent("linked-close-outside.json")
            try Data("outside".utf8).write(to: outside)
            try FileManager.default.removeItem(atPath: path)
            try FileManager.default.createSymbolicLink(
                at: URL(fileURLWithPath: path), withDestinationURL: outside)
            XCTAssertThrowsError(try owner.perform(LinuxIngressRequest(
                operation: .taskClose, commandID: "linked-close-command",
                taskID: request.taskID, sessionID: request.sessionID,
                projectRoot: request.projectRoot))) {
                XCTAssertEqual(($0 as? LinuxDurableStateFailure)?.code,
                               "task_result_non_authoritative")
            }
            XCTAssertEqual(store.load().disposition, .recoveryRequired)
        }
    }

    func testW43DocumentListTruncatesOnlyWhenAFileIsOmitted() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-w43-list-bound-\(UUID().uuidString)")
        let project = scratch.appendingPathComponent("project")
        let artifacts = project.appendingPathComponent("artifacts")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(
            at: artifacts, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        for index in 0..<HeadlessDocumentPathPolicy.maximumListed {
            try Data("x".utf8).write(
                to: artifacts.appendingPathComponent(String(format: "%03d.md", index)))
        }
        let reader = LinuxDocumentReader(tasksDirectory: scratch.path, expectedUID: geteuid())
        let exact = try reader.list(
            projectRoot: project.path, sessionID: "%session", taskID: "task", scope: .project)
        XCTAssertEqual(exact.documents.count, HeadlessDocumentPathPolicy.maximumListed)
        XCTAssertFalse(exact.truncated)

        try Data("x".utf8).write(to: artifacts.appendingPathComponent("200.md"))
        let overflow = try reader.list(
            projectRoot: project.path, sessionID: "%session", taskID: "task", scope: .project)
        XCTAssertEqual(overflow.documents.count, HeadlessDocumentPathPolicy.maximumListed)
        XCTAssertTrue(overflow.truncated)
        XCTAssertEqual(overflow.documents.map(\.identity.relativePath),
                       (0..<HeadlessDocumentPathPolicy.maximumListed).map {
                           String(format: "%03d.md", $0)
                       })
    }

    func testW43DocumentListHoldsRootAndEnforcesDepthAndWalkBounds() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-w43-list-walk-\(UUID().uuidString)")
        let project = scratch.appendingPathComponent("project")
        let artifacts = project.appendingPathComponent("artifacts")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(
            at: artifacts, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        try Data("held".utf8).write(to: artifacts.appendingPathComponent("held.md"))
        let heldArtifacts = project.appendingPathComponent("artifacts-held")
        let reader = LinuxDocumentReader(tasksDirectory: scratch.path, expectedUID: geteuid())
        reader.afterRootOpenForTesting = { _ in
            try FileManager.default.moveItem(at: artifacts, to: heldArtifacts)
            try FileManager.default.createDirectory(
                at: artifacts, withIntermediateDirectories: false,
                attributes: [.posixPermissions: 0o700])
            try Data("replacement".utf8).write(
                to: artifacts.appendingPathComponent("replacement.md"))
        }
        let held = try reader.list(
            projectRoot: project.path, sessionID: "%session", taskID: "task", scope: .project)
        XCTAssertEqual(held.documents.map(\.identity.relativePath), ["held.md"])

        let boundedProject = scratch.appendingPathComponent("bounded-project")
        let boundedArtifacts = boundedProject.appendingPathComponent("artifacts")
        let deepest = boundedArtifacts.appendingPathComponent("a/b/c/d/e")
        let tooDeep = deepest.appendingPathComponent("f")
        try FileManager.default.createDirectory(
            at: tooDeep, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        try Data("limit".utf8).write(to: deepest.appendingPathComponent("limit.md"))
        try Data("too deep".utf8).write(to: tooDeep.appendingPathComponent("excluded.md"))
        let boundedReader = LinuxDocumentReader(
            tasksDirectory: scratch.path, expectedUID: geteuid())
        let bounded = try boundedReader.list(
            projectRoot: boundedProject.path, sessionID: "%session",
            taskID: "task", scope: .project)
        XCTAssertEqual(bounded.documents.map(\.identity.relativePath), ["a/b/c/d/e/limit.md"])

        let walkProject = scratch.appendingPathComponent("walk-project")
        let walkArtifacts = walkProject.appendingPathComponent("artifacts")
        try FileManager.default.createDirectory(
            at: walkArtifacts, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        for index in 0...HeadlessDocumentPathPolicy.maximumWalked {
            XCTAssertTrue(FileManager.default.createFile(
                atPath: walkArtifacts.appendingPathComponent("ignored-\(index).json").path,
                contents: Data()))
        }
        let walked = try boundedReader.list(
            projectRoot: walkProject.path, sessionID: "%session",
            taskID: "task", scope: .project)
        XCTAssertTrue(walked.documents.isEmpty)
        XCTAssertTrue(walked.truncated)
    }

    func testW43LegacyMigrationCutoverHasNoEmptyAuthorityFaultWindow() throws {
        for point in [LinuxLegacyMigrationFaultPoint.afterAuthorityWrite,
                      .afterLegacyArchiveWrite, .afterRollbackFenceWrite] {
            let scratch = canonicalTemporaryDirectory()
                .appendingPathComponent("clawdline-w43-migration-\(point.rawValue)-\(UUID().uuidString)")
            defer { try? FileManager.default.removeItem(at: scratch) }
            try FileManager.default.createDirectory(
                at: scratch, withIntermediateDirectories: true,
                attributes: [.posixPermissions: 0o700])
            let store = try LinuxDurableStateStore(stateDirectory: scratch.path)
            let legacy = Data(#"{"schemaVersion":2,"minimumReaderVersion":1,"daemonEpoch":0,"terminals":[],"tasks":[],"queue":[],"commands":[]}"#.utf8)
            try legacy.write(to: URL(fileURLWithPath: store.legacyStatePath))
            try FileManager.default.setAttributes(
                [.posixPermissions: 0o600], ofItemAtPath: store.legacyStatePath)
            store.migrationFaultInjection = { observed in
                if observed == point { throw observed }
            }
            let loaded = store.load()
            XCTAssertFalse(loaded.authoritative)
            XCTAssertEqual(loaded.disposition, .recoveryRequired)
            XCTAssertTrue(FileManager.default.fileExists(atPath: store.statePath))
            let oldPath = try Data(contentsOf: URL(fileURLWithPath: store.legacyStatePath))
            if point == .afterRollbackFenceWrite {
                let fence = try XCTUnwrap(
                    JSONSerialization.jsonObject(with: oldPath) as? [String: Any])
                XCTAssertEqual(fence["recordKind"] as? String,
                               "clawdline_linux_task_authority_rollback_fence")
            } else {
                XCTAssertEqual(oldPath, legacy,
                               "the old reader must retain schema-2 authority until fence cutover")
            }
        }
    }

    func testW43DocumentAndAuthorityFilesRefuseTraversalLinksDevicesAndOversize() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-w43-refusals-\(UUID().uuidString)")
        let stateRoot = scratch.appendingPathComponent("state")
        let project = scratch.appendingPathComponent("project")
        let artifacts = project.appendingPathComponent("artifacts")
        defer { try? FileManager.default.removeItem(at: scratch) }
        for directory in [scratch, stateRoot, project, artifacts] {
            try FileManager.default.createDirectory(
                at: directory, withIntermediateDirectories: true,
                attributes: [.posixPermissions: 0o700])
        }
        let store = try LinuxDurableStateStore(stateDirectory: stateRoot.path)
        let runtime = FakeLinuxLifecycleRuntime()
        let startup = try LinuxStartupReconciler.reconcile(store: store,
                                                            inventory: .complete([]))
        let owner = LinuxDaemonIngressOwner(store: store, runtime: runtime)
        owner.completeStartup(startup)
        _ = try owner.perform(LinuxIngressRequest(
            operation: .taskCreate, commandID: "create-refusals", taskID: "refusal-task",
            projectRoot: project.path, assistant: .codex, taskSecret: "secret",
            title: "Refusals", claims: []))

        let outside = scratch.appendingPathComponent("outside.md")
        try Data("outside".utf8).write(to: outside)
        try FileManager.default.createSymbolicLink(
            at: artifacts.appendingPathComponent("linked.md"), withDestinationURL: outside)
        try FileManager.default.linkItem(
            at: outside, to: artifacts.appendingPathComponent("hard.md"))
        let fifo = artifacts.appendingPathComponent("device.txt")
        XCTAssertEqual(fifo.path.withCString { mkfifo($0, 0o600) }, 0)
        try Data(count: HeadlessDocumentPathPolicy.maximumBytes + 1).write(
            to: artifacts.appendingPathComponent("large.md"))

        func document(_ command: String, _ path: String) -> LinuxIngressRequest {
            LinuxIngressRequest(
                operation: .documentRead, commandID: command, taskID: "refusal-task",
                sessionID: runtime.sessionID, projectRoot: project.path,
                documentScope: .project, relativePath: path)
        }
        for (command, path) in [("traversal", "../outside.md"),
                                ("symlink", "linked.md"),
                                ("hardlink", "hard.md"),
                                ("device", "device.txt")] {
            XCTAssertThrowsError(try owner.perform(document(command, path)))
        }
        XCTAssertThrowsError(try owner.perform(document("oversize", "large.md"))) {
            XCTAssertEqual(($0 as? LinuxDurableStateFailure)?.code, "document_too_large")
        }

        // The authority file itself is subject to the same no-follow/regular/single-link rule.
        let authoritative = try Data(contentsOf: URL(fileURLWithPath: store.statePath))
        let linkedAuthority = scratch.appendingPathComponent("authority-copy.json")
        try FileManager.default.moveItem(at: URL(fileURLWithPath: store.statePath),
                                         to: linkedAuthority)
        try FileManager.default.linkItem(at: linkedAuthority,
                                         to: URL(fileURLWithPath: store.statePath))
        let refused = store.load()
        XCTAssertFalse(refused.authoritative)
        XCTAssertEqual(refused.disposition, .quarantined)
        XCTAssertEqual(try Data(contentsOf: linkedAuthority), authoritative)
        XCTAssertEqual(store.load().disposition, .recoveryRequired)
    }

    func testW43MigratesLegacyRecordRootIntoAuthoritativeTaskRoot() throws {
        let scratch = canonicalTemporaryDirectory()
            .appendingPathComponent("clawdline-w43-legacy-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(
            at: scratch, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        let store = try LinuxDurableStateStore(stateDirectory: scratch.path)
        try FileManager.default.createDirectory(
            atPath: store.legacyRecordsDirectory, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        let legacy = LinuxDurableState(schemaVersion: 2)
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        try encoder.encode(legacy).write(to: URL(fileURLWithPath: store.legacyStatePath))
        try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                              ofItemAtPath: store.legacyStatePath)
        let loaded = store.load()
        XCTAssertTrue(loaded.authoritative)
        XCTAssertEqual(loaded.disposition, .migrated)
        XCTAssertEqual(loaded.state?.schemaVersion, 3)
        XCTAssertTrue(FileManager.default.fileExists(atPath: store.statePath))
        XCTAssertTrue(store.statePath.hasSuffix("/tasks/authority.json"))
        XCTAssertTrue(FileManager.default.fileExists(atPath: store.legacyStatePath))
        XCTAssertNotNil(loaded.preservedOriginal)
        XCTAssertTrue(FileManager.default.fileExists(atPath: loaded.preservedOriginal ?? ""))
        let fence = try JSONSerialization.jsonObject(
            with: Data(contentsOf: URL(fileURLWithPath: store.legacyStatePath))) as? [String: Any]
        XCTAssertEqual(fence?["schemaVersion"] as? Int, 3)
        XCTAssertEqual(fence?["minimumReaderVersion"] as? Int, 3)
        XCTAssertEqual(fence?["authorityPath"] as? String, "tasks/authority.json")
        try FileManager.default.removeItem(atPath: store.statePath)
        let missingAuthority = store.load()
        XCTAssertFalse(missingAuthority.authoritative)
        XCTAssertEqual(missingAuthority.disposition, .quarantined)
        XCTAssertTrue(missingAuthority.reason?.contains("rollback fence") == true)
        XCTAssertEqual(store.load().disposition, .recoveryRequired)
    }

    func testW42DaemonHealthSeparatesServiceReconciliationFromProviderAuthentication() throws {
        let oldPackage = getenv("CLAWDLINE_PACKAGE_VERSION").map { String(cString: $0) }
        let oldBuild = getenv("CLAWDLINE_BUILD_IDENTITY").map { String(cString: $0) }
        let oldSource = getenv("CLAWDLINE_SOURCE_COMMIT").map { String(cString: $0) }
        let oldDigest = getenv("CLAWDLINE_PACKAGE_DIGEST").map { String(cString: $0) }
        defer {
            restoreEnvironment("CLAWDLINE_PACKAGE_VERSION", oldPackage)
            restoreEnvironment("CLAWDLINE_BUILD_IDENTITY", oldBuild)
            restoreEnvironment("CLAWDLINE_SOURCE_COMMIT", oldSource)
            restoreEnvironment("CLAWDLINE_PACKAGE_DIGEST", oldDigest)
        }
        setenv("CLAWDLINE_PACKAGE_VERSION", "1.2.3", 1)
        setenv("CLAWDLINE_BUILD_IDENTITY", "build-123", 1)
        setenv("CLAWDLINE_SOURCE_COMMIT", String(repeating: "a", count: 40), 1)
        setenv("CLAWDLINE_PACKAGE_DIGEST", String(repeating: "b", count: 64), 1)
        let receipt = LinuxStartupReconciliationReceipt(
            authoritative: true, stateDisposition: .loaded, schemaVersion: 3,
            daemonEpoch: 7, status: "complete", terminalPresent: 0,
            terminalMissing: 0, terminalUnknown: 0, taskTerminal: 0,
            taskReconciling: 0, taskUnknown: 0, queueRecoverable: 0,
            queueUnknown: 0, commandSucceeded: 0, commandInterrupted: 0,
            commandUnknown: 0, preservedOriginal: nil, reason: nil)
        let health = LinuxDaemonService.makeHealth(
            receipt: receipt, providerIdentity: .configured())
        XCTAssertTrue(health.serviceReady)
        XCTAssertFalse(health.ready)
        XCTAssertEqual(health.readinessCode, "w4_provider_authentication_not_proven")
        XCTAssertEqual(health.release.packageVersion, "1.2.3")
        XCTAssertEqual(health.durableSchemaVersion, 3)
        XCTAssertEqual(health.protocolIdentity, "clawdline-linux-local-health-v1")
        XCTAssertTrue(health.providers.allSatisfy { !$0.authenticated && !$0.usable })
    }

    private func canonicalTemporaryDirectory() -> URL {
        let path = FileManager.default.temporaryDirectory.path
        #if os(macOS)
        // Foundation preserves the public `/var` spelling even though macOS implements it as a
        // symlink to `/private/var`; the Linux policy correctly treats that spelling as linked.
        if path == "/var" || path.hasPrefix("/var/") {
            return URL(fileURLWithPath: "/private" + path, isDirectory: true)
        }
        #endif
        return URL(fileURLWithPath: path, isDirectory: true).resolvingSymlinksInPath()
    }

    private func restoreEnvironment(_ name: String, _ value: String?) {
        if let value { setenv(name, value, 1) }
        else { unsetenv(name) }
    }

    private struct FixedProjectRootInspector: ProjectRootInspecting {
        let canonical: String
        let linked: String
        let uid: UInt32
        let gid: UInt32

        func inspectProjectRoot(at path: String) throws -> ProjectRootEvidence {
            ProjectRootEvidence(
                requestedPath: path,
                canonicalPath: path == linked ? canonical : path,
                isDirectory: true,
                containsSymbolicLink: path == linked,
                ownerUID: uid,
                ownerGID: gid)
        }
    }
}
