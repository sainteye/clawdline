import Foundation
import CryptoKit

/// Maximum UTF-8 byte count of the complete phase-write body (design §5.2): the exact
/// two-member `{claim_nonce, blob}` object — claim nonce, wrapper, base64 ct and every field
/// included. This is intentionally not a decoded-ciphertext limit, and it is measured on the
/// complete body by `encodePhaseWriteBody`/`decodePhaseWriteBody`; the wrapper-layer coders
/// apply the same constant to wrapper bytes only as a conservative pre-check (wrapper bytes
/// are strictly smaller than any body embedding them, so that check can only over-reject).
public let PAIRING_PHASE_MAX_BYTES = 65_536

public enum CloudPairingPhase: String, CaseIterable, Hashable {
    case offer
    case grant
    case activate
    case confirm
}

public enum CloudPairingError: Error, Equatable {
    case invalidLength(field: String, expected: String, actual: Int)
    case invalidASCII(field: String)
    case invalidBase64(field: String)
    case nonCanonicalBase64(field: String)
    case invalidKey(field: String)
    case invalidVersion
    case invalidType
    case invalidPhase(String)
    case invalidWrapperFields
    case invalidPhaseBodyFields
    case invalidQRFields
    case zeroSharedSecret
    case keyBindingMismatch
    case phaseBodyTooLarge(actual: Int)
    case unsafeEpochMilliseconds
    case qrExpired
    case qrExpiryTooFar
    case fingerprintMismatch
    case reusedNonce
    case ciphertextMalformed
    case authenticationFailed
}

/// The KDF outputs. Its rendering is stated rather than inherited, in both halves that a
/// diagnostic can reach. `description` is the half that would survive a change of member
/// type: `Data` happens to describe itself as "32 bytes" today, so the synthesised
/// description hides the key by accident, and a member later held as `[UInt8]` or as a
/// base64 `String` would start printing it. `customMirror` is the half that matters right
/// now: `dump` and every Mirror-walking logger print a `Data` member byte by byte, and
/// `CustomStringConvertible` alone does not stop them. Every member is reduced to a byte
/// count — not only `prk` and `phaseKey` — because `saltPreimage` carries the claim nonce
/// and the pairing nonce verbatim, and a mirror that redacts some of its children looks
/// safe while still handing over a secret. The values themselves are unchanged and remain
/// available by name; only the renderings are.
public struct CloudPairingDerivedMaterial: Equatable, CustomStringConvertible,
                                           CustomDebugStringConvertible, CustomReflectable {
    public let saltPreimage: Data
    public let salt: Data
    public let prk: Data
    public let info: Data
    public let phaseKey: Data

    private var redactedMembers: [(String, String)] {
        [
            ("saltPreimage", "<redacted \(saltPreimage.count) bytes>"),
            ("salt", "<redacted \(salt.count) bytes>"),
            ("prk", "<redacted \(prk.count) bytes>"),
            ("info", "<redacted \(info.count) bytes>"),
            ("phaseKey", "<redacted \(phaseKey.count) bytes>")
        ]
    }

    public var description: String {
        let members = redactedMembers.map { "\($0.0): \($0.1)" }.joined(separator: ", ")
        return "CloudPairingDerivedMaterial(\(members))"
    }

    public var debugDescription: String { description }

    public var customMirror: Mirror {
        Mirror(
            self,
            children: redactedMembers.map { (label: Optional($0.0), value: $0.1 as Any) },
            displayStyle: .struct
        )
    }
}

public struct CloudPairingWrapper: Equatable {
    public var version: Int64
    public var phase: CloudPairingPhase
    public var pairingID: String
    public var senderDeviceID: String
    public var ephemeralKey: String
    public var nonce: String
    public var ciphertext: String

    public init(
        version: Int64 = 1,
        phase: CloudPairingPhase,
        pairingID: String,
        senderDeviceID: String,
        ephemeralKey: String,
        nonce: String,
        ciphertext: String
    ) {
        self.version = version
        self.phase = phase
        self.pairingID = pairingID
        self.senderDeviceID = senderDeviceID
        self.ephemeralKey = ephemeralKey
        self.nonce = nonce
        self.ciphertext = ciphertext
    }

    public var cloudJSONValue: CloudJSONValue {
        .object([
            "v": .int(version),
            "phase": .string(phase.rawValue),
            "pairing_id": .string(pairingID),
            "sender_device_id": .string(senderDeviceID),
            "ephemeral_key": .string(ephemeralKey),
            "nonce": .string(nonce),
            "ct": .string(ciphertext)
        ])
    }
}

public struct CloudPairingQR: Equatable {
    public var pairingID: String
    public var claimNonce: String
    public var expiresAt: Int64
    public var accountID: String
    public var machineID: String
    public var machineSigningKey: String
    public var machineFingerprint: String
    public var machineEphemeralKey: String
    public var pairingNonce: String

    public init(
        pairingID: String,
        claimNonce: String,
        expiresAt: Int64,
        accountID: String,
        machineID: String,
        machineSigningKey: String,
        machineFingerprint: String,
        machineEphemeralKey: String,
        pairingNonce: String
    ) {
        self.pairingID = pairingID
        self.claimNonce = claimNonce
        self.expiresAt = expiresAt
        self.accountID = accountID
        self.machineID = machineID
        self.machineSigningKey = machineSigningKey
        self.machineFingerprint = machineFingerprint
        self.machineEphemeralKey = machineEphemeralKey
        self.pairingNonce = pairingNonce
    }

    /// The exact eleven-field QR object. Binary values have already been reduced to their sole
    /// wire representation: canonical RFC 4648 standard padded base64 strings.
    public var cloudJSONValue: CloudJSONValue {
        .object([
            "v": .int(1),
            "type": .string("pairing_qr"),
            "pairing_id": .string(pairingID),
            "claim_nonce": .string(claimNonce),
            "expires_at": .int(expiresAt),
            "account_id": .string(accountID),
            "machine_id": .string(machineID),
            "machine_signing_key": .string(machineSigningKey),
            "machine_fingerprint": .string(machineFingerprint),
            "machine_ephemeral_key": .string(machineEphemeralKey),
            "pairing_nonce": .string(pairingNonce)
        ])
    }
}

public enum CloudPairing {
    private static let saltDomain = Data("clawdline-pair-salt-v1".utf8)
    private static let infoDomain = Data("clawdline-pair-v1".utf8)
    private static let safeIntegerMaximum: Int64 = 9_007_199_254_740_991
    private static let qrLifetimeMilliseconds: Int64 = 600_000

    // MARK: - KDF and key agreement

    /// `uint16_be(byteLength(x)) || x`.
    public static func l16(_ bytes: Data) throws -> Data {
        guard bytes.count <= Int(UInt16.max) else {
            throw CloudPairingError.invalidLength(field: "L16", expected: "0...65535", actual: bytes.count)
        }
        var result = Data([UInt8(bytes.count >> 8), UInt8(bytes.count & 0xFF)])
        result.append(bytes)
        return result
    }

    public static func x25519SharedSecret(privateKeyRaw: Data, peerPublicKeyRaw: Data) throws -> Data {
        try requireLength(privateKeyRaw, field: "private_key", expected: 32)
        try requireLength(peerPublicKeyRaw, field: "peer_public_key", expected: 32)
        do {
            let privateKey = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: privateKeyRaw)
            let publicKey = try Curve25519.KeyAgreement.PublicKey(rawRepresentation: peerPublicKeyRaw)
            let shared = try privateKey.sharedSecretFromKeyAgreement(with: publicKey)
            let raw = shared.withUnsafeBytes { Data($0) }
            try rejectZeroSharedSecret(raw)
            return raw
        } catch let error as CloudPairingError {
            throw error
        } catch {
            throw CloudPairingError.invalidKey(field: "x25519")
        }
    }

    public static func derive(
        sharedSecretRaw: Data,
        pairingNonce: Data,
        pairingID: String,
        claimNonce: Data,
        phase: CloudPairingPhase
    ) throws -> CloudPairingDerivedMaterial {
        try requireLength(sharedSecretRaw, field: "shared_secret", expected: 32)
        try rejectZeroSharedSecret(sharedSecretRaw)
        try requireLength(pairingNonce, field: "pairing_nonce", expected: 32)
        try requireLength(claimNonce, field: "claim_nonce", expected: 32)
        try validateID(pairingID, field: "pairing_id")

        var preimage = try l16(saltDomain)
        preimage.append(try l16(pairingNonce))
        preimage.append(try l16(Data(pairingID.utf8)))
        preimage.append(try l16(claimNonce))
        let salt = Data(SHA256.hash(data: preimage))
        let prk = hmacSHA256(key: salt, data: sharedSecretRaw)

        var info = try l16(infoDomain)
        info.append(try l16(Data(phase.rawValue.utf8)))
        var expandInput = info
        expandInput.append(0x01)
        let phaseKey = hmacSHA256(key: prk, data: expandInput)
        return CloudPairingDerivedMaterial(
            saltPreimage: preimage,
            salt: salt,
            prk: prk,
            info: info,
            phaseKey: phaseKey
        )
    }

    /// The only permitted claim-nonce storage value: SHA-256 of exactly 32 decoded nonce bytes.
    public static func claimNonceSHA256(_ decodedClaimNonce: Data) throws -> Data {
        try requireLength(decodedClaimNonce, field: "claim_nonce", expected: 32)
        return Data(SHA256.hash(data: decodedClaimNonce))
    }

    // MARK: - Standard base64 and QR base64url

    public static func decodeCanonicalBase64(
        _ text: String,
        field: String,
        expectedLength: Int? = nil,
        minimumLength: Int? = nil
    ) throws -> Data {
        guard text.unicodeScalars.allSatisfy({ $0.value <= 0x7F }),
              let decoded = Data(base64Encoded: text) else {
            throw CloudPairingError.invalidBase64(field: field)
        }
        guard decoded.base64EncodedString() == text else {
            throw CloudPairingError.nonCanonicalBase64(field: field)
        }
        if let expectedLength, decoded.count != expectedLength {
            throw CloudPairingError.invalidLength(
                field: field,
                expected: String(expectedLength),
                actual: decoded.count
            )
        }
        if let minimumLength, decoded.count < minimumLength {
            throw CloudPairingError.invalidLength(
                field: field,
                expected: ">=\(minimumLength)",
                actual: decoded.count
            )
        }
        return decoded
    }

    public static func encodeCanonicalBase64URL(_ data: Data) -> String {
        data.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    public static func decodeCanonicalBase64URL(_ text: String) throws -> Data {
        guard !text.isEmpty,
              !text.contains("="), !text.contains("+"), !text.contains("/"),
              text.unicodeScalars.allSatisfy({ scalar in
                  switch scalar.value {
                  case 45, 48...57, 65...90, 95, 97...122: return true
                  default: return false
                  }
              }),
              text.utf8.count % 4 != 1 else {
            throw CloudPairingError.nonCanonicalBase64(field: "qr_fragment")
        }
        var standard = text.replacingOccurrences(of: "-", with: "+")
            .replacingOccurrences(of: "_", with: "/")
        standard += String(repeating: "=", count: (4 - standard.utf8.count % 4) % 4)
        guard let decoded = Data(base64Encoded: standard) else {
            throw CloudPairingError.invalidBase64(field: "qr_fragment")
        }
        guard encodeCanonicalBase64URL(decoded) == text else {
            throw CloudPairingError.nonCanonicalBase64(field: "qr_fragment")
        }
        return decoded
    }

    // MARK: - Wrapper, AAD, and AES-GCM

    /// Canonical AAD is reconstructed from the five authenticated wrapper members. `nonce` and
    /// `ct` never enter this value, even transiently.
    public static func aad(for wrapper: CloudPairingWrapper) throws -> Data {
        guard wrapper.version == 1 else { throw CloudPairingError.invalidVersion }
        try validateID(wrapper.pairingID, field: "pairing_id")
        try validateID(wrapper.senderDeviceID, field: "sender_device_id")
        _ = try decodeCanonicalBase64(wrapper.ephemeralKey, field: "ephemeral_key", expectedLength: 32)
        return CloudCanonicalJSON.canonicalData(.object([
            "v": .int(1),
            "phase": .string(wrapper.phase.rawValue),
            "pairing_id": .string(wrapper.pairingID),
            "sender_device_id": .string(wrapper.senderDeviceID),
            "ephemeral_key": .string(wrapper.ephemeralKey)
        ]))
    }

    /// The size cap of design §5.2, measured over whatever bytes it is handed. The
    /// authoritative call sites hand it the complete two-member phase-write body
    /// (`encodePhaseWriteBody`/`decodePhaseWriteBody`); the wrapper-layer coders hand it
    /// wrapper bytes only, as a conservative pre-check.
    public static func validatePhaseWriteBodySize(_ body: Data) throws {
        guard body.count <= PAIRING_PHASE_MAX_BYTES else {
            throw CloudPairingError.phaseBodyTooLarge(actual: body.count)
        }
    }

    public static func encodeWrapper(_ wrapper: CloudPairingWrapper) throws -> Data {
        try validateWrapper(wrapper)
        let bytes = encodeWrapperUnchecked(wrapper)
        // Conservative only: wrapper bytes past the cap can never fit inside a legal body.
        try validatePhaseWriteBodySize(bytes)
        return bytes
    }

    /// Serialization without semantic validation, useful when a caller needs to construct an
    /// intentionally invalid body for a rejection test. Receivers must always use `decodeWrapper`.
    public static func encodeWrapperUnchecked(_ wrapper: CloudPairingWrapper) -> Data {
        CloudCanonicalJSON.canonicalData(wrapper.cloudJSONValue)
    }

    public static func decodeWrapper(_ body: Data) throws -> CloudPairingWrapper {
        // Conservative pre-check on wrapper bytes; the spec's 65,536 boundary is enforced on
        // the complete two-member body by `decodePhaseWriteBody`.
        try validatePhaseWriteBodySize(body)
        let value = try CloudCanonicalJSON.parseStrict(body)
        return try wrapper(from: value)
    }

    // MARK: - Complete phase-write body (design §5.2)

    /// The complete request body of the four phase-write APIs: exactly two members,
    /// `claim_nonce` (canonical padded base64 of 32 nonce bytes) and `blob` (the seven-field
    /// wrapper), serialized as one RFC 8785 canonical JSON object. `PAIRING_PHASE_MAX_BYTES`
    /// is measured over these bytes — the whole body, not the wrapper and not the decoded ct.
    public static func encodePhaseWriteBody(claimNonce: String, wrapper: CloudPairingWrapper) throws -> Data {
        _ = try decodeCanonicalBase64(claimNonce, field: "claim_nonce", expectedLength: 32)
        try validateWrapper(wrapper)
        let bytes = encodePhaseWriteBodyUnchecked(claimNonce: claimNonce, wrapper: wrapper)
        try validatePhaseWriteBodySize(bytes)
        return bytes
    }

    /// Serialization without semantic or size validation, for constructing intentionally
    /// invalid bodies in rejection tests. Receivers must always use `decodePhaseWriteBody`.
    public static func encodePhaseWriteBodyUnchecked(claimNonce: String, wrapper: CloudPairingWrapper) -> Data {
        CloudCanonicalJSON.canonicalData(.object([
            "claim_nonce": .string(claimNonce),
            "blob": wrapper.cloudJSONValue
        ]))
    }

    public static func decodePhaseWriteBody(_ body: Data) throws -> (claimNonce: String, wrapper: CloudPairingWrapper) {
        // The authoritative §5.2 boundary: the complete body's UTF-8 bytes, checked before
        // any parsing so an oversized body is refused without being interpreted.
        try validatePhaseWriteBodySize(body)
        let value = try CloudCanonicalJSON.parseStrict(body)
        guard case .object(let members) = value,
              Set(members.keys) == Set(["claim_nonce", "blob"]) else {
            throw CloudPairingError.invalidPhaseBodyFields
        }
        guard case .string(let claimNonce)? = members["claim_nonce"] else {
            throw CloudPairingError.invalidPhaseBodyFields
        }
        _ = try decodeCanonicalBase64(claimNonce, field: "claim_nonce", expectedLength: 32)
        guard case .object? = members["blob"] else {
            throw CloudPairingError.invalidPhaseBodyFields
        }
        return (claimNonce, try wrapper(from: members["blob"]!))
    }

    private static func wrapper(from value: CloudJSONValue) throws -> CloudPairingWrapper {
        guard case .object(let object) = value,
              Set(object.keys) == Set(["v", "phase", "pairing_id", "sender_device_id", "ephemeral_key", "nonce", "ct"]) else {
            throw CloudPairingError.invalidWrapperFields
        }
        guard case .int(let version)? = object["v"], version == 1 else {
            throw CloudPairingError.invalidVersion
        }
        let phaseText = try stringValue(object, key: "phase", error: .invalidWrapperFields)
        guard let phase = CloudPairingPhase(rawValue: phaseText) else {
            throw CloudPairingError.invalidPhase(phaseText)
        }
        let wrapper = CloudPairingWrapper(
            version: version,
            phase: phase,
            pairingID: try stringValue(object, key: "pairing_id", error: .invalidWrapperFields),
            senderDeviceID: try stringValue(object, key: "sender_device_id", error: .invalidWrapperFields),
            ephemeralKey: try stringValue(object, key: "ephemeral_key", error: .invalidWrapperFields),
            nonce: try stringValue(object, key: "nonce", error: .invalidWrapperFields),
            ciphertext: try stringValue(object, key: "ct", error: .invalidWrapperFields)
        )
        try validateWrapper(wrapper)
        return wrapper
    }

    public static func validateKeyBinding(
        _ wrapper: CloudPairingWrapper,
        viewerEphemeralKey: String,
        machineEphemeralKey: String
    ) throws {
        _ = try decodeCanonicalBase64(viewerEphemeralKey, field: "viewer_ephemeral_key", expectedLength: 32)
        _ = try decodeCanonicalBase64(machineEphemeralKey, field: "machine_ephemeral_key", expectedLength: 32)
        _ = try decodeCanonicalBase64(wrapper.ephemeralKey, field: "ephemeral_key", expectedLength: 32)
        let expected: String
        switch wrapper.phase {
        case .offer, .activate: expected = viewerEphemeralKey
        case .grant, .confirm: expected = machineEphemeralKey
        }
        guard wrapper.ephemeralKey == expected else {
            throw CloudPairingError.keyBindingMismatch
        }
    }

    public static func seal(
        plaintext: Data,
        phase: CloudPairingPhase,
        pairingID: String,
        senderDeviceID: String,
        ephemeralKey: String,
        phaseKey: Data,
        nonce: Data
    ) throws -> CloudPairingWrapper {
        try requireLength(phaseKey, field: "phase_key", expected: 32)
        try requireLength(nonce, field: "nonce", expected: 12)
        var wrapper = CloudPairingWrapper(
            phase: phase,
            pairingID: pairingID,
            senderDeviceID: senderDeviceID,
            ephemeralKey: ephemeralKey,
            nonce: nonce.base64EncodedString(),
            ciphertext: Data(repeating: 0, count: 16).base64EncodedString()
        )
        let authenticatedData = try aad(for: wrapper)
        do {
            let sealed = try AES.GCM.seal(
                plaintext,
                using: SymmetricKey(data: phaseKey),
                nonce: try AES.GCM.Nonce(data: nonce),
                authenticating: authenticatedData
            )
            var ciphertextAndTag = sealed.ciphertext
            ciphertextAndTag.append(sealed.tag)
            wrapper.ciphertext = ciphertextAndTag.base64EncodedString()
            return wrapper
        } catch {
            throw CloudPairingError.authenticationFailed
        }
    }

    public static func open(
        _ wrapper: CloudPairingWrapper,
        phaseKey: Data,
        viewerEphemeralKey: String,
        machineEphemeralKey: String
    ) throws -> Data {
        // Binding deliberately precedes nonce/ciphertext parsing and AES-GCM open.
        try validateKeyBinding(
            wrapper,
            viewerEphemeralKey: viewerEphemeralKey,
            machineEphemeralKey: machineEphemeralKey
        )
        try requireLength(phaseKey, field: "phase_key", expected: 32)
        let nonce = try decodeCanonicalBase64(wrapper.nonce, field: "nonce", expectedLength: 12)
        let ciphertextAndTag = try decodeCanonicalBase64(wrapper.ciphertext, field: "ct", minimumLength: 16)
        let split = ciphertextAndTag.count - 16
        let ciphertext = ciphertextAndTag.prefix(split)
        let tag = ciphertextAndTag.suffix(16)
        let authenticatedData = try aad(for: wrapper)
        do {
            let box = try AES.GCM.SealedBox(
                nonce: try AES.GCM.Nonce(data: nonce),
                ciphertext: ciphertext,
                tag: tag
            )
            return try AES.GCM.open(box, using: SymmetricKey(data: phaseKey), authenticating: authenticatedData)
        } catch {
            throw CloudPairingError.authenticationFailed
        }
    }

    // MARK: - QR contract and fingerprint

    public static func ed25519Fingerprint(publicKeyRaw: Data) throws -> String {
        try requireLength(publicKeyRaw, field: "machine_signing_key", expected: 32)
        let digestPrefix = Data(SHA256.hash(data: publicKeyRaw).prefix(10))
        let ungrouped = base32WithoutPadding(digestPrefix)
        return stride(from: 0, to: ungrouped.count, by: 4).map { offset in
            let start = ungrouped.index(ungrouped.startIndex, offsetBy: offset)
            let end = ungrouped.index(start, offsetBy: min(4, ungrouped.count - offset))
            return String(ungrouped[start..<end])
        }.joined(separator: "-")
    }

    public static func encodeQRFragment(_ qr: CloudPairingQR) throws -> String {
        try validateQR(qr, nowMilliseconds: nil)
        return encodeQRFragmentUnchecked(qr)
    }

    public static func encodeQRFragmentUnchecked(_ qr: CloudPairingQR) -> String {
        encodeCanonicalBase64URL(CloudCanonicalJSON.canonicalData(qr.cloudJSONValue))
    }

    public static func decodeQRFragment(_ fragment: String, nowMilliseconds: Int64) throws -> CloudPairingQR {
        let bytes = try decodeCanonicalBase64URL(fragment)
        let value = try CloudCanonicalJSON.parseStrict(bytes)
        guard case .object(let object) = value,
              Set(object.keys) == Set([
                  "v", "type", "pairing_id", "claim_nonce", "expires_at", "account_id",
                  "machine_id", "machine_signing_key", "machine_fingerprint",
                  "machine_ephemeral_key", "pairing_nonce"
              ]) else {
            throw CloudPairingError.invalidQRFields
        }
        guard case .int(let version)? = object["v"], version == 1 else {
            throw CloudPairingError.invalidVersion
        }
        guard case .string(let type)? = object["type"], type == "pairing_qr" else {
            throw CloudPairingError.invalidType
        }
        guard case .int(let expiresAt)? = object["expires_at"] else {
            throw CloudPairingError.invalidQRFields
        }
        let qr = CloudPairingQR(
            pairingID: try stringValue(object, key: "pairing_id", error: .invalidQRFields),
            claimNonce: try stringValue(object, key: "claim_nonce", error: .invalidQRFields),
            expiresAt: expiresAt,
            accountID: try stringValue(object, key: "account_id", error: .invalidQRFields),
            machineID: try stringValue(object, key: "machine_id", error: .invalidQRFields),
            machineSigningKey: try stringValue(object, key: "machine_signing_key", error: .invalidQRFields),
            machineFingerprint: try stringValue(object, key: "machine_fingerprint", error: .invalidQRFields),
            machineEphemeralKey: try stringValue(object, key: "machine_ephemeral_key", error: .invalidQRFields),
            pairingNonce: try stringValue(object, key: "pairing_nonce", error: .invalidQRFields)
        )
        try validateQR(qr, nowMilliseconds: nowMilliseconds)
        return qr
    }

    /// Two separately sampled 256-bit values from Swift's system cryptographic RNG. The retry is
    /// only a fail-closed collision guard; neither value is derived from the other or any ID/time.
    public static func generateIndependentNonces() -> (claimNonce: Data, pairingNonce: Data) {
        var generator = SystemRandomNumberGenerator()
        func sample(_ generator: inout SystemRandomNumberGenerator) -> Data {
            Data((0..<32).map { _ in UInt8.random(in: .min ... .max, using: &generator) })
        }
        let claimNonce = sample(&generator)
        var pairingNonce = sample(&generator)
        while pairingNonce == claimNonce {
            pairingNonce = sample(&generator)
        }
        return (claimNonce, pairingNonce)
    }

    // MARK: - Private validation

    private static func validateWrapper(_ wrapper: CloudPairingWrapper) throws {
        _ = try aad(for: wrapper)
        _ = try decodeCanonicalBase64(wrapper.nonce, field: "nonce", expectedLength: 12)
        _ = try decodeCanonicalBase64(wrapper.ciphertext, field: "ct", minimumLength: 16)
    }

    private static func validateQR(_ qr: CloudPairingQR, nowMilliseconds: Int64?) throws {
        try validateID(qr.pairingID, field: "pairing_id")
        try validateID(qr.accountID, field: "account_id")
        try validateID(qr.machineID, field: "machine_id")
        let claimNonce = try decodeCanonicalBase64(qr.claimNonce, field: "claim_nonce", expectedLength: 32)
        let pairingNonce = try decodeCanonicalBase64(qr.pairingNonce, field: "pairing_nonce", expectedLength: 32)
        guard claimNonce != pairingNonce else { throw CloudPairingError.reusedNonce }
        let signingKey = try decodeCanonicalBase64(qr.machineSigningKey, field: "machine_signing_key", expectedLength: 32)
        _ = try decodeCanonicalBase64(qr.machineEphemeralKey, field: "machine_ephemeral_key", expectedLength: 32)
        guard qr.machineFingerprint == (try ed25519Fingerprint(publicKeyRaw: signingKey)) else {
            throw CloudPairingError.fingerprintMismatch
        }
        // Not covered by parseStrict: this guard still fires for encodeQRFragment (which
        // validates before any serialization), for negative wire values (parseStrict accepts
        // the full ±(2^53 − 1) domain, epoch milliseconds only 0...2^53 − 1), and for the
        // caller-supplied clock below. Wire values past 2^53 − 1 die earlier, inside
        // parseStrict, as CloudCanonicalJSONError.integerOutOfRange.
        guard (0...safeIntegerMaximum).contains(qr.expiresAt) else {
            throw CloudPairingError.unsafeEpochMilliseconds
        }
        if let nowMilliseconds {
            guard (0...safeIntegerMaximum).contains(nowMilliseconds) else {
                throw CloudPairingError.unsafeEpochMilliseconds
            }
            guard qr.expiresAt >= nowMilliseconds else { throw CloudPairingError.qrExpired }
            guard qr.expiresAt - nowMilliseconds <= qrLifetimeMilliseconds else {
                throw CloudPairingError.qrExpiryTooFar
            }
        }
    }

    /// An ID is 1...128 bytes of printable ASCII — the same 0x20...0x7E range
    /// `CloudCanonicalJSON.signingInput` demands of a signing domain, and refused here for a
    /// reason of the same kind rather than only for symmetry. Nothing in this unit is
    /// length-ambiguous today: `derive` feeds `pairingID` through `l16`, and the wrapper and
    /// QR forms are JSON strings where a control byte survives as a `\uXXXX` escape, so no
    /// concrete confusion is known at this layer. It is refused anyway because these IDs do
    /// not stay at this layer. They are exactly the values a caller reaches for when it needs
    /// a signing domain, where `CloudCanonicalJSONError.invalidDomain` refuses a NUL because
    /// it would collide with that signing input's NUL separator — an ID admitted here and
    /// refused one layer down only moves the failure somewhere with less context. The rest of
    /// the class travels no better: DEL and the C0 controls are bytes a terminal or a log
    /// reader acts on instead of printing, so an ID carrying one renders as a different ID
    /// than the one that was stored.
    private static func validateID(_ text: String, field: String) throws {
        let bytes = Array(text.utf8)
        guard (1...128).contains(bytes.count),
              text.unicodeScalars.allSatisfy({ $0.value >= 0x20 && $0.value <= 0x7E }) else {
            throw CloudPairingError.invalidASCII(field: field)
        }
    }

    private static func requireLength(_ data: Data, field: String, expected: Int) throws {
        guard data.count == expected else {
            throw CloudPairingError.invalidLength(field: field, expected: String(expected), actual: data.count)
        }
    }

    private static func rejectZeroSharedSecret(_ shared: Data) throws {
        guard shared.contains(where: { $0 != 0 }) else {
            throw CloudPairingError.zeroSharedSecret
        }
    }

    private static func hmacSHA256(key: Data, data: Data) -> Data {
        Data(HMAC<SHA256>.authenticationCode(for: data, using: SymmetricKey(data: key)))
    }

    private static func stringValue(
        _ object: [String: CloudJSONValue],
        key: String,
        error: CloudPairingError
    ) throws -> String {
        guard case .string(let value)? = object[key] else { throw error }
        return value
    }

    private static func base32WithoutPadding(_ data: Data) -> String {
        let alphabet = Array("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567".utf8)
        var result = [UInt8]()
        var accumulator: UInt32 = 0
        var bits = 0
        for byte in data {
            accumulator = (accumulator << 8) | UInt32(byte)
            bits += 8
            while bits >= 5 {
                bits -= 5
                result.append(alphabet[Int((accumulator >> UInt32(bits)) & 0x1F)])
            }
        }
        if bits > 0 {
            result.append(alphabet[Int((accumulator << UInt32(5 - bits)) & 0x1F)])
        }
        return String(decoding: result, as: UTF8.self)
    }
}
