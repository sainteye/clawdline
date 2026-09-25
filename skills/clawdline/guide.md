# Clawdline guide

For an assistant session — Claude Code or Codex — on a machine where **Clawdline Next** runs. It
covers what this daemon serves today, and nothing else: every route below is registered by the
build that printed this guide, and a test fails when one is not. Print it again with
`clawdline guide` rather than trusting a copy; `clawdline guide zh-TW` is the same guide in
Traditional Chinese.

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
   `501 succession_unavailable`), a cancel route for tasks, and the brief fields `serialize`
   and `attach_session` (each refused by name as `bad_task`). `reasoning_effort` is supported:
   `high` or `xhigh`, on a `codex` task only.

## 1. Root or child

If your first message said *"You are a Clawdline CHILD agent for task …"*, you are a **child**. The
`CHILD.md` it names governs you: you do not dispatch, you do not send a turn receipt, and you finish
with `clawdline task finish`. Stop reading here.

Otherwise you are a **root**: an ordinary session a person is talking to. The rest is for you.

## 2. Reaching the daemon

**Use the commands, not hand-built curl, where one exists.** They read the credential inside their
own process, so it never appears in a command line, in `ps`, in their output or in your
transcript.

| Command | What it does |
|---|---|
| `clawdline guide [zh-TW]` | This guide. No daemon needed |
| `clawdline session report --summary "…"` | Records your finished turn (§7) |
| `clawdline send --to <terminal> "…"` | Relays a message into another session (§8) |
| `clawdline notify --title "…" --body "…"` | Pushes a notification to the person (§9) |
| `clawdline assistants` | What each assistant's account has left |
| `clawdline landings` | Every landing still owed on this machine |
| `clawdline cloud pair [--offer <code>]` | Pairs one Cloud browser with this machine |
| `clawdline task finish <task dir>` | A child's completion. Roots never run it |

The orchestration commands above print the daemon's JSON on success; on a refusal they print
`refused, <status> <code>: <message>` and exit 1. Cloud commands use their own human-readable
success and error output. `--port` overrides the port.

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
one gets `401 unauthorized` "This needs a paired device." — the wording is about devices, the
cause is the token.

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
  fields; it moves when a row starts, ends or changes claims.
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
the request, so the child reads the same bytes that were validated.

| Field | Rule |
|---|---|
| `clawdline_protocol` | `1` |
| `task_id` | the same id |
| `assistant` | `claude` or `codex` |
| `project_dir` | absolute path to an existing directory |
| `title` | shown on screen; cut at 200 characters |
| `instructions` | required, at most 16 KiB. They must stand on their own: the child knows nothing else |
| `claims` | **required**: at most 32 relative paths the child may write. `[]` means it writes nothing and is warned about (`claims_missing`) |
| `isolation` | `none` (default) or `worktree` for a private checkout on its own branch |
| `permission_mode` | `ask`, `edits` or `full` |
| `timeout_minutes` | 1–240, default 30 |
| `kind`, `deliverables`, `model` | optional; `model` is `[a-z0-9._-]`, at most 64 characters |
| `work_id` | optional UUID of the board item this serves |
| `root` | **required**: `{"session_id": "<your conversation id>", "assistant": "claude"\|"codex", "label": "…"}` |

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
`claims_ignored_for_worktree`, `dirty_worktree_base`. Posting the same id again answers the stored
task with `replayed: true`, so a retry is safe.

A tab that fails to open still answers 200, with `task.state: "spawn_failed"`.
`POST /v1/orchestrator/tasks/<id>/respawn` (orchestrator token) opens a copy with a new secret, at
most twice per original.

**Refusals you will meet**, in the order they are checked:

| Status | Code | What to do |
|---|---|---|
| 422 | `bad_task` | The message names the field. Includes "No readable task.json under …" — check `task_root` |
| 422 | `claims_required` | Add `claims` |
| 422 | `root_session_required`, `root_assistant_required` | Add `root.session_id` and `root.assistant` |
| 422 | `detached_route_required` | You sent `root.poll_only`; that is detached automation (§6) |
| **409** | **`stale_inventory`** | Your `generation` is missing or old. The whole current inventory is inside the error: read it, decide again, resend with its `generation` |
| 409 | `graph_*` | A task-graph admission rule (the `graph` field) |
| 409 | `no_child_capability` | This platform cannot open a child; `missing` says what |
| 429 | `rate_limited` | Too many dispatches in ten minutes |
| 422 / 409 | `root_unresolved`, `conversation_ambiguous` | Your conversation id matches no live session, or more than one. Fix it; do not switch to detached |
| 429 | `over_capacity` | Your child slots (default 5) or the machine's are full; `retry_after` |
| 409 | `workspace_busy` | Another root's claims overlap; the error names the blocking task |
| 409 | `worktree_unavailable` | The private checkout could not be made |
| 429 | `terminal_busy` | Every terminal-write lane is busy; `retry_after: 5` |

## 5. While it runs, and when it finishes

The child signs for its briefing (`/accepted`), may send one progress note when its plan changes
(`/progress`), may push up to five notifications (`/notify`), and finishes by writing `result.json`
and running `clawdline task finish`. You do not call those routes.

- `GET /v1/orchestrator/tasks/<id>` — one task, with its state. `GET /v1/orchestrator/tasks` lists
  them (`?state=`, `?limit=` up to 500).
- **When it finishes, the daemon types a `<clawdline-notice>` line into your composer** with the
  state, the path of `result.json` and a `notice_id`. It retries on a 5→300-second ladder, eight
  times, until you acknowledge it — and never types while you are showing a menu:

  ```
  POST /v1/orchestrator/tasks/<id>/completion/ack   {"notice_id": "…"}
  ```

  A second ACK answers `changed: false`. Unacknowledged notices are listed at
  `GET /v1/orchestrator/completions`; `POST /v1/orchestrator/completions/reconcile` re-arms them.
- There is **no cancel route**. A task ends by finishing, failing or timing out.
- **A finished child is not landed code.** Its work sits in the shared tree or on its branch until
  you integrate it.

## 6. Landing, and the other three kinds of work

**Record the landing obligation** as soon as a child with claims comes back:

```
POST /v1/orchestrator/tasks/<id>/landing
{"state": "pending" | "landed" | "abandoned" | "nothing_to_land", "target": "<ref>", "commit": "<sha>", "note": "…"}
```

- Only these keys, plus `delivery`, which is accepted and not used; any other key is refused.
  `pending` and `abandoned` accept the task secret or the orchestrator token; `landed` and
  `nothing_to_land` accept the orchestrator token only.
- `landed` needs `target` and `commit`, and the daemon **checks it in Git**; otherwise
  `409 unverified_landing` with a `reason` (`target_unresolved`, `commit_unresolved`,
  `not_on_target`, `predates_dispatch`, `nothing_delivered`, `not_the_delivery`, …).
- `nothing_to_land` is refused with `409 wrote_to_repository` when the task did write.
- A settled landing cannot change: `409 invalid_transition`, or `409 landing_conflict` for a
  different value.

`clawdline landings` (`GET /v1/orchestrator/landings`) is every pending landing on the machine, each
with an `ownership.status`. `unknown` is not "nobody": it means the evidence could not be read.
`503 landings_incomplete` means some rows could not be read, and no shorter list is offered in
their place.

**Two roots landing into one checkout** take a landing lease first (§11).

The other three kinds of work each have their own route. Which one is a boundary, not a detail:

| Kind | Route | What it is |
|---|---|---|
| **Handoff** | `POST /v1/orchestrator/handoffs` | You give an existing line of work, with its full state, to a new session |
| **Root assignment** | `POST /v1/orchestrator/root-assignments` | A new, independently owned Root for a new feature |
| **Detached automation** | `POST /v1/orchestrator/detached-tasks` | Unattended work with nobody to report to |

**Handoff.** Write `<state dir>/handoffs/<handoff_id>/handoff.md` first (the list route answers
`package_root`). It should carry three sections: **REFERENCES** (everything the receiver must
read), **VERIFICATION** (questions it answers from those sources before continuing) and **OPEN
THREADS** (where to pick up). Then post, with a closed body:

```
{"handoff_id": "<uuid>", "from_session": "<your conversation id>", "coordinator_plain_handoff": true,
 "project_dir": "/abs", "assistant": "claude"|"codex", "model": "…", "title": "…"}
```

The receiver is told to read the file, walk its references, answer its verification questions and
continue. You get one `handoff_receipt` notice when it picks up. Refusals: `bad_task` (a missing or
empty `handoff.md` included), `sender_not_found`, `sender_ambiguous`, `rate_limited`,
`terminal_busy`, and `succession_required` if you hold the machine coordinator role — succession is
not available in this daemon (`501`), so that session cannot hand off.

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

## 7. Report your own finished turn

When your turn is genuinely finished — the work done, verified and committed where that applies —
make this your last action before the final answer:

```sh
clawdline session report --summary "One concrete sentence about what was delivered."
```

It draws one check on your session's row: **delivered, awaiting approval**. It is weaker than a
landing and claims no review. It shows while the daemon reads the session as idle — working,
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

## 10. The board

The board has three structures — board items, the Backlog, and each session's own to-do list — and
**a person decides what goes on it**. A session creates a Board item only when the person's own
message, sent through Clawdline, tells it to; otherwise it proposes. It never files a card on its
own initiative.

**TODO / 待辦 / 土度 said together with a Board item means that item's steps.** Put them on the
item with `--step`. Do **not** also write them with `clawdline todo add`. `clawdline todo add` is
only for a list the person asks you to track as this Session's own to-dos with no Board item.

**When the person tells you to create a Board item.** Only when their message — sent through
Clawdline, so it has a run — explicitly asks for one, create it yourself:

```
clawdline item add --project <place id> --kind feature|issue|epic|refactor|plan --title "…" \
  --step "first step" --step "second step" …   [--description-file f | description on stdin]
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
  "title", "description", "deployment_policy"?, "steps"?: ["…"]}`.
- A Feature or Issue arrives **assigned to you**, in `assigned`, with its steps: the `--step` rows
  in order, or — when you give none — two or more top-level Markdown list rows of the description.
  Nothing is typed into your terminal; you asked for it. Work the steps in order, complete each
  one when it is verified (`clawdline item steps <item id>`, `clawdline item step-done <item id>
  <step id>`), and advance the phases as for any assigned item. An Epic, Refactor or Plan is created
  unassigned, in Planning, and takes no steps (`planning_has_no_steps`).
- The person sees the card marked "Created by the Session from your message at HH:MM", with their
  words quoted.
- Refusals, each writing nothing: `run_unknown` (no run named, or none issued), `run_expired`
  (older than a day), `run_other_session` (a message to another Session), `session_not_found`,
  `child_session` (a child reports through `result.json`), `project_not_found`,
  `project_mismatch` (an executable item must be in the Project you work in), `too_many_steps`
  (more than 128), `run_items_exhausted` (one message backs at most five items).
- **No run** — the person typed straight into the terminal, so `item add` answers `no_run` or
  `run_unknown`: fall back to a proposal (below) and tell the person to accept it in the Board's
  Agent proposals.

Never create a Board item on your own initiative, and never several to plan speculative work.

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
- **To propose an item with a TODO list**, write the list as two or more top-level Markdown list
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
{"session_id": "…", "project": "<project>", "question": "…", "options": [{"id": "a", "label": "…"}, …],
 "default": "a", "blocking": true, "due_in_minutes": 1440}
```

Two to four options; `default` must be one of them and is what happens when nobody answers (after
7 days unless `due_in_minutes` says 60–10080). Only a `blocking` decision is pushed. Read the answer
with `GET /v1/orchestrator/decisions/<id>`.

**The person answers; a session only relays what they said.** Proposals, decisions and board items
are answered under `/v1/work/…`. A session writing there must name the run that carried the
person's words, `"via": {"run": "<id>"}`, and is refused without it (`403
session_cannot_decide`). Read the latest run at
`GET /v1/orchestrator/sessions/<conversation>/run`; an invented, expired, other-session, or
pre-question run is refused by name. A person typing straight into a terminal has no run, so ask
them to answer through Clawdline or in the console.

**Your to-do list.** `GET /v1/orchestrator/sessions/<conversation id>/todos` — named by
conversation id, not terminal (`409 session_id_is_terminal` otherwise). The broker opens and closes
these from task facts; there is nothing to write.

At every turn boundary, before declaring yourself idle, also read
`GET /v1/work/v2/agent/session-todos/<conversation id>`. Its `assigned_items` are Board items the
person has given this Session, its `recent_items` are items this Session recently completed, and its
`direct_todos` are quick requests. This pull is how an assignment made while you were working waits
without interrupting the current turn. Finish the current turn, then take the assigned item as your
next owned work and read its complete record at `GET /v1/work/v2/items/<id>`.

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

When the owning Agent needs an action from the person, use the machine-authenticated route:

```
PATCH /v1/work/v2/agent/items/<id>/edit     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "condition": "waiting_user", "user_action": "The one concrete action the person must take"}
```

`user_action` is at most 8 KiB and belongs only to `waiting_user`; omitting the concrete action or
putting one on another condition is refused by name. When the wait ends, set `condition` to the
empty string on the same route; the daemon clears `user_action` with it so the Board cannot retain
a stale request.

An assigned item may contain `steps`. A successful assignment can seed them from two or more top-level
Markdown list rows in the description, and an item you created with `clawdline item add` carries
its `--step` rows. Each step is an item-local TODO, not another Board item.
Complete a verified step with an idempotent machine-authenticated request to
`POST /v1/work/v2/agent/items/<item-id>/steps/<step-id>/complete`, body
`{"expected_version": <item version>, "session_id": "<your conversation id>"}`. Reread after a
version conflict. A transition to `done` is refused with `steps_incomplete` while any step remains
open; Clawdline never checks one merely because the parent phase advanced.

When resolving an issue or incident required substantial investigation to discover the root cause
or to distinguish the real fix from plausible alternatives, add a user-readable completion report
before advancing the item to `done`. A straightforward, directly observed correction does not need
one. Use the machine-authenticated, idempotent document route:

```
POST /v1/work/v2/agent/items/<id>/documents     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "role": "completion_report", "title": "Completion report",
 "body": "What happened, the root cause, what changed, how it was verified, and any remaining boundary"}
```

The body is Markdown, at most 64 KiB. Write for the person who reported the problem, not as a raw
debug log, and keep private data out of it. The active owner must add it before the item becomes
terminal; reread after a version conflict. A completion report is attributed narrative and never
replaces verification, landing, or deployment evidence. When present, it remains on the closed
Board item and opens directly from the Session's Recently Done row.

`/v1/board` is the Swift app's old cards, read-only. Landing is a broker fact: an item is never
marked landed by hand (`422 landing_is_broker_fact`).

## 11. Coordination

**The machine coordinator ("Clawdfather").** `GET /v1/orchestrator/coordinator` inspects the role;
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
