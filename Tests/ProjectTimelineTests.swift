import Foundation

private final class TimelineTestDriver {
    let root: URL
    let file: URL
    let store: ProjectTimelineStore
    init(_ name: String = UUID().uuidString) {
        root = FileManager.default.temporaryDirectory.appendingPathComponent("clawdline-timeline-" + name)
        file = root.appendingPathComponent("timeline.json")
        try? FileManager.default.removeItem(at: root)
        store = ProjectTimelineStore(url: file)
    }
    deinit { try? FileManager.default.removeItem(at: root) }
}

private let productionAPI = ProjectTimelineTarget(environment: "production", component: "api", audience: "all")
private let productionMac = ProjectTimelineTarget(environment: "production", component: "mac", audience: "all")
private let productionPages = ProjectTimelineTarget(environment: "production", component: "pages", audience: "all")

private func timelineEntry(_ id: String = "tl-one", project: String = "project-one",
                           targets: [ProjectTimelineTarget] = [productionAPI],
                           boards: [String] = ["board-stable"], relations: [ProjectTimelineRelation] = [],
                           revisions: [ProjectTimelineRevision] = []) -> ProjectTimelineEntry {
    ProjectTimelineEntry(id: id, projectID: project, deliveryKey: "delivery-" + id,
        primaryCategory: "feature", tags: ["server"], originalTitle: "A shipped feature",
        summary: "One readable delivery.", requiredTargets: targets, boardItemIDs: boards,
        relations: relations, sourceRevisions: revisions, createdAt: 1_788_969_600)
}

private func timelineEvent(_ id: String, entry: String = "tl-one", kind: String,
                           target: ProjectTimelineTarget? = productionAPI,
                           effective: Double? = 100, observed: Double = 110,
                           revision: String? = "aaaaaaaa", previous: String? = nil,
                           result: String = "passed", cohort: Double? = nil,
                           before: String? = nil, after: String? = nil,
                           verification: String? = nil,
                           producer: String = "fixture", source: String? = nil) -> ProjectTimelineEvent {
    ProjectTimelineEvent(id: id, producer: producer, sourceID: source ?? id, sourceVersion: 1,
        entryID: entry, kind: kind, target: target, effectiveAt: effective, observedAt: observed,
        authority: "provider_receipt", revision: revision, previousRevision: previous,
        result: result, cohortPercent: cohort, beforeDigest: before, afterDigest: after,
        verification: verification, sourceURL: nil)
}

private func timelineStatus(_ entry: ProjectTimelineEntry, _ events: [ProjectTimelineEvent]) -> String {
    ProjectTimelineReconciler.project(entry, events: events).status
}

private func timelineError(_ reply: ProjectTimelineStore.Reply) -> String {
    ((reply.body["error"] as? [String: Any])?["code"] as? String) ?? ""
}

@discardableResult
private func timelineGit(_ root: URL, _ arguments: [String]) -> Bool {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/git")
    process.arguments = ["-C", root.path] + arguments
    process.standardOutput = FileHandle.nullDevice
    process.standardError = FileHandle.nullDevice
    var environment = ProcessInfo.processInfo.environment
    environment["GIT_AUTHOR_NAME"] = "Timeline Fixture"
    environment["GIT_AUTHOR_EMAIL"] = "timeline@example.invalid"
    environment["GIT_COMMITTER_NAME"] = "Timeline Fixture"
    environment["GIT_COMMITTER_EMAIL"] = "timeline@example.invalid"
    process.environment = environment
    do { try process.run(); process.waitUntilExit(); return process.terminationStatus == 0 }
    catch { return false }
}

func runProjectTimelineTests() {
group("Project Timeline keeps Git, deployment, availability, rollback, and mode facts distinct") {
    let basic = timelineEntry(revisions: [.init(repositoryID: "github:sainteye/clawdline",
        commit: "6f8aa65ea0e2211d3b359ec583c7cdf2aa24ab48", role: "mac_pages")])
    expect("commit-only is in Git rather than available", timelineStatus(basic, [
        timelineEvent("git", kind: "landed_to_git", target: nil)]), "landed_to_git")
    expect("deploy without availability waits", timelineStatus(basic, [
        timelineEvent("deploy", kind: "deploy_succeeded")]), "deploy_pending")
    expect("matching production revision becomes available", timelineStatus(basic, [
        timelineEvent("deploy", kind: "deploy_succeeded"),
        timelineEvent("health", kind: "availability_verified")]), "available")
    expect("wrong health revision cannot become available", timelineStatus(basic, [
        timelineEvent("deploy", kind: "deploy_succeeded", revision: "aaaaaaaa"),
        timelineEvent("health", kind: "availability_verified", revision: "bbbbbbbb")]), "deploy_pending")
    let crossRepository = timelineEntry("tl-cross", revisions: [
        .init(repositoryID: "github:sainteye/clawdline", commit: "6f8aa65ea0e2211d3b359ec583c7cdf2aa24ab48", role: "mac"),
        .init(repositoryID: "github:sainteye/clawdline-cloud", commit: "baefe2a3aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", role: "cloud")])
    expect("a cross-repository commit tuple without deployment remains unknown",
           timelineStatus(crossRepository, []), "unknown")
    let staging = ProjectTimelineTarget(environment: "staging", component: "api", audience: "all")
    expect("staging evidence cannot become production", timelineStatus(basic, [
        timelineEvent("sd", kind: "deploy_succeeded", target: staging),
        timelineEvent("sh", kind: "availability_verified", target: staging)]), "unknown")
    let stagingEntry = timelineEntry("tl-staging", targets: [staging])
    expect("the same evidence projects available when staging is selected",
           ProjectTimelineReconciler.project(stagingEntry, events: [
            timelineEvent("s2d", entry: stagingEntry.id, kind: "deploy_succeeded", target: staging),
            timelineEvent("s2h", entry: stagingEntry.id, kind: "availability_verified", target: staging)
           ], environment: "staging").status, "available")

    let triplet = timelineEntry(targets: [productionMac, productionPages, productionAPI])
    let partial = [
        timelineEvent("md", kind: "deploy_succeeded", target: productionMac),
        timelineEvent("mh", kind: "availability_verified", target: productionMac),
        timelineEvent("pd", kind: "deploy_succeeded", target: productionPages),
        timelineEvent("ph", kind: "availability_verified", target: productionPages),
        timelineEvent("af", kind: "deploy_failed", target: productionAPI, result: "failed")]
    let partialProjection = ProjectTimelineReconciler.project(triplet, events: partial)
    expect("two of three targets are partial", partialProjection.status, "partial")
    expect("partial target count stays visible", partialProjection.availableTargets, 2)
    expect("required target count stays visible", partialProjection.requiredTargets, 3)
    let cohort = ProjectTimelineTarget(environment: "production", component: "api", audience: "cohort:10")
    expect("a cohort is limited rather than fully available", timelineStatus(basic, [
        timelineEvent("cd", kind: "deploy_succeeded"),
        timelineEvent("ch", kind: "availability_verified", target: cohort, cohort: 10)]), "limited")

    expect("failed deploy stays failed", timelineStatus(basic, [
        timelineEvent("fail", kind: "deploy_failed", result: "failed")]), "deploy_failed")
    expect("full rollback points to rolled back", timelineStatus(basic, [
        timelineEvent("rollback", kind: "rollback", previous: "99999999")]), "rolled_back")
    expect("one component rollback is partial", timelineStatus(triplet, [
        timelineEvent("partial-rollback", kind: "rollback", target: productionMac,
                      previous: "99999999")]), "partial_rollback")
    expect("a later production deploy supersedes an older rollback", timelineStatus(basic, [
        timelineEvent("old-rollback", kind: "rollback", effective: 80, previous: "99999999"),
        timelineEvent("new-deploy", kind: "deploy_succeeded", effective: 100),
        timelineEvent("new-health", kind: "availability_verified", effective: 110)]), "available")
    expect("a new deployment cannot reuse an older revision health receipt", timelineStatus(basic, [
        timelineEvent("old-deploy", kind: "deploy_succeeded", effective: 70, revision: "aaaaaaaa"),
        timelineEvent("old-health", kind: "availability_verified", effective: 80, revision: "aaaaaaaa"),
        timelineEvent("newer-deploy", kind: "deploy_succeeded", effective: 100, revision: "bbbbbbbb")]), "deploy_pending")

    let first = timelineEntry("tl-first"), second = timelineEntry("tl-second")
    check("same commit message never groups entries", first.id != second.id && first.deliveryKey != second.deliveryKey)
    let revert = timelineEntry("tl-revert", relations: [.init(kind: "reverts", targetEntryID: first.id)])
    expect("revert relation preserves the original stable id", revert.relations.first?.targetEntryID, first.id)
    let delayed = ProjectTimelineReconciler.project(basic, events: [
        timelineEvent("dd", kind: "deploy_succeeded", effective: 90, observed: 200),
        timelineEvent("dh", kind: "availability_verified", effective: 95, observed: 220)])
    expect("delayed evidence keeps effective time", delayed.effectiveAt, 95)
    expect("delayed evidence keeps later observed time", delayed.observedAt, 220)

    expect("SSH GitHub remotes canonicalize", ProjectTimelineRemote.repositoryID("git@github.com:Sainteye/Clawdline.git"), "github:sainteye/clawdline")
    expect("HTTPS GitHub remotes canonicalize", ProjectTimelineRemote.repositoryID("https://github.com/Sainteye/Clawdline.git"), "github:sainteye/clawdline")
    expect("non-GitHub remote gets no external identity", ProjectTimelineRemote.repositoryID("ssh://private.example/repo.git"), nil)
    expect("canonical revision gets a commit link", basic.sourceRevisions.first?.githubURL,
           "https://github.com/sainteye/clawdline/commit/6f8aa65ea0e2211d3b359ec583c7cdf2aa24ab48")
    expect("short or malformed revision gets no dead link", ProjectTimelineRemote.commitURL(repositoryID: "github:sainteye/clawdline", commit: "xyz"), nil)

    let cloudRead = CloudHeadlessRead.timeline(session: CloudAppBridge.machineReplySession,
        request: "timeline-read", project: "project-one", entry: "tl-one", cursor: "40",
        environment: "staging", category: "feature", upcoming: true)
    check("Timeline belongs to the closed Cloud read vocabulary",
          CloudAppBridge.readTypes.contains("timeline"))
    let cloudRoute = CloudLocalRoute(read: cloudRead)
    expect("encrypted Cloud read resolves to the local Timeline route", cloudRoute.path, "/v1/timeline")
    expect("Cloud read preserves the selected environment", cloudRoute.query["environment"], "staging")
    expect("Cloud read reply remains bound to its request", cloudRead.name, "read:timeline-read")
    let commandBytes = Data(#"{"operation":"set_enabled"}"#.utf8)
    let cloudCommand = CloudLocalRoute(command: .timeline(body: commandBytes))
    expect("encrypted Cloud command resolves to the local Timeline route", cloudCommand.path, "/v1/timeline")
    expect("Cloud command remains a POST", cloudCommand.method, "POST")
    expect("Cloud command body is unchanged", cloudCommand.body, commandBytes)

    let httpDriver = TimelineTestDriver("http-authority")
    ProjectTimelineHTTP.configureStoreForTesting(httpDriver.store)
    defer { ProjectTimelineHTTP.configureStoreForTesting(nil) }
    let oldRemoteWrite = Config.shared.remoteWrite
    Config.shared.remoteWrite = true
    defer { Config.shared.remoteWrite = oldRemoteWrite }
    let pairedRead = ProjectTimelineHTTP.admit(remoteRequest("GET", "/v1/timeline?project=project-one"),
        machine: false, permission: .allowed(device: "paired-reader", caps: [.read]))
    if case .some(.read(let prepared)) = pairedRead {
        expect("paired Timeline read keeps its authenticated principal",
               prepared.viewer["id"] as? String, "paired-reader")
        expect("paired Timeline reader is read-only",
               prepared.viewer["canWrite"] as? Bool, false)
    } else { check("paired Timeline read enters the bounded read admission", false) }
    let modeBody = #"{"operation":"set_enabled","requestId":"mode","expectedRevision":0,"enabled":false}"#
    let nonAdminMode = ProjectTimelineHTTP.route(remoteRequest("POST", "/v1/timeline", body: modeBody),
        machine: false, permission: .allowed(device: "paired-writer", caps: [.read, .send]))
    expect("non-admin direct device cannot change Timeline mode", nonAdminMode?.status, 403)
    let ingestBody = #"{"operation":"ingest","requestId":"ingest","expectedRevision":0}"#
    let remoteIngest = ProjectTimelineHTTP.route(remoteRequest("POST", "/v1/timeline", body: ingestBody),
        machine: false, permission: .allowed(device: "paired-admin", caps: [.read, .send, .admin]))
    expect("even a paired admin cannot ingest Timeline evidence", remoteIngest?.status, 403)
    let verifiedCloudCommand = RemoteServer.Request(verifiedCloud: .timeline(body: Data(modeBody.utf8)),
        sender: "signed-device", idempotencyKey: "cloud-mode")
    let cloudAdmission = ProjectTimelineHTTP.admit(verifiedCloudCommand, machine: false,
        permission: .allowed(device: "cloud:signed-device", caps: [.read, .send]))
    if case .some(.command(let prepared)) = cloudAdmission {
        expect("encrypted Timeline command is bound to verified sender principal",
               prepared.actor, "cloud:signed-device")
    } else { check("encrypted Timeline command reaches closed command admission", false) }

    let operation = timelineEntry("tl-op", targets: [])
    let applied = ProjectTimelineEvent(id: "op", producer: "fixture", sourceID: "op", sourceVersion: 1,
        entryID: operation.id, kind: "operation_applied", target: productionAPI, effectiveAt: 100,
        observedAt: 110, authority: "provider_receipt", revision: nil, previousRevision: nil,
        result: "passed", cohortPercent: nil, beforeDigest: "dns-before", afterDigest: "dns-after",
        verification: nil, sourceURL: nil)
    expect("unverified operation waits", timelineStatus(operation, [applied]), "operation_pending")
    let verified = timelineEvent("opv", entry: operation.id, kind: "operation_verified",
        target: productionAPI, effective: 120, revision: nil, before: "dns-before",
        after: "dns-after", verification: "dns-readback")
    expect("verified operation becomes available", timelineStatus(operation, [applied, verified]), "available")
    let oldVerification = timelineEvent("old-opv", entry: operation.id, kind: "operation_verified",
        target: productionAPI, effective: 90, revision: nil, before: "dns-before",
        after: "dns-after", verification: "old-readback")
    expect("verification older than the operation cannot turn it green",
           timelineStatus(operation, [oldVerification, applied]), "operation_pending")
    let wrongDigest = timelineEvent("wrong-opv", entry: operation.id, kind: "operation_verified",
        target: productionAPI, effective: 120, revision: nil, before: "other-before",
        after: "other-after", verification: "unrelated-readback")
    expect("unrelated digest verification cannot turn an operation green",
           timelineStatus(operation, [applied, wrongDigest]), "operation_pending")
    let wrongProducer = timelineEvent("foreign-opv", entry: operation.id, kind: "operation_verified",
        target: productionAPI, effective: 120, revision: nil, before: "dns-before",
        after: "dns-after", verification: "foreign-readback", producer: "other-producer")
    expect("another producer cannot verify this operation",
           timelineStatus(operation, [applied, wrongProducer]), "operation_pending")
    let newerApplied = timelineEvent("new-op", entry: operation.id, kind: "operation_applied",
        target: productionAPI, effective: 200, revision: nil, before: "dns-before-2",
        after: "dns-after-2")
    expect("a newer operation invalidates an older verification",
           timelineStatus(operation, [applied, verified, newerApplied]), "operation_pending")
    let newerVerified = timelineEvent("new-opv", entry: operation.id, kind: "operation_verified",
        target: productionAPI, effective: 220, revision: nil, before: "dns-before-2",
        after: "dns-after-2", verification: "new-readback")
    expect("the latest operation becomes available only with its own later verification",
           timelineStatus(operation, [applied, verified, newerApplied, newerVerified]), "available")
}

group("Project Timeline persistence is append-only, idempotent, independently switchable, and stale-readable") {
    let d = TimelineTestDriver()
    let entry = timelineEntry()
    let git = timelineEvent("persist-git", kind: "landed_to_git", target: nil)
    let first = d.store.ingest(entry: entry, events: [git], producer: "fixture", sourceVersion: 1)
    expect("first immutable event is accepted", first.status, "accepted")
    let replay = d.store.ingest(entry: entry, events: [git], producer: "fixture", sourceVersion: 1)
    expect("same receipt replay is unchanged", replay.status, "unchanged")
    let conflict = ProjectTimelineEvent(id: "different-id", producer: "fixture", sourceID: git.sourceID,
        sourceVersion: 1, entryID: entry.id, kind: "landed_to_git", target: nil,
        effectiveAt: nil, observedAt: 999, authority: "provider_receipt", revision: "aaaaaaaa",
        previousRevision: nil, result: "passed", cohortPercent: nil, beforeDigest: nil,
        afterDigest: nil, verification: nil, sourceURL: nil)
    expect("same source identity with different content conflicts",
           d.store.ingest(entry: entry, events: [conflict], producer: "fixture", sourceVersion: 1).reason,
           "timeline_event_conflict")
    let duplicateDriver = TimelineTestDriver("same-batch-duplicate")
    let duplicate = duplicateDriver.store.ingest(entry: entry, events: [git, git],
        producer: "fixture", sourceVersion: 1)
    expect("same-batch identical identities persist one event", duplicate.accepted, 1)
    let duplicateEnvelope = duplicateDriver.store.materializedEnvelope()["timeline"] as? [String: Any]
    let duplicateRow = (duplicateEnvelope?["entries"] as? [[String: Any]])?.first
    expect("same-batch identical evidence is deduplicated atomically",
           (duplicateRow?["events"] as? [[String: Any]])?.count, 1)
    let conflictDriver = TimelineTestDriver("same-batch-conflict")
    let batchConflict = conflictDriver.store.ingest(entry: entry, events: [git, conflict],
        producer: "fixture", sourceVersion: 1)
    expect("same-batch different content conflicts", batchConflict.reason,
           "timeline_event_conflict")
    let conflictEnvelope = conflictDriver.store.materializedEnvelope()["timeline"] as? [String: Any]
    expect("same-batch conflict persists no partial entry",
           (conflictEnvelope?["entries"] as? [[String: Any]])?.count, 0)
    let savedRevision = d.store.readHeader().revision
    let restored = ProjectTimelineStore(url: d.file)
    expect("persisted revision survives a new store", restored.readHeader().revision, savedRevision)
    let snapshot = restored.materializedEnvelope()["timeline"] as? [String: Any]
    expect("persisted history survives a new store", (snapshot?["entries"] as? [[String: Any]])?.count, 1)
    let row = (snapshot?["entries"] as? [[String: Any]])?.first
    expect("stable Board id persists without a title join", row?["boardItemIds"] as? [String], ["board-stable"])

    let off = d.store.command(["operation": "set_enabled", "requestId": "off",
        "expectedRevision": d.store.readHeader().revision, "enabled": false], actor: "admin", trusted: true)
    expect("Timeline mode turns off independently", off.status, 200)
    expect("Timeline OFF refuses new ingestion", d.store.ingest(entry: timelineEntry("tl-later"),
        events: [timelineEvent("later", entry: "tl-later", kind: "landed_to_git", target: nil)],
        producer: "fixture", sourceVersion: 1).reason, "timeline_disabled")
    expect("Timeline OFF keeps existing history", ((d.store.materializedEnvelope()["timeline"] as? [String: Any])?["entries"] as? [[String: Any]])?.count, 1)

    let on = d.store.command(["operation": "set_enabled", "requestId": "on",
        "expectedRevision": d.store.readHeader().revision, "enabled": true], actor: "admin", trusted: true)
    expect("Timeline can be re-enabled without changing Board", on.status, 200)

    let boardRoot = d.root.appendingPathComponent("board.json")
    let board = ProjectBoardStore(url: boardRoot)
    let boardOff = board.command(["operation": "set_enabled", "requestId": "board-off",
        "expectedRevision": board.readHeader().revision, "enabled": false], actor: "admin", trusted: true)
    expect("Board can be OFF without touching Timeline history", boardOff.status, 200)
    expect("Board OFF leaves Timeline ON", d.store.enabled, true)
    let boardIndependent = d.store.ingest(entry: timelineEntry("tl-board-off"),
        events: [timelineEvent("board-off-git", entry: "tl-board-off", kind: "landed_to_git", target: nil)],
        producer: "fixture", sourceVersion: 1)
    expect("Board OFF does not stop Timeline ingestion", boardIndependent.status, "accepted")

    var queued: [() -> Void] = []
    let cache = ProjectTimelineReadCache(execute: { queued.append($0) })
    let model = ProjectTimelineReadCache.Model(revision: savedRevision, observedAt: 10,
                                                envelope: restored.materializedEnvelope())
    cache.seed(model)
    let query = ProjectTimelineReadCache.Query(project: "project-one", entry: nil, cursor: 0,
        environment: "production", category: nil, includeUpcoming: true)
    _ = cache.read(query, header: .init(enabled: true, revision: savedRevision + 1),
                   loading: [:]) { .failure(.init(code: "source_failed", message: "failed")) }
    expect("stale cache admits one refresh", queued.count, 1)
    cache.refresh(header: .init(enabled: true, revision: savedRevision + 2)) { .success(model) }
    cache.refresh(header: .init(enabled: true, revision: savedRevision + 3)) { .success(model) }
    check("event flood retains only one consolidated successor", cache.stateForTesting.successorPending)
    queued.removeFirst()()
    let staleEnvelope = cache.read(query,
        header: .init(enabled: true, revision: savedRevision + 1), loading: [:]) {
            .success(model)
        }
    let staleTimeline = staleEnvelope["timeline"] as? [String: Any]
    expect("failure keeps the earlier readable model", staleTimeline?["status"] as? String, "stale")
    expect("single-flight starts one consolidated successor", queued.count, 1)

    let orderingDriver = TimelineTestDriver("environment-order")
    let stagingAPI = ProjectTimelineTarget(environment: "staging", component: "api", audience: "all")
    let newerProduction = timelineEntry("tl-production-newer", targets: [productionAPI, stagingAPI])
    let newerStaging = timelineEntry("tl-staging-newer", targets: [productionAPI, stagingAPI])
    _ = orderingDriver.store.ingest(entry: newerProduction, events: [
        timelineEvent("pnew-pd", entry: newerProduction.id, kind: "deploy_succeeded", effective: 400),
        timelineEvent("pnew-ph", entry: newerProduction.id, kind: "availability_verified", effective: 410),
        timelineEvent("pnew-sd", entry: newerProduction.id, kind: "deploy_succeeded", target: stagingAPI, effective: 100),
        timelineEvent("pnew-sh", entry: newerProduction.id, kind: "availability_verified", target: stagingAPI, effective: 110),
    ], producer: "fixture", sourceVersion: 1)
    _ = orderingDriver.store.ingest(entry: newerStaging, events: [
        timelineEvent("snew-pd", entry: newerStaging.id, kind: "deploy_succeeded", effective: 200),
        timelineEvent("snew-ph", entry: newerStaging.id, kind: "availability_verified", effective: 210),
        timelineEvent("snew-sd", entry: newerStaging.id, kind: "deploy_succeeded", target: stagingAPI, effective: 500),
        timelineEvent("snew-sh", entry: newerStaging.id, kind: "availability_verified", target: stagingAPI, effective: 510),
    ], producer: "fixture", sourceVersion: 1)
    let orderingEnvelope = orderingDriver.store.materializedEnvelope()
    let orderingTimeline = orderingEnvelope["timeline"] as? [String: Any]
    let productionOrder = (orderingTimeline?["entries"] as? [[String: Any]])?.compactMap { $0["id"] as? String }
    expect("durable model keeps production order", productionOrder,
           [newerProduction.id, newerStaging.id])
    let orderingCache = ProjectTimelineReadCache(execute: { $0() })
    orderingCache.seed(.init(revision: orderingDriver.store.readHeader().revision,
                             observedAt: 600, envelope: orderingEnvelope))
    let stagingEnvelope = orderingCache.read(.init(project: "project-one", entry: nil, cursor: 0,
        environment: "staging", category: nil, includeUpcoming: true),
        header: orderingDriver.store.readHeader(), loading: [:]) { .success(.init(
            revision: orderingDriver.store.readHeader().revision, observedAt: 600,
            envelope: orderingEnvelope)) }
    let stagingRows = ((stagingEnvelope["timeline"] as? [String: Any])?["entries"]
        as? [[String: Any]])?.compactMap { $0["id"] as? String }
    expect("selected environment reorders before pagination", stagingRows,
           [newerStaging.id, newerProduction.id])

    let otherProject = timelineEntry("tl-other-project", project: "project-two")
    _ = orderingDriver.store.ingest(entry: otherProject, events: [
        timelineEvent("other-git", entry: otherProject.id, kind: "landed_to_git", target: nil)
    ], producer: "fixture", sourceVersion: 1)
    let membershipEnvelope = orderingDriver.store.materializedEnvelope()
    let membershipCache = ProjectTimelineReadCache(execute: { $0() })
    membershipCache.seed(.init(revision: orderingDriver.store.readHeader().revision,
                               observedAt: 700, envelope: membershipEnvelope))
    let wrongProject = membershipCache.read(.init(project: "project-one", entry: otherProject.id,
        cursor: 0, environment: "production", category: nil, includeUpcoming: true),
        header: orderingDriver.store.readHeader(), loading: [:]) { .success(.init(
            revision: orderingDriver.store.readHeader().revision, observedAt: 700,
            envelope: membershipEnvelope)) }
    check("detail selection refuses an entry from another Project",
          ((wrongProject["timeline"] as? [String: Any])?["selected"] is NSNull))
    let sameProject = membershipCache.read(.init(project: "project-two", entry: otherProject.id,
        cursor: 0, environment: "production", category: nil, includeUpcoming: true),
        header: orderingDriver.store.readHeader(), loading: [:]) { .success(.init(
            revision: orderingDriver.store.readHeader().revision, observedAt: 700,
            envelope: membershipEnvelope)) }
    expect("detail selection keeps a stable same-Project entry",
           (((sameProject["timeline"] as? [String: Any])?["selected"] as? [String: Any])?["id"] as? String),
           otherProject.id)

    expect("entry capacity is explicit", ProjectTimelineStore.maximumEntries, 2_000)
    expect("event capacity is explicit", ProjectTimelineStore.maximumEvents, 20_000)
    expect("GET page capacity is explicit", ProjectTimelineReadCache.pageSize, 40)
}

group("Project Timeline ingests a real broker landing through persistence and read projection") {
    let d = TimelineTestDriver("producer-chain")
    ProjectTimelineIntegration.configureStoreForTesting(d.store)
    defer { ProjectTimelineIntegration.configureStoreForTesting(nil) }
    let projectPath = FileManager.default.currentDirectoryPath
    let canonical = UsageLedger.canonicalProjectKey(projectDir: projectPath) ?? projectPath
    let projectID = ProjectBoardIntegration.projectID(canonical)
    ProjectTimelineIntegration.observeBrokerRecord([
        "id": "timeline-producer-fixture", "title": "Timeline producer chain",
        "project_dir": projectPath, "work_item_id": "board-chain",
        "landing": ["state": "landed", "verified_commit": "9de1db46b4f6093240e6161fb260df71ca273e15",
                    "landed_at": 1_788_969_600.0]
    ])
    ProjectTimelineIntegration.drainObservationsForTesting()
    let persisted = ProjectTimelineStore(url: d.file).materializedEnvelope()["timeline"] as? [String: Any]
    expect("broker landing producer persists one delivery", (persisted?["entries"] as? [[String: Any]])?.count, 1)
    let read = ProjectTimelineIntegration.read(.init(project: projectID, entry: nil, cursor: 0,
        environment: "production", category: nil, includeUpcoming: true))["timeline"] as? [String: Any]
    let entry = (read?["entries"] as? [[String: Any]])?.first
    expect("materialized read returns the broker delivery", entry?["originalTitle"] as? String, "Timeline producer chain")
    expect("broker landing is explicitly only in Git", (entry?["projection"] as? [String: Any])?["status"] as? String, "landed_to_git")
    expect("real producer preserves stable Board relation", entry?["boardItemIds"] as? [String], ["board-chain"])

    let repository = d.root.appendingPathComponent("local-history", isDirectory: true)
    try? FileManager.default.createDirectory(at: repository, withIntermediateDirectories: true)
    check("Git history fixture initializes", timelineGit(repository, ["init", "-q"]))
    check("Git history fixture commits", timelineGit(repository, ["commit", "--allow-empty", "-q", "-m", "Historical delivery"]))
    let historyStore = ProjectTimelineStore(url: d.root.appendingPathComponent("history.json"))
    let checkpoint = ProjectTimelineGitImporter.importHistory(path: repository.path,
        projectID: "project-history", store: historyStore, observedAt: 1_788_969_700)
    expect("missing remote is a named coverage gap", checkpoint.reason, "github_remote_unavailable")
    expect("read-only importer still ingests local history", checkpoint.imported, 1)
    let historyTimeline = historyStore.materializedEnvelope()["timeline"] as? [String: Any]
    let historyEntry = (historyTimeline?["entries"] as? [[String: Any]])?.first
    expect("Git importer never invents deployment", (historyEntry?["projection"] as? [String: Any])?["status"] as? String, "landed_to_git")
    let historyRevision = (historyEntry?["sourceRevisions"] as? [[String: Any]])?.first
    check("non-GitHub history emits no dead commit link", historyRevision?["githubUrl"] is NSNull)
    let replayCheckpoint = ProjectTimelineGitImporter.importHistory(path: repository.path,
        projectID: "project-history", store: historyStore, observedAt: 1_788_969_800)
    expect("idempotent Git reload remains fully covered", replayCheckpoint.imported, 1)
    expect("idempotent Git reload is not an omission", replayCheckpoint.omitted, 0)
    let reopenedHistory = ProjectTimelineStore(url: d.root.appendingPathComponent("history.json"))
    let reopenedCheckpoint = ProjectTimelineGitImporter.importHistory(path: repository.path,
        projectID: "project-history", store: reopenedHistory, observedAt: 1_788_969_900)
    expect("reopened Git history remains fully covered", reopenedCheckpoint.imported, 1)
    expect("reopened Git history does not invent omissions", reopenedCheckpoint.omitted, 0)
}
}
