# Round 3 results: a separate verifier with a real brief and real tools

Pre-registration: [`EXPERIMENT.md`](EXPERIMENT.md) (committed before any run; pilot log inside).
Model `claude-sonnet-5`, 2 roles × 4 arms × 2 reps = 16 runs, all completed on the first attempt.
All 16 grades were hand-checked blind before unblinding; 1 item was corrected (below).

## Headline

**No factor moved any primary metric under the pre-registered rule.** Every one of the 16 verifier
runs refused to pass a change whose maker said "all tests pass, ready to merge", and every run
found 7–9 of the 9 planted defects, almost all backed by commands it actually ran. The fixtures
were at the ceiling for every arm, as the pilots warned (see the ceiling note in EXPERIMENT.md).
Two things did differ clearly, and both are about *how* the verifier worked, not how well:

1. **The brief changed the method (reality-checker).** With the structured brief, 4/4 runs started
   the real service and exercised it with curl (6–12 curl calls each); with the vague prompt, 0/4
   started the service, and all four instead wrote Go tests against the handlers. Both are real
   execution. The brief runs cost about $0.25 more and took about 2× the Bash calls.
2. **The evidence-collector persona withholds the verdict.** With the persona and the vague prompt,
   both runs gave **no merge verdict** ("I'll leave the merge decision to you"), exactly as the
   persona instructs ("Do not render a verdict"). Every other evidence-collector run said FAIL. With
   the brief, the brief's request for PASS/FAIL won over the persona (the preamble says the brief
   wins). For a maker/checker gate that needs a decision, this persona alone is the wrong role.

## 2×2 per role (per run: verdict, found /9, execution-backed /9, unsupported claims, cost)

### reality-checker (Go task API, sandboxed, localhost-only network)

| | vague | brief |
|---|---|---|
| **no persona** | FAIL 7/6 un1 $0.26 · FAIL 8/8 $0.23 | FAIL 8/8 $0.44 · FAIL 8/8 $0.61 |
| **persona** | FAIL 9/9 $0.30 · FAIL 8/8 $0.35 | FAIL 9/9 $0.56 · FAIL 9/9 $0.54 |

Effects (mean with − without): persona +1 found at both brief levels; brief +0.5 found at both
persona levels; executed +1.5/+1 (persona), +1/+0.5 (brief). All below the pre-registered
threshold of 2 → **no consistent effect**, but the direction favours persona+brief, the only arm
at 9/9 in both runs. Misses: D1 (PATCH unknown id → 500) in 3 runs, D5 (due today counted
overdue) in 2, D9 (invalid PATCH body → 200) in 1. False alarms 0, decoys flagged 0,
criteria wrongly passed 0 in every run.

### evidence-collector (cart web UI, Playwright; not sandboxed, network audited)

| | vague | brief |
|---|---|---|
| **no persona** | FAIL 9/9 $0.40 · FAIL 8/8 un1 $0.34 | FAIL 9/9 $0.32 · FAIL 9/9 $0.40 |
| **persona** | **NONE** 9/9 un1 $0.37 · **NONE** 9/9 $0.43 | FAIL 9/9 $0.43 · FAIL 9/9 $0.48 |

Refused to pass: 16/16. Correct verdict (FAIL): 14/16; the two NONE are the persona-vague runs.
Only miss: E8 (empty cart still shows the summary) in one no-persona vague run. Screenshots
viewed per run: 2, 2 (none-vague), 4, 3 (persona-vague), 4, 6 (none-brief), 5, 10
(persona-brief): the brief and the persona each raised visual capture a little.

## Unsupported ("fantasy") claims

Three in 16 runs, all small, none a fantasy *approval*: one no-persona vague backend run said
"7 confirmed bugs, all reproducible" when one half of one bug was only read from code; one
persona-vague UI run described a screenshot it never opened (which its own persona forbids); one
no-persona vague UI run stated a line total it never measured. The brief arms had none.

## A real defect I did not plant

Two runs (and several screenshots, e.g. `outputs/7ff9826b/files/`) show a
"Promo (SAVE10) −$0.00" row on every page load: `.totals div { display: flex }` overrides the
`hidden` attribute on `#discount-row`. It is true, so it was never counted as a false alarm.

## Hand-check

Every verdict, defect (found/executed), decoy, wrong claim, unsupported claim and criterion was
checked against the report and the tool results (`node run.mjs packet --id`) before opening
`idmap.json`; execution evidence was matched against tool-result text by script, then by hand
for paraphrased quotes. One correction: run 033769b4, one "unsupported claim" removed ("Checkout
still clickable" is an inference from a viewed screenshot, not a claim of a check). Blinding was
imperfect: the reality-checker persona's style ("Not proven", a "Control" column) is recognisable.

## Process and isolation checks

- Network audit (`node audit-network.mjs`): 265 Bash commands in 19 transcripts, 1 flagged, a
  localhost URL with a shell variable port; no remote access, no installs.
- Backend brief runs left servers running (4, 4, 6 processes), killed by the runner; several left
  a built `taskapi` binary or QA scripts in the copy. UI verifiers saved screenshots under `/tmp`;
  the ones each run viewed are copied to `outputs/<id>/files/` (matched by the run's time window),
  and the `/tmp` leftovers were deleted afterwards.
- One persona-vague backend run (60663c52) patched the source in place
  to show its checks go green on fixed code (a control), then restored it and confirmed with diff.

## Threats to validity

n = 2 per arm; ceiling on metrics 1–2; I wrote the fixtures, key and briefs (the brief checklist
comes from upstream templates but can point at defect classes); one model; the UI role ran
without the sandbox; the grader is the same model family as the verifier.

## Cost

Runs $6.44, grading $2.82, pilots $1.23 + $0.33 grading: **total $10.82**.

## What this does and does not show

It does not support "a persona makes a better verifier" or "a structured brief finds more
defects" on these fixtures: a plain Sonnet 5 session with tools, told only "the feature is done,
can you check it", already distrusted the maker, ran things and found nearly everything. It does
show that the brief changes *method* (end-to-end service checks instead of unit-level probes)
and that the evidence-collector persona, used alone, does not produce the verdict a gate needs.
Harder fixtures (below the ceiling) are needed to test the upstream claim on quality.
