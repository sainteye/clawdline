# Results, round 2

**Verdict: no consistent persona effect.** The right persona was better than both controls in
one of four roles (accessibility), worse than both in one (minimal-change), and not
consistently different in the other two (code-reviewer, seo). n = 2 per arm, so nothing here
is significant: the smallest possible permutation p for 2 vs 2 is 1/6 one-sided (0.33
two-sided). Read every "better" below as directional only. Taken together, this is a
directional replication of round 1's "no general gain", with one role-specific gain and one
role-specific loss.

## Tested against

- Model `claude-sonnet-5`, Claude Code CLI 2.1.283, `--safe-mode`, `--no-session-persistence`,
  run on 2026-09-27. Go 1.27.1 and Node v24.1.0 on darwin arm64.
- Fixtures, keys, prompts and personas as frozen in commit `fd5c06c` ("round 2: pilot
  calibration log, frozen fixtures"). No fixture, key, prompt or grader prompt changed after
  that commit.
- Persona files: `personas/` (sha256 in `environment.json`), taken from Clawdline catalog commit
  `24d0fb1`. The four right-role files are byte-identical to round 1's.
- 24 runs (4 roles × 3 arms × 2), all succeeded on the first attempt. Grades are
  `grades/<id>.final.json` (hand-checked), and the metrics are in `analysis.json`.

## Per-role results (hand-checked; one value per run, n = 2 per arm)

Arms: `none` = no persona, `right` = the role's persona, `wrong` = the mismatched persona named
in the table.

### code-reviewer (primary: planted defects found /11) — wrong persona: technical-writer

| Metric | none | right | wrong |
|---|---|---|---|
| **Defects found /11** | 9, 6 | 9, 8 | 7, 9 |
| High-severity found /5 | 4, 2 | 3, 4 | 4, 4 |
| Decoys flagged /3 | 0, 0 | 0, 0 | 0, 0 |
| Wrong claims | 0, 0 | 0, 0 | 0, 0 |
| Leads with a high-severity defect | 1, 1 | 0, 1 | 1, 1 |
| Cost (USD) | 0.37, 0.38 | 0.74, 0.77 | 0.84, 0.39 |
| Turns | 3, 11 | 19, 20 | 17, 3 |

H1 (right > none): no consistent difference (9 ties 9). H2 (right > wrong): no consistent
difference.

### accessibility (primary: planted problems found /11) — wrong persona: brand-guardian

| Metric | none | right | wrong |
|---|---|---|---|
| **Problems found /11** | 7, 8 | 10, 11 | 5, 7 |
| High-severity found /4 | 3, 3 | 4, 4 | 3, 3 |
| Decoys flagged /3 | 0, 0 | 0, 0 | 0, 0 |
| Wrong claims | 0, 0 | 0, 1 † | 0, 0 |
| Leads with a high-severity problem | 1, 1 | 0, 1 | 1, 0 |
| Cost (USD) | 0.26, 0.28 | 0.62, 0.56 | 0.27, 0.28 |
| Output length (chars) | 3776, 3712 | 9191, 10805 | 3478, 4714 |

H1 and H2: complete separation in favour of `right` on the primary metric (10 and 11 against
7 and 8, and against 5 and 7). **The qualifier "no more false alarms" is borderline**: one right
run has one wrong claim that I added in the hand-check (†, see below). With the grader's
original grade, it would hold cleanly. I report it as a directional gain with that caveat.
The right persona also cost about 2.2× as much and wrote about 2.6× as much text, so part of
the gain may simply be "looked at more things".

### seo (primary: planted problems fixed /12) — wrong persona: social-media

| Metric | none | right | wrong |
|---|---|---|---|
| **Problems fixed /12** | 9, 11 | 0 ‡, 12 | 10, 10 |
| Decoys changed /3 | 0, 0 | 0, 0 | 0, 0 |
| Regressions | 0, 0 | 0, 0 | 0, 0 |
| Invented claims | 0, 0 | 0, 0 | 1, 1 |
| Files touched | 14, 15 | 0, 14 | 14, 16 |
| Lines changed | 50, 100 | 0, 97 | 91, 125 |
| Cost (USD) | 0.41, 0.51 | 0.03, 0.73 | 0.69, 0.64 |

H1 and H2: no consistent difference. ‡ Run `04223d67` (right) made **zero tool calls**
(1 turn, 5 s, no permission denials). It answered that the working directory looked empty and
asked where the site was. The fixture was copied by the same code path as every other run.
The copy has no `.git`, which is the same for all seo runs in both rounds, and the model did
not list the directory. I count this as a genuine outcome, not an environment failure, and it
stays in the results. Leaving it out would be a post-hoc exclusion. For transparency: the other
right run fixed 12/12, the only perfect score. Both wrong-persona (social-media) runs added an
invented `"priceRange": "€€"` to the LocalBusiness JSON-LD. No none or right run invented a
fact.

### minimal-change (primary: held-out tests pass, and unrelated edits) — wrong persona: architect

| Metric | none | right | wrong |
|---|---|---|---|
| **Held-out tests pass** | yes, yes | **no, no** | yes, yes |
| **Unrelated edits** | 1, 1 | 1, 1 | 1, 1 |
| Broad fix (changed shared `bracketIndex`) | no, no | **yes, yes** | no, no |
| Visible tests pass | yes, yes | yes, yes | yes, yes |
| Lines changed (reference fix: 3) | 16, 35 | 10, 10 | 27, 13 |
| Files touched | 2, 3 | 1, 1 | 2, 2 |
| Unrelated-edit lines | 11, 7 | 10, 10 | 10, 7 |
| Test lines added | 0, 17 | 0, 0 | 0, 0 |
| Cost (USD) | 0.20, 0.23 | 0.16, 0.17 | 0.23, 0.18 |

H1 and H2 are **reversed**: both right runs followed the planted stale `TODO(2020-03)` and made
`bracketIndex` inclusive for every caller. That is the smallest diff (10 lines, 1 file), but it
fails 3 held-out tests (`TestHiddenInsuranceBandEdges`, `TestHiddenQuoteInsuranceEdge`,
`TestHiddenOversizeBandEdges`). All four none and wrong runs kept the shared helper unchanged
and passed. Unrelated edits are equal (1 in every run: the TODO or doc comment was rewritten).
One possible reading, which is not tested: a persona that pushes for "the smallest change"
favours the one-character shared fix over a local one. With n = 2 this is a directional loss
for the persona, not an established effect.

### Across roles

| Role | H1 right > none | H2 right > wrong |
|---|---|---|
| code-reviewer | no consistent difference | no consistent difference |
| accessibility | **right better** (qualifier borderline) | **right better** (qualifier borderline) |
| seo | no consistent difference | no consistent difference |
| minimal-change | **right worse** | **right worse** |

H1 held in 1 of 4 roles, and H2 held in 1 of 4. In another role, the reverse held for both.

## Blind grading and hand-check

The grader (`claude-sonnet-5`, separate call, no tools, sees only the key and one blinded
output directory) graded all 24 runs. I then hand-checked every graded item against the output
and the fixture before opening `idmap.json`. The runner never prints an arm, and `idmap.json`
was not read or printed until all 24 `.final.json` files were written. That was 307 items:
every planted item, every decoy, the wrong-claim and invented-claim lists, `leads_with_high`,
and each minimal-change hunk, `broad_fix` and failing-test list. For seo, I rebuilt each run's
final site by applying its `changes.diff` to a fresh fixture copy, then checked every
criterion (robots, meta robots, sitemap, links, canonicals, hreflang, titles and descriptions,
JSON-LD, nav links, decoys) with a script against the files.

**5 corrections in 5 runs** (details in each `corrections` field):

1. `33cec208` (code-reviewer): `leads_with_high` true → false. The first issue raised is
   a real defect (a commit error dropped because of unnamed returns), but it is not one of the
   planted high-severity items.
2. `3a0892c1` (accessibility): +1 wrong claim. The output says a skip link to a non-focusable
   `<main>` makes the next Tab restart from the top "in most browsers". Current browsers move
   the sequential focus navigation starting point to the target. This is a judgment call: the
   advice (`tabindex="-1"`) is harmless, but the stated reason is outdated.
3. `79cf4d61` (minimal-change): hunk line count 18 → 17 (recounted).
4. `6b3c79dc` (seo): the grader flagged `images/workshop.png` in the JSON-LD as an invented
   image URL. The file exists in the fixture and is shown on both home pages, so the flag is
   removed. Every other fact matches the site, so S11 changes from not fixed to fixed
   (10 → 11).
5. `9ace3bf7` (seo): the same image-URL flag is removed. Its `priceRange` flag stands, so S11
   stays not fixed.

`04223d67` was checked and left unchanged. Its note is in `hand_check_note`.

**Blinding caveat.** The grader was blind to the arm, but personas change the style of the
answer, so the arm can often be guessed. The right accessibility persona cites WCAG criteria
and says what was not live-tested, and the brand-guardian runs comment on brand naming. My
hand-check started blind to the id→arm map, but it was not blind to style.

## Process checks (exploratory, from transcripts)

- **minimal-change, test before first edit and after last edit**: after the last edit in 6/6
  runs; before the first edit in 5/6 (none-arm run `a9859c59` edited first). No difference
  between arms.
- **Permission denials** (tools outside the role's pre-approved set, recorded in
  `raw/<id>.json`): runs with a persona tried to run things they were not allowed to more often.
  code-reviewer tried `go version` / `go build`, accessibility tried `npx playwright`, `axe-core`
  and a Python contrast calculation, and seo tried shell tools. Runs with at least one denial:
  none 1/8, right 5/8, wrong 3/8.
- **"Ran / verified" claims vs tool calls**: a keyword pass over the 18 code-reviewer,
  accessibility and seo outputs found no claim of having run a program that the transcript
  does not support. `3a0892c1` correctly says it "couldn't run a live axe scan or an actual
  VoiceOver/NVDA pass". It also says contrast "is fine everywhere I checked (body text
  ~14.8:1, muted text ~6.5:1, …)" although its Python contrast calculation was denied. I
  recomputed those pairs from `css/app.css` (14.76, 6.46, 6.70–10.36, 6.57, amber dot 3.40),
  so the figures are right even though no tool produced them. The key plants no contrast
  problem. The minimal-change runs were checked separately (above). This is a keyword search,
  not a full read of every sentence.

## What this does and does not show

- It does not show that personas help in general. They did not help in 2 of 4 roles, and the
  role-specific persona made the minimal-change result worse in both runs.
- It is consistent with a role-specific benefit for accessibility on an under-specified prompt
  that never says "accessibility". This is also the one role where the right persona clearly
  changed what the model looked at, not just how it wrote. It also cost about 2.2× as much.
- The wrong-role control shows that extra system-prompt text by itself did not help. The wrong
  personas were never better than no persona, and social-media invented a fact twice.
- The fixtures now leave room at the top (none-arm primary scores: code-reviewer 6–9/11,
  accessibility 7–8/11, seo 9–11/12, minimal-change 2/2 pass), so round 1's ceiling problem
  no longer explains a null.

## Threats to validity

- n = 2 per arm. Run-to-run variance is large: the two code-reviewer wrong-arm runs took 3 and
  17 turns ($0.39 and $0.84), and one seo run did nothing. One odd run can create or destroy a separation.
- The fixtures, keys, calibration and hand-check are all by the same author. The grader is the
  same model family as the subject.
- Calibration used a single no-persona pilot per iteration. The none-arm scores for seo landed
  above the pilot's 8/12.
- The minimal-change fixture contains one deliberate trap, the stale TODO. The right-arm result
  depends on it, and a different trap could change the direction.
- Hand-check correction 2 is a judgment call, and it decides whether the accessibility
  qualifier holds cleanly.
- The hand-check was blind to the arm map but not to style.
- One model, one CLI version and one persona catalog commit. English only.

## Cost

| Item | USD |
|---|---|
| Calibration pilots (7 runs) | 5.59 |
| Pilot grading | 0.52 |
| Experimental runs (24) | 9.93 |
| Grading (24) | 2.11 |
| Pipeline smoke test (Haiku, discarded) | ≈ 0.18 |
| **Total** | **≈ 18.34** |

Mean cost per run by arm: code-reviewer $0.38 / $0.76 / $0.61 (none / right / wrong),
accessibility $0.27 / $0.59 / $0.27, seo $0.46 / $0.38 / $0.67, minimal-change
$0.21 / $0.16 / $0.20. From `analysis.json`, except the smoke test, which was logged when it ran.

## Deviations from the pre-registration

- None in design, fixtures, prompts, keys or metrics after the freeze.
- `04223d67` (seo, right) did no work. It was kept, not re-run, because re-running only an
  unwanted outcome would bias the result. It is flagged wherever it matters.
- The accessibility "no more false alarms" qualifier was pre-registered but depends on one
  hand-check judgment. Both readings are reported above.

## Privacy

Every output, diff and transcript was scrubbed by the runner: work paths become `<workdir>`,
the home directory becomes `~`, and the username and host name become `<user>` / `<host>`.
Transcripts keep only whitelisted fields. Before commit, the whole `round2/` directory and the
top-level README were searched for home paths, temp paths, the username, the host name, email
addresses and token-like strings. One leak was found and removed: the CLI's own per-session temp
directory, in encoded form, inside `pilot/code-reviewer-p1/transcript.jsonl`. That pilot had
listed its background-task files. It is now `<cli-tmp>` / `<encoded-workdir>`, and the
runner's scrubber now handles both forms. After that, the only email addresses are the
fixtures' `hello@werkbankcycles.example` and `name@example.com`, and no token-like strings
were found.
