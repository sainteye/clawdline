import Foundation

/// Both the local browser and the encrypted Cloud adapter use this exact door.
enum ProjectBoardHTTP {
    static let maximumResponseBytes = 2 * 1024 * 1024

    static func response(_ envelope: [String: Any], status: Int = 200,
                         commandApplied: Bool = false) -> RemoteServer.Response {
        if let board = envelope["board"] as? [String: Any], board["available"] as? Bool == false {
            let error = board["error"] as? [String: Any] ?? [:]
            let message = error["message"] as? String ?? "Board view unavailable."
            return .error(503, error["code"] as? String ?? "board_unavailable",
                commandApplied ? "The command was saved, but its view is unavailable: \(message)" : message,
                extra: ["commandApplied": commandApplied])
        }
        let reply = RemoteServer.Response.json(envelope, status: status)
        guard !reply.body.isEmpty, reply.body.count <= maximumResponseBytes else {
            return .error(503, "board_response_too_large",
                (commandApplied ? "The command was saved. " : "") + "Select a narrower board item; the response exceeds its byte budget.",
                extra: ["commandApplied": commandApplied])
        }
        return reply
    }

    static func viewer(machine: Bool, permission: RemoteAuth.Verdict,
                       source: RemoteServer.Request.Source, remoteWrite: Bool) -> [String: Any] {
        var actor = "machine", canSend = machine, canAdmin = machine
        if case .allowed(let device, let caps) = permission {
            actor = machine ? "machine" : device; canSend = canSend || caps.contains(.send)
            canAdmin = canAdmin || caps.contains(.admin)
        }
        if case .verifiedCloud = source { canAdmin = canSend }
        if case .http = source, !machine, !remoteWrite { canSend = false; canAdmin = false }
        return ["id": actor, "canWrite": canSend, "canManage": canAdmin && canSend]
    }

    static func route(_ request: RemoteServer.Request, machine: Bool,
                      permission: RemoteAuth.Verdict) -> RemoteServer.Response? {
        guard request.path == "/v1/board" else { return nil }
        let viewer = viewer(machine: machine, permission: permission, source: request.source,
                            remoteWrite: Config.shared.remoteWrite)
        let actor = viewer["id"] as? String ?? "machine"
        let canSend = viewer["canWrite"] as? Bool == true
        let canAdmin = viewer["canManage"] as? Bool == true
        if request.method == "GET" {
            guard request.repeatedQueryKeys.isEmpty,
                  request.query.values.allSatisfy({ !$0.isEmpty && $0.utf8.count <= 200 }),
                  Set(request.query.keys).isSubset(of: ["project", "item"]) else {
                return .error(400, "bad_request", "Unknown board query field.")
            }
            var envelope = ProjectBoardIntegration.read(project: request.query["project"], item: request.query["item"])
            if var board = envelope["board"] as? [String: Any] {
                if board["available"] as? Bool == false {
                    return .json(["error": board["error"] ?? ["code": "board_unavailable", "message": "Board unavailable"]], status: 503)
                }
                board["viewer"] = viewer; envelope["board"] = board
            }
            return response(envelope)
        }
        guard request.method == "POST" else { return .status(405) }
        // A verified Cloud sender already holds the existing write capability. A
        // board preference changes no credential, remote permission or subscription.
        if case .http = request.source, !machine, !Config.shared.remoteWrite {
            return .error(403, "write_disabled", "Remote writes are disabled on this Mac.")
        }
        guard canSend else { return .error(403, "forbidden", "This device may only read the board.") }
        guard request.body.count <= 64 * 1024,
              let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
        else { return .error(400, "bad_request", "A bounded board command is required.") }
        if body["operation"] as? String == "set_enabled", !canAdmin {
            return .error(403, "forbidden", "Changing board mode requires an administrative device.")
        }
        // Only a local root may attest a verification/finding. This is an attributed
        // root report, not a claim that the broker executed a test. Landing still
        // enters exclusively through broker-verified ancestry, never this command.
        let acceptsArtifact = body["operation"] as? String == "accept_artifact"
        if acceptsArtifact && !canAdmin {
            return .error(403, "forbidden", "Accepting an artifact requires an administrative device.")
        }
        let recordsEvidence = body["operation"] as? String == "record_evidence"
        if recordsEvidence && (!machine || !["verification", "finding"].contains(body["kind"] as? String ?? "")) {
            return .error(403, "forbidden", "Only a local root may attest verification or findings; landing comes from the broker.")
        }
        let reply = ProjectBoardStore.shared.command(body, actor: recordsEvidence ? "root_attestation" : actor,
            trusted: (acceptsArtifact && canAdmin) || (recordsEvidence && machine))
        var envelope = reply.body
        if var board = envelope["board"] as? [String: Any] {
            board["viewer"] = viewer; envelope["board"] = board
        }
        return response(envelope, status: reply.status, commandApplied: reply.status == 200)
    }
}
