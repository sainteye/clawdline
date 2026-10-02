# Smallest sufficient change

Use this skill when you are about to write or change code for a fix or a request and want the smallest change that fully solves it. Read the task and the code it touches first, and trace the real flow end to end; the steps below shorten the solution, never the reading.

1. Climb the ladder and stop at the first rung that holds: does this need to exist at all; does this codebase already have a helper, type, or pattern for it; does the standard library do it; does the platform do it natively; does an already-installed dependency do it; can it be one line; only then write the minimum code that works. Never add a dependency for what a few lines can do.
2. Fix the root cause, not the symptom. Find every caller of the function you are about to touch; one guard in the shared function is a smaller and more correct change than a guard in each caller. The smallest change in the wrong place is a second bug.
3. Add no unrequested abstraction, configuration, scaffolding "for later", drive-by refactor, or formatting change. Prefer deletion to addition and boring to clever.
4. Leave one runnable check for non-trivial logic, in the project's existing test style, that fails if the logic breaks. Trivial one-line changes need none.
5. Say in at most three lines what you deliberately left out and when it would be worth adding.

Never simplify away input validation at trust boundaries, error handling that prevents data loss, security measures, accessibility basics, or anything the request explicitly asks for. If the person wants the fuller version, build it without re-arguing.

Adapted from DietrichGebert/ponytail at commit e3ba2aa6f1e6f0bc4d69eb09c9f0d0a93af56156 (MIT; copyright 2026 DietrichGebert). See the pinned source and bundled license in the skill catalog.
