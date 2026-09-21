# Eighteen English sentences on a Traditional Chinese screen

Measured 2026-09-21 across `web/console/src` (excluding `legacy/js/**` and
`public/strings/**`, byte-locked copies) and `internal/`. It continues
`first-run-audit.md` §U6.

**There are two causes and they take different fixes.** Splitting them is the
point of this page.

- **A — never reached the catalog.** English written straight into JSX or an
  HTML fragment, or a zh catalog entry whose value is English. The fix is to
  reach the catalog.
- **B — the daemon's English went past `failureSentence`.** `legacy/js/core/
  failure-text.js:98-131` already says `options.sentence` / `options.fallback`,
  "Neither is ever `message`" — the formatter never puts the daemon's English
  on screen. Every B below is a call site that went around it. The fix is not to
  translate the daemon; it is to route the call site through the formatter.

**The original TypeScript AST pass sees none of B.** It catches a refusal that
lost its name, not a refusal whose English became the sentence. The
cross-language pass now coordinated by `refusals/scan.ts` accounts for all
eighteen rows below and treats `.detail`, `.message`, and daemon-produced error
prose reaching a screen without `failureSentence` as `producer_prose`.

## In the order a new person meets them

| # | Where | Shown | Cause |
|---|---|---|---|
| 1 | `App.tsx:789` | `Dashboard` in the drawer | A. The comment admits it: no catalog key names it. `看板` and `現在` next to it both go through their words module. |
| 2 | `pages/schedules.tsx:1581`, `:1583`, `:1589` | `Schedules`, `New schedule`, `Scheduled tasks` | A. `:207` keeps the section visible with no schedules when the tab may write, so it is on the first screen. Twenty other labels on that page are translated. |
| 3 | `Dashboard.tsx:336`, `411-412`, `439`, `468`, `508` | `Sessions`, `Obligations`, `Tasks`, `Schedules`, `Coordinator` | A. Every empty state in the same file is Chinese; only the panel headings are not. |
| 4 | `Dashboard.tsx:388`, `523` | `running / transcript`, `gen 12` | A, and not translation: wire enums used as prose. |
| 5 | `pages/usage/section.html` (34 lines) | the whole Usage page | A. Its drawer row is shown and `ready()` lets it through, so a new person can reach it; the file's claim that `#page=usage` is the only way in was true of the Swift app, not here. |
| 6 | `pages/projects/section.html:9-10`, `19`, `34-40` | `Projects`, its lede, the worktree block | A. `projects-title` is only ever `.focus()`ed, never written; the lede has a translation path that runs only when a board answer carries the mode. |
| 7 | `pages/settings.tsx:307` | `Loading…`, forever | A, and a lie: nothing writes that id, so it is not loading — the toggle does not exist on this daemon. |
| 8 | `pages/settings/BoardBlock.tsx:136`-`193` | seven strings | A. `{board ? words(en, zh) : "<English>"}` — no board answer falls back to English rather than to Chinese. |
| 9 | `pages/settings.tsx:294` | `Oldest first` | A, for the half second before settings load. |
| 10 | `pages/work/words.ts:150`, `155` | `Backlog` | A, **and the zh value is itself English**. No "did it use `T.`" check can catch this. The other hundred entries are Chinese. |
| 11 | `pages/schedules.tsx:194` ← `app/schedules.go:162`, `171-179` | a Go JSON parse error, verbatim | **B**. `error_kind` is already classified; the screen prints `error` instead of looking the kind up. |
| 12 | `Dashboard.tsx:486` | the same Go error, again | **B**, between two Chinese words. |
| 13 | `pages/schedules.tsx:165`, `190`, `192` | `Untitled schedule`, `Invalid schedule`, `invalid` | A. A schedule with no title is common. |
| 14 | `pages/schedules/overlays.html:118`, `130`, `150` | `Model`, `Delete`, `Cancel` | A. `paintStatic()` translates every other label on that form; the delete dialog shows Chinese 刪除 beside English Cancel. |
| 15 | `door/Door.tsx:435` ← `transport/http/auth.go:181`, `192`, `202` | `That is not the password.` | **B**, and sixty lines above it the same file was fixed for exactly this, with a comment explaining why. The password path did not follow. |
| 16 | `pages/settings/window/SettingsWindow.tsx:1351` | a refusal's English `detail` | **B**. The comment above it chose `detail` over `code` for a good reason and stopped one step short: the third option is the formatter, which gives a Chinese sentence and keeps `code · ref` as a tag. |
| 17 | `pages/settings/cloud.ts:135` | English when there is detail, Chinese when there is none | **B**, and backwards. |
| 18 | `app/scheduler.go:138`, `194`; `app/schedules.go:197` | three push notifications | **B**. These are the daemon's own words, not an agent's, and they arrive on a phone in English. |

## Looked at and deliberately not listed

`door/Door.tsx:582-600` is marked "Not translated, deliberately" and the reason
holds: the name is stored on the machine, so it follows the machine's language.
`push.ts:340` is a code tag after a Chinese sentence. `Transcript.tsx:282-289`
is the fix, not the fault — it is the pattern for B: the producer's English
folded into 技術細節, the sentence from the catalog. The plan, ledger, devices,
documents, board and timeline fragments were each checked id by id against
their runtime translation and are clean; so are `bar/words.ts` and
`next-strings.ts` (185 keys, both sides aligned).
