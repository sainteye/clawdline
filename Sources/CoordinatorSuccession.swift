import Foundation

/// Application service for the one Clawdfather-specific ownership move. HTTP parsing and the
/// terminal/SessionWatch adapters stay outside; this type owns the ordered durable transition
/// contract and can be tested with closed observations and side-effect ports.
struct CoordinatorSuccessionService {
    struct Observation {
        let sessions: [Coordinator.LiveSession]
        let sessionsObservedAt: Date?
        let sessionsGeneration: Int?
        let sessionsFresh: Bool
        let coordinator: [String: Any]?
    }

    struct CloseProof {
        let state: Orchestrator.SessionCloseability
        let version: String
        let lost: [[String: Any]]
        let wire: [String: Any]
    }

    enum CloseOutcome {
        case closed
        case absent
        case blocked(CloseProof)
        case failed(String)
    }

    static var closeProofForTesting:
        ((String) -> (state: Orchestrator.SessionCloseability, version: String,
                      lost: [[String: Any]])?)?

    let observe: () -> Observation
    let proveClose: (String) -> CloseProof?
    let closeSender: (String, String) -> CloseOutcome

    func create(_ obj: [String: Any]) -> Orchestrator.Reply {
        let draft: CoordinatorSuccession.Draft
        switch CoordinatorSuccession.draft(from: obj) {
        case .bad(let why): return .refused(400, "bad_request", why)
        case .ok(let value): draft = value
        }
        let prior = CoordinatorSuccession.record(requestID: draft.requestID)
        if prior != nil {
            let accepted = CoordinatorSuccession.accept(draft)
            guard case .ok = accepted else { return accepted }
            guard let record = CoordinatorSuccession.record(requestID: draft.requestID) else {
                return .refused(500, "succession_store_failed",
                                "The accepted succession could not be read back.")
            }
            if record.state == "failed", let failure = record.lastError {
                return .refused(
                    status: 409, code: failure.code, message: failure.message,
                    extra: ["succession": CoordinatorSuccession.publicRecord(record)])
            }
            if record.receiverSessionID != nil {
                return .ok(["ok": true, "created": false,
                            "succession": CoordinatorSuccession.publicRecord(record)])
            }
            if record.receiverOpenRequestedAt != nil {
                let failed = CoordinatorSuccession.recordFailure(
                    draft.requestID, code: "succession_receiver_open_receipt_lost",
                    message: "A receiver open was requested but no terminal receipt was "
                        + "persisted; retrying could open a duplicate receiver.", terminal: true)
                guard case .ok = failed else { return failed }
                return .refused(
                    status: 409, code: "succession_receiver_open_receipt_lost",
                    message: "Receiver identity is ambiguous after a restart; start a new "
                        + "succession.",
                    extra: ["succession": CoordinatorSuccession.publicRecord(
                        requestID: draft.requestID) ?? [:]])
            }
        }

        let observation = observe()
        guard let coordinator = observation.coordinator,
              coordinator["configured"] as? Bool == true,
              coordinator["id"] as? String == draft.coordinatorID,
              coordinator["generation"] as? Int == draft.expectedGeneration,
              coordinator["status"] as? String == "online",
              let session = coordinator["session"] as? [String: Any],
              session["id"] as? String == draft.senderSessionID else {
            return .refused(
                status: 409, code: "succession_sender_not_current",
                message: "The request must name the current online Coordinator binding and "
                    + "generation.",
                extra: ["coordinator": observation.coordinator ?? [:]])
        }

        if prior == nil {
            let accepted = CoordinatorSuccession.accept(draft)
            guard case .ok = accepted else { return accepted }
        }
        guard var record = CoordinatorSuccession.record(requestID: draft.requestID) else {
            return .refused(500, "succession_store_failed",
                            "The accepted succession could not be read back.")
        }
        let requested = CoordinatorSuccession.markOpenRequested(draft.requestID)
        guard case .ok = requested else { return requested }
        record = CoordinatorSuccession.record(requestID: draft.requestID) ?? record
        let handoffReply: Orchestrator.Reply
        if let starter = CoordinatorSuccession.handoffStarterForTesting {
            handoffReply = Orchestrator.handoff(
                CoordinatorSuccession.handoffObject(record), start: starter)
        } else {
            handoffReply = Orchestrator.handoff(CoordinatorSuccession.handoffObject(record))
        }
        switch handoffReply {
        case .refused(let status, let code, let message, _):
            let failed = CoordinatorSuccession.recordFailure(
                draft.requestID, code: "succession_receiver_delivery_failed",
                message: "\(code): \(message)", terminal: true)
            guard case .ok = failed else { return failed }
            return .refused(
                status: status, code: "succession_receiver_delivery_failed",
                message: "The ordinary Handoff could not open its receiver.",
                extra: ["cause": code,
                        "succession": CoordinatorSuccession.publicRecord(
                            requestID: draft.requestID) ?? [:]])
        case .ok(let payload):
            guard let handoff = payload["handoff"] as? [String: Any],
                  let opened = handoff["opened"] as? [String: Any],
                  let receiverID = opened["terminalId"] as? String else {
                let failed = CoordinatorSuccession.recordFailure(
                    draft.requestID, code: "succession_receiver_open_receipt_lost",
                    message: "The ordinary Handoff replay had no receiver terminal receipt.",
                    terminal: true)
                guard case .ok = failed else { return failed }
                return .refused(
                    status: 409, code: "succession_receiver_open_receipt_lost",
                    message: "The receiver terminal cannot be identified without risking a "
                        + "duplicate.",
                    extra: ["succession": CoordinatorSuccession.publicRecord(
                        requestID: draft.requestID) ?? [:]])
            }
            let openedReply = CoordinatorSuccession.markReceiverOpened(
                draft.requestID, receiverSessionID: receiverID)
            guard case .ok = openedReply else { return openedReply }
            RemoteAuth.audit("coordinator.succession.receiver_opened", [
                "request": draft.requestID, "handoff": draft.handoffID,
                "sender": draft.senderSessionID, "receiver": receiverID,
            ])
            return .ok(["ok": true, "created": true,
                        "succession": CoordinatorSuccession.publicRecord(
                            requestID: draft.requestID) ?? [:],
                        "coordinator": coordinator])
        }
    }

    func acknowledge(_ requestID: String, sessionID: String,
                     receipt: String) -> Orchestrator.Reply {
        guard var record = CoordinatorSuccession.record(requestID: requestID) else {
            return .refused(404, "succession_not_found",
                            "No Coordinator succession has that request id.")
        }
        if receipt == "receiver_picked_up" {
            if let pending = synchronizeHandoff(record) { return pending }
            record = CoordinatorSuccession.record(requestID: requestID) ?? record
        }
        if receipt == "receiver_completed" {
            let coordinator = observe().coordinator
            let session = coordinator?["session"] as? [String: Any]
            guard coordinator?["id"] as? String == record.coordinatorID,
                  coordinator?["generation"] as? Int == record.expectedGeneration + 1,
                  coordinator?["status"] as? String == "online",
                  session?["id"] as? String == record.receiverSessionID else {
                return .refused(409, "succession_receiver_not_online",
                                "The replacement binding is not currently proved online.")
            }
        }
        return CoordinatorSuccession.acknowledge(
            requestID, sessionID: sessionID, receipt: receipt)
    }

    func advance(_ requestID: String) -> Orchestrator.Reply {
        guard var record = CoordinatorSuccession.record(requestID: requestID) else {
            return .refused(404, "succession_not_found",
                            "No Coordinator succession has that request id.")
        }
        if record.state == "failed", let failure = record.lastError {
            return .refused(
                status: 409, code: failure.code, message: failure.message,
                extra: ["succession": CoordinatorSuccession.publicRecord(record)])
        }
        if let pending = synchronizeHandoff(record) { return pending }
        record = CoordinatorSuccession.record(requestID: requestID) ?? record
        guard record.packageDeliveredAt != nil,
              record.senderObservedAt != nil, record.pickupObservedAt != nil else {
            return .refused(
                status: 409, code: "succession_waiting_for_observation",
                message: "Package delivery, sender observation and receiver pickup must all be "
                    + "recorded.",
                extra: ["succession": CoordinatorSuccession.publicRecord(record)])
        }

        let observation = observe()
        guard let coordinator = observation.coordinator,
              let coordinatorID = coordinator["id"] as? String,
              let generation = coordinator["generation"] as? Int,
              let status = coordinator["status"] as? String else {
            return .refused(409, "succession_liveness_unknown",
                            "Coordinator state is not readable from current evidence.")
        }
        let boundSession = (coordinator["session"] as? [String: Any])?["id"] as? String
        guard coordinatorID == record.coordinatorID else {
            return terminalFailure(
                record, code: "succession_coordinator_identity_stale",
                message: "The stable Coordinator identity changed during succession.")
        }

        if generation == record.expectedGeneration + 1,
           status == "online", boundSession == record.receiverSessionID {
            guard record.oldBindingOfflineAt != nil else {
                return terminalFailure(
                    record, code: "succession_receipt_gap",
                    message: "The Coordinator moved, but this ledger has no durable offline "
                        + "proof.")
            }
            if record.rebindCommittedAt == nil {
                let marked = CoordinatorSuccession.markRebindCommitted(requestID)
                guard case .ok = marked else { return marked }
            }
            if record.receiverOnlineAt == nil {
                let marked = CoordinatorSuccession.markReceiverOnline(requestID)
                guard case .ok = marked else { return marked }
            }
            return .ok(["ok": true,
                        "succession": CoordinatorSuccession.publicRecord(
                            requestID: requestID) ?? [:],
                        "coordinator": coordinator])
        }
        guard generation == record.expectedGeneration else {
            return terminalFailure(
                record, code: "succession_generation_stale",
                message: "The Coordinator generation advanced outside this succession.")
        }
        if status == "unknown" {
            return .refused(
                status: 409, code: "succession_liveness_unknown",
                message: "SessionWatch cannot currently prove the old binding online or offline.",
                extra: ["succession": CoordinatorSuccession.publicRecord(record),
                        "coordinator": coordinator])
        }
        if status == "online" {
            guard boundSession == record.senderSessionID else {
                return terminalFailure(
                    record, code: "succession_unexpected_online_binding",
                    message: "A different session holds the unchanged Coordinator generation.")
            }
            guard let proof = proveClose(record.senderSessionID),
                  proof.state == .safe, proof.lost.isEmpty else {
                let proof = proveClose(record.senderSessionID)
                return .refused(
                    status: 409, code: "succession_sender_not_drained",
                    message: "The sender is not safely closeable; clear or attest every blocker "
                        + "first.",
                    extra: ["closeability": proof?.wire ?? [:], "lost": proof?.lost ?? [],
                            "succession": CoordinatorSuccession.publicRecord(record)])
            }
            let drained = CoordinatorSuccession.markDrainProven(
                requestID, closeabilityVersion: proof.version)
            guard case .ok = drained else { return drained }
            let requested = CoordinatorSuccession.markCloseRequested(requestID)
            guard case .ok = requested else { return requested }
            switch closeSender(record.senderSessionID, proof.version) {
            case .closed, .absent:
                return .ok(["ok": true,
                            "succession": CoordinatorSuccession.publicRecord(
                                requestID: requestID) ?? [:],
                            "coordinator": coordinator])
            case .blocked(let current):
                let failed = CoordinatorSuccession.recordFailure(
                    requestID, code: "succession_sender_close_stale",
                    message: "Sender closeability changed after drain proof.", terminal: false)
                guard case .ok = failed else { return failed }
                return .refused(
                    status: 409, code: "succession_sender_close_stale",
                    message: "Sender closeability changed after drain proof; retry from a fresh "
                        + "projection.",
                    extra: ["closeability": current.wire, "lost": current.lost,
                            "succession": CoordinatorSuccession.publicRecord(
                                requestID: requestID) ?? [:]])
            case .failed(let why):
                let failed = CoordinatorSuccession.recordFailure(
                    requestID, code: "succession_sender_close_refused",
                    message: why, terminal: false)
                guard case .ok = failed else { return failed }
                return .refused(
                    status: 502, code: "succession_sender_close_refused",
                    message: "The sender terminal refused its proven close.",
                    extra: ["succession": CoordinatorSuccession.publicRecord(
                        requestID: requestID) ?? [:]])
            }
        }

        guard status == "offline", observation.sessionsFresh,
              let observedAt = observation.sessionsObservedAt else {
            return .refused(409, "succession_liveness_unknown",
                            "The old binding is not proved offline by current timestamped "
                                + "evidence.")
        }
        let offline = CoordinatorSuccession.markOldBindingOffline(
            requestID, observedAt: observedAt,
            sessionsGeneration: observation.sessionsGeneration)
        guard case .ok = offline else { return offline }
        record = CoordinatorSuccession.record(requestID: requestID) ?? record
        let candidates = observation.sessions.filter {
            $0.identity.terminalID == record.receiverSessionID
        }
        guard candidates.count == 1, let candidate = candidates.first else {
            return .refused(
                status: 409,
                code: candidates.isEmpty ? "succession_receiver_missing" :
                    "succession_receiver_ambiguous",
                message: "The receiver must resolve to one exact live process before rebind.",
                extra: ["succession": CoordinatorSuccession.publicRecord(record)])
        }
        let rebound = Coordinator.rebind(
            expectedCoordinatorID: record.coordinatorID,
            expectedGeneration: record.expectedGeneration, to: candidate,
            among: observation.sessions, sessionsFresh: observation.sessionsFresh,
            sessionsObservedAt: observation.sessionsObservedAt)
        guard case .ok(let reboundPayload) = rebound else { return rebound }
        let committed = CoordinatorSuccession.markRebindCommitted(requestID)
        guard case .ok = committed else { return committed }
        guard let reboundCoordinator = reboundPayload["coordinator"] as? [String: Any],
              reboundCoordinator["status"] as? String == "online",
              (reboundCoordinator["session"] as? [String: Any])?["id"] as? String
                == record.receiverSessionID else {
            return .refused(409, "succession_receiver_not_online",
                            "Rebind committed, but the replacement is not projected online.")
        }
        let online = CoordinatorSuccession.markReceiverOnline(requestID)
        guard case .ok = online else { return online }
        RemoteAuth.audit("coordinator.succession.rebound", [
            "request": requestID, "coordinator": record.coordinatorID,
            "receiver": record.receiverSessionID ?? "missing",
            "generation": String(record.expectedGeneration + 1),
        ])
        return .ok(["ok": true,
                    "succession": CoordinatorSuccession.publicRecord(
                        requestID: requestID) ?? [:],
                    "coordinator": reboundCoordinator])
    }

    private func synchronizeHandoff(_ record: CoordinatorSuccession.Record)
        -> Orchestrator.Reply? {
        guard record.packageDeliveredAt == nil else { return nil }
        guard let handoff = Orchestrator.handoffRecord(id: record.handoffID),
              let state = handoff["state"] as? String else {
            return .refused(409, "succession_handoff_missing",
                            "The ordinary Handoff envelope is unavailable.")
        }
        switch state {
        case "delivered":
            let marked = CoordinatorSuccession.markPackageDelivered(record.requestID)
            guard case .ok = marked else { return marked }
            return nil
        case "spawn_failed":
            let failed = CoordinatorSuccession.recordFailure(
                record.requestID, code: "succession_receiver_delivery_failed",
                message: "The ordinary Handoff did not deliver its canonical first line.",
                terminal: true)
            guard case .ok = failed else { return failed }
            return .refused(
                status: 502, code: "succession_receiver_delivery_failed",
                message: "The receiver opened but package delivery was not confirmed.",
                extra: ["succession": CoordinatorSuccession.publicRecord(
                    requestID: record.requestID) ?? [:]])
        default:
            return .refused(
                status: 409, code: "succession_package_pending",
                message: "The ordinary Handoff has not yet confirmed its canonical first line.",
                extra: ["succession": CoordinatorSuccession.publicRecord(record)])
        }
    }

    private func terminalFailure(_ record: CoordinatorSuccession.Record,
                                 code: String, message: String) -> Orchestrator.Reply {
        let failed = CoordinatorSuccession.recordFailure(
            record.requestID, code: code, message: message, terminal: true)
        guard case .ok = failed else { return failed }
        return .refused(
            status: 409, code: code, message: message,
            extra: ["succession": CoordinatorSuccession.publicRecord(
                requestID: record.requestID) ?? [:]])
    }

}

/// The transport adapter is one cohesive route family. It parses closed HTTP shapes and delegates
/// every state transition to ``CoordinatorSuccessionService``.
enum CoordinatorSuccessionHTTP {
    private static let prefix = "/v1/orchestrator/coordinator/successions"

    static func route(_ request: RemoteServer.Request, orchestratorAuthed: Bool,
                      server: RemoteServer) -> RemoteServer.Response? {
        guard request.path == prefix || request.path.hasPrefix(prefix + "/") else { return nil }
        guard orchestratorAuthed else {
            return .error(403, "forbidden",
                          "Coordinator succession needs the orchestrator token.")
        }
        let service = server.coordinatorSuccessionService()
        if request.method == "POST", request.path == prefix {
            guard let body = try? JSONSerialization.jsonObject(with: request.body),
                  let obj = body as? [String: Any] else {
                return .error(400, "bad_request", "A JSON succession request is required.")
            }
            return server.answer(service.create(obj))
        }
        let suffix = String(request.path.dropFirst((prefix + "/").count))
        if request.method == "GET", !suffix.contains("/"),
           let row = CoordinatorSuccession.publicRecord(
               requestID: suffix.removingPercentEncoding ?? suffix) {
            return .json(["succession": row])
        }
        if request.method == "GET", !suffix.contains("/") {
            return .error(404, "succession_not_found",
                          "No Coordinator succession has that request id.")
        }
        if request.method == "POST", suffix.hasSuffix("/ack") {
            let id = String(suffix.dropLast("/ack".count))
            guard !id.contains("/"),
                  let body = try? JSONSerialization.jsonObject(with: request.body),
                  let obj = body as? [String: Any],
                  Set(obj.keys) == ["session_id", "receipt"],
                  let sessionID = obj["session_id"] as? String,
                  let receipt = obj["receipt"] as? String else {
                return .error(400, "bad_request",
                              "The closed acknowledgement needs session_id and receipt.")
            }
            return server.answer(service.acknowledge(
                id.removingPercentEncoding ?? id, sessionID: sessionID, receipt: receipt))
        }
        if request.method == "POST", suffix.hasSuffix("/advance") {
            let id = String(suffix.dropLast("/advance".count))
            guard !id.contains("/"),
                  let body = try? JSONSerialization.jsonObject(with: request.body),
                  let obj = body as? [String: Any], obj.isEmpty else {
                return .error(400, "bad_request", "The advance body must be an empty object.")
            }
            return server.answer(service.advance(id.removingPercentEncoding ?? id))
        }
        return nil
    }
}

extension RemoteServer {
    func coordinatorInspection(_ observation: CoordinatorObservation) -> [String: Any] {
        let registry = observation.registry
        let leaseFacts = Orchestrator.leaseBearings()
        return Coordinator.inspection(
            liveSessions: observation.sessions,
            bearings: .init(
                sessionsFresh: observation.sessionsFresh,
                activeTaskCount: registry.activeTasks,
                pendingLandingCount: registry.pendingLandings,
                pendingLandingRows: registry.pendingLandingRows,
                openWaitCount: registry.openWaits,
                leaseState: leaseFacts.state, leaseHolder: leaseFacts.holder,
                leaseQueueDepth: leaseFacts.queueDepth,
                leaseHoldReason: leaseFacts.holdReason,
                sessionsObservedAt: observation.sessionsObservedAt,
                registryObservedAt: registry.observedAt,
                sessionsGeneration: observation.sessionsGeneration))
    }

    func coordinatorSuccessionService() -> CoordinatorSuccessionService {
        CoordinatorSuccessionService(
            observe: { [unowned self] in
                let observation = coordinatorObservation()
                let inspection = coordinatorInspection(observation)
                return .init(
                    sessions: observation.sessions,
                    sessionsObservedAt: observation.sessionsObservedAt,
                    sessionsGeneration: observation.sessionsGeneration,
                    sessionsFresh: observation.sessionsFresh,
                    coordinator: inspection["coordinator"] as? [String: Any])
            },
            proveClose: { [unowned self] sessionID in
                coordinatorSuccessionCloseProof(sessionID: sessionID)
            },
            closeSender: { [unowned self] sessionID, expectedVersion in
                closeCoordinatorSuccessionSender(sessionID, expectedVersion: expectedVersion)
            })
    }

    func closeCoordinatorSuccessionSender(_ sessionID: String, expectedVersion: String)
        -> CoordinatorSuccessionService.CloseOutcome {
        guard let session = session(withID: sessionID) else { return .absent }
        guard let proof = coordinatorSuccessionCloseProof(sessionID: sessionID),
              proof.state == .safe, proof.version == expectedVersion,
              proof.lost.isEmpty else {
            let blocked = coordinatorSuccessionCloseProof(sessionID: sessionID)
                ?? CoordinatorSuccessionService.CloseProof(
                    state: .unknown, version: "unavailable", lost: [],
                    wire: ["state": "unknown", "version": "unavailable"])
            return .blocked(blocked)
        }
        RemoteAuth.audit("coordinator.succession.sender_close", ["id": session.id])
        Orchestrator.cancelChildren(ofRoot: session)
        let failure: String?
        if let suppliedEnd = Self.sessionEndForTesting { failure = suppliedEnd(session) }
        else { failure = Targets.end(session) }
        if let failure { return .failed(failure) }
        DispatchQueue.main.async { SessionWatch.shared.nudge() }
        return .closed
    }

    private func coordinatorSuccessionCloseProof(sessionID: String)
        -> CoordinatorSuccessionService.CloseProof? {
        if let supplied = CoordinatorSuccessionService.closeProofForTesting?(sessionID) {
            return .init(state: supplied.state, version: supplied.version, lost: supplied.lost,
                         wire: ["state": supplied.state.rawValue,
                                "version": supplied.version])
        }
        guard let session = session(withID: sessionID) else { return nil }
        let projection = closeability(of: session)
        return .init(
            state: projection.state, version: projection.version,
            lost: Orchestrator.lostIfClosed(root: session), wire: projection.wire)
    }
}
