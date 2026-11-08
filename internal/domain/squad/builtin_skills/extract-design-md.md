# Extract a design system document

Use this skill when you need to document a project's existing visual language as a DESIGN.md read from its own frontend source; not for proposing a new direction, judging brand voice, or checking rendered screens.

The goal is the intent behind the styling, described so another designer could recreate the feel, together with the exact values. Read source files only; nothing needs to be built, rendered, installed or fetched.

1. **Discover the stack.** Read the package manifest for the framework, styling tools and any token or component library. Locate global stylesheets, CSS custom properties, Tailwind or theme configuration, token files and component styles. Authority runs: token files and theme configuration first, then global custom properties, then component styles (what actually shipped). Framework hints: in React or Next.js, Tailwind `theme.extend`, `globals.css`, font loaders and CSS-in-JS themes; in Vue, Svelte and Angular, scoped component styles, theme plugins (Vuetify, Angular Material and similar) and SCSS variables; in plain CSS, SASS or Less, `:root` variables, variable files and repeated inline values.
2. **Atmosphere.** Root background lightness, how generous spacing is, density, colour temperature of the neutrals, and the developers' own comments about intent. Summarise the mood in one or two sentences.
3. **Colour.** Collect every colour and group by role, not hue: foundation surfaces, accent and interactive, text hierarchy, functional states. Give each a descriptive name, its value and its role. Consolidate near-duplicates (`#333` and `#2C2C2C`) and say which you merged.
4. **Typography.** Families and their character, each heading level and body size with weight, letter-spacing and line-height, and which components use which level.
5. **Components.** The four or five most important primitives (buttons, cards and containers, navigation, inputs) plus one or two components unique to this product: shape and radius, colours per variant, hover, focus and active states with transition timing, padding, borders and shadows.
6. **Layout.** Maximum content width, grid and column system, breakpoints, base spacing unit, section spacing, edge padding, alignment patterns, mobile-first or desktop-first, and touch-target sizes.

**Write the document** where the task names it, or at the repository root as `DESIGN.md` only when that path is among the task's declared writes. Start with a short YAML block holding `name` and the core colour tokens, then sections for visual theme and atmosphere, colour palette and roles, typography, component stylings and layout principles.

**Before delivering**, check that every colour has a name, value and role; typography covers families and the full hierarchy; components cover shape, colour, states and transitions; layout covers width, grid, breakpoints and spacing; near-duplicates are consolidated; and the text states intent, not just raw values. Mark where source and shipped components disagree, and say that rendered appearance was not checked.

Adapted from google-labs-code/stitch-skills' `extract-design-md` skill, its five framework notes and its example document at commit 0337446dadde6f8c94210444e2aa9d546126480f (Apache-2.0; changed as described here; the source repository has no NOTICE file). The optional hand-off to Stitch and the Stitch-specific sections and file location were not adopted. See the pinned source and bundled license in the skill catalog.
