import AppKit

/// Which queue the caller is standing on — which is **not** the same question as which thread.
///
/// They agree right up until `dispatchMain()` runs. After it the main thread is parked and the
/// main *queue* is drained by whichever worker libdispatch hands it to, so inside a block that is
/// already on the main queue `Thread.isMainThread` is false. Code that decides whether to hop on
/// that answer takes a `dispatch_sync` onto the queue that already owns its thread, and
/// libdispatch does not deadlock on that — it traps. A trap in a background reading is a process
/// that disappears with no failing test, no log line and nothing on screen; that is what
/// ``Orchestrator/records()`` did (fixed in b6d1939a) and it cost a day to find.
///
/// It lives in this file because the first two crossings to ask it were here and in
/// ``Transcript``, and both were inside one change's write set. Every synchronous main-queue
/// crossing in the app now asks it through ``hop(from:alreadyOnMain:_:)``, and
/// ``Orchestrator/isOnMainQueue`` is this key rather than one of its own.
enum MainQueue {

    private static let key: DispatchSpecificKey<Bool> = {
        let key = DispatchSpecificKey<Bool>()
        DispatchQueue.main.setSpecific(key: key, value: true)
        return key
    }()

    /// True when the caller is running on the main queue, whichever thread is draining it.
    static var isCurrent: Bool { DispatchQueue.getSpecific(key: key) == true }

    /// Answer `work` on the main queue, hopping only when the caller is not already there.
    ///
    /// **`alreadyOnMain` is the caller's own answer, and it is passed in rather than asked here
    /// because the caller's answer is the thing that goes wrong.** Every site this replaced asked
    /// `Thread.isMainThread`, which reads correctly for as long as nothing runs on the main queue
    /// from a worker — and then stops.
    ///
    /// So the hop is checked against queue identity once more here, one step before it would be
    /// taken. A caller whose predicate was wrong runs its work inline, which is what it wanted
    /// anyway, and is named in ``reentrantHopsForTesting``. This is the net rather than the
    /// decision: what it buys is that a wrong predicate becomes an observable mistake — a line in
    /// the log and a red check — instead of a process that vanishes mid-reading.
    static func hop<T>(from site: String, alreadyOnMain: Bool, _ work: () -> T) -> T {
        noteHopIfRecording(site)
        if alreadyOnMain { return work() }
        guard isCurrent else { return DispatchQueue.main.sync(execute: work) }
        reentryLock.lock()
        reentrantHops.append(site)
        let first = loggedReentry.insert(site).inserted
        reentryLock.unlock()
        // Once per site. A wrong predicate is wrong on every reading, and the panel takes one
        // every 1.2 seconds — a line each would bury the log it is trying to be found in.
        if first {
            Log.write("main-queue hop from \(site) was already on the main queue; ran it there")
        }
        return work()
    }

    private static let reentryLock = NSLock()
    private static var reentrantHops: [String] = []
    private static var loggedReentry: Set<String> = []

    /// Sites whose predicate sent them to ``hop(from:alreadyOnMain:_:)`` while they were already
    /// on the main queue, in the order it happened. Empty is the only correct reading.
    static var reentrantHopsForTesting: [String] {
        reentryLock.lock(); defer { reentryLock.unlock() }
        return reentrantHops
    }

    static func forgetReentrantHopsForTesting() {
        reentryLock.lock()
        reentrantHops = []
        loggedReentry = []
        reentryLock.unlock()
    }

    // MARK: - Which sites actually crossed

    /// Every site that reached ``hop(from:alreadyOnMain:_:)`` while recording was on, in order.
    ///
    /// **Why this exists.** A suite can only tell a real crossing from a helper wearing its name if
    /// the names it checks are *observed* rather than written down. The first version of the
    /// crossing fixture returned a hard-coded list after calling some production functions, so
    /// deleting a crossing outright — reading `SessionWatch.shared.targets` straight from a worker
    /// with no hop at all — left the suite at 5421 of 5421 green. This is that hole closed at the
    /// one place every crossing passes through.
    ///
    /// **What it costs the app.** One uncontended `NSLock` acquisition per crossing, and nothing
    /// else: `recordingHops` is false in every shipping build, so no array is touched and nothing
    /// is allocated. Crossings happen at HTTP-request and panel-reading rate, not in a loop. The
    /// flag is read under the lock rather than beside it because a `Bool` read racing an append is
    /// a data race whether or not it is benign in practice.
    ///
    /// Bounded on purpose: a test that forgets to stop recording would otherwise grow this without
    /// limit for the rest of the process. Past the ceiling the sites stop being appended and
    /// ``endRecordingHopsForTesting()`` says so, which is a fact a check can fail on rather than a
    /// list that quietly stopped being true.
    private static var recordingHops = false
    private static var recordedHops: [String] = []
    private static var recordedHopsOverflowed = false
    private static let recordedHopCeiling = 512

    private static func noteHopIfRecording(_ site: String) {
        reentryLock.lock()
        defer { reentryLock.unlock() }
        guard recordingHops else { return }
        if recordedHops.count < recordedHopCeiling { recordedHops.append(site) }
        else { recordedHopsOverflowed = true }
    }

    /// Start over. Calling this twice without an end in between is not an error; it resets.
    static func beginRecordingHopsForTesting() {
        reentryLock.lock()
        recordedHops = []
        recordedHopsOverflowed = false
        recordingHops = true
        reentryLock.unlock()
    }

    static func endRecordingHopsForTesting() -> (sites: [String], overflowed: Bool) {
        reentryLock.lock()
        defer { reentryLock.unlock() }
        recordingHops = false
        let seen = recordedHops
        let over = recordedHopsOverflowed
        recordedHops = []
        recordedHopsOverflowed = false
        return (seen, over)
    }
}

/// Settings and history, kept in ~/.config/clawdline/.
/// Everything has a default: a missing, corrupt or half-written config must still launch.
final class Config {
    static let shared = Config()

    var yFraction: CGFloat = 0.30      // where the panel top sits, as a fraction of screen height
    var width: CGFloat = 720
    var hotKey = "option+space"
    /// The hotkey only fires while one of these apps is frontmost. Comma-separated bundle
    /// ids; empty string means global. More than one matters now that tmux lets Claude Code
    /// run under any terminal — an iTerm2-only scope would leave those users without a key.
    /// Done in-app rather than handed to a hotkey utility: registering globally takes ⌥Space away
    /// from every other app, while registering per-frontmost leaves them exactly as they were.
    ///
    /// **This is the hotkey and nothing else.** It used to decide which terminal a new session
    /// opened in as well, which meant somebody who wanted the chord bound to iTerm2 could not
    /// ask for sessions in tmux, and somebody who changed it for the terminal lost the binding
    /// they had. That job is ``terminal``'s now.
    var scopeApp = "com.googlecode.iterm2"
    /// Which terminal a new session is opened in.
    ///
    /// Split out of ``scopeApp`` on 2026-09-02, because one value cannot answer two questions —
    /// see ``StartPoints/TerminalChoice``. Read at the moment a session is started rather than at
    /// launch, so changing it in Settings takes effect on the next start with no restart.
    ///
    /// A `config.json` written before this key existed has it filled in once from ``scopeApp``,
    /// by ``StartPoints/TerminalChoice/inheritedFromHotkeyScope(_:)``, so nothing moves under
    /// anybody; the next save writes the answer down and the scope is never consulted again.
    var terminal: StartPoints.TerminalChoice = .auto
    /// The `terminal` value a `config.json` held that this app could not read, kept from the
    /// last ``load()`` and `nil` when the key was absent or legal.
    ///
    /// **An unreadable value and an absent one get the same answer, and they are not the same
    /// event.** Both mean "this file never chose", so both migrate from ``scopeApp`` — but a
    /// hand-typed `"terminal": "ghostty"` is somebody asking for something, and the next
    /// ``save()`` writes the migrated answer over it with no trace of what was asked. Until now
    /// nothing could tell the two apart, so nothing could say so. This is written to the log
    /// once, at the moment it is noticed; it is never persisted and never decides anything.
    private(set) var discardedTerminal: String?
    /// "auto" follows the system, or a tag such as "en" / "zh-Hant"
    var language = "auto"
    /// Which mascot pack to draw. Files live in ~/.config/clawdline/mascots/<name>.json
    var mascot = "clawd"
    /// Where tmux lives. Apps do not inherit a login shell, so PATH almost never has it.
    /// Empty means "look in the usual places".
    /// How tall the output pane is, in points.
    var outputHeight: CGFloat = 340
    /// The font the ⌘J pane draws with. Match it to your terminal's, or the box-drawing
    /// characters a status line is made of come out at the wrong widths.
    /// What ⌘J shows: "auto" prefers the transcript and falls back to the terminal,
    /// "transcript" or "terminal" pin it.
    var outputMode = "auto"
    var outputFont = "Menlo"
    /// Point size of the ⌘J pane, adjustable live with ⌘+ / ⌘-.
    var outputSize: CGFloat = 11.5
    /// Newest message at the top instead of the bottom, toggled live with ⌘R.
    /// Only the transcript reads this way: a terminal capture is a picture of a grid, and
    /// flipping its lines would have a wrapped sentence reading upwards.
    var outputNewestFirst = false
    /// Which recogniser the microphone uses.
    ///
    /// "apple" is live and needs nothing installed. "whisper" transcribes when you stop and
    /// handles a sentence with two languages in it, at the cost of a binary and a model file —
    /// see docs/whisper.md. "auto" uses whisper when both are present.
    var voiceEngine = "auto"
    var whisperBinary = ""
    var whisperModel = ""
    /// What language to transcribe in: a BCP-47 tag like "zh-TW" or "en", or "auto".
    ///
    /// "auto" lets Whisper decide, which is what you want when you really do switch languages —
    /// and what you do not want in a quiet room, because a model asked to identify silence will
    /// pick something. Naming a language also fixes the script: Whisper writes Simplified for
    /// Chinese unless told otherwise.
    var voiceLanguage = "auto"
    /// How long a pause ends a sentence, in seconds. 0 turns it off.
    ///
    /// At a pause the words so far are fixed: Whisper reads that stretch and replaces it, and
    /// nothing after that point rewrites it. Without this, a two-minute dictation is one lump
    /// that gets re-transcribed at the end — slower, and everything you already read moves.
    var voiceSettleSeconds: Double = 1.8
    /// How long a silence ends the whole session, in seconds. 0 leaves the microphone on until
    /// it is pressed again.
    ///
    /// Longer than a settle, because these are different claims: a pause says "that sentence is
    /// finished", a long one says "I am finished". Being late costs an open microphone in a room
    /// where nobody is talking; being early costs the keystroke this exists to remove.
    var voiceStopSeconds: Double = 4.0
    /// Words a transcriber cannot be expected to know, put back afterwards.
    ///
    /// Names, product names, the odd piece of jargon — anything that comes back as something
    /// that merely sounds right ("cloud code"). Apple's recogniser is told to expect them;
    /// Whisper is not, because its only lever is a writing sample and a list of words in one
    /// costs all the punctuation. So they are repaired in the text instead, which is
    /// deterministic and cannot make the sentence worse.
    var voiceVocabulary: [String] = []
    /// Hand images over as images rather than as paths.
    ///
    /// Claude Code turns an image on the system pasteboard into `[Image #3]` when it receives a
    /// Ctrl-V, which is a keystroke rather than text — so the send is split around it and the
    /// pasteboard is borrowed and handed back. What you get is the picture in the message and a
    /// number you can point at, instead of forty characters of directory.
    ///
    /// Set false to go back to sending the path, which is never wrong, only plainer. It also
    /// falls back on its own for anything that is not a Claude Code session, because Ctrl-V in a
    /// shell means something else entirely.
    var sendImagesAsPaste = true
    /// Come back when the terminal does.
    ///
    /// Switching away from a panel you left open is "I need to see something for a moment", not
    /// "I am done with it" — that is what Esc is for. So what an app switch put away, coming
    /// back takes out again. Turn it off if you would rather every appearance be one you asked
    /// for by hand.
    var reopenOnReturn = true
    /// The character that lives in the notch, and whether it lives there.
    ///
    /// **This one is an experiment and is meant to be switchable off in one word.** It tells you
    /// nothing the menu bar mark does not; it is the same reading wearing a costume, and whether
    /// a small animal leaning out of your camera housing is a delight or an irritation is not a
    /// question anybody can answer on your behalf. `false` and it is not created at all — no
    /// window, no observer, no drawing.
    var notch = true
    /// Move the terminal's own tab to whatever the bar is pointing at.
    ///
    /// The bar names its target along the bottom edge and that has always been enough to send
    /// safely — but the target and the tab in front of you were free to be two different
    /// sessions, and the moment you closed the panel you were looking at the wrong one. With this
    /// on they are the same session by construction.
    ///
    /// **Selecting is not the same as activating**, and only the first one happens: iTerm2 is not
    /// brought forward, or every press of Tab would take the keyboard out of the box you are
    /// typing into. Off for anyone who keeps a terminal tab open to read while working elsewhere.
    var followTarget = true
    /// How solid the card is, from 0 (pure frosted glass) to 1 (opaque).
    ///
    /// The material samples whatever is behind the window, so a screen of green diff or a
    /// bright page tints the whole card and drags the text with it. This is a dark layer
    /// between the two: the blur still reads as glass, but the colour behind stops arriving.
    var cardOpacity: Double = 0.55
    /// How far the ⌘J backdrop goes, from 0 (none) to 1 (fully obscured).
    /// Below 1 the blur is partly transparent, so what is behind stays legible.
    var backdropStrength: Double = 0.5
    /// Believe what Claude Code's hooks say about a session, when they are installed.
    ///
    /// Off, and the notes are ignored while the hooks stay wired up — which is the setting to
    /// reach for if a reading ever looks wrong and you want to know whether this is why, without
    /// editing another program's settings file to find out. Nothing else changes: the screen is
    /// still the complete fallback when no matched lifecycle note states more. See
    /// Sources/HookBridge.swift.
    var hooks = true
    /// Believe what Claude Code's session registry says a session is doing.
    ///
    /// Each session writes a small file about itself under `~/.claude/sessions/` and keeps the
    /// status in it current, so unlike a hook this is there whether or not anybody installed
    /// anything — including for sessions that were already open. Off, and the files are ignored
    /// and every reading is the screen's alone, which is the setting to reach for if a state ever
    /// looks wrong and you want to know whether this is why. See Sources/SessionRegistry.swift.
    var sessionRegistry = true
    /// Answer questions over HTTP, on the loopback address, so that a browser or a script can ask
    /// what the panel asks.
    ///
    /// **Off, and that is not a shy default.** A listening socket is the difference between a
    /// program on your machine and a service on your machine, and reading a session hands over a
    /// repository name, a branch and a task title. Turning it on should be a thing somebody did
    /// on purpose. See docs/remote.md and Sources/RemoteServer.swift.
    var remote = false
    var remotePort = 7717
    /// How the outside gets in: "off", "quick" (a generated trycloudflare.com name, no account),
    /// or "named" (your own tunnel and your own hostname). See Sources/RemoteTunnel.swift.
    /// Let a paired device type into a session, and start new ones.
    ///
    /// **Separate from `remote`, and off, because it is a different feature at a different risk
    /// level.** Reading a session discloses a repository name and a task title. Writing to one is
    /// remote code execution — Claude Code runs `bash` — so this is not a finer setting on the
    /// same dial, it is the second dial. Turning it on grants `send` to every paired device;
    /// turning it off takes it back from all of them at once.
    /// A command to run whenever a session changes state — argv, not a shell line.
    ///
    /// The one extension point that pays for itself before anything else does. In Herdr's
    /// ecosystem, where 682 plugins were counted, the single event "this agent's state changed"
    /// accounts for 15% of every hook declared and appears in 44% of the plugins that hook
    /// anything at all: notifications, status lines, dashboards, watchdogs and chat bridges are
    /// all the same shape. So it is the first thing here to be opened up, and it is opened the
    /// way they opened theirs — environment variables and an executable, no SDK, no bindings, no
    /// opinion about what language you write it in.
    ///
    /// `["node", "~/bin/notify.mjs"]`. An array because nothing should be word-split: a path with
    /// a space in it is a path, not two arguments. See Sources/StateHook.swift.
    /// Buzz when a session says it has delivered what it was asked for.
    ///
    /// **The event is a receipt, not a screen reading, and that is the whole difference.** A root
    /// posts one authenticated claim at the end of the turn it delivered in — see
    /// `Orchestrator.reportSessionDelivery` — so what buzzes is a session saying *this is done and
    /// I am waiting for you to look*, which is a thing you can act on. What was here before was
    /// `push_on_finish`, and it named `working → idle` past two minutes: answering one question
    /// ended a turn, so did finishing a step, and the switch's own label claimed the work was
    /// over. Somebody who turned it off was turning off a lie rather than declining the news.
    ///
    /// On by default, for the reason the old one was: at this volume a notification arriving is
    /// itself useful confirmation the thing works, and a rule elaborate enough to suppress the
    /// redundant ones is a rule nobody can debug when it goes quiet.
    var pushOnDelivery = true
    /// Buzz when the last task of a fan-out ends.
    ///
    /// One notification for a whole subtree, with a count and how many failed — worth a phone
    /// because the tabs it ran in have closed by then and the count exists nowhere a person is
    /// looking. It is a separate switch from the one above rather than a finer setting on it:
    /// a delivery is one session saying it is your turn, a fan-out ending is a tree going quiet,
    /// and somebody who wants one of those does not necessarily want the other.
    ///
    /// It inherits `push_on_finish` on the way in. That key covered both events, so an answer of
    /// "do not buzz me when things end" was given about this one too and has to survive the split.
    var pushOnFanout = true
    /// Replace the generic fan-out wording with one bounded Haiku summary.
    ///
    /// Off by default because it spends assistant quota and puts authored work detail on the
    /// lock screen. The ordinary completion notification remains the fallback in every case.
    /// On the delivery push it spends nothing: the session already wrote a sentence about its own
    /// delivery, and that sentence is carried verbatim instead of a generated one.
    var smartNotifications = false
    /// Buzz when a deploy stops running.
    ///
    /// A better candidate than the one above and for the opposite reason: deploys are rare, and
    /// you are usually waiting on one rather than merely interested. Both outcomes are sent —
    /// a deploy that failed is the one you most want to hear about.
    var pushOnDeploy = false
    var onStateChange: [String] = []
    var remoteWrite = false
    var remoteTunnel = "off"
    var remoteTunnelName = ""
    var remoteHostname = ""
    var cloudflaredPath = ""
    var tmuxPath = ""
    /// Where Codex keeps its sessions, when it is not `~/.codex`.
    ///
    /// Codex honours `CODEX_HOME`, and this app cannot see it: launched from Finder it inherits
    /// no login shell, which is the same reason ``Tmux/binary`` cannot look on `PATH`. Blank
    /// means the environment if it has one and `~/.codex` otherwise; `~` is expanded.
    var codexHome = ""
    /// Give a new unnamed conversation a short user-facing name from its first request.
    ///
    /// Off until somebody chooses it because this is not local bookkeeping: it starts one small
    /// assistant turn, sends that request to a model and spends the selected assistant's usage.
    /// Codex conversations use it immediately; Claude conversations use it only after the first
    /// turn ends without Claude Code writing its own title. The helper turn is not persisted, so
    /// naming a session never creates another session to name.
    var codexAutoName = false
    /// Which installed assistant performs the one small naming turn.
    ///
    /// Codex remains the default so every config written before this choice existed keeps the
    /// exact behaviour its `codex_auto_name: true` selected. The older boolean stays on disk as
    /// the enable switch; this value only answers who does the enabled work.
    var automaticNamingAssistant: Assistant = .codex
    /// The single settings control has three states without minting an impossible combination of
    /// an off switch beside a still-selected provider. Turning it off remembers the provider so
    /// turning it back on does not quietly change whose quota it spends.
    var automaticNamingSelection: String {
        get { codexAutoName ? automaticNamingAssistant.rawValue : "off" }
        set {
            guard let assistant = Assistant(rawValue: newValue) else {
                codexAutoName = false
                return
            }
            automaticNamingAssistant = assistant
            codexAutoName = true
        }
    }
    /// The deliberately small model used for that one narrow turn. Kept configurable because
    /// model availability belongs to the account, not to this binary; the default is the current
    /// low-cost Codex model documented for clear, repeatable work.
    var codexAutoNameModel = "gpt-5.6-luna"
    /// An escape hatch for a Finder-launched app whose running Codex process cannot yield its
    /// executable path. Blank means use that process first, then the usual install locations.
    var codexPath = ""
    /// Let a root session ask this app to open child sessions and brief them. See
    /// Sources/Orchestrator.swift and docs/orchestrator.md.
    ///
    /// On by default, and that is defensible where `remote` being off is not: dispatching already
    /// requires a credential that only a local process can read — the 0600 token file — so this
    /// switch is a preference, not the boundary. Off refuses dispatch outright while leaving the
    /// task records readable.
    var orchestratorEnabled = true
    /// How many child sessions one root may have alive at once.
    ///
    /// Per dispatcher rather than per Mac: what the number bounds is how much work one
    /// conversation can have out at a time, and `orchestratorMaxDescendants` is the ceiling over
    /// every conversation on this Mac at once.
    ///
    /// There is no second number under it. The tree is one level deep as a structural fact —
    /// ``Orchestrator/depthFloor`` is a constant, not a reading of this file — so how many a
    /// *child* may open is not a question this type answers any more. See
    /// `orchestrator_max_grandchildren` in ``load()``.
    var orchestratorMaxChildren = 5
    /// How far a dispatched child may go before it stops and asks — the *most* a task may ask
    /// for, not what every task gets. `ask`, `auto`, `edits`, `full`; see ``Permission``.
    ///
    /// `full` by default, arrived at by trying the narrower settings and watching them fail.
    ///
    /// **Nobody is watching a child's tab.** A session that stops for approval there does not
    /// stop for a moment; it stops until the task times out, and afterwards it reads as work that
    /// silently did not happen. So the usual intuition inverts: "ask about everything" is not the
    /// careful setting here, it is the one where nothing gets done and nobody finds out why.
    ///
    /// The narrower stops were each tried against a real child and each stopped it somewhere. A
    /// dispatched session's whole job is running commands and writing files: `ask` stops on the
    /// first thing it does, which is reading its own briefing; `edits` gets it past writing but
    /// not past `cat`, `mkdir`, `curl` or `sleep`, which is most of what handing work on consists
    /// of. There is no flag that covers those and stops short of this one.
    ///
    /// What this is *not* is a widening of who may dispatch. That is still a `0600` file only a
    /// local process can read, and a child already has a shell — this changes how many buttons a
    /// person has to press for work they already authorised, not what the work can reach.
    ///
    /// Still a ceiling as well as a default: set it lower and a task asking for more is quietly
    /// given this instead, because the session doing the asking is not the one that lives with
    /// what happens next.
    var orchestratorPermission = "full"
    /// Every dispatched session on this Mac, whoever asked — four roots' worth of children,
    /// twenty by default, and not a setting of its own because it is not a choice anybody makes
    /// separately from the number it is made of. Several root sessions share this Mac, so it is
    /// deliberately more than one root's cap; it kept the value the two-level arithmetic
    /// (`children × (1 + grandchildren)`) produced, so removing the second level did not quietly
    /// tighten what the machine will hold.
    ///
    /// The per-dispatcher cap says what one session may spend. This says what the machine may
    /// spend, and it is the half that still holds when a caller lies about who it is: declaring
    /// somebody else's session id moves a task into another bucket, never past this line.
    /// The ceiling as the type, with the file's word for it read back through the closed list so
    /// a hand-edit that says something else lands on the default rather than on nothing.
    var orchestratorPermissionCeiling: Permission {
        Permission(rawValue: orchestratorPermission) ?? .full
    }
    var orchestratorMaxDescendants: Int { orchestratorMaxChildren * 4 }
    /// Type one line into the root session when a task it dispatched finishes, so the
    /// conversation that asked for the work is the one that hears it is done.
    var orchestratorNotifyRoot = true
    /// Let an agent send content to the user's subscribed devices through either orchestrator
    /// notification route. Separate from the automatic finish and deploy preferences: those
    /// describe app state, while this is prose an agent chose to send proactively.
    ///
    /// On by default to preserve the behavior from before this preference existed. A missing key
    /// in an older config therefore means on as well.
    var orchestratorAgentNotify = true
    /// What becomes of a child's terminal once it has reported: seconds to leave it open before
    /// the app closes it, `0` to close as soon as it is quiet, `-1` to leave it to the user.
    /// Only a child that reported — success or failure — is closed; one that timed out or never
    /// came up is left where it is, because what went wrong is on that screen.
    var orchestratorChildLinger = 180
    /// How long failed task-owned `work/` directories remain available for diagnosis, in minutes.
    /// Success always reclaims immediately; `0` does the same for every terminal outcome and
    /// `-1` leaves the directory to the ordinary 24-hour task-root sweep.
    var orchestratorWorkGraceMinutes = 60
    /// How long an isolated checkout's `.build/` remains available before it is reclaimed, in
    /// minutes. Shaped exactly like `orchestrator_work_grace_minutes`: success reclaims
    /// immediately, `0` does the same for every terminal outcome, `-1` leaves the build output
    /// until the whole checkout is disposed of.
    ///
    /// It is a separate setting because the two directories answer different questions. `work/`
    /// holds the failing build log somebody reads after a child dies; `.build/` holds object
    /// files nobody has ever read, and on this Mac it was 814 MB of them, kept alive by pending
    /// landings that needed only the source and the branch.
    var orchestratorBuildGraceMinutes = 60
    /// The used-percentage at which an assistant's quota reads as `low` rather than `ok`, both
    /// from `GET /v1/orchestrator/assistants` and at the dispatch gate — see
    /// `Sources/AssistantQuota.swift`.
    ///
    /// 85 by default, which is not picked to match anything about this feature: it is the exact
    /// threshold `claude-bestiary`'s `statusline.py` already turns a terminal red at
    /// (`c = "\x1b[31m" if pct >= 85 else …`), so the color somebody sees in a terminal and the
    /// word this API gives about the same account agree rather than disagreeing by a few points.
    var assistantQuotaLowThreshold: Double = 85
    /// Where the project status files are read from, and where the icon registry lives.
    ///
    /// Both default to what claude-bestiary writes, because that is what most people reading this
    /// already have — but the format is documented (docs/project-status.md) so that anything can
    /// produce them, and a producer that is not claude-bestiary should not have to impersonate it
    /// to be found. Blank means the default; `~` is expanded.
    var statusDir = ""
    var iconsFile = ""
    var lastTargetID: String?
    var history: [String] = []

    /// Session names kept by Clawdline: normally chosen by a person, with a lower-precedence
    /// generated row only when Claude Code supplied no title. Each row carries the identities
    /// appropriate to its source:
    /// Claude's hook session id survives a reopened terminal, the terminal id covers Codex,
    /// shells and Claude installations without hooks, and `startedAt` is what tells the second
    /// conversation in a reused tab apart from the first — see
    /// ``sessionTitle(sessionID:terminalID:conversationStart:currentCustomTitle:)``.
    struct SessionTitle: Equatable {
        let title: String
        let sessionID: String?
        let terminalID: String
        /// False for a name a person chose; true for Claude's model fallback. Keeping both in the
        /// same bounded, locked ledger gives them identical durability without putting generated
        /// prose in the human-title precedence rung.
        let automatic: Bool
        /// When the assistant process in that tab started, or nil for a tab with no assistant
        /// in it. Nil is a value here, not a missing one: a shell has no conversation to
        /// outlive, so "there was no process" has to match "there is no process" exactly.
        let startedAt: Date?
        /// The Claude transcript's own `customTitle` at the moment this name was chosen, or nil
        /// when the transcript had none yet. Compared against a fresh read of the same file on
        /// every later look: a difference means a person has since typed `/rename` in the
        /// terminal, and the newer of the two utterances — theirs — is what should show. Only
        /// meaningful together with ``seenTranscriptPath``. See
        /// ``sessionTitle(sessionID:terminalID:conversationStart:currentCustomTitle:)``.
        let seenCustomTitle: String?
        /// Where that transcript was, captured alongside ``seenCustomTitle`` so the comparison
        /// above never has to re-resolve it. Nil when none could be resolved at all — a
        /// non-Claude session, or one whose file did not exist yet — which switches the whole
        /// comparison off rather than treating "no transcript" as "no rename": a name set before
        /// this field existed, or before Claude Code had written its first transcript byte,
        /// behaves exactly as it always did, and always wins.
        let seenTranscriptPath: String?
        let updatedAt: Date
    }
    /// **The one part of this file that is touched from two threads.** Every other setting here is
    /// read and written by AppKit code on the main thread; a session title is written by
    /// `POST /v1/sessions/:id/title` on the server's own queue and read back by
    /// ``TargetSession/displayLabel``, which the panel computes on main and the server computes on
    /// its queue. The two neighbouring sources in that same expression each carry a lock of their
    /// own — see ``Orchestrator/title(forTerminal:)`` and ``CodexNaming/title(for:)`` — so this one
    /// does too rather than being the odd one out.
    private let sessionTitleLock = NSLock()
    private var sessionTitles: [SessionTitle] = []
    static let sessionTitleLimit = 200
    static let sessionTitleCapacity = 200
    static let sessionTitleLifetime: TimeInterval = 90 * 24 * 60 * 60

    /// True when at least one device has been paired or a password set.
    ///
    /// Read by ``RemoteTunnel`` before it will start anything: **a tunnel to an endpoint with no
    /// authentication is a mistake that should not be reachable by editing one config key**, and
    /// what is behind it is a list of your repositories, branches and task titles.
    var remoteAuthConfigured: Bool { RemoteAuth.isConfigured }

    private let dir: URL
    private var file: URL { dir.appendingPathComponent("config.json") }

    private init() {
        dir = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/clawdline", isDirectory: true)
        load()
    }

    /// A private config directory keeps persistence tests away from the person's real settings.
    init(directoryForTesting directory: URL) {
        dir = directory
        load()
    }

    func load() {
        guard let data = try? Data(contentsOf: file),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return }
        if let v = obj["y_fraction"] as? Double, v > 0.02, v < 0.9 { yFraction = CGFloat(v) }
        if let v = obj["width"] as? Double, v >= 360, v <= 1400 { width = CGFloat(v) }
        if let v = obj["hotkey"] as? String, !v.isEmpty { hotKey = v }
        if let v = obj["scope_app"] as? String { scopeApp = v }
        // After `scope_app`, because a file from before this key existed says what it meant by
        // its hotkey scope and nothing else. An unreadable value is treated as an absent one:
        // both mean "this file never chose", and both get the same answer.
        if let v = obj["terminal"] as? String, let choice = StartPoints.TerminalChoice(rawValue: v) {
            terminal = choice
            discardedTerminal = nil
        } else {
            // Said once, where somebody can find it. The answer is the same as it always was —
            // the migration — but a value that was typed and thrown away used to leave nothing
            // at all behind, and the next save overwrote the file it was typed into.
            discardedTerminal = obj["terminal"] as? String
            terminal = StartPoints.TerminalChoice.inheritedFromHotkeyScope(scopeApp)
            if let discarded = discardedTerminal {
                let legal = StartPoints.TerminalChoice.allCases.map(\.rawValue)
                    .joined(separator: ", ")
                Log.write("config: terminal \"\(discarded)\" is not one of \(legal) — read as no "
                        + "answer at all, and the next save writes \"\(terminal.rawValue)\" over it")
            }
        }
        if let v = obj["language"] as? String, !v.isEmpty { language = v }
        if let v = obj["mascot"] as? String, !v.isEmpty { mascot = v }
        if let v = obj["hooks"] as? Bool { hooks = v }
        if let v = obj["session_registry"] as? Bool { sessionRegistry = v }
        if let v = obj["remote"] as? Bool { remote = v }
        if let v = obj["remote_port"] as? Int, v > 0, v < 65536 { remotePort = v }
        if let v = obj["push_on_delivery"] as? Bool { pushOnDelivery = v }
        // `push_on_fanout` was `push_on_finish`, which also covered the turn-stopped push that no
        // longer exists. Somebody who turned that off asked not to be buzzed when work ended, and
        // the fan-out push is the half of it that survived — so the old answer is taken rather
        // than silently reset to the new default. The old key is never written back.
        if let v = obj["push_on_fanout"] as? Bool {
            pushOnFanout = v
        } else if let legacy = obj["push_on_finish"] as? Bool {
            pushOnFanout = legacy
        }
        if let v = obj["smart_notifications"] as? Bool { smartNotifications = v }
        if let v = obj["push_on_deploy"] as? Bool { pushOnDeploy = v }
        if let v = obj["on_state_change"] as? [String] { onStateChange = v }
        if let v = obj["remote_write"] as? Bool { remoteWrite = v }
        if let v = obj["remote_tunnel"] as? String, !v.isEmpty { remoteTunnel = v }
        if let v = obj["remote_tunnel_name"] as? String { remoteTunnelName = v }
        if let v = obj["remote_hostname"] as? String { remoteHostname = v }
        if let v = obj["cloudflared_path"] as? String { cloudflaredPath = v }
        if let v = obj["tmux_path"] as? String { tmuxPath = v }
        if let v = obj["codex_home"] as? String { codexHome = v }
        if let v = obj["codex_auto_name"] as? Bool { codexAutoName = v }
        if let v = obj["auto_name_assistant"] as? String,
           let assistant = Assistant(rawValue: v) {
            automaticNamingAssistant = assistant
        }
        if let v = obj["codex_auto_name_model"] as? String, !v.isEmpty {
            codexAutoNameModel = v
        }
        if let v = obj["codex_path"] as? String { codexPath = v }
        if let v = obj["orchestrator_enabled"] as? Bool { orchestratorEnabled = v }
        if let v = obj["orchestrator_max_children"] as? Int, v >= 1, v <= 10 {
            orchestratorMaxChildren = v
        }
        // `orchestrator_max_grandchildren` is deliberately not read. It was how many sessions a
        // child could open in turn, and the tree is one level deep now — a fact of the code
        // rather than of this file. Every Mac that ever ran this app has the old key sitting in
        // its `config.json` saying `3`, because the file is seeded once and never migrated; a
        // changed default would have reached none of them. Reading nothing is what makes the
        // stale number harmless, and an unknown key is passed through by `save()` rather than
        // treated as an error, so an old file keeps loading exactly as it did.
        if let v = obj["orchestrator_permission"] as? String,
           Permission(rawValue: v) != nil {
            orchestratorPermission = v
        }
        if let v = obj["orchestrator_notify_root"] as? Bool { orchestratorNotifyRoot = v }
        if let v = obj["orchestrator_agent_notify"] as? Bool { orchestratorAgentNotify = v }
        if let v = obj["orchestrator_child_linger"] as? Int, v >= -1, v <= 3600 {
            orchestratorChildLinger = v
        }
        if let v = obj["orchestrator_work_grace_minutes"] as? Int, v >= -1, v <= 1440 {
            orchestratorWorkGraceMinutes = v
        }
        if let v = obj["orchestrator_build_grace_minutes"] as? Int, v >= -1, v <= 1440 {
            orchestratorBuildGraceMinutes = v
        }
        if let v = obj["assistant_quota_low_threshold"] as? Double, v > 0, v < 100 {
            assistantQuotaLowThreshold = v
        }
        if let v = obj["status_dir"] as? String { statusDir = v }
        if let v = obj["icons_file"] as? String { iconsFile = v }
        if let v = obj["output_height"] as? Double, v >= 80, v <= 900 { outputHeight = CGFloat(v) }
        if let v = obj["backdrop"] as? Double, v >= 0, v <= 1 { backdropStrength = v }
        if let v = obj["output_font"] as? String, !v.isEmpty { outputFont = v }
        if let v = obj["output_mode"] as? String, !v.isEmpty { outputMode = v }
        if let v = obj["output_size"] as? Double, v >= 8, v <= 28 { outputSize = CGFloat(v) }
        if let v = obj["output_newest_first"] as? Bool { outputNewestFirst = v }
        if let v = obj["card_opacity"] as? Double, v >= 0, v <= 1 { cardOpacity = v }
        if let v = obj["reopen_on_return"] as? Bool { reopenOnReturn = v }
        if let v = obj["notch"] as? Bool { notch = v }
        if let v = obj["follow_target"] as? Bool { followTarget = v }
        if let v = obj["voice_engine"] as? String, !v.isEmpty { voiceEngine = v }
        if let v = obj["voice_settle_seconds"] as? Double, v >= 0, v <= 30 { voiceSettleSeconds = v }
        if let v = obj["voice_stop_seconds"] as? Double, v >= 0, v <= 300 { voiceStopSeconds = v }
        if let v = obj["voice_vocabulary"] as? [String] { voiceVocabulary = v }
        if let v = obj["send_images_as_paste"] as? Bool { sendImagesAsPaste = v }
        if let v = obj["voice_language"] as? String, !v.isEmpty { voiceLanguage = v }
        if let v = obj["whisper_binary"] as? String { whisperBinary = v }
        if let v = obj["whisper_model"] as? String { whisperModel = v }
        if let v = obj["last_target_id"] as? String { lastTargetID = v }
        if let v = obj["history"] as? [String] { history = v }
        if let rows = obj["session_titles"] as? [[String: Any]] {
            sessionTitleLock.lock()
            sessionTitles = rows.compactMap(Self.sessionTitle(from:))
            pruneSessionTitlesLocked()
            sessionTitleLock.unlock()
        }
        // What the file said, so a later save can tell an edit of ours from an edit of theirs.
        known = obj
    }

    /// Everything this object holds, as it would be written.
    private var serialised: [String: Any] {
        var obj: [String: Any] = [
            "y_fraction": Double(yFraction),
            "width": Double(width),
            "hotkey": hotKey,
            "scope_app": scopeApp,
            "terminal": terminal.rawValue,
            "language": language,
            "mascot": mascot,
            "hooks": hooks,
            "session_registry": sessionRegistry,
            "remote": remote,
            "remote_port": remotePort,
            "push_on_delivery": pushOnDelivery,
            "push_on_fanout": pushOnFanout,
            "smart_notifications": smartNotifications,
            "push_on_deploy": pushOnDeploy,
            "on_state_change": onStateChange,
            "remote_write": remoteWrite,
            "remote_tunnel": remoteTunnel,
            "remote_tunnel_name": remoteTunnelName,
            "remote_hostname": remoteHostname,
            "cloudflared_path": cloudflaredPath,
            "tmux_path": tmuxPath,
            "codex_home": codexHome,
            "codex_auto_name": codexAutoName,
            "auto_name_assistant": automaticNamingAssistant.rawValue,
            "codex_auto_name_model": codexAutoNameModel,
            "codex_path": codexPath,
            "orchestrator_enabled": orchestratorEnabled,
            "orchestrator_max_children": orchestratorMaxChildren,
            "orchestrator_permission": orchestratorPermission,
            "orchestrator_notify_root": orchestratorNotifyRoot,
            "orchestrator_agent_notify": orchestratorAgentNotify,
            "orchestrator_child_linger": orchestratorChildLinger,
            "orchestrator_work_grace_minutes": orchestratorWorkGraceMinutes,
            "orchestrator_build_grace_minutes": orchestratorBuildGraceMinutes,
            "assistant_quota_low_threshold": assistantQuotaLowThreshold,
            "status_dir": statusDir,
            "icons_file": iconsFile,
            "output_height": Double(outputHeight),
            "backdrop": backdropStrength,
            "output_font": outputFont,
            "output_mode": outputMode,
            "output_size": Double(outputSize),
            "output_newest_first": outputNewestFirst,
            "card_opacity": cardOpacity,
            "reopen_on_return": reopenOnReturn,
            "notch": notch,
            "follow_target": followTarget,
            "voice_engine": voiceEngine,
            "voice_settle_seconds": voiceSettleSeconds,
            "voice_stop_seconds": voiceStopSeconds,
            "voice_vocabulary": voiceVocabulary,
            "send_images_as_paste": sendImagesAsPaste,
            "voice_language": voiceLanguage,
            "whisper_binary": whisperBinary,
            "whisper_model": whisperModel,
            "history": Array(history.suffix(60)),
            "session_titles": sessionTitleRows(),
        ]
        // Only when there is one. A Swift Optional in this dictionary is not a JSON value, and
        // JSONSerialization throws on it — which `try?` then swallowed, so on a fresh install
        // nothing was saved at all until a target had been picked.
        if let id = lastTargetID { obj["last_target_id"] = id }
        return obj
    }

    /// What was on disk when we last read or wrote it. The reference point for "did we change
    /// this, or did they?"
    private var known: [String: Any] = [:]

    /// Keep what somebody edited by hand, keep what the app changed, and do not make them race.
    ///
    /// The app rewrites this file whenever anything moves — a prompt goes into the history, ⌘+
    /// changes the text size — and it used to write everything it held in memory. So editing
    /// the file while the app was running lost the edit, silently and at an unpredictable
    /// moment. "Quit first" was the documented answer, which is another way of saying the
    /// feature did not work.
    ///
    /// A key the app has not touched since it read the file is the file's to answer. A key the
    /// app has changed is the app's. Keys it does not know about are passed through untouched,
    /// so a setting from a newer version survives being opened by an older one.
    ///
    /// Both changed the same key: the app wins. It changed it because somebody pressed
    /// something, and that is the more recent of the two intentions we can see.
    static func merged(mine: [String: Any], known: [String: Any],
                       onDisk: [String: Any]) -> [String: Any] {
        var out = onDisk
        for (key, value) in mine where !equal(value, known[key]) { out[key] = value }
        for (key, value) in mine where out[key] == nil { out[key] = value }
        return out
    }

    private static func equal(_ a: Any?, _ b: Any?) -> Bool {
        switch (a, b) {
        case (nil, nil): return true
        case (nil, _), (_, nil): return false
        default: return (a as? NSObject)?.isEqual(b as? NSObject) ?? false
        }
    }

    /// Whether the settings on disk now say what this object says.
    ///
    /// It used to return nothing, and one route answered `local_applied: true` on the strength
    /// of having called it — see `POST /v1/sessions/:id/title`, whose whole justification for
    /// not queueing a downstream rename is that the local name is durable. A claim about a file
    /// has to come from the write of that file. Discardable because every other caller is a
    /// setting a person just changed in a window they can see, where a failure is a log line and
    /// not a different answer.
    @discardableResult
    func save() -> Bool {
        let mine = serialised
        let onDisk = (try? Data(contentsOf: file))
            .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]
        let obj = Self.merged(mine: mine, known: known, onDisk: onDisk)

        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        guard let data = try? JSONSerialization.data(withJSONObject: obj,
                                                     options: [.prettyPrinted, .sortedKeys])
        else {
            Log.write("config: could not serialise, nothing written")
            return false
        }
        do {
            try data.write(to: file)
            known = obj
            return true
        } catch {
            // Worth a line: everything above this is best-effort, and a config that silently
            // stops persisting looks exactly like one that is being ignored.
            Log.write("config: could not write — \(error.localizedDescription)")
            return false
        }
    }

    var fileURL: URL { file }

    /// One visible line, with terminal control bytes converted to separators rather than kept
    /// in config or sent through a slash command. Length is checked separately, so a route can
    /// tell an overlong request from a title that merely needed trimming and answer each in its
    /// own words; ``setSessionTitle(_:sessionID:terminalID:now:)`` refuses an overlong one too.
    static func normalizedSessionTitle(_ raw: String) -> String? {
        let separated = raw.unicodeScalars.map { scalar -> String in
            CharacterSet.controlCharacters.contains(scalar) ? " " : String(scalar)
        }.joined()
        let line = separated.split(whereSeparator: { $0.isWhitespace }).joined(separator: " ")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return line.isEmpty ? nil : line
    }

    func sessionTitle(for target: TargetSession) -> String? {
        // Both closures are paid only when there is a row that would otherwise be returned: most
        // tabs have no title at all, and this is asked once per session on every redraw.
        // `currentCustomTitle` never re-resolves the transcript — it re-reads the path the row
        // already carries through ``Transcript/customTitle(ofTranscript:)``, which is
        // ``Transcript``'s own size-keyed cache, not a second synchronous lookup on this path.
        sessionTitle(sessionID: hookSessionID(of: target), terminalID: target.id,
                     conversationStart: { Targets.processStart(of: target) },
                     currentCustomTitle: { path in
                         Transcript.customTitle(ofTranscript: URL(fileURLWithPath: path))
                     })
    }

    /// `HookBridge`'s tty table is replaced wholesale on the main thread, so it is read there or
    /// not at all — ``Transcript/sessionID(of:)`` crosses to main for the same table and says so.
    /// The crossing has to be here rather than at the callers: ``TargetSession/displayLabel`` asks
    /// for a title from the server's queue as well as from the panel.
    ///
    /// Not private because ``SessionNaming/look(at:startedAt:sources:)`` needs the same id from
    /// the same queues, and a second copy of this crossing is a second chance to get it wrong.
    ///
    /// **Queue identity, not thread identity** — see ``MainQueue``. This asked
    /// `Thread.isMainThread` until it was fixed alongside the crossing in
    /// ``Transcript/sessionID(of:)``.
    ///
    /// **This is not a production defect, and the distinction is worth keeping.** The app runs
    /// `app.run()`, where NSApplication drains the main queue on the main thread, so the thread
    /// predicate agrees with the queue there and this never fires. It is a process that parks its
    /// main thread and lets a worker drain the queue — which is what the test binary does after
    /// `dispatchMain()` — where the two disagree and the hop below would have trapped. An earlier
    /// version of this comment said the app would trap; it would not, and overstating a defect in
    /// a comment is a false claim nothing tests.
    func hookSessionID(of target: TargetSession) -> String? {
        MainQueue.hop(from: "Config.hookSessionID(of:)", alreadyOnMain: MainQueue.isCurrent) {
            HookBridge.note(for: target)?.session
        }
    }

    /// The name a person gave **this conversation**, not the name somebody once gave this tab.
    ///
    /// A terminal outlives what runs in it: leaving `claude` and starting it again in the same
    /// iTerm2 tab keeps the session UUID, so a row keyed on the terminal alone would hand the
    /// next conversation the previous one's name — for up to ``sessionTitleLifetime`` — and,
    /// because a person's title outranks everything, it would do so on top of the task title a
    /// dispatched tab was opened with. The repository already had a name for this shape one file
    /// over: *a note is keyed on a tty and can outlive the session that left it*.
    ///
    /// So the terminal is a fallback that has to prove itself, and there are two proofs:
    ///
    /// - The hook session id, when there is one. It is the conversation, so a row carrying a
    ///   *different* one is somebody else's row whatever tab it is in, and one carrying the same
    ///   one is this conversation even if the tab has changed. `--resume` keeps it, which is
    ///   right: a resumed conversation is the same conversation.
    /// - Otherwise the assistant process in the tab, by start time. Codex and a Claude without
    ///   hooks have no conversation id to offer, and the process is the conversation for exactly
    ///   as long as it runs. Compared with ``SessionRegistry/startTolerance`` because both ends
    ///   are derived from whole-second `ps` output — see ``ITerm/processStart(ofPID:)``.
    ///
    /// A row surviving both proofs still has to survive a third: **the newer of two human
    /// utterances wins.** `currentCustomTitle`, given the row's own ``SessionTitle/seenTranscriptPath``,
    /// answers what the transcript's `/rename` says right now; ``staleAfterRename(_:currentCustomTitle:)``
    /// compares that with what it said when this name was chosen. A difference means somebody
    /// typed `/rename` afterward, so this row gives way — and is dropped, so the comparison is
    /// not repeated forever.
    func sessionTitle(sessionID: String?, terminalID: String,
                      conversationStart: () -> Date? = { nil },
                      currentCustomTitle: (String) -> String? = { _ in nil }) -> String? {
        sessionTitleLock.lock()
        if let sessionID,
           let row = sessionTitles.last(where: { !$0.automatic && $0.sessionID == sessionID }) {
            sessionTitleLock.unlock()
            if Self.staleAfterRename(row, currentCustomTitle: currentCustomTitle) {
                forgetSessionTitle(row)
                return nil
            }
            return row.title
        }
        // A row that names a conversation this is not stays out of the fallback entirely. A row
        // that names none is still a candidate: a title can be set in the moment before the hook
        // note arrives, and the start time below is what decides it either way.
        let candidate = sessionTitles.last { row in
            !row.automatic && row.terminalID == terminalID
                && (sessionID == nil || row.sessionID == nil || row.sessionID == sessionID)
        }
        sessionTitleLock.unlock()
        guard let candidate,
              Self.sameConversation(candidate.startedAt, conversationStart())
        else { return nil }
        if Self.staleAfterRename(candidate, currentCustomTitle: currentCustomTitle) {
            forgetSessionTitle(candidate)
            return nil
        }
        return candidate.title
    }

    /// A model-generated Claude fallback, addressed only by the transcript's durable conversation
    /// id. There is deliberately no terminal fallback: a tab outlives the assistant inside it,
    /// while a Claude transcript UUID names exactly one conversation and survives resume.
    func automaticSessionTitle(sessionID: String) -> String? {
        guard !sessionID.isEmpty else { return nil }
        sessionTitleLock.lock()
        defer { sessionTitleLock.unlock() }
        return sessionTitles.last { $0.automatic && $0.sessionID == sessionID }?.title
    }

    /// Whether a person has typed `/rename` since this row's name was chosen. No baseline — no
    /// transcript could be resolved at set time — is not evidence of a change, so the local name
    /// stands; see ``SessionTitle/seenTranscriptPath``.
    private static func staleAfterRename(_ row: SessionTitle,
                                         currentCustomTitle: (String) -> String?) -> Bool {
        guard let path = row.seenTranscriptPath else { return false }
        return currentCustomTitle(path) != row.seenCustomTitle
    }

    /// Drop a name a later `/rename` has superseded, keyed by value rather than by index: both
    /// callers above already unlocked once to find the row, and re-finding it by identity here
    /// is simpler than threading an index through that unlock. So the next read finds no row at
    /// all and falls straight through to the automatic label, instead of repeating this same
    /// comparison and the same answer on every redraw.
    private func forgetSessionTitle(_ row: SessionTitle) {
        sessionTitleLock.lock()
        sessionTitles.removeAll { $0 == row }
        sessionTitleLock.unlock()
    }

    /// Whether two readings of "the assistant process in that tab" are the same process.
    ///
    /// Nil on both sides is a match, and that is deliberate: a tab with no assistant in it has
    /// no conversation that could end, so its own name is the tab's for as long as the tab is
    /// there. Nil on one side only is a mismatch — a name chosen in a shell is not a name for
    /// the assistant somebody later started there, and a name chosen for an assistant is not a
    /// name for the shell left behind when it exited.
    static func sameConversation(_ stored: Date?, _ current: Date?) -> Bool {
        guard let stored else { return current == nil }
        guard let current else { return false }
        return abs(stored.timeIntervalSince(current)) <= SessionRegistry.startTolerance
    }

    /// The rows as `save` writes them. Under the lock, because `serialised` is read on whichever
    /// thread called ``save()``.
    private func sessionTitleRows() -> [[String: Any]] {
        sessionTitleLock.lock()
        defer { sessionTitleLock.unlock() }
        return sessionTitles.map { row -> [String: Any] in
            var out: [String: Any] = [
                "title": row.title,
                "terminal_id": row.terminalID,
                "updated_at": row.updatedAt.timeIntervalSince1970,
            ]
            if let sessionID = row.sessionID { out["session_id"] = sessionID }
            if row.automatic { out["automatic"] = true }
            if let startedAt = row.startedAt {
                out["started_at"] = startedAt.timeIntervalSince1970
            }
            // `seen_custom_title` is legitimately absent while `seen_transcript_path` is present
            // — a transcript that had no `/rename` yet when this name was chosen — so the two
            // are written independently rather than as one optional pair.
            if let seenCustomTitle = row.seenCustomTitle { out["seen_custom_title"] = seenCustomTitle }
            if let seenTranscriptPath = row.seenTranscriptPath {
                out["seen_transcript_path"] = seenTranscriptPath
            }
            return out
        }
    }

    /// Replace every address for this live session together, so a name lives in one row rather
    /// than a pair that can drift apart. `startedAt` is the third address and the one the read
    /// side leans on when there is no conversation id — it is what stops the row outliving the
    /// conversation that chose it. `seenCustomTitle`/`seenTranscriptPath` are the fourth and
    /// fifth, the baseline a later `/rename` is measured against. See
    /// ``sessionTitle(sessionID:terminalID:conversationStart:currentCustomTitle:)``.
    ///
    /// Refused before anything is removed, and that order is the point: an overlong title used to
    /// delete the row it was too long to replace, so a request the route answers with `400` would
    /// still have taken the name off the session. The one caller today checks the length first —
    /// the Mac-side entry point this feature is heading for would not have.
    @discardableResult
    func setSessionTitle(_ raw: String, sessionID: String?, terminalID: String,
                         startedAt: Date? = nil, seenCustomTitle: String? = nil,
                         seenTranscriptPath: String? = nil, now: Date = Date()) -> String? {
        let normalized = Self.normalizedSessionTitle(raw)
        if let normalized, normalized.count > Self.sessionTitleLimit { return nil }
        sessionTitleLock.lock()
        sessionTitles.removeAll { row in
            !row.automatic && (row.terminalID == terminalID
                || (sessionID != nil && row.sessionID == sessionID))
        }
        if let normalized {
            sessionTitles.append(SessionTitle(title: normalized, sessionID: sessionID,
                                              terminalID: terminalID, automatic: false,
                                              startedAt: startedAt,
                                              seenCustomTitle: seenCustomTitle,
                                              seenTranscriptPath: seenTranscriptPath,
                                              updatedAt: now))
        }
        pruneSessionTitlesLocked(now: now)
        sessionTitleLock.unlock()
        return normalized
    }

    /// Store the low-precedence title generated for an otherwise unnamed Claude conversation.
    /// A person's row is neither removed nor replaced; it remains independently visible above
    /// this one and clearing it reveals the automatic fallback again.
    @discardableResult
    func setAutomaticSessionTitle(_ raw: String, sessionID: String, terminalID: String,
                                  now: Date = Date()) -> String? {
        guard !sessionID.isEmpty, !terminalID.isEmpty,
              let normalized = Self.normalizedSessionTitle(raw),
              normalized.count <= Self.sessionTitleLimit else { return nil }
        sessionTitleLock.lock()
        sessionTitles.removeAll {
            $0.automatic && $0.sessionID == sessionID
        }
        sessionTitles.append(SessionTitle(title: normalized, sessionID: sessionID,
                                          terminalID: terminalID, automatic: true,
                                          startedAt: nil, seenCustomTitle: nil,
                                          seenTranscriptPath: nil, updatedAt: now))
        pruneSessionTitlesLocked(now: now)
        sessionTitleLock.unlock()
        return normalized
    }

    @discardableResult
    func setSessionTitle(_ raw: String, for target: TargetSession, now: Date = Date()) -> String? {
        // Read here, at the moment the name is chosen, because that is the conversation the
        // person is naming. Asking again later would only ever describe whatever is in the tab
        // by then. The transcript snapshot is the one place this feature pays for
        // ``Transcript/record(of:)`` in full — it happens once, when somebody names a session,
        // never on the redraw path that reads it back.
        let snapshot = Transcript.customTitleSnapshot(of: target)
        return setSessionTitle(raw, sessionID: hookSessionID(of: target), terminalID: target.id,
                               startedAt: Targets.processStart(of: target),
                               seenCustomTitle: snapshot?.title,
                               seenTranscriptPath: snapshot?.path, now: now)
    }

    private static func sessionTitle(from row: [String: Any]) -> SessionTitle? {
        guard let raw = row["title"] as? String,
              let title = normalizedSessionTitle(raw), title.count <= sessionTitleLimit,
              let terminalID = row["terminal_id"] as? String, !terminalID.isEmpty,
              let timestamp = row["updated_at"] as? Double else { return nil }
        let sessionID = (row["session_id"] as? String).flatMap { $0.isEmpty ? nil : $0 }
        let automatic = row["automatic"] as? Bool ?? false
        // An automatic row has no process/terminal fallback by design. Without the transcript
        // UUID it can never be addressed, so do not let a malformed hand edit spend capacity.
        if automatic && sessionID == nil { return nil }
        // Absent in rows written before a title had to prove which conversation it belonged to.
        // Those rows keep working through their session id and stop working through the terminal
        // alone, which is the whole point of the field.
        let startedAt = (row["started_at"] as? Double).map { Date(timeIntervalSince1970: $0) }
        // Both absent in rows written before a name could go stale. Those rows keep working
        // exactly as they did — ``staleAfterRename(_:currentCustomTitle:)`` treats a nil
        // ``seenTranscriptPath`` as "nothing to compare against", not as "a rename happened".
        let seenCustomTitle = row["seen_custom_title"] as? String
        let seenTranscriptPath = (row["seen_transcript_path"] as? String).flatMap {
            $0.isEmpty ? nil : $0
        }
        return SessionTitle(title: title, sessionID: sessionID, terminalID: terminalID,
                            automatic: automatic, startedAt: startedAt,
                            seenCustomTitle: seenCustomTitle,
                            seenTranscriptPath: seenTranscriptPath,
                            updatedAt: Date(timeIntervalSince1970: timestamp))
    }

    /// Under ``sessionTitleLock``.
    private func pruneSessionTitlesLocked(now: Date = Date()) {
        let cutoff = now.addingTimeInterval(-Self.sessionTitleLifetime)
        sessionTitles = sessionTitles.filter { $0.updatedAt >= cutoff }
            .sorted { $0.updatedAt < $1.updatedAt }
        if sessionTitles.count > Self.sessionTitleCapacity {
            sessionTitles.removeFirst(sessionTitles.count - Self.sessionTitleCapacity)
        }
    }
}
