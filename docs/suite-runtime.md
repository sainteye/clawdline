# Where `./test.sh` spends its time

A companion to [machine-resource-scheduling.md](machine-resource-scheduling.md), which measured how
much *memory* one compile takes and why this Mac rebooted twice over it. This one measures where the
*seconds* go, and it exists because the memory answer had gone stale without anybody noticing.

Everything below was taken on 2026-09-03: macOS 15.7.3 (24G419), `hw.ncpu` 14, `hw.memsize`
25,769,803,776 (24 GiB), page size 16,384, swift-driver 1.127.15 / Apple Swift 6.2.4, node v24.1.0.
The idle machine already carried about 6 GB of swap in use before any of it started.

## The four parts, measured

One full `./test.sh`, green, receipt `8353 checks passed`, run in a detached worktree pinned at
`d97d0afb` so no other session's edit could land inside the reading. **288 seconds.**

| part | wall | peak of one process | what that peak was |
|---|---|---|---|
| (d) manifest, architecture guard, trailing-comma scan, three Python guards, protocol vectors | 3 s | 0.115 GiB | `swift tools/generate-protocol-vectors.swift` |
| (c) 31 node suites | 129 s | 0.258 GiB | a `swift-frontend` **inside** a node suite |
| (a) the `swiftc` the machine lock exists for, 152 files | 100 s | 0.846 GiB | `Tests/CloudAccountTests.swift` |
| (b) the test binary | 56 s | 0.621 GiB | `clawdline-tests` |
| whole run | **288 s** | 0.869 GiB | most seen alive together, at t=196 s |

A second full run in the shared tree, at a commit two changes older, read 289.55 s with the same
four boundaries — so the number is the script's, not one tree's accident.

**The first surprise is line (c).** 129 of the 288 seconds are spent before the compile the whole
machine lock is built around has started. And the fifteen browser-contract suites, the obvious
suspects because there are fifteen of them, are **5 seconds of it**. Two suites are 119:

| suite | wall | what it is |
|---|---|---|
| `Tests/test-sh-lock.mjs` | 70 s | ~20 scenarios of the lock protocol, each waiting out a real deadline |
| `Tests/keychain-rebuild-focused.mjs` | 49 s | five small `swiftc` compiles **plus a whole-`Sources/` `-typecheck`** |
| `Tests/app-onboarding-focused.mjs` | 3 s | one focused compile |
| the other 28 | 7 s | |

**The second surprise is inside that 49 s.** `Tests/keychain-rebuild-focused.mjs` runs
`xcrun swiftc -typecheck` over all of `Sources/` — 104 files on `d97d0afb`, 103 after the broker
lease was removed from the tree. The per-process sampler caught the same
files being compiled twice inside one run, named by the `-primary-file` each frontend was given:

| file | in the node suite | in the main compile |
|---|---|---|
| `QuestionSteps.swift` | 0.258 GiB at t=44 s | 0.261 GiB at t=172 s |
| `Orchestrator.swift` | 0.233 GiB at t=37 s | 0.248 GiB at t=160 s |
| `Settings.swift` | 0.114 GiB at t=52 s | 0.182 GiB at t=183 s |
| `RemoteServer.swift` | 0.103 GiB at t=49 s | 0.180 GiB at t=177 s |

It is a second compile of the same sources, at one job, and unlike the main one it is **not inside
the machine lock**.

## What `-j` is worth now, which is not what it was worth

`test.sh` passed no `-j`, and the driver's own default on this Mac is one job. That was measured, it
is still true, and it *was* the right default: `Tests/CloudAccountTests.swift` peaked at 46.06 GiB in
one frontend, and multiplying that by N is what force-rebooted this machine twice.

`a97fb176` split that function into twenty-eight coroutines and took the file to 0.83 GiB. **The
limit went and the decision it justified stayed.** Re-measured, all five values on the same pinned
worktree, `/tmp/clawdline-suite.lock` held across the whole sweep, watchdog armed at 8 GiB and never
fired:

| `-j` | compile wall | speed-up | peak of one frontend | most alive together | the N largest, if they ever coincided |
|---|---|---|---|---|---|
| 1 (the old default) | 102 s | 1.00× | 0.845 GiB | 0.861 GiB | 0.845 GiB |
| 2 | 53 s | 1.92× | 0.822 GiB | 0.949 GiB | 1.216 GiB |
| 4 | 34 s | 3.00× | 0.852 GiB | 1.028 GiB | 1.768 GiB |
| 8 | 24 s | 4.25× | 0.835 GiB | 1.181 GiB | 2.521 GiB |
| 14 | 18 s | 5.67× | 0.833 GiB | 2.012 GiB | 3.528 GiB |

**The per-frontend peak does not move with N**, because it is one file and it costs what it costs
whoever compiles it. What grows is how many are alive together, and it grows far slower than N: the
driver hands each frontend a batch, and the expensive file is only ever in one of them. Eight jobs
cost 1.18 GiB together, not eight times 0.85.

`/usr/bin/time -l`'s `maximum resident set size` read 1.005–1.038 GiB across all five runs — flat,
as it must be, since it reports the largest single child and that child is the same file every time.
It is 20% above the footprint figure beside it because RSS and `phys_footprint` are different
quantities; Jetsam acts on the second.

**Every value from 1 to 14 is safe on this machine, and the safety argument is the last column.**
The measured concurrent figure is what actually happens; the last column is what would happen if the
batching that makes it impossible stopped working. Headroom on this machine with nothing compiling,
taken from `vm_stat` the same afternoon — 159 MB free plus 4,326 MB file-backed — is **4,485 MB**, so
`-j 8` clears its own worst case by 1.9 GiB and `-j 14` clears it by under one. That gap is the whole
reason the default settles on 8 rather than 14, alongside 14 being worth only six seconds more. And
nothing in this table is within a factor of ten of the 46 GiB that caused the reboots.

The headroom figure is worth taking again rather than quoting: `machine-resource-scheduling.md`
records 7.55 GB for the same quantity at 02:17 on the same machine, and both readings are correct.
It moves by 3 GB over an afternoon.

### The second compiler scales too

The same sweep against the whole-`Sources/` `-typecheck` inside `keychain-rebuild-focused.mjs`,
104 files:

| `-j` | wall | peak of one frontend | most alive together |
|---|---|---|---|
| 1 | 34 s | 0.260 GiB | 0.276 GiB |
| 2 | 25 s | 0.261 GiB | 0.274 GiB |
| 4 | 13 s | 0.259 GiB | 0.375 GiB |
| 8 | **7 s** | 0.261 GiB | 0.721 GiB |
| 14 | 8 s | 0.262 GiB | 0.909 GiB |

Eight is the floor here; fourteen is slower. Type checking was never this machine's memory problem —
a quarter of a GiB, three readings agreeing — which is the same conclusion
`machine-resource-scheduling.md` reached from the other direction.

### The 27 seconds that went back

The typecheck row above — 49 s to 22 s — was bought by letting a compile **outside the machine lock**
run eight-wide, and that is the one thing the lock exists to prevent.

`test.sh` takes `/tmp/clawdline-suite.lock` at `clawdline_acquire_suite_lock`, and everything above
that line runs unrationed. Two node suites compile up there, and the comment beside the lock markers
says why they were left outside it: their compiles are *seconds long*, and moving the lock above them
would make every queued run wait out a dozen unrelated node suites first. **That trade was written
down as a fact rather than as an intent**, so the next reader past it — the author of this page —
read "not even inside the machine lock" as a reason to speed it up rather than as a reason to be
careful. Seconds-long is not the same object as eight `swift-frontend` processes competing with
whoever currently holds the lock, and this machine was measured with nine of them alive beside a
lock holder the same afternoon.

So the coupling is gone, and not by capping the shared number at one — **by making it unreachable**:

| | before | after |
|---|---|---|
| `clawdline_acquire_suite_lock` | line 1222 | line 1140 |
| the marked compile-ceiling block | line 177, at the top | line 1169, **below the lock** |
| `CLAWDLINE_COMPILE_JOBS` exported to children | yes | the variable no longer exists |
| the out-of-lock typecheck | `-j 8`, inherited | `-j 1`, stated in its own file |

Above the lock the ceiling is not *restricted* to one job; it has not been defined yet. That turns a
rule somebody has to remember into a variable that is not in scope, which is why this is a structural
fix rather than a policy one.

**`build.sh` already had this shape** — its own ceiling block sits below `clawdline_lease_acquire` —
so what the original change actually broke was a symmetry between the two scripts that nobody had
written down. Both are asserted now.

The cost is the typecheck going back from 7 s to 34 s: **27 seconds, once per run**, measured in the
sweep above. It buys back the property that only one compile at a time is wide on this machine.

## `build.sh` is a different compile, and it was measured separately

Nothing above covers it. `build.sh` compiles the production sources with `-O`, and `-O` is where the
LLVM pass pipeline runs — the phase that reached 46 GiB on the old `CloudAccountTests`. A default
proposed for it from `-Onone` readings would be a guess wearing someone else's evidence.

103 production sources, same instruments, same held lock, on the tree that removes the broker lease:

| `-j` | wall | peak of one frontend | most alive together |
|---|---|---|---|
| 1 (the old default) | 169 s | 0.430 GiB | 0.445 GiB |
| 4 | 54 s | 0.408 GiB | 0.837 GiB |
| 8 | **37 s** | 0.400 GiB | 1.336 GiB |
| 14 | 33 s | 0.410 GiB | 2.064 GiB |

**Every frontend here is about half of what one costs in `test.sh`, and that is not a property of
`-O`.** The expensive files are the test suites, and this compile has none of them. So `-O` being
the phase that once blew up says nothing about how much room this compile needs: it needs less than
the other one. Fourteen buys four seconds over eight and spends every core to do it.

This became worth measuring rather than academic because the broker heavy-compile lease was removed
from the Swift tree while this work was in flight. `build.sh` used to get a `budget.parallelism`
from it; with the lease gone, `CLAWDLINE_SUITE_JOBS` is its only source and unset meant no flag,
which on this machine is one job. It now derives the same `min(8, hw.ncpu)` as `test.sh`.

**One rule, deliberately written twice.** Both scripts carry the same marked ceiling block, and
`Tests/test-sh-lock.mjs` lifts both and drives them against the same five stand-in `sysctl`
readings — sixty-four cores, one core, an answer that is not a number, a `sysctl` that will not
answer, and seven — asserting they give the same five numbers. A behavioural identity rather than a
textual one, because the two blocks print different sentences and always will. Sharing one file was
the other option and it loses more than it saves: the guard machinery here runs a lifted block from
a temporary directory, where a relative `.` finds nothing, and a block that cannot be run on its own
is a block nothing checks.

## The axes that were measured and turned down

A "not worth it" with a number is a result. Three of these are.

**Running the node suites concurrently with each other: 8 seconds.** The 29 compiler-free node
suites take 79 s in sequence and 71 s at eight-way parallelism, zero failures. It cannot go below
73 s, because `test-sh-lock.mjs` alone is 73 of the 79 and is sequential inside itself: its scenarios
share one stand-in lock directory, so making them concurrent is a rewrite of a concurrency test,
which is the worst possible place to introduce flakiness for eight seconds.

**Running the node block concurrently with the compile: worth 100 s before `-j`, and 24 s after.**
The block is safe to overlap in principle — `test-sh-lock.mjs` uses an isolated lock directory and a
stand-in compiler name (`lockprobe<pid>`, `Tests/test-sh-lock.mjs:144`), so it never sees the real
lock or a real `swift-frontend`. But its deadlines are one and two seconds, and the thing it would be
overlapped with is eight saturating compilers. Once `-j` lands, the compile is 24 s and the whole
prize is 24 s of overlap bought with a timing test run under maximum load. Turned down on that
arithmetic, not on principle; if the compile ever grows back, the arithmetic changes.

**A compile cache or `-incremental`: turned down, and not only on the `AGENTS.md` rule.** The rule
("no cache between runs") is the smaller reason. The larger one is that this working tree is shared
by several sessions that edit it continuously — two commits landed *inside* the first measurement run
in this document — so a cache keyed on file contents would be invalidated by other people's work at
unpredictable times, and one keyed on anything weaker would make a green run mean nothing.
`machine-resource-scheduling.md` also records an earlier `-output-file-map` attempt that exited 0,
produced objects, and had silently ignored the map. That failure mode — success reported for work
that did not happen — is the one a cache brings back.

**Sharing compiled products between `build.sh` and `test.sh`: not possible as the two are written,
and the measurement says the prize is smaller than it looks.** They do overlap: `build.sh`'s
production sources are all but one of `test.sh`'s non-test files. But `build.sh` compiles with `-O`
and `test.sh` compiles with no optimisation flag at all, so no object file is interchangeable
between them. Both also compile straight to a linked binary with no intermediate object output to
share. Making them agree means either shipping an unoptimised app or paying `-O` in every test run.

The `-O` table above is what turns that from a trade-off into a decision: at `-j 8` the whole
optimised production compile is 37 seconds. Even if the sharing worked perfectly it could save at
most that, once, and only when a build and a test run happen back to back — against a change that
has to make the two invocations agree on an optimisation level, which is a product decision rather
than a speed-up. Splitting the code into modules with separate `.swiftmodule` outputs is the real
version of this idea, and it belongs to the refactor line in
[architecture-refactor.md](architecture-refactor.md), not here.

## What landed, before and after

Two changes, both to how many jobs a compiler is allowed, neither touching what is checked. The
suite's receipt is `8353 checks passed` on both sides of the pair, and both runs are the same
detached worktree pinned at `d97d0afb`.

`test.sh`'s compile ceiling now derives `min(8, hw.ncpu)` when `CLAWDLINE_SUITE_JOBS` is unset,
instead of adding no flag. **The half of that change which shared the ceiling with a second compile
was wrong and has since been taken back — see "The 27 seconds that went back" below.** As it first
landed, the block also moved to the top of the script and exported what it settled as
`CLAWDLINE_COMPILE_JOBS`, so the typecheck inside `keychain-rebuild-focused.mjs` read the same
number. The table below is that version; it is kept because the two rows it explains are still the
measurement, and because a page that quietly rewrites its own history is worth less than one that
says which part of it did not survive.

| part | before | after |
|---|---|---|
| (d) guards | 3 s | 3 s |
| (c) node suites | 129 s | 101 s |
| — of which `keychain-rebuild-focused.mjs` | 49 s | 22 s |
| — of which `test-sh-lock.mjs` | 70 s | 71 s |
| (a) the main compile | 100 s | 19 s |
| (b) the test binary | 56 s | 60 s |
| **whole run** | **288 s** | **183 s** |

Peak of one process went 0.846 → 0.833 GiB, which is the same file costing the same thing. Most
alive together went 0.869 → 1.536 GiB, which is eight jobs instead of one and is the price. The
binary's four extra seconds are machine load: nothing in either change touches it, and if anything
that makes 105 s the conservative reading of the saving.

**The guard that could not fail is worth recording, because it passed.** Scenario 12's first draft
read `CLAWDLINE_COMPILE_JOBS` back in the same shell that set it, where a variable that was merely
set is indistinguishable from one that was exported. The mutation deleting `export` passed all 171
checks — while the reader the line exists for, a typecheck in a child process, would have received
nothing. It now reads through `bash -c`, and the same mutation fails it. Every other new check was
driven red before it was believed: the cap removed (3 red), `sysctl` ignored for a constant (4), the
non-numeric branch removed (1), the flag never reaching the array (several), and the typecheck's
flags computed correctly but never spread into the argument vector (1).

## What is left after all of it

`test-sh-lock.mjs` at 71 s and the test binary at 60 s are now 72% of a 183-second run, and neither
is addressed by any axis above. Both have since been broken down, with the same instrument turned
inward: `group(title) { body }` prints `  ✓ title` *after* the body, and the binary's stdout is
line-buffered by `Tests/TestIsolation.swift`, so timestamping each line gives per-group wall time
without editing anything. The same trick works on the node suite, whose checks print one line each.

### The binary's minute is twenty groups

510 group lines, 509 of them timed — the first has no predecessor to subtract — summing to
50.9 s of a 51 s run, so nothing is unaccounted for.

| | |
|---|---|
| the single largest group | **15.57 s — 31% of the whole binary** |
| top 5 groups | 28.4 s (56%) |
| top 20 groups | 45.0 s (88%) |
| the remaining ~489 | about 6 s |

The largest is `the SQLite analytics bound belongs to the complete matching query`. **The first
account of it on this page was wrong and is corrected here rather than edited away.** It said the
time was fixture construction, because the group opens by inserting 120,000 rows through the
production SQLite schema and that is the conspicuous thing in it. Timestamping inside the group, in
a full 8,093-check run, says otherwise:

| phase | seconds |
|---|---|
| opening the store, migrating it, and inserting all 120,000 rows | **0.30** |
| the narrow-month queries and their assertions | 1.40 |
| one unfiltered query, which must scan to the bound | **5.95** |
| the CSV export that has to be refused with a typed 413 | **5.97** |
| the narrowed export that succeeds | 1.86 |
| tearing the fixture down | 0.00 |
| total | 15.37, against the 15.57 the outside instrument measured |

So the fixture is 2% of it and **13.8 seconds are three operations that each scan to
`UsageLedger.maxScannedRows`, which is a hard-coded `100_000`**. That is not waste: the two
six-second ones are exactly what the group exists to prove — that an over-bound query reports itself
partial, and that an over-bound export is refused rather than served. They have to reach the bound.

Which makes the remedy the same one, for a better reason. Every assertion here is about the
*behaviour at the bound*, not about a hundred thousand of anything: `rowCount` 20,000 and 1,667,
`scanTruncated` true then false, a 413 then a 200. **If `maxScannedRows` were injectable, a fixture
of about 1,200 rows against a bound of 1,000 would exercise identical code paths and identical
assertions, and the 13.8 seconds would be about 0.14.** The whole group would fall under a second.
The change is one `static let` becoming settable for tests, in `Sources/UsageLedger.swift`.

**A second reading worth carrying out of this:** refusing that export takes 5.97 seconds. The 413 is
decided after the scan, so on a real store past the bound a person asking for a CSV waits six
seconds to be told they cannot have one. That is a product latency, not a test cost, and it is
recorded here because this is where it was measured.

**The 3.53 s group beside it is not a candidate and it is worth saying why**, because from a
profile the two are indistinguishable. `a 60k-row SQLite-to-DTO usage query stays bounded and measured` times its
own query, asserts `seconds < 10`, and prints a `USAGE_ANALYTICS_PERF` receipt. Its fixture *is* the
subject. Shrinking it would leave the check count untouched and delete the measurement, which is the
precise shape of change this page exists to refuse.

### The lock guard's seventy seconds are idle, and that is measurable

`Tests/test-sh-lock.mjs`: a 74.5 s span, of which **70.8 s sits in gaps longer than 0.8 s**. Not one
pathological scenario — about twenty of them, each honestly waiting out a deadline it is there to
prove. The four longest are 8.09 s, 7.88 s, 6.79 s and 6.40 s.

**An earlier section of this page turned down parallelising this file on the ground that a timing
test run under load would go flaky. That was an assumption, and it does not survive being measured.**
The file already isolates itself per process — its own `mkdtemp` scratch, its own `lockprobe<pid>`
stand-in compiler name — so four whole-file runs were started at once on a machine already at load
average 9. **All four finished in 74 seconds, the same as one alone, all green at 164 checks, zero
reds.** Four runs costing what one costs is the definition of idle waiting, and green under 4×
concurrency is evidence the deadlines are not fragile.

So the prize is real and large: about 60 seconds off a 183-second run, a third of it. What stops it
being cheap is not risk but shape. The scenarios are sequential inline code inside one `try` block,
sharing one scratch directory, one stand-in lock directory and one block scope, driven by
**synchronous** `spawnSync`. Running them concurrently means either making twenty scenarios async or
sharding them across processes, and both need the cross-scenario variable scope of a 1,400-line
concurrency test established first. That is a piece of work with a number attached to it now, which
is what it did not have before.

## The instruments, and how to repeat this

Three, deliberately, because two of the errors in `machine-resource-scheduling.md` were a single
instrument believed on its own.

- **Section boundaries** come from `bash -x` with `PS4='+ $SECONDS '`, which needs no edit to the
  script being measured. `$EPOCHREALTIME` is not available: `/bin/bash` here is 3.2.57, and the first
  attempt died on `unbound variable` under `set -u`. `$SECONDS` gives one-second resolution, which is
  a hundredth of the shortest part being measured.
- **Footprint** comes from `proc_pid_rusage(pid, RUSAGE_INFO_V4, …)`, sampled every 50 ms over the
  descendants of the run — never machine-wide, because a machine-wide count of `swift-frontend`
  counts other sessions' compilers, and this repository's own suite spawns a second driver.
  `ri_lifetime_max_phys_footprint` is monotonic, so polling it can only miss growth after the last
  read. `ri_phys_footprint` summed across live processes is the concurrent total; it is a *sample*
  and is reported as a lower bound, never in the same column as a peak.
- **`/usr/bin/time -l`** wraps the whole thing as an independent reading of the largest single child.

The sampler was proved before it was believed, with three controls: it read 1.502 GiB against a
process told to touch 1,536 MiB; it reported one process, not two, while a second allocator of
1.0 GiB ran outside the tree; and with a limit of 1.2 GiB against two 0.75 GiB children it fired,
killed the tree, and left no survivors. A watchdog that has never been seen to fire is not a
watchdog.
