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
        /// The fourth projection, carried beside the work state rather than folded into it.
        /// `ready` says this session can take work; this says whether it can end, and Bearings
        /// keeps them apart because a coordinator deciding what to close needs both.
        let closeability: Orchestrator.SessionCloseability?

        init(identity: Orchestrator.SessionWorkIdentity, label: String, cwd: String?,
             workState: Orchestrator.SessionWorkState, waitingOnSession: Bool,
             hasWaiters: Bool,
             closeability: Orchestrator.SessionCloseability? = nil) {
            self.identity = identity
            self.label = label
            self.cwd = cwd
            self.workState = workState
            self.waitingOnSession = waitingOnSession
            self.hasWaiters = hasWaiters
            self.closeability = closeability
        }
    }

    struct BearingsInput {
        let sessionsFresh: Bool
        let activeTaskCount: Int
        let pendingLandingCount: Int
        let pendingLandingRows: [[String: Any]]
        let openWaitCount: Int
        /// The heavy-compile lease, as four separate facts. `leaseState` keeps `missing`,
        /// `zero`, `unknown`, `queued` and `held` apart on purpose: a single spinner would
        /// collapse exactly the states this exists to show, and "no record yet" is not
        /// "nobody is compiling".
        let leaseState: String
        let leaseHolder: String?
        let leaseQueueDepth: Int
        let leaseHoldReason: String?
        let sessionsObservedAt: Date?
        let registryObservedAt: Date?
        let sessionsGeneration: Int?

        init(sessionsFresh: Bool, activeTaskCount: Int, pendingLandingCount: Int,
             pendingLandingRows: [[String: Any]] = [],
             openWaitCount: Int, leaseState: String = "missing", leaseHolder: String? = nil,
             leaseQueueDepth: Int = 0, leaseHoldReason: String? = nil,
             sessionsObservedAt: Date? = nil,
             registryObservedAt: Date? = nil, sessionsGeneration: Int? = nil) {
            self.sessionsFresh = sessionsFresh
            self.activeTaskCount = activeTaskCount
            self.pendingLandingCount = pendingLandingCount
            self.pendingLandingRows = pendingLandingRows
            self.openWaitCount = openWaitCount
            self.leaseState = leaseState
            self.leaseHolder = leaseHolder
            self.leaseQueueDepth = leaseQueueDepth
            self.leaseHoldReason = leaseHoldReason
            self.sessionsObservedAt = sessionsObservedAt
            self.registryObservedAt = registryObservedAt
            self.sessionsGeneration = sessionsGeneration
        }
    }

    /// A previous process-bound coordinator binding. Rebind keeps this small ledger so completion
    /// envelopes addressed to the old conversation can follow the explicitly proved role move;
    /// ordinary task resolution remains strict and never accepts these aliases.
    private struct BindingAlias: Codable, Equatable {
        let sessionID: String
        let assistant: String
        let conversationID: String
        let boundAt: Double
        let unboundAt: Double
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
        /// Optional for compatibility with every record written before durable completion retry.
        let aliases: [BindingAlias]?
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

    /// The closed vocabulary of ``registrationState`` — the one word a browser is allowed to
    /// gate coordinator creation on.
    ///
    /// It exists because `coordinator.configured` cannot answer the question. An absent record,
    /// a corrupt one and one written by an unknown version all project the identical
    /// `configured:false, status:"unregistered"` tuple, so a page reading that tuple sees an
    /// invitation to register where the store is in fact something ``register`` will refuse
    /// with `coordinator_store_invalid` — after the instruction has already been typed into a
    /// session. The three meanings are therefore stated rather than inferred, and a client that
    /// meets a fourth must treat it as none of them.
    ///
    /// It carries no coordinator id, path, token, stored bytes or corruption text: the whole
    /// projection is one of these three words.
    static let registrationStates: Set<String> = ["available", "configured", "blocked"]

    /// `available` — nothing is stored, so registering writes over nothing.
    /// `configured` — a valid record exists, online or offline; both are owners.
    /// `blocked` — unreadable, unparseable or from an unknown version. Never overwrite it.
    private static func registrationState(_ loaded: Loaded) -> String {
        switch loaded {
        case .absent: return "available"
        case .valid: return "configured"
        case .corrupt, .unsupported: return "blocked"
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

    /// Compatibility entry point for direct, already-current observations.
    static func register(_ candidate: LiveSession, among liveSessions: [LiveSession],
                         now: Date = Date(), makeID: () -> UUID = UUID.init)
        -> Orchestrator.Reply {
        register(sessionID: candidate.identity.terminalID, candidate: candidate,
                 among: liveSessions, sessionsFresh: true, sessionsObservedAt: now,
                 now: now, makeID: makeID)
    }

    /// Explicit construction. Durable presence is decided under the registration lock before
    /// liveness is consulted: an existing singleton keeps its idempotent/coordinator-exists
    /// behaviour even while SessionWatch is degraded. Only an absent store needs construction
    /// evidence, and that evidence must be a complete, timestamped current observation before a
    /// missing candidate can be called `session_not_found` or any bytes can be persisted.
    static func register(sessionID: String, candidate: LiveSession?,
                         among liveSessions: [LiveSession], sessionsFresh: Bool,
                         sessionsObservedAt: Date?,
                         now: Date = Date(), makeID: () -> UUID = UUID.init)
        -> Orchestrator.Reply {
        let registeredAt = now.timeIntervalSince1970
        guard representableTimestamp(registeredAt) else {
            return .refused(500, "coordinator_store_failed",
                            "The coordinator lifecycle timestamp cannot be represented.")
        }

        registrationLock.lock(); defer { registrationLock.unlock() }
        guard let reply = withExclusiveRegistrationLock({ () -> Orchestrator.Reply in
            switch load(force: true) {
            case .valid(let existing):
                if let candidate, candidate.identity.terminalID == sessionID,
                   valid(candidate), liveSessions.contains(where: {
                       exact($0.identity, candidate.identity)
                   }), matches(existing, candidate.identity) {
                    return .ok(["ok": true, "created": false,
                                "coordinator": coordinatorMetadata(
                                    existing, among: liveSessions,
                                    sessionsFresh: sessionsFresh,
                                    sessionsObservedAt: sessionsObservedAt)])
                }
                return .refused(status: 409, code: "coordinator_exists",
                                message: "A different machine coordinator is already registered; "
                                    + "registration is never a takeover operation.",
                                extra: ["coordinator": coordinatorMetadata(
                                    existing, among: liveSessions,
                                    sessionsFresh: sessionsFresh,
                                    sessionsObservedAt: sessionsObservedAt)])
            case .corrupt, .unsupported:
                return .refused(409, "coordinator_store_invalid",
                                "The durable coordinator record is unreadable or from an unknown "
                                    + "version; it was not replaced.")
            case .absent:
                let observedAt = sessionsObservedAt?.timeIntervalSince1970
                guard sessionsFresh,
                      observedAt.map({
                          representableTimestamp($0) && $0 <= registeredAt
                      }) == true else {
                    return .refused(
                        409, "coordinator_liveness_unknown",
                        "The Session inventory is stale or untimestamped, so a coordinator "
                            + "cannot be constructed from it.")
                }
                guard let candidate, candidate.identity.terminalID == sessionID else {
                    return .refused(
                        404, "session_not_found",
                        "No live Claude or Codex session has that terminal-neutral id.")
                }
                guard valid(candidate), liveSessions.contains(where: {
                    exact($0.identity, candidate.identity)
                }) else {
                    return .refused(
                        409, "session_unbound",
                        "That session has no complete process-bound assistant identity.")
                }
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
                    generation: 1, reboundAt: nil, aliases: nil)
                guard save(record) else {
                    return .refused(500, "coordinator_store_failed",
                                    "The coordinator record could not be written.")
                }
                return .ok(["ok": true, "created": true,
                            "coordinator": coordinatorMetadata(
                                record, among: liveSessions,
                                sessionsFresh: sessionsFresh,
                                sessionsObservedAt: sessionsObservedAt)])
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
                            existing, among: liveSessions,
                            sessionsFresh: sessionsFresh,
                            sessionsObservedAt: sessionsObservedAt)])
                }
                let currentGeneration = existing.generation ?? 1
                guard expectedGeneration == currentGeneration else {
                    return .refused(
                        status: 409, code: "coordinator_generation_mismatch",
                        message: "The coordinator lifecycle advanced after the caller observed it; "
                            + "refresh Bearings before reconnecting.",
                        extra: ["coordinator": coordinatorMetadata(
                            existing, among: liveSessions,
                            sessionsFresh: sessionsFresh,
                            sessionsObservedAt: sessionsObservedAt)])
                }

                if matches(existing, candidate.identity) {
                    return .ok(["ok": true, "rebound": false,
                                "coordinator": coordinatorMetadata(
                                    existing, among: liveSessions,
                                    sessionsFresh: sessionsFresh,
                                    sessionsObservedAt: sessionsObservedAt)])
                }
                if liveSessions.contains(where: { matches(existing, $0.identity) }) {
                    guard sessionsFresh, sessionsObservedAt.map({
                        representableTimestamp($0.timeIntervalSince1970)
                    }) == true else {
                        return .refused(
                            status: 409, code: "coordinator_liveness_unknown",
                            message: "The Session inventory is stale, so coordinator liveness "
                                + "cannot be asserted.",
                            extra: ["coordinator": coordinatorMetadata(
                                existing, among: liveSessions,
                                sessionsFresh: sessionsFresh,
                                sessionsObservedAt: sessionsObservedAt)])
                    }
                    return .refused(
                        status: 409, code: "coordinator_online",
                        message: "The exact current coordinator is still online; reconnect cannot "
                            + "take over a live binding.",
                        extra: ["coordinator": coordinatorMetadata(
                            existing, among: liveSessions,
                            sessionsFresh: sessionsFresh,
                            sessionsObservedAt: sessionsObservedAt)])
                }
                guard sessionsFresh else {
                    return .refused(
                        status: 409, code: "coordinator_liveness_unknown",
                        message: "The Session inventory is stale, so the old coordinator cannot "
                            + "be proved offline.",
                        extra: ["coordinator": coordinatorMetadata(
                            existing, among: liveSessions,
                            sessionsFresh: sessionsFresh,
                            sessionsObservedAt: sessionsObservedAt)])
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
                            existing, among: liveSessions,
                            sessionsFresh: sessionsFresh,
                            sessionsObservedAt: sessionsObservedAt)])
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
                let previousBoundAt = existing.reboundAt ?? existing.registeredAt
                var aliases = existing.aliases ?? []
                aliases.append(BindingAlias(
                    sessionID: existing.sessionID, assistant: existing.assistant,
                    conversationID: existing.conversationID,
                    boundAt: previousBoundAt, unboundAt: reboundAt))
                if aliases.count > 32 { aliases = Array(aliases.suffix(32)) }
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
                    reboundAt: reboundAt, aliases: aliases)
                guard save(rebound) else {
                    return .refused(500, "coordinator_store_failed",
                                    "The coordinator reconnect could not be written.")
                }
                return .ok(["ok": true, "rebound": true,
                            "coordinator": coordinatorMetadata(
                                rebound, among: liveSessions,
                                sessionsFresh: sessionsFresh,
                                sessionsObservedAt: sessionsObservedAt)])
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

    /// New-dispatch ingress correction for the one physical identity the durable Coordinator
    /// currently binds. The actual assistant comes from the binding rather than the caller's
    /// label. It returns no evidence for absent, corrupt or unsupported records; callers must
    /// leave unknown identities alone.
    static func rootIdentityEvidence(claimed: String?)
        -> [Orchestrator.RootIdentityEvidence] {
        guard let claimed, case .valid(let record) = load(), record.sessionID == claimed,
              let actualAssistant = Assistant(rawValue: record.assistant) else { return [] }
        return [.init(source: "coordinator_binding", terminalID: record.sessionID,
                      canonicalSessionID: record.conversationID,
                      assistant: actualAssistant)]
    }

    struct DeliveryBinding: Equatable {
        let conversationID: String
        let assistant: Assistant
    }

    /// Resolve only an explicitly proved Coordinator role move. The historical assistant proves
    /// which old binding created the task; the returned tuple is the canonical current binding,
    /// whose assistant may differ after a cross-assistant rebind. Physical terminal ids are not
    /// accepted here, preserving the task resolver's conversation-only contract for legacy rows.
    static func deliveryBinding(for rootSessionID: String,
                                historicalAssistant: Assistant,
                                taskCreated: Date) -> DeliveryBinding? {
        guard case .valid(let record) = load(),
              let currentAssistant = Assistant(rawValue: record.assistant) else { return nil }
        if rootSessionID == record.conversationID {
            guard historicalAssistant == currentAssistant else { return nil }
            return DeliveryBinding(conversationID: rootSessionID, assistant: currentAssistant)
        }
        let created = taskCreated.timeIntervalSince1970
        guard let alias = (record.aliases ?? []).last(where: {
            $0.conversationID == rootSessionID
                && $0.assistant == historicalAssistant.rawValue
                && created >= $0.boundAt && created <= $0.unboundAt
        }) else { return nil }
        _ = alias
        return DeliveryBinding(conversationID: record.conversationID,
                               assistant: currentAssistant)
    }

    /// Durable presence plus a deterministic read-only projection over current broker facts.
    static func sessionSourceFreshness(sessionsFresh: Bool, observedAt: Date?) -> String {
        if !sessionsFresh { return "stale" }
        return observedAt == nil ? "missing" : "current"
    }

    static func inspection(liveSessions: [LiveSession], bearings input: BearingsInput,
                           now: Date = Date()) -> [String: Any] {
        let observed = Int(now.timeIntervalSince1970)
        let loaded = load()
        let coordinator: [String: Any]
        let lifecycle: String
        switch loaded {
        case .valid(let record):
            coordinator = coordinatorMetadata(
                record, among: liveSessions,
                sessionsFresh: input.sessionsFresh,
                sessionsObservedAt: input.sessionsObservedAt)
            lifecycle = coordinator["lifecycle"] as? String ?? "unknown"
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
        // A second, independent tally. A row can be `ready` and not closeable, or quiet and
        // closeable; one number cannot be read as the other, so neither is derived from it.
        // Sessions whose closeability was not projected are counted under `not_projected`
        // rather than folded into `unknown`, because absent and doubtful are different facts.
        var closeabilityCounts: [String: Any] = ["not_projected": 0]
        for state in Orchestrator.SessionCloseability.allCases {
            closeabilityCounts[state.rawValue] = 0
        }
        for session in liveSessions {
            let key = session.closeability?.rawValue ?? "not_projected"
            closeabilityCounts[key] = (closeabilityCounts[key] as? Int ?? 0) + 1
        }
        let sorted = liveSessions.sorted {
            $0.identity.terminalID < $1.identity.terminalID
        }
        let unknown = sorted.filter { $0.workState == .unknown }.map(safeSession)
        let waiting = sorted.filter {
            $0.workState == .waitingYou || $0.workState == .waitingSession
        }.map(safeSession)
        let blocking = sorted.filter(\.hasWaiters).map(safeSession)

        let source: (String, String, Date?) -> [String: Any] = { provenance, freshness, at in
            ["observed_at": at.map { Int($0.timeIntervalSince1970) } ?? NSNull(),
             "provenance": provenance, "freshness": freshness]
        }
        var sessionSource = source(
            "session_watch",
            sessionSourceFreshness(
                sessionsFresh: input.sessionsFresh, observedAt: input.sessionsObservedAt),
            input.sessionsObservedAt)
        if let generation = input.sessionsGeneration {
            sessionSource["generation"] = generation
        }
        let bearings: [String: Any] = [
            "observed_at": observed,
            "coordinator_lifecycle": lifecycle,
            "work_state_counts": counts,
            "closeability_counts": closeabilityCounts,
            "active_task_count": max(0, input.activeTaskCount),
            "pending_landing_count": max(0, input.pendingLandingCount),
            "pending_landings": input.pendingLandingRows,
            "open_wait_count": max(0, input.openWaitCount),
            // Four fields rather than one word, so a reader can tell "nobody has ever taken it"
            // from "nobody holds it now" from "the lock exists and cannot be read".
            "heavy_compile_lease": leaseFacts(input),
            "unknown": unknown,
            "waiting": waiting,
            "blocking": blocking,
            "sources": [
                "sessions": sessionSource,
                "tasks": source("orchestrator_task_registry", "current",
                                input.registryObservedAt),
                "landings": source("orchestrator_landing_registry", "current",
                                   input.registryObservedAt),
                "waits": source("orchestrator_coordination_wait_registry", "current",
                                input.registryObservedAt),
                // The lock directory is the truth and this projection does not re-read it: the
                // freshness word says so, so a reader never mistakes a redraw for an observation
                // of the filesystem.
                "leases": source("orchestrator_lease_registry",
                                 input.leaseState == "missing" ? "missing" : "current",
                                 input.registryObservedAt)
            ]
        ]
        return ["version": 1, "observed_at": observed,
                "store": ["status": loaded.status],
                "registration": ["state": registrationState(loaded)],
                "coordinator": coordinator, "bearings": bearings]
    }

    /// The advertised web commands. Four reads are connected: they are answered by the
    /// device-readable Bearings projection at `GET /v1/orchestrator/coordinator/bearings`,
    /// which the page can reach with its own token.
    ///
    /// The deep audit is the one connected send: the page uses the ordinary user-attributed
    /// Session send route after an explicit confirmation. It grants no machine credential and
    /// creates no mutation route. Everything else that would send, spawn or mutate stays
    /// disabled, and each carries a closed
    /// `reason` code the client renders in its own language — a code cannot drift out of the
    /// client's vocabulary the way a sentence can. The prose `why` stays on the wire for pages
    /// that predate the codes, and says the same honest thing in English; neither field cites
    /// a phase number, because a phase label means nothing to the person holding the phone.
    private static let commands: [[String: Any]] = {
        let unrouted = "No route carries a command from this panel into a session yet, "
            + "so nothing can be sent."
        return [
            ["type": "status_report", "enabled": true,
             "token_effort": "low", "token_effort_basis": "registry_read"],
            ["type": "duplicates_conflicts_ownership", "enabled": true,
             "token_effort": "low", "token_effort_basis": "registry_read"],
            ["type": "landing_closure", "enabled": true,
             "token_effort": "low", "token_effort_basis": "registry_read"],
            ["type": "scope_permissions", "enabled": true,
             "token_effort": "low", "token_effort_basis": "registry_read"],
            ["type": "since_away", "enabled": false, "reason": "no_return_ledger",
             "token_effort": "unknown", "token_effort_basis": "unbuilt",
             "why": "This Mac does not record a return point yet, so there is nothing "
                + "to read one against."],
            ["type": "coordinate_work", "enabled": false, "reason": "no_command_route",
             "token_effort": "unknown", "token_effort_basis": "unbuilt",
             "why": unrouted],
            ["type": "dispatch_independent_work", "enabled": false,
             "reason": "device_cannot_spawn",
             "token_effort": "high", "token_effort_basis": "spawns_session",
             "why": "A paired device can never start a session — that separation is "
                + "deliberate, and this command will not cross it."],
            ["type": "ask_coordinator", "enabled": false, "reason": "no_command_route",
             "token_effort": "medium", "token_effort_basis": "single_session_message",
             "why": unrouted],
            ["type": "deep_status_audit", "enabled": true,
             "token_effort": "high", "token_effort_basis": "session_fanout"],
            ["type": "quiet_watch", "enabled": false, "reason": "no_command_route",
             "token_effort": "unknown", "token_effort_basis": "unbuilt",
             "why": unrouted],
            ["type": "stop", "enabled": false, "reason": "no_command_route",
             "token_effort": "low", "token_effort_basis": "broker_only",
             "why": unrouted],
            ["type": "reconnect", "enabled": false, "reason": "machine_token_only",
             "token_effort": "low", "token_effort_basis": "broker_only",
             "why": "Reconnecting needs the Mac's own orchestrator token, which a paired "
                + "device deliberately does not hold."],
        ] as [[String: Any]]
    }()

    /// The device-readable half of ``inspection`` — what a paired phone may see.
    ///
    /// Built as an allowlist, never by deleting keys from the full answer, so a field added to
    /// the authenticated inspection can never leak here by omission. Everything below is either
    /// already visible to a paired device through `GET /v1/sessions` (terminal-neutral ids,
    /// labels, cwd, work states) or an aggregate count. Withheld deliberately: the durable
    /// coordinator UUID, top-level `generation`, `registered_at` and `rebound_at` (rebind
    /// bookkeeping a device cannot use), `store` health, and the top-level source dictionaries.
    /// Pending-row ownership is a narrower nested allowlist and deliberately retains only its
    /// fixed evidence source names plus `observed_at`, `generation`, `provenance` and `freshness`.
    /// Bearings already excludes tty, pid, process start and conversation ids by construction.
    ///
    /// `store.status` stays withheld, but the one thing a device legitimately needs from it —
    /// whether registering would write over something — crosses as ``registrationStates``, a
    /// single closed word. That is the whole of the difference: no path, no bytes, no reason
    /// text, nothing a caller could use to tell a corrupt store from an unsupported one.
    static func deviceBearings(liveSessions: [LiveSession], bearings input: BearingsInput,
                               now: Date = Date()) -> [String: Any] {
        let full = inspection(liveSessions: liveSessions, bearings: input, now: now)

        // Fail closed, in the projection itself: if the authoritative answer is missing or is
        // a word this build does not know, the device is told the least permissive of the
        // three rather than being handed a state it might read as permission.
        var registrationState = "blocked"
        if let record = full["registration"] as? [String: Any],
           let state = record["state"] as? String, registrationStates.contains(state) {
            registrationState = state
        }

        var coordinator: [String: Any] = [:]
        if let record = full["coordinator"] as? [String: Any] {
            for key in ["configured", "label", "scope", "status", "lifecycle"] {
                if let value = record[key] { coordinator[key] = value }
            }
            if let session = record["session"] as? [String: Any] {
                coordinator["session"] = allowedSession(session)
            }
        }

        var bearings: [String: Any] = [:]
        if let record = full["bearings"] as? [String: Any] {
            for key in ["observed_at", "coordinator_lifecycle", "work_state_counts",
                    "closeability_counts", "heavy_compile_lease",
                        "active_task_count", "pending_landing_count", "open_wait_count"] {
                if let value = record[key] { bearings[key] = value }
            }
            bearings["pending_landings"] = ((record["pending_landings"]
                as? [[String: Any]]) ?? []).map(allowedPendingLanding)
            for key in ["unknown", "waiting", "blocking"] {
                bearings[key] = ((record[key] as? [[String: Any]]) ?? []).map(allowedSession)
            }
            if let sources = record["sources"] as? [String: Any] {
                var reduced: [String: Any] = [:]
                for (name, source) in sources {
                    guard let source = source as? [String: Any] else { continue }
                    var row: [String: Any] = [:]
                    if let at = source["observed_at"] { row["observed_at"] = at }
                    if let freshness = source["freshness"] { row["freshness"] = freshness }
                    reduced[name] = row
                }
                bearings["sources"] = reduced
            }
        }

        return ["version": 1,
                "observed_at": full["observed_at"] ?? Int(now.timeIntervalSince1970),
                "registration": ["state": registrationState],
                "coordinator": coordinator, "bearings": bearings]
    }

    /// The lease facts, kept as separate keys rather than folded into one status word.
    static func leaseFacts(_ input: BearingsInput) -> [String: Any] {
        var out: [String: Any] = ["state": input.leaseState,
                                  "queue_depth": max(0, input.leaseQueueDepth)]
        out["holder"] = input.leaseHolder ?? NSNull()
        if let reason = input.leaseHoldReason { out["hold_reason"] = reason }
        return out
    }

    private static func allowedSession(_ row: [String: Any]) -> [String: Any] {
        var out: [String: Any] = [:]
        for key in ["id", "assistant", "label", "cwd", "work_state", "closeability_state"] {
            if let value = row[key] { out[key] = value }
        }
        return out
    }

    private static func allowedPendingLanding(_ row: [String: Any]) -> [String: Any] {
        var out: [String: Any] = [:]
        for key in ["id", "title", "root_key", "root_label", "paths", "since",
                    "age_seconds", "target", "note"] {
            if let value = row[key] { out[key] = value }
        }
        guard let ownership = row["ownership"] as? [String: Any] else { return out }
        var allowedOwnership: [String: Any] = [:]
        for key in ["version", "status", "subject", "reason", "task_id", "task_state",
                    "root_key", "root_assistant", "observed_work_state"] {
            if let value = ownership[key] { allowedOwnership[key] = value }
        }
        var allowedEvidence: [String: Any] = [:]
        if let evidence = ownership["evidence"] as? [String: Any] {
            for name in ["sessions", "tasks", "landings"] {
                guard let source = evidence[name] as? [String: Any] else { continue }
                var allowedSource: [String: Any] = [:]
                for key in ["observed_at", "generation", "provenance", "freshness"] {
                    if let value = source[key] { allowedSource[key] = value }
                }
                allowedEvidence[name] = allowedSource
            }
        }
        allowedOwnership["evidence"] = allowedEvidence
        out["ownership"] = allowedOwnership
        return out
    }

    private static func safeSession(_ session: LiveSession) -> [String: Any] {
        var row: [String: Any] = [
            "id": session.identity.terminalID, "label": session.label,
            "work_state": session.workState.rawValue
        ]
        if let assistant = session.identity.assistant { row["assistant"] = assistant.rawValue }
        if let cwd = session.cwd { row["cwd"] = cwd }
        if let closeability = session.closeability {
            row["closeability_state"] = closeability.rawValue
        }
        return row
    }

    private static func coordinatorMetadata(_ record: Record,
                                            among liveSessions: [LiveSession],
                                            sessionsFresh: Bool,
                                            sessionsObservedAt: Date?) -> [String: Any] {
        let live = liveSessions.first { matches(record, $0.identity) }
        var session: [String: Any] = [
            "id": record.sessionID, "assistant": record.assistant,
            "label": live?.label ?? record.sessionLabel
        ]
        if let cwd = live?.cwd ?? record.cwd { session["cwd"] = cwd }
        if let workState = live?.workState { session["work_state"] = workState.rawValue }
        if let closeability = live?.closeability {
            session["closeability_state"] = closeability.rawValue
        }
        let observationCurrent = sessionsFresh && sessionsObservedAt.map {
            representableTimestamp($0.timeIntervalSince1970)
        } == true
        let bindingChangedAt = record.reboundAt ?? record.registeredAt
        let status: String
        let lifecycle: String
        if observationCurrent, live != nil {
            status = "online"
            lifecycle = "standby"
        } else if observationCurrent,
                  sessionsObservedAt.map({ $0.timeIntervalSince1970 >= bindingChangedAt }) == true {
            status = "offline"
            lifecycle = "offline"
        } else {
            status = "unknown"
            lifecycle = "unknown"
        }
        var metadata: [String: Any] = [
            "configured": true, "id": record.id, "scope": record.scope,
            "label": record.label, "registered_at": Int(record.registeredAt),
            "generation": record.generation ?? 1,
            "status": status,
            "lifecycle": lifecycle,
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
        let aliasesValid = (record.aliases ?? []).count <= 32
            && (record.aliases ?? []).allSatisfy { alias in
                Assistant(rawValue: alias.assistant) != nil
                    && bounded(alias.sessionID, maximum: 512) != nil
                    && bounded(alias.conversationID, maximum: 1_024) != nil
                    && representableTimestamp(alias.boundAt)
                    && representableTimestamp(alias.unboundAt)
                    && alias.unboundAt >= alias.boundAt
            }
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
            && aliasesValid
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

/// A durable, machine-token-only ledger for moving the one Coordinator role through an ordinary
/// Handoff. Ordinary Handoff remains a transport envelope; this ledger records the succession
/// receipts around it so a crash or retry cannot compress "a tab opened" into "ownership moved".
enum CoordinatorSuccession {
    static let recordVersion = 1

    struct Draft: Equatable {
        let requestID: String
        let coordinatorID: String
        let expectedGeneration: Int
        let senderSessionID: String
        let handoffID: String
        let projectDir: String
        let assistant: Assistant
        let model: String?
        let title: String?
    }

    struct Failure: Codable, Equatable {
        let code: String
        let message: String
        let at: Double
    }

    struct Record: Codable, Equatable {
        let version: Int
        let requestID: String
        let coordinatorID: String
        let expectedGeneration: Int
        let senderSessionID: String
        let handoffID: String
        let projectDir: String
        let assistant: String
        let model: String?
        let title: String?
        let createdAt: Double
        var revision: Int
        var state: String
        var receiverSessionID: String?
        var acceptedAt: Double
        var receiverOpenRequestedAt: Double?
        var receiverOpenedAt: Double?
        var packageDeliveredAt: Double?
        var senderObservedAt: Double?
        var pickupObservedAt: Double?
        var senderDrainProvenAt: Double?
        var senderCloseRequestedAt: Double?
        var oldBindingOfflineAt: Double?
        var oldBindingOfflineGeneration: Int?
        var rebindCommittedAt: Double?
        var receiverOnlineAt: Double?
        var receiverObservedAt: Double?
        var closeabilityVersion: String?
        var lastError: Failure?
    }

    private struct Store: Codable {
        let version: Int
        var successions: [Record]
    }

    enum DraftOutcome {
        case ok(Draft)
        case bad(String)
    }

    static var storeURLOverrideForTesting: URL?
    static var storeSaveInterceptorForTesting: ((Data) -> Bool?)?
    static var handoffStarterForTesting: Orchestrator.HandoffStarter?
    static var storeURL: URL {
        storeURLOverrideForTesting
            ?? RemoteAuth.directory.appendingPathComponent("coordinator-successions.json")
    }
    private static var storeLockURL: URL { storeURL.appendingPathExtension("lock") }
    private static let lock = NSLock()

    static func forgetForTesting() {
        // Reads intentionally have no process cache. Keeping this seam makes restart tests say
        // what they are exercising and leaves room for a future validated cache.
    }

    static func draft(from obj: [String: Any]) -> DraftOutcome {
        let expectedTop: Set<String> = [
            "request_id", "expected_coordinator_id", "expected_generation",
            "sender_session_id", "handoff",
        ]
        guard Set(obj.keys) == expectedTop,
              let requestID = obj["request_id"] as? String,
              Orchestrator.isTaskID(requestID),
              let coordinatorID = obj["expected_coordinator_id"] as? String,
              UUID(uuidString: coordinatorID)?.uuidString.lowercased() == coordinatorID,
              let expectedGeneration = obj["expected_generation"] as? Int,
              expectedGeneration > 0,
              let senderSessionID = bounded(obj["sender_session_id"] as? String, maximum: 512),
              let handoff = obj["handoff"] as? [String: Any] else {
            return .bad("The closed request needs request_id, expected_coordinator_id, "
                + "expected_generation, sender_session_id and handoff.")
        }
        let allowedHandoff: Set<String> = [
            "handoff_id", "project_dir", "assistant", "model", "title",
        ]
        guard Set(handoff.keys).isSubset(of: allowedHandoff),
              ["handoff_id", "project_dir", "assistant"].allSatisfy({ handoff[$0] != nil }),
              let handoffID = handoff["handoff_id"] as? String,
              Orchestrator.isTaskID(handoffID),
              let projectDir = handoff["project_dir"] as? String,
              StartPoints.usable(projectDir), StartPoints.isDirectory(projectDir),
              let assistantName = handoff["assistant"] as? String,
              let assistant = Assistant(rawValue: assistantName) else {
            return .bad("handoff must contain one lowercase handoff_id, an absolute existing "
                + "project_dir and assistant claude or codex.")
        }
        let model: String?
        if let raw = handoff["model"] {
            guard let value = raw as? String, StartPoints.modelName(value) == value else {
                return .bad("handoff.model is not a valid model name.")
            }
            model = value
        } else { model = nil }
        let title: String?
        if let raw = handoff["title"] {
            guard let value = raw as? String, value.count <= 200 else {
                return .bad("handoff.title must be at most 200 characters.")
            }
            title = value
        } else { title = nil }
        return .ok(Draft(
            requestID: requestID, coordinatorID: coordinatorID,
            expectedGeneration: expectedGeneration, senderSessionID: senderSessionID,
            handoffID: handoffID, projectDir: projectDir, assistant: assistant,
            model: model, title: title))
    }

    static func accept(_ draft: Draft, now: Date = Date()) -> Orchestrator.Reply {
        mutate(requestID: draft.requestID) { rows in
            if let existing = rows[draft.requestID] {
                guard matches(existing, draft) else {
                    return .reply(.refused(
                        status: 409, code: "succession_request_conflict",
                        message: "That succession request id already names different content.",
                        extra: ["succession": publicRecord(existing)]))
                }
                return .reply(.ok(["ok": true, "created": false,
                                   "succession": publicRecord(existing)]))
            }
            let at = now.timeIntervalSince1970
            let record = Record(
                version: recordVersion, requestID: draft.requestID,
                coordinatorID: draft.coordinatorID,
                expectedGeneration: draft.expectedGeneration,
                senderSessionID: draft.senderSessionID, handoffID: draft.handoffID,
                projectDir: draft.projectDir, assistant: draft.assistant.rawValue,
                model: draft.model, title: draft.title, createdAt: at, revision: 1,
                state: "accepted", receiverSessionID: nil, acceptedAt: at,
                receiverOpenRequestedAt: nil, receiverOpenedAt: nil,
                packageDeliveredAt: nil, senderObservedAt: nil, pickupObservedAt: nil,
                senderDrainProvenAt: nil, senderCloseRequestedAt: nil,
                oldBindingOfflineAt: nil, oldBindingOfflineGeneration: nil,
                rebindCommittedAt: nil,
                receiverOnlineAt: nil, receiverObservedAt: nil,
                closeabilityVersion: nil, lastError: nil)
            rows[draft.requestID] = record
            return .save(.ok(["ok": true, "created": true,
                              "succession": publicRecord(record)]))
        }
    }

    static func markOpenRequested(_ requestID: String, now: Date = Date())
        -> Orchestrator.Reply {
        update(requestID, now: now) { record, at in
            if record.receiverOpenRequestedAt == nil {
                record.receiverOpenRequestedAt = at
                record.state = "receiver_open_requested"
            }
        }
    }

    static func markReceiverOpened(_ requestID: String, receiverSessionID: String,
                                   now: Date = Date()) -> Orchestrator.Reply {
        update(requestID, now: now) { record, at in
            if let existing = record.receiverSessionID, existing != receiverSessionID {
                record.state = "failed"
                record.lastError = Failure(
                    code: "succession_receiver_identity_changed",
                    message: "A different receiver terminal was recorded for this succession.",
                    at: at)
                return
            }
            record.receiverSessionID = receiverSessionID
            record.receiverOpenedAt = record.receiverOpenedAt ?? at
            record.state = "receiver_opened"
            record.lastError = nil
        }
    }

    static func markPackageDelivered(_ requestID: String, now: Date = Date())
        -> Orchestrator.Reply {
        update(requestID, now: now) { record, at in
            record.packageDeliveredAt = record.packageDeliveredAt ?? at
            record.state = "package_delivered"
            record.lastError = nil
        }
    }

    static func acknowledge(_ requestID: String, sessionID: String, receipt: String,
                            now: Date = Date()) -> Orchestrator.Reply {
        mutate(requestID: requestID) { rows in
            guard var record = rows[requestID] else {
                return .reply(.refused(404, "succession_not_found",
                                       "No Coordinator succession has that request id."))
            }
            let at = now.timeIntervalSince1970
            switch receipt {
            case "sender_accepted":
                guard sessionID == record.senderSessionID else {
                    return .reply(.refused(403, "succession_wrong_observer",
                                           "Only the recorded sender may observe acceptance."))
                }
                record.senderObservedAt = record.senderObservedAt ?? at
                record.state = "sender_observed"
            case "receiver_picked_up":
                guard sessionID == record.receiverSessionID else {
                    return .reply(.refused(403, "succession_wrong_observer",
                                           "Only the recorded receiver may acknowledge pickup."))
                }
                guard record.packageDeliveredAt != nil else {
                    return .reply(.refused(409, "succession_package_not_delivered",
                                           "The ordinary Handoff delivery is not yet proved."))
                }
                record.pickupObservedAt = record.pickupObservedAt ?? at
                record.state = "pickup_observed"
            case "receiver_completed":
                guard sessionID == record.receiverSessionID else {
                    return .reply(.refused(403, "succession_wrong_observer",
                                           "Only the recorded receiver may observe completion."))
                }
                guard record.receiverOnlineAt != nil else {
                    return .reply(.refused(409, "succession_receiver_not_online",
                                           "The replacement binding is not yet proved online."))
                }
                record.receiverObservedAt = record.receiverObservedAt ?? at
                record.state = "complete"
            default:
                return .reply(.refused(400, "bad_request",
                    "receipt must be sender_accepted, receiver_picked_up or receiver_completed."))
            }
            record.revision += 1
            rows[requestID] = record
            return .save(.ok(["ok": true, "succession": publicRecord(record)]))
        }
    }

    static func markDrainProven(_ requestID: String, closeabilityVersion: String,
                                now: Date = Date()) -> Orchestrator.Reply {
        update(requestID, now: now) { record, at in
            record.senderDrainProvenAt = record.senderDrainProvenAt ?? at
            record.closeabilityVersion = closeabilityVersion
            record.state = "sender_drain_proven"
            record.lastError = nil
        }
    }

    static func markCloseRequested(_ requestID: String, now: Date = Date())
        -> Orchestrator.Reply {
        update(requestID, now: now) { record, at in
            record.senderCloseRequestedAt = record.senderCloseRequestedAt ?? at
            record.state = "sender_close_requested"
        }
    }

    static func markOldBindingOffline(_ requestID: String, observedAt: Date,
                                      sessionsGeneration: Int?,
                                      now: Date = Date()) -> Orchestrator.Reply {
        update(requestID, now: now) { record, _ in
            record.oldBindingOfflineAt = record.oldBindingOfflineAt
                ?? observedAt.timeIntervalSince1970
            record.oldBindingOfflineGeneration = record.oldBindingOfflineGeneration
                ?? sessionsGeneration
            record.state = "old_binding_offline"
            record.lastError = nil
        }
    }

    static func markRebindCommitted(_ requestID: String, now: Date = Date())
        -> Orchestrator.Reply {
        update(requestID, now: now) { record, at in
            record.rebindCommittedAt = record.rebindCommittedAt ?? at
            record.state = "rebind_committed"
            record.lastError = nil
        }
    }

    static func markReceiverOnline(_ requestID: String, now: Date = Date())
        -> Orchestrator.Reply {
        update(requestID, now: now) { record, at in
            record.receiverOnlineAt = record.receiverOnlineAt ?? at
            record.state = "receiver_online"
            record.lastError = nil
        }
    }

    static func recordFailure(_ requestID: String, code: String, message: String,
                              terminal: Bool, now: Date = Date()) -> Orchestrator.Reply {
        update(requestID, now: now) { record, at in
            record.lastError = Failure(code: code, message: message, at: at)
            if terminal { record.state = "failed" }
        }
    }

    static func record(requestID: String) -> Record? {
        lock.lock(); defer { lock.unlock() }
        return loadRows().rows?[requestID]
    }

    static func publicRecord(requestID: String) -> [String: Any]? {
        record(requestID: requestID).map(publicRecord)
    }

    static func publicRecord(_ record: Record) -> [String: Any] {
        var out: [String: Any] = [
            "version": record.version, "request_id": record.requestID,
            "coordinator_id": record.coordinatorID,
            "expected_generation": record.expectedGeneration,
            "sender_session_id": record.senderSessionID,
            "handoff_id": record.handoffID, "project_dir": record.projectDir,
            "assistant": record.assistant, "created_at": Int(record.createdAt),
            "revision": record.revision, "state": record.state,
        ]
        if let value = record.model { out["model"] = value }
        if let value = record.title { out["title"] = value }
        if let value = record.receiverSessionID { out["receiver_session_id"] = value }
        let dates: [(String, Double?)] = [
            ("accepted_at", record.acceptedAt),
            ("receiver_open_requested_at", record.receiverOpenRequestedAt),
            ("receiver_opened_at", record.receiverOpenedAt),
            ("package_delivered_at", record.packageDeliveredAt),
            ("sender_observed_at", record.senderObservedAt),
            ("pickup_observed_at", record.pickupObservedAt),
            ("sender_drain_proven_at", record.senderDrainProvenAt),
            ("sender_close_requested_at", record.senderCloseRequestedAt),
            ("old_binding_offline_at", record.oldBindingOfflineAt),
            ("rebind_committed_at", record.rebindCommittedAt),
            ("receiver_online_at", record.receiverOnlineAt),
            ("receiver_observed_at", record.receiverObservedAt),
        ]
        for (key, value) in dates { if let value { out[key] = Int(value) } }
        if let value = record.oldBindingOfflineGeneration {
            out["old_binding_offline_generation"] = value
        }
        if let value = record.closeabilityVersion { out["closeability_version"] = value }
        if let failure = record.lastError {
            out["last_error"] = ["code": failure.code, "message": failure.message,
                                 "at": Int(failure.at)]
        }
        return out
    }

    static func handoffObject(_ record: Record) -> [String: Any] {
        var obj: [String: Any] = [
            "handoff_id": record.handoffID, "project_dir": record.projectDir,
            "assistant": record.assistant, "from_session": record.senderSessionID,
        ]
        if let value = record.model { obj["model"] = value }
        if let value = record.title { obj["title"] = value }
        return obj
    }

    private enum MutationResult {
        case reply(Orchestrator.Reply)
        case save(Orchestrator.Reply)
    }

    private static func update(_ requestID: String, now: Date,
                               _ body: (inout Record, Double) -> Void) -> Orchestrator.Reply {
        mutate(requestID: requestID) { rows in
            guard var record = rows[requestID] else {
                return .reply(.refused(404, "succession_not_found",
                                       "No Coordinator succession has that request id."))
            }
            body(&record, now.timeIntervalSince1970)
            record.revision += 1
            rows[requestID] = record
            return .save(.ok(["ok": true, "succession": publicRecord(record)]))
        }
    }

    private static func mutate(requestID: String,
                               _ body: (inout [String: Record]) -> MutationResult)
        -> Orchestrator.Reply {
        _ = requestID
        lock.lock(); defer { lock.unlock() }
        guard let reply = withExclusiveStoreLock({ () -> Orchestrator.Reply in
            let loaded = loadRows()
            guard let rows = loaded.rows else {
                return .refused(409, "succession_store_invalid",
                                "The durable Coordinator succession ledger is unreadable.")
            }
            var changed = rows
            switch body(&changed) {
            case .reply(let reply): return reply
            case .save(let reply):
                guard saveRows(changed) else {
                    return .refused(500, "succession_store_failed",
                                    "The Coordinator succession receipt could not be persisted.")
                }
                return reply
            }
        }) else {
            return .refused(500, "succession_store_failed",
                            "The Coordinator succession ledger lock could not be secured.")
        }
        return reply
    }

    private static func matches(_ record: Record, _ draft: Draft) -> Bool {
        record.requestID == draft.requestID && record.coordinatorID == draft.coordinatorID
            && record.expectedGeneration == draft.expectedGeneration
            && record.senderSessionID == draft.senderSessionID
            && record.handoffID == draft.handoffID && record.projectDir == draft.projectDir
            && record.assistant == draft.assistant.rawValue && record.model == draft.model
            && record.title == draft.title
    }

    private static func bounded(_ raw: String?, maximum: Int) -> String? {
        guard let raw else { return nil }
        let value = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty, value.count <= maximum,
              !value.unicodeScalars.contains(where: { $0.value == 0 }) else { return nil }
        return value
    }

    private static func loadRows() -> (rows: [String: Record]?, absent: Bool) {
        guard FileManager.default.fileExists(atPath: storeURL.path) else { return ([:], true) }
        guard let data = try? Data(contentsOf: storeURL),
              let store = try? JSONDecoder().decode(Store.self, from: data),
              store.version == recordVersion,
              store.successions.allSatisfy(valid) else { return (nil, false) }
        var rows: [String: Record] = [:]
        for record in store.successions {
            guard rows[record.requestID] == nil else { return (nil, false) }
            rows[record.requestID] = record
        }
        return (rows, false)
    }

    private static func valid(_ record: Record) -> Bool {
        record.version == recordVersion && Orchestrator.isTaskID(record.requestID)
            && UUID(uuidString: record.coordinatorID) != nil
            && record.expectedGeneration > 0 && !record.senderSessionID.isEmpty
            && Orchestrator.isTaskID(record.handoffID) && StartPoints.usable(record.projectDir)
            && Assistant(rawValue: record.assistant) != nil && record.createdAt.isFinite
            && record.createdAt > 0 && record.acceptedAt.isFinite && record.acceptedAt > 0
            && record.revision > 0 && !record.state.isEmpty
            && record.oldBindingOfflineGeneration.map { $0 >= 0 } != false
    }

    private static func saveRows(_ rows: [String: Record]) -> Bool {
        let store = Store(version: recordVersion,
                          successions: rows.values.sorted { $0.createdAt < $1.createdAt })
        guard let data = try? JSONEncoder.sorted.encode(store) else { return false }
        if let intercepted = storeSaveInterceptorForTesting?(data) { return intercepted }
        do {
            guard secureParent(for: storeURL) else { return false }
            try data.write(to: storeURL, options: .atomic)
            guard chmod(storeURL.path, 0o600) == 0,
                  let written = try? Data(contentsOf: storeURL), written == data else { return false }
            return true
        } catch { return false }
    }

    private static func secureParent(for url: URL) -> Bool {
        let parent = url.deletingLastPathComponent()
        do { try FileManager.default.createDirectory(at: parent, withIntermediateDirectories: true) }
        catch { return false }
        var info = stat()
        guard lstat(parent.path, &info) == 0,
              (info.st_mode & S_IFMT) == S_IFDIR else { return false }
        _ = chmod(parent.path, 0o700)
        return true
    }

    private static func withExclusiveStoreLock<T>(_ body: () -> T) -> T? {
        guard secureParent(for: storeLockURL) else { return nil }
        let fd = open(storeLockURL.path, O_CREAT | O_RDWR | O_CLOEXEC | O_NOFOLLOW, 0o600)
        guard fd >= 0 else { return nil }
        defer { _ = close(fd) }
        var info = stat()
        guard fstat(fd, &info) == 0, (info.st_mode & S_IFMT) == S_IFREG,
              fchmod(fd, 0o600) == 0, flock(fd, LOCK_EX) == 0 else { return nil }
        defer { _ = flock(fd, LOCK_UN) }
        return body()
    }
}

extension RemoteServer {
    struct TerminalDrainSnapshot: Equatable {
        let outstanding: Int
        let channels: [String: Int]
    }

    func terminalDrainSnapshot() -> TerminalDrainSnapshot {
        terminalAdmissionLock.lock(); defer { terminalAdmissionLock.unlock() }
        return TerminalDrainSnapshot(outstanding: terminalOutstanding,
                                     channels: terminalOutstandingByChannel)
    }

    /// Work already admitted may finish its nested cascade; its counters are the drain receipt.
    func setRestartMaintenance(active: Bool, requestID: String?) {
        terminalAdmissionLock.lock()
        if active { terminalMaintenanceRequestID = requestID }
        else if requestID == nil || terminalMaintenanceRequestID == requestID {
            terminalMaintenanceRequestID = nil
        }
        terminalAdmissionLock.unlock()
    }

    func terminalMaintenanceRefusal() -> Response? {
        terminalAdmissionLock.lock(); defer { terminalAdmissionLock.unlock() }
        guard let requestID = terminalMaintenanceRequestID else { return nil }
        return .error(503, "restart_maintenance",
                      "Terminal mutations are paused while restart maintenance drains; retry "
                        + "after the replacement process reconciles.",
                      extra: ["retryable": true, "request_id": requestID, "retry_after": 1])
    }

    func beginRestartMaintenance(requestID: String) -> Response {
        guard Orchestrator.validRestartRequestID(requestID) else {
            return .error(400, "bad_restart_request", "request_id must be one lowercase UUID.")
        }
        // Close first: an admitted command remains counted and no newcomer can race the snapshot.
        setRestartMaintenance(active: true, requestID: requestID)
        let drain = terminalDrainSnapshot()
        let reply = Orchestrator.beginRestartMaintenance(
            requestID: requestID, outstanding: drain.outstanding, channels: drain.channels)
        // Derive the in-memory gate from the receipt after every outcome. A retry of an already
        // complete/aborted id is an HTTP success, but must not close admission again.
        if let active = Orchestrator.currentRestartRecord(),
           active["admission_closed"] as? Bool == true,
           let activeID = active["request_id"] as? String {
            setRestartMaintenance(active: true, requestID: activeID)
        } else {
            setRestartMaintenance(active: false, requestID: requestID)
        }
        return answer(reply)
    }

    func pollRestartMaintenance() -> Response {
        guard Orchestrator.currentRestartRecord() != nil else {
            return .error(404, "restart_not_found", "No restart maintenance intent exists.")
        }
        let drain = terminalDrainSnapshot()
        return answer(Orchestrator.advanceRestartMaintenance(
            outstanding: drain.outstanding, channels: drain.channels))
    }

    func abortRestartMaintenance(requestID: String) -> Response {
        let reply = Orchestrator.abortRestartMaintenance(requestID: requestID)
        if case .ok = reply {
            setRestartMaintenance(active: false, requestID: requestID)
        }
        return answer(reply)
    }
}

// The restart lifecycle must preserve the durable Coordinator verbatim. Its maintenance edge is
// kept beside that lifecycle boundary, while the task registry remains the single persisted store.
extension Orchestrator {
    static func validRestartRequestID(_ requestID: String) -> Bool {
        UUID(uuidString: requestID) != nil && requestID == requestID.lowercased()
    }

    static func beginRestartMaintenance(requestID: String, outstanding: Int,
                                        channels: [String: Int], now: Date = Date()) -> Reply {
        guard validRestartRequestID(requestID) else {
            return .refused(400, "bad_restart_request", "request_id must be one lowercase UUID.")
        }
        load()
        lock.lock()
        let candidates = Array(tasks.values)
        let existing = restartReceipt
        lock.unlock()
        let blockers = restartBlockers(in: candidates)
        if let existing, existing.phase == .invalid {
            return .refused(status: 503, code: "restart_store_failed",
                            message: "The stored restart intent is invalid; abort it explicitly "
                                + "before beginning another maintenance window.",
                            extra: ["restart": restartRecord(existing)])
        }
        if let existing, existing.requestID == requestID,
           existing.phase == .complete || existing.phase == .aborted {
            return .ok(["restart": restartRecord(existing)])
        }
        if let existing, existing.requestID != requestID,
           existing.phase != .complete && existing.phase != .aborted {
            return .refused(status: 409, code: "restart_in_progress",
                            message: "A different restart maintenance intent is already active.",
                            extra: ["restart": restartRecord(existing)])
        }
        guard blockers.isEmpty else {
            return .refused(status: 409, code: "restart_blocked_by_task_secret",
                            message: "Queued or spawning work still depends on process-local "
                                + "briefing-secret state; wait for it to become briefed or terminal.",
                            extra: ["blockers": blockers.map(restartBlockerRecord)])
        }
        let next = restartTransition(
            current: (existing?.phase == .complete || existing?.phase == .aborted) ? nil : existing,
            requestID: requestID, instanceID: appInstanceID, outstanding: outstanding,
            channels: channels, blockers: blockers, now: now)
        lock.lock(); restartReceipt = next; lock.unlock()
        guard save() else {
            lock.lock(); restartReceipt = existing; lock.unlock()
            return .refused(503, "restart_store_failed",
                            "The durable restart intent could not be written; replacement is unsafe.")
        }
        return .ok(["restart": restartRecord(next)])
    }

    static func advanceRestartMaintenance(outstanding: Int, channels: [String: Int],
                                          now: Date = Date()) -> Reply {
        load()
        lock.lock()
        guard let current = restartReceipt else {
            lock.unlock()
            return .refused(404, "restart_not_found", "No restart maintenance intent exists.")
        }
        guard current.phase != .invalid else {
            lock.unlock()
            return .refused(503, "restart_store_failed",
                            "The stored restart intent is invalid; abort it explicitly.")
        }
        let next = restartTransition(
            current: current, requestID: current.requestID, instanceID: appInstanceID,
            outstanding: outstanding, channels: channels, blockers: [], now: now)
        let changed = next != current
        restartReceipt = next
        lock.unlock()
        if changed, !save() {
            lock.lock(); restartReceipt = current; lock.unlock()
            return .refused(503, "restart_store_failed",
                            "The drained restart receipt could not be persisted; replacement is unsafe.")
        }
        return .ok(["restart": restartRecord(next)])
    }

    static func abortRestartMaintenance(requestID: String, now: Date = Date()) -> Reply {
        guard validRestartRequestID(requestID) else {
            return .refused(400, "bad_restart_request", "request_id must be one lowercase UUID.")
        }
        load()
        lock.lock()
        guard var current = restartReceipt else {
            lock.unlock()
            return .refused(404, "restart_not_found", "No restart maintenance intent exists.")
        }
        guard current.requestID == requestID else {
            lock.unlock()
            return .refused(status: 409, code: "restart_in_progress",
                            message: "A different restart maintenance intent is active.",
                            extra: ["restart": restartRecord(current)])
        }
        if current.phase == .aborted || current.phase == .complete {
            lock.unlock()
            return .ok(["restart": restartRecord(current)])
        }
        let prior = current
        current.phase = .aborted
        current.abortedAt = now
        current.outstanding = 0
        current.channels = [:]
        restartReceipt = current
        lock.unlock()
        guard save() else {
            lock.lock()
            restartReceipt = prior
            lock.unlock()
            return .refused(503, "restart_store_failed",
                            "The aborted restart receipt could not be persisted; admission stays closed.")
        }
        return .ok(["restart": restartRecord(current)])
    }

    static func currentRestartRecord() -> [String: Any]? {
        load()
        lock.lock(); defer { lock.unlock() }
        return restartReceipt.map(restartRecord)
    }

    static func restartAdmissionClosed() -> Bool {
        load()
        lock.lock(); defer { lock.unlock() }
        return restartReceipt?.admissionClosed == true
    }

    static func resumeRestartIntent() {
        load()
        lock.lock()
        guard let current = restartReceipt, current.phase != .complete else {
            lock.unlock(); return
        }
        if current.phase == .invalid {
            lock.unlock()
            RemoteServer.shared.setRestartMaintenance(active: true,
                                                       requestID: current.requestID)
            return
        }
        let next = restartTransition(
            current: current, requestID: current.requestID, instanceID: appInstanceID,
            outstanding: 0, channels: [:], blockers: [], now: Date())
        restartReceipt = next
        lock.unlock()
        if next != current { _ = save() }
        RemoteServer.shared.setRestartMaintenance(active: true, requestID: next.requestID)
    }

    static func reconcileRestartInventory(_ snapshot: SessionWatch.IdentitySnapshot,
                                          identities: [SessionWorkIdentity], now: Date = Date()) {
        load()
        lock.lock()
        guard var restart = restartReceipt, restart.phase == .reconciling,
              snapshot.complete, let observedAt = snapshot.observedAt,
              observedAt >= (restart.resumedAt ?? restart.requestedAt) else {
            lock.unlock(); return
        }
        let inventory = ExecutorInventory(
            complete: snapshot.complete, observedAt: observedAt,
            generation: snapshot.generation, epoch: snapshot.epoch)
        let priorRestart = restart
        let priorTasks = tasks
        var unresolved: [String] = []
        for (id, var task) in tasks where task.state == .briefed {
            let receipt = reconcileExecutor(task: task, identities: identities,
                                            inventory: inventory,
                                            previous: task.executorReceipt, now: now)
            task.executorReceipt = receipt
            tasks[id] = task
            if receipt.status == .pending { unresolved.append(id) }
        }
        unresolved.sort()
        let reconciliationExpired = now.timeIntervalSince(
            restart.resumedAt ?? restart.requestedAt) >= restartReconciliationGrace
        let allSettled = unresolved.isEmpty || reconciliationExpired
        if allSettled {
            restart.phase = .complete
            restart.reconciledAt = now
            restart.reconciliationTimedOut = !unresolved.isEmpty
            restart.unresolvedTaskIDs = unresolved
            restart.outstanding = 0
            restart.channels = [:]
            restartReceipt = restart
        }
        lock.unlock()
        guard save() else {
            lock.lock()
            tasks = priorTasks
            restartReceipt = priorRestart
            lock.unlock()
            return
        }
        if allSettled {
            RemoteServer.shared.setRestartMaintenance(active: false, requestID: restart.requestID)
            RemoteServer.shared.broadcastOrchestrator()
        }
    }

    static func restartBlockerRecord(_ blocker: RestartBlocker) -> [String: Any] {
        ["task_id": blocker.taskID, "state": blocker.state.rawValue, "code": blocker.code]
    }

    static func restartRecord(_ receipt: RestartReceipt) -> [String: Any] {
        var out: [String: Any] = [
            "request_id": receipt.requestID, "phase": receipt.phase.rawValue,
            "requested_instance_id": receipt.requestedInstanceID,
            "requested_at": Int(receipt.requestedAt.timeIntervalSince1970),
            "outstanding": receipt.outstanding, "channels": receipt.channels,
            "safe_to_replace": receipt.safeToReplace,
            "admission_closed": receipt.admissionClosed,
            "replacement_before_safe": receipt.replacementBeforeSafe,
            "reconciliation_timed_out": receipt.reconciliationTimedOut,
            "unresolved_task_ids": receipt.unresolvedTaskIDs,
        ]
        if let value = receipt.resumedInstanceID { out["resumed_instance_id"] = value }
        if let value = receipt.drainedAt { out["drained_at"] = Int(value.timeIntervalSince1970) }
        if let value = receipt.resumedAt { out["resumed_at"] = Int(value.timeIntervalSince1970) }
        if let value = receipt.reconciledAt { out["reconciled_at"] = Int(value.timeIntervalSince1970) }
        if let value = receipt.abortedAt { out["aborted_at"] = Int(value.timeIntervalSince1970) }
        return out
    }

    static func stored(_ receipt: RestartReceipt) -> [String: Any] {
        var out = restartRecord(receipt)
        out["requested_at"] = receipt.requestedAt.timeIntervalSince1970
        if let value = receipt.drainedAt { out["drained_at"] = value.timeIntervalSince1970 }
        if let value = receipt.resumedAt { out["resumed_at"] = value.timeIntervalSince1970 }
        if let value = receipt.reconciledAt { out["reconciled_at"] = value.timeIntervalSince1970 }
        if let value = receipt.abortedAt { out["aborted_at"] = value.timeIntervalSince1970 }
        return out
    }

    static func restartReceipt(from obj: [String: Any]) -> RestartReceipt? {
        guard let requestID = obj["request_id"] as? String,
              UUID(uuidString: requestID) != nil, requestID == requestID.lowercased(),
              let phaseName = obj["phase"] as? String, let phase = RestartPhase(rawValue: phaseName),
              let instance = obj["requested_instance_id"] as? String, !instance.isEmpty,
              let requested = obj["requested_at"] as? Double,
              let outstanding = obj["outstanding"] as? Int, outstanding >= 0 else { return nil }
        let rawChannels = obj["channels"] as? [String: Any] ?? [:]
        var channels: [String: Int] = [:]
        for (channel, raw) in rawChannels {
            guard !channel.isEmpty, channel.count <= 512,
                  let count = raw as? Int, count >= 0 else { return nil }
            channels[channel] = count
        }
        func date(_ key: String) -> Date? {
            (obj[key] as? Double).map(Date.init(timeIntervalSince1970:))
        }
        var receipt = RestartReceipt(
            requestID: requestID, requestedInstanceID: instance,
            resumedInstanceID: obj["resumed_instance_id"] as? String,
            phase: phase, requestedAt: Date(timeIntervalSince1970: requested),
            drainedAt: date("drained_at"), resumedAt: date("resumed_at"),
            reconciledAt: date("reconciled_at"), abortedAt: date("aborted_at"),
            outstanding: outstanding, channels: channels)
        receipt.replacementBeforeSafe = obj["replacement_before_safe"] as? Bool ?? false
        receipt.reconciliationTimedOut = obj["reconciliation_timed_out"] as? Bool ?? false
        receipt.unresolvedTaskIDs = obj["unresolved_task_ids"] as? [String] ?? []
        guard (phase != .ready || receipt.safeToReplace),
              (phase != .complete || receipt.reconciledAt != nil),
              (phase != .aborted || receipt.abortedAt != nil),
              receipt.unresolvedTaskIDs.allSatisfy({ UUID(uuidString: $0) != nil }) else { return nil }
        return receipt
    }

    static func quarantinedRestartReceipt(from obj: [String: Any], now: Date = Date())
        -> RestartReceipt {
        let rawID = obj["request_id"] as? String ?? ""
        let requestID = validRestartRequestID(rawID)
            ? rawID : "00000000-0000-0000-0000-000000000000"
        let instance = (obj["requested_instance_id"] as? String).flatMap {
            $0.isEmpty ? nil : $0
        } ?? "unreadable"
        return RestartReceipt(
            requestID: requestID, requestedInstanceID: instance,
            resumedInstanceID: nil, phase: .invalid, requestedAt: now,
            drainedAt: nil, resumedAt: nil, reconciledAt: nil, abortedAt: nil,
            outstanding: 0, channels: [:])
    }

    static func identityRecord(_ identity: SessionWorkIdentity) -> [String: Any] {
        var out: [String: Any] = ["terminal_id": identity.terminalID, "tty": identity.tty]
        if let assistant = identity.assistant { out["assistant"] = assistant.rawValue }
        if let pid = identity.pid { out["pid"] = Int(pid) }
        if let start = identity.processStart { out["process_start"] = start.timeIntervalSince1970 }
        if let conversation = identity.conversationID { out["conversation_id"] = conversation }
        return out
    }

    static func identity(from obj: [String: Any]) -> SessionWorkIdentity? {
        guard let terminal = obj["terminal_id"] as? String, !terminal.isEmpty,
              let tty = obj["tty"] as? String, !tty.isEmpty else { return nil }
        return SessionWorkIdentity(
            terminalID: terminal,
            assistant: (obj["assistant"] as? String).flatMap(Assistant.init(rawValue:)), tty: tty,
            pid: (obj["pid"] as? Int).flatMap(Int32.init(exactly:)),
            processStart: (obj["process_start"] as? Double).map(Date.init(timeIntervalSince1970:)),
            conversationID: obj["conversation_id"] as? String)
    }

    static func executorRecord(_ receipt: ExecutorReceipt) -> [String: Any] {
        var out: [String: Any] = [
            "status": receipt.status.rawValue, "provenance": receipt.provenance,
            "inventory_complete": receipt.inventoryComplete,
            "inventory_generation": receipt.inventoryGeneration,
            "inventory_epoch": receipt.inventoryEpoch,
            "mismatch_observations": receipt.mismatchObservations, "mover": receipt.mover,
        ]
        if let kind = receipt.pendingKind { out["pending_kind"] = kind.rawValue }
        if let at = receipt.observedAt { out["observed_at"] = Int(at.timeIntervalSince1970) }
        if let at = receipt.firstMismatchAt { out["first_mismatch_at"] = Int(at.timeIntervalSince1970) }
        return out
    }

    static func stored(_ receipt: ExecutorReceipt) -> [String: Any] {
        var out = executorRecord(receipt)
        if let at = receipt.observedAt { out["observed_at"] = at.timeIntervalSince1970 }
        if let at = receipt.firstMismatchAt { out["first_mismatch_at"] = at.timeIntervalSince1970 }
        if let identity = receipt.observedIdentity { out["observed_identity"] = identityRecord(identity) }
        return out
    }

    static func executorReceipt(from obj: [String: Any]) -> ExecutorReceipt? {
        guard let rawStatus = obj["status"] as? String,
              let status = ExecutorReconciliationStatus(rawValue: rawStatus),
              let provenance = obj["provenance"] as? String, !provenance.isEmpty,
              let complete = obj["inventory_complete"] as? Bool,
              let generation = obj["inventory_generation"] as? Int,
              let epoch = obj["inventory_epoch"] as? String, !epoch.isEmpty,
              let observations = obj["mismatch_observations"] as? Int, observations >= 0,
              let mover = obj["mover"] as? String, ["broker", "person"].contains(mover)
        else { return nil }
        let kind = (obj["pending_kind"] as? String).flatMap(ExecutorIssueKind.init(rawValue:))
        let observedIdentity = (obj["observed_identity"] as? [String: Any]).flatMap(identity(from:))
        return ExecutorReceipt(
            status: status, pendingKind: kind, provenance: provenance,
            observedAt: (obj["observed_at"] as? Double).map(Date.init(timeIntervalSince1970:)),
            inventoryComplete: complete, inventoryGeneration: generation, inventoryEpoch: epoch,
            firstMismatchAt: (obj["first_mismatch_at"] as? Double).map(Date.init(timeIntervalSince1970:)),
            mismatchObservations: observations, observedIdentity: observedIdentity, mover: mover)
    }
}

private extension JSONEncoder {
    static var sorted: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        return encoder
    }
}
