# Machine workflow skill pilot

Current machine selection, frozen launch records and Poteto adaptation are described in [machine workflow profiles](workflow-profiles.md). The pilot below records the original two-switch implementation and its separate evaluation checkpoint.

Status: local pilot, 2026-10-30. Behavioral effectiveness is **not evaluated**.
[繁體中文](workflow-skill-pilot.zh-TW.md)

## Scope and activation

Settings → Sessions and access → Integrations has two independent machine-wide switches,
both **off by default**: Workflow skills and Graphify. This is a machine setting,
not a browser or Project preference. It applies across Projects when Clawdline
opens a new Claude or Codex Session, including broker-created children and new
Roots. A resumed or already-running Session retains its original context; turning
a switch off cannot retract text already loaded by a provider. Open a new Session
to verify the changed setting. Sessions opened outside Clawdline and other
providers are outside this integration.

Workflow skills adds the bundled prefix and both selective skill bodies to the
existing provider instruction: Claude's single inline `--append-system-prompt`
or Codex's single `developer_instructions`. Role instructions, Project memory
and language still share their established launch path. The bodies are:

- `skills/evidence-debugging/SKILL.md`: non-trivial bugs, stalls and repeated failures.
- `skills/evidence-review/SKILL.md`: substantial change reviews and review feedback.

Loading both bodies is automatic when enabled. Applying the relevant workflow
remains a model behavior, not a guaranteed hook. The inline text uses context
tokens; actual cost and benefit have not been measured. Routine copy edits do
not require extra workflow. Turning it off omits these bodies on future launches.
There are no Project discovery symlinks and no global assistant files to remove.
The setting does not uninstall a person's existing Superpowers or role skills.

Graphify adds optional lookup instructions for an existing tool/index and a
read-only pre-query freshness check. It does not install Graphify, build/delete
an index or intercept direct Graphify calls. See
[Graphify integration](graphify-integration.md) for its precise limits and the
preserved local pilot reports. The two switches do not depend on each other.

The narrow `GET/POST /v1/settings/integrations` route stores the boolean keys
`workflow_skills_enabled` and `graphify_enabled` in this machine's `config.json`.
Writes reject unknown keys and non-booleans before any change. Paired devices
need send permission. Cloud carries only these two keys, not arbitrary settings
or Project paths. New launch instructions read current settings; no daemon
restart is needed for a saved toggle.

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

Enabling these switches by default or expanding the adaptations requires a
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

The control includes existing repository instructions. The candidate additionally
includes the same bundled prefix and full skill bodies as the machine launch
integration. Neither receives the rubric or intended answer. All trials must use
independent fresh contexts with identical model/version, effort, tools and
permissions, no inherited conversation, and no ambient integration instructions.
Give each executor only its prompt and supplied artifacts. Do not run the control
inside an environment that already injects the skills. This compares behavior
under the supplied instruction texts; real launch acceptance is a separate
check. Record any protocol departure with the result.

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
also check that only candidate prompts include the bundled instructions and that
Project discovery paths do not bypass the switch.
These prove tooling/configuration contracts only; they are not LLM trials. No
baseline/candidate response has been collected by this change. Effectiveness,
workflow compliance, token cost and time savings remain unknown.

Historical probe of the superseded Project-link design: on 2026-10-30, Codex CLI 0.162.1 `skills/list` with `forceReload: true`, an
isolated temporary `CODEX_HOME`, and this worktree as its cwd returned both
skills enabled with no project skill errors. Only `initialize`, `initialized`
and `skills/list` were sent; no thread or model turn was started. This verified
native Codex discovery for that earlier environment, not selection or compliance.
Those links are now removed; this is not acceptance evidence for the machine
switch. The current acceptance tests inspect provider arguments, settings
persistence and broker/GUI launch paths without starting model turns. The probe
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
