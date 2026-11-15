# Native provider failure metadata

This prerequisite slice preserves native ACP error classification. It does not
implement fallback, choose another Agent, change retry policy, or prove that a
failed prompt had no effects. A running prompt that fails remains unknown under
the existing runtime policy, even when its error is classified as quota.

## Internal contract

`internal/app/projectassistant.ProviderFailure` is an application-only error
wrapper. `errors.As` exposes `Reason` and `RPCCode`; `Unwrap` preserves the domain
failure used by `pa.Code`. RPC errors still have `pa.Unsupported` and the safe
`rpc_<code>` field. There is no public wire/schema change. An empty reason means
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

Fallback selection, attempts, shared cooldowns, safe effect evidence, switching,
handoff, UI and deployment acceptance remain separate work. Typed quota alone
never authorizes replay; EOF, partial writes, cancellation and cleanup ambiguity
retain their existing unknown-effect barriers.
