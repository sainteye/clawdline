import Foundation
import Network

/// The lossy terminal-screen surface kept outside the durable transcript reader.
enum TerminalLivePreview {
    static let sessionDepth = 4
    static let waiterDepth = 6
    static let reuseNanoseconds: UInt64 = 450_000_000
    static let cacheRetentionNanoseconds: UInt64 = 10_000_000_000

    static func isPath(_ path: String) -> Bool {
        guard path.hasPrefix("/v1/sessions/") else { return false }
        let rest = path.dropFirst("/v1/sessions/".count)
        let parts = rest.split(separator: "/", omittingEmptySubsequences: false)
        return parts.count == 2 && !parts[0].isEmpty && parts[1] == "live"
    }

    static func sessionID(in path: String) -> String? {
        guard isPath(path) else { return nil }
        let rest = path.dropFirst("/v1/sessions/".count)
        guard let raw = rest.split(separator: "/", omittingEmptySubsequences: false).first,
              let id = String(raw).removingPercentEncoding, !id.isEmpty else { return nil }
        return id
    }

    static func response(for session: TargetSession) -> RemoteServer.Response {
        guard let screen = Targets.visibleScreen(of: session) else {
            return .error(503, "terminal_unavailable",
                          "The terminal screen could not be read right now.")
        }
        let text = plainText(screen)
        return .json([
            "text": text,
            "signature": signature(text),
            "at": Int(Date().timeIntervalSince1970),
        ])
    }

    static func busyResponse() -> RemoteServer.Response {
        .error(429, "live_preview_busy",
               "Terminal live preview is busy. Try again in a moment.")
    }

    static func plainText(_ screen: String) -> String {
        let plain = Ansi.plain(screen).trimmingCharacters(in: .whitespacesAndNewlines)
        let lines = plain.split(separator: "\n", omittingEmptySubsequences: false)
        return lines.suffix(160).joined(separator: "\n")
    }

    static func signature(_ text: String) -> String {
        // Fixed FNV-1a rather than Swift's intentionally randomised Hasher: a browser compares
        // readings across requests, so this must retain meaning for the process lifetime.
        var hash: UInt64 = 14_695_981_039_346_656_037
        for byte in text.utf8 {
            hash ^= UInt64(byte)
            hash &*= 1_099_511_628_211
        }
        return "\(text.utf8.count)-\(String(hash, radix: 16))"
    }
}

/// One capture per session, with brief reuse and bounded coalesced waiters. All state is touched
/// on RemoteServer's queue; terminal automation stays on its existing serial reading worker.
final class TerminalLiveBroker: @unchecked Sendable {
    typealias Response = RemoteServer.Response
    typealias Request = RemoteServer.Request

    private let serverQueue: DispatchQueue
    private let workerQueue: DispatchQueue
    private let perform: @Sendable (Request) -> Response
    private var waiters: [String: [(Response) -> Void]] = [:]
    private var cache: [String: (at: UInt64, response: Response)] = [:]

    init(serverQueue: DispatchQueue, workerQueue: DispatchQueue,
         perform: @escaping @Sendable (Request) -> Response) {
        self.serverQueue = serverQueue
        self.workerQueue = workerQueue
        self.perform = perform
    }

    func read(_ request: Request, deliver: @escaping (Response) -> Void) {
        guard let id = TerminalLivePreview.sessionID(in: request.path) else {
            deliver(.error(404, "not_found", "No session named that"))
            return
        }
        let now = DispatchTime.now().uptimeNanoseconds
        cache = cache.filter { now &- $0.value.at < TerminalLivePreview.cacheRetentionNanoseconds }
        if let cached = cache[id], now &- cached.at < TerminalLivePreview.reuseNanoseconds {
            deliver(cached.response)
            return
        }
        if waiters[id] != nil {
            guard waiters[id, default: []].count < TerminalLivePreview.waiterDepth else {
                deliver(TerminalLivePreview.busyResponse())
                return
            }
            waiters[id, default: []].append(deliver)
            return
        }
        guard waiters.count < TerminalLivePreview.sessionDepth else {
            deliver(TerminalLivePreview.busyResponse())
            return
        }
        waiters[id] = [deliver]
        workerQueue.async { [weak self] in
            guard let self else { return }
            let response = self.perform(request)
            self.serverQueue.async {
                let deliveries = self.waiters.removeValue(forKey: id) ?? []
                if response.status == 200 {
                    self.cache[id] = (DispatchTime.now().uptimeNanoseconds, response)
                }
                for delivery in deliveries { delivery(response) }
            }
        }
    }
}

extension RemoteServer {
    /// Kept with the terminal preview because it is the durable side of the same browser surface;
    /// moving it here holds RemoteServer's stop-growth boundary without changing its route shape.
    func transcriptPayload(for session: TargetSession, limit: Int) -> Response {
        guard let record = Transcript.record(of: session) else {
            return .json(["entries": [], "signature": ""])
        }
        let file = record.url
        var signature = Transcript.signature(of: file)
        guard var text = Transcript.tail(of: file, bytes: 8 << 20) else {
            return .json(["entries": [], "signature": ""])
        }
        let after = Transcript.signature(of: file)
        if after != signature, let fresh = Transcript.tail(of: file, bytes: 8 << 20) {
            signature = after
            text = fresh
        }
        let entries = Self.transcriptRows(
            Transcript.parse(text, assistant: record.assistant, limit: limit)
        )
        return .json(["entries": entries, "signature": signature])
    }
}
