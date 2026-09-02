import Foundation

/// Sending text into a tmux pane.
///
/// This is what makes every other terminal work. Terminal.app, Warp, Tabby and the rest
/// have no way to write into a running program's stdin — Terminal.app's `do script` sounds
/// like it should, but a program blocked on `read` in one of its tabs never receives a byte
/// of it. The only alternative would be synthetic keystrokes, which needs the accessibility
/// permission and needs the terminal in front: both of them defeat the point of this tool.
///
/// tmux sidesteps all of it: a pane can be written to whichever emulator happens to be
/// drawing it. It is also an ordinary subprocess rather than cross-app automation, so it
/// needs **no permission at all** — no accessibility, no automation prompt, nothing.
enum Tmux {

    static var binaryForTesting: String?
    static var subprocessTimeoutForTesting: TimeInterval?
    static private(set) var lastTimedOutPIDForTesting: pid_t?

    /// tmux is almost never on an app's PATH — apps do not inherit a login shell.
    /// Config can name it outright; otherwise try where package managers actually put it.
    static var binary: String? {
        if let binaryForTesting { return binaryForTesting }
        let configured = Config.shared.tmuxPath
        if !configured.isEmpty, FileManager.default.isExecutableFile(atPath: configured) {
            return configured
        }
        for path in ["/opt/homebrew/bin/tmux", "/usr/local/bin/tmux", "/usr/bin/tmux", "/opt/local/bin/tmux"]
        where FileManager.default.isExecutableFile(atPath: path) {
            return path
        }
        return nil
    }

    struct RunReceipt {
        let out: String
        let ok: Bool
        let failure: TerminalFailure?
    }

    private final class Sink { var data = Data() }

    /// Every tmux invocation is bounded and reaped. stdout and stderr are drained concurrently
    /// so a verbose failure cannot fill a pipe and turn the deadline into a deadlock.
    @discardableResult
    private static func run(_ args: [String], stdin: String? = nil,
                            timeout: TimeInterval? = nil) -> RunReceipt {
        guard let bin = binary else {
            return RunReceipt(out: "", ok: false,
                              failure: TerminalFailure(kind: .io, message: "tmux not found"))
        }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: bin)
        p.arguments = args
        let out = Pipe()
        let err = Pipe()
        p.standardOutput = out
        p.standardError = err
        let input = stdin.map { _ in Pipe() }
        if let input { p.standardInput = input }

        let stdout = Sink(), stderr = Sink()
        let readers = DispatchGroup()
        func drain(_ handle: FileHandle, into sink: Sink) {
            readers.enter()
            let reader = Thread {
                sink.data = handle.readDataToEndOfFile()
                readers.leave()
            }
            reader.stackSize = 64 * 1024
            reader.start()
        }
        drain(out.fileHandleForReading, into: stdout)
        drain(err.fileHandleForReading, into: stderr)

        let exited = DispatchSemaphore(value: 0)
        p.terminationHandler = { _ in exited.signal() }
        do { try p.run() } catch {
            input?.fileHandleForWriting.closeFile()
            out.fileHandleForWriting.closeFile()
            err.fileHandleForWriting.closeFile()
            _ = readers.wait(timeout: .now() + 1)
            return RunReceipt(out: "", ok: false,
                              failure: TerminalFailure(kind: .io,
                                                       message: error.localizedDescription))
        }
        if let stdin {
            input?.fileHandleForWriting.write(Data(stdin.utf8))
        }
        input?.fileHandleForWriting.closeFile()

        let limit = timeout ?? subprocessTimeoutForTesting ?? 15
        if exited.wait(timeout: .now() + limit) == .timedOut {
            let pid = p.processIdentifier
            lastTimedOutPIDForTesting = pid
            p.terminate()
            if exited.wait(timeout: .now() + 0.25) == .timedOut {
                kill(pid, SIGKILL)
                _ = exited.wait(timeout: .now() + 1)
            }
            _ = readers.wait(timeout: .now() + 1)
            return RunReceipt(out: String(data: stdout.data, encoding: .utf8) ?? "",
                              ok: false,
                              failure: TerminalFailure(
                                kind: .timeout,
                                message: "tmux did not finish within \(limit) seconds"))
        }
        _ = readers.wait(timeout: .now() + 1)
        let output = String(data: stdout.data, encoding: .utf8) ?? ""
        let error = String(data: stderr.data, encoding: .utf8)?
            .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let ok = p.terminationStatus == 0
        return RunReceipt(out: output, ok: ok,
                          failure: ok ? nil : TerminalFailure(
                            kind: .io, message: error.isEmpty ? "tmux command failed" : error))
    }

    static func runForTesting(_ args: [String], timeout: TimeInterval) -> RunReceipt {
        run(args, timeout: timeout)
    }

    /// What a failed tmux command says about the server behind it — three answers, not a flag.
    ///
    /// The difference decides what an empty answer is worth everywhere on this path: *no server*
    /// is a complete inventory that happens to hold nothing, while a timeout or an I/O failure
    /// has no authority to prove a pane, a client or a screen absent. Three readers ask it, and
    /// they asked it as three copies of the same pair of substrings until this was pulled out.
    ///
    /// **The vocabulary is tmux's, so this reads tmux's distinction rather than collecting its
    /// sentences** — which is the whole of why a pair of substrings was the wrong shape. Measured
    /// on tmux 3.6a, 2026-09-02, and `list-sessions`, `list-panes -a`, `list-clients` and
    /// `source-file -` all answer identically:
    ///
    /// | the socket | what tmux prints |
    /// |---|---|
    /// | a server ran and was killed — tmux leaves the file behind | `no server running on <path>` |
    /// | no file: tmux has never run here | `error connecting to <path> (No such file or directory)` |
    /// | the file is there and will not open | `error connecting to <path> (Permission denied)` |
    ///
    /// The last two are the *same sentence*, and taking it as a third substring would have made
    /// an unreadable socket say the server is absent — the distinction ``paneObservation()``
    /// exists to protect. What separates them is the parenthesised half, which is
    /// `strerror(errno)` from tmux's own `connect(2)`: `ENOENT` is the strongest proof of absence
    /// there is, because a socket file that does not exist has never had a server on it, and
    /// every other errno is a socket this process could not get to.
    ///
    /// So the rule is stated once, in one place, and an unrecognised message falls to
    /// ``ServerAnswer/reached`` — tmux answering about the command rather than about the server —
    /// which is the reading that lets no caller claim anything it has not been told.
    enum ServerAnswer: Equatable {
        /// tmux got as far as the socket path and there is no server on it. A complete empty
        /// inventory: nothing to list, and nothing a second attempt would find.
        case noServer
        /// tmux could not get to a server at all, and cannot say whether one is there. Not
        /// evidence, and never a reason to go and do the same work again a slower way.
        case unreachable
        /// tmux was reached. Whatever went wrong is about the command that was sent.
        case reached
    }

    static func serverAnswer(_ failure: TerminalFailure?) -> ServerAnswer {
        guard let failure else { return .reached }
        // A deadline is not an answer: a tmux that never finished has said nothing about a
        // server, a pane, a client or a screen.
        if failure.kind == .timeout { return .unreachable }
        let message = failure.message.lowercased()
        // tmux resolved the question itself and is reporting the conclusion.
        if message.contains("no server running") { return .noServer }
        if message.contains("failed to connect to server") { return .noServer }
        // `error connecting to <path> (<strerror>)` — only the errno decides.
        if message.contains("error connecting to") {
            return message.contains("(no such file or directory)") ? .noServer : .unreachable
        }
        return .reached
    }

    /// Every pane in every session, whether or not a client is attached.
    /// `-a` matters: a detached session is still somewhere text can usefully go.
    struct PaneObservation {
        let sessions: [TargetSession]
        let error: TerminalFailure?
        var isComplete: Bool { error == nil }
    }

    static func paneObservation() -> PaneObservation {
        guard binary != nil else { return PaneObservation(sessions: [], error: nil) }
        let fmt = "#{pane_id}\u{1}#{pane_tty}\u{1}#{pane_current_command}\u{1}"
            + "#{session_name}\u{1}#{window_index}\u{1}#{pane_index}\u{1}#{pane_title}"
            + "\u{1}#{pane_current_path}"
        let receipt = run(["list-panes", "-a", "-F", fmt])
        if !receipt.ok {
            // No server is a complete empty inventory, not a failed one. Other failures — and
            // especially a timeout — have no authority to prove a pane absent.
            if serverAnswer(receipt.failure) == .noServer {
                return PaneObservation(sessions: [], error: nil)
            }
            return PaneObservation(sessions: [], error: receipt.failure)
        }
        return PaneObservation(sessions: parsePanes(receipt.out, running: ITerm.assistantPIDs()),
                               error: nil)
    }

    static func panes() -> [TargetSession] {
        paneObservation().sessions
    }

    /// Split out from `panes()` so it can be tested without a tmux server running.
    /// The parsing is where the bugs live; running the binary is not.
    ///
    /// `running` is the second opinion, and on the current installers it is the only one that is
    /// right. tmux reports the *process name* the kernel holds, which is the basename of the
    /// executable — and `claude` is a symlink onto `~/.local/share/claude/versions/2.1.233`, so
    /// the pane running Claude Code announces itself as `2.1.233`. Every tmux session was
    /// therefore listed as an ordinary shell: the one path the README promises for Terminal.app,
    /// Ghostty, Warp and the rest, finding nothing to send to. `ps` reads argv, which still says
    /// `claude`, so the tty is asked as well as the name.
    ///
    /// Codex is the same story from the other end: it ships as a Node shim, so the pane's process
    /// name is `node` and the tty is the only thing that knows better. The name is still asked
    /// first because it costs nothing and is right for a native install of either.
    static func parsePanes(_ output: String,
                           running: [String: Assistant.Running] = [:]) -> [TargetSession] {
        output.split(separator: "\n").compactMap { line in
            let f = line.components(separatedBy: "\u{1}")
            guard f.count >= 7 else { return nil }
            let command = f[2]
            let title = f[6].trimmingCharacters(in: .whitespaces)
            let coords = "\(f[3]):\(f[4]).\(f[5])"
            // A pane title is whatever the program set; Claude Code puts the task there.
            // Fall back to tmux coordinates, which at least say where it is.
            let name = title.isEmpty || title == command ? coords : title
            return TargetSession(
                backend: .tmux,
                id: f[0],                       // %12, stable for the life of the pane
                name: name,
                tty: f[1],
                windowIndex: Int(f[4]) ?? 0,
                tabIndex: Int(f[5]) ?? 0,
                assistant: Assistant.named(command)
                    ?? running[f[1].replacingOccurrences(of: "/dev/", with: "")]?.assistant,
                cwd: f.count > 7 ? f[7] : nil
            )
        }
    }

    // MARK: - Control mode

    /// One tmux client that is speaking control mode — the protocol iTerm2 uses when you start
    /// it with `tmux -CC`.
    struct ControlModeClient: Equatable {
        /// The pty tmux is speaking the control-mode protocol over. It is the *gateway's* tty —
        /// an ordinary iTerm2 session with a real `/dev/ttys006` — and never the tty of any pane
        /// the emulator is drawing on its behalf.
        let tty: String
        /// The tmux session this client is attached to. What makes a per-pane answer possible in
        /// ``shouldActivateITerm(paneSession:controlModeClients:)`` instead of a machine-wide one.
        let session: String
        /// How many windows that session holds — the number of tabs this client is drawing, and
        /// so the ceiling on how many pty-less iTerm2 rows it can account for. `nil` when tmux
        /// did not say; see ``ITerm/drawnWindowCeiling(_:)`` for what an unsized client is worth.
        let sessionWindows: Int?
    }

    /// Whether tmux agrees that something on this Mac is drawing its windows through control
    /// mode, and which sessions those clients are attached to.
    ///
    /// **This exists to be a second source, and the distinction it keeps is the point.** An
    /// iTerm2 row with an identity but no pty is either a tmux window iTerm2 is drawing, or an
    /// anomaly nobody can explain — and the row itself cannot tell you which, because iTerm2's
    /// scripting exposes no tmux property at all. Asking tmux is asking something that was not
    /// involved in producing the first reading. `error` is kept separate from an empty list for
    /// the same reason ``PaneObservation`` keeps it: *tmux says there is no control-mode client*
    /// and *tmux could not be asked* are two different answers, and only the first is evidence.
    struct ControlModeObservation {
        let clients: [ControlModeClient]
        let error: TerminalFailure?
        var isComplete: Bool { error == nil }
    }

    /// What ``controlModeObservation()`` answers when there is no tmux binary to ask.
    ///
    /// **Not an empty answer.** ``binary`` is `Config.shared.tmuxPath` plus four fixed install
    /// paths, so `nil` means *this app cannot find a tmux*, which is not the same fact as *this
    /// Mac has no tmux*: a `-CC` session starts from whatever tmux is on the person's PATH, and
    /// a checkout in `~/bin` is invisible here. Spelling that as `clients: [], error: nil` made
    /// the panel say "tmux reports no control-mode client that would explain it" about a tmux
    /// that was never asked, and sent whoever read it to look at tmux instead of at `tmuxPath`.
    /// The classification is unchanged — an unaskable source has not agreed, so the row is still
    /// unexplained — and the difference is the sentence a person gets.
    ///
    /// ``paneObservation()`` deliberately keeps the other convention: no tmux binary there is a
    /// complete empty pane list, because every Mac without tmux would otherwise carry a
    /// permanently incomplete inventory, which is the close-refusing defect this line exists to
    /// remove. This one is only consulted when there is already a pty-less row to explain.
    static func controlModeObservationWithoutBinary() -> ControlModeObservation {
        ControlModeObservation(
            clients: [],
            error: TerminalFailure(
                kind: .io,
                message: "no tmux found at Config's tmuxPath or the usual install paths"))
    }

    static func controlModeObservation() -> ControlModeObservation {
        guard binary != nil else { return controlModeObservationWithoutBinary() }
        let fmt = "#{client_tty}\u{1}#{client_flags}\u{1}#{client_session}\u{1}#{session_windows}"
        // A deadline of its own, well under the 15 s every other tmux call gets. This question is
        // asked *inside* ``ITerm/snapshot(processScan:)``, which is itself inside a reading that
        // runs every 1.2 s and suppresses the next one while it is in flight; a wedged server
        // would otherwise add its whole 15 s to the 15 s ``paneObservation()`` can already spend.
        // Measured healthy cost on tmux 3.6a: `real 0.00`, five times out of five.
        let receipt = run(["list-clients", "-F", fmt], timeout: subprocessTimeoutForTesting ?? 5)
        if !receipt.ok {
            // Same rule as `paneObservation`: no server is a complete empty answer, and anything
            // else — a timeout above all — has no authority to prove a client absent.
            if serverAnswer(receipt.failure) == .noServer {
                return ControlModeObservation(clients: [], error: nil)
            }
            return ControlModeObservation(clients: [], error: receipt.failure)
        }
        return ControlModeObservation(clients: parseControlModeClients(receipt.out), error: nil)
    }

    /// Split out from ``controlModeObservation()`` for the same reason ``parsePanes(_:running:)``
    /// is split out of ``panes()``: the parsing is where the bugs live, and it can be exercised
    /// with no tmux server running and no `-CC` session open on somebody's real desktop.
    ///
    /// The flag list measured on this Mac against tmux 3.6a, from a live iTerm2 control-mode
    /// client, is `attached,focused,control-mode,wait-exit,pause-after=0,UTF-8`. It is split on
    /// commas and compared whole rather than searched as a substring: `#{client_flags}` is an
    /// open vocabulary that tmux adds to between releases, and a future flag that merely
    /// *contains* these letters must not be read as this one.
    ///
    /// `#{session_windows}` rides along in the same read — measured
    /// `flags=attached,focused,control-mode,UTF-8 session=work windows=2` — because a client that
    /// says how many windows it is drawing is the only thing on this path that can put a ceiling
    /// on how many pty-less rows it is allowed to explain. A tmux old enough not to answer it
    /// leaves the field absent rather than the row unusable.
    static func parseControlModeClients(_ output: String) -> [ControlModeClient] {
        output.split(separator: "\n").compactMap { line in
            let f = line.components(separatedBy: "\u{1}")
            guard f.count >= 2 else { return nil }
            let flags = f[1].split(separator: ",").map {
                $0.trimmingCharacters(in: .whitespaces)
            }
            guard flags.contains("control-mode") else { return nil }
            return ControlModeClient(
                tty: f[0].trimmingCharacters(in: .whitespaces),
                session: f.count > 2 ? f[2] : "",
                sessionWindows: f.count > 3
                    ? Int(f[3].trimmingCharacters(in: .whitespaces))
                    : nil)
        }
    }

    /// Which tmux session a pane belongs to, or nothing when tmux would not say.
    ///
    /// One `display-message` rather than a second `list-panes -a`: the answer wanted here is one
    /// field about one pane, and this is on the path of a keypress somebody is waiting on.
    static func sessionName(ofPane paneID: String) -> String? {
        guard binary != nil else { return nil }
        let receipt = run(["display-message", "-p", "-t", paneID, "#{session_name}"])
        guard receipt.ok else { return nil }
        let name = receipt.out.trimmingCharacters(in: .whitespacesAndNewlines)
        return name.isEmpty ? nil : name
    }

    /// Whether selecting this pane should also bring iTerm2 to the front.
    ///
    /// **Both facts come from tmux**, which is what makes this answerable at all. A control-mode
    /// client is attached to one tmux *session*, and a pane belongs to one — so a pane in the
    /// session iTerm2 is drawing gets the application brought forward, and a pane in a session
    /// Ghostty or Terminal.app is attached to does not, even while an iTerm2 `-CC` client exists
    /// somewhere else on the same server. Nothing here guesses at which iTerm2 *tab* holds the
    /// pane: iTerm2's scripting dictionary carries no tmux property, so there is no supported
    /// mapping and a name-matched one would be a fragile invention.
    ///
    /// An unknown pane session is a no. Bringing the wrong application forward takes somebody's
    /// keyboard away from what they were typing into, which is the failure this whole app exists
    /// not to commit.
    static func shouldActivateITerm(paneSession: String?,
                                    controlModeClients: [ControlModeClient]) -> Bool {
        guard let paneSession, !paneSession.isEmpty else { return false }
        return controlModeClients.contains { $0.session == paneSession }
    }

    /// Bracketed paste through a tmux buffer, then a separate Enter — the same shape as the
    /// iTerm2 path, for the same reason: without the wrapper a two-line prompt submits itself
    /// after the first line.
    ///
    /// The markers go in by hand rather than with `paste-buffer -p`. That flag only inserts
    /// them "if the application has requested bracketed paste mode", and a pane where it has
    /// not silently turns every newline back into a Return — which was measurable: two lines
    /// pasted into a shell ran as two commands. Sending the bytes outright makes tmux behave
    /// exactly like the iTerm2 path instead of almost like it.
    /// The tmux session a server Clawdline started for itself is given, so that a person who was
    /// not at the Mac when it happened has a name to attach to rather than a numbered stranger.
    static let startedSessionName = "clawdline"

    /// What somebody at the Mac types to see a session Clawdline started for them. One string
    /// rather than a sentence spelled out wherever it is needed, because the session name and the
    /// instruction have to stay the same fact.
    static var attachCommand: String { "tmux attach -t \(startedSessionName)" }

    /// A new window in the first session tmux has, running something.
    ///
    /// `-P -F '#{pane_id}'` so the pane it made comes back rather than having to be guessed at by
    /// listing everything again and looking for what is new — which would be a race against
    /// anybody else's window opening at the same moment.
    static func newWindowResult(cwd: String, command: String) -> Result<String, TerminalFailure> {
        openPane(["new-window", "-P", "-F", "#{pane_id}"], cwd: cwd, command: command,
                 refused: "tmux would not open a window.")
    }

    static func newWindow(cwd: String, command: String) -> String? {
        try? newWindowResult(cwd: cwd, command: command).get()
    }

    /// Start a tmux **server** and open the first window on it, with nothing attached.
    ///
    /// The pane is real, reachable and drivable the moment it exists — ``paneObservation()`` asks
    /// `list-panes -a` precisely so a detached session counts — so it reaches the panel and the
    /// phone as an ordinary row. What it is not is *drawn* anywhere: nobody at the Mac sees it
    /// until they type ``attachCommand``. That trade is the point, and it is only worth making
    /// where the alternative is a refusal the person cannot act on; see ``StartPoints/plan``.
    static func newSessionResult(cwd: String, command: String) -> Result<String, TerminalFailure> {
        openPane(["new-session", "-d", "-s", startedSessionName, "-P", "-F", "#{pane_id}"],
                 cwd: cwd, command: command,
                 refused: "tmux would not start a server.")
    }

    /// Make a pane and start the assistant in it, in that order and never in one step.
    ///
    /// **The command is typed into a login shell rather than handed to tmux as the pane's
    /// command, and that is not a style choice.** A pane started as `new-window <command>` is run
    /// by `sh -c` with the *server's* environment, and a server started by this app inherits the
    /// app's — an app launched from Finder has no login shell behind it and therefore no `PATH`
    /// worth reading, which ``Assistant/isInstalled`` already says out loud. Measured on this Mac,
    /// 2026-09-02, starting tmux with a Finder-shaped environment: `new-session -d 'claude …'`
    /// draws `zsh:1: command not found: claude`, `show-environment -g PATH` reads back the app's
    /// four system directories, and every later `new-window 'claude …'` on that server fails the
    /// same way. Wrapping it as `zsh -lc` does not save it either — a login shell that is not
    /// interactive never reads `.zshrc`, which is where the person's `PATH` is actually set.
    ///
    /// What does work, measured in the same run, is tmux's own default: a pane created with no
    /// command at all gets an interactive login shell, which reads that file and finds `claude`.
    /// So the line is typed at it, which is exactly what the iTerm2 backend has always done —
    /// ``ITerm/newTabResult(line:)`` opens a tab and types one line into it — and typing it the
    /// instant the pane exists is safe because the pty holds what is written before the shell
    /// gets round to reading it.
    ///
    /// A pane whose keystrokes tmux would not take is reported as a failure. It leaves a shell
    /// behind, which the person can close from the same list it appears in; saying a session
    /// started when tmux would not even type into it would be the worse of the two.
    ///
    /// **What comes back says the keystrokes were accepted, and that is the most it can say.**
    /// `send-keys` reports that tmux delivered them to the pane's tty, never that the shell ran
    /// them, and those are two different failures. An rc file that flushes pending input —
    /// `tcsetattr(0, TCSAFLUSH, …)`, which is what `stty` and a few framework rc files do —
    /// discards a line already sitting in the tty while both calls still exit 0. Measured on
    /// tmux 3.6a, 2026-09-02, against a `.zshrc` doing exactly that: the pane came back holding
    /// its prompt and nothing else.
    ///
    /// **There is no receipt here because none can be taken in the time this path has.** A
    /// `.zshrc` that merely sleeps for three seconds does *not* lose the line — type-ahead
    /// survives an ordinary slow rc, measured the same day — so inside any deadline somebody
    /// waiting on a start can afford, a swallowed line and a slow shell are the same silence, and
    /// a check would either wait longer than the start is worth or call a working session dead.
    /// ``ITerm/newTabResult(line:)`` has the identical exposure, and for the identical reason.
    /// What catches it afterwards is that the pane never reports an assistant: an orchestrator
    /// task fails its `child.assistant == task.assistant` gate and ends `spawn_failed` rather
    /// than claiming a session that is not there, and a session started by hand appears in the
    /// list as the shell it actually is.
    private static func openPane(_ create: [String], cwd: String, command: String,
                                 refused: String) -> Result<String, TerminalFailure> {
        var args = create
        if !cwd.isEmpty { args += ["-c", cwd] }
        let result = run(args)
        guard result.ok else {
            return .failure(result.failure ?? TerminalFailure(kind: .io, message: refused))
        }
        let id = result.out.trimmingCharacters(in: .whitespacesAndNewlines)
        guard id.hasPrefix("%") else {
            return .failure(TerminalFailure(kind: .io,
                                            message: "tmux returned no new pane id."))
        }
        if let failure = typeLine(command.isEmpty ? Assistant.claude.command : command, into: id) {
            return .failure(failure)
        }
        return .success(id)
    }

    /// Type one line at a pane's shell and run it.
    ///
    /// `-l` sends the string literally, so tmux looks for no key names inside it; the line is one
    /// argument of a subprocess either way, so nothing here is a quoting boundary the way a shell
    /// line would be. Enter is its own keypress for the same reason it is in ``send(_:to:submit:)``.
    private static func typeLine(_ line: String, into paneID: String) -> TerminalFailure? {
        let typed = run(["send-keys", "-t", paneID, "-l", line])
        guard typed.ok else {
            return typed.failure ?? TerminalFailure(
                kind: .io, message: "tmux would not type into the pane it just made.")
        }
        let entered = run(["send-keys", "-t", paneID, "Enter"])
        guard entered.ok else {
            return entered.failure ?? TerminalFailure(
                kind: .io, message: "tmux typed the line but Enter did not land.")
        }
        return nil
    }

    static func send(_ text: String, to paneID: String,
                     submit shouldSubmit: Bool = true) -> String? {
        guard binary != nil else { return "tmux not found — set \"tmux_path\" in the config" }
        let buffer = "clawdline"
        let pasteStart = ["1b", "5b", "32", "30", "30", "7e"]   // ESC [ 2 0 0 ~
        let pasteEnd = ["1b", "5b", "32", "30", "31", "7e"]     // ESC [ 2 0 1 ~

        guard run(["send-keys", "-t", paneID, "-H"] + pasteStart).ok else {
            return "that tmux pane is gone"
        }
        guard run(["load-buffer", "-b", buffer, "-"], stdin: text).ok else {
            return "tmux would not take the text"
        }
        guard run(["paste-buffer", "-d", "-b", buffer, "-t", paneID]).ok else {
            return "that tmux pane is gone"
        }
        run(["send-keys", "-t", paneID, "-H"] + pasteEnd)
        usleep(60_000)
        guard shouldSubmit else { return nil }
        return submit(paneID)
    }

    /// Raw key bytes, as one keypress rather than as text.
    ///
    /// Outside the bracketed paste on purpose: inside one, a control byte is a character being
    /// handed to the program, not a key it is being asked about — and the whole point of sending
    /// 0x16 is that the far side treats it as Ctrl-V. `-H` takes hex, which says the bytes
    /// without tmux looking for key names. One `send-keys` keeps a multi-byte terminal sequence
    /// together; separate processes would leave room for somebody else's input between them.
    static func keystroke(_ bytes: [UInt8], to paneID: String) -> String? {
        guard binary != nil else { return "tmux not found — set \"tmux_path\" in the config" }
        let hex = bytes.map { String(format: "%02x", $0) }
        return run(["send-keys", "-t", paneID, "-H"] + hex).ok ? nil : "that tmux pane is gone"
    }

    static func keystroke(_ byte: UInt8, to paneID: String) -> String? {
        keystroke([byte], to: paneID)
    }

    /// Close a pane. The tmux half of ending a session from somewhere you cannot see it.
    ///
    /// `kill-pane`, not `kill-window`: a window can hold several panes and the others belong to
    /// work nobody asked about. When the pane was the only one, tmux removes the window itself,
    /// which is what somebody pressing this expects.
    static func close(_ paneID: String) -> String? {
        return run(["kill-pane", "-t", paneID]).ok ? nil : "that tmux pane is gone"
    }

    static func submit(_ paneID: String) -> String? {
        run(["send-keys", "-t", paneID, "Enter"]).ok ? nil : "pasted, but Enter did not land"
    }

    /// What that pane shows, plus some scrollback. tmux keeps history that iTerm2's
    /// AppleScript will not hand over, so this path can offer more than the visible screen.
    static func capture(_ paneID: String, scrollback: Int = 200) -> String? {
        guard binary != nil else { return nil }
        let receipt = run(captureArguments(paneID, scrollback: scrollback))
        return receipt.ok ? receipt.out : nil
    }

    /// One pane's capture, as arguments. `-e` keeps the escape sequences, which is the only way
    /// any of this arrives with colour; ``Activity/parse(_:tailLines:)`` strips them again and
    /// says why it has to.
    private static func captureArguments(_ paneID: String, scrollback: Int) -> [String] {
        ["capture-pane", "-p", "-e", "-J", "-S", "-\(scrollback)", "-t", paneID]
    }

    // MARK: - One subprocess per reading

    /// The line a batched reading prints before each pane's screen, so the answers can be told
    /// apart afterwards.
    ///
    /// **The pane id comes from `#{pane_id}` and never from the id being written out.**
    /// `display-message` runs its argument through `strftime`, so a marker built by hand out of
    /// `%0` arrives as `0` with the `%` eaten — measured on tmux 3.6a, 2026-09-02. Asking tmux to
    /// resolve `-t` itself also answers the harder question for free: a target it cannot find
    /// prints an **empty** id rather than the current pane's, so a dead pane cannot quietly take
    /// a live one's screen.
    ///
    /// U+0001 delimits it for the same reason ``parsePanes(_:running:)`` separates fields with it:
    /// a captured screen is made of cells, and a control byte is not one, so nothing a program
    /// can draw collides with the marker. It wraps the id on **both** sides, so a marker line is
    /// recognised by what it opens and closes with rather than by a single leading needle.
    static let batchedCaptureMarker = "\u{1}clawdline-pane\u{1}"

    /// Whether this is a pane id tmux handed out, and therefore a word that may be written into a
    /// command script. Closed on purpose, like ``StartPoints/sessionName(_:)``: everything asked
    /// about here came back out of `list-panes` seconds earlier, so there is no shape to be
    /// generous about, and a batched reading is one place where a stray argument would be read as
    /// a command rather than as a target.
    static func isPaneID(_ id: String) -> Bool {
        guard id.hasPrefix("%") else { return false }
        let digits = id.dropFirst()
        return !digits.isEmpty && digits.allSatisfy { ("0"..."9").contains($0) }
    }

    /// The commands one batched reading sends to tmux, one pane at a time down a single script.
    ///
    /// Split out from ``capture(panes:scrollback:)`` for the reason every other parser here is:
    /// it can be read, and proved, with no tmux server running.
    static func batchedCaptureScript(_ paneIDs: [String], scrollback: Int) -> String {
        let lines = paneIDs.filter(isPaneID).flatMap { id in
            ["display-message -p -t \(id) \"\(batchedCaptureMarker)#{pane_id}\(batchedCaptureMarker)\"",
             captureArguments(id, scrollback: scrollback).joined(separator: " ")]
        }
        guard !lines.isEmpty else { return "" }
        return lines.joined(separator: "\n") + "\n"
    }

    /// One batched reading's output, split back into a screen per pane.
    ///
    /// **A section with no id is dropped, and an empty one is no answer.** Those are the two
    /// shapes a pane that would not answer leaves behind: tmux prints the marker with an empty
    /// `#{pane_id}` and then prints nothing at all for the capture. A live pane always prints its
    /// full height — trailing blank rows included, measured — so "the marker is there and nothing
    /// follows it" is a failure and never a blank screen.
    ///
    /// The first section wins when an id appears twice, so a marker that somehow resolved onto a
    /// pane already read cannot overwrite that pane's real screen with an empty one.
    static func parseBatchedCapture(_ output: String) -> [String: String] {
        var screens: [String: String] = [:]
        var current: String?
        var lines: [String] = []

        func close() {
            defer { current = nil; lines = [] }
            guard let id = current, !id.isEmpty, !lines.isEmpty, screens[id] == nil else { return }
            screens[id] = lines.joined(separator: "\n") + "\n"
        }

        var text = output
        if text.hasSuffix("\n") { text.removeLast() }
        let width = batchedCaptureMarker.count
        for line in text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init) {
            // The marker wraps the id on both sides, so the length test is what keeps a bare
            // marker — which satisfies both ends at once — from being read as a named pane.
            if line.count >= 2 * width,
               line.hasPrefix(batchedCaptureMarker), line.hasSuffix(batchedCaptureMarker) {
                close()
                current = String(line.dropFirst(width).dropLast(width))
            } else if current != nil {
                lines.append(line)
            }
        }
        close()
        return screens
    }

    /// What several panes show, keyed by pane id, in **one** tmux invocation rather than one per
    /// pane. Panes that would not answer are simply absent.
    ///
    /// A reading is one round trip to every terminal, it runs every 1.2 s while the panel is
    /// open, and a reading in progress suppresses the next one — so this cost does not queue up,
    /// it lowers the rate at which the app perceives anything (`docs/waiting.md`). iTerm2 has
    /// answered for all of its sessions in one `osascript` run since the beginning; tmux was
    /// asked pane by pane because `capture-pane` has no plural, which is true and was never the
    /// whole story: tmux takes a whole script at once. Measured on tmux 3.6a with ten panes,
    /// 2026-09-02: one process, median 3.45 ms, against ten processes and 32.01 ms.
    ///
    /// **`source-file -` rather than a `;`-separated argument list, and that is the whole of the
    /// failure semantics.** A command list given as arguments stops dead at the first error — a
    /// single pane that has gone away takes every pane after it with it, measured — while
    /// `source-file` runs the rest and reports the failure in its exit status. So one unanswering
    /// pane costs one missing key, exactly as it did when each pane had its own subprocess.
    ///
    /// The receipt's status is therefore *not* the gate: a batch that half-worked is still the
    /// answer for the panes that did work, and the output is parsed whether tmux was happy or
    /// not.
    static func capture(panes paneIDs: [String], scrollback: Int = 0) -> [String: String] {
        guard binary != nil else { return [:] }
        let script = batchedCaptureScript(paneIDs, scrollback: scrollback)
        guard !script.isEmpty else { return [:] }
        let receipt = run(["source-file", "-"], stdin: script)
        let screens = parseBatchedCapture(receipt.out)
        // A marker anywhere is proof this tmux understood the script, so an empty result is then
        // a real answer about the panes. No marker at all is a tmux that could not be asked this
        // way — `source-file -` wants 3.3 or newer — and going blind on the whole backend is a
        // far worse trade than the round trips this exists to save.
        if !screens.isEmpty || receipt.out.contains(batchedCaptureMarker) { return screens }
        // **Only a tmux that was actually reached can be too old.** The version floor is read off
        // the *absence* of the marker, and three different things are absent in the same way: an
        // old tmux, a server that was never reached, and a run that timed out with nothing to
        // show for it. Asking the last two again pane by pane cannot answer anything the batch
        // did not — and on a wedged server it turns one 15-second reading into one per pane, on
        // the path this exists to keep fast, where a reading in progress suppresses the next one
        // rather than queueing it (`docs/waiting.md`). So the type of the failure decides,
        // instead of being inferred from the same silence the fallback is looking at.
        guard serverAnswer(receipt.failure) == .reached else { return [:] }
        var out: [String: String] = [:]
        for id in paneIDs where isPaneID(id) {
            if let screen = capture(id, scrollback: scrollback) { out[id] = screen }
        }
        return out
    }

    /// Bring a pane to the front within tmux. Whether the terminal window itself comes
    /// forward is up to whichever emulator is drawing it, and not something to chase.
    ///
    /// **There is one emulator where a piece of it can be chased, and only one.** Under
    /// `tmux -CC` iTerm2 follows tmux's window selection — measured: asking iTerm2 for its
    /// current session id before and after a `select-window` returned two different ids, matching
    /// the two tmux windows. What it does not do is bring iTerm2's window forward when another
    /// application is frontmost, so the last step is asked for at the application level. That is
    /// as far as this goes on purpose: selecting the right *tab* would need a pane-to-session
    /// mapping iTerm2 does not publish.
    ///
    /// `activate: false` stops before that, the same as ``ITerm/reveal(_:activate:)``: the prompt
    /// bar walks its list with the terminal following underneath, and a terminal that jumped in
    /// front on every press would take the keyboard away from the box being typed into.
    @discardableResult
    static func reveal(_ paneID: String, activate: Bool = true) -> TerminalFailure? {
        let pane = run(["select-pane", "-t", paneID])
        guard pane.ok else { return pane.failure }
        let window = run(["select-window", "-t", paneID])
        guard window.ok else { return window.failure }
        guard activate else { return nil }
        // **The selection is what was asked for; coming forward is a courtesy, and it leaves
        // this thread.** Three of the four callers are on the main thread —
        // ``NotchIsland`` `jump(_:)` and `reveal()`, and `main.swift`'s `revealTarget()` through
        // ``Controller/revealCurrentTarget()`` — and the tail below is up to four round trips
        // with a deadline each: `list-clients`, `display-message`, and two Apple Events. Waiting
        // for them on main is what ``Controller/follow(_:)`` refuses to do "because this is an
        // osascript round trip", and what `/focus` in ``RemoteServer`` refuses because "a modal
        // must not freeze SessionWatch, the orchestrator beat, SSE, or health responses".
        // Nothing is lost by not waiting: the tail's only outcome is discarded either way.
        let tail = { activateITerm2(forPane: paneID) }
        if let dispatch = revealActivationDispatchForTesting {
            dispatch(tail)
        } else {
            DispatchQueue.global(qos: .utility).async(execute: tail)
        }
        return nil
    }

    /// Where ``reveal(_:activate:)`` hands its activation tail. Production is a background queue;
    /// a test holds the block instead of running it, because what has to be proved is that
    /// `reveal` hands it on rather than waiting for it.
    static var revealActivationDispatchForTesting: ((@escaping () -> Void) -> Void)?

    /// Bring iTerm2 forward for a pane, if iTerm2 is really the thing drawing it.
    ///
    /// **Two questions, and the second one is the one that was missing.** tmux's client list
    /// says that *something* is speaking control mode over a pty; it does not say that something
    /// is iTerm2. `tmux -C attach` from a script, an editor plugin, or anything else carries the
    /// identical `control-mode` flag — measured on tmux 3.6a from a client attached through a
    /// fifo. Activating iTerm2 for one of those would take somebody's keyboard away from what
    /// they were typing into, which is the failure this whole app exists not to commit.
    ///
    /// What is particular to iTerm2 is the *gateway pty*: the session `tmux -CC` was typed into
    /// is an ordinary iTerm2 row with a real tty, and it comes back in iTerm2's own `list`. So
    /// the client has to be found there before anything comes forward, and an unreadable list is
    /// not a yes.
    ///
    /// The cheap question is asked first so the Apple Event is only spent on a pane whose
    /// session tmux already says is being drawn.
    static func activateITerm2(forPane paneID: String) {
        let clients = controlModeObservation().clients
        guard !clients.isEmpty else { return }
        let session = sessionName(ofPane: paneID)
        guard shouldActivateITerm(paneSession: session, controlModeClients: clients) else { return }
        guard let ttys = ITerm.listedRowTTYs() else { return }
        guard shouldActivateITerm(
            paneSession: session,
            controlModeClients: ITerm.controlModeClientsDrawnByITerm2(clients, rowTTYs: ttys))
        else { return }
        // An application that will not come forward is worth no failure of its own.
        _ = ITerm.activate()
    }
}
