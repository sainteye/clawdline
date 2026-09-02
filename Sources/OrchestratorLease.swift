import Foundation

/// The machine-level exclusive lease over the one expensive thing on this Mac: a full Swift
/// compile.
///
/// **The lock directory is the truth. This is the registry and the queue in front of it.**
/// Holding `/tmp/clawdline-suite.lock` is what `mkdir` says it is — atomic, kernel-backed, and
/// still correct for a contributor who runs `./test.sh` with no Clawdline installed at all. What
/// a directory cannot hold is who the holder is in Clawdline terms, who is waiting and in what
/// order, whether the holder is still proving it is alive, and what the reconciliation found
/// after a restart. That is what the durable record adds, and it is the only thing it adds. A
/// second independent lock would be a race wearing the costume of a safety net.
///
/// Everything in this file is a pure function over values. Nothing here reads shared state,
/// takes a lock, touches the filesystem or spawns a process: a decision is a ``Decision``, and
/// the filesystem work it implies is a ``SideEffect`` the caller performs. ``Probes`` is where
/// the observations enter, so a test can drive every branch — a sentinel that outlives its work,
/// an orphaned compiler, a `ps` that timed out — without spawning anything.
///
/// # The two questions, kept apart
///
/// A lease answers two questions and the record keeps them separate, because conflating them is
/// how a threshold becomes a deadlock:
///
///   1. **Exclusion** — whose turn is it. Fail closed: no lease, no compile.
///   2. **Admission** — may the holder start *now*, and **at what size**. A grant carries a
///      parallelism ceiling with a floor of one, and refusal is the last resort rather than the
///      first answer.
///
/// **The ceiling is a ceiling, not a throttle.** Its job is to stop somebody passing `-j 8` and
/// multiplying the peak by eight; it is not a mechanism for making a compile that will not fit
/// fit. On this Mac `swiftc` with no `-j` was measured to run one frontend already, so granting
/// `-j 1` grants nothing — and a second reading on the same machine saw two concurrent frontends
/// during a full compile, reconcilable with the first only if one of them was the stray driver
/// that `node Tests/keychain-rebuild-focused.mjs` starts outside `test.sh`'s own `swiftc` line.
/// Neither reading is this file's to settle. What the ceiling is relied on for is the direction
/// that is certain: it can only lower the number of concurrent frontends, never raise it.
///
/// # Liveness is proved by heartbeat, not by a pid existing
///
/// This is the correction that this design is actually built around, and it came from a live
/// defect: a holder recorded `pid=72929`, and that pid was a `sleep 14400` sentinel adopted by
/// launchd. Binding liveness to a proxy process fails in **both** directions from one cause —
/// the sentinel outlives the work, so the lock becomes a four-hour roadblock nobody can clear;
/// and the obvious patch ("no `swift-frontend` means stale") makes the lock reclaimable in the
/// gaps *between* the compiles of one study, which is the collision back again.
///
/// So the holder proves it is there by **refreshing its record while it works**. A `sleep`
/// cannot renew.
///
/// **A clock on the work is wrong; a clock on the proof of life is right.** A four-hour compile
/// is not stale — a duration timeout on the work is exactly what was withdrawn — but a holder
/// renewing every twenty seconds never trips a sixty-second renewal deadline however long its
/// work runs.
///
/// # Three rules that bind the whole file
///
///   * **Never kill anything.** There is no route, flag or code path here that terminates or
///     suspends a process. The system may queue, may refuse and may tell. Naming the largest
///     holders of anonymous memory in a refusal is information for a person; it is never a
///     target list.
///   * **Fail closed.** Missing, stale or ambiguous evidence reads `unknown` and blocks. It
///     never reads "dead".
///   * **Takeover needs both halves, and the physical backstop is never waived.** A new holder
///     is admitted only when (A) the current holder has stopped proving it is alive **and** (B)
///     no `swift-frontend` exists anywhere on the machine. (B) alone must never admit anybody.
///     (A) alone must never admit anybody either: a holder that is wedged or swapped out and
///     missed a heartbeat while its compile is still burning 46 GB keeps the lock.
enum OrchestratorLease {

    // MARK: - The closed vocabulary

    /// The one exclusive resource. `build.sh` and `test.sh` both take it because it is the same
    /// machine capacity. One resource cannot deadlock against itself; a second one would come
    /// with an acquisition order somebody has to prove, which is why there is not one yet.
    static let heavyCompile = "heavy_compile"

    static let resources: Set<String> = [heavyCompile]

    /// Where the exclusive directory lives. Configurable so a test never touches the real one.
    static let defaultDirectory = "/tmp/clawdline-suite.lock"

    /// The heartbeat file, **inside the lock directory**. That placement is the point: `rmdir`
    /// takes the beat with it, so a released lock can never leave behind an orphan heartbeat
    /// pointing at work that has ended. Its modification time is the beat.
    static let beatFilename = "beat"

    static func beatPath(inside directory: String) -> String {
        (directory as NSString).appendingPathComponent(beatFilename)
    }

    /// How long a holder may go without refreshing its record before it has stopped proving it
    /// is alive.
    ///
    /// This is a clock on the *proof*, not on the work. A holder renewing every twenty seconds
    /// never approaches it; a holder that died leaves the lease reclaimable a minute later —
    /// and even then only if (B) also holds.
    static let renewalDeadline: TimeInterval = 60

    /// How far a recorded process start may sit from the observed one and still be the same
    /// process, for the *reported* identity axis.
    ///
    /// It is not zero because `ps -o lstart=` has whole-second resolution and a shell holder
    /// writes `started=` from `date +%s` on its first line. It is not a weakening of the
    /// pid-reuse rule: for a recycled pid to pass this, the original must have died within two
    /// seconds of starting *and* the machine must have issued its whole pid space in that
    /// window. What the tolerance admits is a clock rounding, which is what it is for.
    static let startMatchTolerance: TimeInterval = 2

    /// A bounded queue, so a wedged machine refuses instead of growing a list nobody reads.
    ///
    /// The limit counts *proving* waiters. An entry whose owner has stopped asking is passed over
    /// rather than reclaimed from — it keeps its place, in case it comes back — but it must not
    /// spend the machine's queue budget on somebody who is no longer waiting, which is how a
    /// blocked head of queue turned into a 429 for everybody.
    static let queueDepthLimit = 32

    /// How long a waiter may go without asking again before it has stopped proving it is waiting.
    ///
    /// `build.sh` polls every five seconds, so two minutes is twenty-four missed polls: long
    /// enough that a wedged machine or a slow answer never costs anybody their place, short enough
    /// that a Ctrl-C does not block the line for the rest of the day. The cost of being wrong is
    /// asymmetric and that is why the number is generous: passing over a live waiter delays it by
    /// one grant, while trusting a dead one blocks the machine until somebody notices.
    static let waiterDeadline: TimeInterval = 120

    /// Above this the list itself is trimmed of its longest-silent entries, so a machine nobody is
    /// watching cannot grow an unbounded array of requests nobody will ever make again.
    static let queueHardLimit = 64

    static let labelLimit = 200
    static let reasonLimit = 500
    static let pathLimit = 4_096
    /// The most work pids a record will carry. A record is a description, not an inventory.
    static let workPIDLimit = 32

    // MARK: - What the holder is doing

    /// What the holder is doing **right now**, refreshed at each renewal.
    ///
    /// It exists because of a picture this design could not otherwise distinguish. At 02:45 on
    /// 2026-09-03 the lock had been held for 36 minutes, `holder.txt` was last written at 02:20,
    /// there were zero `swift-frontend` processes on the machine, no `done_flag` had appeared,
    /// and the holder's sentinel pid was alive. **From outside, "it is between compiles" and "it
    /// finished and forgot to let go" are the same picture** — and another line had been ready
    /// and waiting five minutes with no safe way to tell which one it was looking at.
    ///
    /// So renewal and phase are two halves of one answer. **Renewal proves "I am still here";
    /// phase says "and this is what I am still doing."** Only together do they answer whether
    /// the lock is currently protecting anything. Without it an honest holder writing its report
    /// looks exactly like a dead one, and the only party that knows the difference is the holder.
    ///
    /// **`phase` is never a takeover input.** A stalled phase is something the query can *say*;
    /// the two admission conditions are unchanged and remain the only ones. That boundary is the
    /// point of the field: it can speak, it cannot decide.
    enum Phase: String, Equatable, CaseIterable {
        /// Running the compiler. This is what the lock exists for.
        case compiling
        /// Producing a snapshot, writing a command line, reading results — work between compiles
        /// that is going to compile again.
        case analysing
        /// Still needs the slot and nothing is running right now. The name says exactly that,
        /// which is the whole use of the field: it is the picture that otherwise looks identical
        /// to a holder that finished and forgot to let go.
        case idleHolding = "idle-holding"
        /// No phase was recorded. Older writers, and shell holders that predate the field.
        case unknown

        /// Earlier spellings, accepted on the way in.
        ///
        /// `preparing`, `writing` and `idle` were published in the first draft of this field
        /// before the vocabulary settled at three values, and a writer may already be using
        /// them. Reading them is free; refusing them would turn a holder that is talking to us
        /// into one that is not, which is `unknown`, which blocks.
        static func decode(_ raw: String) -> Phase? {
            if let exact = Phase(rawValue: raw) { return exact }
            switch raw {
            case "preparing": return .analysing
            case "writing": return .idleHolding
            case "idle": return .idleHolding
            default: return nil
            }
        }
    }

    /// **The heartbeat must be emitted by something that stops when the work stops.**
    ///
    /// This is the one way `renewed=` becomes the defect it was introduced to fix, wearing a
    /// different coat. A detached timer —
    ///
    /// ```bash
    /// while true; do touch beat; sleep 60; done &     # this is a sentinel
    /// ```
    ///
    /// — keeps beating after the work it claims to represent has died, which is exactly what
    /// `sleep 14400` did. What is required is a **supervisor loop**: the same loop that waits on
    /// the compiler's pid is the loop that writes the beat, so the heartbeat stops when the
    /// compiler exits or when the supervisor is itself killed. The proposition being proved is
    /// "somebody is still supervising this work", not "a timer is still running on this machine".
    ///
    /// Nothing in the broker can verify that from the outside — which is why it is written down
    /// here, in `docs/orchestrator.md`, and in the child briefing that hands out the `curl`.
    ///
    /// The deadline is 60 seconds against a measured 30-second cadence whose worst observed
    /// drift over 56 samples in an hour was 1 second — including a sample taken with 0.06 GB
    /// free, 392 MB of swap free and a compiler running. That sample was taken along a
    /// small-footprint shell path rather than along a 20 GB compile's supervisor, so it does not
    /// prove the supervisor never stalls; it is the reason the deadline is 60 and not 90, and
    /// the reason (B) is never waived when it passes.
    static let heartbeatSupervisionRequired = true

    /// How long a holder may sit outside `compiling` before a reader is told about it. A number
    /// on a sentence, not on a decision: nothing is reclaimed when it passes.
    static let phaseAttentionAfter: TimeInterval = 300

    // MARK: - Identity

    /// Who is asking, in Clawdline terms. Every field is optional except the label, because a
    /// contributor's plain shell is a legitimate holder and has none of the others.
    struct Owner: Equatable {
        var sessionID: String?
        var taskID: String?
        var rootSessionID: String?
        /// The `holder=` text: what a person reading the directory by hand sees.
        var label: String

        init(sessionID: String? = nil, taskID: String? = nil, rootSessionID: String? = nil,
             label: String) {
            self.sessionID = sessionID
            self.taskID = taskID
            self.rootSessionID = rootSessionID
            self.label = label
        }
    }

    /// `holder.txt`, in the six-field shape two sessions evolved on the night this was written,
    /// plus three additive fields the same night's defect asked for.
    ///
    /// It is the interoperability surface: `test.sh` writes it with `printf`, `build.sh` writes
    /// it with `printf`, and this reads it. A parser that does not know `work=`, `done=` or
    /// `renewed=` ignores them; changing the meaning of one of the original six is a breaking
    /// change to a file three programs share.
    ///
    /// **`pid` is the process actually doing the work, never a sentinel.** When the work is a
    /// sequence of processes — a study that runs several compiles — `work` carries the current
    /// ones and is refreshed at each renewal. A single pid field cannot describe a sequence, and
    /// that gap is precisely how a `sleep 14400` came to be a holder.
    /// `holder.txt`, and **the contract is the whole of it**.
    ///
    /// Three programs write this file — `test.sh`, `build.sh` and ``encode(_:)`` below — and all
    /// three read each other's. They used to write three different subsets: seventeen fields,
    /// eleven and eleven, eight in common. The four the shell's compare-and-swap depends on —
    /// `token`, `owner_pid`, `owner_started`, `heartbeat_deadline` — were written by nobody but
    /// `test.sh`, so against a record this file or `build.sh` wrote, its compare was `"" = ""` and
    /// always true; the re-read beside it was carrying the whole swap alone. In the other
    /// direction `test.sh` wrote `working=` while this file has always read `work=`, so each side
    /// showed an empty working list for the other's holder, and the field the design specifies as
    /// "the record names the process actually working" crossed in neither direction.
    ///
    /// The full list, in the order it is written, is in `test.sh` above
    /// `clawdline_suite_lock_write_record`, which is where a person editing any of the three will
    /// look. Two of its clauses matter here:
    ///
    ///   * **`pid` is the process working right now; `owner_pid` is the run.** A hold is a
    ///     sequence — the compiler driver, then the test binary — so `pid` changes during it and
    ///     is no use as an identity. Every comparison that asks "is this the same holder" uses
    ///     ``owner``, which prefers `owner_pid` and falls back to `pid` for a record written
    ///     before the contract existed.
    ///   * **`owner_started` is the one field a writer may leave empty.** It is a normalised
    ///     `LC_ALL=C ps -o lstart=` line, which a shell can read and a broker holding only epoch
    ///     seconds cannot; empty means "this writer did not record it" and is unknown to every
    ///     reader, never a mismatch. `started` carries the same fact in the form this file
    ///     compares.
    struct HolderFile: Equatable {
        var holder: String
        var pid: Int32
        /// The run itself, which is what ownership is proved against. Absent in a record written
        /// before the contract, where `pid` was the only number there was.
        var ownerPID: Int32?
        /// `owner_pid`'s start identity, as one normalised `LC_ALL=C ps -o lstart=` line. May be
        /// empty; see the note above.
        var ownerStarted: String?
        /// This hold's unique identity, and the compare in every compare-and-swap. A pid is
        /// reused within hours on a busy machine; a token is not.
        var token: String?
        /// Seconds without a beat after which this holder has stopped proving it is alive. A
        /// reader prefers the holder's own number to its own.
        var heartbeatDeadline: Int?
        /// When the current `phase` began. Moved only when the phase itself changes, so a reader
        /// gets "36 minutes in `analysing`" rather than "renewed 4 seconds ago".
        var phaseSince: Date?
        /// When it was last true that something was compiling under this lock. `nil` is `never`.
        var lastCompiling: Date?
        /// Three states: `nil` means this writer does not probe for compilers, `"none"` means it
        /// probed and the machine was clear, anything else is the pids it found. The first two are
        /// different claims and collapsing them is how a backstop stops being one.
        var compilers: String?
        /// When `pid` started, whole seconds. Written as epoch seconds; an ISO-8601 UTC stamp is
        /// also accepted on the way in, because a shell that has `date -u` and no `%s` habit
        /// writes that instead and losing its identity would be the whole bug again.
        var started: Date?
        var tree: String?
        var log: String?
        var note: String?
        /// The pids doing the work right now, refreshed at each renewal.
        var work: [Int32]
        /// Written as `done_flag=`, which is the key the lock on this machine uses; `done=`
        /// is accepted as an alias. A path the run creates when its work has finished.
        /// **Positive signal only**: present
        /// means the work is over; absent proves nothing, because a SIGKILLed run never writes
        /// one. Reading absence as "still running" would rebuild the permanent roadblock
        /// somewhere new.
        var done: String?
        /// Whole seconds, last refreshed by the holder. This is the proof of life a shell holder
        /// leaves for a broker that restarted and has no record of the grant.
        var renewed: Date?
        /// What the holder is doing right now. See ``Phase``.
        var phase: Phase
        /// The heartbeat file this holder touches, which is normally `beat` inside the lock
        /// directory. Named in the file so a reader never has to assume the convention.
        var heartbeat: String?

        /// Who this record belongs to, for every comparison that asks whether the holder changed.
        /// `pid` alone cannot answer it: it names whatever is working at this beat and moves
        /// between the compiler and the test binary within one hold.
        var owner: Int32 { ownerPID ?? pid }

        init(holder: String, pid: Int32, started: Date?, tree: String? = nil,
             log: String? = nil, note: String? = nil, work: [Int32] = [],
             done: String? = nil, renewed: Date? = nil, phase: Phase = .unknown,
             heartbeat: String? = nil, ownerPID: Int32? = nil, ownerStarted: String? = nil,
             token: String? = nil, heartbeatDeadline: Int? = nil, phaseSince: Date? = nil,
             lastCompiling: Date? = nil, compilers: String? = nil) {
            self.holder = holder
            self.pid = pid
            self.started = started
            self.tree = tree
            self.log = log
            self.note = note
            self.work = work
            self.done = done
            self.renewed = renewed
            self.phase = phase
            self.heartbeat = heartbeat
            self.ownerPID = ownerPID
            self.ownerStarted = ownerStarted
            self.token = token
            self.heartbeatDeadline = heartbeatDeadline
            self.phaseSince = phaseSince
            self.lastCompiling = lastCompiling
            self.compilers = compilers
        }
    }

    /// Serialise `holder.txt`. Values are single-line by construction: a newline inside one
    /// would silently invent a field for the next parser along.
    static func encode(_ file: HolderFile) -> String {
        func line(_ key: String, _ value: String?) -> String {
            let flat = (value ?? "")
                .replacingOccurrences(of: "\n", with: " ")
                .replacingOccurrences(of: "\r", with: " ")
            return "\(key)=\(flat)\n"
        }
        func epoch(_ date: Date?) -> String {
            date.map { "\(Int($0.timeIntervalSince1970))" } ?? ""
        }
        // The contract's order, which is part of the contract: a person diffing two records by
        // eye should not have to sort them first.
        var out = ""
        out += line("holder", file.holder)
        out += line("pid", "\(file.pid)")
        out += line("owner_pid", "\(file.owner)")
        out += line("owner_started", file.ownerStarted)
        out += line("token", file.token)
        out += line("phase", file.phase == .unknown ? "" : file.phase.rawValue)
        out += line("phase_since", epoch(file.phaseSince))
        out += line("heartbeat", file.heartbeat)
        out += line("heartbeat_deadline",
                    "\(file.heartbeatDeadline ?? Int(renewalDeadline))")
        out += line("started", epoch(file.started))
        out += line("renewed", epoch(file.renewed))
        out += line("tree", file.tree)
        out += line("log", file.log)
        out += line("done_flag", file.done)
        out += line("work", file.work.map { "\($0)" }.joined(separator: ","))
        out += line("last_compiling", file.lastCompiling.map { "\(Int($0.timeIntervalSince1970))" }
                        ?? "never")
        out += line("compilers", file.compilers)
        out += line("note", file.note)
        return out
    }

    /// Read `holder.txt`. A file with no usable `pid` is not a holder record — it is an
    /// unreadable directory, which is a different and more careful answer than "nobody".
    static func parseHolderFile(_ text: String) -> HolderFile? {
        var fields: [String: String] = [:]
        for rawLine in text.split(separator: "\n", omittingEmptySubsequences: true) {
            let line = rawLine.trimmingCharacters(in: .whitespaces)
            guard let separator = line.firstIndex(of: "=") else { continue }
            let key = String(line[line.startIndex..<separator])
                .trimmingCharacters(in: .whitespaces)
            let value = String(line[line.index(after: separator)...])
                .trimmingCharacters(in: .whitespaces)
            guard !key.isEmpty, fields[key] == nil else { continue }
            fields[key] = value
        }
        guard let pidText = fields["pid"], let pid = Int32(pidText), pid > 0 else { return nil }
        func present(_ key: String) -> String? {
            guard let value = fields[key], !value.isEmpty, value.count <= pathLimit else {
                return nil
            }
            return value
        }
        let work = (fields["work"] ?? "").split(separator: ",")
            .compactMap { Int32($0.trimmingCharacters(in: .whitespaces)) }
            .filter { $0 > 0 }
        // `working=` as well as `work=`: the shell holder wrote the other spelling, space
        // separated, for as long as this parser has existed, and a reader that understands only
        // its own spelling is half of what F10 was.
        let working = (fields["work"] ?? fields["working"] ?? "")
            .split(whereSeparator: { $0 == "," || $0 == " " })
            .compactMap { Int32($0.trimmingCharacters(in: .whitespaces)) }
            .filter { $0 > 0 }
        return HolderFile(holder: present("holder") ?? "unnamed", pid: pid,
                          started: fields["started"].flatMap(parseStamp),
                          tree: present("tree"), log: present("log"), note: present("note"),
                          work: Array((work.isEmpty ? working : work).prefix(workPIDLimit)),
                          done: present("done_flag") ?? present("done"),
                          renewed: fields["renewed"].flatMap(parseStamp),
                          phase: present("phase").flatMap(Phase.decode) ?? .unknown,
                          heartbeat: present("heartbeat"),
                          ownerPID: present("owner_pid").flatMap(Int32.init).flatMap { $0 > 0 ? $0 : nil },
                          ownerStarted: present("owner_started"),
                          token: present("token"),
                          heartbeatDeadline: present("heartbeat_deadline").flatMap(Int.init)
                              .flatMap { $0 > 0 ? $0 : nil },
                          phaseSince: fields["phase_since"].flatMap(parseStamp),
                          lastCompiling: fields["last_compiling"].flatMap(parseStamp),
                          compilers: present("compilers"))
    }

    /// A time in any of the three shapes a `holder.txt` on this machine has actually carried.
    ///
    /// The list is not hospitality, it is a bug report. The lock live on this Mac at 02:20 on
    /// 2026-09-03 wrote `started=2026-09-03 02:08:54` — local time, space separated — and a
    /// parser that accepted only epoch seconds would have read that holder as having no start at
    /// all, which is `unknown`, which blocks forever. Three programs write this file and only
    /// one of them is this one, so the reader takes what the writers write:
    ///
    ///   * epoch seconds, which is what this build writes;
    ///   * `2026-09-03T02:08:54Z`, for a shell with `date -u` and no `%s` habit;
    ///   * `2026-09-03 02:08:54` in **local** time, which is what `date '+%F %T'` produces and
    ///     what the measurement task's lock actually held.
    ///
    /// Anything else is absent, which reads as unknown rather than as a fresh process.
    static func parseStamp(_ raw: String) -> Date? {
        let text = raw.trimmingCharacters(in: .whitespaces)
        guard !text.isEmpty else { return nil }
        if let seconds = Double(text), seconds > 0 { return Date(timeIntervalSince1970: seconds) }
        func read(_ format: String, utc: Bool) -> Date? {
            let formatter = DateFormatter()
            formatter.locale = Locale(identifier: "en_US_POSIX")
            formatter.calendar = Calendar(identifier: .gregorian)
            formatter.timeZone = utc ? TimeZone(identifier: "UTC") : .current
            formatter.dateFormat = format
            return formatter.date(from: text)
        }
        return read("yyyy-MM-dd'T'HH:mm:ss'Z'", utc: true)
            ?? read("yyyy-MM-dd HH:mm:ss", utc: false)
    }

    // MARK: - What the machine says

    /// What the lock directory looks like right now.
    ///
    /// `unreadable` is its own case on purpose. A directory that exists with no legible
    /// `holder.txt` is not free and is not a known holder: it is the state where somebody is
    /// half way through taking it, or wrote it wrong. Both answers below — grant it, take it
    /// over — would be a race, so neither is available.
    enum DirectoryState: Equatable {
        case absent
        case held(HolderFile)
        case unreadable(String)
    }

    /// One `LC_ALL=C ps -o lstart= -p <pid>` reading, for the *reported* identity axis.
    enum ProcessObservation: Equatable {
        case running(Date)
        case absent
        case unknown(String)
    }

    /// Whether the recorded pid is still the process it says it is. **This is reporting, not
    /// admission.** A holder between compiles has a dead work pid and is perfectly alive; a
    /// sentinel has a live pid and proves nothing. Neither value moves a takeover on its own.
    enum ProcessIdentity: String, Equatable {
        case alive, gone, unknown
    }

    /// Compare a recorded start against a freshly read one.
    ///
    /// **Both strings must come out of the same formatter, and that formatter is `LC_ALL=C`.**
    /// Measured on this Mac: `ps -o lstart= -p 72929` prints `Thu Sep  3 02:09:02 2026` under
    /// `LC_ALL=C` and `四  9/ 3 02:09:02 2026` under `zh_TW.UTF-8` — same instant, different
    /// bytes. A writer in one locale and a reader in the other compare unequal, the live holder
    /// reads as gone, and the lock goes to a second compiler.
    ///
    /// It is also why nothing here counts fields. Holding the formatter still and varying only
    /// the day, `zh_TW.UTF-8` gives five whitespace tokens on days 1–9 (`四  9/ 3 …`) and four
    /// on days 10–31 (`一  8/31 …`), while `LC_ALL=C` gives five on every day. Two earlier
    /// readings of "how many fields does zh_TW print" disagreed for that reason and both were
    /// right. Counting fields is not a rule; pinning the locale is.
    static func identity(recordedStart: Date?, observed: ProcessObservation) -> ProcessIdentity {
        switch observed {
        case .unknown:
            return .unknown
        case .absent:
            return .gone
        case .running(let actual):
            guard let recordedStart else { return .unknown }
            // The pid is in use by a process that did not start when the holder did. That is a
            // recycled pid, and a recycled pid reads as gone.
            return abs(actual.timeIntervalSince(recordedStart)) <= startMatchTolerance
                ? .alive : .gone
        }
    }

    /// Whether any `swift-frontend` is running on this machine, and which. This is backstop (B),
    /// and it is never waived.
    ///
    /// **It counts globally, including compilers nobody in this queue started.** That looks like
    /// a false positive and is in fact the definition: the question is "is anything on this
    /// machine burning right now", not "is the holder's own compiler running". `test.sh` itself
    /// produces a driver outside its own `swiftc` line — `node Tests/keychain-rebuild-focused.mjs`
    /// starts one — so a scan narrowed to the holder's process tree would miss exactly the
    /// compiler a takeover would land on top of.
    enum CompilerObservation: Equatable {
        case none
        case present([Int32])
        case unknown(String)
    }

    /// Parse `LC_ALL=C ps -axo pid=,comm=` for `swift-frontend`.
    static func parseCompilerScan(_ output: String, status: Int32?, timedOut: Bool)
        -> CompilerObservation {
        if timedOut { return .unknown("the process scan did not answer in time") }
        // `ps -ax` returns 0 with rows and 0 with none; any other status is a failed read and
        // must not be reported as an empty machine.
        guard let status, status == 0 else {
            return .unknown("the process scan exited "
                            + (status.map(String.init) ?? "without a status"))
        }
        var found: [Int32] = []
        for rawLine in output.split(separator: "\n") {
            let line = rawLine.trimmingCharacters(in: .whitespaces)
            guard !line.isEmpty else { continue }
            let parts = line.split(separator: " ", maxSplits: 1, omittingEmptySubsequences: true)
            guard parts.count == 2, let pid = Int32(parts[0]) else { continue }
            let command = String(parts[1]).trimmingCharacters(in: .whitespaces)
            let name = command.split(separator: "/").last.map(String.init) ?? command
            if name == "swift-frontend" { found.append(pid) }
        }
        return found.isEmpty ? .none : .present(found.sorted())
    }

    /// Parse `LC_ALL=C ps -o lstart= -p <pid>`.
    ///
    /// Note what this does **not** do: it does not count fields to decide whether the read
    /// succeeded. It hands the whole line to one `en_US_POSIX` formatter, which either reads it
    /// or does not. See ``identity(recordedStart:observed:)`` for the measurement behind that.
    static func parseProcessStart(_ output: String, status: Int32?, timedOut: Bool)
        -> ProcessObservation {
        if timedOut { return .unknown("the process probe did not answer in time") }
        let text = output.trimmingCharacters(in: .whitespacesAndNewlines)
        if text.isEmpty {
            // `ps -p` exits 1 with no output when the pid does not exist. That is the one shape
            // that means gone; every other status with no output is a read that failed.
            if let status, status == 1 { return .absent }
            return .unknown("the process probe produced nothing"
                            + (status.map { " and exited \($0)" } ?? ""))
        }
        guard let status, status == 0 else {
            return .unknown("the process probe exited "
                            + (status.map(String.init) ?? "without a status"))
        }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.timeZone = .current
        formatter.dateFormat = "EEE MMM d HH:mm:ss yyyy"
        guard let date = formatter.date(
            from: text.split(whereSeparator: { $0 == " " || $0 == "\n" })
                .map(String.init).joined(separator: " ")) else {
            return .unknown("the process probe printed a start this build cannot read; "
                            + "is LC_ALL=C pinned on the probe?")
        }
        return .running(date)
    }

    /// What the broker knows about the owner that a file lock cannot: whether the owning task
    /// has reached a terminal state, or the owning session is positively gone.
    ///
    /// **This is the third liveness axis and it is the broker's whole reason to exist on top of
    /// the directory.** A lease whose task failed, timed out or was cancelled has stopped
    /// proving liveness no matter what any sentinel pid is doing, and this answers that
    /// immediately rather than waiting out a renewal deadline. It is still subject to backstop
    /// (B): task gone plus a live `swift-frontend` is a refusal that names the orphan, not a
    /// takeover.
    enum OwnerState: Equatable {
        /// The task is live, or the session is present.
        case live
        /// The task reached `failure`, `timeout` or `cancelled`, or the session is positively
        /// gone. Carries the word a reader can act on.
        case gone(String)
        /// Nothing is known about this owner — a shell holder, or a session inventory that was
        /// not complete. This never accelerates anything and never blocks anything.
        case unknown
    }

    /// Every observation a decision needs, gathered once so a decision is made against one
    /// reading of the machine rather than several taken a second apart.
    struct Evidence: Equatable {
        /// The front of the queue, read once per decision. `.unknown` when there is no queue, or
        /// when the reading could not be taken — which says nothing either way.
        ///
        /// **There used to be a `holderProcess` beside this and nothing read it.** It cost one
        /// bounded subprocess on every single decision, on the machine whose subprocess and memory
        /// budget is the whole point of this feature, and `perform`'s own apology for the cost of
        /// a reading counted it. The holder does not have a process axis by design: a pid is a
        /// proxy and proxies outlive the work — a `sleep 14400` recorded as a holder is what
        /// started this — so liveness there is renewal and only renewal. A field that looks like
        /// an axis and is not is worse than no field, so it is gone rather than left to be wired
        /// up by somebody who assumes it was meant to decide something.
        var headWaiterProcess: ProcessObservation = .unknown("not probed")
        var compilers: CompilerObservation
        var owner: OwnerState
        var doneFlag: DoneFlag
        /// The modification time of the holder's beat file, when there is one. It is the shell
        /// holder's proof of life: a script with no broker record still says "I am here" by
        /// touching a file, and that is what a restarted broker reads.
        var beat: Date?
        var pressure: Pressure?

        init(headWaiterProcess: ProcessObservation = .unknown("not probed"),
             compilers: CompilerObservation = .unknown("not observed"),
             owner: OwnerState = .unknown, doneFlag: DoneFlag = .unknown,
             beat: Date? = nil, pressure: Pressure? = nil) {
            self.headWaiterProcess = headWaiterProcess
            self.compilers = compilers
            self.owner = owner
            self.doneFlag = doneFlag
            self.beat = beat
            self.pressure = pressure
        }
    }

    /// The `done=` path. Present is the only value that says anything.
    enum DoneFlag: Equatable {
        case present
        case absent
        case unknown
    }

    // MARK: - Liveness, and the takeover it gates

    /// Whether the holder is still proving it is there.
    enum Liveness: Equatable {
        /// Renewed inside the deadline, and nothing says the owner is gone.
        case proving
        /// Stopped proving, with the reason a reader can act on.
        case stopped(String)

        var code: String {
            switch self {
            case .proving: return "proving"
            case .stopped(let why): return why
            }
        }
    }

    /// The whole of (A).
    ///
    /// Three ways to stop proving, and they are deliberately ordered fastest-first:
    ///
    ///   1. `done=` present — the work said it finished. A positive signal, so it needs no
    ///      deadline. Absence proves nothing and falls through.
    ///   2. the owning task is terminal or its session is positively gone — the broker's own
    ///      axis, which answers a four-hour sentinel immediately.
    ///   3. the renewal deadline passed — the general case, and the only one a shell holder
    ///      with no Clawdline identity can trip.
    static func liveness(renewedAt: Date, owner: OwnerState, doneFlag: DoneFlag,
                         beat: Date? = nil, now: Date,
                         deadline: TimeInterval = renewalDeadline) -> Liveness {
        if doneFlag == .present { return .stopped("work_finished") }
        if case .gone = owner { return .stopped("owner_gone") }
        // Two writers, one clock. A broker-granted holder renews over HTTP; a shell holder
        // touches `beat` inside the lock directory and has no record at all. The later of the
        // two is the proof, so a lease is never read as lapsed because the *other* channel is
        // the one being used.
        let latest = max(renewedAt, beat ?? .distantPast)
        if now.timeIntervalSince(latest) > deadline { return .stopped("heartbeat_lapsed") }
        return .proving
    }

    /// Why a lease may or may not be taken from the holder recorded on it.
    ///
    /// Not a timer on the work. A lease is never freed because it has been *held* a long time —
    /// a long compile is exactly what this exists to protect.
    enum Takeover: Equatable {
        case eligible
        /// (A) does not hold: the holder is still proving it is alive.
        case holderProving
        /// (B) does not hold: no matter what (A) says, a compiler is running.
        case compilersRunning([Int32])
        /// Evidence missing, stale or ambiguous. Blocks; never reads as dead.
        case evidenceUnknown(String)

        /// The typed code a caller branches on. These are the `hold_reason` values on a queued
        /// answer and on the query, and they are the whole of "why am I still waiting".
        var code: String {
            switch self {
            case .eligible: return "eligible"
            case .holderProving: return "holder_proving"
            case .compilersRunning: return "compiler_running"
            case .evidenceUnknown: return "evidence_unknown"
            }
        }
    }

    /// Both halves, or nothing.
    ///
    /// The two failures this shape exists to prevent, from the same live defect:
    ///
    ///   * *(A) waived* — "no `swift-frontend` means stale" would hand the lock to a second
    ///     acquirer in the gap between two compiles of one study. Here a proving holder keeps
    ///     it, compilers or not.
    ///   * *(B) waived* — a holder whose renewal lapsed while its compile still burns 46 GB
    ///     would be reclaimed on top of a live compiler. Here a running `swift-frontend`
    ///     refuses, names the orphan pids, and lets a person act.
    static func takeover(liveness: Liveness, compilers: CompilerObservation) -> Takeover {
        // (B) is evaluated first and unconditionally, so no future edit can arrange for (A) to
        // short-circuit past the physical backstop.
        switch compilers {
        case .unknown(let why):
            return .evidenceUnknown(why)
        case .present(let pids):
            return .compilersRunning(pids)
        case .none:
            break
        }
        if case .proving = liveness { return .holderProving }
        return .eligible
    }

    // MARK: - Admission: a budget, not a yes or no

    /// One reading of what the machine is carrying. Megabytes throughout.
    ///
    /// **Two readings that look like evidence are deliberately absent.** Summed RSS across
    /// processes double-counts every shared page — 622 processes summed to 22.33 GB on this
    /// 24 GB Mac at a moment when `memory_pressure` reported 81% free — and physical free
    /// percentage looks excellent right after Jetsam kills something while swap has not
    /// recovered. What carries the load is swap used plus compressor, read together with
    /// anonymous pages and free.
    struct Pressure: Equatable {
        var physicalMB: Int
        var freeMB: Int
        var anonymousMB: Int
        var fileBackedMB: Int
        var compressorMB: Int
        var swapUsedMB: Int
        var swapFreeMB: Int
        var swapTotalMB: Int
        var observedAt: Date

        /// What can be had without pushing more pages out: free physical plus the file-backed
        /// pages the kernel can drop.
        ///
        /// Swap free is deliberately **not** in here. The swap file is elastic — measured over
        /// forty minutes on this machine with nothing compiling, `vm.swapusage` total went
        /// 9,216 → 10,240 → 12,288 MB and free swung from 353 MB to 1,417 MB — so a floor on it
        /// is a condition whose denominator moves under the test. A gate that can never be
        /// satisfied is a deadlock wearing a threshold's clothes.
        var headroomMB: Int { max(0, freeMB + fileBackedMB) }
    }

    /// The admission policy. Every constant in it is a hypothesis until the measurement task
    /// lands a number, and the default is written so that an unmeasured hypothesis can never
    /// refuse anybody.
    struct Policy: Equatable {
        /// Peak footprint of one `swift-frontend` at this parallelism, in megabytes. **Nil
        /// until measured**, and while it is nil every grant is the floor of one.
        var perCompileMB: Int?
        /// The headroom one compile needs before the floor itself is refused. **Nil until
        /// measured**, and while it is nil the floor is always admitted — which is the whole
        /// deadlock-freedom argument: this build cannot refuse on a number nobody has taken.
        var floorRequirementMB: Int?
        /// Never hand out more than this, whatever the arithmetic says.
        var maximumParallelism: Int

        init(perCompileMB: Int? = nil, floorRequirementMB: Int? = nil,
             maximumParallelism: Int = 8) {
            self.perCompileMB = perCompileMB
            self.floorRequirementMB = floorRequirementMB
            self.maximumParallelism = maximumParallelism
        }
    }

    /// What a grant is allowed to spend. `basis` says which measured quantity decided it, so a
    /// holder can print where its `-j` came from instead of appearing to have chosen it.
    struct Budget: Equatable {
        var parallelism: Int
        var basis: String
        var headroomMB: Int?
        var swapFreeMB: Int?
    }

    /// Why not even the floor could be admitted. Actionable by construction: how much, of which
    /// measured quantity, taken how — plus the largest holders of anonymous memory, **for a
    /// person to read, never as a target list**.
    struct Deficit: Equatable {
        var quantity: String
        var needMB: Int
        var haveMB: Int
        var takenAt: Date
        var method: String
        var topAnonymous: [MemoryHolder]
    }

    struct MemoryHolder: Equatable {
        var pid: Int32
        var command: String
        var anonymousMB: Int
    }

    enum Admission: Equatable {
        case granted(Budget)
        case refused(Deficit)
    }

    /// Admission degrades. The floor is one, and one is refused only against a measured
    /// requirement.
    static func admit(pressure: Pressure?, policy: Policy, topAnonymous: [MemoryHolder],
                      now: Date) -> Admission {
        guard let pressure else {
            // No reading. Fail closed on *size*, not on admission: the smallest possible grant
            // is still a grant, and refusing here would stop work on the absence of a number.
            return .granted(Budget(parallelism: 1, basis: "pressure_not_measured",
                                   headroomMB: nil, swapFreeMB: nil))
        }
        if let floor = policy.floorRequirementMB, pressure.headroomMB < floor {
            return .refused(Deficit(
                quantity: "headroom_mb (free + file-backed)", needMB: floor,
                haveMB: pressure.headroomMB, takenAt: pressure.observedAt,
                method: "vm_stat page counts and sysctl vm.swapusage",
                topAnonymous: topAnonymous))
        }
        // A ceiling, not a throttle: this can only lower the number of concurrent frontends,
        // and granting the floor is not a promise that the compile will fit.
        guard let per = policy.perCompileMB, per > 0 else {
            return .granted(Budget(parallelism: 1, basis: "peak_not_measured",
                                   headroomMB: pressure.headroomMB,
                                   swapFreeMB: pressure.swapFreeMB))
        }
        let affordable = pressure.headroomMB / per
        let ceiling = min(max(1, affordable), max(1, policy.maximumParallelism))
        return .granted(Budget(parallelism: ceiling,
                               basis: "headroom_mb/\(per)mb_per_compile",
                               headroomMB: pressure.headroomMB,
                               swapFreeMB: pressure.swapFreeMB))
    }

    // MARK: - The record

    enum Provenance: String, Equatable {
        /// Granted through a broker route: the session, task and root are known.
        case broker
        /// Adopted from a `holder.txt` this broker did not write — a script, or this same
        /// machine before the app restarted.
        case directory
    }

    struct Holder: Equatable {
        var leaseID: String
        var owner: Owner
        var pid: Int32
        var processStart: Date?
        var acquiredAt: Date
        var renewedAt: Date
        var workPIDs: [Int32]
        var doneFlagPath: String?
        var tree: String?
        var log: String?
        var note: String?
        var budget: Budget?
        var provenance: Provenance
        /// What the holder said it was doing at its last renewal.
        var phase: Phase
        /// When it entered that phase, so a reader gets "36 minutes" rather than a word.
        var phaseSince: Date
        /// When it was last in `compiling`. Nil means it has never said it was.
        var lastCompilingAt: Date?
        /// The beat file this holder touches. Nil falls back to `beat` inside the directory.
        var heartbeatPath: String?
        /// This hold's token, as it appears in `holder.txt`. For a lease this broker granted it
        /// is the request id; for one it adopted from the directory it is whatever the shell that
        /// took the lock minted. Carried so that a rewrite preserves the field the *other* two
        /// writers compare against — dropping it would tell a live shell holder's renewal loop
        /// that its lock had changed hands.
        var token: String? = nil
        /// The owner's start identity as the shell writers record it, when the record carried
        /// one. This broker never mints it; it only passes it through.
        var ownerStarted: String? = nil
    }

    /// One request in the line.
    ///
    /// **A waiter proves it is alive the same way a holder does.** It had no liveness axis at all:
    /// `pid` and `processStart` were recorded and read by nothing, so a waiter whose process died
    /// at the head of the queue left an entry that could never be granted, never expired, and
    /// could only be cancelled by an owner that no longer existed — while the lock itself was
    /// free. Every later acquirer was answered `queued_behind_others` for ever, the queue is
    /// persisted, so it survived an app restart, and after thirty-two such entries the state
    /// became `queue_full` for everybody: the same deadlock with a different code.
    ///
    /// The proof is the poll. `POST /v1/orchestrator/leases` is idempotent on `request_id` and a
    /// waiting client re-sends it every few seconds, so *asking again* is exactly what renewing is
    /// for a holder — and it needs no probe, because it is a fact this broker recorded itself
    /// rather than a reading of a machine that may not answer. ``lastPolledAt`` is that clock.
    struct Waiter: Equatable {
        var requestID: String
        var owner: Owner
        var pid: Int32
        var processStart: Date?
        var requestedAt: Date
        var reason: String
        /// The last time this request was asked for. Set to ``requestedAt`` when the entry is
        /// created, and moved forward by every poll. FIFO order still comes from ``requestedAt``,
        /// so a waiter that goes quiet and comes back does not lose its place in the line.
        var lastPolledAt: Date
    }

    /// What the last reconciliation found. Stored rather than derived, so "the record and the
    /// directory disagreed and the directory won" survives into the next reading instead of
    /// being a fact only the log remembers.
    enum Reconciliation: String, Equatable {
        case idle
        case matched
        case adopted
        case replaced
        case directoryMissing
        case unreadable
    }

    struct Record: Equatable {
        var resource: String
        var directory: String
        var holder: Holder?
        var queue: [Waiter]
        var reconciliation: Reconciliation
        var reconciledAt: Date?
        /// The last takeover verdict, so a query can say why the queue is not moving.
        var holdReason: String?
        /// The last liveness verdict, kept apart from `holdReason` because "the holder is
        /// proving it is alive" and "a compiler is running" are different facts and a reader
        /// acts on them differently.
        var livenessReason: String?
        /// The last admission refusal, kept so that **"asked and was told no" is not
        /// indistinguishable from "never asked"**.
        ///
        /// A refusal leaves the record otherwise untouched — no holder, no queue entry, nothing —
        /// so without this a session refused for lack of headroom looked exactly like a session
        /// that had never wanted the slot, in Bearings and in the Session overlay both. It is a
        /// note about the *asker*, not about the lease, which is why it sits beside the two
        /// verdicts rather than in the state word.
        var lastRefusal: RefusalNote?

        init(resource: String, directory: String = OrchestratorLease.defaultDirectory,
             holder: Holder? = nil, queue: [Waiter] = [], reconciliation: Reconciliation = .idle,
             reconciledAt: Date? = nil, holdReason: String? = nil,
             livenessReason: String? = nil, lastRefusal: RefusalNote? = nil) {
            self.resource = resource
            self.directory = directory
            self.holder = holder
            self.queue = queue
            self.reconciliation = reconciliation
            self.reconciledAt = reconciledAt
            self.holdReason = holdReason
            self.livenessReason = livenessReason
            self.lastRefusal = lastRefusal
        }
    }

    /// One refusal, as much of it as a projection may show.
    struct RefusalNote: Equatable {
        var code: String
        var message: String
        var at: Date
        var requestID: String
        var sessionID: String?
        var taskID: String?
    }

    // MARK: - Requests, refusals and decisions

    struct Request: Equatable {
        var requestID: String
        var resource: String
        var owner: Owner
        var pid: Int32
        var processStart: Date?
        var reason: String
        var tree: String?
        var log: String?
        var note: String?
        var workPIDs: [Int32]
        var doneFlagPath: String?
        var phase: Phase
        var heartbeatPath: String?
        /// An explicit, named override of the *admission* gate. It never touches exclusion: a
        /// lease somebody else holds is not available to an override. A gate with no door is the
        /// deadlock this feature exists to remove, so the door exists, is named, and is logged.
        var pressureOverride: String?
    }

    struct Refusal: Equatable, Error {
        var status: Int
        var code: String
        var message: String
        var extra: [String: String]

        init(status: Int, code: String, message: String, extra: [String: String] = [:]) {
            self.status = status
            self.code = code
            self.message = message
            self.extra = extra
        }
    }

    /// The filesystem work a decision implies. The caller performs it; nothing in this file does.
    ///
    /// `removeDirectory` carries the holder it expects to find, because the one rule a release
    /// path must never break is removing a lock somebody else owns. The executor re-reads
    /// `holder.txt` and removes the directory only when it still names this pid and start.
    enum SideEffect: Equatable {
        case none
        case createDirectory(HolderFile)
        case removeDirectory(expecting: HolderFile)
        /// Take over a directory whose holder has provably stopped proving it is alive: remove
        /// it, then create it again for the new holder. Two steps, one effect, so a caller
        /// cannot perform half of it.
        case takeOverDirectory(expecting: HolderFile, granting: HolderFile)
        /// Rewrite `holder.txt` in place, for a renewal that moved the work pids.
        case refreshHolderFile(HolderFile)
    }

    enum Outcome: Equatable {
        case granted(Budget)
        case queued(position: Int, holdReason: String)
        case released
        case renewed
        case cancelled
        case refused(Refusal)
    }

    /// One request asking for the slot, at one moment.
    ///
    /// **Asking is a fact, and no answer takes it back.** A waiter proves it is alive by asking
    /// again — the route is idempotent on `request_id` and a waiting client re-sends it every few
    /// seconds — so the poll clock has to move for *every* answer that request gets, not only for
    /// the answer that happens to be `queued`. It did not: `lastPolledAt` was moved in `enqueue`
    /// alone, so a head waiter refused for pressure, or told `lease_changed` because the lock moved
    /// between the reading and the write, polled exactly as the contract asks and was recorded as
    /// having gone quiet — passed over after ``waiterDeadline`` and droppable by the hard-limit
    /// trim, while a later arrival that asked at the right moment took the slot.
    struct Ask: Equatable {
        var requestID: String
        var at: Date
    }

    struct Decision: Equatable {
        var record: Record
        var effect: SideEffect
        var outcome: Outcome
        /// The ask this decision is an answer to, when there is one. `nil` for renew, release and
        /// cancel, which are not requests for the slot.
        var ask: Ask? = nil
    }

    // MARK: - Validation

    static func bounded(_ value: String?, limit: Int) -> String? {
        guard let raw = value else { return nil }
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, text.count <= limit,
              !text.unicodeScalars.contains(where: { $0.value == 0 }) else { return nil }
        return text
    }

    static func pids(from raw: Any?) -> [Int32] {
        var found: [Int32] = []
        if let numbers = raw as? [Int] {
            found = numbers.compactMap { $0 > 0 && $0 <= Int(Int32.max) ? Int32($0) : nil }
        } else if let text = raw as? String {
            found = text.split(separator: ",")
                .compactMap { Int32($0.trimmingCharacters(in: .whitespaces)) }
                .filter { $0 > 0 }
        }
        return Array(found.prefix(workPIDLimit))
    }

    /// Turn a request body into a ``Request``, or say exactly which field was wrong.
    static func request(from raw: [String: Any], requestID: String) -> Result<Request, Refusal> {
        guard let resource = bounded(raw["resource"] as? String, limit: labelLimit) else {
            return .failure(Refusal(status: 400, code: "bad_lease",
                                    message: "resource is required."))
        }
        guard resources.contains(resource) else {
            return .failure(Refusal(
                status: 400, code: "unknown_resource",
                message: "This machine leases \(resources.sorted().joined(separator: ", ")) "
                    + "and nothing else. A second resource needs a proven acquisition order "
                    + "before it can exist."))
        }
        let pidValue = (raw["pid"] as? Int) ?? (raw["pid"] as? NSNumber)?.intValue
        guard let pidValue, pidValue > 0, pidValue <= Int(Int32.max) else {
            return .failure(Refusal(status: 400, code: "bad_lease",
                                    message: "pid must be a positive process id, and it must be "
                                        + "the process doing the work rather than a sentinel."))
        }
        guard let label = bounded(raw["holder"] as? String, limit: labelLimit) else {
            return .failure(Refusal(status: 400, code: "bad_lease",
                                    message: "holder is required: it is what a person reading "
                                        + "the lock directory by hand sees."))
        }
        guard let reason = bounded(raw["reason"] as? String, limit: reasonLimit) else {
            return .failure(Refusal(status: 400, code: "bad_lease",
                                    message: "reason is required."))
        }
        let started = (raw["process_start"] as? Double).map(Date.init(timeIntervalSince1970:))
            ?? (raw["process_start"] as? Int).map { Date(timeIntervalSince1970: Double($0)) }
        let owner = Owner(sessionID: bounded(raw["session_id"] as? String, limit: labelLimit),
                          taskID: bounded(raw["task_id"] as? String, limit: labelLimit),
                          rootSessionID: bounded(raw["root_session_id"] as? String,
                                                 limit: labelLimit),
                          label: label)
        return .success(Request(
            requestID: requestID, resource: resource, owner: owner, pid: Int32(pidValue),
            processStart: started, reason: reason,
            tree: bounded(raw["tree"] as? String, limit: pathLimit),
            log: bounded(raw["log"] as? String, limit: pathLimit),
            note: bounded(raw["note"] as? String, limit: reasonLimit),
            workPIDs: pids(from: raw["work_pids"]),
            doneFlagPath: bounded(raw["done_flag"] as? String, limit: pathLimit),
            phase: (raw["phase"] as? String).flatMap(Phase.decode) ?? .unknown,
            heartbeatPath: bounded(raw["heartbeat"] as? String, limit: pathLimit),
            pressureOverride: bounded(raw["pressure_override"] as? String, limit: labelLimit)))
    }

    // MARK: - Reconciliation

    /// Rebuild the record's view from the directory it never stopped owning.
    ///
    /// This runs before every decision, not only after a restart, because the directory can
    /// change under the broker at any moment: `test.sh` on a contributor's shell takes it with
    /// `mkdir` and tells nobody, which is exactly the case this design promises to survive.
    ///
    /// **The directory always wins.** When the two disagree the record is corrected, never the
    /// other way round, and the correction is recorded as `replaced` so the disagreement is
    /// visible rather than smoothed over.
    ///
    /// The one case that does *not* free anything is a record with a holder and no directory. A
    /// missing directory could mean the holder released and the broker missed it, or that
    /// somebody removed a lock by hand while a compile ran. Freeing on the second would be the
    /// deadlock's opposite and worse: two compilers. So the holder is kept, the state is
    /// `directoryMissing`, and it clears only on the same evidence a takeover needs — both
    /// halves.
    static func reconcile(record: Record, directory: DirectoryState, evidence: Evidence,
                          now: Date) -> Record {
        var out = record
        out.reconciledAt = now
        switch directory {
        case .unreadable:
            out.reconciliation = .unreadable
            out.holdReason = "directory_unreadable"
            return out
        case .held(let file):
            // `file.owner`, not `file.pid`: a hold is a sequence — the compiler driver, then
            // the test binary — so the working pid changes *during* one hold, and comparing
            // against it made every shell holder look like a new holder halfway through.
            if var existing = out.holder,
               existing.pid == file.owner, sameStamp(existing.processStart, file.started) {
                // The same holder. A shell holder refreshes only the file, so take its renewal
                // and its work pids from there when they are newer than the record's.
                let fileIsNewer = file.renewed.map { $0 > existing.renewedAt } ?? false
                if fileIsNewer, let renewed = file.renewed { existing.renewedAt = renewed }
                if !file.work.isEmpty { existing.workPIDs = file.work }
                if let done = file.done { existing.doneFlagPath = done }
                if let token = file.token { existing.token = token }
                if let started = file.ownerStarted { existing.ownerStarted = started }
                // **The phase is a claim, and a stale snapshot of the file may not roll it back.**
                // It follows the renewal rule directly above it rather than being unconditional:
                // now that `file(from:)` writes the phase at all — it used to drop it, which is
                // how a broker-mediated renewal came to rewrite a live record without the field a
                // waiter reads — an older reading of the directory would otherwise overwrite what
                // the holder has just told the broker it is doing, and restart the phase clock
                // while doing it.
                if fileIsNewer, file.phase != .unknown, file.phase != existing.phase {
                    existing.phase = file.phase
                    existing.phaseSince = file.renewed ?? now
                }
                if fileIsNewer, file.phase == .compiling {
                    existing.lastCompilingAt = file.renewed ?? now
                }
                out.holder = existing
                out.reconciliation = .matched
                out.holdReason = nil
                return out
            }
            let adopted = Holder(
                leaseID: adoptedLeaseID(for: file), owner: Owner(label: file.holder),
                pid: file.owner, processStart: file.started,
                acquiredAt: file.started ?? now, renewedAt: file.renewed ?? now,
                workPIDs: file.work, doneFlagPath: file.done, tree: file.tree, log: file.log,
                note: file.note, budget: nil, provenance: .directory, phase: file.phase,
                phaseSince: file.phaseSince ?? file.renewed ?? now,
                lastCompilingAt: file.lastCompiling
                    ?? (file.phase == .compiling ? (file.renewed ?? now) : nil),
                heartbeatPath: file.heartbeat, token: file.token,
                ownerStarted: file.ownerStarted)
            out.reconciliation = out.holder == nil ? .adopted : .replaced
            out.holder = adopted
            out.holdReason = nil
            return out
        case .absent:
            guard let existing = out.holder else {
                out.reconciliation = .idle
                out.holdReason = nil
                out.livenessReason = nil
                return out
            }
            let alive = liveness(renewedAt: existing.renewedAt, owner: evidence.owner,
                                 doneFlag: evidence.doneFlag, beat: evidence.beat, now: now)
            let verdict = takeover(liveness: alive, compilers: evidence.compilers)
            out.livenessReason = alive.code
            if case .eligible = verdict {
                out.holder = nil
                out.reconciliation = .idle
                out.holdReason = nil
                return out
            }
            out.reconciliation = .directoryMissing
            out.holdReason = verdict.code
            return out
        }
    }

    /// A stable id for a holder this broker did not grant, so two readings of the same
    /// unregistered holder do not look like two different leases.
    static func adoptedLeaseID(for file: HolderFile) -> String {
        // The token when the record carries one, because that is the id its writer chose and the
        // one every other program compares against. The composed form is the fallback for a
        // record written before the contract, and it is built from the *run*, not from whatever
        // was working at that beat — otherwise one hold produced a new lease id per compile.
        if let token = file.token, !token.isEmpty { return token }
        return "directory-\(file.owner)-\(file.started.map { Int($0.timeIntervalSince1970) } ?? 0)"
    }

    static func sameStamp(_ left: Date?, _ right: Date?) -> Bool {
        guard let left, let right else { return left == nil && right == nil }
        return abs(left.timeIntervalSince(right)) <= startMatchTolerance
    }

    // MARK: - The four decisions

    /// Acquire, or join the queue.
    ///
    /// Idempotent on `request_id`: a caller polls this route with the same id until it is
    /// granted, and polling neither duplicates its queue entry nor loses its place. That is the
    /// whole client contract, and it is a poll rather than a push on purpose — a grant delivered
    /// as a message can be lost, and a lost grant is a lease nobody holds and nobody releases.
    static func acquire(record: Record, request: Request, directory: DirectoryState,
                        evidence: Evidence, policy: Policy = Policy(),
                        topAnonymous: [MemoryHolder] = [], now: Date) -> Decision {
        // Every route through the body below is an answer to one ask, so the ask is stamped once
        // here rather than at each of the eight places that return. ``perform`` applies it to
        // whichever record survives the effect, which is the half that matters when the effect
        // fails: `lease_changed` and `takeover_failed` discard the decision's record entirely, and
        // with it the poll this caller had just made.
        var decision = decideAcquire(record: record, request: request, directory: directory,
                                     evidence: evidence, policy: policy,
                                     topAnonymous: topAnonymous, now: now)
        decision.ask = Ask(requestID: request.requestID, at: now)
        return decision
    }

    /// Written down where a projection can find it: this request asked, whatever it was told.
    ///
    /// Order in the queue is untouched — that comes from ``Waiter/requestedAt`` — and a request
    /// that is not in the queue has nothing to move, which is the ordinary case for a caller
    /// refused before it ever joined the line.
    static func asked(_ record: Record, _ ask: Ask) -> Record {
        guard let index = record.queue.firstIndex(where: { $0.requestID == ask.requestID }),
              record.queue[index].lastPolledAt < ask.at
        else { return record }
        var out = record
        out.queue[index].lastPolledAt = ask.at
        return out
    }

    private static func decideAcquire(record: Record, request: Request,
                                      directory: DirectoryState, evidence: Evidence,
                                      policy: Policy, topAnonymous: [MemoryHolder],
                                      now: Date) -> Decision {
        var out = reconcile(record: record, directory: directory, evidence: evidence, now: now)

        // Already the holder — the answer to a repeated poll after the grant landed.
        if let holder = out.holder, holder.leaseID == request.requestID {
            return Decision(record: out, effect: .none,
                            outcome: .granted(holder.budget
                                ?? Budget(parallelism: 1, basis: "already_held",
                                          headroomMB: nil, swapFreeMB: nil)))
        }

        if let holder = out.holder {
            let alive = liveness(renewedAt: holder.renewedAt, owner: evidence.owner,
                                 doneFlag: evidence.doneFlag, beat: evidence.beat, now: now)
            out.livenessReason = alive.code
            let verdict = takeover(liveness: alive, compilers: evidence.compilers)
            guard case .eligible = verdict,
                  isHeadOfQueue(out, request: request, evidence: evidence, now: now) else {
                var reason = verdict.code
                if case .eligible = verdict { reason = "queued_behind_others" }
                return enqueue(out, request: request, holdReason: reason, evidence: evidence,
                               now: now)
            }
            switch admit(pressure: evidence.pressure, policy: policy,
                         topAnonymous: topAnonymous, now: now) {
            case .refused(let deficit) where request.pressureOverride == nil:
                return refuse(out, request: request, with: refusal(for: deficit), now: now)
            case .refused(let deficit):
                return grant(out, request: request, taking: holder,
                             budget: overrideBudget(deficit, by: request.pressureOverride),
                             now: now)
            case .granted(let budget):
                return grant(out, request: request, taking: holder, budget: budget, now: now)
            }
        }

        if case .unreadable = directory {
            return enqueue(out, request: request, holdReason: "directory_unreadable",
                           evidence: evidence, now: now)
        }

        // Free, and this caller is at the head of the line.
        guard isHeadOfQueue(out, request: request, evidence: evidence, now: now) else {
            return enqueue(out, request: request, holdReason: "queued_behind_others",
                           evidence: evidence, now: now)
        }
        switch admit(pressure: evidence.pressure, policy: policy, topAnonymous: topAnonymous,
                     now: now) {
        case .refused(let deficit) where request.pressureOverride == nil:
            return refuse(out, request: request, with: refusal(for: deficit), now: now)
        case .refused(let deficit):
            return grant(out, request: request, taking: nil,
                         budget: overrideBudget(deficit, by: request.pressureOverride), now: now)
        case .granted(let budget):
            return grant(out, request: request, taking: nil, budget: budget, now: now)
        }
    }

    /// A refusal, written down where a projection can find it.
    ///
    /// It moves the poll clock too, and that is not bookkeeping: **a refusal is an answer to an
    /// ask, and the ask is what a waiter's liveness is made of.** A head of the line refused for
    /// pressure keeps polling every five seconds exactly as the contract asks; with the clock
    /// frozen it read `waiter_stopped_asking` two minutes later, was passed over, and could be
    /// dropped by the hard-limit trim — while it was the one request on this machine that had
    /// never stopped asking. Latent only because `leasePolicy` carries no floor yet, and the next
    /// node in this feature's own plan is the one that measures the number for it.
    private static func refuse(_ record: Record, request: Request, with refusal: Refusal,
                               now: Date) -> Decision {
        var out = asked(record, Ask(requestID: request.requestID, at: now))
        out.lastRefusal = RefusalNote(code: refusal.code, message: refusal.message, at: now,
                                      requestID: request.requestID,
                                      sessionID: request.owner.sessionID,
                                      taskID: request.owner.taskID)
        return Decision(record: out, effect: .none, outcome: .refused(refusal))
    }

    private static func grant(_ record: Record, request: Request, taking previous: Holder?,
                              budget: Budget, now: Date) -> Decision {
        var out = record
        // Served: the refusal this caller was carrying is answered and must stop being shown.
        if out.lastRefusal?.requestID == request.requestID { out.lastRefusal = nil }
        var fresh = holder(from: request, now: now)
        fresh.budget = budget
        fresh.token = request.requestID
        out.holder = fresh
        out.queue.removeAll { $0.requestID == request.requestID }
        out.reconciliation = .matched
        out.holdReason = nil
        out.livenessReason = Liveness.proving.code
        let granting = holderFile(for: request, now: now)
        let effect: SideEffect = previous.map {
            .takeOverDirectory(expecting: file(from: $0), granting: granting)
        } ?? .createDirectory(granting)
        return Decision(record: out, effect: effect, outcome: .granted(budget))
    }

    private static func overrideBudget(_ deficit: Deficit, by owner: String?) -> Budget {
        Budget(parallelism: 1,
               basis: "pressure_override by \(owner ?? "unnamed"): "
                   + "\(deficit.quantity) had \(deficit.haveMB) MB of \(deficit.needMB) MB",
               headroomMB: deficit.haveMB, swapFreeMB: nil)
    }

    /// A refusal a person can act on: how much, of which measured quantity, taken how — and the
    /// largest holders of anonymous memory, so "why am I waiting" is never a spinner.
    static func refusal(for deficit: Deficit) -> Refusal {
        let names = deficit.topAnonymous.prefix(5)
            .map { "\($0.command) (pid \($0.pid), \($0.anonymousMB) MB)" }
            .joined(separator: ", ")
        return Refusal(
            status: 409, code: "pressure_refused",
            message: "This Mac cannot admit even one compiler: \(deficit.quantity) is "
                + "\(deficit.haveMB) MB against \(deficit.needMB) MB, measured by "
                + "\(deficit.method). Largest anonymous-memory holders, for you to look at: "
                + (names.isEmpty ? "not measured" : names)
                + ". Nothing here will end any of them. To proceed anyway, repeat this request "
                + "with pressure_override naming who decided.",
            extra: ["quantity": deficit.quantity, "need_mb": "\(deficit.needMB)",
                    "have_mb": "\(deficit.haveMB)", "method": deficit.method,
                    "taken_at": "\(Int(deficit.takenAt.timeIntervalSince1970))"])
    }

    /// Whether a waiter is still proving it is waiting, and why not when it is not.
    ///
    /// Two axes, the same shape as the holder's, and in the same order of authority:
    ///
    ///   * **The poll clock.** A request not asked for within ``waiterDeadline`` has stopped
    ///     proving. This axis needs no subprocess and cannot be broken by a machine that will not
    ///     answer, which is why it is the primary one.
    ///   * **The process.** Only ever a *strengthening*: a pid this machine says is gone, or is a
    ///     different process now, stops the waiter at once instead of after two minutes. A reading
    ///     that could not be taken says nothing and leaves the clock to decide — the same rule the
    ///     holder side follows, applied here.
    ///
    /// It never removes anybody. An unproven waiter keeps its place and is passed over; if it
    /// starts polling again it is at the front of the line again, because order comes from
    /// ``Waiter/requestedAt`` and not from when it last spoke.
    static func waiterLiveness(_ waiter: Waiter, process: ProcessObservation,
                               now: Date) -> Liveness {
        // `identity` already folds "no such process" and "a different process has that number"
        // into `gone`, and everything it could not read into `unknown`.
        if case .gone = identity(recordedStart: waiter.processStart, observed: process) {
            return .stopped("waiter_process_gone")
        }
        if now.timeIntervalSince(waiter.lastPolledAt) > waiterDeadline {
            return .stopped("waiter_stopped_asking")
        }
        return .proving
    }

    /// The first waiter still proving it is waiting, or `nil` when nobody is.
    ///
    /// The process axis is applied to the front of the line only, and that is deliberate rather
    /// than lazy: probing every entry would run up to thirty-two `ps` calls inside one decision,
    /// and it is the head that blocks the machine. Everything behind it is decided by the poll
    /// clock, which costs nothing.
    static func headOfQueue(_ record: Record, evidence: Evidence, now: Date) -> Waiter? {
        for (index, waiter) in record.queue.enumerated() {
            let process: ProcessObservation = index == 0 ? evidence.headWaiterProcess : .unknown("not probed")
            if case .proving = waiterLiveness(waiter, process: process, now: now) {
                return waiter
            }
        }
        return nil
    }

    /// FIFO with one exception that is not an exception: a caller not yet in the queue is at the
    /// head only when nobody ahead of it is still asking. Joining does not jump.
    private static func isHeadOfQueue(_ record: Record, request: Request, evidence: Evidence,
                                     now: Date) -> Bool {
        guard let head = headOfQueue(record, evidence: evidence, now: now) else { return true }
        return head.requestID == request.requestID
    }

    private static func enqueue(_ record: Record, request: Request, holdReason: String,
                                evidence: Evidence, now: Date) -> Decision {
        var out = record
        out.holdReason = holdReason
        // Asking again *is* the proof of life, so a repeat poll moves the clock. `requestedAt` is
        // untouched: that is the place in the line, and re-asking must not cost it.
        if let index = out.queue.firstIndex(where: { $0.requestID == request.requestID }) {
            out.queue[index].lastPolledAt = now
            return Decision(record: out, effect: .none,
                            outcome: .queued(position: index + 1, holdReason: holdReason))
        }
        // The depth limit counts the line, not the litter. Thirty-two entries whose owners have
        // all stopped asking used to answer 429 to everybody for ever.
        let proving = out.queue.enumerated().filter { index, waiter in
            let process: ProcessObservation = index == 0 ? evidence.headWaiterProcess : .unknown("not probed")
            if case .proving = waiterLiveness(waiter, process: process, now: now) { return true }
            return false
        }.count
        guard proving < queueDepthLimit else {
            return Decision(record: out, effect: .none, outcome: .refused(Refusal(
                status: 429, code: "queue_full",
                message: "\(queueDepthLimit) requests are already waiting for "
                    + "\(record.resource). Something on this Mac is wedged; look at the holder "
                    + "before adding to the line.")))
        }
        out.queue.append(Waiter(requestID: request.requestID, owner: request.owner,
                                pid: request.pid, processStart: request.processStart,
                                requestedAt: now, reason: request.reason, lastPolledAt: now))
        // And the array itself stays bounded. Passing an unproven waiter over keeps its place for
        // it; keeping every one of them for ever would grow a list nobody will ever read. Above
        // the hard limit the longest-silent entries go, and never a waiter that is still asking.
        if out.queue.count > queueHardLimit {
            let quiet = out.queue.enumerated()
                .filter { $0.element.requestID != request.requestID
                    && now.timeIntervalSince($0.element.lastPolledAt) > waiterDeadline }
                .sorted { $0.element.lastPolledAt < $1.element.lastPolledAt }
                .prefix(out.queue.count - queueHardLimit)
                .map { $0.offset }
            for index in quiet.sorted(by: >) { out.queue.remove(at: index) }
        }
        let position = (out.queue.firstIndex { $0.requestID == request.requestID } ?? 0) + 1
        return Decision(record: out, effect: .none,
                        outcome: .queued(position: position, holdReason: holdReason))
    }

    /// Renew: the proof of life, and the only thing that keeps a lease.
    ///
    /// It also refreshes what is actually working right now, so a reader can tell "holder alive,
    /// compiling" from "holder alive, between compiles" instead of guessing from the machine.
    static func renew(record: Record, leaseID: String, owner: Owner, workPIDs: [Int32],
                      phase: Phase = .unknown, directory: DirectoryState, evidence: Evidence,
                      now: Date) -> Decision {
        let out = reconcile(record: record, directory: directory, evidence: evidence, now: now)
        guard let holder = out.holder else {
            return Decision(record: out, effect: .none, outcome: .refused(Refusal(
                status: 409, code: "lease_lost",
                message: "Nothing holds \(record.resource) now, so there is nothing to renew. "
                    + "Acquire it again before compiling.")))
        }
        guard holder.leaseID == leaseID, sameOwner(holder.owner, owner) else {
            return Decision(record: out, effect: .none, outcome: .refused(Refusal(
                status: 403, code: "not_holder",
                message: "\(leaseID) does not hold \(record.resource); \(holder.leaseID) does.")))
        }
        var renewed = out
        renewed.holder?.renewedAt = now
        if !workPIDs.isEmpty {
            renewed.holder?.workPIDs = Array(workPIDs.prefix(workPIDLimit))
        }
        // The phase is the *content* of a renewal: the clock says the holder is there, this says
        // what it is there doing. `phaseSince` moves only when the phase itself changes, so a
        // reader gets "36 minutes in `writing`" rather than "renewed 4 seconds ago".
        if phase != .unknown {
            if phase != holder.phase {
                renewed.holder?.phase = phase
                renewed.holder?.phaseSince = now
            }
            if phase == .compiling { renewed.holder?.lastCompilingAt = now }
        }
        renewed.livenessReason = Liveness.proving.code
        guard let refreshed = renewed.holder else {
            return Decision(record: renewed, effect: .none, outcome: .renewed)
        }
        return Decision(record: renewed, effect: .refreshHolderFile(file(from: refreshed)),
                        outcome: .renewed)
    }

    /// Release. Only the holder may, and the directory is removed only when it still names them.
    static func release(record: Record, leaseID: String, owner: Owner,
                        directory: DirectoryState, evidence: Evidence, now: Date) -> Decision {
        let out = reconcile(record: record, directory: directory, evidence: evidence, now: now)
        guard let holder = out.holder else {
            // Releasing something nobody holds is the shape a retry takes after the first
            // release succeeded. It is not an error and must not read as one.
            return Decision(record: out, effect: .none, outcome: .released)
        }
        guard holder.leaseID == leaseID, sameOwner(holder.owner, owner) else {
            return Decision(record: out, effect: .none, outcome: .refused(Refusal(
                status: 403, code: "not_holder",
                message: "Only the session recorded as holding \(record.resource) may release "
                    + "it. It is held by \(holder.owner.label) (pid \(holder.pid)).")))
        }
        var released = out
        released.holder = nil
        released.reconciliation = .idle
        released.holdReason = nil
        released.livenessReason = nil
        return Decision(record: released, effect: .removeDirectory(expecting: file(from: holder)),
                        outcome: .released)
    }

    /// A queued caller that gave up removes only itself.
    static func cancel(record: Record, requestID: String, owner: Owner) -> Decision {
        var out = record
        guard let index = out.queue.firstIndex(where: { $0.requestID == requestID }) else {
            return Decision(record: out, effect: .none, outcome: .refused(Refusal(
                status: 404, code: "not_queued",
                message: "No request named \(requestID) is waiting for \(record.resource).")))
        }
        guard sameOwner(out.queue[index].owner, owner) else {
            return Decision(record: out, effect: .none, outcome: .refused(Refusal(
                status: 403, code: "not_requester",
                message: "Only the session that made a request may cancel it.")))
        }
        out.queue.remove(at: index)
        return Decision(record: out, effect: .none, outcome: .cancelled)
    }

    /// Two owners are the same when the identity each of them actually carries agrees. A shell
    /// holder has only a label; a session holder has a session id, and then the session id is
    /// what has to match.
    static func sameOwner(_ left: Owner, _ right: Owner) -> Bool {
        if let leftSession = left.sessionID, let rightSession = right.sessionID {
            return leftSession == rightSession
        }
        if let leftTask = left.taskID, let rightTask = right.taskID { return leftTask == rightTask }
        return left.label == right.label
    }

    static func holder(from request: Request, now: Date) -> Holder {
        Holder(leaseID: request.requestID, owner: request.owner, pid: request.pid,
               processStart: request.processStart, acquiredAt: now, renewedAt: now,
               workPIDs: request.workPIDs, doneFlagPath: request.doneFlagPath,
               tree: request.tree, log: request.log, note: request.note, budget: nil,
               provenance: .broker, phase: request.phase, phaseSince: now,
               lastCompilingAt: request.phase == .compiling ? now : nil,
               heartbeatPath: request.heartbeatPath)
    }

    static func holderFile(for request: Request, now: Date) -> HolderFile {
        HolderFile(holder: request.owner.label,
                   pid: request.workPIDs.first ?? request.pid,
                   started: request.processStart ?? now, tree: request.tree, log: request.log,
                   note: request.note, work: request.workPIDs, done: request.doneFlagPath,
                   renewed: now, phase: request.phase,
                   heartbeat: request.heartbeatPath ?? beatPath(inside: defaultDirectory),
                   ownerPID: request.pid, ownerStarted: nil, token: request.requestID,
                   heartbeatDeadline: Int(renewalDeadline), phaseSince: now,
                   lastCompiling: request.phase == .compiling ? now : nil, compilers: nil)
    }

    /// A holder as the record spells it. **It used to drop `phase` and `heartbeat`**, so every
    /// broker-mediated renewal rewrote a live record without the two fields a waiter reads to tell
    /// "compiling" from "finished and forgot to let go" — the exact ambiguity the phase field was
    /// added to remove — and without the path of the beat that proves the holder is there.
    static func file(from holder: Holder) -> HolderFile {
        HolderFile(holder: holder.owner.label,
                   pid: holder.workPIDs.first ?? holder.pid,
                   started: holder.processStart,
                   tree: holder.tree, log: holder.log, note: holder.note, work: holder.workPIDs,
                   done: holder.doneFlagPath, renewed: holder.renewedAt, phase: holder.phase,
                   heartbeat: holder.heartbeatPath, ownerPID: holder.pid,
                   ownerStarted: holder.ownerStarted, token: holder.token ?? holder.leaseID,
                   heartbeatDeadline: Int(renewalDeadline), phaseSince: holder.phaseSince,
                   lastCompiling: holder.lastCompilingAt, compilers: nil)
    }

    // MARK: - The wire

    static func record(_ record: Record, now: Date) -> [String: Any] {
        var out: [String: Any] = [
            "resource": record.resource,
            "directory": record.directory,
            "reconciliation": record.reconciliation.rawValue,
            "renewalDeadlineSeconds": Int(renewalDeadline),
            "queueDepth": record.queue.count,
            "queue": record.queue.enumerated().map { index, waiter -> [String: Any] in
                var row: [String: Any] = [
                    "requestId": waiter.requestID,
                    "holder": waiter.owner.label,
                    "pid": Int(waiter.pid),
                    "position": index + 1,
                    "reason": waiter.reason,
                    "requestedAt": Int(waiter.requestedAt.timeIntervalSince1970),
                    "waitedSeconds": max(0, Int(now.timeIntervalSince(waiter.requestedAt)))
                ]
                if let session = waiter.owner.sessionID { row["sessionId"] = session }
                if let task = waiter.owner.taskID { row["taskId"] = task }
                if let root = waiter.owner.rootSessionID { row["rootSessionId"] = root }
                return row
            }
        ]
        if let reason = record.holdReason { out["holdReason"] = reason }
        if let reason = record.livenessReason { out["livenessReason"] = reason }
        if let at = record.reconciledAt { out["reconciledAt"] = Int(at.timeIntervalSince1970) }
        guard let holder = record.holder else {
            out["holder"] = NSNull()
            return out
        }
        var row: [String: Any] = [
            "leaseId": holder.leaseID,
            "holder": holder.owner.label,
            "pid": Int(holder.pid),
            "provenance": holder.provenance.rawValue,
            "acquiredAt": Int(holder.acquiredAt.timeIntervalSince1970),
            "renewedAt": Int(holder.renewedAt.timeIntervalSince1970),
            "renewalAgeSeconds": max(0, Int(now.timeIntervalSince(holder.renewedAt))),
            "heldSeconds": max(0, Int(now.timeIntervalSince(holder.acquiredAt))),
            "workPids": holder.workPIDs.map(Int.init)
        ]
        if let start = holder.processStart {
            row["processStart"] = Int(start.timeIntervalSince1970)
        }
        if let session = holder.owner.sessionID { row["sessionId"] = session }
        if let task = holder.owner.taskID { row["taskId"] = task }
        if let root = holder.owner.rootSessionID { row["rootSessionId"] = root }
        if let tree = holder.tree { row["tree"] = tree }
        if let log = holder.log { row["log"] = log }
        if let note = holder.note { row["note"] = note }
        if let done = holder.doneFlagPath { row["doneFlag"] = done }
        // What the holder is doing, and — the whole reason the field exists — how long it has
        // been doing something other than compiling. These are facts a waiter reads so it knows
        // who to ask. **None of them is an admission input**: `attention` below is a sentence,
        // not a verdict, and the takeover rule never consults any of it.
        row["phase"] = holder.phase.rawValue
        row["phaseAgeSeconds"] = max(0, Int(now.timeIntervalSince(holder.phaseSince)))
        row["heartbeatAgeSeconds"] = max(0, Int(now.timeIntervalSince(holder.renewedAt)))
        row["heartbeat"] = holder.heartbeatPath ?? NSNull()
        if let compiling = holder.lastCompilingAt {
            row["lastCompilingAt"] = Int(compiling.timeIntervalSince1970)
            if holder.phase != .compiling {
                row["notCompilingForSeconds"] = max(0, Int(now.timeIntervalSince(compiling)))
            }
        } else {
            row["lastCompilingAt"] = NSNull()
        }
        if holder.phase != .compiling {
            let idleFor = now.timeIntervalSince(holder.lastCompilingAt ?? holder.acquiredAt)
            if idleFor > phaseAttentionAfter {
                row["attention"] = "\(holder.owner.label) has not been compiling for "
                    + "\(Int(idleFor / 60)) minutes (phase \(holder.phase.rawValue)), and last "
                    + "renewed \(max(0, Int(now.timeIntervalSince(holder.renewedAt)))) seconds "
                    + "ago. Ask it; nothing here will reclaim the lock for that reason."
            }
        }
        if let budget = holder.budget {
            var budgetRow: [String: Any] = ["parallelism": budget.parallelism,
                                            "basis": budget.basis]
            if let headroom = budget.headroomMB { budgetRow["headroomMb"] = headroom }
            if let swap = budget.swapFreeMB { budgetRow["swapFreeMb"] = swap }
            row["budget"] = budgetRow
        }
        out["holder"] = row
        return out
    }

    // MARK: - Probes and the filesystem, the only impure things in this file

    // MARK: - The projections

    /// What Bearings shows. **No probes**: Bearings is a projection that runs on a redraw, and a
    /// redraw must never start a subprocess. The freshness stamp is how a reader knows that —
    /// ``observedAt`` is when the registry last reconciled against the lock directory, not now.
    struct Bearings: Equatable {
        var holder: String?
        var queueDepth: Int
        var holdReason: String?
        /// `missing`, `unknown`, `zero`, `queued`, `held` or `refused`. Six words that a single
        /// spinner would collapse: "no record yet" is not "nobody is compiling", and "asked and
        /// was told no" is not "never asked".
        var state: String
        var observedAt: Date?
    }

    /// How recent a refusal has to be to still be worth showing. A refusal is a moment, not a
    /// state, so it ages out rather than lingering — but it has to outlive the waiter it is about.
    ///
    /// It was exactly ``waiterDeadline``, and the two clocks then expired together: the moment a
    /// waiter was declared to have stopped asking was the moment its refusal stopped being shown,
    /// so a person opening Bearings at that instant saw neither fact — not the request, and not
    /// the reason it had been given. One whole deadline of overlap, so the answer is still on
    /// screen after the asker has been declared silent.
    static let refusalVisibleFor: TimeInterval = waiterDeadline * 2

    static func bearings(_ record: Record?, now: Date) -> Bearings {
        guard let record else { return Bearings(holder: nil, queueDepth: 0, holdReason: nil,
                                                state: "missing", observedAt: nil) }
        if record.reconciliation == .unreadable {
            return Bearings(holder: nil, queueDepth: record.queue.count,
                            holdReason: record.holdReason, state: "unknown",
                            observedAt: record.reconciledAt)
        }
        guard let holder = record.holder else {
            // Nobody holds it and nobody is waiting — but somebody may have asked and been told
            // no, and that is a third thing.
            if record.queue.isEmpty, let refusal = record.lastRefusal,
               now.timeIntervalSince(refusal.at) <= refusalVisibleFor {
                return Bearings(holder: nil, queueDepth: 0, holdReason: refusal.code,
                                state: "refused", observedAt: record.reconciledAt)
            }
            return Bearings(holder: nil, queueDepth: record.queue.count,
                            holdReason: record.holdReason,
                            state: record.queue.isEmpty ? "zero" : "queued",
                            observedAt: record.reconciledAt)
        }
        return Bearings(holder: holder.owner.label, queueDepth: record.queue.count,
                        holdReason: record.holdReason, state: "held",
                        observedAt: record.reconciledAt)
    }

    /// The quiet Session overlay. It follows the coordination-wait precedent exactly:
    /// `SessionState.waiting` means *a person must answer* and this is not that, so a session
    /// holding, queued for, or refused the compile slot keeps whatever terminal state it had.
    static func sessionRow(_ record: Record?, forSession id: String, now: Date) -> [String: Any]? {
        guard let record else { return nil }
        if let holder = record.holder, holder.owner.sessionID == id {
            var row: [String: Any] = [
                "state": "holding", "resource": record.resource, "leaseId": holder.leaseID,
                "heldSeconds": max(0, Int(now.timeIntervalSince(holder.acquiredAt))),
                "renewalAgeSeconds": max(0, Int(now.timeIntervalSince(holder.renewedAt))),
                "queueDepth": record.queue.count,
            ]
            if let budget = holder.budget { row["parallelism"] = budget.parallelism }
            row["phase"] = holder.phase.rawValue
            return row
        }
        if let index = record.queue.firstIndex(where: { $0.owner.sessionID == id }) {
            let waiter = record.queue[index]
            var row: [String: Any] = [
                "state": "queued", "resource": record.resource, "position": index + 1,
                "requestId": waiter.requestID,
                "waitedSeconds": max(0, Int(now.timeIntervalSince(waiter.requestedAt))),
                "holdReason": record.holdReason ?? "unknown",
                "queueDepth": record.queue.count,
            ]
            // The waiter's own proof of life, said out loud: a queue entry that has stopped
            // asking is passed over for admission, and a person looking at this row should be
            // able to see that rather than wonder why the line is not moving.
            if case .stopped(let why) = waiterLiveness(waiter, process: .unknown("not probed"),
                                                       now: now) {
                row["liveness"] = why
            } else {
                row["liveness"] = Liveness.proving.code
            }
            return row
        }
        // Refused, and still recent enough to mean something. Without this a session told "this
        // Mac cannot admit even one compiler" is indistinguishable from one that never asked.
        if let refusal = record.lastRefusal, refusal.sessionID == id,
           now.timeIntervalSince(refusal.at) <= refusalVisibleFor {
            return ["state": "refused", "resource": record.resource,
                    "requestId": refusal.requestID, "reason": refusal.code,
                    "message": refusal.message,
                    "refusedSecondsAgo": max(0, Int(now.timeIntervalSince(refusal.at))),
                    "queueDepth": record.queue.count]
        }
        return nil
    }

    /// One word for an audit line.
    static func outcomeWord(_ outcome: Outcome) -> String {
        switch outcome {
        case .granted: return "granted"
        case .queued(_, let reason): return "queued:" + reason
        case .released: return "released"
        case .renewed: return "renewed"
        case .cancelled: return "cancelled"
        case .refused(let refusal): return refusal.code
        }
    }

    /// Every observation and every filesystem operation, injectable as a whole. Production uses
    /// ``live``; a test supplies closures and drives every branch without starting a process or
    /// writing to `/tmp`.
    struct Probes {
        var processStart: (Int32) -> ProcessObservation
        var compilers: () -> CompilerObservation
        var pressure: () -> Pressure?
        var topAnonymous: () -> [MemoryHolder]
        var readDirectory: (String) -> DirectoryState
        var fileExists: (String) -> Bool
        /// The modification time of the beat file, or nil when there is not one.
        var beat: (String) -> Date?
        /// `mkdir` plus `holder.txt`. Returns nil on success, or why it failed. A directory that
        /// already exists must report failure — that is the atomicity the whole design rests on.
        var createDirectory: (String, HolderFile) -> String?
        /// Rewrite `holder.txt` in place, only when it still names this holder.
        var refreshHolderFile: (String, HolderFile) -> String?
        /// Remove the directory only if `holder.txt` still names this holder.
        var removeDirectory: (String, HolderFile) -> String?
    }

    static var live: Probes {
        Probes(processStart: liveProcessStart, compilers: liveCompilers, pressure: { livePressure() },
               topAnonymous: { liveTopAnonymous() }, readDirectory: readDirectory,
               fileExists: { FileManager.default.fileExists(atPath: $0) }, beat: beatTime,
               createDirectory: createDirectory, refreshHolderFile: refreshHolderFile,
               removeDirectory: removeDirectory)
    }

    static func liveProcessStart(_ pid: Int32) -> ProcessObservation {
        let run = shell("/bin/ps", ["-o", "lstart=", "-p", "\(pid)"])
        return parseProcessStart(run.out, status: run.status, timedOut: run.timedOut)
    }

    static func liveCompilers() -> CompilerObservation {
        let run = shell("/bin/ps", ["-axo", "pid=,comm="])
        return parseCompilerScan(run.out, status: run.status, timedOut: run.timedOut)
    }

    /// `vm_stat` plus `sysctl vm.swapusage`, and nothing derived from summed RSS.
    static func livePressure(now: Date = Date()) -> Pressure? {
        let vmStat = shell("/usr/bin/vm_stat", [])
        let swap = shell("/usr/sbin/sysctl", ["-n", "vm.swapusage"])
        let memory = shell("/usr/sbin/sysctl", ["-n", "hw.memsize"])
        guard vmStat.status == 0, swap.status == 0 else { return nil }
        return parsePressure(vmStat: vmStat.out, swapusage: swap.out, memsize: memory.out,
                             now: now)
    }

    static func parsePressure(vmStat: String, swapusage: String, memsize: String, now: Date)
        -> Pressure? {
        var pageSize = 4_096.0
        var counts: [String: Double] = [:]
        for rawLine in vmStat.split(separator: "\n") {
            let line = String(rawLine)
            if line.hasPrefix("Mach Virtual Memory Statistics") {
                if let open = line.range(of: "page size of "),
                   let close = line.range(of: " bytes", range: open.upperBound..<line.endIndex),
                   let size = Double(line[open.upperBound..<close.lowerBound]) {
                    pageSize = size
                }
                continue
            }
            guard let colon = line.firstIndex(of: ":") else { continue }
            let key = String(line[line.startIndex..<colon])
            let value = line[line.index(after: colon)...]
                .trimmingCharacters(in: .whitespaces)
                .replacingOccurrences(of: ".", with: "")
            guard let number = Double(value) else { continue }
            counts[key] = number
        }
        guard !counts.isEmpty else { return nil }
        func megabytes(_ key: String) -> Int { Int((counts[key] ?? 0) * pageSize / 1_048_576) }
        var swapUsed = 0, swapFree = 0, swapTotal = 0
        // `sysctl -n vm.swapusage` prints `total = 12288.00M  used = 10870.75M  free = 1417.25M`
        // — spaces around the equals. Normalising first is why this reads three numbers rather
        // than three zeroes, which is the shape a silent "no pressure reading" would take.
        for field in swapusage.replacingOccurrences(of: " = ", with: "=")
            .split(whereSeparator: { $0 == " " || $0 == "\n" }) {
            let text = String(field)
            func value(_ prefix: String) -> Int? {
                guard text.hasPrefix(prefix) else { return nil }
                return Int(Double(text.dropFirst(prefix.count)
                    .replacingOccurrences(of: "M", with: "")) ?? 0)
            }
            if let total = value("total=") { swapTotal = total }
            if let used = value("used=") { swapUsed = used }
            if let free = value("free=") { swapFree = free }
        }
        let physical = Int((Double(memsize.trimmingCharacters(in: .whitespacesAndNewlines))
            ?? 0) / 1_048_576)
        return Pressure(
            physicalMB: physical, freeMB: megabytes("Pages free"),
            anonymousMB: megabytes("Anonymous pages"),
            fileBackedMB: megabytes("File-backed pages"),
            compressorMB: megabytes("Pages occupied by compressor"),
            swapUsedMB: swapUsed, swapFreeMB: swapFree, swapTotalMB: swapTotal, observedAt: now)
    }

    /// The largest holders of anonymous memory, **for a person to read**. Nothing in this build
    /// acts on this list.
    static func liveTopAnonymous(limit: Int = 5) -> [MemoryHolder] {
        let run = shell("/bin/ps", ["-axo", "pid=,rss=,comm="])
        guard run.status == 0 else { return [] }
        var found: [MemoryHolder] = []
        for rawLine in run.out.split(separator: "\n") {
            let parts = rawLine.trimmingCharacters(in: .whitespaces)
                .split(separator: " ", maxSplits: 2, omittingEmptySubsequences: true)
            guard parts.count == 3, let pid = Int32(parts[0]), let rss = Int(parts[1]) else {
                continue
            }
            let command = String(parts[2]).split(separator: "/").last.map(String.init)
                ?? String(parts[2])
            found.append(MemoryHolder(pid: pid, command: command, anonymousMB: rss / 1024))
        }
        return Array(found.sorted { $0.anonymousMB > $1.anonymousMB }.prefix(limit))
    }

    static func beatTime(_ path: String) -> Date? {
        (try? FileManager.default.attributesOfItem(atPath: path))?[.modificationDate] as? Date
    }

    static func readDirectory(_ path: String) -> DirectoryState {
        var isDirectory: ObjCBool = false
        guard FileManager.default.fileExists(atPath: path, isDirectory: &isDirectory) else {
            return .absent
        }
        guard isDirectory.boolValue else {
            return .unreadable("\(path) exists and is not a directory")
        }
        let holderPath = (path as NSString).appendingPathComponent("holder.txt")
        guard let text = try? String(contentsOfFile: holderPath, encoding: .utf8) else {
            return .unreadable("\(holderPath) is missing or unreadable")
        }
        guard let file = parseHolderFile(text) else {
            return .unreadable("\(holderPath) has no usable pid= line")
        }
        return .held(file)
    }

    static func createDirectory(_ path: String, holder: HolderFile) -> String? {
        // `mkdir` and nothing above it: this one call is the exclusion, and
        // `withIntermediateDirectories: true` would silently succeed on an existing directory,
        // which is precisely the race this is here to lose loudly.
        do {
            try FileManager.default.createDirectory(atPath: path,
                                                    withIntermediateDirectories: false)
        } catch {
            return "\(path) could not be created: \(error.localizedDescription)"
        }
        let holderPath = (path as NSString).appendingPathComponent("holder.txt")
        do {
            try encode(holder).write(toFile: holderPath, atomically: true, encoding: .utf8)
        } catch {
            // The directory is taken and its identity could not be written. Give it back rather
            // than leaving a lock nobody can be shown to own.
            try? FileManager.default.removeItem(atPath: path)
            return "\(holderPath) could not be written: \(error.localizedDescription)"
        }
        touchBeat(inside: path)
        return nil
    }

    /// The first beat, and every beat a broker-mediated renewal produces. It lives inside the
    /// lock directory, so `rmdir` takes it with the lock and no orphan heartbeat can survive the
    /// work it stood for.
    static func touchBeat(inside directory: String) {
        let path = beatPath(inside: directory)
        if FileManager.default.fileExists(atPath: path) {
            try? FileManager.default.setAttributes([.modificationDate: Date()], ofItemAtPath: path)
        } else {
            FileManager.default.createFile(atPath: path, contents: Data())
        }
    }

    static func refreshHolderFile(_ path: String, holder: HolderFile) -> String? {
        switch readDirectory(path) {
        case .absent:
            return "\(path) is gone"
        case .unreadable(let why):
            return "refusing to rewrite a holder file that cannot be read: \(why)"
        case .held(let file):
            guard file.owner == holder.owner, sameStamp(file.started, holder.started) else {
                return "refusing to rewrite a lock held by pid \(file.owner), not \(holder.owner)"
            }
            // The token is the identity a pid cannot give: pids are reused within hours here, so
            // a record whose number matches and whose token does not is not this holder's.
            if let mine = holder.token, let theirs = file.token, mine != theirs {
                return "refusing to rewrite a lock whose token is \(theirs), not \(mine)"
            }
            let holderPath = (path as NSString).appendingPathComponent("holder.txt")
            do {
                try encode(holder).write(toFile: holderPath, atomically: true, encoding: .utf8)
            } catch {
                return "\(holderPath) could not be written: \(error.localizedDescription)"
            }
            touchBeat(inside: path)
            return nil
        }
    }

    static func removeDirectory(_ path: String, expecting: HolderFile) -> String? {
        switch readDirectory(path) {
        case .absent:
            return nil
        case .unreadable(let why):
            return "refusing to remove a lock whose holder cannot be read: \(why)"
        case .held(let file):
            guard file.owner == expecting.owner, sameStamp(file.started, expecting.started) else {
                return "refusing to remove a lock held by pid \(file.owner), not \(expecting.owner)"
            }
            if let mine = expecting.token, let theirs = file.token, mine != theirs {
                return "refusing to remove a lock whose token is \(theirs), not \(mine)"
            }
            do {
                try FileManager.default.removeItem(atPath: path)
            } catch {
                return "\(path) could not be removed: \(error.localizedDescription)"
            }
            return nil
        }
    }

    // MARK: - One turn of the crank

    /// What a decision became after its filesystem work was attempted.
    struct Applied {
        var record: Record
        var outcome: Outcome
    }

    /// Read the machine once, so a decision is made against one reading rather than several.
    ///
    /// The owner axis is a closure because it is the one thing this file cannot know: whether
    /// the task that took the lease has reached a terminal state is the broker's own knowledge,
    /// and it is the whole argument for the broker existing on top of the directory.
    static func observe(record: Record, probes: Probes,
                        ownerState: (Holder) -> OwnerState) -> (DirectoryState, Evidence) {
        let directory = probes.readDirectory(record.directory)
        var holderForEvidence = record.holder
        if case .held(let file) = directory,
           holderForEvidence == nil || holderForEvidence?.pid != file.owner {
            holderForEvidence = Holder(
                leaseID: adoptedLeaseID(for: file), owner: Owner(label: file.holder),
                pid: file.owner, processStart: file.started, acquiredAt: file.started ?? Date(),
                renewedAt: file.renewed ?? Date(), workPIDs: file.work, doneFlagPath: file.done,
                tree: file.tree, log: file.log, note: file.note, budget: nil,
                provenance: .directory, phase: file.phase,
                phaseSince: file.phaseSince ?? file.renewed ?? Date(),
                lastCompilingAt: file.lastCompiling
                    ?? (file.phase == .compiling ? file.renewed : nil),
                heartbeatPath: file.heartbeat, token: file.token,
                ownerStarted: file.ownerStarted)
        }
        var evidence = Evidence(compilers: probes.compilers(), pressure: probes.pressure())
        // The front of the line, and only the front: probing thirty-two entries would run
        // thirty-two subprocesses inside one decision, and it is the head that blocks the machine.
        if let head = record.queue.first {
            evidence.headWaiterProcess = probes.processStart(head.pid)
        }
        if let holder = holderForEvidence {
            evidence.owner = ownerState(holder)
            evidence.doneFlag = holder.doneFlagPath
                .map { probes.fileExists($0) ? DoneFlag.present : .absent } ?? .absent
            evidence.beat = probes.beat(holder.heartbeatPath
                                        ?? beatPath(inside: record.directory))
        }
        return (directory, evidence)
    }

    /// Perform the filesystem work a decision implies, and say what actually happened.
    ///
    /// **A failed effect returns the record unchanged.** `mkdir` losing the race is the whole
    /// exclusion doing its job: somebody took the directory between the reading and the write,
    /// so the caller is told `lease_changed` and polls again rather than being handed a grant
    /// the filesystem did not agree to.
    static func perform(_ decision: Decision, on previous: Record, probes: Probes) -> Applied {
        var applied = performEffect(decision, on: previous, probes: probes)
        // **The one thing a failed effect does not take back.** Everything else in the decision is
        // a consequence of the effect and is rightly discarded with it; the caller having asked is
        // not — it happened before the effect was attempted and no `mkdir` losing a race can undo
        // it. Without this, the two refusals whose own text says *ask again* were the two that
        // stopped the asking from counting, so a waiter on a contended machine could be declared
        // silent by the very contention it was waiting out.
        if let ask = decision.ask { applied.record = asked(applied.record, ask) }
        return applied
    }

    private static func performEffect(_ decision: Decision, on previous: Record,
                                      probes: Probes) -> Applied {
        switch decision.effect {
        case .none:
            return Applied(record: decision.record, outcome: decision.outcome)
        case .createDirectory(let file):
            if let problem = probes.createDirectory(previous.directory, file) {
                return Applied(record: previous, outcome: .refused(Refusal(
                    status: 409, code: "lease_changed",
                    message: "The lock directory changed while this request was being decided: "
                        + problem + " Ask again.")))
            }
            return Applied(record: decision.record, outcome: decision.outcome)
        case .takeOverDirectory(let expecting, let granting):
            // **(B) again, immediately before the directory goes.**
            //
            // The decision above was made against a reading taken before the rest of the turn's
            // probes — `pressure` is three subprocesses on its own, and the head waiter's start
            // and the anonymous-memory scan are one each, all bounded at five seconds — so the
            // compiler count it rests on can be many seconds old
            // by the time anything is removed — and the identity compare inside `removeDirectory`
            // cannot see a holder that came back to life, only one whose pid changed. `test.sh`
            // re-runs its whole admission immediately before its rename and the broker should do
            // at least as much. It is the physical half that is re-read because it is the half
            // that is about the machine rather than about the record: a compiler that started in
            // the interval is memory being spent now.
            switch probes.compilers() {
            case .present(let pids):
                return Applied(record: previous, outcome: .refused(Refusal(
                    status: 409, code: "takeover_failed",
                    message: "A compiler started while this takeover was being decided: pid(s) "
                        + pids.map { "\($0)" }.joined(separator: ", ")
                        + ". Nothing here will end them; ask again once they are done.")))
            case .unknown(let why):
                return Applied(record: previous, outcome: .refused(Refusal(
                    status: 409, code: "takeover_failed",
                    message: "Whether a compiler is running could not be read at the moment of "
                        + "the takeover (" + why + "), and unknown blocks. Ask again.")))
            case .none:
                break
            }
            if let problem = probes.removeDirectory(previous.directory, expecting) {
                return Applied(record: previous, outcome: .refused(Refusal(
                    status: 409, code: "takeover_failed",
                    message: "The lock could not be taken over: " + problem)))
            }
            if let problem = probes.createDirectory(previous.directory, granting) {
                return Applied(record: previous, outcome: .refused(Refusal(
                    status: 409, code: "lease_changed",
                    message: "The lock was taken by somebody else mid-takeover: " + problem)))
            }
            return Applied(record: decision.record, outcome: decision.outcome)
        case .removeDirectory(let expecting):
            if let problem = probes.removeDirectory(previous.directory, expecting) {
                return Applied(record: previous, outcome: .refused(Refusal(
                    status: 409, code: "release_failed",
                    message: "The lock was not removed: " + problem
                        + " The next reading will reconcile against whatever is there now.")))
            }
            return Applied(record: decision.record, outcome: decision.outcome)
        case .refreshHolderFile(let file):
            // A failed refresh is not a failed renewal. The record is the proof of life; the
            // file is a courtesy for a broker that is not running yet, and a lease must never be
            // lost because a courtesy could not be written.
            _ = probes.refreshHolderFile(previous.directory, file)
            return Applied(record: decision.record, outcome: decision.outcome)
        }
    }

    /// A bounded subprocess, with the locale pinned.
    ///
    /// `LC_ALL=C` is not decoration. `ps -o lstart=` renders through `LC_TIME`, and this Mac
    /// runs `zh_TW.UTF-8`. A probe that inherited the person's locale would write one string and
    /// compare against another, read every live holder as gone, and hand the lock to a second
    /// compiler. See ``identity(recordedStart:observed:)``.
    static func shell(_ path: String, _ arguments: [String], timeout: TimeInterval = 5)
        -> (out: String, status: Int32?, timedOut: Bool) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: path)
        process.arguments = arguments
        process.environment = ProcessInfo.processInfo.environment
            .merging(["LC_ALL": "C", "LANG": "C"]) { _, forced in forced }
        let output = Pipe()
        process.standardOutput = output
        process.standardError = Pipe()
        do {
            try process.run()
        } catch {
            return ("", nil, false)
        }
        let collected = NSMutableData()
        let readers = DispatchGroup()
        readers.enter()
        DispatchQueue.global(qos: .userInitiated).async {
            collected.append(output.fileHandleForReading.readDataToEndOfFile())
            readers.leave()
        }
        var timedOut = false
        if readers.wait(timeout: .now() + timeout) == .timedOut {
            timedOut = true
            process.terminate()
            _ = readers.wait(timeout: .now() + 1)
        }
        process.waitUntilExit()
        let text = String(data: collected as Data, encoding: .utf8) ?? ""
        return (text, timedOut ? nil : process.terminationStatus, timedOut)
    }
}
