# Provider failure metadata and pre-send Agent fallback

The original prerequisite slice preserves native ACP error classification.
That classification alone does not choose another Agent, change retry policy,
or prove that a failed prompt had no effects. The pre-send backend slice below
adds a separate authoritative guarantee and cleanup transition. A running
prompt that fails remains unknown under
the existing runtime policy, even when its error is classified as quota.

## Internal contract

`internal/app/projectassistant.ProviderFailure` is an application-only error
wrapper. `errors.As` exposes `Reason` and `RPCCode`; `Unwrap` preserves the domain
failure used by `pa.Code`. RPC errors still have `pa.Unsupported` and the safe
`rpc_<code>` field. The classification wrapper remains private; the pre-send
slice below adds optional fallback configuration and attempt facts to the
existing wire. An empty reason means
the RPC error has no recognized classification.

The adapter inspects only actual JSON-RPC error objects. Native error messages,
details and arbitrary data never enter the wrapper, error string, projection or
persistence. Ordinary final replies containing quota or authentication words
remain ordinary replies.

## ACP volume and response ordering

ACP incoming notifications, replay and text capture follow Codync's native
transport behavior: there is no local notification-count, line-size or captured
text-size ceiling. The former 256 queued-event disconnect and 1 MiB ACP/capture
rejections are removed. Reader-owned control responses resolve pending RPCs
directly so a sink waiting for a control answer cannot block its own response.
The ordered consumer still handles notifications, permissions, prompt completion
and the load-replay barrier in wire order. Historical load notifications are
drained while no turn is active, before a new prompt can receive them.

The consumer is started before initialization. Closing wakes its condition
variable, cancels its sink context, drops queued events and retains the existing
owned process cleanup protocol. Queue entries are released as consumed. The
existing RPC/permission ownership, write deadlines, source fences and one-write
prompt behavior remain. Final message/storage admission is unchanged; native
stream capture does not authorize a larger stored message.

Native `max_tokens` and `max_turn_requests` stops retain distinct typed
`prompt_max_tokens` / `prompt_max_turn_requests` fields instead of becoming a
local capacity error or ordinary success. The runtime's unknown barrier still
prevents automatic replay after these stops.

`TestACPBurstPreservesSlowSinkOrderAndPermission` injects 288 notifications with a
slow sink and separately exercises synchronous permission answers and a control
RPC awaited inside that sink. `TestACPLoadReplaysHistoryBeforeResponseWithoutCapture`
replays 1,024 historical notifications before the load response.
`TestACPNativeTextExceedsFormerLocalCaptureAndFrameCaps` checks both aggregate
stream text and a single frame above 1 MiB. Close and provider-stop tests cover
cancellation and native limits. These synthetic reproductions do not identify
the exact cause of any earlier production capacity failure whose cause was not
retained.

| Trusted input | Normalized reason |
| --- | --- |
| RPC code `-32000` | `signed_out` |
| `usage_limit_exceeded` | `usage_limit` |
| `billing_error`, `account_on_hold` | `out_of_credits` |
| `rate_limit`, `rate_limit_exceeded` | `rate_limited` |
| `overloaded`, `server_error`, `server_overloaded`, `internal_server_error`, `response_too_many_failed_attempts` | `overloaded` |
| `authentication_failed`, `oauth_org_not_allowed`, `unauthorized`, `auth` | `signed_out` |
| `model_not_found` | `model_missing` |

The first string among `data.errorKind`, `data.category`, and Codex's
`data.codexErrorInfo` wins before validation. Codex camelCase names are converted
to snake_case; object forms use their first lexically ordered key. Categories
must contain only lowercase ASCII letters and underscores and fit in
`bots.error_kind_bytes` (40 bytes). Invalid or unknown categories stay
unclassified; they do not fall through to a lower-priority category.

For the recognized rate/billing/account categories, Claude's usage-limit text in
the RPC message or details takes precedence over billing. This follows Codync's
`host/src/agent/acp.rs` (`rpc_error`, `codex_kind`) and
`host/src/agent/fallback/reason.rs` at
`9f67eb09084db61b3cca9c84a11bc03df9de29b7`. Clawdline deliberately omits upstream's
uncategorized message-text fallback: quoting usage words in an unknown category
does not gain classification. Native process/setup/model/mode policy and error
conversions outside the RPC classification boundary remain unchanged.

## Verification boundary

`TestACPProviderFailureClassification` sends genuine JSON-RPC error frames from
the owned subprocess fixture into the production Client and PendingPrompt.Wait.
It checks errors.As, unchanged pa.Code/field semantics, sanitized error strings,
usage-before-billing precedence, native category variants and negative controls.
`TestACPReplyQuotaWordsAreOrdinaryText` covers ordinary successful output.

`TestACPProviderFailureRuntimeDoesNotReplay` crosses the actual ACP client,
SQLite repository and Runtime together. A prompt is recorded by the peer before
quota, quota followed by EOF, or direct EOF. Each case becomes TurnUnknown;
repeating the same submission and invoking recovery still yields one provider
Open and exactly one written prompt. Application tests also preserve unknown
codes and cleanup-error precedence through error wrappers.

These fixtures call no external provider or model and use no credentials. On a
machine without native process inventory, subprocess tests explicitly skip;
such a run is not native acceptance. Focused commands are:

```sh
go test ./internal/adapters/projectassistantacp -run 'TestACPProviderFailure|TestACPReplyQuota|TestACPErrorKind' -count=1 -v
go test ./internal/app/projectassistant -run 'TestProviderFailure' -count=1 -v
go test ./internal/domain/capacity -run 'TestEveryBoundIsRegistered|TestTheRegisterMatchesItsSnapshot' -count=1
go test ./internal/transport/http -run 'TestEveryRegisteredRowIsMeasured|TestCapacityMeasuresMatchTheirSnapshot' -count=1
```

Native qualification, mid-turn progress/ledger, parking, recap/catch-up, UI and
deployment acceptance remain separate work. Typed quota alone
never authorizes replay; EOF, partial writes, cancellation and cleanup ambiguity
retain their existing unknown-effect barriers.

## Bot Agent fallback: pre-send backend slice

This is a backend implementation for configured, default-login Claude Code and
Codex identities. It is not acceptance of the complete fallback Board step, a
native-provider qualification, or a public feature release. Bots remain behind
the existing feature gates. No CLI, login, native memory setting, permission
policy, model or command is changed.

### Observable contract

Create and edit accept optional `fallback_agents`, using the existing `claude`
and `codex` enums. A new Bot defaults to no fallbacks. A Bot without a
fallback chain does not consult shared rest to block its primary. When every
configured Agent is resting, a new human turn still tries its primary, as
upstream `relay_turn` uses `next_step(now).unwrap_or(0)`. An already attempted
Agent is never retried inside the same logical turn. An omitted edit preserves
the saved chain; an explicit empty array clears it. The primary Agent cannot be
edited. Identities must be unique, including the primary, within Codync's
three-fallback ceiling. With only these two default-login enums available, at
most one alternate is currently valid. A fixed non-Auto mode refuses an
alternate until a no-freer stand-in mapping is qualified; this does not block
new primary input merely because the alternate mode is unqualified.

Each new visible message chooses the earliest untried, non-resting identity,
subject to those primary-start and alternate-mode rules.
Routines do not select an alternate from this chain. A main binding on another
Agent (or revoked binding) refuses the routine before Open; routine/fallback
interaction remains separately qualified by the routine owner.
The primary `Bot.Agent`, BotKey, full ProjectScope, ConversationID, accepted
message ID and logical Turn ID remain unchanged. `ExecutionBinding.Agent`,
`OpenRequest.Bot.Agent` and the fresh handoff identify the effective Agent;
`OpenRequest.PrimaryAgent` separately retains the configured primary. The
existing two-Agent router receives the effective configuration. Every attempt
has its own binding and increasing generation. One existing prompt deadline
and process slot cover the entire logical turn, including cleanup and switching.

A provider may return `PreSendRefusal` from **Open**, with an owned cleanup
handle, only when it can authoritatively guarantee no prompt or external
action occurred. The wrapped `ProviderFailure.Reason` is diagnostic metadata,
not that guarantee. The Host invalidates the old tools on an Open error; any
tool invocation or native event observed during Open contradicts a no-effect
claim and forces unknown. The runtime also rejects contradictory durable
permissions, work links, output, messages or trace facts. Absence of those
facts alone is never proof. This slice conservatively holds any permission
offer rather than transferring permission authority to another Agent.

The runtime appends a refusal fact and revokes the old binding, confirms Close,
then atomically appends confirmed cleanup and the shared rest. Only afterward
may it mint another fence and tools. It rechecks stop, feature/source access,
Project resolution, configuration, memory and the current binding before Open
and before Send. A configuration change between attempts refuses the switch.
Old events, tools and permission answers remain fenced out. Missing handles or
failed cleanup retain the original owner and process slot. A separate host-only
`OpenNotStarted` fact can reclaim an unused slot when provider Open was never
entered and setup registrations were reclaimed; it never permits fallback. Providers never
share an MCP lifetime. There is no same-Agent Send retry.

Turn `attempts` is an append-only sequence of immutable facts: ordinal, effective
Agent, full fence, stage, whether Send began, effect knowledge, cleanup knowledge
and normalized reason. A `sending` fact commits before the external write.
After Send, effects remain `unknown` even if an answer and successful Close are
observed; this implementation has no authoritative native side-effect ledger.
The final answer and one bilingual switch notice commit through the existing
terminal receipt. Explicit native messages still suppress duplicate final text.

| Boundary | May open an alternate? |
| --- | --- |
| Open returns authoritative pre-send refusal, owned handle and confirmed Close | Yes, only another configured eligible identity |
| A typed usage/category error without the explicit guarantee | No |
| Open returns no handle, cleanup fails, or any host tool was invoked | No; unknown owner/effect is retained |
| Send/Wait returns quota, EOF, timeout, partial-write or uncategorized error | No |
| Permission answer or Broker response is lost | No; existing unknown receipts remain authoritative |
| Crash after opening, refusal, cleanup, transition or sending fact | No automatic advancement or restart continuation |

Recovery marks interrupted configured-fallback turns unknown and does not use
the ordinary restart notice to send them again. A person's next explicit input
is a new logical turn, not replay of the interrupted input. Existing unconfigured
Bots keep their ordinary restart behavior. SQLite CAS, receipt uniqueness and
transactions own these transitions; a failed receipt insert rolls back the
attempt/rest change. Historical multi-Agent bindings require matching attempt
facts. Unknown newer schema/export versions are still refused. The aggregate
schema version stays unchanged; the machine-owned rest table is additive.

### Upstream reference and rests

The read-only reference was verified at Codync commit
`9f67eb09084db61b3cca9c84a11bc03df9de29b7`. Citations are to that exact source:

- `host/src/agent/fallback/mod.rs:20-69,115-132`: primary plus at most three
  fallbacks, unique login identities and effective configuration.
- `host/src/agent/fallback/rest.rs:11-76`: durable shared-login rest; transient
  failures use 1, 5, 25 and 60 minutes. Backoff restarts one hour after the prior
  expiry. Usage with unknown reset rests one hour; no credits rests six hours;
  supported sign-in harnesses rest one hour. Claude and Codex are the supported
  harnesses represented by this slice. Model/mode absence creates no global rest.
- `host/src/agent/fallback/rest.rs:72-76`: upstream can use the latest exhausted
  window reset. This slice has no qualified reset telemetry and therefore uses
  the upstream unknown-reset duration. Usage percentage never selects an Agent.
- `host/src/agent/fallback/reason.rs:62-103`: typed failure categories. Existing
  Clawdline classification is reused unchanged; arbitrary chat/error text does
  not become an authoritative no-effect result.
- `host/src/agent/bot/fallback.rs:348-411,485-515`: bounded advancement without a
  generic retry, and primary eligibility on the next visible turn after rest.
- `host/src/agent/bot/fallback/park.rs:38-105`: upstream kills the old connection
  before taking a parked session. This slice uses confirmed owned cleanup and
  the existing filtered fresh-session handoff instead of parking native history.

Rests are keyed by machine plus Agent and survive Host/database reopen and Bot
deletion. They are separate from Bot memory/export. A successful answer clears
the answering identity's rest in the terminal transaction. Clock expiry changes
selection only on the next message, never mid-turn. The capacity register covers
the three-entry configuration ceiling and upstream rest durations up to six
hours. The HTTP capacity reader measures actual remaining durable rest without
constructing a native provider.

### Verification and remaining work

`TestProjectAssistantFallbackHTTPKeepsLogicalTurnAndDurableRest` uses the real
HTTP routes, Service, Engine, Host and fresh SQLite: known Claude Open refusal,
confirmed cleanup, one Codex Send, one final answer/notice, persisted rest across
Host/database reopen, then fake-clock expiry returning Claude on the next input.
It also restores a legitimate multi-Agent archive and refuses a newer export.
`TestProjectAssistantFallbackHTTPWithOwnedOSProcesses` repeats the pre-send
path using two owned helper OS processes under the production named budgets;
both PIDs are reaped and neither installed vendor Agent is constructed.

Unknown-path tests cover diagnostic-only errors, missing handles, failed cleanup,
Open tool effects and Send/Wait errors. SQLite failure injection covers refusal,
confirmed-cleanup receipt, alternate start and pre-Send persistence boundaries;
reopening and explicit recovery never restart those inputs. Configuration tests
cover read-only/foreign/stale writes and omitted versus explicit-empty edits.

Native adapters deliberately do **not** construct `PreSendRefusal` in this
change. Their existing typed quota errors therefore remain unknown after Send
and do not automatically switch. Installed native A/B/A, authoritative pre-send
qualification, mid-turn tool/effect progress, completed-action ledger, parked
sessions, upstream recap/catch-up, fixed-mode stand-ins, sign-in/reset telemetry,
fallback editing/status/switch-back UI and broader exhaustion UX remain separate
work. Codex continues to report `Resume=false`; no model recap calls are made.
