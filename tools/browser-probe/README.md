# Browser probe

A measurement-only prototype for the Chrome integration. It is not the product
extension: it has no pairing, no allowlist, no lease and no audit log. Results and
conclusions are in `docs/experiments/chrome-extension-probe-2026-11-12.md`.

- `extension/` — an MV3 extension that relays `chrome.debugger` calls to a probe
  server on `127.0.0.1:7791` and nowhere else. Its name, description and popup
  are localized in `_locales/en` and `_locales/zh_TW`; the popup explains what it
  does, what it does not do, why each permission is needed and how to remove it.
- `extension/icons/` — the console's app icon (`web/console/public/icon-512.png`)
  resized with `sips -z <n> <n>` to 16, 32, 48 and 128 px. Regenerate them the same
  way if the app icon changes.
- `run.mjs` — serves the test pages, launches Chromium with the extension, and
  writes the measurements as JSON.

```sh
node tools/browser-probe/run.mjs --browser <Chromium binary> --out result.json
node tools/browser-probe/run.mjs --no-launch --only coexist --tab <tab id>
```

Branded Chrome ignores `--load-extension`, so an automated run needs Chromium
(for example Playwright's). To test in Chrome itself, load `extension/` with
**Load unpacked** on `chrome://extensions` and run with `--no-launch`.
