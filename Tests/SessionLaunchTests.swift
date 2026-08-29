import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - Starting a session from somewhere else

/// A request with nothing in it but what the test is about. `Host` is always right, because the
/// rebinding refusal comes before everything else and a wrong one would make every case below
/// pass for the wrong reason.
func remoteRequest(_ method: String, _ target: String,
                   headers: [String: String] = [:],
                   body: String? = nil) -> RemoteServer.Request {
    var head = "\(method) \(target) HTTP/1.1\r\nHost: 127.0.0.1:\(Config.shared.remotePort)\r\n"
    for (key, value) in headers.sorted(by: { $0.key < $1.key }) { head += "\(key): \(value)\r\n" }
    var request = RemoteServer.Request(head: Data((head + "\r\n").utf8))!
    // Set rather than appended to the head: the reader assembles the body from the socket, so a
    // test that wrote one into the head would be exercising a parse no real request takes.
    if let body { request.body = Data(body.utf8) }
    return request
}

/// The `code` out of an error envelope, and "" for anything that is not one.
func remoteErrorCode(_ response: RemoteServer.Response) -> String {
    let body = (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]
    return ((body?["error"] as? [String: Any])?["code"] as? String) ?? ""
}

/// The `message` out of one. Only worth asking when two refusals share a code and differ in
/// what they were about — which is the case for the two 404s on the start route.
func remoteErrorMessage(_ response: RemoteServer.Response) -> String {
    let body = (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]
    return ((body?["error"] as? [String: Any])?["message"] as? String) ?? ""
}

func runSessionLaunchTests() {
group("the expensive remote reads take exactly one bounded side door") {
    func slow(_ path: String) -> Bool { RemoteServer.isSlowReading(path) }
    func limited(_ path: String) -> Bool { RemoteServer.isLimitedSlowReading(path) }

    check("the places list leaves the shared server queue", slow("/v1/places"))
    check("session info leaves it", slow("/v1/sessions/ABC/info"))
    check("the session transcript leaves it", slow("/v1/sessions/ABC/transcript"))
    check("an encoded session id is still one segment", slow("/v1/sessions/A%2FB/info"))

    check("a places subroute is not selected", !slow("/v1/places/anything"))
    check("the sessions list is not selected", !slow("/v1/sessions"))
    check("an empty session id is not selected", !slow("/v1/sessions//info"))
    check("an agent transcript is not the session transcript",
          !slow("/v1/sessions/ABC/agents/child/transcript"))
    check("a deeper route with the same suffix is not selected",
          !slow("/v1/sessions/ABC/anything/info"))
    check("a trailing slash is not selected", !slow("/v1/sessions/ABC/info/"))

    check("places consumes the shared depth", limited("/v1/places"))
    check("info consumes it", limited("/v1/sessions/ABC/info"))
    check("transcript deliberately does not",
          !limited("/v1/sessions/ABC/transcript"))
    check("nor does an unrelated path", !limited("/v1/health"))
}

group("the slow-reading gate agrees with dispatch") {
    let request = remoteRequest("GET", "/v1/places")
    let atDoor = RemoteServer.shared.slowReadingRefusal(request)
    let throughDispatch = RemoteServer.shared.route(request)

    expect("the side door refuses an unpaired reader with 401", atDoor?.status, 401)
    expect("dispatch gives the same status", throughDispatch.status, atDoor?.status)
    expect("both refusals use the same code", remoteErrorCode(throughDispatch),
           atDoor.map(remoteErrorCode) ?? "")
    expect("and the same sentence", remoteErrorMessage(throughDispatch),
           atDoor.map(remoteErrorMessage) ?? "")
}

group("the slow-reading depth is paired on every exit") {
    let info = "/v1/sessions/ABC/info"

    var refused = RemoteServer.ReadingLimiter()
    for n in 0..<RemoteServer.readingDepth {
        check("bounded reading \(n + 1) is admitted",
              refused.admit(info, depth: RemoteServer.readingDepth))
    }
    let full = refused.count
    check("the next bounded reading is refused",
          !refused.admit("/v1/places", depth: RemoteServer.readingDepth))
    expect("refusal did not increment the counter", refused.count, full)
    for _ in 0..<RemoteServer.readingDepth { refused.finish(info) }
    expect("the admitted work can all leave", refused.count, 0)

    var normal = RemoteServer.ReadingLimiter()
    check("an ordinary reading enters", normal.admit(info, depth: RemoteServer.readingDepth))
    normal.finish(info)
    expect("an ordinary answer leaves no count behind", normal.count, 0)

    var interrupted = RemoteServer.ReadingLimiter()
    check("a reading whose connection will close still enters",
          interrupted.admit(info, depth: RemoteServer.readingDepth))
    // The worker still completes after its NWConnection is cancelled. `readSlowly` calls this
    // before attempting to send, so interruption changes the write and not the accounting.
    interrupted.finish(info)
    expect("an interrupted connection leaves no count behind", interrupted.count, 0)

    var transcript = RemoteServer.ReadingLimiter()
    for n in 0..<(RemoteServer.readingDepth + 2) {
        check("transcript refresh \(n + 1) is never refused by the shared depth",
              transcript.admit("/v1/sessions/ABC/transcript",
                               depth: RemoteServer.readingDepth))
        transcript.finish("/v1/sessions/ABC/transcript")
    }
    expect("transcript never changes the bounded count", transcript.count, 0)
}

group("terminal writes cannot hold the remote server queue") {
    let wasWriting = Config.shared.remoteWrite
    defer {
        Config.shared.remoteWrite = wasWriting
        RemoteServer.sessionPayloadForTesting = nil
        RemoteServer.terminalSendForTesting = nil
        RemoteServer.terminalRouteForTesting = nil
        ITerm.completeInventoryForTesting()
    }
    Config.shared.remoteWrite = true
    ITerm.completeInventoryForTesting()
    let writer = RemoteAuth.addDevice(name: "terminal queue test", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: writer.id) }

    let session = TargetSession(backend: .iterm, id: "BLOCKED-TERMINAL", name: "x",
                                tty: "/dev/ttys77", windowIndex: 0, tabIndex: 0,
                                assistant: .codex)
    RemoteServer.sessionPayloadForTesting = ([session], [:])

    let entered = DispatchSemaphore(value: 0)
    let release = DispatchSemaphore(value: 0)
    let responses = DispatchSemaphore(value: 0)
    let responseLock = NSLock()
    let sentLock = NSLock()
    var sent = 0
    var received: [RemoteServer.Response] = []
    RemoteServer.terminalSendForTesting = { _, _ in
        sentLock.lock(); sent += 1; sentLock.unlock()
        entered.signal()
        _ = release.wait(timeout: .now() + 2)
        return nil
    }

    let key = UUID().uuidString
    let request = remoteRequest(
        "POST", "/v1/sessions/\(session.id)/send",
        headers: ["Authorization": "Bearer \(writer.token)", "Idempotency-Key": key],
        body: "{\"text\":\"once\"}")
    for _ in 0..<2 {
        RemoteServer.shared.sendTerminalForTesting(request) { response in
            responseLock.lock(); received.append(response); responseLock.unlock()
            responses.signal()
        }
    }
    check("the first terminal command actually entered its isolated queue",
          entered.wait(timeout: .now() + 1) == .success)

    let health = DispatchSemaphore(value: 0)
    let heartbeat = DispatchSemaphore(value: 0)
    let slowRead = DispatchSemaphore(value: 0)
    let started = Date()
    RemoteServer.shared.routeOnServerQueueForTesting(remoteRequest("GET", "/v1/health")) {
        if $0.status == 200 { health.signal() }
    }
    RemoteServer.shared.heartbeatTurnForTesting { heartbeat.signal() }
    RemoteServer.shared.readingTurnForTesting { slowRead.signal() }
    check("health finishes while terminal I/O is blocked",
          health.wait(timeout: .now() + 0.25) == .success)
    check("the heartbeat queue turn finishes while terminal I/O is blocked",
          heartbeat.wait(timeout: .now() + 0.25) == .success)
    check("a slow-read worker turn finishes while terminal I/O is blocked",
          slowRead.wait(timeout: .now() + 0.25) == .success)
    check("the three unrelated turns stayed inside the small bound",
          Date().timeIntervalSince(started) < 0.5)

    release.signal()
    check("the original request settles", responses.wait(timeout: .now() + 1) == .success)
    check("the concurrent retry settles with it", responses.wait(timeout: .now() + 1) == .success)
    responseLock.lock(); let settled = received; responseLock.unlock()
    sentLock.lock(); let sentCount = sent; sentLock.unlock()
    expect("the same in-flight key executes terminal input exactly once", sentCount, 1)
    expect("both waiters receive an answer", settled.count, 2)
    check("both waiters receive the same final response",
          settled.count == 2 && settled[0].status == settled[1].status
              && settled[0].body == settled[1].body)

    RemoteServer.terminalSendForTesting = { _, _ in
        ITerm.blockAutomationForTesting("iTerm2 is waiting for a dialog")
        return ITerm.automationAttention
    }
    let failed = DispatchSemaphore(value: 0)
    var failure: RemoteServer.Response?
    let failedRequest = remoteRequest(
        "POST", "/v1/sessions/\(session.id)/send",
        headers: ["Authorization": "Bearer \(writer.token)",
                  "Idempotency-Key": UUID().uuidString],
        body: "{\"text\":\"timeout\"}")
    RemoteServer.shared.sendTerminalForTesting(failedRequest) {
        failure = $0; failed.signal()
    }
    check("a 15-second-equivalent terminal failure settles",
          failed.wait(timeout: .now() + 1) == .success)
    expect("the terminal failure is a bad-gateway response", failure?.status, 502)
    expect("with an actionable typed code", failure.map(remoteErrorCode),
           "iterm_attention_required")

    let tmux = TargetSession(backend: .tmux, id: "%88", name: "tmux",
                             tty: "/dev/ttys88", windowIndex: 0, tabIndex: 0,
                             assistant: .codex)
    RemoteServer.sessionPayloadForTesting = ([session, tmux], [:])
    RemoteServer.terminalSendForTesting = { _, _ in "tmux pane disappeared" }
    let tmuxFailed = DispatchSemaphore(value: 0)
    var tmuxFailure: RemoteServer.Response?
    let validTmuxRequest = remoteRequest(
        "POST", "/v1/sessions/%2588/send",
        headers: ["Authorization": "Bearer \(writer.token)",
                  "Idempotency-Key": UUID().uuidString], body: "{\"text\":\"hello\"}")
    RemoteServer.shared.sendTerminalForTesting(validTmuxRequest) {
        tmuxFailure = $0; tmuxFailed.signal()
    }
    check("a tmux failure settles while the iTerm circuit is open",
          tmuxFailed.wait(timeout: .now() + 1) == .success)
    expect("tmux keeps its backend-aware error", tmuxFailure.map(remoteErrorCode),
           "terminal_io_failed")

    ITerm.completeInventoryForTesting()
    RemoteServer.terminalSendForTesting = { _, _ in
        ITerm.blockAutomationForTesting("an unrelated iTerm operation opened a dialog")
        return "ps failed while inspecting the tty"
    }
    let unrelatedFailed = DispatchSemaphore(value: 0)
    var unrelatedFailure: RemoteServer.Response?
    let unrelatedRequest = remoteRequest(
        "POST", "/v1/sessions/\(session.id)/send",
        headers: ["Authorization": "Bearer \(writer.token)",
                  "Idempotency-Key": UUID().uuidString], body: "{\"text\":\"inspect\"}")
    RemoteServer.shared.sendTerminalForTesting(unrelatedRequest) {
        unrelatedFailure = $0; unrelatedFailed.signal()
    }
    check("an unrelated terminal failure settles while the iTerm circuit is open",
          unrelatedFailed.wait(timeout: .now() + 1) == .success)
    expect("an operation does not borrow an unrelated global modal failure",
           unrelatedFailure.map(remoteErrorCode), "terminal_io_failed")
    RemoteServer.sessionPayloadForTesting = ([session], [:])

    // Keep the worker occupied: an already-open circuit must answer before admission, and must
    // not consume one of the remaining seven places.
    let blockerEntered = DispatchSemaphore(value: 0)
    let blockerRelease = DispatchSemaphore(value: 0)
    check("a deterministic broker blocker is admitted",
          RemoteServer.shared.enqueueTerminalCommand {
              blockerEntered.signal(); _ = blockerRelease.wait(timeout: .now() + 2)
          })
    check("the blocker entered", blockerEntered.wait(timeout: .now() + 1) == .success)
    let refused = DispatchSemaphore(value: 0)
    var preflight: RemoteServer.Response?
    let refusedRequest = remoteRequest(
        "POST", "/v1/sessions/\(session.id)/send",
        headers: ["Authorization": "Bearer \(writer.token)",
                  "Idempotency-Key": UUID().uuidString], body: "{\"text\":\"no\"}")
    RemoteServer.shared.sendTerminalForTesting(refusedRequest) {
        preflight = $0; refused.signal()
    }
    check("an open circuit refuses without waiting behind the broker",
          refused.wait(timeout: .now() + 1) == .success)
    expect("the preflight refusal is typed", preflight.map(remoteErrorCode),
           "iterm_attention_required")
    let drained = DispatchSemaphore(value: 0)
    for index in 0..<7 {
        check("preflight refusal did not consume broker slot \(index + 2)",
              RemoteServer.shared.enqueueTerminalCommand { if index == 6 { drained.signal() } })
    }
    check("the bounded ninth command is refused",
          !RemoteServer.shared.enqueueTerminalCommand {})
    blockerRelease.signal()
    check("the bounded broker drains", drained.wait(timeout: .now() + 1) == .success)

    let channelRelease = DispatchSemaphore(value: 0)
    let channelDrained = DispatchSemaphore(value: 0)
    check("one session may enter its fair-share channel",
          RemoteServer.shared.enqueueTerminalCommand(channel: "same-session") {
              _ = channelRelease.wait(timeout: .now() + 2)
          })
    check("one session may queue one trailing operation",
          RemoteServer.shared.enqueueTerminalCommand(channel: "same-session") {
              channelDrained.signal()
          })
    check("one session cannot monopolise a third broker slot",
          !RemoteServer.shared.enqueueTerminalCommand(channel: "same-session") {})
    channelRelease.signal()
    check("the per-session channel drains", channelDrained.wait(timeout: .now() + 1) == .success)

    let nestedFinished = DispatchSemaphore(value: 0)
    var nestedRuns = 0
    var nestedAdmitted = false
    let nestedLock = NSLock()
    check("a terminal cascade enters the production broker",
          RemoteServer.shared.enqueueTerminalCommand(channel: "cascade") {
              let admitted = RemoteServer.shared.enqueueTerminalCommand(channel: "cascade") {
                  nestedLock.lock(); nestedRuns += 1; nestedLock.unlock()
              }
              nestedLock.lock(); nestedAdmitted = admitted; nestedLock.unlock()
              nestedFinished.signal()
          })
    check("the nested terminal cascade finishes",
          nestedFinished.wait(timeout: .now() + 1) == .success)
    check("broker counters return to zero after nested work", eventually {
        let outstanding = RemoteServer.shared.terminalOutstandingForTesting()
        return outstanding.total == 0 && outstanding.channels == 0
    })
    nestedLock.lock()
    let completedNestedRuns = nestedRuns
    let completedNestedAdmission = nestedAdmitted
    nestedLock.unlock()
    check("nested cascade work runs inline instead of deadlocking", completedNestedAdmission)
    expect("the nested command executes exactly once", completedNestedRuns, 1)
}

group("key and end terminal mutations leave health and SSE turns responsive") {
    let wasWriting = Config.shared.remoteWrite
    defer {
        Config.shared.remoteWrite = wasWriting
        RemoteServer.sessionPayloadForTesting = nil
        RemoteServer.terminalRouteForTesting = nil
    }
    Config.shared.remoteWrite = true
    ITerm.completeInventoryForTesting()
    let writer = RemoteAuth.addDevice(name: "terminal mutation test", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: writer.id) }
    let session = TargetSession(backend: .tmux, id: "%77", name: "x",
                                tty: "/dev/ttys77", windowIndex: 0, tabIndex: 0,
                                assistant: .codex)
    RemoteServer.sessionPayloadForTesting = ([session], [:])

    for (path, body) in [("/v1/sessions/%2577/key", "{\"key\":\"1\"}"),
                         ("/v1/sessions/%2577/end", "{}") ] {
        let entered = DispatchSemaphore(value: 0)
        let release = DispatchSemaphore(value: 0)
        let finished = DispatchSemaphore(value: 0)
        RemoteServer.terminalRouteForTesting = { _ in
            entered.signal(); _ = release.wait(timeout: .now() + 2)
            return .json(["ok": true])
        }
        let request = remoteRequest(
            "POST", path,
            headers: ["Authorization": "Bearer \(writer.token)",
                      "Idempotency-Key": UUID().uuidString], body: body)
        RemoteServer.shared.terminalMutationForTesting(request) { response in
            if response.status == 200 { finished.signal() }
        }
        check("\(path) entered the bounded terminal worker",
              entered.wait(timeout: .now() + 1) == .success)
        let health = DispatchSemaphore(value: 0)
        let heartbeat = DispatchSemaphore(value: 0)
        RemoteServer.shared.routeOnServerQueueForTesting(remoteRequest("GET", "/v1/health")) {
            if $0.status == 200 { health.signal() }
        }
        RemoteServer.shared.heartbeatTurnForTesting { heartbeat.signal() }
        check("health remains responsive during \(path)",
              health.wait(timeout: .now() + 1) == .success)
        check("the SSE queue turn remains responsive during \(path)",
              heartbeat.wait(timeout: .now() + 1) == .success)
        release.signal()
        check("\(path) settles", finished.wait(timeout: .now() + 1) == .success)
    }
    check("place start is classified as a terminal worker route",
          RemoteServer.isTerminalWorkerRoute("/v1/places/project/start/codex"))
    check("legacy place resume is classified as a terminal worker route",
          RemoteServer.isTerminalWorkerRoute(
            "/v1/places/project/resume/11111111-1111-4111-8111-111111111111"))
    check("assistant-qualified place resume is classified as a terminal worker route",
          RemoteServer.isTerminalWorkerRoute(
            "/v1/places/project/resume/codex/11111111-1111-4111-8111-111111111111"))
    check("session focus is classified as a terminal worker route",
          RemoteServer.isTerminalWorkerRoute("/v1/sessions/TAB/focus"))
    check("manual schedule run is classified as an orchestrator terminal worker route",
          RemoteServer.isOrchestratorTerminalWorkerRoute(
            "/v1/orchestrator/schedules/11111111-1111-4111-8111-111111111111/run"))
    check("task cancel is classified as an orchestrator terminal worker route",
          RemoteServer.isOrchestratorTerminalWorkerRoute(
            "/v1/orchestrator/tasks/11111111-1111-4111-8111-111111111111"))
    check("background shell kill is classified as a terminal worker route",
          RemoteServer.isTerminalWorkerRoute("/v1/sessions/TAB/shells/build/kill"))
    let coordinationRequest = remoteRequest(
        "POST", "/v1/orchestrator/waits",
        body: "{\"owner_session_id\":\"OWNER-TERMINAL\","
            + "\"waiter_session_id\":\"WAITER-TERMINAL\"}")
    expect("coordination registration accounts the terminal that receives its delivery",
           RemoteServer.terminalChannelsForTesting(coordinationRequest), ["OWNER-TERMINAL"])

    let coordinationRelease = DispatchSemaphore(value: 0)
    check("the first production-mapped coordination delivery is admitted",
          RemoteServer.shared.enqueueTerminalCommand(
            channels: RemoteServer.terminalChannelsForTesting(coordinationRequest)) {
                _ = coordinationRelease.wait(timeout: .now() + 2)
            })
    check("one trailing coordination delivery to that session is admitted",
          RemoteServer.shared.enqueueTerminalCommand(
            channels: RemoteServer.terminalChannelsForTesting(coordinationRequest)) {})
    check("a third coordination delivery to the same session is refused",
          !RemoteServer.shared.enqueueTerminalCommand(
            channels: RemoteServer.terminalChannelsForTesting(coordinationRequest)) {})
    coordinationRelease.signal()

    let focusSession = TargetSession(backend: .iterm, id: "FOCUS-TAB", name: "focus",
                                     tty: "/dev/ttys78", windowIndex: 0, tabIndex: 0,
                                     assistant: .codex)
    RemoteServer.sessionPayloadForTesting = ([focusSession], [:])
    ITerm.blockAutomationForTesting("iTerm focus dialog")
    let focusFinished = DispatchSemaphore(value: 0)
    var focusResponse: RemoteServer.Response?
    let focusRequest = remoteRequest(
        "POST", "/v1/sessions/FOCUS-TAB/focus",
        headers: ["Authorization": "Bearer \(writer.token)",
                  "Idempotency-Key": UUID().uuidString], body: "{}")
    RemoteServer.shared.terminalMutationForTesting(focusRequest) {
        focusResponse = $0; focusFinished.signal()
    }
    check("focus settles while the iTerm circuit is open",
          focusFinished.wait(timeout: .now() + 1) == .success)
    expect("focus reports its own circuit refusal", focusResponse?.status, 502)
    expect("focus circuit refusal is typed", focusResponse.map(remoteErrorCode),
           "iterm_attention_required")
}

group("a project folder says which directory it is, and is not taken at its word") {
    // Claude Code names the folder after the working directory with every character that is not
    // a letter or a digit turned into a dash. That map is many-to-one, so the name can never be
    // read backwards — `-Users-me-code-cairn-frontend` is `cairn/frontend` and `cairn-frontend`
    // equally. The path is read out of the transcripts instead and checked against the name.
    expect("separators", StartPoints.slug(of: "/Users/me/code/notebook"), "-Users-me-code-notebook")
    expect("a space is a dash too", StartPoints.slug(of: "/a/My Work"), "-a-My-Work")
    expect("and a dot, and an underscore", StartPoints.slug(of: "/a/b.c_d"), "-a-b-c-d")
    expect("a dash was already a dash", StartPoints.slug(of: "/a/mixed-case"), "-a-mixed-case")
    expect("case survives", StartPoints.slug(of: "/a/Mixed-Case"), "-a-Mixed-Case")
    expect("and anything that is not ASCII does not",
           StartPoints.slug(of: "/a/專案"), "-a---")

    // The reason every candidate is checked: a transcript quotes other people's directories.
    // Observed on this machine — a session in `some-app` had an `another_project` cwd sitting
    // in the last hundred kilobytes of it, inside something somebody had pasted in.
    let text = #"{"type":"user","cwd":"/Users/me/code/other"}"# + "\n"
        + #"{"type":"user","cwd":"/Users/me/code/thing"}"#
    expect("every cwd in the text, not the first", StartPoints.cwds(in: text).count, 2)
    expect("in the order they appeared",
           StartPoints.cwds(in: text), ["/Users/me/code/other", "/Users/me/code/thing"])
    expect("escapes come back out",
           StartPoints.cwds(in: #"{"cwd":"/Users/me/it\"s \\ here"}"#), ["/Users/me/it\"s \\ here"])
    expect("half a line is not half a path",
           StartPoints.cwds(in: "{\"cwd\":\"/Users/me/cut\n"), [])

    // End to end over files, because the ordering rule — name wins, position does not — is the
    // whole point and lives across the two halves.
    let folder = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-places-\(getpid())")
    try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: folder) }
    let one = folder.appendingPathComponent("one.jsonl")
    try? Data((#"{"type":"user","cwd":"/Users/me/code/somewhere-else"}"# + "\n"
               + #"{"type":"user","cwd":"/Users/me/code/thing"}"# + "\n").utf8).write(to: one)
    expect("the cwd that matches the folder's name is the one believed",
           StartPoints.directory(named: "-Users-me-code-thing", transcripts: [one]),
           "/Users/me/code/thing")
    expect("and a folder whose transcripts never mention it proves nothing",
           StartPoints.directory(named: "-Users-me-code-missing", transcripts: [one]), nil)
    expect("nor does one with no transcripts at all",
           StartPoints.directory(named: "-Users-me-code-thing", transcripts: []), nil)
}

group("the line a new tab is given, before anything types it") {
    // The list these come from is derived from the filesystem, which is to say from names the
    // person at the Mac did not necessarily choose. A quote in one of them must not be able to
    // end the quoting and start a command.
    // Every line this app types now opens with `env -u …` between the `&&` and the program
    // name. What that prefix is exactly is pinned in "a new tab is not handed the identity of
    // whatever launched the terminal"; here it is composed, so these stay assertions about
    // the thing they were written for.
    let starts = "&& " + Assistant.claude.dropInheritedIdentity + "claude"
    expect("an ordinary path is still quoted",
           StartPoints.itermLine(cwd: "/Users/me/code/notebook"),
           "cd '/Users/me/code/notebook' " + starts)
    expect("a space changes nothing about it",
           StartPoints.itermLine(cwd: "/a/My Work"), "cd '/a/My Work' " + starts)
    expect("a quote cannot close the quoting",
           StartPoints.itermLine(cwd: "/a/it's here"), "cd '/a/it'\\''s here' " + starts)
    expect("a backslash is a backslash inside single quotes",
           StartPoints.itermLine(cwd: "/a/back\\slash"), "cd '/a/back\\slash' " + starts)
    check("and nothing a client sent is anywhere in it",
          !StartPoints.itermLine(cwd: "/a/b").contains(";"))

    // The one thing quoting cannot save, so it never reaches the quoting: on this path the line
    // *is* the submission, and a newline in the middle of one runs the second half as a command.
    check("a newline in a directory name is not a place at all",
          !StartPoints.usable("/a/two\nlines"))
    check("nor is a carriage return", !StartPoints.usable("/a/two\rlines"))
    check("nor a relative path", !StartPoints.usable("code/notebook"))
    check("an ordinary absolute path is", StartPoints.usable("/Users/me/code/notebook"))
    check("and so is one with a quote in it, which the quoting handles",
          StartPoints.usable("/a/it's here"))
}

group("which terminal a session is started in, and when none of them will do") {
    let iterm = StartPoints.itermBundleID
    func plan(_ scope: String, _ running: Set<String>, _ tmux: Bool) -> StartPoints.Plan {
        StartPoints.plan(scope: scope, running: running, hasTmux: tmux)
    }
    expect("iTerm2 is named and open", plan(iterm, [iterm], false), .iterm)
    expect("no scope at all means no preference, and that is iTerm2 first",
           plan("", [iterm], true), .iterm)
    expect("named among others", plan("com.apple.Terminal,\(iterm)", [iterm], false), .iterm)
    expect("iTerm2 is named and shut, and there is a tmux to go through instead",
           plan(iterm, [], true), .tmux)
    expect("iTerm2 is named and shut and there is nothing else",
           plan(iterm, [], false), .notRunning(app: iterm))

    // The refusal that matters: a terminal this cannot drive must be said out loud rather than
    // quietly handed to iTerm2, because a session that opened somewhere nobody was looking is
    // worse than a sentence saying it did not open.
    expect("another terminal, with tmux under it", plan("com.mitchellh.ghostty", [], true), .tmux)
    expect("another terminal and no tmux is refused by name",
           plan("com.mitchellh.ghostty", ["com.mitchellh.ghostty"], false),
           .cannotDrive(app: "com.mitchellh.ghostty"))
    check("and it never silently becomes iTerm2",
          plan("com.apple.Terminal", [iterm], false) != .iterm)
}

group("the list of places, tidied") {
    let now = Date()
    func place(_ path: String, _ ago: TimeInterval) -> StartPoints.Place {
        StartPoints.Place(id: StartPoints.id(for: path), path: path,
                          label: (path as NSString).lastPathComponent,
                          at: now.addingTimeInterval(-ago))
    }
    let all: [StartPoints.Place] = [
        place("/a/old", 900), place("/a/new", 10), place("/a/old", 5),
        place("/a/gone", 1), place("/a/two\nlines", 0), place("relative", 0),
    ]
    let tidied = StartPoints.tidy(all, isDirectory: { $0 != "/a/gone" })
    expect("what is not there any more, and what cannot be typed, are not offered",
           tidied.count, 2)
    expect("newest first, at the newest time the same directory was seen",
           tidied.map(\.path), ["/a/old", "/a/new"])
    expect("a cap is a cap", StartPoints.tidy(all, limit: 1, isDirectory: { _ in true }).count, 1)

    // The id is opaque and stable, which is the entire reason a client never sends a path.
    expect("the same path is the same id twice",
           StartPoints.id(for: "/a/b"), StartPoints.id(for: "/a/b"))
    check("different paths are different ids",
          StartPoints.id(for: "/a/b") != StartPoints.id(for: "/a/c"))
    check("and there is no path anywhere in it",
          !StartPoints.id(for: "/Users/me/code/notebook").contains("notebook"))

    // Codex records every cwd it is launched in, including locations its own apps and another
    // assistant made. They are true cwd values and still not projects somebody created.
    func durable(_ path: String) -> Bool {
        StartPoints.isDurablePlace(path, home: "/Users/me",
                                   temporary: "/private/var/folders/me/T")
    }
    check("the home folder is not invented into a project", !durable("/Users/me"))
    check("Codex Desktop's private workspace is not one",
          !durable("/Users/me/.codex/.chatgpt-projects/g-p-123"))
    check("nor is the Documents workspace Codex Desktop creates",
          !durable("/Users/me/Documents/Codex/2026-08-24/new-chat"))
    check("an assistant scratchpad under the system temp root is not one",
          !durable("/private/var/folders/me/T/claude/scratchpad/probe"))
    check("an ordinary checkout remains a place",
          durable("/Users/me/code/clawdline"))
}

group("the folder budget is spent on plausible places before scratch") {
    // The defect this guards: `recorded` capped the newest N folders *before* anything judged
    // what they were. On the Mac this was measured on, 162 of 192 project folders were scratch
    // — temp directories and worktree checkouts, newer than every real project — so the whole
    // budget went to folders `tidy` was about to throw away, and the menu shrank to five
    // entries on a machine with thirty projects.
    let marks = StartPoints.scratchMarks(home: "/Users/someone",
                                         temporary: "/var/folders/zz/T")
    func plausible(_ name: String) -> Bool { StartPoints.couldBeDurable(name, marks: marks) }
    check("a temp-root name is scratch in the spelling NSTemporaryDirectory uses",
          !plausible("-var-folders-zz-T-tmp-x1"))
    check("and in the /private spelling transcripts actually record",
          !plausible("-private-var-folders-zz-T-tmp-x1"))
    check("a brokered worktree checkout is scratch",
          !plausible("-Users-someone-Library-Application-Support-Clawdline-worktrees-repo-abc"))
    check("an ordinary project is not",
          plausible("-Users-someone-code-dual"))
    check("the match stops at a name boundary, so a sibling is not swept up",
          plausible("-Users-someone--claude2-notes"))
    check("nor is a root that merely shares opening letters",
          plausible("-tmpfs-data"))

    // End to end over a described tree: scratch outnumbers the one real place and every piece
    // of it is newer.
    let fm = FileManager.default
    let base = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-recorded-\(getpid())")
    defer { try? fm.removeItem(at: base) }
    let now = Date()
    func folder(under root: URL, cwd: String, provedBy recorded: String? = nil,
                ago: TimeInterval) {
        let dir = root.appendingPathComponent(StartPoints.slug(of: cwd), isDirectory: true)
        try? fm.createDirectory(at: dir, withIntermediateDirectories: true)
        let transcript = dir.appendingPathComponent("t.jsonl")
        try? Data(("{\"type\":\"user\",\"cwd\":\"\(recorded ?? cwd)\"}" + "\n").utf8)
            .write(to: transcript)
        let stamp = [FileAttributeKey.modificationDate: now.addingTimeInterval(-ago)]
        try? fm.setAttributes(stamp, ofItemAtPath: transcript.path)
        try? fm.setAttributes(stamp, ofItemAtPath: dir.path)
    }
    let projects = base.appendingPathComponent("projects", isDirectory: true)
    folder(under: projects, cwd: "/Users/someone/code/dual", ago: 900)
    for i in 0..<5 {
        folder(under: projects, cwd: "/var/folders/zz/T/tmp-\(i)", ago: TimeInterval(10 + i))
    }
    let out = StartPoints.recorded(root: projects, folders: 3, scan: 10,
                                   home: "/Users/someone", temporary: "/var/folders/zz/T")
    check("the real project is listed even though scratch outnumbers it and is newer",
          out.map(\.path).contains("/Users/someone/code/dual"))
    expect("and it is what the first slot was spent on",
           out.first?.path, "/Users/someone/code/dual")
    expect("the budget is still a budget", out.count, 3)
    check("scratch is delayed to the back of the queue rather than dropped",
          out.dropFirst().allSatisfy { $0.path.hasPrefix("/var/folders/zz/T/") })

    // `scan` bounds the reading, separately from the answer: a run of folders that cannot
    // prove what they are must not make the loop read the whole disk hunting for slots.
    let unproven = base.appendingPathComponent("unproven", isDirectory: true)
    folder(under: unproven, cwd: "/Users/someone/code/keel", ago: 900)
    for i in 0..<3 {
        folder(under: unproven, cwd: "/Users/someone/code/ghost-\(i)",
               provedBy: "/Users/someone/code/elsewhere", ago: TimeInterval(10 + i))
    }
    let strict = StartPoints.recorded(root: unproven, folders: 3, scan: 2,
                                      home: "/Users/someone", temporary: "/var/folders/zz/T")
    expect("the reading has a bound of its own", strict.count, 0)
    let generous = StartPoints.recorded(root: unproven, folders: 3, scan: 10,
                                        home: "/Users/someone", temporary: "/var/folders/zz/T")
    check("and a wider scan reads far enough down to fill the slots the tight one could not",
          generous.map(\.path).contains("/Users/someone/code/keel"))
}

group("starting a session is behind the write gate, like everything else that runs code") {
    let wasWriting = Config.shared.remoteWrite
    defer { Config.shared.remoteWrite = wasWriting }

    let reader = RemoteAuth.addDevice(name: "a phone that may read", caps: [.read])
    let writer = RemoteAuth.addDevice(name: "a phone that may send", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: reader.id); RemoteAuth.revoke(id: writer.id) }

    func post(_ target: String, token: String?, key: String?) -> RemoteServer.Response {
        var headers: [String: String] = [:]
        if let token { headers["Authorization"] = "Bearer \(token)" }
        if let key { headers["Idempotency-Key"] = key }
        return RemoteServer.shared.route(remoteRequest("POST", target, headers: headers))
    }
    let bogusID = "0123456789abcdef"
    let bogus = "/v1/places/\(bogusID)/start"

    // No token at all, and this one is checked before anything else knows the route exists.
    let anonymous = post(bogus, token: nil, key: UUID().uuidString)
    expect("no token is refused", anonymous.status, 401)
    expect("and says so", remoteErrorCode(anonymous), "unauthorized")
    expect("reading the list needs one too",
           RemoteServer.shared.route(remoteRequest("GET", "/v1/places")).status, 401)

    // The switch the owner of the Mac holds, before the capability this device holds — so a
    // device that may not send cannot learn that it may not while the whole feature is off.
    Config.shared.remoteWrite = false
    let shut = post(bogus, token: writer.token, key: UUID().uuidString)
    expect("the write switch is checked first", remoteErrorCode(shut), "write_disabled")
    expect("and that is a 403", shut.status, 403)

    Config.shared.remoteWrite = true
    let readOnly = post(bogus, token: reader.token, key: UUID().uuidString)
    expect("a device that may read and not send is refused", readOnly.status, 403)
    expect("and it is a capability refusal, not the switch",
           remoteErrorCode(readOnly), "forbidden")

    let noKey = post(bogus, token: writer.token, key: nil)
    expect("a retryable write with no idempotency key is refused", noKey.status, 400)
    expect("as a bad request", remoteErrorCode(noKey), "bad_request")

    // Past all three gates, and the id still has to be one the server put on the list. Nothing a
    // client sends is ever a path, so an unknown id is the end of the road rather than a
    // directory that failed validation.
    let unknown = post(bogus, token: writer.token, key: UUID().uuidString)
    expect("an id nobody was given is not a place", unknown.status, 404)
    expect("and says nothing else about it", remoteErrorCode(unknown), "not_found")
    let empty = post("/v1/places//start", token: writer.token, key: UUID().uuidString)
    expect("nor is an empty one", empty.status, 404)

    // **Which assistant is a path segment, and it is a name rather than a command.** The body on
    // this route is still not read at all, so the last segment is the only thing that decides
    // what gets run — and it is resolved against a two-case enum before anything else happens.
    let invented = post("/v1/places/\(bogusID)/start/emacs", token: writer.token,
                        key: UUID().uuidString)
    expect("an assistant nobody has heard of is a 404", invented.status, 404)
    expect("and it is refused before the place is even looked up",
           remoteErrorMessage(invented), "No assistant named that")
    let sneaky = post("/v1/places/\(bogusID)/start/codex;rm%20-rf%20~", token: writer.token,
                      key: UUID().uuidString)
    expect("and so is a name with a command stuck to it", sneaky.status, 404)
    // A real one gets past the name and lands on the same missing place as the plain route,
    // which is the proof that the segment chooses and does not carry.
    let codex = post("/v1/places/\(bogusID)/start/codex", token: writer.token,
                     key: UUID().uuidString)
    expect("a named assistant gets as far as the place", remoteErrorCode(codex), "not_found")

    // **The model is the fourth segment, and it is resolved the same way.** A size this Mac does
    // not offer is refused by name rather than quietly becoming the default: this route has
    // never substituted anything, and a `200` for a session running on a model nobody asked for
    // is the one wrong answer a person cannot see from outside.
    let inventedModel = post("/v1/places/\(bogusID)/start/claude/gpt-9", token: writer.token,
                             key: UUID().uuidString)
    expect("a size nobody offers is a 404", inventedModel.status, 404)
    expect("and it is refused before the place is looked up",
           remoteErrorMessage(inventedModel), "No model named that")
    let dated = post("/v1/places/\(bogusID)/start/claude/claude-opus-5-20260201",
                     token: writer.token, key: UUID().uuidString)
    expect("a dated build is not one of the three sizes either",
           remoteErrorMessage(dated), "No model named that")
    let sized = post("/v1/places/\(bogusID)/start/claude/sonnet", token: writer.token,
                     key: UUID().uuidString)
    expect("a size that is on the list gets as far as the place",
           remoteErrorCode(sized), "not_found")
    expect("and it is the place that is missing, not the model",
           remoteErrorMessage(sized), "No place named that")
    let trailing = post("/v1/places/\(bogusID)/start/claude/", token: writer.token,
                        key: UUID().uuidString)
    expect("an empty fourth segment is not a way to ask for the default",
           remoteErrorMessage(trailing), "No model named that")
    let tooDeep = post("/v1/places/\(bogusID)/start/claude/opus/now", token: writer.token,
                       key: UUID().uuidString)
    expect("and nothing deeper than that is a route", tooDeep.status, 404)
    expect("which is refused as a route rather than as a name",
           remoteErrorMessage(tooDeep), "No such route")

    // The route this replaced took a `cwd` and a `command` out of the body and ran the second in
    // the first, which behind a tunnel is "run anything anywhere" with a token in front of it.
    let old = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/sessions",
        headers: ["Authorization": "Bearer \(writer.token)",
                  "Idempotency-Key": UUID().uuidString]))
    expect("and the route that took a path in the body is gone", old.status, 404)
}

group("place history routes preserve their assistant from path to operation") {
    let legacyHistory = RemoteServer.placeHistoryTarget("/v1/places/place-one/sessions")
    expect("legacy history remains Claude", legacyHistory?.assistant, .claude)
    expect("legacy history preserves its place", legacyHistory?.placeID, "place-one")

    let codexHistory = RemoteServer.placeHistoryTarget("/v1/places/place-one/sessions/codex")
    expect("explicit Codex history reaches the history operation as Codex",
           codexHistory?.assistant, .codex)
    expect("explicit Codex history preserves its place", codexHistory?.placeID, "place-one")

    let legacyResume = RemoteServer.placeResumeTarget(
        "/v1/places/place-one/resume/11111111-1111-4111-8111-111111111111")
    expect("legacy resume remains Claude", legacyResume?.assistant, .claude)
    expect("legacy resume preserves its session", legacyResume?.sessionID,
           "11111111-1111-4111-8111-111111111111")

    let codexResume = RemoteServer.placeResumeTarget(
        "/v1/places/place-one/resume/codex/22222222-2222-4222-8222-222222222222")
    expect("explicit Codex resume reaches the resume operation as Codex",
           codexResume?.assistant, .codex)
    expect("explicit Codex resume preserves its session", codexResume?.sessionID,
           "22222222-2222-4222-8222-222222222222")

    check("unknown history assistant has no route",
          RemoteServer.placeHistoryTarget("/v1/places/place-one/sessions/emacs") == nil)
    check("unknown resume assistant has no route",
          RemoteServer.placeResumeTarget("/v1/places/place-one/resume/emacs/thread-one") == nil)

    let wasWriting = Config.shared.remoteWrite
    defer { Config.shared.remoteWrite = wasWriting }
    Config.shared.remoteWrite = true
    let device = RemoteAuth.addDevice(name: "history route test", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: device.id) }
    let headers = ["Authorization": "Bearer \(device.token)"]
    let unknownHistory = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/places/place-one/sessions/emacs", headers: headers))
    expect("unknown history assistant is a 404", unknownHistory.status, 404)
    let unknownResume = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/places/place-one/resume/emacs/thread-one",
        headers: headers.merging(["Idempotency-Key": UUID().uuidString]) { _, new in new }))
    expect("unknown resume assistant is a 404", unknownResume.status, 404)
}

group("the page is given the words it draws the start sheet with") {
    // A string in `Copy` is not a string on the page. `strings(for:)` is the only thing that
    // decides what a browser is told, and thirteen of these sat translated into fourteen
    // languages for a release without being in it — which is a feature nobody outside English
    // could read, and nothing anywhere went red about it.
    // The header only decides anything while the app is following whoever is asking. A machine
    // running the tests may well have picked a language for the bar, and that choice wins over a
    // browser — so it is put back to `auto` here and restored, or this group would be reading
    // whichever language this Mac happens to be set to.
    let wasLanguage = Config.shared.language
    defer { Config.shared.language = wasLanguage }
    Config.shared.language = "auto"

    func words(language: String) -> [String: Any] {
        let response = RemoteServer.shared.route(
            remoteRequest("GET", "/v1/strings", headers: ["Accept-Language": language]))
        return ((try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]) ?? [:]
    }

    let english = words(language: "en")
    let needed = ["webStart", "webStartLabel", "webStartPick", "webStartEmpty", "webStartFilter",
                  "webStarting", "webStartWaiting", "webStartSlow", "webStartFailed",
                  "webStartGone", "webStartTerminalClosed", "webStartTerminalUnsupported",
                  "webStartOff"]
    let absent = needed.filter { (english[$0] as? String ?? "").isEmpty }
    check("every word the start sheet draws is published", absent.isEmpty,
          "not on /v1/strings: " + absent.joined(separator: ", "))

    // The page writes its own sentence around the terminal's name, which arrives in the error
    // object rather than in the copy. A translation that dropped the hole would leave a phone
    // saying that something unnamed is not running.
    let holeless = L.catalog.filter {
        !$0.copy.webStartTerminalClosed.contains("{app}")
            || !$0.copy.webStartTerminalUnsupported.contains("{app}")
    }.map { $0.tag }.sorted()
    check("every language keeps the hole the terminal's name goes in", holeless.isEmpty,
          holeless.joined(separator: ", "))

    // And they arrive in the language that was asked for, which is the entire reason the page
    // fetches this before it draws anything.
    let french = words(language: "fr")
    check("and they arrive in the language the browser asked for",
          (french["webStart"] as? String).map { !$0.isEmpty && $0 != (english["webStart"] as? String) } == true,
          "fr said \((french["webStart"] as? String) ?? "nothing")")

    // The same rule for the form that makes a schedule: a string in `Copy` is not a string on the
    // page, and thirteen of the start sheet's words sat translated into fourteen languages for a
    // whole release without being on this route.
    let scheduleForm = ["webScheduleNew", "webScheduleNewSay", "webScheduleTitle", "webScheduleAt",
                        "webScheduleOn", "webScheduleWhere", "webScheduleWith", "webScheduleFirst",
                        "webScheduleMore", "webScheduleWhenDone", "webScheduleCloseSuccess",
                        "webScheduleCloseAlways", "webScheduleCloseNever", "webScheduleEnabled",
                        "webScheduleNotify", "webScheduleCatchUp", "webScheduleTimeout",
                        "webScheduleCreate", "webScheduleCreated", "webScheduleFailed",
                        "webScheduleNeedsTime", "webScheduleNeedsPlace", "webScheduleDaily",
                        "webScheduleSun", "webScheduleMon", "webScheduleTue", "webScheduleWed",
                        "webScheduleThu", "webScheduleFri", "webScheduleSat"]
    let missing = scheduleForm.filter { (english[$0] as? String ?? "").isEmpty }
    check("every word the schedule form draws is published", missing.isEmpty,
          "not on /v1/strings: " + missing.joined(separator: ", "))

    // The rows above the form draw three words of their own. `webScheduleNext` is
    // `settingsScheduleNext` under a name of its own — the same "Next" the Mac's Settings list
    // puts above the same number, already translated into fourteen languages — and it was the one
    // word on that row the page was drawing in English no matter who was reading.
    let scheduleRows = ["webScheduleNext", "webScheduleMissed", "webScheduleNoNext"]
    let rowsMissing = scheduleRows.filter { (english[$0] as? String ?? "").isEmpty }
    check("every word a schedule row draws is published too", rowsMissing.isEmpty,
          "not on /v1/strings: " + rowsMissing.joined(separator: ", "))
    check("and Next is translated rather than sent in English to everybody",
          (french["webScheduleNext"] as? String)
            .map { !$0.isEmpty && $0 != (english["webScheduleNext"] as? String) } == true,
          "fr said \((french["webScheduleNext"] as? String) ?? "nothing")")

    // The three this round adds: the row that says how big a session will be, on the command
    // sheet and on the schedule form, and the sentence a create shows when `dispatch_enabled`
    // came back false. The same four-place rule applies to all of them, and this is the place
    // that catches a string translated fourteen times and never sent.
    let sizes = ["webCommandModel", "webScheduleModel", "webScheduleDispatchOff"]
    let unsent = sizes.filter { (english[$0] as? String ?? "").isEmpty }
    check("the model rows and the dispatch-off sentence are published", unsent.isEmpty,
          "not on /v1/strings: " + unsent.joined(separator: ", "))
}
}
