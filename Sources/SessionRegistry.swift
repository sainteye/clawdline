import Foundation

/// Claude Code's own account of what each of its sessions is doing, read from the files it keeps
/// for itself.
///
/// Everything else that answers this question is shape recognition. ``Activity`` knows the
/// spinner, ``SessionState`` knows what a menu looks like, and both are reading a screen nobody
/// promised to keep still. ``HookBridge`` improved on that by getting Claude Code to say when
/// something happened — but only for people who let this app edit their `settings.json`, only
/// for sessions started after they did, and even then a note says *when*, never *what*.
///
/// This says what. Every Claude Code session writes `~/.claude/sessions/<pid>.json` about itself
/// and keeps the `status` field in it current — `idle`, `busy`, `waiting`, and when it is
/// waiting, `waitingFor` says what for. It is the same data `claude agents --json` prints, it is
/// the one dependency in this app with a **documented field table**
/// (`code.claude.com/docs/en/agent-view`, "List sessions as JSON") and the one with a **version
/// number**, `peerProtocol`. Nothing has to be installed and nothing has to be restarted: the
/// files are already there, for every session already open.
///
/// **What it does not carry is the question itself.** `waiting` says a person is being asked; it
/// does not say what the options are, and the phone needs buttons. So the division of labour is:
/// the registry decides *whether* a session is waiting, and the screen still supplies *what it is
/// being asked* — see ``waiting(in:sessions:)``, which hands the screen parser the same kind of
/// out-of-band fact a hook note does.
///
/// **The screen still wins on one point.** A menu actually recognised on the terminal is stronger
/// evidence than `busy`, because the registry can be a beat behind a dialog that has just been
/// drawn, and a question hidden behind a spinner is the one mistake here that costs somebody
/// something. See ``merge(_:into:sessions:)``.
///
/// Four ways this reaches nothing, all of which land back on exactly today's behaviour: the
/// directory is missing (an older Claude Code, or one of the cloud backends whose peer features
/// this rides along with, or a container); the file states a `peerProtocol` this build was not
/// written against; the `status` is a word this build does not know — Claude Code writes `shell`
/// while you are in `!` mode and the documentation does not list it; or ``Config/sessionRegistry``
/// is off. **Codex needs no mention anywhere in here**: it writes no such file, so its sessions
/// fall through every one of these gates on their own.
enum SessionRegistry {

    /// The protocol version of the registry files this build was written against.
    ///
    /// The only explicit version number anything in this app reads. A file that states a
    /// different one is not a file to guess at — Claude Code put a number there precisely so
    /// that a reader could stop.
    static let protocolVersion = 1

    /// What a session says it is doing.
    ///
    /// `other` is not an error case. Claude Code writes `shell` while somebody is in `!` mode,
    /// which is documented nowhere, and it will write words this build has never heard of. The
    /// answer to a word we do not know is to have no opinion and let the screen decide, which is
    /// what carrying it as a case rather than dropping it makes possible to say out loud.
    enum Status: Equatable {
        case idle
        case busy
        case waiting
        case other(String)

        init(_ raw: String) {
            switch raw {
            case "idle": self = .idle
            case "busy": self = .busy
            case "waiting": self = .waiting
            default: self = .other(raw)
            }
        }
    }

    /// One session's file, as it left it.
    struct Entry: Equatable {
        let pid: Int32
        /// Claude Code's own id for the conversation, which is also the name of its transcript
        /// file — see ``Transcript/locate(cwd:tabTitle:startedAt:sessionID:)``. Free here, and
        /// it removes the guessing that finding a transcript otherwise needs.
        let sessionID: String?
        let cwd: String?
        /// The name Claude Code gave the session, `clawdline-03` and the like.
        let name: String?
        let version: String?
        /// `nil` while the file exists but no status has been written into it yet, which is a
        /// real state a session passes through in its first few hundred milliseconds.
        let status: Status?
        /// Why it is waiting, when it is: `permission prompt`, `input needed`, `sandbox request`,
        /// `worker request`, `dialog open`. Not used to decide anything — a person is waiting
        /// whichever of those it is — and kept because it is the only place the reason exists.
        let waitingFor: String?
        /// When the process started, as `LC_ALL=C TZ=UTC ps -o lstart=` printed it. The whole
        /// point of it is ``isSameProcess(_:startedAt:)``.
        let procStart: String?
        let peerProtocol: Int
        let statusUpdatedAt: Date?
    }

    /// What this Mac can say about the process behind a session.
    ///
    /// Both halves are needed before a file may speak for a session. The pid says which file to
    /// read; the start time says whether that file is about *this* process or about a dead one
    /// whose number has since been handed out again.
    struct Process: Equatable {
        let pid: Int32
        /// `nil` when `ps` could not say, which is a reason to use nothing rather than a reason
        /// to trust the pid on its own.
        let started: Date?

        init(pid: Int32, started: Date?) {
            self.pid = pid
            self.started = started
        }
    }

    /// One round of registry reading: the files, and the process facts that decide which session
    /// each of them is allowed to speak for. Kept together because neither half means anything
    /// without the other.
    struct Reading: Equatable {
        var entries: [Int32: Entry] = [:]
        var processes: [String: Process] = [:]

        init(entries: [Int32: Entry] = [:], processes: [String: Process] = [:]) {
            self.entries = entries
            self.processes = processes
        }
    }

    // MARK: - Where everything lives

    static var directory: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".claude/sessions", isDirectory: true)
    }

    /// The files for a known set of pids.
    ///
    /// Named files rather than a directory listing, on purpose. Nothing prunes this directory
    /// when a session's process goes away, so a listing grows with everything that has ever run
    /// on the machine while this grows with the number of tabs open right now — and every file a
    /// listing would have found extra is one there is no session to attach it to anyway.
    ///
    /// A missing directory is not a failure to report. It is an older Claude Code, or one of the
    /// backends without the peer features these files belong to, and the answer is an empty
    /// dictionary, which makes every function below a no-op.
    static func entries(in dir: URL = directory, pids: [Int32]) -> [Int32: Entry] {
        var out: [Int32: Entry] = [:]
        for pid in pids {
            let url = dir.appendingPathComponent("\(pid).json")
            guard let data = try? Data(contentsOf: url), let entry = parse(data),
                  entry.pid == pid else { continue }
            out[pid] = entry
        }
        return out
    }

    static func parse(_ data: Data) -> Entry? {
        guard let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let raw = obj["pid"] as? Int, let pid = Int32(exactly: raw) else { return nil }
        // Absent means "no version stated", which is not the version this was written against.
        // Silence is the one answer a version gate must not read as agreement.
        let peer = obj["peerProtocol"] as? Int ?? 0
        let updated = (obj["statusUpdatedAt"] as? Double).map {
            Date(timeIntervalSince1970: $0 / 1000)
        }
        return Entry(pid: pid,
                     sessionID: text(obj["sessionId"]),
                     cwd: text(obj["cwd"]),
                     name: text(obj["name"]),
                     version: text(obj["version"]),
                     status: text(obj["status"]).map(Status.init),
                     waitingFor: text(obj["waitingFor"]),
                     procStart: text(obj["procStart"]),
                     peerProtocol: peer,
                     statusUpdatedAt: updated)
    }

    private static func text(_ value: Any?) -> String? {
        guard let s = value as? String, !s.isEmpty else { return nil }
        return s
    }

    // MARK: - Which file belongs to which session

    /// How far apart the two start times are allowed to be.
    ///
    /// They are measured two different ways and neither is exact. Claude Code writes the whole
    /// second `ps -o lstart=` printed; this app derives its own from `etime`, which is also whole
    /// seconds and is stamped after a subprocess has come back. So the honest gap between two
    /// readings of the same process is "under a couple of seconds", and this is that with room
    /// for a machine under load. Nothing about pid reuse wants it tighter: a recycled number
    /// belongs to a process that started hours or days after the one whose file is in hand.
    static let startTolerance: TimeInterval = 5

    /// `LC_ALL=C TZ=UTC ps -o lstart=`, which is the exact command Claude Code runs to produce
    /// `procStart`. Fixed locale and fixed zone on both sides, so this is not a localised date
    /// being parsed — it is one program's C-locale output read by another.
    private static let procStartFormat: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = TimeZone(identifier: "UTC")
        f.dateFormat = "EEE MMM d HH:mm:ss yyyy"
        return f
    }()

    static func procStartDate(_ text: String?) -> Date? {
        guard let text else { return nil }
        // `ps` pads a single-digit day of month with a second space — "Tue Aug  5" — and the
        // formatter wants one separator, so runs of blanks collapse before it sees them.
        let squashed = text.split(whereSeparator: { $0 == " " || $0 == "\t" || $0 == "\n" })
            .joined(separator: " ")
        guard !squashed.isEmpty else { return nil }
        return procStartFormat.date(from: squashed)
    }

    /// Whether a file is about the process this Mac has on that pid, rather than about a dead one
    /// that used to have it.
    ///
    /// Claude Code guards its own cross-session messaging exactly this way, and the reason to
    /// copy it is that the failure without it is silent and wrong in the worst direction: a
    /// leftover file whose number has been handed to somebody else's tab paints that tab with a
    /// stranger's state. Unverifiable counts as failed — no start time on either side means no
    /// opinion, not the benefit of the doubt.
    static func isSameProcess(_ entry: Entry, startedAt measured: Date?) -> Bool {
        guard let measured, let written = procStartDate(entry.procStart) else { return false }
        return abs(measured.timeIntervalSince(written)) <= startTolerance
    }

    /// The file that is really about this session, or nothing.
    ///
    /// Three gates, and a session that fails any of them is one the screen answers for alone:
    /// there is a process behind it whose start time this Mac agrees with, its file states the
    /// protocol version this build knows, and the file is about that same process.
    static func entry(for sessionID: String, in reading: Reading) -> Entry? {
        guard let process = reading.processes[sessionID],
              let entry = reading.entries[process.pid],
              entry.peerProtocol == protocolVersion,
              isSameProcess(entry, startedAt: process.started) else { return nil }
        return entry
    }

    // MARK: - What a file is allowed to change

    /// The sessions the registry says are stopped on a question, by session id.
    ///
    /// Handed to the screen parser *before* it classifies anything, which is the same door a
    /// hook note goes through and for the same reason: Claude Code's AskUserQuestion draws its
    /// selection caret flush left, in the same column as the caret in front of the box you type
    /// into, so the shape alone cannot be told apart from a numbered list somebody echoed. This
    /// says a question really is open, and the parser is then free to read the options off the
    /// screen — which is the half the registry does not carry.
    ///
    /// Strictly better than the hook version of the same fact, which fires for permission
    /// requests that auto mode approves without ever drawing a dialog. `waiting` is written only
    /// when something is actually blocked.
    static func waiting(in reading: Reading, sessions: [TargetSession]) -> Set<String> {
        guard !reading.entries.isEmpty else { return [] }
        var out: Set<String> = []
        for session in sessions where entry(for: session.id, in: reading)?.status == .waiting {
            out.insert(session.id)
        }
        return out
    }

    /// The readings, with what each session says about itself folded in.
    ///
    /// Pure, and shaped after ``HookBridge/merge(_:into:sessions:now:)`` because it is the same
    /// job: an outside fact laid over a screen reading, with the rules for who wins written where
    /// a test can reach them. No clock here — nothing in a registry file expires. A note is a
    /// thing that *happened* and goes stale; a status is a thing that *is*, rewritten by the
    /// session itself whenever it stops being true.
    static func merge(_ reading: Reading, into states: [String: SessionState],
                      sessions: [TargetSession]) -> [String: SessionState] {
        guard !reading.entries.isEmpty else { return states }
        var out = states
        for session in sessions {
            guard let entry = entry(for: session.id, in: reading) else { continue }
            let screen = states[session.id] ?? .unknown

            switch entry.status {
            case .waiting:
                // The whole point. A question no longer has to be *recognised* to be reported;
                // it only has to be recognised to be answered from the phone.
                out[session.id] = .waiting

            case .busy:
                // A menu found on screen outranks this. The registry can be a beat behind a
                // dialog that has just been drawn, and of the two ways to be wrong for that beat
                // — a waiting session shown as working, or a working session shown as waiting —
                // only the first hides the row somebody has to act on.
                if screen == .waiting { continue }
                // Keep whatever the terminal said it was doing. The live line is Claude Code's
                // own sentence, with its own clock in it, and this has nothing better to put
                // there — it only knows *that* something is running.
                if case .working = screen { continue }
                out[session.id] = .working("")

            case .idle:
                if screen == .waiting { continue }
                // This is the stale-spinner fix, without the guesswork the hook version needs:
                // Claude Code does not always erase its live line when a fast turn ends, and
                // where `HookBridge` has to decide how long a `Stop` may plausibly be talking
                // about, this is the session saying, right now, that it is not working.
                out[session.id] = .idle

            case .other, nil:
                // A word this build does not know, or a file written before the session had a
                // status to put in it. Neither is a state, and the screen is the whole answer.
                continue
            }
        }
        return out
    }

    /// Just the statuses, for asking whether anything worth looking at has changed.
    ///
    /// ``Entry`` carries clocks that move on every write, so comparing whole entries would answer
    /// "something changed" to every write there has ever been — which is the opposite of what
    /// ``watch(_:)`` is for.
    static func statuses(_ entries: [Int32: Entry]) -> [Int32: Status] {
        var out: [Int32: Status] = [:]
        for (pid, entry) in entries {
            guard let status = entry.status else { continue }
            out[pid] = status
        }
        return out
    }

    /// Claude Code's own id for the conversation in this session, when its file says so.
    ///
    /// The id names the transcript, so having it removes an entire piece of guesswork — see
    /// ``Transcript/locate(cwd:tabTitle:startedAt:sessionID:)``, which without one narrows by
    /// project, then by tab title, then settles for whichever file was written most recently.
    /// A hook note already carried this for anybody who had installed hooks; this carries it for
    /// everybody, and through the same identity check as every other field here, so a leftover
    /// file cannot attach one session's tab to another session's conversation.
    static func sessionID(of session: TargetSession) -> String? {
        guard Config.shared.sessionRegistry else { return nil }
        return entry(for: session.id, in: Targets.registry(of: [session]))?.sessionID
    }

    // MARK: - Being told rather than asking

    private static var source: DispatchSourceFileSystemObject?
    private static var watchFD: Int32 = -1

    /// Watch the directory and call back when something in it moves.
    ///
    /// The directory rather than the files: a session writes to a temporary name and moves it
    /// into place, so the file a watcher had open is never the one that gets the new bytes.
    ///
    /// Harmless where there is nothing to watch — a missing directory simply produces no
    /// callbacks, and this is deliberately not the place that creates one. This app has no
    /// business making a directory inside another program's dot-folder.
    static func watch(_ onChange: @escaping () -> Void) {
        source?.cancel()
        if watchFD >= 0 { close(watchFD) }
        watchFD = open(directory.path, O_EVTONLY)
        guard watchFD >= 0 else { return }
        let s = DispatchSource.makeFileSystemObjectSource(
            fileDescriptor: watchFD, eventMask: [.write, .rename, .delete],
            queue: DispatchQueue.global(qos: .utility))
        // Every session on the machine writes into this one directory, so a busy afternoon lands
        // several files inside the same instant. A tick of delay collapses that into one answer.
        s.setEventHandler {
            DispatchQueue.main.async {
                guard !pending else { return }
                pending = true
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.04) {
                    pending = false
                    onChange()
                }
            }
        }
        s.setCancelHandler { }
        s.resume()
        source = s
    }

    private static var pending = false
}
