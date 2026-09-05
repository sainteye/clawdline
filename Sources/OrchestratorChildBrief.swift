import Foundation

// What a child is told when it is dispatched. Almost all of this file is the briefing text
// itself, which is data rather than logic, and it was the largest block in Orchestrator.swift
// that reads nothing the lock protects. `policySection()` comes with it: it has exactly one
// caller, and Swift's `private` would have made it invisible from here.
extension Orchestrator {
    /// What this Mac has said about itself, for every child briefing.
    ///
    /// **This travelled with the dispatch recipe once, and that was the wrong home for it.**
    /// The file was reasoned about as rules for *handing work out*, so when the tree lost its
    /// second level the section read as dead weight and was deleted with the recipe. It is not
    /// dead weight: the same file is where a person writes down what is true of this machine,
    /// and one of those sentences — that a Codex child's sandbox has no network — is measurably
    /// what stops a Codex child spending a turn on a `curl` that cannot connect. A leaf reads it
    /// and behaves differently, which is the whole test of whether a paragraph belongs in a
    /// briefing. So it goes to every child, dispatcher or not, and there is no longer any such
    /// thing as the second kind.
    ///
    /// Read from disk at briefing time, so an edit reaches the next child rather than the next
    /// launch. Empty when nobody has written anything, rather than a heading with nothing under
    /// it.
    private static func policySection() -> String {
        guard let policy = policy() else { return "" }
        return """


        ## What this Mac says

        House rules and machine facts, from \(policyURL.path) and its optional local sibling at
        \(localPolicyURL.path). They are the person's, not this app's; where they and your own
        judgement disagree, follow them and say so in your summary.

        \(policy)
        """
    }


    /// The announcement rides in the typed line and not only in CHILD.md, because an assistant
    /// answers the line before it opens the file — and the first thing on the screen should be
    /// what the child was sent to do, in the language of whoever is looking.
    static func firstLine(id: String, secret: String, announce: String? = nil) -> String {
        let opening = announce.map { "Say this line first, verbatim: \($0) Then read" } ?? "Read"
        return "You are a Clawdline CHILD agent for task \(id). "
            + "\(opening) /tmp/.clawdline/\(id)/CHILD.md and follow it exactly. TASK_SECRET=\(secret)"
    }

    /// The interface language, named twice — in English for the assistant reading the briefing,
    /// and in itself for the person reading over its shoulder: "Traditional Chinese (繁體中文)".
    static var languageName: String { rootAssignmentLanguage().name }

    static func rootAssignmentLanguage(copy: Copy = L.t) -> RootAssignmentLanguage {
        let tag = L.tag(of: copy)
        let english: String
        switch tag {
        case "zh-Hant": english = "Traditional Chinese"; case "zh-Hans": english = "Simplified Chinese"
        default: english = Locale(identifier: "en").localizedString(forIdentifier: tag) ?? tag
        }
        let native = Locale(identifier: tag).localizedString(forIdentifier: tag) ?? tag
        return RootAssignmentLanguage(tag: tag,
            name: english == native ? english : "\(english) (\(native))")
    }

    /// How many levels of dispatch this Mac has: one. A root opens children, and a child is the
    /// bottom — it opens nothing.
    ///
    /// **A constant rather than a setting, and that is the whole point.** This used to be read
    /// out of `orchestrator_max_grandchildren`, which meant the depth of the tree was a number
    /// in a file. Two things are wrong with that. `config.json` is seeded once and never
    /// migrated, so changing the default would have left every Mac that had already run this app
    /// dispatching grandchildren for ever; and a rule that a hand-edit can undo is not a rule,
    /// it is a preference. What a child needs when a job is too big for one session is its own
    /// assistant's subagents — Claude Code's Task tool, Codex's subagents — which cost no
    /// terminal tab, no broker capacity and no second level of supervision.
    static let depthFloor = 1

    /// Whether a task at this depth may exist at all. `depth` is the new task's own level: 1 for
    /// a root's child, 2 for anything a child tries to open. Pure, so the one-level rule can be
    /// checked without a broker, a terminal or a config file.
    static func depthIsAllowed(_ depth: Int) -> Bool { depth <= depthFloor }

    static func childBrief(for task: Task) -> String {
        let dir = "/tmp/.clawdline/\(task.id)"
        let workspaceRule: String
        let isolationSection: String
        if let worktree = task.worktree {
            workspaceRule = "- Work inside \(worktree.cwd). Commit repository changes there; put "
                + "non-repository artifacts in \(dir)/artifacts/."
            isolationSection = """

            ## Your isolated checkout

            This is a fresh checkout of commit `\(worktree.base)` on branch `\(worktree.branch)`.
            Uncommitted files from the base repository are deliberately absent. Files ignored by
            gitignore — dependencies, build caches, and local environment files — are absent too;
            install them only after checking that doing so will not consume most of your timeout.

            \(task.assistant == .codex
              ? """
                **Do not commit. Leave your work uncommitted in this checkout.** A linked
                worktree keeps its git metadata in the base repository's
                `.git/worktrees/<task-id>/`, which is outside what you can write, so `git add`
                fails with `Operation not permitted` on `index.lock` and the delivery is
                reported as a failure with the work done. That has cost this repository whole
                rounds. The root reads this checkout and commits for you; your delivery is the
                bytes, not a branch. You may use `git status`, `git diff`, `git log` and
                `git show` to read.
                """
              : """
                **Commit early and often.** Commit only on this branch: the branch is the
                delivery, and uncommitted changes can be lost when the checkout is cleaned. You
                may use `git add`, `git commit`, `git status`, `git diff`, `git log`, and
                `git show` here.
                """)
            Do not push, switch or check out another branch, rebase, merge, hard-reset, stash,
            use `--git-dir` or `git -C` to reach the base repository, run any `git worktree`
            command, or run `./build.sh`. The app records commits, HEAD and dirty state from git;
            these rules are briefing rules rather than a shell sandbox.

            This checkout's `.build/` is reclaimed on the same schedule as `work/` once the task
            ends. The source and the delivery branch are never touched by that, but nothing you
            want to keep should be left inside a build directory.
            """
        } else {
            workspaceRule = "- Work inside \(task.projectDir). Put every file you produce in "
                + "\(dir)/artifacts/\n  (create the directory if it is missing)."
            isolationSection = ""
        }
        // Where this one stands, said plainly and once. Written into the briefing rather than
        // left to be discovered, because a child that finds out by being refused has already
        // spent a turn on it — and one that assumes it may dispatch spends several. The second
        // sentence is the part that changes behaviour rather than only forbidding it: the work
        // that used to be handed to a grandchild is work an assistant's own subagents do,
        // without a terminal tab, a briefing or a level of supervision under this one.
        let handOnRule = "**You are the bottom of this tree: you cannot dispatch Clawdline tasks "
            + "of your own, and a request to open one is refused.** When part of this needs to "
            + "run in parallel or wants a context of its own, use your own assistant's built-in "
            + "subagents (Claude Code's Task tool, Codex's subagents). They cost no terminal tab "
            + "and no broker capacity, and their answers come back to you rather than to a file."
        // What this Mac has said about itself, for every child rather than for a dispatcher.
        // See `policySection()`; it is empty when nobody has written anything.
        let houseRules = policySection()
        let verificationMinutes = task.timeoutMinutes % 3 == 0
            ? String(task.timeoutMinutes / 3)
            : String(format: "%.1f", Double(task.timeoutMinutes) / 3.0)
        let attachedSection = task.attachSessionId == nil ? "" : """

        ## Your standing session

        This task was attached to a standing session instead of opening a new tab. Finishing,
        failing or cancelling this task does not end this session; after `result.json` is written,
        leave the tab ready for the next complete follow-up task.

        Clawdline recorded that this process was launched with access to the whole
        `/tmp/.clawdline` task root; sessions given only their original task directory are
        refused before a follow-up is typed. This follow-up did not open the tab, however: if any
        permission, plan or confirmation menu appears, leave it for the session's owner.
        Clawdline does not choose from a menu on a session this task did not open. If the briefing
        is still unaccepted when this task's timeout expires, the task ends as `timeout` and
        releases the standing session and its claims.

        """
        // A Codex child's sandbox has no network at all — measured by task be9a54c0, not
        // inferred: CODEX_SANDBOX_NETWORK_DISABLED=1 is set, a curl to 127.0.0.1 exits 7 after
        // 0 ms, DNS itself is off, and no approval prompt ever appears. 133 codex children
        // were briefed to curl a progress note and 0 notes arrived; result.json always worked
        // because it is a file. So every loopback recipe below is per-assistant: HTTP stays
        // the fast path for a child that can reach it, and a child that cannot is told what
        // actually works instead of being left to discover the dead network by trying.
        let sandboxed = task.assistant == .codex
        let progressFile = """
        ```json
        {"task_secret": "<the TASK_SECRET value from your first message>",
         "note": "<one sentence, at most \(progressLimit) characters>"}
        ```
        """
        let timelySection = sandboxed
            ? """
              ## Notifications cannot leave your sandbox

              Other briefings carry a push-notification recipe here; it is a loopback HTTP call
              your sandbox cannot make, so it is not in yours. Anything the user needs to know
              mid-flight goes in `progress.json` below, and the answer itself in `result.json`
              — both are collected and read, so nothing timely is lost by not pushing.
              """
            : """
              ## Up to 5 timely notifications, when the user is waiting

              You may use your own TASK_SECRET to push one sentence the user needs to know now,
              before completion or for 60 seconds afterwards:

              ```bash
              curl --fail-with-body -sS -X POST http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks/\(task.id)/notify \\
                -H "X-Clawdline-Task-Secret: <TASK_SECRET>" \\
                -H 'Content-Type: application/json' \\
                -d '{"title":"<at most 80 characters>","body":"<at most 500 characters>"}'
              ```

              **`--fail-with-body` is not decoration, and it is on every command in this
              briefing.** Plain `curl` exits 0 whatever the server says: a `401` from a stale
              secret and a `200` are the same exit status, so a request that was refused reads
              exactly like one that arrived. With the flag the server's typed error still
              prints and the command exits non-zero — measured on this Mac, a wrong task secret
              answers `403 forbidden` and exits 22. **Look at that status before you say you
              sent something.**

              The value of push is rarity. Routine results belong in `result.json`; notify only
              when the user is waiting for the answer, including a scheduled task such as
              today's weather whose useful output is the notification itself. Empty title/body
              values are refused. Each task may send at most 5 notifications, and this Mac
              accepts at most 30 per hour. The user may turn agent notifications off. A `409
              agent_notify_disabled` response is not your fault, and neither is any other
              refusal here: leave the content in `result.json`, report failure honestly, and do
              not retry.
              """
        let progressChannel = sandboxed
            ? """
              **Your sandbox has no network, so say it with a file.** A `curl` to 127.0.0.1
              from here exits 7 after 0 ms — DNS is off too, and no approval prompt will
              appear — so do not spend a turn discovering that. Write \(dir)/progress.json
              with your file-writing tool, replacing the whole file each time:

              \(progressFile)

              The broker collects it within seconds, the way it collects `result.json`; a
              half-written file simply fails to parse and is read again. Only the latest
              sentence in the file is collected — overwrite, do not append.
              """
            : """
              ```bash
              curl --fail-with-body -sS -X POST http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks/\(task.id)/progress \\
                -H "X-Clawdline-Task-Secret: <TASK_SECRET>" \\
                -H 'Content-Type: application/json' \\
                -d '{"note":"<one sentence, at most \(progressLimit) characters>"}'
              ```

              **A non-zero exit means the note was not recorded.** `--fail-with-body` prints
              what the server said and then fails; without it a `401` or a `404` exits 0 and
              you carry on believing the note is on somebody's screen. A refusal here is
              usually the secret: it is the TASK_SECRET from your first message and nothing
              else.

              If that `curl` cannot connect — some sandboxes have no loopback — or it keeps
              being refused, do not keep retrying it: write \(dir)/progress.json with your
              file-writing tool instead, replacing the whole file each time,

              \(progressFile)

              and the broker collects it the way it collects `result.json`.
              """
        let inflightSection = sandboxed
            ? """
              ## Before you start work you believe is new

              Another session's isolated checkout is invisible from the shared tree: a finished
              delivery sitting on a branch nobody has merged shows up in no `git status`, no
              `git diff` and no file listing. So "nothing here does that yet" is not evidence.
              Other briefings carry a live self-check against the broker's task list; your
              sandbox cannot reach it, so what you have instead is the plan above, when the
              dispatcher wrote one, and `task.json`. If part of your task looks like it may
              already be somebody else's work, write that suspicion into `progress.json` now
              and into your summary rather than silently building it twice — whoever reads
              your result can see the whole board and settle it.
              """
            : """
              ## Before you start work you believe is new, look

              Another session's isolated checkout is invisible from the shared tree: a finished
              delivery sitting on a branch nobody has merged shows up in no `git status`, no
              `git diff` and no file listing. So "nothing here does that yet" is not evidence.
              This is:

              ```bash
              curl --fail-with-body -sS http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks/\(task.id)/inflight \\
                -H "X-Clawdline-Task-Secret: <TASK_SECRET>"
              ```

              Every line of work outstanding in this repository: what it is, who has it, what
              state it is in, what files it claimed, and for isolated ones the branch and head
              where its code actually lives. Read it before you build something you think
              nobody has built. If a row looks like your job, say so in your result rather
              than doing it twice.

              **If this one fails you have no answer, which is not the same as an empty
              board.** Without the flag a refusal prints nothing and exits 0, and nothing on
              the screen is exactly what "no other work" looks like. So on a non-zero exit —
              and on an empty list, which is only what the broker knows about this repository
              at this moment — say in your result what you checked and what it told you,
              rather than reporting the ground as clear.
              """
        let announceSection = sandboxed
            ? """
              There is no completion announcement to attempt from your sandbox: the file alone
              is the completion signal, and it has always been enough.
              """
            : """
              Optionally, if outbound network is permitted in your sandbox, you may ALSO announce it:
              `curl --fail-with-body -sS -X POST http://127.0.0.1:\(Config.shared.remotePort)/v1/orchestrator/tasks/\(task.id)/complete \\
                 -H "X-Clawdline-Task-Secret: <TASK_SECRET>" -H 'Content-Type: application/json' \\
                 -d '{"status":"success","summary":"..."}'`
              This is never required; the file alone is enough — so when this call fails, the
              work is already reported and there is nothing to repair.
              """
        let reviewReporting = typedReviewReporting(for: task)
        return """
        # Clawdline child briefing — task \(task.id)

        You are a CHILD session working for a Clawdline root session. Your one job is the task
        described in \(dir)/task.json — read that file now.
        \(planningSection(for: task))\(attachedSection)
        ## Language, and the first thing you say

        The person watching this terminal reads \(languageName). Everything you say in this
        session, and the "summary" you write into result.json, is in that language — this
        briefing is in English only so that every assistant reads it the same way.

        Before you read task.json or touch anything, say exactly this line, on its own:

        \(L.t.childAnnounce(task.title))

        Then, once you have read task.json, one more line in the same language saying in your
        own words what you are about to do and where the output will go.

        ## Rules

        \(workspaceRule)
        - Put heavyweight temporary work (repo copies, build outputs, mutation worktrees and
          compiler indexes) in \(dir)/work/, not in the assistant scratchpad. Everything there
          is deleted when the task ends — immediately on success, after the configured grace
          period otherwise — so copy any log or diff worth keeping into `artifacts/` **before**
          writing `result.json`.
        - \(handOnRule)
        - Do not read any directory under /tmp/.clawdline/ except your own and any your
          instructions name explicitly. That second one is how a reviewing node works: it is sent
          to read what other nodes produced, so its instructions list those paths.
        - Landing records belong to the root after delivery; by protocol convention, a child does
          not call its task's `/landing` route itself even though it holds that task's secret.
        - Do not do work the task did not ask for.
        - You have \(task.timeoutMinutes) minutes before the task is marked timed out.\(isolationSection)\(houseRules)

        ## Verification budget

        `./build.sh` is forbidden. Do not use an app restart or clicking the real UI as acceptance,
        re-run a full suite as a ritual after every small edit, run a suite unrelated to the paths this task claimed,
        or repeat a run whose only purpose is to see whether something is flaky.

        Do one verification that actually proves the change: compile, run the tests covering the
        paths this task touched, and see one red-before-green run for every test you add. Iterating
        until the change first compiles and passes is ordinary work. Until the repository ships a
        focused Swift runner, an implementer whose behavior cannot be exercised more narrowly may
        use one full-suite run and record `focused_runner_unavailable`; a reviewer does not repeat
        it.

        **An expensive compile goes through the machine lock, and there is exactly one slot.**
        Four `swift-frontend` processes have force-rebooted this Mac. `./test.sh` and `./build.sh`
        take the slot for themselves, so ordinarily you do nothing but run them: a run that waits
        is queueing, not stuck, and it prints who holds the slot and for how long.

        **Do not start `swiftc` by hand and do not work around a wait.** Compiling outside the slot
        is the thing that rebooted the machine. If your task genuinely cannot be verified any other
        way, say so in `result.json` rather than compiling around the queue — a blocked verification
        reported honestly costs this machine nothing, and a second compiler costs it everything.
        `CLAWDLINE_SUITE_JOBS=<n>` is the supported way to ask `./test.sh` for fewer compiler jobs.

        Nothing in this system will ever end somebody else's compile, and neither may you.

        Verification stops after one third of this task's timeout
        (\(verificationMinutes) minutes). At the limit, stop and report the state reached in `result.json`. Point
        verification's private `TMPDIR` at `\(dir)/work/tmp`; the repository's
        snapshot recipe remains unchanged, and its test binary is then reclaimed with the task.

        \(timelySection)

        ## Say what you are doing — once at the start, and again when it changes

        **Send the first one within about three minutes of starting**, before you begin the work
        rather than during it: one sentence saying what you have decided to do now that you have
        read this file and `task.json`. It is the only thing that lets a wrong direction be
        cancelled at minute three instead of minute twenty-six — the two most expensive cancelled
        tasks on this Mac burned 18.5M and 16.5M tokens before anybody could tell what they had
        set off to do. Nobody can read your screen; this note is the whole of what they have.

        After that, your title was fixed before you started. When what you are actually doing
        stops matching it — you decide to rewrite the fixture too, the real problem turns out to
        be somewhere else, you have moved on to the second half — say so the same way:

        \(progressChannel)

        **This is not a status report and nobody is waiting to read it.** It is one sentence, it
        costs you a second, and it is what another session sees when it asks whether the thing it
        is about to start is already being done. The newest \(progressKept) are kept; sending the
        same sentence twice is ignored rather than refused.

        \(inflightSection)

        ## Reporting — this is the completion signal, do it exactly

        When the work is done (or has failed for good), write \(dir)/result.json:

        ```json
        {"clawdline_protocol": 1,
         "task_id": "\(task.id)",
         "task_secret": "<the TASK_SECRET value from your first message>",
         "status": "success",
         "summary": "<one paragraph: what you did, or why it failed>",
         "symbols": ["<every name your change introduced>", "..."],
         "artifacts": ["artifacts/<file>", "..."],
         "verification": {"runs": 2, "seconds": 940, "last": "pass", "scope": "swift suite + web-schedules"},
         "finished_at": "<ISO8601 UTC>"}
        ```

        Use "status": "failure" when you could not do it. Write it LAST — the moment it exists
        your work is considered finished.
        \(reviewReporting)

        **`symbols` is how your work is told apart from everybody else's.** This tree is shared:
        by the time root commits, the files you edited may hold two or three sessions' unfinished
        work, and root separates them by looking for vocabulary. Guessing that vocabulary is
        error-prone — root has staged trees that would not compile because a hunk *reading* like
        yours actually called somebody else's new function. So list what you introduced: new
        functions and types, new fields, new string keys, the names of test groups you added.
        Names, not descriptions. A wrong or missing list costs somebody an hour; it costs you a
        minute.

        **If you gave part of this to your own subagents and it did not come back, say so in the
        summary.** Doing it yourself instead is usually right — the answer is what was asked for,
        not who produced it. What is not right is a summary that reads as though those subagents
        did the work when they never finished. Whoever reads this is deciding how much to trust
        the result, and "both halves came back" and "both halves failed and I did it myself" are
        different amounts of evidence behind the same answer.

        **Write it with your file-writing tool, not with a shell command.** A shell line that
        builds JSON and moves it into place gets refused by command screening on its own shape —
        quotes inside braces, a redirect it cannot analyse statically — and that refusal is a
        prompt with no "always allow" on a tab nobody is watching. Atomicity is not yours to
        arrange: a half-written file simply fails to parse and is read again a few seconds later.

        \(announceSection)
        """
    }
}
