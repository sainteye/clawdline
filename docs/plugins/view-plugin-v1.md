# View Plugin v1

Revision 3 describes the **experimental browser-local runtime** implemented in
`web/console/src/view-plugins`. The first two packages are Session Galaxy and
Session List. Public Release acceptance is separate from source and Cloud delivery.
The earlier revision proposed machine-owned installation; that transport is deferred.

## What the host owns

The sidebar’s **View plugins** entry owns installation and management. The bottom
status-bar orbit button opens installed, enabled views only: one view opens
directly, while multiple views use a compact picker. An empty launcher points to
the sidebar without embedding installation controls. When the status bar is
absent, the orbit button sits in the Session-list pane. A granted view fills the app.
The host owns installation, consent, current machine identity, history, focus,
close and Session navigation. The plugin owns its own HTML, CSS, canvas and motion.
The Session page, classifier and dispatch broker are not plugin extension points.

After the host update, importing, replacing, disabling, removing and rolling back
compatible packages needs no core edit, frontend rebuild, daemon restart or app
reinstall. Each browser/interface stores its own packages in IndexedDB. They are
not copied between phones, machines, accounts or browsers. Packages do not start
automatically. No grant or Session data is persisted by this registry. After a
person approves a scope, the mounted interface may remember only the exact
package digest, source identity and Project selection in memory for that visit.
The status button can reopen it without repeating consent. Every reopening still
obtains a fresh authorized reading and creates a new iframe, port, stream and refs.
No Session snapshot is retained across closing. Page reload, registry mutation,
source/access changes or a plugin fault clear the matching in-memory approval.
This memory is bounded by the installed package count and never written to storage. Installing
on Cloud does not install into a local daemon console or another browser profile.

The person reviews the package ID, version and capabilities before activation.
Same ID with different bytes requires explicit replacement. IDs and SHA-256
hashes are identities, not verified publisher signatures. The registry atomically
checks the expected active digest and retains one previous archive for rollback.
A failed import leaves the active package intact. Registry changes, including
cross-tab hints, revoke running grants before rereading the saved archives.

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
  "capabilities": ["sessions.read", "sessions.open"]
}
```

Vendor `plugins/sdk/view-plugin-v1.js` inline, or put `<!-- CLAWDLINE_SDK -->` in
the HTML and let the repository packer insert that SDK:

```sh
python3 tools/pack-view-plugin.py plugins/session-galaxy /tmp/session-galaxy.zip
python3 tools/pack-view-plugin.py plugins/session-list /tmp/session-list.zip
```

The packer uses Python's standard library and deterministic ZIP timestamps. It
installs nothing and makes no network requests. The browser performs authoritative
validation before installation. The example ZIPs under
`web/console/public/view-plugins/` are static downloads, never compiled React views.
Import a replacement ZIP through the manager to test it without rebuilding the host.

## Consent and data ownership

The first opening during an interface visit requires consent for a machine and
either all Projects or a specific Project. The consent panel explains its lifetime.
An unchanged package and scope may reuse that in-memory approval until invalidated;
changes require consent again. The host obtains a fresh authorized reading before
creating every frame. Cloud
uses its existing `SessionProjectionSource` and exact execution destinations;
local uses the existing fleet list. A Cloud read never falls back to phone
localhost. Account, device, client generation, pairing or access changes invalidate
the source. A source without confirmed operation permission is not offered.

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

`project` may be null. States are `ready`, `working`, `holding`, `waiting_you`,
`waiting_session`, `unknown`, `milestone_complete`, `work_complete`. Freshness is
`fresh`, `stale`, `unknown`. The host generates new opaque references per opening;
it does not expose raw Session IDs, machine IDs, filesystem project keys,
transcripts, prompts, tokens, native bridges or a generic API client. Titles may
contain personal information and are explicitly part of the person's grant.
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
scopeLabel, connection}`, with theme `dark|light` and connection
`connected|disconnected`. Stop motion when invisible or reduced motion is on.
Retained disconnected data is stale immediately; reconnect needs a new snapshot.
Empty lists are valid. Every visual needs a readable list/fallback and text states.
The supplied Galaxy intentionally retains its dark astronomical palette; List
honors both host themes. Both honor language and reduced motion.

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
The independent host Close button works even if package logic fails.

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
| ZIP bytes | 262144 | Refuse import |
| Expanded bytes | 524288 | Refuse import |
| Manifest bytes | 8192 | Refuse import |
| Files | Exactly two | Refuse import |
| Installed packages | 8, each active + one previous | Remove one first |
| Snapshot bytes | 1048576 | Stop view, narrow scope |
| Sessions per snapshot | 256 | Narrow scope, no silently omitted rows |
| Pending requests | 16 | SDK refuses; host stops flooding frame |
| Handshake/request/read deadline | 10000 ms | Refuse/unavailable/uncertain timeout |
| Requests per frame per second | 32 | Stop flooding frame |
| Manifest/request/display string | 256 Unicode characters | Refuse, no silent truncation |

The daemon reports these browser-local capacities as unmeasured, not zero.
No global UI badges, remote update feed, publisher signing, cross-device package
sync, plugin database or native capabilities are included.

## Verification and release boundary

`runtime.test.mjs` exercises actual ZIP parsing, scoped references, capability
refusal, event ordering/coalescing, disconnection, flood isolation and Cloud
revocation during a delayed read. `tools/view-plugins.e2e.mjs` bundles the production
manager/frame/registry and supplied packages with synthetic Session sources. It
exercises import, two distinct views, Project filtering, Back/focus, hostile label
text, revocation, persisted packages without persisted grants, direct reopening after visit-scoped
consent, a management-free picker, cross-tab disabling, replacement,
rollback, failed import, disable and remove. It accepts an existing Playwright
module path; it installs no browser dependencies.

These tests establish the isolated fixture behavior. Local daemon-console startup,
production Cloud startup/current authorization, unchanged package digests, and any
native wrapper must be verified at their actual destination before claiming that
destination is ready. A successful source check or bundle is not deployment proof.
No public Release acceptance is implied by an experimental Cloud/dev delivery.
