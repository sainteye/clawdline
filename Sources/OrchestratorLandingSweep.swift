import Foundation

/// **The landing record's lock stops only ever being added to.**
///
/// `landed` had exactly one entrance and a person was standing in it: the record moves only when
/// somebody calls `POST /v1/orchestrator/tasks/:id/landing` with the machine credential, and
/// `POST /v1/orchestrator/sessions/:id/complete` deliberately does not close it. So the
/// closeability projection could add `pending_landing_owned` and `touched_claims_without_closure`
/// and never take one away. On 2026-09-06 one root's card read 「還有 21 項未了結」 — 12 pending
/// landings and 9 touched-claim rows, all from twelve children that had reached `success` months
/// of machine-time earlier and whose records nobody had ever closed.
///
/// This is the broker closing, by itself, what it can prove by itself. It proves two different
/// propositions and never lets the weaker one wear the stronger one's clothes:
///
/// - **Arm 1, ancestry.** An isolated delivery first has to carry a complete clean, non-empty
///   worktree receipt: known base, recorded head, positive commit count, `dirty == false`, chosen
///   head different from base, and no disagreement between a successfully observed live ref and
///   the recorded head. A successfully confirmed absent ref may use the stored receipt; an
///   unreadable ref scan may not. Only then may the sweep ask whether that delivery head is
///   contained by `refs/heads/<target>` in the task's own repository. That is the same ancestry
///   question `tools/check-landing-records.py` asks and calls *unrecorded landing*, and the same one
///   `OrchestratorDraft.verifyTargetLanding` asks for the HTTP route. The record closes through
///   that exact verified path, so `verification_origin`,
///   `verified_commit`, `verified_target_commit` and `landed_at` are all real and
///   ``Orchestrator/isBrokerVerifiedTargetLanding(_:)`` stays true — this delivery reached the
///   target, and the two checks say so.
/// - **Arm 2, write-set containment.** A shared-checkout task with no delivery branch, whose every
///   declared claim **resolves to something git can see** and is both unmodified in its project
///   directory and identical to `refs/heads/<target>` — on two readings at least
///   ``Orchestrator/landingSweepStabilityWindow`` apart. Git cannot be asked about ancestry here —
///   `check-landing-records.py` correctly calls this class `undecidable` — so what is proved is
///   narrower: **nothing this task was allowed to write was outstanding, at two named instants.**
///   The record closes with the target's own commit, a note saying which of the two sentences it
///   rests on and when it was measured, and **no verification fields at all**, so it can never
///   become the two-check `work_complete`.
///
/// **Two rules arm 2 has that arm 1 does not**, both from the review of 2026-09-06, and both
/// because this arm's proposition is instantaneous while the record it writes is permanent:
/// every claim must resolve positively (``landingSweepWriteSet(projectDir:targetCommit:claims:)``
/// — an empty answer about a path that does not exist is not a clean path), and the same clear
/// answer must come back twice (``landingSweepStabilityWindow`` — four rows clean at 20:24 were
/// dirty again at 20:39, and a live sweep would have closed them for ever in between).
///
/// **What it refuses, always.** A settled record (a settled state may never move to another one);
/// a task that is not terminal; a record with no named target; `abandoned`, which is a sentence
/// about somebody giving up and not a thing a machine can observe; and `nothing_to_land`, which
/// ``Orchestrator/nothingToLandAdmission(for:declaredWritePaths:)`` refuses for exactly these
/// tasks anyway. A repository git will not answer for is **skipped**, never read as "nothing
/// landed" — the failure mode `check-landing-records.py` names in `contained_commits`.
extension Orchestrator {

    // MARK: - What a pass may settle

    /// Which of the two propositions closed a record. They are different sentences and the record
    /// says which one it rests on, in its note and in whether it carries verification at all.
    enum LandingSweepArm: String, Equatable {
        /// The delivery reached the target branch.
        case ancestry
        /// Nothing of the task's declared write set is outstanding.
        case writeSet = "write_set"
    }

    /// The registry evidence that says what kind of delivery head a candidate can have. Keeping
    /// the worktree facts together makes it impossible for the question below to mistake an
    /// isolated branch's base commit for delivered bytes merely because git contains that base.
    enum LandingSweepDeliveryEvidence: Equatable {
        case isolated(base: String, head: String?, commits: Int?, dirty: Bool?)
        case sharedCheckout
    }

    /// What one repository-wide delivery-ref scan observed. A successful empty map is evidence
    /// that the named ref is absent; a command that did not answer has a different case and can
    /// never be mistaken for that absence.
    enum LandingSweepDeliveryRefs: Equatable {
        case observed([String: String])
        case unreadable(String)

        func observation(for branch: String) -> LandingSweepDeliveryRefObservation {
            switch self {
            case .observed(let heads):
                return heads[branch].map(LandingSweepDeliveryRefObservation.present) ?? .absent
            case .unreadable(let reason):
                return .unreadable(reason)
            }
        }
    }

    /// The three answers the ref scan can give for one candidate. In particular, `.absent` means
    /// git successfully enumerated the namespace and did not list this branch; it never means git
    /// failed to launch, exited nonzero, or returned output that could not be parsed.
    enum LandingSweepDeliveryRefObservation: Equatable {
        case present(String)
        case absent
        case unreadable(String)
    }

    /// A pending landing this pass may be able to settle, chosen from records alone before git has
    /// been asked anything. Nothing here stats a filesystem: that is the next phase's job.
    struct LandingSweepCandidate: Equatable {
        let taskID: String
        let projectDir: String
        let target: String
        /// The branch a delivery of this task would be on, whether or not one exists.
        let deliveryBranch: String
        /// The typed evidence for the kind of delivery this row represents. An isolated delivery
        /// must answer every stored fact before ancestry can be asked; a shared checkout has no
        /// branch receipt of its own and is normally judged through its write set.
        let deliveryEvidence: LandingSweepDeliveryEvidence
        /// The declared write set. Empty for an isolated task by design — the broker drops that
        /// lease — which is why an isolated task is never an arm-2 candidate.
        let claims: [String]
    }

    /// What one candidate can be asked, once the repository's live delivery refs are known.
    enum LandingSweepQuestion: Equatable {
        case ancestry(head: String)
        case writeSet(paths: [String])
        /// Neither question can be put to git for this row, and why.
        case unanswerable(String)
    }

    /// Why a pass left a record exactly as it found it, or which arm closed it.
    enum LandingSweepVerdict: Equatable {
        case closed(LandingSweepArm)
        case left(String)
    }

    struct LandingSweepOutcome: Equatable {
        let taskID: String
        let verdict: LandingSweepVerdict
    }

    /// What the checkout said about one task's declared write set. Three lists rather than one
    /// boolean, because *somebody is still editing it*, *it never matched the target* and *git was
    /// never asked about it at all* are different findings and a reader of the log needs to tell
    /// them apart.
    struct LandingSweepWriteSet: Equatable {
        /// Claimed paths git reported as modified, staged or untracked in the project directory.
        let outstanding: [String]
        /// Claimed paths whose content differs from the target branch.
        let differing: [String]
        /// **Claims that name nothing git can see**, in the checkout or in the target. An empty
        /// answer from `status` and `diff` about such a claim is not a clean path: it is git
        /// having been asked about nothing. See ``landingSweepWriteSet(projectDir:targetCommit:claims:)``.
        let unresolved: [String]

        init(outstanding: [String], differing: [String], unresolved: [String] = []) {
            self.outstanding = outstanding
            self.differing = differing
            self.unresolved = unresolved
        }

        var isSettled: Bool {
            outstanding.isEmpty && differing.isEmpty && unresolved.isEmpty
        }
    }

    /// One clear reading of a write set, kept between passes so that ``landingSweepWindow`` can
    /// ask whether the same sentence was true twice. Held in memory only: a restart restarts the
    /// window, which is the conservative direction and costs one interval.
    struct LandingSweepObservation: Equatable {
        /// The target tip the write set was compared against.
        let targetCommit: String
        /// The claims that were asked about, in the order they were asked.
        let claims: [String]
        let at: Date
    }

    /// What the stability window says about closing this write set now.
    enum LandingSweepWindow: Equatable {
        /// The first clear reading. Nothing is written; the window opens here.
        case opened
        /// The target moved, or the claims did, so the two readings are not about one subject and
        /// the window starts again rather than continuing.
        case restarted(String)
        /// Clear twice, but not far enough apart yet.
        case tooSoon(TimeInterval)
        /// Clear on two readings of the same subject, at least ``landingSweepStabilityWindow``
        /// apart. Both instants travel so the record can name them.
        case held(first: Date, second: Date)
    }

    /// How many pending records one pass will look at. A cap rather than a whole registry, because
    /// arm 2 costs two short subprocesses per row and this runs on a timer; what a pass does not
    /// reach, the next one does.
    static let landingSweepLimit = 20

    /// How often a pass may run. Nothing here is urgent — the records it closes have been open for
    /// hours — and the cost it must not impose is a git subprocess storm on a five-second beat.
    static let landingSweepInterval: TimeInterval = 300

    /// **How long a write set must stay clear before arm 2 believes it.**
    ///
    /// Arm 2 proves an instantaneous proposition and writes an irreversible record, and the
    /// reviewer measured the gap on real data: four rows that were clean at 20:24 on 2026-09-06
    /// were dirty again by 20:39, because somebody was still editing exactly those files. A live
    /// sweep would have closed all four for ever in between, and no state can move afterwards.
    ///
    /// The asymmetry is total. Every refusal this window can make is *look again in five
    /// minutes*, and a settled tree stays settled — so the cost of waiting is one interval on a
    /// mechanism that runs for ever, and the cost of not waiting is a permanent false sentence
    /// about somebody's unfinished work. That is why this is a wait rather than a heuristic about
    /// mtimes: it does not try to guess whether anybody is still typing, it lets them prove it.
    static let landingSweepStabilityWindow: TimeInterval = 300

    /// The queue one pass runs on. **Its own**, not ``worktreeQueue``.
    ///
    /// A pass is bounded but not short: twenty candidates can be a hundred and forty git
    /// subprocesses at fifteen seconds each in the worst case, and `worktreeQueue` is where
    /// worktree creation and disposal wait. Sharing it made "how long may a pass hold that queue
    /// while a dispatch is waiting behind it" a question somebody had to answer; a queue of its
    /// own makes it a question nobody has to ask. Serial, so two passes still cannot overlap, and
    /// utility, so it competes with nothing on screen.
    static let landingSweepQueue = DispatchQueue(
        label: "com.tsunamiworks.clawdline.orchestrator.landing-sweep", qos: .utility)

    // MARK: - The pure half

    /// Every pending landing this pass may be able to settle, oldest first.
    ///
    /// Pure, and given its tasks rather than reading the registry, so the whole selection table can
    /// be exercised without seeding one. The three refusals that matter are all here: a settled
    /// record is not a candidate at all, a task that has not finished owes nothing yet, and a
    /// record with no target names nothing to be contained by.
    static func landingSweepCandidates(_ tasks: [Task],
                                       limit: Int = landingSweepLimit) -> [LandingSweepCandidate] {
        tasks.filter { task in
            guard task.state.isTerminal, let landing = task.landing,
                  landing.state == .pending,
                  let target = landing.target, !target.isEmpty else { return false }
            // Something has to be askable. A task with neither a checkout nor a declared write set
            // leaves this Mac no evidence of what it wrote, and an absence is not a proof.
            return task.worktree != nil || !task.claims.isEmpty
        }
        .sorted { $0.created == $1.created ? $0.id < $1.id : $0.created < $1.created }
        .prefix(max(0, limit))
        .map { task in
            LandingSweepCandidate(
                taskID: task.id, projectDir: task.projectDir,
                target: task.landing?.target ?? "",
                deliveryBranch: task.worktree?.branch
                    ?? OrchestratorDraft.worktreeBranch(for: task.id) ?? "",
                deliveryEvidence: task.worktree.map {
                    .isolated(base: $0.base, head: $0.head,
                              commits: $0.commits, dirty: $0.dirty)
                } ?? .sharedCheckout,
                claims: task.worktree == nil ? task.claims : [])
        }
    }

    /// A claimed path this sweep is willing to hand to git as a pathspec.
    ///
    /// Dispatch already refuses `..`, and this refuses it again rather than trusting a record
    /// written by an older build; `:` is pathspec magic, and a leading `-` is an option. A single
    /// unusable claim disqualifies the whole task: a write set checked in part is not a write set.
    static func landingSweepClaimsUsable(_ claims: [String]) -> Bool {
        guard !claims.isEmpty else { return false }
        return claims.allSatisfy { path in
            !path.isEmpty && !path.hasPrefix("-") && !path.hasPrefix(":") && path.count <= 500
                && !path.split(separator: "/", omittingEmptySubsequences: false).contains("..")
        }
    }

    /// **Whether a claim names something git can see**, given the paths git listed for it.
    ///
    /// Pathspec matching belongs to git, and this is deliberately the narrow reading of what git
    /// returned: the claim as a file, or the claim as a directory those paths sit under. A claim
    /// git would match some other way — a glob, say — is listed by git and still resolves to
    /// nothing here, and the write set becomes unanswerable rather than settled. That is the
    /// fail-closed direction and the whole reason this function exists.
    static func landingSweepClaimIsListed(_ claim: String, among paths: Set<String>) -> Bool {
        var normalized = claim
        while normalized.hasSuffix("/") { normalized.removeLast() }
        guard !normalized.isEmpty else { return false }
        if paths.contains(normalized) { return true }
        return paths.contains { $0.hasPrefix(normalized + "/") }
    }

    /// The status codes `git status --porcelain=v1` may put in its two-character field.
    ///
    /// A porcelain line is `XY <path>`, and anything that is not that shape did not come from the
    /// records half of the stream. `T` is a typechange, which is in the format and was missing
    /// from every hand-written list of these characters this Mac has produced.
    static let landingSweepPorcelainCodes: Set<Character> = [
        " ", "M", "T", "A", "D", "R", "C", "U", "?", "!",
    ]

    /// The path in one porcelain record, or `nil` if this line is not a porcelain record at all.
    ///
    /// **This is the second half of the reviewer's F2.** git writes diagnostics that are not
    /// failures — `warning: could not open directory 'a/nope/'` arrives with a `0` status — and
    /// when they shared a pipe with the records, `dropFirst(3)` turned that warning into a
    /// claimed path named `ning: could not open directory …`. The seam now keeps the streams
    /// apart, and this keeps the reader honest whatever the seam does: a line that is not
    /// `XY <path>` means git did not answer, never an empty answer.
    static func landingSweepPorcelainPath(_ line: String) -> String? {
        let characters = Array(line)
        guard characters.count > 3,
              landingSweepPorcelainCodes.contains(characters[0]),
              landingSweepPorcelainCodes.contains(characters[1]),
              characters[2] == " " else { return nil }
        let path = String(characters.dropFirst(3))
        return path.trimmingCharacters(in: .whitespaces).isEmpty ? nil : path
    }

    /// Whether a write set found clear now may be closed, or has only started a window.
    ///
    /// Pure, and given the previous observation rather than reading it, so the whole table can be
    /// exercised without waiting five minutes for it. See ``landingSweepStabilityWindow`` for why
    /// there is a window at all.
    ///
    /// **Both readings must be about the same subject.** A moved target tip or a changed claim
    /// list means the second reading answered a different question from the first, so the window
    /// restarts rather than continuing — otherwise "clear twice" could be assembled out of two
    /// halves that were never true together.
    static func landingSweepWindow(now: LandingSweepObservation,
                                   previous: LandingSweepObservation?,
                                   window: TimeInterval = landingSweepStabilityWindow)
        -> LandingSweepWindow {
        guard let previous else { return .opened }
        if previous.targetCommit != now.targetCommit {
            return .restarted("refs/heads/<target> moved from "
                              + "\(previous.targetCommit.prefix(8)) to "
                              + "\(now.targetCommit.prefix(8)) between the two readings")
        }
        if previous.claims != now.claims {
            return .restarted("the declared write set changed between the two readings")
        }
        let elapsed = now.at.timeIntervalSince(previous.at)
        guard elapsed >= window else { return .tooSoon(window - elapsed) }
        return .held(first: previous.at, second: now.at)
    }

    /// The sweep's own sentence, with the note the record was opened with kept behind it.
    ///
    /// A root's pending note says what it was waiting for. The HTTP route replaces it with a
    /// person standing there who can write the sentence again; nothing here can, so the original
    /// travels with the closure. `OrchestratorStore.landing(from:)` drops the **whole record**
    /// when a note is longer than its limit, so what gets cut when they do not both fit is the
    /// borrowed half, never the sweep's own account of what it proved.
    /// The result is never longer than `limit`, whatever it is given: a branch name may be 200
    /// characters and the sweep's own sentence is not short, so the arithmetic is done here rather
    /// than assumed to come out.
    static func landingSweepNote(_ sentence: String, keeping existing: String?,
                                 limit: Int = 500) -> String {
        func clipped(_ text: String) -> String {
            text.count <= limit ? text : String(text.prefix(max(0, limit - 1))) + "…"
        }
        let kept = existing?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !kept.isEmpty else { return clipped(sentence) }
        let preamble = " Opened with this note: "
        let room = limit - sentence.count - preamble.count
        // Under this much room the fragment stops being a sentence and starts being a puzzle, so
        // the record says nothing rather than a third of something.
        guard room >= 24 else { return clipped(sentence) }
        if kept.count <= room { return sentence + preamble + kept }
        return sentence + preamble + String(kept.prefix(room - 1)) + "…"
    }

    /// Which question this candidate answers, given what the repository's refs say right now.
    ///
    /// A shared checkout with a live delivery ref keeps the old ancestry question; otherwise it
    /// uses its declared write set. An isolated delivery is stricter: ancestry is admissible only
    /// when its complete typed receipt proves a clean, non-empty branch and a successfully observed
    /// live ref (when present) agrees with the recorded head. An unreadable scan, and every missing
    /// or contradictory fact, leaves the obligation pending; an isolated row never falls through
    /// to the write-set arm.
    static func landingSweepQuestion(for candidate: LandingSweepCandidate,
                                     deliveryRef: LandingSweepDeliveryRefObservation)
        -> LandingSweepQuestion {
        switch candidate.deliveryEvidence {
        case .isolated(let base, let storedHead, let commits, let dirty):
            guard !base.isEmpty else {
                return .unanswerable("isolated delivery base is unknown")
            }
            guard let storedHead, !storedHead.isEmpty else {
                return .unanswerable("isolated delivery head is unknown")
            }
            guard let commits else {
                return .unanswerable("isolated delivery commit count is unknown")
            }
            guard commits > 0 else {
                return .unanswerable("isolated delivery branch has no commits beyond its base")
            }
            guard let dirty else {
                return .unanswerable("isolated delivery dirty state is unknown")
            }
            guard !dirty else {
                return .unanswerable("isolated delivery worktree still has dirty bytes")
            }
            let head: String
            switch deliveryRef {
            case .present(let liveHead):
                guard liveHead == storedHead else {
                    return .unanswerable(
                        "isolated delivery live head contradicts its recorded head")
                }
                head = liveHead
            case .absent:
                // A successfully enumerated namespace with no such ref is the one situation where
                // a clean, non-empty stored receipt may outlive its deleted delivery branch.
                head = storedHead
            case .unreadable(let reason):
                return .unanswerable(reason)
            }
            guard head != base else {
                return .unanswerable("isolated delivery head is still its base")
            }
            return .ancestry(head: head)
        case .sharedCheckout:
            switch deliveryRef {
            case .present(let liveHead):
                return .ancestry(head: liveHead)
            case .absent:
                break
            case .unreadable(let reason):
                return .unanswerable(reason)
            }
        }
        guard landingSweepClaimsUsable(candidate.claims) else {
            return .unanswerable("no delivery branch, and no usable declared write set")
        }
        return .writeSet(paths: candidate.claims)
    }

    /// The record a proved candidate closes as. Fail-closed in both directions: an ancestry arm
    /// without a broker verification, or a write-set arm carrying one, returns nil rather than
    /// writing a record whose shape contradicts what was proved.
    static func landingSweepLanding(existing: Landing, arm: LandingSweepArm,
                                    verification: LandingVerification?,
                                    targetCommit: String, target: String,
                                    claimCount: Int, now: Date,
                                    observed: (first: Date, second: Date)? = nil) -> Landing? {
        switch arm {
        case .ancestry:
            guard let verification, verification.origin == "local_target_branch",
                  observed == nil,
                  isFullObjectID(verification.commit),
                  isFullObjectID(verification.targetCommit) else { return nil }
            return Landing(
                state: .landed, target: target, delivery: existing.delivery,
                ownerRootKey: existing.ownerRootKey, since: existing.since,
                commit: verification.commit,
                note: landingSweepNote(
                    "Broker landing sweep: refs/heads/\(target) contains this delivery's head "
                        + "\(verification.commit.prefix(8)), verified in the task's own "
                        + "repository. That proves the delivery reached the target.",
                    keeping: existing.note),
                landedAt: now,
                verificationOrigin: verification.origin,
                verifiedCommit: verification.commit,
                verifiedTargetCommit: verification.targetCommit)
        case .writeSet:
            // Two instants, or nothing. What this arm proves is true at a moment, and the record
            // it writes is permanent, so a record that cannot say *when* it was measured is the
            // shape the reviewer's F3 is about — see ``landingSweepStabilityWindow``.
            guard verification == nil, isFullObjectID(targetCommit),
                  let observed, observed.second >= observed.first else { return nil }
            // No `landedAt` and no verification triple, deliberately. This arm never verified that
            // a commit of this task's reached the target — there is no commit of its own to check
            // — so a record that looked broker-verified would be the weaker sentence wearing the
            // stronger one's clothes, and `isBrokerVerifiedTargetLanding` would turn it into the
            // double check.
            return Landing(
                state: .landed, target: target, delivery: existing.delivery,
                ownerRootKey: existing.ownerRootKey, since: existing.since,
                commit: targetCommit,
                // Past tense, and with both instants in it. The sentence this proves was true
                // when it was measured; a reader months later needs to know when that was rather
                // than only that somebody once said it.
                note: landingSweepNote(
                    "Broker landing sweep: all \(claimCount) declared path(s) were unmodified in "
                        + "this task's project directory and identical to refs/heads/\(target) at "
                        + "\(targetCommit.prefix(8)), on two readings — "
                        + "\(landingSweepInstant(observed.first)) and "
                        + "\(landingSweepInstant(observed.second)). That proves nothing of this "
                        + "task's write set was outstanding. It is not proof that a delivery "
                        + "reached the target.",
                    keeping: existing.note),
                landedAt: nil)
        }
    }

    /// The instants an arm-2 note names, to the second and in UTC, because the note is read by
    /// whoever is deciding whether the sentence is still true.
    static func landingSweepInstant(_ date: Date) -> String {
        landingSweepClock.string(from: date)
    }

    private static let landingSweepClock: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        return formatter
    }()

    /// A full lowercase object id, the only shape a commit written here may take. The same test
    /// `isBrokerVerifiedTargetLanding` and `OrchestratorStore.landing(from:)` already apply.
    static func isFullObjectID(_ id: String) -> Bool {
        (id.count == 40 || id.count == 64)
            && id.allSatisfy { ("0"..."9").contains($0) || ("a"..."f").contains($0) }
    }

    // MARK: - The git half

    /// Every commit `refs/heads/<target>` contains, and that branch's own tip.
    ///
    /// One subprocess for a whole repository rather than a `merge-base --is-ancestor` per task —
    /// the shape `check-landing-records.py` measured at 1,196 commits in 9 ms. `nil` means git did
    /// not answer, and the caller then skips the repository rather than reading an empty set as
    /// "nothing has landed", which would make every delivery look outstanding.
    static func landingSweepTargetHistory(gitDirectory: String, target: String)
        -> (tip: String, contained: Set<String>)? {
        // `refs/heads/a..b` is a revision *range*, so an unchecked target would silently change
        // the question. This is the same gate `verifyTargetLanding` opens with.
        guard let format = OrchestratorDraft.git(["check-ref-format", "--branch", target],
                                                 cwd: "/", gitDirectory: gitDirectory),
              format.status == 0,
              let answer = OrchestratorDraft.git(["rev-list", "refs/heads/\(target)"],
                                                 cwd: "/", gitDirectory: gitDirectory),
              answer.status == 0 else { return nil }
        var contained: Set<String> = []
        var tip = ""
        for line in answer.output.split(separator: "\n") {
            let id = line.trimmingCharacters(in: .whitespaces)
            guard !id.isEmpty else { continue }
            if tip.isEmpty { tip = id }
            contained.insert(id)
        }
        guard isFullObjectID(tip) else { return nil }
        return (tip, contained)
    }

    /// Test-only injection at the exact subprocess seam used by the production pass. Nil runs the
    /// real command. Tests use a nonzero answer to prove command failure remains different from a
    /// successful scan which simply did not list one branch.
    static var landingSweepDeliveryRefsGitOverrideForTesting:
        ((String) -> OrchestratorDraft.GitAnswer?)?

    /// Delivery branch name → the commit it points at now. One subprocess per repository. The
    /// result keeps command failure and malformed output out of the successful-map case, so an
    /// empty map can mean only that git successfully confirmed the namespace was empty.
    static func landingSweepDeliveryRefs(gitDirectory: String) -> LandingSweepDeliveryRefs {
        let answer: OrchestratorDraft.GitAnswer?
        if let override = landingSweepDeliveryRefsGitOverrideForTesting {
            answer = override(gitDirectory)
        } else {
            answer = OrchestratorDraft.git(
                ["for-each-ref", "--format=%(refname:short) %(objectname)",
                 "refs/heads/clawdline/task/"], cwd: "/", gitDirectory: gitDirectory)
        }
        guard let answer else {
            return .unreadable("git could not launch the delivery-ref scan")
        }
        guard answer.status == 0 else {
            return .unreadable("git delivery-ref scan exited with status \(answer.status)")
        }
        guard answer.outputIsUTF8 else {
            return .unreadable("git delivery-ref scan output is not valid UTF-8")
        }
        var heads: [String: String] = [:]
        for line in answer.output.split(separator: "\n") {
            let parts = line.split(separator: " ")
            guard parts.count == 2 else {
                return .unreadable("git delivery-ref scan returned malformed output")
            }
            let branch = String(parts[0])
            let head = String(parts[1])
            guard branch.hasPrefix("clawdline/task/"), isFullObjectID(head),
                  heads[branch] == nil else {
                return .unreadable("git delivery-ref scan returned malformed output")
            }
            heads[branch] = head
        }
        return .observed(heads)
    }

    /// Whether every claimed path resolves to something, is unmodified in the checkout, and is
    /// identical to the target.
    ///
    /// Three questions and three subprocesses at most, all read-only — `GIT_OPTIONAL_LOCKS=0` is
    /// set on the one git seam this goes through, so a pass cannot take the index lock out from
    /// under somebody working in that tree. `nil` means git would not answer, which is never read
    /// as a settled write set.
    ///
    /// **The first question is the one that was missing, and it is the reviewer's F1.**
    /// `git status` and `git diff` both answer *exit 0, nothing* for a pathspec that matches no
    /// file at all, which is the same answer they give for a path that is clean. So a claim like
    /// `blog/en/what-clawdline-is delivered-is-not-landed work-you-can-cancel.md` — three real
    /// filenames a dispatch joined with spaces into one path that does not exist — read as *this
    /// task's whole write set is settled*, and three of the twelve rows in the 11:43 snapshot of
    /// 2026-09-06 were exactly that. The record it would have written is permanent.
    ///
    /// So every claim must resolve **positively**, and there are two ways rather than one:
    ///
    /// - `git ls-files` lists it in the checkout, or
    /// - `git ls-tree -r` lists it in the target commit.
    ///
    /// The second exists so that a delivery whose whole point was deleting a file is not read as
    /// undecidable: the path is gone from the checkout and still in the target. A claim neither
    /// answers for makes the **whole** write set unanswerable, naming that claim — a write set
    /// checked in part is not a write set. Two consequences are deliberate: a claim naming a file
    /// this delivery created and never committed anywhere resolves nowhere and is left open, and
    /// so is a landed deletion, whose path is absent from both. Both are *look again* rather than
    /// a permanent sentence, and neither is a claim about somebody's work.
    static func landingSweepWriteSet(projectDir: String, targetCommit: String,
                                     claims: [String]) -> LandingSweepWriteSet? {
        guard landingSweepClaimsUsable(claims), isFullObjectID(targetCommit) else { return nil }
        var isDirectory: ObjCBool = false
        guard FileManager.default.fileExists(atPath: projectDir, isDirectory: &isDirectory),
              isDirectory.boolValue else { return nil }
        // `-z` rather than newlines: git quotes a path with a non-ASCII byte in it, and these are
        // matched against the claims rather than only counted.
        guard let listed = OrchestratorDraft.git(
                ["ls-files", "-z", "--"] + claims,
                cwd: projectDir, separateStandardError: true), listed.status == 0
        else { return nil }
        var resolved = Set(listed.output.split(separator: "\0").map(String.init))
        var unresolved = claims.filter { !landingSweepClaimIsListed($0, among: resolved) }
        // Only asked when the checkout could not answer for every claim, which is the uncommon
        // half: an ordinary write set costs three subprocesses, not four.
        if !unresolved.isEmpty {
            guard let inTarget = OrchestratorDraft.git(
                    ["ls-tree", "-r", "-z", "--name-only", targetCommit, "--"] + unresolved,
                    cwd: projectDir, separateStandardError: true), inTarget.status == 0
            else { return nil }
            resolved.formUnion(inTarget.output.split(separator: "\0").map(String.init))
            unresolved = unresolved.filter { !landingSweepClaimIsListed($0, among: resolved) }
        }
        guard let status = OrchestratorDraft.git(
                ["status", "--porcelain=v1", "--untracked-files=all", "--"] + claims,
                cwd: projectDir, separateStandardError: true), status.status == 0,
              let diff = OrchestratorDraft.git(
                ["diff", "--name-only", "--no-ext-diff", targetCommit, "--"] + claims,
                cwd: projectDir, separateStandardError: true), diff.status == 0 else { return nil }
        // A line that is not `XY <path>` means this stream is not what it is being read as, and
        // an unreadable answer is `nil` — never an empty one. See ``landingSweepPorcelainPath``.
        var outstanding: [String] = []
        for line in status.output.split(separator: "\n") {
            guard let path = landingSweepPorcelainPath(String(line)) else { return nil }
            outstanding.append(path)
        }
        let differing = diff.output.split(separator: "\n").map(String.init)
            .filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
        return LandingSweepWriteSet(outstanding: outstanding, differing: differing,
                                    unresolved: unresolved)
    }

    // MARK: - One pass

    private static var landingSweepInFlight = false
    private static var landingSweepLastPass: Date?
    /// The last clear reading of each task's write set, for ``landingSweepWindow``. Written and
    /// read only from a pass, which is serialized on ``landingSweepQueue``, and cleared by
    /// ``forgetLandingSweepObservations()`` where a test needs a fresh window.
    private static var landingSweepObservations: [String: LandingSweepObservation] = [:]

    /// Main thread, from ``beat(fromTimer:)``, and nowhere else.
    ///
    /// Everything expensive is behind two gates: a pass runs at most once every
    /// ``landingSweepInterval``, and never while one is still running. The work itself goes to
    /// ``landingSweepQueue``, a serial utility queue of this sweep's own — so nothing here ever
    /// asks git a question from the main queue, nothing asks one while holding the registry lock,
    /// and a long pass cannot make a dispatch wait behind it. A `sync` back onto a queue this pass
    /// is already on is the shape that produced an exit-133 deadlock here; there is none.
    static func scheduleLandingSweep(now: Date = Date()) {
        guard !landingSweepInFlight else { return }
        if let last = landingSweepLastPass, now.timeIntervalSince(last) < landingSweepInterval {
            return
        }
        landingSweepLastPass = now
        landingSweepInFlight = true
        landingSweepQueue.async {
            _ = landingSweepPass(now: Date())
            DispatchQueue.main.async { landingSweepInFlight = false }
        }
    }

    /// One pass, synchronous, off the main queue. Returns what it decided about every candidate it
    /// looked at, oldest first, which is what a test reads and what the audit log records.
    @discardableResult
    static func landingSweepPass(now: Date = Date(),
                                 limit: Int = landingSweepLimit) -> [LandingSweepOutcome] {
        load()
        lock.lock()
        let evidence = Array(tasks.values)
        lock.unlock()
        let candidates = landingSweepCandidates(evidence, limit: limit)
        guard !candidates.isEmpty else { return [] }
        let byID = Dictionary(evidence.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })

        // Repository identity is resolved once per checkout rather than once per task: for a
        // shared-checkout backlog — which is the whole class arm 2 exists for — that is one
        // resolution for twelve rows. It stays per task only where the answer genuinely depends on
        // the task, which is the worktree case `landingGitDirectory` validates against the task id.
        var identities: [String: String?] = [:]
        var histories: [String: (tip: String, contained: Set<String>)?] = [:]
        var deliveryRefs: [String: LandingSweepDeliveryRefs] = [:]
        var out: [LandingSweepOutcome] = []
        // One save and one broadcast for a whole pass rather than per row. `broadcastOrchestrator`
        // rebuilds the wire shape through `records()`, which crosses to the main queue, and a
        // first pass over a backlog closes twenty rows: twenty crossings and twenty whole-registry
        // writes for one screen that changes once.
        var closed: [(task: Task, arm: LandingSweepArm, landing: Landing)] = []

        for candidate in candidates {
            guard let task = byID[candidate.taskID], let existing = task.landing else { continue }
            let identityKey = task.isolation == .worktree
                ? "worktree:\(task.id)"
                : "shared:\(task.projectDir)|\(task.repositoryCommonDir ?? "")"
            let gitDirectory: String?
            if let cached = identities[identityKey] {
                gitDirectory = cached
            } else {
                gitDirectory = OrchestratorDraft.landingGitDirectory(for: task, among: evidence)
                identities[identityKey] = gitDirectory
            }
            guard let gitDirectory else {
                out.append(LandingSweepOutcome(
                    taskID: candidate.taskID,
                    verdict: .left("this Mac cannot resolve the task's repository")))
                continue
            }
            let historyKey = "\(gitDirectory)|\(candidate.target)"
            let history: (tip: String, contained: Set<String>)?
            if let cached = histories[historyKey] {
                history = cached
            } else {
                history = landingSweepTargetHistory(gitDirectory: gitDirectory,
                                                    target: candidate.target)
                histories[historyKey] = history
            }
            guard let history else {
                out.append(LandingSweepOutcome(
                    taskID: candidate.taskID,
                    verdict: .left("git would not answer for refs/heads/\(candidate.target) in "
                                   + "this repository, which is not the same as nothing having "
                                   + "landed")))
                continue
            }
            let refObservation: LandingSweepDeliveryRefs
            if let cached = deliveryRefs[gitDirectory] {
                refObservation = cached
            } else {
                refObservation = landingSweepDeliveryRefs(gitDirectory: gitDirectory)
                deliveryRefs[gitDirectory] = refObservation
            }

            switch landingSweepQuestion(
                    for: candidate,
                    deliveryRef: refObservation.observation(for: candidate.deliveryBranch)) {
            case .unanswerable(let why):
                out.append(LandingSweepOutcome(taskID: candidate.taskID, verdict: .left(why)))
            case .ancestry(let head):
                guard history.contained.contains(head) else {
                    out.append(LandingSweepOutcome(
                        taskID: candidate.taskID,
                        verdict: .left("refs/heads/\(candidate.target) does not contain "
                                       + "\(head.prefix(8))")))
                    continue
                }
                guard let verification = OrchestratorDraft.verifyTargetLanding(
                        gitDirectory: gitDirectory, target: candidate.target, commit: head),
                      let landing = landingSweepLanding(
                        existing: existing, arm: .ancestry, verification: verification,
                        targetCommit: history.tip, target: candidate.target,
                        claimCount: candidate.claims.count, now: now) else {
                    out.append(LandingSweepOutcome(
                        taskID: candidate.taskID,
                        verdict: .left("the containment held and the verification did not, so "
                                       + "nothing was written")))
                    continue
                }
                let applied = applyLandingSweep(task: task, expected: existing, landing: landing,
                                                arm: .ancestry)
                if applied.settled != nil {
                    closed.append((task, .ancestry, landing))
                }
                out.append(LandingSweepOutcome(taskID: candidate.taskID,
                                               verdict: applied.verdict))
            case .writeSet(let paths):
                guard let answer = landingSweepWriteSet(projectDir: candidate.projectDir,
                                                        targetCommit: history.tip, claims: paths)
                else {
                    out.append(LandingSweepOutcome(
                        taskID: candidate.taskID,
                        verdict: .left("git would not answer for this task's declared write set "
                                       + "in \(candidate.projectDir)")))
                    continue
                }
                guard answer.isSettled else {
                    // A claim that resolves to nothing is reported before the other two, because
                    // it is the one that says *this question was never asked* rather than *the
                    // answer was no*. The whole write set is unanswerable and the reason names
                    // the claim, so that a dispatch which joined three filenames into one path is
                    // legible from the log rather than only from the registry.
                    let why: String
                    if let first = answer.unresolved.first {
                        why = "unanswerable: the declared path \"\(first)\" names nothing in the "
                            + "checkout or in refs/heads/\(candidate.target)"
                            + (answer.unresolved.count > 1
                               ? ", and \(answer.unresolved.count - 1) other(s) do not either" : "")
                    } else if answer.outstanding.isEmpty {
                        why = "\(answer.differing.count) claimed path(s) differ from "
                            + "refs/heads/\(candidate.target)"
                    } else {
                        why = "\(answer.outstanding.count) claimed path(s) are still uncommitted "
                            + "in the checkout"
                    }
                    out.append(LandingSweepOutcome(taskID: candidate.taskID, verdict: .left(why)))
                    landingSweepObservations[candidate.taskID] = nil
                    continue
                }
                // Clear now. Whether it was also clear five minutes ago is a different question,
                // and it is the one that decides whether a permanent record may be written —
                // see ``landingSweepStabilityWindow``.
                let reading = LandingSweepObservation(targetCommit: history.tip, claims: paths,
                                                      at: now)
                let window = landingSweepWindow(
                    now: reading, previous: landingSweepObservations[candidate.taskID])
                let observed: (first: Date, second: Date)
                switch window {
                case .opened:
                    landingSweepObservations[candidate.taskID] = reading
                    out.append(LandingSweepOutcome(
                        taskID: candidate.taskID,
                        verdict: .left("the write set is clear on this reading; a second reading "
                                       + "at least \(Int(landingSweepStabilityWindow))s later has "
                                       + "to agree before anything is written")))
                    continue
                case .restarted(let why):
                    landingSweepObservations[candidate.taskID] = reading
                    out.append(LandingSweepOutcome(
                        taskID: candidate.taskID,
                        verdict: .left("the stability window restarted: \(why)")))
                    continue
                case .tooSoon(let remaining):
                    out.append(LandingSweepOutcome(
                        taskID: candidate.taskID,
                        verdict: .left("the write set has been clear on two readings, "
                                       + "\(Int(remaining))s short of the "
                                       + "\(Int(landingSweepStabilityWindow))s this arm waits")))
                    continue
                case .held(let first, let second):
                    observed = (first, second)
                }
                guard let landing = landingSweepLanding(
                        existing: existing, arm: .writeSet, verification: nil,
                        targetCommit: history.tip, target: candidate.target,
                        claimCount: paths.count, now: now, observed: observed) else {
                    out.append(LandingSweepOutcome(
                        taskID: candidate.taskID,
                        verdict: .left("the write set was settled and its record would not form")))
                    continue
                }
                let applied = applyLandingSweep(task: task, expected: existing, landing: landing,
                                                arm: .writeSet)
                if applied.settled != nil {
                    closed.append((task, .writeSet, landing))
                    landingSweepObservations[candidate.taskID] = nil
                }
                out.append(LandingSweepOutcome(taskID: candidate.taskID,
                                               verdict: applied.verdict))
            }
        }
        // Once, and after the write is durable: an audit line and a ledger row are receipts for
        // something that happened, and a crash between the two would otherwise leave a ledger
        // saying `landed` about a record the store never kept.
        if !closed.isEmpty {
            _ = save()
            RemoteServer.shared.broadcastOrchestrator()
            for (task, arm, landing) in closed {
                RemoteAuth.audit("orchestrator.landing.sweep", [
                    "task": task.id, "arm": arm.rawValue, "target": landing.target ?? "",
                    "commit": landing.commit ?? "",
                    "verified": Orchestrator.isBrokerVerifiedTargetLanding(landing) ? "1" : "0",
                ])
                var settled = task
                settled.landing = landing
                recordLandingInLedger(settled)
            }
        }
        return out
    }

    /// Forget every open stability window. For tests, which drive two passes seconds apart and
    /// must not inherit a window an earlier group opened about the same task id.
    static func forgetLandingSweepObservations() {
        landingSweepObservations.removeAll()
    }

    /// The write, under the lock, with the compare-and-swap the git subprocesses above make
    /// necessary: the record and the task state must both be exactly what this pass decided about,
    /// or the decision belonged to a world that has moved and nothing is written. A record that
    /// settled while git was running — the route, another pass, a person — is left alone, because
    /// a settled state may never move to another one.
    /// Saving, broadcasting and the receipts belong to the pass, once, after every row — this is
    /// only the compare-and-swap.
    ///
    /// Internal rather than private so that the losing side of that swap can be driven directly:
    /// a pass cannot race itself on one thread, and a refusal nobody has watched happen is a
    /// refusal nobody knows works.
    static func applyLandingSweep(task: Task, expected: Landing, landing: Landing,
                                  arm: LandingSweepArm)
        -> (verdict: LandingSweepVerdict, settled: Task?) {
        lock.lock()
        guard var current = tasks[task.id], current.state == task.state,
              current.landing == expected, expected.state == .pending else {
            lock.unlock()
            return (.left("the record changed while git was being asked, so it was left alone"),
                    nil)
        }
        current.landing = landing
        tasks[task.id] = current
        let settled = current
        lock.unlock()
        return (.closed(arm), settled)
    }
}
