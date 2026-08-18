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
        // First, and outside the early return below: turning sending on or off must take effect
        // even when nothing about the listener changed, which is the usual case.
        syncWriteCapability()
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
        syncWriteCapability()
        // Made now rather than lazily, so that the first thing a script does is find a token
        // already sitting in ~/.config/clawdline/remote-token rather than a 401 it has to
        // understand. It does not satisfy the tunnel interlock — see RemoteAuth.isConfigured.
        _ = RemoteAuth.localToken()

        // One observer for every stream there will ever be. Registering per client would mean a
        // reading fanned out by the watch to N closures that all do the same work.
        SessionWatch.shared.observers["remote"] = { [weak self] in self?.broadcast() }
    }

    /// Bring every paired device in line with the one switch.
    ///
    /// Per-device grants would be a finer control and a worse one to have as the only one: the
    /// moment somebody wants sending off, they want it off *everywhere*, and having to walk a
    /// list is how one gets missed. The switch is the decision; the devices follow it.
    func syncWriteCapability() {
        let want: Set<RemoteAuth.Capability> = Config.shared.remoteWrite ? [.read, .send] : [.read]
        for device in RemoteAuth.approvedDevices where device.caps != want {
            RemoteAuth.setCapabilities(want, for: device.id)
        }
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
        // The event stream is the one route that does not answer and close — and the one that
        // carries everything, so it is gated before it is opened rather than after.
        if request.method == "GET", request.path == "/v1/events" {
            if let refusal = crossOriginRefusal(request) { send(refusal, on: conn); return }
            if case .denied = permission(for: request) {
                send(.error(401, "unauthorized", "This needs a paired device."), on: conn)
                return
            }
            openStream(on: conn)
            return
        }
        let response = route(request)
        send(response, on: conn)
    }

    /// Every route that answers with a body and closes. Split out from the connection handling so
    /// that a test can ask it a question without opening a socket.
    func route(_ request: Request) -> Response {
        // Before anything else, and before authentication, because these two refusals are about
        // *who is allowed to be asking at all* rather than about who they are.
        if let refusal = crossOriginRefusal(request) { return refusal }

        // The four things reachable without a token, and each of them is on this list for a
        // reason rather than for convenience: you cannot log in through a page you cannot load,
        // and you cannot pair with a machine you cannot ask.
        let open: Set<String> = ["/", "/index.html", "/manifest.webmanifest", "/v1/health"]
        let pairing = request.path.hasPrefix("/v1/auth/")
        if !open.contains(request.path), !pairing {
            if case .denied = permission(for: request) {
                return .error(401, "unauthorized", "This needs a paired device.")
            }
        }
        // A cookie is sent by the browser whether or not the page asking wanted it to be, so a
        // mutating route additionally has to be sure the request came from our own page. `Origin`
        // is set by the browser and cannot be forged by script — and the JSON content type below
        // is the second half of it, because the shapes a cross-site form can send do not include
        // one. Reads are exempt: they are already gated by the token.
        if request.method != "GET", let origin = request.headers["origin"], !isOurs(origin) {
            return .error(403, "forbidden", "That request did not come from this page.")
        }

        switch (request.method, request.path) {

        case ("POST", "/v1/auth/pair"):
            return beginPairing(request)

        case ("POST", "/v1/auth/pair/confirm"):
            return confirmPairing(request)

        case ("POST", "/v1/auth/adopt"):
            // Turn a token the page was handed into a cookie it can keep.
            //
            // This exists for one reason: **`EventSource` cannot set headers**, so a page holding
            // a bearer token in a variable cannot open the event stream with it. The token comes
            // in a URL fragment — which browsers do not send to servers and do not put in logs —
            // and is traded here for the cookie the stream will use. Nothing is granted: an
            // unknown token is refused exactly as it would be anywhere else.
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
            guard let token = body["token"] as? String,
                  case .allowed = RemoteAuth.verify(bearer: token) else {
                return .error(401, "unauthorized", "That token is not one of ours.")
            }
            return signedIn(token, secure: request.headers["x-forwarded-proto"] == "https")

        case ("POST", "/v1/auth/password"):
            return exchangePassword(request)

        case ("POST", "/v1/auth/logout"):
            return Response(status: 200,
                            headers: ["Content-Type": "application/json; charset=utf-8",
                                      "Set-Cookie": "clawdline=; Path=/; Max-Age=0; HttpOnly; SameSite=Strict"],
                            body: Data("{\"ok\":true}".utf8))

        case ("GET", "/v1/health"):
            return .json([
                "ok": true,
                "version": Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "?",
                "protocol": Self.protocolVersion,
                // The client uses these to decide what to draw at all. Saying "you may not" once
                // is kinder than a button that fails when pressed.
                "write": Config.shared.remoteWrite,
                "auth": RemoteAuth.isConfigured,
                "authed": { if case .allowed = permission(for: request) { return true }; return false }(),
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

        case ("GET", "/v1/projects"):
            // The icon registry is the closest thing to a list of "projects I work on" that
            // already exists on this machine, and the stack panel reads it for the same reason.
            // It is what the "start a session in…" menu is built from.
            return .json(["projects": ProjectIcon.knownPaths().sorted().map { path -> [String: Any] in
                var row: [String: Any] = ["path": path,
                                          "label": (path as NSString).lastPathComponent]
                if let registry = ProjectIcon.row(forCwd: path) {
                    if let label = registry["label"] as? String { row["label"] = label }
                    if let grid = ProjectIcon.grid(for: registry) { row["icon"] = json(of: grid) }
                }
                return row
            }])

        // Starting a session is the one write that is not aimed at an existing one — and it is
        // the same risk, because what it starts runs `bash` too. Same gate, deliberately.
        case ("POST", "/v1/sessions"):
            return writing(request) { body in
                let cwd = (body["cwd"] as? String) ?? ""
                let command = (body["command"] as? String) ?? "claude"
                guard !cwd.isEmpty else {
                    return .error(400, "bad_request", "That needs a cwd.")
                }
                var isDirectory: ObjCBool = false
                guard FileManager.default.fileExists(atPath: cwd, isDirectory: &isDirectory),
                      isDirectory.boolValue else {
                    return .error(400, "bad_request", "There is no directory there.")
                }
                guard let made = Targets.create(cwd: cwd, command: command) else {
                    return .error(500, "internal", "Could not open a tab.")
                }
                RemoteAuth.audit("session.create", ["cwd": cwd, "command": command, "id": made.id])
                // Read it back on the next beat, so whatever asked sees the new row arrive the
                // same way every other client does rather than through a special case.
                DispatchQueue.main.async { SessionWatch.shared.nudge() }
                return .json(["ok": true, "id": made.id, "backend": made.backend.rawValue])
            }

        case ("POST", let path) where path.hasSuffix("/send") && path.hasPrefix("/v1/sessions/"):
            let id = String(path.dropFirst("/v1/sessions/".count).dropLast("/send".count))
            return writing(request) { body in
                guard let session = self.session(withID: id.removingPercentEncoding ?? id) else {
                    return .error(404, "not_found", "No session named that")
                }
                guard let text = body["text"] as? String, !text.isEmpty else {
                    return .error(400, "bad_request", "That needs some text.")
                }
                // Off the main thread on purpose: this is an osascript round trip of a hundred
                // milliseconds or so, and the only thing it touches is a terminal.
                let problem = Targets.send(text, to: session)
                // Written down before the answer goes back, because the interesting case for the
                // log is the one where something went wrong afterwards.
                RemoteAuth.audit("session.send", ["id": session.id, "tty": session.tty,
                                                  "chars": "\(text.count)",
                                                  "ok": problem == nil ? "1" : "0"])
                if let problem { return .error(502, "internal", problem) }
                return .json(["ok": true, "at": Int(Date().timeIntervalSince1970)])
            }

        case ("POST", let path) where path.hasSuffix("/focus") && path.hasPrefix("/v1/sessions/"):
            let id = String(path.dropFirst("/v1/sessions/".count).dropLast("/focus".count))
            return writing(request) { _ in
                guard let session = self.session(withID: id.removingPercentEncoding ?? id) else {
                    return .error(404, "not_found", "No session named that")
                }
                DispatchQueue.main.async { Targets.reveal(session) }
                RemoteAuth.audit("session.focus", ["id": session.id])
                return .json(["ok": true])
            }

        case ("POST", let path) where path.hasPrefix("/v1/sessions/"):
            return .error(404, "not_found", "No such route")

        case ("GET", "/"), ("GET", "/index.html"):
            // `/?t=<token>` signs the browser in and bounces to `/`.
            //
            // The page cannot be handed a token any other way: there is nowhere sensible for a
            // person to type one, and `EventSource` cannot carry a header even if they did. So
            // the Mac's "Open in a browser" button and a QR code both point here, the cookie is
            // set on the way through, and the redirect takes the token back out of the address
            // bar before anybody can copy it into a chat window. A fragment would keep it off the
            // wire entirely and the page handles that too — but a fragment is invisible to the
            // server, so it cannot work on the very first load, and half the QR readers in the
            // world drop one.
            if let token = request.query["t"], !token.isEmpty,
               case .allowed = RemoteAuth.verify(bearer: token) {
                var response = signedIn(token, secure: request.headers["x-forwarded-proto"] == "https")
                response.status = 303
                response.headers["Location"] = "/"
                response.body = Data()
                return response
            }
            return page()

        case ("GET", "/manifest.webmanifest"):
            return manifest()

        default:
            return .error(404, "not_found", "No such route")
        }
    }

    // MARK: - Who is asking

    /// The bearer token for a request, from either place a client can put one.
    ///
    /// The header is the right way and the only way a script would do it. The cookie exists
    /// because of one specific limitation: **the browser's `EventSource` cannot set headers**, at
    /// all, and the event stream is the whole reason the web interface feels live. So a paired
    /// browser gets an `HttpOnly` cookie and the stream works; everything else prefers the header.
    private func bearer(_ request: Request) -> String? {
        if let header = request.headers["authorization"], header.count > 7,
           header.lowercased().hasPrefix("bearer ") {
            return String(header.dropFirst(7)).trimmingCharacters(in: .whitespaces)
        }
        for pair in (request.headers["cookie"] ?? "").split(separator: ";") {
            let kv = pair.split(separator: "=", maxSplits: 1)
            guard kv.count == 2, kv[0].trimmingCharacters(in: .whitespaces) == "clawdline" else { continue }
            return String(kv[1]).trimmingCharacters(in: .whitespaces)
        }
        return nil
    }

    private func permission(for request: Request) -> RemoteAuth.Verdict {
        RemoteAuth.verify(bearer: bearer(request))
    }

    /// The two refusals that come before authentication, because they are about a browser being
    /// made to ask on somebody else's behalf.
    ///
    /// **`Host`, and this is the one that matters.** A page on `evil.com` can already `fetch`
    /// `http://127.0.0.1:7717/…`; what normally saves a local server is that the page cannot
    /// *read* the reply, because the origins differ. **DNS rebinding removes that**: the attacker
    /// lets `evil.com` resolve to their own address long enough for the page to load, then
    /// re-answers with `127.0.0.1`, and now the browser believes the local server *is*
    /// `evil.com` — same origin, no protection left. The one thing that does not change through
    /// all of it is the `Host` header, which still says `evil.com`. So a request whose `Host` is
    /// not a name this server actually answers to is refused before it is looked at, and the
    /// whole attack is over. This costs nothing and it is not optional.
    ///
    /// **`Sec-Fetch-Site`** is the modern browser saying, unforgeably, that the page asking is on
    /// a different site. Absent for anything that is not a browser, so a script is unaffected.
    func crossOriginRefusal(_ request: Request) -> Response? {
        if Self.isCrossSiteSubresource(request.headers) {
            return .error(403, "forbidden", "Cross-site requests are not answered.")
        }
        guard let host = request.headers["host"], Self.isAllowedHost(host,
                                                                     port: Config.shared.remotePort,
                                                                     hostname: Config.shared.remoteHostname)
        else {
            return .error(403, "forbidden", "Wrong host.")
        }
        return nil
    }

    /// A cross-site request that is **not** somebody following a link.
    ///
    /// The distinction cost a bug and is worth the words. `Sec-Fetch-Site: cross-site` covers two
    /// completely different things: a page's script reaching for this server behind the user's
    /// back, and the user typing the address into a bar that happened to be on another page —
    /// Chrome calls a navigation out of `chrome://newtab` cross-site, so the first version of
    /// this refused to open at all when you typed the URL in.
    ///
    /// What separates them is not the site, it is the *mode*: a top-level navigation says
    /// `navigate` / `document`, and a script's fetch cannot claim either — the browser sets both
    /// headers and a page cannot forge them. So a navigation is let through and everything else
    /// cross-site is refused, which is the shape the attack actually has.
    ///
    /// Absent headers mean it is not a browser, and a script is left alone: it has to bring a
    /// token like everything else, and that is the check that matters for it.
    static func isCrossSiteSubresource(_ headers: [String: String]) -> Bool {
        guard headers["sec-fetch-site"] == "cross-site" else { return false }
        let navigating = headers["sec-fetch-mode"] == "navigate"
            && headers["sec-fetch-dest"] == "document"
        return !navigating
    }

    /// Pure, so the rebinding case can be tested without a socket.
    ///
    /// A quick tunnel's name is generated per run and cannot be in anybody's config, so the whole
    /// suffix is allowed. That is safe for the attack this defends against: rebinding needs the
    /// attacker to control the DNS answer, and `trycloudflare.com` answers are Cloudflare's.
    static func isAllowedHost(_ header: String, port: Int, hostname: String) -> Bool {
        var host = header.trimmingCharacters(in: .whitespaces).lowercased()
        if host.hasPrefix("[") {                       // [::1]:7717
            guard let close = host.firstIndex(of: "]") else { return false }
            host = String(host[host.index(after: host.startIndex)..<close])
        } else if let colon = host.lastIndex(of: ":") {
            host = String(host[host.startIndex..<colon])
        }
        if ["127.0.0.1", "localhost", "::1"].contains(host) { return true }
        let configured = hostname.trimmingCharacters(in: .whitespaces).lowercased()
        if !configured.isEmpty, host == configured { return true }
        return host.hasSuffix(".trycloudflare.com")
    }

    /// How many pairing requests have been started lately.
    ///
    /// This route is reachable without a token — it has to be — and it **puts a modal alert on
    /// somebody's screen**. Left alone that is a way to make a Mac unusable from a shell script,
    /// so: one pairing open at a time, three in ten minutes, and then nothing until it lapses.
    private var pairingTimes: [Date] = []
    private func pairingAllowed() -> Bool {
        let now = Date()
        pairingTimes = pairingTimes.filter { now.timeIntervalSince($0) < 600 }
        guard pairingTimes.count < 3 else { return false }
        pairingTimes.append(now)
        return true
    }

    /// Same host as the page was served from. Anything else is a different site asking on the
    /// user's behalf, which is exactly what the check exists to refuse.
    private func isOurs(_ origin: String) -> Bool {
        guard let url = URL(string: origin), let host = url.host else { return false }
        if host == "127.0.0.1" || host == "localhost" { return true }
        let configured = Config.shared.remoteHostname.trimmingCharacters(in: .whitespaces)
        if !configured.isEmpty, host == configured { return true }
        // A quick tunnel's hostname is generated per run, so it cannot be in the config. It is
        // always under this one domain, though, and that is a narrow enough thing to allow.
        return host.hasSuffix(".trycloudflare.com")
    }

    // MARK: - Pairing

    private func beginPairing(_ request: Request) -> Response {
        guard pairingAllowed() else {
            return .error(429, "rate_limited", "Too many pairing attempts. Try again in a few minutes.")
        }
        let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
        let entry = RemoteAuth.beginPairing(name: body["name"] as? String ?? "A browser")
        // The code is not in this response, and that is the entire security property: the person
        // who can finish this is the person who can see the Mac's screen.
        return .json(["pairing_id": entry.id, "expires": Int(entry.expires.timeIntervalSince1970)])
    }

    private func confirmPairing(_ request: Request) -> Response {
        let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
        guard let id = body["pairing_id"] as? String, let code = body["code"] as? String else {
            return .error(400, "bad_request", "That needs a pairing_id and a code.")
        }
        switch RemoteAuth.confirmPairing(id: id, code: code) {
        case .paired(let token):
            return signedIn(token, secure: request.headers["x-forwarded-proto"] == "https")
        case .wrongCode(let left):
            return .error(403, "forbidden", "That code is not right. \(left) tries left.")
        case .expired:
            return .error(403, "forbidden", "That pairing has expired. Start again.")
        }
    }

    private func exchangePassword(_ request: Request) -> Response {
        let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
        guard let password = body["password"] as? String else {
            return .error(400, "bad_request", "That needs a password.")
        }
        let name = body["name"] as? String ?? "A browser"
        guard let token = RemoteAuth.exchange(password: password, deviceName: name) else {
            return .error(401, "unauthorized", "That is not the password.")
        }
        return signedIn(token, secure: request.headers["x-forwarded-proto"] == "https")
    }

    /// The token goes back twice: in the body for a script that will keep it, and in an
    /// `HttpOnly` cookie for the page, because the event stream cannot carry a header.
    ///
    /// `Secure` only when the request arrived over HTTPS — which through a tunnel it did, and
    /// locally it did not. Setting it unconditionally would mean the cookie is silently dropped
    /// on `http://127.0.0.1` and nothing would work at the desk.
    private func signedIn(_ token: String, secure: Bool) -> Response {
        var cookie = "clawdline=\(token); Path=/; Max-Age=31536000; HttpOnly; SameSite=Strict"
        if secure { cookie += "; Secure" }
        let body = (try? JSONSerialization.data(withJSONObject: ["ok": true, "token": token],
                                                options: [.withoutEscapingSlashes])) ?? Data()
        return Response(status: 200,
                        headers: ["Content-Type": "application/json; charset=utf-8",
                                  "Set-Cookie": cookie],
                        body: body)
    }

    // MARK: - Writing

    /// Everything a mutating route has to be true before it happens, in one place.
    ///
    /// Three separate gates, and they are separate on purpose: the switch is a decision the owner
    /// of the Mac made, the capability is a decision about *this* device, and the idempotency key
    /// is about the network. A route that forgot one of them would be a route that quietly did
    /// something the other two were meant to prevent.
    private func writing(_ request: Request,
                         _ body: ([String: Any]) -> Response) -> Response {
        guard Config.shared.remoteWrite else {
            return .error(403, "write_disabled",
                          "Sending is switched off. Settings → Remote turns it on, and it is off "
                          + "by default because typing into a session runs code on this Mac.")
        }
        guard case .allowed(let device, let caps) = permission(for: request), caps.contains(.send) else {
            return .error(403, "forbidden", "This device may read, and not send.")
        }
        // **A retried POST must not be a second prompt.** Phones change networks mid-request and
        // clients retry; typing the same instruction into somebody's agent twice is not something
        // that can be taken back, so the key is required rather than merely honoured.
        guard let key = request.headers["idempotency-key"], !key.isEmpty else {
            return .error(400, "bad_request", "That needs an Idempotency-Key header.")
        }
        if let seen = idempotent[key], Date().timeIntervalSince(seen.at) < 600 {
            return seen.response
        }
        let parsed = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
        let response = body(parsed)
        idempotent = idempotent.filter { Date().timeIntervalSince($0.value.at) < 600 }
        idempotent[key] = (Date(), response)
        Log.write("remote: \(request.method) \(request.path) by \(device) → \(response.status)")
        return response
    }

    private var idempotent: [String: (at: Date, response: Response)] = [:]

    // MARK: - What the routes answer with

    private func sessionsPayload() -> [String: Any] {
        let watch = SessionWatch.shared
        return [
            "sessions": watch.targets.map { json(of: $0) },
            "at": Int(Date().timeIntervalSince1970),
        ]
    }

    /// The reading lives on the main thread and this runs on the server's queue, so the crossing
    /// is here and nowhere else — one hop for a dictionary lookup, rather than a copy of the
    /// session list kept in two places and drifting.
    private func session(withID id: String) -> TargetSession? {
        if Thread.isMainThread { return SessionWatch.shared.targets.first { $0.id == id } }
        return DispatchQueue.main.sync { SessionWatch.shared.targets.first { $0.id == id } }
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
            "write": Config.shared.remoteWrite,
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
            case 303: return "See Other"
            case 400: return "Bad Request"
            case 401: return "Unauthorized"
            case 403: return "Forbidden"
            case 404: return "Not Found"
            case 429: return "Too Many Requests"
            case 431: return "Request Header Fields Too Large"
            default:  return "Error"
            }
        }
    }
}
