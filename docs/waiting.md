# Waiting: what this app is doing while it does nothing

Written for whoever changes something here and wonders why it made the whole app go quiet.

Clawdline spends most of its life waiting. It waits for `osascript` to come back from iTerm2, for
`tmux` to print a pane's contents, for `git status` to finish, for `claude --version`. None of
that is interesting work, and all of it happens between a person doing something and the screen
saying so — which makes **how** this app waits one of the few things that decides whether it feels
alive.

Two ways of waiting have already broken it, and they broke it in opposite directions. Both are
worth knowing before touching anything on this path, because neither failure looks like what it
is: the first one corrupts a record and the second one stops the app observing anything at all,
and in both cases the app carries on looking perfectly healthy.

## A wait that runs everything else

`Process.waitUntilExit()` does not block the thread. It **polls the thread's run loop** while it
waits. On a background thread that is free — nothing of ours is scheduled there. On the main
thread it means every timer, every `DispatchQueue.main.async` and every observer callback runs
*inside* the wait.

So a function that shells out from the main thread is a function that can be re-entered halfway
through, at a line nobody writing it had to think about. Measured on this machine: a one-second
`waitUntilExit()` on the main thread let a timer with a fifth-of-a-second interval fire five
times.

That is what put two walks of the dispatched-task list on the stack at once. `Orchestrator.beat`
runs on the main thread, types a briefing into a terminal through `osascript` on the way, and the
five-second timer fired during that wait. The second walk carried on from a copy of a task the
first was about to advance, found the secret already spent, and marked the task failed — while the
child it had opened was doing the work and went on to finish it. Nothing about that looked like a
threading problem. It looked like the broker losing track of a session.

The counter is still there. `orchestrator.beat_overlap` in
[the audit log](remote.md#when-something-looks-wrong) records a walk that begins while another is
still running, with its sequence number and thread. It should never say anything again; if it
does, this page is missing a second cause.

## A wait that starves the thing it waits for

The fix for the above is to do the waiting somewhere a run loop turning costs nothing, and that is
what [`Sources/Subprocess.swift`](../Sources/Subprocess.swift) is for. The first version of it
dispatched the wait to `DispatchQueue.global` and blocked the caller on a semaphore.

That is a deadlock, and a bad one. **The caller is usually already on that pool.** Blocking one of
a pool's threads to wait for another block from the same pool means that when the pool is full,
the waiter is holding a thread the block it is waiting for needs, and neither ever moves. Filling
the pool to seventy blocked threads and then asking for one wait reproduces it every time.

`SessionWatch.read` — the one place that reads every terminal — runs on that pool and shells out
from inside it. So the first time the pool filled, that reading never finished, the flag saying a
reading was in progress never cleared, and **nothing was ever read again**. Every session kept
whatever state it was last seen in: a spinner frozen mid-sentence, a session that had finished
still drawn as working. A phone or a browser got its snapshot on connect and then silence, so a
tab opened or closed afterwards never appeared or disappeared.

The reason that one is worth a page of its own is how it presents. The app stays up. The menu bar
draws. The web page loads, the event stream connects, the first payload is correct and complete.
Every part reports success. The only symptom is that time has stopped, and the natural reading of
that is *the remote page is broken*, which sends you to the wrong half of the program.

So the wait has a thread of its own, which the dispatch pool cannot starve.

## The rule

**Never wait for a subprocess with `waitUntilExit()`.** Use `waitQuietly()` from
`Sources/Subprocess.swift`, which waits on a thread of its own and returns when the process is
reaped, exactly as `waitUntilExit()` would.

The test that holds this is `waiting for a subprocess does not run anything else on this thread`
in `Tests/main.swift`. It uses a real subprocess rather than a mock, because the behaviour being
pinned belongs to Foundation and not to anything written here, and it fills the global pool before
asking for a wait so that the second failure fails the suite rather than shipping. It also asserts
that the wait actually waited and that the process was reaped, because without those the whole
group passes just as well when the wait returns immediately — which is the way this test would go
quietly useless.

## Where the work runs, and what that costs

Worth having in mind before optimising anything on this path:

| | runs on | what a wait there costs |
|---|---|---|
| `SessionWatch.read` | a global queue, one reading at a time | a stuck reading stops **all** observation; nothing else notices |
| `Orchestrator.beat` | the main thread | a pumped run loop re-enters it; a blocked one delays the panel |
| the panel, the notch, the menu bar | the main thread | anything slow here is felt directly |
| `RemoteServer` routes | the server's own queue, crossing to main for state | a slow crossing shows up as a slow phone |

Two consequences follow from the first row, and they are the ones most likely to be forgotten.
A reading is **one round trip to every terminal**, batched per backend on purpose — iTerm2 answers
for all of its sessions in a single `osascript` run, and tmux answers for all of its panes in a
single `source-file`. And a reading in progress **suppresses the next one**, so anything that makes
a reading slower does not queue up work; it lowers the rate at which the app perceives anything.

tmux used to be asked pane by pane, and the reason written down here was that `capture-pane` has no
plural. That is true and it was never the whole story: **tmux takes a whole script at once**, and
the panes can be told apart by putting a `display-message` marker in front of each capture.
Measured on tmux 3.6a with ten panes, 2026-09-02: one process and a median of 3.45 ms, against ten
processes and 32.01 ms. Ten panes were ten process spawns per beat, and by the arithmetic above
that cost did not queue — it lowered the rate at which anything was perceived.

The delimiter is a marker rather than the order the answers came back in, and that is the failure
semantics rather than a nicety. A command list handed to tmux as `;`-separated *arguments* stops
dead at the first error, so one pane that has gone away takes every pane after it with it —
measured. `source-file -` runs the rest and reports the failure in its exit status, so the reading
is parsed whether tmux was happy or not, and one unanswering pane costs one `.unknown` exactly as
it did when every pane had a subprocess of its own. A tmux too old for `source-file -` prints no
marker at all, which is the one case that falls back to asking pane by pane: going blind on a whole
backend is a far worse trade than the round trips this exists to save.

That is the shape of the budget. The panel open asks every 1.2 seconds; away from it, every
twenty. What fits inside 1.2 seconds is the constraint on this whole path, and the reason
[hooks](hooks.md) exist is to notice the moments that matter without paying for that round trip
more often.
