import Foundation

/// Claude Code telling us what it is doing, instead of us reading it off the screen.
///
/// Everything else here works by looking. ``Activity`` recognises the spinner, ``SessionState``
/// recognises the shape of a menu, and that has one property nothing can improve on: it works on
/// sessions that were started before this app was installed, it needs nothing added to anybody's
/// configuration, and it cannot break what it is reading. It also has a cost it cannot avoid —
/// it only knows what it has looked at, and looking costs a round trip to a terminal. Away from
/// the panel that is once every twenty seconds, which is a long time to leave a permission
/// dialog sitting there unnoticed.
///
/// Claude Code will say so itself, if asked. A hook is a command it runs at a moment it cares
/// about; the one this installs writes a one-line note and exits. The app watches the directory
/// those notes land in, so an interesting moment arrives in milliseconds, and the polling
/// underneath stays exactly where it was.
///
/// **The screen stays the authority, and that is the design rather than a hedge.** A note says
/// *when* something happened and never *what is on the screen* — `Notification` fires both for a
/// permission request and for a session that has merely been quiet for a minute, and to anybody
/// reading the list those are not the same state. So a note's job is mostly to say "look now",
/// and the reading that follows is the same reading as always. **No note asserts that a session
/// is working**, which is a narrowing that came out of measuring rather than out of caution — see
/// `merge`. The one thing a note does settle is something the screen gets wrong rather than
/// misses: Claude Code does not always erase its live line when a fast turn ends, so a capture
/// taken a moment later can find one and call a finished session busy. A `Stop` overrides that,
/// briefly.
///
/// Which is why the fallback is not a degraded mode. With nothing installed this file does
/// nothing at all, and every reading still arrives — one poll later.
enum HookBridge {

    /// The moments worth being told about.
    ///
    /// Five, all of them rare. That is deliberate: a hook on `PreToolUse` would fire hundreds of
    /// times an hour and put this script on the critical path of every tool call somebody's agent
    /// makes, to tell us something the pair below already brackets. `UserPromptSubmit` and `Stop`
    /// are the two ends of a turn, and between them there is nothing left to ask.
    ///
    /// `SubagentStop` is missing for the same reason it would be wrong: a subagent finishing is
    /// not the session finishing, and treating it as one would call a session idle while its main
    /// agent was still working.
    enum Event: String, CaseIterable {
        case sessionStart = "SessionStart"
        case userPromptSubmit = "UserPromptSubmit"
        case stop = "Stop"
        case notification = "Notification"
        case sessionEnd = "SessionEnd"
    }

    /// One note, as the script left it.
    struct Note: Equatable {
        let event: Event
        let tty: String
        let at: Date
        /// Claude Code's own id for the session, which is also the name of its transcript file.
        /// Worth carrying for that alone — see ``Transcript/locate(cwd:tabTitle:startedAt:sessionID:)``.
        let session: String?
    }

    // MARK: - Where everything lives

    private static var configDirectory: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/clawdline", isDirectory: true)
    }

    /// The script, copied out of the app rather than run from inside it.
    ///
    /// A path in the bundle would be a path that changes when somebody moves the app to
    /// /Applications, and one that does not exist for the second and a half a rebuild takes to
    /// replace it. Neither is a good thing to have written into another program's settings file.
    static var scriptURL: URL { configDirectory.appendingPathComponent("hook.sh") }

    /// Where the notes land. One file per tty, overwritten in place.
    static var notesDirectory: URL { configDirectory.appendingPathComponent("hooks", isDirectory: true) }

    static var settingsURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".claude/settings.json")
    }

    // MARK: - Reading

    /// The newest note from each session, by bare tty — `ttys004`, the same key
    /// ``TargetSession/tty`` carries with `/dev/` on the front.
    private(set) static var notes: [String: Note] = [:]

    /// Called on the main thread when a note lands. Somebody hangs a reading off this.
    static var onNote: (() -> Void)?

    private static var source: DispatchSourceFileSystemObject?
    private static var watchFD: Int32 = -1

    /// Start listening. Cheap and harmless when nothing is installed: it makes a directory and
    /// watches it, and an empty directory produces no notes.
    static func start() {
        try? FileManager.default.createDirectory(at: notesDirectory,
                                                 withIntermediateDirectories: true)
        refreshScriptIfStale()
        reload()
        watch()
    }

    /// Watch the directory rather than each file: the script writes to a temporary name and moves
    /// it into place, so the file a watcher had open is never the file that gets the new content.
    /// A directory sees the move.
    private static func watch() {
        source?.cancel()
        if watchFD >= 0 { close(watchFD) }
        watchFD = open(notesDirectory.path, O_EVTONLY)
        guard watchFD >= 0 else { return }
        let s = DispatchSource.makeFileSystemObjectSource(
            fileDescriptor: watchFD, eventMask: [.write, .rename, .delete],
            queue: DispatchQueue.global(qos: .utility))
        s.setEventHandler { coalesce() }
        s.setCancelHandler { }
        s.resume()
        source = s
    }

    /// A turn can land two notes at once — `Stop` from one session while another is starting —
    /// and one directory event per file would mean re-reading the directory twice and asking
    /// every terminal what it is doing twice. A tick of delay collapses that into one.
    private static var pending = false
    private static func coalesce() {
        DispatchQueue.main.async {
            guard !pending else { return }
            pending = true
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.04) {
                pending = false
                reload()
                onNote?()
            }
        }
    }

    static func reload() {
        let fm = FileManager.default
        guard let names = try? fm.contentsOfDirectory(atPath: notesDirectory.path) else {
            notes = [:]
            return
        }
        var found: [String: Note] = [:]
        for name in names where name.hasSuffix(".json") && !name.hasPrefix(".") {
            let url = notesDirectory.appendingPathComponent(name)
            guard let data = try? Data(contentsOf: url), let note = parse(data) else { continue }
            found[note.tty] = note
        }
        notes = found
    }

    static func parse(_ data: Data) -> Note? {
        guard let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let raw = obj["event"] as? String, let event = Event(rawValue: raw),
              let tty = obj["tty"] as? String, !tty.isEmpty,
              let at = obj["at"] as? Double else { return nil }
        let session = (obj["session"] as? String).flatMap { $0.isEmpty ? nil : $0 }
        return Note(event: event, tty: tty, at: Date(timeIntervalSince1970: at), session: session)
    }

    /// The note for a session, if it left one.
    static func note(for session: TargetSession) -> Note? {
        notes[session.tty.replacingOccurrences(of: "/dev/", with: "")]
    }

    // MARK: - What a note is allowed to change

    /// How long a `Stop` is allowed to override a screen that still shows a spinner.
    ///
    /// Short on purpose. The problem it exists for is a stale line — Claude Code does not always
    /// erase the spinner when a fast turn ends, so a capture taken a moment later can still find
    /// one and call a finished session busy. That is a matter of a second or two. Anything
    /// genuinely running for longer than this window will have started with a `UserPromptSubmit`
    /// of its own, which replaces the note; so the only thing a longer window could buy is the
    /// chance to call a working session idle, which is the worst answer this can give.
    static let stopWindow: TimeInterval = 10

    /// The readings, with what the notes know folded in.
    ///
    /// Pure, and takes its clock as an argument, because the interesting cases here are all about
    /// *when* — and a test that cannot say what time it is can only check the easy half.
    static func merge(_ notes: [String: Note], into states: [String: SessionState],
                      sessions: [TargetSession], now: Date = Date()) -> [String: SessionState] {
        guard !notes.isEmpty else { return states }
        var out = states
        for session in sessions {
            let bare = session.tty.replacingOccurrences(of: "/dev/", with: "")
            guard let note = notes[bare] else { continue }
            let screen = states[session.id] ?? .unknown

            // A question on screen outranks everything. It is the one state read from a shape
            // rather than inferred from a moment, it is the only one that costs somebody
            // something per second, and no note ever contradicts it usefully — `Notification`
            // fires for a permission request and for a minute of quiet alike.
            if screen == .waiting { continue }

            switch note.event {
            case .userPromptSubmit:
                // Nothing. A turn beginning is a moment worth looking at, not a state to assert.
                //
                // It used to assert one, and measuring took it back out. Claude Code draws its
                // live line about **two seconds** after you press Return, and while plain text is
                // coming back it draws no line at all — so a claim short enough to be safe covers
                // almost none of a turn, and one long enough to cover a turn cannot be retracted
                // when you press Esc, because cancelling fires no hook at all. Herdr shipped the
                // long version and had to take it out again; their issue #249 is this exact bug.
                //
                // The burst in `SessionWatch.nudge` does the same job without the claim: look
                // now, and look again once the line has had time to appear.
                continue
            case .stop, .sessionEnd:
                // The turn is over, whatever was left on the screen.
                if case .working = screen, now.timeIntervalSince(note.at) < stopWindow {
                    out[session.id] = .idle
                } else if screen == .unknown {
                    out[session.id] = .idle
                }
            case .notification, .sessionStart:
                // These only ask us to look. The reading that just happened is the answer.
                continue
            }
        }
        return out
    }

    // MARK: - Installing

    /// True when every one of our events is wired up in `~/.claude/settings.json`.
    static var isInstalled: Bool {
        let hooks = installedHooks()
        return Event.allCases.allSatisfy { event in
            (hooks[event.rawValue] as? [[String: Any]] ?? []).contains { group in
                (group["hooks"] as? [[String: Any]] ?? []).contains { isOurs($0) }
            }
        }
    }

    /// When a note last arrived, from the newest one on disk. The difference between "installed"
    /// and "installed and actually running" is the only question worth putting in the settings
    /// window, and it is the one a checkbox cannot answer.
    static var lastHeard: Date? { notes.values.map(\.at).max() }

    private static func installedHooks() -> [String: Any] {
        guard let data = try? Data(contentsOf: settingsURL),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return [:]
        }
        return obj["hooks"] as? [String: Any] ?? [:]
    }

    /// Ours, by the path it points at. Loose enough to still recognise an entry written when the
    /// home directory had a different name, which is what an uninstall has to survive.
    static func isOurs(_ hook: [String: Any]) -> Bool {
        guard let command = hook["command"] as? String else { return false }
        return command.contains("clawdline/hook.sh")
    }

    /// The same settings with our five events wired in, and everything else exactly as it was.
    ///
    /// Pure, and separate from the file handling, because this is the part that can be wrong in a
    /// way nobody notices until it has eaten somebody's configuration. What it has to get right
    /// is not "does it add ours" — it is "does a `PreToolUse` entry belonging to a plugin come
    /// back out the other side", and that is a question a test can ask.
    static func adding(_ script: String, to settings: [String: Any]) -> [String: Any] {
        var settings = settings
        var hooks = settings["hooks"] as? [String: Any] ?? [:]
        for event in Event.allCases {
            // Ours dropped first, so installing twice leaves one of each rather than two.
            var groups = (hooks[event.rawValue] as? [[String: Any]] ?? []).filter { group in
                !(group["hooks"] as? [[String: Any]] ?? []).contains(where: isOurs)
            }
            groups.append([
                "hooks": [[
                    "type": "command",
                    "command": "\(shellQuoted(script)) \(event.rawValue)",
                    // Generous next to the eleven milliseconds it takes, and it is the ceiling
                    // on how long Claude Code could ever be made to wait for this.
                    "timeout": 5,
                ]],
            ])
            hooks[event.rawValue] = groups
        }
        settings["hooks"] = hooks
        return settings
    }

    /// The same settings with ours taken out, and nothing else touched.
    static func removing(from settings: [String: Any]) -> [String: Any] {
        var settings = settings
        guard var hooks = settings["hooks"] as? [String: Any] else { return settings }
        for (event, value) in hooks {
            guard let groups = value as? [[String: Any]] else { continue }
            let kept = groups.filter { group in
                !(group["hooks"] as? [[String: Any]] ?? []).contains(where: isOurs)
            }
            // An event left with nothing in it goes away rather than sitting there as an empty
            // array: the file should read as though this had never touched it.
            //
            // `removeValue` rather than assigning nil, and the difference is not style. The
            // values here are `Any`, and `Any` will happily hold an Optional — so assigning nil
            // to one of these keys stores a boxed nothing under it instead of taking the key
            // out, and the file grows a `"Stop": null` that was never there.
            if kept.isEmpty { hooks.removeValue(forKey: event) } else { hooks[event] = kept }
        }
        if hooks.isEmpty { settings.removeValue(forKey: "hooks") } else { settings["hooks"] = hooks }
        return settings
    }

    /// Wire the five events into `~/.claude/settings.json`. nil means it worked.
    ///
    /// Everything already in that file is read, changed and written back — this is somebody's
    /// own configuration and the app is a guest in it. A copy is kept the first time, next to the
    /// original, because "it edited my settings and I do not know what it did" is a thing to be
    /// able to answer with a diff rather than an assurance.
    static func install() -> String? {
        let fm = FileManager.default
        do {
            try fm.createDirectory(at: notesDirectory, withIntermediateDirectories: true)
            try writeScript()

            var settings = (try? Data(contentsOf: settingsURL))
                .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]
            if fm.fileExists(atPath: settingsURL.path) {
                let backup = settingsURL.deletingLastPathComponent()
                    .appendingPathComponent("settings.json.before-clawdline")
                if !fm.fileExists(atPath: backup.path) {
                    try? fm.copyItem(at: settingsURL, to: backup)
                }
            }

            settings = adding(scriptURL.path, to: settings)

            try fm.createDirectory(at: settingsURL.deletingLastPathComponent(),
                                   withIntermediateDirectories: true)
            let data = try JSONSerialization.data(
                withJSONObject: settings,
                options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes])
            try data.write(to: settingsURL, options: .atomic)
            Log.write("hooks: installed into \(settingsURL.path)")
            reload()
            return nil
        } catch {
            Log.write("hooks: install failed — \(error.localizedDescription)")
            return error.localizedDescription
        }
    }

    /// Take ours back out and leave everything else. nil means it worked.
    static func uninstall() -> String? {
        do {
            guard let data = try? Data(contentsOf: settingsURL),
                  let existing = try JSONSerialization.jsonObject(with: data) as? [String: Any]
            else { return nil }
            let settings = removing(from: existing)

            let out = try JSONSerialization.data(
                withJSONObject: settings,
                options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes])
            try out.write(to: settingsURL, options: .atomic)
            try? FileManager.default.removeItem(at: notesDirectory)
            try? FileManager.default.createDirectory(at: notesDirectory,
                                                     withIntermediateDirectories: true)
            notes = [:]
            Log.write("hooks: removed from \(settingsURL.path)")
            return nil
        } catch {
            Log.write("hooks: uninstall failed — \(error.localizedDescription)")
            return error.localizedDescription
        }
    }

    /// Keep the installed copy level with the one in the app, so an update that changes the
    /// script does not need somebody to press Install again to get it.
    private static func refreshScriptIfStale() {
        guard FileManager.default.fileExists(atPath: scriptURL.path),
              let bundled = bundledScript(),
              let installed = try? String(contentsOf: scriptURL, encoding: .utf8),
              installed != bundled else { return }
        try? writeScript()
        Log.write("hooks: script refreshed")
    }

    private static func bundledScript() -> String? {
        guard let url = Bundle.main.url(forResource: "clawdline-hook", withExtension: "sh"),
              let text = try? String(contentsOf: url, encoding: .utf8) else { return nil }
        return text
    }

    private static func writeScript() throws {
        guard let text = bundledScript() else {
            throw NSError(domain: "clawdline", code: 1,
                          userInfo: [NSLocalizedDescriptionKey: L.t.scriptMissing])
        }
        try text.write(to: scriptURL, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755],
                                              ofItemAtPath: scriptURL.path)
    }

    /// A path going into a shell command line. Homes with a space in them exist, and so do
    /// the people who own them.
    static func shellQuoted(_ path: String) -> String {
        "'" + path.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }
}
