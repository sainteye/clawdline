# Bots: advanced verification boundaries

This is the evidence matrix for the existing advanced verification step. It
adds a real-duration source-protocol soak without changing production limits,
retry policy, feature access, or the release gate. Public Release still hides
Bots. The source baseline is `cd0f027f1060e299168e7f1b7370ea825bbd61f2`.

## New HTTP and SQLite soak

[`project_assistants_soak_test.go`](../internal/transport/http/project_assistants_soak_test.go)
uses the existing `newPAServerFixture` composition: production HTTP handler,
authentication gate, Host, Service, Engine, and a real isolated SQLite store.
Requests use an actual `127.0.0.1` HTTP listener. The only provider is an
in-process scripted Go peer; the test has no installed-provider alternative,
vendor executable, login, dependency installation, or paid API operation.
`CLAWDLINE_NEXT_DIR` is an empty test-owned directory; the Server's explicit
configuration and selected Project also use fresh test-owned directories.

The always-on `TestProjectAssistantHTTPDurableSoakControl` runs two successful
turns and the final unknown/queue/reopen controls. The separate ten-minute test
skips unless `CLAWDLINE_BOTS_SOAK_817B8489=1`. Ordinary CI and local gates must
leave that flag unset, along with the installed-native qualification flags.

```sh
go test -v ./internal/transport/http \
  -run '^TestProjectAssistantHTTPDurableSoakControl$' -count=1

CLAWDLINE_BOTS_SOAK_817B8489=1 go test -json ./internal/transport/http \
  -run '^TestProjectAssistantHTTPDurableSoakTenMinutes$' -count=1 -timeout=12m

env -u CLAWDLINE_BOTS_SOAK_817B8489 \
  -u CLAWDLINE_NATIVE_QUALIFICATION_07DDAF71 \
  -u CLAWDLINE_NATIVE_QUALIFICATION_ROOT tools/check.sh
```

Use a writable task-owned Go cache when the sandbox denies the default cache.
No full suite or frontend bundle is required for this bounded slice. The
functional HTTP/SQLite waits use the registered production cleanup budget;
they do not borrow the deliberately shortened deadlines of fault unit tests.
The ten-minute duration, 60 successful turns, 10-second pacing, and 20 ms
observation ticker are test inputs, not new product capacity limits.

| Acceptance criterion | Observable assertion |
| --- | --- |
| Real elapsed duration | `time.Now`/`time.Since`, fixed wall-clock pacing across at least ten minutes, UTC start/end and every completed-turn checkpoint; no injected clock. |
| Bounded traffic | 60 sequential successful HTTP admissions/completions, then one intentionally unknown prompt and one already admitted queued message; existing production admission limits are unchanged. |
| Disconnect/reconnect | Close the client's idle TCP connections after every completed turn, construct a new client, and resubmit that exact accepted HTTP command. Receipt identity, command, resource, state and event sequence must remain identical. This is completed-request connection loss, not an interrupted-body or SSE test. |
| Source cursor continuity | Read a source snapshot, then actual HTTP event pages from its frontier across all reconnects. Reject duplicate IDs, nonadvancing pages and events at/before the saved cursor. Compare the observed event set against the SQLite export to detect missing events. This is the source read-view cursor, not a roster SSE cursor or a person's read marker. |
| No duplicate input, answer or receipt | Assert exact message/turn totals, unique durable message IDs/texts and receipt IDs, and one successful-send receipt per successful turn. The queued and unknown messages have no generated answer. |
| Lost execution response | Only the final scripted `PendingPrompt.Wait` returns `ExecutionEffectUnknown`, after its `Send` has been counted and after a second HTTP message has been queued. No vendor retry or synthetic successful fallback answer is involved. |
| Further effects blocked | While unknown and queued turns coexist, a fresh HTTP send must return HTTP 500 `pa_outcome_unknown` / `inspect_interrupted_turn`. Scripted Send stays at 61. This tests the existing typed error mapping, not a newly invented HTTP status. |
| Cleanup and durable reopen | Stop the source with production cleanup budgets, prove known zero owned actors/processes, close the SQL pool, reopen a real Server on the same file, start feature lifecycle and replay the original unknown admission. Messages, turns and receipts remain identical to the post-cleanup snapshot; its receipt is unchanged and Send stays at 61. Unknown stays unknown; orderly cleanup cancels the unsent queued turn. |

The distinction in the last two rows matters. In the current product, when
**only unknown turns remain**, a genuinely new message explicitly acknowledges
them and can start a new turn (`Engine.Submit` in `runtime.go`). It does not
replay the old prompt. This soak does not assert that every new message must
be refused after orderly cleanup, and does not change that product behavior.
It verifies refusal while a queue is held, then exact original-command replay
after cleanup/reopen without any extra provider Send.

The real-duration run passed on Darwin/arm64, from
`2026-11-15T21:06:13.403933Z` through `2026-11-15T21:16:13.558815Z`.
The independently recorded wall duration was **600.138790 seconds** (Go's
test event rounds elapsed time to 600.15 seconds). Observed counts were:

| Measurement | Observed value |
| --- | ---: |
| Unique accepted user messages (`submitted`) | 62 |
| Completed prompts / answers | 60 / 60 |
| Unknown prompt / unsent queued message | 1 / 1 |
| Scripted provider Open / Send / Close | 61 / 61 / 61 |
| Completed-send admission receipts | 60 |
| All durable receipts after cleanup/reopen | 247 |
| Source events observed / saved frontier | 306 / 307 |
| TCP reconnects / SQLite pool reopens | 60 / 1 |
| Extra Send after unknown | 0 |
| Native vendor prompts / paid API requests | 0 / 0 |

`submitted` counts unique accepted messages; it excludes the intentionally
refused fresh send and repeated queries of existing admissions. It is not a
count of every HTTP request. The two-turn short control passed in 0.243
seconds on the same test source before the single final long run.

The task artifacts retain full final control/soak logs, extracted JSON
evidence, source SHA-256 manifests, prior CI logs and the two-file patch.
The authoritative ordinary gate outcome is `final-check.log` and
`final-evidence.json`, rather than an inference from this soak's success.
The preflight found the canonical existing frontend dependencies missing
`@noble/curves@2.4.0` (`npm ls --all --silent` exited 1). Because this task
forbids dependency installation, its final gate uses a task-local npm
wrapper that permits ordinary read-only validation but refuses `npm ci` or
other installation commands. It does not bypass dependency validation. If
the existing dependency tree is still incomplete, Root must restore it and
rerun the ordinary gate; the soak alone is not a green commit gate.

## Existing independently inspectable coverage

The following definitions were read at the baseline; unchanged suites were
not rerun for this matrix. Their suite receipts are described separately
below. A definition describes coverage, not proof of a new execution.

| Boundary | Existing source definition and assertion | Scope/limitation |
| --- | --- | --- |
| Above former 256-notification threshold | `projectassistantacp/client_test.go`: `TestACPBurstPreservesSlowSinkOrderAndPermission` checks 288 ordered chunks, permission/control responses inside a slow sink, final text, and exactly one recorded prompt. `TestACPLoadReplaysHistoryBeforeResponseWithoutCapture` drains 1,024 history notifications without adding current capture. | Scripted ACP subprocesses; short stress/control cases, not ten-minute vendor streaming. |
| Above 1 MiB frame/text | Same file: `TestACPNativeTextExceedsFormerLocalCaptureAndFrameCaps` checks long aggregate stream and one frame above 1 MiB. `projectassistantacp/process_test.go`: `TestProcessWritesACPFrameBeyondFormerLocalCeiling` writes a request containing `(1<<20)+128` context bytes and checks the peer response. | Transport acceptance does not raise final message/storage admission. Process test explicitly skips Windows or unavailable inventory. |
| Unknown execution and recovery | `projectassistantacp/fallback_error_test.go`: `TestACPProviderFailureRuntimeDoesNotReplay`; `projectassistantstore/runtime_recovery_integration_test.go`: `TestRuntimeSQLiteReopenRetainsUnknownTurnAndWorkReceipt`. | Real ACP/SQLite/Engine and durable queue/work receipt controls. No provider billing or production Cloud claim. |
| Message, observed receipt and read-marker recovery | Same store file: `TestRuntimeSQLiteReconnectKeepsMessagesEventsAndReadCursor` checks one input/one explicit answer, separate executing/delivered/observed states, read marker persistence, no new event after saved frontier, Open=1/Send=1. | This independently covers the person's read marker and observed receipt; the soak covers source event cursors and admission receipts. |
| OS cleanup ownership | `projectassistantacp/process_test.go`: `TestProcessCleansObservedSetsidDescendant` uses `nativeLimits()` for actual census and a controlled 300 ms census delay. `TestProcessCleanupUsesOneRemainingBudgetAndFallsBackToOwnedHandle` deliberately exhausts a short remaining budget and retains unknown while reaping the owned direct process. | Unix inventory/setsid controls skip on Windows. A Windows cross-build/vet is not Windows execution. No OS process is created by the new soak. |
| Late Open/cleanup uncertainty | `project_assistants_resilience_test.go`: `TestProjectAssistantServerLateOpenRetainsUnknownCleanupHandle`; `projectassistanthost/provider_test.go`: `TestHostUnknownOpenWithoutHandleKeepsResourceOwner`. | Deliberate short-failure tests retain ownership; do not use their tiny budgets to judge ordinary OS/SQLite operations. |
| Durable cleanup failure | `project_assistants_host_test.go`: `TestProjectAssistantServerCleanupFailurePersistsFalseAndUnknown` installs a rejecting SQL trigger, preserves durable opt-out and unknown capacity, then repairs and explicitly retries cleanup. | Local real SQLite fault; no production-store mutation. |
| Atomic storage rollback | `projectassistantstore/repository_test.go`: `TestCommitRollbackOnLateSQLFailure` rejects receipt insertion and compares the whole export before/after. | Transactional rollback of facts/events/receipt, not physical disk failure or long-running storage exhaustion. |
| Final publication authority | `project_assistants_http_test.go`: `TestProjectAssistantHTTPFinalPublicationRechecksSource` revokes/denies/hides the source after repository read and refuses to publish content; export also checks the post-read fence. | Handler/service fault injection, with a repository fixture. Real-store/Cloud-router read controls also exist in `project_assistants_view_server_test.go`; neither is a live Cloud deployment. |

## Existing CI receipts and platform interpretation

Read-only GitHub run queries and complete existing logs were retained for:

- [CI 43758214060](https://github.com/sainteye/clawdline/actions/runs/43758214060), exact head `c534e3800fcdd40c8115450d59a7653a729d633a`: completed, five jobs successful.
- [CI 43756466403](https://github.com/sainteye/clawdline/actions/runs/43756466403), exact head `b04f06303201910df5f5124c73ddd014c57110c1`: completed, five jobs successful.

The five jobs are formatting/vet/cross-builds/guards, Linux Go tests,
macOS/Darwin paths, console checks/builds/tests, and the CI notification job.
The ten existing test files named in the matrix match both CI heads
byte-for-byte; the artifact `ci-source-comparison.json` records each comparison.
They are not five native operating systems. The workflow runs Go suites on
Linux and macOS with named host exclusions/quarantine; the static Linux job
cross-builds Windows and vets Windows-only test compilation. There is no
Windows runtime runner. Individual test-local skips still apply, so package
success alone does not prove every subprocess fixture executed. These prior
runs do not include this new soak file or establish the eventual integrated
tree's exact CI. Root owns that receipt and the complete parent-step decision.

## Uncovered acceptance

The soak uses Darwin/arm64 locally. Its Go-only provider and HTTP/SQLite code
has no OS-specific exclusions, but this task does not claim it ran on Linux
or Windows. It does not exercise installed Claude/Codex, ACP process lifetime
for ten minutes, live Bots, a continuously streaming native prompt, physical
media, production Cloud/relay/network failure, multi-device UI recovery,
multi-day endurance, maximum-concurrency load, actual process RSS/CPU growth,
or power-loss durability. Provider calls and durable row counts are scoped
measurements; OS resource usage is explicitly unobserved. Virtual-clock
routine tests elsewhere are not counted as real elapsed endurance.

There are zero native-vendor prompts and zero paid API calls in this slice.
No live Bot runtime, existing application state, credentials, user settings,
deployment, Release, or Board item was modified. Only the task protocol uses
the owning orchestrator. Existing [native acceptance](project-assistants-native-acceptance.md),
[recovery](project-assistants-resilience.md), and
[ACP/failure classification](project-assistants-fallback.md) retain their own
limitations. Passing this slice does not accept the entire parent step.
