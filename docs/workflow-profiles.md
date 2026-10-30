# Machine workflow profiles

Settings → Session and access → Integrations selects one managed main workflow for new Claude/Codex Sessions on this machine. Graphify remains an independent lookup option. This is a launch integration, not an interceptor of every model action or an installation manager.

## Compatibility and selection

`clawdline` preserves the existing task method. It contributes no workflow prompt, compulsory stages, extra confirmation, model routing or delegation. Existing permissions, repository instructions, work gates, landing and release checks remain authoritative for every profile. A waiting shell wrapper carries a random launch ID for observation without adding text to the standard provider prompt.

`superpowers` uses the existing selected debugging/review adaptation, not the entire upstream project. `poteto` uses the bundled Clawdline adaptation of pstack 0.15.36 at cursor/plugins revision c742dd61706fbacc1b95f1a20b7b988cf096ce6c. It routes investigation, bug-fix, feature, refactoring and evaluation tasks; models data before implementation; uses verifiable units and a criterion-to-evidence map; and verifies the affected surface. It does not depend on missing Cursor skills, automatically install tools, use PR-stack rewriting, call another model or authorize external actions. Source: https://github.com/cursor/plugins/blob/c742dd61706fbacc1b95f1a20b7b988cf096ce6c/pstack/skills/poteto-mode/SKILL.md. The upstream MIT notice is retained in `skills/LICENSE.pstack`.

When `workflow_profile` is absent, the old `workflow_skills_enabled=false` maps to `clawdline` and true maps to `superpowers`. Reads do not rewrite settings. Explicit profile selection wins. A legacy client write updates the explicit profile in the same atomic settings change; sending both selectors is refused. Graphify and unrelated settings are preserved.

## Frozen context and evidence

A fresh launch seals the schema, profile ID, version, source, instruction bytes, stage metadata and Graphify selection into a SHA-256 snapshot before opening the terminal. The observer binds it only to the matching provider descendant and terminal, then to the actual conversation. Before that background binding finishes, Session info can read the snapshot from the live process marker and matching terminal receipt without writing a binding. Dispatch uses its already-resolved live root to finish the binding before preparing a child; it does not add another inventory scan. A launch receipt means instructions were supplied, not that the model read or followed them. Session info never infers a historical choice from current machine settings.

Resume uses the original conversation snapshot without reinjecting new instructions. A child inherits the parent's snapshot even if settings changed. A legacy parent or conversation without a recorded snapshot remains explicitly unsnapshotted and does not acquire today's workflow. Externally launched Sessions and unsupported providers remain unsnapshotted. Changing the setting affects future roots; it does not withdraw existing model context. Tool-use and quality claims still require their own evidence.

The five descriptive boundaries are `brief`, `plan`, `execute`, `verify` and `deliver`. Each carries its output and acceptance description. Small tasks may combine stages, and investigations need not modify code. This first version does not claim to enforce natural-language criteria or receive stage-completion events. The standard profile has no added stages. Independent existing gates remain enforced by their existing owners.

## Extension boundary

Built-in profiles use the same versioned `workflow.Snapshot` contract intended for future custom definitions. The digest includes stages, instructions and tool selection; edited definitions cannot silently change an existing conversation. This version exposes only the three reviewed built-ins. It does not expose custom-file imports, an arbitrary script runner, a custom editor or additional authority. A future editor can copy a built-in, validate a new version, trial it in isolation, and select it through the same resolver.

For a controlled comparison, keep the task, starting tree, model, tools, resource budget and independent acceptance criteria fixed; isolate contexts and working trees. Record quality, rework, elapsed time, token/cost evidence and human intervention. A small trial establishes feasibility, not universal benefit or causal superiority. The original workflow-skills pilot checkpoint remains separate.

See [the earlier skill pilot](workflow-skill-pilot.md) and [Graphify's lookup boundaries](graphify-integration.md).
