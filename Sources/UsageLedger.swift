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
/// **Five invariants, and they are the whole design.** Each one is stated here because the
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
/// 5. *A row's coverage marks accumulate; they do not overwrite.* If two things are true about a
///    row's coverage, both reach every reader. ``coverage_reasons`` is a set and every writer
///    unions into it, because one slot with a last writer loses the earlier mark in silence —
///    and the earlier mark was always ``CoverageReason/sourceRegressed``, on precisely the rows
///    whose number was measured across a seam. This is invariant 3's defect wearing the writer's
///    clothes: a mark that never reaches a reader is a mark that was never made.
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

    /// The store's own migration counter, and deliberately **not** ``schemaVersion``: that one is
    /// part of every interval key, so bumping it to add a column would orphan every row ever
    /// written from the key its own collector will compute next time. Adding a column, an index
    /// or a column *name* changes neither an identity nor the meaning of a stored value, and
    /// needs a number of its own.
    static let storeVersion = 5

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

    /// **Why a row is marked — and a row may be marked more than once.**
    ///
    /// These are the store's own words for *something is true of this row's coverage that its
    /// numbers do not show*. They are not exclusive and they never were: a task filed under an
    /// invented identity can also be read across a replaced transcript, and a session that
    /// rotated can also be unreadable by the time it closes. Both of those are reachable today.
    ///
    /// The column that holds them is therefore a **set** (``coverageReasons(stored:)``), and
    /// every writer adds to it rather than assigning it. One slot with a last writer was the
    /// same defect the reader seam was built to stop, arriving from the other side: the mark
    /// that loses is always the earlier one — `source_regressed` — and every reader is then told
    /// only half of what the store knew.
    enum CoverageReason: String, CaseIterable {
        /// Neither collector ever knew the session this task ran in, so the store invented one.
        case sessionUnresolved = "session_unresolved"
        /// A cumulative counter went backwards: this row's number spans a replaced source.
        case sourceRegressed = "source_regressed"
        /// The source could not be read at the moment the segment was sealed.
        case sourceUnreadableAtClose = "source_unreadable_at_close"
        /// The work reached a terminal state carrying no usage object at all.
        case noUsageRecorded = "no_usage_recorded"
    }

    /// The marks on one row, read out of the column that holds them.
    ///
    /// Stored **space-separated** rather than comma-separated on purpose: the export is the
    /// surface a month gets audited from, and a comma inside a CSV field is correct, quoted, and
    /// the exact thing that a `cut -d,` or a hand-rolled splitter gets wrong. Every mark this
    /// store writes is one ``CoverageReason``, so a separator can never appear inside one.
    ///
    /// Unknown words are kept rather than dropped. A mark this build does not recognise is still
    /// a mark, and a reader that silently discarded it would be the defect, one release later.
    static func coverageReasons(stored: String?) -> [String] {
        (stored ?? "").split(separator: " ").map(String.init)
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

    /// How the spend is billed. `unknown` is the answer for Claude and it is a real one: a
    /// transcript does not record whether the session ran against a subscription or an API key,
    /// and `metered` exists so that saying `unknown` means something rather than being the only
    /// word available.
    enum BillingMode: String {
        case plan, metered, unknown
    }

    /// **Which reading of `input` the arithmetic chose, and why.**
    ///
    /// ``normalize(raw:assistant:)`` decides whether a source's `input` already includes its
    /// cached input by computing both readings and keeping the one that reconciles with the
    /// source's own total. That decision reshapes the stored parts, and until this column existed
    /// the only way to tell "the cache was taken out of input" from "input never included it" was
    /// to re-derive the arithmetic from `usage_raw` — a determination nobody can audit later,
    /// which is the same shape as the unknowns this whole store exists to keep visible. So the
    /// decision is recorded on the row, including the benign case where both readings agree.
    ///
    /// `reconciliation` keeps its own meaning — *something did not add up* — and stays NULL when
    /// everything did. These are two different facts and they are two columns.
    enum InputBasis: String {
        /// The parts as they came reconcile with the stated total: input excludes the cache.
        case excludesCache = "excludes_cache"
        /// The total only reconciles once the cache is taken out of input, so it was.
        case includesCache = "includes_cache"
        /// Both readings reconcile, which happens exactly when the cache read (or the input) is
        /// zero and the two are therefore the same number. Benign, and worth saying so.
        case readingsAgree = "readings_agree"
        /// No stated total to check against; the cache was taken out of input on the assistant's
        /// known shape alone. The weakest determination here, and it says which one it is.
        case includesCacheAssumed = "includes_cache_assumed"
        /// No stated total, and no reason to reshape the parts.
        case unstated
        /// Neither reading reconciles. The parts are kept exactly as they came.
        case unreconciled
        /// Neither reading reconciles **and** the cache was still taken out of input on the
        /// assistant's shape. Without this word, `parts_do_not_sum` on such a row reads as "the
        /// parts are wrong" when what happened is "they still do not sum after I removed the
        /// cache" — two different things to whoever reads the row next.
        case includesCacheUnreconciled = "includes_cache_unreconciled"
    }

    /// The prefix of an identity the store had to invent because neither collector ever knew the
    /// session a task ran in. It is a mark on the row, and ``Row/measurement`` is where that mark
    /// is turned back into something a reader can see.
    static let unresolvedSessionPrefix = "unresolved-session:"

    /// Durable lineage columns. Store version 5 fills the facts the broker already records; `graph_id`
    /// and `disposition` remain NULL until an explicit producer exists. Neither is inferred from
    /// a root Session or a successful terminal state.
    static let lineageColumns = ["graph_id", "parent_task_id", "retry_of", "attempt",
                                 "landing_state", "disposition"]
    /// Columns for which no durable producer exists. Feature is intentionally absent: accepted
    /// append-only attribution events now provide that dimension without pretending it is a task
    /// registry column. Both the legacy aggregate and Portfolio capability surfaces use this one
    /// answer.
    static let unavailableDimensions = ["graph_id", "disposition"]

    static let reservedColumnsReason =
        "A whole graph, accepted outcome, or Feature is unavailable unless explicit lineage or "
        + "an accepted attribution event exists. Clawdline never infers them from a root Session "
        + "or task success."

    enum AttributionDimension: String, CaseIterable { case project, feature }
    /// Who made the assignment. `heuristic` is the local Feature classifier in
    /// `Sources/UsageFeatureClassifier.swift`: it is not `llm`, because there is no model in it
    /// and saying otherwise would be a lie about provenance, and it is not `policy`, because a
    /// policy decides while this observes evidence. `valid(_:)` holds it to the same evidence
    /// requirements as the other two machine sources.
    enum AttributionSource: String, CaseIterable {
        case explicit, inherited, manual, llm, policy, heuristic
    }
    enum AttributionDecision: String, CaseIterable { case proposed, accepted, rejected }

    /// One append-only classification decision. A small LLM may write `proposed`; analytics uses
    /// only one unambiguous accepted head. The evidence itself stays outside the ledger — only a
    /// digest is retained — so Feature grouping never becomes a second prompt archive.
    struct AttributionEvent: Equatable {
        var eventID: String
        var intervalKey: String
        var dimension: AttributionDimension
        var valueID: String
        var valueLabel: String
        var source: AttributionSource
        var confidence: Double?
        var classifierID: String?
        var classifierVersion: String?
        var evidenceDigest: String?
        var decision: AttributionDecision
        var decisionSource: String
        var assignedAt: Date
        var supersedesEventID: String?
    }

    /// The only attribution shape analytics is allowed to aggregate. The event history stays
    /// private; a value appears here only after the store has proved that one active accepted
    /// head exists for the interval and dimension.
    struct AcceptedAttribution: Equatable {
        var id: String
        var label: String
    }

    /// Evidence permitted to move a legacy managed-worktree Project key. Neither case derives a
    /// repository from the UUID/basename: one asks Git for the still-live common directory; the
    /// other requires an exact durable task/worktree/project-dir receipt.
    enum ProjectMigrationEvidenceSource: String {
        case gitCommonDirectory = "git_common_directory"
        case taskWorktreeReceipt = "task_worktree_receipt"
    }

    struct LegacyProjectMigrationEvidence: Equatable {
        var legacyProjectKey: String
        var repositoryRoot: String
        var source: ProjectMigrationEvidenceSource
        var reference: String
        var evidenceDigest: String
    }

    struct LegacyProjectMigrationAuditEntry: Equatable {
        var intervalKey: String
        var legacyProjectKey: String
        var repositoryRoot: String?
        var source: ProjectMigrationEvidenceSource?
        var evidenceDigest: String?
        var eventID: String?
        var status: String
        var reason: String?
    }

    struct LegacyProjectMigrationPlan {
        static let version = 1
        var events: [AttributionEvent]
        var audit: [LegacyProjectMigrationAuditEntry]

        var payload: [String: Any] {
            [
                "schemaVersion": Self.version,
                "mode": "dry_run",
                "events": events.map { event in
                    ["eventId": event.eventID, "intervalKey": event.intervalKey,
                     "repositoryRoot": event.valueID, "projectLabel": event.valueLabel,
                     "evidenceDigest": event.evidenceDigest as Any? ?? NSNull()]
                },
                "audit": audit.map { entry in
                    ["intervalKey": entry.intervalKey,
                     "legacyProjectKey": entry.legacyProjectKey,
                     "repositoryRoot": entry.repositoryRoot as Any? ?? NSNull(),
                     "source": entry.source?.rawValue as Any? ?? NSNull(),
                     "evidenceDigest": entry.evidenceDigest as Any? ?? NSNull(),
                     "eventId": entry.eventID as Any? ?? NSNull(),
                     "status": entry.status,
                     "reason": entry.reason as Any? ?? NSNull()]
                },
            ]
        }
    }

    struct LegacyProjectMigrationApplyResult: Equatable {
        var applied: Int
        var alreadyPresent: Int
        var failed: Int
        var backupDigest: String
    }

    // MARK: - Token counts, every part optional

    /// The four token parts, named. Spelled the way the wire spells them, because a part's name
    /// is what a reader is told when the store could not measure it.
    enum Part: String, CaseIterable {
        case inputNew, output, cacheRead, cacheWrite
    }

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

        /// One of the four, by name. The readers below need to say *which* part a row could not
        /// measure, and a name is what travels to a consumer; four hand-written branches in each
        /// of them is how one of them ends up disagreeing with the others.
        subscript(part: Part) -> Int? {
            get {
                switch part {
                case .inputNew: return inputNew
                case .output: return output
                case .cacheRead: return cacheRead
                case .cacheWrite: return cacheWrite
                }
            }
            set {
                switch part {
                case .inputNew: inputNew = newValue
                case .output: output = newValue
                case .cacheRead: cacheRead = newValue
                case .cacheWrite: cacheWrite = newValue
                }
            }
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
        /// Which reading of `input` won, and on what evidence — see ``InputBasis``. Nil only
        /// when the arithmetic never ran, because the source did not carry all four parts.
        var inputBasis: InputBasis?
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
            // write, which is the only remaining evidence — and say that that is what happened,
            // because it is the weakest of these determinations.
            if assistant == .codex, input >= cacheRead {
                reading.counts.inputNew = input - cacheRead
                reading.inputBasis = .includesCacheAssumed
            } else {
                reading.inputBasis = .unstated
            }
            return reading
        }
        if stated == disjoint {
            // The two readings are the same number exactly when there is no cache read to move
            // (or no input to move it out of), so nothing was decided and nothing is at stake.
            reading.inputBasis = disjoint == overlapped ? .readingsAgree : .excludesCache
            return reading
        }
        if stated == overlapped, input >= cacheRead {
            reading.counts.inputNew = input - cacheRead
            reading.inputBasis = .includesCache
            return reading
        }
        if assistant == .codex, input >= cacheRead {
            reading.counts.inputNew = input - cacheRead
            reading.inputBasis = .includesCacheUnreconciled
        } else {
            reading.inputBasis = .unreconciled
        }
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
        guard let year = Int(parts[0]), year > 0,
              let month = Int(parts[1]), let day = Int(parts[2]) else { return false }
        var calendar = Calendar(identifier: .gregorian)
        calendar.locale = Locale(identifier: "en_US_POSIX")
        calendar.timeZone = TimeZone(secondsFromGMT: 0)!
        let components = DateComponents(calendar: calendar, timeZone: calendar.timeZone,
                                        year: year, month: month, day: day)
        guard components.isValidDate(in: calendar), let date = calendar.date(from: components)
        else { return false }
        let roundTrip = calendar.dateComponents([.year, .month, .day], from: date)
        return roundTrip.year == year && roundTrip.month == month && roundTrip.day == day
    }

    /// The local calendar day a reading belongs to. Segments are cut at local midnight so a
    /// month's total is the month the person lived through, not a UTC approximation of it.
    static func localDay(of date: Date, calendar: Calendar = .autoupdatingCurrent) -> String {
        let parts = calendar.dateComponents([.year, .month, .day], from: date)
        return String(format: "%04d-%02d-%02d", parts.year ?? 0, parts.month ?? 0, parts.day ?? 0)
    }

    /// Project identity is the repository root, never the disposable worktree cwd. Task records
    /// already persist Git's common directory; manual Sessions use the nearest `.git` marker.
    /// This does no Git subprocess work on SessionWatch's five-minute checkpoint path.
    static func canonicalProjectKey(projectDir: String?, repositoryCommonDir: String? = nil)
        -> String? {
        if let common = repositoryCommonDir?.trimmingCharacters(in: .whitespacesAndNewlines),
           !common.isEmpty {
            let url = URL(fileURLWithPath: common).standardizedFileURL
            if url.lastPathComponent == ".git" {
                return url.deletingLastPathComponent().path
            }
            let parts = url.pathComponents
            if let git = parts.firstIndex(of: ".git"), git > 0 {
                return NSString.path(withComponents: Array(parts.prefix(git)))
            }
        }
        guard let raw = projectDir?.trimmingCharacters(in: .whitespacesAndNewlines),
              raw.hasPrefix("/") else { return nil }
        var cursor = URL(fileURLWithPath: raw).standardizedFileURL
        var isDirectory: ObjCBool = false
        if FileManager.default.fileExists(atPath: cursor.path, isDirectory: &isDirectory),
           !isDirectory.boolValue { cursor.deleteLastPathComponent() }
        while cursor.path != "/" {
            let marker = cursor.appendingPathComponent(".git")
            var markerDirectory: ObjCBool = false
            if FileManager.default.fileExists(atPath: marker.path,
                                              isDirectory: &markerDirectory) {
                if markerDirectory.boolValue { return cursor.path }
                if let text = try? String(contentsOf: marker, encoding: .utf8),
                   let line = text.split(whereSeparator: \.isNewline).first,
                   line.hasPrefix("gitdir:") {
                    let rawGit = line.dropFirst("gitdir:".count)
                        .trimmingCharacters(in: .whitespacesAndNewlines)
                    let gitDir = rawGit.hasPrefix("/")
                        ? URL(fileURLWithPath: rawGit).standardizedFileURL
                        : cursor.appendingPathComponent(rawGit).standardizedFileURL
                    let commonFile = gitDir.appendingPathComponent("commondir")
                    if let relative = try? String(contentsOf: commonFile, encoding: .utf8)
                        .trimmingCharacters(in: .whitespacesAndNewlines), !relative.isEmpty {
                        let common = gitDir.appendingPathComponent(relative).standardizedFileURL
                        if common.lastPathComponent == ".git" {
                            return common.deletingLastPathComponent().path
                        }
                    }
                }
                return cursor.path
            }
            cursor.deleteLastPathComponent()
        }
        return URL(fileURLWithPath: raw).standardizedFileURL.path
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
        var graphID: String?
        var parentTaskID: String?
        var retryOf: String?
        var attempt: Int?
        var landingState: String?
        var disposition: String?

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
        /// What this reading has to say about the row's coverage, in the order it was decided.
        /// A list rather than a slot, and ``mark(_:)`` rather than an assignment, because a
        /// reading can be true of two of these at once — a task with no known session whose
        /// source also carried no usage is both, and used to arrive as one.
        var coverageReasons: [CoverageReason] = []

        init(assistant: Assistant, sessionID: String, boundaryKind: BoundaryKind,
             boundaryID: String, origin: Origin) {
            self.assistant = assistant
            self.sessionID = sessionID
            self.boundaryKind = boundaryKind
            self.boundaryID = boundaryID
            self.origin = origin
        }

        /// Add a mark. Adding one never removes another, and adding the same one twice is one
        /// mark — the store's copy is a set and this is where that starts.
        mutating func mark(_ reason: CoverageReason) {
            guard !coverageReasons.contains(reason) else { return }
            coverageReasons.append(reason)
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
        var graphID: String?
        var parentTaskID: String?
        var retryOf: String?
        var attempt: Int?
        var landingState: String?
        var disposition: String?
        var billingMode = ""
        var rawUsage: String?
        var counts = Counts()
        var total: Int?
        var sourceTotal: Int?
        var reconciliation: String?
        var inputBasis: String?
        var costValue: Double?
        var costUnit: String?
        var costBasis = ""
        var priceSnapshotID: String?
        var missingReason: String?
        var coverage = ""
        /// Every mark the store put on this row, in the order they were written. See
        /// ``CoverageReason``: this is a set, and the column behind it is one too.
        var coverageReasons: [String] = []
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

        /// **The one seam where a stored row becomes a number for a consumer.** See
        /// ``Measurement``; every reader in this file asks for this and none of them reads the
        /// token columns directly — including the two that used to: the range's ordering, which
        /// asks ``Measurement/incomplete`` instead of re-deciding "incomplete" in SQL, and the
        /// `was` snapshot a correction records.
        var measurement: Measurement {
            var out = Measurement()
            out.counts = counts
            out.total = counts.total
            for part in Part.allCases {
                if let value = counts[part] { out.measured += value }
                else { out.unknownParts.append(part) }
            }
            out.reasons = coverageReasons
            // A synthetic identity is a mark the row carries in its own session id, so it is read
            // off the row here rather than trusted to whichever writer was supposed to note it.
            // The mark has to survive at the reader; that is the whole point of this type.
            //
            // **Added to what the row already says, never instead of it.** Reading it back only
            // when the row was otherwise unmarked is how `source_regressed` disappeared from
            // every reader of a row that was both: the number spans a replaced source *and* the
            // session was invented, and a consumer needs both to know what it is holding.
            let invented = UsageLedger.CoverageReason.sessionUnresolved.rawValue
            if sessionID.hasPrefix(UsageLedger.unresolvedSessionPrefix),
               !out.reasons.contains(invented) {
                out.reasons.append(invented)
            }
            return out
        }
    }

    /// What a reader may render for one stored row, and what it must carry alongside.
    ///
    /// **The defect this type exists to make unrepresentable** was found three times in one
    /// review, in three different readers: the store marks a row — a part it never measured, a
    /// coverage reason, an identity it had to invent — and a reader turns it back into an
    /// ordinary number. `COALESCE(cache_read, 0)` in an aggregate, a row silently dropped out of
    /// a total, a `coverage_reason` the wire payload had no field to carry. Three patches would
    /// have fixed three symptoms; one seam is what stops the fourth reader from doing it again.
    ///
    /// So: the aggregate, the wire payload and the CSV export all read a row through this and
    /// nothing else, and every one of them is handed the marks along with the numbers.
    ///
    /// **And the marks are a set, because the same defect came back from the writer side.** With
    /// one slot and a last writer, a row that was both filed under an invented identity and
    /// measured across a replaced source reached every reader carrying one of those facts —
    /// always the earlier one lost, which was always `source_regressed`. A seam that faithfully
    /// reports a column somebody else already overwrote is not a seam. See ``CoverageReason``.
    struct Measurement: Equatable {
        /// The parts, each still nil where the source measured nothing. Never coalesced.
        var counts = Counts()
        /// The four parts summed, and nil the moment any one of them is unknown — the same rule
        /// ``Counts/total`` and `recomputeTotal` follow, so the three can never drift apart.
        var total: Int?
        /// What *was* measured, summed. A floor rather than a total: a row with three known
        /// parts and one unknown still spent the three, and dropping it out of a range's total
        /// understates the range as surely as counting its unknown part as zero overstates it.
        /// It is never presented without ``unknownParts`` beside it.
        var measured = 0
        /// Which of the four this row could not measure.
        var unknownParts: [Part] = []
        /// **Every** mark the store put on this row — `session_unresolved`, `source_regressed`,
        /// `source_unreadable_at_close`, `no_usage_recorded` — not the last one written. Two of
        /// these can be true of one row, and when they are, both travel: a reader handed only
        /// the newer mark is being told the row is one kind of doubtful when it is two.
        var reasons: [String] = []

        /// Nothing at all was measured: the row that must never be rendered as a number.
        var unknown: Bool { unknownParts.count == Part.allCases.count }
        /// Something was not. The weaker condition, and the one the readers count, because a row
        /// three-quarters measured is exactly the shape that used to arrive looking whole.
        var incomplete: Bool { !unknownParts.isEmpty }
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

    private let queue = DispatchQueue(label: "com.tsunamiworks.clawdline.usageledger")
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
        guard version < UsageLedger.storeVersion else { return }
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
        if version < 2 {
            // Added here rather than in the CREATE above so that a store written by store
            // version 1 and a store created today end up with exactly the same columns, by the
            // same statement. Neither addition rewrites a stored value.
            exec(db, "ALTER TABLE usage_intervals ADD COLUMN input_basis TEXT;")
            // **Re-importing the same evidence has to change nothing.** A correction is a note
            // that something disagrees with a sealed row; four identical imports wrote four of
            // them, which turned the count the route publishes as "a correction is outstanding"
            // into a count of app launches. The duplicates are removed once, the constraint
            // stops them coming back, and `COALESCE` is there because SQLite treats two NULLs as
            // distinct in a unique index — which would have left the same door open.
            exec(db, """
                DELETE FROM usage_corrections WHERE id NOT IN (
                  SELECT MIN(id) FROM usage_corrections
                   GROUP BY interval_key, reason, COALESCE(proposed, ''));
                CREATE UNIQUE INDEX IF NOT EXISTS usage_corrections_once
                  ON usage_corrections (interval_key, reason, COALESCE(proposed, ''));
                """)
        }
        if version < 3 {
            // One name, because the column now holds a **set** of marks rather than the last one
            // written — see ``CoverageReason``. A rename, not a new column: every value already
            // stored is a valid one-element set, so nothing is rewritten and nothing is lost,
            // and leaving the old column behind would leave a second place a future reader could
            // ask for a row's coverage and be told half of it. `schema_version` is deliberately
            // untouched: it is part of every interval key, and no stored *value* changed meaning.
            exec(db, """
                ALTER TABLE usage_intervals
                RENAME COLUMN coverage_reason TO coverage_reasons;
                """)
        }
        if version < 4 {
            // Analytics orders and bounds by the observed instant, not by the machine-local day
            // stored for the forensic surface. This index keeps an old requested month bounded
            // without first materialising every newer row on the Mac.
            exec(db, """
                CREATE INDEX IF NOT EXISTS usage_intervals_started
                  ON usage_intervals (started_at DESC, interval_key DESC);
                """)
        }
        if version < 5 {
            // Classification is mutable knowledge, not mutable accounting. Keep it in an
            // append-only event log beside the immutable interval instead of adding last-writer-
            // wins Feature columns to `usage_intervals`.
            exec(db, """
                CREATE TABLE IF NOT EXISTS usage_attribution_events (
                  event_id TEXT PRIMARY KEY,
                  interval_key TEXT NOT NULL,
                  dimension TEXT NOT NULL,
                  value_id TEXT NOT NULL,
                  value_label TEXT NOT NULL,
                  source TEXT NOT NULL,
                  confidence REAL,
                  classifier_id TEXT,
                  classifier_version TEXT,
                  evidence_digest TEXT,
                  decision TEXT NOT NULL,
                  decision_source TEXT NOT NULL,
                  assigned_at REAL NOT NULL,
                  supersedes_event_id TEXT,
                  FOREIGN KEY(interval_key) REFERENCES usage_intervals(interval_key),
                  FOREIGN KEY(supersedes_event_id) REFERENCES usage_attribution_events(event_id)
                );
                CREATE INDEX IF NOT EXISTS usage_attribution_interval
                  ON usage_attribution_events (interval_key, dimension, assigned_at, event_id);
                """)
        }
        // Each statement above is independent of the ones before it, which is what makes a
        // half-applied migration self-heal: a crash between the ALTER and this line leaves the
        // next launch re-running an ALTER that fails harmlessly (logged) and then finishing.
        exec(db, "PRAGMA user_version=\(UsageLedger.storeVersion);")
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

    /// Append one Project/Feature assignment decision. Event ids make retries idempotent; a
    /// duplicate returns false and never replaces the first event. LLM evidence is represented
    /// only by a SHA-256 digest and classifier snapshot, never raw prompt or transcript text.
    @discardableResult
    func record(_ event: AttributionEvent) -> Bool {
        queue.sync {
            guard valid(event), let db = database() else { return false }
            if let predecessor = event.supersedesEventID,
               !sameAttributionScope(db, eventID: predecessor, event: event,
                                     requireSameValue: event.source == .policy
                                        && event.decision == .accepted) { return false }
            var statement: OpaquePointer?
            defer { sqlite3_finalize(statement) }
            guard sqlite3_prepare_v2(db, """
                INSERT INTO usage_attribution_events
                  (event_id, interval_key, dimension, value_id, value_label, source, confidence,
                   classifier_id, classifier_version, evidence_digest, decision,
                   decision_source, assigned_at, supersedes_event_id)
                VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(event_id) DO NOTHING;
                """, -1, &statement, nil) == SQLITE_OK else { return false }
            bind(statement, 1, event.eventID); bind(statement, 2, event.intervalKey)
            bind(statement, 3, event.dimension.rawValue); bind(statement, 4, event.valueID)
            bind(statement, 5, event.valueLabel); bind(statement, 6, event.source.rawValue)
            bind(statement, 7, event.confidence); bind(statement, 8, event.classifierID)
            bind(statement, 9, event.classifierVersion); bind(statement, 10, event.evidenceDigest)
            bind(statement, 11, event.decision.rawValue); bind(statement, 12, event.decisionSource)
            sqlite3_bind_double(statement, 13, event.assignedAt.timeIntervalSince1970)
            bind(statement, 14, event.supersedesEventID)
            guard sqlite3_step(statement) == SQLITE_DONE else { return false }
            return sqlite3_changes(db) == 1
        }
    }

    /// Every accepted decision nothing has superseded. `acceptedHead(from:)` collapses "none" and
    /// "two of them" into the same nil, and a producer deciding whether it may append another
    /// acceptance has to tell those two apart.
    static func activeAcceptedHeads(from events: [AttributionEvent]) -> [AttributionEvent] {
        let superseded = Set(events.compactMap(\.supersedesEventID))
        return events.filter { $0.decision == .accepted && !superseded.contains($0.eventID) }
    }

    /// Only a single active accepted head is usable. A proposal, rejection, or two conflicting
    /// accepted heads returns nil, so a classifier disagreement cannot silently split totals.
    static func acceptedHead(from events: [AttributionEvent]) -> AttributionEvent? {
        let accepted = activeAcceptedHeads(from: events)
        return accepted.count == 1 ? accepted[0] : nil
    }

    private static func migrationDigest(_ fields: [String]) -> String {
        SHA256.hash(data: Data(fields.joined(separator: "\u{1f}").utf8))
            .map { String(format: "%02x", $0) }.joined()
    }

    private static func migrationPath(_ raw: String) -> String {
        URL(fileURLWithPath: raw).standardizedFileURL.path
    }

    /// One implementation, in `Sources/UsageFeatureClassifier.swift`, because the Feature scope
    /// and the Projects table have to refuse the same paths — see
    /// ``UsageFeatureClassifier/resolveProject(projectKey:acceptedProjectIdentity:)``.
    static func legacyManagedWorktreeTaskID(_ raw: String) -> String? {
        UsageFeatureClassifier.legacyManagedWorktreeTaskID(raw)
    }

    private static func gitCommonDirectory(_ directory: String) -> String? {
        let process = Process(), pipe = Pipe()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/git")
        process.arguments = ["-C", directory, "rev-parse", "--path-format=absolute",
                             "--git-common-dir"]
        process.standardOutput = pipe
        process.standardError = FileHandle.nullDevice
        do { try process.run() } catch { return nil }
        process.waitUntilExit()
        guard process.terminationStatus == 0 else { return nil }
        let output = String(decoding: pipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard !output.isEmpty else { return nil }
        return migrationPath(output)
    }

    private static func repositoryRoot(commonDirectory: String) -> String? {
        let url = URL(fileURLWithPath: commonDirectory)
        guard url.lastPathComponent == ".git" else { return nil }
        let root = migrationPath(url.deletingLastPathComponent().path)
        guard gitCommonDirectory(root) == migrationPath(commonDirectory) else { return nil }
        return root
    }

    /// Build proof directly from a still-existing managed worktree. This is read-only: Git names
    /// its common directory and the canonical repository is independently checked against it.
    static func liveWorktreeProjectMigrationEvidence(
        projectKey: String
    ) -> LegacyProjectMigrationEvidence? {
        guard legacyManagedWorktreeTaskID(projectKey) != nil,
              let common = gitCommonDirectory(projectKey),
              let root = repositoryRoot(commonDirectory: common) else { return nil }
        let legacy = migrationPath(projectKey)
        let reference = "git-common-dir:" + common
        return LegacyProjectMigrationEvidence(
            legacyProjectKey: legacy, repositoryRoot: root,
            source: .gitCommonDirectory, reference: reference,
            evidenceDigest: migrationDigest(["project-migration-evidence-v1", legacy, root,
                                             ProjectMigrationEvidenceSource.gitCommonDirectory.rawValue,
                                             reference]))
    }

    /// Build proof for a deleted worktree from an exact durable registry receipt. The task id must
    /// equal the worktree UUID, both stored worktree paths must equal the legacy key, and the stored
    /// repository and original project_dir must agree on a live Git common directory. A basename or
    /// broker slug is never accepted as evidence.
    static func taskReceiptProjectMigrationEvidence(
        projectKey: String, taskRecords: [[String: Any]]
    ) -> LegacyProjectMigrationEvidence? {
        guard let taskID = legacyManagedWorktreeTaskID(projectKey) else { return nil }
        let legacy = migrationPath(projectKey)
        let matches = taskRecords.compactMap { record -> LegacyProjectMigrationEvidence? in
            guard record["id"] as? String == taskID,
                  let projectDir = record["project_dir"] as? String,
                  let worktree = record["worktree"] as? [String: Any],
                  let path = worktree["path"] as? String,
                  let cwd = worktree["cwd"] as? String,
                  let repository = worktree["repository"] as? String,
                  migrationPath(path) == legacy, migrationPath(cwd) == legacy,
                  migrationPath(repository) == migrationPath(projectDir),
                  let common = gitCommonDirectory(repository),
                  let root = repositoryRoot(commonDirectory: common),
                  root == migrationPath(projectDir) else { return nil }
            let reference = "task-worktree-receipt:" + taskID
            let base = worktree["base"] as? String ?? ""
            return LegacyProjectMigrationEvidence(
                legacyProjectKey: legacy, repositoryRoot: root,
                source: .taskWorktreeReceipt, reference: reference,
                evidenceDigest: migrationDigest(["project-migration-evidence-v1", taskID, legacy,
                                                 root, common, base, reference]))
        }
        guard Set(matches.map(\.repositoryRoot)).count == 1 else { return nil }
        return matches.first
    }

    /// Pure dry-run planner. It emits a deterministic proposal plus an accepted Project event per
    /// legacy interval only when exactly one validated evidence receipt names the repository.
    /// Unresolved rows stay explicit in the audit manifest and receive no event.
    static func planLegacyProjectMigration(
        rows: [Row], evidence: [LegacyProjectMigrationEvidence], assignedAt: Date
    ) -> LegacyProjectMigrationPlan {
        let grouped = Dictionary(grouping: evidence) { migrationPath($0.legacyProjectKey) }
        var events: [AttributionEvent] = []
        var audit: [LegacyProjectMigrationAuditEntry] = []
        for row in rows.sorted(by: { $0.intervalKey < $1.intervalKey }) {
            guard let raw = row.projectKey,
                  legacyManagedWorktreeTaskID(raw) != nil else { continue }
            let legacy = migrationPath(raw)
            let candidates = grouped[legacy] ?? []
            let roots = Set(candidates.map { migrationPath($0.repositoryRoot) })
            guard candidates.count == 1, roots.count == 1, let proof = candidates.first,
                  proof.evidenceDigest.count == 64,
                  proof.evidenceDigest.allSatisfy({ $0.isNumber || ("a"..."f").contains($0) }),
                  legacyManagedWorktreeTaskID(proof.repositoryRoot) == nil,
                  proof.reference.nonEmpty != nil else {
                audit.append(LegacyProjectMigrationAuditEntry(
                    intervalKey: row.intervalKey, legacyProjectKey: legacy,
                    repositoryRoot: nil, source: nil, evidenceDigest: nil, eventID: nil,
                    status: "unresolved",
                    reason: candidates.isEmpty ? "migration_evidence_missing"
                        : "migration_evidence_conflict"))
                continue
            }
            let root = migrationPath(proof.repositoryRoot)
            let proposalID = "project-migration-proposal-v1-" + String(migrationDigest(
                [row.intervalKey, legacy, root, proof.evidenceDigest]).prefix(55))
            let eventID = "project-migration-v1-" + String(migrationDigest(
                [row.intervalKey, legacy, root, proof.evidenceDigest]).prefix(64))
            let proposal = AttributionEvent(
                eventID: proposalID, intervalKey: row.intervalKey, dimension: .project,
                valueID: root,
                valueLabel: URL(fileURLWithPath: root).lastPathComponent,
                source: .manual, confidence: nil,
                classifierID: nil, classifierVersion: nil,
                evidenceDigest: proof.evidenceDigest,
                decision: .proposed,
                decisionSource: "legacy-project-migration-proposal-v1:" + proof.source.rawValue,
                assignedAt: assignedAt, supersedesEventID: nil)
            let event = AttributionEvent(
                eventID: eventID, intervalKey: row.intervalKey, dimension: .project,
                valueID: root,
                valueLabel: URL(fileURLWithPath: root).lastPathComponent,
                source: .policy, confidence: 1,
                classifierID: "clawdline-project-migration",
                classifierVersion: "1", evidenceDigest: proof.evidenceDigest,
                decision: .accepted,
                decisionSource: "legacy-project-migration-v1:" + proof.source.rawValue,
                assignedAt: assignedAt, supersedesEventID: proposalID)
            events.append(contentsOf: [proposal, event])
            audit.append(LegacyProjectMigrationAuditEntry(
                intervalKey: row.intervalKey, legacyProjectKey: legacy,
                repositoryRoot: root, source: proof.source,
                evidenceDigest: proof.evidenceDigest, eventID: eventID,
                status: "resolved", reason: nil))
        }
        return LegacyProjectMigrationPlan(events: events, audit: audit)
    }

    /// Whether the store already holds this event id. It is the second half of the only honest
    /// reading of `record(_:)` returning false: a duplicate id, or a refusal. The Feature
    /// attribution run in `Sources/UsageFeatureAttribution.swift` asks it for the same reason the
    /// legacy Project migration below does.
    func containsAttributionEvent(_ eventID: String) -> Bool {
        queue.sync {
            guard let db = database() else { return false }
            var statement: OpaquePointer?
            defer { sqlite3_finalize(statement) }
            guard sqlite3_prepare_v2(db,
                "SELECT 1 FROM usage_attribution_events WHERE event_id = ? LIMIT 1;",
                -1, &statement, nil) == SQLITE_OK else { return false }
            bind(statement, 1, eventID)
            return sqlite3_step(statement) == SQLITE_ROW
        }
    }

    /// Applying is refused without the caller's SHA-256 backup receipt. Reapplying the same plan
    /// is idempotent because every event id is deterministic and the event store is append-only.
    func applyLegacyProjectMigration(
        _ plan: LegacyProjectMigrationPlan, backupDigest: String
    ) -> LegacyProjectMigrationApplyResult {
        guard backupDigest.count == 64,
              backupDigest.allSatisfy({ $0.isNumber || ("a"..."f").contains($0) }) else {
            return LegacyProjectMigrationApplyResult(applied: 0, alreadyPresent: 0,
                                                     failed: plan.events.count,
                                                     backupDigest: backupDigest)
        }
        var applied = 0, already = 0, failed = 0
        for event in plan.events {
            if record(event) { applied += 1 }
            else if containsAttributionEvent(event.eventID) { already += 1 }
            else { failed += 1 }
        }
        return LegacyProjectMigrationApplyResult(applied: applied, alreadyPresent: already,
                                                 failed: failed, backupDigest: backupDigest)
    }

    /// Logical rollback is append-only too: one deterministic rejection supersedes each migration
    /// event, returning those rows to Unknown Project. Restoring the pre-migration SQLite backup is
    /// the byte-for-byte recovery path and is documented beside the dry-run contract.
    func rollbackLegacyProjectMigration(
        _ plan: LegacyProjectMigrationPlan, backupDigest: String, assignedAt: Date
    ) -> LegacyProjectMigrationApplyResult {
        let rollbackEvents = plan.events.filter { $0.decision == .accepted }.map { original in
            AttributionEvent(
                eventID: "project-migration-rollback-v1-"
                    + String(Self.migrationDigest([original.eventID]).prefix(55)),
                intervalKey: original.intervalKey, dimension: .project,
                valueID: original.valueID, valueLabel: original.valueLabel,
                source: .policy, confidence: 1,
                classifierID: "clawdline-project-migration",
                classifierVersion: "1", evidenceDigest: original.evidenceDigest,
                decision: .rejected, decisionSource: "legacy-project-migration-rollback-v1",
                assignedAt: assignedAt, supersedesEventID: original.eventID)
        }
        return applyLegacyProjectMigration(
            LegacyProjectMigrationPlan(events: rollbackEvents, audit: plan.audit),
            backupDigest: backupDigest)
    }

    func resolvedAttribution(intervalKey: String, dimension: AttributionDimension)
        -> AttributionEvent? {
        queue.sync {
            guard let db = database() else { return nil }
            let events = attributionEvents(db, intervalKey: intervalKey, dimension: dimension)
            return Self.acceptedHead(from: events)
        }
    }

    /// The accepted heads this interval already carries, superseded ones excluded. A machine
    /// about to append another acceptance reads this first: appending beside an existing head
    /// makes the interval resolve to nothing, which is how an interval silently leaves its
    /// Feature for `Unknown`.
    func activeAcceptedAttribution(intervalKey: String, dimension: AttributionDimension)
        -> [AttributionEvent] {
        queue.sync {
            guard let db = database() else { return [] }
            return Self.activeAcceptedHeads(
                from: attributionEvents(db, intervalKey: intervalKey, dimension: dimension))
        }
    }

    private func valid(_ event: AttributionEvent) -> Bool {
        func short(_ text: String, _ limit: Int) -> Bool {
            !text.isEmpty && text.count <= limit
                && !text.unicodeScalars.contains { CharacterSet.controlCharacters.contains($0) }
        }
        guard short(event.eventID, 128), short(event.intervalKey, 128),
              short(event.valueID, 128), short(event.valueLabel, 120),
              short(event.decisionSource, 120), event.assignedAt.timeIntervalSince1970.isFinite,
              event.confidence.map({ $0.isFinite && (0...1).contains($0) }) ?? true
        else { return false }
        if event.source == .llm || event.source == .policy || event.source == .heuristic {
            guard let classifier = event.classifierID, short(classifier, 120),
                  let version = event.classifierVersion, short(version, 120),
                  let digest = event.evidenceDigest, digest.count == 64,
                  digest.allSatisfy({ $0.isNumber || ("a"..."f").contains($0) }),
                  event.confidence != nil else { return false }
        }
        if event.source == .policy && event.decision == .accepted {
            guard event.supersedesEventID != nil else { return false }
        }
        return true
    }

    private func sameAttributionScope(_ db: OpaquePointer, eventID: String,
                                      event: AttributionEvent, requireSameValue: Bool) -> Bool {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            SELECT interval_key, dimension, value_id FROM usage_attribution_events
             WHERE event_id = ?;
            """, -1, &statement, nil) == SQLITE_OK else { return false }
        bind(statement, 1, eventID)
        guard sqlite3_step(statement) == SQLITE_ROW else { return false }
        guard Self.text(statement, 0) == event.intervalKey,
              Self.text(statement, 1) == event.dimension.rawValue else { return false }
        return !requireSameValue || Self.text(statement, 2) == event.valueID
    }

    private func attributionEvents(_ db: OpaquePointer, intervalKey: String,
                                   dimension: AttributionDimension) -> [AttributionEvent] {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            SELECT event_id, interval_key, dimension, value_id, value_label, source, confidence,
                   classifier_id, classifier_version, evidence_digest, decision,
                   decision_source, assigned_at, supersedes_event_id
              FROM usage_attribution_events WHERE interval_key = ? AND dimension = ?
             ORDER BY assigned_at, event_id;
            """, -1, &statement, nil) == SQLITE_OK else { return [] }
        bind(statement, 1, intervalKey); bind(statement, 2, dimension.rawValue)
        var out: [AttributionEvent] = []
        while sqlite3_step(statement) == SQLITE_ROW {
            guard let eventID = Self.text(statement, 0),
                  let storedInterval = Self.text(statement, 1),
                  let rawDimension = Self.text(statement, 2),
                  let storedDimension = AttributionDimension(rawValue: rawDimension),
                  let valueID = Self.text(statement, 3), let label = Self.text(statement, 4),
                  let rawSource = Self.text(statement, 5),
                  let source = AttributionSource(rawValue: rawSource),
                  let rawDecision = Self.text(statement, 10),
                  let decision = AttributionDecision(rawValue: rawDecision),
                  let decisionSource = Self.text(statement, 11),
                  let assigned = Self.double(statement, 12) else { continue }
            out.append(AttributionEvent(
                eventID: eventID, intervalKey: storedInterval, dimension: storedDimension,
                valueID: valueID, valueLabel: label, source: source,
                confidence: Self.double(statement, 6), classifierID: Self.text(statement, 7),
                classifierVersion: Self.text(statement, 8), evidenceDigest: Self.text(statement, 9),
                decision: decision, decisionSource: decisionSource,
                assignedAt: Date(timeIntervalSince1970: assigned),
                supersedesEventID: Self.text(statement, 13)))
        }
        return out
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
            // A cumulative counter that went backwards means the source was replaced, rotated or
            // truncated under the same session id. The reading is never subtracted and never
            // attributed — but the cursor **re-anchors to it**, because leaving the anchor at a
            // high-water mark the replacement will not reach again silently deletes everything
            // that source goes on to record below it, and reports the result as a healthy row.
            // The trade is deliberate and it is the reason the mark is written first: a source
            // that dipped and recovered can now be counted twice across the dip, and
            // `source_regressed` is what tells every reader that this row's number was measured
            // across a seam rather than read off one continuous counter.
            if delta.regressed { mark(db, key: key, reason: .sourceRegressed) }
            else { write(db, key: key, delta: delta, sample: sample, reading: reading) }
            current = Self.advance(current, by: reading)
        }
        current.model = sample.model ?? current.model
        current.localDay = day
        if let bytes = sample.sourceBytes { current.sourceBytes = bytes }
        updateLineage(db, key: key, sample: sample)
        updateFacts(db, key: key, sample: sample, reading: reading)
        // Added to whatever this call has already put on the row — the regression above included.
        // Both are true of it, and both are what the row is for.
        for reason in sample.coverageReasons { mark(db, key: key, reason: reason) }
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
              local_day, observed_at, updated_at, graph_id, parent_task_id, retry_of, attempt,
              landing_state, disposition)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,?,?,?,?,?,?,?,?,?)
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
        bind(statement, 29, sample.graphID)
        bind(statement, 30, sample.parentTaskID)
        bind(statement, 31, sample.retryOf)
        bind(statement, 32, sample.attempt)
        bind(statement, 33, sample.landingState)
        bind(statement, 34, sample.disposition)
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
            UPDATE usage_intervals SET usage_raw = ?, source_total = ?, reconciliation = ?,
                   input_basis = ?
            WHERE interval_key = ? AND sealed = 0;
            """, -1, &statement, nil) == SQLITE_OK else { return }
        bind(statement, 1, String(decoding: data, as: UTF8.self))
        bind(statement, 2, reading.sourceTotal)
        bind(statement, 3, reading.reconciliation)
        bind(statement, 4, reading.inputBasis?.rawValue)
        bind(statement, 5, key)
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

    /// Lineage is descriptive metadata and may arrive after the immutable usage is sealed — a
    /// landing is normally recorded later by the root. Filling metadata never changes token or
    /// cost columns; identity conflicts are ignored, while landing state may advance.
    private func updateLineage(_ db: OpaquePointer, key: String, sample: Sample) {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            UPDATE usage_intervals SET
              project_key = COALESCE(project_key, ?),
              graph_id = COALESCE(graph_id, ?),
              parent_task_id = COALESCE(parent_task_id, ?),
              retry_of = COALESCE(retry_of, ?),
              attempt = COALESCE(attempt, ?),
              landing_state = COALESCE(?, landing_state),
              disposition = COALESCE(disposition, ?)
            WHERE interval_key = ?;
            """, -1, &statement, nil) == SQLITE_OK else { return }
        bind(statement, 1, sample.projectKey); bind(statement, 2, sample.graphID)
        bind(statement, 3, sample.parentTaskID); bind(statement, 4, sample.retryOf)
        bind(statement, 5, sample.attempt); bind(statement, 6, sample.landingState)
        bind(statement, 7, sample.disposition); bind(statement, 8, key)
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

    /// Add one mark to a row's coverage. **A union, never an assignment.**
    ///
    /// The `instr` is the union: a mark already in the set leaves the column exactly as it was,
    /// and a new one is appended. It is `instr` rather than `LIKE` so that no character in a
    /// mark can be read as a wildcard, and the separators around both sides are what stop
    /// `source_regressed` from matching inside a longer word.
    ///
    /// This being an assignment is the whole of the defect this round exists to close: `apply()`
    /// writes `source_regressed` and then the sample's own mark in the same call, so with one
    /// slot the first was never once observable by anything.
    private func mark(_ db: OpaquePointer, key: String, reason: CoverageReason) {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            UPDATE usage_intervals SET coverage_reasons = CASE
                WHEN coverage_reasons IS NULL OR coverage_reasons = '' THEN ?1
                WHEN instr(' ' || coverage_reasons || ' ', ' ' || ?1 || ' ') > 0
                     THEN coverage_reasons
                ELSE coverage_reasons || ' ' || ?1 END,
              updated_at = ?2
            WHERE interval_key = ?3 AND sealed = 0;
            """, -1, &statement, nil) == SQLITE_OK else { return }
        bind(statement, 1, reason.rawValue)
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
                     reason: CoverageReason? = nil, at: Date = Date()) {
        queue.async { [weak self] in
            guard let self, let db = self.database() else { return }
            guard let current = self.cursor(db, assistant: assistant.rawValue,
                                            session: sessionID),
                  let key = current.openKey else { return }
            if let reason { self.mark(db, key: key, reason: reason) }
            self.sealRow(db, key: key, coverage: coverage, at: at)
        }
    }

    // MARK: - Corrections

    /// A measurement that disagrees with a sealed row. Written as new metadata, because a value
    /// that may already have appeared in a month's total is never silently rewritten.
    ///
    /// **Idempotent, and that is load-bearing.** The backfill re-reads the whole registry on
    /// every launch, so a correction that is written again each time it is proposed turns
    /// `corrections` — which the route publishes as *something disagrees with a sealed row* —
    /// into a count of how often the app has been started. Four identical imports wrote four of
    /// these. So an identical note already on the row is not written twice, in code because the
    /// answer is wanted here, and in the unique index because a second writer will not remember.
    @discardableResult
    private func correction(intervalKey: String, reason: String,
                            proposed: [String: Any]) -> Bool {
        guard let db = database(),
              let proposedData = try? JSONSerialization.data(withJSONObject: proposed,
                                                             options: [.sortedKeys])
        else { return false }
        let proposedJSON = String(decoding: proposedData, as: UTF8.self)
        guard !hasCorrection(db, intervalKey: intervalKey, reason: reason,
                             proposed: proposedJSON) else { return false }
        // Through the seam like every other reader, rather than off the columns. It is honest
        // either way today — the nils are already written as `NSNull` — but "honest today and
        // nothing enforces it" is what the seam exists to replace: this snapshot is what a
        // person compares a disputed month against, and it now cannot disagree with the numbers
        // the route and the export gave them.
        let was = row(db, key: intervalKey).map { row -> [String: Any] in
            let measurement = row.measurement
            return ["input_new": measurement.counts.inputNew as Any? ?? NSNull(),
                    "output": measurement.counts.output as Any? ?? NSNull(),
                    "cache_read": measurement.counts.cacheRead as Any? ?? NSNull(),
                    "cache_write": measurement.counts.cacheWrite as Any? ?? NSNull(),
                    "cost_value": row.costValue as Any? ?? NSNull()]
        }
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            INSERT OR IGNORE INTO usage_corrections
                   (interval_key, reason, was, proposed, written_at)
            VALUES (?,?,?,?,?);
            """, -1, &statement, nil) == SQLITE_OK else { return false }
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
        bind(statement, 4, proposedJSON)
        sqlite3_bind_double(statement, 5, Date().timeIntervalSince1970)
        guard sqlite3_step(statement) == SQLITE_DONE else { return false }
        return sqlite3_changes(db) > 0
    }

    /// Whether this exact note is already on the row. On the ledger's own queue, like everything
    /// else that touches the handle.
    private func hasCorrection(_ db: OpaquePointer, intervalKey: String, reason: String,
                               proposed: String) -> Bool {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            SELECT 1 FROM usage_corrections
             WHERE interval_key = ? AND reason = ? AND COALESCE(proposed, '') = ? LIMIT 1;
            """, -1, &statement, nil) == SQLITE_OK else { return false }
        bind(statement, 1, intervalKey)
        bind(statement, 2, reason)
        bind(statement, 3, proposed)
        return sqlite3_step(statement) == SQLITE_ROW
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
               cost_basis, price_snapshot_id, missing_reason, coverage, coverage_reasons, sealed,
               source_bytes, started_at, ended_at, local_day, updated_at, input_basis,
               graph_id, parent_task_id, retry_of, attempt, landing_state, disposition
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
        row.coverageReasons = coverageReasons(stored: text(statement, 35))
        row.sealed = (integer(statement, 36) ?? 0) == 1
        row.sourceBytes = integer(statement, 37)
        row.startedAt = Date(timeIntervalSince1970: double(statement, 38) ?? 0)
        row.endedAt = double(statement, 39).map { Date(timeIntervalSince1970: $0) }
        row.localDay = text(statement, 40) ?? ""
        row.updatedAt = Date(timeIntervalSince1970: double(statement, 41) ?? 0)
        row.inputBasis = text(statement, 42)
        row.graphID = text(statement, 43)
        row.parentTaskID = text(statement, 44)
        row.retryOf = text(statement, 45)
        row.attempt = integer(statement, 46)
        row.landingState = text(statement, 47)
        row.disposition = text(statement, 48)
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

    /// Rows inside a local-day range. **Rows that could not measure something come first** —
    /// any part unknown, not only all four — because the sessions most likely to go missing are
    /// the long ones and a reader who never scrolls would otherwise never see the bias in the
    /// totals below them. A row three-quarters measured biases a total exactly as a row nobody
    /// could read does, only by less.
    ///
    /// **Which rows those are is the seam's to say, not SQL's.** The order used to be decided by
    /// a `NULL` test written out in the `ORDER BY`, which agreed with ``Measurement/incomplete``
    /// only for as long as nobody widened one of them; the day the seam counts a marked row as
    /// incomplete, a sort that never asked it would quietly disagree — and this ordering is the
    /// bias protection the whole surface rests on. SQL puts the range in a stable order and the
    /// partition below is a stable one, so rows equal on the seam keep it.
    func rows(from: String? = nil, to: String? = nil) -> [Row] {
        queue.sync {
            guard let db = database() else { return [] }
            var statement: OpaquePointer?
            defer { sqlite3_finalize(statement) }
            guard sqlite3_prepare_v2(db, Self.selection + """
                 WHERE (? IS NULL OR local_day >= ?) AND (? IS NULL OR local_day <= ?)
                 ORDER BY local_day, started_at, segment_no;
                """, -1, &statement, nil) == SQLITE_OK else { return [] }
            bind(statement, 1, from); bind(statement, 2, from)
            bind(statement, 3, to); bind(statement, 4, to)
            var out: [Row] = []
            while sqlite3_step(statement) == SQLITE_ROW { out.append(Self.row(from: statement)) }
            return out.filter { $0.measurement.incomplete }
                + out.filter { !$0.measurement.incomplete }
        }
    }

    /// The complete analytics predicate, expressed at the SQLite boundary. `limit` is always the
    /// public scan ceiling plus one: that extra row is the proof that the *matching query* was
    /// truncated, while a narrow old range remains readable no matter how large the newer store
    /// has grown. The forensic `rows(from:to:)` reader above deliberately keeps its historical
    /// incomplete-first ordering; analytics has a separate, documented newest-first contract.
    struct AnalyticsFilter {
        var start: Date?
        var end: Date?
        var assistant: String?
        var model: String?
        var origin: String?
        var project: String?
        var limit: Int
        /// The equal previous-range read needs token rows, not Feature attribution. Keeping this
        /// choice at the SQLite seam avoids repeating the attribution join whose result would be
        /// discarded by every comparison.
        var includeFeatureAttribution = true
    }

    struct AnalyticsRead {
        var rows: [Row]
        var corrections: Int
        var latestLedgerObservation: Date?
        var acceptedFeatures: [String: AcceptedAttribution]
        var acceptedProjects: [String: AcceptedAttribution]
    }

    func analyticsRead(_ filter: AnalyticsFilter) -> AnalyticsRead {
        queue.sync {
            guard let db = database() else {
                return AnalyticsRead(rows: [], corrections: 0, latestLedgerObservation: nil,
                                     acceptedFeatures: [:], acceptedProjects: [:])
            }
            let predicate = """
                (? IS NULL OR started_at >= ?) AND (? IS NULL OR started_at < ?)
                AND (? IS NULL OR assistant = ?)
                AND (? IS NULL OR model = ?)
                AND (? IS NULL OR origin = ?)
                AND (? IS NULL OR rtrim(COALESCE(project_key, working_dir), '/') = ?
                     OR rtrim(COALESCE(project_key, working_dir), '/') LIKE ? ESCAPE '\\'
                     OR interval_key IN (
                       SELECT e.interval_key FROM usage_attribution_events e
                        WHERE e.dimension = 'project' AND e.decision = 'accepted'
                          AND NOT EXISTS (
                            SELECT 1 FROM usage_attribution_events successor
                             WHERE successor.supersedes_event_id = e.event_id
                          )
                        GROUP BY e.interval_key
                       HAVING COUNT(*) = 1 AND MAX(e.value_label) = ?
                     ))
                """
            func escapedLike(_ value: String) -> String {
                value.replacingOccurrences(of: "\\", with: "\\\\")
                    .replacingOccurrences(of: "%", with: "\\%")
                    .replacingOccurrences(of: "_", with: "\\_")
            }
            func bindFilter(_ statement: OpaquePointer?) {
                let start = filter.start?.timeIntervalSince1970
                let end = filter.end?.timeIntervalSince1970
                bind(statement, 1, start); bind(statement, 2, start)
                bind(statement, 3, end); bind(statement, 4, end)
                bind(statement, 5, filter.assistant); bind(statement, 6, filter.assistant)
                bind(statement, 7, filter.model); bind(statement, 8, filter.model)
                bind(statement, 9, filter.origin); bind(statement, 10, filter.origin)
                bind(statement, 11, filter.project); bind(statement, 12, filter.project)
                bind(statement, 13, filter.project.map { "%/" + escapedLike($0) })
                bind(statement, 14, filter.project)
            }

            var rowsStatement: OpaquePointer?
            defer { sqlite3_finalize(rowsStatement) }
            var rows: [Row] = []
            if sqlite3_prepare_v2(db, Self.selection + " WHERE " + predicate
                + " ORDER BY started_at DESC, interval_key DESC LIMIT ?;",
                -1, &rowsStatement, nil) == SQLITE_OK {
                bindFilter(rowsStatement)
                bind(rowsStatement, 15, filter.limit)
                while sqlite3_step(rowsStatement) == SQLITE_ROW {
                    rows.append(Self.row(from: rowsStatement))
                }
            }

            // Corrections describe the same bounded subject as every other figure in a partial
            // response. The subquery is bounded by the identical predicate and order, so this
            // count cannot turn a capped analytics request back into an unbounded ledger read.
            var correctionsStatement: OpaquePointer?
            defer { sqlite3_finalize(correctionsStatement) }
            var corrections = 0
            let correctionsSQL = """
                SELECT COUNT(*) FROM usage_corrections c JOIN (
                  SELECT interval_key FROM usage_intervals WHERE \(predicate)
                   ORDER BY started_at DESC, interval_key DESC LIMIT ?
                ) matched ON matched.interval_key = c.interval_key;
                """
            if sqlite3_prepare_v2(db, correctionsSQL, -1, &correctionsStatement, nil)
                == SQLITE_OK {
                bindFilter(correctionsStatement)
                bind(correctionsStatement, 15, filter.limit)
                if sqlite3_step(correctionsStatement) == SQLITE_ROW {
                    corrections = Int(sqlite3_column_int64(correctionsStatement, 0))
                }
            }

            var attributionEvents: [AttributionDimension: [String: [AttributionEvent]]] = [:]
            do {
                // Resolve Feature attribution for this exact bounded subject in one query. Doing
                // one lookup per row turns a 100k-row dashboard into 100k SQLite statements;
                // joining the same matched subquery keeps the read bounded and holds the observed
                // intervals still.
                var featureStatement: OpaquePointer?
                defer { sqlite3_finalize(featureStatement) }
                let featureSQL = """
                    SELECT e.event_id, e.interval_key, e.dimension, e.value_id, e.value_label,
                           e.source, e.confidence, e.classifier_id, e.classifier_version,
                           e.evidence_digest, e.decision, e.decision_source, e.assigned_at,
                           e.supersedes_event_id
                      FROM usage_attribution_events e JOIN (
                        SELECT interval_key FROM usage_intervals WHERE \(predicate)
                         ORDER BY started_at DESC, interval_key DESC LIMIT ?
                      ) matched ON matched.interval_key = e.interval_key
                     WHERE e.dimension IN (?, ?)
                     ORDER BY e.interval_key, e.assigned_at, e.event_id;
                    """
                if sqlite3_prepare_v2(db, featureSQL, -1, &featureStatement, nil) == SQLITE_OK {
                    bindFilter(featureStatement)
                    bind(featureStatement, 15, filter.limit)
                    bind(featureStatement, 16, filter.includeFeatureAttribution
                         ? AttributionDimension.feature.rawValue
                         : AttributionDimension.project.rawValue)
                    bind(featureStatement, 17, AttributionDimension.project.rawValue)
                    while sqlite3_step(featureStatement) == SQLITE_ROW {
                        guard let eventID = Self.text(featureStatement, 0),
                              let intervalKey = Self.text(featureStatement, 1),
                              let rawDimension = Self.text(featureStatement, 2),
                              let dimension = AttributionDimension(rawValue: rawDimension),
                              let valueID = Self.text(featureStatement, 3),
                              let valueLabel = Self.text(featureStatement, 4),
                              let rawSource = Self.text(featureStatement, 5),
                              let source = AttributionSource(rawValue: rawSource),
                              let rawDecision = Self.text(featureStatement, 10),
                              let decision = AttributionDecision(rawValue: rawDecision),
                              let decisionSource = Self.text(featureStatement, 11),
                              let assigned = Self.double(featureStatement, 12) else { continue }
                        attributionEvents[dimension, default: [:]][intervalKey, default: []]
                            .append(AttributionEvent(
                            eventID: eventID, intervalKey: intervalKey, dimension: dimension,
                            valueID: valueID, valueLabel: valueLabel, source: source,
                            confidence: Self.double(featureStatement, 6),
                            classifierID: Self.text(featureStatement, 7),
                            classifierVersion: Self.text(featureStatement, 8),
                            evidenceDigest: Self.text(featureStatement, 9), decision: decision,
                            decisionSource: decisionSource,
                            assignedAt: Date(timeIntervalSince1970: assigned),
                            supersedesEventID: Self.text(featureStatement, 13)))
                    }
                }
            }
            var acceptedFeatures: [String: AcceptedAttribution] = [:]
            for (intervalKey, events) in attributionEvents[.feature] ?? [:] {
                if let head = Self.acceptedHead(from: events) {
                    acceptedFeatures[intervalKey] = AcceptedAttribution(
                        id: head.valueID, label: head.valueLabel)
                }
            }
            var acceptedProjects: [String: AcceptedAttribution] = [:]
            for (intervalKey, events) in attributionEvents[.project] ?? [:] {
                if let head = Self.acceptedHead(from: events) {
                    acceptedProjects[intervalKey] = AcceptedAttribution(
                        id: head.valueID, label: head.valueLabel)
                }
            }

            var latestStatement: OpaquePointer?
            defer { sqlite3_finalize(latestStatement) }
            var latest: Date?
            if sqlite3_prepare_v2(db, "SELECT MAX(updated_at) FROM usage_intervals;", -1,
                                  &latestStatement, nil) == SQLITE_OK,
               sqlite3_step(latestStatement) == SQLITE_ROW,
               let seconds = Self.double(latestStatement, 0) {
                latest = Date(timeIntervalSince1970: seconds)
            }
            return AnalyticsRead(rows: rows, corrections: corrections,
                                 latestLedgerObservation: latest,
                                 acceptedFeatures: acceptedFeatures,
                                 acceptedProjects: acceptedProjects)
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
        /// Rows counted in `rows` that could not measure **something** — not only the rows that
        /// measured nothing at all. A row with three parts known and one NULL used to take the
        /// same path as a fully measured one, which rendered the unknown part as `0` and left
        /// this count at zero: the aggregate said "nothing missing here" about the exact row it
        /// had just guessed at.
        var tokenRowsUnknown = 0
        /// Per part, how many rows could not measure it — so a summed column that is short says
        /// so beside itself rather than in a footnote. A part **no** row measured stays nil in
        /// `tokens` and is never a zero.
        var partsUnknown: [String: Int] = [:]
        /// Why rows in this bucket are marked, counted — `session_unresolved`,
        /// `source_regressed`, `source_unreadable_at_close`. Until this existed the route had no
        /// field that could carry a coverage reason at all, so a session filed under an invented
        /// identity and a session read across a rotated transcript both arrived at a consumer
        /// looking exactly like a healthy one.
        ///
        /// **A row marked twice is counted under both**, so these counts can sum to more than
        /// `rows` and that is the correct reading of them: they answer *how many rows carry this
        /// mark*, never *how many rows are marked*, which is what `coverage` and
        /// `tokenRowsUnknown` are for.
        var coverageReasons: [String: Int] = [:]
        /// Money summed **per unit**, never across them. Codex rows can only ever carry credits
        /// or nothing, and adding credits to dollars would be a number with no meaning.
        var costByUnit: [String: Double] = [:]
        var costRows = 0
        var costBases: [String: Int] = [:]
        var unpricedRows = 0
        var missingReasons: [String: Int] = [:]
        var coverage: [String: Int] = [:]

        /// **Every row enters this bucket through ``Row/measurement`` and nothing else.** A part
        /// is summed over the rows that measured it and stays nil where none did; a part some row
        /// could not measure is counted in `partsUnknown` beside its own column; and the total is
        /// the sum of what was measured, so a row three-quarters known contributes its three
        /// quarters instead of either inventing a zero or vanishing out of the range.
        mutating func add(_ row: Row) {
            rows += 1
            coverage[row.coverage, default: 0] += 1
            let measurement = row.measurement
            for reason in measurement.reasons { coverageReasons[reason, default: 0] += 1 }
            if measurement.incomplete { tokenRowsUnknown += 1 }
            var counts = tokens ?? Counts()
            var measured = false
            for part in Part.allCases {
                guard let value = measurement.counts[part] else {
                    partsUnknown[part.rawValue, default: 0] += 1
                    continue
                }
                counts[part] = (counts[part] ?? 0) + value
                measured = true
            }
            if measured {
                tokens = counts
                total = (total ?? 0) + measurement.measured
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
                // Which part was short, and on how many rows. A summed column with no such note
                // beside it is a column every row measured.
                "tokenPartsUnknown": bucket.partsUnknown,
                "total": bucket.total as Any? ?? NSNull(),
                "tokenRowsUnknown": bucket.tokenRowsUnknown,
                "cost": cost,
                "unpriced": ["rows": bucket.unpricedRows, "reasons": bucket.missingReasons],
                "coverage": bucket.coverage,
                // The marks the store put on these rows, reaching the wire. `session_unresolved`
                // means a session's cumulative counters may have been attributed twice under an
                // invented identity, and `source_regressed` means a number was measured across a
                // replaced source — neither is visible in `coverage`, which says only how much
                // of the source was read. A row that is both is counted under both, so these
                // can sum past `rows`.
                "coverageReasons": bucket.coverageReasons]
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
            "unavailable": ["columns": unavailableDimensions, "why": reservedColumnsReason],
        ]
    }

    // MARK: - The export

    static let exportColumns = [
        "interval_key", "local_day", "assistant", "session_id", "boundary_kind", "boundary_id",
        "segment_no", "segment_reason", "origin", "task_id", "schedule_id", "project_key",
        "working_dir", "kind_raw", "isolation", "depth", "claim_count", "timeout_seconds",
        "task_state", "model", "reasoning_effort", "billing_mode", "input_new", "output",
        "cache_read", "cache_write", "total", "measured", "source_total", "reconciliation",
        "input_basis", "cost_value", "cost_unit", "cost_basis", "price_snapshot_id",
        "missing_reason", "coverage", "coverage_reasons", "sealed", "started_at", "ended_at",
    ] + lineageColumns

    /// The whole range as CSV. **An unknown is an empty field, never `0`**. Store version 5 carries the
    /// lineage facts the broker actually knows and leaves only unavailable facts empty.
    ///
    /// **`total` and `measured` are two different quantities and the file carries both.** `total`
    /// is strict — empty the moment any one part is unknown — and `measured` is the sum of what
    /// was actually measured, which is the quantity the route's `total` sums. With only the
    /// strict column here, an export of a range containing one partly measured row could no
    /// longer be added up to the number the route had just given for the same range, and a
    /// figure that cannot be reconciled with the figure beside it is the thing this store exists
    /// to stop. `measured` is empty, not `0`, for a row that measured nothing at all — the same
    /// rule the aggregate follows, so the two agree on every range including that one.
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
            // The same seam the aggregate reads through, for the same reason: the export was
            // already honest about a NULL, and it stays honest because it is asking the one type
            // that cannot hand out a coalesced zero rather than because it remembers to.
            let measurement = row.measurement
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
            fields.append(number(measurement.counts.inputNew))
            fields.append(number(measurement.counts.output))
            fields.append(number(measurement.counts.cacheRead))
            fields.append(number(measurement.counts.cacheWrite))
            fields.append(number(measurement.total))
            fields.append(measurement.unknown ? "" : String(measurement.measured))
            fields.append(number(row.sourceTotal))
            fields.append(row.reconciliation ?? "")
            fields.append(row.inputBasis ?? "")
            fields.append(row.costValue.map { String($0) } ?? "")
            fields.append(row.costUnit ?? "")
            fields.append(row.costBasis)
            fields.append(row.priceSnapshotID ?? "")
            fields.append(row.missingReason ?? "")
            fields.append(row.coverage)
            // Every mark, space-separated, and never only the newest one.
            fields.append(measurement.reasons.joined(separator: " "))
            fields.append(row.sealed ? "1" : "0")
            fields.append(formatter.string(from: row.startedAt))
            fields.append(row.endedAt.map { formatter.string(from: $0) } ?? "")
            fields.append(row.graphID ?? "")
            fields.append(row.parentTaskID ?? "")
            fields.append(row.retryOf ?? "")
            fields.append(number(row.attempt))
            fields.append(row.landingState ?? "")
            fields.append(row.disposition ?? "")
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
        let usage = record["usage"] as? [String: Any]

        // **A record that has never had a session is not a session.** Nothing was observed: no
        // transcript, no counters, not even empty ones — so a row here would describe work that
        // was *anticipated* rather than work that happened, and it would be joined by a second,
        // real row the moment the task actually ran. The backfill runs over the whole registry
        // on every launch, so without this the queued half of the registry became permanent
        // unmeasured rows: five queued tasks beside one genuinely unreadable session read as six
        // unmeasured rows, which buries the one gap that was real under five that never were.
        guard known != nil || usage != nil else { return false }

        var sample = Sample(assistant: assistant,
                            sessionID: known ?? "\(UsageLedger.unresolvedSessionPrefix)\(id)",
                            boundaryKind: .task, boundaryID: id, origin: origin)
        if known == nil { sample.mark(.sessionUnresolved) }
        sample.observedAt = (record["finished_at"] as? Double).map {
            Date(timeIntervalSince1970: $0)
        } ?? now
        sample.taskID = id
        sample.scheduleID = record["schedule_id"] as? String
        let worktree = record["worktree"] as? [String: Any]
        sample.projectKey = UsageLedger.canonicalProjectKey(
            projectDir: record["project_dir"] as? String,
            repositoryCommonDir: record["repository_common_dir"] as? String)
        sample.workingDir = (worktree?["cwd"] as? String) ?? (record["project_dir"] as? String)
        sample.kindRaw = record["kind"] as? String
        sample.isolation = (record["isolation"] as? String) ?? Orchestrator.Isolation.none.rawValue
        sample.depth = record["depth"] as? Int
        sample.claimCount = (record["claims"] as? [String])?.count
            ?? (record["claim_keys"] as? [String])?.count
        sample.timeoutSeconds = (record["timeout_minutes"] as? Int).map { $0 * 60 }
        sample.taskState = state
        sample.reasoningEffort = record["reasoning_effort"] as? String
        sample.graphID = record["graph_id"] as? String
        sample.parentTaskID = record["parent_task"] as? String
        sample.retryOf = record["respawn_of"] as? String
        sample.attempt = record["respawn_generation"] as? Int
        sample.landingState = (record["landing"] as? [String: Any])?["state"] as? String
        sample.disposition = record["disposition"] as? String
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
        // Added, not defaulted: a task whose session nobody ever knew *and* whose record carried
        // no usage is two different holes in the same row, and `??` reported only the first.
        if usage == nil && terminal { sample.mark(.noUsageRecorded) }

        // A row that is already sealed and disagrees with this reading is a correction, never a
        // rewrite: the earlier number may already have been quoted in a month's total.
        //
        // **An import whose content equals what is already recorded is a no-op**, and "already
        // recorded" means both halves of the record: the sealed row's own object, and the
        // corrections already standing against it. The second half is not decoration — a sealed
        // row's `usage_raw` is deliberately frozen, because rewriting it would destroy the
        // evidence of what was actually sealed, so a comparison against that column alone can
        // never converge. A row sealed `source_missing` has no object at all to compare with,
        // and every launch would find it "changed" and file another note.
        let key = UsageLedger.intervalKey(assistant: assistant, sessionID: sample.sessionID,
                                          boundaryKind: .task, boundaryID: id, segmentNo: 0)
        if let db = database(), let existing = row(db, key: key), existing.sealed {
            updateLineage(db, key: key, sample: sample)
            guard let usage,
                  let data = try? JSONSerialization.data(withJSONObject: usage,
                                                         options: [.sortedKeys]),
                  String(decoding: data, as: UTF8.self) != (existing.rawUsage ?? "")
            else { return false }
            return correction(intervalKey: key, reason: "source_changed_after_seal",
                              proposed: usage)
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
             WHERE task_id = ? AND session_id NOT LIKE ? || '%'
             ORDER BY started_at LIMIT 1;
            """, -1, &statement, nil) == SQLITE_OK else { return nil }
        bind(statement, 1, id)
        bind(statement, 2, UsageLedger.unresolvedSessionPrefix)
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

    /// Whether this terminal was checkpointed too recently to be worth opening anything for.
    ///
    /// Asked **before** the record is resolved, because resolving one can cost a working-
    /// directory lookup and the panel takes this reading every 1.2 seconds. Split out from
    /// ``checkpoint(sessions:now:)`` so the rule can be exercised without a live terminal: it is
    /// the difference between a store that gains one row per session per five minutes and one
    /// that gains three hundred an hour.
    static func checkpointThrottled(since last: Date?, now: Date) -> Bool {
        guard let last else { return false }
        return now.timeIntervalSince(last) < checkpointInterval
    }

    /// Whether a resolved reading says anything the previous one did not. A file that has not
    /// moved has nothing to attribute, and a **different** session id behind the same terminal
    /// is always something new — a tab reused for a second conversation must not be skipped
    /// because the two files happen to be the same length.
    static func checkpointUnchanged(lastSessionID: String?, lastBytes: Int?,
                                    sessionID: String, bytes: Int?) -> Bool {
        lastSessionID == sessionID && lastBytes != nil && lastBytes == bytes
    }

    /// Remember what is behind a terminal, which is what makes a later disappearance mean
    /// something. ``checkpoint(sessions:now:)`` is the production caller.
    static func remember(terminalID: String, assistant: Assistant, sessionID: String,
                         record: URL, bytes: Int?, at: Date) {
        watchLock.lock()
        watchedSessions[terminalID] = Watched(assistant: assistant, sessionID: sessionID,
                                              record: record, at: at, bytes: bytes)
        watchLock.unlock()
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
            if checkpointThrottled(since: previous?.at, now: now) { continue }
            guard let record = Transcript.record(of: session),
                  let sessionID = Transcript.sessionID(in: record.url,
                                                       assistant: record.assistant)
            else { continue }
            let bytes = (try? FileManager.default
                .attributesOfItem(atPath: record.url.path)[.size] as? Int) ?? nil
            let unchanged = checkpointUnchanged(lastSessionID: previous?.sessionID,
                                                lastBytes: previous?.bytes,
                                                sessionID: sessionID, bytes: bytes)
            remember(terminalID: session.id, assistant: record.assistant, sessionID: sessionID,
                     record: record.url, bytes: bytes, at: now)
            if unchanged { continue }
            shared.observe(sample(assistant: record.assistant, sessionID: sessionID,
                                  record: record.url, terminalID: session.id,
                                  cwd: Targets.workingDirectory(of: session),
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
                // A mark added to whatever this row already carries. A session that rotated
                // earlier and is unreadable now is both, and the close used to erase the first.
                if final.rawUsage == nil { final.mark(.sourceUnreadableAtClose) }
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
            sample.projectKey = UsageLedger.canonicalProjectKey(
                projectDir: task["project_dir"] as? String,
                repositoryCommonDir: task["repository_common_dir"] as? String)
            sample.workingDir = ((task["worktree"] as? [String: Any])?["cwd"] as? String)
                ?? (task["project_dir"] as? String) ?? cwd
            sample.graphID = task["graph_id"] as? String
            sample.parentTaskID = task["parent_task"] as? String
            sample.retryOf = task["respawn_of"] as? String
            sample.attempt = task["respawn_generation"] as? Int
            sample.landingState = (task["landing"] as? [String: Any])?["state"] as? String
            sample.disposition = task["disposition"] as? String
        } else {
            sample = Sample(assistant: assistant, sessionID: sessionID, boundaryKind: .session,
                            boundaryID: sessionID, origin: .manual)
            sample.projectKey = UsageLedger.canonicalProjectKey(projectDir: cwd)
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

// MARK: - Usage Analytics query contract

/// A bounded, privacy-preserving reader over the durable usage ledger.
///
/// The ledger owns collection and storage invariants; this service owns presentation invariants:
/// a range is interpreted in the timezone the caller named, pagination is stable, paths and raw
/// transcript material never cross the wire, and a quantity cannot leave without its availability,
/// unit and basis. It intentionally stays in this file so a future reader cannot bypass
/// `Row.measurement` by reaching for the SQLite token columns directly.
final class UsageQueryService {
    static let maxPageSize = 200
    static let maxScannedRows = 100_000

    enum View: String, CaseIterable { case overview, agentWork = "agent_work" }
    enum Bucket: String, CaseIterable { case day, week, month }

    struct Query {
        var from: String?
        var to: String?
        var timezoneID: String
        var groupBy: UsageLedger.GroupBy
        var bucket: Bucket
        var assistant: String?
        var model: String?
        var origin: String?
        var project: String?
        var view: View
        var limit: Int
        var cursor: String?

        init(from: String? = nil, to: String? = nil,
             timezoneID: String = TimeZone.autoupdatingCurrent.identifier,
             groupBy: UsageLedger.GroupBy = .model, bucket: Bucket = .day,
             assistant: String? = nil, model: String? = nil, origin: String? = nil,
             project: String? = nil, view: View = .overview, limit: Int = 50,
             cursor: String? = nil) {
            self.from = from
            self.to = to
            self.timezoneID = timezoneID
            self.groupBy = groupBy
            self.bucket = bucket
            self.assistant = assistant
            self.model = model
            self.origin = origin
            self.project = project
            self.view = view
            self.limit = limit
            self.cursor = cursor
        }
    }

    struct ParseResult {
        var query: Query?
        var error: String?
    }

    /// The DTO shared by HTTP JSON, safe exports and the web MVP. `payload` is already in wire
    /// spelling; `rows` and `allRows` remain typed only long enough to make pagination and export
    /// use the exact same selected subjects.
    struct UsageAnalyticsDTO {
        var payload: [String: Any]
        var rows: [UsageLedger.Row]
        var allRows: [UsageLedger.Row]
        var acceptedProjects: [String: UsageLedger.AcceptedAttribution]
        var nextCursor: String?
        var scanTruncated: Bool
    }

    private struct Cursor: Codable {
        var at: Double
        var key: String

        var encoded: String {
            guard let data = try? JSONEncoder().encode(self) else { return "" }
            return data.base64EncodedString()
                .replacingOccurrences(of: "+", with: "-")
                .replacingOccurrences(of: "/", with: "_")
                .replacingOccurrences(of: "=", with: "")
        }

        static func decode(_ text: String) -> Cursor? {
            guard !text.isEmpty,
                  text.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" })
            else { return nil }
            var base64 = text.replacingOccurrences(of: "-", with: "+")
                .replacingOccurrences(of: "_", with: "/")
            while base64.count % 4 != 0 { base64 += "=" }
            guard let data = Data(base64Encoded: base64),
                  let cursor = try? JSONDecoder().decode(Cursor.self, from: data),
                  cursor.at.isFinite, !cursor.key.isEmpty else { return nil }
            return cursor
        }
    }

    private struct PreviousRange {
        var start: Date
        var end: Date
        var from: String
        var to: String
    }

    enum ExportError: Error { case jsonSerialization }
    static var jsonEncoderForTesting: (([String: Any]) -> Data?)?

    private let readRows: (UsageLedger.AnalyticsFilter) -> UsageLedger.AnalyticsRead
    private let readScheduleLabels: () -> [String: String]
    /// Whether a Feature producer is configured, injected the way `readScheduleLabels` is: a test
    /// says what the setting is without reaching for the person's own config file.
    private let readFeatureClassifier: () -> UsageLedger.FeatureClassifierState
    /// A Project's pixel mark, keyed by the canonical identity `resolveProject` returned, and
    /// injected for the same reason the two above are: a test must be able to give a Feature row
    /// an icon without `~/.claude/project-icons.json` existing or saying anything in particular.
    ///
    /// **The identity goes in and only colours come out.** `docs/usage-attribution.md` says public
    /// analytics exposes the canonical Project's final name and never the filesystem path, and
    /// this seam is where that could quietly stop being true — so the path is the argument, never
    /// part of the answer.
    private let readProjectIcon: (String) -> [String: Any]?
    private let encodeJSON: ([String: Any]) -> Data?

    init() {
        readRows = { UsageLedger.shared.analyticsRead($0) }
        readScheduleLabels = { Orchestrator.usageScheduleLabels() }
        readFeatureClassifier = { UsageLedger.featureClassifierState() }
        readProjectIcon = { ProjectIcon.grid(forCwd: $0).map(ProjectIcon.gridJSON) }
        encodeJSON = Self.jsonEncoderForTesting ?? {
            try? JSONSerialization.data(withJSONObject: $0,
                                        options: [.sortedKeys, .withoutEscapingSlashes])
        }
    }

    /// Test seam for presentation invariants. Production never enters through this initializer:
    /// its complete predicate and max+1 bound live in `UsageLedger.analyticsRead` above.
    init(rows: @escaping () -> [UsageLedger.Row], corrections: @escaping () -> Int = { 0 },
         acceptedFeatures: @escaping () -> [String: UsageLedger.AcceptedAttribution] = { [:] },
         acceptedProjects: @escaping () -> [String: UsageLedger.AcceptedAttribution] = { [:] },
         scheduleLabels: @escaping () -> [String: String] = { [:] },
         featureClassifier: @escaping () -> UsageLedger.FeatureClassifierState = {
             .notConfigured
         },
         projectIcon: @escaping (String) -> [String: Any]? = { _ in nil },
         jsonEncoder: @escaping ([String: Any]) -> Data? = {
             try? JSONSerialization.data(withJSONObject: $0,
                                         options: [.sortedKeys, .withoutEscapingSlashes])
         }) {
        readRows = { _ in
            let rows = rows()
            return UsageLedger.AnalyticsRead(
                rows: rows, corrections: corrections(),
                latestLedgerObservation: rows.map(\.updatedAt).max(),
                acceptedFeatures: acceptedFeatures(), acceptedProjects: acceptedProjects())
        }
        readScheduleLabels = scheduleLabels
        readFeatureClassifier = featureClassifier
        readProjectIcon = projectIcon
        encodeJSON = jsonEncoder
    }

    /// Parse a closed query. Unknown and repeated keys are errors: accepting either would turn a
    /// typo into a broader accounting query, which is the unsafe direction for this surface.
    static func parse(_ values: [String: String], repeatedKeys: Set<String>) -> ParseResult {
        let allowed: Set<String> = ["from", "to", "timezone", "group", "bucket", "assistant",
                                    "model", "origin", "project", "view", "limit", "cursor"]
        let unknown = Set(values.keys).subtracting(allowed)
        guard unknown.isEmpty else {
            return ParseResult(error: "Unknown usage query field: \(unknown.sorted().joined(separator: ", ")).")
        }
        guard repeatedKeys.isEmpty else {
            return ParseResult(error: "Usage query fields may appear only once.")
        }
        let timezoneID = values["timezone"].flatMap { $0.isEmpty ? nil : $0 }
            ?? TimeZone.autoupdatingCurrent.identifier
        guard TimeZone(identifier: timezoneID) != nil else {
            return ParseResult(error: "timezone must be an IANA timezone identifier.")
        }
        let from = values["from"].flatMap { $0.isEmpty ? nil : $0 }
        let to = values["to"].flatMap { $0.isEmpty ? nil : $0 }
        guard [from, to].allSatisfy({ $0 == nil || UsageLedger.isLocalDay($0!) }) else {
            return ParseResult(error: "from and to are local dates, YYYY-MM-DD.")
        }
        guard from == nil || to == nil || from! <= to! else {
            return ParseResult(error: "from must not be after to.")
        }
        let group = values["group"].flatMap(UsageLedger.GroupBy.init(rawValue:)) ?? .model
        if values["group"] != nil && UsageLedger.GroupBy(rawValue: values["group"]!) == nil {
            return ParseResult(error: "group must be one of \(UsageLedger.GroupBy.allCases.map(\.rawValue).joined(separator: ", ")).")
        }
        let bucket = values["bucket"].flatMap(Bucket.init(rawValue:)) ?? .day
        if values["bucket"] != nil && Bucket(rawValue: values["bucket"]!) == nil {
            return ParseResult(error: "bucket must be day, week or month.")
        }
        let view = values["view"].flatMap(View.init(rawValue:)) ?? .overview
        if values["view"] != nil && View(rawValue: values["view"]!) == nil {
            return ParseResult(error: "view must be overview or agent_work.")
        }
        let parsedLimit = values["limit"].flatMap(Int.init)
        if values["limit"] != nil && parsedLimit == nil {
            return ParseResult(error: "limit must be an integer between 1 and \(maxPageSize).")
        }
        let limit = parsedLimit ?? 50
        guard (1...maxPageSize).contains(limit) else {
            return ParseResult(error: "limit must be between 1 and \(maxPageSize).")
        }
        let cursor = values["cursor"].flatMap { $0.isEmpty ? nil : $0 }
        guard cursor == nil || Cursor.decode(cursor!) != nil else {
            return ParseResult(error: "cursor is not a usage continuation issued by this service.")
        }
        func optional(_ name: String) -> String? {
            values[name]?.trimmingCharacters(in: .whitespacesAndNewlines)
                .nonEmpty
        }
        return ParseResult(query: Query(from: from, to: to, timezoneID: timezoneID,
                                        groupBy: group, bucket: bucket,
                                        assistant: optional("assistant"), model: optional("model"),
                                        origin: optional("origin"), project: optional("project"),
                                        view: view, limit: limit, cursor: cursor))
    }

    func query(_ query: Query, now: Date = Date()) -> UsageAnalyticsDTO {
        let timezone = TimeZone(identifier: query.timezoneID) ?? TimeZone(secondsFromGMT: 0)!
        var calendar = Calendar(identifier: .gregorian)
        calendar.locale = Locale(identifier: "en_US_POSIX")
        calendar.timeZone = timezone
        let bounds = Self.dateBounds(from: query.from, to: query.to, calendar: calendar)

        let filter = UsageLedger.AnalyticsFilter(
            start: bounds.start, end: bounds.end, assistant: query.assistant, model: query.model,
            origin: query.origin, project: query.project, limit: Self.maxScannedRows + 1)
        let reading = readRows(filter)
        // Production has already applied this complete predicate in SQLite. Reapplying it here
        // keeps the injectable test seam honest without changing the bounded production cost.
        let matched = matching(reading.rows, start: bounds.start, end: bounds.end, query: query,
                               acceptedProjects: reading.acceptedProjects)
        let truncated = matched.count > Self.maxScannedRows
        let filtered = Array(matched.prefix(Self.maxScannedRows))

        let priorRange = previousRange(for: query, bounds: bounds, calendar: calendar)
        var previousRows: [UsageLedger.Row] = []
        var previousAcceptedProjects: [String: UsageLedger.AcceptedAttribution] = [:]
        var previousTruncated = false
        if let priorRange {
            let previousReading = readRows(UsageLedger.AnalyticsFilter(
                start: priorRange.start, end: priorRange.end, assistant: query.assistant,
                model: query.model, origin: query.origin, project: query.project,
                limit: Self.maxScannedRows + 1, includeFeatureAttribution: false))
            let previousMatched = matching(previousReading.rows, start: priorRange.start,
                                           end: priorRange.end, query: query,
                                           acceptedProjects: previousReading.acceptedProjects)
            previousTruncated = previousMatched.count > Self.maxScannedRows
            previousRows = Array(previousMatched.prefix(Self.maxScannedRows))
            previousAcceptedProjects = previousReading.acceptedProjects
        }

        let continuation = query.cursor.flatMap(Cursor.decode)
        let afterCursor = filtered.filter { row in
            guard let continuation else { return true }
            let at = row.startedAt.timeIntervalSince1970
            return at < continuation.at || (at == continuation.at && row.intervalKey < continuation.key)
        }
        let page = Array(afterCursor.prefix(query.limit))
        let hasMore = afterCursor.count > page.count
        let next = hasMore ? page.last.map {
            Cursor(at: $0.startedAt.timeIntervalSince1970, key: $0.intervalKey).encoded
        } : nil
        let corrections = reading.corrections

        let totals = Self.summary(filtered)
        let breakdown = Self.breakdown(filtered, groupBy: query.groupBy, calendar: calendar,
                                       acceptedProjects: reading.acceptedProjects)
        let trend = Self.trend(filtered, bucket: query.bucket, calendar: calendar)
        let latest = reading.latestLedgerObservation
        let rangeLatest = filtered.map(\.updatedAt).max()
        let age = latest.map { max(0, Int(now.timeIntervalSince($0))) }
        let rangeAge = rangeLatest.map { max(0, Int(now.timeIntervalSince($0))) }
        let freshnessStatus: String
        if latest == nil { freshnessStatus = "empty" }
        else if (age ?? 0) > Int(UsageLedger.checkpointInterval * 2) { freshnessStatus = "stale" }
        else { freshnessStatus = "current" }
        let rangeFreshnessStatus: String
        if rangeLatest == nil { rangeFreshnessStatus = "empty" }
        else if (rangeAge ?? 0) > Int(UsageLedger.checkpointInterval * 2) {
            rangeFreshnessStatus = "historical"
        } else { rangeFreshnessStatus = "current" }

        let formatter = ISO8601DateFormatter()
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        var payload: [String: Any] = [
            "schemaVersion": UsageLedger.schemaVersion,
            "view": query.view.rawValue,
            "range": ["from": query.from as Any? ?? NSNull(),
                      "to": query.to as Any? ?? NSNull(),
                      "timezone": timezone.identifier],
            "freshness": ["generatedAt": formatter.string(from: now),
                          "latestObservedAt": latest.map(formatter.string(from:)) as Any? ?? NSNull(),
                          "ageSeconds": age as Any? ?? NSNull(), "status": freshnessStatus,
                          "scanTruncated": truncated],
            "rangeFreshness": [
                "dataThrough": rangeLatest.map(formatter.string(from:)) as Any? ?? NSNull(),
                "ageSeconds": rangeAge as Any? ?? NSNull(), "status": rangeFreshnessStatus,
            ],
            "capabilities": [
                "views": View.allCases.map(\.rawValue),
                "groupBy": UsageLedger.GroupBy.allCases.map(\.rawValue),
                "buckets": Bucket.allCases.map(\.rawValue),
                "filters": ["from", "to", "timezone", "assistant", "model", "origin", "project"],
                "exports": ["csv", "json"], "maxPageSize": Self.maxPageSize,
                "maxScannedRows": Self.maxScannedRows,
                "attribution": [
                    "dimensions": UsageLedger.AttributionDimension.allCases.map(\.rawValue),
                    "sources": UsageLedger.AttributionSource.allCases.map(\.rawValue),
                    "decisions": UsageLedger.AttributionDecision.allCases.map(\.rawValue),
                    "featureAggregation": "one_unambiguous_accepted_head",
                    "automaticFeatureAttribution": false,
                    "featureProducer": "manual_or_external_only",
                    "llmEvidence": "classifier_version_confidence_and_sha256_only",
                ],
            ],
            "priceSnapshot": [
                "activeId": UsageLedger.priceSnapshotID,
                "observedIds": Array(Set(filtered.compactMap(\.priceSnapshotID))).sorted(),
                "meaning": "Observed ids priced rows in this range; activeId is the current list-price table, not an actual bill.",
            ],
            "totals": totals,
            "coverage": totals["coverage"] as Any,
            "corrections": corrections,
            "breakdown": breakdown,
            "groupBy": query.groupBy.rawValue,
            "trend": trend,
            "bucket": query.bucket.rawValue,
            "rows": page.map { Self.publicRow($0, acceptedProjects: reading.acceptedProjects) },
            "rowCount": filtered.count,
            "pagination": ["limit": query.limit, "nextCursor": next as Any? ?? NSNull(),
                           "hasMore": hasMore],
            "unavailableDimensions": [
                "dimensions": ["graph_id", "disposition"],
                "reason": UsageLedger.reservedColumnsReason,
                "graphView": false, "retryView": false, "landingView": false,
                "featureView": true,
                "featureAvailability": "one_unambiguous_accepted_head_or_unknown",
            ],
        ]
        payload["portfolio"] = Self.portfolio(
            rows: filtered, previousRows: previousRows,
            acceptedFeatures: reading.acceptedFeatures,
            acceptedProjects: reading.acceptedProjects,
            previousAcceptedProjects: previousAcceptedProjects, calendar: calendar,
            scheduleLabels: readScheduleLabels(),
            featureClassifier: readFeatureClassifier(),
            projectIcon: readProjectIcon,
            query: query, priorRange: priorRange,
            comparisonTruncated: truncated || previousTruncated)
        if truncated {
            payload["availability"] = ["status": "partial", "reason": "scan_limit_reached"]
        } else {
            payload["availability"] = ["status": "complete"]
        }
        return UsageAnalyticsDTO(payload: payload, rows: page, allRows: filtered,
                                 acceptedProjects: reading.acceptedProjects,
                                 nextCursor: next, scanTruncated: truncated)
    }

    /// Safe, reconciliation-ready CSV. It is intentionally a public projection rather than the
    /// ledger's forensic export: no session id, working directory, source bytes or raw object.
    func exportCSV(_ result: UsageAnalyticsDTO) -> String {
        let columns = ["interval_id", "task_id", "started_at", "ended_at", "assistant", "model", "origin",
                       "project", "input_new", "output", "cache_read", "cache_write",
                       "strict_total", "measured_floor", "unknown_token_parts", "source_total",
                       "reconciliation", "input_basis", "cost_value", "cost_unit", "cost_basis",
                       "price_snapshot_id", "missing_cost_reason", "coverage", "coverage_reasons"]
        var lines = [columns.joined(separator: ",")]
        let formatter = ISO8601DateFormatter()
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        for row in result.allRows {
            let measurement = row.measurement
            let fields: [String] = [
                row.intervalKey, row.taskID ?? "", formatter.string(from: row.startedAt),
                row.endedAt.map(formatter.string(from:)) ?? "",
                row.assistant, row.model ?? "", row.origin,
                Self.projectName(row, acceptedProjects: result.acceptedProjects) ?? "",
                measurement.counts.inputNew.map(String.init) ?? "",
                measurement.counts.output.map(String.init) ?? "",
                measurement.counts.cacheRead.map(String.init) ?? "",
                measurement.counts.cacheWrite.map(String.init) ?? "",
                measurement.total.map(String.init) ?? "",
                measurement.unknown ? "" : String(measurement.measured),
                measurement.unknownParts.map(\.rawValue).joined(separator: " "),
                row.sourceTotal.map(String.init) ?? "", row.reconciliation ?? "",
                row.inputBasis ?? "",
                row.costValue.map { String($0) } ?? "", row.costUnit ?? "", row.costBasis,
                row.priceSnapshotID ?? "", row.missingReason ?? "", row.coverage,
                measurement.reasons.joined(separator: " "),
            ]
            lines.append(fields.map(Self.csvCell).joined(separator: ","))
        }
        return lines.joined(separator: "\n") + "\n"
    }

    /// Lossless means lossless with respect to the public analytics contract: every null, zero,
    /// unit, basis, availability mark and reason survives. Raw prompts and paths are not part of
    /// that contract in the first place.
    func exportJSON(_ result: UsageAnalyticsDTO) throws -> Data {
        var object = result.payload
        object["rows"] = result.allRows.map {
            Self.publicRow($0, acceptedProjects: result.acceptedProjects)
        }
        object["rowCount"] = result.allRows.count
        object["truncated"] = result.scanTruncated
        object.removeValue(forKey: "pagination")
        guard let data = encodeJSON(object) else { throw ExportError.jsonSerialization }
        return data
    }

    /// Not private, because `UsageProjectWorktreeService` bounds its own read by the same
    /// requested days in the same zone. Two copies of this would be two ranges wearing one name.
    static func dateBounds(from: String?, to: String?, calendar: Calendar)
        -> (start: Date?, end: Date?) {
        let formatter = DateFormatter()
        formatter.calendar = calendar
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = calendar.timeZone
        formatter.dateFormat = "yyyy-MM-dd"
        let start = from.flatMap(formatter.date(from:))
        let finalDay = to.flatMap(formatter.date(from:))
        return (start, finalDay.flatMap { calendar.date(byAdding: .day, value: 1, to: $0) })
    }

    private func previousRange(for query: Query, bounds: (start: Date?, end: Date?),
                               calendar: Calendar) -> PreviousRange? {
        guard query.from != nil, query.to != nil, let start = bounds.start, let end = bounds.end,
              let dayCount = calendar.dateComponents([.day], from: start, to: end).day,
              dayCount > 0,
              let previousStart = calendar.date(byAdding: .day, value: -dayCount, to: start),
              let previousLast = calendar.date(byAdding: .day, value: -1, to: start)
        else { return nil }
        let formatter = DateFormatter()
        formatter.calendar = calendar
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = calendar.timeZone
        formatter.dateFormat = "yyyy-MM-dd"
        return PreviousRange(start: previousStart, end: start,
                             from: formatter.string(from: previousStart),
                             to: formatter.string(from: previousLast))
    }

    private func matching(
        _ rows: [UsageLedger.Row], start: Date?, end: Date?, query: Query,
        acceptedProjects: [String: UsageLedger.AcceptedAttribution]
    ) -> [UsageLedger.Row] {
        rows.sorted(by: newestFirst).filter { row in
            if let start, row.startedAt < start { return false }
            if let end, row.startedAt >= end { return false }
            if let assistant = query.assistant, row.assistant != assistant { return false }
            if let model = query.model, row.model != model { return false }
            if let origin = query.origin, row.origin != origin { return false }
            if let project = query.project,
               Self.projectName(row, acceptedProjects: acceptedProjects) != project { return false }
            return true
        }
    }

    private func newestFirst(_ left: UsageLedger.Row, _ right: UsageLedger.Row) -> Bool {
        if left.startedAt != right.startedAt { return left.startedAt > right.startedAt }
        return left.intervalKey > right.intervalKey
    }

    private static func summary(_ rows: [UsageLedger.Row]) -> [String: Any] {
        var tokenSums: [UsageLedger.Part: Int] = [:]
        var tokenKnown: Set<UsageLedger.Part> = []
        var partsUnknown: [String: Int] = [:]
        var measuredFloor = 0
        var measuredRows = 0
        var strictTotal = 0
        var strictAvailable = !rows.isEmpty
        var unknownRows = 0
        var coverageStates: [String: Int] = [:]
        var coverageReasons: [String: Int] = [:]
        var costs: [String: (unit: String, basis: String, value: Double, rows: Int,
                            snapshots: Set<String>)] = [:]
        var missingCosts: [String: Int] = [:]
        var origins: [String: Int] = [:]
        var scheduledTasks: Set<String> = []

        for row in rows {
            origins[row.origin, default: 0] += 1
            if row.origin == UsageLedger.Origin.schedule.rawValue {
                scheduledTasks.insert(row.taskID ?? row.intervalKey)
            }
            let measurement = row.measurement
            if measurement.incomplete { unknownRows += 1 }
            for part in UsageLedger.Part.allCases {
                if let value = measurement.counts[part] {
                    tokenKnown.insert(part)
                    tokenSums[part, default: 0] += value
                } else {
                    partsUnknown[part.rawValue, default: 0] += 1
                }
            }
            if measurement.unknown {
                strictAvailable = false
            } else {
                measuredFloor += measurement.measured
                measuredRows += 1
                if let total = measurement.total { strictTotal += total }
                else { strictAvailable = false }
            }
            coverageStates[row.coverage, default: 0] += 1
            for reason in measurement.reasons { coverageReasons[reason, default: 0] += 1 }
            if let value = row.costValue, let unit = row.costUnit {
                let key = unit + "\u{1f}" + row.costBasis
                var cost = costs[key] ?? (unit, row.costBasis, 0, 0, [])
                cost.value += value
                cost.rows += 1
                if let snapshot = row.priceSnapshotID { cost.snapshots.insert(snapshot) }
                costs[key] = cost
            } else {
                missingCosts[row.missingReason ?? UsageLedger.MissingCost.noCostRecorded.rawValue,
                             default: 0] += 1
            }
        }
        var tokenPayload: [String: Any] = [:]
        for part in UsageLedger.Part.allCases {
            tokenPayload[part.rawValue] = tokenKnown.contains(part)
                ? tokenSums[part] as Any : NSNull()
        }
        let costPayload = costs.values.sorted {
            $0.unit == $1.unit ? $0.basis < $1.basis : $0.unit < $1.unit
        }.map { cost -> [String: Any] in
            ["unit": cost.unit, "basis": cost.basis, "value": cost.value, "rows": cost.rows,
             "priceSnapshotIds": cost.snapshots.sorted()]
        }
        return [
            "rows": rows.count,
            "tokens": tokenPayload,
            "tokenPartsUnknown": partsUnknown,
            "tokenRowsUnknown": unknownRows,
            "measuredFloor": measuredRows == 0 ? NSNull() : measuredFloor,
            "strictTotal": strictAvailable ? strictTotal : NSNull(),
            "costs": costPayload,
            "unavailableCost": ["rows": missingCosts.values.reduce(0, +),
                                "reasons": missingCosts],
            "origins": origins,
            "scheduledRuns": scheduledTasks.count,
            "coverage": ["states": coverageStates, "reasons": coverageReasons,
                         "tokenRowsUnknown": unknownRows, "tokenPartsUnknown": partsUnknown],
        ]
    }

    // MARK: - Project Portfolio projection

    private struct ProjectIdentityEvidence {
        var identity: String?
        var reason: String?
    }

    /// Portfolio identity asks only the canonical key the collector persisted. `workingDir` is
    /// intentionally not a fallback here: a checkout directory can be a disposable worktree, and
    /// turning its basename into a Project is precisely the inference this surface must refuse.
    ///
    /// Rows recorded before canonical Project keys landed may already contain a Clawdline-managed
    /// worktree path ending in a task UUID. Until the user chooses an append-only migration policy,
    /// this read seam suppresses that disposable label into Unknown without rewriting the ledger.
    ///
    /// **The rule itself lives in ``UsageFeatureClassifier/resolveProject(projectKey:
    /// acceptedProjectIdentity:)``**, because the Feature scope has to reach the same answer for
    /// the same row. Two copies of it split three work lines into six Features.
    private static func projectIdentity(
        _ row: UsageLedger.Row,
        acceptedProjects: [String: UsageLedger.AcceptedAttribution]
    ) -> ProjectIdentityEvidence {
        let resolution = UsageFeatureClassifier.resolveProject(
            projectKey: row.projectKey,
            acceptedProjectIdentity: acceptedProjects[row.intervalKey]?.id)
        return ProjectIdentityEvidence(identity: resolution.identity,
                                       reason: resolution.refusal?.rawValue)
    }

    /// **The Project id every surface joins on.** Not private for the reason the rule it hashes
    /// is not: a second surface computing this digest itself would produce ids that look like
    /// these and join nothing.
    static func projectID(_ identity: String?) -> String {
        guard let identity else { return "unknown-project" }
        let digest = SHA256.hash(data: Data(identity.utf8)).prefix(8)
            .map { String(format: "%02x", $0) }.joined()
        return "project-" + digest
    }

    static func projectLabel(_ identity: String?, acceptedLabel: String? = nil) -> String {
        guard let identity else { return "Unknown Project" }
        if let acceptedLabel, !acceptedLabel.isEmpty { return acceptedLabel }
        let label = URL(fileURLWithPath: identity).lastPathComponent
        return label.isEmpty ? "Unnamed Project" : label
    }

    /// Which bucket of the Projects table a row falls in. A single implementation because the
    /// Feature table now has to land in the same bucket as the Projects table for the same row —
    /// the two are joined by the id below, and a second spelling of "unknown" would join nothing.
    private static func projectGroupKey(
        _ row: UsageLedger.Row,
        acceptedProjects: [String: UsageLedger.AcceptedAttribution]
    ) -> String {
        projectIdentity(row, acceptedProjects: acceptedProjects).identity ?? "\u{0}unknown-project"
    }

    private static func groupedProjects(
        _ rows: [UsageLedger.Row],
        acceptedProjects: [String: UsageLedger.AcceptedAttribution]
    ) -> [String: (identity: String?, label: String?, reasons: Set<String>,
                  rows: [UsageLedger.Row])] {
        var groups: [String: (identity: String?, label: String?, reasons: Set<String>,
                              rows: [UsageLedger.Row])] = [:]
        for row in rows {
            let accepted = acceptedProjects[row.intervalKey]
            let evidence = projectIdentity(row, acceptedProjects: acceptedProjects)
            let key = projectGroupKey(row, acceptedProjects: acceptedProjects)
            if groups[key] == nil { groups[key] = (evidence.identity, accepted?.label, [], []) }
            if let reason = evidence.reason { groups[key]?.reasons.insert(reason) }
            groups[key]?.rows.append(row)
        }
        return groups
    }

    private static func runID(_ row: UsageLedger.Row) -> String {
        if let task = row.taskID?.trimmingCharacters(in: .whitespacesAndNewlines),
           !task.isEmpty { return "task:" + task }
        let kind = row.boundaryKind.trimmingCharacters(in: .whitespacesAndNewlines)
        let boundary = row.boundaryID.trimmingCharacters(in: .whitespacesAndNewlines)
        if !kind.isEmpty, !boundary.isEmpty { return kind + ":" + boundary }
        let session = row.sessionID.trimmingCharacters(in: .whitespacesAndNewlines)
        if !session.isEmpty { return "session:" + session }
        return "interval:" + row.intervalKey
    }

    private static func unknownOutputRuns(_ rows: [UsageLedger.Row]) -> Int {
        Set(rows.filter { $0.measurement.counts.output == nil }.map(runID)).count
    }

    private static func output(_ totals: [String: Any]) -> Int? {
        (totals["tokens"] as? [String: Any])?[UsageLedger.Part.output.rawValue] as? Int
    }

    private static func unknownPart(_ totals: [String: Any], _ part: UsageLedger.Part) -> Int {
        ((totals["tokenPartsUnknown"] as? [String: Int])?[part.rawValue]) ?? 0
    }

    private static func coveragePayload(_ rows: [UsageLedger.Row], totals: [String: Any])
        -> [String: Any] {
        let coverage = totals["coverage"] as? [String: Any] ?? [:]
        let incomplete = rows.filter { $0.measurement.incomplete }.count
        let status: String
        if rows.isEmpty { status = "unavailable" }
        else if incomplete == 0 { status = "complete" }
        else { status = "partial" }
        return [
            "status": status, "rows": rows.count, "completeRows": rows.count - incomplete,
            "partialRows": incomplete,
            "unknownOutputRuns": unknownOutputRuns(rows),
            "states": coverage["states"] as Any? ?? [:],
            "reasons": coverage["reasons"] as Any? ?? [:],
        ]
    }

    /// Estimated spending is intentionally Claude Code-only. Codex login usage has no dollar
    /// value, and that absence must neither become zero nor make Claude's recorded estimate
    /// disappear from a mixed Project.
    private static func comparableCost(_ rows: [UsageLedger.Row]) -> [String: Any] {
        let scoped = rows.filter { $0.assistant == Assistant.claude.rawValue }
        guard !scoped.isEmpty else {
            return ["status": "unavailable", "reason": "no_claude_code_usage",
                    "assistant": Assistant.claude.rawValue]
        }
        let totals = summary(scoped)
        let costs = totals["costs"] as? [[String: Any]] ?? []
        let unavailable = totals["unavailableCost"] as? [String: Any] ?? [:]
        let missing = unavailable["rows"] as? Int ?? 0
        guard missing == 0 else {
            return ["status": "unavailable", "reason": "partial_cost_coverage",
                    "unavailableRows": missing, "assistant": Assistant.claude.rawValue]
        }
        guard costs.count == 1, let cost = costs.first else {
            return ["status": "unavailable", "assistant": Assistant.claude.rawValue,
                    "reason": costs.isEmpty ? "no_cost_series" : "mixed_cost_series"]
        }
        return ["status": "available", "value": cost["value"] ?? NSNull(),
                "unit": cost["unit"] ?? NSNull(), "basis": cost["basis"] ?? NSNull(),
                "rows": cost["rows"] ?? NSNull(), "assistant": Assistant.claude.rawValue]
    }

    private static func lineagePayload(_ rows: [UsageLedger.Row]) -> [String: Any] {
        var roles: [String: String] = [:]
        for row in rows {
            let id = runID(row)
            let role: String
            if row.origin == UsageLedger.Origin.schedule.rawValue {
                role = "scheduled"
            } else if row.boundaryKind == UsageLedger.BoundaryKind.session.rawValue {
                role = "root"
            } else if row.boundaryKind == UsageLedger.BoundaryKind.task.rawValue,
                      row.depth == Orchestrator.depthFloor {
                // The broker stores depth 1 for the only dispatched level it permits: a root's
                // child. Depth 2 cannot be produced, and therefore never means "child" here.
                role = "child"
            } else {
                role = "unknown"
            }
            if roles[id] == nil || roles[id] == "unknown" { roles[id] = role }
        }
        let root = roles.values.filter { $0 == "root" }.count
        let child = roles.values.filter { $0 == "child" }.count
        let scheduled = roles.values.filter { $0 == "scheduled" }.count
        let unknown = roles.values.filter { $0 == "unknown" }.count
        let status: String = unknown == 0 ? "available"
            : ((root + child) == 0 ? "unavailable" : "partial")
        return ["status": status, "rootRuns": root, "childRuns": child,
                "scheduledRuns": scheduled, "unknownRuns": unknown,
                "reason": unknown == 0 ? NSNull() : "lineage_evidence_missing"]
    }

    private static func outputComparison(current: [UsageLedger.Row],
                                         previous: [UsageLedger.Row],
                                         unavailableReason: String? = nil) -> [String: Any] {
        if let unavailableReason {
            return ["status": "unavailable", "reason": unavailableReason]
        }
        guard !previous.isEmpty else {
            return ["status": "unavailable", "reason": "no_previous_data"]
        }
        let currentTotals = summary(current), previousTotals = summary(previous)
        guard unknownPart(currentTotals, .output) == 0,
              unknownPart(previousTotals, .output) == 0,
              let currentOutput = output(currentTotals),
              let previousOutput = output(previousTotals) else {
            return ["status": "unavailable", "reason": "incomplete_output"]
        }
        let delta = currentOutput - previousOutput
        return [
            "status": "comparable", "current": currentOutput, "previous": previousOutput,
            "absolute": delta,
            "percent": previousOutput == 0 ? NSNull()
                : (Double(delta) / Double(previousOutput)) * 100,
            "percentReason": previousOutput == 0 ? "previous_zero" : NSNull(),
        ]
    }

    private static func mix(_ rows: [UsageLedger.Row], by value: (UsageLedger.Row) -> String?)
        -> [[String: Any]] {
        var groups: [String: [UsageLedger.Row]] = [:]
        for row in rows { groups[value(row) ?? "Unknown", default: []].append(row) }
        return groups.map { label, rows -> [String: Any] in
            let totals = summary(rows)
            return ["label": label, "runs": Set(rows.map(runID)).count,
                    "output": output(totals) as Any? ?? NSNull(),
                    "unknownOutputRuns": unknownOutputRuns(rows)]
        }.sorted {
            let left = $0["output"] as? Int ?? -1, right = $1["output"] as? Int ?? -1
            if left != right { return left > right }
            return ($0["label"] as? String ?? "") < ($1["label"] as? String ?? "")
        }
    }

    private static func projectPayload(identity: String?, label: String?,
                                       identityReasons: Set<String>,
                                       rows: [UsageLedger.Row],
                                       previous: [UsageLedger.Row], calendar: Calendar,
                                       comparisonReason: String?) -> [String: Any] {
        let totals = summary(rows)
        let scheduledRows = rows.filter { $0.origin == UsageLedger.Origin.schedule.rawValue }
        let scheduledTotals = summary(scheduledRows)
        let recent = rows.sorted {
            $0.startedAt == $1.startedAt ? $0.intervalKey > $1.intervalKey
                : $0.startedAt > $1.startedAt
        }.prefix(6).map { row -> [String: Any] in
            var publicValue = publicRow(row, acceptedProjects: [:])
            publicValue["project"] = projectLabel(identity, acceptedLabel: label)
            return publicValue
        }
        return [
            "id": projectID(identity), "label": projectLabel(identity, acceptedLabel: label),
            "identity": [
                "status": identity == nil ? "unavailable" : "available",
                "reasons": identityReasons.sorted(),
            ],
            "output": output(totals) as Any? ?? NSNull(),
            "unknownOutputRuns": unknownOutputRuns(rows),
            "tokens": totals["tokens"] ?? NSNull(),
            "tokenPartsUnknown": totals["tokenPartsUnknown"] ?? [:],
            "runs": Set(rows.map(runID)).count,
            "scheduledRuns": Set(scheduledRows.map(runID)).count,
            "scheduledOutput": output(scheduledTotals) as Any? ?? NSNull(),
            "cost": comparableCost(rows),
            "coverage": coveragePayload(rows, totals: totals),
            "lineage": lineagePayload(rows),
            "comparison": outputComparison(current: rows, previous: previous,
                                             unavailableReason: comparisonReason),
            "trend": trend(rows, bucket: .day, calendar: calendar),
            "assistantMix": mix(rows, by: { $0.assistant }),
            "workMix": mix(rows, by: {
                $0.origin == UsageLedger.Origin.schedule.rawValue ? "Scheduled" : "Interactive"
            }),
            "recentWork": Array(recent),
        ]
    }

    private static func scheduledWork(_ rows: [UsageLedger.Row], calendar: Calendar,
                                      labels: [String: String])
        -> [String: Any] {
        var groups: [String: [UsageLedger.Row]] = [:]
        let scheduledRows = rows.filter { $0.origin == UsageLedger.Origin.schedule.rawValue }
        for row in scheduledRows {
            if let id = row.scheduleID?.trimmingCharacters(in: .whitespacesAndNewlines),
               !id.isEmpty { groups[id, default: []].append(row) }
        }
        let formatter = ISO8601DateFormatter()
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        let schedules = groups.map { id, rows -> [String: Any] in
            let totals = summary(rows)
            let days = Set(rows.map { UsageLedger.localDay(of: $0.startedAt, calendar: calendar) })
            return [
                "id": id, "label": labels[id] ?? id,
                "runs": Set(rows.map(runID)).count,
                "output": output(totals) as Any? ?? NSNull(),
                "unknownOutputRuns": unknownOutputRuns(rows),
                "activeDays": days.count,
                "lastRunAt": rows.map(\.startedAt).max().map(formatter.string(from:))
                    as Any? ?? NSNull(),
                "coverage": coveragePayload(rows, totals: totals),
            ]
        }.sorted {
            let left = $0["output"] as? Int ?? -1, right = $1["output"] as? Int ?? -1
            if left != right { return left > right }
            return ($0["id"] as? String ?? "") < ($1["id"] as? String ?? "")
        }
        let unknownRows = scheduledRows.filter {
            $0.scheduleID?.trimmingCharacters(in: .whitespacesAndNewlines).nonEmpty == nil
        }
        let scheduledTotals = summary(scheduledRows)
        return [
            "status": schedules.isEmpty ? "unavailable" : "available",
            "reason": schedules.isEmpty ? "no_schedule_identity_in_range" : NSNull(),
            "runs": Set(scheduledRows.map(runID)).count,
            "output": output(scheduledTotals) as Any? ?? NSNull(),
            "unknownOutputRuns": unknownOutputRuns(scheduledRows),
            "schedules": schedules,
            "unknownSchedule": [
                "runs": Set(unknownRows.map(runID)).count,
                "reason": "schedule_identity_missing",
            ],
        ]
    }

    /// One Project exactly as a Feature row may name it.
    ///
    /// Built from the Projects table's own grouping rather than re-derived, so `id` is the same
    /// string that Project's row in `projects[]` carries and a reader can join the two tables.
    /// The identity itself is not a field here: only the id, the final name, and colours.
    private struct FeatureProjectScope {
        var id: String
        var label: String
        var icon: [String: Any]?
    }

    /// Which Project a Feature belongs to, or the honest refusal.
    ///
    /// A Feature id is computed inside a Project scope, so every row in one group should resolve
    /// to the same Project — but *should* is not a thing to render. When the rows disagree the
    /// answer is `mixed_project_scope` and no id at all: a Feature attributed to the wrong
    /// Project is worse than a Feature attributed to none.
    private static func featureProject(
        _ rows: [UsageLedger.Row],
        acceptedProjects: [String: UsageLedger.AcceptedAttribution],
        scopes: [String: FeatureProjectScope]
    ) -> [String: Any] {
        let keys = Set(rows.map { projectGroupKey($0, acceptedProjects: acceptedProjects) })
        // The lookup cannot independently fail: `scopes` was built from these same rows by this
        // same key, so disagreement is the only way past this guard.
        guard keys.count == 1, let key = keys.first, let scope = scopes[key] else {
            return ["id": NSNull(), "label": "Unknown Project", "reason": "mixed_project_scope"]
        }
        var payload: [String: Any] = ["id": scope.id, "label": scope.label]
        // Absent rather than null: a Project with no mark and a Project whose mark failed to
        // read are the same thing to draw, and `drawIcon` already falls back to the placeholder.
        if let icon = scope.icon { payload["icon"] = icon }
        return payload
    }

    private static func featureWork(_ rows: [UsageLedger.Row],
                                    accepted: [String: UsageLedger.AcceptedAttribution],
                                    acceptedProjects: [String: UsageLedger.AcceptedAttribution],
                                    projectScopes: [String: FeatureProjectScope],
                                    classifier: UsageLedger.FeatureClassifierState)
        -> [String: Any] {
        var groups: [String: (label: String, rows: [UsageLedger.Row])] = [:]
        var unknown: [UsageLedger.Row] = []
        for row in rows {
            guard let feature = accepted[row.intervalKey] else {
                unknown.append(row)
                continue
            }
            if groups[feature.id] == nil { groups[feature.id] = (feature.label, []) }
            groups[feature.id]?.rows.append(row)
        }
        let payload = groups.map { id, value -> [String: Any] in
            let totals = summary(value.rows)
            return ["id": id, "label": value.label,
                    "project": featureProject(value.rows, acceptedProjects: acceptedProjects,
                                              scopes: projectScopes),
                    "runs": Set(value.rows.map(runID)).count,
                    "output": output(totals) as Any? ?? NSNull(),
                    "unknownOutputRuns": unknownOutputRuns(value.rows),
                    "coverage": coveragePayload(value.rows, totals: totals)]
        }.sorted {
            let left = $0["output"] as? Int ?? -1, right = $1["output"] as? Int ?? -1
            if left != right { return left > right }
            return ($0["id"] as? String ?? "") < ($1["id"] as? String ?? "")
        }
        let unknownTotals = summary(unknown)
        return [
            "status": payload.isEmpty && !rows.isEmpty
                ? "no_accepted_attribution" : "available",
            // What the producer situation actually is, rather than the literal `false` this
            // carried while no producer existed. An empty table under a configured classifier
            // and an empty table under none are different facts, and the surface has to be able
            // to tell them apart.
            "automaticAttribution": classifier.configured,
            "policy": "one_unambiguous_accepted_head",
            "classifier": classifier.payload,
            "groups": payload,
            "unknown": [
                "label": "Unknown Feature", "runs": Set(unknown.map(runID)).count,
                "output": output(unknownTotals) as Any? ?? NSNull(),
                "unknownOutputRuns": unknownOutputRuns(unknown),
                "reason": "no_unambiguous_accepted_head",
            ],
        ]
    }

    private static func portfolioInsights(projects: [[String: Any]], rows: [UsageLedger.Row],
                                          previousRows: [UsageLedger.Row]) -> [[String: Any]] {
        var insights: [[String: Any]] = []
        let movers = projects.compactMap { project -> (project: [String: Any], delta: Int)? in
            guard let comparison = project["comparison"] as? [String: Any],
                  comparison["status"] as? String == "comparable",
                  let delta = comparison["absolute"] as? Int, delta != 0 else { return nil }
            return (project, delta)
        }
        if let mover = movers.max(by: { abs($0.delta) < abs($1.delta) }) {
            insights.append([
                "kind": "top_mover", "projectId": mover.project["id"] ?? NSNull(),
                "title": "Largest output change",
                "detail": "\(mover.project["label"] as? String ?? "Project") changed by \(mover.delta) generated tokens versus the equal previous range. Inspect the work mix before drawing a conclusion.",
            ])
        }
        let ratios = projects.compactMap { project -> (project: [String: Any], ratio: Double)? in
            guard let tokens = project["tokens"] as? [String: Any],
                  let output = tokens["output"] as? Int, output > 0,
                  let input = tokens["inputNew"] as? Int,
                  let cache = tokens["cacheRead"] as? Int,
                  let unknown = project["tokenPartsUnknown"] as? [String: Int],
                  (unknown[UsageLedger.Part.output.rawValue] ?? 0) == 0,
                  (unknown[UsageLedger.Part.inputNew.rawValue] ?? 0) == 0,
                  (unknown[UsageLedger.Part.cacheRead.rawValue] ?? 0) == 0
            else { return nil }
            return (project, Double(input + cache) / Double(output))
        }
        if let context = ratios.max(by: { $0.ratio < $1.ratio }), context.ratio >= 10 {
            insights.append([
                "kind": "context_to_output", "projectId": context.project["id"] ?? NSNull(),
                "title": "High context-to-output ratio",
                "detail": String(format: "%@ read %.1fx as much new-plus-cached context as it generated. This can be normal for review or retrieval-heavy work; inspect recent runs.", context.project["label"] as? String ?? "Project", context.ratio),
            ])
        }
        if !rows.isEmpty, !previousRows.isEmpty {
            let currentComplete = Double(rows.filter { !$0.measurement.incomplete }.count)
                / Double(rows.count)
            let previousComplete = Double(previousRows.filter { !$0.measurement.incomplete }.count)
                / Double(previousRows.count)
            if previousComplete - currentComplete >= 0.1 {
                insights.append([
                    "kind": "coverage_degradation", "title": "Coverage declined",
                    "detail": String(format: "Complete rows fell from %.0f%% to %.0f%% versus the equal previous range. Check the named coverage reasons before using totals.", previousComplete * 100, currentComplete * 100),
                ])
            }
        }
        let spending = comparableCost(rows)
        if spending["status"] as? String == "available",
           let totalValue = spending["value"] as? Double, totalValue > 0 {
            let priced = projects.compactMap { project -> (project: [String: Any], value: Double)? in
                guard let value = (project["cost"] as? [String: Any])?["value"] as? Double
                else { return nil }
                return (project, value)
            }
            if let leader = priced.max(by: { $0.value < $1.value }),
               leader.value / totalValue >= 0.5 {
                insights.append([
                    "kind": "cost_concentration", "projectId": leader.project["id"] ?? NSNull(),
                    "title": "Comparable cost is concentrated",
                    "detail": String(format: "%@ accounts for %.0f%% of Claude Code's comparable %@ / %@ estimated spending in this range.", leader.project["label"] as? String ?? "Project", leader.value / totalValue * 100, spending["unit"] as? String ?? "unit", spending["basis"] as? String ?? "basis"),
                ])
            }
        }
        return Array(insights.prefix(4))
    }

    private static func portfolio(rows: [UsageLedger.Row], previousRows: [UsageLedger.Row],
                                  acceptedFeatures: [String: UsageLedger.AcceptedAttribution],
                                  acceptedProjects: [String: UsageLedger.AcceptedAttribution],
                                  previousAcceptedProjects: [String: UsageLedger.AcceptedAttribution],
                                  calendar: Calendar, scheduleLabels: [String: String],
                                  featureClassifier: UsageLedger.FeatureClassifierState,
                                  projectIcon: (String) -> [String: Any]?,
                                  query: Query, priorRange: PreviousRange?,
                                  comparisonTruncated: Bool) -> [String: Any] {
        let currentGroups = groupedProjects(rows, acceptedProjects: acceptedProjects)
        let previousGroups = groupedProjects(previousRows,
                                             acceptedProjects: previousAcceptedProjects)
        let comparisonReason: String? = priorRange == nil ? "closed_range_required"
            : (comparisonTruncated ? "range_truncated" : nil)
        let projects = currentGroups.map { key, value in
            projectPayload(identity: value.identity, label: value.label,
                           identityReasons: value.reasons,
                           rows: value.rows,
                           previous: previousGroups[key]?.rows ?? [], calendar: calendar,
                           comparisonReason: comparisonReason)
        }.sorted {
            let left = $0["output"] as? Int ?? -1, right = $1["output"] as? Int ?? -1
            if left != right { return left > right }
            return ($0["id"] as? String ?? "") < ($1["id"] as? String ?? "")
        }.enumerated().map { index, project -> [String: Any] in
            var ranked = project
            ranked["rank"] = index + 1
            return ranked
        }
        // The Feature table's Project column, taken from the Projects grouping above rather than
        // resolved a second time. The mark is read here, from the canonical identity, so nothing
        // downstream of this line has a path to leak.
        let featureProjectScopes = currentGroups.mapValues { value in
            FeatureProjectScope(id: projectID(value.identity),
                                label: projectLabel(value.identity, acceptedLabel: value.label),
                                icon: value.identity.flatMap(projectIcon))
        }
        var comparison = outputComparison(current: rows, previous: previousRows,
                                          unavailableReason: comparisonReason)
        comparison["currentRange"] = ["from": query.from as Any? ?? NSNull(),
                                      "to": query.to as Any? ?? NSNull()]
        comparison["previousRange"] = priorRange.map { ["from": $0.from, "to": $0.to] }
            as Any? ?? NSNull()
        return [
            "schemaVersion": 1,
            "primarySignal": "generated_output",
            "scoreWarning": "Generated output is an operational signal, not a productivity score.",
            "runs": Set(rows.map(runID)).count,
            "comparison": comparison,
            "projects": projects,
            "scheduledWork": scheduledWork(rows, calendar: calendar,
                                           labels: scheduleLabels),
            "features": featureWork(rows, accepted: acceptedFeatures,
                                    acceptedProjects: acceptedProjects,
                                    projectScopes: featureProjectScopes,
                                    classifier: featureClassifier),
            "insights": portfolioInsights(projects: projects, rows: rows,
                                           previousRows: previousRows),
        ]
    }

    private static func breakdown(_ rows: [UsageLedger.Row], groupBy: UsageLedger.GroupBy,
                                  calendar: Calendar,
                                  acceptedProjects: [String: UsageLedger.AcceptedAttribution])
        -> [[String: Any]] {
        var grouped: [String?: [UsageLedger.Row]] = [:]
        for row in rows {
            let key: String?
            switch groupBy {
            case .model: key = row.model
            case .assistant: key = row.assistant
            case .origin: key = row.origin
            case .project: key = projectName(row, acceptedProjects: acceptedProjects)
            case .day: key = UsageLedger.localDay(of: row.startedAt, calendar: calendar)
            case .coverage: key = row.coverage
            case .task: key = row.taskID
            }
            grouped[key, default: []].append(row)
        }
        return grouped.map { key, rows -> [String: Any] in
            var out = summary(rows)
            out["key"] = key as Any? ?? NSNull()
            return out
        }.sorted {
            let left = (($0["tokens"] as? [String: Any])?[UsageLedger.Part.output.rawValue] as? Int) ?? -1
            let right = (($1["tokens"] as? [String: Any])?[UsageLedger.Part.output.rawValue] as? Int) ?? -1
            if left != right { return left > right }
            return String(describing: $0["key"]) < String(describing: $1["key"])
        }
    }

    private static func trend(_ rows: [UsageLedger.Row], bucket: Bucket, calendar: Calendar)
        -> [[String: Any]] {
        var grouped: [Date: [UsageLedger.Row]] = [:]
        for row in rows {
            let start: Date
            switch bucket {
            case .day:
                start = calendar.startOfDay(for: row.startedAt)
            case .week:
                start = calendar.dateInterval(of: .weekOfYear, for: row.startedAt)?.start
                    ?? calendar.startOfDay(for: row.startedAt)
            case .month:
                start = calendar.dateInterval(of: .month, for: row.startedAt)?.start
                    ?? calendar.startOfDay(for: row.startedAt)
            }
            grouped[start, default: []].append(row)
        }
        let formatter = DateFormatter()
        formatter.calendar = calendar
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = calendar.timeZone
        formatter.dateFormat = bucket == .month ? "yyyy-MM" : "yyyy-MM-dd"
        return grouped.keys.sorted().map { start in
            let totals = summary(grouped[start] ?? [])
            return ["bucket": formatter.string(from: start),
                    "tokens": totals["tokens"] as Any,
                    "measuredFloor": totals["measuredFloor"] as Any,
                    "strictTotal": totals["strictTotal"] as Any,
                    "coverage": totals["coverage"] as Any]
        }
    }

    private static func publicRow(
        _ row: UsageLedger.Row,
        acceptedProjects: [String: UsageLedger.AcceptedAttribution]
    ) -> [String: Any] {
        let measurement = row.measurement
        let formatter = ISO8601DateFormatter()
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        let cost: Any
        if let value = row.costValue, let unit = row.costUnit {
            cost = ["value": value, "unit": unit, "basis": row.costBasis,
                    "priceSnapshotId": row.priceSnapshotID as Any? ?? NSNull()]
        } else { cost = NSNull() }
        var tokens: [String: Any] = [:]
        for part in UsageLedger.Part.allCases {
            tokens[part.rawValue] = measurement.counts[part] as Any? ?? NSNull()
        }
        return [
            "id": row.intervalKey,
            "taskId": row.taskID as Any? ?? NSNull(),
            "scheduleId": row.scheduleID as Any? ?? NSNull(),
            "startedAt": formatter.string(from: row.startedAt),
            "endedAt": row.endedAt.map(formatter.string(from:)) as Any? ?? NSNull(),
            "assistant": row.assistant,
            "model": row.model as Any? ?? NSNull(),
            "origin": row.origin,
            "project": projectName(row, acceptedProjects: acceptedProjects) as Any? ?? NSNull(),
            "tokens": tokens,
            "strictTotal": measurement.total as Any? ?? NSNull(),
            "measuredFloor": measurement.unknown ? NSNull() : measurement.measured,
            "unknownTokenParts": measurement.unknownParts.map(\.rawValue),
            "sourceTotal": row.sourceTotal as Any? ?? NSNull(),
            "cost": cost,
            "missingCostReason": row.missingReason as Any? ?? NSNull(),
            "coverage": row.coverage,
            "coverageReasons": measurement.reasons,
            "reconciliation": row.reconciliation as Any? ?? NSNull(),
            "inputBasis": row.inputBasis as Any? ?? NSNull(),
            "lineage": [
                "graphId": row.graphID as Any? ?? NSNull(),
                "parentTaskId": row.parentTaskID as Any? ?? NSNull(),
                "retryOf": row.retryOf as Any? ?? NSNull(),
                "attempt": row.attempt as Any? ?? NSNull(),
                "landingState": row.landingState as Any? ?? NSNull(),
                "disposition": row.disposition as Any? ?? NSNull(),
            ],
        ]
    }

    private static func projectName(
        _ row: UsageLedger.Row,
        acceptedProjects: [String: UsageLedger.AcceptedAttribution] = [:]
    ) -> String? {
        // Public analytics names only the canonical Project key persisted by the collector.
        // A working directory may be a disposable worktree; its basename is not attribution.
        if let accepted = acceptedProjects[row.intervalKey] { return accepted.label }
        guard let raw = row.projectKey, !raw.isEmpty else { return nil }
        let standardized = URL(fileURLWithPath: raw).standardizedFileURL.path
        if UsageLedger.legacyManagedWorktreeTaskID(standardized) != nil { return nil }
        let name = URL(fileURLWithPath: raw).lastPathComponent
        return name.isEmpty ? nil : name
    }

    /// RFC 4180 quoting after spreadsheet neutralization. Leading whitespace does not make a
    /// formula safe: spreadsheets commonly trim it before deciding whether to evaluate a cell.
    private static func csvCell(_ original: String) -> String {
        var value = original
        let first = value.drop(while: { $0 == " " }).first
        if let first, "=+-@\t\r".contains(first) { value = "'" + value }
        if value.contains(where: { $0 == "," || $0 == "\"" || $0 == "\n" || $0 == "\r" }) {
            return "\"" + value.replacingOccurrences(of: "\"", with: "\"\"") + "\""
        }
        return value
    }
}

/// **Which worktrees under one Project have finished a Feature, and which Features those were.**
///
/// `git worktree list` answers a different question. This Mac carries 58 managed checkouts, most
/// of them finished task directories that produced nothing anybody kept; what a person asks in
/// front of a Project is narrower — *which of these actually made something, and what was it*.
///
/// **This is a read-time join and not a new column**, because the material is already stored:
///
/// * `usage_intervals.working_dir` is the checkout a task ran in, and for a Clawdline-managed
///   worktree that path ends in the task UUID which names it;
/// * `usage_intervals.project_key` is the canonical repository the task belonged to, which is
///   what ``UsageFeatureClassifier/resolveProject(projectKey:acceptedProjectIdentity:)`` turns
///   into the Project every other surface names;
/// * `usage_attribution_events` carries the accepted Feature head for that interval;
/// * `task_state` and `landing_state` say how each of those tasks ended.
///
/// **The last of those is not the only source, because it is not the only fact.** `landing_state`
/// says whether anybody recorded a landing; git says whether the delivery is in the tree, and the
/// two had different answers for 35 of one repository's 53 "delivered" worktrees on 2026-09-06 —
/// 22 on branches HEAD already contained with commits of their own, and 13 on branches that had
/// gone from the repository entirely, which are two different answers and not one. So this
/// read also asks the repository what it knows about its own `clawdline/task/…` branches, through
/// the same two `for-each-ref` calls the in-flight list uses, and says in ``LandingEvidence``
/// which of the two each verdict came from.
///
/// The worktree's identity is consumed and discarded when a row's Project is resolved — a managed
/// worktree path resolves to *no* Project rather than to one of its own — so nothing is filed
/// under a worktree anywhere in the store. The column it was read from is still there, which is
/// the whole reason this needs no migration: the join reads `working_dir` for the identity that
/// resolution threw away, and reads the Project by the one shared rule so that a row lands in the
/// same Project the Portfolio put it in.
///
/// **A row whose Project cannot be named is not silently dropped.** Some rows recorded before
/// canonical Project keys landed carry a managed-worktree path as their own `project_key`; those
/// belong to no Project a surface may name, so they can appear under no Project here either. They
/// are counted and named in `unattributed` instead, with the refusal that produced them — the
/// migration that would give them a Project is specified in `docs/usage-attribution.md`.
final class UsageProjectWorktreeService {

    /// **How the work in one worktree ended.** The three states the person asked to be able to
    /// tell apart are `landed`, `delivered` and `abandoned`; `active` exists because folding
    /// work that is running right now into "abandoned" would be a lie, `branchGone` exists
    /// because a delivery whose branch nobody can find any more is not one of the other five,
    /// and `unknown` exists because a verdict with no evidence behind it must not wear one of
    /// the others' names.
    ///
    /// The ladder is evaluated in this order, and each rung is a stored fact or a git fact rather
    /// than an inference:
    ///
    /// 1. `landed` — some row's task carries `landing = landed`: a root recorded that this
    ///    delivery reached its target branch. It outranks everything, including a task that
    ///    reported failure, because the branch is in the tree whatever the child said.
    /// 1a. `nothing_to_land` — some row's task was settled as having had nothing to land, and
    ///    none landed. A read-only audit has no target and no commit, so `landed` cannot say
    ///    this and `abandoned` would say the work was given up when its artifact shipped. It
    ///    sits beside `landed` rather than above `delivered` for one reason: both are closed
    ///    obligations, and this block exists to list the open ones. It is read *before* the two
    ///    git rungs below for the same reason the veto in rung 2 is: a settlement somebody
    ///    recorded is a decision, and the shape of a branch does not overrule one.
    /// 2. **A root that wrote `landing_state = abandoned` vetoes the two git rungs below.** Not
    ///    an outcome of its own: it is a person having looked at this delivery and given the
    ///    obligation up, and the shape of a repository does not overrule a decision. Without it,
    ///    a branch somebody merged for unrelated reasons would report the abandoned obligation as
    ///    `landed`. Everything under the two git rungs is left exactly as it was, so an
    ///    abandoned obligation on work that succeeded is still `delivered` — see rung 5.
    /// 3. `landed` — git says the delivery branch carries commits and is already contained by
    ///    the repository's HEAD. The same reasoning as rung 1, one step weaker in provenance and
    ///    no weaker in fact: the commits are in the tree whether or not anybody wrote it down.
    ///    **Carrying commits is half of that rung and not a detail**: `git worktree add -b` cuts
    ///    the branch at the base commit, so a branch that never received one is an ancestor of
    ///    HEAD the moment it exists — 12 of this Mac's 75 merged delivery branches on
    ///    2026-09-06, 10 of them with a dirty checkout still on disk. See ``branchEvidence(worktree:branches:bases:)``.
    /// 4. `branchGone` — some row's task reached `success` and git says the branch it delivered
    ///    on is not in the repository at all. **This is not `landed`**: the app deletes a
    ///    delivery branch only when it carries no commits, but the app is not the only thing that
    ///    can delete one — 8 branches it explicitly kept because they carried commits (1, 4, 10,
    ///    63 and 122 of them) are gone from this repository with no removal record anywhere. So
    ///    absence means *this side cannot see the delivery any more*, which is worth its own word
    ///    on screen and must not disappear into either neighbour.
    /// 5. `delivered` — some row's task reached `success`, nothing above settled it, and the
    ///    branch is still there unmerged (or git could not be asked). This is "done, not landed",
    ///    which includes an open landing obligation (`pending`) and one that was given up
    ///    (`abandoned`); both spellings travel in `landingStates` beside the word.
    /// 6. `active` — neither of the above, and one of these tasks is still live in the registry
    ///    now. Liveness is asked of the registry rather than measured as an age: a task the
    ///    registry does not hold is certainly not running, which is the direction that is safe to
    ///    conclude from an absence.
    /// 7. `abandoned` — neither landed nor successful, and nothing left running: every task
    ///    either ended without success, or stopped being observed and was never finalized. That
    ///    second shape is what debris looks like in this store — `b57fc96f` sat at `briefed` for
    ///    41 hours because the session died before anything wrote a terminal state.
    /// 8. `unknown` — no row carried any task state at all.
    ///
    /// **A branch that is gone only settles work that succeeded**, which is why rung 4 asks for
    /// `success` and the top of the ladder does not own that case. On a task that failed, an
    /// absent branch is the ordinary shape of debris — the checkout was thrown away empty — and
    /// that worktree stays `abandoned`.
    ///
    /// **Every stored rung is read from the live registry record where there still is one**, and
    /// from the row's frozen copy only where the registry has swept the task. `landingStates` is
    /// what is true now, `storedLandingStates` is what the row recorded, and `landingBasis` says
    /// which of the two each verdict rests on — because a stored copy that can disagree with the
    /// live record must not be handed over as the current answer without saying so.
    ///
    /// **`landingBasis` and ``LandingEvidence`` are two questions, and neither is a spelling of
    /// the other.** The basis says whether the words this verdict was read from came from the
    /// registry as it stands or from the row's frozen copy; the evidence says whether a landing
    /// was *recorded by somebody* or *read off the shape of a branch*. One worktree can carry
    /// `landingBasis = live` and `landingEvidence = branch_merged` at the same time, and that
    /// pair is not a contradiction: the registry answered for its task, and it holds no landing.
    enum Outcome: String, CaseIterable {
        case landed
        /// Settled as having had nothing to land: a read-only delivery that wrote to no
        /// repository, so there was never a branch for anybody to merge.
        case nothingToLand = "nothing_to_land"
        case delivered
        /// Finished, and the branch it was delivered on is not in the repository any more.
        case branchGone = "branch_gone"
        case active, abandoned, unknown

        /// Strongest first. A worktree's own outcome is this ladder applied to every row of every
        /// Feature it carries, which is the same answer as the strongest of its Features'.
        var rank: Int { Outcome.allCases.firstIndex(of: self) ?? Outcome.allCases.count }
    }

    /// **Where the belief that this delivery reached the tree comes from**, published beside the
    /// verdict so that a reader can tell a receipt from a resemblance.
    ///
    /// Measured on 2026-09-05, this route called 53 of one repository's worktrees "delivered, not
    /// landed" while git said 24 of their branches were already ancestors of the checkout's HEAD
    /// and 13 no longer existed. The ladder had asked whether anybody had filled a column in,
    /// which is a question about the registry rather than about the tree — and the same night the
    /// landing queue, which does ask git, said 17. Three screens, three answers.
    ///
    /// `record` and `branch_merged` are not the same claim and are deliberately not spelled the
    /// same way. A record was written by the broker only after it verified with a machine
    /// credential that the commit resolves in the task's repository and is contained by the named
    /// target branch; `branch_merged` is this side reading `for-each-ref --merged HEAD` and
    /// recognising the shape of a landing nobody recorded.
    enum LandingEvidence: String {
        /// A root recorded a verified landing: `landing_state = landed`.
        case record
        /// git says the delivery branch is contained by the repository's current HEAD **and it
        /// carries at least one commit of its own**, so what HEAD contains is a delivery.
        case branchMerged = "branch_merged"
        /// git says the branch is contained by HEAD and it still points at the commit it was cut
        /// from: nothing was ever committed on it. Containment is then a property of how
        /// `git worktree add -b` makes a branch and says nothing at all about a delivery.
        case branchEmpty = "branch_empty"
        /// git says the branch is contained by HEAD and nothing records what it was cut from, so
        /// a real merge cannot be told apart from the empty branch above. **This is not
        /// `unknown`**: git answered, and the half that is missing is the registry's.
        case branchBaseUnknown = "branch_base_unknown"
        /// git answered and the branch is not in the repository at all.
        case branchAbsent = "branch_absent"
        /// git answered, the branch is there, and HEAD does not contain it. **This is the one
        /// `delivered` with a fact behind it**, and the reason it is not spelled `unknown`.
        case branchUnmerged = "branch_unmerged"
        /// **Nobody could say.** git was not asked, could not answer, or the worktree id is not
        /// one a branch name can be built from. Everything else on this screen keeps the answer
        /// it had before git existed as a source, because the costs are not symmetric: showing a
        /// merged delivery costs a glance and hiding an unmerged one costs somebody a day.
        case unknown
    }

    struct Query: Equatable {
        /// The Portfolio's opaque Project id, or the Project's final name, or its absolute
        /// canonical path. Required: this read is about one Project and has no machine-wide form.
        var project: String
        var from: String?
        var to: String?
        var timezoneID: String

        init(project: String, from: String? = nil, to: String? = nil,
             timezoneID: String = TimeZone.autoupdatingCurrent.identifier) {
            self.project = project
            self.from = from
            self.to = to
            self.timezoneID = timezoneID
        }
    }

    struct ParseResult {
        var query: Query?
        var error: String?
    }

    struct Refusal: Equatable {
        var status: Int
        var code: String
        var message: String
    }

    /// One or the other, never both and never neither: an empty list is an answer this type is
    /// allowed to give, and it must not be reachable by accident from a read that did not happen.
    enum Answer {
        case reading([String: Any])
        case refused(Refusal)

        var payload: [String: Any]? {
            if case .reading(let payload) = self { return payload }
            return nil
        }

        var refusal: Refusal? {
            if case .refused(let refusal) = self { return refusal }
            return nil
        }
    }

    static let schemaVersion = 1

    private let readRows: (UsageLedger.AnalyticsFilter) -> UsageLedger.AnalyticsRead
    /// What the task registry says right now about the tasks these rows belong to: the landing
    /// obligation, the title, whether it is still running, and whether anything it wrote is on
    /// record. Injected the way `UsageQueryService` injects its schedule labels: a test says what
    /// the registry holds without a registry existing.
    ///
    /// It replaces a narrower `readLiveTaskIDs` that answered only the liveness half. Liveness is
    /// still read from it — `isLive` on each record — so nothing below lost that question; what
    /// it gained is the ability to ask the registry the landing question too, instead of reading
    /// the copy a row froze when it was written.
    private let readLiveTaskRecords: () -> [String: UsageLedger.LiveTaskRecord]
    /// What git says about this repository's delivery branches. Injected the same way liveness
    /// is, and for the same reason: a test says what the branches are without a repository
    /// existing.
    ///
    /// It is ``Orchestrator/repositoryBranches(in:)`` in production — **two** `for-each-ref`
    /// calls for the whole repository rather than one subprocess per worktree, which is what
    /// makes asking git affordable on a read that already joins a hundred thousand rows.
    private let readBranches: (String) -> Orchestrator.RepositoryBranches
    /// The commit each delivery branch was cut from, which is the one fact git cannot supply
    /// about its own branches and the one that tells a merged delivery from a branch that never
    /// received a commit. Injected through the same seam as the two above, and for the third
    /// time for the same reason: this read must be describable in a test without a registry and
    /// without a repository. See ``branchEvidence(worktree:branches:bases:)``.
    private let readWorktreeBases: () -> [String: String]

    init() {
        readRows = { UsageLedger.shared.analyticsRead($0) }
        readLiveTaskRecords = { Orchestrator.usageLiveTaskRecords() }
        readBranches = { Orchestrator.repositoryBranches(in: $0) }
        readWorktreeBases = { Orchestrator.usageWorktreeBases() }
    }

    /// Test seam. Production never enters here: the bounded predicate lives in
    /// `UsageLedger.analyticsRead`, and this initializer's rows arrive already in memory.
    ///
    /// `branches` defaults to a repository git never answered for, which is exactly the state
    /// this service was in before it asked: every verdict below then comes out of the stored
    /// columns alone, so a test that says nothing about branches is asserting the old ladder.
    ///
    /// `worktreeBases` defaults to a registry that recorded no base for any branch, which is the
    /// fail-safe half of ``branchEvidence(worktree:branches:bases:)`` rather than a convenience:
    /// a test that describes a merged branch and says nothing about what it was cut from gets
    /// `branch_base_unknown` and no upgrade, exactly as production does for a task record the
    /// registry has swept.
    init(rows: @escaping () -> [UsageLedger.Row],
         acceptedFeatures: @escaping () -> [String: UsageLedger.AcceptedAttribution] = { [:] },
         acceptedProjects: @escaping () -> [String: UsageLedger.AcceptedAttribution] = { [:] },
         liveTaskIDs: @escaping () -> Set<String> = { [] },
         liveTaskRecords: @escaping () -> [String: UsageLedger.LiveTaskRecord] = { [:] },
         branches: @escaping (String) -> Orchestrator.RepositoryBranches
             = { _ in Orchestrator.RepositoryBranches() },
         worktreeBases: @escaping () -> [String: String] = { [:] }) {
        readBranches = branches
        readWorktreeBases = worktreeBases
        readRows = { _ in
            let rows = rows()
            return UsageLedger.AnalyticsRead(
                rows: rows, corrections: 0,
                latestLedgerObservation: rows.map(\.updatedAt).max(),
                acceptedFeatures: acceptedFeatures(), acceptedProjects: acceptedProjects())
        }
        // A test that names live ids and nothing else is naming exactly that: those tasks are
        // running, and the registry holds no landing for them. Anything it does not name has no
        // record at all, which is the case where the row's own frozen copy is all there is.
        readLiveTaskRecords = {
            var records = liveTaskRecords()
            for id in liveTaskIDs() where records[id] == nil {
                records[id] = UsageLedger.LiveTaskRecord(isLive: true)
            }
            return records
        }
    }

    /// A closed query, refused on an unknown or repeated key for the same reason the analytics
    /// query is: a misspelled filter must not quietly widen an accounting read.
    static func parse(_ values: [String: String], repeatedKeys: Set<String>) -> ParseResult {
        let allowed: Set<String> = ["project", "from", "to", "timezone"]
        let unknown = Set(values.keys).subtracting(allowed)
        guard unknown.isEmpty else {
            return ParseResult(error: "Unknown project-worktrees query field: "
                                 + unknown.sorted().joined(separator: ", ") + ".")
        }
        guard repeatedKeys.isEmpty else {
            return ParseResult(error: "Project-worktrees query fields may appear only once.")
        }
        let project = values["project"]?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !project.isEmpty else {
            return ParseResult(error: "project is required: the Portfolio's project id, the "
                                 + "Project's final name, or its absolute canonical path.")
        }
        let timezoneID = values["timezone"].flatMap { $0.isEmpty ? nil : $0 }
            ?? TimeZone.autoupdatingCurrent.identifier
        guard TimeZone(identifier: timezoneID) != nil else {
            return ParseResult(error: "timezone must be an IANA timezone identifier.")
        }
        let from = values["from"].flatMap { $0.isEmpty ? nil : $0 }
        let to = values["to"].flatMap { $0.isEmpty ? nil : $0 }
        guard [from, to].allSatisfy({ $0 == nil || UsageLedger.isLocalDay($0!) }) else {
            return ParseResult(error: "from and to are local dates, YYYY-MM-DD.")
        }
        guard from == nil || to == nil || from! <= to! else {
            return ParseResult(error: "from must not be after to.")
        }
        return ParseResult(query: Query(project: project, from: from, to: to,
                                        timezoneID: timezoneID))
    }

    /// The Portfolio's opaque Project id: `project-` and exactly sixteen lowercase hex digits.
    /// Matched by shape rather than by prefix so that a Project whose directory really is called
    /// `project-something` is still reachable by its name.
    static func isProjectID(_ value: String) -> Bool {
        guard value.hasPrefix("project-") else { return false }
        let digest = value.dropFirst("project-".count)
        return digest.count == 16 && digest.allSatisfy { $0.isNumber || ("a"..."f").contains($0) }
    }

    func read(_ query: Query, now: Date = Date()) -> Answer {
        let timezone = TimeZone(identifier: query.timezoneID) ?? TimeZone(secondsFromGMT: 0)!
        var calendar = Calendar(identifier: .gregorian)
        calendar.locale = Locale(identifier: "en_US_POSIX")
        calendar.timeZone = timezone
        let bounds = UsageQueryService.dateBounds(from: query.from, to: query.to,
                                                  calendar: calendar)
        let reading = readRows(UsageLedger.AnalyticsFilter(
            start: bounds.start, end: bounds.end,
            limit: UsageQueryService.maxScannedRows + 1))
        // Production has already applied these bounds in SQLite; reapplying them keeps the
        // injectable seam above honest without changing the bounded production cost.
        let inRange = reading.rows.filter { row in
            if let start = bounds.start, row.startedAt < start { return false }
            if let end = bounds.end, row.startedAt >= end { return false }
            return true
        }
        let truncated = inRange.count > UsageQueryService.maxScannedRows
        let rows = Array(inRange.prefix(UsageQueryService.maxScannedRows))

        // One pass by the Portfolio's own rule, so a row lands in the Project the Projects table
        // put it in. A second copy of that rule split three work lines into six Features once.
        var groups: [String: (label: String, rows: [UsageLedger.Row])] = [:]
        var unattributed: [String: Set<String>] = [:]
        for row in rows {
            let accepted = reading.acceptedProjects[row.intervalKey]
            let resolution = UsageFeatureClassifier.resolveProject(
                projectKey: row.projectKey, acceptedProjectIdentity: accepted?.id)
            guard let identity = resolution.identity else {
                guard let directory = row.workingDir,
                      let worktree = UsageLedger.legacyManagedWorktreeTaskID(directory)
                else { continue }
                unattributed[resolution.refusal?.rawValue ?? "project_unresolved",
                             default: []].insert(worktree)
                continue
            }
            let label = UsageQueryService.projectLabel(identity, acceptedLabel: accepted?.label)
            if groups[identity] == nil { groups[identity] = (label, []) }
            groups[identity]?.rows.append(row)
        }

        let wanted = query.project.trimmingCharacters(in: .whitespacesAndNewlines)
        let wantedPath = URL(fileURLWithPath: wanted).standardizedFileURL.path
        let candidates = groups.filter { identity, group in
            if Self.isProjectID(wanted) { return UsageQueryService.projectID(identity) == wanted }
            return group.label == wanted || (wanted.hasPrefix("/") && identity == wantedPath)
        }
        // **Two Projects may share a final name**, and a name is what the Portfolio puts on
        // screen. Answering for whichever the dictionary happened to hand over first would file
        // one repository's worktrees under another's, so the ambiguity is refused with both ids
        // in the message — the caller has an unambiguous handle and is told to use it.
        guard candidates.count <= 1 else {
            return .refused(Refusal(
                status: 409, code: "ambiguous_project",
                message: "\(candidates.count) Projects in this range are named \(wanted). Ask by "
                    + "id: " + candidates.keys.map { UsageQueryService.projectID($0) }.sorted()
                        .joined(separator: ", ") + "."))
        }
        guard let matched = candidates.first else {
            // **A Project nothing in this window mentions is a refusal, not an empty list.** The
            // two are the same picture on a screen and completely different facts, and this is
            // the one of them a reader can act on: narrow the question or widen the range.
            return .refused(Refusal(
                status: 404, code: "project_not_found",
                message: "No usage row in this range resolves to a Project named "
                    + "\(wanted). \(rows.count) row(s) were read."))
        }

        var worktrees: [String: [UsageLedger.Row]] = [:]
        var worktreeRows = 0
        var featureRows = 0
        for row in matched.value.rows {
            guard let directory = row.workingDir,
                  let id = UsageLedger.legacyManagedWorktreeTaskID(directory) else { continue }
            worktreeRows += 1
            if reading.acceptedFeatures[row.intervalKey] != nil { featureRows += 1 }
            worktrees[id, default: []].append(row)
        }

        // **The join that makes this read current.** One registry snapshot, applied to every row
        // below: `records` is what is true now and the rows keep what they froze.
        let records = readLiveTaskRecords()
        let live = Set(records.filter { $0.value.isLive }.keys)
        // Asked once for the whole repository, and only when there is something to ask about: two
        // `for-each-ref` calls answer for every worktree below, and a Project whose work never
        // left the shared checkout costs no subprocess at all. `matched.key` is the canonical
        // repository path the Portfolio resolved, which is the directory these branches live in.
        //
        // **And only when that key is a path.** `UsageFeatureClassifier.resolveProject` returns
        // an accepted Project identity unchanged, and an accepted identity is whatever a person
        // accepted rather than something the type says is a directory. Handing a non-path to
        // `repositoryBranches(in:)` would run `for-each-ref` in the app's own current directory
        // — and if that happened to sit inside a repository, another repository's branches would
        // decide this Project's verdicts with `known == true`. Today nothing can write such an
        // identity; this is the line that keeps it that way.
        let branches = worktrees.isEmpty || !matched.key.hasPrefix("/")
            ? Orchestrator.RepositoryBranches() : readBranches(matched.key)
        // Read once beside the branches, and only when git answered: with no branch facts there
        // is nothing for a base to qualify.
        let bases = branches.known ? readWorktreeBases() : [:]
        var payloads: [[String: Any]] = []
        var withoutFeature = 0
        for (id, rows) in worktrees {
            let readings = rows.map { Reading($0, records: records) }
            let branch = Self.branchEvidence(worktree: id, branches: branches, bases: bases)
            let features = Self.features(readings, accepted: reading.acceptedFeatures, live: live,
                                         branch: branch)
            guard !features.isEmpty else {
                // The 58-checkout answer the person did not ask for. Counted, never listed.
                withoutFeature += 1
                continue
            }
            let carried = readings.filter { reading.acceptedFeatures[$0.row.intervalKey] != nil }
            payloads.append(Self.worktree(id: id, rows: carried, features: features, live: live,
                                          branch: branch))
        }
        payloads.sort {
            let left = $0["lastSeenAt"] as? String ?? "", right = $1["lastSeenAt"] as? String ?? ""
            if left != right { return left > right }
            return ($0["id"] as? String ?? "") < ($1["id"] as? String ?? "")
        }

        let formatter = ISO8601DateFormatter()
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        var range: [String: Any] = ["timezone": query.timezoneID]
        range["from"] = query.from as Any? ?? NSNull()
        range["to"] = query.to as Any? ?? NSNull()
        return .reading([
            "schemaVersion": Self.schemaVersion,
            // `partial` says the scan hit its cap, which is the one way this list can be short
            // without the store being short. It is never the word for an empty answer.
            "status": truncated ? "partial" : "available",
            "policy": "one_unambiguous_accepted_head",
            "outcomeRule": "landed_by_record_then_settled_then_landed_by_nonempty_merged_branch_"
                + "then_branch_gone_then_delivered_then_live_then_abandoned",
            "generatedAt": formatter.string(from: now),
            "range": range,
            "project": ["id": UsageQueryService.projectID(matched.key),
                        "label": matched.value.label],
            // **The receipt that says this query ran.** An empty `worktrees` with rows behind it
            // is an answer; the same list with nothing behind it is a read that did not happen,
            // and only these counts can tell a reader which one they are looking at.
            "read": ["rows": rows.count, "projectRows": matched.value.rows.count,
                     "worktreeRows": worktreeRows, "featureRows": featureRows,
                     "truncated": truncated,
                     "maxScannedRows": UsageQueryService.maxScannedRows],
            "worktrees": payloads,
            "excluded": ["worktreesWithoutFeature": withoutFeature,
                         "reason": "no_unambiguous_accepted_head"],
            "unattributed": ["worktrees": unattributed.values.reduce(into: Set<String>()) {
                                 $0.formUnion($1)
                             }.count,
                             "reasons": unattributed.mapValues(\.count)],
        ])
    }

    /// **One ledger row read together with the registry record for the task that produced it.**
    ///
    /// The row is history and is never edited — this store is append-only, and a sealed row's
    /// numbers may already have been quoted in a month's total. The record is the answer. Both
    /// travel together so that the payload can say which of the two each verdict rests on, which
    /// is the difference between a stale reading and a stale reading that admits it.
    struct Reading {
        let row: UsageLedger.Row
        let live: UsageLedger.LiveTaskRecord?

        init(_ row: UsageLedger.Row, records: [String: UsageLedger.LiveTaskRecord]) {
            self.row = row
            self.live = row.taskID?.nonEmpty.flatMap { records[$0] }
        }

        /// The stored-copy-only reading: what a row says when the registry no longer holds its
        /// task, and the shape a unit test drives the ladder with.
        init(stored row: UsageLedger.Row) {
            self.row = row
            self.live = nil
        }

        /// The obligation as it stands now. The registry wins wherever it still has the task —
        /// including when it holds no landing at all, which is then the true answer and not an
        /// absence to paper over with the copy.
        var landingState: String? {
            guard let live else { return row.landingState?.nonEmpty }
            return live.landingState?.nonEmpty
        }

        var storedLandingState: String? { row.landingState?.nonEmpty }
        /// `live` where the registry answered for this row, `stored` where only the copy is left.
        var basis: String { live == nil ? "stored" : "live" }
    }

    /// The Features one worktree carries, each with the outcome of its own rows.
    ///
    /// The branch fact is the worktree's, and every Feature inside it was delivered on that one
    /// branch — so it is handed down rather than looked up again. A Feature's own rows may still
    /// carry a landing record of their own, which is stronger and is read per Feature.
    private static func features(_ rows: [Reading],
                                 accepted: [String: UsageLedger.AcceptedAttribution],
                                 live: Set<String>,
                                 branch: LandingEvidence) -> [[String: Any]] {
        var grouped: [String: (label: String, rows: [Reading])] = [:]
        for row in rows {
            guard let head = accepted[row.row.intervalKey] else { continue }
            if grouped[head.id] == nil { grouped[head.id] = (head.label, []) }
            grouped[head.id]?.rows.append(row)
        }
        return grouped.map { id, group in
            var payload = summary(group.rows, live: live, branch: branch)
            payload["id"] = id
            payload["label"] = group.label
            return payload
        }.sorted {
            let left = $0["lastSeenAt"] as? String ?? "", right = $1["lastSeenAt"] as? String ?? ""
            if left != right { return left > right }
            return ($0["id"] as? String ?? "") < ($1["id"] as? String ?? "")
        }
    }

    private static func worktree(id: String, rows: [Reading],
                                 features: [[String: Any]], live: Set<String>,
                                 branch: LandingEvidence) -> [String: Any] {
        var payload = summary(rows, live: live, branch: branch)
        payload["id"] = id
        payload["features"] = features
        payload["needs"] = needs(rows, live: live, branch: branch) as Any? ?? NSNull()
        return payload
    }

    /// **What would take one row out of the "done, never landed" block**, or nil where nothing
    /// would: a row that is already settled needs nothing, and this says so by not answering.
    ///
    /// It is the means and not the outcome. Nothing here closes anything: a landing record is
    /// durable and terminal, and one closed on a guess is worse than a wrong count. What it does
    /// is tell the two cases apart using the same predicate the landing route admits
    /// `nothing_to_land` by, so a row can never advise a close the route would refuse.
    ///
    /// * `no_record` — the registry has swept at least one of these tasks. There is nothing left
    ///   to call `POST /v1/orchestrator/tasks/:id/landing` with, so this row is history now.
    /// * `nothing_to_land` — every one of its tasks is admissible: no claims, no commits on the
    ///   branch, no dirty checkout, no target already named.
    /// * `land_or_abandon` — something here wrote, so a person decides. That is the whole of the
    ///   remaining backlog once the two above are taken out.
    ///
    /// **The branch fact travels in so that this asks the ladder the payload published.** Without
    /// it, a worktree git has already shown to be landed would compute `delivered` here and be
    /// advised to land itself, on the same screen that calls it landed.
    static func needs(_ rows: [Reading], live: Set<String>,
                      branch: LandingEvidence = .unknown) -> String? {
        guard outcome(rows, live: live, branch: branch) == .delivered else { return nil }
        let tasks = Set(rows.compactMap { $0.row.taskID?.nonEmpty })
        guard !tasks.isEmpty else { return "no_record" }
        let known = rows.compactMap { $0.live }
        guard known.count == rows.filter({ $0.row.taskID?.nonEmpty != nil }).count else {
            return "no_record"
        }
        return known.allSatisfy(\.nothingToLand) ? "nothing_to_land" : "land_or_abandon"
    }

    /// The shape both a worktree and one of its Features report: the verdict, what that verdict
    /// rests on, the words it was read from, what the work itself was, and the tasks and instants
    /// behind it.
    private static func summary(_ rows: [Reading], live: Set<String>,
                                branch: LandingEvidence) -> [String: Any] {
        let formatter = ISO8601DateFormatter()
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        let tasks = Set(rows.compactMap { $0.row.taskID?.nonEmpty }).sorted()
        let landingStates = Set(rows.compactMap(\.landingState)).sorted()
        let stored = Set(rows.compactMap(\.storedLandingState)).sorted()
        let taskStates = Set(rows.compactMap { $0.row.taskState?.nonEmpty }).sorted()
        let bases = Set(rows.filter { $0.row.taskID?.nonEmpty != nil }.map(\.basis))
        return [
            "outcome": outcome(rows, live: live, branch: branch).rawValue,
            "landingEvidence": evidence(rows, branch: branch).rawValue,
            "runs": tasks.isEmpty ? rows.count : tasks.count,
            "tasks": tasks,
            "liveTasks": tasks.filter(live.contains),
            "taskStates": taskStates,
            // What is true now, what the rows froze, and which of the two the verdict rests on.
            "landingStates": landingStates,
            "storedLandingStates": stored,
            "landingBasis": bases.count == 1 ? (bases.first ?? "none")
                : (bases.isEmpty ? "none" : "mixed"),
            "work": work(rows) as Any? ?? NSNull(),
            "firstSeenAt": rows.map(\.row.startedAt).min().map(formatter.string(from:))
                as Any? ?? NSNull(),
            "lastSeenAt": rows.map(\.row.startedAt).max().map(formatter.string(from:))
                as Any? ?? NSNull(),
        ]
    }

    /// **What the work was**, as against which root owned it.
    ///
    /// Every card on the Projects page was titled with the accepted head's label — `Clawdfather —
    /// handoff 18bde7c3` on nine of them at once — because that label is the *work line* a
    /// classifier grouped by, and a work line is not an answer to "what is this". The task's own
    /// title is, and it is already stored: `6769836c` calls itself 「一輪 correction：handoff
    /// sender contract 的八個 finding」.
    ///
    /// **The rule where one Feature covers several tasks is the most recently seen one**, and it
    /// is a headline rather than a summary: nothing here invents a sentence, and the alternatives
    /// stay one lookup away because `tasks` is in the same payload. Most such groups are a
    /// delivery and its corrections, and the newest is the one somebody is deciding about.
    ///
    /// A title lives only in the registry — the ledger stores none — so this is empty for a task
    /// old enough to have been swept, and empty is what it says rather than borrowing the label
    /// above it.
    private static func work(_ rows: [Reading]) -> String? {
        rows.filter { $0.live?.title != nil }
            .max { $0.row.startedAt < $1.row.startedAt }?.live?.title
    }

    /// The ladder in ``Outcome``, and the only place it is written down.
    ///
    /// `branch` defaults to ``LandingEvidence/unknown``, which is not a convenience: it is the
    /// answer this function gave before it had a second source, so every caller that cannot say
    /// what git thinks gets exactly the old ladder rather than a guess in either direction.
    static func outcome(_ rows: [Reading], live: Set<String>,
                        branch: LandingEvidence = .unknown) -> Outcome {
        if rows.contains(where: { $0.landingState == Orchestrator.LandingState.landed.rawValue }) {
            return .landed
        }
        // A delivery settled as having had nothing to land. Read here, above the two git rungs,
        // for the same reason the veto below guards them: this is a settlement somebody recorded,
        // and a branch's shape is not an appeal against one. A read-only delivery commits
        // nothing, so its branch is `branch_empty` and no git rung would have claimed it anyway —
        // but the order is the reason, not the coincidence.
        if rows.contains(where: {
            $0.landingState == Orchestrator.LandingState.nothingToLand.rawValue
        }) {
            return .nothingToLand
        }
        // **A decision a person made is not overruled by the shape of a repository.** A root that
        // wrote `abandoned` looked at this delivery and gave the obligation up; the two rungs
        // this guards would otherwise call the same worktree `landed` because somebody's HEAD
        // happens to contain the branch, or `branchGone` because somebody deleted it. Both are
        // git noticing a shape, and neither is news to the root that decided. It guards those two
        // rungs and nothing under them, so the ladder below is exactly the one that ran before
        // git was a source: a given-up obligation on work that succeeded is still `delivered`,
        // with `abandoned` travelling beside it in `landingStates`.
        let givenUp = rows.contains {
            $0.landingState == Orchestrator.LandingState.abandoned.rawValue
        }
        if !givenUp {
            if branch == .branchMerged { return .landed }
            // A delivery whose branch git can no longer find. Not `landed` — the app deletes a
            // branch only when it is empty, but the app is not the only deleter, and eight
            // branches it kept for their commits have gone missing on this Mac — and not
            // `delivered` either, because there is no branch left for anybody to land.
            if branch == .branchAbsent,
               rows.contains(where: { $0.row.taskState == Orchestrator.State.success.rawValue }) {
                return .branchGone
            }
        }
        if rows.contains(where: { $0.row.taskState == Orchestrator.State.success.rawValue }) {
            return .delivered
        }
        if rows.contains(where: { $0.row.taskID.map(live.contains) == true }) { return .active }
        if rows.contains(where: { $0.row.taskState?.nonEmpty != nil }) { return .abandoned }
        return .unknown
    }

    /// The ladder read from stored rows alone, which is what a row carries once the registry has
    /// swept its task.
    static func outcome(_ rows: [UsageLedger.Row], live: Set<String>,
                        branch: LandingEvidence = .unknown) -> Outcome {
        outcome(rows.map(Reading.init(stored:)), live: live, branch: branch)
    }

    /// What the verdict above rests on, for these rows and this branch fact. A landing record is
    /// the stronger of the two and is read per row set, so a Feature carrying its own landing
    /// says `record` even inside a worktree whose branch nobody merged.
    ///
    /// It reads ``Reading/landingState``, which is the registry's answer wherever the registry
    /// still holds the task — so a landing filed after these rows were written says `record`
    /// here, on the same read that files it.
    static func evidence(_ rows: [Reading], branch: LandingEvidence) -> LandingEvidence {
        rows.contains { $0.landingState == Orchestrator.LandingState.landed.rawValue }
            ? .record : branch
    }

    /// The same question asked of stored rows alone, for a caller that has no registry snapshot.
    static func evidence(_ rows: [UsageLedger.Row], branch: LandingEvidence) -> LandingEvidence {
        evidence(rows.map(Reading.init(stored:)), branch: branch)
    }

    /// The branch fact for one worktree, out of the two `for-each-ref` readings taken for the
    /// whole repository.
    ///
    /// **The branch name is a convention and is spelled in exactly one place**, which is the
    /// place the worktree was created from: `OrchestratorDraft.worktreeBranch(for:)`. A worktree
    /// id that is not a task id builds no branch name and is therefore `unknown` rather than
    /// `branch_absent` — the difference between *git has no such branch* and *this side never
    /// asked about one*.
    ///
    /// **A branch with no commits on it is contained by HEAD for free, and that is not a
    /// landing.** `OrchestratorDraft.addWorktree` runs `git worktree add -b <branch> <path>
    /// <base>`, so a delivery branch begins life pointing at the base commit — an ancestor of
    /// HEAD from the moment it exists. Reading that as "merged" turns every delivery that has not
    /// committed yet into a landing: on 2026-09-06 that was 12 of this Mac's 75 contained
    /// delivery branches, 10 of them with an uncommitted checkout still sitting on disk, which is
    /// the ordinary shape of a Codex worktree child — `dispatch-policy.md` tells one to leave its
    /// bytes dirty for the root, because a linked worktree's git metadata is outside what that
    /// sandbox may write.
    ///
    /// So `bases` — the commit each branch was cut from, out of the registry's own task records —
    /// is what separates the two, and **a base nobody can supply refuses the upgrade rather than
    /// granting it**. The costs are not symmetric in that direction either: calling an unlanded
    /// delivery landed costs somebody a day of rebuilding it, and the other way costs a glance.
    ///
    /// **Two things this cannot see, deliberately.** `--merged HEAD` is ancestry, so a delivery
    /// that was squashed or cherry-picked into the target still reads as unmerged; running
    /// `git cherry` per branch would be one subprocess per worktree on a read that already costs
    /// two. And that `HEAD` is the main checkout's current HEAD, which is not literally `main` —
    /// on a repository parked on another branch, "merged" means "contained by whatever is checked
    /// out there".
    static func branchEvidence(worktree id: String,
                               branches: Orchestrator.RepositoryBranches,
                               bases: [String: String]) -> LandingEvidence {
        guard branches.known, let branch = OrchestratorDraft.worktreeBranch(for: id) else {
            return .unknown
        }
        // The listing of what exists decides absence, before containment is consulted at all: a
        // name in `merged` that the listing has no line for is a reading that disagrees with
        // itself, and the safe half of it is that this side cannot see the branch.
        guard let head = branches.heads[branch] else { return .branchAbsent }
        guard branches.merged.contains(branch) else { return .branchUnmerged }
        guard let base = bases[branch] else { return .branchBaseUnknown }
        return head == base ? .branchEmpty : .branchMerged
    }
}

private extension String {
    var nonEmpty: String? { isEmpty ? nil : self }
}
