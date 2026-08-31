import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - Where the main-queue crossings are, as source

/// Every synchronous main-queue crossing in the app: which file holds the call site, the exact
/// site name it hands ``MainQueue/hop(from:alreadyOnMain:_:)``, and how many call sites in that
/// file spell it. `RemoteIcon` is three render callers sharing one helper and therefore one name.
let productionCrossingCallSites: [(file: String, site: String, callSites: Int)] = [
    ("Sources/StartPoints.swift", "StartPoints.openTranscripts", 1),
    ("Sources/StartPoints.swift", "StartPoints.live", 1),
    ("Sources/StartPoints.swift", "StartPoints.runningApps", 1),
    ("Sources/RemoteIcon.swift", "RemoteIcon.onMain", 3),
    ("Sources/SessionImagePreview.swift", "SessionImagePresentation.materialize", 1),
    ("Sources/Orchestrator.swift", "Orchestrator.resolveAttachment", 1),
    ("Sources/Orchestrator.swift", "Orchestrator.dispatch.attachFailure", 1),
    ("Sources/Orchestrator.swift", "Orchestrator.startQueuedTaskIfEligible.secretFailure", 1),
    ("Sources/Orchestrator.swift", "Orchestrator.startQueuedTaskIfEligible.spawnFailure", 1),
    ("Sources/Orchestrator.swift", "Orchestrator.cancel.child", 1),
    ("Sources/Orchestrator.swift", "Orchestrator.cancel.task", 1),
    ("Sources/Orchestrator.swift", "Orchestrator.records", 1),
    ("Sources/Orchestrator.swift", "Orchestrator.record(id:)", 1),
    ("Sources/Orchestrator.swift", "Orchestrator.target(withID:)", 1),
    ("Sources/Orchestrator.swift", "Orchestrator.rootTargets", 1),
    ("Sources/RemoteServer.swift", "RemoteServer.titleState(of:)", 1),
    ("Sources/RemoteServer.swift", "RemoteServer.sessionRefresh", 1),
    ("Sources/RemoteServer.swift", "RemoteServer.sessionWhoAmI", 1),
]

/// The production crossings the fixture at the end of this file drives for real and watches hop.
let dynamicallyExercisedCrossingSites: Set<String> = [
    "StartPoints.openTranscripts",
    "StartPoints.live",
    "StartPoints.runningApps",
    "RemoteIcon.onMain",
    "SessionImagePresentation.materialize",
    "Orchestrator.resolveAttachment",
    "RemoteServer.titleState(of:)",
]

/// The file with its whole-line comments dropped, so this guard is about the code and not about
/// the prose around it. Written after the first run of it went red on a doc comment that named the
/// very call it exists to forbid: a guard a contributor cannot describe in a comment is a guard
/// that teaches people to stop describing things.
func codeOnly(_ text: String) -> String {
    text.split(separator: "\n", omittingEmptySubsequences: false)
        .filter { !$0.trimmingCharacters(in: .whitespaces).hasPrefix("//") }
        .joined(separator: "\n")
}

func occurrences(of needle: String, in haystack: String) -> Int {
    guard !needle.isEmpty else { return 0 }
    var count = 0
    var index = haystack.startIndex
    while let hit = haystack.range(of: needle, range: index..<haystack.endIndex) {
        count += 1
        index = hit.upperBound
    }
    return count
}

/// Direct singleton call sites with formatting erased. This is deliberately a supported-syntax
/// guard rather than a Swift parser: a direct call may wrap across lines, but aliases and dynamic
/// dispatch are outside the production convention and therefore do not silently join the count.
func compactCode(_ text: String) -> String {
    String(codeOnly(text).filter { !$0.isWhitespace })
}

func sourceSlice(_ text: String, from start: String, through end: String) -> String {
    guard let lower = text.range(of: start)?.lowerBound,
          let upper = text.range(of: end, range: lower..<text.endIndex)?.upperBound
    else { return "" }
    return String(text[lower..<upper])
}





// MARK: - A reading with something in it

/// Take one reading with ``SessionWatch``, with the terminals replaced, and wait for it to be
/// published.
///
/// **`start()`, not a private entry point.** Starting this class is the thing the suite had never
/// done, and it is the single cause behind two unrelated-looking gaps: the usage ledger's forced
/// checkpoints hang off a reading and were asserted nowhere, and every main-queue crossing a live
/// target leads to was dormant because `targets` was permanently empty.
///
/// The scan carries no assistant processes, which is what keeps a reading inside a test process
/// off the two paths that would leave it — no screen captures and no registry walk. Everything
/// this fixture exists for still runs: reconciliation, the ledger, and the publish onto main.
@discardableResult
func sessionWatchReading(_ sessions: [TargetSession]) -> Bool {
    SessionWatch.inventoryForTesting = {
        (scan: ITerm.AssistantProcessScan(assistants: [:], error: nil),
         snapshot: Targets.Snapshot(sessions: sessions, currentID: nil, error: nil,
                                    isComplete: true))
    }
    SessionWatch.shared.start()
    // `start()` asks immediately, but an earlier suite group may still have a real terminal read
    // in flight. `read()` deliberately drops overlapping cadence work, so starting alone can
    // leave this fixture waiting on the twenty-second timer while its three-second assertion
    // reads the previous population. `refresh()` is the public single-debt path: it remembers one
    // follow-up behind the in-flight read and gives us the completed sequence to wait beyond.
    let receipt = SessionWatch.shared.refresh()
    return eventually(timeout: 10) {
        let snapshot = SessionWatch.shared.identitySnapshot()
        return SessionWatch.shared.completedScanSequence > receipt.completedScanSequence
            && snapshot.complete
            && snapshot.targets.map(\.id) == sessions.map(\.id)
    }
}

func runSessionWatchTests() {
group("every main-queue crossing is a named production call site, in the source on disk") {
    // **Structural, and the name says so.** This group reads the four files rather than running
    // them, because nine of the nineteen call sites below cannot be driven from a test process:
    // `Orchestrator.dispatch` opens a terminal tab, all five of its failure and cancellation tails
    // call `finalize`, which types into somebody's session, and the four registry readers answer
    // through paths the async fixture already covers by other means. The other ten *are* driven
    // for real at the end of this file and watched hopping by name.
    //
    // What went wrong without this: the crossing fixture used to return a hard-coded list of site
    // names after calling some production functions, so restoring
    // `Orchestrator.dispatch.attachFailure` to its old `Thread.isMainThread`/`main.sync` shape, or
    // deleting `StartPoints.live`'s crossing outright, both left the suite at 5421 of 5421 green.
    // A guard that cannot go red is worse than no guard, because somebody believes it.
    let crossingFiles = ["Sources/StartPoints.swift", "Sources/RemoteIcon.swift",
                         "Sources/SessionImagePreview.swift",
                         "Sources/Orchestrator.swift", "Sources/RemoteServer.swift"]
    var sources: [String: String] = [:]
    for file in crossingFiles {
        guard let text = try? String(contentsOfFile: file, encoding: .utf8) else {
            check("\(file) can be read for the crossing guard", false)
            continue
        }
        sources[file] = codeOnly(text)
    }

    for site in productionCrossingCallSites {
        let text = sources[site.file] ?? ""
        expect("\(site.site) crosses at its own named call site",
               occurrences(of: "onMain(from: \"\(site.site)\"", in: text), site.callSites)
    }

    for file in crossingFiles {
        let text = sources[file] ?? ""
        // A restored true shape reintroduces the one call this whole change exists to remove, so
        // this is the check that answers "did somebody put it back".
        expect("\(file) takes no synchronous main hop of its own",
               occurrences(of: "DispatchQueue.main.sync", in: text), 0)
        // Every hop in these files names its site with a literal. Passing a variable is how the
        // old fixture simulated five call sites it never reached: a loop over names calling the
        // shared helper reads exactly like the real thing to everything except this line.
        expect("\(file) names every hop site with a literal",
               occurrences(of: "onMain(from: \"", in: text),
               occurrences(of: "onMain(from:", in: text))
        // And the table above is the whole list for that file, so a new crossing has to be
        // declared here rather than added quietly.
        expect("\(file) has no crossing missing from the table",
               occurrences(of: "onMain(from: \"", in: text),
               productionCrossingCallSites.filter { $0.file == file }
                   .reduce(0) { $0 + $1.callSites })
    }

    let declared = Set(productionCrossingCallSites.map { $0.site })
    check("every site the fixture drives is one of the declared call sites",
          dynamicallyExercisedCrossingSites.isSubset(of: declared),
          "\(dynamicallyExercisedCrossingSites.subtracting(declared).sorted())")
}

group("every direct SessionWatch nudge call site is inventoried across production source") {
    let expected = [
        "Sources/RemoteServer.swift": 11,
        "Sources/Orchestrator.swift": 8,
        "Sources/CoordinatorSuccession.swift": 1,
        "Sources/main.swift": 1,
    ]
    let sourceFiles = (try? FileManager.default.contentsOfDirectory(atPath: "Sources"))?
        .filter { $0.hasSuffix(".swift") }.map { "Sources/\($0)" } ?? []
    var found: [String: Int] = [:]
    let call = "SessionWatch.shared.nudge()"
    let wrapped = "DispatchQueue.main.async{SessionWatch.shared.nudge()}"
    for file in sourceFiles {
        guard let source = try? String(contentsOfFile: file, encoding: .utf8) else {
            check("\(file) can be read for the SessionWatch nudge guard", false)
            continue
        }
        let compact = compactCode(source)
        let count = occurrences(of: call, in: compact)
        if count > 0 { found[file] = count }
    }
    expect("the complete direct-nudge file inventory is declared", found, expected)
    if let remote = try? String(contentsOfFile: "Sources/RemoteServer.swift", encoding: .utf8) {
        expect("every RemoteServer direct nudge explicitly re-enters main",
               occurrences(of: wrapped, in: compactCode(remote)),
               expected["Sources/RemoteServer.swift"] ?? 0)
    } else {
        check("RemoteServer source is readable for the direct nudge guard", false)
    }
    if let orchestrator = try? String(contentsOfFile: "Sources/Orchestrator.swift", encoding: .utf8) {
        expect("the four cross-queue Orchestrator nudges explicitly re-enter main",
               occurrences(of: wrapped, in: compactCode(orchestrator)), 4)
    } else {
        check("Orchestrator source is readable for the direct nudge guard", false)
    }
}

group("one published Session inventory owns process evidence and bounded subprocess cleanup") {
    let sessionWatch = (try? String(contentsOfFile: "Sources/SessionWatch.swift",
                                    encoding: .utf8)) ?? ""
    let orchestrator = (try? String(contentsOfFile: "Sources/Orchestrator.swift",
                                    encoding: .utf8)) ?? ""
    let remote = (try? String(contentsOfFile: "Sources/RemoteServer.swift",
                              encoding: .utf8)) ?? ""
    let iterm = (try? String(contentsOfFile: "Sources/ITerm.swift", encoding: .utf8)) ?? ""
    check("SessionWatch publishes every cross-queue Session field and provenance together",
          sessionWatch.contains("struct InventoryPublication")
            && sessionWatch.contains("let identities: [String: PublishedIdentity]")
            && sessionWatch.contains("let labels: [String: String]")
            && sessionWatch.contains("let menus: [String: SessionState.Menu]")
            && sessionWatch.contains("let agents: [String: [Subagents.Agent]]")
            && sessionWatch.contains("let shells: [String: [Shells.Shell]]")
            && sessionWatch.contains("let provenance: String"))

    let beat = sourceSlice(orchestrator,
                           from: "static func beat(fromTimer: Bool)",
                           through: "guard !liveIDs.isEmpty else { return }")
    check("the Orchestrator beat consumes identities already bound to its watch snapshot",
          beat.contains("watchSnapshot.identities")
            && !beat.contains("map(RemoteServer.sessionWorkIdentity)"), beat)

    let identity = sourceSlice(remote,
                               from: "static func sessionWorkIdentity(_ session: TargetSession)",
                               through: "struct SessionInventoryEvidence")
    check("session serialization cannot start a fresh process scan",
          !identity.contains("Targets.pid")
            && !identity.contains("Targets.processStart")
            && !identity.contains("Transcript.sessionID(of:")
            && identity.contains("publishedIdentity"), identity)

    let producer = sourceSlice(sessionWatch,
                               from: "private static func identities(",
                               through: "private func read()")
    check("publication assembly does not re-enter main or start per-row naming readers",
          !producer.contains("observedDisplayLabel")
            && !producer.contains("MainQueue.hop")
            && !producer.contains("Targets.processStart")
            && !producer.contains("Transcript.sessionID(of:"), producer)

    let coordinator = sourceSlice(remote,
                                  from: "func coordinatorObservation()",
                                  through: "static func attachCoordinator")
    check("coordinator inventory never parks the main queue on a semaphore",
          !coordinator.contains("DispatchQueue.main")
            && !coordinator.contains("DispatchSemaphore")
            && !coordinator.contains("onMain(")
            && coordinator.contains("publishedInventory"), coordinator)

    let shell = sourceSlice(iterm,
                            from: "private static func shell(_ path: String",
                            through: "private static var scriptPath")
    check("every bounded subprocess has a forced termination rung and explicit pipe closure",
          shell.contains("SIGKILL")
            && occurrences(of: "closeOwned(", in: shell) >= 11, shell)

    // Hold one exact Session and one exact publication still. This is the route the browser uses
    // to open the row; process counters and the shared hop recorder make "no scan" observable
    // rather than inferred from its response time.
    SessionWatch.shared.stop()
    let fixedID = "SESSION-PUBLISHED-IDENTITY"
    let conversationID = "11111111-3333-4555-8777-999999999999"
    let fixed = TargetSession(
        backend: .iterm, id: fixedID, name: "fixed", tty: "/dev/ttys933",
        windowIndex: 0, tabIndex: 0, assistant: .codex, cwd: "/tmp/fixed-project")
    let shellOnly = TargetSession(
        backend: .iterm, id: "SESSION-SHELL-ONLY", name: "shell", tty: "/dev/ttys934",
        windowIndex: 0, tabIndex: 1, assistant: nil, cwd: "/tmp/shell-project")
    let observedAt = Date(timeIntervalSince1970: 1_800_000_000)
    let published = SessionWatch.PublishedIdentity(
        assistant: .codex, tty: fixed.tty, pid: 93_300,
        processStart: observedAt.addingTimeInterval(-60), conversationID: conversationID,
        workingDirectory: fixed.cwd, recordURL: nil,
        observedAt: observedAt,
        provenance: "session_watch_codex_rollout_process_scan",
        conversationSource: .codexRollout, conversationObservedAt: observedAt)
    SessionWatch.shared.installPublicationForTesting(
        targets: [fixed, shellOnly], states: [fixedID: .idle, shellOnly.id: .idle],
        identities: [fixedID: published], labels: [fixedID: "fixed publication"],
        complete: true, observedAt: observedAt)
    expect("subprocess metrics reset accepts an idle boundary",
           ITerm.resetSubprocessMetricsForTesting(), true)
    MainQueue.beginRecordingHopsForTesting()
    let reader = RemoteAuth.addDevice(name: "fixed publication route", caps: [.read])
    let opened = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/sessions/\(fixedID)",
        headers: ["Authorization": "Bearer \(reader.token)"]))
    RemoteAuth.revoke(id: reader.id)
    let hops = MainQueue.endRecordingHopsForTesting()
    let routeMetrics = ITerm.subprocessMetrics()
    expect("opening the fixed published Session succeeds", opened.status, 200)
    let openedBody = (try? JSONSerialization.jsonObject(with: opened.body)) as? [String: Any]
    let openedSession = openedBody?["session"] as? [String: Any]
    expect("the route returns the conversation bound by that publication",
           openedSession?["sessionId"] as? String, conversationID)
    check("session open starts no ps, lsof or osascript subprocess",
          routeMetrics.started == 0 && routeMetrics.inFlight == 0
            && routeMetrics.openPipeHandles == 0, "\(routeMetrics)")
    check("session open performs no synchronous main-queue hop or semaphore crossing",
          hops.sites.isEmpty && !hops.overflowed, "\(hops)")
    let accepted = SessionWatch.shared.publishedInventory()
    check("identity freshness and provenance remain attached to the accepted generation",
          accepted.observedAt == observedAt
            && accepted.identities[fixedID]?.observedAt == observedAt
            && accepted.identities[fixedID]?.provenance
                == "session_watch_codex_rollout_process_scan"
            && accepted.identities[fixedID]?.conversationSource == .codexRollout)
    check("the publication carries a complete label map even without process identity",
          accepted.labels.count == 2
            && accepted.labels[fixedID] == "fixed publication"
            && accepted.labels[shellOnly.id] == shellOnly.coordinate, "\(accepted.labels)")

    let seamOnly = TargetSession(
        backend: .iterm, id: "SESSION-ROUTE-SEAM", name: "seam", tty: "/dev/ttys935",
        windowIndex: 0, tabIndex: 2, assistant: nil, cwd: nil)
    RemoteServer.sessionPayloadForTesting = ([seamOnly], [seamOnly.id: .idle])
    let seamReader = RemoteAuth.addDevice(name: "detail route seam", caps: [.read])
    let seamOpened = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/sessions/\(seamOnly.id)",
        headers: ["Authorization": "Bearer \(seamReader.token)"]))
    RemoteAuth.revoke(id: seamReader.id)
    RemoteServer.sessionPayloadForTesting = nil
    expect("the detail route consumes the same fixture inventory as the list route",
           seamOpened.status, 200)

    let spawn = Date(timeIntervalSince1970: 2_000)
    expect("registry provenance stays on the registry identity ladder",
           Orchestrator.publishedClaudeConversation(
                id: conversationID, source: .registry, sourceObservedAt: observedAt,
                spawnedAt: spawn).registryID,
           conversationID)
    expect("a tty hook older than the task remains fail closed",
           Orchestrator.publishedClaudeConversation(
                id: conversationID, source: .hook,
                sourceObservedAt: spawn.addingTimeInterval(-1), spawnedAt: spawn).hookID,
           nil)
    expect("a fresh published tty hook stays distinct from registry evidence",
           Orchestrator.publishedClaudeConversation(
                id: conversationID, source: .hook,
                sourceObservedAt: spawn, spawnedAt: spawn).hookID,
           conversationID)

    let processStamp = Date(timeIntervalSince1970: 1_700_000_123)
    let stampedScan = ITerm.parseAssistantProcessScan(
        "?? 1 0 Mon Jan  1 00:00:00 2024 /sbin/launchd", timedOut: false,
        observedAt: processStamp)
    expect("process evidence keeps the instant before the terminal walk",
           stampedScan.observedAt, processStamp)

    // Hold one ps boundary open, then ask again on this thread. A cache lock that is released
    // before launch admits both callers; real backpressure returns an incomplete observation
    // immediately and leaves the first caller as the only subprocess owner.
    let processScanFixtureWasIdle = ITerm.resetAssistantProcessScanForTesting()
    let firstProcessScanFinished = DispatchSemaphore(value: 0)
    ITerm.assistantProcessRunForTesting = ("ttys006 101 1 claude", false, 0)
    ITerm.assistantProcessDelayForTesting = 0.25
    let processScanFixtureQueue = DispatchQueue(label: "process-scan-single-flight-fixture")
    processScanFixtureQueue.async {
        _ = ITerm.assistantProcessScan()
        firstProcessScanFinished.signal()
    }
    let firstAdmissionDeadline = Date().addingTimeInterval(2)
    while !ITerm.assistantProcessScanInFlightForTesting() && Date() < firstAdmissionDeadline {
        Thread.sleep(forTimeInterval: 0.002)
    }
    let firstProcessScanStarted = ITerm.assistantProcessScanInFlightForTesting()
    check("the assistant process scan fixture starts from an idle admitted boundary",
          processScanFixtureWasIdle && firstProcessScanStarted)
    let coalescedProcessScan = firstProcessScanStarted
        ? ITerm.assistantProcessScan()
        : ITerm.AssistantProcessScan(assistants: [:], error: "fixture was not admitted")
    let admittedProcessScans = ITerm.assistantProcessRunCountForTesting()
    check("concurrent assistant process refreshes admit one subprocess and fail closed without waiting",
          admittedProcessScans == 1 && !coalescedProcessScan.isComplete
            && coalescedProcessScan.error?.contains("already in flight") == true,
          "calls=\(admittedProcessScans), error=\(coalescedProcessScan.error ?? "nil")")
    let firstProcessScanCompleted = firstProcessScanFinished.wait(timeout: .now() + 2) == .success

    let staleBoundaryReady = firstProcessScanCompleted
        && ITerm.expireAssistantProcessCacheForTesting()
    let refreshProcessScanFinished = DispatchSemaphore(value: 0)
    ITerm.assistantProcessRunForTesting = ("ttys006 202 1 claude", false, 0)
    processScanFixtureQueue.async {
        _ = ITerm.assistantProcessScan()
        refreshProcessScanFinished.signal()
    }
    let refreshAdmissionDeadline = Date().addingTimeInterval(2)
    while !ITerm.assistantProcessScanInFlightForTesting() && Date() < refreshAdmissionDeadline {
        Thread.sleep(forTimeInterval: 0.002)
    }
    let refreshProcessScanStarted = ITerm.assistantProcessScanInFlightForTesting()
    let staleProcessScan = refreshProcessScanStarted
        ? ITerm.assistantProcessScan()
        : ITerm.AssistantProcessScan(assistants: [:], error: "refresh fixture was not admitted")
    check("a coalesced refresh keeps prior process evidence but cannot claim it is fresh",
          staleBoundaryReady && refreshProcessScanStarted
            && ITerm.assistantProcessRunCountForTesting() == 2
            && staleProcessScan.assistants["ttys006"]?.pid == 101
            && !staleProcessScan.isComplete && staleProcessScan.coalesced
            && staleProcessScan.error?.contains("already in flight") == true,
          "ready=\(staleBoundaryReady), started=\(refreshProcessScanStarted), "
            + "calls=\(ITerm.assistantProcessRunCountForTesting()), "
            + "pid=\(staleProcessScan.assistants["ttys006"].map { String($0.pid) } ?? "nil"), "
            + "error=\(staleProcessScan.error ?? "nil")")
    let refreshProcessScanCompleted = refreshProcessScanFinished.wait(timeout: .now() + 2) == .success
    ITerm.assistantProcessRunForTesting = nil
    ITerm.assistantProcessDelayForTesting = nil
    check("completed single-flight process scans return to an idle cache boundary",
          firstProcessScanCompleted && refreshProcessScanCompleted
            && ITerm.resetAssistantProcessScanForTesting())

    // Partial output, non-zero exit and a TERM-resistant timeout take different exits through
    // the same cleanup. Sequential repetitions make the expected high-water mark exactly one
    // process/four parent pipe handles; any accumulation changes the counters deterministically.
    expect("subprocess metrics reset remains observable before cleanup checks",
           ITerm.resetSubprocessMetricsForTesting(), true)
    let partial = ITerm.runSubprocessForTesting(
        "/bin/sh", ["-c", "printf partial; printf diagnostic >&2; exit 7"], timeout: 1)
    expect("a partial-output failure preserves stdout", partial.out, "partial")
    expect("a partial-output failure preserves its exit status", partial.status, 7)
    expect("a partial-output failure is not mislabeled timeout", partial.timedOut, false)
    let timeoutStart = Date()
    var timeoutReceipts = 0
    for _ in 0..<3 {
        let receipt = ITerm.runSubprocessForTesting(
            "/bin/sh", ["-c", "trap '' TERM; printf partial; while :; do :; done"],
            timeout: 0.15)
        if receipt.timedOut { timeoutReceipts += 1 }
    }
    let timeoutSeconds = Date().timeIntervalSince(timeoutStart)
    let cleanup = ITerm.subprocessMetrics()
    expect("every injected hung subprocess reaches its deadline", timeoutReceipts, 3)
    check("the timeout ladder has an explicit wall-clock upper bound",
          timeoutSeconds < 6, "\(timeoutSeconds) seconds")
    check("success, partial failure and timeout leave no process or pipe ownership behind",
          cleanup.started == 4 && cleanup.finished == 4 && cleanup.timedOut == 3
            && cleanup.forceKilled >= 1 && cleanup.inFlight == 0
            && cleanup.openPipeHandles == 0 && cleanup.peakInFlight == 1
            && cleanup.peakOpenPipeHandles == 4, "\(cleanup)")

    expect("the descendant-pipe probe starts from a clean metrics boundary",
           ITerm.resetSubprocessMetricsForTesting(), true)
    let inheritedStart = Date()
    let inherited = ITerm.runSubprocessForTesting(
        "/bin/sh", ["-c", "printf inherited; sleep 3 & exit 0"], timeout: 5)
    let inheritedSeconds = Date().timeIntervalSince(inheritedStart)
    let inheritedMetrics = ITerm.subprocessMetrics()
    check("a descendant-held pipe is returned only as fail-closed incomplete output",
          inherited.timedOut && inherited.status == nil && inherited.out.isEmpty,
          "\(inherited)")
    check("reader timeout closes every parent pipe on a bounded wall clock",
          inheritedSeconds < 2 && inheritedMetrics.readerTimeouts == 1
            && inheritedMetrics.timedOut == 1 && inheritedMetrics.inFlight == 0
            && inheritedMetrics.openPipeHandles == 0,
          "\(inheritedSeconds) seconds; \(inheritedMetrics)")

    expect("the in-flight reset probe starts from a clean boundary",
           ITerm.resetSubprocessMetricsForTesting(), true)
    let resetProbeFinished = DispatchSemaphore(value: 0)
    DispatchQueue.global().async {
        _ = ITerm.runSubprocessForTesting("/bin/sh", ["-c", "sleep 0.35"], timeout: 1)
        resetProbeFinished.signal()
    }
    let resetProbeDeadline = Date().addingTimeInterval(1)
    while ITerm.subprocessMetrics().inFlight == 0 && Date() < resetProbeDeadline {
        Thread.sleep(forTimeInterval: 0.005)
    }
    expect("metrics reset reports refusal while a subprocess owns resources",
           ITerm.resetSubprocessMetricsForTesting(), false)
    _ = resetProbeFinished.wait(timeout: .now() + 2)
    let afterResetProbe = ITerm.subprocessMetrics()
    check("the reset refusal does not disturb eventual subprocess cleanup",
          afterResetProbe.finished == 1 && afterResetProbe.inFlight == 0
            && afterResetProbe.openPipeHandles == 0, "\(afterResetProbe)")
}

group("whoami revalidates real Claude and Codex files across fresh process passes") {
    let path = "/v1/orchestrator/whoami"
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let conversation = "11111111-2222-4333-8444-555555555555"
    let replacement = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let external = FileManager.default.temporaryDirectory.appendingPathComponent(
        "clawdline-whoami-production-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: external, withIntermediateDirectories: true)
    let previousSessionRegistry = Config.shared.sessionRegistry
    defer {
        RemoteServer.sessionPayloadForTesting = nil
        RemoteServer.sessionConversationIDForTesting = nil
        RemoteServer.sessionIdentityPassDidFinishForTesting = nil
        ITerm.identityProcessEvidenceForTesting = nil
        Transcript.identityRegistryDirectoryForTesting = nil
        Transcript.identityClaudeProjectDirectoryForTesting = nil
        Transcript.identityHookDirectoryForTesting = nil
        Transcript.identityBackgroundProcessStartForTesting = nil
        Transcript.identityWorkingDirectoryForTesting = nil
        Config.shared.sessionRegistry = previousSessionRegistry
        SessionWatch.inventoryForTesting = nil
        SessionWatch.shared.stop()
        try? FileManager.default.removeItem(at: external)
    }

    let project = external.appendingPathComponent("project", isDirectory: true).path
    let rolloutDirectory = external.appendingPathComponent("sessions/2026/08/29",
                                                            isDirectory: true)
    try! FileManager.default.createDirectory(at: rolloutDirectory,
                                             withIntermediateDirectories: true)
    let rollout = rolloutDirectory.appendingPathComponent(
        "rollout-2026-08-29T08-00-00-whoami.jsonl")
    let spareRollout = rolloutDirectory.appendingPathComponent(
        "rollout-2026-08-29T08-00-01-spare.jsonl")
    func writeCodexIdentity(_ id: String, to url: URL = rollout) {
        try! Data("""
        {"type":"session_meta","payload":{"session_id":"\(id)","cwd":"\(project)","thread_source":"user","source":"cli"}}
        """.utf8).write(to: url)
    }
    writeCodexIdentity(conversation)
    writeCodexIdentity(replacement, to: spareRollout)

    let codex = TargetSession(
        backend: .iterm, id: "CODEX-WHOAMI", name: "codex", tty: "/dev/ttys811",
        windowIndex: 0, tabIndex: 0, assistant: .codex, cwd: project)
    let spare = TargetSession(
        backend: .iterm, id: "CODEX-SPARE", name: "spare", tty: "/dev/ttys812",
        windowIndex: 0, tabIndex: 1, assistant: .codex, cwd: project)
    check("the production fixture publishes multiple targets through SessionWatch",
          sessionWatchReading([codex, spare]))
    check("and whoami reads a complete real identity snapshot",
          eventually { SessionWatch.shared.identitySnapshot().complete })

    let codexPID: Int32 = 81_101
    let sparePID: Int32 = 81_102
    var processEvidenceReads = 0
    var populations: [Set<String>] = []
    ITerm.identityProcessEvidenceForTesting = { requestedTTYs in
        processEvidenceReads += 1
        populations.append(requestedTTYs)
        return ITerm.IdentityProcessEvidence(
            scan: ITerm.AssistantProcessScan(assistants: [
                "ttys811": Assistant.Running(assistant: .codex, pid: codexPID,
                    processStart: Date(timeIntervalSince1970: 1_800_000_000)),
                "ttys812": Assistant.Running(assistant: .codex, pid: sparePID,
                    processStart: Date(timeIntervalSince1970: 1_800_000_001)),
            ], error: nil),
            openFilesByPID: [codexPID: [rollout.path], sparePID: [spareRollout.path]])
    }
    RemoteServer.sessionIdentityPassDidFinishForTesting = { pass in
        if pass == 1 { writeCodexIdentity(replacement) }
    }
    let codexMoved = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("an unchanged Codex rollout path is freshly re-read after its identity changes",
           codexMoved.status, 409)
    expect("the Codex rollout mutation is a named stale identity",
           remoteErrorCode(codexMoved), "session_identity_stale")
    expect("Codex revalidation makes two independent process-evidence readings",
           processEvidenceReads, 2)
    check("each pass reads the whole frozen target population once",
          populations == Array(repeating: Set([codex.tty, spare.tty]), count: 2),
          "\(populations)")

    // Claude uses the same production reader shape, with the process registry and exact named
    // transcript redirected into this fixture's private directory. Only the timing seam mutates
    // the external file between passes.
    let registryDirectory = external.appendingPathComponent("registry", isDirectory: true)
    let claudeProject = external.appendingPathComponent("claude-project", isDirectory: true)
    try! FileManager.default.createDirectory(at: registryDirectory,
                                             withIntermediateDirectories: true)
    try! FileManager.default.createDirectory(at: claudeProject,
                                             withIntermediateDirectories: true)
    let claudePID: Int32 = 81_103
    let procStart = "Tue Aug 25 06:21:43 2026"
    let claudeStarted = SessionRegistry.procStartDate(procStart)!
    func writeClaudeIdentity(_ id: String) {
        let json = """
        {"pid":\(claudePID),"sessionId":"\(id)","cwd":"\(project)","procStart":"\(procStart)","peerProtocol":1,"kind":"interactive","status":"busy"}
        """
        try! Data(json.utf8).write(
            to: registryDirectory.appendingPathComponent("\(claudePID).json"))
    }
    try! Data("{}\n".utf8).write(
        to: claudeProject.appendingPathComponent("\(conversation).jsonl"))
    try! Data("{}\n".utf8).write(
        to: claudeProject.appendingPathComponent("\(replacement).jsonl"))
    writeClaudeIdentity(conversation)
    let claude = TargetSession(
        backend: .iterm, id: "CLAUDE-WHOAMI", name: "claude", tty: "/dev/ttys813",
        windowIndex: 0, tabIndex: 2, assistant: .claude)
    check("the Claude fixture has the production iTerm shape with no stored cwd",
          claude.cwd == nil)
    Transcript.identityWorkingDirectoryForTesting = { candidate in
        candidate.id == claude.id ? project : nil
    }
    check("the same production snapshot path can publish both assistants",
          sessionWatchReading([claude, codex]))
    Transcript.identityRegistryDirectoryForTesting = registryDirectory
    Transcript.identityClaudeProjectDirectoryForTesting = claudeProject
    processEvidenceReads = 0
    populations = []
    ITerm.identityProcessEvidenceForTesting = { requestedTTYs in
        processEvidenceReads += 1
        populations.append(requestedTTYs)
        return ITerm.IdentityProcessEvidence(
            scan: ITerm.AssistantProcessScan(assistants: [
                "ttys813": Assistant.Running(assistant: .claude, pid: claudePID,
                                               processStart: claudeStarted),
                "ttys811": Assistant.Running(assistant: .codex, pid: codexPID,
                    processStart: Date(timeIntervalSince1970: 1_800_000_000)),
            ], error: nil),
            openFilesByPID: [codexPID: [rollout.path]])
    }
    RemoteServer.sessionIdentityPassDidFinishForTesting = { pass in
        if pass == 1 { writeClaudeIdentity(replacement) }
    }
    let claudeMoved = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("Claude's registry file is freshly re-read after its conversation changes",
           claudeMoved.status, 409)
    expect("the Claude registry mutation is the same named stale identity",
           remoteErrorCode(claudeMoved), "session_identity_stale")
    expect("Claude revalidation also makes two independent process-evidence readings",
           processEvidenceReads, 2)
    check("Claude and Codex rows share each pass instead of rescanning per target",
          populations == Array(repeating: Set([claude.tty, codex.tty]), count: 2),
          "\(populations)")

    // The positive production path has no route identity seam: the same uncached registry and
    // transcript readers which detected the mutation above have to serialize a successful 200.
    writeClaudeIdentity(conversation)
    RemoteServer.sessionIdentityPassDidFinishForTesting = nil
    let claudeFresh = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("a stable real Claude registry binding succeeds without the route identity seam",
           claudeFresh.status, 200)
    let claudeFreshBody = (try? JSONSerialization.jsonObject(
        with: claudeFresh.body)) as? [String: Any]
    expect("the production success serializes the exact proved conversation",
           claudeFreshBody?["conversation_id"] as? String, conversation)
    expect("and its current terminal-neutral address",
           claudeFreshBody?["terminal_id"] as? String, claude.id)

    // Parking leaves the interactive registry file behind and moves the live conversation into
    // a background process. Its pid must really be alive because attachBackground deliberately
    // asks the kernel before reading a historical directory entry.
    let parkedJob = "whoami1"
    let backgroundPID = getpid()
    let backgroundStarted = SessionRegistry.procStartDate("Tue Aug 25 06:22:43 2026")!
    Transcript.identityBackgroundProcessStartForTesting = { pid in
        pid == backgroundPID ? backgroundStarted : nil
    }
    let processDate = DateFormatter()
    processDate.locale = Locale(identifier: "en_US_POSIX")
    processDate.timeZone = TimeZone(identifier: "UTC")
    processDate.dateFormat = "EEE MMM d HH:mm:ss yyyy"
    func writeParkedIdentity(_ id: String, peerProtocol: Int = 1,
                             jobID: String = parkedJob,
                             processStart: Date = backgroundStarted) {
        let parked = """
        {"pid":\(claudePID),"sessionId":"parked-old","cwd":"\(project)","procStart":"\(procStart)","peerProtocol":1,"kind":"interactive","parkedJobId":"\(parkedJob)","status":"busy"}
        """
        let background = """
        {"pid":\(backgroundPID),"sessionId":"\(id)","cwd":"\(project)","procStart":"\(processDate.string(from: processStart))","peerProtocol":\(peerProtocol),"kind":"bg","jobId":"\(jobID)","status":"busy"}
        """
        try! Data(parked.utf8).write(
            to: registryDirectory.appendingPathComponent("\(claudePID).json"))
        try! Data(background.utf8).write(
            to: registryDirectory.appendingPathComponent("\(backgroundPID).json"))
    }
    writeParkedIdentity(conversation)
    let parked = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("a parked Claude tab resolves through its exact live background job", parked.status,
           200)
    let parkedBody = (try? JSONSerialization.jsonObject(with: parked.body)) as? [String: Any]
    expect("the parked route returns the interactive tab which owns that job",
           parkedBody?["terminal_id"] as? String, claude.id)

    RemoteServer.sessionIdentityPassDidFinishForTesting = { pass in
        if pass == 1 { writeParkedIdentity(replacement) }
    }
    writeParkedIdentity(conversation)
    let parkedMoved = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("parked background evidence changing between passes fails closed",
           parkedMoved.status, 409)
    expect("the parked change has the typed two-pass stale code",
           remoteErrorCode(parkedMoved), "session_identity_stale")

    RemoteServer.sessionIdentityPassDidFinishForTesting = nil
    writeParkedIdentity(conversation, peerProtocol: 0)
    let parkedOldProtocol = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("a parked background with the wrong protocol stays unresolved",
           parkedOldProtocol.status, 404)
    writeParkedIdentity(conversation, jobID: "another-job")
    let parkedWrongJob = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("a parked background from another job stays unresolved",
           parkedWrongJob.status, 404)
    writeParkedIdentity(conversation,
                        processStart: backgroundStarted.addingTimeInterval(60))
    let parkedWrongStart = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("a parked background from another process start stays unresolved",
           parkedWrongStart.status, 404)
    writeParkedIdentity(conversation)
    try? FileManager.default.removeItem(
        at: registryDirectory.appendingPathComponent("\(backgroundPID).json"))
    let parkedMissingBackground = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("a parked tab never falls back to its frozen identity without a background",
           parkedMissingBackground.status, 404)

    // A hook note is the strict legacy source: it names one transcript for this exact tty. The
    // registry is disabled here, so no registry row can make either success pass accidentally.
    let hookDirectory = external.appendingPathComponent("hooks", isDirectory: true)
    try! FileManager.default.createDirectory(at: hookDirectory,
                                             withIntermediateDirectories: true)
    Transcript.identityHookDirectoryForTesting = hookDirectory
    Config.shared.sessionRegistry = false
    func writeHookIdentity(_ id: String, tty: String = "ttys813") {
        let json = """
        {"event":"SessionStart","kind":"session_start","tty":"\(tty)","at":1787990400,"session":"\(id)"}
        """
        try! Data(json.utf8).write(
            to: hookDirectory.appendingPathComponent("ttys813.json"))
    }
    RemoteServer.sessionIdentityPassDidFinishForTesting = nil
    writeHookIdentity(conversation)
    let hook = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("a production-shaped no-cwd iTerm row resolves exact hook evidence through the dispatch working-directory fallback",
           hook.status, 200)
    let hookBody = (try? JSONSerialization.jsonObject(with: hook.body)) as? [String: Any]
    expect("the hook source still returns only its exact terminal",
           hookBody?["terminal_id"] as? String, claude.id)

    RemoteServer.sessionIdentityPassDidFinishForTesting = { pass in
        if pass == 1 { writeHookIdentity(replacement) }
    }
    writeHookIdentity(conversation)
    let hookMoved = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("hook evidence changing between passes fails closed", hookMoved.status, 409)
    expect("the hook change has the typed two-pass stale code",
           remoteErrorCode(hookMoved), "session_identity_stale")

    RemoteServer.sessionIdentityPassDidFinishForTesting = nil
    writeHookIdentity(conversation, tty: "ttys999")
    let hookWrongTTY = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("a hook whose embedded tty disagrees with its exact filename stays unresolved",
           hookWrongTTY.status, 404)
    let missingTranscript = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
    writeHookIdentity(missingTranscript)
    let hookMissingTranscript = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(missingTranscript)", headers: auth))
    expect("a hook cannot resolve a transcript which does not exist",
           hookMissingTranscript.status, 404)
    let oldTranscript = "cccccccc-dddd-4eee-8fff-000000000000"
    let oldTranscriptURL = claudeProject.appendingPathComponent("\(oldTranscript).jsonl")
    try! Data("{}\n".utf8).write(to: oldTranscriptURL)
    try! FileManager.default.setAttributes(
        [.modificationDate: claudeStarted.addingTimeInterval(-1)],
        ofItemAtPath: oldTranscriptURL.path)
    writeHookIdentity(oldTranscript)
    let hookOldTranscript = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(oldTranscript)", headers: auth))
    expect("a hook cannot resolve a transcript older than the current process",
           hookOldTranscript.status, 404)
    writeHookIdentity("")
    let hookEmptyID = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("a hook with an empty conversation id stays unresolved", hookEmptyID.status, 404)
    try? FileManager.default.removeItem(
        at: hookDirectory.appendingPathComponent("ttys813.json"))
    let trulyMissing = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("without registry or hook evidence the production route remains a miss",
           trulyMissing.status, 404)
    expect("the genuine miss remains typed as conversation_not_found",
           remoteErrorCode(trulyMissing), "conversation_not_found")

    Config.shared.sessionRegistry = previousSessionRegistry
    Transcript.identityHookDirectoryForTesting = nil
    writeClaudeIdentity(conversation)

    // A partial terminal inventory goes through SessionWatch's real confidence seam. No payload
    // or conversation override is involved, so deleting the production complete guard makes
    // this named response change.
    SessionWatch.inventoryForTesting = {
        (scan: ITerm.AssistantProcessScan(assistants: [:], error: nil),
         snapshot: Targets.Snapshot(sessions: [claude, codex], currentID: nil,
                                    error: "fixture incomplete", isComplete: false))
    }
    SessionWatch.shared.start()
    check("the incomplete fixture is published as incomplete by SessionWatch",
          eventually { !SessionWatch.shared.identitySnapshot().complete })
    RemoteServer.sessionIdentityPassDidFinishForTesting = nil
    let incomplete = RemoteServer.shared.route(remoteRequest(
        "GET", "\(path)?conversation_id=\(conversation)", headers: auth))
    expect("an incomplete real registry snapshot fails closed", incomplete.status, 409)
    expect("and names the registry confidence failure", remoteErrorCode(incomplete),
           "registry_stale")
}

group("a reading with live sessions in it reaches the ledger, both ways") {
    let fm = FileManager.default
    let store = freshUsageLedger()
    defer { forgetUsageLedger(store) }
    Orchestrator.forget()
    UsageLedger.forgetWatchedForTesting()

    let codexHome = store.appendingPathComponent("codex", isDirectory: true)
    let day = codexHome.appendingPathComponent("sessions/2026/08/29", isDirectory: true)
    try! fm.createDirectory(at: day, withIntermediateDirectories: true)
    let project = store.appendingPathComponent("project", isDirectory: true).path
    let rollout = day.appendingPathComponent("rollout-2026-08-29T09-00-00-watch-live.jsonl")
    try! Data("""
    {"type":"session_meta","payload":{"session_id":"watch-live","cwd":"\(project)",\
    "originator":"codex-tui","thread_source":"user","source":"cli"}}
    {"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":\
    {"input_tokens":10,"output_tokens":5,"cached_input_tokens":85,\
    "cache_write_input_tokens":0,"total_tokens":100}}}}
    """.utf8).write(to: rollout)

    let previousCodexHome = Config.shared.codexHome
    let previousAutoName = Config.shared.codexAutoName
    Config.shared.codexHome = codexHome.path
    Config.shared.codexAutoName = false
    defer {
        Config.shared.codexHome = previousCodexHome
        Config.shared.codexAutoName = previousAutoName
        Orchestrator.forget()
        // The timer `start()` installed is on `RunLoop.main` in the common modes, and
        // `eventually` pumps that run loop all through the rest of the suite — so a watch left
        // running would take a real `ps` and a real Apple-event inventory somewhere in the middle
        // of an unrelated group. The seam is deliberately *not* cleared with it: the registry
        // watcher `start()` installed outlives `stop()`, and a reading that got through it after
        // this point should still be answered from a fixture rather than from the machine.
        SessionWatch.shared.stop()
        try? fm.removeItem(at: codexHome)
    }

    // A Codex tab whose rollout can be found, and a Claude tab whose transcript cannot. Both are
    // live targets; only the first is something the ledger can measure, which is the split the
    // collector already documents.
    let codexTab = TargetSession(backend: .iterm, id: "TERM-WATCH-CODEX", name: "codex",
                                 tty: "/dev/ttys900", windowIndex: 0, tabIndex: 0,
                                 assistant: .codex, cwd: project)
    let claudeTab = TargetSession(backend: .iterm, id: "TERM-WATCH-CLAUDE", name: "claude",
                                  tty: "/dev/ttys901", windowIndex: 0, tabIndex: 1,
                                  assistant: .claude,
                                  cwd: store.appendingPathComponent("nowhere").path)

    check("a reading publishes what the inventory found", sessionWatchReading([codexTab, claudeTab]))
    expect("both tabs are live targets", SessionWatch.shared.targets.count, 2)

    // The wiring, not the arithmetic. What round one proved was that `checkpoint` computes the
    // right row when it is called; what nothing proved was that a reading calls it at all.
    check("the reading checkpointed the session it could resolve",
          eventually { UsageLedger.shared.rows().contains { $0.sessionID == "watch-live" } },
          "\(UsageLedger.shared.rows().map(\.sessionID))")
    let live = UsageLedger.shared.rows().first { $0.sessionID == "watch-live" }
    expect("carrying what that rollout says", live?.total, 100)
    expect("filed as a session a person opened", live?.origin, "manual")
    expect("and open, because the session is still there", live?.sealed, false)

    // The other half of the same wire. A session that leaves a *complete* reading is one of the
    // three forced checkpoints, and `apply` is the only caller of it in the app.
    check("a second reading without it publishes the loss", sessionWatchReading([claudeTab]))
    check("the session that disappeared is sealed by the reading that lost it",
          eventually {
              UsageLedger.shared.rows().first { $0.sessionID == "watch-live" }?.sealed == true
          },
          "\(UsageLedger.shared.rows().map { ($0.sessionID, $0.sealed) })")
    expect("sealed complete, because its rollout was still readable",
           UsageLedger.shared.rows().first { $0.sessionID == "watch-live" }?.coverage, "complete")

    // Left behind on purpose: the crossings probe in the asynchronous half of this file asks
    // `SessionWatch` for a target, and a target that came out of a real reading is the whole
    // point — an empty list is how both of those crossings stayed dormant for a release.
    expect("and the Claude tab is still live for the crossings to be asked about",
           SessionWatch.shared.targets.map(\.id), ["TERM-WATCH-CLAUDE"])
}

group("a manual session refresh has coherent evidence and bounded backpressure") {
    SessionWatch.shared.stop()
    let previousInventory = SessionWatch.inventoryForTesting
    let previousClock = SessionWatch.refreshClockForTesting
    let previousSchedule = SessionWatch.refreshScheduleForTesting
    let retainedTargets = SessionWatch.shared.targets
    let lock = NSLock()
    let releaseFirst = DispatchSemaphore(value: 0)
    let releaseStopped = DispatchSemaphore(value: 0)
    let releaseNudge = DispatchSemaphore(value: 0)
    var reads = 0
    var publishedCompletions = 0
    var now: TimeInterval = 10_000
    var scheduled: [(delay: TimeInterval, work: DispatchWorkItem)] = []
    SessionWatch.refreshClockForTesting = { now }
    SessionWatch.refreshScheduleForTesting = { delay, work in
        scheduled.append((delay, work))
    }
    SessionWatch.shared.scanCompletionObservers["manual-refresh-test"] = {
        publishedCompletions += 1
    }
    SessionWatch.inventoryForTesting = {
        lock.lock()
        reads += 1
        let thisRead = reads
        lock.unlock()
        if thisRead == 1 { _ = releaseFirst.wait(timeout: .now() + 2) }
        if thisRead == 4 { _ = releaseStopped.wait(timeout: .now() + 2) }
        if thisRead == 5 { _ = releaseNudge.wait(timeout: .now() + 2) }
        if thisRead == 6 {
            return (scan: ITerm.AssistantProcessScan(
                        assistants: [:], error: "fixture process inventory failed"),
                    snapshot: Targets.Snapshot(sessions: [], currentID: nil, error: nil,
                                               isComplete: false))
        }
        return (scan: ITerm.AssistantProcessScan(assistants: [:], error: nil),
                snapshot: Targets.Snapshot(sessions: retainedTargets, currentID: nil, error: nil,
                                           isComplete: true))
    }
    defer {
        releaseFirst.signal(); releaseStopped.signal(); releaseNudge.signal()
        SessionWatch.shared.stop()
        SessionWatch.inventoryForTesting = previousInventory
        SessionWatch.refreshClockForTesting = previousClock
        SessionWatch.refreshScheduleForTesting = previousSchedule
        SessionWatch.shared.scanCompletionObservers.removeValue(forKey: "manual-refresh-test")
    }

    let reader = RemoteAuth.addDevice(name: "a phone refreshing session evidence", caps: [.read])
    defer { RemoteAuth.revoke(id: reader.id) }
    func body(_ response: RemoteServer.Response) -> [String: Any] {
        ((try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]) ?? [:]
    }
    func request() -> RemoteServer.Response {
        RemoteServer.shared.route(remoteRequest(
            "POST", "/v1/sessions/refresh",
            headers: ["Authorization": "Bearer \(reader.token)"]))
    }
    func state(_ response: RemoteServer.Response) -> String? { body(response)["state"] as? String }
    func completed(_ payload: [String: Any]) -> [String: Any] {
        ((payload["scan"] as? [String: Any])?["completed"] as? [String: Any]) ?? [:]
    }
    func dispositionIsCoherent(_ payload: [String: Any]) -> Bool {
        guard let named = payload["state"] as? String else { return false }
        return payload["accepted"] as? Bool == (named == "accepted")
            && payload["coalesced"] as? Bool == (named == "coalesced")
            && payload["throttled"] as? Bool == (named == "throttled")
    }

    expect("an anonymous retry is refused",
           RemoteServer.shared.route(remoteRequest("POST", "/v1/sessions/refresh")).status, 401)
    if let remoteSource = try? String(contentsOfFile: "Sources/RemoteServer.swift", encoding: .utf8) {
        let nudgeLines = codeOnly(remoteSource).split(separator: "\n").filter {
            $0.contains("SessionWatch.shared.nudge()")
        }
        check("every RemoteServer nudge re-enters the main queue", !nudgeLines.isEmpty &&
              nudgeLines.allSatisfy { $0.contains("DispatchQueue.main.async") },
              nudgeLines.joined(separator: " | "))
    } else {
        check("RemoteServer source is readable for the nudge ownership guard", false)
    }
    let completionBeforeRequest = SessionWatch.shared.completedScanSequence
    let accepted = request()
    expect("a paired read-only device may ask for fresh evidence", accepted.status, 200)
    expect("the response truthfully names a newly accepted read", state(accepted), "accepted")
    let acceptedBody = body(accepted)
    expect("the accepted response body carries ok", acceptedBody["ok"] as? Bool, true)
    expect("an accepted response sets only its matching disposition", acceptedBody["accepted"] as? Bool,
           true)
    expect("and is neither coalesced", acceptedBody["coalesced"] as? Bool, false)
    expect("nor throttled", acceptedBody["throttled"] as? Bool, false)
    expect("the acknowledgement has a closed top-level schema", Set(acceptedBody.keys),
           Set(["ok", "state", "accepted", "coalesced", "throttled", "scan"]))
    expect("the acknowledgement scan has only its completion baseline",
           Set(((acceptedBody["scan"] as? [String: Any]) ?? [:]).keys), Set(["completed"]))
    expect("the completion baseline object has a closed schema",
           Set(completed(acceptedBody).keys), Set(["sequence"]))
    expect("the ack carries the coherent pre-request completed-scan sequence",
           completed(acceptedBody)["sequence"] as? Int, completionBeforeRequest)
    check("the completion baseline is a non-negative JavaScript safe integer",
          completionBeforeRequest >= 0 && completionBeforeRequest <= 9_007_199_254_740_991)
    check("the accepted acknowledgement's state and Booleans agree",
          dispositionIsCoherent(acceptedBody))
    check("the accepted retry starts a real SessionWatch reading", eventually {
        lock.lock(); defer { lock.unlock() }
        return reads == 1
    })

    // The first inventory is deliberately blocked. Every later request joins one Boolean debt;
    // none can create a second worker or claim that it was independently accepted.
    let coalesced = request()
    expect("a request behind an in-flight read says coalesced", state(coalesced), "coalesced")
    expect("the coalesced response body is not accepted", body(coalesced)["accepted"] as? Bool,
           false)
    expect("and explicitly marks the coalesced disposition", body(coalesced)["coalesced"] as? Bool,
           true)
    expect("without marking it throttled", body(coalesced)["throttled"] as? Bool, false)
    check("the coalesced acknowledgement's state and Booleans agree",
          dispositionIsCoherent(body(coalesced)))
    for _ in 0..<8 { expect("each extra in-flight request is coalesced", state(request()), "coalesced") }
    releaseFirst.signal()
    check("completion schedules exactly one floor-bounded follow-up", eventually {
        scheduled.filter { abs($0.delay - 1.2) < 0.001 }.count == 1
    })
    expect("an unchanged completed read uses the completion publication lane",
           publishedCompletions, 1)
    lock.lock(); let beforeFloor = reads; lock.unlock()
    expect("the debt does not run terminal automation before its floor", beforeFloor, 1)
    let firstFloor = scheduled.removeFirst()
    let completionBeforeFollowup = SessionWatch.shared.completedScanSequence
    now += firstFloor.delay
    firstFloor.work.perform()
    check("the one coalesced debt becomes one completed follow-up read", eventually {
        SessionWatch.shared.completedScanSequence > completionBeforeFollowup
    })
    lock.lock(); let readsAfterFollowup = reads; lock.unlock()
    expect("the coalesced debt bought exactly one read", readsAfterFollowup, 2)

    // A completed read starts a new floor. The first request is explicitly throttled, and all
    // requests after it join the same scheduled purchase rather than extending the deadline.
    let throttled = request()
    expect("a request inside the completed-read floor says throttled", state(throttled), "throttled")
    let throttledBody = body(throttled)
    expect("the throttled body marks throttled", throttledBody["throttled"] as? Bool, true)
    expect("and does not pretend it was accepted", throttledBody["accepted"] as? Bool, false)
    expect("or that it joined an already-scheduled request", throttledBody["coalesced"] as? Bool,
           false)
    check("the throttled acknowledgement's state and Booleans agree",
          dispositionIsCoherent(throttledBody))
    let schedulesAtFloor = scheduled.count
    for _ in 0..<20 { expect("a floor burst is coalesced", state(request()), "coalesced") }
    expect("a floor burst buys only one follow-up schedule", scheduled.count, schedulesAtFloor)
    let secondFloor = scheduled.removeFirst()
    let completionBeforeFloorBurst = SessionWatch.shared.completedScanSequence
    now += secondFloor.delay
    secondFloor.work.perform()
    check("the floor burst buys one completed inventory read", eventually {
        SessionWatch.shared.completedScanSequence > completionBeforeFloorBurst
    })
    lock.lock(); let readsAfterFloorBurst = reads; lock.unlock()
    expect("the floor burst buys one and only one inventory read", readsAfterFloorBurst, 3)

    // stop() does not claim to cancel terminal work already in flight. It does cancel the debt
    // behind that work, so its eventual completion must not make another read after the stop.
    now += 2
    expect("a post-floor request can start another read", state(request()), "accepted")
    check("the stop fixture has one read in flight", eventually {
        lock.lock(); defer { lock.unlock() }
        return reads == 4
    })
    expect("a request behind the stop fixture is coalesced", state(request()), "coalesced")
    let schedulesBeforeStop = scheduled.count
    let completionBeforeStoppedRead = SessionWatch.shared.completedScanSequence
    SessionWatch.shared.stop()
    releaseStopped.signal()
    check("the already-started read is allowed to finish after stop", eventually {
        SessionWatch.shared.completedScanSequence > completionBeforeStoppedRead
    })
    expect("stop clears debt and schedules no payment after the active read returns",
           scheduled.count, schedulesBeforeStop)
    lock.lock(); let readsAfterStop = reads; lock.unlock()
    expect("stop does not launch a hidden follow-up", readsAfterStop, 4)

    // The first nudge owns the settle deadline. Repeated nudges cannot cancel and recreate it,
    // which would otherwise postpone fresh evidence forever on a noisy event stream.
    now += 2
    SessionWatch.shared.nudge()
    check("the nudge fixture starts its read", eventually {
        lock.lock(); defer { lock.unlock() }
        return reads == 5
    })
    let firstSettle = scheduled.first { abs($0.delay - 2.5) < 0.001 }?.work
    for _ in 0..<12 { SessionWatch.shared.nudge() }
    let settleWorks = scheduled.filter { abs($0.delay - 2.5) < 0.001 }.map(\.work)
    expect("repeated nudges retain one bounded settle deadline", settleWorks.count, 1)
    check("and retain the original scheduled work item", settleWorks.first === firstSettle)
    SessionWatch.shared.stop()
    let completionBeforeStoppedNudge = SessionWatch.shared.completedScanSequence
    releaseNudge.signal()
    check("the already-started nudge read is allowed to finish after stop", eventually {
        SessionWatch.shared.completedScanSequence > completionBeforeStoppedNudge
    })
    lock.lock(); let finalReads = reads; lock.unlock()
    expect("a stopped settle/debt pair performs no post-stop read", finalReads, 5)

    // Completion and content are separate facts. This read fails before reconciliation, so it
    // must publish a new completion receipt without manufacturing a successful content update.
    now += 2
    let contentBeforeFailure = SessionWatch.shared.scanGeneration
    let completionBeforeFailure = SessionWatch.shared.completedScanSequence
    expect("a post-stop manual failure fixture is admitted", state(request()), "accepted")
    check("the failed inventory still publishes exact completion evidence", eventually {
        SessionWatch.shared.completedScanSequence > completionBeforeFailure
    })
    expect("a failed inventory does not advance reconciled content generation",
           SessionWatch.shared.scanGeneration, contentBeforeFailure)
    expect("the exact completed scan is marked incomplete",
           SessionWatch.shared.completedScanComplete, false)
    let afterFailure = body(RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/sessions", headers: ["Authorization": "Bearer \(reader.token)"])))
    expect("the sessions payload publishes the failed scan's exact sequence",
           completed(afterFailure)["sequence"] as? Int,
           SessionWatch.shared.completedScanSequence)
    expect("the sessions payload makes the failed scan visible",
           completed(afterFailure)["complete"] as? Bool, false)
    check("the sessions completion sequence remains a JavaScript safe integer",
          (completed(afterFailure)["sequence"] as? Int).map {
              $0 >= 0 && $0 <= 9_007_199_254_740_991
          } == true)
    check("the retry fixture preserves the live targets the crossing tests inherit", eventually {
        SessionWatch.shared.targets.map(\.id) == retainedTargets.map(\.id)
    })
}

group("what a terminal's spend is filed under, and what the backfill hands over") {
    Orchestrator.forget()
    defer { Orchestrator.forget() }

    // The one place in the app that turns an `Orchestrator.Usage` back into a source object. A
    // misspelled key here drops a field in production and nowhere else — and the field most
    // likely to be dropped is the cache read, which is 96.6% of every token on this machine.
    var usage = Orchestrator.Usage()
    usage.input = 2
    usage.output = 3
    usage.cacheRead = 160_655
    usage.cacheWrite = 5
    usage.total = 160_665
    usage.model = "claude-opus-5"
    usage.costUsd = 5.469
    let raw = UsageLedger.rawUsage(of: usage)
    expect("the usage object goes back out in the registry's own spelling",
           Set(raw.keys), Set(["input", "output", "cache_read", "cache_write", "total", "model",
                               "cost_usd"]))
    expect("with the cache read under the key the ledger reads", raw["cache_read"] as? Int,
           160_655)
    expect("and the recorded cost under its own", raw["cost_usd"] as? Double, 5.469)
    let read = UsageLedger.normalize(raw: raw, assistant: .claude)
    expect("so a round trip through it loses nothing", read.counts.cacheRead, 160_655)
    expect("and the parts still sum back to the total", read.counts.total, 160_665)
    var bare = Orchestrator.Usage()
    bare.total = 0
    check("a usage with no model and no cost invents neither",
          UsageLedger.rawUsage(of: bare)["model"] == nil
            && UsageLedger.rawUsage(of: bare)["cost_usd"] == nil)

    // Which task a watched terminal's spend belongs to. A live task owns the tab; an attached
    // task is a guest and owns nothing once it ends, because a standing session wearing a
    // finished task's name is the one shape this must not have.
    func task(_ id: String, _ state: Orchestrator.State, terminal: String,
              created: Date, attached: String? = nil) -> Orchestrator.Task {
        var task = Orchestrator.Task(id: id, state: state, kind: "code", title: "fixture",
                                     assistant: .claude, projectDir: "/tmp/project",
                                     timeoutMinutes: 30, created: created,
                                     secretHash: String(repeating: "0", count: 64))
        task.childTerminalId = terminal
        task.attachSessionId = attached
        return task
    }
    let old = task("aaaaaaaa-0000-4000-8000-000000000001", .success, terminal: "TERM-1",
                   created: Date(timeIntervalSince1970: 1_000))
    let recent = task("aaaaaaaa-0000-4000-8000-000000000002", .success, terminal: "TERM-1",
                      created: Date(timeIntervalSince1970: 2_000))
    let live = task("aaaaaaaa-0000-4000-8000-000000000003", .briefed, terminal: "TERM-1",
                    created: Date(timeIntervalSince1970: 500))
    let guest = task("aaaaaaaa-0000-4000-8000-000000000004", .success, terminal: "TERM-2",
                     created: Date(timeIntervalSince1970: 3_000), attached: "standing-tab")
    for held in [old, recent, guest] { Orchestrator.holdScheduleTaskForTesting(held) }
    expect("the newest task that owned the tab, when none is live",
           Orchestrator.ledgerTaskRecord(forTerminal: "TERM-1")?["id"] as? String, recent.id)
    Orchestrator.holdScheduleTaskForTesting(live)
    expect("and the live one whenever there is one, however old",
           Orchestrator.ledgerTaskRecord(forTerminal: "TERM-1")?["id"] as? String, live.id)
    check("a finished guest never claims the session it was a guest in",
          Orchestrator.ledgerTaskRecord(forTerminal: "TERM-2") == nil)
    check("and a terminal this app never opened is nobody's",
          Orchestrator.ledgerTaskRecord(forTerminal: "TERM-NOBODY") == nil)
    check("what is handed to the ledger carries no credential",
          Orchestrator.ledgerTaskRecord(forTerminal: "TERM-1")?["secret_hash"] == nil
            && Orchestrator.ledgerTaskRecord(forTerminal: "TERM-1")?["queued_secret"] == nil)

    // And the backfill's own door: every row the registry still holds, oldest first, so that a
    // session's segments are opened in the order the work actually happened.
    let backfill = Orchestrator.ledgerBackfillRecords()
    expect("the backfill hands over every record the registry holds", backfill.count, 4)
    expect("oldest first, so a session's segments open in the order the work happened",
           backfill.compactMap { $0["created"] as? Double },
           [500, 1_000, 2_000, 3_000])
    check("and none of them carries a credential",
          backfill.allSatisfy { $0["secret_hash"] == nil && $0["queued_secret"] == nil })
}
}
