# Product interface design

Use this skill when designing or refining a product interface such as a dashboard, tool, settings page or data view; not for a landing page, campaign or brand-only work, and not for a review that only grades an existing screen.

Generic output comes from defaults that look like infrastructure: the usual font, the usual sidebar, the usual metric card. Decide each of them on purpose.

1. **Intent first.** Before code, write a short brief: who the person is and where they are, the one verb they must accomplish, and what the screen should feel like in concrete words ("dense like a trading floor", not "clean and modern"). Apply that intent to every token: surfaces, text, borders, accents and type all follow it. If the request is too vague to answer these, ask one question or state your assumption.
2. **Explore the domain.** Name at least five concepts from the product's world, five colours that exist in it, one signature element only this product could have, and the three obvious defaults you are rejecting. Remove the product name from your proposal: someone should still guess what it is for.
3. **One focal point per view.** Name the element the person came for and make it win through size, weight, contrast or surrounding space. Build hierarchy from size, weight and colour together; a single size can hold three tiers through weight and opacity. Pick a type ratio (about 1.2 for dense UI, 1.25 for most product UI) and round to whole pixels.
4. **Density and rhythm are decisions.** Choose the padding values once and repeat them. Space related things tightly and put real space between groups. Widths state relationships: decide what a sidebar's width says.
5. **Quiet structure.** Surfaces stack with small lightness steps; inputs sit slightly inset. Borders are low-contrast and findable, not loud. Pick one depth strategy (borders, subtle shadows, layered shadows or surface shifts) and keep it. One accent colour carries meaning; grey builds structure.
6. **Use what exists.** Native HTML first, then the project's existing component library or primitives, then its tokens. Hand-roll a control only when nothing fits, and then implement its full keyboard, focus and ARIA contract. Adding a new dependency needs the task's authorisation.
7. **Before writing a component**, state its intent, focal element, palette, depth, surfaces, type and spacing, each with a reason. A choice you cannot explain is a default.
8. **States are part of the design**: default, hover, active, focus and disabled for controls; loading, empty and error for data.
9. **Check before showing.** Swap test (would a standard template look the same?), squint test (does hierarchy survive blurring?), signature test (point to five places the signature appears), token test (do token names belong to this product?). Then verify the rendered result at desktop and mobile widths.

When the project already documents its design system, its decisions win over this text; extend them rather than replace them. Write design notes to a file only when the task asks for it.

Adapted from Dammyjay93/interface-design's `interface-design` skill at commit 2f9be3206855bcb2d1d0af262c8bae25cba6658d (MIT; copyright 2026 Damola Akinleye). See the pinned source and bundled license in the skill catalog.
