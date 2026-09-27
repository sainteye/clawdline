# Pre-registration, round 2: harder fixtures, a wrong-role control, transcripts

Written 2026-09-27, after round 1 and before any round-2 run (pilot or experimental). This
file is committed before the first pilot run. The design, prompts, grading standard, metrics,
hypotheses and decision rules below are frozen from that commit. The fixtures are frozen at
the end of the pilot calibration described below. Every calibration change is logged in the
"Pilot log" section at the end, and that is the only section that changes after this commit
(and before any experimental run). Anything that changes later is listed under "Deviations"
in `results.md`.

## Why a second round

Round 1 (`../EXPERIMENT.md`, `../results.md`) found no primary-metric gain for any of the four
roles. The no-persona arm was at or near the ceiling (7/7, 8/8, identical minimal diffs, 7/8),
so those fixtures could not show an improvement. Round 1 also had no wrong-role arm, so it could
not separate "the right role" from "any extra system prompt", and it did not keep transcripts, so
process claims ("ran the test first") could not be checked.

Round 2 fixes these three points: fixtures calibrated so the no-persona arm is below the
ceiling, a wrong-persona control arm, and full transcripts.

## Question

Does the right persona give a better result than (a) no persona and (b) a plausible but
mismatched persona of similar length, on under-specified, realistic requests where the role
should matter?

## Design

Four roles, as in round 1: code-reviewer, minimal-change, seo, accessibility. Three arms per
role:

- `none`: no persona.
- `right`: the role's own persona file.
- `wrong`: a mismatched persona from the same catalog (table below).

**2 runs per arm** → 4 roles × 3 arms × 2 = **24 runs**.

Persona files are the exact rendered text the daemon injects (preamble + role body +
attribution), snapshotted in `personas/`, with sha256 in `environment.json`. The four
right-role files are byte-identical to round 1's.

### The wrong-role arm

| Role | Wrong persona | Size (bytes) right → wrong | Why this one |
|---|---|---|---|
| code-reviewer | technical-writer | 4065 → 3988 | Reads code and checks claims against it (plausible for "look at this PR"), but aims at documentation for a reader, not at defects. |
| minimal-change | architect | 3597 → 4386 | Plausible for "fix this bug" in a codebase, but its checklist (options, boundaries, design) pulls toward restructuring, the opposite of minimal scope. It is 22 % longer; every persona in the catalog is 3.6–4.8 kB, and no shorter persona is as close in domain. |
| seo | social-media | 4454 → 3916 | Works from a site's own content for discoverability and traffic (plausible for "get found on Google"), but on social platforms, not search engines. |
| accessibility | brand-guardian | 4007 → 4261 | Reviews front-end surfaces for consistency and cites `file:line` (plausible for "look over this front end"), but against a brand, not against users with disabilities. It mentions contrast once, so it overlaps a little. |

The wrong arm answers: if the right persona helps, is it the role, or any ~4 kB of extra
instruction in the system prompt?

### Invocation (every run)

```
claude -p "<prompt>" --model claude-sonnet-5 --no-session-persistence --safe-mode \
  --allowedTools <role tools> --output-format stream-json --verbose \
  [--append-system-prompt-file personas/<persona>.md]
```

The only change from round 1 is the output format, which now streams the full transcript. The
runner keeps only the fields needed to check process claims: model, tools, each message's
content (text, thinking, tool calls and tool results), and the final result, with cost and
turns. It drops ids, socket and plugin paths, and rate-limit data, and scrubs local paths
(`<workdir>`, `~`), the username and the host name. Each run gets a fresh temporary copy of the
fixture, outside this directory and outside any git repository. Run order is a seeded shuffle
of the 24 runs (seed `persona-exp-2026-r2`), 4 at a time. A failed run is retried up to 3 times,
and the retries are recorded in `raw/<id>.json`.

## Roles, fixtures, prompts

Prompts are natural English and deliberately under-specified, like round 1. The
accessibility prompt again does not say "accessibility".

| Role | Fixture | Prompt (exact) | Tools |
|---|---|---|---|
| code-reviewer | `fixtures/code-reviewer`: a Go wallet/ledger service (standard library only). The PR adds transfers, a payouts client with retries, a daily transfer limit in a business time zone, a bounded webhook wait, amount formatting and an error-mapping refactor: `pr.diff`, 10 files, +678/−64. The base is `fixtures-src/code-reviewer-base`. | `Can you take a look at this PR and give me some feedback? pr.diff` (same as round 1) | Read Grep Glob |
| minimal-change | `fixtures/minimal-change`: Go `parcelrate` package, 839 lines in 10 files, with lots of tempting mess (duplicated lookups, a deprecated helper, TODOs, a hand-written sort, a stale NOTE saying two lookups "could reuse" each other). `TestQuoteTierBoundary` fails. Held-out tests are in `fixtures-hidden/minimal-change/`. The model never sees them; the runner copies them in after the run. | `A parcel that weighs exactly 2 kg is being quoted at the 5 kg price instead of the 2 kg price — can you fix it?` | Read Grep Glob Edit Write Bash |
| seo | `fixtures/seo`: a static site for a bike repair shop, with 13 HTML pages (10 English, 3 German), `robots.txt`, `sitemap.xml`, CSS and images | `Our bike shop's website barely shows up on Google, in English or German. Can you help it get found more easily?` | Read Grep Glob Edit Write |
| accessibility | `fixtures/accessibility`: a vanilla-JS clinic appointment booking SPA, 1183 lines in 14 files (views, components, API wrapper, state, CSS) | `Can you look over the front end of this appointment booking app and tell me what should change before we launch? index.html` | Read Grep Glob |

Answer keys (`keys/*.json`) were written with the fixtures, before any run:

- **code-reviewer**: 11 planted defects, all introduced by the PR.
  - 5 high:
    - G1: the new `GET /transfers/{id}` is guarded by an owner check that reads `{id}` as an account id.
    - G2: a shadowed `err` makes a deferred commit run after a failed step.
    - G3: an overdraft through a TOCTOU balance check against a balance read earlier by middleware.
    - G4: a shadowed `err` in the retry loop reports a failed payout as success.
    - G5: a goroutine blocks forever on an unbuffered channel after the wait times out.
  - 4 medium:
    - G6: a response body is not closed on the 429/5xx retry path.
    - G7: `%v` instead of `%w` breaks `errors.Is`, so a 404 becomes a 500.
    - G8: the daily limit is in whole units in config but compared with cents.
    - G9: the business day is started from the UTC date.
  - 2 low:
    - G10: the `Location` header is set after the body is written.
    - G11: negative amounts are formatted as `-5.-50`.
  - 3 decoys: an unlocked read of a map written only in `init()`; `rows` closed by `defer`
    inside the helper it is passed to; an `int32` conversion that is range-checked in another
    file.
  - Every defect and decoy was demonstrated with a throwaway test against a fake SQL driver,
    outside the fixture.
- **minimal-change**:
  - The bug: `Quote` passes inclusive tier limits ("up to 2000 g") to a shared helper,
    `bracketIndex`, that treats limits as exclusive.
  - The reference fix changes 3 lines in `rates.go` (`UpToGrams + 1` where the limits are
    built).
  - The tempting broad fix is to make `bracketIndex` inclusive. It passes the visible tests but
    breaks insurance and oversize fees, whose exclusive boundaries are documented in
    `brackets.go` and at both call sites. Held-out tests catch it.
  - Held-out tests also catch hard-coded fixes, "reusing" `bracketIndex` for letters as the
    stale NOTE suggests, and "unifying" the documented truncation in `applyZoneFactor`.
  - All of this was verified: the original fails only the bug tests, the reference fix passes
    everything, and each broad fix passes the visible tests but fails held-out ones.
- **seo**: 12 planted problems spread over pages and site files, 3 decoys.
  - The planted problems:
    - `robots.txt` still blocks `/blog/`.
    - `noindex` was left on the services page.
    - The sitemap lists a 404 and a noindexed page.
    - Two internal links point to a renamed page.
    - The German home page's canonical points to the English home page.
    - One canonical uses `http://`.
    - The product page has two conflicting canonicals.
    - The contact pair uses `hreflang="ge"`.
    - The German services page has no return hreflang.
    - Two posts copy the blog index's title and description.
    - There is no LocalBusiness JSON-LD, although the facts are visible on the pages.
    - The "Shop" nav item on all 12 header pages is a `<span onclick>` with no `href`.
  - The 3 decoys, all correct and to be left as they are: `noindex` on the thank-you page, the
    print version's canonical to the article, and `rel="nofollow ugc"` on a reader comment
    link.
  - Every item has an objective `fixed_when`, judged from the diff.
- **accessibility**: 11 planted problems, 3 decoys.
  - 4 high:
    - A1: the confirm dialog does not move focus in, trap focus, close on Esc or return focus.
    - A2: a div-based specialty select has no role, state or keyboard support.
    - A3: a 5-minute slot hold expires without warning or a way to extend.
    - A4: `outline: none` on slot buttons, with no replacement.
  - 5 medium:
    - A5: availability is shown only by coloured dots.
    - A6: field errors are not associated with their inputs (no `aria-describedby` or
      `aria-invalid`).
    - A7: submit errors go to a container that is not a live region.
    - A8: view changes do not move focus or update `document.title`.
    - A9: an auto-advancing carousel has no pause and ignores `prefers-reduced-motion`.
  - 2 low:
    - A10: a 2-second toast.
    - A11: `tabindex="1"` on the search box.
  - 3 decoys: `aria-hidden` on a decorative SVG in a text button; `tabindex="-1"` on a
    programmatic focus target; `outline: none` followed by a visible `:focus-visible` ring.

## Calibration (pilot), then freeze

Goal: the no-persona arm must be able to miss things, and the right arm must be able to do
better. For each role:

1. Run **one** pilot run with no persona on the candidate fixture (`node run.mjs pilot`).
2. Grade it with the same grader prompt and key as the experiment, and hand-check the grade.
3. Target for the review/fix roles (code-reviewer, accessibility, seo): the pilot finds or fixes
   **roughly 50–75 %** of the planted items (code-reviewer and accessibility 6–8 of 11, seo 6–9
   of 12). Target for minimal-change: the pilot is **not at the ceiling**. The ceiling means all
   of these hold: the held-out tests pass, there are 0 unrelated edits, and the lines changed are
   at most twice the reference fix.
4. If the pilot is outside the target, change the **fixture only**: add or remove noise, move a
   defect deeper or shallower, remove or add an adjacent hint. The prompt and the key's standard
   for "found" or "fixed" stay the same. The key is updated only to follow the fixture, for
   example to fix line numbers or to replace a planted item with one of the same kind and
   severity. Then run a new pilot.
5. At most **3 pilot iterations per role**. If a role is still outside the target after the
   third, the last version is frozen anyway, and that is reported.

Pilot runs, their grades and every change are logged below. Pilot outputs are kept in `pilot/`
and are **excluded from the results**. The pilot uses the no-persona arm only, so the fixture
is not tuned against a persona.

## Grading

Blind, as in round 1. Every output is stored under a random id in `outputs/<id>/`: the final
message, plus `changes.diff` for editing roles and, for minimal-change, `tests.txt` (`go test`
with the visible tests, then with the held-out tests copied in). `idmap.json` maps id → role, arm
and rep. It is never given to the grader. **The runner never prints an arm, and I do not open
`idmap.json`, and do not print its contents, until every hand-check is written.** Each output is
graded by a separate `claude -p` call (same model, `--safe-mode`, no tools), which sees only
`prompts/grader-common.md`, `prompts/grader-<role>.md`, the role's key, and that output.

Then I hand-check **every graded item** of every output against the output (and diff) and the
key, and record the corrections in `grades/<id>.final.json`. Metrics use the hand-checked
grades. Transcripts are read after the hand-check, for the process checks below.

Outputs may still reveal their arm by style (a persona's headings). That cannot be blinded
away, and the results say how often it happened.

## Metrics (per role, per arm; each run, plus the mean over 2)

- **code-reviewer** (primary: defects found of 11). Secondary: high-severity found (of `high_ids`), decoys
  flagged (of 3), wrong claims, leads with a high-severity defect.
- **accessibility** (primary: problems found of 11). Secondary: high found (of `high_ids`), decoys
  flagged (of 3), wrong claims, leads with a high-severity problem.
- **seo** (primary: problems fixed of 12, judged from the resulting files). Secondary: decoys changed (of
  3; each also counts as a regression), regressions, invented claims, files touched, lines
  changed.
- **minimal-change** (primary: held-out tests pass (0/1) **and** unrelated edits (count)).
  Secondary:
  visible tests pass, broad fix (changed shared behaviour listed in the key), lines changed,
  files touched, unrelated-edit lines, test lines added.
- **all**: cost (USD), turns, duration, output length (characters).
- **process** (from transcripts, exploratory): for minimal-change, whether the failing test was
  run before the first edit and after the last one; for every role, whether a claim in the
  final message about something having been run or checked matches a tool call in the
  transcript.

## Hypotheses (directional, per role)

- H1 (role helps): `right` scores better than `none` on the primary metric, with no more false
  alarms or damage.
- H2 (it is the role, not extra prompt): `right` scores better than `wrong` on the primary
  metric, with no more false alarms or damage.
- H0 (round 1 replicates): no consistent difference between arms.

Per role, "better" means **both** runs of one arm score higher than **both** runs of the other
(complete separation). Otherwise the result is "no consistent difference". For minimal-change,
"better" means more held-out passes with no more unrelated edits, or fewer unrelated edits in
both runs with no fewer held-out passes. Across roles, I report in how many of the four roles
H1 and H2 held.

## What this can and cannot show

With n = 2 per arm, no test has useful power. The smallest possible permutation p for 2 vs 2 is
1/6 ≈ 0.17 one-sided, or 0.33 two-sided, so no result here is significant, and none will be
called significant. A complete separation in a role is a **directional** result, and a
consistent pattern across roles is at most a **directional replication** or non-replication of
round 1. The fixtures, keys and hand-check are by the same author, and the grader is the same
model family as the subject.

## Pipeline smoke test

Before the first pilot, the runner was exercised once in a scratch copy with a cheaper model
(`claude-haiku-4-5-20251001`, one pilot each for minimal-change and seo, and one grading). This
only checked the plumbing: files written, held-out tests copied in, transcripts whitelisted
and scrubbed, and grader JSON parsed. Those outputs were discarded. They changed no fixture,
key, prompt or metric. They are not counted as pilots, and their cost (≈ $0.18) is reported
with the total.

Note on tools, the same as round 1: `--allowedTools` pre-approves the role's tools. Other
tools are still listed in the session, but in `-p` mode they are denied without a prompt.
Denials are recorded per run in `raw/<id>.json` (`permission_denials`).

## Pilot log

Each pilot is one run with no persona, graded by the experiment's grader and then
hand-checked. Outputs are in `pilot/<role>-<label>/`.

### Iteration 1 (fixtures as committed in the pre-registration)

| Role | Pilot | Result (hand-checked) | Target | Decision |
|---|---|---|---|---|
| code-reviewer | `code-reviewer-p1` | 10/11. Missed G11 (negative formatting). Decoys 0, wrong claims 0. $2.04. | 6–8 | too easy → change the fixture |
| accessibility | `accessibility-p1` | 7/11. Missed A3, A6, A7, A10. Decoys 0, wrong claims 0. $0.25. | 6–8 | **freeze** |
| seo | `seo-p1` | 8/12. Not fixed: S3 (`/thanks.html` left in the sitemap), S5, S10, S11. Decoys unchanged, 0 regressions, 0 invented claims. $0.51. | 6–9 | **freeze** |
| minimal-change | `minimal-change-p1` | The reference fix, plus a comment on the changed lines. 5 lines, 1 file, 0 unrelated edits, held-out tests pass. $0.14. | not at ceiling | at ceiling → change the fixture |

I agreed with the grader on every item of these four pilots.


### Iteration 2

Changes to the fixtures. The prompt and the grading standard did not change. The keys were
updated only for line numbers and locations.

- **minimal-change**:
  - Added a stale `TODO(2020-03)` on `bracketIndex` saying limits "should be inclusive", and
    pointed the `Quote` call-site comment at that TODO.
  - Removed the dependence note from `bracketIndex`'s doc. The dependence is still documented at
    both callers (`insurance.go`, `surcharges.go`).
  - Added a visible `brackets_test.go` that does not pin either semantics at the boundaries.
  - Re-verified: the original fails only the bug tests; the reference fix (`UpToGrams + 1`)
    passes everything; the TODO's broad fix passes the visible tests and fails 3 held-out
    tests.
- **code-reviewer** (same 11 mechanisms, same severities, same decoys). PR is now +700/−66
  in 10 files.
  - G3: `Withdraw` now also uses the unconditional `adjustBalance`, which removes the contrast
    with a conditional `UPDATE`. Payouts now have the same stale-balance check.
  - G5: the channel is created in `notify.go`, and the timed wait moved to a
    `waitOrTimeout` helper in `handler.go`.
  - G8: the parameter is renamed from `amountCents` to `amount`. The unit is now documented
    only on the config field.
  - G9: `startOfBusinessDay` moved to `config.go`, with a passing multi-time-zone test whose
    instants happen to share the UTC date.
  - G10: the header is now set inside a `writeCreated` helper, after `writeJSON`.
  - Every defect and decoy was re-demonstrated with the scratch harness under `-race`.

| Role | Pilot | Result (hand-checked) | Target | Decision |
|---|---|---|---|---|
| minimal-change | `minimal-change-p2` | Visible and held-out tests pass, and no broad fix: shared `bracketIndex` behaviour is unchanged. But it added a new `bracketIndexInclusive` helper plus its test, and rewrote the `bracketIndex` doc comment and deleted the TODO (1 unrelated edit, comment churn). 42 changed lines in 3 files, against a 3-line reference. $0.22. | not at ceiling | **freeze** |
| code-reviewer | `code-reviewer-p2` | The grader said 9/11. Hand-check: **10/11**. G3 is corrected to found: "daily-limit check and balance update aren't atomic, allowing concurrent requests to race past the daily limit and drive balance negative" names the non-atomic check-then-debit overdraft. Missed G8 only. Decoys 0, wrong claims 0. $2.02. | 6–8 | still too easy → iteration 3 (the last allowed) |

### Iteration 3 (code-reviewer only; the last iteration allowed)

The protocol allows replacing a planted item with one of the same kind and severity. Five of
the most textbook-looking defects were replaced by less familiar forms of the same kind. The
PR is now +737/−64 in 10 files.

| Item | Before | After |
|---|---|---|
| G5 (high, goroutine leak) | Unbuffered channel and timeout in one or two functions | The channel is created by a small `goResult` helper in `transfer.go`, wired by `NotifyWithin` in `notify.go`, and abandoned by `waitOrTimeout` in `handler.go`. Each piece looks fine alone. |
| G6 (medium, leak on the retry/error path) | `defer Body.Close()` registered after an early return | The body of a retryable response is kept for `Retry-After`, and it is not closed when the context is cancelled during the backoff. |
| G7 (medium, error mapping) | `%v` instead of `%w` | A new `lookupError` type has no `Unwrap`, so `errors.Is(ErrNotFound)` fails and a 404 becomes a 500. |
| G10 (low, HTTP response metadata) | Header set after the body was written | The `Location` header is built with `r.URL.JoinPath(id)`, which gives `/accounts/{id}/transfers/{id}`, a route that does not exist. |
| G11 (low, amount formatting) | Negative modulo in `FormatAmount` | `FormatAmount` is now correct. The webhook `Event.Summary` hard-codes a 2-decimal scale, which is wrong for JPY (0 decimals) and KWD (3). |

G4 kept its mechanism (a shadowed `err` in the retry loop makes the function return nil after
all attempts fail) in the rewritten `Send`. The other items and the decoys are unchanged. Every
defect and decoy was re-demonstrated under `-race`. The build, vet and tests pass, and
`pr.diff` reproduces the post-PR tree exactly.

| Role | Pilot | Result (hand-checked) | Target | Decision |
|---|---|---|---|---|
| code-reviewer | `code-reviewer-p3` | 8/11. Missed G2 (shadowed `err` → half commit), G5 (goroutine leak), G7 (`lookupError` without `Unwrap`). I agreed with the grader on every item. Decoys 0, wrong claims 0. $0.40, 3 turns. | 6–8 | **freeze** |

### Frozen state

All four fixtures are frozen at the commit that adds this line, before any experimental run.
Final pilot results with no persona: code-reviewer 8/11, accessibility 7/11, seo 8/12,
minimal-change not at the ceiling (a 42-line fix with 1 unrelated edit; held-out tests pass).
Seven pilot runs in total. Pilot cost: see `results.md`.

Caveat: each calibration point is a single run. Pilot costs for code-reviewer varied from $0.40
to $2.04 (3 to 17 turns), so run-to-run variance is large, and the experimental no-persona runs
may land outside the pilot's range.
