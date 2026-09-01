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

    private let lock = NSLock()
    private var readers: [String: Date] = [:]
    private var timer: DispatchSourceTimer?
    private let queue = DispatchQueue(label: "com.tsunamiworks.clawdline.screenfollow",
                                      qos: .utility)

    /// Test seam: what to capture with, so a test never asks iTerm2 anything.
    static var captureForTesting: ((TargetSession) -> String?)?
    /// Test seam: which sessions exist, for the same reason.
    static var targetsForTesting: (() -> [TargetSession])?

    private init() {}

    /// Somebody is reading this session's transcript right now.
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
        let targets = (Self.targetsForTesting?() ?? SessionWatch.shared.publishedInventory().targets)
            .filter { ids.contains($0.id) }
        guard !targets.isEmpty else { return }
        for target in targets {
            let screen = Self.captureForTesting?(target) ?? Targets.capture(target)
            ScreenTail.observe(target.id, screen: screen)
        }
    }

    /// Test seam: forget every reader and stop the timer.
    func forgetForTesting() {
        lock.lock()
        readers = [:]
        let source = timer
        timer = nil
        lock.unlock()
        source?.cancel()
    }

    /// Test seam: run one beat without waiting for the timer.
    func tickForTesting() { tick() }
}
