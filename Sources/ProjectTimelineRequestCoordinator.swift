import Foundation

/// Separate bounded read/write lanes keep Timeline encoding and durability off Session traffic.
final class ProjectTimelineRequestCoordinator {
    static let readDepth = 4
    static let commandDepth = 8
    private let readWorker = DispatchQueue(label: "com.tsunamiworks.clawdline.timeline.reads", qos: .userInitiated)
    private let commandWorker = DispatchQueue(label: "com.tsunamiworks.clawdline.timeline.commands", qos: .utility)
    private var reads = 0
    private var commands = 0

    func start(_ request: RemoteServer.Request, machine: Bool, permission: RemoteAuth.Verdict,
               preAuthRefusal: RemoteServer.Response?, postAuthRefusal: RemoteServer.Response?,
               decorate: @escaping (RemoteServer.Response) -> RemoteServer.Response,
               completeOnOwner: @escaping (@escaping () -> Void) -> Void,
               deliver: @escaping (RemoteServer.Response) -> Void) {
        if let preAuthRefusal { deliver(decorate(preAuthRefusal)); return }
        if !machine, case .denied = permission {
            deliver(decorate(.error(401, "unauthorized", "This needs a paired device."))); return
        }
        if let postAuthRefusal { deliver(decorate(postAuthRefusal)); return }
        guard let admission = ProjectTimelineHTTP.admit(request, machine: machine, permission: permission) else {
            deliver(decorate(.status(404))); return
        }
        switch admission {
        case .response(let response): deliver(decorate(response))
        case .read(let read):
            guard reads < Self.readDepth else { deliver(decorate(busy("timeline_read_busy", debt: reads))); return }
            reads += 1
            readWorker.async {
                let response = decorate(read.execute())
                completeOnOwner { self.reads -= 1; deliver(response) }
            }
        case .command(let command):
            guard commands < Self.commandDepth else { deliver(decorate(busy("timeline_command_busy", debt: commands))); return }
            commands += 1
            commandWorker.async {
                let response = decorate(command.execute())
                completeOnOwner { self.commands -= 1; deliver(response) }
            }
        }
    }

    private func busy(_ code: String, debt: Int) -> RemoteServer.Response {
        var response = RemoteServer.Response.error(429, code,
            "This Mac already has the bounded Timeline lane in use. Retry after it drains.",
            extra: ["retry_after": 1, "retry_debt": debt])
        response.headers["Retry-After"] = "1"
        return response
    }
}
