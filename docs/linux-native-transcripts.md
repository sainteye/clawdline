# Linux native transcript adapter boundary

Status: implemented in the Linux runtime and Cloud command route; signed-image live-host acceptance
is still required before declaring provider-version support. Linux `transcript` reads provider
JSONL through the native identity adapter. The `screen` route remains separate and is the only
Cloud reply that preserves SGR and safe OSC links.

## Boundary

The provider-neutral native transcript seam takes
a subject proved jointly by the durable task-to-terminal edge, the current tmux pane, and the
current provider process. Its output is the existing Web `{entries, signature}` shape. Claude
registry/JSONL evidence and Codex open-file/rollout evidence are separate adapters behind that
seam; Relay code does not guess provider paths.

The read sequence is fail closed:

1. Apply viewer authorization and resolve the durable task-to-terminal edge.
2. Require the retained create incarnation to match a current provider observation. Carry the
   exact PID, process-group and Linux procfs start token used for that observation.
3. Derive provider HOME and canonical cwd from runtime configuration and the admitted Session,
   never from request text.
4. Select and read one provider file through held, no-follow descriptors and bounded input.
5. Recheck viewer authorization, terminal incarnation, the retained exact process identity,
   writer UID/credentials, registry evidence and file
   identity after the read. A change discards the bytes. Files larger than the bounded window are
   read from a descriptor-held 8 MiB tail, with its real start offset bound into the signature.
   One retained reader instance admits one bounded read at a time; contention never falls back to
   a terminal capture.

No strong provider evidence means a typed unavailable or unknown reply. It never means an empty
transcript and never falls back to the tmux screen. A successful `entries: []` is reserved for a
stable, strongly bound file that contains no completed semantic row.

## Provider bindings

Claude uses exact `<provider-home>/.claude/sessions/<foreground-pid>.json` evidence. The adapter
requires `peerProtocol: 1`, exact PID, canonical cwd and a process-start match. Parked work resolves only
one live matching background job whose PID and session id both differ from the foreground; it never
reuses the frozen foreground process or session id. The transcript
path is exact `.claude/projects/<slug>/<session-id>.jsonl`, where the slug follows the existing
UTF-16-unit, ASCII-alphanumeric preservation rule. Whether supported Linux Claude versions expose
the same registry and process-start format remains a live-host unknown.

Codex enumerates a bounded `/proc/<foreground-pid>/fd` set and accepts exactly one open regular
file inside `<provider-home>/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`. Year, month and day use
ASCII digits and must form a real Gregorian date. The kernel-held descriptor
and a separately no-follow-opened provider-home path must name the same inode. The first complete
`session_meta` row must have exact cwd, a nonempty id and `thread_source: "user"`; no recent-file or
same-cwd fallback is allowed. Whether supported Linux Codex versions keep the user rollout open on
the foreground process remains a live-host unknown.

## File and output limits

Directory components reject symlinks, `.`/`..`, slash-bearing or empty ids and unsafe ownership or
mode. Leaves must be service-owned regular files with one link. A held file is statted before and
after a bounded `pread`; replacement, truncation or append during an attempt makes that attempt
unknown. A suffix begun after byte zero is newline aligned, and a torn final JSON row is ignored.

The compatibility ceiling for bytes read in one attempt is an 8 MiB suffix and the requested entry
limit is clamped to 1...1,000. Reverse parsing admits at most 4,096 physical rows and a five-second
deadline, so an 8 MiB suffix of tiny nonsemantic JSON rows cannot monopolize ingress or allocate a
range per row. Row work overflow is typed 413; deadline exhaustion is typed 429. The reader records
the actual post-read file size and suffix offset;
the parser drops the leading partial line. If a nonzero suffix contains no newline boundary, it is
not a provable JSONL row window and returns typed 413. Proposed registry, directory and proc-fd scan
limits require measurement on the signed release image before they become supported production
constants. Partial semantic rows are never returned when a limit is exceeded.

## External outcomes

- `200` with nonempty entries: subject, writer and stable file were proved.
- `200` with `entries: []`: the same proof succeeded with no completed semantic row.
- `403 cloud_read_refused`: viewer authorization failed before or after the read.
- `404 session_not_found` (or legacy `not_found`): the authoritative durable Session edge is
  absent; only a 404 with one of these codes may prune a stale browser row.
- `503 session_identity_incomplete`: terminal/process incarnation evidence is missing or changed.
- `503 transcript_unavailable`: the strong provider mechanism is temporarily absent.
- `503 transcript_identity_unknown`: evidence exists but is mismatched, ambiguous or unsafe.
- `413 transcript_limit_exceeded`: a named input or scan bound was exceeded.

Only the documented transcript codes cross the public error boundary. Unexpected internal typed
failures are rewritten to generic `500 internal_failure` without their code or message. Reader
contention or a changing bounded source
uses `429 transcript_busy` with a bounded `retry_after`. Error replies contain no transcript bytes
or provider paths. The hosted client keeps the last-good transcript for 503, 413 and 429.

## Signed-image acceptance before support can ship

Use one fixed synthetic subject across Claude, Codex, unit, integration and Web fixtures. Cover
authorization revocation, pane/PID reuse, wrong task edges, symlinks and unsafe modes, file mutation,
Claude parked ambiguity, Codex missing/ambiguous fd evidence, semantic row parity, and the distinction
between a proven empty transcript and unavailable evidence. Web must retain last-good transcript on
typed 503, prune only on true 404, and render ANSI/OSC only on `screen`.

Release acceptance additionally requires throwaway sessions on the exact signed Ubuntu image:
prove provider versions and HOME, procfs visibility, selected writer identity, restart/reboot
behavior, two successful native provider reads, all named negative probes, an independently
reviewed exact candidate receipt, and the matching hosted-console build stamp. Until those facts
exist, this page is an adapter contract and explicit unknown list—not a support claim.
