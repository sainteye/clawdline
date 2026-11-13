package capacity

const (
	MaximumClosedSessionReceipts       = 512
	MaximumStartedSessionRetrySeconds  = 5
	MaximumStartedSessionFollowSeconds = 60
	MaximumStartedSessionReads         = 12
)

// consoleBounds is every registered row named console.*.
func consoleBounds() []Entry {
	return append(append(append(append(append(append(publicAnalyticsUIBounds(), connectionHealthUIBounds()...), servicePerformanceUIBounds()...), supportUIBounds()...), viewerHealthBounds()...), ownerRetentionBounds()...), []Entry{
		{Name: "console.copilot_session_report_batch", Class: Buffer, Unit: Rows, Limit: 3, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon},
		{Name: "console.copilot_session_report_refs", Class: Cache, Unit: Rows, Limit: 24, AtLimit: EvictOldest, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon},
		{Name: "console.copilot_session_report_sessions", Class: Buffer, Unit: Rows, Limit: 24, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person},
		{
			Name: ConsoleClosedSessionReceipts, Class: Cache, Unit: Rows,
			Limit: MaximumClosedSessionReceipts, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumClosedSessionReceipts"},
		},
		{
			Name: ConsoleStartedSessionRetry, Class: Cache, Unit: Seconds,
			Limit: MaximumStartedSessionRetrySeconds, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumStartedSessionRetrySeconds"},
		},
		{
			Name: ConsoleStartedSessionReads, Class: Buffer, Unit: Rows,
			Limit: MaximumStartedSessionReads, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumStartedSessionReads"},
		},
		{
			Name: ConsoleStartedSessionFollow, Class: Cache, Unit: Seconds,
			Limit: MaximumStartedSessionFollowSeconds, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumStartedSessionFollowSeconds"},
		},
		{
			Name: "console.voice_recording_bytes", Class: Buffer, Unit: Bytes,
			Limit: MaximumVoiceRecordingBytes, AtLimit: Refuse,
			Told: []Channel{Sender, Diagnostics}, EvictedBy: Person,
			Sources: []string{"internal/domain/capacity.MaximumVoiceRecordingBytes"},
		},
		{
			Name: "console.voice_stop_milliseconds", Class: Buffer, Unit: Milliseconds,
			Limit: MaximumVoiceStopMilliseconds, AtLimit: Refuse,
			Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumVoiceStopMilliseconds"},
		},
		{
			Name: "console.voice_prepare_milliseconds", Class: Buffer, Unit: Milliseconds,
			Limit: MaximumVoicePrepareMilliseconds, AtLimit: Refuse,
			Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumVoicePrepareMilliseconds"},
		},
		{
			Name: "console.voice_read_milliseconds", Class: Buffer, Unit: Milliseconds,
			Limit: MaximumVoiceReadMilliseconds, AtLimit: Refuse,
			Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumVoiceReadMilliseconds"},
		},
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
			// MEMORY_TURNS in realtime/copilot-memory.ts: the lines of earlier
			// voice copilot calls this browser keeps for the next call. The
			// oldest is dropped; none older than three days is kept.
			Name: "console.copilot_memory_turns", Class: UserInput, Unit: Rows,
			Limit: 24, AtLimit: EvictOldest, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
		},
		{
			// LIST_LATEST_READS in realtime/copilot-tools.ts: how many group
			// Sessions' newest words one Session list carries, one transcript
			// read each. Further Sessions are listed without them.
			Name: "console.copilot_list_transcript_reads", Class: Buffer, Unit: Rows,
			Limit: 6, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
		},
		{
			// HELP_SECTIONS in realtime/copilot-help.ts: how many user guide
			// sections one Clawdline help answer carries.
			Name: "console.copilot_help_sections", Class: Buffer, Unit: Rows,
			Limit: 3, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
		},
		{
			// HELP_SECTION_CHARS in realtime/copilot-help.ts: how much of one
			// guide section is carried; a longer one is cut at a line.
			Name: "console.copilot_help_section_characters", Class: Buffer, Unit: Characters,
			Limit: 2500, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// RECAP_PAGES in realtime/copilot-tools.ts: how far back a Session
			// recap pages; a longer Session is recapped from its recent part
			// and the answer says so.
			Name: "console.copilot_recap_pages", Class: Buffer, Unit: Rows,
			Limit: 5, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
		},
		{
			// REPLY_CHARS in realtime/copilot-tools.ts: how much of a Session's
			// last reply explain_session_reply hands the call; a longer reply
			// keeps its beginning and end and says it was shortened.
			Name: "console.copilot_reply_characters", Class: Buffer, Unit: Characters,
			Limit: 8000, AtLimit: Coalesce, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
		},
		{Name: "console.copilot_recent_idle_seconds", Class: Buffer, Unit: Seconds, Limit: 1800, AtLimit: Coalesce, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon},
		{Name: "console.copilot_idle_fallback_seconds", Class: Buffer, Unit: Seconds, Limit: 3600, AtLimit: Coalesce, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon},

		{
			// SMART_RENAME_SESSIONS in realtime/copilot-tools.ts: how many
			// Sessions one smart_rename_sessions call names, one small model
			// turn each; a longer list is refused and asked to be split.
			Name: "console.copilot_smart_rename_sessions", Class: Buffer, Unit: Rows,
			Limit: 6, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
		},
		{
			// UPDATE_PAGES in realtime/copilot-tools.ts: how many transcript
			// pages get_session_updates reads back to find where the last
			// report ended; further back, it reports the recent part and says so.
			Name: "console.copilot_update_pages", Class: Buffer, Unit: Rows,
			Limit: 3, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
		},
		{
			// UPDATE_MARKS in realtime/copilot-marks.ts: how many Sessions'
			// report marks one browser keeps; the least recently saved is dropped.
			Name: "console.copilot_update_marks", Class: UserInput, Unit: Rows,
			Limit: 100, AtLimit: EvictOldest, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// SCREEN_READ_ATTEMPTS in realtime/copilot-tools.ts: how many times
			// a pending screen capture is read again before the copilot says it
			// timed out.
			Name: "console.copilot_screen_reads", Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
		},
		{
			// ORB_MAX_FPS in realtime/copilot-orb.ts: how many times a second
			// the voice copilot's cloud orb is drawn. A display's extra frames
			// are skipped; nothing is drawn while the page is hidden.
			Name: "console.copilot_orb_frames_per_second", Class: Buffer, Unit: Rows,
			Limit: 30, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// ORB_MAX_PIXEL_RATIO in realtime/copilot-orb.ts: the most device
			// pixels per CSS pixel the orb's canvas is drawn at; a denser screen
			// is drawn at this ratio and scaled.
			Name: "console.copilot_orb_pixel_ratio", Class: Buffer, Unit: Multipliers,
			Limit: 2, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
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
			// While an Ops link waits for a Session to read it, the open Ops
			// page reads the list again at this pace (OPS_RESOLVING_POLL_MS,
			// pages/ops.tsx), because the agent's answer arrives with no event.
			Name: ConsoleOpsResolvingPoll, Class: Cache, Unit: Seconds,
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
