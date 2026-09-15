import Foundation
import Darwin
#if canImport(ClawdlineApplication)
import ClawdlineApplication // W3-1 correction: real cross-module import, see Sources/HostPorts.swift
#endif

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
    /// Nil for the local event stream, which announces changes only: a page that has just
    /// connected reads its transcript anyway, and one event per session on connect would make
    /// every open page read again. The Cloud demand has no such read, so it is told the
    /// signature each watch starts with and when a watch ends.
    private let forgotten: (@Sendable (String) -> Void)?
    private var entries: [String: Entry] = [:]
    private var debounceGeneration: [String: UInt64] = [:]

    init(changed: @escaping @Sendable (String, String) -> Void,
         forgotten: (@Sendable (String) -> Void)? = nil) {
        self.changed = changed
        self.forgotten = forgotten
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
            // A moved file is announced by the add below, so it is not forgotten in between.
            remove(id, announce: wanted[id] == nil)
        }
        for (id, url) in wanted where entries[id] == nil {
            add(id, url: url)
        }
    }

    private func add(_ id: String, url: URL) {
        let descriptor = open(url.path, O_EVTONLY)
        guard descriptor >= 0 else { forgotten?(id); return }
        let source = DispatchSource.makeFileSystemObjectSource(
            fileDescriptor: descriptor,
            eventMask: [.write, .extend, .rename, .delete, .revoke],
            queue: queue
        )
        let entry = Entry(url: url, source: source, signature: Transcript.signature(of: url))
        entries[id] = entry
        if forgotten != nil, !entry.signature.isEmpty { changed(id, entry.signature) }
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

    private func remove(_ id: String, announce: Bool = true) {
        debounceGeneration.removeValue(forKey: id)
        guard let entry = entries.removeValue(forKey: id) else { return }
        entry.source.cancel()
        if announce { forgotten?(id) }
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
    private let resolve: @Sendable (TargetSession) -> URL?

    init(changed: @escaping @Sendable (String, String) -> Void,
         forgotten: (@Sendable (String) -> Void)? = nil,
         resolve: @escaping @Sendable (TargetSession) -> URL? = { Transcript.record(of: $0)?.url }) {
        watch = TranscriptRevisionWatch(changed: changed, forgotten: forgotten)
        self.resolve = resolve
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
        let resolve = self.resolve
        DispatchQueue.global(qos: .utility).async { [weak self] in
            let candidates = demand.targets.compactMap { session in
                resolve(session).map { url in
                    TranscriptRevisionWatch.Candidate(id: session.id, url: url)
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

/// The Cloud bridge's transcript demand, on its own watch so the local event stream keeps its
/// own lifetime (it runs only while a local page is connected) and its change-only events.
///
/// Resolving a Session to its file costs process and registry reads, so a `track` naming the
/// same Sessions with the same selecting facts does not resolve again until
/// `rebindIntervalSeconds` has passed or a watch has ended — which is when a file can have moved
/// without any of those facts changing. Signatures themselves come from fd events, never from
/// the publication path.
final class CloudTranscriptSignatureWatch: CloudTranscriptSignatureSource, @unchecked Sendable {
    static let rebindIntervalSeconds: TimeInterval = 60

    /// The watch's callbacks outlive any one `track`, so they reach the current report and the
    /// binding cache through this box rather than through closures made at construction.
    private final class Relay: @unchecked Sendable {
        private let lock = NSLock()
        private var report: CloudTranscriptSignatureReport?
        private var ended: (() -> Void)?

        func set(report value: CloudTranscriptSignatureReport?) {
            lock.lock(); report = value; lock.unlock()
        }

        func set(ended value: (() -> Void)?) {
            lock.lock(); ended = value; lock.unlock()
        }

        func deliver(_ id: String, _ signature: String?) {
            lock.lock()
            let current = report
            let end = signature == nil ? ended : nil
            lock.unlock()
            end?()
            current?(id, signature)
        }
    }

    private let lock = NSLock()
    private let relay: Relay
    private let stream: TranscriptRevisionStream
    private let targets: () -> [TargetSession]
    private let uptime: () -> TimeInterval
    private var generation: UInt64 = 0
    private var bound: (subjects: [CloudTranscriptSubject], at: TimeInterval)?

    /// `targets` is read on the main queue, where SessionWatch keeps them.
    init(targets: @escaping () -> [TargetSession] = { SessionWatch.shared.targets },
         resolve: @escaping @Sendable (TargetSession) -> URL? = { Transcript.record(of: $0)?.url },
         uptime: @escaping () -> TimeInterval = { ProcessInfo.processInfo.systemUptime }) {
        let relay = Relay()
        self.relay = relay
        self.targets = targets
        self.uptime = uptime
        stream = TranscriptRevisionStream(
            changed: { relay.deliver($0, $1) },
            forgotten: { relay.deliver($0, nil) },
            resolve: resolve)
        relay.set(ended: { [weak self] in self?.forgetBinding() })
    }

    func track(_ subjects: [CloudTranscriptSubject],
               report: @escaping CloudTranscriptSignatureReport) {
        relay.set(report: report)
        lock.lock()
        let now = uptime()
        if let bound, bound.subjects == subjects, now - bound.at < Self.rebindIntervalSeconds {
            lock.unlock()
            return
        }
        bound = (subjects, now)
        generation &+= 1
        let owned = generation
        lock.unlock()
        let wanted = Set(subjects.map(\.sessionID))
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            let found = self.targets().filter { wanted.contains($0.id) }
            self.lock.lock()
            defer { self.lock.unlock() }
            guard self.generation == owned else { return }
            // A Session SessionWatch has not listed yet is asked for again on the next track.
            if found.count != wanted.count { self.bound = nil }
            self.stream.sync(targets: found, active: true)
        }
    }

    func stop() {
        relay.set(report: nil)
        lock.lock()
        defer { lock.unlock() }
        generation &+= 1
        bound = nil
        stream.stop()
    }

    private func forgetBinding() {
        lock.lock(); bound = nil; lock.unlock()
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
