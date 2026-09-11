import Foundation

// W2-3: the host boundary, from the application's side.
//
// A use case asks the machine it runs on for six things — terminal sessions, the process table, a
// file system, a secret store, a clock and fresh identifiers — and asks for them here, in
// Foundation types, rather than by naming iTerm2, tmux, AppKit, the Keychain, `ps`, `kill` or the
// wall clock. This Mac's leaves are `Sources/MacHostAdapters.swift`; a Linux daemon supplies its
// own against the same contracts. The first lifecycle moved onto them is safe close
// (``TerminalSafeClose``), and `Tests/HostPortsTests.swift` runs it on fake ports.
//
// Nothing in this file may name a platform effect. `tools/check-architecture-boundaries.sh`
// refuses any import but Foundation and any code line naming iTerm2, tmux, the Mac facade, the
// pasteboard, the Keychain, a subprocess, a signal, a sleep or the wall clock, and calibrates that
// scan against the adapter file before it believes a zero.

// MARK: - Capabilities

/// Everything a host may or may not be able to do. Closed on purpose: a new capability has to be
/// given an owning port in ``HostPorts/provides(_:)`` before anything can ask for it.
///
/// **Not a wire schema.** The raw values are diagnostic spellings for this machine's logs and
/// typed errors. The capability identifiers Cloud routes on are fixed by a later compatibility
/// contract (`docs/platform-capability-matrix.md`), and nothing here is sent to it.
///
/// A clock and an identifier source are not in this list: every host has both, and their ports
/// exist so that time and identity are injected, not so that they can be missing.
enum HostCapability: String, CaseIterable {
    case terminalITerm = "terminal.iterm"
    case terminalTmux = "terminal.tmux"
    case processObservation = "process.observe"
    case processSignal = "process.signal"
    case files = "files"
    case secrets = "secrets"

    /// The capability that drives one terminal backend.
    static func terminal(_ backend: Backend) -> HostCapability {
        switch backend {
        case .iterm: return .terminalITerm
        case .tmux: return .terminalTmux
        }
    }
}

/// The typed answer to asking a host for something it does not do.
///
/// An error rather than an empty value, because every empty value on these ports already means
/// something: an empty inventory is "no sessions", an absent observation is "the assistant left",
/// a `nil` secret is "never stored". A host that cannot look must not answer as if it looked.
struct HostCapabilityUnavailable: Error, Equatable {
    /// The code the capability matrix reserves for this refusal.
    static let code = "capability_unavailable"

    let capability: HostCapability
    /// What was asked, in the asker's words: `end`, `observe`, `read secret`.
    let operation: String

    var message: String {
        "\(operation) needs \(capability.rawValue), which this host does not provide (\(Self.code))."
    }
}

/// What every optional port says about itself, so a lifecycle can refuse before its first effect
/// rather than discover halfway through that a later step is impossible.
protocol HostCapabilityProviding {
    var capabilities: Set<HostCapability> { get }
}

// MARK: - Terminal vocabulary

/// Where a session lives, and therefore how text gets into it.
enum Backend: String {
    case iterm
    case tmux
}

/// Somewhere text can be sent.
///
/// W2-3 correction, F1: moved here from `Sources/ITerm.swift`. Every terminal port call carries
/// one of these, so the type has to be readable from this Foundation-only file; it was not,
/// while it lived only in a file named for one backend. Its naming and display vocabulary —
/// `label`, `displayLabel` and the rest, which reach into this app's own live state
/// (``SessionWatch``, ``CodexNaming``, ``Config``, ``Orchestrator``) rather than into the host —
/// stays behind as an `extension TargetSession` in `Sources/ITerm.swift`; this is only the
/// identity a use case needs to address a session.
struct TargetSession: Equatable, Identifiable {
    let backend: Backend
    let id: String          // iTerm2 session UUID, or tmux pane id
    let name: String        // tab title (Claude Code sets it to the current task)
    let tty: String         // /dev/ttysNNN
    let windowIndex: Int
    let tabIndex: Int
    /// Which assistant is running here, or nothing when it is an ordinary shell.
    ///
    /// This was `isClaude`, a boolean, for as long as there was only one thing it could be
    /// about. It is still asked as one — see ``isAssistant`` — everywhere the question is
    /// "can I send work to this", because that answer has not changed; what changed is that
    /// how to read its screen, where to find its record and what word ends it are now three
    /// answers rather than three assumptions. See ``Assistant``.
    let assistant: Assistant?
    var cwd: String?

    /// Somewhere work can be sent, as opposed to a shell somebody left open.
    var isAssistant: Bool { assistant != nil }

    /// Kept because Claude Code genuinely is a special case in two places — the Ctrl-V paste
    /// that turns a clipboard image into `[Image #3]`, and the transcripts under `~/.claude`.
    /// Everywhere else that used to ask this wanted ``isAssistant`` and now says so.
    var isClaude: Bool { assistant == .claude }

    /// Where the tab is: the keystroke that would bring it to the front.
    ///
    /// The last thing a row can say when nothing knows what the session is *called* — and it is
    /// still a true statement about this session, which is what makes it the right last thing.
    /// A profile name is not: eleven tabs reading `Default` at once name nothing.
    var coordinate: String { "⌘\(windowIndex + 1)-\(tabIndex + 1)" }
}

/// A failure returned by the terminal operation that produced it. The kind travels with the
/// message so an HTTP response or orchestrator record never has to sample unrelated global state
/// later and guess whether this particular operation met an iTerm modal, a timeout, or ordinary
/// terminal I/O failure.
struct TerminalFailure: Error, Equatable {
    enum Kind: Equatable {
        case io
        case timeout
        case iTermAttention
        case incompleteInventory
        case identityChanged
        case unknownActivity
    }

    let kind: Kind
    let message: String
}

/// One reading of every terminal session a host can see. `Targets.Snapshot` on the Mac facade is
/// this type under its old name.
struct TerminalInventory {
    var sessions: [TargetSession] = []
    var currentID: String?
    var error: String?
    /// True only when every source needed to decide absence was actually enumerated.
    /// A partial snapshot may add or refresh rows, but it has no authority to remove one.
    var isComplete = true

    /// The ones with an assistant in them, whichever assistant that is.
    var assistantSessions: [TargetSession] { sessions.filter { $0.isAssistant } }
}

/// What assistant, if any, is still running on one tty — asked now rather than remembered.
///
/// A whole-machine process listing answers the same question for every tty at once and may hold
/// the answer for a couple of seconds, which is right for a status display and wrong here: this is
/// asked in a loop by safe close while it waits for a session to finish leaving, and a
/// two-second-old "still there" is exactly the difference between closing a quiet tab and closing
/// one that is still working. The result is scoped to the exact tty after a fresh whole-process
/// read, whose success/failure status is unambiguous. `ITerm.TTYAssistantObservation` on the Mac
/// is this type under its old name.
struct TerminalProcessObservation {
    let running: Assistant.Running?
    let error: String?
    var isComplete: Bool {
        error == nil && (running == nil || running?.processStart != nil)
    }
}

/// The two ways a lifecycle asks a process to leave. The port owns the platform's numbers for them.
enum HostProcessSignal: Equatable {
    /// Ask. Claude Code and Codex both handle it and flush on the way out.
    case terminate
    /// Stop asking.
    case kill
}

/// A process, identified precisely enough to tell "the process I mean" from "a different process
/// that now happens to have the same pid" — which is exactly what a pid means the moment the
/// original process exits and the kernel hands its number to something new. `processStart` is
/// what makes the two distinguishable.
///
/// W2-3 correction, F4: ``ProcessHost/signal(_:_:)`` used to take a bare `pid_t`, with a comment
/// asking the caller to already have proved it was still the right process. The caller — safe
/// close's `drain` loop — did hold that proof at the moment it decided to signal, but the port
/// itself received only the number, so nothing stopped a stale caller from handing it a pid with
/// no identity behind it, and nothing at the actual `kill(2)` boundary re-checked what the caller
/// had proved several steps earlier. Carrying the full identity through the port lets the adapter
/// that is about to perform the effect revalidate it right there instead of trusting a comment.
struct HostProcessIdentity: Equatable {
    let pid: pid_t
    let processStart: Date
}

/// What a ``ProcessHost`` throws from ``ProcessHost/signal(_:_:)`` when the process it was asked
/// to signal is no longer the one identified — checked again at the effect boundary rather than
/// trusted from the caller's earlier proof.
///
/// **A narrowed race, not a closed one.** Revalidating immediately before the effect shrinks the
/// window from "however long the whole safe-close loop has been running" to "the gap between one
/// read of the process table and one `kill(2)` call" — but on a Mac those are still two separate
/// syscalls, and POSIX has no atomic "signal this exact process, not whatever now has its pid"
/// primitive. This type exists so that residual gap is a typed limitation a caller can see and
/// reason about, not a silent one behind a comment claiming the proof was already made. A Linux
/// `ProcessHost` can close the gap for real with `pidfd_send_signal`, which this type's shape
/// does not block: an adapter built on it can still throw ``processGone`` for a `pidfd` that no
/// longer resolves, it would just never need ``pidReused`` — pidfd cannot be reused the way a pid
/// can.
struct HostProcessIdentityChanged: Error, Equatable {
    enum Reason: Equatable {
        /// Nothing is running at that pid any more.
        case processGone
        /// A process is running at that pid, but its start time no longer matches: the original
        /// process left and the kernel handed its number to something new.
        case pidReused
    }

    let identity: HostProcessIdentity
    let reason: Reason
}

// MARK: - Ports

/// A backend-specific way to reach a not-yet-existing session: what iTerm2's Apple Event needs to
/// open a tab, or what tmux needs to open a window on its running server or start a brand-new
/// detached session nothing is attached to yet. Two request shapes because the two backends do
/// not open the same way — `StartPoints.open` already carries this exact asymmetry.
enum TerminalCreateRequest {
    /// Open a new iTerm2 tab and type `line` at its prompt.
    case iTermTab(line: String)
    /// Open a new window on tmux's running server.
    case tmuxWindow(cwd: String, command: String)
    /// Start a brand-new detached tmux session nothing is attached to yet.
    case tmuxDetachedSession(cwd: String, command: String)
}

/// What creating a session hands back: enough to address it on later port calls, and — only for
/// a brand-new detached tmux session — the command that attaches a terminal to it. The new
/// session is deliberately not a ``TargetSession``: window/tab index, name and assistant are not
/// known at creation time, only from the next inventory, the same reasoning
/// `Sources/Targets.swift` already documents beside why starting a session is not a facade call.
struct TerminalCreated: Equatable {
    let id: String
    let backend: Backend
    let tty: String?
    /// Set only for a brand-new detached tmux session; `nil` for a session drawn on screen
    /// already (an iTerm2 tab, or a window on a server something is attached to).
    let attachCommand: String?
}

/// Terminal sessions: create one, enumerate them, address one directly, take one away.
///
/// W2-3 correction, F1: this used to be `inventory`/`sendLine`/`close` — enough for the
/// safe-close sub-lifecycle and nothing else, so every other terminal operation
/// (create/start, capture, reveal, the raw control bytes `answer` sends) still reached `ITerm`
/// or `Tmux` directly and bypassed this boundary entirely. ``create(_:)``, ``capture(_:)``,
/// ``reveal(_:activate:)`` and ``interrupt(_:to:)`` complete the terminal application surface
/// `docs/ubuntu-headless-runtime-plan.md` already named as this port's target shape;
/// `Sources/MacHostAdapters.swift` gives each a real Mac leaf and `Tests/HostPortsTests.swift`
/// proves it against a fake. `StartPoints.start` keeps its admission/refusal policy but delegates
/// its admitted iTerm/tmux creation to ``create(_:)``; `Targets.answer` keeps menu parsing in the
/// facade while its admitted bytes cross ``interrupt(_:to:)``. The policy remains application
/// code, while the platform effect is owned by this boundary.
protocol TerminalHost: HostCapabilityProviding {
    /// A fresh inventory taken now. Incompleteness is carried in the value rather than thrown,
    /// because a partial reading is still worth publishing; it is only never proof of absence.
    func inventory() throws -> TerminalInventory
    /// Type one line into a session and submit it. `nil` is delivery to the tty, not proof that
    /// anything read it; any other answer is the backend's own reason.
    func sendLine(_ text: String, to session: TargetSession) throws -> String?
    /// Take the tab or pane away. The caller proves nothing is still running in it first.
    func close(_ session: TargetSession) throws -> String?
    /// Open a new tab, window or detached session. Throws the backend's own ``TerminalFailure``
    /// — never flattened to a message — the same type ``sendLine(_:to:)``/``close(_:)`` already
    /// mean by "the backend's own reason", just not lost on this one path that already carried a
    /// structured failure kind (``TerminalFailure/Kind/iTermAttention`` among them) before this
    /// port existed.
    func create(_ request: TerminalCreateRequest) throws -> TerminalCreated
    /// What is currently visible in the session — no history, even on a backend that keeps
    /// scrollback — or `nil` when nothing could be captured. Silence, not a throw, is this port's
    /// existing answer for an ordinary capture failure; a genuinely unavailable backend still
    /// throws ``HostCapabilityUnavailable``.
    func capture(_ session: TargetSession) throws -> String?
    /// Bring the session's tab or window forward, or merely make it the one its terminal is
    /// showing when `activate` is false. Throws the backend's own ``TerminalFailure`` on refusal;
    /// success is silent.
    func reveal(_ session: TargetSession, activate: Bool) throws
    /// Raw key bytes delivered outside a bracketed paste — a menu digit, back-tab, or a control
    /// byte such as the interrupt byte a caller sends to stop a turn without closing the tab.
    /// `nil` is delivery, the same contract as ``sendLine(_:to:)``.
    func interrupt(_ bytes: [UInt8], to session: TargetSession) throws -> String?
}

/// The process table, as far as safe close needs it: one exact tty, and one identified process.
protocol ProcessHost: HostCapabilityProviding {
    /// A fresh, confidence-bearing reading of one tty. A scan that failed is an incomplete
    /// observation, never an absent assistant.
    func observeAssistant(onTTY tty: String) throws -> TerminalProcessObservation
    /// Signal one process. W2-3 correction, F4: the caller still owns the proof that `identity`
    /// is the process it means — this only changed from a bare `pid_t` to
    /// ``HostProcessIdentity`` so the adapter performing the effect can check that proof again
    /// right before it acts, and throw ``HostProcessIdentityChanged`` rather than silently
    /// signal whatever now has that pid.
    func signal(_ identity: HostProcessIdentity, _ signal: HostProcessSignal) throws
}

/// Bytes at a path. Absence and failure are never spelled the same way.
protocol FileSystemHost: HostCapabilityProviding {
    /// `nil` only when nothing is at `path`. A directory or an unreadable file throws.
    func contents(atPath path: String) throws -> Data?
    /// The whole new contents or the old ones; a reader never sees half a write.
    func writeAtomically(_ data: Data, toPath path: String) throws
    /// Removing what is already absent succeeds: the state asked for is the state reached.
    func removeItem(atPath path: String) throws
}

/// Named secrets. The bytes cross this boundary as values and are never part of a refusal.
///
/// W2-3 correction, F3: ``loadOrCreate(_:create:)`` and ``rotate(_:replace:)`` are **closed**
/// operations — a concrete store serializes each of them against every other call for the same
/// `account`, the way ``CloudKeyStoring/coordinator`` already serializes
/// `CloudKeys.loadOrCreateDeviceKeyPair()`/`loadOrCreateMasterSecret()`/`restoreMasterSecret(from:)`
/// today. A caller must not assemble the same behaviour out of ``data(for:)`` followed by
/// ``set(_:for:)``: two callers racing that composition can each read absence and each write a
/// different secret under the same account, and the second write silently wins. ``data(for:)``
/// stays on this protocol for read-only inspection — nothing here stops a caller from looking —
/// but it is not the load half of a create-if-absent or a replace.
protocol SecretStore: HostCapabilityProviding {
    /// `nil` only when nothing is stored under `account`. Read-only: see the type's doc for why
    /// this must not be paired with ``set(_:for:)`` to implement create-if-absent or replace.
    func data(for account: String) throws -> Data?
    func set(_ data: Data, for account: String) throws
    /// The existing value under `account`, or — atomically, and only once even when several
    /// callers race this for the same account — `create()`'s value, stored and then returned to
    /// every one of them. `@Sendable` to match ``CloudKeyStoreCoordinator/withCriticalRegion(_:)``,
    /// which every concrete store's closed region runs through.
    func loadOrCreate(_ account: String, create: @Sendable () throws -> Data) throws -> Data
    /// Replace whatever is under `account` (`nil` if nothing was) with `replace`'s result, store
    /// it, and return it — atomically, and serialized against every other call this protocol
    /// makes for the same account, including a concurrent ``loadOrCreate(_:create:)``.
    func rotate(_ account: String, replace: @Sendable (Data?) throws -> Data) throws -> Data
    func remove(_ account: String) throws
}

/// Elapsed time and a way to wait on it, injected so a lifecycle's deadlines can be checked
/// against time that is passed in rather than time that has to pass.
///
/// **Monotonic, not the wall clock.** W2-3 correction, F7: this used to be `now() -> Date`, and a
/// bounded wait measured elapsed time as the difference between two of them. NTP stepping the
/// clock back, or somebody setting it back by hand, would unboundedly stretch what is supposed
/// to be a five-and-a-half-second TERM/KILL escalation; stepping it forward would skip the
/// polite wait entirely. Neither is hypothetical on a Mac somebody actually uses, and a bounded
/// safe close that is not actually bounded is the defect this port exists to close.
/// ``monotonicNow()`` is a reading from a source that only ever advances forward at the
/// machine's own pace and carries no calendar meaning by itself — only a difference between two
/// readings from the same host means anything, which is the only thing this lifecycle ever asks
/// of it.
protocol HostClock {
    func monotonicNow() -> TimeInterval
    func sleep(for seconds: TimeInterval)
}

/// Fresh identifiers.
protocol IdentityHost {
    /// A new opaque identifier, unique for the life of this host's durable state.
    func newIdentifier() -> String
}

/// A host that provides none of the optional capabilities. Every operation is a typed refusal, so
/// a composition that left a port out fails by name instead of answering with an empty value that
/// already means something else.
struct UnsupportedHost: TerminalHost, ProcessHost, FileSystemHost, SecretStore {
    var capabilities: Set<HostCapability> { [] }

    func inventory() throws -> TerminalInventory {
        // An inventory is at least the portable backend's, so that is the capability named.
        throw HostCapabilityUnavailable(capability: .terminalTmux, operation: "terminal inventory")
    }

    func sendLine(_ text: String, to session: TargetSession) throws -> String? {
        throw HostCapabilityUnavailable(capability: .terminal(session.backend), operation: "send")
    }

    func close(_ session: TargetSession) throws -> String? {
        throw HostCapabilityUnavailable(capability: .terminal(session.backend), operation: "close")
    }

    func create(_ request: TerminalCreateRequest) throws -> TerminalCreated {
        // No session exists yet to name a backend from, so — as with `inventory()` — the
        // portable backend is what this capability-less host names itself unable to do.
        throw HostCapabilityUnavailable(capability: .terminalTmux, operation: "create")
    }

    func capture(_ session: TargetSession) throws -> String? {
        throw HostCapabilityUnavailable(capability: .terminal(session.backend), operation: "capture")
    }

    func reveal(_ session: TargetSession, activate: Bool) throws {
        throw HostCapabilityUnavailable(capability: .terminal(session.backend), operation: "reveal")
    }

    func interrupt(_ bytes: [UInt8], to session: TargetSession) throws -> String? {
        throw HostCapabilityUnavailable(capability: .terminal(session.backend), operation: "interrupt")
    }

    func observeAssistant(onTTY tty: String) throws -> TerminalProcessObservation {
        throw HostCapabilityUnavailable(capability: .processObservation, operation: "observe")
    }

    func signal(_ identity: HostProcessIdentity, _ signal: HostProcessSignal) throws {
        throw HostCapabilityUnavailable(capability: .processSignal, operation: "signal")
    }

    func contents(atPath path: String) throws -> Data? {
        throw HostCapabilityUnavailable(capability: .files, operation: "read file")
    }

    func writeAtomically(_ data: Data, toPath path: String) throws {
        throw HostCapabilityUnavailable(capability: .files, operation: "write file")
    }

    func removeItem(atPath path: String) throws {
        throw HostCapabilityUnavailable(capability: .files, operation: "remove file")
    }

    func data(for account: String) throws -> Data? {
        throw HostCapabilityUnavailable(capability: .secrets, operation: "read secret")
    }

    func set(_ data: Data, for account: String) throws {
        throw HostCapabilityUnavailable(capability: .secrets, operation: "store secret")
    }

    func loadOrCreate(_ account: String, create: @Sendable () throws -> Data) throws -> Data {
        throw HostCapabilityUnavailable(capability: .secrets, operation: "load-or-create secret")
    }

    func rotate(_ account: String, replace: @Sendable (Data?) throws -> Data) throws -> Data {
        throw HostCapabilityUnavailable(capability: .secrets, operation: "rotate secret")
    }

    func remove(_ account: String) throws {
        throw HostCapabilityUnavailable(capability: .secrets, operation: "remove secret")
    }
}

// MARK: - Composition

/// The ports one use case runs on. Built at the edge — ``HostPorts/mac`` for this app, fakes in a
/// test, a Linux composition later — and handed in; nothing inside a lifecycle reaches for a
/// platform global.
struct HostPorts {
    let terminal: any TerminalHost
    let process: any ProcessHost
    let files: any FileSystemHost
    let secrets: any SecretStore
    let clock: any HostClock
    let identity: any IdentityHost

    /// The optional ports default to ``UnsupportedHost``. The clock and the identifier source never
    /// default, so a composition cannot fall back to real time without saying so.
    init(terminal: any TerminalHost = UnsupportedHost(),
         process: any ProcessHost = UnsupportedHost(),
         files: any FileSystemHost = UnsupportedHost(),
         secrets: any SecretStore = UnsupportedHost(),
         clock: any HostClock,
         identity: any IdentityHost) {
        self.terminal = terminal
        self.process = process
        self.files = files
        self.secrets = secrets
        self.clock = clock
        self.identity = identity
    }

    /// Whether the port that owns `capability` provides it. Asked of that owner rather than of a
    /// union, so a terminal port cannot vouch for a secret store.
    func provides(_ capability: HostCapability) -> Bool {
        switch capability {
        case .terminalITerm, .terminalTmux: return terminal.capabilities.contains(capability)
        case .processObservation, .processSignal: return process.capabilities.contains(capability)
        case .files: return files.capabilities.contains(capability)
        case .secrets: return secrets.capabilities.contains(capability)
        }
    }

    /// The first of `required` this host lacks, as the refusal `operation` returns before doing
    /// anything at all.
    func firstUnavailable(_ required: [HostCapability],
                          operation: String) -> HostCapabilityUnavailable? {
        required.first { !provides($0) }.map {
            HostCapabilityUnavailable(capability: $0, operation: operation)
        }
    }
}

// MARK: - Safe close, on ports

/// What a safe close returns instead of closing. The Mac facade has always answered with a
/// sentence, and ``message`` is that sentence byte for byte; the case says which kind it was.
enum TerminalSafeCloseRefusal: Error, Equatable {
    /// The host cannot perform a step this close needs. Returned before the first effect.
    case capabilityUnavailable(HostCapabilityUnavailable)
    /// A step ran and refused, or the backend failed. The tab stays open.
    case refused(String)

    var message: String {
        switch self {
        case .capabilityUnavailable(let unavailable): return unavailable.message
        case .refused(let message): return message
        }
    }
}

/// End a session and close the tab it was in, using nothing but the ports it is handed.
///
/// **Two steps, in this order, and the order is the whole of it.** `/exit` first, so Claude Code
/// leaves the way it would if somebody typed it — flushing its transcript rather than being killed
/// in the middle of writing one. Then the tab, once the process it was holding is actually gone.
///
/// Closing straight away would work and would be worse: the session's own record of the
/// conversation is the thing you would still want tomorrow, and it is being appended to right up
/// to the moment the process ends.
///
/// **This used to be a fixed pause, and the fixed pause is what broke.** It waited 1.2 seconds and
/// closed regardless — which is fine when the word lands at an idle prompt and wrong the moment it
/// does not. A session in the middle of a tool call queues `/exit` and keeps working, so the tab
/// still had a job in it when the close arrived, and iTerm2 does what a terminal should do about
/// that: it puts up a sheet and asks. A sheet is modal. The Apple event never returns, `osascript`
/// never exits, and because every remote request is answered on one serial queue, a phone that
/// pressed End froze every page in the house until somebody walked to the Mac and clicked a button
/// they could not see.
///
/// So the pause is now an answer instead of a guess — see ``Farewell``. The ordinary case got
/// faster too: `/exit` at an idle prompt is done in a few hundred milliseconds.
///
/// A deadline never becomes permission to close a busy tab. After the polite word and bounded
/// TERM/KILL attempts, a fresh exact-tty scan must positively prove the assistant absent. If the
/// process remains, or that scan fails, the tab stays open and the caller records a terminal
/// intervention.
///
/// **And a host that cannot finish the close is refused before it starts it.** Typing the quit
/// word into a session that this host could not then observe or signal would leave it half ended,
/// so every operation asks ``HostPorts/firstUnavailable(_:operation:)`` first and returns
/// ``TerminalSafeCloseRefusal/capabilityUnavailable(_:)`` with nothing sent.
enum TerminalSafeClose {
    /// What to do next while waiting for a session to finish leaving.
    ///
    /// Split out from the loop that runs it because this is the part with the decisions in it,
    /// and a decision that can only be exercised by ending somebody's real session is a decision
    /// with no tests. The loop below is three lines of sleeping; everything that could be wrong
    /// about *when to stop being polite* is here, and is checked against a clock that is passed
    /// in rather than one that has to pass.
    enum Farewell {
        /// The port's own identity type (W2-3 correction, F4) — kept under this name too because
        /// `Tests/MascotTests.swift` and `Sources/Targets.swift` already spell it
        /// `Farewell.ProcessIdentity`/`Targets.Farewell.ProcessIdentity`.
        typealias ProcessIdentity = HostProcessIdentity

        enum Step: Equatable {
            /// Still leaving on its own. Look again in a moment.
            case wait
            /// Ask the process to go.
            case term(pid_t)
            /// Stop asking.
            case kill(pid_t)
            /// Nothing is holding the tab. Take it.
            case close
            /// The bounded attempts are over, but a process is still positively present.
            case refuse
        }

        /// How long the word gets before anything harsher happens.
        ///
        /// Three seconds, and short on purpose. A session at its prompt reads `/exit` and is gone
        /// inside one; a session in the middle of a tool call has *queued* the word and will not
        /// read it until the tool returns, which is not a thing three more seconds fixes. Waiting
        /// longer would only be waiting — and this runs on the queue that answers every other
        /// request, so every second here is a second the page does not repaint.
        ///
        /// It can afford to be short because the next rung is not violence. `SIGTERM` is how a
        /// program is asked to leave; both assistants handle it and flush on the way out. The
        /// thing this replaced — closing the tab regardless — hung up the tty underneath them,
        /// which is less notice than any step below.
        static let polite: TimeInterval = 3
        /// After `SIGTERM`. Claude Code and Codex both handle it and leave; this is the room to.
        static let afterTerm: TimeInterval = 1.5
        /// After `SIGKILL`. Only the kernel's own bookkeeping happens in here.
        static let afterKill: TimeInterval = 1

        static func step(elapsed: TimeInterval, pid: pid_t?,
                         termed: Bool, killed: Bool) -> Step {
            // Gone is gone, at any point — including before the first sleep, which is the
            // common case and the reason this is faster than what it replaces.
            guard let pid else { return .close }
            if elapsed < polite { return .wait }
            if !termed { return .term(pid) }
            if elapsed < polite + afterTerm { return .wait }
            if !killed { return .kill(pid) }
            if elapsed < polite + afterTerm + afterKill { return .wait }
            // A process still visible after SIGKILL is exactly the case where a tab close is not
            // safe. Preserve it for a person; elapsed time is never evidence of absence.
            return .refuse
        }

        /// Identity-bearing form used by production safe close. Kept as its own seam so tests
        /// can replace a process between TERM and KILL without signalling a real process.
        static func step(elapsed: TimeInterval, identity: ProcessIdentity?,
                         termed: ProcessIdentity?, killed: ProcessIdentity?) -> Step {
            // Once a signal has been sent, the next rung belongs only to the same kernel
            // process. A different PID is plainly different; the same PID with a different
            // start instant is PID reuse and is just as different. In either case fail closed —
            // a new process must never inherit the old one's TERM/KILL history.
            if let identity, let termed, identity != termed { return .refuse }
            if let identity, let killed, identity != killed { return .refuse }
            return step(elapsed: elapsed, pid: identity?.pid,
                        termed: termed != nil, killed: killed != nil)
        }
    }

    /// Require a fresh, complete inventory to preserve the exact terminal id/backend/tty tuple.
    /// The assistant label is included too: a tab now occupied by another assistant is not the
    /// terminal operation the caller admitted.
    static func stableTerminal(_ expected: TargetSession,
                               in snapshot: TerminalInventory,
                               allowAssistantGone: Bool = false)
        -> Result<TargetSession, TerminalFailure> {
        guard snapshot.isComplete else {
            return .failure(TerminalFailure(
                kind: .incompleteInventory,
                message: snapshot.error ?? "The terminal inventory was incomplete; nothing was closed."))
        }
        guard let observed = snapshot.sessions.first(where: { $0.id == expected.id }) else {
            return .failure(TerminalFailure(
                kind: .incompleteInventory,
                message: "The terminal is no longer present in the fresh inventory; nothing was closed."))
        }
        let assistantStable = observed.assistant == expected.assistant
            || (allowAssistantGone && expected.assistant != nil && observed.assistant == nil)
        guard observed.backend == expected.backend, observed.tty == expected.tty,
              assistantStable else {
            return .failure(TerminalFailure(
                kind: .identityChanged,
                message: "The terminal identity changed before close; nothing was closed."))
        }
        return .success(observed)
    }

    /// The quit word, the wait, and the close. `nil` means the tab was closed.
    static func end(_ session: TargetSession, ports: HostPorts) -> TerminalSafeCloseRefusal? {
        if let missing = ports.firstUnavailable(
            [.terminal(session.backend), .processObservation, .processSignal], operation: "end") {
            return .capabilityUnavailable(missing)
        }
        do {
            let current: TargetSession
            let before = try ports.terminal.inventory()
            switch stableTerminal(session, in: before) {
            case .success(let observed): current = observed
            case .failure(let failure): return .refused(failure.message)
            }
            // Typed as a line, not as a keystroke: the word is text at a prompt. Which word depends
            // on which assistant — Claude Code leaves on `/exit`, Codex on `/quit`, and each
            // refuses the other's, which would leave the session open with the tab closing under it.
            let word = (current.assistant ?? .claude).quitLine
            if let failure = try ports.terminal.sendLine(word, to: current) {
                return .refused(failure)
            }
            if let refusal = try drain(current, ports: ports) { return refusal }
            let after = try ports.terminal.inventory()
            switch stableTerminal(current, in: after, allowAssistantGone: true) {
            case .failure(let failure):
                return .refused(failure.message)
            case .success(let closing):
                // The inventory and exact-tty process observation are independent proofs. Ask the
                // tty once more after the final inventory so a process that appeared during the
                // quit sequence cannot be hung up by the backend close.
                let observation = try ports.process.observeAssistant(onTTY: closing.tty)
                guard observation.isComplete else {
                    return .refused(observation.error
                        ?? "Could not verify whether the assistant left the tty.")
                }
                guard observation.running == nil else {
                    return .refused("An assistant appeared on \(closing.tty); the tab was left open.")
                }
                return try ports.terminal.close(closing).map(TerminalSafeCloseRefusal.refused)
            }
        } catch {
            return refusal(for: error)
        }
    }

    /// Close a shell-only tab only after a fresh exact-tty process observation proves there is no
    /// assistant left in it. Inventory labels can be nil while a scan is degraded; they are not
    /// authority to hang up a tty. Nothing is signalled, so signalling is not required.
    static func closeIfAssistantGone(_ session: TargetSession,
                                     ports: HostPorts) -> TerminalSafeCloseRefusal? {
        if let missing = ports.firstUnavailable(
            [.terminal(session.backend), .processObservation], operation: "close") {
            return .capabilityUnavailable(missing)
        }
        do {
            let current: TargetSession
            let before = try ports.terminal.inventory()
            switch stableTerminal(session, in: before, allowAssistantGone: true) {
            case .success(let observed): current = observed
            case .failure(let failure): return .refused(failure.message)
            }
            let observation = try ports.process.observeAssistant(onTTY: current.tty)
            guard observation.isComplete else {
                return .refused(observation.error
                    ?? "Could not verify whether the assistant left the tty.")
            }
            guard observation.running == nil else {
                return .refused("An assistant is still running on \(current.tty); the tab was left open.")
            }
            let after = try ports.terminal.inventory()
            switch stableTerminal(current, in: after, allowAssistantGone: true) {
            case .failure(let failure):
                return .refused(failure.message)
            case .success(let closing):
                return try ports.terminal.close(closing).map(TerminalSafeCloseRefusal.refused)
            }
        } catch {
            return refusal(for: error)
        }
    }

    /// Block until nothing is running on that session's tty, or until it has been made so.
    /// `nil` means the tty is empty; the tab has not been touched.
    static func waitToBeGone(_ session: TargetSession, ports: HostPorts) -> TerminalSafeCloseRefusal? {
        if let missing = ports.firstUnavailable(
            [.processObservation, .processSignal], operation: "wait") {
            return .capabilityUnavailable(missing)
        }
        do {
            return try drain(session, ports: ports)
        } catch {
            return refusal(for: error)
        }
    }

    /// The tty and not the session id, because this is a question about processes and iTerm2's
    /// idea of a session is not one. It works the same for a tmux pane: `kill-pane` does not put
    /// up a sheet, but it does send a `SIGHUP` to whatever is still running, and a transcript
    /// half-written is no better on that side.
    private static func drain(_ session: TargetSession,
                              ports: HostPorts) throws -> TerminalSafeCloseRefusal? {
        let started = ports.clock.monotonicNow()
        var termed: Farewell.ProcessIdentity?
        var killed: Farewell.ProcessIdentity?
        while true {
            let observation = try ports.process.observeAssistant(onTTY: session.tty)
            guard observation.isComplete else {
                return .refused(observation.error
                    ?? "Could not verify whether the assistant left the tty.")
            }
            let running = observation.running
            let identity = running.flatMap { running -> Farewell.ProcessIdentity? in
                guard let processStart = running.processStart else { return nil }
                return Farewell.ProcessIdentity(pid: running.pid, processStart: processStart)
            }
            guard running == nil || identity != nil else {
                return .refused("Could not verify the assistant process start on \(session.tty).")
            }
            let elapsed = ports.clock.monotonicNow() - started
            switch Farewell.step(elapsed: elapsed, identity: identity,
                                 termed: termed, killed: killed) {
            case .close:
                return nil
            case .refuse:
                return .refused("The assistant is still running on \(session.tty); the tab was left open.")
            case .wait:
                ports.clock.sleep(for: 0.2)
            case .term(let pid):
                guard let identity, identity.pid == pid else {
                    return .refused("The assistant identity changed before TERM; the tab was left open.")
                }
                termed = identity
                try ports.process.signal(identity, .terminate)
                ports.clock.sleep(for: 0.2)
            case .kill(let pid):
                guard let identity, identity == termed, identity.pid == pid else {
                    return .refused("The assistant identity changed after TERM; the tab was left open.")
                }
                killed = identity
                try ports.process.signal(identity, .kill)
                ports.clock.sleep(for: 0.2)
            }
        }
    }

    /// A port that throws despite claiming the capability is still a typed refusal, never a close.
    private static func refusal(for error: Error) -> TerminalSafeCloseRefusal {
        if let unavailable = error as? HostCapabilityUnavailable {
            return .capabilityUnavailable(unavailable)
        }
        // W2-3 correction, F4: the adapter revalidated the identity right at the signal boundary
        // and found it had already changed — a real answer, not a bug, and worth its own sentence
        // rather than a raw struct description.
        if let changed = error as? HostProcessIdentityChanged {
            let why = changed.reason == .processGone
                ? "it was no longer running"
                : "its pid had been reused by a different process"
            return .refused("Could not signal pid \(changed.identity.pid): \(why); "
                             + "the tab was left open.")
        }
        return .refused(String(describing: error))
    }
}
