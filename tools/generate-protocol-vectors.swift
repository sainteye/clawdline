// Deterministic protocol-vector generator. Every key and nonce in this file is TEST-ONLY.
// Fixed AES-GCM nonces make cross-runtime fixtures reproducible; production callers must use
// fresh random nonces for every message under a master secret.

import CryptoKit
import Darwin
import Foundation

private enum VectorEnvelopeClass: String, Encodable {
    case stream
    case ctl
    case dispatch
    case ho
}

private struct VectorEnvelope: Encodable {
    let v: Int
    let ch: String
    let seq: UInt64
    let ts: UInt64
    let envelopeClass: VectorEnvelopeClass
    let keyID: String
    let nonce: String
    let ct: String
    let sender: String
    let sig: String

    enum CodingKeys: String, CodingKey {
        case v, ch, seq, ts
        case envelopeClass = "class"
        case keyID = "key_id"
        case nonce, ct, sender, sig
    }

    var signingBytes: Data {
        Data([
            String(v), ch, String(seq), String(ts), envelopeClass.rawValue,
            keyID, nonce, ct,
        ].joined(separator: "|").utf8)
    }
}

private struct VectorDocument: Encodable {
    struct Entry: Encodable {
        let name: String
        let plaintext: String
        let envelope: VectorEnvelope
    }

    struct Receipt: Encodable {
        let name: String
        let body: String
        let byteLength: Int
        let sha256: String
        let headers: [String: String]

        enum CodingKeys: String, CodingKey {
            case name, body, sha256, headers
            case byteLength = "byte_length"
        }
    }

    struct CanonicalBytes: Encodable {
        let name: String
        let body: String
        let byteLength: Int
        let sha256: String
        let fieldCount: Int

        enum CodingKeys: String, CodingKey {
            case name, body, sha256
            case byteLength = "byte_length"
            case fieldCount = "field_count"
        }
    }

    struct ReplyKey: Encodable {
        let keyID: String
        let key: String
        let decodedByteLength: Int

        enum CodingKeys: String, CodingKey {
            case key
            case keyID = "key_id"
            case decodedByteLength = "decoded_byte_length"
        }
    }

    struct OpenResult: Encodable {
        let name: String
        let keyID: String
        let succeeds: Bool

        enum CodingKeys: String, CodingKey {
            case name, succeeds
            case keyID = "key_id"
        }
    }

    struct ControlResponse: Encodable {
        let channel: String
        let allowedClasses: [String]
        let replyKey: ReplyKey
        let requestEnvelope: CanonicalBytes
        let responseEnvelope: CanonicalBytes
        let responsePayload: CanonicalBytes
        let responseOpenResults: [OpenResult]

        enum CodingKeys: String, CodingKey {
            case channel
            case allowedClasses = "allowed_classes"
            case replyKey = "reply_key"
            case requestEnvelope = "request_envelope"
            case responseEnvelope = "response_envelope"
            case responsePayload = "response_payload"
            case responseOpenResults = "response_open_results"
        }
    }

    /// The seven-member `CloudPairingWrapper` the Mac writes into the one ciphertext slot
    /// `POST /v1/pairing/complete` carries.
    struct PairingWrapper: Encodable {
        let v: Int
        let phase: String
        let pairingID: String
        let senderDeviceID: String
        let ephemeralKey: String
        let nonce: String
        let ct: String

        enum CodingKeys: String, CodingKey {
            case v, phase, nonce, ct
            case pairingID = "pairing_id"
            case senderDeviceID = "sender_device_id"
            case ephemeralKey = "ephemeral_key"
        }
    }

    /// One complete viewer/Mac key handover, generated here so three implementations —
    /// this script, `Sources/CloudHandover.swift` and
    /// `Resources/web/app/js/net/cloud-pairing.js` — have to agree on the same bytes rather
    /// than on a shared reading of the same paragraph.
    struct PairingHandover: Encodable {
        let name: String
        let nowMilliseconds: Int64
        let viewerEphemeralPrivateKey: String
        let machineEphemeralPrivateKey: String
        let offer: String
        let offerFragment: String
        let phaseKey: String
        let aad: String
        let wrapper: PairingWrapper
        let handover: String
        let senderDeviceID: String

        enum CodingKeys: String, CodingKey {
            case name, offer, wrapper, handover, aad
            case nowMilliseconds = "now_milliseconds"
            case viewerEphemeralPrivateKey = "viewer_ephemeral_private_key"
            case machineEphemeralPrivateKey = "machine_ephemeral_private_key"
            case offerFragment = "offer_fragment"
            case phaseKey = "phase_key"
            case senderDeviceID = "sender_device_id"
        }
    }

    let format = 1
    let cipher = "AES-256-GCM"
    let nonceBytes = 12
    let ed25519Seed: String
    let ed25519PublicKey: String
    let masterSecret: String
    let envelopes: [Entry]
    let receipts: [Receipt]
    let controlResponse: ControlResponse
    let pairingHandover: PairingHandover

    enum CodingKeys: String, CodingKey {
        case format, cipher, envelopes, receipts
        case controlResponse = "control_response"
        case pairingHandover = "pairing_handover"
        case nonceBytes = "nonce_bytes"
        case ed25519Seed = "ed25519_seed"
        case ed25519PublicKey = "ed25519_public_key"
        case masterSecret = "master_secret"
    }
}

private struct ReceiptSpecification {
    let name: String
    let body: String
    let expectedByteLength: Int
    let expectedSHA256: String
}

private enum GeneratorError: Error, CustomStringConvertible {
    case receiptMismatch(name: String, expectedLength: Int, actualLength: Int,
                         expectedSHA256: String, actualSHA256: String)
    case nodeSignature(String)
    case controlResponseInvariant(String)

    var description: String {
        switch self {
        case let .receiptMismatch(name, expectedLength, actualLength,
                                  expectedSHA256, actualSHA256):
            return "receipt_vector_mismatch name=\(name) expected_length=\(expectedLength) "
                + "actual_length=\(actualLength) expected_sha256=\(expectedSHA256) "
                + "actual_sha256=\(actualSHA256)"
        case let .nodeSignature(message):
            return "node_signature_failed \(message)"
        case let .controlResponseInvariant(message):
            return "control_response_vector_invariant \(message)"
        }
    }
}

private let receiptSpecifications = [
    ReceiptSpecification(
        name: "delivered",
        body: #"{"confirm_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","finalized_at":"2026-08-27T12:34:56.789Z","pairing_id":"pair-0001","released_bytes":270336,"released_rows":1,"rotation_id":"rot-0001","status":"delivered","v":1,"viewer_device_id":"viewer-0001"}"#,
        expectedByteLength: 279,
        expectedSHA256: "4b3c30d5306679a58bc3df271d265616af1fa2220b41c8297bc5da5390181487"
    ),
    ReceiptSpecification(
        name: "rotation-expired",
        body: #"{"finalized_at":"2026-08-27T12:34:56.789Z","pairing_id":"pair-0001","reason":"expired","released_bytes":270336,"released_rows":1,"rotation_id":"rot-0001","status":"released","v":1,"viewer_device_id":"viewer-0001"}"#,
        expectedByteLength: 213,
        expectedSHA256: "1eac2431e8a4f6bbdd126ff2cdbc461b5c46056302490c5cff38ed2c39b1d620"
    ),
    ReceiptSpecification(
        name: "rotation-manual-revoke",
        body: #"{"finalized_at":"2026-08-27T12:34:56.789Z","pairing_id":"pair-0001","reason":"manual_revoke","released_bytes":270336,"released_rows":1,"rotation_id":"rot-0001","status":"released","v":1,"viewer_device_id":"viewer-0001"}"#,
        expectedByteLength: 219,
        expectedSHA256: "ddfabff04a185e2f7e1e27b55e782722bc8cb008588e1485dda2c5076e97bc41"
    ),
    ReceiptSpecification(
        name: "rotation-start-retry-cleanup",
        body: #"{"finalized_at":"2026-08-27T12:34:56.789Z","pairing_id":"pair-0001","reason":"start_retry_cleanup","released_bytes":270336,"released_rows":1,"rotation_id":"rot-0001","status":"released","v":1,"viewer_device_id":""}"#,
        expectedByteLength: 214,
        expectedSHA256: "e1445357605d3f3abd0c6b2844f56c1652b28e2e4c27270ceb0c4d3fcd605320"
    ),
    ReceiptSpecification(
        name: "ordinary-expired",
        body: #"{"finalized_at":"2026-08-27T12:34:56.789Z","pairing_id":"pair-0001","reason":"expired","released_bytes":270336,"released_rows":1,"rotation_id":"","status":"released","v":1,"viewer_device_id":"viewer-0001"}"#,
        expectedByteLength: 205,
        expectedSHA256: "0148dc9c895c54d3c1d8a96c1c927c6a8d4120ca792db6805f5d90a18007b946"
    ),
]

func generateProtocolVectorBytes() throws -> Data {
    let seed = Data((0..<32).map(UInt8.init))
    let masterRaw = Data((0..<32).map { UInt8(0xa0 + $0) })
    let signingKey = try Curve25519.Signing.PrivateKey(rawRepresentation: seed)
    let sender = "device-vector-01"
    let timestamp: UInt64 = 1_787_817_600_000
    let largePayload = Data((0..<65_536).map { UInt8($0 % 251) })

    let specs: [(String, Data, String, UInt64, VectorEnvelopeClass, Data)] = [
        ("stream-empty", Data(), "s/mac-01/session-01", 1, .stream, nonce(1)),
        ("control-command", Data("{\"answer\":\"yes\"}".utf8), "ctl/mac-01", 2, .ctl, nonce(2)),
        ("dispatch-command", Data("{\"task\":\"build\"}".utf8), "ctl/mac-01", 3, .dispatch, nonce(3)),
        ("handoff-package", Data([0x00, 0xff, 0x10, 0x80]), "ho/account-01/handoff-01", 4, .ho, nonce(4)),
        // Relay channel segments are printable ASCII. A Unicode logical id is represented by
        // its canonical UTF-8 percent encoding so the deployed parser accepts it byte-for-byte.
        ("unicode-channel-id", Data("你好，Clawdline".utf8), "t/mac-01/%E6%9C%83%E8%A9%B1", 5, .stream, nonce(5)),
        ("large-stream-64k", largePayload, "orch/mac-01", 9_007_199_254_740_991, .stream, nonce(6)),
    ]

    let entries = try specs.map { name, plaintext, channel, sequence, envelopeClass, fixedNonce in
        let aesNonce = try AES.GCM.Nonce(data: fixedNonce)
        let sealed = try AES.GCM.seal(
            plaintext, using: SymmetricKey(data: masterRaw), nonce: aesNonce
        )
        let unsigned = VectorEnvelope(
            v: 1, ch: channel, seq: sequence, ts: timestamp,
            envelopeClass: envelopeClass, keyID: "ms-1",
            nonce: fixedNonce.base64EncodedString(),
            ct: (sealed.ciphertext + sealed.tag).base64EncodedString(),
            sender: sender, sig: ""
        )
        // CryptoKit deliberately hedges Ed25519 signatures with randomness. Golden bytes need
        // RFC 8032's deterministic spelling, so ask Node's built-in implementation (also used by
        // the relay runtime) to sign the same fixed seed and exact UTF-8 relay base string.
        let signature = try nodeEd25519Signature(seed: seed, message: unsigned.signingBytes)
        return VectorDocument.Entry(
            name: name,
            plaintext: plaintext.base64EncodedString(),
            envelope: VectorEnvelope(
                v: unsigned.v, ch: unsigned.ch, seq: unsigned.seq, ts: unsigned.ts,
                envelopeClass: unsigned.envelopeClass, keyID: unsigned.keyID,
                nonce: unsigned.nonce, ct: unsigned.ct, sender: unsigned.sender,
                sig: signature.base64EncodedString()
            )
        )
    }

    let receipts = try receiptSpecifications.map { specification in
        let bytes = Data(specification.body.utf8)
        let actualSHA256 = sha256(bytes)
        guard bytes.count == specification.expectedByteLength,
              actualSHA256 == specification.expectedSHA256
        else {
            throw GeneratorError.receiptMismatch(
                name: specification.name,
                expectedLength: specification.expectedByteLength,
                actualLength: bytes.count,
                expectedSHA256: specification.expectedSHA256,
                actualSHA256: actualSHA256
            )
        }
        return VectorDocument.Receipt(
            name: specification.name,
            body: specification.body,
            byteLength: bytes.count,
            sha256: actualSHA256,
            headers: ["X-Clawdline-Receipt-SHA256": actualSHA256]
        )
    }

    let replyKeyRaw = Data((0..<32).map { UInt8(0x40 + $0) })
    let replyKeyID = "rk-zF3jN8rQ4Wm2pV6sT0uYxA"
    let replyKeyPattern = try NSRegularExpression(pattern: #"^rk-[A-Za-z0-9_-]{22}$"#)
    let replyKeyRange = NSRange(replyKeyID.startIndex..<replyKeyID.endIndex, in: replyKeyID)
    guard replyKeyPattern.firstMatch(in: replyKeyID, range: replyKeyRange)?.range == replyKeyRange,
          replyKeyRaw.count == 32
    else {
        throw GeneratorError.controlResponseInvariant("invalid_reply_key")
    }

    let requestPlaintext = Data((
        #"{"deadline_at":1787817720000,"images":[],"reply":{"device":"viewer-device-01","key":""#
            + replyKeyRaw.base64EncodedString()
            + #"","key_id":"rk-zF3jN8rQ4Wm2pV6sT0uYxA"},"request_id":"018f2f7a-7d65-4aa8-8e01-11a8f4257ed1","session":"session-id","text":"hello","type":"send","v":1}"#
    ).utf8)
    let requestEnvelope = try makeEnvelope(
        plaintext: requestPlaintext,
        key: masterRaw,
        seed: seed,
        channel: "ctl/mac-01",
        sequence: 411,
        timestamp: timestamp,
        envelopeClass: .ctl,
        keyID: "ms-1",
        fixedNonce: nonce(7),
        sender: "viewer-device-01"
    )

    let responsePayload = Data(
        #"{"code":"ok","completed_at":1787817600440,"request_id":"018f2f7a-7d65-4aa8-8e01-11a8f4257ed1","request_seq":411,"result":{"accepted":true},"retryable":false,"status":"ok","type":"execution_response","v":1}"#.utf8
    )
    let responseNonce = nonce(8)
    let responseEnvelope = try makeEnvelope(
        plaintext: responsePayload,
        key: replyKeyRaw,
        seed: seed,
        channel: "ctlr/mac-01/viewer-device-01",
        sequence: 912,
        timestamp: timestamp + 456,
        envelopeClass: .ctl,
        keyID: replyKeyID,
        fixedNonce: responseNonce,
        sender: "mac-01"
    )

    let responseCiphertext = try decodeCanonicalBase64(responseEnvelope.ct, field: "response_ct")
    let masterSecretOpenSucceeded: Bool
    do {
        _ = try openAESGCM(responseCiphertext, nonce: responseNonce, key: masterRaw)
        masterSecretOpenSucceeded = true
    } catch {
        masterSecretOpenSucceeded = false
    }
    guard !masterSecretOpenSucceeded else {
        throw GeneratorError.controlResponseInvariant("master_secret_opened_response")
    }
    let replyKeyOpened = try openAESGCM(
        responseCiphertext, nonce: responseNonce, key: replyKeyRaw
    )
    guard replyKeyOpened == responsePayload else {
        throw GeneratorError.controlResponseInvariant("reply_key_plaintext_mismatch")
    }

    let controlResponse = VectorDocument.ControlResponse(
        channel: responseEnvelope.ch,
        allowedClasses: [VectorEnvelopeClass.ctl.rawValue],
        replyKey: VectorDocument.ReplyKey(
            keyID: replyKeyID,
            key: replyKeyRaw.base64EncodedString(),
            decodedByteLength: replyKeyRaw.count
        ),
        requestEnvelope: try canonicalBytes(
            name: "request-ams-envelope",
            body: canonicalEnvelopeBody(requestEnvelope),
            expectedFieldCount: 10
        ),
        responseEnvelope: try canonicalBytes(
            name: "response-reply-key-envelope",
            body: canonicalEnvelopeBody(responseEnvelope),
            expectedFieldCount: 10
        ),
        responsePayload: try canonicalBytes(
            name: "execution-response-payload",
            body: String(decoding: responsePayload, as: UTF8.self),
            expectedFieldCount: 9
        ),
        responseOpenResults: [
            VectorDocument.OpenResult(
                name: "response-with-master-secret", keyID: "ms-1", succeeds: false
            ),
            VectorDocument.OpenResult(
                name: "response-with-reply-key", keyID: replyKeyID, succeeds: true
            ),
        ]
    )

    let document = VectorDocument(
        ed25519Seed: seed.base64EncodedString(),
        ed25519PublicKey: signingKey.publicKey.rawRepresentation.base64EncodedString(),
        masterSecret: masterRaw.base64EncodedString(),
        envelopes: entries,
        receipts: receipts,
        controlResponse: controlResponse,
        pairingHandover: try makePairingHandoverVector(masterSecretRaw: masterRaw)
    )
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
    var output = try encoder.encode(document)
    output.append(0x0a)
    return output
}

// MARK: - Pairing handover (viewer <-> Mac key handover, PROTOCOL §5 "start/complete/claim")

/// RFC 8785 §3.2.2.2 escaping, written out here so this script agrees with
/// `CloudCanonicalJSON.appendEscaped` by construction rather than by a shared `JSONEncoder`
/// setting. Base64 values contain `/`, which a default encoder escapes and canonical JSON
/// does not.
private func canonicalJSONString(_ text: String) -> String {
    let hexDigits = Array("0123456789abcdef")
    var out = "\""
    for scalar in text.unicodeScalars {
        switch scalar {
        case "\u{22}": out += "\\\""
        case "\u{5C}": out += "\\\\"
        case "\u{08}": out += "\\b"
        case "\u{09}": out += "\\t"
        case "\u{0A}": out += "\\n"
        case "\u{0C}": out += "\\f"
        case "\u{0D}": out += "\\r"
        default:
            if scalar.value < 0x20 {
                out += "\\u00"
                out.append(hexDigits[Int(scalar.value >> 4)])
                out.append(hexDigits[Int(scalar.value & 0xF)])
            } else {
                out.unicodeScalars.append(scalar)
            }
        }
    }
    return out + "\""
}

/// Members are given as already-serialized JSON values; keys are sorted by UTF-16 code unit,
/// which is what RFC 8785 asks for and what both other implementations do.
private func canonicalJSONObject(_ members: [String: String]) -> String {
    let keys = members.keys.sorted { $0.utf16.lexicographicallyPrecedes($1.utf16) }
    return "{" + keys.map { canonicalJSONString($0) + ":" + members[$0]! }.joined(separator: ",")
        + "}"
}

private func lengthPrefixed(_ bytes: Data) -> Data {
    Data([UInt8(bytes.count >> 8), UInt8(bytes.count & 0xFF)]) + bytes
}

private func hmacSHA256(key: Data, data: Data) -> Data {
    Data(HMAC<SHA256>.authenticationCode(for: data, using: SymmetricKey(data: key)))
}

private func base32Fingerprint(publicKeyRaw: Data) -> String {
    let alphabet = Array("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567")
    let digest = Data(SHA256.hash(data: publicKeyRaw).prefix(10))
    var characters: [Character] = []
    var accumulator: UInt32 = 0
    var bits = 0
    for byte in digest {
        accumulator = (accumulator << 8) | UInt32(byte)
        bits += 8
        while bits >= 5 {
            bits -= 5
            characters.append(alphabet[Int((accumulator >> UInt32(bits)) & 0x1F)])
        }
    }
    if bits > 0 {
        characters.append(alphabet[Int((accumulator << UInt32(5 - bits)) & 0x1F)])
    }
    return stride(from: 0, to: characters.count, by: 4)
        .map { String(characters[$0..<min($0 + 4, characters.count)]) }
        .joined(separator: "-")
}

private func base64URLWithoutPadding(_ data: Data) -> String {
    data.base64EncodedString()
        .replacingOccurrences(of: "+", with: "-")
        .replacingOccurrences(of: "/", with: "_")
        .replacingOccurrences(of: "=", with: "")
}

/// One deterministic handover. Every key, nonce and identifier below is TEST-ONLY; production
/// callers sample fresh randomness for all of them.
private func makePairingHandoverVector(
    masterSecretRaw: Data
) throws -> VectorDocument.PairingHandover {
    let viewerEphemeralRaw = Data((0..<32).map { UInt8(0x10 + $0) })
    let machineEphemeralRaw = Data((0..<32).map { UInt8(0x50 + $0) })
    let viewerSigningSeed = Data((0..<32).map { UInt8(0x80 + $0) })
    let machineSigningSeed = Data((0..<32).map { UInt8(0xb0 + $0) })
    let claimNonce = Data((0..<32).map { UInt8(0x01 + $0) })
    let pairingNonce = Data((0..<32).map { UInt8(0x21 + $0) })
    let sealNonce = Data(repeating: 0, count: 11) + Data([0x2a])

    let viewerEphemeral = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: viewerEphemeralRaw)
    let machineEphemeral = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: machineEphemeralRaw)
    let viewerSigning = try Curve25519.Signing.PrivateKey(rawRepresentation: viewerSigningSeed)
    let machineSigning = try Curve25519.Signing.PrivateKey(rawRepresentation: machineSigningSeed)

    let pairingID = "pairing-vector-01"
    let accountID = "account-vector-01"
    let machineID = "mac-vector-01"
    let viewerDeviceID = "viewer-vector-01"
    let expiresAt: Int64 = 1_787_817_900_000
    let nowMilliseconds: Int64 = 1_787_817_600_000

    let offerMembers: [String: String] = [
        "v": "1",
        "type": canonicalJSONString("pairing_offer"),
        "pairing_id": canonicalJSONString(pairingID),
        "claim_nonce": canonicalJSONString(claimNonce.base64EncodedString()),
        "pairing_nonce": canonicalJSONString(pairingNonce.base64EncodedString()),
        "account_id": canonicalJSONString(accountID),
        "viewer_device_id": canonicalJSONString(viewerDeviceID),
        "viewer_signing_key": canonicalJSONString(
            viewerSigning.publicKey.rawRepresentation.base64EncodedString()),
        "viewer_ephemeral_key": canonicalJSONString(
            viewerEphemeral.publicKey.rawRepresentation.base64EncodedString()),
        "viewer_fingerprint": canonicalJSONString(
            base32Fingerprint(publicKeyRaw: viewerSigning.publicKey.rawRepresentation)),
        "expires_at": String(expiresAt),
    ]
    let offer = canonicalJSONObject(offerMembers)

    // The KDF of `CloudPairing.derive`, phase `grant`.
    let shared = try machineEphemeral.sharedSecretFromKeyAgreement(
        with: viewerEphemeral.publicKey
    ).withUnsafeBytes { Data($0) }
    var saltPreimage = lengthPrefixed(Data("clawdline-pair-salt-v1".utf8))
    saltPreimage.append(lengthPrefixed(pairingNonce))
    saltPreimage.append(lengthPrefixed(Data(pairingID.utf8)))
    saltPreimage.append(lengthPrefixed(claimNonce))
    let salt = Data(SHA256.hash(data: saltPreimage))
    let prk = hmacSHA256(key: salt, data: shared)
    var info = lengthPrefixed(Data("clawdline-pair-v1".utf8))
    info.append(lengthPrefixed(Data("grant".utf8)))
    let phaseKey = hmacSHA256(key: prk, data: info + Data([0x01]))

    let handoverMembers: [String: String] = [
        "v": "1",
        "type": canonicalJSONString("pairing_handover"),
        "account_id": canonicalJSONString(accountID),
        "machine_id": canonicalJSONString(machineID),
        "machine_signing_key": canonicalJSONString(
            machineSigning.publicKey.rawRepresentation.base64EncodedString()),
        "machine_fingerprint": canonicalJSONString(
            base32Fingerprint(publicKeyRaw: machineSigning.publicKey.rawRepresentation)),
        "key_id": canonicalJSONString("ms-1"),
        "master_secret": canonicalJSONString(masterSecretRaw.base64EncodedString()),
    ]
    let handover = canonicalJSONObject(handoverMembers)

    let machineEphemeralPublic = machineEphemeral.publicKey.rawRepresentation.base64EncodedString()
    let aad = canonicalJSONObject([
        "v": "1",
        "phase": canonicalJSONString("grant"),
        "pairing_id": canonicalJSONString(pairingID),
        "sender_device_id": canonicalJSONString(machineID),
        "ephemeral_key": canonicalJSONString(machineEphemeralPublic),
    ])
    let sealed = try AES.GCM.seal(
        Data(handover.utf8),
        using: SymmetricKey(data: phaseKey),
        nonce: try AES.GCM.Nonce(data: sealNonce),
        authenticating: Data(aad.utf8)
    )

    return VectorDocument.PairingHandover(
        name: "viewer-key-handover",
        nowMilliseconds: nowMilliseconds,
        viewerEphemeralPrivateKey: viewerEphemeralRaw.base64EncodedString(),
        machineEphemeralPrivateKey: machineEphemeralRaw.base64EncodedString(),
        offer: offer,
        offerFragment: base64URLWithoutPadding(Data(offer.utf8)),
        phaseKey: phaseKey.base64EncodedString(),
        aad: aad,
        wrapper: VectorDocument.PairingWrapper(
            v: 1,
            phase: "grant",
            pairingID: pairingID,
            senderDeviceID: machineID,
            ephemeralKey: machineEphemeralPublic,
            nonce: sealNonce.base64EncodedString(),
            ct: (sealed.ciphertext + sealed.tag).base64EncodedString()
        ),
        handover: handover,
        senderDeviceID: machineID
    )
}

private func nonce(_ lastByte: UInt8) -> Data {
    Data(repeating: 0, count: 11) + Data([lastByte])
}

private func sha256(_ data: Data) -> String {
    SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
}

private func makeEnvelope(
    plaintext: Data,
    key: Data,
    seed: Data,
    channel: String,
    sequence: UInt64,
    timestamp: UInt64,
    envelopeClass: VectorEnvelopeClass,
    keyID: String,
    fixedNonce: Data,
    sender: String
) throws -> VectorEnvelope {
    let sealed = try AES.GCM.seal(
        plaintext,
        using: SymmetricKey(data: key),
        nonce: AES.GCM.Nonce(data: fixedNonce)
    )
    let unsigned = VectorEnvelope(
        v: 1,
        ch: channel,
        seq: sequence,
        ts: timestamp,
        envelopeClass: envelopeClass,
        keyID: keyID,
        nonce: fixedNonce.base64EncodedString(),
        ct: (sealed.ciphertext + sealed.tag).base64EncodedString(),
        sender: sender,
        sig: ""
    )
    let signature = try nodeEd25519Signature(seed: seed, message: unsigned.signingBytes)
    return VectorEnvelope(
        v: unsigned.v,
        ch: unsigned.ch,
        seq: unsigned.seq,
        ts: unsigned.ts,
        envelopeClass: unsigned.envelopeClass,
        keyID: unsigned.keyID,
        nonce: unsigned.nonce,
        ct: unsigned.ct,
        sender: unsigned.sender,
        sig: signature.base64EncodedString()
    )
}

private func canonicalEnvelopeBody(_ envelope: VectorEnvelope) throws -> String {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
    return String(decoding: try encoder.encode(envelope), as: UTF8.self)
}

private func canonicalBytes(
    name: String,
    body: String,
    expectedFieldCount: Int
) throws -> VectorDocument.CanonicalBytes {
    let bytes = Data(body.utf8)
    guard let object = try JSONSerialization.jsonObject(with: bytes) as? [String: Any],
          object.count == expectedFieldCount
    else {
        throw GeneratorError.controlResponseInvariant(
            "field_count name=\(name) expected=\(expectedFieldCount)"
        )
    }
    return VectorDocument.CanonicalBytes(
        name: name,
        body: body,
        byteLength: bytes.count,
        sha256: sha256(bytes),
        fieldCount: object.count
    )
}

private func decodeCanonicalBase64(_ text: String, field: String) throws -> Data {
    guard let data = Data(base64Encoded: text), data.base64EncodedString() == text else {
        throw GeneratorError.controlResponseInvariant("invalid_base64 field=\(field)")
    }
    return data
}

private func openAESGCM(_ ciphertextAndTag: Data, nonce: Data, key: Data) throws -> Data {
    guard ciphertextAndTag.count > 16 else {
        throw GeneratorError.controlResponseInvariant("ciphertext_too_short")
    }
    let box = try AES.GCM.SealedBox(
        nonce: AES.GCM.Nonce(data: nonce),
        ciphertext: Data(ciphertextAndTag.dropLast(16)),
        tag: Data(ciphertextAndTag.suffix(16))
    )
    return try AES.GCM.open(box, using: SymmetricKey(data: key))
}

private func nodeEd25519Signature(seed: Data, message: Data) throws -> Data {
    let javascript = #"""
    const crypto = require("node:crypto");
    const seed = Buffer.from(process.argv[1], "base64");
    const message = Buffer.from(process.argv[2], "base64");
    const prefix = Buffer.from("302e020100300506032b657004220420", "hex");
    const key = crypto.createPrivateKey({
      key: Buffer.concat([prefix, seed]), format: "der", type: "pkcs8"
    });
    process.stdout.write(crypto.sign(null, message, key).toString("base64"));
    """#
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
    process.arguments = [
        "node", "-e", javascript,
        seed.base64EncodedString(), message.base64EncodedString(),
    ]
    let output = Pipe()
    process.standardOutput = output
    process.standardError = output
    try process.run()
    let result = output.fileHandleForReading.readDataToEndOfFile()
    process.waitUntilExit()
    guard process.terminationStatus == 0,
          let text = String(data: result, encoding: .utf8),
          let signature = Data(base64Encoded: text),
          signature.count == 64
    else {
        throw GeneratorError.nodeSignature(String(decoding: result, as: UTF8.self))
    }
    return signature
}

private func writeStandardError(_ message: String) {
    FileHandle.standardError.write(Data((message + "\n").utf8))
}

private func checkCanonicalFile(at path: String, expected: Data) -> Int32 {
    let expectedSHA256 = sha256(expected)
    let fileDescriptor = path.withCString { open($0, O_RDONLY) }
    guard fileDescriptor >= 0 else {
        let reason = errno == ENOENT || errno == ENOTDIR ? "missing" : "unreadable"
        writeStandardError(
            "protocol_vectors_check_failed reason=\(reason) expected_sha256=\(expectedSHA256)"
        )
        return 1
    }

    let actual: Data
    do {
        let handle = FileHandle(fileDescriptor: fileDescriptor, closeOnDealloc: true)
        defer { try? handle.close() }
        actual = try handle.readToEnd() ?? Data()
    } catch {
        writeStandardError(
            "protocol_vectors_check_failed reason=unreadable expected_sha256=\(expectedSHA256)"
        )
        return 1
    }

    guard actual.count == expected.count, actual.elementsEqual(expected) else {
        writeStandardError(
            "protocol_vectors_check_failed expected_sha256=\(expectedSHA256) "
                + "actual_sha256=\(sha256(actual)) expected_length=\(expected.count) "
                + "actual_length=\(actual.count)"
        )
        return 1
    }
    return 0
}

private func run() -> Int32 {
    let arguments = Array(CommandLine.arguments.dropFirst())
    let mode: (check: Bool, path: String)
    if arguments.count == 1, arguments[0] != "--check" {
        mode = (false, arguments[0])
    } else if arguments.count == 2, arguments[0] == "--check" {
        mode = (true, arguments[1])
    } else {
        writeStandardError(
            "protocol_vectors_usage usage=\"generate-protocol-vectors.swift <output-path> | "
                + "generate-protocol-vectors.swift --check <canonical-path>\""
        )
        return 2
    }

    do {
        let expected = try generateProtocolVectorBytes()
        if mode.check {
            return checkCanonicalFile(at: mode.path, expected: expected)
        }
        try expected.write(to: URL(fileURLWithPath: mode.path), options: .atomic)
        print(sha256(expected))
        return 0
    } catch {
        if mode.check {
            writeStandardError("protocol_vectors_check_failed reason=generator_error error=\(error)")
        } else {
            writeStandardError("protocol_vectors_generation_failed error=\(error)")
        }
        return 1
    }
}

exit(run())
