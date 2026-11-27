# Acceptance: Bots settings and dev server rows at 390px (main 6904a5552)

Captured 2026-11-24 between 10:34Z and 10:55Z by a dispatched evidence child. Synthetic fixtures
only: no daemon on 7727, no person's data, no Cloud account, no paid API.

**Overall: NEEDS WORK** — 1 PASS (claim 3), 1 PASS with a minor layout finding (claim 1),
1 partially unverified (claim 2), claim 4 (1280 comparison) captured. One minor visual issue and
two test-infrastructure defects are listed below.

## What was under test

- Source: `git archive 6904a5552 web` extracted into the task's `work/src`; dependencies linked from
  the shared checkout's `web/node_modules` except the `@clawdline/*` workspace links, which point
  into the extracted 6904a5552 tree. The Go route test ran on this task branch's base
  (`df6468593`), after `git diff --quiet 6904a5552 HEAD` confirmed
  `internal/transport/http/project_assistants_machine_settings{,_test}.go` and
  `internal/app/cloudops/ops_project_assistants.go` are identical to 6904a5552.
- Browsers: Playwright from an existing installation, Chromium (with
  `--disable-features=OmniboxEverywhere`) and WebKit, headless, `deviceScaleFactor: 2`,
  locale `zh-TW`, dark fixture page. Mobile runs use `390x844` with `hasTouch`; desktop runs
  `1280x800`.
- Harness: `acc-6904a55.e2e.mjs` (kept in the task artifacts with its two fixture copies). It
  mounts the committed fixtures `bots-settings.fixture.tsx` and `roster-mixed.fixture.tsx`, and a
  copy of `tools/project-assistants-dev-servers.fixture.mjs` / `tools/fixtures/project-assistants-dev-servers.tsx`
  with two changes made only in the copy: the import of the removed `BotDetailsPanel` is dropped,
  and two synthetic rows are added — `Lost preview server after restart` (`state: "unknown"`) and
  `Storybook on a long name that wraps` (`state: "running", unconfirmed: true`).

Rerun (from the extracted `web/console`):

```sh
PLAYWRIGHT_MODULE=<playwright index.mjs> BOTS_ARTIFACTS=<dir> \
  ACC_ENGINES=chromium,webkit ACC_SIZES=390x844,1280x800 node tools/acc-6904a55.e2e.mjs
```

The final verdicts come from four runs (raw logs kept): `acc2.log` (Chromium 390),
`acc-webkit.log` (WebKit 390), `acc-c1280.log` (Chromium 1280), `acc-w1280.log` (WebKit 1280).
Earlier runs failed for harness reasons described under "Harness problems"; their logs are kept too.

## Claim 1 — Bots settings opens, edits and saves at phone width — PASS (minor finding)

Quoted requirement: open Bots settings from the Bots list "⋯" menu, change a value, save, reopen
and see it kept; focus is correct after Esc/close; at 390x844 the modal is not clipped and the
Safe Area and bottom buttons are usable.

| Step | Chromium 390 | WebKit 390 | Chromium 1280 | WebKit 1280 |
|---|---|---|---|---|
| List options (`清單選項`) holds `Bots 設定` (`aria-haspopup=dialog`), opens the dialog | ok | ok | ok | ok |
| Esc closes it and focus returns to the options button | ok | ok | ok | ok |
| Dialog box | 0,0 390x844 | 0,0 390x844 | 300,56 680x688 | 300,56 680x688 |
| `儲存` on screen and hit-testable (`elementFromPoint`) | y 796–832 of 844 | y 796–832 | ok | ok |
| No control horizontally outside the viewport; `scrollWidth == innerWidth` | ok | ok | ok | ok |
| Add memory entry, `儲存`, fixture records the saved document | ok | ok | ok | ok |
| Reopen shows the new entry | ok | ok | ok | ok |

- Screenshots: `1-entry-menu-<engine>-<w>.png`, `1-entry-modal-…`, `1-settings-open-…`,
  `1-settings-edited-…`, `1-settings-saved-…`, `1-settings-reopened-…`.
- What `1-settings-saved-webkit-390.png` shows: header `Bots 設定` with `×`; two rule cards
  (`寫程式`, `生圖` scoped to `Sample Web`); the shared memory section with the original entry and
  the new `合成驗收記憶 webkit-390`; a fixed footer with `取消` and a greyed `儲存` after the save.
  No text is clipped and no horizontal scroll appears.
- Safe Area: at ≤640px the panel uses `padding-top: env(safe-area-inset-top)` and the footer
  `padding-bottom: max(12px, env(safe-area-inset-bottom))`
  (`web/console/src/project-assistants/bots-settings.css:41-42`). Headless browsers report zero
  insets, so the inset values themselves are unverified on a notched device; the CSS is present.
- The entry fixture (`roster-mixed`) has no shared-settings source, so the dialog opened there
  shows the typed "version too old" message (`1-entry-modal-webkit-390.png`): the entry path and
  the edit/save path are proved with two different fixtures, not one continuous flow.
- In `bots-settings.e2e`'s own fixture the opener is a plain button; WebKit does not focus a
  clicked button, so focus after Esc lands on `body` there (`focusAfterEsc: ""`). The real entry
  path above returns focus correctly in WebKit, so this is a fixture limitation, not a defect.

**Finding 1.1 (minor, confidence high).** At 390px a fallback agent row whose controls do not fit
wraps its `×` remove button onto a line of its own (the `生圖` rule's first `Codex` row and the
`Grok · 此電腦無法使用` row in `1-settings-saved-webkit-390.png` and the Chromium equivalent).
A person sees a lone `×` under a row with blank space to its left, and must infer which row it
removes. Cause: `.pa-bs-agent { flex-wrap: wrap }` with `.pa-bs-model { min-width: 120px }` and
`.pa-bs-icon { margin-inline-start: auto }` (`web/console/src/project-assistants/bots-settings.css:18-19,49`).
Not a blocker: the button is visible and usable.

## Claim 2 — Open and save through the Cloud path — PARTIALLY UNVERIFIED

Quoted requirement: open and save Bots settings through the hosted console's relay to a machine.

Executed:

- `node --experimental-strip-types --test --test-name-pattern="[Ss]hared settings|[Ss]ettings" src/cloud/project-assistants.test.ts`
  on 6904a5552: 3 pass, 0 fail, including "Bots shared settings cross the Cloud relay on the named
  machine and save with the version read" and "an older machine answers Bots shared settings as
  unsupported before anything is sent" (`logs/cloud-ts.log`).
- `go test -count=1 -run 'TestProjectAssistantMachineSettings|ProjectAssistant.*Settings' ./internal/transport/http/ ./internal/app/cloudops/`:
  `internal/transport/http ok`; `internal/app/cloudops` had no test matching that pattern
  (`logs/go.log`).

Not executed, and why: a hosted bundle served by `build:cloud` needs a signed-in Cloud account and a
paired machine to relay to. The brief forbids the person's account and port 7727, and no
self-contained Cloud relay fixture exists in the 2a025526/90211651 commits. The browser surface of
the modal itself is the same component in both consoles (the fixture passes a source "as a Cloud
or local console passes one"), but a real relay round trip in a browser is **unverified**.

## Claim 3 — The dev server "can't confirm" row at 390px — PASS

Quoted requirement: text complete, no broken layout, the action (remove a lost server) usable,
status words and colours consistent with other rows.

Rendered inside the real `Chat` component's information panel (`詳情` tab), six rows:

| Row | State text shown (zh-Hant) |
|---|---|
| Web preview | `執行中 · :<port>` |
| Watch build | `已停止` |
| Network preview | `執行中 · :43124` |
| Unconfirmed watcher | `執行中` |
| Lost preview server after restart | `狀態未確認` |
| Storybook on a long… | `執行中 · 無法確認 · :<port>`, followed by the full sentence `Clawdline 暫時無法確認這個 Bot 是否仍可執行這個伺服器。它會繼續執行，每秒重新確認一次；不需要的話可以停止它。` |

- In all four engine/size runs: no row overflows, no state text or note is clipped at the section's
  right edge, `document.scrollWidth` equals the viewport width, and every state text has the same
  computed colour `rgb(153, 153, 159)`. Only the long name is shortened with an ellipsis, by design.
- The lost row's `⋯` menu shows the warning `Clawdline 已無法確認這個伺服器的狀態（例如重新啟動之後）。移除只會停止追蹤；指令如果還在執行，請自行停止。`,
  then `日誌` and `移除（無法確認已停止）`. The remove item is enabled, hit-testable, inside the
  viewport (390: 302x44 at x 63), and clicking it removes the row (`3-devservers-after-remove-…`).
- Screenshots: `3-devservers-<engine>-<w>.png`, `3-devservers-list-…`, `3-devservers-lost-menu-…`,
  `3-devservers-after-remove-…`.

**Finding 3.1 (nit, confidence high).** On a touch screen the row's `⋯` button is 36x36 px
(`.pa-dev-server .pa-server-menu { width:36px; min-height:36px }`,
`web/console/src/project-assistants/dev-servers.css:10`), below the 44px target the Bots toolbar
asserts for itself. It is still tappable in the capture.

## Claim 4 — 1280x800 comparison — CAPTURED

Every screenshot above exists at 1280 in both engines. At 1280 the settings dialog is a centred
680px panel and the dev server menu opens at the right edge (remove item 234x36 at x 1017).
Nothing at 1280 was clipped.

## Harness problems found (not product defects)

1. `web/console/tools/project-assistants-dev-servers.fixture.mjs` does not build at 6904a5552: its
   fixture imports `../../src/project-assistants/BotDetailsPanel`, which no longer exists
   (`web/console/tools/fixtures/project-assistants-dev-servers.tsx:7`). Raw error in
   `logs/dev-servers.log`.
2. `roster-toolbar.e2e.mjs` (and this harness's first version) can hang until killed at 1280:
   it awaits every `document.getAnimations()` `finished` promise inside `page.evaluate`, which has no
   timeout, and an infinite animation never finishes. This harness bounds the wait; the committed
   test does not (`logs/roster-toolbar.log`, exit 124).

## Issues seen that no claim covered

None beyond the findings above.
