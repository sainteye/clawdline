# Bot work completion admits one continuation

One opted-in dispatched task can queue at most one durable Bot continuation. If the broker retries after the continuation was committed but before the outbox records success, the Bot must not receive a second follow-up turn.

## Code correspondence and bounds

Code correspondence last checked at `eab8fa2519b20e7835031422b99c3163ab2609c3`. The code links and the table below were brought up to date at `2139dcec0`; the model itself was not changed or re-run.

| Model step | Code |
| --- | --- |
| `Settle` | The broker settles a terminal task and emits the completion effect in [`broker.go`](../../../internal/app/orchestrator/broker.go). |
| `PublishResult` | [`completeWork`](../../../internal/adapters/projectassistanthost/work_completion.go) reconciles the worker result, then `completeLink` in the same file asks the runtime to queue a continuation only when the Bot's work review setting is on (`ReviewsWork()`) or the dispatch asked for one (`ContinueOnCompletion`). |
| `InvalidateOrigin` | [`checkWorkCompletion`](../../../internal/app/projectassistant/work_completion.go) refuses a stale session or a forgotten origin, and refuses an origin turn in any state other than running, waiting for permission, unknown or settled (succeeded, or failed with `ExecutionEffectUnknown`). A review behind a running, permission-waiting or unknown origin is queued behind it. The model has no ordering between origin completion and admission and collapses those changes into a single irreversible invalidation for this task. |
| `TryAdmission` | [`WakeWorkCompletion`](../../../internal/app/projectassistant/work_completion.go) uses the same task-derived send key on each attempt. [`Submit`](../../../internal/app/projectassistant/submit.go) checks for replay under the Bot actor lock. [`commitWith`](../../../internal/adapters/projectassistantstore/repository.go) also checks for replay and writes the queued turn and receipt in one transaction. The model treats that durable admission as a single action. |
| `FinishEffect` | The [completion effect handler](../../../internal/app/orchestrator/assistant_work_completion.go) returns after runtime admission or a permanent refusal. Recovery can retry while the effect remains pending. |

The configuration covers one Bot, one already-dispatched task, and two admission attempts. It assumes a stable task ID, the same result message on replay, and persistence of the receipt throughout recovery. A crash before a durable write leaves this model's state unchanged. A crash or lost acknowledgement after a durable admission is represented by allowing another attempt before the outbox finishes. The counter `queued` records admissions over the whole modeled history, including after invalidation.

The model has one producer, `Settle` → `PublishResult` → `TryAdmission`. The code has others that use the same task-derived idempotency key and are not modeled: the single review turn queued when a task first waits for a person (`EffectAssistantWorkWaiting`, emitted in `broker.go`), and a Root's delivery (`Host.rootDelivered` → `completeLink`). It also writes the receipt without queuing a turn in two ways the model does not represent: the chain-limit refusal (`refuseWorkCompletionChain`, bounded by `pa.MaxRoutineFollowUpChain`) and `RefuseWorkCompletion`, which the host calls on the last attempt when `AssistantWorkGivenUp` holds. In the model a permanent refusal needs `~originCurrent`; in code it can also come from a full Bot or the chain limit while the origin is still current.

This is a safety check of the model over those bounds. Provider calls, queue draining, receipt retention, races inside the database, and eventual delivery are outside its scope. Update the correspondence when covered code changes. Replay a counterexample against the Go path before treating it as a product defect.

## Run

Use Java and [TLC from TLA+ v1.7.4](https://github.com/tlaplus/tlaplus/releases/tag/v1.7.4). Set `TLA2TOOLS_JAR` to the absolute path of the released JAR. Its SHA-256 is `936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88`.

From the repository root:

```sh
(
  set -eu
  cd docs/formal-verification/bot-work-completion
  state_dir=$(mktemp -d)
  trap 'rm -rf "$state_dir"' EXIT
  java -XX:+UseParallelGC -cp "${TLA2TOOLS_JAR:?set this to the absolute path to tla2tools.jar}" \
    tlc2.TLC -workers 1 -metadir "$state_dir" BotWorkCompletion
)
```

TLC found 15 distinct reachable states with no invariant violation. For the negative control, copy the model and configuration into a scratch directory and change the assignment to `queued' = queued + 1` in `TryAdmission`. With the same configuration, TLC exited 12 and showed the counterexample: the broker settles the task, publishes its result, admits a continuation, then retries before recording completion and admits a second one.

To check the affected Go path from the repository root:

```sh
tools/check.sh --quick ./internal/adapters/projectassistanthost \
  --run='TestDispatchedWorkCompletionWakesBotOnce|TestCompletedWorkRespectsForgottenOrigin'
```

The first test injects a failed delivery, recovers the effect, replays completion, and asserts a single continuation and result. The second verifies that a forgotten origin keeps the result without starting another turn.
