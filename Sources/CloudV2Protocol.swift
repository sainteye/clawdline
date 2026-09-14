#if canImport(CryptoKit)
import CryptoKit
#elseif canImport(Crypto)
import Crypto
#else
#error("Cloud v2 protocol requires CryptoKit or swift-crypto")
#endif
import Foundation

/// Policy-independent wire and verification primitives for the greenfield Cloud v2 storage
/// contract. This unit deliberately owns no persistence, retention tier, relay upload, or key
/// wrapping. A caller must inject replication policy, and the default policy denies replication.
public enum CloudV2StoredObjectPlane: String, CaseIterable, Equatable, Sendable {
    case status
    case transcriptSegment = "transcript_segment"
    case transcriptHead = "transcript_head"
    case transcriptTail = "transcript_tail"
    case blob
    case manifest
    case machineLiveness = "machine_liveness"
    case commandRequest = "command_request"
    case commandReceipt = "command_receipt"
}

public enum CloudV2ProtocolError: Error, Equatable, Sendable {
    case invalidSchemaVersion(Int64)
    case invalidField(String)
    case invalidDigestLength(field: String, count: Int)
    case ciphertextDigestMismatch
    case invalidSignature
    case aeadAuthenticationFailed
    case unauthorizedSigner
    case unexpectedStream
    case keyEpochConflict
    case commandDeadlineInvalid
    case commandExpired
    case dedupeHorizonUnsafe
    case repairableGap
    case rollbackDetected
    case revisionRegressed
    case writerEpochConflict
    case streamFork

    public var code: String {
        switch self {
        case .invalidSchemaVersion: return "unsupported_schema"
        case .invalidField: return "invalid_field"
        case .invalidDigestLength: return "invalid_digest"
        case .ciphertextDigestMismatch: return "ciphertext_digest_mismatch"
        case .invalidSignature: return "invalid_signature"
        case .aeadAuthenticationFailed: return "aead_authentication_failed"
        case .unauthorizedSigner: return "unauthorized_signer"
        case .unexpectedStream: return "unexpected_stream"
        case .keyEpochConflict: return "key_epoch_conflict"
        case .commandDeadlineInvalid: return "command_deadline_invalid"
        case .commandExpired: return "command_expired"
        case .dedupeHorizonUnsafe: return "dedupe_horizon_unsafe"
        case .repairableGap: return "repairable_gap"
        case .rollbackDetected: return "rollback_detected"
        case .revisionRegressed: return "revision_regressed"
        case .writerEpochConflict: return "writer_epoch_conflict"
        case .streamFork: return "stream_fork"
        }
    }
}

public struct CloudV2StreamIdentity: Equatable, Hashable, Sendable {
    public let account: String
    public let machine: String
    public let plane: CloudV2StoredObjectPlane
    public let streamKey: String

    public init(account: String, machine: String, plane: CloudV2StoredObjectPlane,
                streamKey: String) {
        self.account = account
        self.machine = machine
        self.plane = plane
        self.streamKey = streamKey
    }

    public init(_ header: CloudV2StoredObjectHeader) {
        self.init(account: header.account, machine: header.machine,
                  plane: header.plane, streamKey: header.streamKey)
    }
}

/// The full verified position a command observed. A bare revision is deliberately insufficient:
/// a restore may open a new writer epoch and eventually reuse the same numeric revision.
public struct CloudV2ObservedPosition: Equatable, Sendable {
    public let stream: CloudV2StreamIdentity
    public let writerEpoch: Int64
    public let writerGeneration: Int64
    public let projectorVersion: Int64
    public let lastRevision: Int64
    public let ciphertextDigest: Data

    public init(accepting header: CloudV2StoredObjectHeader) {
        stream = CloudV2StreamIdentity(header)
        writerEpoch = header.writerEpoch
        writerGeneration = header.writerGeneration
        projectorVersion = header.projectorVersion
        lastRevision = header.lastRevision
        ciphertextDigest = header.ciphertextDigest
    }

    fileprivate var cloudJSONValue: CloudJSONValue {
        .object([
            "account": .string(stream.account), "machine": .string(stream.machine),
            "plane": .string(stream.plane.rawValue), "stream_key": .string(stream.streamKey),
            "writer_epoch": .int(writerEpoch), "writer_generation": .int(writerGeneration),
            "projector_version": .int(projectorVersion), "last_revision": .int(lastRevision),
            "ciphertext_digest": .base64(ciphertextDigest),
        ])
    }
}

/// Signed command identity shared by request and receipt records. The stream key is derived from
/// the sender and idempotency key, so Cloud cannot splice one device's receipt onto another job.
public struct CloudV2CommandBinding: Equatable, Sendable {
    public let senderDeviceID: String
    public let idempotencyKey: String
    /// Opaque per-Session stream identity. This is not a local Session id or display label.
    public let targetStreamKey: String
    public let commandType: String
    public let notAfter: Int64
    public let precondition: CloudV2ObservedPosition?

    public init(senderDeviceID: String, idempotencyKey: String, targetStreamKey: String,
                commandType: String, notAfter: Int64,
                precondition: CloudV2ObservedPosition?) {
        self.senderDeviceID = senderDeviceID
        self.idempotencyKey = idempotencyKey
        self.targetStreamKey = targetStreamKey
        self.commandType = commandType
        self.notAfter = notAfter
        self.precondition = precondition
    }

    public var streamKey: String {
        let value: CloudJSONValue = .object([
            "device_id": .string(senderDeviceID),
            "idempotency_key": .string(idempotencyKey),
        ])
        let bytes = CloudCanonicalJSON.canonicalData(value)
        return Data(SHA256.hash(data: bytes)).base64EncodedString()
    }

    fileprivate var cloudJSONValue: CloudJSONValue {
        .object([
            "sender_device_id": .string(senderDeviceID),
            "idempotency_key": .string(idempotencyKey),
            "target_stream_key": .string(targetStreamKey),
            "command_type": .string(commandType),
            "not_after": .int(notAfter),
            "precondition": precondition?.cloudJSONValue ?? .null,
        ])
    }
}

public struct CloudV2CommandLifetimePolicy: Equatable, Sendable {
    public let maximumJobLifeSeconds: Int64
    public let maximumClockSkewSeconds: Int64
    public let dedupeRetentionSeconds: Int64

    public init(maximumJobLifeSeconds: Int64, maximumClockSkewSeconds: Int64,
                dedupeRetentionSeconds: Int64) throws {
        let requiredRetention = maximumJobLifeSeconds.addingReportingOverflow(
            maximumClockSkewSeconds)
        guard maximumJobLifeSeconds > 0, maximumClockSkewSeconds >= 0,
              !requiredRetention.overflow,
              dedupeRetentionSeconds >= requiredRetention.partialValue else {
            throw CloudV2ProtocolError.dedupeHorizonUnsafe
        }
        self.maximumJobLifeSeconds = maximumJobLifeSeconds
        self.maximumClockSkewSeconds = maximumClockSkewSeconds
        self.dedupeRetentionSeconds = dedupeRetentionSeconds
    }

    public func validate(notAfter: Int64, guardedNow: Int64) throws {
        guard notAfter >= guardedNow else {
            throw CloudV2ProtocolError.commandExpired
        }
        let horizon = guardedNow.addingReportingOverflow(maximumJobLifeSeconds)
        guard !horizon.overflow, notAfter <= horizon.partialValue else {
            throw CloudV2ProtocolError.commandDeadlineInvalid
        }
    }
}

/// Availability is not represented as an empty successful result. These are the complete gaps
/// introduced by the first v2 storage contract slice; product policy may decide when replication
/// is enabled, but it may not invent an untyped fallback.
public enum CloudV2AvailabilityGap: String, CaseIterable, Error, Equatable, Sendable {
    case machineOffline = "machine_offline"
    case notReplicated = "not_replicated"
    case retentionExpired = "retention_expired"
    case cursorExpired = "cursor_expired"
    case replicationQuotaExhausted = "replication_quota_exhausted"
}

public struct CloudV2ReplicationPolicy: Sendable {
    public typealias Decision = @Sendable (
        _ plane: CloudV2StoredObjectPlane, _ streamKey: String
    ) -> Bool

    private let decision: Decision

    public init(decision: @escaping Decision) {
        self.decision = decision
    }

    /// B3 remains a product decision. The protocol foundation cannot opt content into Cloud
    /// storage merely because a caller forgot to inject policy.
    public static let disabled = CloudV2ReplicationPolicy { _, _ in false }

    public func requireReplication(
        plane: CloudV2StoredObjectPlane, streamKey: String
    ) throws {
        guard decision(plane, streamKey) else { throw CloudV2AvailabilityGap.notReplicated }
    }
}

/// The canonical header is both the AEAD associated data and the signed identity/order body.
/// Digests are raw SHA-256 bytes in Swift and canonical padded base64 strings on the JSON wire.
public struct CloudV2StoredObjectHeader: Equatable, Sendable {
    public static let currentSchemaVersion: Int64 = 2
    public static let digestByteCount = 32
    public static let generatedTimeQuantumSeconds: Int64 = 60
    public static let signingDomain = "clawdline-cloud-stored-object-v2"

    public let schemaVersion: Int64
    public let account: String
    public let machine: String
    public let signerDeviceID: String
    public let signingKeyID: String
    public let plane: CloudV2StoredObjectPlane
    public let streamKey: String
    public let writerEpoch: Int64
    public let writerGeneration: Int64
    public let projectorVersion: Int64
    public let firstRevision: Int64
    public let lastRevision: Int64
    public let previousDigest: Data?
    public let keyEpoch: Int64
    public let coarseGeneratedAt: Int64
    public let ciphertextDigest: Data
    public let commandBinding: CloudV2CommandBinding?

    public init(
        schemaVersion: Int64 = CloudV2StoredObjectHeader.currentSchemaVersion,
        account: String,
        machine: String,
        signerDeviceID: String,
        signingKeyID: String,
        plane: CloudV2StoredObjectPlane,
        streamKey: String,
        writerEpoch: Int64,
        writerGeneration: Int64,
        projectorVersion: Int64,
        firstRevision: Int64,
        lastRevision: Int64,
        previousDigest: Data?,
        keyEpoch: Int64,
        coarseGeneratedAt: Int64,
        ciphertextDigest: Data,
        commandBinding: CloudV2CommandBinding? = nil
    ) throws {
        self.schemaVersion = schemaVersion
        self.account = account
        self.machine = machine
        self.signerDeviceID = signerDeviceID
        self.signingKeyID = signingKeyID
        self.plane = plane
        self.streamKey = streamKey
        self.writerEpoch = writerEpoch
        self.writerGeneration = writerGeneration
        self.projectorVersion = projectorVersion
        self.firstRevision = firstRevision
        self.lastRevision = lastRevision
        self.previousDigest = previousDigest
        self.keyEpoch = keyEpoch
        self.coarseGeneratedAt = coarseGeneratedAt
        self.ciphertextDigest = ciphertextDigest
        self.commandBinding = commandBinding
        try validateWireShape()
    }

    public static func digest(of ciphertext: Data) -> Data {
        Data(SHA256.hash(data: ciphertext))
    }

    public var cloudJSONValue: CloudJSONValue {
        .object([
            "schema_version": .int(schemaVersion),
            "account": .string(account),
            "machine": .string(machine),
            "signer_device_id": .string(signerDeviceID),
            "signing_key_id": .string(signingKeyID),
            "plane": .string(plane.rawValue),
            "stream_key": .string(streamKey),
            "writer_epoch": .int(writerEpoch),
            "writer_generation": .int(writerGeneration),
            "projector_version": .int(projectorVersion),
            "first_revision": .int(firstRevision),
            "last_revision": .int(lastRevision),
            "previous_digest": previousDigest.map(CloudJSONValue.base64) ?? .null,
            "key_epoch": .int(keyEpoch),
            "coarse_generated_at": .int(coarseGeneratedAt),
            "ciphertext_digest": .base64(ciphertextDigest),
            "command": commandBinding?.cloudJSONValue ?? .null,
        ])
    }

    /// RFC 8785 bytes supplied as AEAD associated data. This method throws rather than calling
    /// only `canonicalData` so every numeric field is checked against the shared JS safe domain.
    public func canonicalAAD() throws -> Data {
        try validateWireShape()
        try CloudCanonicalJSON.validateNumberDomain(cloudJSONValue)
        return CloudCanonicalJSON.canonicalData(cloudJSONValue)
    }

    public func canonicalSigningBytes() throws -> Data {
        try validateWireShape()
        return try CloudCanonicalJSON.signingInput(
            domain: Self.signingDomain, object: cloudJSONValue)
    }

    public func validate() throws {
        guard schemaVersion == Self.currentSchemaVersion else {
            throw CloudV2ProtocolError.invalidSchemaVersion(schemaVersion)
        }
        try validateWireShape()
    }

    private func validateWireShape() throws {
        guard schemaVersion > 0 else {
            throw CloudV2ProtocolError.invalidField("schema_version")
        }
        try Self.requireOpaqueToken(account, field: "account")
        try Self.requireOpaqueToken(machine, field: "machine")
        try Self.requireOpaqueToken(signerDeviceID, field: "signer_device_id")
        try Self.requireOpaqueToken(signingKeyID, field: "signing_key_id")
        try Self.requireOpaqueToken(streamKey, field: "stream_key")
        guard writerEpoch > 0 else { throw CloudV2ProtocolError.invalidField("writer_epoch") }
        guard writerGeneration > 0 else {
            throw CloudV2ProtocolError.invalidField("writer_generation")
        }
        guard projectorVersion > 0 else {
            throw CloudV2ProtocolError.invalidField("projector_version")
        }
        guard firstRevision >= 0, lastRevision >= firstRevision else {
            throw CloudV2ProtocolError.invalidField("revision_range")
        }
        guard keyEpoch > 0 else { throw CloudV2ProtocolError.invalidField("key_epoch") }
        guard coarseGeneratedAt >= 0,
              coarseGeneratedAt % Self.generatedTimeQuantumSeconds == 0 else {
            throw CloudV2ProtocolError.invalidField("coarse_generated_at")
        }
        if let previousDigest, previousDigest.count != Self.digestByteCount {
            throw CloudV2ProtocolError.invalidDigestLength(
                field: "previous_digest", count: previousDigest.count)
        }
        guard ciphertextDigest.count == Self.digestByteCount else {
            throw CloudV2ProtocolError.invalidDigestLength(
                field: "ciphertext_digest", count: ciphertextDigest.count)
        }
        let isCommandPlane = plane == .commandRequest || plane == .commandReceipt
        guard isCommandPlane == (commandBinding != nil) else {
            throw CloudV2ProtocolError.invalidField("command")
        }
        if let commandBinding {
            try Self.requireOpaqueToken(commandBinding.senderDeviceID, field: "sender_device_id")
            try Self.requireOpaqueToken(commandBinding.idempotencyKey, field: "idempotency_key")
            try Self.requireOpaqueToken(
                commandBinding.targetStreamKey, field: "target_stream_key")
            try Self.requireOpaqueToken(commandBinding.commandType, field: "command_type")
            guard commandBinding.notAfter >= 0,
                  commandBinding.notAfter % Self.generatedTimeQuantumSeconds == 0 else {
                throw CloudV2ProtocolError.invalidField("not_after")
            }
            guard streamKey == commandBinding.streamKey else {
                throw CloudV2ProtocolError.invalidField("command_stream_key")
            }
            if plane == .commandRequest, signerDeviceID != commandBinding.senderDeviceID {
                throw CloudV2ProtocolError.invalidField("command_sender")
            }
            if let position = commandBinding.precondition,
               position.ciphertextDigest.count != Self.digestByteCount {
                throw CloudV2ProtocolError.invalidDigestLength(
                    field: "precondition.ciphertext_digest",
                    count: position.ciphertextDigest.count)
            }
            if let position = commandBinding.precondition {
                try Self.requireOpaqueToken(position.stream.account,
                                            field: "precondition.account")
                try Self.requireOpaqueToken(position.stream.machine,
                                            field: "precondition.machine")
                try Self.requireOpaqueToken(position.stream.streamKey,
                                            field: "precondition.stream_key")
                guard position.writerEpoch > 0, position.writerGeneration > 0,
                      position.projectorVersion > 0, position.lastRevision >= 0 else {
                    throw CloudV2ProtocolError.invalidField("precondition.position")
                }
            }
        }
        try CloudCanonicalJSON.validateNumberDomain(cloudJSONValue)
    }

    private static func requireOpaqueToken(_ value: String, field: String) throws {
        guard !value.isEmpty, value.utf8.count <= 256,
              value.unicodeScalars.allSatisfy({ $0.value >= 0x20 && $0.value <= 0x7e }) else {
            throw CloudV2ProtocolError.invalidField(field)
        }
    }
}

public typealias CloudV2AuthorizedWriterKey = (
    _ stream: CloudV2StreamIdentity, _ signerDeviceID: String, _ signingKeyID: String
) -> Data?

/// The authenticated encrypted body. `ciphertextDigest` covers `ciphertext`; the AEAD tag binds
/// that body to the exact canonical header, and the outer Ed25519 signature binds both for readers
/// that do not yet possess the plane key.
public struct CloudV2AEADPayload: Equatable, Sendable {
    public let nonce: Data
    public let ciphertext: Data
    public let tag: Data

    public init(nonce: Data, ciphertext: Data, tag: Data) {
        self.nonce = nonce
        self.ciphertext = ciphertext
        self.tag = tag
    }

    public static func seal(
        plaintext: Data,
        key: Data,
        makeHeader: (_ ciphertext: Data) throws -> CloudV2StoredObjectHeader
    ) throws -> (header: CloudV2StoredObjectHeader, payload: CloudV2AEADPayload) {
        let symmetricKey = SymmetricKey(data: key)
        let nonce = AES.GCM.Nonce()
        // GCM's encrypted body is independent of AAD. The first pass obtains those bytes so their
        // digest can enter the header; the second pass fixes the authentication tag to that header.
        // The preliminary tag is intentionally never returned or persisted.
        let preliminary = try AES.GCM.seal(plaintext, using: symmetricKey, nonce: nonce)
        let header = try makeHeader(preliminary.ciphertext)
        let sealed = try AES.GCM.seal(
            plaintext, using: symmetricKey, nonce: nonce,
            authenticating: header.canonicalAAD())
        guard sealed.ciphertext == preliminary.ciphertext else {
            throw CloudV2ProtocolError.ciphertextDigestMismatch
        }
        return (header, CloudV2AEADPayload(
            nonce: Data(nonce), ciphertext: sealed.ciphertext, tag: sealed.tag))
    }

    public func open(header: CloudV2StoredObjectHeader, key: Data) throws -> Data {
        guard header.ciphertextDigest == CloudV2StoredObjectHeader.digest(of: ciphertext) else {
            throw CloudV2ProtocolError.ciphertextDigestMismatch
        }
        do {
            let sealed = try AES.GCM.SealedBox(
                nonce: AES.GCM.Nonce(data: nonce), ciphertext: ciphertext, tag: tag)
            return try AES.GCM.open(
                sealed, using: SymmetricKey(data: key),
                authenticating: header.canonicalAAD())
        } catch let error as CloudV2ProtocolError {
            throw error
        } catch {
            throw CloudV2ProtocolError.aeadAuthenticationFailed
        }
    }
}

public struct CloudV2SignedStoredObject: Equatable, Sendable {
    public let header: CloudV2StoredObjectHeader
    public let signature: Data

    public init(header: CloudV2StoredObjectHeader, signature: Data) {
        self.header = header
        self.signature = signature
    }

    public static func sign(
        header: CloudV2StoredObjectHeader,
        ciphertext: Data,
        using keyPair: CloudDeviceKeyPair,
        policy: CloudV2ReplicationPolicy
    ) throws -> CloudV2SignedStoredObject {
        try policy.requireReplication(plane: header.plane, streamKey: header.streamKey)
        try header.validate()
        guard header.ciphertextDigest == CloudV2StoredObjectHeader.digest(of: ciphertext) else {
            throw CloudV2ProtocolError.ciphertextDigestMismatch
        }
        let signature = try keyPair.signature(for: header.canonicalSigningBytes())
        return CloudV2SignedStoredObject(header: header, signature: signature)
    }

    public func verify(
        ciphertext: Data,
        expected: CloudV2StreamIdentity,
        authorizedWriterKey: CloudV2AuthorizedWriterKey
    ) throws {
        guard header.ciphertextDigest == CloudV2StoredObjectHeader.digest(of: ciphertext) else {
            throw CloudV2ProtocolError.ciphertextDigestMismatch
        }
        try verifySignature(expected: expected, authorizedWriterKey: authorizedWriterKey)
    }

    public func verifySignature(
        expected: CloudV2StreamIdentity,
        authorizedWriterKey: CloudV2AuthorizedWriterKey
    ) throws {
        do {
            let actual = CloudV2StreamIdentity(header)
            guard actual == expected else {
                throw CloudV2ProtocolError.unexpectedStream
            }
            guard let publicKeyRaw = authorizedWriterKey(
                actual, header.signerDeviceID, header.signingKeyID
            ) else {
                throw CloudV2ProtocolError.unauthorizedSigner
            }
            let publicKey = try Curve25519.Signing.PublicKey(rawRepresentation: publicKeyRaw)
            guard publicKey.isValidSignature(signature, for: try header.canonicalSigningBytes()) else {
                throw CloudV2ProtocolError.invalidSignature
            }
            try header.validate()
        } catch let error as CloudV2ProtocolError {
            throw error
        } catch {
            throw CloudV2ProtocolError.invalidSignature
        }
    }
}

/// A CAS result is scoped to one stream and fences both restored local state and simultaneous
/// writers. The grant echoes the key epoch, but the executor must compare it with its locally
/// pinned key ring before encrypting; Cloud never chooses which retained key protects a write.
public struct CloudV2WriterGrant: Equatable, Sendable {
    public let account: String
    public let machine: String
    public let plane: CloudV2StoredObjectPlane
    public let streamKey: String
    public let writerEpoch: Int64
    public let writerGeneration: Int64
    public let projectorVersion: Int64
    public let keyEpoch: Int64

    public init(
        account: String, machine: String, plane: CloudV2StoredObjectPlane, streamKey: String,
        writerEpoch: Int64, writerGeneration: Int64, projectorVersion: Int64, keyEpoch: Int64
    ) {
        self.account = account
        self.machine = machine
        self.plane = plane
        self.streamKey = streamKey
        self.writerEpoch = writerEpoch
        self.writerGeneration = writerGeneration
        self.projectorVersion = projectorVersion
        self.keyEpoch = keyEpoch
    }

    fileprivate func matches(_ header: CloudV2StoredObjectHeader) -> Bool {
        account == header.account && machine == header.machine && plane == header.plane
            && streamKey == header.streamKey && writerEpoch == header.writerEpoch
            && writerGeneration == header.writerGeneration
            && projectorVersion == header.projectorVersion && keyEpoch == header.keyEpoch
    }

    /// Call immediately after accepting a Cloud grant and before constructing encrypted bytes.
    /// The locally pinned key ring, never Cloud, selects the epoch used for a write.
    public func validatePinnedKeyEpoch(_ pinnedKeyEpoch: Int64) throws {
        guard keyEpoch == pinnedKeyEpoch else {
            throw CloudV2ProtocolError.keyEpochConflict
        }
    }
}

public struct CloudV2StreamHighWater: Equatable, Sendable {
    public let account: String
    public let machine: String
    public let plane: CloudV2StoredObjectPlane
    public let streamKey: String
    public let writerEpoch: Int64
    public let writerGeneration: Int64
    public let projectorVersion: Int64
    public let keyEpoch: Int64
    public let lastRevision: Int64
    public let digest: Data

    public init(accepting header: CloudV2StoredObjectHeader) {
        account = header.account
        machine = header.machine
        plane = header.plane
        streamKey = header.streamKey
        writerEpoch = header.writerEpoch
        writerGeneration = header.writerGeneration
        projectorVersion = header.projectorVersion
        keyEpoch = header.keyEpoch
        lastRevision = header.lastRevision
        digest = header.ciphertextDigest
    }
}

public enum CloudV2HighWaterTransition: Equatable, Sendable {
    case initial(CloudV2StreamHighWater)
    case advanced(CloudV2StreamHighWater)
    case idempotent(CloudV2StreamHighWater)
}

public enum CloudV2HighWaterValidator {
    public static func validateWrite(
        current: CloudV2StreamHighWater?,
        incoming: CloudV2StoredObjectHeader,
        grant: CloudV2WriterGrant?,
        pinnedKeyEpoch: Int64
    ) throws -> CloudV2HighWaterTransition {
        try incoming.validate()
        guard let grant else {
            throw CloudV2ProtocolError.writerEpochConflict
        }
        try grant.validatePinnedKeyEpoch(pinnedKeyEpoch)
        guard incoming.keyEpoch == pinnedKeyEpoch else {
            throw CloudV2ProtocolError.keyEpochConflict
        }
        guard grant.matches(incoming) else {
            throw CloudV2ProtocolError.writerEpochConflict
        }
        return try validateWriterAdvance(current: current, incoming: incoming)
    }

    public static func validateRead(
        current: CloudV2StreamHighWater?,
        incoming: CloudV2StoredObjectHeader,
        expected: CloudV2StreamIdentity,
        minimumKeyEpoch: Int64
    ) throws -> CloudV2HighWaterTransition {
        try incoming.validate()
        guard CloudV2StreamIdentity(incoming) == expected else {
            throw CloudV2ProtocolError.unexpectedStream
        }
        guard incoming.keyEpoch >= minimumKeyEpoch else {
            throw CloudV2ProtocolError.rollbackDetected
        }
        return try validateReaderAdvance(current: current, incoming: incoming)
    }

    private static func validateWriterAdvance(
        current: CloudV2StreamHighWater?, incoming: CloudV2StoredObjectHeader
    ) throws -> CloudV2HighWaterTransition {
        let next = CloudV2StreamHighWater(accepting: incoming)
        guard let current else { return .initial(next) }

        guard current.account == incoming.account, current.machine == incoming.machine,
              current.plane == incoming.plane, current.streamKey == incoming.streamKey else {
            throw CloudV2ProtocolError.streamFork
        }
        if incoming.keyEpoch < current.keyEpoch {
            throw CloudV2ProtocolError.rollbackDetected
        }
        if incoming.writerEpoch < current.writerEpoch {
            throw CloudV2ProtocolError.rollbackDetected
        }
        if incoming.writerEpoch > current.writerEpoch {
            guard incoming.writerGeneration > current.writerGeneration,
                  incoming.previousDigest == current.digest else {
                throw CloudV2ProtocolError.writerEpochConflict
            }
            return .advanced(next)
        }

        guard incoming.writerGeneration == current.writerGeneration else {
            throw CloudV2ProtocolError.writerEpochConflict
        }
        guard incoming.projectorVersion == current.projectorVersion else {
            if incoming.projectorVersion < current.projectorVersion {
                throw CloudV2ProtocolError.rollbackDetected
            }
            // An incompatible projector must open a new CAS writer/stream epoch; otherwise a
            // revision can mean different plaintext under the same signed stream identity.
            throw CloudV2ProtocolError.streamFork
        }
        if incoming.lastRevision < current.lastRevision {
            throw CloudV2ProtocolError.revisionRegressed
        }
        if incoming.lastRevision == current.lastRevision {
            guard incoming.ciphertextDigest == current.digest else {
                throw CloudV2ProtocolError.streamFork
            }
            return .idempotent(current)
        }
        let revisionsAdvanceWithoutOverlap = incoming.firstRevision > current.lastRevision
        let revisionRangeIsValid: Bool
        if incoming.plane == .transcriptSegment || incoming.plane == .commandReceipt {
            revisionRangeIsValid = incoming.firstRevision == current.lastRevision + 1
        } else {
            // Latest-value status/head/tail records may coalesce intermediate revisions. Their
            // signed previous digest still binds the new checkpoint to the verified predecessor.
            revisionRangeIsValid = revisionsAdvanceWithoutOverlap
        }
        guard revisionRangeIsValid,
              incoming.previousDigest == current.digest else {
            throw CloudV2ProtocolError.streamFork
        }
        return .advanced(next)
    }

    /// Readers verify monotonic checkpoints, not writer CAS ancestry. A latest-value reader may
    /// legitimately miss one or more published predecessors; immutable chained planes surface a
    /// typed repairable gap so the caller can fetch the missing segment instead of poisoning its
    /// stream as a permanent fork.
    private static func validateReaderAdvance(
        current: CloudV2StreamHighWater?, incoming: CloudV2StoredObjectHeader
    ) throws -> CloudV2HighWaterTransition {
        let next = CloudV2StreamHighWater(accepting: incoming)
        guard let current else { return .initial(next) }

        guard current.account == incoming.account, current.machine == incoming.machine,
              current.plane == incoming.plane, current.streamKey == incoming.streamKey else {
            throw CloudV2ProtocolError.streamFork
        }
        if incoming.keyEpoch < current.keyEpoch || incoming.writerEpoch < current.writerEpoch {
            throw CloudV2ProtocolError.rollbackDetected
        }
        if incoming.writerEpoch > current.writerEpoch {
            guard incoming.writerGeneration > current.writerGeneration else {
                throw CloudV2ProtocolError.writerEpochConflict
            }
            return .advanced(next)
        }

        guard incoming.writerGeneration == current.writerGeneration else {
            throw CloudV2ProtocolError.writerEpochConflict
        }
        guard incoming.projectorVersion == current.projectorVersion else {
            if incoming.projectorVersion < current.projectorVersion {
                throw CloudV2ProtocolError.rollbackDetected
            }
            throw CloudV2ProtocolError.streamFork
        }
        if incoming.lastRevision < current.lastRevision {
            throw CloudV2ProtocolError.revisionRegressed
        }
        if incoming.lastRevision == current.lastRevision {
            guard incoming.ciphertextDigest == current.digest else {
                throw CloudV2ProtocolError.streamFork
            }
            return .idempotent(current)
        }

        if incoming.plane == .transcriptSegment || incoming.plane == .commandReceipt {
            if incoming.firstRevision > current.lastRevision + 1 {
                throw CloudV2ProtocolError.repairableGap
            }
            guard incoming.firstRevision == current.lastRevision + 1 else {
                throw CloudV2ProtocolError.streamFork
            }
        } else {
            guard incoming.firstRevision > current.lastRevision else {
                throw CloudV2ProtocolError.streamFork
            }
        }
        return .advanced(next)
    }
}

public enum CloudV2ReadClassification: String, CaseIterable, Equatable, Sendable {
    case replicatedStatus = "replicated_status"
    case replicatedTranscript = "replicated_transcript"
    case replicatedBlob = "replicated_blob"
    case liveQuery = "live_query"
    case command
    case unsupported
}

public enum CloudV2ReadType: String, CaseIterable, Equatable, Sendable {
    case transcript
    case info
    case agent
    case shell
    case skills
    case git
    case image
    case documents
    case document
    case screen
    case board
    case timeline
    case places
    case projectWorktrees = "project-worktrees"
    case pastSessions = "past-sessions"
    case schedules
    case snippets
    case schedule
    case pushKey = "push-key"
    case projectWorktreeLifecycle = "project-worktree-lifecycle"
    case projectWorktreeLifecycleRefresh = "project-worktree-lifecycle-refresh"
}

public struct CloudV2ReadCatalogEntry: Equatable, Sendable {
    public let type: CloudV2ReadType
    public let classification: CloudV2ReadClassification
}

/// One exhaustive switch is the classification authority. `CloudAppBridge.readTypes` derives
/// from `entries`, so a newly admitted bridge read cannot exist without a deliberate enum case and
/// classification. The mutating lifecycle refresh remains a compatibility spelling, but is a
/// command rather than an effect-free live query.
public enum CloudV2ReadCatalog {
    public static let entries: [CloudV2ReadCatalogEntry] = CloudV2ReadType.allCases.map {
        CloudV2ReadCatalogEntry(type: $0, classification: classification(of: $0))
    }

    public static let readTypeNames: Set<String> = Set(entries.map { $0.type.rawValue })

    public static func classification(
        for rawValue: String
    ) -> CloudV2ReadClassification? {
        CloudV2ReadType(rawValue: rawValue).map { classification(of: $0) }
    }

    public static func requiresWriteGate(for rawValue: String) -> Bool {
        classification(for: rawValue) == .command
    }

    public static func classification(
        of type: CloudV2ReadType
    ) -> CloudV2ReadClassification {
        switch type {
        case .info:
            return .replicatedStatus
        case .transcript:
            return .replicatedTranscript
        case .image:
            return .replicatedBlob
        case .projectWorktreeLifecycleRefresh:
            return .command
        case .agent, .shell, .skills, .git, .documents, .document, .screen, .board,
             .timeline, .places, .projectWorktrees, .pastSessions, .schedules, .snippets,
             .schedule, .pushKey, .projectWorktreeLifecycle:
            return .liveQuery
        }
    }
}
