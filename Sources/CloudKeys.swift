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
import Security

enum CloudKeyError: Error, LocalizedError, Equatable {
    case invalidDevicePrivateKey
    case invalidMasterSecretLength(Int)
    case invalidRecoveryCode
    case recoveryChecksumMismatch
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
        case .keychain(let status):
            return "Keychain operation failed with OSStatus \(status)."
        }
    }
}

struct CloudDeviceKeyPair: Equatable {
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

struct CloudMasterSecret: Equatable {
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

/// The narrow persistence seam keeps envelope tests and recovery flows Keychain-free.
protocol CloudKeyStoring {
    func data(for account: String) throws -> Data?
    func set(_ data: Data, for account: String) throws
}

final class CloudKeychainStore: CloudKeyStoring {
    static let defaultService = "app.clawdline.cloud.keys"

    let service: String

    init(service: String = CloudKeychainStore.defaultService) {
        self.service = service
    }

    func data(for account: String) throws -> Data? {
        let query: [CFString: Any] = [
            kSecClass: kSecClassGenericPassword,
            kSecAttrService: service,
            kSecAttrAccount: account,
            kSecReturnData: true,
            kSecMatchLimit: kSecMatchLimitOne,
        ]
        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess, let bytes = item as? Data else {
            throw CloudKeyError.keychain(status)
        }
        return bytes
    }

    func set(_ data: Data, for account: String) throws {
        let identity: [CFString: Any] = [
            kSecClass: kSecClassGenericPassword,
            kSecAttrService: service,
            kSecAttrAccount: account,
        ]
        let update: [CFString: Any] = [kSecValueData: data]
        let updateStatus = SecItemUpdate(identity as CFDictionary, update as CFDictionary)
        if updateStatus == errSecSuccess { return }
        guard updateStatus == errSecItemNotFound else {
            throw CloudKeyError.keychain(updateStatus)
        }

        var insert = identity
        insert[kSecValueData] = data
        insert[kSecAttrAccessible] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        let insertStatus = SecItemAdd(insert as CFDictionary, nil)
        guard insertStatus == errSecSuccess else { throw CloudKeyError.keychain(insertStatus) }
    }
}

final class CloudInMemoryKeyStore: CloudKeyStoring {
    private var values: [String: Data]

    init(values: [String: Data] = [:]) {
        self.values = values
    }

    func data(for account: String) throws -> Data? { values[account] }
    func set(_ data: Data, for account: String) throws { values[account] = data }
}

struct CloudKeys {
    static let deviceKeyAccount = "device-ed25519-v1"
    static let masterSecretAccount = "account-master-secret-v1"

    private let store: CloudKeyStoring

    init(store: CloudKeyStoring = CloudKeychainStore()) {
        self.store = store
    }

    func loadOrCreateDeviceKeyPair() throws -> CloudDeviceKeyPair {
        if let raw = try store.data(for: Self.deviceKeyAccount) {
            return try CloudDeviceKeyPair(privateKeyRaw: raw)
        }
        let pair = CloudDeviceKeyPair()
        try store.set(pair.privateKeyRaw, for: Self.deviceKeyAccount)
        return pair
    }

    func loadOrCreateMasterSecret() throws -> CloudMasterSecret {
        if let raw = try store.data(for: Self.masterSecretAccount) {
            return try CloudMasterSecret(rawRepresentation: raw)
        }
        let secret = try CloudMasterSecret()
        try store.set(secret.rawRepresentation, for: Self.masterSecretAccount)
        return secret
    }

    func restoreMasterSecret(from recoveryCode: String) throws -> CloudMasterSecret {
        let secret = try CloudMasterSecret(recoveryCode: recoveryCode)
        try store.set(secret.rawRepresentation, for: Self.masterSecretAccount)
        return secret
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
