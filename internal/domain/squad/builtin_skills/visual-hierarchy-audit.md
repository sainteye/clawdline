# Visual hierarchy audit

Use this skill when a web interface looks flat, cluttered or amateur and you need to audit and fix its hierarchy, spacing, colour and depth; not for choosing a new visual direction or reviewing motion.

Hierarchy comes from systems, not talent: fixed scales for spacing, type, colour and shadow.

1. **Score it first.** Answer eight yes/no checks and report `round(passed / 8 × 10)` with each failed check named:
   - the hierarchy still reads when blurred (squint test);
   - it still reads in greyscale, so colour is not doing hierarchy's job;
   - there is enough white space, especially between groups;
   - labels are quieter than the values they describe;
   - spacing follows one scale;
   - text width is capped (roughly 45–75 characters);
   - text and controls meet contrast minimums (4.5:1 body text, 3:1 large text and UI boundaries);
   - shadows match each element's elevation.
2. **Hierarchy.** Use size, weight and colour as three levers, and combine rather than stack them: only the single most important element gets all three. De-emphasise labels, table headers and metadata. A routine destructive action can be a muted secondary button rather than a loud red one. Buttons follow primary (filled), secondary (outlined or muted), tertiary (text).
3. **Spacing.** Use the project's spacing scale, or one such as 4, 8, 16, 24, 32, 48, 64. Space between groups exceeds space within them. Start with too much space and remove it. Constrain widths; full width is rarely right for text or forms.
4. **Type.** A small modular scale, tight line-height for headings and relaxed for body, no body weight under 400, at most two families.
5. **Colour.** Design in greyscale, add colour last. Each colour has a range of shades; the darkest text is a near-black, not pure black; greys may carry a slight tint matching the product's temperature. Never hide pricing, terms or cancellation with de-emphasis.
6. **Depth.** A small shadow scale: small for raised controls, larger for floating menus, largest for modals. Two-part shadows (tight and soft) read crisper. If everything floats, nothing has depth.
7. **Images and icons.** Keep aspect ratios with cropping, never stretch. Put an overlay under text on images. Use one icon set at consistent sizes.
8. **Layout.** Left-align by default; centre only short headlines, single-action calls to action and empty states.

Report each finding with location, the failed check, the current value and the replacement in the project's own tokens and styling system. Values in this text are starting points only where the project has no scale of its own.

Adapted from wondelai/skills' `refactoring-ui` skill and its five reference notes at commit d8d5d75f83da9f16d8aad5bab5f5b341a4770a30 (MIT; copyright 2025 Wondel.ai sp. z o.o.). It summarises principles from the book *Refactoring UI*; the book's text is not included. See the pinned source and bundled license in the skill catalog.
