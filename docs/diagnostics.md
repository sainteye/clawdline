# Reading a browser-side defect from a file

Some faults only exist on the phone. A home-screen web app has no address bar, no console and no
inspector, and the person holding it is not going to read a stack trace to you. This document is
the standing arrangement for that: **the app writes its own diagnostic to a fixed path on this Mac,
and whoever is debugging opens the file.**

It replaces asking somebody to paste a report. On 2026-09-06 that failed in the only way that
matters — the report was long enough that pasting it into a conversation hung the program, and
Universal Clipboard had not synced it either — and the fallback was a person reading a string off a
screen out loud. The rule that came out of it: **the most you may ask of the person holding the
phone is one press.**

## The three lines

1. **Add an observation point** where you cannot see. `Diagnostics.note("<your.event>", { … })` from
   `Resources/web/app/js/core/layout-diagnostics.js`. Names are yours; nothing on the Mac has to
   learn them.
2. **Ask for one press.** Settings → the small version line at the bottom → **five taps inside two
   seconds** → the `LAYOUT DEBUG` button appears at the bottom left → open it → **Send to Mac**.
   On the hosted Cloud PWA that line shows the immutable web build until the Mac version arrives;
   an absent or late machine snapshot therefore cannot remove the door needed to diagnose it.
   The panel then says, in words, where the file went.
3. **Read the file.**

```
~/Library/Logs/Clawdline/diagnostics/report.json
```

That path is a constant, and that is the point of the whole feature: it is not a UUID, not
"whatever is newest in the directory", and you do not have to ask which one. The press before the
last one is kept beside it, so a second press does not destroy the reading you were about to open:

```
~/Library/Logs/Clawdline/diagnostics/previous.json
```

Two files with fixed names is also the whole of "it cannot grow without bound". Nothing else is
ever written there.

**A fixed path has one failure mode, so check for it.** The file is always there once anybody has
ever pressed the button, which means the reading you open may be from last Tuesday. `written_at` in
the envelope is UTC and is how you know it is the press you asked for; if it is older than your
request, the press did not happen or it went to a different Mac — say so rather than reasoning about
the wrong afternoon.

**Open the panel after the fault, not before.** The report is a snapshot of the page's bounded
trace; opening it first only spends that window on the gesture used to reach the panel.

## The gesture, said out loud

Five presses on the version line is deliberately unguessable, and the reason is in
`Diagnostics.reveal`: `?debug=layout` needs an address bar, and the one device this exists for does
not have one. It stays hidden. What it must not be is *unwritten*, which is why it is here, in a
document rather than in somebody's memory.

## What the file looks like

JSON, always, with the page's own document untouched inside an envelope:

```json
{
  "clawdline_diagnostic_report": 1,
  "written_at": "2026-09-06T09:12:33Z",
  "written_by": "<the paired device that sent it>",
  "received_bytes": 18204,
  "completeness": { "…": "hoisted out of the report below, so you see it first" },
  "report": { "completeness": {}, "current": {}, "savedIncidents": [], "lastDetail": {}, "currentTrace": [] }
}
```

`report` is exactly what `window.__clawdlineDiagnostics.report()` returns. Nothing in the route
reads it, validates it, or knows what any of the event names mean — a body that is not a JSON
object is refused and everything else is stored as sent. That is what makes the mechanism outlive
the defect it was built during: today's events are page, route and layout facts; next month's will be
something else, and neither the route nor this document has to change for it.

## It says how complete it is

The page keeps a bounded trace, so the report states what that bound discarded:

```json
"completeness": {
  "trace":      { "kept": 80, "limit": 80, "dropped": 46, "droppedThrough": 8123, "from": 8130, "to": 20455 },
  "incidents":  { "kept": 3, "limit": 5 },
  "whole":      false
}
```

`trace.dropped` is how many entries the ring buffer threw away before the ones being read, and
`droppedThrough` is the page clock reading where that lost prefix ended. `whole` is true when
nothing was dropped. It is not a claim that the fault is visible in the file; it says only that
the page's bounded trace did not truncate itself.

Notification registration records finite boundaries as `push.<stage>.begin`, `.end`, and
`.failure`: `permission`, `worker.ready` (with `worker.register`/`worker.activate` when needed),
`key`, `browser.subscribe`, and `server.subscribe`. Failure data contains only its typed code; the
subscription endpoint and key are never recorded.

A report carrying no `completeness` block — from an older build — is wrapped as
`{"stated": false, …}` rather than rendered as an empty report.

Older reports can contain a `sources` array for the retired service-worker notification trace.
Current builds no longer create those rows: Declarative Web Push bypasses the missing
`notificationclick` event, so keeping a Cache Storage recorder and routing fallback for that
event would preserve the mechanism that caused old Sessions to reopen.

## The route

`POST /v1/diagnostics/report`, body = the report.

It goes to **whatever server is serving the page** — this Mac, directly on the local network or
through its own tunnel. A page served by anything else answers something else, and the panel prints
that refusal rather than a tick: what you must never get here is a green mark and no file.

**On the hosted console it is a Cloud command instead**, because `app.clawdline.com` has no such
route and the POST reached nothing. The panel hands the report to the Cloud transport, which sends
`diagnostics.report` to the Mac (`docs/cloud-error-transparency.md` §11.5) with the browser's recent
command trail attached as `cloud_trail` — sequences, steps and codes, no contents. It checks the
serialized size first against the smaller of `DiagnosticReport.maxBytes` and the relay's `ctl`
ceiling (16 MiB since `clawdline-cloud` D17), and over it the panel says `browser · report_too_large`
without sending anything. The Mac writes the same two files with the envelope's sender as
`written_by` and answers with the receipt below; the panel prints that `path`, or the refusal as
`layer · code · ref`.

The exact command is
`{"type":"diagnostics.report","session":"__clawdline_machine__","request":<uuid>,"report":<object>}`.
The Mac answers on `action:<request>` by calling the same `DiagnosticReport.save`, so the size
limit, rotation, success body and refusal codes below remain the route's own. Two differences are
deliberate: `written_by` is the paired device id, and a refusal also carries
`layer: "mac_route"` plus the envelope sequence. The command is read-level and is not refused just
because remote writes are off. An older Mac that has never advertised `cloud_status.v >= 1` is not
sent this command; the page immediately says that the versions do not understand each other.

**The Cloud status sheet is the other half.** Settings has a "Cloud status" row in Cloud mode, and
every failure line on the page opens it at its `ref`: this browser's steps for each recent command,
each Mac's `cloud.status` steps and refusal for the same ref, the clock guard, token expiry, key id,
drop counts, key-id drift and the other-tab hint. The same `ref` is what `cloud-status.json` on the
Mac is indexed by. See [`cloud.md`](cloud.md#what-is-wired-in-the-browser).

**Authentication is the paired device, at read level.** Two decisions, both deliberate:

* **Not the machine-level orchestrator token.** This is one browser handing over what it recorded
  about *itself*, and the credential that identifies that browser is the one it is already holding
  — the same one `/v1/push/subscribe` and `/v1/push/test` use. A machine token would let anything
  on this Mac write into the file an agent is told to trust.
* **Not behind the remote-write gate.** That gate exists for typing into somebody's session, it is
  off by default for a phone, and the person who needs this most is precisely the person on a phone
  with it off. What the route may do is bounded by construction instead: two fixed file names, a
  size limit, and nothing in the request that could name a path.

A successful answer is `{"ok": true, "path": …, "previous": …, "bytes": …, "limit": …,
"completeness_stated": …}`, and the panel prints the `path` it was given rather than one it composed
— a page that prints its own idea of the path will name a file nobody wrote the day either end
moves. **`bytes` in that answer is the file's size; `received_bytes` inside the file is the body
that arrived.** They differ by the envelope, and they are two names on purpose.

Every call is written to the audit log as `diagnostics.report`, refusals included.

**Why `~/Library/Logs` and not `~/.config/clawdline`.** That directory is settings and secrets —
`remote.json` at mode `0600`, the orchestrator token — and a report is neither. Logs is where macOS
keeps "what happened", it is already where [`docs/remote.md`](remote.md) sends somebody asking what
this app did, and mixing an inbox into a configuration store makes *what I set* and *what happened*
the same place. The file itself is `0600`: it carries session ids and project paths.

### Refusals, by name

| code | status | when |
|---|---|---|
| `unauthorized` | 401 | no paired device |
| `empty_report` | 400 | no body |
| `report_not_json` | 400 | a body that will not parse, or parses to something that is not an object |
| `report_too_large` | 413 | over `DiagnosticReport.maxBytes` — 2 MiB, well under the server's own 20 MiB body limit so that this refusal is the one you get |
| `report_write_failed` | 500 | the directory or the file could not be written, or fewer bytes came back than went in |

**Nothing is ever truncated to fit.** The failure this whole feature repairs was a reading that
looked like a reading and was not one; a report cut down to the limit would be exactly that again,
one layer deeper. Over the limit is a refusal with both numbers in it, and the last good report
stays where it is.

## The Cloud status file

A second fixed file sits beside the report, and nobody has to press anything for it:

```
~/Library/Logs/Clawdline/diagnostics/cloud-status.json
```

It is this Mac's own account of its Clawdline Cloud link — the snapshot described in
[`cloud.md`](cloud.md#what-is-wired-on-the-mac): transport state and token expiry, the command
clock guard's state, reason and countdown, the key id, whether the paired-device roster could be
read, drop counts by code since `counting_since`, the 20 newest dropped envelopes, the 50 newest
commands with each Mac-side step (`accepted_at_ms`, `executed_at_ms`, `outcome`, `delivered_at_ms`,
`undeliverable`, `refusal`), the 20 newest notices and the reply-side counts. It is rewritten when
something changes, at most once every two seconds, as mode `0600`, and only while a Cloud bridge is
running in this process; it is not written during tests or before the first bridge starts. A phone
reads the same snapshot with the Cloud read `cloud.status`.

**It is the other half of every Cloud refusal on screen.** A refusal names a layer, a code and a
reference `sender·seq`; search this file for that `seq` under the same `sender` to see how far the
command got. The Mac log carries the same reference on one line per refusal:
`cloud: refusal layer=… code=… sender=… seq=… request=… … reply=…`.

**Check `generated_at` first**, for the same reason as `written_at` above: a file left by a bridge
that has since stopped keeps its last reading. `counting_since` is when this process started
counting; the counts do not reset when the connection does.

It holds codes, device ids, sequences, request ids, key ids, counts and times. It never holds a
command body, a prompt, transcript text, a title or a filesystem path.

## The viewer events file

The third fixed file is the phone's side of the Cloud link, and nobody presses anything for it
either:

```
~/Library/Logs/Clawdline/diagnostics/cloud-viewer-events.jsonl
~/Library/Logs/Clawdline/diagnostics/cloud-viewer-events.1.jsonl   (the previous 4 MiB, after rotation)
```

The hosted console keeps a row whenever an envelope it receives fails to open or apply, and whenever
the "This browser cannot decrypt Sessions" door rises or falls. The rows wait in the page's
`localStorage` (bounded, surviving a reload or a killed Home Screen app) and go to the paired Mac as
the Cloud command `diagnostics.events` about five seconds after a burst and again after the next
`ready`. The Mac appends **one line per batch** here, mode `0600`, and writes one counts-only line to
`Clawdline.log` (`cloud viewer events: appended rows=… dropped_rate_limited=… …`, or `duplicate rows=…`
for a batch it already holds). A row leaves the phone only when this Mac's receipt names its batch.

**A batch is appended once.** The command ledger answers a resend of the same request for 24 hours;
past that, the Mac still recognises the `batch_id` from `cloud-viewer-events.batches` beside the JSONL
— the last 2,048 `<device>\t<batch_id>` pairs appended, a file of its own, so the record survives a
restart and the rotation to `.1`. A resend it recognises answers `duplicate: true` with the batch's
rows and appends nothing. The id is written after the line is synced, so a crash between the two can
still append a batch twice; it cannot lose one. The target is a machine this browser is **paired
with** — its `orch/<machine>` snapshot opened through the browser's pairing for that machine, exact or
bound from a legacy pin — that is **a Mac** (its descriptor names no other platform) and **takes the
command** (it published `cloud_status.v >= 1`). It is never "the only machine": a Linux executor on the
same account, paired or not, is excluded by platform and by capability, and does not stop delivery.
The batch is sealed with that Mac's pairing, like every other command. Wire format:
[`cloud-error-transparency.md` §11.9](cloud-error-transparency.md#119-cloud-指令-diagnosticsevents).

**Each line states its own completeness.** `device` is the envelope's authenticated sender; then
`web_build`, `tab` (the tab that sealed the batch), `batch_id`, `written_at`, `row_count` and
`completeness`. Every kept, overflowed or unflushed row consumed one `n`, so
`rows + dropped_overflow + dropped_unflushed = n_to - n_from + 1`, and the Mac writes whether that held
as `completeness_consistent`. `dropped_rate_limited` counts rows a burst limit refused (three per key,
thirty in all, per minute); `completeness.rate_limited[]` names each key, its count and `sample_n`,
the kept row that carries that key's metadata. `dropped_unflushed` counts rows a page recorded and was
killed before writing (below); rate-limited rows lost the same way are under the key `(unflushed)`.
`dropped_refused` counts the rows of an earlier batch this Mac refused for its content: that batch is
gone, a `viewer_events.batch.refused` row in this one names it, and its range is not this batch's.
`storage_errors` counts failed `localStorage` writes on a page that lived to report them.

**What the page can lose, and what it counts.** The page keeps its state in memory and writes it to
`localStorage` at most once a second, when it is hidden and at once for anything delivery does; a
receive failure itself writes only a few bytes (`:journal`, the next `n` and the running
rate-limited total). A page killed before a write loses the rows since the last one, and the next page
counts them from the journal as `dropped_unflushed`. Tabs of one device share the stored state, and
with Web Locks only the tab holding its lock writes it or delivers; another tab keeps its rows in
memory and adds them, renumbered, when the lock reaches it — so a second tab closed first loses its
rows, uncounted. A browser without Web Locks writes from every tab, and two tabs
writing within the same second can overwrite each other's rows, uncounted. When a tab takes the lock
it also removes this account's logs for any other device id no tab holds — a replaced device's rows
and unsent batch — and records `viewer_events.log.pruned` with what they held.

**A row is `{n, at_ms, event, tab, data}`,** `tab` naming the page that recorded it, and `data` is
flat. Adding an event changes neither the route nor this file's writer. **An exception's words are kept
only when this code base wrote them:** `error_message` and `cause_message` hold a sentence from the
fixed list `OWN_ERROR_MESSAGES` in `cloud-viewer-events.js` and are otherwise `null`, because an engine
writes the values involved into its own messages (V8's `Cannot create property 'x' on string '…'`)
and a Mac or relay refusal carries a server's words. `error_class` says what was withheld: `own` (kept),
`typed` (a failure code whose words are not on the list), `javascript` (a built-in error type),
`platform` (a `DOMException` or other named web-platform error), `other` or `not_an_error`; a name not
shaped like an error type's is `(unlisted)`. The events so far:

| event | `data` |
|---|---|
| `cloud.receive.failed` | `code` the transport threw (`null` for a raw error); `stage`, set before each step, so it is where the exception came from (the steps are listed below); `error_name`/`error_class`/`error_message`, the original exception rather than `unreadable_envelope`'s sentence; `cause_name`/`cause_message`, what signature verification swallowed; envelope metadata `channel_kind`, `channel_prefix`, `machine`, `session`, `sender`, `key_id`, `seq`, `ts`, `class`, `realign`, `ct_bytes`, `field_names`; `browser_key_id`; `socket_ready`, `ms_since_ready`, `opens_since_ready_sender`, `opens_since_ready_total`; the pairing — `routed_machine`, `pairing_found` (`null` when no lookup ran), `pairing_found_before` (what this viewer's previous lookup for that machine answered), `pairing_legacy`, `pairing_key_id`, `pairing_sender`, `pairing_source` (`memory` or `store`), `pairing_lookup_ms`, `machines_with_pairing`, `machines_without_pairing` (ids only, as this viewer has looked them up); `sender_key_found`, `sender_key_source` (`pairing`, `memory` or `store`), `sender_key_lookup_ms`, `senders_with_keys`, `senders_without_keys`; `target_machine`, `target_sender`, `sender_is_target` (where delivery would go now); `visibility`, `ms_since_visibility_change`, `ms_since_page_load`, `online`, `web_build`. Never a nonce, ciphertext, signature, plaintext or key: of a pairing only its kind, key id and sender id |
| `cloud.frame.failed` | a relay frame that failed outside an envelope: `frame_type`, `code`, `layer`, `error_name`, `error_class`, `error_message`, `socket_ready`, `ms_since_ready` |
| `cloud.door.raised` | `kind`, `code`, `error_name`, `error_class`, `error_message`, `cause_n` (the `cloud.receive.failed` row behind it; `cause_rate_limited`/`cause_key` when that row was not kept), `invitation` |
| `cloud.door.hidden` | `by` (`sessions` or `connected`), `raised_n`, `ms_raised`, `failures_while_raised`, and the lowering envelope's `channel_kind`, `machine`, `sender`, `seq`, `realign`, `authoritative`, `self_healed` |
| `cloud.receive.binding_unsaved` | a legacy binding the signature and decrypt proved but the key store could not write: `machine`, `sender`, `key_id`, `error_name`, `error_class`, `error_message`. The envelope applied; the binding is held in memory for this client |
| `viewer_events.batch.refused` | a batch this Mac refused for its content, dropped: `code`, `layer`, `status`, `refusal_path` (the field path the Mac named, when it named one), `machine`, `mac_build`, `web_build`, `batch_id`, `rows`, `attempts`, `created_at_ms`, and the refused batch's own `n_from`, `n_to` and dropped counts |
| `viewer_events.log.pruned` | another device's log for this account, removed: `device`, `rows`, `outbox_rows`, `rate_limited`, `bytes` |

**The steps, in the order an envelope takes them.** Each row's `stage` is the step that threw, and
the door column is what `cloudSessionAccessProblem` makes of that step's code: *encryption* is "This
browser cannot decrypt Sessions", *pairing* is "Pair this browser with the selected machine".

| `stage` | what runs | codes it can throw | door |
|---|---|---|---|
| `channel_parse` | `ch` is parsed and its machine decoded | a raw `TypeError` | none |
| `machine_pairing_lookup` | this browser's pairing for that machine, from memory or the store | `machine_key_incomplete`; `extractable_key`; a raw store error | pairing; encryption; none |
| `pairing_key_id` | a pairing's key id against the envelope's | `unknown_key` | encryption |
| `sender_key_lookup` | the pairing's sender key, or with no pairing the legacy sender pin | `machine_not_paired` (a routed machine with no pairing and no pin for its sender); `unknown_sender` (no pin, on a channel that names no machine, or a client without machine scoping); a raw store error | none; encryption; none |
| `master_key_lookup` | the pairing's content key, or with no pairing the account key for `key_id` | `unknown_key`, `extractable_key` | encryption |
| `validate`, `signature_verify`, `decrypt` | the envelope's shape, its signature, AES-GCM | `unreadable_envelope` | encryption |
| `paired_sender` | the decrypted channel's clear `sender` against the pairing's | `unknown_sender` | encryption |
| `legacy_binding` | a machine with no pairing is bound to the pin that just opened it | `machine_key_incomplete`, `machine_pairing_required` | pairing |
| `sequence` | replay protection | `replay` | none |
| `payload`, `apply` | the plaintext as JSON, then the snapshot | `bad_payload`; whatever a handler throws | none |

A binding that succeeds leaves no row; the next envelope from that machine opens through it from
memory. A binding whose store write **fails** after the signature and decrypt proved it is held in
memory, the envelope applies, and `cloud.receive.binding_unsaved` says so; a later page binds again. A
pairing store that **rejects** throws a raw error with `code: null`: the envelope is lost, but no door
rises. A store that answers **nothing** has different effects at each step.

**`machine_not_paired` is one machine's state, never the account's door.** The relay delivers every
machine's `s/` and `orch/` to every viewer of the account, so an envelope from a machine this browser
holds no pairing for, signed by a sender it holds no key for, says nothing about this browser's keys.
The client refuses it with `machine_not_paired` (`detail.machine`), keeps `machineAccess(machine)` as
`{state: "not_paired", sender, since_ms, last_ms, envelopes}` for a surface that offers Pair a Browser
for a selected machine, and keeps that state across a token renewal until a pairing for the machine
is found. A pairing, a legacy binding or a pin for the sender keeps today's codes, so real key drift —
including a legacy browser whose account key drifted before any binding — still raises the decrypt
door. The store's "none" for a machine this viewer never found paired is remembered for the client's
life, so the next envelope does not open IndexedDB again; a Pair a Browser flow completed in the page
clears it, and a machine found paired once is asked every time.

**Reading an intermittent decrypt door.** These are what each open hypothesis would look like; none
of them is established:

| hypothesis | the rows that would say so |
|---|---|
| H1a — a second machine on the account that this browser is **not paired with** (the relay sends every viewer each machine's `s/` and `orch/`) | `stage: sender_key_lookup`, `code: machine_not_paired`, `pairing_found: false`, `sender_key_found: false`, `routed_machine` in `machines_without_pairing` and not `target_machine`, `sender_is_target: false`, and no door row. Before this client scoped machines it was `code: unknown_sender` with a `cloud.door.raised` row, then `cloud.door.hidden` `by: sessions` at the next Session snapshot from the Mac |
| H1b — a machine this browser **is** paired with, whose pairing is incomplete or under another key id | incomplete: `stage: machine_pairing_lookup`, `code: machine_key_incomplete`, `pairing_found: true` (the *pairing* door, not the decrypt door); another key id: `stage: pairing_key_id`, `code: unknown_key`, `pairing_key_id` unlike `key_id`; the paired sender changed: `stage: paired_sender`, `code: unknown_sender`, `pairing_sender` unlike `sender`; the same key id with another content key: `stage: decrypt`, `error_name: OperationError`, `pairing_found: true` |
| H2 — a realign replays envelopes sealed under an older key or sender | `realign: true` with a small `ms_since_ready`, `stage: pairing_key_id` (a pairing) or `master_key_lookup` (a legacy pin) with a `key_id` unlike `pairing_key_id`/`browser_key_id`, or `stage: decrypt`/`signature_verify`, or `code: replay`; old `ts`; then `cloud.door.hidden` `by: sessions` soon after |
| H3 — the key store fails on resume | a rejection: `stage: machine_pairing_lookup`, `pairing_source: store`, `code: null`, `error_name` such as `UnknownError` (no door). An empty answer for a paired machine with no legacy pin standing in: `stage: sender_key_lookup`, `code: machine_not_paired` (no door) with `pairing_found: false` — and `pairing_found_before: true` when the same page's previous client had found it; that machine is asked again at every envelope. A legacy browser's pin store is asked only until the machine is bound (`stage: sender_key_lookup`, `sender_key_source: store`), and the binding's own write can fail at `stage: legacy_binding`. Each with a small `ms_since_visibility_change`. A pairing the store returned is kept for the client's life, so for a paired machine this follows a reload or a token renewal |
| H4 — an envelope that was never valid | `stage: validate`, `error_name: TypeError` with the validator's sentence, `field_names`; or `stage: channel_parse` with `channel_kind: unparsed` (a raw `TypeError`, no door) |

**What it cannot tell you.** A page whose account key is wrong cannot seal a command this Mac can
open, so its rows wait on the phone until the keys are repaired. A device without `send_prompt`
cannot publish at all (`cloud_read_needs_send_prompt`). A browser that has authenticated two capable
Macs refuses to guess (`cloud_machine_ambiguous`). A machine that has never published `cloud_status` is
not asked (`cloud_feature_unavailable` when it might be a Mac); one that answers `unknown_command` is
not asked again until its build, the page's build or the day changes — and a block recorded against one
machine does not hold the rows from another that later qualifies. A batch refused for its content
(`viewer_events_malformed`, `viewer_events_too_large`, `viewer_events_empty`, `malformed_command`, or any
final refusal from the Mac's route) is not held: no resend of those bytes could succeed, so it is
dropped, counted in the next batch as `dropped_refused` and named by a `viewer_events.batch.refused`
row, and batches refused one after another back off like attempts of one batch. The page cuts its rows
at 2,048 UTF-8 bytes and 48 fields and its batch at 240 KiB, and never ends a string inside a surrogate
pair, so a batch it seals is one this Mac accepts; `Tests/cloud-viewer-events-contract-batches.json` is
such a batch, sealed by the real page code and validated by the real Mac code. A
target whose pairing is gone by the time of the send is refused locally by `_outboundMachinePairing`
(`machine_pairing_required`) and tried again under the backoff. In every one of those the rows stay
on the phone, except for a content refusal. `machines_with_pairing` lists what this viewer has looked up, not every key in the
browser's store. Sends are at
least a minute apart, doubling per failed attempt to thirty minutes, at most 48 a day, from one tab
of a device at a time.

## What it cannot tell you

The file is the page's account of itself. If the page never ran, never reached `Diagnostics.bind`,
or crashed before the press, there is nothing to send and the panel will say it could not reach the
Mac. It is also a snapshot: it holds the last five saved incidents and the last 80 trace entries,
and `dropped` is how you find out that was not enough.

## Where the parts live

| | |
|---|---|
| the recorder and the panel | `Resources/web/app/js/core/layout-diagnostics.js` |
| the route | the `POST /v1/diagnostics/report` case in `Sources/RemoteServer.swift` |
| the write, the limit and the rotation | `Sources/DiagnosticReport.swift` |
| the store's behaviour on a real disk | `Tests/diagnostic-report-focused.mjs` |
| the panel, the completeness block and the two spellings of the route | `Tests/web-diagnostics-send.mjs` |
| the Cloud command, answered with the same store | `CloudDiagnosticsReportRoute` in `Sources/CloudLocalRoute.swift` |
| `cloud-status.json`, `cloud.status` and the notice | `Sources/CloudStatus.swift`, fed by `Sources/CloudAppBridge.swift` and `Sources/CloudTransport.swift` |
| the Cloud report route, status file and notice on real files and fixtures | `Tests/CloudTransparencyTests.swift` |
| the Cloud command, its size check and the status sheet's data | `Tests/web-cloud-failures.mjs` |
| receive-failure rows, their buffer and automatic delivery | `Resources/web/app/js/net/cloud-viewer-events.js`, `_recordReceiveFailure` and `_deliverViewerEvents` in `net/cloud-client.js`, `noteCloudDoor` in `main.js` |
| `cloud-viewer-events.jsonl`, its bounds and its log line | `CloudViewerEventLog` in `Sources/DiagnosticReport.swift`, `CloudViewerEventsRoute` in `Sources/CloudLocalRoute.swift` |
| the rows' failure injection, delivery and red proofs | the `viewer events ·` checks in `Tests/web-cloud-failures.mjs`, the door rows in `Tests/web-cloud-onboarding.mjs`, the `diagnostics.events` checks in `Tests/CloudTransparencyTests.swift` |
| the page→Mac batch contract | `Tests/cloud-viewer-events-contract-batches.json`, regenerated with `CLAWDLINE_WRITE_VIEWER_CONTRACT=1 node Tests/web-cloud-failures.mjs` and compared byte for byte there; the `page→Mac contract` checks in `Tests/CloudTransparencyTests.swift` |
| the notification road this was built during | [`docs/notifications.md`](notifications.md) |
