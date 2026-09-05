import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - Durable coordinator identity and read-only Bearings

func coordinatorFixture(_ terminalID: String, assistant: Assistant = .codex,
                        tty: String = "/dev/ttys041", pid: Int32 = 410,
                        processStart: Date = Date(timeIntervalSince1970: 1_800_000_000),
                        conversation: String = "conversation-a",
                        workState: Orchestrator.SessionWorkState = .ready,
                        waitingOnSession: Bool = false,
                        hasWaiters: Bool = false,
                        closeability: Orchestrator.SessionCloseability? = nil)
    -> Coordinator.LiveSession {
    Coordinator.LiveSession(
        identity: Orchestrator.SessionWorkIdentity(
            terminalID: terminalID, assistant: assistant, tty: tty, pid: pid,
            processStart: processStart,
            conversationID: conversation),
        label: terminalID == "father" ? "Clawdfather" : "ordinary work",
        cwd: "/Users/me/code/clawdline", workState: workState,
        waitingOnSession: waitingOnSession, hasWaiters: hasWaiters,
        closeability: closeability)
}



/*
   The one word a browser is allowed to gate coordinator creation on.

   `coordinator.configured` cannot be that word. An absent store, a corrupt one and one written
   by an unknown version all project the identical `configured:false, status:"unregistered"`
   tuple — so a page reading that tuple offers to register over a record `register` will refuse
   with `coordinator_store_invalid`, and the refusal lands in a session's transcript rather than
   in the browser that caused it. `registration.state` is derived from the same authoritative
   `load()` the refusal is, and separates the three.

   Every fixture below is written to a real store and read back through the ordinary projection.
   None of them is a dictionary assembled here to have the shape the assertion wants.
*/

func runCoordinatorTests() {
group("a coordinator is explicitly registered, durable, singleton and process-bound") {
    let manager = FileManager.default
    let directory = manager.temporaryDirectory
        .appendingPathComponent("clawdline-coordinator-\(UUID().uuidString)", isDirectory: true)
    try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
    Coordinator.storeURLOverrideForTesting = directory.appendingPathComponent("coordinator.json")
    Coordinator.forgetForTesting()
    defer {
        Coordinator.storeURLOverrideForTesting = nil
        Coordinator.forgetForTesting()
        RemoteServer.coordinatorSessionsForTesting = nil
        try? manager.removeItem(at: directory)
    }

    let father = coordinatorFixture("father")
    let other = coordinatorFixture("other", pid: 411, conversation: "conversation-b")
    let created = Coordinator.register(
        father, among: [father, other], now: Date(timeIntervalSince1970: 1_800_000_010),
        makeID: { UUID(uuidString: "11111111-2222-4333-8444-555555555555")! })
    guard case .ok(let createdBody) = created else {
        check("the first exact live identity registers", false); return
    }
    expect("the first registration creates the stable opaque id",
           (createdBody["coordinator"] as? [String: Any])?["id"] as? String,
           "11111111-2222-4333-8444-555555555555")
    expect("the durable record is private to this user",
           ((try? manager.attributesOfItem(atPath: Coordinator.storeURL.path)[.posixPermissions])
            as? NSNumber)?.intValue, 0o600)
    expect("the registration lock is private to this user",
           ((try? manager.attributesOfItem(
            atPath: Coordinator.registrationLockURL.path)[.posixPermissions]) as? NSNumber)?.intValue,
           0o600)
    expect("the coordinator store parent is private to this user",
           ((try? manager.attributesOfItem(atPath: directory.path)[.posixPermissions])
            as? NSNumber)?.intValue, 0o700)

    let again = Coordinator.register(
        father, among: [father, other], now: Date(timeIntervalSince1970: 1_800_000_020),
        makeID: { UUID() })
    guard case .ok(let againBody) = again else {
        check("the exact registration is idempotent", false); return
    }
    expect("idempotency is explicit", againBody["created"] as? Bool, false)
    expect("and keeps the first coordinator id",
           (againBody["coordinator"] as? [String: Any])?["id"] as? String,
           "11111111-2222-4333-8444-555555555555")

    let insideTolerance = coordinatorFixture(
        "father", processStart: Date(timeIntervalSince1970:
            1_800_000_000 + SessionRegistry.startTolerance - 0.25))
    check("a reconstructed process start inside the canonical tolerance stays bound",
          Coordinator.sessionProjection(for: insideTolerance) != nil)
    Coordinator.forgetForTesting()
    guard case .ok(let driftedAgain) = Coordinator.register(
        insideTolerance, among: [insideTolerance, other], makeID: { UUID() }) else {
        check("inside-tolerance restart registration is idempotent", false); return
    }
    expect("inside-tolerance drift never creates a second coordinator",
           driftedAgain["created"] as? Bool, false)
    expect("inside-tolerance drift keeps the durable coordinator id",
           (driftedAgain["coordinator"] as? [String: Any])?["id"] as? String,
           "11111111-2222-4333-8444-555555555555")

    let beyondTolerance = coordinatorFixture(
        "father", processStart: Date(timeIntervalSince1970:
            1_800_000_000 + SessionRegistry.startTolerance + 0.25))
    check("process start drift beyond tolerance fails closed",
          Coordinator.sessionProjection(for: beyondTolerance) == nil)
    check("an assistant change fails closed",
          Coordinator.sessionProjection(for: coordinatorFixture("father", assistant: .claude)) == nil)
    check("a tty change fails closed",
          Coordinator.sessionProjection(for: coordinatorFixture("father", tty: "/dev/ttys099")) == nil)

    guard case .refused(let conflictStatus, let conflictCode, _, let conflictExtra) =
        Coordinator.register(other, among: [father, other]) else {
        check("a different live identity cannot take over", false); return
    }
    expect("the singleton conflict is 409", conflictStatus, 409)
    expect("the singleton conflict is typed", conflictCode, "coordinator_exists")
    check("the conflict returns only safe current metadata",
          conflictExtra["coordinator"] is [String: Any])

    Coordinator.forgetForTesting()
    check("restart reload binds the same exact process",
          Coordinator.sessionProjection(for: father) != nil)
    check("a reused terminal with a different process cannot inherit the role",
          Coordinator.sessionProjection(for: coordinatorFixture(
            "father", pid: 999, conversation: "conversation-a")) == nil)
    check("a stale conversation in the same terminal and process cannot inherit the role",
          Coordinator.sessionProjection(for: coordinatorFixture(
            "father", pid: 410, conversation: "conversation-stale")) == nil)
    check("a label that says Clawdfather never creates the role",
          Coordinator.sessionProjection(for: Coordinator.LiveSession(
            identity: other.identity, label: "Clawdfather father root", cwd: other.cwd,
            workState: .working, waitingOnSession: false, hasWaiters: false)) == nil)

    let offline = Coordinator.inspection(
        liveSessions: [other], bearings: .init(
            sessionsFresh: true, activeTaskCount: 0, pendingLandingCount: 0,
            openWaitCount: 0,
            sessionsObservedAt: Date(timeIntervalSince1970: 1_800_000_030)))
    expect("a missing exact process preserves durable identity but reports offline",
           (offline["coordinator"] as? [String: Any])?["lifecycle"] as? String, "offline")
    check("offline presence exposes no private binding evidence",
          !String(decoding: try! JSONSerialization.data(withJSONObject: offline), as: UTF8.self)
            .contains("conversation-a"))
    let stale = Coordinator.inspection(
        liveSessions: [other], bearings: .init(
            sessionsFresh: false, activeTaskCount: 0, pendingLandingCount: 0,
            openWaitCount: 0,
            sessionsObservedAt: Date(timeIntervalSince1970: 1_800_000_030)))
    check("stale evidence cannot positively assert a durable coordinator offline",
          (stale["coordinator"] as? [String: Any])?["status"] as? String == "unknown"
            && (stale["coordinator"] as? [String: Any])?["lifecycle"] as? String == "unknown")
    let missing = Coordinator.inspection(
        liveSessions: [other], bearings: .init(
            sessionsFresh: true, activeTaskCount: 0, pendingLandingCount: 0,
            openWaitCount: 0, sessionsObservedAt: nil))
    check("missing evidence cannot positively assert a durable coordinator offline",
          (missing["coordinator"] as? [String: Any])?["status"] as? String == "unknown"
            && (missing["coordinator"] as? [String: Any])?["lifecycle"] as? String == "unknown")

    let projected = Coordinator.sessionProjection(for: father)
    expect("the authenticated optional row has the fixed label",
           projected?["label"] as? String, "Clawdfather")
    expect("the exact live row is online", projected?["status"] as? String, "online")
    let commands = projected?["commands"] as? [[String: Any]] ?? []
    let byType = Dictionary(uniqueKeysWithValues: commands.compactMap { row -> (String, [String: Any])? in
        guard let type = row["type"] as? String else { return nil }; return (type, row)
    })
    for type in ["status_report", "duplicates_conflicts_ownership", "landing_closure",
                 "scope_permissions"] {
        check("the four reads are connected and advertised enabled with nothing to explain",
              byType[type]?["enabled"] as? Bool == true && byType[type]?["reason"] == nil
              && byType[type]?["why"] == nil)
    }
    let disabledReasons = ["since_away": "no_return_ledger",
                           "coordinate_work": "no_command_route",
                           "dispatch_independent_work": "device_cannot_spawn",
                           "ask_coordinator": "no_command_route",
                           "quiet_watch": "no_command_route",
                           "stop": "no_command_route",
                           "reconnect": "machine_token_only"]
    for (type, reason) in disabledReasons {
        check("\(type) stays disabled with the closed reason code \(reason)",
              byType[type]?["enabled"] as? Bool == false
              && byType[type]?["reason"] as? String == reason)
        let why = byType[type]?["why"] as? String ?? ""
        check("\(type)'s compatibility prose is honest and cites no phase number",
              !why.isEmpty && !why.contains("Phase"))
    }

    // The device-readable half of Bearings: what a paired phone may see, and nothing more.
    let device = Coordinator.deviceBearings(
        liveSessions: [father, coordinatorFixture(
            "triage", pid: 412, conversation: "conversation-c", workState: .unknown)],
        bearings: .init(sessionsFresh: false, activeTaskCount: 3, pendingLandingCount: 2,
                        pendingLandingRows: [[
                            "id": "pending-device-row", "title": "safe title",
                            "root_key": "12345678", "root_label": "safe root",
                            "paths": ["Sources/Safe.swift"], "since": 1_800_000_001,
                            "age_seconds": 19, "target": "main", "note": "safe note",
                            "repository_common_dir": "/private/repository/.git",
                            "transcript": "/private/transcript.jsonl",
                            "ownership": [
                                "version": 1, "status": "unknown", "subject": "root",
                                "reason": "session_inventory_incomplete",
                                "task_id": "pending-device-row", "task_state": "success",
                                "root_key": "12345678", "root_assistant": "codex",
                                "observed_work_state": NSNull(),
                                "root_session_id": "private-root-conversation",
                                "pid": 9_999,
                                "evidence": [
                                    "sessions": [
                                        "observed_at": NSNull(), "generation": 42,
                                        "provenance": "session_watch", "freshness": "stale",
                                        "process_start": 1_800_000_000.0,
                                    ],
                                    "tasks": [
                                        "observed_at": 1_800_000_006,
                                        "provenance": "orchestrator_task_registry",
                                        "freshness": "current", "repository": "/private/repo",
                                    ],
                                    "landings": [
                                        "observed_at": 1_800_000_006,
                                        "provenance": "orchestrator_landing_registry",
                                        "freshness": "current", "token": "private-token",
                                    ],
                                    "private_source": ["transcript": "private-transcript"],
                                ],
                            ],
                        ]], openWaitCount: 1,
                        sessionsObservedAt: Date(timeIntervalSince1970: 1_800_000_005),
                        registryObservedAt: Date(timeIntervalSince1970: 1_800_000_006),
                        sessionsGeneration: 42),
        now: Date(timeIntervalSince1970: 1_800_000_020))
    let deviceCoordinator = device["coordinator"] as? [String: Any] ?? [:]
    expect("the device projection keeps degraded presence without asserting liveness",
           deviceCoordinator["status"] as? String, "unknown")
    expect("and the machine scope", deviceCoordinator["scope"] as? String, "machine")
    check("the durable UUID and lifecycle bookkeeping are withheld from devices",
          deviceCoordinator["id"] == nil && deviceCoordinator["generation"] == nil
          && deviceCoordinator["registered_at"] == nil
          && deviceCoordinator["rebound_at"] == nil)
    check("store health is withheld from devices", device["store"] == nil)
    let deviceFacts = device["bearings"] as? [String: Any] ?? [:]
    expect("aggregate counts survive the allowlist", deviceFacts["active_task_count"] as? Int, 3)
    expect("and the landing count", deviceFacts["pending_landing_count"] as? Int, 2)
    let deviceLanding = (deviceFacts["pending_landings"] as? [[String: Any]])?.first ?? [:]
    let deviceOwner = deviceLanding["ownership"] as? [String: Any] ?? [:]
    let deviceEvidence = deviceOwner["evidence"] as? [String: Any] ?? [:]
    check("device pending rows, owners and evidence are closed allowlists",
          Set(deviceLanding.keys) == [
            "id", "title", "root_key", "root_label", "paths", "since", "age_seconds",
            "target", "note", "ownership",
          ]
            && Set(deviceOwner.keys) == [
                "version", "status", "subject", "reason", "task_id", "task_state",
                "root_key", "root_assistant", "observed_work_state", "evidence",
            ]
            && Set(deviceEvidence.keys) == ["sessions", "tasks", "landings"]
            && deviceEvidence.values.allSatisfy { value in
                guard let source = value as? [String: Any] else { return false }
                return Set(source.keys).isSubset(of: [
                    "observed_at", "generation", "provenance", "freshness",
                ])
            })
    expect("the triage rows survive with their session facts",
           ((deviceFacts["unknown"] as? [[String: Any]])?.first?["id"]) as? String, "triage")
    let deviceSessionsSource = ((deviceFacts["sources"] as? [String: Any])?["sessions"])
        as? [String: Any] ?? [:]
    expect("source freshness survives, so a stale picture is never drawn as current",
           deviceSessionsSource["freshness"] as? String, "stale")
    check("provenance names and the watch generation counter are withheld",
          deviceSessionsSource["provenance"] == nil && deviceSessionsSource["generation"] == nil)
    let deviceJSON = String(decoding: try! JSONSerialization.data(withJSONObject: device),
                            as: UTF8.self)
    check("no private binding evidence crosses the device boundary",
          !deviceJSON.contains("conversation-") && !deviceJSON.contains("ttys0")
          && !deviceJSON.contains("\"pid\"") && !deviceJSON.contains("1800000000.0")
          && !deviceJSON.contains("/private/") && !deviceJSON.contains("private-token")
          && !deviceJSON.contains("private-transcript"))

    // The route-level gate difference — 401 unpaired, 403 for a paired device on the full
    // inspection, 200 for a paired device on the projection — is exercised in the coordinator
    // routes group below, which owns a paired-device fixture.

    let validBytes = try! Data(contentsOf: Coordinator.storeURL)
    var future = try! JSONSerialization.jsonObject(with: validBytes) as! [String: Any]
    future["version"] = 99
    try! JSONSerialization.data(withJSONObject: future).write(
        to: Coordinator.storeURL, options: .atomic)
    Coordinator.forgetForTesting()
    check("an unknown durable version fails closed",
          Coordinator.sessionProjection(for: father) == nil)
    let unsupported = Coordinator.inspection(
        liveSessions: [father], bearings: .init(sessionsFresh: true, activeTaskCount: 0,
                                                pendingLandingCount: 0, openWaitCount: 0))
    expect("unknown-version evidence is preserved and explained",
           (unsupported["store"] as? [String: Any])?["status"] as? String, "unsupported")

    // Corruption is not absence with a convenient fallback. It removes every projection and is
    // exposed only as store health on the authenticated inspection surface.
    try! Data("{not-json".utf8).write(to: Coordinator.storeURL, options: .atomic)
    Coordinator.forgetForTesting()
    check("a corrupt durable record fails closed", Coordinator.sessionProjection(for: father) == nil)
    let corrupt = Coordinator.inspection(
        liveSessions: [father], bearings: .init(sessionsFresh: true, activeTaskCount: 0,
                                                pendingLandingCount: 0, openWaitCount: 0),
        now: Date(timeIntervalSince1970: 1_800_000_030))
    expect("corruption is explained without inventing an identity",
           (corrupt["store"] as? [String: Any])?["status"] as? String, "corrupt")
    expect("and no coordinator is configured",
           (corrupt["coordinator"] as? [String: Any])?["status"] as? String, "unregistered")
}

group("registration availability is a closed word derived from the durable store") {
    let manager = FileManager.default
    let directory = manager.temporaryDirectory
        .appendingPathComponent("clawdline-coordinator-registration-\(UUID().uuidString)",
                                isDirectory: true)
    try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
    Coordinator.storeURLOverrideForTesting = directory.appendingPathComponent("coordinator.json")
    Coordinator.forgetForTesting()
    defer {
        Coordinator.storeURLOverrideForTesting = nil
        Coordinator.forgetForTesting()
        try? manager.removeItem(at: directory)
    }

    let facts = Coordinator.BearingsInput(sessionsFresh: true, activeTaskCount: 0,
                                          pendingLandingCount: 0, openWaitCount: 0)
    let father = coordinatorFixture("father")
    let other = coordinatorFixture("other", pid: 411, conversation: "conversation-b")

    func state(_ live: [Coordinator.LiveSession]) -> (String?, String?) {
        let full = Coordinator.inspection(liveSessions: live, bearings: facts)
        let device = Coordinator.deviceBearings(liveSessions: live, bearings: facts)
        return ((full["registration"] as? [String: Any])?["state"] as? String,
                (device["registration"] as? [String: Any])?["state"] as? String)
    }

    // 1 — absent. Nothing is stored, so registering writes over nothing.
    let absent = state([father])
    expect("an absent store is the one state that invites registration", absent.0, "available")
    expect("and a paired device is told the same word", absent.1, "available")

    // 2 — a valid record whose exact process is live.
    guard case .ok = Coordinator.register(
        father, among: [father, other], now: Date(timeIntervalSince1970: 1_800_000_010),
        makeID: { UUID(uuidString: "11111111-2222-4333-8444-555555555555")! }) else {
        check("the registration fixture registers", false); return
    }
    let online = state([father, other])
    expect("a live owner is configured, not available", online.0, "configured")
    expect("and the device projection agrees", online.1, "configured")

    // 3 — the same valid record with no matching live process. Still an owner.
    let offline = state([other])
    expect("an offline owner is still an owner", offline.0, "configured")
    expect("and offline never reads as available", offline.1, "configured")

    // 4 — an unknown durable version. The bytes are somebody else's and must survive.
    let validBytes = try! Data(contentsOf: Coordinator.storeURL)
    var future = try! JSONSerialization.jsonObject(with: validBytes) as! [String: Any]
    future["version"] = 99
    let futureBytes = try! JSONSerialization.data(withJSONObject: future)
    try! futureBytes.write(to: Coordinator.storeURL, options: .atomic)
    Coordinator.forgetForTesting()
    let unsupported = state([father])
    expect("an unknown version blocks registration rather than inviting it",
           unsupported.0, "blocked")
    expect("and the device is told so before it types anything", unsupported.1, "blocked")
    expect("the compatibility tuple is unchanged, which is why it cannot be the gate",
           (Coordinator.inspection(liveSessions: [father], bearings: facts)["coordinator"]
            as? [String: Any])?["status"] as? String, "unregistered")

    // 5 — unparseable bytes. Same answer, same reason: it is not ours to overwrite.
    let corruptBytes = Data("{not-json".utf8)
    try! corruptBytes.write(to: Coordinator.storeURL, options: .atomic)
    Coordinator.forgetForTesting()
    let corrupt = state([father])
    expect("a corrupt store blocks registration", corrupt.0, "blocked")
    expect("and says so on the device projection too", corrupt.1, "blocked")
    expect("a blocked store is still reported as unregistered for compatibility",
           (Coordinator.inspection(liveSessions: [father], bearings: facts)["coordinator"]
            as? [String: Any])?["configured"] as? Bool, false)
    expect("and the bytes nobody may overwrite are still there",
           try! Data(contentsOf: Coordinator.storeURL), corruptBytes)

    // The vocabulary is closed. A client may switch on it exhaustively, which is the whole
    // reason it is worth sending: an open-ended word would have to fail closed on every value
    // and would therefore be no better than the tuple it replaces.
    check("the projection has exactly three words",
          Coordinator.registrationStates == ["available", "configured", "blocked"])
    for words in [absent, online, offline, unsupported, corrupt] {
        check("every state produced from a real store is one of the three",
              words.0.map(Coordinator.registrationStates.contains) == true
                && words.1.map(Coordinator.registrationStates.contains) == true)
    }

    // Non-sensitive by construction: the projection is one of three words and carries no path,
    // no coordinator id, no token, no stored bytes and no corruption text.
    let deviceJSON = String(
        decoding: try! JSONSerialization.data(
            withJSONObject: Coordinator.deviceBearings(liveSessions: [father], bearings: facts)),
        as: UTF8.self)
    check("the blocked answer discloses nothing about the store it is protecting",
          !deviceJSON.contains("not-json") && !deviceJSON.contains(directory.path)
            && !deviceJSON.contains("coordinator.json")
            && !deviceJSON.contains("11111111-2222-4333-8444-555555555555")
            && !deviceJSON.contains("corrupt") && !deviceJSON.contains("unsupported"))
    check("store health itself is still withheld from devices",
          Coordinator.deviceBearings(liveSessions: [father], bearings: facts)["store"] == nil)
}

group("an offline coordinator can be rebound without changing its durable identity") {
    let manager = FileManager.default
    let directory = manager.temporaryDirectory
        .appendingPathComponent("clawdline-coordinator-rebind-\(UUID().uuidString)",
                                isDirectory: true)
    try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
    Coordinator.storeURLOverrideForTesting = directory.appendingPathComponent("coordinator.json")
    Coordinator.forgetForTesting()
    defer {
        Coordinator.storeURLOverrideForTesting = nil
        Coordinator.forgetForTesting()
        try? manager.removeItem(at: directory)
    }

    let durableID = "11111111-2222-4333-8444-555555555555"
    let old = coordinatorFixture("father")
    let replacement = coordinatorFixture(
        "father-new", assistant: .claude, tty: "/dev/ttys099", pid: 999,
        processStart: Date(timeIntervalSince1970: 1_800_000_500),
        conversation: "replacement-private-conversation")
    _ = Coordinator.register(
        old, among: [old], now: Date(timeIntervalSince1970: 1_800_000_010),
        makeID: { UUID(uuidString: durableID)! })
    var legacyObject = try! JSONSerialization.jsonObject(
        with: Data(contentsOf: Coordinator.storeURL)) as! [String: Any]
    legacyObject.removeValue(forKey: "generation")
    legacyObject.removeValue(forKey: "reboundAt")
    try! JSONSerialization.data(withJSONObject: legacyObject).write(
        to: Coordinator.storeURL, options: .atomic)
    Coordinator.forgetForTesting()
    let legacy = Coordinator.inspection(
        liveSessions: [old], bearings: .init(
            sessionsFresh: true, activeTaskCount: 0, pendingLandingCount: 0, openWaitCount: 0))
    expect("an A1 record without lifecycle fields remains generation one",
           (legacy["coordinator"] as? [String: Any])?["generation"] as? Int, 1)

    guard case .refused(let staleStatus, let staleCode, _, _) = Coordinator.rebind(
        expectedCoordinatorID: durableID, expectedGeneration: 1,
        to: replacement, among: [replacement],
        sessionsFresh: false, sessionsObservedAt: Date(timeIntervalSince1970: 1_800_000_500),
        now: Date(timeIntervalSince1970: 1_800_000_510)) else {
        check("a stale inventory cannot prove the old binding offline", false); return
    }
    expect("stale offline proof is a conflict", staleStatus, 409)
    expect("stale offline proof has a typed refusal", staleCode,
           "coordinator_liveness_unknown")

    guard case .refused(let onlineStatus, let onlineCode, _, _) = Coordinator.rebind(
        expectedCoordinatorID: durableID, expectedGeneration: 1,
        to: replacement, among: [old, replacement],
        sessionsFresh: true, sessionsObservedAt: Date(timeIntervalSince1970: 1_800_000_500),
        now: Date(timeIntervalSince1970: 1_800_000_510)) else {
        check("a live old binding cannot be taken over", false); return
    }
    expect("online takeover is a conflict", onlineStatus, 409)
    expect("online takeover has a typed refusal", onlineCode, "coordinator_online")

    guard case .refused(_, let identityCode, _, _) = Coordinator.rebind(
        expectedCoordinatorID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
        expectedGeneration: 1,
        to: replacement, among: [replacement], sessionsFresh: true,
        sessionsObservedAt: Date(timeIntervalSince1970: 1_800_000_500)) else {
        check("the caller must name the durable identity it observed", false); return
    }
    expect("a stale expected id cannot redirect the role", identityCode,
           "coordinator_identity_mismatch")

    guard case .ok(let rebound) = Coordinator.rebind(
        expectedCoordinatorID: durableID, expectedGeneration: 1,
        to: replacement, among: [replacement],
        sessionsFresh: true, sessionsObservedAt: Date(timeIntervalSince1970: 1_800_000_500),
        now: Date(timeIntervalSince1970: 1_800_000_510)) else {
        check("an exact live candidate replaces a provably offline binding", false); return
    }
    expect("the first reconnect reports a rebind", rebound["rebound"] as? Bool, true)
    let reboundCoordinator = rebound["coordinator"] as? [String: Any]
    expect("rebind preserves the stable coordinator id", reboundCoordinator?["id"] as? String,
           durableID)
    expect("rebind preserves original construction time",
           reboundCoordinator?["registered_at"] as? Int, 1_800_000_010)
    expect("rebind advances the durable generation", reboundCoordinator?["generation"] as? Int, 2)
    expect("rebind records a safe reconnect time", reboundCoordinator?["rebound_at"] as? Int,
           1_800_000_510)
    expect("the replacement is now the safe public session",
           (reboundCoordinator?["session"] as? [String: Any])?["id"] as? String,
           "father-new")
    check("the old process loses the optional role", Coordinator.sessionProjection(for: old) == nil)
    check("the exact replacement process receives the optional role",
          Coordinator.sessionProjection(for: replacement) != nil)

    guard case .refused(_, let staleGenerationCode, _, _) = Coordinator.rebind(
        expectedCoordinatorID: durableID, expectedGeneration: 1,
        to: replacement, among: [replacement], sessionsFresh: true,
        sessionsObservedAt: Date(timeIntervalSince1970: 1_800_000_520)) else {
        check("an old lifecycle generation cannot pass as idempotent", false); return
    }
    expect("stale lifecycle CAS is typed", staleGenerationCode,
           "coordinator_generation_mismatch")

    let laterCandidate = coordinatorFixture(
        "father-later", pid: 1_001,
        processStart: Date(timeIntervalSince1970: 1_800_000_600),
        conversation: "later-private-conversation")
    guard case .refused(_, let oldObservationCode, _, _) = Coordinator.rebind(
        expectedCoordinatorID: durableID, expectedGeneration: 2,
        to: laterCandidate, among: [laterCandidate],
        sessionsFresh: true,
        sessionsObservedAt: Date(timeIntervalSince1970: 1_800_000_505),
        now: Date(timeIntervalSince1970: 1_800_000_600)) else {
        check("a scan from before the current binding cannot disprove it", false); return
    }
    expect("a pre-binding scan has unknown liveness", oldObservationCode,
           "coordinator_liveness_unknown")

    guard case .ok(let again) = Coordinator.rebind(
        expectedCoordinatorID: durableID, expectedGeneration: 2,
        to: replacement, among: [replacement],
        sessionsFresh: true, sessionsObservedAt: Date(timeIntervalSince1970: 1_800_000_999),
        now: Date(timeIntervalSince1970: 1_800_000_999)) else {
        check("repeating the current exact binding is idempotent", false); return
    }
    expect("idempotent reconnect does not report another rebind", again["rebound"] as? Bool, false)
    let againCoordinator = again["coordinator"] as? [String: Any]
    expect("idempotency does not advance generation", againCoordinator?["generation"] as? Int, 2)
    expect("idempotency does not rewrite reconnect time", againCoordinator?["rebound_at"] as? Int,
           1_800_000_510)

    Coordinator.forgetForTesting()
    let afterReload = Coordinator.inspection(
        liveSessions: [replacement], bearings: .init(
            sessionsFresh: true, activeTaskCount: 0, pendingLandingCount: 0, openWaitCount: 0))
    let encoded = String(decoding: try! JSONSerialization.data(withJSONObject: afterReload),
                         as: UTF8.self)
    expect("the rebound generation survives a fresh read",
           (afterReload["coordinator"] as? [String: Any])?["generation"] as? Int, 2)
    check("public lifecycle metadata never leaks private binding evidence",
          !encoded.contains("replacement-private-conversation")
            && !encoded.contains("ttys099") && !encoded.contains("\"pid\""))

    let validData = try! Data(contentsOf: Coordinator.storeURL)
    var unsupportedObject = try! JSONSerialization.jsonObject(with: validData) as! [String: Any]
    unsupportedObject["version"] = 999
    let unsupportedData = try! JSONSerialization.data(withJSONObject: unsupportedObject)
    try! unsupportedData.write(to: Coordinator.storeURL, options: .atomic)
    Coordinator.forgetForTesting()
    guard case .refused(_, let unsupportedCode, _, _) = Coordinator.rebind(
        expectedCoordinatorID: durableID, expectedGeneration: 2,
        to: old, among: [old], sessionsFresh: true,
        sessionsObservedAt: Date(timeIntervalSince1970: 1_800_001_000)) else {
        check("an unsupported store cannot be repaired by rebind", false); return
    }
    expect("unknown versions fail closed", unsupportedCode, "coordinator_store_invalid")
    expect("unknown versions are preserved byte for byte",
           try! Data(contentsOf: Coordinator.storeURL), unsupportedData)

    let corruptData = Data("{not-json".utf8)
    try! corruptData.write(to: Coordinator.storeURL, options: .atomic)
    Coordinator.forgetForTesting()
    guard case .refused(_, let corruptCode, _, _) = Coordinator.rebind(
        expectedCoordinatorID: durableID, expectedGeneration: 2,
        to: old, among: [old], sessionsFresh: true,
        sessionsObservedAt: Date(timeIntervalSince1970: 1_800_001_000)) else {
        check("a corrupt store cannot be repaired by rebind", false); return
    }
    expect("corrupt stores fail closed", corruptCode, "coordinator_store_invalid")
    expect("corrupt bytes are preserved", try! Data(contentsOf: Coordinator.storeURL), corruptData)

    try! manager.removeItem(at: Coordinator.storeURL)
    Coordinator.forgetForTesting()
    guard case .refused(_, let absentCode, _, _) = Coordinator.rebind(
        expectedCoordinatorID: durableID, expectedGeneration: 2,
        to: old, among: [old], sessionsFresh: true,
        sessionsObservedAt: Date(timeIntervalSince1970: 1_800_001_000)) else {
        check("rebind never constructs an absent role", false); return
    }
    expect("an absent role needs explicit registration", absentCode,
           "coordinator_not_configured")
}

group("clock rollback cannot lower a rebound binding's freshness barrier") {
    let manager = FileManager.default
    let directory = manager.temporaryDirectory
        .appendingPathComponent("clawdline-coordinator-clock-\(UUID().uuidString)", isDirectory: true)
    try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
    Coordinator.storeURLOverrideForTesting = directory.appendingPathComponent("coordinator.json")
    Coordinator.forgetForTesting()
    defer {
        Coordinator.storeURLOverrideForTesting = nil
        Coordinator.forgetForTesting()
        try? manager.removeItem(at: directory)
    }
    let durableID = "33333333-4444-4555-8666-777777777777"
    let first = coordinatorFixture("clock-first")
    let second = coordinatorFixture(
        "clock-second", pid: 1_102,
        processStart: Date(timeIntervalSince1970: 1_800_001_102), conversation: "clock-second")
    let third = coordinatorFixture(
        "clock-third", pid: 1_103,
        processStart: Date(timeIntervalSince1970: 1_800_001_103), conversation: "clock-third")
    _ = Coordinator.register(
        first, among: [first], now: Date(timeIntervalSince1970: 1_800_001_000),
        makeID: { UUID(uuidString: durableID)! })
    guard case .ok = Coordinator.rebind(
        expectedCoordinatorID: durableID, expectedGeneration: 1,
        to: second, among: [second], sessionsFresh: true,
        sessionsObservedAt: Date(timeIntervalSince1970: 1_800_001_100),
        now: Date(timeIntervalSince1970: 1_800_001_100)) else {
        check("the first monotonic reconnect succeeds", false); return
    }
    guard case .refused(_, let rollbackCode, _, _) = Coordinator.rebind(
        expectedCoordinatorID: durableID, expectedGeneration: 2,
        to: third, among: [third], sessionsFresh: true,
        sessionsObservedAt: Date(timeIntervalSince1970: 1_800_001_200),
        now: Date(timeIntervalSince1970: 1_800_001_050)) else {
        check("a second reconnect cannot move the binding clock backward", false); return
    }
    expect("clock rollback fails closed", rollbackCode, "coordinator_store_invalid")
    let after = Coordinator.inspection(
        liveSessions: [second, third], bearings: .init(
            sessionsFresh: true, activeTaskCount: 0, pendingLandingCount: 0, openWaitCount: 0))
    let coordinator = after["coordinator"] as? [String: Any]
    expect("rollback preserves the prior generation", coordinator?["generation"] as? Int, 2)
    expect("rollback preserves the prior freshness barrier", coordinator?["rebound_at"] as? Int,
           1_800_001_100)
    expect("rollback preserves the prior exact binding",
           (coordinator?["session"] as? [String: Any])?["id"] as? String, "clock-second")
}

group("coordinator cache observes another process's atomic creation") {
    let manager = FileManager.default
    let directory = manager.temporaryDirectory
        .appendingPathComponent("clawdline-coordinator-cache-\(UUID().uuidString)", isDirectory: true)
    try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
    let source = directory.appendingPathComponent("source.json")
    let observed = directory.appendingPathComponent("observed.json")
    let father = coordinatorFixture("father")
    defer {
        Coordinator.storeURLOverrideForTesting = nil
        Coordinator.forgetForTesting()
        try? manager.removeItem(at: directory)
    }

    Coordinator.storeURLOverrideForTesting = source
    Coordinator.forgetForTesting()
    _ = Coordinator.register(father, among: [father], makeID: {
        UUID(uuidString: "22222222-3333-4444-8555-666666666666")!
    })
    let externalRecord = try! Data(contentsOf: source)

    Coordinator.storeURLOverrideForTesting = observed
    Coordinator.forgetForTesting()
    check("the first inspection records genuine absence",
          Coordinator.sessionProjection(for: father) == nil)
    try! externalRecord.write(to: observed, options: .atomic)
    check("a later inspection sees an atomic file created by another process",
          Coordinator.sessionProjection(for: father) != nil)

    try! manager.removeItem(at: observed)
    try! manager.createSymbolicLink(at: observed, withDestinationURL: source)
    Coordinator.forgetForTesting()
    check("a symlinked coordinator record fails closed",
          Coordinator.sessionProjection(for: father) == nil)
    guard case .refused(_, let symlinkCode, _, _) = Coordinator.register(
        father, among: [father]) else {
        check("a symlinked store is never replaced", false); return
    }
    expect("a symlinked store reports invalid durable state", symlinkCode,
           "coordinator_store_invalid")
}

group("coordinator registration is a real cross-process singleton") {
    let manager = FileManager.default
    let directory = manager.temporaryDirectory
        .appendingPathComponent("clawdline-coordinator-race-\(UUID().uuidString)", isDirectory: true)
    let barrier = directory.appendingPathComponent("barrier", isDirectory: true)
    try! manager.createDirectory(at: barrier, withIntermediateDirectories: true)
    defer { try? manager.removeItem(at: directory) }
    let store = directory.appendingPathComponent("coordinator.json")

    func worker(_ role: String) -> (Process, Pipe) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: CommandLine.arguments[0])
        process.arguments = Array(CommandLine.arguments.dropFirst())
        var environment = ProcessInfo.processInfo.environment
        environment["CLAWDLINE_COORDINATOR_RACE_ROLE"] = role
        environment["CLAWDLINE_COORDINATOR_RACE_STORE"] = store.path
        environment["CLAWDLINE_COORDINATOR_RACE_BARRIER"] = barrier.path
        process.environment = environment
        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = pipe
        try! process.run()
        return (process, pipe)
    }

    let first = worker("first")
    let second = worker("second")
    check("both processes first observe absence", eventually(timeout: 5) {
        manager.fileExists(atPath: barrier.appendingPathComponent("first").path)
            && manager.fileExists(atPath: barrier.appendingPathComponent("second").path)
    })
    try! Data("go".utf8).write(to: barrier.appendingPathComponent("go"))
    first.0.waitUntilExit()
    second.0.waitUntilExit()
    let replies = [first, second].map { process, pipe in
        String(decoding: pipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }.sorted()
    expect("exactly one process creates and the other observes coordinator_exists",
           replies, ["coordinator_exists", "created"])
}

group("Bearings is a closed deterministic projection without transcript data") {
    let manager = FileManager.default
    let directory = manager.temporaryDirectory
        .appendingPathComponent("clawdline-bearings-\(UUID().uuidString)", isDirectory: true)
    try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
    Coordinator.storeURLOverrideForTesting = directory.appendingPathComponent("coordinator.json")
    Coordinator.forgetForTesting()
    defer {
        Coordinator.storeURLOverrideForTesting = nil
        Coordinator.forgetForTesting()
        RemoteServer.coordinatorSessionsForTesting = nil
        try? manager.removeItem(at: directory)
    }

    let states = Orchestrator.SessionWorkState.allCases
    let sessions = states.enumerated().map { index, state in
        coordinatorFixture(index == 0 ? "father" : "session-\(index)", pid: Int32(500 + index),
                           conversation: "private-conversation-\(index)", workState: state,
                           waitingOnSession: state == .waitingSession,
                           hasWaiters: state == .working)
    }
    _ = Coordinator.register(
        sessions[0], among: sessions, makeID: {
            UUID(uuidString: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")!
        })
    let snapshot = Coordinator.inspection(
        liveSessions: sessions,
        bearings: .init(sessionsFresh: false, activeTaskCount: 3,
                        pendingLandingCount: 2, openWaitCount: 1),
        now: Date(timeIntervalSince1970: 1_800_000_100))
    let bearings = snapshot["bearings"] as? [String: Any]
    let counts = bearings?["work_state_counts"] as? [String: Any]
    expect("all eight work states are counted even when zero would be needed", counts?.count, 8)
    for state in states { expect("one \(state.rawValue) row", counts?[state.rawValue] as? Int, 1) }
    expect("active task count comes through unchanged", bearings?["active_task_count"] as? Int, 3)
    expect("pending landing count comes through unchanged",
           bearings?["pending_landing_count"] as? Int, 2)
    expect("open wait count comes through unchanged", bearings?["open_wait_count"] as? Int, 1)
    expect("an incomplete session scan is marked stale",
           ((bearings?["sources"] as? [String: Any])?["sessions"] as? [String: Any])?["freshness"] as? String,
           "stale")
    expect("needs-triage names only that safe session",
           (bearings?["unknown"] as? [[String: Any]])?.count, 1)
    expect("waiting names human and peer waiting states",
           (bearings?["waiting"] as? [[String: Any]])?.count, 2)
    expect("blocking names only a session with current waiters",
           (bearings?["blocking"] as? [[String: Any]])?.count, 1)
    let encoded = String(decoding: try! JSONSerialization.data(withJSONObject: snapshot), as: UTF8.self)
    check("inspection never leaks assistant conversation ids", !encoded.contains("private-conversation"))
    check("inspection never leaks tty or pid binding evidence",
          !encoded.contains("ttys041") && !encoded.contains("\"pid\""))

    let before = Orchestrator.projectSessionWorkState(
        terminalState: .idle, task: nil, hasCoordinationWait: false,
        hasOpenHandoff: false, assignmentKnownAbsent: false)
    _ = Coordinator.register(sessions[0], among: sessions)
    let after = Orchestrator.projectSessionWorkState(
        terminalState: .idle, task: nil, hasCoordinationWait: false,
        hasOpenHandoff: false, assignmentKnownAbsent: false)
    expect("registration changes no hierarchy or work-state decision", after, before)

    let owner = coordinatorFixture("owner", pid: 610, conversation: "owner",
                                   workState: .ready, hasWaiters: true)
    let waiter = coordinatorFixture("waiter", pid: 611, conversation: "waiter",
                                    workState: .waitingSession, waitingOnSession: true)
    let both = coordinatorFixture("both", pid: 612, conversation: "both",
                                  workState: .waitingSession, waitingOnSession: true,
                                  hasWaiters: true)
    let human = coordinatorFixture("human", pid: 613, conversation: "human",
                                   workState: .waitingYou)
    let overlap = Coordinator.inspection(
        liveSessions: [owner, waiter, both, human],
        bearings: .init(sessionsFresh: true, activeTaskCount: 0,
                        pendingLandingCount: 0, openWaitCount: 2))
    let overlapBearings = overlap["bearings"] as? [String: Any]
    let waitingIDs = Set((overlapBearings?["waiting"] as? [[String: Any]] ?? [])
        .compactMap { $0["id"] as? String })
    let blockingIDs = Set((overlapBearings?["blocking"] as? [[String: Any]] ?? [])
        .compactMap { $0["id"] as? String })
    expect("waiting honestly includes peer waiters, both-role sessions and human questions",
           waitingIDs, Set(["waiter", "both", "human"]))
    expect("blocking honestly includes owners even when that owner is also waiting",
           blockingIDs, Set(["owner", "both"]))
    check("named Bearings lists are filters and may overlap", waitingIDs.contains("both")
          && blockingIDs.contains("both"))

    let serverSource = try! String(contentsOfFile: "Sources/RemoteServer.swift", encoding: .utf8)
    check("Bearings uses one Orchestrator registry snapshot after its SessionWatch observation",
          serverSource.contains("Orchestrator.coordinatorSnapshot(")
          && !serverSource.contains("let counts = Orchestrator.coordinatorCounts()"))
    let coordinatorCommands =
        Coordinator.sessionProjection(for: sessions[0])?["commands"] as? [[String: Any]] ?? []
    let enabledCoordinatorTypes = Set(coordinatorCommands.compactMap { command -> String? in
        command["enabled"] as? Bool == true ? command["type"] as? String : nil
    })
    check("the four connected reads and deep audit are the only enabled advertisement",
          enabledCoordinatorTypes == Set([
            "status_report", "duplicates_conflicts_ownership", "landing_closure",
            "scope_permissions", "deep_status_audit",
          ]))
}

group("Bearings takes one coherent Orchestrator snapshot for every session fact") {
    try? FileManager.default.removeItem(at: Orchestrator.storeURL)
    Orchestrator.forget()
    defer {
        try? FileManager.default.removeItem(at: Orchestrator.storeURL)
        Orchestrator.forget()
    }
    func makeWait(owner: String, waiter: String, path: String) {
        _ = Orchestrator.registerCoordinationWait([
            "repository": "/tmp/coordinator-snapshot", "paths": [path],
            "owner_session_id": owner, "waiter_session_id": waiter,
            "reason": "snapshot fixture", "release_condition": "explicit release",
        ], deliver: { _, _ in nil })
    }
    makeWait(owner: "owner", waiter: "both", path: "Sources/Owner.swift")
    makeWait(owner: "both", waiter: "waiter", path: "Sources/Waiter.swift")
    let identities = ["owner", "waiter", "both", "human"].enumerated().map { index, id in
        Orchestrator.SessionWorkIdentity(
            terminalID: id, assistant: .codex, tty: "/dev/ttys\(80 + index)",
            pid: Int32(800 + index),
            processStart: Date(timeIntervalSince1970: 1_800_000_800 + Double(index)),
            conversationID: "snapshot-\(id)")
    }
    let snapshot = Orchestrator.coordinatorSnapshot(
        identities.enumerated().map { index, identity in
            .init(identity: identity, terminalState: index == 3 ? .waiting : .idle)
        }, now: Date(timeIntervalSince1970: 1_800_000_900))
    expect("the registry snapshot has one positional fact per SessionWatch row",
           snapshot.sessions.count, identities.count)
    let owner = snapshot.sessions[0]
    let waiter = snapshot.sessions[1]
    let both = snapshot.sessions[2]
    let human = snapshot.sessions[3]
    check("owner-only provenance has waiters but is not itself waiting",
          owner.coordination.waitingOn.isEmpty && owner.coordination.waitedOnBy.count == 1)
    check("waiter-only provenance waits but owns no waiter",
          waiter.coordination.waitingOn.count == 1 && waiter.coordination.waitedOnBy.isEmpty)
    check("the same session can honestly be both owner and waiter",
          both.coordination.waitingOn.count == 1 && both.coordination.waitedOnBy.count == 1)
    expect("a human question outranks other registry-derived work states",
           human.work.state, .waitingYou)
    expect("both wait groups and their per-session flags share the snapshot",
           snapshot.openWaits, 2)
    expect("the registry source keeps its own observation time",
           snapshot.observedAt, Date(timeIntervalSince1970: 1_800_000_900))
}

group("coordinator routes require the machine token and expose no implicit takeover") {
    let manager = FileManager.default
    let directory = manager.temporaryDirectory
        .appendingPathComponent("clawdline-coordinator-route-\(UUID().uuidString)", isDirectory: true)
    try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
    Coordinator.storeURLOverrideForTesting = directory.appendingPathComponent("coordinator.json")
    Coordinator.forgetForTesting()
    let father = coordinatorFixture("father", closeability: .safe)
    let other = coordinatorFixture(
        "other", pid: 411, conversation: "conversation-b", closeability: .blocked)
    let unbound = Coordinator.LiveSession(
        identity: Orchestrator.SessionWorkIdentity(
            terminalID: "unbound", assistant: .claude, tty: "/dev/ttys099", pid: nil,
            processStart: nil, conversationID: nil),
        label: "Clawdfather by title only", cwd: "/Users/me/code/clawdline",
        workState: .unknown, waitingOnSession: false, hasWaiters: false,
        closeability: .unknown)
    let unprojected = coordinatorFixture(
        "unprojected", pid: 412, conversation: "conversation-c", workState: .unknown)
    RemoteServer.coordinatorSessionsForTesting = [father, other, unbound, unprojected]
    defer {
        RemoteServer.coordinatorSessionsForTesting = nil
        RemoteServer.coordinatorObservationEvidenceForTesting = nil
        RemoteServer.sessionPayloadForTesting = nil
        Coordinator.storeURLOverrideForTesting = nil
        Coordinator.forgetForTesting()
        try? manager.removeItem(at: directory)
    }

    let path = "/v1/orchestrator/coordinator/register"
    expect("anonymous registration is refused",
           RemoteServer.shared.route(remoteRequest("POST", path,
               body: "{\"session_id\":\"father\"}")).status, 401)
    let phone = RemoteAuth.addDevice(name: "coordinator route phone", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: phone.id) }
    expect("a paired device cannot register the coordinator",
           RemoteServer.shared.route(remoteRequest(
            "POST", path, headers: ["Authorization": "Bearer \(phone.token)"],
            body: "{\"session_id\":\"father\"}")).status, 403)
    let taskSecret = ["X-Clawdline-Task-Secret": String(repeating: "ab", count: 32)]
    expect("a task secret cannot register the coordinator",
           RemoteServer.shared.route(remoteRequest(
            "POST", path, headers: taskSecret,
            body: "{\"session_id\":\"father\"}")).status, 401)
    expect("a task secret cannot read Bearings",
           RemoteServer.shared.route(remoteRequest(
            "GET", "/v1/orchestrator/coordinator", headers: taskSecret)).status, 401)
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let malformed = RemoteServer.shared.route(remoteRequest("POST", path, headers: auth, body: "{}"))
    expect("registration requires a closed session id field", malformed.status, 400)
    let brokenJSON = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: "{not-json"))
    expect("malformed JSON is rejected before identity lookup", brokenJSON.status, 400)
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: Date(timeIntervalSince1970: 1_800_000_020), generation: 1,
        complete: false)
    let staleAbsent = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: "{\"session_id\":\"missing\"}"))
    expect("stale registration evidence fails before cache-shaped absence",
           staleAbsent.status, 409)
    expect("stale registration is typed as unknown liveness",
           remoteErrorCode(staleAbsent), "coordinator_liveness_unknown")
    check("stale registration evidence writes no durable candidate",
          !manager.fileExists(atPath: Coordinator.storeURL.path))
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: nil, generation: 1, complete: true)
    let untimestampedAbsent = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: "{\"session_id\":\"father\"}"))
    expect("untimestamped registration evidence fails closed",
           remoteErrorCode(untimestampedAbsent), "coordinator_liveness_unknown")
    check("untimestamped registration evidence writes no durable candidate",
          !manager.fileExists(atPath: Coordinator.storeURL.path))
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: Date(), generation: 2, complete: true)
    let unknown = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: "{\"session_id\":\"missing\"}"))
    expect("an unknown terminal-neutral session is refused", unknown.status, 404)
    expect("with a typed identity error", remoteErrorCode(unknown), "session_not_found")
    let incomplete = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: "{\"session_id\":\"unbound\"}"))
    expect("a visible row without complete process proof is refused", incomplete.status, 409)
    expect("without falling back to its title", remoteErrorCode(incomplete), "session_unbound")
    let made = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: "{\"session_id\":\"father\"}"))
    expect("an exact live assistant session registers", made.status, 200)
    let conflict = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: "{\"session_id\":\"other\"}"))
    expect("the route refuses implicit takeover", conflict.status, 409)
    expect("with the singleton code", remoteErrorCode(conflict), "coordinator_exists")
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: Date(timeIntervalSince1970: 1_800_000_020), generation: 1,
        complete: false)
    let staleIdempotent = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: "{\"session_id\":\"father\"}"))
    expect("a durable exact registration stays idempotent under stale observation",
           staleIdempotent.status, 200)
    let staleIdempotentBody = (try? JSONSerialization.jsonObject(with: staleIdempotent.body))
        as? [String: Any]
    check("stale idempotency mutates nothing and reports unknown liveness",
          staleIdempotentBody?["created"] as? Bool == false
            && (staleIdempotentBody?["coordinator"] as? [String: Any])?["status"]
                as? String == "unknown")
    let staleConflict = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: "{\"session_id\":\"other\"}"))
    expect("a different durable identity still returns coordinator_exists while stale",
           remoteErrorCode(staleConflict), "coordinator_exists")
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: Date(), generation: 3, complete: true)

    let anonymousGet = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/coordinator"))
    expect("anonymous inspection is refused", anonymousGet.status, 401)
    let get = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/coordinator", headers: auth))
    expect("machine-authenticated inspection succeeds", get.status, 200)
    let body = (try? JSONSerialization.jsonObject(with: get.body)) as? [String: Any]
    expect("the exact binding is standby",
           (body?["coordinator"] as? [String: Any])?["lifecycle"] as? String, "standby")

    // The gate difference is the whole design of the device projection: the full inspection
    // stays machine-token-only, while /bearings falls through to ordinary device auth.
    let bearingsPath = "/v1/orchestrator/coordinator/bearings"
    expect("anonymous Bearings projection is refused at the pairing gate",
           RemoteServer.shared.route(remoteRequest("GET", bearingsPath)).status, 401)
    expect("the full inspection refuses even a paired device",
           RemoteServer.shared.route(remoteRequest(
            "GET", "/v1/orchestrator/coordinator",
            headers: ["Authorization": "Bearer \(phone.token)"])).status, 403)
    let deviceGet = RemoteServer.shared.route(remoteRequest(
        "GET", bearingsPath, headers: ["Authorization": "Bearer \(phone.token)"]))
    expect("a paired device reads the projection", deviceGet.status, 200)
    let deviceGetBody = (try? JSONSerialization.jsonObject(with: deviceGet.body))
        as? [String: Any]
    expect("and it names presence",
           (deviceGetBody?["coordinator"] as? [String: Any])?["status"] as? String, "online")
    let deviceBearings = deviceGetBody?["bearings"] as? [String: Any]
    let closeabilityCounts = deviceBearings?["closeability_counts"] as? [String: Any]
    check("Bearings counts all four states and keeps absence as not_projected",
          closeabilityCounts?["safe"] as? Int == 1
            && closeabilityCounts?["blocked"] as? Int == 1
            && closeabilityCounts?["unknown"] as? Int == 1
            && closeabilityCounts?["needs_attestation"] as? Int == 0
            && closeabilityCounts?["not_projected"] as? Int == 1)
    let unknownRows = deviceBearings?["unknown"] as? [[String: Any]] ?? []
    let projectedUnknown = unknownRows.first { $0["id"] as? String == "unbound" }
    check("the reduced Bearings row names its reduced schema distinctly",
          projectedUnknown?["closeability_state"] as? String == "unknown"
            && projectedUnknown?["closeability"] == nil)
    let absentUnknown = unknownRows.first { $0["id"] as? String == "unprojected" }
    check("a not_projected row does not counterfeit unknown",
          absentUnknown?["closeability_state"] == nil)
    check("while the durable UUID and store health never cross the device boundary",
          (deviceGetBody?["coordinator"] as? [String: Any])?["id"] == nil
          && deviceGetBody?["store"] == nil)

    let ordinary: [String: Any] = ["id": "other", "label": "ordinary"]
    var projectedOrdinary = ordinary
    RemoteServer.attachCoordinator(to: &projectedOrdinary, liveSession: other)
    check("ordinary rows have no coordinator field", projectedOrdinary["coordinator"] == nil)
    var projectedFather: [String: Any] = ["id": "father", "label": "ordinary title"]
    RemoteServer.attachCoordinator(to: &projectedFather, liveSession: father)
    check("only the exact row gets the optional coordinator record",
          projectedFather["coordinator"] is [String: Any])
    let fatherTarget = TargetSession(
        backend: .iterm, id: "father", name: "ordinary title", tty: "/dev/ttys041",
        windowIndex: 0, tabIndex: 0, assistant: .codex, cwd: "/Users/me/code/clawdline")
    let ordinaryTarget = TargetSession(
        backend: .iterm, id: "other", name: "ordinary work", tty: "/dev/ttys042",
        windowIndex: 0, tabIndex: 1, assistant: .codex, cwd: "/Users/me/code/clawdline")
    let lookalikeTarget = TargetSession(
        backend: .iterm, id: "lookalike", name: "Clawdfather", tty: "/dev/ttys043",
        windowIndex: 0, tabIndex: 2, assistant: .codex, cwd: "/Users/me/code/clawdline")
    let serializerTargets = [fatherTarget, ordinaryTarget, lookalikeTarget]
    let serializerStates: [String: SessionState] = [
        "father": .idle, "other": .idle, "lookalike": .idle,
    ]
    func coordinatorRecordIsClosed(_ row: [String: Any]) -> Bool {
        guard let record = row["coordinator"] as? [String: Any],
              record["label"] as? String == "Clawdfather",
              record["status"] as? String == "online",
              let commands = record["commands"] as? [[String: Any]]
        else { return false }
        let expected: [String: (enabled: Bool, effort: String, basis: String,
                                reason: String?)] = [
            "status_report": (true, "low", "registry_read", nil),
            "duplicates_conflicts_ownership": (true, "low", "registry_read", nil),
            "landing_closure": (true, "low", "registry_read", nil),
            "scope_permissions": (true, "low", "registry_read", nil),
            "since_away": (false, "unknown", "unbuilt", "no_return_ledger"),
            "coordinate_work": (false, "unknown", "unbuilt", "no_command_route"),
            "dispatch_independent_work":
                (false, "high", "spawns_session", "device_cannot_spawn"),
            "ask_coordinator":
                (false, "medium", "single_session_message", "no_command_route"),
            "deep_status_audit": (true, "high", "session_fanout", nil),
            "quiet_watch": (false, "unknown", "unbuilt", "no_command_route"),
            "stop": (false, "low", "broker_only", "no_command_route"),
            "reconnect": (false, "low", "broker_only", "machine_token_only"),
        ]
        guard commands.count == expected.count,
              Set(commands.compactMap { $0["type"] as? String }) == Set(expected.keys)
        else { return false }
        return Set(record.keys) == Set(["label", "status", "commands"])
            && commands.allSatisfy { command in
                guard let type = command["type"] as? String,
                      let specification = expected[type],
                      command["enabled"] as? Bool == specification.enabled,
                      command["token_effort"] as? String == specification.effort,
                      command["token_effort_basis"] as? String == specification.basis
                else { return false }
                if specification.enabled {
                    return Set(command.keys) == Set([
                        "type", "enabled", "token_effort", "token_effort_basis",
                    ])
                }
                return Set(command.keys) == Set([
                    "type", "enabled", "reason", "token_effort", "token_effort_basis", "why",
                ])
                    && command["reason"] as? String == specification.reason
                    && !(command["why"] as? String ?? "").isEmpty
            }
    }

    let addressRows = RemoteServer.coordinationSessionRows(
        serializerTargets, states: serializerStates)
    check("the orchestrator Session serializer projects the exact coordinator row",
          addressRows.first(where: { $0["id"] as? String == "father" })
            .map(coordinatorRecordIsClosed) == true)
    for id in ["other", "lookalike"] {
        check("the orchestrator Session serializer leaves (id) ordinary",
              addressRows.first(where: { $0["id"] as? String == id })?["coordinator"] == nil)
    }

    RemoteServer.sessionPayloadForTesting = (serializerTargets, serializerStates)
    let sessionsResponse = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/sessions", headers: ["Authorization": "Bearer \(phone.token)"]))
    expect("the ordinary Session payload route executes", sessionsResponse.status, 200)
    let sessionsBody = (try? JSONSerialization.jsonObject(with: sessionsResponse.body))
        as? [String: Any]
    let sessionRows = sessionsBody?["sessions"] as? [[String: Any]] ?? []
    check("the ordinary Session serializer projects the exact coordinator row",
          sessionRows.first(where: { $0["id"] as? String == "father" })
            .map(coordinatorRecordIsClosed) == true)
    for id in ["other", "lookalike"] {
        check("the ordinary Session serializer leaves (id) ordinary",
              sessionRows.first(where: { $0["id"] as? String == id })?["coordinator"] == nil)
    }

    let apiDoc = try! String(contentsOfFile: "docs/api.md", encoding: .utf8)
    let orchestratorDoc = try! String(contentsOfFile: "docs/orchestrator.md", encoding: .utf8)
    // The protocol page, not the working Artifact it replaced. That Artifact lives behind an
    // ignored symlink into a private repository, so reading it here meant `./test.sh` could only
    // pass on a machine that also had that repository checked out beside this one — a fresh clone
    // trapped on this very line, at check 283 of about four thousand, and everything after it had
    // never run anywhere.
    let protocolPage = try! String(contentsOfFile: "docs/clawdline-protocol.html", encoding: .utf8)
    for (name, text) in [("API", apiDoc), ("orchestrator", orchestratorDoc),
                         ("protocol page", protocolPage)] {
        check("\(name) documents observer_unreachable provenance without authorizing restart",
              text.contains("observer_unreachable") && text.contains("sandbox_loopback")
              && text.contains("host_listener") && text.contains("host_health")
              && text.lowercased().contains("cannot authorize restart"))
    }

    // **A keyword scan is what let the page drift.** A reviewer ran one over it and missed three
    // false claims, one of which was a refusal the broker had gained and the page had never heard
    // of. So this does not look for words somebody thought to list: it derives the set of refusal
    // codes from the source and requires the page to name exactly that set, reporting the
    // difference in both directions. Undocumented codes and documented-but-nonexistent ones are
    // the same defect seen from either end, and the second one shipped here once already.
    // Both files, because the attach refusals are declared in `OrchestratorDraft` and
    // `root_unresolved` in `Orchestrator`. Reading one of them would turn this from "derived from
    // the source" into "derived from part of the source", which is the drift it exists to catch.
    let source = ["Sources/Orchestrator.swift", "Sources/OrchestratorDraft.swift"]
        .map { try! String(contentsOfFile: $0, encoding: .utf8) }.joined(separator: "\n")
    func codes(in text: String, matching pattern: String) -> Set<String> {
        let expression = try! NSRegularExpression(pattern: pattern)
        let range = NSRange(text.startIndex..., in: text)
        var found: Set<String> = []
        expression.enumerateMatches(in: text, range: range) { match, _, _ in
            guard let match, let captured = Range(match.range(at: 1), in: text) else { return }
            found.insert(String(text[captured]))
        }
        return found
    }
    // Written both ways in the source: `code: "…"` in a typed refusal, `"code": "…"` in a warning.
    let declared = codes(in: source, matching: "\"?code\"?: \"(attach_[a-z_]+|root_unresolved)\"")
    let documented = codes(in: protocolPage, matching: "(attach_[a-z_]+|root_unresolved)")
        .filter { $0 != "attach_session" }          // the task.json field, not a refusal
    check("the protocol page names every attach refusal the broker can return, and no others",
          declared == Set(documented),
          "undocumented: \(declared.subtracting(documented).sorted()); "
          + "documented but absent from the source: \(Set(documented).subtracting(declared).sorted())")
}

group("the reconnect route is closed, machine-only and refuses online takeover") {
    let manager = FileManager.default
    let directory = manager.temporaryDirectory
        .appendingPathComponent("clawdline-coordinator-rebind-route-\(UUID().uuidString)",
                                isDirectory: true)
    try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
    Coordinator.storeURLOverrideForTesting = directory.appendingPathComponent("coordinator.json")
    Coordinator.forgetForTesting()
    let old = coordinatorFixture("route-old")
    let replacement = coordinatorFixture(
        "route-new", assistant: .claude, tty: "/dev/ttys091", pid: 901,
        processStart: Date(timeIntervalSince1970: 1_800_000_901),
        conversation: "route-private-replacement")
    RemoteServer.coordinatorSessionsForTesting = [old, replacement]
    defer {
        RemoteServer.coordinatorSessionsForTesting = nil
        Coordinator.storeURLOverrideForTesting = nil
        Coordinator.forgetForTesting()
        try? manager.removeItem(at: directory)
    }
    let durableID = "22222222-3333-4444-8555-666666666666"
    _ = Coordinator.register(
        old, among: [old, replacement], makeID: { UUID(uuidString: durableID)! })
    let path = "/v1/orchestrator/coordinator/rebind"
    let requestBody = "{\"expected_coordinator_id\":\"\(durableID)\","
        + "\"expected_generation\":1,\"session_id\":\"route-new\"}"

    expect("anonymous reconnect is refused",
           RemoteServer.shared.route(remoteRequest("POST", path, body: requestBody)).status, 401)
    let phone = RemoteAuth.addDevice(name: "coordinator reconnect phone", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: phone.id) }
    expect("a paired device cannot reconnect the coordinator",
           RemoteServer.shared.route(remoteRequest(
            "POST", path, headers: ["Authorization": "Bearer \(phone.token)"],
            body: requestBody)).status, 403)
    let taskSecret = ["X-Clawdline-Task-Secret": String(repeating: "cd", count: 32)]
    expect("a task secret cannot reconnect the coordinator",
           RemoteServer.shared.route(remoteRequest(
            "POST", path, headers: taskSecret, body: requestBody)).status, 401)
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    for body in ["{}", "{not-json",
                 "{\"expected_coordinator_id\":\"\(durableID)\","
                    + "\"expected_generation\":1,\"session_id\":\"route-new\","
                    + "\"takeover\":true}",
                 "{\"expected_coordinator_id\":\"not-a-uuid\","
                    + "\"expected_generation\":1,\"session_id\":\"route-new\"}"] {
        expect("the reconnect route rejects malformed or open schemas",
               RemoteServer.shared.route(remoteRequest(
                "POST", path, headers: auth, body: body)).status, 400)
    }
    let missing = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth,
        body: "{\"expected_coordinator_id\":\"\(durableID)\","
            + "\"expected_generation\":1,\"session_id\":\"missing\"}"))
    expect("an unknown reconnect candidate is absent", missing.status, 404)
    expect("the absence is typed", remoteErrorCode(missing), "session_not_found")

    let duplicate = coordinatorFixture(
        "route-new", assistant: .codex, tty: "/dev/ttys092", pid: 902,
        processStart: Date(timeIntervalSince1970: 1_800_000_902), conversation: "duplicate")
    RemoteServer.coordinatorSessionsForTesting = [old, replacement, duplicate]
    let ambiguous = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: requestBody))
    expect("a duplicate terminal-neutral candidate is refused", ambiguous.status, 409)
    expect("duplicate identity has a typed refusal", remoteErrorCode(ambiguous),
           "session_ambiguous")

    RemoteServer.coordinatorSessionsForTesting = [old, replacement]
    let online = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: requestBody))
    expect("a live exact binding cannot be replaced", online.status, 409)
    expect("the online refusal is typed", remoteErrorCode(online), "coordinator_online")
    let mismatch = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth,
        body: "{\"expected_coordinator_id\":"
            + "\"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee\","
            + "\"expected_generation\":1,\"session_id\":\"route-new\"}"))
    expect("a stale expected identity cannot reconnect", mismatch.status, 409)
    expect("the compare-and-swap refusal is typed", remoteErrorCode(mismatch),
           "coordinator_identity_mismatch")

    RemoteServer.coordinatorSessionsForTesting = [replacement]
    let rebound = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: requestBody))
    expect("a complete scan may reconnect a provably offline role", rebound.status, 200)
    let reboundBody = (try? JSONSerialization.jsonObject(with: rebound.body)) as? [String: Any]
    expect("the route reports the actual reconnect", reboundBody?["rebound"] as? Bool, true)
    expect("the route keeps the durable identity",
           (reboundBody?["coordinator"] as? [String: Any])?["id"] as? String, durableID)
    let encoded = String(decoding: rebound.body, as: UTF8.self)
    check("the route leaks no private candidate binding",
          !encoded.contains("route-private-replacement") && !encoded.contains("ttys091")
            && !encoded.contains("\"pid\""))
    let staleLifecycle = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: requestBody))
    expect("the route refuses an observed generation after it advances", staleLifecycle.status,
           409)
    expect("the route types stale lifecycle CAS", remoteErrorCode(staleLifecycle),
           "coordinator_generation_mismatch")
    let currentBody = "{\"expected_coordinator_id\":\"\(durableID)\","
        + "\"expected_generation\":2,\"session_id\":\"route-new\"}"
    let idempotent = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: currentBody))
    expect("repeating the current exact route binding succeeds", idempotent.status, 200)
    let idempotentBody = (try? JSONSerialization.jsonObject(with: idempotent.body))
        as? [String: Any]
    expect("the repeated route call is explicitly idempotent",
           idempotentBody?["rebound"] as? Bool, false)
}

group("the production route preserves scan evidence across cache reads and app generations") {
    let manager = FileManager.default
    let directory = manager.temporaryDirectory
        .appendingPathComponent("clawdline-coordinator-scan-evidence-\(UUID().uuidString)",
                                isDirectory: true)
    try! manager.createDirectory(at: directory, withIntermediateDirectories: true)
    Coordinator.storeURLOverrideForTesting = directory.appendingPathComponent("coordinator.json")
    Coordinator.forgetForTesting()
    defer {
        RemoteServer.coordinatorSessionsForTesting = nil
        RemoteServer.coordinatorObservationEvidenceForTesting = nil
        Coordinator.storeURLOverrideForTesting = nil
        Coordinator.forgetForTesting()
        try? manager.removeItem(at: directory)
    }
    let durableID = "55555555-6666-4777-8888-999999999999"
    let old = coordinatorFixture("scan-old")
    let replacement = coordinatorFixture(
        "scan-new", pid: 1_201,
        processStart: Date(timeIntervalSince1970: 1_750_000_201), conversation: "scan-new")
    let afterRestart = coordinatorFixture(
        "scan-restarted", pid: 1_202,
        processStart: Date(timeIntervalSince1970: 1_750_000_202), conversation: "scan-restarted")
    _ = Coordinator.register(
        old, among: [old], now: Date(timeIntervalSince1970: 1_700_000_000),
        makeID: { UUID(uuidString: durableID)! })
    var legacy = try! JSONSerialization.jsonObject(
        with: Data(contentsOf: Coordinator.storeURL)) as! [String: Any]
    legacy.removeValue(forKey: "generation")
    legacy.removeValue(forKey: "reboundAt")
    try! JSONSerialization.data(withJSONObject: legacy).write(
        to: Coordinator.storeURL, options: .atomic)
    Coordinator.forgetForTesting()

    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let path = "/v1/orchestrator/coordinator/rebind"
    func body(_ session: String, generation: Int) -> String {
        "{\"expected_coordinator_id\":\"\(durableID)\","
            + "\"expected_generation\":\(generation),\"session_id\":\"\(session)\"}"
    }
    RemoteServer.coordinatorSessionsForTesting = [replacement]
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: nil, generation: 100, complete: true)
    let unavailable = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: body("scan-new", generation: 1)))
    expect("missing completed-scan time fails closed", unavailable.status, 409)
    expect("missing scan time is unknown liveness", remoteErrorCode(unavailable),
           "coordinator_liveness_unknown")

    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: Date(timeIntervalSince1970: 1_600_000_000),
        generation: 99, complete: true)
    let cached = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: body("scan-new", generation: 1)))
    expect("a cached pre-binding scan is not relabelled by the HTTP read time", cached.status, 409)
    expect("pre-binding production evidence fails as unknown liveness",
           remoteErrorCode(cached), "coordinator_liveness_unknown")

    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: Date(timeIntervalSince1970: 1_750_000_000),
        generation: 0, complete: true)
    let legacyRebound = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: body("scan-new", generation: 1)))
    expect("a real post-binding scan reconnects a legacy A1 record", legacyRebound.status, 200)

    RemoteServer.coordinatorSessionsForTesting = [afterRestart]
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: Date(), generation: 0, complete: true)
    let restarted = RemoteServer.shared.route(remoteRequest(
        "POST", path, headers: auth, body: body("scan-restarted", generation: 2)))
    expect("an A2 binding reconnects after process-local scan generation resets", restarted.status,
           200)
    let restartedBody = (try? JSONSerialization.jsonObject(with: restarted.body)) as? [String: Any]
    expect("restart-safe reconnect advances lifecycle generation",
           (restartedBody?["coordinator"] as? [String: Any])?["generation"] as? Int, 3)

    let serverSource = try! String(contentsOfFile: "Sources/RemoteServer.swift", encoding: .utf8)
    let watchSource = try! String(contentsOfFile: "Sources/SessionWatch.swift", encoding: .utf8)
    check("production carries SessionWatch's accepted-scan time instead of HTTP read time",
          serverSource.contains("let publication = SessionWatch.shared.publishedInventory()")
            && serverSource.contains(
                "observedAt: unavailable ? nil : publication.observedAt")
            && !serverSource.contains("Date(), watch.scanGeneration"))
    check("only an accepted complete scan advances the evidence timestamp",
          watchSource.contains("if scanComplete { self.scanObservedAt = observedAt }")
            && !watchSource.contains("if scanComplete { self.scanObservedAt = Date() }"))
}

group("Clawdfather succession keeps every handoff and rebind boundary durable") {
    let manager = FileManager.default
    let directory = manager.temporaryDirectory
        .appendingPathComponent("clawdline-coordinator-succession-\(UUID().uuidString)",
                                isDirectory: true)
    let handoffRoot = directory.appendingPathComponent("handoffs", isDirectory: true)
    try! manager.createDirectory(at: handoffRoot, withIntermediateDirectories: true)
    Coordinator.storeURLOverrideForTesting = directory.appendingPathComponent("coordinator.json")
    CoordinatorSuccession.storeURLOverrideForTesting = directory
        .appendingPathComponent("coordinator-successions.json")
    Orchestrator.storeURLOverrideForTesting = directory.appendingPathComponent("orchestrator.json")
    Orchestrator.handoffRootOverrideForTesting = handoffRoot
    Coordinator.forgetForTesting()
    CoordinatorSuccession.forgetForTesting()
    Orchestrator.forget()
    defer {
        RemoteServer.coordinatorSessionsForTesting = nil
        RemoteServer.coordinatorObservationEvidenceForTesting = nil
        RemoteServer.sessionPayloadForTesting = nil
        RemoteServer.sessionWorkIdentityForTesting = nil
        RemoteServer.sessionEndForTesting = nil
        CoordinatorSuccessionService.closeProofForTesting = nil
        CoordinatorSuccession.handoffStarterForTesting = nil
        CoordinatorSuccession.storeSaveInterceptorForTesting = nil
        CoordinatorSuccession.storeURLOverrideForTesting = nil
        Coordinator.storeURLOverrideForTesting = nil
        Orchestrator.storeURLOverrideForTesting = nil
        Orchestrator.handoffRootOverrideForTesting = nil
        Coordinator.forgetForTesting()
        CoordinatorSuccession.forgetForTesting()
        Orchestrator.forget()
        try? manager.removeItem(at: directory)
    }

    let coordinatorID = "77777777-8888-4999-8aaa-bbbbbbbbbbbb"
    let sender = coordinatorFixture(
        "SENDER", tty: "/dev/ttys301", pid: 3_001,
        processStart: Date(timeIntervalSince1970: 1_780_003_001),
        conversation: "sender-conversation")
    let receiver = coordinatorFixture(
        "RECEIVER", assistant: .claude, tty: "/dev/ttys302", pid: 3_002,
        processStart: Date(timeIntervalSince1970: 1_780_003_002),
        conversation: "receiver-conversation")
    _ = Coordinator.register(
        sender, among: [sender, receiver], now: Date(timeIntervalSince1970: 1_780_003_010),
        makeID: { UUID(uuidString: coordinatorID)! })
    RemoteServer.coordinatorSessionsForTesting = [sender, receiver]
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: Date(timeIntervalSince1970: 1_780_003_020), generation: 20,
        complete: true)
    let senderTarget = TargetSession(
        backend: .iterm, id: "SENDER", name: "sender", tty: "/dev/ttys301",
        windowIndex: 0, tabIndex: 0, assistant: .codex, cwd: "/tmp")
    let receiverTarget = TargetSession(
        backend: .iterm, id: "RECEIVER", name: "receiver", tty: "/dev/ttys302",
        windowIndex: 0, tabIndex: 1, assistant: .claude, cwd: "/tmp")
    RemoteServer.sessionPayloadForTesting = ([senderTarget, receiverTarget],
                                             ["SENDER": .idle, "RECEIVER": .idle])
    RemoteServer.sessionWorkIdentityForTesting = { target in
        target.id == "SENDER" ? sender.identity : receiver.identity
    }

    // The sender contract that comes before any of this. `POST /v1/orchestrator/handoffs` used to
    // take `from_session` as optional and free-form, so the route could not say who sent a
    // handoff — which is how the Clawdfather handoff of 2026-09-04 skipped the whole sequence
    // below without anything noticing. The fixture above is exactly the situation that matters:
    // SENDER holds the coordinator binding at generation 1, RECEIVER is an ordinary session, and
    // one complete observation covers both.
    let handoffAuth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    let liveEvidence = RemoteServer.coordinatorObservationEvidenceForTesting
    func senderPackage(_ id: String) -> String {
        let directory = handoffRoot.appendingPathComponent(id, isDirectory: true)
        try? manager.createDirectory(at: directory, withIntermediateDirectories: true)
        try? Data("# OBJECTIVE\nsender contract\n".utf8)
            .write(to: directory.appendingPathComponent("handoff.md"), options: .atomic)
        return id
    }
    func handoffBody(_ id: String, _ fields: String) -> String {
        "{\"handoff_id\":\"\(id)\",\"project_dir\":\"/tmp\"\(fields)}"
    }
    func postHandoff(_ id: String, _ fields: String) -> RemoteServer.Response {
        RemoteServer.shared.route(remoteRequest(
            "POST", "/v1/orchestrator/handoffs", headers: handoffAuth,
            body: handoffBody(id, fields)))
    }
    func verdict(_ id: String, _ fields: String) -> Orchestrator.HandoffSenderVerdict {
        let request = (try? JSONSerialization.jsonObject(
            with: Data(handoffBody(id, fields).utf8))) as? [String: Any] ?? [:]
        return Orchestrator.handoffSenderVerdict(
            request, evidence: RemoteServer.shared.handoffSenderEvidence())
    }
    let anonymousID = senderPackage("b0000001-0000-4000-8000-000000000001")
    let anonymous = postHandoff(anonymousID, "")
    expect("a handoff that names no sender is refused", anonymous.status, 400)
    expect("and the refusal names the missing field",
           remoteErrorCode(anonymous), "from_session_required")
    expect("an empty sender is the same answer, not a different one",
           verdict(anonymousID, ",\"from_session\":\"   \""), .missing)
    expect("a sender that is not a string is its own refusal",
           verdict(anonymousID, ",\"from_session\":{\"id\":\"SENDER\"}"), .malformed)
    expect("and so is one past the length boundary",
           verdict(anonymousID, ",\"from_session\":\"\(String(repeating: "s", count: 201))\""),
           .malformed)
    let stranger = postHandoff(anonymousID, ",\"from_session\":\"NOBODY\"")
    expect("a sender nothing answers to is refused", stranger.status, 404)
    expect("and is told the id resolved to nobody", remoteErrorCode(stranger), "sender_not_found")
    let cloudID = postHandoff(anonymousID,
                              ",\"from_session\":\"session_012m5AH5gSNtQ8mTzNRAzzS5\"")
    expect("the older cloud-session spelling is refused", cloudID.status, 404)
    expect("under its own code, because the next move is different",
           remoteErrorCode(cloudID), "from_session_wrong_namespace")
    check("the namespace hint is only ever a better message on a refusal",
          Orchestrator.handoffSenderIsCloudSessionID("session_012m5AH5gSNtQ8mTzNRAzzS5")
            && !Orchestrator.handoffSenderIsCloudSessionID("SENDER")
            && !Orchestrator.handoffSenderIsCloudSessionID("session_"))
    expect("the conversation namespace resolves as well as the terminal one",
           verdict(anonymousID, ",\"from_session\":\"receiver-conversation\""),
           .accepted(sessionID: "RECEIVER"))

    // Ambiguity fails closed and never picks one. Two live sessions answering to one spelling is
    // the shape the relay route already refuses; a handoff may not be more willing than a message.
    let twin = coordinatorFixture(
        "RECEIVER-TWIN", assistant: .claude, tty: "/dev/ttys303", pid: 3_003,
        processStart: Date(timeIntervalSince1970: 1_780_003_003),
        conversation: "receiver-conversation")
    RemoteServer.coordinatorSessionsForTesting = [sender, receiver, twin]
    let ambiguous = postHandoff(anonymousID, ",\"from_session\":\"receiver-conversation\"")
    expect("two sessions answering to one id is refused", ambiguous.status, 409)
    expect("and nothing is chosen between them", remoteErrorCode(ambiguous), "sender_ambiguous")
    RemoteServer.coordinatorSessionsForTesting = [sender, receiver]

    // The three answers that are neither allow nor refuse-for-cause. Each is a real projection of
    // a real store or a real reading, because a guard that silently allows when it cannot see is
    // the defect this contract exists to remove.
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: Date(timeIntervalSince1970: 1_780_003_020), generation: 21, complete: false)
    expect("an incomplete reading of the machine proves no sender",
           verdict(anonymousID, ",\"from_session\":\"RECEIVER\""), .inventoryIncomplete)
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: nil, generation: nil, complete: true)
    expect("and neither does a reading that never happened",
           verdict(anonymousID, ",\"from_session\":\"RECEIVER\""), .inventoryIncomplete)
    RemoteServer.coordinatorSessionsForTesting = [receiver]
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: Date(timeIntervalSince1970: 1_780_003_005), generation: 22, complete: true)
    expect("a binding this reading cannot place is not permission to hand off",
           verdict(anonymousID, ",\"from_session\":\"RECEIVER\""), .coordinatorLivenessUnknown)
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: Date(timeIntervalSince1970: 1_780_003_030), generation: 23, complete: true)
    expect("an offline coordinator is a fact, and an ordinary handoff proceeds",
           verdict(anonymousID, ",\"from_session\":\"RECEIVER\""),
           .accepted(sessionID: "RECEIVER"))
    // …and it is that fixture, not the word, that makes it a fact: SENDER is absent from the
    // reading altogether. `offline` covers every current reading holding no row that agrees with
    // the record on assistant, terminal, tty, pid, process start *and* conversation, so a live
    // Claude session whose transcript went unlocated this round stops agreeing while it is still
    // running — the review's `REVIEW PROBE A`, red on the delivered bytes as `accepted("SENDER")`.
    let unprovedSender = Coordinator.LiveSession(
        identity: Orchestrator.SessionWorkIdentity(
            terminalID: "SENDER", assistant: .codex, tty: "/dev/ttys301", pid: 3_001,
            processStart: Date(timeIntervalSince1970: 1_780_003_001), conversationID: nil),
        label: "ordinary work", cwd: "/Users/me/code/clawdline", workState: .ready,
        waitingOnSession: false, hasWaiters: false, closeability: nil)
    RemoteServer.coordinatorSessionsForTesting = [unprovedSender, receiver]
    expect("a live coordinator this reading cannot place is not a coordinator that is gone",
           verdict(anonymousID, ",\"from_session\":\"SENDER\""), .coordinatorLivenessUnknown)
    expect("and an ordinary sender is refused by it too, because what cannot be placed is the "
           + "binding and not the asker",
           verdict(anonymousID, ",\"from_session\":\"RECEIVER\""), .coordinatorLivenessUnknown)
    RemoteServer.coordinatorSessionsForTesting = [receiver]
    let corruptStore = directory.appendingPathComponent("coordinator-corrupt.json")
    try! Data("{".utf8).write(to: corruptStore, options: .atomic)
    Coordinator.storeURLOverrideForTesting = corruptStore
    Coordinator.forgetForTesting()
    expect("a coordinator record that cannot be read is not an absent one",
           verdict(anonymousID, ",\"from_session\":\"RECEIVER\""), .coordinatorStoreUnreadable)
    Coordinator.storeURLOverrideForTesting = directory
        .appendingPathComponent("coordinator.json")
    Coordinator.forgetForTesting()
    RemoteServer.coordinatorSessionsForTesting = [sender, receiver]
    RemoteServer.coordinatorObservationEvidenceForTesting = liveEvidence

    // The refusal this task exists for, and the one that proves it does not refuse everything.
    let crownID = senderPackage("b0000002-0000-4000-8000-000000000002")
    let crown = postHandoff(crownID, ",\"from_session\":\"SENDER\"")
    expect("the coordinator's own plain handoff is refused", crown.status, 409)
    expect("and is told which route replaces it", remoteErrorCode(crown), "succession_required")
    let crownBody = (try? JSONSerialization.jsonObject(with: crown.body)) as? [String: Any]
    let crownError = crownBody?["error"] as? [String: Any]
    expect("the refusal names the succession route", crownError?["route"] as? String,
           "POST /v1/orchestrator/coordinator/successions")
    expect("and carries the compare-and-swap id the caller would have to look up",
           crownError?["coordinator_id"] as? String, coordinatorID)
    expect("and its current generation", crownError?["expected_generation"] as? Int, 1)
    expect("and the sender id that request takes",
           crownError?["sender_session_id"] as? String, "SENDER")

    var senderContractOpens = 0
    let ordinaryID = senderPackage("b0000003-0000-4000-8000-000000000003")
    let ordinary = Orchestrator.handoff(
        (try? JSONSerialization.jsonObject(
            with: Data(handoffBody(ordinaryID, ",\"from_session\":\"RECEIVER\"").utf8)))
            as? [String: Any] ?? [:],
        sender: { RemoteServer.shared.handoffSenderEvidence() },
        start: { _, _, _, _ in
            senderContractOpens += 1
            return .started(id: "ORDINARY", backend: .tmux, attach: nil)
        })
    var ordinaryRecord: [String: Any] = [:]
    if case .ok(let payload) = ordinary,
       let record = payload["handoff"] as? [String: Any] { ordinaryRecord = record }
    expect("a handoff from an ordinary session still opens its tab", senderContractOpens, 1)
    expect("and it records the sender it proved",
           ordinaryRecord["fromSession"] as? String, "RECEIVER")
    check("an ordinary handoff asserts nothing about the coordinator",
          !ordinaryRecord.isEmpty && ordinaryRecord["coordinatorPlainHandoff"] == nil)

    let waivedID = senderPackage("b0000004-0000-4000-8000-000000000004")
    let waived = Orchestrator.handoff(
        (try? JSONSerialization.jsonObject(with: Data(handoffBody(
            waivedID,
            ",\"from_session\":\"SENDER\",\"coordinator_plain_handoff\":true").utf8)))
            as? [String: Any] ?? [:],
        sender: { RemoteServer.shared.handoffSenderEvidence() },
        start: { _, _, _, _ in
            senderContractOpens += 1
            return .started(id: "WAIVED", backend: .tmux, attach: nil)
        })
    var waivedRecord: [String: Any] = [:]
    if case .ok(let payload) = waived,
       let record = payload["handoff"] as? [String: Any] { waivedRecord = record }
    expect("a deliberate plain handoff from the coordinator is allowed", senderContractOpens, 2)
    check("and the envelope records that it was a decision",
          waivedRecord["coordinatorPlainHandoff"] as? Bool == true)
    check("the waiver survives the store round trip",
          OrchestratorStore.handoff(from: OrchestratorStore.stored(
            Orchestrator.HandoffEnvelope(
                id: waivedID, projectDir: "/tmp", title: nil, fromSession: "SENDER",
                coordinatorPlainHandoff: true, created: Date(timeIntervalSince1970: 1_780_003_040),
                state: .opening)))?.coordinatorPlainHandoff == true)
    check("and an envelope written before it existed reads as no assertion",
          OrchestratorStore.handoff(from: [
            "handoff_id": waivedID, "project_dir": "/tmp", "created": 1_780_003_040.0,
            "state": "delivered"])?.coordinatorPlainHandoff == false)
    check("the waiver never waives the resolution in front of it",
          verdict(anonymousID, ",\"coordinator_plain_handoff\":true") == .missing)
    check("and a field whose meaning is a decision has no false",
          Orchestrator.handoffDraft(
            from: ["handoff_id": anonymousID, "project_dir": "/tmp",
                   "from_session": "SENDER", "coordinator_plain_handoff": false],
            isDirectory: { _ in true }, packageIsReady: { _ in true }).isBad)
    // The waiver's *value*, a different question from its position. `JSONSerialization` returns
    // `NSNumber` for a JSON `true` and a JSON `1` alike and `NSNumber(1) as? Bool` is `true`, so
    // the old cast let a client serialising booleans as 0 and 1 waive the only refusal protecting
    // the binding without deciding anything — the review's `PROBE B` and `PROBE C`, red here.
    let numericWaiver = (try? JSONSerialization.jsonObject(with: Data(handoffBody(
        anonymousID, ",\"from_session\":\"SENDER\",\"coordinator_plain_handoff\":1").utf8)))
        as? [String: Any] ?? [:]
    check("a JSON 1 is not the exactly-true this field is documented to take",
          Orchestrator.handoffDraft(from: numericWaiver, isDirectory: { _ in true },
                                    packageIsReady: { _ in true }).isBad)
    expect("and a JSON 1 does not waive the succession refusal either",
           Orchestrator.handoffSenderVerdict(
            numericWaiver, evidence: RemoteServer.shared.handoffSenderEvidence()),
           .successionRequired(sessionID: "SENDER", coordinatorID: coordinatorID, generation: 1))
    check("the strictness is about the value, so a native true still asserts",
          Orchestrator.handoffWaiverAsserted(true)
            && !Orchestrator.handoffWaiverAsserted(false)
            && !Orchestrator.handoffWaiverAsserted(1) && !Orchestrator.handoffWaiverAsserted("true")
            && !Orchestrator.handoffWaiverAsserted(NSNumber(value: 1)))

    let requestID = "88888888-9999-4aaa-8bbb-cccccccccccc"
    let handoffID = "99999999-aaaa-4bbb-8ccc-dddddddddddd"
    let package = handoffRoot.appendingPathComponent(handoffID, isDirectory: true)
    try! manager.createDirectory(at: package, withIntermediateDirectories: true)
    try! Data("# OBJECTIVE\ncontinue\n".utf8)
        .write(to: package.appendingPathComponent("handoff.md"), options: .atomic)
    let request = """
    {"request_id":"\(requestID)","expected_coordinator_id":"\(coordinatorID)",
     "expected_generation":1,"sender_session_id":"SENDER","handoff":{
       "handoff_id":"\(handoffID)","project_dir":"/tmp","assistant":"claude",
       "title":"Clawdfather succession"}}
    """
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    var opens = 0
    CoordinatorSuccession.handoffStarterForTesting = { _, _, _, _ in
        opens += 1
        return .started(id: "RECEIVER", backend: .iterm, attach: nil)
    }
    let createPath = "/v1/orchestrator/coordinator/successions"
    let created = RemoteServer.shared.route(remoteRequest(
        "POST", createPath, headers: auth, body: request))
    expect("a succession request is accepted", created.status, 200)
    let createdObject = (try? JSONSerialization.jsonObject(with: created.body)) as? [String: Any]
    let createdRecord = createdObject?["succession"] as? [String: Any]
    check("accepted and receiver-opened are separate durable receipts",
          createdRecord?["accepted_at"] != nil
            && createdRecord?["receiver_open_requested_at"] != nil
            && createdRecord?["receiver_opened_at"] != nil
            && createdRecord?["receiver_session_id"] as? String == "RECEIVER")
    expect("the receiver tab is opened once", opens, 1)

    let duplicate = RemoteServer.shared.route(remoteRequest(
        "POST", createPath, headers: auth, body: request))
    expect("an exact duplicate request is idempotent", duplicate.status, 200)
    expect("an exact duplicate opens no second receiver", opens, 1)
    let conflictingRequest = request.replacingOccurrences(
        of: "Clawdfather succession", with: "different request under the same id")
    let conflict = RemoteServer.shared.route(remoteRequest(
        "POST", createPath, headers: auth, body: conflictingRequest))
    expect("a reused request id with different content is refused", conflict.status, 409)
    expect("the duplicate-content refusal is typed", remoteErrorCode(conflict),
           "succession_request_conflict")

    let lostRequestID = "aaaaaaa1-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let lostHandoffID = "aaaaaaa2-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let lostPackage = handoffRoot.appendingPathComponent(lostHandoffID, isDirectory: true)
    try! manager.createDirectory(at: lostPackage, withIntermediateDirectories: true)
    try! Data("# OBJECTIVE\nopen once\n".utf8)
        .write(to: lostPackage.appendingPathComponent("handoff.md"), options: .atomic)
    let lostRequest = """
    {"request_id":"\(lostRequestID)","expected_coordinator_id":"\(coordinatorID)",
     "expected_generation":1,"sender_session_id":"SENDER","handoff":{
       "handoff_id":"\(lostHandoffID)","project_dir":"/tmp","assistant":"claude",
       "title":"lost open receipt"}}
    """
    var lostSaves = 0
    var lostOpens = 0
    CoordinatorSuccession.storeSaveInterceptorForTesting = { _ in
        lostSaves += 1
        return lostSaves == 3 ? false : nil
    }
    CoordinatorSuccession.handoffStarterForTesting = { _, _, _, _ in
        lostOpens += 1
        return .started(id: "LOST-RECEIVER", backend: .iterm, attach: nil)
    }
    let lostReceipt = RemoteServer.shared.route(remoteRequest(
        "POST", createPath, headers: auth, body: lostRequest))
    expect("a crash-sized save failure after opening is surfaced", lostReceipt.status, 500)
    expect("the receiver side effect happened only once before its receipt was lost", lostOpens, 1)
    CoordinatorSuccession.storeSaveInterceptorForTesting = nil
    let lostRetry = RemoteServer.shared.route(remoteRequest(
        "POST", createPath, headers: auth, body: lostRequest))
    expect("restart refuses to guess after an unreceipted receiver open", lostRetry.status, 409)
    expect("the ambiguous restart is typed", remoteErrorCode(lostRetry),
           "succession_receiver_open_receipt_lost")
    expect("the ambiguous retry never opens a duplicate receiver", lostOpens, 1)

    let failedRequestID = "aaaaaaa3-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let failedHandoffID = "aaaaaaa4-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let failedPackage = handoffRoot.appendingPathComponent(failedHandoffID, isDirectory: true)
    try! manager.createDirectory(at: failedPackage, withIntermediateDirectories: true)
    try! Data("# OBJECTIVE\nfail delivery\n".utf8)
        .write(to: failedPackage.appendingPathComponent("handoff.md"), options: .atomic)
    let failedRequest = """
    {"request_id":"\(failedRequestID)","expected_coordinator_id":"\(coordinatorID)",
     "expected_generation":1,"sender_session_id":"SENDER","handoff":{
       "handoff_id":"\(failedHandoffID)","project_dir":"/tmp","assistant":"claude",
       "title":"receiver delivery failure"}}
    """
    var failedStarts = 0
    CoordinatorSuccession.handoffStarterForTesting = { _, _, _, _ in
        failedStarts += 1
        return .refused(status: 503, code: "terminal_closed",
                        message: "injected receiver refusal", app: "iTerm2")
    }
    let failedDelivery = RemoteServer.shared.route(remoteRequest(
        "POST", createPath, headers: auth, body: failedRequest))
    expect("receiver delivery failure remains the HTTP failure", failedDelivery.status, 503)
    expect("receiver delivery failure is typed at the succession boundary",
           remoteErrorCode(failedDelivery), "succession_receiver_delivery_failed")
    let failedDuplicate = RemoteServer.shared.route(remoteRequest(
        "POST", createPath, headers: auth, body: failedRequest))
    expect("a failed delivery retry returns its durable failure", failedDuplicate.status, 409)
    expect("a failed delivery retry opens nothing else", failedStarts, 1)

    CoordinatorSuccession.handoffStarterForTesting = { _, _, _, _ in
        opens += 1
        return .started(id: "RECEIVER", backend: .iterm, attach: nil)
    }

    Orchestrator.settleHandoff(handoffID, delivered: true, assistant: .claude, why: nil)
    let ackPath = "/v1/orchestrator/coordinator/successions/\(requestID)/ack"
    let senderAck = RemoteServer.shared.route(remoteRequest(
        "POST", ackPath, headers: auth,
        body: #"{"session_id":"SENDER","receipt":"sender_accepted"}"#))
    expect("the sender explicitly observes acceptance", senderAck.status, 200)
    let pickup = RemoteServer.shared.route(remoteRequest(
        "POST", ackPath, headers: auth,
        body: #"{"session_id":"RECEIVER","receipt":"receiver_picked_up"}"#))
    expect("the receiver explicitly acknowledges package pickup", pickup.status, 200)
    let pickupObject = (try? JSONSerialization.jsonObject(with: pickup.body)) as? [String: Any]
    let pickupRecord = pickupObject?["succession"] as? [String: Any]
    check("delivery, sender observation and pickup stay three receipts",
          pickupRecord?["package_delivered_at"] != nil
            && pickupRecord?["sender_observed_at"] != nil
            && pickupRecord?["pickup_observed_at"] != nil)

    CoordinatorSuccessionService.closeProofForTesting = { _ in
        (state: .safe, version: "cl1-safe", lost: [])
    }
    RemoteServer.sessionEndForTesting = { _ in "injected close refusal" }
    let advancePath = "/v1/orchestrator/coordinator/successions/\(requestID)/advance"
    let closeRefused = RemoteServer.shared.route(remoteRequest(
        "POST", advancePath, headers: auth, body: "{}"))
    expect("a terminal close refusal is not hidden as progress", closeRefused.status, 502)
    expect("the sender close refusal is typed", remoteErrorCode(closeRefused),
           "succession_sender_close_refused")
    let afterRefusal = CoordinatorSuccession.publicRecord(requestID: requestID)
    check("drain proof and close request survive the refused terminal action",
          afterRefusal?["sender_drain_proven_at"] != nil
            && afterRefusal?["sender_close_requested_at"] != nil
            && (afterRefusal?["last_error"] as? [String: Any])?["code"] as? String
                == "succession_sender_close_refused")

    var closes = 0
    RemoteServer.sessionEndForTesting = { _ in closes += 1; return nil }
    let closeRetry = RemoteServer.shared.route(remoteRequest(
        "POST", advancePath, headers: auth, body: "{}"))
    expect("a safe retry closes the sender", closeRetry.status, 200)
    expect("the terminal close is attempted exactly once on that retry", closes, 1)
    let beforeOffline = CoordinatorSuccession.publicRecord(requestID: requestID)
    check("a successful close request is not mistaken for exact-offline proof",
          beforeOffline?["sender_close_requested_at"] != nil
            && beforeOffline?["old_binding_offline_at"] == nil
            && beforeOffline?["rebind_committed_at"] == nil)
    let stillOld = Coordinator.inspection(
        liveSessions: [sender, receiver],
        bearings: .init(sessionsFresh: true, activeTaskCount: 0,
                        pendingLandingCount: 0, openWaitCount: 0,
                        sessionsObservedAt: Date(timeIntervalSince1970: 1_780_003_025)))
    expect("the old online binding keeps its generation before offline proof",
           (stillOld["coordinator"] as? [String: Any])?["generation"] as? Int, 1)

    RemoteServer.coordinatorSessionsForTesting = [receiver]
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: Date(timeIntervalSince1970: 1_780_003_030), generation: 21,
        complete: true)
    var rebindReceiptSaves = 0
    CoordinatorSuccession.storeSaveInterceptorForTesting = { _ in
        rebindReceiptSaves += 1
        return rebindReceiptSaves == 2 ? false : nil
    }
    let interruptedRebind = RemoteServer.shared.route(remoteRequest(
        "POST", advancePath, headers: auth, body: "{}"))
    expect("a crash-sized failure after compare-and-swap is surfaced",
           interruptedRebind.status, 500)
    let afterRebindSideEffect = Coordinator.inspection(
        liveSessions: [receiver],
        bearings: .init(sessionsFresh: true, activeTaskCount: 0,
                        pendingLandingCount: 0, openWaitCount: 0,
                        sessionsObservedAt: Date(timeIntervalSince1970: 1_780_003_031)))
    expect("the Coordinator side effect still advanced exactly once",
           (afterRebindSideEffect["coordinator"] as? [String: Any])?["generation"] as? Int, 2)
    let afterInterruptedRebind = CoordinatorSuccession.publicRecord(requestID: requestID)
    check("restart recovery has offline proof but no invented rebind receipt",
          afterInterruptedRebind?["old_binding_offline_at"] != nil
            && afterInterruptedRebind?["rebind_committed_at"] == nil
            && afterInterruptedRebind?["receiver_online_at"] == nil)
    CoordinatorSuccession.storeSaveInterceptorForTesting = nil
    let rebound = RemoteServer.shared.route(remoteRequest(
        "POST", advancePath, headers: auth, body: "{}"))
    expect("a fresh exact-offline observation advances through rebind", rebound.status, 200)
    let reboundObject = (try? JSONSerialization.jsonObject(with: rebound.body)) as? [String: Any]
    let reboundRecord = reboundObject?["succession"] as? [String: Any]
    check("offline proof, rebind commit and receiver-online remain separate receipts",
          reboundRecord?["old_binding_offline_at"] != nil
            && reboundRecord?["old_binding_offline_generation"] as? Int == 21
            && reboundRecord?["rebind_committed_at"] != nil
            && reboundRecord?["receiver_online_at"] != nil)
    expect("the stable coordinator advances exactly one generation",
           (reboundObject?["coordinator"] as? [String: Any])?["generation"] as? Int, 2)

    let observed = RemoteServer.shared.route(remoteRequest(
        "POST", ackPath, headers: auth,
        body: #"{"session_id":"RECEIVER","receipt":"receiver_completed"}"#))
    expect("the replacement explicitly observes completed succession", observed.status, 200)
    let observedObject = (try? JSONSerialization.jsonObject(with: observed.body)) as? [String: Any]
    let observedRecord = observedObject?["succession"] as? [String: Any]
    check("only the receiver observation closes the durable succession",
          observedRecord?["state"] as? String == "complete"
            && observedRecord?["receiver_observed_at"] != nil)
    CoordinatorSuccession.forgetForTesting()
    let afterRestart = CoordinatorSuccession.publicRecord(requestID: requestID)
    let durableBoundaries = [
        "accepted_at", "receiver_open_requested_at", "receiver_opened_at",
        "package_delivered_at", "sender_observed_at", "pickup_observed_at",
        "sender_drain_proven_at", "sender_close_requested_at", "old_binding_offline_at",
        "rebind_committed_at", "receiver_online_at", "receiver_observed_at"
    ]
    check("a restart reload preserves every distinct succession receipt",
          durableBoundaries.allSatisfy { afterRestart?[$0] != nil })

    let boundaryRequestID = "aaaaaaa7-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let boundaryHandoffID = "aaaaaaa8-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let boundaryDraft = CoordinatorSuccession.Draft(
        requestID: boundaryRequestID, coordinatorID: coordinatorID,
        expectedGeneration: 2, senderSessionID: "BOUNDARY-SENDER",
        handoffID: boundaryHandoffID, projectDir: "/tmp", assistant: .codex,
        model: nil, title: "boundary failures")
    func successionRefused(_ reply: Orchestrator.Reply) -> Bool {
        if case .refused = reply { return true }
        return false
    }
    CoordinatorSuccession.storeSaveInterceptorForTesting = { _ in false }
    let failedAccept = CoordinatorSuccession.accept(boundaryDraft)
    CoordinatorSuccession.storeSaveInterceptorForTesting = nil
    check("accept persistence failure goes red", successionRefused(failedAccept))
    check("failed accept leaves no invented durable row",
          CoordinatorSuccession.publicRecord(requestID: boundaryRequestID) == nil)
    _ = CoordinatorSuccession.accept(boundaryDraft)
    CoordinatorSuccession.forgetForTesting()
    check("accepted receipt survives restart",
          CoordinatorSuccession.publicRecord(requestID: boundaryRequestID)?["accepted_at"] != nil)

    func failThenCommitBoundary(_ name: String, key: String,
                                _ transition: () -> Orchestrator.Reply) {
        CoordinatorSuccession.storeSaveInterceptorForTesting = { _ in false }
        let failed = transition()
        CoordinatorSuccession.storeSaveInterceptorForTesting = nil
        check("\(name) persistence failure goes red", successionRefused(failed))
        CoordinatorSuccession.forgetForTesting()
        check("\(name) failure leaves its receipt absent",
              CoordinatorSuccession.publicRecord(requestID: boundaryRequestID)?[key] == nil)
        _ = transition()
        CoordinatorSuccession.forgetForTesting()
        check("\(name) receipt survives restart",
              CoordinatorSuccession.publicRecord(requestID: boundaryRequestID)?[key] != nil)
    }
    failThenCommitBoundary("receiver-open request", key: "receiver_open_requested_at") {
        CoordinatorSuccession.markOpenRequested(boundaryRequestID)
    }
    failThenCommitBoundary("receiver-open identity", key: "receiver_opened_at") {
        CoordinatorSuccession.markReceiverOpened(
            boundaryRequestID, receiverSessionID: "BOUNDARY-RECEIVER")
    }
    failThenCommitBoundary("package delivery", key: "package_delivered_at") {
        CoordinatorSuccession.markPackageDelivered(boundaryRequestID)
    }
    failThenCommitBoundary("sender observation", key: "sender_observed_at") {
        CoordinatorSuccession.acknowledge(
            boundaryRequestID, sessionID: "BOUNDARY-SENDER", receipt: "sender_accepted")
    }
    failThenCommitBoundary("receiver pickup", key: "pickup_observed_at") {
        CoordinatorSuccession.acknowledge(
            boundaryRequestID, sessionID: "BOUNDARY-RECEIVER", receipt: "receiver_picked_up")
    }
    failThenCommitBoundary("sender drain proof", key: "sender_drain_proven_at") {
        CoordinatorSuccession.markDrainProven(
            boundaryRequestID, closeabilityVersion: "boundary-safe")
    }
    failThenCommitBoundary("sender-close request", key: "sender_close_requested_at") {
        CoordinatorSuccession.markCloseRequested(boundaryRequestID)
    }
    failThenCommitBoundary("exact-offline proof", key: "old_binding_offline_at") {
        CoordinatorSuccession.markOldBindingOffline(
            boundaryRequestID, observedAt: Date(), sessionsGeneration: 77)
    }
    failThenCommitBoundary("rebind commit", key: "rebind_committed_at") {
        CoordinatorSuccession.markRebindCommitted(boundaryRequestID)
    }
    failThenCommitBoundary("receiver-online proof", key: "receiver_online_at") {
        CoordinatorSuccession.markReceiverOnline(boundaryRequestID)
    }
    failThenCommitBoundary("receiver completion", key: "receiver_observed_at") {
        CoordinatorSuccession.acknowledge(
            boundaryRequestID, sessionID: "BOUNDARY-RECEIVER", receipt: "receiver_completed")
    }

    let next = coordinatorFixture(
        "NEXT", assistant: .claude, tty: "/dev/ttys303", pid: 3_003,
        processStart: Date(timeIntervalSince1970: 1_780_003_003),
        conversation: "next-conversation")
    let rogue = coordinatorFixture(
        "ROGUE", tty: "/dev/ttys304", pid: 3_004,
        processStart: Date(timeIntervalSince1970: 1_780_003_004),
        conversation: "rogue-conversation")
    RemoteServer.coordinatorSessionsForTesting = [receiver, next, rogue]
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: Date(), generation: 22, complete: true)
    let staleRequestID = "aaaaaaa5-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let staleHandoffID = "aaaaaaa6-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    let stalePackage = handoffRoot.appendingPathComponent(staleHandoffID, isDirectory: true)
    try! manager.createDirectory(at: stalePackage, withIntermediateDirectories: true)
    try! Data("# OBJECTIVE\nstale generation\n".utf8)
        .write(to: stalePackage.appendingPathComponent("handoff.md"), options: .atomic)
    let staleRequest = """
    {"request_id":"\(staleRequestID)","expected_coordinator_id":"\(coordinatorID)",
     "expected_generation":2,"sender_session_id":"RECEIVER","handoff":{
       "handoff_id":"\(staleHandoffID)","project_dir":"/tmp","assistant":"claude",
       "title":"stale generation"}}
    """
    CoordinatorSuccession.handoffStarterForTesting = { _, _, _, _ in
        .started(id: "NEXT", backend: .iterm, attach: nil)
    }
    let staleCreated = RemoteServer.shared.route(remoteRequest(
        "POST", createPath, headers: auth, body: staleRequest))
    expect("the second succession begins at the current generation", staleCreated.status, 200)
    Orchestrator.settleHandoff(staleHandoffID, delivered: true, assistant: .claude, why: nil)
    let staleAckPath = "/v1/orchestrator/coordinator/successions/\(staleRequestID)/ack"
    _ = RemoteServer.shared.route(remoteRequest(
        "POST", staleAckPath, headers: auth,
        body: #"{"session_id":"RECEIVER","receipt":"sender_accepted"}"#))
    _ = RemoteServer.shared.route(remoteRequest(
        "POST", staleAckPath, headers: auth,
        body: #"{"session_id":"NEXT","receipt":"receiver_picked_up"}"#))
    let externalObservation = Date()
    let externalRebind = Coordinator.rebind(
        expectedCoordinatorID: coordinatorID, expectedGeneration: 2, to: rogue,
        among: [next, rogue], sessionsFresh: true,
        sessionsObservedAt: externalObservation,
        now: externalObservation.addingTimeInterval(1))
    if case .ok = externalRebind { check("failure injection advances outside the succession", true) }
    else { check("failure injection advances outside the succession", false) }
    RemoteServer.coordinatorSessionsForTesting = [next, rogue]
    RemoteServer.coordinatorObservationEvidenceForTesting = (
        observedAt: externalObservation.addingTimeInterval(2), generation: 23,
        complete: true)
    let staleAdvancePath = "/v1/orchestrator/coordinator/successions/\(staleRequestID)/advance"
    let staleAdvance = RemoteServer.shared.route(remoteRequest(
        "POST", staleAdvancePath, headers: auth, body: "{}"))
    expect("an externally advanced generation cannot be inherited", staleAdvance.status, 409)
    expect("the stale generation refusal is typed", remoteErrorCode(staleAdvance),
           "succession_generation_stale")
}
}
