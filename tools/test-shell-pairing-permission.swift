import Foundation

/// Answers every request the stubbed session makes, and keeps what it was asked.
final class StubProtocol: URLProtocol {
    nonisolated(unsafe) static var status = 200
    nonisolated(unsafe) static var body = Data(#"{"ok":true}"#.utf8)
    nonisolated(unsafe) static var seen: [(URLRequest, Data)] = []

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        var body = request.httpBody ?? Data()
        if body.isEmpty, let stream = request.httpBodyStream {
            stream.open()
            var buffer = [UInt8](repeating: 0, count: 4096)
            while stream.hasBytesAvailable {
                let n = stream.read(&buffer, maxLength: buffer.count)
                if n <= 0 { break }
                body.append(buffer, count: n)
            }
            stream.close()
        }
        Self.seen.append((request, body))
        let response = HTTPURLResponse(url: request.url!, statusCode: Self.status,
                                       httpVersion: "HTTP/1.1", headerFields: nil)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Self.body)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

/// Records every screen the flow puts up, in order.
final class Screens {
    var events: [String] = []
    var choice: PairingChoice = .grant(.readOnly)
    var retries: [Bool] = []

    var value: PairingScreens {
        PairingScreens(
            choose: { [self] _ in events.append("choose"); return choice },
            showCode: { [self] notice, permission in events.append("code \(notice.code) \(permission.rawValue)") },
            showFailure: { [self] _, outcome in
                events.append("failure \(outcome)")
                return retries.isEmpty ? false : retries.removeFirst()
            })
    }
}

@main
struct ShellPairingPermissionTest {
    static let base = URL(string: "http://127.0.0.1:7999/")!
    static let live = Date(timeIntervalSince1970: 1_000)
    static let notice = PairingNotice(pairing_id: "pr_1", name: "Phone", code: "123456", expires: 1_120)

    static func check(_ condition: Bool, _ what: String) {
        if !condition {
            print("FAIL: \(what)")
            exit(1)
        }
    }

    static func waitFor(_ what: String, _ done: () -> Bool) {
        let deadline = Date().addingTimeInterval(5)
        while !done() && Date() < deadline {
            RunLoop.main.run(until: Date().addingTimeInterval(0.01))
        }
        check(done(), what)
    }

    /// Runs one flow whose save answers `outcomes` in turn, synchronously.
    static func run(_ screens: Screens, _ outcomes: [PairingPermissionOutcome],
                    now start: Date = live, after: Date? = nil) -> (saves: [PairingPermission], finished: Int) {
        var outcomes = outcomes
        var now = start
        var saves: [PairingPermission] = []
        var finished = 0
        let flow = PairingPermissionFlow(
            notice, screens: screens.value,
            save: { _, permission, done in
                saves.append(permission)
                screens.events.append("save \(permission.rawValue)")
                if let after { now = after }
                done(outcomes.removeFirst())
            },
            now: { now }, finished: { finished += 1 })
        flow.start()
        return (saves, finished)
    }

    static func requests() throws {
        let client = PairingPermissionClient(base: base, token: { nil })
        let request = client.request(pairingID: "pr_1", permission: .readOnly, token: "T")!
        check(request.url?.absoluteString == "http://127.0.0.1:7999/v1/auth/pairings/pr_1/permission", "path")
        check(request.httpMethod == "POST", "method")
        check(request.value(forHTTPHeaderField: "Authorization") == "Bearer T", "local token")
        check(request.value(forHTTPHeaderField: "Content-Type") == "application/json", "content type")
        // The registered bound (console.native_pairing_save_seconds; the
        // capacity package's native_pairing_test.go compares the value).
        check(request.timeoutInterval == PairingPermissionClient.saveTimeoutSeconds, "the request uses saveTimeoutSeconds")
        check(PairingPermissionClient.saveTimeoutSeconds > 0 && PairingPermissionClient.saveTimeoutSeconds < 120,
              "a save ends well inside the two minutes a code lives")
        let body = try JSONSerialization.jsonObject(with: request.httpBody!) as! [String: String]
        check(body == ["permission": "read-only"], "read-only body")
        let operate = client.request(pairingID: "pr_1", permission: .readAndOperate, token: "T")!
        let operateBody = try JSONSerialization.jsonObject(with: operate.httpBody!) as! [String: String]
        check(operateBody == ["permission": "read-and-operate"], "read-and-operate body")
        let odd = client.request(pairingID: "a/b?c", permission: .readOnly, token: "T")!
        check(odd.url?.absoluteString == "http://127.0.0.1:7999/v1/auth/pairings/a%2Fb%3Fc/permission",
              "an id stays one path segment")
        check(client.request(pairingID: "", permission: .readOnly, token: "T") == nil, "no empty id")
    }

    static func outcomes() {
        let ok = Data(#"{"ok":true}"#.utf8)
        check(PairingPermissionClient.outcome(status: 200, body: ok) == .saved, "200 ok is saved")
        check(PairingPermissionClient.outcome(status: 200, body: Data("{}".utf8)) == .refused(200),
              "200 without ok is not saved")
        check(PairingPermissionClient.outcome(status: 200, body: nil) == .refused(200), "empty 200 is not saved")
        check(PairingPermissionClient.outcome(status: 404, body: nil) == .expired, "404 is expired")
        check(PairingPermissionClient.outcome(status: 400, body: nil) == .refused(400), "400 refused")
        check(PairingPermissionClient.outcome(status: 401, body: nil) == .refused(401), "401 refused")
        check(PairingPermissionClient.outcome(status: nil, body: nil) == .unreachable, "no answer")
    }

    static func client() {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [StubProtocol.self]
        let session = URLSession(configuration: config)
        let client = PairingPermissionClient(base: base, token: { "T" }, session: session)

        var result: PairingPermissionOutcome?
        var onMain = false
        client.save(notice, .readAndOperate) { result = $0; onMain = Thread.isMainThread }
        check(result == nil, "save does not answer before the request")
        waitFor("save answers") { result != nil }
        check(result == .saved && onMain, "saved, answered on main")
        check(StubProtocol.seen.count == 1, "one request")
        let (request, body) = StubProtocol.seen[0]
        check(request.url?.path == "/v1/auth/pairings/pr_1/permission", "request path")
        let json = try? JSONSerialization.jsonObject(with: body) as? [String: String]
        check(json == ["permission": "read-and-operate"], "request JSON")

        StubProtocol.status = 404
        StubProtocol.body = Data(#"{"error":{"code":"not_found","message":"","request_id":"x"}}"#.utf8)
        result = nil
        client.save(notice, .readOnly) { result = $0 }
        waitFor("404 answers") { result != nil }
        check(result == .expired, "404 reaches the flow as expired")

        StubProtocol.seen = []
        result = nil
        PairingPermissionClient(base: base, token: { nil }, session: session).save(notice, .readOnly) { result = $0 }
        waitFor("no token answers") { result != nil }
        check(result == .unreachable && StubProtocol.seen.isEmpty, "no token, no request, nothing saved")
    }

    static func flows() {
        // Ignored: nothing saved, no code, the gate opens.
        var s = Screens(); s.choice = .ignore
        var r = run(s, [])
        check(s.events == ["choose"] && r.saves.isEmpty && r.finished == 1, "ignore: \(s.events)")

        // Saved: the code only after the save, with the saved permission.
        s = Screens(); s.choice = .grant(.readOnly)
        r = run(s, [.saved])
        check(s.events == ["choose", "save read-only", "code 123456 read-only"] && r.finished == 1,
              "saved: \(s.events)")
        s = Screens(); s.choice = .grant(.readAndOperate)
        r = run(s, [.saved])
        check(s.events.last == "code 123456 read-and-operate" && r.finished == 1, "operate saved")

        // Refused, then ignored: no code.
        s = Screens()
        r = run(s, [.refused(400)])
        check(s.events == ["choose", "save read-only", "failure refused(400)"] && r.finished == 1,
              "refused: \(s.events)")

        // Unreachable, retried, then saved: one code, after the second save.
        s = Screens(); s.retries = [true]
        r = run(s, [.unreachable, .saved])
        check(s.events == ["choose", "save read-only", "failure unreachable", "save read-only",
                           "code 123456 read-only"] && r.finished == 1, "retry: \(s.events)")

        // Expired answer: said once, never retried, no code.
        s = Screens(); s.retries = [true, true]
        r = run(s, [.expired])
        check(s.events == ["choose", "save read-only", "failure expired"] && r.saves.count == 1 && r.finished == 1,
              "expired: \(s.events)")

        // Saved, but the two minutes ran out while it was asked: no code.
        s = Screens()
        r = run(s, [.saved], after: Date(timeIntervalSince1970: 1_120))
        check(s.events == ["choose", "save read-only", "failure expired"] && r.finished == 1,
              "late save: \(s.events)")

        // Already expired when it arrives: nothing on screen at all.
        s = Screens()
        r = run(s, [], now: Date(timeIntervalSince1970: 2_000))
        check(s.events.isEmpty && r.finished == 1, "stale notice: \(s.events)")
    }

    /// The watcher's one-at-a-time gate stays shut while a save is in flight
    /// and opens on every way out of the flow.
    static func gate() {
        let watcher = PairingWatcher(base: base)
        var shown: [String] = []
        var pending: ((PairingPermissionOutcome) -> Void)?
        var flow: PairingPermissionFlow?
        let screens = Screens()
        watcher.onPairing = { notice in
            shown.append(notice.pairing_id)
            flow = PairingPermissionFlow(
                notice, screens: screens.value,
                save: { _, _, done in pending = done },
                now: { live }, finished: { watcher.alertClosed() })
            flow?.start()
        }
        watcher.deliver(notice)
        waitFor("first pairing shown") { shown.count == 1 }
        watcher.deliver(PairingNotice(pairing_id: "pr_2", name: "Other", code: "654321", expires: 1_120))
        RunLoop.main.run(until: Date().addingTimeInterval(0.1))
        check(shown == ["pr_1"] && pending != nil, "a second pairing waits behind a save in flight")

        pending?(.refused(500))
        check(!screens.events.contains { $0.hasPrefix("code") }, "a failed save shows no code")
        watcher.deliver(PairingNotice(pairing_id: "pr_3", name: "Third", code: "111111", expires: 1_120))
        waitFor("the gate opens after a failure") { shown.count == 2 }
        check(shown == ["pr_1", "pr_3"], "next pairing after failure")

        pending?(.saved)
        check(screens.events.last == "code 111111 read-only", "a saved pairing shows its code")
        _ = flow

        screens.choice = .ignore
        watcher.deliver(PairingNotice(pairing_id: "pr_4", name: "Fourth", code: "222222", expires: 1_120))
        waitFor("fourth shown") { shown.count == 3 }
        watcher.deliver(PairingNotice(pairing_id: "pr_5", name: "Fifth", code: "333333", expires: 1_120))
        waitFor("the gate opens after ignore") { shown.count == 4 }
        check(shown == ["pr_1", "pr_3", "pr_4", "pr_5"], "ignore opens the gate: \(shown)")
    }

    static func main() throws {
        let cloud = URL(string: "https://app.clawdline.com")!
        func admitted(_ main: Bool = true, _ scheme: String = "https", _ host: String = "app.clawdline.com",
                      _ port: Int = 0, _ page: URL? = URL(string: "https://app.clawdline.com/#machines")) -> Bool {
            CloudPairingAccess.allows(mainFrame: main, scheme: scheme, host: host, port: port, page: page, trusted: cloud)
        }
        check(admitted(), "Cloud main frame can request native confirmation")
        check(!admitted(false), "Cloud iframe cannot pair")
        check(!admitted(true, "http"), "HTTP cannot impersonate HTTPS Cloud")
        check(!admitted(true, "https", "app.clawdline.com.attacker.example"), "host suffix cannot pair")
        check(!admitted(true, "https", "attacker.example"), "foreign frame cannot pair")
        check(!admitted(true, "https", "app.clawdline.com", 8443), "another port cannot pair")
        check(!admitted(true, "https", "app.clawdline.com", 0, URL(string: "https://attacker.example")), "navigation away revokes bridge admission")
        check(!admitted(true, "https", "app.clawdline.com", 0, nil), "missing top-level page cannot pair")
        try requests()
        outcomes()
        client()
        flows()
        gate()
        print("Swift shell pairing permission: request, outcomes, client, flows and gate passed")
    }
}
