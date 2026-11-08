# Improve codebase architecture

Use this skill when asked to find where a codebase's structure makes change or testing hard and to propose restructurings; not for recording a decision already made or for implementing a chosen refactor.

Vocabulary, used exactly so proposals can be compared. A **module** is anything with an interface and an implementation, at any scale. Its **interface** is everything a caller must know: types, invariants, ordering, error modes, configuration and performance. A module is **deep** when much behavior sits behind a small interface and **shallow** when the interface is nearly as complex as what it hides. A **seam** is where an interface lives and behavior can be swapped without editing callers; an **adapter** fills a seam. Depth gives callers **leverage** and maintainers **locality**: change, bugs and tests concentrate in one place.

1. Scope before scanning. Take the area the person named. Otherwise read the recent history (`git log`) for files and areas that keep changing and start there; restructuring pays off where change happens.
2. Read the project's own vocabulary and decision records first (a glossary, `docs/`, ADRs). Do not re-propose what a recorded decision rejected unless the friction is real enough to reopen it, and then say which record it contradicts and why.
3. Explore and note friction: one concept spread across many small modules; shallow wrappers; helpers extracted for testing while the bugs live in how they are called; modules leaking across a seam; code hard to test through its current interface. Apply the deletion test to each suspect: if deleting it would make complexity reappear across callers it earns its place; if complexity simply vanishes it was a pass-through.
4. Introduce a seam only where something actually varies across it. One adapter is a hypothetical seam; two (for example production and a test fake) make it real.
5. Report each candidate with: files involved; the problem in one sentence; the change in one sentence; the gain in terms of locality, leverage and which tests would cross the new interface; and a strength of `Strong`, `Worth exploring` or `Speculative`. End with the one you would do first and why.

Do not design the new interfaces yet and do not edit code; ask which candidate to explore. Every claim about the code names the file you read. Write the report where the task says, otherwise in the reply; do not open browsers, load remote scripts or create files outside the task's write set.

Adapted from mattpocock/skills' `improve-codebase-architecture` skill at commit a496f5513d4a4cf889bc48e189e579f4b0bdf6aa, with the vocabulary of its companion `codebase-design` skill restated (MIT; copyright 2026 Matt Pocock). See the pinned source and bundled license in the skill catalog.
