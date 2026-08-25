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

| who finished, or stopped | who is blocked on it | channel | what it says |
| --- | --- | --- | --- |
| root stops to ask | you | push | Unconditional, ahead of every preference. The one interruption in the app that earns itself. |
| root ends a long turn | you | push | Only past `finishThreshold` (120s), and only with `push_on_finish` on. Under that you were still looking at the screen. |
| child stops to ask | you, and only you | push | Louder than a root asking, and it carries the clock. |
| child finishes (depth 1) | the root session | typed line, no push | The id, the state, the path to `result.json`. |
| grandchild finishes (depth 2) | the child that dispatched it | typed line, no push | The same, plus how many of that child's own tasks are still running. |
| the last of a fan-out ends | you | push | One notification for the whole subtree, with a count and how many failed. |
| a tab whose task is over | nobody | silent | A child's terminal lingers for `orchestrator_child_linger` (180s) after the work ends. |

`StateHook.pushDecision(_:role:minutesLeft:)` is that table as one pure function. It takes the
event, the role and a number of minutes, and answers with a sentence or with silence; it touches
no terminal, no clock and no phone, so the whole policy is checkable in a test.

## Why a child that finishes says nothing to your phone

Five children with three of their own is twenty sessions, and every one of them is a terminal that
goes idle when it is done. Before the role table existed, a single fan-out was therefore up to
twenty identical *finished a long run* notifications — none of which said which tree it belonged
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

**The batch is swept from the beat, not from `finalize`.** A task that ends cancels the work it
handed on *asynchronously*, so at the instant it finishes, a grandchild about to be taken down
still counts as live and the count never reaches zero. `Orchestrator.sweepBatches` asks again a
beat later, after the dust has settled, which also covers cancellation, timeouts, and a tab
somebody closed by hand.

**The line to a grandchild's parent had never fired.** A root writes `root.session_id` into the
task it dispatches, so a depth-1 task can be traced back to a tab through the hook notes. The
briefing tells a child to dispatch with `root.parent_task` and nothing else — deliberately, because
a Codex child has no hook note to be found by — so at depth 2 the first guard in `notifyRoot`
failed and the line was dropped in silence. What was left was the polling loop the briefing
prescribes: a child spending turns on `sleep`. The parent task's own terminal is the answer, and
`record(of:)` had been resolving it that way all along.

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

## The two switches

`push_on_finish` covers the whole *it finished* class — root turns and fan-outs alike.
`orchestrator_notify_root` covers the typed line, in both directions.

A session that has stopped to ask is under neither of them, in either direction, on purpose.
