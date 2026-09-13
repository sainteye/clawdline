#if canImport(CryptoKit)
import CryptoKit
#else
import Crypto
#endif
import Foundation

public enum CloudEnvelopeError: Error, LocalizedError, Equatable, Sendable {
    case invalidVersion
    case unsafeInteger(String)
    case invalidChannel
    case classChannelMismatch
    case invalidToken(String)
    case invalidBase64(String)
    case invalidNonceLength(Int)
    case invalidCiphertext
    case invalidSignatureLength(Int)
    case unknownSender
    case badSignature
    case replay

    public var errorDescription: String? {
        switch self {
        case .invalidVersion: return "Unsupported cloud envelope version."
        case .unsafeInteger(let field): return "\(field) is outside the relay's safe integer range."
        case .invalidChannel: return "The cloud envelope channel is invalid."
        case .classChannelMismatch: return "The envelope class does not match its channel."
        case .invalidToken(let field): return "The envelope \(field) is invalid."
        case .invalidBase64(let field): return "The envelope \(field) is not canonical padded base64."
        case .invalidNonceLength(let count): return "The AES-GCM nonce is \(count) bytes; expected 12."
        case .invalidCiphertext: return "The AES-GCM ciphertext is too short to contain its tag."
        case .invalidSignatureLength(let count): return "The Ed25519 signature is \(count) bytes; expected 64."
        case .unknownSender: return "No paired public key exists for the envelope sender."
        case .badSignature: return "The cloud envelope signature is invalid."
        case .replay: return "The sender sequence is not strictly monotonic."
        }
    }
}

public enum CloudEnvelopeClass: String, Codable, CaseIterable, Sendable {
    case stream
    case ctl
    case dispatch
    case ho
}

/// The exact ten-field JSON object accepted by the relay.
///
/// `nonce`, `ct`, and `sig` intentionally retain their canonical, padded standard-base64
/// spelling: the relay signs the UTF-8 text, not a re-encoded byte array.
public struct CloudEnvelope: Codable, Equatable, Sendable {
    public static let version = 1
    public static let nonceByteCount = 12 // D16: AES-256-GCM with a 96-bit nonce.
    public static let tagByteCount = 16
    public static let signatureByteCount = 64
    public static let maximumRelayInteger: UInt64 = 9_007_199_254_740_991

    public let v: Int
    public let ch: String
    public let seq: UInt64
    public let ts: UInt64
    public let envelopeClass: CloudEnvelopeClass
    public let keyID: String
    public let nonce: String
    public let ct: String
    public let sender: String
    public let sig: String

    enum CodingKeys: String, CodingKey, CaseIterable {
        case v, ch, seq, ts
        case envelopeClass = "class"
        case keyID = "key_id"
        case nonce, ct, sender, sig
    }

    public init(from decoder: Decoder) throws {
        let dynamic = try decoder.container(keyedBy: CloudDynamicCodingKey.self)
        let received = Set(dynamic.allKeys.map(\.stringValue))
        let expected = Set(CodingKeys.allCases.map(\.rawValue))
        guard received == expected else {
            throw DecodingError.dataCorrupted(.init(
                codingPath: decoder.codingPath,
                debugDescription: "Envelope must contain exactly: \(expected.sorted().joined(separator: ", "))"
            ))
        }

        let values = try decoder.container(keyedBy: CodingKeys.self)
        let v = try values.decode(Int.self, forKey: .v)
        let ch = try values.decode(String.self, forKey: .ch)
        let seq = try values.decode(UInt64.self, forKey: .seq)
        let ts = try values.decode(UInt64.self, forKey: .ts)
        let envelopeClass = try values.decode(CloudEnvelopeClass.self, forKey: .envelopeClass)
        let keyID = try values.decode(String.self, forKey: .keyID)
        let nonce = try values.decode(String.self, forKey: .nonce)
        let ct = try values.decode(String.self, forKey: .ct)
        let sender = try values.decode(String.self, forKey: .sender)
        let sig = try values.decode(String.self, forKey: .sig)
        try self.init(
            v: v, ch: ch, seq: seq, ts: ts, envelopeClass: envelopeClass,
            keyID: keyID, nonce: nonce, ct: ct, sender: sender, sig: sig
        )
    }

    public func encode(to encoder: Encoder) throws {
        var values = encoder.container(keyedBy: CodingKeys.self)
        try values.encode(v, forKey: .v)
        try values.encode(ch, forKey: .ch)
        try values.encode(seq, forKey: .seq)
        try values.encode(ts, forKey: .ts)
        try values.encode(envelopeClass, forKey: .envelopeClass)
        try values.encode(keyID, forKey: .keyID)
        try values.encode(nonce, forKey: .nonce)
        try values.encode(ct, forKey: .ct)
        try values.encode(sender, forKey: .sender)
        try values.encode(sig, forKey: .sig)
    }

    public init(
        v: Int = CloudEnvelope.version,
        ch: String,
        seq: UInt64,
        ts: UInt64,
        envelopeClass: CloudEnvelopeClass,
        keyID: String,
        nonce: String,
        ct: String,
        sender: String,
        sig: String
    ) throws {
        self.v = v
        self.ch = ch
        self.seq = seq
        self.ts = ts
        self.envelopeClass = envelopeClass
        self.keyID = keyID
        self.nonce = nonce
        self.ct = ct
        self.sender = sender
        self.sig = sig
        try validateWireShape()
    }

    /// `v|ch|seq|ts|class|key_id|nonce|ct`, encoded as UTF-8 exactly as relay/envelope.ts does.
    public var signingString: String {
        [
            String(v), ch, String(seq), String(ts), envelopeClass.rawValue,
            keyID, nonce, ct,
        ].joined(separator: "|")
    }

    public var signingBytes: Data { Data(signingString.utf8) }

    public static func decodeJSON(_ data: Data) throws -> CloudEnvelope {
        try JSONDecoder().decode(CloudEnvelope.self, from: data)
    }

    public func encodeJSON(prettyPrinted: Bool = false) throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = prettyPrinted ? [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes] : [.sortedKeys]
        return try encoder.encode(self)
    }

    /// Seals then signs an envelope. Production callers must omit `nonceForTesting`.
    /// Fixed AES-GCM nonces are exposed only so checked-in protocol vectors are reproducible;
    /// reusing one with the same master secret in production destroys AEAD security.
    public static func seal(
        _ plaintext: Data,
        ch: String,
        seq: UInt64,
        ts: UInt64,
        envelopeClass: CloudEnvelopeClass,
        keyID: String,
        sender: String,
        masterSecret: CloudMasterSecret,
        signingKey: CloudDeviceKeyPair,
        nonceForTesting: Data? = nil
    ) throws -> CloudEnvelope {
        let nonceBytes: Data
        if let supplied = nonceForTesting {
            nonceBytes = supplied
        } else {
            let generated = AES.GCM.Nonce()
            nonceBytes = generated.withUnsafeBytes { Data($0) }
        }
        guard nonceBytes.count == nonceByteCount else {
            throw CloudEnvelopeError.invalidNonceLength(nonceBytes.count)
        }

        let symmetricKey = SymmetricKey(data: masterSecret.rawRepresentation)
        let aesNonce = try AES.GCM.Nonce(data: nonceBytes)
        let sealed = try AES.GCM.seal(plaintext, using: symmetricKey, nonce: aesNonce)
        let ciphertextAndTag = sealed.ciphertext + sealed.tag
        let nonceText = nonceBytes.base64EncodedString()
        let ciphertextText = ciphertextAndTag.base64EncodedString()

        let unsigned = CloudEnvelope.unchecked(
            v: version, ch: ch, seq: seq, ts: ts, envelopeClass: envelopeClass,
            keyID: keyID, nonce: nonceText, ct: ciphertextText, sender: sender, sig: ""
        )
        try unsigned.validateUnsignedWireShape()
        let signature = try signingKey.signature(for: unsigned.signingBytes)
        return try CloudEnvelope(
            ch: ch, seq: seq, ts: ts, envelopeClass: envelopeClass, keyID: keyID,
            nonce: nonceText, ct: ciphertextText, sender: sender,
            sig: signature.base64EncodedString()
        )
    }

    /// Resolving by sender is deliberate: `sender` is not part of the protocol's base string,
    /// so changing it must select another pinned key (or no key), never reuse a caller-supplied key.
    public func verify(using publicKeyForSender: (String) -> Data?) -> Bool {
        do {
            try validateWireShape()
            guard let publicKeyRaw = publicKeyForSender(sender) else { return false }
            let publicKey = try Curve25519.Signing.PublicKey(rawRepresentation: publicKeyRaw)
            let signature = try Self.decodeCanonicalBase64(sig, field: "sig")
            return publicKey.isValidSignature(signature, for: signingBytes)
        } catch {
            return false
        }
    }

    public func open(
        masterSecret: CloudMasterSecret,
        publicKeyForSender: (String) -> Data?
    ) throws -> Data {
        guard publicKeyForSender(sender) != nil else { throw CloudEnvelopeError.unknownSender }
        guard verify(using: publicKeyForSender) else { throw CloudEnvelopeError.badSignature }
        let nonceBytes = try Self.decodeCanonicalBase64(nonce, field: "nonce")
        let ciphertextAndTag = try Self.decodeCanonicalBase64(ct, field: "ct")
        guard ciphertextAndTag.count >= Self.tagByteCount else {
            throw CloudEnvelopeError.invalidCiphertext
        }
        let ciphertext = Data(ciphertextAndTag.dropLast(Self.tagByteCount))
        let tag = Data(ciphertextAndTag.suffix(Self.tagByteCount))
        let box = try AES.GCM.SealedBox(
            nonce: AES.GCM.Nonce(data: nonceBytes), ciphertext: ciphertext, tag: tag
        )
        return try AES.GCM.open(box, using: SymmetricKey(data: masterSecret.rawRepresentation))
    }

    private func validateWireShape() throws {
        try validateUnsignedWireShape()
        let signature = try Self.decodeCanonicalBase64(sig, field: "sig")
        guard signature.count == Self.signatureByteCount else {
            throw CloudEnvelopeError.invalidSignatureLength(signature.count)
        }
    }

    private func validateUnsignedWireShape() throws {
        guard v == Self.version else { throw CloudEnvelopeError.invalidVersion }
        guard seq <= Self.maximumRelayInteger else { throw CloudEnvelopeError.unsafeInteger("seq") }
        guard ts <= Self.maximumRelayInteger else { throw CloudEnvelopeError.unsafeInteger("ts") }
        guard let channelKind = Self.channelKind(ch) else { throw CloudEnvelopeError.invalidChannel }
        guard Self.allowedClasses(for: channelKind).contains(envelopeClass) else {
            throw CloudEnvelopeError.classChannelMismatch
        }
        guard Self.isValidToken(keyID, maximumLength: 64) else {
            throw CloudEnvelopeError.invalidToken("key_id")
        }
        guard Self.isValidToken(sender, maximumLength: 128) else {
            throw CloudEnvelopeError.invalidToken("sender")
        }
        let nonceBytes = try Self.decodeCanonicalBase64(nonce, field: "nonce")
        guard nonceBytes.count == Self.nonceByteCount else {
            throw CloudEnvelopeError.invalidNonceLength(nonceBytes.count)
        }
        let ciphertext = try Self.decodeCanonicalBase64(ct, field: "ct")
        guard !ciphertext.isEmpty else { throw CloudEnvelopeError.invalidCiphertext }
    }

    private enum ChannelKind { case stream, ctl, handoff }

    private static func channelKind(_ channel: String) -> ChannelKind? {
        guard !channel.isEmpty, channel.utf8.count <= 300 else { return nil }
        let parts = channel.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
        guard let prefix = parts.first else { return nil }
        let segments = parts.dropFirst()
        guard segments.allSatisfy({ isValidToken($0, maximumLength: 128) }) else { return nil }
        switch prefix {
        case "s" where parts.count == 3: return .stream
        case "t" where parts.count == 3: return .stream
        case "orch" where parts.count == 2: return .stream
        case "ctl" where parts.count == 2: return .ctl
        case "ho" where parts.count == 3: return .handoff
        default: return nil
        }
    }

    private static func allowedClasses(for channel: ChannelKind) -> Set<CloudEnvelopeClass> {
        switch channel {
        case .stream: return [.stream]
        case .ctl: return [.ctl, .dispatch]
        case .handoff: return [.ho]
        }
    }

    private static func isValidToken(_ value: String, maximumLength: Int) -> Bool {
        guard !value.isEmpty, value.utf8.count <= maximumLength else { return false }
        return value.utf8.allSatisfy { byte in
            byte >= 0x21 && byte <= 0x7e && byte != 0x2f && byte != 0x7c
        }
    }

    private static func decodeCanonicalBase64(_ text: String, field: String) throws -> Data {
        guard text.utf8.count % 4 == 0,
              text.utf8.allSatisfy({
                  ($0 >= 65 && $0 <= 90) || ($0 >= 97 && $0 <= 122) ||
                  ($0 >= 48 && $0 <= 57) || $0 == 43 || $0 == 47 || $0 == 61
              }),
              let bytes = Data(base64Encoded: text, options: []),
              bytes.base64EncodedString() == text
        else { throw CloudEnvelopeError.invalidBase64(field) }
        return bytes
    }

    private static func unchecked(
        v: Int,
        ch: String,
        seq: UInt64,
        ts: UInt64,
        envelopeClass: CloudEnvelopeClass,
        keyID: String,
        nonce: String,
        ct: String,
        sender: String,
        sig: String
    ) -> CloudEnvelope {
        CloudEnvelope(
            uncheckedV: v, ch: ch, seq: seq, ts: ts, envelopeClass: envelopeClass,
            keyID: keyID, nonce: nonce, ct: ct, sender: sender, sig: sig
        )
    }

    private init(
        uncheckedV: Int,
        ch: String,
        seq: UInt64,
        ts: UInt64,
        envelopeClass: CloudEnvelopeClass,
        keyID: String,
        nonce: String,
        ct: String,
        sender: String,
        sig: String
    ) {
        v = uncheckedV
        self.ch = ch
        self.seq = seq
        self.ts = ts
        self.envelopeClass = envelopeClass
        self.keyID = keyID
        self.nonce = nonce
        self.ct = ct
        self.sender = sender
        self.sig = sig
    }
}

/// Per-sender replay protection: the highest sequence accepted from each sender, and which of the
/// `windowSize` positions below it were accepted too (design `cloud-error-transparency.md` §11.8).
///
/// **The one rule: the same `(sender, seq)` is claimed at most once, and anything this cannot
/// decide is refused.** A sequence above the highest is new. One inside the window is new only if
/// its bit is clear. One older than the window cannot be decided — its bit has fallen off — so it
/// is refused exactly as a repeat is. The 300-second envelope deadline and the ledger's request
/// idempotency are separate owners and are not what makes this safe.
///
/// A strictly increasing tracker used to stand here, and it refused the lower-numbered tab
/// whenever one device had two tabs open, because each tab reserves its own block of sequences.
///
/// A claim is final. The caller claims only after authentication and never gives a claim back,
/// including when capacity then refuses the command, so a refused envelope sent again is a replay.
struct CloudSequenceTracker {
    static let windowSize: UInt64 = 1_024
    /// Every tracked sender costs a fixed 16-word window. A sender that would exceed this is refused
    /// rather than tracked by evicting another, because an evicted window can no longer decide.
    static let defaultMaximumSenders = 4_096
    private static let words = Int(windowSize / 64)

    enum Decision: Equatable {
        case accepted
        case replay(highestSequence: UInt64)
        case senderCapacity
    }

    private struct Window {
        var highest: UInt64
        /// Bit `i` is set when `highest - 1 - i` was claimed.
        var seen: [UInt64]
    }

    private var windows: [String: Window] = [:]
    let maximumSenders: Int

    init(maximumSenders: Int = CloudSequenceTracker.defaultMaximumSenders) {
        precondition(maximumSenders > 0)
        self.maximumSenders = maximumSenders
    }

    mutating func claim(sender: String, sequence: UInt64) -> Decision {
        guard var window = windows[sender] else {
            guard windows.count < maximumSenders else { return .senderCapacity }
            windows[sender] = Window(
                highest: sequence, seen: Array(repeating: 0, count: Self.words))
            return .accepted
        }
        if sequence > window.highest {
            let distance = sequence - window.highest
            Self.advance(&window.seen, by: distance)
            window.highest = sequence
            windows[sender] = window
            return .accepted
        }
        let offset = window.highest - sequence
        guard offset >= 1, offset <= Self.windowSize else {
            return .replay(highestSequence: window.highest)
        }
        let bit = offset - 1
        let word = Int(bit / 64)
        let mask = UInt64(1) << (bit % 64)
        guard window.seen[word] & mask == 0 else {
            return .replay(highestSequence: window.highest)
        }
        window.seen[word] |= mask
        windows[sender] = window
        return .accepted
    }

    func highestSequence(for sender: String) -> UInt64? { windows[sender]?.highest }

    var trackedSenderCount: Int { windows.count }

    /// Moves every recorded position `distance` further from the new highest and records the old
    /// highest itself, which sits at offset `distance`. Positions past the window fall off.
    private static func advance(_ seen: inout [UInt64], by distance: UInt64) {
        if distance >= windowSize {
            for index in seen.indices { seen[index] = 0 }
        } else {
            let wordShift = Int(distance / 64)
            let bitShift = distance % 64
            for index in stride(from: words - 1, through: 0, by: -1) {
                let source = index - wordShift
                var value: UInt64 = 0
                if source >= 0 {
                    value = seen[source] << bitShift
                    if bitShift > 0, source - 1 >= 0 {
                        value |= seen[source - 1] >> (64 - bitShift)
                    }
                }
                seen[index] = value
            }
        }
        let oldHighestBit = distance - 1
        guard oldHighestBit < windowSize else { return }
        seen[Int(oldHighestBit / 64)] |= UInt64(1) << (oldHighestBit % 64)
    }
}

/// The process-lifetime owner of inbound replay state. `CloudTransport.production` shares
/// `process` across every transport this process builds, because a bridge rebuilt after sign-out,
/// retry or an identity change would otherwise start from an empty window and accept an
/// envelope the previous transport had already accepted. Internal transports built by tests pass
/// their own instance so unrelated fixtures cannot see each other's sequences.
final class CloudInboundReplayWindow: @unchecked Sendable {
    static let process = CloudInboundReplayWindow()

    private let lock = NSLock()
    private var tracker: CloudSequenceTracker

    init(maximumSenders: Int = CloudSequenceTracker.defaultMaximumSenders) {
        tracker = CloudSequenceTracker(maximumSenders: maximumSenders)
    }

    /// Decides and records in one critical section, so two transports alive during a bridge
    /// replacement cannot both accept the same envelope.
    func claim(sender: String, sequence: UInt64) -> CloudSequenceTracker.Decision {
        lock.lock()
        defer { lock.unlock() }
        return tracker.claim(sender: sender, sequence: sequence)
    }

    func highestSequence(for sender: String) -> UInt64? {
        lock.lock()
        defer { lock.unlock() }
        return tracker.highestSequence(for: sender)
    }
}

private struct CloudDynamicCodingKey: CodingKey {
    let stringValue: String
    let intValue: Int? = nil
    init?(stringValue: String) { self.stringValue = stringValue }
    init?(intValue: Int) { return nil }
}
