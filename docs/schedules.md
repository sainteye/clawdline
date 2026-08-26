# Scheduled tasks

A schedule is a task template Clawdline turns into an ordinary orchestrator dispatch at a local
wall-clock time. It is for work that should open a real Claude Code or Codex session, inherit the
same claims, serialization, capacity, depth, permission and trust checks, and leave the same task
record as work dispatched by a session.

Put one JSON file per schedule at:

```text
~/.config/clawdline/schedules/<schedule-id>.json
```

The filename and `schedule_id` must be the same lower-case UUID. Files are the source of truth.
Clawdline rereads them, rejects unknown fields, exposes each bad file as an `invalid` row, audits
and notifies once per invalid content revision, and continues loading the valid neighbors.

One route writes one: [`POST /v1/orchestrator/schedules`](#making-one-without-a-text-editor). It
creates a file and never edits or deletes one, and it is the only thing in the app besides a text
editor that can. Changing an existing schedule is still done in the file, with the one exception
below.

The Settings app owns one convenience edit: its switch changes only the top-level `enabled`
boolean in place. Every other byte and every other field is always yours; edit those in the JSON
file itself. If an unusual but valid JSON representation cannot be edited in place, Clawdline
falls back to a full JSON rewrite and records that fallback in the audit log.

## The file

```json
{
  "clawdline_schedule": 1,
  "schedule_id": "4d2f54ce-b4b5-4f60-8623-34011f35aa43",
  "title": "Publish the next post",
  "when": { "at": "09:30", "days": ["mon", "wed", "fri"] },
  "task": {
    "assistant": "codex",
    "model": "gpt-5.6-sol",
    "project_dir": "/Users/me/code/blog",
    "title": "Publish the next post",
    "instructions": "Read the publishing checklist, publish the next ready post, and report it.",
    "claims": ["posts", "public"],
    "serialize": ["deploy"],
    "permission_mode": "full",
    "timeout_minutes": 45,
    "deliverables": ["the published URL"]
  },
  "enabled": true,
  "close_tab": "on_success",
  "catch_up_hours": 6,
  "notify_on_failure": true
}
```

- `clawdline_schedule` is exactly `1`.
- `schedule_id` is a lower-case UUID and matches the filename.
- `title` (at most 120 characters) names the schedule in the API, root label and push notification.
  Audit rows identify the stable `schedule_id` instead.
- `when.at` is `HH:MM` in the Mac's current local time zone. It is not UTC.
- `when.days` is `"daily"` or a non-empty, duplicate-free array drawn from `sun`, `mon`, `tue`,
  `wed`, `thu`, `fri`, `sat`.
- `task` contains the task-template fields `assistant`, optional `model`, `project_dir`, optional
  `title`, `instructions`, `claims`, `serialize`, `isolation`, `isolation_base`,
  `permission_mode`, `timeout_minutes`, `deliverables`, `kind`, and `plan`. Their validation is the
  same as [`task.json`](orchestrator.md#taskjson--written-by-the-root-before-it-asks-for-anything).
  Clawdline generates `task_id`, the task secret, and `root`; templates cannot set them.
- `enabled` is a required boolean. A disabled schedule is listed but never fires.
- `close_tab` is `on_success` (the default), `always`, or `never`. `on_success` closes immediately
  after success but leaves failures, timeouts and spawn failures for takeover. `always` closes any
  terminal outcome, including cancellation. These two explicit per-schedule choices take priority
  over the global child-linger preference, including across an app restart. `never` adds no
  schedule-specific immediate close and follows the global orchestrator linger policy instead.
- `catch_up_hours` is an integer from `0` through `168`, default `6`.
- `notify_on_failure` is a boolean, default `true`. It covers missed catch-up windows, dispatch
  refusals, failures, timeouts and spawn failures.
- `created_at` is a Unix timestamp in seconds, written by `POST /v1/orchestrator/schedules` and
  never named by the request. It exists so that an occurrence from before the schedule was made
  is neither run nor counted as missed: without it, making a `09:00` schedule at one in the
  afternoon opened a session within the minute, and making one in the evening pushed a
  notification about a run that was never owed. **A file without it is not wrong** — a schedule
  written by hand has always meant "as far back as anyone knows", and it still does.

A scheduled task may also use the task-secret `/notify` route to push that day's **successful
content** to the user — a daily forecast is the canonical shape. Say so in `task.instructions`;
`notify_on_failure` is only Clawdline's separate state/failure notification policy. This explicit
content delivery deliberately bypasses the automatic push preference switches; a separate
agent-content preference remains backlog work.

## Making one without a text editor

`POST /v1/orchestrator/schedules` is the only route that writes a schedule file. It takes the
fields a person filled in, generates the id, assembles the object above, and hands it to the same
parser every source file goes through — then reads the file back off disk through that parser
before answering. A schedule this app cannot itself parse must not survive the request that made
it: it would come back as an `invalid` row, audit itself and send a push, and nobody would be able
to say which request left it there.

```sh
curl -s -X POST "http://127.0.0.1:$port/v1/orchestrator/schedules" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"title":"Publish the next post","at":"09:30","days":["mon","wed","fri"],
       "place_id":"3f2a91c47e0b5d68","assistant":"codex",
       "instructions":"Read the publishing checklist and publish the next ready post."}'
{"ok":true,"schedule":{"id":"4d2f54ce-…","title":"Publish the next post","enabled":true,
                       "next_fire":1787880600}}
```

**It is behind the write gate a paired device passes, not the orchestrator token.** The three
gates are the ones `POST /v1/voice` and `POST /v1/intents` already sit behind: the write switch in
Settings → Remote, the `send` capability on the device, and an `Idempotency-Key`. The orchestrator
token is a `0600` file on this Mac — that is what makes it a proof of being local — and a phone
cannot have one. This route exists for the phone, and sending the orchestrator header instead of a
device token is refused like any other device that may not send.

**A `place_id`, never a path.** The body has nowhere to write a directory. `place_id` is an id
from [`GET /v1/places`](api.md#get-v1places) and is resolved against that list on the Mac, which
is the argument `POST /v1/places/:id/start` makes and is worth making twice: a device can only
name a project this Mac has already shown it. An id that is not on the list is a `400`, never a
guess, and `project_dir`, `claims`, `permission_mode` and every other task-template field are not
fields a request may carry at all.

Then the part that is genuinely new, because burying it would be the only dishonest way to write
this page: **a paired phone can now arrange work that runs later, in a session nobody is watching,
in a project that was on the list when the phone last looked.** Everything before this needed
somebody at the Mac — either at a terminal with the orchestrator token, or in a text editor. What
bounds it is what bounds the rest: the switch that can be turned off, a device that can be
revoked, the same capacity and claims checks every dispatch meets when the schedule finally fires,
and a written record of every one of them. A schedule created while task dispatch is switched off
is listed and never fires.

The fields are the ones in [The file](#the-file), flattened, plus the `place_id` that replaces
`project_dir`:

- required: `title`, `at`, `days`, `place_id`, `assistant`, `instructions`.
- optional: `enabled` (default `true`), `close_tab`, `catch_up_hours`, `notify_on_failure`,
  `timeout_minutes`, `model`. An empty `model` is left out of the file rather than written into
  it as an empty string.
- written by the Mac, not by the request: `schedule_id`, the filename it must match, and
  `created_at`.
- `days` is **not** defaulted. A request that does not say which days is refused, because
  choosing `daily` on somebody's behalf is choosing how often their work runs. `enabled` is the
  opposite: a schedule somebody has just asked for is on.
- Every refusal carries the parser's own sentence — `when.at must be HH:MM in local time` and the
  rest. They were written for a person to read and there is no second wording of them worth
  inventing.

Ten of these in ten minutes is the brake, answered as `429 busy`. It is aimed at a client
retrying in a loop with a fresh key each time rather than at anybody filling in a form, and it is
deliberately not the dispatch brake: making a schedule is not a dispatch, and spending a tree's
tickets on it would have a form on a phone quietly stopping a root session from opening children.

New files are `0600`, and the schedules directory is created `0700` when it is not already there —
the same as task files, because a schedule carries the same first message and the same absolute
path. An existing directory's mode is left exactly as it is.

## The minute that actually fires

Clawdline checks once a minute. The first check after wake naturally sees the most recent scheduled
time: within `catch_up_hours` it runs that occurrence once; outside the window it audits `missed`
and, when enabled, sends a push. If any task from the same schedule is still non-terminal, the new
occurrence is skipped and audited instead of stacking another session on it; that occurrence is
not tried again later that day. A manual run at or after an occurrence also records it as handled,
while a manual validation before the scheduled time does not consume the upcoming occurrence.

Timing is calculated away from HTTP work, but the complete dispatch transaction returns to the
same single serial queue as external dispatches before it checks capacity, spends rate budget or
opens a tab. Scheduled tasks intentionally share the anonymous `orchestrator_max_children` bucket
(default 5) with each other and with external dispatches that provide no root identity; the
machine-wide descendant ceiling is shared too. An `over_capacity` refusal does not consume the
occurrence: the next minute retries while it remains inside the catch-up window, and only expiry
becomes the ordinary audited and notified `missed` outcome. Other dispatch refusals consume it.

The honest boundary is the app process: **if Clawdline is not open, nothing fires.** Opening it
later can catch up only while the most recent occurrence remains inside its window. This is not a
launch daemon and does not wake a powered-off Mac.

## Inspect and verify

The examples use the orchestrator token. It authenticates every route here; a paired device with
read access may also use both `GET`s, while a manual run requires the orchestrator token just like
dispatch. Creating one is the exception and goes the other way — see
[Making one without a text editor](#making-one-without-a-text-editor):

```sh
port=$(jq -r '.remote_port // 7717' ~/.config/clawdline/config.json 2>/dev/null || echo 7717)
token=$(cat ~/.config/clawdline/orchestrator-token)

curl -s "http://127.0.0.1:$port/v1/orchestrator/schedules" \
  -H "X-Clawdline-Orchestrator: $token"

curl -s "http://127.0.0.1:$port/v1/orchestrator/schedules/4d2f54ce-b4b5-4f60-8623-34011f35aa43" \
  -H "X-Clawdline-Orchestrator: $token"

curl -s -X POST \
  "http://127.0.0.1:$port/v1/orchestrator/schedules/4d2f54ce-b4b5-4f60-8623-34011f35aa43/run" \
  -H "X-Clawdline-Orchestrator: $token"
```

`GET` returns `id`, `title`, `enabled`, the next local fire as `next_fire`, an optional
`last_missed_at` for the most recent occurrence that expired outside its catch-up window, and an
optional `last_run` with `task_id`, `state`, and `at`. `last_run` can disappear after the task registry's
200-record retention limit removes the task. A bad source file appears in the same `schedules`
array with `state: "invalid"`, `file`, an `error` summary, and `error_kind`; a temporarily missing
`project_dir` uses `error_kind: "project_unavailable"` so it is distinguishable from schema errors.
No task-template field is exposed.

`GET /v1/orchestrator/schedules/:id` is the one place that does expose it. The list is a list — it
says what exists, when it next fires and how the last run went — which is the right amount for a
row and the wrong amount for the only screen where somebody can check what they just made. It adds
`file`, `when` in the file's own spelling, `close_tab`, `catch_up_hours`, `notify_on_failure`, and
the whole `task` template including `project_dir` and `instructions`. An unknown or invalid id is
`404`; a `read` device may ask, like it may ask for the list.

Manual run ignores `enabled` and the clock but refuses while any task from that schedule is
active; a successful answer has the same task and warning payload as ordinary dispatch.

A paired device (read-only is sufficient) sees the schedule list and any single schedule through
these same GET routes; a manual run remains restricted to the orchestrator token.

The local scheduler is the on-Mac form of the cloud blueprint's Phase 6: the trigger and worker
may move to another machine later, while the task protocol and lifecycle stay the same.
