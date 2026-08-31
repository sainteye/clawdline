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

import CryptoKit
import Foundation
import os
import Security

enum CloudKeyError: Error, LocalizedError, Equatable {
    case invalidDevicePrivateKey
    case invalidMasterSecretLength(Int)
    case invalidRecoveryCode
    case recoveryChecksumMismatch
    case mainThreadReadForbidden
    case keychain(OSStatus)

    var errorDescription: String? {
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
        case .keychain(let status):
            return "Keychain operation failed with OSStatus \(status)."
        }
    }
}

/// The UI adapter from synchronous Keychain APIs into app state. The operation is captured
/// privately and can only be started on its dedicated serial queue, so a caller cannot
/// accidentally invoke it on the main actor while trying to refresh a screen.
final class CloudKeychainReader<Value: Sendable>: @unchecked Sendable {
    private let queue: DispatchQueue
    private let operation: @Sendable () throws -> Value

    init(
        label: String = "clawdline.cloud.keychain-read",
        operation: @escaping @Sendable () throws -> Value
    ) {
        queue = DispatchQueue(label: label, qos: .utility)
        self.operation = operation
    }

    func read(completion: @escaping @MainActor (Result<Value, Error>) -> Void) {
        queue.async { [operation] in
            let result = Result { try operation() }
            DispatchQueue.main.async { completion(result) }
        }
    }
}

struct CloudDeviceKeyPair: Equatable, Sendable {
    static let privateKeyBytes = 32

    let privateKeyRaw: Data

    init() {
        privateKeyRaw = Curve25519.Signing.PrivateKey().rawRepresentation
    }

    init(privateKeyRaw: Data) throws {
        guard privateKeyRaw.count == Self.privateKeyBytes,
              (try? Curve25519.Signing.PrivateKey(rawRepresentation: privateKeyRaw)) != nil
        else { throw CloudKeyError.invalidDevicePrivateKey }
        self.privateKeyRaw = privateKeyRaw
    }

    var publicKeyRaw: Data {
        // The initializer was checked above (or produced by CryptoKit), so this cannot fail.
        let key = try! Curve25519.Signing.PrivateKey(rawRepresentation: privateKeyRaw)
        return key.publicKey.rawRepresentation
    }

    /// An 80-bit SHA-256 fingerprint, rendered as four groups of RFC 4648 base32.
    /// This is the short value people compare while pairing; it is not a key id.
    var pairingFingerprint: String {
        let digest = Data(SHA256.hash(data: publicKeyRaw).prefix(10))
        return CloudBase32.group(CloudBase32.encode(digest), every: 4)
    }

    func signature(for bytes: Data) throws -> Data {
        let key = try Curve25519.Signing.PrivateKey(rawRepresentation: privateKeyRaw)
        return try key.signature(for: bytes)
    }
}

struct CloudMasterSecret: Equatable, Sendable {
    static let byteCount = 32
    private static let recoveryPrefix = "CLAWD1"
    private static let checksumDomain = Data("clawdline-recovery-v1".utf8)

    let rawRepresentation: Data

    init() throws {
        var bytes = Data(count: Self.byteCount)
        let status = bytes.withUnsafeMutableBytes { raw in
            SecRandomCopyBytes(kSecRandomDefault, Self.byteCount, raw.baseAddress!)
        }
        guard status == errSecSuccess else { throw CloudKeyError.keychain(status) }
        rawRepresentation = bytes
    }

    init(rawRepresentation: Data) throws {
        guard rawRepresentation.count == Self.byteCount else {
            throw CloudKeyError.invalidMasterSecretLength(rawRepresentation.count)
        }
        self.rawRepresentation = rawRepresentation
    }

    init(recoveryCode: String) throws {
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

    var recoveryCode: String {
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
final class CloudKeyStoreCoordinator: Sendable {
    private let lock = NSRecursiveLock()

    func withCriticalRegion<T: Sendable>(_ body: @Sendable () throws -> T) rethrows -> T {
        lock.lock()
        defer { lock.unlock() }
        return try body()
    }
}

private final class CloudKeychainCoordinatorPool: Sendable {
    static let shared = CloudKeychainCoordinatorPool()

    private let coordinators = OSAllocatedUnfairLock(
        initialState: [String: CloudKeyStoreCoordinator]())

    func coordinator(for service: String) -> CloudKeyStoreCoordinator {
        coordinators.withLock { values in
            if let existing = values[service] { return existing }
            let created = CloudKeyStoreCoordinator()
            values[service] = created
            return created
        }
    }
}

/// The narrow persistence seam keeps envelope tests and recovery flows Keychain-free.
protocol CloudKeyStoring: Sendable {
    var coordinator: CloudKeyStoreCoordinator { get }
    func data(for account: String) throws -> Data?
    func set(_ data: Data, for account: String) throws
    func remove(_ account: String) throws
}

final class CloudKeychainStore: CloudKeyStoring {
    static let defaultService = "app.clawdline.cloud.keys"

    let service: String
    let coordinator: CloudKeyStoreCoordinator

    init(service: String = CloudKeychainStore.defaultService) {
        self.service = service
        coordinator = CloudKeychainCoordinatorPool.shared.coordinator(for: service)
    }

    func data(for account: String) throws -> Data? {
        guard !Thread.isMainThread else {
            os_log("Clawdline rejected a Keychain read on the main thread", type: .fault)
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

    func set(_ data: Data, for account: String) throws {
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

    func remove(_ account: String) throws {
        let query = Self.identity(service: service, account: account)
        let status = SecItemDelete(query as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw CloudKeyError.keychain(status)
        }
    }

    static func identity(service: String, account: String) -> [CFString: Any] {
        [
            kSecClass: kSecClassGenericPassword,
            kSecAttrService: service,
            kSecAttrAccount: account,
        ]
    }

    static func insertAttributes(
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

final class CloudInMemoryKeyStore: CloudKeyStoring {
    let coordinator = CloudKeyStoreCoordinator()
    private let values: OSAllocatedUnfairLock<[String: Data]>

    init(values: [String: Data] = [:]) {
        self.values = OSAllocatedUnfairLock(initialState: values)
    }

    func data(for account: String) throws -> Data? {
        values.withLock { $0[account] }
    }

    func set(_ data: Data, for account: String) throws {
        values.withLock { $0[account] = data }
    }

    func remove(_ account: String) throws {
        _ = values.withLock { $0.removeValue(forKey: account) }
    }
}

struct CloudKeys: Sendable {
    static let deviceKeyAccount = "device-ed25519-v1"
    static let masterSecretAccount = "account-master-secret-v1"

    private let store: CloudKeyStoring

    init(store: CloudKeyStoring = CloudKeychainStore()) {
        self.store = store
    }

    func loadOrCreateDeviceKeyPair() throws -> CloudDeviceKeyPair {
        try store.coordinator.withCriticalRegion {
            if let raw = try store.data(for: Self.deviceKeyAccount) {
                return try CloudDeviceKeyPair(privateKeyRaw: raw)
            }
            let pair = CloudDeviceKeyPair()
            try store.set(pair.privateKeyRaw, for: Self.deviceKeyAccount)
            return pair
        }
    }

    func loadOrCreateMasterSecret() throws -> CloudMasterSecret {
        try store.coordinator.withCriticalRegion {
            if let raw = try store.data(for: Self.masterSecretAccount) {
                return try CloudMasterSecret(rawRepresentation: raw)
            }
            let secret = try CloudMasterSecret()
            try store.set(secret.rawRepresentation, for: Self.masterSecretAccount)
            return secret
        }
    }

    func restoreMasterSecret(from recoveryCode: String) throws -> CloudMasterSecret {
        try store.coordinator.withCriticalRegion {
            let secret = try CloudMasterSecret(recoveryCode: recoveryCode)
            try store.set(secret.rawRepresentation, for: Self.masterSecretAccount)
            return secret
        }
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
