import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

/// A byte-stable 1x1 PNG used after `dispatchMain()` without constructing AppKit state on a worker.
func onePixelPNG() -> Data? {
    Data(base64Encoded:
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScL0WQAAAABJRU5ErkJggg==")
}

// MARK: - Result


func mainQueueIdentityProbe() async
    -> (isMainThread: Bool, isOnMainQueue: Bool, recordsReturned: Bool, missingRecord: Bool) {
    await withCheckedContinuation { continuation in
        Thread.detachNewThread {
            Thread.sleep(forTimeInterval: 0.25)
            DispatchQueue.main.async {
                _ = Orchestrator.records()
                let missing = Orchestrator.record(id: "00000000-0000-4000-8000-000000000000")
                continuation.resume(returning: (
                    Thread.isMainThread, Orchestrator.isOnMainQueue, true, missing == nil
                ))
            }
        }
    }
}

func sessionImagePresentationCrossingProbe() async
    -> (rendered: Bool, observedSites: [String], hopsOverflowed: Bool,
        reentrantHops: [String]) {
    await withCheckedContinuation { continuation in
        Thread.detachNewThread {
            guard let data = onePixelPNG() else {
                continuation.resume(returning: (false, [], false, []))
                return
            }
            let artifact = SessionImageArtifact(
                id: "11111111-2222-4333-8444-555555555555", mediaType: "image/png",
                byteCount: data.count, width: 1, height: 1,
                expiresAt: Int(Date().addingTimeInterval(60).timeIntervalSince1970))
            MainQueue.forgetReentrantHopsForTesting()
            MainQueue.beginRecordingHopsForTesting()
            let rendered = SessionImagePresentation.exerciseQueueCrossingForTesting(
                artifact: artifact, data: data)
            let recorded = MainQueue.endRecordingHopsForTesting()
            continuation.resume(returning: (
                rendered, recorded.sites, recorded.overflowed,
                MainQueue.reentrantHopsForTesting
            ))
        }
    }
}

/// The crossings a reading with live targets in it leads to, asked from the one place they
/// can be wrong: a main-queue block after `dispatchMain()`, where the main thread is parked and
/// this block is running on a worker.
///
/// **This cannot be written as "call it and see whether it survives".** A `dispatch_sync` onto the
/// queue that already owns the calling thread traps inside libdispatch, and a trap is not a failing
/// check — it is a suite that stops printing, which is exactly why the same mistake in
/// ``Orchestrator/records()`` took a day to find. So the mistake is caught one step earlier, by
/// ``MainQueue/hop(from:alreadyOnMain:_:)``, which records the site and runs the work where it
/// already is. Reverting any caller's predicate to `Thread.isMainThread` turns a file-specific
/// check below red and leaves the process alive to report it.
///
/// **The site names are observed, not declared.** Each `exerciseQueueCrossingsForTesting` below
/// returns nothing; which sites crossed comes from ``MainQueue/endRecordingHopsForTesting()``,
/// which sees the hop itself. The earlier version returned a hard-coded list after doing the same
/// work, and a crossing deleted outright — `StartPoints.live` reading `SessionWatch.shared.targets`
/// straight from a worker — was still reported as having happened.
///
/// **The seams are read first, on purpose.** `resolveAttachment` and two of the RemoteServer
/// readers answer out of a fixture when their `…ForTesting` inventory is set, taking no crossing at
/// all. A group that forgot to clear one would leave this fixture proving nothing, quietly, so the
/// all four are named here and checked rather than assumed.
///
/// The targets come from ``SessionWatch``, filled by a real reading earlier in this file, because
/// an empty target list is the whole reason these crossings were dormant.
func sessionWatchCrossingProbe() async
    -> (isMainThread: Bool, isOnMainQueue: Bool, targets: Int, crossed: Int,
        seamsLeftSet: [String], observedSites: [String], hopsOverflowed: Bool,
        reentrantHops: [String]) {
    await withCheckedContinuation { continuation in
        Thread.detachNewThread {
            Thread.sleep(forTimeInterval: 0.25)
            DispatchQueue.main.async {
                MainQueue.forgetReentrantHopsForTesting()
                var seamsLeftSet: [String] = []
                if Orchestrator.attachmentInventoryForTesting != nil {
                    seamsLeftSet.append("Orchestrator.attachmentInventoryForTesting")
                }
                if RemoteServer.coordinatorSessionsForTesting != nil {
                    seamsLeftSet.append("RemoteServer.coordinatorSessionsForTesting")
                }
                if RemoteServer.coordinatorObservationUnavailableForTesting {
                    seamsLeftSet.append(
                        "RemoteServer.coordinatorObservationUnavailableForTesting")
                }
                if RemoteServer.sessionPayloadForTesting != nil {
                    seamsLeftSet.append("RemoteServer.sessionPayloadForTesting")
                }
                if RemoteServer.sessionConversationIDForTesting != nil {
                    seamsLeftSet.append("RemoteServer.sessionConversationIDForTesting")
                }
                if RemoteServer.sessionWorkIdentityForTesting != nil {
                    seamsLeftSet.append("RemoteServer.sessionWorkIdentityForTesting")
                }
                if RemoteServer.sessionEndForTesting != nil {
                    seamsLeftSet.append("RemoteServer.sessionEndForTesting")
                }
                let targets = SessionWatch.shared.targets
                var crossed = 0
                for target in targets {
                    _ = Config.shared.hookSessionID(of: target)
                    _ = Transcript.sessionID(of: target)
                    crossed += 1
                }
                MainQueue.beginRecordingHopsForTesting()
                if let target = targets.first {
                    StartPoints.exerciseQueueCrossingsForTesting()
                    RemoteIcon.exerciseQueueCrossingsForTesting(size: 37)
                    if let data = onePixelPNG() {
                        let artifact = SessionImageArtifact(
                            id: "11111111-2222-4333-8444-555555555555", mediaType: "image/png",
                            byteCount: data.count, width: 1, height: 1,
                            expiresAt: Int(Date().addingTimeInterval(60).timeIntervalSince1970))
                        _ = SessionImagePresentation.exerciseQueueCrossingForTesting(
                            artifact: artifact, data: data)
                    }
                    Orchestrator.exerciseQueueCrossingsForTesting(
                        sessionID: target.id, assistant: target.assistant ?? .codex)
                    RemoteServer.shared.exerciseQueueCrossingsForTesting(sessionID: target.id)
                }
                let recorded = MainQueue.endRecordingHopsForTesting()
                continuation.resume(returning: (
                    Thread.isMainThread, MainQueue.isCurrent, targets.count, crossed,
                    seamsLeftSet, recorded.sites, recorded.overflowed,
                    MainQueue.reentrantHopsForTesting
                ))
            }
        }
    }
}

// MARK: - The usage ledger

/// A store of its own per group, so one group's rows can never explain another's totals.
func freshUsageLedger() -> URL {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-usage-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    UsageLedger.shared.closeForTesting()
    UsageLedger.storeURLOverrideForTesting = directory.appendingPathComponent("usage.sqlite3")
    return directory
}

func forgetUsageLedger(_ directory: URL) {
    UsageLedger.shared.closeForTesting()
    UsageLedger.storeURLOverrideForTesting = isolatedTestStoreDirectory
        .appendingPathComponent("usage.sqlite3")
    try? FileManager.default.removeItem(at: directory)
}

func ledgerSample(_ assistant: Assistant, session: String,
                  boundary: UsageLedger.BoundaryKind = .session, id: String? = nil,
                  origin: UsageLedger.Origin = .manual,
                  usage: [String: Any]? = nil, model: String? = nil,
                  at: Date = Date()) -> UsageLedger.Sample {
    var sample = UsageLedger.Sample(assistant: assistant, sessionID: session,
                                    boundaryKind: boundary, boundaryID: id ?? session,
                                    origin: origin)
    sample.rawUsage = usage
    sample.model = model
    sample.observedAt = at
    return sample
}

















// A row the store marked reaches every reader still marked. One seam — `Row.measurement` — and
// three readers that all go through it, because the review found the same defect in three
// different places: an aggregate that coalesced a NULL part to `0` and then dropped the row out
// of its own total, a wire payload with no field a coverage reason could travel in, and an
// under-count that came back looking like a healthy session.






// The reader seam turned "a marked row reaches every reader marked" into a structural fact. The
// same defect then arrived from the writer's side: `coverage_reason` was one slot with a last
// writer, so a row that was true of two things reached every reader carrying one of them — and
// the mark that lost was always `source_regressed`, on exactly the rows whose number was measured
// across a replaced source. Both reachable paths are here, and neither had a guard.


/// Run SQL against a usage store directly, around the ledger rather than through it. Only the
/// migration group needs this: everywhere else, asking the store something through anything but
/// `Row.measurement` is the defect the seam exists to stop.
@discardableResult
func usageStoreExec(_ url: URL, _ sql: String) -> Bool {
    var db: OpaquePointer?
    guard sqlite3_open_v2(url.path, &db,
                          SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE, nil) == SQLITE_OK
    else { sqlite3_close(db); return false }
    defer { sqlite3_close(db) }
    var error: UnsafeMutablePointer<CChar>?
    let ok = sqlite3_exec(db, sql, nil, nil, &error) == SQLITE_OK
    if let error { print("      sqlite: \(String(cString: error))"); sqlite3_free(error) }
    return ok
}

/// The first column of the first row, as text. Enough to ask a store what shape it is in.
func usageStoreScalar(_ url: URL, _ sql: String) -> String? {
    var db: OpaquePointer?
    guard sqlite3_open_v2(url.path, &db, SQLITE_OPEN_READONLY, nil) == SQLITE_OK
    else { sqlite3_close(db); return nil }
    defer { sqlite3_close(db) }
    var statement: OpaquePointer?
    defer { sqlite3_finalize(statement) }
    guard sqlite3_prepare_v2(db, sql, -1, &statement, nil) == SQLITE_OK,
          sqlite3_step(statement) == SQLITE_ROW,
          sqlite3_column_type(statement, 0) != SQLITE_NULL,
          let raw = sqlite3_column_text(statement, 0) else { return nil }
    return String(cString: raw)
}

/// A store exactly as store version 1 wrote one: that migration's `CREATE` statements verbatim —
/// no `input_basis`, `coverage_reason` singular, no unique index on the corrections — and
/// `PRAGMA user_version = 1`.
///
/// **Nothing in this suite had ever built one.** Every other ledger test starts from an empty
/// file, which runs every branch of `migrate` in a single pass and therefore exercises the
/// creation path rather than the upgrade path: the `ALTER`s, the de-duplication and the rename
/// had zero coverage, on a store that already exists on this machine and holds the only copy of
/// evidence nothing else keeps.
func writeVersionOneUsageStore(at url: URL, today: String) {
    usageStoreExec(url, """
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
        CREATE INDEX IF NOT EXISTS usage_intervals_day ON usage_intervals (local_day);
        CREATE INDEX IF NOT EXISTS usage_intervals_task ON usage_intervals (task_id);
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
        PRAGMA user_version=1;
        """)

    // One row a v1 store measured and marked, and one it sealed with nothing to measure. The
    // keys are written out rather than computed so that a change to the key recipe fails here
    // instead of orphaning every row on the machine and looking like an empty month.
    usageStoreExec(url, """
        INSERT INTO usage_intervals
          (interval_key, schema_version, assistant, session_id, boundary_kind, boundary_id,
           segment_no, segment_reason, origin, task_id, model, billing_mode,
           input_new, output, cache_read, cache_write, total,
           cost_basis, coverage, coverage_reason, sealed,
           started_at, local_day, observed_at, updated_at)
        VALUES
          ('\(versionOneMarkedKey)', 1, 'claude', 'sess-v1', 'task', 'task-v1', 0, 'start',
           'dispatch', 'task-v1', 'claude-opus-5', 'unknown',
           10, 10, 980, 0, 1000,
           'unknown', 'partial', 'source_regressed', 0,
           1787000000.0, '\(today)', 1787000000.0, 1787000000.0),
          ('\(versionOneUnknownKey)', 1, 'claude', 'sess-v1-gone', 'task', 'task-v1-gone', 0,
           'start', 'dispatch', 'task-v1-gone', 'claude-opus-5', 'unknown',
           NULL, NULL, NULL, NULL, NULL,
           'unknown', 'source_missing', NULL, 1,
           1787000000.0, '\(today)', 1787000000.0, 1787000000.0);

        INSERT INTO usage_cursors
          (assistant, session_id, attributed_input, attributed_output, attributed_cache_read,
           attributed_cache_write, open_key, boundary_kind, boundary_id, segment_no, model,
           local_day, updated_at)
        VALUES ('claude', 'sess-v1', 10, 10, 980, 0, '\(versionOneMarkedKey)', 'task',
                'task-v1', 0, 'claude-opus-5', '\(today)', 1787000000.0);

        INSERT INTO usage_corrections (interval_key, reason, was, proposed, written_at) VALUES
          ('\(versionOneMarkedKey)', 'source_changed_after_seal', NULL, '{"a":1}', 1787000001.0),
          ('\(versionOneMarkedKey)', 'source_changed_after_seal', NULL, '{"a":1}', 1787000002.0),
          ('\(versionOneMarkedKey)', 'source_changed_after_seal', NULL, '{"a":1}', 1787000003.0),
          ('\(versionOneMarkedKey)', 'source_changed_after_seal', NULL, '{"a":2}', 1787000004.0),
          ('\(versionOneMarkedKey)', 'source_changed_after_seal', NULL, NULL, 1787000005.0),
          ('\(versionOneMarkedKey)', 'source_changed_after_seal', NULL, NULL, 1787000006.0);
        """)
}

/// `SHA256(claude, sess-v1, task, task-v1, 0, 1)`, written down. See `writeVersionOneUsageStore`.
let versionOneMarkedKey = "17bf4de916dc66318782e55de585df82064f02e4af31b7f7de50c85b1edaa362"
let versionOneUnknownKey = "6f1e1418f8240a682ef82431d9437a18404f6b0ee7ab5cf953f7f495a7dfffa5"

// A migration is the one piece of this store that runs against data nobody can replace. The
// review that asked for this had to build a version-1 store by hand to check it at all.


// The backfill runs over the whole registry on every launch, and `./build.sh` closes and reopens
// the app. So "running it again is free" is not a nicety about a startup path: anything it does
// twice, it does once per launch for as long as the row survives.






// The half of the ledger that watches sessions nobody dispatched. Its two decisions bound what
// the store costs and what it can miss, and its close is the moment that decides whether a row
// is `complete` or `source_missing` — and none of it was covered.

func runUsageLedgerTests() {
group("a settings tab taller than the screen is capped, and the overflow goes to the scroller") {
    // The window has no resize control and is pinned by its title bar, so a height past the
    // screen's is not merely awkward — those rows cannot be reached by any means. The device
    // list and the schedule list both got there.
    let chrome: CGFloat = 46 + 22 + 24 + 40      // strip, its gap, the gap under the pane, footer

    let fits = SettingsWindow.contentFit(natural: 600, ceiling: 900, chrome: chrome)
    check("a tab that fits still sizes the window to itself", fits.height == 600,
          "got \(fits.height)")
    check("and the scroller is exactly the pane, so no scroller shows",
          fits.viewport == 600 - chrome, "got \(fits.viewport)")

    let overflows = SettingsWindow.contentFit(natural: 2000, ceiling: 900, chrome: chrome)
    check("a tab taller than the screen stops at the screen", overflows.height == 900,
          "got \(overflows.height)")
    check("and the scroller gets the rest to scroll through",
          overflows.viewport == 900 - chrome, "got \(overflows.viewport)")

    let squeezed = SettingsWindow.contentFit(natural: 2000, ceiling: 60, chrome: chrome)
    check("a ceiling below the chrome itself never asks for a negative viewport",
          squeezed.viewport == 0, "got \(squeezed.viewport)")

    check("a live settings relayout keeps the reader at the same row",
          SettingsWindow.restoredScrollY(previous: 420, document: 1200, viewport: 600) == 420)
    check("a shrinking settings pane clamps the old position to its new bottom",
          SettingsWindow.restoredScrollY(previous: 700, document: 850, viewport: 600) == 250)
    check("a settings relayout never restores a negative scroll position",
          SettingsWindow.restoredScrollY(previous: -20, document: 850, viewport: 600) == 0)
}

group("the ledger reads every usage spelling and decides the shape by arithmetic") {
    // The same numbers, in the four spellings this machine has actually been observed to write.
    // The registry writes one on disk and the HTTP record writes another for the same row; a
    // collector that knows only one silently reads 0 for the field that is 96.6% of all tokens.
    let onDisk: [String: Any] = ["input": 10, "output": 20, "cache_read": 30,
                                 "cache_write": 40, "total": 100, "cost_usd": 5.469]
    let overHTTP: [String: Any] = ["input": 10, "output": 20, "cacheRead": 30,
                                   "cacheWrite": 40, "total": 100, "costUsd": 5.469]
    let claudeTranscript: [String: Any] = ["input_tokens": 10, "output_tokens": 20,
                                           "cache_read_input_tokens": 30,
                                           "cache_creation_input_tokens": 40]
    let disk = UsageLedger.normalize(raw: onDisk, assistant: .claude)
    let http = UsageLedger.normalize(raw: overHTTP, assistant: .claude)
    let transcript = UsageLedger.normalize(raw: claudeTranscript, assistant: .claude)
    expect("the disk spelling reads the cache", disk.counts.cacheRead, 30)
    expect("and the HTTP spelling reads the same one", http.counts, disk.counts)
    expect("and so does the transcript's", transcript.counts, disk.counts)
    expect("the recorded cost is copied, not recomputed", disk.cost, 5.469)
    expect("under either spelling", http.cost, 5.469)
    expect("the normalized parts sum back to the source's own total",
           disk.counts.total, disk.sourceTotal)

    // Codex's cumulative input *includes* its cached input, and its total is input + output.
    // Measured from a real rollout on this machine.
    let codex: [String: Any] = ["input": 8_190_546, "output": 16_956,
                                "cache_read": 7_978_752, "cache_write": 0,
                                "total": 8_207_502]
    let read = UsageLedger.normalize(raw: codex, assistant: .codex)
    expect("Codex cached input never lands in new input", read.counts.inputNew, 211_794)
    expect("and the cache read is kept whole", read.counts.cacheRead, 7_978_752)
    expect("and the normalized parts sum back to the total", read.counts.total, 8_207_502)
    check("nothing about that reading is unreconciled", read.reconciliation == nil,
          read.reconciliation ?? "")

    // The shape is decided by the arithmetic rather than by the assistant's name: the same
    // numbers under a total that says the parts are already disjoint keep the input they came
    // with, even when the writer is Codex.
    let disjointCodex: [String: Any] = ["input": 100, "output": 10, "cache_read": 30,
                                        "cache_write": 0, "total": 140]
    expect("a total that already excludes the cache leaves input alone",
           UsageLedger.normalize(raw: disjointCodex, assistant: .codex).counts.inputNew, 100)

    let nothing = UsageLedger.normalize(raw: ["model": "claude-opus-5"], assistant: .claude)
    check("an object with no usage keys yields no counts at all", nothing.counts.isEmpty)
    check("and no total invented from them", nothing.counts.total == nil)
    check("and no cost invented either", nothing.cost == nil)
    expect("and it says why", nothing.reconciliation, "no_recognised_usage_keys")

    let partial = UsageLedger.normalize(raw: ["input": 5, "output": 6], assistant: .claude)
    expect("a partial object keeps what it carried", partial.counts.inputNew, 5)
    check("and leaves what it did not as unknown", partial.counts.cacheRead == nil)
    check("and refuses a total it cannot compute", partial.counts.total == nil)

    expect("Codex has no per-session dollar figure in any login mode",
           UsageLedger.missingCostReason(assistant: .codex, model: "gpt-5.6-sol"), .planBilled)
    expect("a model with no published price says so",
           UsageLedger.missingCostReason(assistant: .claude, model: "gpt-5.6"), .noPriceForModel)
    expect("a source that did not name a model says that instead",
           UsageLedger.missingCostReason(assistant: .claude, model: nil), .unknownModel)
    expect("and a price-able model with no cost key is a fourth thing",
           UsageLedger.missingCostReason(assistant: .claude, model: "claude-opus-5"),
           .noCostRecorded)

    expect("the interval key is deterministic",
           UsageLedger.intervalKey(assistant: .claude, sessionID: "s", boundaryKind: .task,
                                   boundaryID: "t", segmentNo: 0),
           UsageLedger.intervalKey(assistant: .claude, sessionID: "s", boundaryKind: .task,
                                   boundaryID: "t", segmentNo: 0))
    var keys: Set<String> = []
    keys.insert(UsageLedger.intervalKey(assistant: .claude, sessionID: "s", boundaryKind: .task,
                                        boundaryID: "t", segmentNo: 0))
    keys.insert(UsageLedger.intervalKey(assistant: .codex, sessionID: "s", boundaryKind: .task,
                                        boundaryID: "t", segmentNo: 0))
    keys.insert(UsageLedger.intervalKey(assistant: .claude, sessionID: "s2", boundaryKind: .task,
                                        boundaryID: "t", segmentNo: 0))
    keys.insert(UsageLedger.intervalKey(assistant: .claude, sessionID: "s", boundaryKind: .session,
                                        boundaryID: "t", segmentNo: 0))
    keys.insert(UsageLedger.intervalKey(assistant: .claude, sessionID: "s", boundaryKind: .task,
                                        boundaryID: "t2", segmentNo: 0))
    keys.insert(UsageLedger.intervalKey(assistant: .claude, sessionID: "s", boundaryKind: .task,
                                        boundaryID: "t", segmentNo: 1))
    keys.insert(UsageLedger.intervalKey(assistant: .claude, sessionID: "s", boundaryKind: .task,
                                        boundaryID: "t", segmentNo: 0, schemaVersion: 2))
    expect("and every part of it changes the key", keys.count, 7)

    check("a range bound must be a local day", UsageLedger.isLocalDay("2026-08-28")
            && !UsageLedger.isLocalDay("2026-8-28") && !UsageLedger.isLocalDay("yesterday")
            && !UsageLedger.isLocalDay("2026-13-01"))
}

group("re-reading a source never double-counts, and a boundary cuts a segment") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    func usage(_ input: Int, _ output: Int, _ cacheRead: Int, cost: Double? = nil)
        -> [String: Any] {
        var out: [String: Any] = ["input": input, "output": output, "cache_read": cacheRead,
                                  "cache_write": 0,
                                  "total": input + output + cacheRead]
        if let cost { out["cost_usd"] = cost }
        return out
    }

    let session = "session-a"
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: session,
                                               usage: usage(10, 5, 85, cost: 0.5),
                                               model: "claude-opus-5"))
    expect("the first reading of a session opens one row",
           UsageLedger.shared.rows().count, 1)
    expect("carrying what it measured", UsageLedger.shared.rows().first?.total, 100)

    // The same file, read again. Every measurement this store takes is a session cumulative, so
    // the second reading of an unchanged transcript must attribute nothing.
    UsageLedger.shared.observeNow(ledgerSample(.claude, session: session,
                                               usage: usage(10, 5, 85, cost: 0.5),
                                               model: "claude-opus-5"))
    expect("reading the same source again adds no row", UsageLedger.shared.rows().count, 1)
    expect("and adds no tokens", UsageLedger.shared.rows().first?.total, 100)
    expect("and adds no money", UsageLedger.shared.rows().first?.costValue, 0.5)

    UsageLedger.shared.observeNow(ledgerSample(.claude, session: session,
                                               usage: usage(20, 10, 120, cost: 0.9),
                                               model: "claude-opus-5"))
    expect("a grown source contributes only its increment",
           UsageLedger.shared.rows().first?.total, 150)
    expectClose("and only its increment of money",
                CGFloat(UsageLedger.shared.rows().first?.costValue ?? 0), 0.9, 0.0001)

    // A task begins inside the session somebody was already using. The session segment is sealed
    // where it stood; the task is charged its own increment and nothing before it.
    UsageLedger.shared.observeNow(ledgerSample(
        .claude, session: session, boundary: .task, id: "task-1", origin: .followUp,
        usage: usage(30, 20, 250, cost: 1.4), model: "claude-opus-5"))
    let rows = UsageLedger.shared.rows()
    let sessionRow = rows.first { $0.boundaryKind == "session" }
    let taskRow = rows.first { $0.boundaryKind == "task" }
    expect("the boundary cuts a second row", rows.count, 2)
    expect("the session keeps what it had spent", sessionRow?.total, 150)
    check("and is sealed at that number", sessionRow?.sealed == true)
    expect("and the task is charged only its own increment", taskRow?.total, 150)
    expect("which is where the whole session's spend still adds up",
           (sessionRow?.total ?? 0) + (taskRow?.total ?? 0), 300)
    expect("the follow-up says where it came from", taskRow?.origin, "follow_up")
    expect("and why it was cut", taskRow?.segmentReason, "boundary")

    // A model switch is its own boundary, so a per-model figure is reproducible.
    UsageLedger.shared.observeNow(ledgerSample(
        .claude, session: session, boundary: .task, id: "task-1", origin: .followUp,
        usage: usage(40, 30, 330, cost: 1.9), model: "claude-haiku-4-5"))
    let afterSwitch = UsageLedger.shared.rows().filter { $0.boundaryID == "task-1" }
    expect("a model switch produces a second segment", afterSwitch.count, 2)
    expect("the first priced under the model that spent it",
           afterSwitch.first { $0.segmentNo == 0 }?.model, "claude-opus-5")
    expect("the second under the one that took over",
           afterSwitch.first { $0.segmentNo == 1 }?.model, "claude-haiku-4-5")
    expect("and each carries the price snapshot its number was made under",
           afterSwitch.compactMap(\.priceSnapshotID).count, 2)
    expect("with the increment on the newer segment only",
           afterSwitch.first { $0.segmentNo == 1 }?.total, 100)
    expect("and the cut named", afterSwitch.first { $0.segmentNo == 1 }?.segmentReason,
           "model_switch")

    // A cumulative counter that goes backwards means the source was replaced, not that tokens
    // were refunded.
    UsageLedger.shared.observeNow(ledgerSample(
        .claude, session: session, boundary: .task, id: "task-1", origin: .followUp,
        usage: usage(1, 1, 1), model: "claude-haiku-4-5"))
    let afterRegression = UsageLedger.shared.rows().first { $0.segmentNo == 1 }
    expect("a source that shrank never subtracts", afterRegression?.total, 100)
    expect("and says what happened", afterRegression?.coverageReasons, ["source_regressed"])
}

group("a source that cannot be read is a state, and nothing renders it as zero") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    var missing = ledgerSample(.claude, session: "gone", boundary: .task, id: "task-gone",
                               origin: .dispatch, usage: nil, model: "claude-opus-5")
    missing.seal = true
    missing.sealCoverage = .sourceMissing
    missing.mark(.sourceUnreadableAtClose)
    UsageLedger.shared.observeNow(missing)

    let row = UsageLedger.shared.rows().first
    expect("a transcript that was never readable still leaves a row",
           UsageLedger.shared.rows().count, 1)
    expect("sealed as source_missing", row?.coverage, "source_missing")
    check("with unknown tokens left unknown", row?.counts.isEmpty == true)
    check("and no total standing in for them", row?.total == nil)
    check("which is what makes it sort to the top of a coverage view",
          row?.usageUnknown == true)

    // The line most likely to be quietly removed by somebody who finds a NULL inconvenient to
    // format. Asserted on every reader this slice ships.
    let csv = UsageLedger.shared.exportCSV()
    let header = csv.split(separator: "\n").first.map(String.init) ?? ""
    let body = csv.split(separator: "\n").dropFirst().first.map(String.init) ?? ""
    let columns = header.split(separator: ",", omittingEmptySubsequences: false).map(String.init)
    let values = body.split(separator: ",", omittingEmptySubsequences: false).map(String.init)
    func field(_ name: String) -> String? {
        guard let index = columns.firstIndex(of: name), index < values.count else { return nil }
        return values[index]
    }
    expect("the export leaves an unknown token count empty", field("total"), "")
    // The lower bound is a number and it is still not `0` here: nothing was measured, so the
    // quantity is unknown, and the aggregate leaves the same range's `total` nil for the same
    // reason. A `0` in this column would be the 1137M-tokens-$0.00 shape in a new field.
    expect("and the measured lower bound empty too, because nothing was measured",
           field("measured"), "")
    expect("and an unknown cache read empty", field("cache_read"), "")
    expect("never the zero that would read as a measurement", field("input_new"), "")
    expect("and it says which kind of unavailable the cost is",
           field("missing_reason"), "plan_billed" == field("missing_reason")
            ? field("missing_reason") : "no_cost_recorded")
    let reserved = ["graph_id", "parent_task_id", "retry_of", "attempt", "landing_state",
                    "disposition"]
    expect("the six lineage columns are exactly those", UsageLedger.lineageColumns, reserved)
    check("every one of them is present in the export and empty",
          reserved.allSatisfy { columns.contains($0) && field($0) == "" })

    // The other way a row reaches `source_missing`: it was seen, it had been counted at zero
    // because nothing had happened yet, and then the source went. Zeros already on the row are
    // not a measurement and must go back to NULL with it.
    let counted = ledgerSample(.claude, session: "empty", usage: ["input": 0, "output": 0,
                                                                 "cache_read": 0,
                                                                 "cache_write": 0, "total": 0],
                               model: "claude-opus-5")
    UsageLedger.shared.observeNow(counted)
    expect("a session that has spent nothing yet is still a row",
           UsageLedger.shared.rows().count, 2)
    check("counted at zero while it is readable",
          UsageLedger.shared.rows().first { $0.sessionID == "empty" }?.total == 0)
    UsageLedger.shared.sealSession(assistant: .claude, sessionID: "empty",
                                   coverage: .sourceMissing, reason: .sourceUnreadableAtClose)
    check("and unknown once the source has gone, never left reading as a measured zero",
          eventually {
              let sealed = UsageLedger.shared.rows().first { $0.sessionID == "empty" }
              return sealed?.coverage == "source_missing" && sealed?.total == nil
                  && sealed?.counts.isEmpty == true
          })

    let aggregate = UsageLedger.shared.aggregate(groupBy: .model)
    check("the aggregate refuses to draw tokens for a group that has none",
          aggregate.totals.tokens == nil && aggregate.totals.total == nil)
    expect("and reports how many rows it could not count", aggregate.totals.tokenRowsUnknown, 2)
    expect("beside the row count it does have", aggregate.totals.rows, 2)
    let payload = UsageLedger.payload(of: aggregate)
    let totals = payload["totals"] as? [String: Any]
    check("the route renders that as null rather than zero",
          totals?["tokens"] is NSNull && totals?["total"] is NSNull)
    expect("and names the columns it cannot answer for",
           (payload["unavailable"] as? [String: Any])?["columns"] as? [String],
           ["graph_id", "disposition"])
}

group("a cost is copied where it exists and never invented where it does not") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    let priced: [String: Any] = ["id": "task-priced", "assistant": "claude", "state": "success",
                                 "kind": "code", "project_dir": "/tmp/project",
                                 "timeout_minutes": 30, "depth": 1, "child_session": "sess-1",
                                 "claims": ["a.swift", "b.swift"],
                                 "usage": ["input": 2, "output": 3, "cache_read": 160_655,
                                           "cache_write": 5, "total": 160_665,
                                           "model": "claude-opus-5", "cost_usd": 5.469]]
    let unpriced: [String: Any] = ["id": "task-unpriced", "assistant": "codex",
                                   "state": "success", "kind": "code",
                                   "project_dir": "/tmp/project", "timeout_minutes": 30,
                                   "depth": 1, "child_session": "sess-2",
                                   "usage": ["input": 900_000, "output": 1_000,
                                             "cache_read": 800_000, "cache_write": 0,
                                             "total": 901_000, "model": "gpt-5.6-sol"]]
    expect("both records import", UsageLedger.shared.importTaskRecords([priced, unpriced]), 2)

    let pricedRow = UsageLedger.shared.rows(taskID: "task-priced").first
    expectClose("the recorded cost is copied through unchanged",
                CGFloat(pricedRow?.costValue ?? 0), 5.469, 0.0001)
    expect("stamped with what kind of number it is", pricedRow?.costBasis, "list_price_estimate")
    expect("and in what unit", pricedRow?.costUnit, "USD")
    expect("and under which price version", pricedRow?.priceSnapshotID,
           UsageLedger.priceSnapshotID)
    check("with nothing claiming it is missing", pricedRow?.missingReason == nil)
    expect("the columns describing the work are populated", pricedRow?.claimCount, 2)
    expect("including its kind", pricedRow?.kindRaw, "code")
    expect("and its origin", pricedRow?.origin, "dispatch")

    let unpricedRow = UsageLedger.shared.rows(taskID: "task-unpriced").first
    check("a source with no cost gets none invented", unpricedRow?.costValue == nil)
    expect("not from a price table", unpricedRow?.costBasis, "unknown")
    expect("and it names which kind of unavailable it is", unpricedRow?.missingReason,
           "plan_billed")
    expect("its tokens are still counted", unpricedRow?.total, 901_000)
    expect("with the cached input kept out of the new input", unpricedRow?.counts.inputNew,
           100_000)

    let aggregate = UsageLedger.shared.aggregate(groupBy: .assistant)
    expectClose("the total sums only the money that exists",
                CGFloat(aggregate.totals.costByUnit["USD"] ?? 0), 5.469, 0.0001)
    expect("and reports the rows it could not price beside it",
           aggregate.totals.unpricedRows, 1)
    expect("with the reason attached", aggregate.totals.missingReasons["plan_billed"], 1)
    let codex = aggregate.groups.first { $0.key == "codex" }?.bucket
    check("a group with no cost at all renders as absent, never as $0.00",
          codex?.costByUnit.isEmpty == true)
    check("which is what the wire says too",
          (UsageLedger.payload(of: codex ?? UsageLedger.Bucket())["cost"]) is NSNull)
}

/// One analytics row without going through a collector. Query tests hold the observed subject
/// still and vary only the reader: this is deliberately not evidence about collection.
func analyticsRow(_ key: String, at: Date, assistant: String = "claude",
                  model: String? = "claude-opus-5", project: String? = "/private/acme/widget",
                  counts: UsageLedger.Counts = .init(inputNew: 1, output: 2,
                                                     cacheRead: 3, cacheWrite: 4),
                  cost: Double? = nil, unit: String? = nil, basis: String = "unknown",
                  missing: String? = "no_cost_recorded",
                  coverage: String = "complete", reasons: [String] = []) -> UsageLedger.Row {
    var row = UsageLedger.Row()
    row.intervalKey = key
    row.assistant = assistant
    row.sessionID = "private-session-\(key)"
    row.boundaryKind = "task"
    row.boundaryID = "task-\(key)"
    row.taskID = "task-\(key)"
    row.origin = "dispatch"
    row.projectKey = project
    row.workingDir = project.map { $0 + "/checkout" }
    row.model = model
    row.counts = counts
    row.total = counts.total
    row.costValue = cost
    row.costUnit = unit
    row.costBasis = basis
    row.priceSnapshotID = cost == nil ? nil : UsageLedger.priceSnapshotID
    row.missingReason = missing
    row.coverage = coverage
    row.coverageReasons = reasons
    row.startedAt = at
    row.updatedAt = at.addingTimeInterval(30)
    row.localDay = UsageLedger.localDay(of: at)
    row.sealed = true
    return row
}

group("usage analytics keeps quantity semantics and privacy in one closed contract") {
    let at = ISO8601DateFormatter().date(from: "2026-11-01T05:30:00Z")!
    let rows = [
        analyticsRow("estimated", at: at, counts: .init(inputNew: 10, output: 20,
                                                        cacheRead: 30, cacheWrite: nil),
                     cost: 1.25, unit: "USD", basis: "list_price_estimate", missing: nil,
                     coverage: "partial", reasons: ["source_regressed"]),
        // 05:30Z and 06:30Z are both 01:30 on the fall-back day, with different offsets.
        analyticsRow("actual", at: at.addingTimeInterval(3_600), counts: .init(inputNew: 0,
                     output: 0, cacheRead: 0, cacheWrite: 0), cost: 2.5, unit: "USD",
                     basis: "provider_actual", missing: nil),
        analyticsRow("plan", at: at.addingTimeInterval(7_200), assistant: "codex",
                     model: "gpt-5.6-sol", counts: .init(), cost: nil, basis: "unknown",
                     missing: "plan_billed", coverage: "source_missing",
                     reasons: ["source_unreadable_at_close"]),
    ]
    let service = UsageQueryService(rows: { rows })
    let result = service.query(.init(from: "2026-11-01", to: "2026-11-01",
                                     timezoneID: "America/New_York", groupBy: .assistant,
                                     bucket: .day, limit: 50), now: at.addingTimeInterval(600))
    let payload = result.payload
    expect("the schema is explicit", payload["schemaVersion"] as? Int,
           UsageLedger.schemaVersion)
    expect("the requested timezone survives the response",
           (payload["range"] as? [String: Any])?["timezone"] as? String,
           "America/New_York")
    check("freshness, capabilities, price snapshot, coverage and unavailable dimensions travel",
          payload["freshness"] is [String: Any]
            && payload["capabilities"] is [String: Any]
            && payload["priceSnapshot"] is [String: Any]
            && payload["coverage"] is [String: Any]
            && payload["unavailableDimensions"] is [String: Any])

    let totals = payload["totals"] as? [String: Any]
    expect("a partial range publishes its measured floor", totals?["measuredFloor"] as? Int, 60)
    check("and its strict total is unknown rather than the same number or zero",
          totals?["strictTotal"] is NSNull)
    let costs = totals?["costs"] as? [[String: Any]] ?? []
    expect("cost stays in one series per unit and basis", costs.count, 2)
    check("the two USD bases are never added together",
          costs.contains { $0["unit"] as? String == "USD"
            && $0["basis"] as? String == "list_price_estimate"
            && $0["value"] as? Double == 1.25 }
            && costs.contains { $0["unit"] as? String == "USD"
                && $0["basis"] as? String == "provider_actual"
                && $0["value"] as? Double == 2.5 })
    expect("plan billing is unavailable rather than free",
           ((totals?["unavailableCost"] as? [String: Any])?["reasons"]
                as? [String: Int])?["plan_billed"], 1)
    let unknownOnly = UsageQueryService(rows: {
        [analyticsRow("unknown-only", at: at, counts: .init(), missing: "plan_billed")]
    }).query(.init(timezoneID: "UTC"), now: at)
    check("an entirely unknown quantity remains null rather than a zero",
          ((unknownOnly.payload["totals"] as? [String: Any])?["measuredFloor"]) is NSNull)

    var oldPriced = analyticsRow("old-price", at: at, cost: 0.004, unit: "USD",
                                 basis: "list_price_estimate", missing: nil)
    oldPriced.priceSnapshotID = "prices-observed-in-row"
    let snapshotPayload = UsageQueryService(rows: { [oldPriced] })
        .query(.init(timezoneID: "UTC"), now: at).payload
    let snapshot = snapshotPayload["priceSnapshot"] as? [String: Any]
    expect("the active price snapshot is named separately from observed data",
           snapshot?["activeId"] as? String, UsageLedger.priceSnapshotID)
    expect("the row's observed snapshot set is what the range reports",
           snapshot?["observedIds"] as? [String], ["prices-observed-in-row"])

    let trend = payload["trend"] as? [[String: Any]] ?? []
    expect("both repeated local hours land in one DST-safe day bucket", trend.count, 1)
    let firstTrend = trend.first? ["tokens"] as? [String: Any]
    expect("the trend exposes exactly the four mutually exclusive token parts",
           Set(firstTrend?.keys.map { $0 } ?? []),
           Set(["inputNew", "output", "cacheRead", "cacheWrite"]))
    let publicRows = payload["rows"] as? [[String: Any]] ?? []
    let actualRow = publicRows.first { $0["id"] as? String == "actual" }
    let actualTokens = actualRow?["tokens"] as? [String: Any]
    let actualCost = actualRow?["cost"] as? [String: Any]
    check("one Agent Work row carries four token parts and the complete cost identity",
          Set(actualTokens?.keys.map { $0 } ?? [])
            == Set(["inputNew", "output", "cacheRead", "cacheWrite"])
            && actualCost?["value"] as? Double == 2.5
            && actualCost?["unit"] as? String == "USD"
            && actualCost?["basis"] as? String == "provider_actual"
            && actualCost?["priceSnapshotId"] as? String == UsageLedger.priceSnapshotID)
    let json = String(decoding: (try? JSONSerialization.data(withJSONObject: publicRows,
                                                              options: [.sortedKeys])) ?? Data(),
                      as: UTF8.self)
    check("drill-down has no raw session, prompt or filesystem path",
          !json.contains("private-session") && !json.contains("/private/")
            && !json.contains("workingDir") && !json.contains("usageRaw"), json)
}

group("usage analytics validates filters and paginates a changing ledger stably") {
    let base = Date(timeIntervalSince1970: 1_800_000_000)
    var rows = (0..<5).map { analyticsRow("row-\($0)", at: base.addingTimeInterval(Double($0))) }
    let service = UsageQueryService(rows: { rows })
    let query = UsageQueryService.Query(timezoneID: "UTC", groupBy: .model,
                                        bucket: .day, limit: 2)
    let first = service.query(query, now: base.addingTimeInterval(20))
    expect("the first page is bounded", first.rows.map(\.intervalKey), ["row-4", "row-3"])
    check("a full page has an opaque continuation", first.nextCursor != nil)
    rows.append(analyticsRow("newer", at: base.addingTimeInterval(100)))
    var next = query
    next.cursor = first.nextCursor
    let second = service.query(next, now: base.addingTimeInterval(120))
    expect("a newer insertion does not duplicate or skip the continuation",
           second.rows.map(\.intervalKey), ["row-2", "row-1"])

    let tied = ["x", "z", "y"].map { analyticsRow($0, at: base) }
    let tiedService = UsageQueryService(rows: { tied })
    let tiedFirst = tiedService.query(query, now: base)
    var tiedNext = query
    tiedNext.cursor = tiedFirst.nextCursor
    expect("equal timestamps use interval id as a stable cursor tie-break",
           tiedService.query(tiedNext, now: base).rows.map(\.intervalKey), ["x"])

    for invalid in [
        ["timezone": "Mars/Olympus"], ["limit": "0"], ["limit": "201"],
        ["limit": "abc"], ["limit": "1e3"], ["limit": "50.5"],
        ["from": "2026-02-31"], ["to": "2025-04-31"],
        ["bucket": "quarter"], ["view": "graph"], ["mystery": "yes"],
    ] {
        check("an invalid closed query is rejected: \(invalid)",
              UsageQueryService.parse(invalid, repeatedKeys: []).error != nil)
    }
    check("repeated keys are refused instead of last-writer-wins",
          UsageQueryService.parse(["timezone": "UTC"], repeatedKeys: ["timezone"]).error != nil)
    check("real leap days round-trip as local calendar dates",
          UsageQueryService.parse(["from": "2024-02-29", "to": "2024-02-29",
                                   "timezone": "Asia/Taipei"], repeatedKeys: []).query != nil)
}

group("usage analytics uses one requested timezone for day grouping and trend") {
    let at = ISO8601DateFormatter().date(from: "2026-08-29T23:00:00Z")!
    var crossing = analyticsRow("zone-crossing", at: at)
    crossing.localDay = "2026-08-30" // what a Taipei writer stored
    let payload = UsageQueryService(rows: { [crossing] }).query(
        .init(from: "2026-08-29", to: "2026-08-29", timezoneID: "America/New_York",
              groupBy: .day, bucket: .day), now: at).payload
    expect("day breakdown follows the request timezone",
           (payload["breakdown"] as? [[String: Any]])?.first?["key"] as? String,
           "2026-08-29")
    expect("trend names that same requested-timezone day",
           (payload["trend"] as? [[String: Any]])?.first?["bucket"] as? String,
           "2026-08-29")
}


// MARK: - The local Feature classifier and its acceptance policy

/// One evidence item with every field it does not care about left out. The fixtures below give
/// every field a *different* value on purpose: on 2026-09-02 an assertion in this repository named
/// `every field survives` stayed green while three fields were dropped, because two timestamps in
/// its fixture happened to be equal. Values that coincide cannot tell two fields apart.
func featureEvidence(_ key: String, project: String? = nil, task: String? = nil,
                     durable: Bool = true, kind: String? = nil, title: String? = nil,
                     line: String? = nil, plan: String? = nil, schedule: String? = nil,
                     scheduleTitle: String? = nil, parent: String? = nil,
                     retryOf: String? = nil) -> UsageFeatureClassifier.Evidence {
    UsageFeatureClassifier.Evidence(
        intervalKey: key, projectKey: project, taskID: task, hasDurableTaskRecord: durable,
        taskKind: kind, taskTitle: title, declaredWorkLine: line, planHeadline: plan,
        scheduleID: schedule, scheduleTitle: scheduleTitle, parentTaskID: parent,
        retryOf: retryOf)
}

/// Three stored intervals: two tasks of one declared work line, and one task carrying a label
/// nobody else does. No two share an interval key, a task id, a session, a timestamp or any
/// token count.
func featureFixtureIntervals(at: Date) -> [String] {
    let rows: [(name: String, task: String, seconds: Double, project: String,
                counts: [String: Any])] = [
        ("ladder-alpha", "task-ladder-alpha", 0, "/private/acme/ladder",
         ["input": 3, "output": 11, "cache_read": 19, "cache_write": 29, "total": 62]),
        ("ladder-bravo", "task-ladder-bravo", 90, "/private/acme/ladder",
         ["input": 5, "output": 13, "cache_read": 23, "cache_write": 31, "total": 72]),
        ("solo-charlie", "task-solo-charlie", 210, "/private/acme/solo",
         ["input": 7, "output": 17, "cache_read": 37, "cache_write": 41, "total": 102]),
    ]
    return rows.map { row in
        var sample = ledgerSample(.claude, session: "feature-session-\(row.name)",
                                  boundary: .task, id: row.task, origin: .dispatch,
                                  usage: row.counts, model: "claude-opus-5",
                                  at: at.addingTimeInterval(row.seconds))
        sample.taskID = row.task
        sample.projectKey = row.project
        sample.depth = 1
        sample.seal = true
        return UsageLedger.shared.observeNow(sample)!
    }
}

/// The durable records behind those three intervals. Two carry one work line, the third carries a
/// label of its own — which is the difference between a Feature and a one-off task.
func featureFixtureFacts() -> [String: UsageLedger.TaskFacts] {
    ["task-ladder-alpha": UsageLedger.TaskFacts(
        title: "Ladder alpha title", declaredWorkLine: "Attribution ladder",
        planHeadline: "alpha plan headline", kind: "code"),
     "task-ladder-bravo": UsageLedger.TaskFacts(
        title: "Ladder bravo title", declaredWorkLine: "Attribution ladder",
        planHeadline: "bravo plan headline", kind: "code-review"),
     "task-solo-charlie": UsageLedger.TaskFacts(
        title: "Solo charlie title", declaredWorkLine: "Solitary sweep",
        planHeadline: "charlie plan headline", kind: "custom")]
}

/// The bytes a reader would render for one stored row. Compared before and after a classification
/// pass, this is what "the token row never moves" means: attribution is a second table, and
/// nothing on this path opens `usage_intervals` for writing at all.
func featureMeasurementBytes(_ row: UsageLedger.Row) -> Data {
    let measurement = row.measurement
    let payload: [String: Any] = [
        "inputNew": measurement.counts.inputNew as Any? ?? NSNull(),
        "output": measurement.counts.output as Any? ?? NSNull(),
        "cacheRead": measurement.counts.cacheRead as Any? ?? NSNull(),
        "cacheWrite": measurement.counts.cacheWrite as Any? ?? NSNull(),
        "total": measurement.total as Any? ?? NSNull(),
        "measured": measurement.measured,
        "unknownParts": measurement.unknownParts.map(\.rawValue),
        "reasons": measurement.reasons,
    ]
    return (try? JSONSerialization.data(withJSONObject: payload,
                                        options: [.sortedKeys])) ?? Data()
}

func featurePortfolio(_ payload: [String: Any]) -> [String: Any] {
    ((payload["portfolio"] as? [String: Any])?["features"] as? [String: Any]) ?? [:]
}

group("the local Feature classifier proposes only from durable evidence") {
    // One interval per rung, one per decline reason, and one pair that proves Feature identity is
    // scoped to a Project: the same label under two Project keys is two Features, not one.
    let evidence = [
        featureEvidence("iv-hint", project: "/private/acme/alpha", task: "task-hint",
                        kind: "code", title: "a title the hint outranks",
                        plan: "Feature: \u{201c}Ledger receipts\u{201d} for the whole range"),
        featureEvidence("iv-schedule", project: "/private/acme/bravo", task: "task-schedule",
                        kind: "custom", title: "Nightly title", schedule: "schedule-nightly",
                        scheduleTitle: "Nightly sweep"),
        featureEvidence("iv-line-one", project: "/private/acme/charlie", task: "task-line-one",
                        kind: "code-review", title: "Line one title", line: "Attribution ladder"),
        featureEvidence("iv-line-two", project: "/private/acme/charlie", task: "task-line-two",
                        kind: "docs", title: "Line two title", line: "attribution   LADDER"),
        featureEvidence("iv-lineage", project: "/private/acme/charlie", task: "task-lineage",
                        kind: "correction", title: "Lineage title", parent: "task-line-one"),
        featureEvidence("iv-delta-one", project: "/private/acme/delta", task: "task-delta-one",
                        kind: "research", title: "Delta one title", line: "Attribution ladder"),
        featureEvidence("iv-delta-two", project: "/private/acme/delta", task: "task-delta-two",
                        kind: "plan", title: "Delta two title", line: "Attribution ladder"),
        featureEvidence("iv-no-task", project: "/private/acme/echo", task: nil, durable: false),
        featureEvidence("iv-no-record", project: "/private/acme/foxtrot", task: "task-orphan",
                        durable: false, kind: "shell", title: "Orphan title"),
        featureEvidence("iv-solitary", project: "/private/acme/golf", task: "task-solitary",
                        kind: "review", title: "Solitary title", line: "Solitary sweep"),
        featureEvidence("iv-bare", project: "/private/acme/hotel", task: "task-bare",
                        kind: "chore", title: "A bare title with no Feature prefix"),
        // A plan headline that exists and names no Feature: the title is still read.
        featureEvidence("iv-title-hint", project: "/private/acme/kilo", task: "task-title-hint",
                        kind: "audit", title: "Feature: \u{300c}Title rung\u{300d}",
                        plan: "a plan headline that names no Feature"),
        // One work line, two spellings, and the lower-case one first — the opposite order to
        // charlie's pair above, so the stored label cannot be "whichever arrived first" in either
        // direction.
        featureEvidence("iv-india-one", project: "/private/acme/india", task: "task-india-one",
                        kind: "sweep", title: "India one title", line: "zebra   crossing"),
        featureEvidence("iv-india-two", project: "/private/acme/india", task: "task-india-two",
                        kind: "triage", title: "India two title", line: "Zebra Crossing"),
        // One task owning two rows, carrying one declared label. Rung 3 counts distinct *tasks*,
        // so this is a one-off however many intervals it spans. Everything else in this fixture
        // gives every field a different value; these two deliberately share a task, a title and a
        // label, because that sharing is the thing under test.
        featureEvidence("iv-twin-one", project: "/private/acme/juliet", task: "task-twin",
                        kind: "verify", title: "Twin title", line: "Twin rows"),
        featureEvidence("iv-twin-two", project: "/private/acme/juliet", task: "task-twin",
                        kind: "seal", title: "Twin title", line: "Twin rows"),
    ]
    let outcome = UsageFeatureClassifier.classify(evidence)
    var proposals: [String: UsageFeatureClassifier.Proposal] = [:]
    for proposal in outcome.proposals { proposals[proposal.intervalKey] = proposal }
    expect("every classified interval leaves as a proposal", outcome.proposals.count, 10)

    expect("an explicit Feature hint is the first rung", proposals["iv-hint"]?.rung,
           .explicitFeatureHint)
    expect("and it is the most confident one", proposals["iv-hint"]?.confidence, 0.95)
    expect("the hint's own words become the label, without either half of its quotation",
           proposals["iv-hint"]?.featureLabel, "Ledger receipts for the whole range")
    expect("a headline that names no Feature still lets the title name one",
           proposals["iv-title-hint"]?.rung, .explicitFeatureHint)
    expect("and that title's quotation is stripped in the same pair",
           proposals["iv-title-hint"]?.featureLabel, "Title rung")
    expect("a schedule identity is the second rung", proposals["iv-schedule"]?.rung,
           .scheduleIdentity)
    expect("at 0.88", proposals["iv-schedule"]?.confidence, 0.88)
    expect("labelled by the schedule's title rather than its id",
           proposals["iv-schedule"]?.featureLabel, "Nightly sweep")
    expect("a label two tasks carry is a declared work line", proposals["iv-line-one"]?.rung,
           .declaredWorkLine)
    expect("at 0.82", proposals["iv-line-one"]?.confidence, 0.82)
    expect("labelled as the root wrote it", proposals["iv-line-one"]?.featureLabel,
           "Attribution ladder")
    check("normalization merges spacing and case into one work line",
          proposals["iv-line-one"]?.featureID == proposals["iv-line-two"]?.featureID)
    expect("lineage is the last rung", proposals["iv-lineage"]?.rung, .lineage)
    expect("at 0.66", proposals["iv-lineage"]?.confidence, 0.66)
    check("and it inherits its parent's Feature verbatim rather than inventing one",
          proposals["iv-lineage"]?.featureID == proposals["iv-line-one"]?.featureID
            && proposals["iv-lineage"]?.featureLabel == proposals["iv-line-one"]?.featureLabel)

    check("normalization keeps one label for one work line, whichever spelling arrived first",
          proposals["iv-india-one"]?.featureLabel == "Zebra Crossing"
            && proposals["iv-india-two"]?.featureLabel == "Zebra Crossing"
            && proposals["iv-line-one"]?.featureLabel == proposals["iv-line-two"]?.featureLabel)

    check("every digest is 64 lowercase hex characters",
          outcome.proposals.allSatisfy { proposal in
              proposal.evidenceDigest.count == 64
                  && proposal.evidenceDigest.allSatisfy { character in
                      character.isNumber || ("a"..."f").contains(character)
                  }
          })
    // What the digest is over, asserted in both directions. `no two intervals share a digest`
    // used to stand here and could not fail: the interval key is a digest input and every fixture
    // key differs, so it held for any recipe whatsoever.
    let digestSubject = featureEvidence("iv-digest", project: "/private/acme/digest",
                                        task: "task-digest", kind: "code", title: "Digest title",
                                        plan: "Feature: Digest hint")
    var digestOtherTask = digestSubject
    digestOtherTask.taskID = "task-digest-other"
    var digestOtherKind = digestSubject
    digestOtherKind.taskKind = "a kind the hint rung never consumed"
    func digest(_ item: UsageFeatureClassifier.Evidence) -> String? {
        UsageFeatureClassifier.classify([item]).proposals.first?.evidenceDigest
    }
    check("a field the rung consumed changes the digest",
          digest(digestSubject) != nil && digest(digestSubject) != digest(digestOtherTask))
    check("and a field it did not consume leaves the digest alone",
          digest(digestSubject) == digest(digestOtherKind))

    // A golden vector, computed by an independent implementation of the recipe — SHA-256 over the
    // scope, the rung and the normalized grouping key, joined by U+001F — rather than by asking
    // this code what it thinks. Every other identity assertion here compares the classifier with
    // itself, so none of them can notice a change to `normalizedKey`, to the scope, to the
    // U+001F field order, to the 32/40-character prefixes or to the digest's own prefix string —
    // which is the exact set of changes that orphans every event already in a real ledger.
    let golden = UsageFeatureClassifier.classify([
        featureEvidence("iv-golden", project: "/private/acme/golden", task: "task-golden",
                        kind: "docs", title: "Golden title", plan: "Feature:  Golden   hint"),
    ]).proposals.first
    expect("the Feature id recipe is the documented one", golden?.featureID,
           "feature-4aef34c26a966067604adf70e9fd5ba6")
    expect("so is the evidence digest recipe", golden?.evidenceDigest,
           "c82e12b250984aaacdd35c3201bccf9d0cb915be6f5781735e1f919ee146a3ef")
    expect("and so is the event id seed", golden?.proposalEventID,
           "feature-proposal-v1-f167d0e8b4c9c30246f4a17fa852c5a76342a3a5")
    expect("whose acceptance shares that seed and nothing else", golden?.acceptanceEventID,
           "feature-accepted-v1-f167d0e8b4c9c30246f4a17fa852c5a76342a3a5")

    check("every event id is one the ledger will accept", outcome.proposals.allSatisfy {
        $0.proposalEventID.hasPrefix("feature-proposal-v1-") && $0.proposalEventID.count <= 128
            && $0.acceptanceEventID.hasPrefix("feature-accepted-v1-")
            && $0.acceptanceEventID != $0.proposalEventID
    })
    check("classifying the same batch twice is the same answer",
          UsageFeatureClassifier.classify(evidence) == outcome)
    check("the same label in two Projects is two Features, never one",
          proposals["iv-line-one"]?.featureID != proposals["iv-delta-one"]?.featureID
            && proposals["iv-delta-one"]?.featureID == proposals["iv-delta-two"]?.featureID)

    var declines: [String: UsageFeatureClassifier.DeclineReason] = [:]
    for decline in outcome.declined { declines[decline.intervalKey] = decline.reason }
    expect("a row with no task identity says so", declines["iv-no-task"], .noTaskIdentity)
    expect("so does one no durable record confirms", declines["iv-no-record"],
           .noDurableTaskRecord)
    expect("a label one task carries is a one-off, not a work line", declines["iv-solitary"],
           .solitaryDeclaredLabel)
    check("and two intervals of that one task are still one task, not two",
          declines["iv-twin-one"] == .solitaryDeclaredLabel
            && declines["iv-twin-two"] == .solitaryDeclaredLabel
            && proposals["iv-twin-one"] == nil && proposals["iv-twin-two"] == nil)
    expect("and evidence with nothing to group on says that instead", declines["iv-bare"],
           .noGroupingEvidence)
    expect("every interval leaves exactly once", outcome.proposals.count + outcome.declined.count,
           evidence.count)
}

group("the acceptance policy promotes only above its configured threshold") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    let at = ISO8601DateFormatter().date(from: "2026-08-21T09:15:00Z")!
    let keys = featureFixtureIntervals(at: at)
    let evidence = UsageLedger.featureEvidence(rows: UsageLedger.shared.rows(),
                                               taskFacts: featureFixtureFacts(),
                                               scheduleTitles: [:])
    let proposal = UsageFeatureClassifier.classify(evidence).proposals
        .first { $0.intervalKey == keys[0] }
    let before = UsageLedger.shared.rows().first { $0.intervalKey == keys[0] }!

    let run = UsageLedger.shared.runFeatureAttribution(evidence: evidence, now: at,
                                                       threshold: 0.80, accepting: true)
    expect("the work line's two intervals are proposed", run.proposed, 2)
    expect("and accepted at 0.80", run.accepted, 2)
    expect("with nothing left below the threshold", run.belowThreshold, 0)
    expect("the solitary label is declined rather than proposed",
           run.declined[UsageFeatureClassifier.DeclineReason.solitaryDeclaredLabel.rawValue], 1)
    let head = UsageLedger.shared.resolvedAttribution(intervalKey: keys[0], dimension: .feature)
    expect("the accepted head is the policy's", head?.source, .policy)
    expect("the proposal it superseded was the classifier's own", head?.supersedesEventID,
           proposal?.proposalEventID)
    expect("and it carries that proposal's value id", head?.valueID, proposal?.featureID)
    expect("under the deterministic acceptance id", head?.eventID, proposal?.acceptanceEventID)
    expect("naming the policy that decided", head?.decisionSource, "confidence-threshold-v1")
    expect("the work line's other interval resolves the same Feature",
           UsageLedger.shared.resolvedAttribution(intervalKey: keys[1],
                                                  dimension: .feature)?.valueID,
           proposal?.featureID)
    // The classifier's own source is held to the same evidence bar as `llm` and `policy`: an
    // event calling itself a heuristic without a classifier, a version, a 64-hex digest and a
    // confidence is not an attribution, it is an assertion.
    func heuristicEvent(_ id: String, digest: String?, confidence: Double?)
        -> UsageLedger.AttributionEvent {
        UsageLedger.AttributionEvent(
            eventID: id, intervalKey: keys[2], dimension: .feature,
            valueID: "feature-unproven", valueLabel: "Unproven Feature", source: .heuristic,
            confidence: confidence, classifierID: UsageFeatureClassifier.classifierID,
            classifierVersion: UsageFeatureClassifier.classifierVersion, evidenceDigest: digest,
            decision: .proposed, decisionSource: "focused-test", assignedAt: at,
            supersedesEventID: nil)
    }
    check("a heuristic event with no evidence digest is refused",
          !UsageLedger.shared.record(heuristicEvent("heuristic-no-digest", digest: nil,
                                                    confidence: 0.71)))
    check("and one with no confidence is refused too",
          !UsageLedger.shared.record(heuristicEvent("heuristic-no-confidence",
                                                    digest: String(repeating: "b", count: 64),
                                                    confidence: nil)))
    check("while one carrying both is accepted by the store",
          UsageLedger.shared.record(heuristicEvent("heuristic-with-evidence",
                                                   digest: String(repeating: "c", count: 64),
                                                   confidence: 0.71)))

    let again = UsageLedger.shared.runFeatureAttribution(
        evidence: evidence, now: at.addingTimeInterval(600), threshold: 0.80, accepting: true)
    expect("a second pass proposes nothing new", again.proposed, 0)
    expect("and accepts nothing new", again.accepted, 0)
    expect("the proposals are already present instead", again.proposalsAlreadyPresent, 2)
    expect("and so are the acceptances", again.acceptancesAlreadyPresent, 2)
    let after = UsageLedger.shared.rows().first { $0.intervalKey == keys[0] }!
    check("the token row never moves: the measurement is byte-identical",
          featureMeasurementBytes(before) == featureMeasurementBytes(after))
    check("and no other column of that row moved either", before == after)

    let stricter = freshUsageLedger()
    defer { forgetUsageLedger(stricter) }
    let strictKeys = featureFixtureIntervals(at: at)
    let strictEvidence = UsageLedger.featureEvidence(rows: UsageLedger.shared.rows(),
                                                      taskFacts: featureFixtureFacts(),
                                                      scheduleTitles: [:])
    let strict = UsageLedger.shared.runFeatureAttribution(evidence: strictEvidence, now: at,
                                                          threshold: 0.85, accepting: true)
    expect("the same evidence proposes the same two intervals", strict.proposed, 2)
    expect("but 0.82 is below 0.85, so the policy accepts none of it", strict.accepted, 0)
    expect("both are left for a person to look at", strict.belowThreshold, 2)
    check("and no interval resolves a Feature at all",
          strictKeys.allSatisfy {
              UsageLedger.shared.resolvedAttribution(intervalKey: $0, dimension: .feature) == nil
          })

    // A refusal is not "already present". `record(_:)` returns false for a duplicate event id and
    // for an event the store will not take at all, and a receipt that spells those the same way
    // reads a pass that wrote nothing exactly like a clean idempotent re-run — which is the one
    // statement this receipt exists to make.
    let refusing = freshUsageLedger()
    defer { forgetUsageLedger(refusing) }
    let overlongKey = String(repeating: "iv-refused-", count: 12)
    let refused = UsageLedger.shared.runFeatureAttribution(
        evidence: [featureEvidence(overlongKey, project: "/private/acme/refused",
                                   task: "task-refused", kind: "audit", title: "Refused title",
                                   plan: "Feature: Refused hint")],
        now: at, threshold: 0.80, accepting: true)
    check("an interval key the ledger will not accept is a refusal, not an absence",
          overlongKey.count > 128)
    expect("counted as refused", refused.proposalsRefused, 1)
    expect("never as already present", refused.proposalsAlreadyPresent, 0)
    expect("with nothing written", refused.proposed, 0)
    expect("and no acceptance attempted on a predecessor that is not there",
           refused.accepted + refused.acceptancesAlreadyPresent + refused.acceptancesRefused, 0)
    // A bounded reader that came back full has dropped its oldest rows, and rung 3 asks about
    // *this batch*, so the receipt has to be able to say so.
    expect("a receipt says its window was not truncated", refused.windowTruncated, false)
    expect("and says so when the reader that filled it was",
           UsageLedger.shared.runFeatureAttribution(evidence: [], now: at, threshold: 0.80,
                                                    accepting: false, windowTruncated: true)
               .windowTruncated, true)

    // The second opinion a hand-built rival head cannot produce: the classifier's own next pass,
    // after the evidence under one interval moved. Nothing here is manual.
    let moved = freshUsageLedger()
    defer { forgetUsageLedger(moved) }
    let movedKeys = featureFixtureIntervals(at: at)
    UsageLedger.shared.runFeatureAttribution(
        evidence: UsageLedger.featureEvidence(rows: UsageLedger.shared.rows(),
                                              taskFacts: featureFixtureFacts(),
                                              scheduleTitles: [:]),
        now: at, threshold: 0.80, accepting: true)
    let settledHead = UsageLedger.shared.resolvedAttribution(intervalKey: movedKeys[0],
                                                             dimension: .feature)
    // The durable record gains the plan headline it did not have when it was first classified, so
    // the row moves from rung 3 to rung 1: the Feature id, the rung and therefore the event id
    // seed all change. A `classifierVersion` bump — which §3.2 *mandates* for any change to a
    // rung, a confidence, a normalization rule or the digest recipe — moves the same seed the
    // same way, and this is the half of it a test can reach.
    var movedFacts = featureFixtureFacts()
    movedFacts["task-ladder-alpha"]?.planHeadline = "Feature: Ladder alpha, named at last"
    let movedEvidence = UsageLedger.featureEvidence(rows: UsageLedger.shared.rows(),
                                                    taskFacts: movedFacts, scheduleTitles: [:])
    let movedProposal = UsageFeatureClassifier.classify(movedEvidence).proposals
        .first { $0.intervalKey == movedKeys[0] }
    let second = UsageLedger.shared.runFeatureAttribution(
        evidence: movedEvidence, now: at.addingTimeInterval(900), threshold: 0.80,
        accepting: true)
    check("the moved interval really did change Feature",
          movedProposal != nil && movedProposal?.rung == .explicitFeatureHint
            && movedProposal?.featureID != settledHead?.valueID)
    expect("its new proposal is written", second.proposed, 1)
    expect("and no acceptance is appended beside the head already there", second.accepted, 0)
    expect("the receipt says so in one field", second.heldExistingAcceptedHead, 1)
    check("so the interval keeps the Feature it had instead of silently becoming Unknown",
          UsageLedger.shared.resolvedAttribution(intervalKey: movedKeys[0],
                                                 dimension: .feature)?.eventID
            == settledHead?.eventID && settledHead != nil)
    check("with the disagreement on the record for a person to settle",
          movedProposal.map { UsageLedger.shared.containsAttributionEvent($0.proposalEventID) }
            == true)
    expect("while the interval whose evidence did not move is untouched",
           second.acceptancesAlreadyPresent, 1)
}

group("a conflicting accepted Feature head stays Unknown") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    let at = ISO8601DateFormatter().date(from: "2026-08-22T14:45:00Z")!
    // Four intervals, four tasks, four hints, four different output totals — so each one is a
    // Feature of its own and no two rows can be confused for each other.
    let fixtures: [(name: String, seconds: Double, output: Int, hint: String)] = [
        ("settled", 0, 101, "Settled receipts"),
        ("conflict", 120, 211, "Conflicted receipts"),
        ("rejected", 240, 307, "Rejected receipts"),
        ("plain", 360, 401, "Plain receipts"),
    ]
    var keys: [String: String] = [:]
    var facts: [String: UsageLedger.TaskFacts] = [:]
    for fixture in fixtures {
        var sample = ledgerSample(.claude, session: "conflict-session-\(fixture.name)",
                                  boundary: .task, id: "task-\(fixture.name)", origin: .dispatch,
                                  usage: ["input": fixture.output + 1, "output": fixture.output,
                                          "cache_read": fixture.output + 2,
                                          "cache_write": fixture.output + 3,
                                          "total": fixture.output * 4 + 6],
                                  model: "claude-opus-5", at: at.addingTimeInterval(fixture.seconds))
        sample.taskID = "task-\(fixture.name)"
        sample.projectKey = "/private/acme/conflict"
        sample.depth = 1
        sample.seal = true
        keys[fixture.name] = UsageLedger.shared.observeNow(sample)!
        facts["task-\(fixture.name)"] = UsageLedger.TaskFacts(
            title: "\(fixture.name) title", planHeadline: "Feature: \(fixture.hint)",
            kind: fixture.name)
    }
    let evidence = UsageLedger.featureEvidence(rows: UsageLedger.shared.rows(), taskFacts: facts,
                                               scheduleTitles: [:])
    let classified = UsageFeatureClassifier.classify(evidence)
    var byInterval: [String: UsageFeatureClassifier.Proposal] = [:]
    for proposal in classified.proposals { byInterval[proposal.intervalKey] = proposal }

    func query() -> [String: Any] {
        featurePortfolio(UsageQueryService().query(
            .init(from: "2026-08-22", to: "2026-08-22", timezoneID: "UTC"), now: at).payload)
    }
    func groupIDs(_ features: [String: Any]) -> [String] {
        (features["groups"] as? [[String: Any]] ?? []).compactMap { $0["id"] as? String }
    }

    // A proposal nobody accepted is not an attribution. This is the sibling assertion for
    // proposal-only, and it runs before the policy does.
    UsageLedger.shared.backfillFeatureProposals(evidence: evidence, now: at)
    let proposedOnly = query()
    check("four proposals resolve no Feature at all", fixtures.allSatisfy {
        UsageLedger.shared.resolvedAttribution(intervalKey: keys[$0.name]!,
                                               dimension: .feature) == nil
    })
    check("so the Portfolio names no group", groupIDs(proposedOnly).isEmpty)
    expect("and says why every run is Unknown",
           (proposedOnly["unknown"] as? [String: Any])?["reason"] as? String,
           "no_unambiguous_accepted_head")
    expect("counting all four of them",
           (proposedOnly["unknown"] as? [String: Any])?["runs"] as? Int, 4)

    UsageLedger.shared.runFeatureAttribution(evidence: evidence, now: at, threshold: 0.80,
                                             accepting: true)
    // A second, disagreeing accepted head arrives — a person's assignment beside the policy's.
    // Neither supersedes the other, which is exactly the case analytics must refuse to guess at.
    let rival = UsageLedger.AttributionEvent(
        eventID: "conflict-manual-head", intervalKey: keys["conflict"]!, dimension: .feature,
        valueID: "feature-manual-rival", valueLabel: "Manual rival Feature", source: .manual,
        confidence: nil, classifierID: nil, classifierVersion: nil, evidenceDigest: nil,
        decision: .accepted, decisionSource: "focused-test", assignedAt: at,
        supersedesEventID: nil)
    check("the rival head records", UsageLedger.shared.record(rival))
    // And one interval's only head is withdrawn, which is a third, different way to be Unknown.
    check("a rejection supersedes the accepted head it names", UsageLedger.shared.record(
        UsageLedger.AttributionEvent(
            eventID: "rejected-withdrawal", intervalKey: keys["rejected"]!, dimension: .feature,
            valueID: byInterval[keys["rejected"]!]!.featureID,
            valueLabel: byInterval[keys["rejected"]!]!.featureLabel, source: .manual,
            confidence: nil, classifierID: nil, classifierVersion: nil, evidenceDigest: nil,
            decision: .rejected, decisionSource: "focused-test", assignedAt: at,
            supersedesEventID: byInterval[keys["rejected"]!]!.acceptanceEventID)))

    let conflicted = query()
    check("two accepted heads resolve to none",
          UsageLedger.shared.resolvedAttribution(intervalKey: keys["conflict"]!,
                                                 dimension: .feature) == nil)
    check("the conflicted interval is in no named group",
          !groupIDs(conflicted).contains(byInterval[keys["conflict"]!]!.featureID)
            && !groupIDs(conflicted).contains("feature-manual-rival"))
    check("neither is the one whose head was rejected",
          !groupIDs(conflicted).contains(byInterval[keys["rejected"]!]!.featureID))
    expect("both sit in Unknown Feature",
           (conflicted["unknown"] as? [String: Any])?["runs"] as? Int, 2)
    expect("for the one reason the Portfolio has",
           (conflicted["unknown"] as? [String: Any])?["reason"] as? String,
           "no_unambiguous_accepted_head")
    check("while the settled interval is a named group",
          groupIDs(conflicted).contains(byInterval[keys["settled"]!]!.featureID))

    // The same fixture, one head withdrawn: it is the *conflict* that excluded the interval and
    // nothing else about it, which is the assertion this group exists for.
    check("withdrawing one of the two rival heads records", UsageLedger.shared.record(
        UsageLedger.AttributionEvent(
            eventID: "conflict-manual-withdrawal", intervalKey: keys["conflict"]!,
            dimension: .feature, valueID: "feature-manual-rival",
            valueLabel: "Manual rival Feature", source: .manual, confidence: nil,
            classifierID: nil, classifierVersion: nil, evidenceDigest: nil, decision: .rejected,
            decisionSource: "focused-test", assignedAt: at,
            supersedesEventID: "conflict-manual-head")))
    let settled = query()
    expect("one unambiguous head is now resolvable",
           UsageLedger.shared.resolvedAttribution(intervalKey: keys["conflict"]!,
                                                  dimension: .feature)?.valueID,
           byInterval[keys["conflict"]!]!.featureID)
    check("and the same interval is a named group at last",
          groupIDs(settled).contains(byInterval[keys["conflict"]!]!.featureID))
    expect("leaving only the withdrawn one Unknown",
           (settled["unknown"] as? [String: Any])?["runs"] as? Int, 1)
}

group("Feature backfill proposes without accepting") {
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    let at = ISO8601DateFormatter().date(from: "2026-08-24T11:30:00Z")!
    let keys = featureFixtureIntervals(at: at)
    let evidence = UsageLedger.featureEvidence(rows: UsageLedger.shared.rows(),
                                               taskFacts: featureFixtureFacts(),
                                               scheduleTitles: [:])
    let run = UsageLedger.shared.backfillFeatureProposals(evidence: evidence, now: at)
    check("a backfill never takes a policy decision", !run.accepting)
    expect("it writes the proposals", run.proposed, 2)
    expect("and no acceptance at all", run.accepted, 0)
    expect("so nothing is counted as below a threshold nobody applied", run.belowThreshold, 0)
    expect("its receipt still names every decline",
           run.declined[UsageFeatureClassifier.DeclineReason.solitaryDeclaredLabel.rawValue], 1)
    check("no historical row resolves a Feature", keys.allSatisfy {
        UsageLedger.shared.resolvedAttribution(intervalKey: $0, dimension: .feature) == nil
    })
    let features = featurePortfolio(UsageQueryService().query(
        .init(from: "2026-08-24", to: "2026-08-24", timezoneID: "UTC"), now: at).payload)
    check("and the Portfolio table is unchanged by a dry run",
          (features["groups"] as? [[String: Any]])?.isEmpty == true)
    expect("every run staying Unknown", (features["unknown"] as? [String: Any])?["runs"] as? Int, 3)
    expect("a second backfill writes nothing twice",
           UsageLedger.shared.backfillFeatureProposals(evidence: evidence, now: at).proposed, 0)
}

group("the analytics payload states whether a Feature classifier is configured") {
    let at = ISO8601DateFormatter().date(from: "2026-08-25T08:05:00Z")!
    let row = analyticsRow("feature-classifier-state", at: at)
    func features(_ state: UsageLedger.FeatureClassifierState) -> [String: Any] {
        featurePortfolio(UsageQueryService(rows: { [row] }, featureClassifier: { state })
            .query(.init(from: "2026-08-25", to: "2026-08-25", timezoneID: "UTC"),
                   now: at).payload)
    }
    let off = features(.notConfigured)
    let offClassifier = off["classifier"] as? [String: Any] ?? [:]
    expect("with no producer, attribution is not automatic",
           off["automaticAttribution"] as? Bool, false)
    expect("and the classifier says so in one word", offClassifier["configured"] as? Bool, false)
    check("reporting no threshold, because nothing is applying one",
          offClassifier["threshold"] == nil && offClassifier["id"] == nil
            && offClassifier["version"] == nil)

    let on = features(UsageLedger.FeatureClassifierState(
        configured: true, classifierID: "fixture-feature-merger", classifierVersion: "7",
        threshold: 0.91))
    let onClassifier = on["classifier"] as? [String: Any] ?? [:]
    expect("a configured producer is stated as one", on["automaticAttribution"] as? Bool, true)
    expect("by the id that is actually configured", onClassifier["id"] as? String,
           "fixture-feature-merger")
    expect("at the version that is actually configured", onClassifier["version"] as? String, "7")
    expect("and the threshold that is actually applied", onClassifier["threshold"] as? Double, 0.91)
    check("while the policy and the Unknown reason are the same either way",
          on["policy"] as? String == "one_unambiguous_accepted_head"
            && off["policy"] as? String == "one_unambiguous_accepted_head"
            && (on["unknown"] as? [String: Any])?["reason"] as? String
                == "no_unambiguous_accepted_head")

    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-feature-classifier-config-\(UUID().uuidString)",
                                isDirectory: true)
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    func stored(_ json: String) -> Config {
        try! Data(json.utf8).write(to: directory.appendingPathComponent("config.json"))
        return Config(directoryForTesting: directory)
    }
    let untouched = stored("{}")
    expect("a config file that says nothing leaves the classifier off",
           untouched.usageFeatureClassifier, false)
    expect("with the documented default threshold",
           untouched.usageFeatureAcceptanceThreshold, 0.80)
    let asked = stored("{\"usage_feature_classifier\":true,"
        + "\"usage_feature_acceptance_threshold\":0.93}")
    expect("a file can turn the classifier on", asked.usageFeatureClassifier, true)
    expect("and move the threshold inside its range",
           asked.usageFeatureAcceptanceThreshold, 0.93)
    expect("a threshold below 0.5 is ignored rather than clamped, because 0 accepts everything",
           stored("{\"usage_feature_acceptance_threshold\":0.2}")
               .usageFeatureAcceptanceThreshold, 0.80)
    expect("and what a reader is told is what that file asked for",
           UsageLedger.featureClassifierState(config: asked).threshold, 0.93)
}

group("a Feature is scoped by the Project rule the Portfolio uses") {
    // The rule itself, first. A Clawdline-managed worktree path ends in a task UUID, so its
    // basename is a disposable task id and not a repository — the Projects table refuses to make
    // a Project of it, and the Feature scope has to refuse it in the same place.
    let worktree = "/Users/tester/Library/Application Support/Clawdline/worktrees/"
    let repository = "/private/acme/november"
    let missing = UsageFeatureClassifier.resolveProject(projectKey: "   ")
    expect("a row with no Project key names no Project", missing.identity, nil)
    expect("and says which refusal that was", missing.refusal, .missingProjectKey)
    let disposable = UsageFeatureClassifier.resolveProject(
        projectKey: worktree + "11111111-2222-4333-8444-555555555555")
    expect("neither does a disposable managed worktree", disposable.identity, nil)
    expect("and it says which refusal it was", disposable.refusal, .legacyManagedWorktree)
    let canonical = UsageFeatureClassifier.resolveProject(projectKey: repository + "/./")
    expect("a canonical key is the standardized path", canonical.identity, repository)
    expect("with nothing to refuse", canonical.refusal, nil)
    expect("and an accepted Project head outranks the stored key",
           UsageFeatureClassifier.resolveProject(
               projectKey: worktree + "11111111-2222-4333-8444-555555555555",
               acceptedProjectIdentity: repository).identity,
           repository)

    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    let at = ISO8601DateFormatter().date(from: "2026-08-26T13:20:00Z")!
    // Two tasks of one declared work line, each recorded inside its own disposable worktree.
    // Split by the raw `project_key`, they are two Features carrying one label — which is what
    // real data showed on 2026-09-03: three work lines arriving as six rows on the Feature table
    // with nothing on screen to explain why.
    let rows: [(name: String, task: String, seconds: Double, worktreeID: String, output: Int)] = [
        ("scope-one", "task-scope-one", 0, "11111111-2222-4333-8444-555555555555", 131),
        ("scope-two", "task-scope-two", 180, "66666666-7777-4888-8999-aaaaaaaaaaaa", 227),
    ]
    var keys: [String: String] = [:]
    for row in rows {
        var sample = ledgerSample(.claude, session: "scope-session-\(row.name)", boundary: .task,
                                  id: row.task, origin: .dispatch,
                                  usage: ["input": row.output + 1, "output": row.output,
                                          "cache_read": row.output + 2,
                                          "cache_write": row.output + 3,
                                          "total": row.output * 4 + 6],
                                  model: "claude-opus-5", at: at.addingTimeInterval(row.seconds))
        sample.taskID = row.task
        sample.projectKey = worktree + row.worktreeID
        sample.depth = 1
        sample.seal = true
        keys[row.name] = UsageLedger.shared.observeNow(sample)!
    }
    let facts: [String: UsageLedger.TaskFacts] = [
        "task-scope-one": UsageLedger.TaskFacts(
            title: "Scope one title", declaredWorkLine: "November ladder",
            planHeadline: "one plan headline naming nothing", kind: "code"),
        "task-scope-two": UsageLedger.TaskFacts(
            title: "Scope two title", declaredWorkLine: "November ladder",
            planHeadline: "another plan headline naming nothing", kind: "review"),
    ]
    func classify(_ acceptedProjects: [String: UsageLedger.AcceptedAttribution])
        -> [String: UsageFeatureClassifier.Proposal] {
        let evidence = UsageLedger.featureEvidence(rows: UsageLedger.shared.rows(),
                                                   taskFacts: facts, scheduleTitles: [:],
                                                   acceptedProjects: acceptedProjects)
        var byInterval: [String: UsageFeatureClassifier.Proposal] = [:]
        for proposal in UsageFeatureClassifier.classify(evidence).proposals {
            byInterval[proposal.intervalKey] = proposal
        }
        return byInterval
    }
    func projects() -> [[String: Any]] {
        ((UsageQueryService().query(.init(from: "2026-08-26", to: "2026-08-26",
                                          timezoneID: "UTC"), now: at)
            .payload["portfolio"] as? [String: Any])?["projects"] as? [[String: Any]]) ?? []
    }

    let suppressed = classify([:])
    check("two disposable worktrees carrying one label are one Feature",
          suppressed[keys["scope-one"]!]?.featureID != nil
            && suppressed[keys["scope-one"]!]?.featureID
                == suppressed[keys["scope-two"]!]?.featureID)
    expect("scoped where the Projects table put them, which is nowhere",
           suppressed[keys["scope-one"]!]?.featureID,
           UsageFeatureClassifier.featureID(
               projectScope: UsageFeatureClassifier.unknownProjectScope,
               rung: .declaredWorkLine, groupingKey: "November ladder"))
    let suppressedProjects = projects()
    check("and the Projects table says exactly that about the same two rows",
          suppressedProjects.count == 1
            && suppressedProjects.first?["label"] as? String == "Unknown Project"
            && suppressedProjects.first?["runs"] as? Int == 2)
    expect("in the classifier's own word for the refusal",
           (suppressedProjects.first?["identity"] as? [String: Any])?["reasons"] as? [String],
           [UsageFeatureClassifier.ProjectScopeRefusal.legacyManagedWorktree.rawValue])

    // The migration's own remedy, applied: one accepted Project head per interval naming the
    // repository. The Projects table names it, and the Feature scope moves with it.
    for name in rows.map(\.name) {
        let key = keys[name]!
        check("an accepted Project head records for \(name)", UsageLedger.shared.record(
            UsageLedger.AttributionEvent(
                eventID: "scope-project-head-\(name)", intervalKey: key, dimension: .project,
                valueID: repository, valueLabel: "november", source: .manual, confidence: nil,
                classifierID: nil, classifierVersion: nil, evidenceDigest: nil,
                decision: .accepted, decisionSource: "focused-test", assignedAt: at,
                supersedesEventID: nil)))
    }
    let accepted = classify(
        UsageLedger.shared.analyticsRead(UsageLedger.AnalyticsFilter(limit: 500)).acceptedProjects)
    expect("the Projects table now names the repository",
           projects().first?["label"] as? String, "november")
    expect("and the Feature is scoped to that same accepted Project",
           accepted[keys["scope-one"]!]?.featureID,
           UsageFeatureClassifier.featureID(projectScope: repository, rung: .declaredWorkLine,
                                            groupingKey: "November ladder"))
    check("still one Feature, and not the one the suppressed scope produced",
          accepted[keys["scope-one"]!]?.featureID == accepted[keys["scope-two"]!]?.featureID
            && accepted[keys["scope-one"]!]?.featureID
                != suppressed[keys["scope-one"]!]?.featureID)
}

// A row as it actually lands in the ledger for a task that ran inside a Clawdline-managed
// worktree: `working_dir` is the checkout, `project_key` is the canonical repository, and the two
// state words are what the broker record said when the row was collected.
func worktreeRow(_ key: String, at: Date, worktree: String, task: String,
                 project: String? = "/private/acme/widget", state: String? = "success",
                 landing: String? = nil) -> UsageLedger.Row {
    var row = UsageLedger.Row()
    row.intervalKey = key
    row.assistant = "claude"
    row.sessionID = "private-session-\(key)"
    row.boundaryKind = "task"
    row.boundaryID = task
    row.taskID = task
    row.origin = "dispatch"
    row.projectKey = project
    row.workingDir = worktree.isEmpty
        ? project.map { $0 + "/checkout" }
        : "/Users/tester/Library/Application Support/Clawdline/worktrees/widget-9f1c/" + worktree
    row.isolation = worktree.isEmpty ? "none" : "worktree"
    row.taskState = state
    row.landingState = landing
    row.model = "claude-opus-5"
    row.counts = .init(inputNew: 1, output: 2, cacheRead: 3, cacheWrite: 4)
    row.total = row.counts.total
    row.costBasis = "unknown"
    row.missingReason = "no_cost_recorded"
    row.coverage = "complete"
    row.startedAt = at
    row.updatedAt = at.addingTimeInterval(30)
    row.localDay = UsageLedger.localDay(of: at)
    row.sealed = true
    return row
}

func acceptedFeature(_ id: String, _ label: String) -> UsageLedger.AcceptedAttribution {
    UsageLedger.AcceptedAttribution(id: id, label: label)
}

group("a worktree's outcome tells landed from delivered from debris") {
    let at = ISO8601DateFormatter().date(from: "2026-09-01T09:00:00Z")!
    func outcome(_ rows: [UsageLedger.Row], live: Set<String> = [],
                 branch: UsageProjectWorktreeService.LandingEvidence = .unknown) -> String {
        UsageProjectWorktreeService.outcome(rows, live: live, branch: branch).rawValue
    }
    func evidence(_ rows: [UsageLedger.Row],
                  branch: UsageProjectWorktreeService.LandingEvidence) -> String {
        UsageProjectWorktreeService.evidence(rows, branch: branch).rawValue
    }
    // A landing record outranks the child's own word about itself. Two rows on this Mac say
    // `failure` beside `landed`: the child reported failure and the root integrated the branch
    // anyway, and what a person is asking about is the branch.
    expect("a landing record makes it landed, whatever the task said",
           outcome([worktreeRow("landed", at: at, worktree: "w1", task: "t1",
                                state: "failure", landing: "landed")]), "landed")
    // b1103ab1's shape: it finished, nothing landed it, and it sat for 26 hours.
    expect("a task that succeeded with no landing is delivered",
           outcome([worktreeRow("spawning", at: at, worktree: "w1", task: "t1",
                                state: "spawning"),
                    worktreeRow("done", at: at, worktree: "w1", task: "t1", state: "success")]),
           "delivered")
    expect("an open landing obligation is still delivered, not landed",
           outcome([worktreeRow("pending", at: at, worktree: "w1", task: "t1",
                                state: "success", landing: "pending")]), "delivered")
    expect("and so is one that was given up",
           outcome([worktreeRow("given-up", at: at, worktree: "w1", task: "t1",
                                state: "success", landing: "abandoned")]), "delivered")
    // b57fc96f's shape, and the one a threshold would have got wrong: nothing terminal was ever
    // written, because the session died before anything could write it.
    let stalled = [worktreeRow("stalled", at: at, worktree: "w2", task: "t2", state: "briefed")]
    expect("a task stuck at briefed with nothing live is debris", outcome(stalled), "abandoned")
    expect("the same rows are active while that task is still running",
           outcome(stalled, live: ["t2"]), "active")
    expect("liveness is asked about this task, not about any task",
           outcome(stalled, live: ["t9"]), "abandoned")
    for state in ["failure", "timeout", "cancelled", "spawn_failed"] {
        expect("a \(state) task with nothing landed is debris",
               outcome([worktreeRow(state, at: at, worktree: "w3", task: "t3", state: state)]),
               "abandoned")
    }
    expect("a row carrying no state at all claims none of the four",
           outcome([worktreeRow("silent", at: at, worktree: "w4", task: "t4", state: nil)]),
           "unknown")

    // **The half of the ladder that asks git rather than asking whether anybody wrote it down.**
    // On 2026-09-05 this route called 53 of one repository's worktrees "delivered, not landed"
    // while git said 24 of those branches were already ancestors of HEAD and 13 were gone.
    let finished = [worktreeRow("finished", at: at, worktree: "w5", task: "t5", state: "success")]
    expect("a delivery whose branch git says is merged has landed, with nobody recording it",
           outcome(finished, branch: .branchMerged), "landed")
    expect("and says which of the two sources answered",
           evidence(finished, branch: .branchMerged), "branch_merged")
    expect("a delivery whose branch git says is gone has nothing outstanding on it",
           outcome(finished, branch: .branchAbsent), "landed")
    expect("named as the branch's absence and never as a record",
           evidence(finished, branch: .branchAbsent), "branch_absent")
    expect("a branch that is there and unmerged is the one delivered with a fact behind it",
           outcome(finished, branch: .branchUnmerged), "delivered")
    expect("and that fact travels beside it", evidence(finished, branch: .branchUnmerged),
           "branch_unmerged")
    // The direction that costs a day if it is wrong: git not answering must never read as
    // settled. This is the same fail-safe `workVisibility` takes with deletion.
    expect("git failing to answer leaves the verdict exactly where it was",
           outcome(finished, branch: .unknown), "delivered")
    expect("and says so rather than implying nothing landed it",
           evidence(finished, branch: .unknown), "unknown")
    // **A deleted branch settles only work that succeeded.** `disposeWorktree` deletes a delivery
    // branch exactly when it carries no commits, so on a failed task absence means the checkout
    // was thrown away empty — debris, and not a landing.
    let failed = [worktreeRow("failed", at: at, worktree: "w6", task: "t6", state: "failure")]
    expect("a failed task whose branch is gone is still debris",
           outcome(failed, branch: .branchAbsent), "abandoned")
    expect("even though the branch fact beside it is exactly the same word",
           evidence(failed, branch: .branchAbsent), "branch_absent")
    expect("while a failed task whose branch is merged landed, for the reason a record does",
           outcome(failed, branch: .branchMerged), "landed")
    let recorded = [worktreeRow("recorded", at: at, worktree: "w7", task: "t7",
                                state: "success", landing: "landed")]
    expect("a root's record outranks a branch this side merely found unmerged",
           outcome(recorded, branch: .branchUnmerged), "landed")
    expect("and is reported as the record it is",
           evidence(recorded, branch: .branchUnmerged), "record")

    // The branch name is a convention, and this is the only place that turns a worktree id into
    // one. A repository git never answered for says nothing about any of its branches.
    let known = Orchestrator.RepositoryBranches(
        heads: ["clawdline/task/11111111-2222-4333-8444-555555555555": "d00dfeed",
                "clawdline/task/22222222-2222-4333-8444-555555555555": "cafed00d"],
        merged: ["clawdline/task/22222222-2222-4333-8444-555555555555"], known: true)
    func branchFact(_ id: String, _ branches: Orchestrator.RepositoryBranches) -> String {
        UsageProjectWorktreeService.branchEvidence(worktree: id, branches: branches).rawValue
    }
    expect("a listed branch HEAD contains is merged",
           branchFact("22222222-2222-4333-8444-555555555555", known), "branch_merged")
    expect("a listed branch it does not contain is unmerged",
           branchFact("11111111-2222-4333-8444-555555555555", known), "branch_unmerged")
    expect("a branch the listing has no line for is gone",
           branchFact("33333333-2222-4333-8444-555555555555", known), "branch_absent")
    expect("a repository git could not read says nothing about any of them",
           branchFact("22222222-2222-4333-8444-555555555555",
                      Orchestrator.RepositoryBranches()), "unknown")
    // *Absent* is a fact about the repository; *unknown* is a fact about this side. A worktree id
    // that builds no branch name is the second, and reading it as the first would settle a
    // delivery nobody ever asked about.
    expect("an id no branch name can be built from is unknown, not absent",
           branchFact("not-a-task-id", known), "unknown")

    check("and the ladder is ordered strongest first",
          UsageProjectWorktreeService.Outcome.landed.rank
            < UsageProjectWorktreeService.Outcome.delivered.rank
            && UsageProjectWorktreeService.Outcome.delivered.rank
                < UsageProjectWorktreeService.Outcome.active.rank
            && UsageProjectWorktreeService.Outcome.active.rank
                < UsageProjectWorktreeService.Outcome.abandoned.rank)
}

group("a Project's worktrees are joined at read time and named by the Portfolio's own rule") {
    let at = ISO8601DateFormatter().date(from: "2026-09-02T10:00:00Z")!
    let repository = "/private/acme/widget"
    let alpha = "11111111-2222-4333-8444-555555555555"
    let beta = "66666666-7777-4888-8999-aaaaaaaaaaaa"
    let quiet = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
    let managed = "/Users/tester/Library/Application Support/Clawdline/worktrees/widget-9f1c/"
    let rows = [
        // One worktree, two tasks: the owning child and a grandchild dispatched from inside it.
        worktreeRow("alpha-own", at: at, worktree: alpha, task: alpha, landing: "landed"),
        worktreeRow("alpha-grandchild", at: at.addingTimeInterval(600), worktree: alpha,
                    task: "grandchild-of-alpha"),
        worktreeRow("beta-own", at: at.addingTimeInterval(1_200), worktree: beta, task: beta,
                    state: "briefed"),
        // A worktree with rows and no accepted Feature: counted, never listed. This is most of
        // the 58 checkouts on the real Mac, and the reason this read is not `git worktree list`.
        worktreeRow("quiet", at: at.addingTimeInterval(1_800), worktree: quiet, task: quiet),
        // Work in the shared checkout: this Project's, and no worktree's.
        worktreeRow("shared", at: at.addingTimeInterval(2_400), worktree: "", task: "shared-task"),
        // A row recorded before canonical Project keys landed: its own project_key is a managed
        // worktree, so it resolves to no Project and may appear under none.
        worktreeRow("legacy", at: at.addingTimeInterval(3_000), worktree: quiet,
                    task: "legacy-task", project: managed + quiet),
    ]
    let features = ["alpha-own": acceptedFeature("feature-a", "The landing queue"),
                    "alpha-grandchild": acceptedFeature("feature-b", "Its focused runner"),
                    "beta-own": acceptedFeature("feature-a", "The landing queue"),
                    "shared": acceptedFeature("feature-a", "The landing queue"),
                    "legacy": acceptedFeature("feature-c", "Something with no Project")]
    func read(_ project: String, live: Set<String> = []) -> UsageProjectWorktreeService.Answer {
        UsageProjectWorktreeService(rows: { rows }, acceptedFeatures: { features },
                                    liveTaskIDs: { live })
            .read(.init(project: project, timezoneID: "UTC"), now: at)
    }
    let byName = read("widget").payload ?? [:]
    let project = byName["project"] as? [String: Any] ?? [:]
    expect("the Project is the one the Portfolio names", project["label"] as? String, "widget")
    expect("under the id the Portfolio computes", project["id"] as? String,
           UsageQueryService.projectID(repository))
    let byID = read(UsageQueryService.projectID(repository)).payload?["project"]
        as? [String: Any]
    check("and asking by that id is the same answer",
          byID?["id"] as? String == project["id"] as? String)
    let byPath = read(repository).payload?["project"] as? [String: Any]
    check("as is asking by its absolute canonical path",
          byPath?["id"] as? String == project["id"] as? String)

    let worktrees = byName["worktrees"] as? [[String: Any]] ?? []
    expect("only the worktrees that finished a Feature are listed", worktrees.count, 2)
    expect("newest first", worktrees.first?["id"] as? String, beta)
    let listed = Set(worktrees.compactMap { $0["id"] as? String })
    check("the quiet worktree and the shared checkout are not among them",
          !listed.contains(quiet) && listed == Set([alpha, beta]))
    let alphaRow = worktrees.first { $0["id"] as? String == alpha } ?? [:]
    expect("a worktree that hosted a grandchild is still one worktree",
           (alphaRow["tasks"] as? [String])?.count, 2)
    expect("with both Features it carried", (alphaRow["features"] as? [[String: Any]])?.count, 2)
    expect("and the landing that reached the tree", alphaRow["outcome"] as? String, "landed")
    let alphaFeatures = alphaRow["features"] as? [[String: Any]] ?? []
    expect("each Feature answers for its own rows",
           alphaFeatures.first { $0["id"] as? String == "feature-b" }?["outcome"] as? String,
           "delivered")
    expect("under the label the accepted head carries",
           alphaFeatures.first { $0["id"] as? String == "feature-b" }?["label"] as? String,
           "Its focused runner")
    let betaRow = worktrees.first { $0["id"] as? String == beta } ?? [:]
    expect("a worktree whose only task stalled is debris", betaRow["outcome"] as? String,
           "abandoned")
    let whileLive = (read("widget", live: [beta]).payload?["worktrees"]
        as? [[String: Any]]) ?? []
    expect("and the same worktree is active while that task runs",
           whileLive.first { $0["id"] as? String == beta }?["outcome"] as? String, "active")

    let excluded = byName["excluded"] as? [String: Any] ?? [:]
    expect("the worktrees with no nameable Feature are counted",
           excluded["worktreesWithoutFeature"] as? Int, 1)
    expect("with the reason the Portfolio uses for the same absence",
           excluded["reason"] as? String, "no_unambiguous_accepted_head")
    let unattributed = byName["unattributed"] as? [String: Any] ?? [:]
    expect("a worktree belonging to no Project is reported rather than dropped",
           unattributed["worktrees"] as? Int, 1)
    expect("with the refusal that produced it",
           (unattributed["reasons"] as? [String: Int])?["legacy_managed_worktree_project_key"], 1)
    let receipt = byName["read"] as? [String: Any] ?? [:]
    expect("the receipt says how much was read", receipt["rows"] as? Int, rows.count)
    expect("how much of it was this Project's", receipt["projectRows"] as? Int, 5)
    expect("how much of that ran in a worktree", receipt["worktreeRows"] as? Int, 4)
    expect("and how much of that carried a Feature", receipt["featureRows"] as? Int, 3)
    expect("a complete scan is not partial", byName["status"] as? String, "available")
    expect("and the rule it answered by names both of the sources it may use",
           byName["outcomeRule"] as? String,
           "landed_by_record_or_branch_then_delivered_then_live_then_abandoned")
    expect("with no branch fact, every verdict rests on the stored columns and says so",
           alphaRow["landingEvidence"] as? String, "record")
    expect("including a Feature whose own rows carry no landing at all",
           alphaFeatures.first { $0["id"] as? String == "feature-b" }?["landingEvidence"] as? String,
           "unknown")

    // **The same rows, once the repository is allowed to answer.** `beta`'s branch is there and
    // unmerged; `alpha`'s is not there at all.
    var asked: [String] = []
    func withGit(_ branches: Orchestrator.RepositoryBranches) -> [String: Any] {
        UsageProjectWorktreeService(
            rows: { rows }, acceptedFeatures: { features },
            branches: { repository in asked.append(repository); return branches })
            .read(.init(project: "widget", timezoneID: "UTC"), now: at).payload ?? [:]
    }
    let gitAnswered = withGit(Orchestrator.RepositoryBranches(
        heads: ["clawdline/task/" + beta: "cafed00d"], merged: [], known: true))
    expect("git is asked about the Project's own canonical repository, once", asked, [repository])
    let seen = (gitAnswered["worktrees"] as? [[String: Any]]) ?? []
    let alphaSeen = seen.first { $0["id"] as? String == alpha } ?? [:]
    let betaSeen = seen.first { $0["id"] as? String == beta } ?? [:]
    expect("a worktree carrying a landing record still says the record, not the branch",
           alphaSeen["landingEvidence"] as? String, "record")
    let grandchild = (alphaSeen["features"] as? [[String: Any]] ?? [])
        .first { $0["id"] as? String == "feature-b" } ?? [:]
    expect("while the Feature with no record of its own is settled by the missing branch",
           grandchild["outcome"] as? String, "landed")
    expect("saying which fact settled it", grandchild["landingEvidence"] as? String,
           "branch_absent")
    expect("a stalled worktree whose branch is still sitting there stays debris",
           betaSeen["outcome"] as? String, "abandoned")
    expect("and reports the branch fact rather than pretending there is none",
           betaSeen["landingEvidence"] as? String, "branch_unmerged")

    let merged = withGit(Orchestrator.RepositoryBranches(
        heads: ["clawdline/task/" + beta: "cafed00d"],
        merged: ["clawdline/task/" + beta], known: true))
    let betaMerged = ((merged["worktrees"] as? [[String: Any]]) ?? [])
        .first { $0["id"] as? String == beta } ?? [:]
    expect("a branch HEAD already contains landed, whatever the task record says",
           betaMerged["outcome"] as? String, "landed")
    expect("on the branch's evidence and not on a record nobody wrote",
           betaMerged["landingEvidence"] as? String, "branch_merged")
}

group("an empty worktree list says the query ran, and an unknown Project is refused") {
    let at = ISO8601DateFormatter().date(from: "2026-09-03T11:00:00Z")!
    let rows = [worktreeRow("shared-only", at: at, worktree: "", task: "shared-task")]
    let service = UsageProjectWorktreeService(
        rows: { rows }, acceptedFeatures: { ["shared-only": acceptedFeature("f", "A Feature")] })
    let empty = service.read(.init(project: "widget", timezoneID: "UTC"), now: at)
    expect("a Project whose work never left the shared checkout has no worktrees",
           (empty.payload?["worktrees"] as? [[String: Any]])?.count, 0)
    let receipt = empty.payload?["read"] as? [String: Any] ?? [:]
    // **This is the pair that must never look alike.** An empty list with rows behind it is an
    // answer; the same list with nothing behind it is a query that did not run.
    check("and says so with rows behind it rather than with silence",
          receipt["rows"] as? Int == 1 && receipt["projectRows"] as? Int == 1
            && receipt["worktreeRows"] as? Int == 0
            && empty.payload?["status"] as? String == "available")
    expect("nothing was refused", empty.refusal, nil)

    let missing = service.read(.init(project: "gadget", timezoneID: "UTC"), now: at)
    expect("a Project nothing in range mentions is refused, not answered",
           missing.refusal?.code, "project_not_found")
    expect("with the status a client can branch on", missing.refusal?.status, 404)
    check("and no payload at all", missing.payload == nil
            && missing.refusal?.message.contains("1 row(s) were read") == true)
    let outOfRange = service.read(.init(project: "widget", from: "2026-08-01", to: "2026-08-02",
                                        timezoneID: "UTC"), now: at)
    expect("a range that excludes every row reaches the same refusal",
           outOfRange.refusal?.code, "project_not_found")

    // Two repositories whose final name is the same. The Portfolio puts that name on screen, so
    // answering for whichever one came first would file one's worktrees under the other.
    let namesake = [worktreeRow("left", at: at, worktree: "aaaaaaaa-1111-4222-8333-444444444444",
                                task: "left-task", project: "/private/acme/widget"),
                    worktreeRow("right", at: at, worktree: "aaaaaaaa-1111-4222-8333-555555555555",
                                task: "right-task", project: "/private/other/widget")]
    let ambiguous = UsageProjectWorktreeService(rows: { namesake })
        .read(.init(project: "widget", timezoneID: "UTC"), now: at)
    expect("two Projects of one name are refused rather than merged",
           ambiguous.refusal?.code, "ambiguous_project")
    expect("as a conflict, not a miss", ambiguous.refusal?.status, 409)
    check("and the message hands back both unambiguous ids",
          ambiguous.refusal?.message.contains(
            UsageQueryService.projectID("/private/acme/widget")) == true
            && ambiguous.refusal?.message.contains(
                UsageQueryService.projectID("/private/other/widget")) == true)
    check("while either id answers on its own",
          UsageProjectWorktreeService(rows: { namesake })
            .read(.init(project: UsageQueryService.projectID("/private/other/widget"),
                        timezoneID: "UTC"), now: at).refusal == nil)

    func parse(_ values: [String: String], repeated: Set<String> = []) -> String? {
        UsageProjectWorktreeService.parse(values, repeatedKeys: repeated).error
    }
    expect("the query is closed", parse(["project": "widget", "group": "day"]) != nil, true)
    expect("and may not repeat a key",
           parse(["project": "widget"], repeated: ["project"]) != nil, true)
    expect("a read with no Project is refused before it reads anything",
           parse([:]) != nil, true)
    expect("blank counts as none", parse(["project": "   "]) != nil, true)
    expect("dates are local days", parse(["project": "w", "from": "2026-9-1"]) != nil, true)
    expect("and cannot run backwards",
           parse(["project": "w", "from": "2026-09-02", "to": "2026-09-01"]) != nil, true)
    expect("the timezone must be a zone", parse(["project": "w", "timezone": "Mars/Olympus"]) != nil,
           true)
    expect("a well-formed query parses", parse(["project": "widget", "timezone": "Asia/Taipei"]),
           nil)
    check("a Project id is matched by its shape and not by its prefix",
          UsageProjectWorktreeService.isProjectID(UsageQueryService.projectID("/private/acme/w"))
            && !UsageProjectWorktreeService.isProjectID("project-not-a-digest")
            && !UsageProjectWorktreeService.isProjectID("project-things"))

    // The door, end to end: a refusal this service invents is only worth having if it is the
    // one the route hands back.
    let anonymous = RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/usage/project-worktrees?project=widget"))
    expect("an anonymous reader is refused before the scan", anonymous.status, 401)
    check("and this read takes the analytics side door rather than the shared queue",
          RemoteServer.isUsageAnalyticsReading("/v1/orchestrator/usage/project-worktrees"))
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    check("while the orchestrator token reaches the worker",
          RemoteServer.shared.slowReadingRefusal(
            remoteRequest("GET", "/v1/orchestrator/usage/project-worktrees?project=widget",
                          headers: auth)) == nil)
    let unknownProject = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/usage/project-worktrees?project=no-such-project-anywhere",
        headers: auth))
    expect("a Project this Mac has never recorded is 404 on the route too",
           unknownProject.status, 404)
    expect("carrying the service's own code", remoteErrorCode(unknownProject),
           "project_not_found")
    let misspelled = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/usage/project-worktrees?project=widget&group=day",
        headers: auth))
    expect("and a misspelled filter is refused rather than quietly widening the query",
           misspelled.status, 400)
    expect("as a bad request", remoteErrorCode(misspelled), "bad_request")
}

}
