import AppKit
import Foundation
import Network

/// Decide whether a local title can also be handed to the assistant without changing what its
/// current turn means. Claude accepts a slash command only at an idle prompt; Codex names thread
/// metadata out of band and therefore does not share that restriction.
enum SessionTitleSync {
    enum Action: Equatable {
        case localOnly
        case busy
        case unavailable
        case renameClaude
        case renameCodex(threadID: String)
    }

    static func action(assistant: Assistant?, state: SessionState, clearing: Bool,
                       codexThreadID: String?) -> Action {
        guard !clearing else { return .localOnly }
        switch assistant {
        case .claude:
            return state == .idle ? .renameClaude : .busy
        case .codex:
            guard let codexThreadID, !codexThreadID.isEmpty else { return .unavailable }
            return .renameCodex(threadID: codexThreadID)
        case nil:
            return .localOnly
        }
    }

    /// One look at the actual screen before anything is typed into it.
    ///
    /// ``action(assistant:state:clearing:codexThreadID:)`` decides from the session list's
    /// cached state, and that cache is refreshed every 1.2 seconds while the app is in front and
    /// **every twenty seconds while it is not** — which is the state the Mac is in whenever
    /// somebody is renaming a session from their phone. A cached `idle` can therefore be twenty
    /// seconds behind a Claude that is now showing a menu, and a slash command sent to a menu is
    /// not typed: the picker discards the bracketed paste and acts on the Return after it. The
    /// measurement is written up beside `/send`, which pays for the same capture — with the
    /// caret on the third option, sending the word "Tea" answered "Water".
    ///
    /// So the capture is paid here too, and only here: `busy`, `unavailable`, `local_only` and
    /// the Codex path never reach a keyboard, and a rename is the one operation on this server
    /// that is never urgent. `showingMenu` is a closure for that reason — an unread one is a
    /// terminal round trip not made.
    static func confirmed(_ action: Action, showingMenu: () -> Bool) -> Action {
        guard action == .renameClaude, showingMenu() else { return action }
        return .busy
    }
}

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
final class RemoteServer: @unchecked Sendable {

    static let shared = RemoteServer()
    private init() {}

    /// SessionWatch is main-queue-owned, but queue ownership and main-thread identity diverge
    /// after the test runner calls `dispatchMain()`. The app still drains this queue on its main
    /// thread; this helper preserves that production shape while asking the identity that the
    /// synchronous crossing actually depends on.
    private func onMain<T>(from site: String, _ work: () -> T) -> T {
        MainQueue.hop(from: site, alreadyOnMain: MainQueue.isCurrent, work)
    }

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

    private let queue = DispatchQueue(label: "com.tsunamiworks.clawdline.remote")
    private var listener: NWListener?
    private var streams: [ObjectIdentifier: Stream] = [:]
    private var nextEventID = 0
    private lazy var transcriptRevisionStream = TranscriptRevisionStream { [weak self] id, signature in
        self?.queue.async { [weak self] in
            guard let self else { return }
            let payload: [String: Any] = ["id": id, "signature": signature,
                                          "at": Int(Date().timeIntervalSince1970)]
            for stream in self.streams.values {
                self.write(event: "transcript", data: payload, to: stream)
            }
        }
    }
    /// Set only through `attachCloudBridge`. It lives on `queue`, beside the SSE streams whose
    /// already-serialized readings it shares.
    private var cloudBridge: CloudAppBridge?
    private var cloudPublishTail: Task<Void, Never>?
    private var cloudLifecycleTask: Task<Void, Never>?
    private var cloudLifecycleGeneration: UInt64 = 0
    /// Main-thread mirror used only to decide whether SessionWatch still needs its observer when
    /// the loopback listener is off.
    private var cloudAttached = false

    /// Pure route seam: tests supply complete process-bound observations without asking iTerm,
    /// tmux or assistant transcript registries. Production never sets it.
    static var coordinatorSessionsForTesting: [Coordinator.LiveSession]?
    static var coordinatorObservationEvidenceForTesting:
        (observedAt: Date?, generation: Int?, complete: Bool)?
    /// Failure injection for the SessionWatch observation boundary. The production path never
    /// sets it; route tests use it to prove landings remain readable when live observation is
    /// unavailable.
    static var coordinatorObservationUnavailableForTesting = false
    /// Route-level serializer seam. Tests still execute `sessionsPayload()` and `json(of:)`; they
    /// replace only SessionWatch's external terminal inventory with deterministic rows.
    static var sessionPayloadForTesting: ([TargetSession], [String: SessionState])?
    /// Supplies one complete process-bound identity while route tests still exercise the real
    /// serializers and gates. Production always resolves these fields from the watched process.
    static var sessionWorkIdentityForTesting:
        ((TargetSession) -> Orchestrator.SessionWorkIdentity)?
    /// Replaces only the process-bound conversation reader used by whoami. The route still takes
    /// a real coherent target snapshot and executes both exact-resolution passes.
    static var sessionConversationIDForTesting: ((TargetSession) -> String?)?
    /// Runs after the first identity pass, for a fixture that changes real registry/rollout
    /// bytes between observations. It changes timing only and never supplies an identity.
    static var sessionIdentityPassDidFinishForTesting: ((Int) -> Void)?
    /// Replaces only the final terminal handoff. Parsing, gates, lookup, reservation and response
    /// settlement still use the production path.
    static var terminalSendForTesting: ((String, TargetSession) -> String?)?
    /// Replaces the route body only after a non-send terminal mutation entered the production
    /// bounded worker. This keeps queue/isolation tests away from real ttys.
    static var terminalRouteForTesting: ((Request) -> Response)?
    /// Replaces only the final destructive handoff after every `/end` gate has run.
    static var sessionEndForTesting: ((TargetSession) -> String?)?
    /// Deterministic expiry seam for artifact route and relay lifecycle tests.
    static var imageArtifactNowForTesting: (() -> Date)?
    /// Already-serialized full snapshots for bridge lifecycle integration tests. Production keeps
    /// using the SessionWatch/Orchestrator serializers on the main thread.
    static var cloudSnapshotDataForTesting: (sessions: Data, orchestrator: Data)?

    /// Put non-HTTP orchestrator work behind the same serial gate as requests. Schedule timing
    /// is calculated off this queue; only the ordinary dispatch transaction enters here.
    func serialized(_ work: @escaping () -> Void) {
        queue.async(execute: work)
    }

    /// Terminal commands have one bounded, serial lane of their own. iTerm2 serializes Apple
    /// events itself; doing the same here keeps that queue visible and, crucially, keeps a modal
    /// sheet from occupying the HTTP/SSE queue.
    @discardableResult
    func enqueueTerminalCommand(channel: String? = nil, _ work: @escaping () -> Void) -> Bool {
        enqueueTerminalCommand(channels: channel.map { [$0] } ?? [], work)
    }

    /// Multi-recipient form used by durable coordination delivery. One HTTP operation may fan a
    /// release out to several waiter terminals; reserving every known recipient before enqueue
    /// makes the documented per-session depth true for those production paths too.
    @discardableResult
    func enqueueTerminalCommand(channels rawChannels: [String],
                                _ work: @escaping () -> Void) -> Bool {
        let channels = Array(Set(rawChannels.filter { !$0.isEmpty })).sorted()
        // Nested terminal work is already inside the single broker ordering domain. Execute it
        // inline so a root `/end` can finish children deepest-first without deadlocking behind
        // itself or reversing the safety order.
        if DispatchQueue.getSpecific(key: Self.terminalWorkerKey) == true {
            let added = channels.filter { !terminalActiveChannels.contains($0) }
            terminalAdmissionLock.lock()
            guard added.allSatisfy({ (terminalOutstandingByChannel[$0] ?? 0)
                                        < Self.terminalChannelDepth }) else {
                terminalAdmissionLock.unlock()
                return false
            }
            for channel in added {
                terminalOutstandingByChannel[channel, default: 0] += 1
            }
            terminalAdmissionLock.unlock()
            let previous = terminalActiveChannels
            terminalActiveChannels.formUnion(added)
            work()
            terminalActiveChannels = previous
            terminalAdmissionLock.lock()
            for channel in added {
                let remaining = (terminalOutstandingByChannel[channel] ?? 1) - 1
                if remaining == 0 { terminalOutstandingByChannel.removeValue(forKey: channel) }
                else { terminalOutstandingByChannel[channel] = remaining }
            }
            terminalAdmissionLock.unlock()
            return true
        }
        terminalAdmissionLock.lock()
        guard terminalMaintenanceRequestID == nil,
              terminalOutstanding < Self.terminalDepth,
              channels.allSatisfy({ (terminalOutstandingByChannel[$0] ?? 0)
                                      < Self.terminalChannelDepth }) else {
            terminalAdmissionLock.unlock()
            return false
        }
        terminalOutstanding += 1
        for channel in channels { terminalOutstandingByChannel[channel, default: 0] += 1 }
        terminalAdmissionLock.unlock()
        terminalQueue.async {
            let previous = self.terminalActiveChannels
            self.terminalActiveChannels.formUnion(channels)
            work()
            self.terminalActiveChannels = previous
            self.terminalAdmissionLock.lock()
            self.terminalOutstanding -= 1
            for channel in channels {
                let remaining = (self.terminalOutstandingByChannel[channel] ?? 1) - 1
                if remaining == 0 { self.terminalOutstandingByChannel.removeValue(forKey: channel) }
                else { self.terminalOutstandingByChannel[channel] = remaining }
            }
            self.terminalAdmissionLock.unlock()
        }
        return true
    }

    /// Test receipt for the production admission counters, read under the same lock that mutates
    /// them. A nested terminal cascade must finish with both totals back at zero.
    func terminalOutstandingForTesting() -> (total: Int, channels: Int) {
        terminalAdmissionLock.lock(); defer { terminalAdmissionLock.unlock() }
        return (terminalOutstanding, terminalOutstandingByChannel.values.reduce(0, +))
    }

    /// Enter a verified cloud command through the same route transaction local HTTP uses. The
    /// bridge supplies an idempotency key derived from the already replay-checked sender/sequence.
    func routeVerifiedCloudCommand(_ command: CloudHeadlessCommand, sender: String,
                                   idempotencyKey: String) async -> Response {
        await withCheckedContinuation { continuation in
            queue.async { [weak self] in
                guard let self else {
                    continuation.resume(returning: .error(503, "unavailable",
                                                          "The local command broker is unavailable."))
                    return
                }
                continuation.resume(returning: self.dispatch(Request(
                    verifiedCloud: command, sender: sender, idempotencyKey: idempotencyKey
                )))
            }
        }
    }

    /// Explicit Cloud lifecycle seam. Merely constructing a bridge changes nothing; attaching it
    /// starts its transport and installs the existing SessionWatch observer. Passing nil detaches
    /// and shuts it down without touching the local listener or its write setting.
    func attachCloudBridge(_ bridge: CloudAppBridge?) {
        queue.async { [weak self] in
            guard let self else { return }
            let previous = self.cloudBridge
            // The server queue is the one ordering domain for bridge identity, lifecycle,
            // observer intent and its main-thread mirror. It therefore cannot replay an older
            // off-main mirror after a newer main-thread detach (the former ABA race).
            DispatchQueue.main.async { [weak self] in
                self?.cloudAttached = bridge != nil
                self?.syncSnapshotObserver()
            }
            if previous === bridge { return }
            self.cloudLifecycleGeneration &+= 1
            let generation = self.cloudLifecycleGeneration
            let predecessorLifecycle = self.cloudLifecycleTask
            let predecessorPublication = self.cloudPublishTail
            predecessorLifecycle?.cancel()
            predecessorPublication?.cancel()
            // Publications accepted after this point belong only to the new generation. They
            // must never queue behind an invalidated predecessor tail.
            self.cloudPublishTail = nil
            self.cloudBridge = bridge
            self.cloudLifecycleTask = Task { [weak self] in
                // Stop the directly replaced bridge first: that is the signal which lets a
                // suspended connect/publication unwind. Cancellation must not skip the inherited
                // join that follows, so rapid A -> B -> C remains a transitive lifecycle.
                if let previous { await previous.stop() }
                await predecessorLifecycle?.value
                await predecessorPublication?.value
                guard let self, let bridge, !Task.isCancelled else { return }
                await bridge.setTransportReadyObserver { [weak self, weak bridge] readyGeneration in
                    guard let bridge else { return }
                    self?.cloudTransportBecameReady(
                        bridge, lifecycleGeneration: generation,
                        transportGeneration: readyGeneration
                    )
                }
                do {
                    try await bridge.start()
                    let stillCurrent = await self.cloudBridgeIsCurrent(
                        bridge, generation: generation
                    )
                    if Task.isCancelled || !stillCurrent {
                        await bridge.stop()
                    }
                } catch is CancellationError {
                    await bridge.stop()
                } catch {
                    Log.write("cloud: app bridge could not start — \(error.localizedDescription)")
                }
            }
        }
    }

    /// Wait until the attach, replacement, or detach most recently accepted by `queue` has
    /// completed. Configuration owners use this when replacing credentials or tearing down.
    func awaitCloudBridgeLifecycle() async {
        let task: Task<Void, Never>? = await withCheckedContinuation { continuation in
            queue.async { [weak self] in
                continuation.resume(returning: self?.cloudLifecycleTask)
            }
        }
        await task?.value
    }

    func cloudLifecycleStateForTesting(
        bridge expected: CloudAppBridge?
    ) async -> (bridgeMatches: Bool, generation: UInt64) {
        await withCheckedContinuation { continuation in
            queue.async { [weak self, weak expected] in
                guard let self else {
                    continuation.resume(returning: (expected == nil, 0))
                    return
                }
                let matches = (expected == nil && self.cloudBridge == nil)
                    || (expected != nil && self.cloudBridge === expected)
                continuation.resume(returning: (matches, self.cloudLifecycleGeneration))
            }
        }
    }

    private func cloudBridgeIsCurrent(_ bridge: CloudAppBridge, generation: UInt64) async -> Bool {
        await withCheckedContinuation { continuation in
            queue.async { [weak self] in
                continuation.resume(returning:
                    self?.cloudLifecycleGeneration == generation && self?.cloudBridge === bridge
                )
            }
        }
    }

    /// A relay-ready generation is authoritative evidence that relay memory may be fresh. Build
    /// both full snapshots on the main thread, then re-check lifecycle ownership before enqueueing
    /// them so a stale bridge cannot publish after detach or replacement.
    private func cloudTransportBecameReady(
        _ bridge: CloudAppBridge, lifecycleGeneration: UInt64, transportGeneration: UInt64
    ) {
        if let supplied = Self.cloudSnapshotDataForTesting {
            enqueueCloudReadySnapshots(
                supplied.sessions, orchestrator: supplied.orchestrator, bridge: bridge,
                lifecycleGeneration: lifecycleGeneration,
                transportGeneration: transportGeneration
            )
            return
        }
        DispatchQueue.main.async { [weak self, weak bridge] in
            guard let self, let bridge else { return }
            let sessions = try? JSONSerialization.data(
                withJSONObject: self.sessionsPayload(), options: [.withoutEscapingSlashes]
            )
            let orchestrator = try? JSONSerialization.data(withJSONObject: [
                "tasks": Orchestrator.records(), "at": Int(Date().timeIntervalSince1970),
            ], options: [.withoutEscapingSlashes])
            guard let sessions, let orchestrator else { return }
            self.enqueueCloudReadySnapshots(
                sessions, orchestrator: orchestrator, bridge: bridge,
                lifecycleGeneration: lifecycleGeneration,
                transportGeneration: transportGeneration
            )
        }
    }

    private func enqueueCloudReadySnapshots(
        _ sessions: Data, orchestrator: Data, bridge: CloudAppBridge,
        lifecycleGeneration: UInt64, transportGeneration: UInt64
    ) {
        queue.async { [weak self, weak bridge] in
            guard let self, let bridge,
                  self.cloudLifecycleGeneration == lifecycleGeneration,
                  self.cloudBridge === bridge
            else { return }
            _ = transportGeneration
            self.enqueueCloudPublication {
                try await bridge.publishSessions(sessions)
            }
            self.enqueueCloudPublication {
                try await bridge.publishOrchestrator(orchestrator)
            }
        }
    }

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
        syncSnapshotObserver()
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
        listener?.cancel()
        listener = nil
        syncSnapshotObserver()
        queue.async { [weak self] in
            guard let self else { return }
            for stream in self.streams.values { stream.connection.cancel() }
            self.streams.removeAll()
            self.transcriptRevisionStream.stop()
        }
    }

    private func syncSnapshotObserver() {
        if isRunning || cloudAttached {
            SessionWatch.shared.observers["remote"] = { [weak self] in self?.broadcast() }
            SessionWatch.shared.scanCompletionObservers["remote"] = {
                [weak self] in self?.broadcast()
            }
        } else {
            SessionWatch.shared.observers.removeValue(forKey: "remote")
            SessionWatch.shared.scanCompletionObservers.removeValue(forKey: "remote")
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
            if let refusal = eventStreamRefusal(request) { send(refusal, on: conn); return }
            openStream(on: conn)
            return
        }
        // Dictation is the other route that cannot go through `route`, and for the opposite
        // reason: it does answer and close, but not for a second and a half. `route` runs on the
        // one queue every connection is read on, and whisper-cli takes 1.6 seconds warm and about
        // twelve after a reboot — measured, docs/whisper.md. Left in `route`, one recording would
        // stop every other request, the event stream and its heartbeat for as long as it took,
        // and a stream that goes quiet for twelve seconds is a phone that decides the Mac died.
        if request.method == "POST", request.path == "/v1/voice" {
            transcribe(request, on: conn)
            return
        }
        // And the planner is the third, for the same reason with a bigger number on it. Dictation
        // is off `route` because 1.6 seconds on the shared queue stops everything; a model turn
        // is 3.2 to 5.1 seconds measured on this Mac and 30 at its deadline, which is not a
        // slower version of the same problem — it is a phone deciding the Mac died. The reasoning
        // in the comment above is not about whisper, it is about *how long the answer takes*, so
        // anything that takes seconds belongs here rather than in the switch.
        if request.method == "POST", request.path == "/v1/intents" {
            plan(request, on: conn)
            return
        }
        // Analytics has its own bounded worker and admission budget: a full analytics queue must
        // never spend the depth reserved for a phone's /info, transcript or places refresh.
        if request.method == "GET", Self.isUsageAnalyticsReading(request.path) {
            readUsageAnalytics(request, on: conn)
            return
        }
        // And these bounded reads are that same rule applied backwards, to routes written before anybody
        // had said it out loud. None of them takes seconds the way the two above do — the dearest
        // is 0.531 — but they are what a phone asks for over and over, and on the shared queue a
        // request is not slow, it is *exclusive*: five `/info` in flight answered `/v1/health`, a
        // one-millisecond route, in 3.143 seconds, and held the event stream and its heartbeat for
        // the same three. See `readSlowly`.
        // Transcript owns a bounded serial reader that is independent of info and places.
        if request.method == "GET", Self.isTranscriptReading(request.path) {
            readTranscript(request, on: conn)
            return
        }
        if request.method == "GET", Self.isSlowReading(request.path) {
            readSlowly(request, on: conn)
            return
        }
        if (request.method == "POST" || request.method == "DELETE"),
           Self.isOrchestratorTerminalWorkerRoute(request.path) {
            unfiledTerminalMutation(request) { [weak self] response in
                guard let self else { conn.cancel(); return }
                self.send(self.withCachePolicy(response), on: conn)
            }
            return
        }
        if request.method == "POST", Self.isTerminalSend(request.path) {
            sendTerminal(request) { [weak self] response in
                guard let self else { conn.cancel(); return }
                self.send(self.withCachePolicy(response), on: conn)
            }
            return
        }
        if request.method == "POST", Self.isTerminalWorkerRoute(request.path) {
            terminalMutation(request) { [weak self] response in
                guard let self else { conn.cancel(); return }
                self.send(self.withCachePolicy(response), on: conn)
            }
            return
        }
        let response = route(request)
        send(response, on: conn)
    }

    /// The event stream's real admission seam, shared with the focused cookie test. Returning
    /// `nil` means the caller may upgrade this request into the long-lived stream.
    func eventStreamRefusal(_ request: Request) -> Response? {
        if let refusal = crossOriginRefusal(request) { return refusal }
        if case .denied = permission(for: request) {
            return .error(401, "unauthorized", "This needs a paired device.")
        }
        return nil
    }

    /// A JSON object body, or an empty one. Shared by the orchestrator routes, which read their
    /// identity fields out of it and refuse in the model rather than at the door.
    func orchestratorBody(_ request: Request) -> [String: Any] {
        (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
    }

    static func isTerminalSend(_ path: String) -> Bool {
        guard path.hasPrefix("/v1/sessions/"), path.hasSuffix("/send") else { return false }
        let id = path.dropFirst("/v1/sessions/".count).dropLast("/send".count)
        return !id.isEmpty && !id.contains("/")
    }

    /// The remaining HTTP mutations whose existing route bodies may wait on terminal
    /// automation. `/send` keeps its image-aware path; these share its bounded worker.
    static func isTerminalWorkerRoute(_ path: String) -> Bool {
        if path.hasPrefix("/v1/sessions/") {
            return path.hasSuffix("/key") || path.hasSuffix("/end")
                || path.hasSuffix("/focus")
                || (path.hasSuffix("/kill") && path.contains("/shells/"))
        }
        guard path.hasPrefix("/v1/places/") else { return false }
        let rest = path.dropFirst("/v1/places/".count)
        let parts = rest.split(separator: "/", omittingEmptySubsequences: false)
        if (2...4).contains(parts.count) && parts[1] == "start" { return true }
        return (parts.count == 3 || parts.count == 4) && parts[1] == "resume"
    }

    /// Orchestrator writes that may open a tab or deliver a coordination message. Their own task,
    /// handoff and wait identifiers provide idempotency, so they need bounded isolation but not
    /// the paired-device idempotency table used by session routes.
    static func isOrchestratorTerminalWorkerRoute(_ path: String) -> Bool {
        if path == "/v1/orchestrator/tasks"
            || path == "/v1/orchestrator/detached-tasks"
            || path == "/v1/orchestrator/handoffs"
            || path == "/v1/orchestrator/root-assignments"
            || path == "/v1/orchestrator/waits"
            || path == "/v1/orchestrator/landing-queue/advance" { return true }
        if path.hasPrefix("/v1/orchestrator/schedules/") && path.hasSuffix("/run") {
            return true
        }
        let task = path.dropFirst("/v1/orchestrator/tasks/".count)
        if path.hasPrefix("/v1/orchestrator/tasks/"), !task.isEmpty,
           !task.contains("/") { return true } // the DELETE cancel route
        return path.hasPrefix("/v1/orchestrator/waits/") && path.hasSuffix("/release")
    }

    /// Every route that answers with a body and closes. Split out from the connection handling so
    /// that a test can ask it a question without opening a socket.
    func route(_ request: Request) -> Response {
        withCachePolicy(dispatch(request))
    }

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
    ///
    /// A step of its own rather than four lines inside `route`, because dictation does not go
    /// through `route` — it is written to the connection from another queue — and an answer that
    /// went out around the door would be the one response here a browser is free to guess about.
    private func withCachePolicy(_ response: Response) -> Response {
        var response = response
        if response.headers["Cache-Control"] == nil {
            response.headers["Cache-Control"] = "no-store"
        }
        return response
    }

    /// Decode only the closed, data-free part of the history routes. Keeping this separate from
    /// place lookup makes the compatibility default and the explicit assistant one testable
    /// without teaching a test how to manufacture somebody's real transcript directory.
    struct PlaceHistoryTarget {
        let placeID: String
        let assistant: Assistant
    }

    struct PlaceResumeTarget {
        let placeID: String
        let sessionID: String
        let assistant: Assistant
    }

    static func placeHistoryTarget(_ path: String) -> PlaceHistoryTarget? {
        guard path.hasPrefix("/v1/places/") else { return nil }
        let rest = String(path.dropFirst("/v1/places/".count))
        let parts = rest.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
        guard parts.count == 2 || parts.count == 3, parts[1] == "sessions",
              let assistant = parts.count == 2 ? Assistant.claude
                : Assistant(rawValue: parts[2]) else { return nil }
        return PlaceHistoryTarget(placeID: parts[0], assistant: assistant)
    }

    static func placeResumeTarget(_ path: String) -> PlaceResumeTarget? {
        guard path.hasPrefix("/v1/places/") else { return nil }
        let rest = String(path.dropFirst("/v1/places/".count))
        let parts = rest.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
        let assistant: Assistant
        let conversationPart: String
        if parts.count == 3, parts[1] == "resume" {
            assistant = .claude
            conversationPart = parts[2]
        } else if parts.count == 4, parts[1] == "resume",
                  let named = Assistant(rawValue: parts[2]) {
            assistant = named
            conversationPart = parts[3]
        } else {
            return nil
        }
        return PlaceResumeTarget(placeID: parts[0],
                                 sessionID: conversationPart.removingPercentEncoding
                                    ?? conversationPart,
                                 assistant: assistant)
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
        // `/project-<size>-<packed>.png` is here for a different reason and it is worth writing
        // down. That one is not the same drawing for everybody — it is a project's own mark. It
        // is open because the fetch is not the page's: an operating system drawing a notification
        // goes and gets the icon itself, with none of this device's credentials, so a route
        // behind the gate is a notification with no picture on it. What keeps that honest is the
        // shape of the URL — it carries the colours and nothing else, no path, no session id, no
        // project id — so answering it discloses exactly what the caller already wrote down. See
        // `RemoteIcon.pack`.
        let icon = request.path == "/sw.js"
            || (request.path.hasPrefix("/splash-") && request.path.hasSuffix(".png"))
            || request.path == "/favicon.ico"
            || (request.path.hasPrefix("/icon-") && request.path.hasSuffix(".png"))
            || (request.path.hasPrefix("/project-") && request.path.hasSuffix(".png"))
        // The orchestrator speaks with a credential of its own — a 0600 file only a local
        // process running as the user can read — because through a tunnel every request arrives
        // from 127.0.0.1, and a paired phone must never be able to start sessions. Reads without
        // that token fall through to ordinary device auth, so the page can show the tasks; the
        // complete, notify and landing routes are gated inside their handlers. Complete and
        // notify accepts only the per-task secret. Landing accepts that secret for pending or
        // abandoned, while its landed transition requires the machine token inside the handler.
        let orchestrated = request.path.hasPrefix("/v1/orchestrator/")
        let orchestratorAuthed = orchestrated
            && Orchestrator.verifyDispatch(token: request.headers["x-clawdline-orchestrator"])
        let taskSecretRoute = orchestrated
            && request.path.hasPrefix("/v1/orchestrator/tasks/")
            && ((request.method == "POST"
                 && (request.path.hasSuffix("/complete") || request.path.hasSuffix("/notify")
                     || request.path.hasSuffix("/landing")
                     || request.path.hasSuffix("/progress")))
                // The one read a task may make with its own secret: what else is in flight in
                // the repository it was sent to. A child has no orchestrator token, and the
                // alternative to this door is teaching every leaf to read one.
                || (request.method == "GET" && request.path.hasSuffix("/inflight")))
        if !open.contains(request.path), !pairing, !shell, !icon, !orchestratorAuthed, !taskSecretRoute {
            if case .denied = permission(for: request) {
                return .error(401, "unauthorized", "This needs a paired device.")
            }
        }
        if let response = writeOriginRefusal(request) ?? CoordinatorSuccessionHTTP.route(request, orchestratorAuthed: orchestratorAuthed, server: self) { return response }

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
                // A random per-process correlation value, disclosed with health so the bootstrap
                // replacement can prove that the listener answering now is not the one it stopped.
                // It names no session, task, repository, path or credential; it reveals only the
                // same restart boundary already observable from this route's availability/build.
                "instance": Orchestrator.appInstanceID,
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
            return RemotePage.strings(for: request)

        case ("GET", "/v1/sessions"):
            return .json(sessionsPayload())

        // Read-level despite being a POST: this types nothing and changes no session. It asks the
        // Mac to replace its published evidence, which a plain GET cannot do when the last scan
        // happened to collide with an iTerm automation stall. SessionWatch bounds repeated asks to
        // one in-flight read plus one remembered follow-up.
        case ("POST", "/v1/sessions/refresh"):
            let receipt = onMain(from: "RemoteServer.sessionRefresh") {
                SessionWatch.shared.refresh()
            }
            let state = receipt.disposition.rawValue
            return .json([
                "ok": true,
                "state": state,
                "accepted": receipt.disposition == .accepted,
                "coalesced": receipt.disposition == .coalesced,
                "throttled": receipt.disposition == .throttled,
                "scan": [
                    "completed": ["sequence": receipt.completedScanSequence],
                ],
            ])
        // Same-origin authenticated bytes for a reference already published in a transcript.
        // The URL carries only an opaque id; paths and source filenames never cross this route.
        case ("GET", let path) where path.hasPrefix("/v1/artifacts/images/"):
            let id = String(path.dropFirst("/v1/artifacts/images/".count))
            guard !id.isEmpty, !id.contains("/") else {
                return .error(404, "artifact_not_found", "No image artifact named that.")
            }
            let now = Self.imageArtifactNowForTesting?() ?? Date()
            switch SessionImageArtifactStore().lookup(id: id, now: now) {
            case .live(let artifact, let data):
                return Response(
                    status: 200,
                    headers: ["Content-Type": artifact.mediaType,
                              "Cache-Control": "private, no-store"],
                    body: data)
            case .expired:
                return .error(410, "artifact_expired",
                              "That image artifact has expired or been pruned.")
            case .missing:
                return .error(404, "artifact_not_found", "No image artifact named that.")
            }

        // A project status file may name one of two private HTML artifacts. The request contains
        // only a live session id and a typed slot; it never accepts a path from the caller. The
        // registered path is resolved again under that session's cwd before any bytes are read.
        case ("GET", let path) where path.hasPrefix("/v1/sessions/")
                && path.contains("/artifacts/"):
            let prefix = "/v1/sessions/"
            let rest = String(path.dropFirst(prefix.count))
            guard let marker = rest.range(of: "/artifacts/"),
                  !rest[..<marker.lowerBound].isEmpty else {
                return .error(404, "artifact_not_found", "No project artifact named that.")
            }
            let id = String(rest[..<marker.lowerBound])
            let kind = String(rest[marker.upperBound...])
            guard !kind.isEmpty, !kind.contains("/"),
                  let session = self.session(withID: id.removingPercentEncoding ?? id),
                  let cwd = Targets.workingDirectory(of: session) else {
                return .error(404, "artifact_not_found", "No project artifact named that.")
            }
            let status = ProjectStatus.read(cwd: cwd, remote: Project.info(cwd: cwd)?.remote,
                                            registry: ProjectIcon.row(forCwd: cwd)?["health"]
                                                as? [String: Any])
            let artifact: String?
            switch kind {
            case "backlog": artifact = status.backlog?.artifact
            case "milestone": artifact = status.milestone?.artifact
            default: artifact = nil
            }
            return Self.projectArtifactResponse(cwd: cwd, artifact: artifact)

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
            return .json(["links": linksPayload(cwd: cwd, sessionID: id)])

        // The facts behind this session's compact status line and expanded card: what it has
        // spent, what is left of the plan's window, how much has changed on disk, whether the
        // last deploy went out. Never put on the stream, for the reason `/links` gives — this
        // reads a transcript that can be fifty megabytes on top of the `git`; the page caches it.
        case ("GET", let path) where path.hasSuffix("/info") && path.hasPrefix("/v1/sessions/"):
            let id = String(path.dropFirst("/v1/sessions/".count).dropLast("/info".count))
            guard let session = self.session(withID: id.removingPercentEncoding ?? id) else {
                return .error(404, "not_found", "No session named that")
            }
            // Full info stays compatible; summary omits screen, Git and links/deploy.
            return .json(["info": infoPayload(for: session,
                includeDeferred: Self.infoIncludesDeferredComponents(request.query))])

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
                let publication = SessionWatch.shared.publishedInventory()
                let supplied = Self.sessionPayloadForTesting
                let targets = supplied?.0 ?? publication.targets
                let states = supplied?.1 ?? publication.states
                guard let fixed = Self.session(withID: id, among: targets) else {
                    return .error(404, "not_found", "No session named that")
                }
                let identity = Self.sessionWorkIdentity(
                    fixed, publishedIdentity: publication.identities[fixed.id])
                let matches = Self.identityMatchCounts(
                    targets.filter(\.isAssistant).map { candidate in
                        Self.sessionWorkIdentity(
                            candidate,
                            publishedIdentity: publication.identities[candidate.id])
                    })
                return .json(["session": json(
                    of: fixed, stateOverride: states[fixed.id], identity: identity,
                    identityMatches: matches[fixed.id] ?? 1,
                    inventory: sessionInventoryEvidence(publication),
                    acceptedPublication: publication,
                    publishedIdentity: publication.identities[fixed.id])])
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
            // One background command's own output. An agent has a conversation and this has a
            // file it is appending to, so the two routes sit beside each other and answer
            // differently — see `shellPayload`.
            if parts.count == 3, parts[1] == "shells" {
                let bytes = min(max(Int(request.query["bytes"] ?? "") ?? (64 << 10), 1 << 10),
                                1 << 20)
                let shell = parts[2].removingPercentEncoding ?? parts[2]
                return shellPayload(for: session, shell: shell, bytes: bytes)
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
            // One, and it says what it is. The only other way to see a notification is to
            // make a session ask you a question and wait, which is a long way to go to find out
            // whether a key was minted correctly — and a test that arrived must never be
            // mistaken for a session that needs you, so it carries no project and no mark.
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
            guard (2...4).contains(parts.count), parts[1] == "start" else {
                return .error(404, "not_found", "No such route")
            }
            let id = parts[0]
            let named = parts.count >= 3 ? parts[2] : Assistant.claude.rawValue
            guard let assistant = Assistant(rawValue: named) else {
                return .error(404, "not_found", "No assistant named that")
            }
            // A fourth segment for the model, parsed exactly as the third already is: matched
            // against ``Planner/models`` and nothing else, and refused by name rather than
            // quietly ignored. This route has never silently substituted anything — an
            // assistant nobody has heard of is a `404` — and a size it did not understand
            // becoming "whichever one is the default" would be a session running on a model
            // somebody did not ask for, with a `200` saying it worked.
            let model = parts.count == 4 ? parts[3] : ""
            guard parts.count < 4 || Planner.models.contains(model) else {
                return .error(404, "not_found", "No model named that")
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
                switch StartPoints.start(place, assistant: assistant,
                                         model: model.isEmpty ? nil : model) {
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
                    // `model` is echoed the way `assistant` is, and is empty when the path did
                    // not name one — a client that asked for a size should be able to see it
                    // arrived rather than infer it from a `200`.
                    return .json(["ok": true, "id": made, "backend": backend.rawValue,
                                  "assistant": assistant.rawValue, "model": model,
                                  "place": place.id, "cwd": place.path,
                                  "at": Int(Date().timeIntervalSince1970)])
                }
            }

        // What has already been said in a place. Read-level, and for the same reason the place
        // list is: it discloses conversation titles for a directory whose name this token could
        // already see. The final assistant segment selects that assistant's own supported index;
        // leaving it out is the original Claude-only route and stays compatible.
        case ("GET", let path) where path.hasPrefix("/v1/places/") && path.contains("/sessions"):
            guard let target = Self.placeHistoryTarget(path) else {
                return .error(404, "not_found", "No such route")
            }
            let id = target.placeID
            guard let place = StartPoints.place(withID: id.removingPercentEncoding ?? id) else {
                return .error(404, "not_found", "No place named that")
            }
            return .json(pastPayload(place, assistant: target.assistant))

        case ("GET", let path) where path.hasPrefix("/v1/places/"):
            return .error(404, "not_found", "No such route")

        // Picking one of those back up. **The conversation is a path segment, like the assistant
        // above and the place before it**, and it is resolved twice before it becomes a flag:
        // once for shape by `StartPoints.sessionName`, and once against the listing this Mac
        // builds for that directory. The body is still not read. There is still nowhere on this
        // route a directory or a command could be written.
        case ("POST", let path) where path.hasPrefix("/v1/places/") && path.contains("/resume/"):
            guard let target = Self.placeResumeTarget(path) else {
                return .error(404, "not_found", "No such route")
            }
            let id = target.placeID
            let assistant = target.assistant
            let conversation = target.sessionID
            return writing(request) { _ in
                guard let place = StartPoints.place(withID: id.removingPercentEncoding ?? id) else {
                    RemoteAuth.audit("place.resume", ["place": String(id.prefix(64)), "ok": "0",
                                                      "why": "not_found"])
                    return .error(404, "not_found", "No place named that")
                }
                // Written down whichever way it goes. An id that was never handed out is what
                // somebody guessing looks like, and this is the route where guessing right would
                // matter most.
                switch StartPoints.resume(place, sessionID: conversation, assistant: assistant) {
                case .refused(let status, let code, let message, let app):
                    RemoteAuth.audit("place.resume", ["place": place.id, "cwd": place.path,
                                                      "assistant": assistant.rawValue,
                                                      "session": String(conversation.prefix(64)),
                                                      "ok": "0", "why": code])
                    return .error(status, code, message, extra: app.map { ["app": $0] } ?? [:])
                case .started(let made, let backend):
                    RemoteAuth.audit("place.resume", ["place": place.id, "cwd": place.path,
                                                      "assistant": assistant.rawValue,
                                                      "session": conversation,
                                                      "ok": "1", "id": made])
                    DispatchQueue.main.async { SessionWatch.shared.nudge() }
                    return .json(["ok": true, "id": made, "backend": backend.rawValue,
                                  "assistant": assistant.rawValue,
                                  "place": place.id, "cwd": place.path,
                                  "session": conversation,
                                  "at": Int(Date().timeIntervalSince1970)])
                }
            }

        case ("POST", let path) where path.hasPrefix("/v1/places/"):
            return .error(404, "not_found", "No such route")

        // Root sessions dispatching child sessions. See Sources/Orchestrator.swift and
        // docs/orchestrator.md; who may call what is decided above, where `orchestratorAuthed`
        // is computed.

        case ("POST", "/v1/orchestrator/root-assignments"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Launching an independent Feature Root needs the orchestrator token.")
            }
            guard let body = try? JSONSerialization.jsonObject(with: request.body),
                  let obj = body as? [String: Any] else {
                return .error(400, "bad_request", "A JSON Root Assignment request is required.")
            }
            return answer(Orchestrator.rootAssignment(
                obj, idempotencyKey: request.headers["idempotency-key"]))

        case ("POST", "/v1/orchestrator/handoffs"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden", "Handing off needs the orchestrator token.")
            }
            guard let body = try? JSONSerialization.jsonObject(with: request.body),
                  let obj = body as? [String: Any], obj["handoff_id"] != nil else {
                return .error(400, "bad_request", "handoff_id is required.")
            }
            return answer(Orchestrator.handoff(obj))

        case ("POST", "/v1/orchestrator/tasks"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden", "Dispatching needs the orchestrator token.")
            }
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
            guard let taskID = body["task_id"] as? String,
                  let secret = body["secret"] as? String else {
                return .error(400, "bad_request", "task_id and secret are required.")
            }
            return answer(Orchestrator.dispatch(taskID: taskID, secret: secret,
                                                requireRootSession: true))

        case ("POST", "/v1/orchestrator/detached-tasks"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Detached automation needs the orchestrator token.")
            }
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            guard let taskID = body["task_id"] as? String,
                  let secret = body["secret"] as? String else {
                return .error(400, "bad_request", "task_id and secret are required.")
            }
            return answer(Orchestrator.dispatch(
                taskID: taskID, secret: secret, requireRootSession: true,
                allowDetachedAutomation: true))

        case ("POST", "/v1/orchestrator/notify"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden", "Agent notification needs the orchestrator token.")
            }
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            return answer(Orchestrator.agentNotify(title: body["title"] as? String ?? "",
                                                   body: body["body"] as? String ?? ""))

        case ("POST", "/v1/orchestrator/coordinator/register"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Registering the machine coordinator needs the orchestrator token.")
            }
            guard let body = try? JSONSerialization.jsonObject(with: request.body),
                  let obj = body as? [String: Any], Set(obj.keys) == ["session_id"],
                  let sessionID = obj["session_id"] as? String, !sessionID.isEmpty else {
                return .error(400, "bad_request",
                              "The closed request schema requires only session_id.")
            }
            let observation = coordinatorObservation()
            let live = observation.sessions
            let candidate = live.first(where: {
                $0.identity.terminalID == sessionID
            })
            return answer(Coordinator.register(
                sessionID: sessionID, candidate: candidate, among: live,
                sessionsFresh: observation.sessionsFresh,
                sessionsObservedAt: observation.sessionsObservedAt))

        case ("POST", "/v1/orchestrator/coordinator/rebind"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Reconnecting the machine coordinator needs the orchestrator token.")
            }
            guard let body = try? JSONSerialization.jsonObject(with: request.body),
                  let obj = body as? [String: Any],
                  Set(obj.keys) == ["expected_coordinator_id", "expected_generation", "session_id"],
                  let expectedID = obj["expected_coordinator_id"] as? String,
                  UUID(uuidString: expectedID) != nil,
                  let expectedGeneration = obj["expected_generation"] as? Int,
                  expectedGeneration > 0,
                  let sessionID = obj["session_id"] as? String, !sessionID.isEmpty else {
                return .error(400, "bad_request",
                              "The closed request schema requires expected_coordinator_id, "
                                + "expected_generation and session_id.")
            }
            let observation = coordinatorObservation()
            let candidates = observation.sessions.filter {
                $0.identity.terminalID == sessionID
            }
            guard !candidates.isEmpty else {
                return .error(404, "session_not_found",
                              "No live Claude or Codex session has that terminal-neutral id.")
            }
            guard candidates.count == 1, let candidate = candidates.first else {
                return .error(409, "session_ambiguous",
                              "More than one live process claims that terminal-neutral id.")
            }
            return answer(Coordinator.rebind(
                expectedCoordinatorID: expectedID, expectedGeneration: expectedGeneration,
                to: candidate,
                among: observation.sessions, sessionsFresh: observation.sessionsFresh,
                sessionsObservedAt: observation.sessionsObservedAt))

        case ("GET", "/v1/orchestrator/coordinator"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Reading the machine coordinator needs the orchestrator token.")
            }
            let observation = coordinatorObservation()
            let live = observation.sessions
            let registry = observation.registry
            return .json(Coordinator.inspection(
                liveSessions: live,
                bearings: .init(
                    sessionsFresh: observation.sessionsFresh,
                    activeTaskCount: registry.activeTasks,
                    pendingLandingCount: registry.pendingLandings,
                    pendingLandingRows: registry.pendingLandingRows,
                    openWaitCount: registry.openWaits,
                    sessionsObservedAt: observation.sessionsObservedAt,
                    registryObservedAt: registry.observedAt,
                    sessionsGeneration: observation.sessionsGeneration)))

        // The device-readable half of Bearings. No `orchestratorAuthed` guard on purpose: an
        // orchestrator read without the machine token falls through to ordinary device auth at
        // the top gate, which is exactly who this projection is for: the Clawdfather panel's four
        // read-only commands and the new-Session creation sheet's open/pre-send registration
        // checks, on a page that holds a device token. What it answers is an allowlist built in
        // `Coordinator.deviceBearings`: aggregate counts plus session facts a paired device can
        // already read from `GET /v1/sessions`, and never the durable coordinator UUID,
        // lifecycle bookkeeping, store health, tty, pid or conversation ids.
        case ("GET", "/v1/orchestrator/coordinator/bearings"):
            let observation = coordinatorObservation()
            let registry = observation.registry
            return .json(Coordinator.deviceBearings(
                liveSessions: observation.sessions,
                bearings: .init(
                    sessionsFresh: observation.sessionsFresh,
                    activeTaskCount: registry.activeTasks,
                    pendingLandingCount: registry.pendingLandings,
                    pendingLandingRows: registry.pendingLandingRows,
                    openWaitCount: registry.openWaits,
                    sessionsObservedAt: observation.sessionsObservedAt,
                    registryObservedAt: registry.observedAt,
                    sessionsGeneration: observation.sessionsGeneration)))

        case ("GET", "/v1/orchestrator/waits"):
            return .json(["waits": Orchestrator.coordinationWaitRecords(),
                          "at": Int(Date().timeIntervalSince1970)])

        case ("GET", "/v1/orchestrator/landings"):
            let observation = coordinatorObservation()
            return .json([
                "landings": observation.registry.pendingLandingRows,
                "sources": observation.registry.landingSources,
                "at": Int(observation.registry.observedAt.timeIntervalSince1970),
            ])

        // **Who is waiting to land here, in what order, and what they will collide over.**
        //
        // Read-level like `landings` above it, and derived the same way `inflight` is: there is no
        // route that adds an entry, because membership comes out of the task registry rather than
        // out of anybody's memory of it. `project` is any directory; the repository containing it
        // is resolved on this side, exactly as `inflight` does.
        case ("GET", "/v1/orchestrator/landing-queue"):
            let project = request.query["project"] ?? request.query["project_dir"] ?? ""
            return answer(OrchestratorLandingQueue.queueReply(project: project))

        case ("GET", "/v1/orchestrator/storage"):
            return .json(Orchestrator.storageInventory())

        // **What is being worked on in a repository right now, including what a worktree hides.**
        //
        // Read-level like the two lists above it, and mechanical: no model, no quota, no account.
        // The agent below is the good answer and this is the one that always works, which is why
        // both exist — an ask that cannot reach an assistant still hands the caller this list.
        // `project` is any directory; the repository containing it is resolved on this side.
        case ("GET", "/v1/orchestrator/inflight"):
            let project = request.query["project"] ?? request.query["project_dir"] ?? ""
            guard !project.isEmpty, project.hasPrefix("/"),
                  let repository = Orchestrator.inflightRepository(project) else {
                return .error(400, "bad_request",
                              "project must be an absolute path inside a Git repository.")
            }
            let branches = Orchestrator.repositoryBranches(in: repository)
            return .json(["repository": repository,
                          "inflight": Orchestrator.inflightRecords(repository: repository,
                                                                   branches: branches),
                          "at": Int(Date().timeIntervalSince1970)])

        // **Which sessions a coordination wait may name.** `POST /v1/orchestrator/waits` takes two
        // terminal-neutral session ids and, until this existed, there was nowhere for the caller
        // holding its credential to read one: `GET /v1/sessions` is the paired-device API and
        // answers the orchestrator token with a 401. So the one route that registers a wait was
        // reachable and the ids it takes were not.
        //
        // The remaining fallback covers only sessions already inside a wait:
        // `GET /v1/orchestrator/waits` cannot help the first session to wait on somebody. A
        // caller finding its own address uses the exact conversation-bound whoami route below;
        // `$ITERM_SESSION_ID` is only a cached terminal hint and can survive restart/resume.
        //
        // A route of its own rather than a wider door on `/v1/sessions`: that one carries the
        // screen — the line a session is working on, the question a waiting one is showing, the
        // agents and shells it has out, its transcript id — and this credential exists to
        // dispatch work, not to read somebody's terminal. What comes back here is an address
        // book. See `coordinationSessionRows` for where the line falls and why.
        case ("GET", "/v1/orchestrator/sessions"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Reading the sessions a wait can name needs the orchestrator token.")
            }
            let supplied = Self.sessionPayloadForTesting
            let publication = SessionWatch.shared.publishedInventory()
            return .json(["sessions": Self.coordinationSessionRows(
                            supplied?.0 ?? publication.targets,
                            states: supplied?.1 ?? publication.states,
                            inventory: self.sessionInventoryEvidence(publication),
                            publishedIdentities: publication.identities,
                            publishedLabels: publication.labels),
                          "at": Int(Date().timeIntervalSince1970)])

        // A process asks which terminal-neutral address currently holds its exact conversation.
        // The environment's iTerm id is not an input: after iTerm restarts and the assistant is
        // resumed it can name a terminal which no longer exists. Resolution uses the same strict
        // process-bound root resolver as dispatch, over one SessionWatch publication, then runs
        // that resolution again before returning so a concurrent detach/rebind refuses stale.
        case ("GET", "/v1/orchestrator/whoami"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Resolving session identity needs the orchestrator token.")
            }
            guard Set(request.query.keys) == Set(["conversation_id"]),
                  request.repeatedQueryKeys.isEmpty,
                  let conversationID = request.query["conversation_id"],
                  !conversationID.isEmpty else {
                return .error(400, "conversation_id_required",
                              "The closed query needs exactly one conversation_id.")
            }
            guard StartPoints.sessionName(conversationID) != nil else {
                return .error(400, "conversation_id_malformed",
                              "conversation_id must be one lowercase UUID.")
            }
            let supplied = Self.sessionPayloadForTesting?.0
            let snapshot = onMain(from: "RemoteServer.sessionWhoAmI") {
                supplied.map {
                    SessionWatch.IdentitySnapshot(targets: $0, generation: -1,
                                                  complete: true,
                                                  observedAt: Date(timeIntervalSince1970: 0),
                                                  epoch: "testing")
                } ?? SessionWatch.shared.identitySnapshot()
            }
            guard snapshot.complete else {
                return .error(
                    409, "registry_stale",
                    "The live session registry is incomplete; retry after the next scan.",
                    extra: ["retryable": true,
                            "registry_generation": snapshot.generation])
            }
            let identityPass: () -> [String: String]
            if let suppliedIdentity = Self.sessionConversationIDForTesting {
                identityPass = {
                    var values: [String: String] = [:]
                    for session in snapshot.targets {
                        if let value = suppliedIdentity(session) { values[session.id] = value }
                    }
                    return values
                }
            } else {
                identityPass = {
                    Transcript.freshIdentityPass(among: snapshot.targets).identities
                }
            }
            let answer: Response
            switch Self.sessionWhoAmI(
                    conversationID: conversationID, among: snapshot.targets,
                    identityPass: identityPass,
                    afterPass: Self.sessionIdentityPassDidFinishForTesting) {
                case .notFound:
                    answer = .error(
                        404, "conversation_not_found",
                        "No live session is bound to that exact conversation id.")
                case .ambiguous:
                    answer = .error(
                        409, "conversation_ambiguous",
                        "More than one live session claims that conversation id; none was chosen.")
                case .stale:
                    answer = .error(
                        409, "session_identity_stale",
                        "The conversation binding changed while it was being resolved; retry.",
                        extra: ["retryable": true,
                                "registry_generation": snapshot.generation])
                case .resolved(let target, let assistant, let canonicalConversationID):
                    var provenance: [String: Any] = [
                        "source": "live_session_registry",
                        "registry_generation": snapshot.generation,
                        "registry_complete": snapshot.complete,
                        "consistency": "single_snapshot_revalidated",
                    ]
                    if let observed = snapshot.observedAt {
                        provenance["registry_observed_at"] =
                            Int(observed.timeIntervalSince1970)
                    }
                    answer = .json([
                        "conversation_id": canonicalConversationID,
                        "terminal_id": target.id,
                        "assistant": assistant.rawValue,
                        "provenance": provenance,
                        "at": Int(Date().timeIntervalSince1970),
                    ])
            }
            return answer

        // A live assistant session speaking to another through Clawdline. This is not the
        // paired-device `/send` route: the source is resolved against current process-bound
        // session identity, the machine credential is required, and the terminal receives a
        // closed envelope which transcript readers can attribute without calling it the user.
        case ("POST", "/v1/orchestrator/messages"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Relaying a session message needs the orchestrator token.")
            }
            return orchestratorWriting(request) { body in
                let required = Set(["from_session", "to_session", "text"])
                let allowed = required.union(["images"])
                guard Set(body.keys).isSubset(of: allowed),
                      required.isSubset(of: Set(body.keys)),
                      let sourceID = body["from_session"] as? String, !sourceID.isEmpty,
                      let targetID = body["to_session"] as? String, !targetID.isEmpty,
                      let text = body["text"] as? String,
                      text.count <= 100_000 else {
                    return .error(400, "bad_request",
                                  "The closed body needs only from_session, to_session, "
                                  + "0…100000 characters of text and optional images.")
                }
                var imagePaths: [String] = []
                if let raw = body["images"] {
                    guard let images = raw as? [[String: Any]], !images.isEmpty,
                          images.count <= SessionImageArtifactStore.productionPolicy
                            .maxImagesPerMessage else {
                        return .error(400, "bad_request",
                                      "images must be a non-empty bounded array of local paths.")
                    }
                    for image in images {
                        guard Set(image.keys) == Set(["path"]),
                              let path = image["path"] as? String, !path.isEmpty else {
                            return .error(400, "bad_request",
                                          "Each image accepts only one string path field.")
                        }
                        imagePaths.append(path)
                    }
                }
                guard !text.isEmpty || !imagePaths.isEmpty else {
                    return .error(400, "bad_request",
                                  "A session message needs text or at least one local image.")
                }
                guard let source = self.sessionMessageSource(withID: sourceID),
                      let sourceAssistant = source.assistant else {
                    return .error(404, "source_not_found",
                                  "No current assistant session has that terminal or "
                                  + "conversation id.")
                }
                guard let target = self.session(withID: targetID), target.isAssistant else {
                    return .error(404, "target_not_found",
                                  "No current assistant session has that terminal id.")
                }
                guard source.id != target.id else {
                    return .error(409, "same_session",
                                  "A session message must go to a different session.")
                }
                if self.state(of: target.id) == .waiting, Targets.isChoosing(target) {
                    return .error(409, "target_busy",
                                  "The target is showing a menu; typing would answer it instead "
                                  + "of delivering the message.")
                }

                let now = Self.imageArtifactNowForTesting?() ?? Date()
                let store = SessionImageArtifactStore()
                let stored: [SessionImageArtifactStore.Stored]
                if imagePaths.isEmpty {
                    stored = []
                } else {
                    do {
                        stored = try store.importPaths(imagePaths, now: now)
                    } catch let refusal as SessionImageArtifactStore.Refusal {
                        return .error(refusal.status, refusal.code, refusal.message)
                    } catch {
                        return .error(500, "artifact_storage_failed",
                                      "Clawdline could not persist the image artifacts.")
                    }
                }

                let labelPublication = SessionWatch.shared.publishedInventory()
                let message = ClawdlineSessionMessage.Message(
                    source: .init(
                        id: source.id,
                        label: labelPublication.labels[source.id] ?? source.coordinate,
                                  assistant: sourceAssistant),
                    body: text,
                    artifacts: stored.map(\.artifact))
                let wire = ClawdlineSessionMessage.encode(message)
                guard wire.hasPrefix(ClawdlineSessionMessage.opening) else {
                    store.delete(ids: stored.map { $0.artifact.id }, now: now)
                    return .error(500, "encoding_failed",
                                  "The session message could not be encoded safely.")
                }
                let failure: String?
                if let seam = Self.terminalSendForTesting {
                    failure = seam(wire, target)
                } else {
                    failure = Targets.send(wire, to: target)
                }
                if let failure {
                    store.delete(ids: stored.map { $0.artifact.id }, now: now)
                    return .error(502, "delivery_failed", failure)
                }
                RemoteAuth.audit("orchestrator.message", [
                    "from": source.id, "to": target.id,
                    "assistant": sourceAssistant.rawValue, "chars": "\(text.count)",
                    "images": "\(stored.count)",
                ])
                DispatchQueue.main.async { SessionWatch.shared.nudge() }
                let at = Int(Date().timeIntervalSince1970)
                var answer: [String: Any] = ["ok": true, "accepted_at": at, "at": at]
                if !stored.isEmpty {
                    answer["artifacts"] = stored.map { $0.artifact.object }
                }
                return .json(answer)
            }

        // A root's explicit end-of-turn receipt. The path names the terminal-neutral id already
        // published by the GET above; every process/conversation fact is resolved here from the
        // live target, never trusted from the request body. It is a single-check delivery claim,
        // not the broker-verified landing that produces work_complete.
        case ("POST", let path) where path.hasPrefix("/v1/orchestrator/sessions/")
            && path.hasSuffix("/complete"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Reporting root delivery needs the orchestrator token.")
            }
            let encoded = String(path.dropFirst("/v1/orchestrator/sessions/".count)
                .dropLast("/complete".count))
            let id = encoded.removingPercentEncoding ?? encoded
            guard !id.isEmpty, !id.contains("/") else {
                return .error(400, "bad_request", "The route must name one session id.")
            }
            guard let target = self.session(withID: id), target.isAssistant else {
                return .error(404, "session_not_found", "No current assistant session named \(id).")
            }
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            guard Set(body.keys) == Set(["summary"]), body["summary"] is String else {
                return .error(400, "bad_request", "The body must contain only string summary.")
            }
            let reply = Orchestrator.reportSessionDelivery(
                identity: Self.sessionWorkIdentity(target), terminalState: self.state(of: id),
                summary: body["summary"] as? String ?? "")
            DispatchQueue.main.async { SessionWatch.shared.nudge() }
            return answer(reply)

        // Evidence about one Session that the broker cannot observe itself: shared-tree hunks it owns, local
        // todos nothing registered, artifacts and deployments outside the repository, decisions
        // never written to `owed`. A route of its own rather than another meaning for `/state`,
        // because this one is about *ending* and that one is about what to do next.
        //
        // **Bound to one turn by `activity_generation`, not by a same-millisecond screen
        // reading.** The session writes this at the end of its turn; whether a SessionWatch beat
        // landed on the same instant is not evidence about anything, and requiring it would be a
        // race the honest caller loses. The machine orchestrator token is the authorization
        // boundary. The receipt binds the assertion to the exact target process and turn; it
        // does not prove which credential holder authored it. Only a fresh broker reread may
        // output `safe`.
        case ("POST", let path) where path.hasPrefix("/v1/orchestrator/sessions/")
            && path.hasSuffix("/closure"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Attesting closure needs the orchestrator token.")
            }
            let encoded = String(path.dropFirst("/v1/orchestrator/sessions/".count)
                .dropLast("/closure".count))
            let id = encoded.removingPercentEncoding ?? encoded
            guard !id.isEmpty, !id.contains("/") else {
                return .error(400, "bad_request", "The route must name one session id.")
            }
            guard let target = self.session(withID: id), target.isAssistant else {
                return .error(404, "session_not_found", "No current assistant session named \(id).")
            }
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            let allowed: Set<String> = ["status", "activity_generation", "note", "audit_id"]
            guard !body.isEmpty, Set(body.keys).isSubset(of: allowed) else {
                return .error(400, "bad_request",
                              "The body may contain only status, activity_generation, note "
                                  + "and audit_id.")
            }
            guard let status = body["status"] as? String else {
                return .error(400, "bad_request", "status must be a string.")
            }
            guard body["activity_generation"] == nil
                    || body["activity_generation"] is Int else {
                return .error(400, "bad_request", "activity_generation must be a number.")
            }
            if let note = body["note"], !(note is String) {
                return .error(400, "bad_request", "note must be a string.")
            }
            if let auditID = body["audit_id"], !(auditID is String) {
                return .error(400, "bad_request", "audit_id must be a string.")
            }
            let reply = Orchestrator.attestClosure(
                identity: Self.sessionWorkIdentity(target), status: status,
                activityGeneration: body["activity_generation"] as? Int,
                note: body["note"] as? String, auditID: body["audit_id"] as? String)
            return answer(Self.closureProjectionAnswer(reply) {
                self.closeability(of: target)
            })

        // A session's declaration about its own quiet state — the `self` half of the work-state
        // provenance boundary (docs/session-states.md). Identity is resolved from the live
        // watched process exactly as `/complete` does; the body may claim only `ready` or
        // `holding`, may record or clear the `owed` debt, and can never produce a check.
        case ("POST", let path) where path.hasPrefix("/v1/orchestrator/sessions/")
            && path.hasSuffix("/state"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Declaring a session state needs the orchestrator token.")
            }
            let encoded = String(path.dropFirst("/v1/orchestrator/sessions/".count)
                .dropLast("/state".count))
            let id = encoded.removingPercentEncoding ?? encoded
            guard !id.isEmpty, !id.contains("/") else {
                return .error(400, "bad_request", "The route must name one session id.")
            }
            guard let target = self.session(withID: id), target.isAssistant else {
                return .error(404, "session_not_found", "No current assistant session named \(id).")
            }
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            let allowed: Set<String> = ["state", "note", "moved_by", "person_needed", "owed"]
            guard !body.isEmpty, Set(body.keys).isSubset(of: allowed) else {
                return .error(400, "bad_request",
                              "The body may contain only state, note, moved_by, person_needed "
                                  + "and owed.")
            }
            if let raw = body["state"], !(raw is String) {
                return .error(400, "bad_request", "state must be a string.")
            }
            var owed: [String: Any]?
            var clearOwed = false
            if let rawOwed = body["owed"] {
                if rawOwed is NSNull {
                    clearOwed = true
                } else if let dict = rawOwed as? [String: Any] {
                    guard Set(dict.keys).isSubset(of: ["note", "moved_by", "person_needed"]) else {
                        return .error(400, "bad_request",
                                      "owed may contain only note, moved_by and person_needed.")
                    }
                    owed = dict
                } else {
                    return .error(400, "bad_request", "owed must be an object, or null to clear.")
                }
            }
            let declared = Orchestrator.declareSessionState(
                identity: Self.sessionWorkIdentity(target), terminalState: self.state(of: id),
                claim: body["state"] as? String, note: body["note"] as? String,
                movedBy: body["moved_by"] as? String,
                personNeeded: body["person_needed"] as? Bool,
                owed: owed, clearOwed: clearOwed)
            DispatchQueue.main.async { SessionWatch.shared.nudge() }
            return answer(declared)

        // What this Mac can say about each assistant's own account-level quota — one read of two
        // small local files, 5-second cached, and deliberately *not* behind `readingDepth`: it
        // has to be cheap enough for `Orchestrator.dispatch()` itself to call synchronously at
        // its own gate. See docs/api.md and Sources/AssistantQuota.swift.
        case ("GET", "/v1/orchestrator/assistants"):
            let now = Date()
            return .json(["at": Int(now.timeIntervalSince1970),
                          "assistants": AssistantQuota.all(now: now).map { $0.payload(now: now) }])

        case ("POST", "/v1/orchestrator/waits"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Registering a coordination wait needs the orchestrator token.")
            }
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            if let waiterID = body["waiter_session_id"] as? String,
               self.session(withID: waiterID) == nil {
                return .error(404, "waiter_not_found", "No waiter session named \(waiterID).")
            }
            let reply = Orchestrator.registerCoordinationWait(
                body, readiness: self.coordinationReadiness,
                deliver: { targetID, text in
                    guard let target = self.session(withID: targetID) else {
                        return "No session named \(targetID)."
                    }
                    return Targets.send(text, to: target)
                })
            DispatchQueue.main.async { SessionWatch.shared.nudge() }
            return answer(reply)

        case ("POST", let path) where path.hasPrefix("/v1/orchestrator/waits/")
            && path.hasSuffix("/release"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Releasing a coordination wait needs the orchestrator token.")
            }
            let id = String(path.dropFirst("/v1/orchestrator/waits/".count)
                .dropLast("/release".count))
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            let reply = Orchestrator.releaseCoordinationWait(
                id: id.removingPercentEncoding ?? id,
                ownerSessionID: body["owner_session_id"] as? String ?? "",
                commit: body["commit"] as? String, note: body["note"] as? String,
                readiness: self.coordinationReadiness,
                deliver: { targetID, text in
                    guard let target = self.session(withID: targetID) else {
                        return "No session named \(targetID)."
                    }
                    return Targets.send(text, to: target)
                })
            DispatchQueue.main.async { SessionWatch.shared.nudge() }
            return answer(reply)

        // **Position is the only thing a coordinator writes.** It cannot add an entry and it
        // cannot take one away — both refusals are typed below — so the worst a wrong order can
        // do is put somebody in the wrong place, which is visible, rather than out of the queue,
        // which was not.
        case ("POST", "/v1/orchestrator/landing-queue/order"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Setting the landing order needs the orchestrator token.")
            }
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            guard let keys = body["order"] as? [String] else {
                return .error(400, "bad_request", "order is an array of queued root keys.")
            }
            return answer(OrchestratorLandingQueue.setOrder(
                project: body["project"] as? String ?? body["project_dir"] as? String ?? "",
                keys: keys, ifGeneration: body["if_generation"] as? Int,
                setBy: body["set_by"] as? String))

        // **The handoff a person used to type.** The next holder is arithmetic; this is the call
        // that reaches them, once, with the structured answer quoted rather than summarised.
        case ("POST", "/v1/orchestrator/landing-queue/advance"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Handing on the landing slot needs the orchestrator token.")
            }
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            let reply = OrchestratorLandingQueue.advance(
                project: body["project"] as? String ?? body["project_dir"] as? String ?? "",
                readiness: self.coordinationReadiness,
                deliver: { targetID, text in
                    guard let target = self.session(withID: targetID) else {
                        return "No session named \(targetID)."
                    }
                    return Targets.send(text, to: target)
                })
            DispatchQueue.main.async { SessionWatch.shared.nudge() }
            return answer(reply)

        case ("POST", let path) where path.hasPrefix("/v1/orchestrator/waits/")
            && path.hasSuffix("/cancel"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Cancelling a coordination wait needs the orchestrator token.")
            }
            let id = String(path.dropFirst("/v1/orchestrator/waits/".count)
                .dropLast("/cancel".count))
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            let reply = Orchestrator.cancelCoordinationWait(
                id: id.removingPercentEncoding ?? id,
                waiterSessionID: body["waiter_session_id"] as? String ?? "")
            DispatchQueue.main.async { SessionWatch.shared.nudge() }
            return answer(reply)

        case ("GET", "/v1/orchestrator/schedules"):
            return .json(["schedules": Orchestrator.scheduleRecords(),
                          "at": Int(Date().timeIntervalSince1970)])

        // **The first way anything but a text editor makes a schedule.**
        //
        // Gated by the three gates every other paired-device write goes through, and
        // deliberately *not* by the orchestrator token: that token is a `0600` file on this Mac,
        // which is exactly what makes it a proof of being local, and a phone cannot have one.
        // This route is for the phone. What it adds to a paired device's reach is real and is
        // said out loud in docs/schedules.md — a phone can now arrange work that runs later with
        // nobody watching it — and what it does not add is anywhere to write a path: the body
        // carries a `place_id` from `/v1/places` and the directory is looked up on this side.
        //
        // `429` and the two `write_failed` fives are the answers not filed under the key, for the
        // reason `transcribe` spells out: they are facts about this machine at this moment rather
        // than about the request, and a cached one would tell the retry that was supposed to work
        // that the brake is still on long after it let go.
        case ("POST", "/v1/orchestrator/schedules"):
            return writing(request, keeping: { $0.status != 429 && $0.status < 500 }) { body in
                self.answer(RemoteServer.scheduleAnswer(
                    Orchestrator.createSchedule(from: body),
                    dispatchEnabled: Config.shared.orchestratorEnabled))
            }

        // One schedule, in full, including the task template and retained run history the list
        // leaves out — see `Orchestrator.scheduleRecord(id:now:)`. Read-level, like the list it
        // came from.
        case ("GET", let path) where path.hasPrefix("/v1/orchestrator/schedules/"):
            let id = String(path.dropFirst("/v1/orchestrator/schedules/".count))
            guard let record = Orchestrator.scheduleRecord(id: id.removingPercentEncoding ?? id)
            else { return .error(404, "not_found", "No schedule named that") }
            return .json(["schedule": record])

        // **Changing one, and taking one away.** Behind the same three gates as the route that
        // makes one and deliberately not behind the orchestrator token, for the same reason: a
        // phone cannot hold a `0600` file, and these are for the phone. Until these existed the
        // only way to fix a wrong time was a text editor on this Mac, so every mistaken creation
        // had to be cleaned up back at the desk.
        //
        // PATCH takes the body POST takes. `schedule_id` and `created_at` are not fields it may
        // carry — they are read off the file being replaced, and `Orchestrator.updateSchedule`
        // says why the second of those matters.
        //
        // The same answers are kept out of the ten-minute idempotency table as on the create
        // route: `429` and the fives are facts about this machine at this moment rather than
        // about the request, and a cached one would tell the retry that was supposed to work
        // that the brake is still on long after it let go.
        case ("PATCH", let path) where path.hasPrefix("/v1/orchestrator/schedules/"):
            let id = String(path.dropFirst("/v1/orchestrator/schedules/".count))
            let cleaned = id.removingPercentEncoding ?? id
            return writing(request, keeping: { $0.status != 429 && $0.status < 500 }) { body in
                self.answer(Orchestrator.updateSchedule(id: cleaned, from: body))
            }

        case ("DELETE", let path) where path.hasPrefix("/v1/orchestrator/schedules/"):
            let id = String(path.dropFirst("/v1/orchestrator/schedules/".count))
            let cleaned = id.removingPercentEncoding ?? id
            return writing(request, keeping: { $0.status < 500 }) { _ in
                self.answer(Orchestrator.deleteSchedule(id: cleaned))
            }

        case ("POST", let path) where path.hasPrefix("/v1/orchestrator/schedules/")
            && path.hasSuffix("/run"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden", "Running a schedule needs the orchestrator token.")
            }
            let id = String(path.dropFirst("/v1/orchestrator/schedules/".count)
                .dropLast("/run".count))
            return answer(Orchestrator.runSchedule(id: id.removingPercentEncoding ?? id))

        // What every session on this machine has spent, out of the durable ledger rather than
        // out of the registry — which keeps 200 rows and is why the ledger exists. Read-level,
        // like the schedules list: it answers a question, it starts nothing.
        //
        // Three shapes over one public projection: the dashboard contract, spreadsheet-safe CSV
        // and lossless JSON. The query service removes raw paths/session ids once, before any of
        // the three can see them, and keeps pagination/filtering off the shared HTTP queue.
        //
        // **Both are required to render an unknown as absent.** `tokens`, `total` and `cost` come
        // back `null` where nothing was measured, and the CSV leaves the field empty; a sealed
        // `source_missing` row is never a zero on either. That is not a nicety. Summing absent
        // costs as zero once produced "1137M tokens, $0.00", which is a month-end that looks
        // entirely normal and is wrong in the direction nobody checks.
        //
        // **And a row the store marked arrives here still marked.** `tokenPartsUnknown` says
        // which part a summed column is short of and on how many rows, and `coverageReasons`
        // carries the store's own words — `session_unresolved` for a session identity that had
        // to be invented, `source_regressed` for a number measured across a replaced transcript.
        // Neither is visible in `coverage`, which says only how much of a source was read; both
        // came back from review as rows that reached this route looking perfectly healthy.
        // These two URLs are the pre-existing forensic contracts. They deliberately retain the
        // old aggregate schema and 44-column CSV byte shape; privacy-safe public analytics lives
        // on the explicit versioned DTO paths below rather than changing old callers in place.
        case ("GET", "/v1/orchestrator/usage"), ("GET", "/v1/orchestrator/usage.csv"):
            let group = UsageLedger.GroupBy(rawValue: request.query["group"] ?? "")
            if request.query["group"] != nil, group == nil {
                return .error(400, "bad_request",
                              "group must be one of "
                                + UsageLedger.GroupBy.allCases.map(\.rawValue).joined(separator: ", "))
            }
            let from = request.query["from"].flatMap { $0.isEmpty ? nil : $0 }
            let to = request.query["to"].flatMap { $0.isEmpty ? nil : $0 }
            for value in [from, to] where value != nil && !UsageLedger.isLocalDay(value!) {
                return .error(400, "bad_request", "from and to are local dates, YYYY-MM-DD.")
            }
            if request.path.hasSuffix(".csv") {
                let csv = UsageLedger.shared.exportCSV(from: from, to: to)
                return Response(status: 200,
                                headers: ["Content-Type": "text/csv; charset=utf-8",
                                          "Content-Disposition":
                                            "attachment; filename=\"clawdline-usage.csv\""],
                                body: Data(csv.utf8))
            }
            let aggregate = UsageLedger.shared.aggregate(from: from, to: to,
                                                         groupBy: group ?? .model)
            return .json(["usage": UsageLedger.payload(of: aggregate)])

        case ("GET", "/v1/orchestrator/usage/analytics"),
             ("GET", "/v1/orchestrator/usage/analytics.csv"),
             ("GET", "/v1/orchestrator/usage/analytics.json"):
            let parsed = UsageQueryService.parse(request.query,
                                                 repeatedKeys: request.repeatedQueryKeys)
            guard let query = parsed.query else {
                return .error(400, "bad_request", parsed.error ?? "Invalid usage query.")
            }
            let service = UsageQueryService()
            let result = service.query(query)
            if request.path.hasSuffix(".csv") {
                guard !result.scanTruncated else {
                    return .error(413, "export_too_large",
                                  "The matching export exceeds \(UsageQueryService.maxScannedRows) rows; narrow the range.")
                }
                let csv = service.exportCSV(result)
                return Response(status: 200,
                                headers: ["Content-Type": "text/csv; charset=utf-8",
                                          "Content-Disposition":
                                            "attachment; filename=\"clawdline-usage.csv\""],
                                body: Data(csv.utf8))
            }
            if request.path.hasSuffix(".json") {
                guard !result.scanTruncated else {
                    return .error(413, "export_too_large",
                                  "The matching export exceeds \(UsageQueryService.maxScannedRows) rows; narrow the range.")
                }
                do {
                    return Response(status: 200,
                                    headers: ["Content-Type": "application/json; charset=utf-8",
                                              "Content-Disposition":
                                                "attachment; filename=\"clawdline-usage.json\""],
                                    body: try service.exportJSON(result))
                } catch {
                    return .error(500, "json_serialization_failed",
                                  "The lossless usage export could not be serialized.")
                }
            }
            return .json(["usage": result.payload])

        case ("GET", "/v1/orchestrator/root-assignments"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Reading Root Assignments needs the orchestrator token.")
            }
            return .json(["root_assignments": Orchestrator.rootAssignmentRecords(),
                          "at": Int(Date().timeIntervalSince1970)])

        case ("GET", let path) where path.hasPrefix("/v1/orchestrator/root-assignments/"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Reading a Root Assignment needs the orchestrator token.")
            }
            let raw = String(path.dropFirst("/v1/orchestrator/root-assignments/".count))
            guard !raw.isEmpty, !raw.contains("/"),
                  let record = Orchestrator.rootAssignmentRecord(
                    id: raw.removingPercentEncoding ?? raw) else {
                return .error(404, "not_found", "No Root Assignment named that")
            }
            return .json(["root_assignment": record])

        case ("GET", "/v1/orchestrator/tasks"):
            return .json(["tasks": Orchestrator.records(),
                          "at": Int(Date().timeIntervalSince1970)])

        case ("GET", "/v1/orchestrator/graphs"):
            return .json(["graphs": Orchestrator.graphRecords(),
                          "at": Int(Date().timeIntervalSince1970)])

        case ("POST", "/v1/orchestrator/maintenance/restart"):
            guard orchestratorAuthed else { return .error(403, "forbidden",
                "Beginning restart maintenance needs the orchestrator token.") }
            guard let body = (try? JSONSerialization.jsonObject(with: request.body))
                    as? [String: Any], Set(body.keys) == ["request_id"],
                  let requestID = body["request_id"] as? String else { return .error(
                    400, "bad_restart_request", "The closed restart schema requires only request_id.") }
            return beginRestartMaintenance(requestID: requestID)

        case ("GET", "/v1/orchestrator/maintenance/restart"):
            guard orchestratorAuthed else { return .error(403, "forbidden",
                "Reading restart maintenance needs the orchestrator token.") }
            return pollRestartMaintenance()

        case ("DELETE", "/v1/orchestrator/maintenance/restart"):
            guard orchestratorAuthed else { return .error(403, "forbidden",
                "Aborting restart maintenance needs the orchestrator token.") }
            guard let body = (try? JSONSerialization.jsonObject(with: request.body))
                    as? [String: Any], Set(body.keys) == ["request_id"],
                  let requestID = body["request_id"] as? String else { return .error(
                    400, "bad_restart_request", "The closed restart schema requires only request_id.") }
            return abortRestartMaintenance(requestID: requestID)

        case ("GET", "/v1/orchestrator/completions"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Reading completion delivery needs the orchestrator token.")
            }
            guard Set(request.query.keys).isSubset(of: ["pending"]),
                  request.repeatedQueryKeys.isEmpty else {
                return .error(400, "bad_request",
                              "The closed completion query accepts one pending=true|false|1|0.")
            }
            let pending: Bool
            switch request.query["pending"] {
            case nil, "false", "0": pending = false
            case "true", "1": pending = true
            default:
                return .error(400, "bad_request",
                              "The closed completion query accepts one pending=true|false|1|0.")
            }
            return .json(["completions": Orchestrator.completionRecords(pendingOnly: pending),
                          "pending_only": pending,
                          "at": Int(Date().timeIntervalSince1970)])

        case ("POST", "/v1/orchestrator/completions/reconcile"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Reconciling completion delivery needs the orchestrator token.")
            }
            guard !request.body.isEmpty,
                  let decoded = try? JSONSerialization.jsonObject(with: request.body),
                  let body = decoded as? [String: Any] else {
                return .error(400, "bad_request",
                              "The reconcile body must be one JSON object.")
            }
            guard Set(body.keys).isSubset(of: ["task_id", "include_dead_letter"]),
                  (body["task_id"] == nil || body["task_id"] is String) else {
                return .error(400, "bad_request",
                              "Only task_id and boolean include_dead_letter are accepted.")
            }
            let includeDeadLetters: Bool
            if let raw = body["include_dead_letter"] {
                guard let number = raw as? NSNumber,
                      CFGetTypeID(number) == CFBooleanGetTypeID() else {
                    return .error(400, "bad_request",
                                  "Only task_id and boolean include_dead_letter are accepted.")
                }
                includeDeadLetters = number.boolValue
            } else {
                includeDeadLetters = false
            }
            return answer(Orchestrator.reconcileCompletions(
                taskID: body["task_id"] as? String,
                includeDeadLetters: includeDeadLetters))

        case ("POST", let path) where path.hasPrefix("/v1/orchestrator/tasks/")
            && path.hasSuffix("/completion/ack"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden",
                              "Acknowledging completion needs the orchestrator token.")
            }
            let id = String(path.dropFirst("/v1/orchestrator/tasks/".count)
                .dropLast("/completion/ack".count))
            guard let body = try? JSONSerialization.jsonObject(with: request.body)
                    as? [String: Any], Set(body.keys) == ["notice_id"],
                  let noticeID = body["notice_id"] as? String else {
                return .error(400, "bad_request",
                              "The closed ACK schema requires only notice_id.")
            }
            return answer(Orchestrator.acknowledgeCompletion(
                taskID: id.removingPercentEncoding ?? id, noticeID: noticeID))

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
            && path.hasSuffix("/notify"):
            let id = String(path.dropFirst("/v1/orchestrator/tasks/".count)
                .dropLast("/notify".count))
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            let secret = request.headers["x-clawdline-task-secret"]
                ?? (body["secret"] as? String) ?? ""
            return answer(Orchestrator.agentNotify(taskID: id.removingPercentEncoding ?? id,
                                                   secret: secret,
                                                   title: body["title"] as? String ?? "",
                                                   body: body["body"] as? String ?? ""))

        case ("POST", let path) where path.hasPrefix("/v1/orchestrator/tasks/")
            && path.hasSuffix("/landing"):
            let id = String(path.dropFirst("/v1/orchestrator/tasks/".count)
                .dropLast("/landing".count))
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            let secret = request.headers["x-clawdline-task-secret"] ?? ""
            return answer(Orchestrator.updateLanding(taskID: id.removingPercentEncoding ?? id,
                                                      secret: secret,
                                                      orchestratorToken: request.headers[
                                                        "x-clawdline-orchestrator"],
                                                      raw: body))

        // One sentence about what this task is actually doing, from the task itself. The
        // per-task secret is the credential, like `complete` and `notify`: a child has it, it
        // names exactly one task, and nothing here can reach another one's record.
        case ("POST", let path) where path.hasPrefix("/v1/orchestrator/tasks/")
            && path.hasSuffix("/progress"):
            let id = String(path.dropFirst("/v1/orchestrator/tasks/".count)
                .dropLast("/progress".count))
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            let secret = request.headers["x-clawdline-task-secret"] ?? ""
            return answer(Orchestrator.recordProgress(taskID: id.removingPercentEncoding ?? id,
                                                      secret: secret,
                                                      note: body["note"] as? String ?? ""))

        case ("POST", let path) where path.hasPrefix("/v1/orchestrator/tasks/")
            && path.hasSuffix("/cancel"):
            let id = String(path.dropFirst("/v1/orchestrator/tasks/".count)
                .dropLast("/cancel".count))
            let cleaned = id.removingPercentEncoding ?? id
            // The local credential may cancel outright; a paired device goes through the same
            // three gates every other write does.
            if orchestratorAuthed { return answer(Orchestrator.cancel(taskID: cleaned)) }
            return writing(request) { _ in answer(Orchestrator.cancel(taskID: cleaned)) }

        // Handing back claims before a task ends, so a `409 workspace_busy` blocked on them can
        // retry immediately — see docs/orchestrator.md#releasing-claims-early. Local-credential
        // only, like dispatch itself: this changes what another root may claim, not the phone's
        // own reach.
        case ("POST", let path) where path.hasPrefix("/v1/orchestrator/tasks/")
            && path.hasSuffix("/claims/release"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden", "Releasing claims needs the orchestrator token.")
            }
            let id = String(path.dropFirst("/v1/orchestrator/tasks/".count)
                .dropLast("/claims/release".count))
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            let paths = body["paths"] as? [String] ?? []
            return answer(Orchestrator.releaseClaims(taskID: id.removingPercentEncoding ?? id,
                                                      paths: paths))

        // Retrying a tab that never opened, without making the root write the task out again.
        // Local-credential only, like dispatch itself: this opens a session.
        case ("POST", let path) where path.hasPrefix("/v1/orchestrator/tasks/")
            && path.hasSuffix("/respawn"):
            guard orchestratorAuthed else {
                return .error(403, "forbidden", "Respawning a task needs the orchestrator token.")
            }
            let id = String(path.dropFirst("/v1/orchestrator/tasks/".count)
                .dropLast("/respawn".count))
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            return answer(Orchestrator.respawn(taskID: id.removingPercentEncoding ?? id,
                                                secret: body["secret"] as? String))

        // Before the single-task read below, which would otherwise swallow this path.
        case ("GET", let path) where path.hasPrefix("/v1/orchestrator/tasks/")
            && path.hasSuffix("/inflight"):
            let id = String(path.dropFirst("/v1/orchestrator/tasks/".count)
                .dropLast("/inflight".count))
            return answer(Orchestrator.inflightReply(
                taskID: id.removingPercentEncoding ?? id,
                secret: request.headers["x-clawdline-task-secret"] ?? ""))

        case ("GET", let path) where path.hasPrefix("/v1/orchestrator/tasks/"):
            let id = String(path.dropFirst("/v1/orchestrator/tasks/".count))
            guard let record = Orchestrator.record(id: id.removingPercentEncoding ?? id) else {
                return .error(404, "not_found", "No task named that")
            }
            return .json(["task": record])

        // Answering a menu, which is a different act from typing — see `Targets.answer`.
        //
        // Write-level and allowlisted twice over: only `1`–`9`, Tab, and the exact back-tab
        // sequence Claude Code uses for permission modes reach a tty, and the allowlist that
        // matters is the one in `Targets`, not this parse. A route that took "any key" would be
        // a way to write escape sequences into somebody's terminal from a phone, and no amount
        // of validating the *question* would make that not true.
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
            return writing(request) { body in
                guard let session = self.session(withID: id.removingPercentEncoding ?? id) else {
                    return .error(404, "not_found", "No session named that")
                }
                // The CAS proof, when the caller asked for one. A client copies the opaque
                // `closeability.version` it drew and hands it back here; the broker recomputes
                // at its end of the same press and refuses anything that is not still `safe`
                // under that exact token.
                //
                // **`accept_loss` is not an answer to this gate.** That flag is the human
                // override for a positive victim list somebody was shown — and a stale,
                // ambiguous or unattested projection has no victim list to accept. Overriding
                // it would mean accepting a loss nobody could enumerate.
                //
                // Omitting the field leaves every existing client exactly where it was: the
                // `lost_if_closed` gate below is unchanged and is still the whole contract.
                switch Self.closeProofRequest(body["expected_closeability_version"]) {
                case .malformed:
                    return .error(400, "bad_request",
                                  "expected_closeability_version must be a non-empty string.")
                case .notRequested:
                    break
                case .expecting(let expected):
                    let projected = self.closeability(of: session)
                    if !Self.closeIsProven(projected, expected: expected,
                                           acceptLoss: body["accept_loss"] as? Bool == true) {
                        RemoteAuth.audit("session.end.refused",
                                         ["id": session.id,
                                          "closeability": projected.state.rawValue])
                        return .error(409, "close_not_proven",
                                      "This close asked to be proven and the broker cannot "
                                          + "prove it. Read closeability.reasons, clear or "
                                          + "attest them, and repeat with the version the "
                                          + "next projection carries.",
                                      extra: ["closeability": projected.wire])
                    }
                }
                // `lost_if_closed`, computed at the moment somebody presses close — the one
                // moment a list of live children and stranded waiters can still change the
                // outcome. A caller that has shown the list repeats the request with
                // `accept_loss: true`; a close with nothing at stake is unchanged.
                let lost = Orchestrator.lostIfClosed(root: session)
                if !lost.isEmpty, body["accept_loss"] as? Bool != true {
                    return .error(409, "would_lose_work",
                                  "Closing this session cancels work still in flight. Show the "
                                      + "lost list, then repeat with accept_loss: true.",
                                  extra: ["lost": lost])
                }
                RemoteAuth.audit("session.end", ["id": session.id,
                                                 "lost": String(lost.count)])
                // **The children first, while the root is still there to be recognised.** A task
                // is matched to its root by the session id in that session's hook note, and the
                // note is found through the tty of the tab this line is about to close — after
                // `end` there is nothing left to match, and the children would run on as orphans.
                // Nothing happens here for a session that dispatched nothing, which is most of
                // them; the cascade and its reasoning live in `Orchestrator`.
                Orchestrator.cancelChildren(ofRoot: session)
                let endFailure: String?
                if let suppliedEnd = Self.sessionEndForTesting {
                    endFailure = suppliedEnd(session)
                } else {
                    endFailure = Targets.end(session)
                }
                if let failure = endFailure {
                    return Self.terminalFailure(failure, backend: session.backend)
                }
                DispatchQueue.main.async { SessionWatch.shared.nudge() }
                return .json(["ok": true])
            }

        case ("POST", let path) where path.hasSuffix("/title")
            && path.hasPrefix("/v1/sessions/"):
            let id = String(path.dropFirst("/v1/sessions/".count).dropLast("/title".count))
            return writing(request) { body in
                guard let raw = body["title"] as? String else {
                    return .error(400, "bad_request", "title must be a string.")
                }
                let title = Config.normalizedSessionTitle(raw)
                guard title?.count ?? 0 <= Config.sessionTitleLimit else {
                    return .error(400, "bad_request",
                                  "title must be at most \(Config.sessionTitleLimit) characters.")
                }
                guard let session = self.session(withID: id.removingPercentEncoding ?? id) else {
                    return .error(404, "not_found", "No session named that")
                }

                let config = Config.shared
                _ = config.setSessionTitle(raw, for: session)
                // The answer says whether the name is durable, because that is the promise the
                // rest of this route is built on: a busy Claude is deliberately not queued for a
                // later `/rename`, and the reason given is that the local name survives anyway.
                // A failed write still leaves the name in memory and on every surface that draws
                // one, so this is a 200 that tells the truth rather than a 500 that undoes what
                // the person can already see.
                let durable = config.save()

                let state = titleState(of: session.id)
                let action = SessionTitleSync.confirmed(
                    SessionTitleSync.action(
                        assistant: session.assistant, state: state, clearing: title == nil,
                        codexThreadID: CodexNaming.shared.threadID(for: session)),
                    showingMenu: { Targets.isChoosing(session) })
                var downstream = "local_only"
                var synced = false
                switch action {
                case .localOnly:
                    // Clearing has to reach Codex's display cache as well, or it clears nothing
                    // there: the name a person typed was remembered on the way in and
                    // `displayLabel` would go on finding it. The thread keeps that name — see
                    // `CodexNaming.forget(target:)` and `docs/api.md`.
                    if title == nil { CodexNaming.shared.forget(target: session) }
                case .busy:
                    downstream = "busy"
                case .unavailable:
                    downstream = "unavailable"
                case .renameClaude:
                    if let failure = Targets.send("/rename \(title ?? "")", to: session) {
                        downstream = "failed"
                        Log.write("session title: Claude rename failed — \(failure)")
                    } else {
                        downstream = "synced"
                        synced = true
                    }
                case .renameCodex(let threadID):
                    CodexNaming.shared.name(title ?? "", thread: threadID, target: session,
                                            replacingExisting: true)
                    downstream = "queued"
                }
                RemoteAuth.audit("session.title", ["id": session.id,
                                                    "cleared": title == nil ? "1" : "0",
                                                    "downstream": downstream])
                DispatchQueue.main.async { SessionWatch.shared.labelsDidChange() }
                return .json(["ok": true, "title": title ?? "",
                              "display_title": session.observedDisplayLabel,
                              "local_applied": durable,
                              "downstream": downstream, "downstream_synced": synced])
            }

        // Stopping one of a session's background commands.
        //
        // **The second route on this server that destroys something**, after `/end`, and behind
        // the same gate for the same reason: a build somebody is forty minutes into dies on a
        // mis-tap, and nothing brings it back. What makes it defensible at all is that the app
        // proves which process it is signalling before it signals — see `Shells.stop(_:of:)` —
        // so `409` here means "could not identify it", never "signalled something and hoped".
        case ("POST", let path) where path.hasSuffix("/kill") && path.hasPrefix("/v1/sessions/")
            && path.contains("/shells/"):
            let rest = String(path.dropFirst("/v1/sessions/".count).dropLast("/kill".count))
            let parts = rest.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
            guard parts.count == 3, parts[1] == "shells" else {
                return .error(404, "not_found", "No such route")
            }
            let id = parts[0].removingPercentEncoding ?? parts[0]
            let shell = parts[2].removingPercentEncoding ?? parts[2]
            return writing(request) { _ in
                guard let session = self.session(withID: id) else {
                    return .error(404, "not_found", "No session named that")
                }
                RemoteAuth.audit("shell.kill", ["id": session.id, "shell": shell])
                switch Shells.stop(shell, of: session) {
                case .stopped:
                    DispatchQueue.main.async { SessionWatch.shared.nudge() }
                    return .json(["ok": true])
                case .gone:
                    return .error(404, "not_found", "That command is not running")
                case .unidentified:
                    return .error(409, "unidentified",
                                  "Could not tell which process that command is, so nothing was "
                                  + "signalled. Stop it on the Mac with /tasks.")
                }
            }

        case ("POST", let path) where path.hasSuffix("/key") && path.hasPrefix("/v1/sessions/"):
            let id = String(path.dropFirst("/v1/sessions/".count).dropLast("/key".count))
            return writing(request) { body in
                // **Parsed before the session is looked up.** Not for secrecy — a well-formed key
                // still tells you whether a session exists — but because the allowlist is the
                // thing this route is for, and a check that runs after two other steps is a check
                // somebody will later move.
                let key = (body["key"] as? String) ?? ""
                let bytes: [UInt8]
                // **`submit` is a name, not a key**, because the button it presses has no key: a
                // multi-select's rows are toggled by their digits and nothing is sent until the
                // button under them takes the caret. Naming the act rather than the keystroke
                // keeps the walk that gets there on this side, where it can read the screen back
                // before it commits — see `Targets.submitMenu(on:)`.
                if key == "submit" {
                    bytes = []
                } else if key == "tab" {
                    bytes = [0x09]
                } else if key == "shift+tab" {
                    bytes = [0x1b, 0x5b, 0x5a]
                } else if key.count == 1, let c = key.unicodeScalars.first,
                          ("1"..."9").contains(key) {
                    bytes = [UInt8(c.value)]
                } else {
                    return .error(400, "bad_request",
                                  "key must be \"1\"…\"9\", \"tab\", \"shift+tab\" or \"submit\".")
                }
                guard let session = self.session(withID: id.removingPercentEncoding ?? id) else {
                    return .error(404, "not_found", "No session named that")
                }
                let failed = key == "submit" ? Targets.submitMenu(on: session)
                                             : Targets.answer(bytes, to: session)
                if let failure = failed {
                    return Self.terminalFailure(failure, backend: session.backend)
                }
                RemoteAuth.audit("session.key", ["id": session.id, "key": key])
                // A reading now would still show the menu — the terminal has not repainted yet.
                DispatchQueue.main.async { SessionWatch.shared.nudge() }
                return .json(["ok": true])
            }

        case ("POST", let path) where path.hasSuffix("/focus") && path.hasPrefix("/v1/sessions/"):
            let id = String(path.dropFirst("/v1/sessions/".count).dropLast("/focus".count))
            return writing(request) { _ in
                guard let session = self.session(withID: id.removingPercentEncoding ?? id) else {
                    return .error(404, "not_found", "No session named that")
                }
                // This route is admitted to the bounded terminal worker before dispatch reaches
                // here. Do not hop back to main: iTerm focus is an Apple Event and a modal must
                // not freeze SessionWatch, the orchestrator beat, SSE, or health responses.
                if let failure = Targets.reveal(session) {
                    return Self.terminalFailure(failure)
                }
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
            return RemotePage.page(for: request)

        case ("GET", "/manifest.webmanifest"):
            return RemotePage.manifest()

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
            return RemotePage.asset(String(path.dropFirst("/app/".count)))

        case ("GET", "/sw.js"):
            return RemotePage.serviceWorker()

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

        case ("GET", let path) where path.hasPrefix("/project-") && path.hasSuffix(".png"):
            // `/project-192-<packed>.png`. The size first, because the packed grid is base64url
            // and contains hyphens of its own — splitting from the front is the only way round
            // that never has to guess where one field ends.
            let body = path.dropFirst("/project-".count).dropLast(".png".count)
            guard let dash = body.firstIndex(of: "-"), let want = Int(body[body.startIndex..<dash]),
                  [64, 96, 128, 192, 256].contains(want),
                  let cells = RemoteIcon.unpack(String(body[body.index(after: dash)...])),
                  let data = RemoteIcon.project(cells: cells, size: want) else {
                return .error(404, "not_found", "No mark that size")
            }
            return Response(status: 200,
                            headers: ["Content-Type": "image/png",
                                      // A year, and `immutable`, because the URL is the content:
                                      // this answer can never become the wrong answer.
                                      "Cache-Control": "public, max-age=31536000, immutable"],
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
        if case .verifiedCloud(let sender) = request.source {
            // CloudTransport already verified this sender against the locally pinned device key,
            // decrypted it, and replay-checked its sequence. This is an in-process source only;
            // the HTTP parser cannot construct it.
            return .allowed(device: "cloud:\(sender)", caps: [.read, .send])
        }
        return RemoteAuth.verify(bearer: bearer(request))
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
        if case .verifiedCloud = request.source { return nil }
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

    /// A cookie is sent by the browser whether or not the page asking wanted it to be, so a
    /// mutating route additionally has to be sure the request came from our own page. `Origin` is
    /// set by the browser and cannot be forged by script — and the JSON content type is the second
    /// half of it, because the shapes a cross-site form can send do not include one. Reads are
    /// exempt: they are already gated by the token.
    ///
    /// A function rather than four lines inside `dispatch`, because there is now a mutating route
    /// that never reaches `dispatch`: dictation is taken a step earlier, and a write that answered
    /// without this check would be a hole in exactly the shape this closes.
    func writeOriginRefusal(_ request: Request) -> Response? {
        if case .verifiedCloud = request.source { return nil }
        guard request.method != "GET", let origin = request.headers["origin"], !isOurs(origin)
        else { return nil }
        return .error(403, "forbidden", "That request did not come from this page.")
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

    /// A terminal send whose HTTP request is still waiting for its one final answer. The array is
    /// deliberate: a concurrent retry with the same key joins this operation instead of entering
    /// iTerm2 a second time. This dictionary and the completed idempotency table are touched only
    /// on `queue`; admission for both HTTP and internal commands has its own small lock.
    private var terminalPending: [String: [(Response) -> Void]] = [:]
    private static let terminalWorkerKey = DispatchSpecificKey<Bool>()
    private lazy var terminalQueue: DispatchQueue = {
        let queue = DispatchQueue(label: "com.tsunamiworks.clawdline.remote.terminal")
        queue.setSpecific(key: Self.terminalWorkerKey, value: true)
        return queue
    }()
    let terminalAdmissionLock = NSLock()
    var terminalOutstanding = 0
    var terminalOutstandingByChannel: [String: Int] = [:]
    var terminalMaintenanceRequestID: String?
    /// Touched only on the serial terminal queue. It lets nested inline work inherit an outer
    /// reservation without double-counting that same terminal while still accounting a newly
    /// discovered child/coordination recipient.
    private var terminalActiveChannels: Set<String> = []
    static let terminalDepth = 8
    static let terminalChannelDepth = 2

    /// Production's asynchronous `/send` path. Every decision stays on the HTTP queue; only the
    /// terminal handoff crosses to `terminalQueue`, and settlement crosses back before touching
    /// idempotency or delivering any response.
    private func sendTerminal(_ request: Request, deliver: @escaping (Response) -> Void) {
        if let refusal = crossOriginRefusal(request) {
            deliver(refusal); return
        }
        if case .denied = permission(for: request) {
            deliver(.error(401, "unauthorized", "This needs a paired device.")); return
        }
        if let refusal = writeOriginRefusal(request) {
            deliver(refusal); return
        }

        let device: String, key: String
        switch writeGate(request) {
        case .refused(let response), .replay(let response):
            deliver(response); return
        case .go(let allowed, let filed):
            device = allowed
            key = filed
        }

        // The reservation precedes every terminal observation, including the menu capture. A
        // retry arriving while that capture or send is blocked joins the first request and cannot
        // press Return a second time.
        if terminalPending[key] != nil {
            terminalPending[key, default: []].append(deliver)
            return
        }
        if let refusal = terminalMaintenanceRefusal() {
            deliver(refusal); return
        }

        let parsed = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
        let text = (parsed["text"] as? String) ?? ""
        let images = (parsed["images"] as? [String]) ?? []
        guard !text.isEmpty || !images.isEmpty else {
            let response = Response.error(400, "bad_request", "That needs some text or an image.")
            remember(response, under: key, for: request, by: device)
            deliver(response)
            return
        }
        let id = String(request.path.dropFirst("/v1/sessions/".count).dropLast("/send".count))
        let terminalPublication = SessionWatch.shared.publishedInventory()
        let suppliedInventory = Self.sessionPayloadForTesting
        let terminalTargets = suppliedInventory?.0 ?? terminalPublication.targets
        let terminalStates = suppliedInventory?.1 ?? terminalPublication.states
        guard let session = Self.session(
            withID: id.removingPercentEncoding ?? id, among: terminalTargets) else {
            let response = Response.error(404, "not_found", "No session named that")
            remember(response, under: key, for: request, by: device)
            deliver(response)
            return
        }

        // Refuse before reservation/admission and before image files are materialised. A request
        // that cannot run must not occupy a bounded slot or wait behind a blocked command. tmux
        // remains independent of iTerm's circuit.
        if let attention = ITerm.automationAttention, session.backend == .iterm {
            let response = Self.terminalFailure(attention, backend: .iterm)
            remember(response, under: key, for: request, by: device)
            deliver(response)
            return
        }

        var pieces: [Drop.Piece]?
        var stored: [String] = []
        if !images.isEmpty {
            let made = Self.pieces(text: text, images: images)
            guard made.pieces.contains(where: {
                if case .image = $0 { return true }; return false
            }) else {
                let response = Response.error(400, "bad_request",
                                              "None of those were images I could read.")
                remember(response, under: key, for: request, by: device)
                deliver(response)
                return
            }
            pieces = made.pieces
            stored = made.stored
        }

        terminalPending[key] = [deliver]
        let shouldCheckMenu = terminalStates[session.id] == .waiting

        let admitted = enqueueTerminalCommand(channel: session.id) { [weak self] in
            guard let self else { return }
            let response: Response
            if let attention = ITerm.automationAttention, session.backend == .iterm {
                Self.finishUploads(stored, sent: false)
                response = Self.terminalFailure(attention, backend: .iterm)
            } else if shouldCheckMenu && Targets.isChoosing(session) {
                Self.finishUploads(stored, sent: false)
                response = .error(409, "showing_a_menu",
                                  "That session is showing a menu. Sending text would confirm "
                                  + "whichever option is highlighted rather than typing. "
                                  + "Answer it with POST /v1/sessions/<id>/key.")
            } else {
                // This timestamp shares the transcript row's Mac clock and is captured before the
                // terminal handoff. The row may be visible while a slow osascript round trip is
                // still running; a completion-only timestamp would put it outside reconciliation.
                let acceptedAt = Int(Date().timeIntervalSince1970)
                let problem: String?
                if let pieces {
                    problem = Targets.send(pieces, to: session)
                    Self.finishUploads(stored, sent: problem == nil)
                } else if let seam = Self.terminalSendForTesting {
                    problem = seam(text, session)
                } else {
                    problem = Targets.send(text, to: session)
                }
                RemoteAuth.audit("session.send", ["id": session.id, "tty": session.tty,
                                                   "chars": "\(text.count)",
                                                   "images": "\(images.count)",
                                                   "ok": problem == nil ? "1" : "0"])
                response = problem.map { Self.terminalFailure($0, backend: session.backend) }
                    ?? .json(["ok": true, "accepted_at": acceptedAt,
                              "at": Int(Date().timeIntervalSince1970)])
            }

            self.queue.async {
                let waiters = self.terminalPending.removeValue(forKey: key) ?? []
                self.remember(response, under: key, for: request, by: device)
                for waiter in waiters { waiter(response) }
            }
        }
        if !admitted {
            Self.finishUploads(stored, sent: false)
            terminalPending.removeValue(forKey: key)
            deliver(terminalMaintenanceRefusal() ?? .error(
                429, "busy",
                "This Mac already has \(Self.terminalDepth) terminal commands in hand. "
                    + "Try again after they drain."))
        }
    }

    private static func terminalFailure(_ problem: String, backend: Backend) -> Response {
        if backend == .iterm, let attention = ITerm.automationAttention,
           problem == attention {
            return .error(502, "iterm_attention_required", attention,
                          extra: ["app": "iTerm2", "action": "answer_dialog"])
        }
        return .error(502, "terminal_io_failed",
                      "The terminal command did not complete: \(problem)")
    }

    private static func terminalFailure(_ failure: TerminalFailure) -> Response {
        if failure.kind == .iTermAttention {
            return .error(502, "iterm_attention_required", failure.message,
                          extra: ["app": "iTerm2", "action": "answer_dialog"])
        }
        return .error(502, "terminal_io_failed",
                      "The terminal command did not complete: \(failure.message)")
    }

    /// Asynchronous wrapper for `/key`, `/end` and `/start`. Validation, reservation and
    /// idempotency stay on the server queue; only the existing route body enters the bounded
    /// terminal worker. The queue-specific marker lets `writing` skip duplicate bookkeeping.
    private func terminalMutation(_ request: Request, deliver: @escaping (Response) -> Void) {
        if let refusal = crossOriginRefusal(request) { deliver(refusal); return }
        if case .denied = permission(for: request) {
            deliver(.error(401, "unauthorized", "This needs a paired device.")); return
        }
        if let refusal = writeOriginRefusal(request) { deliver(refusal); return }

        let device: String, key: String
        switch writeGate(request) {
        case .refused(let response), .replay(let response): deliver(response); return
        case .go(let allowed, let filed): device = allowed; key = filed
        }
        if terminalPending[key] != nil {
            terminalPending[key, default: []].append(deliver)
            return
        }
        if let refusal = terminalMaintenanceRefusal() {
            deliver(refusal); return
        }

        // A session-bound iTerm command can be refused before admission. Start is deliberately
        // allowed through: when iTerm2 is stopped, newtab is the operation that launches it.
        if request.path.hasPrefix("/v1/sessions/") {
            let rest = request.path.dropFirst("/v1/sessions/".count)
            let rawID = rest.split(separator: "/", maxSplits: 1).first.map(String.init) ?? ""
            if let session = session(withID: rawID.removingPercentEncoding ?? rawID),
               session.backend == .iterm, let attention = ITerm.automationAttention {
                let response = Self.terminalFailure(TerminalFailure(
                    kind: .iTermAttention, message: attention))
                remember(response, under: key, for: request, by: device)
                deliver(response)
                return
            }
        }

        terminalPending[key] = [deliver]
        let admitted = enqueueTerminalCommand(channels: Self.terminalChannels(for: request)) { [weak self] in
            guard let self else { return }
            let response = Self.terminalRouteForTesting?(request) ?? self.route(request)
            self.queue.async {
                let waiters = self.terminalPending.removeValue(forKey: key) ?? []
                self.remember(response, under: key, for: request, by: device)
                for waiter in waiters { waiter(response) }
            }
        }
        if !admitted {
            terminalPending.removeValue(forKey: key)
            deliver(terminalMaintenanceRefusal() ?? .error(
                429, "busy",
                "This Mac already has \(Self.terminalDepth) terminal commands in hand. "
                    + "Try again after they drain."))
        }
    }

    private func unfiledTerminalMutation(_ request: Request,
                                         deliver: @escaping (Response) -> Void) {
        if let refusal = terminalMaintenanceRefusal() {
            deliver(refusal); return
        }
        let admitted = enqueueTerminalCommand(channels: Self.terminalChannels(for: request)) { [weak self] in
            guard let self else { return }
            let response = self.route(request)
            self.queue.async { deliver(response) }
        }
        if !admitted {
            deliver(terminalMaintenanceRefusal() ?? .error(
                429, "busy",
                "This Mac already has \(Self.terminalDepth) terminal commands in hand. "
                    + "Try again after they drain."))
        }
    }

    private static func terminalChannels(for request: Request) -> [String] {
        if request.path.hasPrefix("/v1/sessions/") {
            let rest = request.path.dropFirst("/v1/sessions/".count)
            guard let first = rest.split(separator: "/", maxSplits: 1).first else { return [] }
            return [String(first).removingPercentEncoding ?? String(first)]
        }
        if request.path.hasPrefix("/v1/places/") {
            let rest = request.path.dropFirst("/v1/places/".count)
            guard let first = rest.split(separator: "/", maxSplits: 1).first else { return [] }
            return ["start:" + String(first)]
        }
        if request.path == "/v1/orchestrator/waits" {
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
            return (body?["owner_session_id"] as? String).map { [$0] } ?? []
        }
        if request.path == "/v1/orchestrator/landing-queue/advance" {
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
            let project = body?["project"] as? String ?? body?["project_dir"] as? String ?? ""
            return ["landing-queue:" + project]
        }
        if request.path == "/v1/orchestrator/root-assignments" {
            let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
            let requestID = body?["request_id"] as? String
                ?? request.headers["idempotency-key"] ?? "unknown"
            return ["root-assignment:" + requestID]
        }
        if request.path.hasPrefix("/v1/orchestrator/waits/")
            && request.path.hasSuffix("/release") {
            let id = String(request.path.dropFirst("/v1/orchestrator/waits/".count)
                .dropLast("/release".count)).removingPercentEncoding ?? ""
            return Orchestrator.coordinationWaitRecords().compactMap { row in
                guard row["id"] as? String == id else { return nil }
                return row["waiter_session_id"] as? String
            }
        }
        if request.path.hasPrefix("/v1/orchestrator/schedules/")
            && request.path.hasSuffix("/run") {
            let id = request.path.dropFirst("/v1/orchestrator/schedules/".count)
                .dropLast("/run".count)
            return ["schedule:" + String(id)]
        }
        if request.path.hasPrefix("/v1/orchestrator/tasks/") {
            let id = String(request.path.dropFirst("/v1/orchestrator/tasks/".count))
            if let child = Orchestrator.record(id: id)?["child"] as? [String: Any],
               let terminal = child["terminalId"] as? String {
                return [terminal]
            }
            return ["task:" + id]
        }
        return []
    }

    static func terminalChannelsForTesting(_ request: Request) -> [String] {
        terminalChannels(for: request)
    }

    /// Deterministic queue seams. They submit to the production queues rather than imitating
    /// them, so a blocked test command proves the actual server and reading lanes still move.
    func sendTerminalForTesting(_ request: Request,
                                completion: @escaping (Response) -> Void) {
        queue.async { self.sendTerminal(request, deliver: completion) }
    }

    func terminalMutationForTesting(_ request: Request,
                                    completion: @escaping (Response) -> Void) {
        queue.async { self.terminalMutation(request, deliver: completion) }
    }

    func routeOnServerQueueForTesting(_ request: Request,
                                      completion: @escaping (Response) -> Void) {
        queue.async { completion(self.route(request)) }
    }

    func heartbeatTurnForTesting(_ completion: @escaping () -> Void) {
        queue.async(execute: completion)
    }

    func readingTurnForTesting(_ completion: @escaping () -> Void) {
        readingQueue.async(execute: completion)
    }

    /// What the three gates in front of a mutating route decided.
    ///
    /// A verdict rather than a call, because one of the routes behind this gate does not finish in
    /// the same breath it starts: dictation hands its work to another queue and comes back a
    /// second and a half later, so it needs the decision as a value it can carry rather than as a
    /// closure it can be called inside. The rules are the ones `writing` used to keep to itself,
    /// and they moved here so that there is one copy of them and not two.
    enum WriteGate {
        /// One of the gates said no, and this is what the device is told.
        case refused(Response)
        /// This key was answered inside the last ten minutes, and the old answer is the answer.
        case replay(Response)
        /// Every gate passed. `device` is for the log; `key` is what the answer will be filed under.
        case go(device: String, key: String)
    }

    /// Everything a mutating route has to be true before it happens, in one place.
    ///
    /// Three separate gates, and they are separate on purpose: the switch is a decision the owner
    /// of the Mac made, the capability is a decision about *this* device, and the idempotency key
    /// is about the network. A route that forgot one of them would be a route that quietly did
    /// something the other two were meant to prevent.
    ///
    /// Asked on the server's own queue and nowhere else — it only reads, but what it reads is
    /// written by `remember` on that queue, and that is the whole of the locking here.
    private func writeGate(_ request: Request) -> WriteGate {
        if case .http = request.source {
            guard Config.shared.remoteWrite else {
                return .refused(.error(403, "write_disabled",
                                       "Sending is switched off. Settings → Remote turns it on, and it "
                                       + "is off by default because typing into a session runs code on "
                                       + "this Mac."))
            }
        }
        guard case .allowed(let device, let caps) = permission(for: request), caps.contains(.send) else {
            return .refused(.error(403, "forbidden", "This device may read, and not send."))
        }
        // **A retried POST must not be a second prompt.** Phones change networks mid-request and
        // clients retry; typing the same instruction into somebody's agent twice is not something
        // that can be taken back, so the key is required rather than merely honoured.
        guard let key = request.headers["idempotency-key"], !key.isEmpty else {
            return .refused(.error(400, "bad_request", "That needs an Idempotency-Key header."))
        }
        if let seen = idempotent[key], Date().timeIntervalSince(seen.at) < 600 {
            return .replay(seen.response)
        }
        return .go(device: device, key: key)
    }

    /// File an answer under its key, and write the line that says what happened.
    ///
    /// The sweep is here rather than on a timer because the only moment this table can grow is
    /// the moment something is written into it, so that is the moment to throw away what has
    /// expired.
    private func remember(_ response: Response, under key: String,
                          for request: Request, by device: String) {
        idempotent = idempotent.filter { Date().timeIntervalSince($0.value.at) < 600 }
        idempotent[key] = (Date(), response)
        note(response, for: request, by: device)
    }

    /// The line that says what happened, on its own — because an answer that is deliberately not
    /// filed under its key is still an answer somebody may have to find in the log afterwards.
    private func note(_ response: Response, for request: Request, by device: String) {
        Log.write("remote: \(request.method) \(request.path) by \(device) → \(response.status)")
    }

    /// `keeping` is what the ten-minute cache is allowed to hold on to. It says yes to everything
    /// by default, because the ordinary case is that a retry must not repeat an effect and a
    /// refusal had no effect to repeat either way. A route passes something narrower when one of
    /// its answers is a fact about *this machine at this moment* rather than about the request —
    /// see the note in `transcribe` about `429` and `503`, which is the same argument.
    private func writing(_ request: Request, keeping keep: (Response) -> Bool = { _ in true },
                         _ body: ([String: Any]) -> Response) -> Response {
        if DispatchQueue.getSpecific(key: Self.terminalWorkerKey) == true {
            let parsed = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
                ?? [:]
            return body(parsed)
        }
        switch writeGate(request) {
        case .refused(let response), .replay(let response):
            return response
        case .go(let device, let key):
            let parsed = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
            let response = body(parsed)
            if keep(response) { remember(response, under: key, for: request, by: device) }
            else { note(response, for: request, by: device) }
            return response
        }
    }

    /// Machine-token writes use the same ten-minute replay table as paired-device writes without
    /// inheriting that route family's remote-write switch or device capability gate. A relay is
    /// still a terminal write, so retrying it without an idempotency key is still unsafe.
    private func orchestratorWriting(_ request: Request,
                                     _ body: ([String: Any]) -> Response) -> Response {
        guard let key = request.headers["idempotency-key"], !key.isEmpty else {
            return .error(400, "bad_request", "That needs an Idempotency-Key header.")
        }
        if let seen = idempotent[key], Date().timeIntervalSince(seen.at) < 600 {
            return seen.response
        }
        let parsed = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any]
            ?? [:]
        let response = body(parsed)
        remember(response, under: key, for: request, by: "orchestrator")
        return response
    }

    private var idempotent: [String: (at: Date, response: Response)] = [:]

    // MARK: - Dictation

    /// A recording made on a phone, turned into words by this Mac and nobody else's.
    ///
    /// The point of the route is where the audio does *not* go. A browser can already dictate —
    /// every phone has a recogniser behind a permission prompt — and what that costs is that the
    /// sentence goes to whoever wrote the recogniser. This app's position on that is stated
    /// everywhere else it comes up: the text lives on the Mac. So the phone records, the samples
    /// come here, and the model that reads them is the one already on this disk for the bar's own
    /// dictation — same binary, same model, same language setting, and no client may name a
    /// different one.
    ///
    /// **Answered from another queue, and that is the design rather than an optimisation.**
    /// Everything else here is read, decided and answered on one serial queue, which is what makes
    /// the state safe to touch without a lock. Whisper takes 1.6 seconds warm and about twelve
    /// after a reboot, so on that queue a single dictation would hold every other request and the
    /// event stream for as long as it ran. The gates are therefore checked here, where the state
    /// they read lives, and only the second and a half goes elsewhere.
    ///
    /// The queue it goes to is serial too, which is the other half: two whispers at once on one
    /// machine are slower than two in a row, so the queue *is* the concurrency limit and the only
    /// thing left to choose is how long a line is worth standing in.
    private func transcribe(_ request: Request, on conn: NWConnection) {
        if let refusal = crossOriginRefusal(request) ?? writeOriginRefusal(request) {
            send(withCachePolicy(refusal), on: conn)
            return
        }
        // **The one check `route` does for every other route.** Not being paired is answered
        // before not being allowed to send, because they are different sentences and only the
        // second one is about permission. Skipping `route` to keep whisper off the shared queue
        // also skipped its door, so an unpaired phone was told "This device may read, and not
        // send" — which claims it may read, and it may not. Measured against a running app:
        // `/v1/sessions/:id/send` answered 401 and this answered 403 for the same empty request.
        if case .denied = permission(for: request) {
            send(withCachePolicy(.error(401, "unauthorized", "This needs a paired device.")),
                 on: conn)
            return
        }
        // **Write, not read, and the reason is not that this writes anything.** It is that a
        // device which may only read has nowhere to put a sentence — it cannot send one — while
        // transcribing costs this Mac ten seconds of every core it has. Read-level access is
        // meant to be cheap for the Mac to grant; this is not.
        let device: String, key: String
        switch writeGate(request) {
        case .refused(let response), .replay(let response):
            send(withCachePolicy(response), on: conn)
            return
        case .go(let allowed, let filed):
            device = allowed
            key = filed
        }

        let parsed = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
        let samples: Data
        switch Self.voiceSamples(from: parsed) {
        case .refused(let response):
            // Filed under the key like any other answer: a body that cannot be read is a settled
            // fact about *this request*, so a retry of it deserves the same sentence rather than
            // another look at the same broken audio.
            remember(response, under: key, for: request, by: device)
            send(withCachePolicy(response), on: conn)
            return
        case .samples(let audio):
            samples = audio
        }

        // **Not filed under the key, and neither is a 503.** Both of those refusals are about this
        // machine at this moment rather than about the request — the queue drains, whisper gets
        // installed — and an answer frozen for ten minutes would mean the retry that was supposed
        // to work is told "busy" by a cache long after the queue emptied. The cache exists so that
        // a retry cannot repeat an effect, and a refusal had no effect to repeat.
        guard voiceQueued < Self.voiceDepth else {
            send(withCachePolicy(.error(429, "busy",
                                        "Two recordings are already waiting to be transcribed on "
                                        + "this Mac. Try again in a moment.")), on: conn)
            return
        }
        voiceQueued += 1
        voiceQueue.async { [weak self] in
            guard let self else { conn.cancel(); return }
            let response = Self.transcription(of: samples, by: device)
            // Back to the server's queue before anything is remembered or written, because the
            // idempotency table belongs to it and this closure does not.
            self.queue.async {
                self.voiceQueued -= 1
                // A transcript is worth keeping under the key; a machine that had no whisper a
                // second ago is not. Two identical keys arriving while the first is still running
                // will both transcribe — nothing is filed until there is something to file — and
                // the depth above is what keeps that from being unbounded.
                if response.status == 200 {
                    self.remember(response, under: key, for: request, by: device)
                }
                self.send(self.withCachePolicy(response), on: conn)
            }
        }
    }

    /// The queue the second and a half happens on. Serial, so this Mac only ever runs one whisper
    /// at a time no matter how many phones are pointed at it.
    private let voiceQueue = DispatchQueue(label: "com.tsunamiworks.clawdline.remote.voice")

    /// How many recordings are on it. Touched only from the server's queue, like everything else
    /// that is not behind a lock here.
    private var voiceQueued = 0

    /// One running, one waiting, and the third is told to come back.
    ///
    /// Not a resource limit — it is how long somebody is willing to hold a phone. A third
    /// recording accepted now would be answered five seconds after it was spoken, by which time
    /// its author has given up and pressed the button again; `busy` arriving straight away is the
    /// smaller answer and the more useful one, because it can be acted on.
    static let voiceDepth = 2

    /// The only sample rate this takes, and the shape that comes with it: little-endian 16-bit
    /// mono. Not a preference — `Whisper.transcribe` writes the WAV header around these bytes
    /// itself, and the bar's own recorder converts to exactly this before calling the same
    /// function. One rate is what makes the phone's path and the Mac's path the same path.
    static let voiceRate = 16_000.0

    /// Under a quarter of a second is a button pressed by accident, not a sentence. The same floor
    /// `Whisper.transcribe` keeps for itself, said here as well because the answer here can
    /// explain itself: whisper's way of refusing is `nil`, which this route would otherwise have
    /// to report as "nothing was said".
    static let voiceFloor = 0.25

    /// And five minutes is the far end.
    ///
    /// Not about the size of the request — a twenty-megabyte body already stops at about eight
    /// minutes of audio — but about what it costs. Whisper runs at something near real time on
    /// this hardware, so an eight-minute upload holds the queue for minutes with nobody able to
    /// call it back, and every dictation behind it waits. Five minutes is far past anything
    /// somebody says into a phone in one breath, and it bounds the worst a paired device can do
    /// here without inventing a refusal for the ordinary case.
    static let voiceCeiling = 300.0

    /// What a `/v1/voice` body turned out to be: samples worth handing to whisper, or the refusal
    /// they earned.
    enum Recording {
        case samples(Data)
        case refused(Response)
    }

    /// Read the body, and decide everything that can be decided before any of it costs a second of
    /// somebody's CPU.
    ///
    /// Split out and static for the reason `route` is split out from the connection handling: a
    /// test can ask it every question on this list without opening a socket, owning a microphone,
    /// or having whisper installed at all.
    ///
    /// The rate is read before the audio is decoded — the cheap field first, because a body that
    /// names the wrong rate is a body whose megabytes are not worth turning into bytes. And it is
    /// checked rather than resampled: 16 kHz is what the recorder produces and what whisper wants,
    /// so a client sending 48 kHz has not made a small mistake, it has sent something that would
    /// transcribe as a voice three times too fast. Refusing says that; resampling would hide it.
    static func voiceSamples(from body: [String: Any]) -> Recording {
        guard let rate = (body["rate"] as? NSNumber)?.doubleValue, rate == voiceRate else {
            return .refused(.error(400, "bad_request",
                                   "rate must be \(Int(voiceRate)). That is the only rate this "
                                   + "transcribes, and quietly resampling somebody's voice is a "
                                   + "worse answer than saying no."))
        }
        guard let encoded = body["audio"] as? String, !encoded.isEmpty else {
            return .refused(.error(400, "bad_request",
                                   "That needs audio: base64 of little-endian 16-bit mono PCM."))
        }
        // Unknown characters ignored, because an encoder that wraps its lines is not a client
        // making a mistake. Something that is not base64 at all still arrives as nothing, which is
        // the refusal underneath.
        guard let samples = Data(base64Encoded: encoded, options: [.ignoreUnknownCharacters]),
              !samples.isEmpty else {
            return .refused(.error(400, "bad_request", "That audio was not base64."))
        }
        let seconds = Double(samples.count) / (rate * 2)      // two bytes a sample, one channel
        guard seconds >= voiceFloor else {
            return .refused(.error(400, "bad_request",
                                   "That was \(String(format: "%.2f", seconds)) seconds of audio "
                                   + "and anything under \(voiceFloor) is a tap, not a sentence."))
        }
        guard seconds <= voiceCeiling else {
            return .refused(.error(400, "bad_request",
                                   "That was \(Int(seconds)) seconds of audio and the limit is "
                                   + "\(Int(voiceCeiling)). Send it in pieces."))
        }
        return .samples(samples)
    }

    /// The second and a half this route exists for, run on the dictation queue and nowhere else.
    ///
    /// Static because none of it belongs to the server: it takes bytes and the name of the device
    /// that sent them, and everything else it reads — binary, model, language, vocabulary — is
    /// the same config the bar's own dictation reads, from the same place.
    private static func transcription(of samples: Data, by device: String) -> Response {
        // Asked here rather than at the door on purpose. `Whisper.status` walks a few directories
        // and, on a Mac that has none of this, shells out to `which` — small, but the server's
        // queue is the one place where a small cost is paid by every other connection. Nothing
        // queues behind this answer anyway: a machine with no whisper has no transcription to be
        // slow about.
        switch Whisper.status(binary: Config.shared.whisperBinary, model: Config.shared.whisperModel) {
        case .noBinary:
            return .error(503, "no_whisper",
                          "This Mac has no whisper-cli, so there is nothing here to read a "
                          + "recording with. See docs/whisper.md.",
                          extra: ["reason": "no_binary"])
        case .noModel:
            // The commonest half of the two, and the reason `Whisper.Status` tells them apart at
            // all: `brew install whisper-cpp` leaves you here, and "no whisper" would send
            // somebody to check the thing they already did.
            return .error(503, "no_whisper",
                          "whisper-cli is installed on this Mac and has no model to read with. "
                          + "See docs/whisper.md.",
                          extra: ["reason": "no_model"])
        case .ready:
            break
        }

        let started = Date()
        // **No vocabulary, and that is not this route's decision to make.** `Whisper.transcribe`
        // takes the list and documents at length why it does not put it in the prompt: the prompt
        // is a writing sample, and a list of words in it costs the transcript its punctuation. The
        // repair happens afterwards instead, a few lines below, which is where the bar does it
        // too.
        let heard = Whisper.transcribe(samples, rate: voiceRate, vocabulary: [],
                                       language: Config.shared.voiceLanguage)
        let ms = Int(Date().timeIntervalSince(started) * 1000)
        // The same repair `Voice.emit` puts in front of everything on its way to the box, so that
        // a phone and the bar spell this project's own name the same way. `transcribe` has already
        // settled the script and the spacing; this is only the names it cannot be expected to know.
        let text = heard.map {
            Whisper.applyVocabulary($0, terms: Voice.alwaysExpected + Config.shared.voiceVocabulary)
        } ?? ""
        let seconds = Double(samples.count) / (voiceRate * 2)
        // Written from this queue rather than after the hop back, because an audit line is a file
        // append and the server's queue is where every other connection is waiting.
        RemoteAuth.audit("voice.transcribe", ["device": device,
                                              "seconds": String(format: "%.1f", seconds),
                                              "ms": "\(ms)",
                                              "chars": "\(text.count)",
                                              "ok": heard == nil ? "0" : "1"])
        // **Nothing heard is a 200.** Whisper answers `nil` for silence, for a clip it decided was
        // a groove, and for a recording of a room — and none of those is a failure of the request.
        // What was asked is "what was said", and "nothing" is an answer to that; a 4xx here would
        // have a page apologising for a microphone that was working perfectly.
        return .json(["text": text, "ms": ms])
    }

    // MARK: - Planning

    /// What a `/v1/intents` body turned out to be: a sentence worth planning from, or the refusal
    /// it earned. The shape ``Recording`` has, for the same reason it has it — a test can ask
    /// every question on this list without a socket, a model, or an account to bill.
    enum Sentence {
        case text(String)
        case refused(Response)
    }

    /// Four kibibytes of it, and that is generous rather than tight.
    ///
    /// The far end of this is somebody talking into a phone: five minutes of speech — the ceiling
    /// `/v1/voice` keeps — is a few hundred words, well inside this. What the limit is really for
    /// is the client that sends a file instead of a sentence, because every byte past the point a
    /// person could have said it is a byte this Mac pays a model to read.
    static let intentLimit = 4 << 10

    static func intent(from body: [String: Any]) -> Sentence {
        guard let raw = body["text"] as? String else {
            return .refused(.error(400, "bad_request", "That needs text: what the person said."))
        }
        // Counted in bytes rather than characters, because the limit is about what is paid for
        // and a Chinese sentence is three bytes a character. Counted *before* trimming, so a body
        // that is four kilobytes of whitespace is refused for its size rather than for being
        // empty — which is the more useful of the two sentences to be told.
        guard raw.utf8.count <= intentLimit else {
            return .refused(.error(400, "bad_request",
                                   "That was \(raw.utf8.count) bytes and the limit is "
                                   + "\(intentLimit). This plans a sentence, not a document."))
        }
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else {
            return .refused(.error(400, "bad_request", "That text was empty."))
        }
        return .text(text)
    }

    /// One spoken sentence, turned into a draft of a session — see ``Planner``.
    ///
    /// **Gated exactly like dictation, and for the third of the three reasons.** It starts
    /// nothing: what it answers with is an object a person reads, edits and may throw away, and
    /// `POST /v1/places/:id/start` remains the only thing in the app that opens a session. What
    /// it does spend is a model turn on somebody else's account and half a minute of this Mac's
    /// patience, and read-level access is meant to be cheap for the Mac to grant.
    ///
    /// Answered off the server's queue for the reason `handle` gives: five seconds on the one
    /// queue every connection is read on is five seconds of nothing else being answered.
    private func plan(_ request: Request, on conn: NWConnection) {
        if let refusal = crossOriginRefusal(request) ?? writeOriginRefusal(request) {
            send(withCachePolicy(refusal), on: conn)
            return
        }
        // Not being paired before not being allowed to send, the same order dictation puts them
        // in and for the same reason: they are different sentences and only the second is about
        // permission. See the note in `transcribe`.
        if case .denied = permission(for: request) {
            send(withCachePolicy(.error(401, "unauthorized", "This needs a paired device.")),
                 on: conn)
            return
        }
        let device: String, key: String
        switch writeGate(request) {
        case .refused(let response), .replay(let response):
            send(withCachePolicy(response), on: conn)
            return
        case .go(let allowed, let filed):
            device = allowed
            key = filed
        }

        let parsed = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
        let sentence: String
        switch Self.intent(from: parsed) {
        case .refused(let response):
            // Filed under the key, like dictation files a body it could not read: a sentence that
            // is too long is a settled fact about *this request*, and a retry of it deserves the
            // same answer rather than another look at the same four kilobytes.
            remember(response, under: key, for: request, by: device)
            send(withCachePolicy(response), on: conn)
            return
        case .text(let said):
            sentence = said
        }

        // Neither `busy` nor `no_planner` is filed under the key, for the reason spelled out in
        // `transcribe`: both are about this machine at this moment rather than about the request,
        // and an answer frozen for ten minutes would tell the retry that was supposed to work
        // that the queue is still full long after it emptied.
        guard planQueued < Self.planDepth else {
            send(withCachePolicy(.error(429, "busy",
                                        "This Mac is already working out two of these. Try again "
                                        + "in a moment.")), on: conn)
            return
        }
        planQueued += 1
        Planner.queue.async { [weak self] in
            guard let self else { conn.cancel(); return }
            let response = Self.planning(of: sentence, by: device)
            // Back to the server's queue before anything is remembered or written, because the
            // idempotency table and the counter above both belong to it and this closure does not.
            self.queue.async {
                self.planQueued -= 1
                if response.status == 200 {
                    self.remember(response, under: key, for: request, by: device)
                }
                self.send(self.withCachePolicy(response), on: conn)
            }
        }
    }

    /// How many of these are on ``Planner/queue``. Touched only from the server's queue.
    private var planQueued = 0

    /// One running, one waiting, and the third is told to come back — dictation's number, and the
    /// same argument: this is how long somebody is willing to hold a phone, not a resource limit.
    /// A model turn is longer than a transcription, which makes the case stronger rather than
    /// weaker.
    static let planDepth = 2

    /// The model turn this route exists for, run on the planner's queue and nowhere else.
    private static func planning(of sentence: String, by device: String) -> Response {
        let started = Date()
        let outcome = Planner.draft(for: sentence)
        let ms = Int(Date().timeIntervalSince(started) * 1000)
        switch outcome {
        case .noPlanner:
            RemoteAuth.audit("intent.plan", ["device": device, "ms": "\(ms)", "ok": "0",
                                             "why": "no_planner"])
            return .error(503, "no_planner",
                          "This Mac has neither claude nor codex on it, so there is nothing here "
                          + "to work out what you meant with.")
        case .failed:
            RemoteAuth.audit("intent.plan", ["device": device, "ms": "\(ms)", "ok": "0",
                                             "why": "failed"])
            // A 502 rather than a 500: what failed is the thing this asked, not this. The two
            // ways it happens — a turn that ran out of its thirty seconds, and one that answered
            // with prose instead of an object — are one sentence to whoever is holding the phone,
            // and the log has the difference.
            return .error(502, "plan_failed",
                          "The planner did not come back with anything usable. Try saying it "
                          + "again, or start the session by hand.")
        case .drafted(let draft):
            RemoteAuth.audit("intent.plan", ["device": device, "ms": "\(ms)",
                                             "place": draft.placeID ?? "none",
                                             "assistant": draft.assistant.rawValue,
                                             "confidence": String(format: "%.2f", draft.confidence),
                                             "ok": "1"])
            return .json(["draft": draft.payload, "ms": ms])
        }
    }

    /// A created schedule, plus the one thing the file cannot say about itself: whether anything
    /// on this Mac will ever run it.
    ///
    /// **Making one is deliberately not gated on the dispatch switch.** Writing a file is not
    /// dispatching, and refusing to save an evening's arrangement because the switch is off
    /// would be this route deciding what somebody may plan for later. What was missing is the
    /// other half: with `Settings → Remote → Agent tasks → Let a session hand work to another`
    /// off, the create said `Created.` and the minute timer then returned before it looked at
    /// any schedule at all — no session, and no sentence anywhere saying why. So this is a fact
    /// reported beside the answer, not a refusal added to it — `ok` is still true and the file
    /// is still there.
    ///
    /// **A refusal passes through untouched.** Nothing was made, so there is nothing to say
    /// about whether it would have run, and a client reading `dispatch_enabled` off a `400`
    /// would be reading it off a schedule that does not exist.
    static func scheduleAnswer(_ reply: Orchestrator.Reply,
                               dispatchEnabled: Bool) -> Orchestrator.Reply {
        guard case .ok(var payload) = reply else { return reply }
        payload["dispatch_enabled"] = dispatchEnabled
        return .ok(payload)
    }

    /// A closure write records only the attestation. Its HTTP receipt is then decorated from a
    /// fresh real projection, never from permissive placeholder inventory or terminal state.
    static func closureProjectionAnswer(
        _ reply: Orchestrator.Reply,
        projection: () -> Orchestrator.SessionCloseabilityProjection
    ) -> Orchestrator.Reply {
        guard case .ok(var payload) = reply else { return reply }
        payload["closeability"] = projection().wire
        return .ok(payload)
    }

    /// An orchestrator reply, in the envelope everything else already uses.
    func answer(_ reply: Orchestrator.Reply) -> Response {
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

    // MARK: - Readings too expensive for the shared queue

    /// Optional file/subprocess readers leave the shared server queue so they cannot delay health
    /// or SSE. Transcript has a separate bounded worker because first paint must not wait behind
    /// `/info`'s screen/Git/project work. Match the whole route shape, not a suffix.
    static func isSlowReading(_ path: String) -> Bool {
        if path == "/v1/places" { return true }
        guard path.hasPrefix("/v1/sessions/") else { return false }
        let rest = path.dropFirst("/v1/sessions/".count)
        let parts = rest.split(separator: "/", omittingEmptySubsequences: false)
        guard parts.count == 2, !parts[0].isEmpty else { return false }
        return parts[1] == "info" || parts[1] == "live"
    }

    static func isUsageAnalyticsReading(_ path: String) -> Bool {
        path == "/v1/orchestrator/usage/analytics"
            || path == "/v1/orchestrator/usage/analytics.csv"
            || path == "/v1/orchestrator/usage/analytics.json"
    }

    /// The slow optional reads that may be refused before they enter their worker queue.
    /// Transcript is classified separately and is never refused by this budget.
    static func isLimitedSlowReading(_ path: String) -> Bool {
        isSlowReading(path)
    }

    /// The one conversation route that must not inherit `/info`'s iTerm/Git/project latency.
    static func isTranscriptReading(_ path: String) -> Bool {
        guard path.hasPrefix("/v1/sessions/") else { return false }
        let rest = path.dropFirst("/v1/sessions/".count)
        let parts = rest.split(separator: "/", omittingEmptySubsequences: false)
        return parts.count == 2 && !parts[0].isEmpty && parts[1] == "transcript"
    }

    /// Missing or unknown query values preserve the complete v1 payload.
    static func infoIncludesDeferredComponents(_ query: [String: String]) -> Bool {
        query["parts"] != "summary"
    }

    static func isForegroundTranscript(_ query: [String: String]) -> Bool {
        query["priority"] == "foreground"
    }
    static func transcriptBusyResponse(retryDebt: Int = TranscriptReadCoordinator.depth) -> Response {
        var response = Response.error(
            429, "transcript_busy",
            "This Mac has \(retryDebt) transcript read(s) ahead. Try again after the bounded drain.",
            extra: ["retry_after": 1, "retry_debt": retryDebt])
        response.headers["Retry-After"] = "1"
        return response
    }

    /// Authenticate and encode at the transport boundary; the coordinator sees only opaque work.
    private func readTranscript(_ request: Request, on conn: NWConnection) {
        startTranscriptRead(request) { [weak self] response in
            guard let self else { conn.cancel(); return }
            self.send(response, on: conn)
        }
    }

    private func startTranscriptRead(_ request: Request,
                                     deliver: @escaping (Response) -> Void) {
        if let refusal = slowReadingRefusal(request) {
            deliver(withCachePolicy(refusal))
            return
        }
        transcriptReads.start(
            foreground: Self.isForegroundTranscript(request.query),
            executor: Self.transcriptTestExecutor,
            refusal: { self.withCachePolicy(Self.transcriptBusyResponse(retryDebt: $0)) },
            work: { Self.transcriptTestRoute?(request) ?? self.route(request) },
            completeOnOwner: { self.serialized($0) },
            deliver: deliver)
    }

    func configureTranscriptReadForTesting(
        executor: TranscriptReadCoordinator.Executor?,
        route: ((Request) -> Response)?
    ) {
        serialized {
            Self.transcriptTestExecutor = executor
            Self.transcriptTestRoute = route
        }
    }

    func transcriptReadForTesting(_ request: Request,
                                  completion: @escaping (Response) -> Void) {
        serialized { self.startTranscriptRead(request, deliver: completion) }
    }

    func transcriptOutstandingForTesting(
        _ completion: @escaping (_ total: Int, _ background: Int) -> Void
    ) {
        serialized {
            let counts = self.transcriptReads.counts
            completion(counts.total, counts.background)
        }
    }

    /// Admit on the owner queue, execute the canonical `route(request)` elsewhere, then return to
    /// the owner before draining. This preserves authentication/cache behavior without copying a
    /// route and keeps the limiter away from the worker thread.
    /// The optional reads, answered from the last good reading while the next one is taken. The
    /// lane's depth now refuses only a request that had nothing to serve; policy in `SlowReadings`.
    private func readSlowly(_ request: Request, on conn: NWConnection) {
        if let refusal = slowReadingRefusal(request) {
            send(withCachePolicy(refusal), on: conn)
            return
        }
        let arrived = Date()
        let key = SlowReadings.key(for: request)
        slowReadings.read(
            key, policy: SlowReadings.policy(for: request.path),
            admit: { self.readingLimiter.admit(request.path, depth: Self.readingDepth) },
            refusal: { SlowReadings.busy(depth: Self.readingDepth) },
            execute: { [readingQueue] work in readingQueue.async(execute: work) },
            compute: { self.route(request) }, classify: SlowReadings.classify,
            completeOnOwner: { [queue] work in queue.async(execute: work) },
            release: { self.readingLimiter.finish(request.path) },
            deliver: { [weak self] answer in
                guard let self else { conn.cancel(); return }
                self.send(SlowReadings.stamp(answer, arrived: arrived, lane: "reading",
                                             key: key, trace: Self.readingTrace), on: conn)
            })
    }

    private let slowReadings = SlowReadings.Readings()

    /// Analytics is independently shed and independently executed. Its SQLite work is bounded,
    /// but a full analytics queue still cannot be allowed to turn `/info` into 429 or park the
    /// serial Apple-event reading worker behind an accounting export.
    private func readUsageAnalytics(_ request: Request, on conn: NWConnection) {
        if let refusal = slowReadingRefusal(request) {
            send(withCachePolicy(refusal), on: conn)
            return
        }
        guard usageAnalyticsLimiter.admit(depth: Self.usageAnalyticsDepth) else {
            send(withCachePolicy(Self.usageAnalyticsBusyResponse()), on: conn)
            return
        }
        usageAnalyticsQueue.async { [weak self] in
            guard let self else { conn.cancel(); return }
            let response = self.route(request)
            self.queue.async {
                self.usageAnalyticsLimiter.finish()
                self.send(response, on: conn)
            }
        }
    }

    /// The part of `readSlowly`'s gate that `dispatch` will apply again on the worker. Kept as a
    /// seam so a test can prove that leaving `route` did not invent a different authentication
    /// answer.
    func slowReadingRefusal(_ request: Request) -> Response? {
        if let refusal = crossOriginRefusal(request) { return refusal }
        let orchestratorAuthed = request.path.hasPrefix("/v1/orchestrator/")
            && Orchestrator.verifyDispatch(token: request.headers["x-clawdline-orchestrator"])
        if case .denied = permission(for: request), !orchestratorAuthed {
            return .error(401, "unauthorized", "This needs a paired device.")
        }
        return nil
    }

    /// Serial keeps iTerm Apple events and unlocked reader caches behind one observable bound.
    /// The dedicated transcript worker remains independent, so this optional queue cannot delay
    /// first transcript paint.
    private let readingQueue = DispatchQueue(label: "com.tsunamiworks.clawdline.remote.reading")

    private let usageAnalyticsQueue = DispatchQueue(
        label: "com.tsunamiworks.clawdline.remote.usage-analytics")

    /// Optional-read counters are owner-queue state; transcript accounting lives in its service.
    private var readingLimiter = ReadingLimiter()
    private let transcriptReads = TranscriptReadCoordinator()
    private static var transcriptTestExecutor: TranscriptReadCoordinator.Executor?
    private static var transcriptTestRoute: ((Request) -> Response)?
    private var usageAnalyticsLimiter = UsageAnalyticsLimiter()

    /// The counter operation as one testable unit. `finish` happens before the response is sent,
    /// so an already-interrupted connection cannot strand a place in the queue. An unbounded
    /// transcript is admitted and finished without changing the count.
    struct ReadingLimiter {
        private(set) var count = 0

        mutating func admit(_ path: String, depth: Int) -> Bool {
            guard RemoteServer.isLimitedSlowReading(path) else { return true }
            guard count < depth else { return false }
            count += 1
            return true
        }

        mutating func finish(_ path: String) {
            guard RemoteServer.isLimitedSlowReading(path) else { return }
            precondition(count > 0)
            count -= 1
        }
    }

    struct UsageAnalyticsLimiter {
        private(set) var count = 0

        mutating func admit(depth: Int) -> Bool {
            guard count < depth else { return false }
            count += 1
            return true
        }

        mutating func finish() {
            precondition(count > 0)
            count -= 1
        }
    }

    static func usageAnalyticsBusyResponse() -> Response {
        .error(429, "usage_analytics_busy",
               "This Mac already has \(usageAnalyticsDepth) usage analytics requests queued. Try again in a moment.")
    }

    /// Eight, shared only by `/info` and `/v1/places`; transcript has its own admission budget.
    ///
    /// Dictation's two is an answer to "how long will somebody hold a phone" with a five-second
    /// answer under it. In the measured healthy case these reads take about half a second, so
    /// eight is a useful patience bound. It is not a promise about wait time or drain rate: an
    /// `/info` can sit in a 15-second Apple event timeout or a six-second project-status timeout,
    /// and while it does the eighth request can wait minutes. At that point rejecting the next
    /// bounded read is load shedding, not evidence that trying again immediately will work.
    static let readingDepth = 8
    static let readingTrace = ReadingTrace()
    static let usageAnalyticsDepth = 2
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
            // `availability` alone, no window detail: enough for a start button to grey itself
            // out or carry a badge, without paying for the fuller answer nobody asked for here.
            // See `GET /v1/orchestrator/assistants` for the rest.
            "assistants": Assistant.available.map {
                ["id": $0.rawValue, "label": $0.label,
                 "availability": AssistantQuota.current(for: $0).availability.rawValue]
            },
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

    /// The conversations already recorded in one place — see ``StartPoints/past(in:limit:)``.
    ///
    /// `id` is the transcript's own name, and it is the only part a client sends back: the
    /// resume route takes an id and no path, and resolves it against a listing of its own.
    /// `live` says something is writing to that transcript right now, which is the one thing
    /// a person needs to know before tapping a row — resuming it would put a second process on
    /// the same file.
    private func pastPayload(_ place: StartPoints.Place,
                             assistant: Assistant = .claude) -> [String: Any] {
        // One more than is sent, so the reply can say whether there were any. A list that simply
        // ends at its cap is a list that lies by omission — which is the bug this whole route
        // already had once, at forty, and it is not one to leave a second copy of at two hundred.
        let cap = 200
        let found = StartPoints.past(in: place, assistant: assistant, limit: cap + 1)
        return [
            "at": Int(Date().timeIntervalSince1970),
            "place": place.id,
            "assistant": assistant.rawValue,
            "more": found.count > cap,
            "sessions": found.prefix(cap).map { past -> [String: Any] in
                [
                    "id": past.id,
                    "title": past.title,
                    "at": Int(past.at.timeIntervalSince1970),
                    "live": past.live,
                ]
            },
        ]
    }

    /// The card behind `GET /v1/sessions/:id/info`. Everything in it is gathered here and shaped
    /// in ``SessionInfo/payload(id:assistant:sessionId:model:cwd:startedAt:now:usage:limits:files:deploy:)``,
    /// which is the half a test can reach.
    ///
    /// The token totals are the orchestrator's own readers, because a child's transcript and a
    /// session's transcript are the same kind of file and one summing of it is enough. The model
    /// comes from the same pass as the limits rather than from the totals: the last assistant
    /// turn of a session that just hit its window is `<synthetic>`, and that is not a model.
    private func infoPayload(for session: TargetSession, includeDeferred: Bool = true) -> [String: Any] {
        let publication = SessionWatch.shared.publishedInventory()
        let publishedIdentity = publication.identities[session.id]
        let cwd = publishedIdentity?.workingDirectory ?? session.cwd
        let record = publishedIdentity.flatMap { identity in
            identity.recordURL.map { (url: $0, assistant: identity.assistant) }
        }

        var usage: Orchestrator.Usage?
        var context: SessionInfo.Context?
        var costOverrideUsd: Double?
        var limits = SessionInfo.Limits()
        var model: String?
        // The transcript is read if it can be, and its absence no longer silences everything
        // else. Claude Code's status-line cache answers the context fill and the exact cost on
        // its own, under the same id this record names, so a transcript that is missing, empty
        // or unreadable now costs only the facts that actually come from it. Every reader below
        // takes an empty buffer as "the transcript said nothing" and answers absent.
        if let record {
            let data = (try? Data(contentsOf: record.url)) ?? Data()
            switch record.assistant {
            case .claude:
                let sessionID = record.url.deletingPathExtension().lastPathComponent
                let cache = SessionInfo.claudeSessionCache(
                    cacheDirectory: ProjectStatus.cacheDirectory, sessionID: sessionID)
                usage = Orchestrator.claudeUsage(transcript: record.url)
                let read = SessionInfo.claudeLimits(transcript: data)
                limits = read.limits
                model = read.model
                let usageModel = usage?.model.flatMap { $0.hasPrefix("<") ? nil : $0 }
                context = SessionInfo.claudeContext(
                    transcript: data, cache: cache, model: model ?? usageModel)
                costOverrideUsd = SessionInfo.claudeCost(cache: cache)
            case .codex:
                usage = Orchestrator.codexUsage(rollout: record.url)
                context = SessionInfo.codexContext(rollout: data)
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
        let links = includeDeferred
            ? (cwd.map { linksPayload(cwd: $0, sessionID: session.id) } ?? []) : []
        let deploy = links.filter { row in
            let kind = row["kind"] as? String
            return kind == "deploy" || kind == "ci"
        }

        let permission = includeDeferred && session.assistant == .claude
            ? SessionInfo.permissionMode(screen: Targets.visibleScreen(of: session)) : nil
        var payload = SessionInfo.payload(
            id: session.id, title: publication.labels[session.id] ?? session.coordinate,
            assistant: session.assistant,
            sessionId: Self.sessionIdentity(assistant: session.assistant,
                                            processBound: publishedIdentity?.conversationID),
            model: model,
            cwd: cwd, startedAt: publishedIdentity?.processStart,
            usage: usage, context: context, costOverrideUsd: costOverrideUsd, limits: limits,
            files: includeDeferred ? cwd.flatMap { SessionInfo.files(cwd: $0) } : nil,
            deploy: deploy, models: SessionInfo.models(for: session.assistant),
            permission: permission)
        if includeDeferred { payload["links"] = links }
        return payload
    }

    private func sessionsPayload() -> [String: Any] {
        let watch = SessionWatch.shared
        let publication = watch.publishedInventory()
        let supplied = Self.sessionPayloadForTesting
        let targets = supplied?.0 ?? publication.targets
        let states = supplied?.1 ?? publication.states
        // The identity walk is done once for the whole list and handed down. Each row needed it
        // anyway, and the duplicate count is a fact about the list rather than about one row.
        let identities = Dictionary(
            targets.map { session in
                (session.id, Self.sessionWorkIdentity(
                    session, publishedIdentity: publication.identities[session.id]))
            },
            uniquingKeysWith: { first, _ in first })
        let matches = Self.identityMatchCounts(
            targets.filter(\.isAssistant).compactMap { identities[$0.id] })
        let evidence = sessionInventoryEvidence(publication)
        let registry = Orchestrator.closeabilityRegistrySnapshot()
        return [
            "sessions": targets.map {
                json(of: $0, stateOverride: states[$0.id], identity: identities[$0.id],
                     identityMatches: matches[$0.id] ?? 1, inventory: evidence,
                     closeabilityRegistry: registry,
                     acceptedPublication: publication,
                     publishedIdentity: publication.identities[$0.id])
            },
            "at": Int(Date().timeIntervalSince1970),
            "scan": [
                "epoch": publication.epoch,
                "generation": publication.generation,
                "complete": publication.complete,
                "provenance": publication.provenance,
                "emptyAuthoritative": publication.emptyAuthoritative,
                "completed": [
                    "sequence": publication.completedSequence,
                    "complete": publication.completedComplete,
                ],
            ],
        ]
    }

    /// The assistant conversation id a public session row may expose.
    ///
    /// Split out from ``json(of:)`` so the identity source is a testable contract rather than a
    /// field assignment hidden among presentation data. `processBound` is the identity proved by
    /// the current process/transcript reader. A tty hook is deliberately not an input: it can
    /// outlive the Claude process that wrote it and must never identify a later Codex process or
    /// an ordinary shell which reused the terminal.
    static func sessionIdentity(assistant: Assistant?, processBound: String?) -> String? {
        guard assistant != nil else { return nil }
        return processBound
    }

    /// The process facts used by both the public conversation id and the broker work-state
    /// projection. A replacement between reads can only produce a mixed tuple that fails the
    /// all-fields task comparison; it can never promote a stale receipt.
    static func sessionWorkIdentity(_ session: TargetSession) -> Orchestrator.SessionWorkIdentity {
        if let supplied = sessionWorkIdentityForTesting { return supplied(session) }
        let publishedIdentity = SessionWatch.shared.publishedInventory().identities[session.id]
        return sessionWorkIdentity(session, publishedIdentity: publishedIdentity)
    }

    static func sessionWorkIdentity(
        _ session: TargetSession, publishedIdentity: SessionWatch.PublishedIdentity?
    ) -> Orchestrator.SessionWorkIdentity {
        if let supplied = sessionWorkIdentityForTesting { return supplied(session) }
        let evidence = publishedIdentity.flatMap { identity -> SessionWatch.PublishedIdentity? in
            guard identity.assistant == session.assistant, identity.tty == session.tty else {
                return nil
            }
            return identity
        }
        return Orchestrator.SessionWorkIdentity(
            terminalID: session.id, assistant: session.assistant, tty: session.tty,
            pid: evidence?.pid, processStart: evidence?.processStart,
            conversationID: sessionIdentity(
                assistant: session.assistant, processBound: evidence?.conversationID))
    }

    /// What the SessionWatch reading that produced a row was worth. Closeability needs all
    /// three: a reading that never happened, one that was incomplete, and a current one are
    /// three different answers and only the last of them can support `safe`.
    struct SessionInventoryEvidence {
        let complete: Bool
        let observedAt: Date?
        let generation: Int?
    }

    /// How many rows of one inventory resolve to each row's exact (assistant, conversation)
    /// pair. Anything but one is `session_identity_ambiguous`: two tabs resuming one
    /// conversation, or a row absent from the reading it was supposed to be in. Both are the
    /// same fact for closeability — the broker cannot say which process it is talking about.
    static func identityMatchCounts(_ identities: [Orchestrator.SessionWorkIdentity])
        -> [String: Int] {
        SessionClosePolicy.identityMatchCounts(identities)
    }

    private func sessionInventoryEvidence(
        _ acceptedPublication: SessionWatch.InventoryPublication? = nil
    ) -> SessionInventoryEvidence {
        if let supplied = Self.coordinatorObservationEvidenceForTesting {
            return SessionInventoryEvidence(complete: supplied.complete,
                                            observedAt: supplied.observedAt,
                                            generation: supplied.generation)
        }
        if Self.sessionPayloadForTesting != nil {
            return SessionInventoryEvidence(complete: true, observedAt: Date(), generation: nil)
        }
        let publication = acceptedPublication ?? SessionWatch.shared.publishedInventory()
        return SessionInventoryEvidence(
            complete: publication.complete, observedAt: publication.observedAt,
            generation: publication.generation)
    }

    private func sessionIdentityMatchCount(for identity: Orchestrator.SessionWorkIdentity)
        -> Int {
        guard identity.assistant != nil, identity.conversationID != nil else { return 1 }
        let targets: [TargetSession]
        let publishedIdentities: [String: SessionWatch.PublishedIdentity]
        if let supplied = Self.sessionPayloadForTesting?.0 {
            targets = supplied
            publishedIdentities = [:]
        } else {
            let publication = SessionWatch.shared.publishedInventory()
            targets = publication.targets
            publishedIdentities = publication.identities
        }
        let counts = Self.identityMatchCounts(
            targets.filter(\.isAssistant).map { session in
                Self.sessionWorkIdentity(
                    session, publishedIdentity: publishedIdentities[session.id])
            })
        return counts[identity.terminalID] ?? 0
    }

    /// One Session's closeability, read through the same seams every other Session read uses.
    /// Used by the close gate and by the attestation route, both of which are about exactly one
    /// session and can afford the identity walk a list route amortises.
    func closeability(of session: TargetSession,
                              identity: Orchestrator.SessionWorkIdentity? = nil)
        -> Orchestrator.SessionCloseabilityProjection {
        let resolved = identity ?? Self.sessionWorkIdentity(session)
        let evidence = sessionInventoryEvidence()
        return Orchestrator.sessionCloseability(
            identity: resolved, terminalState: state(of: session.id),
            inventoryComplete: evidence.complete, inventoryObservedAt: evidence.observedAt,
            inventoryGeneration: evidence.generation,
            identityMatches: sessionIdentityMatchCount(for: resolved))
    }

    enum CloseProofRequest: Equatable {
        /// No `expected_closeability_version`: the close keeps exactly the contract it had.
        case notRequested
        case expecting(String)
        case malformed
    }

    static func closeProofRequest(_ raw: Any?) -> CloseProofRequest {
        guard let raw else { return .notRequested }
        guard let expected = raw as? String, !expected.isEmpty else { return .malformed }
        return .expecting(expected)
    }

    /// **`acceptLoss` is a parameter this deliberately does not read.**
    ///
    /// It is here so the claim is a thing a test can break rather than a sentence in a comment:
    /// `accept_loss` is the human override for a positive `lost_if_closed` list somebody was
    /// shown, and stale, ambiguous, unattested or merely superseded evidence produces no such
    /// list. Accepting a loss nobody can enumerate is not consent, and the one honest answer to
    /// "the broker cannot prove this" is to refuse and say why.
    static func closeIsProven(_ projection: Orchestrator.SessionCloseabilityProjection,
                              expected: String, acceptLoss: Bool) -> Bool {
        _ = acceptLoss
        return projection.state == .safe && projection.version == expected
    }

    private struct CoordinatorSessionBase {
        let identity: Orchestrator.SessionWorkIdentity
        let state: SessionState
        let label: String
        let cwd: String?
    }

    struct CoordinatorSessionInventory {
        let targets: [TargetSession]
        let states: [String: SessionState]
        let identities: [String: SessionWatch.PublishedIdentity]
        let labels: [String: String]
        let complete: Bool
        let observedAt: Date?
        let generation: Int?

        init(targets: [TargetSession], states: [String: SessionState],
             identities: [String: SessionWatch.PublishedIdentity] = [:],
             labels: [String: String] = [:], complete: Bool,
             observedAt: Date?, generation: Int?) {
            self.targets = targets
            self.states = states
            self.identities = identities
            self.labels = labels
            self.complete = complete
            self.observedAt = observedAt
            self.generation = generation
        }
    }

    struct CoordinatorObservation {
        let sessions: [Coordinator.LiveSession]
        let sessionsObservedAt: Date?
        let sessionsGeneration: Int?
        let sessionsFresh: Bool
        let registry: Orchestrator.CoordinatorSnapshot
    }

    static func coordinatorSessionsFresh(complete: Bool, observedAt: Date?) -> Bool {
        observedAt == nil ? true : complete
    }

    /// SessionWatch and Orchestrator are independent sources, so they are observed in that order
    /// and carry separate times/provenance. Within the second window, all work/ownership rows and
    /// all three totals are one registry snapshot.
    func coordinatorObservation() -> CoordinatorObservation {
        if let supplied = Self.coordinatorSessionsForTesting {
            let evidence = Self.coordinatorObservationEvidenceForTesting
            let observedAt: Date? = evidence.map(\.observedAt) ?? Date()
            let observations = supplied.map {
                Orchestrator.CoordinatorSessionObservation(
                    identity: $0.identity, terminalState: .unknown,
                    projectedWorkState: $0.workState)
            }
            return CoordinatorObservation(
                sessions: supplied, sessionsObservedAt: observedAt,
                sessionsGeneration: evidence?.generation,
                sessionsFresh: evidence?.complete ?? true,
                registry: Orchestrator.coordinatorSnapshot(
                    observations, sessionsFresh: evidence?.complete ?? true,
                    sessionsObservedAt: observedAt,
                    sessionsGeneration: evidence?.generation))
        }
        let publication = SessionWatch.shared.publishedInventory()
        let unavailable = Self.coordinatorObservationUnavailableForTesting
        let inventory = CoordinatorSessionInventory(
            targets: unavailable ? [] : publication.targets,
            states: unavailable ? [:] : publication.states,
            identities: unavailable ? [:] : publication.identities,
            labels: unavailable ? [:] : publication.labels,
            complete: unavailable ? false : publication.complete,
            observedAt: unavailable ? nil : publication.observedAt,
            generation: unavailable ? nil : publication.generation)
        // Missing evidence has no observed time and fails ownership closed as unknown. Otherwise
        // freshness is the confidence bit from the exact immutable publication being projected.
        let sessionsFresh = Self.coordinatorSessionsFresh(
            complete: inventory.complete, observedAt: inventory.observedAt)
        let bases = inventory.targets.filter(\.isAssistant).map { session in
            CoordinatorSessionBase(
                identity: Self.sessionWorkIdentity(
                    session, publishedIdentity: inventory.identities[session.id]),
                state: inventory.states[session.id] ?? .unknown,
                label: inventory.labels[session.id] ?? session.coordinate,
                cwd: inventory.identities[session.id]?.workingDirectory)
        }
        let registry = Orchestrator.coordinatorSnapshot(
            bases.map { .init(identity: $0.identity, terminalState: $0.state) },
            sessionsFresh: sessionsFresh, sessionsObservedAt: inventory.observedAt,
            sessionsGeneration: inventory.generation)
        let matches = Self.identityMatchCounts(bases.map(\.identity))
        let closeabilityRegistry = Orchestrator.closeabilityRegistrySnapshot()
        let sessions = zip(bases, registry.sessions).map { base, facts in
            Coordinator.LiveSession(
                identity: base.identity, label: base.label, cwd: base.cwd,
                workState: facts.work.state,
                waitingOnSession: !facts.coordination.waitingOn.isEmpty,
                hasWaiters: !facts.coordination.waitedOnBy.isEmpty,
                closeability: Orchestrator.sessionCloseability(
                    identity: base.identity, terminalState: base.state,
                    inventoryComplete: sessionsFresh,
                    inventoryObservedAt: inventory.observedAt,
                    inventoryGeneration: inventory.generation,
                    identityMatches: matches[base.identity.terminalID] ?? 1,
                    registrySnapshot: closeabilityRegistry).state)
        }
        return CoordinatorObservation(
            sessions: sessions, sessionsObservedAt: inventory.observedAt,
            sessionsGeneration: inventory.generation,
            sessionsFresh: sessionsFresh, registry: registry)
    }

    /// Shared by both Session JSON surfaces. Keeping this one optional assignment as the whole
    /// integration point makes an unregistered installation byte-for-byte equivalent at row level.
    static func attachCoordinator(to row: inout [String: Any],
                                  liveSession: Coordinator.LiveSession) {
        if let coordinator = Coordinator.sessionProjection(for: liveSession) {
            row["coordinator"] = coordinator
        }
    }

    /// Production constructs the projection from the current process-bound facts. Tests may
    /// replace only that external observation, so both real serializers can be executed without
    /// inventing a PID or transcript in the test runner process.
    private static func coordinatorProjectionSession(
        identity: Orchestrator.SessionWorkIdentity, label: String, cwd: String?,
        workState: Orchestrator.SessionWorkState, waitingOnSession: Bool, hasWaiters: Bool,
        closeability: Orchestrator.SessionCloseability? = nil
    ) -> Coordinator.LiveSession {
        if let supplied = coordinatorSessionsForTesting?.first(where: {
            $0.identity.terminalID == identity.terminalID
        }) { return supplied }
        return Coordinator.LiveSession(
            identity: identity, label: label, cwd: cwd, workState: workState,
            waitingOnSession: waitingOnSession, hasWaiters: hasWaiters,
            closeability: closeability)
    }

    /// The sessions a coordination wait can address, as the facts needed to address one and
    /// nothing else.
    ///
    /// Internal, and static, for direct serialization tests: what a row contains is a pure
    /// function of the sessions and their states, in the way `transcriptRows` is of its entries.
    ///
    /// **Where the line falls.** Everything here is something the caller has to know before it
    /// can write a wait down — which session, running what, in which checkout, and whether
    /// anybody is home. `label` is the only field that is not purely structural: it is what
    /// Clawdline calls the session — a name a person typed for it, else the Clawdline task title
    /// when this app opened the tab, else what the conversation calls itself in the assistant's
    /// own records, else the coordinate `⌘<window>-<tab>` — and **never the tab's own title**,
    /// which is a place a name is displayed and not a place one is kept. ``SessionNaming`` states
    /// that rule and the incident behind it; `docs/api.md` documents this row from here, so an
    /// edit that softens it here is the one the next reader will copy. That is still a phrase,
    /// and without it two sessions in one checkout cannot be told apart, which is exactly the
    /// case waits exist for. Everything past a phrase stays out — no `line`, no `menu`, no
    /// `agents`, no `shells`,
    /// and in particular no `sessionId`, which is the name of the assistant's own transcript
    /// file and would turn a dispatch credential into a reading one.
    ///
    /// Sessions with no assistant in them are left out. A wait is delivered by typing a line into
    /// the owner's session, and a shell prompt has nobody to read it — listing one would offer an
    /// address that cannot answer, and would make this a list of every terminal window open.
    static func coordinationSessionRows(_ sessions: [TargetSession],
                                        states: [String: SessionState],
                                        inventory: SessionInventoryEvidence? = nil,
                                        publishedIdentities:
                                            [String: SessionWatch.PublishedIdentity] = [:],
                                        publishedLabels: [String: String] = [:])
        -> [[String: Any]] {
        let assistants = sessions.filter { $0.isAssistant }
        let identities = Dictionary(assistants.map {
            ($0.id, sessionWorkIdentity(
                $0, publishedIdentity: publishedIdentities[$0.id]))
        },
                                    uniquingKeysWith: { first, _ in first })
        let matches = identityMatchCounts(assistants.compactMap { identities[$0.id] })
        let evidence = inventory ?? SessionInventoryEvidence(
            complete: true, observedAt: Date(), generation: nil)
        let registry = Orchestrator.closeabilityRegistrySnapshot()
        return assistants.map { session -> [String: Any] in
            let terminalState = states[session.id] ?? .unknown
            let identity = identities[session.id] ?? sessionWorkIdentity(session)
            let work = Orchestrator.sessionWorkProjection(
                identity: identity, terminalState: terminalState)
            // The address book answers "can this one still be given work" and now also "can it
            // be ended". Both are structural facts about a session; neither is screen content.
            let closeable = Orchestrator.sessionCloseability(
                identity: identity, terminalState: terminalState,
                inventoryComplete: evidence.complete, inventoryObservedAt: evidence.observedAt,
                inventoryGeneration: evidence.generation,
                identityMatches: matches[session.id] ?? 1,
                registrySnapshot: registry)
            var row: [String: Any] = [
                "id": session.id,
                "label": publishedLabels[session.id] ?? session.coordinate,
                "state": name(of: terminalState),
                "work_state": work.state.rawValue,
                "closeability": closeable.wire,
            ]
            if let assistant = session.assistant { row["assistant"] = assistant.rawValue }
            let cwd = publishedIdentities[session.id]?.workingDirectory
            if let cwd { row["cwd"] = cwd }
            // The task this tab was opened for, when Clawdline opened it. The same credential
            // already reads the whole record at `GET /v1/orchestrator/tasks`, so this discloses
            // nothing new — and it is the one address that needs no label matching at all.
            if let role = Orchestrator.role(forTerminal: session.id) { row["taskId"] = role.taskID }
            if let assignment = Orchestrator.rootAssignmentSessionRecord(identity: identity) {
                row["root_assignment"] = assignment
            }
            let live = coordinatorProjectionSession(
                identity: identity,
                label: publishedLabels[session.id] ?? session.coordinate,
                cwd: cwd, workState: work.state,
                waitingOnSession: false, hasWaiters: false, closeability: closeable.state)
            attachCoordinator(to: &row, liveSession: live)
            return row
        }
    }

    /// The session an id names, in one list and one comparison.
    ///
    /// `POST /v1/orchestrator/waits` finds its waiter through here and
    /// `GET /v1/orchestrator/sessions` publishes ids from the same field, so the index cannot
    /// come to name sessions the wait routes then refuse to find.
    static func session(withID id: String, among sessions: [TargetSession]) -> TargetSession? {
        sessions.first { $0.id == id }
    }

    /// Resolve a relay source in the two namespaces already exposed to assistant sessions: its
    /// terminal-neutral id, or the conversation id proved for the process currently in it. No
    /// title, prefix or tty matching is allowed, and ambiguity fails closed.
    static func sessionMessageSource(withID id: String, among sessions: [TargetSession],
                                     conversationID: (TargetSession) -> String?)
        -> TargetSession? {
        let matches = sessions.filter { session in
            session.id == id || conversationID(session) == id
        }
        return matches.count == 1 ? matches[0] : nil
    }

    enum SessionWhoAmIResolution {
        case resolved(TargetSession, Assistant, String)
        case notFound
        case ambiguous
        case stale
    }

    /// Resolve only the conversation namespace through the broker's exact root-session seam.
    /// A second full pass is the consistency boundary: if the process binding moved, vanished or
    /// became ambiguous between passes, the caller gets `stale` and never a terminal from mixed
    /// observations. Labels, cwd, terminal state and terminal ids are not consulted.
    static func sessionWhoAmI(conversationID wanted: String,
                              among sessions: [TargetSession],
                              identityPass: () -> [String: String],
                              afterPass: ((Int) -> Void)? = nil)
        -> SessionWhoAmIResolution {
        func matches(_ observed: [String: String]) -> [TargetSession] {
            Orchestrator.targets(forRootSession: wanted, assistant: nil, resolution: .task,
                                 among: sessions, sessionID: { session in
                observed[session.id]
            })
        }
        let firstObserved = identityPass()
        let first = matches(firstObserved)
        guard !first.isEmpty else { return .notFound }
        guard first.count == 1 else { return .ambiguous }
        guard let assistant = first[0].assistant else { return .notFound }
        afterPass?(1)
        let secondObserved = identityPass()
        let second = matches(secondObserved)
        guard second.count == 1, second[0].id == first[0].id,
              second[0].assistant == assistant,
              let canonical = secondObserved[second[0].id] else { return .stale }
        return .resolved(second[0], assistant, canonical)
    }

    /// SessionWatch publishes an immutable lock-protected generation. This server-queue lookup
    /// reads that value directly; opening one row neither synchronously enters main nor asks the
    /// machine for a replacement process observation.
    func session(withID id: String) -> TargetSession? {
        if let supplied = Self.sessionPayloadForTesting?.0 {
            return Self.session(withID: id, among: supplied)
        }
        return Self.session(
            withID: id, among: SessionWatch.shared.publishedInventory().targets)
    }

    private func sessionMessageSource(withID id: String) -> TargetSession? {
        if let supplied = Self.sessionPayloadForTesting?.0 {
            return Self.sessionMessageSource(withID: id, among: supplied) { session in
                Self.sessionIdentity(assistant: session.assistant,
                                     processBound: session.id)
            }
        }
        let publication = SessionWatch.shared.publishedInventory()
        let sessions = publication.targets
        return Self.sessionMessageSource(withID: id, among: sessions) { session in
            Self.sessionIdentity(assistant: session.assistant,
                                 processBound: publication.identities[session.id]?.conversationID)
        }
    }

    private func state(of sessionID: String) -> SessionState {
        if let supplied = Self.sessionPayloadForTesting?.1 {
            return supplied[sessionID] ?? .unknown
        }
        return SessionWatch.shared.publishedInventory().states[sessionID] ?? .unknown
    }

    private func titleState(of sessionID: String) -> SessionState {
        onMain(from: "RemoteServer.titleState(of:)") {
            SessionWatch.shared.states[sessionID] ?? .unknown
        }
    }

    /// Exercise every production SessionWatch reader. The fixture supplies an id from
    /// SessionWatch itself, keeping the reads non-empty without replacing the inventory through a
    /// test seam. Which sites crossed is read from ``MainQueue/endRecordingHopsForTesting()``, so
    /// a reader whose crossing was deleted cannot still be named by the list it used to return.
    func exerciseQueueCrossingsForTesting(sessionID: String) {
        _ = titleState(of: sessionID)
        _ = coordinatorObservation()
        _ = session(withID: sessionID)
        _ = sessionMessageSource(withID: sessionID)
        _ = state(of: sessionID)
        _ = sessionInventoryEvidence()
    }

    /// Whether Clawdline may type a coordination-wait notice into that session right now, in the
    /// same terms `POST /v1/sessions/<id>/send` already uses: a picker discards the words and acts
    /// on the Return that follows them, so a message sent into one is lost *and* answers somebody
    /// else's permission question. Nothing stricter — a session that is merely busy, or holding a
    /// half-typed line, still reads what arrives, and refusing those would park every wait behind
    /// whatever the owner happens to be running.
    ///
    /// A session this Mac cannot see at all is not a readiness answer: that is the delivery
    /// failure the caller already reports, and answering it here would file "the owner is gone"
    /// under "the owner is busy".
    private func coordinationReadiness(_ targetID: String) -> String? {
        let publication = SessionWatch.shared.publishedInventory()
        guard let target = Self.session(withID: targetID, among: publication.targets),
              publication.states[target.id] == .waiting,
              Targets.isChoosing(target) else { return nil }
        return "That session is showing a menu, so typing into it would confirm whichever "
            + "option is highlighted rather than deliver this notice."
    }

    private func json(of session: TargetSession,
                      stateOverride: SessionState? = nil,
                      identity precomputedIdentity: Orchestrator.SessionWorkIdentity? = nil,
                      identityMatches: Int? = nil,
                      inventory: SessionInventoryEvidence? = nil,
                      closeabilityRegistry: Orchestrator.CloseabilityRegistrySnapshot? = nil,
                      acceptedPublication: SessionWatch.InventoryPublication? = nil,
                      publishedIdentity acceptedPublishedIdentity:
                        SessionWatch.PublishedIdentity? = nil)
        -> [String: Any] {
        let watch = SessionWatch.shared
        let publication = acceptedPublication ?? watch.publishedInventory()
        let publishedIdentity = acceptedPublishedIdentity ?? publication.identities[session.id]
        let state = stateOverride ?? publication.states[session.id] ?? .unknown
        let menu = publication.menus[session.id]
        var out: [String: Any] = [
            "id": session.id,
            "backend": session.backend.rawValue,
            "tty": session.tty.replacingOccurrences(of: "/dev/", with: ""),
            "label": publication.labels[session.id] ?? session.coordinate,
            // Kept next to `assistant`, and it means what it always did. A page built against
            // the old field still draws a Claude Code session correctly; what it does with a
            // Codex one is show it as an ordinary terminal, which is wrong but not broken —
            // and the alternative was every existing client losing its session list at once.
            "isClaude": session.isClaude,
            "state": Self.name(of: state),
        ]
        let identity = precomputedIdentity ?? Self.sessionWorkIdentity(session)
        let work = Orchestrator.sessionWorkProjection(
            identity: identity, terminalState: state)
        out["work_state"] = work.state.rawValue
        // The evidence fields beside the state: who said so (`broker` projected it, or the
        // session declared it), since when, in whose words, and who will move it. The `owed`
        // overlay is the independent second axis and rides beside any state.
        out["work_provenance"] = work.provenance
        if let note = work.note { out["work_note"] = note }
        if let since = work.since { out["work_since"] = Int(since.timeIntervalSince1970) }
        if let movedBy = work.movedBy { out["work_moved_by"] = movedBy }
        if let personNeeded = work.personNeeded { out["work_person_needed"] = personNeeded }
        if let owed = work.owed { out["owed"] = owed }
        if let disposition = work.disposition { out["disposition"] = disposition }
        // The fourth projection, additive and beside the other three. `ready` is about taking
        // work and this is about ending; a client that does not know the field is unaffected.
        let evidence = inventory ?? sessionInventoryEvidence()
        out["closeability"] = Orchestrator.sessionCloseability(
            identity: identity, terminalState: state,
            inventoryComplete: evidence.complete, inventoryObservedAt: evidence.observedAt,
            inventoryGeneration: evidence.generation,
            identityMatches: identityMatches ?? sessionIdentityMatchCount(for: identity),
            registrySnapshot: closeabilityRegistry).wire
        if let assistant = session.assistant { out["assistant"] = assistant.rawValue }
        if let assignment = Orchestrator.rootAssignmentSessionRecord(identity: identity) {
            out["root_assignment"] = assignment
        }
        let coordination = Orchestrator.coordination(forTerminal: session.id)
        if !coordination.waitingOn.isEmpty || !coordination.waitedOnBy.isEmpty {
            let waitingOn = coordination.waitingOn.map { raw -> [String: Any] in
                var row = raw
                if let id = row["ownerSessionId"] as? String,
                   let owner = publication.targets.first(where: { $0.id == id }) {
                    row["ownerLabel"] = publication.labels[owner.id] ?? owner.coordinate
                }
                return row
            }
            let waitedOnBy = coordination.waitedOnBy.map { raw -> [String: Any] in
                var row = raw
                if let id = row["waiterSessionId"] as? String,
                   let waiter = publication.targets.first(where: { $0.id == id }) {
                    row["waiterLabel"] = publication.labels[waiter.id] ?? waiter.coordinate
                }
                return row
            }
            out["coordination"] = [
                "state": waitingOn.isEmpty ? "has_waiters" : "waiting_on_session",
                "waitingOn": waitingOn, "waitedOnBy": waitedOnBy,
            ]
        }
        Self.attachCoordinator(to: &out, liveSession: Self.coordinatorProjectionSession(
            identity: identity,
            label: publication.labels[session.id] ?? session.coordinate,
            cwd: publishedIdentity?.workingDirectory ?? session.cwd, workState: work.state,
            waitingOnSession: !coordination.waitingOn.isEmpty,
            hasWaiters: !coordination.waitedOnBy.isEmpty,
            closeability: (out["closeability"] as? [String: Any])
                .flatMap { $0["state"] as? String }
                .flatMap(Orchestrator.SessionCloseability.init(rawValue:))))
        if case .working(let line) = state { out["line"] = line }
        if state == .waiting, let menu {
            // The page's transcript revision predates structured menus and watches `line`, not
            // `menu`. Waiting rows never display `line`, so this stable value is only a revision:
            // when the transcript-backed picker arrives after the waiting notification, the page
            // refetches immediately instead of waiting until the answer changes the state.
            out["line"] = Self.menuRevision(menu)
        }
        // The question itself, so a phone can answer it instead of being told to go and find a
        // Mac. Only ever present on a waiting session, and absent when the menu could not be
        // read — which the page has to handle anyway, because that is the old behaviour and it
        // is still what happens when a dialog is drawn in a shape this does not recognise.
        if let menu { out["menu"] = Self.menuObject(menu) }
        let agents = publication.agents[session.id] ?? []
        if !agents.isEmpty { out["agents"] = agents.map { json(of: $0) } }
        // The commands it left running, which are the reason an idle row is not a finished one.
        // Absent rather than empty, like the agents above: a page built before this existed
        // draws exactly what it drew before.
        let shells = publication.shells[session.id] ?? []
        if !shells.isEmpty { out["shells"] = shells.map { json(of: $0) } }
        if let cwd = publishedIdentity?.workingDirectory ?? session.cwd { out["cwd"] = cwd }
        if let sessionID = identity.conversationID {
            out["sessionId"] = sessionID
        }
        if let grid = publication.grids[session.id] { out["icon"] = json(of: grid) }
        return out
    }

    private static func name(of state: SessionState) -> String {
        switch state {
        case .working: return "working"
        case .waiting: return "waiting"
        case .idle:    return "idle"
        case .unknown: return "unknown"
        }
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

    /// One command still running where nobody can see it.
    ///
    /// `at` is an instant rather than an age, for the same reason an agent's is: the page already
    /// knows how to draw a clock from one, and an age worked out here would be wrong by however
    /// long the beat took to arrive over a tunnel.
    private func json(of shell: Shells.Shell) -> [String: Any] {
        var out: [String: Any] = [
            "id": shell.id,
            "at": Int(shell.at.timeIntervalSince1970),
        ]
        // What somebody asked for, which is the only thing here they can match against what they
        // remember asking for. Absent rather than empty when the two records it is joined from
        // straddled a read of the transcript — see `Shells.announced(in:)`.
        if !shell.command.isEmpty { out["command"] = shell.command }
        if let what = shell.what { out["what"] = what }
        if let doing = shell.doing { out["doing"] = doing }
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
        let publication = SessionWatch.shared.publishedInventory()
        if let agent = publication.agents[session.id]?.first(where: { $0.id == id }) {
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
        out["entries"] = Self.transcriptRows(
            Transcript.parse(text, assistant: .claude, limit: limit, sidechains: true)
        )
        out["signature"] = signature
        return .json(out)
    }

    /// What one background command has printed, plus the row the strip is showing for it.
    ///
    /// **Text and not entries.** Everything else on this server that a reader can open is a
    /// conversation, and a conversation has turns; this is a command's stdout, and the only
    /// honest shape for it is the bytes in the order they were written. `ended` is the fact the
    /// page cannot work out for itself — the last line under a finished command says so, and
    /// the strip only ever lists running ones, so a reader who has been watching one land needs
    /// this to be told it has.
    ///
    /// The tail is taken twice when the file moved under the first read, the same bargain
    /// `agentPayload` strikes: a command printing hard is the ordinary case here, not the rare
    /// one, and half a read is worse than a slightly older one.
    private func shellPayload(for session: TargetSession, shell id: String,
                              bytes: Int) -> Response {
        guard var read = Shells.output(of: session, id: id, bytes: bytes) else {
            return .error(404, "not_found", "No command named that")
        }
        if let fresh = Shells.output(of: session, id: id, bytes: bytes),
           fresh.signature != read.signature {
            read = fresh
        }
        var out: [String: Any] = [
            "text": read.text,
            "ended": read.ended,
            "at": Int(read.at.timeIntervalSince1970),
            "signature": read.signature,
        ]
        let publication = SessionWatch.shared.publishedInventory()
        if let shell = publication.shells[session.id]?.first(where: { $0.id == id }) {
            out["shell"] = json(of: shell)
        }
        return .json(out)
    }

    /// Entries as the page reads them. One shape for both transcripts, because a reader following
    /// an agent should meet the same pane they left.
    // Internal, and static, for direct serialization tests: the shape a row takes is a pure
    // function of the entries and nothing a running server owns.
    static func transcriptRows(_ entries: [Transcript.Entry]) -> [[String: Any]] {
        entries.map { entry -> [String: Any] in
            var row: [String: Any] = ["role": name(of: entry.kind), "text": entry.text]
            if let tool = entry.tool { row["tool"] = tool }
            if let time = entry.time { row["at"] = Int(time.timeIntervalSince1970) }
            // Current user rows are a closed image contract: zero means an authored literal
            // marker, while omission remains the legacy shape whose marker had to imply images.
            if entry.kind == .user { row["imageCount"] = entry.imageCount }
            if let source = entry.source, !source.isEmpty { row["source"] = source }
            if let mode = entry.sourceMode, !mode.isEmpty { row["sourceMode"] = mode }
            if let assistant = entry.sourceAssistant {
                row["sourceAssistant"] = assistant.rawValue
            }
            if !entry.artifacts.isEmpty {
                row["artifacts"] = entry.artifacts.map(\.object)
            }
            if let notice = entry.notice {
                row["notice"] = ClawdlineMessage.webObject(for: notice)
            }
            if !entry.fileChanges.isEmpty {
                row["fileChanges"] = entry.fileChanges.map { change -> [String: Any] in
                    var out: [String: Any] = ["path": change.path, "kind": change.kind]
                    if let diff = change.unifiedDiff { out["unifiedDiff"] = diff }
                    if let content = change.content { out["content"] = content }
                    if let destination = change.movePath { out["movePath"] = destination }
                    return out
                }
            }
            if !entry.plan.isEmpty {
                row["plan"] = entry.plan.map { ["step": $0.step, "status": $0.status] }
            }
            if let activity = entry.activity {
                var out: [String: Any] = ["kind": activity.kind]
                if let title = activity.title { out["title"] = title }
                if let status = activity.status { out["status"] = status }
                if let duration = activity.durationMilliseconds { out["durationMs"] = duration }
                if let result = activity.result { out["result"] = result }
                out["actions"] = activity.actions.map { action -> [String: Any] in
                    var value: [String: Any] = ["kind": action.kind]
                    if let command = action.command { value["command"] = command }
                    if let name = action.name { value["name"] = name }
                    if let path = action.path { value["path"] = path }
                    if let query = action.query { value["query"] = query }
                    return value
                }
                row["activity"] = out
            }
            return row
        }
    }

    private static func name(of kind: Transcript.Entry.Kind) -> String {
        switch kind {
        case .user:       return "user"
        case .assistant:  return "assistant"
        case .peer:       return "peer"
        case .message:    return "message"
        case .notice:     return "notice"
        case .tool:       return "tool"
        case .toolResult: return "tool"
        }
    }

    // MARK: - The event stream

    private final class Stream {
        let connection: NWConnection
        init(_ connection: NWConnection) { self.connection = connection }
    }

    static func restartHelloPayload() -> [String: Any] {
        [
            "version": Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "?",
            "build": Self.buildStamp,
            "instance": Orchestrator.appInstanceID,
            "protocol": Self.protocolVersion,
            "write": Config.shared.remoteWrite,
        ]
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
                if self?.streams.isEmpty == true { self?.transcriptRevisionStream.stop() }
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
        write(event: "hello", data: Self.restartHelloPayload(), to: stream)
        DispatchQueue.main.async {
            let payload = self.sessionsPayload()
            let targets = SessionWatch.shared.targets
            // The task list rides the same stream: a page that reconnects is level on both
            // without asking, for the same reason the whole session list goes out above.
            let tasks: [String: Any] = ["tasks": Orchestrator.records(),
                                        "at": Int(Date().timeIntervalSince1970)]
            self.queue.async {
                self.write(event: "sessions", data: payload, to: stream)
                self.write(event: "orchestrator", data: tasks, to: stream)
                self.transcriptRevisionStream.sync(targets: targets, active: !self.streams.isEmpty)
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
        let targets = SessionWatch.shared.targets
        let cloudPayload = try? JSONSerialization.data(withJSONObject: payload,
                                                        options: [.withoutEscapingSlashes])
        queue.async { [weak self] in
            guard let self else { return }
            for stream in self.streams.values { self.write(event: "sessions", data: payload, to: stream) }
            self.transcriptRevisionStream.sync(targets: targets, active: !self.streams.isEmpty)
            if let bridge = self.cloudBridge, let cloudPayload {
                self.enqueueCloudPublication {
                    try await bridge.publishSessions(cloudPayload)
                }
            }
        }
    }

    /// Called by the orchestrator whenever any task record changes, from whichever thread it
    /// changed on — `records()` does its own main-thread crossing.
    func broadcastOrchestrator() {
        let payload: [String: Any] = ["tasks": Orchestrator.records(),
                                      "at": Int(Date().timeIntervalSince1970)]
        let cloudPayload = try? JSONSerialization.data(withJSONObject: payload,
                                                        options: [.withoutEscapingSlashes])
        queue.async { [weak self] in
            guard let self else { return }
            for stream in self.streams.values {
                self.write(event: "orchestrator", data: payload, to: stream)
            }
            if let bridge = self.cloudBridge, let cloudPayload {
                self.enqueueCloudPublication {
                    try await bridge.publishOrchestrator(cloudPayload)
                }
            }
        }
    }

    /// Preserve observation order before calls cross into the bridge actor. Full snapshots make
    /// reconnect replay unnecessary, but an older reading must not acquire a newer sequence.
    private func enqueueCloudPublication(_ work: @escaping @Sendable () async throws -> Void) {
        let previous = cloudPublishTail
        cloudPublishTail = Task {
            await withTaskCancellationHandler {
                await previous?.value
                guard !Task.isCancelled else { return }
                try? await work()
            } onCancel: {
                // The tail owns the complete serial chain. Cancelling it recursively wakes queued
                // predecessors, while bridge.stop() cancels and joins work already past entry.
                previous?.cancel()
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

    private static let responseCloseGraceSeconds = 30

    private func send(_ response: Response, on conn: NWConnection) {
        // `cancel()` is an abort, not HTTP's `Connection: close`. Curl accepted the complete
        // bytes it had already read, but Chrome treated the reset that followed a token-adoption
        // 303 as ERR_FAILED and never followed it. A complete final context asks TCP for the
        // write-close (FIN) that the header promises. Afterwards, drain through the peer's FIN
        // before releasing the connection; the backstop bounds a client that never closes.
        let backstop = DispatchWorkItem { conn.cancel() }
        queue.asyncAfter(deadline: .now() + .seconds(Self.responseCloseGraceSeconds),
                         execute: backstop)
        conn.send(content: response.wire,
                  contentContext: .finalMessage,
                  isComplete: true,
                  completion: .contentProcessed { [weak self] error in
                      guard error == nil, let self else {
                          backstop.cancel()
                          conn.cancel()
                          return
                      }
                      self.awaitPeerClose(on: conn, backstop: backstop)
                  })
    }

    private func awaitPeerClose(on conn: NWConnection, backstop: DispatchWorkItem) {
        conn.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) {
            [weak self] _, _, done, error in
            guard error == nil, !done, let self else {
                backstop.cancel()
                conn.cancel()
                return
            }
            // `Connection: close` makes another request on this socket invalid, but drain any
            // bytes already in flight so local cancellation cannot turn the completed response
            // back into the reset Chrome rejected.
            self.awaitPeerClose(on: conn, backstop: backstop)
        }
    }
}

// MARK: - The two halves of HTTP that this needs

extension RemoteServer {

    struct Request {
        enum Source {
            case http
            case verifiedCloud(sender: String)
        }

        var source: Source = .http
        var method = "GET"
        var path = "/"
        var query: [String: String] = [:]
        var repeatedQueryKeys: Set<String> = []
        var headers: [String: String] = [:]
        var contentLength = 0
        var body: Data = Data()

        /// There is deliberately no header spelling for this initializer. Only the in-process
        /// CloudAppBridge can name a verified-cloud source.
        init(verifiedCloud command: CloudHeadlessCommand, sender: String,
             idempotencyKey: String) {
            source = .verifiedCloud(sender: sender)
            method = "POST"
            headers = ["idempotency-key": idempotencyKey]
            let segment: String
            let object: [String: Any]
            switch command {
            case .send(let session, let text, let images):
                segment = CloudAppBridge.channelSegment(session)
                path = "/v1/sessions/\(segment)/send"
                object = ["text": text, "images": images]
            case .answer(let session, let key):
                segment = CloudAppBridge.channelSegment(session)
                path = "/v1/sessions/\(segment)/key"
                object = ["key": key]
            }
            body = (try? JSONSerialization.data(withJSONObject: object,
                                                 options: [.withoutEscapingSlashes])) ?? Data()
            contentLength = body.count
        }

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
                    if query[key] != nil { repeatedQueryKeys.insert(key) }
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
            case 410: return "Gone"
            case 415: return "Unsupported Media Type"
            case 409: return "Conflict"
            case 429: return "Too Many Requests"
            case 413: return "Payload Too Large"
            case 431: return "Request Header Fields Too Large"
            case 503: return "Service Unavailable"
            default:  return "Error"
            }
        }
    }
}
