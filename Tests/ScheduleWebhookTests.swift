import Foundation
import CryptoKit

private final class ScheduleWebhookEventLog: @unchecked Sendable {
    private let lock = NSLock()
    private var values: [String] = []
    func add(_ value: String) { lock.lock(); values.append(value); lock.unlock() }
    func all() -> [String] { lock.lock(); defer { lock.unlock() }; return values }
}

private final class ScheduleWebhookCloudFixture: ScheduleWebhookCloudAPI, @unchecked Sendable {
    private let lock = NSLock()
    private var claim: ScheduleWebhookClaim?
    private var refuseFirstReceipt: Bool
    private let events: ScheduleWebhookEventLog

    init(claim: ScheduleWebhookClaim?, refuseFirstReceipt: Bool,
         events: ScheduleWebhookEventLog) {
        self.claim = claim; self.refuseFirstReceipt = refuseFirstReceipt; self.events = events
    }

    func activateScheduleWebhook(hookID: String, expectedRevision: Int,
                                 idempotencyKey: String) async throws
        -> ScheduleWebhookActivation {
        .init(hookID: hookID, state: "active", revision: expectedRevision + 1)
    }

    func claimScheduleWebhookDelivery(waitSeconds: Int) async throws
        -> ScheduleWebhookClaimResult {
        let value = takeClaim()
        return .init(delivery: value, serverTime: "2026-09-08T10:20:10.000Z",
                     pollAfterMilliseconds: value == nil ? 5_000 : 0)
    }

    func sendScheduleWebhookReceipt(deliveryID: String, receipt: ScheduleWebhookReceipt)
        async throws -> ScheduleWebhookReceiptAck {
        events.add("send:\(receipt.receiptVersion)")
        if takeReceiptFailure() {
            throw CloudAccountError.http(status: 503, code: "temporarily_unavailable")
        }
        events.add("ack:\(receipt.receiptVersion)")
        return .init(deliveryID: deliveryID, receiptVersion: receipt.receiptVersion,
                     state: receipt.kind, acknowledgedAt: "2026-09-08T10:20:11.000Z",
                     duplicate: false)
    }

    private func takeClaim() -> ScheduleWebhookClaim? {
        lock.lock(); defer { lock.unlock() }
        let value = claim; claim = nil; return value
    }
    private func takeReceiptFailure() -> Bool {
        lock.lock(); defer { lock.unlock() }
        let value = refuseFirstReceipt; refuseFirstReceipt = false; return value
    }
}

private final class ScheduleWebhookEffectFixture: ScheduleWebhookEffecting, @unchecked Sendable {
    private let lock = NSLock()
    private var dispatches = 0
    private var dispatchCalls = 0
    private var preparedTaskID: String?
    private var dispatchedTaskID: String?
    private var registeredTaskID: String?
    private var removedTaskIDs: [String] = []
    private var answers: [(Bool, String?)]
    private let events: ScheduleWebhookEventLog
    init(events: ScheduleWebhookEventLog,
         answers: [(Bool, String?)] = [(true, nil)]) {
        self.events = events; self.answers = answers
    }

    func prepare(scheduleID: String, taskID: String) throws -> ScheduleWebhookPreparation {
        lock.lock(); preparedTaskID = taskID; lock.unlock()
        events.add("prepare")
        return .init(taskFileSHA256: String(repeating: "c", count: 64), outcome: nil)
    }
    func dispatch(scheduleID: String, taskID: String, taskSecret: String)
        -> (accepted: Bool, code: String?) {
        lock.lock()
        dispatchCalls += 1
        if registeredTaskID == taskID {
            lock.unlock()
            events.add("join")
            return (true, nil)
        }
        dispatches += 1; dispatchedTaskID = taskID
        let answer = answers.isEmpty ? (true, nil) : answers.removeFirst()
        if answer.0 { registeredTaskID = taskID }
        lock.unlock()
        events.add("dispatch")
        return (answer.0, answer.1)
    }
    func taskState(scheduleID: String, taskID: String) -> String? {
        lock.lock(); defer { lock.unlock() }
        return registeredTaskID == taskID ? "briefed" : nil
    }
    func remove(taskID: String) {
        lock.lock(); removedTaskIDs.append(taskID); lock.unlock()
    }
    func snapshot() -> (Int, String?, String?) {
        lock.lock(); defer { lock.unlock() }
        return (dispatches, preparedTaskID, dispatchedTaskID)
    }
    func removed() -> [String] { lock.lock(); defer { lock.unlock() }; return removedTaskIDs }
    func calls() -> Int { lock.lock(); defer { lock.unlock() }; return dispatchCalls }
}

private struct ScheduleWebhookInjectedCrash: Error {}

private final class ScheduleWebhookCrashOnce: @unchecked Sendable {
    private let lock = NSLock()
    private var pending = true
    func fire() throws {
        lock.lock(); defer { lock.unlock() }
        if pending { pending = false; throw ScheduleWebhookInjectedCrash() }
    }
}

private final class ScheduleWebhookLeaseCloud: ScheduleWebhookCloudAPI, @unchecked Sendable {
    private let lock = NSLock()
    private var claims: [ScheduleWebhookClaim]
    private let acceptedLease: String?
    private let expireReceipt: Bool
    private var receipts: [ScheduleWebhookReceipt] = []
    private var claimCount = 0

    init(claims: [ScheduleWebhookClaim], acceptedLease: String?, expireReceipt: Bool = false) {
        self.claims = claims
        self.acceptedLease = acceptedLease
        self.expireReceipt = expireReceipt
    }

    func activateScheduleWebhook(hookID: String, expectedRevision: Int,
                                 idempotencyKey: String) async throws
        -> ScheduleWebhookActivation {
        .init(hookID: hookID, state: "active", revision: expectedRevision + 1)
    }

    func claimScheduleWebhookDelivery(waitSeconds: Int) async throws
        -> ScheduleWebhookClaimResult {
        lock.lock()
        claimCount += 1
        let claim = claims.isEmpty ? nil : claims.removeFirst()
        lock.unlock()
        return .init(delivery: claim, serverTime: "2026-09-08T10:20:10.000Z",
                     pollAfterMilliseconds: claim == nil ? 5_000 : 0)
    }

    func sendScheduleWebhookReceipt(deliveryID: String, receipt: ScheduleWebhookReceipt)
        async throws -> ScheduleWebhookReceiptAck {
        lock.lock(); receipts.append(receipt); lock.unlock()
        if expireReceipt { throw CloudAccountError.http(status: 409, code: "delivery_expired") }
        if receipt.receiptVersion == 1, receipt.leaseToken != acceptedLease {
            throw CloudAccountError.http(status: 409, code: "stale_lease")
        }
        return .init(deliveryID: deliveryID, receiptVersion: receipt.receiptVersion,
                     state: receipt.kind, acknowledgedAt: "2026-09-08T10:20:11.000Z",
                     duplicate: false)
    }

    func snapshot() -> (Int, [ScheduleWebhookReceipt]) {
        lock.lock(); defer { lock.unlock() }; return (claimCount, receipts)
    }
}

private final class ScheduleWebhookClock: @unchecked Sendable {
    private let lock = NSLock(); private var value: Date
    init(_ value: Date) { self.value = value }
    func now() -> Date { lock.lock(); defer { lock.unlock() }; return value }
    func advance(to epoch: Int64) {
        lock.lock(); value = Date(timeIntervalSince1970: TimeInterval(epoch)); lock.unlock()
    }
}

private final class ScheduleWebhookUnauthorizedCloud: ScheduleWebhookCloudAPI, @unchecked Sendable {
    private let lock = NSLock(); private var claims = 0
    func activateScheduleWebhook(hookID: String, expectedRevision: Int,
                                 idempotencyKey: String) async throws
        -> ScheduleWebhookActivation { throw CloudAccountError.invalidResponse }
    func claimScheduleWebhookDelivery(waitSeconds: Int) async throws
        -> ScheduleWebhookClaimResult {
        increment(); throw CloudAccountError.http(status: 401, code: "no_machine_credential")
    }
    func sendScheduleWebhookReceipt(deliveryID: String, receipt: ScheduleWebhookReceipt)
        async throws -> ScheduleWebhookReceiptAck { throw CloudAccountError.invalidResponse }
    private func increment() { lock.lock(); claims += 1; lock.unlock() }
    func count() -> Int { lock.lock(); defer { lock.unlock() }; return claims }
}

private final class ScheduleWebhookCounter: @unchecked Sendable {
    private let lock = NSLock(); private var value = 0
    func increment() { lock.lock(); value += 1; lock.unlock() }
    func count() -> Int { lock.lock(); defer { lock.unlock() }; return value }
}

private func scheduleWebhookAwait(_ operation: @escaping @Sendable () async -> Void) {
    let done = DispatchSemaphore(value: 0)
    Task.detached { await operation(); done.signal() }
    _ = done.wait(timeout: .now() + 5)
}

func runScheduleWebhookTests() {
    group("schedule webhook binding and delivery records are closed durable authorities") {
        let root = FileManager.default.temporaryDirectory
            .appendingPathComponent("clawdline-webhook-\(UUID().uuidString)", isDirectory: true)
        defer { try? FileManager.default.removeItem(at: root) }
        let bindingURL = root.appendingPathComponent("schedule-webhooks-v1.json")
        let bindings = ScheduleWebhookBindingStore(fileURL: bindingURL)
        let hook = "swh_0123456789abcdefghjkmnpqrs"
        let schedule = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
        do {
            let first = try bindings.bind(hookID: hook, scheduleID: schedule,
                                          replaceHookID: nil, at: 1_788_863_000)
            expect("binding stores the hook", first.hookID, hook)
            expect("binding stores the local schedule only on the Mac", first.scheduleID, schedule)
            expect("same binding is an idempotent replay",
                   try bindings.bind(hookID: hook, scheduleID: schedule,
                                     replaceHookID: nil, at: 1_788_863_001), first)
            let attributes = try? FileManager.default.attributesOfItem(atPath: bindingURL.path)
            let mode = (attributes?[.posixPermissions] as? NSNumber)?.intValue
            expect("binding file is mode 0600", mode, 0o600)
        } catch {
            check("a valid binding is accepted", false, String(describing: error))
        }

        let bindLedgerDirectory = root.appendingPathComponent("bind-ledger", isDirectory: true)
        let bindLedger = ScheduleWebhookBindLedger(directory: bindLedgerDirectory)
        let bindRequestID = "11111111-2222-4333-8444-555555555555"
        let bindDigest = String(repeating: "a", count: 64)
        do {
            if case .fresh = try bindLedger.begin(requestID: bindRequestID,
                                                   digest: bindDigest, now: 100) {
                check("a verified bind reserves its durable request digest", true)
            } else { check("a verified bind reserves its durable request digest", false) }
            let restartedLedger = ScheduleWebhookBindLedger(directory: bindLedgerDirectory)
            if case .pending = try restartedLedger.begin(requestID: bindRequestID,
                                                          digest: bindDigest, now: 101) {
                check("a restart joins the pending verified bind", true)
            } else { check("a restart joins the pending verified bind", false) }
            let answer = CloudCommandResult(status: 200, code: nil, body: Data("{\"ok\":true}".utf8))
            try restartedLedger.complete(requestID: bindRequestID, digest: bindDigest,
                                         result: answer, now: 102)
            if case .completed(let replay) = try bindLedger.begin(
                requestID: bindRequestID, digest: bindDigest, now: 103) {
                expect("a completed verified bind replays its durable response", replay, answer)
            } else { check("a completed verified bind replays its durable response", false) }
            if case .conflict = try bindLedger.begin(requestID: bindRequestID,
                digest: String(repeating: "b", count: 64), now: 104) {
                check("reusing a bind request id with changed intent conflicts", true)
            } else { check("reusing a bind request id with changed intent conflicts", false) }
        } catch {
            check("verified bind ledger remains available across restart", false,
                  String(describing: error))
        }

        let receiptOne = ScheduleWebhookReceipt(
            schema: "clawdline.schedule_webhook.receipt.v1", receiptVersion: 1,
            previousReceiptVersion: 0, kind: "mac_durable_accepted",
            occurredAt: "2026-09-08T10:20:03.000Z", macBuild: "test+1",
            leaseToken: "swhl_" + String(repeating: "a", count: 43), taskID: nil,
            outcomeCode: nil, taskTerminalState: nil, retryAt: nil)
        let receiptTwo = ScheduleWebhookReceipt(
            schema: "clawdline.schedule_webhook.receipt.v1", receiptVersion: 2,
            previousReceiptVersion: 1, kind: "schedule_dispatch_refused",
            occurredAt: "2026-09-08T10:20:04.000Z", macBuild: "test+1",
            leaseToken: nil, taskID: nil, outcomeCode: "schedule_disabled",
            taskTerminalState: nil, retryAt: nil)
        do {
            let encoded = try [receiptOne, receiptTwo].map { receipt in
                try JSONSerialization.jsonObject(with: JSONEncoder().encode(receipt)) as! [String: Any]
            }
            let exactKeys = Set(["schema", "receipt_version", "previous_receipt_version", "kind",
                                 "occurred_at", "mac_build", "lease_token", "task_id",
                                 "outcome_code", "task_terminal_state", "retry_at"])
            check("real receipt encoding emits the Cloud closed eleven-key schema",
                  encoded.allSatisfy { Set($0.keys) == exactKeys })
            check("receipt v1 alone carries the lease token",
                  encoded[0]["lease_token"] is String && encoded[1]["lease_token"] is NSNull)
            check("unused receipt fields encode as JSON null instead of disappearing",
                  encoded[0]["task_id"] is NSNull && encoded[0]["outcome_code"] is NSNull
                    && encoded[0]["task_terminal_state"] is NSNull
                    && encoded[0]["retry_at"] is NSNull)
        } catch {
            check("real receipt JSON encoding succeeds", false, String(describing: error))
        }

        let activationJSON = Data("""
        {"schema":"clawdline.schedule_webhook.management.v1","hook":{
          "hook_id":"\(hook)","machine_id":"mac-1","state":"active",
          "availability":"active","generation":1,"revision":1,
          "created_at":"2026-09-08T10:00:00.000Z","activated_at":"2026-09-08T10:01:00.000Z",
          "rotated_at":null,"disabled_at":null,"credential_fingerprint":"123456789abc"}}
        """.utf8)
        do {
            let activation = try ScheduleWebhookActivationDecoder.decode(
                activationJSON, hookID: hook, expectedRevision: 0)
            expect("Mac accepts the production compact activation response revision",
                   activation.revision, 1)
        } catch {
            check("Mac accepts the production compact activation response", false,
                  String(describing: error))
        }

        do {
            _ = try bindings.bind(
                hookID: "swh_1123456789abcdefghjkmnpqrs", scheduleID: schedule,
                replaceHookID: "swh_2123456789abcdefghjkmnpqrs", at: 1_788_863_002)
            check("replace_hook_id is a local compare-and-set", false)
        } catch let error as ScheduleWebhookStoreError {
            expect("replace_hook_id mismatch is typed", error, .bindingConflict)
        } catch {
            check("replace_hook_id mismatch is typed", false, String(describing: error))
        }

        try? Data("{\"clawdline_schedule_webhooks\":1,\"bindings\":[],\"unknown\":true}".utf8)
            .write(to: bindingURL)
        do {
            _ = try bindings.all()
            check("unknown binding-store fields fail closed", false)
        } catch let error as ScheduleWebhookStoreError {
            expect("unknown binding-store fields are unavailable, not empty", error, .unavailable)
        } catch {
            check("unknown binding-store fields are unavailable, not empty", false,
                  String(describing: error))
        }
        var unavailableProjection: [String: Any] = ["webhook_hook_id": hook]
        ScheduleWebhookProjection.apply(scheduleID: schedule, to: &unavailableProjection,
                                        store: bindings)
        expect("corrupt binding projects an explicit unavailable state",
               unavailableProjection["webhook_binding_availability"] as? String,
               "binding_store_unavailable")
        check("corrupt binding projection cannot retain a stale active hook id",
              unavailableProjection["webhook_hook_id"] == nil)

        let deliveryDir = root.appendingPathComponent("deliveries", isDirectory: true)
        let deliveries = ScheduleWebhookDeliveryStore(directory: deliveryDir)
        let claim = ScheduleWebhookClaim(
            deliveryID: "swd_0123456789abcdefghjkmnpqrs", hookID: hook,
            hookGeneration: 3, acceptedAt: "2026-09-08T10:20:00.000Z",
            expiresAt: "2026-09-09T10:20:00.000Z",
            deliveryDigest: String(repeating: "a", count: 64), attempt: 2,
            lease: .init(token: "swhl_abcdefghijklmnopqrstuvwxyz0123456789ABCDE",
                         revision: 2, expiresAt: "2026-09-08T10:21:00.000Z"))
        do {
            let first = try deliveries.reserve(claim: claim, scheduleID: schedule,
                                               now: 1_788_863_003)
            check("first durable reserve creates one row", first.created)
            let replay = try deliveries.reserve(claim: claim, scheduleID: schedule,
                                                now: 1_788_863_004)
            check("same delivery and digest joins the durable row", !replay.created)
            expect("a joined delivery keeps one task identity", replay.row.taskID, first.row.taskID)
            var prepared = replay.row
            prepared.taskID = "11111111-2222-4333-8444-555555555555"
            prepared.taskSecret = String(repeating: "b", count: 64)
            prepared.taskFileSHA256 = String(repeating: "c", count: 64)
            try deliveries.save(prepared)
            let reloaded = try ScheduleWebhookDeliveryStore(directory: deliveryDir)
                .row(deliveryID: claim.deliveryID)
            expect("stable task id survives a new store instance", reloaded?.taskID, prepared.taskID)
            expect("prepared task digest survives a new store instance",
                   reloaded?.taskFileSHA256, prepared.taskFileSHA256)
            let attributes = try? FileManager.default.attributesOfItem(atPath: deliveryDir.path)
            let mode = (attributes?[.posixPermissions] as? NSNumber)?.intValue
            expect("delivery journal directory is mode 0700", mode, 0o700)
        } catch {
            check("delivery reserve and reload succeed", false, String(describing: error))
        }

        var conflict = claim
        conflict.deliveryDigest = String(repeating: "d", count: 64)
        do {
            _ = try deliveries.reserve(claim: conflict, scheduleID: schedule,
                                       now: 1_788_863_005)
            check("one delivery id cannot change digest", false)
        } catch let error as ScheduleWebhookStoreError {
            expect("delivery digest collision is permanently typed", error, .deliveryConflict)
        } catch {
            check("delivery digest collision is permanently typed", false, String(describing: error))
        }

        check("empty poll uses bounded server delay",
              ScheduleWebhookBackoff.emptyPollMilliseconds(server: 5_000, unitRandom: 0.5)
                == 5_000)
        check("network backoff caps at thirty seconds",
              ScheduleWebhookBackoff.networkMilliseconds(failure: 99, unitRandom: 1) == 30_000)
        check("a 2xx resets network backoff",
              ScheduleWebhookBackoff.networkMilliseconds(failure: 1, unitRandom: 1) == 1_000)

        let behaviorRoot = root.appendingPathComponent("behavior", isDirectory: true)
        let behaviorBindings = ScheduleWebhookBindingStore(
            fileURL: behaviorRoot.appendingPathComponent("bindings.json"))
        let behaviorDeliveries = ScheduleWebhookDeliveryStore(
            directory: behaviorRoot.appendingPathComponent("deliveries", isDirectory: true))
        let identity = CloudMachineIdentity(accountID: "acct-1", machineID: "mac-1")
        let deliveryID = "swd_1123456789abcdefghjkmnpqrs"
        let accepted = "2026-09-08T10:20:00.000Z"
        let expires = "2026-09-09T10:20:00.000Z"
        let digest = RemoteAuth.hex(SHA256.hash(data: CloudCanonicalJSON.canonicalData(.object([
            "schema": .string("clawdline.schedule_webhook.v1"),
            "delivery_id": .string(deliveryID), "hook_id": .string(hook),
            "hook_generation": .int(3), "account_id": .string(identity.accountID),
            "machine_id": .string(identity.machineID), "accepted_at": .string(accepted),
            "expires_at": .string(expires),
        ]))))
        let durableClaim = ScheduleWebhookClaim(
            deliveryID: deliveryID, hookID: hook, hookGeneration: 3,
            acceptedAt: accepted, expiresAt: expires, deliveryDigest: digest, attempt: 1,
            lease: .init(token: "swhl_" + String(repeating: "a", count: 43), revision: 1,
                         expiresAt: "2026-09-08T10:21:00.000Z"))
        do { _ = try behaviorBindings.bind(hookID: hook, scheduleID: schedule,
                                            replaceHookID: nil, at: 1_788_862_800) }
        catch { check("behavior fixture binds", false, String(describing: error)) }
        let events = ScheduleWebhookEventLog()
        let cloud = ScheduleWebhookCloudFixture(
            claim: durableClaim, refuseFirstReceipt: true, events: events)
        let effect = ScheduleWebhookEffectFixture(events: events)
        let fixedNow = { Date(timeIntervalSince1970: 1_788_862_810) }

        scheduleWebhookAwait {
            let processor = ScheduleWebhookDeliveryProcessor(
                identity: identity, cloud: cloud, bindings: behaviorBindings,
                deliveries: behaviorDeliveries, effect: effect, now: fixedNow)
            do { _ = try await processor.pollOnce(waitSeconds: 0) } catch {}
        }
        let afterLoss = effect.snapshot()
        expect("receipt response loss produces no schedule effect before Cloud ack",
               afterLoss.0, 0)
        let pending = (try? behaviorDeliveries.row(deliveryID: deliveryID)) ?? nil
        expect("the exact durable receipt remains pending after response loss",
               pending?.pendingReceipt?.receiptVersion, 1)
        let stable = pending?.taskID

        scheduleWebhookAwait {
            let restarted = ScheduleWebhookDeliveryProcessor(
                identity: identity, cloud: cloud, bindings: behaviorBindings,
                deliveries: behaviorDeliveries, effect: effect, now: fixedNow)
            try? await restarted.resumeRows()
            try? await restarted.resumeRows()
        }
        let recovered = effect.snapshot()
        expect("Cloud ack releases exactly one ordinary dispatch across restart", recovered.0, 1)
        expect("restart dispatch uses the journal's stable task id", recovered.2, stable)
        let order = events.all()
        if let ackIndex = order.firstIndex(of: "ack:1"),
           let dispatchIndex = order.firstIndex(of: "dispatch") {
            check("dispatch occurs only after durable receipt ack",
                  ackIndex < dispatchIndex, "\(order)")
        } else {
            check("dispatch occurs only after durable receipt ack", false, "\(order)")
        }

        let crashRoot = root.appendingPathComponent("post-dispatch-crash", isDirectory: true)
        let crashBindings = ScheduleWebhookBindingStore(
            fileURL: crashRoot.appendingPathComponent("bindings.json"))
        let crashDeliveries = ScheduleWebhookDeliveryStore(
            directory: crashRoot.appendingPathComponent("deliveries", isDirectory: true))
        try? crashBindings.bind(hookID: hook, scheduleID: schedule,
                                replaceHookID: nil, at: 1_788_862_800)
        let crashEvents = ScheduleWebhookEventLog()
        let crashCloud = ScheduleWebhookCloudFixture(
            claim: durableClaim, refuseFirstReceipt: false, events: crashEvents)
        let crashEffect = ScheduleWebhookEffectFixture(events: crashEvents)
        let crash = ScheduleWebhookCrashOnce()
        scheduleWebhookAwait {
            let processor = ScheduleWebhookDeliveryProcessor(
                identity: identity, cloud: crashCloud, bindings: crashBindings,
                deliveries: crashDeliveries, effect: crashEffect, now: fixedNow,
                afterDispatch: { try crash.fire() })
            try? await processor.pollOnce(waitSeconds: 0)
        }
        scheduleWebhookAwait {
            let restarted = ScheduleWebhookDeliveryProcessor(
                identity: identity, cloud: crashCloud, bindings: crashBindings,
                deliveries: crashDeliveries, effect: crashEffect, now: fixedNow)
            try? await restarted.resumeRows()
        }
        expect("post-registration crash joins the stable task instead of starting another",
               crashEffect.snapshot().0, 1)
        expect("restart recognizes the registered stable task before calling dispatch again",
               crashEffect.calls(), 1)
        let crashRow = (try? crashDeliveries.row(deliveryID: deliveryID)) ?? nil
        expect("stable-task join advances to dispatch accepted after restart",
               crashRow?.state, .dispatchAccepted)

        let deferredRoot = root.appendingPathComponent("deferred", isDirectory: true)
        let deferredBindings = ScheduleWebhookBindingStore(
            fileURL: deferredRoot.appendingPathComponent("bindings.json"))
        let deferredDeliveries = ScheduleWebhookDeliveryStore(
            directory: deferredRoot.appendingPathComponent("deliveries", isDirectory: true))
        try? deferredBindings.bind(hookID: hook, scheduleID: schedule,
                                   replaceHookID: nil, at: 1_788_862_800)
        let deferredID = "swd_2123456789abcdefghjkmnpqrs"
        let deferredDigest = RemoteAuth.hex(SHA256.hash(
            data: CloudCanonicalJSON.canonicalData(.object([
                "schema": .string("clawdline.schedule_webhook.v1"),
                "delivery_id": .string(deferredID), "hook_id": .string(hook),
                "hook_generation": .int(3), "account_id": .string(identity.accountID),
                "machine_id": .string(identity.machineID), "accepted_at": .string(accepted),
                "expires_at": .string(expires),
            ]))))
        let deferredClaim = ScheduleWebhookClaim(
            deliveryID: deferredID, hookID: hook, hookGeneration: 3,
            acceptedAt: accepted, expiresAt: expires, deliveryDigest: deferredDigest, attempt: 1,
            lease: .init(token: "swhl_" + String(repeating: "b", count: 43), revision: 1,
                         expiresAt: "2026-09-08T10:21:00.000Z"))
        let deferredEvents = ScheduleWebhookEventLog()
        let deferredCloud = ScheduleWebhookCloudFixture(
            claim: deferredClaim, refuseFirstReceipt: false, events: deferredEvents)
        let deferredEffect = ScheduleWebhookEffectFixture(
            events: deferredEvents, answers: [(false, "terminal_busy"), (true, nil)])
        let clock = ScheduleWebhookClock(Date(timeIntervalSince1970: 1_788_862_810))
        scheduleWebhookAwait {
            let processor = ScheduleWebhookDeliveryProcessor(
                identity: identity, cloud: deferredCloud, bindings: deferredBindings,
                deliveries: deferredDeliveries, effect: deferredEffect,
                now: { clock.now() }, unitRandom: { 1 })
            try? await processor.pollOnce(waitSeconds: 0)
            try? await processor.resumeRows()
        }
        expect("deferred receipt ack does not immediately recurse into another dispatch",
               deferredEffect.snapshot().0, 1)
        let deferredRow = (try? deferredDeliveries.row(deliveryID: deferredID)) ?? nil
        check("deferred retry deadline is durable", (deferredRow?.retryAt ?? 0) > 1_788_862_810)
        if let retryAt = deferredRow?.retryAt { clock.advance(to: retryAt + 1) }
        scheduleWebhookAwait {
            let restarted = ScheduleWebhookDeliveryProcessor(
                identity: identity, cloud: deferredCloud, bindings: deferredBindings,
                deliveries: deferredDeliveries, effect: deferredEffect,
                now: { clock.now() }, unitRandom: { 1 })
            try? await restarted.resumeRows()
            try? await restarted.resumeRows()
        }
        expect("deferred dispatch retries once after its durable deadline",
               deferredEffect.snapshot().0, 2)

        let unauthorizedCloud = ScheduleWebhookUnauthorizedCloud()
        let unauthorized = ScheduleWebhookCounter()
        scheduleWebhookAwait {
            let poller = ScheduleWebhookPoller()
            let processor = ScheduleWebhookDeliveryProcessor(
                identity: identity, cloud: unauthorizedCloud,
                bindings: ScheduleWebhookBindingStore(
                    fileURL: root.appendingPathComponent("unauthorized-bindings.json")),
                deliveries: ScheduleWebhookDeliveryStore(
                    directory: root.appendingPathComponent("unauthorized-deliveries")),
                effect: ScheduleWebhookEffectFixture(events: ScheduleWebhookEventLog()))
            await poller.start(processor: processor) {
                unauthorized.increment()
            }
            try? await Task.sleep(nanoseconds: 40_000_000)
            await poller.stop()
        }
        expect("401 no_machine_credential invokes the lifecycle refusal once",
               unauthorized.count(), 1)
        expect("401 stops polling instead of entering network backoff", unauthorizedCloud.count(), 1)

        let isolationRoot = root.appendingPathComponent("corrupt-row", isDirectory: true)
        let isolationBindings = ScheduleWebhookBindingStore(
            fileURL: isolationRoot.appendingPathComponent("bindings.json"))
        let isolationDeliveriesURL = isolationRoot.appendingPathComponent("deliveries")
        let isolationDeliveries = ScheduleWebhookDeliveryStore(directory: isolationDeliveriesURL)
        try? isolationBindings.bind(hookID: hook, scheduleID: schedule,
                                    replaceHookID: nil, at: 1_788_862_800)
        try? FileManager.default.createDirectory(at: isolationDeliveriesURL,
                                                 withIntermediateDirectories: true)
        try? Data("{corrupt".utf8).write(to: isolationDeliveriesURL
            .appendingPathComponent("swd_9123456789abcdefghjkmnpqrs.json"))
        let isolationEffect = ScheduleWebhookEffectFixture(events: ScheduleWebhookEventLog())
        let isolationCloud = ScheduleWebhookCloudFixture(
            claim: durableClaim, refuseFirstReceipt: false, events: ScheduleWebhookEventLog())
        scheduleWebhookAwait {
            let processor = ScheduleWebhookDeliveryProcessor(
                identity: identity, cloud: isolationCloud, bindings: isolationBindings,
                deliveries: isolationDeliveries, effect: isolationEffect, now: fixedNow)
            try? await processor.pollOnce(waitSeconds: 0)
        }
        expect("one corrupt journal row does not block a newly claimable delivery",
               isolationEffect.snapshot().0, 1)
        let quarantined = (try? FileManager.default.contentsOfDirectory(
            at: isolationDeliveriesURL.appendingPathComponent("quarantine"),
            includingPropertiesForKeys: nil)) ?? []
        expect("the unreadable journal bytes are quarantined for diagnosis", quarantined.count, 1)

        let leaseRoot = root.appendingPathComponent("lease-rollover", isDirectory: true)
        let leaseBindings = ScheduleWebhookBindingStore(
            fileURL: leaseRoot.appendingPathComponent("bindings.json"))
        let leaseDeliveries = ScheduleWebhookDeliveryStore(
            directory: leaseRoot.appendingPathComponent("deliveries"))
        try? leaseBindings.bind(hookID: hook, scheduleID: schedule,
                                replaceHookID: nil, at: 1_788_862_800)
        let renewedToken = "swhl_" + String(repeating: "d", count: 43)
        let renewedClaim = ScheduleWebhookClaim(
            deliveryID: durableClaim.deliveryID, hookID: durableClaim.hookID,
            hookGeneration: durableClaim.hookGeneration, acceptedAt: durableClaim.acceptedAt,
            expiresAt: durableClaim.expiresAt, deliveryDigest: durableClaim.deliveryDigest,
            attempt: 2, lease: .init(token: renewedToken, revision: 2,
                                     expiresAt: "2026-09-08T10:22:00.000Z"))
        let leaseCloud = ScheduleWebhookLeaseCloud(
            claims: [durableClaim, renewedClaim], acceptedLease: renewedToken)
        let leaseEffect = ScheduleWebhookEffectFixture(events: ScheduleWebhookEventLog())
        scheduleWebhookAwait {
            let processor = ScheduleWebhookDeliveryProcessor(
                identity: identity, cloud: leaseCloud, bindings: leaseBindings,
                deliveries: leaseDeliveries, effect: leaseEffect, now: fixedNow)
            try? await processor.pollOnce(waitSeconds: 0)
            try? await processor.pollOnce(waitSeconds: 0)
        }
        let leaseSnapshot = leaseCloud.snapshot()
        expect("stale v1 receipt permits a same-delivery re-claim", leaseSnapshot.0, 2)
        check("re-claim replaces the stale v1 token and later receipts carry null",
              leaseSnapshot.1.contains { $0.receiptVersion == 1 && $0.leaseToken == renewedToken }
                && leaseSnapshot.1.contains { $0.receiptVersion == 2 && $0.leaseToken == nil })
        expect("lease recovery releases one stable schedule effect", leaseEffect.snapshot().0, 1)

        let expiryRoot = root.appendingPathComponent("delivery-expiry", isDirectory: true)
        let expiryBindings = ScheduleWebhookBindingStore(
            fileURL: expiryRoot.appendingPathComponent("bindings.json"))
        let expiryDeliveries = ScheduleWebhookDeliveryStore(
            directory: expiryRoot.appendingPathComponent("deliveries"))
        try? expiryBindings.bind(hookID: hook, scheduleID: schedule,
                                 replaceHookID: nil, at: 1_788_862_800)
        let expiryCloud = ScheduleWebhookLeaseCloud(
            claims: [durableClaim], acceptedLease: nil, expireReceipt: true)
        let expiryEffect = ScheduleWebhookEffectFixture(events: ScheduleWebhookEventLog())
        scheduleWebhookAwait {
            let processor = ScheduleWebhookDeliveryProcessor(
                identity: identity, cloud: expiryCloud, bindings: expiryBindings,
                deliveries: expiryDeliveries, effect: expiryEffect, now: fixedNow)
            try? await processor.pollOnce(waitSeconds: 0)
        }
        let expiredRow = (try? expiryDeliveries.row(deliveryID: deliveryID)) ?? nil
        expect("Cloud 24-hour expiry terminalizes the local journal", expiredRow?.state, .expired)
        expect("expiry removes the prepared but unregistered ordinary task",
               expiryEffect.removed().count, 1)
        expect("expiry never invokes the ordinary runner", expiryEffect.snapshot().0, 0)

        let cleanupRoot = root.appendingPathComponent("journal-cleanup", isDirectory: true)
        let cleanupDeliveries = ScheduleWebhookDeliveryStore(directory: cleanupRoot)
        var cleanupRow = try? cleanupDeliveries.reserve(
            claim: durableClaim, scheduleID: schedule, now: 100).row
        cleanupRow?.state = .taskTerminal
        cleanupRow?.lastReceiptVersion = 3
        cleanupRow?.cloudAcknowledgedThrough = 3
        cleanupRow?.terminalAt = 100
        if let cleanupRow { try? cleanupDeliveries.save(cleanupRow) }
        var nonterminalClaim = durableClaim
        nonterminalClaim.deliveryID = "swd_3123456789abcdefghjkmnpqrs"
        nonterminalClaim.deliveryDigest = String(repeating: "3", count: 64)
        _ = try? cleanupDeliveries.reserve(
            claim: nonterminalClaim, scheduleID: schedule, now: 100)
        var unacknowledgedClaim = durableClaim
        unacknowledgedClaim.deliveryID = "swd_4123456789abcdefghjkmnpqrs"
        unacknowledgedClaim.deliveryDigest = String(repeating: "4", count: 64)
        var unacknowledgedRow = try? cleanupDeliveries.reserve(
            claim: unacknowledgedClaim, scheduleID: schedule, now: 100).row
        unacknowledgedRow?.state = .taskTerminal
        unacknowledgedRow?.lastReceiptVersion = 3
        unacknowledgedRow?.cloudAcknowledgedThrough = 2
        unacknowledgedRow?.terminalAt = 100
        if let unacknowledgedRow { try? cleanupDeliveries.save(unacknowledgedRow) }
        let cleanupClock = ScheduleWebhookClock(Date(timeIntervalSince1970:
            TimeInterval(100 + 30 * 24 * 3600 - 1)))
        let cleanupProcessor = ScheduleWebhookDeliveryProcessor(
            identity: identity, cloud: ScheduleWebhookCloudFixture(
                claim: nil, refuseFirstReceipt: false, events: ScheduleWebhookEventLog()),
            bindings: ScheduleWebhookBindingStore(
                fileURL: root.appendingPathComponent("cleanup-bindings.json")),
            deliveries: cleanupDeliveries,
            effect: ScheduleWebhookEffectFixture(events: ScheduleWebhookEventLog()),
            now: { cleanupClock.now() })
        scheduleWebhookAwait { try? await cleanupProcessor.resumeRows() }
        check("lifecycle cleanup preserves a terminal journal just before thirty days",
              (try? cleanupDeliveries.row(deliveryID: deliveryID)) != nil)
        cleanupClock.advance(to: 100 + 30 * 24 * 3600)
        scheduleWebhookAwait { try? await cleanupProcessor.resumeRows() }
        check("lifecycle cleanup removes the acknowledged terminal journal at thirty days",
              (try? cleanupDeliveries.row(deliveryID: deliveryID)) == nil)
        check("lifecycle cleanup retains nonterminal journals past thirty days",
              (try? cleanupDeliveries.row(deliveryID: nonterminalClaim.deliveryID)) != nil)
        check("lifecycle cleanup retains terminal journals until Cloud acknowledgement catches up",
              (try? cleanupDeliveries.row(deliveryID: unacknowledgedClaim.deliveryID)) != nil)
    }
}
