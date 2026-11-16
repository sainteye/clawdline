# Bot integration: Linux test recovery on 2026-11-16

CI run [43797787008](https://github.com/sainteye/clawdline/actions/runs/43797787008),
on b0adb8cc7, failed two Linux tests. The push guard correctly stopped integration.

The shared-writer cleanup test kept its injected 30 ms deadline during healthy
SQLite recovery. A controlled writer held for 60 ms reproduced that failure.
Failure and fenced re-enable still use 30 ms; subsequent healthy recovery now
restores the fixture's original production budget. The focused test passed,
queued turns were cancelled and no execution factory initialized. Product
cleanup deadlines and unknown-cleanup refusal are unchanged.

The native background-task test could observe idle before its completed watcher
was detached. readBackground publishes idle before followBackground reacquires
the lock and calls stopWatch. The test now waits for both facts within the
existing three-second deadline, keeping both final assertions. The original
Linux failure is retained; its exact scheduling was not reproduced locally.
The focused test passed. No vendor CLI, native prompt or production Bot ran.

The integration commit must pass its ordinary gate; exact CI remains separate.

The concurrently integrated main commit `ccb9184ab` independently fixes both
fixture conditions. Integration keeps its watch test byte for byte and its
normal cleanup-budget restoration, adding the controlled 60 ms writer recovery
case from this branch. Both commit histories and original failures remain.
