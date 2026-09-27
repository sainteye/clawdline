# Results

**Verdict: not shown.** In this setup (Claude Sonnet 5, four small fixtures, 2 runs per arm), the
right persona did **not** improve the pre-registered primary metric for any of the four roles.
For code-reviewer and accessibility, the persona arm found **one fewer** planted item in both of
its runs. For minimal-change and seo, both arms produced the same outcome. The model without a
persona already scored at or near the ceiling on every fixture, so these fixtures mostly could
not show an improvement. What the personas did change, consistently, was the **form and cost**
of the answer (see "Exploratory observations"). Those differences were not pre-registered.

n = 2 runs per arm per role, 16 runs in total. This is a demonstration, not a test: the smallest
two-sided permutation p-value possible for 2 vs 2 is 0.33. All outputs were graded blind, and
every grading was then hand-checked.

## Tested against

| | |
|---|---|
| Date | 2026-09-27 (runs started 05:45 UTC) |
| Claude CLI | 2.1.283 (Claude Code) |
| Model (runs and grader) | `claude-sonnet-5` (confirmed in every run's `modelUsage`) |
| Persona files | `personas/*.md`, sha256 in `environment.json`; byte-identical to the files the daemon injects |
| Persona catalog commit | `24d0fb1ab7b346f86f0d43157abdbc34cfe5c09b` |
| Go / Node | go1.27.1 darwin/arm64 / v24.1.0 |
| Isolation | `--safe-mode` (no user CLAUDE.md, skills, plugins, hooks or MCP); a fresh temp copy of the fixture per run |
| Retries | none needed: all 16 runs succeeded on the first attempt (`raw/<id>.json`) |

## Headline table (mean ± sd over 2 runs; hand-checked grades)

| Role | Primary metric | none | persona | False alarms / damage (none → persona) | Output chars | Cost per run (USD) |
|---|---|---|---|---|---|---|
| code-reviewer | defects found /7 | **7.0 ± 0** | 6.0 ± 0 | decoys 0 → 0, wrong claims 0 → 0 | 3427 → 4892 | 0.202 → 0.315 |
| code-reviewer | high-severity found /4 | 4.0 ± 0 | 4.0 ± 0 | | | |
| code-reviewer | leads with high-severity | 2/2 | 2/2 | | | |
| minimal-change | tests pass | 2/2 | 2/2 | unrelated edits 0 → 0 | 369 → 819 | 0.077 → 0.102 |
| minimal-change | lines changed / files | 7 / 1 | 7 / 1 | (all 4 diffs byte-identical) | | |
| seo | problems fixed /8 | 7.0 ± 0 | 7.0 ± 0 | regressions 0 → 0, invented 0 → 0 | 1311 → 2011 | 0.182 → 0.254 |
| seo | files touched | 1.0 | 1.5 ± 0.7 | | | |
| accessibility | problems found /8 | **8.0 ± 0** | 7.0 ± 0 | decoys 0 → 0, wrong claims 0 → 0 | 2491 → 5542 | 0.097 → 0.160 |
| accessibility | high-severity found /3 | 3.0 ± 0 | 3.0 ± 0 | | | |
| accessibility | leads with high-severity | 2/2 | 2/2 | | | |

Per run (id → arm is in `idmap.json`):

| Role | Arm | id | Primary | Missed |
|---|---|---|---|---|
| code-reviewer | none | fd9ece61 | 7/7 | — |
| code-reviewer | none | 13edbec0 | 7/7 | — |
| code-reviewer | persona | 4de4c437 | 6/7 | G6 (delete error swallowed, 204 returned) |
| code-reviewer | persona | bb475455 | 6/7 | G6 |
| accessibility | none | 1a9f2773 | 8/8 | — |
| accessibility | none | e487add9 | 8/8 | — |
| accessibility | persona | b57668a5 | 7/8 | A1 (`<html>` without `lang`) |
| accessibility | persona | 7d2fd285 | 7/8 | A1 |
| seo | none | 08346c33 / e447f9bc | 7/8, 7/8 | S7 (structured data) in both |
| seo | persona | 4e893106 / 6711ce1b | 7/8, 7/8 | S7 in both |
| minimal-change | none | 03ddb9e9 / 3c709e87 | pass, 7 lines, 0 unrelated | — |
| minimal-change | persona | 714310e8 / d87cb75e | pass, 7 lines, 0 unrelated | — |

### Hypotheses

| Hypothesis | Result |
|---|---|
| H-CR: persona finds more defects, no more false alarms | **Not supported.** The difference points the other way (6 vs 7 in both runs); false alarms equal (0). |
| H-MC: tests pass in both arms; persona makes fewer unrelated edits | **No difference.** Both arms produced the reference fix byte-for-byte, with 0 unrelated edits (ceiling). |
| H-SEO: persona fixes more of 8, no more regressions or invented claims | **No difference.** 7/8 in all four runs; none added structured data. |
| H-A11Y: persona finds more of 8, no more false alarms | **Not supported.** The difference points the other way (7 vs 8 in both runs); false alarms equal (0). |

Across roles: the persona arm was better on its primary metric in **0 of 4** roles, equal in
2, and lower by one item in 2.

## Blind grading and hand-check

Each output was graded by a separate `claude -p` call (same model, `--safe-mode`, no tools).
The grader saw only the role's grader prompt, key and output (`grades/<id>.json`). I then read
every output (or diff) against the key and checked each graded item: **156 items, 5 corrected,
96.8 % agreement**. The corrected grades are in `grades/<id>.final.json`, with the reason for each change.

| id | Task | Grader said | Corrected to | Why |
|---|---|---|---|---|
| 03ddb9e9, 3c709e87 | minimal-change | fix hunk = 6 changed lines | 7 | the diff is +6/−1. The metric is computed from the diff, so it was unaffected. |
| 4e893106, e447f9bc | seo | 1 invented claim ("skillet cornbread with brown butter") | 0 | That is the linked post's own title (`fixtures/seo/index.html:19`, `posts/cast-iron-cornbread.html:15`). |
| 6711ce1b | seo | 1 regression (deleted `.post-title` CSS rule) | 0 | `grep` shows only the edited article used that class, so no page is damaged. It stays visible as files touched = 2. |

All 8 code-reviewer and accessibility gradings agreed with my reading on every item. I also
checked that the "found" quotes really name the mechanism. For example, every G5 credit names
the unclosed `countRows`, not just "the count query".

Blinding caveat: after the runs finished, the runner's console tail showed the first three rows
of `idmap.json` (d87cb75e and 714310e8 = minimal-change/persona, 1a9f2773 =
accessibility/none). So my hand-check of those three was not blind. None of the three needed a
correction. The persona outputs are also recognisable by style (verdict line, "Verified
correct", WCAG criteria), so the grader and I could often guess the arm anyway.

## Exploratory observations (not pre-registered; counted by hand from the outputs)

The personas did change *how* the answer looks, in the direction each persona file asks for:

| Role | Feature | none | persona |
|---|---|---|---|
| code-reviewer | one-line verdict + blocker / should-fix / nit ranking | 0/2 (Critical / Bugs / Minor headings) | 2/2 |
| code-reviewer | "Verified correct" section that explicitly clears a decoy | 0/2 (1 run clears D2 in a closing sentence) | 2/2 (clears D1+D2, D2+D3) |
| code-reviewer | connection leak (G5, key severity high) ranked as | "Minor" in 2/2 | blocker 1, should-fix 1 |
| code-reviewer | "Questions for the author" section | 0/2 | 2/2 |
| accessibility | WCAG criterion lines cited | 0 and 2 | 9 and 10 |
| accessibility | states what was *not* tested (no live keyboard/screen-reader pass) | 0/2 | 2/2 |
| accessibility | "what already works" + conformance statement | 0/2 | 2/2 |
| minimal-change | lists what it deliberately did not change | 0/2 | 2/2 |
| minimal-change | says the failing test was seen failing before the fix | 0/2 (both say "presumably failing") | 1/2 (claimed; not verifiable, since transcripts were not captured) |
| seo | edits outside the article | 0/2 | 1/2 (removed the now-unused `.post-title` CSS rule) |
| all | cost | 1.0× | 1.3–1.7× |
| all | wall time | 1.0× | 1.1–2.1× |

A plausible reading, which this experiment did not test: the persona steers attention toward its
own checklist (verification, severity, WCAG criteria, scope notes). That makes the answer more
auditable and costs more. On these small fixtures it also coincided with one quieter item being
dropped in both runs of two roles. With n = 2, that last pattern may be chance.

### Representative excerpts

code-reviewer, **none** (fd9ece61). Finds all 7 defects but files the leak under "Minor":
> **7. `countRows` is never closed** (`store.go`, `ListOrders`) — `s.db.QueryContext(...)` for the count leaks the `*sql.Rows`/connection on every call.

code-reviewer, **persona** (4de4c437). Ranked, with an explicit record of what was checked and found correct:
> ## Verdict: needs rework
> …
> 6. **DB connection leak on every list call.** …
> ## Verified correct
> - go.mod pins Go 1.22, which has per-iteration loop variables, so the `go func(){...}()` closures over `i`/`chunk` in `batchGetOrders` … are race-free despite not being passed as params.

accessibility, **none** (e487add9):
> - **Submit control isn't a real button** (`signup.html:42`): `<div id="submit" onclick="submitForm()">` is not focusable and has no keydown handler, so keyboard-only and screen-reader users can't submit the form at all. Use `<button type="submit">`.

accessibility, **persona** (7d2fd285):
> This was a **static code-level audit** … I did not have a live browser + NVDA/VoiceOver session available in this environment, so treat the keyboard/AT findings below as "verify live" items …
> **1. Submit control is a `<div>`, not a button** … **Criterion:** 2.1.1 Keyboard (A), 4.1.2 Name, Role, Value (A) … **Verify:** Tab through the form; the submit control should receive focus and activate on Enter/Space.

minimal-change, **none** (3c709e87):
> All tests pass, including the existing negative-amount cases in `money_test.go` that were presumably already failing before.

minimal-change, **persona** (d87cb75e):
> **Not changed** (noticed but out of scope): `round_cents_legacy` and `fmt_percent` are unrelated dead/misnamed code — left alone since the ask was specifically the invoice sign bug.

seo, **none** (08346c33) and **persona** (6711ce1b): the same seven fixes. The persona run also
deleted the now-unused `.post-title` CSS rule and reported "Verified: no build step exists …
confirmed the canonical URL matches the existing sitemap entry".

## What this does and does not show

It shows:
- On these fixtures, Sonnet 5 without a persona already does the job: it found 7/7 PR defects
  and 8/8 accessibility problems with no false alarms, made a minimal fix with no unrelated edits,
  and fixed 7 of 8 SEO problems.
- Adding the right persona did not raise those numbers. In two roles it was one item lower in
  both runs.
- The personas reliably change structure, severity labelling, stated verification and scope
  notes, and they raise cost by about 30–65 %.

It does not show:
- That personas never help. The fixtures turned out too easy (ceiling), and n = 2.
- That the one-item drops are caused by the persona. They are consistent across both runs but
  within what chance can produce at n = 2.
- Anything about "role vs. more system prompt". There was no wrong-role arm in this version.
- Anything about other models, longer or messier tasks, or sessions where the persona competes
  with a project CLAUDE.md (Clawdline's real setting). `--safe-mode` removed that context here.

## Threats to validity

- **Fixtures are ours and small.** The same person wrote the fixtures, planted the defects and
  wrote the keys, so the planted items may be "textbook" and easy for the model. Both review
  roles hit the ceiling without a persona, so the setup cannot detect an improvement.
- **n = 2 per arm.** With every run within an arm identical, no variance estimate is meaningful,
  and no significance test has power.
- **Same model family grades.** The grader is the same model as the subject. Hand-checking every
  grading reduces this, but I am not independent either: I wrote the keys.
- **Blinding is partial.** Persona outputs are recognisable by style, and 3 ids were seen
  unblinded before the hand-check (listed above).
- **One prompt per role.** A different phrasing could change the result either way.
- **`--safe-mode` differs from production.** Real Clawdline sessions also load the user's and the
  project's CLAUDE.md, skills and hooks. This experiment deliberately removed them, for privacy
  and reproducibility.
- **Transcripts not captured.** Claims such as "ran the test before the fix" cannot be checked,
  because only the final message, the diff and the test output are recorded.
- **"Primary metric" is recall of planted items.** Qualities the personas aim at (a verified
  record, severity calibration, scope discipline) were only looked at exploratorily.

## Cost

| | USD |
|---|---|
| 16 experimental runs | 2.78 |
| 16 grader calls | 0.64 |
| **Total (experiment)** | **3.42** |
| Pipeline smoke test (Haiku 4.5, discarded) | ≈1.0 (two run passes + one grading pass) |

## Deviations from the pre-registration

- None in fixtures, keys, prompts, grader prompts or metrics after the pre-registration commit.
- The runner's diff-header clean-up was fixed during the Haiku smoke test, before any experimental
  run. The first version left `b<workdir>/` in paths.
- The exploratory table above was not pre-registered, and it is labelled as such.
