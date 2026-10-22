package capacity

// cloudBounds is every registered row named cloud.*. The cloud.health_*
// rows are declared in register_connection_health.go, and the
// cloud.health_store_* rows in register_healthjournal.go.
func cloudBounds() []Entry {
	return append(append(append(append(connectionHealthBounds(), healthJournalBounds()...), healthUploadBounds()...), healthSealedBounds()...), append(healthInvestigationBounds(), []Entry{
		{
			Name: "cloud.viewer_rekey_pending", Class: Buffer, Unit: Rows,
			Limit: 1, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			Name: "cloud.viewer_rekey_cooldown", Class: Cache, Unit: Seconds,
			Limit: 120, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			Name: "cloud.rekey_pending", Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/cloud.RawRekeyPendingLimit", "internal/adapters/cloud.(*Transport).dispatchRawRekey:chan(RawRekeyPendingLimit)"},
		},
		{
			Name: "cloud.rekey_interval", Class: Buffer, Unit: Seconds,
			Limit: 2, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.RawRekeyIntervalSecondsLimit"},
		},
		{
			Name: "cloud.envelope_key_id", Class: Buffer, Unit: Bytes,
			Limit: 128, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/cloud.MaxKeyIDBytes"},
		},
		{
			Name: "cloud.rekey_lifetime", Class: Cache, Unit: Seconds,
			Limit: 120, AtLimit: Expire, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/cloud.RekeyLifetimeMSLimit"},
		},
		{
			Name: "cloud.rekey_body", Class: Buffer, Unit: Bytes,
			Limit: 4096, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/cloud.MaxRekeyBodyBytes"},
		},
		{
			// A Cloud Session row (s/), status row (ss/) or status marker
			// whose only change is its activity time, its sub-agents or its
			// line's token count is re-sent at most once per this interval;
			// the next pass after it carries the newest value. Any other
			// change goes at once and starts that row's window again. Each
			// row has its own window: a status row is published only when it
			// changed, never because another row in the set did. The local
			// event stream is not held.
			Name: CloudSessionRowVolatile, Class: Cache, Unit: Seconds,
			Limit: 15, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.SessionRowVolatileSecondsLimit"},
		},
		{
			// The task list the machine descriptor carries is read when a task
			// row was written, when a viewer holding nothing has been heard
			// from, and otherwise no less often than this. It is the
			// descriptor's own heartbeat, so the pass that re-reads is the
			// pass that re-states; before the signal existed the list was read
			// on every five-second pass (288 reads in 1,426 seconds on the
			// running daemon, 2026-10-10, with no task changing).
			Name: CloudTaskListReread, Class: Cache, Unit: Seconds,
			Limit: 240, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.TaskListRereadSecondsLimit"},
		},
		{
			// Locally pinned machine-to-machine pairs are authorization evidence.
			// No account roster fallback or automatic eviction may free a slot.
			Name: CloudPeerPairs, Class: Evidence, Unit: Rows,
			Limit: 128, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/peerstore.PairLimit"},
		},
		{
			// A target's exact Session scope grants remain until a person removes
			// them; expiry and revocation deny without erasing the evidence.
			Name: CloudPeerGrants, Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/peerstore.GrantLimit"},
		},
		{
			// A received Agent message or handoff is retained as evidence.
			Name: CloudPeerInbox, Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/peerstore.InboxLimit"},
		},
		{
			// One encrypted content read carries one message and its receipt.
			Name: CloudPeerInboxPage, Class: Buffer, Unit: Rows,
			Limit: 1, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/peerstore.InboxPageLimit"},
		},
		{
			Name: CloudPeerBody, Class: Buffer, Unit: Bytes,
			Limit: 512 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/domain/agenthandoff.MaxBodyBytes"},
		},
		{
			// One batch of a hosted page's own failure rows. Larger is
			// refused `viewer_events_too_large`; the page drops it, counts
			// it, and sends the rows behind it.
			Name: CloudViewerEventsBatch, Class: Buffer, Unit: Bytes,
			Limit: 256 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app/cloudops.ViewerEventsBatchBytesLimit"},
		},
		{
			// Rows of one batch written as a log line each; the rest are one
			// count on the batch's summary line.
			Name: CloudViewerEventsRows, Class: Buffer, Unit: Rows,
			Limit: 128, AtLimit: Coalesce,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app/cloudops.ViewerEventsLoggedRowsLimit"},
		},
		{
			// The Session id a `sessions.row` read names. Longer is refused
			// `malformed_read`; no id this machine issues comes near it.
			Name: CloudSessionRowID, Class: Buffer, Unit: Bytes,
			Limit: 256, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app/cloudops.SessionRowIDBytesLimit"},
		},
		{
			// `sessions.row` asks waiting for the publisher's loop. Past it
			// the sender is answered 429 cloud_read_busy and asks again.
			Name: CloudSessionRowAsks, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.SessionRowAsksLimit",
				"internal/transport/cloud.(*Publisher).open:chan(SessionRowAsksLimit)"},
		},
		{
			// One reported row's log line; a longer one is cut and says so.
			Name: CloudViewerEventsLine, Class: Buffer, Unit: Bytes,
			Limit: 1024, AtLimit: Coalesce,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app/cloudops.ViewerEventsLineBytesLimit"},
		},
		{
			// Delivered batch ids remembered so a resent batch is not
			// written twice. The oldest is forgotten first; a batch resent
			// after that is written again, never lost.
			Name: CloudViewerEventsSeen, Class: Cache, Unit: Rows,
			Limit: 256, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app/cloudops.ViewerEventsSeenBatchesLimit"},
		},
		{
			Name: CloudPeerFrame, Class: Buffer, Unit: Bytes,
			Limit: 1 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Log, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/cloud.PeerFrameBytesLimit"},
		},
		{
			// A source keeps the request and relay evidence for later checks.
			Name: CloudPeerOutbox, Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/peerstore.OutboxLimit"},
		},

		{
			Name: "cloud.peer_discovery_reply", Class: Buffer, Unit: Rows,
			Limit: 1, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.DiscoveryReplyLimit",
				"internal/transport/cloud.(*Link).InspectPeer:chan(DiscoveryReplyLimit)"},
		},
		{
			// Peer frames, starts and source probes wait off the socket reader
			// for fresh authorization on their separate bounded workers.
			Name: CloudPeerIngress, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Log, Sender},
			EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.PeerIngressLimit",
				"internal/transport/cloud.PeerAckIngressLimit",
				"internal/transport/cloud.PeerTaskStartIngressLimit",
				"internal/transport/cloud.PeerProbeIngressLimit",
				"internal/transport/cloud.(*Link).runOnce:chan(PeerIngressLimit)",
				"internal/transport/cloud.(*Link).runOnce:chan(PeerAckIngressLimit)",
				"internal/transport/cloud.(*Link).runOnce:chan(PeerTaskStartIngressLimit)",
				"internal/transport/cloud.(*Link).runOnce:chan(PeerProbeIngressLimit)"},
		},
		{
			// Busy replies must not synchronously hold the request dispatcher.
			// One worker sends them while this FIFO bounds what may wait; an
			// unsent reply is counted and logged instead of spawning a goroutine.
			Name: CloudLaneRefusals, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Log}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudLaneRefusalsLimit",
				"internal/transport/cloud.(*Link).runService:chan(refusalDepth)"},
		},
		{
			// Eight workers execute reads while a separate FIFO absorbs finite
			// viewer bursts. A full worker pool does not refuse an ordinary read;
			// a full waiting queue answers the new request, by name, with 429
			// cloud_ingress_busy and retry_after. Commands never wait on reads.
			Name: CloudReadQueue, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Log, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudReadQueueLimit",
				"internal/transport/cloud.(*Link).runService:chan(readDepth)"},
		},
		{
			// Decrypted Cloud requests waiting for the bridge (limits N20).
			// At the limit the new request is refused and nothing already
			// taken is let go: the sender is answered 429 cloud_ingress_busy
			// with a retry_after on the channel it is waiting on (C3). Only a
			// refusal that could not itself be sent reaches nobody, and that
			// is counted as dropped.
			Name: CloudRelayQueue, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, CloudStatus, Log, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/cloud.(*Relay).start:chan(r.depth())"},
		},
		{
			// A replacement line waits for its old connection and schedule
			// poller to stop. A timeout refuses the replacement to avoid overlap.
			Name: CloudReloadStopSeconds, Class: Buffer, Unit: Seconds,
			Limit: 15, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Log}, EvictedBy: Daemon,
			Sources: []string{"cmd/clawdline.startCloudLine.maxCloudReloadStopSeconds"},
		},
		{
			// The Cloud line's outbound spool (limits N22): answers and
			// snapshots sealed for the relay and not yet answered by it. At
			// the limit a new answer is refused and nothing already queued is
			// let go — a queued row is somebody's answer — and the refusal is
			// counted here rather than only logged. So are the rows the spool
			// burns: an older snapshot replaced by a newer one (coalesced), a
			// row too old to send (dropped), and a row written and never
			// answered within its window, whose delivery is unknown (in the
			// note: unknown is neither sent nor dropped).
			//
			// **Answered rows are not in this figure.** They were, and both
			// this row and the byte row below therefore measured a population
			// no reservation is really competing for; the receipts have a row
			// of their own now (CloudSpoolReceipts).
			Name: CloudSpool, Class: Buffer, Unit: Rows,
			Limit: 2_000, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Log},
			EvictedBy: Daemon,
		},
		{
			// The same spool's bytes, its second bound: whichever of the two
			// a new answer would cross refuses it.
			Name: CloudSpoolBytes, Class: Buffer, Unit: Bytes,
			Limit: 16 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Log},
			EvictedBy: Daemon,
		},
		{
			// What one wire channel may have owed to it at once, and the
			// bound that refuses a Cloud answer in practice: measured on
			// 2026-09-21, one session's transcript channel was refused 34
			// times over while the two rows above stood at 401/2,000 and
			// 3.09 MB/16 MiB.
			//
			// **Four mebibytes, and where the four comes from.** One channel
			// has to hold the largest single answer that may legally be
			// published on it, or that answer can never be delivered at all;
			// the largest this daemon builds outside a picture is a document,
			// bounded at 2 MiB by internal/adapters/documents.MaximumBytes.
			// It has to hold a second one behind it, because the first is
			// still waiting for its receipt while the next read is answered.
			// Two of them is 4 MiB, which is also exactly the spool's
			// in-flight window (OutboundWindowByteCap): past it a channel
			// would be queueing more than the line can carry, which is a
			// backlog to refuse rather than to keep. For scale, the answers
			// measured on this machine the same day were 151,932 bytes for a
			// full transcript page and 362,457 bytes for the largest of 35
			// project boards.
			//
			// A refusal is admitted past this cap under a reserve of its own
			// (internal/adapters/cloud.spoolRefusalByteLimit), because a
			// channel that cannot say it is full is the silence this row
			// exists to end.
			Name: CloudSpoolChannelBytes, Class: Buffer, Unit: Bytes,
			Limit: 4 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Log},
			EvictedBy: Daemon,
		},
		{
			// The spool's tombstones: a row that has been answered, kept for
			// ten minutes so that a second receipt for the same sequence is
			// answered "late, ignored" rather than "no such row" — which is
			// what a receipt for a sequence this machine never sent must
			// mean, and only that.
			//
			// It is an idempotency table and not a queue, which is the whole
			// point of separating it: the payload went at Settle, so a
			// receipt holds no delivery capacity and must not be charged as
			// though it did. Past the cap the oldest receipt expires early
			// and is counted, because from then on a late ack for that
			// sequence reads as a correlation failure.
			Name: CloudSpoolReceipts, Class: Idempotency, Unit: Rows,
			Limit: 4_096, AtLimit: Expire,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
		},
		{
			// The refusals one wire channel may have owed at once, each at
			// most internal/adapters/cloud.spoolRefusalByteLimit and admitted
			// past that channel's own byte cap. Until 2026-09-25 the rule was
			// one: every carried read waits on its own read id, so the second
			// refused read on a channel got no answer at all and its browser
			// waited out its sixty seconds ("did not fit its channel and the
			// refusal did not either"). Sixty-four is a whole Board's worth
			// of reference images refused at once, and 64 × 4 KiB is 256 KiB
			// a channel at the very worst. Past it a refusal is dropped and
			// the drop is logged with the read's operation.
			Name: CloudSpoolRefusals, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
		},
		{
			// A roster read this machine made stands behind an answer for
			// this many seconds. The next terminal operation or Session
			// content read waits for one bounded refresh or is refused; one
			// read serves both lanes, and a revocation takes effect at most
			// this long after the account records it. The ordinary Cloud
			// command path keeps the 60 s adaptercloud.RosterRefresh.
			Name: CloudTerminalRosterRefresh, Class: Cache, Unit: Seconds,
			Limit: 2, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalRosterRefreshLimit"},
		},
		{
			// A terminal roster request past this deadline fails closed.
			Name: CloudTerminalRosterDeadline, Class: Cache, Unit: Seconds,
			Limit: 2, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalRosterDeadlineLimit"},
		},
		{
			// A short authority read may recover one stalled network path
			// through a fresh connection within the same two-second deadline.
			// Further failures keep content and terminal access closed.
			Name: CloudRosterShortReadAttempts, Class: Buffer, Unit: Rows,
			Limit: 2, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/cloud.RosterShortReadAttemptsLimit"},
		},
		{
			// A failed terminal roster read is read again on the first check
			// this long after it failed; until then terminal rights stay
			// closed as unverified (terminal_busy), never revoked.
			Name: CloudTerminalRosterRetry, Class: Cache, Unit: Seconds,
			Limit: 1, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalRosterRetrySecondsLimit"},
		},
		{
			// A registered connection whose viewer's authority cannot be
			// verified is paused (no frames, input refused as busy) and retired
			// without a revocation notice after this long.
			Name: CloudTerminalUnverifiedRetire, Class: Cache, Unit: Seconds,
			Limit: 10, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalUnverifiedRetireSecondsLimit"},
		},
		{
			Name: CloudTerminalConnections, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalConnectionsLimit"},
		},
		{
			Name: CloudTerminalViewerConnections, Class: Buffer, Unit: Rows,
			Limit: 2, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalViewerConnectionsLimit"},
		},
		{
			Name: CloudTerminalRequestBytes, Class: Buffer, Unit: Bytes,
			Limit: 6<<20 + 4<<10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalRequestBytesLimit"},
		},
		{
			Name: CloudTerminalHistoryReceipt, Class: Buffer, Unit: Bytes,
			Limit: 8 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalHistoryReceiptBytesLimit"},
		},
		{
			Name: CloudTerminalHistoryLine, Class: Buffer, Unit: Bytes,
			Limit: 4 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalHistoryLineBytesLimit"},
		},
		{
			Name: CloudTerminalHistoryCapture, Class: Buffer, Unit: Bytes,
			Limit: 4 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalHistoryCaptureBytesLimit"},
		},
		{
			// Receipts kept per connection for a re-sent request id; each goes after CloudTerminalReceiptSeconds, and at the limit the oldest goes early so the new request is answered.
			Name: CloudTerminalReceipts, Class: Idempotency, Unit: Rows,
			Limit: 512, AtLimit: EvictOldest,
			Deviation: "limits §4.2 has an idempotency row refuse new requests rather than evict inside its window. Decided 2026-10-07: refusing here refused every key of a key held down past 12.8 s (40 a second against 512 in 15 s), while the bound exists only for memory and the viewer never resends a request id itself (cloud-terminal-wire.md); the oldest receipt, the least likely to be asked for again, goes instead, counted as Evicted and logged as stage receipt_evicted.",
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/cloud.CloudTerminalReceiptsLimit"},
		},
		{
			Name: CloudTerminalKeySeconds, Class: Cache, Unit: Seconds,
			Limit: 600, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalKeySecondsLimit"},
		},
		{
			Name: CloudTerminalIngress, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalIngressLimit", "internal/transport/cloud.(*Link).runOnce:chan(CloudTerminalIngressLimit)"},
		},
		{
			Name: CloudTerminalListIngress, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalListIngressLimit", "internal/transport/cloud.(*Link).runOnce:chan(CloudTerminalListIngressLimit)"},
		},
		{
			Name: CloudTerminalRefusals, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Log}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalRefusalsLimit", "internal/transport/cloud.(*Link).runOnce:chan(CloudTerminalRefusalsLimit)"},
		},
		{
			Name: CloudTerminalRevocationRetire, Class: Cache, Unit: Seconds,
			Limit: 3, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalRevocationRetireSecondsLimit"},
		},
		{
			Name: CloudTerminalFrameHeartbeat, Class: Cache, Unit: Seconds,
			Limit: 3, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalFrameHeartbeatSecondsLimit"},
		},
		{
			// A browser keeps only the newest verified complete frame that
			// arrives before its terminal ID is known. This is a Console bound;
			// daemon diagnostics publish the policy but cannot measure tab usage.
			Name: CloudTerminalEarlyFrames, Class: Buffer, Unit: Rows,
			Limit: 1, AtLimit: Coalesce,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// A browser page keeps its newest 128 content-free terminal
			// stages, evicting the oldest, and pins its first 16 failure-like
			// stages so a report shows both the start of a break and the
			// latest state. Its console, and a daemon connection's log, write
			// the first 128 stages of each kind in full and then sample them
			// (every 64th routine stage, every 16th failure stage), so neither
			// goes silent and neither grows without bound. The daemon
			// publishes the policy but cannot measure page usage.
			Name: CloudTerminalObservationRows, Class: Observation, Unit: Rows,
			Limit: 128, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalObservationRowsLimit"},
		},
		{
			// A browser retries a rate-limited terminal list read once after
			// the relay's short authority grant renews. It never retries input.
			Name: CloudTerminalListRetry, Class: Buffer, Unit: Rows,
			Limit: 1, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// A Console tab retains only content-free Session header read
			// stages. The daemon publishes the browser policy but cannot inspect
			// another device's sessionStorage.
			Name: CloudHeaderReadDiagnosticRows, Class: Observation, Unit: Rows,
			Limit: 24, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			Name: CloudHeaderReadDiagnosticAge, Class: Cache, Unit: Seconds,
			Limit: 3600, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			Name: CloudTerminalUnconfirmed, Class: Cache, Unit: Seconds,
			Limit: 15, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalUnconfirmedSecondsLimit"},
		},
		{
			// A machine holds at most this many viewers' direct data channels; a further offer is refused `terminal_busy` and that viewer stays on the relay.
			Name: CloudTerminalDirectPeers, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectPeersLimit"},
		},
		{
			// Direct offers one viewer may make in a minute; more are refused `terminal_busy`. It also bounds the connectivity checks an offer can point this machine at.
			Name: CloudTerminalDirectOffers, Class: Buffer, Unit: Rows,
			Limit: 6, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectOffersPerMinuteLimit"},
		},
		{
			// One opened offer SDP; larger is refused `terminal_invalid` before it is parsed.
			Name: CloudTerminalDirectSDP, Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectSDPBytesLimit"},
		},
		{
			// Candidate lines in one offer; more are refused `terminal_invalid`.
			Name: CloudTerminalDirectCandidates, Class: Buffer, Unit: Rows,
			Limit: 32, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectCandidatesLimit"},
		},
		{
			// From the answer to an open data channel; a peer that has not opened by then is closed and the terminal stays on the relay.
			Name: CloudTerminalDirectNegotiate, Class: Cache, Unit: Seconds,
			Limit: 5, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectNegotiateSecondsLimit"},
		},
		{
			// How long the machine gathers its own candidates before answering with those it has.
			Name: CloudTerminalDirectGather, Class: Cache, Unit: Seconds,
			Limit: 3, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectGatherSecondsLimit"},
		},
		{
			// One reassembled data-channel message; a larger one closes the channel, and the terminal falls back to the relay.
			Name: CloudTerminalDirectMessage, Class: Buffer, Unit: Bytes,
			Limit: 9 << 20, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectMessageBytesLimit"},
		},
		{
			// A partly received chunked message held longer than this closes the channel.
			Name: CloudTerminalDirectChunk, Class: Buffer, Unit: Seconds,
			Limit: 2, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectChunkSecondsLimit"},
		},
		{
			// A direct frame not acknowledged by the viewer in this time retires the connection.
			Name: CloudTerminalDirectAck, Class: Cache, Unit: Seconds,
			Limit: 3, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectAckSecondsLimit"},
		},
		{
			// Interval between relay probes of one direct connection; one is in flight at a time.
			Name: CloudTerminalDirectProbe, Class: Cache, Unit: Seconds,
			Limit: 1, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectProbeSecondsLimit"},
		},
		{
			// A relay probe unsettled for this long retires the direct connection, so a relay that stops answering cannot leave a direct terminal unchecked.
			Name: CloudTerminalDirectProbeUnsettled, Class: Cache, Unit: Seconds,
			Limit: 2, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectProbeUnsettledSecondsLimit"},
		},
		{
			// Every Cloud terminal connection is re-checked this often: authority, deadlines, and a direct connection's ack and probe.
			Name: CloudTerminalSweep, Class: Cache, Unit: Seconds,
			Limit: 1, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalSweepSecondsLimit"},
		},
		{
			// A receipt the relay refused with rate_limited (its account terminal budget was spent)
			// is published again this many times before the connection is retired.
			Name: CloudTerminalReceiptBusyRetries, Class: Buffer, Unit: Rows,
			Limit: 3, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalReceiptBusyRetriesLimit"},
		},
		{
			// How long a terminal receipt is kept for its request id: past the viewer's ten-second wait, after which it never sends that id again.
			Name: CloudTerminalReceiptSeconds, Class: Idempotency, Unit: Seconds,
			Limit: 15, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalReceiptSecondsLimit"},
		},
		{
			// The wait before such a receipt is published again; the relay refreshes the budget every two seconds.
			Name: CloudTerminalReceiptBusyRetry, Class: Cache, Unit: Seconds,
			Limit: 2, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalReceiptBusyRetrySecondsLimit"},
		},
	}...)...)
}
