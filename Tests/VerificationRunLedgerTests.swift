import Foundation
import SQLite3

private let verificationRepository = String(repeating: "a", count: 64)
private let verificationTreeA = String(repeating: "b", count: 40)
private let verificationTreeB = String(repeating: "c", count: 40)
private let verificationCommand = String(repeating: "d", count: 64)
private let verificationEnvironment = String(repeating: "e", count: 64)
private let verificationLog = String(repeating: "f", count: 64)

private func verificationStore() -> (URL, URL) {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("verification-run-ledger-\(UUID().uuidString)", isDirectory: true)
    try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    return (directory.appendingPathComponent("usage.sqlite3"), directory)
}

private func verificationRequest(
    requestID: String = UUID().uuidString.lowercased(),
    taskID: String? = UUID().uuidString.lowercased(),
    subject: VerificationRunLedger.Subject = .commitTree(treeSHA: verificationTreeA),
    kind: VerificationRunLedger.VerificationKind = .focused,
    variant: VerificationRunLedger.Variant = .baseline,
    mutationID: String? = nil,
    baselineReceiptID: String? = nil
) -> VerificationRunLedger.ReservationRequest {
    VerificationRunLedger.ReservationRequest(
        requestID: requestID, repositorySHA256: verificationRepository, taskID: taskID,
        subject: subject, questionID: "verification.exact-reuse", verificationKind: kind,
        variant: variant, mutationID: mutationID, baselineReceiptID: baselineReceiptID,
        commandSHA256: verificationCommand, environmentSHA256: verificationEnvironment)
}

private func verificationOutcome(
    state: VerificationRunLedger.OutcomeState = .passed,
    completionID: String = UUID().uuidString.lowercased(),
    fullSeal: String? = nil,
    inconclusiveCode: String? = nil
) -> VerificationRunLedger.Outcome {
    VerificationRunLedger.Outcome(
        completionID: completionID, state: state,
        exitStatus: state == .passed ? 0 : 1,
        checksPassed: state == .passed ? 17 : 0,
        checksFailed: state == .failed ? 1 : 0,
        durationMS: 1200, logSHA256: verificationLog,
        inconclusiveCode: inconclusiveCode, fullSuiteReceiptSHA256: fullSeal)
}

private func reservedRun(_ decision: VerificationRunLedger.ReserveDecision)
    -> VerificationRunLedger.StoredRun? {
    switch decision {
    case .runRequired(let run, _), .active(let run), .reusable(let run): return run
    default: return nil
    }
}

private func responseObject(_ response: RemoteServer.Response) -> [String: Any]? {
    (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]
}

private func verificationHTTPRequest(method: String, path: String,
                                     object: [String: Any]? = nil) -> RemoteServer.Request {
    let head = "\(method) \(path) HTTP/1.1\r\nHost: 127.0.0.1:\(Config.shared.remotePort)\r\n\r\n"
    var request = RemoteServer.Request(head: Data(head.utf8))!
    if let object {
        request.body = try! JSONSerialization.data(withJSONObject: object)
        request.contentLength = request.body.count
    }
    return request
}

private func reservationObject(_ request: VerificationRunLedger.ReservationRequest) -> [String: Any] {
    var subject: [String: Any] = ["kind": request.subject.kind]
    if let tree = request.subject.treeSHA { subject["tree_sha"] = tree }
    if let base = request.subject.baseCommit { subject["base_commit"] = base }
    if let overlay = request.subject.overlaySHA256 { subject["overlay_sha256"] = overlay }
    var row: [String: Any] = [
        "request_id": request.requestID, "repository_sha256": request.repositorySHA256,
        "subject": subject, "question_id": request.questionID,
        "verification_kind": request.verificationKind.rawValue,
        "variant": request.variant.rawValue, "command_sha256": request.commandSHA256,
        "environment_sha256": request.environmentSHA256,
    ]
    if let task = request.taskID { row["task_id"] = task }
    if let mutation = request.mutationID { row["mutation_id"] = mutation }
    if let baseline = request.baselineReceiptID { row["baseline_receipt_id"] = baseline }
    return row
}

func runVerificationRunLedgerTests() {
group("an exact verification tuple has one active producer") {
    let (url, directory) = verificationStore()
    defer { try? FileManager.default.removeItem(at: directory) }
    let ledgerA = VerificationRunLedger(storeURL: url)
    let ledgerB = VerificationRunLedger(storeURL: url)
    // Open/migrate both connections before the concurrent question so this test isolates the
    // reservation transaction rather than SQLite schema admission.
    _ = ledgerA.reading(receiptID: UUID().uuidString.lowercased())
    _ = ledgerB.reading(receiptID: UUID().uuidString.lowercased())
    let first = verificationRequest(requestID: "10000000-0000-4000-8000-000000000001")
    let second = verificationRequest(requestID: "10000000-0000-4000-8000-000000000002",
                                     taskID: first.taskID)
    let lock = NSLock(), done = DispatchGroup()
    var decisions: [VerificationRunLedger.ReserveDecision] = []
    for (ledger, request) in [(ledgerA, first), (ledgerB, second)] {
        done.enter()
        DispatchQueue.global().async {
            let decision = ledger.reserve(request)
            lock.lock(); decisions.append(decision); lock.unlock()
            done.leave()
        }
    }
    expect("both concurrent callers receive a typed answer", done.wait(timeout: .now() + 5), .success)
    expect("one caller owns the new run", decisions.filter {
        if case .runRequired(_, created: true) = $0 { return true }; return false
    }.count, 1)
    expect("and the other sees that exact tuple active", decisions.filter {
        if case .active = $0 { return true }; return false
    }.count, 1)
    expect("with one durable reservation row", Set(decisions.compactMap { reservedRun($0)?.receiptID }).count, 1)

    // The same winning request is an idempotent replay, not a second row. Which caller wins is
    // intentionally nondeterministic; the transaction is the subject, not dispatch order.
    let winner = decisions.compactMap { decision -> VerificationRunLedger.ReservationRequest? in
        if case .runRequired(let run, created: true) = decision { return run.request }
        return nil
    }.first!
    let replay = ledgerA.reserve(winner)
    check("replaying one request keeps its receipt identity",
          reservedRun(replay)?.receiptID == reservedRun(decisions.first { decision in
              reservedRun(decision)?.request.requestID == winner.requestID
          }!)?.receiptID)
}

group("completion is append-only, idempotent, and bound to its reserved tuple") {
    let (url, directory) = verificationStore()
    defer { try? FileManager.default.removeItem(at: directory) }
    let ledger = VerificationRunLedger(storeURL: url)
    let request = verificationRequest()
    let run = reservedRun(ledger.reserve(request))!
    var wrongTree = request
    wrongTree.subject = .commitTree(treeSHA: verificationTreeB)
    let outcome = verificationOutcome()
    if case .conflict(let code) = ledger.complete(receiptID: run.receiptID,
                                                   request: wrongTree, outcome: outcome) {
        expect("a stale or mismatched tree is refused by name", code, "receipt_tuple_mismatch")
    } else { check("a stale or mismatched tree is refused", false) }
    if case .reserved = ledger.reading(receiptID: run.receiptID) {
        check("a refused completion leaves the reservation active", true)
    } else { check("a refused completion leaves the reservation active", false) }

    if case .completed(_, created: true) = ledger.complete(receiptID: run.receiptID,
                                                            request: request, outcome: outcome) {
        check("the first matching outcome appends", true)
    } else { check("the first matching outcome appends", false) }
    if case .completed(_, created: false) = ledger.complete(receiptID: run.receiptID,
                                                             request: request, outcome: outcome) {
        check("the exact completion replay changes nothing", true)
    } else { check("the exact completion replay changes nothing", false) }
    let conflicting = verificationOutcome(state: .failed, completionID: outcome.completionID)
    if case .conflict(let code) = ledger.complete(receiptID: run.receiptID,
                                                   request: request, outcome: conflicting) {
        expect("one receipt cannot be rewritten red", code, "outcome_conflict")
    } else { check("one receipt cannot be rewritten red", false) }

    let restarted = VerificationRunLedger(storeURL: url)
    var nextTask = request
    nextTask.requestID = UUID().uuidString.lowercased()
    nextTask.taskID = UUID().uuidString.lowercased()
    if case .reusable(let reused) = restarted.reserve(nextTask) {
        expect("a restarted app reuses the same exact commit-tree proof", reused.receiptID, run.receiptID)
    } else { check("a restarted app reuses the same exact commit-tree proof", false) }
}

group("focused, overlay, failed, and inconclusive evidence cannot forge reusable full proof") {
    let (url, directory) = verificationStore()
    defer { try? FileManager.default.removeItem(at: directory) }
    let ledger = VerificationRunLedger(storeURL: url)
    let focused = verificationRequest(kind: .focused)
    let focusedRun = reservedRun(ledger.reserve(focused))!
    let forgedFullOutcome = verificationOutcome(fullSeal: String(repeating: "1", count: 64))
    if case .malformed(let code) = ledger.complete(receiptID: focusedRun.receiptID,
                                                    request: focused, outcome: forgedFullOutcome) {
        expect("focused-as-full completion is malformed", code, "malformed_outcome")
    } else { check("focused-as-full completion is malformed", false) }
    var fullForgery = focused
    fullForgery.verificationKind = .full
    if case .conflict(let code) = ledger.complete(receiptID: focusedRun.receiptID,
                                                   request: fullForgery, outcome: forgedFullOutcome) {
        expect("changing the reserved kind is a tuple mismatch", code, "receipt_tuple_mismatch")
    } else { check("changing the reserved kind is a tuple mismatch", false) }

    let overlaySubject = VerificationRunLedger.Subject.workingOverlay(
        baseCommit: verificationTreeA, overlaySHA256: String(repeating: "2", count: 64))
    let ownerlessOverlay = verificationRequest(taskID: nil, subject: overlaySubject)
    if case .malformed(let code) = ledger.reserve(ownerlessOverlay) {
        expect("a working overlay without task provenance is refused", code, "malformed_receipt")
    } else { check("a working overlay without task provenance is refused", false) }
    let overlay = verificationRequest(subject: overlaySubject)
    var simultaneousOtherTask = overlay
    simultaneousOtherTask.requestID = UUID().uuidString.lowercased()
    simultaneousOtherTask.taskID = UUID().uuidString.lowercased()
    let overlayRun = reservedRun(ledger.reserve(overlay))!
    if case .runRequired(_, created: true) = ledger.reserve(simultaneousOtherTask) {
        check("an active overlay producer does not own another task's bytes", true)
    } else { check("an active overlay producer does not own another task's bytes", false) }
    _ = ledger.complete(receiptID: overlayRun.receiptID, request: overlay,
                        outcome: verificationOutcome())
    var otherTask = overlay
    otherTask.requestID = UUID().uuidString.lowercased()
    otherTask.taskID = UUID().uuidString.lowercased()
    if case .runRequired = ledger.reserve(otherTask) {
        check("a green overlay cannot cross a task boundary", true)
    } else { check("a green overlay cannot cross a task boundary", false) }

    let red = verificationRequest(subject: .commitTree(treeSHA: verificationTreeB))
    let redRun = reservedRun(ledger.reserve(red))!
    _ = ledger.complete(receiptID: redRun.receiptID, request: red,
                        outcome: verificationOutcome(state: .failed))
    var retry = red
    retry.requestID = UUID().uuidString.lowercased()
    if case .runRequired = ledger.reserve(retry) { check("failed evidence requires another run", true) }
    else { check("failed evidence requires another run", false) }

    var uncertain = verificationRequest()
    uncertain.questionID = "verification.environment-gap"
    let uncertainRun = reservedRun(ledger.reserve(uncertain))!
    _ = ledger.complete(receiptID: uncertainRun.receiptID, request: uncertain,
                        outcome: verificationOutcome(state: .inconclusiveEnvironment,
                                                     inconclusiveCode: "sandbox_capability"))
    if case .inconclusiveEnvironment = ledger.reading(receiptID: uncertainRun.receiptID) {
        check("environmental uncertainty is its own stored state", true)
    } else { check("environmental uncertainty is its own stored state", false) }
    uncertain.requestID = UUID().uuidString.lowercased()
    if case .runRequired = ledger.reserve(uncertain) {
        check("typed inconclusive evidence authorizes a new attempt, not reuse", true)
    } else { check("typed inconclusive evidence authorizes a new attempt, not reuse", false) }
}

group("a mutation receipt cannot exist without its exact passing baseline") {
    let (url, directory) = verificationStore()
    defer { try? FileManager.default.removeItem(at: directory) }
    let ledger = VerificationRunLedger(storeURL: url)
    let missingBaseline = verificationRequest(kind: .mutation, variant: .mutation,
        mutationID: "remove-reservation-transaction", baselineReceiptID: UUID().uuidString.lowercased())
    if case .conflict(let code) = ledger.reserve(missingBaseline) {
        expect("an absent mutation baseline fails closed", code, "mutation_baseline_unusable")
    } else { check("an absent mutation baseline fails closed", false) }

    let baseline = verificationRequest()
    let baselineRun = reservedRun(ledger.reserve(baseline))!
    _ = ledger.complete(receiptID: baselineRun.receiptID, request: baseline,
                        outcome: verificationOutcome())
    var wrongSubject = verificationRequest(subject: .commitTree(treeSHA: verificationTreeB),
        kind: .mutation, variant: .mutation,
        mutationID: "remove-reservation-transaction", baselineReceiptID: baselineRun.receiptID)
    wrongSubject.taskID = baseline.taskID
    if case .conflict(let code) = ledger.reserve(wrongSubject) {
        expect("a passing baseline for another exact subject is unusable", code,
               "mutation_baseline_unusable")
    } else { check("a passing baseline for another exact subject is unusable", false) }

    let overlaySubject = VerificationRunLedger.Subject.workingOverlay(
        baseCommit: verificationTreeA, overlaySHA256: String(repeating: "3", count: 64))
    let overlayBaseline = verificationRequest(subject: overlaySubject)
    let overlayBaselineRun = reservedRun(ledger.reserve(overlayBaseline))!
    _ = ledger.complete(receiptID: overlayBaselineRun.receiptID, request: overlayBaseline,
                        outcome: verificationOutcome())
    var foreignOverlayMutation = verificationRequest(subject: overlaySubject,
        kind: .mutation, variant: .mutation, mutationID: "remove-overlay-guard",
        baselineReceiptID: overlayBaselineRun.receiptID)
    foreignOverlayMutation.taskID = UUID().uuidString.lowercased()
    if case .conflict(let code) = ledger.reserve(foreignOverlayMutation) {
        expect("an overlay baseline cannot authorize another task", code,
               "mutation_baseline_unusable")
    } else { check("an overlay baseline cannot authorize another task", false) }

    var mutation = verificationRequest(kind: .mutation, variant: .mutation,
        mutationID: "remove-reservation-transaction", baselineReceiptID: baselineRun.receiptID)
    mutation.taskID = baseline.taskID
    if case .runRequired(let run, _) = ledger.reserve(mutation) {
        expect("the mutation stores the baseline it is about",
               run.request.baselineReceiptID, baselineRun.receiptID)
    } else { check("a mutation with a passing baseline may run", false) }
}

group("malformed stored evidence remains visible instead of becoming absent") {
    let (url, directory) = verificationStore()
    defer { try? FileManager.default.removeItem(at: directory) }
    let ledger = VerificationRunLedger(storeURL: url)
    let request = verificationRequest()
    let run = reservedRun(ledger.reserve(request))!
    var db: OpaquePointer?
    expect("the failure injector opens the private store", sqlite3_open(url.path, &db), SQLITE_OK)
    let sql = """
        INSERT INTO verification_run_outcomes
          (completion_id,receipt_id,request_id,state,exit_status,checks_passed,checks_failed,
           duration_ms,log_sha256,completed_at)
        VALUES ('30000000-0000-4000-8000-000000000001','\(run.receiptID)',
                '\(request.requestID)','green',0,17,0,1,'\(verificationLog)',1);
        """
    expect("the malformed outcome is injected", sqlite3_exec(db, sql, nil, nil, nil), SQLITE_OK)
    sqlite3_close(db)
    if case .malformed = ledger.reading(receiptID: run.receiptID) {
        check("an invalid stored state is malformed", true)
    } else { check("an invalid stored state is malformed", false) }
    let absent = UUID().uuidString.lowercased()
    if case .absent = ledger.reading(receiptID: absent) { check("a missing id remains absent", true) }
    else { check("a missing id remains absent", false) }
    var next = request
    next.requestID = UUID().uuidString.lowercased()
    if case .malformed(let code) = ledger.reserve(next) {
        expect("malformed matching evidence blocks a duplicate run", code, "stored_receipt_malformed")
    } else { check("malformed matching evidence blocks a duplicate run", false) }
}

group("stored tuple corruption cannot hide a producer behind its derived index") {
    let (url, directory) = verificationStore()
    defer { try? FileManager.default.removeItem(at: directory) }
    let ledger = VerificationRunLedger(storeURL: url)
    let request = verificationRequest()
    let run = reservedRun(ledger.reserve(request))!
    var db: OpaquePointer?
    expect("the tuple failure injector opens the private store", sqlite3_open(url.path, &db), SQLITE_OK)
    let corrupt = "UPDATE verification_run_reservations SET tuple_sha256 = '"
        + String(repeating: "0", count: 64) + "' WHERE receipt_id = '\(run.receiptID)';"
    expect("the stored tuple index is corrupted", sqlite3_exec(db, corrupt, nil, nil, nil), SQLITE_OK)
    sqlite3_close(db)
    if case .malformed = ledger.reading(receiptID: run.receiptID) {
        check("the corrupted stored tuple remains visible as malformed", true)
    } else { check("the corrupted stored tuple remains visible as malformed", false) }
    var duplicate = request
    duplicate.requestID = UUID().uuidString.lowercased()
    if case .malformed(let code) = ledger.reserve(duplicate) {
        expect("semantic lookup finds the corrupted producer and blocks a duplicate", code,
               "stored_receipt_malformed")
    } else { check("semantic lookup finds the corrupted producer and blocks a duplicate", false) }
}

group("SQLite step failures are unavailable rather than absent or partial") {
    let (url, directory) = verificationStore()
    defer { try? FileManager.default.removeItem(at: directory) }
    let healthy = VerificationRunLedger(storeURL: url)
    let request = verificationRequest()
    let run = reservedRun(healthy.reserve(request))!
    let broken = VerificationRunLedger(storeURL: url, selectStep: { _ in SQLITE_BUSY })
    if case .unavailable = broken.reading(receiptID: run.receiptID) {
        check("a failed receipt read is unavailable, not absent", true)
    } else { check("a failed receipt read is unavailable, not absent", false) }
    var duplicate = request
    duplicate.requestID = UUID().uuidString.lowercased()
    if case .unavailable = broken.reserve(duplicate) {
        check("a failed producer lookup cannot reserve a duplicate", true)
    } else { check("a failed producer lookup cannot reserve a duplicate", false) }
    if case .unavailable = broken.complete(receiptID: run.receiptID, request: request,
                                            outcome: verificationOutcome()) {
        check("a failed completion lookup is unavailable, not not-found", true)
    } else { check("a failed completion lookup is unavailable, not not-found", false) }
}

group("verification run HTTP is a closed machine-only protocol") {
    let (url, directory) = verificationStore()
    defer { try? FileManager.default.removeItem(at: directory) }
    let ledger = VerificationRunLedger(storeURL: url)
    let request = verificationRequest()
    let http = verificationHTTPRequest(method: "POST", path: VerificationRunLedgerHTTP.reservePath,
                                       object: reservationObject(request))
    let denied = VerificationRunLedgerHTTP.route(http, machine: false, ledger: ledger)!
    expect("a paired or anonymous caller cannot reserve", denied.status, 403)
    let accepted = VerificationRunLedgerHTTP.route(http, machine: true, ledger: ledger)!
    expect("the machine reservation is created", accepted.status, 201)
    expect("and answers with the execution decision",
           responseObject(accepted)?["decision"] as? String, "run_required")

    var malformed = reservationObject(request)
    malformed["unknown_field"] = true
    let refused = VerificationRunLedgerHTTP.route(
        verificationHTTPRequest(method: "POST", path: VerificationRunLedgerHTTP.reservePath,
                                object: malformed), machine: true, ledger: ledger)!
    expect("an unknown field is refused by the closed request", refused.status, 400)
    var wrongOptionalType = reservationObject(request)
    wrongOptionalType["task_id"] = 7
    let wrongType = VerificationRunLedgerHTTP.route(
        verificationHTTPRequest(method: "POST", path: VerificationRunLedgerHTTP.reservePath,
                                object: wrongOptionalType), machine: true, ledger: ledger)!
    expect("an optional field cannot become absent by carrying the wrong type", wrongType.status, 400)
    let malformedID = VerificationRunLedgerHTTP.route(
        verificationHTTPRequest(method: "GET", path: VerificationRunLedgerHTTP.prefix + "not-a-uuid"),
        machine: true, ledger: ledger)!
    expect("a malformed receipt id is not rendered as an absent receipt", malformedID.status, 400)
    let missing = VerificationRunLedgerHTTP.route(
        verificationHTTPRequest(method: "GET",
                                path: VerificationRunLedgerHTTP.prefix + UUID().uuidString.lowercased()),
        machine: true, ledger: ledger)!
    expect("an absent receipt is represented, not turned into malformed",
           (responseObject(missing)?["verificationRun"] as? [String: Any])?["state"] as? String,
           "absent")

    let server = RemoteServer.shared
    let anonymous = server.route(verificationHTTPRequest(method: "POST",
        path: VerificationRunLedgerHTTP.reservePath, object: [:]))
    expect("the real dispatch gate hides the machine route from anonymous callers",
           anonymous.status, 401)
    var taskSecret = verificationHTTPRequest(method: "POST",
        path: VerificationRunLedgerHTTP.reservePath, object: [:])
    taskSecret.headers["x-clawdline-task-secret"] = String(repeating: "ab", count: 32)
    expect("the real dispatch gate does not treat a task secret as machine authority",
           server.route(taskSecret).status, 401)
    let pairing = RemoteAuth.beginPairing(name: "verification-ledger-test")
    if case .paired(let token) = RemoteAuth.confirmPairing(id: pairing.id, code: pairing.code) {
        var paired = verificationHTTPRequest(method: "POST",
            path: VerificationRunLedgerHTTP.reservePath, object: [:])
        paired.headers["authorization"] = "Bearer \(token)"
        expect("a real paired device reaches the ledger's explicit forbidden response",
               server.route(paired).status, 403)
        RemoteAuth.revoke(id: pairing.id)
    } else { check("the paired-device integration fixture is created", false) }
    var machine = verificationHTTPRequest(method: "POST",
        path: VerificationRunLedgerHTTP.reservePath, object: [:])
    machine.headers["x-clawdline-orchestrator"] = Orchestrator.dispatchToken()
    expect("the real machine route reaches closed-body validation", server.route(machine).status, 400)
}
}
