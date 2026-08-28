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
| **A typed line** | One sentence typed into a terminal on this Mac, so the conversation that asked for the work is the conversation that hears it finished — `Orchestrator.notifyRoot`. |
| **The island** | The notch leans out. Waiting wins, then a finish held for 3.4 seconds, then whatever is running underneath — `NotchIsland.refresh`. |
| **Your own program** | `on_state_change` runs an argv array with the event in environment variables. No shell, four at a time, ten seconds each — `StateHook.fire`. |

## The routing table

| what happened | who is blocked on it | channel | what it says |
| --- | --- | --- | --- |
| root stops to ask | you | push | Unconditional, ahead of every preference. The one interruption in the app that earns itself. |
| root ends a long turn | you | push | Only past `finishThreshold` (120s), and only with `push_on_finish` on. With `smart_notifications`, Haiku gets the bounded last request and answer and replaces the generic body with one sentence. |
| child stops to ask | you, and only you | push | Louder than a root asking, and it carries the clock. |
| child finishes (depth 1) | the root session | typed line, no push | The id, the state, the path to `result.json`. |
| a task below a task finishes (depth 2) | the task that dispatched it | typed line, no push | The same, plus how many of that task's own children are still running. Unreachable in a live tree, where a child dispatches nothing; kept for a stored record an older build left behind. |
| the last of a fan-out ends | you | push | One notification for the whole subtree, with a count and how many failed. With `smart_notifications`, the task titles, states and authored summaries become one sentence instead. |
| an agent has timely content you are waiting for | you | push | The task-secret or root `/notify` route, only with `orchestrator_agent_notify` on. When it is off, `409 agent_notify_disabled` spends no allowance; the agent does not retry and keeps the content in `result.json`. |
| a tab whose task is over | nobody | silent | A child's terminal lingers for `orchestrator_child_linger` (180s) after the work ends. |

`StateHook.pushDecision(_:role:minutesLeft:)` is that table as one pure function. It takes the
event, the role and a number of minutes, and answers with a sentence or with silence; it touches
no terminal, no clock and no phone, so the whole policy is checkable in a test.

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

## The numbers

| | |
| --- | --- |
| `StateHook.finishThreshold` | 120s. How long a turn must run before finishing it is news. |
| `WebPush.ttl` | 3600s. Longer than a lift, a tunnel or a meeting; shorter than the point at which the sentence stops being true. |
| `WebPush.urgency` | `high`. Defensible only because it is rare. |
| `WebPush.maxPayload` | 3993 octets. If a title and body crowd out the mark, the mark is dropped and the message still goes. |
| `orchestrator_max_descendants` | 20 — five children with three of their own. The arithmetic that made per-tab pushes untenable. |
| `orchestrator_child_linger` | 180s. How long a finished child's tab is kept before it is closed for you. |
| `SmartNotification.timeout` | 8s. The most a completion waits for Haiku before the ordinary wording wins. |
| `SmartNotification.maxPending` | 4. Past the bounded queue, the ordinary notification is sent immediately. |

## The five switches

`push_on_finish` covers the whole *it finished* class — root turns and fan-outs alike.
`smart_notifications` changes only the wording of that class. It is off by default; when enabled,
the last request and answer or the task result summaries are sent through a tool-free, low-effort
`haiku` turn.
It never sends a second notification: missing input, queue pressure, timeout and malformed output
all choose the ordinary wording before the one push is handed off.
`push_on_deploy` covers the separate deploy-finished push, on success and failure.
`orchestrator_notify_root` covers the typed line, in both directions.
`orchestrator_agent_notify` covers content an agent proactively sends through either `/notify`
route; it is on by default, and turning it off does not stop the task itself.

A session that has stopped to ask is under none of them, in either direction, on purpose.
