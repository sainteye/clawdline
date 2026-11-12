# Source-owned Bot Project choices

API level 58 adds `GET /v1/project-assistant-targets`. A Bot creation form can
read registered Project choices without using the deliberately empty legacy
`GET /v1/projects` catalog. The separate path reserves no Bot ID: `projects`
remains a valid ID under `/v1/project-assistants/`.

This endpoint supplies configuration metadata. It does not check CLI
installation, credentials, read-only enforcement, native memory isolation or
provider readiness. It neither starts a provider nor enables the feature.

## Request and response

All three query parameters are required exactly once. Unknown keys, duplicate
keys, malformed query encoding and non-integer page sizes are refused.

| Parameter | Meaning |
| --- | --- |
| `machine_id` | The currently authenticated source machine; never a routing hint to another machine. |
| `cursor` | Empty for a new snapshot, otherwise the opaque `next_cursor` from the preceding page. |
| `page_size` | Integer from 1 through the existing `projectassistant.MaxPageRows` bound (100). |

For example:

```http
GET /v1/project-assistant-targets?machine_id=source-machine&cursor=&page_size=100
```

The generated DTOs are `ProjectAssistantProjectsRequest`,
`ProjectAssistantProjectsResponse` and `ProjectAssistantProjectOption` in
`api/v1/project-assistants.schema.json`. The response contains:

| Field | Meaning |
| --- | --- |
| `contract_version` | `project-assistants.v1`. |
| `source` | The existing current source-status envelope, including actual read/write access. |
| `agents` | `claude` and `codex`, the configuration kinds accepted by Create. These are not native capability evidence. |
| `revision` | SHA-256 of the complete, stably sorted registered metadata snapshot. |
| `items` | Complete `{target: {machine_id, project_id}, label}` choices. |
| `next_cursor` | Continuation after the last returned choice; empty on the final page. |
| `has_more` | Whether at least one actual choice remains in the same snapshot. |

An empty registry returns `items: []`, `next_cursor: ""` and `has_more: false`.
A byte-limited page may contain fewer than `page_size` choices. Clients must
follow `has_more` and `next_cursor`, preserve the target IDs for duplicate
labels, and discard a partially collected snapshot when a cursor expires.

## Source, identity and feature policy

The real HTTP gate supplies the request witness. The handler revalidates that
witness with the existing human-authority adapter, current Cloud viewer grant
when applicable, feature state and source policy before reading the list.
Caller-supplied identity fields, a fabricated verdict, an orchestrator token or
a cursor cannot substitute for that witness.

The source must be paired, readable, online and fresh. Hidden/default-off
sources return `pa_disabled`; reading this endpoint does not create a Host or
install feature tables. Retained `history_only` data follows the existing read
policy. A read-only device or viewer can read configuration choices on a
visible source; its source envelope stays non-writable, and existing Create
and native-turn admission continue to enforce their own write requirements.

After the response has been encoded, the handler rereads the registry and
revalidates current authority immediately before publication. A changed
snapshot or changed principal, reader, authorization revision, feature or
source policy refuses the entire response. This is an in-process publication
fence, not a transaction with an external Cloud relay.

## Registered metadata and continuation

The only Project catalog input is
`projects.OpenPlaceRegistry(...).List()`. Each choice uses `PlaceID(Path)` as
its `project_id`, the current source machine as its `machine_id`, and a safe
basename as its label. Equal labels with different IDs remain separate;
linked worktrees remain separate choices. Invalid/ambiguous registry metadata
is refused rather than collapsed or published partially. Hidden registry
choices are absent. The existing `places.registered` capacity limits the
visible snapshot; no cache, timeout or new row limit is introduced.

Listing does not enumerate a home directory, read provider/session history,
read Project content, stat every registered path or establish canonical
scope. A deleted directory can still be listed as saved metadata. Create and
each native turn continue through the existing `projectAssistantProjects.Resolve`
to recheck the selected ID, directory and canonical scope.

Rows are sorted by Project ID. Revision hashing includes `Path`, `AddedAt` and
`ProjectKey` for the complete registry snapshot, so reordering its file does
not expire a cursor but adding, removing or changing metadata does.

The cursor contains a fixed cryptographic frame: the authority/snapshot
binding digest, the last returned Project ID and an HMAC-SHA-256. The existing
daemon local token supplies a domain-separated signing key; reading metadata
creates no token or permanent cursor state. The binding includes the
authenticated principal, reader, authorization revision, source machine,
feature/source policy and catalog revision. Observation time alone is excluded
because every fresh source read produces a new observation time. Rotation of
the signing token expires old cursors.

A cursor must authenticate against current metadata and current authority,
and its `after` ID must be a real nonterminal boundary row. Malformed,
tampered, cross-principal, cross-machine, downgraded-grant or outdated
continuations return `pa_cursor_expired`, not a fabricated empty page.

## Complete byte budget

Both the complete HTTP JSON body, including its encoder newline, and the
Cloud read wrapper must fit `cloudops.ReadPlaintextBytesLimit` (8 MiB).
`cloudops.ProjectAssistantReadReplySize(replyName, status, body)` is a pure
measurement through the existing `payload` serializer, including JSON HTML
escaping. Empty/invalid JSON or failed serialization returns an error, never a
successful zero-byte measurement. It grants no authority and publishes nothing.

The HTTP handler measures each complete candidate prefix with the worst legal
read correlation name: `read:` plus 128 `<` bytes. The existing `requestName`
rule accepts at most 128 printable bytes, and `<` has the maximum JSON escaping
cost per input byte. This derives a conservative budget from that existing
rule; it adds no new request limit. Tests exercise `requestName`, `machinePlan`
and the actual payload serializer and fail if that rule changes. The operation
word is not the correlation name and would undercount a long request's wrapper.
Cursor, source metadata, agent options,
revision, `has_more` and wrapper bytes are included. It keeps the last full
prefix that fits. The final prefix can be smaller than the preceding one when
removing a continuation cursor, so all allowed complete prefixes are checked.
No labels are truncated, no choices are skipped and no partially encoded
Project content is sent. If even one complete choice or the empty metadata
envelope cannot fit, the typed result is `pa_capacity_full`.

Cloud operation registration and hosted consumption belong to the Cloud
integration. This endpoint alone does not establish a usable hosted picker.

## Verification and scope

`TestProjectAssistantTargets*` uses real `Server.New`, `Handler`, the HTTP
gate, empty configuration directories, version-1 registry files and synthetic
identities. It covers default-off table/provider absence, non-repository and
repository/worktree choices with duplicate labels, deleted directories versus
Create, 101-row pagination without repetition/omission, metadata revisions,
strict query/cursor failures, different machines/principals/grants, read-only
access, invalid request witnesses, hidden and malformed registries, retained
history, and controlled authorization/catalog changes before publication.
Registry, hidden-path, removal and ID fixtures use `t.TempDir` and
`filepath.Join` for the current operating system's absolute paths. Malformed
registry tests retain valid path encoding so rejection exercises the intended
structure or duplicate, rather than a different platform's absolute-path rule.
Cross-compilation does not establish Windows runtime acceptance.

Budget tests compare the exported measurement with an actual existing
`Bridge.Handle`/`Answer` payload, exercise HTML escaping and invalid JSON,
force multi-page complete-row packing, and cover wrapper-only, cursor-only,
metadata-only and single-row capacity failures at the real 8 MiB limit.
The old Project catalog stays empty and `projects` remains a usable Bot ID.

The design follows the separation of saved Bot configuration from first-send
execution in Codync revision `234219cef2d3f991f0f7225ba89e3c99cdd34177`.
Its folder browsing/provider-history mechanisms are not used as authority for
this endpoint. This change performs zero model/provider calls, adds no
dependency, and does not claim UI, production Cloud, native provider or release
acceptance.
