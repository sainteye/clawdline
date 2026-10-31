package capacity

// consoleBounds is every registered row named console.*.
func consoleBounds() []Entry {
	return append(append(append(append(append(append(publicAnalyticsUIBounds(), connectionHealthUIBounds()...), servicePerformanceUIBounds()...), supportUIBounds()...), viewerHealthBounds()...), ownerRetentionBounds()...), []Entry{
		{
			Name: "console.video_preview_frames", Class: Buffer, Unit: Rows,
			Limit: MaximumVideoPreviewFrames, AtLimit: Refuse,
			Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumVideoPreviewFrames"},
		},
		{
			Name: "console.video_preview_reads", Class: Buffer, Unit: Rows,
			Limit: MaximumVideoPreviewReads, AtLimit: Refuse,
			Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumVideoPreviewReads"},
		},
		{
			Name: "console.video_preview_side", Class: Buffer, Unit: Rows,
			Limit: MaximumVideoPreviewSide, AtLimit: Refuse,
			Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumVideoPreviewSide"},
		},
		{
			Name: "console.video_preview_bytes", Class: Buffer, Unit: Bytes,
			Limit: MaximumVideoPreviewBytes, AtLimit: Refuse,
			Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumVideoPreviewBytes"},
		},
		{
			Name: "console.video_preview_milliseconds", Class: Buffer, Unit: Milliseconds,
			Limit: MaximumVideoPreviewMilliseconds, AtLimit: Refuse,
			Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumVideoPreviewMilliseconds"},
		},
		{
			// NextMessages.add shares KEPT_CARDS from session/persist.ts.
			Name: "console.next_messages", Class: Buffer, Unit: Rows,
			Limit: 20, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person,
		},
		{
			// readOlderRecovering in session/transcript-follow.ts clears stale
			// held history, reads newest once, and retries older once per request.
			// A second refusal remains visible for manual recovery.
			Name: "console.transcript_revision_restarts", Class: Buffer, Unit: Rows,
			Limit: 1, AtLimit: Refuse,
			Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
		},
		{
			// AssistantFactsRequest uses ANTIGRAVITY_STATE_FRESH_MS in
			// session/antigravity-ui.ts for a mounted view's inventory read.
			// Expiry aborts the request and displays an unavailable state.
			Name: "console.assistant_facts_read_milliseconds", Class: Buffer, Unit: Milliseconds,
			Limit: 15000, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
		},
		{
			// Encoded DataChannel answers waiting for authentication in wire order.
			// Shared by this browser's machines; overflow closes only the affected carrier.
			Name: "console.carrier_receive_bytes", Class: Buffer, Unit: Bytes,
			Limit: 32 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
		},
		{
			Name: "console.carrier_receive_rows", Class: Buffer, Unit: Rows,
			Limit: 32, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
		},
		{
			// HISTORY_FILL_PAGES in session/history-fill.ts bounds automatic
			// older reads while the transcript does not fill its viewport.
			// Polling and resize do not reset it; a manual request does.
			Name: ConsoleTranscriptFillPages, Class: Buffer, Unit: Rows,
			Limit: 32, AtLimit: Refuse,
			Told: []Channel{Sender, Diagnostics}, EvictedBy: Person,
		},
		{
			// Both opened-detail readers share one automatic recovery per
			// user opening (SessionExecutionRecovery). At the limit the
			// existing failure and manual recovery action remain visible.
			Name: ConsoleExecutionRecoveryAttempts, Class: Buffer, Unit: Rows,
			Limit: 1, AtLimit: Refuse,
			Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
		},
		{
			// The transcript pane reads the whole newest page at least this
			// often (SAFETY_MS); between, it reads only what was appended, when
			// the session row says the record grew. Past it the held page
			// expires and is read whole: that read is what catches a row that
			// said nothing and refreshes pictures an appended read never
			// revisits.
			Name: ConsoleTranscriptSafetySeconds, Class: Cache, Unit: Seconds,
			Limit: 30, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// After a send, the pane reads on its follow pace (two seconds,
			// then four) for at most this long (FOLLOW_WINDOW_MS), then goes
			// back to the safety pace. The
			// card's own lifetime is session/pending.ts's and unchanged.
			Name: ConsoleTranscriptFollowSeconds, Class: Cache, Unit: Seconds,
			Limit: 90, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// The entries one held page may grow to by appended reads
			// (MERGED_ROWS_LIMIT), the route's own ceiling for a page. Past it
			// the next read is a whole one, which starts again at 200.
			Name: ConsoleTranscriptMergedRows, Class: Cache, Unit: Rows,
			Limit: 1000, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Hours of direct-attempt counts one browser keeps per machine
			// (HISTORY_HOURS, web/console/src/cloud/direct-history.ts). An hour
			// older than the window is dropped on the next write.
			Name: ConsoleDirectHistoryHours, Class: Observation, Unit: Rows,
			Limit: 24, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Machines one browser keeps a direct record for (HISTORY_MACHINES).
			// Past it, the machine whose newest hour is oldest goes.
			Name: ConsoleDirectHistoryMachines, Class: Observation, Unit: Rows,
			Limit: 32, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// The open machine list re-reads the account's machines at this
			// pace (MACHINE_LIST_POLL_MS, pages/devices/AccountMachines.tsx),
			// because which road a machine's reads take changes with no event.
			Name: ConsoleMachineListPoll, Class: Cache, Unit: Seconds,
			Limit: 5, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// A failed transcript read waits 2, 4, 8 … seconds, at most this
			// long (BACKOFF.maxMs), before the next.
			Name: ConsoleTranscriptBackoffSeconds, Class: Cache, Unit: Seconds,
			Limit: 30, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// For 45 seconds after a write the Cloud seam asks the machine for
			// the transcript again when the row moved, and otherwise at most
			// this often (TRANSCRIPT_EXPECT_REREAD_MS); its held answer expires
			// at this age.
			Name: ConsoleRelayTranscriptExpectReask, Class: Cache, Unit: Seconds,
			Limit: 4, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// How often, at most, a page asks the machine to restate its rows
			// because the Session a person has open is not among them
			// (ROW_RESTATE_MS, web/console/src/cloud/relay-reader.ts).
			//
			// A Session's rich row is published when the machine has
			// something to say about it and restated on the Cloud status
			// cadence, so a page that does not hold the opened one has
			// nothing to draw it with. Measured on 2026-10-11: re-reading one
			// from the relay's retained channel took 19.6 seconds, against
			// 131 ms for the conversation itself; asking the machine put the
			// row back in one second.
			Name: ConsoleOpenedRowRestate, Class: Cache, Unit: Seconds,
			Limit: 5, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// How long the opened Session's row may be missing before the
			// pane stops saying it is reading and says the machine has not
			// sent it (OPENED_ROW_QUIET_MS,
			// web/console/src/cloud/opened-row.ts). Longer than the ask and
			// its answer, twice over.
			Name: ConsoleOpenedRowQuiet, Class: Cache, Unit: Seconds,
			Limit: 8, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// How long a read waits for the chosen machine's own list of
			// words before it is asked or refused (FEATURE_WAIT_MS,
			// web/console/src/cloud/relay-reader.ts).
			//
			// A page that has just opened decides a word from the descriptor
			// it remembered last time, whose command list the copied client
			// cut to 64 words while this daemon publishes 151, so a read of a
			// word past the cut refused itself before anything was sent: 11 of
			// 21 read failures on this machine between 11:30 and 14:22 on
			// 2026-10-10, every one of them `transcript` or
			// `work.v2.session-todos`. This is how long such a read may wait
			// for the live descriptor instead; past it the cut list cannot
			// refuse the read, so the machine's signed answer decides.
			Name: ConsoleRelayFeatureWait, Class: Cache, Unit: Seconds,
			Limit: 5, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// A browser retries a missing retained ss/ row after a recovery
			// was refused or timed out (STATUS_GAP_RETRY_MS,
			// web/console/src/cloud/status-source.ts). A failed attempt must
			// not pin the list in event_gap for the rest of the status pass.
			Name: ConsoleStatusGapRetry, Class: Cache, Unit: Seconds,
			Limit: 5, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// A transient failure reading one machine's role catalog gets one
			// retry per machine (RETRY_MS, web/console/src/personas.ts). A
			// phone returning from a Cloud connection gap must not keep an
			// empty catalog, and therefore hide Session roles, indefinitely.
			Name: ConsolePersonaRetry, Class: Cache, Unit: Seconds,
			Limit: 5, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// A cold browser replays one missing ss/ inventory marker, then
			// waits before asking again while default status pushes may arrive.
			// The same interval revalidates a successful pinned machine list used
			// while a cold browser still has an ss/ row gap. A failed list starts
			// at ConsoleStatusGapRetry and backs off to this bound.
			Name: ConsoleStatusMarkerRetry, Class: Cache, Unit: Seconds,
			Limit: 60, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// One pinned machine list read yields after ten seconds of
			// silence (PINNED_LIST_READ_TIMEOUT_MS, status-client.js).
			Name: ConsolePinnedListReadTimeout, Class: Cache, Unit: Seconds,
			Limit: 10, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// The macOS shell's one save of the permission the person chose
			// for a pairing (shell/darwin/Pairing.swift
			// PairingPermissionClient.saveTimeoutSeconds). At the limit the
			// shell abandons the request, shows no code and asks the person
			// to retry or ignore; native_pairing_test.go holds the two sides
			// together.
			Name: ConsoleNativePairingSaveSeconds, Class: Cache, Unit: Seconds,
			Limit: 15, AtLimit: Expire,
			Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
		},
		{
			// One retained marker replay yields after five seconds of
			// silence (STATUS_MARKER_RECOVERY_TIMEOUT_MS, status-client.js).
			Name: ConsoleStatusMarkerRecoveryWait, Class: Cache, Unit: Seconds,
			Limit: 5, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// After a send the pane reads every two seconds for this long
			// (FOLLOW_FAST_MS), then every FOLLOW_LATER_MS until the follow
			// window ends.
			Name: ConsoleTranscriptFollowFast, Class: Cache, Unit: Seconds,
			Limit: 10, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// The rest of the follow window after a send is read this often
			// (FOLLOW_LATER_MS).
			Name: ConsoleTranscriptFollowLater, Class: Cache, Unit: Seconds,
			Limit: 4, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// While this machine's own event stream is open, the connection
			// light reads /v1/health this often (HEALTH_LIVE_MS) instead of
			// every 15 seconds; a dropped stream, or a Cloud page, keeps 15.
			Name: ConsoleHealthLive, Class: Cache, Unit: Seconds,
			Limit: 90, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// An open Session page's to-dos are read at least this often
			// (TODO_SAFETY_MS), and at once when the page is seen or focused.
			// Its attention notes are read on this pace only while their panel
			// is open. A row that carries `attention_count` says whether there
			// are any: none is read for a Session with none, a Session with
			// some reads them as the page opens so that opening the panel
			// shows words, and a count that moves on the stream is what asks
			// again (web/console/src/session/attention-reads.ts). A row
			// without the count keeps the faster TODO_REFRESH_MS it had.
			Name: ConsoleSessionNotesSafety, Class: Cache, Unit: Seconds,
			Limit: 60, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
	}...)
}
