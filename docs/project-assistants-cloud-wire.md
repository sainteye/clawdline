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

The 16 `SourceClient` methods use the following 15 new words plus `projects`.
`B` below is `/v1/project-assistants`; IDs are escaped single segments.

| SourceClient method | Cloud word | Local HTTP | Write gate |
| --- | --- | --- | --- |
| status | project-assistants.status | GET B/status | No |
| projects | projects (existing) | GET /v1/projects | No |
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

All 15 new routes run as a device, without the machine's local exemption.
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

The full catalog test contains explicit new cases for all 15 words. The
decoder golden has 219,679 distinct rows over 245 words after recording.
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
