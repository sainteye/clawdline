import Foundation
import Network

/// Proves the loopback page is reachable before Settings sends a browser to it.
///
/// `RemoteServer.listener != nil` only means `start(queue:)` was called. On the 2026-09-08 launch
/// that state lasted 4.7 seconds before Network.framework reported `.ready`; Chrome opened during
/// the gap, rendered a permanent `ERR_FAILED`, and the listener coming up afterwards could not
/// repair the already-failed page. A real TCP connection is the boundary the browser needs, and
/// keeps this lifecycle concern out of the already frozen `RemoteServer.swift` transport owner.
enum LocalBrowserReadiness {
    private static let queue = DispatchQueue(label: "com.tsunamiworks.clawdline.browser-readiness")

    static func whenReadyForBrowser(port: UInt16, timeout: TimeInterval = 10,
                                    _ completion: @escaping (Bool) -> Void) {
        let deadline = Date().addingTimeInterval(timeout)
        queue.async { attempt(port: port, deadline: deadline, completion) }
    }

    private static func attempt(port: UInt16, deadline: Date,
                                _ completion: @escaping (Bool) -> Void) {
        guard Date() < deadline, let endpointPort = NWEndpoint.Port(rawValue: port) else {
            finish(false, completion)
            return
        }
        let connection = NWConnection(host: .ipv4(.loopback), port: endpointPort, using: .tcp)
        var settled = false

        func settle(_ ready: Bool) {
            guard !settled else { return }
            settled = true
            connection.cancel()
            if ready {
                finish(true, completion)
            } else if Date() < deadline {
                queue.asyncAfter(deadline: .now() + .milliseconds(50)) {
                    attempt(port: port, deadline: deadline, completion)
                }
            } else {
                finish(false, completion)
            }
        }

        connection.stateUpdateHandler = { state in
            switch state {
            case .ready: settle(true)
            case .failed, .cancelled: settle(false)
            default: break
            }
        }
        connection.start(queue: queue)
        // A connection stuck in `.waiting` or `.preparing` still has to release the attempt. The
        // outer deadline bounds the whole button press; this one-second edge only starts a fresh
        // probe so a single Network.framework object cannot consume all of it.
        queue.asyncAfter(deadline: .now() + 1) { settle(false) }
    }

    private static func finish(_ ready: Bool, _ completion: @escaping (Bool) -> Void) {
        DispatchQueue.main.async { completion(ready) }
    }
}
