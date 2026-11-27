# Cross-machine collaboration diagram

The diagram uses the marketing site's neutral surfaces, warm text and violet peer-traffic color. Each Session carries the same fixed pixel mark in violet, not the product's per-Session mark; assistant logos identify its provider. Apple and Linux marks identify the two machines, not the assistant.

- Apple and Linux paths: Simple Icons, revision `4d29a35c954699b389a834aacc97dd6e7e74143c`, https://github.com/simple-icons/simple-icons, CC0 (vendor/simple-icons-CC0.txt).
- Claude and OpenAI paths: existing marketing `AssistantLogo.astro`, from Lobe Icons, https://github.com/lobehub/lobe-icons, MIT (vendor/lobe-icons-MIT.txt). Codex uses the same OpenAI knot as the product's Session list.
- The renderer reads these paths and their colours from `docs/assets/cross-machine-icons.json`, which records no per-entry source, and embeds the application mark from `internal/domain/icon/brand.svg`.
- Logos identify compatible tools and platforms; they do not imply endorsement.

Run `python3 tools/render-cross-machine-diagram.py` to regenerate both SVG locales. The script draws in its original blue and teal colours and then maps each one to the site palette through a replacement table, so a new colour must be added to that table. The website uses a responsive HTML diagram with the same palette and existing assistant/Session components.
