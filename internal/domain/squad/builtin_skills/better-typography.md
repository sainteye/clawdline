# Typography

Use this skill when setting or reviewing how text renders in a web interface: type scale, line-height, font loading, wrapping, truncation, numbers and punctuation; not for the wording itself or for choosing colours.

Write every fix in the project's styling system. Exact rules are findings when missed; heuristics are reported only where they break the project's own scale. Never propose a new typeface unless the task asks for a type change.

**Exact rules**
- Line-height is unitless: display about 1.1, headings 1.2–1.3, body 1.5–1.6. Anything that wraps to three or more lines needs at least 1.4.
- Below 18px use weight 400 or heavier; weights 100–300 only from about 28px.
- Cap long-form text at 60–75 characters per line.
- Changing numbers (timers, counters, prices, numeric columns) use `font-variant-numeric: tabular-nums`; numbers in prose stay proportional.
- Mobile inputs render at 16px or more, or iOS zooms the page. Never fix that by disabling zoom.
- `font-size` in `rem`, `letter-spacing` in `em`. Long-form body starts at 16px; UI text rarely goes below 12px.
- Load every face the design uses instead of letting the browser synthesise bold or italic. Prefer CSS properties (`font-weight`, `font-variant-numeric`) to raw `font-variation-settings` or `font-feature-settings` tags, which are only for custom axes and features without a property.

**Heuristics**
- Few families, sizes and weights; a small named scale where each size is paired with its line-height and weight. Heading sizes descend with level.
- Headings from 24px take slight negative tracking; small uppercase labels take about 0.05em; body stays at 0.

**Wrapping and truncation**
- `text-wrap: balance` for headings and short descriptions, `text-wrap: pretty` for paragraphs, `overflow-wrap: break-word` where a long ID or URL could escape, `nowrap` on badges.
- Interface text aligns to the start, never justified.
- Truncated text needs a way to the full value (expand, detail view, or a tooltip reachable by keyboard).

**Characters and language**
- Store text in natural case and set case with `text-transform`.
- Rendered text uses the typographic punctuation of its own language: the ellipsis character, proper dashes, non-breaking spaces inside values such as `10 MB`. Follow each language's conventions (for example corner brackets in Traditional Chinese); code keeps straight quotes.
- Set `lang` and `dir`; wrap mixed-direction values in `<bdi>`.
- Keep content text selectable; `user-select: none` only on drag or gesture surfaces.
- Font smoothing is set once on the root, never per component.

**Report.** `HIGH`: unreadable text, truncation with no way back, or clipping at 320px width or 200% zoom. `MEDIUM`: broken type system or heading hierarchy. `LOW`: isolated polish. One row per root cause with location, before, after and why. Mark every check you could not run, such as rendered wrapping without a browser, as not verified.

Adapted from jakubkrehel/skills' `better-typography` skill and its six reference notes at commit 592aedc44d7d5b3e29740984252902f3e5402a1c (MIT; copyright 2026 Jakub Krehel). See the pinned source and bundled license in the skill catalog.
