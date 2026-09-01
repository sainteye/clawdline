import Foundation
import Darwin

/// File-system driven transcript revisions for the web reader.
///
/// SessionWatch describes the terminal, not the JSONL file. Keeping this beside it rather than
/// inside it means transcript bytes can move on their own clock without making every menu-bar,
/// island and session-list consumer repaint. The watch carries no conversation content: its one
/// result is the signature the authenticated transcript GET already understands.
final class TranscriptRevisionWatch: @unchecked Sendable {
    struct Candidate {
        let id: String
        let url: URL
    }

    private final class Entry {
        let url: URL
        let source: DispatchSourceFileSystemObject
        var signature: String

        init(url: URL, source: DispatchSourceFileSystemObject, signature: String) {
            self.url = url
            self.source = source
            self.signature = signature
        }
    }

    private let queue = DispatchQueue(label: "com.tsunamiworks.clawdline.transcript-revisions")
    private let changed: @Sendable (String, String) -> Void
    private var entries: [String: Entry] = [:]
    private var debounceGeneration: [String: UInt64] = [:]

    init(changed: @escaping @Sendable (String, String) -> Void) {
        self.changed = changed
    }

    func replace(with candidates: [Candidate]) {
        queue.async { [weak self] in self?.apply(candidates) }
    }

    func stop() {
        queue.async { [weak self] in self?.removeAll() }
    }

    private func apply(_ candidates: [Candidate]) {
        let wanted = Dictionary(candidates.map { ($0.id, $0.url) },
                                uniquingKeysWith: { first, _ in first })
        for id in Array(entries.keys) where wanted[id] == nil || wanted[id] != entries[id]?.url {
            remove(id)
        }
        for (id, url) in wanted where entries[id] == nil {
            add(id, url: url)
        }
    }

    private func add(_ id: String, url: URL) {
        let descriptor = open(url.path, O_EVTONLY)
        guard descriptor >= 0 else { return }
        let source = DispatchSource.makeFileSystemObjectSource(
            fileDescriptor: descriptor,
            eventMask: [.write, .extend, .rename, .delete, .revoke],
            queue: queue
        )
        let entry = Entry(url: url, source: source, signature: Transcript.signature(of: url))
        entries[id] = entry
        source.setEventHandler { [weak self, weak entry] in
            guard let self, let entry, self.entries[id] === entry else { return }
            let flags = source.data
            self.noteChange(id, entry: entry)
            if !flags.intersection([.rename, .delete, .revoke]).isEmpty {
                self.remove(id)
            }
        }
        source.setCancelHandler { close(descriptor) }
        source.resume()
    }

    private func noteChange(_ id: String, entry: Entry) {
        let generation = (debounceGeneration[id] ?? 0) &+ 1
        debounceGeneration[id] = generation
        queue.asyncAfter(deadline: .now() + .milliseconds(90)) { [weak self, weak entry] in
            guard let self, let entry,
                  self.debounceGeneration[id] == generation,
                  FileManager.default.fileExists(atPath: entry.url.path) else { return }
            let signature = Transcript.signature(of: entry.url)
            guard !signature.isEmpty, signature != entry.signature else { return }
            entry.signature = signature
            self.changed(id, signature)
        }
    }

    private func remove(_ id: String) {
        debounceGeneration.removeValue(forKey: id)
        entries.removeValue(forKey: id)?.source.cancel()
    }

    private func removeAll() {
        let held = entries.values
        entries.removeAll()
        debounceGeneration.removeAll()
        for entry in held { entry.source.cancel() }
    }
}

/// Resolve session-to-file bindings off the UI/server queues and invalidate stale passes when a
/// newer SessionWatch publication arrives. This is the lifecycle half of the low-level fd watch.
final class TranscriptRevisionStream: @unchecked Sendable {
    private let queue = DispatchQueue(label: "com.tsunamiworks.clawdline.transcript-discovery")
    private let watch: TranscriptRevisionWatch
    private var generation: UInt64 = 0
    private var resolving = false
    private var pending: (targets: [TargetSession], generation: UInt64)?
    private let changed: @Sendable (String, String) -> Void

    /// The stream a running server owns, so a screen reading can announce a change without the
    /// server growing an entry point for it. Weak, because the server's lifetime is the real one
    /// and a screen follower must never be the reason a stopped server is kept alive.
    private(set) nonisolated(unsafe) static weak var live: TranscriptRevisionStream?

    init(changed: @escaping @Sendable (String, String) -> Void) {
        self.changed = changed
        watch = TranscriptRevisionWatch(changed: changed)
        Self.live = self
    }

    /// A session's screen now says something its file does not.
    ///
    /// **Why this needs its own door.** The watch behind this stream is a file watch, and the
    /// file is precisely what has not moved — a question's turn stays unwritten until it is
    /// answered, and an answer being typed is not written until it ends. Without this a reader
    /// sitting on a session watches the Mac fill up and their own page stay still.
    ///
    /// The revision is a token, not a signature: the client uses it to fetch once per change and
    /// not to verify what came back, so it only has to differ when the words do.
    func announceScreen(_ sessionID: String, revision: String) {
        changed(sessionID, revision)
    }

    func sync(targets: [TargetSession], active: Bool) {
        queue.async { [weak self] in
            guard let self else { return }
            self.generation &+= 1
            let pass = self.generation
            guard active else { self.pending = nil; self.watch.stop(); return }
            self.pending = (targets, pass)
            self.pump()
        }
    }

    private func pump() {
        guard !resolving, let demand = pending else { return }
        pending = nil
        resolving = true
        DispatchQueue.global(qos: .utility).async { [weak self] in
            let candidates = demand.targets.compactMap { session in
                Transcript.record(of: session).map { record in
                    TranscriptRevisionWatch.Candidate(id: session.id, url: record.url)
                }
            }
            self?.queue.async { [weak self] in
                guard let self else { return }
                self.resolving = false
                if self.generation == demand.generation { self.watch.replace(with: candidates) }
                self.pump()
            }
        }
    }

    func stop() {
        queue.async { [weak self] in
            guard let self else { return }
            self.generation &+= 1
            self.pending = nil
            self.watch.stop()
        }
    }
}

extension RemoteServer {
    /// Read one stable transcript snapshot: if the file moves while its tail is being read,
    /// repeat once so the entries and the signature still describe the same bytes.
    func transcriptPayload(for session: TargetSession, limit: Int) -> Response {
        // Reading a transcript is what "somebody is looking at this session" means here. It is
        // the signal that buys the closer screen sampling ``ScreenFollow`` does, and it costs the
        // caller nothing to declare because it already declared it by asking.
        ScreenFollow.shared.noteReader(of: session)
        guard let record = Transcript.record(of: session) else {
            return .json(["entries": [], "signature": ""])
        }
        let file = record.url
        var signature = Transcript.signature(of: file)
        guard var text = Transcript.tail(of: file, bytes: 8 << 20) else {
            return .json(["entries": [], "signature": ""])
        }
        let after = Transcript.signature(of: file)
        if after != signature, let fresh = Transcript.tail(of: file, bytes: 8 << 20) {
            signature = after
            text = fresh
        }
        var entries = Self.transcriptRows(
            Transcript.parse(text, assistant: record.assistant, limit: limit)
        )
        var revision = signature
        if let unsynced = Self.unsyncedRow(for: session, entries: entries) {
            entries.append(unsynced.row)
            revision += unsynced.revision
        }
        return .json(["entries": entries, "signature": revision])
    }

    /// The words on the screen that the transcript file has not written down yet, as one more
    /// entry — or nil, which is the ordinary answer.
    ///
    /// **Whenever the screen is ahead of the file**, which is not only the waiting case. Claude
    /// Code writes an assistant message when the message is complete, so a long answer exists on
    /// the Mac for as long as it takes to say and nowhere else; a question's turn is the extreme
    /// of that, unwritten until it is answered. The test is the same either way — the screen has
    /// words the file does not — and no state has to be consulted to ask it.
    ///
    /// **It steps aside, paragraph by paragraph, as the file catches up.** Whole-row suppression
    /// was wrong twice over. It said nothing when the file held only the first half of what the
    /// screen showed, and it said everything twice when the comparison missed — which it did on
    /// the first real screen it met, because a transcript entry is Markdown (`` `zh_TW` ``,
    /// asterisks, link syntax) and a screen is the rendered text. So the comparison keeps only
    /// letters and digits, and it is made per paragraph: a paragraph the file already has is
    /// dropped, whatever is left is what the reader is actually missing.
    ///
    /// `provisional` rides on the row because the difference is real and a reader is owed it: one
    /// of these is a record of what was said, the other is a reading of a screen, reassembled
    /// from a hard-wrapped terminal. `ReadingFreshness` publishes age for the same reason.
    static func unsyncedRow(for session: TargetSession,
                            entries: [[String: Any]]) -> (row: [String: Any], revision: String)? {
        guard Self.screenCanLeadTheFile(SessionWatch.shared.publishedInventory().states[session.id]),
              let prose = ScreenTail.unsyncedProse(session.id),
              let text = Self.unsyncedText(in: prose, alreadyIn: entries) else { return nil }
        let row: [String: Any] = ["role": "assistant", "text": text, "provisional": true,
                                  "at": Int(Date().timeIntervalSince1970)]
        return (row, "+p" + String(ScreenTail.fingerprint(of: text), radix: 36))
    }

    /// Whether this session's screen can be ahead of its transcript at all.
    ///
    /// **A still screen is a written screen.** Claude Code writes an assistant message when the
    /// message is complete, so a session that finished talking has already been recorded and its
    /// screen holds nothing the file does not — offering a reading of it is at best a second copy
    /// of what the reader is already looking at. Two states are exceptions and they are the only
    /// two: text being written now, and a question's turn, which stays unwritten until it is
    /// answered.
    static func screenCanLeadTheFile(_ state: SessionState?) -> Bool {
        switch state {
        case .working, .waiting: return true
        default: return false
        }
    }

    /// The paragraphs of a screen reading that no parsed entry already holds, or nil when the
    /// file has all of them.
    static func unsyncedText(in prose: String, alreadyIn entries: [[String: Any]]) -> String? {
        let known = entries.compactMap { entry -> String? in
            let text = comparableText((entry["text"] as? String) ?? "")
            return text.isEmpty ? nil : text
        }
        // Two things are dropped: a paragraph the file already holds, and a paragraph this
        // reading has already offered. The second is the plain rule — **something the reader has
        // seen once is not shown again** — and it holds whatever produced the repeat, which
        // matters because a redraw the reconstruction failed to fold away looks exactly like an
        // ordinary paragraph by the time it reaches here.
        var seen = Set<String>()
        let fresh = prose.components(separatedBy: "\n\n").filter { block in
            let compared = comparableText(block)
            guard !compared.isEmpty else { return false }
            guard seen.insert(compared).inserted else { return false }
            return !known.contains { $0.contains(compared) }
        }
        return fresh.isEmpty ? nil : fresh.joined(separator: "\n\n")
    }

    /// Letters and digits only.
    ///
    /// A screen shows what Markdown renders to; a transcript entry holds the Markdown itself.
    /// Everything that differs between those two — the backticks, the asterisks, the brackets,
    /// and the hard wrap's own whitespace — is punctuation, and none of it is the sentence.
    static func comparableText(_ text: String) -> String {
        String(text.filter { $0.isLetter || $0.isNumber })
    }

}
