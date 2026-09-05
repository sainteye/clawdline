import Foundation

private func snippetReply(_ reply: Snippets.Reply) -> (status: Int, code: String, body: [String: Any]) {
    switch reply {
    case .ok(let body): return (200, "", body)
    case .refused(let status, let code, _, let extra): return (status, code, extra)
    }
}

private func snippetObject(id: String, scope: String = "global", project: String? = nil,
                           position: Int = 100) -> [String: Any] {
    var object: [String: Any] = [
        "id": id, "title": "title \(id.suffix(3))", "body": "body",
        "scope": scope, "position": position,
        "created_at": 1_789_000_000, "updated_at": 1_789_000_000,
    ]
    if let project { object["project"] = project }
    return object
}

private func writeSnippetFixture(_ object: [String: Any], to directory: URL, named id: String) {
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    try! JSONSerialization.data(withJSONObject: object, options: [.sortedKeys])
        .write(to: directory.appendingPathComponent("\(id).json"), options: .atomic)
}

private func snippetID(_ number: Int) -> String {
    String(format: "00000000-0000-4000-8000-%012x", number)
}

func runSnippetStoreTests() {
group("snippet files are strict, bounded, atomic, and addressed only by UUID") {
    let root = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-snippets-\(UUID().uuidString)", isDirectory: true)
    let directory = root.appendingPathComponent("snippets", isDirectory: true)
    defer {
        Snippets.resetForTesting()
        try? FileManager.default.removeItem(at: root)
    }
    Snippets.resetForTesting(directory: directory)

    let made = snippetReply(Snippets.create(from: [
        "title": "Commit", "body": "commit one file", "scope": "global",
    ], now: Date(timeIntervalSince1970: 1_789_700_000)))
    expect("a valid snippet is stored", made.status, 200)
    let id = made.body["id"] as? String ?? ""
    check("the answer is the exact stored record",
          Set(made.body.keys) == ["id", "title", "body", "scope", "position",
                                  "created_at", "updated_at"])
    let file = directory.appendingPathComponent("\(id).json")
    check("one UUID-named file exists", UUID(uuidString: id) != nil
            && FileManager.default.fileExists(atPath: file.path))
    let mode = ((try? FileManager.default.attributesOfItem(atPath: file.path)[.posixPermissions])
        as? NSNumber)?.intValue
    expect("the file lands private before it is readable", mode, 0o600)
    expect("a round trip returns one record", Snippets.records().count, 1)

    let changed = snippetReply(Snippets.update(
        id: id, from: ["body": "commit only named files"],
        now: Date(timeIntervalSince1970: 1_789_700_001)))
    expect("an edit replaces the record", changed.status, 200)
    expect("and preserves the fields it did not patch", changed.body["title"] as? String, "Commit")
    expect("while changing the named field", changed.body["body"] as? String,
           "commit only named files")
    check("the neighboring staging name is gone after replacement",
          !FileManager.default.fileExists(
            atPath: directory.appendingPathComponent(".\(id).json.new").path))

    let unknown = snippetReply(Snippets.create(from: [
        "title": "x", "body": "y", "scope": "global", "surprise": true,
    ]))
    expect("an unknown key is a typed refusal", unknown.code, "malformed_snippet")
    expect("and it writes nothing", Snippets.records().count, 1)
    expect("a long body has its own code", snippetReply(Snippets.create(from: [
        "title": "x", "body": String(repeating: "a", count: 4_001), "scope": "global",
    ])).code, "snippet_too_long")
    expect("a project path on global scope has its own code",
           snippetReply(Snippets.create(from: [
            "title": "x", "body": "y", "scope": "global", "project": "/tmp/p",
           ])).code, "snippet_scope_mismatch")

    // The three bounds count UTF-8 bytes, which is what the file, the audit line and every
    // snapshot broadcast actually pay for. Twenty Han characters are sixty bytes and fit;
    // twenty-one are sixty-three and do not, and the count beside the code says so.
    let wideTitle = snippetReply(Snippets.create(from: [
        "title": String(repeating: "字", count: 21), "body": "y", "scope": "global",
    ]))
    expect("twenty-one Han characters are sixty-three bytes of title and too long",
           wideTitle.code, "snippet_too_long")
    expect("and the count beside the code is the bytes it counted",
           wideTitle.body["title_count"] as? Int, 63)
    let widePath = snippetReply(Snippets.create(from: [
        "title": "x", "body": "y", "scope": "project",
        "project": "/tmp/" + String(repeating: "p", count: 2_000),
    ]))
    // The one field with no bound at all, and the only one that reaches `remote-audit.jsonl`.
    expect("the project path has a bound of its own", widePath.code, "snippet_too_long")
    expect("which the refusal names too", widePath.body["project_count"] as? Int, 2_005)

    let malformedID = snippetID(998)
    var malformed = snippetObject(id: malformedID)
    malformed["unknown"] = "not silently discarded"
    writeSnippetFixture(malformed, to: directory, named: malformedID)
    try! Data("not json".utf8).write(to: directory.appendingPathComponent("broken.json"))
    expect("a UUID file with an unknown record key is isolated", Snippets.records().count, 1)

    // Two hand-written files that passed the record check and should not have. The first is the
    // read side of the byte bound: 1400 Han characters are 1400 grapheme clusters and 4200
    // bytes. The second is a `position` that was read back unbounded and then had 100 added to
    // it by the next create in that scope — an arithmetic trap armed by a file, where every
    // other bad value only isolates its own.
    let wideStoredID = snippetID(996)
    var wideStored = snippetObject(id: wideStoredID)
    wideStored["body"] = String(repeating: "字", count: 1_400)
    writeSnippetFixture(wideStored, to: directory, named: wideStoredID)
    let armedID = snippetID(997)
    writeSnippetFixture(snippetObject(id: armedID, position: Int.max - 1), to: directory,
                        named: armedID)
    expect("a stored body of 4200 bytes and a position past the bound are both isolated",
           Snippets.records().count, 1)
    let afterArmed = snippetReply(Snippets.create(from: [
        "title": "still writable", "body": "x", "scope": "global",
    ], now: Date(timeIntervalSince1970: 1_789_700_002)))
    expect("and the next create in that scope answers rather than trapping on + 100",
           afterArmed.status, 200)
    _ = Snippets.delete(id: afterArmed.body["id"] as? String ?? "",
                        now: Date(timeIntervalSince1970: 1_789_700_003))
    try? FileManager.default.removeItem(at: directory.appendingPathComponent("\(armedID).json"))
    try? FileManager.default.removeItem(
        at: directory.appendingPathComponent("\(wideStoredID).json"))

    let outside = root.appendingPathComponent("outside.json")
    try! Data("keep".utf8).write(to: outside)
    expect("a delete which is not a UUID is not a path",
           snippetReply(Snippets.delete(id: "../outside")).code, "snippet_not_found")
    check("and cannot reach a neighboring file", FileManager.default.fileExists(atPath: outside.path))
    expect("the valid UUID can be deleted", snippetReply(Snippets.delete(id: id)).status, 200)
    check("the file is really gone", !FileManager.default.fileExists(atPath: file.path))

    try? FileManager.default.removeItem(at: directory)
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    Snippets.resetForTesting(directory: directory)
    for number in 1...50 {
        let candidate = snippetID(number)
        writeSnippetFixture(snippetObject(id: candidate, position: number * 100),
                            to: directory, named: candidate)
    }
    let scopedLimit = snippetReply(Snippets.create(from: [
        "title": "one too many", "body": "x", "scope": "global",
    ]))
    expect("the fifty-first snippet in one scope is refused", scopedLimit.code,
           "snippet_limit_reached")
    expect("and the refusal carries the observed count", scopedLimit.body["count"] as? Int, 50)

    try? FileManager.default.removeItem(at: directory)
    try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    Snippets.resetForTesting(directory: directory)
    for number in 1...100 {
        let candidate = snippetID(number)
        let project = number <= 50 ? "/tmp/one" : "/tmp/two"
        writeSnippetFixture(snippetObject(id: candidate, scope: "project", project: project,
                                          position: (number % 50 + 1) * 100),
                            to: directory, named: candidate)
    }
    let totalLimit = snippetReply(Snippets.create(from: [
        "title": "one too many", "body": "x", "scope": "project", "project": "/tmp/three",
    ]))
    expect("the hundred-and-first snippet on the Mac is refused", totalLimit.code,
           "snippet_limit_reached")
    expect("and that refusal carries the Mac count", totalLimit.body["count"] as? Int, 100)
}

group("snippet scope follows the mark, the git common directory, and then cwd") {
    let registry: [String: [String: Any]] = [
        "/Users/me/code/atrium": ["label": "Atrium"],
        "/Users/me/code/atrium/backend": ["label": "Atrium API"],
    ]
    let exact = Snippets.project(forCwd: "/Users/me/code/atrium", registry: registry,
                                 gitCommonDirectory: { _ in nil })
    expect("an exact icon entry supplies the key", exact.key, "/Users/me/code/atrium")
    expect("and the icon's label", exact.label, "Atrium")
    let nested = Snippets.project(forCwd: "/Users/me/code/atrium/backend/lib",
                                  registry: registry, gitCommonDirectory: { _ in nil })
    expect("the icon matcher keeps its longest containing path", nested.key,
           "/Users/me/code/atrium/backend")
    expect("and the nested row's own label", nested.label, "Atrium API")

    var askedCwd = ""
    let worktree = Snippets.project(
        forCwd: "/Users/me/Library/Application Support/Clawdline/worktrees/child",
        registry: [:], gitCommonDirectory: { cwd in
            askedCwd = cwd
            return "/Users/me/code/clawdline/.git\n"
        })
    expect("Git is asked about the session cwd", askedCwd,
           "/Users/me/Library/Application Support/Clawdline/worktrees/child")
    expect("a worktree folds into the checkout holding its common .git", worktree.key,
           "/Users/me/code/clawdline")
    expect("that checkout labels itself by its last component", worktree.label, "clawdline")

    let named = Snippets.project(
        forCwd: "/Users/me/Library/Application Support/Clawdline/worktrees/child",
        registry: ["/Users/me/code/clawdline": ["label": "Clawdline"]],
        gitCommonDirectory: { _ in "/Users/me/code/clawdline/.git\n" })
    expect("a folded worktree still keys on the checkout", named.key, "/Users/me/code/clawdline")
    expect("and asks the registry about it, so both sessions of one project say one name",
           named.label, "Clawdline")

    // A symlink is the one thing `standardizingPath` does not take out, and inside a repository
    // step 2 hid that: Git answers with the real checkout however you walked in. A registered
    // project that is not a repository had no second chance and silently showed an empty list.
    let scopeRoot = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-snippet-scope-\(UUID().uuidString)", isDirectory: true)
    let checkout = scopeRoot.appendingPathComponent("real", isDirectory: true)
    try! FileManager.default.createDirectory(
        at: checkout.appendingPathComponent("sub", isDirectory: true),
        withIntermediateDirectories: true)
    let entrance = scopeRoot.appendingPathComponent("link")
    try! FileManager.default.createSymbolicLink(at: entrance, withDestinationURL: checkout)
    defer { try? FileManager.default.removeItem(at: scopeRoot) }
    let registered: [String: [String: Any]] = [checkout.path: ["label": "Linked"]]
    let straight = Snippets.project(forCwd: checkout.path, registry: registered,
                                    gitCommonDirectory: { _ in nil })
    let throughLink = Snippets.project(forCwd: entrance.path, registry: registered,
                                       gitCommonDirectory: { _ in nil })
    expect("a project entered through a symlink is the same project", throughLink.key,
           straight.key)
    expect("and carries the registry's label rather than an empty list", throughLink.label,
           "Linked")
    expect("a subdirectory reached through that link is too",
           Snippets.project(forCwd: entrance.appendingPathComponent("sub").path,
                            registry: registered, gitCommonDirectory: { _ in nil }).key,
           straight.key)
    expect("and a trailing slash names one directory, not two",
           Snippets.project(forCwd: checkout.path + "/", registry: registered,
                            gitCommonDirectory: { _ in nil }).key,
           straight.key)

    let plain = Snippets.project(forCwd: "/tmp/no-registry-here", registry: [:],
                                 gitCommonDirectory: { _ in nil })
    expect("outside a registry and repository the cwd is the key", plain.key,
           "/tmp/no-registry-here")
    expect("and the cwd supplies the fallback label", plain.label, "no-registry-here")
}

group("snippet routes share the write gate and preserve typed refusals") {
    let directory = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-snippet-routes-\(UUID().uuidString)", isDirectory: true)
    Snippets.resetForTesting(directory: directory)
    let wasWriting = Config.shared.remoteWrite
    let reader = RemoteAuth.addDevice(name: "snippet reader", caps: [.read])
    let writer = RemoteAuth.addDevice(name: "snippet writer", caps: [.read, .send])
    defer {
        Config.shared.remoteWrite = wasWriting
        RemoteAuth.revoke(id: reader.id); RemoteAuth.revoke(id: writer.id)
        Snippets.resetForTesting()
        try? FileManager.default.removeItem(at: directory)
    }
    let body = #"{"title":"Commit","body":"commit named files","scope":"global"}"#
    func call(_ method: String, _ path: String = "/v1/snippets", token: String?,
              key: String?, body: String? = nil) -> RemoteServer.Response {
        var headers: [String: String] = [:]
        if let token { headers["Authorization"] = "Bearer \(token)" }
        if let key { headers["Idempotency-Key"] = key }
        return RemoteServer.shared.route(remoteRequest(method, path, headers: headers, body: body))
    }

    Config.shared.remoteWrite = true
    expect("a snippet write without a device is unauthorized",
           call("POST", token: nil, key: UUID().uuidString, body: body).status, 401)
    expect("a read-only device cannot write snippets",
           remoteErrorCode(call("POST", token: reader.token, key: UUID().uuidString, body: body)),
           "forbidden")
    Config.shared.remoteWrite = false
    expect("the Mac's write switch is checked before the other gates",
           remoteErrorCode(call("POST", token: writer.token, key: nil, body: body)),
           "write_disabled")
    Config.shared.remoteWrite = true
    expect("a missing idempotency key is the ordinary bad request",
           remoteErrorCode(call("POST", token: writer.token, key: nil, body: body)), "bad_request")

    // The list rides `orchestratorSnapshot`, and until this counter existed nothing republished
    // it after a snippet write: a browser on the Cloud path reads its whole list out of the
    // published rows, so it held a stale one until an unrelated orchestrator event repainted it.
    RemoteServer.snippetRepublishCountForTesting = 0
    let malformed = call("POST", token: writer.token, key: UUID().uuidString,
                         body: #"{"title":"x","body":"y","scope":"global","extra":1}"#)
    expect("the create route preserves the store's unknown-key code",
           remoteErrorCode(malformed), "malformed_snippet")
    expect("a refusal changed nothing, so it publishes nothing",
           RemoteServer.snippetRepublishCountForTesting, 0)
    let created = call("POST", token: writer.token, key: UUID().uuidString, body: body)
    expect("POST /v1/snippets creates one", created.status, 200)
    expect("and a create republishes the snapshot the list rides on",
           RemoteServer.snippetRepublishCountForTesting, 1)
    let createdBody = (try? JSONSerialization.jsonObject(with: created.body)) as? [String: Any]
    let id = createdBody?["id"] as? String ?? ""

    let missing = call("PATCH", "/v1/snippets/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
                       token: writer.token, key: UUID().uuidString, body: #"{"title":"x"}"#)
    expect("PATCH preserves snippet_not_found", remoteErrorCode(missing), "snippet_not_found")
    let mismatch = call("PATCH", "/v1/snippets/\(id)", token: writer.token,
                        key: UUID().uuidString, body: #"{"scope":"project"}"#)
    expect("PATCH preserves the scope mismatch", remoteErrorCode(mismatch),
           "snippet_scope_mismatch")
    let patched = call("PATCH", "/v1/snippets/\(id)", token: writer.token,
                       key: UUID().uuidString,
                       body: #"{"scope":"project","project":"/tmp/route-project"}"#)
    expect("PATCH /v1/snippets/:id changes one", patched.status, 200)
    expect("so does an edit", RemoteServer.snippetRepublishCountForTesting, 2)

    let incompleteOrder = call("POST", "/v1/snippets/order", token: writer.token,
                               key: UUID().uuidString,
                               body: #"{"scope":"project","project":"/tmp/route-project","order":[]}"#)
    expect("order cannot remove a scope member", remoteErrorCode(incompleteOrder),
           "snippet_scope_mismatch")
    let ordered = call("POST", "/v1/snippets/order", token: writer.token,
                       key: UUID().uuidString,
                       body: "{\"scope\":\"project\",\"project\":\"/tmp/route-project\","
                            + "\"order\":[\"\(id)\"]}")
    expect("POST /v1/snippets/order saves a full scope order", ordered.status, 200)
    expect("and a reorder", RemoteServer.snippetRepublishCountForTesting, 3)

    let deletionKey = UUID().uuidString
    let deleted = call("DELETE", "/v1/snippets/\(id)", token: writer.token,
                       key: deletionKey)
    expect("DELETE /v1/snippets/:id removes one", deleted.status, 200)
    expect("and a delete", RemoteServer.snippetRepublishCountForTesting, 4)
    expect("a retry replays the successful delete",
           call("DELETE", "/v1/snippets/\(id)", token: writer.token,
                key: deletionKey).status, 200)
    expect("a replayed answer is not a second change to publish",
           RemoteServer.snippetRepublishCountForTesting, 4)
    expect("and a fresh delete sees the typed absence",
           remoteErrorCode(call("DELETE", "/v1/snippets/\(id)", token: writer.token,
                                key: UUID().uuidString)), "snippet_not_found")

    Snippets.resetForTesting(directory: directory)
    for number in 1...10 {
        let response = Snippets.create(from: [
            "title": "rate \(number)", "body": "x", "scope": "project",
            "project": "/tmp/rate-\(number)",
        ], now: Date(timeIntervalSince1970: 1_789_800_000 + Double(number)))
        expect("write ticket \(number) is admitted", snippetReply(response).status, 200)
    }
    let braked = snippetReply(Snippets.create(from: [
        "title": "eleven", "body": "x", "scope": "project", "project": "/tmp/rate-11",
    ], now: Date(timeIntervalSince1970: 1_789_800_011)))
    expect("the eleventh write in ten minutes is braked", braked.code, "rate_limited")
}

group("snippets ride the snapshot and session reads are already filtered") {
    let root = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-snippet-snapshot-\(UUID().uuidString)", isDirectory: true)
    let directory = root.appendingPathComponent("store", isDirectory: true)
    Snippets.resetForTesting(directory: directory)
    let projectPath = root.appendingPathComponent("project", isDirectory: true).path
    try! FileManager.default.createDirectory(atPath: projectPath,
                                             withIntermediateDirectories: true)
    _ = Snippets.create(from: ["title": "global", "body": "g", "scope": "global"])
    _ = Snippets.create(from: ["title": "local", "body": "l", "scope": "project",
                                      "project": projectPath])
    _ = Snippets.create(from: ["title": "other", "body": "o", "scope": "project",
                                      "project": "/tmp/somewhere-else"])
    let session = TargetSession(backend: .iterm, id: "SNIPPET-SESSION", name: "snippet",
                                tty: "/dev/ttys999", windowIndex: 0, tabIndex: 0,
                                assistant: .codex, cwd: projectPath)
    // A tmux pane id, which is what every terminal this app drives through tmux actually has:
    // `Sources/Tmux.swift` reads it straight off `list-panes` as `%150`. It is here because the
    // route decoded the query value a second time after the request parser had already decoded
    // it, and `%15` is a valid escape — so `%1`–`%9` survived and every pane from `%10` up
    // answered 404. Percent-encoded on the way in, exactly as `net/live.js` sends it.
    let paneSession = TargetSession(backend: .tmux, id: "%150", name: "pane",
                                    tty: "/dev/ttys998", windowIndex: 0, tabIndex: 0,
                                    assistant: .claude, cwd: projectPath)
    RemoteServer.sessionPayloadForTesting = ([session, paneSession], [:])
    let reader = RemoteAuth.addDevice(name: "snippet list reader", caps: [.read])
    defer {
        RemoteAuth.revoke(id: reader.id)
        RemoteServer.sessionPayloadForTesting = nil
        Snippets.resetForTesting()
        try? FileManager.default.removeItem(at: root)
    }
    let headers = ["Authorization": "Bearer \(reader.token)"]
    let all = RemoteServer.shared.route(remoteRequest("GET", "/v1/snippets", headers: headers))
    let allBody = (try? JSONSerialization.jsonObject(with: all.body)) as? [String: Any]
    expect("GET /v1/snippets returns the editor's whole inventory",
           (allBody?["snippets"] as? [[String: Any]])?.count, 3)

    let filtered = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/snippets?session=SNIPPET-SESSION", headers: headers))
    let filteredBody = (try? JSONSerialization.jsonObject(with: filtered.body)) as? [String: Any]
    let project = filteredBody?["project"] as? [String: Any]
    expect("the session answer carries the Mac-resolved key", project?["key"] as? String,
           projectPath)
    let titles = (filteredBody?["snippets"] as? [[String: Any]])?.compactMap {
        $0["title"] as? String
    }
    expect("the session list is project first, then global, with other projects absent",
           titles, ["local", "global"])

    let pane = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/snippets?session=%25150", headers: headers))
    expect("a tmux pane id survives the route", pane.status, 200)
    let paneBody = (try? JSONSerialization.jsonObject(with: pane.body)) as? [String: Any]
    expect("and resolves the same project as any other session",
           (paneBody?["project"] as? [String: Any])?["key"] as? String, projectPath)
    expect("with that session's own list",
           (paneBody?["snippets"] as? [[String: Any]])?.compactMap { $0["title"] as? String },
           ["local", "global"])

    let snapshot = RemoteServer.orchestratorSnapshot(now: Date(timeIntervalSince1970: 1_789_900_000))
    expect("the orchestrator snapshot carries the same full inventory",
           (snapshot["snippets"] as? [[String: Any]])?.count, 3)
}
}
