# Persona experiment round 5

Round 5 was designed to compare no persona, the pinned upstream original, Clawdline's audited
rewrite, and a structured brief on Codex. Calibration could not get the no-persona vague arm
below the ceiling: after increasing the security fixture from 12 to 22 held-out checks, both valid
runs scored 21/22 (95%). The pre-registered stop rule therefore ended the experiment before any
persona, brief, performance, or scored arm ran.

The design and calibration record are in [`EXPERIMENT.md`](EXPERIMENT.md); the result and its
limits are in [`results.md`](results.md).

## Layout

| Path | Contents |
|---|---|
| `fixtures/<role>/` | Fresh-copy inputs for each run; every Go fixture has its own `go.mod` |
| `keys/<role>/` | Held-out tests and scoring keys, never exposed during a run |
| `prompts/` | Exact vague and structured prompts |
| `personas/upstream/` | Pinned MIT upstream snapshots (security is the documented concatenation) |
| `personas/rewrite/` | Rendered audited Clawdline persona snapshots |
| `pilot/<id>/` | Pilot report, final source tree, scripted score, and metadata |
| `outputs/<id>/` | Scored-arm outputs (none exist because the stop rule fired) |
| `transcripts/<id>.jsonl` | Scrubbed Codex event stream |
| `scores/<id>.json` | Scored-arm results (none exist because the stop rule fired) |
| `run.mjs` | Isolation, execution, scrubbing, scoring, and analysis |
| `calls.json` | Append-only request ledger enforcing the approved 27-call hard cap |
| `idmap.json` | Seeded randomized order and blinded ids |
| `environment.json` | Exact CLI, model, platform, hashes, and isolation settings |
| `analysis.json`, `results.md` | Machine summary and human report |

The outputs are public-repository-safe: local paths, account names, host names, emails, and real
thread/session ids are scrubbed. Fixture ids use the `5e550000-0000-4000-8000-00000000000N`
form. The upstream snapshots remain attributed to agency-agents commit
`053ddbbf392a1688fc7043d81529f47ef2cf86c8` under MIT.
