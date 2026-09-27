# Round 4 results: maker tasks, persona × structured brief

Pre-registration: [`EXPERIMENT.md`](EXPERIMENT.md) (committed before any scored run, pilot log
inside). Model `claude-sonnet-5`, 2 roles × 4 arms × 2 runs = 16 runs, all completed on the first
attempt, all scored by held-out checks the model never saw. Total cost **$9.13** (pilots $0.53,
runs $8.61, no LLM grading).

## Headline

**Round 4 did not get below the ceiling.** The none + vague pilots already scored at the maximum
(security 6/6, performance 274× against a 227× reference fix), and the 30-minute task limit left
no time to recalibrate, as recorded in EXPERIMENT.md before the scored runs. Under the
pre-registered rule (≥ 1.5× speedup ratio, ≥ 2 exploits):

- **Persona: no difference** (performance ×1.22 given vague, ×0.94 given brief; security +0.5 at
  both brief levels).
- **Brief: no difference** on the primary metric (performance ×1.05 / ×0.82; security −0.5 at
  both persona levels), and one clear **harm**: see below.
- **Both together vs neither: no difference** (performance ×0.99, security 0).
- **Cost:** persona roughly doubles it, brief roughly doubles it, both together ≈ 3–4×.

n = 2 per arm: directional only.

## 2×2 per role

### performance (speedup on the held-out benchmark, gate: byte-identical report)

| | vague | brief |
|---|---|---|
| **no persona** | 274× · 158× — mean 216×, $0.22 | 225× · 230× — mean 227×, $0.43 |
| **persona** | 248× · 277× — mean 263×, $0.49 | 218× · 211× — mean 215×, $0.85 |

Every run passed the gate and fixed the N+1, the report mutex and the per-order regexp compile;
the spread (7.7–13.5 ms) is within what the remaining small costs and measurement noise explain.
Every run, in every arm, measured before its first edit: the vague one-sentence prompt was
enough to make the model benchmark first.

### security (held-out exploits blocked /6, gate: functional tests)

| | vague | brief |
|---|---|---|
| **no persona** | 5 · 6 — $0.32 | 5 · 5 — $0.55 |
| **persona** | 6 · 6 — $0.57 | 5 · 6 — $0.87 |

No run broke legitimate behaviour (16/16 passed the functional gate). The only misses:
- **S6 (timing-unsafe token compare) in 3 of 4 brief runs, none of the 4 vague runs.** All three
  saw it and left it on purpose, quoting the brief: *"I couldn't produce a working timing exploit
  … so per the 'no evidence, no report' rule I left it as-is"*. The brief's evidence rule, written
  to stop unsupported claims, also stopped a correct defence-in-depth fix. This is the round's
  clearest signal, and it is a cost of the brief, not a gain.
- S1 (IDOR on `GET /notes/{id}`) in one none + vague run.

## Method differences (from transcripts)

- **The brief made the security runs exploit the running service.** curl calls per run: 0–5
  (vague, no persona), 0 (persona + vague), 9–15 (brief), 9–20 (persona + brief). Started the
  service: 2/4 vague, 3/4 brief. The persona alone did not do this: both persona + vague security
  runs wrote tests only.
- **The persona made runs longer, not different.** Bash calls, performance: 9 (none + vague),
  25.5 (persona + vague), 21.5 (brief), 33.5 (both). The extra work did not change the score.
- Performance runs measured before the first edit in 8/8 runs; the brief added nothing here.

## Compared with rounds 1–3

Same shape as round 3: at the ceiling, no factor moves the result; the brief changes the method
(running service, curl), and a role instruction can have a side effect (round 3: the
evidence-collector persona withheld the verdict; round 4: the brief's evidence rule withheld a
fix). A real test below the ceiling needs harder fixtures and an iterative pilot, which needs a
longer task limit than 30 minutes.

Data: `analysis.json` (per run: score, cost, method), `scores/`, `outputs/<id>/` (report, scrubbed
transcript, final source tree), `idmap.json` (arm per id).
