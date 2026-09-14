import Foundation
import XCTest
#if canImport(CryptoKit)
import CryptoKit
#else
import Crypto
#endif
@testable import ClawdlineApplication
@testable import ClawdlineLinux

private final class PairingLocked<Value>: @unchecked Sendable {
    private let lock = NSLock()
    private var value: Value

    init(_ value: Value) { self.value = value }

    func read() -> Value {
        lock.lock(); defer { lock.unlock() }
        return value
    }

    func update(_ body: (inout Value) -> Void) {
        lock.lock(); body(&value); lock.unlock()
    }
}

private final class PairingMemorySecretStore: SecretStore, @unchecked Sendable {
    private enum InjectedFailure: Error { case persist }
    let capabilities: Set<HostCapability> = [.secrets]
    private let values = PairingLocked<[String: Data]>([:])
    private let rotateCount = PairingLocked(0)
    private let failRotateAt: Int?

    init(failRotateAt: Int? = nil) { self.failRotateAt = failRotateAt }

    func data(for account: String) throws -> Data? { values.read()[account] }
    func set(_ data: Data, for account: String) throws {
        values.update { $0[account] = data }
    }
    func loadOrCreate(
        _ account: String, create: @Sendable () throws -> Data
    ) throws -> Data {
        if let existing = values.read()[account] { return existing }
        let made = try create()
        values.update { $0[account] = made }
        return made
    }
    func rotate(
        _ account: String, replace: @Sendable (Data?) throws -> Data
    ) throws -> Data {
        let made = try replace(values.read()[account])
        var shouldFail = false
        rotateCount.update {
            $0 += 1
            shouldFail = $0 == failRotateAt
        }
        if shouldFail { throw InjectedFailure.persist }
        values.update { $0[account] = made }
        return made
    }
    func remove(_ account: String) throws { values.update { $0.removeValue(forKey: account) } }
}

private final class PairingCompatibilityClient: CloudCompatibilityPairingClient,
                                                @unchecked Sendable {
    let invitation: CloudPairingInvitationStart
    let ready: CloudPairingInvitationPoll
    let expectedSecretHash: Data
    let deliveredFingerprint: String
    private let completed = PairingLocked(0)
    private let starts = PairingLocked(0)
    private let deliveredBlobs = PairingLocked<[String]>([])
    private let failedCompletionAttempts: Set<Int>

    init(invitation: CloudPairingInvitationStart, ready: CloudPairingInvitationPoll,
         expectedSecretHash: Data, deliveredFingerprint: String,
         failedCompletionAttempts: Set<Int> = []) {
        self.invitation = invitation
        self.ready = ready
        self.expectedSecretHash = expectedSecretHash
        self.deliveredFingerprint = deliveredFingerprint
        self.failedCompletionAttempts = failedCompletionAttempts
    }

    func startPairingInvitation(secretHash: Data) async throws -> CloudPairingInvitationStart {
        guard secretHash == expectedSecretHash else { throw CloudAccountError.invalidPairingBlob }
        starts.update { $0 += 1 }
        return invitation
    }

    func pollPairingInvitation(invitationID: String) async throws
        -> CloudPairingInvitationPoll {
        guard invitationID == invitation.invitationID else {
            throw CloudAccountError.invalidResponse
        }
        return ready
    }

    func completePairing(
        pairingID: String, blob: CloudOpaquePairingBlob
    ) async throws -> CloudPairingDelivery {
        guard pairingID == "pair-linux", !blob.wireBase64.isEmpty else {
            throw CloudAccountError.invalidResponse
        }
        var attempt = 0
        completed.update {
            $0 += 1
            attempt = $0
        }
        deliveredBlobs.update { $0.append(blob.wireBase64) }
        if failedCompletionAttempts.contains(attempt) {
            throw URLError(.networkConnectionLost)
        }
        return CloudPairingDelivery(fingerprint: deliveredFingerprint)
    }

    func completionCount() -> Int { completed.read() }
    func startCount() -> Int { starts.read() }
    func blobs() -> [String] { deliveredBlobs.read() }
}

private struct BrowserPairingFixture {
    let authority: CloudExecutorIdentityAuthority
    let client: PairingCompatibilityClient
    let handover: CloudCompatibilityPairingMachineHandover
    let viewerFingerprint: String
}

private func makeBrowserPairingFixture(
    store: PairingMemorySecretStore = PairingMemorySecretStore(),
    apiViewerDeviceID: String = "viewer_phone",
    failedCompletionAttempts: Set<Int> = []
) throws -> BrowserPairingFixture {
    let now: Int64 = 1_000_000
    let invitationSecret = Data(repeating: 0x31, count: 32)
    let viewerSigning = try CloudDeviceKeyPair(
        privateKeyRaw: Data(repeating: 0x42, count: 32))
    let viewerEphemeralPrivate = Data(repeating: 0x43, count: 32)
    let offer = CloudPairingOffer(
        pairingID: "pair-linux",
        claimNonce: Data(repeating: 0x44, count: 32).base64EncodedString(),
        pairingNonce: Data(repeating: 0x45, count: 32).base64EncodedString(),
        accountID: "usr_alpha", viewerDeviceID: "viewer_phone",
        viewerSigningKey: viewerSigning.publicKeyRaw.base64EncodedString(),
        viewerEphemeralKey: try CloudPairing.x25519PublicKey(
            privateKeyRaw: viewerEphemeralPrivate).base64EncodedString(),
        viewerFingerprint: viewerSigning.pairingFingerprint,
        expiresAt: now + 500_000)
    let fragment = try CloudCompatibilityPairing.encodeOfferFragment(
        offer, nowMilliseconds: now)
    let nonce = try AES.GCM.Nonce(data: Data(repeating: 0x46, count: 12))
    let box = try AES.GCM.seal(
        Data(fragment.utf8), using: SymmetricKey(data: invitationSecret), nonce: nonce,
        authenticating: Data("clawdline-pairing-invitation-v1\0invite-linux".utf8))
    let encrypted = try CloudOpaquePairingBlob(
        base64: try XCTUnwrap(box.combined).base64EncodedString())

    let authority = CloudExecutorIdentityAuthority(store: store)
    _ = try authority.provision(
        accountID: "usr_alpha", machineID: "mac_linux",
        deviceKey: CloudDeviceKeyPair(
            privateKeyRaw: Data(repeating: 0x47, count: 32)),
        masterSecret: CloudMasterSecret(
            rawRepresentation: Data(repeating: 0x48, count: 32)))
    let client = PairingCompatibilityClient(
        invitation: CloudPairingInvitationStart(
            invitationID: "invite-linux",
            expiresAt: Date(timeIntervalSince1970: Double(now + 500_000) / 1_000),
            expiresIn: 500),
        ready: .ready(
            accountID: "usr_alpha", viewerDeviceID: apiViewerDeviceID,
            machineID: "mac_linux", encryptedOffer: encrypted),
        expectedSecretHash: Data(SHA256.hash(data: invitationSecret)),
        deliveredFingerprint: viewerSigning.pairingFingerprint,
        failedCompletionAttempts: failedCompletionAttempts)
    let handover = CloudCompatibilityPairingMachineHandover(
        client: client, authority: authority, nowMilliseconds: { now },
        randomBytes: { _ in invitationSecret })
    return BrowserPairingFixture(
        authority: authority, client: client, handover: handover,
        viewerFingerprint: viewerSigning.pairingFingerprint)
}

extension LinuxRuntimeContractTests {
    func testPairBrowserCommandRequiresProtectedConfig() throws {
        XCTAssertEqual(
            try LinuxCompositionCommand.parse([
                "pair-browser", "--config", "/etc/clawdline/daemon.json"
            ]),
            .pairBrowser("/etc/clawdline/daemon.json"))
        XCTAssertThrowsError(try LinuxCompositionCommand.parse(["pair-browser"]))
    }

    func testPairBrowserNamesDaemonOnlyForAnActualWriterLock() throws {
        let locked = LinuxBrowserPairing.pairingError(for: .writerLockHeld)
        XCTAssertEqual(locked.code, "cloud_pairing_failed")
        XCTAssertTrue(locked.message.contains("Stop clawdline-daemon"))

        let persist = LinuxBrowserPairing.pairingError(for: .persist)
        XCTAssertEqual(persist.code, "cloud_pairing_failed")
        XCTAssertTrue(persist.message.contains("persist"))
        XCTAssertFalse(persist.message.contains("clawdline-daemon"))
    }

    func testHeadlessPairingEmitsInvitationOnceAndReturnsOnlyIdentityReceipt() async throws {
        let emitted = PairingLocked<[String]>([])
        let advances = PairingLocked(0)
        let url = try XCTUnwrap(URL(
            string: "https://app.clawdline.com/#pair=one-time-secret-fragment"))
        let start = CloudCompatibilityPairingStart(
            verificationURL: url, accountID: "usr_alpha", machineID: "mac_linux",
            machineFingerprint: "AAAA-BBBB-CCCC-DDDD",
            expiresAtMilliseconds: 601_000)
        let receipt = CloudCompatibilityPairingReceipt(
            accountID: "usr_alpha", machineID: "mac_linux",
            viewerDeviceID: "viewer_phone", viewerFingerprint: "EEEE-FFFF-GGGG-HHHH",
            machineFingerprint: "AAAA-BBBB-CCCC-DDDD")

        let output = try await LinuxBrowserPairing.execute(
            emit: { data in
                emitted.update { $0.append(String(decoding: data, as: UTF8.self)) }
            },
            nowMilliseconds: { 1_000 },
            sleep: { _ in },
            requestTimeoutNanoseconds: 50_000_000,
            begin: { start },
            advance: {
                var answer = CloudCompatibilityPairingProgress.waiting
                advances.update { value in
                    value += 1
                    if value == 2 { answer = .complete(receipt) }
                }
                return answer
            })

        XCTAssertEqual(advances.read(), 2)
        XCTAssertEqual(emitted.read().count, 1)
        XCTAssertTrue(emitted.read()[0].contains("browser_pairing_required"))
        XCTAssertTrue(emitted.read()[0].contains("one-time-secret-fragment"))
        let final = String(decoding: output, as: UTF8.self)
        XCTAssertTrue(final.contains("browser_pairing_complete"))
        XCTAssertTrue(final.contains("usr_alpha"))
        XCTAssertTrue(final.contains("mac_linux"))
        XCTAssertTrue(final.contains("viewer_phone"))
        XCTAssertFalse(final.contains("one-time-secret-fragment"))
    }

    func testHeadlessPairingExpiresWithoutUnboundedPolling() async throws {
        let now = PairingLocked<Int64>(1_000)
        let advances = PairingLocked(0)
        let start = CloudCompatibilityPairingStart(
            verificationURL: try XCTUnwrap(URL(
                string: "https://app.clawdline.com/#pair=short-lived-secret")),
            accountID: "usr_alpha", machineID: "mac_linux",
            machineFingerprint: "AAAA-BBBB-CCCC-DDDD",
            expiresAtMilliseconds: 2_000)

        do {
            _ = try await LinuxBrowserPairing.execute(
                emit: { _ in },
                nowMilliseconds: { now.read() },
                sleep: { _ in now.update { $0 = 2_000 } },
                requestTimeoutNanoseconds: 50_000_000,
                begin: { start },
                advance: {
                    advances.update { $0 += 1 }
                    return .waiting
                })
            XCTFail("an expired invitation must stop polling")
        } catch let error as LinuxCompositionError {
            XCTAssertEqual(error.code, "cloud_pairing_failed")
            XCTAssertTrue(error.message.contains("expired"))
        }
        XCTAssertEqual(advances.read(), 1)
    }

    func testInvitationDiagnosticsRedactTheOneTimeSecret() throws {
        let secret = Data(repeating: 0x7a, count: CloudPairingInvitation.secretBytes)
        let invitation = try CloudPairingInvitation(
            invitationID: "invite-alpha", secret: secret,
            expiresAtMilliseconds: 600_000)
        XCTAssertFalse(invitation.description.contains(secret.base64EncodedString()))
        XCTAssertTrue(invitation.description.contains("<redacted>"))
        XCTAssertFalse(String(reflecting: invitation).contains(secret.base64EncodedString()))
    }

    func testCompatibilityHandoverDecryptsBrowserFragmentBeforePinningViewer() async throws {
        let fixture = try makeBrowserPairingFixture()

        let started = try await fixture.handover.begin()
        XCTAssertEqual(started.accountID, "usr_alpha")
        XCTAssertEqual(started.machineID, "mac_linux")
        guard case .complete(let receipt) = try await fixture.handover.advance() else {
            return XCTFail("the ready browser offer must complete")
        }
        XCTAssertEqual(receipt.viewerDeviceID, "viewer_phone")
        XCTAssertEqual(fixture.client.completionCount(), 1)
        XCTAssertTrue(try fixture.authority.snapshot().pairedDevices.contains {
            $0.deviceID == "viewer_phone"
        })
    }

    func testMismatchedAPIViewerCannotPolluteProtectedPendingHandover() async throws {
        let fixture = try makeBrowserPairingFixture(apiViewerDeviceID: "viewer_attacker")
        _ = try await fixture.handover.begin()
        do {
            _ = try await fixture.handover.advance()
            XCTFail("a clear API viewer id must match the authenticated encrypted offer")
        } catch let error as CloudExecutorIdentityError {
            XCTAssertEqual(error, .identityMismatch)
        }
        XCTAssertFalse(try fixture.authority.snapshot().hasPreparedHandover)
        XCTAssertEqual(fixture.client.completionCount(), 0)
    }

    func testLostCloudCompletionResponseRetriesTheIdenticalPreparedDelivery() async throws {
        let fixture = try makeBrowserPairingFixture(failedCompletionAttempts: [1])
        _ = try await fixture.handover.begin()
        let firstAdvance = try await fixture.handover.advance()
        XCTAssertEqual(firstAdvance, .retrying(.deliveryUncertain))
        XCTAssertTrue(try fixture.authority.snapshot().hasPreparedHandover)

        guard case .complete(let receipt) = try await fixture.handover.advance() else {
            return XCTFail("the same prepared delivery must reconcile after response loss")
        }
        XCTAssertEqual(receipt.viewerDeviceID, "viewer_phone")
        XCTAssertEqual(fixture.client.startCount(), 1)
        XCTAssertEqual(fixture.client.completionCount(), 2)
        XCTAssertEqual(Set(fixture.client.blobs()).count, 1)
        XCTAssertFalse(try fixture.authority.snapshot().hasPreparedHandover)
    }

    func testLocalCommitFailureRetriesTheIdenticalCloudDeliveryAndPendingState() async throws {
        let fixture = try makeBrowserPairingFixture(
            store: PairingMemorySecretStore(failRotateAt: 2))
        _ = try await fixture.handover.begin()
        let firstAdvance = try await fixture.handover.advance()
        XCTAssertEqual(firstAdvance, .retrying(.localCommitPending))
        XCTAssertTrue(try fixture.authority.snapshot().hasPreparedHandover)

        guard case .complete(let receipt) = try await fixture.handover.advance() else {
            return XCTFail("the pending local commit must reconcile without a new invitation")
        }
        XCTAssertEqual(receipt.viewerDeviceID, "viewer_phone")
        XCTAssertEqual(fixture.client.startCount(), 1)
        XCTAssertEqual(fixture.client.completionCount(), 2)
        XCTAssertEqual(Set(fixture.client.blobs()).count, 1)
        XCTAssertFalse(try fixture.authority.snapshot().hasPreparedHandover)
    }

    func testCloudRequestTimeoutCancelsAStalledPairingDependency() async throws {
        let cancelled = PairingLocked(false)
        let advances = PairingLocked(0)
        let start = CloudCompatibilityPairingStart(
            verificationURL: try XCTUnwrap(URL(
                string: "https://app.clawdline.com/#pair=bounded-secret")),
            accountID: "usr_alpha", machineID: "mac_linux",
            machineFingerprint: "AAAA-BBBB-CCCC-DDDD",
            expiresAtMilliseconds: 601_000)
        do {
            _ = try await LinuxBrowserPairing.execute(
                emit: { _ in }, nowMilliseconds: { 1_000 }, sleep: { _ in },
                requestTimeoutNanoseconds: 1_000_000,
                begin: { start },
                advance: {
                    advances.update { $0 += 1 }
                    return try await withTaskCancellationHandler(operation: {
                        while !Task.isCancelled { await Task.yield() }
                        throw CancellationError()
                    }, onCancel: { cancelled.update { $0 = true } })
                })
            XCTFail("a stalled Cloud request must time out")
        } catch let error as LinuxCompositionError {
            XCTAssertEqual(error.code, "cloud_pairing_failed")
            XCTAssertTrue(error.message.contains("reconcile"))
        }
        XCTAssertTrue(cancelled.read())
        XCTAssertEqual(advances.read(), LinuxBrowserPairing.maximumRecoveryAttempts + 1)
    }
}
