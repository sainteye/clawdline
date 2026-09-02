import Foundation

// The machine-level lease over the heavy-compile slot. Every group below is written against the
// defect it exists to prevent rather than against the code that prevents it, because two of them
// are corrections to a rule that was already wrong once tonight on this machine.

private let leaseNow = Date(timeIntervalSince1970: 1_788_370_000)
private let leaseDirectory = "/tmp/clawdline-lease-tests.lock"

private func leaseOwner(_ label: String, session: String? = nil, task: String? = nil)
    -> OrchestratorLease.Owner {
    OrchestratorLease.Owner(sessionID: session, taskID: task, label: label)
}

private func leaseRequest(_ id: String, label: String, pid: Int32 = 4_242,
                          session: String? = nil, task: String? = nil,
                          override: String? = nil,
                          start: Date? = leaseNow) -> OrchestratorLease.Request {
    OrchestratorLease.Request(
        requestID: id, resource: OrchestratorLease.heavyCompile,
        owner: leaseOwner(label, session: session, task: task), pid: pid, processStart: start,
        reason: "compiling the suite", tree: "/tmp/tree", log: "/tmp/log", note: nil,
        workPIDs: [], doneFlagPath: nil, phase: .analysing, pressureOverride: override)
}

private func leaseHolder(_ id: String, label: String, pid: Int32 = 4_242,
                         renewedAt: Date, session: String? = nil,
                         work: [Int32] = [],
                         phase: OrchestratorLease.Phase = .compiling,
                         phaseSince: Date = leaseNow,
                         lastCompilingAt: Date? = leaseNow) -> OrchestratorLease.Holder {
    OrchestratorLease.Holder(
        leaseID: id, owner: leaseOwner(label, session: session), pid: pid,
        processStart: leaseNow, acquiredAt: leaseNow, renewedAt: renewedAt, workPIDs: work,
        doneFlagPath: nil, tree: "/tmp/tree", log: "/tmp/log", note: nil, budget: nil,
        provenance: .broker, phase: phase, phaseSince: phaseSince,
        lastCompilingAt: lastCompilingAt)
}

private func heldRecord(_ holder: OrchestratorLease.Holder,
                        queue: [OrchestratorLease.Waiter] = []) -> OrchestratorLease.Record {
    OrchestratorLease.Record(resource: OrchestratorLease.heavyCompile,
                             directory: leaseDirectory, holder: holder, queue: queue,
                             reconciliation: .matched)
}

private func heldDirectory(_ holder: OrchestratorLease.Holder)
    -> OrchestratorLease.DirectoryState {
    .held(OrchestratorLease.file(from: holder))
}

/// A grant, unwrapped, or a named failure. Used everywhere a test needs the budget it produced.
private func leaseGrant(_ outcome: OrchestratorLease.Outcome) -> OrchestratorLease.Budget? {
    if case .granted(let budget) = outcome { return budget }
    return nil
}

private func leaseRefusal(_ outcome: OrchestratorLease.Outcome) -> OrchestratorLease.Refusal? {
    if case .refused(let refusal) = outcome { return refusal }
    return nil
}

private func leaseQueued(_ outcome: OrchestratorLease.Outcome)
    -> (position: Int, holdReason: String)? {
    if case .queued(let position, let reason) = outcome { return (position, reason) }
    return nil
}

func runOrchestratorLeaseTests() {

group("holder.txt is the shape three programs share, and an unusable one is not an empty lock") {
    let file = OrchestratorLease.HolderFile(
        holder: "root af1b83ba terminal 7E3C25BF", pid: 72_929,
        started: Date(timeIntervalSince1970: 1_788_365_342), tree: "/Users/x/code/clawdline",
        log: "/tmp/suite.log", note: "full swift suite", work: [81_001, 81_002],
        done: "/tmp/clawdline-suite.lock/done", renewed: leaseNow, phase: .compiling,
        heartbeat: "/tmp/clawdline-suite.lock/beat")
    let text = OrchestratorLease.encode(file)
    guard let parsed = OrchestratorLease.parseHolderFile(text) else {
        check("a written holder file reads back", false); return
    }
    expect("every field survives the round trip", parsed, file)
    check("the six original fields are all present as key=value lines",
          ["holder=", "pid=", "started=", "tree=", "log=", "note="]
            .allSatisfy { text.contains("\n" + $0) || text.hasPrefix($0) })
    check("and the three additive ones are written under the keys the lock really uses",
          ["work=", "done_flag=", "renewed=", "phase=", "heartbeat="]
            .allSatisfy { text.contains("\n" + $0) })

    // A shell holder that writes only the original six is still a holder. The additive fields
    // are optional by construction, which is what lets test.sh land before or after this.
    let shellWritten = """
    holder=test.sh on tty003
    pid=51
    started=1788365000
    tree=/Users/x/code/clawdline
    log=/tmp/x.log
    note=
    """
    guard let shell = OrchestratorLease.parseHolderFile(shellWritten) else {
        check("a six-field shell holder parses", false); return
    }
    expect("its pid is read", shell.pid, 51)
    expect("an empty note is absent rather than empty", shell.note, nil)
    expect("no work pids is an empty list, not a guess", shell.work, [])
    expect("and no renewal stamp is nil", shell.renewed, nil)
    expect("a holder that records no phase is `unknown`, not a guess", shell.phase, .unknown)

    // The bytes of the lock that was actually held on this Mac at 02:20 on 2026-09-03, copied
    // rather than invented. A hand-written fixture would have agreed with the parser and hidden
    // both of the differences below: `started=` is local time with a space, and the flag is
    // spelled `done_flag=`. Either one read as absent is a holder with no identity, which is
    // `unknown`, which blocks forever.
    let liveLock = """
    holder=clawdline child a77d9908 — compile peak measurement (task peak, graph e9bcefb4)
    pid=72929
    started=2026-09-03 02:08:54
    tree=493f0c0b6eba80ce3d46d8dc3b17f828fc3af672
    log=/tmp/.clawdline/a77d9908-1e9f-499d-a028-b9105ba46ecf/work/runs/
    note=This lock is held for the whole of task a77d9908, not for one compile.
    done_flag=/tmp/.clawdline/a77d9908-1e9f-499d-a028-b9105ba46ecf/work/DONE
    """
    guard let live = OrchestratorLease.parseHolderFile(liveLock) else {
        check("the lock that was really held on this machine parses", false); return
    }
    check("the lock that was really held on this machine parses", true)
    expect("its pid is the one it wrote", live.pid, 72_929)
    check("its local-time start is read rather than dropped", live.started != nil)
    expect("and the done flag is found under the key that file actually uses",
           live.done, "/tmp/.clawdline/a77d9908-1e9f-499d-a028-b9105ba46ecf/work/DONE")
    expect("`done=` stays an accepted alias so neither speller loses its flag",
           OrchestratorLease.parseHolderFile("pid=3\ndone=/tmp/x\n")?.done, "/tmp/x")

    let isoStamped = "pid=9\nstarted=2026-09-03T02:09:02Z\n"
    expect("an ISO-8601 UTC start is accepted, because a shell without %s writes that",
           OrchestratorLease.parseHolderFile(isoStamped)?.started,
           Date(timeIntervalSince1970: 1_788_401_342))
    check("a file with no usable pid is not a holder record",
          OrchestratorLease.parseHolderFile("holder=someone\nstarted=1\n") == nil)
    check("nor is a negative one", OrchestratorLease.parseHolderFile("pid=-4\n") == nil)
    check("an unknown key is ignored rather than fatal",
          OrchestratorLease.parseHolderFile("pid=7\nfuture_field=x\n")?.pid == 7)
    // The whole reason `unreadable` exists: a directory with no legible holder is neither free
    // nor a known holder, and both of the other answers would be a race.
    if case .unreadable = OrchestratorLease.readDirectory("/tmp") {
        check("a directory with no holder.txt reads as unreadable, never as free", true)
    } else {
        check("a directory with no holder.txt reads as unreadable, never as free", false)
    }
    if case .absent = OrchestratorLease.readDirectory("/tmp/clawdline-lease-tests-absent") {
        check("and a directory that is not there reads as absent", true)
    } else {
        check("and a directory that is not there reads as absent", false)
    }
}

group("liveness is proved by renewal, so a sentinel pid cannot hold a lock open") {
    // THE FIRST RED. Measured instance: a holder recorded pid=72929, and that pid was
    // `sleep 14400` with PPID 1 — a sentinel adopted by launchd. Under a pid-existence rule the
    // sentinel outlives its work for four hours and the lock is a permanent roadblock. Renewal
    // is what a `sleep` cannot do.
    let sentinelStillAlive = OrchestratorLease.ProcessObservation.running(leaseNow)
    let lapsed = leaseNow.addingTimeInterval(-600)
    let liveness = OrchestratorLease.liveness(
        renewedAt: lapsed, owner: .unknown, doneFlag: .absent, now: leaseNow)
    expect("a holder that stopped refreshing has stopped proving it is alive",
           liveness, .stopped("heartbeat_lapsed"))
    expect("and its sentinel process being alive changes nothing",
           OrchestratorLease.identity(recordedStart: leaseNow, observed: sentinelStillAlive),
           .alive)
    expect("so with no compiler on the machine the lease is reclaimable",
           OrchestratorLease.takeover(liveness: liveness, compilers: .none), .eligible)

    // THE SECOND RED, pointing the other way. "No swift-frontend means stale" reclaims the lock
    // in the gap between two compiles of one study, and the collision is back.
    let between = OrchestratorLease.liveness(
        renewedAt: leaseNow.addingTimeInterval(-5), owner: .live, doneFlag: .absent,
        now: leaseNow)
    expect("a holder renewing five seconds ago is proving it is alive", between, .proving)
    expect("and is NOT reclaimable merely because it is between compiles",
           OrchestratorLease.takeover(liveness: between, compilers: .none), .holderProving)

    // The deadline is a clock on the proof, not on the work.
    expect("a holder that renewed once four hours ago has stopped proving it",
           OrchestratorLease.liveness(renewedAt: leaseNow.addingTimeInterval(-20),
                                      owner: .live, doneFlag: .absent,
                                      now: leaseNow.addingTimeInterval(14_400)),
           .stopped("heartbeat_lapsed"))
    expect("while a four-hour compile that keeps renewing is alive, because the clock is on "
           + "the proof of life and never on the work",
           OrchestratorLease.liveness(renewedAt: leaseNow.addingTimeInterval(14_380),
                                      owner: .live, doneFlag: .absent,
                                      now: leaseNow.addingTimeInterval(14_400)),
           .proving)
    expect("exactly at the deadline is still proving",
           OrchestratorLease.liveness(
                renewedAt: leaseNow.addingTimeInterval(-OrchestratorLease.renewalDeadline),
                owner: .live, doneFlag: .absent, now: leaseNow),
           .proving)

    // The broker's own axis: what a file lock cannot know.
    expect("a terminal task stops proving at once, without waiting out a deadline",
           OrchestratorLease.liveness(renewedAt: leaseNow, owner: .gone("task timed out"),
                                      doneFlag: .absent, now: leaseNow),
           .stopped("owner_gone"))
    expect("an owner nobody can resolve neither accelerates nor blocks",
           OrchestratorLease.liveness(renewedAt: leaseNow, owner: .unknown, doneFlag: .absent,
                                      now: leaseNow),
           .proving)

    // done_flag is a positive signal only.
    expect("a done flag ends the work at once",
           OrchestratorLease.liveness(renewedAt: leaseNow, owner: .live, doneFlag: .present,
                                      now: leaseNow),
           .stopped("work_finished"))
    expect("its absence proves nothing, because a SIGKILLed run never writes one",
           OrchestratorLease.liveness(renewedAt: leaseNow, owner: .live, doneFlag: .absent,
                                      now: leaseNow),
           .proving)
    expect("and an unreadable flag proves nothing either",
           OrchestratorLease.liveness(renewedAt: leaseNow, owner: .live, doneFlag: .unknown,
                                      now: leaseNow),
           .proving)
}

group("the physical backstop is never waived, and neither half admits anybody alone") {
    let stopped = OrchestratorLease.Liveness.stopped("heartbeat_lapsed")
    let proving = OrchestratorLease.Liveness.proving

    expect("(A) alone does not admit: a lapsed holder whose compile still burns keeps the lock",
           OrchestratorLease.takeover(liveness: stopped, compilers: .present([9_001, 9_002])),
           .compilersRunning([9_001, 9_002]))
    expect("(B) alone does not admit either: no compiler running is not staleness",
           OrchestratorLease.takeover(liveness: proving, compilers: .none), .holderProving)
    expect("both halves admit", OrchestratorLease.takeover(liveness: stopped, compilers: .none),
           .eligible)
    expect("a compiler scan that failed blocks rather than reading as an empty machine",
           OrchestratorLease.takeover(liveness: stopped, compilers: .unknown("ps timed out")),
           .evidenceUnknown("ps timed out"))
    expect("and it blocks even when the holder is plainly proving",
           OrchestratorLease.takeover(liveness: proving, compilers: .unknown("ps timed out")),
           .evidenceUnknown("ps timed out"))

    // ROOT'S SECOND REQUIRED RED, in full. A holder whose heartbeat lapsed while a compiler is
    // still running keeps the lock, and the refusal names the orphan pids. A build with (B)
    // removed lets this through and starts a second driver beside a compile that is still
    // burning 46 GB — which is the 01:24 and 01:45 reboots.
    let lapsed = OrchestratorLease.liveness(
        renewedAt: leaseNow.addingTimeInterval(-3_600), owner: .unknown, doneFlag: .absent,
        beat: leaseNow.addingTimeInterval(-3_600), now: leaseNow)
    expect("a heartbeat an hour old has stopped proving", lapsed, .stopped("heartbeat_lapsed"))
    let refused = OrchestratorLease.takeover(liveness: lapsed, compilers: .present([9_101, 9_102]))
    expect("and it is still not reclaimable, because a compiler is running",
           refused, .compilersRunning([9_101, 9_102]))
    if case .compilersRunning(let orphans) = refused {
        expect("the refusal names the orphans, for a person, never as a target list",
               orphans, [9_101, 9_102])
    } else {
        check("the refusal names the orphans", false)
    }
    expect("with the code a waiter branches on", refused.code, "compiler_running")

    // ROOT'S THIRD REQUIRED RED. `pgrep -f` matches arguments, so a sampler or a
    // `/usr/bin/time swift-frontend …` wrapper reads as a running compiler and the guard answers
    // the opposite of the truth. This scan matches the executable's own basename and nothing
    // else, so every decoy below is correctly not a compiler.
    let decoys = """
      601 /usr/bin/time
      602 /Users/x/bin/swift-frontend-sampler
      603 /bin/sh
      604 swift-frontendish
      605 /Users/x/swift-frontend.old
    """
    expect("a process merely mentioning swift-frontend is not a compiler",
           OrchestratorLease.parseCompilerScan(decoys, status: 0, timedOut: false), .none)

    // And against the real `ps` on this machine, with a real decoy this test starts: a process
    // whose *arguments* contain the word and whose executable does not.
    let decoy = Process()
    decoy.executableURL = URL(fileURLWithPath: "/bin/sh")
    decoy.arguments = ["-c", "sleep 2", "swift-frontend"]
    try? decoy.run()
    let scanned = OrchestratorLease.liveCompilers()
    if case .present(let pids) = scanned {
        check("the real scan does not count a decoy naming swift-frontend in its arguments",
              !pids.contains(decoy.processIdentifier), "\(pids)")
    } else {
        check("the real scan does not count a decoy naming swift-frontend in its arguments", true)
    }
    decoy.waitUntilExit()

    // The refusal has to name the orphans, because nothing here may end them.
    if case .compilersRunning(let pids) = OrchestratorLease.takeover(
        liveness: .stopped("owner_gone"), compilers: .present([7, 3])) {
        expect("an orphaned compile after its owner died is named, not killed", pids, [7, 3])
    } else {
        check("an orphaned compile after its owner died is named, not killed", false)
    }
    expect("the hold reason a waiter reads is the typed code",
           OrchestratorLease.takeover(liveness: stopped, compilers: .present([7])).code,
           "compiler_running")
    expect("and an unknown is its own code, never folded into the others",
           OrchestratorLease.takeover(liveness: stopped, compilers: .unknown("x")).code,
           "evidence_unknown")

    let scan = """
      501 /Applications/Xcode.app/Contents/.../swift-frontend
      777 /usr/bin/ssh
      502 swift-frontend
      503 /usr/local/bin/swift-frontend-wrapper
    """
    expect("the scan matches the basename exactly and nothing that merely starts with it",
           OrchestratorLease.parseCompilerScan(scan, status: 0, timedOut: false),
           .present([501, 502]))
    expect("an empty machine is `none`",
           OrchestratorLease.parseCompilerScan("  1 launchd\n", status: 0, timedOut: false),
           .none)
    check("a non-zero scan is unknown rather than empty",
          OrchestratorLease.parseCompilerScan("", status: 2, timedOut: false) != .none)
    check("and a timed-out scan is unknown too",
          OrchestratorLease.parseCompilerScan("", status: nil, timedOut: true) != .none)
}

group("a recycled pid reads as gone, and an unpinned locale reads as unknown rather than dead") {
    let recorded = Date(timeIntervalSince1970: 1_788_365_342)
    expect("the same process reads as alive",
           OrchestratorLease.identity(recordedStart: recorded, observed: .running(recorded)),
           .alive)
    expect("a one-second rounding is the same process",
           OrchestratorLease.identity(recordedStart: recorded,
                                      observed: .running(recorded.addingTimeInterval(1))),
           .alive)
    expect("a pid now in use by something that started later is gone",
           OrchestratorLease.identity(recordedStart: recorded,
                                      observed: .running(recorded.addingTimeInterval(4_000))),
           .gone)
    expect("a pid nothing answers for is gone",
           OrchestratorLease.identity(recordedStart: recorded, observed: .absent), .gone)
    expect("a recorded start nobody wrote is unknown, never gone",
           OrchestratorLease.identity(recordedStart: nil, observed: .running(recorded)),
           .unknown)
    expect("and a probe that failed is unknown",
           OrchestratorLease.identity(recordedStart: recorded, observed: .unknown("no answer")),
           .unknown)

    // Measured on this Mac at 02:13 on 2026-09-03, holding the formatter still and varying only
    // the day: `LC_ALL=C` prints five whitespace tokens on every day, `zh_TW.UTF-8` prints five
    // on days 1-9 and four on days 10-31. Two earlier readings of "how many fields" disagreed
    // for exactly that reason and both were right, which is why nothing here counts fields.
    let pinned = "Thu Sep  3 02:09:02 2026"
    let localisedFiveToken = "四  9/ 3 02:09:02 2026"
    let localisedFourToken = "一  8/31 02:09:02 2026"
    guard case .running(let readBack) = OrchestratorLease.parseProcessStart(
        pinned, status: 0, timedOut: false) else {
        check("the LC_ALL=C shape parses", false); return
    }
    check("the LC_ALL=C shape parses to a real instant", readBack.timeIntervalSince1970 > 0)
    expect("a five-token localised shape is unknown, which blocks — it is never `gone`",
           OrchestratorLease.parseProcessStart(localisedFiveToken, status: 0, timedOut: false),
           .unknown("the process probe printed a start this build cannot read; "
                    + "is LC_ALL=C pinned on the probe?"))
    expect("and so is the four-token one that the same locale prints after the 9th",
           OrchestratorLease.parseProcessStart(localisedFourToken, status: 0, timedOut: false),
           .unknown("the process probe printed a start this build cannot read; "
                    + "is LC_ALL=C pinned on the probe?"))
    expect("an exit 1 with no output is the one shape that means gone",
           OrchestratorLease.parseProcessStart("", status: 1, timedOut: false), .absent)
    check("any other silence is unknown",
          OrchestratorLease.parseProcessStart("", status: 3, timedOut: false) != .absent)

    // Against the real `ps` on this machine rather than only against a fixture. A hand-written
    // English fixture is exactly how a zh_TW Mac once lost every session's identity.
    let selfPID = ProcessInfo.processInfo.processIdentifier
    let observed = OrchestratorLease.liveProcessStart(selfPID)
    guard case .running(let mine) = observed else {
        check("the real ps on this machine reads this process's start", false, "\(observed)")
        return
    }
    check("the real ps on this machine reads this process's start", true)
    check("and it is a plausible instant, not a formatter artefact",
          abs(mine.timeIntervalSinceNow) < 86_400, "\(mine)")
    expect("comparing that reading against itself is `alive` on this machine's locale",
           OrchestratorLease.identity(recordedStart: mine, observed: observed), .alive)
    // A pid this test started and reaped itself, so "gone" is a fact rather than a hope.
    let reaped = Process()
    reaped.executableURL = URL(fileURLWithPath: "/usr/bin/true")
    try? reaped.run()
    let deadPID = reaped.processIdentifier
    reaped.waitUntilExit()
    expect("and a process this test started and reaped reads as gone",
           OrchestratorLease.liveProcessStart(deadPID), .absent)
}

group("the queue is FIFO, observable, and joining it does not jump it") {
    let free = OrchestratorLease.Record(resource: OrchestratorLease.heavyCompile,
                                        directory: leaseDirectory)
    let evidence = OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent)
    let first = OrchestratorLease.acquire(
        record: free, request: leaseRequest("req-1", label: "first", session: "s1"),
        directory: .absent, evidence: evidence, now: leaseNow)
    guard let firstBudget = leaseGrant(first.outcome) else {
        check("the first acquirer is granted", false, "\(first.outcome)"); return
    }
    check("the first acquirer is granted", true)
    expect("and the effect is one mkdir, which is the whole exclusion", first.effect,
           .createDirectory(OrchestratorLease.holderFile(
                for: leaseRequest("req-1", label: "first", session: "s1"), now: leaseNow)))
    expect("with a budget the holder must honour", firstBudget.parallelism, 1)

    let granted = first.record
    let second = OrchestratorLease.acquire(
        record: granted, request: leaseRequest("req-2", label: "second", session: "s2"),
        directory: heldDirectory(granted.holder!), evidence: evidence, now: leaseNow)
    guard let queued = leaseQueued(second.outcome) else {
        check("a second acquirer is queued rather than granted", false, "\(second.outcome)")
        return
    }
    check("a second acquirer is queued rather than granted", true)
    expect("at position one", queued.position, 1)
    expect("and it is told why, in a word it can branch on", queued.holdReason,
           "holder_proving")
    expect("queueing performs no filesystem work at all", second.effect, .none)

    let third = OrchestratorLease.acquire(
        record: second.record, request: leaseRequest("req-3", label: "third", session: "s3"),
        directory: heldDirectory(granted.holder!), evidence: evidence, now: leaseNow)
    expect("a third joins behind the second", leaseQueued(third.outcome)?.position, 2)

    // Polling is the client contract, so polling must not duplicate or lose a place.
    let polled = OrchestratorLease.acquire(
        record: third.record, request: leaseRequest("req-2", label: "second", session: "s2"),
        directory: heldDirectory(granted.holder!), evidence: evidence, now: leaseNow)
    expect("polling with the same request id keeps its place", leaseQueued(polled.outcome)?.position, 1)
    expect("and does not grow the queue", polled.record.queue.count, 2)

    // FIFO: the holder goes, and only the head may take it.
    let lapsedHolder = leaseHolder("req-1", label: "first",
                                   renewedAt: leaseNow.addingTimeInterval(-600), session: "s1",
                                   phase: .compiling, phaseSince: leaseNow,
                                   lastCompilingAt: leaseNow)
    let stale = heldRecord(lapsedHolder, queue: polled.record.queue)
    let jumper = OrchestratorLease.acquire(
        record: stale, request: leaseRequest("req-3", label: "third", session: "s3"),
        directory: .absent, evidence: evidence, now: leaseNow)
    expect("the second in line does not get it before the first",
           leaseQueued(jumper.outcome)?.holdReason, "queued_behind_others")
    let head = OrchestratorLease.acquire(
        record: stale, request: leaseRequest("req-2", label: "second", session: "s2"),
        directory: .absent, evidence: evidence, now: leaseNow)
    check("the head of the queue does", leaseGrant(head.outcome) != nil, "\(head.outcome)")
    expect("and it leaves the queue when it is granted", head.record.queue.count, 1)

    var crowded = free
    crowded.queue = (0..<OrchestratorLease.queueDepthLimit).map {
        OrchestratorLease.Waiter(requestID: "w\($0)", owner: leaseOwner("w\($0)"), pid: 1,
                                 processStart: leaseNow, requestedAt: leaseNow, reason: "r")
    }
    let overflow = OrchestratorLease.acquire(
        record: crowded, request: leaseRequest("req-x", label: "late", session: "sx"),
        directory: heldDirectory(lapsedHolder),
        evidence: OrchestratorLease.Evidence(compilers: .present([1]), owner: .live,
                                             doneFlag: .absent),
        now: leaseNow)
    expect("a full queue refuses rather than growing a list nobody reads",
           leaseRefusal(overflow.outcome)?.code, "queue_full")

    let wire = OrchestratorLease.record(third.record, now: leaseNow.addingTimeInterval(30))
    expect("the queue is observable: who, in what order, since when",
           (wire["queue"] as? [[String: Any]])?.count, 2)
    expect("with an age a person can read",
           ((wire["queue"] as? [[String: Any]])?.first?["waitedSeconds"] as? Int), 30)
    expect("and a hold reason beside it", wire["holdReason"] as? String, "holder_proving")
}

group("release and cancel belong to the sessions that own them") {
    let holder = leaseHolder("lease-1", label: "first", renewedAt: leaseNow, session: "s1")
    let waiter = OrchestratorLease.Waiter(requestID: "req-2", owner: leaseOwner("second",
                                                                               session: "s2"),
                                          pid: 9, processStart: leaseNow, requestedAt: leaseNow,
                                          reason: "waiting")
    let record = heldRecord(holder, queue: [waiter])
    let evidence = OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent)

    let stranger = OrchestratorLease.release(
        record: record, leaseID: "lease-1", owner: leaseOwner("second", session: "s2"),
        directory: heldDirectory(holder), evidence: evidence, now: leaseNow)
    expect("release by a non-owner is refused", leaseRefusal(stranger.outcome)?.code,
           "not_holder")
    expect("and it touches nothing on disk", stranger.effect, .none)
    let wrongID = OrchestratorLease.release(
        record: record, leaseID: "lease-other", owner: leaseOwner("first", session: "s1"),
        directory: heldDirectory(holder), evidence: evidence, now: leaseNow)
    expect("release naming another lease is refused too",
           leaseRefusal(wrongID.outcome)?.code, "not_holder")

    let owner = OrchestratorLease.release(
        record: record, leaseID: "lease-1", owner: leaseOwner("first", session: "s1"),
        directory: heldDirectory(holder), evidence: evidence, now: leaseNow)
    if case .released = owner.outcome {
        check("the holder may release", true)
    } else {
        check("the holder may release", false, "\(owner.outcome)")
    }
    expect("and the removal names the holder it expects to find, so it cannot remove another's",
           owner.effect, .removeDirectory(expecting: OrchestratorLease.file(from: holder)))
    check("the queue is untouched by a release", owner.record.queue.count == 1)
    check("and the record no longer has a holder", owner.record.holder == nil)

    let repeated = OrchestratorLease.release(
        record: owner.record, leaseID: "lease-1", owner: leaseOwner("first", session: "s1"),
        directory: .absent, evidence: evidence, now: leaseNow)
    if case .released = repeated.outcome {
        check("releasing twice is the shape a retry takes and is not an error", true)
    } else {
        check("releasing twice is the shape a retry takes and is not an error", false)
    }

    let notMine = OrchestratorLease.cancel(record: record, requestID: "req-2",
                                           owner: leaseOwner("third", session: "s3"))
    expect("only the session that made a request may cancel it",
           leaseRefusal(notMine.outcome)?.code, "not_requester")
    let cancelled = OrchestratorLease.cancel(record: record, requestID: "req-2",
                                             owner: leaseOwner("second", session: "s2"))
    if case .cancelled = cancelled.outcome {
        check("a waiter may remove itself", true)
    } else {
        check("a waiter may remove itself", false, "\(cancelled.outcome)")
    }
    expect("and only itself", cancelled.record.queue.count, 0)
    check("the holder is unaffected", cancelled.record.holder != nil)
    expect("cancelling something that is not queued is a typed 404",
           leaseRefusal(OrchestratorLease.cancel(record: record, requestID: "nope",
                                                 owner: leaseOwner("second",
                                                                   session: "s2")).outcome)?.code,
           "not_queued")

    // Renewal is the proof of life, and it refreshes what is actually working.
    let renewed = OrchestratorLease.renew(
        record: record, leaseID: "lease-1", owner: leaseOwner("first", session: "s1"),
        workPIDs: [5_101, 5_102], directory: heldDirectory(holder), evidence: evidence,
        now: leaseNow.addingTimeInterval(20))
    if case .renewed = renewed.outcome {
        check("the holder may renew", true)
    } else {
        check("the holder may renew", false, "\(renewed.outcome)")
    }
    expect("the clock moves", renewed.record.holder?.renewedAt, leaseNow.addingTimeInterval(20))
    expect("the record carries what is working right now", renewed.record.holder?.workPIDs,
           [5_101, 5_102])
    if case .refreshHolderFile(let file) = renewed.effect {
        expect("and holder.txt is refreshed with it, for a broker that has not started yet",
               file.work, [5_101, 5_102])
    } else {
        check("and holder.txt is refreshed with it", false, "\(renewed.effect)")
    }
    expect("renewing something nobody holds says so rather than granting it",
           leaseRefusal(OrchestratorLease.renew(
                record: OrchestratorLease.Record(resource: OrchestratorLease.heavyCompile),
                leaseID: "lease-1", owner: leaseOwner("first", session: "s1"), workPIDs: [],
                directory: .absent, evidence: evidence, now: leaseNow).outcome)?.code,
           "lease_lost")
}

group("a restart reconciles from the directory it never stopped owning") {
    let evidence = OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent)
    let empty = OrchestratorLease.Record(resource: OrchestratorLease.heavyCompile,
                                         directory: leaseDirectory)

    // The app restarted with the directory still held by a script it never registered.
    let script = OrchestratorLease.HolderFile(
        holder: "test.sh on tty003", pid: 51, started: leaseNow.addingTimeInterval(-100),
        tree: "/tmp/tree", log: "/tmp/x.log", note: nil, work: [900], done: nil,
        renewed: leaseNow.addingTimeInterval(-10), phase: .compiling,
        heartbeat: "/tmp/clawdline-lease-tests.lock/beat")
    let adopted = OrchestratorLease.reconcile(record: empty, directory: .held(script),
                                              evidence: evidence, now: leaseNow)
    expect("a directory the broker did not write is adopted", adopted.reconciliation, .adopted)
    expect("with the script's identity", adopted.holder?.pid, 51)
    expect("its provenance says the broker did not grant it",
           adopted.holder?.provenance, .directory)
    expect("and its renewal comes from the file, which is the shell's proof of life",
           adopted.holder?.renewedAt, leaseNow.addingTimeInterval(-10))

    // And the same reading twice is the same lease, not two.
    let again = OrchestratorLease.reconcile(record: adopted, directory: .held(script),
                                            evidence: evidence, now: leaseNow)
    expect("reading it again is the same lease", again.holder?.leaseID, adopted.holder?.leaseID)
    expect("and now it matches", again.reconciliation, .matched)

    // A second acquirer cannot be granted on top of an adopted holder.
    let intruder = OrchestratorLease.acquire(
        record: adopted, request: leaseRequest("req-9", label: "another", session: "s9"),
        directory: .held(script), evidence: evidence, now: leaseNow)
    check("a restart cannot grant the same directory twice",
          leaseGrant(intruder.outcome) == nil, "\(intruder.outcome)")

    // The record and the directory disagree. The directory wins, visibly.
    let stale = heldRecord(leaseHolder("lease-old", label: "old", pid: 4_242,
                                       renewedAt: leaseNow))
    let replaced = OrchestratorLease.reconcile(record: stale, directory: .held(script),
                                               evidence: evidence, now: leaseNow)
    expect("when they disagree the directory wins", replaced.holder?.pid, 51)
    expect("and the disagreement is recorded rather than smoothed over",
           replaced.reconciliation, .replaced)

    // A durable grant whose directory is gone is not a free slot.
    let orphanRecord = heldRecord(leaseHolder("lease-1", label: "holder", renewedAt: leaseNow))
    let missing = OrchestratorLease.reconcile(
        record: orphanRecord, directory: .absent,
        evidence: OrchestratorLease.Evidence(compilers: .present([7]), owner: .live,
                                             doneFlag: .absent),
        now: leaseNow)
    expect("a grant whose directory vanished while a compiler runs keeps its holder",
           missing.holder?.leaseID, "lease-1")
    expect("and says so", missing.reconciliation, .directoryMissing)
    expect("with the backstop as the reason", missing.holdReason, "compiler_running")
    let cleared = OrchestratorLease.reconcile(
        record: orphanRecord, directory: .absent,
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .gone("task cancelled"),
                                             doneFlag: .absent),
        now: leaseNow)
    check("it clears only on both halves", cleared.holder == nil)
    expect("and then the slot is idle", cleared.reconciliation, .idle)

    // The beat file is the shell holder's proof of life, and it is the *only* one it has: a
    // script that took the directory with `mkdir` has no broker record for a restarted broker to
    // read. So the record's own clock and the beat are both consulted, and the later one wins —
    // otherwise a lock taken before the app started reads as an hour stale the moment it is
    // adopted, and gets handed to a second compiler.
    let adoptedHolder = adopted.holder!
    expect("an adopted holder whose beat is five seconds old is proving it is alive",
           OrchestratorLease.liveness(renewedAt: leaseNow.addingTimeInterval(-3_600),
                                      owner: .unknown, doneFlag: .absent,
                                      beat: leaseNow.addingTimeInterval(-5), now: leaseNow),
           .proving)
    expect("and the same holder with a ten-minute-old beat has stopped",
           OrchestratorLease.liveness(renewedAt: leaseNow.addingTimeInterval(-3_600),
                                      owner: .unknown, doneFlag: .absent,
                                      beat: leaseNow.addingTimeInterval(-600), now: leaseNow),
           .stopped("heartbeat_lapsed"))
    expect("a beat older than the record does not age the record",
           OrchestratorLease.liveness(renewedAt: leaseNow, owner: .unknown, doneFlag: .absent,
                                      beat: leaseNow.addingTimeInterval(-3_600), now: leaseNow),
           .proving)
    expect("no beat at all falls back to the record's own clock",
           OrchestratorLease.liveness(renewedAt: leaseNow.addingTimeInterval(-3_600),
                                      owner: .unknown, doneFlag: .absent, beat: nil,
                                      now: leaseNow),
           .stopped("heartbeat_lapsed"))
    expect("the adopted holder names the beat file it touches, so nobody has to assume it",
           adoptedHolder.heartbeatPath, "/tmp/clawdline-lease-tests.lock/beat")
    let beatKept = OrchestratorLease.reconcile(
        record: heldRecord(adoptedHolder), directory: .absent,
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .unknown,
                                             doneFlag: .absent,
                                             beat: leaseNow.addingTimeInterval(-5)),
        now: leaseNow)
    check("and a live beat keeps a lease whose directory went missing",
          beatKept.holder != nil)
    expect("with the heartbeat named as the reason it is still held",
           beatKept.livenessReason, "proving")

        // An unreadable directory closes admission and is never taken over.
    let unreadable = OrchestratorLease.acquire(
        record: empty, request: leaseRequest("req-u", label: "hopeful", session: "su"),
        directory: .unreadable("holder.txt is missing"), evidence: evidence, now: leaseNow)
    expect("an unreadable lock queues rather than granting",
           leaseQueued(unreadable.outcome)?.holdReason, "directory_unreadable")
    check("and nothing is written", unreadable.effect == .none)
}

group("admission degrades to one compiler rather than refusing, and the gate has a named door") {
    let plenty = OrchestratorLease.Pressure(
        physicalMB: 24_576, freeMB: 2_090, anonymousMB: 11_038, fileBackedMB: 5_642,
        compressorMB: 1_270, swapUsedMB: 10_870, swapFreeMB: 1_417, swapTotalMB: 12_288,
        observedAt: leaseNow)
    expect("headroom is free plus file-backed, and never elastic swap",
           plenty.headroomMB, 2_090 + 5_642)

    // Until the measurement task lands a per-compile peak, every grant is the floor. That is
    // conservative *and* deadlock-free: this build cannot refuse on a number nobody has taken.
    guard case .granted(let unmeasured) = OrchestratorLease.admit(
        pressure: plenty, policy: OrchestratorLease.Policy(), topAnonymous: [],
        now: leaseNow) else {
        check("an unmeasured peak still admits", false); return
    }
    expect("an unmeasured peak grants the floor of one", unmeasured.parallelism, 1)
    expect("and says why", unmeasured.basis, "peak_not_measured")
    expect("no reading at all is also a grant, at the floor",
           leaseGrantParallelism(OrchestratorLease.admit(pressure: nil,
                                                         policy: OrchestratorLease.Policy(),
                                                         topAnonymous: [], now: leaseNow)),
           1)

    let measured = OrchestratorLease.Policy(perCompileMB: 3_000, maximumParallelism: 8)
    expect("with a measured peak the ceiling is arithmetic on headroom",
           leaseGrantParallelism(OrchestratorLease.admit(pressure: plenty, policy: measured,
                                                         topAnonymous: [], now: leaseNow)),
           2)
    var tight = plenty
    tight.freeMB = 100
    tight.fileBackedMB = 100
    expect("low headroom degrades to one rather than waiting for a slot that never comes",
           leaseGrantParallelism(OrchestratorLease.admit(pressure: tight, policy: measured,
                                                         topAnonymous: [], now: leaseNow)),
           1)
    var huge = plenty
    huge.freeMB = 200_000
    expect("and the ceiling is capped however much headroom there is",
           leaseGrantParallelism(OrchestratorLease.admit(pressure: huge, policy: measured,
                                                         topAnonymous: [], now: leaseNow)),
           8)

    // Refusal exists, is measured, and is actionable.
    let floored = OrchestratorLease.Policy(perCompileMB: 3_000, floorRequirementMB: 4_000)
    guard case .refused(let deficit) = OrchestratorLease.admit(
        pressure: tight, policy: floored,
        topAnonymous: [OrchestratorLease.MemoryHolder(pid: 903, command: "Chrome",
                                                      anonymousMB: 3_100)],
        now: leaseNow) else {
        check("a measured floor that cannot be met refuses", false); return
    }
    check("a measured floor that cannot be met refuses", true)
    expect("naming how much", deficit.needMB, 4_000)
    expect("of what it actually has", deficit.haveMB, 200)
    expect("of which measured quantity", deficit.quantity, "headroom_mb (free + file-backed)")
    check("taken how", !deficit.method.isEmpty)
    let refusal = OrchestratorLease.refusal(for: deficit)
    expect("with a typed code", refusal.code, "pressure_refused")
    check("naming the largest anonymous-memory holders for a person to look at",
          refusal.message.contains("Chrome") && refusal.message.contains("pid 903"))
    check("and saying plainly that it will not end any of them",
          refusal.message.contains("Nothing here will end any of them"))

    // A gate with no door is the deadlock this feature exists to remove.
    let overridden = OrchestratorLease.acquire(
        record: OrchestratorLease.Record(resource: OrchestratorLease.heavyCompile,
                                         directory: leaseDirectory),
        request: leaseRequest("req-o", label: "root", session: "s1", override: "root af1b83ba"),
        directory: .absent,
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent,
                                             pressure: tight),
        policy: floored,
        topAnonymous: [], now: leaseNow)
    guard let budget = leaseGrant(overridden.outcome) else {
        check("a named override proceeds at the floor", false, "\(overridden.outcome)"); return
    }
    check("a named override proceeds at the floor", true)
    expect("still at one", budget.parallelism, 1)
    check("and the record says who decided", budget.basis.contains("root af1b83ba"))

    // The override is admission only. It can never take a lease somebody else holds.
    let held = heldRecord(leaseHolder("lease-1", label: "first", renewedAt: leaseNow,
                                      session: "s1"))
    let forced = OrchestratorLease.acquire(
        record: held,
        request: leaseRequest("req-f", label: "impatient", session: "s2",
                              override: "root af1b83ba"),
        directory: heldDirectory(held.holder!),
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent,
                                             pressure: plenty),
        policy: floored, topAnonymous: [], now: leaseNow)
    expect("an override never crosses exclusion", leaseQueued(forced.outcome)?.holdReason,
           "holder_proving")

    // The pressure reader itself, against the shapes this Mac prints.
    let vmStat = """
    Mach Virtual Memory Statistics: (page size of 16384 bytes)
    Pages free:                              133766.
    Anonymous pages:                         706432.
    File-backed pages:                       352128.
    Pages occupied by compressor:             81264.
    """
    guard let pressure = OrchestratorLease.parsePressure(
        vmStat: vmStat, swapusage: "total = 12288.00M  used = 10870.75M  free = 1417.25M",
        memsize: "25769803776", now: leaseNow) else {
        check("vm_stat and vm.swapusage parse", false); return
    }
    check("vm_stat and vm.swapusage parse", true)
    expect("the page size comes from the header, not from an assumption",
           pressure.freeMB, 133_766 * 16_384 / 1_048_576)
    expect("swap used is read", pressure.swapUsedMB, 10_870)
    expect("swap free is read but is not the budget", pressure.swapFreeMB, 1_417)
    expect("and physical memory comes from hw.memsize", pressure.physicalMB, 24_576)
}

group("a holder that is not compiling is reported, and is still not reclaimable") {
    // 02:45 on 2026-09-03: the lock had been held 36 minutes, holder.txt was last written at
    // 02:20, there were zero swift-frontend processes on the machine, no done_flag had appeared,
    // and the sentinel pid was alive. From outside, "between compiles" and "finished and forgot
    // to let go" were the same picture, and another line had been waiting five minutes with no
    // safe way to tell them apart. `phase` is that difference, and it is a sentence rather than
    // a verdict: it may say, it may not decide.
    let writing = leaseHolder("lease-1", label: "child a77d9908", renewedAt: leaseNow,
                              session: "s1", phase: .idleHolding,
                              phaseSince: leaseNow.addingTimeInterval(-2_160),
                              lastCompilingAt: leaseNow.addingTimeInterval(-2_160))
    let record = heldRecord(writing)
    let idle = OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent)

    // (1) It is renewing, so it is alive, however long it has been writing its report.
    expect("a holder renewing while it writes its report is proving it is alive",
           OrchestratorLease.liveness(renewedAt: writing.renewedAt, owner: .live,
                                      doneFlag: .absent, now: leaseNow),
           .proving)
    expect("and is NOT reclaimable, with zero compilers on the machine and 36 minutes of it",
           OrchestratorLease.takeover(
                liveness: OrchestratorLease.liveness(renewedAt: writing.renewedAt, owner: .live,
                                                     doneFlag: .absent, now: leaseNow),
                compilers: .none),
           .holderProving)
    let waiter = OrchestratorLease.acquire(
        record: record, request: leaseRequest("req-2", label: "another", session: "s2"),
        directory: heldDirectory(writing), evidence: idle, now: leaseNow)
    expect("so a second acquirer is queued, not granted",
           leaseQueued(waiter.outcome)?.holdReason, "holder_proving")

    // (2) And the query has to say what that picture is, or it is the same blank as tonight's.
    let wire = OrchestratorLease.record(record, now: leaseNow)
    guard let row = wire["holder"] as? [String: Any] else {
        check("the query answers with a holder", false); return
    }
    expect("the query says who", row["holder"] as? String, "child a77d9908")
    expect("what it is doing", row["phase"] as? String, "idle-holding")
    expect("for how long", row["phaseAgeSeconds"] as? Int, 2_160)
    expect("how long since it last compiled", row["notCompilingForSeconds"] as? Int, 2_160)
    expect("and how fresh its proof of life is", row["renewalAgeSeconds"] as? Int, 0)
    expect("named as a heartbeat, which is the word the file uses",
           row["heartbeatAgeSeconds"] as? Int, 0)
    guard let attention = row["attention"] as? String else {
        check("a holder 36 minutes outside `compiling` is called out", false); return
    }
    check("a holder 36 minutes outside `compiling` is called out", true)
    check("naming who to ask", attention.contains("child a77d9908"))
    check("and saying plainly that nothing will reclaim it for that",
          attention.contains("nothing here will reclaim the lock for that reason"))

    // A compiling holder is not called out at all — the sentence exists for the other picture.
    let busy = OrchestratorLease.record(
        heldRecord(leaseHolder("lease-2", label: "busy", renewedAt: leaseNow)), now: leaseNow)
    let busyRow = busy["holder"] as? [String: Any]
    expect("a compiling holder is simply compiling", busyRow?["phase"] as? String, "compiling")
    check("with nothing to call out", busyRow?["attention"] == nil)
    check("and no not-compiling clock at all", busyRow?["notCompilingForSeconds"] == nil)

    // The phase never reaches the takeover rule, whatever it says.
    for phase in OrchestratorLease.Phase.allCases {
        let holder = leaseHolder("lease-3", label: "whatever", renewedAt: leaseNow,
                                 phase: phase, phaseSince: leaseNow.addingTimeInterval(-9_999),
                                 lastCompilingAt: nil)
        expect("phase \(phase.rawValue) does not make a renewing holder reclaimable",
               OrchestratorLease.takeover(
                    liveness: OrchestratorLease.liveness(renewedAt: holder.renewedAt,
                                                         owner: .live, doneFlag: .absent,
                                                         now: leaseNow),
                    compilers: .none),
               .holderProving)
    }

    // Renewal is what carries it, and `phaseSince` moves only when the phase itself changes.
    let renewed = OrchestratorLease.renew(
        record: record, leaseID: "lease-1", owner: leaseOwner("child a77d9908", session: "s1"),
        workPIDs: [], phase: .compiling, directory: heldDirectory(writing), evidence: idle,
        now: leaseNow.addingTimeInterval(20))
    expect("a renewal that changes the phase records it",
           renewed.record.holder?.phase, .compiling)
    expect("and restarts its clock", renewed.record.holder?.phaseSince,
           leaseNow.addingTimeInterval(20))
    let again = OrchestratorLease.renew(
        record: renewed.record, leaseID: "lease-1",
        owner: leaseOwner("child a77d9908", session: "s1"), workPIDs: [], phase: .compiling,
        directory: heldDirectory(writing), evidence: idle,
        now: leaseNow.addingTimeInterval(40))
    expect("a renewal that does not change it leaves the clock alone",
           again.record.holder?.phaseSince, leaseNow.addingTimeInterval(20))
    let silent = OrchestratorLease.renew(
        record: renewed.record, leaseID: "lease-1",
        owner: leaseOwner("child a77d9908", session: "s1"), workPIDs: [],
        directory: heldDirectory(writing), evidence: idle, now: leaseNow.addingTimeInterval(60))
    expect("and a renewal that says nothing leaves the phase alone rather than resetting it",
           silent.record.holder?.phase, .compiling)
}

group("the lease record survives a store round trip") {
    var holder = leaseHolder("lease-1", label: "root af1b83ba", pid: 72_929,
                             renewedAt: leaseNow.addingTimeInterval(30), session: "s1",
                             work: [8_101, 8_102])
    holder.doneFlagPath = "/tmp/clawdline-suite.lock/done"
    holder.budget = OrchestratorLease.Budget(parallelism: 2, basis: "headroom_mb/3000mb_per_compile",
                                             headroomMB: 7_732, swapFreeMB: 1_417)
    holder.phase = .idleHolding
    holder.phaseSince = leaseNow.addingTimeInterval(-2_160)
    holder.lastCompilingAt = leaseNow.addingTimeInterval(-2_160)
    holder.owner.taskID = "23e78454"
    holder.owner.rootSessionID = "af1b83ba"
    let waiter = OrchestratorLease.Waiter(requestID: "req-2",
                                          owner: leaseOwner("second", session: "s2"), pid: 9,
                                          processStart: leaseNow, requestedAt: leaseNow,
                                          reason: "queued for the suite")
    let record = OrchestratorLease.Record(
        resource: OrchestratorLease.heavyCompile, directory: leaseDirectory, holder: holder,
        queue: [waiter], reconciliation: .matched, reconciledAt: leaseNow,
        holdReason: nil, livenessReason: "proving")
    let stored = OrchestratorStore.stored(record)
    guard let back = OrchestratorStore.lease(from: stored) else {
        check("a stored lease reads back", false); return
    }
    expect("every field survives", back, record)

    let minimal = OrchestratorLease.Record(resource: OrchestratorLease.heavyCompile)
    guard let minimalBack = OrchestratorStore.lease(from: OrchestratorStore.stored(minimal))
    else {
        check("an idle lease reads back too", false); return
    }
    expect("an idle lease reads back too", minimalBack, minimal)
    check("a row with no resource is dropped rather than resurrected with a guess",
          OrchestratorStore.lease(from: ["directory": leaseDirectory]) == nil)
    check("and a row naming a resource this build does not lease is dropped",
          OrchestratorStore.lease(from: ["resource": "gpu", "directory": leaseDirectory]) == nil)

    let wire = OrchestratorLease.record(record, now: leaseNow.addingTimeInterval(60))
    let holderRow = wire["holder"] as? [String: Any]
    expect("the wire carries the renewal age, which is what a reader acts on",
           holderRow?["renewalAgeSeconds"] as? Int, 30)
    expect("and what is working right now", (holderRow?["workPids"] as? [Int])?.count, 2)
    expect("and the budget the holder was given",
           ((holderRow?["budget"] as? [String: Any])?["parallelism"] as? Int), 2)
    expect("an idle lease says so with an explicit null, not a missing key",
           OrchestratorLease.record(minimal, now: leaseNow)["holder"] as? NSNull, NSNull())
}

}

/// The parallelism a grant carries, or zero. Small enough to live beside the group that uses it.
private func leaseGrantParallelism(_ admission: OrchestratorLease.Admission) -> Int {
    if case .granted(let budget) = admission { return budget.parallelism }
    return 0
}
