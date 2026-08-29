import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - Claude Code's session registry

// Real files, taken off a running machine — the ones quoted in
// artifacts/2026-08-25-contracts-over-shapes.md §2.3. Kept whole rather than trimmed to the
// fields under test: what breaks a parser is the field nobody thought about.
private let registryBusy = #"{"pid":10407,"sessionId":"f0108a4c-18fc-48fd-98f5-46016169a6a8","cwd":"/Users/sainteye/code/clawdline","startedAt":1787638904666,"procStart":"Tue Aug 25 06:21:43 2026","version":"2.1.245","peerProtocol":1,"peerFeatures":["notify_idle","artifact_yield"],"kind":"interactive","entrypoint":"cli","messagingSocketPath":"/tmp/cc-socks/10407.sock","name":"clawdline-03","nameSource":"derived","nameSince":1787638904666,"status":"busy","updatedAt":1787638966573,"statusUpdatedAt":1787638966573,"bridgeSessionId":"session_01KkTi3hSUWwQc9KdBweow7T"}"#

private let registryIdle = #"{"pid":33510,"sessionId":"1032cdf2-194f-4d29-aff1-8703719e8977","cwd":"/Users/sainteye/code/sugar-elite","startedAt":1787635103029,"procStart":"Tue Aug 25 05:18:22 2026","version":"2.1.241","peerProtocol":1,"peerFeatures":["notify_idle"],"kind":"interactive","entrypoint":"cli","messagingSocketPath":"/tmp/cc-socks/33510.sock","name":"sugar-elite-e1","nameSource":"derived","nameSince":1787635103029,"status":"idle","updatedAt":1787635103098,"statusUpdatedAt":1787635103098,"bridgeSessionId":"session_019TBjKEZPzR2kFbab5QK7ix"}"#

// The state this whole file exists for, and the one the report caught live: pid 73886 sat in
// `waiting`/`input needed` for fifty-three seconds while somebody was away from the terminal.
private let registryWaiting = #"{"pid":73886,"sessionId":"7864bbba-68f5-493b-a8ff-2c891e998a76","cwd":"/Users/sainteye/code/clawdline","startedAt":1787633574594,"procStart":"Tue Aug 25 04:52:53 2026","version":"2.1.241","peerProtocol":1,"peerFeatures":["notify_idle"],"kind":"interactive","entrypoint":"cli","messagingSocketPath":"/tmp/cc-socks/73886.sock","name":"clawdline-cd","nameSource":"derived","nameSince":1787633574594,"status":"waiting","waitingFor":"input needed","updatedAt":1787636333659,"statusUpdatedAt":1787636333659,"bridgeSessionId":"session_01TyawSRwKDDs198RArmDhik"}"#

// `!` mode. Observed 147 times in a fifteen-minute poll and listed in no documentation, which is
// the entire argument for having a case for words this build does not know.
private let registryShell = #"{"pid":12872,"sessionId":"5cbe1f22-2b26-4e35-9f1e-2f1cf5f0f4b1","cwd":"/Users/sainteye/code/cairn","startedAt":1787636273000,"procStart":"Tue Aug 25 05:37:53 2026","version":"2.1.245","peerProtocol":1,"peerFeatures":["notify_idle"],"kind":"interactive","entrypoint":"cli","name":"cairn-05","status":"shell","updatedAt":1787636273670,"statusUpdatedAt":1787636273670}"#

// The file exists and the session has not written a status into it yet. Caught twice in the same
// poll, so it is a state to have an answer for rather than a hypothetical.
private let registryNewborn = #"{"pid":67398,"sessionId":"9d0f1a77-1a3b-4e5d-8c0a-77c1a5f0d2b4","cwd":"/Users/sainteye/code/sugarjs","startedAt":1787635955290,"procStart":"Tue Aug 25 05:32:35 2026","version":"2.1.245","peerProtocol":1,"peerFeatures":["notify_idle"],"kind":"interactive","entrypoint":"cli","name":"sugarjs-0f","updatedAt":1787635955352}"#

// A tab whose conversation has been moved to the background, and the session it moved into.
// Both taken off this machine at once, forty minutes after the move: the tab's file still says
// `busy` with `statusUpdatedAt` frozen at the moment it happened, while the background session
// had changed state three times since. `bridgeSessionId` is a real `null` in the parked one,
// which is the sort of field a fixture is kept whole for.
private let registryParked = #"{"pid":66274,"sessionId":"8967a1ee-9718-45ed-94d5-c81178870072","cwd":"/Users/sainteye/code/clawdline","startedAt":1787648437146,"procStart":"Tue Aug 25 09:00:36 2026","version":"2.1.245","peerProtocol":1,"peerFeatures":["notify_idle","artifact_yield"],"kind":"interactive","entrypoint":"cli","messagingSocketPath":"/tmp/cc-socks/66274.sock","name":"修正瀏覽器問答","nameSince":1787648453353,"updatedAt":1787648740672,"status":"busy","statusUpdatedAt":1787648514977,"bridgeSessionId":null,"parkedJobId":"1f47c762"}"#

private let registryBackground = #"{"pid":77507,"sessionId":"1f47c762-60e9-4dc1-8967-fadb4038448c","cwd":"/Users/sainteye/code/clawdline","startedAt":1787648741820,"procStart":"Tue Aug 25 09:05:41 2026","version":"2.1.245","peerProtocol":1,"peerFeatures":["notify_idle","artifact_yield"],"kind":"bg","entrypoint":"cli","messagingSocketPath":"/tmp/cc-socks/77507.sock","name":"修正瀏覽器問答","nameSince":1787648741820,"jobId":"1f47c762","status":"idle","updatedAt":1787651142644,"statusUpdatedAt":1787651142644,"bridgeSessionId":"session_01PDfmY51xZ4Kq67WCstLDtm"}"#

// A background session nobody parked into: the daemon keeps a warmed-up worker ahead of demand,
// and it has a job id of its own from the moment it exists. It is here because it is what the
// directory really holds next to the one being looked for.
private let registrySpare = #"{"pid":78522,"sessionId":"84e9b9f1-aeb6-450d-aff6-e5f16d0a8fdb","cwd":"/Users/sainteye/code/clawdline","startedAt":1787649513458,"procStart":"Tue Aug 25 09:06:00 2026","version":"2.1.245","peerProtocol":1,"peerFeatures":["notify_idle","artifact_yield"],"kind":"bg","entrypoint":"cli","messagingSocketPath":"/tmp/cc-socks/78522.sock","name":"84e9b9f1","nameSince":1787649513458,"agent":"claude","jobId":"84e9b9f1","spare":true,"status":"idle","updatedAt":1787649513298,"statusUpdatedAt":1787649513298}"#

private func registrySession(_ id: String, assistant: Assistant? = .claude) -> TargetSession {
    TargetSession(backend: .iterm, id: id, name: "x", tty: "/dev/ttys004",
                  windowIndex: 0, tabIndex: 0, assistant: assistant)
}

/// A reading in which `entry` really does belong to session `id`: the pid matches and the start
/// time this Mac would have measured agrees with the one in the file.
private func registryReading(_ id: String, _ json: String, drift: TimeInterval = 0,
                             measured: Bool = true) -> SessionRegistry.Reading {
    guard let entry = SessionRegistry.parse(Data(json.utf8)) else { return SessionRegistry.Reading() }
    let started = measured
        ? SessionRegistry.procStartDate(entry.procStart)?.addingTimeInterval(drift) : nil
    return SessionRegistry.Reading(entries: [entry.pid: entry],
                                   processes: [id: SessionRegistry.Process(pid: entry.pid,
                                                                           started: started)])
}

/// A reading of a parked tab belonging to session `id`, with the background session its
/// conversation moved into folded in the way ``SessionRegistry/attachBackground(to:in:started:)``
/// would have folded it in.
///
/// `live: nil` is the case this whole thing exists to get right: the tab is parked and there is
/// no background session to be found — it ended, its file was never written, the machine was
/// rebooted under it. `job` overrides which job the background half is filed under, for the one
/// question a lookup by name cannot answer on its own: whether the two really are about the same
/// conversation or merely landed in the same dictionary.
private func registryParkedReading(_ id: String, live: String? = registryBackground,
                                   job: String? = nil, drift: TimeInterval = 0,
                                   measured: Bool = true) -> SessionRegistry.Reading {
    var reading = registryReading(id, registryParked)
    guard let json = live, let entry = SessionRegistry.parse(Data(json.utf8)),
          let key = job ?? entry.jobId else { return reading }
    let started = measured
        ? SessionRegistry.procStartDate(entry.procStart)?.addingTimeInterval(drift) : nil
    reading.entries[entry.pid] = entry
    reading.background[key] = SessionRegistry.Process(pid: entry.pid, started: started)
    return reading
}
















// MARK: - Commands a session left running

func runSessionRegistryTests() {
group("session registry: reading a file a session wrote about itself") {
    let busy = SessionRegistry.parse(Data(registryBusy.utf8))
    check("a whole file reads", busy != nil)
    expect("its pid", busy?.pid, 10407)
    expect("its status", busy?.status, .busy)
    expect("the id that also names its transcript", busy?.sessionID,
           "f0108a4c-18fc-48fd-98f5-46016169a6a8")
    expect("where it is working", busy?.cwd, "/Users/sainteye/code/clawdline")
    expect("the name Claude Code gave it", busy?.name, "clawdline-03")
    expect("the protocol version it states", busy?.peerProtocol, 1)
    check("nothing is waiting on it", busy?.waitingFor == nil)

    let waiting = SessionRegistry.parse(Data(registryWaiting.utf8))
    expect("a session stopped on a question says so", waiting?.status, .waiting)
    expect("and says what for", waiting?.waitingFor, "input needed")

    // The word documented nowhere. Carried rather than dropped, so that the merge can tell "I
    // have no opinion" apart from "there was no file".
    expect("a status this build has never heard of is kept as itself",
           SessionRegistry.parse(Data(registryShell.utf8))?.status, .other("shell"))
    check("a file written before its session had a status is not in any state",
          SessionRegistry.parse(Data(registryNewborn.utf8))?.status == nil)

    // The three fields a parked tab turns on. Nothing else in either file says the conversation
    // has moved, and everything else in the parked one keeps saying what was true before it did.
    let parked = SessionRegistry.parse(Data(registryParked.utf8))
    expect("a tab that has been parked names the job its conversation left for",
           parked?.parkedJobId, "1f47c762")
    expect("and still calls itself the kind of session somebody types into",
           parked?.kind, "interactive")
    check("a tab holds no job of its own", parked?.jobId == nil)
    expect("while its own fields go on describing the conversation that left",
           parked?.sessionID, "8967a1ee-9718-45ed-94d5-c81178870072")

    let background = SessionRegistry.parse(Data(registryBackground.utf8))
    expect("the session it moved into says which job it is", background?.jobId, "1f47c762")
    expect("and that nobody is watching it", background?.kind, "bg")
    expect("with a conversation, and a transcript, of its own", background?.sessionID,
           "1f47c762-60e9-4dc1-8967-fadb4038448c")
    expect("and a status forty minutes newer than the one the tab is stuck on",
           background?.status, .idle)
    check("an ordinary session is neither parked nor a job",
          busy?.parkedJobId == nil && busy?.jobId == nil)

    check("half a file is dropped",
          SessionRegistry.parse(Data(#"{"pid":10407,"sta"#.utf8)) == nil)
    check("a file with no pid in it is dropped",
          SessionRegistry.parse(Data(#"{"status":"waiting","peerProtocol":1}"#.utf8)) == nil)
    // Silence is the one answer a version gate must never read as agreement.
    expect("a file that states no protocol version states zero",
           SessionRegistry.parse(Data(#"{"pid":1,"status":"idle"}"#.utf8))?.peerProtocol, 0)
}

group("session registry: telling one process from the next one to get its number") {
    // `LC_ALL=C TZ=UTC ps -o lstart=`, which is the exact command Claude Code runs — so this is
    // one program's C-locale output being read by another, not a localised date being guessed at.
    let parsed = SessionRegistry.procStartDate("Tue Aug 25 06:21:43 2026")
    check("the format Claude Code writes parses", parsed != nil)
    expect("as the instant the file's own startedAt names, to the second",
           parsed.map { Int($0.timeIntervalSince1970) }, 1787638903)
    // `ps` pads a single-digit day with a second space, and that day comes round every month.
    check("a single-digit day, padded the way ps pads it, parses too",
          SessionRegistry.procStartDate("Tue Aug  5 06:21:43 2026") != nil)
    check("something that is not a date at all is nothing",
          SessionRegistry.procStartDate("just now") == nil)
    check("and neither is a missing field", SessionRegistry.procStartDate(nil) == nil)

    guard let entry = SessionRegistry.parse(Data(registryBusy.utf8)) else {
        check("the fixture parses", false)
        return
    }
    let real = SessionRegistry.procStartDate(entry.procStart)
    check("a process that started when the file says it did is the same process",
          SessionRegistry.isSameProcess(entry, startedAt: real))
    check("and still is a second or two out, which is all the two clocks can agree to",
          SessionRegistry.isSameProcess(entry, startedAt: real?.addingTimeInterval(2)))
    // The reason this exists: a dead session's file left behind, and its number handed to
    // somebody else's tab. Nothing else in the entry would say so.
    check("a process that started an hour later is a different process",
          !SessionRegistry.isSameProcess(entry, startedAt: real?.addingTimeInterval(3600)))
    check("a start time this Mac could not measure is not the benefit of the doubt",
          !SessionRegistry.isSameProcess(entry, startedAt: nil))
    let noStart = SessionRegistry.parse(Data(#"{"pid":10407,"status":"waiting","peerProtocol":1}"#.utf8))
    check("and neither is a file that does not say when its process started",
          noStart.map { !SessionRegistry.isSameProcess($0, startedAt: Date()) } ?? false)
}

group("session registry: what a file is allowed to change") {
    let session = registrySession("A")
    let other = registrySession("B")
    let sessions = [session, other]

    // Nothing installed, nothing running, an older Claude Code, one of the cloud backends: the
    // state every reading has to be right in, so it is the first check.
    expect("with no files at all, the screen is the whole answer",
           SessionRegistry.merge(SessionRegistry.Reading(), into: ["A": .idle],
                                 sessions: sessions)["A"],
           .idle)
    expect("and a session nobody found a file for is untouched",
           SessionRegistry.merge(registryReading("A", registryBusy), into: ["B": .idle],
                                 sessions: sessions)["B"],
           .idle)

    // 1. The whole point. A question no longer has to be *recognised* to be reported.
    expect("a session that says it is waiting is waiting, whatever the screen looked like",
           SessionRegistry.merge(registryReading("A", registryWaiting), into: ["A": .idle],
                                 sessions: sessions)["A"],
           .waiting)
    expect("including behind a spinner that was never erased",
           SessionRegistry.merge(registryReading("A", registryWaiting),
                                 into: ["A": .working("Cogitating… (7s)")],
                                 sessions: sessions)["A"],
           .waiting)
    expect("and on a screen that could not be read at all",
           SessionRegistry.merge(registryReading("A", registryWaiting), into: ["A": .unknown],
                                 sessions: sessions)["A"],
           .waiting)
    expect("the screen parser is told, so it can go and read the options",
           SessionRegistry.waiting(in: registryReading("A", registryWaiting), sessions: sessions),
           ["A"])

    // 2. The one place the screen still wins. A menu recognised on the terminal is stronger than
    // `busy`, because the registry can be a beat behind a dialog that has just been drawn — and
    // of the two ways to be wrong for that beat, only this one hides the row somebody must act on.
    expect("a menu found on screen is not overwritten by a session that says it is busy",
           SessionRegistry.merge(registryReading("A", registryBusy), into: ["A": .waiting],
                                 sessions: sessions)["A"],
           .waiting)
    expect("nor by one that says it is idle",
           SessionRegistry.merge(registryReading("A", registryIdle), into: ["A": .waiting],
                                 sessions: sessions)["A"],
           .waiting)

    // The two seconds before Claude Code draws its live line, which the screen alone reads as
    // an idle session — and the stale line after a fast turn, which it reads as a busy one.
    expect("a session that says it is busy is working, before it has drawn anything",
           SessionRegistry.merge(registryReading("A", registryBusy), into: ["A": .idle],
                                 sessions: sessions)["A"],
           .working(""))
    expect("and keeps the sentence the terminal already showed, clock and all",
           SessionRegistry.merge(registryReading("A", registryBusy),
                                 into: ["A": .working("Cogitating… (7s)")],
                                 sessions: sessions)["A"],
           .working("Cogitating… (7s)"))
    expect("a session that says it is idle beats a spinner left on the screen",
           SessionRegistry.merge(registryReading("A", registryIdle),
                                 into: ["A": .working("Cogitating… (7s)")],
                                 sessions: sessions)["A"],
           .idle)

    // 3. The one explicit version number anything in this app reads.
    let future = registryBusy.replacingOccurrences(of: #""peerProtocol":1"#,
                                                   with: #""peerProtocol":2"#)
    expect("a file speaking a protocol this build was not written against is ignored whole",
           SessionRegistry.merge(registryReading("A", future), into: ["A": .idle],
                                 sessions: sessions)["A"],
           .idle)

    // 4. `shell`, and every word after it that nobody has written down yet.
    expect("a status this build does not know decides nothing",
           SessionRegistry.merge(registryReading("A", registryShell),
                                 into: ["A": .working("Cogitating… (7s)")],
                                 sessions: sessions)["A"],
           .working("Cogitating… (7s)"))
    expect("and neither does a file written before its session had one",
           SessionRegistry.merge(registryReading("A", registryNewborn), into: ["A": .idle],
                                 sessions: sessions)["A"],
           .idle)
    expect("an unknown status does not open the parsing gate either",
           SessionRegistry.waiting(in: registryReading("A", registryShell), sessions: sessions),
           [])

    // 5. The leftover file. Without this a dead session's number, handed out again, paints a
    // stranger's state onto somebody's tab — and nothing on the screen would contradict it.
    expect("a file about a process that started an hour earlier is thrown away",
           SessionRegistry.merge(registryReading("A", registryWaiting, drift: 3600),
                                 into: ["A": .idle], sessions: sessions)["A"],
           .idle)
    expect("and so is one this Mac could not check at all",
           SessionRegistry.merge(registryReading("A", registryWaiting, measured: false),
                                 into: ["A": .idle], sessions: sessions)["A"],
           .idle)

    // 7. Codex, without a line of code naming Codex. It writes no file, so there is no pid in
    // the reading for it, so every gate above is already closed.
    let codex = registrySession("C", assistant: .codex)
    expect("a Codex session falls back to its screen, because nothing here is about it",
           SessionRegistry.merge(registryReading("A", registryWaiting), into: ["C": .idle],
                                 sessions: [codex])["C"],
           .idle)
}

group("session registry: a conversation that has moved to the background") {
    let session = registrySession("A")
    let sessions = [session]

    // The measurement this exists for: the phone showed a conversation's last message at 17:05:29
    // while the conversation itself was at 17:31. Every gate in `entry(for:in:)` passed — same
    // process, same pid, same start time — and what came back was a stopped clock.
    expect("a parked tab answers with the conversation that is actually running",
           SessionRegistry.entry(for: "A", in: registryParkedReading("A"))?.sessionID,
           "1f47c762-60e9-4dc1-8967-fadb4038448c")
    expect("and with that conversation's status, not the one frozen at the move",
           SessionRegistry.entry(for: "A", in: registryParkedReading("A"))?.status, .idle)
    // The tab's own file says `busy`. Left to speak, it paints a spinner on a session that has
    // been sitting at an empty prompt for half an hour.
    expect("so a spinner left on the screen is cleared by the session that is really there",
           SessionRegistry.merge(registryParkedReading("A"),
                                 into: ["A": .working("Cogitating… (7s)")],
                                 sessions: sessions)["A"],
           .idle)
    let asking = registryBackground.replacingOccurrences(of: #""status":"idle""#,
                                                         with: #""status":"waiting""#)
    expect("and a question asked in the background reaches the phone",
           SessionRegistry.merge(registryParkedReading("A", live: asking), into: ["A": .idle],
                                 sessions: sessions)["A"],
           .waiting)
    expect("through the same gate it opens for any other session",
           SessionRegistry.waiting(in: registryParkedReading("A", live: asking),
                                   sessions: sessions),
           ["A"])

    // The safety net, and the point of the whole change. A frozen file is worse than no file:
    // no file loses to the hook note and to the tab title, both of which are about the
    // conversation that is running, while a frozen one outranks them and cannot be argued with.
    check("a parked tab whose background session cannot be found answers nothing",
          SessionRegistry.entry(for: "A", in: registryParkedReading("A", live: nil)) == nil)
    check("nor does it answer with the id of the transcript that stops mid-sentence",
          SessionRegistry.entry(for: "A", in: registryParkedReading("A", live: nil))?.sessionID
              == nil)
    // This is the shape of the harm, in one line: the tab froze at `busy`, the screen can see
    // perfectly well what is happening, and a stopped clock must not be allowed to overrule it.
    expect("and it leaves the screen to say what it can see",
           SessionRegistry.merge(registryParkedReading("A", live: nil),
                                 into: ["A": .working("Cogitating… (7s)")],
                                 sessions: sessions)["A"],
           .working("Cogitating… (7s)"))
    // The same thing in the direction nobody would notice. Frozen at `idle`, this used to repaint
    // a session that was working as one doing nothing at all — no dot in the menu bar, no notch,
    // no live line — and hand `SessionWatch` a transition that fired an external "finished" hook
    // for a turn that had not finished.
    let frozenIdle = registryParked.replacingOccurrences(of: #""status":"busy""#,
                                                         with: #""status":"idle""#)
    expect("in the other direction too, which is the quiet one nobody would notice",
           SessionRegistry.merge(registryReading("A", frozenIdle),
                                 into: ["A": .working("Cogitating… (7s)")],
                                 sessions: sessions)["A"],
           .working("Cogitating… (7s)"))
    expect("and it opens no parsing gate on a question asked before the move",
           SessionRegistry.waiting(in: registryReading("A", registryParked
               .replacingOccurrences(of: #""status":"busy""#, with: #""status":"waiting""#)),
                                   sessions: sessions),
           [])

    // Nothing prunes this directory, so the background session named by a tab parked last week
    // is a file about a pid that now belongs to something else entirely. The same check that
    // stops a leftover file painting a stranger's state onto a tab stops this.
    check("a background file about a process that started an hour later is not that job",
          SessionRegistry.entry(for: "A",
                                in: registryParkedReading("A", drift: 3600)) == nil)
    check("and neither is one this Mac could not check at all",
          SessionRegistry.entry(for: "A",
                                in: registryParkedReading("A", measured: false)) == nil)
    let future = registryBackground.replacingOccurrences(of: #""peerProtocol":1"#,
                                                         with: #""peerProtocol":2"#)
    check("a background file speaking a protocol this build does not know is ignored whole",
          SessionRegistry.entry(for: "A", in: registryParkedReading("A", live: future)) == nil)
    // Filed under the right job by a caller that got it wrong, or by a file that was rewritten:
    // the entry has to say the job itself, or the two are only sharing a dictionary key.
    check("a file filed under a job it does not claim is not that job either",
          SessionRegistry.entry(for: "A",
                                in: registryParkedReading("A", live: registrySpare,
                                                          job: "1f47c762")) == nil)
    check("and neither is a tab that happens to be filed there",
          SessionRegistry.entry(for: "A",
                                in: registryParkedReading("A", live: registryBusy,
                                                          job: "1f47c762")) == nil)

    // The ordinary case, which must not have moved: a tab nobody has parked is still its own
    // answer, and none of the above runs for it.
    expect("a session that was never parked is unaffected by any of this",
           SessionRegistry.entry(for: "A", in: registryReading("A", registryBusy))?.sessionID,
           "f0108a4c-18fc-48fd-98f5-46016169a6a8")
    var unrelated = registryReading("A", registryBusy)
    if let bg = SessionRegistry.parse(Data(registryBackground.utf8)) {
        unrelated.entries[bg.pid] = bg
        unrelated.background["1f47c762"] = SessionRegistry.Process(
            pid: bg.pid, started: SessionRegistry.procStartDate(bg.procStart))
    }
    expect("and a background session no tab is pointing at decides nothing",
           SessionRegistry.merge(unrelated, into: ["A": .idle], sessions: sessions)["A"],
           .working(""))
}

group("session registry: finding the session a parked tab moved into") {
    let dir = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-parked-test-\(getpid())")
    try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: dir) }

    // The pid in each file name is checked against the running process before the file is read,
    // so the fixtures are written under this test's own pid and one number that cannot be alive.
    let mine = getpid()
    let dead: Int32 = 999_999
    func file(_ pid: Int32, _ json: String) {
        let body = json.replacingOccurrences(of: "\"pid\":\(SessionRegistry.parse(Data(json.utf8))?.pid ?? 0)",
                                             with: "\"pid\":\(pid)")
        try? Data(body.utf8).write(to: dir.appendingPathComponent("\(pid).json"))
    }
    file(mine, registryBackground)
    file(dead, registrySpare)

    var measured: [Int32] = []
    func attach(_ reading: SessionRegistry.Reading) -> SessionRegistry.Reading {
        var out = reading
        measured = []
        SessionRegistry.attachBackground(to: &out, in: dir) { pid in
            measured.append(pid)
            return Date()
        }
        return out
    }

    // Nothing is parked, so there is nothing to look for — and the directory is never opened.
    // This is the whole cost argument: a listing that only happens when somebody has parked
    // something, rather than a `claude agents --json` on a path the poll walks every 1.2 seconds.
    let plain = attach(registryReading("A", registryBusy))
    check("with nothing parked, nothing is looked for", plain.background.isEmpty)
    check("and no process is asked about", measured.isEmpty)

    let found = attach(registryReading("A", registryParked))
    expect("a parked tab finds the background session claiming its job",
           found.background["1f47c762"]?.pid, mine)
    expect("whose file joins the reading like any other", found.entries[mine]?.jobId, "1f47c762")
    expect("and whose process is asked about exactly once", measured, [mine])
    check("the spare worker sitting next to it in the directory is not it",
          found.background["84e9b9f1"] == nil)
    check("and the tab's own file is still there", found.entries[66274] != nil)

    // Nothing prunes this directory, so the file of a background session that ended last week is
    // still in it, still claiming its job, and still pointed at by a tab parked at the same time.
    // Written under a number no process can have, which is what the kernel is asked about before
    // anything is read.
    try? FileManager.default.removeItem(at: dir.appendingPathComponent("\(mine).json"))
    file(dead, registryBackground)
    let gone = attach(registryReading("A", registryParked))
    check("a job whose session has ended is not found by its leftover file",
          gone.background["1f47c762"] == nil)
    check("and that file is not read at all — the kernel is asked first, and it is cheaper",
          measured.isEmpty)

    var absent = registryReading("A", registryParked)
    SessionRegistry.attachBackground(to: &absent,
                                     in: dir.appendingPathComponent("nope")) { _ in Date() }
    check("a directory that is not there reads as no background session at all",
          absent.background.isEmpty)
    check("and the reading it was handed comes back as it went in", absent.entries.count == 1)
}

group("session registry: which files get read, and when a change is worth a look") {
    let dir = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-registry-test-\(getpid())")
    try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: dir) }

    // 6. The degrade that matters most: an older Claude Code, a cloud backend, a container.
    // There is no directory, and the answer is nothing rather than an error.
    expect("a directory that is not there reads as no sessions at all",
           SessionRegistry.entries(in: dir.appendingPathComponent("nope"), pids: [10407]).count, 0)

    try? Data(registryBusy.utf8).write(to: dir.appendingPathComponent("10407.json"))
    try? Data(registryWaiting.utf8).write(to: dir.appendingPathComponent("73886.json"))
    expect("a file is found by the pid that names it",
           SessionRegistry.entries(in: dir, pids: [10407])[10407]?.status, .busy)
    expect("and a pid with no file is simply absent",
           SessionRegistry.entries(in: dir, pids: [10407, 99999]).count, 1)
    // The directory is never pruned, so it holds every session the machine has ever run. Reading
    // it by name is what keeps the cost proportional to the tabs open rather than to the history.
    expect("a file nobody asked about is not read",
           SessionRegistry.entries(in: dir, pids: [10407])[73886]?.status, nil)
    // Belt for a file that has been moved or renamed by hand: the name and the contents have to
    // agree about which process this is, or it is not a fact about anything.
    try? Data(registryBusy.utf8).write(to: dir.appendingPathComponent("55555.json"))
    expect("a file whose contents disagree with its own name is dropped",
           SessionRegistry.entries(in: dir, pids: [55555]).count, 0)

    // What the directory watcher compares. Every session on the machine writes here at every
    // turn boundary it has, and a reading costs a round trip to every terminal — so the clocks
    // that move on every write must not count as a change.
    let before = SessionRegistry.entries(in: dir, pids: [10407, 73886])
    let touched = registryBusy.replacingOccurrences(of: #""updatedAt":1787638966573"#,
                                                    with: #""updatedAt":1787638999999"#)
    try? Data(touched.utf8).write(to: dir.appendingPathComponent("10407.json"))
    let after = SessionRegistry.entries(in: dir, pids: [10407, 73886])
    check("a file rewritten with the same status is not worth a terminal round trip",
          SessionRegistry.statuses(after) == SessionRegistry.statuses(before))
    let moved = registryBusy.replacingOccurrences(of: #""status":"busy""#,
                                                  with: #""status":"waiting""#)
    try? Data(moved.utf8).write(to: dir.appendingPathComponent("10407.json"))
    check("a session that has stopped for a question is",
          SessionRegistry.statuses(SessionRegistry.entries(in: dir, pids: [10407, 73886]))
              != SessionRegistry.statuses(before))
}

group("waiting for a subprocess does not run anything else on this thread") {
    // `waitUntilExit()` polls the current run loop, so on the main thread it lets timers and
    // queued blocks run *inside* the wait — which is how one walk of the orchestrator's task list
    // came to start inside another one's. This is the invariant that stopped it, and it is worth a
    // real subprocess rather than a mock: the behaviour being pinned belongs to Foundation, not to
    // anything written here.
    var fired = 0
    let ticker = Timer(timeInterval: 0.05, repeats: true) { _ in fired += 1 }
    RunLoop.main.add(ticker, forMode: .common)
    defer { ticker.invalidate() }

    let p = Process()
    p.executableURL = URL(fileURLWithPath: "/bin/sleep")
    p.arguments = ["0.4"]
    let started = Date()
    try? p.run()
    p.waitQuietly()
    let waited = Date().timeIntervalSince(started)

    check("nothing on the main run loop ran while the wait was in progress", fired == 0,
          "a timer fired \(fired) times, so the wait is polling the run loop again")
    // Without these two the check above passes just as well when the wait returns immediately,
    // which is the way this test would go quietly useless.
    check("and it really waited", waited >= 0.35, "returned after \(waited)s")
    check("and the process was reaped", !p.isRunning)

    // The first version of this waited on a thread borrowed from the global pool, which is where
    // its own caller usually already is. That is a deadlock the moment the pool is full — the
    // waiter is holding a thread the block it waits for needs — and it stopped every reading in
    // the app for an afternoon. So the pool is filled here on purpose before the wait is asked
    // for: it has to finish while there is nothing of that pool left to lend.
    let held = DispatchSemaphore(value: 0)
    let crowd = 70
    for _ in 0..<crowd { DispatchQueue.global(qos: .userInitiated).async { held.wait() } }
    Thread.sleep(forTimeInterval: 0.4)

    let starved = Process()
    starved.executableURL = URL(fileURLWithPath: "/bin/sleep")
    starved.arguments = ["0.2"]
    let returned = DispatchSemaphore(value: 0)
    Thread { try? starved.run(); starved.waitQuietly(); returned.signal() }.start()
    let outcome = returned.wait(timeout: .now() + 8)
    for _ in 0..<crowd { held.signal() }

    check("and it still returns when the global queue has no threads left to lend",
          outcome == .success, "the wait did not come back within eight seconds")
}

group("a background command is over when its own output says so") {
    let fm = FileManager.default
    let dir = fm.temporaryDirectory
        .appendingPathComponent("clawdline-shells-\(UUID().uuidString)", isDirectory: true)
    try! fm.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? fm.removeItem(at: dir) }

    /// One output file, read once. The signature is the caller's business in the app — file size
    /// and mtime — and a fresh name per case here so no test can be answered from another's cache.
    func reading(_ name: String, _ text: String, signature: String = "1") -> Shells.Ending {
        let file = dir.appendingPathComponent(name)
        try! text.write(to: file, atomically: true, encoding: .utf8)
        return Shells.read(file, signature: signature)
    }

    // The shape Claude Code actually writes, taken off a real run: the command's output, a blank
    // line, and the marker on its own at the end.
    let done = reading("done.output", "start\ndone\n\n[exited with code 0]\n")
    check("a command that exited has ended", done.ended)
    check("and nothing of it is reported as a live line", done.last == nil)

    // A build half way through. This is the state the whole feature is for.
    let going = reading("going.output", "[6/22] Compiling Clawdline Strings.swift\n"
                        + "[7/22] Compiling Clawdline Panel.swift\n")
    check("a command with no marker under it is still running", !going.ended)
    expect("and its last line is what it last printed", going.last ?? "",
           "[7/22] Compiling Clawdline Panel.swift")

    // `sleep 600 &`. The file exists from the moment the command starts and stays empty, which is
    // the honest answer rather than a missing one: something is running and it has said nothing.
    let quiet = reading("quiet.output", "")
    check("a command that has printed nothing is still running", !quiet.ended)
    check("and it has no line to show for itself", quiet.last == nil)

    // **Only the last line decides.** A test suite printing its own bracketed lines, a log that
    // quotes a previous run — the marker is a thing written under a finished command, and a
    // command that is still printing has not finished however its output reads.
    let quoting = reading("quoting.output", "[exited with code 0]\nand then it carried on\n")
    check("a line that looks like the marker earlier in the file decides nothing", !quoting.ended)
    expect("the real last line is still the live one", quoting.last ?? "",
           "and then it carried on")

    // The other ending Claude Code writes, for a command that was stopped rather than one that
    // ended on its own. Seven of the 114 output files on the machine this was written against.
    let killed = reading("killed.output", "serving on :3000\n[killed]\n")
    check("a command that was killed has ended", killed.ended)

    // And the reason the rule reads the words rather than the brackets: one real output file ends
    // with a Chrome log line, which is bracketed and is not an ending.
    let logging = reading("logging.output",
                          "[53966:5720383:0824/124336.389973:ERROR:x.cc:618] Network service crashed\n")
    check("a bracketed log line is not an ending", !logging.ended)

    // Trailing blank lines are whitespace, not output.
    let padded = reading("padded.output", "ok\n[exited with code 1]\n\n\n")
    check("blank lines under the marker do not hide it", padded.ended)

    // The tail is all that is read, so a command that has printed a megabyte still ends properly.
    let long = String(repeating: "line of build output\n", count: 60_000)
    check("the marker at the end of a large file is still found",
          reading("long.output", long + "[exited with code 0]\n").ended)
    check("and a large file with no marker is still running",
          !reading("longer.output", long).ended)

    // Read once per version of the file. Same signature, different bytes: the answer that was
    // already worked out is the one that comes back, which is what stops six output files being
    // re-read once a second for a session that has not printed anything.
    let path = "cached.output"
    _ = reading(path, "still going\n", signature: "same")
    let again = reading(path, "finished\n[exited with code 0]\n", signature: "same")
    check("a file whose signature has not moved is not read again", !again.ended)
    let moved = reading(path, "finished\n[exited with code 0]\n", signature: "moved")
    check("and one whose signature has is", moved.ended)

    // A command can print a hundred and fifty kilobytes without a newline in it — `curl` of an
    // ordinary page does — and the first live reading of this put the whole of one on the wire.
    let wide = reading("wide.output", String(repeating: "x", count: 4000) + "\n")
    check("a line longer than a phone is cut", (wide.last ?? "").count <= 160)
    check("and cut with a mark on it", (wide.last ?? "").hasSuffix("…"))
}

group("a file left behind by an interrupted command is not a command still running") {
    let fm = FileManager.default
    let dir = fm.temporaryDirectory
        .appendingPathComponent("clawdline-announced-\(UUID().uuidString)", isDirectory: true)
    try! fm.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? fm.removeItem(at: dir) }

    // Two records off a real transcript, trimmed to what is read: the sentence Claude Code answers
    // a backgrounded `Bash` with, and an ordinary message that says nothing of the kind.
    let started = #"{"type":"user","message":{"content":[{"tool_use_id":"toolu_01FU5QVdeqA1fDXfF254GFnk","type":"tool_result","content":"Command running in background with ID: bvlp3xmku. Output is being written to: /private/tmp/claude-501/x/y/tasks/bvlp3xmku.output. You will be notified when it completes."}]}}"#
    let ordinary = #"{"type":"assistant","message":{"content":[{"type":"text","text":"Building for debugging..."}]}}"#
    // The assistant's side of the same command, which carries what was actually asked for and
    // points at the reply above through `tool_use_id`. It comes first in an append-only file.
    let asked = #"{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_01FU5QVdeqA1fDXfF254GFnk","name":"Bash","input":{"command":"swift build 2>&1 | tail -30","description":"Compile the package","run_in_background":true}}]}}"#

    let file = dir.appendingPathComponent("transcript.jsonl")
    try! (asked + "\n" + ordinary + "\n" + started + "\n")
        .write(to: file, atomically: true, encoding: .utf8)
    let first = Shells.announced(in: file)
    check("the id of a backgrounded command is found", first["bvlp3xmku"] != nil)
    expect("and nothing else is", first.count, 1)
    // Joined to the call that started it, which is the only place the command line exists.
    expect("the command it was asked to run comes with it",
           first["bvlp3xmku"]?.command ?? "", "swift build 2>&1 | tail -30")
    expect("and the description written beside it",
           first["bvlp3xmku"]?.what ?? "", "Compile the package")

    // The whole point of the offset: a session that has been going for ninety minutes has pushed
    // that announcement far past the end of any window a tail could afford to read.
    let filler = String(repeating: ordinary + "\n", count: 400)
    let second = #"{"type":"user","message":{"content":[{"tool_use_id":"toolu_0139kcvC1WTpjRsFSKeU9KWC","type":"tool_result","content":"Command running in background with ID: bke4ijfjf. Output is being written to: /private/tmp/claude-501/x/y/tasks/bke4ijfjf.output."}]}}"#
    let handle = try! FileHandle(forWritingTo: file)
    try! handle.seekToEnd()
    try! handle.write(contentsOf: Data((filler + second + "\n").utf8))
    try! handle.close()

    let grown = Shells.announced(in: file)
    check("an announcement in the new bytes is found too", grown["bke4ijfjf"] != nil)
    check("and the one before them is still remembered", grown["bvlp3xmku"] != nil)
    // Its own call is not in this file at all, so the join cannot be made. The id is what decides
    // whether anything is running and it survives; the command line is what is lost.
    expect("an announcement with no call to join to still counts",
           grown["bke4ijfjf"]?.command ?? "", "")

    // A half-written record at the end is not read now and not skipped forever: the scan stops at
    // the last newline, so the next call starts where this one stopped.
    let partial = try! FileHandle(forWritingTo: file)
    try! partial.seekToEnd()
    try! partial.write(contentsOf: Data(#"{"type":"user","message":{"content":[{"type":"tool_result","content":"Command running in background with ID: bhal"#.utf8))
    try! partial.close()
    check("half a record at the end is not read", Shells.announced(in: file)["bhal"] == nil)

    // A file that has shrunk was replaced rather than appended to, so the offset means nothing.
    try! ordinary.appending("\n").write(to: file, atomically: true, encoding: .utf8)
    check("a replaced transcript is read again from the start",
          Shells.announced(in: file).isEmpty)

    check("and a transcript that does not exist announces nothing",
          Shells.announced(in: dir.appendingPathComponent("gone.jsonl")).isEmpty)
}

group("nothing is signalled until it is known whose process it is") {
    // The shape off a real machine: Claude Code (13655) started `zsh -c …` (86319), which is the
    // group leader, and the `sleep` inside it (37626) is in that same group. Both hold the
    // command's output file open, and only the shell is a child of the session.
    let real: [Int32: (parent: Int32, group: Int32)] = [
        37626: (parent: 86319, group: 86319),
        86319: (parent: 13655, group: 86319),
    ]
    func lineage(_ map: [Int32: (parent: Int32, group: Int32)]) -> (Int32) -> (parent: Int32, group: Int32)? {
        { map[$0] }
    }

    expect("the group signalled is the shell the session started",
           Shells.group(among: [37626, 86319], under: 13655, avoiding: 13655,
                        lineage: lineage(real)) ?? -1, 86319)
    // The holders come off `lsof` in whatever order it lists them; the descendant is first here.
    expect("and finding it does not depend on the order lsof listed them",
           Shells.group(among: [86319, 37626], under: 13655, avoiding: 13655,
                        lineage: lineage(real)) ?? -1, 86319)

    // Somebody else's session. The file is open, the processes are real, and none of them is
    // this session's to stop.
    check("a holder belonging to another session is not ours to signal",
          Shells.group(among: [37626, 86319], under: 99999, avoiding: 99999,
                       lineage: lineage(real)) == nil)

    check("nothing holding it open is nothing to signal",
          Shells.group(among: [], under: 13655, avoiding: 13655, lineage: lineage(real)) == nil)

    check("a process ps has no answer for is not signalled",
          Shells.group(among: [55555], under: 13655, avoiding: 13655,
                       lineage: lineage(real)) == nil)

    // **The three that would take something else down with the command.** A group of 0 is every
    // process in the session, 1 is init, and Claude Code's own group is the session itself —
    // which is the one mistake here that would end somebody's conversation to stop their build.
    for (name, group) in [("everything", Int32(0)), ("init", Int32(1)),
                          ("the session itself", Int32(13655))] {
        let bad: [Int32: (parent: Int32, group: Int32)] = [86319: (parent: 13655, group: group)]
        check("a command whose group is \(name) is refused, not signalled",
              Shells.group(among: [86319], under: 13655, avoiding: 13655,
                           lineage: lineage(bad)) == nil)
    }
    // And the same when Claude Code's own group is not its pid — which is what a session started
    // from a shell script looks like.
    let shared: [Int32: (parent: Int32, group: Int32)] = [86319: (parent: 13655, group: 4242)]
    check("nor is the group Claude Code itself is in",
          Shells.group(among: [86319], under: 13655, avoiding: 4242,
                       lineage: lineage(shared)) == nil)
}
}
