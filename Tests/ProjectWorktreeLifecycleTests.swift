import CryptoKit
import Foundation

// `ProjectWorktreeLifecycleService` is the one probe, classifier, preservation and cleanup owner
// for a Project's registered worktrees; `ProjectWorktreeHTTP` is only its codec. Every fixture here
// is a real repository with real linked worktrees in a private temporary directory, and every port
// the service reads — the task registry, the Session inventory, the clock — is injected, so no
// check depends on this Mac's own registry or checkouts.

private final class WorktreeWorld {
    let root: URL
    let repository: URL
    let managedRoot: URL
    let state: URL
    var tasks: [Orchestrator.Task] = []
    var authoritative = true
    var live = ProjectWorktreeLifecycleService.LiveEvidence(complete: true, observedAt: Date(), sessions: [])
    var clock = Date()
    var base = ""
    var beforeApplyAction: ((ProjectWorktreeLifecycleService.ActionKind, String) -> Void)?
    private(set) lazy var service = ProjectWorktreeLifecycleService(ports: .init(
        stateDirectory: state, managedWorktreeRoot: managedRoot,
        projectDirectories: { [unowned self] in [self.repository.path] },
        tasks: { [unowned self] in .init(authoritative: self.authoritative, tasks: self.tasks) },
        live: { [unowned self] in self.live },
        now: { [unowned self] in self.clock },
        beforeApplyAction: { [unowned self] kind, worktreeID in
            self.beforeApplyAction?(kind, worktreeID)
        }))

    init() {
        root = FileManager.default.temporaryDirectory
            .appendingPathComponent("clawdline-worktrees-\(UUID().uuidString)", isDirectory: true)
        repository = root.appendingPathComponent("repo", isDirectory: true)
        managedRoot = root.appendingPathComponent("managed", isDirectory: true)
        state = root.appendingPathComponent("state", isDirectory: true)
        try? FileManager.default.createDirectory(at: repository, withIntermediateDirectories: true)
        try? FileManager.default.createDirectory(at: managedRoot.appendingPathComponent("repo-1234"),
                                                 withIntermediateDirectories: true)
        git(["init", "-q", "-b", "main"], repository)
        write("README.md", "base\n", in: repository)
        commit("base", in: repository)
        base = git(["rev-parse", "HEAD"], repository).output
    }

    deinit { try? FileManager.default.removeItem(at: root) }

    var projectID: String {
        ProjectWorktreeLifecycleService.projectIdentity(forDirectory: repository.path)?.id ?? "missing"
    }

    @discardableResult
    func git(_ arguments: [String], _ cwd: URL) -> (status: Int32, output: String) {
        testGit(["-c", "user.name=Clawdline Tests", "-c", "user.email=tests@clawdline.invalid"] + arguments, cwd: cwd)
    }

    func write(_ path: String, _ text: String, in directory: URL) {
        let url = directory.appendingPathComponent(path)
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try? Data(text.utf8).write(to: url)
    }

    func commit(_ message: String, in directory: URL) {
        git(["add", "-A"], directory)
        git(["commit", "-qm", message], directory)
    }

    /// A Clawdline-shaped task checkout: managed path, task branch, recorded base.
    func addTask(_ state: Orchestrator.State) -> (id: String, path: URL) {
        let id = UUID().uuidString.lowercased()
        let path = managedRoot.appendingPathComponent("repo-1234/\(id)", isDirectory: true)
        git(["worktree", "add", "-q", "-b", "clawdline/task/\(id)", path.path, base], repository)
        var task = Orchestrator.Task(id: id, state: state, kind: "custom", title: "fixture \(id.prefix(8))",
                                     assistant: .claude, projectDir: repository.path, timeoutMinutes: 30,
                                     created: Date(), secretHash: String(repeating: "0", count: 64))
        task.isolation = .worktree
        task.worktree = Orchestrator.Worktree(path: path.path, branch: "clawdline/task/\(id)", base: base,
                                              repository: repository.path, cwd: path.path)
        task.childSessionId = UUID().uuidString
        task.childTerminalId = "%9\(tasks.count)"
        task.rootLabel = "Fixture root"
        tasks.append(task)
        return (id, path)
    }

    func refresh() -> [String: Any] {
        guard case .success(let value) = service.refresh(projectID: projectID) else { return [:] }
        return value
    }

    func row(_ snapshot: [String: Any], _ path: URL) -> [String: Any] {
        let wanted = ProjectWorktreeLifecycleService.comparablePath(path.path)
        return (snapshot["rows"] as? [[String: Any]] ?? []).first {
            ProjectWorktreeLifecycleService.comparablePath($0["path"] as? String ?? "") == wanted
        } ?? [:]
    }

    func classes(_ snapshot: [String: Any], _ path: URL) -> [String] {
        row(snapshot, path)["classifications"] as? [String] ?? ["<missing row>"]
    }

    func blockers(_ snapshot: [String: Any], _ path: URL) -> [String] {
        ((row(snapshot, path)["cleanup"] as? [String: Any])?["blockers"] as? [[String: Any]] ?? [])
            .compactMap { $0["code"] as? String }
    }
}

private func refusalCode<T>(_ result: Result<T, ProjectWorktreeLifecycleService.Refusal>) -> String {
    if case .failure(let refusal) = result { return refusal.code }
    return "succeeded"
}

private func actionErrorCodes(
    _ result: Result<[String: Any], ProjectWorktreeLifecycleService.Refusal>
) -> [String] {
    guard case .success(let receipt) = result else { return [] }
    return (receipt["actions"] as? [[String: Any]] ?? []).compactMap {
        ($0["error"] as? [String: Any])?["code"] as? String
    }
}

func runProjectWorktreeLifecycleTests() {
    group("worktree lifecycle classifies all seven classes and keeps mixed facts together") {
        let world = WorktreeWorld()
        check("a read before any observation says not_observed and runs nothing",
              {
                  guard case .success(let snapshot) = world.service.snapshot(projectID: world.projectID) else { return false }
                  return (snapshot["error"] as? [String: Any])?["code"] as? String == "not_observed"
                      && snapshot["complete"] as? Bool == false
                      && (snapshot["counts"] as? [String: Any])?["rows"] is NSNull
                      && !FileManager.default.fileExists(atPath: world.state.path)
              }())
        let temporary = world.addTask(.success)
        let merged = world.addTask(.success)
        let identical = world.addTask(.success)
        let unlanded = world.addTask(.failure)
        let mixed = world.addTask(.success)
        let prunable = world.addTask(.cancelled)
        let live = world.addTask(.briefed)
        let orphan = world.addTask(.success)
        world.tasks.removeAll { $0.id == orphan.id }
        let foreign = world.root.appendingPathComponent("foreign", isDirectory: true)
        world.git(["worktree", "add", "-q", "--detach", foreign.path, world.base], world.repository)

        world.write("feature.txt", "delivered\n", in: merged.path)
        world.commit("delivery", in: merged.path)
        world.git(["merge", "-q", "--ff-only", "clawdline/task/\(merged.id)"], world.repository)
        world.write("README.md", "landed\n", in: world.repository)
        world.write("a.txt", "same\n", in: world.repository)
        world.commit("landing", in: world.repository)
        world.write("README.md", "landed\n", in: identical.path)
        world.write("draft.txt", "unlanded draft\n", in: unlanded.path)
        world.write("a.txt", "same\n", in: mixed.path)
        world.git(["add", "a.txt"], mixed.path)
        world.write("README.md", "not what landed\n", in: mixed.path)
        world.write("b.txt", "new\n", in: mixed.path)
        try? FileManager.default.removeItem(at: prunable.path)

        let snapshot = world.refresh()
        expect("an unmerged-nothing checkout of a finished task is task temporary",
               world.classes(snapshot, temporary.path), ["task_owned_temporary"])
        expect("a delivery contained by the target is landed residue",
               world.classes(snapshot, merged.path), ["landed_identical_residue"])
        expect("dirty bytes identical to the target are landed residue",
               world.classes(snapshot, identical.path), ["landed_identical_residue"])
        expect("an untracked file the target lacks is genuinely unlanded",
               world.classes(snapshot, unlanded.path), ["genuinely_unlanded"])
        expect("staged-identical, modified-different and untracked-new content keeps every fact",
               world.classes(snapshot, mixed.path),
               ["landed_identical_residue", "genuinely_unlanded", "mixed_conflicted"])
        expect("a registered checkout whose directory is gone is prunable metadata",
               world.classes(snapshot, prunable.path), ["prunable_stale_metadata"])
        expect("a briefed task's checkout is in use", world.classes(snapshot, live.path), ["active_in_use"])
        check("a managed checkout with no task record is unknown evidence",
              world.classes(snapshot, orphan.path).contains("unknown_incomplete_evidence")
                && world.blockers(snapshot, orphan.path).contains("owner_unknown"))
        check("a checkout Clawdline did not create is unknown and refused as foreign",
              world.classes(snapshot, foreign).contains("unknown_incomplete_evidence")
                && world.blockers(snapshot, foreign).contains("foreign_worktree"))
        let status = world.row(snapshot, mixed.path)["status"] as? [String: Any] ?? [:]
        check("staged, modified and untracked are separate counts",
              status["complete"] as? Bool == true && status["staged"] as? Int == 1
                && status["modified"] as? Int == 1 && status["untracked"] as? Int == 1, "\(status)")
        let missing = world.row(snapshot, prunable.path)
        check("an unreadable row's counts are null, never zero",
              (missing["status"] as? [String: Any])?["staged"] is NSNull
                && (snapshot["counts"] as? [String: Any])?["staged"] is NSNull
                && (snapshot["counts"] as? [String: Any])?["rows"] as? Int == 10)
        check("the frozen v1 snapshot and row key sets",
              Set(snapshot.keys) == ["schemaVersion", "project", "repository", "observedAt", "complete", "error",
                                     "rows", "truncated", "counts"]
                && Set(world.row(snapshot, merged.path).keys) == ["worktreeId", "path", "branch", "base", "head",
                    "target", "owner", "active", "status", "classifications", "localObservation",
                    "canonicalTargetObservation", "cleanup"]
                && Set((world.row(snapshot, merged.path)["owner"] as? [String: Any] ?? [:]).keys)
                    == ["taskId", "sessionId", "terminalId", "title", "evidence"]
                && Set((snapshot["counts"] as? [String: Any] ?? [:]).keys)
                    == ["rows", "active", "staged", "modified", "untracked", "unknown"])
        let owner = world.row(snapshot, merged.path)["owner"] as? [String: Any] ?? [:]
        let recorded = world.tasks.first { $0.id == merged.id }
        check("owner carries the conversation UUID and the separate terminal id",
              owner["sessionId"] as? String == recorded?.childSessionId?.lowercased()
                && owner["terminalId"] as? String == recorded?.childTerminalId
                && owner["evidence"] as? String == "exact_task_worktree_record")
        check("residue and temporary rows are eligible; unlanded, live and unknown are not",
              (world.row(snapshot, temporary.path)["cleanup"] as? [String: Any])?["eligible"] as? Bool == true
                && (world.row(snapshot, merged.path)["cleanup"] as? [String: Any])?["eligible"] as? Bool == true
                && world.blockers(snapshot, unlanded.path).contains("unlanded_work")
                && world.blockers(snapshot, live.path).contains("worktree_in_use")
                && world.blockers(snapshot, mixed.path).contains("mixed_or_conflicted"))
        check("the canonical target of a local-only repository is its local branch, observed now",
              (world.row(snapshot, merged.path)["canonicalTargetObservation"] as? [String: Any])?["ref"] as? String
                == "refs/heads/main")
    }

    group("worktree lifecycle keeps unknown liveness, registry and stale canonical evidence out of cleanup") {
        let world = WorktreeWorld()
        let residue = world.addTask(.success)
        world.write("feature.txt", "delivered\n", in: residue.path)
        world.commit("delivery", in: residue.path)
        world.git(["merge", "-q", "--ff-only", "clawdline/task/\(residue.id)"], world.repository)

        world.live = .init(complete: false, observedAt: nil, sessions: [])
        var snapshot = world.refresh()
        check("an incomplete Session inventory makes activity unknown rather than idle",
              world.row(snapshot, residue.path)["active"] is NSNull
                && world.classes(snapshot, residue.path).contains("unknown_incomplete_evidence")
                && world.blockers(snapshot, residue.path).contains("live_inventory_incomplete")
                && (snapshot["counts"] as? [String: Any])?["active"] is NSNull
                && snapshot["complete"] as? Bool == false)
        world.live = .init(complete: true, observedAt: Date(),
                           sessions: [.init(terminalID: "%7", conversationID: nil,
                                            cwd: residue.path.appendingPathComponent("sub").path)])
        world.clock = world.clock.addingTimeInterval(10)
        snapshot = world.refresh()
        check("a live Session inside the checkout makes it active",
              world.classes(snapshot, residue.path).contains("active_in_use"))
        world.live = .init(complete: true, observedAt: Date(), sessions: [])
        world.authoritative = false
        world.clock = world.clock.addingTimeInterval(10)
        snapshot = world.refresh()
        check("a non-authoritative registry never proves ownership",
              world.blockers(snapshot, residue.path).contains("task_registry_unavailable"))
        world.authoritative = true

        let remote = world.root.appendingPathComponent("remote.git", isDirectory: true)
        world.git(["init", "-q", "--bare", remote.path], world.root)
        world.git(["remote", "add", "origin", remote.path], world.repository)
        world.git(["push", "-q", "origin", "main"], world.repository)
        world.git(["fetch", "-q", "origin"], world.repository)
        let fetchHead = world.repository.appendingPathComponent(".git/FETCH_HEAD").path
        try? FileManager.default.setAttributes([.modificationDate: world.clock.addingTimeInterval(-7 * 3600)],
                                               ofItemAtPath: fetchHead)
        world.clock = world.clock.addingTimeInterval(10)
        snapshot = world.refresh()
        let canonical = world.row(snapshot, residue.path)["canonicalTargetObservation"] as? [String: Any] ?? [:]
        check("a canonical remote fetched hours ago is stale and blocks residue cleanup",
              canonical["state"] as? String == "stale" && canonical["ref"] as? String == "refs/remotes/origin/main"
                && world.blockers(snapshot, residue.path).contains("canonical_target_stale"), "\(canonical)")
        try? FileManager.default.setAttributes([.modificationDate: world.clock], ofItemAtPath: fetchHead)
        world.clock = world.clock.addingTimeInterval(10)
        snapshot = world.refresh()
        check("a fresh, published canonical remote lets the residue through",
              (world.row(snapshot, residue.path)["cleanup"] as? [String: Any])?["eligible"] as? Bool == true,
              "\(world.blockers(snapshot, residue.path))")
    }

    group("worktree cleanup preview pins its evidence and apply refuses every changed pin") {
        let world = WorktreeWorld()
        let temporary = world.addTask(.success)
        _ = world.refresh()
        func preview() -> (id: String, pin: String) {
            guard case .success(let value) = world.service.preview(projectID: world.projectID, worktreeIDs: nil) else {
                return ("", "")
            }
            return (value["previewId"] as? String ?? "", value["pinDigest"] as? String ?? "")
        }
        var plan = preview()
        let actions = { () -> [String] in
            guard case .success(let value) = world.service.preview(projectID: world.projectID, worktreeIDs: nil) else { return [] }
            return (value["actions"] as? [[String: Any]] ?? []).compactMap { $0["action"] as? String }
        }()
        expect("a temporary checkout previews removal and an exact-commit branch delete",
               actions, ["remove_checkout", "delete_branch"])
        let key = "key-" + UUID().uuidString.lowercased()
        expect("apply without confirmation is refused",
               refusalCode(world.service.apply(projectID: world.projectID, previewID: plan.id, pinDigest: plan.pin,
                                               confirm: false, idempotencyKey: key)), "confirmation_required")
        expect("a malformed idempotency key is refused",
               refusalCode(world.service.apply(projectID: world.projectID, previewID: plan.id, pinDigest: plan.pin,
                                               confirm: true, idempotencyKey: "short")), "bad_idempotency_key")
        expect("a pin digest from somewhere else is refused",
               refusalCode(world.service.apply(projectID: world.projectID, previewID: plan.id,
                                               pinDigest: String(repeating: "0", count: 64),
                                               confirm: true, idempotencyKey: key)), "pin_digest_mismatch")
        world.write("new.txt", "arrived after the preview\n", in: temporary.path)
        expect("new untracked content after the preview refuses the whole apply",
               refusalCode(world.service.apply(projectID: world.projectID, previewID: plan.id, pinDigest: plan.pin,
                                               confirm: true, idempotencyKey: key)), "status_changed")
        try? FileManager.default.removeItem(at: temporary.path.appendingPathComponent("new.txt"))
        check("the refused apply removed nothing", FileManager.default.fileExists(atPath: temporary.path.path))
        plan = preview()
        if let index = world.tasks.firstIndex(where: { $0.id == temporary.id }) { world.tasks[index].state = .briefed }
        expect("an owner that changed state after the preview refuses",
               refusalCode(world.service.apply(projectID: world.projectID, previewID: plan.id, pinDigest: plan.pin,
                                               confirm: true, idempotencyKey: key)), "owner_changed")
        if let index = world.tasks.firstIndex(where: { $0.id == temporary.id }) { world.tasks[index].state = .success }
        plan = preview()
        world.live = .init(complete: true, observedAt: Date(),
                           sessions: [.init(terminalID: "%3", conversationID: nil, cwd: temporary.path.path)])
        expect("a checkout that became live after the preview refuses",
               refusalCode(world.service.apply(projectID: world.projectID, previewID: plan.id, pinDigest: plan.pin,
                                               confirm: true, idempotencyKey: key)), "live_changed")
        world.live = .init(complete: true, observedAt: Date(), sessions: [])
        // Pin only the task row: moving the target also moves the main checkout's HEAD, which is a
        // worktree change of its own and is refused as `worktree_changed` when the main row is pinned.
        let temporaryID = ProjectWorktreeLifecycleService.worktreeID(
            commonDirectory: OrchestratorDraft.gitCommonDirectory(at: world.repository.path) ?? "",
            path: temporary.path.path)
        if case .success(let value) = world.service.preview(projectID: world.projectID, worktreeIDs: [temporaryID]) {
            plan = (value["previewId"] as? String ?? "", value["pinDigest"] as? String ?? "")
        }
        world.write("README.md", "target moved\n", in: world.repository)
        world.commit("move target", in: world.repository)
        expect("a target that moved after the preview refuses",
               refusalCode(world.service.apply(projectID: world.projectID, previewID: plan.id, pinDigest: plan.pin,
                                               confirm: true, idempotencyKey: key)), "comparison_changed")
        plan = preview()
        world.clock = world.clock.addingTimeInterval(ProjectWorktreeLifecycleService.previewLifetime + 1)
        expect("an expired preview refuses",
               refusalCode(world.service.apply(projectID: world.projectID, previewID: plan.id, pinDigest: plan.pin,
                                               confirm: true, idempotencyKey: key)), "preview_expired")
        expect("a preview of an id that is not registered refuses rather than guessing",
               refusalCode(world.service.preview(projectID: world.projectID,
                                                 worktreeIDs: ["wt-000000000000000000000000"])), "worktree_not_found")
    }

    group("worktree cleanup rechecks exact destructive targets immediately before each effect") {
        do {
            let world = WorktreeWorld()
            let residue = world.addTask(.success)
            world.write("README.md", "landed\n", in: world.repository)
            world.commit("landing", in: world.repository)
            world.write("README.md", "landed\n", in: residue.path)
            guard case .success(let preview) = world.service.preview(
                projectID: world.projectID, worktreeIDs: nil
            ) else {
                check("the same-path byte race preview is produced", false); return
            }
            world.beforeApplyAction = { kind, _ in
                guard kind == .removeCheckout else { return }
                world.beforeApplyAction = nil
                world.write("README.md", "changed after preservation\n", in: residue.path)
            }
            let applied = world.service.apply(
                projectID: world.projectID,
                previewID: preview["previewId"] as? String ?? "",
                pinDigest: preview["pinDigest"] as? String ?? "",
                confirm: true,
                idempotencyKey: "same-path-" + UUID().uuidString.lowercased())
            check("changing bytes under the same dirty path refuses checkout removal",
                  actionErrorCodes(applied).contains("status_changed_during_apply")
                    && FileManager.default.fileExists(atPath: residue.path.path))
        }

        do {
            let world = WorktreeWorld()
            let temporary = world.addTask(.success)
            let branch = "refs/heads/clawdline/task/\(temporary.id)"
            guard case .success(let preview) = world.service.preview(
                projectID: world.projectID, worktreeIDs: nil
            ) else {
                check("the target-ref race preview is produced", false); return
            }
            world.beforeApplyAction = { kind, _ in
                guard kind == .deleteBranch else { return }
                world.beforeApplyAction = nil
                world.write("target-moved.txt", "new target\n", in: world.repository)
                world.commit("move target after checkout removal", in: world.repository)
            }
            let applied = world.service.apply(
                projectID: world.projectID,
                previewID: preview["previewId"] as? String ?? "",
                pinDigest: preview["pinDigest"] as? String ?? "",
                confirm: true,
                idempotencyKey: "target-ref-" + UUID().uuidString.lowercased())
            check("rewriting the target immediately before branch deletion keeps the last ref",
                  actionErrorCodes(applied).contains("comparison_changed_during_apply")
                    && world.git(["rev-parse", "--verify", "--quiet", branch], world.repository).status == 0)
        }

        do {
            let world = WorktreeWorld()
            let first = world.addTask(.cancelled)
            let second = world.addTask(.cancelled)
            let firstGitFile = (try? Data(contentsOf: first.path.appendingPathComponent(".git"))) ?? Data()
            try? FileManager.default.removeItem(at: first.path)
            let firstID = ProjectWorktreeLifecycleService.worktreeID(
                commonDirectory: OrchestratorDraft.gitCommonDirectory(at: world.repository.path) ?? "",
                path: first.path.path)
            guard case .success(let preview) = world.service.preview(
                projectID: world.projectID, worktreeIDs: [firstID]
            ) else {
                check("the prune-set substitution preview is produced", false); return
            }
            world.beforeApplyAction = { kind, _ in
                guard kind == .pruneMetadata else { return }
                world.beforeApplyAction = nil
                try? FileManager.default.createDirectory(at: first.path, withIntermediateDirectories: true)
                try? firstGitFile.write(to: first.path.appendingPathComponent(".git"))
                try? FileManager.default.removeItem(at: second.path)
            }
            let applied = world.service.apply(
                projectID: world.projectID,
                previewID: preview["previewId"] as? String ?? "",
                pinDigest: preview["pinDigest"] as? String ?? "",
                confirm: true,
                idempotencyKey: "prune-set-" + UUID().uuidString.lowercased())
            check("equal-cardinality prune substitution cannot remove an unpinned record",
                  actionErrorCodes(applied).contains("prune_set_changed")
                    && world.git(["worktree", "list", "--porcelain"], world.repository).output
                        .contains(second.path.path))
        }

        do {
            let world = WorktreeWorld()
            let pinned = world.addTask(.cancelled)
            let hidden = world.addTask(.cancelled)
            let hiddenGit = (try? String(contentsOf: hidden.path.appendingPathComponent(".git"),
                                         encoding: .utf8)) ?? ""
            let hiddenAdmin = hiddenGit.replacingOccurrences(of: "gitdir: ", with: "")
                .trimmingCharacters(in: .whitespacesAndNewlines)
            try? FileManager.default.removeItem(atPath: hiddenAdmin + "/gitdir")
            try? FileManager.default.removeItem(at: pinned.path)
            let pinnedID = ProjectWorktreeLifecycleService.worktreeID(
                commonDirectory: OrchestratorDraft.gitCommonDirectory(at: world.repository.path) ?? "",
                path: pinned.path.path)
            guard case .success(let preview) = world.service.preview(
                projectID: world.projectID, worktreeIDs: [pinnedID]
            ) else {
                check("the exact stale-record preview is produced", false); return
            }
            let applied = world.service.apply(
                projectID: world.projectID,
                previewID: preview["previewId"] as? String ?? "",
                pinDigest: preview["pinDigest"] as? String ?? "", confirm: true,
                idempotencyKey: "exact-prune-" + UUID().uuidString.lowercased())
            check("removing one pinned stale record never prunes an unlisted admin record",
                  actionErrorCodes(applied).isEmpty
                    && FileManager.default.fileExists(atPath: hiddenAdmin)
                    && FileManager.default.fileExists(atPath: hidden.path.path))
        }
    }

    group("worktree cleanup preserves verifiable bytes first, stops on failed preservation, and replays") {
        do {
            let nestedWorld = WorktreeWorld()
            let checkout = nestedWorld.addTask(.success)
            let nested = checkout.path.appendingPathComponent("nested", isDirectory: true)
            try? FileManager.default.createDirectory(at: nested, withIntermediateDirectories: true)
            nestedWorld.git(["init", "-q"], nested)
            nestedWorld.write("inner.txt", "not captured by a gitlink patch\n", in: nested)
            let snapshot = nestedWorld.refresh()
            check("an untracked nested repository is never offered verified preservation",
                  nestedWorld.blockers(snapshot, checkout.path).contains("unsupported_dirty_entry")
                    && ((nestedWorld.row(snapshot, checkout.path)["cleanup"] as? [String: Any])?["eligible"] as? Bool) == false)
        }
        do {
            let ignoredWorld = WorktreeWorld()
            let checkout = ignoredWorld.addTask(.success)
            ignoredWorld.write(".gitignore", ".env\n", in: checkout.path)
            ignoredWorld.write(".env", "secret\n", in: checkout.path)
            let snapshot = ignoredWorld.refresh()
            check("ignored bytes that were not pinned block checkout removal",
                  ignoredWorld.blockers(snapshot, checkout.path).contains("ignored_entries_present"))
        }
        let world = WorktreeWorld()
        let identical = world.addTask(.success)
        let unlanded = world.addTask(.success)
        world.write("README.md", "landed\n", in: world.repository)
        world.commit("landing", in: world.repository)
        world.write("README.md", "landed\n", in: identical.path)
        world.write("notes/draft.txt", "keep me\n", in: unlanded.path)
        world.write("README.md", "unlanded edit\n", in: unlanded.path)
        _ = world.refresh()
        guard case .success(let first) = world.service.preview(projectID: world.projectID, worktreeIDs: nil) else {
            check("the preservation preview is produced", false); return
        }
        let actions = (first["actions"] as? [[String: Any]] ?? []).map {
            ($0["worktreeId"] as? String ?? "", $0["action"] as? String ?? "")
        }
        let unlandedID = ProjectWorktreeLifecycleService.worktreeID(
            commonDirectory: OrchestratorDraft.gitCommonDirectory(at: world.repository.path) ?? "",
            path: unlanded.path.path)
        check("unlanded bytes are offered preservation only, and residue is preserved before removal",
              actions.filter { $0.0 == unlandedID }.map(\.1) == ["preserve_patch"]
                && actions.filter { $0.0 != unlandedID }.map(\.1) == ["preserve_patch", "remove_checkout", "delete_branch"],
              "\(actions)")
        let recovery = (first["actions"] as? [[String: Any]] ?? []).first?["recovery"] as? [String: Any] ?? [:]
        check("every preview names its expiry and a recovery method, artifact, base and digest",
              first["expiresAt"] is String && recovery["method"] as? String == "git_binary_patch"
                && recovery["artifact"] is String && recovery["base"] is String && recovery["digest"] is String)

        try? FileManager.default.createDirectory(at: world.state, withIntermediateDirectories: true)
        try? Data("not a directory".utf8).write(to: world.state.appendingPathComponent("preserved"))
        let failedKey = "failed-" + UUID().uuidString.lowercased()
        let failed = world.service.apply(projectID: world.projectID, previewID: first["previewId"] as? String ?? "",
                                         pinDigest: first["pinDigest"] as? String ?? "", confirm: true,
                                         idempotencyKey: failedKey)
        expect("preservation that cannot write refuses as preservation_failed", refusalCode(failed), "preservation_failed")
        expect("a failed preservation replays with the same failure status",
               refusalCode(world.service.apply(projectID: world.projectID,
                                                previewID: first["previewId"] as? String ?? "",
                                                pinDigest: first["pinDigest"] as? String ?? "",
                                                confirm: true, idempotencyKey: failedKey)),
               "preservation_failed")
        check("a failed preservation removed no checkout and no bytes",
              FileManager.default.fileExists(atPath: identical.path.path)
                && FileManager.default.fileExists(atPath: unlanded.path.appendingPathComponent("notes/draft.txt").path)
                && world.git(["rev-parse", "--verify", "clawdline/task/\(identical.id)"], world.repository).status == 0)
        try? FileManager.default.removeItem(at: world.state.appendingPathComponent("preserved"))

        world.clock = world.clock.addingTimeInterval(10)
        guard case .success(let second) = world.service.preview(projectID: world.projectID, worktreeIDs: nil) else {
            check("the second preview is produced", false); return
        }
        let key = "apply-" + UUID().uuidString.lowercased()
        let previewID = second["previewId"] as? String ?? ""
        let pin = second["pinDigest"] as? String ?? ""
        guard case .success(let receipt) = world.service.apply(projectID: world.projectID, previewID: previewID,
                                                                pinDigest: pin, confirm: true, idempotencyKey: key) else {
            check("the confirmed apply succeeds", false); return
        }
        expect("the confirmed apply records applied", receipt["state"] as? String, "applied")
        check("residue was removed through git and its exact-commit branch deleted",
              !FileManager.default.fileExists(atPath: identical.path.path)
                && world.git(["rev-parse", "--verify", "--quiet", "refs/heads/clawdline/task/\(identical.id)"],
                             world.repository).status != 0)
        check("unlanded bytes stay where they were", FileManager.default.fileExists(
            atPath: unlanded.path.appendingPathComponent("notes/draft.txt").path))
        let preserved = (receipt["actions"] as? [[String: Any]] ?? [])
            .first { $0["worktreeId"] as? String == unlandedID }?["recovery"] as? [String: Any] ?? [:]
        let patch = preserved["artifact"] as? String ?? ""
        let restore = world.root.appendingPathComponent("restore", isDirectory: true)
        world.git(["worktree", "add", "-q", "--detach", restore.path, preserved["base"] as? String ?? "-"], world.repository)
        let applied = world.git(["apply", "--binary", patch], restore)
        check("the preserved patch reapplies to its named base with the exact bytes",
              preserved["verified"] as? Bool == true && applied.status == 0
                && (try? String(contentsOf: restore.appendingPathComponent("notes/draft.txt"), encoding: .utf8)) == "keep me\n"
                && (try? String(contentsOf: restore.appendingPathComponent("README.md"), encoding: .utf8)) == "unlanded edit\n",
              applied.output)
        let manifestData = (try? Data(contentsOf: URL(fileURLWithPath: patch).deletingLastPathComponent()
            .appendingPathComponent("manifest.json"))) ?? Data()
        let manifest = (try? JSONSerialization.jsonObject(with: manifestData)) as? [String: Any] ?? [:]
        let patchDigest = SHA256.hash(data: (try? Data(contentsOf: URL(fileURLWithPath: patch))) ?? Data())
            .map { String(format: "%02x", $0) }.joined()
        check("the manifest records the patch digest it can be checked against",
              manifest["patchSha256"] as? String == patchDigest && manifest["base"] as? String == preserved["base"] as? String)

        guard case .success(let replay) = world.service.apply(projectID: world.projectID, previewID: previewID,
                                                               pinDigest: pin, confirm: true, idempotencyKey: key) else {
            check("a repeated apply replays", false); return
        }
        check("a repeated apply returns the same receipt without repeating work",
              replay["receiptId"] as? String == receipt["receiptId"] as? String && replay["replayed"] as? Bool == true)
        guard case .success(let third) = world.service.preview(projectID: world.projectID, worktreeIDs: nil) else {
            check("a third preview is produced", false); return
        }
        expect("one idempotency key cannot settle a second preview",
               refusalCode(world.service.apply(projectID: world.projectID, previewID: third["previewId"] as? String ?? "",
                                               pinDigest: third["pinDigest"] as? String ?? "", confirm: true,
                                               idempotencyKey: key)), "idempotency_key_reused")
        let original = (try? Data(contentsOf: world.service.ledgerURL)) ?? Data()
        let unfinished = "unfinished-" + UUID().uuidString.lowercased()
        var ledger = (try? JSONSerialization.jsonObject(with: original)) as? [String: Any] ?? [:]
        var entries = ledger["entries"] as? [String: Any] ?? [:]
        entries[unfinished] = ["previewId": third["previewId"] as? String ?? "", "pinDigest": third["pinDigest"] as? String ?? "",
                               "state": "started", "at": world.clock.timeIntervalSince1970, "receipt": NSNull()]
        ledger["entries"] = entries
        try? JSONSerialization.data(withJSONObject: ledger).write(to: world.service.ledgerURL)
        expect("an apply that started and never recorded its outcome refuses as unknown",
               refusalCode(world.service.apply(projectID: world.projectID, previewID: third["previewId"] as? String ?? "",
                                               pinDigest: third["pinDigest"] as? String ?? "", confirm: true,
                                               idempotencyKey: unfinished)), "apply_outcome_unknown")
        try? Data("{broken".utf8).write(to: world.service.ledgerURL)
        expect("an unreadable ledger refuses and is left in place",
               refusalCode(world.service.apply(projectID: world.projectID, previewID: third["previewId"] as? String ?? "",
                                               pinDigest: third["pinDigest"] as? String ?? "", confirm: true,
                                               idempotencyKey: "other-" + UUID().uuidString.lowercased())),
               "cleanup_ledger_unavailable")
        check("the unreadable ledger's bytes were not replaced",
              (try? Data(contentsOf: world.service.ledgerURL)) == Data("{broken".utf8))
        world.git(["worktree", "remove", "--force", restore.path], world.repository)
    }

    group("worktree lifecycle routes keep cleanup machine-only and Cloud reads settle the closed vocabulary") {
        let world = WorktreeWorld()
        _ = world.addTask(.success)
        ProjectWorktreeHTTP.configureServiceForTesting(world.service)
        defer { ProjectWorktreeHTTP.configureServiceForTesting(nil) }
        let id = world.projectID
        expect("the four routes parse, and nothing else does",
               ["/v1/projects/\(id)/worktrees", "/v1/projects/\(id)/worktrees/refresh",
                "/v1/projects/\(id)/worktrees/cleanup/preview", "/v1/projects/\(id)/worktrees/cleanup/apply",
                "/v1/projects/\(id)/worktrees/cleanup", "/v1/projects/\(id)", "/v1/projects"]
                .map { ProjectWorktreeHTTP.target($0)?.operation },
               [.read, .refresh, .preview, .apply, nil, nil, nil])
        func request(_ method: String, _ path: String, body: String = "") -> RemoteServer.Request {
            var made = RemoteServer.Request(head: Data("\(method) \(path) HTTP/1.1\r\n\r\n".utf8))!
            made.body = Data(body.utf8)
            made.contentLength = made.body.count
            return made
        }
        func respond(_ admission: ProjectWorktreeHTTP.Admission?) -> (Int, [String: Any]) {
            let response: RemoteServer.Response
            switch admission {
            case .response(let value)?: response = value
            case .work(let work)?: response = work()
            case nil: return (0, [:])
            }
            return (response.status, (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any] ?? [:])
        }
        let reader = RemoteAuth.Verdict.allowed(device: "phone", caps: [.read])
        let admin = RemoteAuth.Verdict.allowed(device: "laptop", caps: [.read, .send, .admin])
        let read = respond(ProjectWorktreeHTTP.admit(request("GET", "/v1/projects/\(id)/worktrees"),
                                                     machine: false, permission: reader))
        check("a paired reader gets the cached read model under its success wrapper",
              read.0 == 200 && read.1["projectWorktreeLifecycle"] is [String: Any])
        expect("an unauthenticated read is refused",
               respond(ProjectWorktreeHTTP.admit(request("GET", "/v1/projects/\(id)/worktrees"),
                                                 machine: false, permission: .denied)).0, 401)
        let refreshed = respond(ProjectWorktreeHTTP.admit(request("POST", "/v1/projects/\(id)/worktrees/refresh"),
                                                          machine: false, permission: reader))
        check("a paired reader's refresh is a bounded observation that answers with rows",
              refreshed.0 == 200 && ((refreshed.1["projectWorktreeLifecycle"] as? [String: Any])?["rows"] as? [Any])?.count == 2)
        let refused = respond(ProjectWorktreeHTTP.admit(request("POST", "/v1/projects/\(id)/worktrees/cleanup/preview", body: "{}"),
                                                        machine: false, permission: admin))
        check("even an administrative device cannot preview cleanup without the orchestrator token",
              refused.0 == 403 && (refused.1["error"] as? [String: Any])?["code"] as? String == "machine_token_required")
        let previewed = respond(ProjectWorktreeHTTP.admit(request("POST", "/v1/projects/\(id)/worktrees/cleanup/preview", body: "{}"),
                                                          machine: true, permission: .denied))
        check("the orchestrator token previews", previewed.0 == 200
                && (previewed.1["projectWorktreeCleanupPreview"] as? [String: Any])?["previewId"] is String)
        expect("apply takes exactly its four fields",
               respond(ProjectWorktreeHTTP.admit(request("POST", "/v1/projects/\(id)/worktrees/cleanup/apply",
                                                         body: #"{"preview_id":"x","confirm":true}"#),
                                                 machine: true, permission: .denied)).0, 400)
        expect("numeric one is not accepted as explicit confirmation",
               respond(ProjectWorktreeHTTP.admit(request("POST", "/v1/projects/\(id)/worktrees/cleanup/apply",
                                                         body: #"{"preview_id":"x","pin_digest":"0000000000000000000000000000000000000000000000000000000000000000","confirm":1,"idempotency_key":"0123456789abcdef"}"#),
                                                 machine: true, permission: .denied)).0, 400)
        expect("a preview naming a filesystem path instead of a worktree id is refused",
               (respond(ProjectWorktreeHTTP.admit(request("POST", "/v1/projects/\(id)/worktrees/cleanup/preview",
                                                          body: #"{"worktrees":["/tmp"]}"#),
                                                  machine: true, permission: .denied)).1["error"] as? [String: Any])?["code"]
                   as? String, "bad_worktree_ids")
        expect("query fields are refused rather than ignored",
               respond(ProjectWorktreeHTTP.admit(request("GET", "/v1/projects/\(id)/worktrees?path=/tmp"),
                                                 machine: false, permission: reader)).0, 400)

        let cloudPermission = RemoteAuth.Verdict.allowed(device: "cloud:viewer", caps: [.read, .send])
        let cloudRead = RemoteServer.Request(verifiedCloudRead: .projectWorktreeLifecycle(
            session: CloudAppBridge.machineReplySession, request: "r-1", project: id), sender: "viewer")
        let cloudRefresh = RemoteServer.Request(verifiedCloudRead: .projectWorktreeLifecycleRefresh(
            session: CloudAppBridge.machineReplySession, request: "r-2", project: id), sender: "viewer")
        check("the two Cloud read words become the local GET and read-admitted refresh",
              cloudRead.method == "GET" && cloudRead.path == "/v1/projects/\(id)/worktrees"
                && cloudRefresh.method == "POST" && cloudRefresh.path == "/v1/projects/\(id)/worktrees/refresh"
                && CloudHeadlessRead.projectWorktreeLifecycleRefresh(session: "s", request: "r-2", project: id).name == "read:r-2")
        expect("a verified Cloud refresh is admitted as a read",
               respond(ProjectWorktreeHTTP.admit(cloudRefresh, machine: false, permission: cloudPermission)).0, 200)
        var cloudPreview = cloudRefresh
        cloudPreview.path = "/v1/projects/\(id)/worktrees/cleanup/preview"
        expect("no Cloud request reaches cleanup, whatever it carries",
               respond(ProjectWorktreeHTTP.admit(cloudPreview, machine: true, permission: cloudPermission)).0, 403)
    }
}
