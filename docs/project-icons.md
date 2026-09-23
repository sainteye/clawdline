# Project icons across machines

A project icon is a colour grid (`accent`, `cells`), not a bitmap file or an application logo.
All console surfaces consume the daemon's resolved grid. The application mark and assistant
logos are separate assets, outside the project registry.

## Resolution and identity

`internal/domain/icon` resolves a mark in this order:

1. A copied mark in `CLAWDLINE_NEXT_DIR/project-icon-overrides.json`, using the longest containing
   path. A copied project mark also applies to sessions in its subdirectories.
2. The longest containing entry in `~/.claude/project-icons.json`. This compatibility input stays
   read-only. It supports palette/row artwork and the original hue/tone/shape creature generator.
3. The deterministic creature generated from the working directory's FNV-1a hash.

A local path is not a cross-machine project identity. Two checkouts can have different paths;
distinct projects can have the same folder name. Neither path hashes nor labels are safe
matching keys. This version asks the person to choose the destination explicitly and copies the
resolved grid, giving generated marks and custom artwork the same transfer semantics.

## Using the copy

In Projects, open **Copy project icon** (labelled `複製專案圖示`). Select the source project and copy
its icon. Switch to the receiving machine in the hosted console, return to Projects, choose the
destination, inspect the current and copied marks, and apply. Only the picture survives in this
browser tab's session storage; no source name or path is retained. Clear it when finished.
Closing the tab ends the transfer buffer.

This is a snapshot copy, not continuous synchronization. Later source changes do not propagate.
Both machines need not be online together: the source must answer when copying and the receiver
when applying. Both must already be paired with the viewer; the receiver must allow remote writes.
An older receiver that does not advertise `project-icon-copy` refuses by name and needs an update.
A source only needs the existing places read. Failures are shown, never reported as successful copies.

`PUT /v1/projects/{place-id}/icon` accepts `{ "icon": <grid>, "expected": <current-grid> }`.
It resolves the destination from this machine's places, checks device send permission, validates
the grid, compares the current mark, and saves atomically. A changed target answers `409
icon_changed`: reread and decide again. Retrying the same resulting mark succeeds. No
client-supplied path is opened or written.

One daemon owns the new registry, loading it at startup and updating under a mutex. A synced,
private temporary file and rename precede the in-memory update. Invalid/unreadable saved data
refuses startup instead of pretending the icons disappeared. Original entries and project names
are never modified. Changing a project's path requires another explicit copy.

## Cloud responsibilities

The sibling cloud service's relay (`relay/src/lib/routing.ts`, `relay/src/lib/envelope.ts`, and
AccountDO) routes authenticated envelope addresses/classes, not plaintext operation names.
The existing boundary is reused:

- The viewer reads a source grid through the encrypted `places` operation.
- The receiver gets `project-icon-copy` on the authenticated, encrypted machine command channel.
  The console resolves its Cloud place handle to the receiver's local place id and refuses a
  machine mismatch.
- `internal/app/cloudops` maps the command to the same local HTTP handler. Pairing, the write gate,
  device authorization and reply transport remain in force.
- The cloud API owns accounts/pairing; the relay transports ciphertext. Neither needs an icon
  database, image CDN, public image URLs, or a protocol change.

No cloud-service implementation change is required. This avoids a second authoritative icon
store and exposing artwork to the service. Continuous synchronization would first need explicit
cross-machine project bindings, revisions/conflicts, deletion behavior and offline rules.
Matching folder names alone must not create a binding.

## Bounds and verification

The capacity register covers 512 saved marks, 64 rows/columns per mark, and a 96 KiB copy request.
Colours are literal six-digit RGB hex strings or transparent cells; rows are rectangular. URLs,
SVG, arbitrary labels and paths are not part of a transferred mark. At capacity new marks are
refused; existing marks can still be replaced. Nothing is automatically evicted.

Tests cover persistence, descendant inheritance without sibling leakage, conflict refusal,
retries, grid validation, capacity refusal, corrupt storage, Cloud routing, destination identity,
write-gate refusal, and the HTTP body/permission boundary. The capacity guard was first run red
with all three new declarations unregistered, then made green by registering them.
