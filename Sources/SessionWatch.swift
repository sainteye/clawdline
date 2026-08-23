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

    /// The menu a waiting session is showing, by session id — its options and which one the
    /// caret is on. Only the sessions that have one, which is a handful at most.
    ///
    /// Read out of the same screen capture as the state above, so it costs nothing extra. It is
    /// here rather than folded into `.waiting` because that case is compared against in twenty
    /// places and none of them care what the question was — see ``Targets/reading(of:)``.
    private(set) var menus: [String: SessionState.Menu] = [:]

    /// Sessions that were working a moment ago and are not now — the ones that just finished.
    ///
    /// Kept because it is the one thing a reading cannot say on its own: "idle" is the same word
    /// for a session that has been quiet all afternoon and one that stopped a second ago, and
    /// only the second is worth a celebration.
    private(set) var justFinished: [TargetSession] = []

    /// Which project each session is in, as its registry mark, by session id.
    ///
    /// Resolved here rather than by each consumer because the answer costs a process listing and
    /// an `lsof` on a cold cache, and there are now three places that want it: the ⌘K rows, the
    /// island's right ear, and the menu the island offers when its number stands for more than
    /// one session. It is also the most stable thing about a session — Claude Code is started in
    /// a directory and stays there — so a session that already has one is never asked again.
    private(set) var grids: [String: ProjectIcon.Grid] = [:]

    /// The background agents each session has running, by session id.
    ///
    /// **The one thing here that is not read off a screen.** A session that has sent three agents
    /// off to work looks exactly like one thinking hard about a single sentence: the terminal
    /// shows one spinner either way, because the agents are not drawn there. So this is read from
    /// the transcripts Claude Code keeps, and it is the only answer to "what is it actually doing"
    /// that the capture could never have given.
    private(set) var agents: [String: [Subagents.Agent]] = [:]

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

    /// Read now, because something said it was worth it.
    ///
    /// This is what a hook buys, and it is most of what a hook buys. Away from the panel the
    /// timer is on twenty seconds — long enough that a permission dialog can sit there through a
    /// whole train of thought — and the reason it is that long is that a reading costs a round
    /// trip to every terminal, which is a thing worth doing rarely when nothing has happened.
    /// A note says something has happened. The cadence underneath does not change; it just stops
    /// being the only thing that decides when to look.
    ///
    /// Not a separate path: it calls the same reading, and drops out under the same guard if one
    /// is already running.
    ///
    /// **Two readings, not one**, and the second is the one that does the work. Measured against a
    /// real session: Claude Code draws its live line about two seconds after you press Return, so
    /// a single reading taken the instant a turn begins looks at a screen that has nothing on it
    /// yet — and away from the panel the next scheduled one is up to twenty seconds later. The
    /// follow-up costs one more round trip per turn, and it removes the need for anything to
    /// *claim* that a session is working, which was the part that could be wrong.
    func nudge() {
        read()
        DispatchQueue.main.asyncAfter(deadline: .now() + settleDelay) { [weak self] in self?.read() }
    }

    /// How long to wait before looking again. Just past the two seconds it takes the live line to
    /// appear, and comfortably inside the twenty-second gap it exists to cover.
    private let settleDelay: TimeInterval = 2.5

    /// Read every session's screen, once. Overlapping readings are dropped rather than queued:
    /// on a loaded machine a slow round trip would otherwise pile up behind itself.
    private func read() {
        guard !reading else { return }
        reading = true
        // Taken here, on the main thread, because that is the only thread that writes them.
        let notes = Config.shared.hooks ? HookBridge.notes : [:]
        DispatchQueue.global(qos: isForeground ? .userInitiated : .utility).async { [weak self] in
            guard let self else { return }
            // Cheapest possible answer to "is any of this worth doing": one `ps`, already cached
            // for a couple of seconds, and nothing at all follows it on a machine with no Claude
            // Code running. tmux panes are in here too — they are ordinary processes on a tty.
            let anyAssistant = !ITerm.assistantPIDs().isEmpty
            let snap = anyAssistant ? Targets.snapshot() : Targets.Snapshot()
            let sessions = snap.assistantSessions.isEmpty ? snap.sessions : snap.assistantSessions
            // Named `screens` and not `reading`: there is a `reading` flag on `self` guarding
            // this whole function, and shadowing it here is a trap for the next edit.
            let screens = anyAssistant ? Targets.reading(of: sessions) : Targets.Reading()
            // What was read, with what Claude Code said about itself folded in. A no-op when
            // nothing is installed, which is the state every reading has to be right in.
            let states = HookBridge.merge(notes, into: screens.states, sessions: sessions)
            // Dropped for any session the merge moved off `waiting`: a note can settle that a
            // turn ended before the terminal has repainted, and a menu left behind from the
            // capture would be a set of buttons for a question nobody is asking any more.
            let menus = screens.menus.filter { states[$0.key] == .waiting }

            // Every background agent these sessions have going, which is a question the screen
            // cannot answer at all — a subagent leaves no mark on the terminal. Files only, so
            // it adds no round trip to the reading it rides along with.
            let agents = Subagents.reading(of: sessions)

            // Only the ones nothing is known about yet.
            var grids: [String: ProjectIcon.Grid] = [:]
            for session in sessions where self.grids[session.id] == nil {
                guard let cwd = Targets.workingDirectory(of: session),
                      let grid = ProjectIcon.grid(forCwd: cwd) else { continue }
                grids[session.id] = grid
            }

            DispatchQueue.main.async {
                self.reading = false
                self.grids.merge(grids) { _, new in new }
                self.menus = menus
                self.agents = agents
                self.apply(targets: sessions, states: states)
            }
        }
    }

    /// What the last round told the observers, so a menu opening or an agent finishing counts as
    /// a change. Without these the panel redrew only when a *state* moved, and a session that was
    /// already `working` could start and finish three agents without the pane noticing.
    private var lastMenus: [String: SessionState.Menu] = [:]
    private var lastAgents: [String: [Subagents.Agent]] = [:]

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
            || menus != lastMenus || agents != lastAgents
        lastMenus = menus
        lastAgents = agents
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

    /// The project mark for a session, if its project has one.
    func grid(of id: String) -> ProjectIcon.Grid? { grids[id] }

    /// The menu a session is showing, if it is showing one.
    func menu(of id: String) -> SessionState.Menu? { menus[id] }

    /// The background agents a session has running right now.
    func agents(of id: String) -> [Subagents.Agent] { agents[id] ?? [] }

    /// The live line a session last showed, if it is showing one.
    func liveLine(of id: String) -> String? {
        if case .working(let line) = states[id] { return line }
        return nil
    }
}
