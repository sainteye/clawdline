# Persistent assistant recovery and unknown effects

This local acceptance slice extends [runtime ownership](project-assistants-runtime.md),
[SQLite persistence](project-assistants-store.md), and
[server composition](project-assistants-server.md). The reference remains Codync
`234219cef2d3f991f0f7225ba89e3c99cdd34177`; see
[reuse boundaries](project-assistants-reuse.md) and `third_party/codync/NOTICE`.
It does not introduce a separate chat authority or enable the production provider.

## Execution boundary correction

Capability inspection, history/memory reads, provider Open, and waiting for the
memory writer can outlive the authority snapshot that preceded them. The Engine
now revalidates current source authority after preparation, before persisting a
returned native binding, and inside the current-memory callback immediately
before Send. A preparation refusal never opens a process. A refusal after Open
keeps the turn unknown, closes the owned handle, and never sends a prompt.
Terminal reconciliation remains a durable maintenance write; it is not a newly
authorized model/tool operation. No resource bounds were added.

Human request authority and background machine source authority are distinct.
Revoking a LAN bearer rejects further human writes; it does not by itself revoke
an already admitted background Bot. The HTTP execution test replaces its
synthetic source account to exercise the background authority boundary.

## Rerunnable acceptance map

The tests below use the actual Store, Engine, and HTTP/Host composition, with
scripted Go providers. All input, identity, native session and work data are
synthetic. Counts refer to those scripted external-effect entry points.

| Test | Exact input and surface | Persisted output and call counts |
| --- | --- | --- |
| `TestRuntimeRevalidatesAfterExecutionPreparationAndOpen` | Revoke during worker capability preparation, Open return, or current-memory handoff; Engine Submit | Preparation: failed turn, Open=0, Send=0. Open return: unknown turn, no native binding, Open=1, Send=0. Memory handoff: unknown turn, Open=1, Send=0. Actors reclaimed after owned cleanup. |
| `TestRuntimeSQLiteReopenRetainsUnknownTurnAndWorkReceipt` | Submit one turn; DispatchWork loses response; repeat identical and conflicting work commands; close Engine; persist a queued admission behind the interrupted turn; close SQL pool and reopen the same file; Recover; retry original send and a conflicting send; submit a new send | Original accepted receipt remains identical; work intent remains unknown and cannot be observed as delivered. Identical work retry reports unknown; changed hash conflicts. Reopened unknown turn blocks the queued turn and new send. Recovery/retries leave the exported snapshot unchanged. Total Open=1, Send=1, Dispatch=1. |
| `TestRuntimeSQLiteReconnectKeepsMessagesEventsAndReadCursor` | Submit; inspect executing receipts; publish one explicit reply twice and a changed payload; publish old generation; complete with final text; Observe and MarkRead; close/reopen SQLite; Recover and replay original commands | Accepted, executing, delivered and observed are separate receipts. Executing cannot be observed as completion. Exactly two message facts (one input, one explicit reply); final text adds no bubble. Conflicting event and stale generation refuse. One durable read cursor survives. Read metadata adds one read event, no message event; reconnect from the saved frontier returns no new events. Total Open=1, Send=1. |
| `TestProjectAssistantServerRevocationDuringExecutionPreparation` | HTTP accepts a send, then synthetic source identity changes while worker capability inspection is blocked | Real SQL contains one failed turn and one user message, with no native binding or provider Open. The HTTP acceptance is distinct from background completion. |
| `TestProjectAssistantServerCapacityAndReconnectDoNotRepeatEffects` | Hold one prompt; fill the existing pending-turn limit through HTTP; send overflow; replay the exact original request including its original fence; change its text; disable | Typed HTTP 429 `pa_capacity_full`; exact replay succeeds at capacity, changed payload returns 409 `pa_idempotency_conflict`. Those requests leave the SQL export unchanged. Disable retains one unknown turn and cancels every unsent queued turn. Open=1, Send=1, Close=1. |
| `TestProjectAssistantServerLateOpenRetainsUnknownCleanupHandle` | Block Open; disable; release Open with a handle whose Close fails; attempt enable; explicitly retry disable after allowing Close | Disabled setting and unknown turn survive. While Open/cleanup is unresolved, one actor/process slot remains with unknown usage; enable refuses. Explicit cleanup reclaims handle and slot, Send=0, Close attempted at least twice. |

Additional existing behavior is covered without building a second fixture:

- `TestProjectAssistantServerCleanupFailurePersistsFalseAndUnknown`: SQL cleanup
  failure keeps durable opt-out and unknown usage; retry completes after repair.
- `TestProjectAssistantServerOptOutQuiescesRestartedQueueWithoutActors`: persisted
  queue cleanup does not construct a provider.
- `TestProjectAssistantServerCloudFreshPinAndMetadataThroughGate`: synthetic Cloud
  router, read metadata and fresh pin revocation, without a production Cloud call.
- `TestProjectAssistantServerRevalidatesWitnessAfterCapabilityAdmission` and
  `TestProjectAssistantServerFailedEnableWriteCompensatesHostAndReportsUnknown`:
  human admission revocation and settings-write compensation.
- `TestHostUnknownOpenWithoutHandleKeepsResourceOwner`: unknown Open without a
  cleanup handle remains inspectable; no invented successful close frees its slot.
- `TestRuntimeStopTerminatesPendingPermissionInSameMutation` and
  `TestRuntimeFreshRevocationPreventsIncomingEffects`: pending permissions and late
  protocol events cannot create new authorized effects after stop/revocation.

Run the new slice with:

```sh
go test ./internal/app/projectassistant ./internal/adapters/projectassistantstore ./internal/transport/http \
  -run '^(TestRuntime(RevalidatesAfterExecutionPreparationAndOpen|SQLite)|TestProjectAssistantServer(RevocationDuringExecutionPreparation|CapacityAndReconnectDoNotRepeatEffects|LateOpenRetainsUnknownCleanupHandle))' -count=1 -v
```

## Acceptance limits

These are local integration results, not native-vendor or production-Cloud
acceptance. No vendor adapter, vendor initialization, login, model prompt, live
Bot provider, running daemon, browser, deployment or public release is exercised.
The feature's default-off and production-provider admission policy are unchanged.
PA-05's broader acceptance must retain those separate pending surfaces rather
than treating this slice as proof of the whole step.
