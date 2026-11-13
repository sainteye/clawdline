# Persistent Bots over Cloud

This transport module connects the captured source client to the in-process
HTTP seam. It is module-ready, not evidence that the host has mounted the
handler, enabled the feature, deployed a console, or passed phone acceptance.
The [feature contract](project-assistants.md) and
[host obligations](project-assistants-host.md) remain authoritative.

## Operations and identity

`internal/domain/cloudvocab/vocabulary.json` remains the single Cloud table.
The existing generator emits its strict decoders and console carry list.
Fixed nested paths such as `command.key.bot_id` read declared object
properties; they do not interpret credentials, model templates or list paths.
Each nested object is closed and lists its required fields. Absent fields,
additional fields, nulls, non-integer numbers, negative sequence/version
values and integers outside `cloud.MaxSafeInteger` are rejected before Local.

The 16 `SourceClient` methods use the following 16 `project-assistants.*` words.
`B` below is `/v1/project-assistants`; IDs are escaped single segments.

| SourceClient method | Cloud word | Local HTTP | Write gate |
| --- | --- | --- | --- |
| status | project-assistants.status | GET B/status | No |
| projects | project-assistants.projects | GET /v1/project-assistant-targets | No |
| list | project-assistants.list | GET B | No |
| create | project-assistants.create | POST B | Yes |
| get | project-assistants.get | GET B/{bot_id} | No |
| edit | project-assistants.edit | PATCH B/{bot_id} | Yes |
| history | project-assistants.history | GET B/{bot_id}/messages | No |
| send | project-assistants.send | POST B/{bot_id}/messages | Yes |
| stop | project-assistants.stop | POST B/{bot_id}/stop | Yes |
| answer | project-assistants.answer | POST B/{bot_id}/permissions/{offer_id}/answer | Yes |
| markRead | project-assistants.read | POST B/{bot_id}/read | No, readLevel action |
| observe | project-assistants.observed | POST B/{bot_id}/observed | No, readLevel action |
| events | project-assistants.events | GET B/{bot_id}/events | No |
| forget | project-assistants.forget | POST B/{bot_id}/memory/forget | Yes |
| saveMemory | project-assistants.memory | POST B/{bot_id}/memory | Yes |
| export | project-assistants.export | GET B/{bot_id}/export | No |

Projects retains `machine_id`, `cursor`, and `page_size` on its independent route.
List retains `machine_id`, `query`, `cursor`, and `page_size`. History retains
`key`, `conversation_id`, `before_seq`, and `page_size`; events retain `key`,
`after_seq`, and `page_size`. Get/export retain their complete Bot key. The
generated local query converts those values to their original HTTP spelling.
List/history accept 1–100 rows, events 1–256, using the domain constants and
the copied-limit guard. There is no synthetic pagination or snapshot cursor.

Writes retain the entire generated request, including command, content hash,
expected version/revision, memory provenance and fence. The command operation
is specific to each word (`observe` and `memory_save` retain their domain
spellings). The request ref must equal `command.idempotency_key`, which also
becomes the local `Idempotency-Key`. The supplied content hash remains a
string, including the client's empty default; the source discards it and
recomputes the canonical hash bound to its authenticated principal.

The generated route derives the Bot/offer path from that same request, so
there is no independently supplied path subject. Cross-field Bot, source,
fence, cursor and memory-source conflicts are refused before Local. Bot IDs
reject `status`, dot segments, slash, backslash and control characters;
ordinary Unicode single segments remain valid. Names are not Bot IDs.

All 16 feature routes run as a device, without the machine's local exemption.
`LocalRequest.Viewer` comes only from the verified envelope sender. Body
fields cannot provide a principal. `reader_id` remains an untrusted hint
which the source service replaces with its own authenticated reader.

## Authorization and host assembly

Read and observed receipts are mutating POST actions with stable refs and
source idempotency. Their `readLevel` flag permits a paired viewer whose
content-write switch is off. They still require a live valid grant and a
previously observed, fresh, readable online source. A history-only source
can acknowledge its own views; it cannot create, edit, send, stop, answer
permissions, forget or save memory.

Registration constructs no runtime and mounts no route. The integrating host
must mount the existing HTTP handler behind the real device gate; carry the
authenticated sender into its current source authorization; bind machine ID
to the addressed source; resolve the trusted principal; and recheck source,
project, fence and revocation at the service's effect/publication boundaries.
Current fence comparison requires source state and is not replaced by a
Cloud body comparison. The constructor's existence alone proves none of
these host obligations. The host also owns the final API-level negotiation,
feature defaults, pairing adapter, capabilities and runtime assembly.

## Errors, receipts and capacity

Only a complete `project-assistants.v1` failure envelope passes the PA-specific
partial hook. It preserves source HTTP status and the full generated
`error.code`, `error.field` and `error.retry_after_seconds`. This last field
is the source's HTTP Retry-After equivalent: LocalResponse has no generic
header carriage. Other words retain their existing refusal filtering.
The copied Cloud reader returns the complete body to the captured client,
which rejects the typed failure without converting it into generic English.
An unknown send outcome remains unknown and is not automatically resent.

Successful replies must be JSON objects with the expected contract version.
A missing or malformed contract becomes a typed `pa_outcome_unknown`, outer
502. Full PA payloads reuse the registered 8,388,608-byte plaintext ceiling,
including `read`, status and body wrapper overhead. A response beyond that
ceiling is replaced with a complete `pa_capacity_full` envelope naming
`cloud_reply_bytes`, outer 429 and matching `Answer.Code`. No source content,
partial page or invented cursor is published. Raw oversized bodies are
refused before parsing or constructing another full payload.

PA words do not opt into the existing negotiated gzip read list. This work
adds no stream, retry queue, reply cache or capacity number. The existing
16 MiB account ciphertext ceiling and 32 MiB WebSocket framing are separate
bounds; a larger frame allowance is not a reason to enlarge the plaintext
limit.

Synthetic generated-contract fixtures demonstrate why row count is not a
byte bound. A 100-row page at 32,768 text bytes per row encoded with `x`
produces a 3,303,898-byte complete history payload and a 3,304,775-byte export
payload; both are preserved. With `<` instead, JSON escaping produces
19,687,898 and 19,688,775 bytes respectively; both become the small typed
capacity failure. These are reproducible fixture sizes, not measurements of
a person's data or a claim about daily traffic. Export is a whole source
snapshot, so exceeding the transport ceiling remains an explicit limitation.

## Verification

`TestProjectAssistantCapturedSourceRoundTrip` runs the actual TypeScript
SourceClient and captured connection adapter with Node, then drives their
produced envelopes through the real Go Bridge. It compares all 16 original
HTTP methods, escaped paths, complete queries/bodies, device/viewer identity,
stable refs and command headers at the LocalRouter seam. Node absence causes
an explicit skip; a passing Go package alone then does not establish that
cross-language fixture. This test does not open a real network connection.

The PA tests additionally cover malformed/foreign fields, nulls, numeric
boundaries, operation/ref/fence conflicts, fixed nested lookup near misses,
Unicode/path IDs, per-machine reply isolation, content-write gates, revoked
senders, metadata actions, typed errors, unknown outcomes and exact reply
byte boundaries. The console tests exercise the actual copied reply reader,
the carry table, late grant revocation, history-only metadata and no retry.

The original 15-word transport delivery recorded 219,679 distinct decoder
golden rows over 245 words. That retained baseline is described here; the
registered catalog addition is recorded separately below.
Every prior 202,410 distinct input and its result remains present and
unchanged. Comparison preserves JSON number lexemes as well as value types:
for example, `1e3` and `1000.0` must remain different inputs because raw
number spelling can survive into a local request body. Recording adds 5,616
probes to existing words and 11,653 probes for the 15 new words, removing no
prior case. The recorded golden SHA-256 is
`e11ee45ca33e9b52f8bd135c6cbcb86c80e9b4d5dc44d42a992bdf21f07622ef`.
An earlier artifact comparison incorrectly normalized numbers and reported
duplicates; that report is preserved as superseded evidence, not used here.
This is decoder compatibility evidence, not host, Cloud or phone acceptance.

Rerun the focused evidence with:

```sh
go test ./internal/domain/cloudvocab ./internal/app/cloudops ./tools/contract-gen
go test -race ./internal/app/cloudops -run 'TestProjectAssistant'
go run ./tools/contract-gen -check
node --test web/console/src/cloud/vocabulary.test.ts web/console/src/cloud/project-assistants.test.ts
npm run check --prefix web
```

The integrating root owns the normal-environment gate on the exact final
merged tree, source-check fingerprint refresh, review, landing and release.


## Registered configuration choices (API 58)

The captured source client requires API level 58. Older sources report typed
unsupported and never fall back to the legacy `projects` word or `/v1/projects`.
That legacy word remains available for its other callers. The new read has no
body, credential, principal or role field. Its exact closed required fields are
`machine_id`, `cursor`, and integer `page_size` (the existing 1..100 bound).
It uses the authenticated current connection sender and the captured machine,
without consulting the shell's active machine. Read-only grants may read fresh,
readable metadata; creation still requires a fresh interactive writable status.

`projects(request, signal)` returns the generated
`ProjectAssistantProjectsResponse`: contract version, one source, `agents`,
revision, complete `items`, next cursor and `has_more`. Configuration agents
(`claude`, `codex`) are independent of native capability readiness. Returning
these choices does not admit an adapter, authenticate a CLI, verify read-only
execution or memory isolation, or start a model. The normal first-Send runtime
admission still refuses an unavailable production provider.

Before publishing a page, the client checks the current grant and AbortSignal,
contract, source machine/freshness/readability, known unique agent values,
complete same-machine targets, unique Project IDs and cursor consistency.
The form additionally checks one revision/agent set across pages and refuses
repeated cursors or IDs across the accumulated choices. Errors retain their
complete typed envelopes; no error becomes an empty catalog or a write retry.
Only an explicit Load more reads another page. An expired revision requires
an explicit fresh first-page retry. Other page failures can retry that page.
No new cache, row or time bound is introduced.

A source switch clears the selected Project, while name, appearance, policy
and instructions survive. Old initial/page/create responses cannot publish
after source replacement, revocation, cancellation or form closure. The form
rechecks its captured source/revision immediately after the awaited status
returns, before calling create, even if the host has not rendered the authority
change yet. SourceClient retains its own transport pre-request check. An unknown
create keeps its exact command, settings and source through explicit retries,
including a later preflight refusal, until success or an explicit new form.
Roster labels read only the first Project page; unseen IDs retain the existing
ID fallback and are not treated as deleted or unwritable Projects.

The catalog addition records 220,164 distinct golden rows over 246 words,
compared with the retained 219,679-row/245-word baseline. A canonical comparison
preserving JSON number lexemes and number/string types finds zero missing old
inputs and zero changed old results. Added probes are 474 for
`project-assistants.projects`, two for `chrome-tab-input`, and nine for
`copilot-settings-update`; the latter are observed additional null-property subset probes. Their causal
attribution has not been established by a controlled recording comparison;
no old input or outcome changed. Reproduce with
`CLAWDLINE_DECODER_GOLDEN=record go test ./internal/app/cloudops -run
'^TestGeneratedDecodersKeepTheGolden$' -count=1`; compare the retained baseline
with a JSON parser that preserves numbers rather than normalizing floats.

`TestProjectAssistantProjectsEscapedCorrelationCapacity` sends the longest
legal correlation ID (128 `<` bytes) through the real Bridge and checks the
complete `read:<request>` JSON wrapper at the exact 8 MiB boundary and one byte
over. The captured cross-language fixture uses that same escaped name for the
Projects word. This is transport-seam evidence; actual Server route/store and
production encrypted transport acceptance remain separately identified.


`TestProjectAssistantCatalogBridgeThroughServerAndRegistry` closes the local
integration gap: the new vocabulary enters `Bridge.Handle`, delegates through
the production Cloud Router and Server.Handler/device gate, and reads a real
PlaceRegistry populated through Add. It checks two pages, exact complete
queries, stable registered IDs/labels, configuration Agents, fresh source DTO,
read-only pin authority, wrong-machine refusal, a cursor expired by a registry
mutation, and a freshly revoked pin. All six requests initialize zero native
providers. The full typed failure bodies survive the Bridge. This test starts
at the already verified/decrypted sender seam; it does not test relay signature
verification, encryption, production Cloud networking or a physical phone.

The synthetic form browser matrix passes Chromium/WebKit × 390/1440 × English
and Traditional Chinese. A retained pre-fix component provides two failing
controls: after a delayed fresh status, revoking/replacing current authority
still called create once. The candidate calls create zero times in both
controlled scenarios. These browser sources remain synthetic; the real local
Server/registry test above is separate evidence, not a production E2E claim.
