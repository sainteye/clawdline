# Persona blind test, round 2

Round 1 (`../README.md`) found no gain from the right persona, but its fixtures were too easy:
the model without a persona was at or near the ceiling. Round 2 asks the same question with
three changes:

1. **Harder fixtures, calibrated first.** Each fixture was tuned with single no-persona pilot
   runs until the model found about 50–75 % of the planted items. The pilots are excluded from
   the results.
2. **A wrong-role control.** Each role also runs with a plausible but mismatched persona of
   similar length, which separates "the right role" from "any extra system prompt".
3. **Transcripts.** Every run's full tool-call transcript is kept, so claims such as "I ran the
   test first" can be checked.

**Short answer: no consistent persona effect.** On the pre-registered primary metric, the right
persona beat both no persona and the wrong persona in one role (accessibility: 10 and 11 of 11,
against 7 and 8, and against 5 and 7). It lost to both in another (minimal-change: both right
runs took a tempting broad fix and failed the held-out tests, while all four other runs
passed). It showed no consistent difference in code-reviewer and seo. One of the seo right
runs did no work at all. The wrong personas were never better than no persona, and the
social-media persona invented a fact in both of its seo runs. With n = 2 per arm, nothing is
significant (smallest p = 1/6 one-sided). This is a directional replication of round 1's
"no general gain", with a role-specific gain and a role-specific loss.

| Role | Primary metric | none | right | wrong |
|---|---|---|---|---|
| code-reviewer | defects found /11 | 9, 6 | 9, 8 | 7, 9 |
| accessibility | problems found /11 | 7, 8 | 10, 11 | 5, 7 |
| seo | problems fixed /12 | 9, 11 | 0, 12 | 10, 10 |
| minimal-change | held-out tests pass; unrelated edits | pass, pass; 1, 1 | fail, fail; 1, 1 | pass, pass; 1, 1 |

Total cost, including pilots and grading: ≈ $18.34. All 307 graded items were hand-checked
before unblinding, and 5 were corrected.

## Design in one paragraph

There are four roles: code-reviewer, minimal-change, seo and accessibility. Each role has one
fixture with an answer key written before any run, and one under-specified English prompt.
Each prompt runs in three arms: no persona (`none`), the role's persona (`right`) and a
mismatched persona (`wrong`), with 2 runs per arm, for 24 runs in total. The model is Claude
Sonnet 5 (`claude-sonnet-5`), the same as in round 1. Every run uses `--safe-mode`, a fresh
temp copy of the fixture, and the persona file appended with `--append-system-prompt-file`,
exactly as Clawdline injects it. Outputs are stored under random ids and graded blind by a
separate model call that sees only the key. I then hand-checked every graded item before
unblinding. The pre-registration, including the pilot log, is [`EXPERIMENT.md`](EXPERIMENT.md).
Results and caveats are in [`results.md`](results.md).

| Role | Fixture | Wrong persona |
|---|---|---|
| code-reviewer | Go wallet service; an 800-line PR with 11 planted defects and 3 decoys | technical-writer |
| minimal-change | Go parcel-rate package (858 lines); a boundary bug whose tempting fix changes a shared helper; held-out tests | architect |
| seo | 13-page bilingual static site; 12 planted problems, 3 decoys | social-media |
| accessibility | Vanilla-JS booking SPA (1183 lines); 11 planted problems, 3 decoys; the prompt does not say "accessibility" | brand-guardian |

## Layout

| Path | What |
|---|---|
| `EXPERIMENT.md` | Pre-registration, pilot calibration log and freeze |
| `results.md` | Results, hand-check, process checks, threats, cost |
| `run.mjs` | The runner (an extended copy of `../run.mjs`): pilot, run, grade, analyze |
| `fixtures/<role>/` | What each run gets a copy of (original code and content, MIT) |
| `fixtures-src/code-reviewer-base/` | The pre-PR Go project; `pr.diff` = base → `fixtures/code-reviewer` |
| `fixtures-hidden/minimal-change/` | Held-out tests, copied in only after the run |
| `keys/<role>.json` | Answer keys: planted items, decoys, severities, what counts |
| `prompts/<role>.txt`, `prompts/grader-*.md` | The exact task prompts and grader prompts |
| `personas/` | Snapshot of the 8 injected persona files (4 right, 4 wrong); sha256 in `environment.json` |
| `pilot/<role>-p<n>/` | Calibration pilots (no persona; excluded from results) with their grades and transcripts |
| `outputs/<id>/` | Blinded outputs: `output.md`, plus `changes.diff` and `tests.txt` for editing roles |
| `transcripts/<id>.jsonl` | Whitelisted, scrubbed stream-json transcripts |
| `raw/<id>.json` | Per-run metadata: attempts, turns, tokens, cost, duration, permission denials |
| `grades/<id>.json` / `.final.json` | Grader output / hand-checked grade with corrections |
| `idmap.json` | id → role, arm, rep and run order (the unblinding key) |
| `environment.json`, `analysis.json` | Exact environment; computed per-arm metrics and cost |

## Re-running

You need the `claude` CLI (logged in), `git`, `go` 1.22+ and Node 18+. Round 1 is unaffected
and still re-runs from the parent directory with `node run.mjs …`.

```sh
cd round2
export PERSONA_CATALOG_COMMIT=$(git -C <clawdline checkout> rev-parse HEAD)   # optional, recorded
mv idmap.json outputs raw transcripts grades analysis.json environment.json /some/archive/   # start clean
node run.mjs run     --model claude-sonnet-5 --reps 2 --concurrency 4
node run.mjs grade   --model claude-sonnet-5
# hand-check grades/<id>.json against outputs/<id>/ and keys/ BEFORE opening idmap.json,
# then write grades/<id>.final.json
node run.mjs analyze
# calibration pilots (not part of the results):
node run.mjs pilot --role seo --label p9 && node run.mjs grade --pilot seo-p9
```

`run` is resumable. It skips ids that already have an `outputs/<id>/output.md`. The console
prints only ids, never an arm. `--personas DIR` points at another directory of rendered
persona files, for example the daemon's own.

## License and privacy

Fixtures, keys, prompts and the runner are original work for this experiment and are MIT
licensed, like the rest of the repository. The persona files are Clawdline's own, adapted from
agency-agents (MIT), with the attribution inside each file.

Transcripts keep only a whitelist of fields: model, tools, message content and the final
result. Session ids, socket and plugin paths and rate-limit data are dropped. Local paths
(`<workdir>`, `~`), the username and the host name are replaced in every output, diff and
transcript. Before publishing, the whole directory was scanned for home paths, usernames, host
names, email addresses and tokens (see `results.md`).
