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
          cwd: "/Users/x/code/clawdline", state: "working", line: "Gallivanting… (2m 4s · ↓ 6.4k tokens)",
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
        // Waiting, **with the question in it**. This is what the phone could never see: the
        // options were parsed on the Mac and thrown away, so the box could only say "go and
        // find the Mac". The caret is on the second row, which is what a bare Return confirms.
        { id: "2C71-90", backend: "iterm", tty: "ttys011", label: "the signup flow keeps 500ing",
          cwd: "/Users/x/code/atrium", state: "waiting", line: null,
          isClaude: true, assistant: "claude", sessionId: null, icon: atrium,
          menu: { selected: 2, options: [
              { n: 1, label: "Yes", selected: false, can: true },
              { n: 2, label: "Yes, and don't ask again for rails commands in atrium",
                selected: true, can: true },
              { n: 3, label: "No, tell Claude what to do instead", selected: false, can: true }
          ] } },
        // Idle, **and not finished** — the turn ended an hour ago and the build it started is
        // still going. This is the row that said nothing at all before `Shells`: the terminal
        // mentions it once, where the turn ended, and every list after that drew it as done.
        { id: "9B04-2D", backend: "iterm", tty: "ttys002", label: "rewrite the CSV importer",
          cwd: "/Users/x/code/notebook", state: "idle", line: null,
          isClaude: true, assistant: "claude", sessionId: null, icon: creature,
          shells: [
              { id: "bvlp3xmku", at: now - 6, doing: "[214/318] Compiling importer/rows.rs" }
          ] },
        { id: "A15E-77", backend: "tmux", tty: "tmux:%12", label: "docs pass before the release",
          cwd: "/Users/x/code/clawdline/docs", state: "idle", line: null,
          isClaude: true, assistant: "claude", sessionId: null, icon: clawdline },
        { id: "44D2-05", backend: "iterm", tty: "ttys017", label: "scratch",
          cwd: "/Users/x/tmp/notes", state: "idle", line: null,
          isClaude: false, assistant: "codex", sessionId: null, icon: null },
        { id: "C0FF-3E", backend: "iterm", tty: "ttys021", label: "build box over ssh",
          cwd: "/Users/x", state: "unknown", line: null,
          isClaude: true, assistant: "claude", sessionId: null, icon: null },
        { id: "5E20-8B", backend: "tmux", tty: "tmux:%14", label: "check the German strings",
          cwd: "/Users/x/code/clawdline", state: "working", line: "Reading Copy+German.swift",
          isClaude: true, assistant: "claude", sessionId: null, icon: clawdline }
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
        ["user", "summarise what changed", null],
        // Everything the renderer knows how to draw, in one answer, so the fixture is also the
        // test: a table with alignment markers, a heading, both kinds of list, a quote, a rule.
        ["assistant", "## What changed\n\n| file | change | lines |\n|---|:---:|---:|\n| `Sources/Webhook.swift` | ack before the email | **+18 −24** |\n| `Sources/Ledger.swift` | upsert on `event_id` | +7 −2 |\n| `0009_event_id_unique.sql` | new | +4 |\n\n### Still to do\n\n1. Run the migration on staging\n2. Watch the redelivery counter for a day\n   - it should fall to zero\n   - if it does not, the index did not take\n\n> The handler now answers as soon as the row is durable, and *not* before.\n\n**Run `0009_event_id_unique.sql` on staging first** — until it has run there is no index, and nothing stopping the second row.\n\n---\n\nSee [the Stripe note on retries](https://stripe.com/docs/webhooks) for where the 30 seconds comes from.", null]
    ];

    var transcripts = {};
    transcripts["8F3A-1C"] = script.map(function (e, i) {
        return { role: e[0], text: e[1], tool: e[2], at: now - (script.length - i) * 47 };
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
        { role: "assistant", text: "It explains the file and never says where to put it. Everything else assumes you already know that `devstack.json` sits at the root of the project it describes.", tool: null, at: now - 3580 }
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
            session: { id: "8F3A-1C", assistant: "claude", sessionId: "a2937509-a3d4-4c31-87a7-cdb7ff073d38",
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
            session: { id: "2C71-90", assistant: "claude", model: "claude-opus-5", cwd: "/Users/x/code/atrium",
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
            session: { id: "44D2-05", assistant: "codex", model: "gpt-5.3-codex", cwd: "/Users/x/tmp/notes",
                       startedAt: now - 840, seconds: 840 },
            usage: { input: 8190546, output: 16956, cacheRead: 7978752, cacheWrite: 0, total: 8207502,
                     model: "gpt-5.3-codex" },
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
    // refusals with a sentence of their own on the other end, and `empty` is a Mac with
    // nowhere to start at all.
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
        handlers.sessions(JSON.parse(JSON.stringify(sessions)), Math.floor(Date.now() / 1000));
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

    /** The interesting part: states that move, so the transitions can be watched. */
    function step() {
        beat += 1;
        var order = [
            function () { find("9B04-2D").state = "working"; },
            function () { find("8F3A-1C").state = "idle"; find("8F3A-1C").line = null;
                          transcripts["8F3A-1C"].push({ role: "assistant", text: "Pushed to `fix/webhook-idempotency`. Nothing else is outstanding.", tool: null, at: Math.floor(Date.now() / 1000) }); },
            function () { find("44D2-05").state = "waiting"; },
            function () { find("2C71-90").state = "working"; find("C0FF-3E").state = "idle"; },
            function () { find("9B04-2D").state = "waiting"; find("44D2-05").state = "idle"; },
            function () { find("2C71-90").state = "waiting"; find("8F3A-1C").state = "working"; },
            function () { find("9B04-2D").state = "idle"; find("C0FF-3E").state = "unknown"; }
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
                    if (s) { s.state = "working"; s.line = "Accepting your message…"; }
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
                    if (s) { s.state = "working"; s.line = "Deciding\u2026 (1s)"; s.menu = null; }
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

        end: function (id) {
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
                        session: { id: id, assistant: session.assistant, cwd: session.cwd },
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

        places: function () {
            return new Promise(function (done) {
                setTimeout(function () {
                    var now = Math.floor(Date.now() / 1000);
                    done({
                        at: now,
                        // `?start=empty` empties the list, which is the other thing this screen
                        // has to be able to say: a Mac that has never run Claude Code anywhere.
                        // Both, so the chooser is on screen in a demo. A real Mac answers with
                        // whichever of them it actually has a home directory for.
                        assistants: [{ id: "claude", label: "Claude Code" }, { id: "codex", label: "Codex" }],
                        places: MOCK_START === "empty" ? [] : places.map(function (p, i) {
                            return { id: p.id, label: p.label, path: p.path, icon: p.icon, at: now - i * 900 };
                        })
                    });
                }, 260);
            });
        },

        /** The real one answers before the session exists, so this does too — and the row turns
         *  up a couple of seconds later, which is the gap the band above the list is for. */
        startPlace: function (id, assistant) {
            return new Promise(function (done, fail) {
                setTimeout(function () {
                    if (!MOCK_WRITE) { fail(Object.assign(new Error("Sending is not enabled on this server."), { code: "write_disabled" })); return; }
                    var place = null;
                    for (var i = 0; i < places.length; i++) if (places[i].id === id) place = places[i];
                    if (!place || MOCK_START === "gone") {
                        fail(Object.assign(new Error("No place named that"), { code: "not_found" }));
                        return;
                    }
                    if (MOCK_START === "closed") {
                        fail(Object.assign(new Error("Ghostty is not running, and this will not launch it for you."),
                                           { code: "terminal_closed", app: "Ghostty" }));
                        return;
                    }
                    if (MOCK_START === "unsupported") {
                        fail(Object.assign(new Error("A session cannot be started in that terminal from here."),
                                           { code: "terminal_unsupported", app: "Ghostty" }));
                        return;
                    }
                    var made = "N" + Math.floor(Math.random() * 9000 + 1000) + "-" + Math.floor(Math.random() * 90 + 10);
                    done({ ok: true, id: made, backend: "iterm", assistant: assistant || "claude",
                           place: place.id, cwd: place.path, at: Math.floor(Date.now() / 1000) });
                    // Not with the reply: the whole point is that the id is answered before
                    // there is a session to go with it. `?start=slow` is the one that never does.
                    if (MOCK_START === "slow") return;
                    setTimeout(function () {
                        sessions.push({ id: made, backend: "iterm", tty: "ttys0" + Math.floor(Math.random() * 90 + 10),
                                        label: place.label, cwd: place.path, state: "idle", line: null,
                                        // Absent for a moment on the real thing too — a shell has
                                        // to start before the assistant is a process anything
                                        // can see, whichever one was asked for.
                                        isClaude: false, assistant: null,
                                        sessionId: null, icon: place.icon });
                        emit();
                    }, 2600);
                }, 320);
            });
        }
    };
})();
