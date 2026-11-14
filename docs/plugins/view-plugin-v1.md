# View Plugin v1

This contract is for community authors building interactive Session visualizations inside Clawdline. Status: **implementation specification, revision 1; not an available product capability**. A host and its conformance evidence must ship before third-party installation is advertised. The galaxy prototype is the first consumer, not a special host feature.

## Product guarantee and boundary

The host requires a one-time Clawdline update. After that, a compatible plugin is installed or replaced as a package at runtime: no application reinstall, daemon restart, core source edit, frontend rebuild or App Store release for that plugin. Opening a package must never run an installer or execute native code.

Version 1 contributes one full-app view and a host-rendered bottom-status-bar entry. It does not replace Sessions, register arbitrary routes, inject React components, add global CSS, execute commands, dispatch tasks or expose arbitrary REST calls. The host owns navigation, focus, browser history, authorization and Cloud transport. The plugin owns art, layout, rendering and local presentation state.

## Package and installation

A ZIP contains `manifest.json`, an HTML entry and optional bundled JavaScript, CSS, fonts and images. It needs no build tools on the person's machine. The manifest schema is [view-plugin-v1.schema.json](view-plugin-v1.schema.json).

```json
{
  "manifestVersion": 1,
  "id": "org.example.session-galaxy",
  "version": "1.0.0",
  "hostApi": 1,
  "name": { "en": "Session Galaxy", "zh-Hant": "Session 星系" },
  "entry": "index.html",
  "capabilities": ["sessions.read", "sessions.open"]
}
```

Package IDs identify an installation, not a verified publisher. Entry and asset paths are normalized relative package paths: no absolute paths, parent traversal, hidden segments, backslashes, symlinks or network URLs. The installer rejects duplicate normalized names, invalid UTF-8, an absent entry and an unsupported manifest/API version before any package script executes. Hashing proves package byte identity, not publisher trust.

The person chooses Install, selects the ZIP, reviews its identity/version/capabilities and chooses the machine/Project scope. The host validates into staging, then atomically activates an immutable content-addressed revision. The registry is data, separate from the compiled page registry. A failed validation or activation leaves the previous revision active. Same ID with different bytes is replacement, not a second hidden installation; require explicit replacement confirmation. Never silently downgrade or change scopes. There are no automatic remote updates in v1.

Replacement closes the old frame/port and creates a new instance. Existing grants may be reused only with unchanged capabilities and scope. Added capabilities require new consent. Disabling or uninstalling removes the entry, stops its frame and closes ports; unrelated work and Session data remain intact. The host retains the previous approved revision for explicit rollback. Plugin persistent storage is outside v1.

## Host lifecycle

The host owns the status-bar entry, plugin selector and its badges; plugins cannot invent global attention counts. Activating the entry creates or reveals a full-app view. Close, desktop Escape and phone Back restore the originating surface, scroll and focus. Session navigation is mediated by the host.

The plugin receives visibility, theme, language and reduced-motion context. It must stop animation loops and rendering when invisible and support reduced motion. The host may destroy a hidden frame; reopening must work from a new snapshot. No animation may be required to learn a work title or state. WebGL/canvas effects must have a readable fallback when unsupported or context is lost.

## Isolated execution and resources

Untrusted package HTML is never rendered directly on the application's authenticated origin, including when its resource URL is opened as a top-level document. The host must use a dedicated non-privileged resource delivery boundary plus an iframe with only `sandbox="allow-scripts"`; it must not grant same-origin, forms, popups, top navigation, downloads, camera, microphone or geolocation. A sandbox is not a CPU, GPU, browser-process or complete exfiltration boundary; hostile-code isolation requires browser acceptance beyond this document.

All dependencies and creative assets are bundled. The host supplies a package-scoped asset base usable by both native/local and Cloud clients. Relative package resources must work in the supported host without plugin-specific rewriting by authors. The resource layer must not disclose a daemon token or serve other packages/private filesystem paths. Remote CDN scripts, analytics, fetch, WebSocket, workers and native bridges are not v1 capabilities. CSP restricts scripts, styles, fonts, images and media to package resources (plus explicitly needed inline/data/blob resources), with `connect-src 'none'`, `object-src 'none'`, `frame-src 'none'`, `base-uri 'none'` and `form-action 'none'`; no eval permission. CSP is an additional restriction, not proof that all network exfiltration is impossible. Third-party hostile code must not be advertised as safe until its isolation tests pass.

Cloud must transfer installed package resources over its authorized machine transport, identify the selected machine and cache by package digest. A Cloud view must not request localhost on the phone or silently fall back to a different machine. Installation is scoped to the selected machine: installing on one does not install on all. Capability/reachability failures are visible and do not produce a fake live scene. v1 promises no offline Session data.

## Connection protocol

The window message channel is bootstrap only. The plugin sends `{type:"clawdline.plugin.hello", protocolVersion:1}` to its parent after registering its message listener. The host accepts a hello only from the expected active `iframe.contentWindow`; an origin of `null` alone is never trusted. Exactly one dedicated MessagePort is transferred to that frame with:

```js
{ type: "clawdline.plugin.connect", protocolVersion: 1,
  instanceId: "host-generated-opaque-instance", context: {
    language: "zh-Hant", theme: "dark", reducedMotion: false,
    visible: true, scopeLabel: "Selected project", connection: "connected"
  } }
```

The plugin accepts this only from its parent, once, with exactly one port. Bootstrap uses `targetOrigin: "*"` only because the sandbox has an opaque origin; it carries no bearer credentials or Session snapshot. Both peers call `port.start()`. All subsequent data/requests use this port, never broadcast window messages. A new frame or authorization/scope change gets a new instance and port. Close old ports, reject pending requests and destroy the old frame before exposing another scope. `instanceId` is correlation, not authorization.

Requests, replies and events are structured-clone-compatible plain JSON, without executable functions, DOM objects or binary payloads. The host checks the current grant, active instance, scope and payload on every request. Request IDs are unique strings per instance. Only these methods exist:

| Method | Required grant | Parameters | Result |
| --- | --- | --- | --- |
| `sessions.subscribe` | `sessions.read` | `{}` | Initial complete snapshot; at most one logical subscription per instance |
| `sessions.open` | `sessions.open` | `{ref: string}` | `{opened:true}` only after host navigation succeeds |
| `view.close` | none | `{}` | `{closed:true}` accepted; host then closes the frame |

`sessions.open` is navigation, not message sending or process execution. A ref from an old instance, different scope or absent current Session is refused. The host resolves refs to current machine/Session identity; a plugin cannot supply raw terminal IDs or override ownership. The host may close the plugin view to show the Session. A successful close reply confirms acceptance, not proof a person observed the resulting surface.

```js
// Plugin -> host
{ v:1, instanceId:"i1", id:"r1", method:"sessions.subscribe", params:{} }
// Host -> plugin, exactly one of result/error
{ v:1, instanceId:"i1", id:"r1", result:{streamId:"s1", revision:1, sessions:[]} }
{ v:1, instanceId:"i1", id:"r2", error:{code:"permission_denied", message:"..."} }
// Host -> plugin notifications
{ v:1, instanceId:"i1", event:"sessions.snapshot", data:{streamId:"s1", revision:2, sessions:[]} }
{ v:1, instanceId:"i1", event:"host.context", data:{language:"zh-Hant", theme:"dark", reducedMotion:false, visible:false, scopeLabel:"Selected project", connection:"disconnected"} }
```

The subscribe reply is the initial snapshot. Notifications for that subscription are delivered after that reply on the same port. Repeating subscribe returns the current snapshot of the same logical subscription. Each snapshot is a complete replacement, not a patch. Revision is a monotonically increasing JSON-safe integer within `streamId`; ignore equal/older revisions. The host coalesces pending snapshots to the newest complete one. A new stream is initialized through a new instance; never merge streams. After a disconnect, render retained rows as stale immediately, even if their last row freshness was fresh. Connection restoration requires a new authoritative snapshot before displaying live status. There is no replay/history promise.

## Session projection

```json
{
  "ref": "opaque-instance-scoped-reference",
  "title": "Review changes",
  "project": { "ref": "opaque-project-reference", "label": "Example project" },
  "workState": "working",
  "freshness": "fresh"
}
```

`project` may be null. `workState` is one of `ready`, `working`, `holding`, `waiting_you`, `waiting_session`, `unknown`, `milestone_complete`, `work_complete`. `freshness` is `fresh`, `stale` or `unknown`. Empty snapshots mean no visible Sessions, not an error. Refs are opaque and meaningful only in the current instance; they are not persistent storage keys. This projection excludes transcripts, prompts, filesystem paths, process IDs, credentials and raw provider logs. Titles and labels are untrusted text and must not become HTML.

Plugins may group and animate these facts; they may not infer precise progress, task dependency edges or completed acceptance from `working`/`work_complete`. Unknown state and stale data remain explicit. Opening a Session revalidates host identity; it is not authorized solely by a stale visual.

Errors use codes `invalid_request`, `method_not_supported`, `permission_denied`, `scope_changed`, `session_unavailable`, `host_unavailable`, `busy`, `protocol_mismatch`. A refusal is not execution. Error messages are explanatory text, not parsable routing instructions. No automatic retry of navigation. Port closure fails all pending calls locally as `host_unavailable`; explicit reopen establishes a new instance. Request timeout is an uncertain outcome, not proof navigation did not happen.

## Author-facing SDK contract

An SDK may hide the port, but must preserve these wire semantics. The author's only required concepts are:

```js
const view = await connectViewPlugin();
const initial = await view.sessions.subscribe(snapshot => render(snapshot));
render(initial);
view.onContext(context => updatePresentation(context));
await view.sessions.open(selected.ref); // a real, explicit click
await view.close();
```

The snapshot callback receives subsequent snapshots, never the initial reply; this avoids a hidden double-initial-render contract. `onContext(handler)` synchronously calls the handler with the latest bootstrap/context value when registered, then delivers subsequent context updates, and returns an unsubscribe function. `dispose()` closes listeners/port and fails pending requests. A demo/mock adapter has the same methods, data shape and disposal behavior, is clearly labelled, and is supplied outside the scene. Scene code must not reach into the host React store or use a homemade authenticated fetch fallback. This specification does not claim an SDK implementation is already shipped.

## Compatibility, bounds and readiness gates

`manifestVersion:1` and `hostApi:1` are exact major versions; package `version` is independent SemVer. Missing required capabilities or unsupported major versions refuse activation with the previous version intact. Additive negotiated features can be introduced without making old packages depend on them; changed field meaning requires a new major. Display a compatibility reason before activation.

Installation bytes, expanded bytes, file counts, message size, projected Session count, in-flight requests and update frequency all need named limits in `internal/domain/capacity` before host implementation ships. No value is invented or enforced by this design document. A host cannot pass conformance without published concrete limits and tests for their refusals; authors cannot be promised unlimited scenes or messages. Capacity changes require documented compatible degradation, never silent partial snapshots. If a scope cannot fit the supported snapshot bound, refuse with an explicit scope-narrowing action rather than omit rows invisibly.

Public release acceptance requires evidence for all of the following:

1. Install two independently built packages, switch between them, replace one and roll it back while core source, built console, daemon version and unrelated Sessions remain unchanged. No app reinstall, build or daemon restart occurs during these operations.
2. Reject malformed manifests, unsupported versions, missing entry, traversal, symlinks, duplicate files, oversized packages/messages and capability escalation; previous package/grants remain unchanged.
3. Test actual opaque-frame bootstrap, forged messages, stale ports/refs, revocation/scope switches, hostile label text and forbidden resource access, including a direct top-level resource visit. No generic daemon authority or private origin leaks. Record remaining isolation limitations.
4. Test initial/event ordering, coalescing, disconnect/stale, new streams, empty scopes, refusal and navigation with current identity. No duplicate operation on timeout/retry.
5. Check status entry/full view/close/back/focus on desktop and phone, hidden-render suspension, reduced motion, WebGL loss and plugin failure recovery without breaking the host. The host's close button works independently of plugin code; this alone does not establish process isolation.
6. Run the same unchanged plugin digest on local/native and production Cloud, with actual resource loading and authorization tests on both. A local demo does not prove Cloud compatibility.

Until these pass, describe the package as a prototype against this contract, not an installable community plugin system.

## Browser references

The [HTML iframe standard](https://html.spec.whatwg.org/multipage/iframe-embed-object.html) defines sandbox flags and opaque origin behavior. The [HTML messaging standard](https://html.spec.whatwg.org/multipage/web-messaging.html) defines window messaging and transferred ports. These primitives inform the proposed boundary; they do not verify Clawdline's unimplemented host.
