# Bot Cloud access and cross-viewer continuation: source-protocol verification

This page maps every assertion of the Bot (project assistant) Cloud access and
resume matrix to what it actually proves. It is a **source-only fixture
record**. It is not encrypted relay acceptance, browser acceptance, physical
device acceptance or production Cloud acceptance; those rows are listed as
pending at the end and remain owned by the Root integration step.

## What the fixtures compose

| Layer | In the matrix | Not in the matrix |
|---|---|---|
| Cloud word | `cloudops.Bridge.Handle` with the real generated decoder, starting at the verified/decrypted sender seam (`Command.Sender`, `VerifiedKey`) | relay envelope sealing, signature verification, the relay link's `Authority` (roster, key match, clock) |
| Dispatch | `cloudtransport.Router` with `LocalAuthorizer`, through the production HTTP gate | a second process |
| Local devices | a real `127.0.0.1` listener (`httptest.NewServer`) over the production handler, with the local, read-only and sending device tokens | LAN discovery, TLS |
| Source | production `Server`, Project Assistant `Host`, `Service`, SQLite store, pin store (`ProjectAssistantGrant`) and identity store, in an empty temporary state directory with a synthetic Cloud identity and Project | the person's state, port 7727 |
| Provider | a synthetic in-process provider that records prompts and can hold a reply on a channel | Claude Code, Codex or any model |

Interleavings use explicit barriers only: the provider's capability callback
(inside runtime admission, before the SQL effect), the source's
`beforeViewPublish` hook (after the SQL read, before publication), and a
channel that holds the provider reply. No row depends on a sleep. Waiting for
a finished turn uses the existing `routineHumanIdle` helper.

## Matrix

Test files: `internal/transport/http/project_assistants_cloud_access_matrix_test.go`
(A, B), `internal/transport/http/project_assistants_cloud_resume_matrix_test.go`
(C, D), `internal/app/cloudops/ops_project_assistants_access_matrix_test.go` (E).

| Row | Assertion | Proof level |
|---|---|---|
| A1 | Account-paired operate and read-only pins, and invited read-only and operate pins, read `get` and `history` through real words; an empty or unpinned sender reads nothing. | source fixture |
| A2 | Read-only grants (account and invited) are refused `send` with `pa_read_only` before the provider or store; an invited writer is admitted as itself. | source fixture |
| A3 | The `read` body's `reader_id` is never identity: the returned cursor is the pin grant's reader. Account-paired devices share the owner reader; each invitation has its own. | source fixture |
| A4 | A TCP device token cannot borrow an owner, Cloud viewer or another reader through spoofed headers or body; its read-only send is refused by the device gate (`forbidden`). | source fixture over TCP |
| A5 | After the source identity's account is replaced, Cloud viewers have no grant (`pa_revoked`, `viewer_grant`) and a local TCP request meets `pa_scope_denied`; nothing reaches the provider; restoring the identity restores access. | source fixture |
| A6 | Replaying an accepted command returns the identical receipt and creates no turn or prompt. | source fixture |
| B1 | A grant revoked after the SQL read and before publication publishes nothing of the packed view, for a Cloud viewer and for a TCP device whose gate grant is revoked. | source fixture, barrier |
| B2 | After a fresh re-pair of the same sender (same principal and reader, new authorization revision), the replay of an already accepted command is the same receipt and does not prompt again. | source fixture |
| B3 | A pin revoked inside runtime admission refuses the send with `pa_revoked` before any effect; retrying the same command is refused again and creates no prompt. | source fixture, barrier |
| B4 | A downgrade to read-only inside runtime admission refuses the send with `pa_read_only`. | source fixture, barrier |
| C1 | Two account-paired viewer instances: the second asks for the first's command after an unknown reply and gets the identical receipt (id, state, resource, content hash) with no new prompt; the same key with different intent is `pa_idempotency_conflict`. | source fixture |
| C2 | Both instances read the same durable history (user, then assistant). | source fixture |
| C3 | The second instance continues the conversation from its own snapshot; both turns share the conversation and the continuation resumes the recorded native session (`Resume`, native session id). | source fixture, synthetic provider |
| C4 | Account-paired devices share the owner read frontier by design; an invited viewer's frontier and unread count are its own and do not move the owner's. | source fixture |
| C5 | Two TCP device clients keep separate reader namespaces and frontiers. | source fixture over TCP |
| D1 | A roster page cursor issued before a fresh re-pair is `pa_cursor_expired` afterwards; the reader is kept and a fresh page works. | source fixture |
| D2 | A turn accepted before a re-pair, whose reply is held in flight across it, completes exactly once; replays during and after the reply return the accepted receipt; one prompt, one reply. | source fixture, barrier |
| D3 | A view packed under one grant and re-paired before publication is refused (`pa_revoked`); a fresh request succeeds. | source fixture, barrier |
| D4 | An older-history view cursor from before a re-pair is `pa_cursor_expired`. | source fixture |
| E1 | Every one of the 34 Bot words' executable vocabulary examples is routed exactly once as the verified sender; commands carry their idempotency key; only real commands ask for the write gate, `read`/`observed` do not, reads ask no command authority. | Bridge unit, recording ports |
| E2 | The machine write switch stops every command before routing and never stops reads or read/observed metadata. | Bridge unit |
| E3 | A read-only viewer (marked as the relay link marks it, only when a write gate is asked) is refused every command with `viewer_read_only` before routing and keeps reads and its own metadata. | Bridge unit |
| E4 | A sender the roster no longer holds is refused every command with `unknown_sender`; Bot reads are not refused at the Bridge and stay the source pin grant's decision (A1, B1). | Bridge unit |
| E5 | A body cannot name its own viewer, and a request on another machine's channel is `wrong_machine`; neither is routed. | Bridge unit |

Mutation checks were run in the task worktree and reverted: making every Cloud
grant writable turns A2 red, and removing the Bridge's `ViewerReadOnly` refusal
turns E3 red.

## Defect candidate kept out of the green matrix

**D1-candidate — a fresh re-pair during runtime admission does not invalidate
the in-flight send.** When the sender is re-paired (same device, new key and
paired time, so the same principal and reader with a new authorization
revision) while its send is inside the provider capability check, the send is
accepted (`200`, receipt `accepted`) and later prompts. `Service` effects treat
an authorization revision change as `pa_stale_version` (`effect_authorization`),
but the runtime's post-capability recheck re-authorizes without comparing the
revision admitted by `Service`. A downgrade or revocation at the same barrier
is refused (B3, B4), so no permission is widened; the gap is that an old
grant's in-flight request is not invalidated by a fresh grant. The reproducer
and its output are private task artifacts; fixing it needs a production change
outside this verification's write set, so it is reported to Root rather than
asserted as green or blessed here.

## Still pending, not proved here

- Encrypted relay round trip, envelope signatures and the relay link's
  `Authority` (roster, key equality after re-pair, clock) with these words.
- Hosted console and browser behavior, and physical phone or second-computer
  viewers; a viewport test is not a device.
- Production Cloud deployment and the sender's browser candidate (API 74
  `bot_id` + conversation), which the current API 73 source does not carry.
- Native Claude Code and Codex resume; the matrix uses a synthetic provider.
- Routine progress, fallback and group behavior beyond reading existing
  baseline fields.
