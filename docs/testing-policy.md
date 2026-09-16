# What gets a test, and how much to run

This page decides what deserves a test in this repository and how often the suite runs. Where
`AGENTS.md`, `docs/verification-workflow.md`, `docs/landing.md`, `docs/suite-runtime.md`,
a dispatch policy or a task brief asks for more verification than this
page, **this page wins**.

## Why it exists

Measured on 2026-09-15 over the month since 2026-08-15:

- `Tests/` and `test.sh` gained 162,015 lines; `Sources/` gained 174,755. Writing tests cost about
  as much as writing the product.
- 644 of 1,481 non-merge commits touched neither `Sources/` nor `Resources/web/app/js/`.
- The last six GitHub CI runs were all red, so CI was not stopping anything.
- The defect found that day (the 7d quota on the web page flipping between 25%, 45% and 64%) sat
  between two programs and several writers of one file. Mac-side quota tests were green, because
  that code was right. It was found by sampling the live file, not by a test.

Most of the effort was going into guarding the process (receipts, counts, seals, documents agreeing
with each other, guards proving guards) rather than into catching product regressions.

## The rule

**Write a test when a regression would reach a person silently, and running the code can show
it cheaply.** Otherwise do not write one.

## Must have a test

1. **A bug a person actually hit.** One regression test that runs the fixed path and fails on the
   old code. One test, not a new suite.
2. **Logic whose wrong answer is silent.** Parsers of formats we do not own (Claude transcripts,
   Codex rollouts, status-line JSON, `git`/`tmux` output), quota and availability math, protocol
   encoding and cross-runtime vectors, ordering/idempotency/dedupe rules, and anything that deletes
   or overwrites a person's data.
3. **Security boundaries.** Token and pairing checks, authorization decisions, encryption envelopes.
4. **Reading old durable state.** A migration or a reader of an on-disk format an older build wrote.

## Does not get a test

1. **Source text.** Greps over Swift, JS or docs for a spelling, a header name, a literal, a MARK
   or a banned call. The one exception is a scan that stands in for a compiler we really ship with
   (for example Swift syntax the older Linux/CI toolchain cannot parse).
2. **Counts and seals.** Expected totals of passed checks, line-count or file-size ceilings,
   generated manifests and governance tables, reliability seals, receipt files.
3. **Documents agreeing.** README parity, "the docs mention X", changelog route lists, UI labels in
   docs, instruction topology or coverage indexes.
4. **Tests of the test machinery.** "This guard can go red" proofs, suite rosters, `test.sh` lock
   and streaming internals, run-progress file producers.
5. **Process records.** Landing records, task receipts, broker bookkeeping. They are workflow facts,
   not product behavior; a workflow tool that matters gets a small behavior test of the tool itself,
   not a gate in the product suite.
6. **Visual layout and wording.** Look at the running app or a screenshot instead.
7. **Glue the compiler already checks,** or a change a single manual run shows working.

**Machine protection is not a test and stays.** The compile lock and the per-function
suspension-point limit in `test.sh` exist because a compile took 46 GiB and rebooted this 24 GB Mac
twice on 2026-09-03. Keep checks of that kind; do not add product assertions to them.

## Bugs between programs: observe first

For a defect that crosses processes (Mac ↔ web ↔ Cloud, a writer and a reader of one file, several
sessions at once), reproduce it on the real app or the real file first: sample it, run it, read what
it actually holds. Fix it, then add **at most one** test at the narrowest seam that reproduces what
was observed. Do not answer an integration bug with a pile of unit tests around the half that was
already right.

## The order of a delivery: reviewed first, tested once

This is the order for every delivery unit in this repository, and it replaces any earlier sequence
that put a proof in front of the review or a reviewer after a correction.

1. **The review comes first and runs nothing.** When the work is complete, one review reads it and
   answers whether the design is right: the gap, the wrong shape, the case nobody handled, the risky
   decision nobody stated. It does not run the suite, does not wait for one, and does not ask for a
   test receipt. A reader is not there to execute code. Whether that reader is an independent session
   or the owner reading their own diff follows the risk rule in
   [`verification-workflow.md`](verification-workflow.md).
2. **One correction pass answers the whole finding set,** and it does not go back to the reviewer.
   **There is one review per delivery, and that was it.**
3. **The tests run once, at the end** — when the work is about to be committed, or built into a
   release. A commit runs the affected groups; a release candidate runs the full `./test.sh`. That
   single run is the only scheduled one in the whole delivery.
4. **A red run buys one correction, then the same run again.** It never reopens the review. If two
   corrections have not cleared it, stop and say so to the person waiting instead of grinding on.

The reason for this order: a reader finds design faults a green suite cannot, and a suite run before
the review is a receipt for code that is about to change. The run that happens last is the only one
whose result describes what actually ships.

## How much to run

- **While working:** nothing scheduled. Run something when you need an answer only it can give.
- **At the commit or the release build:** the one run above — affected groups, or the full suite for
  a release candidate. It compiles either way.
- **Never:** a run per file, per assertion or per finding; a run to reconfirm a green somebody else
  already has; a run because ownership moved; a second full run for an unchanged tree.
- **No new gate in `test.sh`** unless it tests product behavior under "Must have a test".
- **No mutation ceremony.** A new regression test should fail on the old code; checking that once
  by reading or running it is enough. Do not add machinery to prove it.

## A test is written for a regression, not for the process

Before writing a test, name the regression it catches and how that regression would reach a person.
If you cannot name one, do not write the test. Tests written to satisfy a procedure are the ones that
later go red for reasons that have nothing to do with the product, and somebody pays for that every
time it happens.

## Keeping it small

- A test whose failures over the last 30 days were always fixed by editing the test, never product
  code, is a deletion candidate.
- Deleting a test that does not meet "Must have a test" needs no review round.
