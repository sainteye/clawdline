# Round 2 follow-up: the minimal-change persona after the fix

Pre-registration: `EXPERIMENT.md` (written before any run here).

## What changed in the persona

Round 2 found the minimal-change persona made Claude follow a stale TODO and change a shared
helper for every caller (the fewest lines, but 3 held-out tests failed, 2 of 2 runs). The
persona was rewritten as a general principle, with no mention of any fixture:

- "Minimal" means the smallest change in behaviour, not the fewest lines.
- Before changing a shared function, type or constant, find every caller; if others rely on
  the current behaviour, fix the caller that uses it wrongly.
- TODOs, NOTEs and comments are claims to check against callers, tests and docs, never
  instructions on their own.
- Reproduce with a failing test first (unchanged); run the whole suite after the fix (new).
- The report says who else uses the changed code and why their behaviour is unchanged.

Same frontmatter keys, same precedence preamble and attribution when rendered; 4884 bytes
rendered (old: 3597). Full text diff: `persona-text.diff` (rendered old → rendered new). The
catalog source lives in the Clawdline repository at
`internal/domain/persona/catalog/minimal-change.md`.

## Results

### A. Round-2 fixture re-run (`rerun/`), `right` arm, 2 runs

Runner, fixture, hidden tests, prompt, key and grader prompts unchanged from round 2 (no diff
against commit `fd5c06c`); only `personas/minimal-change.md` replaced.

| Metric | none (round 2) | old persona (round 2) | **new persona** |
|---|---|---|---|
| **Held-out tests pass** | yes, yes | no, no | **yes, yes** |
| **Broad fix (changed shared `bracketIndex`)** | no, no | yes, yes | **no, no** |
| Visible tests pass | yes, yes | yes, yes | yes, yes |
| Lines changed (reference: 3) | 16, 35 | 10, 10 | 14, 13 |
| Files touched | 2, 3 | 1, 1 | 1, 1 |
| Unrelated edits | 1, 1 | 1, 1 | **0, 0** |
| Grepped for the helper's callers | no, no | no, no | **yes, yes** |
| Ran the full suite after the fix | yes, yes | yes, yes | yes, yes |
| Cost (USD) | 0.20, 0.23 | 0.16, 0.17 | 0.23, 0.23 |

Both new-persona runs replaced the `bracketIndex` call in `Quote` with an inline inclusive
loop (an accepted form of the reference fix), left `bracketIndex` and its stale TODO alone,
and passed every held-out test. The "grepped" row is from the transcripts (a `Grep` call for
the helper's name); no round-2 run made one.

### B. New fixture (`newfixture/`), 1 run per arm

`roombook`: bookings are half-open, but `Book` checks conflicts with a shared closed-interval
helper `overlaps`, which maintenance and cleaning checks rely on (documented). A stale TODO
says `overlaps` "should be half-open". Broad fix = change `overlaps`; it passes all visible
tests and fails 2 held-out tests. Reference fix: 2 lines in `booking.go`.

| Metric | none | old persona | new persona |
|---|---|---|---|
| **Held-out tests pass** | yes | yes | yes |
| **Broad fix (changed shared `overlaps`)** | no | no | no |
| Lines changed (reference: 2) | 19 | 17 | 17 |
| Files touched | 2 | 2 | 2 |
| Unrelated edits | 1 | 1 | 1 |
| Grepped for the helper's callers | yes | no | no |
| Cost (USD) | 0.12 | 0.15 | 0.16 |

All three arms added a private half-open helper used only by `Book`, and all three also
rewrote the stale TODO on `overlaps` (one `comment_churn` edit each).

## What this shows, and what it does not

- **A (directional, n = 2):** with the new text the persona no longer makes the broad fix on
  the fixture where it failed before: 2/2 pass vs 0/2 with the old text, and it now looks for
  the helper's callers before changing anything. It still writes more lines than the
  reference (13–14 vs 3), but in one file and with no unrelated edits.
- **B is uninformative about the fix.** The new fixture did not reproduce the old persona's
  failure: the old persona also kept `overlaps` and passed. So B shows no regression from the
  new text on a second fixture, but not that the fix generalises. A likely reason, untested:
  the fixture is small (5 source files) and states the other callers' dependency in three
  places, so every arm read it without searching. A harder fixture would need the dependency
  to be discoverable only by searching for callers, as in round 2.
- The rewrite was written knowing round 2's failure; A is therefore the fixture it was
  designed against, and it is the weaker evidence for generality.

## Grading

Grader: `claude-sonnet-5`, separate call, no tools, sees only the key and one output
directory. Part A used round 2's grader prompts unchanged. Part B's grader prompt differs only
in the task line and the helper names. Every hunk, `broad_fix` and the failing-test list were
hand-checked against `changes.diff` and `tests.txt`: 0 corrections (`grades/*.final.json`).
Part A's ids were blind to the grader; for Part B I wrote `idmap.json` myself, so the
hand-check was not blind to the arm (the grader was).

## Cost

| | Runs | Grading |
|---|---|---|
| A. rerun | 0.46 | 0.10 |
| B. new fixture | 0.43 | 0.17 |
| **Total** | | **1.17 USD** |

(`cost.json`.)

## Deviations from the pre-registration

None in design. Part B's "right"/"wrong" runner slots hold the new and the old persona as
pre-registered (`personas/minimal-change.md` = new, `personas/architect.md` = old).

## Layout

- `EXPERIMENT.md`: pre-registration.
- `persona-text.diff`, `personas-new/minimal-change.md`: the persona change as injected.
- `rerun/`, `newfixture/`: each a self-contained copy of round 2's runner (byte-identical
  `run.mjs`) with `fixtures/`, `fixtures-hidden/`, `keys/`, `prompts/`, `personas/`,
  `idmap.json`, `outputs/`, `transcripts/`, `raw/`, `grades/`, `environment.json`, logs.
- Tested with `claude-sonnet-5`, `--safe-mode`, `--no-session-persistence`, on 2026-09-27.
