# Reading stored tool output

A titled Codex MCP call previously put the first 4,000 characters of its outer
result directly on the card. Nested MCP envelopes spent that preview on JSON
escaping and automatic screenshot metadata, sometimes before any readable text.

The `tools-v1` transcript view projects recognized `content` blocks before
applying a 1,024-byte UTF-8 preview. It preserves text order, native tool identity,
status and duration. Image bytes stay out of the initial page; unknown blocks
remain represented in the text and available in stored native output. An inner
MCP error is a failed tool call. Completion says nothing about a test passing.
Existing explored-command folding and ordinary tool rows retain their behavior.

Short text stays inline. Longer text and the stored native JSON are read on
expansion, with a 150 KiB detail budget and explicit truncation flags. The native
JSON is a serialization of the stored result object, not a byte-for-byte copy of
the provider's original response. Codex may already have shortened that object,
including the middle of an embedded image. Truncation warnings follow the
representation being read: hidden image metadata does not warn on complete
visible text, while the stored native view still reports its own truncation.

A result reference names a JSONL row and a digest scoped to the Session,
conversation, kernel-pinned execution, native tool identity and image projection.
Changing image eligibility invalidates older references so an old image index
cannot silently select different content after an update. Reads resolve
only the Session's own record, check the row boundary and digest, and refuse a
replaced or missing source. Each read is bounded by the existing transcript read
window. No client-supplied filesystem path or remote image URL is accepted.
Cloud additionally requires the existing exact machine and execution pin and
rechecks transcript authority through the content-read bridge.

Automatic operation screenshots in `codex/toolSurface` metadata stay out of
normal tool cards: there is no count, image action, retry or metadata-only warning.
Their original stored representation remains available in native output. Explicit
image content is offered only after strict Base64, raster decoding and existing
byte/pixel policy checks succeed. Incomplete explicit image bytes remain in stored
native output without an image action. Uploaded and deliberately delivered
Clawdline image attachments keep their existing presentation.

Eligible explicit images load only when requested, one selected image at a time. The console reads
the original directly on that click, without an intermediate thumbnail request.
The existing thumbnail representation remains available to compatible clients.
An animated progress indicator stays visible through transfer and image decoding;
transient failures can be retried explicitly. A missing or incomplete stored
image has an explanatory terminal state without a retry action. Technical refusal
details stay available behind a disclosure. Cards retain their identity when
older rows arrive or the live transcript window shifts. Text and native-output
expansion also show progress. Metadata and controls share the card header's horizontal inset.
Only complete, strictly decoded embedded raster images within the
existing pixel and byte policies are eligible. A middle-truncated Base64 value
is refused. There is no reconstruction, remote fetch, or rerun of a tool.

## Wire compatibility and reuse

A machine with the pinned content-read rail advertises `transcript_tools_v1` in
its authenticated descriptor. A viewer without that explicit capability keeps
the original transcript request. A capable viewer uses the `view` field, which
also distinguishes its reply name from legacy and historical reads. A stale
capability may fall back once on a malformed read or read timeout; permissions,
execution failures and aborts do not trigger a fallback.

`tools-v1z` and `tool-v1z` explicitly allow gzip before AES-GCM encryption and
signature generation. Compression is used only when smaller. Authenticated
plaintext starts with `gz1` followed by a zero byte and a gzip stream; ordinary
JSON remains valid. The viewer verifies the envelope and decrypts before
bounded streaming decompression, refusing output above 8 MiB. The daemon never
compresses retained publications or replies to viewers that did not opt in.

A compact page carries a SHA-256 version over its exact serialized data and
machine/execution scope. `tools-v1[z]:<version>` returns only `unchanged` and the
matching version when the page is identical. The existing reader owns the held
page; no new persistent cache is introduced. Older pages retain their separate
cursor and revision semantics. Any source change returns a full current page.
Cloud append-delta reads remain unsupported: the existing local append protocol
and its stale-cursor recovery are unchanged.

## Verification

Synthetic behavior tests cover hidden automatic screenshots, complete explicit
images, independent text/native clipping warnings, old-index rejection, mixed and
unknown blocks, UTF-8 boundaries,
legacy output, stored-source truncation, wrong targets, edited rows, historical
rows outside the tail, conditional page reuse, thumbnail/original separation,
image corruption, negotiated compression, envelope authentication, decompression
limits and old-machine requests. The optional `TestReplayToolResultTraffic`
reads an explicitly named private export and runs the real projection, Cloud
bridge, compression and encrypted envelope serializer. It prints aggregate byte
counts only; private Session content is never a repository fixture.

Reply-byte savings do not establish daily traffic or phone-bill savings. Opening
full results or original images adds requests and content. UI folding by itself
saves no network bytes.
