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

**Open the panel after the fault, not before.** Some of what the report contains — the service
worker's half of a notification tap, for one — is only folded in when the page next wakes.

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
the defect it was built during: today's events are `sw.*` and `route.*`, next month's will be
something else, and neither the route nor this document has to change for it.

## It says how complete it is

**An empty record and a broken record look identical** unless something writes down the difference,
and this machine has paid for that lesson more than once. So the report states its own gaps:

```json
"completeness": {
  "trace":      { "kept": 80, "limit": 80, "dropped": 46, "droppedThrough": 8123, "from": 8130, "to": 20455 },
  "incidents":  { "kept": 3, "limit": 5 },
  "sources":    [ { "name": "serviceWorker", "state": "unread", "entries": 0, "reads": 0, "at": null } ],
  "whole":      false
}
```

* **`trace.dropped`** is how many entries the ring buffer threw away before the ones you are
  reading, and **`droppedThrough`** is the clock reading it stopped at. A trace that begins in the
  middle of the story and a trace where the story begins there are different readings.
* **`sources`** are recorders that live somewhere the page cannot see and are folded in when it
  wakes. Their `state` is the distinction that matters:

  | state | what it means |
  |---|---|
  | `unread` | declared, and this page has not looked yet — **not** the same as nothing being there |
  | `merged` | looked, and folded in `entries` of them. `merged` with `entries: 0` means there really was nothing |
  | `unavailable` | the store it lives in is not present in this browser at all |
  | `failed` | the read threw, or what came back was not what it should be |

* **`whole`** is `true` only when nothing was dropped and every declared source has been merged. It
  is not a claim that the fault is visible in the file — no counter can say that — only that
  nothing the recorder knows about is missing from it.

A report that carries no `completeness` block at all — an older build — is written with
`"completeness": {"stated": false, …}` rather than with the field simply absent, so an absent block
never reads as an empty one.

## The rows a flood cannot reach

The trace is a ring of 80 against a page that writes about five entries a second, so it holds
roughly **eighteen seconds** — and the gesture that sends a report (Settings, five taps, the
button, *Send to Mac*) takes longer than that. On 2026-09-06 and again on the 7th, two reports
came back from a real phone in which six independent 80-entry windows contained not one `sw.` or
`route.` entry between them. The evidence of the fault was written down and then evicted by
`layout` before anybody could read it.

**`sources` is the half of this recorder that a flood cannot reach**: one row per recorder, holding
the last state, how many times it was looked at and a running count. It survived both of those
reports intact. So the notification road is read from four rows rather than from the trace:

| row | `reads` | `state` | `entries` |
|---|---|---|---|
| `serviceWorker` | times the worker's trace was read back | `merged` / `unavailable` / `failed` | entries folded in |
| `notificationMessage` | messages that actually arrived | the last one's `type` | same as `reads` |
| `notificationWant` | reads of the worker's record | the last answer: `routed`, `stale`, `settled`, `declined`, `none`, `unavailable` | times it routed |
| `notificationOpen` | decisions by `openWanted` | `found` or `missing` | sessions actually opened |
| `serviceWorkerMark` | times the worker's own note was read | `wants` — the worker knows how to leave a record — or `absent` | 1 when current |
| `workerPosted` | reads that folded in worker entries | `posted` | taps the worker says it handed to a window |

**`notificationMessage.reads` is the number the road could not produce before.** Zero means no
message was ever delivered to the page; one with no routing behind it means a message arrived and
was declined. Those are different faults with opposite fixes, and until these rows existed both of
them looked like an empty trace.

**`serviceWorkerMark: absent` means the phone is running a worker older than the page**, and every
row under it is then about that worker rather than about the build you are reading the source of.
A page and the worker beneath it are two builds that can drift apart, and on 2026-09-07 they did:
116 reads of a record that the worker on the device did not know how to write, with nothing on
either side able to say so. The worker writes a capability rather than a version, because a version
is a number somebody has to remember to bump.

**`workerPosted` against `notificationMessage` is the drop, and neither alone is.** One is how many
taps the worker says it handed to a window; the other how many of them the page received.

**Zero is only a reading when the recorder could have said otherwise.** A row at zero because
nothing happened and a row at zero because the reporting was removed look identical, so read them
together: `notificationWant` and `notificationOpen` reporting while `notificationMessage` is zero
is a silent road; all four silent is a silent recorder.

## Adding a source of your own

If your observation point records into somewhere the page reads back later — Cache Storage, a
worker, IndexedDB — declare it once at module load and report each read:

```js
Diagnostics.source("myRecorder");                      // now "unread" until something looks
Diagnostics.sourceRead("myRecorder", "merged", count); // or "unavailable", or "failed"
```

`Resources/web/app/js/input/route.js` is the worked example: it declares `serviceWorker` at load
and reports every one of `readWorkerTrace`'s four outcomes.

## The route

`POST /v1/diagnostics/report`, body = the report.

It goes to **whatever server is serving the page** — this Mac, directly on the local network or
through its own tunnel. A page served by anything else answers something else, and the panel prints
that refusal rather than a tick: what you must never get here is a green mark and no file.

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
| the notification road this was built during | [`docs/notifications.md`](notifications.md) |
