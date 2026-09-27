# Round 5 pre-registration: upstream persona vs audited rewrite vs none on Codex

This document, the runner, fixtures, held-out keys, prompts, persona snapshots, and randomized
run map must be committed before any scored run. Pilot rows may be appended after that initial
commit, but no scored arm may run until calibration is complete and the final frozen inputs are
committed again.

## Question

Rounds 1–4 used Claude and stayed at the ceiling. A later audit found that 29 of 42 Clawdline
rewrites had weakened or reversed at least one upstream rule; those defects were repaired before
this round. Round 5 asks below the ceiling, on the actual Codex launch path:

1. Does either matching persona beat no persona on a vague task?
2. Is the audited rewrite better, worse, or equal to the upstream original?
3. Does a structured brief beat the best persona arm?

## Roles and why

- **`security`** reuses round 4's notes API, first expanded from 6 to 12 held-out exploit checks
  and then, after the first valid pilot still scored 12/12, to 22 checks across a larger API
  surface. The audit restored defence in depth, a stated
  exception for fixes that cannot have a direct exploit test, and the refusal to defer a known
  vulnerability. Its upstream arm concatenates both source files named by the catalog.
- **`performance`** reuses round 4's report service, expanded from one N+1 to three independent
  store round trips plus concurrency, allocation, regular-expression, distinct-selection, and
  JSON-encoding costs. The audit restored a verdict against the target and changed unverifiable
  numbers from suppressed to explicitly unverified.

Both roles make changes, have deterministic Go fixtures, and can be scored without another model.

## Arms

Each role has four arms, two independent runs per arm (16 scored runs):

1. `none-vague`: vague prompt, no persona.
2. `upstream-vague`: same prompt, upstream original persona.
3. `rewrite-vague`: same prompt, Clawdline's audited rewrite.
4. `none-brief`: no persona, structured objective/scope/acceptance/evidence/deliverable brief.

The brief says to mark unverified material and explain why; it does not say to stop, refuse to
act, or omit it. Run order is a seeded shuffle recorded in `idmap.json` before execution.

## Codex injection and isolation

Every model call uses `codex exec`, codex-cli 0.157.1, model `gpt-5.6-sol` named explicitly,
`--sandbox workspace-write`, `--ephemeral`, `--ignore-user-config`, `--ignore-rules`, and a fresh
temporary fixture copy outside every repository. `CODEX_HOME` is a fresh mode-0700 directory that
contains only a mode-0600 copy of the existing ChatGPT login file; it is removed after the run.
No config, history, memory, `AGENTS.md`, or custom instruction file is copied. External network is
disabled. The runner sets only fixture-safe environment values plus the inherited
`CODEX_SANDBOX=seatbelt` infrastructure marker when it is itself running inside Codex; retaining
that marker prevents codex-cli from trying to nest macOS `sandbox-exec` while the outer seatbelt
already enforces the workspace boundary.

Compatibility note recorded before any valid or persona pilot: codex-cli 0.157.1 still attempted
to nest macOS `sandbox-exec` even with the marker and the kernel rejected every tool command. The
runner therefore retains `--sandbox workspace-write` as the requested policy and also uses the
CLI's documented `--dangerously-bypass-approvals-and-sandbox` switch, which its own help limits to
already externally sandboxed environments. The effective boundary is this child's outer
workspace-write seatbelt. `GOPROXY` is off, standard proxy variables point to an unavailable
loopback endpoint with localhost exempted, and the transcript analysis records any command that
attempts non-local network access. A run with such a command is reported, not silently accepted.

The persona arms use the same mechanism as `projects.PersonaArgs`: one `-c` value whose key is
`developer_instructions` and whose TOML string is exactly `persona.CodexInstruction`, pointing at
a read-only persona file copied into that run's sandbox. Upstream security is a documented
concatenation of the AppSec Engineer and Security Architect files at agency-agents commit
`053ddbbf392a1688fc7043d81529f47ef2cf86c8`; upstream performance is the one source file at that
commit. Both are MIT. The rewrite snapshots are the rendered files Clawdline injects (preamble,
audited body, attribution), not catalog front matter.

Before any pilot, one isolation control asks Codex to print the non-system instructions and
project instruction files it received. It is inspected for local setup or private material and is
not committed if it contains any. The control is not scored and is not used to tune fixtures.

## Calibration rule

Pilot **only `none-vague`**. Run two pilots per role first. The target is an arm mean between
30% and 60% of maximum: initially security 3.6–7.2 of 12 and performance 2.4–4.8 of 8. If a role misses, adjust
only fixture difficulty, the held-out key, or scale—not the vague prompt—and run another pair.
Log every pilot and every adjustment below. Stop after about five pilots per role. If a role is
still above 60%, stop the experiment and do not run any scored arm.

After calibration, freeze fixtures, keys, prompts, runner, persona snapshots, and `idmap.json` in
a commit. Scored outputs may then be produced; frozen inputs never change in response to an arm.

## Held-out scoring and correctness gates

- **Security:** one point for each of 22 exploit checks blocked in the final calibrated fixture.
  The score is zero unless `go vet`, fixture tests, and all five held-out legitimate-flow tests pass.
- **Performance:** one point for each of eight held-out optimization checks. The score is zero
  unless `go vet`, fixture tests, and both held-out response-correctness tests pass.

Held-out tests are copied in only after Codex exits. Codex never sees `keys/` or this document.
The runner also records command executions, file changes, tests/measurements before the first
edit, duration, and token usage when Codex reports it.

## What counts as a difference

A comparison is called a **difference** only when the arm means differ by at least **2 held-out
points** for that role. Smaller changes are reported as no difference under this pre-registered
rule. Required comparisons: rewrite vs none, upstream vs none, rewrite vs upstream, and brief vs
the better of the two persona arms. n = 2 per arm is directional only, not statistically
significant.

## Pilot and adjustment log

No pilots have run at pre-registration time.

| Role | Pilot | Score | Adjustment decided before the next pilot |
|---|---|---:|---|
| security | `security-p1` | invalid | Nested `sandbox-exec` failed before any read or edit; retained as approved call 2, not retried |
| security | `security-p2` | invalid | Inheriting `CODEX_SANDBOX=seatbelt` did not prevent the nested bootstrap; retained as call 3 |
| security | `security-p3` | 12/12 (100%) | Ceiling. Add an independently routed integration surface with 10 deeper trust-boundary checks; prompt/model unchanged |
| security | `security-p4` | 21/22 (95%); initial gate failures were test defects | Legitimate create must omit client ownership and secure rotate responses may redact secrets; corrected those held-out assumptions and rescored the same work without another model call |
| security | `security-p5` | 21/22 (95%) | Stop: the two runs on the harder frozen design average 95%, far above the 60% ceiling criterion |

The experiment stopped at calibration as pre-registered. Performance calibration and all 16
scored runs were not started. Therefore round 5 estimates no persona, rewrite, upstream, or brief
effect; it establishes only that this Codex setup still solved the security maker task at ceiling.

## Stop conditions and deviations

- No persona arm is piloted.
- No automatic model retry is allowed; a failed call is recorded as failed.
- At most five none-vague pilots per role and 27 total `codex exec` calls (control, pilots,
  scored runs) are allowed.
- A correctness-gate failure scores zero; it is not repaired by the experimenter.
