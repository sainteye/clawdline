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
}
