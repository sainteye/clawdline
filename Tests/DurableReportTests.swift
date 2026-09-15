import Foundation

func runDurableReportTests() {
group("durable reports are explicit, immutable, bounded, restart-safe Project documents") {
    let manager = FileManager.default
    let fixture = manager.temporaryDirectory
        .appendingPathComponent("clawdline-durable-report-\(UUID().uuidString)", isDirectory: true)
    let storeRoot = fixture.appendingPathComponent("durable-reports", isDirectory: true)
    let taskID = UUID().uuidString.lowercased()
    let requestID = UUID().uuidString.lowercased()
    let taskDirectory = URL(fileURLWithPath: "/tmp/.clawdline/\(taskID)", isDirectory: true)
    let artifacts = taskDirectory.appendingPathComponent("artifacts", isDirectory: true)
    try! manager.createDirectory(at: artifacts, withIntermediateDirectories: true,
                                 attributes: [.posixPermissions: 0o700])
    try! manager.createDirectory(at: fixture, withIntermediateDirectories: true,
                                 attributes: [.posixPermissions: 0o700])
    defer {
        DurableReportStore.sharedOverrideForTesting = nil
        try? manager.removeItem(at: taskDirectory)
        try? manager.removeItem(at: fixture)
    }

    let reportURL = artifacts.appendingPathComponent("audit.md")
    let original = Data("# Audit\n\nStable evidence.\n".utf8)
    try! original.write(to: reportURL, options: .atomic)
    let verifiedAt = 1_789_460_000
    let sessionID = "session-durable-a"
    let projectDir = "/Users/example/project"
    func taskRecord(path: String = "audit.md", verified: Bool = true,
                    terminal: String = sessionID) -> [String: Any] {
        var row: [String: Any] = [
            "id": taskID, "state": "success", "projectDir": projectDir,
            "dir": taskDirectory.path, "artifacts": ["artifacts/\(path)"],
            "root": ["terminalId": terminal],
        ]
        if verified { row["resultVerifiedAt"] = verifiedAt }
        return row
    }
    func body(id: String = requestID, path: String = "audit.md") -> [String: Any] {
        ["request_id": id, "task_id": taskID, "session_id": sessionID,
         "path": path, "title": "Durable audit"]
    }
    func replyCode(_ reply: DurableReportStore.Reply) -> String {
        ((reply.body["error"] as? [String: Any])?["code"] as? String) ?? ""
    }
    func validated(_ raw: [String: Any] = body(), key: String? = requestID,
                   record: [String: Any]? = nil, cwd: String? = projectDir)
        -> Result<(DurableReportStore.PromotionRequest,
                   DurableReportStore.TrustedTaskSource), DurableReportStore.Reply> {
        DurableReportPromotion.validate(body: raw, idempotencyKey: key,
                                        taskRecord: record ?? taskRecord(),
                                        sessionProjectDir: cwd)
    }

    let pair: (DurableReportStore.PromotionRequest, DurableReportStore.TrustedTaskSource)
    switch validated() {
    case .success(let value):
        pair = value
        check("an authenticated terminal task/result/artifact tuple authorizes promotion", true)
    case .failure:
        check("an authenticated terminal task/result/artifact tuple authorizes promotion", false)
        return
    }
    var extra = body(); extra["filesystem_path"] = "/etc/passwd"
    check("the promotion request is closed to arbitrary filesystem fields", {
        if case .failure = validated(extra) { return true }; return false
    }())
    check("the idempotency header must equal the immutable request identity", {
        if case .failure = validated(key: UUID().uuidString.lowercased()) { return true }
        return false
    }())
    check("a result without authenticated verification provenance is refused", {
        if case .failure = validated(record: taskRecord(verified: false)) { return true }
        return false
    }())
    check("a file absent from the authenticated result artifact list is refused", {
        if case .failure = validated(record: taskRecord(path: "other.md")) { return true }
        return false
    }())
    check("a different Root Session cannot borrow the task provenance", {
        if case .failure = validated(record: taskRecord(terminal: "session-other")) { return true }
        return false
    }())
    check("a Session in another Project cannot promote the task", {
        if case .failure = validated(cwd: "/Users/example/other") { return true }
        return false
    }())
    var controlledTitle = body(); controlledTitle["title"] = "audit\nforged"
    check("a receipt title cannot carry control characters", {
        if case .failure = validated(controlledTitle) { return true }; return false
    }())
    let promotionRoute = remoteRequest(
        "POST", "/v1/orchestrator/durable-reports/promotions", body: "{}")
    expect("the modular promotion handler refuses absent machine authority",
           RemoteServer.shared.durableReportPromotionRoute(
            promotionRoute, orchestratorAuthed: false)?.status, 403)
    expect("the public promotion route stops an unauthenticated caller at the shared boundary",
           RemoteServer.shared.route(promotionRoute).status, 401)

    let store = DurableReportStore(root: storeRoot)
    let first = store.promote(pair.0, source: pair.1, machineID: "machine-durable-a",
                              now: Date(timeIntervalSince1970: 1_789_470_000))
    expect("the first explicit promotion creates one immutable report", first.status, 201)
    expect("the promotion is not reported as a replay", first.body["replayed"] as? Bool, false)
    let promotion = first.body["promotion"] as? [String: Any]
    expect("the immutable id is the request identity", promotion?["immutable_id"] as? String,
           requestID)
    expect("the receipt counts the exact bytes", promotion?["byte_count"] as? Int, original.count)
    let checksum = promotion?["checksum_sha256"] as? String ?? ""
    check("the receipt carries a SHA-256 checksum", checksum.count == 64)
    check("the receipt carries Project, Session, task and authenticated-result provenance",
          promotion?["project_dir"] as? String == projectDir
            && promotion?["session_id"] as? String == sessionID
            && promotion?["task_id"] as? String == taskID
            && (promotion?["provenance"] as? [String: Any])?["kind"] as? String
                == "authenticated_task_result_artifact")
    expect("a durable report is pinned rather than given silent retention",
           promotion?["pinned"] as? Bool, true)
    let receiptID = promotion?["receipt_id"] as? String ?? ""
    check("the promotion has a distinct immutable receipt", receiptID.hasPrefix("drp-")
          && receiptID.count == 68)
    let document = promotion?["document"] as? [String: Any]
    let publicPath = document?["path"] as? String ?? ""
    check("the receipt points into the existing Project document scope",
          document?["scope"] as? String == "project"
            && publicPath.hasPrefix(DurableReportStore.namespace + "/"))
    let cloudURL = document?["canonical_cloud_url"] as? String ?? ""
    check("the receipt builds a credential-free canonical Cloud identity",
          cloudURL.hasPrefix("https://app.clawdline.com/#document=1&")
            && cloudURL.contains("machine=machine-durable-a")
            && cloudURL.contains("session=session-durable-a")
            && !cloudURL.lowercased().contains("token"))

    let replay = store.promote(pair.0, source: pair.1, machineID: "machine-durable-a")
    expect("same promotion identity and bytes replay successfully", replay.status, 200)
    expect("an exact replay says so", replay.body["replayed"] as? Bool, true)
    expect("an exact replay retains the same receipt",
           (replay.body["promotion"] as? [String: Any])?["receipt_id"] as? String, receiptID)
    try! Data("# changed\n".utf8).write(to: reportURL, options: .atomic)
    let conflict = store.promote(pair.0, source: pair.1, machineID: nil)
    expect("same promotion identity with different bytes conflicts", conflict.status, 409)
    expect("the conflict is typed", replyCode(conflict), "durable_report_promotion_conflict")

    let restarted = DurableReportStore(root: storeRoot)
    let restartedRows: [DurableReportStore.Report]
    switch restarted.reports(projectDir: projectDir) {
    case .success(let value): restartedRows = value
    case .failure: restartedRows = []
    }
    expect("restart reloads one indexed report", restartedRows.count, 1)
    switch restarted.read(sessionID: sessionID, publicPath: publicPath) {
    case .report(let row, let bytes):
        check("restart verifies checksum and byte count against immutable content",
              row.checksumSHA256 == checksum && row.byteCount == bytes.count && bytes == original)
    default:
        check("restart verifies checksum and byte count against immutable content", false)
    }

    try! manager.removeItem(at: taskDirectory)
    switch restarted.read(sessionID: sessionID, publicPath: publicPath) {
    case .report(_, let bytes):
        check("deleting the original task temp does not delete the durable report", bytes == original)
    default:
        check("deleting the original task temp does not delete the durable report", false)
    }
    let cleanedReplay = restarted.promote(pair.0, source: pair.1,
                                          machineID: "machine-durable-a")
    expect("an exact replay survives ordinary whole-task cleanup", cleanedReplay.status, 200)
    check("a Project list never relabels an older report with another Session identity",
          RemoteServer.shared.documentsPayload(cwd: projectDir, sessionID: "new-session",
                                               durable: restartedRows).isEmpty)
    check("the receipt-bound Session still discovers its promoted report",
          RemoteServer.shared.documentsPayload(cwd: projectDir, sessionID: sessionID,
                                               durable: restartedRows).count == 1)
    check("control characters cannot enter a canonical Cloud machine identity",
          DurableReportStore.canonicalCloudURL(machineID: "machine\nforged",
                                               sessionID: sessionID,
                                               publicPath: publicPath) == nil)
    DurableReportStore.sharedOverrideForTesting = restarted
    let durableRoute = "/v1/sessions/\(sessionID)/documents/project/"
        + ProjectDocuments.escaped(publicPath)
    let direct = RemoteServer.shared.documentsRoute(durableRoute)
    check("the local ProjectDocuments route reads a receipt after the Session is no longer live",
          direct.status == 200 && direct.body == original)
    let unauthenticated = RemoteServer.shared.route(remoteRequest("GET", durableRoute))
    expect("the permanent route does not bypass document authentication",
           unauthenticated.status, 401)
    let reader = RemoteAuth.addDevice(name: "durable report reader", caps: [.read])
    defer { RemoteAuth.revoke(id: reader.id) }
    let paired = RemoteServer.shared.route(remoteRequest(
        "GET", durableRoute, headers: ["Authorization": "Bearer \(reader.token)"]))
    check("the paired/Cloud read boundary returns the same immutable bytes",
          paired.status == 200 && paired.body == original)

    let board = BoardTestDriver(name: "durable-report-board-\(UUID().uuidString)")
    board.createProject()
    let item = board.create(type: "feature", title: "Audit trail")
    let before = board.item(item)
    let reference = board.send("document_reference", [
        "itemId": item, "title": "Durable audit", "purpose": "reference",
        "documentId": receiptID, "version": 1, "url": cloudURL,
    ])
    expect("Board accepts the promotion receipt as a canonical document reference",
           reference.status, 200)
    let after = board.item(item)
    check("Board stores only narrative receipt authority",
          ((after["documentReferences"] as? [[String: Any]])?.first?["authority"] as? String)
            == "narrative_only")
    check("a durable reference does not promote lifecycle, verification or landing",
          after["state"] as? String == before["state"] as? String
            && after["scopeRevision"] as? Int == before["scopeRevision"] as? Int
            && (after["verifications"] as? [[String: Any]])?.count
                == (before["verifications"] as? [[String: Any]])?.count
            && (after["landings"] as? [[String: Any]])?.count
                == (before["landings"] as? [[String: Any]])?.count)

    // Every hostile source shape reaches the same resolver and descriptor boundary as production.
    let boundaryTaskID = UUID().uuidString.lowercased()
    let boundaryTask = URL(fileURLWithPath: "/tmp/.clawdline/\(boundaryTaskID)",
                           isDirectory: true)
    let boundaryArtifacts = boundaryTask.appendingPathComponent("artifacts", isDirectory: true)
    try! manager.createDirectory(at: boundaryArtifacts, withIntermediateDirectories: true,
                                 attributes: [.posixPermissions: 0o700])
    defer { try? manager.removeItem(at: boundaryTask) }
    let boundaryRoot = fixture.appendingPathComponent("boundary/durable-reports", isDirectory: true)
    var boundaryPolicy = DurableReportStore.Policy()
    boundaryPolicy.maximumReportBytes = 64
    let boundaryStore = DurableReportStore(root: boundaryRoot, policy: boundaryPolicy)
    func boundarySource(_ path: String) -> DurableReportStore.TrustedTaskSource {
        .init(taskID: boundaryTaskID, sessionID: "boundary-session",
              projectDir: projectDir, taskDirectory: boundaryTask.path,
              path: path, title: "Boundary report", resultVerifiedAt: verifiedAt)
    }
    func boundaryRequest(_ path: String) -> DurableReportStore.PromotionRequest {
        .init(requestID: UUID().uuidString.lowercased(), taskID: boundaryTaskID,
              sessionID: "boundary-session", path: path, title: "Boundary report")
    }
    try! Data("hidden".utf8).write(to: boundaryArtifacts.appendingPathComponent(".hidden.md"))
    try! Data("unsafe".utf8).write(to: boundaryArtifacts.appendingPathComponent("run.sh"))
    try! Data("task_secret: never-copy-this".utf8)
        .write(to: boundaryArtifacts.appendingPathComponent("secret.md"))
    try! Data(repeating: 0x61, count: 65)
        .write(to: boundaryArtifacts.appendingPathComponent("large.md"))
    let outside = fixture.appendingPathComponent("outside.md")
    try! Data("outside".utf8).write(to: outside)
    try! manager.createSymbolicLink(at: boundaryArtifacts.appendingPathComponent("escape.md"),
                                    withDestinationURL: outside)
    let linkedOutside = fixture.appendingPathComponent("linked.md")
    try! manager.linkItem(at: outside, to: linkedOutside)
    try! manager.linkItem(at: outside,
                          to: boundaryArtifacts.appendingPathComponent("hardlink.md"))
    for (name, path, status) in [
        ("traversal", "../task.json", 404), ("hidden", ".hidden.md", 404),
        ("unsafe extension", "run.sh", 404), ("symlink escape", "escape.md", 404),
        ("hard link", "hardlink.md", 415), ("secret marker", "secret.md", 422),
        ("oversize", "large.md", 413),
    ] {
        let refused = boundaryStore.promote(boundaryRequest(path),
                                            source: boundarySource(path), machineID: nil)
        expect("\(name) source is refused", refused.status, status)
    }

    // A committed object with no index is an identifiable crash orphan, charged to quota and
    // healed only by replaying the exact promotion request.
    let crashTaskID = UUID().uuidString.lowercased()
    let crashTask = URL(fileURLWithPath: "/tmp/.clawdline/\(crashTaskID)", isDirectory: true)
    let crashArtifacts = crashTask.appendingPathComponent("artifacts", isDirectory: true)
    try! manager.createDirectory(at: crashArtifacts, withIntermediateDirectories: true)
    try! Data("crash-safe\n".utf8).write(to: crashArtifacts.appendingPathComponent("report.txt"))
    defer { try? manager.removeItem(at: crashTask) }
    let crashID = UUID().uuidString.lowercased()
    let crashRequest = DurableReportStore.PromotionRequest(
        requestID: crashID, taskID: crashTaskID, sessionID: "crash-session",
        path: "report.txt", title: "Crash report")
    let crashSource = DurableReportStore.TrustedTaskSource(
        taskID: crashTaskID, sessionID: "crash-session", projectDir: projectDir,
        taskDirectory: crashTask.path, path: "report.txt", title: "Crash report",
        resultVerifiedAt: verifiedAt)
    let crashRoot = fixture.appendingPathComponent("crash/durable-reports", isDirectory: true)
    var crashPolicy = DurableReportStore.Policy()
    crashPolicy.maximumReports = 1
    crashPolicy.maximumReportsPerProject = 1
    let interrupted = DurableReportStore(root: crashRoot, policy: crashPolicy,
                                         fault: { $0 == .afterObjectCommit })
    expect("a crash after object fsync never reports a successful promotion",
           interrupted.promote(crashRequest, source: crashSource, machineID: nil).status, 503)
    let recovered = DurableReportStore(root: crashRoot, policy: crashPolicy)
    expect("restart replay heals the exact orphan even at the count quota and writes its index",
           recovered.promote(crashRequest, source: crashSource, machineID: nil).status, 201)
    let staleIndex = crashRoot.appendingPathComponent(
        ".index.\(UUID().uuidString.lowercased()).new")
    try! Data("partial".utf8).write(to: staleIndex)
    try! manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: staleIndex.path)
    let afterIndexCrash = DurableReportStore(root: crashRoot, policy: crashPolicy)
    check("restart removes only a recognized private unpublished index temporary",
          !manager.fileExists(atPath: staleIndex.path))
    if case .report(_, let bytes) = afterIndexCrash.read(
        sessionID: "crash-session",
        publicPath: DurableReportStore.namespace + "/\(crashID).txt") {
        check("cleaning the index temporary preserves the committed report", bytes == Data("crash-safe\n".utf8))
    } else {
        check("cleaning the index temporary preserves the committed report", false)
    }

    let unknownRoot = fixture.appendingPathComponent("unknown/durable-reports", isDirectory: true)
    try! manager.createDirectory(at: unknownRoot, withIntermediateDirectories: true,
                                 attributes: [.posixPermissions: 0o700])
    let unknownEntry = unknownRoot.appendingPathComponent("unexpected")
    try! Data("unknown".utf8).write(to: unknownEntry)
    try! manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: unknownEntry.path)
    let unknownStore = DurableReportStore(root: unknownRoot)
    check("an unknown managed-root entry fails closed and is preserved", {
        guard manager.fileExists(atPath: unknownEntry.path) else { return false }
        if case .failure = unknownStore.reports(projectDir: projectDir) { return true }
        return false
    }())

    let objectURL = storeRoot.appendingPathComponent("objects/\(requestID).md")
    let indexURL = storeRoot.appendingPathComponent("index.json")
    func mode(_ url: URL) -> Int {
        ((try? manager.attributesOfItem(atPath: url.path)[.posixPermissions]) as? NSNumber)?
            .intValue ?? -1
    }
    check("managed directories are private 0700", mode(storeRoot) == 0o700
          && mode(storeRoot.appendingPathComponent("objects")) == 0o700)
    check("the immutable object and atomic index are private 0600",
          mode(objectURL) == 0o600 && mode(indexURL) == 0o600)

    var onePolicy = DurableReportStore.Policy()
    onePolicy.maximumReports = 1
    onePolicy.maximumReportsPerProject = 1
    let quotaTaskID = UUID().uuidString.lowercased()
    let quotaTask = URL(fileURLWithPath: "/tmp/.clawdline/\(quotaTaskID)", isDirectory: true)
    let quotaArtifacts = quotaTask.appendingPathComponent("artifacts", isDirectory: true)
    try! manager.createDirectory(at: quotaArtifacts, withIntermediateDirectories: true)
    try! Data("one\n".utf8).write(to: quotaArtifacts.appendingPathComponent("one.md"))
    try! Data("two\n".utf8).write(to: quotaArtifacts.appendingPathComponent("two.md"))
    defer { try? manager.removeItem(at: quotaTask) }
    let quotaStore = DurableReportStore(
        root: fixture.appendingPathComponent("quota/durable-reports", isDirectory: true),
        policy: onePolicy)
    func quotaPair(_ path: String) -> (DurableReportStore.PromotionRequest,
                                       DurableReportStore.TrustedTaskSource) {
        let id = UUID().uuidString.lowercased()
        return (.init(requestID: id, taskID: quotaTaskID, sessionID: "quota-session",
                      path: path, title: path),
                .init(taskID: quotaTaskID, sessionID: "quota-session", projectDir: projectDir,
                      taskDirectory: quotaTask.path, path: path, title: path,
                      resultVerifiedAt: verifiedAt))
    }
    let kept = quotaPair("one.md")
    expect("the first report fits the explicit count quota",
           quotaStore.promote(kept.0, source: kept.1, machineID: nil).status, 201)
    let denied = quotaPair("two.md")
    expect("quota exhaustion refuses instead of evicting a pinned report",
           quotaStore.promote(denied.0, source: denied.1, machineID: nil).status, 409)
    let keptPath = DurableReportStore.namespace + "/\(kept.0.requestID).md"
    if case .report = quotaStore.read(sessionID: "quota-session", publicPath: keptPath) {
        check("the pinned report remains readable after quota refusal", true)
    } else {
        check("the pinned report remains readable after quota refusal", false)
    }

    let validIndex = try! Data(contentsOf: indexURL)
    var malformed = try! JSONSerialization.jsonObject(with: validIndex) as! [String: Any]
    var malformedRows = malformed["reports"] as! [[String: Any]]
    malformedRows[0]["session_id"] = ""
    malformed["reports"] = malformedRows
    try! JSONSerialization.data(withJSONObject: malformed, options: [.sortedKeys])
        .write(to: indexURL, options: .atomic)
    try! manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: indexURL.path)
    check("structurally invalid stored provenance fails closed", {
        if case .failure = DurableReportStore(root: storeRoot).reports(projectDir: projectDir) {
            return true
        }
        return false
    }())
    try! validIndex.write(to: indexURL, options: .atomic)
    try! manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: indexURL.path)
    try! Data("tampered\n".utf8).write(to: objectURL, options: .atomic)
    try! manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: objectURL.path)
    expect("a replay never reports success for a corrupted durable object",
           restarted.promote(pair.0, source: pair.1, machineID: nil).status, 503)

    var corrupt = try! JSONSerialization.jsonObject(with: Data(contentsOf: indexURL))
        as! [String: Any]
    corrupt["schema_version"] = 99
    try! JSONSerialization.data(withJSONObject: corrupt, options: [.sortedKeys])
        .write(to: indexURL, options: .atomic)
    try! manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: indexURL.path)
    let damaged = DurableReportStore(root: storeRoot)
    check("an unknown index version fails closed instead of showing an empty report list", {
        if case .failure = damaged.reports(projectDir: projectDir) { return true }; return false
    }())
    expect("an unknown index version cannot be overwritten by a new promotion",
           damaged.promote(pair.0, source: pair.1, machineID: nil).status, 503)
}
}
