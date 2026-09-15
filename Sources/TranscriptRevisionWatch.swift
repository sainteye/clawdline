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
        /// The folder holding `url` as the pass that chose it saw that folder, or nil when the
        /// folder changed while that pass ran — see ``folderStamp(of:resolvingSince:)``.
        let folder: FolderStamp?
    }

    /// A folder's modification time. A conversation that moves on to a new file adds that file
    /// beside the old one, which moves this; appending to a transcript does not.
    struct FolderStamp: Equatable {
        let seconds: Int
        let nanoseconds: Int
    }

    /// How long writes may hold a report back: until they pause for `quietMilliseconds`, so a run
    /// of appends is one report, and never longer than `maximumMilliseconds` after the first write
    /// a report covers, so appends that never pause that long still report once per interval.
    struct Debounce: Sendable {
        let quietMilliseconds: Int
        let maximumMilliseconds: Int

        static let standard = Debounce(quietMilliseconds: 90, maximumMilliseconds: 1_000)
    }

    private final class Entry {
        let url: URL
        let source: DispatchSourceFileSystemObject
        var signature: String
        var folder: FolderStamp?

        init(url: URL, source: DispatchSourceFileSystemObject, signature: String,
             folder: FolderStamp?) {
            self.url = url
            self.source = source
            self.signature = signature
            self.folder = folder
        }
    }

    private let queue = DispatchQueue(label: "com.tsunamiworks.clawdline.transcript-revisions")
    private let changed: @Sendable (String, String) -> Void
    /// Nil for the local event stream, which announces changes only: a page that has just
    /// connected reads its transcript anyway, and one event per session on connect would make
    /// every open page read again. The Cloud demand has no such read, so it is told the
    /// signature each watch starts with and when a watch ends.
    private let forgotten: (@Sendable (String) -> Void)?
    private let debounce: Debounce
    private var entries: [String: Entry] = [:]
    private var debounceGeneration: [String: UInt64] = [:]
    /// The generation of the write that opened each still-unreported run of writes.
    private var burstGeneration: [String: UInt64] = [:]

    init(changed: @escaping @Sendable (String, String) -> Void,
         forgotten: (@Sendable (String) -> Void)? = nil,
         debounce: Debounce = .standard) {
        self.changed = changed
        self.forgotten = forgotten
        self.debounce = debounce
    }

    func replace(with candidates: [Candidate]) {
        queue.async { [weak self] in self?.apply(candidates) }
    }

    func stop() {
        queue.async { [weak self] in self?.removeAll() }
    }

    /// Call `moved` when any watched file's folder no longer has the stamp the pass that chose
    /// the file recorded. One `stat` per watched file, on this watch's queue.
    func probeFolders(_ moved: @escaping @Sendable () -> Void) {
        queue.async { [weak self] in
            guard let self else { return }
            let stale = self.entries.values.contains { entry in
                entry.folder.map { $0 != TranscriptRevisionWatch.folderStamp(of: entry.url) }
                    ?? true
            }
            if stale { moved() }
        }
    }

    static func folderStamp(of file: URL) -> FolderStamp {
        var info = stat()
        guard stat(file.deletingLastPathComponent().path, &info) == 0 else {
            return FolderStamp(seconds: 0, nanoseconds: 0)
        }
        return FolderStamp(seconds: Int(info.st_mtimespec.tv_sec),
                           nanoseconds: Int(info.st_mtimespec.tv_nsec))
    }

    /// The stamp a pass that began resolving at `began` may keep, or nil when the folder changed
    /// after that moment (a second of slack for coarse file-system clocks): the pass may have
    /// listed the folder before the new file arrived, so the next probe resolves once more
    /// rather than trusting a stamp that already includes it.
    static func folderStamp(of file: URL, resolvingSince began: Date) -> FolderStamp? {
        let stamp = folderStamp(of: file)
        let changedAt = Double(stamp.seconds) + Double(stamp.nanoseconds) / 1_000_000_000
        // Bounded above as well, so a folder dated in the future is not resolved on every probe.
        let during = changedAt >= began.timeIntervalSince1970 - 1
            && changedAt <= Date().timeIntervalSince1970 + 1
        return during ? nil : stamp
    }

    private func apply(_ candidates: [Candidate]) {
        let wanted = Dictionary(candidates.map { ($0.id, $0) },
                                uniquingKeysWith: { first, _ in first })
        for id in Array(entries.keys)
        where wanted[id] == nil || wanted[id]?.url != entries[id]?.url {
            // A moved file is announced by the add below, so it is not forgotten in between.
            remove(id, announce: wanted[id] == nil)
        }
        for (id, candidate) in wanted {
            if let entry = entries[id] {
                entry.folder = candidate.folder
            } else {
                add(id, url: candidate.url, folder: candidate.folder)
            }
        }
    }

    private func add(_ id: String, url: URL, folder: FolderStamp?) {
        let descriptor = open(url.path, O_EVTONLY)
        guard descriptor >= 0 else { forgotten?(id); return }
        let source = DispatchSource.makeFileSystemObjectSource(
            fileDescriptor: descriptor,
            eventMask: [.write, .extend, .rename, .delete, .revoke],
            queue: queue
        )
        let entry = Entry(url: url, source: source, signature: Transcript.signature(of: url),
                          folder: folder)
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
        let quiet = DispatchTime.now() + .milliseconds(debounce.quietMilliseconds)
        queue.asyncAfter(deadline: quiet) { [weak self, weak entry] in
            guard let self, let entry, self.entries[id] === entry,
                  self.debounceGeneration[id] == generation else { return }
            self.report(id, entry: entry)
        }
        // Each later write pushes the quiet report back; this one, set by the write that opens
        // the run, is the report they cannot push back.
        guard burstGeneration[id] == nil else { return }
        burstGeneration[id] = generation
        let latest = DispatchTime.now() + .milliseconds(debounce.maximumMilliseconds)
        queue.asyncAfter(deadline: latest) { [weak self, weak entry] in
            guard let self, let entry, self.entries[id] === entry,
                  self.burstGeneration[id] == generation else { return }
            self.report(id, entry: entry)
        }
    }

    /// Either deadline reports the file as it is now and closes the run it covered.
    private func report(_ id: String, entry: Entry) {
        burstGeneration.removeValue(forKey: id)
        guard FileManager.default.fileExists(atPath: entry.url.path) else { return }
        let signature = Transcript.signature(of: entry.url)
        guard !signature.isEmpty, signature != entry.signature else { return }
        entry.signature = signature
        changed(id, signature)
    }

    private func remove(_ id: String, announce: Bool = true) {
        debounceGeneration.removeValue(forKey: id)
        burstGeneration.removeValue(forKey: id)
        guard let entry = entries.removeValue(forKey: id) else { return }
        entry.source.cancel()
        if announce { forgotten?(id) }
    }

    private func removeAll() {
        let held = entries.values
        entries.removeAll()
        debounceGeneration.removeAll()
        burstGeneration.removeAll()
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
         resolve: @escaping @Sendable (TargetSession) -> URL? = { Transcript.record(of: $0)?.url },
         debounce: TranscriptRevisionWatch.Debounce = .standard) {
        watch = TranscriptRevisionWatch(changed: changed, forgotten: forgotten, debounce: debounce)
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
            let candidates: [TranscriptRevisionWatch.Candidate] = demand.targets.compactMap {
                session in
                // Taken before resolving, so a file that arrives while the folder is being read
                // leaves this pass's stamp unsettled.
                let began = Date()
                return resolve(session).map { url in
                    TranscriptRevisionWatch.Candidate(
                        id: session.id, url: url,
                        folder: TranscriptRevisionWatch.folderStamp(of: url, resolvingSince: began))
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

    /// Ask the watch whether a watched file's folder has moved since its pass. Skipped while a pass
    /// is resolving or queued: that pass records the stamps the next probe compares with.
    func probeFolders(_ moved: @escaping @Sendable () -> Void) {
        queue.async { [weak self] in
            guard let self, !self.resolving, self.pending == nil else { return }
            self.watch.probeFolders(moved)
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
/// `rebindIntervalSeconds` has passed, a watch has ended, or the folder holding a watched file has
/// changed since the pass that chose it — which is when a file can have moved without any of
/// those facts changing: a conversation that goes on in a new file, with no conversation id on
/// its row, adds that file beside the old one. Asking the folders costs one `stat` per watched
/// file on the watch's queue; a changed folder buys one pass over the same Sessions. Signatures
/// themselves come from fd events, never from the publication path.
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
         uptime: @escaping () -> TimeInterval = { ProcessInfo.processInfo.systemUptime },
         debounce: TranscriptRevisionWatch.Debounce = .standard) {
        let relay = Relay()
        self.relay = relay
        self.targets = targets
        self.uptime = uptime
        stream = TranscriptRevisionStream(
            changed: { relay.deliver($0, $1) },
            forgotten: { relay.deliver($0, nil) },
            resolve: resolve,
            debounce: debounce)
        relay.set(ended: { [weak self] in self?.forgetBinding() })
    }

    func track(_ subjects: [CloudTranscriptSubject],
               report: @escaping CloudTranscriptSignatureReport) {
        relay.set(report: report)
        lock.lock()
        let now = uptime()
        if let bound, bound.subjects == subjects, now - bound.at < Self.rebindIntervalSeconds {
            let owned = generation
            lock.unlock()
            stream.probeFolders { [weak self] in self?.rebindAfterFolderChange(generation: owned) }
            return
        }
        bound = (subjects, now)
        generation &+= 1
        let owned = generation
        lock.unlock()
        bind(subjects, generation: owned)
    }

    /// A watched file's folder changed after the pass that chose the file: the same Sessions are
    /// resolved again, unless a newer `track` or `stop()` has already taken over.
    private func rebindAfterFolderChange(generation owned: UInt64) {
        lock.lock()
        guard generation == owned, let current = bound else { lock.unlock(); return }
        bound = (current.subjects, uptime())
        generation &+= 1
        let next = generation
        lock.unlock()
        bind(current.subjects, generation: next)
    }

    private func bind(_ subjects: [CloudTranscriptSubject], generation owned: UInt64) {
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
