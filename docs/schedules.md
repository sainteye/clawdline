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

Three routes write one, and between them a schedule now has a whole life outside a text editor:
[`POST /v1/orchestrator/schedules`](#making-one-without-a-text-editor) creates a file,
[`PATCH /v1/orchestrator/schedules/:id`](#changing-one-and-taking-one-away) rewrites one, and
[`DELETE /v1/orchestrator/schedules/:id`](#changing-one-and-taking-one-away) removes it. All three
sit behind the write gate a paired device passes, which is the plain statement worth reading
twice: **a paired phone can now change and remove work that runs later, unattended.** Until they
existed a wrong time could only be fixed at this Mac, in this file, by hand — so every mistaken
creation had to be cleaned up back at the desk.

Files are still the source of truth and hand-editing is still a first-class way to work. What the
routes do not do is guess: an edit replaces the whole file from the same fields a create takes,
`schedule_id` and `created_at` are carried across from the file being replaced rather than taken
from the request, and the task-template fields no form can show are carried too rather than
dropped. A save that moves the firing times also records when it moved them, so an edit cannot
fire an occurrence that did not exist until the edit.

The Settings app owns one convenience edit: its switch changes only the top-level `enabled`
boolean in place. Every other byte and every other field is always yours; edit those in the JSON
file itself, through `PATCH`, or through whatever Settings offers. If an unusual but valid JSON
representation cannot be edited in place, Clawdline falls back to a full JSON rewrite and records
that fallback in the audit log.

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
- `task` contains the task-template fields `assistant`, optional `model`, optional
  `reasoning_effort`, `project_dir`, optional `title`, `instructions`, `claims`, `serialize`,
  `isolation`, `isolation_base`,
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
- `when_changed_at` is the same kind of stamp one question further along, written by
  `PATCH /v1/orchestrator/schedules/:id` and by the Mac's Edit button, never named by a request.
  `created_at` answers *did this schedule exist yet*; this answers *did this occurrence exist
  yet*. Moving a `21:00` schedule to `09:00` at two in the afternoon invents an occurrence six
  hours old — inside the default catch-up window — and without a second stamp the timer cannot
  tell it from a morning the Mac slept through: measured, it opened a session within the minute
  while the save's own answer said the next run was tomorrow, and past the window it pushed that
  a run had been missed. Only a save that really moves `when.at` or `when.days` writes it; a save
  that changes a title carries the old value across untouched, so a nine o'clock that genuinely
  was missed is still missed after somebody fixes a typo at eleven. A file without it is a
  schedule whose firing times have never been edited through the app, and it behaves exactly as
  every schedule always has — including one retimed by hand in this file, which is the one path
  no stamp can cover.

A scheduled task may also use the task-secret `/notify` route to push that day's **successful
content** to the user — a daily forecast is the canonical shape. Say so in `task.instructions`;
`notify_on_failure` is only Clawdline's separate state/failure notification policy. This explicit
content delivery can be turned off by the user in Settings → Remote. While it is off, `/notify`
returns the named `409 agent_notify_disabled` refusal; the content stays in `result.json` and the
scheduled task itself still runs normally.

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

**They are not fields a request may set, and on a save they are fields a request keeps.** Those
are different statements and only the first one used to be worth making. Since a save stopped
dropping the fields no form can show, a paired phone can rewrite the title, the times and the
**first message** of a schedule that runs with `"permission_mode": "full"` and a list of `claims`
— it cannot name those fields, it cannot add them to a schedule that has none, and it cannot
raise the permission of one it is editing, but the instructions it does rewrite are the ones that
run under them. That is the sharpest thing on this page: the alternative was a save that silently
took somebody's `permission_mode` away, which is worse, but the trade is real and belongs in
writing rather than in a commit message.

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
- written by the Mac, not by the request: `schedule_id`, the filename it must match, `created_at`,
  and — on a save that moves the firing times — `when_changed_at`.
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

## Changing one, and taking one away

```sh
curl -s -X PATCH "http://127.0.0.1:$port/v1/orchestrator/schedules/4d2f54ce-…" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"title":"Publish the next post","at":"07:05","days":"daily",
       "place_id":"3f2a91c47e0b5d68","assistant":"codex",
       "instructions":"Read the publishing checklist and publish the next ready post."}'
{"ok":true,"schedule":{"id":"4d2f54ce-…","title":"Publish the next post","enabled":true,
                       "next_fire":1787905500}}

curl -s -X DELETE "http://127.0.0.1:$port/v1/orchestrator/schedules/4d2f54ce-…" \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: $(uuidgen)"
{"ok":true,"deleted":"4d2f54ce-…"}
```

`PATCH` takes the body `POST` takes, every field of it, and **rewrites the whole file** — it is a
save, not a patch of individual keys. A field the body may name and leaves out goes back to the
parser's default rather than staying as it was, so read the current schedule with
[`GET /v1/orchestrator/schedules/:id`](api.md#get-v1orchestratorschedulesid) and send it back with
the changes in it. The exceptions are the fields no form has a control for — `claims`,
`permission_mode`, `serialize`, `isolation`, `isolation_base`, `deliverables`, `kind`, `plan` and
`model` — which are read off the file being replaced and carried across, because a save that
dropped them would be an edit changing something it never put on screen. `model` is the one of
those a body may still name, and the two spellings mean different things: **no `model` key leaves
the model alone, `"model": ""` takes it off.** Both routes are behind the same three gates as the
create route, for the same reason: they are for the phone, and the orchestrator token is a local
credential a phone cannot hold.

**`schedule_id`, `created_at` and `when_changed_at` are the Mac's and are not fields a request may
carry** — naming any of them is refused as an unknown field, alongside `project_dir`. The last two
are not bookkeeping.

`created_at` is what keeps a schedule from running for an occurrence that predates it, so a save
that restamped it would make editing a `09:00` schedule at lunchtime open a session for this
morning: the same bug that field was added to stop, handed back through a different door. A
hand-written file that never had a `created_at` does not get one from being edited either — it
goes on meaning *as far back as anyone knows*.

`when_changed_at` closes the half of that bug carrying `created_at` across leaves open. A schedule
made last week is a week old however its times move, so its own age cannot say whether *this
morning's nine o'clock* is a run the Mac slept through or one this save invented sixty seconds
ago. Moving `21:00` to `09:00` at two in the afternoon used to dispatch today's nine within the
minute — while this route's own answer said tomorrow — and past the catch-up window it pushed that
a run had been missed. So a save that moves `when.at` or `when.days` stamps this instant, the
timer ignores every occurrence older than it, and **a save that moves neither carries the old
value across untouched**: a nine o'clock that really was missed is still missed after somebody
fixes the title at eleven. Unlike `created_at`, a hand-written file *does* get one the first time
a save retimes it, because the two claim different things — one would be a guess about a past this
app was not there for, the other is a fact it is watching happen. Retiming this file by hand
writes nothing and is the one path no stamp covers; it behaves as it always has.

Everything the create route refuses, an edit refuses, because both assemble the same object and
hand it to the same parser: the refusal carries that parser's own sentence, and an edit is not a
way to write a file a create would not write. The written file is then read back off disk through
the parser before the request is answered — and where a failed create deletes what it wrote, a
failed edit **puts the previous file back**. The schedule somebody already had is not a failed
save's to lose.

`PATCH` is a save and spends the same ten-in-ten-minutes ticket a create does, since a client
retrying a save in a loop writes a file per attempt. `DELETE` is deliberately not braked: it
leaves nothing behind to sweep up, and it is what somebody reaches for when they want work to
stop.

**Two different failures, and a caller can tell them apart.** `404 not_found` is *there was no
such schedule*, and it is also the answer for an id that is not an id — the file is addressed as
`<id>.json` and nothing else, which is the whole of the path handling. `500 delete_failed` is
*the file would not go*, which is a fact about this Mac that somebody has to go and look at, and
reporting it as a `404` would say the schedule is gone while it is still on disk and still firing.

`DELETE` does not read the file it removes. A file named after a UUID whose contents this app
cannot parse is exactly the file somebody most wants gone, and needing to understand it first
would be a rule with no purpose. `PATCH` is the other way round and refuses one with `404`: an
edit replaces the whole file, an invalid one has no `created_at` worth carrying, and the list does
not give an invalid row an id to address in the first place — so removing it and making a new one
is the repair. A file whose *name* is not a UUID, like `broken.json`, has no id at all and still
goes from the Finder.

**Neither route is blocked by a task that is running right now**, which is the one place they part
company with [`POST …/run`](#inspect-and-verify) and its `409 schedule_active`. That refusal is
about stacking a second session on top of a first. An edit changes a file nothing in flight will
read again — a task is materialised from the template when it is dispatched — so the occurrence a
save lands in the middle of keeps the terms it was dispatched under and the next one uses the new
file. And refusing to remove a schedule while its work is running would be refusing precisely when
somebody most wants it gone.

**Removing one is not cancelling its task.** The task keeps its own id, its own tab and its own
record; [`POST /v1/orchestrator/tasks/:id/cancel`](api.md#post-v1orchestratortasksidcancel) is
what stops it. What removal does reach is an occurrence the minute timer has already chosen and
handed to the serial queue: that dispatch looks the file up again before it opens anything, so a
schedule deleted in the second between the decision and the session opens nothing and is audited
as `orchestrator.schedule.skipped` with `why=removed`.

The audit lines are `orchestrator.schedule.updated` and `orchestrator.schedule.deleted`, written
whichever way each goes.

## Runs are conversations too

`GET /v1/orchestrator/schedules/:id` carries the retained tasks created from that schedule under
`runs`, newest first. A run names its state, timestamps, assistant, project and optional summary.
While its terminal is still present, the Web interface opens that existing Session instead of
starting a second process. After the tab has gone, a terminal run is resumable only when its
assistant transcript or rollout still exists and Clawdline has proved that conversation belongs to
that exact task; only then does the detail response include `session_id`.

Scheduled children remain absent from the ordinary project-history picker — they are still broker
plumbing there. The existing place resume route accepts this narrow second source only when the
retained task is terminal and its schedule marker, project, assistant, child conversation and
transcript ownership all agree. Resuming starts an ordinary interactive Session in the schedule's
original project. It does not create a new scheduled run and does not affect the schedule's active
occurrence arbitration.

The task registry is machine-wide and keeps its newest 200 records. When that boundary has been
reached, the detail response says `runs_may_be_truncated: true`; the Web sheet says older runs may
not be listed rather than presenting the retained tail as complete history.

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
dispatch. The three routes that write a file go the other way and take a device token instead —
see [Making one without a text editor](#making-one-without-a-text-editor) and
[Changing one, and taking one away](#changing-one-and-taking-one-away):

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
these same GET routes; a manual run remains restricted to the orchestrator token. Making, changing
and removing one need `send` and the write switch instead — `GET /v1/orchestrator/schedules/:id`
is the read half of that form, and it was built for it.

The local scheduler is the on-Mac form of the cloud blueprint's Phase 6: the trigger and worker
may move to another machine later, while the task protocol and lifecycle stay the same.
