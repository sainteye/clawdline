# Project workflow skill pilot

Status: local pilot, 2026-10-30. Behavioral effectiveness is **not evaluated**.
[繁體中文](workflow-skill-pilot.zh-TW.md)

## Scope and activation

Two repository-local skills extend the existing work rules:

- `skills/evidence-debugging/SKILL.md`: non-trivial bugs, stalls and repeated failures.
- `skills/evidence-review/SKILL.md`: substantial change reviews and review feedback.

Both `.agents/skills/` and `.claude/skills/` expose relative links to these same
texts. Native skill discovery makes them available to new Claude/Codex sessions
in this checkout without selecting a Squad role or invoking a Playbook.
Availability is not proof of selection or use: confirm the provider's visible
skill inventory and the Session's invocation/read evidence. Already-open sessions
can retain their previous inventory; use a new Session for discovery acceptance.
Other providers and other repositories are outside this pilot.

The daemon, Cloud, global assistant configuration and built-in role snapshots
are unchanged. Existing roles already carry Superpowers-derived verification and
test skills, and the code reviewer already has a two-axis review skill; this pilot
adds a general project entry point for ordinary sessions.

These are selective adaptations of [obra/superpowers at
2ce8aa15d05bd57213e6f746f953aafb7ded7a8b](https://github.com/obra/superpowers/tree/2ce8aa15d05bd57213e6f746f953aafb7ded7a8b),
with its [MIT notice](licenses/obra-superpowers-MIT.txt). No upstream plugin,
hook, telemetry, orchestration script or model call is installed. The existing
targeted local gate, background full CI, shared-tree ownership and authorization
rules remain in force. Routine copy edits do not gain a design gate, TDD rule,
extra reviewer or full-suite requirement.

## First checkpoint

Review on the **first applicable task on or after 2026-11-06 (Asia/Taipei)**, or
earlier if five applicable task receipts are already available. This is a task
trigger, not a scheduled background job or an unattended evaluation at midnight.
The user continues making normal requests.

Before performing the checkpoint, read the Project memory entry
`workflow-skill-pilot`. If it already records a completed checkpoint for these
skill versions, do not repeat it. After review, update that same entry with the
review date, input revisions, evidence limits and outcome (including
insufficient evidence). Preserve its earlier record. Store only the summary and
evidence references there, not private transcript contents.

At that checkpoint, inspect only already-authorized, identified task records;
do not search unrelated private histories. Record invocation evidence, a concrete
decision affected by the skill, verification outcome, unnecessary questions,
extra tool/model work if measured, and regressions. Listing a skill or claiming to
have used it is weaker evidence than its actual read and work. Five different
tasks are observational evidence, not a causal baseline/candidate comparison.
If no applicable tasks have occurred, report **insufficient evidence**, rather
than extending the pilot or launching model calls automatically.

Expansion to built-in roles, global defaults or other projects requires a
controlled comparison and review of both benefit and overhead. Preserve negative
results. The checkpoint can recommend removal if the skills add no value.

## Offline controlled-evaluation preparation

The standard-library-only tool prepares six synthetic scenarios: reconnecting
send, wrong review premise, stale production assets, unlanded child work, small
copy edit, and a green test missing a restart requirement. It does not run an
assistant or call a network service.

```sh
python3 tools/workflow-eval.py prepare --out /tmp/workflow-pilot-trial \
  --model 'exact model version; effort; harness; permissions' --repeats 5
python3 tools/workflow-eval.py report /tmp/workflow-pilot-trial
```

Use a fresh, non-existent directory outside **every** Git checkout. The default
five repetitions create 60 prompts: six cases × two arms × five repetitions.
Replace the model label with the actual fixed configuration before execution.
Smaller preparations are tooling smoke checks and cannot set
`repeated_sample_ready`.

The control includes existing repository instructions. The candidate adds only
the new skill catalog and frozen local skill paths; bodies are read only if
selected. Neither receives the rubric or intended answer. All trials must use
independent fresh contexts with identical model/version, effort, tools and
permissions, no inherited conversation, and no ambient skill catalog. Give each
executor only its prompt and access to that experiment's `sources/` files. Do not
run the control inside a checkout that already discovers the pilot skills. This
tests discovery/application under the supplied catalog; native CLI discovery is
a separate acceptance observation. Record any protocol departure with the result.

Running prompts may incur model costs. Preparation is not authorization: before
execution, follow the current paid-API rule with provider/SKU, estimated cost,
explicit call/cost cap, no automatic retries, and approval where required. There
is no automatic runner or paid fallback. A no-cost checkpoint can use persisted
task evidence with its observational limits clearly stated.

## Response and grading contract

1. Save the verbatim response as `responses/<trial-id>.txt`, outside the repo.
   Preserve actual provider usage/cost receipts separately if calls were made.
2. Give an independent grader only frozen `rubric.json`, case IDs in
   `grading.json`, and responses; hide `manifest.json` and prompts to keep
   assignments blinded as far as the responses allow. Grade behavior, not
   keywords. The same evaluator must not generate and grade its own responses.
   Disclose any arm revealed by a response itself.
3. In `grading.json`, fill `reviewer`, the exact `model` label and
   `response_sha256` (SHA-256 of verbatim file bytes). For every criterion fill
   boolean `passed`, a literal response excerpt in `evidence`, and a `reason`.
   A quotation supports traceability; a person must still judge meaning and
   whether a required behavior is absent.
4. Run `report`. Exit 0 means structurally valid preparation or a fully reviewed
   report, **not** proven effectiveness. Exit 2 means unrun/incomplete evidence
   and emits no arm comparison. Exit 1 means invalid, changed or unreadable
   evidence. Do not remove a trial or criterion to repair a failure.

The manifest and all sources/prompts/rubric are frozen by hashes. These detect
accidental drift, not hostile rewriting of both data and hashes. A changed
response invalidates its previous grade. All planned samples must be present,
paired, and reviewed under the same configuration before a comparison is printed.
Per-case results remain visible so total wins cannot hide regressions. The tool
never decides adoption; even five repetitions are descriptive evidence for these
cases, not statistical proof of general productivity or quality improvement.

## Evidence boundary

Local tests exercise missing/duplicate samples, altered evidence, false quotes,
model mismatch, source drift, per-case regressions and path containment. They
also check that both discovery directories resolve to the canonical skills.
These prove tooling/configuration contracts only; they are not LLM trials. No
baseline/candidate response has been collected by this change. Effectiveness,
native selection rate, token cost and time savings remain unknown.

On 2026-10-30, Codex CLI 0.162.1 `skills/list` with `forceReload: true`, an
isolated temporary `CODEX_HOME`, and this worktree as its cwd returned both
skills enabled with no project skill errors. Only `initialize`, `initialized`
and `skills/list` were sent; no thread or model turn was started. This verifies
native Codex discovery for that environment, not selection or compliance.
Claude discovery remains checked at the relative-link level only. The probe
uses the documented [skills/list protocol](https://learn.chatgpt.com/docs/app-server)
and [repository discovery locations](https://learn.chatgpt.com/docs/build-skills).

A mutation that marked the unrun experiment reviewed was rejected by the
empty-evidence test; the original tool was restored byte-for-byte. All 19 local
tests then passed. A CLI smoke preparation produced 60 prompts and zero model
calls; its report correctly returned exit 2 with zero reviewed samples and no
comparison. These are infrastructure results, not skill-effectiveness results.

See [existing role-skill decisions](persona-skill-decisions.md) for already bundled
adaptations and [checks](checks.md) for the commit gate. The gate runs
`PYTHONDONTWRITEBYTECODE=1 python3 tools/test-workflow-eval.py` without model calls.
