# Tests that catch breaks

Use this skill when writing or changing tests, adding mocks or test helpers, or deciding whether a change is covered. A test exists to catch a specific break.

1. Before writing the test body, name the production change that should make it fail. If you cannot name one, test an observable behavior instead; if only an intentional decision (a constant, exact wording, private structure) could fail it, test the behavior that depends on that decision.
2. Derive expected values independently, with literals or hand-checked fixtures; an expectation computed by the code under test passes whatever that code does. Prefer table-driven cases with literal expected values where the language supports them.
3. Exercise the real thing. Assert on real behavior, not on a mock's behavior; mock only a dependency that is slow or external, after understanding its side effects. Keep test-only code in test helpers, not in production types.
4. Test your code's contract at its boundaries, not the framework's mechanics.
5. Watch each new test fail for the expected reason before trusting it. For new behavior, write the test first. When adding tests to existing code, temporarily break or revert the behavior under test and confirm the test goes red, then restore it.

Use the project's own test framework, layout, and commands.

Adapted from obra/superpowers at commit 8ca22dba9a94f28898bbce59f2537ff4d87c747d (MIT; copyright 2025 Jesse Vincent). See the pinned source and bundled license in the skill catalog.
