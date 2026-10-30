# Poteto workflow for Clawdline

This is Clawdline's bundled adaptation of pstack 0.15.36, pinned to cursor/plugins c742dd61706fbacc1b95f1a20b7b988cf096ce6c. It is not the complete Cursor plugin. Its routing and verification practices run with Clawdline's existing tools. No remote skill, dependency installation, model routing, hook, PR stack, or autonomous publishing is required.

The current request, repository rules, task boundaries, permissions and existing checks remain authoritative. This workflow grants no permission to spend, contact others, delegate, restart, deploy, delete data, force-push or bypass a guard. Ask for authorization only when the governing rules require it. Never infer authorization from a workflow name.

## Route the task

Start with the person's desired outcome and the smallest useful change. Determine whether this is an investigation, bug fix, feature, refactoring or evaluation. Investigations stop at a supported answer; they do not manufacture implementation work. A bug fix starts by reproducing the observed failure on the affected surface. A feature starts with the data shape and externally observable acceptance criteria. A refactoring preserves behavior. An evaluation defines the common task, controls and measurements before comparing results.

## Work in verifiable units

Identify the data owned by each boundary before changing code. Prefer a small, direct structure with explicit state over extra layers. For a real design uncertainty, inspect the repository or build a bounded local prototype. Product preference and authorization decisions still belong to the person.

Divide substantial work into independently checkable units. Each unit names its inputs, output and acceptance evidence. Keep a short checkpoint of what is proved, what remains uncertain and the next verification. Combine stages for small changes; do not impose an elaborate ceremony on a one-line edit.

Delegate only independent, bounded work when the current authorization and repository rules allow it. Give each worker the goal, exact write scope, starting revision, required tools and acceptance criteria. Use the existing Clawdline child/worktree/claims protocol where required; inherit the task's frozen workflow. Review the returned changes yourself. Do not invent Cursor agents, unavailable control skills or different-model calls.

## Verify the delivered behavior

For a defect, show that the reproducer fails before the fix and passes afterward. For a feature, make a verification map from each acceptance criterion to a test, real UI observation, artifact inspection or explicitly pending human judgment. Verify the actual surface: UI behavior in a browser, CLI behavior through its entry point, and deployment only at its authorized destination. Compilation alone does not establish behavior.

Tests should exercise observable behavior rather than source spelling. Use the smallest relevant checks while editing and the repository's required commit gate before committing. Do not broaden testing repeatedly without a new failure or unresolved concern. Respect the repository's landing and release policy; pstack's stack shipping commands are not part of this adaptation.

## Deliver evidence

Report the change, exact revision, checks actually executed, failures or unknowns, and remaining work. Distinguish an instruction added to a launch, an agent reporting it followed the workflow, and independently verified results. No report proves causal benefit merely because it mentions this workflow. Preserve rerunnable evidence for measured claims. State the request count and cost for separately authorized paid API work; this workflow never authorizes it.

If a repeated defect reveals a missing structural guard, propose or implement the smallest appropriate guard within scope and verify that it rejects the defect. Keep user-facing prose plain and specific. Never claim an unavailable tool ran or that a successful check proves an untested destination.
