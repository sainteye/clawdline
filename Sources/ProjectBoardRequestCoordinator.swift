import Foundation

/// Owns Board request execution outside RemoteServer's interactive owner.
///
/// Authentication, body decoding and request identity are admitted by the adapter first. Commands
/// then enter one bounded serial writer; reads enter a separate bounded serial encoder lane, so a
/// maximum-size GET never queues behind command durability and neither lane occupies message/SSE
/// admission. The coordinator itself is owner-queue confined; workers return accounting to that
/// owner before delivery.
final class ProjectBoardRequestCoordinator {
    typealias Executor = (@escaping () -> Void) -> Void

    struct CommandIdentity: Hashable {
        let actor: String
        let requestID: String
    }

    struct State {
        let commands: Int
        let reads: Int
        let joinedWaiters: Int
    }

    static let commandDepth = 8
    static let readDepth = 4
    static let replyDepthPerCommand = 8

    private struct PendingCommand {
        let fingerprint: String
        var waiters: [(RemoteServer.Response) -> Void]
    }

    private let commandWorker = DispatchQueue(label: "com.tsunamiworks.clawdline.board.commands",
                                              qos: .utility)
    private let readWorker = DispatchQueue(label: "com.tsunamiworks.clawdline.board.reads",
                                           qos: .userInitiated)
    private var pending: [CommandIdentity: PendingCommand] = [:]
    private var readOutstanding = 0

    func start(_ request: RemoteServer.Request, machine: Bool, permission: RemoteAuth.Verdict,
               preAuthRefusal: RemoteServer.Response?, postAuthRefusal: RemoteServer.Response?,
               decorate: @escaping (RemoteServer.Response) -> RemoteServer.Response,
               completeOnOwner: @escaping (@escaping () -> Void) -> Void,
               deliver: @escaping (RemoteServer.Response) -> Void) {
        if let preAuthRefusal { deliver(decorate(preAuthRefusal)); return }
        if !machine, case .denied = permission {
            deliver(decorate(.error(401, "unauthorized", "This needs a paired device.")))
            return
        }
        if let postAuthRefusal { deliver(decorate(postAuthRefusal)); return }
        guard let admission = ProjectBoardHTTP.admit(
            request, machine: machine, permission: permission) else {
            deliver(decorate(.status(404)))
            return
        }
        switch admission {
        case .response(let response): deliver(decorate(response))
        case .read(let read):
            startRead(work: { decorate(read.execute()) }, completeOnOwner: completeOnOwner,
                      deliver: deliver)
        case .command(let command):
            startCommand(identity: .init(actor: command.actor, requestID: command.requestID),
                         fingerprint: command.fingerprint,
                         work: { decorate(command.execute()) },
                         completeOnOwner: completeOnOwner, deliver: deliver)
        }
    }

    func startCommand(identity: CommandIdentity, fingerprint: String,
                      executor override: Executor? = nil,
                      work: @escaping () -> RemoteServer.Response,
                      completeOnOwner: @escaping (@escaping () -> Void) -> Void,
                      deliver: @escaping (RemoteServer.Response) -> Void) {
        if var active = pending[identity] {
            guard active.fingerprint == fingerprint else {
                deliver(.error(409, "request_id_conflict",
                               "requestId is already executing with a different Board command."))
                return
            }
            guard active.waiters.count < Self.replyDepthPerCommand else {
                var response = RemoteServer.Response.error(
                    429, "board_command_busy",
                    "This Board command already has its reply capacity in hand. Retry the same request later.",
                    extra: ["retry_after": 1, "retry_debt": active.waiters.count])
                response.headers["Retry-After"] = "1"
                deliver(response)
                return
            }
            active.waiters.append(deliver)
            pending[identity] = active
            return
        }
        guard pending.count < Self.commandDepth else {
            var response = RemoteServer.Response.error(
                429, "board_command_busy",
                "This Mac already has \(Self.commandDepth) Board commands in hand. Try again after they drain.",
                extra: ["retry_after": 1, "retry_debt": pending.count])
            response.headers["Retry-After"] = "1"
            deliver(response)
            return
        }
        pending[identity] = PendingCommand(fingerprint: fingerprint, waiters: [deliver])
        let execute = override ?? { [commandWorker] operation in
            commandWorker.async(execute: operation)
        }
        execute {
            let response = work()
            completeOnOwner {
                let waiters = self.pending.removeValue(forKey: identity)?.waiters ?? []
                for waiter in waiters { waiter(response) }
            }
        }
    }

    func startRead(executor override: Executor? = nil,
                   work: @escaping () -> RemoteServer.Response,
                   completeOnOwner: @escaping (@escaping () -> Void) -> Void,
                   deliver: @escaping (RemoteServer.Response) -> Void) {
        guard readOutstanding < Self.readDepth else {
            var response = RemoteServer.Response.error(
                429, "board_read_busy",
                "This Mac already has \(Self.readDepth) Board reads in hand. Try again after they drain.",
                extra: ["retry_after": 1, "retry_debt": readOutstanding])
            response.headers["Retry-After"] = "1"
            deliver(response)
            return
        }
        readOutstanding += 1
        let execute = override ?? { [readWorker] operation in
            readWorker.async(execute: operation)
        }
        execute {
            let response = work()
            completeOnOwner {
                precondition(self.readOutstanding > 0)
                self.readOutstanding -= 1
                deliver(response)
            }
        }
    }

    var state: State {
        State(commands: pending.count, reads: readOutstanding,
              joinedWaiters: pending.values.reduce(0) { $0 + max(0, $1.waiters.count - 1) })
    }
}
