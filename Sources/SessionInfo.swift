import Foundation

/// The facts behind a session's compact web status line and its expanded card — what it has
/// spent, what is left of the plan's window, how much has changed on disk, and whether the last
/// deploy went out. It is what the Claude Code status line draws at the bottom of a terminal,
/// for somebody who is not looking at that terminal.
///
/// The parsing lives up here and the process that obtains the text lives at the bottom, apart,
/// for the reason `Targets.swift` gives: the parsing is where the bugs are, and a test can hand
/// it a string. Nothing in this file writes anywhere.
enum SessionInfo {

    // MARK: - Files

    /// What `git status --porcelain=v2 --branch` says, **counted rather than listed**. Counts,
    /// because "something is uncommitted" and "eleven files are uncommitted" are different facts
    /// and only the second one decides anything; not files, because the page has a place for
    /// those already and a card is not a list.
    struct Files: Equatable {
        var branch = ""
        var head = ""
        var ahead = 0
        var behind = 0
        var staged = 0
        var unstaged = 0
        var untracked = 0
        var conflict = 0
    }

    /// Parse the porcelain. One file can be both staged and unstaged (a partial add) and then
    /// counts under both, which is how `git status` lists it too.
    static func parseStatus(_ text: String) -> Files {
        var out = Files()
        for raw in text.split(separator: "\n", omittingEmptySubsequences: true) {
            let line = String(raw)
            if line.hasPrefix("# branch.oid ") {
                let oid = line.dropFirst("# branch.oid ".count).trimmingCharacters(in: .whitespaces)
                out.head = oid == "(initial)" ? "" : oid
            } else if line.hasPrefix("# branch.head ") {
                let head = line.dropFirst("# branch.head ".count).trimmingCharacters(in: .whitespaces)
                out.branch = head == "(detached)" ? "" : head
            } else if line.hasPrefix("# branch.ab ") {
                for token in line.dropFirst("# branch.ab ".count).split(separator: " ") {
                    if token.hasPrefix("+") { out.ahead = Int(token.dropFirst()) ?? 0 }
                    else if token.hasPrefix("-") { out.behind = Int(token.dropFirst()) ?? 0 }
                }
            } else if line.hasPrefix("1 ") || line.hasPrefix("2 ") {
                // `1 XY sub mH mI mW hH hI path` — the second field is the two-letter state.
                let fields = line.split(separator: " ", maxSplits: 2, omittingEmptySubsequences: false)
                guard fields.count > 1, fields[1].count == 2 else { continue }
                let xy = Array(fields[1])
                if xy[0] != "." { out.staged += 1 }
                if xy[1] != "." { out.unstaged += 1 }
            } else if line.hasPrefix("? ") {
                out.untracked += 1
            } else if line.hasPrefix("u ") {
                out.conflict += 1
            }
        }
        return out
    }

    // MARK: - Limits

    /// One window of a plan's allowance — the five hours, or the seven days.
    struct Window: Equatable {
        /// `5h`, `7d`: the names the status line uses, so the two read the same in both places.
        var name: String
        /// 0…100. Absent when nobody said, which is not the same as zero.
        var usedPercent: Double?
        /// Unix seconds.
        var resetsAt: Int?
        /// The window is spent: the provider refused the last request that was made on it.
        var hit = false
    }

    /// What is known about the plan, and when it was known. An empty `windows` is "nobody said",
    /// and the page draws that as *unknown* rather than as 0% — a full window shown as an empty
    /// one is the one wrong answer that changes what somebody does next.
    struct Limits: Equatable {
        var windows: [Window] = []
        /// When the record this came from was written. Unix seconds.
        var at: Int?
    }

    /// A window's name from its length. Codex says `window_minutes`; Claude names the type.
    static func windowName(minutes: Int) -> String {
        if minutes == 300 { return "5h" }
        if minutes == 10080 { return "7d" }
        if minutes % 1440 == 0 { return "\(minutes / 1440)d" }
        if minutes % 60 == 0 { return "\(minutes / 60)h" }
        return "\(minutes)m"
    }

    static func windowName(claudeType: String) -> String {
        switch claudeType {
        case "five_hour": return "5h"
        case "seven_day": return "7d"
        default: return claudeType
        }
    }

    /// The plan's state as a Claude transcript records it, and the model the session is on.
    ///
    /// **Claude Code writes no running percentage into a transcript.** The status line is handed
    /// `rate_limits.five_hour.used_percentage` on its stdin, and that is the only place it goes;
    /// what does reach the file is a `quotaLimits` block on the turn where a window ran out —
    /// `{"status":"rejected","resetsAt":…,"rateLimitType":"five_hour"}`. So the honest answer is
    /// three-valued: a window that is spent and when it comes back, a window that has since come
    /// back (the record is older than its own reset, so it says nothing about now), or nothing.
    /// If a build starts writing the status line's shape into the file, the second branch reads
    /// it, and the card gets percentages without anybody touching this.
    ///
    /// Read from the end, because the last record is the whole answer and these files reach
    /// fifty megabytes; the model comes from the same pass for the same reason. The model of the
    /// refusal itself is `<synthetic>`, which is not a model and is skipped.
    ///
    /// **The model is not only the assistant rows' to say.** A `/model` is written down as a user
    /// row, and the line it printed as the one after it; neither is a turn. So between a switch
    /// and the reply that proves it — which is a whole session, for one that has been opened and
    /// switched and not yet asked anything — the file's last word on the model is the one the
    /// session has just left, and the card answers *unknown* or, worse, confidently wrong.
    /// Reading from the end settles which of the two wins without a rule about it: whichever
    /// comes first is the newest thing anybody said. The printed line is met *before* the command
    /// that caused it, so it is held until that command turns up and `Set model to …` is only
    /// believed where a `/model` actually asked for it.
    static func claudeLimits(transcript data: Data, now: Date = Date()) -> (limits: Limits, model: String?) {
        var limits: Limits?
        var model: String?
        var printed: String?      // a `Set model to …` seen on the way past, not yet accounted for
        for line in data.split(separator: 0x0A).reversed() {
            if limits != nil, model != nil { break }
            let text = String(decoding: line, as: UTF8.self)
            let wantsLimit = limits == nil && (text.contains("quotaLimits") || text.contains("rate_limits"))
            // Three spellings rather than one, because the rows a switch leaves behind put the
            // word in prose inside a tag rather than in a key. Still spelled out rather than a
            // bare `contains("model")`: what follows a match is a JSON parse of the whole line,
            // and a line can be a hundred kilobytes of tool result that merely mentions models.
            let wantsModel = model == nil && (text.contains("\"model\"")
                || text.contains("/model") || text.contains("Set model to"))
            guard wantsLimit || wantsModel,
                  let obj = (try? JSONSerialization.jsonObject(with: Data(line))) as? [String: Any]
            else { continue }
            if wantsModel, obj["type"] as? String == "assistant",
               let message = obj["message"] as? [String: Any],
               let named = message["model"] as? String, !named.hasPrefix("<"), !named.isEmpty {
                model = named
            }
            if wantsModel, model == nil, obj["type"] as? String == "user", let said = userText(obj) {
                if let word = modelSwitch(inRow: said) {
                    model = claudeModelID(word: word, printed: printed)
                } else if let name = modelPrinted(inRow: said) {
                    printed = name
                }
            }
            if wantsLimit {
                if let quota = obj["quotaLimits"] as? [String: Any] {
                    limits = fromQuota(quota, at: stamp(obj["timestamp"]), now: now)
                } else if let rates = obj["rate_limits"] as? [String: Any]
                    ?? (obj["message"] as? [String: Any])?["rate_limits"] as? [String: Any] {
                    limits = fromRates(rates, at: stamp(obj["timestamp"]))
                }
            }
        }
        return (limits ?? Limits(), model)
    }

    // MARK: - What a `/model` says

    /// The word a `/model` row asked for — `opus` — or nil for any row that is not one.
    ///
    /// The empty string is its own answer: `/model` typed with nothing after it opens a picker,
    /// so the row records that a switch happened without recording what to, and the line it
    /// printed becomes the only place the choice appears.
    static func modelSwitch(inRow text: String) -> String? {
        guard let name = between("<command-name>", "</command-name>", in: text)?
            .trimmingCharacters(in: .whitespacesAndNewlines) else { return nil }
        guard name == "/model" || name == "model" else { return nil }
        let args = between("<command-args>", "</command-args>", in: text)?
            .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return args.split(separator: " ").first.map(String.init) ?? ""
    }

    /// The model a `/model` printed: `Set model to ⟨bold⟩Opus 5⟨/bold⟩ and saved as your default
    /// for new sessions`, with the terminal's bold taken back out.
    ///
    /// Prose, and read as prose. It is the only place the chosen name appears when a picker was
    /// used rather than an argument typed, which is worth one string match — and a build that
    /// words it differently comes back nil, which sends the caller to the word that was typed.
    static func modelPrinted(inRow text: String) -> String? {
        guard let out = between("<local-command-stdout>", "</local-command-stdout>", in: text)
        else { return nil }
        let plain = Ansi.plain(out)
        guard let said = plain.range(of: "Set model to ") else { return nil }
        var rest = plain[said.upperBound...]
        if let stop = rest.firstIndex(of: "\n") { rest = rest[..<stop] }
        if let stop = rest.range(of: " and ") { rest = rest[..<stop.lowerBound] }
        let name = rest.trimmingCharacters(in: .whitespacesAndNewlines)
        return name.isEmpty ? nil : name
    }

    /// The id an assistant row would have carried, for a model named the way `/model` names it.
    ///
    /// The two sources have to end up saying the same thing. Otherwise one card reads
    /// `claude-opus-5` before a reply and `Opus 5` after it about a session that never changed,
    /// and the page's row matching — by id, by prefix — stops finding the button to light up.
    ///
    /// A name this build has never heard of is passed through as it was written. A model released
    /// after this build is still a true answer to "what is it on", and an id invented for it
    /// would not be.
    static func claudeModelID(word: String, printed: String?) -> String? {
        if !word.isEmpty, let row = claudeModels.first(where: { $0.command == word }) { return row.id }
        if let printed, !printed.isEmpty {
            return claudeModels.first(where: { $0.name == printed })?.id ?? printed
        }
        return word.isEmpty ? nil : word
    }

    /// A user row's text, in either shape a build has written it: one string, or a list of blocks.
    private static func userText(_ obj: [String: Any]) -> String? {
        guard let message = obj["message"] as? [String: Any] else { return nil }
        if let text = message["content"] as? String { return text }
        guard let blocks = message["content"] as? [[String: Any]] else { return nil }
        let texts = blocks.compactMap { $0["text"] as? String }
        return texts.isEmpty ? nil : texts.joined(separator: "\n")
    }

    /// What sits between two markers, or nil when the opening one is not there.
    private static func between(_ open: String, _ close: String, in text: String) -> String? {
        guard let start = text.range(of: open) else { return nil }
        guard let end = text.range(of: close, range: start.upperBound..<text.endIndex) else {
            return String(text[start.upperBound...])   // cut off mid-write: the rest is the block
        }
        return String(text[start.upperBound..<end.lowerBound])
    }

    private static func fromQuota(_ quota: [String: Any], at: Int?, now: Date) -> Limits {
        var out = Limits(at: at)
        guard quota["status"] as? String == "rejected",
              let type = quota["rateLimitType"] as? String,
              let resets = int(quota["resetsAt"]) else { return out }
        // A refusal whose reset has passed is history, not a state.
        guard Double(resets) > now.timeIntervalSince1970 else { return out }
        out.windows = [Window(name: windowName(claudeType: type), usedPercent: 100,
                              resetsAt: resets, hit: true)]
        return out
    }

    /// The plan's windows as the status line writes them down. Claude Code hands the status
    /// line `rate_limits` on stdin and nowhere else, so claude-bestiary's `statusline.py` keeps
    /// the last set it was given in `rate-limits.json` under its cache directory:
    /// `{"at": …, "session_id": …, "rate_limits": {"five_hour": {…}, "seven_day": {…}}}`. The
    /// windows are the account's rather than any session's, which is why one file serves every
    /// card. A window whose reset has passed is dropped: the number was true of a window that
    /// is now over, and the honest word for the new one is *unknown*.
    static func claudeLimits(cache data: Data, now: Date = Date()) -> Limits {
        guard let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              let rates = obj["rate_limits"] as? [String: Any] else { return Limits() }
        var out = fromRates(rates, at: int(obj["at"]))
        out.windows.removeAll { window in
            if let resets = window.resetsAt { return Double(resets) <= now.timeIntervalSince1970 }
            return false
        }
        return out
    }

    static func claudeLimits(cacheDirectory: URL, now: Date = Date()) -> Limits {
        let url = cacheDirectory.appendingPathComponent("rate-limits.json")
        guard let data = try? Data(contentsOf: url) else { return Limits() }
        return claudeLimits(cache: data, now: now)
    }

    /// What the transcript says, laid over what the status line wrote down. The transcript's
    /// word is the stronger one — it is a refusal, from the provider, on this session — so a
    /// window it names replaces the cache's row of that name; the rest of the cache stands.
    static func merged(transcript: Limits, cache: Limits) -> Limits {
        guard !transcript.windows.isEmpty else { return cache }
        var out = transcript
        for window in cache.windows where !transcript.windows.contains(where: { $0.name == window.name }) {
            out.windows.append(window)
        }
        out.windows.sort { $0.name < $1.name }   // 5h before 7d, whichever side each came from
        if out.at == nil { out.at = cache.at }
        return out
    }

    /// The status line's shape, wherever it turns up.
    private static func fromRates(_ rates: [String: Any], at: Int?) -> Limits {
        var out = Limits(at: at)
        for key in ["five_hour", "seven_day"] {
            guard let window = rates[key] as? [String: Any],
                  let used = double(window["used_percentage"]) else { continue }
            out.windows.append(Window(name: windowName(claudeType: key), usedPercent: used,
                                      resetsAt: int(window["resets_at"]), hit: used >= 100))
        }
        return out
    }

    /// The plan's state as a Codex rollout records it: every `token_count` event carries
    /// `rate_limits.primary` — `used_percent`, `window_minutes`, `resets_at` — and sometimes a
    /// `secondary`; the newest event is the whole answer, as it is for the token totals.
    static func codexLimits(rollout data: Data) -> Limits {
        for line in data.split(separator: 0x0A).reversed() {
            let text = String(decoding: line, as: UTF8.self)
            guard text.contains("token_count"), text.contains("rate_limits"),
                  let obj = (try? JSONSerialization.jsonObject(with: Data(line))) as? [String: Any],
                  let payload = obj["payload"] as? [String: Any],
                  payload["type"] as? String == "token_count",
                  let rates = payload["rate_limits"] as? [String: Any] else { continue }
            var out = Limits(at: stamp(obj["timestamp"]))
            for key in ["primary", "secondary"] {
                guard let window = rates[key] as? [String: Any],
                      let used = double(window["used_percent"]) else { continue }
                let minutes = int(window["window_minutes"]) ?? 0
                out.windows.append(Window(name: windowName(minutes: minutes), usedPercent: used,
                                          resetsAt: int(window["resets_at"]), hit: used >= 100))
            }
            return out
        }
        return Limits()
    }

    // MARK: - Permission mode

    /// The four positions Claude Code visits with Shift-Tab, plus the fact that its screen could
    /// not be read. `manual` and `unknown` must stay separate: a readable screen with no mode line
    /// is Claude Code's default mode, while an absent screen says nothing about where the cycle is.
    enum PermissionMode: String, Equatable {
        case auto
        case manual
        case acceptEdits
        case plan
        case unknown
    }

    static let permissionModes: [PermissionMode] = [.auto, .manual, .acceptEdits, .plan]

    /// Read only the three phrases Claude Code actually prints. The fourth position deliberately
    /// has no status line, so any non-empty screen without one of them is `manual`; treating an
    /// empty or failed capture the same way would make the page press from a position it does not
    /// know and land on the wrong mode.
    static func permissionMode(screen: String?) -> PermissionMode {
        guard let screen else { return .unknown }
        let plain = Ansi.plain(screen)
        guard !plain.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return .unknown }
        // tmux preserves colour escapes. Remove them before asking about a line's beginning, then
        // require the status line's own prefix so a conversation merely quoting "auto mode on"
        // cannot impersonate the current mode.
        for raw in plain.split(separator: "\n", omittingEmptySubsequences: false).reversed() {
            let line = String(raw)
            if line.hasPrefix("  ⏵⏵ auto mode on") { return .auto }
            if line.hasPrefix("  ⏵⏵ accept edits on") { return .acceptEdits }
            if line.hasPrefix("  ⏸ plan mode on") { return .plan }
        }
        return .manual
    }

    /// How many Shift-Tabs move between two known positions. `nil` is the important answer for
    /// `unknown`: without a starting index there is no honest number of presses. The web client
    /// uses the same four-item wire order when it performs the switch.
    static func permissionSteps(from: PermissionMode, to: PermissionMode) -> Int? {
        guard let start = permissionModes.firstIndex(of: from),
              let end = permissionModes.firstIndex(of: to) else { return nil }
        return (end - start + permissionModes.count) % permissionModes.count
    }

    // MARK: - Models

    /// A model the session could be moved to, and the word that moves it. `command` is what goes
    /// after `/model` in that assistant's own terminal: Claude Code takes an alias (`sonnet`) and
    /// Codex takes the slug itself. Both set the model in place without opening a picker, which
    /// is what makes this a button on a phone rather than a menu on the Mac.
    struct Model: Equatable {
        var id: String
        var name: String
        var command: String
    }

    /// The models Claude Code answers `/model` with by alias. There is no file to read these
    /// from — the aliases are Claude Code's own and move with its releases — so this is the
    /// table. The page matches the session's current model against `id` **by prefix**, which is
    /// how a dated `claude-haiku-4-5-20251001` still finds its row.
    static let claudeModels: [Model] = [
        Model(id: "claude-fable-5", name: "Fable 5", command: "fable"),
        Model(id: "claude-opus-5", name: "Opus 5", command: "opus"),
        Model(id: "claude-sonnet-5", name: "Sonnet 5", command: "sonnet"),
        Model(id: "claude-haiku-4-5", name: "Haiku 4.5", command: "haiku"),
    ]

    /// Codex keeps the list its own picker shows in `~/.codex/models_cache.json`, fetched from
    /// the server when it starts. Read rather than typed in: the names change with the plan and
    /// the month, and a list this app carried would be wrong within one of them. Only what the
    /// picker would list — `visibility` is `hide` for the rows it would not.
    static func codexModels(cache data: Data) -> [Model] {
        guard let json = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              let rows = json["models"] as? [[String: Any]] else { return [] }
        return rows.compactMap { row in
            guard let slug = row["slug"] as? String, !slug.isEmpty else { return nil }
            if let visibility = row["visibility"] as? String, visibility != "list" { return nil }
            let shown = row["display_name"] as? String
            return Model(id: slug, name: (shown?.isEmpty == false) ? shown! : slug, command: slug)
        }
    }

    static func codexModels(home: URL = FileManager.default.homeDirectoryForCurrentUser) -> [Model] {
        let url = home.appendingPathComponent(".codex/models_cache.json")
        guard let data = try? Data(contentsOf: url) else { return [] }
        return codexModels(cache: data)
    }

    static func models(for assistant: Assistant?) -> [Model] {
        switch assistant {
        case .claude: return claudeModels
        case .codex: return codexModels()
        case nil: return []
        }
    }

    // MARK: - The wire shape

    /// What the route answers with. Pure, so a test can hand it every piece and read the JSON.
    ///
    /// `usage` and `files` are **absent** rather than zeroed when they could not be had: a session
    /// whose transcript has not been found yet has not spent nothing, and a directory that is not
    /// a repository has not got a clean tree. `limits.windows` is empty for the same reason.
    /// `deploy` is the deploy and CI rows of `/links`, unchanged, so a state means there what it
    /// means here.
    static func payload(id: String, assistant: Assistant?, sessionId: String?, model: String?,
                        cwd: String?, startedAt: Date?, now: Date = Date(),
                        usage: Orchestrator.Usage?, limits: Limits, files: Files?,
                        deploy: [[String: Any]], models: [Model] = [],
                        permission: PermissionMode? = nil) -> [String: Any] {
        var session: [String: Any] = ["id": id]
        if let assistant { session["assistant"] = assistant.rawValue }
        if let sessionId { session["sessionId"] = sessionId }
        if let model, !model.isEmpty { session["model"] = model }
        if let cwd { session["cwd"] = cwd }
        if let startedAt {
            session["startedAt"] = Int(startedAt.timeIntervalSince1970)
            session["seconds"] = max(0, Int(now.timeIntervalSince(startedAt)))
        }

        var out: [String: Any] = ["session": session, "deploy": deploy]
        // Every row is a button on the card. `command` is the word the page sends after
        // `/model`; `id` is what it compares the session's current model against.
        out["models"] = models.map { ["id": $0.id, "name": $0.name, "command": $0.command] }
        // Codex has no corresponding cycle, so absence is the contract rather than an empty
        // object a client might mistake for a feature whose reading merely failed.
        if assistant == .claude {
            out["permission"] = [
                "current": (permission ?? .unknown).rawValue,
                "options": permissionModes.map(\.rawValue),
            ]
        }

        if let usage {
            var counts: [String: Any] = [
                "input": usage.input, "output": usage.output,
                "cacheRead": usage.cacheRead, "cacheWrite": usage.cacheWrite,
                "total": usage.total,
            ]
            if let model = usage.model { counts["model"] = model }
            if let cost = usage.costUsd { counts["costUsd"] = cost }
            out["usage"] = counts
        }

        var plan: [String: Any] = [
            "windows": limits.windows.map { window -> [String: Any] in
                var row: [String: Any] = ["name": window.name, "hit": window.hit]
                if let used = window.usedPercent { row["usedPercent"] = used }
                if let resets = window.resetsAt { row["resetsAt"] = resets }
                return row
            },
        ]
        if let at = limits.at { plan["at"] = at }
        out["limits"] = plan

        if let files {
            out["files"] = [
                "branch": files.branch, "head": files.head,
                "ahead": files.ahead, "behind": files.behind,
                "staged": files.staged, "unstaged": files.unstaged,
                "untracked": files.untracked, "conflict": files.conflict,
            ]
        }
        return out
    }

    // MARK: - Obtaining

    /// The working tree, counted. `nil` when the directory is not a repository or `git` did not
    /// answer in time — and the two are the same answer on purpose, because both mean "there is
    /// no count to show", and a card that said *clean* about a directory it could not read would
    /// be lying in the direction that matters.
    static func files(cwd: String) -> Files? {
        guard let text = git(["status", "--porcelain=v2", "--branch"], cwd: cwd) else { return nil }
        return parseStatus(text)
    }

    /// One `git`, read-only, and never left to hang: `GIT_OPTIONAL_LOCKS=0` so a status does not
    /// touch the index while the session is mid-commit, and a deadline because a repository on a
    /// network volume can take longer than a phone will wait.
    private static func git(_ arguments: [String], cwd: String, timeout: TimeInterval = 3) -> String? {
        let task = Process()
        task.executableURL = URL(fileURLWithPath: "/usr/bin/git")
        task.arguments = arguments
        task.currentDirectoryURL = URL(fileURLWithPath: cwd)
        var environment = ProcessInfo.processInfo.environment
        environment["GIT_OPTIONAL_LOCKS"] = "0"
        task.environment = environment
        let pipe = Pipe()
        task.standardInput = FileHandle.nullDevice
        task.standardOutput = pipe
        task.standardError = FileHandle.nullDevice
        do { try task.run() } catch { return nil }
        let killer = DispatchWorkItem { if task.isRunning { task.terminate() } }
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + timeout, execute: killer)
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        task.waitUntilExit()
        killer.cancel()
        guard task.terminationStatus == 0 else { return nil }
        return String(data: data, encoding: .utf8)
    }

    // MARK: - Small readers

    private static func int(_ value: Any?) -> Int? {
        if let n = value as? Int { return n }
        if let n = value as? Double { return Int(n) }
        if let s = value as? String { return Int(s) }
        return nil
    }

    private static func double(_ value: Any?) -> Double? {
        if let n = value as? Double { return n }
        if let n = value as? Int { return Double(n) }
        if let s = value as? String { return Double(s) }
        return nil
    }

    private static let iso: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()
    private static let isoPlain: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()

    /// A record's `timestamp` — ISO 8601 in both assistants' files — as unix seconds.
    private static func stamp(_ value: Any?) -> Int? {
        guard let text = value as? String else { return nil }
        guard let date = iso.date(from: text) ?? isoPlain.date(from: text) else { return nil }
        return Int(date.timeIntervalSince1970)
    }
}
