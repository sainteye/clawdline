// clawdline.com control-plane client. This file owns device-code login and the
// long-lived machine credential; CloudTransport owns short-lived relay tokens.

import Foundation
import CoreFoundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

public enum CloudAccountError: Error, LocalizedError, Equatable {
    case missingMachineCredential
    case invalidResponse
    case http(status: Int, code: String?)
    case invalidPairingBlob
    case loginAbandoned
    case credentialInvalidationFailed(String)
    case credentialCleanupPending(String)

    public var errorDescription: String? {
        switch self {
        case .missingMachineCredential:
            return "This Mac is not signed in to Clawdline Cloud."
        case .invalidResponse:
            return "The Clawdline Cloud API returned an invalid response."
        case .http(let status, let code):
            let suffix = code.map { " (\($0))" } ?? ""
            return "The Clawdline Cloud API returned HTTP \(status)\(suffix)."
        case .invalidPairingBlob:
            return "The pairing handover is not valid opaque base64 data."
        case .loginAbandoned:
            return "This Mac was signed out while that sign-in was still finishing, "
                + "so the new credential was discarded."
        case .credentialInvalidationFailed(let reason):
            return "This Clawdline process stopped using the credential, but could not durably "
                + "mark it invalid (\(reason)). A restart could admit it again; retry "
                + "invalidation before restarting."
        case .credentialCleanupPending(let reason):
            return "The credential is durably invalid, but its stale Keychain item could not "
                + "be removed (\(reason)). Retry cleanup; it cannot be used after restart."
        }
    }
}

/// Which sign-in a credential belongs to.
///
/// It exists because cancelling a `Task` is a request rather than a guarantee: a transport that
/// ignores cancellation still returns a real credential, and by then the person may have signed
/// out. Comparing this value inside the persistence transaction is what refuses that write —
/// a check anywhere above the store cannot, because the write is already on its way.
public struct CloudCredentialGeneration: Equatable, Sendable {
    fileprivate let value: UInt64

    public init(_ value: UInt64) { self.value = value }
}

/// Durable, nonsecret invalidation state. A Keychain item whose embedded epoch is older than
/// this value is unusable even when `SecItemDelete` failed, including after process restart.
public protocol CloudCredentialInvalidationStoring: Sendable {
    /// Raise the process-local floor synchronously. This is atomic-only and must not touch a
    /// persistence API: Settings calls it before cancelling the in-flight login Task.
    func reserve(atLeast epoch: UInt64) -> UInt64
    func currentEpoch() throws -> UInt64
    func advance(to epoch: UInt64) throws
}

public final class CloudInMemoryCredentialInvalidationStore:
    CloudCredentialInvalidationStoring, @unchecked Sendable
{
    private let epoch: CloudLocked<UInt64>

    public init(epoch: UInt64 = 0) {
        self.epoch = CloudLocked(epoch)
    }

    public func reserve(atLeast wanted: UInt64) -> UInt64 {
        epoch.withLock { current in
            current = max(current, wanted)
            return current
        }
    }

    public func currentEpoch() throws -> UInt64 { epoch.withLock { $0 } }

    public func advance(to wanted: UInt64) throws {
        epoch.withLock { current in current = max(current, wanted) }
    }
}

/// Production persistence for the nonsecret epoch. UserDefaults is intentionally separate from
/// the Keychain: an unavailable or locked Keychain must not be able to roll invalidation back.
public final class CloudCredentialInvalidationDefaultsStore:
    CloudCredentialInvalidationStoring, @unchecked Sendable
{
    public static let defaultKey = "cloud.machine-credential.minimum-valid-epoch"

    private final class ProcessCoordinator: @unchecked Sendable {
        let lock = NSLock()
        var floor: UInt64 = 0
    }

    /// UserDefaults has no compare-and-swap operation. Instances addressing the same durable
    /// subject share this read/reserve/write coordinator, so a low writer cannot overtake a high
    /// reservation. The namespace is explicit because a defaults key alone does not identify a
    /// domain; test suites and production must never share a process floor by spelling accident.
    private static let coordinators = CloudLocked([String: ProcessCoordinator]())

    private let defaults: UserDefaults
    private let key: String
    private let coordinator: ProcessCoordinator

    public init(
        defaults: UserDefaults = .standard,
        key: String = defaultKey,
        persistenceNamespace: String = "user-defaults.standard"
    ) {
        self.defaults = defaults
        self.key = key
        let subject = persistenceNamespace + "\u{0}" + key
        coordinator = Self.coordinators.withLock { values in
            if let existing = values[subject] { return existing }
            let created = ProcessCoordinator()
            values[subject] = created
            return created
        }
    }

    public func reserve(atLeast wanted: UInt64) -> UInt64 {
        coordinator.lock.lock()
        defer { coordinator.lock.unlock() }
        coordinator.floor = max(coordinator.floor, wanted)
        return coordinator.floor
    }

    public func currentEpoch() throws -> UInt64 {
        coordinator.lock.lock()
        defer { coordinator.lock.unlock() }
        let durable = (defaults.object(forKey: key) as? NSNumber)?.uint64Value ?? 0
        coordinator.floor = max(coordinator.floor, durable)
        return coordinator.floor
    }

    public func advance(to wanted: UInt64) throws {
        coordinator.lock.lock()
        defer { coordinator.lock.unlock() }
        let durable = (defaults.object(forKey: key) as? NSNumber)?.uint64Value ?? 0
        let required = max(coordinator.floor, max(wanted, durable))
        coordinator.floor = required
        guard required > durable else { return }
        defaults.set(NSNumber(value: required), forKey: key)
        let retained = (defaults.object(forKey: key) as? NSNumber)?.uint64Value ?? 0
        guard retained >= required else {
            throw CloudAccountError.credentialInvalidationFailed("the epoch was not retained")
        }
    }
}

public protocol CloudAccountHTTPTransport: Sendable {
    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse)
}

public struct CloudAccountURLSessionTransport: CloudAccountHTTPTransport, Sendable {
    public let session: URLSession

    public init(session: URLSession = .shared) { self.session = session }

    public func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw CloudAccountError.invalidResponse
        }
        return (data, http)
    }
}

public struct CloudMachineMetadata: Equatable, Sendable {
    public let name: String
    public let platform: String
    public let appVersion: String?

    public init(name: String, platform: String, appVersion: String? = nil) {
        self.name = name
        self.platform = platform
        self.appVersion = appVersion
    }
}

public struct CloudDeviceLoginStart: Equatable, Sendable,
                              CustomStringConvertible, CustomDebugStringConvertible {
    public let deviceCode: String
    public let userCode: String
    public let verificationURL: URL
    public let verificationCompleteURL: URL
    public let expiresIn: Int
    public let interval: Int
    fileprivate let receivedAt: TimeInterval
    /// Captured when the flow opened, so every poll it later makes is judged against the state
    /// of the world *before* the person could have signed out.
    fileprivate let generation: CloudCredentialGeneration

    public var description: String {
        "CloudDeviceLoginStart(deviceCode: <redacted>, userCode: \(userCode), interval: \(interval))"
    }

    public var debugDescription: String { description }
}

public struct CloudMachineIdentity: Equatable, Sendable {
    public let accountID: String
    public let machineID: String

    public init(accountID: String, machineID: String) {
        self.accountID = accountID
        self.machineID = machineID
    }
}

public struct ScheduleWebhookLease: Codable, Equatable, Sendable {
    public let token: String
    public let revision: Int
    public let expiresAt: String
    enum CodingKeys: String, CodingKey {
        case token, revision
        case expiresAt = "expires_at"
    }
}

public struct ScheduleWebhookClaim: Codable, Equatable, Sendable {
    public var deliveryID: String
    public let hookID: String
    public let hookGeneration: Int
    public let acceptedAt: String
    public let expiresAt: String
    public var deliveryDigest: String
    public let attempt: Int
    public let lease: ScheduleWebhookLease
    enum CodingKeys: String, CodingKey {
        case deliveryID = "delivery_id"
        case hookID = "hook_id"
        case hookGeneration = "hook_generation"
        case acceptedAt = "accepted_at"
        case expiresAt = "expires_at"
        case deliveryDigest = "delivery_digest"
        case attempt, lease
    }
}

public struct ScheduleWebhookClaimResult: Equatable, Sendable {
    public let delivery: ScheduleWebhookClaim?
    public let serverTime: String
    public let pollAfterMilliseconds: Int
}

public struct ScheduleWebhookActivation: Equatable, Sendable {
    public let hookID: String
    public let state: String
    public let revision: Int
}

public struct ScheduleWebhookReceiptAck: Equatable, Sendable {
    public let deliveryID: String
    public let receiptVersion: Int
    public let state: String
    public let acknowledgedAt: String
    public let duplicate: Bool
}

public struct ScheduleWebhookReceipt: Codable, Equatable, Sendable {
    public let schema: String
    public let receiptVersion: Int
    public let previousReceiptVersion: Int
    public let kind: String
    public let occurredAt: String
    public let macBuild: String
    public let leaseToken: String?
    public let taskID: String?
    public let outcomeCode: String?
    public let taskTerminalState: String?
    public let retryAt: String?
    enum CodingKeys: String, CodingKey {
        case schema, kind
        case receiptVersion = "receipt_version"
        case previousReceiptVersion = "previous_receipt_version"
        case occurredAt = "occurred_at"
        case macBuild = "mac_build"
        case leaseToken = "lease_token"
        case taskID = "task_id"
        case outcomeCode = "outcome_code"
        case taskTerminalState = "task_terminal_state"
        case retryAt = "retry_at"
    }

    public init(schema: String, receiptVersion: Int, previousReceiptVersion: Int,
                kind: String, occurredAt: String, macBuild: String,
                leaseToken: String?, taskID: String?, outcomeCode: String?,
                taskTerminalState: String?, retryAt: String?) {
        self.schema = schema
        self.receiptVersion = receiptVersion
        self.previousReceiptVersion = previousReceiptVersion
        self.kind = kind
        self.occurredAt = occurredAt
        self.macBuild = macBuild
        self.leaseToken = leaseToken
        self.taskID = taskID
        self.outcomeCode = outcomeCode
        self.taskTerminalState = taskTerminalState
        self.retryAt = retryAt
    }

    public func encode(to encoder: Encoder) throws {
        var values = encoder.container(keyedBy: CodingKeys.self)
        try values.encode(schema, forKey: .schema)
        try values.encode(receiptVersion, forKey: .receiptVersion)
        try values.encode(previousReceiptVersion, forKey: .previousReceiptVersion)
        try values.encode(kind, forKey: .kind)
        try values.encode(occurredAt, forKey: .occurredAt)
        try values.encode(macBuild, forKey: .macBuild)
        try values.encode(leaseToken, forKey: .leaseToken)
        try values.encode(taskID, forKey: .taskID)
        try values.encode(outcomeCode, forKey: .outcomeCode)
        try values.encode(taskTerminalState, forKey: .taskTerminalState)
        try values.encode(retryAt, forKey: .retryAt)
    }
}

enum ScheduleWebhookActivationDecoder {
    static func decode(_ data: Data, hookID: String, expectedRevision: Int) throws
        -> ScheduleWebhookActivation {
        guard let object = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw CloudAccountError.invalidResponse
        }
        guard Set(object.keys) == ["schema", "hook"],
              object["schema"] as? String == "clawdline.schedule_webhook.management.v1",
              let hook = object["hook"] as? [String: Any] else {
            throw CloudAccountError.invalidResponse
        }
        let hookKeys = Set(["hook_id", "machine_id", "state", "availability", "generation",
                            "revision", "created_at", "activated_at", "rotated_at",
                            "disabled_at", "credential_fingerprint"])
        guard Set(hook.keys) == hookKeys,
              hook["hook_id"] as? String == hookID,
              let state = hook["state"] as? String, state == "active",
              let revision = strictInt(hook["revision"]),
              revision == expectedRevision + 1 else {
            throw CloudAccountError.invalidResponse
        }
        return .init(hookID: hookID, state: state, revision: revision)
    }
}

public enum CloudDeviceLoginPollState: Equatable, Sendable {
    case authorizationPending
    case slowDown(retryAfter: Int)
    case accessDenied
    case expired
    case complete(CloudMachineIdentity)

    public var retryAfter: Int? {
        switch self {
        case .slowDown(let seconds): return seconds
        default: return nil
        }
    }
}

public struct CloudMachineCredential: Equatable, Codable, Sendable,
                               CustomStringConvertible, CustomDebugStringConvertible {
    public let accountID: String
    public let machineID: String
    fileprivate let secret: String
    fileprivate let validityEpoch: UInt64

    public init(accountID: String, machineID: String, secret: String, validityEpoch: UInt64 = 0) {
        self.accountID = accountID
        self.machineID = machineID
        self.secret = secret
        self.validityEpoch = validityEpoch
    }

    public var description: String {
        "CloudMachineCredential(accountID: \(accountID), machineID: \(machineID), secret: <redacted>)"
    }

    public var debugDescription: String { description }

    enum CodingKeys: String, CodingKey {
        case accountID = "account_id"
        case machineID = "machine_id"
        case secret = "machine_credential"
        case validityEpoch = "validity_epoch"
    }

    public init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        accountID = try values.decode(String.self, forKey: .accountID)
        machineID = try values.decode(String.self, forKey: .machineID)
        secret = try values.decode(String.self, forKey: .secret)
        // Credentials written before the invariant existed belong to the original epoch.
        validityEpoch = try values.decodeIfPresent(UInt64.self, forKey: .validityEpoch) ?? 0
    }

    public func encode(to encoder: Encoder) throws {
        var values = encoder.container(keyedBy: CodingKeys.self)
        try values.encode(accountID, forKey: .accountID)
        try values.encode(machineID, forKey: .machineID)
        try values.encode(secret, forKey: .secret)
        try values.encode(validityEpoch, forKey: .validityEpoch)
    }
}

public struct CloudMachine: Equatable, Sendable {
    public let id: String
    public let name: String
    public let platform: String
    public let appVersion: String?
    public let publicKey: String
    public let lastSeenAt: Date?
    public let createdAt: Date
    public let revokedAt: Date?
}

public struct CloudMachineList: Equatable, Sendable {
    public let machines: [CloudMachine]
    public let active: Int
}

public enum CloudDeviceKind: String, Codable, Sendable {
    case browser
    case ios
    case android
}

public enum CloudDeviceCapability: String, Codable, Sendable {
    case readSessions = "read_sessions"
    case readTranscript = "read_transcript"
    case sendPrompt = "send_prompt"
    case startSession = "start_session"
}

public struct CloudViewerDevice: Equatable, Sendable {
    public let id: String
    public let kind: CloudDeviceKind
    public let name: String
    public let capabilities: [CloudDeviceCapability]
    public let publicKey: String
    public let lastSeenAt: Date?
    public let createdAt: Date
    public let revokedAt: Date?
}

public struct CloudDeviceList: Equatable, Sendable {
    public let devices: [CloudViewerDevice]
    public let active: Int
}

public struct CloudHeartbeat: Equatable, Sendable {
    public let at: Date
}

public struct CloudRevocation: Equatable, Sendable {
    public let revokedAt: Date
    public let routing: String
    public let contentKeyRotation: String
}

/// The server and this client treat the payload only as base64 bytes. Its description is
/// deliberately redacted so diagnostics cannot accidentally reveal a key handover.
public struct CloudOpaquePairingBlob: Equatable, Sendable,
                               CustomStringConvertible, CustomDebugStringConvertible {
    fileprivate let base64: String

    public init(base64: String) throws {
        guard let decoded = Data(base64Encoded: base64),
              decoded.base64EncodedString() == base64 else {
            throw CloudAccountError.invalidPairingBlob
        }
        self.base64 = base64
    }

    public var description: String { "CloudOpaquePairingBlob(<redacted>)" }
    public var debugDescription: String { description }

    /// The exact bytes the wire carries. Named so that reading them is a deliberate act: the
    /// only caller is the suite, proving that what went to `POST /v1/pairing/complete` is what
    /// a viewer can open.
    public var wireBase64ForTesting: String { base64 }

    /// The opaque bytes at the transport/cryptography seam. Callers still cannot inspect them
    /// through logs or descriptions; invitation decryption is the second legitimate consumer.
    public var wireBase64: String { base64 }
}

public struct CloudPairingStart: Equatable, Sendable,
                          CustomStringConvertible, CustomDebugStringConvertible {
    public let pairingID: String
    public let claimNonce: String
    public let expiresAt: Date
    public let expiresIn: Int

    public var description: String {
        "CloudPairingStart(pairingID: \(pairingID), claimNonce: <redacted>, expiresIn: \(expiresIn))"
    }

    public var debugDescription: String { description }
}

public struct CloudPairingDelivery: Equatable, Sendable {
    public let fingerprint: String
}

public enum CloudPairingClaim: Equatable, Sendable {
    case pending
    case complete(blob: CloudOpaquePairingBlob, senderDeviceID: String?)
}

public struct CloudPairingInvitationStart: Equatable, Sendable {
    public let invitationID: String
    public let expiresAt: Date
    public let expiresIn: Int
}

public enum CloudPairingInvitationPoll: Equatable, Sendable {
    case pending
    case ready(
        accountID: String, viewerDeviceID: String, machineID: String,
        encryptedOffer: CloudOpaquePairingBlob)
}

/// The public W5-2 machine-side identity protocol. These values deliberately model the exact
/// canonical-JSON contract rather than an `Encodable` approximation: signatures, duplicate
/// detection, and reply-loss replay all name the same bytes on every platform.
public struct CloudIdentityPairingStartRequest: Equatable, Sendable {
    public let machineSigningKey: String
    public let machineFingerprint: String
    public let machineEphemeralKey: String
    public let pairingNonce: String
    public let previousContentKeyEpoch: Int64
    public let contentKeyEpoch: Int64
    public let rotationID: String

    public init(machineSigningKey: String, machineFingerprint: String,
                machineEphemeralKey: String, pairingNonce: String,
                previousContentKeyEpoch: Int64, contentKeyEpoch: Int64,
                rotationID: String) {
        self.machineSigningKey = machineSigningKey
        self.machineFingerprint = machineFingerprint
        self.machineEphemeralKey = machineEphemeralKey
        self.pairingNonce = pairingNonce
        self.previousContentKeyEpoch = previousContentKeyEpoch
        self.contentKeyEpoch = contentKeyEpoch
        self.rotationID = rotationID
    }

    public var canonicalBody: Data {
        CloudCanonicalJSON.canonicalData(.object([
            "v": .int(1),
            "machine_signing_key": .string(machineSigningKey),
            "machine_fingerprint": .string(machineFingerprint),
            "machine_ephemeral_key": .string(machineEphemeralKey),
            "pairing_nonce": .string(pairingNonce),
            "previous_content_key_epoch": .int(previousContentKeyEpoch),
            "content_key_epoch": .int(contentKeyEpoch),
            "rotation_id": .string(rotationID)
        ]))
    }
}

public struct CloudIdentityEpochs: Equatable, Sendable {
    public let identityEpoch: Int64
    public let machineKeyEpoch: Int64
    public let viewerKeyEpoch: Int64?
    public let contentKeyEpoch: Int64?
    public let jwksGeneration: Int64
}

public struct CloudIdentityRotationWindow: Equatable, Sendable {
    public let oldEpoch: Int64
    public let newEpoch: Int64
    public let oldAcceptUntilMilliseconds: Int64
    public let newAcceptFromMilliseconds: Int64
}

public struct CloudIdentityPairingStart: Equatable, Sendable {
    public let qr: CloudPairingQR
    public let epochs: CloudIdentityEpochs
    public let rotationID: String
    public let signingRotation: CloudIdentityRotationWindow
    public let jwksRotation: CloudIdentityRotationWindow
    public let contentKeyRotation: CloudIdentityRotationWindow
}

public struct CloudIdentityPairingPhaseReceipt: Equatable, Sendable {
    public enum Status: String, Sendable { case recorded, duplicate }
    public let status: Status
    public let pairingID: String
    public let phase: CloudPairingPhase
    public let phaseSHA256: String
    public let recordedAtMilliseconds: Int64
    public let epochs: CloudIdentityEpochs
}

public enum CloudIdentityPairingPoll: Equatable, Sendable {
    case pending(pairingID: String, phase: CloudPairingPhase)
    case ready(pairingID: String, phase: CloudPairingPhase, wrapper: CloudPairingWrapper,
               phaseSHA256: String, recordedAtMilliseconds: Int64, finalized: Bool)
}

public struct CloudMachineIdentityRotationReceipt: Equatable, Sendable {
    public enum Status: String, Sendable { case rotated, duplicate }
    public let status: Status
    public let deviceID: String
    public let identityEpoch: Int64
    public let keyEpoch: Int64
    public let capabilityEpoch: Int64
    public let keyFingerprint: String
    public let oldAcceptUntilMilliseconds: Int64
    public let newAcceptFromMilliseconds: Int64
}

public enum CloudIdentityPairingWireContract {
    public static let startPath = "/v1/pairing/identity/start"
    public static let phasePathPrefix = "/v1/pairing/identity/phases/"
    public static let pollPath = "/v1/pairing/identity/poll"
    public static let machineRotationPath = "/v1/machines/:id/identity/rotate"

    /// Cross-repository comparison vector. These are byte-for-byte the private API/Relay vector,
    /// not a public-side re-rendering of the same idea, so the owning root can compare one digest.
    public static let canonicalVector = Data((
        "{\"v\":1,\"start\":{\"method\":\"POST\",\"route\":\"/v1/pairing/identity/start\",\"writer_role\":\"machine\"},\"phases\":[" +
        "{\"phase\":\"offer\",\"write\":{\"method\":\"POST\",\"route\":\"/v1/pairing/identity/phases/offer\",\"role\":\"viewer\"},\"poll\":{\"method\":\"POST\",\"route\":\"/v1/pairing/identity/poll\",\"role\":\"machine\"}}," +
        "{\"phase\":\"grant\",\"write\":{\"method\":\"POST\",\"route\":\"/v1/pairing/identity/phases/grant\",\"role\":\"machine\"},\"poll\":{\"method\":\"POST\",\"route\":\"/v1/pairing/identity/poll\",\"role\":\"viewer\"}}," +
        "{\"phase\":\"activate\",\"write\":{\"method\":\"POST\",\"route\":\"/v1/pairing/identity/phases/activate\",\"role\":\"viewer\"},\"poll\":{\"method\":\"POST\",\"route\":\"/v1/pairing/identity/poll\",\"role\":\"machine\"}}," +
        "{\"phase\":\"confirm\",\"write\":{\"method\":\"POST\",\"route\":\"/v1/pairing/identity/phases/confirm\",\"role\":\"machine\"},\"poll\":{\"method\":\"POST\",\"route\":\"/v1/pairing/identity/poll\",\"role\":\"viewer\"}}]}"
    ).utf8)

    /// Public lifecycle vector adds exact replay members and pin timing without changing the
    /// smaller shared route vector above.
    public static let canonicalLifecycleVector = CloudCanonicalJSON.canonicalData(.object([
        "v": .int(1),
        "routes": .object([
            "start": .string(startPath), "phase": .string(phasePathPrefix + ":phase"),
            "poll": .string(pollPath), "machine_rotation": .string(machineRotationPath)
        ]),
        "writers": .object([
            "offer": .string("viewer"), "grant": .string("machine"),
            "activate": .string("viewer"), "confirm": .string("machine")
        ]),
        "readers": .object([
            "offer": .string("machine"), "grant": .string("viewer"),
            "activate": .string("machine"), "confirm": .string("viewer")
        ]),
        "pin_after": .string("confirm_receipt"),
        "phase_write_members": .array([.string("blob"), .string("claim_nonce")]),
        "poll_members": .array([.string("claim_nonce"), .string("pairing_id"), .string("phase")]),
        "replay": .string("same_phase_same_canonical_bytes_returns_duplicate")
    ]))
}

/// Client-side QR key derivation is intentionally outside this transport seam. A later UX
/// component must generate that material and decide what bytes become `ciphertext`.
public protocol CloudPairingCryptographyProviding: Sendable {
    func makeOpaqueHandover() async throws -> CloudOpaquePairingBlob
    func openOpaqueHandover(_ blob: CloudOpaquePairingBlob) async throws
}

public final class CloudAccountClient: Sendable {
    public typealias DeviceKeyLoader = @Sendable () throws -> CloudDeviceKeyPair
    public typealias Sleeper = @Sendable (TimeInterval) async throws -> Void
    public typealias Clock = @Sendable () -> TimeInterval

    public static let machineCredentialAccount = "machine-credential-v1"

    public let apiBaseURL: URL
    private let transport: any CloudAccountHTTPTransport
    private let credentialStore: any CloudKeyStoring
    private let invalidationStore: any CloudCredentialInvalidationStoring
    private let deviceKeyLoader: DeviceKeyLoader
    private let sleeper: Sleeper
    private let clock: Clock
    /// Deliberately **not** guarded by the store's coordinator. Bumping it must never wait on a
    /// Keychain call, because the callers that bump it — sign-out and cancel — are the ones a
    /// blocked Keychain would otherwise trap. It only ever increases, so a comparison taken
    /// inside the transaction is sound without holding the same lock.
    private let loginGeneration: CloudLocked<UInt64>

    public init(
        apiBaseURL: URL,
        transport: any CloudAccountHTTPTransport,
        credentialStore: any CloudKeyStoring,
        invalidationStore: any CloudCredentialInvalidationStoring =
            CloudInMemoryCredentialInvalidationStore(),
        deviceKeyLoader: @escaping DeviceKeyLoader,
        sleeper: @escaping Sleeper = { seconds in
            guard seconds > 0 else { return }
            guard let nanoseconds = checkedSleepNanoseconds(seconds) else {
                throw CloudAccountError.invalidResponse
            }
            try await Task.sleep(nanoseconds: nanoseconds)
        },
        clock: @escaping Clock = { ProcessInfo.processInfo.systemUptime }
    ) {
        self.apiBaseURL = apiBaseURL
        self.transport = transport
        self.credentialStore = credentialStore
        self.invalidationStore = invalidationStore
        self.deviceKeyLoader = deviceKeyLoader
        self.sleeper = sleeper
        self.clock = clock
        loginGeneration = CloudLocked((try? invalidationStore.currentEpoch()) ?? 0)
    }

#if canImport(Security)
    public convenience init(
        apiBaseURL: URL = URL(string: "https://api.clawdline.com")!,
        session: URLSession = .shared,
        credentialStore: any CloudKeyStoring = CloudProtectedKeychainStore(),
        invalidationStore: any CloudCredentialInvalidationStoring =
            CloudCredentialInvalidationDefaultsStore(),
        keys: CloudKeys = CloudKeys()
    ) {
        self.init(
            apiBaseURL: apiBaseURL,
            transport: CloudAccountURLSessionTransport(session: session),
            credentialStore: credentialStore,
            invalidationStore: invalidationStore,
            deviceKeyLoader: { try keys.loadOrCreateDeviceKeyPair() }
        )
    }
#endif

    /// The generation a credential must still belong to for the store to accept it.
    public func credentialGeneration() -> CloudCredentialGeneration {
        let durable = (try? invalidationStore.currentEpoch()) ?? 0
        return CloudCredentialGeneration(loginGeneration.withLock { current in
            current = max(current, durable)
            return current
        })
    }

    /// Abandon every sign-in already in flight. Their credentials are refused at the store even
    /// if the network answers afterwards, so cancelling a login cannot leave one behind.
    ///
    /// Cheap and non-blocking on purpose: sign-out and cancel both call it from the main actor.
    @discardableResult
    public func reservePendingLoginInvalidation() -> CloudCredentialGeneration {
        let candidate = loginGeneration.withLock { current in
            if current < UInt64.max { current += 1 }
            return current
        }
        let reserved = invalidationStore.reserve(atLeast: candidate)
        return CloudCredentialGeneration(loginGeneration.withLock { current in
            current = max(current, reserved)
            return current
        })
    }

    /// Persist a reservation off the main actor. UI adapters reserve synchronously (an atomic
    /// increment only), then hand this call to their bounded writer so a slow persistence seam
    /// cannot freeze AppKit.
    public func persistPendingLoginInvalidation(_ generation: CloudCredentialGeneration) throws {
        do {
            try invalidationStore.advance(to: generation.value)
        } catch let error as CloudAccountError {
            throw error
        } catch {
            throw CloudAccountError.credentialInvalidationFailed(Self.message(for: error))
        }
    }

    @discardableResult
    public func invalidatePendingLogins() throws -> CloudCredentialGeneration {
        let generation = reservePendingLoginInvalidation()
        try persistPendingLoginInvalidation(generation)
        return generation
    }

    public func startDeviceLogin(metadata: CloudMachineMetadata) async throws -> CloudDeviceLoginStart {
        let generation = credentialGeneration()
        let key = try deviceKeyLoader()
        let body = DeviceStartRequest(
            name: metadata.name,
            platform: metadata.platform,
            appVersion: metadata.appVersion,
            publicKey: key.publicKeyRaw.base64EncodedString()
        )
        let data = try await send(method: "POST", path: ["v1", "auth", "device", "start"], body: body)
        let receivedAt = clock()
        try requireObject(data, keys: [
            "device_code", "user_code", "verification_uri", "verification_uri_complete",
            "expires_in", "interval",
        ])
        let wire: DeviceStartResponse = try decode(data)
        guard !wire.deviceCode.isEmpty, !wire.userCode.isEmpty,
              let verificationURL = URL(string: wire.verificationURI),
              let verificationCompleteURL = URL(string: wire.verificationURIComplete),
              isSchedulableSleepSeconds(wire.expiresIn),
              isSchedulableSleepSeconds(wire.interval) else {
            throw CloudAccountError.invalidResponse
        }
        return CloudDeviceLoginStart(
            deviceCode: wire.deviceCode,
            userCode: wire.userCode,
            verificationURL: verificationURL,
            verificationCompleteURL: verificationCompleteURL,
            expiresIn: wire.expiresIn,
            interval: wire.interval,
            receivedAt: receivedAt,
            generation: generation
        )
    }

    /// `startedAt` names the sign-in this poll belongs to. Left out, the poll stands for itself
    /// and is judged from the moment it was made — which still refuses a sign-out that lands
    /// during the round trip, and is the honest default for a single call.
    public func pollDeviceLogin(
        deviceCode: String,
        startedAt generation: CloudCredentialGeneration? = nil
    ) async throws -> CloudDeviceLoginPollState {
        // Read before the request leaves. Reading it after the answer arrives would compare the
        // world against itself and could never notice the sign-out in between.
        let admitted = generation ?? credentialGeneration()
        let data = try await send(
            method: "POST", path: ["v1", "auth", "device", "poll"],
            body: DevicePollRequest(deviceCode: deviceCode)
        )
        let object = try jsonObject(data)
        guard let status = object["status"] as? String else {
            throw CloudAccountError.invalidResponse
        }
        switch status {
        case "authorization_pending":
            try requireKeys(object, exactly: ["status"])
            return .authorizationPending
        case "slow_down":
            try requireKeys(object, exactly: ["status", "retry_after_seconds"])
            guard let seconds = strictInt(object["retry_after_seconds"]),
                  isSchedulableSleepSeconds(seconds) else {
                throw CloudAccountError.invalidResponse
            }
            return .slowDown(retryAfter: seconds)
        case "access_denied":
            try requireKeys(object, exactly: ["status"])
            return .accessDenied
        case "expired_token":
            try requireKeys(object, exactly: ["status"])
            return .expired
        case "complete":
            try requireKeys(object, exactly: [
                "status", "account_id", "machine_id", "machine_credential",
            ])
            guard let accountID = nonemptyString(object["account_id"]),
                  let machineID = nonemptyString(object["machine_id"]),
                  let secret = nonemptyString(object["machine_credential"]) else {
                throw CloudAccountError.invalidResponse
            }
            let credential = CloudMachineCredential(
                accountID: accountID, machineID: machineID, secret: secret,
                validityEpoch: admitted.value)
            try withPersistenceTransaction {
                try persistUnlocked(credential, admittedAt: admitted)
            }
            return .complete(CloudMachineIdentity(accountID: accountID, machineID: machineID))
        default:
            throw CloudAccountError.invalidResponse
        }
    }

    /// Polls until the RFC 8628 flow reaches a terminal state. `slow_down` overrides the
    /// normal interval for the next request; all other waiting polls use the advertised interval.
    public func waitForDeviceLogin(
        _ started: CloudDeviceLoginStart,
        onState: @Sendable (CloudDeviceLoginPollState) async -> Void = { _ in }
    ) async throws -> CloudDeviceLoginPollState {
        var delay = started.interval
        let deadline = started.receivedAt + TimeInterval(started.expiresIn)
        while true {
            try Task.checkCancellation()
            let remaining = deadline - clock()
            guard remaining > 0 else { return .expired }
            try await sleeper(min(TimeInterval(delay), remaining))
            try Task.checkCancellation()
            guard clock() < deadline else { return .expired }
            let state = try await pollDeviceLogin(
                deviceCode: started.deviceCode, startedAt: started.generation)
            try Task.checkCancellation()
            await onState(state)
            switch state {
            case .authorizationPending:
                delay = started.interval
            case .slowDown(let retryAfter):
                delay = retryAfter
            default:
                return state
            }
        }
    }

    public func restoredMachineIdentity() throws -> CloudMachineIdentity? {
        try withPersistenceTransaction {
            guard let credential = try loadCredentialUnlocked() else { return nil }
            return CloudMachineIdentity(
                accountID: credential.accountID, machineID: credential.machineID)
        }
    }

    public func authorizationHeader() throws -> String {
        try withPersistenceTransaction {
            try authorizationHeaderUnlocked()
        }
    }

    /// Explicit sign-out removes the persisted bearer before the client reports signed-out state.
    ///
    /// **The generation is bumped before the transaction is entered, not inside it.** A poll
    /// already blocked on the coordinator has captured its generation and will compare it after
    /// it acquires the lock; bumping first means it sees the new value whichever of the two gets
    /// there first, so the two orders both end signed out rather than one of them resurrecting
    /// the credential this call just removed.
    ///
    /// It must be called off the main thread: ``CloudKeychainStore/remove(_:)`` refuses there.
    public func signOut() throws {
        let invalidation = reservePendingLoginInvalidation()
        try signOut(reservedAt: invalidation)
    }

    public func signOut(reservedAt invalidation: CloudCredentialGeneration) throws {
        try persistPendingLoginInvalidation(invalidation)
        do {
            try withPersistenceTransaction {
                try credentialStore.remove(Self.machineCredentialAccount)
            }
        } catch {
            throw CloudAccountError.credentialCleanupPending(Self.message(for: error))
        }
    }

    public func authorizationHeaderProvider() -> CloudAPIDeviceTokenProvider.AuthorizationHeaderProvider {
        { [weak self] in
            guard let self else { throw CloudAccountError.missingMachineCredential }
            return try self.authorizationHeader()
        }
    }

    public func deviceTokenProvider(session: URLSession = .shared) -> CloudAPIDeviceTokenProvider {
        CloudAPIDeviceTokenProvider(
            apiBaseURL: apiBaseURL,
            session: session,
            authorizationHeader: authorizationHeaderProvider()
        )
    }

    public func heartbeat(appVersion: String? = nil) async throws -> CloudHeartbeat {
        let data = try await send(
            method: "POST", path: ["v1", "machines", "heartbeat"],
            body: HeartbeatRequest(appVersion: appVersion), authorization: .machineCredential
        )
        try requireObject(data, keys: ["ok", "at"])
        let wire: HeartbeatResponse = try decode(data)
        guard wire.ok, let date = parseDate(wire.at) else {
            throw CloudAccountError.invalidResponse
        }
        return CloudHeartbeat(at: date)
    }

    public func activateScheduleWebhook(hookID: String, expectedRevision: Int,
                                 idempotencyKey: String) async throws
        -> ScheduleWebhookActivation {
        let result = try await rawSend(
            method: "POST", path: ["v1", "schedule-webhooks", hookID, "activate"],
            body: ScheduleWebhookActivateRequest(expectedRevision: expectedRevision),
            authorization: .machineCredential,
            headers: ["Idempotency-Key": idempotencyKey])
        try requireStatus(result, expected: 200)
        return try ScheduleWebhookActivationDecoder.decode(
            result.data, hookID: hookID, expectedRevision: expectedRevision)
    }

    public func claimScheduleWebhookDelivery(waitSeconds: Int = 20) async throws
        -> ScheduleWebhookClaimResult {
        guard (0...25).contains(waitSeconds) else { throw CloudAccountError.invalidResponse }
        let result = try await rawSend(
            method: "POST", path: ["v1", "schedule-webhook-deliveries", "claim"],
            body: ScheduleWebhookClaimRequest(
                protocolName: "clawdline.schedule_webhook.v1", waitSeconds: waitSeconds),
            authorization: .machineCredential)
        try requireStatus(result, expected: 200)
        let object = try jsonObject(result.data)
        try requireKeys(object, exactly: ["schema", "delivery", "server_time", "poll_after_ms"])
        guard object["schema"] as? String == "clawdline.schedule_webhook.claim.v1",
              let serverTime = object["server_time"] as? String,
              let pollAfter = strictInt(object["poll_after_ms"]),
              (0...300_000).contains(pollAfter) else {
            throw CloudAccountError.invalidResponse
        }
        if object["delivery"] is NSNull {
            return .init(delivery: nil, serverTime: serverTime,
                         pollAfterMilliseconds: pollAfter)
        }
        guard let deliveryObject = object["delivery"] as? [String: Any] else {
            throw CloudAccountError.invalidResponse
        }
        try requireKeys(deliveryObject, exactly: ["delivery_id", "hook_id", "hook_generation",
                                                  "accepted_at", "expires_at", "delivery_digest",
                                                  "attempt", "lease"])
        guard let lease = deliveryObject["lease"] as? [String: Any] else {
            throw CloudAccountError.invalidResponse
        }
        try requireKeys(lease, exactly: ["token", "revision", "expires_at"])
        let bytes = try JSONSerialization.data(withJSONObject: deliveryObject)
        let delivery: ScheduleWebhookClaim = try decode(bytes)
        return .init(delivery: delivery, serverTime: serverTime,
                     pollAfterMilliseconds: pollAfter)
    }

    public func sendScheduleWebhookReceipt(deliveryID: String, receipt: ScheduleWebhookReceipt)
        async throws -> ScheduleWebhookReceiptAck {
        let result = try await rawSend(
            method: "POST",
            path: ["v1", "schedule-webhook-deliveries", deliveryID, "receipts"],
            body: receipt, authorization: .machineCredential,
            headers: ["Idempotency-Key": "receipt:\(deliveryID):\(receipt.receiptVersion)"])
        try requireStatus(result, expected: 200)
        let object = try jsonObject(result.data)
        try requireKeys(object, exactly: ["schema", "delivery_id", "receipt_version", "state",
                                          "acknowledged_at", "duplicate"])
        guard object["schema"] as? String == "clawdline.schedule_webhook.receipt_ack.v1",
              object["delivery_id"] as? String == deliveryID,
              let version = strictInt(object["receipt_version"]),
              version == receipt.receiptVersion,
              let state = object["state"] as? String,
              let at = object["acknowledged_at"] as? String,
              let duplicate = object["duplicate"] as? Bool else {
            throw CloudAccountError.invalidResponse
        }
        return .init(deliveryID: deliveryID, receiptVersion: version, state: state,
                     acknowledgedAt: at, duplicate: duplicate)
    }

    public func listMachines() async throws -> CloudMachineList {
        let data = try await send(
            method: "GET", path: ["v1", "machines"], authorization: .machineCredential)
        let object = try jsonObject(data)
        try requireKeys(object, exactly: ["machines", "active"])
        guard let rows = object["machines"] as? [[String: Any]],
              let active = strictInt(object["active"]), active >= 0 else {
            throw CloudAccountError.invalidResponse
        }
        let machines = try rows.map(decodeMachine)
        guard active <= machines.count else { throw CloudAccountError.invalidResponse }
        return CloudMachineList(machines: machines, active: active)
    }

    public func listDevices() async throws -> CloudDeviceList {
        let data = try await send(
            method: "GET", path: ["v1", "devices"], authorization: .machineCredential)
        let object = try jsonObject(data)
        try requireKeys(object, exactly: ["devices", "active"])
        guard let rows = object["devices"] as? [[String: Any]],
              let active = strictInt(object["active"]), active >= 0 else {
            throw CloudAccountError.invalidResponse
        }
        let devices = try rows.map(decodeDevice)
        guard active <= devices.count else { throw CloudAccountError.invalidResponse }
        return CloudDeviceList(devices: devices, active: active)
    }

    /// Deployed DELETE routes require the PWA's `cl_session`; a machine bearer is not accepted.
    public func revokeMachine(id: String, browserSessionCookie: String) async throws -> CloudRevocation {
        let startingCredential = try withPersistenceTransaction {
            try loadCredentialUnlocked()
        }
        let data = try await send(
            method: "DELETE", path: ["v1", "machines", id],
            authorization: .browserSessionCookie(browserSessionCookie)
        )
        let revocation = try decodeRevocation(data, machine: true)
        try withPersistenceTransaction {
            guard let startingCredential, startingCredential.machineID == id,
                  try loadCredentialUnlocked() == startingCredential else { return }
            try credentialStore.remove(Self.machineCredentialAccount)
        }
        return revocation
    }

    /// Deployed DELETE routes require the PWA's `cl_session`; a machine bearer is not accepted.
    public func revokeDevice(id: String, browserSessionCookie: String) async throws -> CloudRevocation {
        let data = try await send(
            method: "DELETE", path: ["v1", "devices", id],
            authorization: .browserSessionCookie(browserSessionCookie)
        )
        return try decodeRevocation(data, machine: false)
    }

    public func startIdentityPairing(_ request: CloudIdentityPairingStartRequest) async throws
        -> CloudIdentityPairingStart {
        let result = try await rawSend(
            method: "POST", path: ["v1", "pairing", "identity", "start"],
            canonicalBody: request.canonicalBody, authorization: .machineCredential)
        try requireStatus(result, expected: 200)
        return try Self.decodeIdentityPairingStart(result.data)
    }

    /// Writes the exact `{claim_nonce,blob}` bytes defined by `CloudPairing`; an ordinary
    /// `JSONEncoder` is intentionally not in this path because duplicate detection is by digest.
    public func writeIdentityPairingPhase(
        pairingID: String, phase: CloudPairingPhase, claimNonce: String,
        wrapper: CloudPairingWrapper
    ) async throws -> CloudIdentityPairingPhaseReceipt {
        guard phase == .grant || phase == .confirm,
              wrapper.pairingID == pairingID, wrapper.phase == phase else {
            throw CloudAccountError.invalidResponse
        }
        let body = try CloudPairing.encodePhaseWriteBody(
            claimNonce: claimNonce, wrapper: wrapper)
        let result = try await rawSend(
            method: "POST",
            path: ["v1", "pairing", "identity", "phases", phase.rawValue],
            canonicalBody: body, authorization: .machineCredential)
        try requireStatus(result, expected: 200)
        return try Self.decodeIdentityPhaseReceipt(
            result.data, expectedPairingID: pairingID, expectedPhase: phase)
    }

    public func pollIdentityPairingPhase(
        pairingID: String, phase: CloudPairingPhase, claimNonce: String
    ) async throws -> CloudIdentityPairingPoll {
        guard phase == .offer || phase == .activate else {
            throw CloudAccountError.invalidResponse
        }
        let body = CloudCanonicalJSON.canonicalData(.object([
            "pairing_id": .string(pairingID), "phase": .string(phase.rawValue),
            "claim_nonce": .string(claimNonce)
        ]))
        let result = try await rawSend(
            method: "POST", path: ["v1", "pairing", "identity", "poll"],
            canonicalBody: body, authorization: .machineCredential)
        guard result.response.statusCode == 200 || result.response.statusCode == 202 else {
            try requireStatus(result, expected: 200)
            throw CloudAccountError.invalidResponse
        }
        return try Self.decodeIdentityPhasePoll(
            result.data, statusCode: result.response.statusCode,
            expectedPairingID: pairingID, expectedPhase: phase)
    }

    public func rotateMachineIdentity(
        machineID: String, expectedKeyEpoch: Int64, publicKey: String, fingerprint: String
    ) async throws -> CloudMachineIdentityRotationReceipt {
        let body = CloudCanonicalJSON.canonicalData(.object([
            "expected_key_epoch": .int(expectedKeyEpoch),
            "public_key": .string(publicKey), "fingerprint": .string(fingerprint)
        ]))
        let result = try await rawSend(
            method: "POST", path: ["v1", "machines", machineID, "identity", "rotate"],
            canonicalBody: body, authorization: .machineCredential)
        try requireStatus(result, expected: 200)
        return try Self.decodeMachineIdentityRotation(result.data, expectedMachineID: machineID)
    }

    public func startPairing(fingerprint: String) async throws -> CloudPairingStart {
        let data = try await send(
            method: "POST", path: ["v1", "pairing", "start"],
            body: PairingStartRequest(fingerprint: fingerprint), authorization: .machineCredential
        )
        try requireObject(data, keys: ["pairing_id", "claim_nonce", "expires_at", "expires_in"])
        let wire: PairingStartResponse = try decode(data)
        guard !wire.pairingID.isEmpty, !wire.claimNonce.isEmpty,
              let expiresAt = parseDate(wire.expiresAt), wire.expiresIn > 0 else {
            throw CloudAccountError.invalidResponse
        }
        return CloudPairingStart(
            pairingID: wire.pairingID, claimNonce: wire.claimNonce,
            expiresAt: expiresAt, expiresIn: wire.expiresIn)
    }

    public func completePairing(pairingID: String, blob: CloudOpaquePairingBlob) async throws -> CloudPairingDelivery {
        let data = try await send(
            method: "POST", path: ["v1", "pairing", "complete"],
            body: PairingCompleteRequest(pairingID: pairingID, ciphertext: blob.base64),
            authorization: .machineCredential
        )
        try requireObject(data, keys: ["status", "fingerprint"])
        let wire: PairingCompleteResponse = try decode(data)
        guard wire.status == "delivered", !wire.fingerprint.isEmpty else {
            throw CloudAccountError.invalidResponse
        }
        return CloudPairingDelivery(fingerprint: wire.fingerprint)
    }

    public func claimPairing(pairingID: String, claimNonce: String) async throws -> CloudPairingClaim {
        let result = try await rawSend(
            method: "POST", path: ["v1", "pairing", "claim"],
            body: PairingClaimRequest(pairingID: pairingID, claimNonce: claimNonce),
            authorization: .machineCredential
        )
        if result.response.statusCode == 202 {
            guard try errorCode(result.data) == "pairing_pending" else {
                throw CloudAccountError.invalidResponse
            }
            return .pending
        }
        try requireStatus(result, expected: 200)
        try requireObject(result.data, keys: ["ciphertext", "sender_device_id"])
        let wire: PairingClaimResponse = try decode(result.data)
        let blob = try CloudOpaquePairingBlob(base64: wire.ciphertext)
        return .complete(blob: blob, senderDeviceID: wire.senderDeviceID)
    }

    public func startPairingInvitation(secretHash: Data) async throws -> CloudPairingInvitationStart {
        guard secretHash.count == 32 else { throw CloudAccountError.invalidPairingBlob }
        let data = try await send(
            method: "POST", path: ["v1", "pairing", "invitations", "start"],
            body: PairingInvitationStartRequest(secretHash: secretHash.base64EncodedString()),
            authorization: .machineCredential
        )
        try requireObject(data, keys: ["status", "invitation_id", "expires_at", "expires_in"])
        let wire: PairingInvitationStartResponse = try decode(data)
        guard wire.status == "pending", !wire.invitationID.isEmpty,
              let expiresAt = parseDate(wire.expiresAt), wire.expiresIn > 0 else {
            throw CloudAccountError.invalidResponse
        }
        return CloudPairingInvitationStart(
            invitationID: wire.invitationID, expiresAt: expiresAt, expiresIn: wire.expiresIn)
    }

    public func pollPairingInvitation(invitationID: String) async throws -> CloudPairingInvitationPoll {
        let result = try await rawSend(
            method: "POST", path: ["v1", "pairing", "invitations", "poll"],
            body: PairingInvitationPollRequest(invitationID: invitationID),
            authorization: .machineCredential
        )
        if result.response.statusCode == 202 {
            let wire: PairingInvitationPendingResponse = try decode(result.data)
            guard wire.status == "pending" else { throw CloudAccountError.invalidResponse }
            return .pending
        }
        try requireStatus(result, expected: 200)
        try requireObject(
            result.data,
            keys: ["status", "account_id", "viewer_device_id", "machine_id", "encrypted_offer"])
        let wire: PairingInvitationReadyResponse = try decode(result.data)
        guard wire.status == "ready", !wire.accountID.isEmpty, !wire.viewerDeviceID.isEmpty,
              !wire.machineID.isEmpty else { throw CloudAccountError.invalidResponse }
        return .ready(
            accountID: wire.accountID, viewerDeviceID: wire.viewerDeviceID,
            machineID: wire.machineID,
            encryptedOffer: try CloudOpaquePairingBlob(base64: wire.encryptedOffer))
    }

    private enum Authorization {
        case machineCredential
        case browserSessionCookie(String)
    }

    private struct RawResult {
        let data: Data
        let response: HTTPURLResponse
    }

    private func send<T: Encodable>(
        method: String, path: [String], body: T,
        authorization: Authorization? = nil
    ) async throws -> Data {
        let result = try await rawSend(
            method: method, path: path, body: body, authorization: authorization)
        try requireStatus(result, expected: 200)
        return result.data
    }

    private func send(
        method: String, path: [String], authorization: Authorization? = nil
    ) async throws -> Data {
        let result = try await rawSend(
            method: method, path: path, body: Optional<Int>.none,
            authorization: authorization, encodeNilBody: false)
        try requireStatus(result, expected: 200)
        return result.data
    }

    private func rawSend<T: Encodable>(
        method: String, path: [String], body: T,
        authorization: Authorization?, encodeNilBody: Bool = true,
        headers: [String: String] = [:]
    ) async throws -> RawResult {
        let url = try endpoint(path)
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if encodeNilBody {
            request.httpBody = try JSONEncoder().encode(body)
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        for (name, value) in headers { request.setValue(value, forHTTPHeaderField: name) }
        var sentCredential: CloudMachineCredential?
        switch authorization {
        case .machineCredential:
            let credential = try withPersistenceTransaction { () throws -> CloudMachineCredential in
                guard let credential = try loadCredentialUnlocked() else {
                    throw CloudAccountError.missingMachineCredential
                }
                return credential
            }
            sentCredential = credential
            request.setValue("Bearer \(credential.secret)", forHTTPHeaderField: "Authorization")
        case .browserSessionCookie(let cookie):
            request.setValue("cl_session=\(cookie)", forHTTPHeaderField: "Cookie")
        case nil:
            break
        }
        let (data, response) = try await transport.data(for: request)
        if response.statusCode == 401,
           let sentCredential,
           (try? errorCode(data)) == "no_machine_credential" {
            try withPersistenceTransaction {
                guard try loadCredentialUnlocked() == sentCredential else { return }
                try credentialStore.remove(Self.machineCredentialAccount)
            }
        }
        return RawResult(data: data, response: response)
    }

    private func rawSend(
        method: String, path: [String], canonicalBody: Data,
        authorization: Authorization?, headers: [String: String] = [:]
    ) async throws -> RawResult {
        let url = try endpoint(path)
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = canonicalBody
        for (name, value) in headers { request.setValue(value, forHTTPHeaderField: name) }
        var sentCredential: CloudMachineCredential?
        switch authorization {
        case .machineCredential:
            let credential = try withPersistenceTransaction { () throws -> CloudMachineCredential in
                guard let credential = try loadCredentialUnlocked() else {
                    throw CloudAccountError.missingMachineCredential
                }
                return credential
            }
            sentCredential = credential
            request.setValue("Bearer \(credential.secret)", forHTTPHeaderField: "Authorization")
        case .browserSessionCookie(let cookie):
            request.setValue("cl_session=\(cookie)", forHTTPHeaderField: "Cookie")
        case nil:
            break
        }
        let (data, response) = try await transport.data(for: request)
        if response.statusCode == 401, let sentCredential,
           (try? errorCode(data)) == "no_machine_credential" {
            try withPersistenceTransaction {
                guard try loadCredentialUnlocked() == sentCredential else { return }
                try credentialStore.remove(Self.machineCredentialAccount)
            }
        }
        return RawResult(data: data, response: response)
    }

    private func endpoint(_ path: [String]) throws -> URL {
        var components = URLComponents(url: apiBaseURL, resolvingAgainstBaseURL: false)
        var allowed = CharacterSet.urlPathAllowed
        allowed.remove(charactersIn: "/")
        let encoded = try path.map { component -> String in
            guard let value = component.addingPercentEncoding(withAllowedCharacters: allowed) else {
                throw CloudAccountError.invalidResponse
            }
            return value
        }.joined(separator: "/")
        let base = components?.percentEncodedPath ?? ""
        components?.percentEncodedPath = base.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
            .isEmpty ? "/\(encoded)" : "\(base.hasSuffix("/") ? String(base.dropLast()) : base)/\(encoded)"
        guard let url = components?.url else { throw CloudAccountError.invalidResponse }
        return url
    }

    private func requireStatus(_ result: RawResult, expected: Int) throws {
        guard result.response.statusCode == expected else {
            throw CloudAccountError.http(
                status: result.response.statusCode,
                code: try? errorCode(result.data)
            )
        }
    }

    private func persistUnlocked(_ credential: CloudMachineCredential) throws {
        let data = try JSONEncoder().encode(credential)
        try credentialStore.set(data, for: Self.machineCredentialAccount)
    }

    /// Persist a freshly minted credential, unless the sign-in that minted it was abandoned.
    ///
    /// **The generation is read twice, and the two reads are not redundant.**
    ///
    /// The first refuses the ordinary case — sign-out happened while the network was answering —
    /// and its value is that the secret is *never written*. Undoing a write is second best: by
    /// then the credential has been on disk.
    ///
    /// The second closes the window the first cannot see. `signOut()` bumps the generation
    /// *before* it queues behind this transaction, so a sign-out arriving between the guard and
    /// `SecItemAdd` returning would find nothing to remove and leave the credential behind.
    /// Taking it back here is what makes "signed out" mean signed out under either interleaving.
    ///
    /// Deleting either one leaves a test red; they were checked separately for that reason.
    private func persistUnlocked(
        _ credential: CloudMachineCredential, admittedAt admitted: CloudCredentialGeneration
    ) throws {
        guard credentialGeneration() == admitted else { throw CloudAccountError.loginAbandoned }
        try persistUnlocked(credential)
        guard credentialGeneration() == admitted else {
            do {
                try credentialStore.remove(Self.machineCredentialAccount)
                throw CloudAccountError.loginAbandoned
            } catch let error as CloudAccountError {
                throw error
            } catch {
                throw CloudAccountError.credentialCleanupPending(Self.message(for: error))
            }
        }
    }

    private func loadCredentialUnlocked() throws -> CloudMachineCredential? {
        guard let data = try credentialStore.data(for: Self.machineCredentialAccount) else {
            return nil
        }
        do {
            let credential = try JSONDecoder().decode(CloudMachineCredential.self, from: data)
            guard !credential.accountID.isEmpty, !credential.machineID.isEmpty,
                  !credential.secret.isEmpty else { throw CloudAccountError.invalidResponse }
            let durableEpoch: UInt64
            do {
                durableEpoch = try invalidationStore.currentEpoch()
            } catch {
                throw CloudAccountError.credentialInvalidationFailed(Self.message(for: error))
            }
            let requiredEpoch = max(durableEpoch, credentialGeneration().value)
            guard credential.validityEpoch >= requiredEpoch else { return nil }
            return credential
        } catch let error as CloudAccountError {
            throw error
        } catch {
            throw CloudAccountError.invalidResponse
        }
    }

    private func authorizationHeaderUnlocked() throws -> String {
        guard let credential = try loadCredentialUnlocked() else {
            throw CloudAccountError.missingMachineCredential
        }
        return "Bearer \(credential.secret)"
    }

    private func withPersistenceTransaction<T: Sendable>(
        _ body: @Sendable () throws -> T
    ) rethrows -> T {
        try credentialStore.coordinator.withCriticalRegion(body)
    }

    private static func message(for error: Error) -> String {
        if let localized = error as? LocalizedError,
           let description = localized.errorDescription, !description.isEmpty {
            return description
        }
        let description = error.localizedDescription
        return description.isEmpty ? "unknown persistence error" : description
    }

    private func decodeRevocation(_ data: Data, machine: Bool) throws -> CloudRevocation {
        let allowed = machine
            ? ["revoked_at", "routing", "content_key_rotation", "note"]
            : ["revoked_at", "routing", "content_key_rotation"]
        try requireObject(data, keys: Set(allowed))
        let wire: RevocationResponse = try decode(data)
        guard let date = parseDate(wire.revokedAt), wire.routing == "stopped",
              wire.contentKeyRotation == "lazy" else {
            throw CloudAccountError.invalidResponse
        }
        return CloudRevocation(
            revokedAt: date, routing: wire.routing,
            contentKeyRotation: wire.contentKeyRotation)
    }

    private static func identityObject(_ value: CloudJSONValue, keys: Set<String>) throws
        -> [String: CloudJSONValue] {
        guard case .object(let object) = value, Set(object.keys) == keys else {
            throw CloudAccountError.invalidResponse
        }
        return object
    }

    private static func identityString(_ object: [String: CloudJSONValue], _ key: String)
        throws -> String {
        guard case .string(let value)? = object[key], !value.isEmpty else {
            throw CloudAccountError.invalidResponse
        }
        return value
    }

    private static func identityInt(_ object: [String: CloudJSONValue], _ key: String)
        throws -> Int64 {
        guard case .int(let value)? = object[key], value >= 0 else {
            throw CloudAccountError.invalidResponse
        }
        return value
    }

    private static func identitySHA256(
        _ object: [String: CloudJSONValue], _ key: String
    ) throws -> String {
        let value = try identityString(object, key)
        guard value.utf8.count == 64,
              value.utf8.allSatisfy({ (0x30...0x39).contains($0) || (0x61...0x66).contains($0) })
        else { throw CloudAccountError.invalidResponse }
        return value
    }

    private static func decodeEpochs(
        _ value: CloudJSONValue, phaseReceipt: Bool
    ) throws -> CloudIdentityEpochs {
        let keys: Set<String> = phaseReceipt
            ? ["identity_epoch", "machine_key_epoch", "viewer_key_epoch",
               "content_key_epoch", "jwks_generation"]
            : ["identity_epoch", "machine_key_epoch", "jwks_generation"]
        let object = try identityObject(value, keys: keys)
        return CloudIdentityEpochs(
            identityEpoch: try identityInt(object, "identity_epoch"),
            machineKeyEpoch: try identityInt(object, "machine_key_epoch"),
            viewerKeyEpoch: phaseReceipt ? try identityInt(object, "viewer_key_epoch") : nil,
            contentKeyEpoch: phaseReceipt ? try identityInt(object, "content_key_epoch") : nil,
            jwksGeneration: try identityInt(object, "jwks_generation"))
    }

    private static func decodeRotationWindow(
        _ value: CloudJSONValue, generationNames: Bool = false
    ) throws -> CloudIdentityRotationWindow {
        let old = generationNames ? "old_generation" : "old_epoch"
        let new = generationNames ? "new_generation" : "new_epoch"
        let object = try identityObject(
            value, keys: [old, new, "old_accept_until_ms", "new_accept_from_ms"])
        return CloudIdentityRotationWindow(
            oldEpoch: try identityInt(object, old), newEpoch: try identityInt(object, new),
            oldAcceptUntilMilliseconds: try identityInt(object, "old_accept_until_ms"),
            newAcceptFromMilliseconds: try identityInt(object, "new_accept_from_ms"))
    }

    private static func decodeIdentityPairingStart(_ data: Data) throws
        -> CloudIdentityPairingStart {
        let root = try identityObject(try CloudCanonicalJSON.parseStrict(data), keys: [
            "v", "status", "qr", "identity", "rotation"
        ])
        guard try identityInt(root, "v") == 1,
              try identityString(root, "status") == "pending",
              let qrValue = root["qr"], let identityValue = root["identity"],
              let rotationValue = root["rotation"] else {
            throw CloudAccountError.invalidResponse
        }
        let qrObject = try identityObject(qrValue, keys: [
            "v", "type", "pairing_id", "claim_nonce", "expires_at", "account_id",
            "machine_id", "machine_signing_key", "machine_fingerprint",
            "machine_ephemeral_key", "pairing_nonce"
        ])
        guard try identityInt(qrObject, "v") == 1,
              try identityString(qrObject, "type") == "pairing_qr" else {
            throw CloudAccountError.invalidResponse
        }
        let qr = CloudPairingQR(
            pairingID: try identityString(qrObject, "pairing_id"),
            claimNonce: try identityString(qrObject, "claim_nonce"),
            expiresAt: try identityInt(qrObject, "expires_at"),
            accountID: try identityString(qrObject, "account_id"),
            machineID: try identityString(qrObject, "machine_id"),
            machineSigningKey: try identityString(qrObject, "machine_signing_key"),
            machineFingerprint: try identityString(qrObject, "machine_fingerprint"),
            machineEphemeralKey: try identityString(qrObject, "machine_ephemeral_key"),
            pairingNonce: try identityString(qrObject, "pairing_nonce"))
        _ = try CloudPairing.encodeQRFragment(qr)
        let epochs = try decodeEpochs(identityValue, phaseReceipt: false)
        let rotation = try identityObject(
            rotationValue, keys: ["rotation_id", "signing", "jwks", "content_key"])
        guard let signing = rotation["signing"], let jwks = rotation["jwks"],
              let contentKey = rotation["content_key"] else {
            throw CloudAccountError.invalidResponse
        }
        return CloudIdentityPairingStart(
            qr: qr, epochs: epochs, rotationID: try identityString(rotation, "rotation_id"),
            signingRotation: try decodeRotationWindow(signing),
            jwksRotation: try decodeRotationWindow(jwks, generationNames: true),
            contentKeyRotation: try decodeRotationWindow(contentKey))
    }

    private static func decodeIdentityPhaseReceipt(
        _ data: Data, expectedPairingID: String, expectedPhase: CloudPairingPhase
    ) throws -> CloudIdentityPairingPhaseReceipt {
        let root = try identityObject(try CloudCanonicalJSON.parseStrict(data), keys: [
            "v", "status", "pairing_id", "phase", "phase_sha256", "recorded_at_ms",
            "identity_epoch", "machine_key_epoch", "viewer_key_epoch", "content_key_epoch",
            "jwks_generation"
        ])
        guard try identityInt(root, "v") == 1,
              let status = CloudIdentityPairingPhaseReceipt.Status(
                rawValue: try identityString(root, "status")),
              try identityString(root, "pairing_id") == expectedPairingID,
              try identityString(root, "phase") == expectedPhase.rawValue else {
            throw CloudAccountError.invalidResponse
        }
        let epochs = try decodeEpochs(.object(root.filter {
            ["identity_epoch", "machine_key_epoch", "viewer_key_epoch",
             "content_key_epoch", "jwks_generation"].contains($0.key)
        }), phaseReceipt: true)
        return CloudIdentityPairingPhaseReceipt(
            status: status, pairingID: expectedPairingID, phase: expectedPhase,
            phaseSHA256: try identitySHA256(root, "phase_sha256"),
            recordedAtMilliseconds: try identityInt(root, "recorded_at_ms"), epochs: epochs)
    }

    private static func decodeIdentityPhasePoll(
        _ data: Data, statusCode: Int, expectedPairingID: String,
        expectedPhase: CloudPairingPhase
    ) throws -> CloudIdentityPairingPoll {
        let value = try CloudCanonicalJSON.parseStrict(data)
        if statusCode == 202 {
            let root = try identityObject(value, keys: ["v", "status", "pairing_id", "phase"])
            guard try identityInt(root, "v") == 1,
                  try identityString(root, "status") == "pending",
                  try identityString(root, "pairing_id") == expectedPairingID,
                  try identityString(root, "phase") == expectedPhase.rawValue else {
                throw CloudAccountError.invalidResponse
            }
            return .pending(pairingID: expectedPairingID, phase: expectedPhase)
        }
        let root = try identityObject(value, keys: [
            "v", "status", "pairing_id", "phase", "blob", "phase_sha256",
            "recorded_at_ms", "finalized"
        ])
        guard try identityInt(root, "v") == 1,
              try identityString(root, "status") == "ready",
              try identityString(root, "pairing_id") == expectedPairingID,
              try identityString(root, "phase") == expectedPhase.rawValue,
              case .bool(let finalized)? = root["finalized"], let blob = root["blob"] else {
            throw CloudAccountError.invalidResponse
        }
        let wrapper = try CloudPairing.decodeWrapper(CloudCanonicalJSON.canonicalData(blob))
        guard wrapper.pairingID == expectedPairingID, wrapper.phase == expectedPhase else {
            throw CloudAccountError.invalidResponse
        }
        return .ready(
            pairingID: expectedPairingID, phase: expectedPhase, wrapper: wrapper,
            phaseSHA256: try identitySHA256(root, "phase_sha256"),
            recordedAtMilliseconds: try identityInt(root, "recorded_at_ms"),
            finalized: finalized)
    }

    private static func decodeMachineIdentityRotation(
        _ data: Data, expectedMachineID: String
    ) throws -> CloudMachineIdentityRotationReceipt {
        let root = try identityObject(try CloudCanonicalJSON.parseStrict(data), keys: [
            "v", "status", "device_id", "identity_epoch", "key_epoch", "capability_epoch",
            "key_fingerprint", "old_accept_until_ms", "new_accept_from_ms"
        ])
        guard try identityInt(root, "v") == 1,
              let status = CloudMachineIdentityRotationReceipt.Status(
                rawValue: try identityString(root, "status")),
              try identityString(root, "device_id") == expectedMachineID else {
            throw CloudAccountError.invalidResponse
        }
        return CloudMachineIdentityRotationReceipt(
            status: status, deviceID: expectedMachineID,
            identityEpoch: try identityInt(root, "identity_epoch"),
            keyEpoch: try identityInt(root, "key_epoch"),
            capabilityEpoch: try identityInt(root, "capability_epoch"),
            keyFingerprint: try identityString(root, "key_fingerprint"),
            oldAcceptUntilMilliseconds: try identityInt(root, "old_accept_until_ms"),
            newAcceptFromMilliseconds: try identityInt(root, "new_accept_from_ms"))
    }
}

private struct DeviceStartRequest: Encodable {
    let name: String
    let platform: String
    let appVersion: String?
    let publicKey: String

    enum CodingKeys: String, CodingKey {
        case name, platform
        case appVersion = "app_version"
        case publicKey = "public_key"
    }

    func encode(to encoder: Encoder) throws {
        var values = encoder.container(keyedBy: CodingKeys.self)
        try values.encode(name, forKey: .name)
        try values.encode(platform, forKey: .platform)
        if let appVersion { try values.encode(appVersion, forKey: .appVersion) }
        else { try values.encodeNil(forKey: .appVersion) }
        try values.encode(publicKey, forKey: .publicKey)
    }
}

private struct DeviceStartResponse: Decodable {
    let deviceCode: String
    let userCode: String
    let verificationURI: String
    let verificationURIComplete: String
    let expiresIn: Int
    let interval: Int
    enum CodingKeys: String, CodingKey {
        case deviceCode = "device_code"
        case userCode = "user_code"
        case verificationURI = "verification_uri"
        case verificationURIComplete = "verification_uri_complete"
        case expiresIn = "expires_in"
        case interval
    }
}

private struct DevicePollRequest: Encodable {
    let deviceCode: String
    enum CodingKeys: String, CodingKey { case deviceCode = "device_code" }
}

private struct HeartbeatRequest: Encodable {
    let appVersion: String?
    enum CodingKeys: String, CodingKey { case appVersion = "app_version" }
}

private struct HeartbeatResponse: Decodable { let ok: Bool; let at: String }

private struct ScheduleWebhookActivateRequest: Encodable {
    let expectedRevision: Int
    enum CodingKeys: String, CodingKey { case expectedRevision = "expected_revision" }
}

private struct ScheduleWebhookClaimRequest: Encodable {
    let protocolName: String
    let waitSeconds: Int
    enum CodingKeys: String, CodingKey {
        case protocolName = "protocol"
        case waitSeconds = "wait_seconds"
    }
}

private struct PairingStartRequest: Encodable { let fingerprint: String }

private struct PairingStartResponse: Decodable {
    let pairingID: String
    let claimNonce: String
    let expiresAt: String
    let expiresIn: Int
    enum CodingKeys: String, CodingKey {
        case pairingID = "pairing_id"
        case claimNonce = "claim_nonce"
        case expiresAt = "expires_at"
        case expiresIn = "expires_in"
    }
}

private struct PairingCompleteRequest: Encodable {
    let pairingID: String
    let ciphertext: String
    enum CodingKeys: String, CodingKey { case pairingID = "pairing_id"; case ciphertext }
}

private struct PairingCompleteResponse: Decodable { let status: String; let fingerprint: String }

private struct PairingClaimRequest: Encodable {
    let pairingID: String
    let claimNonce: String
    enum CodingKeys: String, CodingKey {
        case pairingID = "pairing_id"
        case claimNonce = "claim_nonce"
    }
}

private struct PairingClaimResponse: Decodable {
    let ciphertext: String
    let senderDeviceID: String?
    enum CodingKeys: String, CodingKey {
        case ciphertext
        case senderDeviceID = "sender_device_id"
    }
}

private struct PairingInvitationStartRequest: Encodable {
    let secretHash: String
    enum CodingKeys: String, CodingKey { case secretHash = "secret_hash" }
}

private struct PairingInvitationStartResponse: Decodable {
    let status: String
    let invitationID: String
    let expiresAt: String
    let expiresIn: Int
    enum CodingKeys: String, CodingKey {
        case status
        case invitationID = "invitation_id"
        case expiresAt = "expires_at"
        case expiresIn = "expires_in"
    }
}

private struct PairingInvitationPollRequest: Encodable {
    let invitationID: String
    enum CodingKeys: String, CodingKey { case invitationID = "invitation_id" }
}

private struct PairingInvitationPendingResponse: Decodable { let status: String }

private struct PairingInvitationReadyResponse: Decodable {
    let status: String
    let accountID: String
    let viewerDeviceID: String
    let machineID: String
    let encryptedOffer: String
    enum CodingKeys: String, CodingKey {
        case status
        case accountID = "account_id"
        case viewerDeviceID = "viewer_device_id"
        case machineID = "machine_id"
        case encryptedOffer = "encrypted_offer"
    }
}

private struct RevocationResponse: Decodable {
    let revokedAt: String
    let routing: String
    let contentKeyRotation: String
    enum CodingKeys: String, CodingKey {
        case revokedAt = "revoked_at"
        case routing
        case contentKeyRotation = "content_key_rotation"
    }
}

private func decode<T: Decodable>(_ data: Data) throws -> T {
    do { return try JSONDecoder().decode(T.self, from: data) }
    catch { throw CloudAccountError.invalidResponse }
}

private func jsonObject(_ data: Data) throws -> [String: Any] {
    guard let value = try? JSONSerialization.jsonObject(with: data),
          let object = value as? [String: Any] else {
        throw CloudAccountError.invalidResponse
    }
    return object
}

private func requireObject(_ data: Data, keys: Set<String>) throws {
    try requireKeys(jsonObject(data), exactly: keys)
}

private func requireKeys(_ object: [String: Any], exactly keys: Set<String>) throws {
    guard Set(object.keys) == keys else { throw CloudAccountError.invalidResponse }
}

private func nonemptyString(_ value: Any?) -> String? {
    guard let string = value as? String, !string.isEmpty else { return nil }
    return string
}

private func strictInt(_ value: Any?) -> Int? {
    guard let number = value as? NSNumber,
          CFGetTypeID(number) != CFBooleanGetTypeID() else { return nil }
    return Int(number.stringValue)
}

@usableFromInline
internal let cloudSleepNanosecondsPerSecond: UInt64 = 1_000_000_000
@usableFromInline
internal let cloudMaximumSleepSeconds = UInt64.max / cloudSleepNanosecondsPerSecond

private func isSchedulableSleepSeconds(_ seconds: Int) -> Bool {
    guard let unsigned = UInt64(exactly: seconds) else { return false }
    return unsigned > 0 && unsigned <= cloudMaximumSleepSeconds
}

@usableFromInline
internal func checkedSleepNanoseconds(_ seconds: TimeInterval) -> UInt64? {
    guard seconds.isFinite, seconds > 0,
          seconds <= TimeInterval(cloudMaximumSleepSeconds) else { return nil }
    return UInt64(seconds * TimeInterval(cloudSleepNanosecondsPerSecond))
}

private func parseDate(_ value: String) -> Date? {
    let fractional = ISO8601DateFormatter()
    fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return fractional.date(from: value) ?? ISO8601DateFormatter().date(from: value)
}

private func decodeMachine(_ object: [String: Any]) throws -> CloudMachine {
    try requireKeys(object, exactly: [
        "id", "name", "platform", "app_version", "public_key", "last_seen_at",
        "created_at", "revoked_at",
    ])
    guard let id = nonemptyString(object["id"]), let name = nonemptyString(object["name"]),
          let platform = nonemptyString(object["platform"]),
          let publicKey = nonemptyString(object["public_key"]),
          let rawKey = Data(base64Encoded: publicKey), rawKey.count == 32,
          let createdString = nonemptyString(object["created_at"]),
          let createdAt = parseDate(createdString) else {
        throw CloudAccountError.invalidResponse
    }
    let appVersion = try optionalString(object, key: "app_version")
    let lastSeenAt = try optionalDate(object, key: "last_seen_at")
    let revokedAt = try optionalDate(object, key: "revoked_at")
    return CloudMachine(
        id: id, name: name, platform: platform, appVersion: appVersion,
        publicKey: publicKey, lastSeenAt: lastSeenAt, createdAt: createdAt,
        revokedAt: revokedAt)
}

private func decodeDevice(_ object: [String: Any]) throws -> CloudViewerDevice {
    try requireKeys(object, exactly: [
        "id", "kind", "name", "caps", "public_key", "last_seen_at", "created_at", "revoked_at",
    ])
    guard let id = nonemptyString(object["id"]), let name = nonemptyString(object["name"]),
          let kindString = nonemptyString(object["kind"]), let kind = CloudDeviceKind(rawValue: kindString),
          let publicKey = nonemptyString(object["public_key"]),
          let rawKey = Data(base64Encoded: publicKey), rawKey.count == 32,
          let rawCaps = object["caps"] as? [String],
          let createdString = nonemptyString(object["created_at"]),
          let createdAt = parseDate(createdString) else {
        throw CloudAccountError.invalidResponse
    }
    let capabilities = try rawCaps.map { value -> CloudDeviceCapability in
        guard let capability = CloudDeviceCapability(rawValue: value) else {
            throw CloudAccountError.invalidResponse
        }
        return capability
    }
    guard Set(capabilities.map(\.rawValue)).count == capabilities.count else {
        throw CloudAccountError.invalidResponse
    }
    return CloudViewerDevice(
        id: id, kind: kind, name: name, capabilities: capabilities, publicKey: publicKey,
        lastSeenAt: try optionalDate(object, key: "last_seen_at"), createdAt: createdAt,
        revokedAt: try optionalDate(object, key: "revoked_at"))
}

private func optionalString(_ object: [String: Any], key: String) throws -> String? {
    let value = object[key]
    if value is NSNull { return nil }
    guard let string = value as? String else { throw CloudAccountError.invalidResponse }
    return string
}

private func optionalDate(_ object: [String: Any], key: String) throws -> Date? {
    guard let string = try optionalString(object, key: key) else { return nil }
    guard let date = parseDate(string) else { throw CloudAccountError.invalidResponse }
    return date
}

private func errorCode(_ data: Data) throws -> String? {
    let object = try jsonObject(data)
    try requireKeys(object, exactly: ["error"])
    guard let error = object["error"] as? [String: Any],
          Set(error.keys).isSubset(of: ["code", "message", "details"]),
          let code = nonemptyString(error["code"]),
          nonemptyString(error["message"]) != nil else {
        throw CloudAccountError.invalidResponse
    }
    return code
}
