# Persona blind test

Does giving a Claude session the *right* built-in persona make its work measurably better on a
realistic, under-specified request?

**Short answer from this run: no measurable gain.** Four roles were tested with Claude Sonnet 5,
2 runs per arm. On the pre-registered primary metric, the persona arm was never better. It was
one item lower in both runs for code-reviewer and accessibility, and identical for
minimal-change and seo. Without a persona the model already hit or neared the ceiling on these
fixtures. The personas consistently changed the *shape* of the answer (ranked verdicts,
"verified correct" lists, WCAG criteria, stated test gaps, scope notes) and cost 30–65 % more.
Full details, the hand-check, excerpts and caveats are in [`results.md`](results.md).

| Role | Primary metric | no persona | persona | False alarms / damage | Cost per run |
|---|---|---|---|---|---|
| code-reviewer | planted defects found /7 | 7, 7 | 6, 6 | 0 in all runs | $0.20 → $0.32 |
| accessibility | planted problems found /8 | 8, 8 | 7, 7 | 0 in all runs | $0.10 → $0.16 |
| minimal-change | tests pass / unrelated edits | pass, 0 / pass, 0 | pass, 0 / pass, 0 | identical 7-line diffs | $0.08 → $0.10 |
| seo | planted problems fixed /8 | 7, 7 | 7, 7 | 0 regressions, 0 invented | $0.18 → $0.25 |

n = 2 per arm is a demonstration, not proof. See "Threats to validity" in `results.md`.

## Rounds

| Round | What changed | Result | Details |
|---|---|---|---|
| 1 | Four small fixtures, none vs right persona | No gain; the no-persona runs were at or near the ceiling | this directory: [`results.md`](results.md) |
| 2 | Harder fixtures calibrated by pilots, a wrong-persona control, transcripts | No consistent effect: right better in accessibility, worse in minimal-change, no difference in code-reviewer and seo | [`round2/`](round2/README.md): [`results.md`](round2/results.md) |
| 3 | Upstream setup: a separate verifier with tools (tests, server, curl, headless browser), 2×2 persona × structured brief | All 16 runs refused to pass and found 7–9/9 defects; no factor moved a primary metric (ceiling). The brief changed method; the evidence-collector persona alone gave no verdict | [`round3/`](round3/README.md): [`results.md`](round3/results.md) |

**Combined headline:** across three rounds, a matching persona never reliably improved the
result. Rounds 1–2 (one session, vague prompt, few tools): no gain in round 1, one role better and
one worse in round 2. Round 3 (the upstream way: a separate verifier with real tools, persona ×
structured brief): the plain verifier was already at the ceiling, so neither persona nor brief
could show a gain; the brief changed how it verified, and one persona (evidence-collector) on its
own withheld the verdict a merge gate needs. Personas reliably change style and usually cost.
Every comparison is n = 2 per arm: directional, not significant.

## Design in one paragraph

Each role gets its own small fixture with an answer key written first, plus one under-specified
English prompt (e.g. *"Can you take a look at this PR and give me some feedback? pr.diff"*).
Each prompt is run with no persona and with the role's persona file, appended through
`--append-system-prompt-file` exactly as Clawdline injects it. Every run uses a fresh temp copy of
the fixture and `--safe-mode`, so no local CLAUDE.md, skills or hooks leak in. Outputs are stored
under random ids and graded blind by a separate model call. Every grading was then hand-checked
(156 items, 5 corrected). The pre-registration is [`EXPERIMENT.md`](EXPERIMENT.md).

## Layout

| Path | What |
|---|---|
| `EXPERIMENT.md` | Pre-registration: design, prompts, metrics, hypotheses (written before any run) |
| `results.md` | Results, hand-check, excerpts, what it does and does not show, threats, cost |
| `run.mjs` | The runner: runs, blinding, grading and analysis |
| `fixtures/<role>/` | What each run gets a copy of (original code and content, MIT) |
| `fixtures-src/code-reviewer-base/` | The pre-change Go project; `pr.diff` = base → `fixtures/code-reviewer` |
| `keys/<role>.json` | Answer keys (planted items, decoys, severities, what counts) |
| `prompts/<role>.txt` | The exact task prompts |
| `prompts/grader-*.md` | The grader prompts |
| `personas/<role>.md` | Snapshot of the injected persona text (see sha256 in `environment.json`) |
| `outputs/<id>/` | Blinded outputs: `output.md`, plus `changes.diff` / `tests.txt` for editing roles |
| `raw/<id>.json` | Per-run metadata: attempts, turns, tokens, cost, duration |
| `grades/<id>.json` / `.final.json` | Grader output / hand-checked grade with corrections |
| `idmap.json` | id → role, arm, rep and run order (the unblinding key) |
| `environment.json`, `analysis.json` | Exact environment; computed per-arm metrics and cost |
| `go.mod` | Keeps the Go fixtures (some deliberately broken) out of the Clawdline module, so its `go test ./...` never builds them |

The prompts are English equivalents of the Traditional Chinese originals in the first brief
(e.g. 「幫我看一下這個 PR，給點意見。」 → "Can you take a look at this PR and give me some
feedback?"). The whole experiment was run in English.

## Re-running

You need the `claude` CLI (logged in), `git`, `go` 1.22+ and Node 18+.

```sh
# optional: use personas from elsewhere (defaults to ./personas, the snapshot used here)
#   --personas ~/.config/clawdline-next/personas     (the daemon's rendered files)
# record which catalog commit the personas came from:
export PERSONA_CATALOG_COMMIT=$(git -C <clawdline checkout> rev-parse HEAD)

mv idmap.json outputs raw grades analysis.json environment.json /some/archive/   # start clean
node run.mjs run     --model claude-sonnet-5 --reps 2 --concurrency 4
node run.mjs grade   --model claude-sonnet-5
# hand-check grades/<id>.json against outputs/<id>/ and keys/, then write grades/<id>.final.json
node run.mjs analyze
```

`run` is resumable: it skips ids that already have an `outputs/<id>/output.md`. The persona
files must be the rendered form that the daemon injects (preamble + body + attribution), not the
catalog source with front matter.

## License and privacy

Fixtures, keys, prompts and the runner are original work for this experiment and are MIT
licensed like the rest of the repository. The persona files are Clawdline's own, adapted from
agency-agents (MIT), with attribution inside each file. Outputs were scrubbed of local paths
(`<workdir>`, `~`), and the directory was checked for usernames, home paths, host names and
email addresses before publishing.
