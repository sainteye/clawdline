# Codync-derived persistent assistant foundation

Upstream: https://github.com/leepokai/Codync

Pinned revision: `234219cef2d3f991f0f7225ba89e3c99cdd34177`.

The accompanying LICENSE and NOTICE are unchanged copies from that source's
design/reference bundle. Their terms and attribution remain intact.

Modified/adapted for Clawdline on 2026-11-12:

- `host/src/store/model.rs`: stable Bot identity, avatar shape vocabulary,
  profile/settings and separate chat entries adapted into Go domain/wire types.
- `host/src/agent/bot/queue.rs` and `turn.rs`: serial-turn/snapshot rules and
  explicit-message versus final-text fallback adapted into effect-free Go
  policy functions and synthetic behavior fixtures.
- `host/src/chat/outbox.rs`: explicit visible-message semantics and bounded
  message validation adapted for durable idempotent Go delivery.
- `host/src/agent/acp.rs`, `bot/session.rs`: incoming/pending RPC and native
  session separation retained as contracts/fixtures; the actual client/process
  implementation is not included in this foundation.
- `host/crates/memory/src/lib.rs`, `compaction.rs` and
  `docs/features/context-and-memory.md`: source-aware memory/handoff concepts
  adapted to SQLite revisions, durable tombstones and filtered new bindings.

This is a Go rule/data adaptation, not a wholesale Rust-host copy. No runtime
provider, Electron service, upstream account/relay, group/routine, active dream
or model compaction is imported. Clawdline adds source-machine/canonical
Project authority, actor capabilities, durable receipts, isolation and removal
boundaries. Native Codex memory controls intentionally require fresh evidence
rather than copying the pinned upstream omission.

UI source paths are mapped in `docs/project-assistants-reuse.md`; no UI source
is copied by PA-01. Later extracted functions and approved outline SVGs must
add their actual source/modification record when PA-04 delivers them. The
original upstream NOTICE must remain alongside later derived distributions.

Dialogue alignment on 2026-11-18 also references upstream revision
`9b47189739216b405416b334b9ada5b5b28176c1`:

- `host/src/chat/outbox.rs`: multiple explicit user messages per turn, short
  messages with one useful thought, and final text retained as trace after a send.
- `host/src/agent/bot/updates.rs`, `turn.rs`, and `tool_entry.rs`: distinct text
  and thought segments, per-tool status/output/diffs, and last-segment fallback.
- `apps/desktop/src/renderer/views/thread/TraceView.tsx` and `trace-turns.ts`:
  readable narration and expandable tool details, separate from chat bubbles.

Clawdline retains durable idempotency keys, existing source authorization and
paged Cloud reads. An optional trace step identity preserves new steps while
legacy records continue to use category snapshots. Text is persisted as deltas,
then joined by step on read; it is not stored repeatedly as a growing snapshot.
This is a source-based adaptation, not runtime verification of Codync itself.
