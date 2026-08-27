// Deterministic protocol-vector generator. Every key and nonce in this file is TEST-ONLY.
// Fixed AES-GCM nonces make cross-runtime fixtures reproducible; production callers must let
// CloudEnvelope.seal generate a fresh random nonce for every message under a master secret.

import Foundation

private struct VectorDocument: Encodable {
    struct Entry: Encodable {
        let name: String
        let plaintext: String
        let envelope: CloudEnvelope
    }

    let format = 1
    let cipher = "AES-256-GCM"
    let nonceBytes = CloudEnvelope.nonceByteCount
    let ed25519Seed: String
    let ed25519PublicKey: String
    let masterSecret: String
    let envelopes: [Entry]

    enum CodingKeys: String, CodingKey {
        case format, cipher, envelopes
        case nonceBytes = "nonce_bytes"
        case ed25519Seed = "ed25519_seed"
        case ed25519PublicKey = "ed25519_public_key"
        case masterSecret = "master_secret"
    }
}

@main
private struct GenerateCloudProtocolVectors {
    static func main() throws {
        guard CommandLine.arguments.count == 2 else {
            fatalError("usage: generate-vectors <output.json>")
        }
        let seed = Data((0..<32).map(UInt8.init))
        let masterRaw = Data((0..<32).map { UInt8(0xa0 + $0) })
        let signingKey = try CloudDeviceKeyPair(privateKeyRaw: seed)
        let master = try CloudMasterSecret(rawRepresentation: masterRaw)
        let sender = "device-vector-01"
        let timestamp: UInt64 = 1_787_817_600_000
        let largePayload = Data((0..<65_536).map { UInt8($0 % 251) })

        let specs: [(String, Data, String, UInt64, CloudEnvelopeClass, Data)] = [
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
            let randomized = try CloudEnvelope.seal(
                plaintext, ch: channel, seq: sequence, ts: timestamp,
                envelopeClass: envelopeClass, keyID: "ms-1", sender: sender,
                masterSecret: master, signingKey: signingKey,
                nonceForTesting: fixedNonce
            )
            // CryptoKit deliberately hedges Ed25519 signatures with randomness. Golden bytes need
            // RFC 8032's deterministic spelling, so the fixture generator asks Node's built-in
            // Ed25519 implementation (the relay runtime, no package dependency) to sign the same
            // fixed seed and exact UTF-8 relay base string.
            let deterministicSignature = try nodeEd25519Signature(
                seed: seed, message: randomized.signingBytes
            )
            return VectorDocument.Entry(
                name: name,
                plaintext: plaintext.base64EncodedString(),
                envelope: try CloudEnvelope(
                    ch: randomized.ch, seq: randomized.seq, ts: randomized.ts,
                    envelopeClass: randomized.envelopeClass, keyID: randomized.keyID,
                    nonce: randomized.nonce, ct: randomized.ct, sender: randomized.sender,
                    sig: deterministicSignature.base64EncodedString()
                )
            )
        }
        let document = VectorDocument(
            ed25519Seed: seed.base64EncodedString(),
            ed25519PublicKey: signingKey.publicKeyRaw.base64EncodedString(),
            masterSecret: masterRaw.base64EncodedString(),
            envelopes: entries
        )
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        var output = try encoder.encode(document)
        output.append(0x0a)
        try output.write(to: URL(fileURLWithPath: CommandLine.arguments[1]), options: .atomic)
    }

    private static func nonce(_ lastByte: UInt8) -> Data {
        Data(repeating: 0, count: CloudEnvelope.nonceByteCount - 1) + Data([lastByte])
    }

    private static func nodeEd25519Signature(seed: Data, message: Data) throws -> Data {
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
              signature.count == CloudEnvelope.signatureByteCount
        else {
            throw NSError(
                domain: "generate-vectors", code: Int(process.terminationStatus),
                userInfo: [NSLocalizedDescriptionKey: String(decoding: result, as: UTF8.self)]
            )
        }
        return signature
    }
}
