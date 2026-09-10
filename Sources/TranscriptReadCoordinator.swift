import Foundation

/// Owns transcript-read admission, serial execution and completion accounting independently of
/// any transport. Its generic result is deliberately opaque: HTTP authentication, response
/// encoding, cache policy and delivery remain the caller's adapter responsibility.
final class TranscriptReadCoordinator {
    typealias Executor = (@escaping () -> Void) -> Void

    static let depth = 2
    static let backgroundDepth = 1

    struct Limiter {
        private(set) var count = 0
        private(set) var backgroundCount = 0

        mutating func admit(foreground: Bool, depth: Int, backgroundDepth: Int) -> Bool {
            guard count < depth else { return false }
            guard foreground || backgroundCount < backgroundDepth else { return false }
            count += 1
            if !foreground { backgroundCount += 1 }
            return true
        }

        mutating func finish(foreground: Bool) {
            precondition(count > 0)
            count -= 1
            if !foreground {
                precondition(backgroundCount > 0)
                backgroundCount -= 1
            }
        }
    }

    // Interactive reads and agent/background reads have separate serial executors. Each lane is
    // bounded to one active parse, so an agent cannot make a person wait behind a large rollout,
    // while neither class can fan out an unbounded number of JSON decoders on this Mac.
    private let interactiveWorker = DispatchQueue(
        label: "com.tsunamiworks.clawdline.remote.transcript-reading.interactive")
    private let backgroundWorker = DispatchQueue(
        label: "com.tsunamiworks.clawdline.remote.transcript-reading.background")
    private var limiter = Limiter()

    var counts: (total: Int, background: Int) {
        (limiter.count, limiter.backgroundCount)
    }

    /// Called on the adapter's owner queue. `completeOnOwner` returns accounting there; the
    /// counter moves before `deliver`, so an ignored or interrupted delivery cannot strand debt.
    func start<Result>(foreground: Bool,
               executor override: Executor? = nil,
               admitted: (_ total: Int, _ background: Int) -> Void = { _, _ in },
               refusal: (_ retryDebt: Int) -> Result,
               work: @escaping () -> Result,
               completeOnOwner: @escaping (@escaping () -> Void) -> Void,
               deliver: @escaping (Result) -> Void) {
        guard limiter.admit(
            foreground: foreground,
            depth: Self.depth,
            backgroundDepth: Self.backgroundDepth) else {
            deliver(refusal(limiter.count))
            return
        }
        admitted(limiter.count, limiter.backgroundCount)
        let worker = foreground ? interactiveWorker : backgroundWorker
        let execute = override ?? { work in worker.async(execute: work) }
        execute {
            let result = work()
            completeOnOwner {
                self.limiter.finish(foreground: foreground)
                deliver(result)
            }
        }
    }
}

/// One local trace joins admission, bounded queueing and parsing without copying transcript text.
/// It is kept beside the coordinator so RemoteServer remains only the transport adapter.
struct TranscriptReadDiagnostics: Sendable {
    let foreground: Bool
    private let id = String(UUID().uuidString.prefix(8)).lowercased()
    private let lane: String
    private let source: String
    private let path: String
    private let admittedAt = DispatchTime.now().uptimeNanoseconds

    init(_ request: RemoteServer.Request) {
        foreground = RemoteServer.isForegroundTranscript(request.query)
        lane = foreground ? "interactive" : "background"
        path = request.path
        switch request.source {
        case .http: source = "http"
        case .verifiedCloud(let sender):
            source = "cloud:\(CloudAppBridge.channelSegment(sender))"
        }
    }

    func admitted(total: Int, background: Int) {
        Log.write("transcript: admitted id=\(id) lane=\(lane) source=\(source) path=\(path) "
            + "total=\(total) background=\(background)")
    }

    func refused(_ debt: Int) {
        Log.write("transcript: refused id=\(id) lane=\(lane) source=\(source) path=\(path) "
            + "status=429 debt=\(debt)")
    }

    func measure(_ work: () -> RemoteServer.Response) -> RemoteServer.Response {
        let startedAt = DispatchTime.now().uptimeNanoseconds
        let response = work()
        let finishedAt = DispatchTime.now().uptimeNanoseconds
        Log.write("transcript: completed id=\(id) lane=\(lane) source=\(source) path=\(path) "
            + "queue_ms=\(Self.milliseconds(admittedAt, startedAt)) "
            + "work_ms=\(Self.milliseconds(startedAt, finishedAt)) "
            + "total_ms=\(Self.milliseconds(admittedAt, finishedAt)) "
            + "status=\(response.status) bytes=\(response.body.count)")
        return response
    }

    private static func milliseconds(_ start: UInt64, _ end: UInt64) -> UInt64 {
        end >= start ? (end - start) / 1_000_000 : 0
    }
}
