import Foundation

// MARK: - Which Cloud Session rows go out, and what they say about the transcript

/// A controllable `nowMilliseconds` for the bridge.
private final class CloudRowClock: @unchecked Sendable {
    private let lock = NSLock()
    private var value: UInt64

    init(_ value: UInt64) { self.value = value }

    func now() -> UInt64 {
        lock.lock(); defer { lock.unlock() }
        return value
    }

    func advance(_ milliseconds: UInt64) {
        lock.lock(); value += milliseconds; lock.unlock()
    }
}

/// The bridge's republication wait, held until the test releases it and recording what it asked.
private final class CloudRowWaits: @unchecked Sendable {
    private let lock = NSLock()
    private var asked: [UInt64] = []
    private var released = 0

    func wait(_ milliseconds: UInt64) async throws {
        let turn = ask(milliseconds)
        while !isReleased(turn) { try await Task.sleep(nanoseconds: 2_000_000) }
    }

    private func ask(_ milliseconds: UInt64) -> Int {
        lock.lock(); defer { lock.unlock() }
        asked.append(milliseconds)
        return asked.count
    }

    private func isReleased(_ turn: Int) -> Bool {
        lock.lock(); defer { lock.unlock() }
        return released >= turn
    }

    func release() {
        lock.lock(); released += 1; lock.unlock()
    }

    var requests: [UInt64] {
        lock.lock(); defer { lock.unlock() }
        return asked
    }
}

/// A signature source the test speaks for.
private final class CloudRowSignatureSource: CloudTranscriptSignatureSource, @unchecked Sendable {
    private let lock = NSLock()
    private var report: CloudTranscriptSignatureReport?
    private var tracked: [[CloudTranscriptSubject]] = []
    private var stops = 0

    func track(_ subjects: [CloudTranscriptSubject],
               report: @escaping CloudTranscriptSignatureReport) {
        lock.lock(); tracked.append(subjects); self.report = report; lock.unlock()
    }

    func stop() {
        lock.lock(); stops += 1; report = nil; lock.unlock()
    }

    func send(_ sessionID: String, _ signature: String?) {
        lock.lock(); let current = report; lock.unlock()
        current?(sessionID, signature)
    }

    var trackCalls: [[CloudTranscriptSubject]] {
        lock.lock(); defer { lock.unlock() }
        return tracked
    }

    var stopCount: Int {
        lock.lock(); defer { lock.unlock() }
        return stops
    }
}

private final class CloudRowOutcome<Value>: @unchecked Sendable {
    private let lock = NSLock()
    private var value: Result<Value, Error>?

    func set(_ result: Result<Value, Error>) {
        lock.lock(); value = result; lock.unlock()
    }

    var result: Result<Value, Error>? {
        lock.lock(); defer { lock.unlock() }
        return value
    }
}

/// Run async bridge work from a synchronous group, keeping the main queue turning meanwhile.
@discardableResult
private func cloudRowAwait<Value>(
    _ label: String, recording: Bool = true,
    _ body: @escaping @Sendable () async throws -> Value
) -> Value? {
    let outcome = CloudRowOutcome<Value>()
    Task.detached {
        do { outcome.set(.success(try await body())) } catch { outcome.set(.failure(error)) }
    }
    let finished = eventually(timeout: 5) { outcome.result != nil }
    switch outcome.result {
    case .success(let value)?:
        if recording { check("\(label) completes", true) }
        return value
    case .failure(let error)?:
        check("\(label) completes", false, "threw \(error)")
    case nil:
        check("\(label) completes", finished, "timed out")
    }
    return nil
}

private struct CloudRowFixture {
    let transport: CloudAppBridgeTestTransport
    let clock: CloudRowClock
    let secret: CloudMasterSecret
    let signer: CloudDeviceKeyPair
    let bridge: CloudAppBridge

    init(source: CloudRowSignatureSource? = nil, waits: CloudRowWaits? = nil) {
        let transport = CloudAppBridgeTestTransport()
        let clock = CloudRowClock(1_800_000_000_000)
        let secret = try! CloudMasterSecret(rawRepresentation: Data(repeating: 0x5c, count: 32))
        let signer = CloudDeviceKeyPair()
        self.transport = transport
        self.clock = clock
        self.secret = secret
        self.signer = signer
        bridge = CloudAppBridge(
            transport: transport,
            identity: CloudAppIdentity(machineID: "mac-rows", deviceID: "mac-rows-device",
                                       keyID: "ms-1", masterSecret: secret, signingKey: signer),
            sequencing: CloudAppBridgeTestSequence(),
            nowMilliseconds: { clock.now() },
            transcriptSignatures: source,
            waitMilliseconds: { milliseconds in
                if let waits { try await waits.wait(milliseconds) }
            })
    }

    /// Every opened payload published on `s/mac-rows/<session>`, oldest first.
    func frames(_ session: String) -> [[String: Any]] {
        let signer = self.signer
        return transport.envelopes().filter { $0.ch == "s/mac-rows/" + session }.compactMap {
            (try? $0.open(masterSecret: secret, publicKeyForSender: {
                $0 == "mac-rows-device" ? signer.publicKeyRaw : nil
            })).flatMap { (try? JSONSerialization.jsonObject(with: $0)) as? [String: Any] }
        }
    }

    func lastRow(_ session: String) -> [String: Any]? {
        frames(session).last?["session"] as? [String: Any]
    }

    func publish(_ rows: [[String: Any]], at: Int, generation: Int, label: String) {
        let payload = try! JSONSerialization.data(withJSONObject: [
            "sessions": rows, "at": at,
            "scan": ["generation": generation, "complete": true,
                     "emptyAuthoritative": rows.isEmpty] as [String: Any],
        ])
        let bridge = self.bridge
        cloudRowAwait(label) { try await bridge.publishSessions(payload) }
    }
}

/// A row with the closeability block the Mac actually serializes.
private func cloudRow(_ id: String, state: String = "idle", observedAt: Int, generation: Int,
                      sourceObservedAt: Int, freshness: String = "current") -> [String: Any] {
    [
        "id": id, "label": "Row \(id)", "state": state, "backend": "tmux", "tty": "ttys0\(id.count)",
        "assistant": "claude", "cwd": "/work/\(id)", "work_state": "ready",
        "closeability": [
            "state": "blocked",
            "reasons": [["code": "task_running", "kind": "obligation",
                         "mover": ["kind": "session", "self": true, "person_needed": false]]],
            "observed_at": observedAt, "session_generation": generation,
            "activity_generation": 3, "obligation_generation": 9, "version": "cl1_rows",
            "provenance": ["broker"], "attestation_id": NSNull(), "mover": NSNull(),
            "source": ["provenance": "session_watch", "freshness": freshness,
                       "observed_at": sourceObservedAt, "max_age_seconds": 120] as [String: Any],
        ] as [String: Any],
    ]
}

private func closeabilityValue(_ row: [String: Any]?, _ path: String...) -> Int? {
    var object = row?["closeability"] as? [String: Any]
    for key in path.dropLast() { object = object?[key] as? [String: Any] }
    return path.last.flatMap { object?[$0] as? Int }
}

func runCloudSessionRowTests() {
group("Cloud Session rows skip freshness-only changes and republish the whole row when a read field moves") {
    let fixture = CloudRowFixture()
    let bridge = fixture.bridge
    cloudRowAwait("the row bridge starts") { try await bridge.start() }
    fixture.publish([cloudRow("alpha", observedAt: 100, generation: 1, sourceObservedAt: 99),
                     cloudRow("beta", observedAt: 100, generation: 1, sourceObservedAt: 99)],
                    at: 100, generation: 1, label: "the first scan")
    expect("the first scan publishes both rows and the inventory",
           fixture.transport.envelopes().count, 3)

    // The measured defect: every SessionWatch reading re-sealed every row for these three alone.
    fixture.clock.advance(20_000)
    fixture.publish([cloudRow("alpha", observedAt: 120, generation: 2, sourceObservedAt: 119),
                     cloudRow("beta", observedAt: 120, generation: 2, sourceObservedAt: 119)],
                    at: 120, generation: 2, label: "a freshness-only scan")
    expect("a scan differing only in closeability observed_at, session_generation and source.observed_at publishes nothing",
           fixture.transport.envelopes().count, 3)

    fixture.clock.advance(20_000)
    var working = cloudRow("alpha", state: "working", observedAt: 140, generation: 3,
                           sourceObservedAt: 139)
    working["line"] = "Working (1s • esc to interrupt)"
    fixture.publish([working,
                     cloudRow("beta", observedAt: 140, generation: 3, sourceObservedAt: 139)],
                    at: 140, generation: 3, label: "a scan with one read field changed")
    expect("a changed read field publishes exactly that row", fixture.frames("alpha").count, 2)
    expect("its unchanged neighbour stays quiet", fixture.frames("beta").count, 1)
    let current = fixture.lastRow("alpha")
    check("the published row is the complete current row, freshness values included",
          current?["state"] as? String == "working"
            && current?["line"] as? String == "Working (1s • esc to interrupt)"
            && current?["label"] as? String == "Row alpha"
            && closeabilityValue(current, "observed_at") == 140
            && closeabilityValue(current, "session_generation") == 3
            && closeabilityValue(current, "source", "observed_at") == 139,
          "\(current ?? [:])")
    check("the publication keeps the scan's own at and scan beside the row",
          fixture.frames("alpha").last?["at"] as? Int == 140
            && (fixture.frames("alpha").last?["scan"] as? [String: Any])?["generation"] as? Int == 3)

    fixture.clock.advance(20_000)
    fixture.publish([working,
                     cloudRow("beta", observedAt: 160, generation: 4, sourceObservedAt: 159,
                              freshness: "stale")],
                    at: 160, generation: 4, label: "a scan whose source went stale")
    expect("source.freshness is read by the viewer's closeability gate, so it still publishes",
           fixture.frames("beta").count, 2)

    // Presence: nothing else keeps an idle Mac inside the console's five-minute machine window.
    let beforeQuiet = fixture.transport.envelopes().count
    fixture.clock.advance(CloudAppBridge.sessionPresenceIntervalMilliseconds - 1)
    fixture.publish([working,
                     cloudRow("beta", observedAt: 170, generation: 5, sourceObservedAt: 169,
                              freshness: "stale")],
                    at: 170, generation: 5, label: "an idle scan inside the presence interval")
    expect("an idle scan inside the presence interval publishes nothing",
           fixture.transport.envelopes().count, beforeQuiet)
    fixture.clock.advance(1)
    fixture.publish([working,
                     cloudRow("beta", observedAt: 180, generation: 6, sourceObservedAt: 179,
                              freshness: "stale")],
                    at: 180, generation: 6, label: "an idle scan at the presence interval")
    expect("three quiet minutes re-send one frame", fixture.transport.envelopes().count,
           beforeQuiet + 1)
    check("and that frame is the inventory marker, not a row",
          fixture.transport.envelopes().last?.ch == "s/mac-rows/__clawdline_inventory_v1__"
            && fixture.frames("alpha").count == 2 && fixture.frames("beta").count == 2)
    check("the freshness-only list names exactly the three audited paths",
          CloudAppBridge.freshnessOnlySessionRowFields
            == [["closeability", "observed_at"], ["closeability", "session_generation"],
                ["closeability", "source", "observed_at"]])
    cloudRowAwait("the row bridge stops") { await bridge.stop() }
}

group("Cloud Session rows carry a debounced transcript signature, and local rows never do") {
    let source = CloudRowSignatureSource()
    let waits = CloudRowWaits()
    let fixture = CloudRowFixture(source: source, waits: waits)
    let bridge = fixture.bridge
    func state() -> (known: [String: String], pending: [String], scheduled: Bool) {
        cloudRowAwait("reading the demand", recording: false) {
            await bridge.transcriptSignatureStateForTesting()
        }
            ?? ([:], [], false)
    }
    cloudRowAwait("the signature bridge starts") { try await bridge.start() }
    expect("a started bridge has asked for no transcript yet", source.trackCalls.count, 0)
    let alpha = cloudRow("alpha", observedAt: 100, generation: 1, sourceObservedAt: 99)
    var beta = cloudRow("beta", observedAt: 100, generation: 1, sourceObservedAt: 99)
    fixture.publish([alpha, beta], at: 100, generation: 1, label: "the first signature scan")
    check("a running publication names every published Session to the source",
          source.trackCalls.last?.map(\.sessionID) == ["alpha", "beta"]
            && source.trackCalls.last?.first?.binding == "claude\u{1}ttys05\u{1}-\u{1}/work/alpha")
    check("an unknown signature is omitted, never an empty string",
          fixture.lastRow("alpha").map { $0["transcript_signature"] == nil } == true)

    source.send("alpha", "100-1")
    check("a signature change republishes the row at once when no pass ran recently",
          eventually { fixture.frames("alpha").count == 2 })
    check("with the signature and the whole current row",
          fixture.lastRow("alpha")?["transcript_signature"] as? String == "100-1"
            && fixture.lastRow("alpha")?["label"] as? String == "Row alpha"
            && closeabilityValue(fixture.lastRow("alpha"), "observed_at") == 100)
    expect("the first pass did not wait", waits.requests, [])

    // A burst: one unchanged report and three changes inside the interval become one pass.
    fixture.clock.advance(10)
    source.send("alpha", "100-1")
    source.send("alpha", "200-2")
    source.send("alpha", "300-3")
    source.send("beta", "50-1")
    check("the burst is taken in and waits out the rest of the interval",
          eventually {
              let now = state()
              return now.known["alpha"] == "300-3" && now.known["beta"] == "50-1"
                && waits.requests == [990]
          }, "\(state()) waits=\(waits.requests)")
    check("nothing is published while the interval runs",
          fixture.frames("alpha").count == 2 && fixture.frames("beta").count == 1)
    waits.release()
    check("the released pass publishes each changed row once",
          eventually { fixture.frames("alpha").count == 3 && fixture.frames("beta").count == 2 })
    check("carrying the newest signature of the burst",
          fixture.lastRow("alpha")?["transcript_signature"] as? String == "300-3"
            && fixture.lastRow("beta")?["transcript_signature"] as? String == "50-1")
    check("and it settles with nothing pending",
          eventually { let now = state(); return now.pending.isEmpty && !now.scheduled })
    expect("one wait covered the whole burst", waits.requests, [990])
    expect("an unchanged signature republished nothing", fixture.frames("alpha").count, 3)

    fixture.clock.advance(1_000)
    source.send("alpha", nil)
    check("a signature that becomes unknown republishes the row",
          eventually { fixture.frames("alpha").count == 4 })
    check("and that row omits the field",
          fixture.lastRow("alpha").map { $0["transcript_signature"] == nil } == true)
    fixture.clock.advance(1_000)
    source.send("alpha", "")
    source.send("beta", "60-1")
    check("an empty report is still unknown, so only the other row moves",
          eventually { fixture.frames("beta").count == 3 } && fixture.frames("alpha").count == 4)

    fixture.clock.advance(20_000)
    beta["state"] = "working"
    fixture.publish([alpha, beta], at: 120, generation: 2, label: "a scan after signatures")
    check("a scan publication carries the signature it knows",
          fixture.frames("beta").count == 4
            && fixture.lastRow("beta")?["transcript_signature"] as? String == "60-1")
    expect("and an unchanged row with an unknown signature stays quiet",
           fixture.frames("alpha").count, 4)

    fixture.publish([beta], at: 140, generation: 3, label: "a scan that closes alpha")
    check("the closed Session is tombstoned",
          fixture.frames("alpha").count == 5 && fixture.frames("alpha").last?["deleted"] as? Bool == true)
    check("and is no longer named to the source",
          source.trackCalls.last?.map(\.sessionID) == ["beta"])
    fixture.clock.advance(2_000)
    source.send("alpha", "900-9")
    source.send("beta", "61-1")
    check("a late report for a tombstoned Session cannot resurrect its row",
          eventually { fixture.frames("beta").count == 5 } && fixture.frames("alpha").count == 5)

    cloudRowAwait("the signature bridge stops") { await bridge.stop() }
    check("stopping the bridge ends the source's demand", source.stopCount >= 1)

    // Local rows: the one serializer both `/v1/sessions` and SSE use never names the field.
    let remoteSource = (try? String(contentsOfFile: "Sources/RemoteServer.swift", encoding: .utf8)) ?? ""
    check("the local Session serializer never spells transcript_signature",
          !remoteSource.isEmpty && !remoteSource.contains("transcript_signature"))
    let phone = RemoteAuth.addDevice(name: "cloud row local phone", caps: [.read])
    defer {
        RemoteAuth.revoke(id: phone.id)
        RemoteServer.sessionPayloadForTesting = nil
    }
    RemoteServer.sessionPayloadForTesting = (
        [TargetSession(backend: .tmux, id: "%881", name: "local", tty: "/dev/ttys881",
                       windowIndex: 0, tabIndex: 0, assistant: .claude, cwd: "/work/local")],
        ["%881": .idle])
    let local = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/sessions", headers: ["Authorization": "Bearer \(phone.token)"]))
    let localRows = ((try? JSONSerialization.jsonObject(with: local.body)) as? [String: Any])?[
        "sessions"] as? [[String: Any]] ?? []
    check("a local /v1/sessions row never carries transcript_signature",
          local.status == 200 && localRows.count == 1
            && localRows.allSatisfy { $0["transcript_signature"] == nil },
          "status \(local.status), rows \(localRows)")
}

group("the Cloud transcript signature watch reports current, changed and ended signatures") {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-cloud-rows-\(UUID().uuidString)", isDirectory: true)
    try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let file = directory.appendingPathComponent("alpha.jsonl")
    try? Data("{\"type\":\"user\"}\n".utf8).write(to: file)
    let target = TargetSession(backend: .tmux, id: "%882", name: "alpha", tty: "/dev/ttys882",
                               windowIndex: 0, tabIndex: 0, assistant: .claude, cwd: directory.path)
    let reports = CloudRowOutcome<[(String, String?)]>()
    reports.set(.success([]))
    func received() -> [(String, String?)] { (try? reports.result?.get()) ?? [] }
    let resolutions = CloudRowClock(0)
    let uptime = CloudRowClock(1_000)
    let watch = CloudTranscriptSignatureWatch(
        targets: { [target] },
        resolve: { session in resolutions.advance(1); return session.id == "%882" ? file : nil },
        uptime: { TimeInterval(uptime.now()) })
    let subjects = [CloudTranscriptSubject(sessionID: "%882", binding: "claude")]
    let collect: CloudTranscriptSignatureReport = { id, signature in
        reports.set(.success(((try? reports.result?.get()) ?? []) + [(id, signature)]))
    }
    watch.track(subjects, report: collect)
    let initial = Transcript.signature(of: file)
    check("a new watch reports the signature its file has now",
          eventually { received().contains { $0.0 == "%882" && $0.1 == initial } },
          "\(received())")
    watch.track(subjects, report: collect)
    check("the same Sessions and bindings inside the rebind interval resolve nothing again",
          eventually(timeout: 0.3) { resolutions.now() > 1 } == false)

    let handle = try? FileHandle(forWritingTo: file)
    handle?.seekToEndOfFile()
    handle?.write(Data("{\"type\":\"assistant\",\"message\":\"more\"}\n".utf8))
    try? handle?.close()
    check("an appended transcript reports the signature the transcript read answer would carry",
          eventually { received().last?.1 == Transcript.signature(of: file) && received().last?.1 != initial },
          "\(received())")

    try? FileManager.default.removeItem(at: file)
    check("a removed transcript reports an unknown signature",
          eventually { received().last.map { $0.0 == "%882" && $0.1 == nil } == true },
          "\(received())")
    uptime.advance(1)
    watch.track(subjects, report: collect)
    check("an ended watch makes the next track resolve again",
          eventually { resolutions.now() >= 2 })
    watch.stop()

    // The local event stream keeps its change-only contract.
    let localFile = directory.appendingPathComponent("local.jsonl")
    try? Data("{}\n".utf8).write(to: localFile)
    let localReports = CloudRowOutcome<[String]>()
    localReports.set(.success([]))
    let stream = TranscriptRevisionStream(
        changed: { _, signature in
            localReports.set(.success(((try? localReports.result?.get()) ?? []) + [signature]))
        },
        resolve: { _ in localFile })
    stream.sync(targets: [target], active: true)
    _ = eventually(timeout: 1) { false }
    let appended = try? FileHandle(forWritingTo: localFile)
    appended?.seekToEndOfFile()
    appended?.write(Data("{\"more\":true}\n".utf8))
    try? appended?.close()
    check("the local stream announces a change",
          eventually { ((try? localReports.result?.get()) ?? []).count == 1 })
    check("and never the signature a watch starts with",
          ((try? localReports.result?.get()) ?? []) == [Transcript.signature(of: localFile)])
    stream.stop()
}
}
