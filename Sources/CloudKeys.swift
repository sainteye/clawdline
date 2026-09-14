// Cloud key material and its persistence boundary.
//
// Keychain item names (generic-password class):
//   service: app.clawdline.cloud.keys
//   account: device-ed25519-v1       (32-byte CryptoKit private-key seed)
//   account: account-master-secret-v1 (32 random bytes)
//
// Recovery codes are `CLAWD1-` followed by grouped RFC 4648 base32. The encoded
// bytes are the 32-byte master secret followed by the first four bytes of
// SHA-256("clawdline-recovery-v1" || secret). The code is a lossless,
// user-held backup of the secret, not a password or a server escrow mechanism.

import Foundation
#if canImport(CryptoKit)
import CryptoKit
#elseif canImport(Crypto)
import Crypto
#else
#error("CloudKeys requires CryptoKit or swift-crypto")
#endif
#if canImport(Security)
import Security
#endif
#if canImport(ClawdlineApplication) && !CLAWDLINE_APPLICATION_TARGET
import ClawdlineApplication // W3-1 correction: real cross-module import, see Sources/HostPorts.swift
#endif

public enum CloudKeyError: Error, LocalizedError, Equatable {
    case invalidDevicePrivateKey
    case invalidMasterSecretLength(Int)
    case invalidRecoveryCode
    case recoveryChecksumMismatch
    case mainThreadReadForbidden
    case mainThreadWriteForbidden
    case operationTimedOut(seconds: Int)
    case keychain(Int32)

    public var errorDescription: String? {
        switch self {
        case .invalidDevicePrivateKey:
            return "The Ed25519 private key is not a valid 32-byte CryptoKit key."
        case .invalidMasterSecretLength(let count):
            return "The account master secret must be 32 bytes, not \(count)."
        case .invalidRecoveryCode:
            return "The recovery code is not valid CLAWD1 base32."
        case .recoveryChecksumMismatch:
            return "The recovery code checksum does not match."
        case .mainThreadReadForbidden:
            return "A Keychain read was rejected on the main thread."
        case .mainThreadWriteForbidden:
            return "A Keychain write was rejected on the main thread."
        case .operationTimedOut(let seconds):
            return "The Keychain did not answer within \(seconds) seconds. Its state is unknown; "
                + "Clawdline is still waiting for the terminal result."
        case .keychain(let status):
            return "Keychain operation failed with OSStatus \(status)."
        }
    }
}

/// Small portable lock used by the Application target. `OSAllocatedUnfairLock` is a macOS
/// implementation detail and cannot be part of the Ubuntu identity boundary.
final class CloudLocked<State>: @unchecked Sendable {
    private let lock = NSLock()
    private var state: State

    init(_ state: State) { self.state = state }

    func withLock<Result>(_ body: (inout State) throws -> Result) rethrows -> Result {
        lock.lock()
        defer { lock.unlock() }
        return try body(&state)
    }
}

/// The UI adapter from synchronous Keychain APIs into app state. The operation is captured
/// privately and can only be started on its dedicated serial queue, so a caller cannot
/// accidentally invoke it on the main actor while trying to refresh a screen.
public final class CloudKeychainReader<Value: Sendable>: @unchecked Sendable {
    public static var defaultTimeoutSeconds: Int { 10 }

    public enum AwaitError: Error, Equatable {
        case timedOut(seconds: Int)
    }

    @MainActor
    fileprivate final class Delivery {
        private var timedOut = false
        private var terminal = false
        private var cancelled = false

        func timeout(_ seconds: Int, callback: @MainActor (Int) -> Void) {
            guard !cancelled, !terminal, !timedOut else { return }
            timedOut = true
            callback(seconds)
        }

        func finish(
            _ result: Result<Value, Error>,
            callback: @MainActor (Result<Value, Error>) -> Void
        ) {
            guard !cancelled, !terminal else { return }
            terminal = true
            callback(result)
        }

        func cancel() { cancelled = true }
    }

    @MainActor
    public final class Handle {
        private let delivery: Delivery
        fileprivate init(delivery: Delivery) { self.delivery = delivery }
        public func cancel() { delivery.cancel() }
    }

    /// One-shot async spelling for flows where a timeout must return control to the caller.
    /// The synchronous Security call cannot be cancelled, but its result is detached from this
    /// await after timeout/cancellation so a pairing sheet can close immediately and truthfully.
    @MainActor
    private final class AwaitedDelivery {
        private var continuation: CheckedContinuation<Value, Error>?
        private var handle: Handle?
        private var finished = false

        func register(_ continuation: CheckedContinuation<Value, Error>) {
            self.continuation = continuation
        }

        func attach(_ handle: Handle) {
            self.handle = handle
            if finished { handle.cancel() }
        }

        func finish(_ result: Result<Value, Error>) {
            guard !finished, let continuation else { return }
            finished = true
            self.continuation = nil
            handle?.cancel()
            continuation.resume(with: result)
        }
    }

    private let queue: DispatchQueue
    private let timeoutSeconds: Int
    private let operation: @Sendable () throws -> Value

    public init(
        label: String = "clawdline.cloud.keychain-read",
        timeoutSeconds: Int = CloudKeychainReader.defaultTimeoutSeconds,
        operation: @escaping @Sendable () throws -> Value
    ) {
        queue = DispatchQueue(label: label, qos: .utility)
        self.timeoutSeconds = max(1, timeoutSeconds)
        self.operation = operation
    }

    /// Timeout is observable progress, not terminal failure. The late result is retained for
    /// reconciliation, and cancellation suppresses delivery without claiming the Security call
    /// itself stopped.
    @discardableResult
    @MainActor
    public func read(
        onStart: () -> Void = {},
        onTimeout: @escaping @MainActor (Int) -> Void = { _ in },
        completion: @escaping @MainActor (Result<Value, Error>) -> Void
    ) -> Handle {
        onStart()
        let delivery = Delivery()
        queue.async { [operation] in
            let result = Result { try operation() }
            DispatchQueue.main.async { delivery.finish(result, callback: completion) }
        }
        let seconds = timeoutSeconds
        DispatchQueue.main.asyncAfter(deadline: .now() + .seconds(seconds)) {
            delivery.timeout(seconds, callback: onTimeout)
        }
        return Handle(delivery: delivery)
    }

    /// Await the first terminal UI answer: the Security result, a timeout, or cancellation.
    /// This deliberately differs from callback-based restore, which retains a late terminal
    /// value for reconciliation after publishing timeout progress.
    @MainActor
    public func value() async throws -> Value {
        let awaited = AwaitedDelivery()
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                awaited.register(continuation)
                guard !Task.isCancelled else {
                    awaited.finish(.failure(CancellationError()))
                    return
                }
                let handle = read(
                    onTimeout: { seconds in
                        awaited.finish(.failure(AwaitError.timedOut(seconds: seconds)))
                    },
                    completion: { result in awaited.finish(result) })
                awaited.attach(handle)
            }
        } onCancel: {
            Task { @MainActor in
                awaited.finish(.failure(CancellationError()))
            }
        }
    }
}

/// What a bounded Keychain mutation did, as one value a screen can render.
///
/// `timedOut` is a distinct case rather than an `Error`, because it is the one outcome where
/// nothing is yet known: the Security call is still running on its own queue and may still
/// succeed. Collapsing it into `failed` would tell somebody their sign-out did not happen when
/// it may be about to.
public enum CloudKeychainWriteOutcome: Sendable {
    case succeeded
    case failed(Error)
    case timedOut(seconds: Int)

    /// The shape only, for a test or a log line that must not depend on an error's spelling.
    public enum Kind: String, Equatable, Sendable { case succeeded, failed, timedOut }

    public var kind: Kind {
        switch self {
        case .succeeded: return .succeeded
        case .failed: return .failed
        case .timedOut: return .timedOut
        }
    }

    /// The sentence a person reads. Never empty, so a screen cannot render a blank failure.
    public var message: String {
        switch self {
        case .succeeded:
            return "Done."
        case .failed(let error):
            if let localized = error as? LocalizedError,
               let description = localized.errorDescription, !description.isEmpty {
                return description
            }
            let described = error.localizedDescription
            return described.isEmpty ? "\(error)" : described
        case .timedOut(let seconds):
            return "The Keychain did not answer within \(seconds) seconds. "
                + "Its state is unknown; Clawdline is still waiting to reconcile the final "
                + "result. It may be locked, and you may retry without cancelling that check."
        }
    }

    public var isTerminal: Bool {
        switch self {
        case .succeeded, .failed: return true
        case .timedOut: return false
        }
    }
}

/// A timeout is a progress event; Security's eventual answer is the terminal event. Both are
/// main-actor confined, so ordering needs no lock and the store's real result is never discarded.
@MainActor
private final class CloudKeychainWriteLatch {
    private var timedOut = false
    private var terminal = false
    private var cancelled = false

    func deliverProgress(
        _ outcome: CloudKeychainWriteOutcome,
        to completion: @MainActor (CloudKeychainWriteOutcome) -> Void
    ) {
        guard !cancelled, !terminal, !timedOut, !outcome.isTerminal else { return }
        timedOut = true
        completion(outcome)
    }

    func deliverTerminal(
        _ outcome: CloudKeychainWriteOutcome,
        to completion: @MainActor (CloudKeychainWriteOutcome) -> Void
    ) {
        guard !cancelled, !terminal, outcome.isTerminal else { return }
        terminal = true
        completion(outcome)
    }

    func cancel() { cancelled = true }
}

/// The caller's grip on one in-flight mutation.
///
/// **`cancel()` stops the answer, not the Security call.** `SecItemUpdate`, `SecItemAdd` and
/// `SecItemDelete` are synchronous C functions with no cancellation, so a cancelled write may
/// still land. Nothing here pretends otherwise: what stops a *cancelled sign-in* from leaving a
/// credential behind is the generation guard inside ``CloudAccountClient``'s persistence
/// transaction, not this handle.
@MainActor
public final class CloudKeychainWriteHandle {
    private let latch: CloudKeychainWriteLatch

    fileprivate init(latch: CloudKeychainWriteLatch) { self.latch = latch }

    public func cancel() { latch.cancel() }
}

/// The UI adapter from synchronous Keychain *mutations* into app state.
///
/// It is the write-side twin of ``CloudKeychainReader`` and exists for the same reason: the
/// operation is captured privately and can only be started on its dedicated serial queue, so no
/// caller can run `SecItemAdd`/`SecItemDelete` while the main thread is trying to draw. Unlike
/// the reader it is also *bounded* — a locked Keychain answers when the person answers a system
/// dialog, which is not a length any screen may wait for.
public final class CloudKeychainWriter: @unchecked Sendable {
    /// Long enough that an ordinary unlocked write never trips it, short enough that a locked
    /// Keychain becomes a sentence on screen rather than a spinner nobody can leave.
    public static let defaultTimeoutSeconds = 10

    private let queue: DispatchQueue
    private let timeoutSeconds: Int
    private let operation: @Sendable () throws -> Void

    public init(
        label: String = "clawdline.cloud.keychain-write",
        timeoutSeconds: Int = CloudKeychainWriter.defaultTimeoutSeconds,
        operation: @escaping @Sendable () throws -> Void
    ) {
        queue = DispatchQueue(label: label, qos: .userInitiated)
        self.timeoutSeconds = max(1, timeoutSeconds)
        self.operation = operation
    }

    /// `onStart` runs before the operation is handed to its queue, so "started" is observable
    /// even when the Keychain never answers. The returned handle is discardable only because a
    /// caller that never cancels is ordinary; the timeout still bounds it.
    @discardableResult
    @MainActor
    public func write(
        onStart: () -> Void = {},
        completion: @escaping @MainActor (CloudKeychainWriteOutcome) -> Void
    ) -> CloudKeychainWriteHandle {
        onStart()
        let latch = CloudKeychainWriteLatch()
        queue.async { [operation] in
            let outcome: CloudKeychainWriteOutcome
            do {
                try operation()
                outcome = .succeeded
            } catch {
                outcome = .failed(error)
            }
            DispatchQueue.main.async { latch.deliverTerminal(outcome, to: completion) }
        }
        let seconds = timeoutSeconds
        DispatchQueue.main.asyncAfter(deadline: .now() + .seconds(seconds)) {
            latch.deliverProgress(.timedOut(seconds: seconds), to: completion)
        }
        return CloudKeychainWriteHandle(latch: latch)
    }
}

public struct CloudDeviceKeyPair: Equatable, Sendable {
    public static let privateKeyBytes = 32

    public let privateKeyRaw: Data

    public init() {
        privateKeyRaw = Curve25519.Signing.PrivateKey().rawRepresentation
    }

    public init(privateKeyRaw: Data) throws {
        guard privateKeyRaw.count == Self.privateKeyBytes,
              (try? Curve25519.Signing.PrivateKey(rawRepresentation: privateKeyRaw)) != nil
        else { throw CloudKeyError.invalidDevicePrivateKey }
        self.privateKeyRaw = privateKeyRaw
    }

    public var publicKeyRaw: Data {
        // The initializer was checked above (or produced by CryptoKit), so this cannot fail.
        let key = try! Curve25519.Signing.PrivateKey(rawRepresentation: privateKeyRaw)
        return key.publicKey.rawRepresentation
    }

    /// An 80-bit SHA-256 fingerprint, rendered as four groups of RFC 4648 base32.
    /// This is the short value people compare while pairing; it is not a key id.
    public var pairingFingerprint: String {
        let digest = Data(SHA256.hash(data: publicKeyRaw).prefix(10))
        return CloudBase32.group(CloudBase32.encode(digest), every: 4)
    }

    public func signature(for bytes: Data) throws -> Data {
        let key = try Curve25519.Signing.PrivateKey(rawRepresentation: privateKeyRaw)
        return try key.signature(for: bytes)
    }
}

public struct CloudMasterSecret: Equatable, Sendable {
    public static let byteCount = 32
    private static let recoveryPrefix = "CLAWD1"
    private static let checksumDomain = Data("clawdline-recovery-v1".utf8)

    public let rawRepresentation: Data

    public init() throws {
        var generator = SystemRandomNumberGenerator()
        rawRepresentation = Data((0..<Self.byteCount).map { _ in
            UInt8.random(in: .min ... .max, using: &generator)
        })
    }

    public init(rawRepresentation: Data) throws {
        guard rawRepresentation.count == Self.byteCount else {
            throw CloudKeyError.invalidMasterSecretLength(rawRepresentation.count)
        }
        self.rawRepresentation = rawRepresentation
    }

    public init(recoveryCode: String) throws {
        let compact = recoveryCode
            .uppercased()
            .filter { $0 != "-" && !$0.isWhitespace }
        guard compact.hasPrefix(Self.recoveryPrefix) else {
            throw CloudKeyError.invalidRecoveryCode
        }
        let encoded = String(compact.dropFirst(Self.recoveryPrefix.count))
        guard let recovered = CloudBase32.decode(encoded),
              recovered.count == Self.byteCount + 4
        else { throw CloudKeyError.invalidRecoveryCode }

        let secret = recovered.prefix(Self.byteCount)
        let suppliedChecksum = recovered.suffix(4)
        let wantedChecksum = Self.checksum(for: Data(secret))
        guard CloudMasterSecret.constantTimeEqual(Data(suppliedChecksum), wantedChecksum) else {
            throw CloudKeyError.recoveryChecksumMismatch
        }
        rawRepresentation = Data(secret)
    }

    public var recoveryCode: String {
        let body = rawRepresentation + Self.checksum(for: rawRepresentation)
        return Self.recoveryPrefix + "-" + CloudBase32.group(CloudBase32.encode(body), every: 5)
    }

    private static func checksum(for secret: Data) -> Data {
        Data(SHA256.hash(data: checksumDomain + secret).prefix(4))
    }

    private static func constantTimeEqual(_ lhs: Data, _ rhs: Data) -> Bool {
        guard lhs.count == rhs.count else { return false }
        return zip(lhs, rhs).reduce(UInt8(0)) { $0 | ($1.0 ^ $1.1) } == 0
    }
}

/// Serializes multi-call persistence transactions. Stores that address the same durable boundary
/// must expose the same coordinator, so independent clients cannot interleave read/compare/write.
public final class CloudKeyStoreCoordinator: Sendable {
    private let lock = NSRecursiveLock()

    public init() {}

    public func withCriticalRegion<T: Sendable>(_ body: @Sendable () throws -> T) rethrows -> T {
        lock.lock()
        defer { lock.unlock() }
        return try body()
    }
}

#if canImport(Security)
private final class CloudKeychainCoordinatorPool: Sendable {
    static let shared = CloudKeychainCoordinatorPool()

    private let coordinators = CloudLocked([String: CloudKeyStoreCoordinator]())

    func coordinator(for service: String) -> CloudKeyStoreCoordinator {
        coordinators.withLock { values in
            if let existing = values[service] { return existing }
            let created = CloudKeyStoreCoordinator()
            values[service] = created
            return created
        }
    }
}
#endif

/// The narrow persistence seam keeps envelope tests and recovery flows Keychain-free.
public protocol CloudKeyStoring: Sendable {
    var coordinator: CloudKeyStoreCoordinator { get }
    func data(for account: String) throws -> Data?
    func set(_ data: Data, for account: String) throws
    func remove(_ account: String) throws
}

#if canImport(Security)
final class CloudKeychainStore: CloudKeyStoring {
    public static let defaultService = "app.clawdline.cloud.keys"

    public let service: String
    public let coordinator: CloudKeyStoreCoordinator

    public init(service: String = CloudKeychainStore.defaultService) {
        self.service = service
        coordinator = CloudKeychainCoordinatorPool.shared.coordinator(for: service)
    }

    public func data(for account: String) throws -> Data? {
        guard !Thread.isMainThread else {
            NSLog("Clawdline rejected a Keychain read on the main thread")
            throw CloudKeyError.mainThreadReadForbidden
        }
        var query = Self.identity(service: service, account: account)
        query[kSecReturnData] = true
        query[kSecMatchLimit] = kSecMatchLimitOne
        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess, let bytes = item as? Data else {
            throw CloudKeyError.keychain(status)
        }
        return bytes
    }

    public func set(_ data: Data, for account: String) throws {
        try Self.refuseMainThread(operation: "set")
        let identity = Self.identity(service: service, account: account)
        let update: [CFString: Any] = [kSecValueData: data]
        let updateStatus = SecItemUpdate(identity as CFDictionary, update as CFDictionary)
        if updateStatus == errSecSuccess { return }
        guard updateStatus == errSecItemNotFound else {
            throw CloudKeyError.keychain(updateStatus)
        }

        let insert = Self.insertAttributes(service: service, account: account, data: data)
        let insertStatus = SecItemAdd(insert as CFDictionary, nil)
        guard insertStatus == errSecSuccess else { throw CloudKeyError.keychain(insertStatus) }
    }

    public func remove(_ account: String) throws {
        try Self.refuseMainThread(operation: "remove")
        let query = Self.identity(service: service, account: account)
        let status = SecItemDelete(query as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw CloudKeyError.keychain(status)
        }
    }

    /// The write-side twin of the guard in ``data(for:)``, and the reason it is a separate
    /// function: a mutation on a locked Keychain blocks on a system dialog exactly as a read
    /// does, so both doors need the same lock. The throw happens *before* any `SecItem…` call,
    /// so the refusal is reached whatever the Keychain's state is.
    private static func refuseMainThread(operation: String) throws {
        guard Thread.isMainThread else { return }
        NSLog("Clawdline rejected a Keychain %@ on the main thread", operation)
        throw CloudKeyError.mainThreadWriteForbidden
    }

    public static func identity(service: String, account: String) -> [CFString: Any] {
        [
            kSecClass: kSecClassGenericPassword,
            kSecAttrService: service,
            kSecAttrAccount: account,
            // A locked item is an ordinary typed failure, never an unbounded system dialog.
            // The same base dictionary is used by copy, update, add and delete, so every
            // production Security operation inherits this contract.
            kSecUseAuthenticationUI: kSecUseAuthenticationUIFail,
        ]
    }

    public static func insertAttributes(
        service: String, account: String, data: Data
    ) -> [CFString: Any] {
        var insert = identity(service: service, account: account)
        insert[kSecValueData] = data
        // Local builds have no application-identifier/keychain-access-group entitlement, so the
        // Data Protection Keychain refuses them with errSecMissingEntitlement. This store uses
        // the traditional macOS login keychain and its code-signing ACL deliberately. Do not add
        // kSecAttrAccessible here: Apple documents that attribute as applying on macOS only in
        // the Data Protection (or synchronizable) namespace.
        return insert
    }
}

/// Public Application adapter for Mac composition. Keeping the historical implementation type
/// internal preserves its existing same-module `SecretStore` extension in the flat compatibility
/// build; this wrapper is the only cross-module spelling.
public final class CloudProtectedKeychainStore: CloudKeyStoring, @unchecked Sendable {
    public static let defaultService = CloudKeychainStore.defaultService
    private let store: CloudKeychainStore

    public init(service: String = CloudProtectedKeychainStore.defaultService) {
        store = CloudKeychainStore(service: service)
    }

    public var coordinator: CloudKeyStoreCoordinator { store.coordinator }
    public func data(for account: String) throws -> Data? { try store.data(for: account) }
    public func set(_ data: Data, for account: String) throws { try store.set(data, for: account) }
    public func remove(_ account: String) throws { try store.remove(account) }
}
#endif

final class CloudInMemoryKeyStore: CloudKeyStoring, @unchecked Sendable {
    public let coordinator = CloudKeyStoreCoordinator()
    private let values: CloudLocked<[String: Data]>

    public init(values: [String: Data] = [:]) {
        self.values = CloudLocked(values)
    }

    public func data(for account: String) throws -> Data? {
        values.withLock { $0[account] }
    }

    public func set(_ data: Data, for account: String) throws {
        values.withLock { $0[account] = data }
    }

    public func remove(_ account: String) throws {
        _ = values.withLock { $0.removeValue(forKey: account) }
    }

    // W2-3 correction, F3: `values`' own lock already makes each of `data`/`set`/`remove`
    // individually atomic, but a caller composing "read, then set if absent" out of them can
    // still race a second caller doing the same thing — the lock only ever covers one call. These
    // two go through `coordinator` instead, the same closed region the Keychain-backed
    // `SecretStore` conformance in `Sources/MacHostAdapters.swift` uses, so this fake gives the
    // same atomicity guarantee it is standing in for.
    public func loadOrCreate(_ account: String, create: @Sendable () throws -> Data) throws -> Data {
        try coordinator.withCriticalRegion {
            if let existing = try data(for: account) { return existing }
            let created = try create()
            try set(created, for: account)
            return created
        }
    }

    public func rotate(_ account: String, replace: @Sendable (Data?) throws -> Data) throws -> Data {
        try coordinator.withCriticalRegion {
            let replacement = try replace(try data(for: account))
            try set(replacement, for: account)
            return replacement
        }
    }
}

/// Adapts the shared protected-store capability to the key/account clients without creating a
/// second persistence owner. Both macOS Keychain and Ubuntu's contained 0600-file store enter
/// through this same Application seam.
public final class CloudSecretStoreKeyAdapter: CloudKeyStoring, @unchecked Sendable {
    public let coordinator = CloudKeyStoreCoordinator()
    private let store: any SecretStore

    public init(store: any SecretStore) { self.store = store }

    public func data(for account: String) throws -> Data? { try store.data(for: account) }
    public func set(_ data: Data, for account: String) throws { try store.set(data, for: account) }
    public func remove(_ account: String) throws { try store.remove(account) }
}

public struct CloudKeys: Sendable {
    public static let deviceKeyAccount = "device-ed25519-v1"
    public static let masterSecretAccount = "account-master-secret-v1"

    private let store: any CloudKeyStoring

    public init(store: any CloudKeyStoring) {
        self.store = store
    }

#if canImport(Security)
    public init() { self.store = CloudKeychainStore() }
#endif

    public func loadOrCreateDeviceKeyPair() throws -> CloudDeviceKeyPair {
        try store.coordinator.withCriticalRegion {
            if let raw = try store.data(for: Self.deviceKeyAccount) {
                return try CloudDeviceKeyPair(privateKeyRaw: raw)
            }
            let pair = CloudDeviceKeyPair()
            try store.set(pair.privateKeyRaw, for: Self.deviceKeyAccount)
            return pair
        }
    }

    public func loadOrCreateMasterSecret() throws -> CloudMasterSecret {
        try store.coordinator.withCriticalRegion {
            if let raw = try store.data(for: Self.masterSecretAccount) {
                return try CloudMasterSecret(rawRepresentation: raw)
            }
            let secret = try CloudMasterSecret()
            try store.set(secret.rawRepresentation, for: Self.masterSecretAccount)
            return secret
        }
    }

    public func restoreMasterSecret(from recoveryCode: String) throws -> CloudMasterSecret {
        try store.coordinator.withCriticalRegion {
            let secret = try CloudMasterSecret(recoveryCode: recoveryCode)
            try store.set(secret.rawRepresentation, for: Self.masterSecretAccount)
            return secret
        }
    }
}

// MARK: - Cross-platform executor identity authority (W5-2)

/// Headless machine owner for the deployed console's invitation protocol.
///
/// It prepares the exact encrypted grant through `CloudExecutorIdentityAuthority`, writes it to
/// Cloud, and pins the viewer only after Cloud echoes the expected fingerprint. The invitation
/// secret is held only in this actor and the one URL explicitly returned to the operator.
public actor CloudCompatibilityPairingMachineHandover {
    private struct Session: Sendable {
        let invitation: CloudPairingInvitation
        let accountID: String
        let machineID: String
        let machineFingerprint: String
    }

    private struct PreparedDelivery: Sendable {
        let pairingID: String
        let claimNonce: String
        let viewerDeviceID: String
        let viewerFingerprint: String
        let blob: CloudOpaquePairingBlob
    }

    private let client: any CloudCompatibilityPairingClient
    private let authority: CloudExecutorIdentityAuthority
    private let nowMilliseconds: @Sendable () -> Int64
    private let randomBytes: @Sendable (Int) -> Data
    private var session: Session?
    private var preparedDelivery: PreparedDelivery?

    public init(
        client: any CloudCompatibilityPairingClient,
        authority: CloudExecutorIdentityAuthority,
        nowMilliseconds: @escaping @Sendable () -> Int64,
        randomBytes: @escaping @Sendable (Int) -> Data = { count in
            var generator = SystemRandomNumberGenerator()
            return Data((0..<count).map {
                _ in UInt8.random(in: .min ... .max, using: &generator)
            })
        }
    ) {
        self.client = client
        self.authority = authority
        self.nowMilliseconds = nowMilliseconds
        self.randomBytes = randomBytes
    }

    public func begin() async throws -> CloudCompatibilityPairingStart {
        guard session == nil else { throw CloudExecutorIdentityError.pairingClaimed }
        let snapshot = try authority.snapshot()
        let secret = randomBytes(CloudPairingInvitation.secretBytes)
        guard secret.count == CloudPairingInvitation.secretBytes else {
            throw CloudExecutorIdentityError.protectedStateCorrupt
        }
        let started = try await client.startPairingInvitation(
            secretHash: Data(SHA256.hash(data: secret)))
        let expiresAt = Int64(started.expiresAt.timeIntervalSince1970 * 1_000)
        let invitation = try CloudPairingInvitation(
            invitationID: started.invitationID, secret: secret,
            expiresAtMilliseconds: expiresAt)
        guard expiresAt > nowMilliseconds(),
              expiresAt - nowMilliseconds() <= 600_000,
              let url = invitation.qrURL() else {
            throw CloudHandoverError.malformedInvitation
        }
        session = Session(
            invitation: invitation, accountID: snapshot.accountID,
            machineID: snapshot.machineID, machineFingerprint: snapshot.machineFingerprint)
        return CloudCompatibilityPairingStart(
            verificationURL: url, accountID: snapshot.accountID,
            machineID: snapshot.machineID, machineFingerprint: snapshot.machineFingerprint,
            expiresAtMilliseconds: expiresAt)
    }

    public func advance() async throws -> CloudCompatibilityPairingProgress {
        guard let session else { throw CloudExecutorIdentityError.pairingNotPrepared }
        guard session.invitation.expiresAtMilliseconds >= nowMilliseconds() else {
            self.session = nil
            preparedDelivery = nil
            throw CloudHandoverError.invitationExpired
        }
        if let preparedDelivery {
            return try await deliver(preparedDelivery, for: session)
        }
        switch try await client.pollPairingInvitation(
            invitationID: session.invitation.invitationID) {
        case .pending:
            return .waiting
        case .ready(let accountID, let viewerDeviceID, let machineID, let encryptedOffer):
            guard accountID == session.accountID, machineID == session.machineID else {
                throw CloudExecutorIdentityError.identityMismatch
            }
            let offerFragment = try session.invitation.openEncryptedOffer(
                encryptedOffer, nowMilliseconds: nowMilliseconds())
            let decoded = try CloudCompatibilityPairing.validatedOfferBytes(
                offerFragment, nowMilliseconds: nowMilliseconds())
            // The API route is authenticated, but its clear viewer id is still untrusted input.
            // Reject it before `prepareHandover` durably reserves a claimant: otherwise a
            // mismatched response can poison the protected pending slot until expiry.
            guard decoded.offer.accountID == accountID,
                  decoded.offer.viewerDeviceID == viewerDeviceID else {
                throw CloudExecutorIdentityError.identityMismatch
            }
            let prepared = try authority.prepareHandover(
                offerBytes: decoded.bytes, nowMilliseconds: nowMilliseconds())
            guard prepared.viewerDeviceID == viewerDeviceID,
                  prepared.viewerFingerprint == decoded.offer.viewerFingerprint else {
                throw CloudExecutorIdentityError.identityMismatch
            }
            let resumable = PreparedDelivery(
                pairingID: prepared.pairingID, claimNonce: decoded.offer.claimNonce,
                viewerDeviceID: prepared.viewerDeviceID,
                viewerFingerprint: prepared.viewerFingerprint,
                blob: try CloudOpaquePairingBlob(
                    base64: prepared.wrapperBytes.base64EncodedString()))
            preparedDelivery = resumable
            return try await deliver(resumable, for: session)
        }
    }

    private func deliver(
        _ prepared: PreparedDelivery, for session: Session
    ) async throws -> CloudCompatibilityPairingProgress {
        let delivery: CloudPairingDelivery
        do {
            delivery = try await client.completePairing(
                pairingID: prepared.pairingID, blob: prepared.blob)
        } catch {
            // The POST may have reached Cloud even when its response did not reach this process.
            // Keep the exact persisted wrapper and retry that identity; never poll a new offer or
            // create a second invitation while delivery is ambiguous.
            return .retrying(.deliveryUncertain)
        }
        guard delivery.fingerprint == prepared.viewerFingerprint else {
            throw CloudExecutorIdentityError.identityMismatch
        }
        let snapshot: CloudExecutorIdentitySnapshot
        do {
            snapshot = try authority.commitPreparedHandover(
                pairingID: prepared.pairingID, claimNonce: prepared.claimNonce,
                deliveredFingerprint: delivery.fingerprint,
                nowMilliseconds: nowMilliseconds())
        } catch CloudExecutorIdentityError.protectedStateUnavailable {
            // Cloud has the wrapper but the local atomic rotation did not commit. Retaining the
            // same prepared bytes lets the next bounded attempt reconcile both sides.
            return .retrying(.localCommitPending)
        }
        self.session = nil
        preparedDelivery = nil
        return .complete(CloudCompatibilityPairingReceipt(
            accountID: snapshot.accountID, machineID: snapshot.machineID,
            viewerDeviceID: prepared.viewerDeviceID,
            viewerFingerprint: prepared.viewerFingerprint,
            machineFingerprint: session.machineFingerprint))
    }
}

public enum CloudExecutorIdentityError: Error, LocalizedError, Equatable {
    case protectedStateMissing
    case protectedStateUnavailable
    case protectedStateCorrupt
    case protectedStateFutureVersion(Int64)
    case identityMismatch
    case generationOverflow
    case capacityExceeded
    case pairingClaimed
    case pairingNotPrepared
    case pairingExpired
    case pairingFingerprintMismatch
    case revokedIdentity
    case invalidReconnectProof
    case durableResumeUnavailable

    public var errorDescription: String? {
        switch self {
        case .protectedStateMissing: return "Protected executor identity is missing."
        case .protectedStateUnavailable: return "Protected executor identity is unavailable."
        case .protectedStateCorrupt: return "Protected executor identity is corrupt or noncanonical."
        case .protectedStateFutureVersion(let version):
            return "Protected executor identity version \(version) is newer than this executable."
        case .identityMismatch: return "Executor identity does not match the protected owner."
        case .generationOverflow: return "Executor identity generation is exhausted."
        case .capacityExceeded: return "Executor identity reached its protected-state capacity."
        case .pairingClaimed: return "That pairing already has another claimant or handover."
        case .pairingNotPrepared: return "That exact pairing handover is not prepared."
        case .pairingExpired: return "That pairing handover has expired."
        case .pairingFingerprintMismatch: return "The pairing fingerprint echo does not match."
        case .revokedIdentity: return "A revoked executor or viewer identity cannot be restored."
        case .invalidReconnectProof: return "Reconnect did not prove the current executor identity."
        case .durableResumeUnavailable:
            return "Reconnect requires the durable command ledger and outbound spool."
        }
    }
}

public struct CloudExecutorPairedDevice: Equatable, Sendable {
    public let deviceID: String
    public let signingKey: Data
    public let fingerprint: String
    public let pairedAtMilliseconds: Int64
    public let identityGeneration: UInt64
    public let capabilities: [String]

    public init(deviceID: String, signingKey: Data, fingerprint: String,
                pairedAtMilliseconds: Int64, identityGeneration: UInt64,
                capabilities: [String]) {
        self.deviceID = deviceID
        self.signingKey = signingKey
        self.fingerprint = fingerprint
        self.pairedAtMilliseconds = pairedAtMilliseconds
        self.identityGeneration = identityGeneration
        self.capabilities = capabilities
    }
}

public struct CloudExecutorIdentitySnapshot: Equatable, Sendable {
    public let accountID: String
    public let machineID: String
    public let deviceID: String
    public let machineSigningKey: Data
    public let machineFingerprint: String
    public let keyID: String
    public let identityGeneration: UInt64
    public let keyEpoch: UInt64
    public let revocationEpoch: UInt64
    public let pairedDevices: [CloudExecutorPairedDevice]
    public let revokedDeviceIDs: [String]
    public let hasPreparedHandover: Bool
}

public enum CloudExecutorIdentityReadiness: Equatable, Sendable {
    case ready(CloudExecutorIdentitySnapshot)
    case blocked(CloudExecutorIdentityError)
}

public struct CloudExecutorPreparedHandover: Equatable, Sendable,
                                                CustomStringConvertible,
                                                CustomDebugStringConvertible,
                                                CustomReflectable {
    public let pairingID: String
    public let viewerDeviceID: String
    public let viewerFingerprint: String
    public let machineFingerprint: String
    public let expiresAtMilliseconds: Int64
    public let wrapperBytes: Data
    public let alreadyCommitted: Bool

    public var description: String {
        "CloudExecutorPreparedHandover(pairingID: \(pairingID), "
            + "wrapperBytes: <redacted \(wrapperBytes.count) bytes>, alreadyCommitted: \(alreadyCommitted))"
    }
    public var debugDescription: String { description }
    public var customMirror: Mirror {
        Mirror(self, children: [
            (label: Optional("pairingID"), value: pairingID as Any),
            (label: Optional("viewerDeviceID"), value: viewerDeviceID as Any),
            (label: Optional("viewerFingerprint"), value: viewerFingerprint as Any),
            (label: Optional("machineFingerprint"), value: machineFingerprint as Any),
            (label: Optional("expiresAtMilliseconds"), value: expiresAtMilliseconds as Any),
            (label: Optional("wrapperBytes"),
             value: "<redacted \(wrapperBytes.count) bytes>" as Any),
            (label: Optional("alreadyCommitted"), value: alreadyCommitted as Any),
        ], displayStyle: .struct)
    }
}

public struct CloudExecutorRotationTransition: Equatable, Sendable {
    public let previousGeneration: UInt64
    public let identityGeneration: UInt64
    public let previousKeyEpoch: UInt64
    public let keyEpoch: UInt64
    public let previousKeyID: String
    public let keyID: String
}

public struct CloudExecutorTransportMaterial: Sendable,
                                              CustomStringConvertible,
                                              CustomDebugStringConvertible,
                                              CustomReflectable {
    public let binding: CloudExecutorTransportBinding
    public let deviceKey: CloudDeviceKeyPair
    public let masterSecret: CloudMasterSecret
    public let keyID: String
    public let pairedDevicePublicKeys: [String: Data]

    public var description: String {
        "CloudExecutorTransportMaterial(binding: \(binding), deviceKey: <redacted>, "
            + "masterSecret: <redacted>, pairedDevices: \(pairedDevicePublicKeys.count))"
    }
    public var debugDescription: String { description }
    public var customMirror: Mirror {
        Mirror(self, children: [
            (label: Optional("binding"), value: binding as Any),
            (label: Optional("deviceKey"), value: "<redacted>" as Any),
            (label: Optional("masterSecret"), value: "<redacted>" as Any),
            (label: Optional("keyID"), value: keyID as Any),
            (label: Optional("pairedDeviceCount"), value: pairedDevicePublicKeys.count as Any),
        ], displayStyle: .struct)
    }
}

/// The sole protected owner of executor identity, current key epoch, paired viewers and pairing
/// handover state. Mac injects its Keychain-backed `SecretStore`; Ubuntu injects
/// `LinuxProtectedFileSecretStore`. Missing or unreadable bytes are never replaced by memory.
public final class CloudExecutorIdentityAuthority: @unchecked Sendable {
    public static let protectedAccount = "executor-identity-v1"
    public static let masterKeyID = "master-v1"
    public static let pairingLifetimeMilliseconds: Int64 = 600_000
    /// Matches the production Ubuntu protected-file reader. Reject before a write can create
    /// state that the same daemon would be unable to restore after restart.
    public static let maximumProtectedStateBytes = 4_096
    public static let maximumHandoverWrapperBytes = 2_048
    public static let defaultCapabilities = [
        "read_sessions", "read_transcript", "send_prompt", "start_session",
    ]

    private let store: any SecretStore
    private let lock = NSRecursiveLock()

    public init(store: any SecretStore) { self.store = store }

    /// Explicit enrollment/migration. Ordinary readiness never creates keys or an empty record.
    /// Repeating the exact migration is idempotent; a different owner or key is refused.
    @discardableResult
    public func provision(accountID: String, machineID: String,
                          deviceKey: CloudDeviceKeyPair, masterSecret: CloudMasterSecret,
                          keyID: String = CloudExecutorIdentityAuthority.masterKeyID,
                          importedPairedDevices: [CloudExecutorPairedDevice] = [])
        throws -> CloudExecutorIdentitySnapshot {
        lock.lock(); defer { lock.unlock() }
        let candidate = try Record.initial(
            accountID: accountID, machineID: machineID, deviceKey: deviceKey,
            masterSecret: masterSecret, keyID: keyID,
            importedPairedDevices: importedPairedDevices)
        let bytes: Data
        do {
            bytes = try store.loadOrCreate(Self.protectedAccount) { candidate.encoded() }
        } catch let error as CloudExecutorIdentityError {
            throw error
        } catch {
            throw CloudExecutorIdentityError.protectedStateUnavailable
        }
        let record = try Record.decode(bytes)
        guard record.sameOwnerAndSecrets(as: candidate) else {
            throw CloudExecutorIdentityError.identityMismatch
        }
        return record.snapshot
    }

    public func snapshot() throws -> CloudExecutorIdentitySnapshot {
        lock.lock(); defer { lock.unlock() }
        return try load().snapshot
    }

    public func readiness(expectedAccountID: String? = nil,
                          expectedMachineID: String? = nil) -> CloudExecutorIdentityReadiness {
        do {
            let value = try snapshot()
            guard expectedAccountID.map({ $0 == value.accountID }) ?? true,
                  expectedMachineID.map({ $0 == value.machineID }) ?? true else {
                return .blocked(.identityMismatch)
            }
            return .ready(value)
        } catch let error as CloudExecutorIdentityError {
            return .blocked(error)
        } catch {
            return .blocked(.protectedStateUnavailable)
        }
    }

    public func transportMaterial() throws -> CloudExecutorTransportMaterial {
        lock.lock(); defer { lock.unlock() }
        let record = try load()
        let deviceKey = try CloudDeviceKeyPair(privateKeyRaw: record.machinePrivateKey)
        let master = try CloudMasterSecret(rawRepresentation: record.masterSecret)
        let paired = Dictionary(uniqueKeysWithValues: record.paired.map { ($0.deviceID, $0.signingKey) })
        return CloudExecutorTransportMaterial(
            binding: record.snapshot.transportBinding,
            deviceKey: deviceKey,
            masterSecret: master,
            keyID: record.keyID,
            pairedDevicePublicKeys: paired)
    }

    /// Prepare and durably retain the exact v1 grant before the caller writes it to Cloud.
    /// An exact restart retry returns the identical wrapper; a second claimant cannot replace it.
    public func prepareHandover(
        offerBytes: Data,
        nowMilliseconds: Int64,
        machineEphemeralPrivateKey: Data? = nil,
        randomBytes: @Sendable (Int) -> Data = { count in
            var generator = SystemRandomNumberGenerator()
            return Data((0..<count).map {
                _ in UInt8.random(in: .min ... .max, using: &generator)
            })
        }
    ) throws -> CloudExecutorPreparedHandover {
        lock.lock(); defer { lock.unlock() }
        let offer = try PairingOffer.decode(offerBytes, nowMilliseconds: nowMilliseconds)
        return try mutate { record in
            guard offer.accountID == record.accountID else {
                throw CloudExecutorIdentityError.identityMismatch
            }
            let offerDigest = Self.sha256(offerBytes)
            guard !record.revoked.contains(where: { $0.deviceID == offer.viewerDeviceID }) else {
                throw CloudExecutorIdentityError.revokedIdentity
            }
            if let complete = record.completed, complete.pairingID == offer.pairingID {
                guard complete.claimNonceDigest == Self.sha256(offer.claimNonce),
                      complete.offerDigest == offerDigest else {
                    throw CloudExecutorIdentityError.pairingClaimed
                }
                return (false, complete.prepared(alreadyCommitted: true))
            }
            if let pending = record.pending {
                if pending.expiresAt < nowMilliseconds {
                    record.pending = nil
                } else {
                    guard pending.pairingID == offer.pairingID,
                          pending.claimNonceDigest == Self.sha256(offer.claimNonce),
                          pending.offerDigest == offerDigest else {
                        throw CloudExecutorIdentityError.pairingClaimed
                    }
                    return (false, pending.prepared(alreadyCommitted: false))
                }
            }
            // `completed` is the idempotency receipt for the most recently delivered grant.
            // Once a different pairing is accepted for preparation, retaining that receipt beside
            // the new pending wrapper can exceed the protected record's bounded size. The prior
            // row is safe to retire here: it was already delivered, while the exact-same pairing
            // retry returned above before reaching this point.
            record.completed = nil
            let ephemeralPrivate = machineEphemeralPrivateKey ?? randomBytes(32)
            let nonce = randomBytes(12)
            guard ephemeralPrivate.count == 32, nonce.count == 12 else {
                throw CloudExecutorIdentityError.protectedStateCorrupt
            }
            let ephemeralPublic = try CloudPairing.x25519PublicKey(
                privateKeyRaw: ephemeralPrivate)
            let shared = try CloudPairing.x25519SharedSecret(
                privateKeyRaw: ephemeralPrivate, peerPublicKeyRaw: offer.viewerEphemeralKey)
            let derived = try CloudPairing.derive(
                sharedSecretRaw: shared, pairingNonce: offer.pairingNonce,
                pairingID: offer.pairingID, claimNonce: offer.claimNonce, phase: .grant)
            let handover = CloudCanonicalJSON.canonicalData(.object([
                "v": .int(1), "type": .string("pairing_handover"),
                "account_id": .string(record.accountID),
                "machine_id": .string(record.machineID),
                "machine_signing_key": .base64(record.machinePublicKey),
                "machine_fingerprint": .string(record.machineFingerprint),
                "key_id": .string(record.keyID), "master_secret": .base64(record.masterSecret),
            ]))
            let wrapper = try CloudPairing.seal(
                plaintext: handover, phase: .grant, pairingID: offer.pairingID,
                senderDeviceID: record.deviceID,
                ephemeralKey: ephemeralPublic.base64EncodedString(),
                phaseKey: derived.phaseKey, nonce: nonce)
            let wrapperBytes = try CloudPairing.encodeWrapper(wrapper)
            let nextGeneration = try Self.increment(record.generation)
            let pending = Pending(
                pairingID: offer.pairingID,
                claimNonceDigest: Self.sha256(offer.claimNonce),
                offerDigest: offerDigest,
                viewerDeviceID: offer.viewerDeviceID,
                viewerSigningKey: offer.viewerSigningKey,
                viewerFingerprint: offer.viewerFingerprint,
                machineFingerprint: record.machineFingerprint,
                expiresAt: offer.expiresAt,
                wrapperBytes: wrapperBytes,
                preparedGeneration: nextGeneration)
            record.pending = pending
            record.generation = nextGeneration
            return (true, pending.prepared(alreadyCommitted: false))
        }
    }

    /// Pin only the claimant whose exact grant was prepared and whose fingerprint Cloud echoed.
    /// The exact repeated commit is idempotent after restart and does not advance an epoch twice.
    @discardableResult
    public func commitPreparedHandover(pairingID: String, claimNonce: String,
                                        deliveredFingerprint: String,
                                        nowMilliseconds: Int64)
        throws -> CloudExecutorIdentitySnapshot {
        lock.lock(); defer { lock.unlock() }
        return try mutate { record in
            let claim = try CloudPairing.decodeCanonicalBase64(
                claimNonce, field: "claim_nonce", expectedLength: 32)
            let digest = Self.sha256(claim)
            if let complete = record.completed, complete.pairingID == pairingID {
                guard complete.claimNonceDigest == digest,
                      complete.viewerFingerprint == deliveredFingerprint else {
                    throw CloudExecutorIdentityError.pairingClaimed
                }
                return (false, record.snapshot)
            }
            guard let pending = record.pending, pending.pairingID == pairingID,
                  pending.claimNonceDigest == digest else {
                throw CloudExecutorIdentityError.pairingNotPrepared
            }
            guard pending.expiresAt >= nowMilliseconds else {
                throw CloudExecutorIdentityError.pairingExpired
            }
            guard pending.viewerFingerprint == deliveredFingerprint else {
                throw CloudExecutorIdentityError.pairingFingerprintMismatch
            }
            guard !record.revoked.contains(where: { $0.deviceID == pending.viewerDeviceID }) else {
                throw CloudExecutorIdentityError.revokedIdentity
            }
            if let existing = record.paired.first(where: { $0.deviceID == pending.viewerDeviceID }),
               existing.signingKey != pending.viewerSigningKey {
                throw CloudExecutorIdentityError.identityMismatch
            }
            guard record.paired.contains(where: { $0.deviceID == pending.viewerDeviceID })
                    || record.paired.count < 8 else {
                throw CloudExecutorIdentityError.capacityExceeded
            }
            let nextGeneration = try Self.increment(record.generation)
            record.paired.removeAll { $0.deviceID == pending.viewerDeviceID }
            record.paired.append(Paired(
                deviceID: pending.viewerDeviceID, signingKey: pending.viewerSigningKey,
                fingerprint: pending.viewerFingerprint, pairedAt: nowMilliseconds,
                generation: nextGeneration, capabilities: Self.defaultCapabilities))
            record.paired.sort { $0.deviceID < $1.deviceID }
            record.completed = pending
            record.pending = nil
            record.generation = nextGeneration
            return (true, record.snapshot)
        }
    }

    @discardableResult
    public func revokeDevice(_ deviceID: String, expectedGeneration: UInt64)
        throws -> CloudExecutorIdentitySnapshot {
        lock.lock(); defer { lock.unlock() }
        return try mutate { record in
            guard record.generation == expectedGeneration else {
                throw CloudExecutorIdentityError.identityMismatch
            }
            guard !record.revoked.contains(where: { $0.deviceID == deviceID }) else {
                return (false, record.snapshot)
            }
            guard record.paired.contains(where: { $0.deviceID == deviceID }) else {
                throw CloudExecutorIdentityError.identityMismatch
            }
            guard record.revoked.count < 8 else {
                throw CloudExecutorIdentityError.capacityExceeded
            }
            let nextGeneration = try Self.increment(record.generation)
            let nextRevocation = try Self.increment(record.revocationEpoch)
            record.paired.removeAll { $0.deviceID == deviceID }
            record.revoked.append(Revoked(deviceID: deviceID, revocationEpoch: nextRevocation))
            record.revoked.sort { $0.deviceID < $1.deviceID }
            if record.completed?.viewerDeviceID == deviceID { record.completed = nil }
            record.revocationEpoch = nextRevocation
            record.generation = nextGeneration
            return (true, record.snapshot)
        }
    }

    public func rotateKeys(deviceKey: CloudDeviceKeyPair, masterSecret: CloudMasterSecret,
                           keyID: String, expectedGeneration: UInt64)
        throws -> CloudExecutorRotationTransition {
        lock.lock(); defer { lock.unlock() }
        return try mutate { record in
            guard record.generation == expectedGeneration, record.keyID != keyID else {
                throw CloudExecutorIdentityError.identityMismatch
            }
            guard record.retiredKeyIDs.count < 8 else {
                throw CloudExecutorIdentityError.generationOverflow
            }
            let nextGeneration = try Self.increment(record.generation)
            let nextKeyEpoch = try Self.increment(record.keyEpoch)
            let transition = CloudExecutorRotationTransition(
                previousGeneration: record.generation, identityGeneration: nextGeneration,
                previousKeyEpoch: record.keyEpoch, keyEpoch: nextKeyEpoch,
                previousKeyID: record.keyID, keyID: keyID)
            record.retiredKeyIDs.append(record.keyID)
            record.retiredKeyIDs = Array(Set(record.retiredKeyIDs)).sorted()
            record.machinePrivateKey = deviceKey.privateKeyRaw
            record.machinePublicKey = deviceKey.publicKeyRaw
            record.machineFingerprint = deviceKey.pairingFingerprint
            record.masterSecret = masterSecret.rawRepresentation
            record.keyID = keyID
            record.keyEpoch = nextKeyEpoch
            record.generation = nextGeneration
            record.pending = nil
            record.completed = nil
            return (true, transition)
        }
    }

    public func verifyReconnect(_ proof: CloudExecutorReconnectProof)
        throws -> CloudExecutorReconnectDisposition {
        lock.lock(); defer { lock.unlock() }
        let record = try load()
        guard proof.accountID == record.accountID, proof.machineID == record.machineID,
              proof.deviceID == record.deviceID,
              record.generation == proof.identityGeneration,
              record.keyEpoch == proof.keyEpoch,
              record.revocationEpoch == proof.revocationEpoch else {
            throw CloudExecutorIdentityError.invalidReconnectProof
        }
        guard proof.durableLedgerOpened, proof.durableSpoolOpened else {
            throw CloudExecutorIdentityError.durableResumeUnavailable
        }
        return .resumeFromDurableLedgerAndSpool
    }

    private func load() throws -> Record {
        let bytes: Data?
        do { bytes = try store.data(for: Self.protectedAccount) }
        catch { throw CloudExecutorIdentityError.protectedStateUnavailable }
        guard let bytes else { throw CloudExecutorIdentityError.protectedStateMissing }
        return try Record.decode(bytes)
    }

    private func mutate<Result: Sendable>(
        _ update: @Sendable (inout Record) throws -> (changed: Bool, result: Result)
    ) throws -> Result {
        let resulting = CloudLocked<Result?>(nil)
        do {
            _ = try store.rotate(Self.protectedAccount) { current in
                guard let current else { throw CloudExecutorIdentityError.protectedStateMissing }
                var record = try Record.decode(current)
                let decision = try update(&record)
                resulting.withLock { $0 = decision.result }
                if decision.changed {
                    try record.validate()
                    return record.encoded()
                }
                return current
            }
        } catch let error as CloudExecutorIdentityError {
            throw error
        } catch {
            throw CloudExecutorIdentityError.protectedStateUnavailable
        }
        guard let answer = resulting.withLock({ $0 }) else {
            throw CloudExecutorIdentityError.protectedStateUnavailable
        }
        return answer
    }

    private static func increment(_ value: UInt64) throws -> UInt64 {
        guard value < UInt64(CloudCanonicalJSON.maximumSafeInteger) else {
            throw CloudExecutorIdentityError.generationOverflow
        }
        return value + 1
    }

    private static func safeInteger(_ value: UInt64) throws -> Int64 {
        guard value <= UInt64(CloudCanonicalJSON.maximumSafeInteger) else {
            throw CloudExecutorIdentityError.generationOverflow
        }
        return Int64(value)
    }

    private static func sha256(_ bytes: Data) -> Data { Data(SHA256.hash(data: bytes)) }

    private struct PairingOffer {
        let pairingID: String
        let claimNonce: Data
        let pairingNonce: Data
        let accountID: String
        let viewerDeviceID: String
        let viewerSigningKey: Data
        let viewerEphemeralKey: Data
        let viewerFingerprint: String
        let expiresAt: Int64

        static func decode(_ bytes: Data, nowMilliseconds: Int64) throws -> PairingOffer {
            guard bytes.count <= CloudExecutorIdentityAuthority.maximumProtectedStateBytes else {
                throw CloudExecutorIdentityError.protectedStateCorrupt
            }
            let value: CloudJSONValue
            do { value = try CloudCanonicalJSON.parseStrict(bytes) }
            catch { throw CloudExecutorIdentityError.protectedStateCorrupt }
            guard case .object(let object) = value,
                  Set(object.keys) == Set([
                    "v", "type", "pairing_id", "claim_nonce", "pairing_nonce", "account_id",
                    "viewer_device_id", "viewer_signing_key", "viewer_ephemeral_key",
                    "viewer_fingerprint", "expires_at",
                  ]), case .int(1)? = object["v"],
                  case .string("pairing_offer")? = object["type"],
                  case .int(let expiresAt)? = object["expires_at"] else {
                throw CloudExecutorIdentityError.protectedStateCorrupt
            }
            func text(_ key: String) throws -> String {
                guard case .string(let value)? = object[key], validID(value) else {
                    throw CloudExecutorIdentityError.protectedStateCorrupt
                }
                return value
            }
            let claim = try CloudPairing.decodeCanonicalBase64(
                text("claim_nonce"), field: "claim_nonce", expectedLength: 32)
            let pairing = try CloudPairing.decodeCanonicalBase64(
                text("pairing_nonce"), field: "pairing_nonce", expectedLength: 32)
            guard claim != pairing, nowMilliseconds >= 0, expiresAt >= nowMilliseconds,
                  expiresAt - nowMilliseconds <= pairingLifetimeMilliseconds else {
                throw CloudExecutorIdentityError.pairingExpired
            }
            let signing = try CloudPairing.decodeCanonicalBase64(
                text("viewer_signing_key"), field: "viewer_signing_key", expectedLength: 32)
            let ephemeral = try CloudPairing.decodeCanonicalBase64(
                text("viewer_ephemeral_key"), field: "viewer_ephemeral_key", expectedLength: 32)
            let fingerprint = try text("viewer_fingerprint")
            guard fingerprint == (try CloudPairing.ed25519Fingerprint(publicKeyRaw: signing)) else {
                throw CloudExecutorIdentityError.pairingFingerprintMismatch
            }
            return PairingOffer(
                pairingID: try text("pairing_id"), claimNonce: claim, pairingNonce: pairing,
                accountID: try text("account_id"), viewerDeviceID: try text("viewer_device_id"),
                viewerSigningKey: signing, viewerEphemeralKey: ephemeral,
                viewerFingerprint: fingerprint, expiresAt: expiresAt)
        }

        fileprivate static func validID(_ value: String) -> Bool {
            (1...128).contains(value.utf8.count)
                && value.unicodeScalars.allSatisfy { $0.value >= 0x20 && $0.value <= 0x7e }
        }
    }

    private struct Paired: Equatable {
        var deviceID: String
        var signingKey: Data
        var fingerprint: String
        var pairedAt: Int64
        var generation: UInt64
        var capabilities: [String]
    }
    private struct Revoked: Equatable { var deviceID: String; var revocationEpoch: UInt64 }
    private struct Pending: Equatable {
        var pairingID: String
        var claimNonceDigest: Data
        var offerDigest: Data
        var viewerDeviceID: String
        var viewerSigningKey: Data
        var viewerFingerprint: String
        var machineFingerprint: String
        var expiresAt: Int64
        var wrapperBytes: Data
        var preparedGeneration: UInt64

        func prepared(alreadyCommitted: Bool) -> CloudExecutorPreparedHandover {
            CloudExecutorPreparedHandover(
                pairingID: pairingID,
                viewerDeviceID: viewerDeviceID, viewerFingerprint: viewerFingerprint,
                machineFingerprint: machineFingerprint, expiresAtMilliseconds: expiresAt,
                wrapperBytes: wrapperBytes, alreadyCommitted: alreadyCommitted)
        }
    }

    private struct Record {
        var accountID: String
        var machineID: String
        var deviceID: String
        var machinePrivateKey: Data
        var machinePublicKey: Data
        var machineFingerprint: String
        var masterSecret: Data
        var keyID: String
        var generation: UInt64
        var keyEpoch: UInt64
        var revocationEpoch: UInt64
        var paired: [Paired]
        var revoked: [Revoked]
        var retiredKeyIDs: [String]
        var pending: Pending?
        var completed: Pending?

        static func initial(accountID: String, machineID: String,
                            deviceKey: CloudDeviceKeyPair, masterSecret: CloudMasterSecret,
                            keyID: String,
                            importedPairedDevices: [CloudExecutorPairedDevice]) throws -> Record {
            guard PairingOffer.validID(accountID), PairingOffer.validID(machineID),
                  PairingOffer.validID(keyID), importedPairedDevices.count <= 8,
                  Set(importedPairedDevices.map(\.deviceID)).count
                    == importedPairedDevices.count else {
                throw CloudExecutorIdentityError.identityMismatch
            }
            let paired = importedPairedDevices.map { device in
                Paired(
                    deviceID: device.deviceID, signingKey: device.signingKey,
                    fingerprint: device.fingerprint,
                    pairedAt: device.pairedAtMilliseconds, generation: 1,
                    capabilities: defaultCapabilities)
            }.sorted { $0.deviceID < $1.deviceID }
            let record = Record(
                accountID: accountID, machineID: machineID, deviceID: machineID,
                machinePrivateKey: deviceKey.privateKeyRaw,
                machinePublicKey: deviceKey.publicKeyRaw,
                machineFingerprint: deviceKey.pairingFingerprint,
                masterSecret: masterSecret.rawRepresentation, keyID: keyID,
                generation: 1, keyEpoch: 1, revocationEpoch: 0,
                paired: paired, revoked: [], retiredKeyIDs: [], pending: nil, completed: nil)
            try record.validate()
            return record
        }

        var snapshot: CloudExecutorIdentitySnapshot {
            CloudExecutorIdentitySnapshot(
                accountID: accountID, machineID: machineID, deviceID: deviceID,
                machineSigningKey: machinePublicKey, machineFingerprint: machineFingerprint,
                keyID: keyID, identityGeneration: generation, keyEpoch: keyEpoch,
                revocationEpoch: revocationEpoch,
                pairedDevices: paired.map {
                    CloudExecutorPairedDevice(
                        deviceID: $0.deviceID, signingKey: $0.signingKey,
                        fingerprint: $0.fingerprint, pairedAtMilliseconds: $0.pairedAt,
                        identityGeneration: $0.generation, capabilities: $0.capabilities)
                },
                revokedDeviceIDs: revoked.map(\.deviceID),
                hasPreparedHandover: pending != nil)
        }

        func sameOwnerAndSecrets(as other: Record) -> Bool {
            accountID == other.accountID && machineID == other.machineID
                && deviceID == other.deviceID && machinePrivateKey == other.machinePrivateKey
                && machinePublicKey == other.machinePublicKey && masterSecret == other.masterSecret
                && keyID == other.keyID
        }

        func encoded() -> Data {
            func pendingValue(_ row: Pending?) -> CloudJSONValue {
                guard let row else { return .null }
                return .object([
                    "pairing_id": .string(row.pairingID),
                    "claim_nonce_sha256": .base64(row.claimNonceDigest),
                    "offer_sha256": .base64(row.offerDigest),
                    "viewer_device_id": .string(row.viewerDeviceID),
                    "viewer_signing_key": .base64(row.viewerSigningKey),
                    "viewer_fingerprint": .string(row.viewerFingerprint),
                    "machine_fingerprint": .string(row.machineFingerprint),
                    "expires_at": .int(row.expiresAt),
                    "wrapper": .base64(row.wrapperBytes),
                    "prepared_generation": .int(Int64(row.preparedGeneration)),
                ])
            }
            return CloudCanonicalJSON.canonicalData(.object([
                "v": .int(1), "type": .string("executor_identity"),
                "account_id": .string(accountID), "machine_id": .string(machineID),
                "device_id": .string(deviceID),
                "machine_private_key": .base64(machinePrivateKey),
                "machine_public_key": .base64(machinePublicKey),
                "machine_fingerprint": .string(machineFingerprint),
                "master_secret": .base64(masterSecret), "key_id": .string(keyID),
                "identity_generation": .int(Int64(generation)),
                "key_epoch": .int(Int64(keyEpoch)),
                "revocation_epoch": .int(Int64(revocationEpoch)),
                "paired_devices": .array(paired.map { row in .object([
                    "device_id": .string(row.deviceID), "signing_key": .base64(row.signingKey),
                    "fingerprint": .string(row.fingerprint), "paired_at": .int(row.pairedAt),
                    "identity_generation": .int(Int64(row.generation)),
                    "capabilities": .array(row.capabilities.map(CloudJSONValue.string)),
                ]) }),
                "revoked_devices": .array(revoked.map { row in .object([
                    "device_id": .string(row.deviceID),
                    "revocation_epoch": .int(Int64(row.revocationEpoch)),
                ]) }),
                "retired_key_ids": .array(retiredKeyIDs.map(CloudJSONValue.string)),
                "pending_handover": pendingValue(pending),
                "completed_handover": pendingValue(completed),
            ]))
        }

        static func decode(_ bytes: Data) throws -> Record {
            guard bytes.count <= CloudExecutorIdentityAuthority.maximumProtectedStateBytes else {
                throw CloudExecutorIdentityError.protectedStateCorrupt
            }
            let value: CloudJSONValue
            do { value = try CloudCanonicalJSON.parseStrict(bytes) }
            catch { throw CloudExecutorIdentityError.protectedStateCorrupt }
            guard case .object(let object) = value,
                  Set(object.keys) == Set([
                    "v", "type", "account_id", "machine_id", "device_id",
                    "machine_private_key", "machine_public_key", "machine_fingerprint",
                    "master_secret", "key_id", "identity_generation", "key_epoch",
                    "revocation_epoch", "paired_devices", "revoked_devices",
                    "retired_key_ids", "pending_handover", "completed_handover",
                  ]), case .int(let version)? = object["v"] else {
                throw CloudExecutorIdentityError.protectedStateCorrupt
            }
            guard version == 1 else {
                if version > 1 { throw CloudExecutorIdentityError.protectedStateFutureVersion(version) }
                throw CloudExecutorIdentityError.protectedStateCorrupt
            }
            guard case .string("executor_identity")? = object["type"] else {
                throw CloudExecutorIdentityError.protectedStateCorrupt
            }
            func text(_ key: String) throws -> String {
                guard case .string(let value)? = object[key] else {
                    throw CloudExecutorIdentityError.protectedStateCorrupt
                }
                return value
            }
            func epoch(_ key: String) throws -> UInt64 {
                guard case .int(let value)? = object[key], value >= 0 else {
                    throw CloudExecutorIdentityError.protectedStateCorrupt
                }
                return UInt64(value)
            }
            func binary(_ key: String, _ length: Int) throws -> Data {
                try CloudPairing.decodeCanonicalBase64(text(key), field: key, expectedLength: length)
            }
            func parsePending(_ value: CloudJSONValue?) throws -> Pending? {
                if case .null? = value { return nil }
                guard case .object(let row)? = value,
                      Set(row.keys) == Set([
                        "pairing_id", "claim_nonce_sha256", "offer_sha256", "viewer_device_id",
                        "viewer_signing_key", "viewer_fingerprint", "machine_fingerprint",
                        "expires_at", "wrapper", "prepared_generation",
                      ]) else { throw CloudExecutorIdentityError.protectedStateCorrupt }
                func rowText(_ key: String) throws -> String {
                    guard case .string(let text)? = row[key] else {
                        throw CloudExecutorIdentityError.protectedStateCorrupt
                    }
                    return text
                }
                guard case .int(let expires)? = row["expires_at"], expires >= 0,
                      case .int(let generation)? = row["prepared_generation"], generation > 0 else {
                    throw CloudExecutorIdentityError.protectedStateCorrupt
                }
                return Pending(
                    pairingID: try rowText("pairing_id"),
                    claimNonceDigest: try CloudPairing.decodeCanonicalBase64(
                        rowText("claim_nonce_sha256"), field: "claim_nonce_sha256", expectedLength: 32),
                    offerDigest: try CloudPairing.decodeCanonicalBase64(
                        rowText("offer_sha256"), field: "offer_sha256", expectedLength: 32),
                    viewerDeviceID: try rowText("viewer_device_id"),
                    viewerSigningKey: try CloudPairing.decodeCanonicalBase64(
                        rowText("viewer_signing_key"), field: "viewer_signing_key", expectedLength: 32),
                    viewerFingerprint: try rowText("viewer_fingerprint"),
                    machineFingerprint: try rowText("machine_fingerprint"),
                    expiresAt: expires,
                    wrapperBytes: try CloudPairing.decodeCanonicalBase64(
                        rowText("wrapper"), field: "wrapper"),
                    preparedGeneration: UInt64(generation))
            }
            guard case .array(let pairedValues)? = object["paired_devices"],
                  case .array(let revokedValues)? = object["revoked_devices"],
                  case .array(let retiredValues)? = object["retired_key_ids"] else {
                throw CloudExecutorIdentityError.protectedStateCorrupt
            }
            var paired: [Paired] = []
            for value in pairedValues {
                guard case .object(let row) = value,
                      Set(row.keys) == Set([
                        "device_id", "signing_key", "fingerprint", "paired_at",
                        "identity_generation", "capabilities",
                      ]), case .string(let id)? = row["device_id"],
                      case .string(let key)? = row["signing_key"],
                      case .string(let fingerprint)? = row["fingerprint"],
                      case .int(let pairedAt)? = row["paired_at"], pairedAt >= 0,
                      case .int(let generation)? = row["identity_generation"], generation > 0,
                      case .array(let capabilityValues)? = row["capabilities"] else {
                    throw CloudExecutorIdentityError.protectedStateCorrupt
                }
                let capabilities = try capabilityValues.map { value -> String in
                    guard case .string(let capability) = value else {
                        throw CloudExecutorIdentityError.protectedStateCorrupt
                    }
                    return capability
                }
                paired.append(Paired(
                    deviceID: id,
                    signingKey: try CloudPairing.decodeCanonicalBase64(
                        key, field: "signing_key", expectedLength: 32),
                    fingerprint: fingerprint, pairedAt: pairedAt,
                    generation: UInt64(generation), capabilities: capabilities))
            }
            var revoked: [Revoked] = []
            for value in revokedValues {
                guard case .object(let row) = value,
                      Set(row.keys) == Set(["device_id", "revocation_epoch"]),
                      case .string(let id)? = row["device_id"],
                      case .int(let revocation)? = row["revocation_epoch"], revocation > 0 else {
                    throw CloudExecutorIdentityError.protectedStateCorrupt
                }
                revoked.append(Revoked(deviceID: id, revocationEpoch: UInt64(revocation)))
            }
            let retired = try retiredValues.map { value -> String in
                guard case .string(let id) = value else {
                    throw CloudExecutorIdentityError.protectedStateCorrupt
                }
                return id
            }
            let record = Record(
                accountID: try text("account_id"), machineID: try text("machine_id"),
                deviceID: try text("device_id"), machinePrivateKey: try binary("machine_private_key", 32),
                machinePublicKey: try binary("machine_public_key", 32),
                machineFingerprint: try text("machine_fingerprint"),
                masterSecret: try binary("master_secret", 32), keyID: try text("key_id"),
                generation: try epoch("identity_generation"), keyEpoch: try epoch("key_epoch"),
                revocationEpoch: try epoch("revocation_epoch"), paired: paired, revoked: revoked,
                retiredKeyIDs: retired, pending: try parsePending(object["pending_handover"]),
                completed: try parsePending(object["completed_handover"]))
            try record.validate()
            return record
        }

        func validate() throws {
            guard PairingOffer.validID(accountID), PairingOffer.validID(machineID),
                  PairingOffer.validID(deviceID), PairingOffer.validID(keyID),
                  machineID == deviceID, generation > 0, keyEpoch > 0,
                  machinePrivateKey.count == 32, machinePublicKey.count == 32,
                  masterSecret.count == 32,
                  paired.count <= 8, revoked.count <= 8, retiredKeyIDs.count <= 8,
                  Set(paired.map(\.deviceID)).count == paired.count,
                  Set(revoked.map(\.deviceID)).count == revoked.count,
                  Set(paired.map(\.deviceID)).isDisjoint(with: Set(revoked.map(\.deviceID))),
                  retiredKeyIDs == Array(Set(retiredKeyIDs)).sorted(),
                  !retiredKeyIDs.contains(keyID), paired == paired.sorted(by: { $0.deviceID < $1.deviceID }),
                  revoked.map(\.deviceID) == revoked.map(\.deviceID).sorted() else {
                throw CloudExecutorIdentityError.protectedStateCorrupt
            }
            guard encoded().count <= CloudExecutorIdentityAuthority.maximumProtectedStateBytes else {
                throw CloudExecutorIdentityError.capacityExceeded
            }
            let key = try CloudDeviceKeyPair(privateKeyRaw: machinePrivateKey)
            guard key.publicKeyRaw == machinePublicKey,
                  key.pairingFingerprint == machineFingerprint else {
                throw CloudExecutorIdentityError.protectedStateCorrupt
            }
            for row in paired {
                guard PairingOffer.validID(row.deviceID), row.signingKey.count == 32,
                      row.fingerprint == (try CloudPairing.ed25519Fingerprint(publicKeyRaw: row.signingKey)),
                      row.pairedAt >= 0, row.generation <= generation,
                      row.capabilities == Array(Set(row.capabilities)).sorted(),
                      Set(row.capabilities).isSubset(of: Set(defaultCapabilities)) else {
                    throw CloudExecutorIdentityError.protectedStateCorrupt
                }
            }
            for row in revoked where !PairingOffer.validID(row.deviceID)
                || row.revocationEpoch == 0 || row.revocationEpoch > revocationEpoch {
                throw CloudExecutorIdentityError.protectedStateCorrupt
            }
            for row in [pending, completed].compactMap({ $0 }) {
                guard PairingOffer.validID(row.pairingID), PairingOffer.validID(row.viewerDeviceID),
                      row.claimNonceDigest.count == 32, row.offerDigest.count == 32,
                      row.viewerSigningKey.count == 32, row.expiresAt >= 0,
                      row.wrapperBytes.count <= CloudExecutorIdentityAuthority.maximumHandoverWrapperBytes,
                      row.preparedGeneration <= generation,
                      row.machineFingerprint == machineFingerprint,
                      row.viewerFingerprint == (try CloudPairing.ed25519Fingerprint(
                        publicKeyRaw: row.viewerSigningKey)) else {
                    throw CloudExecutorIdentityError.protectedStateCorrupt
                }
                let wrapper = try CloudPairing.decodeWrapper(row.wrapperBytes)
                guard wrapper.phase == .grant, wrapper.pairingID == row.pairingID,
                      wrapper.senderDeviceID == deviceID else {
                    throw CloudExecutorIdentityError.protectedStateCorrupt
                }
            }
        }
    }
}

private extension CloudExecutorIdentitySnapshot {
    var transportBinding: CloudExecutorTransportBinding {
        CloudExecutorTransportBinding(
            accountID: accountID, machineID: machineID, deviceID: deviceID,
            signingKeyFingerprint: machineFingerprint,
            identityGeneration: identityGeneration, keyEpoch: keyEpoch,
            revocationEpoch: revocationEpoch)
    }
}

private enum CloudBase32 {
    private static let alphabet = Array("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567".utf8)

    static func encode(_ data: Data) -> String {
        var buffer: UInt32 = 0
        var bitCount = 0
        var output: [UInt8] = []
        for byte in data {
            buffer = (buffer << 8) | UInt32(byte)
            bitCount += 8
            while bitCount >= 5 {
                bitCount -= 5
                output.append(alphabet[Int((buffer >> UInt32(bitCount)) & 31)])
            }
            if bitCount == 0 { buffer = 0 }
            else { buffer &= (1 << UInt32(bitCount)) - 1 }
        }
        if bitCount > 0 {
            output.append(alphabet[Int((buffer << UInt32(5 - bitCount)) & 31)])
        }
        return String(decoding: output, as: UTF8.self)
    }

    static func decode(_ text: String) -> Data? {
        var lookup: [UInt8: UInt8] = [:]
        for (index, character) in alphabet.enumerated() { lookup[character] = UInt8(index) }
        var buffer: UInt32 = 0
        var bitCount = 0
        var output = Data()
        for character in text.utf8 {
            guard let value = lookup[character] else { return nil }
            buffer = (buffer << 5) | UInt32(value)
            bitCount += 5
            if bitCount >= 8 {
                bitCount -= 8
                output.append(UInt8((buffer >> UInt32(bitCount)) & 0xff))
            }
            if bitCount == 0 { buffer = 0 }
            else { buffer &= (1 << UInt32(bitCount)) - 1 }
        }
        // RFC 4648's unused low bits must be zero; accepting both spellings would make
        // checksummed recovery codes needlessly non-canonical.
        if bitCount > 0 && buffer != 0 { return nil }
        return output
    }

    static func group(_ text: String, every count: Int) -> String {
        stride(from: 0, to: text.count, by: count).map { offset in
            let start = text.index(text.startIndex, offsetBy: offset)
            let end = text.index(start, offsetBy: min(count, text.count - offset))
            return String(text[start..<end])
        }.joined(separator: "-")
    }
}
