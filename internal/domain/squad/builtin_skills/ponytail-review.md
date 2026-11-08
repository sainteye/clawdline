# Over-engineering review

Use this skill when you review a change and want to know what it could delete or shrink: reinvented standard-library code, unneeded dependencies, speculative abstractions, configuration nobody sets, or flexibility no caller uses. It complements a correctness review and does not replace it.

1. Read the diff and enough surrounding code to know who calls each new piece. A layer, option or interface is speculative only when you have checked that it has one implementation or no caller that needs it.
2. Report one line per finding: the location, a tag, what to cut, and what replaces it. Tags: `delete` (dead code, unused flexibility, an unrequested feature; nothing replaces it), `stdlib` (hand-written code the standard library provides; name the function), `native` (a dependency or code doing what the platform already does; name the feature), `yagni` (an abstraction with one implementation, a setting nobody sets, a layer with one caller), `shrink` (the same logic in fewer lines; show the shorter form).
3. End with the number of lines the findings would remove. If there is nothing to cut, say so in one line and stop.

Correctness, security and performance findings are out of scope here; hand them to the main review. Never mark for deletion input validation at a trust boundary, error handling that prevents data loss, security measures, accessibility basics, a minimal test or self-check, or anything the request explicitly asks for. List the cuts only; do not apply them.

Adapted from DietrichGebert/ponytail's `ponytail-review` skill at commit e3ba2aa6f1e6f0bc4d69eb09c9f0d0a93af56156 (MIT; copyright 2026 DietrichGebert). See the pinned source and bundled license in the skill catalog.
