---
name: evidence-review
description: Use when reviewing a substantial change against its request, or assessing code-review feedback before applying it in the current project. Skip trivial copy edits and routine status questions.
---

# Evidence-led review

Review the actual revision and surrounding behavior. Existing project checks, acceptance rules and role skills remain authoritative. Keep review proportional to the change; this skill does not require another agent, an extra approval, or a full suite for every edit.

For a proposed change, make two separate judgments:

- **Requested behavior:** map each material requirement to the implementation and its evidence. Include negative and failure paths relevant to this change. Unavailable requirements remain unverified.
- **Implementation quality:** examine correctness, data ownership, concurrency, permissions, compatibility and maintainability where the diff touches them. A passing test does not establish all of these, and a preference is not a defect.

Report confirmed findings by severity with location, impact and supporting evidence. Distinguish pre-existing issues, optional improvements and unresolved questions. For a large change, inspect the integrated flow as well as individual tasks. Use an independent reviewer only when the task and project rules authorize it and the risk justifies it.

When receiving feedback, first restate the concrete proposed change and verify its premise against the current code, callers, supported platforms and documented decisions. Implement technically supported corrections; explain unsupported suggestions with evidence. Clarify only the part whose ambiguity blocks progress and continue independent authorized work.

Match acceptance evidence to its subject: a child success message needs its artifact and landing checked; deployment needs the intended version and served asset checked; a bug needs its original symptom retested. Report partial verification as partial. Do not modify or delete someone else's work while checking a claim.

Keep invocation evidence and findings in the normal Session history. If the current project has an approved workflow pilot, follow its own checkpoint and evidence limits. Do not launch extra model calls as part of ordinary review.

Adapted from obra/superpowers `receiving-code-review` and the spec/quality review distinction in `subagent-driven-development` at commit 2ce8aa15d05bd57213e6f746f953aafb7ded7a8b. Copyright (c) 2025 Jesse Vincent, MIT. No orchestration, model-selection or branch-finishing instructions were imported.
