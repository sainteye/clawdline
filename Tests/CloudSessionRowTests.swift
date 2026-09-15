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

/// The bridge's diagnostic lines, in order.
private final class CloudRowLines: @unchecked Sendable {
    private let lock = NSLock()
    private var lines: [String] = []

    func append(_ line: String) {
        lock.lock(); lines.append(line); lock.unlock()
    }

    var all: [String] {
        lock.lock(); defer { lock.unlock() }
        return lines
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
         status: CloudStatus? = nil, suspendPublication: Bool = false,
         durableRuntime: CloudDurableRuntime? = nil, diagnostic: CloudRowLines? = nil) {
        let transport = CloudAppBridgeTestTransport(suspendPublication: suspendPublication)
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
            diagnostic: { diagnostic?.append($0) },
            durableRuntime: durableRuntime,
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

    /// Every opened payload published on `orch/mac-rows`, oldest first.
    func orchestratorFrames() -> [[String: Any]] {
        let signer = self.signer
        return transport.envelopes().filter { $0.ch == "orch/mac-rows" }.compactMap {
            (try? $0.open(masterSecret: secret, publicKeyForSender: {
                $0 == "mac-rows-device" ? signer.publicKeyRaw : nil
            })).flatMap { (try? JSONSerialization.jsonObject(with: $0)) as? [String: Any] }
        }
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

/// A task record with the fields the Mac's snapshot carries, most of which no Cloud view reads.
private func cloudTask(_ id: String, state: String, child: String?, finishedAt: Int? = nil,
                       title: String? = nil, total: Int = 18) -> [String: Any] {
    var record: [String: Any] = [
        "id": id, "state": state, "title": title ?? "Task \(id)", "created": 1_789_000_000,
        "kind": "custom", "assistant": "claude", "projectDir": "/work", "isolation": "worktree",
        "claims": ["Sources/A.swift"], "landing_paths": ["Sources/A.swift"],
        "executor": ["observed_at": 1_789_000_100, "inventory_generation": 7,
                     "status": "observed"] as [String: Any],
        "worktree": ["branch": "clawdline/task/\(id)", "path": "/tmp/\(id)"],
        "usage": ["input": 3, "output": 4, "cacheRead": 5, "cacheWrite": 6, "total": total,
                  "model": "claude-opus-5", "costUsd": 0.25] as [String: Any],
        "root": ["terminalId": "root-row", "sessionId": "root-session", "label": "Root",
                 "assistant": "claude"],
    ]
    if let child {
        record["child"] = ["terminalId": child, "sessionId": "session-\(id)", "backend": "tmux"]
    }
    if let finishedAt { record["finishedAt"] = finishedAt }
    return record
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

    // The refresh pass: the relay forgets every row with its object and a viewer that may not
    // publish on ctl/ cannot ask for them, so three minutes after the last pass that sent every row
    // and the inventory — the first scan here, 60 s ago — a scan sends them all again. Rows that
    // changed meanwhile do not restart that clock.
    let beforeQuiet = fixture.transport.envelopes().count
    fixture.clock.advance(CloudAppBridge.sessionPresenceIntervalMilliseconds - 60_000 - 1)
    fixture.publish([working,
                     cloudRow("beta", observedAt: 170, generation: 5, sourceObservedAt: 169,
                              freshness: "stale")],
                    at: 170, generation: 5, label: "an idle scan inside the refresh interval")
    expect("an idle scan inside the refresh interval publishes nothing",
           fixture.transport.envelopes().count, beforeQuiet)
    fixture.clock.advance(1)
    fixture.publish([working,
                     cloudRow("beta", observedAt: 180, generation: 6, sourceObservedAt: 179,
                              freshness: "stale")],
                    at: 180, generation: 6, label: "an idle scan at the refresh interval")
    expect("three minutes after the last whole pass, an idle scan re-sends every row and the inventory",
           fixture.transport.envelopes().count, beforeQuiet + 3)
    check("each row once, whole and current, and the inventory last",
          fixture.frames("alpha").count == 3 && fixture.frames("beta").count == 3
            && fixture.lastRow("alpha")?["line"] as? String == "Working (1s • esc to interrupt)"
            && closeabilityValue(fixture.lastRow("beta"), "observed_at") == 180
            && fixture.transport.envelopes().last?.ch == "s/mac-rows/__clawdline_inventory_v1__",
          "alpha=\(fixture.frames("alpha").count) beta=\(fixture.frames("beta").count)")
    fixture.clock.advance(20_000)
    fixture.publish([working,
                     cloudRow("beta", observedAt: 200, generation: 7, sourceObservedAt: 199,
                              freshness: "stale")],
                    at: 200, generation: 7, label: "the idle scan after a refresh")
    expect("and the next idle scan is quiet again: the interval counts from that pass",
           fixture.transport.envelopes().count, beforeQuiet + 3)
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

    // (a) A viewer connecting after the relay lost its replay asks; remote writes are off here. The
    // orchestrator snapshot went out longer ago than the re-send floor, so it goes again at once.
    fixture.clock.advance(CloudAppBridge.orchestratorResendFloorMilliseconds)
    fixture.askForRows("snap-1", sequence: 1, extra: #","orchestrator":true"#)
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
    expect("the orchestrator snapshot the request said it lacks goes out again too",
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
    expect("and no orchestrator snapshot, which neither said it lacks", orchestratorFrames().count, 2)
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

    // F4: an orchestrator request inside the floor after the Mac's last orch/ publication is owed,
    // not answered with another few hundred kilobytes to every viewer at once.
    fixture.clock.advance(CloudAppBridge.sessionSnapshotIntervalMilliseconds)
    let orchBeforeOwed = orchestratorFrames().count
    let rowsBeforeOwed = fixture.frames("alpha").count
    fixture.askForRows("owed-1", sequence: 300, extra: #","orchestrator":true"#)
    check("a request for the orchestrator snapshot inside the floor gets its rows and answer at once",
          eventually { fixture.replies()["read:owed-1"] != nil && fixture.frames("alpha").count == rowsBeforeOwed + 1 })
    let owedWait = CloudAppBridge.orchestratorResendFloorMilliseconds
        - CloudAppBridge.sessionSnapshotIntervalMilliseconds - 2_000
    check("while the orchestrator snapshot waits out the rest of the floor",
          eventually { waits.requests.last == owedWait }, "waits=\(waits.requests)")
    expect("and is not re-sent meanwhile", orchestratorFrames().count, orchBeforeOwed)
    check("what is owed is visible",
          cloudRowAwait("reading the owed state", recording: false) { await bridge.orchestratorResendStateForTesting() }
            .map { $0.owedSince != nil && $0.scheduled } == true)
    waits.release()
    check("at the floor it goes out once", eventually { orchestratorFrames().count == orchBeforeOwed + 1 })
    check("and nothing is owed after it",
          eventually {
              cloudRowAwait("reading the owed state", recording: false) { await bridge.orchestratorResendStateForTesting() }
                .map { $0.owedSince == nil && !$0.scheduled } == true
          })

    fixture.clock.advance(CloudAppBridge.sessionSnapshotIntervalMilliseconds)
    fixture.askForRows("owed-2", sequence: 301, extra: #","orchestrator":true"#)
    check("a second request inside the new floor is owed too",
          eventually { fixture.replies()["read:owed-2"] != nil && waits.requests.count == 4 }, "waits=\(waits.requests)")
    fixture.clock.advance(1_000)
    // The Mac handing over the snapshot it already sent publishes nothing, and so answers nobody:
    // the page that asked still gets it at the floor.
    let beforeIdentical = orchestratorFrames().count
    cloudRowAwait("the Mac hands over the same orchestrator snapshot again") { try await bridge.publishOrchestrator(orchestrator) }
    expect("an unchanged snapshot does not go out", orchestratorFrames().count, beforeIdentical)
    waits.release()
    check("and answers nobody: at the floor the owed snapshot still goes out",
          eventually { orchestratorFrames().count == orchBeforeOwed + 2 }
            && eventually {
                cloudRowAwait("reading the owed state", recording: false) { await bridge.orchestratorResendStateForTesting() }
                  .map { $0.owedSince == nil && !$0.scheduled } == true
            }, "orch=\(orchestratorFrames().count)")

    fixture.clock.advance(CloudAppBridge.sessionSnapshotIntervalMilliseconds)
    fixture.askForRows("owed-3", sequence: 302, extra: #","orchestrator":true"#)
    check("a third request inside the new floor is owed too",
          eventually { fixture.replies()["read:owed-3"] != nil && waits.requests.count == 5 }, "waits=\(waits.requests)")
    fixture.clock.advance(1_000)
    let changedOrchestrator = try! JSONSerialization.data(withJSONObject: [
        "tasks": [] as [Any], "machine": ["name": "Mac", "platform": "macos"] as [String: Any],
        "schedules": [] as [Any],
    ])
    cloudRowAwait("the Mac's own next orchestrator publication") { try await bridge.publishOrchestrator(changedOrchestrator) }
    waits.release()
    check("a changed publication after the request answers it, and the floor adds nothing",
          eventually {
              cloudRowAwait("reading the owed state", recording: false) { await bridge.orchestratorResendStateForTesting() }
                .map { $0.owedSince == nil && !$0.scheduled } == true
          } && !eventually(timeout: 0.3) { orchestratorFrames().count != orchBeforeOwed + 3 },
          "orch=\(orchestratorFrames().count)")

    // F6: a pass waiting out its window while a scan tombstones a row sends what is published when it
    // runs — never the row that closed — and its answer does not name it.
    fixture.clock.advance(1_000)
    fixture.askForRows("across-tombstone", sequence: 303)
    check("a request inside the window waits for it", eventually { waits.requests.count == 6 }, "waits=\(waits.requests)")
    let betaBeforeTombstone = fixture.frames("beta").count
    let alphaBeforeTombstone = fixture.frames("alpha").count
    fixture.publish([cloudRow("alpha", observedAt: 390, generation: 8, sourceObservedAt: 389)],
                    at: 390, generation: 8, label: "a scan that closes beta while the pass waits")
    check("the scan tombstones beta",
          fixture.frames("beta").count == betaBeforeTombstone + 1
            && fixture.frames("beta").last?["deleted"] as? Bool == true)
    waits.release()
    check("the pass that ran after it answers with alpha alone",
          eventually {
              (fixture.replies()["read:across-tombstone"]?["body"] as? [String: Any])?["sessions"] as? [String] == ["alpha"]
          }, "\(fixture.replies()["read:across-tombstone"] ?? [:])")
    check("re-sent alpha and never beta, whose last word is still its tombstone",
          fixture.frames("alpha").count == alphaBeforeTombstone + 1
            && fixture.frames("beta").count == betaBeforeTombstone + 1
            && fixture.frames("beta").last?["deleted"] as? Bool == true)
    check("and its inventory names alpha alone",
          ((fixture.frames("__clawdline_inventory_v1__").last?["inventory"] as? [String: Any])?["sessions"]
            as? [String]) == ["alpha"])
    fixture.publish([cloudRow("alpha", observedAt: 395, generation: 9, sourceObservedAt: 394),
                     cloudRow("beta", observedAt: 395, generation: 9, sourceObservedAt: 394)],
                    at: 395, generation: 9, label: "a scan where beta is back")

    // M2 and the refresh pass: scans that stay incomplete still keep the Mac inside the console's
    // machine window, and still send every published row again.
    fixture.clock.advance(CloudAppBridge.sessionPresenceIntervalMilliseconds)
    let beforeIncomplete = fixture.frames("__clawdline_inventory_v1__").count
    let alphaBeforeIncomplete = fixture.frames("alpha").count
    let betaBeforeIncomplete = fixture.frames("beta").count
    let incomplete = try! JSONSerialization.data(withJSONObject: [
        "sessions": [cloudRow("alpha", observedAt: 400, generation: 9, sourceObservedAt: 399)],
        "at": 400, "scan": ["generation": 9, "complete": false, "emptyAuthoritative": false] as [String: Any],
    ])
    cloudRowAwait("an incomplete scan after three quiet minutes") { try await bridge.publishSessions(incomplete) }
    expect("sends the presence marker even though the scan was incomplete",
           fixture.frames("__clawdline_inventory_v1__").count, beforeIncomplete + 1)
    check("and every published row, the one the scan named and the one it did not",
          fixture.frames("alpha").count == alphaBeforeIncomplete + 1
            && fixture.frames("beta").count == betaBeforeIncomplete + 1
            && closeabilityValue(fixture.lastRow("alpha"), "observed_at") == 400
            && closeabilityValue(fixture.lastRow("beta"), "observed_at") == 395,
          "alpha=\(fixture.frames("alpha").count) beta=\(fixture.frames("beta").count)")
    check("naming every row still published, so no viewer drops one",
          ((fixture.frames("__clawdline_inventory_v1__").last?["inventory"] as? [String: Any])?["sessions"]
            as? [String]) == ["alpha", "beta"])
    cloudRowAwait("the snapshot bridge stops") { await bridge.stop() }
    check("a stopped bridge holds no pass",
          cloudRowAwait("reading the pass", recording: false) { await bridge.sessionSnapshotStateForTesting() }
            .map { $0.waiting == 0 && !$0.scheduled } == true)

    check("a status no bridge has named features for lists none", CloudStatus().noticeDigest()["features"] == nil)

    // F6: `stop()` while a pass is publishing joins it, and nothing of the pass goes out after.
    let held = CloudRowFixture(suspendPublication: true)
    let heldBridge = held.bridge
    cloudRowAwait("the held bridge starts") { try await heldBridge.start() }
    held.transport.releasePublications(3)
    held.publish([cloudRow("alpha", observedAt: 100, generation: 1, sourceObservedAt: 99),
                  cloudRow("beta", observedAt: 100, generation: 1, sourceObservedAt: 99)],
                 at: 100, generation: 1, label: "the held Mac's first scan")
    held.askForRows("stopped", sequence: 1)
    check("the pass starts publishing its first row and is held there",
          eventually { held.transport.state().publicationStarts == 4 }, "starts=\(held.transport.state().publicationStarts)")
    cloudRowAwait("stopping the bridge in the middle of a pass") { await heldBridge.stop() }
    let afterStop = held.transport.envelopes().count
    check("stop returned with nothing of the pass after its held row: no neighbour, inventory or answer",
          held.frames("beta").count == 1 && held.frames("__clawdline_inventory_v1__").count == 1
            && held.replies()["read:stopped"] == nil && afterStop <= 4, "envelopes=\(afterStop)")
    check("and nothing arrives later either",
          !eventually(timeout: 0.3) { held.transport.envelopes().count != afterStop })
    check("a bridge stopped in the middle of a pass holds nothing",
          cloudRowAwait("reading the stopped pass", recording: false) { await heldBridge.sessionSnapshotStateForTesting() }
            .map { $0.waiting == 0 && !$0.scheduled } == true)

    // The `orch/` snapshot a reconnecting viewer converges to: it carries what a Cloud view reads,
    // goes out when that changes, paces a burst, and a notice is that snapshot too. Its own bridge
    // and clock, in this group because it is the same convergence question as the re-sends above.
    do {
        let waits = CloudRowWaits()
        let status = CloudStatus()
        let fixture = CloudRowFixture(waits: waits, status: status)
        let bridge = fixture.bridge
        cloudRowAwait("the orchestrator bridge starts") { try await bridge.start() }
        fixture.publish([cloudRow("kid-listed", observedAt: 100, generation: 1, sourceObservedAt: 99),
                         cloudRow("root-row", observedAt: 100, generation: 1, sourceObservedAt: 99)],
                        at: 100, generation: 1, label: "the Mac's Sessions")
        func snapshot(_ tasks: [[String: Any]], at: Int) -> Data {
            try! JSONSerialization.data(withJSONObject: [
                "tasks": tasks, "at": at,
                "app": ["version": "1", "build": 7, "protocol": 3] as [String: Any],
                "machine": ["name": "Mac", "platform": "macos"],
                "schedules": [["id": "morning", "title": "Morning", "enabled": true] as [String: Any]],
                "snippets": [["id": "snip", "title": "Ship", "body": "commit", "scope": "global"]],
            ] as [String: Any])
        }
        func publish(_ label: String, _ payload: Data, force: Bool = false) {
            cloudRowAwait(label) { try await bridge.publishOrchestrator(payload, force: force) }
        }
        func state() -> (coalescing: Bool, presence: Bool, lastSnapshotAt: UInt64?)? {
            cloudRowAwait("reading the pacing", recording: false) {
                await bridge.orchestratorPublicationStateForTesting()
            }
        }
        func ids(_ frame: [String: Any]?) -> [String] {
            (frame?["tasks"] as? [[String: Any]] ?? []).compactMap { $0["id"] as? String }
        }
        let running = cloudTask("running", state: "briefed", child: "kid-running")
        let listed = cloudTask("listed", state: "success", child: "kid-listed", finishedAt: 1_789_000_500)
        let rootOnly = cloudTask("root-only", state: "success", child: "kid-gone", finishedAt: 1_789_000_400)
        let unknown = cloudTask("unknown", state: "held_by_a_newer_build", child: "kid-gone")
        let late = cloudTask("late", state: "spawn_failed", child: "kid-late", finishedAt: 1_789_000_600)

        // What goes out: the records a Cloud view can reach, and of those only what it reads.
        publish("the Mac's orchestrator snapshot", snapshot([running, listed, rootOnly, unknown, late], at: 1))
        let first = fixture.orchestratorFrames().last
        expect("one snapshot goes out", fixture.orchestratorFrames().count, 1)
        expect("with the running task, the finished one whose child is a listed Session and the one this build cannot read — not one only its root or a closed child keeps",
               ids(first), ["running", "listed", "unknown"])
        let kept = (first?["tasks"] as? [[String: Any]])?.first { $0["id"] as? String == "listed" } ?? [:]
        check("of a record only the fields a Cloud view reads go out",
              Set(kept.keys) == ["id", "state", "title", "created", "finishedAt", "child", "root", "usage"]
                && (kept["child"] as? [String: Any])?.keys.sorted() == ["terminalId"]
                && (kept["root"] as? [String: Any])?.keys.sorted() == ["terminalId"]
                && (kept["usage"] as? [String: Any])?.keys.sorted() == ["costUsd", "total"]
                && (kept["child"] as? [String: Any])?["terminalId"] as? String == "kid-listed"
                && (kept["usage"] as? [String: Any])?["total"] as? Int == 18, "\(kept)")
        check("and the rest of the snapshot goes out as the Mac built it, with the digest",
              (first?["schedules"] as? [[String: Any]])?.first?["title"] as? String == "Morning"
                && (first?["snippets"] as? [[String: Any]])?.first?["body"] as? String == "commit"
                && (first?["machine"] as? [String: Any])?["platform"] as? String == "macos"
                && (first?["app"] as? [String: Any])?["build"] as? Int == 7 && first?["at"] as? Int == 1
                && (first?["cloud_status"] as? [String: Any])?["v"] as? Int == 1, "\(first ?? [:])")

        // Nothing a viewer reads changed: `at`, and the executor's observation clock, which moves with
        // every SessionWatch reading of a running child.
        fixture.clock.advance(10_000)
        var observedAgain = running
        observedAgain["executor"] = ["observed_at": 1_789_000_999, "inventory_generation": 8]
        publish("a snapshot differing only in `at` and a field no Cloud view reads",
                snapshot([observedAgain, listed, rootOnly, unknown, late], at: 2))
        expect("publishes nothing", fixture.orchestratorFrames().count, 1)

        // A burst: the first change goes out at once, the ones behind it inside the interval become
        // one publication of the newest state when it has passed.
        let renamed = cloudTask("running", state: "briefed", child: "kid-running", title: "Renamed")
        publish("a change after a quiet interval", snapshot([renamed, listed, rootOnly, unknown, late], at: 3))
        expect("goes out at once", fixture.orchestratorFrames().count, 2)
        fixture.clock.advance(1_000)
        let counted = cloudTask("running", state: "briefed", child: "kid-running", title: "Renamed", total: 40)
        publish("a change one second later", snapshot([counted, listed, rootOnly, unknown, late], at: 4))
        check("waits out the rest of the interval",
              eventually { waits.requests.last == CloudAppBridge.orchestratorPublicationIntervalMilliseconds - 1_000 },
              "waits=\(waits.requests)")
        fixture.clock.advance(500)
        let recounted = cloudTask("running", state: "briefed", child: "kid-running", title: "Renamed", total: 41)
        publish("and another behind it", snapshot([recounted, listed, rootOnly, unknown, late], at: 5))
        check("which joins the same wait rather than adding one",
              waits.requests.count == 1 && fixture.orchestratorFrames().count == 2
                && state().map { $0.coalescing } == true, "waits=\(waits.requests)")
        fixture.clock.advance(3_500)
        waits.release()
        check("once it has passed, one publication carries the newest state",
              eventually { fixture.orchestratorFrames().count == 3 }
                && ((fixture.orchestratorFrames().last?["tasks"] as? [[String: Any]])?.first?["usage"]
                    as? [String: Any])?["total"] as? Int == 41,
              "frames=\(fixture.orchestratorFrames().count)")
        check("and nothing is left waiting", eventually { state().map { !$0.coalescing } == true })

        // A notice: the whole snapshot again with the digest that changed, because a page that predates
        // the status-only tolerance takes whatever arrives on orch/ for the whole snapshot.
        fixture.clock.advance(CloudAppBridge.orchestratorPublicationIntervalMilliseconds)
        status.recordDrop(CloudInboundDrop(code: .replay, sender: "web_orch", sequence: 9, highestSequence: 10))
        check("a dropped command is announced on orch/",
              eventually { fixture.orchestratorFrames().count == 4 }, "frames=\(fixture.orchestratorFrames().count)")
        let notice = fixture.orchestratorFrames().last ?? [:]
        check("as the current snapshot — tasks, schedules, snippets, descriptor — with the drop in its cloud_status",
              ids(notice) == ["running", "listed", "unknown"]
                && ((notice["tasks"] as? [[String: Any]])?.first?["usage"] as? [String: Any])?["total"] as? Int == 41
                && (notice["schedules"] as? [[String: Any]])?.first?["id"] as? String == "morning"
                && (notice["snippets"] as? [[String: Any]])?.first?["id"] as? String == "snip"
                && (notice["machine"] as? [String: Any])?["platform"] as? String == "macos"
                && (notice["app"] as? [String: Any])?["build"] as? Int == 7
                && ((notice["cloud_status"] as? [String: Any])?["recent_drops"] as? [[String: Any]])?
                    .first?["seq"] as? Int == 9, "\(notice)")
        check("and nothing is scheduled behind it", !eventually(timeout: 0.3) { fixture.orchestratorFrames().count != 4 }
                && waits.requests.count == 1 && state().map { !$0.coalescing && !$0.presence } == true,
              "waits=\(waits.requests)")

        // A Session published after its task finished makes that task readable, and it goes out.
        fixture.clock.advance(CloudAppBridge.orchestratorPublicationIntervalMilliseconds)
        fixture.publish([cloudRow("kid-listed", observedAt: 200, generation: 2, sourceObservedAt: 199),
                         cloudRow("root-row", observedAt: 200, generation: 2, sourceObservedAt: 199),
                         cloudRow("kid-late", observedAt: 200, generation: 2, sourceObservedAt: 199)],
                        at: 200, generation: 2, label: "a scan that lists the child of a task that already failed")
        check("the snapshot goes out with that task",
              eventually { fixture.orchestratorFrames().count == 5 }
                && ids(fixture.orchestratorFrames().last) == ["running", "listed", "unknown", "late"],
              "frames=\(fixture.orchestratorFrames().count) ids=\(ids(fixture.orchestratorFrames().last))")
        fixture.clock.advance(CloudAppBridge.orchestratorPublicationIntervalMilliseconds)
        fixture.publish([cloudRow("root-row", observedAt: 300, generation: 3, sourceObservedAt: 299),
                         cloudRow("kid-late", observedAt: 300, generation: 3, sourceObservedAt: 299)],
                        at: 300, generation: 3, label: "a scan where a finished child's Session closed")
        check("a Session closing only makes a record unreadable, so nothing goes out for it",
              !eventually(timeout: 0.3) { fixture.orchestratorFrames().count != 5 }
                && state().map { !$0.coalescing } == true)

        // Sixty visible changes a second apart, the clock turning with them: one at once, then one
        // per interval, and the newest state once the burst stops.
        fixture.clock.advance(CloudAppBridge.orchestratorPublicationIntervalMilliseconds)
        let beforeBurst = fixture.orchestratorFrames().count
        var due: UInt64?
        func releaseIfDue() {
            guard let deadline = due, fixture.clock.now() >= deadline else { return }
            let frames = fixture.orchestratorFrames().count
            waits.release()
            due = nil
            _ = eventually { fixture.orchestratorFrames().count > frames }
        }
        for step in 1...60 {
            if step > 1 { fixture.clock.advance(1_000) }
            releaseIfDue()
            let asked = waits.requests.count
            let changed = cloudTask("running", state: "briefed", child: "kid-running", title: "Step \(step)")
            publish("burst change \(step)", snapshot([changed, rootOnly, unknown, late], at: 100 + step))
            if waits.requests.count > asked, let wait = waits.requests.last {
                due = fixture.clock.now() + wait
            }
        }
        fixture.clock.advance(CloudAppBridge.orchestratorPublicationIntervalMilliseconds)
        releaseIfDue()
        let burst = Array(fixture.orchestratorFrames().dropFirst(beforeBurst))
        check("sixty changes in sixty seconds go out as thirteen snapshots, not sixty",
              burst.count == 13, "snapshots=\(burst.count)")
        check("and the last carries the sixtieth change",
              (burst.last?["tasks"] as? [[String: Any]])?.first?["title"] as? String == "Step 60",
              "\(burst.last ?? [:])")

        // A device that cannot ask has only the relay's replay, which an eviction empties of rows and
        // snapshot alike: the refresh pass that brings it the rows brings the snapshot too, however
        // recently one went out, and a page asking for the rows alone does not put that pass off.
        fixture.clock.advance(30_000)
        let rootRowsBeforeAsk = fixture.frames("root-row").count
        fixture.askForRows("rows-only", sequence: 1)
        check("a page holding the snapshot asks for the rows alone and gets them",
              eventually { fixture.replies()["read:rows-only"] != nil
                && fixture.frames("root-row").count == rootRowsBeforeAsk + 1 }, "\(fixture.replies())")
        let beforePresence = fixture.orchestratorFrames().count
        let rootRowsBeforePresence = fixture.frames("root-row").count
        fixture.clock.advance(CloudAppBridge.sessionPresenceIntervalMilliseconds - 60_000)
        fixture.publish([cloudRow("root-row", observedAt: 400, generation: 4, sourceObservedAt: 399),
                         cloudRow("kid-late", observedAt: 400, generation: 4, sourceObservedAt: 399)],
                        at: 400, generation: 4, label: "the scan three minutes after the last refresh pass")
        check("is the refresh pass: every row, and the snapshot that went out two and a half minutes ago",
              eventually { fixture.orchestratorFrames().count == beforePresence + 1 }
                && fixture.frames("root-row").count == rootRowsBeforePresence + 1
                && (fixture.orchestratorFrames().last?["tasks"] as? [[String: Any]])?.first?["title"]
                    as? String == "Step 60",
              "frames=\(fixture.orchestratorFrames().count - beforePresence) rows=\(fixture.frames("root-row").count - rootRowsBeforePresence)")
        fixture.clock.advance(20_000)
        fixture.publish([cloudRow("root-row", observedAt: 420, generation: 5, sourceObservedAt: 419),
                         cloudRow("kid-late", observedAt: 420, generation: 5, sourceObservedAt: 419)],
                        at: 420, generation: 5, label: "the next scan")
        check("and the next scan inside the interval does not",
              !eventually(timeout: 0.3) { fixture.orchestratorFrames().count != beforePresence + 1 })

        // A transport-ready generation: the relay may have lost its replay, so it goes out whatever.
        let beforeReady = fixture.orchestratorFrames().count
        publish("the same snapshot for a new ready generation",
                snapshot([cloudTask("running", state: "briefed", child: "kid-running", title: "Step 60"),
                          rootOnly, unknown, late], at: 200), force: true)
        expect("goes out at once", fixture.orchestratorFrames().count, beforeReady + 1)
        cloudRowAwait("the orchestrator bridge stops") { await bridge.stop() }
        check("a stopped bridge holds no pacing", state().map { !$0.coalescing && !$0.presence } == true)
    }

    // The same snapshot through the durable spool a Mac really publishes with. It keeps only the
    // newest unsent orch/ row (`CloudSpoolChannel.isLatestValue`), so whatever it keeps must be the
    // whole current snapshot; and a transport-ready generation forgets what was recorded as sent.
    do {
        let status = CloudStatus()
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(
            "clawdline-orch-spool-\(UUID().uuidString)", isDirectory: true)
        try? FileManager.default.createDirectory(
            at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let runtime = try? CloudDurableRuntime.open(
            directory: directory, runtime: .mac, metrics: CloudStatusSpoolMetrics(status: status))
        check("the durable spool opens", runtime != nil)
        if let runtime {
            let waits = CloudRowWaits()
            let lines = CloudRowLines()
            let readies = CloudRowLines()
            let fixture = CloudRowFixture(waits: waits, status: status, suspendPublication: true,
                                          durableRuntime: runtime, diagnostic: lines)
            let bridge = fixture.bridge
            cloudRowAwait("the spool bridge observes its ready generations") {
                await bridge.setTransportReadyObserver { readies.append("\($0)") }
            }
            cloudRowAwait("the spool bridge starts") { try await bridge.start() }
            check("its first generation is ready", eventually { readies.all == ["1"] }, "\(readies.all)")
            func snapshot(_ title: String) -> Data {
                try! JSONSerialization.data(withJSONObject: [
                    "tasks": [cloudTask("moving", state: "briefed", child: "kid-moving", title: title)], "at": 1,
                    "machine": ["name": "Mac", "platform": "macos"],
                    "schedules": [["id": "morning", "title": "Morning", "enabled": true] as [String: Any]],
                ] as [String: Any])
            }
            func sequences() -> [UInt64] {
                fixture.transport.envelopes().filter { $0.ch == "orch/mac-rows" }.map { $0.seq }
            }

            cloudRowAwait("the Mac's snapshot") { try await bridge.publishOrchestrator(snapshot("One")) }
            check("goes to the socket, which holds it", eventually { fixture.transport.state().publicationStarts == 1 })
            fixture.clock.advance(CloudAppBridge.orchestratorPublicationIntervalMilliseconds)
            cloudRowAwait("a change while the socket holds the first") { try await bridge.publishOrchestrator(snapshot("Two")) }
            fixture.clock.advance(CloudAppBridge.orchestratorPublicationIntervalMilliseconds)
            status.recordDrop(CloudInboundDrop(code: .replay, sender: "web_spool", sequence: 5, highestSequence: 6))
            check("a notice follows it into the spool while the change is still unsent",
                  eventually { lines.all.contains { $0.hasPrefix("cloud: orchestrator") && $0.contains("notice") } },
                  "\(lines.all)")
            fixture.transport.releasePublications(16)
            check("once the socket moves, the spool's newest orch/ row goes out after the held one",
                  eventually { sequences().count == 2 } && !eventually(timeout: 0.3) { sequences().count != 2 },
                  "sequences=\(sequences())")
            let kept = fixture.orchestratorFrames().last
            check("and it is the whole current snapshot: the unsent change, the schedules and the drop",
                  (kept?["tasks"] as? [[String: Any]])?.first?["title"] as? String == "Two"
                    && (kept?["schedules"] as? [[String: Any]])?.first?["id"] as? String == "morning"
                    && ((kept?["cloud_status"] as? [String: Any])?["recent_drops"] as? [[String: Any]])?
                        .first?["seq"] as? Int == 5, "\(kept ?? [:])")

            for envelope in fixture.transport.envelopes() where envelope.ch == "orch/mac-rows" {
                fixture.transport.acknowledge(envelope)
            }
            let sentBeforeReady = sequences().max() ?? 0
            fixture.transport.signalReady()
            check("a new ready generation arrives", eventually { readies.all == ["1", "2"] }, "\(readies.all)")
            fixture.clock.advance(CloudAppBridge.orchestratorPublicationIntervalMilliseconds)
            // Unforced: a newer snapshot can replace the forced ready one in the Mac's own lane.
            cloudRowAwait("the Mac hands over the snapshot it last sent") { try await bridge.publishOrchestrator(snapshot("Two")) }
            check("after a ready generation the same snapshot is a new publication, not one already out",
                  eventually { sequences().contains { $0 > sentBeforeReady } }
                    && (fixture.orchestratorFrames().last?["tasks"] as? [[String: Any]])?.first?["title"] as? String == "Two",
                  "sequences=\(sequences())")
            cloudRowAwait("the spool bridge stops") { await bridge.stop() }
        }
        try? FileManager.default.removeItem(at: directory)
    }
}
}
