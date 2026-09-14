import Foundation
import FoundationNetworking

struct ProbeResult: Encodable {
    let didOpen: Bool
    let httpStatus: Int?
    let errorDomain: String?
    let errorCode: Int?
    let elapsedMilliseconds: Int
}

final class ProbeObserver: NSObject, URLSessionWebSocketDelegate, @unchecked Sendable {
    private let lock = NSLock()
    private let done = DispatchSemaphore(value: 0)
    private let started = DispatchTime.now().uptimeNanoseconds
    private var finished = false
    private var result: ProbeResult?

    func wait(seconds: Double) -> ProbeResult {
        if done.wait(timeout: .now() + seconds) != .success {
            finish(didOpen: false, response: nil,
                   error: NSError(domain: NSURLErrorDomain, code: NSURLErrorTimedOut))
            _ = done.wait(timeout: .now() + 1)
        }
        lock.lock(); defer { lock.unlock() }
        return result!
    }

    func urlSession(
        _ session: URLSession,
        webSocketTask: URLSessionWebSocketTask,
        didOpenWithProtocol protocol: String?
    ) {
        finish(didOpen: true, response: webSocketTask.response, error: nil)
    }

    func urlSession(
        _ session: URLSession,
        task: URLSessionTask,
        didCompleteWithError error: Error?
    ) {
        finish(didOpen: false, response: task.response, error: error)
    }

    func urlSession(
        _ session: URLSession,
        webSocketTask: URLSessionWebSocketTask,
        didCloseWith closeCode: URLSessionWebSocketTask.CloseCode,
        reason: Data?
    ) {
        finish(
            didOpen: false,
            response: webSocketTask.response,
            error: NSError(domain: NSURLErrorDomain, code: Int(closeCode.rawValue)))
    }

    private func finish(didOpen: Bool, response: URLResponse?, error: Error?) {
        lock.lock()
        guard !finished else { lock.unlock(); return }
        finished = true
        let nsError = error as NSError?
        result = ProbeResult(
            didOpen: didOpen,
            httpStatus: (response as? HTTPURLResponse)?.statusCode,
            errorDomain: nsError?.domain,
            errorCode: nsError?.code,
            elapsedMilliseconds: Int(
                (DispatchTime.now().uptimeNanoseconds - started) / 1_000_000))
        lock.unlock()
        done.signal()
    }
}

guard CommandLine.arguments.count == 2,
      let url = URL(string: CommandLine.arguments[1]),
      ["ws", "wss", "http", "https"].contains(url.scheme?.lowercased() ?? "") else {
    fputs("usage: linux-websocket-upgrade-probe <ws(s)-or-http(s)-URL>\n", stderr)
    exit(64)
}

var request = URLRequest(url: url)
request.setValue("Bearer clawdline-invalid-linux-upgrade-probe", forHTTPHeaderField: "Authorization")
let observer = ProbeObserver()
let configuration = URLSessionConfiguration.ephemeral
configuration.timeoutIntervalForRequest = 15
configuration.timeoutIntervalForResource = 15
let session = URLSession(configuration: configuration, delegate: observer, delegateQueue: nil)
let task = session.webSocketTask(with: request)
task.maximumMessageSize = 32 * 1024 * 1024
task.resume()
let result = observer.wait(seconds: 16)
task.cancel(with: .goingAway, reason: nil)
session.invalidateAndCancel()
let encoder = JSONEncoder()
encoder.outputFormatting = [.sortedKeys]
FileHandle.standardOutput.write(try encoder.encode(result))
FileHandle.standardOutput.write(Data("\n".utf8))
