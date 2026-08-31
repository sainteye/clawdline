import CryptoKit
import Foundation
import Security

private struct CloudProtocolVectors: Decodable {
    struct Vector: Decodable {
        let name: String
        let plaintext: String
        let envelope: CloudEnvelope
    }

    let format: Int
    let cipher: String
    let nonceBytes: Int
    let ed25519Seed: String
    let ed25519PublicKey: String
    let masterSecret: String
    let envelopes: [Vector]

    enum CodingKeys: String, CodingKey {
        case format, cipher, envelopes
        case nonceBytes = "nonce_bytes"
        case ed25519Seed = "ed25519_seed"
        case ed25519PublicKey = "ed25519_public_key"
        case masterSecret = "master_secret"
    }
}

private struct CloudTestFailure: Error, CustomStringConvertible {
    let description: String
}

/// Runs without the app and without the Keychain. The checked-in vectors path is passed in so
/// the Swift and relay suites can consume the byte-identical JSON file after root lands it.
func runCloudEnvelopeTests(vectorsURL: URL) throws -> Int {
    var checks = 0
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        checks += 1
        if !condition() { throw CloudTestFailure(description: message) }
    }

    let data = try Data(contentsOf: vectorsURL)
    let vectors = try JSONDecoder().decode(CloudProtocolVectors.self, from: data)
    try require(vectors.format == 1, "vector format")
    try require(vectors.cipher == "AES-256-GCM", "vector cipher")
    try require(vectors.nonceBytes == CloudEnvelope.nonceByteCount, "vector nonce width")
    try require(vectors.envelopes.count >= 6, "six golden envelopes")

    let seed = try requireBase64(vectors.ed25519Seed)
    let publicKey = try requireBase64(vectors.ed25519PublicKey)
    let masterRaw = try requireBase64(vectors.masterSecret)
    let signingKey = try CloudDeviceKeyPair(privateKeyRaw: seed)
    let master = try CloudMasterSecret(rawRepresentation: masterRaw)
    try require(signingKey.publicKeyRaw == publicKey, "seed derives expected public key")

    let keyForSender: (String) -> Data? = { sender in
        sender == "device-vector-01" ? publicKey : nil
    }
    var classes = Set<CloudEnvelopeClass>()
    for vector in vectors.envelopes {
        classes.insert(vector.envelope.envelopeClass)
        try require(vector.envelope.verify(using: keyForSender), "\(vector.name): signature")
        let opened = try vector.envelope.open(masterSecret: master, publicKeyForSender: keyForSender)
        let expectedPlaintext = try requireBase64(vector.plaintext)
        try require(opened == expectedPlaintext, "\(vector.name): plaintext")

        // This duplicates relay/envelope.ts on purpose. It catches a shared helper drifting while
        // still producing self-consistent signatures.
        let relayBase = [
            String(vector.envelope.v), vector.envelope.ch, String(vector.envelope.seq),
            String(vector.envelope.ts), vector.envelope.envelopeClass.rawValue,
            vector.envelope.keyID, vector.envelope.nonce, vector.envelope.ct,
        ].joined(separator: "|")
        try require(relayBase == vector.envelope.signingString, "\(vector.name): exact relay base")
        let goldenSignature = try requireBase64(vector.envelope.sig)
        let verifyingKey = try Curve25519.Signing.PublicKey(rawRepresentation: publicKey)
        try require(
            verifyingKey.isValidSignature(goldenSignature, for: Data(relayBase.utf8)),
            "\(vector.name): golden signature covers relay base"
        )
        try require(
            !verifyingKey.isValidSignature(goldenSignature, for: Data((relayBase + " ").utf8)),
            "\(vector.name): golden signature rejects a different base"
        )

        let roundTrip = try CloudEnvelope.decodeJSON(vector.envelope.encodeJSON())
        try require(roundTrip == vector.envelope, "\(vector.name): JSON round trip")
    }
    try require(classes == Set(CloudEnvelopeClass.allCases), "vectors cover all envelope classes")

    let roundTripPlaintext = Data("round trip — 密文".utf8)
    let roundTrip = try CloudEnvelope.seal(
        roundTripPlaintext, ch: "ctl/mac-01", seq: 99, ts: 1_787_817_600_000,
        envelopeClass: .ctl, keyID: "ms-1", sender: "device-vector-01",
        masterSecret: master, signingKey: signingKey
    )
    try require(roundTrip.verify(using: keyForSender), "fresh envelope verifies")
    let openedRoundTrip = try roundTrip.open(masterSecret: master, publicKeyForSender: keyForSender)
    try require(openedRoundTrip == roundTripPlaintext, "fresh envelope opens")

    let source = vectors.envelopes[1].envelope
    let sourceObject = try JSONSerialization.jsonObject(with: source.encodeJSON()) as! [String: Any]
    let mutations: [(String, (inout [String: Any]) -> Void)] = [
        ("v", { $0["v"] = 2 }),
        ("ch", { $0["ch"] = "ctl/mac-02" }),
        ("seq", { $0["seq"] = (sourceObject["seq"] as! NSNumber).uint64Value + 1 }),
        ("ts", { $0["ts"] = (sourceObject["ts"] as! NSNumber).uint64Value + 1 }),
        ("class", { $0["class"] = "dispatch" }),
        ("key_id", { $0["key_id"] = "ms-2" }),
        ("nonce", { $0["nonce"] = flippedBase64Byte(source.nonce) }),
        ("ct", { $0["ct"] = flippedBase64Byte(source.ct) }),
        ("sender", { $0["sender"] = "device-vector-02" }),
        ("sig", { $0["sig"] = flippedBase64Byte(source.sig) }),
    ]
    for (field, mutate) in mutations {
        var object = sourceObject
        mutate(&object)
        let encoded = try JSONSerialization.data(withJSONObject: object, options: [.sortedKeys])
        let accepted = (try? CloudEnvelope.decodeJSON(encoded))?.verify(using: keyForSender) == true
        try require(!accepted, "tampered \(field) is rejected")
    }

    var tracker = CloudSequenceTracker()
    try require(tracker.accept(sender: "a", sequence: 5), "first sequence")
    try require(!tracker.accept(sender: "a", sequence: 5), "duplicate sequence")
    try require(!tracker.accept(sender: "a", sequence: 4), "older sequence")
    try require(tracker.accept(sender: "a", sequence: 6), "newer sequence")
    try require(tracker.accept(sender: "b", sequence: 1), "independent sender")
    try require(tracker.highestSequence(for: "a") == 6, "highest sequence")

    let memoryStore = CloudInMemoryKeyStore()
    let injectedKeys = CloudKeys(store: memoryStore)
    let firstPair = try injectedKeys.loadOrCreateDeviceKeyPair()
    let secondPair = try injectedKeys.loadOrCreateDeviceKeyPair()
    let firstMaster = try injectedKeys.loadOrCreateMasterSecret()
    let secondMaster = try injectedKeys.loadOrCreateMasterSecret()
    try require(firstPair == secondPair, "injected store persists device key")
    try require(firstMaster == secondMaster, "injected store persists master secret")
    let recoveredMaster = try CloudMasterSecret(recoveryCode: firstMaster.recoveryCode)
    try require(firstMaster == recoveredMaster, "recovery re-derives secret")
    try require(firstPair.pairingFingerprint.split(separator: "-").count == 4, "pairing fingerprint format")

    let keychainInsert = CloudKeychainStore.insertAttributes(
        service: CloudKeychainStore.defaultService, account: CloudKeys.deviceKeyAccount,
        data: Data(repeating: 0x41, count: 32))
    try require(keychainInsert[kSecUseDataProtectionKeychain] == nil,
                "an unentitled local build stays in the macOS login-keychain namespace")
    try require(keychainInsert[kSecAttrAccessible] == nil,
                "the file-keychain insert claims no inert data-protection accessibility")

    return checks
}

private func requireBase64(_ text: String) throws -> Data {
    guard let data = Data(base64Encoded: text), data.base64EncodedString() == text else {
        throw CloudTestFailure(description: "invalid vector base64")
    }
    return data
}

private func flippedBase64Byte(_ text: String) -> String {
    var bytes = Data(base64Encoded: text)!
    bytes[bytes.startIndex] ^= 0x01
    return bytes.base64EncodedString()
}

#if CLOUD_ENVELOPE_STANDALONE
@main
private struct CloudEnvelopeStandaloneTests {
    static func main() throws {
        guard CommandLine.arguments.count == 2 else {
            throw CloudTestFailure(description: "usage: CloudEnvelopeTests <protocol-vectors.json>")
        }
        let count = try runCloudEnvelopeTests(
            vectorsURL: URL(fileURLWithPath: CommandLine.arguments[1])
        )
        print("\(count) CloudEnvelope checks passed")
    }
}
#endif
