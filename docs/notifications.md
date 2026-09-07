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

**Declarative Web Push does not rescue the per-project icon, and that was tried rather than
assumed.** Safari 18.4
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

## On Apple, the browser owns the tap

An installed web app that was already open produced the decisive reading on 2026-09-07: the page
and worker were the same build, the worker had registered all five listeners including
`notificationclick`, the notification arrived, and tapping it emitted no click entry and opened no
Session. That is upstream of every message, cache, focus and routing fallback below. WebKit bug
268797 describes the same missing event, and a Firebase report describes the same dependence on
whether the home-screen app was already open.

Apple subscriptions therefore receive Declarative Web Push. Their encrypted plaintext has
`"web_push": 8030`, a nested `notification`, and an absolute same-origin `navigate` URL, and the
request is labelled `application/notification+json`. Safari can display it and open the Session
without waking the worker or dispatching `notificationclick`. The authenticated request `Origin`
is stored with the subscription so a root-relative Session address can become that absolute URL;
an older row without an origin stays on the legacy path until the page subscribes again.

Non-Apple endpoints keep the existing payload and `application/octet-stream`. The declarative
JSON is also a valid legacy payload: if a browser does hand it to the worker, the worker reads the
nested notification and uses `navigate` as the old `data.url`. One physical device owns one current
endpoint, so a PWA reinstall cannot leave an old legacy row buzzing beside the new declarative one.

## Enabling notifications

The page does not wait directly on `navigator.serviceWorker.ready`: that promise never rejects and
may wait forever. It registers `/sw.js` at the moment the reader presses Enable, uses that exact
registration, and gives installation/activation 15 seconds. Permission has a 60-second bound;
VAPID-key lookup, the browser's `PushManager.subscribe`, and sending the subscription to the Mac
each have a 30-second bound. A failure always releases the button and shows `[stage: code]`.

The same boundaries are recorded as `push.<stage>.begin`, `.end`, or `.failure` in the bounded
diagnostic trace. They contain only a stage and error code, never an endpoint, public key, or
subscription. On the Cloud PWA the Settings version line falls back to the immutable
`window.__clawdlineCloud.build` until the Mac's friendlier app version arrives, so the five-press
diagnostic door is available even when the snapshot that would normally populate `S.version` is
late.

## Tapping one

A notification about a Session carries `/#session=<encoded-id>`. On Apple subscriptions the
declarative `notification.navigate` member is the routing mechanism: WebKit opens that URL and
the page's ordinary fragment router selects the Session. It does not depend on
`notificationclick`, because the foreground case measured on iOS did not dispatch that event.

Browsers without Declarative Web Push receive the same nested JSON through the legacy `push`
event. The service worker draws it, then its small `notificationclick` compatibility handler
focuses one window and posts `{type: "navigate", url}`, navigates an uncontrolled client, or
opens a window when none exists. The page feeds that URL into the same fragment router.

There is deliberately no persistent second routing road. Earlier builds wrote a
`clawdline-notification-wanted` record to Cache Storage and reread it at boot, on visibility,
on focus, and after every Session-list update. Once declarative navigation had already succeeded,
that record could outlive the tap: returning to the list replayed an older request and reopened its
Session. The worker now deletes caches left by those builds when it activates, and the page never
reads or writes a notification routing record. Leaving a phone detail also removes
`#session=…` from the current history entry, so the list's address no longer asks to reopen what
the reader just closed.

The contracts are split by boundary:

- Swift push tests hold the encrypted declarative payload, content type, origin, and encoded
  Session URL.
- `Tests/web-service-worker.mjs` executes the legacy worker's install, activate, fetch, push, and
  click handlers, including the nested declarative payload.
- `Tests/web-pages.mjs` drives the fragment router with real tmux-shaped ids and keeps a
  regression for the stale Cache Storage record that used to reopen a Session after the reader
  returned to the list.

The phone diagnostic report remains a page/layout recorder. It no longer tries to reconstruct a
service-worker click that WebKit never emitted; the retired cache trace and its dedicated
cross-context test measured an abandoned fallback rather than the mechanism Apple now uses.

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
