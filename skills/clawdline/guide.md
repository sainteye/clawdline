# Clawdline guide

For an assistant session — Claude Code or Codex — on a machine where **Clawdline Next** runs. It
covers what this daemon serves today, and nothing else: every route below is registered by the
build that printed this guide, and a test fails when one is not. Print it again with
`clawdline guide` rather than trusting a copy; `clawdline guide zh-TW` is the same guide in
Traditional Chinese. `clawdline guide` prints the core and names the other parts; print a part
(`clawdline guide dispatch`) when you reach the work it covers, or `clawdline guide all` for
the full text. Whatever it prints, the core included, starts with `guide-version: <sha256>`;
run the same command with `--since <hash>` and, when that text is unchanged, it prints the one
line `unchanged <hash>` instead. `clawdline guide refused <code>` prints the part that explains a
refusal code, and exits 1 with nothing on stdout when no part names it.

In this guide, a **step** is one checklist entry on an item, a task's **writes** are the paths it
may change, **assignment** is who owns an item, and a **part** is one named piece of this guide.

## 0. If you learned Clawdline from the Swift app, read this first

The Swift app was retired on 2026-09-19: it is stopped, it no longer starts at login, and nothing
answers port 7717. Its directory, `~/.config/clawdline`, is still on disk and is still read — only
read — for history this daemon never held. This daemon is not a copy of that app, and five
differences are where people fall:

1. **Task directories are `<state dir>/tasks`, not `/tmp/.clawdline`.** `/tmp/.clawdline` was the
   Swift broker's; two brokers writing one directory of task ids would have collided where nobody
   looks. Do not hard-code either: read `task_root` from the inventory (§3) and write `task.json`
   under it.
2. **There is no workflow envelope and no workflow route to call.** Messages no longer carry a
   board classification, and a session never opens board cards by itself.
   `POST /v1/orchestrator/sessions/<terminal>/workflow` still exists only so an old helper does not
   fail mid-turn: it answers `workflow_retired`, records nothing, and new code must not call it.
3. **Board participation goes through proposals and decisions** (§10): a session proposes, a
   person answers. There is nothing to `begin` or `deliver`.
4. **A different door.** Port 7727 (or `CLAWDLINE_NEXT_PORT`), state in `~/.config/clawdline-next`
   (or `CLAWDLINE_NEXT_DIR`). Never read `~/.config/clawdline`: its token is not this daemon's and
   is refused with `401 unauthorized`.
5. **Things the Swift app had and this daemon does not:** durable-report promotion (answers
   `501 durable_report_promotion_unsupported`), coordinator succession (answers
   `501 succession_unavailable`), and the brief fields `serialize`
   and `attach_session` (each refused by name as `bad_task`). `reasoning_effort` is supported:
   `high` or `xhigh`, on a `codex` task only.

## 1. Root or child

If your first message said *"You are a Clawdline CHILD agent for task …"*, you are a **child**. The
`CHILD.md` it names governs you: you do not dispatch, you do not send a turn receipt, you sign with
`clawdline task accept` and you finish with `clawdline task finish`. Stop reading here.

Otherwise you are a **root**: an ordinary session a person is talking to. The rest is for you.

If it said *"You are an independently owned Clawdline Feature Root …"*, or a Board item was assigned
to you, print `clawdline guide feature-root` next: it is the whole ordinary path from reading the
item to `done`, and it names the part to print for anything rarer.

## 2. Reaching the daemon

**Use the commands, not hand-built curl, where one exists.** They read the credential inside their
own process, so it never appears in a command line, in `ps`, in their output or in your
transcript. A hand-built curl without that credential answers `401 unauthorized` ("No valid
credential came with this request …"): it is the credential that is missing, not a permission. Run
the command instead.

| Command | What it does |
|---|---|
| `clawdline guide [zh-TW]` | This guide. No daemon needed |
| `clawdline session report --summary "…"` | Records your finished turn (§7) |
| `clawdline session close [--dry-run] [--terminal id]` | Audits and closes a finished Session, never by force (§2a) |
| `clawdline dispatch --title "…" --claims a,b < brief.md` | Dispatches an owned child (§4) |
| `clawdline item show\|steps\|name\|phase\|step-add\|step-done\|doc\|acceptance <item id> …` | Reads and advances a Board item you own (`clawdline guide feature-root`, §10) |
| `clawdline todo add\|list\|done` | This Session's own to-dos, only when the person asks (§10) |
| `clawdline heavy -- <command…>` | Runs a build or test suite in the machine's one compile slot (§11) |
| `clawdline send --to <terminal> "…"` | Relays a message into another session (§8) |
| `clawdline notify --title "…" --body "…"` | Pushes a notification to the person (§9) |
| `clawdline note create --body-file <JSON> [--target <terminal>]` | Leaves one actionable note above a Session (§9a) |
| `clawdline assistants` | What each assistant's account has left |
| `clawdline landings` | Every landing still owed on this machine; `--work-id <item id>`: every landing recorded for one Board item |
| `clawdline leases [--json]` | Who holds the compile slot and each landing lease, and who waits behind them |
| `clawdline sessions [--json]` | The Sessions a send, a wait or a handoff can name, with their state and task |
| `clawdline usage [--session <c> \| --task <id> \| --item <id>]` | What a session, child task or Board item spent, by category; yours by default |
| `clawdline cloud pair [--offer <code>]` | Pairs one Cloud browser with this machine |
| `clawdline task show [--json] <task id>` | One child task compactly: state, verdict, summary, leftover titles, verification, landing, checkout (§5) |
| `clawdline task wait <task id>… [--timeout 9m] [--any]` | Waits until the children finish (all, or `--any` one), shows each as `task show` does and closes its notice. Exit 0 all succeeded, 1 one failed, 5 one was cancelled and none failed, 3 timed out, 4 a task could not be read; 4 over 3 over 1 over 5 (§5) |
| `clawdline task cancel <task id> --reason "…"` | Stops a child you dispatched by mistake: its tab is closed, its writes and slot are released, a branch with commits is kept for you (§5) |
| `clawdline task ack <task id> <notice id>` | Closes a completion notice by hand; rarely needed, since `task show` and `task wait` close it (§5) |
| `clawdline task accept <task dir>` | A child signing for its briefing. Roots never run it |
| `clawdline task finish <task dir>` | A child's completion. Roots never run it |
| `clawdline webhook fire [--url-file <path>] [--deliver-within 60s] [--timeout 60m] [--no-wait]` | Starts a schedule through its Cloud webhook, on any machine, and waits for its result; the exit code says how it ended ("Schedule future work"). No daemon needed |

The orchestration commands above (not `webhook fire`) print the daemon's JSON on success; on a refusal they print
`refused, <status> <code>: <message>`, then each scalar the refusal carries as `key: value`, one per
line, and its remediation last, and exit 1. Cloud commands use their own human-readable
success and error output. `--port` overrides the port.

`clawdline usage` is the token ledger (`docs/token-ledger.md` in the repository): what each token was
spent on — `board`, `protocol`, `rules`, `impl`, `delegate`, `harness`, `talk`, `compaction`,
`other`. With no flag it reads your own session, named by `CLAUDE_CODE_SESSION_ID` or
`CODEX_THREAD_ID`. It prints one header line (calls, peak context, cost), one line per category by
cost — share, tokens, cost — and then every gap; `--json` prints the daemon's answer. `rules` is an
upper bound, and says so: a guard run in one shell command with other work takes that whole command.
The routes are `GET /v1/usage/sessions/<conversation>`, `GET /v1/usage/tasks/<task id>` and
`GET /v1/usage/items/<item id>`, read with a paired device or the orchestrator token. A session the
ledger has not read, or can no longer read, answers `not_yet_read`, `transcript_missing` or
`transcript_unreadable` — never an empty total; an id nobody knows is 404 `unknown_session`,
`unknown_task` or `unknown_item`. Whether the ledger is still reading is `usage` in
`/v1/diagnostics`.

**Waiting on a long command.** `clawdline heavy`, `clawdline dispatch` and a long test run print
nothing while they wait, and end on their own. Wait for one with **one long wait**, not by
checking it every few seconds: each check is a turn that rereads your whole context, and a token
review counted 520 such turns (72.2M tokens) over ten items, mostly on queued `heavy` runs.

- **Claude Code:** one Bash call with a long `timeout` (up to `600000` ms), or `run_in_background`
  and then nothing until its completion notification arrives. Not a loop of `sleep` and `tail`.
- **Codex (codex-cli 0.157.1, code mode):** put `// @exec: {"yield_time_ms": 600000}` on the
  first line of the `functions.exec` cell. After `exec_command` returns a session ID, await
  `write_stdin` with empty `chars` and `yield_time_ms: 300000`; if it still runs, repeat inside
  that same cell. Measured: the outer cell stayed open for 330 seconds with `600000`, while an
  empty `write_stdin` waited up to 300 seconds. If the outer cell yields, use `wait` with a long
  `yield_time_ms` to collect it.

`clawdline heavy` waits at most `--max-wait` (default 30m) and then exits 75 without running the
command; a longer wait than your tool allows is a background run.

**Curl to an orchestrator route.** Read `<state dir>/orchestrator-token` and send it in the
`X-Clawdline-Orchestrator` header. Keep the token out of command arguments: use
`DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"`, then
`-H @<(printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")")`.
Use `curl --fail-with-body`; every POST with a JSON body also needs
`-H 'Content-Type: application/json'` (otherwise `415 unsupported_media_type`).

### Pair a Cloud browser

Pairing changes who may read this machine. A paired browser may read it immediately and, when
Cloud `commands` are on, may drive it. Run a pairing command only when the person explicitly asks
to pair that browser or gives you the exact pairing command or offer. Pairing does not turn
commands on; that remains a separate setting.

There are two supported directions:

1. **The browser shows an offer.** Run the exact line it gives you on the machine:

   ```sh
   clawdline cloud pair -offer '<code>'
   ```

   Keep the single quotes. The offer is an opaque, short-lived, one-use secret: do not decode,
   edit, store, or repeat it in the final answer. If it expires or was already used, get a fresh
   offer from the browser instead of retrying or modifying it.
2. **The machine makes the invitation.** Run `clawdline cloud pair`. It prints a one-time
   `https://app.clawdline.com/#pair=…` link and waits. The person opens that complete link in the
   browser they want to pair, while signed in to the same Clawdline Cloud account. Treat the link
   like the offer: do not publish or retain it.

Success prints three lines: `paired` names the browser device id, `browser` is the browser
fingerprint, and `machine` is the machine fingerprint. Compare the browser fingerprint with the
one shown in the browser and the machine fingerprint with the one shown for this machine. A
mismatch is not success: immediately run `clawdline cloud revoke <device-id>` using the `paired`
id, then report the mismatch. `clawdline cloud devices` lists the current browser roster and its
local trust status; it is also the read-only check to use after pairing.

These commands go through the running local daemon. If one fails, report its exact stderr. Do not
turn Cloud on, log in, enable commands, rotate keys, or replace the supplied offer unless the
person separately asked for that change.

**Where things are.**

- Port: `CLAWDLINE_NEXT_PORT`, else **7727**. Loopback only: `http://127.0.0.1:<port>`.
- State directory: `CLAWDLINE_NEXT_DIR`, else `$XDG_CONFIG_HOME/clawdline-next`, else
  `~/.config/clawdline-next` (`%APPDATA%\clawdline-next` on Windows).
- `GET /v1/health` needs no credential and answers `served_by: "clawdline-go"`. Use it to tell
  "not running" from "refused".

**Credentials.** Three exist, and a session uses the first:

| Credential | Where | Sent as | Opens |
|---|---|---|---|
| Orchestrator token | `<state dir>/orchestrator-token` | header `X-Clawdline-Orchestrator` | Everything under `/v1/orchestrator/`, `/v1/work/`, `/v1/board`, `GET /v1/places`, `POST /v1/artifacts/images` |
| Task secret | chosen by the root at dispatch | header `X-Clawdline-Task-Secret` | A child's own routes under `/v1/orchestrator/tasks/<id>/`, and `POST /v1/orchestrator/proposals` |
| Device token | `<state dir>/local-token`, or a paired device's | `Authorization: Bearer` | The console's routes (`/v1/sessions/…`). A session does not need it |

The orchestrator token sent as `Bearer` is compared with devices and refused. A wrong or missing
one gets `401 unauthorized`, which says so: the token is missing or is not this daemon's, or the
device is not paired. `clawdline doctor` prints the directory and port the CLI reads.

**When you must use curl**, keep the token out of the command line:

```sh
DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"
PORT="${CLAWDLINE_NEXT_PORT:-7727}"
auth() { printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")"; }
curl --fail-with-body -sS -H @<(auth) "http://127.0.0.1:$PORT/v1/orchestrator/inventory?project=$PWD"
```

- `--fail-with-body`: without it a refusal exits 0 and reads like success.
- **Every POST with a body needs `-H 'Content-Type: application/json'`**, or it is refused with
  `415 unsupported_media_type`. `curl -d` alone sends a form type.
- Bodies are capped at 2 MiB unless a route says less.
- A tmux terminal id such as `%47` goes into a path escaped as one segment: `%2547`.

**Refusals come in two shapes.** Branch on the code, never on the sentence:

- `{"error":{"code":"…","message":"…","request_id":"…", …extras}}` — the gate and the broker.
  Extras such as `retry_after` sit inside `error`.
- `{"error":"<code>","detail":"…"}` — route misses, wrong methods and some reads.

A route this daemon does not own is refused as `501 not_implemented`, and the refusal names the
route. That is the answer on any ordinary machine. It is only forwarded when somebody deliberately
put another daemon behind this one with `CLAWDLINE_NEXT_UPSTREAM_PORT`, and then a `502
upstream_unreachable` names the address that did not answer. Neither is an answer from this
daemon. Before 2026-09-19 the forwarding was on by default and went to the Swift app on 7717, so a
note written then will say an unowned route reaches that app; it does not.

### Set up a Project's Clawdline display

Use this part when the person asks you to make the Project you are working in legible in
Clawdline. The outcome is not “some files exist”; it is that the Project has a truthful name and
mark, long-running work can report progress, and its development servers can be seen without
Clawdline starting them.

Start by reading this repository's instructions, README, deploy/build scripts and existing process
manager configuration. Preserve the commands the Project already uses. Do not add a second deploy
path or process supervisor merely for Clawdline, and do not start, stop, restart or deploy anything
unless the person asked for that operational change. Configuration and an actual deployment are
different work.

Work through these four checks, skipping a check only when it genuinely does not apply:

1. **Project.** Run `clawdline project list`. If this checkout is absent, add its repository root
   with `clawdline project add <absolute-root>` and list again. This records a place from which a
   Session may start; it does not change the repository.
2. **Name and icon.** Clawdline derives a stable icon when none is configured. If the person wants
   a deliberate name or pixel mark, preserve every other entry in `~/.claude/project-icons.json`
   and edit only the longest-containing path for this Project. The format is documented in the
   Clawdline repository's `docs/project-status.md`; the Projects page can also copy an existing
   resolved icon without hand-editing JSON. A global user file is not repository content: show the
   exact proposed entry before changing it when the request did not already authorize that edit.
3. **Deploy and long work.** Clawdline only reads status receipts; it never runs a deploy. For a
   GitHub repository, a deploy receipt is
   `~/.claude/statusline-cache/ghrun-<owner>-<repo>.json`, where owner and repo come from `origin`.
   The producer that already knows the run writes `state` (`running`, `ok`, `fail` or `none`),
   `label`, `url`, `started_at` and a measured `typical_seconds`, atomically. For a local build,
   test, import or deploy command, use `clawdline-progress run --label <label> -- <command>` when
   that helper exists, or implement the `run-<path>.json` contract from `docs/project-status.md`.
   Never invent a duration; omit it until it has been measured. A killed producer must not leave a
   permanent running state.
4. **Development servers.** Add or update `.devstack.json` at the nearest deployable root. The Go
   daemon currently reads the declared `processes` and probes their loopback `port` or opens their
   `url`; it does **not** execute `status`, `up`, `down`, `restart` or `logs` commands from the
   browser. Prefer the smallest truthful Tier 0 file, for example:

   ```json
   {"version":1,"name":"myapp","processes":[{"name":"api","port":8002},{"name":"web","port":3001}]}
   ```

   A process with no stable port or URL should not be guessed into the file. Do not probe or
   restart production while verifying a development declaration.

Verify each changed layer separately: `clawdline project list` names the checkout; every JSON file
parses; the repository's own tests for changed scripts pass; `GET /v1/devstacks` shows declared
servers as running, stopped or unknown rather than silently omitting them; and a Session in the
Project shows a fresh progress/deploy receipt. If a read is unavailable, malformed or stale, say
which one and leave it unknown — never report absence as success. Finish by listing what was
configured, what was intentionally not applicable, and any user-owned file changed outside git.

## 2a. A Feature Root's ordinary path

This is everything an ordinary Feature Root — a Session that owns one Board item — runs, in order.
Every step is a command: the commands carry the credential, and a hand-built curl to the same route
is refused. Anything rarer is one `clawdline guide <part>` away; the pointers are at the end.

**1. Read the item.** `clawdline item show <item id>` prints its kind, phase, acceptance criteria,
captured gates, the acceptance version (`acceptance vN`), for a Feature the person's Needs
independent review switch, its steps, and every document with its body. That is
the record you work from. `clawdline item show <item id> --doc <doc id>` prints one document's
body alone, to pipe into a file; `clawdline item steps <item id>` is the same record without the bodies. Every item
write prints `wrote …; item <id> is at version N`, a short item summary, and an `item show`
hint. Read the full acceptance, steps, and documents with `item show`. Writes act on the item's current version unless you pass
`--expected-version`.

If your ASSIGNMENT.md has a **HANDOFF** heading, you are taking over an item another Session left
mid-flight (reassigned after implementing began, before done). Read the pack it names
first, before planning. The daemon built it from its own records and git, without asking the
previous owner: the tasks bound to the item and their results, commits not yet landed, each
worktree's uncommitted changes saved as a patch with its sha256 and the `git apply` command that
restores it on its base, the previous owner's last message (or why it could not be read), and the
phase and open steps. The patches live beside ASSIGNMENT.md and outlast the worktrees. Continue
from there; do not start over.

**2. Name your Session**, if it was opened for this item:
`clawdline item name <item id> "<task name>"`, once, after reading the objective and scope. It renames the Session, not the item.

**3. Before implementing.**

- Captured planning on and no acceptance criteria: write observable ones with
  `clawdline item acceptance <item id> --body-file acceptance.md`.
- Needs independent review checked, or an Epic: the reviewed-plan path in `clawdline guide epic`
  comes first. Unchecked: no plan, no review child.
- Multi-stage work with no steps: `clawdline item step-add <item id> "first" "second" …` (two to
  eight steps you can verify one at a time; a single change takes none).
- Then `clawdline item phase <item id> implementing`.

**4. Work in this Session by default.** Investigate, implement, verify and land the Feature
yourself. Dispatch only when a concrete need makes a separate Session useful: genuinely independent
parallel work, different tools or permissions, or required independent review. Say why before
dispatching; a routine investigation or implementation alone is not a reason.

When dispatch is needed, keep synthesis, integration and landing here:

```sh
clawdline dispatch --title "…" --claims a.go,b.go --isolation worktree --work-id <item id> < brief.md
```

- `--work-id` binds the child to the item, so its landing counts as the item's. Repeat it when one
  child does several items: the first is the child's line, and the landing counts for every one.
- The title is one line of at most 60 characters saying what will be different. Any colon (`:` or
  `：`) is refused, because it joins an observation to an explanation; so is "the user" as the
  subject, or a title opening with a code-formatted identifier. Each answers `bad_task` with
  `title: …` saying which.
- The brief stands on its own. Put in it the facts you have already verified, each with its
  `file:line` or the command that showed it, so the child does not rediscover them.
- An investigation or Explore child's brief also states its stop condition — the question that,
  once answered, ends the task — and a turn limit.
- Read-only work is `--claims ""`. Every flag, and every refusal code, is in
  `clawdline guide dispatch`.

**5. If a child finishes**, a `<clawdline-notice>` line is typed into your composer. Run
`clawdline task show <task id>`, then integrate the delivery; reading it closes the notice, so there
is no separate ACK. To block until your children finish instead, run
`clawdline task wait <task id>…` (default `--timeout 9m`, `--any` for the first one). Integrate a worktree child by **merging its branch** into the target. **The merge records the
landing by itself** within a few minutes: do not post a landing by hand. `clawdline landings` lists
what is still owed. A child dispatched with `--claims ""` that wrote nothing is recorded
`nothing_to_land` by the broker. Anything else is `clawdline task land <task id> <state>`
(`clawdline guide landing`).

**6. Completion report**, when finding the cause took substantial investigation (a direct,
observed fix needs none). Add it before `done`: once the item is done it is unassigned, and the
report answers `409 not_item_owner`.

```sh
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

Write it for the person who reported the problem, in Markdown, with no private data.

**7. Finish the item.** Complete each step once it is verified with
`clawdline item step-done <item id> <step id>`. Commit and push direct work from a disposable
worktree; merge a child's branch when one was used. Once landed, one command takes the item to
`done`. A child's recorded landing supplies commit, target and remote; direct work names them:

```sh
clawdline item finish <item id> --verification "what was run and what it showed" \
  --commit <sha> --target main --remote origin \
  --deployment "what went live, where, which version"      # omit landing flags for a landed child
```

Or advance one phase at a time:

```sh
clawdline item phase <item id> deploying        # a landed --work-id child is the evidence
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin   # otherwise; push first
clawdline item phase <item id> deploying --no-landing-reason "why there is no code"    # work with no code
clawdline item phase <item id> done --deployment "what went live, where, which version"
clawdline item phase <item id> done --no-deployment-reason "why nothing needs deploying"
```

`done` takes `--deployment` or `--no-deployment-reason` as the item's deployment policy says. When
`clawdline item steps <item id>` prints a gate line, this item keeps the longer path that line
points to.

**8. Report the turn**: `clawdline session report --summary "…"` (§7).

**9. Leave the owning Session open.** Completing or cancelling a Board item releases its assignment;
it does not end the Session that owned it. After `session report`, leave the Session available for
follow-up work. Do not run `clawdline session close` merely because the item reached `done` or
`cancelled`. A person may explicitly ask to close the Session later. The broker separately closes
an agent-dispatched child's tab after that child's task ends, under the child-tab rule in
`clawdline guide child`.

**When something is refused.** `version_conflict`: run the same command again; it rereads the
version. `steps_incomplete`: a step is still open. Any other code: §12, then the part that covers
it.

**Rarer work, one part each:** `clawdline guide board` — proposals, decisions, to-dos, reopening a
done item, waiting on the person, gates and every phase refusal; `clawdline guide epic` — plans,
plan review, an Epic's child items, personas; `clawdline guide landing` — landing by hand,
handoffs (a long Root's milestone handoff too), Root assignments; `clawdline guide running` — stalled children, leftovers, respawn.

## 3. Before you dispatch: read what is already there

Another session's work may already be doing your job, and it is invisible from the shared tree: a
finished delivery on an unmerged branch shows up in no `git status`. Read first.

```
GET /v1/orchestrator/inventory?project=<absolute repo path>[&claims=a,b]
```

- Answers `generation`, `task_root`, and four lists: `live`, `unlanded`, `droppable`,
  `unreadable`. Every row carries a `do` the daemon would accept. With `claims`, each live row
  says what it `overlaps`.
- **`generation` is required to dispatch** (§4). It is 16 hex characters over the rows' sealed
  fields; it moves when a row starts, ends or changes writes.
- **`task_root` is where your `task.json` goes.** It is this daemon's own field; the Swift broker
  had none because it hard-coded `/tmp/.clawdline`.
- `400 bad_request` when `project` is not an absolute path inside a Git repository.

Also worth one read:

- `GET /v1/orchestrator/inflight?project=…` — every outstanding line of work in the repository,
  who has it and what it claimed.
- `clawdline assistants` — per assistant: `availability` (`ok`, `low`, `exhausted`, `unknown`),
  `windows`, `stale`, `resets_at`. Choose who to dispatch to after reading it; nothing refuses a
  dispatch for quota.

**Should it be dispatched at all?** Work that splits into independent pieces goes faster in
parallel. A chain where every step depends on the last does worse split up, because each handoff
breaks it. Diagnosis, work smaller than its own briefing, and anything somebody is waiting on stay
in your own session. The machine's house rules are in `<state dir>/dispatch-policy.md` (and the
person's `dispatch-policy.local.md`); every child receives them in its briefing.

## 4. Dispatch an owned child

An owned child is a bounded task under you. **You keep synthesis, integration and landing.**

**One command does all four steps below**, with the brief on stdin or in a file:

```sh
clawdline dispatch --title "…" --claims a.go,b.go [--isolation worktree] [--assistant codex] \
  [--permission-mode ask|edits|full] [--timeout 90] [--kind k] [--deliverable p] [--model m] \
  [--persona <id>] [--work-id uuid …] [--task-id uuid] [--label "…"] [--project-dir D] < brief.md     # or --instructions-file brief.md
```

It makes the id and the secret, reads the inventory for `generation` and `task_root`, writes
`task.json`, posts the task, and on one `stale_inventory` reads the inventory again and resends
once. It prints `dispatched <id> <state> [worktree <path>]`, then one line per warning — the
daemon's, and each live task whose writes overlap yours. `--json` prints the daemon's answer
instead. A refusal is `refused, <status> <code>: <message>` on stderr, then its extras and remediation one
line each, and exit 1; the table at the
end of this part says what each code means. The root is your conversation, from
`CLAUDE_CODE_SESSION_ID` or `CODEX_THREAD_ID`, else `--conversation`; the child's assistant is
yours unless `--assistant` says otherwise; the project is this directory's git top-level unless
`--project-dir` says otherwise. `--claims ""` declares a child that writes nothing. While the
daemon opens the worktree and the child's tab it prints nothing; it is one request that answers
when the child exists, so wait for it once (§2, "Waiting on a long command"). The secret is
never in argv, in `task.json` or in what it prints, and the token is read as every thin command
reads it.

For a review that may need retrying, choose a lowercase UUID before the first call and pass it
as `--task-id` on every attempt. The command keeps a private copy of the original dispatch
intent beside `task.json`, so an identical retry can resend even after the daemon rewrites the
brief. The broker returns the original task id with `(replayed)`; a changed brief is refused locally. If
the command times out or its output is lost, check `GET /v1/orchestrator/tasks/<id>` before
assuming failure. A missing task can be retried with the same id and brief. An explicit
refusal did not create a task and can also be retried after correcting its cause.

`--persona <id>` launches the child as a built-in persona (`persona` in `task.json`); an id this
build lacks is refused locally. No kind gets one by default, `plan_review` included: name
`code-reviewer` yourself when you want it. `GET /v1/personas` lists the ids this build carries; what a persona is is in the Epic
part of §10 (`clawdline guide epic`).

The steps it takes, for a caller without the binary:

**1. Choose an id and a secret.**

```sh
TASK_ID=$(uuidgen | tr 'A-Z' 'a-z')     # 36 characters, lowercase
SECRET=$(openssl rand -hex 32)          # 64 lowercase hex
```

The secret goes from you to the daemon in the POST body, and from the daemon to the child in the
one line it types there. It is not in `task.json` and not in the dispatch's answer, and you do not
need it again. (A respawn is the one answer that carries a secret: its copy's new one.)

**2. Read the inventory** (§3) for `generation` and `task_root`.

**3. Write `<task_root>/<TASK_ID>/task.json`.** The daemon reads the brief from this file, not from
the request. At admission it validates it, rewrites `task.json` from what it admitted, and writes the
child's `CHILD.md` from the same record — title, instructions, writes, deliverables, kind and timeout
included — so the task the child reads is the one that was validated, and it does not read `task.json`.

| Field | Rule |
|---|---|
| `clawdline_protocol` | `1` |
| `task_id` | the same id |
| `assistant` | `claude` or `codex` |
| `project_dir` | absolute path to an existing directory |
| `title` | shown on screen: one line of at most 60 characters saying what will be different. A colon (`:` or `：`), "the user" as the subject, or an opening code-formatted identifier is refused as `bad_task` (`title: …`) |
| `instructions` | required, at most 16 KiB. They must stand on their own: the child knows nothing else. Carry the facts you already verified, each with its `file:line` or command; an investigation child also gets a stop condition and a turn limit |
| `claims` | **required**: at most 32 relative paths the child may write. `[]` means it writes nothing and is warned about (`claims_missing`) |
| `isolation` | `none` (default) or `worktree` for a private checkout on its own branch |
| `permission_mode` | `ask`, `edits` or `full` |
| `timeout_minutes` | 1–240, default 30 |
| `kind`, `deliverables`, `model` | optional; `model` is `[a-z0-9._-]`, at most 64 characters |
| `work_id` | optional UUID of the board item this serves |
| `persona` | optional built-in persona id (`GET /v1/personas`); none by default |
| `auto_compact_window` | optional, Claude only: the context size in tokens (50000–1000000) the child compacts at, or `null` for none. Absent follows the machine's `claude_auto_compact_window`, which is off unless the person set it. For comparing runs, not for everyday briefs: a compaction may lose detail |
| `root` | **required**: `{"session_id": "<your conversation id>", "assistant": "claude"\|"codex", "project_dir": "<the same absolute repository path as project_dir>", "label": "…"}`. A role-scoped root needs `root.project_dir` for the daemon to verify its Project scope. |

**Match the worker surface and launch mode to every tool the child must use before dispatching it.**
The brief names the required tools and the root proves that the chosen surface supplies them. A
Codex CLI child does not gain the ChatGPT desktop app's built-in `@Browser` merely from a permission
flag. For a UI, accessibility or responsive-layout review, route the work to a surface that really
has Browser/Computer Use, or name an acceptance-equivalent local browser harness such as
Playwright/Chrome CDP and prove it is installed. When that surface may request app, origin or GUI
access, dispatch with `--permission-mode ask`: Codex `full` means a non-interactive shell launch
(`--ask-for-approval never`), not every tool, and Auto-review cannot review a request that is never
created. At task start the child exercises each required tool, not just checks a command name. If
one is unavailable, it reports the exact gap immediately and the root restores access or
redispatches. It does not finish a tool-dependent acceptance check as unverified because the root
chose an incompatible worker.

**`root.session_id` is your conversation id, never a terminal id.** Claude Code exports it as
`CLAUDE_CODE_SESSION_ID`; Codex as `CODEX_THREAD_ID`. It is how the daemon groups the child under
you and tells you when it finishes. To check it names this tab:
`GET /v1/orchestrator/whoami?conversation_id=<id>` answers `terminal_id`.

**4. Dispatch**, with the orchestrator token:

```
POST /v1/orchestrator/tasks
{"task_id": "…", "secret": "…", "inventory_generation": "…"}
```

Send the body through stdin (`jq -n … | curl --data-binary @- -H 'Content-Type: application/json' …`)
so the secret stays out of argv.
The answer is `{ok, task, warnings?}`. Read `warnings`: `claims_overlap`, `claims_missing`,
`claims_ignored_for_worktree`, `dirty_worktree_base`, and `work_not_placed` (the named item could
not be moved onto the board yet; the board's sweep does it within a tick). Posting the same id again answers the stored
task with `replayed: true`, so a retry is safe.

A tab that fails to open still answers 200, with `task.state: "spawn_failed"`.
`POST /v1/orchestrator/tasks/<id>/respawn` (orchestrator token) opens a copy with a new secret, at
most twice per original.

Dispatched the wrong child — the wrong brief, the wrong scope, or the same work twice? Do not wait
for it to finish or time out while it holds a slot and its writes: `clawdline task cancel <id>
--reason "…"` stops it now (§5).

**Refusals you will meet**, in the order they are checked:

| Status | Code | What to do |
|---|---|---|
| 409 | `task_unreadable` | A task with this id is stored but cannot be read; do not resend under the same id |
| 422 | `bad_task` | The message names the field. Includes "No readable task.json under …" — check `task_root` |
| 422 | `claims_required` | Add `claims` |
| 422 | `root_session_required`, `root_assistant_required` | Add `root.session_id` and `root.assistant` |
| 403 | `session_scope_mismatch` | Check that `root.project_dir` is present and matches the root Session's Project and role snapshot. Correct the brief or CLI; do not ask the person to change Project settings. |
| 422 | `detached_route_required` | You sent `root.poll_only`; that is detached automation (§6) |
| **409** | **`stale_inventory`** | Your `generation` is missing or old. The whole current inventory is inside the error: read it, decide again, resend with its `generation` |
| 422 | `work_not_found`, `work_other_project`, `work_closed` | The `work_id` you named is no item, another Project's, or closed |
| 422 | `also_work_not_found` | An id in `also_work_ids` is no Board item; it is checked like `work_id` after that |
| 503 | `store_unavailable` | The board could not be read to check the named item; nothing was started, send it again |
| 409 | `graph_*` | A task-graph admission rule (the `graph` field) |
| 409 | `no_child_capability` | This platform cannot open a child; `missing` says what |
| 429 | `squad_launch_capacity` | Too many persona launches are still waiting for their Session; try again later |
| 429 | `rate_limited` | Too many dispatches in ten minutes |
| 422 / 409 | `root_unresolved`, `conversation_ambiguous` | Your conversation id matches no live session, or more than one. Fix it; do not switch to detached |
| 403 | `session_actor_required` | A root opened with a role must dispatch with its own Session's squad capability, from that Session |
| 403 | `session_scope_mismatch` | Also here: the root Session's Project does not match its role snapshot |
| 503 | `squad_policy_unavailable` | The role assignment settings could not be read; nothing was started |
| 409 | `persona_disabled_for_auto_assignment` | That persona is turned off for automatic assignment in the target Project |
| 429 | `over_capacity` | Your child slots (default 5) or the machine's are full; `retry_after` |
| 409 | `workspace_busy` | Another root's writes overlap; the error names the blocking task |
| 409 | `worktree_unavailable` | The private checkout could not be made |
| 429 | `terminal_busy` | Every terminal-write lane is busy; `retry_after: 5` |

## 5. While it runs, and when it finishes

The child signs for its briefing (`clawdline task accept`, which posts `/accepted` or leaves
`accepted.json`), may send one progress note when its plan changes (`/progress`), may push up to five
notifications (`/notify`), and finishes by writing `result.json` and running `clawdline task finish`.
You do not call those routes.

- `clawdline task show <id>` — one task, with its state (`GET /v1/orchestrator/tasks/<id>`).
  `GET /v1/orchestrator/tasks` lists them (`?state=`, `?limit=` up to 500).
- **When it finishes, the daemon types a `<clawdline-notice>` line into your composer.** Its `body`
  is one short sentence: the task, how it ended, the facts that are this delivery's alone (a stall,
  released writes, its branch, how many leftovers) and the one command to run,
  `clawdline task show <id>`, which closes the notice once it has printed the task. Its JSON
  (version 3) carries `task`, `state` and `notice_id`, and `outstanding`, `leftovers` and
  `claims_released` only when they say something; the result is what `task show` prints. A line
  that could not be typed is retried on a 5→300-second ladder. Once it is on your screen and you
  have not read the task, it is not typed again whole: a short `task_reminder` line naming the
  same command is, on a 2→30-minute ladder — eight typings in all, then it gives up. It never
  types while you are showing a menu. A menu does not use up those eight: the line waits, for up
  to 12 hours, and is typed once the menu is gone. The route `task show` sends:

  ```
  POST /v1/orchestrator/tasks/<id>/completion/ack   {"notice_id": "…"}
  ```

  `clawdline task show <id>` and `clawdline task wait <id>…` send it for a finished task after printing it;
  `clawdline task ack <id> <notice_id>` sends it by hand and prints one line. A second ACK answers
  `changed: false`. Unacknowledged notices are listed at
  `GET /v1/orchestrator/completions`; `POST /v1/orchestrator/completions/reconcile` re-arms them.
  A notice that gave up is typed once more the next time your session is idle.
- **You may never see the line, and you still learn of it.** Your own
  `GET /v1/work/v2/agent/session-todos/<conversation id>`, which you read at every turn boundary,
  lists `unacknowledged_completions` — each child of yours that finished and that you have not
  acknowledged, with `task_id`, `title`, `state`, `kind`, `result_path`, `notice_id` and `ack_path`, whether
  its notice is still pending or gave up. `clawdline session report` prints them after its receipt.
  For each: `clawdline task show <id>`, then integrate it; the read is the ACK and takes it off both lists.
  `task show` prints the summary whole and counts what it leaves out; `--json` is the daemon's whole
  answer, symbols and artifacts included. Read `result.json` itself only when that is not enough.
- **A delivery that names leftovers** — things the child says it did not do — changes nothing by
  itself. `task show` lists their titles. To put one to the person,
  `POST /v1/orchestrator/proposals {"session_id":"<yours>","task_id":"<id>","leftover":"<its title>"}`;
  they answer track, later (Backlog) or no, and nothing reaches their board until they do.
- **A child that stops right after its briefing is nudged, then reported.** If a child whose
  briefing was typed has not signed for it and its screen reads idle — prompt drawn, composer
  empty, no menu, no working line — for 5 minutes, the daemon types it one line naming its
  `CHILD.md` (never the secret). Still unsigned and idle 5 minutes later, the task ends
  `spawn_failed` with a verdict that says it stalled, and you get a notice of `"kind":
  "task_stalled"` instead of `task_finished`. Respawn it (`POST /v1/orchestrator/tasks/<id>/respawn`)
  or dispatch again, then ACK it. A child that is working, showing a menu or has signed is never
  typed at.
- **Cancel a child you dispatched by mistake** — the wrong brief, the wrong scope, a duplicate:
  `clawdline task cancel <id> --reason "wrong brief"`
  (`POST /v1/orchestrator/tasks/<id>/cancel`, `{"reason":"…"}`). The reason is required, at most 500
  bytes. The task ends `cancelled` with the reason as its verdict, its tab is closed, its writes and
  child slot are released, and you get one notice saying it was cancelled and why. **Commits are not
  thrown away:** a child that committed keeps its branch and checkout, and its landing stays pending
  with a note that says how many commits are on it; `task show` and `clawdline landings` show it.
  Merge what you want of it, or record it with `clawdline task land <id> abandoned`. Only the root
  Session that dispatched the task, or the person from the console, may cancel it; anyone else is
  refused `403 not_task_root`, and a Session opened with a role must send its own capability
  (`session_actor_required`; the command does that for you). A task that has already ended answers
  `409 task_already_terminal` with its `state`; running the same cancel again answers the same success
  with `replayed: true`. `clawdline task wait` exits 5 when a task it waited for was cancelled.
  Otherwise a task ends by finishing, failing or timing out.
- **A finished child is not landed code.** Its work sits in the shared tree or on its branch until
  you integrate it.

## 6. Landing, and the other three kinds of work

**After you merge a child's branch, do nothing more**: the broker records `landed` itself (below).
A child that declared no writes (`--claims ""`), finished by itself, and left nothing on its branch
or in its checkout is recorded `nothing_to_land` by the broker before its notice is typed. A
landing recorded by hand is for what neither covers — a cherry-pick, an `incorporated` delivery,
`nothing_to_land`, `abandoned` — and is one command, sent with the orchestrator token:

```
clawdline task land <task id> <landed|incorporated|abandoned|nothing_to_land|pending> \
  [--target <branch>] [--commit <sha>] [--carrier-task <task id>] [--note "…"]
```

It is this route, which a script may call with the `X-Clawdline-Orchestrator` header:

```
POST /v1/orchestrator/tasks/<id>/landing
{"state": "pending" | "landed" | "incorporated" | "abandoned" | "nothing_to_land", "target": "<ref>", "commit": "<sha>", "carrier_task": "<task id>", "note": "…"}
```

- Only these keys, plus `delivery`, which is accepted and not used; any other key is refused.
  `pending` and `abandoned` accept the task secret or the orchestrator token; `landed`,
  `incorporated` and `nothing_to_land` accept the orchestrator token only.
- `landed` needs `target` and `commit`; `incorporated` needs `target`, `commit` and
  `carrier_task`, the other task whose verified landing carried this delivery. The daemon
  **checks either in Git**; otherwise `409 unverified_landing` with one of these `reason`s:
  `commit_unresolved`, `target_unresolved`, `not_on_target`, `base_unknown`, `predates_dispatch`,
  `delivery_unknown`, `nothing_delivered`, `not_the_delivery`, and for `incorporated`
  `carrier_required`, `carrier_is_delivery`, `carrier_unresolved`, `carrier_not_landed`,
  `carrier_repository_mismatch`, `carrier_target_mismatch`, `carrier_commit_mismatch`,
  `delivery_is_ancestor`.
- `nothing_to_land` is refused with `409 wrote_to_repository` when the task did write.
- A settled landing cannot change: `409 invalid_transition`, or `409 landing_conflict` for a
  different value.
- **A merge records itself.** Once a finished task's branch is merged into its target, the broker
  records `landed` on its own within a few minutes, through the same Git check, with the target's
  head as the commit. With no target on record it names one only when the primary checkout's branch
  is the single branch holding the delivery. A cherry-pick, an `incorporated` delivery and
  `nothing_to_land` are still yours to record.
- **The completion notice names the state of the branch when the task ended**, and each asks for
  one thing, as a command. *Nothing is committed on its branch*: a landing is proved from that
  branch, so as it stands nothing could ever be recorded as landed — commit in its checkout, on that
  branch, while the checkout is still on disk, or `clawdline task land <id> abandoned`. *Committed on
  its branch*: merge that branch into its target; the merge records the landing. *Could not be
  read*: look at the branch, then record it. *Wrote the shared checkout*: `clawdline task land <id>
  landed` with the commit that carries that work onto its target, or `abandoned`. *It wrote nothing,
  and the broker recorded nothing_to_land*: only the ACK is left.

`clawdline landings` (`GET /v1/orchestrator/landings`) is every pending landing on the machine, each
with an `ownership.status`. `unknown` is not "nobody": it means the evidence could not be read.
`503 landings_incomplete` means some rows could not be read, and no shorter list is offered in
their place.

`clawdline landings --work-id <item id>` (`GET /v1/orchestrator/landings?work_id=<item id>`) is
every landing **recorded** for one Board item instead: `{"work_id", "landings": [...], "at"}`, each
row with its `id` and `source` — `task` (a bound child's landed or incorporated record; the id is
the task id), `root` (the record `item phase deploying --commit` wrote) or `phase_event` (a copy an
older daemon kept in the item's history). A bound task whose record cannot be read is a row with
`state: "unknown"`, never left out. The item read carries the same rows as `landings`. An item that
does not exist is `404 work_not_found`.

**Two roots landing into one checkout** take a landing lease first (§11).

The other three kinds of work each have their own route. Which one is a boundary, not a detail:

| Kind | Route | What it is |
|---|---|---|
| **Handoff** | `POST /v1/orchestrator/handoffs` | You give an existing line of work, with its full state, to a new session |
| **Root assignment** | `POST /v1/orchestrator/root-assignments` | A new, independently owned Root for a new feature |
| **Detached automation** | `POST /v1/orchestrator/detached-tasks` | Unattended work with nobody to report to |

**Handoff.** Write `<state dir>/handoffs/<handoff_id>/handoff.md` first (the list route answers
`package_root`). It should carry three headings: **REFERENCES** (everything the receiver must
read), **VERIFICATION** (questions it answers from those sources before continuing) and **OPEN
THREADS** (where to pick up). Then post, with a closed body:

```
{"handoff_id": "<uuid>", "from_session": "<your conversation id>", "coordinator_plain_handoff": true,
 "project_dir": "/abs", "assistant": "claude"|"codex", "model": "…", "title": "…"}
```

The receiver is told to read the file, walk its references, answer its verification questions and
continue. At opening, the handoff captures the sender's open Board items in that Project. Once the
receiver has a conversation id and its first conversation record is observed, those items' active
assignments and owners move to the receiver in one transaction. A failed handoff leaves ownership
with the sender. An item already closed, moved to somebody else or being assigned separately is
left alone. A verification-gated item in verifying or merging returns to implementing so its new
owner must verify it again. You get one `handoff_receipt` notice when the handoff line is typed;
that notice alone does not prove the Board transfer occurred. Refusals: `bad_task` (a missing or
empty `handoff.md` included), `sender_not_found`, `sender_ambiguous`, `rate_limited`,
`terminal_busy`, and `succession_required` if you hold the machine coordinator role — succession is
not available in this daemon (`501`), so that session cannot hand off.

**Milestone handoff.** A long-running Root that has reached a milestone hands over with
`clawdline handoff --summary summary.md`, so the work after it does not re-read everything before
it on every call. The summary has exactly five `## ` headings — Goal, Verified decisions, Blockers,
Evidence (paths, commits, ids or `clawdline` commands to open, not their contents), Next step — at
most 6 KiB, with no credential and no conversation text; `--check` lists every problem without
opening anything, and the daemon refuses the same ones as `bad_milestone_summary`. The daemon
writes `obligations.md` beside it: your Board items (they move, a decision the person has not
answered included) and your running children, unacknowledged notices and owed landings (they stay
yours). So after handing over, keep acknowledging and landing those, then `clawdline session close`
once it reads `safe`. Handing over is your choice: `clawdline usage --compare-handoff` says whether
it has paid on this machine, and it is never forced.

**Root assignment.** The `Idempotency-Key` header must equal `request_id`:

```
{"request_id": "<uuid>", "assistant": "claude"|"codex", "model": "…", "project_dir": "/abs", "label": "…",
 "assignment": {"objective": "…", "scope": "…", "constraints": "…", "relevant_references": "…", "acceptance": "…"}}
```

Each assignment field is 1–8192 bytes, 32 KiB in all. The daemon writes the brief itself and opens
the session. It has **no parent, secret, timeout, result or landing**: nobody is told when it
finishes, because it answers to nobody. Refusals: `bad_root_assignment`, `idempotency_mismatch`,
`request_conflict`, `rate_limited`. Never fake one with a child, a detached task or a handoff.

**Detached automation.** Like a dispatch (§4) — `task.json` under `task_root`, then
`{"task_id", "secret", "inventory_generation"}` — but the brief's root must be
`{"session_id": null, "poll_only": true}`, or it is refused as `detached_task_required`. Nobody is
notified; poll `GET /v1/orchestrator/tasks/<id>` and read `result.json`. It is never a Root or a
feature owner.

### Schedule future work

Clawdline Next owns scheduled work itself. Do not use the retired app, `cron`, or a chain of
detached tasks. Read `GET /v1/orchestrator/schedules`; read one in full at
`GET /v1/orchestrator/schedules/<id>`. `GET /v1/places` supplies the `place_id` a write names.

A one-time schedule (`on`) may be made directly with the orchestrator token. A repeating schedule
(`days`) is a standing instruction and needs the person's explicit instruction from this session:

1. Read `GET /v1/orchestrator/sessions/<conversation>/run`. It is the recent run issued when the
   person sent this session their message through Clawdline.
2. `POST /v1/orchestrator/schedules` with an `Idempotency-Key` and the ordinary schedule body plus
   that conversation and run:

```json
{"title":"Morning sweep","at":"09:00","days":"daily","place_id":"<place id>",
 "assistant":"codex","instructions":"Inspect the overnight failures and report actionable findings.",
 "session_id":"<conversation id>","via":{"run":"<run id>"}}
```

The same proof authorizes `PATCH /v1/orchestrator/schedules/<id>` (send the whole schedule body)
and `DELETE /v1/orchestrator/schedules/<id>` (send the two proof fields as its JSON body) when the
person explicitly asked for that change.
`POST /v1/orchestrator/schedules/<id>/run` runs one now. Read the created or changed schedule back
before reporting success.

Without `via`, the orchestrator token remains limited to a one-time `on` schedule. An invented,
expired, or other-session run is refused as `run_unknown`, `run_expired`, or `run_other_session`;
malformed proof is `invalid_user_authorization`. If the person typed straight into a terminal,
there is no run: ask them to send the instruction through Clawdline. Never reuse a run as blanket
permission for work the person's message did not request. This proof makes the relay auditable; it
does not turn the machine-wide orchestrator token into a session-specific credential.

A schedule may have no time: send `"trigger_only": true` instead of `at`, `days` and `on`. The
clock never runs it; it runs only by `…/run` or by its webhook, and only while `enabled`. It is a
standing instruction like a repeating one and needs the same proof.

**Start a task on another machine and learn how it ended.** There is no machine-to-machine channel.
Make the task a trigger-only schedule on the target machine, bind a Cloud webhook to it, and store
the URL where the caller can read it (it is a credential: a file readable only by you, or
`CLAWDLINE_WEBHOOK_URL`; never a command-line argument). Then, on the calling machine:

```sh
clawdline webhook fire --url-file <path>
```

It sends `{"deliver_within_seconds": 60}` (`--deliver-within`), so a target that is off does not
run it later; it follows the delivery's status until it ends or `--timeout` (60m) passes, printing
changes on stderr and one final line on stdout. Exit `0` succeeded · `1` finished without success
(failure, timed_out, cancelled, spawn_failed) · `2` never reached the machine (expired, canceled,
unreachable) · `3` refused (dispatch_refused and its code, the URL unavailable, rate-limited) · `4`
stopped waiting, with the last state seen. `--no-wait` prints the delivery id and returns after the
`202`. Report the exit code and final line as they are: `2` means nothing ran, not that it failed.

A scheduled task that reads data for something waiting to be verified (the sidebar's 驗收,
docs/verifications.md) writes its readout as a note on that record with its own task secret — not
the orchestrator token, which it should not hold:

```sh
curl -sS -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$TASK_ID/verification-note" \
  -H "X-Clawdline-Task-Secret: $TASK_SECRET" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: readout-$TASK_ID" -d '{"verification":"<record id>","text":"<readout>"}'
```

It lands only on a record whose `schedule_id` is the schedule that started this task
(`schedule_mismatch` otherwise), is signed `task:<task id>`, and is refused as `not_scheduled` for a
task no schedule started. Find the record id with `clawdline verify list`.

## 7. Report your own finished turn

When your turn is genuinely finished — the work done, verified and committed where that applies —
make this your last action before the final answer:

```sh
clawdline session report --summary "One concrete sentence about what was delivered."
```

It draws one check on your session's row: **delivered, awaiting approval**. It is weaker than a
landing and asks for no review. It shows while the daemon reads the session as idle — working,
waiting or an unreadable screen outrank it — and only while that terminal holds the same
conversation.

- **Only for a finished turn.** Not for partial work, a diagnosis, a blocker or a question back to
  the person. A child never sends it (`409 child_session`).
- The command finds your conversation from `CLAUDE_CODE_SESSION_ID` or `CODEX_THREAD_ID`
  (`--conversation` otherwise), asks `GET /v1/orchestrator/whoami` for the terminal, and posts
  `{"summary"}` to `POST /v1/orchestrator/sessions/<terminal>/complete`.
- The summary is 1–500 characters. Each call is a new receipt; the newest counts.
- The answer also carries `open_todos`: this Session's direct to-dos that were sent or read and are
  not completed, oldest first, at most 20 (`open_todos_truncated` when there are more). The command
  prints them on stderr after the receipt, one id and text per line. Check off each one you
  finished with `clawdline todo done <id>`. The receipt is recorded either way and the exit status
  does not change; `open_todos_unknown: true` means they could not be read, not that none are open.
- Refusals: `conversation_id_malformed` (not a lowercase UUID), `conversation_not_found`,
  `conversation_ambiguous`, `registry_stale`, `session_not_found`, `session_unbound`,
  `child_session`. Report the refusal honestly; a sentence in chat is not a receipt.

## 8. Talk to another session

**Find it.** `GET /v1/orchestrator/sessions` is the address book: every session's `id` (its
terminal id), `label`, `assistant`, `cwd`, `state`, `work_state`, and `taskId` for a live child.

**Send.**

```sh
clawdline send --to <terminal id> "text"        # or text on stdin
```

This is `POST /v1/orchestrator/messages` with `{from_session, to_session, text}` and an
`Idempotency-Key`. The daemon types it into the recipient's composer inside a
`<clawdline-message>` envelope that names you as the source.

- `to_session` is a **terminal id**: a message follows the tab you meant, not a conversation.
  `from_session` is your terminal or conversation id (the command fills it in).
- Text only, at most 100,000 characters. There is no `images` field: one sent is silently dropped.
- `ok` means the bytes reached a composer, not that anybody read them.
- It can take tens of seconds: the daemon reads every session on the machine before it types (about
  30 s a relay on one Mac, measured 2026-09-19). The command waits up to two minutes. Do not cut it
  short — a relay cut off mid-way can type and not be recorded, and the same key then answers
  `409 request_in_progress`.
- The command prints its `Idempotency-Key` first. If the call fails on the way, run it again with
  `--key <that key>`: the same key and body are typed once. The same key with a different body is
  `409 idempotency_key_reused`.
- Refusals: `source_not_found`, `target_not_found`, `same_session`, `target_busy` (the recipient
  shows a menu; nothing was typed), `terminal_busy`, `delivery_failed`.

**Show the person a picture.** Do not paste a local path: on a phone it opens nothing.

```
POST /v1/artifacts/images    {"images": [{"path": "/absolute/path.png"}]}
```

- Orchestrator token only. One to six local files; each must be a plain file, at most 12 MiB and
  12,000 px a side. PNG, JPEG and GIF are read directly; other formats go through `sips` on macOS.
- The answer lists `artifacts`, each with a `marker` such as `<clawdline-image id="…">`. **Put the
  marker in your reply**; the console shows the picture where the marker is. Pictures are kept 24
  hours.

## 9. Tell the person

```sh
clawdline notify --title "At most 80 characters" --body "At most 500 characters"
```

`POST /v1/orchestrator/notify`. Only for something the person is waiting on: the value of a push is
that it is rare. `--session <terminal>` makes a tap open that session.

- `409 agent_notify_disabled`: the person turned agent notifications off. Not your fault; do not
  retry.
- `409 not_subscribed`: no device is subscribed to pushes.
- `429 rate_limited`: 30 an hour for the whole machine, shared with every child's notifications.
- `502 push_failed`: the push service refused; `sent` and `failed` are in the error.

## 9a. Leave a human intervention note

Use a note when a long-running Agent has one concrete thing the person should read, do, or decide and an ordinary chat message could disappear in the stream. The note remains in the target Session's collapsed attention area, marked with a red dot until the person moves it to handled. You may then continue independent work; the person can return at a natural stopping point. A note is not a progress log, a private reminder, a notification, or authorization for a Board decision. Avoid duplicate notes for the same request.

**Before asking the person to choose in chat**, create one `answer` note containing the actual question, the tradeoffs needed to decide, and two to four complete suggested replies. Tapping a button sends its reply as a conversation message, so make each `draft` unambiguous on its own. A brief chat pointer is enough after creation. Do not treat note creation or a note marked handled as the person's answer; wait for the conversation message — a tapped reply or one the person typed — before acting on it. If creation fails, say so and ask the question directly. Reserve notes for decisions needing human judgment, not routine choices the Agent can make.

Create one with a JSON body file. Without `--target`, the CLI resolves this live Root's own terminal id through `whoami`. For another Session, use its live **terminal id** from the address book (`clawdline guide send`) as `--target`. `--from` defaults to this live Root's conversation id from the environment. The CLI reads the machine credential without putting it in the command line, injects the source and target ids, and prints the daemon's durable note id. Reuse the printed `--key` after an uncertain result.

```json
{"kind":"answer","title":"Choose a date","summary":"One release date needs your choice.","action":"Choose a date when you have a moment.","reason":"Only you can choose it.","options":[{"label":"Tuesday","draft":"Tuesday works for me."},{"label":"Wednesday","draft":"Wednesday works for me."}]}
```

```sh
clawdline note create --body-file note.json
# For another Session: clawdline note create --target <terminal-id> --body-file note.json
```

`kind` is `read`, `answer`, `action`, or `report`; `title`, `summary`, `action`, and `reason` are required. An `answer` can offer two to four choices. Each `draft` is the suggested reply shown on its button. When the person taps it, the Console sends it right away as a conversation message to the note's Session, followed by a context line with the note ID, title, and action so the receiving Session knows which request the person answered; the context is not shown on the button. Only after the send succeeds does the note move to recently handled. If the send fails, the note stays pending and the attention control says the reply was not sent. `detail` can hold longer text. `document_url` can link to a real, readable Cloud document; verify the document route and file before posting it. Act on the conversation message that arrives, not on the note's state: a note the person marked handled by hand sent nothing. If your work is actually blocked on the answer, record the waiting-user state and send the existing attention notification once. A visible note alone sends no push and does not wake an Agent.

## 10. The board

The board has three structures — board items, the Backlog, and each session's own to-do list — and
**a person decides what goes on it**. A session creates a Board item only when the person's own
message, sent through Clawdline, tells it to; otherwise it proposes. It never files a card on its
own initiative. The one exception is the owner of an Epic: after the Epic's reviewed plan it may
break the Epic into Feature and Issue items and assign them to Sessions (`clawdline guide epic`).

**TODO / 待辦 / 土度 said together with a Board item means that item's steps.** Put them on the
item with `--step`. Do **not** also write them with `clawdline todo add`. `clawdline todo add` is
only for a list the person asks you to track as this Session's own to-dos with no Board item.

**When the person tells you to create a Board item.** Only when their message — sent through
Clawdline, so it has a run — explicitly asks for one, create it yourself:

```
clawdline item add --project <place id> --kind feature|issue|epic|refactor|plan --title "…" \
  --step "first step" --step "second step" …   [--description-file f | description on stdin] [--assign-self]
```

Worked example. The person writes: *"Make a Board item to clean up the release notes, TODO: draft
them, check the links, publish."* That is one command and nothing else:

```
echo "Clean up the release notes before the next release." | \
  clawdline item add --project <place id> --kind feature --title "Clean up the release notes" \
  --step "Draft the notes" --step "Check the links" --step "Publish"
```

- `item add` reads this conversation's latest run (`GET /v1/orchestrator/sessions/<conversation>/run`)
  unless `--run` names one, prints its Idempotency-Key before asking (`--key` retries the same
  write), and prints the created item with each step's id. It is
  `POST /v1/work/v2/agent/items` with `{"session_id", "via": {"run"}, "project_id", "kind",
  "title", "description", "deployment_policy"?, "steps"?: ["…"], "assign"?: {"mode": "self"} | {"mode": "existing_session", "terminal_id": "…"} | {"mode": "new_session", "assistant"?: "…"}}`,
  answered `201` with `{"item", "assigned", "assignment_state"}`.
- A Feature, Issue or Epic arrives **unassigned** by default, where the person's unassigned items of
  that kind wait on the Board, with its steps: the `--step` rows in order, or — when you give none —
  two or more top-level Markdown list rows of the description. The person often asks you to write
  work up for later; creating the item does not make it yours. `assignment_state` is
  `not_requested`.
- Add `--assign-self` (`"assign": {"mode": "self"}`) **only when the person's message asks this
  Session to do the work now** ("make an item for this and do it"). The item then arrives
  **assigned to you**, in `assigned`, in the same write. Nothing is typed into your terminal; you
  asked for it. Work the steps in order, complete each one when it is verified
  (`clawdline item steps <item id>`, `clawdline item step-done <item id> <step id>`; `clawdline item step-add` adds
  one the work turns out to need), and advance the phases with `clawdline item phase` as for any
  assigned item (below). An Epic taken this way then follows the Epic procedure (below) before it
  may be implemented. If the person asks you to take an item you created unassigned later, use
  `clawdline item claim` (below).
  A Refactor is executable work that changes internal structure but not outward behaviour: it is
  assigned, takes steps and follows a Feature's phases, gate and review switch. A Plan is created
  unassigned, in Planning, with or without `--assign-self`, and takes no steps (`planning_has_no_steps`).
- The registered Clawdfather is the exception for executable Project work: it never owns or edits
  Project code. When the person's message explicitly requests a new item, it may use
  `clawdline item add --project <place id> --kind feature --title "…" --assign-new` (or
  `--assign-terminal <id>`) to create the item first, then delegate it to a Project Session.
  `assignment_state` says whether a Project owner was recorded (`assigned`), a new Session needs its
  first dialog answered (`awaiting_user`), assignment failed (`failed` with `assignment_error`), or
  no assignment was requested (`not_requested`). In the latter two cases, the person can assign
  from the Board. A replay with `pending` names the original item after an interrupted delegation;
  inspect the Board before trying another assignment. Without that explicit message, propose the
  item and wait for acceptance.
- The person sees the card marked "Created by the Session from your message at HH:MM", with their
  words quoted.
- Refusals, each writing nothing: `run_unknown` (no run named, or none issued), `run_expired`
  (older than a day), `run_other_session` (a message to another Session), `session_not_found`,
  `child_session` (a child reports through `result.json`), `project_not_found`,
  `project_mismatch` (an ordinary executable item must be in the Project you work in),
  `coordinator_required` (a machine Session without the live role), `machine_delegation_required`
  (an ordinary Session requested the machine-only combined assignment to another Session),
  `invalid_assignment` (a malformed `assign`, or Clawdfather asking for `self`), `too_many_steps`
  (more than 128), `run_items_exhausted` (one message backs at most five items).
- **No run** — the person typed straight into the terminal, so `item add` answers `no_run` or
  `run_unknown`: fall back to a proposal (below) and tell the person to accept it in the Board's
  Agent proposals.

Never create a Board item on your own initiative, and never several to plan speculative work.

**Claim a Board item the person pointed you at.** When the person's message through Clawdline tells
you to take a specific item already on the Board — *"take the release-notes item"*, *"claim
<item id>"* — claim it; that is one command:

```
clawdline item claim <item id>
```

- `item claim` reads this conversation's latest run unless `--run` names one, reads the item for
  its version, prints its Idempotency-Key before asking (`--key` retries the same write), and
  prints the item after. It is `POST /v1/work/v2/agent/items/<id>/claim` with
  `{"expected_version", "session_id", "via": {"run"}}`, and it assigns the item to **you, the
  Session that message was sent to** — nothing in it names another Session or terminal.
- The item then reads exactly as if the person had assigned it to you from the Board: you own it,
  it moves to `assigned`, its steps are seeded from the description's list if it had none. Nothing
  is typed into your terminal. Work it as any assigned item (below).
- The person sees the card marked "Claimed by the Session from your message at HH:MM", with their
  words quoted.
- Refusals, each writing nothing: `run_unknown`, `run_expired`, `run_other_session`,
  `session_not_found`, `child_session` (as for `item add`); `work_not_found`; `project_mismatch`
  (the item is in a Project you do not work in); `item_assigned` (it already has a Session, or one
  is being opened for it — only the person moves an item between Sessions); `item_terminal` (done or
  cancelled); `planning_not_assignable` (a Plan stays in Planning; an Epic or a Refactor can be claimed);
  `version_conflict` (it changed; run the command again); `run_claims_exhausted` (one message backs
  at most five uses).
- **No run** answers `no_run` or `run_unknown`: leave the item for the person to assign.

**Assign a Board item to a new Session the person asked for.** When the person's message through
Clawdline asks you to hand a specific unassigned Feature or Issue to a new Session — *"open a
security Session for <item id>"* — assign it; that is one command:

```
clawdline item assign <item id> --new [--assistant claude|codex] [--model m] [--persona <id>]
```

- On an item that is no Epic's child, `item assign` reads this conversation's latest run unless
  `--run` names one, as `item claim` does. It is `POST /v1/work/v2/agent/items/<id>/assign` with
  `{"expected_version", "session_id", "mode": "new_session", "assistant"?, "model"?, "persona"?,
  "via": {"run"}}`, and opens the new Session the person's own "New Session" choice opens.
- The card says "Assigned by a Session from your message at HH:MM", with their words quoted and
  the persona the new Session runs as.
- Refusals, each writing nothing: those of `item claim` (`run_unknown`, `run_expired`,
  `run_other_session`, `session_not_found`, `child_session`, `project_mismatch`, `item_assigned`,
  `item_terminal`, `version_conflict`, and `run_claims_exhausted`: `item claim` and `item assign` share one
  message's five); `kind_person_assigns` (only a Feature or an Issue); `new_session_only` (to take it
  yourself, claim it); `unknown_persona`; `persona_disabled_for_auto_assignment` (the role is off for
  automatic assignment in that Project).

Never claim or assign an item on your own initiative — only the one the person's message names — and
never use the person's `POST /v1/work/v2/items/<id>/assign`, which refuses a Session
(`session_cannot_create_item`). An Epic's owner also assigns the Epic's own children with
`clawdline item assign` (`clawdline guide epic`).

**Name a new Session opened for a Board item.** After you read its objective and scope, choose a
short name that describes your actual task and run `clawdline item name <item id> "<task name>"`.
This changes your Session's name once, without changing the Board item's title or starting another
model turn. The active new-Session owner alone may do it. Sending the same name again is safe; a
different name is refused, and the person can still set a manual Session title. A Board item given
to an existing Session leaves that Session's name alone.

**Propose a Board item.** The Board's **Agent proposals** queue is fed by one route:

```
POST /v1/work/v2/agent/proposals     (Idempotency-Key required)
{"project_id": "<place id>", "kind": "feature" | "issue" | "epic" | "refactor" | "plan",
 "title": "…", "description": "…", "reason": "why this is worth doing",
 "suggested_acceptance": "what would count as done", "session_id": "<your conversation id>",
 "source_work_id": "<uuid>" or "source_todo_id": "<uuid>"}
```

- `project_id` is the `id` of a row from `GET /v1/places`.
- One source is required, and it must be yours: a Board item this Session owns, or one of this
  Session's own to-dos (`proposal_source_required`, `proposal_source_invalid`). A proposal that
  came from something the person asked for cites the to-do it came from — so the path is: the
  person asks, you `clawdline todo add` it (below), and you propose from that to-do's id.
- Write the proposal in plain language a person can understand directly: `title` names the outcome
  they can notice, `description` says what changes, `reason` says why it is worth doing now, and
  `suggested_acceptance` says what they can observe when it is done. All four are required. Do not
  make unexplained acronyms, internal identifiers, code paths, or implementation jargon the main
  explanation. The Board first shows title, source, and reason; **Explain / 詳細說明** expands what
  changes and what the person will see when it is done.
- **To propose an item with steps**, write the list as two or more top-level Markdown list
  rows in `description`. When the person accepts and assigns the item, each row becomes one of its
  `steps` (see below).
- `201` answers the pending proposal. The person accepts, edits or rejects it in the Board's Agent
  proposals queue; nothing becomes a Board item until they do. Refusals: `invalid_proposal`,
  `proposal_too_large`, `project_not_found`, `proposals_full`.

**The older proposal route.** `POST /v1/orchestrator/proposals` (with `…/<id>/asked` after asking
in the conversation) is still served: it is where a child files a leftover with its task secret and
`task_id`, and where a root's line-of-work proposal gets its ask-now-or-hold `instructions`. Its
rows appear in the older "to confirm" area, **not** in the v2 Board's Agent proposals queue, so it
is not how to put an item in front of the person on the Board.

**Ask the person a decision.**

```
POST /v1/orchestrator/decisions     (Idempotency-Key required)
{"session_id": "…", "work_id": "<the Board item this is about>", "question": "…", "options": [{"id": "a", "label": "…"}, …],
 "default": "a", "blocking": true, "due_in_minutes": 1440}
```

The item must be open and owned by this Session: its card is the context in which the Board shows
the question (`decision_source_required`, `decision_source_not_found`, `decision_source_invalid`,
`decision_source_closed`). Two to four options; `default` must be one of them and is what happens
when nobody answers (after 7 days unless `due_in_minutes` says 60–10080). Only a `blocking`
decision is pushed. Read the answer with `GET /v1/orchestrator/decisions/<id>`.

**The person answers; a session only relays what they said.** Proposals, decisions and board items
are answered under `/v1/work/…`. A session writing there must name the run that carried the
person's words, `"via": {"run": "<id>"}`, and is refused without it (`403
session_cannot_decide`). Read the latest run at
`GET /v1/orchestrator/sessions/<conversation>/run`; an invented, expired, other-session, or
pre-question run is refused by name. A person typing straight into a terminal has no run, so ask
them to answer through Clawdline or in the console.

**Your children still to collect.** `GET /v1/orchestrator/sessions/<conversation id>/todos` — named by
conversation id, not terminal (`409 session_id_is_terminal` otherwise). The broker opens and closes
these from task facts; there is nothing to write.

At every turn boundary, before declaring yourself idle, also read
`GET /v1/work/v2/agent/session-todos/<conversation id>`. Its `assigned_items` are Board items the
person has given this Session, its `recent_items` are items this Session recently completed, and its
`direct_todos` are quick requests, and its `unacknowledged_completions` are children of yours that
finished without your ACK (§5). This pull is how an assignment made while you were working waits
without interrupting the current turn. Finish the current turn, then take the assigned item as your
next owned work and read its complete record, document bodies included, with
`clawdline item show <id>`.

**A to-do the person sent.** A message whose last line reads
`(Clawdline to-do <id>. When it is done: clawdline todo done <id>)` is one of this Session's
`direct_todos` that the person sent from Clawdline; the words above that line are the request. Do
the work, and once it is verified done run `clawdline todo done <id>` with that id **before** you
report the turn — otherwise the row stays open on the person's list although the work is finished.
One that is not finished stays open. `clawdline session report` lists on stderr every to-do that was
sent to this Session and is still not checked off (§7).

**Your own to-dos, when the person asks.** Only when the person explicitly asks this Session to
record its work as Clawdline to-dos — or hands it a list of several items and says to track them
there — write them to this Session's own list:

```
clawdline todo add "first item" "second item" …     (or one item per non-empty stdin line)
clawdline todo list
clawdline todo done <to-do id>
```

`todo add` is `POST /v1/work/v2/agent/session-todos/<conversation id>` with
`{"todos": [{"text": "…"}, …]}` and an Idempotency-Key it prints first (`--key` retries the same
write). One call carries 1–20 rows of at most 8 KiB each, inside the 96 KiB request body, and adds
all of them or none; a list that would take this Session past 500 open to-dos is refused whole
(`direct_todos_full`). It answers `201` with the rows, in the order given. The conversation must be
a live Session this daemon knows (`conversation_id_malformed`, `session_not_found`); a Clawdline
child is refused (`child_session`) and keeps reporting through `result.json`.

Never do this on your own initiative, and never to plan speculative work. Complete each row with
`clawdline todo done <id>` only once it is verified done. The person sees these rows marked as
added by the Session, and only the person can send or delete them. They are not Board items and
never appear on the Board. To-dos are a list of chores in the current Session; a Board item is work
the person wants tracked on the Board — when they ask for that, use `clawdline item add` (above),
and its list goes in as the item's `--step` rows, never as to-dos as well.

If the person's meaning clearly says that the item you just completed is still unfinished, correct
the Board yourself; do not leave it in Recently Done, create a replacement item, or ask the person
to reopen it. Reread the item for its current version, then use:

```
POST /v1/work/v2/agent/items/<id>/reopen     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "reason": "The concrete behavior or acceptance claim that remains unfinished"}
```

Use this only when the reference to your just-completed item is clear. The route accepts only
`done` work whose final assignment was released by this same Session; it cannot reverse a person's
cancellation or take another Session's completion. It preserves the earlier evidence, starts a new
cycle in `implementing`, restores this Session as owner, and records the reason in immutable item
history. The reason is at most 8 KiB. An ambiguous follow-up is not authority to change a Board
item.

When the owning Agent needs the person to act or choose, it asks a decision and waits on it.
First open a decision about this item (`POST /v1/orchestrator/decisions` with this item's
`work_id`, two to four options, a `default` and a due — for an action, options such as
`{"id": "done", "label": "I've done it"}` and `{"id": "cannot", "label": "I can't"}`), then point
the item at it on the machine-authenticated route:

```
PATCH /v1/work/v2/agent/items/<id>/edit     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "condition": "waiting_user", "decision_id": "<the decision's id>"}
```

The decision must exist (`decision_not_found`), be this Session's (`decision_other_session`), be
about this item (`decision_other_item`) and still be open (`decision_not_open`); `decision_id` on
any other condition is `decision_requires_waiting_user`. A `waiting_user` without a decision is
refused with `waiting_user_requires_decision`. The person answers on the Board card or in
"Waiting on you"; when the decision is answered, or its default stands at the due, the daemon
clears the item's `waiting_user` in the same write, records the answer on the item, and types the
chosen option's id and label into this Session when it is idle. To stop waiting yourself, set
`condition` to the empty string (or another condition) on the same route; the decision is then
`withdrawn` and leaves "Waiting on you", and so does it when the item is released, reassigned,
cancelled or done. Answering a withdrawn decision is refused with `decision_withdrawn`.

**Captured planning and verification gates.** `planning_gate` defaults on and `verify_gate` off;
`clawdline setting get|set planning_gate|verify_gate` accepts `on/off` or `true/false`. The first
successful assignment in an execution cycle freezes both values. Reassignment and later global
setting changes do not change that cycle. An Epic or Feature with captured planning on needs
acceptance criteria before implementing. An Epic also needs a plan and independent review; a Feature
needs them only when the person checked its Needs independent review switch (below). An Issue never has a planning gate. Planning off bypasses forced Epic planning
too. Both on means planning then independent verification; planning only retains ordinary merge
verification; verification only skips planning but still checks the exact candidate; both off uses
the ordinary lifecycle. The person need not fill acceptance on the Board. If a gated item arrives
without it, write observable criteria with `clawdline item acceptance <item id> --body-file <file>`
after assignment and before the gated transition. The owning Session may fill an empty contract
once. When the person explicitly tells this owning Root through Clawdline to revise this item's
acceptance, write the complete replacement Markdown to a file and run
`clawdline item acceptance-revise <id> --run <message run> --expected-version <item version> --body-file <file>`.
The item version is the `item version N` line `clawdline item show <id>` prints (`item steps`
prints only the acceptance version); read the message run from
`GET /v1/orchestrator/sessions/<conversation>/run`. The retained message excerpt must explicitly
request an acceptance change; a prohibition, discussion, or bare question is not authorization.
It may refer to the item by conversation context if this Root
owns exactly one open item; otherwise it must identify the item by ID or title. The run must be
newer than the current acceptance version. A typed refusal means nothing changed. Retry an uncertain response with
the same `--key`, `--run`, `--expected-version`, and file bytes. The person can also edit directly.
Changing it before merging invalidates old PASS and overrides; once merging starts it is locked.

With captured verification on, run `clawdline item phase <id> verifying` from a clean registered
worktree at its committed candidate: the CLI sends the current branch and full HEAD, and the daemon
checks Project, cycle base, tree and acceptance digest. A detached read-only Codex checker uses
`code-reviewer` for Issue, `reality-checker` for Epic, and `evidence-collector` for a Feature with
reference pictures or a design document (otherwise `reality-checker`). Its typed verdict is
`PASS`, `FAIL`, or `NEEDS_WORK`; unverified statements say why and never authorize merging. A missing
or malformed result is a technical failure, with one bounded retry and then escalation. An Epic's
final end-to-end round waits until all children are terminal and the affected components are integrated
into one runnable candidate. Focused child tests and integration smoke checks happen first; do not
dispatch final browser or multi-account end-to-end work against mock UI, disconnected branches, or
incomplete APIs. Before dispatch, prove the chosen worker can actually open the target URL with an
authorized browser or equivalent local automation, and has the test accounts, fixtures, and origin
permissions it needs. Name that route in the brief; `--permission-mode full` alone is not browser
access. Resolve a failed tool preflight before retrying, rather than sending another verifier into
the same blocker; a failed preflight is not an end-to-end attempt. Plan one comprehensive
end-to-end round per Epic, not one per child or revision. After fixing a defect, rerun only
affected scenarios. Repeat the comprehensive round only when the acceptance scope or integration
boundary materially changes, and record why. `verifying → merging` needs a live
PASS for the exact candidate/criteria or an explicitly reasoned override; a verification sentence
alone cannot grant it. Three consecutive FAILs escalate to the live parent Epic owner, then to the
person if that owner is unavailable; a technical failure escalates separately. Only the designated
parent owner uses `POST /v1/work/v2/agent/items/<id>/gate-decision`; a person uses
`POST /v1/work/v2/items/<id>/gate-decision`. Never use the person route as an Agent. The Board names
AI, person, and technical overrides separately, never as checker PASS. At retained-detail capacity,
the person first downloads `GET /v1/work/v2/items/<id>/gate-export`, verifies the manifest digest,
then confirms `POST /v1/work/v2/items/<id>/gate-purge` with that digest and item version. Purge
removes only eligible closed detail; aggregates, newest facts and audit remain.

**Advancing the phase.** The owning Session moves its item through the execution phases itself;
nobody else does, and a turn receipt or a cleared condition does not. The phase is not a field of
`…/edit` (`phase_not_editable`). Run each transition when the work it names has actually happened:

```
clawdline item phase <item id> implementing                  # when you start
clawdline item phase <item id> verifying                     # the change exists; now check it
clawdline item phase <item id> merging --verification "what was run and what it showed"
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin --landing-project <place id>
clawdline item phase <item id> deploying --no-landing-reason "why there is no code to land"
clawdline item phase <item id> done --deployment "what went live, where, which version"
clawdline item phase <item id> done --no-deployment-reason "why nothing needs deploying"
```

The command reads the item's version, prints its Idempotency-Key (`--key` retries the same write)
and prints the item. It is

```
POST /v1/work/v2/agent/items/<id>/phase     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "next": "<phase>",
 "verification"?: "…", "landing"?: {"commit", "target", "remote", "project"?},
 "no_landing_reason"?: "…", "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- One step at a time: `assigned → implementing → verifying → merging → deploying → done`.
  From `verifying` you may go back to `implementing`; from `merging`, back to `implementing` or
  `verifying`. An item whose captured verify gate is off may also go from `implementing` straight
  to `deploying` on its landing evidence (below), with `verification` optional; a gated item is
  refused that step with `verification_gate_on` and walks the whole line. Nothing else skips a
  phase, and `done` is reached only from `deploying`.
- `merging` needs `verification`. `deploying` needs a landing: a broker child of this item that
  landed, or `landing` naming a commit the daemon finds on both the Project's local `target` branch
  and `refs/remotes/<remote>/<target>` — push first. When the work landed in another repository
  (a backend item whose change was a frontend commit), `landing.project` (`--landing-project`)
  names that Project's id from `GET /v1/places`, and the commit is looked for there instead — a
  nested repository inside the Project directory (`cloud/`) is its own Project here. The proof is
  written as the broker's **root landing record**, once: the same item, repository, commit and
  target recorded again is the same record. The item's history names it as `landing_id`, and
  `clawdline landings --work-id <item id>` lists it. Before `deploying` the daemon asks git at once
  whether a bound child's branch has been merged, so a merge made a moment ago counts without
  waiting for the broker's next look. Work with no code takes `no_landing_reason`
  (`--no-landing-reason`) instead of a landing; it is refused beside a `landing`
  (`invalid_landing_evidence`), while a bound child still owes its landing (`landing_owed`, naming
  the task), and beside a child that landed (`landing_recorded`). `done` needs `deployment` or
  `no_deployment_reason`; the item's `deployment_policy` decides which (`required` takes only
  `deployment`, `not_required` only `no_deployment_reason`, `agent_decides` either). Every step
  must be complete first.
- `done` releases your assignment and moves the item to the Session's recently done row. Add a
  completion report (below) before it when one is owed.
- Refusals: `invalid_transition` (not a next phase, or its evidence is missing), `steps_incomplete`,
  `not_item_owner`, `item_unassigned`, `item_terminal` (a person reopens it), `evidence_unknown`,
  `direct_landing_not_applicable`, `invalid_landing_evidence`, `landing_project_not_found`,
  `landing_commit_unresolved`,
  `landing_target_unresolved`, `landing_not_on_target`, `landing_remote_unresolved`,
  `landing_not_published`, `landing_owed`, `landing_recorded`, `landings_full` (the item keeps 64
  root landings), and `version_conflict`: reread and send again. A refusal for want of a landing
  ends with what the broker found when it looked just now (a branch not yet merged, a repository it
  could not read).

**Finishing in one command.** After the work has landed, `clawdline item finish <item id>` walks
the item from `implementing`, `verifying`, `merging` or `deploying` to `done` in one transaction:

```
POST /v1/work/v2/agent/items/<id>/finish    (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "verification"?: "…",
 "landing"?: {"commit"?, "target"?, "remote"?, "project"?}, "no_landing_reason"?: "…",
 "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- Each step is the one `item phase` takes, through the same gates, and writes its own
  `item.phase_changed`; any step refused refuses the whole and nothing is written.
- The landing is read, not typed: the commit is the gate's authorized candidate on a gated item,
  otherwise the bound children's landed commit; the target is the branch their landings name; the
  remote is the one that branch tracks. Any field you give wins, and the result is proved against
  git as `item phase deploying` proves it. A captured verification gate still needs its PASS: from
  `implementing` a gated item is refused `verification_candidate_required`, so enter `verifying`
  with `item phase` from the candidate worktree, wait for the PASS, then finish.
- A bound child whose branch was merged a moment ago is recorded landed by the finish itself: the
  daemon asks git before it reads the landings, so `landing_required` right after a merge means the
  branch is not on a target, and the refusal says what the broker found.
- Work with no code: `clawdline item finish <item id> --no-landing-reason "…" --no-deployment-reason "…"`,
  under the same rules as on `item phase`.
- An item already `done` is answered as it stands and nothing is written, so the same landing seen
  twice moves nothing.
- Refusals add `verification_required`, `landing_required`, `deployment_required` (the note that
  step lacked), `landing_target_unknown`, `landing_remote_unknown`, `landing_remote_unreadable`,
  `landing_ambiguous` (name it with the flag), `landing_owed`, `landing_recorded` and
  `finish_not_started` (still before `implementing`).

An assigned item may contain `steps`. A successful assignment can seed them from two or more top-level
Markdown list rows in the description, and an item you created with `clawdline item add` carries
its `--step` rows. Each step is a checklist entry on that item, not another Board item.
Complete a verified step with `clawdline item step-done <item id> <step id>`; it sends
`POST /v1/work/v2/agent/items/<item-id>/steps/<step-id>/complete` with `{"session_id"}`. On the
Agent routes `expected_version` is optional: omitted, the write acts on the current version; named
(`--expected-version`), it is compared and a stale one answers `version_conflict`. A transition to `done` is refused with `steps_incomplete` while any step remains
open; Clawdline never checks one merely because the parent phase advanced.

**Breaking your own item into steps.** When an item you own has no steps and the work is
multi-stage — several changes that are verified separately, or more than one part of the system —
break it into its ordered steps yourself, before you implement: two to eight concrete steps, each
one you can verify on its own. A single straightforward change takes **no** steps; do not pad a
list to have one. When the work turns out bigger than it looked, add the step then.

```
clawdline item step-add <item id> "Wire the route" "Cover it with a test" "Say it in the guide"
```

Titles are arguments, or one per non-empty stdin line. The command rereads the item before each
title, prints each Idempotency-Key before its write, and prints a short receipt with an `item show` hint. It is
one owner-only request per title,

```
POST /v1/work/v2/agent/items/<id>/steps     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "title": "…", "position": <n>}
```

with `"position"` one past the last existing step's, since steps are ordered by position. Then
complete each with `clawdline item step-done` once it is verified. This is not the "own initiative"
that is forbidden for Board items and to-dos: the item is already yours, and its steps are how you
show the person the stages of work you were given.

When resolving an issue or incident required substantial investigation to discover the root cause
or to distinguish the real fix from plausible alternatives, add a user-readable completion report
before advancing the item to `done`. A straightforward, directly observed correction does not need
one. Write what happened, the root cause, what changed, how it was verified and any remaining
boundary into a file, then:

```
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

It sends `POST /v1/work/v2/agent/items/<id>/documents` for you (the Epic part of §10,
`clawdline guide epic`, lists its fields). A hand-built curl to that route without the credential
the command reads answers `401 unauthorized`. The body is Markdown, at most 64 KiB. Write for the person who reported the problem, not as a raw
debug log, and keep private data out of it. The active owner must add it before the item becomes
terminal; reread after a version conflict. A completion report is attributed narrative and never
replaces verification, landing, or deployment evidence. When present, it remains on the closed
Board item and opens directly from the Session's Recently Done row.

`/v1/board` is the Swift app's old cards, read-only. Landing is a broker fact: an item is never
marked landed by hand (`422 landing_is_broker_fact`).

### Epic and Feature: follow the person's review switch before implementation

An Epic whose cycle captured planning on needs a plan and independent review. A Feature carries the
person's **Needs independent review** switch (`review_required` on the item; `clawdline item steps
<id>` prints it). Only the person sets it, on the Board; you cannot, and you do not judge the
Feature's risk yourself. The daemon reads the switch when you ask to enter `implementing`.

- **Unchecked** (the default): write the Feature's short acceptance criteria, implement, and run
  focused tests. Do not write a plan for review, do not dispatch a `plan_review` child, and do not
  record a risk assessment.
- **Checked**, and for every planning-on Epic: use the reviewed-plan path.

If you think an unchecked Feature deserves review, say so to the person and let them check it;
there is no Agent-side way to ask the daemon for one.

1. Plan it carefully and write the plan onto the item:
   ```
   clawdline item doc <item id> --role plan --title "Plan" --body-file plan.md
   ```
2. Dispatch a read-only child whose brief is to review that plan critically — what is missing,
   wrong or risky:
   ```
   clawdline dispatch --kind plan_review --work-id <item id> --claims "" …
   ```
3. Wait for the child to finish. A successful review child dispatched with `--work-id` records its
   review receipt on the item as the `plan_review` document by itself; check `.documents` in
   `GET /v1/work/v2/items/<id>`. Only if it is not there — for example, the child was dispatched
   without `--work-id` — record it by hand:
   ```
   clawdline item doc <item id> --role plan_review --title "Plan review" --reference <task id> --body-file review.md
   ```
   Running that command again for the same task is harmless: it answers the document already
   there. A short summary for the person of what the plan changed in response is a separate
   `other` document, not a second review.
   If a Feature plan changes after review, write an `other` document titled
   `Review boundary assessment` after the revised plan with JSON
   `{"new_risk_boundary":false,"reason":"..."}` only when the change stays inside the
   earlier review's risk boundary. A new or uncertain boundary gets a focused fresh review.
   The existing Epic two-review ceiling still applies.
4. Break the work into steps with `clawdline item step-add <item id> …`.
5. Only then `clawdline item phase <item id> implementing`.

Plan verification as a sequence. Each implementation child checks its own code with focused tests;
the Epic owner integrates the affected components and runs the smallest useful cross-component smoke
check. Only after the integrated candidate works should the owner dispatch real end-to-end verification
and the applicable independent UX/product review. Check the verifier's browser route, target URL,
test accounts, fixtures, and permissions before dispatch. Do not use repeated read-only verifier
tasks to discover or work around a missing browser: fix the access or choose an equivalent local
browser harness first. A failed preflight is not an end-to-end attempt. Plan one comprehensive
end-to-end round per Epic for the stable candidate, not one per child or revision; after a focused
fix, rerun only affected paths. Repeat the whole round only after a material acceptance or
integration change, and record that reason.

`clawdline item doc` reads the item for its version and last document position, prints its
Idempotency-Key (`--key` retries the same write) and prints the item. The body comes from
`--body-file` or stdin. It is `POST /v1/work/v2/agent/items/<id>/documents` with
`{"expected_version", "session_id", "role", "title", "body", "reference", "position"}`; the roles
are `spec`, `design`, `test`, `deploy`, `completion_report`, `other`, `plan` and `plan_review`.

To revise a document, write it again with the same `--role` and `--title`: the daemon replaces its
body, reference and position, keeps its id, raises its version by one and records
`document.revised`. The CLI says `added … at v1` or `revised … to vN`, and `clawdline item show`
prints each document's `vN`. Sending the same text again changes nothing and answers the document
as it is, so a retry is safe. The old text is not kept; use a different title to keep both.

- `plan`, `plan_review` and the review boundary are never revised in place: each write adds a new
  document, because the planning gate reads them in order and a review names the plan it read.
- An item holds at most 32 documents, and a `completion_report` is not one of them: it always fits,
  even on a full item. An item holds one `completion_report`; writing another, under any title,
  revises it and takes the new title.
- A 33rd document is refused with `documents_full` and nothing is written; the message names the
  `clawdline item doc` command that revises an existing document instead.

- `plan` and `plan_review` belong to an Epic or Feature (`document_role_not_applicable` for other kinds).
- A `plan_review`'s `reference` is the task id of the Clawdline child that reviewed the plan. The
  daemon accepts it only when that task exists (`plan_review_task_unknown`), was dispatched by the
  item's owning Session (`plan_review_task_not_owned`), is on this item's line if it names one
  (`plan_review_task_other_item`), has kind `plan_review` (`plan_review_task_wrong_kind`), finished
  with `success` (`plan_review_task_unfinished`), and was dispatched no earlier than the latest plan
  (`plan_review_task_stale`). A review with no plan before it is refused `epic_plan_required`.
  The automatic document from a review child passes the same checks, and a repeated write for the
  same task is idempotent.
- `clawdline item phase <item id> implementing` on a planning-on Epic is refused without its
  reviewed plan (`epic_plan_required` or `epic_plan_review_required`). A planning-on Feature the person checked
  Needs independent review on is refused the same way (`feature_plan_required` or
  `feature_plan_review_required`); an unchecked one needs only its acceptance criteria. A revised
  plan on a checked Feature also needs unchanged-boundary evidence or another review.
  A planning-off Epic may enter implementing directly.
- The gate reads the latest `plan_review`'s receipt from its review task. Each finding has a
  `severity` of `blocking` or `non_blocking` (`important` and `minor`, from the older template,
  count as non-blocking). The verdict is `safe_to_land` with no findings, `proceed_with_findings`
  when every finding is `non_blocking`, and `changes_required` when any is `blocking`. A latest
  review with a blocking finding refuses `item phase implementing` and any dispatch with
  `--work-id` on the still-assigned item except `--kind plan_review`
  (`epic_plan_review_blocking` or `feature_plan_review_blocking`); the refusal lists the blocking
  findings and the next commands: revise the plan, then dispatch a new review. Only non-blocking
  findings, or an older receipt whose findings carry no severity, let the item proceed. An Epic
  whose two reviews are used and whose second still blocks goes on only by the person's override
  (a Feature's Needs independent review switch, or `planning_gate` off for the next cycle) or by a
  revised plan once the person raises the review limit; do not dispatch a third review yourself.

**Break the Epic into child items, and hand them out.** This is the one exception to "a session
creates a Board item only when the person's message tells it to" and to "only the person assigns
items": the person assigned you the Epic, and that is the authority to break it up. After the
reviewed plan has taken the Epic into `implementing`, when parts of it are better done by other
Sessions, create Feature or Issue items under it and assign them:

```
clawdline item child <epic id> --kind feature|issue --title "…" [--step "…"]… \
  [--description-file f | description on stdin] [--deploy policy] \
  [--assign-terminal <terminal id> | --assign-new [--assistant claude|codex] [--model m] [--persona <id>]]
clawdline item assign <child id> (--terminal <terminal id> | --new [--assistant a] [--model m] [--persona <id>])
```

- Terminal ids are in the session address book, `GET /v1/orchestrator/sessions` (`clawdline guide
  send`); the Session must work in the Epic's Project. You may assign a child to yourself, and
  `--assign-new` opens a new Session with a Root Assignment that names the Epic. Without an
  `--assign` flag the child waits unassigned for the person.
- `item child` reads the Epic for its version, prints its Idempotency-Key (`--key` retries the same
  write) and prints the child. It is `POST /v1/work/v2/agent/items/<epic id>/children` with
  `{"expected_version", "session_id", "kind", "title", "description", "steps"?, "deployment_policy"?,
  "assign"?: {"mode": "existing_session", "terminal_id"} | {"mode": "new_session", "assistant"?,
  "model"?, "persona"?}}`, answered `201` with `{"item", "assigned", "assignment_error"?: {"code", "message"}}`.
  The child is in the Epic's Project, carries `parent_id` (the Epic), and its card says the Epic's
  owner Session created it. Its steps are your `--step` rows, or — when you give none — its
  description's list once it is assigned.
- The child is created first and assigned second. When the assignment fails the child **stays,
  unassigned**, the answer carries `assignment_error` with the assignment's code
  (`session_unavailable`, `project_mismatch`, `assignment_failed`, …), and the command exits 1:
  assign it again with `item assign`, or leave it for the person.
- `item assign` is `POST /v1/work/v2/agent/items/<child id>/assign` with `{"expected_version",
  "session_id", "mode", "terminal_id"? | "assistant"?, "model"?, "persona"?}`; it moves an open child of your Epic
  to another Session, the same assignment a person's choice makes.
- Refusals, each writing nothing: `not_epic_owner` (you are not the Epic's owner), `parent_not_epic`
  (the parent is not an Epic), `epic_not_planned` (the Epic is still before `implementing`: the
  children come out of a reviewed plan), `item_terminal` (the Epic is finished),
  `child_kind_not_allowed` (only `feature` or `issue`), `epic_children_full` (an Epic holds at most
  32 children, open or closed), `not_epic_child` (`item assign` of an item that is no Epic's child —
  the person assigns it, unless their message asks you to: `clawdline guide board`), `invalid_assignment`, `version_conflict`, `persona_not_applicable` (422: a
  persona with an existing Session) and `unknown_persona` (400: an id the catalog lacks).
- **A persona** is a role a new Session is launched with: text added to its system prompt that makes
  it work the way that role works, for the whole conversation. It is only for a new Session
  (`--assign-new`, `--new`, `dispatch`); an existing Session keeps the one it opened with. None by
  default. A persona never overrides `CLAUDE.md`/`AGENTS.md`, the brief, `CHILD.md` or this
  protocol. `GET /v1/personas` lists them; the ids (`teams` on each lists every team a persona is in, and one may be in several):
  - `architect` — planning an Epic;
  - `backend` — a daemon, API or store feature;
  - `frontend` — a console or phone-layout feature;
  - `minimal-change` — an issue: the smallest fix that holds;
  - `code-reviewer` — review and `plan_review` children;
  - `reality-checker` — verifying: evidence before "it works";
  - `security` — work touching permissions, pairing or Cloud;
  - `technical-writer` — docs and guides;
  - marketing, for a blog, site or docs repository: `seo` (pages and metadata), `content-writer`
    (articles drafted in files), `ai-search` (pages AI answer engines can cite), `social-media`,
    `instagram`, `email` (newsletters), `growth` (measured experiments) and `pr` (announcements).
  - product, quality and operations: `product-manager`, `sprint-prioritizer`, `feedback-synthesizer`,
    `trend-researcher`, `ux-researcher`; `test-automation`, `accessibility`, `performance`,
    `api-tester`, `evidence-collector` (rules PASS or FAIL per claim from captured proof); `sre`, `devops`,
    `incident-commander`, `finops` and `secrets`.
  - design and business: `ui-designer` (screens in the project's design system), `ux-architect` (flows
    and layout structure), `brand-guardian` (brand consistency), `ui-finish-gate` (the visual check
    before shipping), `image-prompt` (image-generation prompts), `pricing`, `customer-success`,
    `support` (drafted replies), `analytics` (answers from real data), `devrel` (samples that run)
    and `privacy` (personal-data checks; not legal advice).
  - `zero-review-lead` — owns a review Epic that re-examines an existing feature or process from
    zero: plans the role lenses, dispatches them as read-only reviewer children with one shared fact
    pack, and turns their evidence into a target design; its skill is `zero-based-review`.
- **Add an independent UX/product review when the Epic changes a person-facing experience.** In the
  plan, classify whether the Epic changes a human-facing interface, user journey, or product policy.
  If it does, before merging dispatch at least one read-only specialist child, using `ux-architect`
  by default for layout, interaction and end-to-end product flow:

  ```
  clawdline dispatch --kind review --work-id <epic id> --claims "" --persona ux-architect --permission-mode ask …
  ```

  Its brief names the integrated candidate and asks for desktop and smallest-supported-mobile
  evidence, keyboard and screen-reader behavior, dead ends, product fit, severity and a concrete
  recommendation. It must **mark unverified and say why** where evidence is unavailable. Use
  `product-manager` instead when policy and scope, not layout, is the dominant risk; add
  `ui-finish-gate` when a separate pre-ship visual pass is material. Resolve every blocking finding
  and record the task id, verdict and disposition in the Epic's verification evidence or completion
  report. If there is no person-facing impact, say why in the plan and do not add review ceremony.
  Record the scope this review covered. Normally dispatch each relevant specialist only once for
  the integrated Epic; do not routinely send UX, brand, security, and other roles as a checklist.
  Close small copy, spacing, test, or in-scope finding corrections yourself with focused checks.
  Redispatch only when a later change materially alters the user journey, product policy, brand
  direction, security boundary, or another risk outside that recorded scope; name the changed
  boundary and request only its relevant specialist. This review never substitutes for the
  captured plan gate or the verification gate's exact-candidate checker PASS.
- **You remain responsible for the Epic after each child is merged.** Immediately reread that
  child's item and `clawdline item steps <child id>`; check that every step is complete. A merge
  does not close the child, and `merging` is not a resting state. The child's owning Session must
  complete any remaining steps, record a landing receipt for the exact commit already reachable
  from the local target and `origin/main`, then advance `deploying` → `done` with deployment evidence
  or a no-deployment reason matching its deployment policy. If you own the child, do those actions
  yourself. If another Session owns it, follow up with that owner or use the authorized child
  reassignment path; do not impersonate its owner (`not_item_owner`). ACK the broker completion
  notice where one exists, then classify worktree residue and remove only proven landed-identical
  or task-temporary material. Keep unlanded, mixed, or unknown bytes for the next owner. Do not
  declare the parent Epic done until every child is `done` or `cancelled`: `epic_children_open`
  names how many remain. Create no Board item other than the Epic's children.
- **A completed child does not close its independent Feature Root Session.** For every Root opened
  by the Epic with `--assign-new`, use `clawdline session close --dry-run --terminal <id>` (it answers
  `closing as epic_owner` only for a Root this Epic opened) to read its `closeability`; do not infer ownership from a label or terminal position,
  and do not treat `clawdline session report` as closure. After the child reaches `done`, ask that
  Root's owner to audit its own tasks, landings, notices, to-dos, and worktree, then complete its
  close report. Obtain an attestation only through a route supported by the current daemon;
  the retired Swift closure route is not such a route. If that route or a guarded close is
  unavailable, record the product blocker and next owner, and retain the Session. Only when
  identity and work are verified and
  `closeability.state=safe` may `clawdline session close --terminal <id>` end it; it rereads the
  inventory first, and a second run answers `session_not_found` once it is gone. Do not bypass the guard with `clawdline close <terminal id>`. Follow up on
  `blocked` with its named mover. For `unknown` (including `terminal_unreadable`), preserve the
  Session and record the missing evidence and next owner; do not force-close, archive, or claim it
  was cleared. Before declaring Epic coordination finished, enumerate each Root's close result or
  named blocker. Board `done` does not replace this inventory.

## 11. Coordination

**The machine coordinator ("Clawdfather").** It works from a daemon-owned machine workspace, outside Projects, to report on Sessions and manage supported machine operations. It never edits Project source code, including Clawdline. On the person's explicit request for engineering work, create a Project Board item first with `clawdline item add --project … --assign-new`, then delegate to a Project Session. Without that request, propose an item for the person to accept. The assigned Project owner handles child dispatch, verification and landing. The workspace is an organizational boundary, not a filesystem sandbox. New bindings must come from that workspace; existing bindings stay readable. Open the Session from the console's separate Clawdfather action, then register its conversation ID. See `docs/clawdfather-role.md` for the product boundary.
Run `clawdline coordinator bind` inside that new Session to register it or rebind a proven offline predecessor. The command reads its own conversation ID and refuses to replace an online or unreadable holder.
`GET /v1/orchestrator/coordinator` inspects the role;
`/coordinator/bearings` is the machine at a glance (active tasks, pending landings, open waits,
dead letters, held leases, and what is `unknown`). `POST …/coordinator/register` with
`{"session_id": "<conversation id>"}` takes the role; `POST …/coordinator/rebind` moves it once
the bound session is offline (`expected_coordinator_id`, `expected_generation`). Succession
answers `501 succession_unavailable`.

**File waits.** A wait says "tell me when the owner is done with these paths". It is a record and a
message, not a lock or a file watch.

- `POST /v1/orchestrator/waits` —
  `{"repository", "paths", "owner_session_id", "waiter_session_id", "reason", "release_condition"}`
  (session ids are conversation ids). The owner is told once, in its composer.
- The owner ends it: `POST /v1/orchestrator/waits/<id>/release` with `{"owner_session_id",
  "commit"?, "note"?}`; each waiter is told. Nothing releases a wait on a timer.
- A waiter leaves: `POST …/waits/<id>/cancel` with `{"waiter_session_id"}`.
- `409 owner_busy` and `502 request_delivery_failed` mean **the wait was recorded** but the owner
  was not told yet. `502 release_incomplete` lists who is still pending: send the release again.

**Leases.** Two resources: `heavy_compile` (the machine's one compile slot) and `landing` (one per
checkout).

**Run a build or a test suite through `clawdline heavy -- <command>`**, not bare. It queues for
`heavy_compile`, waits until the machine has memory available (a quarter of it, at most 1 GB, and
no memory stall above 10%), runs the command at a lower priority — on Linux also as the first
thing the kernel kills if memory runs out — renews the lease while it runs and releases it after. It keeps the
command's exit status. It never refuses to build for a missing daemon or a refusal it does not
know: it runs the command anyway with a sentence on stderr. When `--max-wait` (default 30m) passes
before it has the slot and the memory, it gives up its place, does not run the command, and exits
**75** — a code no command's own failure is mistaken for; run it again later. While it waits it
prints one line when the wait starts and one when it ends, nothing in between: wait for it once,
long (§2, "Waiting on a long command"). A `heavy` inside a `heavy` runs directly. `--min-available 1500M` asks for more; `--no-slot` checks memory only. In a repository
that has it, `tools/heavy.sh <command>` finds the binary for you.

- `POST /v1/orchestrator/leases` —
  `{"request_id": "<uuid>", "resource", "checkout" (landing only), "holder", "reason", "session_id", "pid"}`.
  Answers `granted`, or `queued` with a `position` and `retry_after_seconds`. A queued request asks
  again with the same `request_id`.
- `POST …/leases/renew | release | cancel` with `{"request_id", "resource", "checkout"}`. The holder
  renews with `/renew` (asking `POST /v1/orchestrator/leases` again with its own `request_id`
  renews too).
- Renew within 60 seconds or the lease is presumed gone. `409 lease_lost` means it was.
  `429 queue_full` at 32 waiters.

**Graphs** (`GET /v1/orchestrator/graphs`) are read-only views computed from dispatched tasks'
`graph` fields. **Reclaim** (`/v1/orchestrator/reclaim`) sweeps finished checkouts; a POST is a dry
run unless the body says `{"dry_run": false}`.

## 12. What to do when something is refused

- Branch on `error.code` (or `error` in the flat shape). The message is for people.
- `retry_after` means it is a capacity answer: wait that long, then send the same request.
- `409 stale_write`, `503 orchestrator_store_busy`: the store was busy; the same request again is
  safe.
- `unknown` anywhere — an ownership, a liveness, a source — means the daemon could not read it. It
  is not "absent", and nothing should be deleted or declared dead on it.
- If a route you expected answers `404 not_found` or `501`, it is not in this daemon. Say so; do not
  fall back to the Swift app's routes or to provider-native subagents in its place.
