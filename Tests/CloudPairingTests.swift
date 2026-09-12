import Foundation
import CryptoKit

private enum CloudPairingTestFailure: Error, CustomStringConvertible {
    case failed(String)

    var description: String {
        switch self {
        case .failed(let message): return message
        }
    }
}

private struct CloudPairingTestHarness {
    private(set) var checks = 0

    mutating func check(_ condition: @autoclosure () throws -> Bool, _ name: String) throws {
        checks += 1
        guard try condition() else { throw CloudPairingTestFailure.failed("check \(checks) failed: \(name)") }
    }

    mutating func equal<T: Equatable>(_ actual: @autoclosure () throws -> T, _ expected: T, _ name: String) throws {
        checks += 1
        let value = try actual()
        guard value == expected else {
            throw CloudPairingTestFailure.failed("check \(checks) failed: \(name); got \(value), expected \(expected)")
        }
    }

    mutating func rejects(_ name: String, _ operation: () throws -> Void) throws {
        checks += 1
        do {
            try operation()
            throw CloudPairingTestFailure.failed("check \(checks) failed: \(name); operation was accepted")
        } catch let error as CloudPairingTestFailure {
            throw error
        } catch {
            return
        }
    }

    mutating func rejects(_ expected: CloudPairingError, _ name: String, _ operation: () throws -> Void) throws {
        checks += 1
        do {
            try operation()
            throw CloudPairingTestFailure.failed("check \(checks) failed: \(name); operation was accepted")
        } catch let error as CloudPairingTestFailure {
            throw error
        } catch let error as CloudPairingError {
            guard error == expected else {
                throw CloudPairingTestFailure.failed("check \(checks) failed: \(name); got \(error), expected \(expected)")
            }
        } catch {
            throw CloudPairingTestFailure.failed("check \(checks) failed: \(name); unexpected error \(error)")
        }
    }

    mutating func rejects(_ expected: CloudCanonicalJSONError, _ name: String, _ operation: () throws -> Void) throws {
        checks += 1
        do {
            try operation()
            throw CloudPairingTestFailure.failed("check \(checks) failed: \(name); operation was accepted")
        } catch let error as CloudPairingTestFailure {
            throw error
        } catch let error as CloudCanonicalJSONError {
            guard error == expected else {
                throw CloudPairingTestFailure.failed("check \(checks) failed: \(name); got \(error), expected \(expected)")
            }
        } catch {
            throw CloudPairingTestFailure.failed("check \(checks) failed: \(name); unexpected error \(error)")
        }
    }

    mutating func rejects(_ expected: CloudExecutorIdentityError, _ name: String,
                          _ operation: () throws -> Void) throws {
        checks += 1
        do {
            try operation()
            throw CloudPairingTestFailure.failed("check \(checks) failed: \(name); operation was accepted")
        } catch let error as CloudPairingTestFailure {
            throw error
        } catch let error as CloudExecutorIdentityError {
            guard error == expected else {
                throw CloudPairingTestFailure.failed(
                    "check \(checks) failed: \(name); got \(error), expected \(expected)")
            }
        } catch {
            throw CloudPairingTestFailure.failed(
                "check \(checks) failed: \(name); unexpected error \(error)")
        }
    }

}

private final class CloudPairingIdentityTestStore: SecretStore, @unchecked Sendable {
    let capabilities: Set<HostCapability> = [.secrets]
    let coordinator = CloudKeyStoreCoordinator()
    private let lock = NSLock()
    private var values: [String: Data] = [:]

    func data(for account: String) throws -> Data? {
        lock.lock(); defer { lock.unlock() }
        return values[account]
    }
    func set(_ data: Data, for account: String) throws {
        lock.lock(); values[account] = data; lock.unlock()
    }
    func loadOrCreate(_ account: String, create: @Sendable () throws -> Data) throws -> Data {
        lock.lock(); defer { lock.unlock() }
        if let value = values[account] { return value }
        let value = try create(); values[account] = value; return value
    }
    func rotate(_ account: String, replace: @Sendable (Data?) throws -> Data) throws -> Data {
        lock.lock(); defer { lock.unlock() }
        let value = try replace(values[account]); values[account] = value; return value
    }
    func remove(_ account: String) throws {
        lock.lock(); values.removeValue(forKey: account); lock.unlock()
    }
}

extension CloudPairingIdentityTestStore: CloudKeyStoring {}

private actor IdentityPairingRouteHTTP: CloudAccountHTTPTransport {
    struct Reply: Sendable { let status: Int; let bytes: Data }
    private var replies: [Reply] = []
    private var requests: [URLRequest] = []

    func enqueue(status: Int = 200, bytes: Data) {
        replies.append(Reply(status: status, bytes: bytes))
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        guard !replies.isEmpty, let url = request.url else {
            throw CloudAccountError.invalidResponse
        }
        requests.append(request)
        let reply = replies.removeFirst()
        return (reply.bytes, HTTPURLResponse(
            url: url, statusCode: reply.status, httpVersion: "HTTP/1.1",
            headerFields: ["Content-Type": "application/json"])!)
    }

    func captured() -> [URLRequest] { requests }
}

private func pairingHex(_ text: String) -> Data {
    let clean = text.filter { !$0.isWhitespace }
    precondition(clean.count.isMultiple(of: 2))
    var data = Data()
    var index = clean.startIndex
    while index < clean.endIndex {
        let next = clean.index(index, offsetBy: 2)
        data.append(UInt8(clean[index..<next], radix: 16)!)
        index = next
    }
    return data
}

private func pairingHexString(_ data: Data) -> String {
    data.map { String(format: "%02x", $0) }.joined()
}

private func pairingSHA256Hex(_ data: Data) -> String {
    pairingHexString(Data(SHA256.hash(data: data)))
}

private func replacingLastByte(_ data: Data) -> Data {
    var bytes = [UInt8](data)
    bytes[bytes.count - 1] ^= 1
    return Data(bytes)
}

/// Every rendering a diagnostic reaches without naming a member: interpolation, the two
/// `String` initialisers, and `dump`, which walks the value's mirror and is the one that
/// prints a `Data` member byte by byte.
private func pairingRenderings(of value: Any) -> [(String, String)] {
    var dumped = ""
    dump(value, to: &dumped)
    return [
        ("interpolation", "\(value)"),
        ("String(describing:)", String(describing: value)),
        ("String(reflecting:)", String(reflecting: value)),
        ("dump", dumped)
    ]
}

/// True when `secret`'s bytes are recoverable from `text` in any form these renderings
/// produce: hex in either case, base64, or the ordered decimal-per-byte listing `dump`
/// prints for a `Data`. The real bytes are what is scanned for on purpose — an assertion
/// that the word "redacted" appears would still pass after somebody renamed the member it
/// is supposed to be hiding, or swapped it for one that prints itself.
private func pairingRenderingLeaks(_ text: String, secret: Data) -> Bool {
    precondition(!secret.isEmpty)
    if text.lowercased().contains(pairingHexString(secret)) { return true }
    if text.contains(secret.base64EncodedString()) { return true }
    let printed = text.split(whereSeparator: { !$0.isNumber }).compactMap { Int($0) }
    let wanted = secret.map { Int($0) }
    guard printed.count >= wanted.count else { return false }
    for start in 0...(printed.count - wanted.count)
    where Array(printed[start..<(start + wanted.count)]) == wanted {
        return true
    }
    return false
}

/// Wrapper bytes alone — NOT the complete §5.2 phase-write body. Used to pin the
/// wrapper-layer conservative size check, never the spec's full-body boundary.
private func canonicalWrapperBytes(exactByteCount target: Int, ephemeralKey: String) -> Data {
    for senderLength in 1...128 {
        var wrapper = CloudPairingWrapper(
            phase: .offer,
            pairingID: "p",
            senderDeviceID: String(repeating: "s", count: senderLength),
            ephemeralKey: ephemeralKey,
            nonce: Data(repeating: 0, count: 12).base64EncodedString(),
            ciphertext: ""
        )
        let fixedLength = CloudPairing.encodeWrapperUnchecked(wrapper).count
        let requiredBase64Length = target - fixedLength
        guard requiredBase64Length >= 24, requiredBase64Length.isMultiple(of: 4) else { continue }
        wrapper.ciphertext = Data(repeating: 0xA5, count: requiredBase64Length / 4 * 3).base64EncodedString()
        let body = CloudPairing.encodeWrapperUnchecked(wrapper)
        if body.count == target { return body }
    }
    preconditionFailure("could not construct canonical wrapper bytes of \(target) bytes")
}

/// A semantically valid wrapper whose complete two-member phase-write body
/// `{claim_nonce, blob}` serializes to exactly `target` canonical UTF-8 bytes.
private func phaseWriteWrapper(bodyExactByteCount target: Int, claimNonce: String, ephemeralKey: String) -> CloudPairingWrapper {
    for senderLength in 1...128 {
        var wrapper = CloudPairingWrapper(
            phase: .offer,
            pairingID: "p",
            senderDeviceID: String(repeating: "s", count: senderLength),
            ephemeralKey: ephemeralKey,
            nonce: Data(repeating: 0, count: 12).base64EncodedString(),
            ciphertext: ""
        )
        let fixedLength = CloudPairing.encodePhaseWriteBodyUnchecked(claimNonce: claimNonce, wrapper: wrapper).count
        let requiredBase64Length = target - fixedLength
        guard requiredBase64Length >= 24, requiredBase64Length.isMultiple(of: 4) else { continue }
        wrapper.ciphertext = Data(repeating: 0xA5, count: requiredBase64Length / 4 * 3).base64EncodedString()
        let body = CloudPairing.encodePhaseWriteBodyUnchecked(claimNonce: claimNonce, wrapper: wrapper)
        if body.count == target { return wrapper }
    }
    preconditionFailure("could not construct a wrapper giving a \(target)-byte phase-write body")
}

private enum IdentityPairingReplyLoss: Error { case afterAcceptedWrite }

private actor IdentityPairingReplayClient: CloudIdentityPairingClient {
    let pairingID = "pairing-four-phase"
    let claimNonce = Data(repeating: 0x31, count: 32)
    let viewerSigning = try! CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 0x41, count: 32))
    let viewerAgreementPrivate = Data(repeating: 0x51, count: 32)
    var request: CloudIdentityPairingStartRequest?
    var grant: CloudPairingWrapper?
    var grantSHA: String?
    var activate: CloudPairingWrapper?
    var activateSHA: String?
    var confirm: CloudPairingWrapper?
    var lostGrantReply = false
    var lostConfirmReply = false

    func startIdentityPairing(_ request: CloudIdentityPairingStartRequest) async throws
        -> CloudIdentityPairingStart {
        self.request = request
        let qr = CloudPairingQR(
            pairingID: pairingID, claimNonce: claimNonce.base64EncodedString(),
            expiresAt: 700_000, accountID: "account-four-phase",
            machineID: "machine-four-phase", machineSigningKey: request.machineSigningKey,
            machineFingerprint: request.machineFingerprint,
            machineEphemeralKey: request.machineEphemeralKey,
            pairingNonce: request.pairingNonce)
        let epochs = CloudIdentityEpochs(
            identityEpoch: 1, machineKeyEpoch: 1, viewerKeyEpoch: nil,
            contentKeyEpoch: 1, jwksGeneration: 1)
        let window = CloudIdentityRotationWindow(
            oldEpoch: 0, newEpoch: 1, oldAcceptUntilMilliseconds: 600_000,
            newAcceptFromMilliseconds: 100_000)
        return CloudIdentityPairingStart(
            qr: qr, epochs: epochs, rotationID: request.rotationID,
            signingRotation: window, jwksRotation: window, contentKeyRotation: window)
    }

    func pollIdentityPairingPhase(
        pairingID: String, phase: CloudPairingPhase, claimNonce: String
    ) async throws -> CloudIdentityPairingPoll {
        guard let request, pairingID == self.pairingID,
              claimNonce == self.claimNonce.base64EncodedString() else {
            throw CloudAccountError.invalidResponse
        }
        let viewerPublic = try CloudPairing.x25519PublicKey(
            privateKeyRaw: viewerAgreementPrivate)
        let machinePublic = try CloudPairing.decodeCanonicalBase64(
            request.machineEphemeralKey, field: "machine_ephemeral_key", expectedLength: 32)
        let shared = try CloudPairing.x25519SharedSecret(
            privateKeyRaw: viewerAgreementPrivate, peerPublicKeyRaw: machinePublic)
        let pairingNonce = try CloudPairing.decodeCanonicalBase64(
            request.pairingNonce, field: "pairing_nonce", expectedLength: 32)
        switch phase {
        case .offer:
            let clear = CloudCanonicalJSON.canonicalData(.object([
                "v": .int(1), "type": .string("pairing_offer"),
                "pairing_id": .string(pairingID), "claim_nonce": .string(claimNonce),
                "pairing_nonce": .string(request.pairingNonce),
                "account_id": .string("account-four-phase"),
                "viewer_device_id": .string("viewer-four-phase"),
                "viewer_signing_key": .base64(viewerSigning.publicKeyRaw),
                "viewer_ephemeral_key": .base64(viewerPublic),
                "viewer_fingerprint": .string(viewerSigning.pairingFingerprint),
                "expires_at": .int(700_000)
            ]))
            let key = try CloudPairing.derive(
                sharedSecretRaw: shared, pairingNonce: pairingNonce, pairingID: pairingID,
                claimNonce: self.claimNonce, phase: .offer)
            let wrapper = try CloudPairing.seal(
                plaintext: clear, phase: .offer, pairingID: pairingID,
                senderDeviceID: "viewer-four-phase",
                ephemeralKey: viewerPublic.base64EncodedString(), phaseKey: key.phaseKey,
                nonce: Data(repeating: 0x61, count: 12))
            let digest = pairingSHA256Hex(try CloudPairing.encodeWrapper(wrapper))
            return .ready(pairingID: pairingID, phase: phase, wrapper: wrapper,
                          phaseSHA256: digest, recordedAtMilliseconds: 100_010,
                          finalized: false)
        case .activate:
            guard let grantSHA else {
                return .pending(pairingID: pairingID, phase: phase)
            }
            if let activate, let activateSHA {
                return .ready(pairingID: pairingID, phase: phase, wrapper: activate,
                              phaseSHA256: activateSHA, recordedAtMilliseconds: 100_030,
                              finalized: false)
            }
            let clear = CloudCanonicalJSON.canonicalData(.object([
                "v": .int(1), "type": .string("pairing_activate"),
                "grant_sha256": .string(grantSHA)
            ]))
            let key = try CloudPairing.derive(
                sharedSecretRaw: shared, pairingNonce: pairingNonce, pairingID: pairingID,
                claimNonce: self.claimNonce, phase: .activate)
            let wrapper = try CloudPairing.seal(
                plaintext: clear, phase: .activate, pairingID: pairingID,
                senderDeviceID: "viewer-four-phase",
                ephemeralKey: viewerPublic.base64EncodedString(), phaseKey: key.phaseKey,
                nonce: Data(repeating: 0x62, count: 12))
            let digest = pairingSHA256Hex(try CloudPairing.encodeWrapper(wrapper))
            activate = wrapper
            activateSHA = digest
            return .ready(pairingID: pairingID, phase: phase, wrapper: wrapper,
                          phaseSHA256: digest, recordedAtMilliseconds: 100_030,
                          finalized: false)
        default:
            throw CloudAccountError.invalidResponse
        }
    }

    func writeIdentityPairingPhase(
        pairingID: String, phase: CloudPairingPhase, claimNonce: String,
        wrapper: CloudPairingWrapper
    ) async throws -> CloudIdentityPairingPhaseReceipt {
        let bytes = try CloudPairing.encodeWrapper(wrapper)
        let digest = pairingSHA256Hex(bytes)
        let duplicate: Bool
        switch phase {
        case .grant:
            duplicate = grant != nil
            if let grant { guard grant == wrapper else { throw CloudAccountError.invalidResponse } }
            else { grant = wrapper; grantSHA = digest }
            if !lostGrantReply { lostGrantReply = true; throw IdentityPairingReplyLoss.afterAcceptedWrite }
        case .confirm:
            duplicate = confirm != nil
            if let confirm { guard confirm == wrapper else { throw CloudAccountError.invalidResponse } }
            else { confirm = wrapper }
            if !lostConfirmReply { lostConfirmReply = true; throw IdentityPairingReplyLoss.afterAcceptedWrite }
        default:
            throw CloudAccountError.invalidResponse
        }
        return CloudIdentityPairingPhaseReceipt(
            status: duplicate ? .duplicate : .recorded, pairingID: pairingID, phase: phase,
            phaseSHA256: digest, recordedAtMilliseconds: 100_020,
            epochs: CloudIdentityEpochs(
                identityEpoch: 1, machineKeyEpoch: 1, viewerKeyEpoch: 1,
                contentKeyEpoch: 1, jwksGeneration: 1))
    }
}

public func runCloudPairingTests() async throws -> Int {
    var t = CloudPairingTestHarness()

    try t.equal(
        pairingSHA256Hex(CloudIdentityPairingWireContract.canonicalVector),
        "2f79369d4ee866976c6da2a41358c1aab5321ab0e6054bf8a3afe16e215f69a9",
        "public and private identity routes share the sealed canonical vector")
    try t.equal(
        pairingSHA256Hex(CloudIdentityPairingWireContract.canonicalLifecycleVector),
        "79504ce608fd278cecdb2e26f62d6d1c7e915ef66f94a79cc12ff240a95e410e",
        "public lifecycle vector pins reply replay, exact members, epochs and confirm timing")
    let identityStartBody = CloudIdentityPairingStartRequest(
        machineSigningKey: "signing", machineFingerprint: "fingerprint",
        machineEphemeralKey: "ephemeral", pairingNonce: "nonce",
        previousContentKeyEpoch: 4, contentKeyEpoch: 5, rotationID: "rotation-5")
    try t.equal(
        String(decoding: identityStartBody.canonicalBody, as: UTF8.self),
        "{\"content_key_epoch\":5,\"machine_ephemeral_key\":\"ephemeral\","
            + "\"machine_fingerprint\":\"fingerprint\",\"machine_signing_key\":\"signing\","
            + "\"pairing_nonce\":\"nonce\",\"previous_content_key_epoch\":4,"
            + "\"rotation_id\":\"rotation-5\",\"v\":1}",
        "identity start request uses exact canonical member names and ordering")

    let routeHTTP = IdentityPairingRouteHTTP()
    let routeStore = CloudPairingIdentityTestStore()
    try routeStore.set(
        try JSONEncoder().encode(CloudMachineCredential(
            accountID: "account-route", machineID: "machine-route",
            secret: "route-secret")),
        for: CloudAccountClient.machineCredentialAccount)
    let routeSigning = try CloudDeviceKeyPair(
        privateKeyRaw: Data(repeating: 0x01, count: 32))
    let routeEphemeral = try CloudPairing.x25519PublicKey(
        privateKeyRaw: Data(repeating: 0x02, count: 32))
    let routeRequest = CloudIdentityPairingStartRequest(
        machineSigningKey: routeSigning.publicKeyRaw.base64EncodedString(),
        machineFingerprint: routeSigning.pairingFingerprint,
        machineEphemeralKey: routeEphemeral.base64EncodedString(),
        pairingNonce: Data(repeating: 0x03, count: 32).base64EncodedString(),
        previousContentKeyEpoch: 0, contentKeyEpoch: 1, rotationID: "rotation-route")
    let routeQR = CloudPairingQR(
        pairingID: "pairing-route", claimNonce: Data(repeating: 0x04, count: 32).base64EncodedString(),
        expiresAt: 600_000, accountID: "account-route", machineID: "machine-route",
        machineSigningKey: routeRequest.machineSigningKey,
        machineFingerprint: routeRequest.machineFingerprint,
        machineEphemeralKey: routeRequest.machineEphemeralKey,
        pairingNonce: routeRequest.pairingNonce)
    let routeWindow: CloudJSONValue = .object([
        "old_epoch": .int(0), "new_epoch": .int(1),
        "old_accept_until_ms": .int(600_000), "new_accept_from_ms": .int(1)
    ])
    let routeJWKS: CloudJSONValue = .object([
        "old_generation": .int(0), "new_generation": .int(1),
        "old_accept_until_ms": .int(600_000), "new_accept_from_ms": .int(1)
    ])
    await routeHTTP.enqueue(bytes: CloudCanonicalJSON.canonicalData(.object([
        "v": .int(1), "status": .string("pending"), "qr": routeQR.cloudJSONValue,
        "identity": .object([
            "identity_epoch": .int(1), "machine_key_epoch": .int(1),
            "jwks_generation": .int(1)
        ]),
        "rotation": .object([
            "rotation_id": .string("rotation-route"), "signing": routeWindow,
            "jwks": routeJWKS, "content_key": routeWindow
        ])
    ])))
    let routeClient = CloudAccountClient(
        apiBaseURL: URL(string: "https://api.example.invalid")!, transport: routeHTTP,
        credentialStore: routeStore, deviceKeyLoader: { routeSigning })
    let routeStart = try await routeClient.startIdentityPairing(routeRequest)
    try t.equal(routeStart.qr, routeQR, "identity/start strictly decodes the exact QR echo")
    await routeHTTP.enqueue(status: 202, bytes: CloudCanonicalJSON.canonicalData(.object([
        "v": .int(1), "status": .string("pending"),
        "pairing_id": .string(routeQR.pairingID), "phase": .string("offer")
    ])))
    let routePoll = try await routeClient.pollIdentityPairingPhase(
        pairingID: routeQR.pairingID, phase: .offer, claimNonce: routeQR.claimNonce)
    try t.equal(routePoll, .pending(pairingID: routeQR.pairingID, phase: .offer),
                "identity/poll preserves the typed 202 pending state")
    let routeGrant = CloudPairingWrapper(
        phase: .grant, pairingID: routeQR.pairingID, senderDeviceID: routeQR.machineID,
        ephemeralKey: routeQR.machineEphemeralKey,
        nonce: Data(repeating: 0x05, count: 12).base64EncodedString(),
        ciphertext: Data(repeating: 0x06, count: 16).base64EncodedString())
    await routeHTTP.enqueue(bytes: CloudCanonicalJSON.canonicalData(.object([
        "v": .int(1), "status": .string("recorded"),
        "pairing_id": .string(routeQR.pairingID), "phase": .string("grant"),
        "phase_sha256": .string(String(repeating: "a", count: 64)),
        "recorded_at_ms": .int(10), "identity_epoch": .int(1),
        "machine_key_epoch": .int(1), "viewer_key_epoch": .int(1),
        "content_key_epoch": .int(1), "jwks_generation": .int(1)
    ])))
    _ = try await routeClient.writeIdentityPairingPhase(
        pairingID: routeQR.pairingID, phase: .grant,
        claimNonce: routeQR.claimNonce, wrapper: routeGrant)
    await routeHTTP.enqueue(bytes: CloudCanonicalJSON.canonicalData(.object([
        "v": .int(1), "status": .string("rotated"),
        "device_id": .string("machine-route"), "identity_epoch": .int(2),
        "key_epoch": .int(2), "capability_epoch": .int(1),
        "key_fingerprint": .string(routeSigning.pairingFingerprint),
        "old_accept_until_ms": .int(600_000), "new_accept_from_ms": .int(1)
    ])))
    _ = try await routeClient.rotateMachineIdentity(
        machineID: "machine-route", expectedKeyEpoch: 1,
        publicKey: routeSigning.publicKeyRaw.base64EncodedString(),
        fingerprint: routeSigning.pairingFingerprint)
    let routeRequests = await routeHTTP.captured()
    try t.equal(routeRequests.map { $0.url!.path }, [
        CloudIdentityPairingWireContract.startPath,
        CloudIdentityPairingWireContract.pollPath,
        CloudIdentityPairingWireContract.phasePathPrefix + "grant",
        "/v1/machines/machine-route/identity/rotate"
    ], "public client emits the exact identity route identities")
    try t.equal(routeRequests[0].httpBody, routeRequest.canonicalBody,
                "identity/start sends the canonical request bytes unchanged")
    try t.equal(routeRequests[1].httpBody, CloudCanonicalJSON.canonicalData(.object([
        "pairing_id": .string(routeQR.pairingID), "phase": .string("offer"),
        "claim_nonce": .string(routeQR.claimNonce)
    ])), "identity/poll sends its exact three-member canonical body")
    try t.equal(routeRequests[2].httpBody,
                try CloudPairing.encodePhaseWriteBody(
                    claimNonce: routeQR.claimNonce, wrapper: routeGrant),
                "phase write sends exact canonical bytes used for duplicate detection")
    try t.equal(routeRequests[3].httpBody, CloudCanonicalJSON.canonicalData(.object([
        "expected_key_epoch": .int(1),
        "public_key": .string(routeSigning.publicKeyRaw.base64EncodedString()),
        "fingerprint": .string(routeSigning.pairingFingerprint)
    ])), "machine rotation sends the exact canonical epoch/key/fingerprint body")
    var machineOffer = routeGrant
    machineOffer.phase = .offer
    do {
        _ = try await routeClient.writeIdentityPairingPhase(
            pairingID: routeQR.pairingID, phase: .offer,
            claimNonce: routeQR.claimNonce, wrapper: machineOffer)
        throw CloudPairingTestFailure.failed("machine wrote the viewer-owned offer phase")
    } catch CloudAccountError.invalidResponse {}

    let invitationSecret = Data((0..<32).map(UInt8.init))
    let invitation = try CloudPairingInvitation(
        invitationID: "invite-vector-01", secret: invitationSecret,
        expiresAtMilliseconds: 1_900_000)
    try t.equal(
        invitation.secretHash.base64EncodedString(),
        "Yw3NKWbEM2aRElRIu7JbT/QSpJxzLbLIq8G4WBvXEN0=",
        "invitation sends only the SHA-256 of its QR secret to Cloud")
    guard let invitationURL = invitation.qrURL() else {
        throw CloudPairingTestFailure.failed("invitation URL was not made")
    }
    try t.equal(invitationURL.host, "app.clawdline.com", "invitation QR uses the public console")
    try t.check(invitationURL.query == nil && invitationURL.fragment?.hasPrefix("pair=") == true,
                "invitation secret stays in the URL fragment and never reaches the server")
    let invitationPlaintext = "viewer-offer-fragment"
    let invitationNonce = try AES.GCM.Nonce(data: Data(repeating: 7, count: 12))
    let invitationAAD = Data("clawdline-pairing-invitation-v1\0invite-vector-01".utf8)
    let invitationBox = try AES.GCM.seal(
        Data(invitationPlaintext.utf8), using: SymmetricKey(data: invitationSecret),
        nonce: invitationNonce, authenticating: invitationAAD)
    guard let invitationCombined = invitationBox.combined else {
        throw CloudPairingTestFailure.failed("AES-GCM did not make combined bytes")
    }
    let invitationBlob = try CloudOpaquePairingBlob(
        base64: invitationCombined.base64EncodedString())
    try t.equal(
        try invitation.openEncryptedOffer(invitationBlob, nowMilliseconds: 1_800_000),
        invitationPlaintext, "Mac opens only the offer encrypted by the scanned QR secret")
    try t.rejects("another QR secret cannot open the relayed offer") {
        let other = try CloudPairingInvitation(
            invitationID: invitation.invitationID, secret: Data(repeating: 9, count: 32),
            expiresAtMilliseconds: invitation.expiresAtMilliseconds)
        _ = try other.openEncryptedOffer(invitationBlob, nowMilliseconds: 1_800_000)
    }
    try t.rejects("an expired QR cannot open a relayed offer") {
        _ = try invitation.openEncryptedOffer(invitationBlob, nowMilliseconds: 1_900_001)
    }

    let viewerPrivate = pairingHex("77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
    let viewerPublicText = "hSDwCYkwp1R0i33ctD73Wg2/Og0mOBr066SpjqqbTmo="
    let macPrivate = pairingHex("5dab087e624a8a4b79e17f8b83800ee66f3bb1292618b6fd1c2f8b27ff88e0eb")
    let macPublicText = "3p7bfXt9wbTTW2HC7OQ1Nz+DQ8hbeGdNrfx+FG+IK08="
    let pairingID = "pair-v0-vector-01"
    let pairingNonceText = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
    let claimNonceText = "ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="
    let viewerPublic = try CloudPairing.decodeCanonicalBase64(viewerPublicText, field: "viewer_public", expectedLength: 32)
    let macPublic = try CloudPairing.decodeCanonicalBase64(macPublicText, field: "mac_public", expectedLength: 32)
    let pairingNonce = try CloudPairing.decodeCanonicalBase64(pairingNonceText, field: "pairing_nonce", expectedLength: 32)
    let claimNonce = try CloudPairing.decodeCanonicalBase64(claimNonceText, field: "claim_nonce", expectedLength: 32)

    try t.equal(viewerPublic.count, 32, "viewer public decoded length")
    try t.equal(macPublic.count, 32, "Mac public decoded length")
    try t.equal(pairingNonce.count, 32, "pairing nonce decoded length")
    try t.equal(claimNonce.count, 32, "claim nonce decoded length")

    let viewerShared = try CloudPairing.x25519SharedSecret(privateKeyRaw: viewerPrivate, peerPublicKeyRaw: macPublic)
    let macShared = try CloudPairing.x25519SharedSecret(privateKeyRaw: macPrivate, peerPublicKeyRaw: viewerPublic)
    try t.equal(viewerShared, macShared, "viewer and Mac derive identical raw X25519 secret")
    try t.equal(pairingHexString(viewerShared), "4a5d9d5ba4ce2de1728e3bf480350f25e07e21c947d19e3376f09b3c1e161742", "RFC 7748 shared vector")

    let offer = try CloudPairing.derive(
        sharedSecretRaw: viewerShared,
        pairingNonce: pairingNonce,
        pairingID: pairingID,
        claimNonce: claimNonce,
        phase: .offer
    )
    try t.equal(pairingHexString(offer.saltPreimage), "0016636c6177646c696e652d706169722d73616c742d76310020000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f0011706169722d76302d766563746f722d30310020202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f", "salt preimage vector")
    try t.equal(pairingHexString(offer.salt), "e44434ff069bc3b7623b4cf6dcba3159dae6d3654d3ec3b21025908f0d2d83de", "salt vector")
    try t.equal(pairingHexString(offer.prk), "5e0266fe8371085a05b68248654395fd8461544ff59fad960fdc7f84f73076d3", "PRK vector")
    try t.equal(pairingHexString(try CloudPairing.claimNonceSHA256(claimNonce)), "72dbb7336c76780023f83da4c355f2eeea85733b13d3477697917790c1229084", "raw claim nonce SHA-256 vector")

    let infoHex: [CloudPairingPhase: String] = [
        .offer: "0011636c6177646c696e652d706169722d763100056f66666572",
        .grant: "0011636c6177646c696e652d706169722d763100056772616e74",
        .activate: "0011636c6177646c696e652d706169722d763100086163746976617465",
        .confirm: "0011636c6177646c696e652d706169722d76310007636f6e6669726d"
    ]
    let keyHex: [CloudPairingPhase: String] = [
        .offer: "32f436fd04a67f1dfda4dbe730e9b5795ed3d978e7918adf547f5081b471ae30",
        .grant: "f020d0f73b1a3465dfe32f8f306ce0739ebf030b061bac28ccf25e7b098830b2",
        .activate: "bf5cc0b899aa9a4461502b72b55098e719451cdc4a94c97d6c0cc9d0b9573a68",
        .confirm: "e95160d2f6971234fe9e225663cfebc4b6986132a2aba4d5ce79f892bff13d74"
    ]
    var derivedByPhase: [CloudPairingPhase: CloudPairingDerivedMaterial] = [:]
    for phase in CloudPairingPhase.allCases {
        let material = try CloudPairing.derive(sharedSecretRaw: macShared, pairingNonce: pairingNonce, pairingID: pairingID, claimNonce: claimNonce, phase: phase)
        derivedByPhase[phase] = material
        try t.equal(pairingHexString(material.info), infoHex[phase]!, "\(phase.rawValue) info vector")
        try t.equal(pairingHexString(material.phaseKey), keyHex[phase]!, "\(phase.rawValue) phase-key vector")
        try t.equal(material.salt, offer.salt, "\(phase.rawValue) salt is phase-independent")
        try t.equal(material.prk, offer.prk, "\(phase.rawValue) PRK is phase-independent")
    }

    let senderByPhase: [CloudPairingPhase: String] = [
        .offer: "viewer-vector-01", .grant: "mac-vector-01",
        .activate: "viewer-vector-01", .confirm: "mac-vector-01"
    ]
    let keyByPhase: [CloudPairingPhase: String] = [
        .offer: viewerPublicText, .grant: macPublicText,
        .activate: viewerPublicText, .confirm: macPublicText
    ]
    let aadLength: [CloudPairingPhase: Int] = [.offer: 157, .grant: 154, .activate: 160, .confirm: 156]
    let aadHash: [CloudPairingPhase: String] = [
        .offer: "0403f78cea3e220621158e90483f90fa856502e2022230b37d6cce15f65770b5",
        .grant: "d2cf2f77786913dac20d1c2808c4997ba1f929ea882184337d8cea72350f5cfe",
        .activate: "01054c2e72f7c7783a8656231855f63c345a6ef4e7bbc61ec9fa66f8893b47fe",
        .confirm: "16797d25db4258b21d532e852daaf6160c1f714d1d3b9ee8f90f530a2edf68ad"
    ]
    var wrappers: [CloudPairingPhase: CloudPairingWrapper] = [:]
    for phase in CloudPairingPhase.allCases {
        let wrapper = CloudPairingWrapper(
            phase: phase,
            pairingID: pairingID,
            senderDeviceID: senderByPhase[phase]!,
            ephemeralKey: keyByPhase[phase]!,
            nonce: Data(repeating: UInt8(phase.rawValue.count), count: 12).base64EncodedString(),
            ciphertext: Data(repeating: 0xA5, count: 16).base64EncodedString()
        )
        wrappers[phase] = wrapper
        let aad = try CloudPairing.aad(for: wrapper)
        let exact = "{\"ephemeral_key\":\"\(keyByPhase[phase]!)\",\"pairing_id\":\"pair-v0-vector-01\",\"phase\":\"\(phase.rawValue)\",\"sender_device_id\":\"\(senderByPhase[phase]!)\",\"v\":1}"
        try t.equal(String(decoding: aad, as: UTF8.self), exact, "\(phase.rawValue) exact AAD bytes")
        try t.equal(aad.count, aadLength[phase]!, "\(phase.rawValue) AAD length")
        try t.equal(pairingSHA256Hex(aad), aadHash[phase]!, "\(phase.rawValue) AAD SHA-256")
    }

    try t.rejects(.zeroSharedSecret, "all-zero shared secret rejected before KDF") {
        _ = try CloudPairing.derive(sharedSecretRaw: Data(repeating: 0, count: 32), pairingNonce: pairingNonce, pairingID: pairingID, claimNonce: claimNonce, phase: .offer)
    }
    try t.rejects("pairing nonce wire text is not accepted as KDF bytes") {
        _ = try CloudPairing.derive(sharedSecretRaw: viewerShared, pairingNonce: Data(pairingNonceText.utf8), pairingID: pairingID, claimNonce: claimNonce, phase: .offer)
    }
    try t.rejects("claim nonce wire text is not accepted as KDF bytes") {
        _ = try CloudPairing.derive(sharedSecretRaw: viewerShared, pairingNonce: pairingNonce, pairingID: pairingID, claimNonce: Data(claimNonceText.utf8), phase: .offer)
    }
    try t.rejects("claim nonce 31-byte boundary rejected") {
        _ = try CloudPairing.derive(sharedSecretRaw: viewerShared, pairingNonce: pairingNonce, pairingID: pairingID, claimNonce: Data(claimNonce.dropLast()), phase: .offer)
    }
    try t.rejects("claim nonce 33-byte boundary rejected") {
        _ = try CloudPairing.derive(sharedSecretRaw: viewerShared, pairingNonce: pairingNonce, pairingID: pairingID, claimNonce: claimNonce + Data([0]), phase: .offer)
    }
    try t.rejects("pairing nonce 31-byte KDF boundary rejected") {
        _ = try CloudPairing.derive(sharedSecretRaw: viewerShared, pairingNonce: Data(pairingNonce.dropLast()), pairingID: pairingID, claimNonce: claimNonce, phase: .offer)
    }
    try t.rejects("pairing nonce 33-byte KDF boundary rejected") {
        _ = try CloudPairing.derive(sharedSecretRaw: viewerShared, pairingNonce: pairingNonce + Data([0]), pairingID: pairingID, claimNonce: claimNonce, phase: .offer)
    }
    try t.equal(try CloudPairing.l16(Data()), Data([0, 0]), "L16 zero-byte lower boundary")
    let l16Maximum = try CloudPairing.l16(Data(repeating: 0x5A, count: 65_535))
    try t.equal(l16Maximum.count, 65_537, "L16 65,535-byte upper boundary accepted")
    try t.equal(l16Maximum.prefix(2), Data([0xFF, 0xFF]), "L16 upper-bound big-endian prefix")
    try t.rejects("L16 65,536-byte rejection boundary") {
        _ = try CloudPairing.l16(Data(repeating: 0, count: 65_536))
    }
    let changedID = try CloudPairing.derive(sharedSecretRaw: viewerShared, pairingNonce: pairingNonce, pairingID: "pair-v0-vector-00", claimNonce: claimNonce, phase: .offer)
    try t.check(changedID.salt != offer.salt, "one pairing_id byte changes salt")
    try t.check(changedID.phaseKey != offer.phaseKey, "one pairing_id byte changes phase key")
    try t.check(derivedByPhase[.grant]!.info != offer.info, "phase changes info")
    try t.check(derivedByPhase[.grant]!.phaseKey != offer.phaseKey, "phase changes key")
    let noL16Preimage = Data("clawdline-pair-salt-v1".utf8) + pairingNonce + Data(pairingID.utf8) + claimNonce
    try t.check(noL16Preimage != offer.saltPreimage, "removing L16 changes salt preimage")
    try t.check(Data(SHA256.hash(data: noL16Preimage)) != offer.salt, "removing L16 changes salt")
    try t.check((try CloudPairing.claimNonceSHA256(claimNonce)) != Data(SHA256.hash(data: Data(claimNonceText.utf8))), "claim hash is raw bytes, not base64 text")
    try t.rejects("base64 padding removal rejected") {
        _ = try CloudPairing.decodeCanonicalBase64(String(viewerPublicText.dropLast()), field: "key", expectedLength: 32)
    }
    try t.rejects("base64url alphabet substitution rejected") {
        _ = try CloudPairing.decodeCanonicalBase64(CloudPairing.encodeCanonicalBase64URL(macPublic), field: "key", expectedLength: 32)
    }

    let offerWrapper = wrappers[.offer]!
    let aadWithNonceAndCT = CloudCanonicalJSON.canonicalData(.object([
        "v": .int(1), "phase": .string("offer"), "pairing_id": .string(pairingID),
        "sender_device_id": .string("viewer-vector-01"), "ephemeral_key": .string(viewerPublicText),
        "nonce": .string(offerWrapper.nonce), "ct": .string(offerWrapper.ciphertext)
    ]))
    try t.check(aadWithNonceAndCT != (try CloudPairing.aad(for: offerWrapper)), "AAD removes nonce and ct")
    var changedSender = offerWrapper
    changedSender.senderDeviceID = "viewer-vector-02"
    try t.check((try CloudPairing.aad(for: changedSender)) != (try CloudPairing.aad(for: offerWrapper)), "sender mutation changes AAD")
    var changedEphemeral = offerWrapper
    changedEphemeral.ephemeralKey = replacingLastByte(viewerPublic).base64EncodedString()
    try t.check((try CloudPairing.aad(for: changedEphemeral)) != (try CloudPairing.aad(for: offerWrapper)), "ephemeral-key mutation changes AAD")

    for phase in [CloudPairingPhase.offer, .activate] {
        var swapped = wrappers[phase]!
        swapped.ephemeralKey = macPublicText
        try t.rejects(.keyBindingMismatch, "\(phase.rawValue) rejects Mac key before AEAD") {
            _ = try CloudPairing.open(swapped, phaseKey: derivedByPhase[phase]!.phaseKey, viewerEphemeralKey: viewerPublicText, machineEphemeralKey: macPublicText)
        }
    }
    for phase in [CloudPairingPhase.grant, .confirm] {
        var swapped = wrappers[phase]!
        swapped.ephemeralKey = viewerPublicText
        try t.rejects(.keyBindingMismatch, "\(phase.rawValue) rejects Viewer key before AEAD") {
            _ = try CloudPairing.open(swapped, phaseKey: derivedByPhase[phase]!.phaseKey, viewerEphemeralKey: viewerPublicText, machineEphemeralKey: macPublicText)
        }
    }

    let plaintext = Data("pairing secret payload".utf8)
    let sealed = try CloudPairing.seal(
        plaintext: plaintext,
        phase: .grant,
        pairingID: pairingID,
        senderDeviceID: "mac-vector-01",
        ephemeralKey: macPublicText,
        phaseKey: derivedByPhase[.grant]!.phaseKey,
        nonce: Data((0..<12).map(UInt8.init))
    )
    try t.equal(try CloudPairing.open(sealed, phaseKey: derivedByPhase[.grant]!.phaseKey, viewerEphemeralKey: viewerPublicText, machineEphemeralKey: macPublicText), plaintext, "AES-GCM seal/open round trip")
    let encodedWrapper = try CloudPairing.encodeWrapper(sealed)
    try t.equal(try CloudPairing.decodeWrapper(encodedWrapper), sealed, "seven-field wrapper canonical round trip")
    try t.rejects("nonce 11-byte boundary rejected") {
        var invalid = sealed
        invalid.nonce = Data(repeating: 0, count: 11).base64EncodedString()
        _ = try CloudPairing.decodeWrapper(CloudPairing.encodeWrapperUnchecked(invalid))
    }
    try t.rejects("nonce 13-byte boundary rejected") {
        var invalid = sealed
        invalid.nonce = Data(repeating: 0, count: 13).base64EncodedString()
        _ = try CloudPairing.decodeWrapper(CloudPairing.encodeWrapperUnchecked(invalid))
    }
    try t.rejects("wrapper extra field rejected") {
        guard case .object(var object) = try CloudCanonicalJSON.parseStrict(encodedWrapper) else { return }
        object["extra"] = .int(1)
        _ = try CloudPairing.decodeWrapper(CloudCanonicalJSON.canonicalData(.object(object)))
    }
    try t.rejects("wrapper missing field rejected") {
        guard case .object(var object) = try CloudCanonicalJSON.parseStrict(encodedWrapper) else { return }
        object.removeValue(forKey: "ct")
        _ = try CloudPairing.decodeWrapper(CloudCanonicalJSON.canonicalData(.object(object)))
    }
    var tampered = sealed
    var tamperedBytes = try CloudPairing.decodeCanonicalBase64(tampered.ciphertext, field: "ct", minimumLength: 16)
    tamperedBytes[0] ^= 1
    tampered.ciphertext = tamperedBytes.base64EncodedString()
    try t.rejects(.authenticationFailed, "tampered ciphertext rejected") {
        _ = try CloudPairing.open(tampered, phaseKey: derivedByPhase[.grant]!.phaseKey, viewerEphemeralKey: viewerPublicText, machineEphemeralKey: macPublicText)
    }

    try t.equal(PAIRING_PHASE_MAX_BYTES, 65_536, "phase body limit constant")

    // The §5.2 boundary: PAIRING_PHASE_MAX_BYTES caps the COMPLETE two-member phase-write
    // body {claim_nonce, blob} — claim nonce, wrapper, base64 ct and every field included.
    let boundaryWrapper = phaseWriteWrapper(bodyExactByteCount: 65_536, claimNonce: claimNonceText, ephemeralKey: viewerPublicText)
    let maximumWriteBody = CloudPairing.encodePhaseWriteBodyUnchecked(claimNonce: claimNonceText, wrapper: boundaryWrapper)
    try t.equal(maximumWriteBody.count, 65_536, "constructed complete canonical 65,536-byte two-member phase-write body")
    let decodedMaximum = try CloudPairing.decodePhaseWriteBody(maximumWriteBody)
    try t.equal(decodedMaximum.claimNonce, claimNonceText, "65,536-byte complete phase-write body accepted; claim nonce round trips")
    try t.equal(CloudPairing.encodePhaseWriteBodyUnchecked(claimNonce: decodedMaximum.claimNonce, wrapper: decodedMaximum.wrapper), maximumWriteBody, "65,536-byte complete phase-write body re-encodes to identical bytes")
    let oversizedWrapper = phaseWriteWrapper(bodyExactByteCount: 65_537, claimNonce: claimNonceText, ephemeralKey: viewerPublicText)
    let oversizedWriteBody = CloudPairing.encodePhaseWriteBodyUnchecked(claimNonce: claimNonceText, wrapper: oversizedWrapper)
    try t.equal(oversizedWriteBody.count, 65_537, "constructed complete canonical 65,537-byte two-member phase-write body")
    try t.rejects(.phaseBodyTooLarge(actual: 65_537), "65,537-byte complete phase-write body rejected") {
        _ = try CloudPairing.decodePhaseWriteBody(oversizedWriteBody)
    }
    try t.rejects(.phaseBodyTooLarge(actual: 65_537), "encode side rejects the 65,537-byte complete phase-write body") {
        _ = try CloudPairing.encodePhaseWriteBody(claimNonce: claimNonceText, wrapper: oversizedWrapper)
    }

    let sealedWriteBody = try CloudPairing.encodePhaseWriteBody(claimNonce: claimNonceText, wrapper: sealed)
    let decodedSealed = try CloudPairing.decodePhaseWriteBody(sealedWriteBody)
    try t.equal(decodedSealed.wrapper, sealed, "phase-write body round-trips the sealed wrapper")
    try t.equal(decodedSealed.claimNonce, claimNonceText, "phase-write body round-trips the claim nonce")
    try t.rejects(.invalidPhaseBodyFields, "phase-write body extra member rejected") {
        guard case .object(var members) = try CloudCanonicalJSON.parseStrict(sealedWriteBody) else { return }
        members["extra"] = .int(1)
        _ = try CloudPairing.decodePhaseWriteBody(CloudCanonicalJSON.canonicalData(.object(members)))
    }
    try t.rejects(.invalidPhaseBodyFields, "phase-write body missing claim_nonce rejected") {
        guard case .object(var members) = try CloudCanonicalJSON.parseStrict(sealedWriteBody) else { return }
        members.removeValue(forKey: "claim_nonce")
        _ = try CloudPairing.decodePhaseWriteBody(CloudCanonicalJSON.canonicalData(.object(members)))
    }
    try t.rejects(.invalidPhaseBodyFields, "phase-write body string blob rejected") {
        _ = try CloudPairing.decodePhaseWriteBody(CloudCanonicalJSON.canonicalData(.object([
            "claim_nonce": .string(claimNonceText),
            "blob": .string(String(decoding: CloudPairing.encodeWrapperUnchecked(sealed), as: UTF8.self))
        ])))
    }
    try t.rejects(.invalidLength(field: "claim_nonce", expected: "32", actual: 31), "phase-write body claim nonce 31-byte boundary rejected") {
        _ = try CloudPairing.decodePhaseWriteBody(CloudPairing.encodePhaseWriteBodyUnchecked(claimNonce: Data(repeating: 1, count: 31).base64EncodedString(), wrapper: sealed))
    }
    try t.rejects(.invalidBase64(field: "claim_nonce"), "phase-write body unpadded claim nonce rejected") {
        _ = try CloudPairing.decodePhaseWriteBody(CloudPairing.encodePhaseWriteBodyUnchecked(claimNonce: String(claimNonceText.dropLast()), wrapper: sealed))
    }
    try t.rejects(CloudCanonicalJSONError.notCanonical, "non-canonical whole-body bytes rejected") {
        var loose = [UInt8](sealedWriteBody)
        let comma = loose.lastIndex(of: UInt8(ascii: ","))!
        loose.insert(UInt8(ascii: " "), at: comma + 1)
        _ = try CloudPairing.decodePhaseWriteBody(Data(loose))
    }

    // The wrapper layer keeps a conservative application of the same cap: wrapper bytes are
    // strictly smaller than any complete body embedding them, so this can only over-reject,
    // never admit a body the full-body boundary above would refuse.
    let maximumWrapperBytes = canonicalWrapperBytes(exactByteCount: 65_536, ephemeralKey: viewerPublicText)
    try t.equal(maximumWrapperBytes.count, 65_536, "constructed canonical 65,536-byte wrapper bytes")
    _ = try CloudPairing.decodeWrapper(maximumWrapperBytes)
    try t.check(true, "wrapper-layer conservative cap admits 65,536-byte wrapper bytes")
    let oversizedWrapperBytes = canonicalWrapperBytes(exactByteCount: 65_537, ephemeralKey: viewerPublicText)
    try t.equal(oversizedWrapperBytes.count, 65_537, "constructed canonical 65,537-byte wrapper bytes")
    try t.rejects(.phaseBodyTooLarge(actual: 65_537), "wrapper-layer conservative cap rejects 65,537-byte wrapper bytes") {
        _ = try CloudPairing.decodeWrapper(oversizedWrapperBytes)
    }

    let fingerprint = try CloudPairing.ed25519Fingerprint(publicKeyRaw: macPublic)
    try t.equal(fingerprint, "6NPF-MFQW-BIYL-6PDO", "fingerprint SHA-256/base32 vector")
    try t.equal(fingerprint.split(separator: "-").map(String.init), ["6NPF", "MFQW", "BIYL", "6PDO"], "fingerprint four-character grouping")
    try t.check(fingerprint.allSatisfy { $0 == "-" || ($0 >= "A" && $0 <= "Z") || ($0 >= "2" && $0 <= "7") }, "fingerprint RFC 4648 uppercase alphabet")

    let now: Int64 = 1_800_000_000_000
    let qr = CloudPairingQR(
        pairingID: pairingID,
        claimNonce: claimNonceText,
        expiresAt: now + 600_000,
        accountID: "account-vector-01",
        machineID: "machine-vector-01",
        machineSigningKey: macPublicText,
        machineFingerprint: fingerprint,
        machineEphemeralKey: macPublicText,
        pairingNonce: pairingNonceText
    )
    let fragment = try CloudPairing.encodeQRFragment(qr)
    try t.check(!fragment.contains("="), "QR fragment is unpadded")
    try t.check(!fragment.contains("+") && !fragment.contains("/"), "QR fragment uses base64url alphabet")
    try t.equal(try CloudPairing.decodeQRFragment(fragment, nowMilliseconds: now), qr, "canonical QR round trip and ten-minute upper boundary")
    let qrJSON = try CloudPairing.decodeCanonicalBase64URL(fragment)
    try t.equal(try CloudCanonicalJSON.parseStrict(qrJSON), qr.cloudJSONValue, "QR decoded bytes are canonical eleven-field JSON")
    try t.rejects("QR padded re-encoding rejected") {
        _ = try CloudPairing.decodeQRFragment(fragment + "=", nowMilliseconds: now)
    }
    let standardFragment = qrJSON.base64EncodedString().replacingOccurrences(of: "=", with: "")
    if standardFragment != fragment {
        try t.rejects("QR standard alphabet rejected") {
            _ = try CloudPairing.decodeQRFragment(standardFragment, nowMilliseconds: now)
        }
    } else {
        try t.check(true, "QR vector has no alphabet-distinguishing sextet")
    }
    var expired = qr
    expired.expiresAt = now - 1
    try t.rejects(.qrExpired, "QR expiry lower boundary rejected") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragment(expired), nowMilliseconds: now)
    }
    var tooFar = qr
    tooFar.expiresAt = now + 600_001
    try t.rejects(.qrExpiryTooFar, "QR expiry upper boundary rejected") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragment(tooFar), nowMilliseconds: now)
    }
    // C1 narrowed the canonical-JSON integer domain to ±(2^53 − 1), so an expires_at past
    // 2^53 − 1 can no longer arrive through decodeQRFragment: parseStrict fails closed first
    // with its own typed error. The epoch guard is NOT dead code — three paths still reach
    // it, each pinned below.
    var unsafeEpoch = qr
    unsafeEpoch.expiresAt = 9_007_199_254_740_992
    try t.rejects(CloudCanonicalJSONError.integerOutOfRange("9007199254740992"), "wire expires_at 2^53 fails closed at the parse layer") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(unsafeEpoch), nowMilliseconds: now)
    }
    // Path 1: encodeQRFragment validates before serializing — parseStrict never runs.
    try t.rejects(.unsafeEpochMilliseconds, "encode-side epoch guard rejects expires_at 2^53") {
        _ = try CloudPairing.encodeQRFragment(unsafeEpoch)
    }
    // Path 2: parseStrict accepts negative safe integers, but the epoch domain is 0...2^53−1,
    // so a negative expires_at reaches the guard through the full decode path.
    var negativeEpoch = qr
    negativeEpoch.expiresAt = -1
    try t.rejects(.unsafeEpochMilliseconds, "wire expires_at -1 reaches the epoch guard through decodeQRFragment") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(negativeEpoch), nowMilliseconds: now)
    }
    negativeEpoch.expiresAt = -9_007_199_254_740_991
    try t.rejects(.unsafeEpochMilliseconds, "wire expires_at -(2^53-1) reaches the epoch guard through decodeQRFragment") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(negativeEpoch), nowMilliseconds: now)
    }
    // Path 3: nowMilliseconds is caller state and never passes through parseStrict.
    try t.rejects(.unsafeEpochMilliseconds, "caller clock below zero rejected by the epoch guard") {
        _ = try CloudPairing.decodeQRFragment(fragment, nowMilliseconds: -1)
    }
    try t.rejects(.unsafeEpochMilliseconds, "caller clock 2^53 rejected by the epoch guard") {
        _ = try CloudPairing.decodeQRFragment(fragment, nowMilliseconds: 9_007_199_254_740_992)
    }
    // Acceptance at both extremes of the guard's 0...2^53−1 domain.
    var epochExtremes = qr
    epochExtremes.expiresAt = 9_007_199_254_740_991
    try t.equal(try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragment(epochExtremes), nowMilliseconds: 9_007_199_254_740_991), epochExtremes, "expires_at and clock at 2^53-1 accepted")
    epochExtremes.expiresAt = 0
    try t.equal(try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragment(epochExtremes), nowMilliseconds: 0), epochExtremes, "expires_at and clock at zero accepted")
    var badFingerprint = qr
    badFingerprint.machineFingerprint = "AAAA-AAAA-AAAA-AAAA"
    try t.rejects(.fingerprintMismatch, "wire fingerprint must equal recomputed public-key fingerprint") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(badFingerprint), nowMilliseconds: now)
    }
    var shortClaim = qr
    shortClaim.claimNonce = Data(repeating: 1, count: 31).base64EncodedString()
    try t.rejects("QR claim nonce 31-byte boundary rejected") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(shortClaim), nowMilliseconds: now)
    }
    var longClaim = qr
    longClaim.claimNonce = Data(repeating: 1, count: 33).base64EncodedString()
    try t.rejects("QR claim nonce 33-byte boundary rejected") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(longClaim), nowMilliseconds: now)
    }
    var shortPairing = qr
    shortPairing.pairingNonce = Data(repeating: 1, count: 31).base64EncodedString()
    try t.rejects("QR pairing nonce 31-byte boundary rejected") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(shortPairing), nowMilliseconds: now)
    }
    var longPairing = qr
    longPairing.pairingNonce = Data(repeating: 1, count: 33).base64EncodedString()
    try t.rejects("QR pairing nonce 33-byte boundary rejected") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(longPairing), nowMilliseconds: now)
    }
    var reusedNonce = qr
    reusedNonce.claimNonce = reusedNonce.pairingNonce
    try t.rejects(.reusedNonce, "claim nonce cannot reuse pairing nonce") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(reusedNonce), nowMilliseconds: now)
    }
    var unpaddedKey = qr
    unpaddedKey.machineSigningKey = String(macPublicText.dropLast())
    try t.rejects("QR public key requires canonical base64 padding") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(unpaddedKey), nowMilliseconds: now)
    }
    try t.rejects("QR exact field set rejects extras") {
        guard case .object(var object) = qr.cloudJSONValue else { return }
        object["extra"] = .bool(true)
        let bytes = CloudCanonicalJSON.canonicalData(.object(object))
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeCanonicalBase64URL(bytes), nowMilliseconds: now)
    }
    try t.rejects("QR exact field set rejects omissions") {
        guard case .object(var object) = qr.cloudJSONValue else { return }
        object.removeValue(forKey: "account_id")
        let bytes = CloudCanonicalJSON.canonicalData(.object(object))
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeCanonicalBase64URL(bytes), nowMilliseconds: now)
    }
    var emptyID = qr
    emptyID.accountID = ""
    try t.rejects("ID zero-byte boundary rejected") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(emptyID), nowMilliseconds: now)
    }
    var longID = qr
    longID.machineID = String(repeating: "a", count: 129)
    try t.rejects("ID 129-byte boundary rejected") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(longID), nowMilliseconds: now)
    }
    var nonASCIIID = qr
    nonASCIIID.pairingID = "配對"
    try t.rejects("non-ASCII ID rejected") {
        _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(nonASCIIID), nowMilliseconds: now)
    }
    var oneByteIDs = qr
    oneByteIDs.pairingID = "p"
    oneByteIDs.accountID = "a"
    oneByteIDs.machineID = "m"
    try t.equal(try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragment(oneByteIDs), nowMilliseconds: now), oneByteIDs, "ID one-byte lower boundary accepted")
    var maximumIDs = qr
    maximumIDs.pairingID = String(repeating: "p", count: 128)
    maximumIDs.accountID = String(repeating: "a", count: 128)
    maximumIDs.machineID = String(repeating: "m", count: 128)
    try t.equal(try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragment(maximumIDs), nowMilliseconds: now), maximumIDs, "ID 128-byte upper boundary accepted")
    var expiresNow = qr
    expiresNow.expiresAt = now
    try t.equal(try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragment(expiresNow), nowMilliseconds: now), expiresNow, "expiry equal to now accepted")
    for length in [31, 33] {
        var signingLength = qr
        let signingRaw = Data(repeating: 7, count: length)
        signingLength.machineSigningKey = signingRaw.base64EncodedString()
        signingLength.machineFingerprint = (try? CloudPairing.ed25519Fingerprint(publicKeyRaw: signingRaw)) ?? fingerprint
        try t.rejects("machine signing key \(length)-byte boundary rejected") {
            _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(signingLength), nowMilliseconds: now)
        }
        var ephemeralLength = qr
        ephemeralLength.machineEphemeralKey = Data(repeating: 8, count: length).base64EncodedString()
        try t.rejects("machine ephemeral key \(length)-byte boundary rejected") {
            _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(ephemeralLength), nowMilliseconds: now)
        }
    }

    let generated = CloudPairing.generateIndependentNonces()
    try t.equal(generated.claimNonce.count, 32, "generated claim nonce is 256-bit")
    try t.equal(generated.pairingNonce.count, 32, "generated pairing nonce is 256-bit")
    try t.check(generated.claimNonce != generated.pairingNonce, "generated nonces are independent values")
    try t.equal(generated.claimNonce.base64EncodedString().count, 44, "generated claim nonce canonical padded wire length")
    try t.equal(generated.pairingNonce.base64EncodedString().count, 44, "generated pairing nonce canonical padded wire length")

    // Derived key material has no readable rendering. What is scanned for is the material
    // itself, in every form a diagnostic prints bytes in: `prk` and `phaseKey` are the AEAD
    // inputs, and `saltPreimage` carries the claim nonce and the pairing nonce verbatim.
    let renderedSecrets: [(String, Data)] = [
        ("PRK", offer.prk),
        ("phase key", offer.phaseKey),
        ("the claim nonce inside the salt preimage", claimNonce)
    ]
    for (secretName, secret) in renderedSecrets {
        for (rendering, text) in pairingRenderings(of: offer) {
            try t.check(
                !pairingRenderingLeaks(text, secret: secret),
                "\(rendering) of derived material does not expose \(secretName)"
            )
        }
    }
    // The byte scans above carry the security claim; these two pin the exact redacted
    // rendering, so removing the explicit `description` and falling back to the synthesised
    // one — which prints `saltPreimage: 106 bytes` and hides the key only because `Data`
    // describes itself that way — is a visible change rather than a silent one.
    let redactedRendering = "CloudPairingDerivedMaterial(saltPreimage: <redacted 111 bytes>, "
        + "salt: <redacted 32 bytes>, prk: <redacted 32 bytes>, info: <redacted 26 bytes>, "
        + "phaseKey: <redacted 32 bytes>)"
    try t.equal("\(offer)", redactedRendering, "derived material renders exactly the redacted form")
    try t.equal(String(reflecting: offer), redactedRendering, "derived material debug rendering is the same redacted form")

    // IDs are printable ASCII (0x20...0x7E), the same range `CloudCanonicalJSON.signingInput`
    // demands of a signing domain. Both sides of both boundaries, on every entry point that
    // validates an ID.
    for (scalar, controlName) in [(UInt32(0x00), "NUL"), (0x1F, "0x1F"), (0x7F, "DEL")] {
        let control = String(UnicodeScalar(scalar)!)
        try t.rejects(.invalidASCII(field: "pairing_id"), "KDF pairing_id rejects \(controlName)") {
            _ = try CloudPairing.derive(
                sharedSecretRaw: viewerShared, pairingNonce: pairingNonce,
                pairingID: "pair-" + control + "01", claimNonce: claimNonce, phase: .offer
            )
        }
        var controlSender = sealed
        controlSender.senderDeviceID = "mac-" + control + "01"
        try t.rejects(.invalidASCII(field: "sender_device_id"), "wrapper sender_device_id rejects \(controlName)") {
            _ = try CloudPairing.decodeWrapper(CloudPairing.encodeWrapperUnchecked(controlSender))
        }
        var controlAccount = qr
        controlAccount.accountID = "acct-" + control + "01"
        try t.rejects(.invalidASCII(field: "account_id"), "QR account_id rejects \(controlName)") {
            _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(controlAccount), nowMilliseconds: now)
        }
        var controlMachine = qr
        controlMachine.machineID = "mach-" + control + "01"
        try t.rejects(.invalidASCII(field: "machine_id"), "QR machine_id rejects \(controlName)") {
            _ = try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragmentUnchecked(controlMachine), nowMilliseconds: now)
        }
    }
    for (scalar, printableName) in [(UInt32(0x20), "space"), (0x7E, "tilde")] {
        let printable = String(UnicodeScalar(scalar)!)
        let printableMaterial = try CloudPairing.derive(
            sharedSecretRaw: viewerShared, pairingNonce: pairingNonce,
            pairingID: "pair" + printable + "01", claimNonce: claimNonce, phase: .offer
        )
        try t.check(printableMaterial.phaseKey != offer.phaseKey, "KDF pairing_id accepts \(printableName)")
        var printableSender = sealed
        printableSender.senderDeviceID = "mac" + printable + "01"
        try t.equal(
            try CloudPairing.decodeWrapper(CloudPairing.encodeWrapper(printableSender)), printableSender,
            "wrapper sender_device_id accepts \(printableName)"
        )
        var printableQR = qr
        printableQR.accountID = "acct" + printable + "01"
        printableQR.machineID = "mach" + printable + "01"
        try t.equal(
            try CloudPairing.decodeQRFragment(CloudPairing.encodeQRFragment(printableQR), nowMilliseconds: now), printableQR,
            "QR account_id and machine_id accept \(printableName)"
        )
    }

    // W5-2: both host adapters enter this same Application owner. This flat focused proof uses
    // an adapter with the identical `SecretStore` contract; Ubuntu additionally exercises its
    // real descriptor-checked file adapter in LinuxRuntimeContractTests.
    let identityStore = CloudPairingIdentityTestStore()
    let authority = CloudExecutorIdentityAuthority(store: identityStore)
    try t.equal(authority.readiness(), .blocked(.protectedStateMissing),
                "identity readiness never provisions an empty fallback")
    let executorKey = try CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 0x91, count: 32))
    let executorMaster = try CloudMasterSecret(rawRepresentation: Data(repeating: 0xa2, count: 32))
    let legacyViewerKey = try CloudDeviceKeyPair(
        privateKeyRaw: Data(repeating: 0x82, count: 32))
    let executorInitial = try authority.provision(
        accountID: "account-w52", machineID: "machine-w52",
        deviceKey: executorKey, masterSecret: executorMaster,
        importedPairedDevices: [CloudExecutorPairedDevice(
            deviceID: "viewer-legacy", signingKey: legacyViewerKey.publicKeyRaw,
            fingerprint: legacyViewerKey.pairingFingerprint,
            pairedAtMilliseconds: 199_000, identityGeneration: 1,
            capabilities: CloudExecutorIdentityAuthority.defaultCapabilities)])
    try t.equal(executorInitial.identityGeneration, 1, "identity enrollment starts generation one")
    try t.equal(executorInitial.pairedDevices.map(\.deviceID), ["viewer-legacy"],
                "legacy Mac roster enters the protected owner during enrollment")
    let viewerIdentityKey = try CloudDeviceKeyPair(
        privateKeyRaw: Data(repeating: 0xb3, count: 32))
    let viewerAgreementPrivate = Data(repeating: 0xc4, count: 32)
    let viewerAgreementPublic = try CloudPairing.x25519PublicKey(
        privateKeyRaw: viewerAgreementPrivate)
    let executorClaim = Data(repeating: 0xd5, count: 32)
    let executorPairingNonce = Data(repeating: 0xe6, count: 32)
    func executorOffer(_ pairingID: String) -> Data {
        CloudCanonicalJSON.canonicalData(.object([
            "v": .int(1), "type": .string("pairing_offer"),
            "pairing_id": .string(pairingID), "claim_nonce": .base64(executorClaim),
            "pairing_nonce": .base64(executorPairingNonce),
            "account_id": .string("account-w52"),
            "viewer_device_id": .string("viewer-w52"),
            "viewer_signing_key": .base64(viewerIdentityKey.publicKeyRaw),
            "viewer_ephemeral_key": .base64(viewerAgreementPublic),
            "viewer_fingerprint": .string(viewerIdentityKey.pairingFingerprint),
            "expires_at": .int(201_000),
        ]))
    }
    let executorOfferBytes = executorOffer("pairing-w52")
    let fixedMachineAgreementPrivate = Data(repeating: 0x18, count: 32)
    let executorPrepared = try authority.prepareHandover(
        offerBytes: executorOfferBytes, nowMilliseconds: 200_000,
        machineEphemeralPrivateKey: fixedMachineAgreementPrivate,
        randomBytes: { count in Data(repeating: 0xf7, count: count) })
    let executorRetry = try CloudExecutorIdentityAuthority(store: identityStore).prepareHandover(
        offerBytes: executorOfferBytes, nowMilliseconds: 200_100,
        randomBytes: { Data(repeating: 0x29, count: $0) })
    try t.equal(executorRetry.wrapperBytes, executorPrepared.wrapperBytes,
                "restart retry returns the exact durable handover")
    try t.rejects(.pairingClaimed, "a second claimant cannot replace a prepared handover") {
        _ = try authority.prepareHandover(
            offerBytes: executorOffer("pairing-second"), nowMilliseconds: 200_100)
    }
    let executorWrapper = try CloudPairing.decodeWrapper(executorPrepared.wrapperBytes)
    try t.equal(
        executorWrapper.ephemeralKey,
        try CloudPairing.x25519PublicKey(privateKeyRaw: fixedMachineAgreementPrivate)
            .base64EncodedString(),
        "grant is bound to the machine ephemeral key already published by identity/start")
    let executorShared = try CloudPairing.x25519SharedSecret(
        privateKeyRaw: viewerAgreementPrivate,
        peerPublicKeyRaw: try CloudPairing.decodeCanonicalBase64(
            executorWrapper.ephemeralKey, field: "ephemeral_key", expectedLength: 32))
    let executorDerived = try CloudPairing.derive(
        sharedSecretRaw: executorShared, pairingNonce: executorPairingNonce,
        pairingID: "pairing-w52", claimNonce: executorClaim, phase: .grant)
    let executorClear = try CloudPairing.open(
        executorWrapper, phaseKey: executorDerived.phaseKey,
        viewerEphemeralKey: viewerAgreementPublic.base64EncodedString(),
        machineEphemeralKey: executorWrapper.ephemeralKey)
    guard case .object(let executorHandover) = try CloudCanonicalJSON.parseStrict(executorClear) else {
        throw CloudPairingTestFailure.failed("W5-2 handover was not canonical JSON")
    }
    try t.equal(executorHandover.count, 8, "W0-E accepted v1 handover shape is unchanged")
    let executorPaired = try authority.commitPreparedHandover(
        pairingID: "pairing-w52", claimNonce: executorClaim.base64EncodedString(),
        deliveredFingerprint: viewerIdentityKey.pairingFingerprint, nowMilliseconds: 200_200)
    try t.equal(executorPaired.pairedDevices.map(\.deviceID), ["viewer-legacy", "viewer-w52"],
                "the exact delivered claimant is pinned once")
    let executorRevoked = try authority.revokeDevice(
        "viewer-w52", expectedGeneration: executorPaired.identityGeneration)
    try t.check(try authority.transportMaterial().pairedDevicePublicKeys["viewer-w52"] == nil,
                "revocation removes the viewer from live authorization")
    let rotatedKey = try CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 0x3a, count: 32))
    let rotatedMaster = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x4b, count: 32))
    _ = try authority.rotateKeys(
        deviceKey: rotatedKey, masterSecret: rotatedMaster, keyID: "master-v2",
        expectedGeneration: executorRevoked.identityGeneration)
    let executorCurrent = try authority.snapshot()
    let reconnect = CloudExecutorReconnectProof(
        accountID: executorCurrent.accountID, machineID: executorCurrent.machineID,
        deviceID: executorCurrent.deviceID,
        identityGeneration: executorCurrent.identityGeneration,
        keyEpoch: executorCurrent.keyEpoch, revocationEpoch: executorCurrent.revocationEpoch,
        durableLedgerOpened: true, durableSpoolOpened: true)
    try t.equal(try authority.verifyReconnect(reconnect), .resumeFromDurableLedgerAndSpool,
                "exact current identity resumes only through W5-1 durable owners")
    let staleReconnect = CloudExecutorReconnectProof(
        accountID: reconnect.accountID, machineID: reconnect.machineID,
        deviceID: reconnect.deviceID, identityGeneration: reconnect.identityGeneration,
        keyEpoch: reconnect.keyEpoch - 1, revocationEpoch: reconnect.revocationEpoch,
        durableLedgerOpened: true, durableSpoolOpened: true)
    try t.rejects(.invalidReconnectProof, "a retired key epoch cannot reconnect") {
        _ = try authority.verifyReconnect(staleReconnect)
    }
    for (_, rendered) in pairingRenderings(of: try authority.transportMaterial()) {
        try t.check(!pairingRenderingLeaks(rendered, secret: rotatedMaster.rawRepresentation),
                    "executor transport material does not render the content key")
    }

    let replayStore = CloudPairingIdentityTestStore()
    let replayAuthority = CloudExecutorIdentityAuthority(store: replayStore)
    let replaySigning = try CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 0x71, count: 32))
    let replayMaster = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x72, count: 32))
    _ = try replayAuthority.provision(
        accountID: "account-four-phase", machineID: "machine-four-phase",
        deviceKey: replaySigning, masterSecret: replayMaster)
    let replayClient = IdentityPairingReplayClient()
    let machineAgreementPrivate = Data(repeating: 0x73, count: 32)
    let machinePairingNonce = Data(repeating: 0x74, count: 32)
    let replayRandomCall = CloudLocked(0)
    let fourPhase = CloudIdentityPairingMachineHandover(
        client: replayClient, authority: replayAuthority,
        nowMilliseconds: { 100_000 },
        randomBytes: { count in
            replayRandomCall.withLock { call in
                call += 1
                return call == 1 ? machineAgreementPrivate : machinePairingNonce
            }
        })
    let fourPhaseQR = try await fourPhase.begin(rotationID: "rotation-four-phase")
    try t.equal(fourPhaseQR.machineEphemeralKey,
                try CloudPairing.x25519PublicKey(privateKeyRaw: machineAgreementPrivate)
                    .base64EncodedString(),
                "identity/start publishes the machine key later used by grant and confirm")
    do {
        _ = try await fourPhase.advance(pairingID: fourPhaseQR.pairingID)
        throw CloudPairingTestFailure.failed("lost grant reply should surface")
    } catch IdentityPairingReplyLoss.afterAcceptedWrite {}
    try t.check(try replayAuthority.snapshot().pairedDevices.isEmpty,
                "an accepted grant with a lost reply does not pin the viewer")
    do {
        _ = try await fourPhase.advance(pairingID: fourPhaseQR.pairingID)
        throw CloudPairingTestFailure.failed("lost confirm reply should surface")
    } catch IdentityPairingReplyLoss.afterAcceptedWrite {}
    try t.check(try replayAuthority.snapshot().pairedDevices.isEmpty,
                "an accepted confirm with a lost reply remains unpinned until its receipt")
    guard case .complete(let fourPhaseSnapshot) = try await fourPhase.advance(
        pairingID: fourPhaseQR.pairingID) else {
        throw CloudPairingTestFailure.failed("four-phase retry did not complete")
    }
    try t.equal(fourPhaseSnapshot.pairedDevices.map(\.deviceID), ["viewer-four-phase"],
                "duplicate grant/confirm receipts complete exactly one viewer pin")
    let reconnectProvider = CloudLifecycleKeyProvider(identityAuthority: replayAuthority)
    guard let admittedBinding = try await reconnectProvider.transportBinding() else {
        throw CloudPairingTestFailure.failed("production provider had no identity binding")
    }
    try await reconnectProvider.admitReconnect(admittedBinding)
    let postPairRotationKey = try CloudDeviceKeyPair(
        privateKeyRaw: Data(repeating: 0x75, count: 32))
    let postPairRotationMaster = try CloudMasterSecret(
        rawRepresentation: Data(repeating: 0x76, count: 32))
    _ = try replayAuthority.rotateKeys(
        deviceKey: postPairRotationKey, masterSecret: postPairRotationMaster,
        keyID: "master-four-phase-2",
        expectedGeneration: fourPhaseSnapshot.identityGeneration)
    do {
        try await reconnectProvider.admitReconnect(admittedBinding)
        throw CloudPairingTestFailure.failed("stale production binding reconnected")
    } catch let error as CloudPairingTestFailure {
        throw error
    } catch {
        try t.check(true, "production reconnect re-reads and rejects a rotated epoch")
    }

    print("CloudPairingTests: \(t.checks) checks passed")
    return t.checks
}
