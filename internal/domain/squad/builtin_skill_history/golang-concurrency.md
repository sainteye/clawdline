# Go concurrency design

Use this skill when a Go task changes goroutine lifetime, shared state, channels, cancellation, worker pools, or fan-out and fan-in. Read the existing call path first and state who owns each goroutine and who closes each channel.

1. Choose a mutex for shared state, a channel for ownership transfer or coordination, and atomics only for a simple independent value. Explain the choice in terms of the observed code.
2. Give every spawned goroutine a bounded exit path through cancellation, channel close, or a completed worker queue. Propagate errors and context cancellation to callers.
3. Check send and receive blocking, shutdown order, queue bounds, backpressure, duplicate work, and retries. For Clawdline delivery paths, distinguish accepted, executed, delivered, observed, and acknowledged states.
4. Test cancellation, error propagation, shutdown, and contention with deterministic failure injection where possible. Run the repository's required checks before committing.

This skill does not authorize new capacity bounds without the capacity registry or replace the repository's protocol and privacy rules.
