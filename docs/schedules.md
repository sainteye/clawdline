# Scheduled tasks

A schedule is a task template Clawdline turns into an ordinary orchestrator dispatch at a local
wall-clock time. It is for work that should open a real Claude Code or Codex session, inherit the
same claims, serialization, capacity, depth, permission and trust checks, and leave the same task
record as work dispatched by a session.

Put one JSON file per schedule at:

```text
~/.config/clawdline/schedules/<schedule-id>.json
```

The filename and `schedule_id` must be the same lower-case UUID. Files are the source of truth:
there is deliberately no HTTP write route. Clawdline rereads them, rejects unknown fields, exposes
each bad file as an `invalid` row, audits and notifies once per invalid content revision, and
continues loading the valid neighbors.

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

A scheduled task may also use the task-secret `/notify` route to push that day's **successful
content** to the user — a daily forecast is the canonical shape. Say so in `task.instructions`;
`notify_on_failure` is only Clawdline's separate state/failure notification policy. This explicit
content delivery deliberately bypasses the automatic push preference switches; a separate
agent-content preference remains backlog work.

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

The examples use the orchestrator token. It authenticates both routes; a paired device with read
access may also use `GET`, while manual `POST` requires the orchestrator token just like dispatch:

```sh
port=$(jq -r '.remote_port // 7717' ~/.config/clawdline/config.json 2>/dev/null || echo 7717)
token=$(cat ~/.config/clawdline/orchestrator-token)

curl -s "http://127.0.0.1:$port/v1/orchestrator/schedules" \
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
No task-template field is exposed. Manual run ignores `enabled` and the clock but refuses while
any task from that schedule is active; a successful answer has the same task and warning payload
as ordinary dispatch.

A paired device (read-only is sufficient) sees the schedule list through this same GET route;
manual POST remains restricted to the orchestrator token.

The local scheduler is the on-Mac form of the cloud blueprint's Phase 6: the trigger and worker
may move to another machine later, while the task protocol and lifecycle stay the same.
