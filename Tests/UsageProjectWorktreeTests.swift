import Foundation

// **The Projects page's read, and the ladder behind it.** Cut out of `Tests/UsageLedgerTests.swift`
// when that file reached the 2,000-line stop-growth limit: these three groups are the whole of one
// route — the outcome ladder, the read-time join that answers for a Project, and the pair this read
// exists to keep apart — and they run in exactly the position they ran in before, between
// `runUsageLedgerTests()` and `runUsagePortfolioAndLifecycleTests()`, so the executed group order
// and its manifest are untouched by the move.
//
// `worktreeRow` and `acceptedFeature` came with them. They were declared inside
// `runUsageLedgerTests()` after the last group that could have used them, so nothing left behind
// can see that they have gone.

// A row as it actually lands in the ledger for a task that ran inside a Clawdline-managed
// worktree: `working_dir` is the checkout, `project_key` is the canonical repository, and the two
// state words are what the broker record said when the row was collected.
func worktreeRow(_ key: String, at: Date, worktree: String, task: String,
                 project: String? = "/private/acme/widget", state: String? = "success",
                 landing: String? = nil) -> UsageLedger.Row {
    var row = UsageLedger.Row()
    row.intervalKey = key
    row.assistant = "claude"
    row.sessionID = "private-session-\(key)"
    row.boundaryKind = "task"
    row.boundaryID = task
    row.taskID = task
    row.origin = "dispatch"
    row.projectKey = project
    row.workingDir = worktree.isEmpty
        ? project.map { $0 + "/checkout" }
        : "/Users/tester/Library/Application Support/Clawdline/worktrees/widget-9f1c/" + worktree
    row.isolation = worktree.isEmpty ? "none" : "worktree"
    row.taskState = state
    row.landingState = landing
    row.model = "claude-opus-5"
    row.counts = .init(inputNew: 1, output: 2, cacheRead: 3, cacheWrite: 4)
    row.total = row.counts.total
    row.costBasis = "unknown"
    row.missingReason = "no_cost_recorded"
    row.coverage = "complete"
    row.startedAt = at
    row.updatedAt = at.addingTimeInterval(30)
    row.localDay = UsageLedger.localDay(of: at)
    row.sealed = true
    return row
}

func acceptedFeature(_ id: String, _ label: String) -> UsageLedger.AcceptedAttribution {
    UsageLedger.AcceptedAttribution(id: id, label: label)
}

func runUsageProjectWorktreeTests() {
group("a worktree's outcome tells landed from delivered from debris") {
    let at = ISO8601DateFormatter().date(from: "2026-09-01T09:00:00Z")!
    func outcome(_ rows: [UsageLedger.Row], live: Set<String> = [],
                 branch: UsageProjectWorktreeService.LandingEvidence = .unknown) -> String {
        UsageProjectWorktreeService.outcome(rows, live: live, branch: branch).rawValue
    }
    func evidence(_ rows: [UsageLedger.Row],
                  branch: UsageProjectWorktreeService.LandingEvidence) -> String {
        UsageProjectWorktreeService.evidence(rows, branch: branch).rawValue
    }
    // A landing record outranks the child's own word about itself. Two rows on this Mac say
    // `failure` beside `landed`: the child reported failure and the root integrated the branch
    // anyway, and what a person is asking about is the branch.
    expect("a landing record makes it landed, whatever the task said",
           outcome([worktreeRow("landed", at: at, worktree: "w1", task: "t1",
                                state: "failure", landing: "landed")]), "landed")
    // b1103ab1's shape: it finished, nothing landed it, and it sat for 26 hours.
    expect("a task that succeeded with no landing is delivered",
           outcome([worktreeRow("spawning", at: at, worktree: "w1", task: "t1",
                                state: "spawning"),
                    worktreeRow("done", at: at, worktree: "w1", task: "t1", state: "success")]),
           "delivered")
    expect("an open landing obligation is still delivered, not landed",
           outcome([worktreeRow("pending", at: at, worktree: "w1", task: "t1",
                                state: "success", landing: "pending")]), "delivered")
    expect("and so is one that was given up",
           outcome([worktreeRow("given-up", at: at, worktree: "w1", task: "t1",
                                state: "success", landing: "abandoned")]), "delivered")
    // b57fc96f's shape, and the one a threshold would have got wrong: nothing terminal was ever
    // written, because the session died before anything could write it.
    let stalled = [worktreeRow("stalled", at: at, worktree: "w2", task: "t2", state: "briefed")]
    expect("a task stuck at briefed with nothing live is debris", outcome(stalled), "abandoned")
    expect("the same rows are active while that task is still running",
           outcome(stalled, live: ["t2"]), "active")
    expect("liveness is asked about this task, not about any task",
           outcome(stalled, live: ["t9"]), "abandoned")
    for state in ["failure", "timeout", "cancelled", "spawn_failed"] {
        expect("a \(state) task with nothing landed is debris",
               outcome([worktreeRow(state, at: at, worktree: "w3", task: "t3", state: state)]),
               "abandoned")
    }
    expect("a row carrying no state at all claims none of the four",
           outcome([worktreeRow("silent", at: at, worktree: "w4", task: "t4", state: nil)]),
           "unknown")

    // **The half of the ladder that asks git rather than asking whether anybody wrote it down.**
    // On 2026-09-05 this route called 53 of one repository's worktrees "delivered, not landed"
    // while git said 24 of those branches were already ancestors of HEAD and 13 were gone.
    let finished = [worktreeRow("finished", at: at, worktree: "w5", task: "t5", state: "success")]
    expect("a delivery whose branch git says is merged has landed, with nobody recording it",
           outcome(finished, branch: .branchMerged), "landed")
    expect("and says which of the two sources answered",
           evidence(finished, branch: .branchMerged), "branch_merged")
    // **A branch that is gone is not a landing**, and it was one for a day. The reason written
    // down was that `disposeWorktree` deletes a delivery branch exactly when it carries none —
    // true of this app, and this app is not the only thing that deletes branches: eight it kept
    // *because* they carried commits, three of them holding 1, 63 and 122, are gone from that
    // repository with no removal recorded anywhere.
    expect("a delivery whose branch git can no longer find is its own answer, not a landing",
           outcome(finished, branch: .branchAbsent), "branch_gone")
    expect("named as the branch's absence and never as a record",
           evidence(finished, branch: .branchAbsent), "branch_absent")
    check("and it is not the delivered rung either, because there is no branch left to land",
          UsageProjectWorktreeService.Outcome.branchGone
            != UsageProjectWorktreeService.Outcome.delivered
            && UsageProjectWorktreeService.Outcome.branchGone.rawValue == "branch_gone")
    expect("a branch that is there and unmerged is the one delivered with a fact behind it",
           outcome(finished, branch: .branchUnmerged), "delivered")
    expect("and that fact travels beside it", evidence(finished, branch: .branchUnmerged),
           "branch_unmerged")
    // The direction that costs a day if it is wrong: git not answering must never read as
    // settled. This is the same fail-safe `workVisibility` takes with deletion.
    expect("git failing to answer leaves the verdict exactly where it was",
           outcome(finished, branch: .unknown), "delivered")
    expect("and says so rather than implying nothing landed it",
           evidence(finished, branch: .unknown), "unknown")
    // **A deleted branch settles only work that succeeded.** `disposeWorktree` deletes a delivery
    // branch exactly when it carries no commits, so on a failed task absence means the checkout
    // was thrown away empty — debris, and not a landing.
    let failed = [worktreeRow("failed", at: at, worktree: "w6", task: "t6", state: "failure")]
    expect("a failed task whose branch is gone is still debris",
           outcome(failed, branch: .branchAbsent), "abandoned")
    expect("even though the branch fact beside it is exactly the same word",
           evidence(failed, branch: .branchAbsent), "branch_absent")
    expect("while a failed task whose branch is merged landed, for the reason a record does",
           outcome(failed, branch: .branchMerged), "landed")
    // **A root that gave the landing obligation up made a decision, and git's shape does not
    // overrule a decision.** Zero worktrees on this Mac are in this state today, which is luck
    // rather than a design: `abandoned` is not a landing record, so the two git rungs used to
    // reach straight past it.
    let givenUpDebris = [worktreeRow("given-up-debris", at: at, worktree: "w8", task: "t8",
                                     state: "failure", landing: "abandoned")]
    expect("an abandoned obligation stays abandoned when git says the branch is in HEAD",
           outcome(givenUpDebris, branch: .branchMerged), "abandoned")
    expect("and when git says the branch is gone",
           outcome(givenUpDebris, branch: .branchAbsent), "abandoned")
    expect("while the same rows without that record are landed by the merged branch",
           outcome([worktreeRow("no-landing", at: at, worktree: "w8", task: "t8",
                                state: "failure")], branch: .branchMerged), "landed")
    // The rungs *below* the two it guards are untouched: work that succeeded with the obligation
    // given up is still "done, nothing merged it", which is what this screen said before git was
    // a source and what `landingStates` carries the word `abandoned` beside.
    expect("a given-up obligation on work that succeeded is still delivered",
           outcome([worktreeRow("given-up", at: at, worktree: "w8", task: "t8",
                                state: "success", landing: "abandoned")],
                   branch: .branchMerged), "delivered")
    expect("and the branch fact still travels beside it rather than being hidden",
           evidence([worktreeRow("given-up", at: at, worktree: "w8", task: "t8",
                                 state: "success", landing: "abandoned")],
                    branch: .branchMerged), "branch_merged")

    let recorded = [worktreeRow("recorded", at: at, worktree: "w7", task: "t7",
                                state: "success", landing: "landed")]
    expect("a root's record outranks a branch this side merely found unmerged",
           outcome(recorded, branch: .branchUnmerged), "landed")
    expect("and is reported as the record it is",
           evidence(recorded, branch: .branchUnmerged), "record")

    // The branch name is a convention, and this is the only place that turns a worktree id into
    // one. A repository git never answered for says nothing about any of its branches.
    let known = Orchestrator.RepositoryBranches(
        heads: ["clawdline/task/11111111-2222-4333-8444-555555555555": "d00dfeed",
                "clawdline/task/22222222-2222-4333-8444-555555555555": "cafed00d",
                "clawdline/task/44444444-2222-4333-8444-555555555555": "b00c0ffe",
                "clawdline/task/55555555-2222-4333-8444-555555555555": "feedface"],
        merged: ["clawdline/task/22222222-2222-4333-8444-555555555555",
                 "clawdline/task/44444444-2222-4333-8444-555555555555",
                 "clawdline/task/55555555-2222-4333-8444-555555555555"], known: true)
    // What the registry says each branch was cut from. `2222` moved off its base and `4444` never
    // did; `5555` is a merged branch whose task record has been swept, so nothing can say.
    let bases = ["clawdline/task/22222222-2222-4333-8444-555555555555": "0ddba5e",
                 "clawdline/task/44444444-2222-4333-8444-555555555555": "b00c0ffe"]
    func branchFact(_ id: String, _ branches: Orchestrator.RepositoryBranches,
                    _ cutFrom: [String: String] = bases) -> String {
        UsageProjectWorktreeService.branchEvidence(worktree: id, branches: branches,
                                                   bases: cutFrom).rawValue
    }
    expect("a listed branch HEAD contains, with commits of its own, is merged",
           branchFact("22222222-2222-4333-8444-555555555555", known), "branch_merged")
    expect("a listed branch it does not contain is unmerged",
           branchFact("11111111-2222-4333-8444-555555555555", known), "branch_unmerged")
    expect("a branch the listing has no line for is gone",
           branchFact("33333333-2222-4333-8444-555555555555", known), "branch_absent")
    expect("a repository git could not read says nothing about any of them",
           branchFact("22222222-2222-4333-8444-555555555555",
                      Orchestrator.RepositoryBranches()), "unknown")
    // *Absent* is a fact about the repository; *unknown* is a fact about this side. A worktree id
    // that builds no branch name is the second, and reading it as the first would settle a
    // delivery nobody ever asked about.
    expect("an id no branch name can be built from is unknown, not absent",
           branchFact("not-a-task-id", known), "unknown")

    // **`git worktree add -b <branch> <path> <base>` makes every delivery branch an ancestor of
    // HEAD on the day it is created**, so `--merged HEAD` lists one that has never received a
    // commit exactly as it lists a landing. On 2026-09-06 that was 12 of this Mac's 75 contained
    // delivery branches, 10 with an uncommitted checkout still on disk — the ordinary shape of a
    // Codex worktree child, which is told to leave its bytes dirty for the root.
    expect("a merged branch still pointing at what it was cut from carries no delivery",
           branchFact("44444444-2222-4333-8444-555555555555", known), "branch_empty")
    expect("and that is not a landing",
           outcome(finished, branch: .branchEmpty), "delivered")
    // The base is the registry's to remember and the registry is swept. Refusing the upgrade is
    // the direction that costs a glance; granting it costs somebody a day of rebuilding.
    expect("a merged branch nothing can say the base of is not upgraded either",
           branchFact("55555555-2222-4333-8444-555555555555", known), "branch_base_unknown")
    expect("and it stays delivered rather than borrowing the merged rung",
           outcome(finished, branch: .branchBaseUnknown), "delivered")
    expect("a merged branch with no bases known at all is the same refusal, not a merge",
           branchFact("22222222-2222-4333-8444-555555555555", known, [:]), "branch_base_unknown")
    check("neither of the two is spelled as the merge it is not",
          UsageProjectWorktreeService.LandingEvidence.branchEmpty.rawValue == "branch_empty"
            && UsageProjectWorktreeService.LandingEvidence.branchBaseUnknown.rawValue
                == "branch_base_unknown")

    check("and the ladder is ordered strongest first",
          UsageProjectWorktreeService.Outcome.landed.rank
            < UsageProjectWorktreeService.Outcome.delivered.rank
            && UsageProjectWorktreeService.Outcome.delivered.rank
                < UsageProjectWorktreeService.Outcome.branchGone.rank
            && UsageProjectWorktreeService.Outcome.branchGone.rank
                < UsageProjectWorktreeService.Outcome.active.rank
            && UsageProjectWorktreeService.Outcome.active.rank
                < UsageProjectWorktreeService.Outcome.abandoned.rank)
}

group("a Project's worktrees are joined at read time and named by the Portfolio's own rule") {
    let at = ISO8601DateFormatter().date(from: "2026-09-02T10:00:00Z")!
    let repository = "/private/acme/widget"
    let alpha = "11111111-2222-4333-8444-555555555555"
    let beta = "66666666-7777-4888-8999-aaaaaaaaaaaa"
    let quiet = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
    let managed = "/Users/tester/Library/Application Support/Clawdline/worktrees/widget-9f1c/"
    let rows = [
        // One worktree, two tasks: the owning child and a grandchild dispatched from inside it.
        worktreeRow("alpha-own", at: at, worktree: alpha, task: alpha, landing: "landed"),
        worktreeRow("alpha-grandchild", at: at.addingTimeInterval(600), worktree: alpha,
                    task: "grandchild-of-alpha"),
        worktreeRow("beta-own", at: at.addingTimeInterval(1_200), worktree: beta, task: beta,
                    state: "briefed"),
        // A worktree with rows and no accepted Feature: counted, never listed. This is most of
        // the 58 checkouts on the real Mac, and the reason this read is not `git worktree list`.
        worktreeRow("quiet", at: at.addingTimeInterval(1_800), worktree: quiet, task: quiet),
        // Work in the shared checkout: this Project's, and no worktree's.
        worktreeRow("shared", at: at.addingTimeInterval(2_400), worktree: "", task: "shared-task"),
        // A row recorded before canonical Project keys landed: its own project_key is a managed
        // worktree, so it resolves to no Project and may appear under none.
        worktreeRow("legacy", at: at.addingTimeInterval(3_000), worktree: quiet,
                    task: "legacy-task", project: managed + quiet),
    ]
    let features = ["alpha-own": acceptedFeature("feature-a", "The landing queue"),
                    "alpha-grandchild": acceptedFeature("feature-b", "Its focused runner"),
                    "beta-own": acceptedFeature("feature-a", "The landing queue"),
                    "shared": acceptedFeature("feature-a", "The landing queue"),
                    "legacy": acceptedFeature("feature-c", "Something with no Project")]
    func read(_ project: String, live: Set<String> = []) -> UsageProjectWorktreeService.Answer {
        UsageProjectWorktreeService(rows: { rows }, acceptedFeatures: { features },
                                    liveTaskIDs: { live })
            .read(.init(project: project, timezoneID: "UTC"), now: at)
    }
    let byName = read("widget").payload ?? [:]
    let project = byName["project"] as? [String: Any] ?? [:]
    expect("the Project is the one the Portfolio names", project["label"] as? String, "widget")
    expect("under the id the Portfolio computes", project["id"] as? String,
           UsageQueryService.projectID(repository))
    let byID = read(UsageQueryService.projectID(repository)).payload?["project"]
        as? [String: Any]
    check("and asking by that id is the same answer",
          byID?["id"] as? String == project["id"] as? String)
    let byPath = read(repository).payload?["project"] as? [String: Any]
    check("as is asking by its absolute canonical path",
          byPath?["id"] as? String == project["id"] as? String)

    let worktrees = byName["worktrees"] as? [[String: Any]] ?? []
    expect("only the worktrees that finished a Feature are listed", worktrees.count, 2)
    expect("newest first", worktrees.first?["id"] as? String, beta)
    let listed = Set(worktrees.compactMap { $0["id"] as? String })
    check("the quiet worktree and the shared checkout are not among them",
          !listed.contains(quiet) && listed == Set([alpha, beta]))
    let alphaRow = worktrees.first { $0["id"] as? String == alpha } ?? [:]
    expect("a worktree that hosted a grandchild is still one worktree",
           (alphaRow["tasks"] as? [String])?.count, 2)
    expect("with both Features it carried", (alphaRow["features"] as? [[String: Any]])?.count, 2)
    expect("and the landing that reached the tree", alphaRow["outcome"] as? String, "landed")
    let alphaFeatures = alphaRow["features"] as? [[String: Any]] ?? []
    expect("each Feature answers for its own rows",
           alphaFeatures.first { $0["id"] as? String == "feature-b" }?["outcome"] as? String,
           "delivered")
    expect("under the label the accepted head carries",
           alphaFeatures.first { $0["id"] as? String == "feature-b" }?["label"] as? String,
           "Its focused runner")
    let betaRow = worktrees.first { $0["id"] as? String == beta } ?? [:]
    expect("a worktree whose only task stalled is debris", betaRow["outcome"] as? String,
           "abandoned")
    let whileLive = (read("widget", live: [beta]).payload?["worktrees"]
        as? [[String: Any]]) ?? []
    expect("and the same worktree is active while that task runs",
           whileLive.first { $0["id"] as? String == beta }?["outcome"] as? String, "active")

    let excluded = byName["excluded"] as? [String: Any] ?? [:]
    expect("the worktrees with no nameable Feature are counted",
           excluded["worktreesWithoutFeature"] as? Int, 1)
    expect("with the reason the Portfolio uses for the same absence",
           excluded["reason"] as? String, "no_unambiguous_accepted_head")
    let unattributed = byName["unattributed"] as? [String: Any] ?? [:]
    expect("a worktree belonging to no Project is reported rather than dropped",
           unattributed["worktrees"] as? Int, 1)
    expect("with the refusal that produced it",
           (unattributed["reasons"] as? [String: Int])?["legacy_managed_worktree_project_key"], 1)
    let receipt = byName["read"] as? [String: Any] ?? [:]
    expect("the receipt says how much was read", receipt["rows"] as? Int, rows.count)
    expect("how much of it was this Project's", receipt["projectRows"] as? Int, 5)
    expect("how much of that ran in a worktree", receipt["worktreeRows"] as? Int, 4)
    expect("and how much of that carried a Feature", receipt["featureRows"] as? Int, 3)
    expect("a complete scan is not partial", byName["status"] as? String, "available")
    expect("and the rule it answered by names both of the sources it may use",
           byName["outcomeRule"] as? String,
           "landed_by_record_or_nonempty_merged_branch_then_branch_gone_then_delivered_then_"
             + "live_then_abandoned")
    expect("with no branch fact, every verdict rests on the stored columns and says so",
           alphaRow["landingEvidence"] as? String, "record")
    expect("including a Feature whose own rows carry no landing at all",
           alphaFeatures.first { $0["id"] as? String == "feature-b" }?["landingEvidence"] as? String,
           "unknown")

    // **The same rows, once the repository is allowed to answer.** `beta`'s branch is there and
    // unmerged; `alpha`'s is not there at all.
    var asked: [String] = []
    func withGit(_ branches: Orchestrator.RepositoryBranches,
                 bases: [String: String] = [:]) -> [String: Any] {
        UsageProjectWorktreeService(
            rows: { rows }, acceptedFeatures: { features },
            branches: { repository in asked.append(repository); return branches },
            worktreeBases: { bases })
            .read(.init(project: "widget", timezoneID: "UTC"), now: at).payload ?? [:]
    }
    let gitAnswered = withGit(Orchestrator.RepositoryBranches(
        heads: ["clawdline/task/" + beta: "cafed00d"], merged: [], known: true))
    expect("git is asked about the Project's own canonical repository, once", asked, [repository])
    let seen = (gitAnswered["worktrees"] as? [[String: Any]]) ?? []
    let alphaSeen = seen.first { $0["id"] as? String == alpha } ?? [:]
    let betaSeen = seen.first { $0["id"] as? String == beta } ?? [:]
    expect("a worktree carrying a landing record still says the record, not the branch",
           alphaSeen["landingEvidence"] as? String, "record")
    let grandchild = (alphaSeen["features"] as? [[String: Any]] ?? [])
        .first { $0["id"] as? String == "feature-b" } ?? [:]
    expect("while the Feature with no record of its own is settled by the missing branch",
           grandchild["outcome"] as? String, "branch_gone")
    expect("saying which fact settled it", grandchild["landingEvidence"] as? String,
           "branch_absent")
    expect("a stalled worktree whose branch is still sitting there stays debris",
           betaSeen["outcome"] as? String, "abandoned")
    expect("and reports the branch fact rather than pretending there is none",
           betaSeen["landingEvidence"] as? String, "branch_unmerged")

    let mergedBranches = Orchestrator.RepositoryBranches(
        heads: ["clawdline/task/" + beta: "cafed00d"],
        merged: ["clawdline/task/" + beta], known: true)
    let merged = withGit(mergedBranches, bases: ["clawdline/task/" + beta: "0ddba5e"])
    func betaWith(_ payload: [String: Any]) -> [String: Any] {
        ((payload["worktrees"] as? [[String: Any]]) ?? [])
            .first { $0["id"] as? String == beta } ?? [:]
    }
    let betaMerged = betaWith(merged)
    expect("a branch HEAD already contains landed, whatever the task record says",
           betaMerged["outcome"] as? String, "landed")
    expect("on the branch's evidence and not on a record nobody wrote",
           betaMerged["landingEvidence"] as? String, "branch_merged")

    // **The same reading, one field different: the branch never received a commit.** This is what
    // a Codex worktree child leaves behind between finishing and a root recording the landing,
    // and calling it landed is the direction this whole screen exists to stop.
    let betaEmpty = betaWith(withGit(mergedBranches,
                                     bases: ["clawdline/task/" + beta: "cafed00d"]))
    expect("a branch that is only in HEAD because it was cut there has landed nothing",
           betaEmpty["outcome"] as? String, "abandoned")
    expect("and the row says which containment this is",
           betaEmpty["landingEvidence"] as? String, "branch_empty")
    let betaNoBase = betaWith(withGit(mergedBranches))
    expect("nor is it upgraded when nothing can say what the branch was cut from",
           betaNoBase["outcome"] as? String, "abandoned")
    expect("which is git having answered and the registry having been swept, not an unknown",
           betaNoBase["landingEvidence"] as? String, "branch_base_unknown")
    expect("and the rule the payload publishes names what it actually does",
           merged["outcomeRule"] as? String,
           "landed_by_record_or_nonempty_merged_branch_then_branch_gone_then_delivered_then_"
             + "live_then_abandoned")

    // **An accepted Project identity is whatever a person accepted, and this read hands that key
    // to a subprocess.** `repositoryBranches(in:)` makes it the git process's working directory,
    // so a non-path identity would run `for-each-ref` wherever the app happens to be — and inside
    // any repository that answered, another repository's branches would settle this Project's
    // verdicts with `known == true`. Nothing can write such an identity today; this is the line
    // that keeps the door shut rather than a report that it is currently locked.
    var askedByName: [String] = []
    let named = UsageProjectWorktreeService(
        rows: { rows }, acceptedFeatures: { features },
        acceptedProjects: { ["beta-own": UsageLedger.AcceptedAttribution(id: "acme-widget",
                                                                        label: "acme-widget")] },
        branches: { repository in
            askedByName.append(repository)
            return mergedBranches
        },
        worktreeBases: { ["clawdline/task/" + beta: "0ddba5e"] })
        .read(.init(project: "acme-widget", timezoneID: "UTC"), now: at).payload ?? [:]
    expect("a Project whose identity is not a path is never handed to git", askedByName, [])
    let betaNamed = betaWith(named)
    expect("so its verdict is the one the stored columns give on their own",
           betaNamed["outcome"] as? String, "abandoned")
    expect("and it says that nobody was asked rather than borrowing another repository's answer",
           betaNamed["landingEvidence"] as? String, "unknown")
}

group("an empty worktree list says the query ran, and an unknown Project is refused") {
    let at = ISO8601DateFormatter().date(from: "2026-09-03T11:00:00Z")!
    let rows = [worktreeRow("shared-only", at: at, worktree: "", task: "shared-task")]
    let service = UsageProjectWorktreeService(
        rows: { rows }, acceptedFeatures: { ["shared-only": acceptedFeature("f", "A Feature")] })
    let empty = service.read(.init(project: "widget", timezoneID: "UTC"), now: at)
    expect("a Project whose work never left the shared checkout has no worktrees",
           (empty.payload?["worktrees"] as? [[String: Any]])?.count, 0)
    let receipt = empty.payload?["read"] as? [String: Any] ?? [:]
    // **This is the pair that must never look alike.** An empty list with rows behind it is an
    // answer; the same list with nothing behind it is a query that did not run.
    check("and says so with rows behind it rather than with silence",
          receipt["rows"] as? Int == 1 && receipt["projectRows"] as? Int == 1
            && receipt["worktreeRows"] as? Int == 0
            && empty.payload?["status"] as? String == "available")
    expect("nothing was refused", empty.refusal, nil)

    let missing = service.read(.init(project: "gadget", timezoneID: "UTC"), now: at)
    expect("a Project nothing in range mentions is refused, not answered",
           missing.refusal?.code, "project_not_found")
    expect("with the status a client can branch on", missing.refusal?.status, 404)
    check("and no payload at all", missing.payload == nil
            && missing.refusal?.message.contains("1 row(s) were read") == true)
    let outOfRange = service.read(.init(project: "widget", from: "2026-08-01", to: "2026-08-02",
                                        timezoneID: "UTC"), now: at)
    expect("a range that excludes every row reaches the same refusal",
           outOfRange.refusal?.code, "project_not_found")

    // Two repositories whose final name is the same. The Portfolio puts that name on screen, so
    // answering for whichever one came first would file one's worktrees under the other.
    let namesake = [worktreeRow("left", at: at, worktree: "aaaaaaaa-1111-4222-8333-444444444444",
                                task: "left-task", project: "/private/acme/widget"),
                    worktreeRow("right", at: at, worktree: "aaaaaaaa-1111-4222-8333-555555555555",
                                task: "right-task", project: "/private/other/widget")]
    let ambiguous = UsageProjectWorktreeService(rows: { namesake })
        .read(.init(project: "widget", timezoneID: "UTC"), now: at)
    expect("two Projects of one name are refused rather than merged",
           ambiguous.refusal?.code, "ambiguous_project")
    expect("as a conflict, not a miss", ambiguous.refusal?.status, 409)
    check("and the message hands back both unambiguous ids",
          ambiguous.refusal?.message.contains(
            UsageQueryService.projectID("/private/acme/widget")) == true
            && ambiguous.refusal?.message.contains(
                UsageQueryService.projectID("/private/other/widget")) == true)
    check("while either id answers on its own",
          UsageProjectWorktreeService(rows: { namesake })
            .read(.init(project: UsageQueryService.projectID("/private/other/widget"),
                        timezoneID: "UTC"), now: at).refusal == nil)

    func parse(_ values: [String: String], repeated: Set<String> = []) -> String? {
        UsageProjectWorktreeService.parse(values, repeatedKeys: repeated).error
    }
    expect("the query is closed", parse(["project": "widget", "group": "day"]) != nil, true)
    expect("and may not repeat a key",
           parse(["project": "widget"], repeated: ["project"]) != nil, true)
    expect("a read with no Project is refused before it reads anything",
           parse([:]) != nil, true)
    expect("blank counts as none", parse(["project": "   "]) != nil, true)
    expect("dates are local days", parse(["project": "w", "from": "2026-9-1"]) != nil, true)
    expect("and cannot run backwards",
           parse(["project": "w", "from": "2026-09-02", "to": "2026-09-01"]) != nil, true)
    expect("the timezone must be a zone", parse(["project": "w", "timezone": "Mars/Olympus"]) != nil,
           true)
    expect("a well-formed query parses", parse(["project": "widget", "timezone": "Asia/Taipei"]),
           nil)
    check("a Project id is matched by its shape and not by its prefix",
          UsageProjectWorktreeService.isProjectID(UsageQueryService.projectID("/private/acme/w"))
            && !UsageProjectWorktreeService.isProjectID("project-not-a-digest")
            && !UsageProjectWorktreeService.isProjectID("project-things"))

    // The door, end to end: a refusal this service invents is only worth having if it is the
    // one the route hands back.
    let anonymous = RemoteServer.shared.route(
        remoteRequest("GET", "/v1/orchestrator/usage/project-worktrees?project=widget"))
    expect("an anonymous reader is refused before the scan", anonymous.status, 401)
    check("and this read takes the analytics side door rather than the shared queue",
          RemoteServer.isUsageAnalyticsReading("/v1/orchestrator/usage/project-worktrees"))
    let auth = ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken()]
    check("while the orchestrator token reaches the worker",
          RemoteServer.shared.slowReadingRefusal(
            remoteRequest("GET", "/v1/orchestrator/usage/project-worktrees?project=widget",
                          headers: auth)) == nil)
    let unknownProject = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/usage/project-worktrees?project=no-such-project-anywhere",
        headers: auth))
    expect("a Project this Mac has never recorded is 404 on the route too",
           unknownProject.status, 404)
    expect("carrying the service's own code", remoteErrorCode(unknownProject),
           "project_not_found")
    let misspelled = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/orchestrator/usage/project-worktrees?project=widget&group=day",
        headers: auth))
    expect("and a misspelled filter is refused rather than quietly widening the query",
           misspelled.status, 400)
    expect("as a bad request", remoteErrorCode(misspelled), "bad_request")
}
}
