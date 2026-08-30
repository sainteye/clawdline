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

    init(changed: @escaping @Sendable (String, String) -> Void) {
        watch = TranscriptRevisionWatch(changed: changed)
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
        let entries = Self.transcriptRows(
            Transcript.parse(text, assistant: record.assistant, limit: limit)
        )
        return .json(["entries": entries, "signature": signature])
    }
}
