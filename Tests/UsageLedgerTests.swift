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
                if RemoteServer.coordinatorInventoryRefreshGateForTesting != nil {
                    seamsLeftSet.append(
                        "RemoteServer.coordinatorInventoryRefreshGateForTesting")
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

}
