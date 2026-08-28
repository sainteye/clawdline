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
    static let storeVersion = 3

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
               source_bytes, started_at, ended_at, local_day, updated_at, input_basis
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
        "cache_read", "cache_write", "total", "measured", "source_total", "reconciliation",
        "input_basis", "cost_value", "cost_unit", "cost_basis", "price_snapshot_id",
        "missing_reason", "coverage", "coverage_reasons", "sealed", "started_at", "ended_at",
    ] + reservedColumns

    /// The whole range as CSV. **An unknown is an empty field, never `0`** — including every
    /// reserved column, which is empty in every row of schema 1 and is present so that a reader
    /// can see it is reserved rather than wonder where it went.
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
