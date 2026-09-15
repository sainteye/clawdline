import Foundation
import XCTest
@testable import ClawdlineApplication
@testable import ClawdlineLinux

private final class HeartbeatClock: @unchecked Sendable {
    private let lock = NSLock()
    private var value: TimeInterval

    init(_ value: TimeInterval) { self.value = value }

    func read() -> TimeInterval {
        lock.lock(); defer { lock.unlock() }
        return value
    }

    func set(_ next: TimeInterval) {
        lock.lock(); value = next; lock.unlock()
    }
}

private final class HeartbeatSecretStore: SecretStore, @unchecked Sendable {
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
        let value = try create()
        values[account] = value
        return value
    }

    func rotate(_ account: String, replace: @Sendable (Data?) throws -> Data) throws -> Data {
        lock.lock(); defer { lock.unlock() }
        let value = try replace(values[account])
        values[account] = value
        return value
    }

    func remove(_ account: String) throws {
        lock.lock(); values.removeValue(forKey: account); lock.unlock()
    }
}

private final class HeartbeatLifecycle: LinuxLifecyclePerforming {
    func create(commandID: String, projectRoot: String, assistant: Assistant,
                model: String?, reasoningEffort: ReasoningEffort?, permission: Permission,
                additionalDirectory: String?, resumeSessionID: String?) throws
        -> LinuxLifecycleReceipt {
        throw LinuxDurableStateFailure(code: "unused", message: "unused")
    }

    func send(commandID: String, sessionID: String, text: String) throws
        -> LinuxLifecycleReceipt {
        throw LinuxDurableStateFailure(code: "unused", message: "unused")
    }

    func observe(commandID: String, sessionID: String) throws -> LinuxLifecycleReceipt {
        throw LinuxDurableStateFailure(code: "unused", message: "unused")
    }

    func close(commandID: String, sessionID: String) throws -> LinuxLifecycleReceipt {
        throw LinuxDurableStateFailure(code: "unused", message: "unused")
    }


    func cloudSessionIdentity(sessionID _: String) throws -> LinuxCloudSessionIdentity {
        throw LinuxDurableStateFailure(
            code: "session_identity_incomplete",
            message: "Presence heartbeat tests expose no Session process identity.")
    }
}

private final class HeartbeatTransport: CloudTransporting, @unchecked Sendable {
    let source = CloudInboundCommandSource()
    var commands: CloudInboundCommandStream { source.stream }
    let readyGenerations: AsyncStream<UInt64>
    let outboundReceipts: AsyncStream<CloudOutboundTransportReceipt>
    private let readyContinuation: AsyncStream<UInt64>.Continuation
    private let receiptContinuation: AsyncStream<CloudOutboundTransportReceipt>.Continuation
    private let lock = NSLock()
    private var writes: [Data] = []

    init() {
        var ready: AsyncStream<UInt64>.Continuation!
        readyGenerations = AsyncStream(bufferingPolicy: .bufferingNewest(1)) { ready = $0 }
        readyContinuation = ready
        var receipts: AsyncStream<CloudOutboundTransportReceipt>.Continuation!
        outboundReceipts = AsyncStream { receipts = $0 }
        receiptContinuation = receipts
    }

    func connect(role: CloudTransportRole) async throws {
        XCTAssertEqual(role, .machine)
        readyContinuation.yield(1)
    }

    func sendExactPublishFrame(_ bytes: Data) async throws {
        append(bytes)
    }

    func setInboundRefusalHandler(_ handler: CloudTransport.InboundRefusalHandler?) async {}
    func setTerminalAuthorizationHandler(
        _ handler: CloudTransport.TerminalAuthorizationHandler?
    ) async {}

    func shutdown() async {
        source.finish()
        readyContinuation.finish()
        receiptContinuation.finish()
    }

    func receipt(_ value: CloudOutboundTransportReceipt) { receiptContinuation.yield(value) }
    func frames() -> [Data] { lock.lock(); defer { lock.unlock() }; return writes }

    private func append(_ bytes: Data) {
        lock.lock(); writes.append(bytes); lock.unlock()
    }
}

// The focused package runner selects `LinuxRuntimeContractTests`; keep this additive fixture under
// that explicit prefix so the production gate executes it without widening to unrelated targets.
final class LinuxRuntimeContractTestsPresenceHeartbeat: XCTestCase {
    func testAuthenticatedRunningMachineRefreshesDescriptorBeforeViewerFreshnessExpires()
        async throws {
        let scratch = FileManager.default.temporaryDirectory
            .appendingPathComponent("clawdline-linux-presence-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: scratch) }
        try FileManager.default.createDirectory(
            at: scratch, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])

        let secrets = HeartbeatSecretStore()
        let authority = CloudExecutorIdentityAuthority(store: secrets)
        let machineKey = try CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 0x71, count: 32))
        let master = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x72, count: 32))
        let identity = try authority.provision(
            accountID: "account-presence", machineID: "machine-presence",
            deviceKey: machineKey, masterSecret: master)
        let durable = try CloudDurableRuntime.open(
            directory: scratch.appendingPathComponent("cloud"), runtime: nil,
            strictPersistedFrameValidation: true)
        let transport = HeartbeatTransport()
        let outbound = CloudDurableOutboundComposition(
            spool: durable.spool, transport: transport,
            identity: CloudAppIdentity(
                machineID: identity.machineID, deviceID: identity.deviceID,
                keyID: identity.keyID, masterSecret: master, signingKey: machineKey),
            nowMilliseconds: { UInt64(max(0, Date().timeIntervalSince1970 * 1_000)) })
        let store = try LinuxDurableStateStore(
            stateDirectory: scratch.appendingPathComponent("daemon").path)
        let startup = try LinuxStartupReconciler.reconcile(store: store, inventory: .complete([]))
        let ingress = LinuxDaemonIngressOwner(store: store, runtime: HeartbeatLifecycle())
        ingress.completeStartup(startup)
        let clock = HeartbeatClock(1_000)
        let relay = LinuxRelayRuntimeOwner(
            machine: CloudMachineIdentity(
                accountID: identity.accountID, machineID: identity.machineID),
            identityAuthority: authority, transport: transport, outbound: outbound,
            ingress: ingress, commandsEnabled: { true },
            presentation: LinuxRelayMachinePresentation(
                displayName: "AWS worker", provider: "aws"),
            places: [LinuxRelayPlace(id: "reaver", label: "reaver", path: scratch.path)],
            monotonicNow: { clock.read() })

        try await relay.start()
        try await waitForFrameCount(2, transport: transport)
        for bytes in transport.frames() {
            let frame = try JSONDecoder().decode(CloudPublishFrame.self, from: bytes)
            transport.receipt(CloudOutboundTransportReceipt(
                channel: frame.envelope.ch, sequence: Int64(frame.envelope.seq), kind: .delivered))
        }

        clock.set(1_239)
        await relay.publishInventory(TerminalInventory())
        try await Task.sleep(nanoseconds: 20_000_000)
        XCTAssertEqual(transport.frames().count, 2,
                       "presence must not mint a frame before its bounded interval")

        clock.set(1_240)
        await relay.publishInventory(TerminalInventory(
            sessions: [], error: "tmux temporarily unreadable", isComplete: false))
        try await waitForFrameCount(3, transport: transport)
        let heartbeat = try JSONDecoder().decode(
            CloudPublishFrame.self, from: transport.frames()[2])
        XCTAssertEqual(heartbeat.envelope.ch, "orch/machine-presence")
        let clear = try heartbeat.envelope.open(
            masterSecret: master,
            publicKeyForSender: { $0 == identity.machineID ? machineKey.publicKeyRaw : nil })
        let payload = try XCTUnwrap(
            JSONSerialization.jsonObject(with: clear) as? [String: Any])
        XCTAssertEqual((payload["machine"] as? [String: Any])?["name"] as? String, "AWS worker")
        XCTAssertEqual((payload["machine"] as? [String: Any])?["platform"] as? String, "linux")
        XCTAssertEqual((payload["machine"] as? [String: Any])?["provider"] as? String, "aws")
        XCTAssertLessThan(
            LinuxRelayRuntimeOwner.descriptorHeartbeatIntervalSeconds, 300,
            "the authenticated refresh must precede the hosted viewer freshness deadline")

        await relay.stop()
        clock.set(2_000)
        await relay.publishInventory(TerminalInventory())
        try await Task.sleep(nanoseconds: 20_000_000)
        XCTAssertEqual(transport.frames().count, 3,
                       "a stopped owner must never manufacture machine presence")
    }

    private func waitForFrameCount(_ count: Int, transport: HeartbeatTransport) async throws {
        for _ in 0..<200 {
            if transport.frames().count >= count { return }
            try await Task.sleep(nanoseconds: 5_000_000)
        }
        XCTFail("timed out waiting for \(count) outbound frames")
        throw LinuxDurableStateFailure(code: "test_timeout", message: "frame timeout")
    }
}
