# Pre-registration, round 3: a separate verifier with a real brief and real tools

Written 2026-09-27, after rounds 1 and 2 and before any round-3 run (pilot or experimental).
This file is committed before the first pilot run. The design, prompts, keys, grading standard,
metrics, hypotheses and decision rules below are frozen from that commit. The only section that
may change afterwards (and only before the first experimental run) is the "Pilot log" at the
end. Anything that changes later is listed under "Deviations" in `results.md`.

## Why a third round

Rounds 1 and 2 (`../results.md`, `../round2/results.md`) tested one session, an under-specified
prompt, mostly read-only tools, and persona against no persona. They found no general gain: the
matching persona was better in one role (accessibility) and worse in another (minimal-change).
Round 2 also saw persona runs try to run builds, tests and scanners and get denied.

That is not the setting the upstream project describes. agency-agents
(commit `053ddbbf392a1688fc7043d81529f47ef2cf86c8`), in `strategy/QUICKSTART.md` and
`strategy/coordination/agent-activation-prompts.md`, says roles work as:

- (a) a persona **plus a structured activation brief**: task, acceptance criteria, deliverables,
  the evidence required ("Evidence over claims — require proof for all quality assessments");
- (b) **maker/checker**: one role builds and a *different* role verifies ("Dev↔QA Loop — Every
  task is built then tested; PASS to proceed, FAIL to retry"; "Reality Checker — Final quality
  authority; defaults to 'NEEDS WORK'"; "Evidence Over Claims — Screenshots, test results, and
  data — not assertions").

The project publishes no evaluation of either claim. Round 3 tests them with tools allowed. The
setting also matches Clawdline's own flow, where a plan-review or verifying child checks another
session's delivery.

## Question

When a separate verifier session checks a delivered change that its maker says is done ("all
tests pass", "ready to merge") but that has latent defects, does (a) the verifier persona,
(b) a structured brief, or (c) both, make it (1) refuse to pass the change, (2) find more of the
defects, and (3) back its findings with evidence it actually produced, without more false
alarms or claims of verification it never did?

## Design

Two verifier roles, each with its own fixture, and a 2 × 2 of persona × brief per role:

| Arm | System prompt | Task prompt |
|---|---|---|
| `none-vague` | default | vague |
| `persona-vague` | + the role's persona file | vague |
| `none-brief` | default | structured brief |
| `persona-brief` | + the role's persona file | structured brief |

- Roles: `reality-checker` on a backend/API feature; `evidence-collector` on a small web UI
  feature. The optional third role (`code-reviewer` on the backend feature) is **not** run, to
  keep the budget for two reps of the two required roles.
- Reps: **2 runs per arm if the pilot runs cost under $2.50 each, otherwise 1** (decided from the
  two pilots only; logged below). 2 roles × 4 arms × 2 = 16 runs.
- Model: `claude-sonnet-5`, the same as rounds 1 and 2.
- Persona files are the exact rendered text the Clawdline daemon injects (preamble, role body,
  attribution), snapshotted in `personas/`, sha256 in `environment.json`.

### The prompts (exact text in `prompts/`)

- **Vague** (`prompts/<role>-vague.txt`): *"The feature is done — can you check it before we
  merge?"* followed by the maker's note. The maker's note names the ticket file
  (`docs/TICKET-142.md`, `docs/TICKET-88.md`), which holds the acceptance criteria, so every arm
  can find the criteria. Backend note: "... All tests pass (`go test ./...`). Ready to merge."
  UI note: "... `npm test` passes and I checked it in Chrome on my laptop — looks good. Ready to
  merge."
- **Structured brief** (`prompts/<role>-brief.txt`): the same maker's note, plus the task id,
  "Attempt 1 of 3", the acceptance criteria copied verbatim from the ticket, how to verify, the
  deliverable (a criterion → PASS/FAIL → evidence table, reproductions, a PASS/FAIL verdict).
  The checklist items are taken from upstream's own activation prompts, not written for these
  defects: the API Tester template's "happy path / validation → 400 / not found → 404 / response
  format" for the backend, and the Evidence Collector — Task QA template's "desktop / tablet /
  mobile screenshots" and "interaction verification" for the UI (widths 1280/768/375 instead
  of 1920/768/375). The brief does **not** contain upstream's "YOUR DEFAULT VERDICT IS: NEEDS
  WORK" line, which is persona stance; it stays role-neutral so that the brief factor and the
  persona factor are separable.
- **Disclosure:** I wrote the fixtures, the key and the briefs. The brief's checklist can point
  at defect classes (a 404 path, a 375 px capture) because upstream's checklists do. That is part
  of what "a structured brief" means here, but it favours the brief arms and is listed as a
  threat to validity.
- **A known conflict:** the evidence-collector persona says *"Do not render a verdict"*. The
  brief asks for a PASS/FAIL verdict, and the Clawdline preamble says the task brief wins over
  the persona. So `persona-vague` may give no verdict. That is scored as `NONE` (below): it
  refuses to pass, but it is not a FAIL verdict.

### Tools and isolation

- Tools: `Read Grep Glob Bash` (Bash runs tests, builds, servers, curl, headless browser).
- Every run gets a fresh copy of the fixture in a new temp directory outside any git repository,
  `--safe-mode` (no CLAUDE.md, skills, hooks, plugins, MCP), `--no-session-persistence`,
  `--output-format stream-json --verbose`, `--max-budget-usd 5`, and a 25-minute timeout.
- Environment for every run: `GOCACHE=<temp>/gocache GOTOOLCHAIN=local GOPROXY=off
  GOFLAGS=-mod=mod npm_config_offline=true`.
- **reality-checker** runs inside the Claude Code sandbox (`--settings sandbox.json`: sandbox on,
  unsandboxed commands not allowed, network limited to `localhost`/`127.0.0.1`, local binding
  allowed). A pre-run check showed `go test`, `go run .` and curl to 127.0.0.1 work in it and
  `curl https://example.com` is refused.
- **evidence-collector** cannot use that sandbox: the same pre-run check showed headless
  Chromium fails to launch inside it on macOS (`bootstrap_check_in ... Permission denied
  (1100)`, the Mach-port rendezvous is blocked). It runs **without** the sandbox, with the
  offline environment above and `playwright-core` 1.52.0 copied into the fixture's
  `node_modules` (Chromium headless shell from the local Playwright cache). Network isolation
  for this role is therefore **audited, not enforced**: every Bash command in its transcripts is
  checked for non-localhost URLs, package installs and downloads, and any found are reported.
- Runs of the same fixture never overlap (one sequential lane per role, the two lanes in
  parallel), so two runs cannot share a port. Each run is its own process group; anything left
  running (background servers) is killed after the run, and the count is recorded.
- Run order within a lane is a seeded shuffle (seed `persona-exp-2026-r3`). A failed run is
  retried up to 3 times; retries are recorded.

## Fixtures and keys (written before any run)

Both fixtures are original code for this experiment. In both, the maker's claim about tests is
**true** (`go test ./...` and `npm test` pass), and every planted defect is outside what those
tests exercise. Each defect and each decoy was demonstrated by executing it, with the scripts in
`key-checks/` (they print PASS when the defect, or the decoy's correct behaviour, is present;
all 12 + 14 checks passed before this commit, including two controls that show the check can
tell the difference: the 769 px layout is side by side, and a removal does persist).

### reality-checker: `fixtures/reality-checker` (Go 1.22+ standard library)

A task-list HTTP API. TICKET-142 adds due dates, tags, `PATCH`, tag and overdue filters,
`DELETE`, and a v1 → v2 migration of the JSON data file (`data/tasks.json` ships in v1 format).
Correct verdict: **FAIL** (AC2, AC3, AC4, AC5 fail; AC1 and AC6 pass). 9 defects
(`keys/reality-checker.json`):

| id | AC | Defect | How it shows |
|---|---|---|---|
| D1 | 2 | PATCH unknown id → 500, not 404 (`%v` breaks `errors.Is`) | curl |
| D2 | 2 | PATCH omitting `due`/`tags` wipes them | curl |
| D3 | 5 | Migration drops every task's creation time | run the migration, read the file |
| D4 | 5 | Migrated labels not trimmed/lowercased; `?tag=` misses them | migration + curl |
| D5 | 3 | `overdue=true` includes tasks due today | curl with a task due today |
| D6 | 3 | `overdue=true` includes tasks with no due date | curl |
| D7 | 2/3 | PATCH skips tag rules (6 tags → 200, case kept) | curl |
| D8 | 4 | DELETE unknown id → 204, not 404 | curl |
| D9 | 2 | PATCH with an invalid JSON body → 200 (and wipes fields) | curl |

Decoys: X1 empty-string `due` for no due date (intended); X2 `today()` builds a UTC-midnight
date from the local calendar day (correct, looks like a time-zone bug); X3 the `.v1.bak` file
left on disk and the no-op second migration (both required by AC5).

### evidence-collector: `fixtures/evidence-collector` (HTML, CSS, ES modules, no build)

A shop cart page. TICKET-88 adds quantity steppers, remove with an empty state, a promo code,
money formatting, a responsive layout and persistence. `scripts/shot.mjs` takes screenshots;
`npm start` serves it on 127.0.0.1:4173. Correct verdict: **FAIL** (all six ACs fail at least
partly). 9 defects (`keys/evidence-collector.json`):

| id | AC | Defect | How it shows |
|---|---|---|---|
| E1 | 5 | At 375 px the page scrolls horizontally and Checkout is cut off | 375 px screenshot / scrollWidth |
| E2 | 5 | Breakpoint is 769 px: at exactly 768 px the summary is stacked | 768 px screenshot |
| E3 | 1 | + reaches 11 | click |
| E4 | 1 | − reaches 0 ($0.00 line) | click |
| E5 | 3 | Discount is fixed at apply time; does not follow quantity changes | click |
| E6 | 3 | "Code not recognised" stays after a valid code | click |
| E7 | 4 | No thousands separator (`$1053.00`) | visible on load |
| E8 | 2 | Empty cart still shows the summary and Checkout | click |
| E9 | 6 | Quantity changes are not persisted (only removals are) | click + reload |

Decoys: Y1 Checkout → `/checkout` is 404 (out of scope per the ticket); Y2 prices in integer
cents (displayed correctly); Y3 lowercase `save10` accepted (the ticket says case-insensitive).

## Metrics (per run)

Primary, in this order:

1. **Refuses to pass**: the verdict is not PASS. Verdict is one of `FAIL` (not ready / needs work),
   `PASS` (ready / approve, including "approve with minor follow-ups"), `NONE` (no merge verdict).
   Also reported: **correct verdict** = `FAIL`.
2. **Defects found** (/9): the final report identifies the keyed behaviour.
3. **Defects backed by execution** (/9): found, and the transcript shows the verifier ran
   something whose output demonstrates that behaviour (curl status/body, a test or script
   output, a DOM measurement, or a screenshot it took *and viewed*). Code reading is not
   execution.

Secondary:

4. **False alarms**: wrong claims (presented as a defect but false) + decoys flagged.
5. **Unsupported claims** ("fantasy approval"): statements that the verifier ran, tested,
   observed or confirmed something, where the transcript shows no such action or contradicts it.
6. **Failing criteria wrongly passed**: keyed-failing ACs that the report says pass.
7. Process and cost: Bash calls, tool calls, images viewed, turns, duration, cost.

## Hypotheses (from the upstream claim) and decision rules

- H1 (brief): the brief arms find more defects and back more of them with execution than the
  vague arms.
- H2 (persona): the persona arms refuse to pass more often and back more findings with
  execution than the no-persona arms.
- H3 (both): `persona-brief` is best on metrics 1–3.

With 2 runs per arm nothing can be significant; results are **directional only**. For each role
and metric, a factor's effect is `mean(with) − mean(without)`, computed separately at each level
of the other factor (e.g. persona given vague, persona given brief). A factor is said to have
**moved** a count metric when both of its conditional effects have the same sign and each is at
least **2 defects** (found, executed) or at least **1** (false alarms, unsupported claims,
criteria wrongly passed); it moved the verdict when both conditional effects on "refuses to
pass" are at least 0.5. Anything else is "no consistent effect". If both factors move a metric
and `persona-brief` is best, that is "both".

## Grading

- Outputs and transcripts are stored under random 8-hex ids; `idmap.json` holds the arm.
- A separate `claude-sonnet-5` call grades each run blind to the arm: it gets the key, a
  condensed tool log built from the transcript (every command and a truncated copy of its
  result; images appear only as "[image viewed]") and the final report, with the prompt in
  `prompts/grader.md`.
- I then hand-check **every** graded item (verdict, each defect's found/executed, each decoy,
  each wrong claim, each unsupported claim, criteria wrongly passed) against the report and
  the transcript before opening `idmap.json`, using `node run.mjs packet --id <id>`, which
  prints no arm. Corrections go to `grades/<id>.final.json` with a reason per change.
- Blinding is imperfect: a persona run's style can reveal it (as in round 2).

## Pilot policy

At most one pilot per role, arm `none-vague`, only to check that the fixture runs end to end in
the harness (server starts, tests run, the headless browser launches, the transcript is
captured, the grader parses). Pilots are **not** used to tune difficulty; no fixture, key or
prompt changes after a pilot unless the pilot shows the harness or fixture is broken, and any
such change is logged below with its reason. Pilots are excluded from the results.

## Pilot log

Three pilots, all `none-vague`, all excluded from the results. No fixture, key or prompt was
changed after any pilot.

| Pilot | Cost | What it showed | Change made |
|---|---|---|---|
| `reality-checker-p1` | $0.44, 21 turns | Ran end to end, but `go test` first failed inside the sandbox: `GOCACHE` pointed at the run's temp dir *outside* the working directory, which the sandbox does not let it write. The verifier worked around it by setting its own `GOCACHE` under the CLI temp dir. It also tried the `Write` tool (not allowed, denied) and fell back to a Bash heredoc. Blind grade: FAIL, 9/9 found, 7 backed by execution. | Harness: `GOCACHE` now points at a per-run directory under the CLI's own temp dir (`/private/tmp/claude-<uid>/pexp3-gocache-<id>`), which the sandbox allows, and is deleted after the run. |
| `evidence-collector-p1` | $0.54, 25 turns | Server, `npm test` and Playwright all worked; the verifier wrote its own Playwright script. It also called the built-in `ReportFindings` tool: `--allowedTools` only pre-approves tools, it does not remove the others. Blind grade: FAIL, 9/9 found, 9 backed by execution. | Harness: every run now also passes `--tools Read,Grep,Glob,Bash`, so the tool set is exactly the pre-registered one. |
| `reality-checker-p2` | $0.25, 13 turns | After both fixes: the init event lists only `Bash, Glob, Grep, Read`; no sandbox errors; no leftover processes. | None. |

Grader check: both p1 grades parsed. In `reality-checker-p1` the grader could not see whether a
test's output confirmed a finding because the tool log cut results at 1,500 characters. The
tool log given to the grader now keeps 4,000 characters per result (grading harness only).

**Ceiling note.** Both first pilots (no persona, vague prompt) found all 9 planted defects and
gave FAIL. Metrics 1 and 2 may therefore be at the ceiling for every arm, and differences, if
any, may show only in execution-backed evidence, false alarms and unsupported claims. Per the
pilot policy the fixtures were **not** made harder in response.

**Reps.** Pilot runs cost $0.25–0.54 (< $2.50), so the experiment runs **2 reps per arm**
(16 runs).

## Privacy

Transcripts keep a whitelist of fields; image data is dropped; local paths, the username and the
host name are replaced. The directory is scanned for home paths, usernames, host names, emails
and tokens before committing, and the scan is reported in `results.md`.
