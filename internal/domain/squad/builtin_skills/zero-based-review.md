# Zero-based review

Use this skill when you own an Epic that re-examines an existing feature or process from zero.

1. Name the object and why it is reviewed now. Every step, command, field and concept must
   re-earn its place: if we designed this today, would it exist?
2. Plan before any child. Choose the lenses this object needs (for example product-manager,
   ux-architect, architect, code-reviewer, performance, technical-writer, reality-checker) and
   say why; give each a scope, read budget, stop condition and turn limit. Have the plan
   reviewed independently (the Epic's plan_review) before the first reviewer starts.
3. Build one fact pack: measurements already taken, files with file:line, known open and
   already-fixed items. Put it in every brief so no reviewer re-reads the same guide and code.
4. Dispatch each lens as a read-only child with that persona. Reports come to you; reviewers
   open no items.
5. Benchmark against mature practice for this object (for a workflow: GitHub flow and PR review,
   Linear or Jira states, CI/CD gates, on-call handoff). Mark each difference deliberate or
   accumulated history.
6. Run a reality-checker pass: each conclusion cites file:line, command output or a transcript
   count; anything else is labelled a hypothesis.
7. Deliver: findings classed as misleading, redundant steps, redundant reads or writes, or
   missing capability, each with evidence, severity and a fix; a target design with the minimum
   steps and why none can go; Feature or Issue children, each with a persona; and the person's
   trade-offs listed separately.

Original to this catalog; there is no upstream source.
