import Foundation
import Network

/// The bar, as something other than a bar.
///
/// Everything this app knows is already computed for four consumers — the panel, the strip above
/// the transcript, the menu bar and the island — by one shared reading. ``SessionWatch``'s own
/// comment says it out loud: *one reading of what every session is doing, for everything that
/// wants to know*. This is the fifth consumer, and it is the first one that is not a piece of
/// screen: an HTTP surface, so that a browser on the sofa, a phone, or somebody else's script can
/// ask the same questions the panel asks.
///
/// **Bound to the loopback address and off until somebody turns it on.** Not a default, not a
/// "probably fine": a listening socket is the difference between a program on your machine and a
/// service on your machine, and that difference should be a thing you did on purpose.
///
/// Hand-written HTTP/1.1 on `NWListener` rather than a framework, for the same reason the rest of
/// this has no dependencies — the surface is a handful of routes and a text protocol, and a
/// package here would be a build system, a lockfile and somebody else's release schedule in
/// exchange for about three hundred lines.
///
/// **What it is allowed to do is the point.** Reading a session leaks a repository name, a branch
/// and a task title; *writing* to one is remote code execution, because Claude Code runs `bash`.
/// Those two are not the same feature and they do not ship together — see `docs/remote.md`. Until
/// the write half exists and is separately armed, every mutating route answers `write_disabled`.
final class RemoteServer {

    static let shared = RemoteServer()
    private init() {}

    /// Bumped when a client would have to be changed. The path carries the same number, so a
    /// client that speaks `/v1` never has to look at this — it exists for the health route, where
    /// a person is asking "what am I talking to".
    static let protocolVersion = 1

    private let queue = DispatchQueue(label: "dev.sainteye.clawdline.remote")
    private var listener: NWListener?
    private var streams: [ObjectIdentifier: Stream] = [:]
    private var nextEventID = 0

    // MARK: - Lifecycle

    var isRunning: Bool { listener != nil }
    private(set) var port: UInt16 = 0

    /// Start, stop, or restart to match the config. Safe to call whenever anything changes.
    func apply() {
        let want = Config.shared.remote
        let wantPort = UInt16(Config.shared.remotePort)
        if want, isRunning, wantPort == port { return }
        stop()
        guard want else { return }
        start(on: wantPort)
    }

    private func start(on wanted: UInt16) {
        let params = NWParameters.tcp
        // Loopback and nothing else. A listener that accepts from the local network is one
        // coffee shop away from being a listener that accepts from the coffee shop, and the way
        // out of this machine is a tunnel that dials out — never an interface that waits.
        params.requiredLocalEndpoint = NWEndpoint.hostPort(host: .ipv4(.loopback),
                                                           port: NWEndpoint.Port(rawValue: wanted) ?? 7717)
        params.allowLocalEndpointReuse = true
        guard let listener = try? NWListener(using: params) else {
            Log.write("remote: could not listen on 127.0.0.1:\(wanted)")
            return
        }
        listener.newConnectionHandler = { [weak self] conn in self?.accept(conn) }
        listener.stateUpdateHandler = { [weak self] state in
            switch state {
            case .ready:
                self?.port = listener.port?.rawValue ?? wanted
                Log.write("remote: listening on http://127.0.0.1:\(self?.port ?? wanted)/")
            case .failed(let error):
                Log.write("remote: listener failed — \(error.localizedDescription)")
                self?.stop()
            default: break
            }
        }
        listener.start(queue: queue)
        self.listener = listener
        self.port = wanted

        // One observer for every stream there will ever be. Registering per client would mean a
        // reading fanned out by the watch to N closures that all do the same work.
        SessionWatch.shared.observers["remote"] = { [weak self] in self?.broadcast() }
    }

    func stop() {
        SessionWatch.shared.observers.removeValue(forKey: "remote")
        listener?.cancel()
        listener = nil
        queue.async { [weak self] in
            guard let self else { return }
            for stream in self.streams.values { stream.connection.cancel() }
            self.streams.removeAll()
        }
    }

    // MARK: - Connections

    private func accept(_ conn: NWConnection) {
        conn.start(queue: queue)
        receive(conn, buffer: Data())
    }

    /// Read until the headers are complete, then answer.
    ///
    /// The body is read only when a `Content-Length` says there is one, and it is capped — this
    /// listens on loopback, but "on loopback" is not a reason to let anything on the machine hand
    /// it a gigabyte.
    private func receive(_ conn: NWConnection, buffer: Data) {
        conn.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) { [weak self] data, _, done, error in
            guard let self else { return }
            var buffer = buffer
            if let data { buffer.append(data) }
            if error != nil || (done && buffer.isEmpty) { conn.cancel(); return }

            guard let headEnd = Self.range(of: Data("\r\n\r\n".utf8), in: buffer) else {
                if buffer.count > 64 * 1024 { self.send(.status(431), on: conn); return }
                if done { conn.cancel(); return }
                self.receive(conn, buffer: buffer)
                return
            }
            guard var request = Request(head: buffer[buffer.startIndex..<headEnd.lowerBound]) else {
                self.send(.error(400, "bad_request", "Could not read that request"), on: conn)
                return
            }
            let bodyStart = headEnd.upperBound
            let want = min(request.contentLength, 1 << 20)
            let have = buffer.count - (bodyStart - buffer.startIndex)
            if have < want {
                if done { conn.cancel(); return }
                self.receive(conn, buffer: buffer)
                return
            }
            request.body = buffer[bodyStart..<(bodyStart + want)]
            self.handle(request, on: conn)
        }
    }

    private static func range(of needle: Data, in haystack: Data) -> Range<Data.Index>? {
        haystack.range(of: needle)
    }

    // MARK: - Routing

    private func handle(_ request: Request, on conn: NWConnection) {
        // The event stream is the one route that does not answer and close.
        if request.method == "GET", request.path == "/v1/events" {
            openStream(on: conn)
            return
        }
        let response = route(request)
        send(response, on: conn)
    }

    /// Every route that answers with a body and closes. Split out from the connection handling so
    /// that a test can ask it a question without opening a socket.
    func route(_ request: Request) -> Response {
        switch (request.method, request.path) {

        case ("GET", "/v1/health"):
            return .json([
                "ok": true,
                "version": Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "?",
                "protocol": Self.protocolVersion,
                // The client uses this to decide whether to draw a composer at all. Saying "you
                // may not" once is kinder than a button that fails when pressed.
                "write": false,
            ])

        case ("GET", "/v1/sessions"):
            return .json(sessionsPayload())

        case ("GET", let path) where path.hasPrefix("/v1/sessions/"):
            let rest = String(path.dropFirst("/v1/sessions/".count))
            let parts = rest.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
            guard let id = parts.first?.removingPercentEncoding, !id.isEmpty else {
                return .error(404, "not_found", "No session named that")
            }
            guard let session = session(withID: id) else {
                return .error(404, "not_found", "No session named that")
            }
            if parts.count == 1 {
                return .json(["session": json(of: session)])
            }
            if parts.count == 2, parts[1] == "transcript" {
                let limit = min(max(Int(request.query["limit"] ?? "") ?? 200, 1), 1000)
                return transcriptPayload(for: session, limit: limit)
            }
            return .error(404, "not_found", "No such route")

        // Every mutating route, in one place, saying the same thing. They exist now rather than
        // later so that a client written today finds a documented refusal instead of a 404 it has
        // to guess about.
        case ("POST", let path) where path.hasPrefix("/v1/sessions/"):
            return .error(403, "write_disabled",
                          "This build can read sessions and not write to them. See docs/remote.md.")

        case ("GET", "/"), ("GET", "/index.html"):
            return page()

        case ("GET", "/manifest.webmanifest"):
            return manifest()

        default:
            return .error(404, "not_found", "No such route")
        }
    }

    // MARK: - What the routes answer with

    private func sessionsPayload() -> [String: Any] {
        let watch = SessionWatch.shared
        return [
            "sessions": watch.targets.map { json(of: $0) },
            "at": Int(Date().timeIntervalSince1970),
        ]
    }

    private func session(withID id: String) -> TargetSession? {
        SessionWatch.shared.targets.first { $0.id == id }
    }

    private func json(of session: TargetSession) -> [String: Any] {
        let watch = SessionWatch.shared
        let state = watch.states[session.id] ?? .unknown
        var out: [String: Any] = [
            "id": session.id,
            "backend": session.backend.rawValue,
            "tty": session.tty.replacingOccurrences(of: "/dev/", with: ""),
            "label": session.label,
            "isClaude": session.isClaude,
            "state": name(of: state),
        ]
        if case .working(let line) = state { out["line"] = line }
        if let cwd = Targets.workingDirectory(of: session) { out["cwd"] = cwd }
        if let sessionID = HookBridge.note(for: session)?.session { out["sessionId"] = sessionID }
        if let grid = watch.grid(of: session.id) { out["icon"] = json(of: grid) }
        return out
    }

    private func name(of state: SessionState) -> String {
        switch state {
        case .working: return "working"
        case .waiting: return "waiting"
        case .idle:    return "idle"
        case .unknown: return "unknown"
        }
    }

    /// The mark as colours rather than as a picture.
    ///
    /// A PNG would have been fewer bytes and a worse answer: the client draws these at whatever
    /// size it is drawing at, on a screen whose pixel ratio this end does not know, and a pixel
    /// mark that has been resampled is not a pixel mark any more.
    private func json(of grid: ProjectIcon.Grid) -> [String: Any] {
        [
            "accent": ProjectIcon.hex(grid.accent),
            "cells": grid.cells.map { row in
                row.map { $0.map { ProjectIcon.hex($0) as Any } ?? (NSNull() as Any) }
            },
        ]
    }

    private func transcriptPayload(for session: TargetSession, limit: Int) -> Response {
        guard session.isClaude,
              let cwd = Targets.workingDirectory(of: session),
              let file = Transcript.locate(cwd: cwd, tabTitle: session.name,
                                           startedAt: Targets.processStart(of: session),
                                           sessionID: HookBridge.note(for: session)?.session),
              let text = Transcript.tail(of: file, bytes: 8 << 20) else {
            // Not an error. A session that has not spoken yet has an empty transcript, and that
            // is a different thing from a session that could not be found.
            return .json(["entries": [], "signature": ""])
        }
        let entries = Transcript.parse(text, limit: limit).map { entry -> [String: Any] in
            var row: [String: Any] = ["role": name(of: entry.kind), "text": entry.text]
            if let tool = entry.tool { row["tool"] = tool }
            if let time = entry.time { row["at"] = Int(time.timeIntervalSince1970) }
            return row
        }
        return .json(["entries": entries, "signature": Transcript.signature(of: file)])
    }

    private func name(of kind: Transcript.Entry.Kind) -> String {
        switch kind {
        case .user:       return "user"
        case .assistant:  return "assistant"
        case .tool:       return "tool"
        case .toolResult: return "tool"
        }
    }

    // MARK: - The page

    private func page() -> Response {
        guard let url = Bundle.main.url(forResource: "index", withExtension: "html",
                                        subdirectory: "web"),
              let data = try? Data(contentsOf: url) else {
            return .error(404, "not_found", "The web interface is not in this build")
        }
        return Response(status: 200, headers: ["Content-Type": "text/html; charset=utf-8"], body: data)
    }

    private func manifest() -> Response {
        let obj: [String: Any] = [
            "name": "Clawdline",
            "short_name": "Clawdline",
            "display": "standalone",
            "background_color": "#0e0e11",
            "theme_color": "#0e0e11",
            "start_url": "/",
            "scope": "/",
        ]
        let data = (try? JSONSerialization.data(withJSONObject: obj, options: [.prettyPrinted])) ?? Data()
        return Response(status: 200,
                        headers: ["Content-Type": "application/manifest+json; charset=utf-8"],
                        body: data)
    }

    // MARK: - The event stream

    private final class Stream {
        let connection: NWConnection
        init(_ connection: NWConnection) { self.connection = connection }
    }

    private func openStream(on conn: NWConnection) {
        let head = """
        HTTP/1.1 200 OK\r
        Content-Type: text/event-stream; charset=utf-8\r
        Cache-Control: no-store\r
        Connection: keep-alive\r
        \r\n
        """
        conn.send(content: Data(head.utf8), completion: .contentProcessed { _ in })
        let stream = Stream(conn)
        streams[ObjectIdentifier(stream)] = stream
        conn.stateUpdateHandler = { [weak self, weak stream] state in
            switch state {
            case .cancelled, .failed:
                guard let stream else { return }
                self?.streams.removeValue(forKey: ObjectIdentifier(stream))
            default: break
            }
        }

        // Hello, then the current state — so a client that has just reconnected is level without
        // asking, and never has to replay anything it missed. That is the whole reason the stream
        // carries the entire list on every change rather than a diff.
        write(event: "hello", data: [
            "version": Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "?",
            "protocol": Self.protocolVersion,
            "write": false,
        ], to: stream)
        DispatchQueue.main.async {
            let payload = self.sessionsPayload()
            self.queue.async { self.write(event: "sessions", data: payload, to: stream) }
        }
        startHeartbeat()
    }

    /// A comment line every fifteen seconds. Nothing reads it — its job is to be bytes, so that a
    /// proxy or a phone radio that drops idle connections finds this one is not idle.
    private var heartbeat: DispatchSourceTimer?
    private func startHeartbeat() {
        guard heartbeat == nil else { return }
        let timer = DispatchSource.makeTimerSource(queue: queue)
        timer.schedule(deadline: .now() + 15, repeating: 15)
        timer.setEventHandler { [weak self] in
            guard let self else { return }
            guard !self.streams.isEmpty else { self.heartbeat?.cancel(); self.heartbeat = nil; return }
            for stream in self.streams.values {
                stream.connection.send(content: Data(": ping\n\n".utf8),
                                       completion: .contentProcessed { _ in })
            }
        }
        timer.resume()
        heartbeat = timer
    }

    /// Called on the main thread by the watch. The payload is built there, where the state lives,
    /// and only the writing crosses over.
    private func broadcast() {
        let payload = sessionsPayload()
        queue.async { [weak self] in
            guard let self, !self.streams.isEmpty else { return }
            for stream in self.streams.values { self.write(event: "sessions", data: payload, to: stream) }
        }
    }

    private func write(event: String, data: [String: Any], to stream: Stream) {
        guard let json = try? JSONSerialization.data(withJSONObject: data,
                                                     options: [.withoutEscapingSlashes]),
              let text = String(data: json, encoding: .utf8) else { return }
        nextEventID += 1
        let frame = "event: \(event)\nid: \(nextEventID)\ndata: \(text)\n\n"
        stream.connection.send(content: Data(frame.utf8), completion: .contentProcessed { _ in })
    }

    // MARK: - Writing a response out

    private func send(_ response: Response, on conn: NWConnection) {
        conn.send(content: response.wire, completion: .contentProcessed { _ in conn.cancel() })
    }
}

// MARK: - The two halves of HTTP that this needs

extension RemoteServer {

    struct Request {
        var method = "GET"
        var path = "/"
        var query: [String: String] = [:]
        var headers: [String: String] = [:]
        var contentLength = 0
        var body: Data = Data()

        /// Parse a request head. Deliberately strict about the shape and uninterested in most of
        /// it: this answers a fixed list of routes and has no business being a general parser.
        init?(head: Data) {
            guard let text = String(data: head, encoding: .utf8) else { return nil }
            var lines = text.split(separator: "\r\n", omittingEmptySubsequences: false)
            guard !lines.isEmpty else { return nil }
            let start = lines.removeFirst().split(separator: " ")
            guard start.count >= 2 else { return nil }
            method = String(start[0])

            let target = String(start[1])
            if let mark = target.firstIndex(of: "?") {
                path = String(target[target.startIndex..<mark])
                for pair in target[target.index(after: mark)...].split(separator: "&") {
                    let kv = pair.split(separator: "=", maxSplits: 1)
                    guard let key = kv.first?.removingPercentEncoding else { continue }
                    query[key] = kv.count > 1 ? (kv[1].removingPercentEncoding ?? "") : ""
                }
            } else {
                path = target
            }

            for line in lines where line.contains(":") {
                let kv = line.split(separator: ":", maxSplits: 1)
                guard kv.count == 2 else { continue }
                headers[kv[0].lowercased().trimmingCharacters(in: .whitespaces)] =
                    kv[1].trimmingCharacters(in: .whitespaces)
            }
            contentLength = Int(headers["content-length"] ?? "") ?? 0
        }
    }

    struct Response {
        var status: Int
        var headers: [String: String] = [:]
        var body: Data = Data()

        static func json(_ object: [String: Any], status: Int = 200) -> Response {
            let data = (try? JSONSerialization.data(withJSONObject: object,
                                                    options: [.withoutEscapingSlashes])) ?? Data()
            return Response(status: status,
                            headers: ["Content-Type": "application/json; charset=utf-8"],
                            body: data)
        }

        /// One envelope, everywhere. A client that has handled one error has handled all of them,
        /// and `code` is the part it is allowed to branch on — `message` is for a person.
        static func error(_ status: Int, _ code: String, _ message: String) -> Response {
            .json(["error": ["code": code, "message": message,
                             "request_id": UUID().uuidString.lowercased()]], status: status)
        }

        static func status(_ code: Int) -> Response {
            Response(status: code, headers: ["Content-Type": "text/plain"], body: Data())
        }

        var wire: Data {
            var head = "HTTP/1.1 \(status) \(Response.reason(status))\r\n"
            var headers = self.headers
            headers["Content-Length"] = String(body.count)
            headers["Connection"] = "close"
            // Nothing here is meant to be embedded anywhere, and the page ships in the app rather
            // than being fetched from a site — so the browser is told to trust nothing external.
            headers["X-Content-Type-Options"] = "nosniff"
            headers["Referrer-Policy"] = "no-referrer"
            for (key, value) in headers.sorted(by: { $0.key < $1.key }) {
                head += "\(key): \(value)\r\n"
            }
            head += "\r\n"
            return Data(head.utf8) + body
        }

        static func reason(_ status: Int) -> String {
            switch status {
            case 200: return "OK"
            case 400: return "Bad Request"
            case 403: return "Forbidden"
            case 404: return "Not Found"
            case 431: return "Request Header Fields Too Large"
            default:  return "Error"
            }
        }
    }
}
