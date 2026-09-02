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
            let message = receipt.failure?.message.lowercased() ?? ""
            if message.contains("no server running") || message.contains("failed to connect to server") {
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

    static func controlModeObservation() -> ControlModeObservation {
        guard binary != nil else {
            // No tmux on this Mac is a firm answer rather than a failure: nothing here is drawing
            // anybody's tmux windows, so a pty-less iTerm2 row is genuinely unexplained.
            return ControlModeObservation(clients: [], error: nil)
        }
        let fmt = "#{client_tty}\u{1}#{client_flags}\u{1}#{client_session}"
        let receipt = run(["list-clients", "-F", fmt])
        if !receipt.ok {
            // Same rule as `paneObservation`: no server is a complete empty answer, and anything
            // else — a timeout above all — has no authority to prove a client absent.
            let message = receipt.failure?.message.lowercased() ?? ""
            if message.contains("no server running") || message.contains("failed to connect to server") {
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
    static func parseControlModeClients(_ output: String) -> [ControlModeClient] {
        output.split(separator: "\n").compactMap { line in
            let f = line.components(separatedBy: "\u{1}")
            guard f.count >= 2 else { return nil }
            let flags = f[1].split(separator: ",").map {
                $0.trimmingCharacters(in: .whitespaces)
            }
            guard flags.contains("control-mode") else { return nil }
            return ControlModeClient(tty: f[0].trimmingCharacters(in: .whitespaces),
                                     session: f.count > 2 ? f[2] : "")
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
    /// A new window in the first session tmux has, running something.
    ///
    /// `-P -F '#{pane_id}'` so the pane it made comes back rather than having to be guessed at by
    /// listing everything again and looking for what is new — which would be a race against
    /// anybody else's window opening at the same moment.
    static func newWindowResult(cwd: String, command: String) -> Result<String, TerminalFailure> {
        var args = ["new-window", "-P", "-F", "#{pane_id}"]
        if !cwd.isEmpty { args += ["-c", cwd] }
        args.append(command.isEmpty ? Assistant.claude.command : command)
        let result = run(args)
        guard result.ok else {
            return .failure(result.failure ?? TerminalFailure(
                kind: .io, message: "tmux would not open a window."))
        }
        let id = result.out.trimmingCharacters(in: .whitespacesAndNewlines)
        guard id.hasPrefix("%") else {
            return .failure(TerminalFailure(kind: .io,
                                            message: "tmux returned no new pane id."))
        }
        return .success(id)
    }

    static func newWindow(cwd: String, command: String) -> String? {
        try? newWindowResult(cwd: cwd, command: command).get()
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
        // -e keeps the escape sequences, which is the only way any of this arrives with colour.
        let receipt = run(["capture-pane", "-p", "-e", "-J", "-S", "-\(scrollback)", "-t", paneID])
        return receipt.ok ? receipt.out : nil
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
        // Cheapest question first: with no control-mode client anywhere this costs one
        // `list-clients` on a keypress and stops, and the pane's own session is never asked for.
        let clients = controlModeObservation().clients
        guard !clients.isEmpty else { return nil }
        guard shouldActivateITerm(paneSession: sessionName(ofPane: paneID),
                                  controlModeClients: clients) else { return nil }
        // The selection already happened and is the part that was asked for. An application that
        // will not come forward is worth no failure of its own.
        _ = ITerm.activate()
        return nil
    }
}
