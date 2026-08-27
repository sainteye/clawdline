import Foundation
import Darwin

/// The one explicitly registered machine coordinator.
///
/// This is deliberately a separate identity ledger rather than a title, task-root or ancestry
/// flag. A row receives the optional coordinator projection only when every process-bound fact
/// recorded here still agrees with the identity used by task mounting and Session work receipts.
enum Coordinator {

    static let recordVersion = 1
    static let label = "Clawdfather"
    static let scope = "machine"

    struct LiveSession {
        let identity: Orchestrator.SessionWorkIdentity
        let label: String
        let cwd: String?
        let workState: Orchestrator.SessionWorkState
        let waitingOnSession: Bool
        let hasWaiters: Bool
    }

    struct BearingsInput {
        let sessionsFresh: Bool
        let activeTaskCount: Int
        let pendingLandingCount: Int
        let openWaitCount: Int
        let sessionsObservedAt: Date?
        let registryObservedAt: Date?
        let sessionsGeneration: Int?

        init(sessionsFresh: Bool, activeTaskCount: Int, pendingLandingCount: Int,
             openWaitCount: Int, sessionsObservedAt: Date? = nil,
             registryObservedAt: Date? = nil, sessionsGeneration: Int? = nil) {
            self.sessionsFresh = sessionsFresh
            self.activeTaskCount = activeTaskCount
            self.pendingLandingCount = pendingLandingCount
            self.openWaitCount = openWaitCount
            self.sessionsObservedAt = sessionsObservedAt
            self.registryObservedAt = registryObservedAt
            self.sessionsGeneration = sessionsGeneration
        }
    }

    private struct Record: Codable, Equatable {
        let version: Int
        let id: String
        let scope: String
        let label: String
        let registeredAt: Double
        let sessionID: String
        let assistant: String
        let tty: String
        let pid: Int32
        let processStart: Double
        let conversationID: String
        let sessionLabel: String
        let cwd: String?
        /// Optional so the A1 version-1 record remains readable. Absence means generation one.
        let generation: Int?
        let reboundAt: Double?
    }

    private enum Loaded {
        case absent
        case valid(Record)
        case corrupt
        case unsupported

        var status: String {
            switch self {
            case .absent: return "absent"
            case .valid: return "ready"
            case .corrupt: return "corrupt"
            case .unsupported: return "unsupported"
            }
        }
    }

    private enum StoreFingerprint: Equatable {
        case absent
        case inaccessible(Int32)
        case present(device: UInt64, inode: UInt64, size: Int64,
                     seconds: Int64, nanoseconds: Int64, regular: Bool)
    }

    /// Every in-process read and cache transition uses this lock. Registration has its own
    /// higher-level gate, then takes the on-disk flock before forcing a fresh read.
    private static let lock = NSLock()
    private static let registrationLock = NSLock()
    private static var loadedURL: URL?
    private static var loadedFingerprint: StoreFingerprint?
    private static var cached: Loaded?

    /// Tests point this at task-owned temporary storage; production remains under Clawdline's
    /// existing private configuration directory.
    static var storeURLOverrideForTesting: URL?
    static var storeURL: URL {
        storeURLOverrideForTesting
            ?? RemoteAuth.directory.appendingPathComponent("coordinator.json")
    }
    static var registrationLockURL: URL { storeURL.appendingPathExtension("lock") }

    static func forgetForTesting() {
        lock.lock()
        loadedURL = nil
        loadedFingerprint = nil
        cached = nil
        lock.unlock()
    }

    /// Explicit construction. The candidate must itself occur in the caller's current live
    /// inventory; this prevents a unit or future call site from registering a synthetic tuple.
    static func register(_ candidate: LiveSession, among liveSessions: [LiveSession],
                         now: Date = Date(), makeID: () -> UUID = UUID.init)
        -> Orchestrator.Reply {
        let registeredAt = now.timeIntervalSince1970
        guard valid(candidate), liveSessions.contains(where: {
            exact($0.identity, candidate.identity)
        }) else {
            return .refused(409, "session_unbound",
                            "That session has no complete process-bound assistant identity.")
        }
        guard representableTimestamp(registeredAt) else {
            return .refused(500, "coordinator_store_failed",
                            "The coordinator lifecycle timestamp cannot be represented.")
        }

        registrationLock.lock(); defer { registrationLock.unlock() }
        guard let reply = withExclusiveRegistrationLock({ () -> Orchestrator.Reply in
            switch load(force: true) {
            case .valid(let existing):
                if matches(existing, candidate.identity) {
                    return .ok(["ok": true, "created": false,
                                "coordinator": coordinatorMetadata(existing, among: liveSessions)])
                }
                return .refused(status: 409, code: "coordinator_exists",
                                message: "A different machine coordinator is already registered; "
                                    + "registration is never a takeover operation.",
                                extra: ["coordinator": coordinatorMetadata(
                                    existing, among: liveSessions)])
            case .corrupt, .unsupported:
                return .refused(409, "coordinator_store_invalid",
                                "The durable coordinator record is unreadable or from an unknown "
                                    + "version; it was not replaced.")
            case .absent:
                let identity = candidate.identity
                let record = Record(
                    version: recordVersion, id: makeID().uuidString.lowercased(), scope: scope,
                    label: label, registeredAt: registeredAt,
                    sessionID: identity.terminalID,
                    assistant: identity.assistant!.rawValue, tty: identity.tty,
                    pid: identity.pid!, processStart: identity.processStart!.timeIntervalSince1970,
                    conversationID: identity.conversationID!,
                    sessionLabel: bounded(candidate.label, maximum: 512) ?? "Session",
                    cwd: candidate.cwd.flatMap { bounded($0, maximum: 4_096) },
                    generation: 1, reboundAt: nil)
                guard save(record) else {
                    return .refused(500, "coordinator_store_failed",
                                    "The coordinator record could not be written.")
                }
                return .ok(["ok": true, "created": true,
                            "coordinator": coordinatorMetadata(record, among: liveSessions)])
            }
        }) else {
            return .refused(500, "coordinator_store_failed",
                            "The coordinator registration lock could not be secured.")
        }
        return reply
    }

    /// Move the stable machine role to one exact live process only after a complete current
    /// inventory proves the old process-bound tuple absent. This is reconnect, not takeover:
    /// the caller must name the durable coordinator UUID it observed and an online binding wins.
    static func rebind(expectedCoordinatorID: String, expectedGeneration: Int,
                       to candidate: LiveSession,
                       among liveSessions: [LiveSession], sessionsFresh: Bool,
                       sessionsObservedAt: Date?,
                       now: Date = Date()) -> Orchestrator.Reply {
        guard valid(candidate), liveSessions.contains(where: {
            exact($0.identity, candidate.identity)
        }) else {
            return .refused(409, "session_unbound",
                            "That session has no complete process-bound assistant identity.")
        }

        registrationLock.lock(); defer { registrationLock.unlock() }
        guard let reply = withExclusiveRegistrationLock({ () -> Orchestrator.Reply in
            switch load(force: true) {
            case .absent:
                return .refused(409, "coordinator_not_configured",
                                "No durable machine coordinator exists to reconnect.")
            case .corrupt, .unsupported:
                return .refused(409, "coordinator_store_invalid",
                                "The durable coordinator record is unreadable or from an unknown "
                                    + "version; it was not replaced.")
            case .valid(let existing):
                guard UUID(uuidString: expectedCoordinatorID)?.uuidString.lowercased()
                        == existing.id else {
                    return .refused(
                        status: 409, code: "coordinator_identity_mismatch",
                        message: "The durable coordinator changed after the caller observed it; "
                            + "refresh Bearings before reconnecting.",
                        extra: ["coordinator": coordinatorMetadata(
                            existing, among: liveSessions)])
                }
                let currentGeneration = existing.generation ?? 1
                guard expectedGeneration == currentGeneration else {
                    return .refused(
                        status: 409, code: "coordinator_generation_mismatch",
                        message: "The coordinator lifecycle advanced after the caller observed it; "
                            + "refresh Bearings before reconnecting.",
                        extra: ["coordinator": coordinatorMetadata(
                            existing, among: liveSessions)])
                }

                if matches(existing, candidate.identity) {
                    return .ok(["ok": true, "rebound": false,
                                "coordinator": coordinatorMetadata(
                                    existing, among: liveSessions)])
                }
                if liveSessions.contains(where: { matches(existing, $0.identity) }) {
                    return .refused(
                        status: 409, code: "coordinator_online",
                        message: "The exact current coordinator is still online; reconnect cannot "
                            + "take over a live binding.",
                        extra: ["coordinator": coordinatorMetadata(
                            existing, among: liveSessions)])
                }
                guard sessionsFresh else {
                    return .refused(
                        status: 409, code: "coordinator_liveness_unknown",
                        message: "The Session inventory is stale, so the old coordinator cannot "
                            + "be proved offline.",
                        extra: ["coordinator": coordinatorMetadata(
                            existing, among: liveSessions)])
                }
                let observedAt = sessionsObservedAt?.timeIntervalSince1970
                let bindingChangedAt = existing.reboundAt ?? existing.registeredAt
                let timeProvesNewer = observedAt.map {
                    representableTimestamp($0) && $0 >= bindingChangedAt
                } ?? false
                guard timeProvesNewer else {
                    return .refused(
                        status: 409, code: "coordinator_liveness_unknown",
                        message: "The Session inventory predates the current coordinator binding, "
                            + "so it cannot prove that binding offline.",
                        extra: ["coordinator": coordinatorMetadata(
                            existing, among: liveSessions)])
                }

                let oldGeneration = currentGeneration
                let reboundAt = now.timeIntervalSince1970
                guard oldGeneration < Int.max, representableTimestamp(reboundAt),
                      reboundAt >= bindingChangedAt,
                      observedAt.map({ reboundAt >= $0 }) ?? true else {
                    return .refused(409, "coordinator_store_invalid",
                                    "The durable coordinator lifecycle cannot be advanced.")
                }
                let identity = candidate.identity
                let rebound = Record(
                    version: existing.version, id: existing.id, scope: existing.scope,
                    label: existing.label, registeredAt: existing.registeredAt,
                    sessionID: identity.terminalID,
                    assistant: identity.assistant!.rawValue, tty: identity.tty,
                    pid: identity.pid!, processStart: identity.processStart!.timeIntervalSince1970,
                    conversationID: identity.conversationID!,
                    sessionLabel: bounded(candidate.label, maximum: 512) ?? "Session",
                    cwd: candidate.cwd.flatMap { bounded($0, maximum: 4_096) },
                    generation: oldGeneration + 1,
                    reboundAt: reboundAt)
                guard save(rebound) else {
                    return .refused(500, "coordinator_store_failed",
                                    "The coordinator reconnect could not be written.")
                }
                return .ok(["ok": true, "rebound": true,
                            "coordinator": coordinatorMetadata(rebound, among: liveSessions)])
            }
        }) else {
            return .refused(500, "coordinator_store_failed",
                            "The coordinator registration lock could not be secured.")
        }
        return reply
    }

    /// The only optional record accepted by the existing web renderer. Nothing else in a Session
    /// row changes, and an offline record is never attached to a merely similar live row.
    static func sessionProjection(for session: LiveSession) -> [String: Any]? {
        guard case .valid(let record) = load(), matches(record, session.identity) else { return nil }
        return ["label": label, "status": "online", "commands": commands]
    }

    /// Durable presence plus a deterministic read-only projection over current broker facts.
    static func inspection(liveSessions: [LiveSession], bearings input: BearingsInput,
                           now: Date = Date()) -> [String: Any] {
        let observed = Int(now.timeIntervalSince1970)
        let loaded = load()
        let coordinator: [String: Any]
        let lifecycle: String
        switch loaded {
        case .valid(let record):
            coordinator = coordinatorMetadata(record, among: liveSessions)
            lifecycle = coordinator["lifecycle"] as? String ?? "offline"
        case .absent, .corrupt, .unsupported:
            coordinator = ["configured": false, "status": "unregistered",
                           "lifecycle": "unregistered", "scope": scope, "label": label]
            lifecycle = "unregistered"
        }

        var counts: [String: Any] = [:]
        for state in Orchestrator.SessionWorkState.allCases { counts[state.rawValue] = 0 }
        for session in liveSessions {
            counts[session.workState.rawValue] = (counts[session.workState.rawValue] as? Int ?? 0) + 1
        }
        let sorted = liveSessions.sorted {
            $0.identity.terminalID < $1.identity.terminalID
        }
        let needsTriage = sorted.filter { $0.workState == .needsTriage }.map(safeSession)
        let waiting = sorted.filter {
            $0.workState == .waitingHuman || $0.workState == .waitingSession
        }.map(safeSession)
        let blocking = sorted.filter(\.hasWaiters).map(safeSession)

        let source: (String, String, Date?) -> [String: Any] = { provenance, freshness, at in
            ["observed_at": at.map { Int($0.timeIntervalSince1970) } ?? NSNull(),
             "provenance": provenance, "freshness": freshness]
        }
        var sessionSource = source("session_watch", input.sessionsFresh ? "current" : "stale",
                                   input.sessionsObservedAt)
        if let generation = input.sessionsGeneration {
            sessionSource["generation"] = generation
        }
        let bearings: [String: Any] = [
            "observed_at": observed,
            "coordinator_lifecycle": lifecycle,
            "work_state_counts": counts,
            "active_task_count": max(0, input.activeTaskCount),
            "pending_landing_count": max(0, input.pendingLandingCount),
            "open_wait_count": max(0, input.openWaitCount),
            "needs_triage": needsTriage,
            "waiting": waiting,
            "blocking": blocking,
            "sources": [
                "sessions": sessionSource,
                "tasks": source("orchestrator_task_registry", "current",
                                input.registryObservedAt),
                "landings": source("orchestrator_landing_registry", "current",
                                   input.registryObservedAt),
                "waits": source("orchestrator_coordination_wait_registry", "current",
                                input.registryObservedAt)
            ]
        ]
        return ["version": 1, "observed_at": observed,
                "store": ["status": loaded.status],
                "coordinator": coordinator, "bearings": bearings]
    }

    private static let commands: [[String: Any]] = {
        let previewReason = "Preview only in Phase A2: Bearings exists at authenticated "
            + "GET /v1/orchestrator/coordinator, but this web action is not connected."
        let previews = ["status_report", "duplicates_conflicts_ownership",
                        "landing_closure", "scope_permissions"].map {
            ["type": $0, "enabled": false, "why": previewReason] as [String: Any]
        }
        let reason = "Disabled in Phase A2: reconnect is machine-token-only; other coordinator "
            + "actions remain read-only and advisory."
        let closed = ["since_away", "coordinate_work", "dispatch_independent_work",
                      "ask_coordinator", "quiet_watch", "stop", "reconnect"].map {
            ["type": $0, "enabled": false, "why": reason] as [String: Any]
        }
        return previews + closed
    }()

    private static func safeSession(_ session: LiveSession) -> [String: Any] {
        var row: [String: Any] = [
            "id": session.identity.terminalID, "label": session.label,
            "work_state": session.workState.rawValue
        ]
        if let assistant = session.identity.assistant { row["assistant"] = assistant.rawValue }
        if let cwd = session.cwd { row["cwd"] = cwd }
        return row
    }

    private static func coordinatorMetadata(_ record: Record,
                                            among liveSessions: [LiveSession]) -> [String: Any] {
        let live = liveSessions.first { matches(record, $0.identity) }
        var session: [String: Any] = [
            "id": record.sessionID, "assistant": record.assistant,
            "label": live?.label ?? record.sessionLabel
        ]
        if let cwd = live?.cwd ?? record.cwd { session["cwd"] = cwd }
        if let workState = live?.workState { session["work_state"] = workState.rawValue }
        let online = live != nil
        var metadata: [String: Any] = [
            "configured": true, "id": record.id, "scope": record.scope,
            "label": record.label, "registered_at": Int(record.registeredAt),
            "generation": record.generation ?? 1,
            "status": online ? "online" : "offline",
            "lifecycle": online ? "standby" : "offline",
            "session": session
        ]
        if let reboundAt = record.reboundAt { metadata["rebound_at"] = Int(reboundAt) }
        return metadata
    }

    private static func valid(_ session: LiveSession) -> Bool {
        let identity = session.identity
        guard identity.assistant != nil, identity.pid.map({ $0 > 0 }) == true,
              let started = identity.processStart?.timeIntervalSince1970,
              started.isFinite, started > 0,
              bounded(identity.terminalID, maximum: 512) != nil,
              bounded(identity.tty, maximum: 1_024) != nil,
              identity.conversationID.flatMap({ bounded($0, maximum: 1_024) }) != nil
        else { return false }
        return true
    }

    private static func exact(_ lhs: Orchestrator.SessionWorkIdentity,
                              _ rhs: Orchestrator.SessionWorkIdentity) -> Bool {
        lhs.terminalID == rhs.terminalID && lhs.assistant == rhs.assistant
            && lhs.tty == rhs.tty && lhs.pid == rhs.pid
            && sameProcessStart(lhs.processStart?.timeIntervalSince1970,
                                rhs.processStart?.timeIntervalSince1970)
            && lhs.conversationID == rhs.conversationID
    }

    private static func matches(_ record: Record,
                                _ identity: Orchestrator.SessionWorkIdentity) -> Bool {
        record.sessionID == identity.terminalID
            && record.assistant == identity.assistant?.rawValue
            && record.tty == identity.tty && record.pid == identity.pid
            && sameProcessStart(record.processStart,
                                identity.processStart?.timeIntervalSince1970)
            && record.conversationID == identity.conversationID
    }

    private static func sameProcessStart(_ lhs: Double?, _ rhs: Double?) -> Bool {
        guard let lhs, let rhs, lhs.isFinite, rhs.isFinite else { return false }
        return abs(lhs - rhs) <= SessionRegistry.startTolerance
    }

    private static func bounded(_ raw: String, maximum: Int) -> String? {
        let value = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty, value.count <= maximum,
              !value.unicodeScalars.contains(where: { $0.value == 0 }) else { return nil }
        return value
    }

    private static func representableTimestamp(_ raw: Double) -> Bool {
        raw.isFinite && raw > 0 && raw < Double(Int.max)
    }

    private static func load(force: Bool = false) -> Loaded {
        let url = storeURL
        lock.lock()
        defer { lock.unlock() }
        let observed = fingerprint(of: url)
        if !force, loadedURL == url, loadedFingerprint == observed, let cached {
            return cached
        }
        let found: Loaded
        switch observed {
        case .absent:
            found = .absent
        case .inaccessible, .present(_, _, _, _, _, false):
            found = .corrupt
        case .present(_, _, _, _, _, true):
            guard let data = try? Data(contentsOf: url),
              let record = try? JSONDecoder().decode(Record.self, from: data) else {
                found = .corrupt
                loadedURL = url
                loadedFingerprint = observed
                cached = found
                return found
            }
            if record.version != recordVersion {
                found = .unsupported
            } else if valid(record) {
                found = .valid(record)
            } else {
                found = .corrupt
            }
        }
        loadedURL = url
        loadedFingerprint = observed
        cached = found
        return found
    }

    private static func valid(_ record: Record) -> Bool {
        let generation = record.generation ?? 1
        let reboundValid = record.reboundAt.map {
            representableTimestamp($0) && $0 >= record.registeredAt
        } ?? (generation == 1)
        return record.version == recordVersion && UUID(uuidString: record.id) != nil
            && record.scope == scope && record.label == label
            && representableTimestamp(record.registeredAt)
            && generation > 0 && reboundValid
            && Assistant(rawValue: record.assistant) != nil
            && record.pid > 0 && record.processStart.isFinite && record.processStart > 0
            && bounded(record.sessionID, maximum: 512) != nil
            && bounded(record.tty, maximum: 1_024) != nil
            && bounded(record.conversationID, maximum: 1_024) != nil
            && bounded(record.sessionLabel, maximum: 512) != nil
            && (record.cwd == nil || record.cwd.flatMap {
                bounded($0, maximum: 4_096)
            } != nil)
    }

    private static func save(_ record: Record) -> Bool {
        let url = storeURL
        guard let data = try? JSONEncoder.sorted.encode(record) else { return false }
        lock.lock(); defer { lock.unlock() }
        do {
            guard secureStoreParent(for: url) else { return false }
            if case .present(_, _, _, _, _, false) = fingerprint(of: url) { return false }
            try data.write(to: url, options: .atomic)
            guard chmod(url.path, 0o600) == 0 else { return false }
            let observed = fingerprint(of: url)
            guard case .present(_, _, _, _, _, true) = observed,
                  let storedData = try? Data(contentsOf: url),
                  let stored = try? JSONDecoder().decode(Record.self, from: storedData),
                  stored == record else { return false }
            loadedURL = url
            loadedFingerprint = observed
            cached = .valid(record)
            return true
        } catch {
            Log.write("coordinator: could not persist the durable identity — \(error)")
            return false
        }
    }

    private static func fingerprint(of url: URL) -> StoreFingerprint {
        var info = stat()
        if lstat(url.path, &info) != 0 {
            return errno == ENOENT ? .absent : .inaccessible(errno)
        }
        return .present(
            device: UInt64(info.st_dev), inode: UInt64(info.st_ino), size: info.st_size,
            seconds: Int64(info.st_mtimespec.tv_sec),
            nanoseconds: Int64(info.st_mtimespec.tv_nsec),
            regular: (info.st_mode & S_IFMT) == S_IFREG)
    }

    /// Create and validate the parent without accepting a symlink as the private store root.
    /// Mode repair is best effort for an existing directory; file and lock modes below are hard
    /// requirements because either one directly contains or protects the private binding tuple.
    private static func secureStoreParent(for url: URL) -> Bool {
        let parent = url.deletingLastPathComponent()
        do {
            try FileManager.default.createDirectory(at: parent, withIntermediateDirectories: true)
        } catch { return false }
        var info = stat()
        guard lstat(parent.path, &info) == 0,
              (info.st_mode & S_IFMT) == S_IFDIR else { return false }
        _ = chmod(parent.path, 0o700)
        return true
    }

    /// `flock` is the machine-local singleton boundary. Every registering process forces a fresh
    /// read only after it owns this file, then keeps ownership through creation and read-back.
    private static func withExclusiveRegistrationLock<T>(_ body: () -> T) -> T? {
        let url = registrationLockURL
        guard secureStoreParent(for: url) else { return nil }
        let fd = open(url.path, O_CREAT | O_RDWR | O_CLOEXEC | O_NOFOLLOW, 0o600)
        guard fd >= 0 else { return nil }
        defer { _ = close(fd) }
        var info = stat()
        guard fstat(fd, &info) == 0, (info.st_mode & S_IFMT) == S_IFREG,
              fchmod(fd, 0o600) == 0, flock(fd, LOCK_EX) == 0 else { return nil }
        defer { _ = flock(fd, LOCK_UN) }
        return body()
    }
}

private extension JSONEncoder {
    static var sorted: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        return encoder
    }
}
