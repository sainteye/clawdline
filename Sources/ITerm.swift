import Foundation

/// Somewhere text can be sent.
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

    /// The tab's own title, tidied — **for looking at, not for naming a session by.**
    ///
    /// Two things get taken off. iTerm appends " (job name)", which helps nobody pick a tab. And
    /// Claude Code puts a status glyph on the front — which used to be a fixed ✳ and was worth
    /// keeping as a marker, and is now **a frame of an animation**: 2.1.228 cycles half circles
    /// through the title, so the same tab reads `◐ …`, `◑ …`, `◒ …` one after another. A label
    /// that changes four times a second is not a label, it is noise on every surface that draws
    /// one — and the thing it was standing in for is now answered properly by ``SessionState``.
    ///
    /// **``displayLabel`` deliberately does not use this**, and that is the whole of the fix
    /// recorded there. What is left is nothing in `Sources/`: after that fix this property has no
    /// caller outside the suite, which reads it to prove the tidying still works and that a row
    /// is *not* named from it. Said plainly because the first draft of this paragraph named two
    /// uses that do not exist — `Tmux.swift` never mentions `label`, and
    /// ``Transcript/locate(in:tabTitle:startedAt:sessionID:)`` is handed the raw `name` — and a
    /// list of live uses that are not live is an invitation to wire it back up.
    var label: String {
        var s = name.trimmingCharacters(in: .whitespacesAndNewlines)
        if s.hasSuffix(")"), let open = s.lastIndex(of: "("), open > s.startIndex {
            let before = s.index(before: open)
            if s[before] == " " { s = String(s[s.startIndex..<before]) }
        }
        s = TargetSession.withoutStatusGlyph(s)
        s = s.trimmingCharacters(in: .whitespaces)
        return s.isEmpty ? coordinate : s
    }

    /// What Clawdline calls this session on every surface that lists one.
    ///
    /// **The tab title is not one of the sources, and that is the point.** It used to be the
    /// last one, and on 2026-08-28 that made eleven of fifteen rows read `Default` — the name of
    /// the iTerm2 profile, which is what iTerm2 reports for a tab whose title nobody has set.
    /// Asked directly, iTerm2 said `Default (python)` for those tabs, so the app was reading its
    /// source correctly; the source had simply been emptied. Meanwhile every one of those
    /// sessions had a name sitting in Claude Code's own files the whole time.
    ///
    /// A tab title is **a place a name is displayed**, not a place a name is kept: anything in
    /// the terminal may overwrite it, Claude Code clears it, and nothing announces either. So the
    /// resolution below reads authoritative records only, and a terminal that has been renamed to
    /// `WRONG`, to `Default`, or to nothing at all cannot change a single row.
    var displayLabel: String {
        Self.preferredDisplayLabel(
            manualTitle: Config.shared.sessionTitle(for: self),
            orchestratorTitle: Orchestrator.title(forTerminal: id),
            conversationTitle: SessionNaming.title(of: self),
            threadName: CodexNaming.shared.title(for: self),
            handle: SessionNaming.handle(of: self),
            coordinate: coordinate)
    }

    /// The order, and why each rung is above the next. **No parameter here carries a terminal
    /// title**; there is nowhere to pass one, which is what keeps a later edit from quietly
    /// reintroducing the source this list exists to remove.
    ///
    /// 1. `manualTitle` — the name a person typed for this conversation, from
    ///    ``Config/sessionTitle(sessionID:terminalID:conversationStart:currentCustomTitle:)``.
    ///    The only human-authored source, and not a constant somebody set once: `Config` already
    ///    withholds it the moment a later `/rename` in the terminal supersedes it.
    /// 2. `orchestratorTitle` — the task this app opened the tab for, whichever assistant is in
    ///    it. The title was known before the tab existed and is what a list of work should say.
    /// 3. `conversationTitle` — what the conversation calls *itself*: the `customTitle` a
    ///    `/rename` wrote, or the `aiTitle` Claude Code wrote, out of the transcript under
    ///    `~/.claude/projects/`. This is the rung that was missing. Measured against the one tab
    ///    still showing a name on 2026-08-28: the screen read `Clawdfather 新增介面` and so did
    ///    the transcript's `aiTitle`, while the same session's registry `name` read
    ///    `clawdline-97`. The transcript is where the descriptive name lives.
    /// 4. `threadName` — Codex's persisted thread metadata, or the model fallback Clawdline keeps
    ///    for a Claude conversation whose first turn ended without an `aiTitle`. Either is
    ///    disjoint by assistant and yields immediately if the Claude transcript later gets a
    ///    `customTitle` or `aiTitle` on the rung above.
    /// 5. `handle` — `~/.claude/sessions/<pid>.json`'s `name`, `clawdline-cb` and the like.
    ///    **Never descriptive** — all eleven files read on 2026-08-28 said `nameSource:
    ///    "derived"` — but it is durable, it is per-conversation, and it names the project. It
    ///    is here because a session whose transcript has not been written yet still has one.
    /// 6. `coordinate` — where the tab is. Never a profile name, never a job name.
    static func preferredDisplayLabel(manualTitle: String?, orchestratorTitle: String?,
                                      conversationTitle: String?, threadName: String?,
                                      handle: String?, coordinate: String) -> String {
        for candidate in [manualTitle, orchestratorTitle, conversationTitle, threadName, handle] {
            guard let candidate = candidate?.trimmingCharacters(in: .whitespacesAndNewlines),
                  !candidate.isEmpty else { continue }
            return candidate
        }
        return coordinate
    }

    /// Strip a leading status glyph and the space after it.
    ///
    /// Recognised by *shape* rather than by a list of characters, for the same reason everything
    /// else here is: the glyphs changed once already and the list would have to be chased. What
    /// does not change is that it is one non-alphanumeric mark, on its own, in front of a title —
    /// so a single leading character from the symbol, punctuation or Braille blocks goes, and one
    /// that is followed by anything other than a space stays, because that is a title starting
    /// with a bullet rather than a marker in front of one.
    static func withoutStatusGlyph(_ title: String) -> String {
        var chars = Array(title)
        guard chars.count > 2, chars[1] == " " else { return title }
        guard let scalar = chars[0].unicodeScalars.first, chars[0].unicodeScalars.count == 1,
              !chars[0].isLetter, !chars[0].isNumber else { return title }
        // Braille (⠀–⣿), geometric shapes (◀–◿), dingbats and misc symbols — the blocks a
        // terminal spinner is drawn from. A quotation mark or a bracket is not one of them.
        let v = scalar.value
        let isMarker = (0x2800...0x28FF).contains(v)   // Braille
            || (0x25A0...0x25FF).contains(v)           // geometric shapes, incl. ◐◑◒◓
            || (0x2600...0x27BF).contains(v)           // misc symbols and dingbats, incl. ✳
            || (0x2B00...0x2BFF).contains(v)           // misc symbols and arrows
            || (0x1F300...0x1FAFF).contains(v)         // emoji
        guard isMarker else { return title }
        chars.removeFirst(2)
        return String(chars)
    }
}

/// What a Claude Code conversation calls itself, read out of the files Claude Code keeps rather
/// than off the terminal it happens to be sitting in.
///
/// **The defect this exists to close is a shape, not an incident.** A value a person reads had
/// exactly one source; that source could be emptied by something outside this app; and when it
/// was emptied nothing said so — the row simply started reading `Default`. Two rules follow, and
/// both are load-bearing here:
///
/// - **More than one source.** Four of them answer the same question independently: the registry
///   file names the conversation by pid, a `SessionStart` hook names it by tty, a hook-free
///   installation still has a transcript findable by process start time, and any of those alone
///   is enough. See ``look(at:startedAt:sources:)``.
/// - **A source going quiet is not news that the name has changed.** ``reconcile(remembered:found:startedAt:now:)``
///   keeps the last good answer field by field, and gives it up only on evidence that the tab now
///   holds a *different* conversation. A lookup that fails for a second — a `ps` that did not
///   answer, a registry file mid-rewrite — must not blank a name on screen.
///
/// A third rule was added after review, because the first two on their own bought a worse bug
/// than the one they closed:
///
/// - **No identity, no answer.** When nothing can say *which* conversation is in this tab, the
///   honest reply is a blank, not the project's most recently written transcript. `Default` on
///   eleven rows announced itself; somebody else's descriptive title on one row does not. See the
///   guard in ``look(at:startedAt:sources:)``.
enum SessionNaming {

    /// What one look found. Two fields because they fail independently: a session in its first
    /// seconds has a registry file and no transcript worth reading, and a session whose Claude
    /// Code predates the registry has the transcript and no file.
    struct Name: Equatable {
        /// The conversation's own descriptive title — a `/rename`'s `customTitle`, else the
        /// `aiTitle` Claude Code wrote for it.
        let title: String?
        /// Claude Code's derived short handle for the session, `clawdline-cb` and the like.
        /// Not a description of anything; kept because it is durable, per-conversation, and
        /// better than a coordinate for telling two rows apart.
        let handle: String?
        static let none = Name(title: nil, handle: nil)
        var isEmpty: Bool { title == nil && handle == nil }
    }

    /// A look, kept so the next redraw is free and so a momentary failure cannot blank a row.
    struct Remembered: Equatable {
        let at: Date
        /// When the assistant process behind that look started, as far as anything could tell.
        /// The only thing that can say the tab now holds a *different* conversation.
        let startedAt: Date?
        let name: Name
    }

    /// How long a look stands before it is taken again. Twenty seconds, the same as
    /// ``Targets/workingDirectory(of:)`` and ``Targets/processStart(of:)`` beside it, and for the
    /// same reason: these are asked once per session on every move through the list, and the
    /// answers are facts about a conversation that is not changing its name several times a
    /// second. It bounds the cost too — a busy transcript is re-read at most three times a
    /// minute rather than on every repaint.
    static let ttl: TimeInterval = 20

    private static let lock = NSLock()
    private static var remembered: [String: Remembered] = [:]

    /// The whole of the impure part: decide whether to look, look, reconcile, keep.
    private static func name(of target: TargetSession, now: Date = Date()) -> Name {
        // A tab with no Claude Code in it has no conversation to name, and a tab whose session
        // has exited must not keep wearing its name. Forgetting here rather than in `reconcile`
        // keeps that case out of the pure function, where "no assistant" and "the lookup failed"
        // would otherwise look identical.
        guard target.isClaude else {
            lock.lock(); remembered.removeValue(forKey: target.id); lock.unlock()
            return .none
        }
        lock.lock()
        let previous = remembered[target.id]
        lock.unlock()
        if let previous, now.timeIntervalSince(previous.at) < ttl { return previous.name }

        // The seam replaces the whole of the reading, the `ps` behind the start time included:
        // a suite has no terminal to measure and no business running one to find that out.
        let startedAt: Date?
        let found: Name
        if let stub = lookForTesting {
            startedAt = nil
            found = stub(target)
        } else {
            startedAt = Targets.processStart(of: target)
            found = look(at: target, startedAt: startedAt)
        }
        let kept = reconcile(remembered: previous, found: found, startedAt: startedAt, now: now)
        lock.lock()
        if let kept { remembered[target.id] = kept } else { remembered.removeValue(forKey: target.id) }
        lock.unlock()
        return kept?.name ?? .none
    }

    static func title(of target: TargetSession) -> String? { name(of: target).title }
    static func handle(of target: TargetSession) -> String? { name(of: target).handle }

    /// The three files a look reads, each behind a closure so that ``look(at:startedAt:sources:)``
    /// can be exercised without a live terminal, somebody's real `~/.claude`, or a clock.
    ///
    /// The seam is here rather than around the whole of the reading — which is what
    /// ``lookForTesting`` does, and why it could not answer this — because the reading is the
    /// part with the rules in it. With `look` itself stubbed out, gutting its body to `return
    /// .none` left the suite at the same 4987 passing checks: the feature's own headline claim
    /// had no assertion anywhere behind it.
    struct Sources {
        /// Claude Code's registry file for this tab's process, when there is one.
        var registryEntry: (TargetSession) -> SessionRegistry.Entry?
        /// The session id a `SessionStart` hook recorded against this tty, when one is installed.
        var hookSessionID: (TargetSession) -> String?
        /// The `~/.claude/projects/<slug>` directory this session's transcripts live in.
        var transcripts: (TargetSession) -> URL?

        static let live = Sources(
            registryEntry: { target in
                // Behind the same setting as every other read of that directory: somebody who
                // has turned the registry off has said not to read those files, and the routes
                // below still answer without them.
                Config.shared.sessionRegistry
                    ? SessionRegistry.entry(for: target.id, in: Targets.registry(of: [target]))
                    : nil
            },
            hookSessionID: { Config.shared.hookSessionID(of: $0) },
            transcripts: { target in
                Targets.workingDirectory(of: target).map { Transcript.projectDirectory(forCwd: $0) }
            })
    }

    /// Reading the files. Kept apart from the caching and the reconciling above so that the rules
    /// this type exists for can be tested without a live terminal, a real `~/.claude`, or a clock.
    ///
    /// **`tabTitle: ""` is the fix, stated where it is made.** ``Transcript/locate(in:tabTitle:startedAt:sessionID:)``
    /// will rank candidate transcripts by how well their recorded title matches the tab's, which
    /// is exactly the dependency being removed: a tab renamed to `Default` would go looking for a
    /// transcript called `Default`. Passing nothing skips that branch, leaving the routes that
    /// rest on identity — the session id, and failing that the process start time, which no
    /// terminal can rewrite.
    ///
    /// **And when none of those answer, this returns nothing rather than a ranked guess.** With
    /// no session id and no start time, `locate` has only `files.first` left — the project's most
    /// recently written transcript, which is how a brand-new tab once ended up showing somebody
    /// else's conversation with their project's name on it. That is not a hypothetical here:
    /// the id is absent whenever the registry is switched off, whenever Claude Code predates it,
    /// and through the whole window in which a child inherits a polluted environment and writes
    /// no registry file of its own; the start time is absent whenever its `ps` fails. A blank
    /// name falls back to the handle, or to the coordinate — visibly empty, which somebody can
    /// see is wrong. A stranger's title is a plausible-looking error, and nobody checks those.
    /// ``Transcript/sessionID(of:)`` drew the same line for identity one file over: *without a
    /// registry or hook naming one transcript, no ranked candidate may become somebody's id*.
    static func look(at target: TargetSession, startedAt: Date?,
                     sources: Sources = .live) -> Name {
        let entry = sources.registryEntry(target)
        let handle = entry?.name
        // Both precise sources, in the order ``Transcript/record(of:)`` asks them: the registry
        // is there whether or not anybody installed hooks, and the hook covers the installation
        // whose registry file this build cannot see.
        let sessionID = Transcript.namedClaudeSessionID(registry: entry?.sessionID,
                                                        hook: sources.hookSessionID(target))
        guard sessionID != nil || startedAt != nil else { return Name(title: nil, handle: handle) }
        guard let directory = sources.transcripts(target) else {
            return Name(title: nil, handle: handle)
        }
        guard let url = Transcript.locate(in: directory, tabTitle: "", startedAt: startedAt,
                                          sessionID: sessionID) else {
            return Name(title: nil, handle: handle)
        }
        return Name(title: Transcript.title(ofTranscript: url), handle: handle)
    }

    /// What survives a look, field by field.
    ///
    /// Field by field rather than whole, because the two halves come from two files and go quiet
    /// separately: a registry file rewritten between a `stat` and a read answers with a handle
    /// and no title, and replacing a remembered descriptive name with `clawdline-cb` would be
    /// this feature reintroducing its own bug at a smaller scale.
    ///
    /// Nothing is kept once the tab holds a different conversation — a name is a fact about a
    /// conversation, not about a tab, and a tab outlives what runs in it.
    static func reconcile(remembered previous: Remembered?, found: Name,
                          startedAt: Date?, now: Date) -> Remembered? {
        let carried = continues(previous, startedAt: startedAt) ? previous?.name : nil
        let merged = Name(title: found.title ?? carried?.title,
                          handle: found.handle ?? carried?.handle)
        guard !merged.isEmpty else { return nil }
        // The last start time known for this tab, so a look that could not measure one still
        // leaves a baseline for the look after it to be compared against.
        return Remembered(at: now, startedAt: startedAt ?? previous?.startedAt, name: merged)
    }

    /// Whether the conversation a remembered name was taken from is still the one in the tab.
    ///
    /// **Unknown is not "changed".** ``Config/sameConversation(_:_:)`` next door answers a
    /// stricter question — whether a name a person typed may be shown at all — and reads a
    /// missing start time as a mismatch. Here a missing one is a `ps` that did not answer, which
    /// is the ordinary way a source goes quiet for a moment and is the exact case that must not
    /// blank a row.
    ///
    /// **But the two missing halves are not the same missing.** *This* look failing to measure a
    /// start time is that momentary quiet. The *remembered* look having failed to measure one is
    /// a name with no baseline under it at all — it was written by a look that could not say
    /// which conversation it was about, and treating a measurement that has now arrived as
    /// "still the same one" would carry that name across a conversation change forever, since
    /// ``reconcile(remembered:found:startedAt:now:)`` only ever fills the baseline in once. So a
    /// start time arriving where there was none is a new baseline, not a continuation: the name
    /// is given up, and the look that has evidence writes the record from here on.
    static func continues(_ previous: Remembered?, startedAt: Date?) -> Bool {
        guard let previous else { return false }
        guard let known = previous.startedAt else { return startedAt == nil }
        guard let current = startedAt else { return true }
        return Config.sameConversation(known, current)
    }

    /// Drop what is remembered for tabs that are no longer on screen.
    ///
    /// A name is forgotten when the tab stops holding an assistant, but a tab that is *closed*
    /// is never asked about again, so its row sat in the table until the app quit.
    /// ``Orchestrator/pruneClosedHandoffTitles(visible:)`` is next door for the same reason and
    /// states the sharper half of it: a terminal id is reusable, so a name left behind under a
    /// closed tab's id is a name waiting to be handed to a later session.
    static func forget(closedFrom visible: Set<String>) {
        lock.lock(); defer { lock.unlock() }
        remembered = remembered.filter { visible.contains($0.key) }
    }

    /// Test seam for the caching and reconciling above, which are otherwise reachable only
    /// through a live terminal's `ps`, `lsof` and somebody's real `~/.claude`. **It replaces the
    /// whole of the reading**, ``look(at:startedAt:sources:)`` included, so a test that is about
    /// what the reading does goes through ``Sources`` instead.
    static var lookForTesting: ((TargetSession) -> Name)?

    static func forgetForTesting() {
        lock.lock(); remembered.removeAll(); lock.unlock()
    }

    /// Push every remembered look past ``ttl`` so the next question takes a fresh one. A test for
    /// "the source went quiet and the name stayed" has to get past the cache to reach the case.
    static func expireForTesting() {
        lock.lock()
        remembered = remembered.mapValues {
            Remembered(at: $0.at.addingTimeInterval(-ttl - 1), startedAt: $0.startedAt,
                       name: $0.name)
        }
        lock.unlock()
    }
}

enum ITerm {

    // MARK: - Subprocesses

    /// A failed Apple event is not an invitation to send another one. In practice the failure
    /// that matters is iTerm2 holding a confirmation sheet open: every later command stands
    /// behind the same modal answer and makes the recovery queue longer. The circuit stays open
    /// until `snapshot()` receives a well-formed list response, which is the first positive
    /// evidence that iTerm2 is accepting Apple events again. Process-list confidence is a
    /// separate question: a failed `ps` may make the inventory non-authoritative, but it is not
    /// evidence of an iTerm modal and must never arm this circuit.
    private static let automationLock = NSLock()
    private static var automationFailure: String?

    static var automationReady: Bool {
        automationLock.lock(); defer { automationLock.unlock() }
        return automationFailure == nil
    }

    static var automationAttention: String? {
        automationLock.lock(); defer { automationLock.unlock() }
        return automationFailure.map { failure in
            "iTerm2 needs attention on this Mac before terminal automation can continue. "
                + "Answer the iTerm2 dialog, then wait for Clawdline to complete a fresh terminal "
                + "inventory. Last failure: \(failure)"
        }
    }

    private static func blockAutomation(_ failure: String) {
        automationLock.lock()
        if automationFailure == nil { automationFailure = failure }
        automationLock.unlock()
    }

    private static func completeInventory() {
        automationLock.lock(); automationFailure = nil; automationLock.unlock()
    }

    static func blockAutomationForTesting(_ failure: String) { blockAutomation(failure) }
    static func completeInventoryForTesting() { completeInventory() }

    /// The complete set of evidence allowed to arm the automation circuit. Kept as a pure seam
    /// so tests can prove that process-scan confidence and cross-backend contradictions are not
    /// silently added to this safety boundary.
    static func automationCircuitEvidence(appleEventTimedOut: Bool,
                                          listRowsMalformed: Bool) -> Bool {
        appleEventTimedOut || listRowsMalformed
    }

    /// Somewhere for the reader thread to put what it read. A class, not a captured `var`,
    /// so the handoff across the semaphore is a reference and not a copy in flight.
    private final class Sink { var data = Data() }

    /// Run something and hand back what it printed, or admit that it never finished.
    ///
    /// **Nothing on this path is allowed to wait forever**, and the reason is one specific way
    /// the whole app used to stop dead. Taking a tab away while a job is still running in it
    /// makes iTerm2 put up a confirmation sheet; a sheet is modal, so the Apple event behind the
    /// request never comes back and `osascript` never exits. The remote server answers every
    /// request on one serial queue — so a single unanswered dialog on the Mac froze every page,
    /// every phone and the panel itself until somebody walked over and clicked it.
    ///
    /// ``Targets/end(_:)`` no longer provokes that sheet, but a profile set to always prompt
    /// still can, and so can a dialog this app had nothing to do with. A run that overruns its
    /// deadline is killed and reported as silence, which is what the caller can act on.
    private static func shell(_ path: String, _ args: [String],
                              timeout: TimeInterval = 15)
        -> (out: String, timedOut: Bool, status: Int32?) {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: path)
        p.arguments = args
        let out = Pipe()
        p.standardOutput = out
        p.standardError = Pipe()
        do { try p.run() } catch { return ("", false, nil) }
        // Read on another thread rather than here, because the deadline must not be waiting on
        // the pipe: a child stuck in an Apple event has written nothing and is not going to
        // close its end of it either.
        let sink = Sink()
        let done = DispatchSemaphore(value: 0)
        DispatchQueue.global(qos: .userInitiated).async {
            sink.data = out.fileHandleForReading.readDataToEndOfFile()
            done.signal()
        }
        if done.wait(timeout: .now() + timeout) == .timedOut {
            // Killing `osascript` does not cancel an Apple event iTerm2 has already been handed:
            // whatever was asked for still happens, once the person at the Mac answers the sheet.
            // What this buys is that nothing here is still holding the queue while they decide.
            p.terminate()
            _ = done.wait(timeout: .now() + 2)
            return ("", true, nil)
        }
        p.waitQuietly()
        return (String(data: sink.data, encoding: .utf8) ?? "", false, p.terminationStatus)
    }

    private static var scriptPath: String? {
        Bundle.main.url(forResource: "iterm", withExtension: "js")?.path
    }

    @discardableResult
    private static func osa(_ args: [String], timeout: TimeInterval = 15) -> [String: Any] {
        guard let script = scriptPath else {
            return ["ok": false, "error": L.t.scriptMissing]
        }
        // Inventory is the recovery probe and must always be allowed through. Everything else
        // fails closed while the circuit is open; another automatic close cannot help a person
        // answer the dialog already on screen.
        if args.first != "list", let refusal = automationAttention {
            return ["ok": false, "error": refusal, "attentionRequired": true]
        }
        let run = shell("/usr/bin/osascript", ["-l", "JavaScript", script] + args, timeout: timeout)
        // A deadline missed on this path means iTerm2 is not answering Apple events, and by far
        // the likeliest reason is a dialog waiting on the Mac. Said as its own sentence rather
        // than as "did not respond", because the two ask different things of whoever reads it:
        // one is a fault to report, the other is a thing to go and click.
        if Self.automationCircuitEvidence(appleEventTimedOut: run.timedOut,
                                          listRowsMalformed: false) {
            blockAutomation(L.t.itermBusy)
            return ["ok": false, "error": automationAttention ?? L.t.itermBusy,
                    "attentionRequired": true]
        }
        guard let data = run.out.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            let trimmed = run.out.trimmingCharacters(in: .whitespacesAndNewlines)
            return ["ok": false, "error": trimmed.isEmpty ? L.t.itermSilent : trimmed]
        }
        return obj
    }

    /// Bind failure typing to the dictionary returned by this operation. In particular,
    /// `attentionRequired` is carried by the refused/timed-out Apple event itself; callers never
    /// sample the circuit later and accidentally attribute another operation's modal to this one.
    private static func terminalFailure(_ response: [String: Any], fallback: String)
        -> TerminalFailure {
        let message = response["error"] as? String ?? fallback
        let kind: TerminalFailure.Kind = response["attentionRequired"] as? Bool == true
            ? .iTermAttention : .io
        return TerminalFailure(kind: kind, message: message)
    }

    // MARK: - Who is running an assistant

    struct AssistantProcessScan {
        let assistants: [String: Assistant.Running]
        let error: String?

        var isComplete: Bool { error == nil }
    }

    private static let pidLock = NSLock()
    private static var pidCache: (at: CFAbsoluteTime, scan: AssistantProcessScan)?

    /// Turn one `ps` answer into a confidence-bearing result.
    ///
    /// An empty assistant map is not itself suspicious: most Macs spend most of their time with
    /// no assistant running. An empty *process listing* is different. `ps -ax` always contains at
    /// least its own process and launchd, so silence means the process failed to launch, timed out
    /// or otherwise yielded no observation. Keeping those two empties separate is what prevents a
    /// transient subprocess failure from becoming a confident "every session closed" event.
    static func parseAssistantProcessScan(_ output: String,
                                          timedOut: Bool,
                                          exitStatus: Int32? = 0) -> AssistantProcessScan {
        if timedOut {
            return AssistantProcessScan(assistants: [:], error: "assistant process scan timed out")
        }
        guard exitStatus == 0 else {
            let detail = exitStatus.map(String.init) ?? "launch failure"
            return AssistantProcessScan(assistants: [:],
                                        error: "assistant process scan failed (\(detail))")
        }
        guard !output.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            return AssistantProcessScan(assistants: [:], error: "assistant process scan returned no data")
        }
        return AssistantProcessScan(assistants: Assistant.reading(ofPS: output), error: nil)
    }

    /// JXA saying the app is stopped is normally a trustworthy empty inventory. It stops being
    /// one when the independent process scan still sees an assistant: on the first watch after an
    /// app/server restart there is no previous target row to expose that contradiction for us.
    static func stoppedTerminalContradictsProcesses(appRunning: Bool?,
                                                     processScan: AssistantProcessScan) -> Bool {
        appRunning == false && processScan.isComplete && !processScan.assistants.isEmpty
    }

    /// tty → what is running on it, plus whether the process list itself was readable.
    ///
    /// Only complete results enter the two-second cache. Caching an unreadable result would turn
    /// a single failed `ps` into several authoritative-looking empty scans, which is the failure
    /// mode this confidence bit exists to prevent.
    static func assistantProcessScan() -> AssistantProcessScan {
        pidLock.lock()
        if let c = pidCache, CFAbsoluteTimeGetCurrent() - c.at < 2 {
            defer { pidLock.unlock() }
            return c.scan
        }
        pidLock.unlock()

        let run = shell("/bin/ps", ["-ax", "-o", "tty=,pid=,ppid=,lstart=,command="])
        let scan = parseAssistantProcessScan(run.out, timedOut: run.timedOut,
                                             exitStatus: run.status)
        if scan.isComplete {
            pidLock.lock()
            pidCache = (CFAbsoluteTimeGetCurrent(), scan)
            pidLock.unlock()
        }
        return scan
    }

    /// tty → what is running on it. The one process listing everything else on this path shares.
    ///
    /// Needed because the working directory is the only way into a session's record, and only
    /// the process knows it. The parsing is ``Assistant/reading(ofPS:)``, which is where the
    /// mistakes live and where the tests are.
    ///
    /// Held for a couple of seconds. The scan is a full `ps` — 104 ms measured, by far the most
    /// expensive thing on this path — and the pane asks for it once a second while it is open,
    /// which put a tenth of a second of process listing behind every refresh. A session that
    /// starts or dies is picked up on the next expiry, which is what a status display needs.
    static func assistantPIDs() -> [String: Assistant.Running] {
        assistantProcessScan().assistants
    }

    /// What assistant, if any, is still running on one tty — asked now rather than remembered.
    ///
    /// ``assistantPIDs()`` answers the same question for every tty at once and holds the answer
    /// for a couple of seconds, which is right for a status display and wrong here: this is
    /// asked in a loop by ``Targets/end(_:)`` while it waits for a session to finish leaving,
    /// and a two-second-old "still there" is exactly the difference between closing a quiet tab
    /// and closing one that is still working. The result is scoped to the exact tty after a fresh
    /// whole-process read, whose success/failure status is unambiguous.
    struct TTYAssistantObservation {
        let running: Assistant.Running?
        let error: String?
        var isComplete: Bool {
            error == nil && (running == nil || running?.processStart != nil)
        }
    }

    /// Replaces only the exact-tty subprocess in tests. Callers still exercise the production
    /// safe-close guards and can prove that each decision asks for a fresh observation.
    static var ttyAssistantObservationForTesting: ((String) -> TTYAssistantObservation)?

    static func parseTTYAssistantObservation(_ output: String, tty: String,
                                             timedOut: Bool, exitStatus: Int32?)
        -> TTYAssistantObservation {
        let scan = parseAssistantProcessScan(output, timedOut: timedOut, exitStatus: exitStatus)
        return TTYAssistantObservation(running: scan.assistants[tty], error: scan.error)
    }

    /// A fresh, confidence-bearing reading of one exact tty. `nil` is useful only when the
    /// subprocess itself completed; safe close must distinguish "gone" from "could not scan".
    static func assistantObservation(onTTY tty: String) -> TTYAssistantObservation {
        let bare = tty.hasPrefix("/dev/") ? String(tty.dropFirst("/dev/".count)) : tty
        guard !bare.isEmpty else {
            return TTYAssistantObservation(running: nil, error: "terminal has no tty")
        }
        if let seam = ttyAssistantObservationForTesting { return seam(bare) }
        // A whole-process answer has an unambiguous successful empty-for-this-tty case. `ps -t`
        // uses exit 1 both when no process matched and for some execution failures, which cannot
        // satisfy a fail-closed decision after stderr has been separated.
        let run = shell("/bin/ps", ["-ax", "-o", "tty=,pid=,ppid=,lstart=,command="], timeout: 5)
        return parseTTYAssistantObservation(run.out, tty: bare, timedOut: run.timedOut,
                                            exitStatus: run.status)
    }

    static func assistant(onTTY tty: String) -> Assistant.Running? {
        assistantObservation(onTTY: tty).running
    }

    /// When a process started, from how long it has been running.
    ///
    /// `etime` rather than `lstart`: the latter is a formatted date that changes with the
    /// machine's locale, and parsing a localised date to find a file is a way to work on your
    /// machine and nowhere else.
    static func processStart(ofPID pid: Int32) -> Date? {
        let out = shell("/bin/ps", ["-o", "lstart=", "-p", "\(pid)"]).out
        return Assistant.parseProcessStart(
            out.split(whereSeparator: { $0 == " " || $0 == "\n" }).map(String.init))
    }

    /// `[[dd-]hh:]mm:ss` → seconds.
    static func parseElapsed(_ text: String) -> TimeInterval? {
        var body = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !body.isEmpty else { return nil }
        var days = 0.0
        if let dash = body.firstIndex(of: "-") {
            days = Double(body[body.startIndex..<dash]) ?? 0
            body = String(body[body.index(after: dash)...])
        }
        let parts = body.split(separator: ":").map { Double($0) ?? 0 }
        guard !parts.isEmpty, parts.count <= 3 else { return nil }
        let padded = Array(repeating: 0.0, count: 3 - parts.count) + parts
        return days * 86_400 + padded[0] * 3_600 + padded[1] * 60 + padded[2]
    }

    /// `lsof` rather than the PWD in the environment: an environment variable is whatever it
    /// was at launch, and it splits on spaces, which paths are allowed to contain.
    static func workingDirectory(ofPID pid: Int32) -> String? {
        let out = shell("/usr/sbin/lsof", ["-a", "-p", "\(pid)", "-d", "cwd", "-Fn"]).out
        for line in out.split(separator: "\n") where line.hasPrefix("n/") {
            return String(line.dropFirst())
        }
        return nil
    }

    /// Every file that process has open, by path.
    ///
    /// The same tool and the same argument as the working directory above, without the `-d cwd`
    /// that narrows it to one. It exists because **a Codex session holds its own rollout open**,
    /// which turns "which of these files belongs to that session" from a guess about clocks into
    /// a fact about a file descriptor — see ``Codex/locate(cwd:startedAt:pid:days:)``.
    ///
    /// `-Fn` so the answer is one path per line with an `n` in front of it, rather than a table
    /// whose columns a path with a space in it walks straight through.
    static func openFiles(ofPID pid: Int32) -> [String] {
        shell("/usr/sbin/lsof", ["-p", "\(pid)", "-Fn"]).out
            .split(separator: "\n")
            .filter { $0.hasPrefix("n/") }
            .map { String($0.dropFirst()) }
    }

    /// The other direction: every process holding one file open.
    ///
    /// `openFiles(ofPID:)` answers "what has this process got open", which is the question when
    /// you already know the process. This is the question when you know the *file* and want to
    /// find out whose it is — see ``Shells/stop(_:of:)``, where the file is a background
    /// command's output and the answer decides what may be signalled.
    static func holders(ofPath path: String) -> [Int32] {
        shell("/usr/sbin/lsof", ["-t", "--", path]).out
            .split(separator: "\n")
            .compactMap { Int32($0.trimmingCharacters(in: .whitespaces)) }
    }

    /// A process's parent and its process group, or nothing when `ps` has no answer — which is
    /// what a process that has already gone looks like.
    ///
    /// Both halves matter to the one caller. The parent is the ownership proof: a command
    /// Claude Code started is a child of Claude Code. The group is what gets signalled, because
    /// a shell one-liner is a shell and whatever it has spawned, and killing only the shell
    /// leaves the `sleep` in the middle of it running with nobody waiting on it.
    static func lineage(ofPID pid: Int32) -> (parent: Int32, group: Int32)? {
        let fields = shell("/bin/ps", ["-o", "ppid=,pgid=", "-p", "\(pid)"]).out
            .split(whereSeparator: { $0 == " " || $0 == "\n" })
            .compactMap { Int32($0) }
        guard fields.count >= 2 else { return nil }
        return (fields[0], fields[1])
    }

    // MARK: - API

    static func snapshot() -> Targets.Snapshot {
        var snap = Targets.Snapshot()

        let listed = osa(["list"])
        guard let rows = listed["sessions"] as? [[String: Any]] else {
            snap.isComplete = false
            snap.error = listed["error"] as? String ?? L.t.cannotList
            if automationCircuitEvidence(appleEventTimedOut: false, listRowsMalformed: true) {
                blockAutomation(snap.error ?? L.t.cannotList)
            }
            return snap
        }
        // Reaching this point is the recovery proof: the Apple Event returned a list-shaped
        // answer. Clear the automation circuit before process evidence can independently lower
        // the inventory's authority. In particular, iTerm2 being stopped is a valid empty list
        // and must leave `newtab` able to launch it.
        completeInventory()
        snap.isComplete = listed["complete"] as? Bool ?? (listed["ok"] as? Bool == true)
        if !snap.isComplete {
            snap.error = listed["error"] as? String ?? L.t.cannotList
        }

        let processScan = assistantProcessScan()
        if !processScan.isComplete {
            snap.isComplete = false
            snap.error = processScan.error
        }
        if stoppedTerminalContradictsProcesses(appRunning: listed["appRunning"] as? Bool,
                                               processScan: processScan) {
            snap.isComplete = false
            snap.error = "iTerm2 reported stopped while assistant processes are still running"
        }
        let running = processScan.assistants
        snap.sessions = rows.map { row in
            let tty = row["tty"] as? String ?? ""
            let bare = tty.replacingOccurrences(of: "/dev/", with: "")
            return TargetSession(
                backend: .iterm,
                id: (row["id"] as? String ?? "").uppercased(),
                name: row["name"] as? String ?? "",
                tty: tty,
                windowIndex: row["win"] as? Int ?? 0,
                tabIndex: row["tab"] as? Int ?? 0,
                assistant: running[bare]?.assistant
            )
        }

        let cur = osa(["current"])
        if cur["ok"] as? Bool == true { snap.currentID = (cur["id"] as? String)?.uppercased() }

        return snap
    }

    /// Send text to a session. nil means it worked; anything else is the reason it did not.
    static func send(_ text: String, to sessionID: String, submit: Bool = true) -> String? {
        let res = osa(["send", sessionID, text, submit ? "1" : "0"])
        if res["ok"] as? Bool == true { return nil }
        return res["error"] as? String ?? L.t.sendFailed
    }

    /// Raw key bytes rather than text — see the `key` command in iterm.js.
    /// Close a session's tab. See the `close` command in `iterm.js` for why it is the session
    /// and not the tab.
    static func close(_ sessionID: String) -> String? {
        // The one call with a deadline of its own, because it is the one that can raise a sheet:
        // a tab with a job still in it is closed only after somebody says so. ``Targets/end(_:)``
        // waits for the job to be gone first so that question is not asked, but a profile set to
        // prompt regardless will ask anyway — and six seconds is long enough for a close that is
        // going to happen and short enough that a phone hears about the dialog while it still
        // means something.
        let res = osa(["close", sessionID], timeout: 6)
        if res["ok"] as? Bool == true { return nil }
        return res["error"] as? String ?? "that session is gone"
    }

    static func keystroke(_ bytes: [UInt8], to sessionID: String) -> String? {
        let res = osa(["key", sessionID] + bytes.map(String.init))
        if res["ok"] as? Bool == true { return nil }
        return res["error"] as? String ?? L.t.sendFailed
    }

    static func keystroke(_ byte: UInt8, to sessionID: String) -> String? {
        keystroke([byte], to: sessionID)
    }

    static func submit(_ sessionID: String) -> String? { keystroke(13, to: sessionID) }

    /// What that session currently shows. iTerm2 exposes the visible screen and no more,
    /// so this is a snapshot of the window rather than a transcript.
    static func capture(_ sessionID: String) -> String? {
        let res = osa(["capture", sessionID])
        guard res["ok"] as? Bool == true else { return nil }
        return res["text"] as? String
    }

    /// The tail of several sessions' screens, keyed by session id, in one round trip.
    ///
    /// Asking `capture` per session would be a process and an Apple event bridge each, once a
    /// second, for as long as the list is open. The tail is enough because everything read off a
    /// screen here — the live line, a menu waiting for an answer — is drawn at the bottom of it.
    static func tails(ids: [String], lines: Int = 60) -> [String: String] {
        guard !ids.isEmpty else { return [:] }
        let res = osa(["tails", ids.joined(separator: ","), String(lines)])
        guard res["ok"] as? Bool == true, let rows = res["tails"] as? [String: String] else {
            return [:]
        }
        var out: [String: String] = [:]
        for (id, text) in rows { out[id.uppercased()] = text }
        return out
    }

    /// Open a tab and type one line into it.
    ///
    /// The one operation that is not "talk to a session that already exists", and the only reason
    /// it is here rather than being left to the person at the keyboard: from a phone there is no
    /// keyboard to go to. Returns the new session's id and tty, or nil if it did not happen.
    ///
    /// **The line arrives built and quoted.** It used to arrive as a directory and a command and
    /// be assembled inside `iterm.js`, which meant the string that actually ran only existed on
    /// the far side of an `osascript` call — so the quoting could only be exercised by executing
    /// it, which is the one thing a test must not do. It is built by
    /// ``StartPoints/itermLine(cwd:)`` now, where it is an ordinary value.
    ///
    /// Nothing here brings iTerm2 forward: the tab is made and written into, and the app stays
    /// wherever it was. Whoever asked for this is not at the keyboard.
    static func newTabResult(line: String)
        -> Result<(id: String, tty: String), TerminalFailure> {
        let res = osa(["newtab", line])
        guard res["ok"] as? Bool == true,
              let id = res["id"] as? String, !id.isEmpty else {
            return .failure(terminalFailure(res, fallback: "iTerm2 would not open a tab."))
        }
        return .success((id, res["tty"] as? String ?? ""))
    }

    static func newTab(line: String) -> (id: String, tty: String)? {
        try? newTabResult(line: line).get()
    }

    /// Select a session's window and tab.
    ///
    /// `activate: false` stops short of bringing iTerm2 forward, which is what the prompt bar
    /// wants while it is open: the tab underneath should follow the target you are pointing at,
    /// but the keyboard has to stay in the box you are typing into.
    @discardableResult
    static func reveal(_ sessionID: String, activate: Bool = true) -> TerminalFailure? {
        let res = osa(["reveal", sessionID, activate ? "1" : "0"])
        guard res["ok"] as? Bool == true else {
            return terminalFailure(res, fallback: "iTerm2 would not focus that session.")
        }
        return nil
    }
}
