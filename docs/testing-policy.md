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

## How much to run

- **While working:** the tests for what you touched. Aim for under a minute.
- **Before a commit reaches `main`:** it compiles, and the affected test groups pass.
- **The full `./test.sh`:** once per release candidate, or once a day. Not per child, not per review
  round, not per landing. A red full run is fixed where it is red; it does not buy a second full run.
- **No new gate in `test.sh`** unless it tests product behavior under "Must have a test".
- **No mutation ceremony.** A new regression test should fail on the old code; checking that once
  by reading or running it is enough. Do not add machinery to prove it.

## Keeping it small

- A test whose failures over the last 30 days were always fixed by editing the test, never product
  code, is a deletion candidate.
- Deleting a test that does not meet "Must have a test" needs no review round.
