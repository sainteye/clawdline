import Foundation

/// Storage Clawdline may reason about because it has an explicit ownership receipt.
///
/// This type never discovers candidates by walking a scratch namespace. Its only inventory is
/// ``ledgerURL``; paths in that ledger are compared with a freshly reconstructed canonical path
/// before they can become releasable.
enum OwnedStorage {
    enum DecisionState: String, Equatable {
        case held, releasable, unknown
    }

    enum Source<Value> {
        case known(Value)
        case unreadable
    }

    enum Liveness: Equatable {
        case known(Set<String>)
        case unreadable
    }

    enum Landing: Equatable {
        case none, pending, settled
    }

    enum ProcessStatus: Equatable {
        case absent, dead, reused, alive, unreadable
    }

    enum PathStatus: Equatable {
        case valid, missing, invalid, unreadable
    }

    struct Entry: Equatable {
        let at: Date
        let taskID: String
        let assistant: String
        let sessionID: String
        let path: String
        let proof: String
        let projectDir: String

        init(at: Date, taskID: String, assistant: String, sessionID: String, path: String,
             proof: String, projectDir: String) {
            self.at = at
            self.taskID = taskID
            self.assistant = assistant
            self.sessionID = sessionID
            self.path = path
            self.proof = proof
            self.projectDir = projectDir
        }
    }

    enum LedgerRead {
        case known([Entry], malformedLines: [Int])
        case unreadable
    }

    struct TaskFacts {
        var isTerminal: Bool
        var createdAt: Date
        var finishedAt: Date?
        var childPID: Int32?
        var childProcStart: Date?
    }

    struct EvaluationInput {
        var entry: Entry
        var task: Source<TaskFacts>
        var landing: Source<Landing>
        var retained: Source<Bool> = .known(false)
        var sessions: Liveness
        var process: ProcessStatus
        var path: PathStatus
        var now: Date
    }

    struct Decision: Equatable {
        let state: DecisionState
        let why: String
        let eligibleAt: Date?

        /// Collection code must use this boundary rather than checking for "not held". Unknown
        /// is deliberately not releasable.
        var mayCollect: Bool { state == .releasable }
    }

    static var ledgerURLOverrideForTesting: URL?
    static var scratchRootOverrideForTesting: URL?
    static var sessionsDirectoryOverrideForTesting: URL?

    static var ledgerURL: URL {
        ledgerURLOverrideForTesting
            ?? RemoteAuth.directory.appendingPathComponent("owned-storage.jsonl")
    }

    static var scratchRoot: URL {
        scratchRootOverrideForTesting
            ?? URL(fileURLWithPath: "/private/tmp/claude-\(getuid())", isDirectory: true)
    }

    static var sessionsDirectory: URL {
        sessionsDirectoryOverrideForTesting
            ?? FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent(".claude/sessions", isDirectory: true)
    }

    private static let ledgerLock = NSLock()
    private static let canonicalCharacters = CharacterSet(
        charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-")

    static func canonicalUUID(_ raw: String) -> Bool {
        guard raw == raw.lowercased(), let parsed = UUID(uuidString: raw) else { return false }
        return parsed.uuidString.lowercased() == raw
    }

    static func projectSlug(_ projectDir: String) -> String? {
        guard projectDir.hasPrefix("/"), !projectDir.unicodeScalars.contains(where: { $0.value == 0 })
        else { return nil }
        let slug = projectDir.replacingOccurrences(of: "/", with: "-")
        guard !slug.isEmpty, slug != ".", slug != "..", slug.count <= 512,
              slug.unicodeScalars.allSatisfy({ canonicalCharacters.contains($0) }) else { return nil }
        return slug
    }

    /// The only scratchpad path constructor. Ledger text is never used as an operation target.
    static func scratchpadPath(projectDir: String, sessionID: String) -> String? {
        guard canonicalUUID(sessionID), let slug = projectSlug(projectDir) else { return nil }
        return scratchRoot.appendingPathComponent(slug, isDirectory: true)
            .appendingPathComponent(sessionID, isDirectory: true).path
    }

    /// Idempotently append an ownership receipt. A false answer means no caller may claim that
    /// this proof was registered; a later polling beat may safely retry it.
    static func register(taskID: String, assistant: Assistant, sessionID: String,
                         projectDir: String, at: Date = Date()) -> Bool {
        guard assistant == .claude, canonicalUUID(taskID),
              let path = scratchpadPath(projectDir: projectDir, sessionID: sessionID) else {
            return false
        }
        let entry = Entry(at: at, taskID: taskID, assistant: assistant.rawValue,
                          sessionID: sessionID, path: path, proof: "briefing_marker",
                          projectDir: projectDir)
        ledgerLock.lock(); defer { ledgerLock.unlock() }
        switch readLedgerUnlocked() {
        case .unreadable:
            return false
        case .known(let entries, _):
            if entries.contains(where: {
                $0.taskID == taskID && $0.sessionID == sessionID && $0.path == path
                    && $0.proof == "briefing_marker"
            }) { return true }
        }
        guard let data = encoded(entry) else { return false }
        let manager = FileManager.default
        do {
            try manager.createDirectory(at: ledgerURL.deletingLastPathComponent(),
                                        withIntermediateDirectories: true)
            if !manager.fileExists(atPath: ledgerURL.path) {
                guard manager.createFile(atPath: ledgerURL.path, contents: nil,
                                         attributes: [.posixPermissions: 0o600]) else { return false }
            }
            let handle = try FileHandle(forWritingTo: ledgerURL)
            defer { try? handle.close() }
            try handle.seekToEnd()
            try handle.write(contentsOf: data)
            try manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: ledgerURL.path)
            return true
        } catch {
            return false
        }
    }

    static func readLedger() -> LedgerRead {
        ledgerLock.lock(); defer { ledgerLock.unlock() }
        return readLedgerUnlocked()
    }

    private static func readLedgerUnlocked() -> LedgerRead {
        let data: Data
        do {
            data = try Data(contentsOf: ledgerURL)
        } catch {
            let ns = error as NSError
            if ns.domain == NSCocoaErrorDomain && ns.code == CocoaError.fileReadNoSuchFile.rawValue {
                return .known([], malformedLines: [])
            }
            return .unreadable
        }
        guard let text = String(data: data, encoding: .utf8) else { return .unreadable }
        var entries: [Entry] = []
        var malformed: [Int] = []
        for (offset, raw) in text.split(separator: "\n", omittingEmptySubsequences: false).enumerated() {
            guard !raw.isEmpty else { continue }
            let line = offset + 1
            guard let data = String(raw).data(using: .utf8), let entry = parse(data) else {
                malformed.append(line)
                continue
            }
            entries.append(entry)
        }
        return .known(entries, malformedLines: malformed)
    }

    private static func encoded(_ entry: Entry) -> Data? {
        let object: [String: Any] = [
            "at": entry.at.timeIntervalSince1970,
            "op": "own",
            "kind": "scratchpad",
            "task": entry.taskID,
            "session": entry.sessionID,
            "path": entry.path,
            "proof": entry.proof,
            "assistant": entry.assistant,
            "project_dir": entry.projectDir,
        ]
        guard var data = try? JSONSerialization.data(withJSONObject: object,
                                                      options: [.sortedKeys, .withoutEscapingSlashes])
        else { return nil }
        data.append(0x0A)
        return data
    }

    private static func parse(_ data: Data) -> Entry? {
        guard let object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              object["op"] as? String == "own", object["kind"] as? String == "scratchpad",
              let atNumber = object["at"] as? NSNumber,
              CFGetTypeID(atNumber) != CFBooleanGetTypeID(),
              let taskID = object["task"] as? String, canonicalUUID(taskID),
              let assistant = object["assistant"] as? String, assistant == Assistant.claude.rawValue,
              let sessionID = object["session"] as? String, canonicalUUID(sessionID),
              let path = object["path"] as? String, path.hasPrefix("/"),
              let proof = object["proof"] as? String, proof == "briefing_marker",
              let projectDir = object["project_dir"] as? String,
              let expected = scratchpadPath(projectDir: projectDir, sessionID: sessionID),
              expected == path else { return nil }
        let seconds = atNumber.doubleValue
        guard seconds.isFinite, seconds >= 0 else { return nil }
        return Entry(at: Date(timeIntervalSince1970: seconds), taskID: taskID,
                     assistant: assistant, sessionID: sessionID, path: path,
                     proof: proof, projectDir: projectDir)
    }

    /// Compact only a fully readable ledger. A malformed line is an ownership fact this build
    /// cannot interpret, so rewriting around it would destroy evidence and is refused.
    @discardableResult
    static func compact(now: Date = Date()) -> Bool {
        ledgerLock.lock(); defer { ledgerLock.unlock() }
        guard case .known(let entries, let malformed) = readLedgerUnlocked(), malformed.isEmpty
        else { return false }
        let cutoff = now.addingTimeInterval(-30 * 24 * 3600)
        let kept = entries.filter { $0.at >= cutoff || FileManager.default.fileExists(atPath: $0.path) }
        guard kept.count != entries.count else { return true }
        var data = Data()
        for entry in kept {
            guard let line = encoded(entry) else { return false }
            data.append(line)
        }
        do {
            try data.write(to: ledgerURL, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                                  ofItemAtPath: ledgerURL.path)
            return true
        } catch {
            return false
        }
    }

    static func liveSessions(in directory: URL = sessionsDirectory) -> Liveness {
        let names: [String]
        do {
            names = try FileManager.default.contentsOfDirectory(atPath: directory.path)
        } catch {
            return .unreadable
        }
        var ids: Set<String> = []
        for name in names where name.hasSuffix(".json") {
            let url = directory.appendingPathComponent(name)
            guard let data = try? Data(contentsOf: url),
                  let object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
                  let id = object["sessionId"] as? String, canonicalUUID(id) else {
                return .unreadable
            }
            ids.insert(id)
        }
        return .known(ids)
    }

    static func processStatus(pid: Int32?, recordedStart: Date?) -> ProcessStatus {
        guard let pid else { return recordedStart == nil ? .absent : .unreadable }
        guard pid > 0 else { return .unreadable }
        errno = 0
        if kill(pid, 0) != 0 {
            if errno == ESRCH { return .dead }
            if errno == EPERM { return .alive }
            return .unreadable
        }
        guard let recordedStart else { return .alive }
        guard let current = Targets.processStart(ofPID: pid) else { return .unreadable }
        return abs(current.timeIntervalSince(recordedStart)) <= SessionRegistry.startTolerance
            ? .alive : .reused
    }

    static func pathStatus(for entry: Entry) -> PathStatus {
        guard let expected = scratchpadPath(projectDir: entry.projectDir,
                                            sessionID: entry.sessionID), expected == entry.path
        else { return .invalid }
        let root = scratchRoot
        guard let slug = projectSlug(entry.projectDir) else { return .invalid }
        let project = root.appendingPathComponent(slug, isDirectory: true)
        let session = project.appendingPathComponent(entry.sessionID, isDirectory: true)
        for url in [root, project, session] {
            do {
                let attributes = try FileManager.default.attributesOfItem(atPath: url.path)
                guard attributes[.type] as? FileAttributeType == .typeDirectory else {
                    return attributes[.type] as? FileAttributeType == .typeSymbolicLink
                        ? .invalid : .unreadable
                }
            } catch {
                let ns = error as NSError
                if ns.domain == NSCocoaErrorDomain
                    && ns.code == CocoaError.fileReadNoSuchFile.rawValue { return .missing }
                return .unreadable
            }
        }
        return .valid
    }

    static func directorySize(at path: String) -> Source<Int> {
        let manager = FileManager.default
        var failed = false
        guard let enumerator = manager.enumerator(at: URL(fileURLWithPath: path),
            includingPropertiesForKeys: nil, options: [], errorHandler: { _, _ in
                failed = true
                return false
            }) else { return .unreadable }
        var total: UInt64 = 0
        while let url = enumerator.nextObject() as? URL {
            do {
                let attributes = try manager.attributesOfItem(atPath: url.path)
                if attributes[.type] as? FileAttributeType == .typeRegular,
                   let size = attributes[.size] as? NSNumber {
                    let addition = total.addingReportingOverflow(size.uint64Value)
                    if addition.overflow { failed = true; break }
                    total = addition.partialValue
                }
            } catch {
                failed = true
                break
            }
        }
        guard !failed, total <= UInt64(Int.max) else { return .unreadable }
        return .known(Int(total))
    }

    static func evaluate(_ input: EvaluationInput) -> Decision {
        let task: TaskFacts
        switch input.task {
        case .known(let value): task = value
        case .unreadable: return Decision(state: .unknown, why: "task_unreadable", eligibleAt: nil)
        }
        let landing: Landing
        switch input.landing {
        case .known(let value): landing = value
        case .unreadable:
            return Decision(state: .unknown, why: "landing_unreadable", eligibleAt: nil)
        }
        let retained: Bool
        switch input.retained {
        case .known(let value): retained = value
        case .unreadable:
            return Decision(state: .unknown, why: "retain_unreadable", eligibleAt: nil)
        }
        let live: Set<String>
        switch input.sessions {
        case .known(let value): live = value
        case .unreadable:
            return Decision(state: .unknown, why: "sessions_unreadable", eligibleAt: nil)
        }
        switch input.path {
        case .unreadable:
            return Decision(state: .unknown, why: "path_unreadable", eligibleAt: nil)
        case .invalid:
            return Decision(state: .unknown, why: "path_not_canonical", eligibleAt: nil)
        case .missing:
            return Decision(state: .held, why: "path_missing", eligibleAt: nil)
        case .valid:
            break
        }
        if input.process == .unreadable {
            return Decision(state: .unknown, why: "process_unreadable", eligibleAt: nil)
        }
        if (task.childPID == nil) != (input.process == .absent) {
            return Decision(state: .unknown, why: "process_inconsistent", eligibleAt: nil)
        }
        guard task.isTerminal else {
            return Decision(state: .held, why: "task_not_terminal", eligibleAt: nil)
        }
        guard landing != .pending else {
            return Decision(state: .held, why: "landing_pending", eligibleAt: nil)
        }
        guard !retained else {
            return Decision(state: .held, why: "retained", eligibleAt: nil)
        }
        guard !live.contains(input.entry.sessionID) else {
            return Decision(state: .held, why: "session_live", eligibleAt: nil)
        }
        guard input.process != .alive else {
            return Decision(state: .held, why: "process_alive", eligibleAt: nil)
        }
        let ordinaryFloor: TimeInterval = landing == .settled ? 3600 : 12 * 3600
        let floor = task.childProcStart == nil ? max(ordinaryFloor, 24 * 3600) : ordinaryFloor
        let finished = task.finishedAt ?? task.createdAt
        let eligibleAt = finished.addingTimeInterval(floor)
        guard eligibleAt <= input.now else {
            return Decision(state: .held, why: "floor", eligibleAt: eligibleAt)
        }
        return Decision(state: .releasable, why: "eligible", eligibleAt: eligibleAt)
    }

    // MARK: - The owned scratch root: scratch contract v1 (docs/scratch.md)
    //
    // The ledger above names paths a transcript proved. This names the one root whose entries carry
    // their own receipt, the marker. Neither enumerates `/tmp`: the only directory read here is the
    // owned root itself, and only its direct children.

    struct ScratchOwner: Equatable {
        let pid: Int32
        let processStart: Date
        let command: String
    }

    /// `<entry>/.clawdline-scratch.json` at version 1.
    struct ScratchMarker: Equatable {
        let createdAt: Date
        let purpose: String
        let owner: ScratchOwner?
        let keepUntil: Date?
    }

    enum ScratchMarkerRead: Equatable {
        case marker(ScratchMarker)
        case missing, unreadable, invalid, otherVersion
    }

    enum ScratchRootState: Equatable {
        case present, absent
        case refused(String)
    }

    enum ScratchEntryKind: Equatable {
        case directory(uid: uid_t, mode: mode_t)
        case symlink, other, unreadable
    }

    /// What one direct child of the root is, read without following anything.
    struct ScratchEntryFacts: Equatable {
        var name: String
        var kind: ScratchEntryKind
        var marker: ScratchMarkerRead
        var owner: ProcessStatus
    }

    struct ScratchRow {
        let name: String
        let path: String
        let isDirectory: Bool
        let marker: ScratchMarker?
        let decision: Decision
    }

    struct ScratchListing {
        let root: String
        let state: ScratchRootState
        /// Every direct child, each one judged. Nothing limits this: a limit on what is judged
        /// leaves whatever sorts past it unlisted and unswept on every pass for ever.
        let rows: [ScratchRow]
    }

    static let scratchMarkerName = ".clawdline-scratch.json"
    /// Entries `GET /v1/orchestrator/storage` puts in its body, first by name. It bounds that
    /// response and nothing else: every direct child is judged, counted and swept whatever it says.
    static let scratchListedEntryLimit = 1_000

    /// `[a-z0-9][a-z0-9-]{0,39}`.
    static func isScratchPurpose(_ value: String) -> Bool {
        let bytes = Array(value.utf8)
        func lowerOrDigit(_ byte: UInt8) -> Bool {
            (0x61...0x7a).contains(byte) || (0x30...0x39).contains(byte)
        }
        guard (1...40).contains(bytes.count), lowerOrDigit(bytes[0]) else { return false }
        return bytes.dropFirst().allSatisfy { lowerOrDigit($0) || $0 == 0x2d }
    }

    /// The purpose half of `<purpose>.<random>`, or `nil` when a name is not that shape. The
    /// random half is bounded to letters, digits, `_` and `-`, so no name can carry a separator.
    static func scratchEntryPurpose(_ name: String) -> String? {
        guard let dot = name.firstIndex(of: ".") else { return nil }
        let purpose = String(name[..<dot])
        let random = Array(name[name.index(after: dot)...].utf8)
        guard isScratchPurpose(purpose), (1...64).contains(random.count),
              random.allSatisfy({ byte in
                  (0x30...0x39).contains(byte) || (0x41...0x5a).contains(byte)
                      || (0x61...0x7a).contains(byte) || byte == 0x5f || byte == 0x2d
              }) else { return nil }
        return purpose
    }

    /// Every field the contract names, with its type; a missing `owner` or `keep_until` key is as
    /// invalid as a wrong one, because `null` is how the contract says "none".
    static func parseScratchMarker(_ data: Data) -> ScratchMarkerRead {
        guard data.count <= 65_536,
              let object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              let version = jsonNumber(object["clawdline_scratch"]) else { return .invalid }
        guard version == 1 else { return .otherVersion }
        guard let created = jsonNumber(object["created_at"]), created >= 0,
              let purpose = object["purpose"] as? String, isScratchPurpose(purpose),
              let rawOwner = object["owner"], let rawKeepUntil = object["keep_until"] else {
            return .invalid
        }
        var owner: ScratchOwner?
        if !(rawOwner is NSNull) {
            guard let fields = rawOwner as? [String: Any], let pid = jsonNumber(fields["pid"]),
                  pid == pid.rounded(), pid > 0, pid <= Double(Int32.max),
                  let started = jsonNumber(fields["process_start"]), started >= 0,
                  let command = fields["command"] as? String, !command.isEmpty,
                  !command.contains("/") else { return .invalid }
            owner = ScratchOwner(pid: Int32(pid), processStart: Date(timeIntervalSince1970: started),
                                 command: command)
        }
        var keepUntil: Date?
        if !(rawKeepUntil is NSNull) {
            guard let seconds = jsonNumber(rawKeepUntil), seconds >= 0 else { return .invalid }
            keepUntil = Date(timeIntervalSince1970: seconds)
        }
        return .marker(ScratchMarker(createdAt: Date(timeIntervalSince1970: created),
                                     purpose: purpose, owner: owner, keepUntil: keepUntil))
    }

    /// A JSON number that is finite and is not a boolean.
    private static func jsonNumber(_ value: Any?) -> Double? {
        guard let number = value as? NSNumber, CFGetTypeID(number) != CFBooleanGetTypeID() else {
            return nil
        }
        return number.doubleValue.isFinite ? number.doubleValue : nil
    }

    /// The root, read with `lstat` so a symlink is seen rather than followed. A trailing slash is
    /// dropped first, because `lstat` on `link/` follows the link; a `.` or `..` component is
    /// refused rather than read, because `lstat` on `link/.` follows the link just the same.
    static func scratchRootState(_ root: String, uid: uid_t = getuid()) -> ScratchRootState {
        let path = trimmedScratchRoot(root)
        guard path.hasPrefix("/") else { return .refused("root_not_absolute") }
        guard !hasDotComponent(path) else { return .refused("root_not_normalized") }
        var info = stat()
        guard lstat(path, &info) == 0 else {
            return errno == ENOENT ? .absent : .refused("root_unreadable")
        }
        switch info.st_mode & S_IFMT {
        case S_IFLNK: return .refused("root_symlink")
        case S_IFDIR: return info.st_uid == uid ? .present : .refused("root_not_owned")
        default: return .refused("root_not_directory")
        }
    }

    private static func trimmedScratchRoot(_ root: String) -> String {
        var path = root
        while path.count > 1, path.hasSuffix("/") { path.removeLast() }
        return path
    }

    /// Whether a path names a `.` or `..` component. The kernel resolves one physically, through
    /// whatever symlink stands before it, so that spelling reaches a place a check made on the
    /// spelling never looked at.
    private static func hasDotComponent(_ path: String) -> Bool {
        path.split(separator: "/").contains { $0 == "." || $0 == ".." }
    }

    /// `realpath(3)`: the physical spelling of a path that exists, or `nil`.
    private static func resolvedPath(_ path: String) -> String? {
        guard let resolved = realpath(path, nil) else { return nil }
        defer { free(resolved) }
        return String(cString: resolved)
    }

    /// The components of `path` below `base`, compared byte for byte, or `nil` when `path` is not
    /// spelled below it. `String.hasPrefix` compares characters, and two byte spellings of one
    /// character are two names to a filesystem that does not normalize them.
    static func componentsBelow(_ base: String, in path: String) -> [String]? {
        let prefix = Array((base + "/").utf8)
        let bytes = Array(path.utf8)
        guard bytes.count > prefix.count, bytes.starts(with: prefix) else { return nil }
        return String(decoding: bytes[prefix.count...], as: UTF8.self)
            .split(separator: "/", omittingEmptySubsequences: false).map(String.init)
    }

    enum ContainedDirectory: Equatable {
        /// Every component from the root down, the root included, is a real directory, and the
        /// path still resolves inside the root.
        case proven
        /// A component is not there, so there is nothing to remove.
        case missing
        /// Anything else, with its reason. Never removed.
        case refused(String)

        /// The reason an audit names, or `nil` when the path was proved.
        var refusal: String? {
            switch self {
            case .proven: return nil
            case .missing: return "missing"
            case .refused(let why): return why
            }
        }
    }

    /// The check every reclaim removal makes immediately before it removes. Its order is the
    /// check:
    ///
    /// 1. **The root as spelled** is `lstat`ed first, whatever spelling `path` uses, and must be a
    ///    real directory: a symlink root is `root_symlink`. Its own ancestors may be symlinks
    ///    (`/tmp`, `/var`); a `.` or `..` in its spelling is `root_not_normalized`, because `lstat`
    ///    on `link/.` follows the link.
    /// 2. Only then is `path`'s spelling chosen, byte for byte: below the root as spelled, or below
    ///    the root's resolved spelling — and a resolved spelling is walked only once `lstat` finds
    ///    it to be the very directory step 1 proved, the same device and inode.
    /// 3. Every component below it is `lstat`ed in turn and must be a real directory, so a symlink
    ///    at any level is seen rather than followed.
    /// 4. The path, resolved, must lie inside the root, resolved.
    ///
    /// `lstat` on the last component alone is not this check. The kernel follows every component
    /// above it, so a task directory replaced by a symlink hands that `lstat` a real `work/`
    /// somewhere else, and the recursive removal after it lands there too.
    static func containedDirectory(_ path: String, under root: String) -> ContainedDirectory {
        containment(path, under: root).verdict
    }

    /// ``containedDirectory(_:under:)`` with the components it proved below the root, for a guard
    /// that also has to know how deep the path sits. `below` is empty unless the path is proven.
    static func containment(_ path: String, under root: String)
        -> (verdict: ContainedDirectory, below: [String]) {
        func refused(_ why: String) -> (verdict: ContainedDirectory, below: [String]) {
            (.refused(why), [])
        }
        let rootPath = trimmedScratchRoot(root)
        let target = trimmedScratchRoot(path)
        guard rootPath.hasPrefix("/"), rootPath != "/" else { return refused("root_not_absolute") }
        guard !hasDotComponent(rootPath) else { return refused("root_not_normalized") }
        var rootInfo = stat()
        guard lstat(rootPath, &rootInfo) == 0 else {
            return errno == ENOENT ? (.missing, []) : refused("unreadable")
        }
        switch rootInfo.st_mode & S_IFMT {
        case S_IFDIR: break
        case S_IFLNK: return refused("root_symlink")
        default: return refused("root_not_directory")
        }
        guard let resolvedRoot = resolvedPath(rootPath) else { return refused("unreadable") }
        let spellings = [rootPath, resolvedRoot, OrchestratorDraft.canonicalFilesystemPath(rootPath)]
        guard let index = spellings.firstIndex(where: { componentsBelow($0, in: target) != nil }),
              let below = componentsBelow(spellings[index], in: target) else {
            return refused("outside_root")
        }
        guard !below.contains(where: { $0.isEmpty || $0 == "." || $0 == ".." }) else {
            return refused("outside_root")
        }
        var info = stat()
        if index > 0 {
            // Another spelling of the root is walked only as the very directory just proved.
            guard lstat(spellings[index], &info) == 0 else {
                return errno == ENOENT ? (.missing, []) : refused("unreadable")
            }
            guard (info.st_mode & S_IFMT) == S_IFDIR, info.st_dev == rootInfo.st_dev,
                  info.st_ino == rootInfo.st_ino else { return refused("outside_root") }
        }
        var walked = spellings[index]
        for component in below {
            walked += "/" + component
            guard lstat(walked, &info) == 0 else {
                return errno == ENOENT ? (.missing, []) : refused("unreadable")
            }
            switch info.st_mode & S_IFMT {
            case S_IFDIR: continue
            case S_IFLNK: return refused("symlink")
            default: return refused("not_directory")
            }
        }
        guard let resolved = resolvedPath(walked), componentsBelow(resolvedRoot, in: resolved) != nil
        else { return refused("outside_root") }
        return (.proven, below)
    }

    static func scratchEntryFacts(root: String, name: String,
                                  ownerStatus: (ScratchOwner) -> ProcessStatus = scratchOwnerStatus)
        -> ScratchEntryFacts {
        let path = trimmedScratchRoot(root) + "/" + name
        var facts = ScratchEntryFacts(name: name, kind: .unreadable, marker: .unreadable,
                                      owner: .absent)
        var info = stat()
        guard lstat(path, &info) == 0 else { return facts }
        switch info.st_mode & S_IFMT {
        case S_IFDIR: facts.kind = .directory(uid: info.st_uid, mode: info.st_mode & 0o7777)
        case S_IFLNK: facts.kind = .symlink; return facts
        default: facts.kind = .other; return facts
        }
        let markerPath = path + "/" + scratchMarkerName
        var markerInfo = stat()
        if lstat(markerPath, &markerInfo) != 0 {
            facts.marker = errno == ENOENT ? .missing : .unreadable
        } else if (markerInfo.st_mode & S_IFMT) == S_IFREG, markerInfo.st_size <= 65_536,
                  let data = FileManager.default.contents(atPath: markerPath) {
            facts.marker = parseScratchMarker(data)
        }
        if case .marker(let marker) = facts.marker, let owner = marker.owner {
            facts.owner = ownerStatus(owner)
        }
        return facts
    }

    /// Contract v1's liveness: the pid is running and its start time equals the marker's at
    /// whole-second resolution, ±1 s. Process identity only — never the command name, which the
    /// kernel reports for Claude Code as its version string.
    static func scratchOwnerStatus(_ owner: ScratchOwner) -> ProcessStatus {
        errno = 0
        if kill(owner.pid, 0) != 0 {
            if errno == ESRCH { return .dead }
            if errno == EPERM { return .alive }
            return .unreadable
        }
        guard let started = Targets.processStart(ofPID: owner.pid) else { return .unreadable }
        let difference = abs(started.timeIntervalSince1970.rounded(.down)
                             - owner.processStart.timeIntervalSince1970.rounded(.down))
        return difference <= 1 ? .alive : .reused
    }

    /// Contract v1, three-valued. `unknown` — a name, entry or marker the contract does not
    /// describe, or an owner whose liveness cannot be read — is never collected. `held` is an owner
    /// still running, a `keep_until` still ahead, or the broker's own grace, which counts from the
    /// later of `created_at` and `keep_until` because nothing records when an owner died.
    static func evaluateScratch(_ facts: ScratchEntryFacts, uid: uid_t = getuid(), now: Date,
                                graceMinutes: Int) -> Decision {
        func unknown(_ why: String) -> Decision { Decision(state: .unknown, why: why, eligibleAt: nil) }
        guard let purpose = scratchEntryPurpose(facts.name) else { return unknown("name_not_contract") }
        switch facts.kind {
        case .unreadable: return unknown("entry_unreadable")
        case .symlink: return unknown("entry_symlink")
        case .other: return unknown("entry_not_directory")
        case .directory(let owner, let mode):
            guard owner == uid else { return unknown("entry_not_owned") }
            guard mode & 0o777 == 0o700 else { return unknown("entry_mode_not_0700") }
        }
        let marker: ScratchMarker
        switch facts.marker {
        case .missing: return unknown("marker_missing")
        case .unreadable: return unknown("marker_unreadable")
        case .invalid: return unknown("marker_invalid")
        case .otherVersion: return unknown("marker_version")
        case .marker(let value): marker = value
        }
        guard marker.purpose == purpose else { return unknown("marker_purpose_mismatch") }
        switch facts.owner {
        case .unreadable: return unknown("owner_unreadable")
        case .alive: return Decision(state: .held, why: "owner_alive", eligibleAt: nil)
        case .absent, .dead, .reused: break
        }
        guard graceMinutes >= 0 else {
            return Decision(state: .held, why: "grace_disabled", eligibleAt: nil)
        }
        let from = max(marker.createdAt, marker.keepUntil ?? marker.createdAt)
        let eligibleAt = from.addingTimeInterval(TimeInterval(graceMinutes * 60))
        if let keepUntil = marker.keepUntil, keepUntil > now {
            return Decision(state: .held, why: "keep_until", eligibleAt: eligibleAt)
        }
        guard eligibleAt <= now else {
            return Decision(state: .held, why: "grace", eligibleAt: eligibleAt)
        }
        return Decision(state: .releasable, why: "eligible", eligibleAt: eligibleAt)
    }

    /// The current working directory of every process this user runs, from one `lsof`, or `nil`
    /// when that could not be read.
    ///
    /// A `snapshot-run` killed with `SIGKILL` can leave its command running inside the entry while
    /// the marker names a dead owner, so the marker alone reads releasable. An entry some process
    /// is working in is therefore held. A file merely held open from elsewhere is not seen; that is
    /// the documented limit, and the grace period is what covers it.
    static func processWorkingDirectories() -> Set<String>? {
        guard let answer = Process.collect(
                "/usr/sbin/lsof", ["-w", "-a", "-u", String(getuid()), "-d", "cwd", "-Fn"],
                environment: ["LC_ALL": "en_US.UTF-8", "PATH": "/usr/bin:/bin:/usr/sbin"],
                timeout: 30),
              let text = String(data: answer.output, encoding: .utf8) else { return nil }
        let paths = Set(text.split(separator: "\n").filter { $0.hasPrefix("n/") }
            .map { String($0.dropFirst()) })
        return answer.status == 0 || !paths.isEmpty ? paths : nil
    }

    /// Every direct child of the root with its decision — every one, however many there are, so an
    /// entry never waits behind a thousand others that are held or unknown. A releasable entry is
    /// then asked one more question the marker cannot answer — is a live process working inside
    /// it — and the process table is read at most once per listing, only when something is
    /// releasable.
    static func scratchListing(root: String, now: Date, graceMinutes: Int, uid: uid_t = getuid(),
                               ownerStatus: (ScratchOwner) -> ProcessStatus = scratchOwnerStatus,
                               workingDirectories: () -> Set<String>? = processWorkingDirectories)
        -> ScratchListing {
        let path = trimmedScratchRoot(root)
        let state = scratchRootState(path, uid: uid)
        guard state == .present else {
            return ScratchListing(root: path, state: state, rows: [])
        }
        guard let names = try? FileManager.default.contentsOfDirectory(atPath: path) else {
            return ScratchListing(root: path, state: .refused("root_unreadable"), rows: [])
        }
        var cwds: Set<String>?? = nil
        let rows = names.sorted().map { name -> ScratchRow in
            let facts = scratchEntryFacts(root: path, name: name, ownerStatus: ownerStatus)
            var decision = evaluateScratch(facts, uid: uid, now: now, graceMinutes: graceMinutes)
            let entry = path + "/" + name
            if decision.mayCollect {
                if cwds == nil { cwds = .some(workingDirectories()) }
                if let known = cwds ?? nil {
                    if workingInside(entry, known) {
                        decision = Decision(state: .held, why: "cwd_in_use", eligibleAt: nil)
                    }
                } else {
                    decision = Decision(state: .unknown, why: "cwd_unreadable", eligibleAt: nil)
                }
            }
            var marker: ScratchMarker?
            if case .marker(let value) = facts.marker { marker = value }
            var isDirectory = false
            if case .directory = facts.kind { isDirectory = true }
            return ScratchRow(name: name, path: entry, isDirectory: isDirectory, marker: marker,
                              decision: decision)
        }
        return ScratchListing(root: path, state: state, rows: rows)
    }

    /// Whether any of `directories` is `entry` or inside it, compared both as spelled and through
    /// `realpath`, because `lsof` names `/tmp/x` as `/private/tmp/x`.
    private static func workingInside(_ entry: String, _ directories: Set<String>) -> Bool {
        let spellings = [entry] + (resolvedPath(entry).map { [$0] } ?? [])
        return directories.contains { directory in
            spellings.contains { directory == $0 || directory.hasPrefix($0 + "/") }
        }
    }

    /// Why a removal inside the owned root stopped before removing anything more.
    private struct ScratchRemovalRefused: Error {
        let why: String
    }

    /// Releasable entries one pass takes up. The rest wait for a later pass, which begins after the
    /// last name this one took (``ScratchSweepCursor``), so every releasable entry is taken up within
    /// ⌈releasable ÷ this⌉ passes of one app run.
    static let scratchSweepPassLimit = 64
    /// Entries one reading of the working directories answers for. A pass reads them in its listing
    /// and once for each batch, immediately before that batch's removals: at most
    /// 1 + ⌈``scratchSweepPassLimit`` ÷ this⌉ `lsof` runs a pass, however many entries the root holds.
    static let scratchSweepBatchSize = 16

    /// Where the next pass over each scratch root begins: after the last name a pass took up. An
    /// entry that keeps failing to go is then passed over, rather than taken up first on every pass
    /// ahead of entries that would go. Kept in memory, so a restart begins again at the first name.
    final class ScratchSweepCursor {
        static let shared = ScratchSweepCursor()
        private let lock = NSLock()
        private var after: [String: String] = [:]

        func position(_ root: String) -> String? {
            lock.lock()
            defer { lock.unlock() }
            return after[root]
        }

        func advance(_ root: String, past name: String) {
            lock.lock()
            defer { lock.unlock() }
            after[root] = name
        }
    }

    /// Remove releasable entries — at most ``scratchSweepPassLimit`` a pass, in name order from the
    /// cursor on and wrapping to the front — reading again at the removal everything the listing
    /// decided from: each entry's facts; then, once for each batch of ``scratchSweepBatchSize`` and
    /// immediately before its removals, the working directories of this user's processes, so a
    /// process that entered an entry after the listing looked holds it, and a table that cannot be
    /// read holds the whole batch and ends the pass; and, before every read and removal inside an
    /// entry, its path from the root down, so a root or entry swapped for a symlink since the listing
    /// is refused, never followed. The payload goes first and the marker last, so an entry a removal
    /// only half finished is still a releasable entry a later pass retries, rather than an `unknown`
    /// one nothing will touch.
    @discardableResult
    static func sweepScratch(root: String, now: Date, graceMinutes: Int, uid: uid_t = getuid(),
                             ownerStatus: (ScratchOwner) -> ProcessStatus = scratchOwnerStatus,
                             workingDirectories: () -> Set<String>? = processWorkingDirectories,
                             cursor: ScratchSweepCursor = .shared)
        -> [String] {
        let listing = scratchListing(root: root, now: now, graceMinutes: graceMinutes, uid: uid,
                                     ownerStatus: ownerStatus,
                                     workingDirectories: workingDirectories)
        if case .refused(let why) = listing.state {
            RemoteAuth.audit("orchestrator.scratch.root_refused", ["root": listing.root, "why": why])
            return []
        }
        for row in listing.rows where row.decision.state == .unknown {
            RemoteAuth.audit("orchestrator.scratch.kept", ["path": row.path, "why": row.decision.why])
        }
        let releasable = listing.rows.filter { $0.decision.mayCollect }
        let taken = Array((cursor.position(listing.root).map { after in
            releasable.filter { $0.name > after } + releasable.filter { $0.name <= after }
        } ?? releasable).prefix(scratchSweepPassLimit))
        let manager = FileManager.default
        var removed: [String] = []
        func proved(_ entry: String) throws -> String {
            if let why = containedDirectory(entry, under: listing.root).refusal {
                throw ScratchRemovalRefused(why: why)
            }
            return entry
        }
        for start in stride(from: 0, to: taken.count, by: scratchSweepBatchSize) {
            let batch = taken[start..<min(start + scratchSweepBatchSize, taken.count)]
            let collectable = batch.compactMap { row -> (row: ScratchRow, why: String)? in
                let again = evaluateScratch(
                    scratchEntryFacts(root: listing.root, name: row.name, ownerStatus: ownerStatus),
                    uid: uid, now: now, graceMinutes: graceMinutes)
                guard again.mayCollect else { return nil }
                return (row: row, why: again.why)
            }
            if !collectable.isEmpty {
                guard let working = workingDirectories() else {
                    for entry in collectable {
                        RemoteAuth.audit("orchestrator.scratch.kept",
                                         ["path": entry.row.path, "why": "cwd_unreadable"])
                    }
                    return removed
                }
                for entry in collectable where !workingInside(entry.row.path, working) {
                    let row = entry.row
                    do {
                        for child in try manager.contentsOfDirectory(atPath: proved(row.path))
                        where child != scratchMarkerName {
                            try manager.removeItem(atPath: proved(row.path) + "/" + child)
                        }
                        try manager.removeItem(atPath: proved(row.path))
                        removed.append(row.path)
                        RemoteAuth.audit("orchestrator.scratch.reclaimed", [
                            "path": row.path, "purpose": row.marker?.purpose ?? "", "why": entry.why,
                        ])
                    } catch let refusal as ScratchRemovalRefused {
                        RemoteAuth.audit("orchestrator.scratch.kept", ["path": row.path, "why": refusal.why])
                    } catch {
                        RemoteAuth.audit("orchestrator.scratch.kept", ["path": row.path, "why": "remove_failed"])
                    }
                }
            }
            if let last = batch.last { cursor.advance(listing.root, past: last.name) }
        }
        return removed
    }

    /// The owned scratch root as `GET /v1/orchestrator/storage` lists it: every direct child judged
    /// and counted by state in `totals`, beside the ledger rows and never folded into their totals.
    /// The body lists and sizes the first ``scratchListedEntryLimit`` entries by name and says
    /// `truncated` past that; an entry beyond it still counts by state, and in `unknown_size_items`,
    /// because this response did not size it.
    static func scratchInventory(root: String = Orchestrator.reclaimRoots.scratch, now: Date,
                                 graceMinutes: Int = Config.shared.orchestratorScratchGraceMinutes,
                                 workingDirectories: () -> Set<String>? = processWorkingDirectories)
        -> [String: Any] {
        let listing = scratchListing(root: root, now: now, graceMinutes: graceMinutes,
                                     workingDirectories: workingDirectories)
        let listed = listing.rows.prefix(scratchListedEntryLimit)
        var totals = ["items": 0, "bytes": 0, "held_items": 0, "releasable_items": 0,
                      "unknown_items": 0, "unknown_size_items": listing.rows.count - listed.count]
        for row in listing.rows {
            totals["items", default: 0] += 1
            totals["\(row.decision.state.rawValue)_items", default: 0] += 1
        }
        let entries = listed.map { row -> [String: Any] in
            var bytes: Any = NSNull()
            if row.isDirectory, case .known(let size) = directorySize(at: row.path) {
                bytes = size
                totals["bytes", default: 0] += size
            } else {
                totals["unknown_size_items", default: 0] += 1
            }
            let recorded = row.marker?.owner.map { owner -> [String: Any] in
                ["pid": Int(owner.pid), "process_start": Int(owner.processStart.timeIntervalSince1970),
                 "command": owner.command]
            }
            let owner: Any = recorded as Any? ?? NSNull()
            return [
                "name": row.name, "path": row.path, "state": row.decision.state.rawValue,
                "why": row.decision.why, "bytes": bytes, "owner": owner,
                "purpose": row.marker.map { $0.purpose as Any } ?? NSNull(),
                "created_at": row.marker.map { Int($0.createdAt.timeIntervalSince1970) as Any } ?? NSNull(),
                "keep_until": row.marker?.keepUntil.map { Int($0.timeIntervalSince1970) as Any } ?? NSNull(),
                "eligible_at": row.decision.eligibleAt.map { Int($0.timeIntervalSince1970) as Any } ?? NSNull(),
            ]
        }
        var why: Any = NSNull()
        let state: String
        switch listing.state {
        case .present: state = "present"
        case .absent: state = "absent"
        case .refused(let reason): state = "refused"; why = reason
        }
        return ["root": listing.root, "root_state": state, "why": why, "grace_minutes": graceMinutes,
                "entries": entries, "totals": totals, "truncated": listed.count < listing.rows.count]
    }
}
