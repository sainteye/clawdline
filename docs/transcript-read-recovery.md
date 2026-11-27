# Conversation read recovery

The console previously skipped its quiet period for every `RefusalError`, including temporary
Cloud reconnects. A successful HTTP page with `evidence: "none"` and a diagnostic note also
appeared as an immediate error and could replace a previously readable page. Separately,
`StatusCloudClient` rejected a pending pinned read when a delayed reply named a different request
sequence. None of these conditions proves that the conversation was deleted.

Recovery now has three visible stages: ordinary loading, a polite notice after four seconds,
and an error after at least three consecutive temporary failures spanning twelve seconds.
Failure duration is sampled when an attempt fails. A repaint or the next request starting does
not advance that evidence: a request that succeeds after the quiet period must never flash an
error just before its answer appears. An explicit retry temporarily replaces an established
error with the neutral notice. Success clears the failure history. Authority and execution
identity refusals remain immediate; no pairing or generation checks are relaxed.

An unavailable page becomes a typed local `transcript_record_unavailable` failure before it
can replace held data. Delayed pinned replies are discarded before snapshot projection and
leave the exact pending request under its original timeout. The existing poller remains
single-flight, pauses while hidden and retains its two-second exponential failure backoff,
capped at thirty seconds, for a Session's main transcript; an agent (subagent) transcript view
polls without backoff and waits its ordinary interval after a failure. No new retry loop, queue capacity or transport deadline is added.
The Cloud read lane remains separately bounded at eight workers and sixty-four waiting reads;
queue saturation is a typed refusal, not evidence of an empty conversation.

The three presentation thresholds also have diagnostic measurement entries. Their live browser
timers and failure counts remain explicitly unknown to the daemon, with an explanation; absence
of browser evidence is neither zero occupancy nor an exhausted limit. The HTTP measurement
coverage and unknown-occupancy tests exercise this seam in addition to capacity registration.

## Rerunnable checks

- `node --experimental-strip-types --test web/console/src/session/transcript-trouble.test.ts web/console/src/cloud/status-client.test.mjs`
  exercises temporary and permanent refusals, unavailable pages, held content, and delayed
  pinned replies followed by the correct reply. Before the change, six assertions failed.
- Build the local console with `npm run build` in `web`, then run
  `node --experimental-strip-types --test --test-name-pattern='transcript recovery' web/console/src/session/history-paging.e2e.ts`.
  The real React screen at phone width covers a slow first response, three reconnect failures
  followed by success, persistent failure, and manual retry. A mutation observer records even
  a brief error flash. The reconnect fixture exposed the repaint-time escalation before it
  was fixed. `STATUS_SHOTS` optionally retains screenshots.
- `go test ./internal/domain/capacity -run 'TestTranscriptReadRecoveryIsRegistered|TestEveryBoundIsRegistered|TestTheRegisterMatchesItsSnapshot'`
  verifies the presentation thresholds against their capacity registrations. The registration
  test was run failing before the three entries were added.

These are local failure-injection checks. They do not establish a production failure-rate
reduction or the cause of any one screenshot without its diagnostic code. Retained viewer
events have partial coverage and no complete successful-read denominator; counts alone cannot
establish that the underlying failure rate has recently increased.
