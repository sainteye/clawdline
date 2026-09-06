# Notifications: who hears what

Written for whoever adds a notification here and has to decide who it is for.

A session that has stopped to ask something is the only state that costs money for every second
nobody notices. Everything in this file follows from one rule, and the rule is not about volume:

> **Tell whoever is actually blocked, in the channel they can act in.**

Depth decides the audience. A session with no orchestrator role is one a person opened for
themselves — that is the definition of a root — and everything below one is working for somebody
else, who is a program and can be told directly.

## The four channels

Three of these are this Mac talking to itself. The fourth is handed to Apple, or Mozilla, or
Google, and carried to a device nobody here controls, which is why it is the one with rules
around it.

| | what it is |
| --- | --- |
| **Push** | A phone in another room buzzes. Sealed per subscription under RFC 8291, so the push service sees a P-256 point, a salt and ciphertext. The only thing in the app that leaves the machine on its own — see `WebPush`. |
| **A typed line** | One versioned sentence typed into a terminal on this Mac by the durable completion postman, so the conversation that asked for the work hears it finished. Its stable `notice_id` is ACKed after observation. |
| **The island** | The notch leans out. Waiting wins, then a finish held for 3.4 seconds, then whatever is running underneath — `NotchIsland.refresh`. |
| **Your own program** | `on_state_change` runs an argv array with the event in environment variables. No shell, four at a time, ten seconds each — `StateHook.fire`. |

## The routing table

| what happened | who is blocked on it | channel | what it says |
| --- | --- | --- | --- |
| root stops to ask | you | push | Unconditional, ahead of every preference. The one interruption in the app that earns itself. |
| root says it delivered | you | push | The root's own authenticated receipt, pushed once where it is created and never again for a repeat of the same report. Only with `push_on_delivery` on. With `smart_notifications`, the body carries the session's own summary verbatim — no model turn is spent on this one. |
| child stops to ask | you, and only you | push | Louder than a root asking, and it carries the clock. |
| child finishes (depth 1) | the root session | durable typed line, no push | The id, the state, the path to `result.json`, a stable notice id and the ACK route. |
| a task below a task finishes (depth 2) | the task that dispatched it | durable typed line, no push | The same, plus how many of that task's own children are still running. Unreachable in a live tree, where a child dispatches nothing; kept for a stored record an older build left behind. |
| the last of a fan-out ends | you | push | One notification for the whole subtree, with a count and how many failed, and only with `push_on_fanout` on. With `smart_notifications`, the task titles, states and authored summaries become one sentence instead. |
| an agent has timely content you are waiting for | you | push | The task-secret or root `/notify` route, only with `orchestrator_agent_notify` on. When it is off, `409 agent_notify_disabled` spends no allowance; the agent does not retry and keeps the content in `result.json`. |
| a tab whose task is over | nobody | silent | A child's terminal lingers for `orchestrator_child_linger` (180s) after the work ends. |

`StateHook.pushDecision(role:minutesLeft:)` is the asking half of that table as one pure function.
It takes the role and a number of minutes, and answers with a sentence or with silence; it touches
no terminal, no clock and no phone, so the whole policy is checkable in a test. It used to take an
event as well, because there was a second one — see below.

`Orchestrator.deliveryMessage(project:label:summary:smart:)` and
`Orchestrator.batchMessage(project:label:done:failed:)` are the other two rows' wording, pure for
the same reason.

## Why "a long turn finished" is not in that table any more

There was a row here that fired on `working → idle` when the turn had run for at least 120s, under
a switch labelled *Notify when a long turn finishes*. **The label named the work; the trigger named
the terminal.** Answering one question ends a turn. So does finishing an intermediate step, or
asking something that is not a permission prompt. Every one of those buzzed a phone to say the work
was done, and somebody who turned the switch off was turning off a lie rather than declining news.

What replaced it was already in the app and pushed nowhere: a root reports one authenticated
delivery at the end of the turn it delivered in — `Orchestrator.reportSessionDelivery` — and the
broker de-duplicates a repeat of the same report to `created: false`. So the push hangs off the
receipt, on the branch that creates one, and *has delivered, and is waiting for you* is a
sentence the reader can act on. The fan-out row kept its notification and got a switch of its own;
the point of the split was to leave two switches that mean something rather than one that did not.

The notch is unaffected and still dances when a long job ends. That surface is on the screen you
are already looking at, so being told twice there costs nothing.

## Why a child that finishes says nothing to your phone

Four roots with five children each is twenty sessions, and every one of them is a terminal that
goes idle when it is done. Before the role table existed, a busy Mac was therefore up to twenty
identical *finished a long run* notifications — none of which said which tree it belonged
to, or whether anything was still outstanding.

That is the same mistake `StateHook.react` already argues against for root sessions, made one level
down where nobody had looked: a notification that fires for everything trains somebody who reads
none of them. The one fact a person wants out of all twenty is that the work they asked for has
come back, and how much of it failed. That is one sentence, and it arrives once.

## Why a child that is waiting is louder than a root that is waiting

Nobody is looking at that tab. Its timeout is counting down. A permission prompt from command
screening has no *always allow* on it and no one sitting there to press anything — the child
briefing warns about that failure twice. So it gets a different sentence, not a politer one, and
the sentence carries how long is left.

Minutes are whole minutes, and absent rather than zero once the clock has run out: `0 min left`
on a lock screen reads as a number somebody forgot to fill in.

## Two things that are easy to get wrong

**The batch is swept from the beat, not from `finalize`.** Cancellation runs *asynchronously*, so
at the instant a task finishes, something already on its way out still counts as live and the count
never reaches zero. `Orchestrator.sweepBatches` asks again a beat later, after the dust has settled,
which also covers cancellation, timeouts, and a tab somebody closed by hand.

**The parent-task lane in `notifyRoot` outlived the level it was written for.** A root writes
`root.session_id` into the task it dispatches, so a depth-1 task can be traced back to a tab
through the hook notes. When the tree still had two levels a child dispatched with
`root.parent_task` and nothing else — deliberately, because a Codex child has no hook note to be
found by — and the first guard in `notifyRoot` failed at depth 2, so the line was dropped in
silence; the fix was the parent task's own terminal, which `record(of:)` had been resolving all
along. Nothing reaches that lane now, because a child dispatches nothing. It is kept because a
stored record from an older build still can.

**The line to a grandchild's parent had never fired.** A root writes `root.session_id` into the
task it dispatches, so a depth-1 task can be traced back to a tab through the hook notes. The
briefing tells a child to dispatch with `root.parent_task` and nothing else — deliberately, because
a Codex child has no hook note to be found by — so at depth 2 the old notification guard
failed and the line was dropped in silence. What was left was the polling loop the briefing
prescribes: a child spending turns on `sleep`. The parent task's own terminal is the answer, and
`record(of:)` had been resolving it that way all along. That direct route is still process-bound:
terminal id, assistant, tty, PID, process start, transcript marker proof and conversation id must
all match the stored parent task before a byte is sent. Terminal/TTY reuse is `identity_stale`.

**A terminal-send return is not observation.** Finalization first persists the task outcome and
completion outbox in one atomic registry snapshot. A utility-queue postman then retries
`root_missing`, `root_choosing`, `iterm_modal`, `terminal_timeout`, `identity_stale`, and ordinary
transport failure with bounded backoff; it also retries a successful send until the root ACKs the
same stable `notice_id`. Duplicate lines therefore name one consumption. Eight delivery attempts
end in a visible dead letter, and result/task polling never goes away. Accepted, executed, result-verified,
transport-delivered, observed and acknowledged are six separate facts; HTTP, SSE and Apple Event
success never manufacture the last two.
An ACK store failure compare-and-swaps only that notice's delivery transition back; it does not
replace the latest whole task and therefore cannot erase concurrent worktree, landing or close
receipts.

## The project's mark, and what an iPhone does with it

Notifications carry the project's own pixel mark, served from `/project-<size>-<packed>.png`.

**The picture is the name.** The alternative was an id — a hash of the project's path, or the
session's — and both put a handle to a particular project into a URL that has to be fetchable
without credentials, because the fetch is made by the operating system drawing a notification and
not by the page. The packed form carries the colours themselves and nothing else: no path, no
session id, no project id, nothing to enumerate. It is also why the answer can be cached for a
year — a URL that is its own content can never go stale. See `RemoteIcon.pack`.

Chrome and Firefox draw it. **A home-screen web app on iOS does not.** Measured on real hardware
on 2026-08-25: it draws the icon from the manifest whatever the message says, whether the mark is
fetched from a URL or carried whole inside the sealed payload, and it ignores the large `image`
field the same way. That matches what everybody else reports; the Apple developer forum thread
about it has no reply and no workaround. So on an iPhone a notification is told apart by its words
alone, and there is nothing this end can do about it.

**Declarative Web Push does not rescue it, and that was tried rather than assumed.** Safari 18.4
added a second rendering path: a payload carrying `"web_push": 8030` and a `notification` object,
sent with `Content-Type: application/notification+json`, is validated and drawn by the browser
with no service worker involved — and WebKit's explainer says "most of the optional members of
`NotificationOptions` can also be specified" without saying which. It was worth an experiment
because it is a different code path, not a different spelling of the same one. Measured on the
same hardware on 2026-08-26 with an absolute `icon` URL, verified reachable through the tunnel
without credentials beforehand so that a failed fetch could not be mistaken for a refusal: the
notification arrives, and it carries the manifest icon. The probe was removed; only this
paragraph is left, so that the next person to have the idea gets the answer instead of the build.

What is left for iOS, if a per-project mark ever matters enough: the manifest icon is the only
picture that platform will draw, and it is baked in when the web app is added to the home screen.
There is no per-notification and no per-project version of it.

One note on how that was established, because the first attempt proved nothing. The probe used a
real project's mark, which is an orange creature on a dark ground — and so is the app's own icon.
At the size a phone draws a notification the two were the same picture, so *the icon did not
change* was exactly what a working icon and an ignored one both looked like. A control that looks
like the treatment is not a test.

## Tapping one, and the trace that says where the tap stopped

A notification about a session carries `/#session=<id>`, and reaching that session takes six steps
in three places. Five of them had been proved and the sixth had never been looked at, so a tap that
ended on the session list said nothing about which step lost it.

| | where | what it means when it is missing |
| --- | --- | --- |
| the URL is written encoded | `WebPush.sessionURL` | a tmux pane id's per-cent was read as an escape |
| the worker draws the notification | `RemotePage.serviceWorker()`, `push` | the payload key is not `url` |
| `sw.notificationclick` | the worker | the worker never woke, or Cache Storage is refused |
| `sw.postMessage` / `sw.openWindow` | the worker | which of the two roads this tap took |
| `page.sw.message` | `input/route.js` | the message was sent and never arrived |
| `route.to` | `input/route.js` | the fragment named no session — `/` is the test push |
| `route.openWanted` | `input/route.js` | the id is not in the session list |
| `page.want` | `input/route.js` | the record the tap left behind, and what was decided about it |

The worker's two entries are durable because they have to be: the worker is shut down between
events, and a message posted to a page that was not listening leaves nothing at either end. They go
into Cache Storage — the one store a worker and a page can both open — under
`clawdline-notification-trace`, numbered rather than timestamped, and `readWorkerTrace()` folds
them into the page's own trace at boot and on every `visibilitychange`. `activate` empties Cache
Storage, so a trace does not survive a worker update; that is the right way round, because a trace
is evidence about the build that wrote it.

**Nothing acts on what the trace records.** What an app should do when a client message is dropped
was a design question with more than one answer, and the trace exists so that it could be asked of
a reading rather than of a guess. It has been asked, and the answer is the second road below.

**There is one road for an open window, not two.** `postMessage` is defined on `Client`, so every
window `clients.matchAll` can return has one — which makes the `client.navigate` fallback beside it
unreachable in a browser, and means a dropped message ends the tap with no second attempt behind
it. `clients.openWindow` is reached only when `matchAll` returns nothing at all, and on iOS the
system opens the web app itself before the handler runs, so a phone may take the message road even
from what the person experienced as a cold start. "I tried it both ways" is not evidence of two
roads having been tried.

## The second road: a tap that survives its own message

The message gets one attempt. So `notificationclick` also writes down *what the tap was for* — one
record, in a cache of its own, `clawdline-notification-wanted` — and the page reads it back at the
two moments it wakes up, `boot` and `visibilitychange`, which are the same two the trace is read
at. The message still goes first and still does the work when it arrives. The record is what turns
a dropped message from the end of the tap into a delay.

**And at a third moment, because those two are the edges and the write lands between them.** The
worker does not hold the tap up for its cache write, so `boot` can read the store before the record
is in it — and a page that came up in the foreground gets no `visibilitychange` to read it at, so
by the time one arrives the record is usually past the two-minute window and is thrown away unread.
That is the second road failing silently on exactly the tap it exists for. So `view/list.js` reads
it again with every session list, beside the `openWanted` retry the same request has always had.

**And at a fourth, because a tap made in front of the app is not a wake-up at all.** This is the
case the three above between them do not cover, and it is the one somebody holding the phone is
most likely to be in: the app is open on the session list, a banner slides down, they tap it.
Nothing loads, so there is no `boot`. Nothing was hidden, so no `visibilitychange` comes. And the
session list only arrives when something on the Mac changes — the change that *sent* the
notification, which pushed a list before the banner was ever tapped; after that the stream is as
quiet as the Mac is. So the fourth read point is the window taking the focus back, which a banner
drawn over the app hands back when it goes: `input/route.js` listens for it beside the
`hashchange` listener, and it costs one store read on a signal that arrives with the tap rather
than with the Mac's next state change. It does not replace the list — a page that already has the
focus when the record lands gets nothing from it — which is why there are four and not three.

**None of them stops looking.** Until 2026-09-06 the retry stopped as soon as a read came back
with anything other than "nothing there yet", on the reasoning that a record arriving later would
arrive with a wake-up of its own. That sentence is false — the paragraph above is the shape it is
false in — and the way it failed cost the whole road rather than one tap: a page reads the store
at `boot`, finds a record from last night, answers `stale` *correctly*, and never looks again. A
`/` record from a test push closes it the same way, and so does a tap that worked. After an
evening of testing there is always something in that store to answer the first read with, so on
the phone it was reproducible and looked like the second road had never shipped. The flag was a
fact about the page's life; what it was reaching for is a fact about one record, and the record's
own id already says it — see *It is answered once* below.

What that costs is one store read per session list, and there is no cheaper honest version: the id
that decides whether there is anything to do is inside the record, so it cannot be consulted
without opening the store. So it was measured rather than asserted. Chrome 152 on this Mac, 500
reads per arm, the whole path `caches.open` → `match` → `json`: a median of **0.2 ms** against an
empty store and **0.5 ms** against one holding a record, beside **0.2 ms** for one three-row
`innerHTML` and a forced layout on the same page. Nothing waits on it — it is three promise hops
off the render's path. Two things that measurement cannot say: what iOS Safari charges, and what
the first read of a page costs, which is a different number entirely (the storage subsystem waking
up put the first arm of the first run at a 6 ms mean and a 230 ms worst case, and that cost is paid
once whether this reads or not). The guard that is left is the one that was always right: a read
already in flight, because lists arrive faster than Cache Storage answers.

**It is a separate cache from the trace on purpose.** The trace is a numbered history nobody obeys;
this is an instruction carried out once and then destroyed. In one store the two would share a
read-modify-write, a sequence number and a pruning rule, and the first bug in either would be a tap
that vanished or a tap that fired twice.

Four rules keep the record from being worse than the fault it fixes.

**It goes stale after two minutes** — `WORKER_WANT_MAX_AGE_MS` in `input/route.js`. Not a guess
about attention: it is sized for the slowest thing between the tap and the read, which is iOS
launching the web app cold, pulling the document down the tunnel and running the modules before
`boot` gets there. Seconds would drop exactly the taps this exists for. A day would let a
notification tapped last week move somebody who has just opened the app to read something else,
which is the worse failure of the two, because nothing on the screen would say why it happened. A
record older than the window is thrown away rather than obeyed.

**It is answered once.** The record carries the tap's own id; the page remembers the last id it has
answered, so waking twice does not route twice, and the record is deleted once the session it names
is actually open. **The id is the whole of that rule** — every read point may look as often as it
likes, and what refuses a second routing is the comparison with the id in front of it rather than
anything the page remembers about how many times it has looked.

**And it is spent by its own session, not by the next opening that happens to succeed.** The store
holds one record and the newest tap replaces it whole, so two notifications tapped before the list
arrives are one record while the page is still holding the *first* tap's id: a deletion that simply
emptied the store threw the second tap away unread. The page therefore remembers which session the
record asked for, deletes only a record whose id is the one it is spending, and lets the record go
when the first whole list lets go of the request — a notification about a session that has since
closed is not a request that should survive that answer.

**The message wins when it arrives, and the guard runs in both directions.** The same id travels on
the `postMessage`, so whichever road acts on a tap first marks that id answered and the other one
declines it — a message that lands marks the record on the way past, and a message that lands
*behind* a record already carried out is refused by the same comparison. Both halves are needed
because both orders happen: a page resumed from the background starts its read at
`visibilitychange`, and the message queued while iOS had it suspended is dispatched during the
three asynchronous hops that read takes. Without the second half the two roads both acted on one
tap — the transcript fetched twice, and on a phone a second history entry, which is one back
gesture that does nothing. Comparing addresses instead of ids does not work in either direction: by
the time the page wakes, the address is wherever the person has got to.

**`url: "/"` is not a request.** The test push and a fan-out notification both carry it, and a
record saying *the person wanted the session list* would send whoever tapped one back to the list
they were already on, every time they opened the app for the next two minutes. The worker writes
none, and the page declines one anyway — the worker's copy of that judgement is a second copy of
`sessionCandidates`, and second copies drift, so `Tests/web-notification-route.mjs` drives both
gates over a table of URLs rather than comparing them as text.

**And the cost, which is real: a worker update loses the record.** `activate` empties every cache,
so the one tap that happens across an update keeps only the message road it had before this
existed. That is the same price the trace pays and it is accepted for the same reason — the
alternative is IndexedDB, thirty lines of callbacks for one record — but for the trace losing an
entry costs evidence, and here it costs a tap. If a notification ever stops working exactly once
after an update, this is why.

`Tests/web-notification-route.mjs` runs the worker and the page against each other, with the
message delivered and with it dropped, on tmux pane ids. Every fixture in the two suites either
side of it — `web-service-worker.mjs` taps `/#session-9`, `/#cold`, `/#fresh` — is a URL no
notification has ever carried, which is why neither of them could see this segment. Its 198 checks
cover the dropped message, the list that has not arrived yet, a record too old to obey, a page woken
twice, the two roads meeting in both orders, two notifications tapped before the list arrives, a
request let go of, the record that lands between two reads, a first read that answers `stale` or
`declined` and must not close the road behind it, two taps made one after the other with the app in
front, and the focus coming back as a read point of its own. That number is compared with the
suite's own summary line by `Tests/docs-suite-facts.mjs`, because the last one written here was
right on the day and wrong two commits later, with the suite green at every step.

**Reading it on the phone.** `?debug=layout` needs an address bar and a home-screen web app has
none, so the panel opens from five presses on the version line at the bottom of Settings — wordmark,
Settings, the small line with the version in it, five taps inside two seconds, then the
`LAYOUT DEBUG` button at the bottom left. **Open it after the tap, not before**: the worker's two
entries are read in when the page wakes, so a report taken before the notification was tapped
cannot contain them — and the report now says which of the two it is, because `unread` and
`merged, and there was nothing` used to be one empty trace.

Its Copy report button puts the whole trace on the clipboard, and on 2026-09-06 that was too
long to paste: the program hung and Universal Clipboard had not synced it either. **Send to
Mac** beside it writes the same report to `~/Library/Logs/Clawdline/diagnostics/report.json`,
which is the path to read instead of asking anybody for a paste —
[`docs/diagnostics.md`](diagnostics.md) is the whole of it.

**Every push whose title is a session's name now carries that session's address**, which was
not true when this section was written: `Orchestrator.announce` — a fan-out finishing — titles
itself `label ?? project`, the root's own label, and fell back to `url: "/"` whenever the batch's
root key was `task:<id>` (a root with no session id) or the root no longer resolved. On a lock
screen that was indistinguishable from *waiting for you*, and tapping it correctly went nowhere;
the body was the only tell, `finished 3 tasks` rather than `waiting for you` or `delivered`. Four
other producers were in the same state. The next section is the whole list, both halves of it.

## Which pushes carry an address, and which cannot

> **If the title is a session's or a task's own name, the push must carry that session's address.
> If the title is nobody's name, `/` is the right answer.**

Everything that reaches a phone goes through `Orchestrator.pushURL(forSessionID:)`, which is that
rule with `WebPush.sessionURL` — the encoding — behind it. Where the id came from *outside* rather
than off a record this Mac already holds, `pushURL(forSessionID:watching:)` checks it against the
sessions being watched before promising it: **an address that opens nothing is worse than `/`**,
because the list at least says what there is, while a fragment naming a session nobody has stops
there with nothing on screen to say why.

| push | what its title says | where it points |
| --- | --- | --- |
| `StateHook.sendPush` | the session that stopped to ask | that session |
| `Orchestrator.announceDelivery` | the session that delivered | that session |
| `Orchestrator.notify` (a task's own secret) | the task, by its title | the tab that task is running in |
| `Orchestrator.agentNotify` (the machine token) | whatever the root called it | the session the caller named, when it named one |
| `Orchestrator.announce` | the root's own label | the root; failing that, a task of the batch the watch still holds |
| a scheduled task that failed or timed out | the schedule's title | the tab it ran in |
| `DeployWatch` | the deploy | the session that deployed |
| `POST /v1/push/test` | `Clawdline` | the session the phone named, while this Mac is still watching it |

### And the five that stay `/`, with the reason each

**This is the list somebody will otherwise "fix".** Not one of these has a session to name, and
giving one an address would mean inventing a destination rather than finding one.

| where | why there is no session to name |
| --- | --- |
| `Orchestrator.scheduleInventory` — a schedule file that will not parse | nothing has been started; the fault is in a file and there is no run to open |
| `Orchestrator.sendSchedulePush` — a scheduled dispatch that was refused | the dispatch failed, so no tab was ever opened |
| a scheduled task whose outcome is `spawnFailed` | the same reason one step later: `childTerminalId` is nil by construction for a tab-opening refusal. `Orchestrator.scheduleFailureSessionID(outcome:childTerminalId:)` is that one line, pure, so it can be held to it |
| `SmartNotification.send`, the coalesced branch | its title is *N things finished at once* — the name of no session, and picking one of the eleven would be a claim about the other ten |
| `Sources/main.swift`, `clawdline://push?test=1` | a Mac-side URL scheme with no session context |

The last of those is the one that *could* take an id — `&session=` is a query parameter away — and
it is deliberately left alone. The loop that needed closing is the one a person walks on a phone,
and that is `POST /v1/push/test` with the button in Settings behind it. This scheme is fired from a
hotkey utility or a shell script on the Mac, where what it would open is a browser tab on the
machine you are already sitting at. A parameter nothing passes is a second spelling of the tested
road, and it is the spelling that goes stale.

### The test push is the loop, and its words are what keep it honest

`POST /v1/push/test` takes an optional `session_id`, and the button in Settings sends whichever
transcript is on screen — `S.openId`, not `S.selectedId`, which is only the highlight in the list.
So the road can be walked on purpose instead of waited for: open a session, press the button, put
the app in the background, tap what arrives, and the answer is the screen you land on.

**The wording does not change either way.** The title stays `Clawdline` and the body stays
`L.t.pushTest`, because *a test that arrived must never be mistaken for a session that needs you*
is true on account of the words and not the address. Making it *look* like a session in order to
make it feel real is the mistake this is written down to prevent, and
`Tests/OrchestratorCoordinationTests.swift` pins both halves — the address it carries and the two
things it says.

## The numbers

| | |
| --- | --- |
| `WebPush.ttl` | 3600s. Longer than a lift, a tunnel or a meeting; shorter than the point at which the sentence stops being true. |
| `WebPush.urgency` | `high`. Defensible only because it is rare. |
| `WebPush.maxPayload` | 3993 octets. If a title and body crowd out the mark, the mark is dropped and the message still goes. |
| `orchestrator_max_descendants` | `orchestrator_max_children` × 4 — twenty at the default, and the tree is one level deep, so that is four roots with five children each. The arithmetic that made per-tab pushes untenable. |
| `orchestrator_child_linger` | 180s. How long a finished child's tab is kept before it is closed for you. |
| `SmartNotification.timeout` | 8s. The most a completion waits for Haiku before the ordinary wording wins. |
| `SmartNotification.maxPending` | 4. Past the bounded queue, the ordinary notification is sent immediately. |

## The six switches

`push_on_delivery` covers the delivery receipt: one session saying it is done and waiting for you.
`push_on_fanout` covers the last task of a fan-out coming back. They were one key, `push_on_finish`,
which also covered the turn-stopped push that no longer exists — so `push_on_fanout` inherits its
value on the way in, because an answer of "do not buzz me when work ends" was given about this event
too. `push_on_finish` is never written back.

`smart_notifications` changes only the wording, and it means two different things on the two paths.
On the fan-out push it sends the task titles, states and authored summaries through a tool-free,
low-effort `haiku` turn; it never sends a second notification, and missing input, queue pressure,
timeout and malformed output all choose the ordinary wording before the one push is handed off. On
the delivery push it spends nothing at all: the session's own summary is carried verbatim, so there
is no model turn, no timeout and no fallback to arrange.

`push_on_deploy` covers the separate deploy-finished push, on success and failure.
`orchestrator_notify_root` covers the typed line, in both directions.
`orchestrator_agent_notify` covers content an agent proactively sends through either `/notify`
route; it is on by default, and turning it off does not stop the task itself.

A session that has stopped to ask is under none of them, in either direction, on purpose.
