---
name: clawdline
description: |
  Use Clawdline to dispatch a bounded child task when the current Root keeps synthesis,
  integration, and landing; to hand off an existing work line for full continuation; or to send a
  message, report, status, finding, or coordination note to another live session; or to put a
  **local image** — a screenshot, a generated chart — where the person can actually see it, on the
  phone they are reading on, rather than pasting its file path; or to record this
  session's own finished turn as a delivery receipt — **the check that says this session is done on
  Clawdline's own screen** — which applies in **any** repository Clawdline is watching, not only its
  own. Triggers include
  "dispatch a task", "open a child session", "get Codex to review this", "use Clawdline Handoff",
  "report my milestone", "record this turn as delivered", "show this session as finished in
  Clawdline", "show the user this screenshot", and the equivalent Chinese
  requests 「派任務」「開 child」「使用 Clawdline Handoff」「交接給下一個 session」「回報這一輪做完了」
  「更新 milestone」「在 Clawdline 上顯示完成」「把這張截圖給他看」. A handoff transfers
  the sender's REFERENCES, VERIFICATION, and OPEN THREADS. Detached poll-only tasks are unattended
  automation, never Root or Major Feature owners. Root Assignment / Feature Launch opens an
  independent ordinary Root and must not be faked with a child, detached automation, or handoff. Do not use for
  work this conversation can simply do, provider-native subagent research, or session inventory.
  When this session is a Clawdline child, CHILD.md governs instead.
---

# Clawdline

**This file is a discovery stub, not the usage guide.** The complete, version-matched reference
ships inside the Clawdline app bundle, kept out of this file on purpose so it can never drift from
the build that will actually broker your dispatch.

## Reporting this session's own finished turn

**Clawdline watches every terminal on this Mac, not only sessions working in its own repository.**
So a root whose turn is genuinely complete — in whatever project it is sitting in — reports one
session-scoped delivery receipt as its last tool action before the final answer. That is the
milestone: it draws one check, *delivered; awaiting approval*, and never claims independent review
or a broker-verified landing.

The route, the id it takes, and its typed refusals are in the guide's **"Report the root's
completed turn"** section. Load the guide the way the next section says and read it there; the
commands are deliberately not repeated here, because a copy of them goes stale against the build
that will actually record the receipt.

It is only for a turn that is actually finished. Partial work, a diagnosis-only answer, a blocked
task, and a question back to the user are none of them a milestone — and a Clawdline **child**
reports through its own `result.json` instead.

## Load the guide before you do anything else

Resolve the reader once, and reuse it for every later step:

- If `CLAWDLINE_SKILL_READER` is set, use its value.
- Otherwise `/Applications/Clawdline.app/Contents/Resources/clawdline-skill.sh`.
- Otherwise `$HOME/Applications/Clawdline.app/Contents/Resources/clawdline-skill.sh`.
- Otherwise, inside a checkout of this repository, `Resources/clawdline-skill.sh`.

Below, `READER` stands for the path you resolved. Substitute it before running anything; do not
create a shell variable and do not run `READER` literally.

```
READER get clawdline          # English
READER get clawdline.zh-TW    # 繁體中文
READER list                   # what this build carries
```

That prints the complete guide for the exact build installed on this machine: the six dispatch
steps, handoffs, detached automation, Root Assignment, `claims` and `serialize`, landing closure,
file waits, schedules and the coordinator routes. **Read it first, then run the command you need.**

It is a local file read. It does not need Clawdline to be running, so it answers the same way over
SSH, in a headless context, and while the app is closed.

**Do not guess routes, fields or flags from memory or from a cached copy of this stub.** They
change between releases, and this file deliberately no longer lists them.

If the reader you selected cannot run, report its exact error and stop. Do not fall through to
another path: a different path may describe a different Clawdline build than the one that will
broker your dispatch.

## If no reader exists

Only when every path above is missing. That means Clawdline is not installed here, so say so
rather than inventing routes. One read-only call tells you whether it is merely unfound:

```
PORT=$(jq -r '.remote_port // 7717' ~/.config/clawdline/config.json 2>/dev/null || echo 7717)
curl -s "http://127.0.0.1:$PORT/v1/health"
```

If that answers, the app is running but this stub could not find its bundle. Report both facts and
stop; do not dispatch from memory.

## The one thing this stub states outright

Which route carries which kind of work is a role boundary, not a routing detail, so it is written
here as well as in the guide. It is the same closed contract every Clawdline surface carries.

<!-- clawdline-dispatch-role-contract:v1 -->

- **Owned child.** `POST /v1/orchestrator/tasks` creates a bounded child only when Clawdfather
  retains synthesis, integration, and landing.
- **Handoff.** `POST /v1/orchestrator/handoffs` is continuation or transfer of an existing work
  line; the receiver must walk the sender's complete REFERENCES, answer VERIFICATION, and continue
  from OPEN THREADS.
- **Detached automation.** `POST /v1/orchestrator/detached-tasks` is the only public route that
  accepts `root.session_id: null` with `root.poll_only: true`; ordinary
  `POST /v1/orchestrator/tasks` refuses poll-only. It is only unattended automation, never a Root
  or Major Feature owner.
- **Root Assignment / Feature Launch.** `POST /v1/orchestrator/root-assignments` opens an
  ordinary independent Root and briefs only objective, scope, constraints, relevant references,
  and acceptance. Its durable machine-auth record and UI classification carry no child, handoff,
  detached, timeout, secret, result, parent, or landing lineage.

<!-- /clawdline-dispatch-role-contract:v1 -->
