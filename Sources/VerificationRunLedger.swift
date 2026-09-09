import CryptoKit
import Foundation
import SQLite3

/// Durable, append-only evidence for one exact verification invocation.
///
/// A reservation and its outcome are separate rows.  Reserving never overwrites an earlier run,
/// and completing appends exactly one outcome event.  `BEGIN IMMEDIATE` makes the question "does
/// this exact tuple already have a producer?" and the insertion of its producer one transaction,
/// including when two app/Sessions use separate SQLite connections.
final class VerificationRunLedger {
    static let shared = VerificationRunLedger()
    static let schemaVersion = 1
    static let defaultReservationSeconds: TimeInterval = 3_600

    struct Failure: Error, Equatable { var code: String }

    enum VerificationKind: String, CaseIterable {
        case `static`, typecheck, focused, mutation, full, build, smoke
    }

    enum Variant: String, CaseIterable { case baseline, mutation }

    enum Subject: Equatable {
        case commitTree(treeSHA: String)
        case workingOverlay(baseCommit: String, overlaySHA256: String)

        var kind: String {
            switch self {
            case .commitTree: return "commit_tree"
            case .workingOverlay: return "working_overlay"
            }
        }

        var treeSHA: String? {
            if case .commitTree(let value) = self { return value }
            return nil
        }

        var baseCommit: String? {
            if case .workingOverlay(let value, _) = self { return value }
            return nil
        }

        var overlaySHA256: String? {
            if case .workingOverlay(_, let value) = self { return value }
            return nil
        }
    }

    struct ReservationRequest: Equatable {
        var requestID: String
        var repositorySHA256: String
        var taskID: String?
        var subject: Subject
        var questionID: String
        var verificationKind: VerificationKind
        var variant: Variant
        var mutationID: String?
        var baselineReceiptID: String?
        var commandSHA256: String
        var environmentSHA256: String
    }

    enum OutcomeState: String, CaseIterable { case passed, failed, inconclusiveEnvironment = "inconclusive_environment" }

    struct Outcome: Equatable {
        var completionID: String
        var state: OutcomeState
        var exitStatus: Int
        var checksPassed: Int
        var checksFailed: Int
        var durationMS: Int
        var logSHA256: String
        var inconclusiveCode: String?
        /// Required for a passing full run. It binds the run to the complete Swift/Cloud/witness
        /// tuple emitted by `test.sh`, rather than to one hand-copied count.
        var fullSuiteReceiptSHA256: String?
    }

    struct StoredRun: Equatable {
        var receiptID: String
        var request: ReservationRequest
        var reservedAt: Date
        var expiresAt: Date
        var outcome: Outcome?
        var completedAt: Date?
    }

    enum ReserveDecision: Equatable {
        case reusable(StoredRun)
        case runRequired(StoredRun, created: Bool)
        case active(StoredRun)
        case conflict(code: String)
        case malformed(code: String)
        case unavailable
    }

    enum CompletionDecision: Equatable {
        case completed(StoredRun, created: Bool)
        case conflict(code: String)
        case malformed(code: String)
        case absent
        case unavailable
    }

    enum Reading: Equatable {
        case reserved(StoredRun)
        case passed(StoredRun)
        case failed(StoredRun)
        case inconclusiveEnvironment(StoredRun)
        case malformed
        case absent
        case unavailable
    }

    private let queue: DispatchQueue
    private let storeURL: URL
    private let now: () -> Date
    private let makeID: () -> String
    private let reservationSeconds: TimeInterval
    private let selectStep: (OpaquePointer?) -> Int32
    private var handle: OpaquePointer?

    init(storeURL: URL = UsageLedger.storeURL,
         now: @escaping () -> Date = Date.init,
         makeID: @escaping () -> String = { UUID().uuidString.lowercased() },
         reservationSeconds: TimeInterval = VerificationRunLedger.defaultReservationSeconds,
         selectStep: @escaping (OpaquePointer?) -> Int32 = sqlite3_step) {
        self.storeURL = storeURL
        self.now = now
        self.makeID = makeID
        self.reservationSeconds = max(60, reservationSeconds)
        self.selectStep = selectStep
        queue = DispatchQueue(label: "com.tsunamiworks.clawdline.verification-run-ledger.\(UUID().uuidString)")
    }

    deinit { if let handle { sqlite3_close(handle) } }

    // MARK: Schema

    @discardableResult
    static func createSchema(in db: OpaquePointer) -> Bool {
        let sql = """
            CREATE TABLE IF NOT EXISTS verification_run_reservations (
              receipt_id TEXT PRIMARY KEY,
              request_id TEXT NOT NULL UNIQUE,
              tuple_sha256 TEXT NOT NULL,
              repository_sha256 TEXT NOT NULL,
              task_id TEXT,
              subject_kind TEXT NOT NULL,
              tree_sha TEXT,
              base_commit TEXT,
              overlay_sha256 TEXT,
              question_id TEXT NOT NULL,
              verification_kind TEXT NOT NULL,
              variant TEXT NOT NULL,
              mutation_id TEXT,
              baseline_receipt_id TEXT,
              command_sha256 TEXT NOT NULL,
              environment_sha256 TEXT NOT NULL,
              reserved_at REAL NOT NULL,
              expires_at REAL NOT NULL
            );
            CREATE INDEX IF NOT EXISTS verification_run_reservations_tuple
              ON verification_run_reservations (tuple_sha256, reserved_at);
            CREATE TABLE IF NOT EXISTS verification_run_outcomes (
              completion_id TEXT PRIMARY KEY,
              receipt_id TEXT NOT NULL UNIQUE,
              request_id TEXT NOT NULL,
              state TEXT NOT NULL,
              exit_status INTEGER NOT NULL,
              checks_passed INTEGER NOT NULL,
              checks_failed INTEGER NOT NULL,
              duration_ms INTEGER NOT NULL,
              log_sha256 TEXT NOT NULL,
              inconclusive_code TEXT,
              full_suite_receipt_sha256 TEXT,
              completed_at REAL NOT NULL,
              FOREIGN KEY(receipt_id) REFERENCES verification_run_reservations(receipt_id)
            );
            CREATE INDEX IF NOT EXISTS verification_run_outcomes_receipt
              ON verification_run_outcomes (receipt_id, completed_at);
            """
        var error: UnsafeMutablePointer<CChar>?
        let ok = sqlite3_exec(db, sql, nil, nil, &error) == SQLITE_OK
        if let error { Log.write("verification run ledger schema failed: \(String(cString: error))"); sqlite3_free(error) }
        return ok
    }

    // MARK: Validation and identity

    static func parseReservation(_ raw: Any?) -> Result<ReservationRequest, Failure> {
        guard let row = raw as? [String: Any] else { return .failure(Failure(code: "malformed_receipt")) }
        let allowed = Set(["request_id", "repository_sha256", "task_id", "subject", "question_id",
                           "verification_kind", "variant", "mutation_id", "baseline_receipt_id",
                           "command_sha256", "environment_sha256"])
        guard Set(row.keys).isSubset(of: allowed),
              optionalStringField(row, "task_id"),
              optionalStringField(row, "mutation_id"),
              optionalStringField(row, "baseline_receipt_id"),
              let requestID = row["request_id"] as? String,
              let repository = row["repository_sha256"] as? String,
              let question = row["question_id"] as? String,
              let kindRaw = row["verification_kind"] as? String,
              let kind = VerificationKind(rawValue: kindRaw),
              let variantRaw = row["variant"] as? String,
              let variant = Variant(rawValue: variantRaw),
              let command = row["command_sha256"] as? String,
              let environment = row["environment_sha256"] as? String,
              let subject = parseSubject(row["subject"])
        else { return .failure(Failure(code: "malformed_receipt")) }
        let request = ReservationRequest(
            requestID: requestID, repositorySHA256: repository,
            taskID: optionalString(row["task_id"]), subject: subject, questionID: question,
            verificationKind: kind, variant: variant,
            mutationID: optionalString(row["mutation_id"]),
            baselineReceiptID: optionalString(row["baseline_receipt_id"]),
            commandSHA256: command, environmentSHA256: environment)
        guard valid(request) else { return .failure(Failure(code: "malformed_receipt")) }
        return .success(request)
    }

    static func parseOutcome(_ raw: Any?) -> Result<Outcome, Failure> {
        guard let row = raw as? [String: Any] else { return .failure(Failure(code: "malformed_outcome")) }
        let allowed = Set(["completion_id", "state", "exit_status", "checks_passed",
                           "checks_failed", "duration_ms", "log_sha256", "inconclusive_code",
                           "full_suite_receipt_sha256"])
        guard Set(row.keys).isSubset(of: allowed),
              optionalStringField(row, "inconclusive_code"),
              optionalStringField(row, "full_suite_receipt_sha256"),
              let completionID = row["completion_id"] as? String,
              let stateRaw = row["state"] as? String,
              let state = OutcomeState(rawValue: stateRaw),
              let exit = integer(row["exit_status"]), let passed = integer(row["checks_passed"]),
              let failed = integer(row["checks_failed"]), let duration = integer(row["duration_ms"]),
              let log = row["log_sha256"] as? String
        else { return .failure(Failure(code: "malformed_outcome")) }
        let outcome = Outcome(completionID: completionID, state: state, exitStatus: exit,
                              checksPassed: passed, checksFailed: failed, durationMS: duration,
                              logSHA256: log,
                              inconclusiveCode: optionalString(row["inconclusive_code"]),
                              fullSuiteReceiptSHA256: optionalString(row["full_suite_receipt_sha256"]))
        return valid(outcome) ? .success(outcome) : .failure(Failure(code: "malformed_outcome"))
    }

    private static func parseSubject(_ raw: Any?) -> Subject? {
        guard let row = raw as? [String: Any], let kind = row["kind"] as? String else { return nil }
        switch kind {
        case "commit_tree":
            guard Set(row.keys) == Set(["kind", "tree_sha"]),
                  let tree = row["tree_sha"] as? String, gitObject(tree) else { return nil }
            return .commitTree(treeSHA: tree)
        case "working_overlay":
            guard Set(row.keys) == Set(["kind", "base_commit", "overlay_sha256"]),
                  let base = row["base_commit"] as? String, gitObject(base),
                  let digest = row["overlay_sha256"] as? String, sha256(digest) else { return nil }
            return .workingOverlay(baseCommit: base, overlaySHA256: digest)
        default: return nil
        }
    }

    private static func valid(_ request: ReservationRequest) -> Bool {
        guard uuid(request.requestID), sha256(request.repositorySHA256),
              token(request.questionID, maximum: 200), sha256(request.commandSHA256),
              sha256(request.environmentSHA256), request.taskID.map(uuid) ?? true else { return false }
        switch request.subject {
        case .commitTree(let tree): guard gitObject(tree) else { return false }
        case .workingOverlay(let base, let digest):
            guard gitObject(base), sha256(digest), request.taskID != nil else { return false }
        }
        switch request.variant {
        case .baseline:
            guard request.mutationID == nil, request.baselineReceiptID == nil,
                  request.verificationKind != .mutation else { return false }
        case .mutation:
            guard request.verificationKind == .mutation,
                  request.mutationID.map({ token($0, maximum: 200) }) == true,
                  request.baselineReceiptID.map(uuid) == true else { return false }
        }
        if request.verificationKind == .full {
            guard request.variant == .baseline,
                  case .commitTree = request.subject else { return false }
        }
        return true
    }

    private static func valid(_ outcome: Outcome) -> Bool {
        guard uuid(outcome.completionID), (0...255).contains(outcome.exitStatus),
              outcome.checksPassed >= 0, outcome.checksFailed >= 0, outcome.durationMS >= 0,
              sha256(outcome.logSHA256),
              outcome.fullSuiteReceiptSHA256.map(sha256) ?? true else { return false }
        switch outcome.state {
        case .passed:
            return outcome.exitStatus == 0 && outcome.checksFailed == 0
                && outcome.inconclusiveCode == nil
        case .failed:
            return (outcome.exitStatus != 0 || outcome.checksFailed > 0)
                && outcome.inconclusiveCode == nil && outcome.fullSuiteReceiptSHA256 == nil
        case .inconclusiveEnvironment:
            return outcome.inconclusiveCode.map { token($0, maximum: 120) } == true
                && outcome.fullSuiteReceiptSHA256 == nil
        }
    }

    private static func valid(_ outcome: Outcome, for request: ReservationRequest) -> Bool {
        guard valid(outcome) else { return false }
        if request.verificationKind == .full && outcome.state == .passed {
            return outcome.fullSuiteReceiptSHA256 != nil && outcome.checksPassed > 0
        }
        return outcome.fullSuiteReceiptSHA256 == nil
    }

    private static func optionalString(_ raw: Any?) -> String? {
        if raw == nil || raw is NSNull { return nil }
        return raw as? String
    }

    private static func optionalStringField(_ row: [String: Any], _ key: String) -> Bool {
        guard let value = row[key] else { return true }
        return value is NSNull || value is String
    }

    private static func integer(_ raw: Any?) -> Int? {
        guard let number = raw as? NSNumber,
              CFGetTypeID(number) != CFBooleanGetTypeID() else { return nil }
        let value = number.doubleValue
        guard value.rounded() == value, value >= Double(Int.min), value <= Double(Int.max) else { return nil }
        return Int(value)
    }

    private static func uuid(_ value: String) -> Bool {
        value == value.lowercased() && value.count == 36 && UUID(uuidString: value) != nil
    }

    private static func sha256(_ value: String) -> Bool {
        value.count == 64 && value == value.lowercased()
            && value.unicodeScalars.allSatisfy { (48...57).contains($0.value) || (97...102).contains($0.value) }
    }

    private static func gitObject(_ value: String) -> Bool {
        (value.count == 40 || value.count == 64) && value == value.lowercased()
            && value.unicodeScalars.allSatisfy { (48...57).contains($0.value) || (97...102).contains($0.value) }
    }

    private static func token(_ value: String, maximum: Int) -> Bool {
        !value.isEmpty && value.count <= maximum
            && value.unicodeScalars.allSatisfy {
                (48...57).contains($0.value) || (65...90).contains($0.value)
                    || (97...122).contains($0.value) || "._:-/".unicodeScalars.contains($0)
            }
    }

    static func tupleSHA256(_ request: ReservationRequest) -> String {
        // Commit-tree evidence may be reused across tasks; dirty-overlay evidence is meaningful
        // only inside the task that produced those bytes. Keep that distinction in the producer
        // identity itself so two tasks cannot even share an active overlay reservation.
        let overlayTaskID: String
        if case .workingOverlay = request.subject { overlayTaskID = request.taskID ?? "" }
        else { overlayTaskID = "" }
        let fields = [String(schemaVersion), request.repositorySHA256, request.subject.kind,
                      request.subject.treeSHA ?? "", request.subject.baseCommit ?? "",
                      request.subject.overlaySHA256 ?? "", request.questionID,
                      request.verificationKind.rawValue, request.variant.rawValue,
                      request.mutationID ?? "", request.baselineReceiptID ?? "",
                      request.commandSHA256, request.environmentSHA256, overlayTaskID]
        return SHA256.hash(data: Data(fields.joined(separator: "\u{1f}").utf8))
            .map { String(format: "%02x", $0) }.joined()
    }

    // MARK: Atomic reserve / complete / read

    func reserve(_ request: ReservationRequest) -> ReserveDecision {
        guard Self.valid(request) else { return .malformed(code: "malformed_receipt") }
        return queue.sync {
            guard let db = database(), begin(db) else { return .unavailable }
            var committed = false
            defer { if !committed { rollback(db) } }

            switch run(db, requestID: request.requestID) {
            case .failure: return .unavailable
            case .success(.some(.failure)):
                return .malformed(code: "stored_receipt_malformed")
            case .success(.some(.success(let stored))):
                guard stored.request == request else { return .conflict(code: "request_identity_conflict") }
                if stored.outcome == nil, stored.expiresAt <= now() {
                    guard expire(db, stored) else { return .unavailable }
                    guard commit(db) else { return .unavailable }; committed = true
                    return .conflict(code: "request_reservation_expired")
                }
                guard commit(db) else { return .unavailable }; committed = true
                if stored.outcome == nil { return .runRequired(stored, created: false) }
                if reusable(stored, for: request.taskID) { return .reusable(stored) }
                return .conflict(code: "request_already_completed")
            case .success(nil): break
            }

            if request.variant == .mutation {
                guard let baselineID = request.baselineReceiptID else {
                    return .conflict(code: "mutation_baseline_unusable")
                }
                switch run(db, receiptID: baselineID) {
                case .failure: return .unavailable
                case .success(.some(.failure)):
                    return .malformed(code: "stored_receipt_malformed")
                case .success(.some(.success(let baseline))):
                    guard
                      baseline.outcome?.state == .passed,
                      baseline.request.variant == .baseline,
                      baseline.request.repositorySHA256 == request.repositorySHA256,
                      baseline.request.questionID == request.questionID,
                      baseline.request.environmentSHA256 == request.environmentSHA256,
                      baseline.request.subject == request.subject,
                      Self.mutationTaskScopeMatches(baseline.request, request)
                    else { return .conflict(code: "mutation_baseline_unusable") }
                case .success(nil): return .conflict(code: "mutation_baseline_unusable")
                }
            }

            let tuple = Self.tupleSHA256(request)
            let candidates: [Result<StoredRun, Failure>]
            switch runs(db, matching: request) {
            case .failure: return .unavailable
            case .success(let rows): candidates = rows
            }
            if candidates.contains(where: { if case .failure = $0 { return true }; return false }) {
                return .malformed(code: "stored_receipt_malformed")
            }
            for case .success(let stored) in candidates {
                if stored.outcome == nil {
                    if stored.expiresAt > now() {
                        guard commit(db) else { return .unavailable }; committed = true
                        return .active(stored)
                    }
                    guard expire(db, stored) else { return .unavailable }
                } else if reusable(stored, for: request.taskID) {
                    guard commit(db) else { return .unavailable }; committed = true
                    return .reusable(stored)
                }
            }

            let reservedAt = now(), receiptID = makeID()
            guard Self.uuid(receiptID) else { return .unavailable }
            let stored = StoredRun(receiptID: receiptID, request: request,
                                   reservedAt: reservedAt,
                                   expiresAt: reservedAt.addingTimeInterval(reservationSeconds),
                                   outcome: nil, completedAt: nil)
            guard insert(db, stored, tupleSHA256: tuple) else { return .unavailable }
            guard commit(db) else { return .unavailable }; committed = true
            return .runRequired(stored, created: true)
        }
    }

    func complete(receiptID: String, request: ReservationRequest,
                  outcome: Outcome) -> CompletionDecision {
        guard Self.uuid(receiptID), Self.valid(request), Self.valid(outcome, for: request)
        else { return .malformed(code: "malformed_outcome") }
        return queue.sync {
            guard let db = database(), begin(db) else { return .unavailable }
            var committed = false
            defer { if !committed { rollback(db) } }
            let result: Result<StoredRun, Failure>
            switch run(db, receiptID: receiptID) {
            case .failure: return .unavailable
            case .success(nil):
                guard commit(db) else { return .unavailable }; committed = true; return .absent
            case .success(.some(let found)): result = found
            }
            guard case .success(var stored) = result else { return .malformed(code: "stored_receipt_malformed") }
            guard stored.request == request else { return .conflict(code: "receipt_tuple_mismatch") }
            if let existing = stored.outcome {
                guard existing == outcome else { return .conflict(code: "outcome_conflict") }
                guard commit(db) else { return .unavailable }; committed = true
                return .completed(stored, created: false)
            }
            guard insert(db, receiptID: receiptID, requestID: request.requestID,
                         outcome: outcome, completedAt: now()) else { return .unavailable }
            guard case .success(.some(.success(let finished))) = run(db, receiptID: receiptID)
            else { return .unavailable }
            stored = finished
            guard commit(db) else { return .unavailable }; committed = true
            return .completed(stored, created: true)
        }
    }

    func reading(receiptID: String) -> Reading {
        guard Self.uuid(receiptID) else { return .absent }
        return queue.sync {
            guard let db = database() else { return .unavailable }
            let result: Result<StoredRun, Failure>
            switch run(db, receiptID: receiptID) {
            case .failure: return .unavailable
            case .success(nil): return .absent
            case .success(.some(let found)): result = found
            }
            guard case .success(let stored) = result else { return .malformed }
            switch stored.outcome?.state {
            case nil: return .reserved(stored)
            case .passed: return .passed(stored)
            case .failed: return .failed(stored)
            case .inconclusiveEnvironment: return .inconclusiveEnvironment(stored)
            }
        }
    }

    private func reusable(_ run: StoredRun, for taskID: String?) -> Bool {
        guard run.outcome?.state == .passed else { return false }
        switch run.request.subject {
        case .commitTree: return true
        case .workingOverlay:
            return taskID != nil && run.request.taskID == taskID
        }
    }

    private static func mutationTaskScopeMatches(_ baseline: ReservationRequest,
                                                 _ mutation: ReservationRequest) -> Bool {
        switch mutation.subject {
        case .commitTree: return true
        case .workingOverlay:
            return mutation.taskID != nil && baseline.taskID == mutation.taskID
        }
    }

    // MARK: SQLite

    private static let transient = unsafeBitCast(-1, to: sqlite3_destructor_type.self)

    private func database() -> OpaquePointer? {
        if let handle { return handle }
        try? FileManager.default.createDirectory(at: storeURL.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true,
                                                 attributes: [.posixPermissions: 0o700])
        var db: OpaquePointer?
        guard sqlite3_open_v2(storeURL.path, &db,
                              SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE | SQLITE_OPEN_FULLMUTEX,
                              nil) == SQLITE_OK, let db else { if let db { sqlite3_close(db) }; return nil }
        sqlite3_busy_timeout(db, 5_000)
        _ = execute(db, "PRAGMA journal_mode=WAL;")
        _ = execute(db, "PRAGMA synchronous=NORMAL;")
        _ = execute(db, "PRAGMA foreign_keys=ON;")
        guard Self.createSchema(in: db) else { sqlite3_close(db); return nil }
        try? FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: storeURL.path)
        handle = db
        return db
    }

    private func begin(_ db: OpaquePointer) -> Bool { execute(db, "BEGIN IMMEDIATE;") }
    private func commit(_ db: OpaquePointer) -> Bool { execute(db, "COMMIT;") }
    private func rollback(_ db: OpaquePointer) { _ = execute(db, "ROLLBACK;") }

    @discardableResult
    private func execute(_ db: OpaquePointer, _ sql: String) -> Bool {
        sqlite3_exec(db, sql, nil, nil, nil) == SQLITE_OK
    }

    private func bind(_ statement: OpaquePointer?, _ index: Int32, _ value: String?) {
        if let value { sqlite3_bind_text(statement, index, value, -1, Self.transient) }
        else { sqlite3_bind_null(statement, index) }
    }

    private func insert(_ db: OpaquePointer, _ run: StoredRun, tupleSHA256: String) -> Bool {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            INSERT INTO verification_run_reservations
              (receipt_id,request_id,tuple_sha256,repository_sha256,task_id,subject_kind,
               tree_sha,base_commit,overlay_sha256,question_id,verification_kind,variant,
               mutation_id,baseline_receipt_id,command_sha256,environment_sha256,reserved_at,expires_at)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?);
            """, -1, &statement, nil) == SQLITE_OK else { return false }
        let request = run.request
        let values: [String?] = [run.receiptID, request.requestID, tupleSHA256,
                                 request.repositorySHA256, request.taskID, request.subject.kind,
                                 request.subject.treeSHA, request.subject.baseCommit,
                                 request.subject.overlaySHA256, request.questionID,
                                 request.verificationKind.rawValue, request.variant.rawValue,
                                 request.mutationID, request.baselineReceiptID,
                                 request.commandSHA256, request.environmentSHA256]
        for (offset, value) in values.enumerated() { bind(statement, Int32(offset + 1), value) }
        sqlite3_bind_double(statement, 17, run.reservedAt.timeIntervalSince1970)
        sqlite3_bind_double(statement, 18, run.expiresAt.timeIntervalSince1970)
        return sqlite3_step(statement) == SQLITE_DONE
    }

    private func insert(_ db: OpaquePointer, receiptID: String, requestID: String,
                        outcome: Outcome, completedAt: Date) -> Bool {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            INSERT INTO verification_run_outcomes
              (completion_id,receipt_id,request_id,state,exit_status,checks_passed,checks_failed,
               duration_ms,log_sha256,inconclusive_code,full_suite_receipt_sha256,completed_at)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?);
            """, -1, &statement, nil) == SQLITE_OK else { return false }
        bind(statement, 1, outcome.completionID); bind(statement, 2, receiptID)
        bind(statement, 3, requestID); bind(statement, 4, outcome.state.rawValue)
        sqlite3_bind_int64(statement, 5, Int64(outcome.exitStatus))
        sqlite3_bind_int64(statement, 6, Int64(outcome.checksPassed))
        sqlite3_bind_int64(statement, 7, Int64(outcome.checksFailed))
        sqlite3_bind_int64(statement, 8, Int64(outcome.durationMS))
        bind(statement, 9, outcome.logSHA256); bind(statement, 10, outcome.inconclusiveCode)
        bind(statement, 11, outcome.fullSuiteReceiptSHA256)
        sqlite3_bind_double(statement, 12, completedAt.timeIntervalSince1970)
        return sqlite3_step(statement) == SQLITE_DONE
    }

    private func expire(_ db: OpaquePointer, _ run: StoredRun) -> Bool {
        let outcome = Outcome(completionID: makeID(), state: .inconclusiveEnvironment,
                              exitStatus: 75, checksPassed: 0, checksFailed: 0, durationMS: 0,
                              logSHA256: String(repeating: "0", count: 64),
                              inconclusiveCode: "reservation_expired", fullSuiteReceiptSHA256: nil)
        return Self.valid(outcome) && insert(db, receiptID: run.receiptID,
                                            requestID: run.request.requestID,
                                            outcome: outcome, completedAt: now())
    }

    private func run(_ db: OpaquePointer, requestID: String)
        -> Result<Result<StoredRun, Failure>?, Failure> {
        selectOne(db, where: "r.request_id = ?", values: [requestID])
    }

    private func run(_ db: OpaquePointer, receiptID: String)
        -> Result<Result<StoredRun, Failure>?, Failure> {
        selectOne(db, where: "r.receipt_id = ?", values: [receiptID])
    }

    private func runs(_ db: OpaquePointer, matching request: ReservationRequest)
        -> Result<[Result<StoredRun, Failure>], Failure> {
        var clause = """
            r.repository_sha256 = ? AND r.subject_kind = ?
            AND r.tree_sha IS ? AND r.base_commit IS ? AND r.overlay_sha256 IS ?
            AND r.question_id = ? AND r.verification_kind = ? AND r.variant = ?
            AND r.mutation_id IS ? AND r.baseline_receipt_id IS ?
            AND r.command_sha256 = ? AND r.environment_sha256 = ?
            """
        var values: [String?] = [request.repositorySHA256, request.subject.kind,
            request.subject.treeSHA, request.subject.baseCommit, request.subject.overlaySHA256,
            request.questionID, request.verificationKind.rawValue, request.variant.rawValue,
            request.mutationID, request.baselineReceiptID, request.commandSHA256,
            request.environmentSHA256]
        if case .workingOverlay = request.subject {
            clause += " AND r.task_id = ?"
            values.append(request.taskID)
        }
        return select(db, where: clause, values: values)
    }

    private func selectOne(_ db: OpaquePointer, where clause: String,
                           values: [String?])
        -> Result<Result<StoredRun, Failure>?, Failure> {
        switch select(db, where: clause, values: values, limit: 1) {
        case .failure(let error): return .failure(error)
        case .success(let rows): return .success(rows.first)
        }
    }

    private func select(_ db: OpaquePointer, where clause: String, values: [String?],
                        limit: Int? = nil) -> Result<[Result<StoredRun, Failure>], Failure> {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        let sql = """
            SELECT r.receipt_id,r.request_id,r.tuple_sha256,r.repository_sha256,r.task_id,r.subject_kind,
                   r.tree_sha,r.base_commit,r.overlay_sha256,r.question_id,r.verification_kind,
                   r.variant,r.mutation_id,r.baseline_receipt_id,r.command_sha256,
                   r.environment_sha256,r.reserved_at,r.expires_at,
                   o.completion_id,o.request_id,o.state,o.exit_status,o.checks_passed,
                   o.checks_failed,o.duration_ms,o.log_sha256,o.inconclusive_code,
                   o.full_suite_receipt_sha256,o.completed_at
              FROM verification_run_reservations r
              LEFT JOIN verification_run_outcomes o ON o.receipt_id = r.receipt_id
             WHERE \(clause)
             ORDER BY r.reserved_at DESC, r.receipt_id DESC
             \(limit == nil ? "" : "LIMIT \(limit!)")
            """
        guard sqlite3_prepare_v2(db, sql, -1, &statement, nil) == SQLITE_OK else {
            return .failure(Failure(code: "query_failed"))
        }
        for (offset, value) in values.enumerated() { bind(statement, Int32(offset + 1), value) }
        var result: [Result<StoredRun, Failure>] = []
        while true {
            let code = selectStep(statement)
            if code == SQLITE_ROW { result.append(decode(statement)); continue }
            if code == SQLITE_DONE { return .success(result) }
            return .failure(Failure(code: "query_failed"))
        }
    }

    private func decode(_ statement: OpaquePointer?) -> Result<StoredRun, Failure> {
        func text(_ index: Int32) -> String? {
            guard sqlite3_column_type(statement, index) != SQLITE_NULL,
                  let bytes = sqlite3_column_text(statement, index) else { return nil }
            return String(cString: bytes)
        }
        guard let receiptID = text(0), let requestID = text(1), let storedTuple = text(2),
              let repository = text(3), let subjectKind = text(5), let question = text(9),
              let kindRaw = text(10), let kind = VerificationKind(rawValue: kindRaw),
              let variantRaw = text(11), let variant = Variant(rawValue: variantRaw),
              let command = text(14), let environment = text(15) else {
            return .failure(Failure(code: "malformed"))
        }
        let subject: Subject
        if subjectKind == "commit_tree", let tree = text(6), text(7) == nil, text(8) == nil {
            subject = .commitTree(treeSHA: tree)
        } else if subjectKind == "working_overlay", let base = text(7), let overlay = text(8), text(6) == nil {
            subject = .workingOverlay(baseCommit: base, overlaySHA256: overlay)
        } else { return .failure(Failure(code: "malformed")) }
        let request = ReservationRequest(requestID: requestID, repositorySHA256: repository,
            taskID: text(4), subject: subject, questionID: question, verificationKind: kind,
            variant: variant, mutationID: text(12), baselineReceiptID: text(13),
            commandSHA256: command, environmentSHA256: environment)
        guard Self.valid(request), Self.uuid(receiptID), Self.sha256(storedTuple),
              storedTuple == Self.tupleSHA256(request) else {
            return .failure(Failure(code: "malformed"))
        }
        func finiteTimestamp(_ index: Int32) -> TimeInterval? {
            let type = sqlite3_column_type(statement, index)
            guard type == SQLITE_INTEGER || type == SQLITE_FLOAT else { return nil }
            let value = sqlite3_column_double(statement, index)
            return value.isFinite && value > 0 ? value : nil
        }
        guard let reservedSeconds = finiteTimestamp(16), let expiresSeconds = finiteTimestamp(17),
              expiresSeconds >= reservedSeconds else { return .failure(Failure(code: "malformed")) }
        let reservedAt = Date(timeIntervalSince1970: reservedSeconds)
        let expiresAt = Date(timeIntervalSince1970: expiresSeconds)
        var outcome: Outcome?, completedAt: Date?
        if let completionID = text(18) {
            guard text(19) == requestID, let stateRaw = text(20),
                  let state = OutcomeState(rawValue: stateRaw), let log = text(25),
                  (21...24).allSatisfy({ sqlite3_column_type(statement, Int32($0)) == SQLITE_INTEGER }),
                  let completedSeconds = finiteTimestamp(28) else {
                return .failure(Failure(code: "malformed"))
            }
            let decoded = Outcome(completionID: completionID, state: state,
                exitStatus: Int(sqlite3_column_int64(statement, 21)),
                checksPassed: Int(sqlite3_column_int64(statement, 22)),
                checksFailed: Int(sqlite3_column_int64(statement, 23)),
                durationMS: Int(sqlite3_column_int64(statement, 24)), logSHA256: log,
                inconclusiveCode: text(26), fullSuiteReceiptSHA256: text(27))
            guard Self.valid(decoded, for: request) else {
                return .failure(Failure(code: "malformed"))
            }
            outcome = decoded
            completedAt = Date(timeIntervalSince1970: completedSeconds)
        } else if (19...28).contains(where: { sqlite3_column_type(statement, Int32($0)) != SQLITE_NULL }) {
            return .failure(Failure(code: "malformed"))
        }
        return .success(StoredRun(receiptID: receiptID, request: request,
                                  reservedAt: reservedAt, expiresAt: expiresAt,
                                  outcome: outcome, completedAt: completedAt))
    }
}

/// Closed, machine-token-only HTTP projection of `VerificationRunLedger`.
enum VerificationRunLedgerHTTP {
    static let reservePath = "/v1/orchestrator/verification-runs/reserve"
    static let prefix = "/v1/orchestrator/verification-runs/"

    static func route(_ request: RemoteServer.Request, machine: Bool,
                      ledger: VerificationRunLedger = .shared) -> RemoteServer.Response? {
        guard request.path == reservePath || request.path.hasPrefix(prefix) else { return nil }
        guard machine else {
            return .error(403, "forbidden", "Verification run receipts need the orchestrator token.")
        }
        if request.method == "POST", request.path == reservePath {
            let body = bodyObject(request)
            switch VerificationRunLedger.parseReservation(body) {
            case .failure(let failure):
                return .error(400, failure.code, "The verification reservation is malformed.")
            case .success(let reservation): return reserveResponse(ledger.reserve(reservation))
            }
        }
        let suffix = String(request.path.dropFirst(prefix.count))
        if request.method == "GET", !suffix.isEmpty, !suffix.contains("/") {
            guard validReceiptID(suffix) else {
                return .error(400, "malformed_receipt_id", "The verification receipt id is malformed.")
            }
            return .json(["verificationRun": payload(ledger.reading(receiptID: suffix))])
        }
        if request.method == "POST", suffix.hasSuffix("/complete") {
            let receiptID = String(suffix.dropLast("/complete".count))
            guard validReceiptID(receiptID) else {
                return .error(400, "malformed_receipt_id", "The verification receipt id is malformed.")
            }
            guard let body = bodyObject(request),
                  Set(body.keys) == Set(["reservation", "outcome"]),
                  let rawReservation = body["reservation"], let rawOutcome = body["outcome"] else {
                return .error(400, "malformed_outcome", "Completion needs reservation and outcome objects.")
            }
            guard case .success(let reservation) = VerificationRunLedger.parseReservation(rawReservation),
                  case .success(let outcome) = VerificationRunLedger.parseOutcome(rawOutcome) else {
                return .error(400, "malformed_outcome", "The verification completion is malformed.")
            }
            return completionResponse(ledger.complete(receiptID: receiptID,
                                                       request: reservation, outcome: outcome))
        }
        return .error(405, "method_not_allowed", "This verification run route does not accept that method.")
    }

    private static func bodyObject(_ request: RemoteServer.Request) -> [String: Any]? {
        (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
    }

    private static func validReceiptID(_ value: String) -> Bool {
        value == value.lowercased() && value.count == 36 && UUID(uuidString: value) != nil
    }

    private static func reserveResponse(_ decision: VerificationRunLedger.ReserveDecision) -> RemoteServer.Response {
        switch decision {
        case .reusable(let run): return .json(["decision": "reusable", "verificationRun": payload(run)])
        case .runRequired(let run, let created):
            return .json(["decision": "run_required", "created": created,
                          "verificationRun": payload(run)], status: 201)
        case .active(let run):
            return .json(["decision": "active", "verificationRun": payload(run)], status: 409)
        case .conflict(let code): return .error(409, code, "The verification identity conflicts with stored evidence.")
        case .malformed(let code): return .error(422, code, "Stored verification evidence is malformed.")
        case .unavailable: return .error(503, "verification_ledger_unavailable", "The verification ledger is unavailable.")
        }
    }

    private static func completionResponse(_ decision: VerificationRunLedger.CompletionDecision) -> RemoteServer.Response {
        switch decision {
        case .completed(let run, let created):
            return .json(["created": created, "verificationRun": payload(run)], status: created ? 201 : 200)
        case .conflict(let code): return .error(409, code, "The completion conflicts with its reservation.")
        case .malformed(let code): return .error(422, code, "The completion or stored receipt is malformed.")
        case .absent: return .error(404, "not_found", "No verification reservation has that receipt id.")
        case .unavailable: return .error(503, "verification_ledger_unavailable", "The verification ledger is unavailable.")
        }
    }

    static func payload(_ reading: VerificationRunLedger.Reading) -> [String: Any] {
        switch reading {
        case .reserved(let run): return payload(run, state: "reserved")
        case .passed(let run): return payload(run, state: "passed")
        case .failed(let run): return payload(run, state: "failed")
        case .inconclusiveEnvironment(let run): return payload(run, state: "inconclusive_environment")
        case .malformed: return ["state": "malformed"]
        case .absent: return ["state": "absent"]
        case .unavailable: return ["state": "unavailable"]
        }
    }

    static func payload(_ run: VerificationRunLedger.StoredRun, state: String? = nil) -> [String: Any] {
        let request = run.request
        var subject: [String: Any] = ["kind": request.subject.kind]
        if let tree = request.subject.treeSHA { subject["treeSha"] = tree }
        if let base = request.subject.baseCommit { subject["baseCommit"] = base }
        if let overlay = request.subject.overlaySHA256 { subject["overlaySha256"] = overlay }
        var value: [String: Any] = [
            "schemaVersion": VerificationRunLedger.schemaVersion,
            "receiptId": run.receiptID, "requestId": request.requestID,
            "tupleSha256": VerificationRunLedger.tupleSHA256(request),
            "repositorySha256": request.repositorySHA256, "subject": subject,
            "questionId": request.questionID, "verificationKind": request.verificationKind.rawValue,
            "variant": request.variant.rawValue, "commandSha256": request.commandSHA256,
            "environmentSha256": request.environmentSHA256,
            "reservedAt": Int(run.reservedAt.timeIntervalSince1970),
            "expiresAt": Int(run.expiresAt.timeIntervalSince1970),
            "state": state ?? (run.outcome?.state.rawValue ?? "reserved")
        ]
        if let task = request.taskID { value["taskId"] = task }
        if let mutation = request.mutationID { value["mutationId"] = mutation }
        if let baseline = request.baselineReceiptID { value["baselineReceiptId"] = baseline }
        if let outcome = run.outcome {
            var result: [String: Any] = ["completionId": outcome.completionID,
                "exitStatus": outcome.exitStatus, "checksPassed": outcome.checksPassed,
                "checksFailed": outcome.checksFailed, "durationMs": outcome.durationMS,
                "logSha256": outcome.logSHA256]
            if let code = outcome.inconclusiveCode { result["inconclusiveCode"] = code }
            if let seal = outcome.fullSuiteReceiptSHA256 { result["fullSuiteReceiptSha256"] = seal }
            value["outcome"] = result
            if let completed = run.completedAt { value["completedAt"] = Int(completed.timeIntervalSince1970) }
        }
        return value
    }
}
