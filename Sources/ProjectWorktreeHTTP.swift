import CoreFoundation
import Foundation

/// The authenticated codec for `ProjectWorktreeLifecycleService`, and nothing else.
///
/// Local HTTP and verified-Cloud requests enter the same four routes here. This adapter parses a
/// closed route, authorizes it, validates a closed body and encodes the service's typed answer; it
/// owns no Git, filesystem or classification logic. Work that runs git leaves the shared server
/// queue on a bounded lane of its own, so a slow repository cannot hold `/v1/health` or the event
/// stream.
///
/// | route | who may call it |
/// |---|---|
/// | `GET /v1/projects/:id/worktrees` | orchestrator token, or a device with `read` (Cloud included) |
/// | `POST /v1/projects/:id/worktrees/refresh` | the same: a bounded local observation, never a fetch |
/// | `POST /v1/projects/:id/worktrees/cleanup/preview` | the orchestrator token only |
/// | `POST /v1/projects/:id/worktrees/cleanup/apply` | the orchestrator token only |
enum ProjectWorktreeHTTP {
    static let maximumResponseBytes = 2 * 1024 * 1024
    static let maximumBodyBytes = 16 * 1024
    /// Queued plus running lifecycle work. The service observes one Project at a time; this bounds
    /// how many callers may wait for it.
    static let depth = 4
    private static let lane = DispatchQueue(label: "com.tsunamiworks.clawdline.project-worktrees", qos: .utility)
    private static let admissionLock = NSLock()
    private static var outstanding = 0
    private static var serviceForTesting: ProjectWorktreeLifecycleService?

    static var service: ProjectWorktreeLifecycleService { serviceForTesting ?? .shared }

    static func configureServiceForTesting(_ service: ProjectWorktreeLifecycleService?) {
        serviceForTesting = service
    }

    enum Operation: Equatable { case read, refresh, preview, apply }

    struct Target: Equatable {
        let projectID: String
        let operation: Operation
    }

    static func target(_ path: String) -> Target? {
        guard path.hasPrefix("/v1/projects/") else { return nil }
        let parts = path.dropFirst("/v1/projects/".count)
            .split(separator: "/", omittingEmptySubsequences: false).map(String.init)
        guard parts.count >= 2, !parts[0].isEmpty, parts[1] == "worktrees" else { return nil }
        switch parts.count {
        case 2: return Target(projectID: parts[0], operation: .read)
        case 3 where parts[2] == "refresh": return Target(projectID: parts[0], operation: .refresh)
        case 4 where parts[2] == "cleanup" && parts[3] == "preview":
            return Target(projectID: parts[0], operation: .preview)
        case 4 where parts[2] == "cleanup" && parts[3] == "apply":
            return Target(projectID: parts[0], operation: .apply)
        default: return nil
        }
    }

    static func owns(_ path: String) -> Bool { target(path) != nil }

    enum Admission {
        case response(RemoteServer.Response)
        case work(() -> RemoteServer.Response)
    }

    /// Authorization first, then the closed method/query/body shape. Nothing here runs git.
    static func admit(_ request: RemoteServer.Request, machine: Bool,
                      permission: RemoteAuth.Verdict) -> Admission? {
        guard let target = target(request.path) else { return nil }
        let cloud: Bool
        if case .verifiedCloud = request.source { cloud = true } else { cloud = false }
        switch target.operation {
        case .read, .refresh:
            var mayRead = machine && !cloud
            if case .allowed(_, let caps) = permission, caps.contains(.read) { mayRead = true }
            guard mayRead else {
                return .response(.error(401, "unauthorized",
                                        "This needs a paired device or this Mac's orchestrator token."))
            }
        case .preview, .apply:
            guard machine, !cloud else {
                return .response(.error(403, "machine_token_required",
                    "Worktree cleanup preview and apply need this Mac's orchestrator token; a read capability grants neither."))
            }
        }
        let method = target.operation == .read ? "GET" : "POST"
        guard request.method == method else { return .response(.status(405)) }
        guard request.query.isEmpty, request.repeatedQueryKeys.isEmpty else {
            return .response(.error(400, "bad_request", "Worktree lifecycle routes take no query fields."))
        }
        guard request.body.count <= maximumBodyBytes else {
            return .response(.error(413, "body_too_large", "A worktree lifecycle body is at most 16 KiB."))
        }
        let body: [String: Any]
        if request.body.isEmpty || target.operation == .read {
            guard request.body.isEmpty else {
                return .response(.error(400, "bad_request", "This route takes no body."))
            }
            body = [:]
        } else {
            guard let parsed = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] else {
                return .response(.error(400, "bad_request", "The body must be one JSON object."))
            }
            body = parsed
        }
        let projectID = target.projectID
        switch target.operation {
        case .read:
            return .response(encode(service.snapshot(projectID: projectID), key: "projectWorktreeLifecycle"))
        case .refresh:
            guard body.isEmpty else {
                return .response(.error(400, "bad_request", "A refresh takes an empty body."))
            }
            return .work { encode(service.refresh(projectID: projectID), key: "projectWorktreeLifecycle") }
        case .preview:
            guard Set(body.keys).isSubset(of: ["worktrees"]) else {
                return .response(.error(400, "bad_request", "A preview body may name only worktrees."))
            }
            var worktrees: [String]?
            if let raw = body["worktrees"] {
                guard let ids = raw as? [String], (1...ProjectWorktreeLifecycleService.rowLimit).contains(ids.count),
                      Set(ids).count == ids.count, ids.allSatisfy(isWorktreeID) else {
                    return .response(.error(400, "bad_worktree_ids",
                        "worktrees must be 1–200 distinct ids from a snapshot, never paths."))
                }
                worktrees = ids
            }
            return .work {
                encode(service.preview(projectID: projectID, worktreeIDs: worktrees),
                       key: "projectWorktreeCleanupPreview")
            }
        case .apply:
            let confirmNumber = body["confirm"] as? NSNumber
            guard Set(body.keys) == ["preview_id", "pin_digest", "confirm", "idempotency_key"],
                  let previewID = body["preview_id"] as? String, previewID.count <= 64,
                  let pinDigest = body["pin_digest"] as? String, pinDigest.count == 64,
                  let confirmNumber,
                  CFGetTypeID(confirmNumber) == CFBooleanGetTypeID(),
                  let confirm = confirmNumber as? Bool,
                  let key = body["idempotency_key"] as? String else {
                return .response(.error(400, "bad_request",
                    "Apply takes exactly preview_id, pin_digest, confirm and idempotency_key."))
            }
            return .work {
                encode(service.apply(projectID: projectID, previewID: previewID, pinDigest: pinDigest,
                                     confirm: confirm, idempotencyKey: key),
                       key: "projectWorktreeCleanupReceipt")
            }
        }
    }

    static func isWorktreeID(_ value: String) -> Bool {
        guard value.hasPrefix("wt-") else { return false }
        let digest = value.dropFirst(3)
        return digest.count == 24 && digest.allSatisfy { ("0"..."9").contains($0) || ("a"..."f").contains($0) }
    }

    static func encode(_ result: Result<[String: Any], ProjectWorktreeLifecycleService.Refusal>,
                       key: String) -> RemoteServer.Response {
        switch result {
        case .success(let payload):
            let reply = RemoteServer.Response.json([key: payload])
            guard !reply.body.isEmpty, reply.body.count <= maximumResponseBytes else {
                return .error(503, "worktree_response_too_large",
                              "The worktree answer exceeds its byte budget.")
            }
            return reply
        case .failure(let refusal):
            var extra = refusal.extra
            if let next = refusal.nextOwner { extra["nextOwner"] = next }
            return .error(refusal.status, refusal.code, refusal.message, extra: extra)
        }
    }

    /// The lane: origin refusals in the server's order, then admission, then bounded off-queue work
    /// completed back on the server's own queue.
    static func start(_ request: RemoteServer.Request, machine: Bool, permission: RemoteAuth.Verdict,
                      preAuthRefusal: RemoteServer.Response?, postAuthRefusal: RemoteServer.Response?,
                      decorate: @escaping (RemoteServer.Response) -> RemoteServer.Response,
                      completeOnOwner: @escaping (@escaping () -> Void) -> Void,
                      deliver: @escaping (RemoteServer.Response) -> Void) {
        if let preAuthRefusal { deliver(decorate(preAuthRefusal)); return }
        guard let admission = admit(request, machine: machine, permission: permission) else {
            deliver(decorate(.error(404, "not_found", "No such route"))); return
        }
        if case .response(let response) = admission, response.status == 401 || response.status == 403 {
            deliver(decorate(response)); return
        }
        if let postAuthRefusal { deliver(decorate(postAuthRefusal)); return }
        switch admission {
        case .response(let response):
            deliver(decorate(response))
        case .work(let work):
            admissionLock.lock()
            guard outstanding < depth else {
                admissionLock.unlock()
                deliver(decorate(.error(429, "worktree_lifecycle_busy",
                    "Worktree lifecycle work is already queued on this Mac; try again shortly.")))
                return
            }
            outstanding += 1
            admissionLock.unlock()
            lane.async {
                let response = work()
                admissionLock.lock(); outstanding -= 1; admissionLock.unlock()
                completeOnOwner { deliver(decorate(response)) }
            }
        }
    }
}
