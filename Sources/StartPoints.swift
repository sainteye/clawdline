import AppKit
import CryptoKit
import Foundation

/// The places a session can be started in, and starting one.
///
/// **This is the only thing in the app that creates execution from somewhere that is not this
/// Mac.** Everything else the remote API does reads state, or types into a session a person
/// already opened; this opens a terminal and runs a program. Once a tunnel is up, the request
/// asking for it arrives from a phone on somebody else's network, and the repository is public,
/// so the shape matters more than the feature does:
///
/// - **The client never sends a path.** It sends an ``Place/id`` — an opaque digest — and the
///   server looks it up in a list *it* built and uses *its* copy of the directory. There is no
///   field anywhere on this route that a directory can be written into, which is a stronger
///   statement than "the directory is validated": validation is a thing the next person to touch
///   this file can weaken by accident, and an absent parameter is not.
/// - **The command is fixed.** `claude` or `codex`, both of them literals in ``Assistant``.
///   Which of the two is a named choice out of a closed list — an unknown name is a `400` — and
///   not a string that reaches a shell. Picking a recorded conversation back up is the second
///   named action this file said it would be if it were ever wanted: ``resume(_:sessionID:)``,
///   with its own literal on ``Assistant/resumeFlag``, and never a field on the first one.
/// - **A conversation is named the same way a place is.** ``sessionName(_:)`` admits a UUID and
///   nothing else, and on top of that the id has to be one this Mac just listed for that
///   directory — the same two-step as ``place(withID:)``, so an id nobody was handed is a `404`
///   rather than a string on a command line.
/// - **The one exception is a model name**, and it is an exception in the shape of the rule
///   rather than a hole in it: it is not a fragment of a command line, it is a name out of a
///   closed alphabet. ``modelName(_:)`` is the whole of that claim — `[a-z0-9._-]`, at most 64,
///   never opening with `-` — and no string it admits contains a character a shell reads. It is
///   checked there as well as wherever it came from, because the guarantee belongs to this file.
///   The route a phone can reach still passes nothing: only a dispatched task names a model.
/// - **Codex reasoning is not free-form.** ``ReasoningEffort`` has exactly `high` and `xhigh`;
///   this layer carries that typed choice to ``Assistant/command`` and cannot turn it into an
///   arbitrary config fragment. Nil adds nothing and preserves Codex and user defaults.
/// - **The list cannot name somewhere you have never been.** It is built from each assistant's
///   own record of where it has run and from the sessions clawdline can already see — all of
///   which are places this Mac has already run one of them in.
///
/// The gate itself is `RemoteServer.writing`, the same one sending goes through. Listing is
/// read-level; starting is not.
enum StartPoints {

    // MARK: - A place

    /// One directory a session can be started in.
    struct Place: Equatable {
        /// Opaque, stable between launches, and the only thing a client ever sends back.
        let id: String
        let path: String
        /// What a person should see. The project registry's name when it has one.
        let label: String
        /// When this place was last worked in — what the list is sorted by.
        let at: Date
    }

    /// The identifier a client carries around.
    ///
    /// A digest of the path, and **not because a digest is a secret** — it is not, and the path
    /// comes back in the listing next to it. It is opaque so that the only way to name a place is
    /// to have been handed one: the server resolves an id against the list it just built, so an
    /// invented id is a `404` and never a directory. Sixteen hex characters, because the failure
    /// mode of a collision is two places sharing a row rather than anything being disclosed.
    static func id(for path: String) -> String {
        let digest = SHA256.hash(data: Data(path.utf8)).map { String(format: "%02x", $0) }.joined()
        return String(digest.prefix(16))
    }

    // MARK: - What may be typed

    /// A path this is willing to put in a line and type into a shell.
    ///
    /// Absolute, and with no control character anywhere in it. The quoting below survives a
    /// quote and a backslash; nothing survives a newline, because on this path the line **is**
    /// the submission — a directory called `a<LF>b` would run `cd 'a` on its own and then try to
    /// run `b'` as a command. macOS allows that name, and this list is built out of the
    /// filesystem rather than out of anything the user typed, so it is worth saying out loud
    /// rather than assuming nobody would.
    static func usable(_ path: String) -> Bool {
        guard path.hasPrefix("/") else { return false }
        return !path.unicodeScalars.contains { $0.value < 0x20 || $0.value == 0x7F }
    }

    /// The one line a new iTerm2 tab is given.
    ///
    /// Built here rather than inside `iterm.js` so that **the exact string that will be executed
    /// is a value a test can look at**. The alternative — assembling it on the far side of an
    /// `osascript` call — means the quoting is only ever exercised by running it, which is the
    /// one thing a test must not do.
    ///
    /// The command is a literal on ``Assistant``, plus a model name that has been through
    /// ``modelName(_:)`` if there is one. `cwd` is quoted with the same `shellQuoted`
    /// the git plumbing uses.
    static func itermLine(cwd: String, assistant: Assistant = .claude,
                          model: String? = nil, reasoningEffort: ReasoningEffort? = nil,
                          permission: Permission = .ask,
                          addDir: String? = nil, resume: String? = nil) -> String {
        "cd " + Project.shellQuoted(cwd) + " && "
            + assistant.command(model: modelName(model), reasoningEffort: reasoningEffort,
                                permission: permission,
                                addDir: extraDir(addDir), resume: sessionName(resume))
    }

    /// A directory a session may be given reach over, or nil for anything that is not one.
    ///
    /// The only caller is the orchestrator and the only value it passes is `/tmp/.clawdline/<id>`,
    /// where `<id>` has already been through `OrchestratorDraft.isTaskID`. So this is not
    /// where that
    /// path is decided — it is where the promise in this file's header is kept anyway, because a
    /// second caller with a laxer idea of a path is exactly the change nobody would notice.
    ///
    /// Absolute, no `..` in it, and the same closed alphabet as a model name plus `/`. That is a
    /// stronger rule than "escape it properly": there is nothing in a string this admits for a
    /// shell to read, so `--add-dir <path>` stays one argument whatever arrives. Fail-closed and
    /// silent for the same reason `modelName` is — a session that has to ask about its own task
    /// directory beats no session at all.
    static func extraDir(_ raw: String?) -> String? {
        guard let raw, raw.hasPrefix("/"), raw.count <= 256, !raw.contains("..") else { return nil }
        let ok = raw.allSatisfy {
            ("a"..."z").contains($0) || ("A"..."Z").contains($0) || ("0"..."9").contains($0)
                || $0 == "." || $0 == "_" || $0 == "-" || $0 == "/"
        }
        return ok ? raw : nil
    }

    /// A model name, or nil for anything that is not one.
    ///
    /// The alphabet is `[a-z0-9._-]`, the length is 1…64, and it may not open with `-`. That
    /// holds every slug either CLI answers to — `haiku`, `claude-opus-5-20260201`,
    /// `gpt-5.1-codex` — and holds no character a shell reads: no space, no quote, no `$`, no
    /// `;`, no newline. So a line built with one is still one command with one argument,
    /// whatever was passed.
    ///
    /// **Fail-closed, and deliberately silent here.** A name this refuses becomes *no flag*
    /// rather than a refusal, because by the time a tab is being opened the honest answer is a
    /// session on the default model rather than no session at all. The loud refusal belongs
    /// upstream, where somebody is still holding the request: `OrchestratorDraft.draft`
    /// runs the same check and answers `bad_task`.
    static func modelName(_ raw: String?) -> String? {
        guard let raw, !raw.isEmpty, raw.count <= 64, !raw.hasPrefix("-") else { return nil }
        let ok = raw.allSatisfy {
            ("a"..."z").contains($0) || ("0"..."9").contains($0)
                || $0 == "." || $0 == "_" || $0 == "-"
        }
        return ok ? raw : nil
    }

    /// A conversation id, or nil for anything that is not one.
    ///
    /// A UUID as both CLIs write it: eight-four-four-four-twelve lowercase hex with dashes, and
    /// nothing else — not a search term, not a prefix, not the empty string. Narrower than
    /// ``modelName(_:)`` on purpose. A model name is a slug somebody may reasonably type; this is
    /// only ever a value that came back out of a listing this Mac produced seconds earlier, so
    /// there is no shape to be generous about.
    ///
    /// It matters that this is exact rather than merely shell-safe. `--resume` takes an
    /// **optional** value, and a value the CLI cannot read as an id it becomes a search term for
    /// — which opens an interactive picker in a tab nobody is sitting in front of. So the
    /// failure this refuses is not an injection; it is a session that never starts and never
    /// says why.
    ///
    /// Fail-closed and silent, like `modelName`: what it refuses becomes *no flag*. The loud
    /// refusal is upstream in ``past(withID:in:)``, where an unknown id is a `404` while
    /// somebody is still holding the request.
    static func sessionName(_ raw: String?) -> String? {
        guard let raw, raw.count == 36 else { return nil }
        let groups = raw.split(separator: "-", omittingEmptySubsequences: false)
        guard groups.map(\.count) == [8, 4, 4, 4, 12] else { return nil }
        let hex = groups.joined()
        let ok = hex.allSatisfy { ("0"..."9").contains($0) || ("a"..."f").contains($0) }
        return ok ? raw : nil
    }

    // MARK: - Which terminal

    /// Which terminal a new session goes into, or why none of them will do.
    enum Plan: Equatable {
        case iterm
        /// A tmux server is already running, so a window can be added to it.
        case tmux
        /// tmux is installed and no server is running, so one has to be started with nothing
        /// attached to it. Kept apart from ``tmux`` because the two are different promises to the
        /// person: one puts a session where they are already looking, the other puts it somewhere
        /// they have to go and find. See ``plan(terminal:running:tmux:)``.
        case tmuxDetached
        /// A terminal this can drive, that is not open. The bundle id.
        case notRunning(app: String)
        /// tmux is the terminal Settings asks for and there is no tmux on this Mac. It carries
        /// no name because the setting no longer holds one: the choice is *tmux*, not a bundle
        /// id it has to be reached through.
        case noTmux
    }

    /// How far tmux gets on this Mac. Three states rather than a `hasTmux` flag, because the two
    /// that used to share a spelling — *installed with no server* and *not installed at all* —
    /// are the whole of the question in ``plan(terminal:running:tmux:)``: one of them can be
    /// answered by starting a server and the other cannot be answered at all.
    enum TmuxReach: Equatable {
        /// No tmux this app can find. ``Tmux/binary`` says where it looked.
        case absent
        /// A tmux binary, and no server running on the default socket.
        case installed
        /// A server with panes on it, which is somewhere a window can be opened right now.
        case running
    }

    static let itermBundleID = "com.googlecode.iterm2"

    /// Which backend a new session is opened with — a setting of its own, and the whole of what
    /// ``plan(terminal:running:tmux:)`` is told.
    ///
    /// **It used to be read off the hotkey scope, and that was one setting doing two jobs.**
    /// ``Config/scopeApp`` says where ⌥Space is live; it was also the only place this app was
    /// told which terminal somebody uses, so the two could not be set apart. Somebody who wanted
    /// the chord bound to iTerm2 had no way to ask for sessions in tmux — measured on
    /// 2026-09-02, with iTerm2 running and a live `tmux -CC` session beside it, every new session
    /// still went to a plain iTerm2 tab and Settings had no word for the other answer.
    ///
    /// Two paths exist and only two: **iTerm2**, which has an AppleScript surface that can open a
    /// tab and write into it, and **tmux**, which is how every other terminal works here — see
    /// ``Tmux``. So this is three values rather than a list of bundle ids: the two backends, and
    /// *no preference*.
    enum TerminalChoice: String, Equatable, CaseIterable {
        /// No preference: iTerm2 when it is open, and tmux when it is not. The order every
        /// terminal operation in this app has always used, and what an unset config means.
        case auto
        /// iTerm2, and a refusal naming it when it is shut. Chosen by somebody who does not want
        /// a session appearing in a tmux server they are not attached to.
        case iterm
        /// tmux, whether or not iTerm2 is running. This is the answer that had no way of being
        /// said before.
        case tmux

        /// What a `config.json` written before this setting existed meant by its hotkey scope.
        ///
        /// **A migration, and the only place the scope is still allowed to say anything about a
        /// terminal.** ``plan(terminal:running:tmux:)`` no longer sees it. Without this a Ghostty
        /// or Terminal.app user — whose scope named their terminal, so whose sessions have always
        /// gone to tmux — would silently be moved onto ``auto`` and told to open an iTerm2 they
        /// may not have installed. It runs once, when ``Config`` finds no `terminal` key, and the
        /// next save writes the answer down so it never runs again.
        static func inheritedFromHotkeyScope(_ scope: String) -> TerminalChoice {
            let ids = scope.split(separator: ",")
                .map { $0.trimmingCharacters(in: .whitespaces) }
                .filter { !$0.isEmpty }
            // An empty scope means the hotkey is global, which says nothing about which terminal
            // is in use — so it reads as no preference, exactly as it did inside `plan`.
            if ids.isEmpty { return .auto }
            let namesITerm = ids.contains {
                $0.caseInsensitiveCompare(itermBundleID) == .orderedSame
            }
            return namesITerm ? .auto : .tmux
        }
    }

    /// Which terminal a new session goes into, given the choice in Settings and what is actually
    /// on this Mac.
    ///
    /// **Pure, and it reads nothing.** Everything it decides from is an argument, which is what
    /// lets the whole table below be a test with no terminal anywhere near it.
    ///
    /// **Starting a tmux server is offered in one situation and withheld in the other, and the
    /// difference is whether the person has somewhere else to be sent.** A session in a server
    /// nobody is attached to is real, drivable and invisible: Clawdline lists it, reads it, types
    /// into it and closes it, while at the Mac it does not appear until somebody runs
    /// ``Tmux/attachCommand``. Where tmux is what Settings asks for, the alternative to that
    /// trade is a refusal telling a phone to go and run tmux on a Mac it is not sitting at — so
    /// the server is started. Where the terminal is iTerm2 and it is merely shut, there is a
    /// better answer than either: open iTerm2, which is one click and puts the session where the
    /// person is already looking.
    static func plan(terminal: TerminalChoice, running: Set<String>, tmux: TmuxReach) -> Plan {
        func itermIsOpen() -> Bool {
            running.contains { $0.caseInsensitiveCompare(itermBundleID) == .orderedSame }
        }
        switch terminal {
        case .auto:
            if itermIsOpen() { return .iterm }
            // iTerm2 is shut and nothing was asked for by name. tmux is not a fallback *to*
            // iTerm2 — it is the other real backend, and a pane in it is a session that exists
            // whether or not anything is attached to it.
            if tmux == .running { return .tmux }
            return .notRunning(app: itermBundleID)
        case .iterm:
            // Named, so a running tmux server is not an answer to it: somebody who picked iTerm2
            // over `auto` asked for the tab they can see rather than the pane they cannot.
            return itermIsOpen() ? .iterm : .notRunning(app: itermBundleID)
        case .tmux:
            switch tmux {
            case .running: return .tmux
            case .installed: return .tmuxDetached
            case .absent: return .noTmux
            }
        }
    }

    /// An application's name as a person would say it, for a sentence they are going to read.
    /// The bundle id itself when the app is not installed — which is plain, and true.
    static func appName(_ bundleID: String) -> String {
        guard let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleID) else {
            return bundleID
        }
        return FileManager.default.displayName(atPath: url.path)
    }

    // MARK: - Starting one

    enum Outcome {
        /// `attach` is what somebody types to reach this session, and it is **only** ever filled
        /// in for the one plan that puts a session somewhere nobody is looking: ``Plan/tmuxDetached``,
        /// a tmux server this app started with nothing attached to it. Every other path opens the
        /// session in front of the person who asked for it and has nothing to add.
        ///
        /// **The fact travels with the outcome rather than being worked out again by whoever
        /// receives it.** `backend` is `.tmux` for a window on a server somebody is already
        /// watching and for a server started detached, so no caller can tell the two apart from
        /// what it is given — and the one that can, `plan`, is two frames below by then. Before
        /// this field the session was listed, drivable and unfindable: ``Tmux/attachCommand``
        /// existed and had no callers at all.
        case started(id: String, backend: Backend, attach: String?)
        /// `app` is the terminal the refusal is about, when it is about one. It rides next to the
        /// code in the error envelope so a page can name it in a sentence of its own rather than
        /// having to show the English one.
        case refused(status: Int, code: String, message: String, app: String?)
    }

    /// Everything ``start(_:assistant:model:reasoningEffort:permission:addDir:resume:)`` would
    /// otherwise go and ask this Mac, described instead.
    ///
    /// **It replaces the asking and nothing else.** ``plan(terminal:running:tmux:)`` still
    /// decides, `start` still maps that decision onto an ``Outcome``, and the route above still
    /// resolves an opaque id against a list. What a fixture supplies is the three answers the
    /// plan is read from, the one subprocess the tmux path ends in, and the list — which is
    /// between them the whole of *a Mac with tmux installed and no server running on it*, and
    /// that Mac is the entire subject of the attach command. Without this the success arm of the
    /// start route could only be exercised by opening a real terminal, which is why it never was.
    struct Fixture {
        var terminal: TerminalChoice
        var running: Set<String>
        var tmux: TmuxReach
        /// `Plan` is passed so a fixture can tell a detached server from a window on a running
        /// one; the two strings are the working directory and the line to be typed.
        var open: (Plan, String, String) -> Result<String, TerminalFailure>
        /// What ``places(limit:)`` answers. The paths still have to be real directories: `start`
        /// checks, and a fixture that could skip that check would be describing a Mac that
        /// cannot exist.
        var places: [Place]
    }

    /// Test seam for the whole of the above. Nil in the app, and the only writer is the suite.
    static var fixtureForTesting: Fixture?

    /// Open a tab where that place is and run an assistant in it.
    ///
    /// **No focus is taken.** The person who asked for this is holding a phone; the person at the
    /// Mac, if there is one, is in the middle of something else. `iterm.js` opens the tab without
    /// calling `activate`, so iTerm2 stays wherever it was in the window order. A window is only
    /// created when there is not one already, and that is the one case where something may come
    /// forward — there is no way to make a window and not have it be a window.
    static func start(_ place: Place, assistant: Assistant = .claude,
                      model: String? = nil, reasoningEffort: ReasoningEffort? = nil,
                      permission: Permission = .ask,
                      addDir: String? = nil, resume: String? = nil) -> Outcome {
        guard usable(place.path), isDirectory(place.path) else {
            return .refused(status: 404, code: "not_found",
                            message: "No place named that", app: nil)
        }
        // Asked once and held: `tmuxReach()` lists panes, and asking it twice would be a second
        // subprocess answering a question that may by then have a different answer.
        let fixture = fixtureForTesting
        let chosen = plan(terminal: fixture?.terminal ?? Config.shared.terminal,
                          running: fixture?.running ?? runningApps(),
                          tmux: fixture?.tmux ?? tmuxReach())
        switch chosen {
        case .iterm:
            let opened = ITerm.newTabResult(line: itermLine(cwd: place.path,
                                                             assistant: assistant,
                                                             model: model,
                                                             reasoningEffort: reasoningEffort,
                                                             permission: permission,
                                                             addDir: addDir,
                                                             resume: resume))
            guard case .success(let made) = opened else {
                let failure: TerminalFailure
                if case .failure(let problem) = opened { failure = problem }
                else { fatalError("unreachable terminal result") }
                let modal = failure.kind == .iTermAttention
                return .refused(status: 502,
                                code: modal ? "iterm_attention_required" : "terminal_io_failed",
                                message: failure.message, app: modal ? "iTerm2" : nil)
            }
            // Nothing to attach to and nothing to say: an iTerm2 tab is already on screen.
            return .started(id: made.id, backend: .iterm, attach: nil)

        case .tmux, .tmuxDetached:
            // Nothing is quoted here and nothing needs to be: tmux is given a working directory
            // and a command as separate arguments of a subprocess, so there is no line for a
            // directory name to break out of. The command reaching a shell one level down is why
            // `modelName` is a closed alphabet rather than an escaping rule — there is nothing
            // in a name it admits for that shell to read.
            let line = assistant.command(model: modelName(model),
                                         reasoningEffort: reasoningEffort,
                                         permission: permission,
                                         addDir: extraDir(addDir),
                                         resume: sessionName(resume))
            let opened = fixture?.open(chosen, place.path, line)
                ?? (chosen == .tmuxDetached
                    ? Tmux.newSessionResult(cwd: place.path, command: line)
                    : Tmux.newWindowResult(cwd: place.path, command: line))
            guard case .success(let pane) = opened else {
                let message: String
                if case .failure(let failure) = opened { message = failure.message }
                else { message = "tmux would not open a window." }
                return .refused(status: 502, code: "terminal_io_failed",
                                message: message, app: nil)
            }
            // **The one branch with somewhere to send somebody.** A window on a server that was
            // already running is a window in whatever is attached to that server; a server this
            // app started is drawn nowhere until ``Tmux/attachCommand`` is typed, so that
            // command travels with the outcome rather than being reassembled by a caller that
            // can no longer tell which of the two it got.
            return .started(id: pane, backend: .tmux,
                            attach: chosen == .tmuxDetached ? Tmux.attachCommand : nil)

        case .notRunning(let app):
            let name = appName(app)
            return .refused(status: 409, code: "terminal_closed",
                            message: "\(name) is not running, and this will not launch it for "
                                   + "you. Open it on the Mac and try again.", app: name)

        case .noTmux:
            // This means what it says: not "no tmux server" — one of those gets started — but no
            // tmux on this Mac at all, which is a thing only somebody at the keyboard can change.
            // Telling a phone to go and run tmux was an instruction it could not carry out;
            // installing tmux is at least the true one.
            //
            // **No `app`, on purpose.** The two 409s used to carry the bundle id out of the
            // hotkey scope, and the phone writes its own sentence around it — "a session cannot
            // be started in Ghostty from here". tmux is not that kind of name and there is no
            // longer a terminal id behind this refusal to offer.
            //
            // **A page answering this draws `webStartTerminalUnsupported`, which has no hole in
            // it.** The first spelling of this left the three pages guarding on `e.app` and
            // falling through to "That could not be started." — so the sentence below was
            // written carefully and read by nobody: the one place where somebody who changed
            // nothing saw a worse answer than before. This is the only producer of
            // `terminal_unsupported`, and it never carries a name, so the copy is written whole
            // rather than around a `{app}` nothing can fill.
            return .refused(status: 409, code: "terminal_unsupported",
                            message: "tmux is the terminal for new sessions in Settings, and "
                                   + "there is no tmux on this Mac. Install tmux, or pick a "
                                   + "different terminal in Settings — see docs/remote.md.",
                            app: nil)
        }
    }

    /// Open a tab where that place is and pick a recorded conversation back up in it.
    ///
    /// The second named action, and deliberately a thin one: everything that decides *where* and
    /// *whether* is ``start(_:assistant:model:permission:addDir:resume:)``, unchanged, and this
    /// adds one literal flag and one id that has already been proved to name a file on this Mac.
    /// Ordinary project history supplies that proof for human-started conversations. Scheduled
    /// children stay out of that general picker, so the orchestrator registry supplies the same
    /// proof only for a terminal run whose schedule detail disclosed the exact conversation id.
    /// A separate entry point rather than an argument on the route, because "start something
    /// here" and "carry on with that" are two different permissions to think about even though
    /// today they share a gate.
    static func resume(_ place: Place, sessionID: String,
                       assistant: Assistant = .claude) -> Outcome {
        guard let id = sessionName(sessionID) else {
            return .refused(status: 404, code: "not_found",
                            message: "No conversation named that", app: nil)
        }
        let conversation = past(withID: id, in: place, assistant: assistant)
        let scheduledTitle = conversation == nil ? Orchestrator.scheduledResumeTitle(
            sessionID: id, assistant: assistant, projectDir: place.path) : nil
        guard conversation != nil || scheduledTitle != nil else {
            return .refused(status: 404, code: "not_found",
                            message: "No conversation named that", app: nil)
        }
        let outcome = start(place, assistant: assistant, resume: id)
        return resumed(outcome, conversationID: id,
                       title: conversation?.title ?? scheduledTitle, assistant: assistant)
    }

    /// Finish the transition from a history row to the terminal that now owns it.
    ///
    /// Split from ``resume(_:sessionID:assistant:)`` so the handoff can be proved without opening
    /// a real terminal in the suite. The production caller has already resolved this id and title
    /// from assistant-specific history or the exact retained scheduled task that authorized it.
    static func resumed(_ outcome: Outcome, conversationID: String, title: String?,
                        assistant: Assistant) -> Outcome {
        guard case .started(let terminalID, _, _) = outcome, let title else { return outcome }
        CodexNaming.shared.rememberResumedTitle(
            title, assistant: assistant, conversationID: conversationID, targetID: terminalID)
        return outcome
    }

    // MARK: - Conversations already recorded here

    /// One conversation either assistant has already written down in a place.
    struct Past: Equatable {
        /// The session's own id, which is also what its transcript is named. The only part a
        /// client ever sends back.
        let id: String
        /// What to show. The title Claude Code gave it, the title somebody renamed it to, or —
        /// for the few transcripts that carry neither — the opening of the first thing that was
        /// typed into it.
        let title: String
        /// When it was last written to, which is what the list is sorted by.
        let at: Date
        /// Whether this conversation is open in a terminal on this Mac right now.
        ///
        /// Resuming one of these would put a second process on the same transcript, so a client
        /// is told which they are rather than left to find out. It is a fact about this instant,
        /// which is why it is computed per listing and never cached.
        let live: Bool
    }

    /// What has already been said in a place, newest first.
    ///
    /// Claude Code's half only. Codex owns a separate indexed list behind app-server and the
    /// assistant-taking overload below reads that instead of making this file reader pretend the
    /// two formats are alike.
    ///
    /// Only the top level of the project folder. Subagents' transcripts live in a directory
    /// beside them named for their parent, and a sidechain is not a conversation anybody had.
    ///
    /// **And only conversations somebody had.** Half of what is in a project folder is not one —
    /// see ``Front``. This mattered more than it sounds: in this repository's own folder, of a
    /// hundred and one transcripts, fifty-two were dispatched children and eleven were `-p`
    /// probes. Thirty-five were the work. A list capped at forty was therefore one where the cap
    /// fell in the middle of the plumbing and most of the real conversations were not on screen
    /// at all — including for the filter box, which can only narrow what was loaded.
    ///
    /// `scan` bounds the reading and `limit` bounds the answer, and they are different numbers
    /// for that reason: the front of every candidate has to be read to know whether it counts,
    /// and most of them do not.
    ///
    /// `dir` and `open` are parameters so a test can describe a project folder and a set of
    /// live transcripts instead of having to produce either. Neither is passed in the app.
    static func past(in place: Place, limit: Int = 200, scan: Int = 400,
                     dir: URL? = nil, open: Set<String>? = nil) -> [Past] {
        let dir = dir ?? Transcript.projectDirectory(forCwd: place.path)
        let fm = FileManager.default
        guard let names = try? fm.contentsOfDirectory(atPath: dir.path) else { return [] }

        let files = names.compactMap { name -> (id: String, url: URL, at: Date)? in
            guard name.hasSuffix(".jsonl"),
                  let id = sessionName(String(name.dropLast(".jsonl".count))) else { return nil }
            let url = dir.appendingPathComponent(name)
            var isDirectory: ObjCBool = false
            guard fm.fileExists(atPath: url.path, isDirectory: &isDirectory),
                  !isDirectory.boolValue else { return nil }
            return (id, url, modified(url))
        }.sorted { $0.at > $1.at }.prefix(scan)

        let open = open ?? openTranscripts()
        var out: [Past] = []
        for file in files {
            guard out.count < limit else { break }
            // The front first, because it is the cheap half and it is the half that decides. A
            // title is half a megabyte off the disk; whether this is a conversation at all is
            // answered by one record at the very top of the file.
            let front = cachedFront(of: file.url)
            guard front.isConversation else { continue }
            guard let title = Transcript.title(ofTranscript: file.url) ?? front.opening,
                  !title.isEmpty else { continue }
            out.append(Past(id: file.id, title: title, at: file.at,
                            live: open.contains(file.url.resolvingSymlinksInPath().path)))
        }
        return out
    }

    /// What one assistant has already recorded in a place, newest first.
    ///
    /// Codex owns its index and persisted thread names. Its supported app-server supplies both
    /// through `thread/list`, including an exact cwd filter; Clawdline only turns those rows
    /// into the same small shape the Claude transcript list already uses.
    static func past(in place: Place, assistant: Assistant, limit: Int = 200) -> [Past] {
        switch assistant {
        case .claude:
            return past(in: place, limit: limit)
        case .codex:
            return CodexNaming.listedThreads(cwd: place.path)
                .prefix(limit)
                .map { Past(id: $0.id, title: $0.title, at: $0.at, live: $0.live) }
        }
    }

    /// The conversation with that id in that place, or nothing.
    ///
    /// Resolved against a listing built now, for the same reason ``place(withID:)`` is: a
    /// transcript can be deleted between two looks, and the answer for one that is gone is the
    /// same `404` as an id that was never real.
    static func past(withID id: String, in place: Place,
                     dir: URL? = nil, open: Set<String>? = nil) -> Past? {
        guard sessionName(id) != nil else { return nil }
        return past(in: place, dir: dir, open: open).first { $0.id == id }
    }

    static func past(withID id: String, in place: Place, assistant: Assistant) -> Past? {
        guard sessionName(id) != nil else { return nil }
        return past(in: place, assistant: assistant).first { $0.id == id }
    }

    /// What the front of a transcript says about the session that wrote it.
    ///
    /// All of it off **one** record — the first turn that is somebody addressing the session —
    /// because that record carries both the sentence a person would recognise the conversation
    /// by and Claude Code's own account of where the sentence came from.
    struct Front: Equatable {
        /// The opening of that turn, one line of it. Nothing when there is no such turn.
        let opening: String?
        /// Claude Code's word for how the session was started. `cli` is a terminal somebody sat
        /// at; `sdk-cli` is a `-p` run. Absent on transcripts old enough not to record it, which
        /// is why nothing here treats absence as a refusal.
        let entrypoint: String?
        /// Its word for where the prompt came from. `typed` and `queued` are a person; `sdk` is
        /// a program.
        let promptSource: String?
        /// Whether the first thing said to it *began* with this app's own briefing — see
        /// ``Orchestrator/briefingOpening``, and the note there on why it is not the looser test
        /// the delivery receipt uses.
        let dispatched: Bool

        /// Whether this is a conversation somebody had, which is the only kind this list is of.
        ///
        /// Two things it is not, and neither judgement is a guess about the contents:
        ///
        /// **A session this app dispatched.** Clawdline opened it, typed one briefing into it
        /// and closed it when the work came back. It is this app's own plumbing showing through,
        /// and in this repository's folder it was fifty-two transcripts of a hundred and one.
        ///
        /// **A `-p` run.** `claude -p "what is 2+2"` writes a transcript like everything else,
        /// and eleven of them were sitting in that list under names like `Test` and `Hello`.
        /// Claude Code records what they are: the prompt came from the SDK and the process was
        /// never an interactive one. Across every project on the Mac this was written on — three
        /// hundred and thirteen transcripts — that pair occurred thirty-one times and every one
        /// was a probe.
        ///
        /// Either field alone is enough. They agree in every case seen, and requiring both would
        /// mean a pair that came apart in some later version fails open, quietly, back into a
        /// list nobody would think to look at.
        var isConversation: Bool {
            !dispatched && entrypoint != "sdk-cli" && promptSource != "sdk"
        }
    }

    /// Nothing found, which is what a transcript with no turn in it yet reads as.
    static let noFront = Front(opening: nil, entrypoint: nil, promptSource: nil, dispatched: false)

    /// Read that off a stretch of transcript. One record: the first that is somebody addressing
    /// the session. Records with `isSidechain` or a `toolUseResult` are stepped over — they are
    /// an agent's turn and a tool's answer quoted back, not a person.
    static func front(inText text: String, limit: Int = 80) -> Front {
        for line in text.split(separator: "\n") {
            guard let data = line.data(using: .utf8),
                  let row = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let found = front(inRecord: row, limit: limit) else { continue }
            return found
        }
        return noFront
    }

    /// One record, if it is the kind being looked for.
    ///
    /// **Not narrowed by looking for `"type":"user"` in the raw line first.** That is the obvious
    /// saving and it is a trap: a `file-history-snapshot` carries the contents of edited files,
    /// so a transcript of *this* app contains that exact text inside a record that is not a turn
    /// at all. The type is read after parsing, off the field, where it means what it says.
    static func front(inRecord row: [String: Any], limit: Int = 80) -> Front? {
        guard row["type"] as? String == "user",
              row["isSidechain"] as? Bool != true, row["toolUseResult"] == nil,
              let message = row["message"] as? [String: Any] else { return nil }
        var said = ""
        if let text = message["content"] as? String {
            said = text
        } else if let parts = message["content"] as? [[String: Any]] {
            for part in parts where part["type"] as? String == "text" {
                said = part["text"] as? String ?? ""
                break
            }
        }
        let one = said.split(whereSeparator: \.isNewline).first.map(String.init) ?? said
        let tidy = one.trimmingCharacters(in: .whitespaces)
        guard !tidy.isEmpty else { return nil }
        return Front(opening: tidy.count > limit ? String(tidy.prefix(limit)) + "\u{2026}" : tidy,
                     entrypoint: row["entrypoint"] as? String,
                     promptSource: row["promptSource"] as? String,
                     dispatched: said.hasPrefix(Orchestrator.briefingOpening))
    }

    /// The same, off a file, read a record at a time until the turn turns up.
    ///
    /// **Not a fixed window, and that is the whole point of it.** A `file-history-snapshot` is
    /// one record holding the contents of every file a session has edited, and it can be a
    /// hundred and thirty kilobytes on its own — four of the transcripts in this repository's
    /// folder open with one, this conversation's among them. A head of any fixed size is a head
    /// that record can push the first turn out of, and the transcripts it happens to are the
    /// long-running ones: exactly the conversations somebody wants back.
    ///
    /// What bounds this instead is **records**, because the turn being looked for is the *first*
    /// one — in every transcript measured it is within the first ten. The byte ceiling is a
    /// second bound for a file that is not what it claims to be, not the working limit.
    static func front(ofFile url: URL, records: Int = 60, bytes: Int = 8 << 20) -> Front {
        guard let handle = try? FileHandle(forReadingFrom: url) else { return noFront }
        defer { try? handle.close() }

        var buffer = Data()
        var read = 0
        var seen = 0
        while seen < records, read < bytes {
            guard let chunk = try? handle.read(upToCount: 64 << 10), !chunk.isEmpty else { break }
            read += chunk.count
            buffer.append(chunk)
            while let nl = buffer.firstIndex(of: 0x0A) {
                let line = buffer[buffer.startIndex..<nl]
                buffer = buffer[buffer.index(after: nl)...]
                seen += 1
                guard let row = try? JSONSerialization.jsonObject(with: line) as? [String: Any],
                      let found = front(inRecord: row) else { continue }
                return found
            }
        }
        // What is left when the reading stops has no newline after it. That is not a broken
        // record — a transcript being written to right now ends exactly like this, and so does
        // one whose last line was never terminated. Skipping it would mean a session opened
        // seconds ago is unreadable for as long as it is the only turn in the file.
        if !buffer.isEmpty, seen < records,
           let row = try? JSONSerialization.jsonObject(with: buffer) as? [String: Any],
           let found = front(inRecord: row) {
            return found
        }
        return noFront
    }

    // MARK: - Plumbing for the conversation list

    /// Cross main-queue-owned APIs by queue identity. In the app the main thread drains this
    /// queue; after the test runner calls `dispatchMain()` a worker does, so thread identity is
    /// no longer an answer to the question this hop asks.
    private static func onMain<T>(from site: String, _ work: () -> T) -> T {
        MainQueue.hop(from: site, alreadyOnMain: MainQueue.isCurrent, work)
    }

    private static var fronts: [String: Front] = [:]

    /// Remembered against the **path alone**, unlike the title, which is kept against a stamp.
    ///
    /// The difference is not an oversight. A title is appended to and rewritten for as long as a
    /// conversation runs, so an answer about it is only good until the file changes. The first
    /// turn is written once, when the session starts, and is never rewritten — there is nothing
    /// for a stamp to catch, and paying for one would mean re-reading the front of every
    /// transcript in a project every time the newest of them grew. ``Codex/head(of:)`` keeps its
    /// own answer for the same reason.
    ///
    /// **An empty answer is not kept.** A transcript seconds old can be on disk before its first
    /// turn is in it, and a cache with no stamp would hold that "no" for the life of the process.
    private static func cachedFront(of url: URL) -> Front {
        lock.lock()
        if let hit = fronts[url.path] {
            lock.unlock()
            return hit
        }
        lock.unlock()

        let found = front(ofFile: url)
        guard found.opening != nil else { return found }
        lock.lock()
        fronts[url.path] = found
        lock.unlock()
        return found
    }

    /// The transcripts something is writing to right now, by resolved path.
    ///
    /// Off the same reading of the session list everything else uses, and through
    /// ``Transcript/record(of:)``, which is the one place that knows how to find a session's
    /// file. Codex's rollouts come back in here too and simply never match a Claude Code
    /// transcript path, which costs nothing and keeps the rule in one place.
    private static func openTranscripts() -> Set<String> {
        let sessions = onMain(from: "StartPoints.openTranscripts") {
            SessionWatch.shared.targets
        }
        return Set(sessions.compactMap { Transcript.record(of: $0)?.url }
            .map { $0.resolvingSymlinksInPath().path })
    }

    // MARK: - The list

    /// Every place, newest first. Built fresh on each call, off a cache of the expensive half.
    ///
    /// Both assistants' records go in, and the result says nothing about which of them has been
    /// run where. That is deliberate: a directory is a directory, and a folder you have only ever
    /// opened Claude Code in is a perfectly good place to open Codex. Offering two lists would
    /// have been two lists to scroll for one answer.
    static func places(limit: Int = 40) -> [Place] {
        if let fixture = fixtureForTesting { return fixture.places }
        return tidy(recorded() + codexRecorded() + live(), limit: limit)
    }

    /// Where Codex has been run, out of its own record of it. See ``Codex/workedIn(days:limit:)``.
    static func codexRecorded() -> [Place] {
        Codex.workedIn().map {
            Place(id: id(for: $0.path), path: $0.path, label: label(for: $0.path), at: $0.at)
        }
    }

    /// The place with that id, or nothing.
    ///
    /// Resolved against a list built now rather than against one handed out earlier: a directory
    /// that has been deleted since the phone last looked is not a place any more, and the answer
    /// to an id for it is the same `404` as an id that was never real.
    static func place(withID id: String) -> Place? {
        guard !id.isEmpty else { return nil }
        return places().first { $0.id == id }
    }

    /// Dedupe by path, drop what is not a directory any more, newest first, and cap.
    ///
    /// `isDirectory` is a parameter so a test can describe a filesystem instead of making one.
    static func tidy(_ places: [Place], limit: Int = 40,
                     isDirectory: (String) -> Bool = StartPoints.isDirectory) -> [Place] {
        var best: [String: Place] = [:]
        for place in places {
            guard usable(place.path), isDurablePlace(place.path), isDirectory(place.path) else {
                continue
            }
            if let had = best[place.path], had.at >= place.at { continue }
            best[place.path] = place
        }
        return Array(best.values
            .sorted { $0.at == $1.at ? $0.path < $1.path : $0.at > $1.at }
            .prefix(limit))
    }

    /// A directory somebody would recognise as a place to work, rather than storage an
    /// assistant created while doing its work.
    ///
    /// Rollouts faithfully record all of these locations, which is exactly why the filter lives
    /// here rather than in the parser. Running Codex from a Claude scratchpad, opening Codex
    /// Desktop, or starting a CLI from `$HOME` all produce valid records; none creates a project
    /// the person asked Clawdline to keep in its start list.
    static func isDurablePlace(_ path: String, home: String? = nil,
                               temporary: String? = nil) -> Bool {
        func resolved(_ value: String) -> String {
            URL(fileURLWithPath: value).standardizedFileURL.resolvingSymlinksInPath().path
        }
        func isInside(_ path: String, _ root: String) -> Bool {
            path == root || path.hasPrefix(root + "/")
        }

        let path = resolved(path)
        let home = resolved(home ?? FileManager.default.homeDirectoryForCurrentUser.path)
        if path == home { return false }
        for root in scratchRoots(home: home, temporary: temporary ?? NSTemporaryDirectory()) {
            if isInside(path, resolved(root)) { return false }
        }
        return true
    }

    /// Every root that only ever holds an assistant's own storage — one list, shared between
    /// ``isDurablePlace(_:home:temporary:)`` and the name screen in
    /// ``recorded(root:folders:scan:home:temporary:)``, so the judgement and the screen cannot
    /// drift apart. The home folder is not here because it is refused exactly, not everything
    /// inside it.
    ///
    /// `/tmp` and `/private/tmp` ride alongside `temporary`: `NSTemporaryDirectory` normally
    /// resolves one per-user /var/folders root, and these cover deliberate /tmp worktrees and
    /// the `/private` spelling macOS reports through `lsof`.
    private static func scratchRoots(home: String, temporary: String) -> [String] {
        [home + "/.claude", home + "/.codex", home + "/Documents/Codex",
         home + "/Library/Application Support/Clawdline/worktrees",
         temporary, "/tmp", "/private/tmp"]
    }

    /// What a scratch folder's *name* opens with.
    ///
    /// The roots from ``scratchRoots(home:temporary:)`` pushed through ``slug(of:)``, each in
    /// every spelling a recorded cwd arrives in: as given, symlink-resolved, and the `/private`
    /// twin. The twin is spelled out by hand because `resolvingSymlinksInPath` deliberately
    /// leaves the `/var` and `/tmp` roots alone — `NSTemporaryDirectory()` says
    /// `/var/folders/…` and stays that way through it, while a transcript's cwd says
    /// `/private/var/folders/…`, and a screen that knows only one spelling misses half the
    /// scratch (the first run of the suite against this code caught exactly that). Computed
    /// once per listing rather than once per folder, because resolving is filesystem work.
    ///
    /// `home` and `temporary` are parameters for the same reason they are on `isDurablePlace`:
    /// so a test can describe a machine instead of running on one.
    static func scratchMarks(home: String? = nil, temporary: String? = nil) -> [String] {
        let home = home ?? FileManager.default.homeDirectoryForCurrentUser.path
        let temporary = temporary ?? NSTemporaryDirectory()
        var marks: Set<String> = []
        for root in scratchRoots(home: home, temporary: temporary) {
            let url = URL(fileURLWithPath: root).standardizedFileURL
            for spelling in [url.path, url.resolvingSymlinksInPath().path] {
                marks.insert(slug(of: spelling))
                if spelling.hasPrefix("/private/") {
                    marks.insert(slug(of: String(spelling.dropFirst("/private".count))))
                } else if spelling.hasPrefix("/var/") || spelling.hasPrefix("/tmp/")
                            || spelling == "/tmp" {
                    marks.insert(slug(of: "/private" + spelling))
                }
            }
        }
        return Array(marks)
    }

    /// Whether a project folder's name could stand for a durable place — judged from the name
    /// alone, before anything pays to find out what the folder really is.
    ///
    /// The name is ``slug(of:)`` of the working directory, and that map is many-to-one, so this
    /// proves a negative and nothing more: a name that opens with the slug of a scratch root
    /// names a path inside that root — or a sibling that happens to slug identically, like
    /// `/tmp.backup` against `/tmp`, which is the one shape this misjudges. That is why
    /// ``recorded(root:folders:scan:home:temporary:)`` treats the answer as a priority rather
    /// than a verdict: a doubtful folder resolves last instead of never, so a collision costs
    /// its place in the queue and not its place on the list.
    static func couldBeDurable(_ name: String, marks: [String]) -> Bool {
        !marks.contains { name == $0 || name.hasPrefix($0 + "-") }
    }

    /// The directories clawdline can already see a session in.
    ///
    /// These are trusted enough to type into — the send route does it all day — so they are
    /// trusted enough to start another one in. They also cover the case the recorded lists cannot:
    /// a directory somebody opened an assistant in five seconds ago, before anything was written
    /// down.
    static func live(now: Date = Date()) -> [Place] {
        let sessions = onMain(from: "StartPoints.live") { SessionWatch.shared.targets }
        return sessions.compactMap { session in
            guard let cwd = Targets.workingDirectory(of: session) else { return nil }
            return Place(id: id(for: cwd), path: cwd, label: label(for: cwd), at: now)
        }
    }

    // MARK: - Where Claude Code has been run

    /// Claude Code keeps one directory per project under here, and the transcripts inside it.
    static var projectsRoot: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".claude/projects", isDirectory: true)
    }

    /// Claude Code's name for a working directory: **every** character that is not an ASCII
    /// letter or digit becomes a dash.
    ///
    /// Worth being exact about, because the obvious guess — only the separators — is what
    /// ``Transcript/projectDirectory(forCwd:)`` does, and it is wrong the moment a path has a
    /// space, a dot or an underscore in it. Over UTF-16 units rather than characters, because the
    /// thing being matched was produced by a JavaScript regular expression and that is what one
    /// counts.
    ///
    /// The map is many-to-one, so this is only ever used **forwards**, to check a candidate.
    /// `-Users-me-code-cairn-frontend` is `cairn/frontend` and `cairn-frontend` and there is
    /// nothing in the name to say which, so nothing here ever tries to read a path out of one.
    static func slug(of path: String) -> String {
        var out = ""
        out.reserveCapacity(path.utf16.count)
        for unit in path.utf16 {
            let alnum = (unit >= 48 && unit <= 57)
                || (unit >= 65 && unit <= 90)
                || (unit >= 97 && unit <= 122)
            out.append(alnum ? Character(UnicodeScalar(UInt8(unit))) : "-")
        }
        return out
    }

    /// Every `cwd` a stretch of transcript mentions, in the order it mentions them.
    ///
    /// Read textually rather than parsed. These files reach fifty megabytes with single records a
    /// megabyte long, and what is wanted out of one is a short string near the top — so a JSON
    /// parse would be the most expensive possible way to get it.
    ///
    /// **All of them, not the first.** A transcript quotes other people's directories: a session
    /// in `some-app` had `/Users/…/another_project` sitting in its last hundred kilobytes,
    /// inside something that had been pasted in. Which of them is the real one is decided by
    /// ``slug(of:)`` against the folder the file is in, never by where it appeared.
    static func cwds(in text: String) -> [String] {
        let needle = "\"cwd\":\""
        var found: [String] = []
        var from = text.startIndex
        while let hit = text.range(of: needle, range: from..<text.endIndex) {
            from = hit.upperBound
            var i = hit.upperBound
            var raw = ""
            var closed = false
            while i < text.endIndex {
                let c = text[i]
                if c == "\\" {
                    let next = text.index(after: i)
                    guard next < text.endIndex else { break }
                    raw.append(c)
                    raw.append(text[next])
                    i = text.index(after: next)
                    continue
                }
                if c == "\"" { closed = true; break }
                // A record is one line. A newline before the closing quote means the read stopped
                // in the middle of one, and half a path is not a path.
                if c == "\n" || c == "\r" { break }
                raw.append(c)
                i = text.index(after: i)
            }
            guard closed, let value = unescape(raw), !value.isEmpty else { continue }
            found.append(value)
        }
        return found
    }

    /// A JSON string body back into the string it stood for.
    private static func unescape(_ raw: String) -> String? {
        guard let data = "\"\(raw)\"".data(using: .utf8),
              let value = try? JSONSerialization.jsonObject(with: data,
                                                           options: [.fragmentsAllowed]) as? String
        else { return nil }
        return value
    }

    /// The working directory a project folder stands for, or nothing when it cannot be proved.
    ///
    /// Head first, then tail, over the newest few transcripts, and every candidate is checked
    /// against the folder's own name before it is believed. A project folder that cannot prove
    /// what it is — an empty one, or one whose transcripts have been trimmed — is dropped rather
    /// than guessed at, which is the whole reason the check is here.
    static func directory(named name: String, transcripts: [URL]) -> String? {
        for url in transcripts.prefix(4) {
            for text in [head(of: url, bytes: 256 << 10), tail(of: url, bytes: 64 << 10)] {
                guard let text else { continue }
                for candidate in cwds(in: text) where slug(of: candidate) == name {
                    return candidate
                }
            }
        }
        return nil
    }

    /// Where Claude Code has been run, out of its own record of it.
    ///
    /// The expensive half — proving what a project folder stands for — is remembered against that
    /// folder's stamp, because the answer is a fact about a directory that does not move. What is
    /// read every time is the cheap half: which transcript in it is newest, which is what "most
    /// recently used" means here.
    ///
    /// `folders` bounds the answer and `scan` bounds the reading — the same split
    /// ``past(in:limit:scan:dir:open:)`` uses, for the same reason: whether a folder deserves one
    /// of the `folders` slots is only known after resolving it, and some folders resolve to
    /// nothing. A machine with a thousand projects still does not pay for all of them to answer
    /// one menu: at most `scan` folders are listed and proved, once, and the proof is cached
    /// against each folder's stamp after that.
    ///
    /// **The slots are spent plausible-first.** When this was measured, 162 of the 192 folders on
    /// this Mac were scratch — verification snapshots under `/private/var/folders/…` and brokered
    /// worktree checkouts, most of them newer than every real project — and a budget spent in
    /// plain mtime order went almost entirely to folders ``tidy(_:limit:isDirectory:)`` was about
    /// to throw away: the menu was five entries long on a machine with thirty projects. So a
    /// folder whose *name* already reads as scratch goes to the back of the queue — behind every
    /// plausible one, newest-first within each half. Delayed rather than dropped, because a name
    /// is a many-to-one map that can collide (see ``couldBeDurable(_:marks:)``), and `tidy`
    /// stays the judge of what is actually offered.
    static func recorded(root: URL? = nil, folders: Int = 60, scan: Int = 240,
                         home: String? = nil, temporary: String? = nil) -> [Place] {
        let base = root ?? projectsRoot
        let fm = FileManager.default
        guard let names = try? fm.contentsOfDirectory(atPath: base.path) else { return [] }

        let marks = scratchMarks(home: home, temporary: temporary)
        let dirs = names.compactMap { name -> (name: String, url: URL, at: Date, plausible: Bool)? in
            let url = base.appendingPathComponent(name, isDirectory: true)
            var isDirectory: ObjCBool = false
            guard fm.fileExists(atPath: url.path, isDirectory: &isDirectory),
                  isDirectory.boolValue else { return nil }
            return (name, url, modified(url), couldBeDurable(name, marks: marks))
        }.sorted {
            $0.plausible == $1.plausible ? $0.at > $1.at : $0.plausible
        }.prefix(scan)

        var out: [Place] = []
        for dir in dirs {
            guard out.count < folders else { break }
            let transcripts = (try? fm.contentsOfDirectory(atPath: dir.url.path))?
                .filter { $0.hasSuffix(".jsonl") }
                .map { name -> (url: URL, at: Date) in
                    let url = dir.url.appendingPathComponent(name)
                    return (url, modified(url))
                }
                .sorted { $0.at > $1.at } ?? []
            guard let newest = transcripts.first else { continue }
            guard let path = cachedDirectory(named: dir.name, in: dir.url,
                                             transcripts: transcripts.map(\.url)) else { continue }
            out.append(Place(id: id(for: path), path: path, label: label(for: path), at: newest.at))
        }
        return out
    }

    // MARK: - Plumbing

    private static let lock = NSLock()
    private static var resolved: [String: (stamp: String, path: String?)] = [:]

    private static func cachedDirectory(named name: String, in url: URL,
                                        transcripts: [URL]) -> String? {
        let stamp = Paths.stamp(of: url)
        lock.lock()
        if let hit = resolved[name], hit.stamp == stamp {
            lock.unlock()
            return hit.path
        }
        lock.unlock()

        let found = directory(named: name, transcripts: transcripts)
        lock.lock()
        resolved[name] = (stamp, found)
        lock.unlock()
        return found
    }

    /// The project's own name, the same one the session list draws. The folder's name when the
    /// registry has never heard of it, which is what a person calls it anyway.
    static func label(for path: String) -> String {
        if let row = ProjectIcon.row(forCwd: path), let label = row["label"] as? String,
           !label.isEmpty {
            return label
        }
        return (path as NSString).lastPathComponent
    }

    static func isDirectory(_ path: String) -> Bool {
        var isDirectory: ObjCBool = false
        return FileManager.default.fileExists(atPath: path, isDirectory: &isDirectory)
            && isDirectory.boolValue
    }

    private static func modified(_ url: URL) -> Date {
        let attrs = try? FileManager.default.attributesOfItem(atPath: url.path)
        return (attrs?[.modificationDate] as? Date) ?? .distantPast
    }

    private static func head(of url: URL, bytes: Int) -> String? {
        guard let handle = try? FileHandle(forReadingFrom: url) else { return nil }
        defer { try? handle.close() }
        guard let data = try? handle.read(upToCount: bytes), !data.isEmpty else { return nil }
        return String(decoding: data, as: UTF8.self)
    }

    private static func tail(of url: URL, bytes: Int) -> String? {
        guard let handle = try? FileHandle(forReadingFrom: url) else { return nil }
        defer { try? handle.close() }
        let size = (try? handle.seekToEnd()) ?? 0
        guard size > UInt64(bytes) else { return nil }   // the head already covered it
        try? handle.seek(toOffset: size - UInt64(bytes))
        guard let data = try? handle.readToEnd(), !data.isEmpty else { return nil }
        return String(decoding: data, as: UTF8.self)
    }

    /// Which applications are open, so ``plan(terminal:running:tmux:)`` can be told rather than
    /// having to ask. On the main thread, because `NSWorkspace` is.
    private static func runningApps() -> Set<String> {
        let read = { Set(NSWorkspace.shared.runningApplications.compactMap(\.bundleIdentifier)) }
        return onMain(from: "StartPoints.runningApps", read)
    }

    /// Run the three production readers from a main-queue fixture. It deliberately returns
    /// nothing: which sites crossed is read from ``MainQueue/endRecordingHopsForTesting()``, so a
    /// crossing that was deleted or renamed cannot be reported by the seam that was supposed to
    /// prove it. The values are discarded — reachability, not the machine's current transcript or
    /// application inventory, is what this exercises.
    static func exerciseQueueCrossingsForTesting() {
        _ = openTranscripts()
        _ = live()
        _ = runningApps()
    }

    /// How far tmux gets on this Mac right now: nothing, a binary, or a server with panes on it.
    ///
    /// **An inventory that failed is read as a server, not as an absent one.** `panes()` comes
    /// back empty both when there is no server and when tmux could not be asked, and only the
    /// first of those is permission to start a second server on top of whatever is there —
    /// ``Tmux/paneObservation()`` keeps the two apart precisely so this can tell them apart. A
    /// `new-window` against a server that would not answer fails honestly with tmux's own words,
    /// which is the better of the two wrong answers.
    private static func tmuxReach() -> TmuxReach {
        guard Tmux.binary != nil else { return .absent }
        let observed = Tmux.paneObservation()
        if !observed.sessions.isEmpty { return .running }
        return observed.isComplete ? .installed : .running
    }
}
