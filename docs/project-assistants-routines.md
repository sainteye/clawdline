# Bot routine schedules

This development-only slice follows Codync 9f67eb09084db61b3cca9c84a11bc03df9de29b7. It adds elapsed intervals, one-time timestamps and standard five-field cron with explicit IANA time zones to the existing owned Bot MCP tools. Bots remain hidden in public Release builds. The human management increment adds a panel inside each Bot chat and narrow local/Cloud routing at API level 73. It adds no Broker worker. This is source-level functionality; production Cloud deployment and installed-provider qualification remain separate acceptance steps.

An active Bot with a resumable native adapter can use `list_routines`, `save_routine`, `enable_routine`, `delete_routine` and `test_run_routine`. Save accepts a name, instruction and either legacy `interval_seconds` or 1–20 tagged `triggers` combined with OR; it returns a stable definition ID. Each trigger is `{type: "interval", seconds}`, `{type: "once", at}` (Unix milliseconds), or `{type: "cron", expression, timeZone}`. A new once timestamp must be in the future. An existing unchanged once trigger may be retained after expiry. Unchanged schedules retain their durable next-run array on name/instruction edits, including legacy interval cadence. Changing the trigger list or re-enabling a paused schedule computes fresh future occurrences. Repeating enable on an already enabled schedule does not move its deadline. Omitted edits retain the saved timeout. An omitted timeout is 3,600 seconds; the total run deadline includes setup. An explicit disabled save remains paused. Saving does not itself test-run. An explicit test run may execute a paused definition, as in Codync; scheduled wakes may not. Its persisted source cannot be changed to turn an automatic wake into a test. Webhook/event triggers and extended cron modifiers (such as last/nearest/nth weekday) are refused. Cron uses the pinned `github.com/robfig/cron/v3` v3.0.1 parser with exactly five fields and no embedded timezone or descriptors. It requires a next occurrence in that library's five-year search window; an impossible or more distant occurrence is explicitly refused. Standard day-of-month/day-of-week OR semantics, missing DST wall times and repeated DST wall times follow that parser. No calendar intent is approximated as an elapsed interval. Request keys identify durable operations independently of MCP transport IDs.

One enabled source Host owns the timer. Each interval advances from its previous deadline, skipping missed occurrences; cron computes the next wall-clock occurrence in its saved IANA zone. A once trigger clears its own next-run entry after durable admission and cannot fire again. Simultaneously due OR triggers coalesce into one pending run. A fully spent once schedule has no next timer deadline, but stays enabled so its admitted run can finish. A definition keeps at most one queued wake, including while a prior run is active. The existing per-Bot actor and process capacity serialize routines with human chat. Definition and run changes use the same SQLite compare-and-swap, command receipt and message transaction as chat.

A routine opens a fresh `routine-<run-id>` history slot. Its notice, tool replies and final result stay in the original Bot conversation with a dedicated turn. The remembered main history is separate from the current execution fence. Returning to chat advances the generation and loads the remembered main session; it never restores an old authority generation. Empty legacy history slots preserve their directory identity. Native history from a sibling slot cannot be loaded. An adapter without verified resume support refuses routine save/enable/test; its normal chat remains available. Scripted A/B/A proof does not qualify installed Codex or establish real-provider behavior.

MCP management remains bound to the current Bot, canonical Project, execution lifetime and current source policy. Models cannot choose another Bot or supply an execution fence. The four native permission answers, standing-rule revocation and disabled native automatic memory remain unchanged. Read positions remain per reader; completion does not mark a result read.

Pause or deletion prevents a queued run from starting. An already running captured run may finish. Pending wakes remain coalesced rather than becoming parallel turns. Source disable stops and retains the timer owner until shutdown is observed, seals the runtime, then performs durable cleanup. Failed session cleanup retains its resource owner. A failed terminal save retains the completed outcome and prevents another prompt; explicit verified cleanup can retry that save without executing again. A daemon crash can lose that in-memory completion, so the durable running turn becomes unknown and remains held. This is not an external exactly-once guarantee or automatic execution retry.

An interrupted routine is excluded from ordinary chat restart continuation and fresh-session fallback. Unknown execution blocks background admission. The source records uncertainty and does not interpret a timer wake as human acknowledgement. Extended calendar syntax, webhook/event triggers, progress, follow-ups, automatic retries, qualified native routine recovery, production Cloud and physical-phone acceptance remain follow-up work on the parent Board item. The bounded human manager below does not complete that whole item.

The focused HTTP fixture drives production create/send, the advertised owned HTTP MCP descriptor, the lifecycle tick, scripted ACP subprocesses and the same SQLite HTTP history. It checks main A / isolated B / main A for interval, once, cron and combined OR triggers, one result, command replay, invalid calendar input, foreign definitions and an independent invited reader. Vendor/model requests are zero. Separate fault-injection checks cover retained timer ownership and retrying a failed terminal save without another prompt. These are local entry-point checks, not production or installed-provider acceptance.

## Development downgrade boundary

Once a tagged calendar schedule has been saved, returning to a daemon older than this calendar implementation is unsupported. Those binaries expect a positive legacy interval and may divide by zero or treat a spent once schedule as due. Before any such downgrade, use the calendar-capable daemon to pause and delete every tagged schedule, resolve its queued or active runs, and verify cleanup. Preserve unknown outcomes rather than clearing them to permit the downgrade. Installing an older binary alone does not migrate the saved schedules.

## Exhausted next occurrence

The five-year cron search window also applies when advancing an existing schedule. A saved valid schedule can later exceed it: after February 29, 2096, the next leap day is in 2104, because 2100 is not a leap year. When advancement returns the typed `pa_invalid` / `routine_cron_future` failure, only that definition is paused. Its original triggers, per-trigger deadlines and earliest due timestamp remain intact, including OR entries examined before the failure. It is not deleted, re-anchored, approximated or repeatedly enqueued. Re-enabling still requires computing a valid future occurrence; changing the schedule remains an explicit management operation.

The pause, failed command receipt, terminal routine run, unstarted failed turn and notice/turn events commit atomically through the existing store. The turn retains the parser's failure code and field and links the failed run to its definition. The notice contains the saved routine name; no native prompt or execution binding is created for this scheduling failure. This reuses the existing failure representation rather than adding a schema or contract. Healthy definitions and their queued work continue in the same tick. A storage failure still halts the tick and rolls back the failed definition's whole transaction; the next attempt retains its due intent. Other advancement errors still propagate. Unknown execution continues to prohibit another prompt, while retaining one coalesced healthy wake.

This follows pinned Codync `host/src/routines/runner.rs` (`schedule_due`): an advancement error disables only its routine, records a failure and continues the definition loop; pending wakes remain coalesced. Codync stores `last_error` on its routine; Clawdline instead uses its existing terminal run/turn/event/receipt machinery. Codync `schedule.rs` retains IANA zones, advances intervals from their saved deadline and delegates cron to Rust `croner`; Clawdline keeps the pinned Go `robfig/cron/v3` parser and its five-year search limit. The leap-century failure is therefore a local parser boundary, not a claim that Codync has the same horizon. Clawdline preserves the entire pre-error deadline array rather than persisting partial OR advancement. Codync `check.rs` checks schedules before save and permits retaining an existing past once trigger; these admission rules are unchanged here.

The focused real Engine/SQLite fixture reproduces the former starvation with that valid saved leap-day cron, verifies typed failure/events and one failed receipt, preserves a partially examined OR schedule, completes existing healthy queued once/interval work, and resumes main A after routine B. Separate cases retain an unknown barrier and inject a receipt-write failure to prove rollback and safe retry. These scripted local checks make no vendor/model requests and do not qualify installed agents or production deployment. The human projection renders spent trigger deadlines as empty strings and shows that no future occurrence remains. Installed-provider acceptance remains outside this slice.


## Human management in Bot chat (API 73)

More actions → Routines opens the existing Bot's manager without sending chat text or starting a native session. A person can create, edit, pause, enable and delete definitions, explicitly request a real test run, and read that definition's persisted run status, final assistant result and typed failure. Saving never requests a test. The form supports elapsed intervals, one-time dates and five-field cron with an explicit IANA zone, with up to the existing 20 OR triggers. The one-time input describes this device's local date/time; cron never guesses a time zone. Unedited triggers retain their exact values and order, including one-time milliseconds and a past unchanged one-shot on a name-only edit. Name/instruction edits retain original deadlines. Re-enabling keeps the existing schedule rules described above.

| Method | Path below `/v1/project-assistants/{bot}` | Purpose |
| --- | --- | --- |
| GET | `/routines` | One bounded page of nondeleted definitions |
| GET | `/routines/{id}/runs` | One bounded page of actual runs, including deleted-definition history |
| POST | `/routines` | Save a definition, without execution |
| POST | `/routines/{id}/enabled` | Pause or enable |
| DELETE | `/routines/{id}` | Delete the definition |
| POST | `/routines/{id}/runs` | Explicitly enqueue a test |

`api/v1/project-assistant-routines.schema.json` owns only `ProjectAssistantRoutine*` wire types. Human writes use the existing `edit` receipt namespace with a distinct action, complete payload, host-issued fence, authenticated principal and expected routine revision. The native MCP entry still requires its actual active turn and actor lock. Both use the same pure mutation builder; the human service never invents a native caller or turn. Capability inspection for save/enable/test requires the same supported adapter, enforced read-only boundary, isolated memory and verified resume capability. An adapter without verified resume is refused; the separate Codex native-resume qualification does not by itself establish native routine execution. A Bot with no initial main binding refuses test admission with `resume_unsupported` / `routine_main_session`; opening or saving does not create a main chat merely to bypass that refusal.

Human reads use scoped SQL over definitions/runs and the selected run's stored final assistant message/failure, without an aggregate chat export. They reuse the existing 100-row page ceiling, Cloud plaintext byte ceiling, signed keyset cursor and source incarnation. Cursors bind the authenticated reader/grant/source, full canonical Project, Bot, conversation and collection/definition. Revocation and source changes discard late serialized bytes. Installed disabled history stays readable; writes stay forbidden. Missing tables, invalid inspection and oversized pages are typed failures, never authoritative empty history.

Metadata reads, pause and delete do not allocate an actor or inspect a native provider. Writes resolve the canonical Project and current authority again before their compare-and-swap. Concurrent native work may cause a typed stale revision/generation refusal, preserving the form. The manager refreshes its host-issued fence before a new explicit command, because a completed isolated run changes the execution generation. Updates retain the form's expected routine revision, so that refresh cannot overwrite another edit. A queued scheduled wake is prevented by pause/delete under the existing captured-run checks; an already running captured task may finish. A paused definition with an uncancelled scheduled wake refuses a test rather than claiming that the scheduled wake became an explicit test. A later explicit command can be submitted after cancellation is observed.

The exact pending command is stored in this tab before transport. An unreadable, unsupported or foreign-scope record remains untouched and disables all routine writes while history stays readable. Storage-access failure has the same barrier. The explicit recovery button says that it confirms or finishes the original action: if the source never received it, this action can actually execute it under the same command identity. An unknown response keeps that command held across panel reopening/reload; the only offered recovery explicitly confirms or finishes the same payload and idempotency key. There is no automatic test replay or replacement command. The SourceClient validates the returned receipt's command identity and treats malformed/unknown receipts as unknown outcomes. Storage refusal cannot authorize a send. Server replay returns the original receipt/resource; changed payload under the same key conflicts. Form edits survive typed refusals. The source validates exact field spelling, required/non-null fields, duplicate keys and complete scope. Trigger wire objects carry all fields; fields unused by their selected type are zero/empty and domain validation rejects conflicting fields.

All six Cloud words route through the captured SourceClient and paired principal's existing read/write ceilings. They do not use a selected-machine singleton or a phone-specific read-only mode. Sources below API 73 are honestly unsupported. Local transport and projection fixtures establish the code path, not deployment of the Cloud console/API/relay or acceptance on a physical phone.

The focused human TCP fixture exercises the production Server, Host, service and SQLite with a scripted provider: create/edit, unchanged schedule values, stale revision, save/test replay, actual result/history, pause/enable/delete, read-only/disabled sources, revocation during admission and final publication, and main A / isolated B / main A. The browser harness mounts production Chat and SourceClient against that same TCP source, operates More actions through the actual Clawdline browser CLI, and captures desktop English and mobile Taiwan Traditional Chinese create/edit/test/result/pause/enable/delete/history. It does not install or prompt a native agent. Its two explicit browser tests produce two scripted routine sends; separate main-before/main-after sends prove isolation. External vendor/model/native requests are zero. The run panel refresh is explicit; live run updates, webhook/event delivery, native progress qualification, follow-ups, automatic retries, native recovery, and interaction with future group/Bot exchange features remain unqualified.

### Pinned upstream comparison

The inspected source is Codync `9f67eb09084db61b3cca9c84a11bc03df9de29b7`:

- `host/src/routines/mod.rs:233` scopes listings to one Bot; `:247` shares durable save semantics; `:300` re-enables schedules; `:320` deletes and cancels queued work.
- `host/src/routines/check.rs:18` validates the body; `:48` retains existing unchanged past once triggers.
- `host/src/routines/mcp.rs:5` separates list/save/enable/delete/test tools; `:36` distinguishes saving from a real immediate run and warns against claiming unverified integrations.
- `host/src/routines/schedule.rs:26` retains tagged interval/once/cron values; `:49` validates explicit zones and future once timestamps; `:101` preserves elapsed cadence.
- `host/src/routines/runner.rs:72` checks whether queued work may still begin; `:242` advances due schedules and isolates a scheduling failure.
- `apps/desktop/src/renderer/views/routines/RoutinesView.tsx:41` reads actual definitions/runs and discards another Bot's late reply; `:78` keeps actions distinct from reads. `RoutineEditorView.tsx:42` preserves the original schedule until replaced; `:97` saves only after schedule checks; `:261` confirms deletion.

Clawdline retains its stricter human grant/fence/revision/receipt boundaries and registered read budgets rather than copying upstream's raw listing limit, automatic retries, webhook credentials or provider behavior. These are inspected source correspondences, not measurements of causal benefit from the workflow.


## Human manager recovery verification

Root reproduced the production component opening a replacement form after a
malformed saved action was read. The same real Chrome/HTTP fixture passes after
retaining that record and disabling routine mutations, including after panel
remount. A pure reader also rejects unsupported and foreign scope records
without writing or deleting their bytes. Reader/account/grant changes remount
the panel and cancel its outstanding requests. Normal desktop English and
mobile Taiwan Chinese operations still create/edit/test/result/pause/enable/
delete/read history. A lost save acknowledgement survives an actual origin
reload and is resolved only through the explicitly labelled original action.
The acceptance uses two explicit routine tests and main A before/after: four
scripted provider sends, zero native/vendor/model/API requests. It does not
prove an installed native adapter or production Cloud.


## Progress between isolated runs

The execution-bound MCP now advertises `save_progress` and `read_progress`.
A note belongs to exactly one nondeleted routine in the current Bot, complete
Project and conversation. `save_progress` requires an idempotency key and the
entire `content` string; an explicitly empty string clears it. Missing or null
content is refused. A repeated command returns its original receipt, while a
changed payload under the same key conflicts. Saving a definition preserves its
note and saving a note preserves the definition's schedule, instruction and
native history. Neither operation starts a test run. Human routine listings
expose the saved `progress` through their existing scoped read path.

This follows CodyNC `9f67eb09084db61b3cca9c84a11bc03df9de29b7`,
`host/src/routines/progress.rs`, `kept.rs` and
`host/crates/memory/src/{tokens,check}.rs`: replace the whole note, clear on empty,
refuse rather than truncate above 2,000 estimated tokens, and reject hidden
characters, prompt-carrying links, planted instruction phrases and secret-like
content. The estimate counts one token per CJK rune and one per four other
runes, rounded up. These are deterministic checks, not proof that accepted text
is trustworthy. A run receives the note as a JSON-quoted string explicitly
labelled untrusted data, followed by the host's instructions for leaving its
next stopping point. It is routine-local state, separate from long-term memory.

Clawdline additionally records the memory revision that produced each note.
A forget operation conservatively clears every routine note for that Bot in the
same SQLite transaction as its memory tombstones and receipt, because the
compacted note has no per-source attribution. It increments changed definition
versions so an old human editor cannot restore the note. A failed transaction
retains the entire previous state. An execution from before the forget boundary
cannot save a replacement note; a stale note cannot be imported or committed
into a newer memory state. The send boundary still uses `WithCurrent`, source
revocation, the active execution fence and the existing unknown-effect barrier.

The focused scripted HTTP/ACP/MCP path checks receipt replay and conflict,
foreign routine refusal, rejected planted text, explicit clearing, replacement,
actual prompt data, HTTP readback, and main A / routine B / main A isolation.
SQLite checks cover failed-forget rollback, receipt replay without resurrection,
restart and archive transfer. These local fixtures make zero vendor/model
requests; installed-native and production Cloud qualification remain separate.
Webhook/event triggers, follow-ups and execution retries are still pending.
