import { MOCK_DOOR, MOCK_FLAKY, MOCK_WRITE, params } from "../core/env.js";
import { ASK_MARK, uuid } from "../core/util.js";
import { handlers } from "./handlers.js";
import { Door } from "../door/door.js";

/* ---- fixtures ------------------------------------------------------------
   Enough of a machine to see every state, every animation and the reconnect —
   from a file:// copy with nothing running. It is also how this page was
   checked, which is the actual reason it exists.
   -------------------------------------------------------------------------- */

export var Mock = (function () {
    var C = "#d97757", O = "#141416", BG = "#33201a";          // the clawdline mark
    var W = "#eef6f4", TEAL = "#2f6b5e";                        // atrium
    var BODY = "#5aa6d8", LIMB = "#2f6b95";                     // a generated creature

    function art(rows, palette, bg) {
        return rows.map(function (row) {
            return row.split("").map(function (ch) { return ch === "." ? bg : (palette[ch] || bg); });
        });
    }

    var clawdline = {
        accent: C,
        cells: art([".######.", ".#o##o#.", "########", ".##..##."], { "#": C, "o": O }, BG)
    };
    var atrium = {
        accent: "#5cbba1",
        cells: art([".WWWWW.", ".W...W.", ".W.W.W.", ".W...W."], { "W": W }, TEAL)
    };
    var creature = {
        accent: BODY,
        cells: [[LIMB, null, LIMB, null, LIMB],
                [BODY, BODY, BODY, BODY, BODY],
                [BODY, null, BODY, null, BODY],
                [LIMB, null, LIMB, null, LIMB]]
    };

    var now = Math.floor(Date.now() / 1000);
    var sessions = [
        // Working, with three agents out — the case the terminal cannot show at all, and the
        // reason `Subagents` exists. One of them has just landed and is saying what it found.
        { id: "8F3A-1C", backend: "iterm", tty: "ttys004", label: "investigate the webhook",
          cwd: "/Users/x/code/clawdline", state: "working", work_state: "working",
          line: "Gallivanting… (2m 4s · ↓ 6.4k tokens)",
          isClaude: true, assistant: "claude", sessionId: "a2937509-a3d4-4c31-87a7-cdb7ff073d38", icon: clawdline,
          agents: [
              { id: "a1", what: "Search the delivery logs", type: "general-purpose",
                state: "running", depth: 1, at: now - 8, doing: "Grep: retry_after" },
              { id: "a2", what: "Read the signing middleware", type: "general-purpose",
                state: "running", depth: 1, at: now - 31, doing: "Read: app/webhooks/verify.rb" },
              { id: "a3", what: "Check the queue depth", type: "general-purpose",
                state: "done", depth: 1, at: now - 12,
                result: "Depth is flat at 0 — nothing is backing up.",
                tokens: 18420, tools: 5, seconds: 44.2 }
          ] },
        // Deliberately second in source order: the authenticated optional role, not fixture
        // placement or the word in its title, is what pins this row above every ordinary one.
        { id: "CF00-01", backend: "iterm", tty: "ttys001",
          label: "Clawdfather · machine coordinator", cwd: "/Users/x/code/clawdline",
          state: "working", work_state: "working", line: "Watching Bearings…",
          isClaude: false, assistant: "codex", sessionId: "clawdfather-mock", icon: clawdline,
          coordinator: {
              label: "Clawdfather", status: "online",
              // The advertisement the Mac sends today: four connected reads, one confirmed
              // user-attributed audit send, and closed reasons for the remaining disabled work.
              commands: [
                  { type: "status_report", enabled: true,
                    token_effort: "low", token_effort_basis: "registry_read" },
                  { type: "duplicates_conflicts_ownership", enabled: true,
                    token_effort: "low", token_effort_basis: "registry_read" },
                  { type: "landing_closure", enabled: true,
                    token_effort: "low", token_effort_basis: "registry_read" },
                  { type: "scope_permissions", enabled: true,
                    token_effort: "low", token_effort_basis: "registry_read" },
                  { type: "since_away", enabled: false, reason: "no_return_ledger",
                    token_effort: "unknown", token_effort_basis: "unbuilt",
                    why: "This Mac does not record a return point yet, so there is nothing to read one against." },
                  { type: "coordinate_work", enabled: false, reason: "no_command_route",
                    token_effort: "unknown", token_effort_basis: "unbuilt",
                    why: "No route carries a command from this panel into a session yet, so nothing can be sent." },
                  { type: "dispatch_independent_work", enabled: false,
                    reason: "device_cannot_spawn",
                    token_effort: "high", token_effort_basis: "spawns_session",
                    why: "A paired device can never start a session — that separation is deliberate, and this command will not cross it." },
                  { type: "ask_coordinator", enabled: false, reason: "no_command_route",
                    token_effort: "medium", token_effort_basis: "single_session_message",
                    why: "No route carries a command from this panel into a session yet, so nothing can be sent." },
                  { type: "deep_status_audit", enabled: true,
                    token_effort: "high", token_effort_basis: "session_fanout" },
                  { type: "quiet_watch", enabled: false, reason: "no_command_route",
                    token_effort: "unknown", token_effort_basis: "unbuilt",
                    why: "No route carries a command from this panel into a session yet, so nothing can be sent." },
                  { type: "stop", enabled: false, reason: "no_command_route",
                    token_effort: "low", token_effort_basis: "broker_only",
                    why: "No route carries a command from this panel into a session yet, so nothing can be sent." },
                  { type: "reconnect", enabled: false, reason: "machine_token_only",
                    token_effort: "low", token_effort_basis: "broker_only",
                    why: "Reconnecting needs the Mac's own orchestrator token, which a paired device deliberately does not hold." }
              ]
          } },
        // Waiting, **with the question in it**. This is what the phone could never see: the
        // options were parsed on the Mac and thrown away, so the box could only say "go and
        // find the Mac". The caret is on the second row, which is what a bare Return confirms.
        { id: "2C71-90", backend: "iterm", tty: "ttys011", label: "the signup flow keeps 500ing",
          cwd: "/Users/x/code/atrium", state: "waiting", work_state: "waiting_you", line: null,
          isClaude: true, assistant: "claude", sessionId: null, icon: atrium,
          menu: { selected: 2, options: [
              { n: 1, label: "Yes", selected: false, can: true },
              { n: 2, label: "Yes, and don't ask again for rails commands in atrium",
                selected: true, can: true },
              { n: 3, label: "No, tell Claude what to do instead", selected: false, can: true }
          ] } },
        // Waiting on a question whose options each carry a paragraph. This is the shape that
        // showed the card had no ceiling: it grew past the top of the screen and past the
        // composer, so the conversation you were deciding about and the box you would answer in
        // were both pushed out of reach. The card is capped and scrolls inside itself now, and it
        // folds; this fixture is what proves both, because a short menu never could.
        { id: "9C1D-42", backend: "iterm", tty: "ttys044", label: "which source to drop",
          cwd: "/Users/x/code/atrium", state: "waiting", work_state: "waiting_you", line: null,
          isClaude: true, assistant: "claude", sessionId: null, icon: atrium,
          menu: { selected: 1, question: "The rate limit bites on every backfill. Which way out?", options: [
              { n: 1, label: "Swap to the official feed", selected: true, can: true,
                detail: "Three of the eleven bindings are primary and the rest are a fallback chain, so this is not one move but three — and the official feed spells two of the columns differently, which is the part that has bitten us before. Slowest to land, cheapest to keep." },
              { n: 2, label: "Pay for the higher tier", selected: false, can: true,
                detail: "Nothing in the code changes and the backfill finishes tonight. It is a standing cost against a source we have already said we want to leave, and it does not answer the licensing question underneath any of this." },
              { n: 3, label: "Keep the fallback for now", selected: false, can: true,
                detail: "Daily syncing is unaffected either way; only the history backfill is slow. Doing nothing this week costs nothing this week, and the decision comes back the next time somebody asks for a five-year chart." },
              { n: 4, label: "Type something.", selected: false, can: true },
              { n: 5, label: "Chat about this", selected: false, can: true }
          ] } },
        // Waiting on a **multi-select**, which is a different question from the one above: its
        // rows toggle rather than answer, and nothing is sent until the button under them is
        // pressed. Kept as a fixture because that button was once read as the last row's
        // description, and there was nothing on this page to press.
        { id: "5B0E-11", backend: "iterm", tty: "ttys012", label: "which sources to drop",
          cwd: "/Users/x/code/atrium", state: "waiting", work_state: "waiting_you", line: null,
          isClaude: true, assistant: "claude", sessionId: null, icon: atrium,
          menu: { selected: 1, question: "Which of these should the report drop?",
                  submit: { label: "Submit", selected: false }, options: [
              { n: 1, label: "the rate limit", checked: false, selected: true, can: true,
                detail: "Measured at about 200 requests an hour per token." },
              { n: 2, label: "the licence", checked: true, selected: false, can: true },
              { n: 3, label: "the cost", checked: false, selected: false, can: true },
              { n: 4, label: "Type something", checked: false, selected: false, can: true }
          ] } },
        // Idle, **and not finished** — the turn ended an hour ago and the build it started is
        // still going. This is the row that said nothing at all before `Shells`: the terminal
        // mentions it once, where the turn ended, and every list after that drew it as done.
        { id: "9B04-2D", backend: "iterm", tty: "ttys002", label: "rewrite the CSV importer",
          cwd: "/Users/x/code/notebook", state: "idle", work_state: "unknown", line: null,
          // A debt is an obligation, so this quiet row is not closeable and says who moves it.
          closeability: { state: "blocked", observed_at: now, session_generation: 41,
                          activity_generation: 9, obligation_generation: 87,
                          version: "cl1_a1b2c3d4e5f60718293a4b5c6d7e8f90",
                          provenance: ["broker"], attestation_id: null,
                          reasons: [{ code: "owed_decision", kind: "obligation",
                                      subject_kind: "session", subject_id: "9B04-2D",
                                      mover: { kind: "person", person_needed: true } }],
                          mover: { kind: "person", person_needed: true },
                          source: { provenance: "session_watch", freshness: "current" } },
          isClaude: true, assistant: "claude", sessionId: null, icon: creature,
          // The second axis riding on a quiet row: the session still gets on with its build,
          // and the reader still owes it a call — three days old, which is the point.
          owed: { note: "which CSV dialect wins is still your call", since: now - 3 * 86400,
                  person_needed: true, provenance: "self" },
          shells: [
              { id: "bvlp3xmku", at: now - 6, command: "cargo build --release 2>&1 | tail -40",
                what: "Build the importer with the new row parser",
                doing: "[214/318] Compiling importer/rows.rs" }
          ] },
        // The two declared quiet states: 🔜 holding moves by itself and nobody is needed; 📭
        // ready is an invitation to hand the session work. Both carry `self` provenance, so the
        // row can say stated-not-proven out loud.
        { id: "B770-3A", backend: "iterm", tty: "ttys031", label: "wait out the release build",
          cwd: "/Users/x/code/notebook", state: "idle", work_state: "holding",
          // Nothing the broker can see is outstanding, and that is still not permission: only
          // this session knows whether it owns shared-tree bytes nobody wrote down.
          closeability: { state: "needs_attestation", observed_at: now, session_generation: 41,
                          activity_generation: 4, obligation_generation: 87,
                          version: "cl1_0c1d2e3f405162738495a6b7c8d9e0f1",
                          provenance: ["broker"], attestation_id: null,
                          reasons: [{ code: "attestation_missing", kind: "attestation",
                                      subject_kind: "session", subject_id: "B770-3A",
                                      mover: { kind: "session", "self": true,
                                               person_needed: false } }],
                          mover: { kind: "session", "self": true, person_needed: false },
                          source: { provenance: "session_watch", freshness: "current" } },
          work_provenance: "self", work_note: "resumes when the release build finishes",
          work_moved_by: "the release build", work_person_needed: false, work_since: now - 1200,
          line: null, isClaude: true, assistant: "claude", sessionId: null, icon: creature },
        { id: "B771-4B", backend: "iterm", tty: "ttys032", label: "free hand",
          cwd: "/Users/x/code/notebook", state: "idle", work_state: "ready",
          work_provenance: "self", work_note: "RootSession fix landed; can take new work",
          work_since: now - 300,
          // `ready` and `safe` on one row, which is the pair most worth being able to see:
          // they agree here and they are still two different questions.
          closeability: { state: "safe", reasons: [], observed_at: now, session_generation: 41,
                          activity_generation: 12, obligation_generation: 87,
                          version: "cl1_2f9a4c31d0be5a7788c1e6b04d3f9021",
                          provenance: ["broker", "self"],
                          attestation_id: "6f0b2d1e-9a44-4c2c-b0d5-1f8a3c7e5d20",
                          mover: null, source: { provenance: "session_watch",
                                                 freshness: "current" } },
          line: null, isClaude: true, assistant: "claude", sessionId: null, icon: creature },
        // The **owner** of the wait the row below is stuck on, and the reason this fixture is
        // here: an owner's row used to be drawn exactly like a session in no relationship at
        // all, so the one person who can end the wait was the one person not told about it.
        // Idle, quiet, and still not safe to close.
        { id: "A15E-77", backend: "tmux", tty: "tmux:%12", label: "docs pass before the release",
          cwd: "/Users/x/code/clawdline/docs", state: "idle", work_state: "waiting_session", line: null,
          isClaude: true, assistant: "claude", sessionId: null, icon: clawdline,
          coordination: { state: "has_waiters", waitingOn: [], waitedOnBy: [{
              id: "0ae8b6e7-83b5-4bcd-a61c-776f56924e15",
              repository: "/Users/x/code/clawdline",
              paths: ["artifacts/2026-08-26-clawdline-communication-protocol.html"],
              ownerSessionId: "A15E-77",
              waiterSessionId: "7A19-42", waiterLabel: "update the communication Artifact",
              reason: "the same Artifact file",
              releaseCondition: "the protocol docs are committed and released"
          }] } },
        // Idle because its own turn ended, but not safe to close: Clawdline still owns a durable
        // relationship saying which peer must release which path. This is deliberately separate
        // from `state: "waiting"`, whose orange row means a person has to answer.
        { id: "7A19-42", backend: "iterm", tty: "ttys015",
          label: "update the communication Artifact",
          cwd: "/Users/x/code/clawdline", state: "idle", work_state: "waiting_session", line: null,
          isClaude: false, assistant: "codex", sessionId: null, icon: clawdline,
          coordination: { state: "waiting_on_session", waitedOnBy: [], waitingOn: [{
              id: "0ae8b6e7-83b5-4bcd-a61c-776f56924e15",
              repository: "/Users/x/code/clawdline",
              paths: ["artifacts/2026-08-26-clawdline-communication-protocol.html"],
              ownerSessionId: "A15E-77", ownerLabel: "docs pass before the release",
              releaseCondition: "the protocol docs are committed and released"
          }] } },
        { id: "44D2-05", backend: "iterm", tty: "ttys017", label: "scratch",
          cwd: "/Users/x/tmp/notes", state: "idle", work_state: "unknown", line: null,
          isClaude: false, assistant: "codex", sessionId: null, icon: null },
        { id: "C0FF-3E", backend: "iterm", tty: "ttys021", label: "build box over ssh",
          cwd: "/Users/x", state: "unknown", work_state: "unknown", line: null,
          isClaude: true, assistant: "claude", sessionId: null, icon: null },
        { id: "5E20-8B", backend: "tmux", tty: "tmux:%14", label: "check the German strings",
          cwd: "/Users/x/code/clawdline", state: "working", work_state: "working",
          line: "Reading Copy+German.swift",
          isClaude: true, assistant: "claude", sessionId: null, icon: clawdline },
        // The two receipt glyphs side by side in the mock: task delivery is not graph closure;
        // the double check is the narrower broker-verified target landing for that task scope.
        { id: "D311-01", backend: "iterm", tty: "ttys023", label: "review delivered",
          cwd: "/Users/x/code/clawdline", state: "idle", work_state: "milestone_complete",
          disposition: { scope: "task", taskId: "mock-milestone", title: "review delivered",
                         evidence: "authenticated_task_delivery", receiptAt: now - 30 },
          // Delivery and closeability are intentionally both present: this is the row that
          // proves the receipt stays whole before the independent key, and the info card can
          // explain why a delivered session still needs its own close attestation.
          closeability: { state: "needs_attestation", observed_at: now, session_generation: 42,
                          activity_generation: 5, obligation_generation: 88,
                          version: "cl1_1d2e3f405162738495a6b7c8d9e0f102",
                          provenance: ["broker"], attestation_id: null,
                          reasons: [{ code: "attestation_missing", kind: "attestation",
                                      subject_kind: "session", subject_id: "D311-01",
                                      mover: { kind: "session", "self": true,
                                               person_needed: false } }],
                          mover: { kind: "session", "self": true, person_needed: false },
                          source: { provenance: "session_watch", freshness: "current" } },
          isClaude: true, assistant: "claude", sessionId: null, icon: clawdline },
        { id: "D322-02", backend: "iterm", tty: "ttys024", label: "landed delivery",
          cwd: "/Users/x/code/clawdline", state: "idle", work_state: "work_complete",
          disposition: { scope: "task", taskId: "mock-closure", title: "landed delivery",
                         evidence: "broker_verified_target_landing", receiptAt: now - 90,
                         landedAt: now - 10, commit: "abc123", target: "main",
                         targetCommit: "def456" },
          isClaude: false, assistant: "codex", sessionId: null, icon: clawdline }
    ];

    // What the top session sent away. The first two hang off `8F3A-1C`, and their children are
    // rows from the list above — which is the only way to see the grouping work: the codex row
    // is near the bottom on its own merits and belongs directly under its root.
    //
    // One of each kind that matters. The codex one is still going and has no cost to show —
    // that plan is not billed per token — and the Claude one has landed, which is what puts a
    // figure in the header. A task that finished ten minutes ago stops moving its row; this one
    // is three minutes old, so it is still drawn under the session that asked for it.
    //
    // The third is the second level: a task the *child* `A15E-77` dispatched, indented twice and
    // still running under a parent that has already reported. It names its parent by `taskId`
    // as well as by session, which is the pair the app matches on — and it is the case worth
    // having in the mock, because a grandchild outliving its parent is the one that decides
    // whether a row is drawn under something or floats free.
    var tasks = [
        { id: "6f1b3d84-2a17-4c95-91ce-70b5a4e2d011", state: "briefed", kind: "image",
          title: "Draw the project portrait", assistant: "codex", projectDir: "/Users/x/tmp/notes",
          created: now - 260, spawnedAt: now - 254, briefedAt: now - 248, finishedAt: null,
          depth: 1,
          dir: "/tmp/.clawdline/6f1b3d84-2a17-4c95-91ce-70b5a4e2d011",
          root: { sessionId: "a2937509-a3d4-4c31-87a7-cdb7ff073d38",
                  label: "investigate the webhook", terminalId: "8F3A-1C" },
          child: { terminalId: "44D2-05", backend: "iterm", sessionId: null },
          artifacts: [],
          usage: { input: 21400, output: 3100, cacheRead: 88000, cacheWrite: 1900,
                   total: 114400, model: "gpt-5.1-codex", costUsd: null } },
        { id: "b70e51c9-84af-4d2e-a6d1-1c2f9e330447", state: "success", kind: "code-review",
          title: "Read the docs pass for holes", assistant: "claude",
          projectDir: "/Users/x/code/clawdline/docs",
          created: now - 900, spawnedAt: now - 894, briefedAt: now - 888, finishedAt: now - 180,
          depth: 1,
          dir: "/tmp/.clawdline/b70e51c9-84af-4d2e-a6d1-1c2f9e330447",
          root: { sessionId: "a2937509-a3d4-4c31-87a7-cdb7ff073d38",
                  label: "investigate the webhook", terminalId: "8F3A-1C" },
          child: { terminalId: "A15E-77", backend: "tmux", sessionId: "1f0c8a44-9d21-4b6e-8f30-2ab7c5e91d62" },
          summary: "Three sentences say where the file goes and none of them agree.",
          artifacts: ["artifacts/notes.md"],
          usage: { input: 9600, output: 4200, cacheRead: 61000, cacheWrite: 800,
                   total: 75600, model: "claude-sonnet-4-5", costUsd: 0.0913 } },
        { id: "c9d47a30-51be-4f88-b0a2-3e6d81c4f592", state: "briefed", kind: "review",
          title: "Check the German strings against the English", assistant: "claude",
          projectDir: "/Users/x/code/clawdline",
          created: now - 700, spawnedAt: now - 694, briefedAt: now - 688, finishedAt: null,
          depth: 2,
          dir: "/tmp/.clawdline/c9d47a30-51be-4f88-b0a2-3e6d81c4f592",
          root: { sessionId: "1f0c8a44-9d21-4b6e-8f30-2ab7c5e91d62",
                  label: "docs pass before the release", terminalId: "A15E-77",
                  taskId: "b70e51c9-84af-4d2e-a6d1-1c2f9e330447" },
          child: { terminalId: "5E20-8B", backend: "tmux", sessionId: null },
          artifacts: [],
          usage: { input: 3100, output: 900, cacheRead: 24000, cacheWrite: 400,
                   total: 28400, model: "claude-haiku-4-5", costUsd: 0.0094 } }
    ];

    // The line that cannot wrap, five hundred characters of it, because this is the shape that
    // broke the page and there is no waiting around for a real session to print another one. A
    // phone showed it as a black screen with one strip of garbage floating in the middle: nothing
    // in the string could break, so it stretched the transcript column past the edge of the
    // display and left every readable line stranded off to one side. Escape codes were the first
    // producer of these and are now stripped upstream; a base64 blob or a long path is the same
    // string with a different story, so the fixture keeps one.
    var unbreakable = "aGVsbG8td29ybGQtdGhpcy1pcy1vbmUtdmVyeS1sb25nLXRva2Vu".repeat(10);

    // One session gets a real transcript: twenty-odd entries, prose and tools mixed, with
    // several runs of tool calls back to back — which is what the folding has to hold up
    // under, and the reason the fixture is worth having.
    var script = [
        ["user", "the webhook from stripe is landing twice on staging. same event id, two rows.", null],
        ["assistant", "Two rows for one `event.id` means either the handler is not idempotent or it is being invoked twice. Let me look at where the row is written before guessing.", null],
        ["tool", "Sources/Webhook.swift, Sources/Ledger.swift", "Grep"],
        ["tool", "", "Read"],
        ["assistant", "The insert is unconditional:\n\n```swift\ntry db.insert(Payment(eventID: event.id, amount: event.amount))\n```\n\nNothing consults `eventID` first, so a redelivery writes a second row. Stripe redelivers whenever our 200 arrives late.", null],
        ["user", "how late is late", null],
        ["assistant", "Stripe waits 30 seconds for the ack. Anything slower is treated as a failure and queued for retry — the work still finished on our side, which is exactly how you get one event and two rows.", null],
        ["tool", "grep -n \"await\" Sources/Webhook.swift", "Bash"],
        ["assistant", "The handler does the ledger write, then sends the receipt email, then answers. The email is the slow part and it is on the wrong side of the reply.", null],
        ["user", "so ack first, then work?", null],
        ["assistant", "That is half of it. Acking first stops the redelivery, but a crash between the ack and the write would then lose the payment silently — which is a worse bug than the one being fixed. Both halves: a unique index on `event_id` so a second write cannot land, and the email moved off the request.", null],
        ["tool", "", "Edit"],
        ["tool", "Sources/Ledger.swift", "Edit"],
        ["assistant", "Added the unique index and made the insert an upsert on `event_id`. The handler now answers as soon as the row is durable.", null],
        ["tool", "swift test --filter WebhookTests", "Bash"],
        ["tool", "", "Read"],
        ["assistant", "Eleven tests, all passing, including a new one that posts the same event twice and asserts one row.", null],
        ["user", "add one for the crash-between case", null],
        ["assistant", "Done — it kills the process after the insert and before the email, restarts, and checks that the receipt is still sent from the queue rather than lost.", null],
        ["tool", "swift test", "Bash"],
        ["assistant", "Green. The migration is `0009_event_id_unique.sql`; it will need running on staging before the next deploy or the index will not be there to protect anything.", null],
        // On its own between two answers, so it is never folded away: the point of it is to be
        // on screen. Both halves of the problem are here — the result below, and the same run of
        // characters inside a sentence, which is where a wrapping rule that only covers `.body`
        // and forgets its children falls over.
        ["tool", "curl -s /v1/artifacts/" + unbreakable, "Bash"],
        ["assistant", "The upload came back as `" + unbreakable + "` — one token, no spaces, and nowhere for a line to break.", null],
        ["peer", "I have released `Sources/Webhook.swift`; the migration path is yours. I will keep working in `Sources/Ledger.swift`.", null, "payments-ops-12"],
        ["user", "summarise what changed", null],
        // Everything the renderer knows how to draw, in one answer, so the fixture is also the
        // test: a table with alignment markers, a heading, both kinds of list, a quote, a rule.
        ["assistant", "## What changed\n\n| file | change | lines |\n|---|:---:|---:|\n| `Sources/Webhook.swift` | ack before the email | **+18 −24** |\n| `Sources/Ledger.swift` | upsert on `event_id` | +7 −2 |\n| `0009_event_id_unique.sql` | new | +4 |\n\n### Still to do\n\n1. Run the migration on staging\n2. Watch the redelivery counter for a day\n   - it should fall to zero\n   - if it does not, the index did not take\n\n> The handler now answers as soon as the row is durable, and *not* before.\n\n**Run `0009_event_id_unique.sql` on staging first** — until it has run there is no index, and nothing stopping the second row.\n\n---\n\nSee [the Stripe note on retries](https://stripe.com/docs/webhooks) for where the 30 seconds comes from.", null]
    ];

    var transcripts = {};
    transcripts["8F3A-1C"] = script.map(function (e, i) {
        return { role: e[0], text: e[1], tool: e[2], source: e[3], at: now - (script.length - i) * 47 };
    });
    // A real Codex FileChange wire shape in the transcript used for visual development. Keeping
    // it here makes the red/green patch inspectable in mock mode without a live Codex session.
    Object.assign(transcripts["8F3A-1C"][11], {
        tool: "edit", text: "Sources/Webhook.swift",
        fileChanges: [{
            path: "/Users/x/code/clawdline/Sources/Webhook.swift", kind: "update",
            unifiedDiff: "@@ -42,4 +42,5 @@ func receive(_ event: Event) async throws {\n-    try await mail.send(receipt(for: event))\n-    return .ok\n+    try ledger.upsert(event)\n+    queue.enqueue(receipt(for: event))\n+    return .accepted\n }\n"
        }]
    });
    // A question, in the shape the wire carries one: the marker, then the questions as data.
    // See `ASK_MARK`. The fixture is also where the block's layout is worked on, so it has both
    // kinds — one answer wanted, and any number of them.
    function asked(questions, at) {
        return { role: "tool", tool: "AskUserQuestion", text: ASK_MARK + JSON.stringify(questions), at: at };
    }

    transcripts["2C71-90"] = [
        { role: "user", text: "run the migration on staging", tool: null, at: now - 220 },
        { role: "assistant", text: "This drops `payments_legacy`. Before I run it: the table still has 12,041 rows and nothing in the repo reads it.", tool: null, at: now - 180 },
        asked([{
            h: "Migration",
            q: "`0009_event_id_unique.sql` drops `payments_legacy`, which still holds 12,041 rows. How shall I run it?",
            o: [
                { l: "Copy the table first, then run it", d: "`payments_legacy_2026_08` beside it, then the migration. Nothing is lost and the copy can go next week." },
                { l: "Run it as it is", d: "The table goes. Nothing in the repository reads it, and staging has last night's dump behind it." },
                { l: "Stop here", d: "Leave staging as it is and I will write the copy step into the migration instead." }
            ]
        }], now - 170)
    ];
    transcripts["44D2-05"] = [
        { role: "user", text: "get the release notes ready", tool: null, at: now - 900 },
        asked([{
            h: "Release notes",
            m: true,
            q: "Which of these are worth a line in the notes?",
            o: [
                { l: "The webhook fix", d: "One event, one row. The one somebody wrote in about." },
                { l: "Faster session list", d: "A `ps` per comparison became a `ps` per file." },
                { l: "New icons", d: "Cosmetic, and nobody asked." }
            ]
        }], now - 880)
    ];
    transcripts["A15E-77"] = [
        { role: "user", text: "read docs/devstack.md and tell me what is missing", tool: null, at: now - 3600 },
        { role: "tool", text: "docs/devstack.md", tool: "Read", at: now - 3590 },
        { role: "assistant", text: "It explains the file and never says where to put it. Everything else assumes you already know that `devstack.json` sits at the root of the project it describes.", tool: null, at: now - 3580 },
        { role: "notice", text: "[clawdline] task 2ef96bc1 finished: success", at: now - 3570,
          notice: { kind: "task_finished", audience: "root", state: "success",
                    task: { id: "2ef96bc1-13ac-41c9-9cdb-b709b3b56d09", title: "Review <unsafe> & finish" },
                    result_path: "/tmp/.clawdline/2ef96bc1/result.json", outstanding: 0,
                    claims_released: false, child_may_still_write: false } },
        // A peer card beside the notice card, in one transcript, because that is the pair the
        // two features exist to keep apart: `peer` is what another session said to this one,
        // `notice` is what the app said to it about a task. Neither should look like the other.
        { role: "peer", text: "The release notes still say **August**. Fix that before you push.", tool: null,
          at: now - 3560, source: "release-room", sourceMode: "prompting" },
        { role: "message", text: "你那兩點我都收進去了。\n\n## 狀態\n\n`e23f626b` 還在跑。", tool: null,
          at: now - 3555, source: "clawdline-fa", sourceMode: "clawdline",
          sourceAssistant: "claude" },
        { role: "assistant", text: "Caught it — the heading was left over from the last cut. Changed to September and pushed.", tool: null, at: now - 3550 },
        { role: "notice", text: "[clawdline] workspace overlap: task 7d40aa19 (Release notes) and task 2ef96bc1 (Review) share /Users/you/code/clawdline/docs", at: now - 3540,
          notice: { kind: "workspace_overlap", audience: "root",
                    task: { id: "7d40aa19-6f2b-4e88-a1c3-0d5e91b7fa42", title: "Release notes" },
                    overlaps: [{ task: { id: "2ef96bc1-13ac-41c9-9cdb-b709b3b56d09", title: "Review <unsafe> & finish" },
                                 path: "/Users/you/code/clawdline/docs" }] } },
        { role: "notice", text: "[Clawdline file-wait] Repo: /Users/you/code/clawdline.", at: now - 3530,
          notice: { kind: "file_wait_request", audience: "owner", wait_id: "wait-1",
                    repository: "/Users/you/code/clawdline", paths: ["Sources/<unsafe>.swift"],
                    waiter_session_id: "WAIT-A", reason: "Review & land", release_condition: "commit or release" } },
        { role: "notice", text: "[Clawdline file-wait release] Repo: /Users/you/code/clawdline.", at: now - 3520,
          notice: { kind: "file_wait_release", audience: "waiter", wait_id: "wait-1",
                    repository: "/Users/you/code/clawdline", paths: ["Sources/<unsafe>.swift"],
                    commit: "abc<123>", note: "Rechecked & safe" } },
        { role: "notice", text: "[clawdline] handoff 7c1e9b02 picked up by codex", at: now - 3510,
          notice: { kind: "handoff_receipt", audience: "source", state: "picked_up",
                    handoff_id: "7c1e9b02-4d55-4a80-9c3e-1f6b2a09d431", title: "Cloud <plan>",
                    assistant: "codex", project_dir: "/tmp/<repo>" } },
        { role: "notice", text: "[clawdline] handoff 7c1e9b02 needs manual delivery", at: now - 3500,
          notice: { kind: "handoff_receipt", audience: "source", state: "first_line_failed",
                    handoff_id: "7c1e9b02-4d55-4a80-9c3e-1f6b2a09d431",
                    assistant: "claude", project_dir: "/tmp/repo" } }
    ];

    // Where a session may be started — the Mac's own list, which is not the session list and is
    // not the icon registry either: directories `claude` has been run in that are still there,
    // newest first. Ten of them, because that is enough to put the filter on screen.
    var places = [
        { id: "3b9e26c1587facfd", label: "clawdline", path: "/Users/you/code/clawdline", icon: clawdline },
        { id: "24f9bac626da56ea", label: "atrium", path: "/Users/you/code/atrium", icon: atrium },
        { id: "470885724e5330e1", label: "cairn", path: "/Users/you/code/cairn", icon: creature },
        { id: "9c1d4e77a0b3f215", label: "notebook", path: "/Users/you/code/notebook", icon: creature },
        { id: "b04f8a2c6d915e33", label: "astro", path: "/Users/you/code/astro", icon: null },
        { id: "5e7a1c93f2680b4d", label: "frontend", path: "/Users/you/code/cairn/frontend", icon: creature },
        { id: "1f6b0d38e9a742c5", label: "docs", path: "/Users/you/code/clawdline/docs", icon: clawdline },
        { id: "c83e5f10ab24d967", label: "sketches", path: "/Users/you/code/sketches", icon: null },
        { id: "7a2c9e46b1d05f38", label: "website", path: "/Users/you/code/website", icon: null },
        { id: "e51b7d02c4a86f19", label: "notes", path: "/Users/you/tmp/notes", icon: null }
    ];
    /* What one Project's worktrees look like once the ledger is joined at read time: only the
       ones carrying an accepted Feature head, which is why there are eight here and thirty-one
       more counted in `excluded` below. The proportions are the ones measured on the machine
       this was written on — delivered is the largest group, and it is the whole reason the page
       has a shape. Ids are the task UUIDs that name the checkouts; there is no path in this
       payload and no branch either, by design. */
    function feature(id, label, outcome, runs, first, last, work) {
        return { id: id, label: label, outcome: outcome, runs: runs, tasks: [], liveTasks: [],
                 taskStates: [], landingStates: [], storedLandingStates: [], landingBasis: "live",
                 work: work || null, firstSeenAt: first, lastSeenAt: last };
    }
    /* `label` is the work line a Feature was grouped by and `work` is what the task itself said
       it was doing: on the machine this fixture was written from, nine cards read `Clawdfather —
       handoff 18bde7c3` and none of them said what the work was. `needs` is the row's own next
       step while it sits in the delivered block. */
    function worktree(id, outcome, runs, label, first, last, states, landings, work, needs) {
        return { id: id, outcome: outcome, runs: runs, tasks: [id], liveTasks: [],
                 taskStates: states, landingStates: landings || [],
                 storedLandingStates: [], landingBasis: "live",
                 work: work || null, needs: needs || null,
                 firstSeenAt: first, lastSeenAt: last,
                 features: [feature("feature-" + id.slice(0, 8), label, outcome, runs, first, last,
                                    work)] };
    }
    var worktreesByPath = {
        "/Users/you/code/clawdline": [
            worktree("b1103ab1-6f2c-41d8-9a70-3e5c17d0ba49", "delivered", 2,
                     "Clawdfather: machine coordinator", "2026-09-01T09:08:09Z", "2026-09-01T09:13:12Z",
                     ["spawning", "success"], [],
                     "The succession a dead coordinator leaves behind", "land_or_abandon"),
            worktree("4d92c7e0-1b53-4a86-b2f1-7c08e5d41a63", "delivered", 5,
                     "The schedules page", "2026-08-28T02:41:00Z", "2026-08-29T18:02:44Z",
                     ["success"], [], null, "no_record"),
            worktree("7a15fe38-90c4-4d21-8e07-2b6491cf0d55", "delivered", 3,
                     "Push, and the one lever that reaches a stale page", "2026-08-24T11:20:05Z",
                     "2026-08-24T15:44:19Z", ["success"], ["pending"],
                     "One notification a stale page cannot swallow", "land_or_abandon"),
            worktree("2ef96bc1-13ac-41c9-9cdb-b709b3b56d09", "delivered", 1,
                     "Review the close confirmation", "2026-08-19T07:02:31Z", "2026-08-19T07:58:00Z",
                     ["failure"], ["abandoned"],
                     "Why the confirmation says what it says", "land_or_abandon"),
            worktree("5a3b90ff-2c41-4d7e-8b06-19ae5c7d3f22", "nothing_to_land", 2,
                     "Clawdfather: machine coordinator", "2026-08-31T04:20:00Z",
                     "2026-08-31T09:05:00Z", ["success"], ["nothing_to_land"],
                     "Independent review of the handoff sender contract"),
            worktree("9c077b24-67a1-4a93-ac34-40fee4c97851", "landed", 4,
                     "The sidebar, and which page", "2026-09-04T03:10:00Z", "2026-09-04T11:35:00Z",
                     ["success"], ["landed"]),
            worktree("f0eedc18-2a77-4b90-8c31-5d0ae6b2f947", "landed", 6,
                     "Usage Portfolio", "2026-08-11T05:00:00Z", "2026-08-13T21:30:00Z",
                     ["success"], ["landed"]),
            worktree("3f9a21bc-88d0-4e57-9b12-6ca4de70f381", "active", 1,
                     "The Projects page", "2026-09-04T11:50:54Z", "2026-09-04T12:04:00Z",
                     ["briefed"]),
            worktree("b57fc96f-4e10-42a3-95d8-0c1b7e6a2f84", "abandoned", 1,
                     "Read the delivery logs", "2026-07-30T22:14:00Z", "2026-07-30T22:41:00Z",
                     ["briefed"]),
            worktree("e4402d71-5c88-4b06-a3e9-71fd0b62c95a", "unknown", 1,
                     "Rewrite the README around what it is for", "2026-07-02T13:00:00Z",
                     "2026-07-02T13:26:00Z", [])
        ],
        "/Users/you/code/atrium": [
            worktree("c0aa5f92-7b31-4d68-8e02-45cb1d907e36", "landed", 2,
                     "The greenhouse view", "2026-08-02T08:00:00Z", "2026-08-02T19:12:00Z",
                     ["success"], ["landed"])
        ]
    };

    // What the selected assistant has already recorded in a place. Enough on the first that the
    // filter is on screen and there is something to type into it, one on the second so the
    // sheet's short case is reachable, and nothing at all on the rest. An empty answer is a
    // state the sheet has to say out loud rather than spin on.
    //
    // The first row is `live`: something is writing to that transcript right now. It is the one
    // row on this screen that must not read as ordinary, because resuming it would put a second
    // process on the same file.
    var past = {
        "3b9e26c1587facfd": [
            { id: "105344fb-c769-4b37-b766-403b410897eb", title: "Planner.swift and POST /v1/intents", live: true },
            { id: "bbf8dae0-2e51-4a7c-9d63-1c0f8b4a7e92", title: "Make dictation reusable outside the composer", live: false },
            { id: "7c12c5a4-60b6-4257-bf4e-bebaf6cc152b", title: "Resuming a recorded conversation", live: false },
            { id: "aeee9c51-33d7-4b18-8a20-6e5c9f01b7d4", title: "Session recovery after a reboot", live: false },
            { id: "91db707d-4c62-4e93-b5a1-2f7d80c6e315", title: "Park and unpark, and picking a background session back up", live: false },
            { id: "9a68386e-1b40-4fd2-88c7-3ea5d7f2b061", title: "Where the registry's consumers get it wrong", live: false },
            { id: "5bdc0c33-7a95-4c11-9e48-0db362f7a5c9", title: "One place to see every session at once", live: false },
            { id: "8e29b3df-0f74-4a86-93b2-c15e7d049a83", title: "Several requests arriving at once", live: false },
            { id: "1f47c762-60e9-4dc1-8967-fadb4038448c", title: "The list's arrival animation", live: false },
            { id: "0ded3be9-4b90-401c-b8dc-0c7631ad65a7", title: "Why the shell panel draws nothing", live: false },
            { id: "5c2edf02-6a19-4d78-b0f4-9e2a1c73d845", title: "Subagent rows, and the mark that says whose they are", live: false },
            { id: "782d360b-4e91-42a7-9c05-6db8f3e21a47", title: "Codex session management from the phone", live: false },
            { id: "af32d66f-1b83-4c60-a7e9-0524c8fb1d93", title: "The README, rewritten around what it is for", live: false },
            { id: "56e45cef-9d20-4a15-8b73-e1f60c9a4c25", title: "Three stacks that would not start", live: false },
            { id: "d04b6901-77c4-4e39-a1b8-5c0fe2739b6d", title: "Background agents, counted from a directory", live: false },
            { id: "a2937509-3f18-4b52-9de0-71c4a8065f3e", title: "Whether the notch island can be done at all", live: false },
            { id: "e949e849-2c67-4d80-b3a5-98f107e2ba14", title: "Controlling a session from a phone browser", live: false },
            { id: "655f2cce-8b34-41e6-97d2-3a05be71cf28", title: "A Mac app that restarts servers on its own", live: false },
            { id: "d7f953f4-f92c-41f0-939c-6c99b7403ce0", title: "Moving the prompt to the top of the screen", live: false },
            { id: "62dc0257-5a91-4e08-8f36-b7c204ed19a5", title: "Splitting the page into modules", live: false },
            { id: "808b9569-0e42-4c73-a19b-d5f83162e7c4", title: "Pasting into the composer on a phone", live: false },
            { id: "985eb7c3-6d15-4820-93af-1c7e05b4d962", title: "The iTerm popup when a session is closed from the web", live: false },
            { id: "2c9c9b0f-4f76-4a31-b8e2-90c53d17f6ab", title: "Safe area under the status bar", live: false },
            { id: "76b5b985-1a58-4de9-87c0-64f2b90e3d17", title: "Shimmer timing on the waiting row", live: false },
            { id: "21fff81e-9c03-4b47-a2d8-e6015f7c8b39", title: "What a waiting session looks like on the web", live: false },
            { id: "d055e58f-3e81-4062-95c7-b8a1f4270de6", title: "Changelog, release notes and both READMEs", live: false },
            { id: "a9e58ab5-7b26-4f19-80d3-c542e7169fa8", title: "A model switch the browser could not show", live: false },
            { id: "7d552a7f-2d90-4a53-b716-3f80c95e2417", title: "What the header should say about the project", live: false }
        ],
        "24f9bac626da56ea": [
            { id: "045286cb-22ea-4d0e-91d6-74c8fd0f53dc", title: "First pass at the importer", live: false }
        ]
    };

    // What a project has an address for, per session. Two of the six have anything at all,
    // which is the honest proportion: a project with no CI and no dev stack has an empty list,
    // and that is the case the sheet mostly has to be good at.
    //
    // Between them the rows cover every shape the sheet has to draw — a site that is up, a
    // deploy that has failed, one that is still running, two dev-stack servers on the Mac's own
    // network with one of them down, and a backlog that is a file no browser can open.
    var links = {
        "8F3A-1C": [
            { label: "site", url: "https://clawdline.example.com", kind: "site", state: "ok", local: false },
            { label: "ci", url: "https://github.com/example/clawdline/actions/runs/32206093368", kind: "deploy", state: "fail", local: false },
            { label: "web", url: "http://127.0.0.1:5173", kind: "server", state: "ok", local: true },
            { label: "api", url: "http://127.0.0.1:8787", kind: "server", state: "down", local: true, why: "build-web: bash: npm: command not found" },
            { label: "backlog", url: "file:///Users/you/code/clawdline/artifacts/backlog.html", kind: "artifact", state: "", local: true }
        ],
        "2C71-90": [
            { label: "staging", url: "https://staging.example.com", kind: "site", state: "down", local: false },
            { label: "deploy", url: "https://github.com/example/atrium/actions/runs/32206093412", kind: "deploy", state: "running", local: false,
              startedAt: now - 320, typicalSeconds: 800 }
        ]
    };
    // The info card's shapes. A Claude session that has just spent its five-hour window, with
    // the deploy that failed under it; a Claude session whose plan nobody has reported on, in a
    // clean tree; and a Codex session with the plain percentage its rollout carries. Every
    // other session answers with what a session that has no transcript yet answers with.
    // `?info=fail` is the route refusing.
    // What each assistant's `/model` will take: Claude Code an alias, Codex a slug.
    var CLAUDE_MODELS = [
        { id: "claude-fable-5", name: "Fable 5", command: "fable" },
        { id: "claude-opus-5", name: "Opus 5", command: "opus" },
        { id: "claude-sonnet-5", name: "Sonnet 5", command: "sonnet" },
        { id: "claude-haiku-4-5", name: "Haiku 4.5", command: "haiku" }
    ];
    var CODEX_MODELS = [
        { id: "gpt-5.6-sol", name: "GPT-5.6-Sol", command: "gpt-5.6-sol" },
        { id: "gpt-5.5", name: "GPT-5.5", command: "gpt-5.5" },
        { id: "gpt-5.4", name: "GPT-5.4", command: "gpt-5.4" },
        { id: "gpt-5.4-mini", name: "GPT-5.4-Mini", command: "gpt-5.4-mini" }
    ];
    var PERMISSION_MODES = ["auto", "manual", "acceptEdits", "plan"];
    var info = {
        "8F3A-1C": {
            models: CLAUDE_MODELS,
            permission: { current: "auto", options: PERMISSION_MODES },
            session: { id: "8F3A-1C", title: "fix the webhook signature and ship the receiver",
                       assistant: "claude", sessionId: "a2937509-a3d4-4c31-87a7-cdb7ff073d38",
                       model: "claude-fable-5", cwd: "/Users/x/code/clawdline",
                       startedAt: now - 5580, seconds: 5580 },
            usage: { input: 4821, output: 38210, cacheRead: 2984120, cacheWrite: 214880, total: 3242031,
                     model: "claude-fable-5", costUsd: 7.38 },
            limits: { windows: [
                { name: "5h", usedPercent: 100, resetsAt: now + 2760, hit: true }
            ], at: now - 90 },
            files: { branch: "main", head: "d5c61e9f91c46a77", ahead: 2, behind: 0,
                     staged: 1, unstaged: 4, untracked: 2, conflict: 0 },
            links: links["8F3A-1C"]
        },
        "2C71-90": {
            models: CLAUDE_MODELS,
            permission: { current: "unknown", options: PERMISSION_MODES },
            session: { id: "2C71-90", title: "trace the signup 500 from the browser to the database",
                       assistant: "claude", model: "claude-opus-5", cwd: "/Users/x/code/atrium",
                       startedAt: now - 24300, seconds: 24300 },
            usage: { input: 19340, output: 61022, cacheRead: 7120400, cacheWrite: 380210, total: 7580972,
                     model: "claude-opus-5", costUsd: 5.61 },
            limits: { windows: [] },
            files: { branch: "fix/signup-500", head: "9a1b2c3d4e5f6071", ahead: 0, behind: 3,
                     staged: 0, unstaged: 0, untracked: 0, conflict: 0 },
            links: links["2C71-90"]
        },
        "44D2-05": {
            models: CODEX_MODELS,
            session: { id: "44D2-05", title: "turn the field notes into a publishable technical brief",
                       assistant: "codex", model: "gpt-5.3-codex", cwd: "/Users/x/tmp/notes",
                       startedAt: now - 840, seconds: 840 },
            usage: { input: 8190546, output: 16956, cacheRead: 7978752, cacheWrite: 0, total: 8207502,
                     model: "gpt-5.3-codex" },
            context: { usedPercent: 42.24, usedTokens: 109138, windowTokens: 258400 },
            limits: { windows: [
                { name: "5h", usedPercent: 24, resetsAt: now + 9000, hit: false },
                { name: "7d", usedPercent: 71, resetsAt: now + 3 * 86400 + 4000, hit: false }
            ], at: now - 12 },
            links: []
        }
    };
    var MOCK_INFO = params.get("info") || "";
    var MOCK_GIT = params.get("git") || "";

    // How the fixture's start behaves: `?start=slow` never reports in, so the fifteen seconds
    // can be watched rather than described; `closed`, `unsupported` and `gone` are the three
    // refusals with a sentence of their own on the other end, `detached` is the success with a
    // sentence of its own, and `empty` is a Mac with nowhere to start at all.
    var MOCK_START = params.get("start") || "";

    // Dictation, which is the one fixture that cannot fake its own half of the job: the
    // microphone is the browser's and it really does record. What is faked is the Mac at the
    // other end, and every state it can be in. `?voice=slow` is the twelve seconds a Whisper
    // model takes to come off disk the first time after a reboot — the case the counter in the
    // composer exists for, and the one nobody can reproduce on purpose.
    var MOCK_VOICE = params.get("voice") || "";
    // Two of them, so a second dictation shows what happens to a box that already has words in
    // it. The first is the sentence `docs/whisper.md` holds up as the whole reason Whisper is
    // here at all: two languages, one breath, and Apple's recogniser cannot hear it.
    var HEARD = ["cambia el retry a exponential backoff",
                 "and put a note above it saying why"];
    var heard = 0;

    var live = false;
    var beat = 0;
    var admitted = !MOCK_DOOR;   // ?door=1 arrives with nothing, the way a phone does
    var asked = 0, wrong = 0;

    function emit() {
        if (!live) return;
        // A copy each time, for the same reason the server sends the whole list: nothing
        // downstream should be able to hold a reference into the source of truth.
        handlers.sessions(JSON.parse(JSON.stringify(sessions)), Math.floor(Date.now() / 1000), {
            generation: Date.now(), complete: true, emptyAuthoritative: true
        });
    }

    function find(id) {
        for (var i = 0; i < sessions.length; i++) if (sessions[i].id === id) return sessions[i];
        return null;
    }

    var verbs = ["Gallivanting", "Puzzling", "Noodling", "Percolating", "Untangling", "Reticulating"];
    var clocks = {};

    /** The live line carries its own clock, written by Claude Code. Ticking it here is what
     *  makes a working row look alive between the bigger changes. */
    function tick() {
        var changed = false;
        sessions.forEach(function (s) {
            if (s.state !== "working") { delete clocks[s.id]; return; }
            var c = clocks[s.id] || (clocks[s.id] = { since: Date.now(), verb: verbs[Math.floor(Math.random() * verbs.length)], tokens: 1.2 + Math.random() * 4 });
            var secs = Math.floor((Date.now() - c.since) / 1000);
            c.tokens += 0.04;
            s.line = c.verb + "… (" + (secs >= 60 ? Math.floor(secs / 60) + "m " + (secs % 60) + "s" : secs + "s") +
                " · ↓ " + c.tokens.toFixed(1) + "k tokens)";
            changed = true;
        });
        if (changed) emit();
    }

    // The real server projects these two axes atomically in one payload. Keep transitions going
    // through one helper or a mock frame can manufacture the exact missing/mismatched state the
    // production client is required to fail closed as unknown.
    function setSessionState(id, state) {
        var session = find(id);
        if (!session) return;
        session.state = state;
        if (state === "waiting") session.work_state = "waiting_you";
        else if (state === "working") session.work_state = "working";
        else if (state === "unknown") session.work_state = "unknown";
        else if ((session.coordination && ((session.coordination.waitingOn || []).length ||
                 (session.coordination.waitedOnBy || []).length))) {
            session.work_state = "waiting_session";
        } else if (session.assistant) {
            session.work_state = "unknown";
        } else {
            session.work_state = "ready";
        }
    }

    /** The interesting part: states that move, so the transitions can be watched. */
    function step() {
        beat += 1;
        var order = [
            function () { setSessionState("9B04-2D", "working"); },
            function () { setSessionState("8F3A-1C", "idle"); find("8F3A-1C").line = null;
                          transcripts["8F3A-1C"].push({ role: "assistant", text: "Pushed to `fix/webhook-idempotency`. Nothing else is outstanding.", tool: null, at: Math.floor(Date.now() / 1000) }); },
            function () { setSessionState("44D2-05", "waiting"); },
            function () { setSessionState("2C71-90", "working"); setSessionState("C0FF-3E", "idle"); },
            function () { setSessionState("9B04-2D", "waiting"); setSessionState("44D2-05", "idle"); },
            function () { setSessionState("2C71-90", "waiting"); setSessionState("8F3A-1C", "working"); },
            function () { setSessionState("9B04-2D", "idle"); setSessionState("C0FF-3E", "unknown"); }
        ];
        order[beat % order.length]();
        emit();
    }

    return {
        start: function () {
            handlers.conn("connecting");
            if (!admitted) {
                setTimeout(function () { handlers.conn("locked"); Door.show(); }, 300);
                return;
            }
            setTimeout(function () {
                live = true;
                handlers.hello({ version: "0.6.0-mock", protocol: 1, write: MOCK_WRITE, auth: true, authed: true });
                handlers.conn("live");
                emit();
                // What the stream's own `orchestrator` frame carries, at the moment it carries
                // it: right after the first list, so the rows are already there to be grouped.
                handlers.tasks(JSON.parse(JSON.stringify(tasks)));
            }, 420);
            setInterval(tick, 1000);
            setInterval(step, 4200);
            if (MOCK_FLAKY) {
                setInterval(function () {
                    live = false;
                    handlers.conn("retrying", 3);
                    setTimeout(function () { live = true; handlers.conn("live"); emit(); }, 3000);
                }, 20000);
            }
        },
        refresh: function () { return new Promise(function (done) { setTimeout(function () { emit(); done(); }, 500); }); },

        /// The same two records the fixture's stream sends, for the one fetch that happens
        /// before the stream is open. A copy, for the reason `emit` makes one.
        tasks: function () {
            return new Promise(function (done) {
                setTimeout(function () { done({ tasks: JSON.parse(JSON.stringify(tasks)) }); }, 200);
            });
        },

        // The door, fixtured. The code is 424242 and the password is "mock" — said out loud here
        // because a fixture that keeps secrets from the person testing it is only a nuisance, and
        // `mock_code` is a field no real server sends.
        pair: function (name) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    asked += 1;
                    if (asked > 3) {
                        fail(Object.assign(new Error("Too many pairing attempts. Try again in a few minutes."),
                                           { code: "rate_limited" }));
                        return;
                    }
                    wrong = 0;
                    // `?expires=5` shortens the two minutes so the lapse can be watched rather
                    // than waited out.
                    var life = parseInt(params.get("expires"), 10) || 120;
                    done({ pairing_id: "mock-" + name, expires: Math.floor(Date.now() / 1000) + life,
                           mock_code: "424242" });
                }, 350);
            });
        },
        confirmPair: function (id, code) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (code === "424242") { admitted = true; done({ ok: true }); return; }
                    wrong += 1;
                    fail(Object.assign(new Error(wrong >= 5
                        ? "That pairing has expired. Start again."
                        : "That code is not right. " + (5 - wrong) + " tries left."), { code: "forbidden" }));
                }, 300);
            });
        },
        password: function (secret) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (secret === "mock") { admitted = true; done({ ok: true }); return; }
                    fail(Object.assign(new Error("That is not the password."), { code: "unauthorized" }));
                }, 300);
            });
        },
        transcript: function (id) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    var entries = transcripts[id] || [];
                    if (!entries.length) { fail(Object.assign(new Error("Nothing to read from this session yet."), { code: "not_found" })); return; }
                    done({ entries: entries.slice(), signature: id + ":" + entries.length + ":" + (entries[entries.length - 1] || {}).at });
                }, 220);
            });
        },
        // Stopping one, which the fixtures answer and do not act on: there is no process behind
        // any of this, and a mock that pretended otherwise would be the one place this feature
        // looked like it worked when it did not.
        killShell: function () {
            return new Promise(function (done) { setTimeout(done, 200, { ok: true }); });
        },
        // And enough of a background command's output to see the panel it lands in. Bytes in
        // the order they were written, because that is all a command has to show.
        shell: function (id, shellId) {
            return new Promise(function (done) {
                setTimeout(function () {
                    var s = sessions.filter(function (x) { return x.id === id; })[0];
                    var meta = ((s && s.shells) || []).filter(function (h) { return h.id === shellId; })[0] || null;
                    var lines = [];
                    for (var i = 208; i <= 214; i++) {
                        lines.push("[" + i + "/318] Compiling importer/" +
                                   ["rows.rs", "csv.rs", "quote.rs", "header.rs", "sniff.rs",
                                    "encode.rs", "errors.rs"][i - 208]);
                    }
                    done({
                        shell: meta,
                        text: lines.join("\n") + "\n",
                        ended: false,
                        at: Math.floor(Date.now() / 1000),
                        signature: "mock-" + lines.length
                    });
                }, 140);
            });
        },
        // Enough of an agent's conversation to see the pane it lands in. The row itself comes
        // from the session fixture, so what the header says here is what the strip said.
        agent: function (id, agentId) {
            return new Promise(function (done) {
                setTimeout(function () {
                    var s = sessions.filter(function (x) { return x.id === id; })[0];
                    var meta = ((s && s.agents) || []).filter(function (a) { return a.id === agentId; })[0] || null;
                    var at = Math.floor(Date.now() / 1000) - 90;
                    done({
                        agent: meta,
                        entries: [
                            { role: "user", text: (meta && meta.what) || "Go and have a look.", at: at },
                            { role: "tool", tool: "Grep", text: "retry_after", at: at + 3 },
                            { role: "tool", tool: "Read", text: "app/webhooks/verify.rb", at: at + 9 },
                            { role: "assistant", text: (meta && meta.result) || "Still going.", at: at + 44 }
                        ],
                        signature: agentId + ":mock"
                    });
                }, 200);
            });
        },
        skills: function (id) {
            return new Promise(function (done) {
                setTimeout(function () {
                    var session = sessions.filter(function (s) { return s.id === id; })[0];
                    if (session && session.assistant === "codex") {
                        done({ skills: [
                            { name: "openai-docs", description: "Read official OpenAI documentation", source: "system" },
                            { name: "chrome:control-chrome", description: "Control Chrome for local testing", source: "plugin" },
                            { name: "deploy", description: "Deploy this repository safely", source: "project" }
                        ] });
                        return;
                    }
                    done({ skills: [
                        { name: "recap", description: "Summarize the work in this session", source: "project" },
                        { name: "frontend-design", description: "Build a distinctive production interface", source: "personal" },
                        { name: "design:visual", description: "Review layout and visual hierarchy", source: "plugin" }
                    ] });
                }, 180);
            });
        },
        send: function (id, text, images) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (!MOCK_WRITE) { fail(Object.assign(new Error("Sending is not enabled on this server."), { code: "write_disabled" })); return; }
                    var t = transcripts[id] || (transcripts[id] = []);
                    var carried = (images || []).map(function (_, i) { return "[Image #" + (i + 1) + "]"; }).join(" ");
                    var s = find(id);
                    var alreadyWorking = s && s.state === "working";
                    if (s) { setSessionState(id, "working"); s.line = "Accepting your message…"; }
                    emit();
                    done({ ok: true, at: Math.floor(Date.now() / 1000), audit_id: uuid() });
                    // The HTTP answer means the keystrokes reached the Mac, not that the
                    // assistant has written them into its transcript. Keeping that gap in the
                    // fixture makes the browser exercise the same optimistic/reconcile path as
                    // the real app; a busy session makes the gap conspicuously longer.
                    setTimeout(function () {
                        t.push({ role: "user", text: [carried, text].filter(Boolean).join(" "), tool: null, at: Math.floor(Date.now() / 1000) });
                        if (s) s.line = "Reading your message…";
                        emit();
                    }, alreadyWorking ? 7000 : 4000);
                }, 300);
            });
        },
        /**
         * Naming a session, offline.
         *
         * Behind `MOCK_WRITE` like every other write here, and refusing in the fixture's own
         * words is the point: this method exists because the real one used to be grafted onto
         * this object by `net/api.js`, which sent a rename from `?mock=1` at the network and
         * put a static file server's `Unsupported method ('POST')` on the card.
         *
         * `downstream` is always `local_only`, because there is no terminal behind a fixture to
         * type a slash command into and claiming otherwise would be the one lie a mock of this
         * route could tell.
         */
        title: function (id, title) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (!MOCK_WRITE) { fail(Object.assign(new Error("Renaming is not enabled on this server."), { code: "write_disabled" })); return; }
                    var wanted = String(title || "").replace(/\s+/g, " ").trim();
                    var s = find(id);
                    var shown = wanted || (s && s.label) || id;
                    if (info[id] && info[id].session) info[id].session.title = shown;
                    if (s) s.label = shown;
                    emit();
                    done({ ok: true, title: wanted, display_title: shown,
                           local_applied: true, downstream: "local_only",
                           downstream_synced: false });
                }, 260);
            });
        },

        /** Answering moves the session off `waiting`, which is the whole thing worth seeing
         *  from a file:// copy: the menu goes, the buttons go, and the composer comes back. */
        key: function (id, press) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (!MOCK_WRITE) { fail(Object.assign(new Error("Sending is not enabled on this server."), { code: "write_disabled" })); return; }
                    if (press === "shift+tab") {
                        var permission = info[id] && info[id].permission;
                        var at = permission ? PERMISSION_MODES.indexOf(permission.current) : -1;
                        if (at >= 0) permission.current = PERMISSION_MODES[(at + 1) % PERMISSION_MODES.length];
                        done({ ok: true });
                        return;
                    }
                    var s = find(id);
                    if (s) { setSessionState(id, "working"); s.line = "Deciding\u2026 (1s)"; s.menu = null; }
                    emit();
                    done({ ok: true });
                }, 200);
            });
        },

        focus: function () {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (!MOCK_WRITE) { fail(Object.assign(new Error("Focus is not enabled on this server."), { code: "write_disabled" })); return; }
                    done({ ok: true });
                }, 150);
            });
        },

        /**
         * The Mac's half of a dictation. It ignores the audio entirely — what it is standing in
         * for is the wait and the four answers, which is everything the composer has to be able
         * to draw. Not behind `MOCK_WRITE`: transcribing is not a write, and a fixture that
         * refused it would hide the whole feature from the mode built to look at it.
         *
         * 1.6s is what `docs/whisper.md` measured on an M4 with the turbo model.
         */
        voice: function (audio, rate) {
            return new Promise(function (done, fail) {
                var ms = MOCK_VOICE === "slow" ? 12000 : 1600;
                setTimeout(function () {
                    if (MOCK_VOICE === "busy") {
                        fail(Object.assign(new Error("Two transcriptions are already queued."),
                                           { code: "busy" }));
                        return;
                    }
                    if (MOCK_VOICE === "nowhisper" || MOCK_VOICE === "nomodel") {
                        fail(Object.assign(new Error("This Mac has no Whisper."),
                                           { code: "no_whisper",
                                             reason: MOCK_VOICE === "nomodel" ? "no_model" : "no_binary" }));
                        return;
                    }
                    // A recording of a quiet room. Not an error, and the composer says so in its
                    // own words rather than showing a failure for something that merely happened.
                    if (MOCK_VOICE === "silent") { done({ text: "", ms: ms }); return; }
                    done({ text: HEARD[heard++ % HEARD.length], ms: ms });
                }, ms);
            });
        },

        /**
         * The planner's answer to one sentence, fixtured for `input/command.js`. Unlike `voice`
         * above this one *is* behind `MOCK_WRITE`: the real route is gated exactly like `/v1/send`
         * — see the contract in `Command`'s own file — and the write-off path is worth being able
         * to see from a file:// copy same as everywhere else it applies.
         *
         * `?intents=` picks which of the measured shapes comes back: `noplanner` and `busy` are
         * the two refusals, `unsure` is a low-confidence guess with a question attached — and a
         * model judged anyway, since that is a separate question from which project — `silent`
         * is a draft with nothing to type — a request to open a session and no more — and `etc`
         * reproduces the exact case in `Planner.swift`'s own header — a model that would not pick
         * a directory outside the list but still wrote a sentence about one into `instructions`.
         * `schedule` is a spoken schedule the planner heard enough of to fill in; `scheduleunsure`
         * is the one the plan calls out by name, heard without a time. Both carry `kind:
         * "schedule"` and hand off to `input/schedule.js` instead of opening a session — see
         * `reveal` in `input/command.js`. `nomodel` is the ordinary confident answer with the
         * judgement left out — `draft.model` absent entirely, the shape a Mac too old for this
         * round's Planner sends. Anything else is the ordinary confident session answer, aimed at
         * whichever project is first, and it is also where `draft.model` is exercised: the
         * planner judged this one `sonnet`.
         */
        intents: function (text) {
            return new Promise(function (done, fail) {
                // The measured range is 4.6-5.1s; kept in that range here rather than sped up, so
                // the sheet's honest "about five seconds" is something this mode can actually see.
                var ms = 4800;
                setTimeout(function () {
                    if (!MOCK_WRITE) { fail(Object.assign(new Error("Sending is not enabled on this server."), { code: "write_disabled" })); return; }
                    var mode = params.get("intents") || "";
                    if (mode === "noplanner") {
                        fail(Object.assign(new Error("This Mac has neither claude nor codex on it."),
                                           { code: "no_planner" }));
                        return;
                    }
                    if (mode === "busy") {
                        fail(Object.assign(new Error("This Mac is already working out two of these."),
                                           { code: "busy" }));
                        return;
                    }
                    if (mode === "unsure") {
                        // Unsure of the project and confident about the job in the same breath —
                        // the two are judged separately (see the plan), so a low `confidence`
                        // here still leaves `model` set. This is the shape that proves the model
                        // chips render, and stay pressable, on a draft that stops for review
                        // rather than the confident one that opens straight through.
                        done({ draft: { place_id: null, assistant: "claude", model: "haiku",
                                        instructions: text, title: text.slice(0, 20), confidence: 0.3,
                                        question: "Which project is this for?" }, ms: ms });
                        return;
                    }
                    if (mode === "silent") {
                        // "Open clawdline" and nothing else. The draft names a project and has
                        // nothing to type, which is the whole request — the sheet opens the tab,
                        // sends no message, and goes there.
                        var quiet = places[0];
                        done({ draft: { place_id: quiet ? quiet.id : null, assistant: "claude",
                                        instructions: "", title: "a session", confidence: 0.9,
                                        question: "" }, ms: ms });
                        return;
                    }
                    if (mode === "schedule") {
                        // A schedule the planner heard enough of: a time, some days, a project.
                        // `input/command.js` reads `kind` and hands the whole draft to
                        // `input/schedule.js` instead of opening a session — see the contract.
                        var here = places[0];
                        done({ draft: { kind: "schedule", place_id: here ? here.id : null,
                                        assistant: "claude", model: "opus", at: "09:00",
                                        days: ["mon", "wed", "fri"], instructions: text,
                                        title: text.slice(0, 20) || "a schedule",
                                        confidence: 0.86, question: "" }, ms: ms });
                        return;
                    }
                    if (mode === "nomodel") {
                        // The ordinary confident draft below, minus the one field this round
                        // added — a Mac whose Planner has not been rebuilt yet, or one that
                        // judged the assistant to be codex and left it out on purpose. Either
                        // way `chosenModel` in `input/command.js` has to fall back to "" rather
                        // than choke on a key that is not there at all.
                        var nowhere = places[0];
                        done({ draft: { place_id: nowhere ? nowhere.id : null, assistant: "claude",
                                        instructions: "Understood: " + text,
                                        title: (text.slice(0, 20) || "a session"),
                                        confidence: 0.82, question: "" }, ms: ms });
                        return;
                    }
                    if (mode === "scheduleunsure") {
                        // The one the plan calls out by name: no time heard, confidence below
                        // `sure`, and the form opens holding what little there is rather than a
                        // question this sheet has nowhere to ask.
                        done({ draft: { kind: "schedule", place_id: null, assistant: "claude",
                                        at: "", days: [], instructions: text,
                                        title: text.slice(0, 20) || "a schedule", confidence: 0.2,
                                        question: "What time should this run?" }, ms: ms });
                        return;
                    }
                    if (mode === "etc") {
                        done({ draft: { place_id: null, assistant: "claude",
                                        title: "print hosts", confidence: 0.3,
                                        question: "/etc is not one of the projects on this Mac.",
                                        instructions: "Open a session in /etc and print hosts" },
                               ms: ms });
                        return;
                    }
                    var place = places[0];
                    done({ draft: { place_id: place ? place.id : null, assistant: "claude",
                                    model: "sonnet", instructions: "Understood: " + text,
                                    title: (text.slice(0, 20) || "a session"),
                                    confidence: 0.82, question: "" }, ms: ms });
                }, ms);
            });
        },

        end: function (id, acceptLoss, closeabilityVersion) {
            void acceptLoss; void closeabilityVersion;
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (!MOCK_WRITE) { fail(Object.assign(new Error("Sending is not enabled on this server."), { code: "write_disabled" })); return; }
                    sessions = sessions.filter(function (s) { return s.id !== id; });
                    emit();
                    done({ ok: true });
                // The real route waits for the assistant to quit before it closes the tab. A
                // short fixture made the confirmation disappear before its waiting state could
                // be read, so keep this honest enough to inspect from a file:// copy.
                }, 1200);
            });
        },

        /** Slow because the real route reads a transcript and shells out, and the refresh
         *  button has to be seen to be doing something. */
        info: function (id) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (MOCK_INFO === "fail") {
                        fail(Object.assign(new Error("No session named that"), { code: "not_found" }));
                        return;
                    }
                    if (info[id]) { done({ info: info[id] }); return; }
                    var session = sessions.filter(function (s) { return s.id === id; })[0] || {};
                    done({ info: {
                        session: { id: id, title: session.label, assistant: session.assistant, cwd: session.cwd },
                        limits: { windows: [] },
                        links: (links[id] || []).slice(),
                        models: session.assistant === "codex" ? CODEX_MODELS : CLAUDE_MODELS,
                        permission: session.assistant === "claude"
                            ? { current: "manual", options: PERMISSION_MODES } : undefined
                    } });
                }, 640);
            });
        },

        git: function () {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (MOCK_GIT === "fail") {
                        fail(Object.assign(new Error("Could not read that repository"),
                                           { code: "git_failed" }));
                        return;
                    }
                    if (MOCK_GIT === "notrepo") {
                        fail(Object.assign(new Error("That session is not inside a Git repository"),
                                           { code: "not_a_repo" }));
                        return;
                    }
                    done({ git: {
                        branch: "main", head: "d5c61e9f91c46a77", ahead: 2, behind: 0,
                        clean: MOCK_GIT === "clean",
                        files: MOCK_GIT === "clean" ? [] : [
                            { path: "Sources/RemoteServer.swift", from: null, staged: false,
                              unstaged: true, kind: "modified", additions: 12, deletions: 3 },
                            { path: "Resources/web/components/session/detail/GitChangesPanel.css",
                              from: "Resources/web/components/session/GitPanel.css", staged: true,
                              unstaged: true, kind: "renamed", additions: 4, deletions: 1 },
                            { path: "notes/status.txt", from: null, staged: false,
                              unstaged: true, kind: "untracked", additions: null, deletions: null },
                            { path: "Sources/Conflict.swift", from: null, staged: true,
                              unstaged: true, kind: "conflict", additions: 8, deletions: 5 }
                        ]
                    } });
                }, 420);
            });
        },

        // The device-readable Bearings projection behind the Clawdfather panel's four
        // read-only commands — the same shape `GET /v1/orchestrator/coordinator/bearings`
        // answers, with enough in it to demo every branch of the renderer.
        coordinatorBearings: function () {
            return new Promise(function (done) {
                setTimeout(function () {
                    var now = Math.floor(Date.now() / 1000);
                    done({
                        version: 1, observed_at: now,
                        coordinator: {
                            configured: true, label: "Clawdfather", scope: "machine",
                            status: "online", lifecycle: "standby",
                            session: { id: "CF00-01", assistant: "codex",
                                       label: "Clawdfather · machine coordinator",
                                       cwd: "/Users/x/code/clawdline", work_state: "working" }
                        },
                        bearings: {
                            observed_at: now, coordinator_lifecycle: "standby",
                            work_state_counts: { ready: 1, working: 3, waiting_you: 1,
                                                 waiting_session: 1, unknown: 1,
                                                 milestone_complete: 1, work_complete: 0 },
                            active_task_count: 2, pending_landing_count: 1, open_wait_count: 1,
                            unknown: [{ id: "9D1B-44", assistant: "claude",
                                             label: "the exporter is stuck on fonts",
                                             work_state: "unknown" }],
                            waiting: [{ id: "2C71-90", assistant: "claude",
                                        label: "the signup flow keeps 500ing",
                                        work_state: "waiting_you" }],
                            blocking: [{ id: "CF00-01", assistant: "codex",
                                         label: "Clawdfather · machine coordinator",
                                         work_state: "working" }],
                            sources: {
                                sessions: { observed_at: now, freshness: "current" },
                                tasks: { observed_at: now, freshness: "current" },
                                landings: { observed_at: now, freshness: "current" },
                                waits: { observed_at: now, freshness: "current" }
                            }
                        }
                    });
                }, 360);
            });
        },

        places: function () {
            return new Promise(function (done) {
                setTimeout(function () {
                    var now = Math.floor(Date.now() / 1000);
                    // `?start=empty` empties the list, which is the other thing this screen
                    // has to be able to say: a Mac that has never run Claude Code anywhere.
                    // Both, so the chooser is on screen in a demo. A real Mac answers with
                    // whichever of them it actually has a home directory for.
                    var ALL_ASSISTANTS = [{ id: "claude", label: "Claude Code" }, { id: "codex", label: "Codex" }];
                    // `?assistants=claude` narrows this Mac to one — the shape the schedules
                    // review's finding 2 needed and neither this list nor the schedule fixture
                    // below could make on its own: a schedule naming an assistant this Mac does
                    // not have. Open `813fa8a7…` ("Check deployment readiness", `assistant:
                    // "codex"`) with this set and Codex is still what the sheet shows chosen.
                    var only = params.get("assistants");
                    var assistants = only
                        ? ALL_ASSISTANTS.filter(function (a) { return only.split(",").indexOf(a.id) >= 0; })
                        : ALL_ASSISTANTS;
                    done({
                        at: now,
                        assistants: assistants,
                        places: MOCK_START === "empty" ? [] : places.map(function (p, i) {
                            return { id: p.id, label: p.label, path: p.path, icon: p.icon, at: now - i * 900 };
                        })
                    });
                }, 260);
            });
        },

        /* Which of a Project's worktrees finished a Feature, and whether it landed.
           The shape is the real route's, cut down to a screenful: `?projects=` walks the states
           that are otherwise only reachable on a Mac with the right history on it.

             (unset)     the ordinary answer — every rung of the ladder occupied
             empty       worktrees: [], with the read receipt that says the query ran
             missing     404 project_not_found
             ambiguous   409 ambiguous_project
             partial     the scan hit its ceiling: status "partial", read.truncated true
             busy        429 usage_analytics_busy

           A place this fixture has nothing for answers with the empty shape rather than a
           refusal, because that is what a real Project nobody has finished anything in returns:
           a query that ran, a receipt, and no rows. */
        projectWorktrees: function (project) {
            var mode = params.get("projects") || "";
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (mode === "missing") {
                        fail(Object.assign(new Error("No Project resolves to that, in 726 rows read"),
                                           { code: "project_not_found" }));
                        return;
                    }
                    if (mode === "ambiguous") {
                        fail(Object.assign(new Error("Two Projects are called that: project-9c1f… and project-4b0d…"),
                                           { code: "ambiguous_project" }));
                        return;
                    }
                    if (mode === "busy") {
                        fail(Object.assign(new Error("Usage Analytics is busy"),
                                           { code: "usage_analytics_busy" }));
                        return;
                    }
                    var worktrees = mode === "empty" ? [] : (worktreesByPath[project] || []);
                    done({
                        projectWorktrees: {
                            schemaVersion: 1,
                            status: mode === "partial" ? "partial" : "available",
                            policy: "one_unambiguous_accepted_head",
                            outcomeRule: "landed_then_delivered_then_live_then_abandoned",
                            generatedAt: new Date().toISOString(),
                            range: { from: null, to: null, timezone: "Asia/Taipei" },
                            project: { id: "project-9c1f2e7a4b0d8e35", label: project },
                            read: {
                                rows: 726, projectRows: 237,
                                worktreeRows: worktrees.length ? 240 : 0,
                                featureRows: worktrees.length ? 190 : 0,
                                truncated: mode === "partial", maxScannedRows: 100000
                            },
                            worktrees: worktrees,
                            excluded: { worktreesWithoutFeature: worktrees.length ? 31 : 0,
                                        reason: "no_unambiguous_accepted_head" },
                            unattributed: { worktrees: 13,
                                            reasons: { legacy_managed_worktree_project_key: 13 } }
                        }
                    });
                }, 320);
            });
        },

        /** The real one answers before the session exists, so this does too — and the row turns
         *  up a couple of seconds later, which is the gap the band above the list is for. */
        startPlace: function (id, assistant, model) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (!MOCK_WRITE) { fail(Object.assign(new Error("Sending is not enabled on this server."), { code: "write_disabled" })); return; }
                    var place = null;
                    for (var i = 0; i < places.length; i++) if (places[i].id === id) place = places[i];
                    if (!place || MOCK_START === "gone") {
                        fail(Object.assign(new Error("No place named that"), { code: "not_found" }));
                        return;
                    }
                    // The fourth path segment, walked as a failing path on its own: a model this
                    // fixture does not know is a 404, the same as an assistant it does not know —
                    // never a silent fallback to whatever the default would have been. `?start=`
                    // has no case for this because it needs no query flag: `input/command.js`'s
                    // own chips can only ever send one of these three, so the only way to see it
                    // is a Mac and a page that have drifted apart, which this checks for anyway.
                    if (model && ["haiku", "sonnet", "opus"].indexOf(model) < 0) {
                        fail(Object.assign(new Error("No model named that"), { code: "not_found" }));
                        return;
                    }
                    if (MOCK_START === "closed") {
                        fail(Object.assign(new Error("Ghostty is not running, and this will not launch it for you."),
                                           { code: "terminal_closed", app: "Ghostty" }));
                        return;
                    }
                    // No `app`, because the server has none to send: `terminal_unsupported` has
                    // one producer and it is "tmux is what Settings asks for and there is no tmux
                    // on this Mac". Carrying "Ghostty" here made `?mock=1` unable to draw the only
                    // shape this refusal actually arrives in.
                    if (MOCK_START === "unsupported") {
                        fail(Object.assign(new Error("tmux is the terminal for new sessions in Settings, and there is no tmux on this Mac."),
                                           { code: "terminal_unsupported" }));
                        return;
                    }
                    var made = "N" + Math.floor(Math.random() * 9000 + 1000) + "-" + Math.floor(Math.random() * 90 + 10);
                    // `?start=detached` is the one success that leaves nothing on that Mac's
                    // screen: tmux is what Settings asks for, no server was running, so one was
                    // started with nothing attached to it. The pane is real and Clawdline lists
                    // it; `attach` is the only way anybody at the Mac finds it. Empty on every
                    // other path, exactly as the server sends it.
                    var detached = MOCK_START === "detached";
                    done({ ok: true, id: made, backend: detached ? "tmux" : "iterm",
                           assistant: assistant || "claude", place: place.id, cwd: place.path,
                           attach: detached ? "tmux attach -t clawdline" : "",
                           at: Math.floor(Date.now() / 1000) });
                    // Not with the reply: the whole point is that the id is answered before
                    // there is a session to go with it. `?start=slow` is the one that never does.
                    if (MOCK_START === "slow") return;
                    setTimeout(function () {
                        sessions.push({ id: made, backend: "iterm", tty: "ttys0" + Math.floor(Math.random() * 90 + 10),
                                        label: place.label, cwd: place.path, state: "idle", work_state: "ready", line: null,
                                        // Absent for a moment on the real thing too — a shell has
                                        // to start before the assistant is a process anything
                                        // can see, whichever one was asked for.
                                        isClaude: false, assistant: null,
                                        sessionId: null, icon: place.icon });
                        emit();
                    }, 2600);
                }, 320);
            });
        },

        /** Slow in the same way `places` is: the real route reads a title off the end of every
         *  transcript in a project folder, and the sheet has a line for the wait. */
        pastSessions: function (id, assistant) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (MOCK_START === "gone") {
                        fail(Object.assign(new Error("No place named that"), { code: "not_found" }));
                        return;
                    }
                    var now = Math.floor(Date.now() / 1000);
                    var rows = (MOCK_START === "nopast" ? [] : (past[id] || []));
                    done({
                        at: now, place: id, assistant: assistant || "claude",
                        // `?start=capped` is the Mac having stopped before the end of a project's
                        // history — the one thing on this screen that scrolling cannot reach.
                        more: MOCK_START === "capped",
                        sessions: rows.map(function (r, i) {
                            return { id: r.id, title: r.title, live: r.live, at: now - i * 5400 };
                        })
                    });
                }, 300);
            });
        },

        resumePlace: function (id, session, assistant) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (!MOCK_WRITE) { fail(Object.assign(new Error("Sending is not enabled on this server."), { code: "write_disabled" })); return; }
                    var place = null;
                    for (var i = 0; i < places.length; i++) if (places[i].id === id) place = places[i];
                    var known = (past[id] || []).filter(function (r) { return r.id === session; })[0];
                    if (!place || !known || MOCK_START === "gone") {
                        fail(Object.assign(new Error("No conversation named that"), { code: "not_found" }));
                        return;
                    }
                    if (MOCK_START === "closed") {
                        fail(Object.assign(new Error("Ghostty is not running, and this will not launch it for you."),
                                           { code: "terminal_closed", app: "Ghostty" }));
                        return;
                    }
                    var made = "N" + Math.floor(Math.random() * 9000 + 1000) + "-" + Math.floor(Math.random() * 90 + 10);
                    done({ ok: true, id: made, backend: "iterm", assistant: assistant || "claude",
                           place: place.id, cwd: place.path, session: session,
                           at: Math.floor(Date.now() / 1000) });
                    if (MOCK_START === "slow") return;
                    setTimeout(function () {
                        sessions.push({ id: made, backend: "iterm", tty: "ttys0" + Math.floor(Math.random() * 90 + 10),
                                        // A resumed session comes back under the name it already
                                        // had, which is the whole reason somebody picked it off
                                        // the list rather than starting a new one.
                                        label: known.title, cwd: place.path, state: "idle", work_state: "ready", line: null,
                                        isClaude: false, assistant: null,
                                        sessionId: null, icon: place.icon });
                        emit();
                    }, 2600);
                }, 320);
            });
        }
    };
})();

/* ---- schedules fixture ---------------------------------------------------
   Appended as its own block because this file is also an active editing surface.

   One array of full rows backs every schedule route rather than four disconnected fixtures —
   `Mock.schedules` reads it for the list, `Mock.schedule` for one record in full, and
   `Mock.createSchedule`/`updateSchedule`/`deleteSchedule` mutate it, so a save or a delete made
   from a file:// copy is visible the moment `Schedules.refresh()` asks again, the same as it
   would be against a real Mac. Eight rows exercise every `Orchestrator.State` value, a ninth has
   no entry in `SCHEDULE_PLACES` at all, and there is one invalid source with no id — never
   reachable through a row press, since `view/schedules.js` only puts `data-id` on a valid one.

   `?schedules=empty` verifies that an empty inventory leaves no section behind; it is read only
   by the list below, not by `schedule`/`updateSchedule`/`deleteSchedule`, which stay about one
   row at a time regardless of what the list is showing.

   `model`, `permission_mode` and `claims` are on most of these rows on purpose. Before this,
   `scheduleDetail` only ever assembled `assistant`, `project_dir`, `title`, `instructions` and
   `timeout_minutes` — so the entire class of bug the round these fixtures back is about, a save
   quietly dropping a field the form never showed, was invisible in mock mode no matter how
   thoroughly someone clicked through it. See `scheduleDetail` for what is shown and
   `Mock.updateSchedule` for what a save does to each.
   -------------------------------------------------------------------------- */

// The two places this file's own `places` fixture (inside the closure above) names most often.
// Not shared with it — `Mock.schedules` and friends are appended outside that closure on purpose,
// so every place a schedule might name is repeated here rather than reached into.
var SCHEDULE_PLACES = {
    "3b9e26c1587facfd": { path: "/Users/you/code/clawdline", label: "clawdline" },
    "24f9bac626da56ea": { path: "/Users/you/code/atrium", label: "atrium" },
    "470885724e5330e1": { path: "/Users/you/code/cairn", label: "cairn" },
    "9c1d4e77a0b3f215": { path: "/Users/you/code/notebook", label: "notebook" },
    "b04f8a2c6d915e33": { path: "/Users/you/code/astro", label: "astro" },
    "5e7a1c93f2680b4d": { path: "/Users/you/code/cairn/frontend", label: "frontend" },
    "1f6b0d38e9a742c5": { path: "/Users/you/code/clawdline/docs", label: "docs" },
    "c83e5f10ab24d967": { path: "/Users/you/code/sketches", label: "sketches" },
    "7a2c9e46b1d05f38": { path: "/Users/you/code/website", label: "website" },
    "e51b7d02c4a86f19": { path: "/Users/you/tmp/notes", label: "notes" }
};

var SCHEDULE_BASE = [
    { id: "2bf37143-0a1c-4ba8-a04c-33acd3ee6801", title: "Publish the morning brief",
      enabled: true, at: "07:30", days: "daily", placeId: "3b9e26c1587facfd", assistant: "claude",
      instructions: "Read yesterday's merged PRs and post a two-paragraph summary.",
      close_tab: "always", catch_up_hours: 6, notify_on_failure: true, timeout_minutes: 30,
      model: "sonnet", permission_mode: "full", claims: ["docs/"],
      nextFireOffset: 18 * 60, missedAgo: 6 * 86400,
      lastRun: { task_id: "mock-schedule-success", state: "success", ago: 3 * 3600 } },
    { id: "60dd72ae-777e-4e1e-a595-79cc2740cfb1", title: "Rebuild the search index",
      enabled: true, at: "02:00", days: "daily", placeId: "3b9e26c1587facfd", assistant: "claude",
      instructions: "Rebuild the index from the latest content export.",
      close_tab: "on_success", catch_up_hours: 2, notify_on_failure: true, timeout_minutes: 45,
      permission_mode: "full",
      nextFireOffset: 2 * 3600,
      lastRun: { task_id: "mock-schedule-failure", state: "failure", ago: 22 * 3600 } },
    { id: "26c6e7a3-7fd4-470f-8cf2-70cc9362a63b", title: "Archive weekly reports",
      enabled: false, at: "23:00", days: ["sun"], placeId: "1f6b0d38e9a742c5", assistant: "claude",
      instructions: "Move last week's reports into the archive directory.",
      close_tab: "never", catch_up_hours: 0, notify_on_failure: false, timeout_minutes: 20,
      model: "haiku", permission_mode: "auto",
      nextFireOffset: 26 * 3600,
      lastRun: { task_id: "mock-schedule-timeout", state: "timeout", ago: 6 * 86400 } },
    { id: "65d9d034-4158-484e-a95b-26ed80ed6d05", title: "Prepare the release notes",
      enabled: true, at: "09:00", days: ["mon", "wed", "fri"], placeId: "3b9e26c1587facfd",
      assistant: "claude", instructions: "Draft release notes from the commits since the last tag.",
      close_tab: "on_success", catch_up_hours: 12, notify_on_failure: true, timeout_minutes: 60,
      model: "opus", permission_mode: "full", claims: ["CHANGES.md", "docs/"],
      nextFireOffset: 3 * 3600,
      lastRun: { task_id: "mock-schedule-queued", state: "queued", ago: 30 } },
    // The one non-Claude row, so `drawWith`'s chip row has something to show when this is opened
    // to edit — it stays hidden with fewer than two assistants on offer. Pair it with
    // `?assistants=claude` (see `Mock.places` above) to open it as the review's finding 2 found
    // it: an assistant this Mac does not have, named by a file that predates it not having it.
    { id: "813fa8a7-ffca-4970-a395-e5302b1e5e79", title: "Check deployment readiness",
      enabled: true, at: "08:00", days: "daily", placeId: "3b9e26c1587facfd", assistant: "codex",
      instructions: "Confirm the last deploy is healthy before the day starts.",
      close_tab: "always", catch_up_hours: 4, notify_on_failure: true, timeout_minutes: 15,
      permission_mode: "full",
      nextFireOffset: 4 * 3600,
      lastRun: { task_id: "mock-schedule-spawning", state: "spawning", ago: 45 } },
    { id: "3928f442-ed0a-48e8-b081-36942751fbad", title: "Publish the weekly digest",
      enabled: true, at: "10:30", days: ["fri"], placeId: "3b9e26c1587facfd", assistant: "claude",
      instructions: "Summarise the week's schedule runs into one digest message.",
      close_tab: "on_success", catch_up_hours: 6, notify_on_failure: true, timeout_minutes: 30,
      model: "sonnet", permission_mode: "full",
      nextFireOffset: 5 * 3600,
      lastRun: { task_id: "mock-schedule-briefed", state: "briefed", ago: 60 } },
    { id: "b3c87bec-0544-476f-b2c4-55f20988856b", title: "Prune preview builds",
      enabled: true, at: "04:00", days: "daily", placeId: "3b9e26c1587facfd", assistant: "claude",
      instructions: "Delete preview deploys older than 14 days.",
      close_tab: "always", catch_up_hours: 1, notify_on_failure: false, timeout_minutes: 10,
      permission_mode: "full", claims: ["Resources/web/"],
      nextFireOffset: 6 * 3600,
      lastRun: { task_id: "mock-schedule-cancelled", state: "cancelled", ago: 3600 } },
    // No `nextFireOffset` at all — kept from the original fixture, the one case where `enabled`
    // is true and there is still nothing to show in the "next" column.
    { id: "8c8e0e67-f045-408c-882b-0abace9b2174", title: "Warm the documentation cache",
      enabled: true, at: "06:00", days: "daily", placeId: "1f6b0d38e9a742c5", assistant: "claude",
      instructions: "Prime the docs site's build cache before traffic picks up.",
      close_tab: "on_success", catch_up_hours: 6, notify_on_failure: true, timeout_minutes: 30,
      model: "sonnet", permission_mode: "full",
      lastRun: { task_id: "mock-schedule-spawn-failed", state: "spawn_failed", ago: 7200 } },
    // No `placeId` this file knows — `SCHEDULE_PLACES` falls back to "/Users/you/tmp/unknown",
    // and that path is not in `places` either (see the top of this file). This is what a schedule
    // older than `/v1/places`'s recent-directories cap looks like: `task.project_dir` is real, it
    // just cannot be resolved back to a place id on this list. See the review's "the project that
    // fell off the list".
    { id: "f4d6a9b2-3c17-4e58-9a02-7bb4c6ef9d31", title: "Sync the changelog",
      enabled: true, at: "05:00", days: "daily", placeId: "no-longer-recent", assistant: "claude",
      instructions: "Pull merged PRs since the last sync and append them to CHANGES.md.",
      close_tab: "on_success", catch_up_hours: 6, notify_on_failure: true, timeout_minutes: 20,
      permission_mode: "full",
      nextFireOffset: 7 * 3600 }
];
var SCHEDULE_INVALID = { file: "nightly-maintenance.json", state: "invalid",
                         error: "when must contain exactly at and days", error_kind: "schema" };

var scheduleDeleted = {};   // id -> true, once `Mock.deleteSchedule` has taken it
var scheduleEdits = {};     // id -> the row-shaped fields `Mock.updateSchedule` last saved for it
var scheduleCreated = [];   // rows `Mock.createSchedule` has made this session, newest first

function scheduleRow(id) {
    if (scheduleDeleted[id]) return null;
    var found = SCHEDULE_BASE.filter(function (s) { return s.id === id; })[0]
        || scheduleCreated.filter(function (s) { return s.id === id; })[0];
    return found ? Object.assign({}, found, scheduleEdits[id] || {}) : null;
}

function scheduleSummary(row, at) {
    var out = { id: row.id, title: row.title, enabled: row.enabled };
    if (row.nextFireOffset != null) out.next_fire = at + row.nextFireOffset;
    if (row.lastRun) {
        out.last_run = { task_id: row.lastRun.task_id, state: row.lastRun.state,
                         at: at - row.lastRun.ago };
    }
    if (row.missedAgo != null) out.last_missed_at = at - row.missedAgo;
    return out;
}

// Everything `Orchestrator.scheduleRecord(id:)` answers, off the same row the list above reads.
function scheduleDetail(row) {
    var place = SCHEDULE_PLACES[row.placeId] || { path: "/Users/you/tmp/unknown", label: "unknown" };
    var task = { assistant: row.assistant, project_dir: place.path, title: row.title,
                instructions: row.instructions, timeout_minutes: row.timeout_minutes };
    // `scheduleRecord` sends `schedule.taskTemplate` whole, so a field a schedule was never given
    // is simply absent from it — not `null`, not `[]`. Matched here rather than always including
    // the three: `openEdit`'s form has no control for any of them, so how it treats "missing" is
    // exactly the behaviour this fixture exists to be able to show.
    if (row.model) task.model = row.model;
    if (row.permission_mode) task.permission_mode = row.permission_mode;
    if (row.claims && row.claims.length) task.claims = row.claims;
    return {
        id: row.id, title: row.title, enabled: row.enabled, file: row.id + ".json",
        when: { at: row.at, days: row.days },
        task: task,
        close_tab: row.close_tab, catch_up_hours: row.catch_up_hours,
        notify_on_failure: row.notify_on_failure
    };
}

Mock.schedules = function () {
    var at = Math.floor(Date.now() / 1000);
    var list = params.get("schedules") === "empty" ? [] :
        SCHEDULE_BASE.concat(scheduleCreated)
            .map(function (s) { return scheduleRow(s.id); })
            .filter(Boolean)
            .map(function (row) { return scheduleSummary(row, at); })
            .concat([SCHEDULE_INVALID]);
    return new Promise(function (done) {
        setTimeout(function () { done({ schedules: list, at: at }); }, 180);
    });
};

/// One schedule, in full — what `openEdit` in `input/schedule.js` reads to fill its sheet in.
/// `?scheduleGet=notfound` is the row a second tab already deleted, or a link somebody kept past
/// its schedule's own life.
Mock.schedule = function (id) {
    return new Promise(function (done, fail) {
        setTimeout(function () {
            var row = params.get("scheduleGet") === "notfound" ? null : scheduleRow(id);
            if (!row) {
                fail(Object.assign(new Error("No schedule named that"), { code: "not_found" }));
                return;
            }
            done({ schedule: scheduleDetail(row) });
        }, 220);
    });
};

/* ---- making, changing and removing a schedule -----------------------------
   Appended beside the read fixtures above, for the same reason they are their own block.
   `?scheduleCreate=`, `?scheduleUpdate=` and `?scheduleDelete=` each pick a refusal: `busy` and
   `bad` are the two a person can act on from `input/schedule.js`'s own sheet, `notfound` is what
   a stale row answers with, `dispatchoff` (create only) is not a refusal at all — the file still
   gets written — and anything else is the ordinary confident answer. All three are gated on
   `MOCK_WRITE`, exactly like `intents` and `startPlace` above them.
   -------------------------------------------------------------------------- */
Mock.createSchedule = function (schedule) {
    return new Promise(function (done, fail) {
        setTimeout(function () {
            if (!MOCK_WRITE) { fail(Object.assign(new Error("Sending is not enabled on this server."), { code: "write_disabled" })); return; }
            var mode = params.get("scheduleCreate") || "";
            if (mode === "busy") {
                fail(Object.assign(new Error("This Mac is already writing a schedule."), { code: "busy" }));
                return;
            }
            if (mode === "bad") {
                // The exact shape `Orchestrator.schedule(from:)` writes for a bad `when.at` — see
                // `Sources/Orchestrator.swift`. Shown as it arrives, unedited — see `why` in
                // `input/schedule.js`.
                fail(Object.assign(new Error("when.at must be HH:MM in local time"), { code: "bad_request" }));
                return;
            }
            schedule = schedule || {};
            var row = {
                id: uuid(), title: schedule.title || "", at: schedule.at || "",
                days: schedule.days || "daily", placeId: schedule.place_id || null,
                assistant: schedule.assistant || "claude", instructions: schedule.instructions || "",
                enabled: !!schedule.enabled, close_tab: schedule.close_tab || "on_success",
                catch_up_hours: schedule.catch_up_hours != null ? schedule.catch_up_hours : 6,
                notify_on_failure: schedule.notify_on_failure !== false,
                timeout_minutes: schedule.timeout_minutes != null ? schedule.timeout_minutes : 30,
                model: schedule.model || null,
                nextFireOffset: 3600
            };
            // Pushed into the same table the list and the detail route both read, so the row this
            // press just made is what `Schedules.refresh()` finds a moment later — see the header
            // on this block.
            scheduleCreated.unshift(row);
            done({ ok: true,
                   // Beside the schedule it made, never in place of it: the file this row stands
                   // for is written and valid either way. `?scheduleCreate=dispatchoff` is the
                   // one path through this fixture where `input/schedule.js` has to say so
                   // instead of the ordinary "Created." — see `webScheduleDispatchOff`.
                   dispatch_enabled: mode !== "dispatchoff",
                   schedule: { id: row.id, title: row.title, enabled: row.enabled,
                               next_fire: Math.floor(Date.now() / 1000) + row.nextFireOffset } });
        }, 420);
    });
};

/// Changing one already made. `id` never appears in the body — same as the real PATCH, whose id
/// lives in the path — and `schedule_id`/`created_at` are not fields this fixture's edits can
/// touch either, the same rule the plan's contract puts on the Swift side of this route.
Mock.updateSchedule = function (id, body) {
    return new Promise(function (done, fail) {
        setTimeout(function () {
            if (!MOCK_WRITE) { fail(Object.assign(new Error("Sending is not enabled on this server."), { code: "write_disabled" })); return; }
            var mode = params.get("scheduleUpdate") || "";
            if (mode === "busy") {
                fail(Object.assign(new Error("This Mac is already writing a schedule."), { code: "busy" }));
                return;
            }
            if (mode === "bad") {
                fail(Object.assign(new Error("when.at must be HH:MM in local time"), { code: "bad_request" }));
                return;
            }
            if (mode === "notfound" || !scheduleRow(id)) {
                fail(Object.assign(new Error("No schedule named that"), { code: "not_found" }));
                return;
            }
            body = body || {};
            // Row-shaped, not request-shaped — `place_id` becomes `placeId` here for the same
            // reason `Orchestrator.updateSchedule` turns it into `task.project_dir`: the wire name
            // and the field this fixture reads back are not the same word.
            //
            // `claims` and `permission_mode` are deliberately not keys on this object at all: the
            // real PATCH 400s if a body names either, and the value that survives always comes
            // from the file being replaced — never from the request — so leaving them off here
            // and letting `scheduleRow`'s `Object.assign` fall through to the row already on file
            // is what "carried, not settable" looks like in this fixture. `model` **is** a key,
            // set from the body every time including when the body left it out: that reproduces
            // finding 3 of the review these fixtures are for — `model` is body-settable but, as of
            // this fixture, still missing from `Orchestrator.updateSchedule`'s own carry list, so
            // a save through this page's payload (which never sends it) resets it. Once that carry
            // list gains `model`, this line is the one to change to match.
            scheduleEdits[id] = {
                title: body.title, at: body.at, days: body.days, placeId: body.place_id,
                assistant: body.assistant, instructions: body.instructions,
                enabled: !!body.enabled, close_tab: body.close_tab,
                catch_up_hours: body.catch_up_hours, notify_on_failure: !!body.notify_on_failure,
                timeout_minutes: body.timeout_minutes,
                model: body.model || null
            };
            var row = scheduleRow(id);
            done({ ok: true, schedule: { id: row.id, title: row.title, enabled: row.enabled } });
        }, 420);
    });
};

Mock.deleteSchedule = function (id) {
    return new Promise(function (done, fail) {
        setTimeout(function () {
            if (!MOCK_WRITE) { fail(Object.assign(new Error("Sending is not enabled on this server."), { code: "write_disabled" })); return; }
            var mode = params.get("scheduleDelete") || "";
            if (mode === "busy") {
                fail(Object.assign(new Error("This Mac is already writing a schedule."), { code: "busy" }));
                return;
            }
            if (mode === "notfound" || !scheduleRow(id)) {
                fail(Object.assign(new Error("No schedule named that"), { code: "not_found" }));
                return;
            }
            scheduleDeleted[id] = true;
            done({ ok: true });
        }, 420);
    });
};
