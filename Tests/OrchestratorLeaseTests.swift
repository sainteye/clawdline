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

/// A waiter, with its poll clock defaulting to when it joined — which is what a freshly created
/// entry looks like, and what a restart's `last_polled_at` falls back to.
private func leaseWaiter(_ id: String, label: String, session: String? = nil, pid: Int32 = 9,
                         processStart: Date? = leaseNow, requestedAt: Date = leaseNow,
                         lastPolledAt: Date? = nil, reason: String = "waiting")
    -> OrchestratorLease.Waiter {
    OrchestratorLease.Waiter(requestID: id, owner: leaseOwner(label, session: session), pid: pid,
                             processStart: processStart, requestedAt: requestedAt,
                             reason: reason, lastPolledAt: lastPolledAt ?? requestedAt)
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

/// Probes that answer nothing, so a test can replace exactly the one it is about. `perform` had
/// no test of any kind, which is how the takeover path came to act on a compiler reading taken
/// before four bounded subprocesses without anybody noticing.
private func leaseStubProbes() -> OrchestratorLease.Probes {
    OrchestratorLease.Probes(
        processStart: { _ in .unknown("stub") },
        compilers: { .none },
        pressure: { nil },
        topAnonymous: { [] },
        readDirectory: { _ in .absent },
        fileExists: { _ in false },
        beat: { _ in nil },
        createDirectory: { _, _ in nil },
        refreshHolderFile: { _, _ in nil },
        removeDirectory: { _, _ in nil })
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
        heartbeat: "/tmp/clawdline-suite.lock/beat", ownerPID: 72_929, heartbeatDeadline: 60)
    let text = OrchestratorLease.encode(file)
    guard let parsed = OrchestratorLease.parseHolderFile(text) else {
        check("a written holder file reads back", false); return
    }
    expect("every field survives the round trip", parsed, file)
    // Two of them are filled in rather than carried, because the contract says every record has
    // them: a record with no `owner_pid` names the only process it knows about, and one with no
    // deadline gets the number every reader would have used anyway.
    let bare = OrchestratorLease.parseHolderFile(OrchestratorLease.encode(
        OrchestratorLease.HolderFile(holder: "a shell", pid: 4_242, started: leaseNow)))
    expect("a record written without an owner names the process it does know about",
           bare?.ownerPID, 4_242)
    expect("and without a deadline carries the one every reader would have assumed",
           bare?.heartbeatDeadline, Int(OrchestratorLease.renewalDeadline))
    check("the six original fields are all present as key=value lines",
          ["holder=", "pid=", "started=", "tree=", "log=", "note="]
            .allSatisfy { text.contains("\n" + $0) || text.hasPrefix($0) })
    check("and the three additive ones are written under the keys the lock really uses",
          ["work=", "done_flag=", "renewed=", "phase=", "heartbeat="]
            .allSatisfy { text.contains("\n" + $0) })

    // **One record, three writers, and the whole list.** This writer used to emit eleven of the
    // eighteen fields, and none of the four the shell's compare-and-swap depends on, so against a
    // record it wrote that compare was `"" = ""` and always true.
    let contract = ["holder", "pid", "owner_pid", "owner_started", "token", "phase", "phase_since",
                    "heartbeat", "heartbeat_deadline", "started", "renewed", "tree", "log",
                    "done_flag", "work", "last_compiling", "compilers", "note"]
    let written = text.split(separator: "\n").map { String($0.prefix(while: { $0 != "=" })) }
    expect("this writer emits the contract, in the contract's order", written, contract)
    // Four different instants for the four clocks, for the same reason the store round trip below
    // now has ten: with `started`, `renewed`, `phase_since` and `last_compiling` all set to
    // `leaseNow`, this round trip could not have told two of them apart if the writer had swapped
    // them.
    let full = OrchestratorLease.HolderFile(
        holder: "build.sh", pid: 81_001, started: leaseNow.addingTimeInterval(-1_200),
        work: [81_001], renewed: leaseNow, phase: .compiling, heartbeat: "/tmp/l/beat",
        ownerPID: 4_242, ownerStarted: "Thu Sep  3 02:18:04 2026", token: "build-4242-17",
        heartbeatDeadline: 60, phaseSince: leaseNow.addingTimeInterval(-800),
        lastCompiling: leaseNow.addingTimeInterval(-400), compilers: "none")
    expect("no two clocks in this record carry the same instant either",
           Set([full.started!, full.renewed!, full.phaseSince!, full.lastCompiling!]).count, 4)
    expect("and the four the compare-and-swap needs survive a round trip",
           OrchestratorLease.parseHolderFile(OrchestratorLease.encode(full)), full)
    check("a token is what identity means here: pid alone is reused within hours",
          OrchestratorLease.parseHolderFile(OrchestratorLease.encode(full))?.token
            == "build-4242-17")
    // `owner_pid` is the run; `pid` is whatever is working at this beat and moves during one hold.
    expect("ownership reads the run, not the process that happens to be working",
           OrchestratorLease.parseHolderFile(OrchestratorLease.encode(full))?.owner, 4_242)
    expect("and a record written before the contract falls back to the only number it has",
           OrchestratorLease.parseHolderFile("pid=77\n")?.owner, 77)
    // The other half of the mismatch: the shell wrote `working=`, space separated, and this
    // parser read `work=` — so each side showed an empty working list for the other's holder.
    expect("the shell's older `working=` spelling is still read rather than dropped",
           OrchestratorLease.parseHolderFile("pid=5\nworking=11 12 13\n")?.work,
           [11, 12, 13])
    expect("`compilers` keeps `did not probe` apart from `probed and found nothing`",
           [OrchestratorLease.parseHolderFile("pid=5\ncompilers=\n")?.compilers,
            OrchestratorLease.parseHolderFile("pid=5\ncompilers=none\n")?.compilers],
           [nil, "none"])
    // A rewrite that dropped fields is how a live holder's record loses the two things a waiter
    // reads: what it is doing, and where its beat is.
    let holderRow = leaseHolder("lease-9", label: "root", renewedAt: leaseNow, work: [900],
                                phase: .compiling)
    var stamped = holderRow
    stamped.heartbeatPath = "/tmp/l/beat"
    stamped.token = "lease-9"
    let rewritten = OrchestratorLease.file(from: stamped)
    expect("a rewrite keeps the phase a waiter reads", rewritten.phase, .compiling)
    expect("and the beat it points at", rewritten.heartbeat, "/tmp/l/beat")
    expect("and the token the other two writers compare against", rewritten.token, "lease-9")
    expect("and names the working process as `pid` while ownership stays the run",
           [rewritten.pid, rewritten.owner], [900, 4_242])

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

    // **(B) is read again immediately before the directory goes.** The decision is made against a
    // reading taken before the rest of the turn's bounded probes, so the compiler count it rests
    // on can be many seconds old by the time anything is removed, and the identity compare inside
    // `removeDirectory` cannot see a holder that came back to life. `test.sh` re-runs its whole
    // admission immediately before its rename; this is the broker doing at least as much.
    let departing = leaseHolder("lease-old", label: "old", renewedAt: leaseNow, session: "s0")
    let arriving = leaseRequest("req-new", label: "new", session: "s1")
    let takeover = OrchestratorLease.acquire(
        record: heldRecord(departing),
        request: arriving, directory: heldDirectory(departing),
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .gone("task failure"),
                                             doneFlag: .absent),
        now: leaseNow.addingTimeInterval(600))
    check("a lapsed holder with a clear machine is eligible to be taken over",
          leaseGrant(takeover.outcome) != nil, "\(takeover.outcome)")
    var removals = 0
    var lateProbes = leaseStubProbes()
    lateProbes.compilers = { .present([9_001]) }
    lateProbes.removeDirectory = { _, _ in removals += 1; return nil }
    let late = OrchestratorLease.perform(takeover, on: heldRecord(departing), probes: lateProbes)
    expect("a compiler that started while the takeover was being decided refuses it",
           leaseRefusal(late.outcome)?.code, "takeover_failed")
    expect("and nothing was removed", removals, 0)
    check("the refusal names the compiler rather than leaving a person to find it",
          leaseRefusal(late.outcome)?.message.contains("9001") == true)
    var blindProbes = leaseStubProbes()
    blindProbes.compilers = { .unknown("ps timed out") }
    blindProbes.removeDirectory = { _, _ in removals += 1; return nil }
    let blind = OrchestratorLease.perform(takeover, on: heldRecord(departing), probes: blindProbes)
    expect("and a machine that will not answer blocks the takeover too",
           leaseRefusal(blind.outcome)?.code, "takeover_failed")
    expect("still removing nothing", removals, 0)
    check("and a timed-out scan is unknown too",
          OrchestratorLease.parseCompilerScan("", status: nil, timedOut: true) != .none)

    // **And one process reading per decision, not two.** There was a second, `holderProcess`, and
    // nothing anywhere read it: one bounded subprocess spent on every single decision, on the
    // machine whose subprocess and memory budget is the whole point of this feature, and counted
    // in the apology above for what a reading costs. The holder has no process axis by design — a
    // pid is a proxy and proxies outlive the work, which is how a `sleep 14400` came to be
    // recorded as a holder — so a field that looked like an axis and was not is gone rather than
    // left for somebody to wire up on the assumption it was meant to decide something.
    var counted = leaseStubProbes()
    var readingsTaken: [Int32] = []
    counted.processStart = { pid in readingsTaken.append(pid); return .unknown("stub") }
    let withLine = heldRecord(departing,
                              queue: [leaseWaiter("req-w", label: "waiting", pid: 5_150)])
    _ = OrchestratorLease.observe(record: withLine, probes: counted, ownerState: { _ in .live })
    expect("a decision takes one process reading, and it is the head of the line's",
           readingsTaken, [5_150])
    readingsTaken = []
    _ = OrchestratorLease.observe(record: heldRecord(departing), probes: counted,
                                  ownerState: { _ in .live })
    expect("and with nobody in the line it takes none at all", readingsTaken, [])
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
        leaseWaiter("w\($0)", label: "w\($0)", pid: 1, reason: "r")
    }
    let overflow = OrchestratorLease.acquire(
        record: crowded, request: leaseRequest("req-x", label: "late", session: "sx"),
        directory: heldDirectory(lapsedHolder),
        evidence: OrchestratorLease.Evidence(compilers: .present([1]), owner: .live,
                                             doneFlag: .absent),
        now: leaseNow)
    expect("a full queue refuses rather than growing a list nobody reads",
           leaseRefusal(overflow.outcome)?.code, "queue_full")

    // **A waiter proves it is alive the same way a holder does, and it had no way to.**
    //
    // `pid` and `processStart` were recorded and read by nothing, so a waiter whose process died
    // at the head of the queue left an entry that could never be granted, never expired, and
    // could only be cancelled by an owner that no longer existed — while the lock itself was
    // free. Everybody behind it was answered `queued_behind_others` for ever, the queue is
    // persisted so it survived a restart, and thirty-two of them turned it into `queue_full` for
    // the whole machine: the same deadlock with a different code.
    let later = leaseNow.addingTimeInterval(OrchestratorLease.waiterDeadline + 30)
    var abandoned = OrchestratorLease.Record(resource: OrchestratorLease.heavyCompile,
                                             directory: leaseDirectory)
    abandoned.queue = [leaseWaiter("dead", label: "gave up", session: "sd"),
                       leaseWaiter("alive", label: "still here", session: "sa",
                                   requestedAt: leaseNow.addingTimeInterval(1),
                                   lastPolledAt: later)]
    let past = OrchestratorLease.acquire(
        record: abandoned, request: leaseRequest("alive", label: "still here", session: "sa"),
        directory: .absent,
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent),
        now: later)
    check("a waiter that stopped asking is passed over, not left blocking the line",
          leaseGrant(past.outcome) != nil, "\(past.outcome)")
    expect("and it keeps its place rather than being reclaimed from",
           past.record.queue.map { $0.requestID }, ["dead"])
    // The other direction, which is what makes the first one safe rather than merely convenient.
    let stillWaiting = OrchestratorLease.acquire(
        record: abandoned, request: leaseRequest("late", label: "third", session: "s3"),
        directory: .absent,
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent),
        now: leaseNow.addingTimeInterval(5))
    expect("a waiter that is still asking blocks the ones behind it, exactly as before",
           leaseQueued(stillWaiting.outcome)?.holdReason, "queued_behind_others")
    // Asking again is the proof, and it must not cost a place in the line.
    let reasking = OrchestratorLease.acquire(
        record: abandoned, request: leaseRequest("dead", label: "gave up", session: "sd"),
        directory: .absent,
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent),
        now: later)
    expect("a waiter that starts asking again is proving itself again", reasking.record.queue.count, 2)
    expect("and its place in the line is where it always was",
           reasking.record.queue.first?.requestID, "dead")
    check("the poll moved its clock without moving its position",
          reasking.record.queue.first?.lastPolledAt == later
            && reasking.record.queue.first?.requestedAt == leaseNow)
    // The process axis, applied to the head of the line only, strengthens the clock and never
    // weakens it: a reading that could not be taken says nothing.
    let headGone = OrchestratorLease.Evidence(headWaiterProcess: .absent, compilers: .none,
                                              owner: .live, doneFlag: .absent)
    var fresh = OrchestratorLease.Record(resource: OrchestratorLease.heavyCompile,
                                         directory: leaseDirectory)
    fresh.queue = [leaseWaiter("gone", label: "dead process", session: "sg"),
                   leaseWaiter("second", label: "behind it", session: "sb",
                               requestedAt: leaseNow.addingTimeInterval(1))]
    let overtaken = OrchestratorLease.acquire(
        record: fresh, request: leaseRequest("second", label: "behind it", session: "sb"),
        directory: .absent, evidence: headGone, now: leaseNow.addingTimeInterval(5))
    check("a head waiter whose process is gone is passed over at once, not after two minutes",
          leaseGrant(overtaken.outcome) != nil, "\(overtaken.outcome)")
    let unreadable = OrchestratorLease.acquire(
        record: fresh, request: leaseRequest("second", label: "behind it", session: "sb"),
        directory: .absent,
        evidence: OrchestratorLease.Evidence(headWaiterProcess: .unknown("ps failed"),
                                             compilers: .none,
                                             owner: .live, doneFlag: .absent),
        now: leaseNow.addingTimeInterval(5))
    expect("but a reading that could not be taken leaves the clock to decide",
           leaseQueued(unreadable.outcome)?.holdReason, "queued_behind_others")
    // And the depth limit counts the line, not the litter.
    var litter = OrchestratorLease.Record(resource: OrchestratorLease.heavyCompile,
                                          directory: leaseDirectory)
    litter.queue = (0..<OrchestratorLease.queueDepthLimit).map {
        leaseWaiter("q\($0)", label: "q\($0)", pid: 1, reason: "r")
    }
    let admitted = OrchestratorLease.acquire(
        record: litter, request: leaseRequest("req-y", label: "late", session: "sy"),
        directory: .absent,
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent),
        now: later)
    check("thirty-two entries nobody is asking for no longer answer 429 to everybody",
          leaseRefusal(admitted.outcome)?.code != "queue_full", "\(admitted.outcome)")

    let wire = OrchestratorLease.record(third.record, now: leaseNow.addingTimeInterval(30))
    expect("the queue is observable: who, in what order, since when",
           (wire["queue"] as? [[String: Any]])?.count, 2)
    expect("with an age a person can read",
           ((wire["queue"] as? [[String: Any]])?.first?["waitedSeconds"] as? Int), 30)
    expect("and a hold reason beside it", wire["holdReason"] as? String, "holder_proving")
}

group("release and cancel belong to the sessions that own them") {
    let holder = leaseHolder("lease-1", label: "first", renewedAt: leaseNow, session: "s1")
    let waiter = leaseWaiter("req-2", label: "second", session: "s2")
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

group("a heartbeat that outlives its work is a sentinel, and only the loop shape stops it") {
    // The defect, executed rather than described. Two shells beat into two files; both have a
    // one-second "work" process. One loop's condition is that work still being alive, the other
    // is a timer. When the work ends, exactly one of them stops — and the one that does not is
    // `sleep 14400` wearing a heartbeat.
    let root = (NSTemporaryDirectory() as NSString)
        .appendingPathComponent("clawdline-beat-\(UUID().uuidString.prefix(8))")
    try? FileManager.default.createDirectory(atPath: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(atPath: root) }
    let supervised = (root as NSString).appendingPathComponent("supervised")
    let detached = (root as NSString).appendingPathComponent("detached")

    func shell(_ script: String) -> Process {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/sh")
        process.arguments = ["-c", script]
        try? process.run()
        return process
    }
    // The right shape: the loop that waits on the work is the loop that beats.
    let right = shell("sleep 1 & w=$!; while kill -0 $w 2>/dev/null; "
                      + "do : > \(supervised); sleep 0.2; done")
    // The wrong shape: a timer with no idea what it stands for. Bounded so it ends by itself —
    // nothing in this suite signals a process to make a point.
    let wrong = shell("sleep 1 & i=0; while [ $i -lt 40 ]; "
                      + "do : > \(detached); sleep 0.2; i=$((i+1)); done")
    right.waitUntilExit()

    func modified(_ path: String) -> Date? {
        (try? FileManager.default.attributesOfItem(atPath: path))?[.modificationDate] as? Date
    }
    guard let supervisedStop = modified(supervised), let detachedFirst = modified(detached) else {
        check("both loops produced a beat", false)
        wrong.terminate(); wrong.waitUntilExit(); return
    }
    check("both loops produced a beat", true)
    Thread.sleep(forTimeInterval: 1.0)
    let supervisedAfter = modified(supervised)
    let detachedAfter = modified(detached)
    wrong.terminate()
    wrong.waitUntilExit()

    expect("the supervised beat stopped when its work did", supervisedAfter, supervisedStop)
    guard let detachedAfter, let supervisedAfter else {
        check("the detached beat is still going", false); return
    }
    check("while the detached timer beat on for a work that had already ended",
          detachedAfter > detachedFirst,
          "\(detachedAfter) vs \(detachedFirst)")
    check("so the two are distinguishable by exactly one second of not beating",
          detachedAfter.timeIntervalSince(supervisedAfter) > 0.5,
          "\(detachedAfter.timeIntervalSince(supervisedAfter))")

    // And the shipped script has to be the first shape. This reads build.sh off disk the way
    // `appVersion()` does, because a rule that lives only in a comment is a rule that drifts.
    let build = (try? String(contentsOfFile: "build.sh", encoding: .utf8)) ?? ""
    check("build.sh has a heartbeat at all", build.contains("clawdline_lease_beat"))
    check("emitted by a loop whose condition is the compiler still being alive",
          build.contains("while kill -0 \"$compiler\" 2>/dev/null; do"))
    check("which then waits on that same process",
          build.contains("wait \"$CLAWDLINE_COMPILER\""))
    // Not "the file contains no `while true`" — it has a queue-wait loop and a commented
    // counter-example, and a check that fails on those is a check somebody deletes. What must
    // hold is narrower and is the actual rule: every unbounded loop in the file is a comment,
    // and the only loop that beats is the one supervising the compiler.
    let unbounded = build.split(separator: "\n").filter {
        $0.contains("while true") && !$0.trimmingCharacters(in: .whitespaces).hasPrefix("#")
    }
    check("no live unbounded loop exists at all", unbounded.isEmpty, "\(unbounded)")
    check("and the wrong shape survives only as the counter-example it is named as",
          build.contains("# a sentinel"), "the commented counter-example went missing")
    let beatingLines = build.split(separator: "\n").enumerated().filter {
        $0.element.contains("clawdline_lease_beat ") && !$0.element.contains("()")
            && !$0.element.trimmingCharacters(in: .whitespaces).hasPrefix("#")
    }
    check("the beat is called from exactly one place", beatingLines.count == 1,
          "\(beatingLines.map(\.element))")
    if let beating = beatingLines.first {
        let previous = build.split(separator: "\n")[max(0, beating.offset - 1)]
        check("and that place is the loop waiting on the compiler",
              previous.contains("while kill -0 \"$compiler\""), String(previous))
    }
    check("the beat lives inside the lock directory, so rmdir takes it too",
          build.contains("\"$CLAWDLINE_LEASE_DIR/beat\""))
}

group("the projections keep six states apart, and a refusal is one of them") {
    // Nothing tested `leaseBearings` or `leaseSession` at all — they were sixty lines of pure
    // projection sitting in `Sources/Orchestrator.swift`, executed by no check, which is how
    // `refused` came to be a state neither of them could express: an admission refusal leaves the
    // record otherwise untouched, so a session told "this Mac cannot admit even one compiler"
    // looked exactly like a session that had never asked.
    let holder = leaseHolder("lease-1", label: "root af1b83ba", renewedAt: leaseNow,
                             session: "s1", phase: .compiling)
    expect("no record at all is `missing`, which is not `nobody is compiling`",
           OrchestratorLease.bearings(nil, now: leaseNow).state, "missing")
    var idle = OrchestratorLease.Record(resource: OrchestratorLease.heavyCompile,
                                        directory: leaseDirectory, reconciledAt: leaseNow)
    expect("a record with nobody in it is `zero`",
           OrchestratorLease.bearings(idle, now: leaseNow).state, "zero")
    idle.reconciliation = .unreadable
    expect("a lock that cannot be read is `unknown`",
           OrchestratorLease.bearings(idle, now: leaseNow).state, "unknown")
    var queued = OrchestratorLease.Record(resource: OrchestratorLease.heavyCompile,
                                          directory: leaseDirectory, reconciledAt: leaseNow)
    queued.queue = [leaseWaiter("w1", label: "waiting", session: "s2")]
    expect("a line with nobody holding is `queued`",
           OrchestratorLease.bearings(queued, now: leaseNow).state, "queued")
    var held = heldRecord(holder)
    held.reconciledAt = leaseNow
    expect("and a holder is `held`", OrchestratorLease.bearings(held, now: leaseNow).state, "held")

    var refused = OrchestratorLease.Record(resource: OrchestratorLease.heavyCompile,
                                           directory: leaseDirectory, reconciledAt: leaseNow)
    refused.lastRefusal = OrchestratorLease.RefusalNote(
        code: "pressure_refused", message: "This Mac cannot admit even one compiler",
        at: leaseNow, requestID: "req-1", sessionID: "s9", taskID: nil)
    expect("a session that asked and was told no is `refused`, not `zero`",
           OrchestratorLease.bearings(refused, now: leaseNow).state, "refused")
    expect("with the reason beside it",
           OrchestratorLease.bearings(refused, now: leaseNow).holdReason, "pressure_refused")
    expect("and a refusal ages out, because it is a moment rather than a state",
           OrchestratorLease.bearings(refused,
               now: leaseNow.addingTimeInterval(OrchestratorLease.refusalVisibleFor + 1)).state,
           "zero")

    // The freshness clock. This projection never re-reads the filesystem, so what it shows is as
    // old as the last reconciliation — and the Bearings row used to be stamped with the *task*
    // registry's clock, which made a lock held by a run that died an hour ago read as current.
    expect("the lease carries its own observation time, not another registry's",
           OrchestratorLease.bearings(held, now: leaseNow).observedAt, leaseNow)
    check("and a record that has never been reconciled says so rather than claiming currency",
          OrchestratorLease.bearings(
              OrchestratorLease.Record(resource: OrchestratorLease.heavyCompile),
              now: leaseNow).observedAt == nil)

    // The Session overlay, which is the same three answers for one terminal.
    let holdingRow = OrchestratorLease.sessionRow(held, forSession: "s1", now: leaseNow)
    expect("the session holding the slot is shown holding it",
           holdingRow?["state"] as? String, "holding")
    expect("with what it is doing", holdingRow?["phase"] as? String, "compiling")
    let queuedRow = OrchestratorLease.sessionRow(queued, forSession: "s2", now: leaseNow)
    expect("a queued session is shown its place", queuedRow?["position"] as? Int, 1)
    expect("and whether it is still proving it is waiting",
           queuedRow?["liveness"] as? String, "proving")
    let lapsedRow = OrchestratorLease.sessionRow(
        queued, forSession: "s2",
        now: leaseNow.addingTimeInterval(OrchestratorLease.waiterDeadline + 1))
    expect("a queue entry that stopped asking says so rather than looking like a live wait",
           lapsedRow?["liveness"] as? String, "waiter_stopped_asking")
    let refusedRow = OrchestratorLease.sessionRow(refused, forSession: "s9", now: leaseNow)
    expect("and the refused session gets a row of its own",
           refusedRow?["state"] as? String, "refused")
    expect("naming the refusal it can act on",
           refusedRow?["reason"] as? String, "pressure_refused")
    check("a session that never asked still gets nothing",
          OrchestratorLease.sessionRow(refused, forSession: "s-nobody", now: leaseNow) == nil)
    // A refusal that has been answered stops being shown.
    let served = OrchestratorLease.acquire(
        record: refused, request: leaseRequest("req-1", label: "asker", session: "s9"),
        directory: .absent,
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent),
        now: leaseNow)
    check("once the same asker is granted, its refusal is cleared rather than kept on screen",
          leaseGrant(served.outcome) != nil && served.record.lastRefusal == nil,
          "\(served.outcome)")

    expect("and one audit word per outcome, from the file that owns the vocabulary",
           [OrchestratorLease.outcomeWord(.released),
            OrchestratorLease.outcomeWord(.queued(position: 2, holdReason: "holder_proving")),
            OrchestratorLease.outcomeWord(.refused(OrchestratorLease.Refusal(
                status: 429, code: "queue_full", message: "full")))],
           ["released", "queued:holder_proving", "queue_full"])
}

group("a refusal is an answer to an ask, and the ask is what a waiter proves itself with") {
    // The ratified rule, in the design's own words: `POST /v1/orchestrator/leases` is idempotent
    // on `request_id` and a waiting client re-sends it every few seconds, so **asking again is to
    // a waiter what renewing is to a holder**. The clock moved in `enqueue` and nowhere else, so
    // the two answers that are not `queued` did not count as asking — a refusal, and a decision
    // whose effect failed. A head of the line polling every five seconds exactly as the contract
    // asks was therefore recorded as having gone quiet, passed over after `waiterDeadline`, and
    // droppable by the hard-limit trim whose comment promises "never a waiter that is still
    // asking". Latent today only because `leasePolicy` carries no floor; the next node in this
    // feature's own plan is the one that measures the number that arms it.
    let tight = OrchestratorLease.Pressure(
        physicalMB: 24_576, freeMB: 100, anonymousMB: 20_000, fileBackedMB: 100,
        compressorMB: 1_270, swapUsedMB: 10_870, swapFreeMB: 40, swapTotalMB: 12_288,
        observedAt: leaseNow)
    let floored = OrchestratorLease.Policy(perCompileMB: 3_000, floorRequirementMB: 4_000)
    let joined = leaseNow.addingTimeInterval(-100)
    var waiting = OrchestratorLease.Record(resource: OrchestratorLease.heavyCompile,
                                           directory: leaseDirectory)
    waiting.queue = [leaseWaiter("req-head", label: "head", session: "s1",
                                 requestedAt: joined, lastPolledAt: joined),
                     leaseWaiter("req-behind", label: "behind", session: "s2",
                                 requestedAt: joined.addingTimeInterval(1),
                                 lastPolledAt: joined.addingTimeInterval(1))]
    let refused = OrchestratorLease.acquire(
        record: waiting, request: leaseRequest("req-head", label: "head", session: "s1"),
        directory: .absent,
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent,
                                             pressure: tight),
        policy: floored, topAnonymous: [], now: leaseNow)
    expect("the head of the line is refused when this Mac cannot admit even one compiler",
           leaseRefusal(refused.outcome)?.code, "pressure_refused")
    expect("and its poll clock moves, because it asked", refused.record.queue.first?.lastPolledAt,
           leaseNow)
    expect("its place in the line is untouched — re-asking must never cost it",
           refused.record.queue.first?.requestedAt, joined)
    expect("and nobody else's clock moves with it",
           refused.record.queue.last?.lastPolledAt, joined.addingTimeInterval(1))

    // The consequence, which is the defect rather than the bookkeeping: a waiter refused for the
    // length of the pressure is still asking, and a later arrival must not take the slot from it.
    let soonAfter = leaseNow.addingTimeInterval(OrchestratorLease.waiterDeadline - 10)
    let jumper = OrchestratorLease.acquire(
        record: refused.record, request: leaseRequest("req-late", label: "late", session: "s9"),
        directory: .absent,
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent),
        now: soonAfter)
    expect("so a waiter that has been refused all along still holds the head of the line",
           leaseQueued(jumper.outcome)?.holdReason, "queued_behind_others")
    // The control, so the check above is measuring the clock and not a queue that never moves:
    // the same arrival, against the same queue, once the head really has stopped asking.
    let muchLater = leaseNow.addingTimeInterval(OrchestratorLease.waiterDeadline * 2)
    let inherits = OrchestratorLease.acquire(
        record: refused.record, request: leaseRequest("req-late", label: "late", session: "s9"),
        directory: .absent,
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent),
        now: muchLater)
    check("while a head that has genuinely stopped asking is passed over as it always was",
          leaseGrant(inherits.outcome) != nil, "\(inherits.outcome)")

    // **The same rule for the answer the decision did not even make.** `lease_changed` and
    // `takeover_failed` return the record *unchanged*, which is right for everything the effect
    // would have done and wrong for the one thing that happened before it was attempted. These
    // two are produced by exactly the race this feature exists for — `test.sh` takes the lock
    // directory with `mkdir` and tells nobody — so on a contended machine a waiter could be
    // declared silent by the very contention it was waiting out.
    var racing = leaseStubProbes()
    racing.createDirectory = { _, _ in "somebody took the directory first" }
    let granting = OrchestratorLease.acquire(
        record: waiting, request: leaseRequest("req-head", label: "head", session: "s1"),
        directory: .absent,
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent),
        now: leaseNow)
    check("with no floor in the policy that same head is granted, so an effect is attempted",
          leaseGrant(granting.outcome) != nil, "\(granting.outcome)")
    let applied = OrchestratorLease.perform(granting, on: waiting, probes: racing)
    expect("a lock that changed hands mid-decision answers `lease_changed`, which means ask again",
           leaseRefusal(applied.outcome)?.code, "lease_changed")
    expect("the queue entry comes back, because the grant did not happen",
           applied.record.queue.map { $0.requestID }, ["req-head", "req-behind"])
    expect("and the poll it made on the way through is still counted",
           applied.record.queue.first?.lastPolledAt, leaseNow)

    // And nothing that is not a request for the slot moves anybody's clock. A renewal is the
    // holder's proof of life, not a waiter's.
    let holding = leaseHolder("lease-r", label: "holding", renewedAt: leaseNow, session: "sh")
    let held = heldRecord(holding, queue: waiting.queue)
    let renewed = OrchestratorLease.renew(
        record: held, leaseID: "lease-r", owner: leaseOwner("holding", session: "sh"),
        workPIDs: [7], directory: heldDirectory(holding),
        evidence: OrchestratorLease.Evidence(compilers: .none, owner: .live, doneFlag: .absent),
        now: leaseNow.addingTimeInterval(20))
    expect("renewing a hold moves no waiter's poll clock, because it is not an ask",
           renewed.record.queue.first?.lastPolledAt, joined)

    // A refusal has to still be readable at the moment its asker is declared silent. The two
    // clocks used to expire together, so a person opening Bearings at exactly that moment saw
    // neither the request nor the reason it had been given.
    check("a refusal outlives the waiter deadline it is shown beside",
          OrchestratorLease.refusalVisibleFor > OrchestratorLease.waiterDeadline)
}

group("the lease record survives a store round trip") {
    // **Every value in this fixture is distinct, and that is the assertion.** The group said
    // "every field survives" over a codec that dropped three of them, and it was green: the
    // fixture never set `Holder.token`, `Holder.ownerStarted` or `Record.lastRefusal`, so
    // nothing was there to lose. The round's one new store field was worse — `last_polled_at`
    // could be deleted from `stored()` with this group still green, because the waiter's poll
    // clock equalled its `requestedAt` and the loader falls back to exactly that. A fixture whose
    // values coincide cannot tell two fields apart, and a check that cannot fail is worse than no
    // check, because it is counted.
    let holder = OrchestratorLease.Holder(
        leaseID: "lease-1",
        owner: OrchestratorLease.Owner(sessionID: "s1", taskID: "23e78454",
                                       rootSessionID: "af1b83ba", label: "root af1b83ba"),
        pid: 72_929,
        processStart: leaseNow.addingTimeInterval(-900),
        acquiredAt: leaseNow.addingTimeInterval(-600),
        renewedAt: leaseNow.addingTimeInterval(30),
        workPIDs: [8_101, 8_102],
        doneFlagPath: "/tmp/clawdline-suite.lock/done",
        tree: "/tmp/tree", log: "/tmp/log", note: "the full swift suite",
        budget: OrchestratorLease.Budget(parallelism: 2,
                                         basis: "headroom_mb/3000mb_per_compile",
                                         headroomMB: 7_732, swapFreeMB: 1_417),
        provenance: .broker,
        phase: .idleHolding,
        phaseSince: leaseNow.addingTimeInterval(-300),
        lastCompilingAt: leaseNow.addingTimeInterval(-450),
        heartbeatPath: "/tmp/clawdline-suite.lock/beat",
        // The two the codec dropped. Neither is minted by the broker: they are what the record in
        // the lock directory carries, and the token is the field the *other* two writers compare
        // against — a rewrite that lost it tells a live shell holder its lock changed hands.
        token: "build-72929-1788369400",
        ownerStarted: "Thu Sep  3 02:18:04 2026")
    let waiter = OrchestratorLease.Waiter(
        requestID: "req-2",
        owner: OrchestratorLease.Owner(sessionID: "s2", taskID: "7c19aa30",
                                       rootSessionID: "af1b83ba", label: "second"),
        pid: 9_314,
        processStart: leaseNow.addingTimeInterval(-120),
        requestedAt: leaseNow.addingTimeInterval(-60),
        reason: "queued for the suite",
        // Deliberately not `requestedAt`: this waiter joined the line a minute ago and asked
        // again five seconds ago, which is the only shape in which the poll clock is a fact
        // rather than a copy of another field.
        lastPolledAt: leaseNow.addingTimeInterval(-5))
    let refusal = OrchestratorLease.RefusalNote(
        code: "pressure_refused",
        message: "This Mac cannot admit even one compiler: headroom_mb is 812 MB against 3000 MB",
        at: leaseNow.addingTimeInterval(-15), requestID: "req-3", sessionID: "s3",
        taskID: "aa11bb22")
    let record = OrchestratorLease.Record(
        resource: OrchestratorLease.heavyCompile, directory: leaseDirectory, holder: holder,
        queue: [waiter], reconciliation: .matched, reconciledAt: leaseNow,
        holdReason: nil, livenessReason: "proving", lastRefusal: refusal)
    // The fixture's own control, so this group cannot quietly go back to being unable to fail:
    // ten clocks, ten different instants. Any two that coincide are two fields a dropped line
    // could swap without this group noticing.
    let stamps: [Date] = [holder.processStart!, holder.acquiredAt, holder.renewedAt,
                          holder.phaseSince, holder.lastCompilingAt!,
                          waiter.processStart!, waiter.requestedAt, waiter.lastPolledAt,
                          refusal.at, record.reconciledAt!]
    expect("no two clocks in this fixture carry the same instant, or it could not tell them apart",
           Set(stamps).count, stamps.count)
    let stored = OrchestratorStore.stored(record)
    guard let back = OrchestratorStore.lease(from: stored) else {
        check("a stored lease reads back", false); return
    }
    expect("every field survives", back, record)
    // Named one at a time as well as compared whole, so a red says which line of the codec went
    // rather than printing two records for somebody to diff by eye.
    expect("the holder's token survives, which is what the other two writers compare against",
           back.holder?.token, holder.token)
    expect("and the start identity the shell writers compare beside it",
           back.holder?.ownerStarted, holder.ownerStarted)
    expect("the waiter's poll clock survives, so a restart does not resurrect the line as freshly asked",
           back.queue.first?.lastPolledAt, waiter.lastPolledAt)
    expect("and the refusal, so `asked and was told no` is not `never asked` after a restart",
           back.lastRefusal, refusal)
    // A refusal row that lost the id it is about belongs to nobody — both projections match it
    // against a request — so it is dropped rather than half-resurrected.
    var truncated = stored
    var refusalRow = truncated["last_refusal"] as? [String: Any] ?? [:]
    refusalRow.removeValue(forKey: "request_id")
    truncated["last_refusal"] = refusalRow
    expect("a refusal row with no request id is dropped rather than half-resurrected",
           OrchestratorStore.lease(from: truncated)?.lastRefusal == nil, true)
    // Provenance has a fallback of `.broker`, so only a record that is *not* a broker grant can
    // pin it. An adopted holder is that record, and it is a real state: a `holder.txt` this
    // broker did not write, read back after a restart.
    var adopted = holder
    adopted.provenance = .directory
    adopted.budget = nil
    let adoptedRecord = OrchestratorLease.Record(
        resource: OrchestratorLease.heavyCompile, directory: leaseDirectory, holder: adopted,
        queue: [], reconciliation: .adopted, reconciledAt: leaseNow)
    expect("a holder adopted from the directory reads back as adopted, not as a broker grant",
           OrchestratorStore.lease(from: OrchestratorStore.stored(adoptedRecord)), adoptedRecord)

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
