import Foundation

private func persistenceErrorCode(_ response: RemoteServer.Response) -> String? {
    let object = (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]
    return (object?["error"] as? [String: Any])?["code"] as? String
}

func runOrchestratorPersistenceTests() {
    group("an absent broker store is authoritative first-run state") {
        let manager = FileManager.default
        let root = manager.temporaryDirectory
            .appendingPathComponent("orchestrator-persistence-absent-\(UUID().uuidString)",
                                    isDirectory: true)
        try! manager.createDirectory(at: root, withIntermediateDirectories: true)
        let previous = Orchestrator.storeURLOverrideForTesting
        Orchestrator.storeURLOverrideForTesting = root.appendingPathComponent("orchestrator.json")
        Orchestrator.forget()
        defer {
            Orchestrator.storeURLOverrideForTesting = previous
            Orchestrator.forget()
            try? manager.removeItem(at: root)
        }

        check("a missing file loads as a real empty registry", Orchestrator.load(force: true))
        let health = Orchestrator.storeHealthRecord()
        expect("the first-run state is named", health["status"] as? String, "absent")
        expect("and is authoritative", health["authoritative"] as? Bool, true)
        check("the first durable write is allowed", Orchestrator.save())
        expect("a successful write advances read health", Orchestrator.storeHealthRecord()["status"] as? String,
               "ready")
    }

    group("corrupt, partial and future broker stores never become authoritative empty") {
        let manager = FileManager.default
        let root = manager.temporaryDirectory
            .appendingPathComponent("orchestrator-persistence-invalid-\(UUID().uuidString)",
                                    isDirectory: true)
        try! manager.createDirectory(at: root, withIntermediateDirectories: true)
        let store = root.appendingPathComponent("orchestrator.json")
        let previous = Orchestrator.storeURLOverrideForTesting
        Orchestrator.storeURLOverrideForTesting = store
        defer {
            Orchestrator.storeURLOverrideForTesting = previous
            Orchestrator.forget()
            try? manager.removeItem(at: root)
        }

        let fixtures: [(String, Data, String)] = [
            ("malformed", Data("{not-json".utf8), "corrupt"),
            ("missing tasks", try! JSONSerialization.data(withJSONObject: ["version": 1]),
             "corrupt"),
            ("wrong row", try! JSONSerialization.data(withJSONObject: [
                "version": 1, "tasks": [["id": "not-a-task"]],
            ]), "corrupt"),
            ("future", try! JSONSerialization.data(withJSONObject: [
                "version": 2, "tasks": [],
            ]), "unsupported_version"),
            ("wrong restart shape", try! JSONSerialization.data(withJSONObject: [
                "version": 1, "tasks": [], "restart": "truncated",
            ]), "corrupt"),
            ("negative obligation generation", try! JSONSerialization.data(withJSONObject: [
                "version": 1, "tasks": [], "obligation_generation": -1,
            ]), "corrupt"),
            ("wrong obligation fingerprint shape", try! JSONSerialization.data(withJSONObject: [
                "version": 1, "tasks": [], "obligation_fingerprint": 7,
            ]), "corrupt"),
        ]

        for (name, bytes, status) in fixtures {
            try! bytes.write(to: store, options: .atomic)
            Orchestrator.forget()
            check("\(name) is refused", !Orchestrator.load(force: true))
            let health = Orchestrator.storeHealthRecord()
            expect("\(name) has typed health", health["status"] as? String, status)
            expect("\(name) is not authoritative", health["authoritative"] as? Bool, false)
            expect("\(name) preserves the canonical bytes", try! Data(contentsOf: store), bytes)
            check("\(name) cannot be overwritten by save", !Orchestrator.save())
            expect("\(name) remains byte-identical after refused save",
                   try! Data(contentsOf: store), bytes)
            let quarantine = root.appendingPathComponent("orchestrator-quarantine",
                                                          isDirectory: true)
            let evidence = try! manager.contentsOfDirectory(at: quarantine,
                                                             includingPropertiesForKeys: nil)
            check("\(name) has an exact readable quarantine copy",
                  evidence.contains { (try? Data(contentsOf: $0)) == bytes })
        }
    }

    group("an unreadable broker path is typed and does not publish task snapshots") {
        let manager = FileManager.default
        let root = manager.temporaryDirectory
            .appendingPathComponent("orchestrator-persistence-unreadable-\(UUID().uuidString)",
                                    isDirectory: true)
        let store = root.appendingPathComponent("orchestrator.json", isDirectory: true)
        try! manager.createDirectory(at: store, withIntermediateDirectories: true)
        let previous = Orchestrator.storeURLOverrideForTesting
        Orchestrator.storeURLOverrideForTesting = store
        Orchestrator.forget()
        defer {
            Orchestrator.storeURLOverrideForTesting = previous
            Orchestrator.forget()
            try? manager.removeItem(at: root)
        }

        check("a directory cannot masquerade as an absent registry",
              !Orchestrator.load(force: true))
        expect("the filesystem refusal is typed",
               Orchestrator.storeHealthRecord()["status"] as? String, "unreadable")
        let snapshot = RemoteServer.orchestratorSnapshot()
        check("a non-authoritative snapshot omits tasks and schedules",
              snapshot["tasks"] == nil && snapshot["schedules"] == nil
                && (snapshot["store"] as? [String: Any])?["authoritative"] as? Bool == false)

        let response = RemoteServer.shared.route(remoteRequest(
            "GET", "/v1/orchestrator/tasks",
            headers: ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]))
        expect("task HTTP fails closed instead of drawing an empty list", response.status, 503)
        expect("the HTTP refusal is stable and typed", persistenceErrorCode(response),
               "orchestrator_store_unavailable")
        let storage = RemoteServer.shared.route(remoteRequest(
            "GET", "/v1/orchestrator/storage",
            headers: ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]))
        expect("the diagnostic storage route remains readable", storage.status, 200)
    }

    group("quarantine refuses an external symlink without losing the rejected store") {
        let manager = FileManager.default
        let root = manager.temporaryDirectory
            .appendingPathComponent("orchestrator-persistence-quarantine-\(UUID().uuidString)",
                                    isDirectory: true)
        let outside = manager.temporaryDirectory
            .appendingPathComponent("orchestrator-persistence-outside-\(UUID().uuidString)",
                                    isDirectory: true)
        try! manager.createDirectory(at: root, withIntermediateDirectories: true)
        try! manager.createDirectory(at: outside, withIntermediateDirectories: true)
        try! manager.createSymbolicLink(
            at: root.appendingPathComponent("orchestrator-quarantine"),
            withDestinationURL: outside)
        let store = root.appendingPathComponent("orchestrator.json")
        let bytes = Data("{broken".utf8)
        try! bytes.write(to: store)
        let previous = Orchestrator.storeURLOverrideForTesting
        Orchestrator.storeURLOverrideForTesting = store
        Orchestrator.forget()
        defer {
            Orchestrator.storeURLOverrideForTesting = previous
            Orchestrator.forget()
            try? manager.removeItem(at: root)
            try? manager.removeItem(at: outside)
        }

        check("the malformed canonical store is still refused", !Orchestrator.load(force: true))
        expect("the symlink prevents a false quarantine receipt",
               Orchestrator.storeHealthRecord()["quarantined"] as? Bool, false)
        expect("the canonical rejected bytes remain exact", try! Data(contentsOf: store), bytes)
        expect("no evidence is written outside the store directory",
               try! manager.contentsOfDirectory(atPath: outside.path), [])
    }

    group("installation secrets are created only when absent and invalid originals are preserved") {
        let manager = FileManager.default
        let root = manager.temporaryDirectory
            .appendingPathComponent("orchestrator-persistence-secrets-\(UUID().uuidString)",
                                    isDirectory: true)
        try! manager.createDirectory(at: root, withIntermediateDirectories: true)
        defer { try? manager.removeItem(at: root) }
        let secret = root.appendingPathComponent("secret")
        let original = Data("invalid-existing-secret".utf8)
        try! original.write(to: secret)
        try! manager.setAttributes([.posixPermissions: 0o644], ofItemAtPath: secret.path)
        check("an existing secret cannot be replaced by first-run creation",
              !OrchestratorPersistence.createSecret(Data("replacement".utf8), at: secret))
        expect("the invalid original remains exact", try! Data(contentsOf: secret), original)
        expect("an insecure existing mode is preserved but not adopted",
               OrchestratorPersistence.readSecret(at: secret), .unreadable)

        let refused = root.appendingPathComponent("refused")
        OrchestratorPersistence.secretProtectionInterceptorForTesting = { _ in false }
        check("a failed protection step refuses secret creation",
              !OrchestratorPersistence.createSecret(Data("unpublished".utf8), at: refused))
        OrchestratorPersistence.secretProtectionInterceptorForTesting = nil
        check("failed secret protection leaves no adoptable canonical file",
              !manager.fileExists(atPath: refused.path))

        let fresh = root.appendingPathComponent("fresh")
        let bytes = Data("new-secret".utf8)
        check("an absent secret is created once", OrchestratorPersistence.createSecret(bytes, at: fresh))
        expect("the created identity reads back exactly",
               OrchestratorPersistence.readSecret(at: fresh), .value(bytes))
        check("a second creator cannot replace it",
              !OrchestratorPersistence.createSecret(Data("other".utf8), at: fresh))
        expect("the original first-run value wins", try! Data(contentsOf: fresh), bytes)
    }

    group("Root Assignment effects remain behind a successful durable write") {
        let manager = FileManager.default
        let root = manager.temporaryDirectory
            .appendingPathComponent("orchestrator-persistence-effects-\(UUID().uuidString)",
                                    isDirectory: true)
        try! manager.createDirectory(at: root, withIntermediateDirectories: true)
        let previousURL = Orchestrator.storeURLOverrideForTesting
        Orchestrator.storeURLOverrideForTesting = root.appendingPathComponent("orchestrator.json")
        Orchestrator.forget()
        defer {
            Orchestrator.storeSaveInterceptorForTesting = nil
            Orchestrator.storeProtectionInterceptorForTesting = nil
            Orchestrator.storeURLOverrideForTesting = previousURL
            Orchestrator.forget()
            try? manager.removeItem(at: root)
        }
        check("the failure-injection store starts authoritative", Orchestrator.load(force: true))

        let now = Date(timeIntervalSince1970: 1_789_100_000)
        var assignment = Orchestrator.RootAssignment(
            id: UUID().uuidString.lowercased(), requestID: UUID().uuidString.lowercased(),
            requestDigest: String(repeating: "a", count: 64), assistant: .codex,
            model: "default", projectDir: "/tmp", label: "Persistence boundary",
            objective: "prove persist before effect", scope: "one assignment",
            constraints: "no terminal side effect", relevantReferences: "W1-5",
            acceptance: "rollback exact owned fields", projectApproved: true,
            created: now, state: .promptReady, language: nil)
        Orchestrator.holdRootAssignmentForTesting(assignment)
        Orchestrator.storeSaveInterceptorForTesting = { _ in false }

        let activatedAt = now.addingTimeInterval(1)
        check("the activation fixture mutates only in memory",
              OrchestratorRegistry.withCoordinationRecords {
                  $0.activateRootAssignment(assignment.id, at: activatedAt)
              })
        check("a refused activation save blocks its effect",
              !Orchestrator.persistRootAssignmentActivation(
                  assignment.id, at: activatedAt, previous: assignment))
        let afterActivation = Orchestrator.rootAssignmentForTesting(assignment.id)
        check("and restores the exact prior activation fields",
              afterActivation?.state == .promptReady && afterActivation?.activeAt == nil)

        assignment = afterActivation!
        let briefedAt = now.addingTimeInterval(2)
        check("the briefing fixture mutates only in memory",
              OrchestratorRegistry.withCoordinationRecords {
                  $0.markRootAssignmentBriefed(assignment.id, at: briefedAt)
              })
        check("a refused briefing save blocks its effect",
              !Orchestrator.persistRootAssignmentBriefing(
                  assignment.id, at: briefedAt, previous: assignment))
        let afterBriefing = Orchestrator.rootAssignmentForTesting(assignment.id)
        check("and restores the exact prior briefing fields",
              afterBriefing?.state == .promptReady && afterBriefing?.briefedAt == nil)

        let injectedAt = now.addingTimeInterval(3)
        check("the injection fixture records one pending attempt",
              OrchestratorRegistry.withCoordinationRecords {
                  $0.recordRootAssignmentInjection(assignment.id, at: injectedAt)
              })
        check("a refused injection save blocks terminal delivery",
              !Orchestrator.persistRootAssignmentInjection(assignment.id, at: injectedAt))
        let afterInjection = Orchestrator.rootAssignmentForTesting(assignment.id)
        check("and removes only that unsent attempt",
              afterInjection?.injectAttempts == 0 && afterInjection?.lastInjectAt == nil)

        Orchestrator.storeSaveInterceptorForTesting = nil
        check("the prompt-ready baseline reaches the canonical store", Orchestrator.save())
        let replacedAt = now.addingTimeInterval(4)
        check("the post-write fixture records another activation",
              OrchestratorRegistry.withCoordinationRecords {
                  $0.activateRootAssignment(assignment.id, at: replacedAt)
              })
        Orchestrator.storeProtectionInterceptorForTesting = { _ in false }
        check("a protection refusal reports the activation as uncommitted",
              !Orchestrator.persistRootAssignmentActivation(
                  assignment.id, at: replacedAt, previous: assignment))
        Orchestrator.storeProtectionInterceptorForTesting = nil
        Orchestrator.forget()
        check("the canonical pre-transition registry remains readable",
              Orchestrator.load(force: true))
        expect("and a restart cannot resurrect the unsent activation",
               Orchestrator.rootAssignmentForTesting(assignment.id)?.state, .promptReady)
    }

    group("the task list drops a finished task's own account and keeps what its readers name") {
        // Not a sample record: every key here is one a real reader of the list was checked to
        // name. The console reads id/title/state/created/root.terminalId/child.terminalId, the
        // pre-commit guard reads claims/projectDir/isolation/finishedAt/state and both identity
        // blocks, and `build.sh` reads state/id/title. The four at the end are the ones measured
        // at 71% of a 3.69 MB answer and named by none of them.
        let record: [String: Any] = [
            "id": "t1", "title": "A task", "state": "success", "created": 0, "finishedAt": 1,
            "claims": ["Sources/"], "projectDir": "/Users/you/code/clawdline",
            "isolation": "worktree", "assistant": "claude",
            "root": ["terminalId": "%1", "sessionId": "S1"],
            "child": ["terminalId": "%2", "sessionId": "S2"],
            "summary": String(repeating: "x", count: 4096),
            "review": ["findings": []],
            "graph": ["nodes": []],
            "progress": [["at": 1, "text": "half way"]],
        ]
        let projected = RemoteServer.taskListProjection(record)

        let named = ["id", "title", "state", "created", "finishedAt", "claims", "projectDir",
                     "isolation", "assistant", "root", "child"]
        let missing = named.filter { projected[$0] == nil }
        check("every field a reader of the list was shown to name survives the projection",
              missing.isEmpty, missing.joined(separator: ", "))

        // The load-bearing one, and it fails in both directions. Dropping a field nobody declared
        // breaks a reader silently; keeping a declared one leaves the bytes this exists to remove.
        let dropped = Set(record.keys).subtracting(projected.keys)
        check("the projection removes exactly the fields it declares and nothing else",
              dropped == RemoteServer.taskListOmittedFields,
              "dropped \(dropped.sorted()), declared \(RemoteServer.taskListOmittedFields.sorted())")

        check("a record holding none of them is returned unchanged",
              RemoteServer.taskListProjection(["id": "t2"]).keys.sorted() == ["id"])
    }
}
