# Session output files

A Session's header has an **Output files** button on desktop and phone. Desktop
opens a right-hand panel; phone opens the same listing at full width. There are
no conversation/files tabs. Closing leaves the mounted conversation, reading
position and draft in place. Search and type filters operate on the retained
index. Images load bounded thumbnails on demand; preview and download read the
original only after a person asks.

## Provenance and persistence

The index belongs to the assistant's conversation ID, not a reusable terminal
coordinate. It lives in `session-outputs/` under the daemon's state directory,
with private permissions and atomic checkpoints. It records exact paths,
creation/modification/publication, first and latest event times, update count,
and the native record byte anchor. Missing native timestamps stay unknown rather
than being replaced with the record file’s modification time. Same-path events merge; different directories
remain separate. This is an index of current files, not a version archive.

Native discovery consumes successful Claude Code `toolUseResult` file receipts,
Codex `patch_apply_end` success events, and direct `apply_patch` calls matched
to their own successful result. Native call checkpoints survive a restart. It excludes failed or proposed tool
calls, user attachments, prose mentioning paths, hidden workspace paths,
external symlink targets and workspace-wide mtime searches. Other providers and
shell commands require explicit publication:

```sh
clawdline output add --kind created ./artifacts/plan.md
clawdline output add --kind modified ./src/main.go
clawdline output add /absolute/path/to/generated-image.png
```

Run this after the file exists and only for files the Session actually produced
or is deliberately delivering. `published` is the default: it does not claim a
file was created by the Session. A successful `clawdline image send` also tries
to register each original file. An index failure is reported separately from an
image already stored. The original file stays in its source location; image
publication does not silently make a durable copy of a temporary file. The
existing transcript image cache still has its own expiry policy.

A listing consumes at most one existing 8 MiB transcript reading budget and
checkpoints complete JSONL rows. **Read more records** explicitly advances it.
A partial append is revisited; malformed interior rows mark coverage incomplete.
An oversized single row and an unavailable record
have distinct coverage states. Unknown coverage is never an empty-success
claim. Native coverage does not include arbitrary shell writes, even when it
reaches the end of the record. File deletion keeps a previously indexed row with
an unavailable state. Conversation IDs keep records separate across terminal
reuse and daemon restart.

## Read boundary

`GET /v1/sessions/{id}/outputs` lists one conversation. Machine-authenticated
`POST` on that route registers one exact regular file. A paired read cannot
register, supply an arbitrary path, or choose a filesystem root. Reads of
`/outputs/{opaque-id}` resolve only registered IDs in that same conversation.
The source path is copied as text; downloading stores bytes on the current device.

Local and encrypted Cloud reads share these endpoints. Cloud uses `outputs` and
`output-file` on the pinned content-read channel, with the exact machine,
Session and execution generation checked before and after reading. Each chunk
has an offset, size/mtime revision and immutable file ID. A changed revision
aborts the transfer. This revision is not a content hash and does not defend
against an authorized local writer deliberately preserving metadata.

OpenRoot and regular-file identity checks reject symlink replacement. Downloads
are JSON/base64 chunks, never executable documents served under the console's
origin. Text previews use the existing 2 MiB document budget and stay text; HTML uses
the existing sanitized sandboxed reader. Downloads keep the larger file budget.
SVG is downloadable but is not inserted as active markup. Unsupported formats
retain their download action. A shared file link retains the machine, Session, execution generation and opaque
file ID; it opens the same panel and preview in Cloud. Local mode cannot invent
a paired-machine identity, so its share action is disabled.

Browser Blobs, thumbnail URLs and pending reads
are released when the panel/preview leaves its Session.

## Capacity and first-version scope

The capacity register contains the per-conversation 200-file index, 2 MiB index
byte cap, 4096-byte path cap, 256 retained conversation indexes and 64 MiB total
index storage. New records are refused at capacity; existing records are never
evicted to hide overflow. Indexes are retained until explicitly managed by the
machine owner; this first version does not automatically prune output history.
The file transfer reuses the registered 32 MiB video byte budget and 512 KiB
sequential chunk budget. Thumbnail decoding reuses the registered report image
side and pixel budgets and the existing thumbnail dimensions.

This version does not provide old file snapshots, a file editor, directory
browsing, batch ZIP export, or an assertion that every assistant/tool has native
success receipts. It does not publish files publicly. The design-only proposal
has been superseded for mobile entry: both layouts now use the header button.
