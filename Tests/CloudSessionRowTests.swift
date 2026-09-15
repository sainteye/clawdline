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

    init(source: CloudRowSignatureSource? = nil, waits: CloudRowWaits? = nil,
         status: CloudStatus? = nil) {
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
            status: status,
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

    /// Every answer published on the machine reply channel, by its `read` name (the last one wins).
    func replies() -> [String: [String: Any]] {
        let signer = self.signer
        var out: [String: [String: Any]] = [:]
        for envelope in transport.envelopes() where envelope.ch == "t/mac-rows/__clawdline_machine__" {
            guard let opened = try? envelope.open(masterSecret: secret, publicKeyForSender: {
                      $0 == "mac-rows-device" ? signer.publicKeyRaw : nil
                  }),
                  let object = (try? JSONSerialization.jsonObject(with: opened)) as? [String: Any],
                  let read = object["read"] as? String else { continue }
            out[read] = object
        }
        return out
    }

    /// A viewer's `sessions.snapshot` on this Mac's own `ctl/` channel.
    func askForRows(_ request: String, sequence: UInt64, extra: String = "") {
        transport.yield(#"{"type":"sessions.snapshot","session":"__clawdline_machine__","request":""#
                        + request + "\"" + extra + "}", sequence: sequence, channel: "ctl/mac-rows")
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
    // A folder that changed moments before a pass leaves that pass unsettled (it may have listed
    // the folder first), and this fixture is not about that; see the switch below.
    func backdate(_ url: URL, seconds: TimeInterval) {
        try? FileManager.default.setAttributes(
            [.modificationDate: Date(timeIntervalSinceNow: -seconds)], ofItemAtPath: url.path)
    }
    backdate(directory, seconds: 120)
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

    // M1(a): a conversation that goes on in a new file, on a row with no conversation id, moves
    // none of the facts `track` compares; only the folder the new file lands in says so.
    let folder = directory.appendingPathComponent("switch", isDirectory: true)
    try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    let first = folder.appendingPathComponent("first.jsonl")
    let second = folder.appendingPathComponent("second.jsonl")
    let third = folder.appendingPathComponent("third.jsonl")
    try? Data("{\"n\":1}\n".utf8).write(to: first)
    backdate(first, seconds: 120)
    backdate(folder, seconds: 120)
    let racing = CloudRowClock(0)
    let switchUptime = CloudRowClock(5_000)
    let switchReports = CloudRowOutcome<[(String, String?)]>()
    switchReports.set(.success([]))
    func switched() -> [(String, String?)] { (try? switchReports.result?.get()) ?? [] }
    let switchWatch = CloudTranscriptSignatureWatch(
        targets: { [target] },
        resolve: { _ in
            // Transcript.locate's own answer when nothing names the file: the newest one.
            let names = (try? FileManager.default.contentsOfDirectory(atPath: folder.path)) ?? []
            let files: [URL] = names.filter { $0.hasSuffix(".jsonl") }
                .map { folder.appendingPathComponent($0) }
            let dated: [(url: URL, at: Date)] = files.map { url in
                let attributes = try? FileManager.default.attributesOfItem(atPath: url.path)
                return (url: url, at: (attributes?[.modificationDate] as? Date) ?? .distantPast)
            }
            let chosen: URL? = dated.max { $0.at < $1.at }?.url
            if racing.now() == 1 {
                // A new conversation's file lands after this pass has listed the folder.
                racing.advance(1)
                try? Data("{\"n\":333}\n".utf8).write(to: third)
            }
            return chosen
        },
        uptime: { TimeInterval(switchUptime.now()) })
    let collectSwitch: CloudTranscriptSignatureReport = { id, signature in
        switchReports.set(.success(((try? switchReports.result?.get()) ?? []) + [(id, signature)]))
    }
    switchWatch.track(subjects, report: collectSwitch)
    check("a watch on a conversation that will switch files reports its first file",
          eventually { switched().last?.1 == Transcript.signature(of: first) }, "\(switched())")
    try? Data("{\"n\":22}\n".utf8).write(to: second)
    backdate(second, seconds: 60)
    switchWatch.track(subjects, report: collectSwitch)
    check("a newer transcript beside the old one is followed on the next track with unchanged facts",
          eventually { switched().last?.1 == Transcript.signature(of: second) }, "\(switched())")

    // The same switch landing while a pass reads the folder: that pass chose before the file
    // arrived, and the folder's stamp already includes it, so the stamp cannot be trusted.
    switchUptime.advance(UInt64(CloudTranscriptSignatureWatch.rebindIntervalSeconds) + 1)
    racing.advance(1)
    switchWatch.track(subjects, report: collectSwitch)
    _ = eventually { racing.now() == 2 }
    check("a transcript that lands while its folder is being resolved is followed on a later track",
          eventually {
              switchWatch.track(subjects, report: collectSwitch)
              return switched().last?.1 == Transcript.signature(of: third)
          }, "\(switched())")
    switchWatch.stop()

    // M1(b): appends that never pause for the quiet interval still report inside the maximum.
    let standard = TranscriptRevisionWatch.Debounce.standard
    check("a transcript report waits 90 ms for quiet and never more than a second",
          standard.quietMilliseconds == 90 && standard.maximumMilliseconds == 1_000)
    let burstFile = directory.appendingPathComponent("burst.jsonl")
    try? Data("{}\n".utf8).write(to: burstFile)
    let burstReports = CloudRowOutcome<[(String, String?)]>()
    burstReports.set(.success([]))
    func bursts() -> [(String, String?)] { (try? burstReports.result?.get()) ?? [] }
    // Wider than the standard values, so a busy machine's scheduling cannot pass for a pause.
    let burstWatch = CloudTranscriptSignatureWatch(
        targets: { [target] }, resolve: { _ in burstFile }, uptime: { 1_000 },
        debounce: TranscriptRevisionWatch.Debounce(quietMilliseconds: 300, maximumMilliseconds: 600))
    burstWatch.track(subjects) { id, signature in
        burstReports.set(.success(((try? burstReports.result?.get()) ?? []) + [(id, signature)]))
    }
    check("a watch on a file about to be written continuously reports it first",
          eventually { bursts().count == 1 }, "\(bursts())")
    let writer = try? FileHandle(forWritingTo: burstFile)
    let burstBegan = Date()
    while Date().timeIntervalSince(burstBegan) < 2 {
        writer?.seekToEndOfFile()
        writer?.write(Data("{\"delta\":true}\n".utf8))
        usleep(40_000)
    }
    let duringBurst = bursts().count - 1
    try? writer?.close()
    check("appends closer together than the quiet interval still report at each maximum delay",
          duringBurst >= 2, "\(duringBurst) during the burst: \(bursts())")
    check("and no more often than that while they last",
          duringBurst <= 4, "\(duringBurst) during the burst: \(bursts())")
    check("the last append is reported once the appends stop",
          eventually { bursts().last?.1 == Transcript.signature(of: burstFile) }, "\(bursts())")
    burstWatch.stop()

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

group("a reconnecting viewer's Session snapshot request re-sends every row once per window, and an incomplete scan keeps the Mac present") {
    let waits = CloudRowWaits()
    let status = CloudStatus()
    let fixture = CloudRowFixture(waits: waits, status: status)
    let bridge = fixture.bridge
    cloudRowAwait("the snapshot bridge starts") { try await bridge.start() }
    let orchestrator = try! JSONSerialization.data(withJSONObject: [
        "tasks": [] as [Any], "machine": ["name": "Mac", "platform": "macos"] as [String: Any],
    ])
    cloudRowAwait("the Mac's orchestrator snapshot") { try await bridge.publishOrchestrator(orchestrator) }
    func orchestratorFrames() -> [[String: Any]] {
        let signer = fixture.signer
        return fixture.transport.envelopes().filter { $0.ch == "orch/mac-rows" }.compactMap {
            (try? $0.open(masterSecret: fixture.secret, publicKeyForSender: {
                $0 == "mac-rows-device" ? signer.publicKeyRaw : nil
            })).flatMap { (try? JSONSerialization.jsonObject(with: $0)) as? [String: Any] }
        }
    }
    check("the orchestrator snapshot's cloud_status lists what this Mac answers",
          (orchestratorFrames().last?["cloud_status"] as? [String: Any])?["features"] as? [String]
            == [CloudAppBridge.sessionSnapshotType], "\(orchestratorFrames())")
    let alpha = cloudRow("alpha", observedAt: 100, generation: 1, sourceObservedAt: 99)
    let beta = cloudRow("beta", observedAt: 100, generation: 1, sourceObservedAt: 99)
    fixture.publish([alpha, beta], at: 100, generation: 1, label: "the idle Mac's only scan")
    let inventory = fixture.frames("__clawdline_inventory_v1__").last
    check("the inventory lists what this Mac answers beside the ids, never inside them",
          inventory?["features"] as? [String] == [CloudAppBridge.sessionSnapshotType]
            && (inventory?["inventory"] as? [String: Any]).map { Set($0.keys) } == ["version", "sessions"],
          "\(inventory ?? [:])")
    fixture.clock.advance(20_000)
    fixture.publish([cloudRow("alpha", observedAt: 120, generation: 2, sourceObservedAt: 119),
                     cloudRow("beta", observedAt: 120, generation: 2, sourceObservedAt: 119)],
                    at: 120, generation: 2, label: "an idle scan")
    expect("an idle Mac publishes nothing more on its own — the relay's replay is all a new viewer would get",
           fixture.transport.envelopes().count, 4)

    // (a) A viewer connecting after the relay lost its replay asks; remote writes are off here.
    fixture.askForRows("snap-1", sequence: 1)
    check("asked for its rows, an idle Mac re-sends each row and the inventory at once",
          eventually { fixture.frames("alpha").count == 2 && fixture.frames("beta").count == 2
            && fixture.frames("__clawdline_inventory_v1__").count == 2 })
    check("then answers the request, with remote writes off, naming the ids it sent",
          eventually {
              let answer = fixture.replies()["read:snap-1"]
              return answer?["status"] as? Int == 200
                && (answer?["body"] as? [String: Any])?["sessions"] as? [String] == ["alpha", "beta"]
                && (answer?["body"] as? [String: Any])?["complete"] as? Bool == true
          }, "\(fixture.replies())")
    expect("the orchestrator snapshot, lost with the same replay, goes out again first",
           orchestratorFrames().count, 2)
    check("the re-sent row is the whole current row",
          fixture.lastRow("alpha")?["label"] as? String == "Row alpha"
            && closeabilityValue(fixture.lastRow("alpha"), "observed_at") == 120)
    expect("the first pass did not wait", waits.requests, [])

    // (b) More viewers inside the window: one pass, after the window, answers them all.
    fixture.clock.advance(1_000)
    fixture.askForRows("snap-2", sequence: 2)
    fixture.askForRows("snap-3", sequence: 3)
    check("two requests inside the window wait out the rest of it together",
          eventually { waits.requests == [4_000] }, "waits=\(waits.requests)")
    check("and nothing is re-sent while they wait",
          fixture.frames("alpha").count == 2 && fixture.replies()["read:snap-2"] == nil)
    waits.release()
    check("one pass answers both",
          eventually { fixture.replies()["read:snap-2"] != nil && fixture.replies()["read:snap-3"] != nil })
    expect("with one more copy of each row, however many asked", fixture.frames("alpha").count, 3)
    expect("and of its neighbour", fixture.frames("beta").count, 3)

    fixture.askForRows("bad", sequence: 4, extra: #","limit":1"#)
    check("a request carrying anything else is refused as malformed_read on its own name",
          eventually {
              let answer = fixture.replies()["read:bad"]
              return answer?["status"] as? Int == 400
                && (answer?["error"] as? [String: Any])?["code"] as? String == "malformed_read"
          }, "\(fixture.replies()["read:bad"] ?? [:])")

    // (c) Bounded: past the waiter limit a request is refused as busy rather than queued.
    fixture.clock.advance(1_000)
    for index in 0..<CloudAppBridge.sessionSnapshotWaiterLimit {
        fixture.askForRows("many-\(index)", sequence: UInt64(10 + index))
    }
    fixture.askForRows("one-too-many", sequence: 200)
    check("the request past the limit is refused as cloud_read_busy",
          eventually {
              let answer = fixture.replies()["read:one-too-many"]
              return answer?["status"] as? Int == 429
                && (answer?["error"] as? [String: Any])?["code"] as? String == "cloud_read_busy"
          }, "\(fixture.replies()["read:one-too-many"] ?? [:])")
    check("while the ones inside it wait for the next window", eventually { waits.requests.count == 2 })
    waits.release()
    check("and are all answered by one pass",
          eventually {
              let replies = fixture.replies()
              return (0..<CloudAppBridge.sessionSnapshotWaiterLimit).allSatisfy { replies["read:many-\($0)"] != nil }
          })
    expect("that pass sent each row once more", fixture.frames("alpha").count, 4)

    // M2: scans that stay incomplete still keep the Mac inside the console's machine window.
    fixture.clock.advance(CloudAppBridge.sessionPresenceIntervalMilliseconds)
    let beforeIncomplete = fixture.frames("__clawdline_inventory_v1__").count
    let incomplete = try! JSONSerialization.data(withJSONObject: [
        "sessions": [cloudRow("alpha", observedAt: 400, generation: 9, sourceObservedAt: 399)],
        "at": 400, "scan": ["generation": 9, "complete": false, "emptyAuthoritative": false] as [String: Any],
    ])
    cloudRowAwait("an incomplete scan after three quiet minutes") { try await bridge.publishSessions(incomplete) }
    expect("sends the presence marker even though the scan was incomplete",
           fixture.frames("__clawdline_inventory_v1__").count, beforeIncomplete + 1)
    check("naming every row still published, so no viewer drops one",
          ((fixture.frames("__clawdline_inventory_v1__").last?["inventory"] as? [String: Any])?["sessions"]
            as? [String]) == ["alpha", "beta"])
    cloudRowAwait("the snapshot bridge stops") { await bridge.stop() }
    check("a stopped bridge holds no pass",
          cloudRowAwait("reading the pass", recording: false) { await bridge.sessionSnapshotStateForTesting() }
            .map { $0.waiting == 0 && !$0.scheduled } == true)

    check("a status no bridge has named features for lists none", CloudStatus().noticeDigest()["features"] == nil)
}
}
