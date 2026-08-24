import AppKit
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

    static let buildStamp: Int = {
        // Read once. It cannot change while this process is running — a rebuild replaces
        // the binary and relaunches, so the next answer comes from the next process.
        guard let url = Bundle.main.executableURL,
              let at = (try? FileManager.default.attributesOfItem(atPath: url.path))?[.modificationDate] as? Date
        else { return 0 }
        return Int(at.timeIntervalSince1970)
    }()

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
            // **Refuse a body that is too big; never trim one to fit.**
            //
            // This used to be `min(contentLength, 1 << 20)`, which is not a limit — it is a pair
            // of scissors. A larger request was cut to a megabyte and handed on, so what reached
            // the route was the first megabyte of a JSON document, `JSONSerialization` returned
            // nil, the parsed body became `[:]`, and `/send` answered **"That needs some text or
            // an image."** about a message that had both. Which is the worst kind of wrong
            // answer: it describes the request rather than the limit, so the person retries with
            // the same picture and gets the same sentence.
            //
            // It also disagreed with the page by a factor of twenty. `Shots` in index.html sizes
            // its own limits against a comment saying the server refuses a body over 20MB — so a
            // phone photograph was shrunk to something the client believed was comfortable and
            // then silently beheaded here. A 1600px screenshot kept as PNG, which is what the
            // attach button produces for anything text-shaped, clears a megabyte on its own.
            //
            // So: the number the page already assumes, and a 413 that says which limit was hit.
            if request.contentLength > Self.bodyLimit {
                self.send(.error(413, "too_large",
                                 "That was \(request.contentLength) bytes and the limit is "
                                 + "\(Self.bodyLimit). Send fewer or smaller pictures."), on: conn)
                return
            }
            let want = request.contentLength
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

    /// The largest request body this will assemble in memory.
    ///
    /// Twenty megabytes because that is what the page was already written against, and because
    /// the thing on the other end of the number is a photograph: `Shots` shrinks to a 1600px long
    /// edge and allows six of them, which lands well inside this and nowhere near a megabyte.
    /// It listens on loopback, but "on loopback" is not a reason to let anything on the machine
    /// hand it a gigabyte — the cap is about memory, and the refusal above is about honesty.
    static let bodyLimit = 20 << 20

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
    /// Everything that leaves here, with a cache policy applied at the door.
    ///
    /// **A response with no `Cache-Control` is not uncached — it is cached by guesswork.** With no
    /// header a browser applies heuristic freshness, and Safari on a home-screen web app is
    /// particularly willing to keep a 200 indefinitely. That produced the worst possible pairing:
    /// the page held an interface from an hour ago while `/v1/health` reported a newer build, so
    /// it correctly told its reader they were out of date and then served them the same stale copy
    /// every time they reloaded. **A warning nobody can act on is worse than no warning**, and the
    /// check could not even see itself, because the health answer was cacheable too.
    ///
    /// So: `no-store` unless a route asked for something else. The only routes that do are the
    /// drawn icons and splashes, which are the same picture for everybody and are worth a day.
    func route(_ request: Request) -> Response {
        var response = dispatch(request)
        if response.headers["Cache-Control"] == nil {
            response.headers["Cache-Control"] = "no-store"
        }
        return response
    }

    private func dispatch(_ request: Request) -> Response {
        // Before anything else, and before authentication, because these two refusals are about
        // *who is allowed to be asking at all* rather than about who they are.
        if let refusal = crossOriginRefusal(request) { return refusal }

        // The public shell reachable without a token, and each path is here for a reason rather
        // than for convenience: you cannot log in through a page whose artwork was refused, and
        // you cannot pair with a machine you cannot ask. None of these responses contains a
        // session, repository, path or credential.
        // `/v1/strings` is the newest of them and belongs here for the same reason the page does:
        // the door has to be able to ask for a token *in the reader's own language*, and it cannot
        // ask in a language it has not been told yet. What comes back is interface copy — the same
        // set of sentences for everybody, naming no session, no repository and no path, and
        // already sitting in a public repository — so there is nothing in it to withhold.
        let open: Set<String> = ["/", "/index.html", "/manifest.webmanifest", "/hero-orchestration.webp",
                                 "/v1/health", "/v1/strings"]
        let pairing = request.path.hasPrefix("/v1/auth/")
        // This page's own stylesheets and modules, for the same reason as the page itself: a door
        // nobody can draw is not a door. They are the same files for everybody, they name no
        // session, repository or path, and they have been in a public repository all along — the
        // only thing that changed is that they now sit beside `index.html` instead of inside it.
        let shell = request.path.hasPrefix("/app/")
        // The icon too, and it has to be: a browser asks for `/favicon.ico` on its own, before
        // and independently of the page, and an install prompt fetches the manifest's icons the
        // same way. It discloses nothing — it is the same drawing of the same creature for
        // everybody, and it is in a public repository.
        // The service worker with them: a browser fetches it outside the page's own credentials in
        // some flows, and what it contains is a push handler and nothing else.
        let icon = request.path == "/sw.js"
            || (request.path.hasPrefix("/splash-") && request.path.hasSuffix(".png"))
            || request.path == "/favicon.ico"
            || (request.path.hasPrefix("/icon-") && request.path.hasSuffix(".png"))
        // The orchestrator speaks with a credential of its own — a 0600 file only a local
        // process running as the user can read — because through a tunnel every request arrives
        // from 127.0.0.1, and a paired phone must never be able to start sessions. Reads without
        // that token fall through to ordinary device auth, so the page can show the tasks; the
        // complete route is gated inside its handler by the per-task secret instead, which is
        // the only credential a child was ever handed.
        let orchestrated = request.path.hasPrefix("/v1/orchestrator/")
        let orchestratorAuthed = orchestrated
            && Orchestrator.verifyDispatch(token: request.headers["x-clawdline-orchestrator"])
        let taskSecretRoute = orchestrated && request.method == "POST"
            && request.path.hasSuffix("/complete")
        if !open.contains(request.path), !pairing, !shell, !icon, !orchestratorAuthed, !taskSecretRoute {
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
            // Clearing the cookie is not signing out. The token it held is still a key, and a
            // browser that once had it may still have it written down — so the device goes too,
            // and "sign out" means what somebody handing a laptop back would expect it to mean.
            if case .allowed(let device, _) = permission(for: request) {
                RemoteAuth.revoke(id: device)
            }
            return Response(status: 200,
                            headers: ["Content-Type": "application/json; charset=utf-8",
                                      "Set-Cookie": "clawdline=; Path=/; Max-Age=0; HttpOnly; SameSite=Strict"],
                            body: Data("{\"ok\":true}".utf8))

        case ("GET", "/v1/health"):
            return .json([
                "ok": true,
                "version": Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "?",
                // **Which build, not which release.** `version` comes from the bundle and
                // `build.sh` writes the same string into every build of a release, so a page that
                // watched it could never tell it had fallen behind — which is exactly what
                // happened: a phone held an interface an hour old while the Mac had been rebuilt
                // twice, and the check meant to notice had nothing to compare.
                //
                // The executable's own modification time, because it is the one thing that cannot
                // be forgotten. A build number in `build.sh` is a number somebody has to remember
                // to bump, and the failure mode of forgetting is silence.
                "build": Self.buildStamp,
                "protocol": Self.protocolVersion,
                // The client uses these to decide what to draw at all. Saying "you may not" once
                // is kinder than a button that fails when pressed.
                "write": Config.shared.remoteWrite,
                "auth": RemoteAuth.isConfigured,
                // So a page can decide whether to offer the password path at all, rather than
                // offering it blind and letting somebody learn from a 401 that it was never set.
                "password": RemoteAuth.hasPassword,
                "authed": { if case .allowed = permission(for: request) { return true }; return false }(),
            ])

        case ("GET", "/v1/strings"):
            return strings(for: request)

        case ("GET", "/v1/sessions"):
            return .json(sessionsPayload())

        // Everything about this project that has an address.
        //
        // **A route rather than a field on the session.** The session list goes out on the event
        // stream every time anything moves, and working these out costs a `git` invocation and a
        // handful of file reads per project. Opening a menu is rare and paying for it then is
        // free; paying for it on every beat of the stream is a subprocess per session per second.
        case ("GET", let path) where path.hasSuffix("/links") && path.hasPrefix("/v1/sessions/"):
            let id = String(path.dropFirst("/v1/sessions/".count).dropLast("/links".count))
            guard let session = self.session(withID: id.removingPercentEncoding ?? id),
                  let cwd = Targets.workingDirectory(of: session) else {
                return .error(404, "not_found", "No session named that")
            }
            return .json(["links": linksPayload(cwd: cwd)])

        // The facts behind this session's compact status line and expanded card: what it has
        // spent, what is left of the plan's window, how much has changed on disk, whether the
        // last deploy went out. Never put on the stream, for the reason `/links` gives — this
        // reads a transcript that can be fifty megabytes on top of the `git`; the page caches it.
        case ("GET", let path) where path.hasSuffix("/info") && path.hasPrefix("/v1/sessions/"):
            let id = String(path.dropFirst("/v1/sessions/".count).dropLast("/info".count))
            guard let session = self.session(withID: id.removingPercentEncoding ?? id) else {
                return .error(404, "not_found", "No session named that")
            }
            return .json(["info": infoPayload(for: session)])

        // The skills this particular assistant session can invoke. Metadata only: neither a local
        // path nor the body of a SKILL.md belongs on a paired phone, and reading a menu must never
        // execute the dynamic commands a skill may contain.
        case ("GET", let path) where path.hasSuffix("/skills") && path.hasPrefix("/v1/sessions/"):
            let id = String(path.dropFirst("/v1/sessions/".count).dropLast("/skills".count))
            guard let session = self.session(withID: id.removingPercentEncoding ?? id) else {
                return .error(404, "not_found", "No session named that")
            }
            let skills: [AssistantSkill]
            switch session.assistant {
            case .claude:
                guard let cwd = Targets.workingDirectory(of: session) else {
                    return .error(404, "not_found", "Could not find that session's working directory")
                }
                skills = ClaudeSkills.available(cwd: cwd)
            case .codex:
                guard let record = Transcript.record(of: session), record.assistant == .codex else {
                    return .json(["skills": []])
                }
                skills = CodexSkills.available(in: record.url)
            case nil:
                skills = []
            }
            return .json(["skills": skills.map { skill in
                ["name": skill.command, "description": skill.description,
                 "source": skill.source.rawValue]
            }])

        // Git is another on-demand project reading, for the same reason as `/links` above: a
        // status and two diffs are cheap when this panel is opened and three subprocesses per
        // session per event-stream beat are not. Every invocation is read-only and lock-free.
        case ("GET", let path) where path.hasSuffix("/git") && path.hasPrefix("/v1/sessions/"):
            let id = String(path.dropFirst("/v1/sessions/".count).dropLast("/git".count))
            guard let session = self.session(withID: id.removingPercentEncoding ?? id),
                  let cwd = Targets.workingDirectory(of: session) else {
                return .error(404, "not_found", "No session named that")
            }
            switch GitChanges.read(cwd: cwd) {
            case .snapshot(let snapshot):
                return .json(GitChanges.payload(snapshot))
            case .notRepository:
                return .error(404, "not_a_repo", "That session is not inside a Git repository")
            case .failed:
                return .error(500, "git_failed", "Could not read that repository")
            }

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
            // One background agent's own conversation. The session list already says an agent
            // exists and what it last reached for; this is the rest of it, and it is read the
            // same way the session's transcript is because it is the same kind of file.
            if parts.count == 3, parts[1] == "agents" {
                let limit = min(max(Int(request.query["limit"] ?? "") ?? 200, 1), 1000)
                let agent = parts[2].removingPercentEncoding ?? parts[2]
                return agentPayload(for: session, agent: agent, limit: limit)
            }
            return .error(404, "not_found", "No such route")

        // Subscribing is read-level, deliberately. It does not go through `writing` — that gate is
        // about typing into somebody's session, and asking to be told when one needs an answer is
        // the opposite of that: it is the reading half arriving by a different road.
        case ("GET", "/v1/push/key"):
            return .json(["key": WebPush.publicKey])

        case ("POST", "/v1/push/subscribe"):
            guard case .allowed(let device, _) = permission(for: request) else {
                return .error(401, "unauthorized", "This needs a paired device.")
            }
            let json = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
            // Validated rather than stored as given. An endpoint is a URL **this Mac will POST to
            // from inside your network**, every time a session changes — so an unchecked one is a
            // request-forgery primitive, handed over by whoever holds a token.
            guard let subscription = WebPush.subscription(from: json, device: device) else {
                return .error(400, "bad_request", "That is not a usable push subscription.")
            }
            WebPush.add(subscription)
            RemoteAuth.audit("push.subscribe", ["device": device, "id": subscription.id])
            return .json(["ok": true, "id": subscription.id])

        case ("POST", "/v1/push/test"):
            // Read-level, like subscribing: this reaches nobody but the person who asked, and it
            // is the only way to answer "did that work" without waiting for a session to need
            // you — which is a long way to go to find out whether a key was minted correctly.
            guard case .allowed(let device, _) = permission(for: request) else {
                return .error(401, "unauthorized", "This needs a paired device.")
            }
            let mine = WebPush.subscriptions.filter { $0.device == device }
            guard !mine.isEmpty else {
                return .error(409, "not_subscribed",
                              "This device has not asked for notifications yet.")
            }
            WebPush.send(title: "Clawdline", body: L.t.pushTest, url: "/", tag: "test",
                         device: device)
            RemoteAuth.audit("push.test", ["device": device])
            return .json(["ok": true, "sent": mine.count])

        case ("POST", "/v1/push/unsubscribe"):
            let json = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
            guard let id = json["id"] as? String else {
                return .error(400, "bad_request", "That needs an id.")
            }
            WebPush.remove(id: id)
            return .json(["ok": true])

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

        // Reading which directories a session could be started in is read-level: it discloses the
        // same kind of thing `/v1/projects` does, which is a repository name. **Starting one is
        // not**, and it is the route below.
        //
        // There was a `POST /v1/sessions` here once that took a `cwd` and a `command` out of the
        // request body and ran the second in the first. It is gone. Behind a tunnel that is a
        // remote "run anything anywhere" primitive with a token in front of it, and no amount of
        // checking the path makes it something else — the check is a thing the next person to
        // edit this file can weaken by accident, and an absent parameter is not.
        case ("GET", "/v1/places"):
            return .json(placesPayload())

        // `/start` opens Claude Code; `/start/codex` opens Codex. **Which assistant is a path
        // segment and not a field**, and that is the whole of why it looks like this: the body on
        // this route is still not read at all, so there remains nowhere on it a directory or a
        // command could be written. The segment is resolved by exact match against a two-case
        // enum and anything else is a 404 — it names a choice, it does not carry one.
        case ("POST", let path) where path.hasPrefix("/v1/places/")
            && (path.hasSuffix("/start") || path.contains("/start/")):
            let rest = String(path.dropFirst("/v1/places/".count))
            let parts = rest.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
            guard parts.count == 2 || parts.count == 3, parts[1] == "start" else {
                return .error(404, "not_found", "No such route")
            }
            let id = parts[0]
            let named = parts.count == 3 ? parts[2] : Assistant.claude.rawValue
            guard let assistant = Assistant(rawValue: named) else {
                return .error(404, "not_found", "No assistant named that")
            }
            return writing(request) { _ in
                guard let place = StartPoints.place(withID: id.removingPercentEncoding ?? id) else {
                    // Written down as well, and this is the one worth having: an id nobody was
                    // ever handed is what somebody guessing looks like, and a log that only
                    // records what worked cannot show you that.
                    RemoteAuth.audit("place.start", ["place": String(id.prefix(64)), "ok": "0",
                                                     "why": "not_found"])
                    return .error(404, "not_found", "No place named that")
                }
                switch StartPoints.start(place, assistant: assistant) {
                case .refused(let status, let code, let message, let app):
                    RemoteAuth.audit("place.start", ["place": place.id, "cwd": place.path,
                                                     "assistant": assistant.rawValue,
                                                     "ok": "0", "why": code])
                    return .error(status, code, message, extra: app.map { ["app": $0] } ?? [:])
                case .started(let made, let backend):
                    RemoteAuth.audit("place.start", ["place": place.id, "cwd": place.path,
                                                     "assistant": assistant.rawValue,
                                                     "ok": "1", "id": made])
                    // Read it back on the next beat, so whatever asked sees the new row arrive
                    // the same way every other client does rather than through a special case.
                    DispatchQueue.main.async { SessionWatch.shared.nudge() }
                    return .json(["ok": true, "id": made, "backend": backend.rawValue,
                                  "assistant": assistant.rawValue,
                                  "place": place.id, "cwd": place.path,
                                  "at": Int(Date().timeIntervalSince1970)])
                }
            }

        case ("POST", let path) where path.hasPrefix("/v1/places/"):
            return .error(404, "not_found", "No such route")

        // Root sessions dispatching child sessions. See Sources/Orchestrator.swift and
        // docs/orchestrator.md; who may call what is decided above, where `orchestratorAuthed`
        // is computed.

        case ("POST", "/v1/orchestrator/tasks"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden", "Dispatching needs the orchestrator token.")
            }
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
            guard let taskID = body["task_id"] as? String,
                  let secret = body["secret"] as? String else {
                return .error(400, "bad_request", "task_id and secret are required.")
            }
            return answer(Orchestrator.dispatch(taskID: taskID, secret: secret))

        case ("GET", "/v1/orchestrator/tasks"):
            return .json(["tasks": Orchestrator.records(),
                          "at": Int(Date().timeIntervalSince1970)])

        case ("POST", let path) where path.hasPrefix("/v1/orchestrator/tasks/")
            && path.hasSuffix("/complete"):
            let id = String(path.dropFirst("/v1/orchestrator/tasks/".count)
                .dropLast("/complete".count))
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
            let secret = request.headers["x-clawdline-task-secret"]
                ?? (body["secret"] as? String) ?? ""
            return answer(Orchestrator.complete(taskID: id.removingPercentEncoding ?? id,
                                                secret: secret,
                                                status: body["status"] as? String ?? "",
                                                summary: body["summary"] as? String ?? ""))

        case ("POST", let path) where path.hasPrefix("/v1/orchestrator/tasks/")
            && path.hasSuffix("/cancel"):
            let id = String(path.dropFirst("/v1/orchestrator/tasks/".count)
                .dropLast("/cancel".count))
            let cleaned = id.removingPercentEncoding ?? id
            // The local credential may cancel outright; a paired device goes through the same
            // three gates every other write does.
            if orchestratorAuthed { return answer(Orchestrator.cancel(taskID: cleaned)) }
            return writing(request) { _ in answer(Orchestrator.cancel(taskID: cleaned)) }

        case ("GET", let path) where path.hasPrefix("/v1/orchestrator/tasks/"):
            let id = String(path.dropFirst("/v1/orchestrator/tasks/".count))
            guard let record = Orchestrator.record(id: id.removingPercentEncoding ?? id) else {
                return .error(404, "not_found", "No task named that")
            }
            return .json(["task": record])

        // Answering a menu, which is a different act from typing — see `Targets.answer`.
        //
        // Write-level and allowlisted twice over: nothing but `1`–`9` and `Tab` reaches a tty, and
        // the allowlist that matters is the one in `Targets`, not this parse. A route that took
        // "any key" would be a way to write escape sequences into somebody's terminal from a
        // phone, and no amount of validating the *question* would make that not true.
        // Ending a session, which is the only route here that destroys something.
        //
        // Write-level, idempotency-keyed like every other write, and **`send` rather than a
        // capability of its own**: a device that may type into a session can already type
        // `/exit` and then `exit`, so this is not new power — it is the same power with the two
        // steps joined and named. What it does add is that the second step lands on a tab that
        // has left the list, which is why doing it by hand from a phone was impossible.
        //
        // A session that will not leave on the word is signalled rather than closed out from
        // under — see `Targets.end`. That is not more power than this route already had either:
        // closing the tab hangs up its tty, which is the same process ending with less notice.
        // What it stops being is a modal dialog on the Mac that nobody on a phone can answer.
        case ("POST", let path) where path.hasSuffix("/end") && path.hasPrefix("/v1/sessions/"):
            let id = String(path.dropFirst("/v1/sessions/".count).dropLast("/end".count))
            return writing(request) { _ in
                guard let session = self.session(withID: id.removingPercentEncoding ?? id) else {
                    return .error(404, "not_found", "No session named that")
                }
                RemoteAuth.audit("session.end", ["id": session.id])
                // **The children first, while the root is still there to be recognised.** A task
                // is matched to its root by the session id in that session's hook note, and the
                // note is found through the tty of the tab this line is about to close — after
                // `end` there is nothing left to match, and the children would run on as orphans.
                // Nothing happens here for a session that dispatched nothing, which is most of
                // them; the cascade and its reasoning live in `Orchestrator`.
                Orchestrator.cancelChildren(ofRoot: session)
                if let failure = Targets.end(session) {
                    return .error(502, "internal", failure)
                }
                SessionWatch.shared.nudge()
                return .json(["ok": true])
            }

        case ("POST", let path) where path.hasSuffix("/key") && path.hasPrefix("/v1/sessions/"):
            let id = String(path.dropFirst("/v1/sessions/".count).dropLast("/key".count))
            return writing(request) { body in
                // **Parsed before the session is looked up.** Not for secrecy — a well-formed key
                // still tells you whether a session exists — but because the allowlist is the
                // thing this route is for, and a check that runs after two other steps is a check
                // somebody will later move.
                let key = (body["key"] as? String) ?? ""
                let byte: UInt8
                if key == "tab" {
                    byte = 0x09
                } else if key.count == 1, let c = key.unicodeScalars.first,
                          ("1"..."9").contains(key) {
                    byte = UInt8(c.value)
                } else {
                    return .error(400, "bad_request",
                                  "key must be \"1\"…\"9\" or \"tab\".")
                }
                guard let session = self.session(withID: id.removingPercentEncoding ?? id) else {
                    return .error(404, "not_found", "No session named that")
                }
                if let failure = Targets.answer(byte, to: session) {
                    return .error(502, "internal", failure)
                }
                RemoteAuth.audit("session.key", ["id": session.id, "key": key])
                // A reading now would still show the menu — the terminal has not repainted yet.
                SessionWatch.shared.nudge()
                return .json(["ok": true])
            }

        case ("POST", let path) where path.hasSuffix("/send") && path.hasPrefix("/v1/sessions/"):
            let id = String(path.dropFirst("/v1/sessions/".count).dropLast("/send".count))
            return writing(request) { body in
                guard let session = self.session(withID: id.removingPercentEncoding ?? id) else {
                    return .error(404, "not_found", "No session named that")
                }
                let text = (body["text"] as? String) ?? ""
                let images = (body["images"] as? [String]) ?? []
                guard !text.isEmpty || !images.isEmpty else {
                    return .error(400, "bad_request", "That needs some text or an image.")
                }

                // **Refuse rather than answer the wrong question.**
                //
                // Claude Code's picker discards a bracketed paste and then acts on the Return that
                // follows it — so sending words to a session showing a menu does not type them, it
                // confirms whichever row is highlighted. Measured: with the caret on the third
                // option, sending the word "Tea" answered "Water". Silently. From a phone.
                //
                // Only asked when the cached state already says `waiting`, because the answer
                // costs a screen capture and every other send is the ordinary case.
                if SessionWatch.shared.states[session.id] == .waiting,
                   Targets.isChoosing(session) {
                    return .error(409, "showing_a_menu",
                                  "That session is showing a menu. Sending text would confirm "
                                  + "whichever option is highlighted rather than typing. "
                                  + "Answer it with POST /v1/sessions/<id>/key.")
                }

                // Off the main thread on purpose: this is an osascript round trip of a hundred
                // milliseconds or so, and the only thing it touches is a terminal.
                var problem: String?
                if images.isEmpty {
                    problem = Targets.send(text, to: session)
                } else {
                    // Written to the bounded drop cache and handed over as files, because that is
                    // the shape the existing path already takes. Claude consumes the bytes while
                    // `Drop` borrows the pasteboard; Codex receives the path and opens it only
                    // after this request is over. Keeping the successful handoff is therefore
                    // part of the protocol, not leftover temporary state.
                    let made = Self.pieces(text: text, images: images)
                    guard made.pieces.contains(where: {
                        if case .image = $0 { return true }; return false
                    }) else {
                        return .error(400, "bad_request", "None of those were images I could read.")
                    }
                    problem = Targets.send(made.pieces, to: session)
                    // A failed terminal handoff has no future reader. Successful paths stay in
                    // Drop's bounded cache so Codex can open them when it reaches the prompt.
                    Self.finishUploads(made.stored, sent: problem == nil)
                }
                // Written down before the answer goes back, because the interesting case for the
                // log is the one where something went wrong afterwards.
                RemoteAuth.audit("session.send", ["id": session.id, "tty": session.tty,
                                                  "chars": "\(text.count)",
                                                  "images": "\(images.count)",
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

        case ("GET", "/hero-orchestration.webp"):
            guard let url = Bundle.main.url(forResource: "hero-orchestration", withExtension: "webp",
                                            subdirectory: "web"),
                  let data = try? Data(contentsOf: url) else {
                return .error(404, "not_found", "The hero artwork is not in this build")
            }
            return Response(status: 200,
                            headers: ["Content-Type": "image/webp",
                                      "Cache-Control": "public, max-age=86400"],
                            body: data)

        case ("GET", let path) where path.hasPrefix("/app/"):
            return asset(String(path.dropFirst("/app/".count)))

        case ("GET", "/sw.js"):
            return serviceWorker()

        case ("GET", "/favicon.ico"):
            guard let data = RemoteIcon.ico() else { return .error(404, "not_found", "No icon") }
            return Response(status: 200,
                            headers: ["Content-Type": "image/x-icon",
                                      "Cache-Control": "public, max-age=86400"],
                            body: data)

        case ("GET", let path) where path.hasPrefix("/splash-") && path.hasSuffix(".png"):
            // `/splash-1179x2556.png` — the pixel size of one particular iPhone. iOS names the
            // device in a media query and asks for the image that fits it, so the sizes are not a
            // list this end can know in advance.
            let body = path.dropFirst("/splash-".count).dropLast(".png".count).split(separator: "x")
            guard body.count == 2, let w = Int(body[0]), let h = Int(body[1]),
                  let data = RemoteIcon.splash(width: w, height: h) else {
                return .error(404, "not_found", "No splash that size")
            }
            return Response(status: 200,
                            headers: ["Content-Type": "image/png",
                                      "Cache-Control": "public, max-age=86400"],
                            body: data)

        case ("GET", let path) where path.hasPrefix("/icon-") && path.hasSuffix(".png"):
            let want = Int(path.dropFirst("/icon-".count).dropLast(".png".count)) ?? 0
            // A short list rather than any number somebody asks for: each one is a bitmap kept in
            // memory for the life of the app, and an open-ended size is an open-ended cache.
            guard [32, 64, 180, 192, 512].contains(want), let data = RemoteIcon.png(size: want) else {
                return .error(404, "not_found", "No icon that size")
            }
            return Response(status: 200,
                            headers: ["Content-Type": "image/png",
                                      "Cache-Control": "public, max-age=86400"],
                            body: data)

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
        // Two different things, and until now they were the same code with different English in
        // them — so a client could only tell them apart by reading the sentence, which is the one
        // part of an error nobody should ever branch on. `left` is in the body for the same
        // reason: a page that wants to say "two tries left" should not be counting for itself.
        case .wrongCode(let left):
            return .error(403, "wrong_code", "That code is not right. \(left) tries left.",
                          extra: ["tries_left": left])
        case .expired:
            return .error(403, "expired", "That pairing has expired. Start again.")
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

    /// An orchestrator reply, in the envelope everything else already uses.
    private func answer(_ reply: Orchestrator.Reply) -> Response {
        switch reply {
        case .ok(let obj):
            return .json(obj)
        case .refused(let status, let code, let message, let extra):
            return .error(status, code, message, extra: extra)
        }
    }

    /// Turn a message with pictures in it into the pieces the sender already understands.
    ///
    /// Each image arrives as a `data:` URL and leaves as a file, because that is what
    /// ``Drop/Piece`` is: the pasteboard wants a file it can read, and the path is also the
    /// fallback if the bytes turn out not to be an image after all.
    ///
    /// **Everything is re-encoded to PNG rather than trusted**, and there are two reasons, one
    /// practical and one not. The practical one: a photograph taken on an iPhone is HEIC, and a
    /// terminal程式 asked to read a HEIC gets a file it does not want. The other: these bytes
    /// arrived over a network from something that said they were an image, and the cheapest way
    /// to be sure of that is to decode them and write them out again — what does not survive
    /// being drawn was not a picture.
    ///
    /// The claimed media type is ignored entirely for the same reason. It is a string somebody
    /// sent us.
    static func pieces(text: String, images: [String]) -> (pieces: [Drop.Piece], stored: [String]) {
        var pieces: [Drop.Piece] = []
        var stored: [String] = []
        if !text.isEmpty { pieces.append(.text(text)) }

        for source in images {
            guard let raw = decodeDataURL(source),
                  let rep = NSBitmapImageRep(data: raw),
                  let png = rep.representation(using: .png, properties: [:]) else { continue }
            guard let path = Drop.store(png, as: "png") else { continue }
            stored.append(path)
            pieces.append(.image(path))
        }
        return (pieces, stored)
    }

    /// Remove uploads only when their path never reached a terminal. A successful send is kept
    /// by ``Drop/prune(keeping:)``; in particular, returning HTTP 200 does not mean Codex has
    /// read the file yet.
    static func finishUploads(_ paths: [String], sent: Bool) {
        guard !sent else { return }
        for path in paths { try? FileManager.default.removeItem(atPath: path) }
    }

    /// `data:image/heic;base64,AAAA…` → the bytes. Anything else, including a `data:` URL that is
    /// not base64, comes back nil: this is not a general URL loader and must never become one —
    /// a `file:` or an `http:` here would be somebody making this app fetch things for them.
    static func decodeDataURL(_ text: String) -> Data? {
        guard text.hasPrefix("data:"), let comma = text.firstIndex(of: ",") else { return nil }
        let header = text[text.startIndex..<comma]
        guard header.contains(";base64") else { return nil }
        let body = String(text[text.index(after: comma)...])
        return Data(base64Encoded: body, options: [.ignoreUnknownCharacters])
    }

    // MARK: - What the routes answer with

    /// The directories a session may be started in — see ``StartPoints``.
    ///
    /// `id` is the only part a client sends back, and it is the only part it may send: the
    /// starting route takes an id and no path. `path` is here so a person can see which of two
    /// projects with the same name they are pointing at, not so anything can be built out of it.
    private func placesPayload() -> [String: Any] {
        [
            "at": Int(Date().timeIntervalSince1970),
            // What may be started, from this end rather than from a list baked into the page.
            // Whether Codex is on this Mac is something only this side can answer, and a button
            // for an assistant that is not installed opens a tab saying "command not found".
            "assistants": Assistant.available.map { ["id": $0.rawValue, "label": $0.label] },
            "places": StartPoints.places().map { place -> [String: Any] in
                var row: [String: Any] = [
                    "id": place.id,
                    "label": place.label,
                    "path": place.path,
                    "at": Int(place.at.timeIntervalSince1970),
                ]
                // The same mark the session list draws, and drawn the same way — the registry's
                // when it has one, and a stable creature off the path when it does not.
                if let grid = ProjectIcon.grid(forCwd: place.path) { row["icon"] = json(of: grid) }
                return row
            },
        ]
    }

    /// The addresses a project has, in the order somebody would want them.
    ///
    /// Nothing here is invented: every one of these is a URL some other tool already put in a
    /// file this app reads — the health endpoint from the icon registry, the run from the deploy
    /// status, the servers from the project's own `status` command, the backlog page from
    /// whatever produced it. **Clawdline's contribution is that they are in one list on a phone**,
    /// rather than four places on a Mac that is in another room.
    ///
    /// `kind` is for the client to pick an icon with. `local` says the address only resolves on
    /// the Mac's own network, which is worth knowing before tapping it from a train.
    private func linksPayload(cwd: String) -> [[String: Any]] {
        var out: [[String: Any]] = []
        let registry = ProjectIcon.row(forCwd: cwd)
        let status = ProjectStatus.read(cwd: cwd, remote: Project.info(cwd: cwd)?.remote,
                                        registry: registry?["health"] as? [String: Any])

        if let health = status.health, let url = health.url, !url.isEmpty {
            out.append(["label": health.label, "url": url, "kind": "site",
                        "state": health.state, "local": false])
        }
        if let deploy = status.deploy, let url = deploy.url, !url.isEmpty {
            out.append(["label": deploy.label, "url": url, "kind": "deploy",
                        "state": deploy.state, "local": false])
        }
        // Only a project that declared a stack and was trusted to run its own status command.
        // Neither is guessed: an untrusted one is silent rather than probed.
        let stack = DevStack.find(fromCwd: cwd).flatMap { spec in
            DevStack.isTrusted(spec) ? DevStack.read(spec) : nil
        }
        for process in stack?.processes ?? [] {
            let url = process.url ?? process.port.map { "http://127.0.0.1:\($0)" }
            guard let url, !url.isEmpty else { continue }
            // **Why, not just that.** Only processes with an address appear in a list of
            // addresses, and the one that actually broke usually has neither — a front-end build
            // listens on nothing. So a phone saw `web · down` and had no way to reach the reason,
            // which was sitting two entries away in a process it was never shown. `why` carries
            // the process's own last words, or the stack's root cause named, so the row that is
            // visible can answer for the one that is not.
            let why = process.isUp ? nil : stack?.why(process)
            out.append(["label": process.name, "url": url, "kind": "server",
                        "state": process.isUp ? "ok" : "down", "local": true,
                        "why": why ?? ""])
        }
        if let backlog = status.backlog, let file = backlog.artifact, !file.isEmpty {
            // A path, not a URL. The page cannot open it and says so rather than offering a link
            // that does nothing — see the client. It is here because on the Mac it is the one
            // thing in this list somebody actually wants to open.
            out.append(["label": "backlog", "url": "file://" + file, "kind": "artifact",
                        "state": "", "local": true])
        }
        return out
    }

    /// The card behind `GET /v1/sessions/:id/info`. Everything in it is gathered here and shaped
    /// in ``SessionInfo/payload(id:assistant:sessionId:model:cwd:startedAt:now:usage:limits:files:deploy:)``,
    /// which is the half a test can reach.
    ///
    /// The token totals are the orchestrator's own readers, because a child's transcript and a
    /// session's transcript are the same kind of file and one summing of it is enough. The model
    /// comes from the same pass as the limits rather than from the totals: the last assistant
    /// turn of a session that just hit its window is `<synthetic>`, and that is not a model.
    private func infoPayload(for session: TargetSession) -> [String: Any] {
        let cwd = Targets.workingDirectory(of: session)
        let record = Transcript.record(of: session)

        var usage: Orchestrator.Usage?
        var limits = SessionInfo.Limits()
        var model: String?
        if let record, let data = try? Data(contentsOf: record.url), !data.isEmpty {
            switch record.assistant {
            case .claude:
                usage = Orchestrator.claudeUsage(transcript: record.url)
                let read = SessionInfo.claudeLimits(transcript: data)
                limits = read.limits
                model = read.model
            case .codex:
                usage = Orchestrator.codexUsage(rollout: record.url)
                limits = SessionInfo.codexLimits(rollout: data)
                model = usage?.model
            }
        }
        if model == nil, let named = usage?.model, !named.hasPrefix("<") { model = named }
        // The percentages Claude Code never writes into a transcript, as the status line wrote
        // them down. Laid under whatever the transcript did say — see `SessionInfo.merged`.
        if session.assistant == .claude {
            limits = SessionInfo.merged(
                transcript: limits,
                cache: SessionInfo.claudeLimits(cacheDirectory: ProjectStatus.cacheDirectory))
        }

        // Session info is now the one home for every project address. Keep the smaller `deploy`
        // field in the payload for older web clients, while current clients receive the exact
        // same full list the compatibility `/links` route exposes.
        let links = cwd.map { linksPayload(cwd: $0) } ?? []
        let deploy = links.filter { row in
            let kind = row["kind"] as? String
            return kind == "deploy" || kind == "ci"
        }

        var payload = SessionInfo.payload(
            id: session.id, assistant: session.assistant,
            sessionId: HookBridge.note(for: session)?.session, model: model,
            cwd: cwd, startedAt: Targets.processStart(of: session),
            usage: usage, limits: limits, files: cwd.flatMap { SessionInfo.files(cwd: $0) },
            deploy: deploy, models: SessionInfo.models(for: session.assistant))
        payload["links"] = links
        return payload
    }

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
        let menu = watch.menu(of: session.id)
        var out: [String: Any] = [
            "id": session.id,
            "backend": session.backend.rawValue,
            "tty": session.tty.replacingOccurrences(of: "/dev/", with: ""),
            "label": session.displayLabel,
            // Kept next to `assistant`, and it means what it always did. A page built against
            // the old field still draws a Claude Code session correctly; what it does with a
            // Codex one is show it as an ordinary terminal, which is wrong but not broken —
            // and the alternative was every existing client losing its session list at once.
            "isClaude": session.isClaude,
            "state": name(of: state),
        ]
        if let assistant = session.assistant { out["assistant"] = assistant.rawValue }
        if case .working(let line) = state { out["line"] = line }
        if state == .waiting, let menu {
            // The page's transcript revision predates structured menus and watches `line`, not
            // `menu`. Waiting rows never display `line`, so this stable value is only a revision:
            // when the transcript-backed picker arrives after the waiting notification, the page
            // refetches immediately instead of waiting until the answer changes the state.
            out["line"] = menuRevision(menu)
        }
        // The question itself, so a phone can answer it instead of being told to go and find a
        // Mac. Only ever present on a waiting session, and absent when the menu could not be
        // read — which the page has to handle anyway, because that is the old behaviour and it
        // is still what happens when a dialog is drawn in a shape this does not recognise.
        if let menu { out["menu"] = json(of: menu) }
        let agents = watch.agents(of: session.id)
        if !agents.isEmpty { out["agents"] = agents.map { json(of: $0) } }
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

    /// A menu as rows a finger can hit.
    ///
    /// **`n` is the keystroke and not the position**, which is the only part of this worth being
    /// careful about: the page sends that number straight to `/key`, and renumbering the rows
    /// here to make them tidy would produce buttons that answer a different question than the one
    /// they are labelled with. `can` is false for a row no keystroke reaches — it is drawn, and it
    /// is not offered, because a button that cannot work is worse than a line of text.
    private func json(of menu: SessionState.Menu) -> [String: Any] {
        var out: [String: Any] = [
            "options": menu.options.map { option -> [String: Any] in
                ["n": option.number, "label": option.label,
                 "selected": option.selected, "can": option.answerable]
            },
        ]
        if let selected = menu.selected { out["selected"] = selected }
        if let question = menu.question { out["question"] = question }
        return out
    }

    /// Stable content for the legacy session revision field. Separators prevent different
    /// question/option boundaries from collapsing to the same string.
    private func menuRevision(_ menu: SessionState.Menu) -> String {
        ([menu.question ?? ""] + menu.options.map {
            "\($0.number)\u{1f}\($0.label)\u{1f}\($0.selected ? 1 : 0)"
        }).joined(separator: "\u{1e}")
    }

    /// One background agent.
    ///
    /// `at` goes over as an instant rather than an age: the page already knows how to draw a
    /// clock from one, and an age computed here would be wrong by however long the beat took to
    /// arrive — which on a phone over a tunnel is the interesting case rather than the rare one.
    private func json(of agent: Subagents.Agent) -> [String: Any] {
        var out: [String: Any] = [
            "id": agent.id,
            "what": agent.description,
            "type": agent.type,
            "state": agent.state.rawValue,
            "depth": agent.depth,
            "at": Int(agent.at.timeIntervalSince1970),
        ]
        if let doing = agent.doing { out["doing"] = doing }
        if let result = agent.result { out["result"] = result }
        if let tokens = agent.tokens { out["tokens"] = tokens }
        if let tools = agent.tools { out["tools"] = tools }
        if let seconds = agent.seconds { out["seconds"] = seconds }
        if let model = agent.model { out["model"] = model }
        return out
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
        guard let record = Transcript.record(of: session) else {
            // Not an error. A session that has not spoken yet has an empty transcript, and that
            // is a different thing from a session that could not be found.
            return .json(["entries": [], "signature": ""])
        }
        let file = record.url
        // The signature must never describe bytes newer than the text beside it. A transcript
        // can be appended between the read and a later `stat`; returning that later signature
        // with the earlier tail makes the browser believe the missing final entry is already on
        // screen, so every subsequent fetch with the same signature is correctly ignored.
        //
        // Take the signature first. If the file moves during the read, repeat once from the
        // newer boundary. A second append can only make the signature lag the text, which costs
        // one harmless refetch; it cannot make an absent entry look current forever.
        var signature = Transcript.signature(of: file)
        guard var text = Transcript.tail(of: file, bytes: 8 << 20) else {
            return .json(["entries": [], "signature": ""])
        }
        let after = Transcript.signature(of: file)
        if after != signature, let fresh = Transcript.tail(of: file, bytes: 8 << 20) {
            signature = after
            text = fresh
        }
        let entries = rows(of: Transcript.parse(text, assistant: record.assistant, limit: limit))
        return .json(["entries": entries, "signature": signature])
    }

    /// One agent's conversation, plus the agent itself so the page has something to put in the
    /// header while it is reading it.
    ///
    /// Claude Code only, and there is nothing to check for that: a Codex session has no
    /// `subagents` directory, so the lookup comes back empty and this is a 404 — the same answer
    /// it gives for an id that was never one of this session's.
    private func agentPayload(for session: TargetSession, agent id: String, limit: Int) -> Response {
        guard let file = Subagents.transcript(of: session, agent: id) else {
            return .error(404, "not_found", "No agent named that")
        }
        var out: [String: Any] = ["entries": [], "signature": ""]
        // The strip's own row for it, so a reader who followed a link sees the same description,
        // state and cost that the row they clicked was showing.
        if let agent = SessionWatch.shared.agents(of: session.id).first(where: { $0.id == id }) {
            out["agent"] = json(of: agent)
        }
        var signature = Transcript.signature(of: file)
        guard var text = Transcript.tail(of: file, bytes: 8 << 20) else { return .json(out) }
        let after = Transcript.signature(of: file)
        if after != signature, let fresh = Transcript.tail(of: file, bytes: 8 << 20) {
            signature = after
            text = fresh
        }
        // `sidechains: true` — every row in an agent's file is marked as one, because from the
        // session's point of view that is what an agent is. See `Transcript.parse`.
        out["entries"] = rows(of: Transcript.parse(text, assistant: .claude, limit: limit,
                                                   sidechains: true))
        out["signature"] = signature
        return .json(out)
    }

    /// Entries as the page reads them. One shape for both transcripts, because a reader following
    /// an agent should meet the same pane they left.
    private func rows(of entries: [Transcript.Entry]) -> [[String: Any]] {
        entries.map { entry -> [String: Any] in
            var row: [String: Any] = ["role": name(of: entry.kind), "text": entry.text]
            if let tool = entry.tool { row["tool"] = tool }
            if let time = entry.time { row["at"] = Int(time.timeIntervalSince1970) }
            return row
        }
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

    /// The web interface's own words, in whatever language the browser reads.
    ///
    /// **The page translates nothing.** It ships with the English as a fallback so that a fetch
    /// that fails leaves a readable screen rather than a screen of blank labels, and it asks for
    /// this before its first render; everything it says afterwards comes from here. One set of
    /// words in one place is the whole reason ``Copy`` exists, and a second set living in the
    /// HTML would be a second thing to translate and the first thing to forget.
    ///
    /// Which language is ``L/copy(forAcceptLanguage:)``'s answer: the browser's `Accept-Language`
    /// sorted by `q`, unless somebody has named a language in the config, which wins. The person
    /// holding the phone is not necessarily the person the Mac belongs to.
    ///
    /// A flat object of strings, keyed by the name of the ``Copy`` member it came from, so that a
    /// string can be followed from the page to this file to the translations with one search.
    /// Some of them are not new: a session that is waiting for you says the same thing here as it
    /// does in the bar, and the reused ones are named at the bottom.
    private func strings(for request: Request) -> Response {
        let t = L.copy(forAcceptLanguage: request.headers["accept-language"])
        let tag = L.tag(of: t)
        var out: [String: Any] = [
            // For `<html lang>`, which is what a screen reader picks a voice from and what a
            // browser offers to translate a page against.
            "lang": tag,
            // And `<html dir>`. Every language this app speaks is written left to right, so this
            // is `ltr` today whatever the header said — it is sent, and derived rather than
            // assumed, so that the day one that is not gets added the page is already asking.
            "dir": L.direction(of: tag),
        ]
        func add(_ pairs: [String: String]) {
            for (key, value) in pairs { out[key] = value }
        }

        // The connection chip.
        add([
            "webConnLive": t.webConnLive,
            "webConnConnecting": t.webConnConnecting,
            "webConnRetrying": t.webConnRetrying,
            "webConnOffline": t.webConnOffline,
            "webConnLocked": t.webConnLocked,
            "webConnTipLive": t.webConnTipLive,
            "webConnTipLocked": t.webConnTipLocked,
            "webConnTipDown": t.webConnTipDown,
        ])

        // The header's counts.
        add([
            "webCountWorking": t.webCountWorking,
            "webCountWaiting": t.webCountWaiting,
            "webCountUnreadable": t.webCountUnreadable,
            "webCountQuietOne": t.webCountQuietOne,
            "webCountQuietMany": t.webCountQuietMany,
            "webCountNone": t.webCountNone,
        ])

        // The list, its filter, and the four ways it can be empty.
        add([
            "webFilterPlaceholder": t.webFilterPlaceholder,
            "webFilterLabel": t.webFilterLabel,
            "webListLabel": t.webListLabel,
            "webPull": t.webPull,
            "webPullRelease": t.webPullRelease,
            "webPullBusy": t.webPullBusy,
            "webEmptyFilterTitle": t.webEmptyFilterTitle,
            "webEmptyFilterHint": t.webEmptyFilterHint,
            "webEmptyLockedTitle": t.webEmptyLockedTitle,
            "webEmptyLockedHint": t.webEmptyLockedHint,
            "webEmptyNoneHint": t.webEmptyNoneHint,
            "webEmptyWaitTitle": t.webEmptyWaitTitle,
            "webEmptyWaitHint": t.webEmptyWaitHint,
            "webStateUnreadable": t.webStateUnreadable,
            "webStateWorking": t.webStateWorking,
        ])

        // The transcript pane.
        add([
            "webBack": t.webBack,
            "webBackLabel": t.webBackLabel,
            "webNoSessionOpen": t.webNoSessionOpen,
            "webOrderTip": t.webOrderTip,
            "webShowOnMac": t.webShowOnMac,
            "webShowOnMacTip": t.webShowOnMacTip,
            "webShowOnMacOff": t.webShowOnMacOff,
            "webShowOnMacAsked": t.webShowOnMacAsked,
            "webSessionActions": t.webSessionActions,
            "webSessionGit": t.webSessionGit,
            "webGitTitle": t.webGitTitle,
            "webGitClean": t.webGitClean,
            "webGitNotRepo": t.webGitNotRepo,
            "webGitFailed": t.webGitFailed,
            "webGitRefresh": t.webGitRefresh,
            "webGitClose": t.webGitClose,
            "webGitStaged": t.webGitStaged,
            "webGitUnstaged": t.webGitUnstaged,
            "webGitUntracked": t.webGitUntracked,
            "webGitConflict": t.webGitConflict,
            "webEndSession": t.webEndSession,
            "webConfirmActionTitle": t.webConfirmActionTitle,
            "webConfirmActionSay": t.webConfirmActionSay,
            "webConfirmEndTitle": t.webConfirmEndTitle,
            "webConfirmEndSay": t.webConfirmEndSay,
            "webClosing": t.webClosing,
            "webCancel": t.webCancel,
            "webConfirm": t.webConfirm,
            "webPickSession": t.webPickSession,
            "webReading": t.webReading,
            "webLoading": t.webLoading,
            "webTranscriptFailed": t.webTranscriptFailed,
            "webWhoYou": t.webWhoYou,
            "webWhoTool": t.webWhoTool,
            "webPending": t.webPending,
            "webAttachedImage": t.webAttachedImage,
            "webAttachedImages": t.webAttachedImages,
            "webSteps": t.webSteps,
            "webJustNow": t.webJustNow,
            "webMinutesAgo": t.webMinutesAgo,
        ])

        // The composer, and what it refuses.
        add([
            "webSend": t.webSend,
            "webAttach": t.webAttach,
            "webRemoveShot": t.webRemoveShot,
            "webWriteOpen": t.webWriteOpen,
            "webWriteOff": t.webWriteOff,
            "webShotsOnlyPictures": t.webShotsOnlyPictures,
            "webShotsTooMany": t.webShotsTooMany,
            "webShotTooBig": t.webShotTooBig,
            "webShotsTooBig": t.webShotsTooBig,
            "webShotUnreadable": t.webShotUnreadable,
            "webShotNeedsSession": t.webShotNeedsSession,
        ])

        // Starting a session from the page — the sheet, and the wait between the tab opening and
        // the session turning up in the list. The last two carry `{app}`, which the page fills in
        // from the terminal's name in the error object rather than from anything it knows itself.
        add([
            "webStart": t.webStart,
            "webStartLabel": t.webStartLabel,
            "webStartPick": t.webStartPick,
            "webStartWith": t.webStartWith,
            "webStartEmpty": t.webStartEmpty,
            "webStartFilter": t.webStartFilter,
            "webStarting": t.webStarting,
            "webStartWaiting": t.webStartWaiting,
            "webStartSlow": t.webStartSlow,
            "webStartFailed": t.webStartFailed,
            "webStartGone": t.webStartGone,
            "webStartTerminalClosed": t.webStartTerminalClosed,
            "webStartTerminalUnsupported": t.webStartTerminalUnsupported,
            "webStartOff": t.webStartOff,
        ])

        // The key row along the bottom, on a desk.
        add([
            "webHintMove": t.webHintMove,
            "webHintOpen": t.webHintOpen,
            "webHintFilter": t.webHintFilter,
            "webHintPane": t.webHintPane,
        ])

        // The shortcuts card.
        add([
            "webKeysLabel": t.webKeysLabel,
            "webKeysTitle": t.webKeysTitle,
            "webKeysMove": t.webKeysMove,
            "webKeysOpen": t.webKeysOpen,
            "webKeysFilter": t.webKeysFilter,
            "webKeysEscape": t.webKeysEscape,
            "webKeysList": t.webKeysList,
            "webKeysPane": t.webKeysPane,
            "webKeysEnds": t.webKeysEnds,
            "webKeysReverse": t.webKeysReverse,
            "webKeysThis": t.webKeysThis,
            "webKeysFoot": t.webKeysFoot,
        ])

        // The door.
        add([
            "webDoorLabel": t.webDoorLabel,
            "webDoorAskLede": t.webDoorAskLede,
            "webDoorAskFine": t.webDoorAskFine,
            "webDoorName": t.webDoorName,
            "webDoorAsk": t.webDoorAsk,
            "webDoorToPassword": t.webDoorToPassword,
            "webDoorCodeLede": t.webDoorCodeLede,
            "webDoorCodeFine": t.webDoorCodeFine,
            "webDoorTwoMinutes": t.webDoorTwoMinutes,
            "webDoorDigit": t.webDoorDigit,
            "webDoorConfirm": t.webDoorConfirm,
            "webDoorRestart": t.webDoorRestart,
            "webDoorPasswordLede": t.webDoorPasswordLede,
            "webDoorPasswordFine": t.webDoorPasswordFine,
            "webDoorPassword": t.webDoorPassword,
            "webDoorPasswordGo": t.webDoorPasswordGo,
            "webDoorToPair": t.webDoorToPair,
            "webDoorAsking": t.webDoorAsking,
            "webDoorAskFailed": t.webDoorAskFailed,
            "webDoorRateLimited": t.webDoorRateLimited,
            "webDoorSixDigits": t.webDoorSixDigits,
            "webDoorChecking": t.webDoorChecking,
            "webDoorFinished": t.webDoorFinished,
            "webDoorWrongCode": t.webDoorWrongCode,
            "webDoorNeedPassword": t.webDoorNeedPassword,
            "webDoorWrongPassword": t.webDoorWrongPassword,
            "webDoorExpired": t.webDoorExpired,
            "webDoorPaired": t.webDoorPaired,
        ])

        // What a request that went wrong says.
        add([
            "webOffline": t.webOffline,
            "webNotJSON": t.webNotJSON,
            "webRequestFailed": t.webRequestFailed,
        ])

        // Notifications.
        add([
            "webNotifyGo": t.webNotifyGo,
            "webNotifyAsking": t.webNotifyAsking,
            "webNotifyStop": t.webNotifyStop,
            "webNotifyStopping": t.webNotifyStopping,
            "webNotifyOff": t.webNotifyOff,
            "webNotifyOn": t.webNotifyOn,
            "webNotifyBlocked": t.webNotifyBlocked,
            "webNotifyUnsupported": t.webNotifyUnsupported,
            "webNotifyHomeScreen": t.webNotifyHomeScreen,
            "webNotifyOnFailed": t.webNotifyOnFailed,
            "webNotifyOffFailed": t.webNotifyOffFailed,
        ])

        // The settings sheet, and the composer's in-flight state.
        add([
            "webSettings": t.webSettings,
            "webSettingsNotify": t.webSettingsNotify,
            "webSettingsAssistantIcons": t.webSettingsAssistantIcons,
            "webSettingsAssistantIconsSay": t.webSettingsAssistantIconsSay,
            "webSettingsAssistantIconsShow": t.webSettingsAssistantIconsShow,
            "webSettingsVersion": t.webSettingsVersion,
            "webClose": t.webClose,
            "webNotifySheetOff": t.webNotifySheetOff,
            "webNotifyTest": t.webNotifyTest,
            "webNotifyTestSent": t.webNotifyTestSent,
            "webNotifyTestNone": t.webNotifyTestNone,
            "webNotifyTestFailed": t.webNotifyTestFailed,
            "webSending": t.webSending,
            "webSendTip": t.webSendTip,
        ])
        // Said in both places, so said once. The bar and the page are two windows onto the same
        // sessions, and a row that reads "waiting for you" on a Mac should not read as something
        // else on the phone next to it.
        add([
            "placeholder": t.placeholder,
            "noSession": t.noSession,
            "noOutput": t.noOutput,
            "sessionWaiting": t.sessionWaiting,
            "sendFailed": t.sendFailed,
            "hintList": t.hintList,
            "hintKeys": t.hintKeys,
            "hintOrder": t.hintOrder,
            // A function on the Mac, where it can be called with the answer; two keys here,
            // because a question with two answers does not cross a JSON boundary as one.
            "webOrderNewest": t.outputOrder(newestFirst: true),
            "webOrderOldest": t.outputOrder(newestFirst: false),
        ])

        // A question with a menu on it, a page that has fallen behind the app.
        //
        // **These were translated into fourteen languages and then not sent for a day.** Nothing
        // broke — the page carries an English copy of everything as a fallback — which is exactly
        // why nobody noticed, and why `webWaitingSend`, a warning that sending from here confirms
        // the wrong option, was in English for everybody who does not read English. The test that
        // now walks every `web*` member on `Copy` and fails on anything missing here is the real
        // fix; this block is the part that was owed.
        add([
            "webAskLabel": t.webAskLabel,
            "webAskAny": t.webAskAny,
            "webWaitingTitle": t.webWaitingTitle,
            "webWaitingSay": t.webWaitingSay,
            "webWaitingSend": t.webWaitingSend,
            "webMenuSay": t.webMenuSay,
            "webMenuHighlighted": t.webMenuHighlighted,
            "webMenuSent": t.webMenuSent,
            "webStale": t.webStale,
            "webStaleGo": t.webStaleGo,
        ])

        // What a session has going in the background.
        add([
            "webAgents": t.webAgents,
            "webAgentsCount": t.webAgentsCount,
            "webAgentDone": t.webAgentDone,
            "webAgentFailed": t.webAgentFailed,
            "agentRunning": t.agentRunning,
            "agentTools": t.agentTools,
            "agentEmpty": t.agentEmpty,
            "agentBack": t.agentBack,
            "webAgentOpen": t.webAgentOpen,
        ])

        // The chips on a root session and on the child it sent off — see `Orchestrator`.
        add([
            "webTaskRoot": t.webTaskRoot,
            "webTaskChild": t.webTaskChild,
            "webTaskTasks": t.webTaskTasks,
            "webTaskDone": t.webTaskDone,
            "webTaskFailed": t.webTaskFailed,
            "webTaskRunning": t.webTaskRunning,
        ])

        // The Links sheet.
        add([
            "webLinks": t.webLinks,
            "webLinksTip": t.webLinksTip,
            "webLinksPick": t.webLinksPick,
            "webLinksEmpty": t.webLinksEmpty,
            "webLinksFailed": t.webLinksFailed,
            "webLinksLocal": t.webLinksLocal,
            "webLinksFile": t.webLinksFile,
            "webLinksCopy": t.webLinksCopy,
            "webLinksCopied": t.webLinksCopied,
            "webLinksCopyFailed": t.webLinksCopyFailed,
            "webLinkOk": t.webLinkOk,
            "webLinkFail": t.webLinkFail,
            "webLinkDown": t.webLinkDown,
            "webLinkRunning": t.webLinkRunning,
            "webSettingsOrder": t.webSettingsOrder,
            "webSettingsOrderSay": t.webSettingsOrderSay,
        ])

        // The Session info card.
        add([
            "webSessionInfo": t.webSessionInfo,
            "webInfoTitle": t.webInfoTitle,
            "webInfoSession": t.webInfoSession,
            "webInfoAssistant": t.webInfoAssistant,
            "webInfoModel": t.webInfoModel,
            "webInfoSessionId": t.webInfoSessionId,
            "webInfoDirectory": t.webInfoDirectory,
            "webInfoRunningFor": t.webInfoRunningFor,
            "webInfoUsage": t.webInfoUsage,
            "webInfoInput": t.webInfoInput,
            "webInfoOutput": t.webInfoOutput,
            "webInfoCacheRead": t.webInfoCacheRead,
            "webInfoCacheWrite": t.webInfoCacheWrite,
            "webInfoTotal": t.webInfoTotal,
            "webInfoCost": t.webInfoCost,
            "webInfoNoUsage": t.webInfoNoUsage,
            "webInfoLimits": t.webInfoLimits,
            "webInfoLimitHit": t.webInfoLimitHit,
            "webInfoResets": t.webInfoResets,
            "webInfoUnknown": t.webInfoUnknown,
            "webInfoFiles": t.webInfoFiles,
            "webInfoBranch": t.webInfoBranch,
            "webInfoStaged": t.webInfoStaged,
            "webInfoUnstaged": t.webInfoUnstaged,
            "webInfoUntracked": t.webInfoUntracked,
            "webInfoConflict": t.webInfoConflict,
            "webInfoClean": t.webInfoClean,
            "webInfoNotRepo": t.webInfoNotRepo,
            "webInfoDeploy": t.webInfoDeploy,
            "webInfoNoDeploy": t.webInfoNoDeploy,
            "webInfoFailed": t.webInfoFailed,
            "webInfoRefresh": t.webInfoRefresh,
            "webInfoTokens": t.webInfoTokens,
            "webInfoSwitchModel": t.webInfoSwitchModel,
            "webInfoModelOther": t.webInfoModelOther,
            "webInfoModelSent": t.webInfoModelSent,
            "webInfoModelBusy": t.webInfoModelBusy,
            "webInfoLimitsClaude": t.webInfoLimitsClaude,
            "webInfoCopied": t.webInfoCopied,
            "webInfoAsOf": t.webInfoAsOf,
            "webInfoWhyUnknown": t.webInfoWhyUnknown,
        ])

        var response = Response.json(out)
        // The answer depends on a request header, so a cache that keyed on the URL alone would
        // hand the next reader somebody else's language.
        response.headers["Vary"] = "Accept-Language"
        response.headers["Cache-Control"] = "no-store"
        return response
    }

    private func page() -> Response {
        guard let url = Bundle.main.url(forResource: "index", withExtension: "html",
                                        subdirectory: "web"),
              let data = try? Data(contentsOf: url) else {
            return .error(404, "not_found", "The web interface is not in this build")
        }
        return Response(status: 200, headers: ["Content-Type": "text/html; charset=utf-8"], body: data)
    }

    /// One file from under `Resources/web/app`, and only from under there.
    ///
    /// **`request.path` is never percent-decoded** — see `Request.init` — so what arrives here is
    /// the literal string the client put on the wire. That makes the safe rule a whitelist rather
    /// than a blacklist: there is no `%2e%2e%2f` to recognise, because `%` is not in the alphabet
    /// below. A segment may only be letters, digits, `.`, `-`, `_`; segments are separated by `/`;
    /// no empty segment, no `.` and no `..`; and the extension has to be one this app knows how to
    /// label. Everything else is a 404 before a path is ever built.
    private func asset(_ name: String) -> Response {
        let types = ["css": "text/css; charset=utf-8",
                     "js": "text/javascript; charset=utf-8"]
        // An explicit set of characters rather than `isLetter` / `isNumber`: those ask a question
        // about Unicode's categories, and the question here is "is this the name of a file we put
        // in the bundle ourselves". It also keeps this compiling on the older Swift in CI.
        let allowed = Set("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-_")

        let parts = name.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
        let plain = { (part: String) -> Bool in
            if part.isEmpty || part == "." || part == ".." { return false }
            return part.allSatisfy { allowed.contains($0) }
        }
        guard parts.count >= 1, parts.count <= 4, parts.allSatisfy(plain),
              let file = parts.last,
              let dot = file.lastIndex(of: "."),
              let type = types[String(file[file.index(after: dot)...])] else {
            return .error(404, "not_found", "No such file")
        }

        guard let root = Bundle.main.resourceURL?
                .appendingPathComponent("web", isDirectory: true)
                .appendingPathComponent("app", isDirectory: true) else {
            return .error(404, "not_found", "The web interface is not in this build")
        }
        var url = root
        for part in parts { url.appendPathComponent(part) }
        // Belt, and now braces. The whitelist above already makes this impossible; it is here so
        // that the day somebody widens the alphabet, the file that actually gets read is still
        // under the directory it was resolved from.
        guard url.standardizedFileURL.path.hasPrefix(root.standardizedFileURL.path + "/"),
              let data = try? Data(contentsOf: url) else {
            return .error(404, "not_found", "No such file")
        }
        return Response(status: 200, headers: ["Content-Type": type], body: data)
    }

    /// The service worker, which exists for one reason: **a page cannot receive a push while it
    /// is closed, and a service worker can.**
    ///
    /// Deliberately tiny, and served rather than shipped as a file, because a service worker is
    /// the one script a browser keeps and re-runs on its own — the smaller its surface, the less
    /// there is to be wrong in a copy somebody installed last month.
    private func serviceWorker() -> Response {
        let js = #"""
        // Clawdline's service worker. Its whole job is to be awake when the page is not.
        // **The one lever that can reach a page already stuck on an old copy.**
        //
        // For a long time no route set `Cache-Control`, and a browser with no header applies
        // heuristic freshness — Safari on a home-screen web app especially. Serving `no-store`
        // fixes it for every load after the fix, and does nothing for a device that already
        // holds the old copy: it never asks again, so it never learns. Reloading does not help,
        // because the reload is served from the same cache.
        //
        // A worker can break that, and it is the only thing that can. `sw.js` itself is sent
        // `no-cache`, so a browser revalidates it on its own schedule; when this file changes it
        // installs, `skipWaiting` stops it queuing behind open tabs, `clients.claim` takes those
        // tabs over, and from that moment the handler below fetches the page itself instead of
        // the cache. **One more reload after that and the device is out.**
        self.addEventListener("install", function () { self.skipWaiting(); });

        self.addEventListener("activate", function (event) {
            event.waitUntil(
                // Nothing here writes to Cache Storage, so normally there is nothing to delete.
                // It is done anyway, because "nothing wrote to it" is a claim about every version
                // of this file that has ever run on somebody's phone, and this is two lines.
                caches.keys()
                    .then(function (names) { return Promise.all(names.map(function (n) { return caches.delete(n); })); })
                    .catch(function () {})
                    .then(function () { return self.clients.claim(); })
            );
        });

        self.addEventListener("fetch", function (event) {
            // Only the page. Everything else on this origin is either an API answer, which is
            // `no-store` already, or a drawn icon, which is worth its day of cache.
            if (event.request.mode !== "navigate") { return; }
            event.respondWith(
                fetch(event.request.url, { cache: "reload", credentials: "include" })
                    // Offline: hand back whatever the browser would have done on its own, which
                    // is the stale copy. Stale and readable beats an error page.
                    .catch(function () { return fetch(event.request); })
            );
        });

        self.addEventListener("push", function (event) {
            var payload = {};
            try { payload = event.data ? event.data.json() : {}; } catch (e) {}
            event.waitUntil(self.registration.showNotification(payload.title || "Clawdline", {
                body: payload.body || "",
                // The tag collapses repeats about one session into a single line rather than a
                // stack: a phone that was in a pocket for ten minutes should find one notification
                // about a session, not six.
                tag: payload.tag || "clawdline",
                renotify: true,
                data: { url: payload.url || "/" }
            }));
        });

        self.addEventListener("notificationclick", function (event) {
            event.notification.close();
            var url = (event.notification.data && event.notification.data.url) || "/";
            // Focus a window that is already open before making another one — the point of
            // tapping this is to reach the session, not to collect tabs.
            //
            // **Three things had to be true for this to land on the session and only one of them
            // was.** `navigate()` throws on a client this worker does not control, which is every
            // client until the page has been reloaded once after the worker installed — and a
            // rejected promise here is silent, so it read as "focus worked, routing did not".
            // Second, a URL differing only in its fragment is a same-document navigation: even
            // when `navigate()` succeeds the page is not reloaded, so nothing re-reads it. Third,
            // the page only ever looked at the fragment on first load.
            //
            // So the message is the mechanism and the navigation is the fallback: an open page
            // routes itself, and a cold start gets the fragment the ordinary way.
            event.waitUntil(clients.matchAll({ type: "window", includeUncontrolled: true })
                .then(function (list) {
                    for (var i = 0; i < list.length; i++) {
                        var client = list[i];
                        if (!("focus" in client)) { continue; }
                        if (client.postMessage) {
                            client.postMessage({ type: "navigate", url: url });
                        }
                        return client.focus().then(function () {
                            // Only for a client we control, and only when the message could not
                            // have done it. `catch` because navigating an uncontrolled client
                            // rejects, and an unhandled rejection here would take the whole
                            // handler down with it.
                            if (!client.postMessage && client.navigate) {
                                return client.navigate(url).catch(function () {});
                            }
                        });
                    }
                    return clients.openWindow(url);
                }));
        });
        """#
        return Response(status: 200,
                        headers: ["Content-Type": "text/javascript; charset=utf-8",
                                  "Cache-Control": "no-cache"],
                        body: Data(js.utf8))
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
            // `maskable` as well as `any`, so a launcher that wants to crop this into its own
            // shape crops the dark tile rather than clipping the creature's ears off.
            "icons": [
                ["src": "/icon-192.png", "sizes": "192x192", "type": "image/png",
                 "purpose": "any maskable"],
                ["src": "/icon-512.png", "sizes": "512x512", "type": "image/png",
                 "purpose": "any maskable"],
            ],
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
        // **The same fields `/v1/health` sends, and that is a requirement rather than a
        // convenience.** The page identifies a build from `build|version|protocol` and compares
        // one reading against the next; if the two sources disagree about which fields exist, the
        // stamps differ by construction and every page decides it is out of date the moment the
        // stream connects. `build` was added to health alone, and the result was a "this page is
        // the older one" notice that reloading could not clear — because the fresh page computed
        // the same mismatch again.
        write(event: "hello", data: [
            "version": Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "?",
            "build": Self.buildStamp,
            "protocol": Self.protocolVersion,
            "write": Config.shared.remoteWrite,
        ], to: stream)
        DispatchQueue.main.async {
            let payload = self.sessionsPayload()
            // The task list rides the same stream: a page that reconnects is level on both
            // without asking, for the same reason the whole session list goes out above.
            let tasks: [String: Any] = ["tasks": Orchestrator.records(),
                                        "at": Int(Date().timeIntervalSince1970)]
            self.queue.async {
                self.write(event: "sessions", data: payload, to: stream)
                self.write(event: "orchestrator", data: tasks, to: stream)
            }
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

    /// Called by the orchestrator whenever any task record changes, from whichever thread it
    /// changed on — `records()` does its own main-thread crossing.
    func broadcastOrchestrator() {
        let payload: [String: Any] = ["tasks": Orchestrator.records(),
                                      "at": Int(Date().timeIntervalSince1970)]
        queue.async { [weak self] in
            guard let self, !self.streams.isEmpty else { return }
            for stream in self.streams.values {
                self.write(event: "orchestrator", data: payload, to: stream)
            }
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
        ///
        /// `extra` goes **inside** `error`, next to the code, and exists so that a page can write
        /// its own sentence instead of showing this one: `tries_left` so it can say "two tries
        /// left" without counting for itself, `app` so it can name the terminal it is complaining
        /// about in the reader's own language. `message` is English and stays English.
        static func error(_ status: Int, _ code: String, _ message: String,
                          extra: [String: Any] = [:]) -> Response {
            var error: [String: Any] = ["code": code, "message": message,
                                        "request_id": UUID().uuidString.lowercased()]
            for (key, value) in extra { error[key] = value }
            return .json(["error": error], status: status)
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
            case 409: return "Conflict"
            case 429: return "Too Many Requests"
            case 413: return "Payload Too Large"
            case 431: return "Request Header Fields Too Large"
            default:  return "Error"
            }
        }
    }
}
