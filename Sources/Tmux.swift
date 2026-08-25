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

    /// tmux is almost never on an app's PATH — apps do not inherit a login shell.
    /// Config can name it outright; otherwise try where package managers actually put it.
    static var binary: String? {
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

    @discardableResult
    private static func run(_ args: [String], stdin: String? = nil) -> (out: String, ok: Bool) {
        guard let bin = binary else { return ("", false) }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: bin)
        p.arguments = args
        let out = Pipe()
        p.standardOutput = out
        p.standardError = Pipe()
        if let stdin {
            let input = Pipe()
            p.standardInput = input
            do { try p.run() } catch { return ("", false) }
            input.fileHandleForWriting.write(Data(stdin.utf8))
            input.fileHandleForWriting.closeFile()
            let data = out.fileHandleForReading.readDataToEndOfFile()
            p.waitUntilExit()
            return (String(data: data, encoding: .utf8) ?? "", p.terminationStatus == 0)
        }
        do { try p.run() } catch { return ("", false) }
        let data = out.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        return (String(data: data, encoding: .utf8) ?? "", p.terminationStatus == 0)
    }

    /// Every pane in every session, whether or not a client is attached.
    /// `-a` matters: a detached session is still somewhere text can usefully go.
    static func panes() -> [TargetSession] {
        guard binary != nil else { return [] }
        let fmt = "#{pane_id}\u{1}#{pane_tty}\u{1}#{pane_current_command}\u{1}"
            + "#{session_name}\u{1}#{window_index}\u{1}#{pane_index}\u{1}#{pane_title}"
            + "\u{1}#{pane_current_path}"
        let (out, ok) = run(["list-panes", "-a", "-F", fmt])
        guard ok else { return [] }
        return parsePanes(out, running: ITerm.assistantPIDs())
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
    static func newWindow(cwd: String, command: String) -> String? {
        var args = ["new-window", "-P", "-F", "#{pane_id}"]
        if !cwd.isEmpty { args += ["-c", cwd] }
        args.append(command.isEmpty ? Assistant.claude.command : command)
        let result = run(args)
        guard result.ok else { return nil }
        let id = result.out.trimmingCharacters(in: .whitespacesAndNewlines)
        return id.hasPrefix("%") ? id : nil
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
        let (out, ok) = run(["capture-pane", "-p", "-e", "-J", "-S", "-\(scrollback)", "-t", paneID])
        return ok ? out : nil
    }

    /// Bring a pane to the front within tmux. Whether the terminal window itself comes
    /// forward is up to whichever emulator is drawing it, and not something to chase.
    static func reveal(_ paneID: String) {
        run(["select-pane", "-t", paneID])
        run(["select-window", "-t", paneID])
    }
}
