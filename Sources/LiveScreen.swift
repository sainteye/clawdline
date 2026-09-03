import Foundation
import Darwin

/// A live screen for a phone: tmux says the pane moved, `capture-pane` says what it now shows.
///
/// **Shape C.** Three shapes were measured end to end before any of this was written
/// (2026-09-03, tmux 3.6a, Claude Code 2.1.259, M4 Pro):
///
/// | | latency | idle | what it costs to maintain |
/// | --- | --- | --- | --- |
/// | A  sample `capture-pane` faster | half the interval + 4.15 ms | zero, but it asks anyway | nothing new |
/// | B  `pipe-pane` + a VT emulator | 0.1 ms | one 432 KB `cat` per pane | 349 lines that must follow Claude Code's redraws |
/// | C  `pipe-pane` as the signal, `capture-pane` as the content | **4.3 ms** | one `cat`, **zero captures** | a FIFO's lifetime and one window |
///
/// B's emulator reproduced three real streams at 100.00% cell-and-colour fidelity with zero
/// unimplemented sequences, so it was not the hard half that killed it. The hard half is that a
/// reader joining mid-stream is wrong for 13–14 of 25 lines and, at two of five join points,
/// *never converges*: Claude Code repaints only the lines that changed, so a line drawn before
/// you arrived is blank for you forever. Whatever reaches a phone therefore has to be a screen
/// already computed on this Mac — and once that is true, B and C emit the same thing and C does
/// not need the emulator.
///
/// **Two premises this replaces, both measured false.**
///
/// - **There is no scrollback.** Every live Claude Code pane reported `alternate_on=1` and
///   `history_size=0`, so `capture-pane -S -200` returns the visible 25 lines and nothing else.
///   Nothing here promises history, and ``LiveScreen/lines`` is a ceiling rather than an offer:
///   an ordinary shell pane has history and an assistant pane has none, and the payload says how
///   many lines actually came back rather than how many were asked for.
/// - **The pane is redrawn about nine times a second.** Median write interval 107 ms, 599 B/s,
///   and no quiet second in 139 seconds of work. That is what makes ``ScreenCoalescer``
///   load-bearing: without a window this shape is a 9 Hz sampler, which is the one way it becomes
///   worse than A.
///
/// **The danger, and it is the reason for most of the code below.** `pipe-pane` is machine state
/// that outlives this process. If the app is force-quit the pipe stays on a pane somebody is
/// still using. Three things answer that, in order of how much they can be trusted:
///
/// 1. **The target is a FIFO, so the leak has a floor.** Measured: close the reading end and the
///    `sh -c 'cat > …'` dies on the pane's *first* write, and tmux clears `#{pane_pipe}` on the
///    *second*. So a pane that is being used cleans itself up within two redraws, and a pane that
///    is not costs a sleeping 432 KB process. A file target (`cat >> …`) has neither property: it
///    would keep running, and keep writing, forever.
/// 2. **The FIFO on disk is the ownership record**, so recovery needs no second file to keep in
///    step. ``LiveScreens/reclaim()`` reads the directory, and a `%N.fifo` left there names a pane
///    this app piped and did not take back.
/// 3. **Exactly one owner turns it on**, ``LiveScreens``, and it is the only thing that turns it
///    off. There is no route that attaches a pipe and no route that detaches one; demand is
///    *observed* — see ``LiveScreens/read(_:now:)``.
///
/// And `GET /v1/screens` publishes the whole of it, including the panes tmux says are piped that
/// this app cannot account for, because `#{pane_pipe}` is a boolean and tmux will not say whose
/// pipe it is.
enum LiveScreen {

    /// How long the app waits for a burst to settle before it captures again.
    ///
    /// **100–250 ms is the measured band and this is the middle of it.** The signal fires about
    /// 9.3 times a second while a session is working; one capture is 4.15 ms. Without a window
    /// that is 39 ms of tmux per second per watched pane, and 21% of the captures return bytes
    /// identical to the last one. With 150 ms the rate is bounded by 1/window — at most 6.7
    /// captures a second, 27.8 ms, 2.8% of one core — while the *first* change after a quiet
    /// moment is still immediate, because the window measures from the last capture and not from
    /// the signal. That asymmetry is the point: a phone sees a keystroke echo in 4.3 ms and a
    /// scrolling build in six frames a second.
    static let coalescingWindow: TimeInterval = 0.15

    /// The same idea where there is no signal to coalesce. iTerm2 has no `pipe-pane`, so its
    /// screens can only be sampled, and one round of `ITerm.capture` is 0.16 s measured
    /// (`Resources/iterm.js:372-377`) — about forty times a tmux capture. One second between
    /// captures holds that at roughly a sixth of a core no matter how fast a client asks.
    static let onDemandFloor: TimeInterval = 1.0

    /// How long a reading keeps the signal attached without being asked again.
    ///
    /// **Demand is observed rather than declared, so this is what "somebody is still looking"
    /// means.** There is no subscribe route to forget to call and no unsubscribe route to lose in
    /// a tunnel: a watcher that stops asking stops being a watcher, and the pipe comes off. Thirty
    /// seconds because the page refreshes its lease every fifteen — twice per lease, so one lost
    /// request does not drop the screen.
    static let leaseSeconds: TimeInterval = 30

    /// How often expired leases are swept up. Only a bound on how long a pipe outlives its
    /// watcher; the lease itself is compared against the clock, never against this.
    static let sweepSeconds: TimeInterval = 5

    /// The ceiling on how much screen is asked for. On an assistant pane this is inert — the
    /// alternate screen has no history to give — and on an ordinary shell pane it is the same two
    /// hundred lines ``Targets/screenWithHistory(of:lines:)`` already offers the panel.
    static let lines = 200

    /// What a client compares to decide whether it already has this screen.
    ///
    /// FNV-1a over UTF-8, which is enough for "did these bytes change" and is deliberately not a
    /// promise about anything else. The 21% of samples that come back byte-identical are dropped
    /// against this before anybody is told a screen changed.
    static func revision(of text: String) -> String {
        var hash: UInt64 = 0xcbf2_9ce4_8422_2325
        for byte in text.utf8 {
            hash ^= UInt64(byte)
            hash = hash &* 0x0000_0100_0000_01b3
        }
        return String(hash, radix: 16)
    }

    /// The revision of a screen that could not be read. A constant rather than a hash, so that a
    /// pane which stays unreadable stops producing events after the first one — the client is told
    /// once that the screen went away, not once every window.
    static let unreadableRevision = "unreadable"
}

/// How a watcher finds out that a screen changed, which is a fact about the backend and not about
/// the watcher.
///
/// **Naming it is the whole point.** A view that shows a 4.3 ms live screen on tmux and a
/// second-old sample on iTerm2, with the same chrome and no word about which, repeats a defect
/// this repository already had. So the channel is in every payload and the interface prints it.
enum LiveScreenChannel: String {
    /// tmux: `pipe-pane` says the pane moved and the app captures because of it.
    case signalled
    /// iTerm2: nothing says the screen moved, so it is read when somebody asks and no faster than
    /// ``LiveScreen/onDemandFloor``.
    case onDemand = "on-demand"
}

/// The coalescing window, as arithmetic rather than as a timer.
///
/// Separated from everything that owns a clock or a queue so the thing that decides how often this
/// app touches somebody's terminal can be proved without either. The rule is one line: **the
/// window is measured from the last capture, not from the last signal**, so a burst is bounded at
/// one capture per window while an isolated change is captured immediately.
struct ScreenCoalescer {
    enum Decision: Equatable {
        /// Nothing has been captured inside the window; capture now.
        case now
        /// Capture at this instant, which is one window after the last capture.
        case at(TimeInterval)
        /// A capture is already scheduled for this pane and will pick this signal up.
        case waiting
    }

    let window: TimeInterval
    private var lastCapture: [String: TimeInterval] = [:]
    private var scheduled: Set<String> = []

    init(window: TimeInterval) { self.window = window }

    /// What to do about a change signal for `id` arriving at `now`.
    mutating func signal(_ id: String, at now: TimeInterval) -> Decision {
        if scheduled.contains(id) { return .waiting }
        guard let last = lastCapture[id] else { return .now }
        let due = last + window
        if now >= due { return .now }
        scheduled.insert(id)
        return .at(due)
    }

    /// Record that a capture happened, which is what the next window is measured from.
    mutating func captured(_ id: String, at now: TimeInterval) {
        lastCapture[id] = now
        scheduled.remove(id)
    }

    /// Whether a capture is already on its way for this pane.
    func isScheduled(_ id: String) -> Bool { scheduled.contains(id) }

    mutating func forget(_ id: String) {
        lastCapture.removeValue(forKey: id)
        scheduled.remove(id)
    }
}

/// `pipe-pane` into a FIFO, where the readability of the FIFO is the entire message.
///
/// The bytes are read and thrown away. What is wanted is the wake-up: measured on this Mac,
/// **0.014 ms median** from a byte reaching the pane to the FIFO becoming readable, over forty
/// samples, against 2.78 ms for the same wait when the write is a `tmux send-keys` subprocess —
/// the difference being fork/exec and not the signal.
///
/// **Why a FIFO and not a file.** Both were measured and both are fast enough. The FIFO wins on
/// what happens when this process is not there any more: nothing lands on disk, there is nothing
/// to poll, and the pipe target dies of its own accord once the reading end is gone. A file target
/// keeps running and keeps growing — 51.8 MB per pane per working day at the measured 599 B/s —
/// and would need this app to be alive to stop it.
///
/// **The write end this class holds open is not decoration.** A `DispatchSourceRead` on a FIFO
/// with no writer sees end-of-file, and end-of-file on a FIFO is *readable*, so the source would
/// spin. Holding an `O_WRONLY` descriptor of our own means the FIFO never reaches that state while
/// this app is running. It does not weaken the safety net above: a writer is not a reader, so
/// `cat`'s SIGPIPE still arrives the moment the read descriptor closes.
final class PaneSignal: @unchecked Sendable {

    private final class Entry {
        let paneID: String
        let fifo: URL
        let read: Int32
        let write: Int32
        let source: DispatchSourceRead

        init(paneID: String, fifo: URL, read: Int32, write: Int32, source: DispatchSourceRead) {
            self.paneID = paneID
            self.fifo = fifo
            self.read = read
            self.write = write
            self.source = source
        }
    }

    private let queue: DispatchQueue
    private let directory: URL
    private var entries: [String: Entry] = [:]

    /// Where a wake-up goes. Set after construction because the object that wants it also owns
    /// this one; unset, a signal is read and discarded, which is the correct behaviour for a pane
    /// whose watcher has gone.
    var onMoved: (@Sendable (String) -> Void)?

    /// `queue` is the owner's serial queue: the signal fires on it, so whoever holds the coalescer
    /// and the leases does not have to cross a boundary to answer a wake-up.
    init(directory: URL, queue: DispatchQueue) {
        self.directory = directory
        self.queue = queue
    }

    /// The FIFO for a pane. `%31` becomes `31.fifo`: the `%` is dropped because it is punctuation
    /// in both a shell and a `display-message` format, and the id is reconstructible from the
    /// name, which is what makes the directory an ownership record.
    func fifoURL(for paneID: String) -> URL? {
        guard Tmux.isPaneID(paneID) else { return nil }
        return directory.appendingPathComponent("\(paneID.dropFirst()).fifo")
    }

    /// The pane id a leftover FIFO names, or nil for a file this class did not make.
    static func paneID(ofFIFONamed name: String) -> String? {
        guard name.hasSuffix(".fifo") else { return nil }
        let digits = name.dropLast(".fifo".count)
        guard !digits.isEmpty, digits.allSatisfy({ ("0"..."9").contains($0) }) else { return nil }
        return "%" + digits
    }

    /// The command tmux runs on the far end of the pipe.
    ///
    /// Single-quoted because the app's own directories contain spaces, and refused outright if the
    /// path could close the quote — this string becomes a shell line inside somebody's terminal
    /// multiplexer, which is not a place to be generous.
    static func pipeCommand(writingTo path: String) -> String? {
        guard !path.isEmpty, !path.contains("'") else { return nil }
        return "cat > '\(path)'"
    }

    var attachedPanes: [String] { entries.keys.sorted() }

    func isAttached(_ paneID: String) -> Bool { entries[paneID] != nil }

    /// Attach the signal to a pane, or report why not. Idempotent: a pane already attached is
    /// already the answer.
    @discardableResult
    func attach(_ paneID: String) -> Bool {
        if entries[paneID] != nil { return true }
        guard let fifo = fifoURL(for: paneID),
              let command = Self.pipeCommand(writingTo: fifo.path) else { return false }
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true,
                                                 attributes: [.posixPermissions: 0o700])
        // A leftover from a previous life is replaced rather than reused: its permissions and its
        // readers are not knowable from here.
        try? FileManager.default.removeItem(at: fifo)
        guard mkfifo(fifo.path, 0o600) == 0 else { return false }
        // The reading end first, and non-blocking, because opening a FIFO for reading blocks until
        // somebody opens it for writing — and the writer is the `cat` that does not exist yet.
        let read = open(fifo.path, O_RDONLY | O_NONBLOCK)
        guard read >= 0 else {
            try? FileManager.default.removeItem(at: fifo)
            return false
        }
        let write = open(fifo.path, O_WRONLY | O_NONBLOCK)
        guard write >= 0 else {
            close(read)
            try? FileManager.default.removeItem(at: fifo)
            return false
        }
        let source = DispatchSource.makeReadSource(fileDescriptor: read, queue: queue)
        let entry = Entry(paneID: paneID, fifo: fifo, read: read, write: write, source: source)
        source.setEventHandler { [weak self, weak entry] in
            guard let self, let entry, self.entries[entry.paneID] === entry else { return }
            var buffer = [UInt8](repeating: 0, count: 1 << 14)
            // Read and discard. The bytes are a perfect record of what the pane drew and this
            // deliberately does not keep them: reconstructing a screen from them is shape B, and
            // the measurement that a mid-stream reader never converges is why it is not this.
            while Darwin.read(entry.read, &buffer, buffer.count) > 0 {}
            self.onMoved?(entry.paneID)
        }
        source.setCancelHandler { close(read) }
        entries[paneID] = entry
        source.resume()
        guard Tmux.pipe(paneID, into: command) else {
            detach(paneID)
            return false
        }
        return true
    }

    /// Take the pipe off, close the descriptors, remove the FIFO. In that order, because the
    /// directory listing is the ownership record and a FIFO removed before the pipe is gone would
    /// be a pane nothing can find again.
    func detach(_ paneID: String) {
        guard let entry = entries.removeValue(forKey: paneID) else { return }
        Tmux.unpipe(paneID)
        entry.source.cancel()
        close(entry.write)
        try? FileManager.default.removeItem(at: entry.fifo)
    }

    func detachAll() {
        for id in entries.keys.sorted() { detach(id) }
    }

    /// Panes this app piped in a previous life, as named by the FIFOs it left behind.
    func abandonedPanes() -> [String] {
        let names = (try? FileManager.default.contentsOfDirectory(atPath: directory.path)) ?? []
        return names.compactMap(Self.paneID(ofFIFONamed:)).filter { entries[$0] == nil }.sorted()
    }

    /// Remove the record of an abandoned pane. The caller decides whether to unpipe first; this
    /// only forgets, so a directory scan that could not reach tmux does not erase the evidence.
    func forgetAbandoned(_ paneID: String) {
        guard entries[paneID] == nil, let fifo = fifoURL(for: paneID) else { return }
        try? FileManager.default.removeItem(at: fifo)
    }
}

/// Demand-driven live screens: who is watching what, what their last screen was, and the one
/// place that decides this app may touch a terminal because of it.
///
/// **Nobody watching is the whole design.** No lease means no `pipe-pane`, which means no signal,
/// which means no capture — an idle session measured 0 bytes in 45 seconds, so a watched session
/// that is sitting at its prompt costs nothing at all either. The lease is refreshed by reading,
/// not by a subscribe route, so there is no state a disconnected phone can leave behind.
///
/// This is emphatically **not** ``SessionWatch``'s foreground signal. That one is 1.2 s while this
/// Mac is frontmost and 20 s otherwise, and a person holding a phone in another room is not the
/// Mac being frontmost — it is the wrong subject entirely. Nothing here changes that cadence.
///
/// **Two queues and a published snapshot, for the reason the rest of this server has them.**
/// Everything that can start a subprocess — attaching a pipe, taking one off, a capture — happens
/// on ``state`` or behind it, and an HTTP request never waits on that queue: it reads a
/// lock-protected snapshot and posts its demand behind the answer. So a wedged tmux costs a stale
/// screen and never a stalled connection, which is the same trade ``SessionWatch/publishedInventory()``
/// already makes for the session list.
final class LiveScreens: @unchecked Sendable {

    /// One session's answer, which is the same shape whether it came from a signal or from being
    /// asked. `text` is nil until the first capture completes.
    struct Reading {
        let sessionID: String
        let backend: Backend
        let channel: LiveScreenChannel
        let text: String?
        let revision: String
        let at: Date?
        let readable: Bool
        let watchingUntil: Date
        let captures: Int
        let signals: Int
    }

    /// What a reader may see without touching the state queue.
    private struct Publication {
        var backend: Backend
        var text: String?
        var revision: String
        var at: Date?
        var readable: Bool
        var captures: Int
        var signals: Int
        var attached: Bool
        var until: Date
    }

    private struct Watch {
        var session: TargetSession
        var until: Date
        var text: String?
        var revision: String
        var at: Date?
        var readable: Bool
        var capturing: Bool
        var captures: Int
        var signals: Int
    }

    /// Leases, the cache, the coalescer and the pipes. The only writer of the published snapshot.
    ///
    /// Assigned in `init` rather than here because `PaneSignal` is handed this queue, and a stored
    /// property cannot be read through `self` until every other one has been set.
    private let state: DispatchQueue
    /// Where a capture actually happens. Serial, because two captures of the same pane in flight
    /// answer the same question twice.
    private let captureQueue = DispatchQueue(label: "com.tsunamiworks.clawdline.live-screens.capture")

    private let signal: PaneSignal
    private let changed: @Sendable (String, String) -> Void
    private var watches: [String: Watch] = [:]
    private var coalescer = ScreenCoalescer(window: LiveScreen.coalescingWindow)
    private var sweeper: DispatchSourceTimer?

    private let publicationLock = NSLock()
    private var published: [String: Publication] = [:]

    /// What actually reads a screen. Exactly one reader, and it is the one whose doc comment names
    /// this use: *"for the one thing that is showing a person a terminal rather than deciding
    /// something from it"*. A second reader here would be a second answer to the same question.
    private let capture: @Sendable (TargetSession) -> String?

    init(directory: URL,
         capture: @escaping @Sendable (TargetSession) -> String? = {
             Targets.screenWithHistory(of: $0, lines: LiveScreen.lines)
         },
         changed: @escaping @Sendable (String, String) -> Void) {
        let state = DispatchQueue(label: "com.tsunamiworks.clawdline.live-screens")
        self.state = state
        self.capture = capture
        self.changed = changed
        self.signal = PaneSignal(directory: directory, queue: state)
        // Installed after `self` exists, because it calls back into this object. `PaneSignal`
        // fires it on `state`, so a wake-up crosses no queue boundary on its way to the coalescer.
        self.signal.onMoved = { [weak self] paneID in self?.paneMoved(paneID) }
    }

    /// Which channel this backend can offer, and therefore what the interface may claim.
    static func channel(for backend: Backend) -> LiveScreenChannel {
        switch backend {
        case .tmux:  return .signalled
        case .iterm: return .onDemand
        }
    }

    // MARK: - Demand

    /// The current screen for a session, and the demand that reading it declares.
    ///
    /// **Reading is the subscription.** There is no route that attaches a pipe and none that takes
    /// one off: a read renews a lease, and a lease nobody renews expires and takes the pipe with
    /// it. That is the only shape which also covers the phone that went into a tunnel, and it
    /// means no client can leave machine state behind by crashing.
    ///
    /// **This never waits on a terminal.** The answer comes out of the published snapshot; the
    /// pipe and the first capture are arranged behind it. A watch with nothing captured yet
    /// answers `text: nil`, and the client learns the screen arrived the same way it learns about
    /// every later one — from the `screen` event.
    @discardableResult
    func read(_ session: TargetSession, now: Date = Date()) -> Reading {
        let until = now.addingTimeInterval(LiveScreen.leaseSeconds)
        state.async { [weak self] in self?.demand(session, until: until, now: now) }
        return snapshot(of: session, until: until)
    }

    private func snapshot(of session: TargetSession, until: Date) -> Reading {
        publicationLock.lock()
        let held = published[session.id]
        publicationLock.unlock()
        return Reading(sessionID: session.id, backend: session.backend,
                       channel: LiveScreens.channel(for: session.backend),
                       text: held?.text, revision: held?.revision ?? LiveScreen.unreadableRevision,
                       at: held?.at, readable: held?.readable ?? false, watchingUntil: until,
                       captures: held?.captures ?? 0, signals: held?.signals ?? 0)
    }

    /// What this app has attached, what tmux says about it, and the difference between the two.
    ///
    /// **`#{pane_pipe}` is a boolean, so tmux cannot say whose pipe it is.** A pane that is piped
    /// and not on this app's list is therefore reported as `unattributed` rather than as a leak —
    /// it may be somebody's own `pipe-pane`, and this app does not take other people's pipes off.
    func inventory() -> [String: Any] {
        publicationLock.lock()
        let rows = published.keys.sorted().compactMap { id -> [String: Any]? in
            guard let entry = published[id] else { return nil }
            let now = Date()
            return [
                "id": id,
                "backend": entry.backend.rawValue,
                "channel": LiveScreens.channel(for: entry.backend).rawValue,
                "watching": entry.until > now,
                "expiresIn": max(0, Int(entry.until.timeIntervalSince(now).rounded())),
                "signalled": entry.attached,
                "captures": entry.captures,
                "signals": entry.signals,
                "readable": entry.readable,
            ]
        }
        let ours = Set(published.filter(\.value.attached).keys)
        publicationLock.unlock()
        let piped = Tmux.pipedPanes()
        return [
            "screens": rows,
            "attached": ours.sorted(),
            "piped": piped.filter(\.value).keys.sorted(),
            "unattributed": piped.filter { $0.value && !ours.contains($0.key) }.keys.sorted(),
            "windowMs": Int(LiveScreen.coalescingWindow * 1000),
            "leaseSeconds": Int(LiveScreen.leaseSeconds),
        ]
    }

    /// Panes piped by a previous run of this app, taken back.
    ///
    /// Called once at start. The FIFO left in this app's own directory is the record — no second
    /// file to fall out of step — and tmux is asked before anything is undone, so a pane that has
    /// already cleaned itself up (the measured case: the target dies on the pane's first write
    /// after the reader is gone, and tmux clears the flag on the second) costs one forgotten file
    /// rather than a `pipe-pane` aimed at somebody else's pane id.
    @discardableResult
    func reclaim() -> [String] {
        let abandoned = state.sync { signal.abandonedPanes() }
        guard !abandoned.isEmpty else { return [] }
        let piped = Tmux.pipedPanes()
        var taken: [String] = []
        for pane in abandoned {
            if piped[pane] == true {
                Tmux.unpipe(pane)
                taken.append(pane)
            }
            state.sync { signal.forgetAbandoned(pane) }
        }
        return taken
    }

    /// Everything off, now. The app closing, the server stopping, a test finishing. Synchronous on
    /// purpose: this is the one path where the pipes must be gone before the caller carries on.
    func stop() {
        state.sync {
            sweeper?.cancel()
            sweeper = nil
            watches.removeAll()
            signal.detachAll()
        }
        publicationLock.lock()
        published.removeAll()
        publicationLock.unlock()
    }

    /// Drop leases that have run out and take their pipes with them. Exposed so a test can move
    /// the clock instead of waiting for one.
    func sweep(now: Date = Date()) {
        state.sync { sweepLocked(now: now) }
    }

    /// Settle everything this object has been asked to do. Tests only: the demand a read posts is
    /// deliberately behind the answer, and a test that asserts on the pipe has to wait for it.
    func settleForTesting() {
        state.sync {}
        captureQueue.sync {}
        state.sync {}
    }

    // MARK: - Inside the state queue

    private func demand(_ session: TargetSession, until: Date, now: Date) {
        var watch = watches[session.id] ?? Watch(
            session: session, until: until, text: nil, revision: LiveScreen.unreadableRevision,
            at: nil, readable: false, capturing: false, captures: 0, signals: 0)
        watch.session = session
        watch.until = until
        watches[session.id] = watch
        if session.backend == .tmux { signal.attach(session.id) }
        publish(session.id)
        startSweeping()
        if shouldCaptureOnRead(session.id, now: now) { beginCapture(session.id, now: now) }
    }

    private func sweepLocked(now: Date) {
        for (id, watch) in watches where watch.until <= now {
            watches.removeValue(forKey: id)
            coalescer.forget(id)
            if watch.session.backend == .tmux { signal.detach(id) }
            publicationLock.lock()
            published.removeValue(forKey: id)
            publicationLock.unlock()
        }
        if watches.isEmpty {
            sweeper?.cancel()
            sweeper = nil
        }
    }

    private func startSweeping() {
        guard sweeper == nil else { return }
        let timer = DispatchSource.makeTimerSource(queue: state)
        timer.schedule(deadline: .now() + LiveScreen.sweepSeconds, repeating: LiveScreen.sweepSeconds)
        timer.setEventHandler { [weak self] in self?.sweepLocked(now: Date()) }
        timer.resume()
        sweeper = timer
    }

    /// Whether reading should itself start a capture. On tmux this is only ever the first read:
    /// after that the signal is what causes a capture, and asking again while nothing has moved is
    /// the 100% waste the sampling shape pays and this one does not. On iTerm2 there is no signal,
    /// so a read is the only thing that can cause one, bounded by the on-demand floor.
    private func shouldCaptureOnRead(_ id: String, now: Date) -> Bool {
        guard let watch = watches[id], !watch.capturing else { return false }
        switch watch.session.backend {
        case .tmux:
            return watch.at == nil && !coalescer.isScheduled(id)
        case .iterm:
            guard let at = watch.at else { return true }
            return now.timeIntervalSince(at) >= LiveScreen.onDemandFloor
        }
    }

    /// A pane wrote something. Runs on `state`, because `PaneSignal` fires there.
    private func paneMoved(_ paneID: String) {
        guard var watch = watches[paneID] else { return }
        watch.signals += 1
        watches[paneID] = watch
        publish(paneID)
        let now = Date()
        switch coalescer.signal(paneID, at: now.timeIntervalSince1970) {
        case .now:
            beginCapture(paneID, now: now)
        case .at(let due):
            let delay = max(0, due - now.timeIntervalSince1970)
            state.asyncAfter(deadline: .now() + delay) { [weak self] in self?.flush(paneID) }
        case .waiting:
            break
        }
    }

    /// The trailing edge of the window: whatever the pane has done since the last capture, in one
    /// capture rather than in one per redraw.
    private func flush(_ paneID: String) {
        guard watches[paneID] != nil, coalescer.isScheduled(paneID) else { return }
        beginCapture(paneID, now: Date())
    }

    private func beginCapture(_ id: String, now: Date) {
        guard var watch = watches[id], !watch.capturing else { return }
        watch.capturing = true
        watch.captures += 1
        watches[id] = watch
        coalescer.captured(id, at: now.timeIntervalSince1970)
        publish(id)
        let session = watch.session
        let read = capture
        captureQueue.async { [weak self] in
            let text = read(session)
            self?.state.async { self?.finish(id, text: text) }
        }
    }

    /// **This is where the 21% goes.** Ten samples a second of a working session produced bytes
    /// identical to the previous sample 23 times in 113, and a client told about those would have
    /// fetched the same screen again for nothing. A capture whose text and readability both match
    /// what is already held bumps no revision and sends no event.
    private func finish(_ id: String, text: String?) {
        guard var watch = watches[id] else { return }
        watch.capturing = false
        let readable = text != nil
        let unchanged = readable == watch.readable && text == watch.text
        if unchanged {
            watches[id] = watch
            publish(id)
            return
        }
        watch.readable = readable
        watch.text = text
        watch.at = Date()
        watch.revision = text.map(LiveScreen.revision(of:)) ?? LiveScreen.unreadableRevision
        watches[id] = watch
        publish(id)
        changed(id, watch.revision)
    }

    /// Hand the state queue's answer to whoever is reading over HTTP. Called after every change,
    /// because a snapshot that is only refreshed on some of them is a cache with a hole in it.
    private func publish(_ id: String) {
        guard let watch = watches[id] else { return }
        let entry = Publication(backend: watch.session.backend, text: watch.text,
                                revision: watch.revision, at: watch.at, readable: watch.readable,
                                captures: watch.captures, signals: watch.signals,
                                attached: signal.isAttached(id), until: watch.until)
        publicationLock.lock()
        published[id] = entry
        publicationLock.unlock()
    }
}


extension LiveScreens.Reading {
    /// The wire shape, which says what it is looking at before it says what it saw.
    ///
    /// `backend` and `channel` are first because a screen with no backend named is the defect this
    /// exists to avoid, and `lines` is what came back rather than what was asked for — an
    /// alternate-screen program has no history to give and the payload must not imply otherwise.
    var payload: [String: Any] {
        var out: [String: Any] = [
            "id": sessionID,
            "backend": backend.rawValue,
            "channel": channel.rawValue,
            "revision": revision,
            "readable": readable,
            "pending": text == nil && readable == false && at == nil,
            "watchingUntil": Int(watchingUntil.timeIntervalSince1970),
            "captures": captures,
            "signals": signals,
        ]
        if let text {
            out["text"] = text
            // The trailing newline a capture ends with is a terminator, not a row. Counting it
            // would report 26 lines for a 25-line screen, and the number is the one place this
            // payload says how much history there actually was.
            let body = text.hasSuffix("\n") ? String(text.dropLast()) : text
            out["lines"] = body.isEmpty
                ? 0 : body.split(separator: "\n", omittingEmptySubsequences: false).count
        }
        if let at { out["at"] = Int(at.timeIntervalSince1970) }
        if channel == .onDemand {
            // Said out loud rather than left for the client to discover: on this backend nothing
            // will tell it the screen moved, so the only way to see a change is to ask again, and
            // this is the fastest it may.
            out["askAgainAfterMs"] = Int(LiveScreen.onDemandFloor * 1000)
        }
        return out
    }
}
