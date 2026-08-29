# Clawdline messages

This is the inventory of content Clawdline types into assistant sessions and the transcript role
each shape receives. It exists because a terminal has no sender-metadata channel: without an exact
envelope, Claude Code and Codex both persist injected text as a `user` turn and the App and Web
therefore attribute it to the person.

## The roles are not interchangeable

| Transcript role | Who spoke | Transport | Presentation |
|---|---|---|---|
| `user` | The person or a paired device acting for them | `POST /v1/sessions/:id/send`, composer, terminal | Ordinary user bubble |
| `assistant` | The session whose transcript is open | Assistant transcript | Ordinary answer |
| `peer` | Another Claude session through Claude Code's native peer protocol | `<cross-session-message …>` | Indigo peer card |
| `message` | Another live Claude or Codex session, relayed by Clawdline | `POST /v1/orchestrator/messages`, `<clawdline-message>…` | `Clawdline ↔` card naming the source session and assistant |
| `notice` | Clawdline reporting an orchestrator fact | `<clawdline-notice>…` | Inert state card chosen from typed fields |
| `tool` | A tool call or its result | Assistant transcript | Tool row/run |

The distinction answers two different questions. `message` says *another session said these
words*. `notice` says *the broker observed this state*. A status report from `clawdline-fa` to
Clawdfather is a `message`; task `2ef96bc1` reaching `success` is a `notice`.

Task briefings, handoff packages and ordinary remote prompts are intentionally not inferred from
their prose. Unless they use one of the exact envelopes below, they remain `user`. In particular,
prefixes such as `[a0939bac clawdline-fa]` and headings made from `===` have no protocol meaning.

## Session message

`POST /v1/orchestrator/messages` is the machine-token-only session-to-session route. Its closed
request body is:

```json
{
  "from_session": "A0939BAC-569B-4B87-9DF4-DE493EC327EA",
  "to_session": "509F54A8-356E-420D-9EAC-73D676C9580E",
  "text": "The correction is in the same round.\n\n## Status\n\nThe task is still running.",
  "images": [{"path":"/Users/you/Desktop/current-state.png"}]
}
```

`images` is optional and contains 1…6 closed objects with exactly one `path`. A path must be a
normalized absolute local path; URL strings, relative or dot-segment paths, extra fields,
directories, unreadable files and unsupported bytes are refused. Clawdline does not fetch remote
content and does not trust a filename extension or claimed MIME type. It bounds each source and
decoded raster, decodes it, re-encodes it as PNG, and copies it into the Clawdline-owned artifact
store before terminal delivery. `text` may be empty only when `images` is non-empty.

`from_session` may be the source's exact terminal-neutral id or its current process-bound
conversation id. `to_session` is the exact terminal-neutral id. Clawdline resolves both against
live assistant sessions, rejects aliases, title/prefix matches, ambiguity and self-send, and
refuses a target showing a menu. The route requires the orchestrator token and an
`Idempotency-Key`; a paired device cannot claim an assistant identity.

The source is a live identity resolved under machine authority, not proof that the HTTP caller is
running inside that source process; possession of the orchestrator token is the trust boundary.
An `ok` response proves one terminal typing attempt was accepted, not that the target transcript
observed or acknowledged it.

Text-only messages keep the literal version-1 wire already stored in transcripts. The target
receives one physical terminal line:

```text
<clawdline-message>{"body":"…","kind":"session_message","protocol":"clawdline.message","source":{"assistant":"claude","id":"A093…","label":"clawdline-fa"},"version":1}</clawdline-message>
```

The JSON string may contain escaped newlines while the wrapper itself contains no LF or CR. App
and Web discard the wrapper after strict decoding, restore the body's Markdown and paragraphs, and
show the source as `Clawdline ↔ clawdline-fa · Claude`. The source id is protocol evidence, not a
UI action or link.

Version 1 has one kind, `session_message`, and exactly the fields shown above. Unknown versions,
unknown or missing fields, partial wrappers, prose around a wrapper, and quoted lookalikes are not
partly interpreted. Their bytes stay visible under the role they originally had.

An image message uses version 2 and adds exactly one top-level field, `artifacts`, containing 1…6
closed references:

```json
{"id":"46cb6d40-c13f-4fea-9cf0-936f86b78da4","media_type":"image/png",
 "byte_count":18422,"width":1280,"height":720,"expires_at":1787983200}
```

No reference contains image bytes, a source path, filename, URL, HTML or presentation fields.
`expires_at` is absolute Unix seconds. The same array reaches `Transcript.Entry` and
`GET /v1/sessions/:id/transcript` as `artifacts`; attribution remains all-or-nothing because a
malformed or extra field invalidates the entire v2 envelope. Authenticated same-origin clients
construct `GET /v1/artifacts/images/:artifactId`: a live PNG is `200`, a known expired or pruned id
is typed `410 artifact_expired`, and an id the store never owned is typed
`404 artifact_not_found`.

The store defaults to 24-hour TTL, 64 live artifacts and 64 MiB total, with a 12 MiB source and
normalized-image cap, 12,000-pixel edge cap and 40-megapixel decoded cap. Tombstones are bounded
and retained long enough to distinguish deletion from an unknown id. Pruning removes only files
whose opaque ids and metadata belong to this store; its directory is mode `0700` and its files are
`0600`. `CLAWDLINE_SESSION_IMAGE_DIR` moves the whole deleting store, including for isolated tests.

## Orchestrator notices

Notice writers use protocol `clawdline.notice`, wrapper `<clawdline-notice>`, and current writer
version 2. Version 1 remains readable for stored transcripts. Version 2 has exactly five kinds:

| `kind` | Audience | Meaning | Important typed fields |
|---|---|---|---|
| `task_finished` | `root` or `parent` | One dispatched task reached a terminal state | `task`, `state`, `result_path`, `outstanding`, `claims_released`, `child_may_still_write` |
| `workspace_overlap` | `root` or `parent` | A new task's claims overlap another task | `task`, non-empty `overlaps` |
| `file_wait_request` | `owner` | A session is waiting for exact repository paths | `wait_id`, `repository`, `paths`, `waiter_session_id`, `reason`, `release_condition` |
| `file_wait_release` | `waiter` | The owner released those paths | `wait_id`, `repository`, `paths`, optional `commit`, optional `note` |
| `handoff_receipt` | `source` | A new root received the first line, or did not | `handoff_id`, optional `title`, `assistant`, `project_dir`, `state` |

`task_finished.state` is `success`, `failure`, `timeout`, `cancelled` or `spawn_failed`.
`handoff_receipt.state` is `picked_up` or `first_line_failed`.

Notice payloads cannot supply Markdown, HTML, CSS, actions, URLs or translated copy. App and Web
choose their own wording and styling from the validated kind/state and escape every displayed
field. The envelope's `body` is only the model-readable and legacy-client fallback.

## Adding or changing a type

A message-type change is complete only when all of these move together:

1. The closed encoder/decoder schema and version policy.
2. Claude and Codex transcript normalization.
3. App and Web presentation, including visible fallback for failed recognition.
4. `/v1/sessions/:id/transcript` serialization.
5. Red-before-green tests for wire safety, strict fallback, both transcript readers and rendering.
6. This inventory, the route reference in [`api.md`](api.md), and the protocol overview in
   [`clawdline-protocol.html`](clawdline-protocol.html).

Do not add a prose prefix and teach a renderer to guess it. If identity or state matters to the
display, it belongs in a closed envelope.
