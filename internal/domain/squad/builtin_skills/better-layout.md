# Layout

Use this skill when setting or reviewing web layout structure: grouping, alignment, reading order, responsive behaviour and room for translated or mirrored text; not for colour, motion or the wording itself.

Write fixes in the project's styling system and use its spacing tokens; the numbers here apply only where it has none. A finding needs a failure you can show: a clip, an overlap, a broken mirror, a misread group. A value that merely differs from these starting points is not one.

1. **Group with space before lines.** Negative space first, background shapes where a group must read as one unit, separators only in dense data. The gap between groups is at least twice the gap within one.
2. **Controls look like controls**: a shape, border, underline or a consistent zone such as a toolbar. A non-clickable badge shaped like the buttons beside it is also a defect.
3. **Shared edges.** A few alignment edges; one spacing step per nesting level. Numbers in tables align to the end, text to the start.
4. **Logical properties for anything that mirrors**: `margin-inline-start`, `padding-inline-end`, `inset-inline-start`, `text-align: start`. Progressions (ratings, steps, progress bars) mirror in right-to-left; digit order never does. `translateX`, `background-position` and safe-area insets are physical and need an explicit right-to-left override.
5. **Order by importance.** The most important content near the top and leading edge; in a row, identifying content leads and metadata and actions trail. Visual order matches DOM order: no `order`, `row-reverse` or grid placement that reorders content.
6. **One primary action per view**; more than three secondary actions go behind a menu.
7. **Hint at hidden content.** Off-screen or collapsed content has a visible cue: a 16–32px peek past the scroll edge, or a disclosure naming what it hides. Truncated values a person needs have a keyboard-reachable way to the full text.
8. **Clearance.** About 12px between bordered or filled controls, 24px between the glyphs of borderless controls, without overlapping hit areas. Keep an established usable density.
9. **Content bleeds, controls float.** Backgrounds and media may reach the edges; text and controls stay inside margins and safe areas (`env(safe-area-inset-*)` needs `viewport-fit=cover`). Full-width buttons sit inside about 16px margins on mobile. Set `scroll-padding-block-start` to a sticky header's height.
10. **Breakpoints come from content**, not device presets. Prefer container queries for components; a container with `container-type: inline-size` needs a definite width.
11. **Plan for growth.** Translated strings grow, short labels most. Use `max-width` and `min-height`, never fixed text box sizes; let rows wrap; use `minmax(0, 1fr)` and `min-width: 0` so long words cannot push past the viewport; `100%` instead of `100vw`, `100dvh` instead of `100vh` for full-height panes. Never park a critical action where resizing, zoom or the keyboard clips it.

**Report.** `HIGH`: content or an action blocked at a supported viewport, clipping or overlap at 320px width or 200% zoom, truncation with no way to the full value, or hidden content with no cue. `MEDIUM`: hierarchy, reading order or adaptability harmed. `LOW`: isolated alignment. One row per root cause with location, before, after and why. Without a browser, check logical properties, queries and DOM order in source; with one, check each supported width, 320px, 200% zoom and the right-to-left mirror. Report every check not run as not verified, then `Block` if any `HIGH` remains, else `Approve`.

Adapted from jakubkrehel/skills' `better-layout` skill and its two reference notes at commit 592aedc44d7d5b3e29740984252902f3e5402a1c (MIT; copyright 2026 Jakub Krehel). See the pinned source and bundled license in the skill catalog.
