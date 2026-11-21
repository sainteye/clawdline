# Provenance

Lantern Deep is original to this repository. It ports no upstream project.

- **Characters, enemies, props, effects and motion** are drawn procedurally on a
  canvas by `scene.js`, written for this view. The hooded wanderer, the crawler and
  the drifter are original designs; no sprite, frame or character from any existing
  game is included or traced.
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
