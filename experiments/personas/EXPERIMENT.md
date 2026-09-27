# Pre-registration: does the right persona change the result?

Written 2026-09-27, before any experimental run. The fixtures, answer keys, prompts, grader
prompts and metrics below are frozen from this point; anything changed later is listed under
"Deviations" at the end of `results.md`, with the reason.

## Question

Clawdline can give a session a built-in role ("persona") by appending a file to the system
prompt (`claude --append-system-prompt-file <file>`). Does the right persona give a measurably
different, better result than no persona, on an under-specified, realistic request where the
role should matter?

## Design

Four roles, each with its own fixture and prompt. Two arms per role:

- `none`: no persona.
- `persona`: the role's installed persona file, appended with `--append-system-prompt-file`.

**2 runs per arm** (4 roles x 2 arms x 2 = 16 runs). A wrong-role control arm is not run in
this version (the person narrowed the design to fewer runs and more roles), so this experiment
cannot separate "the right role" from "any extra system prompt".

Persona files are the exact rendered text the daemon injects (preamble + role body +
attribution), snapshotted in `personas/` with their sha256 in `environment.json`.

Every run uses:

```
claude -p "<prompt>" --model claude-sonnet-5 --no-session-persistence --safe-mode \
  --allowedTools <role tools> --output-format json [--append-system-prompt-file personas/<role>.md]
```

in a fresh temporary copy of the fixture (cwd = that copy, outside any git repository and
outside this directory, so the answer keys are not reachable by relative path).
`--safe-mode` disables the machine's own CLAUDE.md, skills, plugins, hooks and MCP servers, so
no private instructions leak into the runs and the result does not depend on one person's
setup. Verified before the experiment: with `--safe-mode` the session does not see the user
CLAUDE.md, and an appended marker file is still seen (control: without the file, it is not).

Run order is a seeded shuffle of all 16 runs (seed `persona-exp-2026`), 4 at a time. A run that
errors is retried (up to 3 attempts) and the retry is recorded in `raw/<id>.json`.

## Roles, fixtures, prompts

Prompts are in English, deliberately as under-specified as a real person would write them.

| Role | Fixture | Prompt (exact) | Tools |
|---|---|---|---|
| code-reviewer | `fixtures/code-reviewer`: Go order API; PR adds listing, batch get, admin delete and an in-memory cache (`pr.diff`, 4 files, 286 changed lines) | `Can you take a look at this PR and give me some feedback? pr.diff` | Read Grep Glob |
| minimal-change | `fixtures/minimal-change`: Go `ledger` package; `TestFormatCents` fails for negative amounts; nearby mess: `TODO(2019)`, deprecated snake_case helper `round_cents_legacy`, `fmt_percent`, a comment typo | `Refunds show up on printed invoices as "$-12.-50" instead of "-$12.50" — can you fix it?` | Read Grep Glob Edit Write Bash |
| seo | `fixtures/seo`: 4-page static cooking blog; target article `posts/seasoning-cast-iron.html` | `Help this article get found more easily on Google. posts/seasoning-cast-iron.html` | Read Grep Glob Edit Write |
| accessibility | `fixtures/accessibility`: one sign-up form page (`signup.html`, `styles.css`, `signup.js`) | `Can you take a look at this sign-up form page and tell me what should change? signup.html` | Read Grep Glob |

Answer keys (`keys/*.json`) were written before any run:

- code-reviewer: 7 planted defects (4 high: SQL injection via `sort`, missing admin auth on the
  new DELETE route, unlocked map read in the cache, leaked `*sql.Rows`; 2 medium: off-by-one
  OFFSET, error swallowed with 204; 1 low: TTL in nanoseconds) and 3 decoys (correct code that
  looks suspicious: `defer tx.Rollback()` after commit, goroutine loop-variable capture under
  Go 1.22 writing distinct slice indices, `fmt.Sprintf` used only for `$N` placeholders).
  G4 was confirmed with `go test -race` (race reported; with `Get` locked, none).
- minimal-change: reference fix is 6 changed lines in `FormatCents` in `money.go` only; it was
  confirmed to make `go test ./...` pass. Unrelated-edit categories are defined in the key.
- seo: 8 planted problems on the article page (no `lang`, duplicate/non-descriptive title, no
  meta description, no `h1`, two images without `alt`, no canonical, no structured data,
  "click here" link text), plus definitions of regressions and invented claims.
- accessibility: 8 planted problems (3 high: unlabelled email/phone inputs, `div` used as the
  submit button, `outline: none` on focus; plus no `lang`, low-contrast help text, error not
  associated/announced, informative image without `alt`, h1 -> h4 skip) and 3 decoys
  (decorative image with `alt=""`, checkbox wrapped in its label, textarea with a
  visually-hidden label).

## Grading

Blind: every output is stored under a random id in `outputs/<id>/` (final message; for editing
roles also `changes.diff` against the fixture and, for minimal-change, `go test ./...` output).
`idmap.json` maps id -> role/arm/rep and is never given to the grader. Each output is graded by a
separate `claude -p` call (same model, `--safe-mode`, no tools) given only the role's grader
prompt (`prompts/grader-common.md` + `prompts/grader-<role>.md`), its key and that output.
Outputs may still reveal their arm by style (e.g. a persona's report headings); that cannot be
blinded away.

Then I hand-check **every** grading against the output and the key, record agreement per
item, and write corrected grades to `grades/<id>.final.json` with the list of corrections.
Metrics use the hand-checked grades.

## Metrics (per role, per arm; mean ± sd over the 2 runs, plus each run)

- code-reviewer: defects found (of 7), high-severity found (of 4), decoys flagged (of 3), wrong
  claims, leads with a high-severity defect (0/1).
- accessibility: problems found (of 8), high found (of 3), decoys flagged (of 3), wrong claims,
  leads with a high-severity problem.
- seo: problems fixed in the resulting files (of 8), regressions, invented claims, files
  touched, lines changed.
- minimal-change: `go test ./...` passes, lines changed, files touched, unrelated edits (count
  of unrelated hunks and their lines), test lines added.
- all: cost (USD), turns, duration, output length (characters).

## Hypotheses (directional, per role)

- H-CR: with the persona, more defects found and no more false alarms (decoys + wrong claims).
- H-MC: with the persona, tests pass in both arms and the persona arm makes fewer unrelated
  edits (smaller diff outside the fix).
- H-SEO: with the persona, more of the 8 problems fixed, with no more regressions or invented
  claims.
- H-A11Y: with the persona, more of the 8 problems found, with no more false alarms.

## What this can and cannot show

With n = 2 per arm, no significance test has useful power (the smallest possible two-sided
permutation p for 2 vs 2 is 0.33). Results are reported descriptively, per run, as a
demonstration: "in this setup, the persona arm did X in both runs". A consistent difference in
both runs of a role is suggestive, not proof. Across roles, I report in how many of the 4 roles
the persona arm was better on its primary metric without being worse on its false-alarm or
damage metric.

Pipeline smoke test: before the experiment, the runner is exercised once end to end with a
cheaper model (`claude-haiku-4-5-20251001`, 1 rep) in a scratch copy, only to check plumbing
(files written, paths scrubbed, grader JSON parses). Those outputs are discarded and do not
change any fixture, key, prompt or metric.
