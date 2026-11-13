# Browser setup and close result recovery

The browser setup guide observes the dedicated native host before the person
presses Finish. This readiness-only poll carries no commands, does not select a
profile and cannot grant Agent or viewer input access. Operational status remains
disconnected while access is disabled. The setup read reports transport readiness
separately; the existing final settings write remains the consent boundary.
Readiness expires using the normal host freshness interval. A daily Chrome host
cannot use this dedicated setup probe.

An emergency stop still admits the person's input after takeover. An empty host
poll waits for work, cancellation or the normal deadline, including while stopped.
Browser permission changes and person input wake an outstanding poll. Agent
admission and permission epochs remain unchanged.

A native host waits for an explicit successful result acknowledgment, including
when the HTTP request succeeds with a non-2xx response or its response is lost.
It retries the same result, never the browser operation. While a result is pending,
it pauses further polling instead of collecting an unbounded result backlog.
Closing Chrome cancels outstanding HTTP requests and retries. Admitted actions
retain their command IDs in the existing bounded audit, so a legacy host's result
retry without a mode can still identify its profile after the caller returns or
the daemon restarts. Unknown or ambiguous legacy IDs remain refused; a result
acknowledgment never repeats the browser action or restores stale page content.

Before posting a close result, the host atomically saves a minimal receipt in
`browser-close-results-<profile-hash>.json` in the daemon directory. The hash binds
it to the mode and absolute profile path. A kernel lock grants one host ownership
of each mode/profile before loading receipts
or polling, and stays held until that host ends. Another port waits without polling;
closing its port cancels that wait. The lock file stays in place, and process exit
releases ownership. The receipt file contains command IDs, tab IDs,
success flags and a generic failure classification, never page content, URLs,
input text or pictures. Its row and byte limits are registered in the capacity
inventory. A host restart replays these receipts until the daemon acknowledges
and then removes the file. Failed writes apply backpressure and retain the result
in memory; they do not claim durable recovery.

The daemon acknowledges close results only after its control-state save succeeds.
A failed save preserves the closing reservation and returns a typed failure, so
a retry can settle it. Duplicate receipts are safe. An actual missing-tab result
also removes stale ownership. Caller cancellation and timeout remain uncertain:
they never justify clearing a closing reservation or repeating the operation.

This is result recovery, not exactly-once browser execution. A process death
between Chrome's effect and the host saving its receipt can still leave an
uncertain operation. An inaccessible or corrupt receipt file requires recovery;
it must not be deleted to infer that the operation did not happen.

## Regression evidence

`browser_readiness_test.go` crosses the actual HTTP host and setup handlers with
default-off settings, verifies readiness, and proves Agent commands remain
refused. `review_regression_test.go` checks probe expiry, profile separation,
empty stopped polls and person input wakeups. The baseline source overlay fails
the setup and stopped-poll regressions; the fixed source passes.

`browser_results_test.go` uses the actual native host relay and a temporary
HTTP daemon. A close executes, its result POST receives 503, the bridge reloads
its persisted state, and the retained result reaches the new bridge with the
original command ID. Mode switching is then possible. Another test replays a
saved receipt after a host restart. `close_test.go` injects a control-state write
failure and proves that a receipt is not acknowledged before durable recovery.
All fixtures use synthetic tabs and isolated directories.

`TestOverlappingBrowserHostsKeepUnacknowledgedReceiptsAcrossRestart` keeps one
host's ACK pending while a second port waits, then disconnects the new owner
before its own result is acknowledged. A third host recovers that result without
repeating the operation. The process-lock regression also kills its own holder
and checks that the kernel releases ownership; an abandoned lock file is safe.
Unmeasured receipt occupancy remains unknown in diagnostics rather than appearing
as an empty queue.
