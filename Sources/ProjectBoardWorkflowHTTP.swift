import Foundation
#if canImport(ClawdlineApplication)
import ClawdlineApplication // W3-1 correction: real cross-module import, see Sources/HostPorts.swift
#endif

/// The machine-authenticated command surface shared by Claude and Codex workflow helpers.
/// Identity is supplied only by RemoteServer after resolving the live process named by the path;
/// no identity field from the request body is accepted.
enum ProjectBoardWorkflowHTTP {
    static let historicalGapsPath = "/v1/orchestrator/workflow/gaps"
    struct Target: Equatable {
        let terminalID: String
    }

    static func target(_ path: String) -> Target? {
        let prefix = "/v1/orchestrator/sessions/"
        let suffix = "/workflow"
        guard path.hasPrefix(prefix), path.hasSuffix(suffix) else { return nil }
        let raw = String(path.dropFirst(prefix.count).dropLast(suffix.count))
        guard !raw.isEmpty, !raw.contains("/"),
              let decoded = raw.removingPercentEncoding, !decoded.isEmpty else { return nil }
        return Target(terminalID: decoded)
    }

    static func route(_ request: RemoteServer.Request, machine: Bool,
                      identity: ProjectBoardWorkflow.Identity?,
                      workflow: ProjectBoardWorkflow = .shared) -> RemoteServer.Response? {
        if request.path == historicalGapsPath {
            guard machine else {
                return .error(403, "forbidden",
                              "Reading durable workflow gaps needs the machine credential.")
            }
            guard request.method == "GET" else { return .status(405) }
            guard request.query.isEmpty, request.repeatedQueryKeys.isEmpty else {
                return .error(400, "bad_request", "The workflow gap read has no query fields.")
            }
            workflow.syncMode()
            return .json(["workflow_gaps": workflow.historicalGaps()])
        }
        guard target(request.path) != nil else { return nil }
        guard machine else {
            return .error(403, "forbidden",
                          "Recording managed workflow facts needs the machine credential.")
        }
        guard let identity else {
            return .error(409, "workflow_identity_unavailable",
                          "The path does not resolve to one observed assistant process.")
        }
        workflow.syncMode()
        if request.method == "GET" {
            guard request.query.isEmpty, request.repeatedQueryKeys.isEmpty else {
                return .error(400, "bad_request", "The workflow status read has no query fields.")
            }
            return .json(["workflow": workflow.snapshot(identity: identity)])
        }
        guard request.method == "POST" else { return .status(405) }
        guard request.body.count <= 64 * 1_024,
              let body = (try? JSONSerialization.jsonObject(with: request.body))
                as? [String: Any],
              let requestID = request.headers["idempotency-key"], !requestID.isEmpty else {
            return .error(400, "bad_request",
                          "A bounded JSON command and Idempotency-Key are required.")
        }
        let fingerprint = ProjectBoardWorkflow.fingerprint(request.body)
        let result = workflow.record(body, requestID: requestID,
                                     fingerprint: fingerprint, identity: identity)
        if result.status >= 400 {
            return .error(result.status, result.code,
                          "The managed workflow command was refused.",
                          extra: ["workflow": result.object])
        }
        return .json(["ok": true, "workflow": result.object], status: result.status)
    }
}

extension RemoteServer {
    /// Production's asynchronous `/send` path. Every decision stays on the HTTP queue; only the
    /// terminal handoff crosses to `terminalQueue`, and settlement crosses back before touching
    /// idempotency or delivering any response.
    func sendTerminal(_ request: Request, deliver: @escaping (Response) -> Void) {
        if let refusal = crossOriginRefusal(request) {
            deliver(refusal); return
        }
        if case .denied = permission(for: request) {
            deliver(.error(401, "unauthorized", "This needs a paired device.")); return
        }
        if let refusal = writeOriginRefusal(request) {
            deliver(refusal); return
        }

        let parsed = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
        let text = (parsed["text"] as? String) ?? ""
        let images = (parsed["images"] as? [String]) ?? []
        let fingerprint = ProjectBoardWorkflow.fingerprint(request.body)
        let id = String(request.path.dropFirst("/v1/sessions/".count).dropLast("/send".count))
        let terminalPublication = SessionWatch.shared.publishedInventory()
        let suppliedInventory = Self.sessionPayloadForTesting
        let terminalTargets = suppliedInventory?.0 ?? terminalPublication.targets
        let terminalStates = suppliedInventory?.1 ?? terminalPublication.states
        let resolvedSession = Self.session(
            withID: id.removingPercentEncoding ?? id, among: terminalTargets)
        let workflowIdentity = resolvedSession.flatMap {
            Self.workflowIdentity($0, publishedIdentity: terminalPublication.identities[$0.id])
        }
        ProjectBoardWorkflow.shared.syncMode()

        let device: String, key: String
        switch writeGate(request, keyNamespace: { device, rawKey in
            Self.terminalSendIdempotencyKey(
                principal: device, method: request.method,
                target: id.removingPercentEncoding ?? id, rawKey: rawKey)
        }) {
        case .refused(let response):
            deliver(response); return
        case .replay(let response):
            if let rawKey = request.headers["idempotency-key"], let workflowIdentity,
               case .allowed(let principal, _) = permission(for: request),
               case .conflict = ProjectBoardWorkflow.shared.retryVerdict(
                    requestID: Self.terminalSendIdempotencyKey(
                        principal: principal, method: request.method,
                        target: id.removingPercentEncoding ?? id, rawKey: rawKey),
                    fingerprint: fingerprint, identity: workflowIdentity) {
                deliver(.error(409, "workflow_request_conflict",
                               "That Idempotency-Key already names different managed content."))
                return
            }
            deliver(response); return
        case .go(let allowed, let filed):
            device = allowed
            key = filed
        }

        // A same-key retry is compared with the durable managed fingerprint before it can join
        // an in-flight terminal operation. Identical retries still share the original waiter;
        // changed content is a conflict and can never borrow the first request's success.
        if let workflowIdentity,
           case .conflict = ProjectBoardWorkflow.shared.retryVerdict(
                requestID: key, fingerprint: fingerprint, identity: workflowIdentity) {
            let response = Response.error(
                409, "workflow_request_conflict",
                "That Idempotency-Key already names different managed content.")
            deliver(response)
            return
        }
        // The reservation precedes every terminal observation, including the menu capture. A
        // retry arriving while that capture or send is blocked joins the first request and cannot
        // press Return a second time.
        if terminalPending[key] != nil {
            terminalPending[key, default: []].append(deliver)
            return
        }
        if let refusal = terminalMaintenanceRefusal() {
            deliver(refusal); return
        }

        guard !text.isEmpty || !images.isEmpty else {
            let response = Response.error(400, "bad_request", "That needs some text or an image.")
            remember(response, under: key, for: request, by: device)
            deliver(response)
            return
        }
        guard let session = resolvedSession else {
            let response = Response.error(404, "not_found", "No session named that")
            remember(response, under: key, for: request, by: device)
            deliver(response)
            return
        }

        // Refuse before reservation/admission and before image files are materialised. A request
        // that cannot run must not occupy a bounded slot or wait behind a blocked command. tmux
        // remains independent of iTerm's circuit.
        if let attention = ITerm.automationAttention, session.backend == .iterm {
            let response = Self.terminalFailure(attention, backend: .iterm)
            remember(response, under: key, for: request, by: device)
            deliver(response)
            return
        }

        var preparedRun: ProjectBoardWorkflow.Prepared?
        var workflowAnnotation: [String: Any]?
        var wireText = text
        if let workflowIdentity {
            switch ProjectBoardWorkflow.shared.prepareIngress(
                requestID: key, fingerprint: fingerprint, text: text,
                imageCount: images.count, identity: workflowIdentity) {
            case .managed(let prepared):
                preparedRun = prepared
                wireText = prepared.wireText
                workflowAnnotation = [
                    "status": "ingress_recorded", "run_id": prepared.runID,
                    "epoch": prepared.epoch, "replay": prepared.replay,
                    "authority": "broker_observed", "coverage": "managed_ingress",
                ]
            case .bypassed(let code):
                workflowAnnotation = ["status": "bypassed", "code": code]
            case .refused(let code):
                // The existing send remains available when Board-only persistence is not. The
                // response says plainly that no reliable workflow record was made.
                workflowAnnotation = ["status": "unrecorded", "code": code]
            }
        } else if ProjectBoardStore.shared.readHeader().enabled {
            workflowAnnotation = [
                "status": "unrecorded", "code": "workflow_identity_unavailable",
            ]
        }

        var pieces: [Drop.Piece]?
        var stored: [String] = []
        if !images.isEmpty {
            let made = Self.pieces(text: wireText, images: images)
            guard made.pieces.contains(where: {
                if case .image = $0 { return true }; return false
            }) else {
                if let preparedRun, let workflowIdentity {
                    workflowAnnotation = ProjectBoardWorkflow.shared.markDelivery(
                        runID: preparedRun.runID, identity: workflowIdentity,
                        delivered: false).object
                }
                let response = Self.annotatingWorkflow(
                    Response.error(400, "bad_request",
                                   "None of those were images I could read."),
                    workflowAnnotation)
                remember(response, under: key, for: request, by: device)
                deliver(response)
                return
            }
            pieces = made.pieces
            stored = made.stored
        }

        terminalPending[key] = [deliver]
        let shouldCheckMenu = terminalStates[session.id] == .waiting

        let admitted = enqueueTerminalCommand(channel: session.id) { [weak self] in
            guard let self else { return }
            var response: Response
            if let attention = ITerm.automationAttention, session.backend == .iterm {
                Self.finishUploads(stored, sent: false)
                response = Self.terminalFailure(attention, backend: .iterm)
            } else if shouldCheckMenu && Targets.isChoosing(session) {
                Self.finishUploads(stored, sent: false)
                response = .error(409, "showing_a_menu",
                                  "That session is showing a menu. Sending text would confirm "
                                  + "whichever option is highlighted rather than typing. "
                                  + "Answer it with POST /v1/sessions/<id>/key.")
            } else {
                // This timestamp shares the transcript row's Mac clock and is captured before the
                // terminal handoff. The row may be visible while a slow osascript round trip is
                // still running; a completion-only timestamp would put it outside reconciliation.
                let acceptedAt = Int(Date().timeIntervalSince1970)
                let problem: String?
                if let pieces {
                    problem = Targets.send(pieces, to: session)
                    Self.finishUploads(stored, sent: problem == nil)
                } else if let seam = Self.terminalSendForTesting {
                    problem = seam(wireText, session)
                } else {
                    problem = Targets.send(wireText, to: session)
                }
                RemoteAuth.audit("session.send", ["id": session.id, "tty": session.tty,
                                                   "chars": "\(text.count)",
                                                   "images": "\(images.count)",
                                                   "ok": problem == nil ? "1" : "0"])
                response = problem.map { Self.terminalFailure($0, backend: session.backend) }
                    ?? .json(["ok": true, "accepted_at": acceptedAt,
                              "at": Int(Date().timeIntervalSince1970)])
            }

            if let preparedRun, let workflowIdentity {
                workflowAnnotation = ProjectBoardWorkflow.shared.markDelivery(
                    runID: preparedRun.runID, identity: workflowIdentity,
                    delivered: response.status < 300).object
            }
            response = Self.annotatingWorkflow(response, workflowAnnotation)

            self.serialized {
                let waiters = self.terminalPending.removeValue(forKey: key) ?? []
                self.remember(response, under: key, for: request, by: device)
                for waiter in waiters { waiter(response) }
            }
        }
        if !admitted {
            Self.finishUploads(stored, sent: false)
            terminalPending.removeValue(forKey: key)
            if let preparedRun, let workflowIdentity {
                workflowAnnotation = ProjectBoardWorkflow.shared.markDelivery(
                    runID: preparedRun.runID, identity: workflowIdentity,
                    delivered: false).object
            }
            let response = terminalMaintenanceRefusal() ?? .error(
                429, "busy",
                "This Mac already has \(Self.terminalDepth) terminal commands in hand. "
                    + "Try again after they drain.")
            deliver(Self.annotatingWorkflow(response, workflowAnnotation))
        }
    }

    static func annotatingWorkflow(_ response: Response,
                                           _ workflow: [String: Any]?) -> Response {
        guard let workflow,
              var object = (try? JSONSerialization.jsonObject(with: response.body))
                as? [String: Any] else { return response }
        object["workflow"] = workflow
        var annotated = Response.json(object, status: response.status)
        for (key, value) in response.headers where key.lowercased() != "content-type" {
            annotated.headers[key] = value
        }
        return annotated
    }

    /// `/send` owns a narrower replay namespace than the generic write cache. A device may reuse
    /// one client-generated key for another Session or route without borrowing the first effect.
    static func terminalSendIdempotencyKey(principal: String, method: String,
                                           target: String, rawKey: String) -> String {
        let scope = ["send-v1", principal, method.uppercased(), target, rawKey]
            .joined(separator: "\u{0}")
        return "send:" + ProjectBoardWorkflow.fingerprint(Data(scope.utf8))
    }

    static func terminalFailure(_ problem: String, backend: Backend) -> Response {
        if backend == .iterm, let attention = ITerm.automationAttention,
           problem == attention {
            return .error(502, "iterm_attention_required", attention,
                          extra: ["app": "iTerm2", "action": "answer_dialog"])
        }
        return .error(502, "terminal_io_failed",
                      "The terminal command did not complete: \(problem)")
    }

    static func terminalFailure(_ failure: TerminalFailure) -> Response {
        if failure.kind == .iTermAttention {
            return .error(502, "iterm_attention_required", failure.message,
                          extra: ["app": "iTerm2", "action": "answer_dialog"])
        }
        return .error(502, "terminal_io_failed",
                      "The terminal command did not complete: \(failure.message)")
    }


}
