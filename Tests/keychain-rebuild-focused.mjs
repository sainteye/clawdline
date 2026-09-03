#!/usr/bin/env node

import {
  chmodSync, mkdtempSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const lifecycleSource = resolve(process.env.CLAWDLINE_KEYCHAIN_LIFECYCLE_SOURCE ||
  "Sources/CloudBridgeLifecycle.swift");
const keysSource = resolve(process.env.CLAWDLINE_KEYCHAIN_KEYS_SOURCE ||
  "Sources/CloudKeys.swift");
const buildSource = resolve(process.env.CLAWDLINE_KEYCHAIN_BUILD_SOURCE || "build.sh");
const setupSource = resolve(process.env.CLAWDLINE_KEYCHAIN_SETUP_SOURCE ||
  "tools/setup-local-signing-identity.sh");
const accountSource = resolve(process.env.CLAWDLINE_KEYCHAIN_ACCOUNT_SOURCE ||
  "Sources/CloudAccount.swift");
const settingsSource = resolve(process.env.CLAWDLINE_KEYCHAIN_SETTINGS_SOURCE ||
  "Sources/CloudSettings.swift");
const appMainSource = resolve(process.env.CLAWDLINE_KEYCHAIN_MAIN_SOURCE ||
  "Sources/main.swift");
const work = mkdtempSync(join(process.env.TMPDIR || tmpdir(), "clawdline-keychain-focused-"));

let nodeChecks = 0;
function check(condition, name) {
  if (!condition) {
    process.stderr.write(`FAIL after ${nodeChecks} node checks: ${name}\n`);
    process.exit(1);
  }
  nodeChecks += 1;
  console.log(`✓ ${name}`);
}

function executable(path, body) {
  writeFileSync(path, body, "utf8");
  chmodSync(path, 0o755);
}

function declaration(source, name) {
  const start = source.indexOf(name);
  if (start < 0) throw new Error(`missing declaration: ${name}`);
  const opening = source.indexOf("{", start);
  let depth = 0;
  let state = "code";
  let blockDepth = 0;
  for (let index = opening; index < source.length; index += 1) {
    const here = source[index];
    const next = source[index + 1];
    if (state === "line") {
      if (here === "\n") state = "code";
      continue;
    }
    if (state === "block") {
      if (here === "/" && next === "*") { blockDepth += 1; index += 1; continue; }
      if (here === "*" && next === "/") {
        blockDepth -= 1; index += 1;
        if (blockDepth === 0) state = "code";
      }
      continue;
    }
    if (state === "string") {
      if (here === "\\") { index += 1; continue; }
      if (here === '"') state = "code";
      continue;
    }
    if (here === "/" && next === "/") { state = "line"; index += 1; continue; }
    if (here === "/" && next === "*") {
      state = "block"; blockDepth = 1; index += 1; continue;
    }
    if (here === '"') { state = "string"; continue; }
    if (here === "{") depth += 1;
    if (here === "}") {
      depth -= 1;
      if (depth === 0) return source.slice(start, index + 1);
    }
  }
  throw new Error(`unterminated declaration: ${name}`);
}

function marked(source, label) {
  const begin = `# BEGIN keychain-rebuild-focused: ${label}`;
  const end = `# END keychain-rebuild-focused: ${label}`;
  const first = source.indexOf(begin);
  const last = source.indexOf(end);
  if (first < 0 || last < first) throw new Error(`missing marked shell block: ${label}`);
  return source.slice(source.indexOf("\n", first) + 1, last);
}

function run(path, args = [], env = {}) {
  return spawnSync(path, args, {
    cwd: process.cwd(), encoding: "utf8", env: { ...process.env, ...env },
  });
}

function swiftSources(directory) {
  return readdirSync(directory, { recursive: true })
    .filter((path) => path.endsWith(".swift") && path !== "main.swift")
    .map((path) => join(directory, path)).sort();
}

try {
  const appMain = readFileSync(appMainSource, "utf8");
  const connectionWiringStart = appMain.indexOf("CloudSettingsModel.onConnectionChange");
  const connectionWiring = connectionWiringStart < 0
    ? "" : appMain.slice(connectionWiringStart, connectionWiringStart + 500);
  check(/if\s+connected\s*\{\s*CloudBridgeLifecycle\.shared\.apply\(\)\s*\}\s*else\s*\{\s*CloudBridgeLifecycle\.shared\.signedOut\(\)/s.test(connectionWiring),
    "production applies a connected identity and directly detaches a disconnected bridge");

  const lifecycle = readFileSync(lifecycleSource, "utf8");
  const keys = readFileSync(keysSource, "utf8");
  const policy = declaration(lifecycle, "struct CloudIdentityReadPolicy");
  const reader = declaration(keys, "final class CloudKeychainReader");
  const swiftHarness = join(work, "main.swift");
  const swiftBinary = join(work, "keychain-concurrency");
  writeFileSync(swiftHarness, `
import Foundation

${policy}

${reader}

final class Probe: @unchecked Sendable {
    private let lock = NSLock()
    private(set) var running = 0
    private(set) var maximum = 0
    private(set) var completions = 0
    private(set) var completionOffMain = false
    private(set) var operationOnMain = false
    private(set) var starts = 0
    func entered() -> Int {
        lock.lock(); running += 1; starts += 1; maximum = max(maximum, running)
        if Thread.isMainThread { operationOnMain = true }
        let value = starts
        lock.unlock()
        return value
    }
    func left() { lock.lock(); running -= 1; lock.unlock() }
    func completed() {
        lock.lock(); completions += 1
        if !Thread.isMainThread { completionOffMain = true }
        lock.unlock()
    }
    func snapshot() -> (Int, Int, Bool, Bool) {
        lock.lock(); defer { lock.unlock() }
        return (maximum, completions, completionOffMain, operationOnMain)
    }
}

final class ReadEvents: @unchecked Sendable {
    private let lock = NSLock()
    private var events: [String] = []
    func add(_ event: String) { lock.lock(); events.append(event); lock.unlock() }
    func snapshot() -> [String] { lock.lock(); defer { lock.unlock() }; return events }
}

var checks = 0
func check(_ condition: @autoclosure () -> Bool, _ name: String) {
    guard condition() else {
        FileHandle.standardError.write(Data(("FAIL after \\(checks) Swift checks: " + name + "\\n").utf8))
        exit(1)
    }
    checks += 1
    print("✓ " + name)
}

var tracker = CloudIdentityReadPolicy.Tracker()
check(tracker.knowledge == .unknown, "launch begins with identity unknown, not signed out")
check(CloudIdentityReadPolicy.request(&tracker) == .start(generation: 1) &&
      tracker.knowledge == .reading, "the first request starts one read and exposes reading")
check(CloudIdentityReadPolicy.request(&tracker) == .coalesced,
      "an overlapping request is coalesced")
check(CloudIdentityReadPolicy.complete(generation: 1, tracker: &tracker) ==
      .restart(generation: 2), "a stale answer starts only the latest requested read")
check(CloudIdentityReadPolicy.complete(generation: 1, tracker: &tracker) == .discard,
      "an answer from a completed generation cannot apply twice")
check(CloudIdentityReadPolicy.complete(generation: 2, tracker: &tracker) == .accept &&
      tracker.knowledge == .resolved, "only the newest answer becomes resolved evidence")
_ = CloudIdentityReadPolicy.request(&tracker)
CloudIdentityReadPolicy.resolveWithoutRead(&tracker)
check(!CloudIdentityReadPolicy.acceptsTimeout(generation: 3, tracker: tracker),
      "a timeout from a signed-out generation is discarded")
check(CloudIdentityReadPolicy.complete(generation: 3, tracker: &tracker) == .discard &&
      tracker.knowledge == .resolved, "foreground sign-out invalidates an answer already returning")
var timeoutTracker = CloudIdentityReadPolicy.Tracker()
check(CloudIdentityReadPolicy.request(&timeoutTracker) == .start(generation: 1) &&
      CloudIdentityReadPolicy.acceptsTimeout(generation: 1, tracker: timeoutTracker),
      "only the current reading generation may publish timeout progress")

MainActor.assumeIsolated {
let probe = Probe()
let started = DispatchSemaphore(value: 0)
let release = DispatchSemaphore(value: 0)
let identityReader = CloudKeychainReader<Int>(label: "clawdline.test.identity") {
    let call = probe.entered()
    if call == 1 { started.signal(); release.wait() }
    probe.left()
    return call
}
let before = Date()
identityReader.read { _ in probe.completed() }
let elapsed = Date().timeIntervalSince(before)
check(elapsed < 0.1, "starting a blocked read returns to the main thread immediately")
check(started.wait(timeout: .now() + 1) == .success,
      "the background reader actually starts its operation")
check(!probe.snapshot().3, "the blocking operation runs off the main thread")
identityReader.read { _ in probe.completed() }
Thread.sleep(forTimeInterval: 0.05)
check(probe.snapshot().0 == 1, "the dedicated reader never overlaps two operations")
release.signal()
let deadline = Date().addingTimeInterval(2)
while probe.snapshot().1 < 2 && Date() < deadline {
    RunLoop.main.run(until: Date().addingTimeInterval(0.01))
}
let final = probe.snapshot()
check(final.1 == 2 && !final.2, "both results return through the main queue")

let lateRelease = DispatchSemaphore(value: 0)
let lateEvents = ReadEvents()
let lateReader = CloudKeychainReader<Int>(
    label: "clawdline.test.identity-timeout", timeoutSeconds: 1
) {
    lateRelease.wait()
    return 7
}
lateReader.read(
    onTimeout: { seconds in lateEvents.add("timeout:\\(seconds)") },
    completion: { result in lateEvents.add("terminal:\\((try? result.get()) ?? -1)") })
let timeoutDeadline = Date().addingTimeInterval(3)
while lateEvents.snapshot().isEmpty && Date() < timeoutDeadline {
    RunLoop.main.run(until: Date().addingTimeInterval(0.01))
}
check(lateEvents.snapshot() == ["timeout:1"],
      "a blocked read publishes bounded unknown progress")
lateRelease.signal()
let terminalDeadline = Date().addingTimeInterval(2)
while lateEvents.snapshot().count < 2 && Date() < terminalDeadline {
    RunLoop.main.run(until: Date().addingTimeInterval(0.01))
}
check(lateEvents.snapshot() == ["timeout:1", "terminal:7"],
      "a read retains its late terminal result for reconciliation")
}
print("\\(checks) Swift checks passed")
`, "utf8");
  const compile = run("swiftc", ["-swift-version", "5", "-target", "arm64-apple-macos13.0",
    swiftHarness, "-o", swiftBinary]);
  check(compile.status === 0, `production concurrency seams compile: ${compile.stderr}`);
  const swift = run(swiftBinary);
  process.stdout.write(swift.stdout);
  process.stderr.write(swift.stderr);
  check(swift.status === 0 && /16 Swift checks passed/.test(swift.stdout),
    "compiled reader proves background, serial, bounded and late-terminal ordering");

  const storeWork = join(work, "store");
  mkdirSync(storeWork);
  const storeHarness = join(storeWork, "main.swift");
  const storeBinary = join(work, "store-main");
  // Top-level code, so the main thread really is the main thread. Inside the Swift suite it is
  // not: `dispatchMain()` parks it, and every later main-*queue* block answers
  // `Thread.isMainThread == false`. A guard spelled on the thread has to be witnessed here.
  writeFileSync(storeHarness, `
import Foundation

let store = CloudKeychainStore(service: "app.clawdline.focused.main-thread-probe")
var storeChecks = 0

func refuses(_ name: String, _ wanted: CloudKeyError, _ body: () throws -> Void) {
    do {
        try body()
        FileHandle.standardError.write(Data("FAIL: main-thread Keychain \\(name) was admitted\\n".utf8))
        exit(1)
    } catch let error as CloudKeyError where error == wanted {
        storeChecks += 1
        print("✓ main-thread \\(name) is refused before Security")
    } catch {
        FileHandle.standardError.write(Data(("FAIL: wrong main-thread \\(name) error: \\(error)\\n").utf8))
        exit(1)
    }
}

refuses("read", .mainThreadReadForbidden) { _ = try store.data(for: "missing") }
refuses("set", .mainThreadWriteForbidden) { try store.set(Data("x".utf8), for: "missing") }
refuses("remove", .mainThreadWriteForbidden) { try store.remove("missing") }
let query = CloudKeychainStore.identity(service: "service", account: "account")
if let queryUI = query[kSecUseAuthenticationUI],
   CFEqual(queryUI as CFTypeRef, kSecUseAuthenticationUIFail) {
    storeChecks += 1
    print("✓ every query refuses Security authentication UI")
} else {
    FileHandle.standardError.write(Data("FAIL: query can create authentication UI\\n".utf8))
    exit(1)
}
let insert = CloudKeychainStore.insertAttributes(
    service: "service", account: "account", data: Data("x".utf8))
if let insertUI = insert[kSecUseAuthenticationUI],
   CFEqual(insertUI as CFTypeRef, kSecUseAuthenticationUIFail) {
    storeChecks += 1
    print("✓ inserts refuse Security authentication UI")
} else {
    FileHandle.standardError.write(Data("FAIL: insert can create authentication UI\\n".utf8))
    exit(1)
}
print("\\(storeChecks) store-boundary checks passed")
`, "utf8");
  const storeCompile = run("swiftc", ["-swift-version", "5", "-target",
    "arm64-apple-macos13.0", keysSource, storeHarness, "-framework", "Security",
    "-o", storeBinary]);
  check(storeCompile.status === 0, `the production Keychain store guard compiles: ${storeCompile.stderr}`);
  const storeProbe = run(storeBinary);
  process.stdout.write(storeProbe.stdout);
  process.stderr.write(storeProbe.stderr);
  check(storeProbe.status === 0 && /5 store-boundary checks passed/.test(storeProbe.stdout),
    "the production store rejects main-thread access and every Security authentication UI");

  // The bounded writer. Its whole reason for existing is the answer that never comes, so the
  // interesting cases are the timeout and what happens to a result that arrives after it.
  const writerWork = join(work, "writer");
  mkdirSync(writerWork);
  const writerHarness = join(writerWork, "main.swift");
  const writerBinary = join(work, "writer-main");
  writeFileSync(writerHarness, `
import Foundation

struct WriterFailure: Error, LocalizedError {
    var errorDescription: String? { "the Keychain said no" }
}

final class Box: @unchecked Sendable {
    private let lock = NSLock()
    private var values: [String] = []
    private var offMain = false
    func record(_ value: String) { lock.lock(); values.append(value); lock.unlock() }
    func noteOperationThread() {
        lock.lock(); offMain = !Thread.isMainThread; lock.unlock()
    }
    func snapshot() -> ([String], Bool) { lock.lock(); defer { lock.unlock() }; return (values, offMain) }
}

var checks = 0
func check(_ condition: @autoclosure () -> Bool, _ name: String) {
    guard condition() else {
        FileHandle.standardError.write(Data(("FAIL after \\(checks) writer checks: " + name + "\\n").utf8))
        exit(1)
    }
    checks += 1
    print("✓ " + name)
}

func drain(untilAtLeast count: Int, in box: Box, seconds: TimeInterval) {
    let deadline = Date().addingTimeInterval(seconds)
    while box.snapshot().0.count < count && Date() < deadline {
        RunLoop.main.run(until: Date().addingTimeInterval(0.01))
    }
}

MainActor.assumeIsolated {
    // 1. Starting a write that cannot finish must still return to the caller at once.
    let blocked = DispatchSemaphore(value: 0)
    let entered = DispatchSemaphore(value: 0)
    let slowBox = Box()
    let slow = CloudKeychainWriter(label: "focused.slow", timeoutSeconds: 1) {
        slowBox.noteOperationThread()
        entered.signal()
        blocked.wait()
    }
    let before = Date()
    slow.write { outcome in slowBox.record(outcome.kind.rawValue) }
    check(Date().timeIntervalSince(before) < 0.1,
          "handing a blocked write over returns to the caller immediately")
    check(entered.wait(timeout: .now() + 2) == .success, "the writer actually starts its operation")
    check(slowBox.snapshot().1, "the blocking mutation runs off the main thread")

    // 2. A Keychain that never answers becomes a timeout rather than a spinner.
    drain(untilAtLeast: 1, in: slowBox, seconds: 4)
    check(slowBox.snapshot().0 == ["timedOut"], "an unanswered write times out on its own bound")
    check(CloudKeychainWriteOutcome.timedOut(seconds: 1).message.contains("locked"),
          "the timeout message names the lock a person can go and clear")

    // 3. Timeout is progress, not a terminal result. The late Security answer is the only
    //    evidence that can reconcile the model with what actually happened in the store.
    blocked.signal()
    drain(untilAtLeast: 2, in: slowBox, seconds: 1)
    check(slowBox.snapshot().0 == ["timedOut", "succeeded"],
          "a result arriving after timeout is retained as the terminal reconciliation")

    // 4. Ordinary success and ordinary failure, both delivered on the main queue.
    let okBox = Box()
    CloudKeychainWriter(label: "focused.ok") {}.write { outcome in
        okBox.record(Thread.isMainThread ? outcome.kind.rawValue : "off-main")
    }
    drain(untilAtLeast: 1, in: okBox, seconds: 2)
    check(okBox.snapshot().0 == ["succeeded"], "a write that works answers succeeded, on the main thread")

    let failBox = Box()
    CloudKeychainWriter(label: "focused.fail") { throw WriterFailure() }.write { outcome in
        failBox.record(outcome.message)
    }
    drain(untilAtLeast: 1, in: failBox, seconds: 2)
    check(failBox.snapshot().0 == ["the Keychain said no"],
          "a failed write carries the error's own sentence, not a blank one")

    // 5. Cancelling suppresses the answer without pretending the call was cancelled.
    let cancelBox = Box()
    let releaseCancelled = DispatchSemaphore(value: 0)
    let cancelled = CloudKeychainWriter(label: "focused.cancel", timeoutSeconds: 1) {
        releaseCancelled.wait()
    }
    let handle = cancelled.write { outcome in cancelBox.record(outcome.kind.rawValue) }
    handle.cancel()
    releaseCancelled.signal()
    let deadline = Date().addingTimeInterval(2.5)
    while Date() < deadline { RunLoop.main.run(until: Date().addingTimeInterval(0.05)) }
    check(cancelBox.snapshot().0.isEmpty,
          "a cancelled write publishes neither its result nor its timeout")
}
print("\\(checks) writer checks passed")
`, "utf8");
  const writerCompile = run("swiftc", ["-swift-version", "5", "-target",
    "arm64-apple-macos13.0", keysSource, writerHarness, "-framework", "Security",
    "-o", writerBinary]);
  check(writerCompile.status === 0, `the bounded Keychain writer compiles: ${writerCompile.stderr}`);
  const writerProbe = run(writerBinary);
  process.stdout.write(writerProbe.stdout);
  process.stderr.write(writerProbe.stderr);
  check(writerProbe.status === 0 && /9 writer checks passed/.test(writerProbe.stdout),
    "the bounded writer is off-main, one-shot, bounded and cancellable");

  // The credential race, against the production CloudAccountClient rather than a copy of it.
  const accountWork = join(work, "account");
  mkdirSync(accountWork);
  const accountHarness = join(accountWork, "main.swift");
  const accountBinary = join(work, "account-main");
  writeFileSync(accountHarness, `
import Foundation

// CloudTransport's real provider drags the whole envelope stack in. Only its name is used here.
struct CloudAPIDeviceTokenProvider: Sendable {
    typealias AuthorizationHeaderProvider = @Sendable () throws -> String
    init(apiBaseURL: URL, session: URLSession, authorizationHeader: @escaping AuthorizationHeaderProvider) {}
}

private actor Wire: CloudAccountHTTPTransport {
    private var payload = Data()
    private var blocked = false
    private var release: CheckedContinuation<Void, Never>?
    private var waiters: [CheckedContinuation<Void, Never>] = []

    func answer(_ json: String, blocking: Bool) {
        payload = Data(json.utf8)
        blocked = blocking
    }

    func waitUntilBlocked() async {
        if release != nil { return }
        await withCheckedContinuation { waiters.append($0) }
    }

    func resume() {
        release?.resume()
        release = nil
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let response = HTTPURLResponse(
            url: request.url!, statusCode: 200, httpVersion: "HTTP/1.1",
            headerFields: ["Content-Type": "application/json"])!
        if blocked {
            blocked = false
            await withCheckedContinuation { continuation in
                release = continuation
                let pending = waiters
                waiters.removeAll()
                pending.forEach { $0.resume() }
            }
        }
        return (payload, response)
    }
}

/// Holds \`set\` open so the test can act inside the write window itself.
final class RaceStore: CloudKeyStoring, @unchecked Sendable {
    let coordinator = CloudKeyStoreCoordinator()
    private let condition = NSCondition()
    private var values: [String: Data] = [:]
    private var armed = false
    private var parked = false
    private var released = false
    private var writes = 0
    private var failRemoval = false

    func data(for account: String) throws -> Data? {
        condition.lock(); defer { condition.unlock() }
        return values[account]
    }

    func set(_ data: Data, for account: String) throws {
        condition.lock()
        values[account] = data
        writes += 1
        if armed {
            armed = false
            parked = true
            condition.broadcast()
            let deadline = Date().addingTimeInterval(3)
            while !released && condition.wait(until: deadline) {}
        }
        condition.unlock()
    }

    func remove(_ account: String) throws {
        condition.lock()
        defer { condition.unlock() }
        if failRemoval { throw StoreFailure() }
        values.removeValue(forKey: account)
    }

    func arm() { condition.lock(); armed = true; parked = false; released = false; condition.unlock() }
    func waitUntilParked() {
        condition.lock()
        let deadline = Date().addingTimeInterval(3)
        while !parked && condition.wait(until: deadline) {}
        condition.unlock()
    }
    func release() { condition.lock(); released = true; condition.broadcast(); condition.unlock() }
    func writeCount() -> Int { condition.lock(); defer { condition.unlock() }; return writes }
    func setRemovalFailure(_ value: Bool) {
        condition.lock(); failRemoval = value; condition.unlock()
    }
}

struct StoreFailure: Error, LocalizedError {
    var errorDescription: String? { "fixture removal failed" }
}

final class InterleavingDefaults: UserDefaults, @unchecked Sendable {
    private let condition = NSCondition()
    private var lowSetEntered = false
    private var releaseLowSet = false
    private var highSetEntered = false

    override func set(_ value: Any?, forKey defaultName: String) {
        let epoch = (value as? NSNumber)?.uint64Value
        condition.lock()
        if epoch == 3 {
            lowSetEntered = true
            condition.broadcast()
            let deadline = Date().addingTimeInterval(2)
            while !releaseLowSet && condition.wait(until: deadline) {}
        } else if epoch == 9 {
            highSetEntered = true
            condition.broadcast()
        }
        condition.unlock()
        super.set(value, forKey: defaultName)
    }

    func waitForLowSet() -> Bool {
        condition.lock()
        let deadline = Date().addingTimeInterval(1)
        while !lowSetEntered && condition.wait(until: deadline) {}
        let result = lowSetEntered
        condition.unlock()
        return result
    }

    func highSetOvertookLow() -> Bool {
        condition.lock()
        let deadline = Date().addingTimeInterval(0.15)
        while !highSetEntered && condition.wait(until: deadline) {}
        let result = highSetEntered
        condition.unlock()
        return result
    }

    func releaseLow() {
        condition.lock(); releaseLowSet = true; condition.broadcast(); condition.unlock()
    }
}

final class FailingInvalidationStore:
    CloudCredentialInvalidationStoring, @unchecked Sendable
{
    private let lock = NSLock()
    private var processEpoch: UInt64 = 0
    private var durableEpoch: UInt64 = 0
    private var failing = true

    func reserve(atLeast wanted: UInt64) -> UInt64 {
        lock.lock(); defer { lock.unlock() }
        processEpoch = max(processEpoch, wanted)
        return processEpoch
    }
    func currentEpoch() throws -> UInt64 {
        lock.lock(); defer { lock.unlock() }
        return max(processEpoch, durableEpoch)
    }
    func advance(to wanted: UInt64) throws {
        lock.lock(); defer { lock.unlock() }
        if failing { throw StoreFailure() }
        durableEpoch = max(durableEpoch, wanted)
        processEpoch = max(processEpoch, wanted)
    }
    func allowPersistence() { lock.lock(); failing = false; lock.unlock() }
    func durable() -> UInt64 { lock.lock(); defer { lock.unlock() }; return durableEpoch }
}

var checks = 0
func check(_ condition: Bool, _ name: String) {
    guard condition else {
        FileHandle.standardError.write(Data(("FAIL after \\(checks) account checks: " + name + "\\n").utf8))
        exit(1)
    }
    checks += 1
    print("✓ " + name)
}

let completeJSON = """
{"status":"complete","account_id":"acct-1","machine_id":"machine-1",
 "machine_credential":"bearer-fixture"}
"""

func isAbandoned(_ error: Error) -> Bool {
    (error as? CloudAccountError) == .loginAbandoned
}

func isCleanupPending(_ error: Error) -> Bool {
    guard case .credentialCleanupPending = error as? CloudAccountError else { return false }
    return true
}

Task {
    let defaultsSuite = "clawdline-focused-invalidation-" + UUID().uuidString
    let crossedDefaults = InterleavingDefaults(suiteName: defaultsSuite)!
    let lowerEpochStore = CloudCredentialInvalidationDefaultsStore(
        defaults: crossedDefaults, key: "epoch",
        persistenceNamespace: defaultsSuite + ".interleaving")
    let higherEpochStore = CloudCredentialInvalidationDefaultsStore(
        defaults: crossedDefaults, key: "epoch",
        persistenceNamespace: defaultsSuite + ".interleaving")
    let lowerEpochWrite = Task.detached { try lowerEpochStore.advance(to: 3) }
    check(crossedDefaults.waitForLowSet(),
          "the lower epoch reaches the held persistence seam")
    let higherEpochWrite = Task.detached { try higherEpochStore.advance(to: 9) }
    let higherOvertook = crossedDefaults.highSetOvertookLow()
    crossedDefaults.releaseLow()
    try await lowerEpochWrite.value
    try await higherEpochWrite.value
    check(!higherOvertook,
          "separate stores share one process-wide invalidation coordinator")
    check(try higherEpochStore.currentEpoch() == 9,
          "a lower cross-instance write cannot regress the durable minimum")

    let reservationKey = "reservation-epoch"
    let reservationNamespace = defaultsSuite + ".reservation"
    let lowReservationStore = CloudCredentialInvalidationDefaultsStore(
        defaults: crossedDefaults, key: reservationKey,
        persistenceNamespace: reservationNamespace)
    let highReservationStore = CloudCredentialInvalidationDefaultsStore(
        defaults: crossedDefaults, key: reservationKey,
        persistenceNamespace: reservationNamespace)
    _ = lowReservationStore.reserve(atLeast: 3)
    _ = highReservationStore.reserve(atLeast: 9)
    try lowReservationStore.advance(to: 3)
    check((crossedDefaults.object(forKey: reservationKey) as? NSNumber)?.uint64Value == 9,
          "a low advance persists the higher cross-instance reservation")
    let intermediateCredentialStore = CloudInMemoryKeyStore(values: [
        CloudAccountClient.machineCredentialAccount: try! JSONEncoder().encode(
            CloudMachineCredential(accountID: "acct-middle", machineID: "machine-middle",
                                   secret: "secret", validityEpoch: 5))
    ])
    let freshReservationClient = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test")!, transport: Wire(),
        credentialStore: intermediateCredentialStore,
        invalidationStore: CloudCredentialInvalidationDefaultsStore(
            defaults: crossedDefaults, key: reservationKey,
            persistenceNamespace: reservationNamespace),
        deviceKeyLoader: { CloudDeviceKeyPair() })
    check(try freshReservationClient.restoredMachineIdentity() == nil,
          "a fresh client rejects an intermediate credential under the durable high floor")
    crossedDefaults.removePersistentDomain(forName: defaultsSuite)

    let failureCredentialStore = CloudInMemoryKeyStore(values: [
        CloudAccountClient.machineCredentialAccount: try! JSONEncoder().encode(
            CloudMachineCredential(accountID: "acct-fail", machineID: "machine-fail",
                                   secret: "secret"))
    ])
    let failingInvalidation = FailingInvalidationStore()
    let failureClient = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test")!, transport: Wire(),
        credentialStore: failureCredentialStore, invalidationStore: failingInvalidation,
        deviceKeyLoader: { CloudDeviceKeyPair() })
    let failedReservation = failureClient.reservePendingLoginInvalidation()
    do {
        try failureClient.persistPendingLoginInvalidation(failedReservation)
        check(false, "invalidation persistence failure is surfaced")
    } catch {
        check((error as? CloudAccountError)?.errorDescription?.contains("restart") == true,
              "invalidation persistence failure names the restart risk")
    }
    check(try failureClient.restoredMachineIdentity() == nil,
          "failed persistence still invalidates the credential in this process")
    let restartBeforeRetry = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test")!, transport: Wire(),
        credentialStore: failureCredentialStore,
        invalidationStore: CloudInMemoryCredentialInvalidationStore(
            epoch: failingInvalidation.durable()),
        deviceKeyLoader: { CloudDeviceKeyPair() })
    check(try restartBeforeRetry.restoredMachineIdentity() != nil,
          "durable failure remains truthfully restart-unsafe")
    failingInvalidation.allowPersistence()
    try failureClient.persistPendingLoginInvalidation(failedReservation)
    let restartAfterRetry = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test")!, transport: Wire(),
        credentialStore: failureCredentialStore,
        invalidationStore: CloudInMemoryCredentialInvalidationStore(
            epoch: failingInvalidation.durable()),
        deviceKeyLoader: { CloudDeviceKeyPair() })
    check(try restartAfterRetry.restoredMachineIdentity() == nil,
          "retrying the same reservation closes the restart window")

    let wire = Wire()
    let store = RaceStore()
    let invalidation = CloudInMemoryCredentialInvalidationStore()
    let key = try! CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 0x31, count: 32))
    let client = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test")!,
        transport: wire, credentialStore: store, invalidationStore: invalidation,
        deviceKeyLoader: { key })

    // 1. Sign-out while the poll is on the wire. The answer is real and still refused.
    await wire.answer(completeJSON, blocking: true)
    let duringPoll = Task { try await client.pollDeviceLogin(deviceCode: "during-poll") }
    await wire.waitUntilBlocked()
    try client.signOut()
    await wire.resume()
    do {
        _ = try await duringPoll.value
        check(false, "a credential minted after sign-out is refused")
    } catch {
        check(isAbandoned(error), "a credential minted after sign-out is refused")
    }
    check(try store.data(for: CloudAccountClient.machineCredentialAccount) == nil,
          "sign-out during a poll leaves no credential behind")
    // Not the same claim as the line above. Undoing a write is a second-best outcome: the
    // secret has already been on disk. The refusal has to happen before the store is touched.
    check(store.writeCount() == 0,
          "the refused credential is never written, not written and then taken back")

    // 2. Abandoned *inside* the write window, which the pre-write guard alone cannot see.
    store.arm()
    await wire.answer(completeJSON, blocking: false)
    let admitted = client.credentialGeneration()
    let raced = Task {
        try await client.pollDeviceLogin(deviceCode: "inside-write", startedAt: admitted)
    }
    store.waitUntilParked()
    check(try store.data(for: CloudAccountClient.machineCredentialAccount) != nil,
          "the raced credential really is in the store while the write is held open")
    try client.invalidatePendingLogins()
    store.release()
    do {
        _ = try await raced.value
        check(false, "a credential abandoned inside the write window is refused")
    } catch {
        check(isAbandoned(error), "a credential abandoned inside the write window is refused")
    }
    check(try store.data(for: CloudAccountClient.machineCredentialAccount) == nil,
          "the write that had already landed is taken back rather than left behind")

    // 3. The cleanup itself can fail. The durable nonsecret epoch, not deletion luck, makes the
    //    already-landed credential unusable after a brand-new client simulates restart.
    store.arm()
    store.setRemovalFailure(true)
    await wire.answer(completeJSON, blocking: false)
    let cleanupRace = Task {
        try await client.pollDeviceLogin(
            deviceCode: "cleanup-fails", startedAt: client.credentialGeneration())
    }
    store.waitUntilParked()
    try client.invalidatePendingLogins()
    store.release()
    do {
        _ = try await cleanupRace.value
        check(false, "cleanup failure is surfaced to its retry owner")
    } catch {
        check(isCleanupPending(error), "cleanup failure is surfaced to its retry owner")
    }
    check(try store.data(for: CloudAccountClient.machineCredentialAccount) != nil,
          "the failure injection really leaves stale credential bytes in the store")
    let restarted = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test")!,
        transport: wire, credentialStore: store, invalidationStore: invalidation,
        deviceKeyLoader: { key })
    check(try restarted.restoredMachineIdentity() == nil,
          "the durable epoch keeps stale bytes invalid after restart")
    store.setRemovalFailure(false)
    try restarted.signOut()
    check(try store.data(for: CloudAccountClient.machineCredentialAccount) == nil,
          "the named retry owner can finish stale credential cleanup")

    // 4. Not a blanket refusal: a sign-in begun after the abandonment stores normally.
    await wire.answer(completeJSON, blocking: false)
    _ = try await client.pollDeviceLogin(
        deviceCode: "after", startedAt: client.credentialGeneration())
    check(try client.restoredMachineIdentity()?.machineID == "machine-1",
          "a sign-in started after the abandonment stores its credential normally")

    print("\\(checks) account checks passed")
    exit(0)
}
dispatchMain()
`, "utf8");
  const accountCompile = run("swiftc", ["-swift-version", "5", "-target",
    "arm64-apple-macos13.0", keysSource, accountSource, accountHarness, "-framework", "Security", "-o", accountBinary]);
  check(accountCompile.status === 0,
    `the production credential client compiles focused: ${accountCompile.stderr}`);
  const accountProbe = run(accountBinary);
  process.stdout.write(accountProbe.stdout);
  process.stderr.write(accountProbe.stderr);
  check(accountProbe.status === 0 && /20 account checks passed/.test(accountProbe.stdout),
    "durable invalidation survives cleanup failure and restart while fresh login still persists");

  // The Settings state machine, with the production wiring underneath it. Its question is the
  // one a `Task {}` inside a `@MainActor` class cannot answer for itself: spelling a call async
  // does not move it off the main actor, so the removal is followed all the way to the store.
  const settingsWork = join(work, "settings");
  mkdirSync(settingsWork);
  const settingsHarness = join(settingsWork, "main.swift");
  const settingsBinary = join(work, "settings-main");
  writeFileSync(settingsHarness, `
import Foundation

struct CloudAPIDeviceTokenProvider: Sendable {
    typealias AuthorizationHeaderProvider = @Sendable () throws -> String
    init(apiBaseURL: URL, session: URLSession, authorizationHeader: @escaping AuthorizationHeaderProvider) {}
}

private struct Refused: Error, LocalizedError {
    var errorDescription: String? { "the Keychain refused" }
}

/// Records the thread the removal actually ran on, which is the only thing that settles it.
final class ThreadWatchingStore: CloudKeyStoring, @unchecked Sendable {
    let coordinator = CloudKeyStoreCoordinator()
    private let lock = NSLock()
    private var values: [String: Data] = [:]
    private var removedOnMainThread: Bool?

    func data(for account: String) throws -> Data? {
        lock.lock(); defer { lock.unlock() }
        return values[account]
    }
    func set(_ data: Data, for account: String) throws {
        lock.lock(); values[account] = data; lock.unlock()
    }
    func remove(_ account: String) throws {
        lock.lock()
        if removedOnMainThread == nil { removedOnMainThread = Thread.isMainThread }
        values.removeValue(forKey: account)
        lock.unlock()
    }
    func seed(_ data: Data, for account: String) {
        lock.lock(); values[account] = data; lock.unlock()
    }
    func removalThread() -> Bool? { lock.lock(); defer { lock.unlock() }; return removedOnMainThread }
}

var checks = 0
func check(_ condition: @autoclosure () -> Bool, _ name: String) {
    guard condition() else {
        FileHandle.standardError.write(Data(("FAIL after \\(checks) settings checks: " + name + "\\n").utf8))
        exit(1)
    }
    checks += 1
    print("✓ " + name)
}

func drain(_ seconds: TimeInterval, until predicate: () -> Bool) {
    let deadline = Date().addingTimeInterval(seconds)
    while !predicate() && Date() < deadline {
        RunLoop.main.run(until: Date().addingTimeInterval(0.01))
    }
}

let metadata = CloudMachineMetadata(name: "Focused Mac", platform: "macos")
let identity = CloudMachineIdentity(accountID: "acct-1", machineID: "machine-1")

MainActor.assumeIsolated {
    // 1. A removal that has not answered leaves a busy phase, not a frozen window.
    var pending: (@MainActor (CloudKeychainWriteOutcome) -> Void)?
    let deferredServices = CloudSettingsServices(
        restoredIdentity: { identity },
        startLogin: { _ in throw Refused() },
        signOut: { _, completion in pending = completion },
        openVerificationURL: { _ in true })
    let model = CloudSettingsModel(services: deferredServices, metadata: metadata)
    drain(1) { model.phase == .connected(identity: identity, origin: .restored) }
    check(model.phase == .connected(identity: identity, origin: .restored),
          "the restored identity is published before sign-out is offered")

    let startedAt = Date()
    model.signOut()
    check(Date().timeIntervalSince(startedAt) < 0.1,
          "pressing sign out returns to the run loop immediately")
    check(model.phase == .signingOut(identity: identity),
          "an unanswered removal shows its own phase rather than looking idle")

    // 2. Timeout is progress, not proof that deletion failed. Keep the late terminal callback.
    let timedOut = CloudKeychainWriteOutcome.timedOut(seconds: CloudKeychainWriter.defaultTimeoutSeconds)
    pending?(timedOut)
    check(model.phase == .signOutReconciliation(identity: identity, message: timedOut.message),
          "a timed-out removal is unknown and still reconciling, not a claimed failure")

    // 3. Retry does not erase the first operation's authority over the same store. Either
    //    terminal success proves the credential is gone and must detach the bridge/model.
    let firstAttempt = pending
    model.signOut()
    check(model.phase == .signingOut(identity: identity), "retrying re-enters the busy phase")
    firstAttempt?(.succeeded)
    check(model.phase == .signedOut,
          "the first removal's late success settles the retrying model")
    pending?(.succeeded)
    check(model.phase == .signedOut, "a redundant terminal success cannot undo settlement")

    // 4. Production wiring: follow the real removal to the store and ask which thread ran it.
    let store = ThreadWatchingStore()
    store.seed(Data(#"{"account_id":"acct-1","machine_id":"machine-1","machine_credential":"s"}"#.utf8),
               for: CloudAccountClient.machineCredentialAccount)
    let client = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.test")!,
        transport: CloudAccountURLSessionTransport(session: .shared),
        credentialStore: store, deviceKeyLoader: { CloudDeviceKeyPair() })
    let production = CloudSettingsServices.production(client: client, openVerificationURL: { _ in true })
    var productionOutcome: CloudKeychainWriteOutcome.Kind?
    let productionReservation = production.reserveCredentialInvalidation()
    production.signOut(productionReservation) { outcome in productionOutcome = outcome.kind }
    check(store.removalThread() == nil,
          "the production removal has not run by the time the caller gets control back")
    drain(5) { productionOutcome != nil }
    check(productionOutcome == .succeeded, "the production removal answers on the main queue")
    check(store.removalThread() == false,
          "the production removal ran off the main thread, not merely inside a Task")
}
print("\\(checks) settings checks passed")
`, "utf8");
  const settingsCompile = run("swiftc", ["-swift-version", "5", "-target",
    "arm64-apple-macos13.0", keysSource, accountSource, settingsSource, settingsHarness,
    "-framework", "Security", "-o", settingsBinary]);
  check(settingsCompile.status === 0,
    `the Settings sign-out state machine compiles focused: ${settingsCompile.stderr}`);
  const settingsProbe = run(settingsBinary);
  process.stdout.write(settingsProbe.stdout);
  process.stderr.write(settingsProbe.stderr);
  check(settingsProbe.status === 0 && /10 settings checks passed/.test(settingsProbe.stdout),
    "sign-out is bounded, generation-checked and never removes on the main thread");

  // **One job, explicitly, because this runs outside the machine lock.** It is a whole-`Sources/`
  // typecheck of 104 files and the second most expensive thing in `./test.sh`: 34 s at one job and
  // 7 s at eight, measured. Seven seconds is the tempting number and it is the wrong one — it buys
  // 27 s by putting eight `swift-frontend` processes beside whoever is currently holding the lock,
  // and the lock exists precisely to stop this machine having several compiles at once. `-j 1` is
  // stated rather than left to the driver's default: the default is one *on this Mac*, measured,
  // and a default is not a rule.
  const typecheckArgs = ["swiftc", "-swift-version", "5", "-target",
    "arm64-apple-macos13.0", "-typecheck", "-j", "1", ...swiftSources("Sources")];
  const productionTypecheck = run("xcrun", typecheckArgs);
  check(productionTypecheck.status === 0,
    `production wiring compiles only through the asynchronous reader: ${productionTypecheck.stderr}`);
  // Asserted against the argument vector actually handed to `xcrun`, not against the array it was
  // built from, because the failure is silent either way round: a width that never reaches the
  // command line reads exactly like one that did.
  const jFlag = typecheckArgs.indexOf("-j");
  check(jFlag > 0 && typecheckArgs[jFlag + 1] === "1",
    "a compile above the machine lock runs at one job, and says so on the command line");

  const fakeBin = join(work, "fake-bin");
  mkdirSync(fakeBin);
  const fakeState = join(work, "identity-count");
  const securityLog = join(work, "security.log");
  const statusLog = join(work, "keychain-status.log");
  executable(join(fakeBin, "security"), `#!/bin/bash
set -e
printf '%s\n' "$*" >> "\${FAKE_SECURITY_LOG:-/dev/null}"
case "$1" in
  show-keychain-info)
    case "\${FAKE_KEYCHAIN_MODE:-unlocked}" in
      locked) echo "SecKeychainCopySettings: User interaction is not allowed." >&2; exit 51 ;;
      hang) sleep 30 ;;
      *) printf 'Keychain "%s" lock-on-sleep timeout=300s\\n' "$2" ;;
    esac
    ;;
  set-key-partition-list)
    printf '%s\\n' "$*" >> "\${FAKE_PARTITION_LOG:-/dev/null}"
    case "\${FAKE_PARTITION_MODE:-ok}" in
      failure) echo "SecKeychainItemSetAccess: User interaction is not allowed." >&2; exit 51 ;;
      hang) sleep 30 ;;
      child124) echo "partition child exited 124" >&2; exit 124 ;;
      *) ;;
    esac
    ;;
  find-identity)
    case "\${FAKE_SECURITY_MODE:-normal}" in
      failure) exit 45 ;;
      duplicate)
        printf '  1) ABCDEF0123456789ABCDEF0123456789ABCDEF01 "Clawdline Local Development"\\n'
        printf '  2) 2222222222222222222222222222222222222222 "Clawdline Local Development"\\n'
        ;;
      invalid)
        if [ -s "$FAKE_SECURITY_STATE" ]; then
          printf '  1) ABCDEF0123456789ABCDEF0123456789ABCDEF01 "Clawdline Local Development"\\n'
        else
          printf '     0 valid identities found\\n'
        fi
        ;;
      *)
        if [ -s "$FAKE_SECURITY_STATE" ]; then
          printf '  1) ABCDEF0123456789ABCDEF0123456789ABCDEF01 "Clawdline Local Development"\\n'
        fi
        ;;
    esac
    ;;
  import)
    case "\${FAKE_IMPORT_MODE:-ok}" in
      hang)
        printf '1' > "$FAKE_SECURITY_STATE"
        sleep 30
        ;;
      failure) echo "fake import failure" >&2; exit 52 ;;
      child124) echo "fake import child 124" >&2; exit 124 ;;
    esac
    count=0
    [ ! -s "$FAKE_SECURITY_STATE" ] || count=$(cat "$FAKE_SECURITY_STATE")
    printf '%s' $((count + 1)) > "$FAKE_SECURITY_STATE"
    ;;
  add-trusted-cert) ;;
  *) exit 2 ;;
esac
`);
  const fakeKeychainStatus = join(fakeBin, "keychain-status");
  executable(fakeKeychainStatus, `#!/bin/bash
printf '%s\n' "$*" >> "\${FAKE_KEYCHAIN_STATUS_LOG:-/dev/null}"
case "\${FAKE_KEYCHAIN_MODE:-unlocked}" in
  locked) echo "locked" >&2; exit 3 ;;
  hang) sleep 30 ;;
  child124) echo "helper deliberately exited 124" >&2; exit 124 ;;
  *) echo "unlocked" ;;
esac
`);
  executable(join(fakeBin, "openssl"), `#!/bin/bash
while [ "$#" -gt 0 ]; do
  case "$1" in
    -keyout|-out) shift; printf fixture > "$1" ;;
  esac
  shift
done
`);
  const fakeKeychain = join(work, "login.keychain-db");
  writeFileSync(fakeKeychain, "fixture", "utf8");
  const setupEnv = {
    PATH: `${fakeBin}${delimiter}${process.env.PATH}`,
    FAKE_SECURITY_STATE: fakeState,
    CLAWDLINE_LOCAL_SIGN_KEYCHAIN: fakeKeychain,
    TMPDIR: work,
  };
  // The task must not execute the real setup script, even against a fake PATH. Syntax-check it
  // and exercise only its extracted helpers below.
  const setup = readFileSync(setupSource, "utf8");
  const setupSyntax = run("bash", ["-n", setupSource]);
  check(setupSyntax.status === 0, `setup script has valid Bash syntax: ${setupSyntax.stderr}`);
  check(setup.includes('security find-identity -v -p codesigning "$KEYCHAIN"') &&
        setup.includes('security import "$work/identity.p12" -k "$KEYCHAIN"'),
    "setup discovery and mutation both hold the explicit Keychain path still");
  writeFileSync(fakeState, "1", "utf8");

  const build = readFileSync(buildSource, "utf8");
  const bounded = marked(build, "bounded signing commands");
  const selection = marked(build, "signing identity selection");
  const branches = marked(build, "signing branches");
  const selectionScript = join(work, "select-signing.sh");
  executable(selectionScript, `#!/bin/bash
set -euo pipefail
LOCAL_SIGN_IDENTITY_NAME="Clawdline Local Development"
LOCAL_SIGNING=0
${bounded}
${selection}
printf 'RESULT=%s|%s\\n' "$SIGN_IDENTITY" "$LOCAL_SIGNING"
`);
  const selectEnv = { PATH: `${fakeBin}${delimiter}${process.env.PATH}`,
    FAKE_SECURITY_STATE: fakeState,
    FAKE_SECURITY_LOG: securityLog, FAKE_KEYCHAIN_STATUS_LOG: statusLog,
    CLAWDLINE_KEYCHAIN_STATUS_HELPER: fakeKeychainStatus,
    CLAWDLINE_LOCAL_SIGN_KEYCHAIN: fakeKeychain };
  writeFileSync(securityLog, "", "utf8");
  writeFileSync(statusLog, "", "utf8");
  const automaticEnv = { ...process.env, ...selectEnv };
  delete automaticEnv.CLAWDLINE_SIGN_IDENTITY;
  const automatic = spawnSync(selectionScript, { encoding: "utf8", env: automaticEnv });
  check(automatic.status === 0 &&
        automatic.stdout.includes("RESULT=ABCDEF0123456789ABCDEF0123456789ABCDEF01|1"),
    "build selection prefers the stable local identity when no override exists");
  check(readFileSync(securityLog, "utf8").trim() ===
        `find-identity -v -p codesigning ${fakeKeychain}` &&
        readFileSync(statusLog, "utf8").trim() === fakeKeychain,
    "identity discovery and unlock status hold the same explicit Keychain path still");
  const developer = run(selectionScript, [], {
    ...selectEnv, CLAWDLINE_SIGN_IDENTITY: "Developer ID Application: Example" });
  check(developer.status === 0 &&
        developer.stdout.includes("RESULT=Developer ID Application: Example|0"),
    "CLAWDLINE_SIGN_IDENTITY still wins exactly over local discovery");
  const explicitAdhoc = run(selectionScript, [], {
    ...selectEnv, CLAWDLINE_SIGN_IDENTITY: "-" });
  check(explicitAdhoc.status === 0 && explicitAdhoc.stdout.includes("RESULT=-|0"),
    "an explicit ad-hoc override remains ad-hoc")
  rmSync(fakeState);
  const absentEnv = { ...process.env, ...selectEnv };
  delete absentEnv.CLAWDLINE_SIGN_IDENTITY;
  const absent = spawnSync(selectionScript, { encoding: "utf8", env: absentEnv });
  check(absent.status !== 0 &&
        (absent.stdout + absent.stderr).includes("CLAWDLINE_SIGN_ADHOC=1"),
    "missing local identity fails and requires an explicit ad-hoc request");
  const inaccessible = run(selectionScript, [], {
    ...selectEnv, FAKE_SECURITY_MODE: "failure",
  });
  check(inaccessible.status !== 0 &&
        (inaccessible.stdout + inaccessible.stderr).includes("failed with status 45") &&
        (inaccessible.stdout + inaccessible.stderr).includes("CLAWDLINE_SIGN_ADHOC=1"),
    "a failed identity query fails closed and requires explicit ad-hoc signing");
  const ambiguous = run(selectionScript, [], {
    ...selectEnv, FAKE_SECURITY_MODE: "duplicate",
  });
  check(ambiguous.status !== 0 &&
        (ambiguous.stdout + ambiguous.stderr).includes("multiple valid"),
    "build refuses two valid identities with the same common name");

  // The case the old script had no answer for: the identity is there and the Keychain holding
  // it is locked, so codesign would find the key and then stop on an unlock dialog.
  writeFileSync(fakeState, "1", "utf8");
  const lockedEnv = { ...process.env, ...selectEnv, FAKE_KEYCHAIN_MODE: "locked" };
  delete lockedEnv.CLAWDLINE_SIGN_IDENTITY;
  const locked = spawnSync(selectionScript, { encoding: "utf8", env: lockedEnv });
  const lockedText = locked.stdout + locked.stderr;
  check(locked.status !== 0 && lockedText.includes("locked or unreadable"),
    "a locked login Keychain fails the build loudly instead of waiting on a dialog");
  check(lockedText.includes("security unlock-keychain") &&
        lockedText.includes("CLAWDLINE_SIGN_ADHOC=1"),
    "the locked refusal names both repairs and unlocks nothing itself");

  const hangingEnv = { ...process.env, ...selectEnv, FAKE_KEYCHAIN_MODE: "hang",
    CLAWDLINE_SIGN_QUERY_TIMEOUT: "1" };
  delete hangingEnv.CLAWDLINE_SIGN_IDENTITY;
  const hangingStarted = Date.now();
  const hanging = spawnSync(selectionScript, { encoding: "utf8", env: hangingEnv });
  const hangingSeconds = (Date.now() - hangingStarted) / 1000;
  check(hanging.status !== 0 &&
        (hanging.stdout + hanging.stderr).includes("did not answer within 1s"),
    "a Keychain probe that never answers is cut off and reported, not waited on");
  check(hangingSeconds < 20,
    `the bounded probe returns in about its own timeout, not the command's: ${hangingSeconds}s`);

  const child124 = run(selectionScript, [], {
    ...selectEnv, FAKE_KEYCHAIN_MODE: "child124",
  });
  const child124Text = child124.stdout + child124.stderr;
  check(child124.status !== 0 && child124Text.includes("status 124") &&
        !child124Text.includes("did not answer"),
    "a helper's own exit 124 is distinct from watchdog timeout");

  writeFileSync(securityLog, "", "utf8");
  const invalidTimeout = run(selectionScript, [], {
    ...selectEnv, CLAWDLINE_SIGN_QUERY_TIMEOUT: "0",
  });
  check(invalidTimeout.status === 2 &&
        (invalidTimeout.stdout + invalidTimeout.stderr).includes("must be a positive integer") &&
        readFileSync(securityLog, "utf8") === "",
    "invalid timeout input is rejected before any child command launches");

  const explicitAdhocContract = run(selectionScript, [], {
    ...selectEnv, CLAWDLINE_SIGN_ADHOC: "1", FAKE_KEYCHAIN_MODE: "locked",
  });
  check(explicitAdhocContract.status === 0 &&
        explicitAdhocContract.stdout.includes("RESULT=-|0") &&
        explicitAdhocContract.stdout.includes("by explicit request"),
    "CLAWDLINE_SIGN_ADHOC=1 is the documented way past a locked Keychain, and consults nothing");
  check(!explicitAdhocContract.stdout.includes("no stable local signing identity"),
    "the explicit ad-hoc path is not reported as a failure to find an identity");
  rmSync(fakeState);

  const signingScript = join(work, "signing-branches.sh");
  const codesignLog = join(work, "codesign.log");
  executable(join(fakeBin, "codesign"), `#!/bin/bash
printf '%s\\n' "$*" >> "$FAKE_CODESIGN_LOG"
case "\${FAKE_CODESIGN_MODE:-ok}" in
  hang) sleep 30 ;;
  failure) echo "fake codesign stderr" >&2; exit 19 ;;
  child124) echo "fake codesign child 124" >&2; exit 124 ;;
  *) ;;
esac
`);
  executable(signingScript, `#!/bin/bash
set -euo pipefail
BUNDLE_ID="com.tsunamiworks.clawdline"
LOCAL_SIGN_IDENTITY_NAME="Clawdline Local Development"
STAGED_APP="/tmp/Clawdline.app"
LOCAL_SIGN_KEYCHAIN="${fakeKeychain}"
${bounded}
${branches}
`);
  function signingArgs(identity, local, extra = {}) {
    writeFileSync(codesignLog, "", "utf8");
    const result = run(signingScript, [], {
      PATH: `${fakeBin}${delimiter}${process.env.PATH}`,
      FAKE_CODESIGN_LOG: codesignLog, SIGN_IDENTITY: identity, LOCAL_SIGNING: String(local),
      ...extra,
    });
    if (extra.expectFailure !== "1") {
      check(result.status === 0, `signing branch exits zero for ${identity}: ${result.stderr}`);
    }
    return { log: readFileSync(codesignLog, "utf8"), result };
  }
  const adhocArgs = signingArgs("-", 0);
  check(adhocArgs.log.includes("--sign - --identifier com.tsunamiworks.clawdline"),
    "ad-hoc fallback signs with the bundle identifier");
  const localArgs = signingArgs("LOCALHASH", 1);
  check(localArgs.log.includes(`--sign LOCALHASH --keychain ${fakeKeychain} --identifier com.tsunamiworks.clawdline`) &&
        !localArgs.log.includes("--timestamp") && !localArgs.log.includes("--options runtime") &&
        localArgs.result.stdout.includes("up to three Keychain prompts"),
    "stable local signing uses its identity without release-only options");
  const releaseArgs = signingArgs("Developer ID Application: Example", 0);
  check(releaseArgs.log.includes("--options runtime --timestamp --entitlements Resources/Clawdline.entitlements"),
    "Developer ID keeps hardened runtime, timestamp, and release entitlements");

  // A codesign that stops on a Keychain access dialog is the failure nobody can see: the dialog
  // may be behind another window. Bounded, it becomes a sentence naming the repair.
  const hangingSignStarted = Date.now();
  const hangingSign = signingArgs("LOCALHASH", 1, {
    FAKE_CODESIGN_MODE: "hang", CLAWDLINE_CODESIGN_TIMEOUT: "1", expectFailure: "1",
  });
  const hangingSignSeconds = (Date.now() - hangingSignStarted) / 1000;
  const hangingSignText = hangingSign.result.stdout + hangingSign.result.stderr;
  check(hangingSign.result.status !== 0 &&
        hangingSignText.includes("codesign did not finish within 1s"),
    "a codesign waiting on a Keychain dialog is cut off and named");
  check(hangingSignText.includes("set-key-partition-list") &&
        hangingSignText.includes("requires '-k password'") &&
        hangingSignText.includes("does not accept or pass"),
    "the timeout states the actual SecurityTool password contract without claiming a prompt");
  check(hangingSignSeconds < 20,
    `bounded codesign returns near its own timeout: ${hangingSignSeconds}s`);

  const failedAdhoc = signingArgs("-", 0, {
    FAKE_CODESIGN_MODE: "failure", expectFailure: "1",
  });
  const failedAdhocText = failedAdhoc.result.stdout + failedAdhoc.result.stderr;
  check(failedAdhoc.result.status === 19 &&
        failedAdhocText.includes("fake codesign stderr") &&
        failedAdhocText.includes("failed with status 19"),
    "ordinary ad-hoc codesign failure preserves stderr and exit status");

  const adhocChild124 = signingArgs("-", 0, {
    FAKE_CODESIGN_MODE: "child124", expectFailure: "1",
  });
  const adhocChild124Text = adhocChild124.result.stdout + adhocChild124.result.stderr;
  check(adhocChild124.result.status === 124 &&
        adhocChild124Text.includes("failed with status 124") &&
        !adhocChild124Text.includes("timed out"),
    "codesign child exit 124 is not rendered as watchdog timeout");

  // Exercise extracted setup helpers only; the real setup script is never executed by this task.
  const setupValidation = marked(setup, "setup timeout validation");
  const setupBounded = marked(setup, "bounded setup commands");
  const setupPartition = marked(setup, "partition list contract");
  const setupHelperScript = join(work, "setup-command-contract.sh");
  executable(setupHelperScript, `#!/bin/bash
set -euo pipefail
IDENTITY_NAME="Clawdline Local Development"
KEYCHAIN="${fakeKeychain}"
SETUP_TIMEOUT="\${CLAWDLINE_SIGN_QUERY_TIMEOUT:-30}"
PARTITION_TIMEOUT="\${CLAWDLINE_PARTITION_LIST_TIMEOUT:-120}"
${setupValidation}
${setupBounded}
probe_out="\${TMPDIR:-/tmp}/setup-helper-output"
${setupPartition}
case "\${1:-report}" in
  report) report_partition_list_contract ;;
  import)
    status=0
    clawdline_bounded "$PARTITION_TIMEOUT" "$probe_out" \
      security import fixture.p12 -k "$KEYCHAIN" -P fixture || status=$?
    if [ "$status" -ne 0 ]; then
      cat "$probe_out" >&2
      if [ "$CLAWDLINE_BOUNDED_OUTCOME" = timeout ]; then
        echo "Import state is unknown: the identity may have landed." >&2
      else
        echo "import failed with exit $status" >&2
      fi
      exit "$status"
    fi
    ;;
esac
`);
  const helperEnv = { ...setupEnv, FAKE_SECURITY_LOG: securityLog };
  writeFileSync(securityLog, "", "utf8");
  const partitionReport = run(setupHelperScript, ["report"], helperEnv);
  check(partitionReport.status === 0 && readFileSync(securityLog, "utf8") === "",
    "partition-list guidance executes no SecurityTool mutation");
  check(partitionReport.stdout.includes("requires '-k password'") &&
        partitionReport.stdout.includes("does not accept or") &&
        partitionReport.stdout.includes("SecurityTool manually"),
    "setup states the real required-password contract and hands ownership to the person");
  check(!setup.includes("security set-key-partition-list -S") &&
        setup.includes("--set-partition-list is unsupported") &&
        setup.indexOf("--set-partition-list is unsupported") < setup.indexOf("identity_hashes()"),
    "the legacy partition option refuses before discovery and no executable mutation remains");

  writeFileSync(securityLog, "", "utf8");
  const invalidSetupTimeout = run(setupHelperScript, ["import"], {
    ...helperEnv, CLAWDLINE_PARTITION_LIST_TIMEOUT: "zero",
  });
  check(invalidSetupTimeout.status === 2 &&
        (invalidSetupTimeout.stdout + invalidSetupTimeout.stderr).includes("positive integer") &&
        readFileSync(securityLog, "utf8") === "",
    "setup timeout validation fails before launching a mutation");

  writeFileSync(fakeState, "", "utf8");
  const importTimedOut = run(setupHelperScript, ["import"], {
    ...helperEnv, FAKE_IMPORT_MODE: "hang", CLAWDLINE_PARTITION_LIST_TIMEOUT: "1",
  });
  const importTimedOutText = importTimedOut.stdout + importTimedOut.stderr;
  check(importTimedOut.status === 124 && importTimedOutText.includes("state is unknown") &&
        readFileSync(fakeState, "utf8") === "1",
    "a timed-out setup mutation reports unknown state after the fake has landed");

  const importChild124 = run(setupHelperScript, ["import"], {
    ...helperEnv, FAKE_IMPORT_MODE: "child124",
  });
  const importChild124Text = importChild124.stdout + importChild124.stderr;
  check(importChild124.status === 124 && importChild124Text.includes("exit 124") &&
        !importChild124Text.includes("state is unknown"),
    "setup child exit 124 remains distinct from watchdog timeout");

  console.log(`${nodeChecks} node checks passed`);
} finally {
  rmSync(work, { recursive: true, force: true });
}
