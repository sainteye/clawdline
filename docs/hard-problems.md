# Problems that took several rounds, and what was true in the end

Most defects here are recorded where they were fixed: a mechanism is explained beside the failure
that produced it, and that is the right place for one. This file is for the other kind — **the ones
that took many rounds, where the interesting thing is not the fix but why it took so long to see.**

It is not a log of everything hard. An entry earns its place by answering one question:

> **What was believed for several rounds that turned out not to be true, and what would have shown
> it sooner?**

If the answer is "nothing, it was just a lot of work", it does not belong here. If the answer is a
knowledge gap — somebody did not know how a browser API behaved — it usually does not either; that
belongs next to the code, where the next reader is. What belongs here is **a way of being wrong that
will happen again**, to somebody who knows everything the last person knew.

Related, and narrower: [`guard-red-proofs.md`](guard-red-proofs.md) is about one member of the
family below — a check that cannot go red. This file is the family.

---

## 2026-09-06/07 — Tapping a notification did not open the session

**Eleven rounds. Four defects, three of them upstream of the one being looked for, and one instrument
that had to be rebuilt three times before it could see any of them.**

### What was actually wrong

1. **The service worker on the phone was never updating.** The page was current; the worker was from
   an earlier build. Registering on every load and `update()` at both wake-ups both failed to move
   it, and there was no way to see the skew, so **every reading taken for two rounds was about a
   program nobody was looking at**. Fixed by having the worker stamp its build into a mark the page
   reads, and unregistering and reinstalling when the two numbers differ.
2. **Taking over deleted the record the tap had just left.** `activate` cleared every cache — two
   defensive lines from when nothing wrote to Cache Storage. By then three things did. The cost was
   written down as "a worker update loses one tap"; it was every tap the worker was restarted under.
3. **The fallback was a one-shot.** A flag meaning "there is nothing left to wait for" was never
   reset, so one stale record read at boot switched the second road off for the life of the page.
4. **`postMessage` is genuinely unreliable** — measured at four posts to one arrival under the old
   worker. That is the defect the whole line was opened for, and it was the last one to be
   confirmed, because the three above kept the instrument from ever seeing it.

### Why it took eleven rounds

Not one of the four was hard to fix. Every one of them took a round to *see*, and the reason is the
same each time, so it is worth naming as one thing:

> **A reading that has no power to disagree with you looks exactly like a reading that agrees.**

Ten instances of that, in one line of work, over two days:

| what was believed | why the reading could not say otherwise |
|---|---|
| "the diagnostic report shows where the tap stopped" | the trace is a ring of 80 against a page writing five a second — eighteen seconds — and the gesture that sends a report takes longer. Six independent windows across two reports contained no entry about the tap |
| "nobody is compiling, the gate says so" | `pgrep -f` matches a truncated command line; the compilers' were 4,832 characters. Three were running |
| "no other suite is running" | `ps -Ao args | grep test.sh` matched the shell running that very grep |
| "the merge is clean, the counts agree" | both parents said 37 runners and 50 suite files, each correct about its own tree. The merged tree had 38 and 51, and `git` had nothing to mark |
| "the negative control passed" | the control's own argv did not contain the string it claimed to test for (`[.]/test[.]sh`) |
| "the mutation went red" | the mutation was applied through an environment override the suite does not read |
| "the worker writes the mark" | the test planted the mark it then read back |
| "the mark says which worker is running" | two consecutive builds both answered `wants`, one keeping the record and one sweeping it away |
| "the fix for the abandoned writes is guarded" | node has no worker to stop, so a handler that abandoned its own writes passed |
| "the build stamp answers it now" | the stamp was added to the writer and the reader was left reporting the old value |

Three of those were caught by other people. Three were caught only by insisting on a red proof
before believing a green one. **None of them was a knowledge gap**, which is why "be more careful"
is not the correction.

### What would have shown it sooner

- **Ask the reading whether it could have said no.** Before a green result is allowed to settle
  anything: *what would have to be true for this to fail, and is that reachable from here?* Nine of
  the ten above answer that question badly in one sentence.
- **Prove the guard red on the real subject, not on a copy you control.** Half the false greens came
  from a mutation that never reached the code it was meant to break — an override the suite ignores,
  a fixture the test wrote itself, an environment that cannot express the failure.
- **When two builds can disagree, make each say which it is.** The single most expensive belief here
  was "the thing I just fixed is what is running". A version the page can read and compare ended two
  rounds' worth of argument in one line of console output.
- **Instrument the thing you cannot see before iterating on it.** The first six rounds each rebuilt,
  waited for a person to tap a phone, and learned one bit. Once the road reported into
  `completeness.sources` — counters, which a flood cannot evict — a single reading replaced a round.
- **A person's taps are the scarcest thing in the loop.** Ten of them went into confirming
  hypotheses one at a time. The instruction that ended it was theirs: *make more assumptions at
  once and fix them together.* Four fixes in one build cost one tap and settled all four.

### Two of the four fixes were not the cause, and the console said so

The round that closed everything at once carried four changes, and a console attached over a cable
disproved two of them as explanations within an hour:

* **"the message went to a window the reader cannot see"** — `windows this worker can reach`
  answered `{count: 1}`, and the one window was `focused: true`, `visibility: "visible"`.
* **"`focus()` was rejected and took the writes with it"** — the line printed was `focus taken`.

Both were real defects and both keep their tests. **Neither is why it started working**, and saying
so is the point of writing this down: a fix that lands beside a recovery collects credit for it
unless something is watching that can say otherwise. Four changes went out together — which was the
right call, and the person paying in taps had asked for it — and the cost of that call is exactly
this: without an instrument fine enough to separate them, all four would now be "the fix".

### What is still not known

**Why a stale worker's `postMessage` does not arrive is not known.** Under the old worker the
counters read four posts to one arrival, with one window, focused and visible; under the current one
the same shape delivers every time. The only variable is which build the worker is, and what WebKit
does differently there is not reachable from this side. Three successes are not proof it will not
return. If it returns, the console lines on both
sides of the gap — `[clawdline/sw]` and `[clawdline/page]` — now say which road took the tap and
where it stopped, and [`diagnostics.md`](diagnostics.md) describes the counters that survive a
flood. **That is the difference between this line and where it started: a failure now names itself.**

---

## Adding an entry

Keep the four headings above: what was actually wrong, why it took as long as it did, what would
have shown it sooner, and what is still not known. The third is the only one that pays for the
file's existence — an entry that stops at "here is what was wrong" is a changelog entry and belongs
in `CHANGELOG.md`.

**Write it when the line ends, not while it is going.** A conclusion written mid-hunt is one more
reading with no power to disagree, which is the thing this file is about.
