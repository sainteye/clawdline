import CryptoKit
import Foundation

/// The local, deterministic producer the usage attribution ledger was missing.
///
/// **Self-contained on purpose.** It imports only Foundation and CryptoKit and names no other
/// type in this repository, so `tools/usage-feature-backfill.swift` can compile it on its own
/// against a copy of a ledger. The translation into `UsageLedger.AttributionEvent` — and every
/// decision about *writing* anything — lives in `Sources/UsageFeatureAttribution.swift`.
///
/// **It is a pure batch function.** Grouping is inherently a batch property: one interval cannot
/// know whether the label it carries names a durable work line or a one-off, so `classify` takes
/// the whole bounded evidence set and returns the whole outcome. No I/O, no clock read, no
/// global, and no prompt, instruction body, transcript, working directory or file path anywhere
/// in `Evidence` — the ledger keeps a digest of the evidence, never the evidence.
enum UsageFeatureClassifier {
    static let classifierID = "clawdline-local-feature-merger"
    /// Classifier/prompt version. Any change to a rung, a confidence, a normalization rule or the
    /// digest recipe increments it, because event ids are derived from it and a re-run under the
    /// same version must be a no-op rather than a second opinion.
    static let classifierVersion = "1"
    static let defaultAcceptanceThreshold = 0.80

    /// The minimum safe metadata `docs/usage-attribution.md` permits.
    struct Evidence: Equatable {
        var intervalKey: String
        /// The Project this row belongs to, **already resolved by ``resolveProject(projectKey:
        /// acceptedProjectIdentity:)``** — never the raw `project_key` column. Feature identity
        /// is scoped to one Project, so two Projects that happen to use the same label stay two
        /// Features, and a row the Portfolio refuses to name a Project for arrives here as `nil`
        /// rather than as a Project of its own.
        var projectKey: String?
        /// Nil for a Session-boundary row, which is the first decline reason below.
        var taskID: String?
        /// Whether a durable broker record for `taskID` was found. Evidence a task record cannot
        /// confirm is not evidence this classifier proposes from.
        var hasDurableTaskRecord: Bool
        var taskKind: String?
        var taskTitle: String?
        /// The root-declared label — `root.label` in task.json — which is what makes several
        /// tasks one work line.
        var declaredWorkLine: String?
        /// First line of the durable record's plan, trimmed. Never the whole plan.
        var planHeadline: String?
        var scheduleID: String?
        var scheduleTitle: String?
        var parentTaskID: String?
        var retryOf: String?

        init(intervalKey: String, projectKey: String? = nil, taskID: String? = nil,
             hasDurableTaskRecord: Bool = false, taskKind: String? = nil,
             taskTitle: String? = nil, declaredWorkLine: String? = nil,
             planHeadline: String? = nil, scheduleID: String? = nil, scheduleTitle: String? = nil,
             parentTaskID: String? = nil, retryOf: String? = nil) {
            self.intervalKey = intervalKey
            self.projectKey = projectKey
            self.taskID = taskID
            self.hasDurableTaskRecord = hasDurableTaskRecord
            self.taskKind = taskKind
            self.taskTitle = taskTitle
            self.declaredWorkLine = declaredWorkLine
            self.planHeadline = planHeadline
            self.scheduleID = scheduleID
            self.scheduleTitle = scheduleTitle
            self.parentTaskID = parentTaskID
            self.retryOf = retryOf
        }
    }

    /// The ladder, in the order it is climbed. First match wins.
    enum Rung: String, CaseIterable {
        case explicitFeatureHint = "explicit_feature_hint"
        case scheduleIdentity = "schedule_identity"
        case declaredWorkLine = "declared_work_line"
        case lineage = "lineage"

        var confidence: Double {
            switch self {
            case .explicitFeatureHint: return 0.95
            case .scheduleIdentity: return 0.88
            case .declaredWorkLine: return 0.82
            case .lineage: return 0.66
            }
        }
    }

    /// Why the classifier declined. These are the classifier's own words: they are reported by a
    /// run receipt and are **not** written into the ledger, and they are **not** the Portfolio's
    /// `no_unambiguous_accepted_head`, which is a different statement about a different thing —
    /// that one is about what the ledger holds, these are about what this pass could see.
    enum DeclineReason: String, CaseIterable {
        case noTaskIdentity = "no_task_identity"
        case noDurableTaskRecord = "no_durable_task_record"
        case solitaryDeclaredLabel = "solitary_declared_label"
        case noGroupingEvidence = "no_grouping_evidence"
    }

    struct Proposal: Equatable {
        var intervalKey: String
        var featureID: String
        var featureLabel: String
        var rung: Rung
        var confidence: Double
        /// 64 lowercase hex characters, over exactly the fields the rung consumed. A reader can
        /// tell two decisions apart without the evidence itself being stored.
        var evidenceDigest: String
        var proposalEventID: String
        var acceptanceEventID: String
    }

    struct Declined: Equatable {
        var intervalKey: String
        var reason: DeclineReason
    }

    struct Outcome: Equatable {
        var proposals: [Proposal]
        var declined: [Declined]
    }

    /// The Project scope every Feature id is computed inside. A row with no Project a surface may
    /// name is not merged with rows that have one: the sentinel is a scope of its own.
    static let unknownProjectScope = "\u{0}unknown-project"

    private static let unitSeparator = "\u{1f}"

    /// Why a row has no Project a surface may name. The Portfolio reports these words on its
    /// Projects table; the Feature table reports nothing and simply scopes such a row to
    /// ``unknownProjectScope``. What matters is that both reach the answer by one rule.
    enum ProjectScopeRefusal: String, CaseIterable {
        case missingProjectKey = "project_key_missing"
        case legacyManagedWorktree = "legacy_managed_worktree_project_key"
    }

    struct ProjectResolution: Equatable {
        /// The Project identity, or nil when there is no Project a surface may name.
        var identity: String?
        var refusal: ProjectScopeRefusal?
    }

    /// A Clawdline-managed worktree path ending in a task UUID. Its basename is a disposable task
    /// id and not a repository, so turning it into a Project is precisely the inference both
    /// surfaces refuse.
    static func legacyManagedWorktreeTaskID(_ raw: String) -> String? {
        let path = URL(fileURLWithPath: raw).standardizedFileURL.path
        guard path.contains("/Clawdline/worktrees/"),
              let leaf = path.split(separator: "/").last,
              UUID(uuidString: String(leaf)) != nil else { return nil }
        return String(leaf)
    }

    /// **The one rule that says which Project a row belongs to.** An accepted Project head first,
    /// then the canonical stored key, and a legacy managed-worktree key resolves to no Project at
    /// all rather than to one of its own.
    ///
    /// The Portfolio's `projectIdentity` and the Feature scope call this and nothing else. When
    /// they were two copies of the rule they disagreed about the same row, and on real data on
    /// 2026-09-03 that split three work lines into six Features: the Projects table suppressed a
    /// managed-worktree path to `Unknown Project` while the Feature table baked that same path's
    /// hash into two Feature ids.
    static func resolveProject(projectKey: String?,
                               acceptedProjectIdentity: String? = nil) -> ProjectResolution {
        if let accepted = acceptedProjectIdentity?.trimmingCharacters(in: .whitespacesAndNewlines),
           !accepted.isEmpty {
            return ProjectResolution(identity: accepted, refusal: nil)
        }
        guard let raw = projectKey?.trimmingCharacters(in: .whitespacesAndNewlines),
              !raw.isEmpty else {
            return ProjectResolution(identity: nil, refusal: .missingProjectKey)
        }
        let path = URL(fileURLWithPath: raw).standardizedFileURL.path
        if legacyManagedWorktreeTaskID(path) != nil {
            return ProjectResolution(identity: nil, refusal: .legacyManagedWorktree)
        }
        return ProjectResolution(identity: path, refusal: nil)
    }

    /// `s` trimmed, internal runs of whitespace collapsed to one space, lowercased.
    static func normalizedKey(_ value: String) -> String {
        value.split(whereSeparator: { $0.isWhitespace })
            .joined(separator: " ")
            .lowercased()
    }

    /// The scope string a Feature id is computed inside, from the identity ``resolveProject``
    /// already returned. It deliberately does not standardize a path of its own: an accepted
    /// Project head's identity is whatever a person accepted, and putting a non-path identity
    /// through `URL(fileURLWithPath:)` would resolve it against the current directory.
    static func projectScope(_ projectIdentity: String?) -> String {
        guard let identity = projectIdentity?.trimmingCharacters(in: .whitespacesAndNewlines),
              !identity.isEmpty else { return unknownProjectScope }
        return identity
    }

    /// `feature-` plus the first 32 hex characters of the scoped grouping digest.
    static func featureID(projectScope: String, rung: Rung, groupingKey: String) -> String {
        "feature-" + String(hex([projectScope, rung.rawValue, normalizedKey(groupingKey)])
            .prefix(32))
    }

    static func evidenceDigest(rung: Rung, projectScope: String, intervalKey: String,
                               taskID: String?, groupingKey: String) -> String {
        hex(["usage-feature-classifier/" + classifierVersion, rung.rawValue, projectScope,
             intervalKey, taskID ?? "", groupingKey])
    }

    /// Deterministic event ids are what make a second pass a no-op: `UsageLedger.record(_:)`
    /// returns false on a duplicate id and never replaces the first event.
    static func eventIDSeed(intervalKey: String, featureID: String, rung: Rung) -> String {
        String(hex([classifierVersion, intervalKey, featureID, rung.rawValue]).prefix(40))
    }

    private static func hex(_ fields: [String]) -> String {
        SHA256.hash(data: Data(fields.joined(separator: unitSeparator).utf8))
            .map { String(format: "%02x", $0) }.joined()
    }

    /// `^\s*Feature\s*[:：]\s*(.+)$`, with one leading opening quote taken off the captured hint —
    /// the plan headlines this fires on are written `Feature: "…"`, and the quote is punctuation
    /// around the name rather than part of it.
    private static let hintExpression = try? NSRegularExpression(
        pattern: "^\\s*Feature\\s*[:：]\\s*(.+)$")

    /// The closing quote that belongs to each opening one. Taking the opening quote off and
    /// leaving its partner behind produced `Ledger receipts” for the whole range` on a real plan
    /// headline: half a quotation, which reads on the dashboard as a typo rather than as a name.
    private static let quotePairs: [Character: Character] = [
        "\"": "\"", "\u{201c}": "\u{201d}", "\u{300c}": "\u{300d}",
    ]

    static func explicitFeatureHint(in source: String?) -> String? {
        guard let expression = hintExpression,
              let source = source?.trimmingCharacters(in: .whitespacesAndNewlines),
              !source.isEmpty else { return nil }
        let text = source as NSString
        guard let match = expression.firstMatch(
            in: source, range: NSRange(location: 0, length: text.length)),
            match.numberOfRanges > 1 else { return nil }
        var hint = text.substring(with: match.range(at: 1))
            .trimmingCharacters(in: .whitespacesAndNewlines)
        if let first = hint.first, let closing = quotePairs[first] {
            hint = String(hint.dropFirst())
            // Only the partner of the quote that was actually removed, and only the first one:
            // a name that goes on after the quotation keeps its own words.
            if let closingIndex = hint.firstIndex(of: closing) { hint.remove(at: closingIndex) }
            hint = hint.trimmingCharacters(in: .whitespacesAndNewlines)
        }
        return hint.isEmpty ? nil : hint
    }

    /// What `UsageLedger.valid(_:)` will accept as a `valueLabel`: at most 120 characters, no
    /// control character, not empty. A label it would refuse is an event that vanishes silently,
    /// so the rung that cannot produce one declines instead of proposing a doomed event.
    static func displayLabel(_ raw: String) -> String? {
        var scalars = String.UnicodeScalarView()
        for scalar in raw.unicodeScalars {
            scalars.append(CharacterSet.controlCharacters.contains(scalar) ? " " : scalar)
        }
        let label = String(String(scalars).prefix(120))
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return label.isEmpty ? nil : label
    }

    /// One rung's answer for one interval, before it becomes a `Proposal`.
    private struct Match {
        var rung: Rung
        var featureID: String
        var featureLabel: String
        var groupingKey: String
    }

    /// Classify a whole bounded evidence set.
    ///
    /// Every interval leaves exactly once, as a proposal or as a decline, and the two arrays keep
    /// the input's order so a receipt reads the same way twice.
    static func classify(_ evidence: [Evidence]) -> Outcome {
        // Rung 3's index. "Two or more distinct task ids" is the whole point of the batch shape:
        // a label one task carries is a one-off, a label two tasks carry is a work line.
        var workLineTasks: [String: Set<String>] = [:]
        for item in evidence {
            guard let taskID = trimmed(item.taskID), item.hasDurableTaskRecord,
                  let label = trimmed(item.declaredWorkLine) else { continue }
            workLineTasks[
                projectScope(item.projectKey) + unitSeparator + normalizedKey(label),
                default: []
            ].insert(taskID)
        }

        var matches: [String: Match] = [:]      // interval key -> rungs 1-3
        var declined: [String: DeclineReason] = [:]
        // What rungs 1-3 decided for each task, so rung 4 can inherit from it. Order-independent
        // on purpose: a task whose rows classify differently must answer the same way whatever
        // order the batch arrived in, so the strongest rung wins and the id breaks the tie.
        var byTask: [String: Match] = [:]

        for item in evidence {
            guard let taskID = trimmed(item.taskID) else {
                declined[item.intervalKey] = .noTaskIdentity
                continue
            }
            guard item.hasDurableTaskRecord else {
                declined[item.intervalKey] = .noDurableTaskRecord
                continue
            }
            let scope = projectScope(item.projectKey)
            var match: Match?
            // The plan headline first and the task title after it — both of them, not whichever
            // exists. A record whose headline says something other than `Feature:` still has a
            // title that may say it, and skipping that costs matches without ever adding a wrong
            // one, which is what §3.1's "(else `taskTitle`)" reads.
            if let hint = explicitFeatureHint(in: item.planHeadline)
                ?? explicitFeatureHint(in: item.taskTitle),
               let label = displayLabel(hint) {
                match = Match(rung: .explicitFeatureHint,
                              featureID: featureID(projectScope: scope,
                                                   rung: .explicitFeatureHint, groupingKey: hint),
                              featureLabel: label, groupingKey: hint)
            } else if let scheduleID = trimmed(item.scheduleID),
                      let label = displayLabel(trimmed(item.scheduleTitle) ?? scheduleID) {
                match = Match(rung: .scheduleIdentity,
                              featureID: featureID(projectScope: scope, rung: .scheduleIdentity,
                                                   groupingKey: scheduleID),
                              featureLabel: label, groupingKey: scheduleID)
            } else if let line = trimmed(item.declaredWorkLine),
                      (workLineTasks[scope + unitSeparator + normalizedKey(line)]?.count ?? 0) >= 2,
                      let label = displayLabel(line) {
                match = Match(rung: .declaredWorkLine,
                              featureID: featureID(projectScope: scope, rung: .declaredWorkLine,
                                                   groupingKey: line),
                              featureLabel: label, groupingKey: line)
            }
            guard let found = match else {
                let reason: DeclineReason = trimmed(item.declaredWorkLine) == nil
                    ? .noGroupingEvidence : .solitaryDeclaredLabel
                declined[item.intervalKey] = reason
                continue
            }
            matches[item.intervalKey] = found
            if let previous = byTask[taskID] {
                if found.rung.confidence > previous.rung.confidence
                    || (found.rung.confidence == previous.rung.confidence
                        && found.featureID < previous.featureID) {
                    byTask[taskID] = found
                }
            } else {
                byTask[taskID] = found
            }
        }

        // Rung 4, one hop only. A row inherits its parent's Feature verbatim rather than
        // inventing one, and an inheriting row never becomes a parent itself — chains would make
        // a Feature's membership depend on the order the batch happened to arrive in.
        var inherited: [String: Match] = [:]
        for item in evidence {
            guard matches[item.intervalKey] == nil, item.hasDurableTaskRecord,
                  let taskID = trimmed(item.taskID),
                  let parent = trimmed(item.parentTaskID) ?? trimmed(item.retryOf),
                  parent != taskID, let ancestor = byTask[parent] else { continue }
            inherited[item.intervalKey] = Match(rung: .lineage, featureID: ancestor.featureID,
                                                featureLabel: ancestor.featureLabel,
                                                groupingKey: parent)
        }

        // One Feature, one label, whatever order the batch arrived in. Two spellings of one work
        // line — `Attribution ladder` and `attribution   LADDER` — normalize to a single id and a
        // single event id, so `record(_:)` keeps whichever spelling was written first and the
        // other can never correct it. The lexicographically smallest spelling wins: an answer
        // that is a property of the group rather than of arrival order.
        var labelByFeature: [String: String] = [:]
        for match in matches.values {
            if let chosen = labelByFeature[match.featureID], chosen <= match.featureLabel {
                continue
            }
            labelByFeature[match.featureID] = match.featureLabel
        }

        var proposals: [Proposal] = []
        var declines: [Declined] = []
        for item in evidence {
            guard let match = matches[item.intervalKey] ?? inherited[item.intervalKey] else {
                guard let reason = declined[item.intervalKey] else { continue }
                declines.append(Declined(intervalKey: item.intervalKey, reason: reason))
                continue
            }
            let seed = eventIDSeed(intervalKey: item.intervalKey, featureID: match.featureID,
                                   rung: match.rung)
            proposals.append(Proposal(
                intervalKey: item.intervalKey, featureID: match.featureID,
                featureLabel: labelByFeature[match.featureID] ?? match.featureLabel,
                rung: match.rung,
                confidence: match.rung.confidence,
                evidenceDigest: evidenceDigest(rung: match.rung,
                                               projectScope: projectScope(item.projectKey),
                                               intervalKey: item.intervalKey,
                                               taskID: trimmed(item.taskID),
                                               groupingKey: match.groupingKey),
                proposalEventID: "feature-proposal-v1-" + seed,
                acceptanceEventID: "feature-accepted-v1-" + seed))
        }
        return Outcome(proposals: proposals, declined: declines)
    }

    private static func trimmed(_ value: String?) -> String? {
        guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines),
              !value.isEmpty else { return nil }
        return value
    }
}
