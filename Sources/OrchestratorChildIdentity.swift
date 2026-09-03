import Foundation

// Which terminal, process and transcript belong to this child, and whether its briefing has
// landed. Everything here is a decision taken from evidence — a screen, a transcript's bytes, a
// pid and its start time, a registry row — and none of it touches the registry lock or any state
// the lock protects. `OrchestratorChildBrief` holds what a child is told; this holds how the
// broker recognises the child it told.
//
// Chosen by measuring rather than by `// MARK:`. It had no heading of its own: it lived in the
// middle of `// MARK: - Independent feature roots`, which never described it. On the base this was
// cut from it referenced no `private` symbol left behind and acquired `Orchestrator.lock` zero
// times — the only two symbols crossing the boundary were `ownershipLock` and `ownershipCache`,
// read by the test reset, which now calls `resetTranscriptOwnershipCacheForTesting()` instead so
// the memo cache stays private to the file that owns it.
//
// It moves as an `extension`, which renames nothing. See `OrchestratorRootAssignmentShape` for
// the branch count that decided that.
extension Orchestrator {

    enum BriefingDecision: Equatable {
        case send, wait, accepted, exhausted
    }

    /// Process and registry facts assembled for one identity decision. The process start is read
    /// from `pid` itself, so the pair cannot combine two generations of the same terminal.
    struct ChildObservation: Equatable {
        var pid: Int32?
        var procStart: Date?
        var registrySessionID: String? = nil
        var registryTranscript: URL? = nil
    }

    enum IdentityStep: Equatable {
        case none
        case useRegistry(sessionID: String, transcript: URL)
        case refuseForeignProcess(seen: Int32?)
    }

    struct IdentityComparison: Equatable {
        var registrySessionID: String
        var registryTranscriptPath: String
        var legacySessionID: String?
        var legacyTranscriptPath: String?
    }

    /// Once a Claude process pair has been recorded, a later process on the same tab cannot
    /// contribute identity. An incomplete pair is unverifiable and therefore fails closed.
    static func identityStep(for task: Task, seeing observation: ChildObservation) -> IdentityStep {
        guard task.assistant == .claude else { return .none }
        if let recordedPID = task.childPID {
            guard observation.pid == recordedPID else {
                return .refuseForeignProcess(seen: observation.pid)
            }
            guard let recordedStart = task.childProcStart, let seenStart = observation.procStart,
                  abs(seenStart.timeIntervalSince(recordedStart))
                    <= SessionRegistry.startTolerance else {
                return .refuseForeignProcess(seen: observation.pid)
            }
        }
        if let sessionID = observation.registrySessionID,
           let transcript = observation.registryTranscript {
            return .useRegistry(sessionID: sessionID, transcript: transcript)
        }
        return .none
    }

    /// Record only a start time read directly from the pid beside it. A partial pair would make
    /// every later strict comparison either too permissive or permanently reject the real child.
    static func recordProcessIdentity(from observation: ChildObservation, in task: inout Task)
        -> Bool {
        guard task.childPID == nil, task.childProcStart == nil,
              let pid = observation.pid, let started = observation.procStart else { return false }
        task.childPID = pid
        task.childProcStart = started
        return true
    }

    /// A transcript can legitimately be absent while both sources already name the same session.
    /// Compare the durable identity, while retaining both paths as evidence when the ids diverge.
    static func identityComparison(registrySessionID: String, registryTranscript: URL,
                                   legacyTask: Task) -> IdentityComparison? {
        guard registrySessionID != legacyTask.childSessionId else { return nil }
        return IdentityComparison(registrySessionID: registrySessionID,
                                  registryTranscriptPath: registryTranscript.path,
                                  legacySessionID: legacyTask.childSessionId,
                                  legacyTranscriptPath: legacyTask.transcriptPath)
    }

    /// Once the briefing receipt has proved this pair, a later registry answer describes a
    /// `/clear` or parked conversation in the same process, not a correction to this task.
    static func adoptRegistryIdentity(sessionID: String, transcript: URL,
                                      in task: inout Task) -> Bool {
        guard !(task.state == .briefed && task.transcriptProven) else { return false }
        guard task.childSessionId != sessionID || task.transcriptPath != transcript.path
        else { return false }
        task.childSessionId = sessionID
        task.transcriptPath = transcript.path
        task.transcriptProven = false
        return true
    }

    /// A current hook may correct a provisional pair only when its note is no older than this
    /// spawn and the recorded Claude process still matches this beat; the caller enforces both
    /// before reaching this seam. The first note remains a fallback when process inspection is
    /// unavailable, but an unverified later note never replaces identity. A correction drops the
    /// old provisional path so locate can resolve the new session. Once the briefing receipt pins
    /// the pair, even the same process may be describing `/clear` or a parked chat.
    static func adoptHookIdentity(sessionID: String, in task: inout Task) -> Bool {
        guard !(task.state == .briefed && task.transcriptProven),
              task.childSessionId != sessionID else { return false }
        let isFirstIdentity = task.childSessionId == nil && task.transcriptPath == nil
        let hasProcessIdentity = task.childPID != nil && task.childProcStart != nil
        guard isFirstIdentity || hasProcessIdentity else { return false }
        task.childSessionId = sessionID
        task.transcriptPath = nil
        task.transcriptProven = false
        return true
    }

    /// The transition control samples each distinct registry answer once. The flag is transient,
    /// so a restart earns one fresh comparison without restoring per-beat I/O.
    static func beginRegistryControl(for sessionID: String, in task: inout Task) -> Bool {
        guard task.registryControlSessionID != sessionID else { return false }
        task.registryControlSessionID = sessionID
        return true
    }

    static let briefingAttemptLimit = 5
    static let briefingReceiptDelay: TimeInterval = 15

    /// Whether the assistant has drawn the empty composer that can accept a new turn.
    ///
    /// This is deliberately narrower than `SessionState.idle`. That state is the absence of a
    /// recognised menu or live line; a startup banner while slow MCP servers are still loading
    /// has exactly that absence. The composer is positive evidence that startup has completed.
    static func briefingInputReady(_ screen: String?, assistant: Assistant) -> Bool {
        guard let screen, !screen.isEmpty else { return false }
        let text = Ansi.plain(screen)
        guard SessionState.read(text, assistant: assistant) == .idle else { return false }
        // Claude Code puts U+00A0 between its caret and the composer, not a space: a real capture
        // reads `❯\u{00A0}` when empty and `❯\u{00A0}/deploy` with a draft in it. Trimming does
        // hide that — `.whitespaces` is Unicode Zs, which U+00A0 belongs to — but only at the ends
        // of a line, so the bare-caret test below passes while a prefix written with a plain space
        // never fires at all. Fold it first, once, rather than spell it into every comparison.
        let lines = text.split(separator: "\n", omittingEmptySubsequences: false)
            .map { $0.replacingOccurrences(of: "\u{00A0}", with: " ")
                     .trimmingCharacters(in: .whitespaces) }

        switch assistant {
        case .claude:
            // Claude's empty composer is either a bare caret or carries its grey `Try "…"`
            // suggestion — the bundle renders it as `Try "` around a bolded example, so a brand
            // new session is far more likely to show that than a bare caret. A submitted message
            // is echoed after the same caret, so accepting an arbitrary suffix here would turn
            // the receipt check below back into a race.
            return lines.contains { $0 == "❯" || $0.hasPrefix("❯ Try \"") }
        case .codex:
            // Codex writes sent messages after the same glyph; its empty composer is the one
            // whose placeholder says what can be entered, not merely any `›` in the scrollback.
            return lines.contains { $0 == "›" || $0.hasPrefix("› Ask Codex to do anything") }
        }
    }

    /// The words in ``firstLine(id:secret:announce:)`` that say what the session is, with the
    /// task id left off.
    ///
    /// Two things read it and they read it for different reasons. The delivery receipt below
    /// wants it *with* a particular task's id after it, which is what proves a transcript
    /// belongs to that task. ``StartPoints/front(inText:limit:)`` wants it without one, because
    /// the question there is only whether this app opened the session at all — a list of
    /// conversations to pick back up is a list of the ones you had, and a child is this app's
    /// own plumbing.
    ///
    /// Not shared with `firstLine` itself, which still spells the sentence out where it is
    /// written. The test *"the mark the list filters on is the line a child is actually given"*
    /// is what keeps the two from drifting apart.
    static let briefingMark = "Clawdline CHILD agent for task"

    /// The same words with the clause `firstLine` opens on, which is what a child's very first
    /// turn *begins* with.
    ///
    /// The narrower one above can appear anywhere in a user turn, and a receipt wants that: it is
    /// looking for delivery, and a briefing that arrived after something else was typed still
    /// arrived. A list of conversations wants the opposite. This conversation's own transcript
    /// opens with a sentence asking why the matching lives in `Orchestrator` — a question about
    /// the mark, containing the mark — and under the loose test it would have filtered itself out
    /// of the list somebody was reading it in.
    static let briefingOpening = "You are a " + briefingMark

    /// The task marker in a user turn is the delivery receipt. Looking for this specific turn,
    /// rather than any user-shaped bookkeeping row, also proves that the transcript belongs to
    /// this task before it is allowed to close the retry gate.
    static func transcriptContainsBriefing(_ transcript: String?, assistant: Assistant,
                                           taskID: String) -> Bool {
        guard let transcript else { return false }
        let marker = "\(briefingMark) \(taskID)"
        return Transcript.parse(transcript, assistant: assistant, limit: 100).contains { entry in
            entry.kind == .user && entry.text.contains(marker)
        }
    }

    static func transcriptContainsHandoff(_ transcript: String?, assistant: Assistant,
                                          handoffID: String) -> Bool {
        guard let transcript else { return false }
        let line = handoffLine(id: handoffID)
        return Transcript.parse(transcript, assistant: assistant, limit: 100).contains { entry in
            entry.kind == .user && entry.text.contains(line)
        }
    }

    /// A first send needs only a ready composer. Every retry additionally needs a known
    /// transcript, exactly as a dispatched briefing does: absence is not evidence of rejection.
    static func handoffRetryAllowed(attempts: Int, transcriptKnown: Bool) -> Bool {
        attempts == 0 || transcriptKnown
    }

    enum TranscriptOwnership: Equatable {
        case belongs, other, unavailable
    }

    private static let ownershipLock = NSLock()
    private static var ownershipCache: [String: (signature: String,
                                                  ownership: TranscriptOwnership)] = [:]
    private static let ownershipCacheLimit = 1_024
    /// A child is a fresh conversation and the briefing is its first user turn. One MiB leaves
    /// ample room for startup bookkeeping while putting a hard ceiling on every main-thread
    /// ownership check.
    private static let ownershipScanBytes = 1_048_576

    /// A guessed or restored path becomes identity only when the child's own first turn names
    /// this task. Timestamps narrow the files worth opening; they do not prove ownership.
    static func transcriptOwnership(_ url: URL, assistant: Assistant,
                                    taskID: String) -> TranscriptOwnership {
        let key = "\(assistant.rawValue)\u{0}\(taskID)\u{0}\(url.path)"
        let signature = Transcript.signature(of: url)
        ownershipLock.lock()
        if let cached = ownershipCache[key], cached.signature == signature {
            ownershipLock.unlock()
            return cached.ownership
        }
        ownershipLock.unlock()

        guard let handle = try? FileHandle(forReadingFrom: url) else { return .unavailable }
        defer { try? handle.close() }
        let data: Data
        do {
            data = try handle.read(upToCount: ownershipScanBytes) ?? Data()
        } catch {
            return .unavailable
        }
        let marker = Data("\(briefingMark) \(taskID)".utf8)
        var from = data.startIndex
        while from < data.endIndex,
              let hit = data.range(of: marker, in: from..<data.endIndex) {
            var low = hit.lowerBound
            while low > data.startIndex, data[data.index(before: low)] != 0x0A {
                low = data.index(before: low)
            }
            var high = hit.upperBound
            while high < data.endIndex, data[high] != 0x0A { high = data.index(after: high) }
            if let row = String(data: data[low..<high], encoding: .utf8),
               transcriptContainsBriefing(row, assistant: assistant, taskID: taskID) {
                cacheOwnership(.belongs, key: key, signature: signature)
                return .belongs
            }
            from = hit.upperBound
        }
        let text = String(decoding: data, as: UTF8.self)
        if Transcript.containsUserTurn(text, assistant: assistant) {
            // The briefing is the fresh child's first user turn. Finding another completed user
            // turn in the bounded prefix is positive disproof even when a long file continues;
            // its expected marker cannot first appear beyond a conversation already in progress.
            cacheOwnership(.other, key: key, signature: signature)
            return .other
        }
        // An empty file, startup metadata, or a prefix cut before any complete user row says
        // nothing about ownership. Cache that answer by signature so an unchanged large prefix
        // is not reread every polling beat; growth naturally invalidates it.
        cacheOwnership(.unavailable, key: key, signature: signature)
        return .unavailable
    }

    private static func cacheOwnership(_ ownership: TranscriptOwnership, key: String,
                                       signature: String) {
        ownershipLock.lock(); defer { ownershipLock.unlock() }
        if ownershipCache[key] == nil, ownershipCache.count >= ownershipCacheLimit,
           let oldest = ownershipCache.keys.first {
            ownershipCache.removeValue(forKey: oldest)
        }
        ownershipCache[key] = (signature, ownership)
    }

    static func transcriptBelongsToTask(_ url: URL, assistant: Assistant,
                                        taskID: String) -> Bool {
        if case .belongs = transcriptOwnership(url, assistant: assistant, taskID: taskID) {
            return true
        }
        return false
    }

    /// Pure policy for the asynchronous hand-off. A retry needs all three facts: enough time has
    /// passed, the named transcript still lacks this task's turn, and the empty composer is back.
    /// Claude and Codex append the user turn before beginning it, so an accepted first send puts
    /// the marker in the transcript before it can execute. That closes this gate before another
    /// copy can be sent; a missing transcript alone is never grounds for a retry.
    static func briefingDecision(screen: String?, assistant: Assistant, transcript: String?,
                                 transcriptKnown: Bool, taskID: String, attempts: Int,
                                 secondsSinceAttempt: TimeInterval?) -> BriefingDecision {
        if transcriptContainsBriefing(transcript, assistant: assistant, taskID: taskID) {
            return .accepted
        }
        guard briefingInputReady(screen, assistant: assistant) else { return .wait }
        if attempts == 0 { return .send }
        guard transcriptKnown else { return .wait }
        if let secondsSinceAttempt, secondsSinceAttempt < briefingReceiptDelay { return .wait }
        return attempts >= briefingAttemptLimit ? .exhausted : .send
    }

    /// Test seam: forget every memoised transcript-ownership answer.
    ///
    /// The reset used to reach into `ownershipLock` and `ownershipCache` directly, which is the
    /// one thing in this block that could not follow it out of `Orchestrator.swift`. A named seam
    /// keeps the cache and its lock private to the file that owns them.
    static func resetTranscriptOwnershipCacheForTesting() {
        ownershipLock.lock()
        ownershipCache = [:]
        ownershipLock.unlock()
    }
}
