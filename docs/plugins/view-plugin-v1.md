# View Plugin v1

Revision 11 describes the **experimental browser-local runtime** implemented in
`web/console/src/view-plugins`. The supplied packages are Session Galaxy, Session Orrery, Session Night City,
Session Departures, Session Territory, World of Codecraft, Kaiju City and Lantern Deep. Public Release acceptance is separate from source and Cloud delivery.
Revision 11 adds the embedded presentation: a desktop can pin a view beside its Session
list ([Full screen and embedded](#full-screen-and-embedded)). The wire protocol is unchanged.
The earlier revision proposed machine-owned installation; that transport is deferred.

## What the host owns

The sidebar’s **View plugins** entry offers the full gallery, ZIP import, and
package management. The status-bar orbit button opens a single enabled view
directly and shows the launcher when there are zero or multiple enabled views.
The launcher recommends Session Departures and World of Codecraft until each
package is installed. Selecting a recommendation opens package review in place.
The orbit entry follows the provider quota bar’s visibility.
An opened view fills the app. On a desktop (900 CSS pixels and wider) the launcher and a
full-screen view also offer **Pin to the right**: the pinned view then fills the Session
pane whenever no Session is open there, in place of the home screen.
The host owns installation, consent, current machine identity, history, focus,
close and Session navigation. The plugin owns its own HTML, CSS, canvas and motion.
The Session page, classifier and dispatch broker are not plugin extension points.

After the host update, importing, replacing, disabling, removing and rolling back
compatible packages needs no core edit, frontend rebuild, daemon restart or app
reinstall. Each browser/interface stores its own packages in IndexedDB. They are
not copied between phones, machines, accounts or browsers. Packages do not start
automatically. Opening an installed view immediately reads **all currently
confirmed, authorized machines and all their Projects**, without a machine/Project
picker or a separate opening-consent screen. Installing a package reviews that
fixed access scope. No scope preference, grant or Session data is persisted.
Every opening obtains fresh authorized readings and creates a new iframe, port,
stream and refs. No Session snapshot is retained across closing. Registry or
source/access changes revoke the active frame; reopening reads the current set.
Installing on Cloud does not install into a local daemon console or another browser profile.

The person reviews the package ID, version and capabilities before activation.
Every new or replacement package must also include its own valid cover image.
The same image appears in package review, the installed library and the launcher.
There is no generic artwork substitute for a newly published view.
Same ID with different bytes requires explicit replacement. IDs and SHA-256
hashes are identities, not verified publisher signatures. The registry atomically
checks the expected active digest and retains one previous archive for rollback.
A failed import leaves the active package intact. Registry changes, including
cross-tab hints, revoke running grants before rereading the saved archives.

## Supplied Session views

Explore views offers Galaxy, Orrery, Night City, Departures, Territory, World of Codecraft, Kaiju City and Lantern Deep. Each
install button opens the standard identity and permission review before saving
that package in this browser. Open installed views from the bottom status bar;
multiple enabled packages appear in its picker. A phone must install packages in
its own browser or PWA, even when another browser already has them.

Orrery puts attention states on rings, Night City groups Sessions into Project
buildings, and Departures keeps waiting-for-you rows first with full Unicode
names. Territory is a Project/Session map and explicitly says that file activity
is unavailable. Lantern Deep is an original hand-drawn cavern whose wanderers fight,
drill, patrol, climb or stand puzzled according to each Session's state; its enemies and
strikes are decoration and measure nothing. Territory's separate synthetic file renderer is not in the published ZIP;
publication does not add file access, transcript access or a new runtime capability.
The packages follow host language and keep their text-state and readable-list
alternatives. User artwork acceptance and public Release acceptance remain separate.

## Withdrawn built-in views

Galaxy and four new Session views are supplied. List, Pop and Deadpan remain removed from
Explore views, source assets and public ZIP downloads. The hosted edge returns
HTTP 410 with `Cache-Control: no-store` for their exact old download URLs, before
reading the static asset binding, so cached archives cannot remain downloadable
on those routes. Galaxy stays a static download. Their three exact built-in
IDs are reserved and rejected on import, including legacy archives. On the next
registry read in the updated interface, their saved active and rollback archives
are deleted in the same transaction before any launchable row is returned.
Galaxy and unrelated community package IDs are preserved. Already open older
host pages must reload to use this policy; no remote phone storage operation is claimed.

## Make a package

The first profile contains exactly **two files in a ZIP STORE archive**:
`manifest.json` and the declared HTML entry. All JavaScript, CSS, fonts and artwork
must be inline in that HTML (data/blob media are permitted). Deflate, encryption,
data descriptors, ZIP64, duplicate names, traversal, hidden segments, symlinks,
invalid UTF-8 and corrupt CRCs are refused. Separate resource files and remote
CDNs are not supported by this profile. The schema is
[view-plugin-v1.schema.json](view-plugin-v1.schema.json).

```json
{
  "manifestVersion": 1,
  "id": "org.example.session-map",
  "version": "1.0.0",
  "hostApi": 1,
  "name": { "en": "Session Map", "zh-Hant": "Session 地圖" },
  "entry": "index.html",
  "capabilities": ["sessions.read", "sessions.open"],
  "cover": "data:image/png;base64,<your dedicated square RGBA8 PNG>"
}
```

Vendor `plugins/sdk/view-plugin-v1.js` inline, or put `<!-- CLAWDLINE_SDK -->` in
the HTML and let the repository packer insert that SDK:

```sh
python3 tools/pack-view-plugin.py plugins/session-galaxy /tmp/session-galaxy.zip
```

The packer uses Python's standard library and deterministic ZIP timestamps. It
installs nothing and makes no network requests. The browser performs authoritative
validation before installation. The example ZIPs under
`web/console/public/view-plugins/` are static downloads, never compiled React views.
Import a replacement ZIP through the manager to test it without rebuilding the host.

Only curated examples have install buttons in **Explore views**. Independent
author candidates can be packed and imported without joining that list; checks
validate every author ZIP and cover separately from curated-list completeness.
The supplied examples have install buttons in **Explore views**. These fetch
their static ZIP and enter the same validation and package review as file import.
Curated install buttons fetch their ZIP with `cache: "no-store"`, so a previously cached
fixed download URL cannot silently install an older published revision. The standard package
review still names the exact downloaded identity and version before registry writes.
When an installed curated package is older than the supplied stable version,
Explore views offers **Review update** instead of disabling its install button.
The downloaded package still goes through the identity, version and capability
review, and replacement uses the current digest check. Matching or newer installed
versions are not offered a downgrade; phone storage is never replaced remotely.


Each supplied view has an install button that needs no native file chooser.
The packages remain separate archives using the existing v1 runtime; community ZIP import stays independent
of this curated example list.

### Required cover

Replace the illustrative `cover` placeholder with the actual canonical PNG data
URL. A cover must be a non-interlaced, 8-bit RGBA PNG, square and at most 256 pixels
on a side. The complete manifest, including base64 data, still has the existing
8,192-byte limit. A 128 × 128 image is practical for the current launcher. Remote
URLs, SVG data, animation, ancillary metadata, invalid CRCs, invalid deflate data, oversized inflated
scanlines and invalid filter bytes are refused. Image validity is checked from
bytes, not the filename, MIME label or header alone.

The public packer refuses a missing or invalid cover before touching its output.
The installer also rejects it before writing browser storage. Repository checks
parse every supplied ZIP and verify its authored cover and curated listing; a
missing cover cannot pass the publication gate. An actual cover must identify
the view and be reviewed as artwork: a technically valid blank or borrowed image
is not publication acceptance.

Each supplied cover has editable `cover.svg` source beside its manifest. They
can be rendered using an already installed local Playwright module, without a
network request or a new dependency:

```sh
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node tools/render-view-plugin-covers.mjs plugins/session-galaxy
```

The SVG renderer embeds the resulting PNG in each manifest; the SVG source is not a
third ZIP entry. The archive remains the same two-file STORE profile and SDK wire
v1. A host supporting revision 6 is needed to import the added manifest field;
refresh an older open page before installing a new package.

The registry has a read-only compatibility path for already stored pre-cover
archives. It never allows a coverless new install or replacement. The exact
previously shipped Galaxy archive digest receives its authored cover
without replacing executable bytes or requiring reinstall. Unknown older packages
remain usable with an explicit **Cover required before publication** warning;
they receive no generic cover and must be updated before being published or
imported again. Existing rollback records remain readable by the same path.

## Full screen and embedded

A view runs in one of two presentations, reported as `context.presentation`. The
same package, the same capabilities and the same wire protocol serve both; only the
layout should change. Design both before submitting a view.

| | `fullscreen` | `embedded` |
|---|---|---|
| Where | Fills the app, opened from the status bar | The desktop Session pane, beside the host's Session list |
| Size | The whole window, phone or desktop | Roughly 540–1500 CSS pixels wide and 500–1200 tall; never narrower than 540 |
| Host chrome | A return bar below the frame | A header above the frame: view name, switcher, **Full screen**, **Remove** |
| Roster | The view must offer a readable list of every Session | The host's Session list beside it is already the complete readable roster |
| `sessions.open` | Opens the Session and closes the view | Opens the Session in the pane; the view stays mounted behind it with `visible: false` and is shown again, same instance, when the person returns |
| `view.close` | Closes the view | Does nothing: only the host's **Remove** unpins. Escape inside the frame therefore cannot remove a pinned view |

The host owns which view is pinned (a per-browser preference), the header, switching,
full screen and removal. A package never pins or unpins itself and never learns which
other views are installed.

**An embedded layout should:**

1. Give the scene or board the whole frame. Drop the package's own title bar, Session
   roster, search, footnotes, credits and legends that the host header or Session list
   already provide. Keep licenses and credits reachable in the full-screen layout.
2. Hide its own close control; the host's **Remove** replaces it.
3. Keep every Session reachable from inside the scene and keep `sessions.open` one
   explicit click away (a selected item's label or a compact selection bar).
4. Follow the frame size, not a fixed canvas. Re-layout on `resize`: the person can
   resize the window and the pane width changes with it.
5. Stop motion while `visible` is false, as for a hidden tab: the frame stays loaded
   behind an open Session.
6. Never depend on `presentation` for data or permissions. It is a layout hint only.

**Implementation pattern.** Toggle one body class from context and keep the rules in
CSS, so a package's own re-render cannot drop the class:

```js
view.onContext(context => {
  document.body.classList.toggle("embedded", context.presentation === "embedded");
  render(context);
});
```

```css
body.embedded header, body.embedded aside, body.embedded footer { display: none; }
body.embedded .scene { height: 100dvh; }
```

If the package assigns `document.body.className` while rendering, include
`embedded` in that assignment instead (the supplied Codecraft and Kaiju do this).

**Compatibility.** A package written before revision 11 still works embedded: it is
shown at the pane's size with its full-screen layout, and its close control does
nothing there. A new package on an older host never receives `presentation` and
keeps its full-screen layout. No manifest field declares embedded support.

**Trying it.** Import the ZIP from Sidebar → View plugins on a desktop browser at least
900 pixels wide, open the status-bar launcher, and choose **Pin to the right**. Check
both presentations in light and dark themes, with reduced motion, with zero Sessions
and with more Sessions than fit, then open a Session from the embedded view and
return. The supplied Session Departures and World of Codecraft are reference
implementations of a board and a scene; `plugins/embedded-pane.acceptance.mjs`
drives both presentations through the production host with synthetic Sessions.

## Consent and data ownership

Every view uses the same fleet source: all currently authorized machines and all
Projects. The host aggregates the existing `SessionProjectionSource` readings;
local uses the existing fleet list. There is no machine or Project selector and
no remembered consent recipe. A Cloud read never falls back to phone localhost.
Account, device, client generation, pairing or access changes invalidate the
source. A machine without confirmed operation permission is not included.

The host namespaces Session and Project identities by source, so equal local IDs
on different machines cannot overwrite one another or route navigation to the
wrong machine. These identities remain host-private. Individual machine failures
leave the other readable machines available, with an explicit host warning about
incomplete or stale data. An offline row stays stale and cannot navigate; online
rows still use their own source's fresh authorization. No successful machine is
silently truncated to satisfy the combined existing Session/message limits.
Permission or identity failures reject the reading rather than exposing a partial
authorization result.

`sessions.read` grants only this projection:

```json
{
  "ref": "opaque-instance-reference",
  "title": "Review changes",
  "project": { "ref": "opaque-project-reference", "label": "Example" },
  "workState": "working",
  "freshness": "fresh"
}
```

`waitKind` is optional additive metadata. The only current value is `callback`,
projected from a live broker `waiting_for` target with kind `callback` or `heavy`.
It carries no task ID, title or command. Missing or unrecognized kinds are omitted.
It does not replace `workState`: a working root with background work remains
working. Orrery uses a distinct waiting orbit only when the Session is not working,
awaiting the person or reporting completion. Stale readings retain their freshness
restrictions and cannot animate or navigate.

`focused` is an optional boolean identifying the host's currently open Session.
Local and Cloud hosts compare their own execution-scoped identities before projecting
this flag; no raw identity crosses the bridge. Older hosts omit it. Views may use a
changed focus to select and frame the corresponding character. Local exploration
must not navigate or acknowledge work. The host still closes the view on explicit
`sessions.open`; an embedded terminal remains outside this contract.

`stateTime` is optional additive metadata: `{ "at": 1700000000000, "basis": "declaration" }`.
`at` is Unix milliseconds. `declaration` means the current self-declared state's
recorded time; `turn` means the supplied working turn began, and is only exposed
beside `working`. Neither is an arrival time or proof of progress. Missing,
non-finite or invalid dates are omitted. Local and Cloud presentations use the
same projection; no raw identities are added. A view may record first observation
within its opening, but must label that fallback and must not infer a historical
state transition. Older hosts omit this optional field.

`project` may be null. States are `ready`, `working`, `holding`, `waiting_you`,
`waiting_session`, `unknown`, `milestone_complete`, `work_complete`. Freshness is
`fresh`, `stale`, `unknown`. The host generates new opaque references per opening;
it does not expose raw Session IDs, machine IDs, filesystem project keys,
transcripts, prompts, tokens, native bridges or a generic API client. Titles may
contain personal information and are explicitly part of the reviewed package access.
Treat every title and label as text, never HTML. Do not infer progress percentages,
dependency edges or accepted completion from these states.

`sessions.open` is optional and permits navigation only. It cannot send messages,
run commands, create Sessions or dispatch work. The host revalidates the exact
Session execution before accepting navigation, closes its modal/history step and
then calls the existing navigation owner. A removed Session or old ref is refused.
There are no automatic retries. A timeout has an uncertain outcome.

## SDK and wire protocol

```js
const view = await connectViewPlugin();
view.onContext(context => updatePresentation(context));
const initial = await view.sessions.subscribe(snapshot => render(snapshot));
render(initial);
// In an explicit click handler:
await view.sessions.open(selected.ref);
// Or close without navigation:
await view.close();
```

`subscribe` returns the initial complete snapshot. Its callback receives only
later complete snapshots. There is one logical subscription per instance;
repeating subscribe returns the current snapshot. A snapshot is
`{streamId, revision, sessions}`. Revision increases within a stream; discard equal
or older snapshots and replace the complete list. Pending updates coalesce to
the newest list. Reopening creates a new stream and grants no history/replay.

`onContext(fn)` synchronously calls `fn` with the latest context and returns an
unsubscribe function. Context is `{language, theme, reducedMotion, visible,
scopeLabel, connection, presentation}`, with theme `dark|light`, connection
`connected|disconnected` and presentation `fullscreen|embedded`. Stop motion when
invisible or reduced motion is on.

`presentation` is `embedded` when the view is pinned beside the Session list and
`fullscreen` otherwise. Hosts before revision 11 omit it; treat a missing value as
`fullscreen`. What changes when embedded is specified in
[Full screen and embedded](#full-screen-and-embedded).
Retained disconnected data is stale immediately; reconnect needs a new snapshot.
For a fleet, `connection` is connected when at least one machine is connected;
per-row freshness still reflects its own source. The host separately warns when
other machines are unavailable, without treating the remaining rows as a complete fleet.
Empty lists are valid. Every visual needs a readable list/fallback and text states.
The supplied Galaxy intentionally retains its dark astronomical palette and honors language and reduced motion.

The SDK registers its window listener before sending this bootstrap hello:

```js
{type:"clawdline.plugin.hello", protocolVersion:1}
```

The host checks `event.source === activeIframe.contentWindow`, once, then transfers
one MessagePort with `{type:"clawdline.plugin.connect", protocolVersion:1,
instanceId, context, limits:{pending,requestMilliseconds}}`. Opaque frames require
bootstrap target origin `*`; no Session snapshot or bearer credential is in that
message. The SDK accepts only its parent and calls `port.start()`. All later
requests, replies and notifications use that port:

```js
{v:1, instanceId:"i", id:"r1", method:"sessions.subscribe", params:{}}
{v:1, instanceId:"i", id:"r1", result:{streamId:"s", revision:1, sessions:[]}}
{v:1, instanceId:"i", id:"r2", method:"sessions.open", params:{ref:"opaque"}}
{v:1, instanceId:"i", id:"r2", result:{opened:true}}
{v:1, instanceId:"i", id:"r3", method:"view.close", params:{}}
{v:1, instanceId:"i", id:"r3", result:{closed:true}}
{v:1, instanceId:"i", id:"r4", error:{code:"permission_denied", message:"..."}}
{v:1, instanceId:"i", event:"sessions.snapshot", data:{streamId:"s",revision:2,sessions:[]}}
{v:1, instanceId:"i", event:"host.context", data:{/* context */}}
```

`opened:true` acknowledges a validated navigation accepted by the host, before
closing the frame; it does not prove content loaded or the person observed it.
`closed:true` similarly acknowledges closing. The subscribe reply precedes its
subsequent snapshot events on the same port. Unknown methods/extra request fields
are refused. A different instance is ignored; old ports are closed on disposal.
`dispose()` closes SDK listeners/port and rejects pending calls. Package code
should handle Escape (close its own detail panel first, then call `view.close()`).
The plugin owns its visible title and header controls. The host does not add a
second visible title/scope bar above the package. A compact **Back to Clawdline**
control stays below the iframe, outside package control, and works even if package
close logic fails. Disconnection warnings and retry also stay in that host footer.
This presentation change leaves existing ZIPs and the v1 wire protocol unchanged.

Errors include `invalid_request`, `method_not_supported`, `permission_denied`,
`scope_changed`, `session_unavailable`, `host_unavailable`, `busy`,
`protocol_mismatch`, `too_many_sessions`, `message_too_large`; SDK deadlines add
`request_timeout`. Compare codes, not message prose.

## Isolation and limits

Package HTML runs only in an opaque `srcdoc` iframe with `sandbox="allow-scripts"`.
The host never serves extracted author HTML on its authenticated origin. Public
ZIP URLs are archive downloads. It grants no same-origin, forms, popups, top-level
navigation, downloads, camera, microphone or geolocation. Host-inserted CSP blocks
fetch/WebSocket, workers, external scripts/assets, eval, nested frames and objects;
inline scripts/styles and data/blob media remain available. No API credentials
cross the bridge.

**Install trusted authors.** A browser sandbox is not complete hostile-process,
CPU/GPU or exfiltration isolation. In particular, a frame may navigate itself to
another origin and disclose text it was explicitly allowed to read. This MVP is
an experimental scoped extension surface, not a claim that arbitrary malicious
third-party code is safe. Package code can also consume resources; Close is not
a guarantee against a hung shared renderer process.

Concrete bounds are registered in `internal/domain/capacity/register_view_plugins.go`
and matched against the browser and SDK by the parity guard:

| Resource | Limit | At limit |
| --- | ---: | --- |
| ZIP bytes | 33554432 | Refuse import |
| Expanded bytes | 33554432 | Refuse import |
| Manifest bytes | 8192 | Refuse import |
| Cover side | 256 pixels, square | Refuse import or packing |
| Files | Exactly two | Refuse import |
| Installed packages | 8, each active + one previous | Remove one first |
| Snapshot bytes | 1048576 | Stop view, no partial list |
| Sessions per snapshot | 256 | Refuse the combined list; no silently omitted rows |
| Pending requests | 16 | SDK refuses; host stops flooding frame |
| Handshake/request/read deadline | 10000 ms | Refuse/unavailable/uncertain timeout |
| Requests per frame per second | 32 | Stop flooding frame |
| Names, versions, paths and request/display strings | 256 Unicode characters | Refuse, no silent truncation |

The daemon reports these browser-local capacities as unmeasured, not zero.
The larger archive profile admits original raster-art scenes without external
resources or ZIP decompression. It applies to every package, including arbitrary
author packages. With eight active packages and one rollback per package, archive
payload is bounded at 512 MiB before browser bookkeeping; browser quota failures
remain typed storage errors. Older hosts reject these larger archives. Manifest,
two-file, CRC, cover, scope, CSP and bridge checks remain enforced.
No global UI badges, remote update feed, publisher signing, cross-device package
sync, plugin database or native capabilities are included.

## Verification and release boundary

`runtime.test.mjs` exercises actual ZIP parsing, scoped references, capability
refusal, event ordering/coalescing, disconnection, flood isolation and Cloud
revocation during a delayed read. `tools/view-plugins.e2e.mjs` bundles the production
manager/frame/registry and supplied packages with synthetic Session sources. It
exercises import, Galaxy plus a synthetic community view, multiple machines/Projects, Back/focus, hostile label
text, revocation, persisted packages without persisted grants, direct opening
including after reload, cross-tab disabling, replacement,
rollback, failed import, disable, remove, rejection of withdrawn IDs and removal of saved withdrawn archives. It accepts an existing Playwright
module path; it installs no browser dependencies.

These tests establish the isolated fixture behavior. Local daemon-console startup,
production Cloud startup/current authorization, unchanged package digests, and any
native wrapper must be verified at their actual destination before claiming that
destination is ready. A successful source check or bundle is not deployment proof.
No public Release acceptance is implied by an experimental Cloud/dev delivery.

### Installing curated packages in the local console

Curated downloads use same-origin credentials and `cache: no-store`. The daemon
protects `/view-plugins/` behind its existing sign-in gate; omitting its HttpOnly
session cookie produces HTTP 401 even while the person is signed in to the App.
Cloud hosts the same ZIPs publicly. Keep the daemon gate unchanged, send the
existing cookie only to its own origin, and never forward it to a different
origin, including another local port. Network or HTTP download failures have
their own error and must not be presented as malformed ZIP packages.

`plugins/curated-install.acceptance.mjs` exercises the production host in
Chromium and WebKit, in English and Taiwan Traditional Chinese. It installs the
supplied archives behind a cookie gate and from a public origin, verifies
reload persistence, refuses a download without modifying the registry, and
checks a cross-origin redirect does not carry the native cookie. It also installs
both recommendations from the launcher at desktop and mobile widths, checks
focus on review and cancellation, and opens the installed views after a reload. Run with
`PLUGIN_ARTIFACTS` and an existing `PLAYWRIGHT_MODULE`. The former credential-omitting
host fails the native installation case before reaching package review.

### Keeping installation attention stable

Download progress stays in the host header, outside the scrolling content. Busy
controls keep their appearance instead of dimming the whole gallery. The manager
reserves its scrollbar gutter, preserves the originating card before inserting
package review, and smoothly scrolls only its own content to that review. Focus
moves with `preventScroll`; reduced-motion users get immediate positioning.

Confirmation keeps the reviewed card mounted while registry notifications revoke
stale grants and refresh installed packages. Only a successful write and registry
read replace its actions with Open and Installed views. The result remains in the
same location; Installed views smoothly reveals and focuses the matching card.
Cancel returns to the original gallery action. A download error is focused and
brought into view without changing installed packages.

`plugins/install-flow.acceptance.mjs` uses the production host and delayed local
downloads in Chromium/WebKit, English/Traditional Chinese, phone/desktop widths,
and both motion preferences. It measures download and completion positions,
observes intermediate scroll positions, and exercises cancellation, error
attention and navigation to the installed card. These are isolated browser
receipts; they do not establish deployment or native App acceptance.

## Official updates

Each installed card has its own Check for updates action. The manager also has
Check for updates for the complete installed set. The host reads
`/view-plugins/catalog.json` at startup, on return to the foreground, after a view
closes and when asked. Version discovery is independent of the currently loaded
host bundle; both the catalog and ZIP downloads bypass HTTP cache and reject
redirects. These reads contain no Session or Project data. A failed check leaves
installed views usable. There is no timer polling or automatic download retry loop.

`python3 tools/build-view-plugin-catalog.py` generates the index from the exact
shipped ZIPs and author manifests and records their SHA-256, bytes, API and
capabilities. It also retains exact official archive digests in
`plugins/official-package-revisions.json` so older installations can migrate without
removal. The initial history came from main through `ef40d5aa515c43669124f4fcf7d08a58e75d0401`.
Future generation retains each current revision. `--check` runs in the console
tests; a package/manifest/catalog mismatch fails publication. The catalog uses
registered byte and entry limits; downloads use the registered archive bound and
update deadline. It is same-origin release metadata, not a claim of publisher
signatures or general trust in a matching package ID.

Only exact known official bytes are adopted automatically. Imported community
packages with other digests keep their existing installation and can be updated by
importing another ZIP. Official cards expose a persistent automatic-update checkbox.
Same-permission newer stable releases can update silently; additional capabilities
or a different host API require review or a newer host. Identity, version, full
archive hash, byte length and capabilities must all match the catalog before any
registry write. Older pages ignore catalog entries for views they do not know.

A shared Web Lock spans each running view. Automatic replacement needs the exclusive
lock, so views in other tabs remain intact; opening a view re-reads the installed
revision after acquiring its lease. Hosts without Web Locks retain manual update.
The registry compares the expected active digest and rechecks automatic-update
consent in its atomic transaction. Enable/disable is preserved, and the prior
archive remains available. Rolling back disables automatic updates until the person
enables them again, preventing an immediate upgrade over their chosen rollback.
This policy is browser/device-local and does not update another phone's registry.
