> **Retired Swift-generation record (through 2026-09-19):** The product reasoning is preserved, but Swift/AppKit/iTerm implementation details, source paths, route inventory, port 7717, `/tmp/.clawdline`, and claims about the running Mac app do not describe the Go daemon. References to unavailable retired files are rendered as code instead of live links.
<!-- clawdline-doc: kind=record audience=both -->

# Clawdline messages

This is the inventory of content Clawdline types into assistant sessions and the transcript role
each shape receives. It exists because a terminal has no sender-metadata channel: without an exact
envelope, Claude Code and Codex both persist injected text as a `user` turn and the App and Web
therefore attribute it to the person.

## The roles are not interchangeable

| Transcript role | Who spoke | Transport | Presentation |
|---|---|---|---|
| `user` | The person or a paired device acting for them | `POST /v1/sessions/:id/send`, composer, terminal | Ordinary user bubble |
| `assistant` | The session whose transcript is open | Assistant transcript, including any `<clawdline-image …>` markers it wrote | Ordinary answer, with a thumbnail under it for each marker |
| `peer` | Another Claude session through Claude Code's native peer protocol | `<cross-session-message …>` | Indigo peer card |
| `message` | Another live Claude or Codex session, relayed by Clawdline | `POST /v1/orchestrator/messages`, `<clawdline-message>…` | `Clawdline ↔` card naming the source session and assistant |
| `notice` | Clawdline reporting an orchestrator fact | `<clawdline-notice>…` | Inert state card chosen from typed fields |
| `tool` | A tool call or its result | Assistant transcript | Tool row/run |

The distinction answers two different questions. `message` says *another session said these
words*. `notice` says *the broker observed this state*. A status report from `clawdline-fa` to
Clawdfather is a `message`; task `aaaaaaaa` reaching `success` is a `notice`.

Task briefings, handoff packages and ordinary remote prompts are intentionally not inferred from
their prose. Unless they use one of the exact envelopes below, they remain `user`. In particular,
prefixes such as `[a0939bac clawdline-fa]` and headings made from `===` have no protocol meaning.

## Session message

`POST /v1/orchestrator/messages` is the machine-token-only session-to-session route. Its supported
request fields are:

```json
{
  "from_session": "c6000001-0000-4000-8000-000000000001",
  "to_session": "c6000002-0000-4000-8000-000000000002",
  "text": "The correction is in the same round.\n\n## Status\n\nThe task is still running."
}
```

The current route and `clawdline send` deliver **text only** (at most 100,000 characters). Do not
put `images` in this request: the Go handler decodes only `from_session`, `to_session` and `text`,
so an extra `images` field is silently ignored. A successful send receipt does not mean the picture
was stored or shown. To show the person a picture from the speaking session, use the image marker
flow below. Read `clawdline guide send` for the installed daemon's current wire contract.

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

Older image messages can still appear in transcripts as version 2 envelopes with one top-level
`artifacts` field containing 1…6 closed references. This is a read format for existing messages,
not a way to send a new picture through `POST /v1/orchestrator/messages`:

```json
{"id":"c6000003-0000-4000-8000-000000000003","media_type":"image/png",
 "byte_count":18422,"width":1280,"height":720,"expires_at":1787983200}
```

No reference contains image bytes, a source path, filename, URL, HTML or presentation fields.
`expires_at` is absolute Unix seconds. The same array reaches `Transcript.Entry` and
`GET /v1/sessions/:id/transcript` as `artifacts`; attribution remains all-or-nothing because a
malformed or extra field invalidates the entire v2 envelope. Authenticated clients derive the
relative same-origin route `/v1/artifacts/images/:artifactId` from the opaque id; that retrieval
URL is presentation state, never a stored public URL or part of the message. A live PNG or JPEG is `200`, a known expired or pruned id
is typed `410 artifact_expired`, and an id the store never owned is typed
`404 artifact_not_found`.

App and Web render a live reference as a bounded thumbnail with an enlarged in-app preview. A
past `expires_at`, native missing/expired lookup, or Web retrieval failure stays in the transcript
as an explicit localized expired tile rather than a broken image or an omission. A future hosted
store may back the same opaque relative route; callers and transcripts do not learn or persist its
storage URL.

The store defaults to 24-hour TTL, 64 live artifacts and 64 MiB total, with a 12 MiB source and
normalized-image cap, 12,000-pixel edge cap and 40-megapixel decoded cap. Tombstones are bounded
and retained long enough to distinguish deletion from an unknown id. Pruning removes only files
whose opaque ids and metadata belong to this store; its directory is mode `0700` and its files are
`0600`. The store lives in `CLAWDLINE_NEXT_DIR/session-images`. Until 2026-10-18 an id this store
did not know was also looked up, read-only, in the retired Swift app's picture cache (moved by
`CLAWDLINE_SESSION_IMAGE_DIR`); that reader and the variable are gone, so such an id is unknown.

## Pictures a session shows the person

The route above cannot show a picture to the session that is speaking. It delivers by typing into
the target terminal, and injected text is a `user` turn — so a session sending itself an envelope
would be handing itself a new instruction, and `POST /v1/orchestrator/messages` refuses
`same_session`. That refusal is correct and is not going anywhere.

What replaces it keeps Clawdline reading transcripts and writing none:

```sh
clawdline image send /absolute/path/to/screenshot.png
```

Put all flags before the first image path, including `--target`, `--from` and `--port`:

```sh
clawdline image send --port 7727 /absolute/path/to/screenshot.png
```

The CLI treats every argument after the first image path as another image path.
A trailing `--port 7727` therefore adds two nonexistent files to the batch and causes
`400 invalid_image_path`, even when the actual pictures are readable. The batch is refused
before any pictures are stored.

It is `POST /v1/artifacts/images` with `{"images": [{"path": …}], "terminal": "<terminal id>"}`,
the terminal being the caller's own Session. The store keeps each picture with the conversation record of
that Session and the start of the first row written after it (`artifacts.Shown`); the transcript
route reads forward from there to the first assistant entry, keeps that row as the picture's
anchor, and draws the picture under that entry on every page (`images_shown.go`). The model's text
is not involved. A row still being written is left out of the offset, so it always meets a row's
start. Measured on 2026-10-16 with Opus 5.5: the picture was drawn under the very message whose tool
call ran `clawdline image send`, because Claude Code wrote that message's row after the command
had started; when that row is already in the record, it is the next reply. The request body has `images`, with 1…6 local files, each as an object with exactly one
absolute `path`, and optionally `terminal`. Each file must be a plain file no larger than 12 MiB or
12,000 px on a side. A local path printed in the reply is not a phone-readable picture.

**This used to be a marker.** The response still carries a `marker` for each picture, and a reply
that contains one still shows it (once: a shown picture whose reply also carries its marker is drawn
a single time). But on 2026-10-15 two Sessions on Opus 5.5 stored pictures, were answered 200,
wrote the markers between two tool calls, and the person saw nothing: those models keep prose
written between tool calls only as a summary of it in the record, and the marker went with the rest.
Nothing told either agent. So the guide no longer asks for a marker, and says plainly that stored is
not seen — `GET /v1/transcript?session=…` lists the pictures in their reply's `artifacts` once it is
written. Claude Code's `SendUserFile` does not reach the Clawdline console.

The browser does not read the original local path; Cloud carries the image from the paired machine.
Cloud accepts the stored image's `image/png` or `image/jpeg` media type; changing a JPEG filename
to `.png` does not change its bytes or media type.

The spelling is exactly one, and it carries an id and nothing else:

```text
<clawdline-image id="c6000003-0000-4000-8000-000000000003">
```

No path, no bytes, no dimensions, no closing tag — the metadata is resolved from the store exactly
as a version-2 envelope's is. **Recognition is all-or-nothing**, for the same reason the envelopes'
is: a marker that half-worked and vanished is indistinguishable from one that worked. Everything
below stays visible as ordinary text, byte for byte, and none of it is partly interpreted:

- an id that is not a lower-case opaque artifact id, including the documented `ARTIFACT_ID`
- a different quote character, a different tag case, a missing `>`
- a marker inside a line-anchored ``` or ~~~ fence, so that a reply *about* this format is not a
  reply *using* it. Only that shape: a four-space indented code block and a fence inside a
  blockquote are read as ordinary lines, and an inline backtick span is deliberately not parsed.
  Every one of those escapes still needs a real opaque artifact id, and the id written in prose
  about this format is `ARTIFACT_ID`, which is refused above.
- every marker after the sixth in one turn, which is the same per-message image bound

An honoured marker is removed from the entry's `text` — with its whole line, when it was alone on
one, so a picture on its own line leaves no gap — and the resolved reference is appended to that
entry's `artifacts`. That is the same field, the same closed shape and the same renderers the
`message` role already uses; `GET /v1/sessions/:id/transcript` needed no new field. Both transcript
readers honour markers only in an **assistant** turn: a person quoting the tag is quoting it.

A reference the store cannot resolve resolves to a row that carries the id, `expires_at` in the
past, zeroed `width`, `height` and `byte_count`, and one extra field naming which of two things
happened:

| `state` | what the store said | what the reader is shown |
|---|---|---|
| `"expired"` | it held this image and no longer does — past its TTL, pruned, or its bytes are gone | **Image expired** |
| `"unknown"` | it has no record of this id at all: never stored here, or stored so long ago that even its tombstone was reaped | **Unknown image** |

The field is present only on those rows, so a version-2 envelope's artifact object still has
exactly its six keys, and a client that has never heard of `state` reads `expires_at` alone and
draws the expired tile it has always drawn. Standing in is deliberate: an entry that shows a
picture today and prints raw wire text next week is worse than one that says the picture is gone.
Saying which absence it is, is the other half — telling somebody an image they never had has
expired is the app inventing a memory for them, and the two cases are distinguishable for the whole
window that matters, since a genuinely stored image reads `expired` from its 24-hour TTL until its
7-day tombstone is reaped.

The store's byte budget is global and shared with the pictures sessions send each other
(`maxCount: 64`, `maxTotalBytes: 64 MiB`, oldest evicted first), so a busy machine can evict an
image that is still on somebody's screen; it becomes an `expired` tile there. That is a known
limitation rather than a guarantee of how long a picture lives.

## Orchestrator notices

Notice writers use protocol `clawdline.notice` and wrapper `<clawdline-notice>`. The completion
notice is written at version 3 and every other kind at version 2; versions 1 and 2 of
`task_finished` remain readable for stored transcripts. Version 2 has exactly five kinds:

| `kind` | Audience | Meaning | Important typed fields |
|---|---|---|---|
| `task_finished` | `root` or `parent` | One dispatched task reached a terminal state | `task`, `state`, `result_path`, `outstanding`, `claims_released`, `child_may_still_write` |
| `workspace_overlap` | `root` or `parent` | A new task's claims overlap another task | `task`, non-empty `overlaps` |
| `file_wait_request` | `owner` | A session is waiting for exact repository paths | `wait_id`, `repository`, `paths`, `waiter_session_id`, `reason`, `release_condition` |
| `file_wait_release` | `waiter` | The owner released those paths | `wait_id`, `repository`, `paths`, optional `commit`, optional `note` |
| `handoff_receipt` | `source` | A new root received the first line, or did not | `handoff_id`, optional `title`, `assistant`, `project_dir`, `state` |

Version 3 is the completion notice alone, with what its `body` and its command already say taken
out (`result_path`, `ack_path`, an `audience` that was always `root`, and the timeout flag said
twice). It has three kinds, each with a closed key set:

| `kind` | Meaning | Keys |
|---|---|---|
| `task_finished`, `task_stalled` | The first typing of one task's completion | `task`, `state`, `notice_id`, `body`; `outstanding`, `leftovers` only when above zero; `claims_released` only when a timeout released the claims (it stands for version 2's `child_may_still_write` too) |
| `task_reminder` | Typed instead of the completion notice once that notice has reached the root's screen and is not yet acknowledged | `task_id`, `notice_id`, `body` |

The `body` names one command, `clawdline task show <task id>`; once it has printed a finished task
it sends that task's ACK, so reading the result closes the notice (`task wait` does the same). Which of the two is typed follows the notice's ledger row: no
`transport_delivered_at` yet — the terminal refused it, the root was away, a hold — is the whole
notice, since that is the first time the root reads it; after it, a reminder, also after a daemon
restart. A reminder is a delivery, not an observation: it neither observes nor acknowledges
anything, and it spends an attempt of the same eight. A reminder the root's own record shows handed
to the model counts as reading that notice, as the notice itself does (`SeenInTranscript`). Its
console card is drawn as a reminder of the same event, not as a second completion.

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
6. This inventory, the route reference in `api.md`, and the protocol overview in
   `clawdline-protocol.html`.

Do not add a prose prefix and teach a renderer to guess it. If identity or state matters to the
display, it belongs in a closed envelope.
