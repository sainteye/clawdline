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

## Folded tool rows and patches

`tools-v2` preserves the v1 MCP projection and additionally withholds long ordinary
tool text and large patches in main Claude/Codex Session pages. Tool text up to
1,200 bytes stays inline; longer text carries a 240-code-point preview and an
execution-bound `detail` reference. A patch stays inline only up to 18 displayed
lines and 1,200 body bytes. Larger patches retain path, kind, move destination,
line count, additions and deletions, without diff or file-content bodies. A tiny
folded patch keeps its body when reference metadata would make its JSON larger.
Questions, plans, activities, formal attachments, provider subagent pages and
unsupported record formats retain their existing readers. No encoding prefix
is used to suppress opaque output.

Opening a row reads `entry-v1:<offset>:<digest>` (or the explicitly compressed
`entry-v1z` form). The reference hashes the complete existing projected entry, its ordinal within
the native row, projection version, machine, Session, conversation and kernel
execution. Identical bodies in one native row remain distinct. The
reader opens only the Session's current native record, checks the row boundary,
parses that bounded row, and selects the candidate with the exact digest. This
also handles several entries from one native row. Appends preserve old references;
content changes and other executions do not. There is no client-supplied path,
background tool rerun, or additional persistent cache.

The full **projected entry** is returned on expansion; provider-side truncation
and existing parser summaries are not reconstructed. In particular, Claude's
legacy first-line tool-result projection remains unchanged. Native records remain
intact. The existing record and Cloud body budgets still apply. Folding does not
discard data; a transport refusal remains a named failure with an explicit retry.

The component reads only after expansion, shows animated progress, cancels an
unfinished read when folded or unmounted, and retains a successful body while
mounted. Row keys use that reference, so loading older messages does not remount
already expanded bodies merely because their display positions shifted. A changed reference remounts it, so identical preview text cannot reuse
a different stored body. Patch headers use summary counts until their lines arrive.

A supporting machine advertises `transcript_tools_v2`; Cloud viewers choose v2
only with that explicit descriptor. Older machines keep v1. A stale v2 descriptor
or an older local daemon refusing the view gets one v1 fallback with the same
execution and history cursor. Permission failures do not add fallback requests.
`tools-v2z` and `entry-v1z` use the existing authenticated, bounded gzip rail.
Cloud append reads remain a separate change.

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

The optional `TestReplayLongToolBodyTraffic` accepts `CLAWDLINE_TOOL_BODY_REPLAY_DIR`,
reads only numbered `transcript-N.json` pages in that explicitly named export,
and prints aggregate JSON sizes for the actual folded-body projection. It contains
no private fixture data.

Reply-byte savings do not establish daily traffic or phone-bill savings. Opening
full results or original images adds requests and content. UI folding by itself
saves no network bytes.

## Incremental Cloud updates

A machine advertising `transcript_delta_v1` accepts `delta-v1[z]:<version>` for
its newest compact page. The first request remains `tools-v2[z]`. An unchanged
page returns the existing version receipt. Otherwise the machine recomputes the
same full projection, compares entries with a retained exact version, and sends
ordered replacement splices plus current page metadata. This covers insertions,
removals, tool completion, history rewrites and attachment updates; it does not
assume the provider only appends. Repeated identical entries retain their count.

The browser applies splices atomically only to its named base version and pinned
Session. Request receipts already bind machine, execution, sequence and read name;
late replies cannot settle another request. Reads for the same execution share
one flight, and a changed execution waits for that flight before reading anew.
Invalid updates trigger one full compact read. The presentation cache continues
to use full merged pages and does not expose native append cursors over Cloud.

The per-daemon cache retains at most 32 immutable serialized baselines and 8 MiB
of JSON, evicting the least recently used baseline first. It is not durable
history. A cache miss, restart, different scope or limit, or an update at least as
large as the full page returns that full page. Older machines retain conditional
compact reads or their existing legacy fallback. No new relay operation or Cloud
API deployment is required; compression still occurs before content encryption.

Ordered splices use original baseline coordinates. A longest common subsequence
preserves matching rows at shifted positions, so a full newest window can drop
its first row and append its last without retransmitting the middle. Comparison
work is bounded to 1,000 entries on each side; larger provider expansions
return a full page. Each entry is encoded once. Native records may expand into
more entries than the requested page size, so the reply states the exact
resulting entry count instead of assuming the request limit is that count.

## Other structured Cloud reads

Machines advertising `read_compression_v1` accept `accept_encoding: "gzip-v1"`
for a fixed set of JSON reads: Board cards and details, to-dos and interventions,
roles, projects, schedules, snippets, quotas, capacity, skills, Session info and
the pinned Session list. The representation field is removed before the existing
operation decoder runs. Unknown fields, unsupported representations, permissions,
execution admission, request receipts and read-only operation checks retain their
normal behavior. Mutations, pictures, videos and document bytes are excluded.

Negotiated answers use a distinct `.encoding.gzip-v1` read-name suffix, including
small plaintext replies and ingress/egress refusals. The request and exact pinned
reply proof therefore cannot be confused with a legacy waiter. Existing operation
words, encrypted channel names and relay routing remain unchanged. Replies retain
JSON when gzip plus its four-byte framing is not smaller; encoding occurs before
content encryption and signing. The existing 8 MiB input/output budgets are reused.

Readers verify and decrypt the envelope before bounded decompression. Unsupported
framing, corrupt gzip and excessive expanded output have separate error codes;
a failed frame does not alter the decoder used by the next frame. English and
Traditional Chinese failure copy preserves the technical code. A stale capability
gets one legacy read with the same destination, pin and cancellation signal, then
uses the client's existing learned capability state until a new machine descriptor
resets it. Authentication and authorization errors do not trigger that retry.
