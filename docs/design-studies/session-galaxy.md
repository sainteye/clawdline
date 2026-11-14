# Session Galaxy — design prototype

Status: **design prototype with a mock bridge.** `session-galaxy.html` is a standalone,
self-contained page (no external scripts, fonts or images). It was verified from a
local HTTP server; `file://` was not tested. It is not an installed plugin, not a View Plugin v1 host, and nothing in it
talks to a daemon. All Sessions are synthetic demo data and nothing can be changed through it.

## What it is for

Clawdline runs many AI coding Sessions at once. The galaxy answers one question at a glance:
**which Sessions need me, and how badly?** The person sits at the centre ("你"). Distance from
the centre is attention; Project is direction.

- **Orbit = work state.** From the core outward: waiting for you (0.26), waiting for another
  Session, holding, working, ready, milestone complete, work complete, and an outer motionless
  fog band for unknown (1.14). Only states that are present get a ring, and each ring is named
  in text where it passes nearest the viewer.
- **Sector = Project.** Each Project owns an angular sector with its own nebula hue and a text
  label at the rim. Sessions without a Project share an "未指定 Project" sector.
- **Every state has its own glyph shape** (ember disc with "!", ion orb with spinning arc,
  hourglass, pause bars, ring-and-dot, half diamond, solid diamond with check, dashed hollow
  circle with "?"), its own text label, and its own radius — colour is never the only cue.

## Motion means something

Motion is reserved for states that are actually moving and fresh:

- Waiting-for-you Sessions emit expanding beacon rings and a light thread with particles
  flowing toward the core.
- Working Sessions drift a little along their orbit with a comet tail.
- Complete states are still, with a faceted crown of light. Unknown is grey, unlit and
  motionless; it never pulses, so absence of data is not disguised as activity.
- **Stale or unknown freshness removes all motion and bloom**, adds a dashed outline and a
  "可能過時 / 新鮮度不明" tag. After a host disconnect every row renders as stale immediately and
  a banner says so.
- No completion percentages, ages, transcript content or progress are invented: the projection
  only carries `ref`, `title`, `project`, `workState`, `freshness`.

## Interaction

1. **Collapsed entry** in the bottom status bar: a tiny orbit icon, "星系" and a host-owned
   attention badge ("2 等你"). The bar is 32px; the button has a 44px hit area.
2. **Expansion**: a circular clip-path reveal from the entry button, while the camera dollies in
   from far away (≈0.8s). Reduced motion uses a short fade and no dolly.
3. **Scene**: canvas 2D with a custom perspective projection, a three-armed spiral starfield
   (2,200 stars desktop, 1,200 phone), Project nebulae, a cheap bloom pass (a 1/3-resolution
   glow layer blurred and added), slow auto-rotation. Drag rotates/tilts, wheel zooms.
4. **Sessions are real HTML buttons** positioned over the canvas every frame: crisp labels,
   44px targets, keyboard focus, full accessible names (title, Project, state, freshness).
   Labels are placed by priority (selected > waiting for you > fresh > nearer) and a label that
   would collide is hidden; the node stays. Phone shows labels only for waiting-for-you and the
   selection.
5. **State chips** (count, label, one-line meaning) filter/highlight a ring and select its first
   Session. **清單** opens a grouped list of every Session — the readable path that needs no
   animation, also used as the fallback if canvas is unavailable.
6. **Selecting** turns the camera to bring the Session to the near side and opens details (right
   panel on desktop, bottom sheet on phone): state tag, title, Project, what the state means,
   freshness, and **前往這個 Session**, which calls `view.sessions.open(ref)`. A refusal shows
   its typed code (e.g. `host_unavailable`); there is no automatic retry.
7. **Return**: "✕ 返回 Session" calls `view.close()`; Escape first closes details, then the view.
   The host restores the originating Session surface, its scroll positions and focus on the entry.

## Separation: scene, adapter, host

The single file has three clearly separated parts:

| Part | Role | Knows about |
|---|---|---|
| A. `MockHost` | **Demo only.** Fixture rows, a mock adapter with the author-facing View Plugin v1 shape, scripted later snapshots, connection and reduced-motion context | fixture, host navigation |
| B. `GalaxyPlugin.mount(root, connect)` | The plugin scene | only `connect()` and its own DOM |
| C. Demo host shell | Status-bar entry, badge, Session list, open/close, Escape, scroll/focus restoration, a labelled "示範宿主控制" dock | `MockHost`, `GalaxyPlugin` |

The scene follows the adapter contract from View Plugin v1 revision 1 (an implementation
specification, not a shipped capability):

```js
const view = await connect();                         // connectViewPlugin() in a real host
const initial = await view.sessions.subscribe(onLaterSnapshot); // later snapshots only
render(initial);
view.onContext(applyContext);                         // reducedMotion, visible, connection, scopeLabel
await view.sessions.open(ref);                        // explicit click only
await view.close();                                   // host closes and restores
view.dispose();                                       // on close; reopening starts a new instance
```

- Snapshots are complete replacements; equal or older revisions in the same stream are ignored.
- Reconnection keeps retained rows stale until a newer snapshot arrives. A delayed adapter
  connection or focus timer cannot reopen or refocus a view that the host already closed.
- Each opening creates a new mock instance with new opaque refs and stream id; refs are never
  stored.
- Titles and labels are treated as untrusted text and are only written with `textContent`. The
  fixture deliberately includes the title `<b>標題是純文字</b> 沒有 Project`, which renders as
  literal characters.
- The scene never fetches, holds credentials, reads the fixture or reaches into the host.
- The render loop runs only while the host context says `visible` and the document is not
  hidden. Closing disposes the adapter, stops the loop and drops the scene DOM. With reduced
  motion it renders only when something changes.

**Not implemented here** (belongs to the future host, per the spec): ZIP install/activation,
the sandboxed `allow-scripts` iframe, `clawdline.plugin.hello`/MessagePort bootstrap, CSP,
Cloud resource transport, capacity limits, conformance tests. In this prototype the plugin
shares the document with the mock host; a real host would load Part B in a sandboxed frame and
replace Part A with its SDK, unchanged scene code being the goal. The host-owned badge is
computed by the mock host, not the plugin.

## Validation performed

Browser: Clawdline-driven user Chrome (`clawdline browser`), served from `127.0.0.1:8791` with
only `docs/design-studies/` exposed; server stopped and all owned tabs closed afterwards.

- Desktop (≈1224×757 CSS px): collapsed entry → expansion → select "正式環境部署前檢查" →
  details → 前往這個 Session (host switched to that Session, scene paused). Frame counter
  stayed at 1026 across reads while collapsed. Scrolled the host list to its last row, opened,
  pressed Escape: list scroll position kept and focus returned to the entry.
- Later snapshot via the mock dock: rev 1 → 2, "看板欄寬自適應" moved to the waiting ring,
  header count became 3. Disconnect: banner, all nodes stale, open refused with
  `host_unavailable`. Reconnect delivered a new revision.
- Reduced motion (host context): frame counter stable (428 on two reads) — on-demand rendering.
- Phone (mobile viewport, 390 px wide): expand, labels limited to waiting Sessions, select →
  bottom sheet with details and a full-width 前往 button, collapse details, close, reopen from a
  new snapshot. No horizontal scrolling observed in screenshots.
- Script parse check with Node (`new Function` on the inline script).

Screenshots are kept in the task's artifacts directory (`01`–`13`). No motion recording was
made: the available browser tool captures still JPEGs only.

## Known limitations

- Labels can still crowd when many Sessions share a ring in one Project; the list view is the
  intended escape hatch, and a real dataset needs a stated bound (`internal/domain/capacity`).
- Phone header controls remain in one row; Project rim labels are clamped within the scene
  and are hidden while the selected Project is shown in the bottom sheet.
- No WebGL-context-loss path is needed (canvas 2D), but there is no automated test that forces
  the list fallback.
- Theme and language context are received but the scene is dark-only and Traditional Chinese
  only.

## Root integration checkpoint

The integrating root removed the scene's direct write to the demo shell's diagnostic element,
kept connection recovery stale until a new snapshot and fenced delayed startup/focus work after
close. Phone header controls no longer wrap and rim labels are clamped to the scene; selected
Project information remains in the bottom sheet. These changes preserve the child's art direction.

The updated inline script parses. A fresh desktop capture was taken using the generic demo
Project labels. Phone revalidation remains pending: the browser tool began refusing further reads
with `browser_setup_required` during the integration run. Earlier child screenshots containing
the retired private demo label are not suitable for delivery. No recording is claimed.
