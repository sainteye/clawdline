---
name: evidence-debugging
description: Use when investigating a non-trivial bug, repeated failure, dropped event, stalled operation, or unexpected behavior in this repository. Skip routine copy, styling, and already-understood mechanical edits.
---

# Evidence-led debugging

Find the failing boundary before choosing a fix. Apply the current task and repository rules, including the existing targeted check gate and shared-tree ownership. This skill grants no permission to install, dispatch, deploy, spend, or restart anything.

1. Record the trigger, expected behavior, actual behavior, and exact revision or running version. Read the real error and nearby code. If reproduction is intermittent, preserve that uncertainty and gather evidence rather than naming a cause from one failure.
2. Follow the value or operation through its callers and component boundaries. For sends and stalls, distinguish acceptance, execution, delivery, observation, and acknowledgement; inspect queue bounds, retries, receipts, resume and failure isolation. Inspect only task-relevant state and redact secrets from diagnostics.
3. State a falsifiable hypothesis and the observation that would disprove it. Compare a working path or version when available; vary one cause at a time. Repeated unsuccessful patches are a reason to revisit the model of the system, not stack another timeout on it.
4. Fix at the earliest responsible boundary with the smallest sufficient change. For behavioral fixes, prove a focused reproduction fails without the fix and succeeds with it, in an isolated tree. Use the repository's existing local gate; broader checks remain governed by project policy.
5. Report the observed cause, correction, evidence and remaining uncertainty. Keep temporary mitigation distinct from a verified fix. Continue authorized work without adding approval checkpoints for routine implementation choices.

For the project pilot, keep an invocation announcement and task verification evidence in the normal Session history. On the first applicable task on or after 2026-11-06, read `docs/workflow-skill-pilot.md` and perform its evidence-only checkpoint. A checkpoint reviews persisted evidence; it does not start paid model calls.

Adapted from obra/superpowers `systematic-debugging` at commit 2ce8aa15d05bd57213e6f746f953aafb7ded7a8b. Copyright (c) 2025 Jesse Vincent, MIT; see `docs/licenses/obra-superpowers-MIT.txt`. Clawdline-specific workflow and permission boundaries are local adaptations.
