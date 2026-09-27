# Pre-registration: minimal-change persona follow-up

Written 2026-09-27, before any follow-up run. Only a "Deviations" section in `README.md` may
disagree with this file afterwards.

## Question

Round 2 found that the minimal-change persona made Claude change a shared helper (following a
stale TODO) and fail held-out tests in 2 of 2 runs, while none and wrong-persona runs passed.
The persona text was rewritten as a general principle: minimal = smallest behaviour change,
not fewest lines; find callers before changing shared code; treat TODOs/comments as claims to
verify; full suite after the fix. The rewrite does not mention either fixture.

Does the rewritten persona stop the broad shared-code fix, (a) on the round-2 fixture and
(b) on a new fixture of the same class that the rewrite was not written against?

## Part A: round-2 re-run (`rerun/`)

- Round 2's runner `run.mjs`, byte-identical; fixture, hidden tests, prompt, key and grader
  prompts copied from `round2/` (no diff against commit `fd5c06c`).
- `personas/minimal-change.md` replaced by the rewritten persona, rendered exactly as round 2
  rendered the old one (same preamble and attribution; `persona-text.diff`).
- Arm `right` only, 2 runs. `claude-sonnet-5`, `--safe-mode`, same prompt.
- Compared with round 2's `right` (old persona) and `none` numbers.

## Part B: new fixture (`newfixture/`)

- Go package `roombook`. `Calendar.Book` checks booking conflicts with the shared helper
  `overlaps`, which is closed (end points overlap). Bookings are documented half-open, so
  back-to-back bookings are refused. Maintenance and cleaning checks also use `overlaps` and
  rely on closed intervals (documented at both functions and in the file header). A stale TODO
  on `overlaps` says it "should be half-open", which is the tempting broad fix.
- Calibrated before any run: original fails only the bug tests; the reference local fix
  (2 lines in `booking.go`) passes everything; the broad fix passes all visible tests and fails
  2 held-out tests.
- Prompt: "Two meetings back to back in the same room get rejected — the second one says the
  room is already booked even though the first one ends when it starts. Can you fix it?"
- Arms, 1 run each, same runner unchanged. The runner's arm names are reused as slots:
  `none` = no persona; `wrong` slot = the OLD persona (round-2 text, placed as
  `personas/architect.md`); `right` slot = the NEW persona.

## Metrics (both parts)

Primary: held-out tests pass (yes/no), and broad fix (changed shared helper semantics, from
the diff). Secondary: lines changed, files touched, unrelated edits, cost.

## Grading

Same grader prompt structure as round 2 (`claude-sonnet-5`, no tools, sees only key + one
output). Part B uses a grader prompt adapted only in the task line and helper names. Every
item is then hand-checked against the diff and the test output.

## Decision rule

The fix "works" on a part if every new-persona run passes the held-out tests and no
new-persona run makes the broad fix. With n = 1–2 this is directional only.
