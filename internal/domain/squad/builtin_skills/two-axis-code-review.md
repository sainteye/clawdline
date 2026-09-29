# Two-axis code review

Use this skill to review a proposed change in any codebase. First identify the change boundary, the request it claims to satisfy, and the rules of the current project. Use its own work tracker, specification format, language, tools, and test commands; if a source is unavailable, name that limit instead of inventing one.

1. Read the diff and the surrounding code needed to understand each changed behavior. Establish the comparison point from the task, pull request, or available history. If the boundary is uncertain, state which files and revisions were actually reviewed.
2. On the **project-rules axis**, check the applicable repository instructions, coding conventions, security and privacy requirements, and required verification. Treat general design smells as hypotheses, not violations; the project's documented decisions take precedence.
3. On the **request axis**, map each observable requirement to implementation and evidence. Look for missing behavior, changed contracts, failure paths, repeated requests, data migration, permissions, and tests that would fail without the change. If no request or specification is available, mark this axis unverified rather than approving it.
4. Combine the two axes into one review ordered by the role's severity rules. For each confirmed finding, give the location, impact, evidence, and a concrete correction. Separate uncertain questions and unrelated pre-existing issues. State what was checked and found correct.

Review only. Do not install tooling, create or change tracker entries, publish comments, run third-party scripts, or delegate work unless the current task and project rules authorize those actions.

Adapted from Matt Pocock's `code-review` skill at commit c55ee46073ed923f86ce59a5eb3b6d895095d1b7 (MIT; copyright 2026 Matt Pocock). See the pinned source and bundled license in the skill catalog.
