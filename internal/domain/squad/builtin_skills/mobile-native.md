# Native feel on mobile web

Use this skill when a web app is built for or reviewed on phones and feels like a website: sticky hover, tap flashes, wrong heights, input zoom, laggy taps, scroll hijacking, notch overlap; not for React Native or motion design.

Most "janky on mobile" reports are platform-layer problems, each fixed by one declaration. Apply a fix only where its reason applies, and write it in the project's styling system.

**Hard rules.** Gate by capability with media queries (`hover`, `pointer`, `env()`, `dvh`), never user-agent sniffing or screen width; touch and mouse can coexist. Never disable zoom with `user-scalable=no` or `maximum-scale=1`; fix the input size instead. Device emulation reproduces none of these bugs, so say which fixes you verified from code and which need a real phone.

| Symptom | Fix |
| --- | --- |
| Hover stuck after a tap | Put every `:hover` rule inside `@media (hover: hover) and (pointer: fine)`; give touch an `:active` state |
| Grey or blue flash on tap | `-webkit-tap-highlight-color: transparent` once on `html`, then an `:active` state on every tappable element |
| Wrong height under the URL bar | `100dvh` for app shells and drawers, `100svh` (as `min-height`) for heroes |
| Page zooms into an input | Input, textarea and select text at 16px or more, at least under `(pointer: coarse)` |
| Taps feel laggy | `touch-action: manipulation` on controls; feedback on press via `:active` (100–160ms, ease-out) or `pointerdown`, not on `click` |
| Pull-to-refresh or rubber band hijacks an app | `overscroll-behavior: none` on `html, body`; `contain` on inner scrollers. Not on a document where pull-to-refresh is welcome. Never a `touchmove` + `preventDefault` listener |
| Content stops at the notch | `viewport-fit=cover` in the viewport meta, then `env(safe-area-inset-*)` padding (with a `0px` fallback in `calc`) on fixed headers, bottom bars, toasts and sheets |
| Long-press selects a button label | `user-select: none`, `-webkit-user-select: none` and `-webkit-touch-callout: none` on controls only; never on `body`, since people copy addresses and error messages |
| Carousel drags the page | `touch-action: pan-y` on a horizontal gesture surface, `none` only on surfaces that own every axis; for native scroll prefer `scroll-snap-type` |
| Status bar colour mismatched | A `theme-color` meta per `prefers-color-scheme`, matching the top of the page; update it when the app switches theme by class |

Set the keyboard too: `inputmode` (`numeric`, `decimal`), `type="email"` or `tel`, `autocapitalize="none"` and `autocorrect="off"` for codes and usernames, `enterkeyhint` naming what Return does. `interactive-widget=resizes-content` in the viewport meta makes Android's keyboard resize the layout like iOS.

**Device testing.** Check on real hardware, an older phone, with the keyboard open and once in landscape; standalone PWA mode changes viewport and safe areas. Serving a development server on the local network or opening a port needs the task's authorisation; otherwise list the device checks as pending.

**Output.** Apply the fixes, then a few lines only: what was wrong (the matched symptom and its reason), what changed (file and declaration), and what still needs a phone.

Adapted from emilkowalski/skills' `mobile-native` skill at commit a81dbca07f2217b7103e510c0f13f8d5b017afc9 (MIT; copyright 2026 Emil Kowalski). See the pinned source and bundled license in the skill catalog.
