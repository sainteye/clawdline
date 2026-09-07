import AppKit
import Foundation

// Where a notification points, driven through the routes that send it.
//
// **Its own file because `Tests/OrchestratorCoordinationTests.swift` is at the 2,000-line
// stop-growth limit**, the same reason `Tests/UsageProjectWorktreeTests.swift` exists. The runner
// is called immediately after that file's, and these groups were written to run immediately after
// its last one, so `expectedOrderedTestGroupTitles` is untouched by where they live.
//
// The pure half of the same subject — which address each rule produces, for every id shape — is in
// `Tests/HookTests.swift`, beside `WebPush.sessionURL`'s own group. What is here is the wiring:
// that the two `/notify` lanes and `POST /v1/push/test` actually reach those rules, through their
// real routes, with their real gates in front of them.
func runNotificationAddressTests() {

group("both agent-notification lanes open the session they were sent from") {
    Orchestrator.forget()
    let agentNotifyWasEnabled = Config.shared.orchestratorAgentNotify
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        Config.shared.orchestratorAgentNotify = agentNotifyWasEnabled
        Orchestrator.agentPushForTesting = nil
        if let before {
            try? before.write(to: store, options: .atomic)
        } else {
            try? FileManager.default.removeItem(at: store)
        }
        Orchestrator.forget()
    }
    Config.shared.orchestratorAgentNotify = true

    // Two tmux pane ids, because the per-cent is the character this whole line broke on.
    let childTab = "%208"
    let rootTab = "%247"
    let secret = String(repeating: "b7", count: 32)
    let taskID = "3f1c9a72-5b64-4f0a-9c31-2ad7e6b40915"
    let rows: [[String: Any]] = [[
        "id": taskID, "state": "briefed", "kind": "custom", "title": "daily weather",
        "assistant": "codex", "project_dir": "/tmp", "timeout_minutes": 30,
        "created": Date().addingTimeInterval(-120).timeIntervalSince1970,
        "secret_hash": Orchestrator.hash(ofSecret: secret), "artifacts": [],
        "child_terminal": childTab,
    ]]
    let stored = (try? JSONSerialization.data(withJSONObject: ["version": 1, "tasks": rows]))
        ?? Data()
    try? FileManager.default.createDirectory(at: store.deletingLastPathComponent(),
                                             withIntermediateDirectories: true)
    try? stored.write(to: store, options: .atomic)
    Orchestrator.forget()

    var urls: [String] = []
    Orchestrator.agentPushForTesting = { _, _, url, _, _ in
        urls.append(url)
        return WebPush.Delivery(sent: 1, failed: 0)
    }
    func notify(_ path: String, _ object: [String: Any],
                headers: [String: String] = [:]) -> RemoteServer.Response {
        let data = (try? JSONSerialization.data(withJSONObject: object)) ?? Data()
        return RemoteServer.shared.route(remoteRequest("POST", path, headers: headers,
                                                       body: String(decoding: data, as: UTF8.self)))
    }

    let fromTask = notify("/v1/orchestrator/tasks/\(taskID)/notify",
                          ["title": "forecast", "body": "sunny", "secret": secret])
    expect("the task lane accepts a notification from its own child", fromTask.status, 200)
    expect("and that notification opens the tab the task is running in",
           urls.last ?? "", "/#session=%25208")

    // The machine token proves this Mac's user is asking and can never say which root did, so a
    // root that wants its own tab opened says which one it is. `AGENTS.md` has a root send this
    // exactly when it is about to wait for an answer — the one message where landing on the
    // session list wastes the wait it was sent to end.
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let fromRoot = notify("/v1/orchestrator/notify",
                          ["title": "about to wait", "body": "need an answer",
                           "session_id": rootTab], headers: auth)
    expect("the root lane accepts a notification that names its own session", fromRoot.status, 200)
    expect("and that notification opens that root", urls.last ?? "", "/#session=%25247")

    let fromScript = notify("/v1/orchestrator/notify",
                            ["title": "a cron line with no tab", "body": "nothing to open"],
                            headers: auth)
    expect("a caller holding the machine token and no session still sends", fromScript.status, 200)
    expect("and its notification goes to the list, exactly as it always has",
           urls.last ?? "", "/")
    expect("three notifications, three separate addresses", urls.count, 3)
}

group("a test push is a test loop, and its words are what keep it one") {
    let phone = RemoteAuth.addDevice(name: "a phone testing its own notifications", caps: [.read])
    defer { RemoteAuth.revoke(id: phone.id) }
    let unsubscribed = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/push/test", headers: ["Authorization": "Bearer \(phone.token)"], body: "{}"))
    expect("a device that has not subscribed is refused before anything is sent",
           unsubscribed.status, 409)
    expect("with the code that says what to do about it",
           remoteErrorCode(unsubscribed), "not_subscribed")

    // Built directly rather than through `subscription(from:)`: what is under test is the address
    // this route decides on, and a real endpoint is the one part of it nothing here should own.
    let subscription = WebPush.Subscription(
        id: "test-loop-\(UUID().uuidString.lowercased())",
        endpoint: URL(string: "https://web.push.apple.com/clawdline-test-loop")!,
        p256dh: Data([UInt8(0x04)] + Array(repeating: UInt8(0x01), count: 64)),
        auth: Data(repeating: 0x02, count: 16), device: phone.id,
        origin: URL(string: "https://phone.clawdline.example")!, created: Date())
    WebPush.add(subscription)
    defer { WebPush.remove(id: subscription.id) }

    var sent: [(title: String, body: String, url: String)] = []
    RemoteServer.pushTestForTesting = { sent.append((title: $0, body: $1, url: $2)) }
    Orchestrator.watchedSessionIDsForTesting = ["%208"]
    defer {
        RemoteServer.pushTestForTesting = nil
        Orchestrator.watchedSessionIDsForTesting = nil
    }
    func press(_ body: String) -> RemoteServer.Response {
        RemoteServer.shared.route(remoteRequest(
            "POST", "/v1/push/test",
            headers: ["Authorization": "Bearer \(phone.token)"], body: body))
    }

    expect("pressing it with a session open answers", press("{\"session_id\":\"%208\"}").status,
           200)
    expect("and what arrives opens that session, per-cent and all",
           sent.last?.url ?? "", "/#session=%25208")
    expect("pressing it with nothing open answers too", press("{}").status, 200)
    expect("and that one goes to the list, which is what it has always done",
           sent.last?.url ?? "", "/")
    expect("a session this Mac is not watching answers as well",
           press("{\"session_id\":\"%141\"}").status, 200)
    expect("and is not promised an address that would open nothing",
           sent.last?.url ?? "", "/")
    expect("a body that is not JSON at all is still a test push", press("not json").status, 200)
    expect("carrying the list", sent.last?.url ?? "", "/")

    // **The one thing that must not move.** A test that arrived may never be mistaken for a
    // session that needs you, and that is true because of the words rather than the address:
    // `Clawdline` over the test sentence is a title no session has ever had. This is the check
    // that goes red if somebody makes the test push *look* like a session to make it feel real.
    check("every test push says the same two things, whatever address it carries",
          sent.count == 4 && sent.allSatisfy { $0.title == "Clawdline" && $0.body == L.t.pushTest })
    check("and the sentence it says is the string table's, not a session's summary",
          L.t.pushTest == English().pushTest || L.t.pushTest == TraditionalChinese().pushTest)
}
}
