# Machine resource scheduling

Status: **the exclusion is built and in use; the scheduler in front of it was built, used by
nobody, and removed on 2026-09-03.** What is on this machine today:

- **`test.sh` takes the machine-wide lock itself** (`10130e45`), about two hundred lines between
  two marker comments, covering the `swiftc` invocation *and* the test-binary run after it.
  `Tests/test-sh-lock.mjs` lifts that block out and runs it against stand-ins — 150 checks.
- **`build.sh` takes the same directory** the same way, with the same eighteen-field record, the
  same heartbeat and the same fail-closed wait.
- **`CLAWDLINE_SUITE_JOBS`** (`54891280`), a compile-job ceiling both scripts read and both print
  the provenance of.
- **The measurements below**, which are the most durable thing the night produced and which depend
  on none of the above.

And what is not:

- **The broker heavy-compile lease** — `Sources/OrchestratorLease.swift` and its 1,506-line suite,
  the registry inside `Orchestrator`, the durable store rows, the five `/v1/orchestrator/leases`
  routes, the `heavy_compile_lease` block in Bearings and the `lease` overlay on a session row. It
  landed as `2eef7bb6` / `15924b14`, was corrected in `5098c2b1` and again after that, and was
  removed whole a day later.

## Why the lease was removed

**Not because it was wrong.** It added three real things a directory cannot hold: durable state
across an app restart, queue position and depth a person could read, and a liveness axis for
waiters — a waiter that has died stops holding up the line. Every one of them is a genuine
improvement on a `mkdir` and a `sleep 5`.

**None of the three was needed on the night.** Two sessions ran `./test.sh` at once, four
`swift-frontend` processes reached 46 / 45 / 27 / 8 GB on a 24 GiB Mac, and Jetsam force-rebooted it
at 01:24 and again at 01:45. What stopped that recurring was one directory that two scripts take
for themselves; the lock ran all night and worked. One of the three can be said to have done
nothing rather than merely gone unused: **the admission half never took a decision it could not
have taken by doing nothing.** `Policy` shipped with a nil per-compile peak, on purpose — this
build cannot refuse on a number nobody has taken — so every grant was the floor of one, and one is
what the driver does anyway (measured, below).

**And it cost more than it returned, in a shape worth naming.** Three independent review rounds,
thirty-one findings, two correction rounds. It broke `build.sh` twice, and both breaks stopped the
script reaching `swiftc`. `cf4b63d6`: `"${compile_jobs[@]}"` on an empty array is an unbound
variable under bash 3.2, so the script died before the compiler on every path where no budget was
granted — the default path, **and the fallback taken when the broker is not answering, which is the
path after a crash, that is, exactly when somebody needs to rebuild.** `5de913dd`: `lease_changed`
and `takeover_failed` are the broker saying *ask again*, and reading them as refusals ended a build
on a condition that resolves itself. Neither could happen without the lease; both were found by
review rather than by use.

**What it was left on, and what is still true.** The third review found the same defect class
outside the correction seam, which by this repository's rule
([`AGENTS.md`](../AGENTS.md#verifying-your-work)) stops the patch loop and sends the boundary back
rather than dispatching a fourth correction. Two boundaries were named, and neither was a bug in a
line:

1. **Whoever starts a renewer refreshes the beat first.** `clawdline_confirm_suite_lock` restarts a
   dead renewer without beating, and the restarted loop sleeps before its first tick — so for one
   renewal interval the lock reads *stale* to every waiter while `confirm` has just returned
   success. At the shipped numbers that is twenty seconds against a five-second poll. The acquire
   path already writes the record before starting the renewer; the confirm path does not, and the
   ordering is an invariant rather than a step, so it belongs in one function both callers use.
   **This one survives the removal**, because it is in `test.sh`, which stays. It has not been
   observed: it needs a renewer to have died *and* a second run to be waiting. It is written down
   here so that whoever repairs it does not have to re-derive it.
2. **A refusal cannot be produced without being recorded.** Eighteen places constructed a
   `Refusal` and one `refuse(...)` recorded it, so `queue_full` — built inline inside its own
   `Decision` — never wrote `lastRefusal`, and a session turned away by the queue cap was
   indistinguishable, in Bearings and in the Session overlay both, from one that never asked. That
   was the class the first review named F12, the second F3 and the third F2. **This one went with
   the code.** The lesson it leaves is general and is the reason the class recurred: *make the
   unrecorded construction impossible rather than discouraged* — a single entry point that returns
   the `Decision`, or a type that cannot be built outside it. Sixteen of the eighteen were correct
   by care alone, which is the definition of a boundary rather than a bug.

**The judgement, in one sentence:** the 80% solution was the half that was used, and a feature that
needs three review rounds to hold still is paying for visibility nobody looked at.

The rest of this page is what the night measured, what the lease was while it existed, and the
proposal the whole thing was built from. **The measurements are the part to keep.** They are real,
they were taken on one Mac on 2026-09-03, and none of them depends on the lease having existed.

Clawdline can keep many assistant Sessions useful at once. Most of those Sessions spend most of
their time reading, reasoning, editing or waiting, which is cheap to run concurrently. The trouble
starts when several of them independently enter an expensive machine operation such as compiling,
running a full test suite, indexing a checkout, signing an application or replacing the installed
bundle. Each operation may be correct in isolation while their overlap makes the Mac unresponsive,
extends every build, increases memory pressure, and lets two installers race at the final promotion
boundary.

This document proposes a machine-local resource scheduler for that boundary. It is written as an
open-source design: the protocol, policy and fallback must work without a hosted Clawdline service,
must expose what the machine is doing, and must remain useful to a contributor who runs the scripts
directly rather than through an orchestrated task.

**The sections from *The current boundary* onward are still the proposal, and reading them as a
description of this machine is the mistake this paragraph exists to prevent.** Of the five phases
at the end, Phase 1 is built and in use, Phase 2 was built on 2026-09-03 and removed the next day,
and Phases 0, 3 and 4 have never been started. What Phase 0 asked for — the one-versus-many
benchmark — is the thing that would decide whether any of the rest is worth building, and it is
still the honest next step. The unresolved decisions at the end are still unresolved.

### What a file lock cannot reach, measured rather than reasoned

The lock lives in `test.sh`, so a tree whose `test.sh` predates it has no lock at all. That was
first written here as a limitation somebody had derived; it is now a measurement, and the two
readings are worth keeping apart because the first one was wrong about a specific morning.

On 2026-09-03 a monitor reported two concurrent suites and no lock held. It was a false alarm, and
the diagnosis offered for it — that isolated worktrees carry an older `test.sh` — was not the cause:
both trees involved had the lock block, both carried the current sealed count, and the lock was
held throughout. One run was compiling; the other's only child was `sleep 5`, which is the wait
loop. **What that morning actually demonstrated is the opposite: with a queue, two top-level
`./test.sh` processes are what success looks like**, and a monitor that counts runs will report a
violation every time the lock works — its false-positive rate rises as the feature works better.
Count compilers instead: the JetsamEvent named four `swift-frontend`, not four `test.sh`.

A later scan, calibrated against a known positive first because a zero and a broken query look the
same, found the real thing:

```
git ls-files test.sh in each live worktree, matching the lock block
  <shared checkout>                          157 hits    has the lock
  /private/tmp/clawdline-refactor/ca-exp       0 hits    base 974f8558, no 10130e45
  /private/tmp/clawdline-refactor/patch-land   0 hits    base 0ae16887, no 10130e45
```

So the limitation is real and currently live: **a checkout based on a commit older than the lock has
no lock, and nothing tells its user that.** The remedy is to rebase such a tree onto a `main` that
contains it — not to hand-write a lock file in it, because a record with no renewer behind it
expires in sixty seconds and lands in exactly the window this page describes above.

**This is the thing a broker lease could have done and a file in one tree cannot**: be visible to
every checkout and every snapshot at once. It is written down because the decision to keep only the
file lock was made knowing this, and the next person to reach for the other design should find the
condition rather than rediscover it.

Two habits are worth taking from the same morning. **A reading that rules out one cause is not
evidence that the cause never happens** — the false alarm was correctly disproved and then quietly
treated as proof that no tree lacked the lock, which one scan refuted. And **a limitation somebody
reasoned out should not be written as an incident that happened**; use a sentence that says which
it is, because somebody sent to reproduce an incident that never occurred will find nothing and
conclude the limitation is imaginary.

## What the 2026-09-03 reboots measured, and what they changed

Two forced reboots that night, 01:24 and 01:45, moved this page from a proposal nobody had costed
into one with numbers attached. What follows is the evidence, then the four design decisions it
overturned. Every reading names the command that produced it, because three of the night's wrong
turns were instrument errors rather than reasoning errors.

### The incident, in the shape it actually had

`/Library/Logs/DiagnosticReports/JetsamEvent-2026-09-03-014340.ips` names `swift-frontend` as
`largestProcess`. Four of them carried `rpages` — that field is `lifetimeMax`, the peak footprint
the process ever reached, in pages of `getconf PAGESIZE` = 16384 bytes — of 3017359, 2992845,
1792295 and 522721: about 46, 45, 27 and 8 GB, on a machine whose `hw.memsize` is 24 GiB. The 01:21
report has four as well.

The first reading of that was *one compile's parallelism*, and it was wrong. A frontend here runs
one primary file: the command line is
`swift-frontend -frontend -c -filelist <every source> -primary-file <one file> … -o <one>.o`, with
no `-enable-batch-mode` and no whole-module flag. So those four processes were **four separate
drivers — four concurrent `./test.sh` runs** — each with a frontend of its own, at different points
in the same work. That distinction decides the remedy: mutual exclusion between runs is exactly the
right instrument, and capping one run's job count is not.

### The instruments, and the three ways they lied

**Peak and sample are different quantities and must never share a table column.** `ps`'s RSS is an
instantaneous sample; taken early in a compile it read about 2 GB against a peak above 23 GB. What
Jetsam prints is the lifetime maximum of `phys_footprint`, which
`proc_pid_rusage(pid, RUSAGE_INFO_V4, …)` exposes as `ri_lifetime_max_phys_footprint`. That counter
is **monotonic**, which is what makes polling it legitimate where polling RSS is not: a poll can
only miss growth after the last read. `/usr/bin/time -l`'s `maximum resident set size` comes from
`getrusage(RUSAGE_CHILDREN)` and is likewise a kernel-tracked maximum rather than a sample.

**Do not sum RSS across processes to get a machine total.** Measured at 02:16 with nothing
compiling: 622 processes summing to 22.33 GB on a 24 GiB machine, while `memory_pressure` reported
`System-wide memory free percentage: 81%` in the same minute. Both readings are real and neither is
the physical footprint, because summing RSS double-counts every shared page.

**Free percentage misleads in the other direction**, because after Jetsam kills something the
percentage looks excellent while swap has not recovered.

What did carry the load is `vm_stat`'s anonymous pages together with the compressor and swap. At
02:17, zero compilers running: anonymous 10.78 GB, wired 3.54 GB, occupied by compressor 1.24 GB,
free 2.04 GB, file-backed 5.51 GB; `vm.swapusage` total 12,288 MB, used 10,870 MB, free 1,417 MB.
**The idle baseline already carries about 11 GB swapped out.** Serialising two suites cannot help
with a machine that is full before either of them starts.

**And `swap free` is not a budget, which is the trap in the obvious fix.** The swap file is
elastic. Sampled every 30 seconds through the night, `vm.swapusage` total moved between 9,216 and
22,528 MB, and free swung from 353 MB to 1,417 MB in three minutes with nothing compiling. A fixed
floor on a quantity whose denominator moves is a condition that may never become true: one line set
`swap free >= 2500 MB` as its own admission gate, and no sample taken that night reached it. **A gate that is permanently
unsatisfiable and has no door is a deadlock wearing a threshold's clothes.**

### Three instrument errors in one night, all the same shape

Two of them over-matched and one silently matched nothing, and every one of them was believed for a
while because the answer looked like data:

| what was run | what it answered | the truth |
|---|---|---|
| `pgrep -f swift-frontend` | 3 | 1 — the extras were a sampler and a `/usr/bin/time` wrapper whose *arguments* contain the string |
| `grep` for `-wmo` over a process listing | present | it had matched the searcher's own shell command line |
| `awk '$0 ~ "\\yvar\\y"'` | 0 crossing variables | macOS `awk` does not support `\y`; `grep -w` found 10 |
| `swiftc -output-file-map <map>` with absolute keys and relative command-line paths | exit 0, and objects | the map was silently ignored and the objects went to default names in the working directory |
| a message body built with `python3 -c "..."` in double quotes | `ok: true` from the API | backticks in the text were run as command substitution by the shell and the words vanished before the request was built |
| `tools/check-architecture-boundaries.sh` on a tree where the manifest and the guard were edited together | green, *governance table agrees* | both had been changed the same wrong way; the layer that would have refused is `validateExecutedTestGroupManifest()`, which needs a full suite |
| a commit SHA quoted in a broadcast | a reader's `git show` | it had been written from memory rather than pasted from a command's output, and named no object |

The family is one sentence: **taking something that returned success as proof that it did the thing
you meant.** It has four faces here — text appearing in a command line read as the thing existing;
a matcher's silence read as a true zero; a zero exit status read as the requested work having
happened; and an accepted request read as the content having arrived as written. Count the outputs
before believing the status, and build any body that will carry arbitrary text with a quoted
heredoc (`<<'EOF'`, which turns off every shell expansion) rather than an interpolating string.

That last one is not a footnote for this design. **A lock record's fields are exactly "arbitrary
text assembled by a shell"** — a holder identity, a phase, a log path. A field eaten by the shell
before the line is written produces a record with a different meaning and no error anywhere, which
is how the broker lease's requests could be answered `ok` while carrying words the shell had
already removed. Both have one prescription: before believing a count, run a positive control that
must match — the third row was caught exactly that way, by checking a word known to be present and
getting 0 from `\y` and 54 from a plain pattern.

**The sixth row is the one that resists that prescription, and it is the most dangerous.** A
positive control tests one reading; this failure is *two readings agreeing*. When the sealed count
in `test.sh` and the governance table in `docs/architecture-refactor.md` are edited together, the
guard that compares them reports **green, governance table agrees** — because they agree, and they
are both wrong. Two sources concurring is the thing we ordinarily use to *raise* confidence, so it
is the last place anybody looks. The layer that actually refuses is
`validateExecutedTestGroupManifest()`, which runs inside the suite: **a green from that comparison
means the two documents match each other, not that either matches the tree.**

The discriminator is worth stating because it is not the whole guard. Its group, runner and suite
counts are computed *from the thing counted* — the manifest file, the runner list, the directory —
so those cannot agree their way to a wrong answer. The susceptible ones are the comparisons where
**both sides are documents**: `compare_documented "Swift checks"` puts `test.sh`'s sealed receipt
against the governance table, and the number they both describe exists only in a run. So: when a
guard is green, ask whether its number was compared against a *thing* or against another *record of
the thing*. Only the second kind can be unanimously wrong.

**And a third shape, which is neither, and is the one that actually happened.** A guard can compare
a thing against a record, get the right answer on both sides, and still miss the defect — because
what it counts is not everything there is to count. On 2026-09-03 a manifest edit was backed out of
a shared index; afterwards the guard counted the manifest file (498) against its own constant (498)
and both were right, while `Tests/UsageLedgerTests.swift` declared five groups that neither of them
listed. **Nothing was inconsistent. The guard simply does not read that file.** Only
`validateExecutedTestGroupManifest()` refuses, because it is the one place that compares what was
*declared* against what was *executed* — and that requires a run.

So the three are worth keeping apart, because they need different answers:

| shape | what it looks like | what catches it |
|---|---|---|
| one reading lying | a count, an exit status, an `ok` | a positive control that must match |
| two records agreeing | *governance table agrees* | comparing one of them against the thing |
| a correct count of the wrong set | everything green, nothing inconsistent | something that observes execution, not declaration |

**The middle row paid for itself twenty minutes after it was written, against the line that wrote
the classification.** A ceiling was set from a number measured on one tree and carried, through a
merge, into a statement about another: `Orchestrator.swift` was 11,932 lines on a delivery branch
that still contained the lease, and 11,678 once integrated onto a `main` where the lease was gone.
The guard's ceiling was filled in as 11,932. **That can never go red** — `-le 11932` holds for ever
against a file of 11,678 — and it silently grants 254 lines of headroom nobody measured and nobody
approved. **A ceiling carrying invented slack is worse than one that is failing**, because a failure
is an argument and slack is an absence.

It was caught because that comparison's left side is a `wc -l` the guard takes every run: a thing,
not a record, so it cannot be wrong in company with a stale number. Had that row been record against
record, editing the script and leaving the table would have left both wrong and everything green.

The third is the hardest to see because **nothing about it is wrong** — it is a scope problem
wearing an accuracy problem's clothes, and it is this repository's older rule about samples
(*a question sampled along a single path measures that path*) applied to a guard's reach rather
than to a probe's. When a guard is green, the second question after *thing or record?* is
**what does it not look at?**

It has a sibling worth naming with it: **three independent readings that agree are not corroboration
when all three were taken with the same wrong instrument** — that was measured here on a compile
peak, where two `ps` samples and a third agreed on about 3 GB against a kernel-tracked lifetime
maximum of 46.06 GiB. Agreement is evidence about the instrument before it is evidence about the
world.

**And the seventh row is the cheapest to avoid.** An identifier written from memory cannot be
repaired by its context: a reader who runs `git show` on it gets an error and then does not know
which half of the message to trust. Paste a SHA from the output of the command that produced it.

This is not a stylistic note. **The physical backstop asks "is any compiler running on this
machine".** Written with `pgrep -f`, it matches the process asking the question and therefore always
answers yes, so the lock can never be reclaimed — the permanent roadblock this design exists to
remove, rebuilt in a new place. Match the executable name exactly (`pgrep -x`, or compare `comm`),
and prove it with a test that puts an unrelated process whose arguments mention `swift-frontend` on
the machine and asserts the guard still answers *none*.

### Reading a process's identity across locales

Identity is `pid` plus process start time, because pids are reused. Start time comes from
`ps -o lstart=`, which renders through `LC_TIME`, and on a machine running `zh_TW.UTF-8` that is not
the English shape. Holding the formatter still and varying only the date, `LC_ALL=zh_TW.UTF-8 date
+%c` — the same rendering `ps` produces — gives `四  9/ 3 …`, **five** whitespace-separated tokens,
on days 1 through 9, and `一  8/31 …`, **four**, on days 10 through 31, while `LC_ALL=C` gives five
on every day. So a parser that counts fields is correct for nine days a month and wrong for the
rest, which is how the same bug was measured, declared unreproducible, and measured again.

Two rules follow. **Never count fields.** And pin `LC_ALL=C` on the writing side and the comparing
side both, so the recorded string and the one it is compared against come out of one formatter; a
writer and a reader in different locales compare unequal, the live holder reads as gone, and the
lock is handed to a second compiler.

### Liveness is proved by renewal, not by a pid existing

The first stopgap recorded a holder pid and called the lock stale when that pid was gone and no
compiler was running. A run that outlives its session is started under `nohup`, and the pid it
recorded was a `sleep 14400` sentinel adopted by `launchd`. That single fact produces two failures
pointing in opposite directions:

- **the sentinel outlives the work**, so the lock is never stale and becomes a roadblock for as long
  as the sentinel is scheduled to live; and
- the note written to work around it — *treat the run as live only while a compiler is running* —
  **makes the lock reclaimable in the gaps between the compiles of one study**, which is the
  collision the lock exists to prevent.

Both are the same cause: the liveness signal was bound to a proxy process instead of to the work.
It is also why a `trap … EXIT` cannot carry this on its own. Written in the outer shell that
launches `nohup … &` and returns, the trap fires immediately and releases a lock whose work is still
running; written in the inner shell, a killed session never runs it and the lock stays for ever.

The rule, therefore:

1. **A holder proves it is alive by renewing.** A `sleep` cannot renew.
2. **A clock on the work is wrong; a clock on the proof of life is right.** A four-hour compile is
   not stale, and a duration timeout was withdrawn for saying it was. A holder renewing every twenty
   seconds never trips a sixty-second renewal deadline however long its work runs.
3. **Admission needs both halves and the physical backstop is never waived.** A new holder is
   admitted only when the current holder has stopped proving it is alive **and** no compiler process
   exists on the machine. Either half alone admits a collision: the second alone reclaims a lock
   during a gap, and the first alone reclaims one from a holder that was merely swapped out while
   its compile still holds twenty gigabytes.
4. **Missing, stale or ambiguous evidence reads `unknown` and blocks.** It never reads *dead*.
5. **A `done_flag` is a positive signal only.** A path the run creates when its work is finished
   means the lock may be reclaimed at once without waiting on any deadline. Its **absence proves
   nothing**, because a killed run never writes one, so absence falls back to renewal and the
   backstop. Reading absence as *still running* rebuilds the roadblock somewhere new.
6. **The record names the process actually working, and refreshes it on renewal.** When the work is
   a sequence of processes — a study that runs several compiles — one static pid field cannot
   describe it, and that gap is exactly how a sentinel came to be the holder.

**This was the argument for a broker beside the lock directory**, and it is worth keeping as an
argument even though the broker is gone. A registry knows what a file cannot: an owning task that
has reached `failure`, `timeout` or `cancelled`, or a session positively gone, has stopped proving
liveness whatever a sentinel pid is doing — an answer available immediately rather than after a
deadline. It would still have been subject to the backstop: a task that is gone while a compiler
still runs is a refusal that names the orphan, not a takeover. What the night showed is that the
sixty-second deadline is a cheap enough answer to the same question that nobody reached for the
immediate one.

### The record, as it was settled

The lock is a directory, because `mkdir` is atomic, and its path is overridable so tests can drive
it. Inside it:

```
holder.txt
  holder=              a readable identity carrying the terminal id, so a blocked run knows who to ask
  pid=                 the process actually doing the work, never a sentinel; when the work is a
                       sequence of processes this names the current one and is refreshed on each beat
  owner_pid=           the run itself — what ownership is proved against, and what the renewal loop
                       supervises. It exists for exactly as long as the run does
  owner_started=       owner_pid's start identity, one normalised `LC_ALL=C ps -o lstart=` line.
                       **The one field a writer may leave empty**, meaning "not recorded by this
                       writer"; empty is unknown to every reader and is never a mismatch
  token=               this hold's unique identity, and the compare in every compare-and-swap
  phase=               compiling | analysing | idle-holding, refreshed on each beat
  phase_since=         when the current phase began, moved only when the phase itself changes
  heartbeat=           path of the beat file, which lives inside the lock directory
  heartbeat_deadline=  seconds without a beat after which this holder has stopped proving liveness;
                       a reader prefers the holder's own number to its own
  started=             when this holder took the lock
  renewed=             this record's own last refresh
  tree=                the exact tree being verified
  log=                 where this run's output is going
  done_flag=           a path the run creates when its work has finished
  work=                comma-separated pids doing the work right now
  last_compiling=      when anything last actually compiled under this lock, or `never`
  compilers=           three states: empty means this writer has no answer — it did not probe, or
                       its probe could not be read — `none` means it probed and the machine was
                       clear, otherwise the pids it found. Empty and `none` are not
                       interchangeable: `none` is a claim about the machine and empty is the
                       absence of one, so a writer that spells an unreadable probe `none` is
                       failing open in the one field written to keep them apart
  note=                what a person about to remove this directory by hand should know
beat                   the beat file itself, so it disappears with the lock rather than outliving it
```

**One record, two writers, and the list above is the whole of it.** `test.sh` and `build.sh` both
write this file and both read each other's. It was three writers while the broker lease existed,
and **the list did not move when that writer went**: nothing in the contract was the broker's
alone, which is the cleanest evidence that the record belongs to the lock rather than to the thing
that was built on top of it. The three did not agree: seventeen fields, eleven and eleven, eight in
common, and the four the shell's compare-and-swap depends on — `token`, `owner_pid`,
`owner_started`, `heartbeat_deadline` — written by nobody but `test.sh`. Against a record either of
the other two wrote, that compare was `"" = ""` and always true, so the re-read beside it was
carrying the whole swap alone. In the other direction `test.sh` wrote `working=` while the Swift
reader read `work=`, so each side showed an empty working list for the other's holder and the field
the design specifies as *the record names the process actually working* crossed in neither
direction. The contract is written out once above `clawdline_suite_lock_write_record` in `test.sh`,
and `Tests/test-sh-lock.mjs` reads every writer and fails when one of them drifts from it.

**`pid` and `owner_pid` are two different questions and only the second is an identity.** A hold is
a sequence — the compiler driver, then the test binary — so `pid` changes *during* one hold. Every
comparison that asks "is this the same holder" uses `owner_pid`, falling back to `pid` for a record
written before this contract existed.

**Takeover requires both halves, and neither is sufficient alone.**

```
(A) the beat file has not been updated inside the threshold   the holder stopped saying it is there
(B) and no swift-frontend process exists on the machine       the physical backstop, never waived
```

`(A)` replaces *is that pid alive*, and that is a real improvement: a pid is an operating-system
by-product, it is reused, and a sentinel's pid outlives the work it was standing in for. A beat is
something the holder asserts.

`(B)` is not redundant. Used **alone** it is wrong, and that error has already happened here: *no
compiler is running* reclaims the lock in the gaps between one study's compiles, which is the
collision the lock exists to prevent. Used as a **necessary** condition it guards the other
direction, which is the expensive one: a holder that is wedged or heavily swapped can miss a beat
while its compile still holds tens of gigabytes, and that is exactly the machine state of the two
reboots — load in the sixties, swap full, processes being chased by Jetsam. Taking over on an
expired beat alone would start a second compile beside a live one, which is the recipe for the
next reboot. On a healthy machine `(B)` passes instantly and costs nothing; it only ever blocks
when the holder is stuck, which is the case it exists for.

There is a second case `(B)` catches, and it is the residue of the correct beat loop below: if the
**supervisor** is killed — a session boundary, a Ctrl-C reaching the group, an OOM killer picking
it — while the compiler it spawned survives, the beats stop and the machine keeps burning. Both
cases have the same shape, *the self-report stopped while the fact continued*, and `(B)` is the
half that is betting on the fact.

> **The beat is what the holder says. `swift-frontend` is what the machine is. A machine-level
> resource guard fails closed on the fact, not on the self-report.**

**And for part of the guarded region `(B)` is vacuous, which has to be said out loud rather than
discovered.** The lock covers the compile *and* the test-binary run that follows it — deliberately,
because that run is minutes of this machine and belongs inside — and through the whole `analysing`
phase there is no `swift-frontend` anywhere. A waiter polling every five seconds through that window
sees zero compilers every time, so `(B)` is permanently satisfied and **admission there rests on
`(A)` alone**. The same holds, for shorter stretches, in the gaps between the per-file frontends of
one `swiftc` invocation.

The answer is not to narrow the region. It is that `(A)` has to be strong enough to carry it, which
is what the renewal rule below is for: a holder that cannot read the machine keeps beating and says
so, and only positive evidence that it no longer owns the lock stops it. An invariant nobody states
is one nobody can check, so it is stated here and in the script's own comment: **during
`analysing`, `(A)` is the whole of the argument.**

**A holder's proof of life is not conditional on readings that can fail.** This is the same rule as
"missing or ambiguous evidence reads `unknown` and blocks", applied to the *writer* rather than the
reader, and it was inverted there for one landing. The renewal loop used to check four conditions
per tick, three of them readings, each `|| exit 0` on a single unretried sample — so one `ps` broken
for two seconds ended the beat permanently while the run went on compiling, and a deadline later a
second run took the lock and both were inside the guarded section. Reproduced twice. Applied to a
reader, ambiguity means *block*; applied to a holder it must not mean *conclude I am no longer the
holder*, which is fail-open. So each probe answers three ways, the process-existence probe carries
its own control (it asks about pid 1 in the same call, so a missing answer is distinguishable from a
missing process), a failed record write costs a tick rather than the lock, and a loop that does stop
says which of the four conditions ended it.

**There is one way out of `unknown`, and it is positive evidence rather than an exception.**
Acquiring is `mkdir`, then a few forks, then the first record; a run that dies in that window leaves
a directory with no record and no beat, which blocks correctly and which nothing could ever clear —
only `stale` reaches the takeover, and a record that will never be written can never become stale.
One ordinary Ctrl-C therefore turned the machine's compile slot into a permanent roadblock that the
note in the directory told the next person not to remove. A directory that has **never** held a
record or a beat, is older than a whole renewal deadline, and sits on a machine with no compiler
running is not absence read as death: it is the distinct, positive fact that nothing was ever
written there. All four conditions hold or it stays `unknown`, and `(B)` is not waived here either.

**A beat has to come from something that stops when the work stops**, or it is a sentinel wearing a
new costume:

```sh
while true; do touch beat; sleep 60; done &     # wrong — this outlives the work exactly as a sleep does
```

What is wanted is the supervising loop that is already waiting on the compiler: the same loop that
`wait`s on it also touches the beat, so the beats stop when the compiler exits or the supervisor is
killed. The claim a beat makes is *somebody is still supervising this work*, not *a timer is still
running on this machine*.

```sh
TMPDIR=... ./test.sh > "$LOG" 2>&1 &
work=$!
while kill -0 "$work" 2>/dev/null; do
  touch "$LOCK/beat"
  sleep 30
done
wait "$work"; rc=$?
```

The beat is conditional on `kill -0 "$work"`, so the work disappearing ends the loop on its next
pass. The test that keeps it that way is direct: **start an independent timer touching the beat
file, kill the supervised compile, and assert the beat stops.** A timer-driven implementation
keeps beating and fails it, which is the point — otherwise the same defect is rediscovered in six
months under a new name.

**`phase` is observable and reportable, and is not a takeover condition.** `idle-holding` says *I
still need this and am not running anything right now* — the third state, which nothing in the
original record could express:

```
the holder is gone                      may be taken over
the holder is alive and using it        must be waited for
the holder is alive and not using it    may only be asked, never taken     <- invisible from outside
```

A query must therefore say who holds the lock, how long since it was last `compiling`, and how long
since the last beat, so that a waiting run knows to **ask** rather than to take, and knows that
asking is worth doing. Without those fields a queued run can only choose between waiting blindly and
taking wrongly. On the night this was written that state cost a person one manual lookup of the
holder's session to resolve, which does not scale — a query that answers only `held` is the picture
that lookup existed to replace.

**`done_flag` stays a positive signal.** Present means the work is over, so with `(B)` the lock may
be reclaimed at once without waiting on the beat threshold. Absent proves nothing, because a killed
run never writes one, so absence falls back to `(A)` and `(B)`. Missing, stale or ambiguous evidence
is `unknown` and blocks.

The tests that have to be seen failing before they are believed: a holder beating normally at
`phase=idle-holding` with no compiler running must **not** be taken over, and the query must say so;
an expired beat with a compiler still running must **not** be taken over, and the refusal must name
the orphan processes; and an unrelated process whose *arguments* mention `swift-frontend` — a
sampler, a `/usr/bin/time` wrapper — must leave the guard still answering *none*.

### Nothing in this design kills or suspends anything

The system may queue, may refuse, and may tell. It may not kill. On the night this was written a
session was asked to clear a compile belonging to somebody else and correctly refused, because that
process was producing the evidence everyone was waiting for. A refusal that names the largest
holders of memory is **information for a person to act on, not a target list**, and no route, flag
or code path in this work terminates a process it did not start.

### The compile-job ceiling, and the axis that turned out to be empty

**A correction, because the axis this was first drawn on turned out to be empty.** The degradation
ladder the lease was built to — a grant carrying a parallelism budget rather than a yes or no — was
written assuming a compile could be made smaller by asking for fewer jobs. On this
machine it cannot. Measured twice against the exact `-c -o "$BIN"` invocation — 7,479 samples at
54 ms from a walker filtering on the driver's own descendants, and 426 `ps` samples at 250 ms — the
driver never had more than one `swift-frontend` alive without `-j`, while the same instruments read
8 when `-j 8` was passed. **The default is one, so granting `-j 1` grants what already happens.**
The peak lives inside a single frontend compiling a single file, which no job count can influence.

So the ceiling is worth having as a **ceiling** — it stops a caller from multiplying the peak by
eight — and is worth nothing as a floor. That is why `CLAWDLINE_SUITE_JOBS` outlived the thing that
was going to hand it down: the knob is real, the policy that would have set it was not. **Any
degradation ladder has to be built from a measured axis, and this one had to be measured before it
could be used.**

There is a second thing that reading exposed, and it lands directly on `(B)`. During that compile a
**second `swift-driver` was running that no lock governs**, spawned by `node
Tests/keychain-rebuild-focused.mjs` — a node test inside `test.sh` itself, further down the same
script that holds the lock. A lock that guards one `swiftc` line therefore does not guard the
script it lives in. It also settles what `(B)` counts: **machine-wide, including compilers that are
not yours.** That looks like a false positive and is the definition — `(B)` asks whether anything on
this machine is burning, not whether your own work is running, so counting somebody else's compiler
is exactly its job, and missing one you spawned yourself is the failure that matters.

`test.sh` gained that injection point in `54891280`: `CLAWDLINE_SUITE_JOBS` is a positive whole
number that becomes `-j <n>`, anything else is refused, unset adds no flag at all, and the run
prints which ceiling it used and where the number came from. `build.sh` reads the same variable and
prints the same two things. Unset adds no flag because the command line then stays byte-identical to
what it has always been — not because the default is unknown: it is known, and by the measurement
above it is one.

### Where the peak actually comes from

Under test as this is written, with the measurements that exist recorded honestly rather than
rounded into a conclusion.

Whole-module `-typecheck` over all sources is cheap, and that includes the expensive file: two
independent runs measured 148 files in 59-60 s at a peak of about 250 MB, and 150 files in 65 s at
370 MB. **Type checking is not this machine's problem, and there are two readings saying so.** The
blow-up is therefore downstream of it, and
`-Xfrontend -warn-long-expression-type-checking` is **exclusionary evidence rather than a locating
tool** — an empty report from it is a result, not a failure to look.

One file dominates. `Tests/CloudAccountTests.swift` reached a lifetime maximum of **at least
23.65 GiB and was still growing** when a swap watchdog stopped the run at 133 seconds; the other
hundred-odd files in the same run each stayed under 0.27 GiB, a figure that has not yet been
reproduced and is quoted with that caveat. A freshly launched frontend compiling that one file
alone reached 8 GB and climbed, which rules out any cumulative-counter artefact: a new process
cannot inherit an earlier file's peak.

Its shape, and three controls that make the variable specific:

| file | largest function | `await` in it | expensive |
|---|---|---|---|
| `Tests/CloudAccountTests.swift` | 891 lines | 143 (and 260 `try`) | **yes** |
| `Tests/CloudOutboundSpoolTests.swift` | 56 lines, 35 functions | 213 in the file | no |
| `Tests/TranscriptTests.swift` | 995 lines | 0 | no |
| `Tests/PlannerTests.swift` | 887 lines | 0 | no |

Length is excluded by the last two, and sheer count of suspension points by the second, which has
more of them than the expensive file and spreads them across small functions. What is left is
**suspension points inside one function**: an `async` function is lowered to a state machine with a
frame holding every variable live across a suspension, and one function with 143 of them, crossed by
260 error-propagation edges, is a different object from thirty-five functions with a handful each.

A falsifiable prediction was published before the data that could answer it: if the variable is
suspension points per function, the second most expensive file must be
`Tests/CloudCommandLedgerTests.swift`. An independent scan that had not seen the prediction ranked it
second, at 640 lines and 131 `await`. **This shape appears only under `Tests/`; no function in
`Sources/` is near the top of that ranking**, so the rule this becomes is one about how tests are
written, not a warning to product code.

What remains open is *why*, and it is worth an experiment rather than an assumption, because the rule
that gets written down will be followed: the expensive file also holds twelve unstructured
`Task{}`/continuation sites where the cheap control holds none, so the comparison moves two variables
at once. Splitting the one function mechanically moves only the first, and the **size** of the drop
separates the models — with the largest remaining half at `m` suspension points out of `n`, a
per-function quadratic cost predicts `(m/n)²`, a per-function linear cost predicts `m/n`, and a cost
driven by total output predicts no change at all. Only the last of those says that splitting will not
help.

### What this means for the scheduler's own justification

Stubbing that one file out of the tree — its two non-private signatures kept, the other 147 files
compiled normally at `-j 1` — produced a peak of **0.84 GiB and 93 seconds** for the whole codegen.
A completed measurement of the file itself then replaced the lower bound it was compared against:
**46.06 GiB and 336.3 s** for that one file, cross-checked three ways — `proc_pid_rusage`'s
lifetime maximum, `/usr/bin/time -l`'s peak footprint, and the 46.04 GiB the JetsamEvent recorded
for the process it killed. The whole 148-file compile completed in 426.2 s with a single-process
peak of 46.06 GiB and the lifetime maxima of its 147 frontends summing to 59.84 GiB, of which 77%
belongs to that one file — 49 times the next worst.

The phase is downstream of everything the earlier guesses pointed at. On that file alone:
`-typecheck` 0.070 GiB, `-emit-silgen` 0.081, `-emit-sil` 0.090, `-emit-irgen` 0.104, all inside a
second; `-emit-ir` reached 37.87 GiB before it was stopped and `-c` completed at 46.06.
**The jump is in the LLVM pass pipeline, after IR generation.** So the `async` lowering is not where
the cost is spent — it is what builds the object the cost is spent on, one enormous LLVM function.
The rule that follows is about function size, and `async` is merely how a function gets that big
here; a reader told "async is expensive" would look in the wrong place.

Splitting that function mechanically into 28 small `async` sections — statement text, order,
assertion set and check count all unchanged — takes the file to **0.83 GiB and 2.7 s**. That split
is also the experiment that separates the two candidate variables, because it leaves every
unstructured `Task {}` site exactly where it was and changes only how many suspension points share
one function. With the largest section carrying 16 of the original 143, a cost linear in
suspension points per function would predict 11.2% and a quadratic one 1.25%; the measurement is
1.80%, which interpolates to an exponent of about 1.8 — 2.0 depending on which denominator is used.
**Quadratic, within the precision of a single data point — and an interpolation between two points on
one file, which is not a licence to extrapolate.** The same study's cross-file table says why:

| file | suspension points in its largest function | peak | alive |
|---|---:|---:|---:|
| `CloudAccountTests.swift` | 143 | 47,163 MiB | 330.0 s |
| `CloudCommandLedgerTests.swift` | 131 | 954 MiB | 3.1 s |
| `CloudTransportTests.swift` | 61 | 283 MiB | 1.0 s |

**Nine per cent more suspension points, forty-nine times the peak and a hundred times the time.** No
power law produces that; a quadratic would have put the 131 row near 39 GiB and it is under one. So
what the data shows is **a cliff somewhere between 131 and 143**, and the exponent measured inside
one file describes the fall on one side of it rather than a curve anybody may extend. The choice
between a 28-section split and a coarser one therefore rests on something simpler than a prediction:
**the 28-section patch is written, measured at 0.83 GiB and applies cleanly, and a coarser one is
not measured at all.**

That cliff matters more than the peak did. **`CloudCommandLedgerTests` sits just under it.** It costs
954 MiB today, so nothing marks it as a problem, and it is perhaps a dozen `await`s away from the
compile that rebooted this machine twice — a file nobody is worried about, one ordinary test addition
from 47 GiB, with no warning in between.

Which is what gives the static guard both its purpose and its constant. A ceiling of **100 suspension
points in one function** stops that file today at 131 and clears everything else by a wide margin,
because the next largest is 61. The number goes in with its reason — cliff observed between 131 and
143, threshold set at 100 for roughly thirty per cent of headroom, next largest 61 — precisely so
that the next person who wants to raise it to make a build pass has to argue with the measurement
rather than with a bare constant.

What is *not* known is which variable the cliff belongs to. The two functions differ in `try` count
as well, 260 against 179, and in closure shape. **The cliff is the data; its cause is not.** The
cheap experiment, for anyone who wants it, is to add no-op suspension points to the 131-point
function until it jumps: that locates the cliff and settles whether the variable is the await count
at all, for the price of one single-file compile under a watchdog. Until then the threshold is
conservative on purpose.

It is worth being honest about what that does to this page's own argument, and it is the strongest
single reason the lease came out. **If one file explains the peak, then fixing that file removes
most of the danger the scheduler was drawn to contain.** Two suites at a gigabyte each do not reboot
a 24 GiB machine. What survives the fix is smaller and still real: fixed build outputs that collide
whatever their size, CPU and I/O contention that makes every concurrent run slower, the promotion
and restart boundaries that were always exclusive for correctness rather than capacity, and the
next function nobody has written yet. A queue is insurance, and insurance against a hazard that has
been measured down by two orders of magnitude is bought differently — which is what a directory two
scripts agree on already is.

Which suggests the cheaper half of the remedy is a guard rather than a scheduler. The measured
shape is specific — one `async` function carrying many suspension points, with the ranking dropping
off a cliff after the top three — so a static check over the test sources can refuse a new one
before it is ever compiled, in the same place and style as the trailing-comma check that already
runs before `swiftc` does. It costs milliseconds, it names the file and the function, and unlike a
queue it prevents the problem instead of scheduling around it. **That guard now exists**, in
`tools/check-architecture-boundaries.sh`, as a ceiling on suspension points in one function, with
the cliff measured between 131 and 143 written down beside the number and the tree's next largest
function written down beside that — because a guard whose constant nobody can justify is the next
thing somebody raises to make their build pass. Read the number off the guard rather than off this
page: it was set at today's worst value while the worst offender was unrepaired and lowered to a
derived threshold once it was.

## The broker lease, while it existed

The three sections below described the half that was removed. They are kept because each one
records a defect that was found and fixed in a shipped design, and those findings outlive the
code: a queue with no liveness axis, a refusal nobody can see, and an admission ladder built on
an axis that turned out to be empty are three ways to get this wrong that the next attempt
should not have to rediscover. Nothing here is running.

### The queue proved itself the same way the holder does

Everything above is about the holder. The line behind it had no liveness axis at all: a waiter
recorded a `pid` and a `process_start` that nothing read, so **a waiter whose process died at the
head of the queue left an entry that could never be granted, never expired, and could only be
cancelled by an owner that no longer existed** — while the lock itself was free. Everybody behind it
was answered `queued_behind_others` for ever; the queue is persisted, so it survived an app restart;
and thirty-two such entries turned the whole thing into `queue_full` for the machine, which is the
same deadlock wearing a different code. `build.sh` never cancelled, so an ordinary Ctrl-C on a
waiting build was enough to produce one.

```
(A) it asked again inside the waiter deadline     the poll clock, and it needs no probe
(B) and its process has not provably gone         a strengthening, never a weakening
```

`POST /v1/orchestrator/leases` was idempotent on `request_id` and a waiting client re-sent it every
few seconds, so **asking again is to a waiter what renewing is to a holder** — and it is the
primary axis because it is a fact the broker recorded itself rather than a reading of a machine that
may not answer. The process axis only ever makes a negative faster: a pid this machine says is gone
stops the waiter at once instead of after the deadline, and a reading that could not be taken says
nothing.

Three consequences are written into the shape rather than left to be discovered:

* **An unproven waiter is passed over, never reclaimed from.** It keeps its entry, and if it starts
  polling again it is back at the front of the line, because order comes from when it *joined* and
  not from when it last spoke.
* **The depth limit counts the line, not the litter.** Entries nobody is asking for do not spend the
  machine's queue budget, which is what turned a blocked head into a 429 for everybody.
* **The array is still bounded.** Above a hard limit the longest-silent entries go, and never one
  that is still asking.

**And every answer counts as an ask, not only `queued`.** The poll clock moved when a request
joined or re-joined the line and nowhere else, so the two answers that are not `queued` did not
count as having asked: a refusal, and a decision whose effect failed. A head of the line refused for
pressure, or told `lease_changed` because the lock moved between the reading and the write, was
polling every five seconds exactly as the contract asks and was recorded as silent — passed over
after the deadline, and droppable by the trim above, by the very contention it was waiting out. So
the ask is stamped once on the decision and applied to whichever record survives the effect: a
failed effect returns the record unchanged in everything the effect would have done, and the caller
having asked is not one of those things. A refusal also stays visible for twice the waiter deadline
rather than exactly one, because the two clocks expiring together left a person looking at Bearings
at that moment with neither the request nor the reason it had been given.

Deliberately asymmetric, and worth saying why: passing over a live waiter costs it one grant, while
trusting a dead one blocks the machine until a person notices. That is why the deadline is generous
— twenty-four missed polls at `build.sh`'s rate — rather than tight.

### Refused was a state, and both projections could say it

An admission refusal left the record otherwise untouched: no holder, no queue entry, nothing. So a
session told *this Mac cannot admit even one compiler* was, in Bearings and in the Session overlay,
indistinguishable from a session that had never asked. The repair was to keep the last refusal in
the record — its code, its message, when, and who asked — and give both projections a sixth word
for it, ageing out on the waiter's clock, because a refusal is a moment rather than a state. **The
rule generalises past the code it was written in:** a state a projection cannot say is a state a
person cannot act on, and *refused* and *never asked* look identical from outside.

The freshness stamp beside it belonged to the lease, and that is the second general rule here.
Neither projection re-read the filesystem — a redraw must never start a subprocess — so what they
showed was as old as the last reconciliation, and that row carried the *task* registry's clock
instead. A lock held by a run that had died an hour before read exactly like a live compile. **A
projection's freshness stamp has to be the clock of the thing it is projecting**, which for that
row was `Record.reconciledAt`.

### Admission and exclusion are two questions, and only the first one is built

Exclusion asks *whose turn is it* and fails closed: no lock, no compile. That is what the directory
does, and it is what is left. Admission asks *may this start now, and at what size*, and the lease
answered it with a **budget** — a ceiling the holder honours, floor of one — so that low headroom
would mean a slower compile rather than a slot that never comes, with a refusal naming the deficit,
the measured quantity it was short of, how that was taken, and who was holding the memory. **No
admission decision was ever taken on this machine**: the policy shipped with no measured
per-compile peak, so every grant was the floor of one, which is what a compile does anyway. That is
the whole of what the admission half cost and produced.

## The current boundary

Clawdline already has three related protections, but none of them is a resource scheduler:

- Task `claims` reserve repository paths. They prevent conflicting roots from being briefed onto
  the same source, not two safe builds from competing for CPU and memory.
- Task `serialize` names a machine-global mutex. It is FIFO and restart-aware, but only a dispatched
  task that declared the token participates, and it holds the token for the task's whole queued,
  spawning and briefed lifetime rather than for the few minutes spent compiling.
- Restart maintenance protects terminal admission while `build.sh` replaces the running app. It
  does not govern the earlier `swiftc`, test or packaging work.

Both `build.sh` and `test.sh` invoke `swiftc` directly. A Root Session, contributor shell, CI job or
third-party tool can therefore start an expensive build without appearing in the orchestrator's
serialize queue. Private `TMPDIR` values prevent fixed test-output collisions, but they do not stop
CPU, RAM and disk contention.

The distinction is important:

| Mechanism | Question it answers |
|---|---|
| path claim | May these two tasks edit the same files? |
| correctness lock | Would these two operations corrupt or overwrite shared state? |
| capacity lease | Can this machine run both operations now without unacceptable contention? |

A complete design needs both of the last two. One global `build` mutex is safe but unnecessarily
serial; unrestricted parallelism is flexible but lets every Session make the same locally rational
decision at once.

## Goals

1. Keep interactive Clawdline, terminal and browser work responsive while background work runs.
2. Prevent destructive overlap at install, restart, signing, device and fixed-output boundaries.
3. Admit expensive work according to measured CPU, memory and I/O capacity rather than Session
   count.
4. Acquire resources only around the operation that consumes them, not around an assistant's whole
   reasoning and editing lifetime.
5. Give local scripts, dispatched tasks, Root Assignments, schedules and CI one interoperable
   protocol.
6. Coalesce identical build requests so the same tree and toolchain are compiled once.
7. Make queue position, holder, pressure, cancellation and failure visible without exporting
   machine telemetry.
8. Survive the Clawdline app being rebuilt or restarted without forgetting a live build or granting
   the same exclusive resource twice.

## Non-goals

- This is not an operating-system security boundary. An arbitrary local process can still launch
  `swiftc` outside Clawdline's wrappers.
- It does not schedule assistant tokens, decide which product feature matters most, or replace the
  task graph.
- It does not make tests parallel-safe. Test-state isolation remains a separate correctness
  requirement.
- It does not promise that two compilers are faster than one. Concurrency is admitted only after a
  benchmark proves the capacity policy on the relevant machine.
- It does not require a hosted coordinator, account, analytics service or proprietary build farm.
- It does not authorize an always-on helper, new API route or build-script change. Those are later
  implementation decisions.

## Principles

### Schedule operations, not Sessions

Ten Sessions that are reading are cheaper than one full Swift compile. Session count is therefore a
poor proxy for pressure. A Session remains free to reason and edit; it requests a lease immediately
before starting the heavy process and releases it immediately after that process has settled.

### Keep correctness and capacity separate

Correctness resources are exclusive: two installed-app promotions, two restart-maintenance owners,
or two writers to one device cannot overlap even on a large machine. Capacity resources are
quantitative: a compile may request CPU units and a memory reservation, and the scheduler may admit
more than one when policy and current pressure allow it.

An operation may require both atomically. It never holds `cpu.compile` while waiting for
`install.clawdline`, because partial acquisition would create resource-order deadlocks and waste
capacity.

### Admission is not execution

The protocol must preserve distinct receipts:

```text
requested -> queued -> granted -> process_started -> process_exited
          -> result_observed -> released
```

`granted` says capacity was reserved. Only a process receipt says the command began. A successful
exit is not proof that the caller observed or accepted the artifact. A crash between any two states
must not produce a second grant or a permanent lease.

### Preserve room for the person

The scheduler does not aim for 100% CPU utilization. Its default policy reserves capacity for the
UI, terminal automation, the browser, filesystem metadata and the person using the Mac. Memory and
thermal pressure can reduce admission below the configured maximum; they never increase it.

### Fail visibly and conservatively

A broker outage, stale holder, impossible resource request, full queue and pressure refusal have
different typed results. A script must not silently bypass coordination because the broker did not
answer. Exclusive promotion fails closed; an ordinary compile may use the documented conservative
file-lock fallback.

## Proposed architecture

### 1. A machine-local lease authority

One authority owns the queue and lease lifecycle for the whole Mac. It should not exist only in the
Clawdline app process, because `build.sh` replaces and restarts that process. Two implementation
directions remain viable:

- a small open-source helper/LaunchAgent with a local socket and durable receipt store; or
- a filesystem-first lease helper whose kernel-held locks survive the app restart and whose queue
  can be reconstructed by the next Clawdline process.

The first supports quantitative scheduling and fair queues more naturally. The second has a much
smaller installation and trust surface. Phase 0 below deliberately measures the problem before
choosing between them.

The authority is local-only. It stores resource names, owner identity, request timing, declared
units, process identity and result receipts. It does not store prompts, transcripts, source code or
credentials, and it sends nothing off the machine.

### 2. Operation-scoped clients

The scripts acquire leases at the point of use rather than relying on the task briefing:

```text
clawdline-resource run \
  --class compile.swift \
  --cpu-units 6 \
  --memory-bytes 8589934592 \
  --priority acceptance \
  -- ./test.sh --compile-only
```

The exact CLI is not settled. Its important properties are:

- the command is an argv, not a shell string;
- the request has an idempotency id and an exact owner;
- the helper launches or observes one exact pid/process-start tuple;
- signals and exit status propagate to the caller;
- cancellation releases queued work immediately and running work only after the process is
  stopped or explicitly detached;
- the lease has a bounded heartbeat/reconciliation contract rather than an arbitrary wall-clock
  timeout that can free resources while the process still runs.

Dispatched tasks may declare expected resources for planning and UI, but the execution wrapper is
the authoritative acquisition. A declaration without an acquired lease cannot be rendered as
running.

### 3. Resource classes

The initial vocabulary should be small and closed:

| Resource | Shape | Initial intent |
|---|---|---|
| `cpu.compile` | quantitative | Swift or another compiler frontend |
| `memory.heavy` | quantitative bytes | compilation, linking and large test processes |
| `io.heavy` | quantitative or single slot | archive, index and large checkout scans |
| `install.clawdline` | exclusive | installed bundle promotion |
| `restart.clawdline` | exclusive | restart-maintenance and reconciliation boundary |
| `signing` | exclusive initially | codesign and interactive Keychain access |
| `device:<stable-id>` | exclusive | one physical device or simulator target |
| `port:<number>` | exclusive | a fixed local listening port |

Arbitrary task-supplied strings are useful for correctness mutexes but unsafe for quantitative
policy. Resource classes that influence CPU or memory admission therefore need a versioned schema.

### 4. Capacity policy

Defaults must be conservative and derived from observable machine facts. A possible starting policy
is one heavy compiler at a time, at least two logical cores and 25% of memory left unreserved, and
no new heavy admission while macOS reports material memory or thermal pressure. Those figures are
hypotheses, not constants for the implementation.

Policy inputs may include:

- performance/logical core count;
- physical memory and current pressure class;
- recent peak RSS for the same job class;
- thermal state;
- whether an interactive foreground operation is waiting;
- measured throughput from controlled one-versus-two-job experiments.

The scheduler must record why a request was admitted or held. `queued` without a reason is not an
explanation.

### 5. Priority and fairness

Priority is derived from lifecycle rather than chosen freely by each assistant:

1. a user-blocking interactive operation;
2. the landing graph's one exact-tree acceptance;
3. focused compile or focused test;
4. ordinary feature implementation;
5. review, background research and scheduled maintenance.

Within a class, requests are FIFO with aging. Aging prevents background work from starving, while a
bounded interactive reserve prevents a long background queue from making the app appear frozen.
Priority changes queue order, not correctness: it never preempts an exclusive promotion already in
its irreversible phase.

### 6. Split build, test and promotion

`build.sh` currently presents one command, but the resource boundary should distinguish:

```text
compile -> package -> verify -> promote/install -> restart/reconcile
```

The expensive compile may run before an exclusive install lease exists. Promotion remains short,
atomic and exclusive. Test compilation and test execution also become distinct: a focused test can
reuse a compiled runner, while only a full execution may mint the full-suite receipt.

This follows the runner direction in `docs/verification-workflow.md`: compile once, execute focused
groups many times, and reserve one exact candidate-tree full run for the landing Root.

### 7. Build identity, caching and coalescing

A build key must include every input that can change the artifact:

```text
source/tree digest
+ compiler executable and version
+ target triple
+ flags and frameworks
+ deterministic source manifest
+ generated inputs and relevant tool versions
```

When several callers request the same key, one becomes the producer and the others become followers.
Followers observe the producer's receipts and reuse only a completed, verified artifact. A failed or
cancelled producer does not become a green cache entry. A different key never shares mutable build
output.

Caches are content-addressed and read-only to consumers. This is especially important for isolated
worktrees: sharing a writable build directory would reintroduce the interference that isolation was
created to remove.

### 8. Ownership and recovery

Every request identifies its origin when available:

- terminal-neutral Session and process-bound conversation;
- task or Root Assignment id;
- repository and exact tree;
- launched pid and process start;
- nullable graph node and landing owner.

The authority periodically reconciles the process tuple. A missing process after a complete local
observation may release capacity; stale or incomplete observation produces `unknown`, not a release.
After a restart, a durable running receipt plus a still-live exact process reconstructs the lease.
A durable grant with no process-start receipt is reconciled separately and cannot be treated as
running or silently granted again.

### 9. User-visible state

Clawdline should show, for each queued or running operation:

- resource class and units;
- queue position and wait age;
- holder and owning Feature when known;
- admission/hold reason;
- current pressure class;
- whether the request may be cancelled;
- producer/follower relationship for a coalesced build;
- last durable receipt.

The page must keep `missing`, `zero`, `unknown`, `refused` and `not yet measured` distinct. A single
spinner would collapse the very states the scheduler exists to make visible.

## Open-source and trust boundary

The scheduler is infrastructure on a contributor's computer, so its trust story must be smaller
than its convenience story:

- The complete protocol, store schema, policy defaults and CLI remain in this repository.
- The default installation works locally without a Clawdline Cloud account.
- No command text, path, pressure sample or usage history is uploaded.
- The broker accepts only local callers under an explicit authentication or same-user boundary.
- It executes only the argv supplied by the caller; it does not fetch code or interpret a shell
  program.
- Configuration is inspectable and versioned. Unknown versions fail closed rather than being
  replaced.
- A contributor can disable the optional daemon and use the conservative documented lock path.
- The feature cannot claim to control processes that bypass its wrapper. Detection may warn, but it
  must not present advisory observation as enforcement.
- macOS can be the first implementation because Clawdline is currently a macOS app, while resource
  names and lifecycle receipts should avoid unnecessary Apple-only semantics.

## Failure behavior to design before implementation

The acceptance suite needs failure injection for at least:

- authority restart after request, grant, process start, process exit and result observation;
- caller crash while queued and while running;
- command starts but the process receipt cannot be persisted;
- lease persisted but process launch fails;
- incomplete or stale process inventory;
- duplicate request ids with same and different content;
- one request needing several resources atomically;
- priority inversion and starvation under a continuous interactive workload;
- memory pressure rising between queue and grant;
- two identical build keys and a producer failure;
- cache metadata present with a missing or corrupt artifact;
- two concurrent install attempts;
- broker unavailable during an ordinary compile and during promotion;
- a direct uncoordinated compiler observed outside the wrapper;
- Clawdline rebuilding and restarting while another repository's compile remains live.

Every refusal is typed. No test should prove only that a timeout exists; it must prove unrelated
health, Session and event-stream work remains responsive while a heavy dependency is blocked.

## Phased delivery

### Phase 0 — measure and freeze the question

- Record single-build and concurrent-build wall time, peak RSS, CPU, I/O, thermal state and
  foreground Chat latency on the same exact trees.
- Identify every repository entry point that compiles, tests, signs, installs or restarts.
- Add no scheduler yet. The output is the capacity baseline and the minimum resource vocabulary.

Exit: the project can say whether two builds improve throughput, only increase contention, or vary
by job class on the supported Mac.

### Phase 1 — correctness locks at the scripts

**Built, in use, and the whole of what this machine runs.** `test.sh` takes the lock in `10130e45`;
`build.sh` takes the same directory the same way.

- Add a machine-local exclusive fallback around install/restart/signing.
- Add a conservative compile lock to `build.sh` and `test.sh`.
- Use unique output and private `TMPDIR` everywhere.
- Print the holder and typed reason instead of hanging silently.

Exit: two direct shells cannot race promotion or accidentally overlap full compiles, even when no
orchestrated task exists.

### Phase 2 — operation leases and queue visibility

**Built on 2026-09-03 and removed on 2026-09-03.** See [*Why the lease was removed*](#why-the-lease-was-removed)
at the top of this page: the exit condition below was met and none of it was used, which is a
different result from "it did not work" and is the one worth carrying forward. Anybody restarting
this phase owes an answer to the question that decided it — *who is waiting on the queue this
would make visible, and what would they do differently seeing it* — before writing the first route.

- Introduce the local lease authority and wrapper.
- Acquire only around compile, test execution and promotion phases.
- Add FIFO, cancellation, durable receipts and crash/restart reconciliation.
- Project queue/holder state into Clawdline.

Exit: task `serialize` is no longer the only coordination path, and direct project scripts use the
same machine queue.

### Phase 3 — quantitative policy

- Add CPU/memory/I/O units, pressure-aware admission, derived priority and aging.
- Prove interactive responsiveness and starvation bounds with failure injection.
- Retain a one-heavy-job policy on machines where the benchmark does not justify wider admission.

Exit: concurrency is a measured policy rather than a constant or a Session count.

### Phase 4 — compile cache and request coalescing

- Split compile from execution scope.
- Add content-addressed immutable artifacts.
- Coalesce identical build keys and preserve producer/follower receipts.
- Reuse exact receipts only for the same tree, question and environment tuple.

Exit: parallel callers asking the same question pay for one compile without weakening exact-tree
acceptance.

## Acceptance metrics

Measurements need a fixed machine, exact tree, toolchain, command and output destination. At
minimum report:

- peak simultaneous heavy compilers;
- queue wait and execution p50/p95 by class;
- total wall time for a fixed batch of builds;
- peak memory and time under pressure;
- foreground Chat open and event-stream heartbeat p50/p95 during builds;
- duplicate exact build requests and coalescing rate;
- stale/unknown lease count and recovery time;
- exclusive-operation overlap count, which must remain zero;
- cache hits tied to a complete build identity;
- starvation bound for the lowest admitted priority.

The success condition is not merely lower compile time. The system succeeds when a fixed batch
finishes no slower in aggregate, interactive work remains responsive, unsafe overlaps stay at zero,
and every wait has an observable reason.

## Decisions deliberately left open

1. Whether the durable authority is a LaunchAgent or a filesystem-first helper.
2. Whether a running low-priority process may ever be suspended, or only future work held.
3. Which macOS pressure APIs are stable enough to affect admission rather than telemetry alone.
4. How much resource declaration belongs in task JSON versus the execution wrapper.
5. Whether compile cache storage belongs to Clawdline or to a general per-language adapter.
6. What controlled benchmark justifies more than one heavy compiler on a machine.

These decisions should be made from Phase 0 evidence. Until then, the narrow safe improvement is a
script-level exclusive lock; the general design remains this proposal rather than an implied current
capability.
