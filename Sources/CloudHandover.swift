import Foundation
import CryptoKit

/// Key handover between a browser viewer and this Mac, over the three pairing calls the
/// deployed control plane actually exposes.
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

enum CloudHandoverError: Error, LocalizedError, Equatable {
    case malformedOffer
    case malformedHandover
    case offerExpired
    case offerLifetimeTooLong
    case wrongAccount
    case wrongSender
    case malformedInvitation
    case invitationExpired
    case storeUnreadable
    case storeUnwritable

    var errorDescription: String? {
        switch self {
        case .malformedOffer: return "The pairing offer is malformed."
        case .malformedHandover: return "The sealed pairing handover is malformed."
        case .offerExpired: return "That pairing offer has expired; show a fresh one."
        case .offerLifetimeTooLong: return "That pairing offer claims an unusable lifetime."
        case .wrongAccount: return "That pairing offer belongs to another account."
        case .wrongSender: return "The handover was sealed by a different device."
        case .malformedInvitation: return "The QR pairing invitation is malformed."
        case .invitationExpired: return "That QR has expired; show a fresh one."
        case .storeUnreadable: return "The paired-device store could not be read."
        case .storeUnwritable: return "The paired-device store could not be written."
        }
    }
}

/// A one-time Mac-displayed QR that transports the viewer's existing pairing offer back to
/// this Mac without making the offer readable to Cloud.
///
/// GitHub establishes account ownership. Possession of `secret` establishes that the browser
/// scanned the QR on this physical Mac. The browser encrypts its ordinary offer with that
/// secret, Cloud relays only the opaque AES-GCM bytes, and the original X25519 handover still
/// moves the account master secret directly from Mac to viewer.
struct CloudPairingInvitation: Equatable, Sendable {
    static let secretBytes = 32
    static let fragmentName = "pair"

    let invitationID: String
    let secret: Data
    let expiresAtMilliseconds: Int64

    init(invitationID: String, secret: Data, expiresAtMilliseconds: Int64) throws {
        guard !invitationID.isEmpty, invitationID.utf8.count <= 128,
              secret.count == Self.secretBytes, expiresAtMilliseconds >= 0 else {
            throw CloudHandoverError.malformedInvitation
        }
        self.invitationID = invitationID
        self.secret = secret
        self.expiresAtMilliseconds = expiresAtMilliseconds
    }

    var secretHash: Data { Data(SHA256.hash(data: secret)) }

    func qrURL(appOrigin: URL = URL(string: "https://app.clawdline.com/")!) -> URL? {
        let value = CloudJSONValue.object([
            "v": .int(1),
            "type": .string("pairing_invitation"),
            "invitation_id": .string(invitationID),
            "secret": .string(secret.base64EncodedString()),
            "expires_at": .int(expiresAtMilliseconds),
        ])
        let fragment = CloudPairing.encodeCanonicalBase64URL(
            CloudCanonicalJSON.canonicalData(value))
        var components = URLComponents(url: appOrigin, resolvingAgainstBaseURL: false)
        components?.fragment = Self.fragmentName + "=" + fragment
        return components?.url
    }

    func openEncryptedOffer(
        _ blob: CloudOpaquePairingBlob, nowMilliseconds: Int64
    ) throws -> String {
        guard nowMilliseconds >= 0, expiresAtMilliseconds >= nowMilliseconds else {
            throw CloudHandoverError.invitationExpired
        }
        guard let combined = Data(base64Encoded: blob.wireBase64) else {
            throw CloudHandoverError.malformedInvitation
        }
        do {
            let box = try AES.GCM.SealedBox(combined: combined)
            let clear = try AES.GCM.open(
                box, using: SymmetricKey(data: secret), authenticating: authenticatedData)
            guard let offer = String(data: clear, encoding: .utf8), !offer.isEmpty else {
                throw CloudHandoverError.malformedInvitation
            }
            return offer
        } catch let error as CloudHandoverError {
            throw error
        } catch {
            throw CloudHandoverError.malformedInvitation
        }
    }

    private var authenticatedData: Data {
        Data(("clawdline-pairing-invitation-v1\0" + invitationID).utf8)
    }
}

/// The pinned viewer devices, on disk beside the rest of this Mac's remote state.
///
/// Account-scoped on purpose: signing this Mac into a different Cloud account must not inherit
/// the previous account's pinned viewers, so a stored account id that does not match the
/// current one makes the whole file a fresh, empty one rather than something to merge.
final class CloudPairedDeviceStore: @unchecked Sendable {
    private let url: URL
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
        do {
            try FileManager.default.removeItem(at: url)
        } catch CocoaError.fileNoSuchFile {
            return
        } catch let error as NSError where error.domain == NSCocoaErrorDomain
                    && error.code == NSFileNoSuchFileError {
            return
        } catch {
            throw CloudHandoverError.storeUnwritable
        }
    }

    private func loadUnlocked(accountID: String) throws -> (devices: [CloudPairedDevice], stored: String?) {
        guard FileManager.default.fileExists(atPath: url.path) else { return ([], nil) }
        guard let data = try? Data(contentsOf: url) else {
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

/// What a viewer hands the Mac out of band. Eleven members, exactly, in canonical JSON —
/// the same closed shape discipline `CloudPairingQR` uses, for the same reason.
struct CloudPairingOffer: Equatable, Sendable {
    var pairingID: String
    var claimNonce: String
    var pairingNonce: String
    var accountID: String
    var viewerDeviceID: String
    var viewerSigningKey: String
    var viewerEphemeralKey: String
    var viewerFingerprint: String
    var expiresAt: Int64

    var cloudJSONValue: CloudJSONValue {
        .object([
            "v": .int(1),
            "type": .string("pairing_offer"),
            "pairing_id": .string(pairingID),
            "claim_nonce": .string(claimNonce),
            "pairing_nonce": .string(pairingNonce),
            "account_id": .string(accountID),
            "viewer_device_id": .string(viewerDeviceID),
            "viewer_signing_key": .string(viewerSigningKey),
            "viewer_ephemeral_key": .string(viewerEphemeralKey),
            "viewer_fingerprint": .string(viewerFingerprint),
            "expires_at": .int(expiresAt),
        ])
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
    static let offerLifetimeMilliseconds: Int64 = 600_000

    private static let offerMembers: Set<String> = [
        "v", "type", "pairing_id", "claim_nonce", "pairing_nonce", "account_id",
        "viewer_device_id", "viewer_signing_key", "viewer_ephemeral_key",
        "viewer_fingerprint", "expires_at",
    ]
    private static let handoverMembers: Set<String> = [
        "v", "type", "account_id", "machine_id", "machine_signing_key",
        "machine_fingerprint", "key_id", "master_secret",
    ]

    static func encodeOfferFragment(_ offer: CloudPairingOffer, nowMilliseconds: Int64) throws -> String {
        try validate(offer, nowMilliseconds: nowMilliseconds)
        return CloudPairing.encodeCanonicalBase64URL(
            CloudCanonicalJSON.canonicalData(offer.cloudJSONValue))
    }

    /// Serialization without validation, so a rejection test can build a fragment the decoder
    /// must refuse. Receivers always use `decodeOfferFragment`.
    static func encodeOfferFragmentUnchecked(_ offer: CloudPairingOffer) -> String {
        CloudPairing.encodeCanonicalBase64URL(
            CloudCanonicalJSON.canonicalData(offer.cloudJSONValue))
    }

    static func decodeOfferFragment(
        _ fragment: String, nowMilliseconds: Int64
    ) throws -> CloudPairingOffer {
        let bytes = try CloudPairing.decodeCanonicalBase64URL(fragment)
        guard let value = try? CloudCanonicalJSON.parseStrict(bytes),
              case .object(let object) = value,
              Set(object.keys) == offerMembers,
              case .int(let version)? = object["v"], version == 1,
              case .string(let type)? = object["type"], type == "pairing_offer",
              case .int(let expiresAt)? = object["expires_at"]
        else {
            throw CloudHandoverError.malformedOffer
        }
        func text(_ key: String) throws -> String {
            guard case .string(let value)? = object[key] else {
                throw CloudHandoverError.malformedOffer
            }
            return value
        }
        let offer = CloudPairingOffer(
            pairingID: try text("pairing_id"),
            claimNonce: try text("claim_nonce"),
            pairingNonce: try text("pairing_nonce"),
            accountID: try text("account_id"),
            viewerDeviceID: try text("viewer_device_id"),
            viewerSigningKey: try text("viewer_signing_key"),
            viewerEphemeralKey: try text("viewer_ephemeral_key"),
            viewerFingerprint: try text("viewer_fingerprint"),
            expiresAt: expiresAt)
        try validate(offer, nowMilliseconds: nowMilliseconds)
        return offer
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
        try validate(offer, nowMilliseconds: nowMilliseconds)
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
        try validate(offer, nowMilliseconds: nowMilliseconds)
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

    private static func validate(_ offer: CloudPairingOffer, nowMilliseconds: Int64) throws {
        try requireID(offer.pairingID)
        try requireID(offer.accountID)
        try requireID(offer.viewerDeviceID)
        let claimNonce = try CloudPairing.decodeCanonicalBase64(
            offer.claimNonce, field: "claim_nonce", expectedLength: 32)
        let pairingNonce = try CloudPairing.decodeCanonicalBase64(
            offer.pairingNonce, field: "pairing_nonce", expectedLength: 32)
        guard claimNonce != pairingNonce else { throw CloudPairingError.reusedNonce }
        let signingKey = try CloudPairing.decodeCanonicalBase64(
            offer.viewerSigningKey, field: "viewer_signing_key", expectedLength: 32)
        _ = try CloudPairing.decodeCanonicalBase64(
            offer.viewerEphemeralKey, field: "viewer_ephemeral_key", expectedLength: 32)
        guard offer.viewerFingerprint == (try CloudPairing.ed25519Fingerprint(publicKeyRaw: signingKey)) else {
            throw CloudPairingError.fingerprintMismatch
        }
        guard offer.expiresAt >= 0, nowMilliseconds >= 0 else {
            throw CloudPairingError.unsafeEpochMilliseconds
        }
        guard offer.expiresAt >= nowMilliseconds else { throw CloudHandoverError.offerExpired }
        guard offer.expiresAt - nowMilliseconds <= offerLifetimeMilliseconds else {
            throw CloudHandoverError.offerLifetimeTooLong
        }
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

/// The Mac's half of pairing, as one call a settings control can make.
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

        var errorDescription: String? {
            switch self {
            case .notSignedIn:
                return "This Mac is not connected to Clawdline Cloud."
            case .wrongAccount:
                return "That pairing code belongs to a different Clawdline Cloud account."
            case .fingerprintNotEchoed:
                return "The pairing service echoed a different fingerprint than the code carries."
            }
        }
    }

    var restoredIdentity: @Sendable () throws -> CloudMachineIdentity?
    var deviceKeyPair: @Sendable () throws -> CloudDeviceKeyPair
    var masterSecret: @Sendable () throws -> CloudMasterSecret
    var deliver: @Sendable (String, CloudOpaquePairingBlob) async throws -> CloudPairingDelivery
    var pin: @Sendable (CloudPairedDevice, String) throws -> Void
    var nowMilliseconds: @Sendable () -> Int64
    var randomBytes: @Sendable (Int) -> Data

    static func production(
        client: CloudAccountClient = CloudAccountClient(),
        keys: CloudKeys = CloudKeys(),
        pairedDevices: CloudPairedDeviceStore = CloudPairedDeviceStore()
    ) -> CloudPairingCompleter {
        CloudPairingCompleter(
            restoredIdentity: { try client.restoredMachineIdentity() },
            deviceKeyPair: { try keys.loadOrCreateDeviceKeyPair() },
            masterSecret: { try keys.loadOrCreateMasterSecret() },
            deliver: { pairingID, blob in
                try await client.completePairing(pairingID: pairingID, blob: blob)
            },
            pin: { device, accountID in try pairedDevices.pin(device, accountID: accountID) },
            nowMilliseconds: { Int64(Date().timeIntervalSince1970 * 1_000) },
            randomBytes: { count in
                var generator = SystemRandomNumberGenerator()
                return Data((0..<count).map { _ in
                    UInt8.random(in: .min ... .max, using: &generator)
                })
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
        guard let identity = try restoredIdentity() else { throw Failure.notSignedIn }
        let now = nowMilliseconds()
        let offer = try CloudHandover.decodeOfferFragment(offerFragment, nowMilliseconds: now)
        guard offer.accountID == identity.accountID else { throw Failure.wrongAccount }

        let signing = try deviceKeyPair()
        let machineFingerprint = try CloudPairing.ed25519Fingerprint(
            publicKeyRaw: signing.publicKeyRaw)
        let handover = CloudPairingHandover(
            accountID: identity.accountID,
            machineID: identity.machineID,
            machineSigningKey: signing.publicKeyRaw.base64EncodedString(),
            machineFingerprint: machineFingerprint,
            keyID: CloudBridgeLifecycle.masterKeyID,
            masterSecret: try masterSecret().rawRepresentation.base64EncodedString())
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
