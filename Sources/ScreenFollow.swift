import Foundation

/// Samples one session's screen closely, for as long as somebody is reading it.
///
/// **What goes wrong without this.** ``ScreenTail`` reconstructs what a session has said from
/// overlapping screen captures, and the overlap is the whole mechanism: two consecutive captures
/// have to share a run of lines or the second one cannot be placed against the first. The
/// captures it is fed come from ``SessionWatch``, whose cadence is 1.2 s while the Mac's own
/// panel is open and **20 s when it is not** — and a phone reading from another room does not
/// open the Mac's panel. Twenty seconds of a streaming answer is several screens, consecutive
/// captures share nothing, and the reconstruction correctly refuses to guess across the break.
/// The reader gets the tail of an answer with its beginning missing, which is the complaint this
/// exists to answer.
///
/// **Why not simply sample everything faster.** Because readings cost what the Mac is doing
/// rather than what was asked for: ten sessions is about 700 ms of Apple events per round trip,
/// and `ReadingFreshness` measured the same routes running four to ten times slower when iTerm2's
/// main thread was busy. A fleet-wide rate rise pays that for nine screens nobody is looking at
/// in order to fix the one somebody is. So the rate rise is bound to attention: **one extra
/// capture per second, for the session whose transcript is actually being read**, and nothing at
/// all when nobody is reading.
///
/// Attention is taken from the reading itself — a transcript request is somebody looking at that
/// session — so there is no new client contract, nothing to declare, and a reader who closes the
/// page stops being followed a few seconds later without having to say so.
final class ScreenFollow: @unchecked Sendable {
    static let shared = ScreenFollow()

    /// How long a transcript read keeps its session followed. Long enough to survive a slow
    /// answer between two polls, short enough that a closed page stops costing anything within
    /// one breath.
    static let attention: TimeInterval = 25

    /// One extra capture per second. Fast enough that a streaming answer's screens overlap —
    /// Claude Code writes prose over tens of seconds, not milliseconds — and slow enough to stay
    /// a rounding error beside the inventory that is already being taken.
    static let cadence: TimeInterval = 1

    /// How many working sessions are swept alongside the one being read.
    ///
    /// **Why they are swept at all.** A reader arrives after the answer was written, not during
    /// it, and what they can be shown is only what was captured while it was being written. A
    /// session that is producing text right now is the only kind whose screen can hold words the
    /// file does not, so those are the screens worth having when somebody opens one of them next.
    ///
    /// **Why only while somebody is reading something.** With no reader anywhere this is pure
    /// cost — nobody will ever ask for it — so a Mac with the phone closed pays nothing at all.
    static let sweepLimit = 4

    /// Working sessions are swept every other beat. The reader's own session is worth a capture a
    /// second; a session nobody has opened yet is worth half that, and halving it is what keeps
    /// the whole sweep near one screen's worth of Apple events per second.
    static let sweepEvery = 2

    private let lock = NSLock()
    private var readers: [String: Date] = [:]
    private var timer: DispatchSourceTimer?
    private var beat = 0
    private let queue = DispatchQueue(label: "com.tsunamiworks.clawdline.screenfollow",
                                      qos: .utility)

    /// Test seam: what to capture with, so a test never asks iTerm2 anything.
    static var captureForTesting: ((TargetSession) -> String?)?
    /// Test seam: which sessions exist, for the same reason.
    static var targetsForTesting: (() -> [TargetSession])?
    /// Test seam: which sessions the inventory says are producing text.
    static var workingForTesting: (() -> [String])?

    private init() {}

    /// Somebody is reading this session's transcript right now.
    ///
    /// **The first read takes its capture inline.** Opening a session is exactly the moment the
    /// reader wants what is on screen, and waiting for the first beat of a timer means the answer
    /// they are handed is the one from before they asked. It is paid once, only for a session
    /// nothing has been captured for yet; every beat after this is the timer's.
    func noteReader(of session: TargetSession) {
        lock.lock()
        readers[session.id] = Date()
        let idle = timer == nil
        lock.unlock()
        if !ScreenTail.hasDocument(session.id) { capture([session]) }
        if idle { start() }
    }

    /// The id-only form, for callers that have nothing to capture with.
    func noteReader(of sessionID: String) {
        lock.lock()
        readers[sessionID] = Date()
        let idle = timer == nil
        lock.unlock()
        if idle { start() }
    }

    /// Who is still being read, oldest attention dropped.
    func followed(now: Date = Date()) -> [String] {
        lock.lock()
        defer { lock.unlock() }
        readers = readers.filter { now.timeIntervalSince($0.value) < Self.attention }
        return Array(readers.keys)
    }

    private func start() {
        lock.lock()
        defer { lock.unlock() }
        guard timer == nil else { return }
        let source = DispatchSource.makeTimerSource(queue: queue)
        source.schedule(deadline: .now() + Self.cadence, repeating: Self.cadence, leeway: .milliseconds(200))
        source.setEventHandler { [weak self] in self?.tick() }
        timer = source
        source.resume()
    }

    private func stop() {
        lock.lock()
        let source = timer
        timer = nil
        lock.unlock()
        source?.cancel()
    }

    /// One beat: capture each followed session and hand it to ``ScreenTail``.
    ///
    /// The timer cancels itself when nobody is reading, so an idle Mac pays nothing at all rather
    /// than paying a wakeup a second to discover it has no work.
    private func tick() {
        let ids = Set(followed())
        guard !ids.isEmpty else { stop(); return }
        lock.lock()
        beat &+= 1
        let sweeping = beat % Self.sweepEvery == 0
        lock.unlock()
        var wanted = ids
        if sweeping {
            // Deterministic order, so a Mac with six busy tabs sweeps the same four every time
            // rather than a different four each beat — half-sampled screens align worse than
            // unsampled ones.
            for id in Self.working().prefix(Self.sweepLimit) { wanted.insert(id) }
        }
        let targets = (Self.targetsForTesting?() ?? SessionWatch.shared.publishedInventory().targets)
            .filter { wanted.contains($0.id) }
        guard !targets.isEmpty else { return }
        capture(targets)
    }

    /// The sessions producing text right now, in a stable order.
    private static func working() -> [String] {
        if let fake = workingForTesting { return fake().sorted() }
        return SessionWatch.shared.publishedInventory().states
            .filter { if case .working = $0.value { return true } else { return false } }
            .keys.sorted()
    }

    /// **One round trip, not one per session.** Asking iTerm2 for each screen separately is a
    /// process and an Apple event bridge each; `ITerm.tails` takes the whole set in one, which is
    /// the same bargain `Targets.reading` already strikes and the reason a sweep is affordable.
    private func capture(_ targets: [TargetSession]) {
        if let fake = Self.captureForTesting {
            for target in targets { ScreenTail.observe(target.id, screen: fake(target)) }
            return
        }
        let iterm = targets.filter { $0.backend == .iterm }
        if !iterm.isEmpty {
            let tails = ITerm.tails(ids: iterm.map { $0.id })
            for target in iterm { ScreenTail.observe(target.id, screen: tails[target.id]) }
        }
        for target in targets where target.backend == .tmux {
            ScreenTail.observe(target.id, screen: Tmux.capture(target.id, scrollback: 0))
        }
    }

    /// Test seam: forget every reader and stop the timer.
    func forgetForTesting() {
        lock.lock()
        readers = [:]
        beat = 0
        let source = timer
        timer = nil
        lock.unlock()
        source?.cancel()
    }

    /// Test seam: run one beat without waiting for the timer.
    func tickForTesting() { tick() }
}
