# Persona experiment, round 4: maker tasks with held-out scores

Two roles not tested before, **performance** (make a Go endpoint faster) and **security** (find
and fix vulnerabilities in a Go API), as maker sessions with Read/Grep/Glob/Edit/Write/Bash in a
localhost-only sandbox, 2×2 persona × structured brief, 2 runs each, `claude-sonnet-5`. Scored by
held-out checks: a benchmark gated on byte-identical output, and exploit tests gated on
functional tests.

**Result:** the none + vague pilots were already at the ceiling (274× speedup, 6/6 exploits) and
there was no time to recalibrate, so round 4 did not get below it. No factor made a difference
under the pre-registered rule. The brief made security runs attack the running service (9–20 curl
calls vs 0–5), and its "no evidence, no report" rule made 3 of 4 brief runs leave a timing-unsafe
compare unfixed. Details: [`results.md`](results.md); design: [`EXPERIMENT.md`](EXPERIMENT.md).
Cost $9.13.

Layout: `fixtures/`, `keys/` (held-out tests, key, reference fix), `prompts/`, `personas/`,
`pilot/`, `outputs/<id>/` (report, transcript, final source), `scores/`, `idmap.json`,
`analysis.json`, `environment.json`, `run.mjs`, `sandbox.json`.

Re-run: `node run.mjs run`, then `score` (sequential, after all sessions), then `analyze`.
