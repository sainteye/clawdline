# Persona experiment, round 3: a separate verifier

Tests upstream agency-agents' claim that roles work as persona + structured activation brief and
as maker/checker. A verifier session, with Read/Grep/Glob/Bash, checks a just-delivered feature
whose maker says "all tests pass, ready to merge" but which has 9 latent defects. Two roles
(reality-checker on a Go API, evidence-collector on a web UI with Playwright), 2×2 persona × brief,
2 runs each, `claude-sonnet-5`.

**Result:** all 16 runs refused to pass and found 7–9/9 defects, mostly with execution evidence:
no factor moved a primary metric (ceiling). The brief changed method (4/4 brief backend runs
tested the running service with curl, 0/4 vague ones did), and the evidence-collector persona with
a vague prompt gave no verdict at all (2/2). Details: [`results.md`](results.md); design:
[`EXPERIMENT.md`](EXPERIMENT.md). Cost $10.82.

Layout: `fixtures/`, `keys/`, `key-checks/` (scripts proving every defect and decoy), `prompts/`,
`personas/`, `pilot/`, `outputs/<id>/` (report + screenshots viewed), `transcripts/`, `raw/`,
`grades/` (`.final.json` = hand-checked), `idmap.json`, `analysis.json`, `run.mjs`,
`audit-network.mjs`, `sandbox.json`.

Re-run: `PW_NODE_MODULES=<node_modules with playwright-core 1.52.0> node run.mjs run`, then
`grade`, hand-check with `packet --id`, write `grades/<id>.final.json`, `analyze`.

Privacy: transcripts are whitelisted, image data dropped, paths/user/host replaced; the directory
was scanned for home paths, usernames, host names and emails before committing. MIT licensed.
