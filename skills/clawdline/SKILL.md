---
name: clawdline
description: |
  Use Clawdline to dispatch a bounded child task while this session keeps synthesis, integration
  and landing; to hand an existing line of work to another session; to open an independent Root
  for a new feature; to send a message or a picture to another live session; to tell the person
  something through a notification; or to record this session's own finished turn as a delivery
  receipt — the check that says "delivered, awaiting approval" on Clawdline's screen, in any
  repository Clawdline watches. Triggers include "dispatch a task", "open a child session",
  "hand this off", "report my milestone", "record this turn as delivered", "show the user this
  screenshot", and 「派任務」「開 child」「交接給下一個 session」「回報這一輪做完了」
  「更新 milestone」「在 Clawdline 上顯示完成」「把這張截圖給他看」. Do not use for work this
  conversation can simply do, or for provider-native subagents. When this session is a Clawdline
  child, the CHILD.md its briefing names governs instead.
---

# Clawdline

**This file is a pointer, not the guide.** The guide is compiled into the `clawdline` binary, so
the one you read is always the one written for the daemon that will answer you. Routes, fields and
refusals are deliberately not repeated here: a copy of them goes stale against the build.

## Load the guide first

Find the binary once, in this order, and use the first that exists:

1. `$CLAWDLINE_SKILL_READER`, if it is set.
2. `$CLAWDLINE_NEXT_DIR/bin/clawdline`, if `CLAWDLINE_NEXT_DIR` is set; otherwise
   `~/.config/clawdline-next/bin/clawdline` (`$XDG_CONFIG_HOME/clawdline-next/bin/clawdline` when
   `XDG_CONFIG_HOME` is set; `%APPDATA%\clawdline-next\bin\clawdline.exe` on Windows). The daemon
   keeps an up-to-date copy of itself there every time it starts.
3. `/Applications/Clawdline Next.app/Contents/MacOS/clawdline`, then the same under
   `~/Applications`.

Call it `BIN` below, and substitute the real path; do not run `BIN` literally.

```
BIN guide            # English
BIN guide zh-TW      # 繁體中文
BIN guide -list      # what this build carries
```

Reading the guide is a local read: it needs no running daemon and no network. **Read it before you
run anything else**, and do not guess routes or fields from memory, from an older guide, or from
the Swift app's bundle — that app's routes are not this daemon's.

If the path you chose exists and fails, report its exact error and stop. Do not fall through to the
next path: another binary may be a different build from the daemon that will answer you. If none
of the paths exists, Clawdline Next is not installed here; say so rather than inventing routes.

## Roles

Which kind of work goes where is a boundary, not a routing detail; the guide names the route for
each.

- **Owned child**: a bounded task under this session, which keeps synthesis, integration and
  landing.
- **Handoff**: continuing or transferring an existing line of work, with its full state.
- **Detached automation**: unattended work with no root. Never a Root or a feature owner.
- **Root assignment**: a new, independently owned Root for a new feature. Never faked with a child,
  a detached task or a handoff.

A Clawdline **child** does not dispatch and does not send a turn receipt; it reports through the
`result.json` its briefing describes.
