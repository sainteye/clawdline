# Linux native transcript adapter boundary

Status: planned, not implemented. Linux `transcript` currently returns a bounded, control-free
terminal projection. It must not be described as a Claude or Codex semantic transcript. The
`screen` route is separate and is the only Cloud reply that preserves SGR and safe OSC links.

## Boundary

The next implementation adds a provider-neutral `LinuxNativeTranscriptReading` seam. Its input is
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
5. Recheck viewer authorization, terminal incarnation, process identity, writer evidence and file
   identity after the read. A change discards the bytes.

No strong provider evidence means a typed unavailable or unknown reply. It never means an empty
transcript and never falls back to the tmux screen. A successful `entries: []` is reserved for a
stable, strongly bound file that contains no completed semantic row.

## Provider bindings

Claude uses exact `<provider-home>/.claude/sessions/<foreground-pid>.json` evidence. The adapter
requires protocol 1, exact PID, canonical cwd and a process-start match. Parked work resolves only
one live matching background job; it never reuses the frozen foreground session id. The transcript
path is exact `.claude/projects/<slug>/<session-id>.jsonl`, where the slug follows the existing
UTF-16-unit, ASCII-alphanumeric preservation rule. Whether supported Linux Claude versions expose
the same registry and process-start format remains a live-host unknown.

Codex enumerates a bounded `/proc/<foreground-pid>/fd` set and accepts exactly one open regular
file inside `<provider-home>/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`. The kernel-held descriptor
and a separately no-follow-opened provider-home path must name the same inode. The first complete
`session_meta` row must have exact cwd, a nonempty id and `thread_source: "user"`; no recent-file or
same-cwd fallback is allowed. Whether supported Linux Codex versions keep the user rollout open on
the foreground process remains a live-host unknown.

## File and output limits

Directory components reject symlinks, `.`/`..`, slash-bearing or empty ids and unsafe ownership or
mode. Leaves must be service-owned regular files with one link. A held file is statted before and
after a bounded `pread`; replacement, truncation or append during an attempt makes that attempt
unknown. A suffix begun after byte zero is newline aligned, and a torn final JSON row is ignored.

The compatibility ceiling for transcript bytes is 8 MiB and the requested entry limit is clamped
to 1...1,000. Proposed registry, directory and proc-fd scan limits require measurement on the
signed release image before they become supported production constants. Partial semantic payloads
are never returned when a limit is exceeded.

## External outcomes

- `200` with nonempty entries: subject, writer and stable file were proved.
- `200` with `entries: []`: the same proof succeeded with no completed semantic row.
- `403 cloud_read_refused`: viewer authorization failed before or after the read.
- `404 session_not_found`: the authoritative durable Session edge is absent; only this may prune a
  stale browser row.
- `503 session_identity_incomplete`: terminal/process incarnation evidence is missing or changed.
- `503 transcript_unavailable`: the strong provider mechanism is temporarily absent.
- `503 transcript_identity_unknown`: evidence exists but is mismatched, ambiguous or unsafe.
- `413 transcript_limit_exceeded`: a named input or scan bound was exceeded.

Unexpected failures remain `500 internal_failure`; an optional bounded reader may use
`429 transcript_busy`. Error replies contain no transcript bytes or provider paths.

## Acceptance before implementation can ship

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
