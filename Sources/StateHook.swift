import Foundation

/// Running a command of somebody else's choosing when a session changes state.
///
/// Everything else here decides on your behalf what a state change is worth: the island leans
/// out, the menu bar mark changes colour, a row moves to the top of the list. Those are opinions,
/// and they are the opinions of whoever wrote them. This is the file for everybody the opinions
/// do not fit — a session went from working to waiting, here is the command you named, here is
/// everything known about the moment, in environment variables.
///
/// **It is the extension point worth having before any other, and that is measured rather than
/// felt.** Herdr publishes 682 plugins with parseable manifests; across all of them the single
/// event `pane.agent_status_changed` accounts for 15% of every event hook declared, and it
/// appears in 44% of the plugins that hook anything at all. Notifications, status lines,
/// dashboards, watchdogs, Slack and Telegram bridges are that one event wearing different
/// clothes. The reactive half of their own example plugin is one event declaration and twelve
/// lines of Node reading two environment variables.
///
/// So: environment variables and an argv array, and nothing else. No SDK, no bindings, no
/// opinion about a language — those 682 are written in bash, node, python, bun and powershell,
/// and nobody ever shipped a binding for any of them, because "the entire CLI is the plugin API"
/// is a cheaper contract to keep than a library. A hook here is a program. It gets an
/// environment and it exits.
///
/// **No shell, and that is a boundary rather than a preference.** `on_state_change` is an array
/// — `["node", "~/bin/notify.mjs"]` — handed to `Process` as a program and its arguments. Nothing
/// is word-split, nothing is glob-expanded, and a `$(…)` that somebody put in a tab title is four
/// characters rather than a command. The one shell convention kept is a leading `~`, expanded per
/// element, because a config file full of `/Users/yourname` is one nobody else can copy.
///
/// **Transitions, never readings**, which is the whole of the correctness here and the part that
/// is easy to get wrong twice. A reading happens every 1.2 seconds with the panel up; the live
/// line inside `working` carries its own clock and therefore changes on every one of them, so a
/// hook keyed on "the state object differs" would fire once a second, forever, for a session that
/// is quietly doing exactly what it was doing before. And `unknown` is not a state to announce at
/// all — a screen that could not be read is the absence of an answer, not an event, so a slow
/// answer from iTerm2 must not look like a session that stopped and started again.
///
/// **Bounded, because the command is not ours.** It runs off the main thread, at most four at a
/// time, and anything still alive after ten seconds is taken out. Past the limit an event is
/// dropped rather than queued — a queue in front of a wedged program is a way of guaranteeing
/// that the notification arrives when it no longer means anything — and every drop is written to
/// the log, because silent truncation reads exactly like nothing having happened.
enum StateHook {

    /// What changed, for one session.
    ///
    /// `from` is Optional because a dictionary lookup is, and for no other reason: nothing is ever
    /// fired with it empty. A session there is no previous reading for is one that has just been
    /// noticed, and being noticed is not a change — see ``transitions(from:to:sessions:)``.
    struct Change: Equatable {
        let session: TargetSession
        let from: SessionState?
        let to: SessionState
    }

    // MARK: - What changed

    /// The transitions between two readings, and nothing else.
    ///
    /// Pure, and takes both readings as arguments, because the three rules below are the entire
    /// feature and each of them is a thing a fixed pair of dictionaries can ask about — no
    /// terminal, no processes, no clock.
    ///
    /// - **The same state twice is not a transition**, even when the two are not equal. A
    ///   `working` session carries the live line Claude Code drew, which counts up in seconds, so
    ///   two readings a second apart are `.working("Cogitating… (7s)")` and
    ///   `.working("Cogitating… (8s)")` and `==` says they differ. They do not differ in any way
    ///   worth waking a program up for.
    /// - **Nothing involving `unknown` fires.** Not into it and not out of it. `unknown` means the
    ///   screen could not be read — iTerm2 busy, a pane that went away, a capture that timed out —
    ///   and treating it as a state would turn every slow answer into a pair of events, one
    ///   leaving the real state and one arriving back at it a second later.
    /// - **A session appearing does not fire.** On the first reading after launch there is no
    ///   previous state for anything, and ten open sessions are not ten state changes; the same
    ///   rule then covers a tab opened an hour later for free.
    static func transitions(from old: [String: SessionState],
                            to new: [String: SessionState],
                            sessions: [TargetSession]) -> [Change] {
        var out: [Change] = []
        for session in sessions {
            guard let to = new[session.id], to != .unknown else { continue }
            guard let from = old[session.id], from != .unknown else { continue }
            guard !alike(from, to) else { continue }
            out.append(Change(session: session, from: from, to: to))
        }
        return out
    }

    /// Whether two readings say the same thing. `Equatable` cannot answer this: the live line is
    /// part of the value and is not part of the state.
    private static func alike(_ a: SessionState, _ b: SessionState) -> Bool {
        if case .working = a, case .working = b { return true }
        return a == b
    }

    /// The word a hook is given for a state. Spelled out here rather than taken from the enum,
    /// so that renaming a case in Swift cannot silently rename part of somebody's public API.
    static func name(_ state: SessionState) -> String {
        switch state {
        case .working: return "working"
        case .waiting: return "waiting"
        case .idle:    return "idle"
        case .unknown: return "unknown"
        }
    }

    // MARK: - What a hook is told

    /// The inherited `PATH`, with the places things are actually installed put in front of it.
    ///
    /// Resolving `argv[0]` ourselves is only half the problem. The other half is a hook written as
    /// `#!/usr/bin/env node`, which is how most of them will be written — the kernel runs `env`,
    /// `env` looks in `PATH`, and an app launched from Finder has the launchd `PATH`, which is
    /// `/usr/bin:/bin:/usr/sbin:/sbin` and contains no `node`, no `bun`, and no Homebrew. So the
    /// script fails with "node: command not found" for a reason that has nothing to do with the
    /// script, and the person debugging it is looking in the wrong place entirely.
    ///
    /// Prepended rather than replaced, and duplicates left alone: `PATH` is somebody's ordering
    /// and it is not this app's business to reorder it, only to make sure the obvious places are
    /// in it at all.
    static func usefulPath(_ inherited: String?) -> String {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let extras = ["/opt/homebrew/bin", "/usr/local/bin",
                      "\(home)/.local/bin", "\(home)/.bun/bin"]
        let already = Set((inherited ?? "").split(separator: ":").map(String.init))
        let missing = extras.filter { !already.contains($0) }
        guard !missing.isEmpty else { return inherited ?? "" }
        let rest = (inherited ?? "").isEmpty ? "/usr/bin:/bin" : inherited!
        return (missing + [rest]).joined(separator: ":")
    }

    /// The variables one change is worth, and only those — the inherited environment is added to
    /// them at launch and never here.
    ///
    /// Pure for that reason. A function that reached for `ProcessInfo` would be a function whose
    /// output depends on how the app happened to be started, and a test of it could only check
    /// that a subset of a much larger dictionary was present. This one is checkable whole.
    ///
    /// Two things are omitted rather than blanked. `CLAWDLINE_LINE` is absent when there is no
    /// live line, because Claude Code draws none at all while plain text is coming back and an
    /// empty string would read as one that had been erased. `CLAWDLINE_CLAUDE_SESSION` is absent
    /// unless a hook told us — it is the name of the transcript file, which is worth a great deal
    /// to anything that wants to read what happened, and worth nothing invented.
    ///
    /// `CLAWDLINE_EVENT_JSON` is built out of the variables rather than beside them, so that the
    /// two cannot drift: a field added above appears in the object without anybody remembering to
    /// add it twice. Keys are sorted, so a hook can diff two of them.
    static func environment(for change: Change, claudeSession: String? = nil) -> [String: String] {
        var out: [String: String] = [
            "CLAWDLINE_EVENT": "state_changed",
            "CLAWDLINE_STATE": name(change.to),
            "CLAWDLINE_SESSION_ID": change.session.id,
            "CLAWDLINE_TTY": change.session.tty,
            "CLAWDLINE_LABEL": change.session.displayLabel
        ]
        if let from = change.from { out["CLAWDLINE_PREV_STATE"] = name(from) }
        if let cwd = change.session.cwd, !cwd.isEmpty { out["CLAWDLINE_CWD"] = cwd }
        if case .working(let line) = change.to {
            let trimmed = line.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty { out["CLAWDLINE_LINE"] = trimmed }
        }
        if let claudeSession, !claudeSession.isEmpty {
            out["CLAWDLINE_CLAUDE_SESSION"] = claudeSession
        }
        out["CLAWDLINE_EVENT_JSON"] = json(out)
        return out
    }

    /// The same fields as one object: `CLAWDLINE_SESSION_ID` becomes `session_id`. For anything
    /// that would rather parse once than read nine variables, and for the languages where
    /// reaching into the environment is the awkward half.
    private static func json(_ vars: [String: String]) -> String {
        var obj: [String: String] = [:]
        for (key, value) in vars where key.hasPrefix("CLAWDLINE_") {
            obj[String(key.dropFirst("CLAWDLINE_".count)).lowercased()] = value
        }
        guard let data = try? JSONSerialization.data(
            withJSONObject: obj, options: [.sortedKeys, .withoutEscapingSlashes]),
            let text = String(data: data, encoding: .utf8) else { return "{}" }
        return text
    }

    // MARK: - Listening

    /// The key this hangs itself off ``SessionWatch`` under. Keyed rather than appended, so
    /// registering twice replaces rather than doubles — which matters here more than anywhere
    /// else, since doubling would run somebody's notifier twice per event.
    private static let observerKey = "statehook"

    /// The last reading, ours rather than the watch's.
    ///
    /// It has to be ours: by the time an observer is called ``SessionWatch`` has already replaced
    /// what it holds, so the previous reading exists nowhere else. Main thread only, like
    /// everything else in this section — observers are called there, and a lock for a dictionary
    /// only one thread ever touches would be a lock explaining nothing.
    private static var previous: [String: SessionState] = [:]

    /// When each session started the turn it is in the middle of.
    ///
    /// Only exists to answer "was this one long enough to be worth a buzz". Keyed by session id
    /// and cleared the moment the turn ends, so a machine that has been up for a week holds one
    /// entry per session that is working right now and nothing else.
    private static var startedWorking: [String: Date] = [:]

    /// How long a turn has to have run before finishing it is news.
    ///
    /// Two minutes, because that is roughly the point where somebody stops watching. Under it you
    /// were still looking at the screen — the notch already told you, and a second telling is the
    /// same fact arriving late. Over it you had gone to do something else, which is the entire
    /// case for a notification.
    static let finishThreshold: TimeInterval = 120

    /// Start listening.
    ///
    /// Seeded from the reading that already happened, and that is the difference between switching
    /// this on and being told a lie. Without it, turning the hook on at three in the afternoon
    /// would compare a full reading against nothing — every session appearing at once — and while
    /// ``transitions(from:to:sessions:)`` refuses to call that a change, the seed makes the intent
    /// explicit at the only place it could be got wrong.
    static func observe() {
        previous = SessionWatch.shared.states
        SessionWatch.shared.observers[observerKey] = { react() }
    }

    /// Stop listening. Anything already running is left to finish: it is bounded by ``timeout``
    /// anyway, and killing somebody's notifier halfway through delivering a notification is worse
    /// than the second of tidiness it buys.
    static func stop() {
        SessionWatch.shared.observers.removeValue(forKey: observerKey)
        previous = [:]
    }

    /// What to call the project a session belongs to on a lock screen.
    static func projectName(for session: TargetSession) -> String {
        let fallback = session.assistant?.label ?? "Clawdline"
        guard let cwd = Targets.workingDirectory(of: session) else { return fallback }
        if let registry = ProjectIcon.row(forCwd: cwd), let label = registry["label"] as? String,
           !label.isEmpty {
            return label
        }
        let name = (cwd as NSString).lastPathComponent
        return name.isEmpty ? fallback : name
    }

    /// The two lines a session notification shows.
    ///
    /// The task is the title because it is the quickest way to tell several sessions in the same
    /// project apart. The project stays in the body beside the event, so the notification still
    /// says both where the work lives and what just happened.
    struct PushMessage: Equatable {
        let title: String
        let body: String
    }

    /// Pure formatting half, kept apart from ``projectName(for:)`` so the lock-screen wording can
    /// be checked without asking a terminal for its working directory.
    static func pushMessage(for session: TargetSession, project: String,
                            event: String) -> PushMessage {
        PushMessage(title: session.displayLabel, body: "\(project) \(event)")
    }

    private static func sendPush(for session: TargetSession, event: String) {
        let message = pushMessage(for: session, project: projectName(for: session), event: event)
        WebPush.send(title: message.title,
                     body: message.body,
                     url: "/#session=\(session.id)",
                     tag: session.id)
    }

    private static func react() {
        let states = SessionWatch.shared.states
        let sessions = SessionWatch.shared.targets
        // Before anything else, and even when the feature is off. A previous reading that stops
        // being updated while nothing is configured is a stale baseline, and the moment somebody
        // filled the setting in they would get a burst of transitions out of whatever the sessions
        // were doing when they last edited their config.
        defer { previous = states }

        let changes = transitions(from: previous, to: states, sessions: sessions)

        // A phone, buzzing, for the one state that is worth it.
        //
        // **Only `waiting`.** "It finished" is the tempting one and it is the wrong one: it
        // happens dozens of times a day, and not being told costs nothing — it will still be
        // finished when you next look. A question with nobody answering it is the only state that
        // costs something for every second it goes unnoticed, and a notification that fires for
        // everything trains somebody who looks at none of them.
        //
        // Above the guard below on purpose: sending to a phone has nothing to do with whether
        // this machine has a command configured to run.
        for change in changes where change.to == .waiting {
            sendPush(for: change.session, event: L.t.pushWaiting)
        }

        // "It finished" — off unless asked for, and thresholded even then. See `finishThreshold`
        // for why the unthresholded version is the mistake.
        let now = Date()
        for change in changes {
            if case .working = change.to { startedWorking[change.session.id] = now; continue }
            guard let began = startedWorking.removeValue(forKey: change.session.id) else { continue }
            guard Config.shared.pushOnFinish, change.to == .idle else { continue }
            guard now.timeIntervalSince(began) >= finishThreshold else { continue }
            sendPush(for: change.session, event: L.t.pushFinished)
        }

        let argv = Config.shared.onStateChange
        guard !argv.isEmpty else { return }
        for change in changes {
            fire(change, argv: argv)
        }
    }

    // MARK: - Running one

    /// How many can be in flight at once. Four is above anything a working setup produces — a
    /// hook that exits in twenty milliseconds is never joined by a second — so reaching it is
    /// itself the signal that something is stuck.
    static let concurrencyLimit = 4

    /// How long one gets. Ten seconds is absurdly generous for something that is supposed to post
    /// a message, and short enough that four wedged children cannot hold the gate shut for a
    /// meaningful stretch of an afternoon.
    static let timeout: TimeInterval = 10

    /// Between `terminate()` and `SIGKILL`. Anything that has ignored a SIGTERM for two seconds
    /// is not going to answer the third one either.
    static let grace: TimeInterval = 2

    /// Main thread only. See ``previous``.
    private static var inFlight = 0
    private static var dropped = 0

    private static func fire(_ change: Change, argv: [String]) {
        guard inFlight < concurrencyLimit else {
            dropped += 1
            Log.write("statehook: dropped \(name(change.to)) for \(change.session.displayLabel)"
                + " — \(inFlight) already running (\(dropped) dropped so far)")
            return
        }
        inFlight += 1
        // Taken here, on the main thread, because that is the only thread that writes them —
        // the same reason `SessionWatch.read` takes its copy of the notes before going wide.
        let claudeSession = Config.shared.hooks ? HookBridge.note(for: change.session)?.session : nil

        DispatchQueue.global(qos: .utility).async {
            // Off the main thread because this can cost a `ps` and an `lsof` on a cold cache.
            // Worth paying: a hook that is told which repository it is about can route to a
            // channel, and one that is not can only say "a session finished".
            var session = change.session
            if session.cwd?.isEmpty != false {
                session.cwd = Targets.workingDirectory(of: session)
            }
            let full = Change(session: session, from: change.from, to: change.to)
            launch(argv, environment(for: full, claudeSession: claudeSession), change: full)
        }
    }

    private static func done() {
        DispatchQueue.main.async { inFlight = max(0, inFlight - 1) }
    }

    private static func launch(_ argv: [String], _ added: [String: String], change: Change) {
        // Every element, not only the first: `["node", "~/bin/notify.mjs"]` is the shape this is
        // meant for, and the interesting `~` is in the argument.
        let expanded = argv.map(Paths.expand)
        guard let first = expanded.first, !first.isEmpty else { done(); return }
        guard let program = resolve(first) else {
            // The commonest way for this feature to do nothing, so it says so by name. An app
            // launched from Finder has the launchd PATH — `/usr/bin:/bin:/usr/sbin:/sbin` — and
            // no amount of `.zshrc` changes that, which is why `resolve` looks where it looks.
            Log.write("statehook: cannot find \(first) — set on_state_change[0] to a full path")
            done()
            return
        }

        let task = Process()
        task.executableURL = URL(fileURLWithPath: program)
        task.arguments = Array(expanded.dropFirst())
        // Added to the inherited environment rather than replacing it. A hook that cannot see
        // HOME or PATH is a hook that cannot find its own config file, and "your program runs in
        // an empty environment" is a surprise nobody should have to discover by debugging.
        var env = ProcessInfo.processInfo.environment
        for (key, value) in added { env[key] = value }
        env["PATH"] = Self.usefulPath(env["PATH"])
        task.environment = env
        // All three at /dev/null. Nothing here reads the output, and a pipe nobody drains is a
        // child that blocks forever the moment it writes 64KB — which is a wedge introduced by
        // the act of collecting output we had already decided to throw away.
        task.standardInput = FileHandle.nullDevice
        task.standardOutput = FileHandle.nullDevice
        task.standardError = FileHandle.nullDevice

        // Built before it can be needed, and cancelled by the termination handler — so a child
        // that exits in twenty milliseconds costs one cancelled work item and nothing else.
        // Only the child itself is signalled: it is spawned into this app's process group, so
        // killing the group would kill the app.
        let killer = DispatchWorkItem {
            guard task.isRunning else { return }
            let pid = task.processIdentifier
            Log.write("statehook: \(program) outlived \(Int(timeout))s — terminating (pid \(pid))")
            task.terminate()
            DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + grace) {
                if task.isRunning { kill(pid, SIGKILL) }
            }
        }
        // Weakly, and that is not a formality: the work item holds the process so it has
        // something to signal, so a handler holding the work item back would be a cycle, and
        // neither object would ever be released — a leaked `Process` per hook, for as long as the
        // app runs. Whichever of the two fires, the item is alive at that moment: until `run()`
        // returns this frame holds it, and after that the queue does until the deadline.
        task.terminationHandler = { [weak killer] _ in
            killer?.cancel()
            done()
        }

        do {
            try task.run()
        } catch {
            // No termination handler runs for a process that never started, so the gate has to be
            // opened here or four failures would close this feature for the rest of the session.
            Log.write("statehook: \(program) would not start — \(error.localizedDescription)")
            done()
            return
        }
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + timeout, execute: killer)

        let was = change.from.map(name) ?? "?"
        Log.write("statehook: \(was) → \(name(change.to)) \(change.session.displayLabel) — \(program)")
    }

    /// Where the program named in element 0 actually is.
    ///
    /// `Process` wants a path, and the setting is allowed to say `node`. Resolving it here rather
    /// than handing the line to `/usr/bin/env` is the same decision as everywhere else in this
    /// file — env would need a shell's PATH to be any use, and this app does not have one. A GUI
    /// application inherits launchd's environment, not a login shell's, so `/opt/homebrew/bin` is
    /// missing from PATH on precisely the machines where everything interesting is installed
    /// there. Hence the extra places, searched after whatever PATH does say.
    static func resolve(_ program: String) -> String? {
        let fm = FileManager.default
        // Anything with a slash is a path and is not searched for: `./notify.sh` failing to exist
        // should be an error about that file, not a hunt for one of the same name in /usr/bin.
        if program.contains("/") {
            return fm.isExecutableFile(atPath: program) ? program : nil
        }
        for dir in searchPath {
            let candidate = dir + "/" + program
            if fm.isExecutableFile(atPath: candidate) { return candidate }
        }
        return nil
    }

    private static var searchPath: [String] {
        let inherited = (ProcessInfo.processInfo.environment["PATH"] ?? "")
            .split(separator: ":").map(String.init)
        let extras = ["/opt/homebrew/bin", "/usr/local/bin",
                      Paths.expand("~/.local/bin"), Paths.expand("~/.bun/bin"),
                      "/usr/bin", "/bin"]
        var seen = Set<String>()
        return (inherited + extras).filter { !$0.isEmpty && seen.insert($0).inserted }
    }
}
