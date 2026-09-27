# Round 4 pre-registration: maker tasks below the ceiling, persona × structured brief

Committed before any scored run. The pilot log at the end is the only part written after that
commit's content was drafted; it records the none + vague pilots and any change they caused.

## Question

Rounds 1–3 hit the ceiling: without a persona and with a vague prompt the model already found
7–9 of 9 defects. Round 4 asks, on tasks with a **continuous or held-out** metric and no easy
ceiling: does a matching persona improve a **maker** session's result, does a structured brief,
and do the two together?

## Roles and fixtures

Two roles not tested before, both makers (they change code), both Go, stdlib only:

1. **`performance`** — `fixtures/performance/` (`ordersvc`): `GET /report` builds a report of
   1,000 orders. `store.go` simulates a database round trip (100 µs sleep) per store call. Planted,
   independent problems of different depth (key: `keys/performance/key.json`):
   P1 N+1 (`GetUser` per order although `GetUsers` batches), P2 a package-level mutex held across
   the whole report including store round trips (only visible under concurrency), P3 two
   `regexp.MustCompile` per order, P4 a quadratic de-duplication, P5 a full copy of the orders per
   request (minor). A load script (`go run ./loadtest`) is included.
2. **`security`** — `fixtures/security/` (`notesapi`): a notes API with planted vulnerabilities
   (key: `keys/security/key.json`): S1 IDOR on `GET /notes/{id}` only (PUT checks ownership),
   S2 `DELETE /notes/{id}` with no authentication, S3 the same route with no ownership check,
   S4 a path traversal that survives a naive prefix check (`docs/../../secret/signing.key`),
   S5 mass assignment on `PUT /me` (role, id), S6 a timing-unsafe admin-token compare. Decoys:
   an SQL-looking audit log line (no database), `math/rand` for a cosmetic colour, md5 for an
   ETag, `Access-Control-Allow-Origin: *` on `/health` only. No SSRF: the sandbox is
   localhost-only, so a redirect-based SSRF cannot be exploited or tested honestly here.

The optional third role (technical-writer) is **skipped**: the task has a 30-minute wall-clock
limit, and two roles already fill it.

## Metrics (all computed by `run.mjs score`, never shown to the model)

- **performance, primary:** speedup = baseline ms / after ms on the held-out benchmark
  (`keys/performance/heldout_test.go`: 16 requests at concurrency 8 through `NewServer`, median of
  3 rounds; baseline 2135 ms on this machine), run sequentially after all sessions ended.
  **Gate:** `go vet`, the fixture's own tests, and a byte-identical report (sha256 of the body for
  two store sizes) must pass; otherwise the speedup scores **0**.
- **security, primary:** held-out exploits blocked, /6 (`keys/security/heldout_test.go`: five
  exploit tests, plus S6 as a static check for `subtle.ConstantTimeCompare` or `hmac.Equal`).
  **Gate:** three held-out functional tests (owner flow, admin, profile + files), `go vet` and the
  fixture's own tests must pass; otherwise the score is **0**.
- **Method (from transcripts, by script):** Bash calls, edits, curl calls, whether the service was
  started, whether a measurement (load test / benchmark) was run, and whether one was run before
  the first edit. Cost.

## What counts as "a difference" (fixed now)

- performance: a ratio of arm-mean speedups **≥ 1.5×** (or ≤ 1/1.5).
- security: a difference of arm means of **≥ 2 exploits blocked**.
- Effects reported: persona given vague, persona given brief, brief given no persona, brief given
  persona, and both vs neither. n = 2 per arm: every result is **directional only**.

## Arms (2×2), 2 runs each, 16 runs

1. none + vague — `prompts/<role>-vague.txt`, one sentence.
2. persona + vague — the same, with `personas/<role>.md` appended.
3. none + brief — `prompts/<role>-brief.txt`: objective, scope, acceptance criteria, evidence
   required before claiming done, deliverable. Generic to the task; names no planted problem.
4. persona + brief.

Personas are the rendered text Clawdline injects (preamble + catalog body + attribution), from
`internal/domain/persona/catalog/{performance,security}.md` at commit `af9e493e`; sha256 in
`environment.json`.

## Run setup

`claude -p`, model `claude-sonnet-5`, `--safe-mode`, `--no-session-persistence`, tools Read Grep
Glob Edit Write Bash, `--max-budget-usd 2` per run, Claude Code sandbox with network limited to
localhost (`sandbox.json`), a fresh copy of the fixture per run, a per-run `ADDR` port so all 16
runs can run in parallel (speed is measured afterwards, not during). Stream-json transcripts are
kept, scrubbed (paths, user, host, any UUID → `5e550000-…` fixture ids). Run order is a seeded
shuffle (`idmap.json`).

## Calibration rule

Pilot **only none + vague**, one run per role. Target: that arm scores roughly 30–60% of the
maximum (security: 2–4 of 6; performance: clearly below the reference, see pilot log). If a pilot
is far outside, change the fixture's difficulty once, never toward the persona or brief arms.
Then freeze fixtures, keys and prompts, commit, run.

## Deviations from the brief, stated up front

- No LLM blind grading. Both primary metrics are held-out and scripted; "problems fixed" is
  derived from the held-out sub-checks. Claims in reports are not graded (time limit).
- One pilot per role, not iterative calibration: the 30-minute task limit allows one adjustment
  at most.

## Pilot log

Reference fix (`keys/performance/reference.diff`: batch the users, drop the report mutex,
compile the regexps once, map-based de-duplication): 9.4 ms, speedup 227×.

| Pilot | Arm | Score | Cost |
|---|---|---|---|
| `performance-p1` | none + vague | 7.8 ms, **274×** (gate passed) — at or above the reference | $0.23 |
| `security-p1` | none + vague | **6/6** exploits blocked, all functional tests pass | $0.30 |

**Both pilots are at the ceiling, outside the 30–60% target.** Recalibrating means harder
fixtures and a second pilot, which does not fit the task's 30-minute wall-clock limit (the pilots
ended 9 minutes in). Decision, made before any scored run: freeze as is and run the 16 runs
anyway. Consequences stated now: security can show a difference only if some arm *drops*
below 6/6 (false fixes, broken functional tests); performance is continuous, so a ≥1.5× ratio
between arms can still appear, but the none + vague arm is already at the reference. A clean
below-the-ceiling test needs a round 5 with harder fixtures. No fixture, key or prompt changed
after the pilots.
