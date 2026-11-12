# Chrome extension probe, 2026-11-12

Step 1 of the board item "Chrome extension integration". The question: can an extension give
Clawdline a live view of a Chrome tab and drive it, and what does that cost? The probe is
`tools/browser-probe/`: an MV3 extension (`chrome.debugger`, CDP 1.3) that relays to a local test
server on 127.0.0.1:7791. It never talks to the daemon on 7727 and runs in a throwaway profile.

```sh
node tools/browser-probe/run.mjs --browser <Chromium binary> --out result.json
node tools/browser-probe/run.mjs --browser <Chromium binary> --only scroll --out result.json
```

## Conditions

- macOS on this Mac, Playwright's Chromium 136.0.7103.25. Branded Chrome no longer honours
  `--load-extension`, so an automated run needs Chromium; the person's Chrome is 157. Results
  for Chrome 157 are inferred, not measured.
- Viewer = the probe server on the same machine. **No Cloud path was measured**: the relay has
  no browser channel yet. The bandwidth figures below are what such a channel would carry.
- Frames are acknowledged by the extension on arrival, so Chrome's own flow control never
  slowed them.

## Results

Streaming (`Page.startScreencast`, JPEG). Latency is from the frame's capture timestamp to the
server receiving it.

| Page and settings | fps | KB/s | KB per frame | latency p50 / p95 |
|---|---|---|---|---|
| Full-motion animation, 1280 px, q70 | 93.1 | 1034 | 11.1 | 3.1 / 4.1 ms |
| Full-motion animation, 800 px, q50 | 93.2 | 484 | 5.2 | 1.2 / 1.7 ms |
| Same, `everyNthFrame: 3` | 31.1 | 161 | 5.2 | 1.3 / 2.2 ms |
| Long text page scrolled 4 times a second, 1280 px, q70 (three runs) | 4.7–5.1 | 474–505 | 99.8–100 | 5.8–6.7 / 8.9–10.3 ms |
| Same, 800 px, q50 (three runs) | 4.9 | 190 | 39 | 2.3–2.5 / 2.9–3.8 ms |
| Same, 800 px, q50, `everyNthFrame: 3` | 1.6 | 64 | 40 | 2.3–2.4 / 3.0–4.8 ms |
| Static page, 10 s | 1 frame | 7.6 avg | 76 | 10.7 ms |

- The full-motion figure is bounded by the display (a 120 Hz panel). An uncapped stream follows
  the display, so the product must cap the rate itself.
- Real pages produce frames only when they change. The scrolled text page is closer to an agent
  browsing than the animation: about 100 KB per desktop frame, 40 KB at phone size.

When the tab is not drawn on screen:

| Situation | Frames in the window | `Page.captureScreenshot` |
|---|---|---|
| Another tab is in front (10 s) | 0 | ok, 175 ms, 32.6 KB |
| Window minimized (10 s) | 1 | ok, 58 ms, 33.2 KB |
| Window covered by another window (8 s) | 1 | ok, 54 ms, 28.9 KB |
| Window moved off screen | not testable: Chrome refuses bounds more than half off screen | — |

Input and control:

- Mouse click (`Input.dispatchMouseEvent`): 20 of 20 landed; server to page handler p50 1 ms,
  p95 3 ms; command round trip p50 1 ms, p95 6 ms.
- `Input.insertText("測試中文輸入 hello")` then a Backspace key event: the field held
  `測試中文輸入 hell`, exactly as intended. Chinese goes in without an input method.
- `alert()` raised `Page.javascriptDialogOpening` and `Page.handleJavaScriptDialog` dismissed it.
- The MV3 service worker kept its WebSocket through 45 s of silence with the debugger attached.
  Longer idle periods and an idle worker with no debugger attached were not tested.

## What this changes in the design

1. **The live panel cannot rely on streaming alone.** When the person is on the phone, Chrome on
   the Mac is usually covered, minimized or showing another tab, and then the stream stops. The
   panel must fall back to screenshots (54–175 ms each here) on a timer, and say which it is
   showing, as the terminal panel does for its live and sampled backends.
2. **Rates need registered bounds.** Phone view at 800 px, q50, with a frame-rate ceiling costs
   about 190 KB/s while a page scrolls and nearly nothing while it is still. The ceiling and frame
   size go into `internal/domain/capacity`.
3. **Acknowledge frames from the viewer, not the extension**, so a slow Cloud path slows the
   stream instead of queueing frames.
4. Typing Chinese from the phone through `Input.insertText` works as designed.

## Not yet measured

- Attaching while Claude in Chrome drives the same tab. It needs the probe loaded unpacked into
  the person's Chrome 157.
- Latency and throughput through Cloud. They need the relay channel (step 12).
- Locked screen and sleeping display.
- Grok. From `docs/grok-cli.md`, an ordinary Grok Session is supervised by default, so each
  shell call to `clawdline browser` asks for permission unless "Grok full permission" is on. Its
  first real input carries the frozen Project context, which is where usage guidance would go.
  Neither point was observed in a live Grok Session.
