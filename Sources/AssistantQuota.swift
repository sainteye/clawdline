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
    /// `nil` until the identity probe (`refreshIdentityIfDue`) has run at least once for this
    /// assistant.
    var loggedIn: Bool?
    /// `nil` until the identity probe has run; never inferred from anywhere else.
    var plan: String?
    var availability: Availability
    var source: QuotaSource
    /// The signal's own time — Unix seconds — not when it was read. `nil` exactly when
    /// `availability == .unknown` and nothing usable has been seen.
    var observedAt: Int?
    /// The tightest live window's reset, when one is known.
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

    /// The Q1 design's §A.1 Codex-only rule. A `rate_limits` record with no named window at all
    /// does not by itself mean the account is out — a rollout with no usage yet looks exactly the
    /// same. It only means that once paired with the last window that *did* have a name, and that
    /// window was already almost full: `limit_id` no longer `"codex"`, no credits left to answer
    /// from instead, and the last real reading at 95% or more.
    static func codexCreditsDepleted(_ depleted: SessionInfo.Limits.Depleted?,
                                     lastNamedUsedPercent: Double?) -> Bool {
        guard let depleted, depleted.limitID != "codex", !depleted.hasCredits,
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

    private static let identityLock = NSLock()
    private static var identityCache: [Assistant: Identity] = [:]
    private static var identityCheckedAt: Date?
    private static let identityInterval: TimeInterval = 3_600

    /// Runs the identity probe — the Q1 design's §B.3 — at most once an hour if it is called
    /// repeatedly; the interval guard is in this function, not in a caller.
    ///
    /// **Nothing in this task's declared scope calls this yet.** It shells out (`claude auth
    /// status` / `codex login status`, each up to a 5-second timeout — see
    /// `probeIdentity(_:)`), so it deliberately is *not* wired into `current(for:now:)`: the Q1
    /// design's own requirement is that a dispatch gate stay a synchronous, file-only read, and
    /// "the first call after launch" would have meant the first `dispatch()` after every restart,
    /// and after every quiet hour, blocking on a live subprocess right at the gate the whole
    /// point of this task was to make cheap. Wiring "once at startup, then hourly" for real needs
    /// one call to this from `Controller.swift` or `main.swift`, both outside this task's claimed
    /// paths — until then, `loggedIn`/`plan` stay `nil` and every `unknown` reads as `unknown`
    /// rather than being split into "not logged in" / "no signal yet". See `CHANGES.md`.
    static func refreshIdentityIfDue(now: Date = Date()) {
        identityLock.lock()
        let due = identityCheckedAt.map { now.timeIntervalSince($0) >= identityInterval } ?? true
        guard due else { identityLock.unlock(); return }
        identityCheckedAt = now
        identityLock.unlock()
        for assistant in Assistant.allCases where assistant.isInstalled {
            guard let identity = probeIdentity(assistant) else { continue }
            identityLock.lock()
            identityCache[assistant] = identity
            identityLock.unlock()
        }
    }

    static func identity(for assistant: Assistant) -> Identity? {
        identityLock.lock()
        defer { identityLock.unlock() }
        return identityCache[assistant]
    }

    /// Test-only: put the clock back so the next `refreshIdentityIfDue` runs unconditionally.
    static func forgetIdentityScheduleForTesting() {
        identityLock.lock()
        identityCheckedAt = nil
        identityCache = [:]
        identityLock.unlock()
    }

    private static func probeIdentity(_ assistant: Assistant) -> Identity? {
        let command = assistant == .claude ? "claude auth status" : "codex login status"
        guard let output = runViaLoginShell(command, timeout: 5) else { return nil }
        return assistant == .claude ? parseClaudeAuthStatus(output) : parseCodexLoginStatus(output)
    }

    /// The user's own login shell, because `claude`/`codex` are typically found the same way
    /// `npm` is (Q1 design's own citation of `DevStack.swift`'s `shell(_:cwd:timeout:)`): an app
    /// launched from Finder inherits no terminal `PATH` at all.
    private static func runViaLoginShell(_ command: String, timeout: TimeInterval) -> String? {
        let task = Process()
        let shellPath = ProcessInfo.processInfo.environment["SHELL"] ?? "/bin/zsh"
        task.executableURL = URL(fileURLWithPath:
            FileManager.default.isExecutableFile(atPath: shellPath) ? shellPath : "/bin/zsh")
        task.arguments = ["-i", "-l", "-c", command]
        let pipe = Pipe()
        task.standardOutput = pipe
        task.standardError = Pipe()
        do { try task.run() } catch { return nil }
        let killer = DispatchWorkItem { if task.isRunning { task.terminate() } }
        DispatchQueue.global().asyncAfter(deadline: .now() + timeout, execute: killer)
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        task.waitQuietly()
        killer.cancel()
        guard task.terminationStatus == 0 else { return nil }
        return String(data: data, encoding: .utf8)
    }

    // MARK: - §C.2: the wire shape

    /// One row of `GET /v1/orchestrator/assistants` — see `docs/api.md`. Field names are exactly
    /// the design's, and `windows` is shaped the same way `SessionInfo.payload` already shapes
    /// the same type, so the two routes describe the same fact in the same words.
    ///
    /// `age_seconds` uses the identical formula the B-task's `ClaimsOverlap.warning(for:now:)`
    /// and `Orchestrator.workspaceBusyExtra` already use: `max(0, Int(now - observed))`, an
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
    /// come from whatever `refreshIdentityIfDue` last cached, which is `nil` on both until
    /// something calls it — see that function's own note on why this deliberately does not call
    /// it for you.
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
        let ident = identity(for: .claude)
        let threshold = Config.shared.assistantQuotaLowThreshold
        let availability = availability(from: limits.windows, now: now, lowThreshold: threshold)
        var quota = AssistantQuota(
            assistant: .claude, installed: Assistant.claude.isInstalled,
            loggedIn: ident?.loggedIn, plan: ident?.plan,
            availability: availability, source: .observed,
            observedAt: limits.at, resetsAt: limits.windows.compactMap(\.resetsAt).min(),
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
        let files = recentRolloutFiles(sessionsRoot: sessionsRoot)
        let results: [SessionInfo.Limits] = files.compactMap { url in
            guard let tail = Transcript.tail(of: url, bytes: 256 * 1_024) else { return nil }
            return SessionInfo.codexLimits(rollout: Data(tail.utf8))
        }
        let newestNamed = results.filter { !$0.windows.isEmpty }
            .max { ($0.at ?? 0) < ($1.at ?? 0) }
        let newestDepleted = results.compactMap(\.depleted).max { ($0.at ?? 0) < ($1.at ?? 0) }
        let windows = newestNamed?.windows ?? []
        let tightest = windows.compactMap(\.usedPercent).max()
        let creditsExhausted = codexCreditsDepleted(newestDepleted, lastNamedUsedPercent: tightest)
        let threshold = Config.shared.assistantQuotaLowThreshold
        var availability = availability(from: windows, now: now, lowThreshold: threshold)
        if creditsExhausted { availability = .exhausted }
        let observedAt: Int? = creditsExhausted
            ? max(newestDepleted?.at ?? 0, newestNamed?.at ?? 0)
            : newestNamed?.at
        let ident = identity(for: .codex)
        var quota = AssistantQuota(
            assistant: .codex, installed: Assistant.codex.isInstalled,
            loggedIn: ident?.loggedIn, plan: ident?.plan,
            availability: availability, source: .observed,
            observedAt: observedAt, resetsAt: windows.compactMap(\.resetsAt).min(),
            detail: "", windows: windows)
        quota = decayed(quota, now: now)
        if !quota.detail.isEmpty { return quota }
        quota.detail = detail(windows: quota.windows, availability: quota.availability,
                              creditsExhausted: creditsExhausted, lastKnown: quota.lastKnown, now: now)
        return quota
    }

    /// Up to 5 rollouts, newest modification time first — the Q1 design's §B.1: cheap enough for
    /// the dispatch gate to read synchronously, and, unlike `Codex.rollouts(days:root:)`'s
    /// day-folder order, sorted by the stamp that actually says which one was touched last.
    private static func recentRolloutFiles(sessionsRoot: URL) -> [URL] {
        let fm = FileManager.default
        let candidates = Codex.rollouts(days: 2, root: sessionsRoot)
        let dated = candidates.map { url -> (URL, Date) in
            let mtime = (try? fm.attributesOfItem(atPath: url.path))?[.modificationDate] as? Date
            return (url, mtime ?? .distantPast)
        }
        return dated.sorted { $0.1 > $1.1 }.prefix(5).map(\.0)
    }
}
