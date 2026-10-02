# Decision record

Use this skill when a significant technical choice is being made or questioned: a framework, data store, protocol, module boundary, or API shape, or when someone asks why the system is built the way it is.

1. Find where this project already records decisions (an ADR folder, a decisions document, design docs) and follow its format and numbering. If it has none, propose a location instead of creating one.
2. State the context: the problem, the constraints, and the forces at play, citing the code and documents you read.
3. State the decision in one to three sentences.
4. List the alternatives actually considered, including doing less or nothing, each with its benefits, drawbacks, and the specific reason it was not chosen.
5. State the consequences: what becomes easier, what becomes harder, and the risks to watch.
6. Mark the status (proposed, accepted, superseded) and link any decision it replaces.

When asked why something was chosen, read the existing records first and answer from them, saying when no record exists. Present a new record as a draft for review; write it to the repository only when the task or the person authorizes it.

Adapted from affaan-m/ECC at commit c05b2d6614f62f6db0047669aa4eefb223d478f9 (MIT; copyright 2026 Affaan Mustafa). See the pinned source and bundled license in the skill catalog.
