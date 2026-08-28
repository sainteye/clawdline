import CryptoKit
import Foundation
import SQLite3

/// What every assistant session on this machine actually spent, kept where nothing can sweep it.
///
/// **Why this exists at all.** The task registry keeps 200 rows and task directories are deleted
/// 24 hours after they finish, so the evidence behind any question about last month is being
/// destroyed continuously. The one study this machine has — 206 tasks, 939.8M tokens — needed a
/// one-off export and a session of its own, and could not be repeated. This is the durable copy
/// that makes the same question answerable next month without either.
///
/// **Four invariants, and they are the whole design.** Each one is stated here because the
/// mechanism underneath is replaceable and the invariant is not:
///
/// 1. *Re-reading a source can never double-count.* Every measurement this store accepts is a
///    **session cumulative** — Claude's transcript sums from the top, Codex keeps a running
///    total, and a task record's `usage` is the whole transcript rather than that task's slice.
///    So the store never adds what it was handed; it adds the difference between what it was
///    handed and what it has already attributed to that session (``usage_cursors``). Reading the
///    same file twice therefore attributes zero the second time, and a task that ran inside a
///    session `SessionWatch` is also watching is counted once, by whichever collector got there
///    first. The deterministic ``intervalKey(assistant:sessionID:boundary:segment:)`` and its
///    unique constraint are the second belt, not the first.
/// 2. *A row's usage is attributable to one work boundary.* A segment is cut when the boundary
///    changes (a task begins or ends inside a standing session), when the model changes, and at
///    local midnight — see ``SegmentReason``. That keeps a standing session's startup cost in its
///    first segment and makes per-month and per-model figures reproducible.
/// 3. *A source that cannot be read is a state, never a zero and never a missing row.* Unknown
///    token counts are SQL NULL, the row is sealed ``Coverage/sourceMissing``, and every reader
///    here — the aggregate and the export — is required to render that as absent rather than 0.
/// 4. *Every number carries what kind of number it is.* Not one `costUsd`: ``cost_value``,
///    ``cost_unit``, ``cost_basis`` and ``price_snapshot_id``, and when there is no cost at all a
///    ``missing_reason`` naming *which* kind of unavailable it is. 134 of this machine's 165
///    rows with usage carry no cost, and summing those as zero produced "1137M tokens, $0.00" —
///    a month-end that looks entirely normal and is wrong.
///
/// **Append and seal.** A sealed row's numbers never move. Work that arrives after a seal opens
/// the next segment; a re-measurement that disagrees with a sealed row is written to
/// ``usage_corrections`` as new metadata rather than rewriting a value that may already have
/// appeared in a month's total.
///
/// Nothing here reads a terminal or opens a window. The parsing and the arithmetic are static and
/// pure so the suite can hand them a dictionary; only ``observe(_:)`` and its neighbours touch
/// the file, and they do it on one private serial queue.
final class UsageLedger {

    static let shared = UsageLedger()

    /// Bumped when the meaning of a stored column changes. It is part of every interval key, so
    /// rows written under two schemas can never collide or be silently merged.
    static let schemaVersion = 1

    /// Which published price table produced a `list_price_estimate`. This is **not** protection
    /// against a historical month being re-priced — recorded costs are recorded and do not move.
    /// It is so the rows that have *no* cost can be priced later with a permanent record of which
    /// rates were used to do it; without it that later decision is irreversible.
    static let priceSnapshotID = "clawdline-prices-2026-08-28"

    // MARK: - Vocabulary

    /// Where the work came from. `manual` is a session a person opened themselves.
    enum Origin: String {
        case manual, dispatch, schedule
        case followUp = "follow_up"
    }

    /// What a segment is attributable to. A task boundary wins over the session it runs inside,
    /// which is how an attached follow-up is charged its own increment and nothing more.
    enum BoundaryKind: String {
        case session, task
    }

    /// Why this segment was cut from the one before it.
    enum SegmentReason: String {
        case start
        case boundary
        case modelSwitch = "model_switch"
        case localMidnight = "local_midnight"
        case afterSeal = "after_seal"
    }

    /// How much of this segment's source was actually read.
    enum Coverage: String {
        /// Still collecting, or collected from a source that is still growing.
        case partial
        /// Sealed with a source that was read to its end.
        case complete
        /// Sealed with no readable source. Token counts stay NULL; see the invariant above.
        case sourceMissing = "source_missing"
    }

    /// What kind of number the cost is. Never absent — a row with no cost is `unknown`.
    enum CostBasis: String {
        case listPriceEstimate = "list_price_estimate"
        case publishedRateEstimate = "published_rate_estimate"
        case providerActual = "provider_actual"
        case unknown
    }

    enum CostUnit: String {
        case usd = "USD"
        case credits
    }

    /// *Which* kind of unavailable. The hazard the ledger exists to stop is absence with no
    /// reason attached: a consumer cannot tell "billed against a plan" from "nobody recorded a
    /// price" from "this model is not in the table", and all three look like a cheap month.
    enum MissingCost: String {
        /// Codex has no per-session dollar figure in any login mode. Credits or unknown, never 0.
        case planBilled = "plan_billed"
        /// A model this machine has no published per-token price for.
        case noPriceForModel = "no_price_for_model"
        /// The source did not say which model it was, so no price could even be looked up.
        case unknownModel = "unknown_model"
        /// The source carried a price-able model and simply no cost key.
        case noCostRecorded = "no_cost_recorded"
    }

    enum BillingMode: String {
        case plan, metered, unknown
    }

    /// The six columns slice 1 reserves and always leaves NULL. Whole-tree and retry identity
    /// need plumbing that does not exist yet, and a NULL the API names as unavailable is honest
    /// where a value derived from the root session is a guess wearing a column name.
    static let reservedColumns = ["graph_id", "parent_task_id", "retry_of", "attempt",
                                  "landing_state", "disposition"]

    static let reservedColumnsReason =
        "Whole-tree and retry identity are not plumbed yet. These columns exist in schema "
        + "\(schemaVersion) and are always NULL; the aggregate refuses to draw a whole-tree view "
        + "rather than infer one."

    // MARK: - Token counts, every part optional

    /// Normalized token counts. **Every field is optional on purpose**: a key the source did not
    /// carry is nil, and nil is written to SQL as NULL. There is no path in this type that turns
    /// an absent count into `0`.
    struct Counts: Equatable {
        var inputNew: Int?
        var output: Int?
        var cacheRead: Int?
        var cacheWrite: Int?

        var isEmpty: Bool {
            inputNew == nil && output == nil && cacheRead == nil && cacheWrite == nil
        }

        /// The parts, summed — and only when every part is known, so a partial object cannot
        /// masquerade as a smaller total.
        var total: Int? {
            guard let inputNew, let output, let cacheRead, let cacheWrite else { return nil }
            return inputNew + output + cacheRead + cacheWrite
        }
    }

    /// One source's usage object, read.
    struct Reading: Equatable {
        var counts = Counts()
        /// What the source itself called the total, when it said. Kept beside the normalized
        /// parts so a disagreement is visible rather than smoothed over.
        var sourceTotal: Int?
        var cost: Double?
        /// Set when the parts and the source's own total do not reconcile.
        var reconciliation: String?
    }

    /// Every spelling of a usage key this machine has been observed to write.
    ///
    /// **This is the "copy the object as it comes" rule, made concrete.** The registry on disk
    /// spells `cache_read` and `cost_usd`; the same values over HTTP spell `cacheRead` and
    /// `costUsd`; Claude's transcript spells `cache_read_input_tokens`; Codex's rollout spells
    /// `cached_input_tokens`. A collector written against one spelling silently reads 0 from
    /// another, and the field most likely to be dropped is the cache read — 96.6% of every token
    /// on this machine. A key that is absent under *every* spelling stays nil.
    private static let spellings: [String: [String]] = [
        "input": ["input", "input_tokens", "inputTokens"],
        "output": ["output", "output_tokens", "outputTokens"],
        "cacheRead": ["cacheRead", "cache_read", "cache_read_input_tokens",
                      "cached_input_tokens", "cacheReadInputTokens"],
        "cacheWrite": ["cacheWrite", "cache_write", "cache_creation_input_tokens",
                       "cache_write_input_tokens", "cacheCreationInputTokens"],
        "total": ["total", "total_tokens", "totalTokens"],
        "cost": ["costUsd", "cost_usd", "total_cost_usd", "cost"],
    ]

    private static func integer(_ raw: [String: Any], _ field: String) -> Int? {
        for key in spellings[field] ?? [] {
            guard let value = raw[key] else { continue }
            if let int = value as? Int { return int }
            if let number = value as? NSNumber, !(value is NSNull) { return number.intValue }
            if let text = value as? String, let int = Int(text) { return int }
        }
        return nil
    }

    private static func number(_ raw: [String: Any], _ field: String) -> Double? {
        for key in spellings[field] ?? [] {
            guard let value = raw[key] else { continue }
            if let double = value as? Double, double.isFinite { return double }
            if let number = value as? NSNumber, !(value is NSNull), number.doubleValue.isFinite {
                return number.doubleValue
            }
            if let text = value as? String, let double = Double(text), double.isFinite {
                return double
            }
        }
        return nil
    }

    /// Turn a source's usage object into disjoint parts that sum back to its own total.
    ///
    /// **The shape is decided by arithmetic, not by which assistant wrote it.** Codex's
    /// `input_tokens` *includes* its cached input and its `total_tokens` is `input + output`;
    /// Claude's `input_tokens` excludes cache reads and its total is the sum of all four. Rather
    /// than trusting a per-assistant assumption that quietly rots, both readings are computed and
    /// the one that reconciles with the source's own total wins. The assistant is only the
    /// fallback for a source that did not state a total, and a source that reconciles with
    /// neither keeps its parts and is flagged.
    static func normalize(raw: [String: Any], assistant: Assistant) -> Reading {
        var reading = Reading()
        let input = integer(raw, "input")
        let output = integer(raw, "output")
        let cacheRead = integer(raw, "cacheRead")
        let cacheWrite = integer(raw, "cacheWrite")
        reading.sourceTotal = integer(raw, "total")
        reading.cost = number(raw, "cost")
        reading.counts = Counts(inputNew: input, output: output,
                                cacheRead: cacheRead, cacheWrite: cacheWrite)
        guard let input, let output, let cacheRead, let cacheWrite else {
            if reading.counts.isEmpty && reading.sourceTotal == nil && reading.cost == nil {
                reading.reconciliation = "no_recognised_usage_keys"
            }
            return reading
        }
        let disjoint = input + output + cacheRead + cacheWrite
        let overlapped = max(0, input - cacheRead) + output + cacheRead + cacheWrite
        guard let stated = reading.sourceTotal else {
            // No total to check against. Fall back to the shape each assistant is known to
            // write, which is the only remaining evidence.
            if assistant == .codex, input >= cacheRead {
                reading.counts.inputNew = input - cacheRead
            }
            return reading
        }
        if stated == disjoint { return reading }
        if stated == overlapped, input >= cacheRead {
            reading.counts.inputNew = input - cacheRead
            return reading
        }
        if assistant == .codex, input >= cacheRead { reading.counts.inputNew = input - cacheRead }
        reading.reconciliation = "parts_do_not_sum"
        return reading
    }

    /// Which kind of unavailable a missing cost is. Never a guessed number — only a reason.
    static func missingCostReason(assistant: Assistant, model: String?) -> MissingCost {
        if assistant == .codex { return .planBilled }
        guard let model, !model.isEmpty else { return .unknownModel }
        return Orchestrator.price(forModel: model) == nil ? .noPriceForModel : .noCostRecorded
    }

    static func billingMode(assistant: Assistant) -> BillingMode {
        // Codex bills against a plan and says so. A Claude session may be a subscription or an
        // API key and the transcript does not record which, so it is `unknown` rather than a
        // coin toss dressed as a fact.
        assistant == .codex ? .plan : .unknown
    }

    // MARK: - Identity

    /// `SHA256(assistant, session_id, boundary_kind, boundary_id, segment_no, schema_version)`.
    /// Deterministic, so the same segment read again lands on the same row.
    static func intervalKey(assistant: Assistant, sessionID: String, boundaryKind: BoundaryKind,
                            boundaryID: String, segmentNo: Int,
                            schemaVersion: Int = UsageLedger.schemaVersion) -> String {
        // Unit separators rather than a plain join: a session id containing the delimiter must
        // not be able to produce another session's key.
        let joined = [assistant.rawValue, sessionID, boundaryKind.rawValue, boundaryID,
                      String(segmentNo), String(schemaVersion)].joined(separator: "\u{1F}")
        return SHA256.hash(data: Data(joined.utf8)).map { String(format: "%02x", $0) }.joined()
    }

    /// `YYYY-MM-DD` and nothing else. Range bounds are compared as text against `local_day`, so a
    /// value that is not a local day would silently select a different set of rows.
    static func isLocalDay(_ value: String) -> Bool {
        let parts = value.split(separator: "-", omittingEmptySubsequences: false)
        guard parts.count == 3, parts[0].count == 4, parts[1].count == 2, parts[2].count == 2,
              value.allSatisfy({ $0 == "-" || $0.isASCII && $0.isNumber }) else { return false }
        guard let month = Int(parts[1]), let day = Int(parts[2]) else { return false }
        return (1...12).contains(month) && (1...31).contains(day)
    }

    /// The local calendar day a reading belongs to. Segments are cut at local midnight so a
    /// month's total is the month the person lived through, not a UTC approximation of it.
    static func localDay(of date: Date, calendar: Calendar = .autoupdatingCurrent) -> String {
        let parts = calendar.dateComponents([.year, .month, .day], from: date)
        return String(format: "%04d-%02d-%02d", parts.year ?? 0, parts.month ?? 0, parts.day ?? 0)
    }

    // MARK: - One reading, handed in

    /// One observation of one session: who it is, which boundary it is inside right now, and the
    /// source's own **cumulative** usage object exactly as it came.
    struct Sample {
        var assistant: Assistant
        var sessionID: String
        var boundaryKind: BoundaryKind
        var boundaryID: String
        var origin: Origin
        var observedAt = Date()

        var taskID: String?
        var scheduleID: String?
        var projectKey: String?
        var workingDir: String?
        var kindRaw: String?
        var isolation: String?
        var depth: Int?
        var claimCount: Int?
        var timeoutSeconds: Int?
        var taskState: String?
        var model: String?
        var reasoningEffort: String?

        /// The source's usage object, copied as it came. **Nil means the source could not be
        /// read**, which is a state — never a zero.
        var rawUsage: [String: Any]?
        /// What kind of number a cost inside `rawUsage` is. The collector knows the provenance;
        /// the store never decides this for itself and never recomputes the value.
        var costBasis = CostBasis.listPriceEstimate
        var costUnit = CostUnit.usd
        /// Byte length of the source at this reading — the checkpoint that lets an unchanged
        /// file be skipped without re-reading it.
        var sourceBytes: Int?
        /// Seal the segment after attributing this reading.
        var seal = false
        /// What the seal should record. Ignored unless `seal` is set.
        var sealCoverage = Coverage.complete
        var coverageReason: String?

        init(assistant: Assistant, sessionID: String, boundaryKind: BoundaryKind,
             boundaryID: String, origin: Origin) {
            self.assistant = assistant
            self.sessionID = sessionID
            self.boundaryKind = boundaryKind
            self.boundaryID = boundaryID
            self.origin = origin
        }
    }

    /// A stored row, read back.
    struct Row: Equatable {
        var intervalKey = ""
        var assistant = ""
        var sessionID = ""
        var boundaryKind = ""
        var boundaryID = ""
        var segmentNo = 0
        var segmentReason = ""
        var origin = ""
        var taskID: String?
        var scheduleID: String?
        var projectKey: String?
        var workingDir: String?
        var kindRaw: String?
        var isolation: String?
        var depth: Int?
        var claimCount: Int?
        var timeoutSeconds: Int?
        var taskState: String?
        var model: String?
        var reasoningEffort: String?
        var billingMode = ""
        var rawUsage: String?
        var counts = Counts()
        var total: Int?
        var sourceTotal: Int?
        var reconciliation: String?
        var costValue: Double?
        var costUnit: String?
        var costBasis = ""
        var priceSnapshotID: String?
        var missingReason: String?
        var coverage = ""
        var coverageReason: String?
        var sealed = false
        var sourceBytes: Int?
        var startedAt = Date.distantPast
        var endedAt: Date?
        var localDay = ""
        var updatedAt = Date.distantPast

        /// True for a row nothing may render as a number. Sorted to the top of every coverage
        /// view because the sessions most likely to go missing are the long ones, which biases
        /// every total downward.
        var usageUnknown: Bool { counts.isEmpty }
    }

    // MARK: - Where it lives

    static var storeURLOverrideForTesting: URL?

    /// `~/Library/Application Support/Clawdline/Observability/usage.sqlite3` — deliberately not
    /// beside `orchestrator.json`, not under `/tmp/.clawdline`, and not in the transcript
    /// archive, so neither the 200-row cap nor the 24-hour sweep can reach it.
    static var storeURL: URL {
        if let override = storeURLOverrideForTesting { return override }
        if let named = ProcessInfo.processInfo.environment["CLAWDLINE_OBSERVABILITY_DIR"],
           !named.isEmpty {
            return URL(fileURLWithPath: Paths.expand(named), isDirectory: true)
                .appendingPathComponent("usage.sqlite3")
        }
        return FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Application Support/Clawdline/Observability",
                                    isDirectory: true)
            .appendingPathComponent("usage.sqlite3")
    }

    private let queue = DispatchQueue(label: "dev.sainteye.clawdline.usageledger")
    private var handle: OpaquePointer?
    private var openedFor: String?

    private init() {}

    // MARK: - SQLite, kept in one place

    private static let transient = unsafeBitCast(-1, to: sqlite3_destructor_type.self)

    /// The open database, opening it the first time and after the store path moves under a test.
    private func database() -> OpaquePointer? {
        let url = UsageLedger.storeURL
        if let handle, openedFor == url.path { return handle }
        if handle != nil { sqlite3_close(handle); handle = nil; openedFor = nil }
        let directory = url.deletingLastPathComponent()
        try? FileManager.default.createDirectory(
            at: directory, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        try? FileManager.default.setAttributes([.posixPermissions: 0o700],
                                               ofItemAtPath: directory.path)
        var db: OpaquePointer?
        guard sqlite3_open_v2(url.path, &db,
                              SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE | SQLITE_OPEN_FULLMUTEX,
                              nil) == SQLITE_OK, let db else {
            Log.write("usage ledger could not open \(url.path)")
            if let db { sqlite3_close(db) }
            return nil
        }
        try? FileManager.default.setAttributes([.posixPermissions: 0o600],
                                               ofItemAtPath: url.path)
        exec(db, "PRAGMA journal_mode=WAL;")
        exec(db, "PRAGMA synchronous=NORMAL;")
        exec(db, "PRAGMA busy_timeout=5000;")
        exec(db, "PRAGMA foreign_keys=ON;")
        migrate(db)
        handle = db
        openedFor = url.path
        return db
    }

    @discardableResult
    private func exec(_ db: OpaquePointer, _ sql: String) -> Bool {
        var error: UnsafeMutablePointer<CChar>?
        let ok = sqlite3_exec(db, sql, nil, nil, &error) == SQLITE_OK
        if !ok, let error {
            Log.write("usage ledger SQL failed: \(String(cString: error))")
            sqlite3_free(error)
        }
        return ok
    }

    /// Schema migrations are the app's, keyed on `PRAGMA user_version`. Adding a version means
    /// adding a case below; existing rows are never rewritten by one.
    private func migrate(_ db: OpaquePointer) {
        var statement: OpaquePointer?
        var version = 0
        if sqlite3_prepare_v2(db, "PRAGMA user_version;", -1, &statement, nil) == SQLITE_OK,
           sqlite3_step(statement) == SQLITE_ROW {
            version = Int(sqlite3_column_int64(statement, 0))
        }
        sqlite3_finalize(statement)
        guard version < UsageLedger.schemaVersion else { return }
        if version < 1 {
            exec(db, """
                CREATE TABLE IF NOT EXISTS usage_intervals (
                  interval_key TEXT PRIMARY KEY,
                  schema_version INTEGER NOT NULL,
                  assistant TEXT NOT NULL,
                  session_id TEXT NOT NULL,
                  boundary_kind TEXT NOT NULL,
                  boundary_id TEXT NOT NULL,
                  segment_no INTEGER NOT NULL,
                  segment_reason TEXT NOT NULL,
                  origin TEXT NOT NULL,
                  task_id TEXT, schedule_id TEXT,
                  project_key TEXT, working_dir TEXT,
                  kind_raw TEXT, isolation TEXT,
                  depth INTEGER, claim_count INTEGER, timeout_seconds INTEGER,
                  task_state TEXT,
                  model TEXT, reasoning_effort TEXT, billing_mode TEXT NOT NULL,
                  usage_raw TEXT,
                  input_new INTEGER, output INTEGER,
                  cache_read INTEGER, cache_write INTEGER, total INTEGER,
                  source_total INTEGER, reconciliation TEXT,
                  cost_value REAL, cost_unit TEXT, cost_basis TEXT NOT NULL,
                  price_snapshot_id TEXT, missing_reason TEXT,
                  coverage TEXT NOT NULL, coverage_reason TEXT,
                  sealed INTEGER NOT NULL DEFAULT 0,
                  source_bytes INTEGER,
                  started_at REAL NOT NULL, ended_at REAL,
                  local_day TEXT NOT NULL,
                  observed_at REAL NOT NULL, updated_at REAL NOT NULL,
                  graph_id TEXT, parent_task_id TEXT, retry_of TEXT, attempt INTEGER,
                  landing_state TEXT, disposition TEXT,
                  UNIQUE (assistant, session_id, boundary_kind, boundary_id,
                          segment_no, schema_version)
                );
                CREATE INDEX IF NOT EXISTS usage_intervals_day
                  ON usage_intervals (local_day);
                CREATE INDEX IF NOT EXISTS usage_intervals_task
                  ON usage_intervals (task_id);
                CREATE TABLE IF NOT EXISTS usage_cursors (
                  assistant TEXT NOT NULL,
                  session_id TEXT NOT NULL,
                  attributed_input INTEGER, attributed_output INTEGER,
                  attributed_cache_read INTEGER, attributed_cache_write INTEGER,
                  attributed_cost REAL,
                  open_key TEXT, boundary_kind TEXT, boundary_id TEXT, segment_no INTEGER,
                  model TEXT, local_day TEXT, source_bytes INTEGER,
                  updated_at REAL NOT NULL,
                  PRIMARY KEY (assistant, session_id)
                );
                CREATE TABLE IF NOT EXISTS usage_corrections (
                  id INTEGER PRIMARY KEY AUTOINCREMENT,
                  interval_key TEXT NOT NULL,
                  reason TEXT NOT NULL,
                  was TEXT, proposed TEXT,
                  written_at REAL NOT NULL
                );
                CREATE TABLE IF NOT EXISTS price_snapshots (
                  id TEXT PRIMARY KEY,
                  captured_at REAL NOT NULL,
                  rates TEXT NOT NULL
                );
                """)
        }
        exec(db, "PRAGMA user_version=\(UsageLedger.schemaVersion);")
        recordPriceSnapshot(db)
    }

    /// The price table as it stands, written once under its id, so a later decision to price the
    /// rows that carry no cost leaves a permanent record of which rates produced the number.
    private func recordPriceSnapshot(_ db: OpaquePointer) {
        var rates: [String: [String: Double]] = [:]
        for model in ["claude-fable-5", "claude-mythos-5", "claude-opus", "claude-sonnet",
                      "claude-haiku-4-5"] {
            guard let price = Orchestrator.price(forModel: model) else { continue }
            rates[model] = ["input": price.input, "output": price.output,
                            "cache_read_multiplier": 0.1, "cache_write_multiplier": 1.25]
        }
        let json = String(decoding: (try? JSONSerialization.data(
            withJSONObject: rates, options: [.sortedKeys])) ?? Data("{}".utf8), as: UTF8.self)
        var statement: OpaquePointer?
        guard sqlite3_prepare_v2(db, """
            INSERT INTO price_snapshots (id, captured_at, rates) VALUES (?, ?, ?)
            ON CONFLICT(id) DO NOTHING;
            """, -1, &statement, nil) == SQLITE_OK else { return }
        bind(statement, 1, UsageLedger.priceSnapshotID)
        sqlite3_bind_double(statement, 2, Date().timeIntervalSince1970)
        bind(statement, 3, json)
        sqlite3_step(statement)
        sqlite3_finalize(statement)
    }

    private func bind(_ statement: OpaquePointer?, _ index: Int32, _ value: String?) {
        if let value {
            sqlite3_bind_text(statement, index, value, -1, UsageLedger.transient)
        } else {
            sqlite3_bind_null(statement, index)
        }
    }

    private func bind(_ statement: OpaquePointer?, _ index: Int32, _ value: Int?) {
        if let value { sqlite3_bind_int64(statement, index, Int64(value)) }
        else { sqlite3_bind_null(statement, index) }
    }

    private func bind(_ statement: OpaquePointer?, _ index: Int32, _ value: Double?) {
        if let value { sqlite3_bind_double(statement, index, value) }
        else { sqlite3_bind_null(statement, index) }
    }

    private static func text(_ statement: OpaquePointer?, _ index: Int32) -> String? {
        guard sqlite3_column_type(statement, index) != SQLITE_NULL,
              let raw = sqlite3_column_text(statement, index) else { return nil }
        return String(cString: raw)
    }

    private static func integer(_ statement: OpaquePointer?, _ index: Int32) -> Int? {
        guard sqlite3_column_type(statement, index) != SQLITE_NULL else { return nil }
        return Int(sqlite3_column_int64(statement, index))
    }

    private static func double(_ statement: OpaquePointer?, _ index: Int32) -> Double? {
        guard sqlite3_column_type(statement, index) != SQLITE_NULL else { return nil }
        return sqlite3_column_double(statement, index)
    }

    // MARK: - The cursor: what has already been attributed to a session

    private struct Cursor {
        var input: Int?
        var output: Int?
        var cacheRead: Int?
        var cacheWrite: Int?
        var cost: Double?
        var openKey: String?
        var boundaryKind: String?
        var boundaryID: String?
        var segmentNo = 0
        var model: String?
        var localDay: String?
        var sourceBytes: Int?
    }

    private func cursor(_ db: OpaquePointer, assistant: String, session: String) -> Cursor? {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            SELECT attributed_input, attributed_output, attributed_cache_read,
                   attributed_cache_write, attributed_cost, open_key, boundary_kind,
                   boundary_id, segment_no, model, local_day, source_bytes
              FROM usage_cursors WHERE assistant = ? AND session_id = ?;
            """, -1, &statement, nil) == SQLITE_OK else { return nil }
        bind(statement, 1, assistant)
        bind(statement, 2, session)
        guard sqlite3_step(statement) == SQLITE_ROW else { return nil }
        var out = Cursor()
        out.input = Self.integer(statement, 0)
        out.output = Self.integer(statement, 1)
        out.cacheRead = Self.integer(statement, 2)
        out.cacheWrite = Self.integer(statement, 3)
        out.cost = Self.double(statement, 4)
        out.openKey = Self.text(statement, 5)
        out.boundaryKind = Self.text(statement, 6)
        out.boundaryID = Self.text(statement, 7)
        out.segmentNo = Self.integer(statement, 8) ?? 0
        out.model = Self.text(statement, 9)
        out.localDay = Self.text(statement, 10)
        out.sourceBytes = Self.integer(statement, 11)
        return out
    }

    private func write(_ db: OpaquePointer, cursor: Cursor, assistant: String, session: String,
                       at: Date) {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            INSERT INTO usage_cursors (assistant, session_id, attributed_input,
                attributed_output, attributed_cache_read, attributed_cache_write,
                attributed_cost, open_key, boundary_kind, boundary_id, segment_no, model,
                local_day, source_bytes, updated_at)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
            ON CONFLICT(assistant, session_id) DO UPDATE SET
                attributed_input = excluded.attributed_input,
                attributed_output = excluded.attributed_output,
                attributed_cache_read = excluded.attributed_cache_read,
                attributed_cache_write = excluded.attributed_cache_write,
                attributed_cost = excluded.attributed_cost,
                open_key = excluded.open_key,
                boundary_kind = excluded.boundary_kind,
                boundary_id = excluded.boundary_id,
                segment_no = excluded.segment_no,
                model = excluded.model,
                local_day = excluded.local_day,
                source_bytes = excluded.source_bytes,
                updated_at = excluded.updated_at;
            """, -1, &statement, nil) == SQLITE_OK else { return }
        bind(statement, 1, assistant)
        bind(statement, 2, session)
        bind(statement, 3, cursor.input)
        bind(statement, 4, cursor.output)
        bind(statement, 5, cursor.cacheRead)
        bind(statement, 6, cursor.cacheWrite)
        bind(statement, 7, cursor.cost)
        bind(statement, 8, cursor.openKey)
        bind(statement, 9, cursor.boundaryKind)
        bind(statement, 10, cursor.boundaryID)
        bind(statement, 11, cursor.segmentNo)
        bind(statement, 12, cursor.model)
        bind(statement, 13, cursor.localDay)
        bind(statement, 14, cursor.sourceBytes)
        sqlite3_bind_double(statement, 15, at.timeIntervalSince1970)
        sqlite3_step(statement)
    }

    // MARK: - Observing

    /// Take one reading of one session. Safe to call as often as anything likes: a reading that
    /// says nothing new attributes nothing.
    func observe(_ sample: Sample) {
        queue.async { [weak self] in _ = self?.apply(sample) }
    }

    /// The same thing, synchronously, for a caller that needs the answer — the suite, and the
    /// finalize collector, which must have written its row before the task's directory goes.
    @discardableResult
    func observeNow(_ sample: Sample) -> String? {
        queue.sync { apply(sample) }
    }

    /// Returns the interval key the reading landed on, or nil when nothing could be written.
    private func apply(_ sample: Sample) -> String? {
        guard let db = database(), !sample.sessionID.isEmpty, !sample.boundaryID.isEmpty
        else { return nil }
        let assistant = sample.assistant.rawValue
        let day = UsageLedger.localDay(of: sample.observedAt)
        var current = cursor(db, assistant: assistant, session: sample.sessionID) ?? Cursor()
        let reading = sample.rawUsage.map {
            UsageLedger.normalize(raw: $0, assistant: sample.assistant)
        }
        var openKey = current.openKey
        let openIsSealed = openKey.map { isSealed(db, key: $0) } ?? false

        // 1. Cut where the work boundary, the model or the local day changed — and after a seal,
        //    because a sealed row's numbers never move again.
        let boundaryChanged = current.boundaryKind != sample.boundaryKind.rawValue
            || current.boundaryID != sample.boundaryID
        let modelChanged = openKey != nil && sample.model != nil && current.model != nil
            && sample.model != current.model
        let dayChanged = openKey != nil && current.localDay != nil && current.localDay != day
        let reason: SegmentReason
        if openKey == nil { reason = .start }
        else if boundaryChanged { reason = .boundary }
        else if modelChanged { reason = .modelSwitch }
        else if dayChanged { reason = .localMidnight }
        else { reason = .afterSeal }
        let mustCut = openKey == nil || boundaryChanged || modelChanged || dayChanged
            || openIsSealed
        if mustCut {
            // Seal before choosing a slot, so the number below can never land on a row that is
            // still open — a session has exactly one open segment at a time, by construction.
            if let key = openKey { sealRow(db, key: key, coverage: nil, at: sample.observedAt) }
            let segment = nextSegment(db, sample: sample)
            let key = UsageLedger.intervalKey(
                assistant: sample.assistant, sessionID: sample.sessionID,
                boundaryKind: sample.boundaryKind, boundaryID: sample.boundaryID,
                segmentNo: segment)
            insertSkeleton(db, key: key, sample: sample, segment: segment, reason: reason, day: day)
            openKey = key
            current.openKey = key
            current.boundaryKind = sample.boundaryKind.rawValue
            current.boundaryID = sample.boundaryID
            current.segmentNo = segment
            current.localDay = day
        }
        guard let key = openKey else { return nil }

        // 2. Attribute the difference **after** the cut, to the segment the reading itself
        //    describes. The caller's boundary is a statement about this measurement — "these
        //    counters are what task T has left behind" — so a follow-up attached to a standing
        //    session is charged its own increment and the session keeps what it had spent before
        //    it arrived. The same rule on a model switch puts the increment on the model that
        //    reported it; the checkpoint cadence is what bounds how much of a shared window can
        //    be misfiled either way.
        if let reading {
            let delta = Self.delta(from: current, to: reading)
            if delta.regressed {
                note(db, key: key, coverageReason: "source_regressed")
            } else {
                write(db, key: key, delta: delta, sample: sample, reading: reading)
                current = Self.advance(current, by: reading)
            }
        }
        current.model = sample.model ?? current.model
        current.localDay = day
        if let bytes = sample.sourceBytes { current.sourceBytes = bytes }
        updateFacts(db, key: key, sample: sample, reading: reading)
        if let reason = sample.coverageReason { note(db, key: key, coverageReason: reason) }
        if sample.seal { sealRow(db, key: key, coverage: sample.sealCoverage, at: sample.observedAt) }
        write(db, cursor: current, assistant: assistant, session: sample.sessionID,
              at: sample.observedAt)
        return key
    }

    /// The next free segment slot for a boundary. Every earlier segment of it is sealed by the
    /// time this is asked, so a session that returns to a boundary it left — a standing session
    /// after the task inside it finished — opens a new segment instead of writing into the old
    /// one and being refused.
    private func nextSegment(_ db: OpaquePointer, sample: Sample) -> Int {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            SELECT COALESCE(MAX(segment_no) + 1, 0) FROM usage_intervals
             WHERE assistant = ? AND session_id = ? AND boundary_kind = ? AND boundary_id = ?
               AND schema_version = ?;
            """, -1, &statement, nil) == SQLITE_OK else { return 0 }
        bind(statement, 1, sample.assistant.rawValue)
        bind(statement, 2, sample.sessionID)
        bind(statement, 3, sample.boundaryKind.rawValue)
        bind(statement, 4, sample.boundaryID)
        bind(statement, 5, UsageLedger.schemaVersion)
        guard sqlite3_step(statement) == SQLITE_ROW else { return 0 }
        return Int(sqlite3_column_int64(statement, 0))
    }

    private struct Delta {
        var input: Int?
        var output: Int?
        var cacheRead: Int?
        var cacheWrite: Int?
        var cost: Double?
        var regressed = false

        var hasTokens: Bool {
            input != nil || output != nil || cacheRead != nil || cacheWrite != nil
        }
    }

    /// What is new since the last reading of this session. **Never negative**: a cumulative
    /// counter that has gone backwards means the source was replaced or truncated, and the honest
    /// answer to that is a coverage note, not a subtraction. A part the source did not carry is
    /// nil here and stays NULL in the row.
    private static func delta(from cursor: Cursor, to reading: Reading) -> Delta {
        var out = Delta()
        func part(_ attributed: Int?, _ measured: Int?) -> Int? {
            guard let measured else { return nil }
            let already = attributed ?? 0
            guard measured >= already else { out.regressed = true; return nil }
            return measured - already
        }
        out.input = part(cursor.input, reading.counts.inputNew)
        out.output = part(cursor.output, reading.counts.output)
        out.cacheRead = part(cursor.cacheRead, reading.counts.cacheRead)
        out.cacheWrite = part(cursor.cacheWrite, reading.counts.cacheWrite)
        if let measured = reading.cost {
            let already = cursor.cost ?? 0
            if measured + 1e-9 >= already { out.cost = measured - already } else { out.regressed = true }
        }
        return out
    }

    private static func advance(_ cursor: Cursor, by reading: Reading) -> Cursor {
        var out = cursor
        if let value = reading.counts.inputNew { out.input = value }
        if let value = reading.counts.output { out.output = value }
        if let value = reading.counts.cacheRead { out.cacheRead = value }
        if let value = reading.counts.cacheWrite { out.cacheWrite = value }
        if let value = reading.cost { out.cost = value }
        return out
    }

    private func isSealed(_ db: OpaquePointer, key: String) -> Bool {
        guard !key.isEmpty else { return false }
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, "SELECT sealed FROM usage_intervals WHERE interval_key = ?;",
                                 -1, &statement, nil) == SQLITE_OK else { return false }
        bind(statement, 1, key)
        guard sqlite3_step(statement) == SQLITE_ROW else { return false }
        return sqlite3_column_int64(statement, 0) == 1
    }

    private func insertSkeleton(_ db: OpaquePointer, key: String, sample: Sample, segment: Int,
                                reason: SegmentReason, day: String) {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            INSERT INTO usage_intervals (
              interval_key, schema_version, assistant, session_id, boundary_kind, boundary_id,
              segment_no, segment_reason, origin, task_id, schedule_id, project_key, working_dir,
              kind_raw, isolation, depth, claim_count, timeout_seconds, task_state, model,
              reasoning_effort, billing_mode, cost_basis, coverage, sealed, started_at,
              local_day, observed_at, updated_at)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,?,?,?)
            ON CONFLICT(interval_key) DO NOTHING;
            """, -1, &statement, nil) == SQLITE_OK else { return }
        bind(statement, 1, key)
        bind(statement, 2, UsageLedger.schemaVersion)
        bind(statement, 3, sample.assistant.rawValue)
        bind(statement, 4, sample.sessionID)
        bind(statement, 5, sample.boundaryKind.rawValue)
        bind(statement, 6, sample.boundaryID)
        bind(statement, 7, segment)
        bind(statement, 8, reason.rawValue)
        bind(statement, 9, sample.origin.rawValue)
        bind(statement, 10, sample.taskID)
        bind(statement, 11, sample.scheduleID)
        bind(statement, 12, sample.projectKey)
        bind(statement, 13, sample.workingDir)
        bind(statement, 14, sample.kindRaw)
        bind(statement, 15, sample.isolation)
        bind(statement, 16, sample.depth)
        bind(statement, 17, sample.claimCount)
        bind(statement, 18, sample.timeoutSeconds)
        bind(statement, 19, sample.taskState)
        bind(statement, 20, sample.model)
        bind(statement, 21, sample.reasoningEffort)
        bind(statement, 22, UsageLedger.billingMode(assistant: sample.assistant).rawValue)
        bind(statement, 23, CostBasis.unknown.rawValue)
        bind(statement, 24, Coverage.partial.rawValue)
        sqlite3_bind_double(statement, 25, sample.observedAt.timeIntervalSince1970)
        bind(statement, 26, day)
        sqlite3_bind_double(statement, 27, sample.observedAt.timeIntervalSince1970)
        sqlite3_bind_double(statement, 28, sample.observedAt.timeIntervalSince1970)
        sqlite3_step(statement)
    }

    /// Add a delta to an open row. A sealed row is never touched here; the caller cuts first.
    ///
    /// **A part the source did not carry is left alone rather than added to.** `COALESCE(x, 0) +
    /// 0` would turn an honest NULL into a `0` that reads as a measurement, which is the one
    /// thing this store must never do; the `CASE` below is what stops it.
    private func write(_ db: OpaquePointer, key: String, delta: Delta, sample: Sample,
                       reading: Reading) {
        let at = sample.observedAt
        if delta.hasTokens {
            var statement: OpaquePointer?
            defer { sqlite3_finalize(statement) }
            guard sqlite3_prepare_v2(db, """
                UPDATE usage_intervals SET
                  input_new = CASE WHEN ?1 IS NULL THEN input_new
                                   ELSE COALESCE(input_new, 0) + ?1 END,
                  output = CASE WHEN ?2 IS NULL THEN output
                                ELSE COALESCE(output, 0) + ?2 END,
                  cache_read = CASE WHEN ?3 IS NULL THEN cache_read
                                    ELSE COALESCE(cache_read, 0) + ?3 END,
                  cache_write = CASE WHEN ?4 IS NULL THEN cache_write
                                     ELSE COALESCE(cache_write, 0) + ?4 END,
                  source_bytes = COALESCE(?5, source_bytes),
                  observed_at = ?6, updated_at = ?6
                WHERE interval_key = ?7 AND sealed = 0;
                """, -1, &statement, nil) == SQLITE_OK else { return }
            bind(statement, 1, delta.input)
            bind(statement, 2, delta.output)
            bind(statement, 3, delta.cacheRead)
            bind(statement, 4, delta.cacheWrite)
            bind(statement, 5, sample.sourceBytes)
            sqlite3_bind_double(statement, 6, at.timeIntervalSince1970)
            bind(statement, 7, key)
            sqlite3_step(statement)
            recomputeTotal(db, key: key)
        }
        if let cost = delta.cost {
            var statement: OpaquePointer?
            defer { sqlite3_finalize(statement) }
            guard sqlite3_prepare_v2(db, """
                UPDATE usage_intervals SET cost_value = COALESCE(cost_value, 0) + ?1,
                       updated_at = ?2
                WHERE interval_key = ?3 AND sealed = 0;
                """, -1, &statement, nil) == SQLITE_OK else { return }
            sqlite3_bind_double(statement, 1, cost)
            sqlite3_bind_double(statement, 2, at.timeIntervalSince1970)
            bind(statement, 3, key)
            sqlite3_step(statement)
        }
        rememberRaw(db, key: key, sample: sample, reading: reading)
    }

    /// The stored total is the sum of the stored parts, and it is NULL the moment any part is —
    /// the same rule ``Counts/total`` follows, so the two can never drift apart.
    private func recomputeTotal(_ db: OpaquePointer, key: String) {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            UPDATE usage_intervals SET total = CASE
                WHEN input_new IS NULL OR output IS NULL OR cache_read IS NULL
                     OR cache_write IS NULL THEN NULL
                ELSE input_new + output + cache_read + cache_write END
            WHERE interval_key = ? AND sealed = 0;
            """, -1, &statement, nil) == SQLITE_OK else { return }
        bind(statement, 1, key)
        sqlite3_step(statement)
    }

    /// Keep the source's own object beside the normalized parts. This is what makes per-row
    /// reconciliation against the registry and the HTTP record possible at all: the comparison is
    /// on the object as it came, not on this store's interpretation of it.
    private func rememberRaw(_ db: OpaquePointer, key: String, sample: Sample, reading: Reading) {
        guard let raw = sample.rawUsage,
              let data = try? JSONSerialization.data(withJSONObject: raw, options: [.sortedKeys])
        else { return }
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            UPDATE usage_intervals SET usage_raw = ?, source_total = ?, reconciliation = ?
            WHERE interval_key = ? AND sealed = 0;
            """, -1, &statement, nil) == SQLITE_OK else { return }
        bind(statement, 1, String(decoding: data, as: UTF8.self))
        bind(statement, 2, reading.sourceTotal)
        bind(statement, 3, reading.reconciliation)
        bind(statement, 4, key)
        sqlite3_step(statement)
    }

    /// The columns that describe the work rather than the spend, refreshed on every reading, plus
    /// the valuation — which is where an absent cost is turned into a named reason instead of a
    /// number nobody can question.
    private func updateFacts(_ db: OpaquePointer, key: String, sample: Sample,
                             reading: Reading?) {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            UPDATE usage_intervals SET
              task_state = COALESCE(?, task_state),
              model = COALESCE(?, model),
              origin = ?,
              cost_basis = ?, cost_unit = ?, price_snapshot_id = ?, missing_reason = ?,
              updated_at = ?
            WHERE interval_key = ? AND sealed = 0;
            """, -1, &statement, nil) == SQLITE_OK else { return }
        let hasCost = costOnRow(db, key: key) != nil || (reading?.cost != nil)
        let basis = hasCost ? sample.costBasis : .unknown
        bind(statement, 1, sample.taskState)
        bind(statement, 2, sample.model)
        bind(statement, 3, sample.origin.rawValue)
        bind(statement, 4, basis.rawValue)
        bind(statement, 5, hasCost ? sample.costUnit.rawValue : nil)
        bind(statement, 6, hasCost ? UsageLedger.priceSnapshotID : nil)
        bind(statement, 7, hasCost ? nil : UsageLedger.missingCostReason(
            assistant: sample.assistant, model: sample.model).rawValue)
        sqlite3_bind_double(statement, 8, sample.observedAt.timeIntervalSince1970)
        bind(statement, 9, key)
        sqlite3_step(statement)
    }

    private func costOnRow(_ db: OpaquePointer, key: String) -> Double? {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, "SELECT cost_value FROM usage_intervals WHERE interval_key = ?;",
                                 -1, &statement, nil) == SQLITE_OK else { return nil }
        bind(statement, 1, key)
        guard sqlite3_step(statement) == SQLITE_ROW else { return nil }
        return Self.double(statement, 0)
    }

    private func note(_ db: OpaquePointer, key: String, coverageReason: String) {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            UPDATE usage_intervals SET coverage_reason = ?, updated_at = ?
            WHERE interval_key = ? AND sealed = 0;
            """, -1, &statement, nil) == SQLITE_OK else { return }
        bind(statement, 1, coverageReason)
        sqlite3_bind_double(statement, 2, Date().timeIntervalSince1970)
        bind(statement, 3, key)
        sqlite3_step(statement)
    }

    /// Seal a row. `coverage` nil closes a segment that is being cut, which keeps whatever
    /// coverage it had — a cut is not evidence about the source.
    private func sealRow(_ db: OpaquePointer, key: String, coverage: Coverage?, at: Date) {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            UPDATE usage_intervals SET
              sealed = 1,
              coverage = COALESCE(?, CASE WHEN coverage = 'partial' THEN 'complete'
                                          ELSE coverage END),
              ended_at = COALESCE(ended_at, ?),
              updated_at = ?
            WHERE interval_key = ? AND sealed = 0;
            """, -1, &statement, nil) == SQLITE_OK else { return }
        bind(statement, 1, coverage?.rawValue)
        sqlite3_bind_double(statement, 2, at.timeIntervalSince1970)
        sqlite3_bind_double(statement, 3, at.timeIntervalSince1970)
        bind(statement, 4, key)
        sqlite3_step(statement)
        if coverage == .sourceMissing { clearUnknownUsage(db, key: key) }
    }

    /// A `source_missing` seal leaves the token columns NULL rather than the zeros a skeleton
    /// starts life with. This is the line most likely to be quietly removed by somebody who finds
    /// a NULL inconvenient to format, so it is one statement with its own name.
    private func clearUnknownUsage(_ db: OpaquePointer, key: String) {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            UPDATE usage_intervals SET
              input_new = NULL, output = NULL, cache_read = NULL, cache_write = NULL, total = NULL
            WHERE interval_key = ?
              AND coverage = 'source_missing'
              AND COALESCE(input_new, 0) = 0 AND COALESCE(output, 0) = 0
              AND COALESCE(cache_read, 0) = 0 AND COALESCE(cache_write, 0) = 0;
            """, -1, &statement, nil) == SQLITE_OK else { return }
        bind(statement, 1, key)
        sqlite3_step(statement)
    }

    /// Seal every open segment of a session, for a source that has gone. The row stays, its usage
    /// is NULL, and nothing downstream may render it as zero.
    func sealSession(assistant: Assistant, sessionID: String, coverage: Coverage,
                     reason: String? = nil, at: Date = Date()) {
        queue.async { [weak self] in
            guard let self, let db = self.database() else { return }
            guard let current = self.cursor(db, assistant: assistant.rawValue,
                                            session: sessionID),
                  let key = current.openKey else { return }
            if let reason { self.note(db, key: key, coverageReason: reason) }
            self.sealRow(db, key: key, coverage: coverage, at: at)
        }
    }

    // MARK: - Corrections

    /// A measurement that disagrees with a sealed row. Written as new metadata, because a value
    /// that may already have appeared in a month's total is never silently rewritten.
    func recordCorrection(intervalKey: String, reason: String, proposed: [String: Any]) {
        queue.sync { correction(intervalKey: intervalKey, reason: reason, proposed: proposed) }
    }

    private func correction(intervalKey: String, reason: String, proposed: [String: Any]) {
        guard let db = database() else { return }
        let was = row(db, key: intervalKey).map { row -> [String: Any] in
            ["input_new": row.counts.inputNew as Any? ?? NSNull(),
             "output": row.counts.output as Any? ?? NSNull(),
             "cache_read": row.counts.cacheRead as Any? ?? NSNull(),
             "cache_write": row.counts.cacheWrite as Any? ?? NSNull(),
             "cost_value": row.costValue as Any? ?? NSNull()]
        }
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            INSERT INTO usage_corrections (interval_key, reason, was, proposed, written_at)
            VALUES (?,?,?,?,?);
            """, -1, &statement, nil) == SQLITE_OK else { return }
        func json(_ object: [String: Any]?) -> String? {
            guard let object,
                  let data = try? JSONSerialization.data(withJSONObject: object,
                                                         options: [.sortedKeys])
            else { return nil }
            return String(decoding: data, as: UTF8.self)
        }
        bind(statement, 1, intervalKey)
        bind(statement, 2, reason)
        bind(statement, 3, json(was))
        bind(statement, 4, json(proposed))
        sqlite3_bind_double(statement, 5, Date().timeIntervalSince1970)
        sqlite3_step(statement)
    }

    func correctionCount(from: String? = nil, to: String? = nil) -> Int {
        queue.sync {
            guard let db = database() else { return 0 }
            var statement: OpaquePointer?
            defer { sqlite3_finalize(statement) }
            let sql = """
                SELECT COUNT(*) FROM usage_corrections c
                  JOIN usage_intervals i ON i.interval_key = c.interval_key
                 WHERE (? IS NULL OR i.local_day >= ?) AND (? IS NULL OR i.local_day <= ?);
                """
            guard sqlite3_prepare_v2(db, sql, -1, &statement, nil) == SQLITE_OK else { return 0 }
            bind(statement, 1, from); bind(statement, 2, from)
            bind(statement, 3, to); bind(statement, 4, to)
            guard sqlite3_step(statement) == SQLITE_ROW else { return 0 }
            return Int(sqlite3_column_int64(statement, 0))
        }
    }

    // MARK: - Reading rows back

    private static let selection = """
        SELECT interval_key, assistant, session_id, boundary_kind, boundary_id, segment_no,
               segment_reason, origin, task_id, schedule_id, project_key, working_dir, kind_raw,
               isolation, depth, claim_count, timeout_seconds, task_state, model,
               reasoning_effort, billing_mode, usage_raw, input_new, output, cache_read,
               cache_write, total, source_total, reconciliation, cost_value, cost_unit,
               cost_basis, price_snapshot_id, missing_reason, coverage, coverage_reason, sealed,
               source_bytes, started_at, ended_at, local_day, updated_at
          FROM usage_intervals
        """

    private static func row(from statement: OpaquePointer?) -> Row {
        var row = Row()
        row.intervalKey = text(statement, 0) ?? ""
        row.assistant = text(statement, 1) ?? ""
        row.sessionID = text(statement, 2) ?? ""
        row.boundaryKind = text(statement, 3) ?? ""
        row.boundaryID = text(statement, 4) ?? ""
        row.segmentNo = integer(statement, 5) ?? 0
        row.segmentReason = text(statement, 6) ?? ""
        row.origin = text(statement, 7) ?? ""
        row.taskID = text(statement, 8)
        row.scheduleID = text(statement, 9)
        row.projectKey = text(statement, 10)
        row.workingDir = text(statement, 11)
        row.kindRaw = text(statement, 12)
        row.isolation = text(statement, 13)
        row.depth = integer(statement, 14)
        row.claimCount = integer(statement, 15)
        row.timeoutSeconds = integer(statement, 16)
        row.taskState = text(statement, 17)
        row.model = text(statement, 18)
        row.reasoningEffort = text(statement, 19)
        row.billingMode = text(statement, 20) ?? ""
        row.rawUsage = text(statement, 21)
        row.counts = Counts(inputNew: integer(statement, 22), output: integer(statement, 23),
                            cacheRead: integer(statement, 24), cacheWrite: integer(statement, 25))
        row.total = integer(statement, 26)
        row.sourceTotal = integer(statement, 27)
        row.reconciliation = text(statement, 28)
        row.costValue = double(statement, 29)
        row.costUnit = text(statement, 30)
        row.costBasis = text(statement, 31) ?? ""
        row.priceSnapshotID = text(statement, 32)
        row.missingReason = text(statement, 33)
        row.coverage = text(statement, 34) ?? ""
        row.coverageReason = text(statement, 35)
        row.sealed = (integer(statement, 36) ?? 0) == 1
        row.sourceBytes = integer(statement, 37)
        row.startedAt = Date(timeIntervalSince1970: double(statement, 38) ?? 0)
        row.endedAt = double(statement, 39).map { Date(timeIntervalSince1970: $0) }
        row.localDay = text(statement, 40) ?? ""
        row.updatedAt = Date(timeIntervalSince1970: double(statement, 41) ?? 0)
        return row
    }

    private func row(_ db: OpaquePointer, key: String) -> Row? {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, Self.selection + " WHERE interval_key = ?;", -1,
                                 &statement, nil) == SQLITE_OK else { return nil }
        bind(statement, 1, key)
        guard sqlite3_step(statement) == SQLITE_ROW else { return nil }
        return Self.row(from: statement)
    }

    func row(intervalKey: String) -> Row? {
        queue.sync { database().flatMap { row($0, key: intervalKey) } }
    }

    /// Every row a task produced, newest segment last. The per-row reconciliation check uses
    /// this: a fixed task id on every surface, never one aggregate against another.
    func rows(taskID: String) -> [Row] {
        queue.sync {
            guard let db = database() else { return [] }
            var statement: OpaquePointer?
            defer { sqlite3_finalize(statement) }
            guard sqlite3_prepare_v2(db, Self.selection
                                     + " WHERE task_id = ? ORDER BY segment_no;",
                                     -1, &statement, nil) == SQLITE_OK else { return [] }
            bind(statement, 1, taskID)
            var out: [Row] = []
            while sqlite3_step(statement) == SQLITE_ROW { out.append(Self.row(from: statement)) }
            return out
        }
    }

    /// Rows inside a local-day range. **Rows whose usage is unknown come first**, because the
    /// sessions most likely to go missing are the long ones and a reader who never scrolls would
    /// otherwise never see the bias in the totals below them.
    func rows(from: String? = nil, to: String? = nil) -> [Row] {
        queue.sync {
            guard let db = database() else { return [] }
            var statement: OpaquePointer?
            defer { sqlite3_finalize(statement) }
            guard sqlite3_prepare_v2(db, Self.selection + """
                 WHERE (? IS NULL OR local_day >= ?) AND (? IS NULL OR local_day <= ?)
                 ORDER BY (input_new IS NULL AND output IS NULL AND cache_read IS NULL
                           AND cache_write IS NULL) DESC,
                          local_day, started_at, segment_no;
                """, -1, &statement, nil) == SQLITE_OK else { return [] }
            bind(statement, 1, from); bind(statement, 2, from)
            bind(statement, 3, to); bind(statement, 4, to)
            var out: [Row] = []
            while sqlite3_step(statement) == SQLITE_ROW { out.append(Self.row(from: statement)) }
            return out
        }
    }

    // MARK: - The aggregate

    enum GroupBy: String, CaseIterable {
        case model, assistant, origin, project, day, coverage, task
    }

    /// One group's answer. **`tokens` and `cost` are optional and stay nil when no row in the
    /// group carried one** — an aggregate that renders an unknown as `0` is the failure this
    /// whole store exists to stop, and it has happened once already: 1137M tokens, $0.00.
    struct Bucket: Equatable {
        var rows = 0
        var tokens: Counts?
        var total: Int?
        /// Rows counted in `rows` whose tokens are unknown and are therefore not in `tokens`.
        var tokenRowsUnknown = 0
        /// Money summed **per unit**, never across them. Codex rows can only ever carry credits
        /// or nothing, and adding credits to dollars would be a number with no meaning.
        var costByUnit: [String: Double] = [:]
        var costRows = 0
        var costBases: [String: Int] = [:]
        var unpricedRows = 0
        var missingReasons: [String: Int] = [:]
        var coverage: [String: Int] = [:]

        mutating func add(_ row: Row) {
            rows += 1
            coverage[row.coverage, default: 0] += 1
            if row.usageUnknown {
                tokenRowsUnknown += 1
            } else {
                var counts = tokens ?? Counts(inputNew: 0, output: 0, cacheRead: 0, cacheWrite: 0)
                counts.inputNew = (counts.inputNew ?? 0) + (row.counts.inputNew ?? 0)
                counts.output = (counts.output ?? 0) + (row.counts.output ?? 0)
                counts.cacheRead = (counts.cacheRead ?? 0) + (row.counts.cacheRead ?? 0)
                counts.cacheWrite = (counts.cacheWrite ?? 0) + (row.counts.cacheWrite ?? 0)
                tokens = counts
                total = (total ?? 0) + (row.total ?? row.counts.total ?? 0)
            }
            if let cost = row.costValue {
                costByUnit[row.costUnit ?? CostUnit.usd.rawValue, default: 0] += cost
                costRows += 1
                costBases[row.costBasis, default: 0] += 1
            } else {
                unpricedRows += 1
                missingReasons[row.missingReason ?? MissingCost.noCostRecorded.rawValue,
                               default: 0] += 1
            }
        }
    }

    struct Aggregate {
        var from: String?
        var to: String?
        var groupBy = GroupBy.model
        /// Keys in report order. A nil key is a column the rows genuinely do not carry, and it
        /// stays nil rather than becoming a word that looks like a value.
        var groups: [(key: String?, bucket: Bucket)] = []
        var totals = Bucket()
        var corrections = 0
    }

    func aggregate(from: String? = nil, to: String? = nil, groupBy: GroupBy = .model) -> Aggregate {
        var out = Aggregate(from: from, to: to, groupBy: groupBy)
        var buckets: [String?: Bucket] = [:]
        var order: [String?] = []
        for row in rows(from: from, to: to) {
            let key: String?
            switch groupBy {
            case .model: key = row.model
            case .assistant: key = row.assistant
            case .origin: key = row.origin
            case .project: key = row.projectKey ?? row.workingDir
            case .day: key = row.localDay
            case .coverage: key = row.coverage
            case .task: key = row.taskID
            }
            if buckets[key] == nil { buckets[key] = Bucket(); order.append(key) }
            buckets[key]?.add(row)
            out.totals.add(row)
        }
        // Unknown-usage first, then the biggest spenders. The first half of that ordering is the
        // point: a coverage gap that sorts last is a coverage gap nobody reads.
        out.groups = order.map { (key: $0, bucket: buckets[$0] ?? Bucket()) }
            .sorted { left, right in
                let leftBlind = left.bucket.tokenRowsUnknown > 0
                let rightBlind = right.bucket.tokenRowsUnknown > 0
                if leftBlind != rightBlind { return leftBlind }
                return (left.bucket.total ?? 0) > (right.bucket.total ?? 0)
            }
        out.corrections = correctionCount(from: from, to: to)
        return out
    }

    // MARK: - The aggregate, on the wire

    /// One group as JSON. **`tokens`, `total` and `cost` are `null` rather than `0` when nothing
    /// in the group carried one**, and the counts that say how many rows were left out sit beside
    /// them rather than in a footnote — an aggregate that quietly excludes rows is the same
    /// mistake as one that sums unknowns as zero, only harder to see.
    static func payload(of bucket: Bucket) -> [String: Any] {
        var tokens: Any = NSNull()
        if let counts = bucket.tokens {
            tokens = ["inputNew": counts.inputNew as Any? ?? NSNull(),
                      "output": counts.output as Any? ?? NSNull(),
                      "cacheRead": counts.cacheRead as Any? ?? NSNull(),
                      "cacheWrite": counts.cacheWrite as Any? ?? NSNull()]
        }
        var cost: Any = NSNull()
        if !bucket.costByUnit.isEmpty {
            cost = ["byUnit": bucket.costByUnit, "rows": bucket.costRows,
                    "bases": bucket.costBases]
        }
        return ["rows": bucket.rows,
                "tokens": tokens,
                "total": bucket.total as Any? ?? NSNull(),
                "tokenRowsUnknown": bucket.tokenRowsUnknown,
                "cost": cost,
                "unpriced": ["rows": bucket.unpricedRows, "reasons": bucket.missingReasons],
                "coverage": bucket.coverage]
    }

    static func payload(of aggregate: Aggregate) -> [String: Any] {
        let groups = aggregate.groups.map { group -> [String: Any] in
            var out = payload(of: group.bucket)
            out["key"] = group.key as Any? ?? NSNull()
            return out
        }
        return [
            "range": ["from": aggregate.from as Any? ?? NSNull(),
                      "to": aggregate.to as Any? ?? NSNull(),
                      "timezone": TimeZone.autoupdatingCurrent.identifier],
            "groupBy": aggregate.groupBy.rawValue,
            "groups": groups,
            "totals": payload(of: aggregate.totals),
            "corrections": aggregate.corrections,
            "schemaVersion": schemaVersion,
            "priceSnapshotId": priceSnapshotID,
            // Named rather than omitted. A consumer that cannot see which columns are reserved
            // will assume the view it is given is the whole tree.
            "unavailable": ["columns": reservedColumns, "why": reservedColumnsReason],
        ]
    }

    // MARK: - The export

    static let exportColumns = [
        "interval_key", "local_day", "assistant", "session_id", "boundary_kind", "boundary_id",
        "segment_no", "segment_reason", "origin", "task_id", "schedule_id", "project_key",
        "working_dir", "kind_raw", "isolation", "depth", "claim_count", "timeout_seconds",
        "task_state", "model", "reasoning_effort", "billing_mode", "input_new", "output",
        "cache_read", "cache_write", "total", "source_total", "reconciliation", "cost_value",
        "cost_unit", "cost_basis", "price_snapshot_id", "missing_reason", "coverage",
        "coverage_reason", "sealed", "started_at", "ended_at",
    ] + reservedColumns

    /// The whole range as CSV. **An unknown is an empty field, never `0`** — including every
    /// reserved column, which is empty in every row of schema 1 and is present so that a reader
    /// can see it is reserved rather than wonder where it went.
    func exportCSV(from: String? = nil, to: String? = nil) -> String {
        func escape(_ value: String?) -> String {
            guard let value else { return "" }
            guard value.contains(where: { $0 == "," || $0 == "\"" || $0 == "\n" || $0 == "\r" })
            else { return value }
            return "\"" + value.replacingOccurrences(of: "\"", with: "\"\"") + "\""
        }
        func number(_ value: Int?) -> String { value.map(String.init) ?? "" }
        var lines = [UsageLedger.exportColumns.joined(separator: ",")]
        let formatter = ISO8601DateFormatter()
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        for row in rows(from: from, to: to) {
            var fields: [String] = []
            fields.append(row.intervalKey)
            fields.append(row.localDay)
            fields.append(row.assistant)
            fields.append(row.sessionID)
            fields.append(row.boundaryKind)
            fields.append(row.boundaryID)
            fields.append(String(row.segmentNo))
            fields.append(row.segmentReason)
            fields.append(row.origin)
            fields.append(row.taskID ?? "")
            fields.append(row.scheduleID ?? "")
            fields.append(row.projectKey ?? "")
            fields.append(row.workingDir ?? "")
            fields.append(row.kindRaw ?? "")
            fields.append(row.isolation ?? "")
            fields.append(number(row.depth))
            fields.append(number(row.claimCount))
            fields.append(number(row.timeoutSeconds))
            fields.append(row.taskState ?? "")
            fields.append(row.model ?? "")
            fields.append(row.reasoningEffort ?? "")
            fields.append(row.billingMode)
            fields.append(number(row.counts.inputNew))
            fields.append(number(row.counts.output))
            fields.append(number(row.counts.cacheRead))
            fields.append(number(row.counts.cacheWrite))
            fields.append(number(row.total))
            fields.append(number(row.sourceTotal))
            fields.append(row.reconciliation ?? "")
            fields.append(row.costValue.map { String($0) } ?? "")
            fields.append(row.costUnit ?? "")
            fields.append(row.costBasis)
            fields.append(row.priceSnapshotID ?? "")
            fields.append(row.missingReason ?? "")
            fields.append(row.coverage)
            fields.append(row.coverageReason ?? "")
            fields.append(row.sealed ? "1" : "0")
            fields.append(formatter.string(from: row.startedAt))
            fields.append(row.endedAt.map { formatter.string(from: $0) } ?? "")
            for _ in UsageLedger.reservedColumns { fields.append("") }
            lines.append(fields.map(escape).joined(separator: ","))
        }
        return lines.joined(separator: "\n") + "\n"
    }

    // MARK: - Task records in, rows out

    /// Import task records in the registry's own spelling — `cache_read`, `cost_usd`.
    ///
    /// This is the whole of the dispatch collector and the whole of the backfill, deliberately:
    /// the registry keeps 200 rows and this is what rescues them before eviction, and running it
    /// again is free because a record that has already been attributed produces a delta of zero.
    /// The same function reads the archived registry snapshot, which is the copy that does not
    /// expire.
    ///
    /// A task's stored `usage` is **the whole session's** counters, not that task's slice — both
    /// `claudeUsage` and `codexUsage` measure a transcript from the top. That is exactly why this
    /// goes through the cursor rather than writing the number down: an attached follow-up is then
    /// charged its own increment, and a task that ran inside a watched session is not counted
    /// twice.
    @discardableResult
    func importTaskRecords(_ records: [[String: Any]], now: Date = Date()) -> Int {
        queue.sync {
            var written = 0
            for record in records where importOnQueue(record, now: now) { written += 1 }
            return written
        }
    }

    @discardableResult
    func importTaskRecord(_ record: [String: Any], now: Date = Date()) -> Bool {
        queue.sync { importOnQueue(record, now: now) }
    }

    /// The finalize collector's door. Asynchronous because a task ending is a main-thread moment
    /// and this is a local file write; the row is needed before the task directory is swept
    /// twenty-four hours later, not before `finalize` returns.
    func collect(taskRecord record: [String: Any]) {
        queue.async { [weak self] in _ = self?.importOnQueue(record, now: Date()) }
    }

    @discardableResult
    private func importOnQueue(_ record: [String: Any], now: Date = Date()) -> Bool {
        guard let id = record["id"] as? String, !id.isEmpty,
              let assistant = Assistant(rawValue: record["assistant"] as? String ?? "")
        else { return false }
        let state = record["state"] as? String
        let terminal = state.flatMap(Orchestrator.State.init(rawValue:))?.isTerminal ?? false
        let attached = (record["attach_session"] as? String).map { !$0.isEmpty } ?? false
        let origin: Origin
        if let schedule = record["schedule_id"] as? String, !schedule.isEmpty { origin = .schedule }
        else if attached { origin = .followUp }
        else { origin = .dispatch }

        // Whichever collector saw this task first decides its session identity, so the two can
        // never file the same work under two names and count it twice. Only when neither knows
        // does a task fall back to a synthetic id — and it says so, because dropping the row
        // would lose real evidence while pretending it is somebody else's session is worse.
        let named = (record["child_session"] as? String).flatMap { $0.isEmpty ? nil : $0 }
        let known = named ?? sessionID(forTask: id)
        var sample = Sample(assistant: assistant,
                            sessionID: known ?? "unresolved-session:\(id)",
                            boundaryKind: .task, boundaryID: id, origin: origin)
        if known == nil { sample.coverageReason = "session_unresolved" }
        sample.observedAt = (record["finished_at"] as? Double).map {
            Date(timeIntervalSince1970: $0)
        } ?? now
        sample.taskID = id
        sample.scheduleID = record["schedule_id"] as? String
        let worktree = record["worktree"] as? [String: Any]
        sample.projectKey = record["project_dir"] as? String
        sample.workingDir = (worktree?["cwd"] as? String) ?? (record["project_dir"] as? String)
        sample.kindRaw = record["kind"] as? String
        sample.isolation = (record["isolation"] as? String) ?? Orchestrator.Isolation.none.rawValue
        sample.depth = record["depth"] as? Int
        sample.claimCount = (record["claims"] as? [String])?.count
            ?? (record["claim_keys"] as? [String])?.count
        sample.timeoutSeconds = (record["timeout_minutes"] as? Int).map { $0 * 60 }
        sample.taskState = state
        sample.reasoningEffort = record["reasoning_effort"] as? String
        let usage = record["usage"] as? [String: Any]
        sample.rawUsage = usage
        sample.model = (usage?["model"] as? String) ?? (record["model"] as? String)
        // `Orchestrator.cost(of:)` is arithmetic on published per-million prices. Copying it
        // through with its basis named is the honest move; recomputing it here would be a second
        // opinion nobody asked for, and inventing one where the source has none is the failure
        // that produced a $0.00 month.
        sample.costBasis = .listPriceEstimate
        sample.costUnit = .usd
        sample.seal = terminal
        sample.sealCoverage = usage == nil ? .sourceMissing : .complete
        if usage == nil && terminal {
            sample.coverageReason = sample.coverageReason ?? "no_usage_recorded"
        }

        // A row that is already sealed and disagrees with this reading is a correction, never a
        // rewrite: the earlier number may already have been quoted in a month's total.
        let key = UsageLedger.intervalKey(assistant: assistant, sessionID: sample.sessionID,
                                          boundaryKind: .task, boundaryID: id, segmentNo: 0)
        if let db = database(), let existing = row(db, key: key), existing.sealed {
            guard let usage,
                  let data = try? JSONSerialization.data(withJSONObject: usage,
                                                         options: [.sortedKeys]),
                  String(decoding: data, as: UTF8.self) != (existing.rawUsage ?? "")
            else { return false }
            correction(intervalKey: key, reason: "source_changed_after_seal", proposed: usage)
            return true
        }
        return apply(sample) != nil
    }

    /// The session a task's rows are already filed under, if anything has filed one.
    private func sessionID(forTask id: String) -> String? {
        guard let db = database() else { return nil }
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            SELECT session_id FROM usage_intervals
             WHERE task_id = ? AND session_id NOT LIKE 'unresolved-session:%'
             ORDER BY started_at LIMIT 1;
            """, -1, &statement, nil) == SQLITE_OK else { return nil }
        bind(statement, 1, id)
        guard sqlite3_step(statement) == SQLITE_ROW else { return nil }
        return Self.text(statement, 0)
    }

    // MARK: - Test seams

    /// Close the handle so a test can point the store somewhere else, or delete it.
    func closeForTesting() {
        queue.sync {
            if let handle { sqlite3_close(handle) }
            handle = nil
            openedFor = nil
        }
        UsageLedger.forgetWatchedForTesting()
    }
}

// MARK: - The hand-opened half

/// Collection for sessions nobody dispatched.
///
/// A ledger that only knew about dispatched tasks could not honestly claim to be what Clawdline
/// sees, and the sessions a person opens themselves are where most of a day goes. This rides on
/// the reading ``SessionWatch`` already takes: the transcripts are on disk, so it adds no
/// terminal round trip, and it is throttled to one checkpoint per session every five minutes
/// because that reading happens every 1.2 seconds while the panel is up.
extension UsageLedger {

    /// Force a checkpoint at most this often per session. The spec's five-to-ten minutes; nothing
    /// is lost by waiting, because every measurement is cumulative and the next one catches up.
    static let checkpointInterval: TimeInterval = 300

    private struct Watched {
        var assistant: Assistant
        var sessionID: String
        var record: URL
        var at: Date
        var bytes: Int?
    }

    private static let watchLock = NSLock()
    private static var watchedSessions: [String: Watched] = [:]

    static func forgetWatchedForTesting() {
        watchLock.lock(); watchedSessions = [:]; watchLock.unlock()
    }

    /// The parsed struct, back in the registry's own spelling.
    ///
    /// The one place in the app that turns `Orchestrator.Usage` into a source object, and it uses
    /// the on-disk spelling deliberately: the collector that reads it is the same one that reads
    /// the registry, so the spelling both surfaces disagree about is exercised in production
    /// rather than only in a test.
    static func rawUsage(of usage: Orchestrator.Usage) -> [String: Any] {
        var out: [String: Any] = ["input": usage.input, "output": usage.output,
                                  "cache_read": usage.cacheRead, "cache_write": usage.cacheWrite,
                                  "total": usage.total]
        if let model = usage.model { out["model"] = model }
        if let cost = usage.costUsd { out["cost_usd"] = cost }
        return out
    }

    /// One checkpoint of every assistant session in a reading. Off the main thread; safe to call
    /// on every reading because of the throttle above.
    static func checkpoint(sessions: [TargetSession], now: Date = Date()) {
        for session in sessions where session.isAssistant {
            // Throttled on the terminal id before anything is opened. The identity lookup below
            // can cost a working-directory resolution, which is not a per-1.2-second price.
            watchLock.lock()
            let previous = watchedSessions[session.id]
            watchLock.unlock()
            if let previous, now.timeIntervalSince(previous.at) < checkpointInterval { continue }
            guard let record = Transcript.record(of: session),
                  let sessionID = Transcript.sessionID(in: record.url,
                                                       assistant: record.assistant)
            else { continue }
            let bytes = (try? FileManager.default
                .attributesOfItem(atPath: record.url.path)[.size] as? Int) ?? nil
            let unchanged = previous?.sessionID == sessionID && previous?.bytes != nil
                && previous?.bytes == bytes
            watchLock.lock()
            watchedSessions[session.id] = Watched(assistant: record.assistant,
                                                  sessionID: sessionID, record: record.url,
                                                  at: now, bytes: bytes)
            watchLock.unlock()
            if unchanged { continue }
            shared.observe(sample(assistant: record.assistant, sessionID: sessionID,
                                  record: record.url, terminalID: session.id, cwd: session.cwd,
                                  bytes: bytes, now: now))
        }
    }

    /// Sessions that were in the last reading and are not in this one. A process that has gone is
    /// one of the three forced checkpoints, and it is the one that decides whether a row is
    /// `complete` or `source_missing` — so the record is read once more before the seal.
    static func departed(_ terminalIDs: [String], now: Date = Date()) {
        var closing: [(String, Watched)] = []
        watchLock.lock()
        for id in terminalIDs {
            if let known = watchedSessions.removeValue(forKey: id) { closing.append((id, known)) }
        }
        watchLock.unlock()
        guard !closing.isEmpty else { return }
        // Called from the main thread, where the reading is applied. The last read of a
        // transcript is file I/O and belongs off it.
        DispatchQueue.global(qos: .utility).async {
            for (id, known) in closing {
                var final = sample(assistant: known.assistant, sessionID: known.sessionID,
                                   record: known.record, terminalID: id, cwd: nil, bytes: nil,
                                   now: now)
                final.seal = true
                final.sealCoverage = final.rawUsage == nil ? .sourceMissing : .complete
                if final.rawUsage == nil { final.coverageReason = "source_unreadable_at_close" }
                shared.observe(final)
            }
        }
    }

    /// One reading of one session, with the work it is inside resolved from the registry. A
    /// terminal this app opened for a task is filed under that task; everything else is `manual`,
    /// which is the honest word for a session a person started.
    private static func sample(assistant: Assistant, sessionID: String, record: URL,
                               terminalID: String, cwd: String?, bytes: Int?,
                               now: Date) -> Sample {
        let usage: Orchestrator.Usage? = assistant == .claude
            ? Orchestrator.claudeUsage(transcript: record)
            : Orchestrator.codexUsage(rollout: record)
        let task = Orchestrator.ledgerTaskRecord(forTerminal: terminalID)
        var sample: Sample
        if let task, let id = task["id"] as? String {
            let origin: Origin
            if let schedule = task["schedule_id"] as? String, !schedule.isEmpty {
                origin = .schedule
            } else if (task["attach_session"] as? String).map({ !$0.isEmpty }) == true {
                origin = .followUp
            } else {
                origin = .dispatch
            }
            sample = Sample(assistant: assistant, sessionID: sessionID, boundaryKind: .task,
                            boundaryID: id, origin: origin)
            sample.taskID = id
            sample.scheduleID = task["schedule_id"] as? String
            sample.kindRaw = task["kind"] as? String
            sample.isolation = (task["isolation"] as? String)
                ?? Orchestrator.Isolation.none.rawValue
            sample.depth = task["depth"] as? Int
            sample.claimCount = (task["claims"] as? [String])?.count
                ?? (task["claim_keys"] as? [String])?.count
            sample.timeoutSeconds = (task["timeout_minutes"] as? Int).map { $0 * 60 }
            sample.taskState = task["state"] as? String
            sample.reasoningEffort = task["reasoning_effort"] as? String
            sample.projectKey = task["project_dir"] as? String
            sample.workingDir = ((task["worktree"] as? [String: Any])?["cwd"] as? String)
                ?? (task["project_dir"] as? String) ?? cwd
        } else {
            sample = Sample(assistant: assistant, sessionID: sessionID, boundaryKind: .session,
                            boundaryID: sessionID, origin: .manual)
            sample.projectKey = cwd
            sample.workingDir = cwd
        }
        sample.observedAt = now
        sample.sourceBytes = bytes
        sample.rawUsage = usage.map(rawUsage(of:))
        sample.model = usage?.model ?? (task?["model"] as? String)
        sample.costBasis = .listPriceEstimate
        sample.costUnit = .usd
        return sample
    }
}
