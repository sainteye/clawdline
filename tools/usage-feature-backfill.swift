// A read-only dry run of the local Feature classifier over a copy of a usage ledger.
//
//   swiftc -O -o /tmp/usage-feature-backfill \
//       tools/usage-feature-backfill.swift Sources/UsageFeatureClassifier.swift
//   sqlite3 <live.sqlite3> ".backup <copy.sqlite3>"
//   sqlite3 <copy.sqlite3> "PRAGMA journal_mode=DELETE;"   # the copy is WAL; see rows(at:)
//   /tmp/usage-feature-backfill --ledger <copy.sqlite3> --facts <task-facts.json> \
//       [--threshold 0.80]
//
// **It only reads.** The ledger is opened `SQLITE_OPEN_READONLY` and no attribution event, and no
// token row, is written anywhere by this program. What it answers is one question: how much of a
// real ledger the ladder in `Sources/UsageFeatureClassifier.swift` can actually name, and what it
// declines when it cannot.
//
// It assembles `Evidence` from SQL columns itself, because it compiles against the classifier
// alone — the classifier names no other type in this repository, which is what makes that
// possible. Those assembly rules are the ones `UsageLedger.featureEvidence(rows:taskFacts:
// scheduleTitles:)` applies in `Sources/UsageFeatureAttribution.swift`; that function and the
// production producer share one implementation, and this instrument mirrors it. A change to
// either belongs in both. The one rule it does not mirror but *calls* is the Project scope:
// `UsageFeatureClassifier.resolveProject` is shared code, because a Feature scoped by a Project
// the Projects table refuses to name is how one work line became two Features.

import Foundation
import SQLite3

@main
enum UsageFeatureBackfill {
    /// What the durable broker record contributes, as `task-facts.json` carries it.
    struct FactRecord {
        var title: String?
        var declaredWorkLine: String?
        var planHeadline: String?
        var kind: String?
        var parentTaskID: String?
        var retryOf: String?
    }

    /// The columns of one `usage_intervals` row this instrument is allowed to look at.
    struct RowFacts {
        var intervalKey = ""
        var taskID: String?
        var projectKey: String?
        var scheduleID: String?
        var kindRaw: String?
        var parentTaskID: String?
        var retryOf: String?
        var boundaryKind = ""
        var boundaryID = ""
        var sessionID = ""
    }

    /// One Feature's totals. `rung` and `confidence` are the **strongest** rung anything in the
    /// group reached, not the first one that happened to arrive: rung 4 copies its parent's
    /// Feature id, so a Feature holding both an explicit hint and rows that inherited it would
    /// otherwise print whichever the batch reached first.
    struct FeatureTally {
        var label = ""
        var rung = UsageFeatureClassifier.Rung.lineage
        var confidence = 0.0
        var rows = 0
        var runs: Set<String> = []
        var rungs: [String: Int] = [:]
    }

    static func fail(_ message: String) -> Never {
        FileHandle.standardError.write(Data((message + "\n").utf8))
        exit(2)
    }

    static func nonEmpty(_ value: String?) -> String? {
        guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines),
              !value.isEmpty else { return nil }
        return value
    }

    /// The first line of a plan, trimmed, and never more than 200 characters of it.
    static func planHeadline(_ plan: String?) -> String? {
        guard let plan else { return nil }
        let headline = plan.split(whereSeparator: \.isNewline).first
            .map { String($0).trimmingCharacters(in: .whitespaces) } ?? ""
        return headline.isEmpty ? nil : String(headline.prefix(200))
    }

    /// Exactly `UsageLedger.runID` — the dashboard counts runs this way, and a number counted any
    /// other way cannot be compared with what it shows.
    static func runID(_ row: RowFacts) -> String {
        if let task = row.taskID { return "task:" + task }
        let kind = row.boundaryKind.trimmingCharacters(in: .whitespacesAndNewlines)
        let boundary = row.boundaryID.trimmingCharacters(in: .whitespacesAndNewlines)
        if !kind.isEmpty, !boundary.isEmpty { return kind + ":" + boundary }
        let session = row.sessionID.trimmingCharacters(in: .whitespacesAndNewlines)
        if !session.isEmpty { return "session:" + session }
        return "interval:" + row.intervalKey
    }

    static func padded(_ value: String, _ width: Int) -> String {
        value.count >= width ? value : value + String(repeating: " ", count: width - value.count)
    }

    static func right(_ value: Int, _ width: Int) -> String {
        let text = String(value)
        return text.count >= width
            ? text : String(repeating: " ", count: width - text.count) + text
    }

    static func taskFacts(at path: String) -> [String: FactRecord] {
        guard let data = FileManager.default.contents(atPath: path),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { fail("could not read task facts from \(path)") }
        var facts: [String: FactRecord] = [:]
        for (taskID, raw) in object {
            let fields = raw as? [String: Any] ?? [:]
            facts[taskID] = FactRecord(
                title: nonEmpty(fields["title"] as? String),
                declaredWorkLine: nonEmpty(fields["declaredWorkLine"] as? String),
                planHeadline: planHeadline(nonEmpty(fields["planHeadline"] as? String)),
                kind: nonEmpty(fields["kind"] as? String),
                parentTaskID: nonEmpty(fields["parentTaskID"] as? String),
                retryOf: nonEmpty(fields["retryOf"] as? String))
        }
        return facts
    }

    static func rows(at path: String) -> (rows: [RowFacts], acceptedProjects: [String: String]) {
        var database: OpaquePointer?
        guard sqlite3_open_v2(path, &database, SQLITE_OPEN_READONLY, nil) == SQLITE_OK,
              let db = database else { fail("could not open \(path) read-only") }
        defer { sqlite3_close(db) }
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            SELECT interval_key, task_id, project_key, schedule_id, kind_raw, parent_task_id,
                   retry_of, boundary_kind, boundary_id, session_id
              FROM usage_intervals ORDER BY started_at, interval_key;
            """, -1, &statement, nil) == SQLITE_OK else {
            // Say what SQLite said. A `.backup` copy of the live ledger is in WAL mode, and a
            // read-only connection to a WAL database whose `-shm` file is absent cannot build
            // one, so the first read fails with `unable to open database file` — which read as
            // "the table is missing" until this message named it.
            fail("could not read usage_intervals: " + String(cString: sqlite3_errmsg(db))
                + " (a WAL copy needs `sqlite3 <copy> 'PRAGMA journal_mode=DELETE;'` first)")
        }
        func text(_ index: Int32) -> String? {
            guard let raw = sqlite3_column_text(statement, index) else { return nil }
            return String(cString: raw)
        }
        var out: [RowFacts] = []
        while sqlite3_step(statement) == SQLITE_ROW {
            var row = RowFacts()
            row.intervalKey = text(0) ?? ""
            row.taskID = nonEmpty(text(1))
            row.projectKey = nonEmpty(text(2))
            row.scheduleID = nonEmpty(text(3))
            row.kindRaw = nonEmpty(text(4))
            row.parentTaskID = nonEmpty(text(5))
            row.retryOf = nonEmpty(text(6))
            row.boundaryKind = text(7) ?? ""
            row.boundaryID = text(8) ?? ""
            row.sessionID = text(9) ?? ""
            guard !row.intervalKey.isEmpty else { continue }
            out.append(row)
        }
        return (out, acceptedProjects(db))
    }

    /// The one active accepted Project head per interval — the same "exactly one, nothing
    /// supersedes it" reading `UsageLedger.acceptedHead(from:)` applies. A Feature is scoped by
    /// the Project a surface would name, so this instrument has to see what the Projects table
    /// sees or its Feature count is not the one the dashboard would show.
    static func acceptedProjects(_ db: OpaquePointer) -> [String: String] {
        var statement: OpaquePointer?
        defer { sqlite3_finalize(statement) }
        guard sqlite3_prepare_v2(db, """
            SELECT e.interval_key, e.value_id FROM usage_attribution_events e
             WHERE e.dimension = 'project' AND e.decision = 'accepted'
               AND NOT EXISTS (
                 SELECT 1 FROM usage_attribution_events successor
                  WHERE successor.supersedes_event_id = e.event_id
               )
             GROUP BY e.interval_key HAVING COUNT(*) = 1;
            """, -1, &statement, nil) == SQLITE_OK else { return [:] }
        var accepted: [String: String] = [:]
        while sqlite3_step(statement) == SQLITE_ROW {
            guard let key = sqlite3_column_text(statement, 0),
                  let value = sqlite3_column_text(statement, 1) else { continue }
            accepted[String(cString: key)] = String(cString: value)
        }
        return accepted
    }

    static func main() {
        var ledgerPath: String?
        var factsPath: String?
        var threshold = UsageFeatureClassifier.defaultAcceptanceThreshold
        var arguments = Array(CommandLine.arguments.dropFirst())
        while let flag = arguments.first {
            arguments.removeFirst()
            switch flag {
            case "--ledger":
                guard let value = arguments.first else { fail("--ledger needs a path") }
                ledgerPath = value
                arguments.removeFirst()
            case "--facts":
                guard let value = arguments.first else { fail("--facts needs a path") }
                factsPath = value
                arguments.removeFirst()
            case "--threshold":
                guard let value = arguments.first, let parsed = Double(value) else {
                    fail("--threshold needs a number")
                }
                threshold = parsed
                arguments.removeFirst()
            default:
                fail("unknown argument: \(flag)")
            }
        }
        guard let ledgerPath, let factsPath else {
            fail("usage: usage-feature-backfill --ledger <sqlite3> --facts <json>"
                + " [--threshold 0.80]")
        }

        let facts = taskFacts(at: factsPath)
        let reading = rows(at: ledgerPath)
        let ledgerRows = reading.rows

        // Schedule titles are a live registry read the production producer makes and this
        // instrument cannot: a ledger copy carries the schedule id and not its name. Rung 2 falls
        // back to the id, exactly as it does in production for a schedule whose file has gone.
        let evidence = ledgerRows.map { row -> UsageFeatureClassifier.Evidence in
            let record = row.taskID.flatMap { facts[$0] }
            // The Project scope, by the one rule both surfaces use: an accepted Project head
            // first, then the canonical key, and a legacy managed-worktree path scoped to no
            // Project at all rather than to one of its own.
            let project = UsageFeatureClassifier.resolveProject(
                projectKey: row.projectKey,
                acceptedProjectIdentity: reading.acceptedProjects[row.intervalKey]).identity
            return UsageFeatureClassifier.Evidence(
                intervalKey: row.intervalKey, projectKey: project, taskID: row.taskID,
                hasDurableTaskRecord: record != nil,
                taskKind: row.kindRaw ?? record?.kind, taskTitle: record?.title,
                declaredWorkLine: record?.declaredWorkLine, planHeadline: record?.planHeadline,
                scheduleID: row.scheduleID, scheduleTitle: nil,
                parentTaskID: row.parentTaskID ?? record?.parentTaskID,
                retryOf: row.retryOf ?? record?.retryOf)
        }
        let outcome = UsageFeatureClassifier.classify(evidence)

        var rowsByInterval: [String: RowFacts] = [:]
        for row in ledgerRows { rowsByInterval[row.intervalKey] = row }

        var features: [String: FeatureTally] = [:]
        var classifiedRuns: Set<String> = []
        for proposal in outcome.proposals {
            guard let row = rowsByInterval[proposal.intervalKey] else { continue }
            var tally = features[proposal.featureID]
                ?? FeatureTally(label: proposal.featureLabel)
            tally.rows += 1
            tally.runs.insert(runID(row))
            tally.rungs[proposal.rung.rawValue, default: 0] += 1
            if proposal.confidence > tally.confidence {
                tally.rung = proposal.rung
                tally.confidence = proposal.confidence
            }
            features[proposal.featureID] = tally
            classifiedRuns.insert(runID(row))
        }

        var unknownRuns: Set<String> = []
        for decline in outcome.declined {
            guard let row = rowsByInterval[decline.intervalKey] else { continue }
            unknownRuns.insert(runID(row))
        }
        // A run whose rows split across both answers is a classified run: the dashboard resolves
        // a Feature per interval, and calling such a run "still Unknown" would count it twice.
        unknownRuns.subtract(classifiedRuns)

        var declines: [String: Int] = [:]
        for decline in outcome.declined { declines[decline.reason.rawValue, default: 0] += 1 }

        print("classifier: \(UsageFeatureClassifier.classifierID)"
            + " v\(UsageFeatureClassifier.classifierVersion)"
            + String(format: "  acceptance threshold %.2f", threshold)
            + " (dry run: nothing is written)")
        print("ledger: \(ledgerPath)")
        print("facts:  \(factsPath) (\(facts.count) durable task records)")
        print("scope:  \(reading.acceptedProjects.count) intervals carry an accepted Project head")
        print("")
        print("Features  (rung and confidence are the strongest rung in the group;"
            + " a group holding more than one names them all)")
        let ordered = features.sorted {
            if $0.value.rows != $1.value.rows { return $0.value.rows > $1.value.rows }
            if $0.value.label != $1.value.label { return $0.value.label < $1.value.label }
            return $0.key < $1.key
        }
        for (id, tally) in ordered {
            let mixed = tally.rungs.count > 1
                ? "  (" + UsageFeatureClassifier.Rung.allCases.compactMap { rung in
                    tally.rungs[rung.rawValue].map { "\(rung.rawValue) \($0)" }
                }.joined(separator: ", ") + ")"
                : ""
            print("  " + right(tally.runs.count, 4) + " runs  " + right(tally.rows, 4) + " rows  "
                + padded(tally.rung.rawValue, 22) + String(format: "%.2f", tally.confidence)
                + "  " + id + "  " + tally.label + mixed)
        }
        print("")
        let allRuns = Set(ledgerRows.map(runID))
        print("rows classified:  \(outcome.proposals.count) of \(ledgerRows.count)")
        print("rows unknown:     \(outcome.declined.count) of \(ledgerRows.count)")
        print("runs classified:  \(classifiedRuns.count) of \(allRuns.count)")
        print("runs unknown:     \(unknownRuns.count) of \(allRuns.count)")
        print("Features:         \(features.count)")
        let accepted = outcome.proposals.filter { $0.confidence >= threshold }.count
        print("above threshold:  \(accepted) proposals would be accepted; "
            + "\(outcome.proposals.count - accepted) left for review")
        print("")
        print("declines")
        for reason in UsageFeatureClassifier.DeclineReason.allCases {
            print("  " + padded(reason.rawValue + ":", 26) + String(declines[reason.rawValue] ?? 0))
        }
        print("")
        print("rungs")
        for rung in UsageFeatureClassifier.Rung.allCases {
            let count = outcome.proposals.filter { $0.rung == rung }.count
            print("  " + padded(rung.rawValue, 22) + String(format: "%.2f", rung.confidence)
                + "  " + String(count) + " rows")
        }
    }
}
