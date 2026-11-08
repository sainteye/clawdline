# Domain modeling

Use this skill when a design discussion turns on what the project's terms mean, or a glossary must be written or corrected; not for merely reading existing terms, and not for writing a decision record by itself.

1. Find the project's existing vocabulary first: a glossary file, a context map for several areas, terms in its docs and code. Use its location and format; do not start a second one beside it.
2. When someone uses a term that conflicts with the recorded meaning, say so at once: "the glossary defines cancellation as X, but this sounds like Y; which is it?"
3. When a term is vague or overloaded, propose one precise name and say what it is not ("account: the Customer or the User?").
4. Test each relationship with a concrete edge-case scenario that forces the boundary between two concepts to be stated.
5. Check stated behavior against the code. When they disagree, show the file and ask which is right; do not silently pick one.
6. Record a resolved term in the glossary as it is resolved: the term in bold, a definition of one or two sentences saying what it is rather than what it does, and the rejected synonyms under "Avoid". Include only terms specific to this project, never general programming concepts or implementation details.
7. Suggest a decision record only when the choice is hard to reverse, would surprise a later reader, and came from a real trade-off; if any of the three is missing, skip it. Follow the project's existing record format and numbering.

Write the glossary or record only when the task's writes include it; otherwise propose the exact text. A glossary is not a spec or a scratch pad.

Adapted from mattpocock/skills' `domain-modeling` skill and its glossary and ADR format notes at commit a496f5513d4a4cf889bc48e189e579f4b0bdf6aa (MIT; copyright 2026 Matt Pocock). See the pinned source and bundled license in the skill catalog.
