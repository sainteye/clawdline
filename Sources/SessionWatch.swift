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

    /// The background commands each session left running, by session id.
    ///
    /// The other half of the sentence ``agents`` starts, and the half that costs somebody more:
    /// an agent runs while its session is busy anyway, and a shell outlives the turn that started
    /// it. The terminal says so once, where the turn ended, and then says nothing — see
    /// ``Shells``.
    private(set) var shells: [String: [Shells.Shell]] = [:]

    /// Called on the main thread after every reading. Keyed so a consumer that registers twice
    /// replaces itself rather than being called twice.
    var observers: [String: () -> Void] = [:]

    /// True while the panel is on screen and something in it is showing this. The only thing it
    /// changes is how often the terminal is asked.
    var isForeground = false {
        didSet { if isForeground != oldValue { start() } }
    }

    /// What the registry said last time, and which file was found to belong to which session.
    /// Kept because the directory watcher needs something to compare against — see
    /// ``registryDidChange()`` — and because it is where a session's own id comes from.
    private(set) var registry = SessionRegistry.Reading()

    private var timer: Timer?
    private var reading = false
    private var watchingRegistry = false

    private var interval: TimeInterval { isForeground ? 1.2 : 20 }

    func start() {
        timer?.invalidate()
        let t = Timer(timeInterval: interval, repeats: true) { [weak self] _ in self?.read() }
        RunLoop.main.add(t, forMode: .common)
        timer = t
        // Once, not on every cadence change: `start()` is called again whenever the panel opens
        // or closes, and re-opening the file descriptor each time would leak one per press.
        if !watchingRegistry {
            watchingRegistry = true
            SessionRegistry.watch { [weak self] in self?.registryDidChange() }
        }
        read()
    }

    /// A session rewrote its registry file. Look now, if it changed anything.
    ///
    /// **The guard is the whole of this.** Every Claude Code session on the machine writes into
    /// that one directory at every turn boundary it has, and a reading costs a round trip to
    /// every terminal — so "a file moved" is not on its own worth paying for. What is worth
    /// paying for is a status that is not the status this app is currently showing, which is the
    /// same rate a hook fires at and buys the same thing: a question noticed in the time it takes
    /// to write the file rather than at the next twenty-second tick.
    ///
    /// Only the sessions already known, because those are the ones a reading would be about; a
    /// session that has just appeared is nobody's question yet and the timer will find it.
    private func registryDidChange() {
        dispatchPrecondition(condition: .onQueue(.main))
        guard Config.shared.sessionRegistry, !registry.entries.isEmpty else { return }
        let fresh = SessionRegistry.entries(pids: Array(registry.entries.keys))
        guard SessionRegistry.statuses(fresh) != SessionRegistry.statuses(registry.entries)
        else { return }
        nudge()
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
        let useRegistry = Config.shared.sessionRegistry
        DispatchQueue.global(qos: isForeground ? .userInitiated : .utility).async { [weak self] in
            guard let self else { return }
            // Cheapest possible answer to "is any of this worth doing": one `ps`, already cached
            // for a couple of seconds, and nothing at all follows it on a machine with no Claude
            // Code running. tmux panes are in here too — they are ordinary processes on a tty.
            let anyAssistant = !ITerm.assistantPIDs().isEmpty
            let snap = anyAssistant ? Targets.snapshot() : Targets.Snapshot()
            let sessions = snap.assistantSessions.isEmpty ? snap.sessions : snap.assistantSessions
            // The note must reach the parser before the screen is classified. AskUserQuestion's
            // flush-left caret is intentionally ambiguous without this protocol fact. The note
            // only opens that parsing gate; the screen still decides whether a menu exists.
            let hookWaiting = Set(sessions.compactMap { session -> String? in
                let bare = session.tty.replacingOccurrences(of: "/dev/", with: "")
                return notes[bare]?.opensMenuGate == true ? session.id : nil
            })
            // What Claude Code says about itself, which is a different kind of fact from either
            // of the two above: not a screen, and not a thing that happened, but each session's
            // own current answer. Empty when the switch is off, when the Claude Code on this
            // machine is too old to write the files, and for every Codex session — and empty is
            // exactly today's behaviour, because everything below it is a no-op on an empty one.
            let registry = useRegistry && anyAssistant
                ? Targets.registry(of: sessions) : SessionRegistry.Reading()
            // A registry `waiting` opens the same parsing gate a hook note opens, and it opens it
            // on better grounds: it is written when something is genuinely blocked, where the
            // permission notes fire for requests auto mode approves without drawing anything.
            // This is the half of "a question is open" the registry cannot answer on its own —
            // it says a person is being asked, the screen says what the options are.
            let gate = hookWaiting.union(SessionRegistry.waiting(in: registry, sessions: sessions))
            // Named `screens` and not `reading`: there is a `reading` flag on `self` guarding
            // this whole function, and shadowing it here is a trap for the next edit.
            let screens = anyAssistant
                ? Targets.reading(of: sessions, hookWaiting: gate) : Targets.Reading()
            // What was read, with what Claude Code said about itself folded in. A no-op when
            // nothing is installed, which is the state every reading has to be right in.
            //
            // The registry goes last, and where the two disagree it wins: a note is a report that
            // a moment passed and has to be reasoned about afterwards, while a status is the
            // session's answer to the question being asked, rewritten the moment it stops being
            // true. Neither can move a session off a menu found on the screen.
            let heard = HookBridge.merge(notes, into: screens.states, sessions: sessions)
            let states = SessionRegistry.merge(registry, into: heard, sessions: sessions)
            // Dropped for any session the merge moved off `waiting`: a note can settle that a
            // turn ended before the terminal has repainted, and a menu left behind from the
            // capture would be a set of buttons for a question nobody is asking any more.
            var menus = screens.menus.filter { states[$0.key] == .waiting }

            // Structured hook data carries the original labels. Prefer it over the terminal
            // drawing, where a narrow pane clips precisely the words a phone needs to offer as
            // buttons. The screen remains the fallback for permission notes and AskUserQuestion,
            // whose opening hook is currently omitted by Claude Code.
            for session in sessions where states[session.id] == .waiting {
                let bare = session.tty.replacingOccurrences(of: "/dev/", with: "")
                if let menu = notes[bare]?.menu {
                    menus[session.id] = menu
                }
            }

            // **The words under each option are only complete in the transcript.** Claude Code
            // fits its dialog to the window and squeezes the explanations to whatever height is
            // left, so a capture carries the first line of a paragraph with the middle cut out of
            // it — and that is what a phone was being asked to choose on. The call is on disk in
            // full while the picker is still open (see ``Transcript/openQuestion(of:)``), so the
            // rows are refilled from it here.
            //
            // **Positional, and only as far as the transcript reaches.** The dialog draws rows the
            // question does not contain — `Type something.`, `Chat about this` — and those keep
            // the words the screen gave them. Everything else about the menu stays the screen's:
            // which row the caret is on, the numbers, whether there is a Submit under it. The
            // transcript knows what was asked; only the terminal knows where the caret is.
            for session in sessions where states[session.id] == .waiting {
                guard var menu = menus[session.id], !menu.options.isEmpty,
                      let asked = Transcript.openQuestion(of: session),
                      asked.options.count >= 2 else { continue }
                for index in 0..<min(menu.options.count, asked.options.count) {
                    menu.options[index].label = asked.options[index].label
                    let note = asked.options[index].note
                    menu.options[index].detail = note.isEmpty ? nil : note
                }
                if !asked.text.isEmpty { menu.question = asked.text }
                menus[session.id] = menu
            }

            // Every background agent these sessions have going, which is a question the screen
            // cannot answer at all — a subagent leaves no mark on the terminal. Files only, so
            // it adds no round trip to the reading it rides along with.
            let agents = Subagents.reading(of: sessions)

            // And every command they left running behind them, which the screen answers once
            // and then forgets.
            let shells = Shells.reading(of: sessions)

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
                self.shells = shells
                self.registry = registry
                self.apply(targets: sessions, states: states)
            }
        }
    }

    /// What the last round told the observers, so a menu opening or an agent finishing counts as
    /// a change. Without these the panel redrew only when a *state* moved, and a session that was
    /// already `working` could start and finish three agents without the pane noticing.
    private var lastMenus: [String: SessionState.Menu] = [:]
    private var lastAgents: [String: [Subagents.Agent]] = [:]
    private var lastShells: [String: [Shells.Shell]] = [:]

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
            || menus != lastMenus || agents != lastAgents || shells != lastShells
        lastMenus = menus
        lastAgents = agents
        lastShells = shells
        self.targets = targets
        self.states = states
        self.justFinished = finished
        // Off by default and a no-op for Claude Code. Kept on the reading path because this is
        // where a brand-new Codex rollout first becomes a concrete session rather than a guess.
        CodexNaming.shared.consider(targets)
        guard changed || !finished.isEmpty else { return }
        for observe in observers.values { observe() }
    }

    /// A Codex thread title arrived without the terminal list or screen state changing. Treat it
    /// as a presentation update so every surface can redraw without another terminal round trip.
    func labelsDidChange() {
        dispatchPrecondition(condition: .onQueue(.main))
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

    /// The background commands a session left running right now.
    func shells(of id: String) -> [Shells.Shell] { shells[id] ?? [] }

    /// The live line a session last showed, if it is showing one.
    func liveLine(of id: String) -> String? {
        if case .working(let line) = states[id] { return line }
        return nil
    }
}
