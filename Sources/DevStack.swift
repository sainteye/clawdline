import Foundation

/// A project's dev stack, as the project itself describes it.
///
/// **Clawdline never starts a process.** It reads `.devstack.json` out of a repository and runs
/// the commands that file names. That single rule is what makes both halves of this work:
///
/// - The servers outlive the app. Anything Clawdline spawned would die with Clawdline — on quit,
///   on update, on crash — and a dev stack whose life is tied to a text field is worse than one
///   tied to a terminal tab, because at least the tab is visible while it dies.
/// - It works for projects nobody here has seen. process-compose, overmind, pm2, docker compose,
///   systemd, a Makefile with PID files: they all reduce to "a command that prints state" and
///   "a command that restarts something". Nothing in this file knows which one a project uses.
///
/// The format is documented in docs/devstack.md. That page is the contract; anything that writes
/// such a file works, exactly as with docs/project-status.md.
///
/// **Absent is the normal case.** Most repositories have no `.devstack.json`, and the footer
/// simply has nothing to say about servers.
enum DevStack {

    // MARK: - What a project declares

    /// One process, as the project's `status` command reported it.
    struct Process: Equatable {
        var name: String
        /// healthy · running · starting · completed · exited · stopped.
        ///
        /// Six words, and deliberately not extensible: a vocabulary that grows is one every
        /// reader has to guess at. `running` means up with no probe configured — the honest
        /// answer when nobody knows whether it is healthy — while `completed` is the one-shot
        /// that finished on purpose (a build, a `docker compose up -d`), which is success and
        /// must not be drawn like a failure.
        var state: String
        var port: Int?
        var url: String?
        var pid: Int?
        /// When this process started, as a unix timestamp.
        ///
        /// **An instant, not a duration.** A state document can be cached or a second stale, and
        /// an absolute moment survives that while "up for 9h 34m" freezes the moment it is
        /// written. The reader subtracts.
        var since: Double?
        var exitCode: Int?
        /// The tail of whatever this process printed as it died, when it died badly.
        ///
        /// Carried in the state document rather than left for a follow-up `logs` call because
        /// whoever is looking at a red dot always wants it next, and because it is the thing
        /// that lets Claude Code go straight from "it is broken" to fixing it.
        var error: String?

        var isDown: Bool { state == "exited" }
        var isUp: Bool { state == "healthy" || state == "running" || state == "completed" }

        /// The one line of this process's dying words worth putting on a row.
        ///
        /// `error` is a *tail*, not a message, and three things stood between it and something
        /// readable. The row used to solve none of them — it took the last line and cut it at
        /// seventy characters — and each one was caught by a different project on this machine.
        ///
        /// - **The envelope.** What a supervisor stores is `{"level":"error","process":
        ///   "build-web","replica":0,"message":"bash: npm: command not found"}`: sixty characters
        ///   of bookkeeping in front of the twenty-eight that are the answer. Cut at seventy, the
        ///   row read `…"message":"bash: np` and stopped exactly where the diagnosis started.
        /// - **Last is not loudest.** The tail holds everything the process printed, and a web
        ///   server prints request logs until the moment it dies. The last line of that was
        ///   `INFO: GET /api/v1/health 200 OK` — a success message offered, under a red ✗, as the
        ///   explanation for a crash.
        /// - **It can end mid-JSON.** The tail is cut to a byte budget by whoever wrote it, so
        ///   the final line is often half an envelope: `{"level":` and nothing after it.
        ///
        /// So: unwrap every line, throw away the halves and the blanks, and prefer the last line
        /// that *reads* like a failure over the last line that merely *is* last. When nothing
        /// reads like one, this is `nil` rather than the last ordinary line — an exit code with
        /// no explanation is a smaller claim than a wrong explanation, and the log is one press
        /// away for anyone who wants the rest.
        var reason: String? {
            guard let error, !error.isEmpty else { return nil }
            var lines: [(text: String, loud: Bool)] = []
            for raw in error.split(separator: "\n", omittingEmptySubsequences: true) {
                let envelope = StackLog.envelope(String(raw))
                let text = Ansi.plain(envelope.message)
                    .trimmingCharacters(in: .whitespacesAndNewlines)
                // A truncated envelope unwraps to itself, because it is not parseable JSON. It is
                // recognisable by the shape it kept, and it is never worth showing.
                if text.isEmpty || text.hasPrefix("{\"") { continue }
                // Loud if the text says so, or if it came down stderr *and* did not say
                // otherwise. The second half is what keeps cloudflared's `INF` startup lines —
                // which process-compose files under `level: error` like everything else on that
                // pipe — from being offered as the reason a tunnel went down.
                lines.append((text, StackLog.level(of: text) == .error
                                    || (envelope.level == "error"
                                        && !StackLog.declaresCalm(text))))
            }
            // Loud by the text it printed, or by the pipe it came down — a shell's
            // `command not found` announces itself as neither ERROR nor FATAL, and a rule that
            // only reads words would lose the one line that mattered here.
            return lines.last(where: { $0.loud })?.text
        }
    }

    /// Everything a project's stack is doing right now.
    struct State: Equatable {
        /// running · partial · stopped · unknown.
        ///
        /// `unknown` is the one that has to exist. A project that declares no ports and whose
        /// `status` command has not been trusted yet cannot be reported as `stopped` — it is
        /// very likely running, and an indicator that says "down" about a live site is worse
        /// than no indicator at all, because the next real outage looks identical to it.
        var state: String
        var updatedAt: Double
        var processes: [Process]

        var isRunning: Bool { state == "running" }
        var isStopped: Bool { state == "stopped" }
        var isUnknown: Bool { state == "unknown" }
        /// The count that fits in a footer: how many are up out of how many are declared.
        var upCount: Int { processes.filter { $0.isUp }.count }
        var brokenNames: [String] { processes.filter { $0.isDown }.map { $0.name } }
        /// When the stack came up: the earliest of its running parts.
        ///
        /// Earliest rather than latest, because one process restarting does not make the stack
        /// young again — after a two-second `restart api` the honest answer is still "up since
        /// this morning".
        var since: Double? { processes.compactMap(\.since).min() }

        /// The broken process worth naming, out of however many are broken.
        ///
        /// **Not the first one.** A status command prints its processes in whatever order it
        /// likes — alphabetical, for the projects here — and the first name in that order is as
        /// likely to be a casualty as a cause. `atrium` showed `✗ blog`: a process that exits
        /// silently, with no output at all, because the build it waits on never completed. The
        /// build that actually failed, and the only line in the entire document that said why,
        /// was `build-blog`, two rows further down and never drawn.
        ///
        /// That is the shape of every dependency failure, not a quirk of one project: the thing
        /// that broke says something, and the things waiting on it die quietly. So prefer one
        /// that left an explanation behind, then one whose exit code is specific — 127 is a
        /// shell's "command not found" and is almost always a cause rather than an effect,
        /// where 1 is the code everything uses for everything — and only then fall back to the
        /// order they arrived in.
        var rootCause: Process? {
            let down = processes.filter { $0.isDown }
            guard var best = down.first else { return nil }
            func rank(_ p: Process) -> Int {
                (p.reason != nil ? 2 : 0) + ((p.exitCode ?? 0) > 1 ? 1 : 0)
            }
            // First among equals, so a tie keeps the project's own order rather than reshuffling
            // the row every time the same two processes are down.
            var bestRank = rank(best)
            for p in down.dropFirst() where rank(p) > bestRank {
                best = p
                bestRank = rank(p)
            }
            return best
        }

        /// Why this process is not up, in one line, for a reader who can only be shown one.
        ///
        /// Its own last words when it left any; otherwise the stack's root cause, named. The
        /// second half is the part that matters: `web` is down and has nothing to say, and
        /// "build-web: bash: npm: command not found" is both true of `web` and the only useful
        /// thing anybody can tell it. Answering "no idea" there, when the document three lines
        /// up knows exactly, is the whole complaint this method exists to answer.
        func why(_ p: Process) -> String? {
            if let reason = p.reason { return reason }
            guard let cause = rootCause, cause.name != p.name, let reason = cause.reason
            else { return nil }
            return "\(cause.name): \(reason)"
        }
    }

    /// The `.devstack.json` itself.
    struct Spec: Equatable {
        /// The repository root the file was found in. Commands run here.
        var root: String
        var name: String
        var status: String?
        var up: String?
        var down: String?
        var restart: String?
        var logs: String?
        var attach: String?
        /// Tier 0: processes the file names outright, with the ports they listen on.
        ///
        /// This exists so that adopting the format costs four lines and no new software. A
        /// project that declares nothing but ports still gets a row in the panel — Clawdline
        /// probes the ports itself. Without this rung, the format is only usable by people who
        /// already run a supervisor, which is to say it is not a format, it is an integration.
        var declared: [Process]
        /// A hash of the file as read, so trust can be tied to the bytes that were trusted.
        var fingerprint: String
    }

    // MARK: - Finding it

    static let filename = ".devstack.json"

    /// Walk up from a session's directory looking for the file.
    ///
    /// Up rather than at the top level only: a monorepo can reasonably put one of these beside
    /// each deployable rather than one at the root. Stops at the home directory — above that is
    /// nobody's project, and reading `/` on every summon is a filesystem call for no reason.
    static func find(fromCwd cwd: String) -> Spec? {
        guard !cwd.isEmpty else { return nil }
        let home = FileManager.default.homeDirectoryForCurrentUser.standardizedFileURL.path
        var dir = URL(fileURLWithPath: cwd).standardizedFileURL
        while true {
            let candidate = dir.appendingPathComponent(filename)
            if let data = try? Data(contentsOf: candidate),
               let spec = parse(data, root: dir.path) {
                return spec
            }
            let parent = dir.deletingLastPathComponent().standardizedFileURL
            if parent.path == dir.path || dir.path == home || dir.path == "/" { return nil }
            dir = parent
        }
    }

    /// The pure half, so the format can be tested without a repository on disk.
    ///
    /// Every field except `name` is optional, and an unknown field is ignored rather than fatal.
    /// A reader that throws the whole file away because one key moved is worse than one that
    /// shows the parts it still recognises — the same rule ProjectStatus follows.
    static func parse(_ data: Data, root: String) -> Spec? {
        guard let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else {
            return nil
        }
        let name = (obj["name"] as? String)
            ?? URL(fileURLWithPath: root).lastPathComponent
        func cmd(_ key: String) -> String? {
            guard let v = obj[key] as? String, !v.trimmingCharacters(in: .whitespaces).isEmpty
            else { return nil }
            return v
        }
        var declared: [Process] = []
        for row in (obj["processes"] as? [[String: Any]] ?? []) {
            guard let n = row["name"] as? String, !n.isEmpty else { continue }
            declared.append(Process(name: n, state: "stopped",
                                    port: row["port"] as? Int,
                                    url: row["url"] as? String))
        }
        return Spec(root: root, name: name,
                    status: cmd("status"), up: cmd("up"), down: cmd("down"),
                    restart: cmd("restart"), logs: cmd("logs"), attach: cmd("attach"),
                    declared: declared,
                    fingerprint: fingerprint(data))
    }

    /// FNV-1a over the file's bytes. Not a security hash — it only has to notice that the file
    /// changed since the day you trusted it, and to do that without a CryptoKit import.
    static func fingerprint(_ data: Data) -> String {
        var h: UInt64 = 0xcbf29ce484222325
        for byte in data {
            h ^= UInt64(byte)
            h = h &* 0x100000001b3
        }
        return String(h, radix: 16)
    }

    // MARK: - Trust
    //
    // `.devstack.json` names commands, and Clawdline runs them. Cloning a repository must not be
    // enough to make that happen — this is the same exposure that made editors grow a "do you
    // trust this workspace" prompt, and it deserves the same answer.
    //
    // The split that keeps it usable: **reading declared ports needs no trust** (probing a TCP
    // port executes nothing), so an untrusted project still shows up in the panel with real
    // state. Only running one of its commands — including its `status` command — is gated.

    private static let trustFile = FileManager.default.homeDirectoryForCurrentUser
        .appendingPathComponent(".config/clawdline/trusted-stacks.json")

    private static func trustTable() -> [String: String] {
        guard let data = try? Data(contentsOf: trustFile),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: String]
        else { return [:] }
        return obj
    }

    /// Trusted only while the file is byte-for-byte the one that was trusted. An edit is a new
    /// decision — that is the whole point, since the edit is where a command would be added.
    static func isTrusted(_ spec: Spec) -> Bool {
        trustTable()[spec.root] == spec.fingerprint
    }

    static func trust(_ spec: Spec) {
        var table = trustTable()
        table[spec.root] = spec.fingerprint
        try? FileManager.default.createDirectory(
            at: trustFile.deletingLastPathComponent(), withIntermediateDirectories: true)
        if let data = try? JSONSerialization.data(withJSONObject: table, options: [.prettyPrinted]) {
            try? data.write(to: trustFile, options: .atomic)
        }
    }

    // MARK: - Reading state

    /// What the project's stack is doing, by the cheapest route it has made available.
    ///
    /// Tier 1/2 (a `status` command) when the project is trusted; Tier 0 (probe the declared
    /// ports) otherwise, or when there is no `status` command to run. Blocking, and meant for a
    /// background queue — it can spawn a subprocess.
    static func read(_ spec: Spec) -> State {
        if let status = spec.status, isTrusted(spec),
           let out = shell(status, cwd: spec.root, timeout: 6),
           let parsed = parseState(Data(out.utf8)) {
            return parsed
        }
        return probeDeclared(spec)
    }

    /// The state document, as `status` prints it (or as something else writes to a file).
    static func parseState(_ data: Data) -> State? {
        guard let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
        else { return nil }
        var procs: [Process] = []
        for row in (obj["processes"] as? [[String: Any]] ?? []) {
            guard let n = row["name"] as? String, !n.isEmpty else { continue }
            procs.append(Process(name: n,
                                 state: row["state"] as? String ?? "stopped",
                                 port: row["port"] as? Int,
                                 url: row["url"] as? String,
                                 pid: row["pid"] as? Int,
                                 since: row["since"] as? Double,
                                 exitCode: row["exit_code"] as? Int,
                                 error: row["error"] as? String))
        }
        // A document with no top-level verdict still has processes; derive it rather than
        // refusing the whole thing. Writers of these files are shell scripts, and a shell script
        // that forgot one key should still light up the dot.
        let derived = procs.isEmpty ? "stopped"
            : (procs.contains { $0.isDown } ? "partial"
               : (procs.allSatisfy { $0.isUp } ? "running" : "partial"))
        // **A top-level state is only believed if it is one of the four.**
        //
        // The two vocabularies share a key name — `state` on a process means `healthy`,
        // `running`, `starting`, `completed`, `exited` or `stopped`, and `state` on the document
        // means `running`, `partial`, `stopped` or `unknown` — so sending a process word at the
        // top is the predictable mistake, and it was made by the first project outside this
        // repository to write one of these. Taking it literally put a healthy stack under a mark
        // that said nobody had agreed to run it.
        //
        // Deriving instead is not a fallback, it is the better answer: it is computed from the
        // writer's own process list, so it cannot disagree with the rest of the document the way
        // a hand-written summary can.
        let top = obj["state"] as? String
        let believed = ["running", "partial", "stopped", "unknown"].contains(top ?? "") ? top! : derived
        return State(state: believed,
                     updatedAt: obj["updated_at"] as? Double ?? Date().timeIntervalSince1970,
                     processes: procs)
    }

    /// Tier 0: the ports the file declared, asked directly.
    ///
    /// "Something is listening on 8002" is a coarser truth than a readiness probe, but it is the
    /// truth for the overwhelming majority of dev stacks, and it costs the project four lines of
    /// JSON and no dependency.
    static func probeDeclared(_ spec: Spec) -> State {
        // Nothing declared and no trusted way to ask: say so. `stopped` here would be a
        // confident wrong answer about a project that is probably serving traffic.
        guard !spec.declared.isEmpty else {
            return State(state: "unknown", updatedAt: Date().timeIntervalSince1970, processes: [])
        }
        let procs = spec.declared.map { p -> Process in
            var out = p
            // No port to ask about: say so rather than guessing. `stopped` would be a lie and a
            // green dot would be a worse one.
            guard let port = p.port else { out.state = "stopped"; return out }
            out.state = isListening(port: port) ? "running" : "stopped"
            return out
        }
        let up = procs.filter { $0.isUp }.count
        return State(state: up == 0 ? "stopped" : (up == procs.count ? "running" : "partial"),
                     updatedAt: Date().timeIntervalSince1970,
                     processes: procs)
    }

    /// Is anything accepting connections on 127.0.0.1:port.
    ///
    /// A direct connect rather than shelling out to `lsof`: this runs for every declared port of
    /// every known project on a timer, and `lsof` is both slow and, on a busy Mac, alarmingly
    /// fond of scanning every open file on the system to answer.
    static func isListening(port: Int, timeoutMs: Int32 = 120) -> Bool {
        guard port > 0, port < 65536 else { return false }
        let fd = socket(AF_INET, SOCK_STREAM, 0)
        guard fd >= 0 else { return false }
        defer { close(fd) }

        var flags = fcntl(fd, F_GETFL, 0)
        flags |= O_NONBLOCK
        _ = fcntl(fd, F_SETFL, flags)

        var addr = sockaddr_in()
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = UInt16(port).bigEndian
        addr.sin_addr.s_addr = UInt32(0x7f00_0001).bigEndian

        let rc = withUnsafePointer(to: &addr) { p in
            p.withMemoryRebound(to: sockaddr.self, capacity: 1) { sa in
                connect(fd, sa, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        if rc == 0 { return true }
        guard errno == EINPROGRESS else { return false }

        var pfd = pollfd(fd: fd, events: Int16(POLLOUT), revents: 0)
        guard poll(&pfd, 1, timeoutMs) > 0 else { return false }
        var err: Int32 = 0
        var len = socklen_t(MemoryLayout<Int32>.size)
        guard getsockopt(fd, SOL_SOCKET, SO_ERROR, &err, &len) == 0 else { return false }
        return err == 0
    }

    // MARK: - Acting

    enum Action { case up, down, restart, logs, attach }

    /// Run one of the project's declared commands and report what happened.
    ///
    /// The command is expected to **finish only once the thing is actually up** — that is the
    /// contract's central demand, and the reason a restart here can report success or failure
    /// rather than "sent". A caller that gets `ok` may believe it.
    ///
    /// Blocking, minutes-long (a front-end build lives in here). Background queue only.
    @discardableResult
    static func run(_ spec: Spec, _ action: Action, process: String? = nil,
                    lines: Int = 200, timeout: Double = 600) -> (ok: Bool, output: String) {
        guard isTrusted(spec) else { return (false, "untrusted") }
        let template: String?
        switch action {
        case .up: template = spec.up
        case .down: template = spec.down
        case .restart: template = spec.restart
        case .logs: template = spec.logs
        case .attach: template = spec.attach
        }
        guard let template else { return (false, "unsupported") }
        let cmd = expand(template, process: process, lines: lines)
        guard let out = shell(cmd, cwd: spec.root, timeout: timeout,
                              wantStatus: true, interactive: true) else {
            return (false, "")
        }
        // shell() marks the exit status on the first line when asked; see below.
        let ok = out.hasPrefix("0\n")
        return (ok, String(out.drop(while: { $0 != "\n" }).dropFirst()))
    }

    /// `{process}` and `{lines}` in a declared command.
    ///
    /// Substituted, never concatenated: the project decides where its process name goes, because
    /// `make stack-restart P=api` and `overmind restart api` do not put it in the same place.
    /// An empty process collapses the whole `P={process}` word rather than leaving `P=` behind,
    /// which is how "restart everything" is spelled.
    static func expand(_ template: String, process: String?, lines: Int = 200) -> String {
        var out = template
        if let process, !process.isEmpty {
            out = out.replacingOccurrences(of: "{process}", with: shellQuoted(process))
        } else {
            // Drop any whitespace-delimited word that contained the placeholder.
            out = out.split(separator: " ", omittingEmptySubsequences: false)
                .filter { !$0.contains("{process}") }
                .joined(separator: " ")
        }
        return out.replacingOccurrences(of: "{lines}", with: String(lines))
            .trimmingCharacters(in: .whitespaces)
    }

    static func shellQuoted(_ s: String) -> String {
        "'" + s.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    // MARK: - Plumbing

    /// Run a command in the project's directory.
    ///
    /// Through a login shell, because these commands are the project's own — `make`,
    /// `process-compose`, `npm` — and an app inherits none of the PATH a terminal has. Without
    /// it every one of them fails as "command not found" on a machine where they all work.
    ///
    /// - Parameter interactive: also read the interactive rc file (`-i`). **This is where `npm`
    ///   lives, and a login shell alone does not find it.** zsh reads `.zshenv`, `.zprofile` and
    ///   `.zlogin` when it is a login shell, and `.zshrc` only when it is interactive — and
    ///   every version manager in common use installs itself into `.zshrc`, because that is what
    ///   nvm's own installer appends to. So `zsh -l -c 'which npm'` comes back empty on a
    ///   machine where `npm` has worked in every terminal for years.
    ///
    ///   That cost three projects here at once, and it cost them for days rather than for a
    ///   command: `up` does not merely run `npm`, it hands its environment to a supervisor that
    ///   then outlives the shell. Every process started under that daemon inherited the PATH
    ///   with no node in it, so `build-web` exited 127 with `bash: npm: command not found`, and
    ///   `web` — which waits on that build completing — exited silently behind it.
    ///
    ///   Not the default, because it is not free: an interactive shell sources a real rc file
    ///   and measured ~0.7s here against ~0.03s. Reading state runs on a timer for every known
    ///   project and stays on the cheap shell, which is enough for it — `make` and `python3` are
    ///   in the login PATH. Acting already takes a minute for a front-end build, so a second of
    ///   shell start-up is invisible there and buys the whole PATH.
    private static func shell(_ command: String, cwd: String, timeout: Double,
                              wantStatus: Bool = false, interactive: Bool = false) -> String? {
        let task = Foundation.Process()
        // The user's own shell, as a login shell, because these commands are the project's own —
        // `make`, `process-compose`, `npm` — and an app inherits none of the PATH a terminal has.
        // Without this every one of them fails as "command not found" on a machine where they
        // all work when typed. (This is the same trap Tmux.binary works around by hand.)
        let shellPath = ProcessInfo.processInfo.environment["SHELL"] ?? "/bin/sh"
        task.executableURL = URL(fileURLWithPath:
            FileManager.default.isExecutableFile(atPath: shellPath) ? shellPath : "/bin/sh")
        // With a status wanted, the exit code goes on the first line and the output follows —
        // one read, and no second channel to keep in sync.
        task.arguments = (interactive ? ["-i", "-l", "-c"] : ["-l", "-c"]) + [wantStatus
            ? "out=$(\(command) 2>&1); code=$?; printf '%s\\n%s' \"$code\" \"$out\""
            : command]
        task.currentDirectoryURL = URL(fileURLWithPath: cwd)
        let pipe = Pipe()
        task.standardOutput = pipe
        task.standardError = wantStatus ? pipe : Pipe()

        do { try task.run() } catch { return nil }

        // Read on a separate queue: a command that outputs more than the pipe buffer while we
        // wait on it deadlocks, and a front-end build outputs a great deal more than that.
        var data = Data()
        let lock = NSLock()
        let reader = DispatchQueue(label: "devstack.read")
        reader.async {
            let chunk = pipe.fileHandleForReading.readDataToEndOfFile()
            lock.lock(); data = chunk; lock.unlock()
        }

        let deadline = Date().addingTimeInterval(timeout)
        while task.isRunning, Date() < deadline { usleep(50_000) }
        if task.isRunning { task.terminate(); return nil }
        task.waitUntilExit()
        reader.sync {}
        lock.lock(); let out = data; lock.unlock()
        return String(data: out, encoding: .utf8)
    }
}
