import Foundation
import CryptoKit
import Darwin
#if canImport(ClawdlineApplication)
import ClawdlineApplication // W3-1 correction: real cross-module import, see Sources/HostPorts.swift
#endif

#if SWIFT_PACKAGE && canImport(ClawdlineApplication)
/// Keeps the existing Mac `SecretStore` conformance in `MacHostAdapters.swift` source-compatible
/// after CloudKeys moved inward. Every operation delegates to Application's store, including its
/// process-wide coordinator; this is a composition alias, not a second Keychain owner.
final class CloudKeychainStore {
    private let applicationStore: ClawdlineApplication.CloudProtectedKeychainStore

    init(service: String = ClawdlineApplication.CloudProtectedKeychainStore.defaultService) {
        applicationStore = ClawdlineApplication.CloudProtectedKeychainStore(service: service)
    }

    var coordinator: CloudKeyStoreCoordinator { applicationStore.coordinator }
    func data(for account: String) throws -> Data? { try applicationStore.data(for: account) }
    func set(_ data: Data, for account: String) throws {
        try applicationStore.set(data, for: account)
    }
    func remove(_ account: String) throws { try applicationStore.remove(account) }
}
#endif

/// Compatibility key handover between a browser viewer and this Mac. The normative W5-2 path is
/// `CloudIdentityPairingMachineHandover` below; this three-call adapter remains only so an older
/// console can finish a pairing it started before the identity-route cutover.
///
/// **Why this is not `CloudPairingQR`.** `CloudPairing` carries a four-phase handover
/// (`offer`, `grant`, `activate`, `confirm`) whose wire form is "the complete request body of
/// the four phase-write APIs" — see the comment at `CloudPairing.encodePhaseWriteBody`. The
/// control plane has no such routes: `api/src/routes/pairing.ts` exposes exactly
/// `start`, `complete` and `claim`, and `api/src/services/pairing.ts` refuses a second
/// `complete` with `already_completed`, so the account has **one** ciphertext slot and it can
/// be written once. `CloudAccountClient.startPairing/completePairing/claimPairing` already
/// speak that three-call shape, and `CloudPairingCryptographyProviding` already asks for one
/// opaque blob rather than four. This file is the single-blob half, built out of
/// `CloudPairing`'s own KDF, wrapper, AAD and canonical-JSON primitives; nothing here invents
/// cryptography.
///
/// **Direction, and why it is this way round.** The blob the API can carry travels from the
/// *sender* (`complete`) to the *requester* (`start` then `claim`), and the master secret has
/// to travel Mac → viewer. So the **viewer** is the requester: it asks the control plane for a
/// `pairing_id` and a one-time `claim_nonce`, then returns that offer to the Mac through the
/// Mac-displayed ``CloudPairingInvitation``. The QR secret encrypts the offer before Cloud sees
/// it, so the human carries only one short-lived QR scan; the Mac seals the account key material
/// for exactly that offer and writes it into the one slot; the viewer claims it once and the
/// record is destroyed.
///
/// The property PROTOCOL §3 asks for survives: an attacker holding the OAuth session can
/// register a viewer device and call `start`, but no Mac ever seals for a `pairing_id` that
/// was not encrypted by the QR on its own screen, so the attacker's device gets no master secret and can
/// produce no command any Mac will accept. Both sides pin the other's Ed25519 key from the
/// out-of-band material rather than from anything the cloud says.

/// A viewer this Mac has pinned. Written only by a completed handover, read as the sole
/// authority for `ctl` sender verification.
struct CloudPairedDevice: Equatable, Sendable {
    let deviceID: String
    /// Raw 32-byte Ed25519 public key, exactly what `CloudTransportKeyProviding` hands back.
    let signingKey: Data
    let pairedAtMilliseconds: Int64
}

/// The pinned viewer devices, on disk beside the rest of this Mac's remote state.
///
/// Account-scoped on purpose: signing this Mac into a different Cloud account must not inherit
/// the previous account's pinned viewers, so a stored account id that does not match the
/// current one makes the whole file a fresh, empty one rather than something to merge.
final class CloudPairedDeviceStore: @unchecked Sendable {
    private let url: URL
    private var migrationURL: URL { url.appendingPathExtension("w5-protected-migration") }
    private let lock = NSLock()

    init(url: URL) {
        self.url = url
    }

    convenience init() {
        self.init(url: RemoteAuth.directory.appendingPathComponent("cloud-devices.json"))
    }

    func devices(accountID: String) throws -> [CloudPairedDevice] {
        lock.lock()
        defer { lock.unlock() }
        return try loadUnlocked(accountID: accountID).devices
    }

    /// Move the old authority out of the pathname a pre-W5 binary understands before importing
    /// it. If protected persistence then fails, the fence remains readable to this version while
    /// an old binary sees no roster to revive. The rename and parent directory are durable before
    /// any protected write begins.
    func beginProtectedMigration(accountID: String) throws -> [CloudPairedDevice] {
        lock.lock()
        defer { lock.unlock() }
        let manager = FileManager.default
        let hasLive = manager.fileExists(atPath: url.path)
        let hasFence = manager.fileExists(atPath: migrationURL.path)
        guard !(hasLive && hasFence) else { throw CloudHandoverError.storeUnreadable }
        if hasLive {
            do {
                try manager.moveItem(at: url, to: migrationURL)
                try syncParentDirectory()
            } catch {
                throw CloudHandoverError.storeUnwritable
            }
        }
        guard manager.fileExists(atPath: migrationURL.path) else { return [] }
        return try loadUnlocked(accountID: accountID, from: migrationURL).devices
    }

    func finishProtectedMigration() throws {
        lock.lock()
        defer { lock.unlock() }
        try removeIfPresent(migrationURL)
    }

    /// Pinning replaces any earlier pin for the same device id: re-pairing a browser that
    /// cleared its IndexedDB is an ordinary thing to do, and two rows for one id would leave
    /// the verifier choosing between them.
    func pin(_ device: CloudPairedDevice, accountID: String) throws {
        lock.lock()
        defer { lock.unlock() }
        var current = try loadUnlocked(accountID: accountID).devices
        current.removeAll { $0.deviceID == device.deviceID }
        current.append(device)
        try writeUnlocked(devices: current, accountID: accountID)
    }

    func forget(deviceID: String, accountID: String) throws {
        lock.lock()
        defer { lock.unlock() }
        var current = try loadUnlocked(accountID: accountID).devices
        let before = current.count
        current.removeAll { $0.deviceID == deviceID }
        guard current.count != before else { return }
        try writeUnlocked(devices: current, accountID: accountID)
    }

    func removeAll() throws {
        lock.lock()
        defer { lock.unlock() }
        try removeIfPresent(url)
        try removeIfPresent(migrationURL)
    }

    private func loadUnlocked(accountID: String) throws -> (devices: [CloudPairedDevice], stored: String?) {
        try loadUnlocked(accountID: accountID, from: url)
    }

    private func loadUnlocked(accountID: String, from sourceURL: URL)
        throws -> (devices: [CloudPairedDevice], stored: String?) {
        guard FileManager.default.fileExists(atPath: sourceURL.path) else { return ([], nil) }
        guard let data = try? Data(contentsOf: sourceURL) else {
            throw CloudHandoverError.storeUnreadable
        }
        guard let value = try? CloudCanonicalJSON.parseStrict(data),
              case .object(let root) = value,
              case .int(let version)? = root["v"], version == 1,
              case .string(let storedAccount)? = root["account_id"],
              case .array(let rows)? = root["devices"]
        else {
            throw CloudHandoverError.storeUnreadable
        }
        // A different account is not a corruption and must not be reported as one; it is simply
        // nothing this account has pinned.
        guard storedAccount == accountID else { return ([], storedAccount) }
        var devices: [CloudPairedDevice] = []
        for row in rows {
            guard case .object(let fields) = row,
                  case .string(let deviceID)? = fields["device_id"],
                  case .string(let signingKey)? = fields["signing_key"],
                  case .int(let pairedAt)? = fields["paired_at"],
                  let raw = try? CloudPairing.decodeCanonicalBase64(
                      signingKey, field: "signing_key", expectedLength: 32)
            else {
                throw CloudHandoverError.storeUnreadable
            }
            devices.append(CloudPairedDevice(
                deviceID: deviceID, signingKey: raw, pairedAtMilliseconds: pairedAt))
        }
        return (devices, storedAccount)
    }

    private func removeIfPresent(_ candidate: URL) throws {
        do {
            try FileManager.default.removeItem(at: candidate)
            try syncParentDirectory()
        } catch CocoaError.fileNoSuchFile {
            return
        } catch let error as NSError where error.domain == NSCocoaErrorDomain
                    && error.code == NSFileNoSuchFileError {
            return
        } catch {
            throw CloudHandoverError.storeUnwritable
        }
    }

    private func syncParentDirectory() throws {
        let descriptor = open(url.deletingLastPathComponent().path, O_RDONLY | O_DIRECTORY)
        guard descriptor >= 0 else { throw CloudHandoverError.storeUnwritable }
        defer { close(descriptor) }
        guard fsync(descriptor) == 0 else { throw CloudHandoverError.storeUnwritable }
    }

    private func writeUnlocked(devices: [CloudPairedDevice], accountID: String) throws {
        let rows = devices.map { device in
            CloudJSONValue.object([
                "device_id": .string(device.deviceID),
                "signing_key": .string(device.signingKey.base64EncodedString()),
                "paired_at": .int(device.pairedAtMilliseconds),
            ])
        }
        let body = CloudCanonicalJSON.canonicalData(.object([
            "v": .int(1),
            "account_id": .string(accountID),
            "devices": .array(rows),
        ]))
        do {
            try FileManager.default.createDirectory(
                at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            try body.write(to: url, options: [.atomic])
            // The pinned list decides which sender may drive this Mac. It is readable only by
            // its owner for the same reason `remote.json` is.
            try FileManager.default.setAttributes(
                [.posixPermissions: 0o600], ofItemAtPath: url.path)
        } catch {
            throw CloudHandoverError.storeUnwritable
        }
    }
}

/// What the Mac seals for exactly one offer. The viewer needs the account's content key and
/// the sender key it will verify every snapshot against, and nothing else.
struct CloudPairingHandover: Equatable, Sendable {
    var accountID: String
    var machineID: String
    var machineSigningKey: String
    var machineFingerprint: String
    var keyID: String
    var masterSecret: String

    var cloudJSONValue: CloudJSONValue {
        .object([
            "v": .int(1),
            "type": .string("pairing_handover"),
            "account_id": .string(accountID),
            "machine_id": .string(machineID),
            "machine_signing_key": .string(machineSigningKey),
            "machine_fingerprint": .string(machineFingerprint),
            "key_id": .string(keyID),
            "master_secret": .string(masterSecret),
        ])
    }
}

enum CloudHandover {
    /// The same window `CloudPairing` gives its QR. An offer is carried across a room, not
    /// kept, and a long-lived one is a claim nonce sitting on a screen.
    static let offerLifetimeMilliseconds = CloudCompatibilityPairing.offerLifetimeMilliseconds
    private static let handoverMembers: Set<String> = [
        "v", "type", "account_id", "machine_id", "machine_signing_key",
        "machine_fingerprint", "key_id", "master_secret",
    ]

    static func encodeOfferFragment(_ offer: CloudPairingOffer, nowMilliseconds: Int64) throws -> String {
        try CloudCompatibilityPairing.encodeOfferFragment(
            offer, nowMilliseconds: nowMilliseconds)
    }

    /// Serialization without validation, so a rejection test can build a fragment the decoder
    /// must refuse. Receivers always use `decodeOfferFragment`.
    static func encodeOfferFragmentUnchecked(_ offer: CloudPairingOffer) -> String {
        CloudCompatibilityPairing.encodeOfferFragmentUnchecked(offer)
    }

    static func decodeOfferFragment(
        _ fragment: String, nowMilliseconds: Int64
    ) throws -> CloudPairingOffer {
        try CloudCompatibilityPairing.decodeOfferFragment(
            fragment, nowMilliseconds: nowMilliseconds)
    }

    /// The machine half. Derives the phase key from the offer, then seals the account key
    /// material into the one ciphertext slot `POST /v1/pairing/complete` will carry.
    ///
    /// `grant` rather than any other phase because `CloudPairing.validateKeyBinding` binds
    /// `grant` and `confirm` to the *machine's* ephemeral key, which is what this wrapper
    /// carries; using `offer` or `activate` here would claim the wrapper was bound to the
    /// viewer's key while carrying the machine's.
    static func seal(
        _ handover: CloudPairingHandover,
        for offer: CloudPairingOffer,
        machineDeviceID: String,
        machineEphemeralPrivateKey: Data,
        nonce: Data,
        nowMilliseconds: Int64
    ) throws -> CloudPairingWrapper {
        try CloudCompatibilityPairing.validate(offer, nowMilliseconds: nowMilliseconds)
        guard handover.accountID == offer.accountID else {
            throw CloudHandoverError.wrongAccount
        }
        try validate(handover)
        let phaseKey = try phaseKey(
            offer: offer,
            privateKeyRaw: machineEphemeralPrivateKey,
            peerPublicKeyRaw: try CloudPairing.decodeCanonicalBase64(
                offer.viewerEphemeralKey, field: "viewer_ephemeral_key", expectedLength: 32))
        let ephemeralPublic = try x25519PublicKey(forPrivateKeyRaw: machineEphemeralPrivateKey)
        return try CloudPairing.seal(
            plaintext: CloudCanonicalJSON.canonicalData(handover.cloudJSONValue),
            phase: .grant,
            pairingID: offer.pairingID,
            senderDeviceID: machineDeviceID,
            ephemeralKey: ephemeralPublic.base64EncodedString(),
            phaseKey: phaseKey,
            nonce: nonce)
    }

    /// The viewer half, in Swift. Production viewers are browsers and run the JavaScript
    /// mirror in `Resources/web/app/js/net/cloud-pairing.js`; this exists so the suite and the
    /// cross-runtime fixture can prove the two agree without a browser.
    ///
    /// `senderDeviceID` is what `POST /v1/pairing/claim` reported. Checking it against the
    /// wrapper closes the one thing the AEAD cannot say by itself: that the device the control
    /// plane recorded as having written the slot is the device that sealed the bytes in it.
    static func open(
        _ wrapper: CloudPairingWrapper,
        for offer: CloudPairingOffer,
        viewerEphemeralPrivateKey: Data,
        senderDeviceID: String?,
        nowMilliseconds: Int64
    ) throws -> CloudPairingHandover {
        try CloudCompatibilityPairing.validate(offer, nowMilliseconds: nowMilliseconds)
        if let senderDeviceID, senderDeviceID != wrapper.senderDeviceID {
            throw CloudHandoverError.wrongSender
        }
        guard wrapper.pairingID == offer.pairingID else { throw CloudHandoverError.wrongSender }
        let machineEphemeral = try CloudPairing.decodeCanonicalBase64(
            wrapper.ephemeralKey, field: "ephemeral_key", expectedLength: 32)
        let phaseKey = try phaseKey(
            offer: offer,
            privateKeyRaw: viewerEphemeralPrivateKey,
            peerPublicKeyRaw: machineEphemeral)
        let clear = try CloudPairing.open(
            wrapper,
            phaseKey: phaseKey,
            viewerEphemeralKey: offer.viewerEphemeralKey,
            machineEphemeralKey: wrapper.ephemeralKey)
        guard let value = try? CloudCanonicalJSON.parseStrict(clear),
              case .object(let object) = value,
              Set(object.keys) == handoverMembers,
              case .int(let version)? = object["v"], version == 1,
              case .string(let type)? = object["type"], type == "pairing_handover"
        else {
            throw CloudHandoverError.malformedHandover
        }
        func text(_ key: String) throws -> String {
            guard case .string(let value)? = object[key] else {
                throw CloudHandoverError.malformedHandover
            }
            return value
        }
        let handover = CloudPairingHandover(
            accountID: try text("account_id"),
            machineID: try text("machine_id"),
            machineSigningKey: try text("machine_signing_key"),
            machineFingerprint: try text("machine_fingerprint"),
            keyID: try text("key_id"),
            masterSecret: try text("master_secret"))
        try validate(handover)
        guard handover.accountID == offer.accountID else { throw CloudHandoverError.wrongAccount }
        return handover
    }

    static func x25519PublicKey(forPrivateKeyRaw raw: Data) throws -> Data {
        guard raw.count == 32 else {
            throw CloudPairingError.invalidLength(
                field: "ephemeral_private_key", expected: "32", actual: raw.count)
        }
        do {
            return try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: raw)
                .publicKey.rawRepresentation
        } catch {
            throw CloudPairingError.invalidKey(field: "ephemeral_private_key")
        }
    }

    private static func phaseKey(
        offer: CloudPairingOffer, privateKeyRaw: Data, peerPublicKeyRaw: Data
    ) throws -> Data {
        let shared = try CloudPairing.x25519SharedSecret(
            privateKeyRaw: privateKeyRaw, peerPublicKeyRaw: peerPublicKeyRaw)
        let material = try CloudPairing.derive(
            sharedSecretRaw: shared,
            pairingNonce: try CloudPairing.decodeCanonicalBase64(
                offer.pairingNonce, field: "pairing_nonce", expectedLength: 32),
            pairingID: offer.pairingID,
            claimNonce: try CloudPairing.decodeCanonicalBase64(
                offer.claimNonce, field: "claim_nonce", expectedLength: 32),
            phase: .grant)
        return material.phaseKey
    }

    private static func validate(_ handover: CloudPairingHandover) throws {
        try requireID(handover.accountID)
        try requireID(handover.machineID)
        try requireID(handover.keyID)
        let signingKey = try CloudPairing.decodeCanonicalBase64(
            handover.machineSigningKey, field: "machine_signing_key", expectedLength: 32)
        _ = try CloudPairing.decodeCanonicalBase64(
            handover.masterSecret, field: "master_secret", expectedLength: 32)
        guard handover.machineFingerprint
                == (try CloudPairing.ed25519Fingerprint(publicKeyRaw: signingKey)) else {
            throw CloudPairingError.fingerprintMismatch
        }
    }

    private static func requireID(_ text: String) throws {
        let bytes = Array(text.utf8)
        guard (1...128).contains(bytes.count),
              text.unicodeScalars.allSatisfy({ $0.value >= 0x20 && $0.value <= 0x7E }) else {
            throw CloudHandoverError.malformedOffer
        }
    }
}

public protocol CloudIdentityPairingClient: Sendable {
    func startIdentityPairing(_ request: CloudIdentityPairingStartRequest) async throws
        -> CloudIdentityPairingStart
    func writeIdentityPairingPhase(
        pairingID: String, phase: CloudPairingPhase, claimNonce: String,
        wrapper: CloudPairingWrapper
    ) async throws -> CloudIdentityPairingPhaseReceipt
    func pollIdentityPairingPhase(
        pairingID: String, phase: CloudPairingPhase, claimNonce: String
    ) async throws -> CloudIdentityPairingPoll
}

extension CloudAccountClient: CloudIdentityPairingClient {}

/// Production machine-side owner of the four-phase identity lifecycle.
///
/// Viewer writes `offer`/`activate`; machine writes `grant`/`confirm`. Grant bytes are retained by
/// `CloudExecutorIdentityAuthority`, and confirm uses a deterministic per-pairing nonce, so an
/// accepted write whose reply was lost is retried byte-for-byte and receives `duplicate`. The
/// viewer is pinned only after the confirm receipt is observed.
public actor CloudIdentityPairingMachineHandover {
    public enum Progress: Equatable, Sendable {
        case waitingForOffer(CloudPairingQR)
        case waitingForActivate(CloudPairingQR)
        case complete(CloudExecutorIdentitySnapshot)
    }

    private struct Session: Sendable {
        let start: CloudIdentityPairingStart
        let machineEphemeralPrivateKey: Data
    }

    private let client: any CloudIdentityPairingClient
    private let authority: CloudExecutorIdentityAuthority
    private let nowMilliseconds: @Sendable () -> Int64
    private let randomBytes: @Sendable (Int) -> Data
    private var sessions: [String: Session] = [:]

    public init(
        client: any CloudIdentityPairingClient,
        authority: CloudExecutorIdentityAuthority,
        nowMilliseconds: @escaping @Sendable () -> Int64 = {
            Int64(Date().timeIntervalSince1970 * 1_000)
        },
        randomBytes: @escaping @Sendable (Int) -> Data = { count in
            var generator = SystemRandomNumberGenerator()
            return Data((0..<count).map { _ in
                UInt8.random(in: .min ... .max, using: &generator)
            })
        }
    ) {
        self.client = client
        self.authority = authority
        self.nowMilliseconds = nowMilliseconds
        self.randomBytes = randomBytes
    }

    public func begin(rotationID: String) async throws -> CloudPairingQR {
        let material = try authority.transportMaterial()
        let privateKey = randomBytes(32)
        let pairingNonce = randomBytes(32)
        guard privateKey.count == 32, pairingNonce.count == 32,
              material.binding.keyEpoch <= UInt64(Int64.max) else {
            throw CloudExecutorIdentityError.protectedStateCorrupt
        }
        let publicKey = try CloudPairing.x25519PublicKey(privateKeyRaw: privateKey)
        let epoch = Int64(material.binding.keyEpoch)
        let request = CloudIdentityPairingStartRequest(
            machineSigningKey: material.deviceKey.publicKeyRaw.base64EncodedString(),
            machineFingerprint: material.binding.signingKeyFingerprint,
            machineEphemeralKey: publicKey.base64EncodedString(),
            pairingNonce: pairingNonce.base64EncodedString(),
            previousContentKeyEpoch: max(0, epoch - 1), contentKeyEpoch: epoch,
            rotationID: rotationID)
        let started = try await client.startIdentityPairing(request)
        guard started.rotationID == rotationID,
              started.qr.accountID == material.binding.accountID,
              started.qr.machineID == material.binding.machineID,
              started.qr.machineSigningKey == request.machineSigningKey,
              started.qr.machineFingerprint == request.machineFingerprint,
              started.qr.machineEphemeralKey == request.machineEphemeralKey,
              started.qr.pairingNonce == request.pairingNonce,
              started.epochs.machineKeyEpoch == epoch,
              started.contentKeyRotation.newEpoch == epoch else {
            throw CloudExecutorIdentityError.identityMismatch
        }
        sessions[started.qr.pairingID] = Session(
            start: started, machineEphemeralPrivateKey: privateKey)
        return started.qr
    }

    public func advance(pairingID: String) async throws -> Progress {
        guard let session = sessions[pairingID] else {
            throw CloudExecutorIdentityError.pairingNotPrepared
        }
        let qr = session.start.qr
        let offerPoll = try await client.pollIdentityPairingPhase(
            pairingID: pairingID, phase: .offer, claimNonce: qr.claimNonce)
        guard case .ready(_, _, let offerWrapper, _, _, _) = offerPoll else {
            return .waitingForOffer(qr)
        }
        let viewerEphemeral = try CloudPairing.decodeCanonicalBase64(
            offerWrapper.ephemeralKey, field: "viewer_ephemeral_key", expectedLength: 32)
        let shared = try CloudPairing.x25519SharedSecret(
            privateKeyRaw: session.machineEphemeralPrivateKey,
            peerPublicKeyRaw: viewerEphemeral)
        let pairingNonce = try CloudPairing.decodeCanonicalBase64(
            qr.pairingNonce, field: "pairing_nonce", expectedLength: 32)
        let claimNonce = try CloudPairing.decodeCanonicalBase64(
            qr.claimNonce, field: "claim_nonce", expectedLength: 32)
        let offerKey = try CloudPairing.derive(
            sharedSecretRaw: shared, pairingNonce: pairingNonce, pairingID: pairingID,
            claimNonce: claimNonce, phase: .offer)
        let offerBytes = try CloudPairing.open(
            offerWrapper, phaseKey: offerKey.phaseKey,
            viewerEphemeralKey: offerWrapper.ephemeralKey,
            machineEphemeralKey: qr.machineEphemeralKey)
        try Self.requireOfferBinding(offerBytes, qr: qr, senderDeviceID: offerWrapper.senderDeviceID)
        let prepared = try authority.prepareHandover(
            offerBytes: offerBytes, nowMilliseconds: nowMilliseconds(),
            machineEphemeralPrivateKey: session.machineEphemeralPrivateKey)
        let grant = try CloudPairing.decodeWrapper(prepared.wrapperBytes)
        guard grant.ephemeralKey == qr.machineEphemeralKey else {
            throw CloudExecutorIdentityError.identityMismatch
        }
        let grantReceipt = try await client.writeIdentityPairingPhase(
            pairingID: pairingID, phase: .grant, claimNonce: qr.claimNonce, wrapper: grant)
        try Self.requireEpochBinding(grantReceipt.epochs, started: session.start)

        let activatePoll = try await client.pollIdentityPairingPhase(
            pairingID: pairingID, phase: .activate, claimNonce: qr.claimNonce)
        guard case .ready(_, _, let activate, let activateSHA, _, _) = activatePoll else {
            return .waitingForActivate(qr)
        }
        let activateKey = try CloudPairing.derive(
            sharedSecretRaw: shared, pairingNonce: pairingNonce, pairingID: pairingID,
            claimNonce: claimNonce, phase: .activate)
        let activateBytes = try CloudPairing.open(
            activate, phaseKey: activateKey.phaseKey,
            viewerEphemeralKey: offerWrapper.ephemeralKey,
            machineEphemeralKey: qr.machineEphemeralKey)
        let wantedActivate = CloudCanonicalJSON.canonicalData(.object([
            "v": .int(1), "type": .string("pairing_activate"),
            "grant_sha256": .string(grantReceipt.phaseSHA256)
        ]))
        guard activateBytes == wantedActivate,
              activate.senderDeviceID == prepared.viewerDeviceID else {
            throw CloudExecutorIdentityError.identityMismatch
        }
        let confirmBytes = CloudCanonicalJSON.canonicalData(.object([
            "v": .int(1), "type": .string("pairing_confirm"),
            "activate_sha256": .string(activateSHA)
        ]))
        var noncePreimage = Data("clawdline-pairing-confirm-nonce-v1\0".utf8)
        noncePreimage.append(try CloudPairing.encodeWrapper(activate))
        let confirmNonce = Data(SHA256.hash(data: noncePreimage).prefix(12))
        let confirmKey = try CloudPairing.derive(
            sharedSecretRaw: shared, pairingNonce: pairingNonce, pairingID: pairingID,
            claimNonce: claimNonce, phase: .confirm)
        let confirm = try CloudPairing.seal(
            plaintext: confirmBytes, phase: .confirm, pairingID: pairingID,
            senderDeviceID: qr.machineID, ephemeralKey: qr.machineEphemeralKey,
            phaseKey: confirmKey.phaseKey, nonce: confirmNonce)
        let confirmReceipt = try await client.writeIdentityPairingPhase(
            pairingID: pairingID, phase: .confirm, claimNonce: qr.claimNonce,
            wrapper: confirm)
        try Self.requireEpochBinding(confirmReceipt.epochs, started: session.start)
        let snapshot = try authority.commitPreparedHandover(
            pairingID: pairingID, claimNonce: qr.claimNonce,
            deliveredFingerprint: prepared.viewerFingerprint,
            nowMilliseconds: nowMilliseconds())
        sessions.removeValue(forKey: pairingID)
        return .complete(snapshot)
    }

    private static func requireEpochBinding(
        _ actual: CloudIdentityEpochs, started: CloudIdentityPairingStart
    ) throws {
        guard actual.identityEpoch == started.epochs.identityEpoch,
              actual.machineKeyEpoch == started.epochs.machineKeyEpoch,
              actual.contentKeyEpoch == started.contentKeyRotation.newEpoch,
              actual.jwksGeneration == started.epochs.jwksGeneration else {
            throw CloudExecutorIdentityError.invalidReconnectProof
        }
    }

    private static func requireOfferBinding(
        _ bytes: Data, qr: CloudPairingQR, senderDeviceID: String
    ) throws {
        guard case .object(let object) = try CloudCanonicalJSON.parseStrict(bytes),
              case .string(let pairingID)? = object["pairing_id"],
              case .string(let claimNonce)? = object["claim_nonce"],
              case .string(let pairingNonce)? = object["pairing_nonce"],
              case .string(let accountID)? = object["account_id"],
              case .string(let viewerDeviceID)? = object["viewer_device_id"],
              pairingID == qr.pairingID, claimNonce == qr.claimNonce,
              pairingNonce == qr.pairingNonce, accountID == qr.accountID,
              viewerDeviceID == senderDeviceID else {
            throw CloudExecutorIdentityError.identityMismatch
        }
    }
}

/// Orders control-plane epoch mutation before the matching protected-state mutation. A server
/// success followed by a local persistence failure is intentionally fail closed: reconnect still
/// presents the retired local epoch and is refused. Retrying with the same replacement consumes
/// the server's `duplicate` receipt and completes the local commit exactly once.
public struct CloudExecutorIdentityMutationCoordinator: Sendable {
    private let client: CloudAccountClient
    private let authority: CloudExecutorIdentityAuthority

    public init(client: CloudAccountClient, authority: CloudExecutorIdentityAuthority) {
        self.client = client
        self.authority = authority
    }

    public func rotateMachine(
        replacementKey: CloudDeviceKeyPair, replacementMasterSecret: CloudMasterSecret,
        replacementKeyID: String
    ) async throws -> CloudExecutorIdentitySnapshot {
        let before = try authority.snapshot()
        guard before.keyEpoch < UInt64(Int64.max) else {
            throw CloudExecutorIdentityError.generationOverflow
        }
        let receipt = try await client.rotateMachineIdentity(
            machineID: before.machineID, expectedKeyEpoch: Int64(before.keyEpoch),
            publicKey: replacementKey.publicKeyRaw.base64EncodedString(),
            fingerprint: replacementKey.pairingFingerprint)
        guard receipt.keyFingerprint == replacementKey.pairingFingerprint,
              receipt.keyEpoch == Int64(before.keyEpoch + 1) else {
            throw CloudExecutorIdentityError.identityMismatch
        }
        let current = try authority.snapshot()
        if current.machineFingerprint == replacementKey.pairingFingerprint,
           current.keyEpoch == UInt64(receipt.keyEpoch) {
            return current
        }
        _ = try authority.rotateKeys(
            deviceKey: replacementKey, masterSecret: replacementMasterSecret,
            keyID: replacementKeyID, expectedGeneration: before.identityGeneration)
        return try authority.snapshot()
    }

    public func revokeViewer(
        deviceID: String, browserSessionCookie: String
    ) async throws -> CloudExecutorIdentitySnapshot {
        _ = try await client.revokeDevice(
            id: deviceID, browserSessionCookie: browserSessionCookie)
        let current = try authority.snapshot()
        if current.revokedDeviceIDs.contains(deviceID) { return current }
        return try authority.revokeDevice(
            deviceID, expectedGeneration: current.identityGeneration)
    }
}

/// The Mac's legacy compatibility half of pairing, as one call a settings control can make.
///
/// Deliberately not a `CloudPairingCryptographyProviding` conformance: that protocol's
/// `makeOpaqueHandover()` takes no arguments, which suits a design where the Mac produces the
/// QR and the viewer answers it. The deployed control plane carries one blob from the sender to
/// the requester, and the account key has to travel Mac → viewer, so the Mac is the *sender* and
/// what it seals depends entirely on the offer it was handed. There is nothing for a
/// zero-argument factory to make.
///
/// Every dependency is injected so the whole path — decode, key agreement, seal, deliver, pin —
/// runs under the suite without a Keychain, a network or a window.
struct CloudPairingCompleter: Sendable {
    struct Outcome: Equatable, Sendable {
        let viewerDeviceID: String
        /// What the viewer is showing on its own screen. The person compares these two.
        let viewerFingerprint: String
        /// What the control plane echoed back from the pairing record it is holding.
        let deliveredFingerprint: String
        let machineFingerprint: String
    }

    enum Failure: Error, LocalizedError, Equatable {
        case notSignedIn
        case wrongAccount
        case fingerprintNotEchoed
        case keychainTimedOut(seconds: Int)

        var errorDescription: String? {
            switch self {
            case .notSignedIn:
                return "This Mac is not connected to Clawdline Cloud."
            case .wrongAccount:
                return "That pairing code belongs to a different Clawdline Cloud account."
            case .fingerprintNotEchoed:
                return "The pairing service echoed a different fingerprint than the code carries."
            case .keychainTimedOut(let seconds):
                return "Pairing stopped because this Mac's Keychain did not answer within "
                    + "\(seconds) seconds. Its operation may still finish, but the pairing "
                    + "window is no longer waiting for it."
            }
        }
    }

    var restoredIdentity: @Sendable () async throws -> CloudMachineIdentity?
    var deviceKeyPair: @Sendable () async throws -> CloudDeviceKeyPair
    var masterSecret: @Sendable () async throws -> CloudMasterSecret
    var deliver: @Sendable (String, CloudOpaquePairingBlob) async throws -> CloudPairingDelivery
    var pin: @Sendable (CloudPairedDevice, String) throws -> Void
    var nowMilliseconds: @Sendable () -> Int64
    var randomBytes: @Sendable (Int) -> Data
    /// Production supplies both closures from the one protected identity authority. `nil` is the
    /// legacy deterministic fixture seam only; no production factory writes the JSON roster.
    var prepareIdentity: (@Sendable (
        CloudMachineIdentity, CloudDeviceKeyPair, CloudMasterSecret, Data, Int64
    ) async throws -> CloudExecutorPreparedHandover)? = nil
    var commitIdentity: (@Sendable (
        String, String, String, Int64
    ) async throws -> CloudExecutorIdentitySnapshot)? = nil

    static func production(
        client: CloudAccountClient = CloudAccountClient(),
        keys: CloudKeys = CloudKeys(),
        identityAuthority: CloudExecutorIdentityAuthority = CloudExecutorIdentityAuthority(
            store: CloudKeychainStore()),
        legacyPairedDevices: CloudPairedDeviceStore = CloudPairedDeviceStore(),
        keychainTimeoutSeconds: Int = CloudKeychainReader<CloudMachineIdentity?>.defaultTimeoutSeconds,
        nowMilliseconds: @escaping @Sendable () -> Int64 = {
            Int64(Date().timeIntervalSince1970 * 1_000)
        }
    ) -> CloudPairingCompleter {
        CloudPairingCompleter(
            restoredIdentity: {
                do {
                    return try await CloudKeychainReader(
                        label: "clawdline.cloud.pairing-identity",
                        timeoutSeconds: keychainTimeoutSeconds
                    ) { try client.restoredMachineIdentity() }.value()
                } catch CloudKeychainReader<CloudMachineIdentity?>.AwaitError.timedOut(let seconds) {
                    throw Failure.keychainTimedOut(seconds: seconds)
                }
            },
            deviceKeyPair: {
                do {
                    return try await CloudKeychainReader(
                        label: "clawdline.cloud.pairing-device-key",
                        timeoutSeconds: keychainTimeoutSeconds
                    ) {
                        switch identityAuthority.readiness() {
                        case .ready:
                            return try identityAuthority.transportMaterial().deviceKey
                        case .blocked(.protectedStateMissing):
                            return try keys.loadOrCreateDeviceKeyPair()
                        case .blocked(let error):
                            throw error
                        }
                    }.value()
                } catch CloudKeychainReader<CloudDeviceKeyPair>.AwaitError.timedOut(let seconds) {
                    throw Failure.keychainTimedOut(seconds: seconds)
                }
            },
            masterSecret: {
                do {
                    return try await CloudKeychainReader(
                        label: "clawdline.cloud.pairing-master-key",
                        timeoutSeconds: keychainTimeoutSeconds
                    ) {
                        switch identityAuthority.readiness() {
                        case .ready:
                            return try identityAuthority.transportMaterial().masterSecret
                        case .blocked(.protectedStateMissing):
                            return try keys.loadOrCreateMasterSecret()
                        case .blocked(let error):
                            throw error
                        }
                    }.value()
                } catch CloudKeychainReader<CloudMasterSecret>.AwaitError.timedOut(let seconds) {
                    throw Failure.keychainTimedOut(seconds: seconds)
                }
            },
            deliver: { pairingID, blob in
                try await client.completePairing(pairingID: pairingID, blob: blob)
            },
            pin: { _, _ in
                assertionFailure("production pairing must pin through CloudExecutorIdentityAuthority")
            },
            nowMilliseconds: nowMilliseconds,
            randomBytes: { count in
                var generator = SystemRandomNumberGenerator()
                return Data((0..<count).map { _ in
                    UInt8.random(in: .min ... .max, using: &generator)
                })
            },
            prepareIdentity: { identity, deviceKey, masterSecret, offerBytes, now in
                do {
                    return try await CloudKeychainReader(
                        label: "clawdline.cloud.pairing-authority-prepare",
                        timeoutSeconds: keychainTimeoutSeconds
                    ) {
                        switch identityAuthority.readiness(
                            expectedAccountID: identity.accountID,
                            expectedMachineID: identity.machineID) {
                        case .ready:
                            break
                        case .blocked(.protectedStateMissing):
                            let imported = try legacyPairedDevices.beginProtectedMigration(
                                accountID: identity.accountID).map {
                                CloudExecutorPairedDevice(
                                    deviceID: $0.deviceID, signingKey: $0.signingKey,
                                    fingerprint: try CloudPairing.ed25519Fingerprint(
                                        publicKeyRaw: $0.signingKey),
                                    pairedAtMilliseconds: $0.pairedAtMilliseconds,
                                    identityGeneration: 1,
                                    capabilities: CloudExecutorIdentityAuthority.defaultCapabilities)
                            }
                            _ = try identityAuthority.provision(
                                accountID: identity.accountID, machineID: identity.machineID,
                                deviceKey: deviceKey, masterSecret: masterSecret,
                                importedPairedDevices: imported)
                        case .blocked(let error):
                            throw error
                        }
                        try legacyPairedDevices.removeAll()
                        return try identityAuthority.prepareHandover(
                            offerBytes: offerBytes, nowMilliseconds: now)
                    }.value()
                } catch CloudKeychainReader<CloudExecutorPreparedHandover>.AwaitError
                    .timedOut(let seconds) {
                    throw Failure.keychainTimedOut(seconds: seconds)
                }
            },
            commitIdentity: { pairingID, claimNonce, fingerprint, now in
                do {
                    return try await CloudKeychainReader(
                        label: "clawdline.cloud.pairing-authority-commit",
                        timeoutSeconds: keychainTimeoutSeconds
                    ) {
                        try identityAuthority.commitPreparedHandover(
                            pairingID: pairingID, claimNonce: claimNonce,
                            deliveredFingerprint: fingerprint, nowMilliseconds: now)
                    }.value()
                } catch CloudKeychainReader<CloudExecutorIdentitySnapshot>.AwaitError
                    .timedOut(let seconds) {
                    throw Failure.keychainTimedOut(seconds: seconds)
                }
            })
    }

    /// Take the fragment a person carried from their browser, seal this account's key material
    /// for exactly that offer, hand the ciphertext to the control plane, and pin the viewer.
    ///
    /// The viewer is pinned **after** the blob is delivered rather than before: pinning is what
    /// makes a device able to drive this Mac, and a device that never received the account key
    /// cannot produce a command anyway. Doing it in the other order would leave a pinned viewer
    /// behind every failed delivery.
    func complete(offerFragment: String) async throws -> Outcome {
        guard let identity = try await restoredIdentity() else { throw Failure.notSignedIn }
        let now = nowMilliseconds()
        let offer = try CloudHandover.decodeOfferFragment(offerFragment, nowMilliseconds: now)
        guard offer.accountID == identity.accountID else { throw Failure.wrongAccount }

        let signing = try await deviceKeyPair()
        let machineFingerprint = try CloudPairing.ed25519Fingerprint(
            publicKeyRaw: signing.publicKeyRaw)
        let master = try await masterSecret()
        if let prepareIdentity, let commitIdentity {
            let offerBytes = CloudCanonicalJSON.canonicalData(offer.cloudJSONValue)
            let prepared = try await prepareIdentity(identity, signing, master, offerBytes, now)
            let delivery = try await deliver(
                offer.pairingID,
                try CloudOpaquePairingBlob(base64: prepared.wrapperBytes.base64EncodedString()))
            guard delivery.fingerprint == offer.viewerFingerprint else {
                throw Failure.fingerprintNotEchoed
            }
            _ = try await commitIdentity(
                offer.pairingID, offer.claimNonce, delivery.fingerprint, now)
            return Outcome(
                viewerDeviceID: offer.viewerDeviceID,
                viewerFingerprint: offer.viewerFingerprint,
                deliveredFingerprint: delivery.fingerprint,
                machineFingerprint: prepared.machineFingerprint)
        }
        let handover = CloudPairingHandover(
            accountID: identity.accountID,
            machineID: identity.machineID,
            machineSigningKey: signing.publicKeyRaw.base64EncodedString(),
            machineFingerprint: machineFingerprint,
            keyID: CloudBridgeLifecycle.masterKeyID,
            masterSecret: master.rawRepresentation.base64EncodedString())
        let wrapper = try CloudHandover.seal(
            handover, for: offer,
            machineDeviceID: identity.machineID,
            machineEphemeralPrivateKey: randomBytes(32),
            nonce: randomBytes(12),
            nowMilliseconds: now)

        let body = CloudPairing.encodeWrapperUnchecked(wrapper)
        let delivery = try await deliver(
            offer.pairingID, try CloudOpaquePairingBlob(base64: body.base64EncodedString()))
        // The control plane stored the requester's fingerprint at `start` and echoes it here.
        // It disagreeing with the fragment means the fragment was not the one that opened this
        // pairing, which is exactly the substitution a person comparing codes cannot see.
        guard delivery.fingerprint == offer.viewerFingerprint else {
            throw Failure.fingerprintNotEchoed
        }

        let viewerKey = try CloudPairing.decodeCanonicalBase64(
            offer.viewerSigningKey, field: "viewer_signing_key", expectedLength: 32)
        try pin(
            CloudPairedDevice(deviceID: offer.viewerDeviceID, signingKey: viewerKey,
                              pairedAtMilliseconds: now),
            identity.accountID)
        return Outcome(
            viewerDeviceID: offer.viewerDeviceID,
            viewerFingerprint: offer.viewerFingerprint,
            deliveredFingerprint: delivery.fingerprint,
            machineFingerprint: machineFingerprint)
    }
}

/// Owns one desktop-browser handover from preview through its terminal result.
///
/// The AppKit surface asks this object to preview an offer, shows the returned fingerprint, and
/// calls ``confirm(_:)`` only after the person accepts that exact preview. Keeping the task and an
/// attempt generation here gives cancellation one owner: a completion that arrives after cancel
/// cannot make an old Settings sheet look successful. Cancellation still means "stop waiting",
/// not "roll back" — a synchronous Keychain operation or a Cloud write that already completed may
/// finish underneath it, which is the same boundary documented by ``CloudKeychainReader``.
@MainActor
final class CloudBrowserPairingWorkflow {
    /// A closed offer has bounded identifiers and fixed-size keys/nonces. Four KiB is deliberately
    /// generous for that JSON after base64url expansion, while still rejecting attacker-sized text
    /// before normalization, base64 decoding or JSON allocation.
    static let maxOfferFragmentUTF8Bytes = 4_096

    struct Preview: Equatable, Sendable {
        let fragment: String
        let viewerDeviceID: String
        let viewerFingerprint: String
    }

    enum Phase: Equatable, Sendable {
        case idle
        case awaitingConfirmation(viewerFingerprint: String)
        case pairing(viewerFingerprint: String)
        case succeeded(CloudPairingCompleter.Outcome)
        case cancelled
        case failed(String)
    }

    enum InputFailure: Error, LocalizedError, Equatable {
        case emptyOffer
        case offerTooLarge(maxBytes: Int)
        case alreadyPairing

        var errorDescription: String? {
            switch self {
            case .emptyOffer:
                return "Paste the pairing code shown by app.clawdline.com."
            case .offerTooLarge(let maxBytes):
                return "That pairing code is larger than the \(maxBytes)-byte protocol limit."
            case .alreadyPairing:
                return "This Mac is already finishing another browser pairing."
            }
        }
    }

    struct Services: Sendable {
        var nowMilliseconds: @Sendable () -> Int64
        var decode: @Sendable (String, Int64) throws -> CloudPairingOffer
        var complete: @Sendable (String) async throws -> CloudPairingCompleter.Outcome
    }

    private let services: Services
    private var pending: Preview?
    private var task: Task<Void, Never>?
    private var generation = 0
    private(set) var phase: Phase = .idle {
        didSet { onChange?(phase) }
    }
    var onChange: ((Phase) -> Void)?

    init(services: Services) {
        self.services = services
    }

    static func production(
        completer: CloudPairingCompleter = .production()
    ) -> CloudBrowserPairingWorkflow {
        CloudBrowserPairingWorkflow(services: Services(
            nowMilliseconds: { Int64(Date().timeIntervalSince1970 * 1_000) },
            decode: { fragment, now in
                try CloudHandover.decodeOfferFragment(fragment, nowMilliseconds: now)
            },
            complete: { fragment in
                try await completer.complete(offerFragment: fragment)
            }))
    }

    func preview(raw: String) throws -> Preview {
        guard task == nil else { throw InputFailure.alreadyPairing }
        guard raw.utf8.count <= Self.maxOfferFragmentUTF8Bytes else {
            throw InputFailure.offerTooLarge(maxBytes: Self.maxOfferFragmentUTF8Bytes)
        }
        let fragment = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !fragment.isEmpty else { throw InputFailure.emptyOffer }
        let offer = try services.decode(fragment, services.nowMilliseconds())
        let preview = Preview(
            fragment: fragment,
            viewerDeviceID: offer.viewerDeviceID,
            viewerFingerprint: offer.viewerFingerprint)
        pending = preview
        phase = .awaitingConfirmation(viewerFingerprint: preview.viewerFingerprint)
        return preview
    }

    /// Cross the consent boundary once for exactly the preview currently on screen.
    func confirm(_ preview: Preview) {
        guard task == nil, pending == preview,
              phase == .awaitingConfirmation(viewerFingerprint: preview.viewerFingerprint)
        else { return }
        generation += 1
        let attempt = generation
        pending = nil
        phase = .pairing(viewerFingerprint: preview.viewerFingerprint)
        let complete = services.complete
        task = Task { [weak self] in
            do {
                let outcome = try await complete(preview.fragment)
                try Task.checkCancellation()
                self?.settle(attempt: attempt, phase: .succeeded(outcome))
            } catch is CancellationError {
                self?.settle(attempt: attempt, phase: .cancelled)
            } catch {
                let described = (error as? LocalizedError)?.errorDescription
                    ?? error.localizedDescription
                self?.settle(attempt: attempt, phase: .failed(described))
            }
        }
    }

    func cancel() {
        generation += 1
        pending = nil
        task?.cancel()
        task = nil
        phase = .cancelled
    }

    private func settle(attempt: Int, phase terminal: Phase) {
        guard generation == attempt, task != nil else { return }
        task = nil
        phase = terminal
    }

    deinit {
        task?.cancel()
    }
}
