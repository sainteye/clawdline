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

public func runCloudPairingTests() async throws -> Int {
    var t = CloudPairingTestHarness()

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

    print("CloudPairingTests: \(t.checks) checks passed")
    return t.checks
}
