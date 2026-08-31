// clawdline.com control-plane client. This file owns device-code login and the
// long-lived machine credential; CloudTransport owns short-lived relay tokens.

import Foundation
import os

enum CloudAccountError: Error, LocalizedError, Equatable {
    case missingMachineCredential
    case invalidResponse
    case http(status: Int, code: String?)
    case invalidPairingBlob
    case loginAbandoned
    case credentialInvalidationFailed(String)
    case credentialCleanupPending(String)

    var errorDescription: String? {
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
struct CloudCredentialGeneration: Equatable, Sendable {
    fileprivate let value: UInt64

    init(_ value: UInt64) { self.value = value }
}

/// Durable, nonsecret invalidation state. A Keychain item whose embedded epoch is older than
/// this value is unusable even when `SecItemDelete` failed, including after process restart.
protocol CloudCredentialInvalidationStoring: Sendable {
    /// Raise the process-local floor synchronously. This is atomic-only and must not touch a
    /// persistence API: Settings calls it before cancelling the in-flight login Task.
    func reserve(atLeast epoch: UInt64) -> UInt64
    func currentEpoch() throws -> UInt64
    func advance(to epoch: UInt64) throws
}

final class CloudInMemoryCredentialInvalidationStore:
    CloudCredentialInvalidationStoring, @unchecked Sendable
{
    private let epoch: OSAllocatedUnfairLock<UInt64>

    init(epoch: UInt64 = 0) {
        self.epoch = OSAllocatedUnfairLock(initialState: epoch)
    }

    func reserve(atLeast wanted: UInt64) -> UInt64 {
        epoch.withLock { current in
            current = max(current, wanted)
            return current
        }
    }

    func currentEpoch() throws -> UInt64 { epoch.withLock { $0 } }

    func advance(to wanted: UInt64) throws {
        epoch.withLock { current in current = max(current, wanted) }
    }
}

/// Production persistence for the nonsecret epoch. UserDefaults is intentionally separate from
/// the Keychain: an unavailable or locked Keychain must not be able to roll invalidation back.
final class CloudCredentialInvalidationDefaultsStore:
    CloudCredentialInvalidationStoring, @unchecked Sendable
{
    static let defaultKey = "cloud.machine-credential.minimum-valid-epoch"

    private final class ProcessCoordinator: @unchecked Sendable {
        let lock = NSLock()
        var floor: UInt64 = 0
    }

    /// UserDefaults has no compare-and-swap operation. Instances addressing the same durable
    /// subject share this read/reserve/write coordinator, so a low writer cannot overtake a high
    /// reservation. The namespace is explicit because a defaults key alone does not identify a
    /// domain; test suites and production must never share a process floor by spelling accident.
    private static let coordinators = OSAllocatedUnfairLock(
        initialState: [String: ProcessCoordinator]())

    private let defaults: UserDefaults
    private let key: String
    private let coordinator: ProcessCoordinator

    init(
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

    func reserve(atLeast wanted: UInt64) -> UInt64 {
        coordinator.lock.lock()
        defer { coordinator.lock.unlock() }
        coordinator.floor = max(coordinator.floor, wanted)
        return coordinator.floor
    }

    func currentEpoch() throws -> UInt64 {
        coordinator.lock.lock()
        defer { coordinator.lock.unlock() }
        let durable = (defaults.object(forKey: key) as? NSNumber)?.uint64Value ?? 0
        coordinator.floor = max(coordinator.floor, durable)
        return coordinator.floor
    }

    func advance(to wanted: UInt64) throws {
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

protocol CloudAccountHTTPTransport: Sendable {
    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse)
}

struct CloudAccountURLSessionTransport: CloudAccountHTTPTransport, Sendable {
    let session: URLSession

    init(session: URLSession = .shared) { self.session = session }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw CloudAccountError.invalidResponse
        }
        return (data, http)
    }
}

struct CloudMachineMetadata: Equatable, Sendable {
    let name: String
    let platform: String
    let appVersion: String?

    init(name: String, platform: String, appVersion: String? = nil) {
        self.name = name
        self.platform = platform
        self.appVersion = appVersion
    }
}

struct CloudDeviceLoginStart: Equatable, Sendable,
                              CustomStringConvertible, CustomDebugStringConvertible {
    let deviceCode: String
    let userCode: String
    let verificationURL: URL
    let verificationCompleteURL: URL
    let expiresIn: Int
    let interval: Int
    fileprivate let receivedAt: TimeInterval
    /// Captured when the flow opened, so every poll it later makes is judged against the state
    /// of the world *before* the person could have signed out.
    fileprivate let generation: CloudCredentialGeneration

    var description: String {
        "CloudDeviceLoginStart(deviceCode: <redacted>, userCode: \(userCode), interval: \(interval))"
    }

    var debugDescription: String { description }
}

struct CloudMachineIdentity: Equatable, Sendable {
    let accountID: String
    let machineID: String
}

enum CloudDeviceLoginPollState: Equatable, Sendable {
    case authorizationPending
    case slowDown(retryAfter: Int)
    case accessDenied
    case expired
    case complete(CloudMachineIdentity)

    var retryAfter: Int? {
        switch self {
        case .slowDown(let seconds): return seconds
        default: return nil
        }
    }
}

struct CloudMachineCredential: Equatable, Codable, Sendable,
                               CustomStringConvertible, CustomDebugStringConvertible {
    let accountID: String
    let machineID: String
    fileprivate let secret: String
    fileprivate let validityEpoch: UInt64

    init(accountID: String, machineID: String, secret: String, validityEpoch: UInt64 = 0) {
        self.accountID = accountID
        self.machineID = machineID
        self.secret = secret
        self.validityEpoch = validityEpoch
    }

    var description: String {
        "CloudMachineCredential(accountID: \(accountID), machineID: \(machineID), secret: <redacted>)"
    }

    var debugDescription: String { description }

    enum CodingKeys: String, CodingKey {
        case accountID = "account_id"
        case machineID = "machine_id"
        case secret = "machine_credential"
        case validityEpoch = "validity_epoch"
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        accountID = try values.decode(String.self, forKey: .accountID)
        machineID = try values.decode(String.self, forKey: .machineID)
        secret = try values.decode(String.self, forKey: .secret)
        // Credentials written before the invariant existed belong to the original epoch.
        validityEpoch = try values.decodeIfPresent(UInt64.self, forKey: .validityEpoch) ?? 0
    }

    func encode(to encoder: Encoder) throws {
        var values = encoder.container(keyedBy: CodingKeys.self)
        try values.encode(accountID, forKey: .accountID)
        try values.encode(machineID, forKey: .machineID)
        try values.encode(secret, forKey: .secret)
        try values.encode(validityEpoch, forKey: .validityEpoch)
    }
}

struct CloudMachine: Equatable, Sendable {
    let id: String
    let name: String
    let platform: String
    let appVersion: String?
    let publicKey: String
    let lastSeenAt: Date?
    let createdAt: Date
    let revokedAt: Date?
}

struct CloudMachineList: Equatable, Sendable {
    let machines: [CloudMachine]
    let active: Int
}

enum CloudDeviceKind: String, Codable, Sendable {
    case browser
    case ios
    case android
}

enum CloudDeviceCapability: String, Codable, Sendable {
    case readSessions = "read_sessions"
    case readTranscript = "read_transcript"
    case sendPrompt = "send_prompt"
    case startSession = "start_session"
}

struct CloudViewerDevice: Equatable, Sendable {
    let id: String
    let kind: CloudDeviceKind
    let name: String
    let capabilities: [CloudDeviceCapability]
    let publicKey: String
    let lastSeenAt: Date?
    let createdAt: Date
    let revokedAt: Date?
}

struct CloudDeviceList: Equatable, Sendable {
    let devices: [CloudViewerDevice]
    let active: Int
}

struct CloudHeartbeat: Equatable, Sendable {
    let at: Date
}

struct CloudRevocation: Equatable, Sendable {
    let revokedAt: Date
    let routing: String
    let contentKeyRotation: String
}

/// The server and this client treat the payload only as base64 bytes. Its description is
/// deliberately redacted so diagnostics cannot accidentally reveal a key handover.
struct CloudOpaquePairingBlob: Equatable, Sendable,
                               CustomStringConvertible, CustomDebugStringConvertible {
    fileprivate let base64: String

    init(base64: String) throws {
        guard let decoded = Data(base64Encoded: base64),
              decoded.base64EncodedString() == base64 else {
            throw CloudAccountError.invalidPairingBlob
        }
        self.base64 = base64
    }

    var description: String { "CloudOpaquePairingBlob(<redacted>)" }
    var debugDescription: String { description }

    /// The exact bytes the wire carries. Named so that reading them is a deliberate act: the
    /// only caller is the suite, proving that what went to `POST /v1/pairing/complete` is what
    /// a viewer can open.
    var wireBase64ForTesting: String { base64 }

    /// The opaque bytes at the transport/cryptography seam. Callers still cannot inspect them
    /// through logs or descriptions; invitation decryption is the second legitimate consumer.
    var wireBase64: String { base64 }
}

struct CloudPairingStart: Equatable, Sendable,
                          CustomStringConvertible, CustomDebugStringConvertible {
    let pairingID: String
    let claimNonce: String
    let expiresAt: Date
    let expiresIn: Int

    var description: String {
        "CloudPairingStart(pairingID: \(pairingID), claimNonce: <redacted>, expiresIn: \(expiresIn))"
    }

    var debugDescription: String { description }
}

struct CloudPairingDelivery: Equatable, Sendable {
    let fingerprint: String
}

enum CloudPairingClaim: Equatable, Sendable {
    case pending
    case complete(blob: CloudOpaquePairingBlob, senderDeviceID: String?)
}

struct CloudPairingInvitationStart: Equatable, Sendable {
    let invitationID: String
    let expiresAt: Date
    let expiresIn: Int
}

enum CloudPairingInvitationPoll: Equatable, Sendable {
    case pending
    case ready(
        accountID: String, viewerDeviceID: String, machineID: String,
        encryptedOffer: CloudOpaquePairingBlob)
}

/// Client-side QR key derivation is intentionally outside this transport seam. A later UX
/// component must generate that material and decide what bytes become `ciphertext`.
protocol CloudPairingCryptographyProviding: Sendable {
    func makeOpaqueHandover() async throws -> CloudOpaquePairingBlob
    func openOpaqueHandover(_ blob: CloudOpaquePairingBlob) async throws
}

final class CloudAccountClient: Sendable {
    typealias DeviceKeyLoader = @Sendable () throws -> CloudDeviceKeyPair
    typealias Sleeper = @Sendable (TimeInterval) async throws -> Void
    typealias Clock = @Sendable () -> TimeInterval

    static let machineCredentialAccount = "machine-credential-v1"

    let apiBaseURL: URL
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
    private let loginGeneration: OSAllocatedUnfairLock<UInt64>

    init(
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
        loginGeneration = OSAllocatedUnfairLock(
            initialState: (try? invalidationStore.currentEpoch()) ?? 0)
    }

    convenience init(
        apiBaseURL: URL = URL(string: "https://api.clawdline.com")!,
        session: URLSession = .shared,
        credentialStore: any CloudKeyStoring = CloudKeychainStore(),
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

    /// The generation a credential must still belong to for the store to accept it.
    func credentialGeneration() -> CloudCredentialGeneration {
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
    func reservePendingLoginInvalidation() -> CloudCredentialGeneration {
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
    func persistPendingLoginInvalidation(_ generation: CloudCredentialGeneration) throws {
        do {
            try invalidationStore.advance(to: generation.value)
        } catch let error as CloudAccountError {
            throw error
        } catch {
            throw CloudAccountError.credentialInvalidationFailed(Self.message(for: error))
        }
    }

    @discardableResult
    func invalidatePendingLogins() throws -> CloudCredentialGeneration {
        let generation = reservePendingLoginInvalidation()
        try persistPendingLoginInvalidation(generation)
        return generation
    }

    func startDeviceLogin(metadata: CloudMachineMetadata) async throws -> CloudDeviceLoginStart {
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
    func pollDeviceLogin(
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
    func waitForDeviceLogin(
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

    func restoredMachineIdentity() throws -> CloudMachineIdentity? {
        try withPersistenceTransaction {
            guard let credential = try loadCredentialUnlocked() else { return nil }
            return CloudMachineIdentity(
                accountID: credential.accountID, machineID: credential.machineID)
        }
    }

    func authorizationHeader() throws -> String {
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
    func signOut() throws {
        let invalidation = reservePendingLoginInvalidation()
        try signOut(reservedAt: invalidation)
    }

    func signOut(reservedAt invalidation: CloudCredentialGeneration) throws {
        try persistPendingLoginInvalidation(invalidation)
        do {
            try withPersistenceTransaction {
                try credentialStore.remove(Self.machineCredentialAccount)
            }
        } catch {
            throw CloudAccountError.credentialCleanupPending(Self.message(for: error))
        }
    }

    func authorizationHeaderProvider() -> CloudAPIDeviceTokenProvider.AuthorizationHeaderProvider {
        { [weak self] in
            guard let self else { throw CloudAccountError.missingMachineCredential }
            return try self.authorizationHeader()
        }
    }

    func deviceTokenProvider(session: URLSession = .shared) -> CloudAPIDeviceTokenProvider {
        CloudAPIDeviceTokenProvider(
            apiBaseURL: apiBaseURL,
            session: session,
            authorizationHeader: authorizationHeaderProvider()
        )
    }

    func heartbeat(appVersion: String? = nil) async throws -> CloudHeartbeat {
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

    func listMachines() async throws -> CloudMachineList {
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

    func listDevices() async throws -> CloudDeviceList {
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
    func revokeMachine(id: String, browserSessionCookie: String) async throws -> CloudRevocation {
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
    func revokeDevice(id: String, browserSessionCookie: String) async throws -> CloudRevocation {
        let data = try await send(
            method: "DELETE", path: ["v1", "devices", id],
            authorization: .browserSessionCookie(browserSessionCookie)
        )
        return try decodeRevocation(data, machine: false)
    }

    func startPairing(fingerprint: String) async throws -> CloudPairingStart {
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

    func completePairing(pairingID: String, blob: CloudOpaquePairingBlob) async throws -> CloudPairingDelivery {
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

    func claimPairing(pairingID: String, claimNonce: String) async throws -> CloudPairingClaim {
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

    func startPairingInvitation(secretHash: Data) async throws -> CloudPairingInvitationStart {
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

    func pollPairingInvitation(invitationID: String) async throws -> CloudPairingInvitationPoll {
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
        authorization: Authorization?, encodeNilBody: Bool = true
    ) async throws -> RawResult {
        let url = try endpoint(path)
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if encodeNilBody {
            request.httpBody = try JSONEncoder().encode(body)
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
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

private let cloudSleepNanosecondsPerSecond: UInt64 = 1_000_000_000
private let cloudMaximumSleepSeconds = UInt64.max / cloudSleepNanosecondsPerSecond

private func isSchedulableSleepSeconds(_ seconds: Int) -> Bool {
    guard let unsigned = UInt64(exactly: seconds) else { return false }
    return unsigned > 0 && unsigned <= cloudMaximumSleepSeconds
}

private func checkedSleepNanoseconds(_ seconds: TimeInterval) -> UInt64? {
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
