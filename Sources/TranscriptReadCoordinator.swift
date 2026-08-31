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

    private let worker = DispatchQueue(
        label: "com.tsunamiworks.clawdline.remote.transcript-reading")
    private var limiter = Limiter()

    var counts: (total: Int, background: Int) {
        (limiter.count, limiter.backgroundCount)
    }

    /// Called on the adapter's owner queue. `completeOnOwner` returns accounting there; the
    /// counter moves before `deliver`, so an ignored or interrupted delivery cannot strand debt.
    func start<Result>(foreground: Bool,
               executor override: Executor? = nil,
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
        let execute = override ?? { [worker] work in worker.async(execute: work) }
        execute {
            let result = work()
            completeOnOwner {
                self.limiter.finish(foreground: foreground)
                deliver(result)
            }
        }
    }
}
