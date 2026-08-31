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

private final class CancellationOrderingRecorder: @unchecked Sendable {
    private let lock = NSLock()
    private var values: [String] = []

    func record(_ value: String) {
        lock.lock(); values.append(value); lock.unlock()
    }

    func snapshot() -> [String] {
        lock.lock(); defer { lock.unlock() }; return values
    }
}

private final class CancellationRaceWaiter: @unchecked Sendable {
    private let lock = NSLock()
    private var entered = false
    private var continuation: CheckedContinuation<CloudDeviceLoginPollState, Never>?

    func wait(recorder: CancellationOrderingRecorder) async -> CloudDeviceLoginPollState {
        await withTaskCancellationHandler {
            await withCheckedContinuation { continuation in
                lock.lock()
                entered = true
                self.continuation = continuation
                lock.unlock()
            }
        } onCancel: {
            recorder.record("cancel-task")
        }
    }

    func hasEntered() -> Bool {
        lock.lock(); defer { lock.unlock() }; return entered
    }

    /// Resume the ignored-cancellation transport answer synchronously. The task's MainActor
    /// publication may run later, but the answer has already landed in the reservation/cancel gap.
    func complete(_ identity: CloudMachineIdentity) {
        lock.lock()
        let continuation = self.continuation
        self.continuation = nil
        lock.unlock()
        continuation?.resume(returning: .complete(identity))
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
        reserveCredentialInvalidation: @escaping @MainActor () -> CloudCredentialGeneration = {
            CloudCredentialGeneration(0)
        },
        signOut: @escaping @MainActor (
            CloudCredentialGeneration,
            @escaping @MainActor (CloudKeychainWriteOutcome) -> Void
        ) -> Void = { _, completion in completion(.succeeded) },
        abandonPendingLogins: @escaping @MainActor (
            CloudCredentialGeneration,
            @escaping @MainActor (CloudKeychainWriteOutcome) -> Void
        ) -> Void = { _, completion in completion(.succeeded) },
        opener: @escaping @MainActor (URL) -> Bool = { _ in true }
    ) -> CloudSettingsServices {
        CloudSettingsServices(
            restoredIdentity: { restored },
            startLogin: { _ in try await start() },
            reserveCredentialInvalidation: reserveCredentialInvalidation,
            signOut: signOut,
            abandonPendingLogins: abandonPendingLogins,
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

        var restoration: (@MainActor (Result<CloudMachineIdentity?, Error>) -> Void)?
        let reconciling = CloudSettingsModel(
            services: CloudSettingsServices(
                readRestoredIdentity: { restoration = $0 },
                startLogin: { _ in throw ForcedFailure.test },
                reserveCredentialInvalidation: { CloudCredentialGeneration(0) },
                signOut: { _, completion in completion(.succeeded) },
                abandonPendingLogins: { _, completion in completion(.succeeded) },
                openVerificationURL: { _ in true }),
            metadata: metadata)
        restoration?(.failure(CloudKeyError.operationTimedOut(seconds: 1)))
        check("restoration timeout is unknown progress rather than signed out",
              reconciling.phase == .restorationReconciliation(
                message: CloudKeyError.operationTimedOut(seconds: 1).errorDescription!))
        restoration?(.success(identity))
        check("restoration retains its late terminal identity for reconciliation",
              reconciling.phase == .connected(identity: identity, origin: .restored))
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
            services: services(
                restored: identity, start: { throw ForcedFailure.test },
                signOut: { _, completion in
                    if let phase = box.value?.phase { observed.append(phase) }
                    completion(fail ? .failed(ForcedFailure.test) : .succeeded)
                }),
            metadata: metadata)
        box.value = model

        model.signOut()
        check("sign-out failure keeps identity visible", model.phase == .signOutFailed(identity: identity, message: "forced failure"))
        check("signed-out state is not published before credential removal",
              observed == [.signingOut(identity: identity)])

        fail = false
        model.signOut()
        check("successful credential removal publishes signed out", model.phase == .signedOut)
    }

    /// The sign-out half of the Keychain-write boundary: that the window is never blocked
    /// waiting for the store, that an answer which never comes still reaches the person, and
    /// that a login already in flight is abandoned before the credential is removed.
    @MainActor
    static func testBoundedSignOutCannotFreezeOrResurrect() {
        var deferred: (@MainActor (CloudKeychainWriteOutcome) -> Void)?
        var signOutCalls = 0
        var connectionChanges = 0
        let model = CloudSettingsModel(
            services: services(
                restored: identity, start: { throw ForcedFailure.test },
                signOut: { _, completion in
                    signOutCalls += 1
                    deferred = completion
                }),
            metadata: metadata)
        let previousConnectionChange = CloudSettingsModel.onConnectionChange
        var connectionTruth: [Bool] = []
        CloudSettingsModel.onConnectionChange = { connected in
            connectionChanges += 1
            connectionTruth.append(connected)
        }
        defer { CloudSettingsModel.onConnectionChange = previousConnectionChange }

        model.signOut()
        check("a sign-out that has not answered publishes its own phase, not silence",
              model.phase == .signingOut(identity: identity))
        check("sign-out owns one durable invalidation-and-removal attempt", signOutCalls == 1)
        check("reservation detaches the bridge before persistence or removal answers",
              connectionChanges == 1 && connectionTruth == [false]
                  && model.connectedIdentity == nil)

        // Timeout is progress: Security is still running and may already have removed the item.
        // The bridge stays attached until the retained terminal result settles that uncertainty.
        deferred?(.timedOut(seconds: CloudKeychainWriter.defaultTimeoutSeconds))
        let timedOutMessage = CloudKeychainWriteOutcome
            .timedOut(seconds: CloudKeychainWriter.defaultTimeoutSeconds).message
        check("a timed-out removal is reported as unknown while reconciliation continues",
              model.phase == .signOutReconciliation(identity: identity, message: timedOutMessage))
        check("the timeout message names unknown state and retained reconciliation",
              timedOutMessage.contains("unknown") && timedOutMessage.contains("reconcil"))
        check("timeout stays detached instead of publishing a second connection transition",
              connectionChanges == 1 && connectionTruth == [false]
                  && model.connectedIdentity == nil)

        deferred?(.succeeded)
        check("a late terminal success completes sign-out without a retry",
              model.phase == .signedOut)

        // The same invariant with a retry in flight: either removal can settle the shared store.
        // A late success from the first attempt must not be discarded merely because a second
        // attempt has started.
        let retryModel = CloudSettingsModel(
            services: services(
                restored: identity, start: { throw ForcedFailure.test },
                signOut: { _, completion in signOutCalls += 1; deferred = completion }),
            metadata: metadata)
        retryModel.signOut()
        let firstAttempt = deferred
        firstAttempt?(.timedOut(seconds: CloudKeychainWriter.defaultTimeoutSeconds))
        check("a timed-out attempt offers retry without claiming failure",
              retryModel.phase == .signOutReconciliation(identity: identity, message: timedOutMessage))
        retryModel.signOut()
        let secondAttempt = deferred
        check("a retry re-enters the busy phase", retryModel.phase == .signingOut(identity: identity))
        firstAttempt?(.succeeded)
        check("the first removal's late success settles the retrying model and store",
              retryModel.phase == .signedOut)
        secondAttempt?(.succeeded)
        check("a redundant terminal answer leaves the settled model signed out",
              retryModel.phase == .signedOut)
        check("each removal owns exactly one durable invalidation reservation", signOutCalls == 3)

        var reversedAnswers: [(@MainActor (CloudKeychainWriteOutcome) -> Void)] = []
        let reversedModel = CloudSettingsModel(
            services: services(
                restored: identity, start: { throw ForcedFailure.test },
                signOut: { _, completion in reversedAnswers.append(completion) }),
            metadata: metadata)
        reversedModel.signOut()
        reversedAnswers[0](.timedOut(seconds: 1))
        reversedModel.signOut()
        reversedAnswers[1](.failed(
            CloudAccountError.credentialCleanupPending("forced cleanup failure")))
        check("a retry failure waits while the timed-out original is still authoritative",
              reversedModel.phase == .signOutReconciliation(
                identity: identity,
                message: "A retry failed, but an earlier Keychain removal is still running; "
                    + "its terminal result remains authoritative."))
        reversedAnswers[0](.succeeded)
        check("the original removal's later success settles after the retry failed first",
              reversedModel.phase == .signedOut)

        var nextReservation: UInt64 = 70
        var invalidationReservations: [CloudCredentialGeneration] = []
        var invalidationAnswers: [(@MainActor (CloudKeychainWriteOutcome) -> Void)] = []
        let invalidationModel = CloudSettingsModel(
            services: services(
                restored: identity, start: { throw ForcedFailure.test },
                reserveCredentialInvalidation: {
                    nextReservation += 1
                    return CloudCredentialGeneration(nextReservation)
                },
                signOut: { reservation, completion in
                    invalidationReservations.append(reservation)
                    invalidationAnswers.append(completion)
                }),
            metadata: metadata)
        invalidationModel.signOut()
        let invalidationError = CloudAccountError
            .credentialInvalidationFailed("forced persistence failure")
        invalidationAnswers[0](.failed(invalidationError))
        check("durable invalidation failure is neither connected nor signed out",
              invalidationModel.phase == .signOutInvalidationPending(
                identity: identity, message: invalidationError.errorDescription!)
                && invalidationModel.connectedIdentity == nil)
        check("invalidation failure copy names restart risk and explicit retry ownership",
              invalidationError.errorDescription!.contains("restart")
                && invalidationError.errorDescription!.contains("retry"))
        invalidationModel.signOut()
        check("retry persists the same already-reserved invalidation epoch",
              invalidationReservations == [CloudCredentialGeneration(71),
                                           CloudCredentialGeneration(71)])
        invalidationAnswers[1](.succeeded)
        check("successful retry closes invalidation-pending state",
              invalidationModel.phase == .signedOut)
    }

    /// Cancelling a sign-in must abandon it at the credential store too. A cancelled `Task` is a
    /// request; a transport that ignores it still returns a credential somebody has to refuse.
    @MainActor
    static func testCancelAbandonsPendingLogins() async {
        var abandonCalls = 0
        var invalidationAnswers: [(@MainActor (CloudKeychainWriteOutcome) -> Void)] = []
        let gate = PollCompletionGate()
        let model = CloudSettingsModel(
            services: services(
                start: {
                    attempt(wait: { _ in await gate.waitIgnoringCancellation() })
                },
                abandonPendingLogins: { _, completion in
                    abandonCalls += 1
                    invalidationAnswers.append(completion)
                }),
            metadata: metadata)
        model.connect()
        guard await eventually({ model.phase == .code(userCode: "ABCD-EFGH") }) else {
            check("cancel test reaches the code phase", false)
            return
        }
        model.confirmAndOpen()
        guard await eventuallyAsync({ await gate.hasEntered() }) else {
            check("cancel test reaches the waiting poll", false)
            return
        }
        check("no login is abandoned while one is legitimately in flight", abandonCalls == 0)
        model.cancel()
        check("cancelling abandons the sign-in at the credential store", abandonCalls == 1)
        check("durable invalidation has an observable in-progress phase", model.phase == .cancelling)
        invalidationAnswers[0](.timedOut(seconds: 1))
        check("invalidation timeout remains unknown while its terminal answer is retained",
              model.phase == .cancellationReconciliation(
                message: CloudKeychainWriteOutcome.timedOut(seconds: 1).message))
        model.retry()
        check("failed or unknown cancellation owns an explicit retry", abandonCalls == 2
              && model.phase == .cancelling)
        invalidationAnswers[1](.failed(
            CloudAccountError.credentialInvalidationFailed("forced retry failure")))
        check("cancellation retry failure waits for the timed-out original terminal",
              model.phase == .cancellationReconciliation(
                message: "A retry failed, but an earlier invalidation is still running; "
                    + "its terminal result remains authoritative."))
        invalidationAnswers[0](.succeeded)
        check("the first invalidation's late success settles a retrying cancellation",
              model.phase == .cancelled)
        invalidationAnswers[1](.succeeded)
        check("a redundant invalidation answer cannot undo cancellation", model.phase == .cancelled)

        // The precise UI ordering seam: an ignored-cancellation completion is released while
        // reserveCredentialInvalidation is still on the stack. Reservation must already exist
        // before Task.cancel() invokes its synchronous cancellation handler.
        let ordering = CancellationOrderingRecorder()
        let race = CancellationRaceWaiter()
        var reservedForPersistence: CloudCredentialGeneration?
        let racingModel = CloudSettingsModel(
            services: services(
                start: {
                    attempt(wait: { _ in await race.wait(recorder: ordering) })
                },
                reserveCredentialInvalidation: {
                    ordering.record("reserve")
                    race.complete(identity)
                    return CloudCredentialGeneration(41)
                },
                abandonPendingLogins: { reservation, completion in
                    ordering.record("persist")
                    reservedForPersistence = reservation
                    completion(.succeeded)
                }),
            metadata: metadata)
        racingModel.connect()
        guard await eventually({ racingModel.phase == .code(userCode: "ABCD-EFGH") }) else {
            check("ordering test reaches the code phase", false)
            return
        }
        racingModel.confirmAndOpen()
        guard await eventually({ race.hasEntered() }) else {
            check("ordering test reaches the cancellable wait", false)
            return
        }
        racingModel.cancel()
        let events = ordering.snapshot()
        check("invalidation reserves before cancelling the in-flight Task",
              events.first == "reserve"
                  && events.firstIndex(of: "reserve")! < events.firstIndex(of: "cancel-task")!)
        check("the exact synchronous reservation is handed to durable persistence",
              reservedForPersistence == CloudCredentialGeneration(41)
                  && events.last == "persist")
        await Task.yield()
        check("a completion released in the reservation/cancel gap cannot reconnect the model",
              racingModel.phase == .cancelled)
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
        testBoundedSignOutCannotFreezeOrResurrect()
        await testCancelAbandonsPendingLogins()

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
