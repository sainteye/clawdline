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
                          model: String? = nil, permission: Permission = .ask,
                          addDir: String? = nil, resume: String? = nil) -> String {
        "cd " + Project.shellQuoted(cwd) + " && "
            + assistant.command(model: modelName(model), permission: permission,
                                addDir: extraDir(addDir), resume: sessionName(resume))
    }

    /// A directory a session may be given reach over, or nil for anything that is not one.
    ///
    /// The only caller is the orchestrator and the only value it passes is `/tmp/.clawdline/<id>`,
    /// where `<id>` has already been through `Orchestrator.isTaskID`. So this is not where that
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
    /// upstream, where somebody is still holding the request: `Orchestrator.draft` runs the same
    /// check and answers `bad_task`.
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
        case tmux
        /// A terminal this can drive, that is not open. The bundle id.
        case notRunning(app: String)
        /// The terminal Settings names is not one this can drive, and there is no tmux to reach
        /// it through. The bundle id.
        case cannotDrive(app: String)
    }

    static let itermBundleID = "com.googlecode.iterm2"

    /// Read off the app list in Settings, which is the only place this app is told which terminal
    /// somebody uses.
    ///
    /// Two paths exist and only two: **iTerm2**, which has an AppleScript surface that can open a
    /// tab and write into it, and **tmux**, which is how every other terminal works here — see
    /// ``Tmux``. Anything else is refused by name rather than quietly handed to iTerm2, because a
    /// session that opened somewhere the person was not looking is worse than a sentence saying
    /// it did not open.
    ///
    /// An empty scope means the hotkey is global, which says nothing about which terminal is in
    /// use — so it reads as *no preference*, and no preference is iTerm2 first: the same order
    /// every other terminal operation in this app has always used.
    static func plan(scope: String, running: Set<String>, hasTmux: Bool) -> Plan {
        let ids = scope.split(separator: ",")
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
        func isITerm(_ id: String) -> Bool {
            id.caseInsensitiveCompare(itermBundleID) == .orderedSame
        }
        if ids.isEmpty || ids.contains(where: isITerm) {
            if running.contains(where: isITerm) { return .iterm }
            // iTerm2 is wanted and shut. tmux is not a fallback *to* iTerm2 — it is the other
            // real backend, and a pane in it is a session that exists whether or not anything is
            // attached to it.
            if hasTmux { return .tmux }
            return .notRunning(app: itermBundleID)
        }
        if hasTmux { return .tmux }
        return .cannotDrive(app: ids[0])
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
        case started(id: String, backend: Backend)
        /// `app` is the terminal the refusal is about, when it is about one. It rides next to the
        /// code in the error envelope so a page can name it in a sentence of its own rather than
        /// having to show the English one.
        case refused(status: Int, code: String, message: String, app: String?)
    }

    /// Open a tab where that place is and run an assistant in it.
    ///
    /// **No focus is taken.** The person who asked for this is holding a phone; the person at the
    /// Mac, if there is one, is in the middle of something else. `iterm.js` opens the tab without
    /// calling `activate`, so iTerm2 stays wherever it was in the window order. A window is only
    /// created when there is not one already, and that is the one case where something may come
    /// forward — there is no way to make a window and not have it be a window.
    static func start(_ place: Place, assistant: Assistant = .claude,
                      model: String? = nil, permission: Permission = .ask,
                      addDir: String? = nil, resume: String? = nil) -> Outcome {
        guard usable(place.path), isDirectory(place.path) else {
            return .refused(status: 404, code: "not_found",
                            message: "No place named that", app: nil)
        }
        switch plan(scope: Config.shared.scopeApp, running: runningApps(), hasTmux: tmuxIsUp()) {
        case .iterm:
            guard let made = ITerm.newTab(line: itermLine(cwd: place.path,
                                                          assistant: assistant,
                                                          model: model,
                                                          permission: permission,
                                                          addDir: addDir,
                                                          resume: resume)) else {
                return .refused(status: 502, code: "internal",
                                message: "iTerm2 would not open a tab.", app: nil)
            }
            return .started(id: made.id, backend: .iterm)

        case .tmux:
            // Nothing is quoted here and nothing needs to be: tmux is given a working directory
            // and a command as separate arguments of a subprocess, so there is no line for a
            // directory name to break out of. The command reaching a shell one level down is why
            // `modelName` is a closed alphabet rather than an escaping rule — there is nothing
            // in a name it admits for that shell to read.
            guard let pane = Tmux.newWindow(cwd: place.path,
                                            command: assistant.command(model: modelName(model),
                                                                       permission: permission,
                                                                       addDir: extraDir(addDir),
                                                                       resume: sessionName(resume))) else {
                return .refused(status: 502, code: "internal",
                                message: "tmux would not open a window.", app: nil)
            }
            return .started(id: pane, backend: .tmux)

        case .notRunning(let app):
            let name = appName(app)
            return .refused(status: 409, code: "terminal_closed",
                            message: "\(name) is not running, and this will not launch it for "
                                   + "you. Open it on the Mac and try again.", app: name)

        case .cannotDrive(let app):
            let name = appName(app)
            return .refused(status: 409, code: "terminal_unsupported",
                            message: "\(name) is the terminal in Settings, and a session cannot "
                                   + "be started in it directly. Run tmux there and this works "
                                   + "— see docs/remote.md.", app: name)
        }
    }

    /// Open a tab where that place is and pick a recorded conversation back up in it.
    ///
    /// The second named action, and deliberately a thin one: everything that decides *where* and
    /// *whether* is ``start(_:assistant:model:permission:addDir:resume:)``, unchanged, and this
    /// adds one literal flag and one id that has already been proved to name a file on this Mac.
    /// A separate entry point rather than an argument on the route, because "start something
    /// here" and "carry on with that" are two different permissions to think about even though
    /// today they share a gate.
    static func resume(_ place: Place, sessionID: String,
                       assistant: Assistant = .claude) -> Outcome {
        guard let id = sessionName(sessionID), past(withID: id, in: place) != nil else {
            return .refused(status: 404, code: "not_found",
                            message: "No conversation named that", app: nil)
        }
        return start(place, assistant: assistant, resume: id)
    }

    // MARK: - Conversations already recorded here

    /// One conversation Claude Code has already written down in a place.
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
    /// Only Claude Code. Codex records the same conversations somewhere else and names its
    /// threads through its app-server rather than in the file, so listing those means starting a
    /// process per listing — a different day's work, and not one this list should wait on.
    ///
    /// Only the top level of the project folder. Subagents' transcripts live in a directory
    /// beside them named for their parent, and a sidechain is not a conversation anybody had.
    /// `dir` and `open` are parameters so a test can describe a project folder and a set of
    /// live transcripts instead of having to produce either. Neither is passed in the app.
    static func past(in place: Place, limit: Int = 40,
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
        }.sorted { $0.at > $1.at }.prefix(limit)

        let open = open ?? openTranscripts()
        return files.compactMap { file in
            guard let title = name(ofTranscript: file.url) else { return nil }
            return Past(id: file.id, title: title, at: file.at,
                        live: open.contains(file.url.resolvingSymlinksInPath().path))
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
        return past(in: place, limit: 200, dir: dir, open: open).first { $0.id == id }
    }

    /// What to call a transcript, or nothing when it has nothing worth calling it by.
    ///
    /// The title first — ``Transcript/title(ofTranscript:tailBytes:)`` already reads a rename in
    /// preference to Claude Code's own, and already remembers its answer against the file's size
    /// and mtime, so this pays for it once per file per change. Most transcripts have one: of the
    /// ninety-six in this repository's own project folder, eighty-four do.
    ///
    /// The rest fall back to the opening of the first thing typed into them, which is what a
    /// person would recognise it by anyway. **Nothing is invented** — a transcript with no title
    /// and nothing typed into it is a tab that opened and closed, and it is left out of the list
    /// rather than shown as an untitled row somebody has to guess at.
    static func name(ofTranscript url: URL) -> String? {
        if let title = Transcript.title(ofTranscript: url), !title.isEmpty { return title }
        return cachedOpening(of: url)
    }

    /// The first thing a person typed into a transcript, trimmed to a line.
    ///
    /// Read off the front rather than parsed, and only far enough in to pass the couple of
    /// bookkeeping records Claude Code writes before the first turn. Records with `isSidechain`
    /// or a `toolUseResult` are not somebody typing — they are an agent's turn and a tool's
    /// answer quoted back — so they are stepped over rather than shown as the session's name.
    static func opening(inText text: String, limit: Int = 80) -> String? {
        for line in text.split(separator: "\n") {
            guard line.contains("\"type\":\"user\"") else { continue }
            guard let data = line.data(using: .utf8),
                  let row = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  row["isSidechain"] as? Bool != true, row["toolUseResult"] == nil,
                  let message = row["message"] as? [String: Any] else { continue }
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
            guard !tidy.isEmpty else { continue }
            return tidy.count > limit ? String(tidy.prefix(limit)) + "…" : tidy
        }
        return nil
    }

    // MARK: - Plumbing for the conversation list

    private static var openings: [String: (signature: String, opening: String?)] = [:]

    /// Remembered against the file's signature, like the title is. A transcript's opening cannot
    /// change while its first bytes stay where they are, and the files this reads from are the
    /// dozen without a title — the ones a fresh read would otherwise cost most on, because there
    /// is nothing in them to stop looking at.
    private static func cachedOpening(of url: URL) -> String? {
        let sig = Transcript.signature(of: url)
        lock.lock()
        if let hit = openings[url.path], hit.signature == sig {
            lock.unlock()
            return hit.opening
        }
        lock.unlock()

        let found = head(of: url, bytes: 128 << 10).flatMap { opening(inText: $0) }
        lock.lock()
        openings[url.path] = (sig, found)
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
        let sessions = Thread.isMainThread
            ? SessionWatch.shared.targets
            : DispatchQueue.main.sync { SessionWatch.shared.targets }
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
        tidy(recorded() + codexRecorded() + live(), limit: limit)
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
        let temporary = resolved(temporary ?? NSTemporaryDirectory())
        if path == home { return false }
        if isInside(path, resolved(home + "/.claude")) { return false }
        if isInside(path, resolved(home + "/.codex")) { return false }
        if isInside(path, resolved(home + "/Documents/Codex")) { return false }
        if isInside(path, temporary) { return false }
        // `NSTemporaryDirectory` normally resolves one per-user /var/folders root. These cover
        // deliberate /tmp worktrees and the `/private` spelling macOS reports through `lsof`.
        if isInside(path, resolved("/tmp")) || isInside(path, resolved("/private/tmp")) {
            return false
        }
        return true
    }

    /// The directories clawdline can already see a session in.
    ///
    /// These are trusted enough to type into — the send route does it all day — so they are
    /// trusted enough to start another one in. They also cover the case the recorded lists cannot:
    /// a directory somebody opened an assistant in five seconds ago, before anything was written
    /// down.
    static func live(now: Date = Date()) -> [Place] {
        let sessions = Thread.isMainThread
            ? SessionWatch.shared.targets
            : DispatchQueue.main.sync { SessionWatch.shared.targets }
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
    static func recorded(root: URL? = nil, folders: Int = 60) -> [Place] {
        let base = root ?? projectsRoot
        let fm = FileManager.default
        guard let names = try? fm.contentsOfDirectory(atPath: base.path) else { return [] }

        // Newest folders first and capped, so a machine with a thousand projects on it does not
        // pay for all of them to answer one menu.
        let dirs = names.compactMap { name -> (name: String, url: URL, at: Date)? in
            let url = base.appendingPathComponent(name, isDirectory: true)
            var isDirectory: ObjCBool = false
            guard fm.fileExists(atPath: url.path, isDirectory: &isDirectory),
                  isDirectory.boolValue else { return nil }
            return (name, url, modified(url))
        }.sorted { $0.at > $1.at }.prefix(folders)

        var out: [Place] = []
        for dir in dirs {
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

    /// Which applications are open, so ``plan(scope:running:hasTmux:)`` can be told rather than
    /// having to ask. On the main thread, because `NSWorkspace` is.
    private static func runningApps() -> Set<String> {
        let read = { Set(NSWorkspace.shared.runningApplications.compactMap(\.bundleIdentifier)) }
        return Thread.isMainThread ? read() : DispatchQueue.main.sync(execute: read)
    }

    /// Whether there is a tmux **server** to open a window on, not merely a tmux binary. Without
    /// a server `new-window` has nothing to add to, and a session nobody can attach to is not a
    /// session anybody asked for.
    private static func tmuxIsUp() -> Bool {
        Tmux.binary != nil && !Tmux.panes().isEmpty
    }
}
