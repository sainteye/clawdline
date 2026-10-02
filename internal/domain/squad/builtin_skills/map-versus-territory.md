# Map versus territory

Use this skill when a claim about the system rests on a document, test, comment, dashboard, summary, or earlier assumption, and especially when observed behavior disagrees with it. Documents and reports are the map; running code and actual data are the territory, and the territory wins.

1. Name the map: quote the exact claim and where it came from.
2. Name the territory check: the code path, runtime value, query, reproduction, or recent change that would prove or disprove it.
3. Check the map's source and freshness: who or what produced it, when, and whether a later change could have made it stale.
4. Observe the territory directly. Do not infer behavior from names or signatures alone.
5. Record the result as: map (claim, source, freshness); territory check; observation (the concrete result, not an interpretation); delta (aligned, map wrong, or territory incomplete); and the action it leads to.
6. Stop once the claim is confirmed or overturned by an observation; do not re-check the same path.

Do not apply this to an artifact that is itself the thing being edited, to a generated source of truth with no claimed drift, or to a mismatch that cannot change the decision.

Adapted from tjboudreaux/cc-thinking-skills at commit 7b8fece345dfaa11773be7152ccd194589cb5437 (MIT; copyright 2025 TJ Boudreaux). See the pinned source and bundled license in the skill catalog.
