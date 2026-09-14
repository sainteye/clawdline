import Foundation

private struct CloudV2ProtocolTestFailure: Error, CustomStringConvertible {
    let description: String
}

private func cloudJSONFieldPaths(
    _ value: CloudJSONValue, prefix: String = ""
) -> Set<String> {
    guard case .object(let fields) = value else { return [] }
    return fields.reduce(into: Set<String>()) { paths, entry in
        let path = prefix.isEmpty ? entry.key : "\(prefix).\(entry.key)"
        paths.insert(path)
        paths.formUnion(cloudJSONFieldPaths(entry.value, prefix: path))
    }
}

private actor CloudV2RefreshAuthorityFixture {
    private var value = CloudCommandEffectAuthorization(
        epochState: .ready, rosterAllowsSender: true, writeGateAllows: true)
    private var calls = 0
    private var secondCall: CheckedContinuation<Void, Never>?

    func current() async -> CloudCommandEffectAuthorization {
        calls += 1
        if calls == 2 { await withCheckedContinuation { secondCall = $0 } }
        return value
    }

    func waitForSecondCall() async throws {
        for _ in 0..<200 {
            if calls >= 2 { return }
            try await Task.sleep(nanoseconds: 10_000_000)
        }
        throw CloudV2ProtocolTestFailure(
            description: "timed out waiting for refresh effect-time authorization")
    }

    func denyAndRelease() {
        value = CloudCommandEffectAuthorization(
            epochState: .ready, rosterAllowsSender: true, writeGateAllows: false)
        secondCall?.resume()
        secondCall = nil
    }
}

func runCloudV2ProtocolTests() async throws -> Int {
    var checks = 0
    func check(_ condition: @autoclosure () -> Bool, _ name: String) throws {
        checks += 1
        guard condition() else {
            throw CloudV2ProtocolTestFailure(description: "check \(checks) failed: \(name)")
        }
    }
    func rejects(
        _ expected: CloudV2ProtocolError, _ name: String, _ operation: () throws -> Void
    ) throws {
        checks += 1
        do { try operation() }
        catch let error as CloudV2ProtocolError where error == expected { return }
        catch {
            throw CloudV2ProtocolTestFailure(
                description: "check \(checks) failed: \(name) threw \(error), expected \(expected)")
        }
        throw CloudV2ProtocolTestFailure(
            description: "check \(checks) failed: \(name) did not reject")
    }

    let ciphertext = Data("ciphertext-v2".utf8)
    let digest = CloudV2StoredObjectHeader.digest(of: ciphertext)
    let priorDigest = Data(repeating: 0x11, count: 32)
    let key = try CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 0x42, count: 32))
    let viewerKey = try CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 0x43, count: 32))
    let otherKey = try CloudDeviceKeyPair(privateKeyRaw: Data(repeating: 0x44, count: 32))
    let enabled = CloudV2ReplicationPolicy { _, _ in true }

    func header(
        schemaVersion: Int64 = 2, account: String = "acct-1",
        machine: String = "machine-1", signerDeviceID: String = "device-1",
        signingKeyID: String = "signing-key-1",
        plane: CloudV2StoredObjectPlane = .status, streamKey: String = "session-1",
        writerEpoch: Int64 = 2, writerGeneration: Int64 = 3,
        projectorVersion: Int64 = 4, firstRevision: Int64 = 10,
        lastRevision: Int64 = 10, previousDigest: Data? = priorDigest,
        keyEpoch: Int64 = 5, coarseGeneratedAt: Int64 = 1_800,
        ciphertextDigest: Data = digest, commandBinding: CloudV2CommandBinding? = nil
    ) throws -> CloudV2StoredObjectHeader {
        try CloudV2StoredObjectHeader(
            schemaVersion: schemaVersion, account: account, machine: machine,
            signerDeviceID: signerDeviceID, signingKeyID: signingKeyID, plane: plane,
            streamKey: streamKey, writerEpoch: writerEpoch,
            writerGeneration: writerGeneration, projectorVersion: projectorVersion,
            firstRevision: firstRevision, lastRevision: lastRevision,
            previousDigest: previousDigest, keyEpoch: keyEpoch,
            coarseGeneratedAt: coarseGeneratedAt, ciphertextDigest: ciphertextDigest,
            commandBinding: commandBinding)
    }
    func grant(_ value: CloudV2StoredObjectHeader) -> CloudV2WriterGrant {
        CloudV2WriterGrant(
            account: value.account, machine: value.machine, plane: value.plane,
            streamKey: value.streamKey, writerEpoch: value.writerEpoch,
            writerGeneration: value.writerGeneration,
            projectorVersion: value.projectorVersion, keyEpoch: value.keyEpoch)
    }
    func validateWrite(
        current: CloudV2StreamHighWater?, incoming: CloudV2StoredObjectHeader,
        writerGrant: CloudV2WriterGrant? = nil, pinnedKeyEpoch: Int64? = nil
    ) throws -> CloudV2HighWaterTransition {
        try CloudV2HighWaterValidator.validateWrite(
            current: current, incoming: incoming, grant: writerGrant ?? grant(incoming),
            pinnedKeyEpoch: pinnedKeyEpoch ?? incoming.keyEpoch)
    }

    let baseHeader = try header()
    let baseStream = CloudV2StreamIdentity(baseHeader)
    let signed = try CloudV2SignedStoredObject.sign(
        header: baseHeader, ciphertext: ciphertext, using: key, policy: enabled)
    let authorizedWriter: CloudV2AuthorizedWriterKey = { stream, deviceID, keyID in
        stream == baseStream && deviceID == "device-1" && keyID == "signing-key-1"
            ? key.publicKeyRaw : nil
    }
    try signed.verify(
        ciphertext: ciphertext, expected: baseStream,
        authorizedWriterKey: authorizedWriter)
    try check(true, "a correctly digested object signed by the stream writer verifies")
    do {
        _ = try CloudV2SignedStoredObject.sign(
            header: baseHeader, ciphertext: ciphertext, using: key, policy: .disabled)
        throw CloudV2ProtocolTestFailure(description: "disabled policy signed a record")
    } catch CloudV2AvailabilityGap.notReplicated { checks += 1 }

    let aad = try baseHeader.canonicalAAD()
    let signingInput = Data(CloudV2StoredObjectHeader.signingDomain.utf8) + Data([0]) + aad
    let canonicalSigningBytes = try baseHeader.canonicalSigningBytes()
    try check(canonicalSigningBytes == signingInput,
              "AAD and signing bytes share one canonical header")
    let aeadKey = Data(repeating: 0x31, count: 32)
    let sealed = try CloudV2AEADPayload.seal(
        plaintext: Data("secret plaintext".utf8), key: aeadKey
    ) { encrypted in
        try header(ciphertextDigest: CloudV2StoredObjectHeader.digest(of: encrypted))
    }
    let openedPlaintext = try sealed.payload.open(header: sealed.header, key: aeadKey)
    try check(openedPlaintext == Data("secret plaintext".utf8),
              "AEAD authenticates the canonical header")
    let alteredAAD = try header(
        coarseGeneratedAt: 1_860, ciphertextDigest: sealed.header.ciphertextDigest)
    try rejects(.aeadAuthenticationFailed, "header mutation breaks AEAD open") {
        _ = try sealed.payload.open(header: alteredAAD, key: aeadKey)
    }

    let mutations: [(String, () throws -> CloudV2StoredObjectHeader)] = [
        ("schema_version", { try header(schemaVersion: 3) }),
        ("account", { try header(account: "acct-2") }),
        ("machine", { try header(machine: "machine-2") }),
        ("signer_device_id", { try header(signerDeviceID: "device-2") }),
        ("signing_key_id", { try header(signingKeyID: "signing-key-2") }),
        ("plane", { try header(plane: .blob) }),
        ("stream_key", { try header(streamKey: "session-2") }),
        ("writer_epoch", { try header(writerEpoch: 3) }),
        ("writer_generation", { try header(writerGeneration: 4) }),
        ("projector_version", { try header(projectorVersion: 5) }),
        ("first_revision", { try header(firstRevision: 9) }),
        ("last_revision", { try header(lastRevision: 11) }),
        ("previous_digest", { try header(previousDigest: Data(repeating: 0x12, count: 32)) }),
        ("key_epoch", { try header(keyEpoch: 6) }),
        ("coarse_generated_at", { try header(coarseGeneratedAt: 1_860) }),
        ("ciphertext_digest", { try header(ciphertextDigest: Data(repeating: 0x13, count: 32)) }),
    ]
    let topLevelSignedFields = cloudJSONFieldPaths(baseHeader.cloudJSONValue).filter {
        !$0.contains(".")
    }
    try check(Set(mutations.map(\.0)).union(["command"]) == topLevelSignedFields,
              "signature mutations track every canonical top-level header field")
    for (field, mutation) in mutations {
        let mutatedHeader = try mutation()
        let rebound = CloudV2SignedStoredObject(header: mutatedHeader, signature: signed.signature)
        try rejects(.invalidSignature, "mutating signed \(field) fails verification") {
            try rebound.verifySignature(
                expected: CloudV2StreamIdentity(mutatedHeader),
                authorizedWriterKey: { _, _, _ in key.publicKeyRaw })
        }
    }
    for (name, signerID, keyID, signingKey) in [
        ("roster viewer", "viewer-device", "viewer-key", viewerKey),
        ("other Mac", "machine-2-device", "machine-2-key", otherKey),
    ] {
        try rejects(.unauthorizedSigner, "\(name) cannot author this machine stream") {
            let forgedHeader = try header(signerDeviceID: signerID, signingKeyID: keyID)
            let forged = try CloudV2SignedStoredObject.sign(
                header: forgedHeader, ciphertext: ciphertext, using: signingKey, policy: enabled)
            try forged.verifySignature(
                expected: CloudV2StreamIdentity(forgedHeader),
                authorizedWriterKey: authorizedWriter)
        }
    }
    try rejects(.ciphertextDigestMismatch, "ciphertext is checked against the signed digest") {
        try signed.verify(
            ciphertext: Data("different".utf8), expected: baseStream,
            authorizedWriterKey: authorizedWriter)
    }

    let otherHeader = try header(machine: "machine-2", streamKey: "session-2")
    let authenticOther = try CloudV2SignedStoredObject.sign(
        header: otherHeader, ciphertext: ciphertext, using: key, policy: enabled)
    try rejects(.unexpectedStream, "an authentic object cannot substitute for another stream") {
        try authenticOther.verifySignature(
            expected: baseStream, authorizedWriterKey: { _, _, _ in key.publicKeyRaw })
    }
    for existing in [nil, CloudV2StreamHighWater(accepting: baseHeader)] {
        try rejects(.unexpectedStream, "read validation binds first and later fetches") {
            _ = try CloudV2HighWaterValidator.validateRead(
                current: existing, incoming: otherHeader, expected: baseStream,
                minimumKeyEpoch: 1)
        }
    }

    let current = CloudV2StreamHighWater(accepting: baseHeader)
    try rejects(.rollbackDetected, "a lower writer epoch is a rollback") {
        _ = try validateWrite(current: current,
                              incoming: header(writerEpoch: 1, writerGeneration: 2))
    }
    try rejects(.rollbackDetected, "a lower key epoch is a rollback") {
        _ = try validateWrite(current: current, incoming: header(keyEpoch: 4))
    }
    try rejects(.keyEpochConflict, "Cloud cannot choose below the pinned key ring") {
        _ = try validateWrite(current: current, incoming: baseHeader, pinnedKeyEpoch: 6)
    }
    try rejects(.revisionRegressed, "a lower same-epoch revision is rejected") {
        _ = try validateWrite(current: current,
                              incoming: header(firstRevision: 9, lastRevision: 9))
    }
    try rejects(.streamFork, "same position with another digest is a fork") {
        _ = try validateWrite(current: current,
            incoming: header(ciphertextDigest: Data(repeating: 0x21, count: 32)))
    }
    try rejects(.streamFork, "advancing revisions chain from the verified digest") {
        _ = try validateWrite(current: current, incoming: header(
            firstRevision: 11, lastRevision: 11,
            previousDigest: Data(repeating: 0x23, count: 32),
            ciphertextDigest: Data(repeating: 0x24, count: 32)))
    }

    let higherEpoch = try header(
        writerEpoch: 3, writerGeneration: 4, firstRevision: 0, lastRevision: 0,
        previousDigest: current.digest, ciphertextDigest: Data(repeating: 0x25, count: 32))
    try rejects(.writerEpochConflict, "higher epoch requires an exact CAS grant") {
        _ = try CloudV2HighWaterValidator.validateWrite(
            current: current, incoming: higherEpoch, grant: nil,
            pinnedKeyEpoch: higherEpoch.keyEpoch)
    }
    try rejects(.writerEpochConflict, "epoch advance requires a newer generation") {
        _ = try validateWrite(current: current, incoming: header(
            writerEpoch: 3, writerGeneration: 3, firstRevision: 0, lastRevision: 0,
            previousDigest: current.digest,
            ciphertextDigest: Data(repeating: 0x30, count: 32)))
    }
    try rejects(.writerEpochConflict, "epoch advance chains its previous digest") {
        _ = try validateWrite(current: current, incoming: header(
            writerEpoch: 3, writerGeneration: 4, firstRevision: 0, lastRevision: 0,
            previousDigest: Data(repeating: 0x32, count: 32),
            ciphertextDigest: Data(repeating: 0x33, count: 32)))
    }
    let higherEpochTransition = try validateWrite(current: current, incoming: higherEpoch)
    try check(higherEpochTransition == .advanced(CloudV2StreamHighWater(accepting: higherEpoch)),
        "a CAS-granted new epoch may reset revision")
    try rejects(.writerEpochConflict, "generation is fixed inside one writer epoch") {
        _ = try validateWrite(current: current, incoming: header(writerGeneration: 4))
    }
    try rejects(.rollbackDetected, "lower projector version rolls back") {
        _ = try validateWrite(current: current, incoming: header(projectorVersion: 3))
    }
    try rejects(.streamFork, "higher projector version opens a new epoch") {
        _ = try validateWrite(current: current, incoming: header(projectorVersion: 5))
    }

    let wrongCurrents = [
        try header(account: "acct-2"), try header(machine: "machine-2"),
        try header(plane: .blob), try header(streamKey: "session-2"),
    ].map(CloudV2StreamHighWater.init(accepting:))
    for wrongCurrent in wrongCurrents {
        try rejects(.streamFork, "current high-water identity cannot move") {
            _ = try validateWrite(current: wrongCurrent, incoming: baseHeader)
        }
    }
    let wrongGrants = [
        CloudV2WriterGrant(account: "acct-2", machine: "machine-1", plane: .status,
            streamKey: "session-1", writerEpoch: 2, writerGeneration: 3,
            projectorVersion: 4, keyEpoch: 5),
        CloudV2WriterGrant(account: "acct-1", machine: "machine-2", plane: .status,
            streamKey: "session-1", writerEpoch: 2, writerGeneration: 3,
            projectorVersion: 4, keyEpoch: 5),
        CloudV2WriterGrant(account: "acct-1", machine: "machine-1", plane: .blob,
            streamKey: "session-1", writerEpoch: 2, writerGeneration: 3,
            projectorVersion: 4, keyEpoch: 5),
        CloudV2WriterGrant(account: "acct-1", machine: "machine-1", plane: .status,
            streamKey: "session-2", writerEpoch: 2, writerGeneration: 3,
            projectorVersion: 4, keyEpoch: 5),
        CloudV2WriterGrant(account: "acct-1", machine: "machine-1", plane: .status,
            streamKey: "session-1", writerEpoch: 3, writerGeneration: 3,
            projectorVersion: 4, keyEpoch: 5),
        CloudV2WriterGrant(account: "acct-1", machine: "machine-1", plane: .status,
            streamKey: "session-1", writerEpoch: 2, writerGeneration: 4,
            projectorVersion: 4, keyEpoch: 5),
        CloudV2WriterGrant(account: "acct-1", machine: "machine-1", plane: .status,
            streamKey: "session-1", writerEpoch: 2, writerGeneration: 3,
            projectorVersion: 5, keyEpoch: 5),
    ]
    for wrongGrant in wrongGrants {
        try rejects(.writerEpochConflict, "every grant field is matched") {
            _ = try validateWrite(
                current: current, incoming: baseHeader, writerGrant: wrongGrant)
        }
    }
    let wrongKeyGrant = CloudV2WriterGrant(
        account: "acct-1", machine: "machine-1", plane: .status,
        streamKey: "session-1", writerEpoch: 2, writerGeneration: 3,
        projectorVersion: 4, keyEpoch: 6)
    try rejects(.keyEpochConflict, "grant key epoch is checked before encryption") {
        try wrongKeyGrant.validatePinnedKeyEpoch(5)
    }
    try rejects(.keyEpochConflict, "grant key epoch conflict keeps its typed write error") {
        _ = try validateWrite(
            current: current, incoming: baseHeader, writerGrant: wrongKeyGrant)
    }
    let next = try header(
        firstRevision: 11, lastRevision: 11, previousDigest: current.digest,
        ciphertextDigest: Data(repeating: 0x26, count: 32))
    let nextTransition = try validateWrite(current: current, incoming: next)
    try check(nextTransition == .advanced(CloudV2StreamHighWater(accepting: next)),
        "a contiguous signed range advances")
    let coalesced = try header(
        firstRevision: 20, lastRevision: 20, previousDigest: current.digest,
        ciphertextDigest: Data(repeating: 0x27, count: 32))
    let coalescedTransition = try validateWrite(current: current, incoming: coalesced)
    try check(coalescedTransition == .advanced(CloudV2StreamHighWater(accepting: coalesced)),
        "latest-value status can coalesce revisions")
    let transcriptHeader = try header(
        plane: .transcriptSegment, firstRevision: 1, lastRevision: 10,
        previousDigest: nil)
    let transcriptCurrent = CloudV2StreamHighWater(accepting: transcriptHeader)
    try rejects(.streamFork, "immutable transcript cannot skip a revision") {
        _ = try validateWrite(current: transcriptCurrent, incoming: header(
            plane: .transcriptSegment, firstRevision: 12, lastRevision: 12,
            previousDigest: transcriptCurrent.digest,
            ciphertextDigest: Data(repeating: 0x28, count: 32)))
    }
    let replayTransition = try validateWrite(current: current, incoming: baseHeader)
    try check(replayTransition == .idempotent(current),
              "exact replay is idempotent")
    try rejects(.rollbackDetected, "readers retain a revoked-key floor") {
        _ = try CloudV2HighWaterValidator.validateRead(
            current: nil, incoming: baseHeader, expected: baseStream, minimumKeyEpoch: 6)
    }

    let missedStatus = try header(
        firstRevision: 12, lastRevision: 12,
        previousDigest: Data(repeating: 0x91, count: 32),
        ciphertextDigest: Data(repeating: 0x92, count: 32))
    let missedStatusTransition = try CloudV2HighWaterValidator.validateRead(
        current: current, incoming: missedStatus, expected: baseStream, minimumKeyEpoch: 5)
    try check(
        missedStatusTransition == .advanced(CloudV2StreamHighWater(accepting: missedStatus)),
        "latest-value readers may advance past an unseen predecessor")
    let readerNewEpoch = try header(
        writerEpoch: 3, writerGeneration: 4, firstRevision: 0, lastRevision: 0,
        previousDigest: Data(repeating: 0x93, count: 32),
        ciphertextDigest: Data(repeating: 0x94, count: 32))
    let readerNewEpochTransition = try CloudV2HighWaterValidator.validateRead(
        current: current, incoming: readerNewEpoch, expected: baseStream, minimumKeyEpoch: 5)
    try check(
        readerNewEpochTransition == .advanced(CloudV2StreamHighWater(accepting: readerNewEpoch)),
        "readers may cross a missed writer-epoch boundary without the predecessor digest")
    try rejects(.revisionRegressed, "reader rejects a lower revision") {
        _ = try CloudV2HighWaterValidator.validateRead(
            current: current, incoming: header(firstRevision: 9, lastRevision: 9),
            expected: baseStream, minimumKeyEpoch: 5)
    }
    try rejects(.streamFork, "reader rejects another digest at the same position") {
        _ = try CloudV2HighWaterValidator.validateRead(
            current: current,
            incoming: header(ciphertextDigest: Data(repeating: 0x95, count: 32)),
            expected: baseStream, minimumKeyEpoch: 5)
    }
    try rejects(.rollbackDetected, "reader rejects a lower writer epoch") {
        _ = try CloudV2HighWaterValidator.validateRead(
            current: current, incoming: header(writerEpoch: 1, writerGeneration: 2),
            expected: baseStream, minimumKeyEpoch: 5)
    }
    try rejects(.repairableGap, "a missed transcript segment requests repair") {
        _ = try CloudV2HighWaterValidator.validateRead(
            current: transcriptCurrent,
            incoming: header(
                plane: .transcriptSegment, firstRevision: 12, lastRevision: 12,
                previousDigest: Data(repeating: 0x96, count: 32),
                ciphertextDigest: Data(repeating: 0x97, count: 32)),
            expected: CloudV2StreamIdentity(transcriptHeader), minimumKeyEpoch: 5)
    }

    let observed = CloudV2ObservedPosition(accepting: baseHeader)
    let command = CloudV2CommandBinding(
        senderDeviceID: "viewer-device", idempotencyKey: "request-1",
        targetStreamKey: "opaque-session-stream-1", commandType: "answer", notAfter: 1_980,
        precondition: observed)
    let requestHeader = try header(
        signerDeviceID: "viewer-device", signingKeyID: "viewer-key",
        plane: .commandRequest, streamKey: command.streamKey, commandBinding: command)
    let request = try CloudV2SignedStoredObject.sign(
        header: requestHeader, ciphertext: ciphertext, using: viewerKey, policy: enabled)
    try request.verifySignature(
        expected: CloudV2StreamIdentity(requestHeader),
        authorizedWriterKey: { _, _, _ in viewerKey.publicKeyRaw })

    func commandBinding(
        sender: String = "viewer-device", idempotencyKey: String = "request-1",
        targetStreamKey: String = "opaque-session-stream-1", commandType: String = "answer",
        notAfter: Int64 = 1_980, precondition: CloudV2ObservedPosition?
    ) -> CloudV2CommandBinding {
        CloudV2CommandBinding(
            senderDeviceID: sender, idempotencyKey: idempotencyKey,
            targetStreamKey: targetStreamKey, commandType: commandType,
            notAfter: notAfter, precondition: precondition)
    }
    func commandRequestHeader(
        _ binding: CloudV2CommandBinding, signer: String = "viewer-device"
    ) throws -> CloudV2StoredObjectHeader {
        try header(
            signerDeviceID: signer, signingKeyID: "viewer-key", plane: .commandRequest,
            streamKey: binding.streamKey, commandBinding: binding)
    }
    let commandMutations: [(String, CloudV2CommandBinding, String)] = [
        ("command.sender_device_id",
         commandBinding(sender: "viewer-device-2", precondition: observed), "viewer-device-2"),
        ("command.idempotency_key",
         commandBinding(idempotencyKey: "request-2", precondition: observed), "viewer-device"),
        ("command.target_stream_key",
         commandBinding(targetStreamKey: "opaque-session-stream-2", precondition: observed),
         "viewer-device"),
        ("command.command_type",
         commandBinding(commandType: "send", precondition: observed), "viewer-device"),
        ("command.not_after",
         commandBinding(notAfter: 2_040, precondition: observed), "viewer-device"),
        ("command.precondition", commandBinding(precondition: nil), "viewer-device"),
        ("command.precondition.account",
         commandBinding(precondition: CloudV2ObservedPosition(
            accepting: try header(account: "acct-2"))), "viewer-device"),
        ("command.precondition.machine",
         commandBinding(precondition: CloudV2ObservedPosition(
            accepting: try header(machine: "machine-2"))), "viewer-device"),
        ("command.precondition.plane",
         commandBinding(precondition: CloudV2ObservedPosition(
            accepting: try header(plane: .blob))), "viewer-device"),
        ("command.precondition.stream_key",
         commandBinding(precondition: CloudV2ObservedPosition(
            accepting: try header(streamKey: "session-2"))), "viewer-device"),
        ("command.precondition.writer_epoch",
         commandBinding(precondition: CloudV2ObservedPosition(
            accepting: try header(writerEpoch: 3))), "viewer-device"),
        ("command.precondition.writer_generation",
         commandBinding(precondition: CloudV2ObservedPosition(
            accepting: try header(writerGeneration: 4))), "viewer-device"),
        ("command.precondition.projector_version",
         commandBinding(precondition: CloudV2ObservedPosition(
            accepting: try header(projectorVersion: 5))), "viewer-device"),
        ("command.precondition.last_revision",
         commandBinding(precondition: CloudV2ObservedPosition(
            accepting: try header(firstRevision: 10, lastRevision: 11))), "viewer-device"),
        ("command.precondition.ciphertext_digest",
         commandBinding(precondition: CloudV2ObservedPosition(accepting: try header(
            ciphertextDigest: Data(repeating: 0xa1, count: 32)))), "viewer-device"),
    ]
    let commandSignedFields = cloudJSONFieldPaths(requestHeader.cloudJSONValue).filter {
        $0.hasPrefix("command.")
    }
    try check(Set(commandMutations.map(\.0)) == commandSignedFields,
              "command signature mutations track every canonical command field")
    for (field, mutatedBinding, signer) in commandMutations {
        let mutatedHeader = try commandRequestHeader(mutatedBinding, signer: signer)
        let rebound = CloudV2SignedStoredObject(
            header: mutatedHeader, signature: request.signature)
        try rejects(.invalidSignature, "mutating signed \(field) fails verification") {
            try rebound.verifySignature(
                expected: CloudV2StreamIdentity(mutatedHeader),
                authorizedWriterKey: { _, _, _ in viewerKey.publicKeyRaw })
        }
    }
    try rejects(.invalidField("command_sender"), "request signer must equal command sender") {
        _ = try header(
            signerDeviceID: "device-1", signingKeyID: "signing-key-1",
            plane: .commandRequest, streamKey: command.streamKey, commandBinding: command)
    }
    try rejects(.invalidField("command_stream_key"), "command stream key is derived") {
        _ = try header(
            signerDeviceID: "viewer-device", signingKeyID: "viewer-key",
            plane: .commandRequest, streamKey: "wrong-command-stream", commandBinding: command)
    }
    try rejects(.invalidField("command"), "command request requires a binding") {
        _ = try header(plane: .commandRequest)
    }
    try rejects(.invalidField("command"), "non-command plane rejects a binding") {
        _ = try header(commandBinding: command)
    }
    try rejects(.invalidField("not_after"), "command expiry is quantized") {
        let unquantized = commandBinding(notAfter: 2_001, precondition: observed)
        _ = try commandRequestHeader(unquantized)
    }

    let receiptHeader = try header(
        plane: .commandReceipt, streamKey: command.streamKey, commandBinding: command)
    let receipt = try CloudV2SignedStoredObject.sign(
        header: receiptHeader, ciphertext: ciphertext, using: key, policy: enabled)
    let receiptStream = CloudV2StreamIdentity(receiptHeader)
    try receipt.verifySignature(expected: receiptStream, authorizedWriterKey: {
        stream, device, keyID in
        stream == receiptStream && device == "device-1" && keyID == "signing-key-1"
            ? key.publicKeyRaw : nil
    })
    try check(true, "executor-signed command receipt verifies")
    let receiptCurrent = CloudV2StreamHighWater(accepting: receiptHeader)
    let skippedReceipt = try header(
        plane: .commandReceipt, streamKey: command.streamKey,
        firstRevision: 12, lastRevision: 12, previousDigest: receiptCurrent.digest,
        ciphertextDigest: Data(repeating: 0xa2, count: 32), commandBinding: command)
    try rejects(.streamFork, "receipt writers cannot skip a chained revision") {
        _ = try validateWrite(current: receiptCurrent, incoming: skippedReceipt)
    }
    try rejects(.unauthorizedSigner, "forged effect receipt is rejected") {
        let forgedHeader = try header(
            signerDeviceID: "viewer-device", signingKeyID: "viewer-key",
            plane: .commandReceipt, streamKey: command.streamKey, commandBinding: command)
        let forged = try CloudV2SignedStoredObject.sign(
            header: forgedHeader, ciphertext: ciphertext, using: viewerKey, policy: enabled)
        try forged.verifySignature(
            expected: CloudV2StreamIdentity(forgedHeader),
            authorizedWriterKey: { _, device, _ in
                device == "device-1" ? key.publicKeyRaw : nil
            })
    }
    try check(observed.writerEpoch == 2 && observed.lastRevision == 10
        && observed.ciphertextDigest == digest,
        "command precondition retains full signed position and digest")
    let lifetime = try CloudV2CommandLifetimePolicy(
        maximumJobLifeSeconds: 300, maximumClockSkewSeconds: 30,
        dedupeRetentionSeconds: 330)
    try lifetime.validate(notAfter: 1_320, guardedNow: 1_020)
    try check(true, "command inside the dedupe horizon is accepted")
    try rejects(.commandDeadlineInvalid, "not_after cannot outlive maximum job life") {
        try lifetime.validate(notAfter: 1_380, guardedNow: 1_020)
    }
    try rejects(.commandExpired, "expired command has a distinct terminal error") {
        try lifetime.validate(notAfter: 960, guardedNow: 1_020)
    }
    try rejects(.dedupeHorizonUnsafe, "retention covers job life plus skew") {
        _ = try CloudV2CommandLifetimePolicy(
            maximumJobLifeSeconds: 300, maximumClockSkewSeconds: 30,
            dedupeRetentionSeconds: 329)
    }
    try rejects(.dedupeHorizonUnsafe, "retention requirement cannot overflow") {
        _ = try CloudV2CommandLifetimePolicy(
            maximumJobLifeSeconds: Int64.max, maximumClockSkewSeconds: 1,
            dedupeRetentionSeconds: Int64.max)
    }
    let overflowingHorizon = try CloudV2CommandLifetimePolicy(
        maximumJobLifeSeconds: 300, maximumClockSkewSeconds: 0,
        dedupeRetentionSeconds: 300)
    try rejects(.commandDeadlineInvalid, "guarded command horizon cannot overflow") {
        try overflowingHorizon.validate(notAfter: Int64.max, guardedNow: Int64.max - 100)
    }

    let statusOnly = CloudV2ReplicationPolicy { plane, _ in plane == .status }
    try statusOnly.requireReplication(plane: .status, streamKey: "session-1")
    try check(Set(CloudV2AvailabilityGap.allCases.map(\.rawValue)) == [
        "machine_offline", "not_replicated", "retention_expired", "cursor_expired",
        "replication_quota_exhausted",
    ], "availability gaps are closed and typed")
    let catalog = CloudV2ReadCatalog.entries
    let grouped = Dictionary(grouping: catalog, by: { $0.type })
    try check(catalog.count == CloudV2ReadType.allCases.count
        && grouped.count == CloudV2ReadType.allCases.count
        && grouped.values.allSatisfy { $0.count == 1 },
        "every read type has exactly one classification")
    try check(CloudV2ReadCatalog.readTypeNames == CloudAppBridge.readTypes,
              "bridge admission derives from the v2 catalog")
    try check(CloudV2ReadCatalog.requiresWriteGate(for: "project-worktree-lifecycle-refresh"),
              "process-running refresh is command-classified")
    try check(CloudV2ReadCatalog.classification(for: "future-read") == nil,
              "unknown future reads fail closed")

    let bridgeKey = CloudDeviceKeyPair()
    let bridgeSecret = try CloudMasterSecret(rawRepresentation: Data(repeating: 0x64, count: 32))
    func identity() -> CloudAppIdentity {
        CloudAppIdentity(
            machineID: "Mac / 台灣", deviceID: "machine-device", keyID: "ms-1",
            masterSecret: bridgeSecret, signingKey: bridgeKey)
    }
    let offTransport = CloudAppBridgeTestTransport()
    let offRouter = CloudAppBridgeTestRouter()
    let offResults = CloudAppBridgeTestResults()
    let offBridge = CloudAppBridge(
        transport: offTransport, identity: identity(), sequencing: CloudAppBridgeTestSequence(),
        allowCloudCommands: { false }, commandRouter: offRouter,
        commandResult: { offResults.append($0) })
    try await offBridge.start()
    offTransport.yield(
        #"{"type":"project-worktree-lifecycle-refresh","session":"__clawdline_machine__","request":"refresh-off","project":"project-0123456789abcdef01234567"}"#,
        sequence: 1)
    try await waitForCloudAppBridge("refresh admission authority") {
        offResults.all().contains { $0.code == "cloud_commands_disabled" }
    }
    let offReads = await offRouter.recordedReads()
    try check(offReads.isEmpty, "disabled refresh never reaches its router")
    await offBridge.stop()

    let authority = CloudV2RefreshAuthorityFixture()
    let effectTransport = CloudAppBridgeTestTransport()
    let effectRouter = CloudAppBridgeTestRouter()
    let effectResults = CloudAppBridgeTestResults()
    let effectBridge = CloudAppBridge(
        transport: effectTransport, identity: identity(),
        sequencing: CloudAppBridgeTestSequence(), allowCloudCommands: { true },
        currentCommandEffectAuthority: { _, _ in await authority.current() },
        commandRouter: effectRouter, commandResult: { effectResults.append($0) })
    try await effectBridge.start()
    effectTransport.yield(
        #"{"type":"project-worktree-lifecycle-refresh","session":"__clawdline_machine__","request":"refresh-revoked","project":"project-0123456789abcdef01234567"}"#,
        sequence: 2)
    try await authority.waitForSecondCall()
    await authority.denyAndRelease()
    try await waitForCloudAppBridge("refresh point-of-no-return authority") {
        effectResults.all().contains { $0.code == "cloud_commands_disabled" }
    }
    let effectReads = await effectRouter.recordedReads()
    try check(effectReads.isEmpty, "revocation after admission prevents refresh effect")
    await effectBridge.stop()

    let refusedTransport = CloudAppBridgeTestTransport()
    let refusedRouter = CloudAppBridgeTestRouter()
    let refusedResults = CloudAppBridgeTestResults()
    let refusedBridge = CloudAppBridge(
        transport: refusedTransport, identity: identity(),
        sequencing: CloudAppBridgeTestSequence(), allowCloudCommands: { false },
        commandRouter: refusedRouter, commandResult: { refusedResults.append($0) })
    try await refusedBridge.start()
    await refusedTransport.refuse(CloudInboundCommand(
        channel: "ctl/Mac%20%2F%20%E5%8F%B0%E7%81%A3", sequence: 3, timestamp: 1,
        commandClass: .ctl, sender: "viewer", plaintext: Data(
            #"{"type":"project-worktree-lifecycle-refresh","session":"__clawdline_machine__","request":"refresh-ingress","project":"project-0123456789abcdef01234567"}"#.utf8)
    ), reason: .countCap)
    try await waitForCloudAppBridge("ingress refusal gate precedence") {
        refusedResults.all().contains { $0.code == "cloud_commands_disabled" }
    }
    let refusedReads = await refusedRouter.recordedReads()
    try check(refusedReads.isEmpty, "ingress pressure cannot relabel a disabled command")
    await refusedBridge.stop()

    return checks
}
