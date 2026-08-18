import AppKit
import Foundation

/// One reading of what every session is doing, for everything that wants to know.
///
/// The bar only ever knew anything while it was on screen: the panel went away and the app went
/// blind until you summoned it again. That is fine for a prompt box and useless for anything that
/// is supposed to tell you *that* you should look — the menu bar mark and the island under the
/// notch both need an answer at a moment when, by definition, nobody is looking at the panel.
///
/// So the reading moved out here, and everybody shares it. One set of terminal round trips feeds
/// the session list, the strip above the transcript, the menu bar and the island; the alternative
/// was four pollers asking the same question of the same terminal.
///
/// **The cadence is the whole cost story.** While the panel is up this runs at the same 1.2s the
/// pane already used. While it is not, it drops to once every twenty seconds — a reading costs
/// about seven hundred milliseconds with ten sessions open, so that is around three per cent of
/// one core, and it buys knowing that something has been waiting for you for the last minute. It
/// also gets out early when there is no `claude` process at all, which is one `ps` and nothing
/// else on a machine that is not running any.
final class SessionWatch {

    static let shared = SessionWatch()
    private init() {}

    /// Every session that could be a target, newest reading.
    private(set) var targets: [TargetSession] = []
    /// What each of them is doing, by session id.
    private(set) var states: [String: SessionState] = [:]

    /// Sessions that were working a moment ago and are not now — the ones that just finished.
    ///
    /// Kept because it is the one thing a reading cannot say on its own: "idle" is the same word
    /// for a session that has been quiet all afternoon and one that stopped a second ago, and
    /// only the second is worth a celebration.
    private(set) var justFinished: [TargetSession] = []

    /// Called on the main thread after every reading. Keyed so a consumer that registers twice
    /// replaces itself rather than being called twice.
    var observers: [String: () -> Void] = [:]

    /// True while the panel is on screen and something in it is showing this. The only thing it
    /// changes is how often the terminal is asked.
    var isForeground = false {
        didSet { if isForeground != oldValue { start() } }
    }

    private var timer: Timer?
    private var reading = false

    private var interval: TimeInterval { isForeground ? 1.2 : 20 }

    func start() {
        timer?.invalidate()
        let t = Timer(timeInterval: interval, repeats: true) { [weak self] _ in self?.read() }
        RunLoop.main.add(t, forMode: .common)
        timer = t
        read()
    }

    func stop() {
        timer?.invalidate()
        timer = nil
    }

    /// Read every session's screen, once. Overlapping readings are dropped rather than queued:
    /// on a loaded machine a slow round trip would otherwise pile up behind itself.
    private func read() {
        guard !reading else { return }
        reading = true
        DispatchQueue.global(qos: isForeground ? .userInitiated : .utility).async { [weak self] in
            guard let self else { return }
            // Cheapest possible answer to "is any of this worth doing": one `ps`, already cached
            // for a couple of seconds, and nothing at all follows it on a machine with no Claude
            // Code running. tmux panes are in here too — they are ordinary processes on a tty.
            let anyClaude = !ITerm.claudePIDs().isEmpty
            let snap = anyClaude ? Targets.snapshot() : Targets.Snapshot()
            let sessions = snap.claudeSessions.isEmpty ? snap.sessions : snap.claudeSessions
            let states = anyClaude ? Targets.states(of: sessions) : [:]

            DispatchQueue.main.async {
                self.reading = false
                self.apply(targets: sessions, states: states)
            }
        }
    }

    private func apply(targets: [TargetSession], states: [String: SessionState]) {
        // Working → not working, and still there. A session that has gone away has not finished
        // anything; it has been closed, and closing a tab is not an achievement worth dancing at.
        var finished: [TargetSession] = []
        for target in targets {
            let was = self.states[target.id]
            let now = states[target.id]
            if case .working = was, case .working = now { continue }
            if case .working = was, now != nil, now != .unknown { finished.append(target) }
        }

        let changed = targets.map(\.id) != self.targets.map(\.id) || states != self.states
        self.targets = targets
        self.states = states
        self.justFinished = finished
        guard changed || !finished.isEmpty else { return }
        for observe in observers.values { observe() }
    }

    // MARK: - Questions the readings answer

    /// Sessions with a question on screen that nobody has answered.
    var waiting: [TargetSession] { targets.filter { states[$0.id] == .waiting } }

    /// Sessions with something running.
    var working: [TargetSession] {
        targets.filter { if case .working = states[$0.id] { return true }; return false }
    }

    /// The live line a session last showed, if it is showing one.
    func liveLine(of id: String) -> String? {
        if case .working(let line) = states[id] { return line }
        return nil
    }
}
