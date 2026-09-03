import Foundation

/// One of the four things this Mac can honestly say about an assistant's account-level quota,
/// tightest to loosest. Never a percentage — see `docs/api.md`'s note on `GET
/// /v1/orchestrator/assistants` for why a computed remaining-percentage is not here: both
/// providers answer "what the account last said", not "what it says right now", and a number
/// implies a precision neither can promise.
enum Availability: String {
    case ok, low, exhausted, unknown
}

/// Where an `AssistantQuota`'s reading came from. `observed` is a file the provider itself wrote
/// (a rollout, the status line's cache) and outranks `probed` — a Mac asking the provider's own
/// identity command about itself, which answers login state but never a percentage. `selfReported`
/// is design-only in v1: a task declaring its own assistant exhausted was judged not worth
/// building — see the Q1 design's §B.4 — and nothing in this file produces it.
enum QuotaSource: String {
    case observed
    case probed
    case selfReported = "self_reported"
}

/// What this Mac can say about one assistant's account: a machine-level, per-assistant fact, not
/// a per-session one. Every account-holding session on this Mac shares the same five-hour or
/// weekly window, so this is the broker's own answer to "does Codex have anything left", asked
/// once at the dispatch gate rather than by opening a session to find out.
///
/// **What this deliberately does not have**: a remaining-percentage field computed from
/// `windows`. `availability` plus `observedAt`/age/`stale` is the honest grain available; a
/// computed percentage would look more precise than the data underneath it ever is — see the Q1
/// design's §F.1.
struct AssistantQuota {
    let assistant: Assistant
    var installed: Bool
    /// `nil` until an identity probe has run at least once for this assistant — which nothing in
    /// this build does yet; see `claude()`/`codex()`'s own note on why.
    var loggedIn: Bool?
    /// `nil` until the identity probe has run; never inferred from anywhere else.
    var plan: String?
    var availability: Availability
    var source: QuotaSource
    /// The signal's own time — Unix seconds — not when it was read. `nil` exactly when
    /// `availability == .unknown` and nothing usable has been seen.
    var observedAt: Int?
    /// The tightest live window's reset, when one is known — `windows`' own highest
    /// `usedPercent`, not the earliest `resetsAt` among them. A window that has already reset
    /// says nothing about the window now open, so `decayed(_:now:)` uses this value to decide
    /// when `exhausted` itself has aged out; pointing it at some other, unrelated window's own
    /// reset — one that already passed while the tight one is still five days out — is exactly
    /// how an account that is still exhausted gets read as merely `unknown` and dispatched to
    /// anyway. See `AssistantQuota.tightestWindowResetsAt(_:)`.
    var resetsAt: Int?
    /// One sentence a person, or an API client with no UI of its own, can print as-is.
    var detail: String
    /// Reused rather than reinvented — see `SessionInfo.Window`. Empty means nobody has said.
    var windows: [SessionInfo.Window]
    /// Whether this reading is old enough that a fresher one should be preferred once available,
    /// without yet being old enough to discard. See `AssistantQuota.decayed(_:now:)`.
    var stale = false
    /// What this was before it aged past `ok`'s `staleAfter` into `unknown`. Set only on an
    /// `unknown` produced by that decay, never on one produced by plain silence.
    var lastKnown: Availability?
}

extension AssistantQuota {

    // MARK: - §A.1: four values from a set of windows

    /// The four-value read of a set of windows, at a given moment. The rule, tightest first:
    /// any live window already spent is `exhausted`; short of that, the tightest live window's
    /// percentage against `lowThreshold` decides `low` versus `ok`; no live window at all —
    /// none present, or every one of them named a `resetsAt` that has since passed — is
    /// `unknown`, because a window whose reset has passed says nothing about the window now open.
    ///
    /// Codex's further rule — a `rate_limits` record with no named window at all, immediately
    /// after one that was nearly full, also means `exhausted` — is not here: it needs the
    /// skipped-record context only `codex()` has. See `codexCreditsDepleted(_:lastNamedUsedPercent:)`.
    static func availability(from windows: [SessionInfo.Window], now: Date = Date(),
                             lowThreshold: Double) -> Availability {
        let live = windows.filter { window in
            guard let resets = window.resetsAt else { return true }
            return Double(resets) > now.timeIntervalSince1970
        }
        guard !live.isEmpty else { return .unknown }
        if live.contains(where: \.hit) { return .exhausted }
        let tightest = live.compactMap(\.usedPercent).max() ?? 0
        if tightest >= 100 { return .exhausted }
        if tightest >= lowThreshold { return .low }
        return .ok
    }

    /// `AssistantQuota.resetsAt`'s own value: the highest-`usedPercent` window's `resetsAt`, the
    /// same notion of "tightest" `availability(from:)` uses above. Callers pass only windows
    /// already known to be live — a window whose own reset has passed is not a candidate, exactly
    /// as `availability(from:)` would not count it toward the answer either.
    static func tightestWindowResetsAt(_ windows: [SessionInfo.Window]) -> Int? {
        windows.max(by: { ($0.usedPercent ?? 0) < ($1.usedPercent ?? 0) })?.resetsAt
    }

    /// The Q1 design's §A.1 Codex-only rule. A `rate_limits` record with no named window at all
    /// does not by itself mean the account is out — a rollout with no usage yet looks exactly the
    /// same. It only means that once paired with the last window that *did* have a name, and that
    /// window was already almost full: `limit_id` no longer `"codex"`, no credits left to answer
    /// from instead, and the last real reading at 95% or more.
    static func codexCreditsDepleted(_ depleted: SessionInfo.Limits.Depleted?,
                                     lastNamedUsedPercent: Double?) -> Bool {
        // `limitID`/`hasCredits` missing entirely (an older Codex, or a renamed field) is "the
        // condition is not established", not "it is established and unfavourable" — see
        // `SessionInfo.Limits.Depleted`'s own doc comment on why both are optional.
        guard let depleted, let limitID = depleted.limitID, let hasCredits = depleted.hasCredits,
              limitID != "codex", !hasCredits,
              let lastNamedUsedPercent, lastNamedUsedPercent >= 95 else { return false }
        return true
    }

    // MARK: - §A.2: monotonic decay, not one TTL

    /// §A.2's aging window: 5% of the window's own length, clamped to 15 minutes–6 hours. A
    /// 5-hour window claps to the 15-minute floor; a 7-day window claps to the 6-hour ceiling.
    ///
    /// **A convention, not a measurement** — the one constant in this design with no observed
    /// number behind it, because how fast an account can burn through the rest of a window was
    /// not something this investigation could watch happen. Written down as such rather than
    /// dressed up as tested, so a later reading of this file does not trust it more than it has
    /// earned.
    static func staleAfter(windowMinutes: Int) -> TimeInterval {
        let raw = Double(windowMinutes) * 60 * 0.05
        return min(max(raw, 15 * 60), 6 * 3600)
    }

    private static func minutes(forWindowNamed name: String) -> Int {
        switch name {
        case "5h": return 300
        case "7d": return 10_080
        default:
            if name.hasSuffix("d"), let n = Int(name.dropLast()) { return n * 1_440 }
            if name.hasSuffix("h"), let n = Int(name.dropLast()) { return n * 60 }
            if name.hasSuffix("m"), let n = Int(name.dropLast()) { return n }
            return 10_080   // an unrecognised name defaults to the more forgiving (longer) clamp
        }
    }

    /// A quota that was true as of its own `observedAt`, aged to `now`. Because `used_percent`
    /// only rises within one `resets_at`, an old reading is a floor rather than an estimate, so
    /// each state ages differently rather than sharing one TTL — see the Q1 design's §A.2 table:
    ///
    /// - `exhausted` never expires on its own; it only leaves once `resetsAt` itself has passed,
    ///   and becomes `unknown` rather than `ok` — the quota did not become fine, a new window
    ///   with no reading of its own yet just started.
    /// - `low` keeps saying `low` however old, but is marked `stale` past `staleAfter`.
    /// - `ok` is only good until `staleAfter`; past that it becomes `unknown` with `lastKnown`
    ///   set, because an old "fine" promises nothing about the account right now.
    /// - `unknown` is already the floor and nothing here changes it.
    static func decayed(_ quota: AssistantQuota, now: Date = Date()) -> AssistantQuota {
        var quota = quota
        guard let observedAt = quota.observedAt else { return quota }
        let age = now.timeIntervalSince1970 - Double(observedAt)
        let windowMinutes = quota.windows
            .max(by: { ($0.usedPercent ?? 0) < ($1.usedPercent ?? 0) })
            .map { minutes(forWindowNamed: $0.name) } ?? 10_080
        switch quota.availability {
        case .exhausted:
            if let resets = quota.resetsAt, Double(resets) <= now.timeIntervalSince1970 {
                quota.availability = .unknown
                quota.observedAt = nil
                quota.resetsAt = nil
                quota.windows = []
                quota.stale = false
                quota.lastKnown = nil
                quota.detail = "unknown; the window that was exhausted has since reset"
            }
        case .low:
            quota.stale = age > staleAfter(windowMinutes: windowMinutes)
        case .ok:
            if age > staleAfter(windowMinutes: windowMinutes) {
                quota.lastKnown = .ok
                quota.availability = .unknown
                quota.observedAt = nil
                quota.resetsAt = nil
                quota.windows = []
                quota.stale = false
                quota.detail = "unknown; last known ok"
            }
        case .unknown:
            break
        }
        return quota
    }

    // MARK: - A sentence a client can print as-is

    static func formatDuration(seconds: Double) -> String {
        let total = max(0, Int(seconds))
        let days = total / 86_400
        let hours = (total % 86_400) / 3_600
        let minutes = (total % 3_600) / 60
        if days > 0 { return hours > 0 ? "\(days)d\(hours)h" : "\(days)d" }
        if hours > 0 { return minutes > 0 ? "\(hours)h\(minutes)m" : "\(hours)h" }
        return "\(max(minutes, 1))m"
    }

    static func detail(windows: [SessionInfo.Window], availability: Availability,
                       creditsExhausted: Bool = false, lastKnown: Availability? = nil,
                       now: Date = Date()) -> String {
        guard !windows.isEmpty else {
            if let lastKnown { return "no fresh signal; last known \(lastKnown.rawValue)" }
            return creditsExhausted ? "premium credits exhausted; no windows reported"
                                    : "no signal yet"
        }
        let parts = windows.map { window -> String in
            let pct = window.usedPercent.map { String(format: "%.0f%%", $0) } ?? "?"
            return "\(window.name) \(pct)"
        }
        var text = parts.joined(separator: ", ")
        if availability == .exhausted,
           let tight = windows.max(by: { ($0.usedPercent ?? 0) < ($1.usedPercent ?? 0) }),
           let resets = tight.resetsAt, Double(resets) > now.timeIntervalSince1970 {
            text += "; resets in " + formatDuration(seconds: Double(resets) - now.timeIntervalSince1970)
        }
        if creditsExhausted { text += "; premium credits exhausted" }
        return text
    }

    // MARK: - §B.3: identity probe — login state only, never a quota number

    struct Identity: Equatable {
        var loggedIn: Bool
        var plan: String?
    }

    /// `claude auth status`'s own stdout — Q1 design §1.6: zero tokens, about a second, and the
    /// output is already JSON:
    /// `{"loggedIn":true,"authMethod":"claude.ai",…,"subscriptionType":"max"}`.
    static func parseClaudeAuthStatus(_ text: String) -> Identity? {
        guard let data = text.data(using: .utf8),
              let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              let loggedIn = obj["loggedIn"] as? Bool else { return nil }
        return Identity(loggedIn: loggedIn, plan: obj["subscriptionType"] as? String)
    }

    /// `codex login status`'s own stdout — Q1 design §2.1: plain text, not JSON, and no plan
    /// comes back on it: `Logged in using ChatGPT`, or a sentence that says otherwise.
    ///
    /// **`hasPrefix`, not `contains`.** "Not logged in" contains the substring "logged in" too —
    /// a `contains` check reads a *refusal* as a confirmation, which is worse than reading nothing
    /// at all.
    static func parseCodexLoginStatus(_ text: String) -> Identity? {
        let plain = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !plain.isEmpty else { return nil }
        return Identity(loggedIn: plain.lowercased().hasPrefix("logged in"), plan: nil)
    }

    // The identity probe itself — what would call `parseClaudeAuthStatus`/`parseCodexLoginStatus`
    // above, cache the answer, and feed `loggedIn`/`plan` — is not wired into this version. It
    // used to live here as `refreshIdentityIfDue`/`identity(for:)`/`probeIdentity`/
    // `runViaLoginShell`, none of which anything in `Sources/` or `Tests/` ever called: `claude()`
    // and `codex()` below hard-code `loggedIn`/`plan` to `nil` instead of reading a cache nothing
    // ever filled. It is removed rather than left connected-to-nothing because `runViaLoginShell`
    // had a live deadlock waiting in it — an unread `standardError` pipe a chatty command could
    // fill past its buffer, and an interactive `zsh -i -l` that runs a full rc and can write
    // prompt escapes into the very output being parsed as JSON — and dead code with a bug in it is
    // a worse place to leave that bug than no code at all. Wiring the probe for real needs a
    // caller outside this file (`Controller.swift` or `main.swift`) and a login-shell runner that
    // reads `standardError` as it goes; see `docs/api.md`'s note on `logged_in`/`plan` staying
    // `null` until then.

    // MARK: - §C.2: the wire shape

    /// One row of `GET /v1/orchestrator/assistants` — see `docs/api.md`. Field names are exactly
    /// the design's, and `windows` is shaped the same way `SessionInfo.payload` already shapes
    /// the same type, so the two routes describe the same fact in the same words.
    ///
    /// `age_seconds` uses the identical formula the B-task's `ClaimsOverlap.warning(for:now:)`
    /// and `OrchestratorDraft.workspaceBusyExtra` already use: `max(0, Int(now - observed))`, an
    /// integer number of seconds — not this route's own idea of what "age" means.
    func payload(now: Date = Date()) -> [String: Any] {
        var out: [String: Any] = [
            "id": assistant.rawValue,
            "label": assistant.label,
            "installed": installed,
            "logged_in": loggedIn as Any? ?? NSNull(),
            "plan": plan as Any? ?? NSNull(),
            "availability": availability.rawValue,
            "source": source.rawValue,
            "observed_at": observedAt as Any? ?? NSNull(),
            "stale": stale,
            "resets_at": resetsAt as Any? ?? NSNull(),
            "detail": detail,
            "windows": windows.map { window -> [String: Any] in
                var row: [String: Any] = ["name": window.name, "hit": window.hit]
                if let used = window.usedPercent { row["usedPercent"] = used }
                if let resets = window.resetsAt { row["resetsAt"] = resets }
                return row
            },
        ]
        if let observedAt {
            out["age_seconds"] = max(0, Int(now.timeIntervalSince1970) - observedAt)
        } else {
            out["age_seconds"] = NSNull()
        }
        if let lastKnown { out["last_known"] = lastKnown.rawValue }
        return out
    }

    // MARK: - §B.1: the passive, file-only providers

    private static let cacheLock = NSLock()
    private static var cache: [Assistant: (until: Date, quota: AssistantQuota)] = [:]

    /// Both assistants, machine-level, 5-second cached — the read the dispatch gate makes on
    /// every dispatch and `GET /v1/orchestrator/assistants` answers with. Not `readingDepth`
    /// queued: see `docs/api.md`.
    static func all(now: Date = Date()) -> [AssistantQuota] {
        Assistant.allCases.map { current(for: $0, now: now) }
    }

    /// A file-only read: no subprocess, nothing that can block on a slow shell. `loggedIn`/`plan`
    /// are always `nil` — no identity probe is wired into this build; see `claude()`/`codex()`'s
    /// own note on why.
    static func current(for assistant: Assistant, now: Date = Date()) -> AssistantQuota {
        cacheLock.lock()
        if let forced = overridesForTesting[assistant] {
            cacheLock.unlock()
            return forced
        }
        if let hit = cache[assistant], hit.until > now {
            cacheLock.unlock()
            return hit.quota
        }
        cacheLock.unlock()
        let quota: AssistantQuota
        switch assistant {
        case .claude: quota = claude(now: now)
        case .codex:  quota = codex(now: now)
        }
        cacheLock.lock()
        cache[assistant] = (until: now.addingTimeInterval(5), quota: quota)
        cacheLock.unlock()
        return quota
    }

    /// Test-only: forget the 5-second cache so a test can hand a fresh directory and see it read.
    static func forgetCacheForTesting() {
        cacheLock.lock()
        cache = [:]
        cacheLock.unlock()
    }

    /// Test-only: force `current(for:)`/`all(now:)` to answer this rather than reading real
    /// machine files. The dispatch gate (`Orchestrator.dispatch`) calls `current(for:)` directly
    /// on whatever assistant a task names, with no injectable path of its own — this is that
    /// seam, the same idea as `Orchestrator.scheduleDirectoryOverrideForTesting`. `nil` removes
    /// the override for that assistant.
    private static var overridesForTesting: [Assistant: AssistantQuota] = [:]
    static func setOverrideForTesting(_ quota: AssistantQuota?, for assistant: Assistant) {
        cacheLock.lock()
        if let quota { overridesForTesting[assistant] = quota }
        else { overridesForTesting.removeValue(forKey: assistant) }
        cacheLock.unlock()
    }
    static func clearOverridesForTesting() {
        cacheLock.lock()
        overridesForTesting = [:]
        cacheLock.unlock()
    }

    /// Claude's machine-level quota. `SessionInfo.claudeLimits(cacheDirectory:now:)` is already
    /// complete for this — see the Q1 design's §B.1 — so this only shapes its answer and applies
    /// §A.1/§A.2 on top.
    static func claude(cacheDirectory: URL = ProjectStatus.cacheDirectory,
                       now: Date = Date()) -> AssistantQuota {
        let limits = SessionInfo.claudeLimits(cacheDirectory: cacheDirectory, now: now)
        let threshold = Config.shared.assistantQuotaLowThreshold
        let availability = availability(from: limits.windows, now: now, lowThreshold: threshold)
        // `loggedIn`/`plan` stay `nil`: the identity probe that would fill them in is not wired
        // into this version — see `docs/api.md`'s note on those two fields.
        var quota = AssistantQuota(
            assistant: .claude, installed: Assistant.claude.isInstalled,
            loggedIn: nil, plan: nil,
            availability: availability, source: .observed,
            observedAt: limits.at, resetsAt: tightestWindowResetsAt(limits.windows),
            detail: "", windows: limits.windows)
        quota = decayed(quota, now: now)
        if !quota.detail.isEmpty { return quota }   // decay already wrote a final sentence
        quota.detail = detail(windows: quota.windows, availability: quota.availability,
                              lastKnown: quota.lastKnown, now: now)
        return quota
    }

    /// Codex's machine-level quota: up to 5 of the most recently modified rollouts, each read
    /// with the same fixed `SessionInfo.codexLimits(rollout:)` a single session's `/info` uses —
    /// see the Q1 design's §B.1 steps 1–4, and §2.4 for why the fix has to come first.
    static func codex(sessionsRoot: URL = Codex.sessionsRoot, now: Date = Date()) -> AssistantQuota {
        let results = recentRolloutResults(sessionsRoot: sessionsRoot)
        let newestNamed = results.filter { !$0.windows.isEmpty }
            .max { ($0.at ?? 0) < ($1.at ?? 0) }
        let newestDepleted = results.compactMap(\.depleted).max { ($0.at ?? 0) < ($1.at ?? 0) }
        // A window whose own reset has passed is over — `SessionInfo.claudeLimits` already
        // drops those before anything downstream sees them; the Codex rollout read has no
        // equivalent step of its own, so it happens here instead. Without it, a record naming
        // both an expired 5h window and a live, spent 7d one carries the expired window's own
        // resetsAt into `tightestWindowResetsAt` below by the older code path, and `decayed(_:)`
        // reads that already-past reset as "the exhausted window has since reset" — silently
        // turning `exhausted` into `unknown` for an account that is still, in fact, exhausted.
        let windows = (newestNamed?.windows ?? []).filter { window in
            guard let resets = window.resetsAt else { return true }
            return Double(resets) > now.timeIntervalSince1970
        }
        let tightest = windows.compactMap(\.usedPercent).max()
        // The design's credits rule is about *the previous named window* — a relationship this
        // file can only see by timestamp once the two readings come from different rollout
        // files. Without this guard, a depleted record left over from a prior, already-reset
        // cycle can pair with an unrelated later reading that merely happens to still be in the
        // last 5 rollouts, and get read as "still depleted" for a window that in fact recovered.
        let depletedIsCurrent = (newestDepleted?.at ?? 0) >= (newestNamed?.at ?? 0)
        let creditsExhausted = depletedIsCurrent
            && codexCreditsDepleted(newestDepleted, lastNamedUsedPercent: tightest)
        let threshold = Config.shared.assistantQuotaLowThreshold
        var availability = availability(from: windows, now: now, lowThreshold: threshold)
        if creditsExhausted { availability = .exhausted }
        let observedAt: Int? = creditsExhausted
            ? max(newestDepleted?.at ?? 0, newestNamed?.at ?? 0)
            : newestNamed?.at
        // `loggedIn`/`plan` stay `nil` — same note as `claude()` above.
        var quota = AssistantQuota(
            assistant: .codex, installed: Assistant.codex.isInstalled,
            loggedIn: nil, plan: nil,
            availability: availability, source: .observed,
            observedAt: observedAt, resetsAt: tightestWindowResetsAt(windows),
            detail: "", windows: windows)
        quota = decayed(quota, now: now)
        if !quota.detail.isEmpty { return quota }
        quota.detail = detail(windows: quota.windows, availability: quota.availability,
                              creditsExhausted: creditsExhausted, lastKnown: quota.lastKnown, now: now)
        return quota
    }

    /// Bounds how far down the newest-first list `recentRolloutResults(sessionsRoot:)` will look
    /// before giving up — a fixed ceiling rather than an unbounded scan, so a night with dozens
    /// of tabs opening and dying still costs one bounded pass rather than growing with them.
    private static let maxRolloutFilesScanned = 40

    /// Up to 5 rollouts that actually named a window or a depleted bucket — newest modification
    /// time first, but **skipping past** any that did not, up to `maxRolloutFilesScanned`. The
    /// night this mattered: quota running out opens a burst of tabs that die within seconds, each
    /// leaving behind a rollout with a newer mtime than everything real but not one `rate_limits`
    /// record in it. Taking literally "the 5 most recently modified files" — the Q1 design's own
    /// words — let five dead tabs push the one file that actually said anything out of the
    /// window, answering `unknown` and dispatching right into the same failure. Sorting on mtime
    /// stays right (unlike `Codex.rollouts(days:root:)`'s day-folder order, it is the stamp that
    /// actually says which file was touched last); what changed is that a file with nothing to
    /// say no longer counts as one of the 5.
    private static func recentRolloutResults(sessionsRoot: URL) -> [SessionInfo.Limits] {
        let fm = FileManager.default
        let candidates = Codex.rollouts(days: 2, root: sessionsRoot)
        let byRecency = candidates
            .map { url -> (URL, Date) in
                let mtime = (try? fm.attributesOfItem(atPath: url.path))?[.modificationDate] as? Date
                return (url, mtime ?? .distantPast)
            }
            .sorted { $0.1 > $1.1 }
            .map(\.0)
        var picked: [SessionInfo.Limits] = []
        for url in byRecency.prefix(maxRolloutFilesScanned) {
            guard let tail = Transcript.tail(of: url, bytes: 256 * 1_024) else { continue }
            let limits = SessionInfo.codexLimits(rollout: Data(tail.utf8))
            guard !limits.windows.isEmpty || limits.depleted != nil else { continue }
            picked.append(limits)
            if picked.count == 5 { break }
        }
        return picked
    }
}
