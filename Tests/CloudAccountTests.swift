import Foundation
import Darwin

private struct CloudAccountTestFailure: Error, CustomStringConvertible {
    let description: String
}

private actor CloudAccountFakeHTTP: CloudAccountHTTPTransport {
    struct Stub: Sendable {
        let status: Int
        let data: Data
        let blocked: Bool
    }

    private var stubs: [Stub] = []
    private var requests: [URLRequest] = []
    private var repeatedStub: Stub?
    private var blockedResult: CheckedContinuation<(Data, HTTPURLResponse), Never>?
    private var blockedPayload: (Data, HTTPURLResponse)?
    private var blockedWaiters: [CheckedContinuation<Void, Never>] = []

    func enqueue(status: Int = 200, json: String) {
        stubs.append(Stub(status: status, data: Data(json.utf8), blocked: false))
    }

    func enqueueBlocked(status: Int, json: String) {
        stubs.append(Stub(status: status, data: Data(json.utf8), blocked: true))
    }

    func repeatResponse(status: Int = 200, json: String) {
        repeatedStub = Stub(status: status, data: Data(json.utf8), blocked: false)
    }

    func waitUntilBlocked() async {
        if blockedResult != nil { return }
        await withCheckedContinuation { blockedWaiters.append($0) }
    }

    func resumeBlocked() {
        if let continuation = blockedResult, let payload = blockedPayload {
            continuation.resume(returning: payload)
        }
        blockedResult = nil
        blockedPayload = nil
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        guard !stubs.isEmpty || repeatedStub != nil else {
            throw CloudAccountTestFailure(description: "unexpected request: \(request.url?.path ?? "")")
        }
        requests.append(request)
        let stub = stubs.isEmpty ? repeatedStub! : stubs.removeFirst()
        let response = HTTPURLResponse(
            url: request.url!, statusCode: stub.status,
            httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"]
        )!
        if stub.blocked {
            return await withCheckedContinuation { continuation in
                blockedResult = continuation
                blockedPayload = (stub.data, response)
                let waiters = blockedWaiters
                blockedWaiters.removeAll()
                waiters.forEach { $0.resume() }
            }
        }
        return (stub.data, response)
    }

    func captured() -> [URLRequest] { requests }
}

private actor CloudAccountTestSleeps {
    private var values: [TimeInterval] = []
    func append(_ value: TimeInterval) { values.append(value) }
    func all() -> [TimeInterval] { values }
}

private actor CloudAccountControlledSleeper {
    private var entered = false
    private var enteredWaiters: [CheckedContinuation<Void, Never>] = []
    private var resumeContinuation: CheckedContinuation<Void, Never>?

    func sleep() async {
        entered = true
        let waiters = enteredWaiters
        enteredWaiters.removeAll()
        waiters.forEach { $0.resume() }
        await withCheckedContinuation { resumeContinuation = $0 }
    }

    func waitUntilEntered() async {
        if entered { return }
        await withCheckedContinuation { enteredWaiters.append($0) }
    }

    func resume() {
        resumeContinuation?.resume()
        resumeContinuation = nil
    }
}

private final class CloudAccountTestClock: @unchecked Sendable {
    private let lock = NSLock()
    private var value: TimeInterval
    private var sleeps: [TimeInterval] = []

    init(_ value: TimeInterval) { self.value = value }

    func now() -> TimeInterval {
        lock.lock()
        defer { lock.unlock() }
        return value
    }

    func advance(_ amount: TimeInterval, recordSleep: Bool = false) {
        lock.lock()
        value += amount
        if recordSleep { sleeps.append(amount) }
        lock.unlock()
    }

    func recordedSleeps() -> [TimeInterval] {
        lock.lock()
        defer { lock.unlock() }
        return sleeps
    }
}

private final class CloudAccountBlockingKeyLoader: @unchecked Sendable {
    private let condition = NSCondition()
    private let key: CloudDeviceKeyPair
    private var active = 0
    private var maximum = 0
    private var calls = 0

    init(key: CloudDeviceKeyPair) { self.key = key }

    func load() -> CloudDeviceKeyPair {
        condition.lock()
        calls += 1
        let call = calls
        active += 1
        maximum = max(maximum, active)
        if call == 1 && active == 1 {
            let deadline = Date().addingTimeInterval(0.2)
            while active == 1 && condition.wait(until: deadline) {}
        } else {
            condition.broadcast()
        }
        active -= 1
        condition.unlock()
        return key
    }

    func maximumConcurrentLoads() -> Int {
        condition.lock()
        defer { condition.unlock() }
        return maximum
    }
}

/// A deliberately blocking synchronous store makes overlap observable without data-racing itself.
private final class CloudAccountBlockingStore: CloudKeyStoring, @unchecked Sendable {
    let coordinator = CloudKeyStoreCoordinator()
    private let condition = NSCondition()
    private var values: [String: Data] = [:]
    private var active = 0
    private var maximum = 0
    private var calls = 0

    func data(for account: String) throws -> Data? {
        withAccess { $0[account] }
    }

    func set(_ data: Data, for account: String) throws {
        withAccess { $0[account] = data }
    }

    func remove(_ account: String) throws {
        _ = withAccess { $0.removeValue(forKey: account) }
    }

    func resetMetrics() {
        condition.lock()
        calls = 0
        maximum = 0
        condition.unlock()
    }

    func maximumConcurrentAccesses() -> Int {
        condition.lock()
        defer { condition.unlock() }
        return maximum
    }

    private func withAccess<T>(_ body: (inout [String: Data]) -> T) -> T {
        condition.lock()
        calls += 1
        let call = calls
        active += 1
        maximum = max(maximum, active)
        if call == 1 && active == 1 {
            let deadline = Date().addingTimeInterval(0.2)
            while active == 1 && condition.wait(until: deadline) {}
        } else {
            condition.broadcast()
        }
        let result = body(&values)
        active -= 1
        condition.unlock()
        return result
    }
}

/// Forces two initial device-key reads to observe the same missing value unless their shared
/// persistence boundary serializes the complete load-or-create transaction.
private final class CloudAccountRacingDeviceKeyStore: CloudKeyStoring, @unchecked Sendable {
    let coordinator = CloudKeyStoreCoordinator()
    private let condition = NSCondition()
    private var values: [String: Data] = [:]
    private var missingDeviceReads = 0
    private var deviceWrites = 0

    func data(for account: String) throws -> Data? {
        condition.lock()
        let snapshot = values[account]
        if account == CloudKeys.deviceKeyAccount, snapshot == nil {
            missingDeviceReads += 1
            if missingDeviceReads == 1 {
                let deadline = Date().addingTimeInterval(0.2)
                while missingDeviceReads == 1 && condition.wait(until: deadline) {}
            } else {
                condition.broadcast()
            }
        }
        condition.unlock()
        return snapshot
    }

    func set(_ data: Data, for account: String) throws {
        condition.lock()
        values[account] = data
        if account == CloudKeys.deviceKeyAccount { deviceWrites += 1 }
        condition.unlock()
    }

    func remove(_ account: String) throws {
        condition.lock()
        values.removeValue(forKey: account)
        condition.unlock()
    }

    func deviceWriteCount() -> Int {
        condition.lock()
        defer { condition.unlock() }
        return deviceWrites
    }
}

/// Returns an old credential snapshot, then pauses that read. Another client can replace the
/// value before the stale client performs its conditional remove unless both use one coordinator.
private final class CloudAccountInterleavingStore: CloudKeyStoring, @unchecked Sendable {
    let coordinator = CloudKeyStoreCoordinator()
    private let condition = NSCondition()
    private var values: [String: Data] = [:]
    private var blockNextCredentialRead = false
    private var credentialReadBlocked = false
    private var resumeCredentialRead = false

    func data(for account: String) throws -> Data? {
        condition.lock()
        let snapshot = values[account]
        if account == CloudAccountClient.machineCredentialAccount, blockNextCredentialRead {
            blockNextCredentialRead = false
            credentialReadBlocked = true
            condition.broadcast()
            let deadline = Date().addingTimeInterval(0.3)
            while !resumeCredentialRead && condition.wait(until: deadline) {}
        }
        condition.unlock()
        return snapshot
    }

    func set(_ data: Data, for account: String) throws {
        condition.lock()
        values[account] = data
        condition.unlock()
    }

    func remove(_ account: String) throws {
        condition.lock()
        values.removeValue(forKey: account)
        condition.unlock()
    }

    func armCredentialReadBlock() {
        condition.lock()
        blockNextCredentialRead = true
        credentialReadBlocked = false
        resumeCredentialRead = false
        condition.unlock()
    }

    func waitUntilCredentialReadBlocked() {
        condition.lock()
        let deadline = Date().addingTimeInterval(1)
        while !credentialReadBlocked && condition.wait(until: deadline) {}
        condition.unlock()
    }

    func resumeBlockedCredentialRead() {
        condition.lock()
        resumeCredentialRead = true
        condition.broadcast()
        condition.unlock()
    }
}

private func cloudAccountBody(_ request: URLRequest) throws -> [String: Any] {
    guard let data = request.httpBody,
          let object = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
        throw CloudAccountTestFailure(description: "request has no JSON object body")
    }
    return object
}

private func cloudAccountRequest(
    _ requests: [URLRequest], path: String, occurrence: Int = 0
) throws -> URLRequest {
    let matching = requests.filter { $0.url?.path == path }
    guard matching.indices.contains(occurrence) else {
        throw CloudAccountTestFailure(description: "missing request \(path) #\(occurrence)")
    }
    return matching[occurrence]
}

private let cloudAccountRegressionModeKey = "CLAWDLINE_CLOUD_ACCOUNT_REGRESSION_MODE"

/// Coordinates a deliberate AB/BA ordering without a fail-open timeout. The scenario always runs
/// in a subprocess, so its parent can kill the complete deadlocked process at a hard deadline.
private final class CloudAccountCrossedLockBarrier: @unchecked Sendable {
    private let condition = NSCondition()
    private var keyCoordinatorLocked = false
    private var keyLoaderEntered = false

    func markKeyCoordinatorLocked() {
        condition.lock()
        keyCoordinatorLocked = true
        condition.broadcast()
        condition.unlock()
    }

    func waitUntilKeyCoordinatorLocked() {
        condition.lock()
        while !keyCoordinatorLocked { condition.wait() }
        condition.unlock()
    }

    func markKeyLoaderEntered() {
        condition.lock()
        keyLoaderEntered = true
        condition.broadcast()
        condition.unlock()
    }

    func waitUntilKeyLoaderEntered() {
        condition.lock()
        while !keyLoaderEntered { condition.wait() }
        condition.unlock()
    }
}

private func cloudAccountRunCrossedLockScenario() async throws {
    let credentialStore = CloudInMemoryKeyStore()
    let deviceKeyStore = CloudInMemoryKeyStore()
    let deviceKeys = CloudKeys(store: deviceKeyStore)
    let barrier = CloudAccountCrossedLockBarrier()
    let fake = CloudAccountFakeHTTP()
    let client = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test/root")!,
        transport: fake,
        credentialStore: credentialStore,
        deviceKeyLoader: {
            barrier.markKeyLoaderEntered()
            return try deviceKeys.loadOrCreateDeviceKeyPair()
        }
    )

    await fake.enqueue(json: """
    {"status":"complete","account_id":"acct-crossed-lock","machine_id":"machine-crossed-lock",
     "machine_credential":"crossed-lock-bearer-fixture"}
    """)
    _ = try await client.pollDeviceLogin(deviceCode: "crossed-lock-credential")
    await fake.enqueue(json: """
    {"device_code":"crossed-lock-device","user_code":"LOCK-0001",
     "verification_uri":"https://clawdline.com/connect",
     "verification_uri_complete":"https://clawdline.com/connect?user_code=LOCK-0001",
     "expires_in":600,"interval":5}
    """)

    let reverseOrder = Task.detached {
        try deviceKeyStore.coordinator.withCriticalRegion {
            barrier.markKeyCoordinatorLocked()
            barrier.waitUntilKeyLoaderEntered()
            _ = try client.authorizationHeader()
        }
    }
    barrier.waitUntilKeyCoordinatorLocked()
    _ = try await client.startDeviceLogin(
        metadata: CloudMachineMetadata(name: "Crossed Lock Mac", platform: "macOS"))
    try await reverseOrder.value
}

private func cloudAccountCrossedLockSubprocessCompletes(
    within timeout: TimeInterval = 2
) throws -> Bool {
    let executable = URL(
        fileURLWithPath: CommandLine.arguments[0],
        relativeTo: URL(fileURLWithPath: FileManager.default.currentDirectoryPath)
    ).standardizedFileURL
    let process = Process()
    process.executableURL = executable
    var environment = ProcessInfo.processInfo.environment
    environment[cloudAccountRegressionModeKey] = "crossed-lock"
    process.environment = environment
    process.standardOutput = FileHandle.nullDevice
    process.standardError = FileHandle.nullDevice
    try process.run()

    let deadline = Date().addingTimeInterval(timeout)
    while process.isRunning && Date() < deadline {
        Thread.sleep(forTimeInterval: 0.01)
    }
    guard process.isRunning else {
        process.waitUntilExit()
        return process.terminationReason == .exit && process.terminationStatus == 0
    }

    process.terminate()
    let terminationDeadline = Date().addingTimeInterval(1)
    while process.isRunning && Date() < terminationDeadline {
        Thread.sleep(forTimeInterval: 0.01)
    }
    if process.isRunning { kill(process.processIdentifier, SIGKILL) }
    process.waitUntilExit()
    return false
}

private func cloudAccountRunSameMachineRevokeABAScenario() async throws {
    let store = CloudInMemoryKeyStore()
    let oldFake = CloudAccountFakeHTTP()
    let replacementFake = CloudAccountFakeHTTP()
    let key = try CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 0x57, count: 32))
    let oldClient = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test/root")!,
        transport: oldFake,
        credentialStore: store,
        deviceKeyLoader: { key }
    )
    let replacementClient = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test/root")!,
        transport: replacementFake,
        credentialStore: store,
        deviceKeyLoader: { key }
    )

    await oldFake.enqueue(json: """
    {"status":"complete","account_id":"acct-revoke-aba","machine_id":"machine-revoke-aba",
     "machine_credential":"revoke-credential-a"}
    """)
    _ = try await oldClient.pollDeviceLogin(deviceCode: "revoke-credential-a")
    await oldFake.enqueueBlocked(status: 200, json: """
    {"revoked_at":"2026-08-27T09:04:00.000Z","routing":"stopped",
     "content_key_rotation":"lazy","note":"Routing stops now."}
    """)
    let staleRevoke = Task {
        try await oldClient.revokeMachine(
            id: "machine-revoke-aba", browserSessionCookie: "cookie")
    }
    await oldFake.waitUntilBlocked()

    await replacementFake.enqueue(json: """
    {"status":"complete","account_id":"acct-revoke-aba","machine_id":"machine-revoke-aba",
     "machine_credential":"revoke-credential-b"}
    """)
    _ = try await replacementClient.pollDeviceLogin(deviceCode: "revoke-credential-b")
    await oldFake.resumeBlocked()
    _ = try await staleRevoke.value
    guard try replacementClient.authorizationHeader() == "Bearer revoke-credential-b" else {
        throw CloudAccountTestFailure(
            description: "a stale revoke removed the replacement credential for the same machine")
    }
}

func runCloudAccountRegressionScenario(mode: String) async throws {
    switch mode {
    case "crossed-lock":
        try await cloudAccountRunCrossedLockScenario()
    case "revoke-aba":
        try await cloudAccountRunSameMachineRevokeABAScenario()
    default:
        throw CloudAccountTestFailure(description: "unknown CloudAccount regression mode: \(mode)")
    }
}

func runCloudAccountTests() async throws -> Int {
    switch ProcessInfo.processInfo.environment[cloudAccountRegressionModeKey] {
    case "crossed-lock":
        try await cloudAccountRunCrossedLockScenario()
        exit(EXIT_SUCCESS)
    case "revoke-aba":
        try await cloudAccountRunSameMachineRevokeABAScenario()
        exit(EXIT_SUCCESS)
    default:
        break
    }

    var checks = 0
    func require(_ condition: Bool, _ message: String) throws {
        checks += 1
        if !condition { throw CloudAccountTestFailure(description: message) }
    }

    try require(try cloudAccountCrossedLockSubprocessCompletes(),
                "startDeviceLogin makes progress under a forced credential/key crossed-lock order")
    try await cloudAccountRunSameMachineRevokeABAScenario()
    try require(true,
                "a stale revoke cannot remove replacement credentials for the same machine ID")

    let fake = CloudAccountFakeHTTP()
    let store = CloudInMemoryKeyStore()
    let key = try CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 0x31, count: 32))
    let sleeps = CloudAccountTestSleeps()
    let client = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test/root")!,
        transport: fake,
        credentialStore: store,
        deviceKeyLoader: { key },
        sleeper: { seconds in await sleeps.append(seconds) }
    )

    await fake.enqueue(json: """
    {"device_code":"dev-secret","user_code":"ABCD-EFGH",
     "verification_uri":"https://clawdline.com/connect",
     "verification_uri_complete":"https://clawdline.com/connect?user_code=ABCD-EFGH",
     "expires_in":600,"interval":5}
    """)
    let started = try await client.startDeviceLogin(
        metadata: CloudMachineMetadata(name: "Studio Mac", platform: "macOS", appVersion: nil))
    try require(started.deviceCode == "dev-secret" && started.interval == 5,
                "device start decodes the deployed response")
    try require(!String(describing: started).contains("dev-secret"),
                "device login descriptions redact the device code")
    var requests = await fake.captured()
    let startRequest = try cloudAccountRequest(requests, path: "/root/v1/auth/device/start")
    let startBody = try cloudAccountBody(startRequest)
    try require(startRequest.httpMethod == "POST", "device start uses POST")
    try require(Set(startBody.keys) == ["name", "platform", "app_version", "public_key"],
                "device start sends exactly the deployed fields")
    try require(startBody["name"] as? String == "Studio Mac"
                && startBody["platform"] as? String == "macOS"
                && startBody["app_version"] is NSNull,
                "device start sends machine metadata including explicit null app version")
    try require(startBody["public_key"] as? String == key.publicKeyRaw.base64EncodedString(),
                "device start carries the existing Ed25519 public key")
    try require(startRequest.value(forHTTPHeaderField: "Authorization") == nil,
                "device start is unauthenticated")

    let concurrentFake = CloudAccountFakeHTTP()
    let racingDeviceKeyStore = CloudAccountRacingDeviceKeyStore()
    let firstConcurrentKeys = CloudKeys(store: racingDeviceKeyStore)
    let secondConcurrentKeys = CloudKeys(store: racingDeviceKeyStore)
    let firstConcurrentClient = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test/root")!,
        transport: concurrentFake,
        credentialStore: racingDeviceKeyStore,
        deviceKeyLoader: { try firstConcurrentKeys.loadOrCreateDeviceKeyPair() }
    )
    let secondConcurrentClient = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test/root")!,
        transport: concurrentFake,
        credentialStore: racingDeviceKeyStore,
        deviceKeyLoader: { try secondConcurrentKeys.loadOrCreateDeviceKeyPair() }
    )
    let concurrentStartJSON = """
    {"device_code":"concurrent-secret","user_code":"CONC-0001",
     "verification_uri":"https://clawdline.com/connect",
     "verification_uri_complete":"https://clawdline.com/connect?user_code=CONC-0001",
     "expires_in":600,"interval":5}
    """
    await concurrentFake.enqueue(json: concurrentStartJSON)
    await concurrentFake.enqueue(json: concurrentStartJSON)
    async let firstStart = firstConcurrentClient.startDeviceLogin(
        metadata: CloudMachineMetadata(name: "First Mac", platform: "macOS"))
    async let secondStart = secondConcurrentClient.startDeviceLogin(
        metadata: CloudMachineMetadata(name: "Second Mac", platform: "macOS"))
    _ = try await (firstStart, secondStart)
    let concurrentRequests = await concurrentFake.captured()
    let firstConcurrentBody = try cloudAccountBody(concurrentRequests[0])
    let secondConcurrentBody = try cloudAccountBody(concurrentRequests[1])
    try require(racingDeviceKeyStore.deviceWriteCount() == 1
                && firstConcurrentBody["public_key"] as? String
                    == secondConcurrentBody["public_key"] as? String,
                "two clients sharing one persistence boundary use exactly one device key")

    let blockingStore = CloudAccountBlockingStore()
    let serializedFake = CloudAccountFakeHTTP()
    let serializedClient = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test/root")!,
        transport: serializedFake,
        credentialStore: blockingStore,
        deviceKeyLoader: { key }
    )
    await serializedFake.enqueue(json: """
    {"status":"complete","account_id":"acct-serialized","machine_id":"machine-serialized",
     "machine_credential":"serialized-bearer-one"}
    """)
    _ = try await serializedClient.pollDeviceLogin(deviceCode: "serialized-one")
    blockingStore.resetMetrics()
    async let firstHeader = serializedClient.authorizationHeader()
    async let secondHeader = serializedClient.authorizationHeader()
    _ = try await (firstHeader, secondHeader)
    try require(blockingStore.maximumConcurrentAccesses() == 1,
                "one client serializes concurrent credential reads")

    blockingStore.resetMetrics()
    await serializedFake.enqueue(json: """
    {"status":"complete","account_id":"acct-serialized","machine_id":"machine-replaced",
     "machine_credential":"serialized-bearer-two"}
    """)
    async let replaced = serializedClient.pollDeviceLogin(deviceCode: "serialized-two")
    async let overlappingHeader = serializedClient.authorizationHeader()
    _ = try await (replaced, overlappingHeader)
    try require(blockingStore.maximumConcurrentAccesses() == 1,
                "one client serializes credential reads and replacement")
    try require(try serializedClient.restoredMachineIdentity()?.machineID == "machine-replaced",
                "serialized replacement leaves one coherent current identity")

    for response in [
        "{\"status\":\"authorization_pending\"}",
        "{\"status\":\"slow_down\",\"retry_after_seconds\":7}",
        "{\"status\":\"access_denied\"}",
        "{\"status\":\"expired_token\"}",
    ] { await fake.enqueue(json: response) }
    try require(try await client.pollDeviceLogin(deviceCode: "one") == .authorizationPending,
                "pending is typed")
    try require(try await client.pollDeviceLogin(deviceCode: "two") == .slowDown(retryAfter: 7),
                "slow_down carries its interval")
    try require(try await client.pollDeviceLogin(deviceCode: "three") == .accessDenied,
                "access_denied is typed")
    try require(try await client.pollDeviceLogin(deviceCode: "four") == .expired,
                "expired_token is typed")

    await fake.enqueue(json: """
    {"status":"complete","account_id":"acct-1","machine_id":"machine-1",
     "machine_credential":"credential-secret"}
    """)
    let completed = try await client.pollDeviceLogin(deviceCode: "five")
    try require(completed == .complete(CloudMachineIdentity(accountID: "acct-1", machineID: "machine-1")),
                "complete exposes only the approved account and machine identifiers")
    try require(try client.restoredMachineIdentity()
                    == CloudMachineIdentity(accountID: "acct-1", machineID: "machine-1"),
                "machine identity restores from the persisted credential")
    try require(try client.authorizationHeader() == "Bearer credential-secret",
                "authorization closure emits the bearer shape CloudTransport consumes")
    try require(try await client.authorizationHeaderProvider()() == "Bearer credential-secret",
                "async authorization provider restores the persisted credential")
    let credentialData = try store.data(for: CloudAccountClient.machineCredentialAccount)
    try require(credentialData != nil, "credential uses its distinct Keychain account name")
    let credentialText = String(decoding: credentialData!, as: UTF8.self)
    try require(credentialText.contains("credential-secret")
                && !String(describing: completed).contains("credential-secret"),
                "credential persists but poll descriptions never return its secret")
    let restoredClient = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test/root")!,
        transport: fake,
        credentialStore: store,
        deviceKeyLoader: { key }
    )
    try require(try restoredClient.authorizationHeader() == "Bearer credential-secret",
                "a fresh client restores the persisted machine credential")

    await fake.enqueue(json: "{\"status\":\"authorization_pending\"}")
    await fake.enqueue(json: "{\"status\":\"slow_down\",\"retry_after_seconds\":7}")
    await fake.enqueue(json: """
    {"status":"complete","account_id":"acct-1","machine_id":"machine-1",
     "machine_credential":"credential-new"}
    """)
    let waited = try await client.waitForDeviceLogin(started)
    try require(waited == .complete(CloudMachineIdentity(accountID: "acct-1", machineID: "machine-1")),
                "wait helper stops on complete")
    try require(await sleeps.all() == [5, 5, 7],
                "poll helper honors advertised and slow_down intervals")

    // The local expiry belongs to the start response receipt, not to the later wait call.
    let expiredFake = CloudAccountFakeHTTP()
    let testClock = CloudAccountTestClock(100)
    let expiredClient = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test/root")!,
        transport: expiredFake,
        credentialStore: CloudInMemoryKeyStore(),
        deviceKeyLoader: { key },
        sleeper: { seconds in testClock.advance(seconds, recordSleep: true) },
        clock: { testClock.now() }
    )
    await expiredFake.enqueue(json: """
    {"device_code":"expired-secret","user_code":"WXYZ-1234",
     "verification_uri":"https://clawdline.com/connect",
     "verification_uri_complete":"https://clawdline.com/connect?user_code=WXYZ-1234",
     "expires_in":10,"interval":4}
    """)
    let expiresFromReceipt = try await expiredClient.startDeviceLogin(
        metadata: CloudMachineMetadata(name: "Expiry Mac", platform: "macOS"))
    testClock.advance(5)
    await expiredFake.repeatResponse(json: "{\"status\":\"authorization_pending\"}")
    try require(try await expiredClient.waitForDeviceLogin(expiresFromReceipt) == .expired,
                "wait expires from the start-response receipt without another poll")
    try require(testClock.recordedSleeps() == [4, 1],
                "wait clamps its final sleep to the received deadline")
    let expiryPolls = await expiredFake.captured().filter {
        $0.url?.path == "/root/v1/auth/device/poll"
    }
    try require(expiryPolls.count == 1,
                "a forever-pending server cannot extend the local device-code deadline")

    let cancelledFake = CloudAccountFakeHTTP()
    let controlledSleeper = CloudAccountControlledSleeper()
    let cancelledClient = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test/root")!,
        transport: cancelledFake,
        credentialStore: CloudInMemoryKeyStore(),
        deviceKeyLoader: { key },
        sleeper: { _ in await controlledSleeper.sleep() }
    )
    let cancelledWait = Task {
        try await cancelledClient.waitForDeviceLogin(started)
    }
    await controlledSleeper.waitUntilEntered()
    cancelledWait.cancel()
    await controlledSleeper.resume()
    do {
        _ = try await cancelledWait.value
        try require(false, "wait checks cancellation when the sleeper does not cooperate")
    } catch is CancellationError {
        try require(true, "wait checks cancellation when the sleeper does not cooperate")
    }
    try require(await cancelledFake.captured().isEmpty,
                "cancelling after a non-cooperative sleep resumes sends no poll")

    await fake.enqueue(json: "{\"status\":\"authorization_pending\",\"surprise\":true}")
    do {
        _ = try await client.pollDeviceLogin(deviceCode: "malformed")
        try require(false, "unknown response fields fail loudly")
    } catch CloudAccountError.invalidResponse {
        try require(true, "unknown response fields fail loudly")
    }
    await fake.enqueue(json: "{\"status\":\"slow_down\",\"retry_after_seconds\":true}")
    do {
        _ = try await client.pollDeviceLogin(deviceCode: "bad-number")
        try require(false, "boolean intervals fail loudly")
    } catch CloudAccountError.invalidResponse {
        try require(true, "boolean intervals fail loudly")
    }

    await fake.enqueue(json:
        "{\"status\":\"slow_down\",\"retry_after_seconds\":9223372036854775808}")
    do {
        _ = try await client.pollDeviceLogin(deviceCode: "oversized-slow-down")
        try require(false, "oversized slow_down integers fail with a typed response error")
    } catch CloudAccountError.invalidResponse {
        try require(true, "oversized slow_down integers fail with a typed response error")
    }

    await fake.enqueue(json: "{\"machines\":[],\"active\":9223372036854775808}")
    do {
        _ = try await client.listMachines()
        try require(false, "oversized active integers fail with a typed response error")
    } catch CloudAccountError.invalidResponse {
        try require(true, "oversized active integers fail with a typed response error")
    }

    for (name, expiresIn, interval) in [
        ("expires_in", 18_446_744_074, 5),
        ("interval", 600, 18_446_744_074),
    ] {
        await fake.enqueue(json: """
        {"device_code":"oversized-start","user_code":"OVER-SIZE",
         "verification_uri":"https://clawdline.com/connect",
         "verification_uri_complete":"https://clawdline.com/connect?user_code=OVER-SIZE",
         "expires_in":\(expiresIn),"interval":\(interval)}
        """)
        do {
            _ = try await client.startDeviceLogin(
                metadata: CloudMachineMetadata(name: "Oversized Mac", platform: "macOS"))
            try require(false, "oversized start \(name) fails before scheduling")
        } catch CloudAccountError.invalidResponse {
            try require(true, "oversized start \(name) fails before scheduling")
        }
    }

    await fake.enqueue(json: "{\"ok\":true,\"at\":\"2026-08-27T09:00:00.000Z\"}")
    let heartbeat = try await client.heartbeat(appVersion: "1.4.0")
    try require(heartbeat.at == ISO8601DateFormatter().date(from: "2026-08-27T09:00:00Z"),
                "heartbeat decodes its timestamp")

    let publicKey = Data(repeating: 0x22, count: 32).base64EncodedString()
    await fake.enqueue(json: """
    {"machines":[{"id":"machine-1","name":"Studio Mac","platform":"macOS",
      "app_version":"1.4.0","public_key":"\(publicKey)",
      "last_seen_at":"2026-08-27T09:00:00.000Z","created_at":"2026-08-01T09:00:00.000Z",
      "revoked_at":null}],"active":1}
    """)
    let machines = try await client.listMachines()
    try require(machines.active == 1 && machines.machines.first?.id == "machine-1",
                "machine list strictly decodes the deployed shape")

    await fake.enqueue(json: """
    {"devices":[{"id":"device-1","kind":"browser","name":"Safari",
      "caps":["read_sessions","send_prompt"],"public_key":"\(publicKey)",
      "last_seen_at":null,"created_at":"2026-08-02T09:00:00.000Z","revoked_at":null}],"active":1}
    """)
    let devices = try await client.listDevices()
    try require(devices.devices.first?.capabilities == [.readSessions, .sendPrompt],
                "device list preserves the pinned capability vocabulary")

    await fake.enqueue(json: """
    {"revoked_at":"2026-08-27T09:01:00.000Z","routing":"stopped",
     "content_key_rotation":"lazy","note":"Routing stops now."}
    """)
    _ = try await client.revokeMachine(id: "machine/with slash", browserSessionCookie: "web-cookie")
    await fake.enqueue(json: """
    {"revoked_at":"2026-08-27T09:02:00.000Z","routing":"stopped",
     "content_key_rotation":"lazy"}
    """)
    _ = try await client.revokeDevice(id: "device-1", browserSessionCookie: "web-cookie")

    requests = await fake.captured()
    let heartbeatRequest = try cloudAccountRequest(requests, path: "/root/v1/machines/heartbeat")
    try require(heartbeatRequest.httpMethod == "POST"
                && heartbeatRequest.value(forHTTPHeaderField: "Authorization") == "Bearer credential-new"
                && (try cloudAccountBody(heartbeatRequest))["app_version"] as? String == "1.4.0",
                "heartbeat uses the machine bearer and exact body")
    let machinesRequest = try cloudAccountRequest(requests, path: "/root/v1/machines")
    let devicesRequest = try cloudAccountRequest(requests, path: "/root/v1/devices")
    try require(machinesRequest.httpMethod == "GET" && devicesRequest.httpMethod == "GET",
                "list operations use exact GET routes")
    let revokeMachineRequest = requests.first {
        $0.httpMethod == "DELETE" && $0.url?.path.contains("/v1/machines/") == true
    }!
    let revokeDeviceRequest = try cloudAccountRequest(requests, path: "/root/v1/devices/device-1")
    try require(revokeMachineRequest.url?.absoluteString.contains("machine%2Fwith%20slash") == true
                && revokeMachineRequest.value(forHTTPHeaderField: "Cookie") == "cl_session=web-cookie",
                "machine revoke path-encodes its id and uses the deployed browser session seam")
    try require(revokeDeviceRequest.httpMethod == "DELETE"
                && revokeDeviceRequest.value(forHTTPHeaderField: "Cookie") == "cl_session=web-cookie",
                "device revoke uses exact DELETE route and browser session seam")

    await fake.enqueue(json: """
    {"pairing_id":"pair-1","claim_nonce":"nonce-secret",
     "expires_at":"2026-08-27T09:10:00.000Z","expires_in":300}
    """)
    let pairing = try await client.startPairing(fingerprint: "AB12-CD34-EF56-7890")
    try require(pairing.pairingID == "pair-1" && pairing.claimNonce == "nonce-secret",
                "pairing start decodes the routing handle and claim nonce")
    try require(!String(describing: pairing).contains("nonce-secret"),
                "pairing start descriptions redact the claim nonce")
    let opaque = try CloudOpaquePairingBlob(base64: Data([0, 1, 2, 0xff]).base64EncodedString())
    await fake.enqueue(json: "{\"status\":\"delivered\",\"fingerprint\":\"AB12-CD34-EF56-7890\"}")
    _ = try await client.completePairing(pairingID: "pair-1", blob: opaque)
    await fake.enqueue(status: 202, json: "{\"error\":{\"code\":\"pairing_pending\",\"message\":\"wait\"}}")
    try require(try await client.claimPairing(pairingID: "pair-1", claimNonce: "nonce-secret") == .pending,
                "pairing claim models the deployed 202 pending response")
    let opaqueBase64 = Data([0, 1, 2, 0xff]).base64EncodedString()
    await fake.enqueue(json: "{\"ciphertext\":\"\(opaqueBase64)\",\"sender_device_id\":\"device-1\"}")
    let claim = try await client.claimPairing(pairingID: "pair-1", claimNonce: "nonce-secret")
    try require(claim == .complete(blob: opaque, senderDeviceID: "device-1"),
                "pairing claim returns exactly the opaque blob")
    try require(!String(describing: opaque).contains(opaqueBase64),
                "opaque blob descriptions redact ciphertext")

    requests = await fake.captured()
    let pairingStartRequest = try cloudAccountRequest(requests, path: "/root/v1/pairing/start")
    let pairingCompleteRequest = try cloudAccountRequest(requests, path: "/root/v1/pairing/complete")
    let pairingClaimRequest = try cloudAccountRequest(requests, path: "/root/v1/pairing/claim")
    try require(pairingStartRequest.httpMethod == "POST"
                && (try cloudAccountBody(pairingStartRequest))["fingerprint"] as? String == "AB12-CD34-EF56-7890",
                "pairing start uses the exact route and body")
    try require((try cloudAccountBody(pairingCompleteRequest))["ciphertext"] as? String == opaqueBase64,
                "pairing complete transports ciphertext unchanged")
    let claimBody = try cloudAccountBody(pairingClaimRequest)
    try require(Set(claimBody.keys) == ["pairing_id", "claim_nonce"],
                "pairing claim sends only the pinned routing values")

    let lifecycleFake = CloudAccountFakeHTTP()
    let lifecycleStore = CloudInMemoryKeyStore()
    let lifecycleClient = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test/root")!,
        transport: lifecycleFake,
        credentialStore: lifecycleStore,
        deviceKeyLoader: { key }
    )
    await lifecycleFake.enqueue(json: """
    {"status":"complete","account_id":"acct-life","machine_id":"machine-life",
     "machine_credential":"old-bearer-fixture"}
    """)
    _ = try await lifecycleClient.pollDeviceLogin(deviceCode: "lifecycle-one")
    let oldCredentialData = try lifecycleStore.data(
        for: CloudAccountClient.machineCredentialAccount)
    try lifecycleClient.signOut()
    try require(try lifecycleClient.restoredMachineIdentity() == nil
                && (try lifecycleStore.data(for: CloudAccountClient.machineCredentialAccount)) == nil,
                "explicit sign-out clears identity and secure storage")
    do {
        _ = try lifecycleClient.authorizationHeader()
        try require(false, "explicit sign-out clears the authorization header")
    } catch CloudAccountError.missingMachineCredential {
        try require(true, "explicit sign-out clears the authorization header")
    }

    await lifecycleFake.enqueue(json: """
    {"status":"complete","account_id":"acct-life","machine_id":"machine-life",
     "machine_credential":"replacement-bearer-fixture"}
    """)
    _ = try await lifecycleClient.pollDeviceLogin(deviceCode: "lifecycle-two")
    let replacementCredentialData = try lifecycleStore.data(
        for: CloudAccountClient.machineCredentialAccount)
    try require(replacementCredentialData != oldCredentialData,
                "re-login atomically replaces the previous credential bytes")
    try require(replacementCredentialData?.range(of: Data("old-bearer-fixture".utf8)) == nil,
                "re-login leaves no previous bearer bytes in secure storage")
    await lifecycleFake.enqueue(json: """
    {"revoked_at":"2026-08-27T09:03:00.000Z","routing":"stopped",
     "content_key_rotation":"lazy","note":"Routing stops now."}
    """)
    _ = try await lifecycleClient.revokeMachine(
        id: "another-machine", browserSessionCookie: "cookie")
    try require(try lifecycleClient.restoredMachineIdentity()?.machineID == "machine-life",
                "revoking another machine preserves the current credential")
    await lifecycleFake.enqueue(json: """
    {"revoked_at":"2026-08-27T09:04:00.000Z","routing":"stopped",
     "content_key_rotation":"lazy","note":"Routing stops now."}
    """)
    _ = try await lifecycleClient.revokeMachine(
        id: "machine-life", browserSessionCookie: "cookie")
    try require(try lifecycleClient.restoredMachineIdentity() == nil
                && (try lifecycleStore.data(for: CloudAccountClient.machineCredentialAccount)) == nil,
                "successful self-revocation clears identity and secure storage")

    await lifecycleFake.enqueue(json: """
    {"status":"complete","account_id":"acct-life","machine_id":"machine-life",
     "machine_credential":"third-bearer-fixture"}
    """)
    _ = try await lifecycleClient.pollDeviceLogin(deviceCode: "lifecycle-three")
    await lifecycleFake.enqueue(
        status: 401,
        json: "{\"error\":{\"code\":\"account_suspended\",\"message\":\"no\"}}")
    do {
        _ = try await lifecycleClient.heartbeat()
        try require(false, "an unrelated 401 is rejected")
    } catch CloudAccountError.http(let status, let code) {
        try require(status == 401 && code == "account_suspended",
                    "an unrelated 401 is rejected")
    }
    try require(try lifecycleClient.restoredMachineIdentity()?.machineID == "machine-life",
                "an unrelated 401 does not invalidate the current credential")

    await lifecycleFake.enqueueBlocked(
        status: 401,
        json: "{\"error\":{\"code\":\"no_machine_credential\",\"message\":\"no\"}}")
    let staleUnauthorized = Task { try await lifecycleClient.heartbeat() }
    await lifecycleFake.waitUntilBlocked()
    await lifecycleFake.enqueue(json: """
    {"status":"complete","account_id":"acct-life","machine_id":"machine-replacement",
     "machine_credential":"fourth-bearer-fixture"}
    """)
    _ = try await lifecycleClient.pollDeviceLogin(deviceCode: "lifecycle-four")
    await lifecycleFake.resumeBlocked()
    do {
        _ = try await staleUnauthorized.value
        try require(false, "the stale request still reports its deployed 401")
    } catch CloudAccountError.http(let status, let code) {
        try require(status == 401 && code == "no_machine_credential",
                    "the stale request still reports its deployed 401")
    }
    try require(try lifecycleClient.restoredMachineIdentity()?.machineID == "machine-replacement",
                "a stale 401 cannot erase an atomically replaced credential")

    let crossClientStore = CloudAccountInterleavingStore()
    let oldBearerFake = CloudAccountFakeHTTP()
    let replacementBearerFake = CloudAccountFakeHTTP()
    let oldBearerClient = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test/root")!,
        transport: oldBearerFake,
        credentialStore: crossClientStore,
        deviceKeyLoader: { key }
    )
    let replacementBearerClient = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test/root")!,
        transport: replacementBearerFake,
        credentialStore: crossClientStore,
        deviceKeyLoader: { key }
    )
    await oldBearerFake.enqueue(json: """
    {"status":"complete","account_id":"acct-cross","machine_id":"machine-old",
     "machine_credential":"cross-client-old-bearer"}
    """)
    _ = try await oldBearerClient.pollDeviceLogin(deviceCode: "cross-client-old")
    await oldBearerFake.enqueueBlocked(
        status: 401,
        json: "{\"error\":{\"code\":\"no_machine_credential\",\"message\":\"no\"}}")
    let crossClientUnauthorized = Task { try await oldBearerClient.heartbeat() }
    await oldBearerFake.waitUntilBlocked()
    crossClientStore.armCredentialReadBlock()
    await oldBearerFake.resumeBlocked()
    crossClientStore.waitUntilCredentialReadBlocked()
    await replacementBearerFake.enqueue(json: """
    {"status":"complete","account_id":"acct-cross","machine_id":"machine-new",
     "machine_credential":"cross-client-new-bearer"}
    """)
    _ = try await replacementBearerClient.pollDeviceLogin(deviceCode: "cross-client-new")
    crossClientStore.resumeBlockedCredentialRead()
    do {
        _ = try await crossClientUnauthorized.value
        try require(false, "the cross-client stale request still reports its deployed 401")
    } catch CloudAccountError.http(let status, let code) {
        try require(status == 401 && code == "no_machine_credential",
                    "the cross-client stale request still reports its deployed 401")
    }
    try require(try replacementBearerClient.restoredMachineIdentity()?.machineID == "machine-new",
                "a late old-bearer 401 cannot delete another client's new bearer")

    await lifecycleFake.enqueue(
        status: 401,
        json: "{\"error\":{\"code\":\"no_machine_credential\",\"message\":\"no\"}}")
    do {
        _ = try await lifecycleClient.heartbeat()
        try require(false, "the deployed missing-credential 401 is rejected")
    } catch CloudAccountError.http(let status, let code) {
        try require(status == 401 && code == "no_machine_credential",
                    "the deployed missing-credential 401 is rejected")
    }
    try require(try lifecycleClient.restoredMachineIdentity() == nil
                && (try lifecycleStore.data(for: CloudAccountClient.machineCredentialAccount)) == nil,
                "the deployed missing-credential 401 invalidates local state")

    let wrongStatusCases: [(String, String, () async throws -> Void)] = [
        ("device start", """
         {"device_code":"status-secret","user_code":"STAT-0001",
          "verification_uri":"https://clawdline.com/connect",
          "verification_uri_complete":"https://clawdline.com/connect?user_code=STAT-0001",
          "expires_in":600,"interval":5}
         """, {
            _ = try await client.startDeviceLogin(
                metadata: CloudMachineMetadata(name: "Status Mac", platform: "macOS"))
         }),
        ("device poll", "{\"status\":\"authorization_pending\"}", {
            _ = try await client.pollDeviceLogin(deviceCode: "status-secret")
        }),
        ("heartbeat", "{\"ok\":true,\"at\":\"2026-08-27T09:00:00.000Z\"}", {
            _ = try await client.heartbeat()
        }),
        ("machine list", "{\"machines\":[],\"active\":0}", {
            _ = try await client.listMachines()
        }),
        ("device list", "{\"devices\":[],\"active\":0}", {
            _ = try await client.listDevices()
        }),
        ("machine revoke", """
         {"revoked_at":"2026-08-27T09:01:00.000Z","routing":"stopped",
          "content_key_rotation":"lazy","note":"Routing stops now."}
         """, {
            _ = try await client.revokeMachine(id: "other-machine", browserSessionCookie: "cookie")
         }),
        ("device revoke", """
         {"revoked_at":"2026-08-27T09:02:00.000Z","routing":"stopped",
          "content_key_rotation":"lazy"}
         """, {
            _ = try await client.revokeDevice(id: "other-device", browserSessionCookie: "cookie")
         }),
        ("pairing start", """
         {"pairing_id":"status-pair","claim_nonce":"status-nonce",
          "expires_at":"2026-08-27T09:10:00.000Z","expires_in":300}
         """, {
            _ = try await client.startPairing(fingerprint: "AB12-CD34-EF56-7890")
         }),
        ("pairing complete", "{\"status\":\"delivered\",\"fingerprint\":\"AB12-CD34-EF56-7890\"}", {
            _ = try await client.completePairing(pairingID: "status-pair", blob: opaque)
        }),
        ("pairing claim", "{\"ciphertext\":\"" + opaqueBase64 + "\",\"sender_device_id\":null}", {
            _ = try await client.claimPairing(pairingID: "status-pair", claimNonce: "status-nonce")
        }),
    ]
    for (name, json, operation) in wrongStatusCases {
        await fake.enqueue(status: 201, json: json)
        do {
            try await operation()
            try require(false, "\(name) rejects an unexpected 2xx status")
        } catch CloudAccountError.http(let status, _) {
            try require(status == 201, "\(name) rejects an unexpected 2xx status")
        }
    }

    await fake.enqueue(status: 401, json: "{\"error\":{\"code\":\"no_machine_credential\",\"message\":\"no\"}}")
    do {
        _ = try await client.heartbeat()
        try require(false, "HTTP errors retain status and safe code")
    } catch CloudAccountError.http(let status, let code) {
        try require(status == 401 && code == "no_machine_credential",
                    "HTTP errors retain status and safe code")
    }

    return checks
}
