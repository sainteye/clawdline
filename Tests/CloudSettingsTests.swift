import Foundation

private enum ForcedFailure: Error, LocalizedError {
    case test
    var errorDescription: String? { "forced failure" }
}

private actor AsyncGate {
    private var permits = 0
    private var waiters: [CheckedContinuation<Void, Never>] = []

    func wait() async {
        if permits > 0 {
            permits -= 1
            return
        }
        await withCheckedContinuation { waiters.append($0) }
    }

    func signal() {
        if waiters.isEmpty { permits += 1 }
        else { waiters.removeFirst().resume() }
    }
}

private actor PollProbe {
    private let gate = AsyncGate()
    private var entered = false

    func run(
        identity: CloudMachineIdentity,
        onState: @escaping @Sendable (CloudDeviceLoginPollState) async -> Void
    ) async throws -> CloudDeviceLoginPollState {
        entered = true
        await onState(.authorizationPending)
        await gate.wait()
        await onState(.slowDown(retryAfter: 7))
        await gate.wait()
        return .complete(identity)
    }

    func hasEntered() -> Bool { entered }
    func advance() async { await gate.signal() }
}

private actor CancellationProbe {
    private var entered = false
    private var cancelled = false

    func run() async throws -> CloudDeviceLoginPollState {
        entered = true
        return try await withTaskCancellationHandler {
            try await Task.sleep(nanoseconds: 60_000_000_000)
            return .expired
        } onCancel: {
            Task { await self.markCancelled() }
        }
    }

    private func markCancelled() { cancelled = true }
    func hasEntered() -> Bool { entered }
    func wasCancelled() -> Bool { cancelled }
}

private actor ProductionHTTPTransport: CloudAccountHTTPTransport {
    private let startResponse: Data
    private var requestPaths: [String] = []

    init(startResponse: String) {
        self.startResponse = Data(startResponse.utf8)
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let path = request.url?.path ?? ""
        requestPaths.append(path)
        guard path == "/v1/auth/device/start", let url = request.url else {
            throw ForcedFailure.test
        }
        let response = HTTPURLResponse(
            url: url, statusCode: 200, httpVersion: "HTTP/1.1",
            headerFields: ["Content-Type": "application/json"]
        )!
        return (startResponse, response)
    }

    func paths() -> [String] { requestPaths }
}

private actor DeinitPollProbe {
    private var entered = false
    private var cancelled = false
    private var continuation: CheckedContinuation<CloudDeviceLoginPollState, Never>?

    func run() async -> CloudDeviceLoginPollState {
        entered = true
        return await withTaskCancellationHandler {
            await withCheckedContinuation { continuation = $0 }
        } onCancel: {
            Task { await self.markCancelled() }
        }
    }

    private func markCancelled() { cancelled = true }
    func hasEntered() -> Bool { entered }
    func wasCancelled() -> Bool { cancelled }
    func complete(_ identity: CloudMachineIdentity) {
        continuation?.resume(returning: .complete(identity))
        continuation = nil
    }
}

private actor DeinitStartProbe {
    private var entered = false
    private var cancelled = false
    private var continuation: CheckedContinuation<CloudSettingsLoginAttempt, Never>?

    func run() async -> CloudSettingsLoginAttempt {
        entered = true
        return await withTaskCancellationHandler {
            await withCheckedContinuation { continuation = $0 }
        } onCancel: {
            Task { await self.markCancelled() }
        }
    }

    private func markCancelled() { cancelled = true }
    func hasEntered() -> Bool { entered }
    func wasCancelled() -> Bool { cancelled }
    func complete(_ attempt: CloudSettingsLoginAttempt) {
        continuation?.resume(returning: attempt)
        continuation = nil
    }
}

private actor SequencedStarter {
    private var calls = 0
    private var firstContinuation: CheckedContinuation<CloudSettingsLoginAttempt, Never>?
    let first: CloudSettingsLoginAttempt
    let second: CloudSettingsLoginAttempt

    init(first: CloudSettingsLoginAttempt, second: CloudSettingsLoginAttempt) {
        self.first = first
        self.second = second
    }

    func start() async -> CloudSettingsLoginAttempt {
        calls += 1
        if calls == 1 {
            return await withCheckedContinuation { firstContinuation = $0 }
        }
        return second
    }

    func count() -> Int { calls }
    func releaseFirst() {
        firstContinuation?.resume(returning: first)
        firstContinuation = nil
    }
}

private actor AttemptQueue {
    private var attempts: [CloudSettingsLoginAttempt]

    init(_ attempts: [CloudSettingsLoginAttempt]) { self.attempts = attempts }

    func next() -> CloudSettingsLoginAttempt {
        attempts.removeFirst()
    }
}

private actor PollCompletionGate {
    private var entered = false
    private var continuation: CheckedContinuation<CloudDeviceLoginPollState, Never>?

    func waitIgnoringCancellation() async -> CloudDeviceLoginPollState {
        entered = true
        return await withCheckedContinuation { continuation = $0 }
    }

    func hasEntered() -> Bool { entered }
    func complete(_ identity: CloudMachineIdentity) {
        continuation?.resume(returning: .complete(identity))
        continuation = nil
    }
}

@MainActor
private final class OpenRecorder {
    var urls: [URL] = []
    func open(_ url: URL) -> Bool { urls.append(url); return true }
}

@MainActor
private final class ModelBox {
    weak var value: CloudSettingsModel?
}

@MainActor
private final class ChangeRecorder {
    private(set) var count = 0
    func record() { count += 1 }
}

private struct CloudSettingsTests {
    static var checks = 0
    static var failures: [String] = []

    @MainActor
    static func check(_ name: String, _ condition: @autoclosure () -> Bool) {
        checks += 1
        if !condition() { failures.append(name) }
    }

    @MainActor
    static func eventually(_ predicate: @MainActor () -> Bool) async -> Bool {
        for _ in 0..<500 {
            if predicate() { return true }
            try? await Task.sleep(nanoseconds: 1_000_000)
        }
        return predicate()
    }

    static func eventuallyAsync(_ predicate: () async -> Bool) async -> Bool {
        for _ in 0..<500 {
            if await predicate() { return true }
            try? await Task.sleep(nanoseconds: 1_000_000)
        }
        return await predicate()
    }

    static let metadata = CloudMachineMetadata(
        name: "Test Mac", platform: "macos", appVersion: "1.2.3")
    static let identity = CloudMachineIdentity(accountID: "acct-1", machineID: "mac-1")
    static let verification = URL(string: "https://github.com/login/device?user_code=ABCD-EFGH")!

    @MainActor
    static func services(
        restored: CloudMachineIdentity? = nil,
        start: @escaping @Sendable () async throws -> CloudSettingsLoginAttempt,
        signOut: @escaping @MainActor () throws -> Void = {},
        opener: @escaping @MainActor (URL) -> Bool = { _ in true }
    ) -> CloudSettingsServices {
        CloudSettingsServices(
            restoredIdentity: { restored },
            startLogin: { _ in try await start() },
            signOut: signOut,
            openVerificationURL: opener)
    }

    static func attempt(
        code: String = "ABCD-EFGH",
        url: URL = verification,
        wait: @escaping @Sendable (@escaping @Sendable (CloudDeviceLoginPollState) async -> Void) async throws -> CloudDeviceLoginPollState
    ) -> CloudSettingsLoginAttempt {
        CloudSettingsLoginAttempt(userCode: code, verificationCompleteURL: url, wait: wait)
    }

    @MainActor
    static func testRestore() {
        let restored = CloudSettingsModel(
            services: services(restored: identity, start: { throw ForcedFailure.test }),
            metadata: metadata)
        check("restored identity is visible", restored.phase == .connected(identity: identity, origin: .restored))

        let signedOut = CloudSettingsModel(
            services: services(start: { throw ForcedFailure.test }), metadata: metadata)
        check("missing identity is visibly signed out", signedOut.phase == .signedOut)
    }

    @MainActor
    static func testConfirmationAndPolling() async {
        let poll = PollProbe()
        let recorder = OpenRecorder()
        let made = attempt { onState in try await poll.run(identity: identity, onState: onState) }
        let model = CloudSettingsModel(
            services: services(start: { made }, opener: { recorder.open($0) }), metadata: metadata)

        model.connect()
        guard await eventually({ model.phase == .code(userCode: "ABCD-EFGH") }) else {
            check("connect presents the one-time user code", false)
            return
        }
        check("verification page stays closed before confirmation", recorder.urls.isEmpty)
        let enteredBeforeConfirmation = await poll.hasEntered()
        check("polling stays stopped before confirmation", enteredBeforeConfirmation == false)

        model.confirmAndOpen()
        check("confirmation opens the API complete URL", recorder.urls == [verification])
        guard await eventually({ model.phase == .waiting(userCode: "ABCD-EFGH") }) else {
            check("confirmed flow shows waiting", false)
            return
        }
        await poll.advance()
        let sawSlowDown = await eventually({
            model.phase == .slowDown(userCode: "ABCD-EFGH", retryAfter: 7)
        })
        check("slow_down is visible", sawSlowDown)
        await poll.advance()
        let sawCompleted = await eventually({
            model.phase == .connected(identity: identity, origin: .completed)
        })
        check("completed identity is visible", sawCompleted)
    }

    @MainActor
    static func testProductionAdapterUsesCompleteURLAfterConfirmation() async {
        let plainURL = URL(string: "https://github.com/login/device")!
        let completeURL = URL(string: "https://github.com/login/device?user_code=PROD-CODE")!
        let transport = ProductionHTTPTransport(startResponse: """
        {
          "device_code": "private-device-code",
          "user_code": "PROD-CODE",
          "verification_uri": "\(plainURL.absoluteString)",
          "verification_uri_complete": "\(completeURL.absoluteString)",
          "expires_in": 600,
          "interval": 5
        }
        """)
        let key = try! CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 7, count: 32))
        let client = CloudAccountClient(
            apiBaseURL: URL(string: "https://api.example.test")!,
            transport: transport,
            credentialStore: CloudInMemoryKeyStore(),
            deviceKeyLoader: { key },
            sleeper: { _ in try await Task.sleep(nanoseconds: 60_000_000_000) },
            clock: { 100 })
        let recorder = OpenRecorder()
        let model = CloudSettingsModel(
            services: .production(client: client, openVerificationURL: { recorder.open($0) }),
            metadata: metadata)

        model.connect()
        guard await eventually({ model.phase == .code(userCode: "PROD-CODE") }) else {
            check("production adapter presents the server user code", false)
            return
        }
        check("production adapter does not open before confirmation", recorder.urls.isEmpty)
        let requestsBeforeConfirmation = await transport.paths()
        check("production adapter does not poll before confirmation",
              requestsBeforeConfirmation == ["/v1/auth/device/start"])

        model.confirmAndOpen()
        check("production adapter opens exactly verification_uri_complete",
              recorder.urls == [completeURL])
        model.cancel()
    }

    @MainActor
    static func terminalPhase(_ terminal: CloudDeviceLoginPollState) async -> CloudSettingsModel.Phase {
        let made = attempt { _ in terminal }
        let model = CloudSettingsModel(services: services(start: { made }), metadata: metadata)
        model.connect()
        guard await eventually({ if case .code = model.phase { return true }; return false }) else {
            return model.phase
        }
        model.confirmAndOpen()
        _ = await eventually({
            switch model.phase {
            case .waiting, .slowDown: return false
            default: return true
            }
        })
        return model.phase
    }

    @MainActor
    static func testTerminalAndErrorStates() async {
        let denied = await terminalPhase(.accessDenied)
        let expired = await terminalPhase(.expired)
        check("denial is visible", denied == .denied)
        check("expiry is visible", expired == .expired)

        let failed = CloudSettingsModel(
            services: services(start: { throw ForcedFailure.test }), metadata: metadata)
        failed.connect()
        let sawFailure = await eventually({ failed.phase == .failed(message: "forced failure") })
        check("start errors are visible", sawFailure)
    }

    @MainActor
    static func testStaleStartCannotPublish() async {
        let first = attempt(code: "OLD-CODE") { _ in .expired }
        let second = attempt(code: "NEW-CODE") { _ in .expired }
        let starter = SequencedStarter(first: first, second: second)
        let model = CloudSettingsModel(
            services: services(start: { await starter.start() }), metadata: metadata)

        model.connect()
        guard await eventuallyAsync({ await starter.count() == 1 }) else {
            check("first generation starts", false)
            return
        }
        model.retry()
        guard await eventually({ model.phase == .code(userCode: "NEW-CODE") }) else {
            check("replacement generation publishes", false)
            return
        }
        await starter.releaseFirst()
        try? await Task.sleep(nanoseconds: 10_000_000)
        check("old start result cannot overwrite replacement", model.phase == .code(userCode: "NEW-CODE"))
    }

    @MainActor
    static func testStalePollCannotPublish() async {
        let oldPoll = PollCompletionGate()
        let old = attempt(code: "OLD-CODE") { _ in await oldPoll.waitIgnoringCancellation() }
        let replacement = attempt(code: "NEW-CODE") { _ in .expired }
        let queue = AttemptQueue([old, replacement])
        let model = CloudSettingsModel(
            services: services(start: { await queue.next() }), metadata: metadata)

        model.connect()
        guard await eventually({ model.phase == .code(userCode: "OLD-CODE") }) else {
            check("old polling generation reaches code", false)
            return
        }
        model.confirmAndOpen()
        guard await eventuallyAsync({ await oldPoll.hasEntered() }) else {
            check("old polling generation starts", false)
            return
        }
        model.retry()
        guard await eventually({ model.phase == .code(userCode: "NEW-CODE") }) else {
            check("poll replacement generation publishes", false)
            return
        }
        await oldPoll.complete(identity)
        try? await Task.sleep(nanoseconds: 10_000_000)
        check("late old polling completion cannot overwrite replacement",
              model.phase == .code(userCode: "NEW-CODE"))
    }

    @MainActor
    static func testCancelAndCloseCancelPolling() async {
        for (name, action) in [
            ("cancel", { (model: CloudSettingsModel) in model.cancel() }),
            ("close", { (model: CloudSettingsModel) in model.close() }),
        ] {
            let probe = CancellationProbe()
            let made = attempt { _ in try await probe.run() }
            let model = CloudSettingsModel(services: services(start: { made }), metadata: metadata)
            model.connect()
            guard await eventually({ if case .code = model.phase { return true }; return false }) else {
                check("\(name) flow reaches code", false)
                continue
            }
            model.confirmAndOpen()
            guard await eventuallyAsync({ await probe.hasEntered() }) else {
                check("\(name) flow starts polling", false)
                continue
            }
            action(model)
            check("\(name) publishes cancelled", model.phase == .cancelled)
            let cancellationObserved = await eventuallyAsync({ await probe.wasCancelled() })
            check("\(name) cancels owned polling task", cancellationObserved)
        }
    }

    @MainActor
    static func testDeinitCancelsOwnedPollingWithoutLatePublication() async {
        let probe = DeinitPollProbe()
        let made = attempt { _ in await probe.run() }
        let recorder = ChangeRecorder()
        var model: CloudSettingsModel? = CloudSettingsModel(
            services: services(start: { made }), metadata: metadata)
        let box = ModelBox()
        box.value = model
        model?.onChange = { recorder.record() }

        model?.connect()
        guard await eventually({ model?.phase == .code(userCode: "ABCD-EFGH") }) else {
            check("deinit polling flow reaches code", false)
            return
        }
        model?.confirmAndOpen()
        guard await eventuallyAsync({ await probe.hasEntered() }) else {
            check("deinit polling flow enters polling", false)
            return
        }

        model = nil
        check("model releases while polling is in flight", box.value == nil)
        let cancellationObserved = await eventuallyAsync({ await probe.wasCancelled() })
        check("deinit cancels the owned polling task", cancellationObserved)
        let publicationsAfterRelease = recorder.count
        await probe.complete(identity)
        try? await Task.sleep(nanoseconds: 10_000_000)
        check("poll completion cannot publish after model deinit",
              recorder.count == publicationsAfterRelease)
    }

    @MainActor
    static func testDeinitCancelsOwnedStartWithoutLatePublication() async {
        let probe = DeinitStartProbe()
        let made = attempt { _ in .expired }
        let recorder = ChangeRecorder()
        var model: CloudSettingsModel? = CloudSettingsModel(
            services: services(start: { await probe.run() }), metadata: metadata)
        let box = ModelBox()
        box.value = model
        model?.onChange = { recorder.record() }

        model?.connect()
        guard await eventuallyAsync({ await probe.hasEntered() }) else {
            check("deinit start flow enters start", false)
            return
        }

        model = nil
        check("model releases while start is in flight", box.value == nil)
        let cancellationObserved = await eventuallyAsync({ await probe.wasCancelled() })
        check("deinit cancels the owned start task", cancellationObserved)
        let publicationsAfterRelease = recorder.count
        await probe.complete(made)
        try? await Task.sleep(nanoseconds: 10_000_000)
        check("start completion cannot publish after model deinit",
              recorder.count == publicationsAfterRelease)
    }

    @MainActor
    static func testSignOutOrdering() {
        let box = ModelBox()
        var fail = true
        var observed: [CloudSettingsModel.Phase] = []
        let model = CloudSettingsModel(
            services: services(restored: identity, start: { throw ForcedFailure.test }, signOut: {
                if let phase = box.value?.phase { observed.append(phase) }
                if fail { throw ForcedFailure.test }
            }), metadata: metadata)
        box.value = model

        model.signOut()
        check("sign-out failure keeps identity visible", model.phase == .signOutFailed(identity: identity, message: "forced failure"))
        check("signed-out state is not published before credential removal", observed == [.connected(identity: identity, origin: .restored)])

        fail = false
        model.signOut()
        check("successful credential removal publishes signed out", model.phase == .signedOut)
    }

    @MainActor
    static func run() async throws -> Int {
        testRestore()
        await testConfirmationAndPolling()
        await testProductionAdapterUsesCompleteURLAfterConfirmation()
        await testTerminalAndErrorStates()
        await testStaleStartCannotPublish()
        await testStalePollCannotPublish()
        await testCancelAndCloseCancelPolling()
        await testDeinitCancelsOwnedPollingWithoutLatePublication()
        await testDeinitCancelsOwnedStartWithoutLatePublication()
        testSignOutOrdering()

        guard failures.isEmpty else {
            throw CloudSettingsTestFailure(failures: failures, checks: checks)
        }
        return checks
    }
}

private struct CloudSettingsTestFailure: Error, CustomStringConvertible {
    let failures: [String]
    let checks: Int

    var description: String {
        "CloudSettingsTests: \(failures.count)/\(checks) failed — "
            + failures.joined(separator: "; ")
    }
}

func runCloudSettingsTests() async throws -> Int {
    try await CloudSettingsTests.run()
}
