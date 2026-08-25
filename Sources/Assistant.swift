import Foundation

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

    /// The same command with a model named on it, when one was.
    ///
    /// Both CLIs spell the flag `--model`, which is the only reason this is one function rather
    /// than a switch. The name is the single variable part of the whole line, and what keeps it
    /// from being a fragment of a command is ``StartPoints/modelName(_:)`` — pass anything that
    /// has not been through it and the guarantee in `StartPoints`' header stops being true.
    func command(model: String?) -> String {
        guard let model, !model.isEmpty else { return command }
        return command + " --model " + model
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
            let start = parent == nil ? 2 : 3
            guard let seen = read(tokens: fields.dropFirst(start).map(String.init)) else { continue }
            rows.append(Row(tty: tty, pid: pid, parent: parent, assistant: seen.assistant,
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
            found[tty] = Running(assistant: chosen.assistant, pid: chosen.pid)
        }
        return found
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
