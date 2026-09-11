import AppKit
import Foundation
#if canImport(ClawdlineApplication)
import ClawdlineApplication // W3-1 correction: real cross-module import, see Sources/HostPorts.swift
#endif

// `Backend`, `TerminalFailure`, the inventory behind `Targets.Snapshot` and the safe-close
// lifecycle are the terminal port's vocabulary and live in `Sources/HostPorts.swift`, which imports
// Foundation only. This file is the Mac facade over them: every name its callers spell is still
// here, and the effects behind the ported paths are in `Sources/MacHostAdapters.swift`.

/// The one list the panel works from, merged out of every backend.
///
/// The two rarely collide in practice. When Claude Code runs inside tmux, the iTerm2 session
/// that hosts it has `tmux` as its foreground process, not `claude` — so only the tmux pane
/// gets flagged, and the same session does not show up twice. The dedupe below is a belt for
/// the cases where it would.
enum Targets {
    /// Replaces only the irreversible backend close in exact-tty guard tests.
    static var terminalCloseForTesting: ((TargetSession) -> String?)?
    /// Replaces the complete terminal inventory used by irreversible close tests. Production
    /// always takes a new combined iTerm/tmux snapshot inside the terminal broker.
    static var safeCloseInventoryForTesting: (() -> Snapshot)?
    /// Replaces only the capture behind safe-close activity classification.
    static var safeCloseCaptureForTesting: ((TargetSession) -> String?)?
    static var safeCloseSignalForTesting: ((pid_t, Int32) -> Void)?
    static var safeCloseSleepForTesting: ((TimeInterval) -> Void)?
    static var safeCloseNowForTesting: (() -> Date)?

    // W2-3 correction, F5: `HostPorts.mac` — what `end(_:)` and every other caller gets — no
    // longer reads any of the three seams above. `Tests/MascotTests.swift` still drives
    // `closeIfAssistantGone(_:)` and `waitToBeGoneForTesting(_:)` through them, so those two
    // entry points, and only those two, run on this composition instead: the real Mac leaves
    // with the seam checked first. This restores each seam to the scope it originally had —
    // `terminalCloseForTesting` affects the shell-only close again, not `end(_:)` too — rather
    // than the general host boundary carrying test-only global mutable state that production
    // never sets but every future Application composition would still have to reason about.
    private static var seamAdaptedPorts: HostPorts {
        HostPorts(terminal: SeamAdaptedTerminalHost(), process: SeamAdaptedProcessHost(),
                  files: HostPorts.mac.files, secrets: HostPorts.mac.secrets,
                  clock: SeamAdaptedClock(), identity: HostPorts.mac.identity)
    }

    /// ``MacTerminalHost`` for everything except `close`, which checks
    /// ``Targets/terminalCloseForTesting`` first.
    private struct SeamAdaptedTerminalHost: TerminalHost {
        private let real = MacTerminalHost()
        var capabilities: Set<HostCapability> { real.capabilities }
        func inventory() throws -> TerminalInventory { try real.inventory() }
        func sendLine(_ text: String, to session: TargetSession) throws -> String? {
            try real.sendLine(text, to: session)
        }
        func close(_ session: TargetSession) throws -> String? {
            if let seam = Targets.terminalCloseForTesting { return seam(session) }
            return try real.close(session)
        }
        func create(_ request: TerminalCreateRequest) throws -> TerminalCreated {
            try real.create(request)
        }
        func capture(_ session: TargetSession) throws -> String? { try real.capture(session) }
        func reveal(_ session: TargetSession, activate: Bool) throws {
            try real.reveal(session, activate: activate)
        }
        func interrupt(_ bytes: [UInt8], to session: TargetSession) throws -> String? {
            try real.interrupt(bytes, to: session)
        }
    }

    /// ``MacProcessHost`` for observation; `signal` checks
    /// ``Targets/safeCloseSignalForTesting`` first.
    private struct SeamAdaptedProcessHost: ProcessHost {
        private let real = MacProcessHost()
        var capabilities: Set<HostCapability> { real.capabilities }
        func observeAssistant(onTTY tty: String) throws -> TerminalProcessObservation {
            try real.observeAssistant(onTTY: tty)
        }
        func signal(_ identity: HostProcessIdentity, _ signal: HostProcessSignal) throws {
            guard let seam = Targets.safeCloseSignalForTesting else {
                try real.signal(identity, signal)
                return
            }
            seam(identity.pid, signal == .terminate ? SIGTERM : SIGKILL)
        }
    }

    /// The real monotonic clock, unless ``Targets/safeCloseNowForTesting`` /
    /// ``Targets/safeCloseSleepForTesting`` are set. The seams stayed `Date`-typed on purpose —
    /// every suite that sets them keeps doing so unchanged — and are converted to the port's
    /// monotonic `TimeInterval` here, at the one place that still has to know both.
    private struct SeamAdaptedClock: HostClock {
        func monotonicNow() -> TimeInterval {
            (Targets.safeCloseNowForTesting?() ?? Date()).timeIntervalSince1970
        }
        func sleep(for seconds: TimeInterval) {
            if let seam = Targets.safeCloseSleepForTesting { seam(seconds) }
            else { Thread.sleep(forTimeInterval: seconds) }
        }
    }

    enum SafeCloseActivity: Equatable {
        case busy
        case idle
        case unknown
    }

    /// One combined iTerm2/tmux reading. The type is the terminal port's ``TerminalInventory``;
    /// this is its name on the facade, kept for every caller that already spells it.
    typealias Snapshot = TerminalInventory

    struct Reconciliation {
        let sessions: [TargetSession]
        /// Rows copied from the previous complete answer because this scan could not see enough
        /// to prove that they disappeared.
        let preserved: Int
        /// Rows omitted by the terminal inventory whose tty was independently proved to have no
        /// assistant process. This lets real closure converge even if JXA stays unhealthy.
        let confirmedRemoved: Int
    }

    /// Apply the information content of a scan, not merely the array it happened to return.
    ///
    /// A complete inventory replaces the old one, including with an empty list. An incomplete
    /// inventory may refresh everything it did observe but cannot use an unobserved row as proof
    /// of closure. This is confirmation rather than a grace period: the very next complete scan
    /// removes a genuinely closed session, however soon or late that answer arrives.
    static func reconcile(previous: [TargetSession], scanned: [TargetSession],
                          complete: Bool,
                          confirmedAbsent: Set<String> = []) -> Reconciliation {
        guard !complete else {
            return Reconciliation(sessions: scanned, preserved: 0, confirmedRemoved: 0)
        }
        let observed = Set(scanned.map(\.id))
        let omitted = previous.filter { !observed.contains($0.id) }
        let kept = omitted.filter { !confirmedAbsent.contains($0.id) }
        return Reconciliation(sessions: scanned + kept, preserved: kept.count,
                              confirmedRemoved: omitted.count - kept.count)
    }

    /// A terminal inventory cannot authoritatively omit an iTerm row while `ps` still sees an
    /// assistant on that row's tty. This catches the contradictory answer where JXA reports
    /// `iTerm2.running() == false` (or silently drops a window) during an Apple-event wobble.
    /// tmux rows are deliberately excluded: iTerm can genuinely be stopped while tmux hosts an
    /// assistant, and the combined snapshot will carry that pane through its own source.
    static func hasLiveProcessContradiction(previous: [TargetSession], scanned: [TargetSession],
                                            runningTTYs: Set<String>) -> Bool {
        func bare(_ tty: String) -> String {
            tty.hasPrefix("/dev/") ? String(tty.dropFirst("/dev/".count)) : tty
        }
        let observed = Set(scanned.filter { $0.backend == .iterm }.map { bare($0.tty) })
        return previous.contains { session in
            guard session.backend == .iterm, session.isAssistant else { return false }
            let tty = bare(session.tty)
            return runningTTYs.contains(tty) && !observed.contains(tty)
        }
    }

    static func snapshot() -> Snapshot {
        snapshot(processScan: ITerm.assistantProcessScan())
    }

    /// Combine terminal backends against one already-observed process population. SessionWatch
    /// owns that population; accepting it here prevents ITerm snapshot construction from
    /// starting a second ps for the same publication.
    static func snapshot(processScan: ITerm.AssistantProcessScan) -> Snapshot {
        var snap = Snapshot()
        let iterm = ITerm.snapshot(processScan: processScan)
        snap.currentID = iterm.currentID
        snap.isComplete = iterm.isComplete

        var seenTTYs = Set<String>()
        // Asked once. Walking it twice was two `list-panes` subprocesses per scan for one answer
        // that cannot have changed between them.
        let tmux = Tmux.paneObservation()
        let panes = tmux.sessions
        if !tmux.isComplete {
            snap.isComplete = false
            snap.error = tmux.error?.message ?? "Could not enumerate tmux panes."
        }
        // tmux first: when a pane and its host terminal both appear, the pane is the one that
        // can actually receive text, so it should win the tty.
        for pane in panes where pane.isAssistant {
            seenTTYs.insert(pane.tty)
            snap.sessions.append(pane)
        }
        for pane in panes where !pane.isAssistant {
            guard !seenTTYs.contains(pane.tty) else { continue }
            seenTTYs.insert(pane.tty)
            snap.sessions.append(pane)
        }
        for session in iterm.sessions where !seenTTYs.contains(session.tty) {
            snap.sessions.append(session)
        }

        // A partial iTerm result remains worth reporting even when tmux supplied valid rows: the
        // caller may publish those rows, but it must not remove older iTerm rows by omission.
        if let e = iterm.error { snap.error = e }
        return snap
    }

    /// Consulted only when nothing else has replaced the walk. The suite installs it once, for
    /// the whole run: safe close is asynchronous and beat-driven, so a fixture left holding a due
    /// deadline can reach this from a background scan long after the group that made it ended —
    /// and a unit run must never take a real inventory of the machine it is running on.
    /// Production never sets either of these.
    static var safeCloseInventoryFallbackForTesting: (() -> Snapshot)?

    static func safeCloseInventory() -> Snapshot {
        safeCloseInventoryForTesting?() ?? safeCloseInventoryFallbackForTesting?() ?? snapshot()
    }

    /// Require a fresh, complete inventory to preserve the exact terminal id/backend/tty tuple.
    /// The rule belongs to the safe-close lifecycle; see
    /// ``TerminalSafeClose/stableTerminal(_:in:allowAssistantGone:)``.
    static func stableTerminal(_ expected: TargetSession,
                               in snapshot: Snapshot,
                               allowAssistantGone: Bool = false)
        -> Result<TargetSession, TerminalFailure> {
        TerminalSafeClose.stableTerminal(expected, in: snapshot,
                                         allowAssistantGone: allowAssistantGone)
    }

    /// Fresh screen classification performed in the broker immediately before an automatic
    /// linger decision. Capture failure is `unknown`, never idle and never permission to close.
    static func safeCloseActivity(of session: TargetSession) -> SafeCloseActivity {
        safeCloseActivity(of: session, screen: safeCloseScreen(of: session))
    }

    /// One capture shared by every decision made at a safe-close instant. Keeping the test seam
    /// here prevents a recovery check from silently bypassing the classifier's observed screen.
    static func safeCloseScreen(of session: TargetSession) -> String? {
        safeCloseCaptureForTesting?(session) ?? capture(session)
    }

    static func safeCloseActivity(of session: TargetSession,
                                  screen: String?) -> SafeCloseActivity {
        guard session.assistant != nil else { return .idle }
        switch SessionState.read(screen, assistant: session.assistant ?? .claude,
                                 hookWaiting: true) {
        case .working, .waiting: return .busy
        case .idle: return .idle
        case .unknown: return .unknown
        }
    }

    // W2-3 correction, F1: this used to switch on `session.backend` and call `ITerm.send`/
    // `Tmux.send` itself. `MacTerminalHost.sendLine` in `Sources/MacHostAdapters.swift` now does
    // that switch instead — the same move `close(_:)` already made — so this, a real production
    // path every plain-text send in the app goes through, runs on the port rather than around it.
    static func send(_ text: String, to session: TargetSession) -> String? {
        do {
            return try HostPorts.mac.terminal.sendLine(text, to: session)
        } catch let unavailable as HostCapabilityUnavailable {
            return unavailable.message
        } catch {
            return String(describing: error)
        }
    }

    /// Send a prompt whose images go over as images rather than as paths.
    ///
    /// Claude Code turns an image on the system pasteboard into `[Image #3]` when it receives a
    /// Ctrl-V — the byte 0x16, as a keystroke. So each image is lent to the pasteboard, the byte
    /// is sent **outside** the bracketed paste (inside one it is just a character), and the
    /// pasteboard is handed back.
    ///
    /// Two conditions, both of them about not making things worse:
    ///
    /// - **Only into a Claude Code session.** In a shell, Ctrl-V is readline's quoted-insert and
    ///   would put a control character in the command line. `isClaude` already knows — and it is
    ///   Claude Code specifically rather than any assistant, because `[Image #3]` is its
    ///   convention and nothing says Codex reads the same byte the same way.
    /// - **Only when the image loads.** Anything that fails falls back to its path, which is
    ///   what this did before and is never wrong, only plainer.
    ///
    /// A pause between pieces because the other end is a program reading a tty: the paste and
    /// the keystroke arrive as bytes in order, but the clipboard is read on the far side when
    /// the keystroke is handled, and that is not the same instant it arrives.
    static func send(_ pieces: [Drop.Piece], to session: TargetSession) -> String? {
        let asPath = pieces.map { piece -> String in
            if case .image(let path) = piece { return Drop.quoted(path) }
            if case .text(let text) = piece { return text }
            return ""
        }.joined()

        let images = pieces.contains { if case .image = $0 { return true }; return false }
        guard images, session.isClaude, Config.shared.sendImagesAsPaste else {
            return send(asPath, to: session)
        }

        // Borrowed once for the whole send and handed back at the end, rather than around each
        // image: the pasteboard is one shared thing, and putting it back between two images only
        // to take it again is churn nobody benefits from.
        let pasteboard = NSPasteboard.general
        let saved = Drop.contents(of: pasteboard)
        var failed: [String] = []
        var problem: String?

        for piece in pieces {
            switch piece {
            case .text(let text):
                guard !text.isEmpty else { continue }
                problem = paste(text, to: session, submit: false)
            case .image(let path) where Drop.offer(path, on: pasteboard):
                problem = keystroke(0x16, to: session)
                // The bytes and the keystroke arrive in order, but the far side reads the
                // clipboard when it handles the key, and that is not the instant it arrives.
                usleep(250_000)
            case .image(let path):
                // Its bytes could not be read. The path still works and is only plainer, which
                // beats a sentence pointing at nothing.
                failed.append(path)
                problem = paste(Drop.quoted(path), to: session, submit: false)
            }
            if let problem { Drop.put(saved, on: pasteboard); return problem }
        }

        let err = submit(to: session)
        // After the Enter, not before: the last image is still being read on the other side.
        usleep(200_000)
        Drop.put(saved, on: pasteboard)
        if !failed.isEmpty { Log.write("send: \(failed.count) image(s) went as paths") }
        return err
    }

    private static func paste(_ text: String, to session: TargetSession, submit: Bool) -> String? {
        switch session.backend {
        case .iterm: return ITerm.send(text, to: session.id, submit: submit)
        case .tmux:  return Tmux.send(text, to: session.id, submit: submit)
        }
    }

    /// Answer a menu, or send the one escape sequence this app names as a key.
    ///
    /// Claude Code's `AskUserQuestion` picker takes a bare digit **outside a bracketed paste** and
    /// treats it as a selection — in a single-select it also confirms, in a multi-select it
    /// toggles and `Tab` moves on to the review. That is why this exists at all and why it is a
    /// key rather than a string: `send` wraps its text in a bracketed paste, and **the picker
    /// throws the whole paste away and then acts on the Return that follows it**.
    ///
    /// Codex's dialogs answer the same way, which was checked rather than assumed: `2` on its
    /// trust dialog picks "No, quit" and the process is gone before the next capture.
    ///
    /// **Allowlisted here rather than at the route**, because the danger is not that somebody
    /// answers the wrong question — it is that a byte channel into a tty is an escape-sequence
    /// channel into a tty. `1`–`9` and `Tab` answer a menu and can do nothing else; the only
    /// sequence admitted is back-tab, which Claude Code uses to cycle permission modes. Keeping
    /// it whole also matters: three HTTP round trips could interleave with somebody typing and
    /// turn an intended key into three unrelated bytes.
    static func answer(_ byte: UInt8, to session: TargetSession) -> String? {
        answer([byte], to: session)
    }

    static func answer(_ bytes: [UInt8], to session: TargetSession) -> String? {
        let digit = bytes.count == 1 && (0x31...0x39).contains(bytes[0])
        let menuKey = digit || (bytes.count == 1 && bytes[0] == 0x09)
        let backTab: [UInt8] = [0x1b, 0x5b, 0x5a]       // ESC [ Z
        guard menuKey || bytes == backTab else {
            return "That is not a key this can send."
        }
        guard digit else { return keystroke(bytes, to: session) }
        let want = Int(bytes[0] - 0x30)

        // **Which kind of picker is this, before anything is typed at it.** A dialog drawn without
        // numbers has numeric selection switched off in the same breath, so the digit is not
        // ignored politely — it falls through the dialog and is typed into the composer
        // underneath. Reading first costs one capture on a path somebody is waiting on anyway.
        //
        // A capture that fails, or a screen that no longer parses, takes the numbered path: that
        // is what this did before this branch existed, and a dialog the reader is looking at right
        // now is far more likely to be the common shape than a shape nothing could read.
        let screen = capture(session)
        let seen = screen.flatMap {
            SessionState.menu($0, assistant: session.assistant ?? .claude, hookWaiting: true)
        }
        // **This path had no record of itself, and that is what let it fail silently.** Answering
        // from a phone is one HTTP call whose whole effect happens on somebody else's screen: the
        // caller is told the keystroke was delivered, which is true, and nothing anywhere says
        // what the app decided to do after that. On 2026-09-05 a tap that reached the tty and
        // stopped there was indistinguishable in the log from one that answered the question, and
        // it took a person tapping twice and a picker sitting for six minutes to notice. A menu
        // is answered a few times an hour, so this costs nothing and is the only account of the
        // decision that exists.
        Log.write("answer: want=\(want) read=\(seen == nil ? "no" : "yes")"
                  + (seen.map { " numbered=\($0.numbered) caret=\($0.selected.map(String.init) ?? "-")"
                                + " steps=\($0.steps.count) submit=\($0.submit == nil ? "no" : "yes")" }
                     ?? " — " + (screen.map(shape(of:)) ?? "nothing was captured")))
        if let menu = seen, !menu.numbered { return highlight(row: want, of: menu, on: session) }
        if let failure = keystroke(bytes, to: session) { return failure }

        // **A multi-select has nothing to confirm, and confirming it undoes the tap.** Its digits
        // toggle a row where a single-select's move the highlight, so the Return that commits one
        // is, on the other, a second press of the same row. Measured the hard way: the caret starts
        // on the first option, so tapping the first option was the one tap that reliably did
        // nothing — toggled by the digit and toggled straight back by the confirmation. Its rows
        // are sent by the button under them, and that is ``submitMenu(on:)``'s job.
        if seen?.submit != nil { return nil }
        return confirmSelection(want, on: session, asked: seen)
    }

    /// What a screen looked like to a parser that could not read it.
    ///
    /// **"That screen no longer reads as a menu" names a verdict without naming its evidence.**
    /// The same capture read correctly a fraction of a second earlier, so the interesting fact is
    /// not that it failed — it is which way the screen differs from the one that worked. These
    /// four separate the shapes that failure comes in: a read that came back short, a screen that
    /// scrolled, a dialog that closed, and rows that are still there under something new.
    private static func shape(of screen: String) -> String {
        let plain = Ansi.plain(screen)
        let lines = plain.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        let filled = lines.filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
        let last = filled.last.map { String($0.trimmingCharacters(in: .whitespaces).prefix(28)) }
        return "bytes=\(screen.utf8.count) lines=\(lines.count) filled=\(filled.count)"
            + " last=\(last.map { "\u{22}\($0)\u{22}" } ?? "-")"
    }

    /// Answer a picker that does not take digits, by moving its highlight onto the row and
    /// confirming there.
    ///
    /// Claude Code binds `j` and `k` to a select's next and previous row alongside the arrow keys,
    /// and `Return` still accepts — the `hideIndexes` flag turns off numeric selection and nothing
    /// else. Two plain letters are deliberately preferred over `ESC [ B`: this app has one place
    /// where bytes reach a tty and the argument for it is that none of them are escape sequences.
    /// A stray `j` in a composer is a letter somebody can see and delete.
    ///
    /// **Nothing is confirmed on faith.** The walk ends in ``confirmSelection(_:on:)`` like every
    /// other answer, so if the reading was wrong and this is not a picker at all, the highlight
    /// never lands, no Return is sent, and the failure is a button that did nothing rather than an
    /// answer nobody chose.
    private static func highlight(row want: Int, of menu: SessionState.Menu,
                                  on session: TargetSession) -> String? {
        guard let here = menu.selected else { return nil }
        let move = walk(from: here, to: want)
        for _ in 0..<move.times {
            if let failure = keystroke([move.key], to: session) { return failure }
        }
        return confirmSelection(want, on: session, asked: menu)
    }

    /// The keystroke that walks a highlight from one row to another, and how many of them.
    /// Split out from the sending so the arithmetic can be checked without a terminal.
    static func walk(from here: Int, to want: Int) -> (key: UInt8, times: Int) {
        (key: want >= here ? 0x6a : 0x6b, times: abs(want - here))   // j / k
    }

    /// Press the button a multi-select draws under its rows.
    ///
    /// **A multi-select does not answer on a digit.** Its numbers toggle rows, Return on a row
    /// toggles that row too, and the only thing that sends is Return while the caret is on the
    /// button below the list. So this is not a keystroke the phone can name — it is a short walk
    /// this end has to make, one row at a time, reading the screen after each step.
    ///
    /// **The step is Tab, and `j` was wrong.** `j` is bound to "next row" and looked like the
    /// obvious choice; measured on 2026-08-26 against a real dialog, it walked four rows and then
    /// typed `jjjj` into the question's own text box. The last row of an `AskUserQuestion` is
    /// `Type something`, an input, and a focused input passes only a named few keys through —
    /// `up`, `down`, `escape`, `tab`, `return` — and swallows everything else as typing. Tab is on
    /// that list, moves to the next row, lands on the button from the last one, **and does nothing
    /// once it is there**, so overshooting is not a failure mode it has. It was already the one
    /// non-digit key this app was allowed to send.
    ///
    /// The check still comes before each step rather than after, and the loop is bounded by the
    /// rows it can see plus two, so a dialog that stops responding costs a few captures and then
    /// gives up with the screen as it was.
    static func submitMenu(on session: TargetSession) -> String? {
        var steps = 0
        while true {
            guard let screen = capture(session),
                  let menu = SessionState.menu(screen, assistant: session.assistant ?? .claude,
                                               hookWaiting: true) else {
                return "Could not read that session's screen."
            }
            guard let submit = menu.submit else {
                return "That question has no Submit to press."
            }
            if submit.selected { return keystroke(13, to: session) }
            guard steps <= menu.options.count + 1 else {
                return "The highlight would not move onto Submit."
            }
            if let failure = keystroke([submitStep], to: session) { return failure }
            steps += 1
            Thread.sleep(forTimeInterval: 0.12)
        }
    }

    /// The key that walks a multi-select's focus toward its button — see ``submitMenu(on:)`` for
    /// why it is this one and not the row-navigation key it looks like it should be.
    static let submitStep: UInt8 = 0x09   // Tab

    /// Press Return, but only once the screen shows the digit landed where it was meant to.
    ///
    /// **A digit no longer confirms.** The comment above described what the picker used to do and
    /// measurement caught up with it: sending `3` moves the highlight to the third row and leaves
    /// the dialog open, sending `1` moves it back, and nothing is ever submitted. Someone
    /// answering from a phone saw the tap do nothing at all, pressed again, and the transcript
    /// recorded no answer — the question had to be finished at the keyboard.
    ///
    /// **Confirming blind would be worse than the bug.** If a picker ever stops taking digits,
    /// the highlight does not move and a bare Return submits whatever row it happens to be
    /// sitting on — a wrong answer sent silently, where today there is merely no answer. So the
    /// screen is read back first, and Return goes only when the highlight is on the row that was
    /// asked for. Anything else — a capture that fails, a shape that no longer parses, a
    /// highlight somewhere else — leaves the dialog exactly as it was.
    ///
    /// Two reads, because a terminal repaints on its own schedule and the first can arrive before
    /// the redraw. Both are cheap next to the round trip that has already happened.
    private static func confirmSelection(_ want: Int, on session: TargetSession,
                                         asked: SessionState.Menu?) -> String? {
        for attempt in 0..<2 {
            Thread.sleep(forTimeInterval: attempt == 0 ? 0.12 : 0.25)
            guard let screen = capture(session) else {
                Log.write("answer: confirm \(attempt) — the screen could not be captured")
                continue
            }
            // `hookWaiting` is true here because this path only exists behind a menu the app
            // already drew buttons for: the reader pressed one of them a moment ago. The gate
            // guards against calling a screen a menu unprompted, which is not this.
            guard let menu = SessionState.menu(screen, assistant: session.assistant ?? .claude,
                                               hookWaiting: true) else {
                // **No menu is two different answers wearing one sentence.** The picker may have
                // closed because the digit answered the question — the common, correct outcome on
                // an ordinary row — or the screen may have become unreadable, which is the bug
                // this logging was added to find. Read on 2026-09-05 as the second when it was
                // the first, and the log then said an answer had been given up on seconds after
                // it landed. The shape is printed either way; the sentence no longer guesses.
                Log.write("answer: confirm \(attempt) — no menu on that screen; it was either"
                          + " answered or is unreadable — \(shape(of: screen))")
                continue
            }
            let verdict = confirmation(want: want, asked: asked, now: menu)
            Log.write("answer: confirm \(attempt) — \(verdict) (caret="
                      + "\(menu.selected.map(String.init) ?? "-") want=\(want))")
            switch verdict {
            case .send: return keystroke(13, to: session)
            case .movedOn: return nil
            case .notYet: continue
            }
        }
        // Two readings and neither could commit. **This is not necessarily a failure**: a digit
        // that answered its question outright leaves no picker to confirm, and that path arrives
        // here too. It is said out loud because the caller is about to be told the keystroke was
        // delivered, and this is the one line that distinguishes "delivered and finished" from
        // "delivered and stranded" — which the two readings above have already described.
        Log.write("answer: sent no Return for want=\(want); see the two readings above")
        return nil
    }

    /// What to do with the screen a moment after a digit was typed at a picker.
    enum Confirmation: Equatable {
        /// The same question, with the wanted row highlighted: Return commits it.
        case send
        /// A different question is up. **The digit already answered, and the picker moved on.**
        case movedOn
        /// The same question, not there yet — look again.
        case notYet
    }

    /// **Why a moved-on picker must not be confirmed.** `AskUserQuestion` can ask a set at once,
    /// and at a row drawn without a preview a digit both answers the current question and puts
    /// the next one up — which is half of the shape, the other half being in
    /// ``confirmation(want:asked:now:)``. A confirming Return
    /// then lands on a question nobody has read, and lands on its *default* row — which for row 1
    /// is exactly the row `menu.selected == want` was checking for, so the guard that was supposed
    /// to make this safe agreed with it.
    ///
    /// Measured 2026-09-02 against a real two-question call: one tap on the first option answered
    /// both questions and went straight to the review screen. The second answer was never chosen
    /// by anybody, and the reader never saw the question. Answering with any other digit hid it —
    /// the highlight did not match, so no Return was sent and the picker looked like it worked.
    static func confirmation(want: Int, asked: SessionState.Menu?,
                             now: SessionState.Menu) -> Confirmation {
        // **Nothing to compare against.** A lone question confirms the way it always did. A set
        // is refused on its own evidence: the reading taken before the keystroke can fail, and a
        // missing `asked` must not be the thing that lets a Return through onto a question
        // nobody has read.
        guard let asked else {
            return now.steps.isEmpty ? (now.selected == want ? .send : .notYet) : .movedOn
        }
        // **"One of a set" was read as "the digit already answered", and that is not a fact
        // about the set.** It is a fact about the question: at an ordinary row a digit answers
        // and puts the next question up, and at a row carrying a `preview` it moves the
        // highlight and stops there, under a hint that says `Enter to select`. One
        // `AskUserQuestion` call draws both shapes, so no reading of the tab bar can be true of
        // both — and withholding the Return from the question still waiting for it is how an
        // answer sent from a phone arrived nowhere. Measured 2026-09-05 against Claude Code
        // v2.1.261, after a picker sat unanswered for six minutes with its receipt already
        // delivered.
        //
        // **The bar is the evidence, because it moves for exactly one of the two.** A digit that
        // answered ticks its question off; a digit that only moved a highlight leaves every box
        // as it was. Compared whole rather than counted, so anything the set does — a tick, a
        // question arriving, the review taking its place — reads as having moved on.
        if now.steps.map(\.answered) != asked.steps.map(\.answered) { return .movedOn }
        if now.question != asked.question { return .movedOn }
        return now.selected == want ? .send : .notYet
    }

    /// Whether this session is showing a menu right now, which changes what typing into it means.
    ///
    /// A capture, so only ask when it matters. See the `/send` route: a session that is merely
    /// `waiting` is the common case and answering it with words is correct; a session showing a
    /// *picker* is the case where words are silently discarded and the Return confirms whatever
    /// happens to be highlighted.
    static func isChoosing(_ session: TargetSession) -> Bool {
        guard let screen = capture(session) else { return false }
        return SessionState.isChoosing(screen, assistant: session.assistant ?? .claude)
    }

    /// End a session and close the tab it was in: the quit word, a wait that escalates to TERM
    /// and KILL on one proven process, and a close only once a fresh exact-tty reading shows the
    /// assistant gone.
    ///
    /// The lifecycle is ``TerminalSafeClose/end(_:ports:)`` running on this Mac's ports
    /// (``HostPorts/mac``), and why each step is there is written beside it. What this returns is
    /// the sentence it has always returned, so the orchestrator and the `/end` route read it as
    /// before.
    static func end(_ session: TargetSession) -> String? {
        TerminalSafeClose.end(session, ports: .mac)?.message
    }

    /// The policy that decides when to stop being polite. It is the lifecycle's; this is its name
    /// on the facade.
    typealias Farewell = TerminalSafeClose.Farewell

    /// The wait-and-escalate half of ``end(_:)`` on this Mac's ports, for the suites that drive it
    /// through the `safeClose…ForTesting` seams above.
    static func waitToBeGoneForTesting(_ session: TargetSession) -> String? {
        TerminalSafeClose.waitToBeGone(session, ports: seamAdaptedPorts)?.message
    }

    /// Close a shell-only tab only after a fresh exact-tty process observation proves there is no
    /// assistant left in it. See ``TerminalSafeClose/closeIfAssistantGone(_:ports:)``.
    static func closeIfAssistantGone(_ session: TargetSession) -> String? {
        TerminalSafeClose.closeIfAssistantGone(session, ports: seamAdaptedPorts)?.message
    }

    private static func keystroke(_ bytes: [UInt8], to session: TargetSession) -> String? {
        do {
            return try HostPorts.mac.terminal.interrupt(bytes, to: session)
        } catch let unavailable as HostCapabilityUnavailable {
            return unavailable.message
        } catch {
            return String(describing: error)
        }
    }

    private static func keystroke(_ byte: UInt8, to session: TargetSession) -> String? {
        keystroke([byte], to: session)
    }

    private static func submit(to session: TargetSession) -> String? {
        switch session.backend {
        case .iterm: return ITerm.submit(session.id)
        case .tmux:  return Tmux.submit(session.id)
        }
    }

    // Starting a session is deliberately **not** here. It used to be — `create(cwd:command:)`,
    // which ran whatever it was handed wherever it was pointed — and that made it a general
    // "run this there" primitive one hop from an HTTP route. Every other function in this file
    // acts on a session somebody already opened; that one created execution, which is a different
    // kind of thing and now lives in ``StartPoints`` with the policy that decides what may be
    // started and where. The new session is not in any snapshot yet either way: the caller gets
    // an id and waits for the next reading like everybody else, because returning something
    // half-filled would be a third kind of `TargetSession` that is true for about a second.

    /// What that session shows **now**, which is what everything reading a screen to decide
    /// something wants.
    ///
    /// **This used to carry ``Tmux/capture(_:scrollback:)``'s two hundred lines of history, and
    /// that was the wrong default for eight of its nine callers.** Every one of them is asking a
    /// question about the present — is this session busy, is a menu on screen, is the highlight
    /// on the row that was asked for, has the composer appeared — and history answers a different
    /// question with the same words. Most are protected by accident: ``SessionState/menu`` reads
    /// the last thirty non-empty lines and ``Activity/parse`` the last twenty-five, so scrollback
    /// is inert while the current screen fills them. ``Orchestrator/briefingInputReady(_:assistant:)``
    /// is the one that is not — it looks for a bare `❯` anywhere in the text it is handed — so a
    /// composer that scrolled away hours ago said "this session is ready for its briefing" while
    /// the session in front of it was still starting up. That could only ever happen on tmux:
    /// iTerm2 exposes the visible screen and no more, so the same code was safe on one backend
    /// and not on the other, which is the asymmetry rather than the number being wrong.
    ///
    /// History is not free either — it crosses a pipe, is parsed, and reaches a phone — but that
    /// is the smaller half of the argument. See ``screenWithHistory(of:lines:)`` for the one
    /// caller that genuinely wants it.
    static func capture(_ session: TargetSession) -> String? {
        visibleScreen(of: session)
    }

    /// What is visible now, without tmux scrollback. A current mode cannot be read from history:
    /// after somebody cycles, an older status line is still true text and a false current answer.
    ///
    /// W2-3 correction, F1: routed through ``HostPorts/mac``'s new `capture` leaf instead of
    /// switching on `session.backend` here — the no-history reading is exactly what that port
    /// method promises, so every caller of ``capture(_:)`` (activity classification, menu
    /// answering, the composer-ready check) is now a real path running on the port.
    static func visibleScreen(of session: TargetSession) -> String? {
        try? HostPorts.mac.terminal.capture(session)
    }

    /// The screen and what scrolled off the top of it, for the one thing that is showing a person
    /// a terminal rather than deciding something from it: the panel's output view.
    ///
    /// It is the only caller for which the wall in `docs/screen-tail.md` is the point — iTerm2
    /// hands over sixty rows and no more, tmux keeps history — so it is the only one that should
    /// pay for the extra lines. On iTerm2 it is the visible screen either way; there is no
    /// scrollback to ask for.
    static func screenWithHistory(of session: TargetSession, lines: Int = 200) -> String? {
        switch session.backend {
        case .iterm: return ITerm.capture(session.id)
        case .tmux:  return Tmux.capture(session.id, scrollback: lines)
        }
    }

    /// What each of these sessions is doing, keyed by session id.
    ///
    /// Batched per backend rather than session by session, because the cost here is round trips
    /// and not text: iTerm2 answers for all of them in one osascript run, and tmux answers for
    /// all of them in one `source-file` — `capture-pane` has no plural, but tmux takes a whole
    /// script at once. See ``Tmux/capture(panes:scrollback:)``.
    ///
    /// A session that could not be read comes back as `.unknown`, which is why the map is filled
    /// in for every session asked about rather than only the ones that answered — a missing key
    /// and a session that is doing nothing must not look the same to the caller.
    static func states(of sessions: [TargetSession]) -> [String: SessionState] {
        reading(of: sessions).states
    }

    /// What was on each screen, keyed by session id: the state, and the menu if there was one.
    ///
    /// **The menu comes out of the same capture, which is the only reason it is affordable.**
    /// Reading a menu used to mean a second round trip to the terminal — `isChoosing(_ session:)`
    /// still is one, and is still right for the single question `/send` asks before it refuses.
    /// But the phone needs the options on *every* beat, for every waiting session, and paying an
    /// osascript per session per second for that would have been the most expensive thing in the
    /// app. These screens have already been fetched. Parsing them twice costs nothing.
    struct Reading {
        var states: [String: SessionState] = [:]
        /// Only the sessions actually showing one, so a missing key means "no menu" and never
        /// "not looked at" — every session asked about gets a state, and most get no menu.
        var menus: [String: SessionState.Menu] = [:]
    }

    /// `hookWaiting` is the sessions something outside the screen says are stopped on a question
    /// — a hook note, or Claude Code's own registry entry, which agree about what the fact means
    /// and only differ in where it came from. It opens one parsing gate and asserts nothing: see
    /// ``SessionState/menu(_:assistant:tailLines:hookWaiting:)``.
    static func reading(of sessions: [TargetSession], hookWaiting: Set<String> = []) -> Reading {
        var out = Reading()

        func note(_ session: TargetSession, _ screen: String?) {
            let assistant = session.assistant ?? .claude
            let gateOpen = hookWaiting.contains(session.id)
            out.states[session.id] = SessionState.read(screen, assistant: assistant,
                                                       hookWaiting: gateOpen)
            // Only when the screen says so. Opening the gate lets that same screen safely count a
            // flush-left caret as a selection; it never supplies `.waiting` in the screen's place.
            guard out.states[session.id] == .waiting, let screen else { return }
            out.menus[session.id] = SessionState.menu(Ansi.plain(screen), assistant: assistant,
                                                      hookWaiting: gateOpen)
        }

        let iterm = sessions.filter { $0.backend == .iterm }
        if !iterm.isEmpty {
            let tails = ITerm.tails(ids: iterm.map { $0.id })
            for session in iterm { note(session, tails[session.id]) }
        }
        // One subprocess for all of them, the same shape as the iTerm2 branch above. Only the
        // visible pane: `-S -0` starts at the top of the screen rather than in the scrollback,
        // which is both cheaper and the right question — what is on screen *now*. A pane that
        // would not answer is missing from the map and becomes `.unknown` through `note`, which
        // is what one failing `capture-pane` cost when each pane had a subprocess of its own.
        let tmux = sessions.filter { $0.backend == .tmux }
        if !tmux.isEmpty {
            let screens = Tmux.capture(panes: tmux.map(\.id), scrollback: 0)
            for session in tmux { note(session, screens[session.id]) }
        }
        return out
    }

    /// Where that session is working. tmux hands it over with the pane list; for iTerm2 it
    /// has to be asked of the process, so it is only looked up when something needs it.
    private static let cwdLock = NSLock()
    private static var cwdCache: [String: (at: CFAbsoluteTime, path: String)] = [:]

    /// Where a session is working.
    ///
    /// Remembered for a while, because a Claude Code session does not move: it is started in a
    /// directory and stays there. Asking again every second cost a process listing and an
    /// `lsof` for an answer that had not changed since the session began.
    static func workingDirectory(of session: TargetSession) -> String? {
        if let cwd = session.cwd, !cwd.isEmpty { return cwd }
        cwdLock.lock()
        if let hit = cwdCache[session.id], CFAbsoluteTimeGetCurrent() - hit.at < 20 {
            defer { cwdLock.unlock() }
            return hit.path
        }
        cwdLock.unlock()

        let bare = session.tty.replacingOccurrences(of: "/dev/", with: "")
        guard let pid = ITerm.assistantPIDs()[bare]?.pid,
              let path = ITerm.workingDirectory(ofPID: pid) else { return nil }
        cwdLock.lock()
        cwdCache[session.id] = (CFAbsoluteTimeGetCurrent(), path)
        cwdLock.unlock()
        return path
    }

    /// When the assistant in this session started. Used to tell its record from the records of
    /// every other session in the same project.
    ///
    /// Remembered for the same reason and the same while as the working directory: it is asked
    /// once per session on every move through the list, it costs a `ps` of its own on top of the
    /// one that finds the pid, and the answer it gives is a fact about a process that has already
    /// started. Held rather than kept forever, because a tab whose session is restarted keeps its
    /// id and gets a new start time.
    private static var startCache: [String: (at: CFAbsoluteTime, started: Date?)] = [:]

    /// The process running in this session, when there is one this can see.
    ///
    /// The tty is the only link between a pane and a process, which is why this exists at all
    /// and why it answers nothing for a session in a shell. Read off the same cached `ps` as
    /// everything else on this path.
    static func pid(of session: TargetSession) -> Int32? {
        let bare = session.tty.replacingOccurrences(of: "/dev/", with: "")
        return ITerm.assistantPIDs()[bare]?.pid
    }

    static func processStart(of session: TargetSession) -> Date? {
        cwdLock.lock()
        if let hit = startCache[session.id], let started = hit.started,
           CFAbsoluteTimeGetCurrent() - hit.at < 20 {
            defer { cwdLock.unlock() }
            return started
        }
        cwdLock.unlock()

        let bare = session.tty.replacingOccurrences(of: "/dev/", with: "")
        let started = ITerm.assistantPIDs()[bare].flatMap { ITerm.processStart(ofPID: $0.pid) }
        cwdLock.lock()
        // Absence is a startup observation, not a process fact. Caching it for twenty seconds
        // can cover the entire briefing window and silence every registry lookup in it.
        if started != nil { startCache[session.id] = (CFAbsoluteTimeGetCurrent(), started) }
        cwdLock.unlock()
        return started
    }

    /// The pid-keyed start cache, used for background sessions and callers that already fixed
    /// the foreground pid so the terminal cannot contribute a mismatched start time.
    ///
    /// Keyed by pid because that is all there is to key it by: the background session behind a
    /// parked tab is running under the daemon, on no terminal, and the only thing naming it is
    /// the number in its file's name. Without this the poll would pay a `ps` a second for every
    /// tab somebody has parked — which is the one cost that would have made following a park
    /// expensive, since everything else about it is a directory listing.
    private static var bgStartCache: [Int32: (at: CFAbsoluteTime, started: Date?)] = [:]

    static func processStart(ofPID pid: Int32) -> Date? {
        cwdLock.lock()
        if let hit = bgStartCache[pid], let started = hit.started,
           CFAbsoluteTimeGetCurrent() - hit.at < 20 {
            defer { cwdLock.unlock() }
            return started
        }
        cwdLock.unlock()

        let started = ITerm.processStart(ofPID: pid)
        cwdLock.lock()
        // Held for the same twenty seconds as a tab's, and for the same reason: a number can be
        // handed to a different process, and that is what the start time being cached is about.
        // Swept on the way past because this one is keyed by pid, so it grows with every
        // background session the machine has ever run rather than with the tabs that are open.
        let now = CFAbsoluteTimeGetCurrent()
        bgStartCache = bgStartCache.filter { now - $0.value.at < 20 }
        if started != nil { bgStartCache[pid] = (now, started) }
        cwdLock.unlock()
        return started
    }

    /// What Claude Code says about these sessions, and the process facts that decide which of
    /// its files is allowed to speak for which session. See ``SessionRegistry``.
    ///
    /// **The two halves cost very different amounts, which is why they are read in this order.**
    /// The pid comes out of one `ps` shared by every session and cached for a couple of seconds.
    /// The start time is a `ps` of its own per session, so it is only asked for the sessions a
    /// registry file was actually found for — which is also the whole of why nothing in here
    /// names Codex. A Codex session writes no file, so it drops out at the same line a Claude
    /// Code session with no registry at all drops out at.
    static func registry(of sessions: [TargetSession],
                         processScan: ITerm.AssistantProcessScan? = nil)
        -> SessionRegistry.Reading {
        var pids: [String: Int32] = [:]
        var byID: [String: TargetSession] = [:]
        for session in sessions where session.isAssistant {
            byID[session.id] = session
            let bare = session.tty.replacingOccurrences(of: "/dev/", with: "")
            if let process = processScan?.assistants[bare],
               process.assistant == session.assistant {
                pids[session.id] = process.pid
            } else if processScan == nil, let pid = pid(of: session) {
                pids[session.id] = pid
            }
        }
        guard !pids.isEmpty else { return SessionRegistry.Reading() }

        var out = SessionRegistry.Reading()
        out.entries = SessionRegistry.entries(pids: Array(pids.values))
        for (id, pid) in pids where out.entries[pid] != nil {
            guard let session = byID[id] else { continue }
            let bare = session.tty.replacingOccurrences(of: "/dev/", with: "")
            let publishedStart = processScan?.assistants[bare].flatMap { process in
                process.assistant == session.assistant ? process.processStart : nil
            }
            out.processes[id] = SessionRegistry.Process(pid: pid,
                started: processScan == nil ? processStart(of: session) : publishedStart)
        }
        // A parked tab's file is about a conversation that has left it, so the other half of the
        // reading is where that conversation went. Free unless somebody has parked something:
        // with no parked file in hand this looks at nothing and runs no `ps`.
        SessionRegistry.attachBackground(to: &out) { processStart(ofPID: $0) }
        return out
    }

    /// Put this session in front of you — or, with `activate: false`, merely make it the one its
    /// terminal is showing.
    ///
    /// tmux was always the second kind, because tmux is not an application and has no front to
    /// move to. It has one now, in one case: when tmux's own client list says a control-mode
    /// client is attached to that pane's session, the application on the other end of that stream
    /// is iTerm2 and can be asked to come forward — and, since iTerm2 turns out to publish which
    /// of its tabs is drawing which tmux pane, to come forward on the right tab. So the flag is
    /// passed on rather than dropped — otherwise the prompt bar walking its list would haul
    /// iTerm2 in front of the box being typed into. See ``Tmux/reveal(_:activate:)``.
    ///
    /// **`activate: false` now moves the tab on both backends**, which it did not until somebody
    /// reported the difference: under tmux the prompt bar's walk selected a tmux window and
    /// stopped, and iTerm2 does not act on that, so the terminal underneath simply never followed
    /// what the bar was aimed at. It is the same courtesy in both cases — the tab is named, the
    /// keyboard is left in the box being typed into — and it costs one Apple Event in both cases,
    /// because the identity check that is four round trips is the permission to *raise* an
    /// application and nothing here raises one. See ``Tmux/followMirrorTab(_:)``.
    ///
    /// W2-3 correction, F1: routed through ``HostPorts/mac``'s new `reveal` leaf, which throws
    /// the backend's own ``TerminalFailure`` — including its `iTermAttention` kind — rather than
    /// flattening it, so this keeps returning the exact structured failure every existing caller
    /// of `reveal(_:activate:)` already switches on.
    @discardableResult
    static func reveal(_ session: TargetSession, activate: Bool = true) -> TerminalFailure? {
        do {
            try HostPorts.mac.terminal.reveal(session, activate: activate)
            return nil
        } catch let failure as TerminalFailure {
            return failure
        } catch let unavailable as HostCapabilityUnavailable {
            return TerminalFailure(kind: .io, message: unavailable.message)
        } catch {
            return TerminalFailure(kind: .io, message: String(describing: error))
        }
    }
}
