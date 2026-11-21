# Provenance

Lantern Deep is original to this repository. It ports no upstream project.

- **Characters and enemies** are generated artwork. Twelve wanderer designs came from
  concept sheets generated on 2026-11-21 (eight with the Codex CLI's image generation,
  four with the Grok CLI) from prompts asking for original designs with no named
  reference. Six enemy designs came from a Codex concept sheet. Each design was then
  redrawn by Codex, with the concept as reference, into a sprite sheet on a flat key
  colour (four sheets and 50 kept frames per wanderer, 8 per enemy; the eight wanderers
  with new weapons had all three sheets redrawn with that weapon, the first sheet serving
  as the reference for the other two), keyed to transparency, cut into equal
  cells and encoded with libwebp at 200 x 200. They are not hand-drawn and were not
  retouched; each sheet was reviewed for consistent design before import.
- **Scenery sprites** (three slabs, a pillar, a training dummy, an archway, a fossil
  shell, mushrooms, a four-frame campfire, a bedroll, a tent, a cooking pot, firewood) were generated and keyed the same way.
- **Effects and motion** (physics, strike arcs, sparks, glows, fog, lamps, ground,
  dividing rock) are drawn procedurally by `scene.js`, written for this view. A
  procedural hooded wanderer remains as the fallback when frames are absent.
- No sprite, frame or character from any existing game is included or traced.
- **Backdrops** (`art/bg-ruins.webp`, `art/bg-grotto.webp`, `art/bg-crystal.webp`)
  were generated on 2026-11-21 with the Codex CLI's built-in image generation from
  prompts describing original scenes (ruined arches with fossil shells, an overgrown
  grotto, a crystal mine) with no characters, text or named reference. The 1664 x 936
  results were encoded with `cwebp -q 80`. They are not hand-painted and were not
  retouched.
- **Cover** is authored in `cover.svg` and rendered by
  `tools/render-view-plugin-covers.mjs`.

The view takes its genre from hand-drawn side-scrolling exploration games: a dark
layered cavern, fog, small lights and quick blade work. That is an atmosphere, not a
copy of any one game's characters, locations, artwork or names.
