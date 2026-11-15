# File activity boundary for Session Territory

Status: **BLOCKED on a new reviewed data contract and permission boundary**. The
installable v1 candidate is a Project/Session overview. Its cells are not files.
No full file heatmap, read trails, collision detector or live file coverage is delivered.

## Investigation at the task base

Source inspected at `a02ae3311fce57b4e7911051f110cf3ae594e766`; no personal records
were opened or exported. Investigation ended after this bounded source review.

* `web/console/src/view-plugins/bridge.ts`: `SourceSession`, `ViewReading`,
  `pluginBridge.snapshot` and its method allowlist expose opaque Session/Project
  refs, title, workState and freshness only. There are no file events or timestamps.
* `web/console/src/view-plugins/sources.ts`: `localPluginRows`, `cloudPluginRows`
  and `fleetPluginSource` are the current adapter/projection seams. Fleet keys are
  source-namespaced before opaque refs are minted. Private identities stay in host.
* `internal/adapters/sessionfiles/store.go`: `Store.List`, `Listing`, `File`
  provide a narrower existing output index. `File` has `Path`, `AbsolutePath`,
  `Source`, `UpdatedAt`, `FirstAt`, `Updates`, `Anchor` and `Bytes`; do **not** pass
  this structure directly to a plugin. `List` can scan and persist its index, so a
  GET is not evidence of zero backend mutation. It is not an all-file inventory.
* `internal/adapters/sessionfiles/scan.go`: `scan`, `nativeReceipts`, `receipts`
  and `safeNativePath` accept proven successful native Claude write receipts,
  Codex patch success and explicitly published outputs, restrict workspace and
  symlink paths, and skip hidden/vendor locations. They do not observe reads or
  arbitrary shell writes. Unsupported providers have published-only coverage.
  `More`, `Partial` and `Coverage` distinguish incomplete reads and unavailable
  records. Missing/zero timestamps cannot establish recent activity.
* `internal/transport/http/session_outputs.go`: `outputPath`, `outputKey`,
  `Server.sessionOutputs` implement `/v1/sessions/{id}/outputs`. They use
  `admitPinnedTarget` before and after listing, `FindForRead`, conversation identity
  and machine-authenticated POST registration. This is an existing **host-side**
  integration seam, not an API granted to the plugin. No call to it was made by
  this candidate. Content, absolute paths and transcript anchors must stay private.

The upstream Territory recipe reads actual tool calls and a git file inventory.
Its filesystem scans and native commands cannot be copied into this opaque v1
frame. The reference was used for the concept only; all artwork/code is original.

## Smallest proposed snapshot

This is a proposal for root review, **not an accepted v1 message**, new endpoint,
SDK method or permission. The local `activity.js` renderer accepts a deliberately
smaller synthetic subset; it is not a contract validator or production adapter.

```json
{
  "streamId": "opening-scoped-opaque-stream",
  "revision": 1,
  "observedAt": "2026-01-01T00:00:10Z",
  "freshness": "fresh",
  "connection": "connected",
  "coverage": {
    "inventory": "observed_files_only",
    "reads": "unavailable",
    "writes": "native_and_published",
    "partial": false,
    "more": false,
    "reason": null
  },
  "files": [{
    "ref": "opening-scoped-file-ref",
    "projectRef": "existing-opaque-project-ref",
    "relativePath": "src/example.ts"
  }],
  "events": [{
    "ref": "deduplicated-opaque-event-ref",
    "fileRef": "opening-scoped-file-ref",
    "sessionRef": "existing-opaque-session-ref",
    "operation": "write",
    "outcome": "succeeded",
    "occurredAt": "2026-01-01T00:00:08Z",
    "observedAt": "2026-01-01T00:00:10Z",
    "provenance": "native_success_receipt"
  }]
}
```

No file contents, raw transcript, tool input/output, command string, user prompt,
machine ID, absolute path or transcript anchor is needed. Equal file areas avoid
requiring file sizes. A new inventory capability would be needed to claim coverage
of untouched files. Explicit publication is provenance `explicit_publication`, not
proof of a tool write; it cannot be silently converted to recent heat.

`occurredAt` is nullable when source time is missing/untrustworthy. `observedAt`
is host observation time, not Session start or event execution time. Do not replace
unknown source time with now to create fake heat. A heat window requires an agreed
clock/skew policy and trustworthy occurrence times. The minimal candidate renderer
shows successful observed events and no time decay or collision claims.

Use source/execution/project/file identity together internally, then mint per-open
opaque refs. Equal relative paths on different machines or Projects must never
merge. A rename, replaced execution or revoked source invalidates old associations.
Event refs must deduplicate repeated reads/checkpoints without exposing offsets.

## Permissions, freshness and missing observations

File names themselves can disclose private information; `sessions.read` consent
does not authorize them. Root must define a separate reviewed metadata capability,
grant at installation/replacement, local and Cloud authorizations, path redaction,
allowed publication roots, cancellation, revocation and error semantics. Recheck
pinned execution and permission on both sides of async reads. Files outside the
workspace explicitly published for documents are not automatically granted here.
Do not add any renderer fetch, generic REST client or credential transport.

Disconnect means immediately stale. Reconnect requires a fresh snapshot before
live presentation resumes. Freshness is per source/row; one reachable source does
not make all files current. A complete replacement snapshot must distinguish an
authorized empty result from unavailable, partial or refused data. Carry coverage
and observation time even when no files are present; no events means **unobserved**,
not proof of no activity. Retention and payload bounds need registered capacities
and explicit full/partial errors before implementation; this proposal adds none.

Shell redirection, scripts, editors, indirect/nested tool calls, uninstrumented
providers, deleted outputs and reads are not covered by the current output index.
An mtime, successful shell exit or intended tool call is not proof of a Session
touch. The existing index aggregates outputs rather than preserving every event,
so it cannot reconstruct read trails or simultaneous file holders. A collision
would require a separately defined observation rule, not Session work states.

## Root acceptance before a real file layer

1. Approve the product permission/data contract and provenance vocabulary; decide
   whether a smaller **observed outputs** view is sufficient before promising a heatmap.
2. Add a narrow host-owned adapter over the existing output listing (or a new
   separately reviewed event source for reads), stripping private fields and
   preserving coverage. Add Cloud parity rather than treating local success as fleet support.
3. Extend reviewed schema, SDK/bridge allowlist and package capabilities together;
   preserve ref namespaces, revisions, revocation, size bounds and freshness.
4. Verify unsupported providers, shell writes, missing records/time, equal paths
   across sources, permission change during reads, partial results and disconnected
   snapshots. Show real authorized events separately from synthetic fixtures.

`activity.js` and its synthetic screenshot prove a visual extension point only.
The public ZIP contains neither that renderer nor a synthetic dataset. The current
v1 bridge acceptance proves the truthful unavailable state independently.
