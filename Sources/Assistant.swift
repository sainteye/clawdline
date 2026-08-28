import Foundation

/// The two deliberate per-dispatch reasoning overrides Clawdline exposes for Codex.
///
/// This is a closed enum rather than a free-form config value: a task can ask for the careful
/// coding default (`high`) or the deeper planning/review setting (`xhigh`). Omitting it leaves
/// Codex and the user's own configuration untouched. Other Codex values are intentionally not
/// part of the dispatch protocol.
enum ReasoningEffort: String, CaseIterable {
    case high, xhigh
}

/// How far a dispatched session may go before it stops and asks.
///
/// Clawdline's own three words rather than either CLI's, because the two spell this differently
/// and neither spelling is the one to teach: Claude Code's `--permission-mode` names six values,
/// Codex's `--ask-for-approval` takes two, and the overlap that means something to somebody
/// writing a task down is three. A closed list, like the assistant itself — a name that is not on
/// it is `bad_task`, never a flag somebody assembled.
///
/// **Every value here was checked against a real terminal, on more than one model.** That second
/// half is why `auto` is not on the list. Claude Code has an `auto` mode and `--permission-mode
/// auto` selects it — on Sonnet and on Opus. On Haiku the same flag produces `manual`, the mode
/// where everything is asked, with no error and no warning.
///
/// So it is not a broken value; it is a **model-dependent** one, which is worse for something
/// with this job. A word here has to mean the same thing to every session a task can name, and a
/// word that quietly becomes the *strictest* setting on the cheapest model is the failure nobody
/// catches: the tab sits at a prompt nobody is watching, and the task times out looking like work
/// that was simply never done. The three below behave the same on every model tried.
enum Permission: String, CaseIterable, Comparable {
    /// Every step that would need approval stops and asks — Claude Code's `manual`, which is also
    /// what it does with no flag at all, and what Haiku falls back to when handed a mode it does
    /// not have. **Nobody is watching a child's tab**, so in practice this is a session that sits
    /// at a prompt until it times out. It is the right answer only for a task somebody intends to
    /// sit and supervise.
    case ask
    /// Files are written without asking; running a command still stops.
    ///
    /// The default, and the one measured against what a dispatched session actually does. A child
    /// reads a briefing, works, and writes a result — and before this it stopped on the last of
    /// those three, one keystroke from done, with nobody in the room. What it does *not* widen is
    /// running commands, which is the half worth still being asked about.
    case edits
    /// Nothing is asked. Whatever the assistant can do, it does, in the directory it was pointed
    /// at. Behind `orchestrator_permission` — a task cannot reach this unless this Mac has said
    /// so, because the session asking is not the person who would live with the consequences.
    ///
    /// The one thing that genuinely needs it is a child that dispatches: the `jq` line that
    /// writes a task file trips Claude Code's command screening on its own shape (a brace next
    /// to a quote reads as obfuscation), and that screening offers no "always allow".
    case full

    private var rank: Int {
        switch self {
        case .ask:   return 0
        case .edits: return 1
        case .full:  return 2
        }
    }
    static func < (a: Permission, b: Permission) -> Bool { a.rank < b.rank }

    /// The flags that spell this for one assistant, or nothing at all where the mode is already
    /// that CLI's own default. Every string here is a literal: nothing a task sends reaches it,
    /// and every one of them was read back off a status line rather than taken from `--help`.
    func flags(for assistant: Assistant) -> String {
        switch (self, assistant) {
        case (.ask, _):        return ""
        // Codex writes inside its workspace without asking already, so the flag that widens
        // Claude Code to match is the whole of the difference on that side.
        case (.edits, .claude): return "--permission-mode acceptEdits"
        case (.edits, .codex):  return "--ask-for-approval on-request --sandbox workspace-write"
        case (.full, .claude): return "--permission-mode bypassPermissions"
        case (.full, .codex):  return "--ask-for-approval never --sandbox workspace-write"
        }
    }
}

/// Which assistant is running in a session, and the few facts that differ between them.
///
/// The app started out able to see exactly one thing: a terminal with `claude` in it. Every
/// question it asks of a session — is it busy, what is it asking me, what has it said — was
/// answered by code that knew that one program's shapes by heart, and `isClaude` was a boolean
/// because there was nothing for it to be a boolean *between*.
///
/// Codex draws a different screen, keeps its record somewhere else, and leaves on a different
/// word. So the shapes became something a session **has** rather than something the app assumes,
/// and this is the list of what that is. Everything genuinely per-assistant lives here or is
/// reached from here; the rest of the app asks a session what it is and stops caring.
///
/// The list is closed on purpose. A third one is a day's work — a screen to read, a record to
/// find, a word to leave on — and not a plugin point, because each of those three is a shape
/// somebody has to observe on a real terminal and keep observing.
enum Assistant: String, CaseIterable {
    case claude
    case codex

    /// What a person calls it. Not translated: these are product names.
    var label: String {
        switch self {
        case .claude: return "Claude Code"
        case .codex:  return "Codex"
        }
    }

    /// The short name, for a row that has to say which of two it is without spending a line on
    /// it. The command, which is also what a person types, so it is not a third name to learn.
    var short: String { rawValue }

    /// The command that starts one, with no arguments. A literal, and it stays a literal —
    /// see ``StartPoints`` for why the one route that runs it has no field a string can enter by.
    var command: String { rawValue }

    /// How this assistant is asked to pick a recorded conversation back up.
    ///
    /// A literal per case rather than one string, because the two spell it differently and only
    /// one of the two spellings is a flag at all: Claude Code takes `--resume <uuid>`, Codex
    /// takes a `resume` **subcommand**. Both resolve a UUID against their own record and refuse
    /// an id they have never written, which is the second half of the promise in
    /// ``StartPoints``' header — the first half being that nothing but a UUID ever gets here.
    var resumeFlag: String {
        switch self {
        case .claude: return "--resume"
        case .codex:  return "resume"
        }
    }

    /// Session identity a tab this app opens must not inherit.
    ///
    /// A terminal is a process, and a process keeps the environment it was started with. iTerm2
    /// launched once from a shell *inside* a Claude Code session carries that session's identity
    /// variables for as long as it runs, and hands a copy to every tab opened afterwards —
    /// including the ones opened from here. Measured on 2026-08-28: `ps -Ewww` on the iTerm2
    /// process held `CLAUDE_PID`, `CLAUDE_CODE_SESSION_ID` and `CLAUDE_CODE_CHILD_SESSION=1`
    /// belonging to a session in a different window, and every child dispatched into that iTerm2
    /// was exec'd with the same three.
    ///
    /// A child that inherits them does not merely misreport itself; it *is* somebody else's
    /// nested session as far as the CLI is concerned. It writes no `~/.claude/sessions/<pid>.json`
    /// and opens no transcript under `~/.claude/projects/` — three live children that afternoon
    /// had neither, while five unaffected sessions of the same age had both. With no transcript
    /// there is no way to prove the briefing arrived, so `expireSpawningIfDue` reads a working
    /// child as a tab that never reached a prompt, and the disposal on that path erases an
    /// uncommitted checkout. One delivery branch was lost that way before the cause was found.
    ///
    /// **The values do not come from this app** — its own process has none of them, checked the
    /// same way — so there is nothing here to stop exporting. What this app can do is refuse to
    /// pass on what it was handed.
    ///
    /// The list follows one rule: **drop what claims to be a session, keep what describes the
    /// installation.** A name is here because its value in a new tab would be a statement about
    /// some other conversation, and a name is missing because its value is still true in the tab
    /// being opened.
    ///
    /// Dropped, for Claude Code — a 2.1.250 session exports twelve names, and these are the nine
    /// of them that are not the three kept below:
    /// - `CLAUDE_CODE_SESSION_ID`, `CLAUDE_CODE_BRIDGE_SESSION_ID` — another conversation's ids.
    /// - `CLAUDE_PID` — the pid that names another session's registry file.
    /// - `CLAUDE_CODE_CHILD_SESSION`, `CLAUDECODE` — the two flags that say "you are running
    ///   inside a Claude Code session". They are the ones that actually change the behaviour;
    ///   the ids above are what it would then be wrong about.
    /// - `CLAUDE_CODE_MESSAGING_SOCKET`, `CLAUDE_CODE_MESSAGING_TOKEN` — a path to another
    ///   session's IPC socket and the credential for it. A new session speaking on that channel
    ///   is impersonation rather than confusion.
    /// - `CLAUDE_EFFORT` — the reasoning effort a person chose in *that* conversation. It is
    ///   neither an id nor an installation fact, which is why the first version of this list left
    ///   it out of both columns and left this paragraph claiming a set it did not hold. It is
    ///   dropped because Clawdline has no effort flag for Claude at all — `reasoningEffort` is
    ///   emitted only as Codex's `--config model_reasoning_effort` — so an inherited value would
    ///   be the *sole* thing deciding how hard a child thinks, chosen by nobody for that task and
    ///   recorded nowhere. Dropped, a child runs at its own default, which is more predictable
    ///   rather than less. The house rules make the same complaint one level up about an
    ///   inherited model: *a named model can be argued with; an inherited one cannot even be
    ///   seen.*
    /// - `AI_AGENT` — `claude-code_2-1-250_agent`, a claim about which assistant is running. It
    ///   carries no conversation id, so it did not cause the incident this list exists for; it is
    ///   here because it is the one name that can never correct itself. A Claude tab overwrites
    ///   it on the way in. **A Codex tab does not** — measured on the live Codex TUI, whose own
    ///   child process had no `AI_AGENT` of its own — so a Codex session opened from a polluted
    ///   terminal, and every command it runs, would carry "I am inside Claude Code 2.1.250" for
    ///   as long as it lives.
    ///
    /// Kept, deliberately: `CLAUDE_CODE_ENTRYPOINT=cli` is true of a tab where `claude` is typed;
    /// `CLAUDE_CODE_MAX_SUBAGENTS_PER_SESSION` is a number this Mac sets in its own settings;
    /// `CLAUDE_CODE_EXECPATH` names the installed program rather than a conversation, and does
    /// not decide which binary runs — `CLAUDE_CODE_EXECPATH=/definitely/not/here claude
    /// --version` still answers `2.1.250`, so dropping it would buy nothing and could only lose
    /// something on a machine that does use it.
    ///
    /// For Codex the same shape exists and is documented in `docs/orchestrator.md`:
    /// `CODEX_THREAD_ID`, with `CODEX_SESSION_ID` as its compatible spelling, is the rollout id a
    /// session exports, which is exactly what a dispatching child is told to read back as its own.
    /// `CODEX_SANDBOX` and `CODEX_SANDBOX_NETWORK_DISABLED` are dropped for the same reason and
    /// not as sandbox policy: they are a claim to be running *inside* another Codex's sandbox,
    /// they are false in a tab this app opened, and a child that believes the second one skips
    /// the loopback half of its progress channel because this repository's own briefings tell it
    /// to. Setting a sandbox is `--sandbox` on the command line, which is unaffected.
    ///
    /// The Claude Code names were read off a live 2.1.250 session's environment and the Codex
    /// ones out of the shipped binary's strings; a version that invents another name will need
    /// this list read again, which is what ``Tests`` pins so that a re-reading is a visible edit.
    var inheritedIdentityVariables: [String] {
        switch self {
        case .claude:
            return ["CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION",
                    "CLAUDE_PID", "CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN",
                    "CLAUDE_CODE_BRIDGE_SESSION_ID", "CLAUDE_EFFORT", "AI_AGENT"]
        case .codex:
            return ["CODEX_THREAD_ID", "CODEX_SESSION_ID",
                    "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED"]
        }
    }

    /// The `env -u NAME …` that starts every command line this app types, trailing space
    /// included, or nothing at all when there is nothing to drop.
    ///
    /// `env` rather than `unset` for two reasons that were checked rather than assumed. It leaves
    /// the rest of the environment alone, so the tab keeps its `PATH`, its `CLAUDE_CODE_ENTRYPOINT`
    /// and everything else a login shell put there: `env -u CLAUDECODE sh -c 'echo
    /// $CLAUDE_CODE_ENTRYPOINT'` still answers `cli`. And `env` execs the program in its own
    /// process, so `ps` shows the program's argv with nothing in front of it — `env -u CLAUDECODE
    /// sleep 3` lists as `sleep 3` — which is what ``Assistant/reading(ofPS:)`` reads a tty's
    /// session out of. A wrapper that stayed on the command line would have made every running
    /// session unrecognisable.
    ///
    /// What it costs is aliases: `env` resolves its program on `PATH`, so a shell alias or
    /// function called `claude` is stepped past. Both CLIs install as real executables — the one
    /// on this Mac is `~/.local/bin/claude`, a symlink into the versions directory — and a line
    /// that runs the installed program rather than somebody's alias is the more predictable of
    /// the two for a tab nobody is watching.
    var dropInheritedIdentity: String {
        let names = inheritedIdentityVariables
        guard !names.isEmpty else { return "" }
        return "env " + names.map { "-u " + $0 }.joined(separator: " ") + " "
    }

    /// The same command with a model named on it, an optional typed Codex reasoning override, a
    /// permission mode, a directory the session may reach outside the one it was started in,
    /// and a conversation to pick back up.
    ///
    /// Model and reach are spelled the same way by both CLIs — `--model`, and `--add-dir` — while
    /// reasoning is deliberately emitted only for Codex. The values are the variable part of
    /// the whole line, and what keeps them from being fragments of a command is
    /// ``StartPoints/modelName(_:)``, ``StartPoints/extraDir(_:)`` and
    /// ``StartPoints/sessionName(_:)``: pass anything that has not been through those and the
    /// guarantee in `StartPoints`' header stops being true.
    ///
    /// **`resume` goes first**, before every other flag. `--resume` takes an optional value, so
    /// it is the one flag here whose meaning depends on what follows it: with the id immediately
    /// after it there is nothing to work out, and with a flag after it instead the CLI would open
    /// its own picker in a tab nobody is sitting at.
    ///
    /// **The line opens with ``dropInheritedIdentity``**, before the program's own name, so a
    /// session started here is never handed the identity of whatever happened to launch the
    /// terminal. It is a prefix rather than a step of its own because every route that types a
    /// command goes through this method — iTerm2 and tmux both — and a second route that
    /// forgot the cleaning is the failure it exists to prevent.
    ///
    /// **`--add-dir` is what makes a dispatched session workable at all.** A child's whole task
    /// lives in `/tmp/.clawdline/<id>/`, which is outside the project it was started in, and a
    /// session asks before it reaches outside — for reading its own briefing, for making its own
    /// artifacts directory, for every file it was sent to write. Nobody is watching that tab to
    /// answer, so without this the first question is where the task stops. It is also the reason
    /// the second level felt so much worse than the first: a child that dispatches touches that
    /// directory a dozen more times, once per grandchild it makes, briefs and reads back.
    func command(model: String?, reasoningEffort: ReasoningEffort? = nil,
                 permission: Permission = .ask, addDir: String? = nil,
                 resume: String? = nil) -> String {
        var line = dropInheritedIdentity + command
        if let resume, !resume.isEmpty { line += " " + resumeFlag + " " + resume }
        if let model, !model.isEmpty { line += " --model " + model }
        if self == .codex, let reasoningEffort {
            line += " --config model_reasoning_effort=" + reasoningEffort.rawValue
        }
        if let addDir, !addDir.isEmpty { line += " --add-dir " + addDir }
        let flags = permission.flags(for: self)
        if !flags.isEmpty { line += " " + flags }
        return line
    }

    /// The line that ends a session, typed at its prompt.
    ///
    /// Claude Code takes `/exit`, Codex takes `/quit` — and each rejects the other's, which is
    /// the entire reason this is not one string in ``Targets/end(_:)``. A refused word leaves the
    /// session open and the tab closing under it a second later, which is the failure this
    /// avoids rather than a detail.
    var quitLine: String {
        switch self {
        case .claude: return "/exit"
        case .codex:  return "/quit"
        }
    }

    /// Whether this assistant has ever run on this Mac.
    ///
    /// Its home directory being there, which is not the same question as the binary being on
    /// `PATH` — and is the only one that can be answered from an app launched from Finder, which
    /// inherits no login shell and therefore no `PATH` worth reading. It is also the better
    /// question: a directory full of sessions is proof the thing ran here, where a binary on a
    /// path is a promise that it could.
    ///
    /// What it decides is whether to *offer* starting one. Getting it wrong in the shy direction
    /// costs a button; getting it wrong the other way opens a tab that says "command not found".
    var isInstalled: Bool {
        let url: URL
        switch self {
        case .claude:
            url = FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent(".claude", isDirectory: true)
        case .codex:
            url = Codex.home
        }
        var isDirectory: ObjCBool = false
        return FileManager.default.fileExists(atPath: url.path, isDirectory: &isDirectory)
            && isDirectory.boolValue
    }

    /// The ones worth offering, in the order they should be offered in.
    ///
    /// Never empty. A Mac with neither home directory on it is one where nothing has run yet,
    /// and answering "nothing" there would leave a person with no way to start the first
    /// session — so the answer is the whole list, which is what it was before any of this
    /// checked anything.
    static var available: [Assistant] {
        let installed = allCases.filter(\.isInstalled)
        return installed.isEmpty ? allCases : installed
    }

    /// Whether a bare digit typed into this assistant's menu answers it.
    ///
    /// Both do, and both were checked on a real one rather than assumed: Claude Code's picker
    /// acts on the digit, and Codex's does too — pressing `2` on its trust dialog picks "No,
    /// quit" and the process is gone before the next capture. It is a property rather than a
    /// constant so that an assistant that does not work this way can say so instead of having
    /// keystrokes sent at it hopefully.
    var answersDigits: Bool { true }

    // MARK: - Reading `ps`

    /// One assistant found running on a tty.
    struct Running: Equatable {
        let assistant: Assistant
        let pid: Int32
        /// Kernel process identity is PID plus its authoritative start instant. `nil` is kept
        /// only for legacy/test process listings that did not carry `lstart`; no safe-close
        /// signal is allowed to use such a partial identity.
        let processStart: Date?

        init(assistant: Assistant, pid: Int32, processStart: Date? = nil) {
            self.assistant = assistant
            self.pid = pid
            self.processStart = processStart
        }
    }

    /// Subcommands that are the same binary doing something you cannot type into.
    ///
    /// `codex exec` runs a prompt and prints; `codex mcp-server` and the two servers next to it
    /// are protocols on a pipe. All of them are `codex` in a process listing, and without this a
    /// script running one in a terminal would appear in the session list as somewhere to send
    /// work — a row that accepts your sentence and drops it.
    ///
    /// Only the **first** argument counts, and only when it is not a flag. `codex -m gpt-5 exec`
    /// gets through, which is worth saying rather than papering over: the alternative is looking
    /// for these words anywhere in the line, and a prompt is a line — `codex "please exec this"`
    /// would then hide a real session, which is the worse of the two mistakes.
    static let notASession: Set<String> = [
        "exec", "e", "mcp-server", "app-server", "exec-server", "review", "sandbox",
    ]

    /// What is running on each tty, from `ps -ax -o tty=,pid=,ppid=,command=`.
    ///
    /// Split out from the process that produces it so it can be tested against fixed output
    /// rather than against whatever happens to be running on the machine at the time — the
    /// parsing is where the mistakes live, and running `ps` is not.
    ///
    /// **Two processes per Codex session, on one tty.** The published CLI is a Node shim that
    /// spawns a native binary, so `ps` shows `node …/bin/codex` and `…/vendor/…/bin/codex`
    /// together. Either proves a session is there; the native one is preferred for the pid,
    /// because it is the process that holds the working directory anybody asks about later.
    static func reading(ofPS output: String) -> [String: Running] {
        struct Row {
            let tty: String
            let pid: Int32
            let parent: Int32?
            let processStart: Date?
            let assistant: Assistant
            let viaShim: Bool
            let denied: Bool
        }

        var rows: [Row] = []

        for line in output.split(separator: "\n") {
            let fields = line.split(separator: " ", omittingEmptySubsequences: true)
            guard fields.count >= 3, fields[0].hasPrefix("ttys"),
                  let pid = Int32(fields[1]) else { continue }
            let tty = String(fields[0])
            // Old three-column fixtures are still understood. The real scan includes ppid,
            // because ancestry is the only reliable way to distinguish `codex exec`'s native
            // child from an interactive Codex session that happens to have an app-server child.
            let parent = fields.count >= 4 ? Int32(fields[2]) : nil
            // Production includes `lstart`, whose five fixed POSIX-English fields sit between
            // ppid and command. Old fixtures and old callers remain readable, but yield no
            // process-start identity and therefore cannot authorize a signal.
            let hasLongStart = parent != nil && fields.count >= 9
                && fields[3].count == 3 && fields[6].contains(":")
                && Int(fields[7]) != nil
            let processStart = hasLongStart
                ? parseProcessStart(fields[3...7].map(String.init)) : nil
            let start = hasLongStart ? 8 : (parent == nil ? 2 : 3)
            guard let seen = read(tokens: fields.dropFirst(start).map(String.init)) else { continue }
            rows.append(Row(tty: tty, pid: pid, parent: parent, processStart: processStart,
                            assistant: seen.assistant,
                            viaShim: seen.viaShim, denied: seen.denied))
        }

        let byPID = Dictionary(uniqueKeysWithValues: rows.map { ($0.pid, $0) })
        func hasDeniedAncestor(_ row: Row) -> Bool {
            var next = row.parent
            var visited = Set<Int32>()
            while let pid = next, visited.insert(pid).inserted, let parent = byPID[pid] {
                if parent.denied { return true }
                next = parent.parent
            }
            return false
        }

        var found: [String: Running] = [:]
        for tty in Set(rows.map(\.tty)) {
            let onTTY = rows.filter { $0.tty == tty }
            // With ppid present, refusal flows down the process tree, not sideways across the
            // tty. Interactive Codex now starts `codex app-server --listen stdio://` beside its
            // UI; that descendant is not a place to type, but its parent still is. Conversely,
            // `codex exec` may spawn an argless native child that looks interactive on its own,
            // and the refused ancestor keeps that child out.
            let legacyDenied = onTTY.contains { $0.parent == nil && $0.denied }
            let candidates = onTTY.filter {
                !$0.denied && !legacyDenied && !hasDeniedAncestor($0)
            }
            // A shim never displaces the native process it started. They are one session, and
            // only the native one can answer where it is working.
            guard let chosen = candidates.last(where: { !$0.viaShim }) ?? candidates.first else {
                continue
            }
            found[tty] = Running(assistant: chosen.assistant, pid: chosen.pid,
                                 processStart: chosen.processStart)
        }
        return found
    }

    /// Parse macOS `ps -o lstart=` without inheriting the person's locale. `ps` emits this
    /// column in its POSIX English shape even when the UI locale is not English.
    static func parseProcessStart(_ fields: [String]) -> Date? {
        guard fields.count == 5 else { return nil }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.timeZone = .current
        formatter.dateFormat = "EEE MMM d HH:mm:ss yyyy"
        return formatter.date(from: fields.joined(separator: " "))
    }

    /// One process's command line, as far as this cares: which assistant it is, whether it
    /// arrived through a shim, and whether it is a session at all.
    private static func read(tokens: [String])
        -> (assistant: Assistant, viaShim: Bool, denied: Bool)? {
        guard let head = tokens.first else { return nil }

        if let assistant = named(head) {
            return (assistant, false, refused(assistant, args: Array(tokens.dropFirst())))
        }
        // `node …/bin/codex …` — the published Codex CLI, whose first token is the interpreter.
        // Deliberately not general: this is not "look at the second token too", it is the one
        // launcher shape that is known to exist.
        guard head == "node" || head.hasSuffix("/node"), tokens.count > 1,
              let assistant = named(tokens[1]) else { return nil }
        return (assistant, true, refused(assistant, args: Array(tokens.dropFirst(2))))
    }

    /// The assistant a path or a process name stands for, or nothing. The whole basename has to
    /// match: `claude-code` and `codexctl` are other people's programs.
    ///
    /// Also tmux's second opinion — it reports a pane's process name, which is this without the
    /// path — so it is not private. See ``Tmux/parsePanes(_:running:)`` for why that opinion is
    /// only ever a second one.
    static func named(_ path: String) -> Assistant? {
        for assistant in Assistant.allCases
        where path == assistant.command || path.hasSuffix("/" + assistant.command) {
            return assistant
        }
        return nil
    }

    private static func refused(_ assistant: Assistant, args: [String]) -> Bool {
        guard assistant == .codex else { return false }
        guard let first = args.first(where: { !$0.hasPrefix("-") }) else { return false }
        return notASession.contains(first)
    }
}
