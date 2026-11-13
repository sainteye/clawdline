package capacity

// artifactsBounds is every registered row named artifacts.*, icons.*,
// screens.* and voice.*: pictures, icons, screen captures and voice models.
func artifactsBounds() []Entry {
	return []Entry{
		{Name: "voice.work_auto_continue", Class: Buffer, Unit: Rows, Limit: 8, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/voicework.MaxAutoContinue"}},
		{Name: "voice.work_plan_rounds", Class: Buffer, Unit: Rows, Limit: 4, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/voicework.MaxPlanRounds"}},
		{Name: "voice.work_progress", Class: Buffer, Unit: Rows, Limit: 8, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/voicework.MaxProgress"}},
		{Name: "voice.work_request_milliseconds", Class: Buffer, Unit: Milliseconds, Limit: 10000, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon},
		{Name: "voice.work_execution_milliseconds", Class: Buffer, Unit: Milliseconds, Limit: 120000, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon},
		{Name: "voice.work_poll_milliseconds", Class: Buffer, Unit: Milliseconds, Limit: 1000, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "voice.work_heartbeat_milliseconds", Class: Buffer, Unit: Milliseconds, Limit: 30000, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "voice.work_records", Class: Buffer, Unit: Rows, Limit: 256, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/voicework.MaxRecords"}},
		{Name: "voice.work_text_characters", Class: Buffer, Unit: Characters, Limit: 4000, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/voicework.MaxTextRunes"}},
		{Name: "voice.work_state_bytes", Class: Buffer, Unit: Bytes, Limit: 2 << 20, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/voicework.MaxStateBytes"}},
		{Name: "voice.work_plan_inputs", Class: Buffer, Unit: Rows, Limit: 8, AtLimit: Coalesce, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/voicework.MaxPlanInputs"}},
		{Name: "voice.work_parallel_reads", Class: Buffer, Unit: Rows, Limit: 2, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/voicework.MaxParallelReads"}},
		{Name: "voice.work_lease_seconds", Class: Buffer, Unit: Seconds, Limit: 90, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/voicework.LeaseSecondsLimit"}},
		{Name: "voice.work_request_bytes", Class: Buffer, Unit: Bytes, Limit: 65536, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/voicework.MaxRequestBytes"}},
		{Name: "artifacts.output_files", Class: Buffer, Unit: Rows, Limit: 200, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/sessionfiles.MaximumFiles"}},
		{Name: "artifacts.output_index_bytes", Class: Buffer, Unit: Bytes, Limit: 2097152, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/sessionfiles.MaximumIndexBytes"}},
		{Name: "artifacts.output_path_bytes", Class: Buffer, Unit: Bytes, Limit: 4096, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/sessionfiles.MaximumPathBytes"}},
		// Conversation indexes are re-derivable from native transcripts, so the
		// least recently used one is let go instead of refusing every new one.
		{Name: "artifacts.output_sessions", Class: Cache, Unit: Rows, Limit: 256, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Log}, EvictedBy: Daemon, Sources: []string{"internal/adapters/sessionfiles.MaximumSessions"}},
		{Name: "artifacts.output_store_bytes", Class: Cache, Unit: Bytes, Limit: 67108864, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Log}, EvictedBy: Daemon, Sources: []string{"internal/adapters/sessionfiles.MaximumStoreBytes"}},
		{Name: "artifacts.video_bytes", Class: Buffer, Unit: Bytes, Limit: 32 * 1024 * 1024, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/documents.MaximumVideoBytes"}},
		{Name: "artifacts.video_chunk_bytes", Class: Buffer, Unit: Bytes, Limit: 512 * 1024, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/documents.MaximumVideoChunkBytes"}},
		{
			Name: "artifacts.report_total_image_pixels", Class: Buffer, Unit: Rows,
			Limit: 64 * 1024 * 1024, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/documents.MaximumReportImagePixels"},
		},
		{
			Name: "artifacts.report_image_side", Class: Buffer, Unit: Rows,
			Limit: 8192, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/documents.MaximumImageSide"},
		},
		{
			Name: "artifacts.report_image_pixels", Class: Buffer, Unit: Rows,
			Limit: 16 * 1024 * 1024, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/documents.MaximumImagePixels"},
		},
		{
			// Pictures behind <clawdline-image id> (limits N15). Every one
			// was stored to be shown in a reply, so every live one is
			// referenced. The daemon measures after each store, so warn and
			// critical are said before the store is full and before the
			// oldest is let go; a picture let go is counted, logged, and its
			// tombstone says why to whoever asks for it.
			Name: ArtifactsImages, Class: UserInput, Unit: Rows,
			Limit: 256, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice, Log},
			EvictedBy: Daemon,
		},
		{
			// The same pictures' bytes, the store's second bound (limits
			// N15): whichever of the two is reached lets the oldest go.
			Name: ArtifactsImageSize, Class: UserInput, Unit: Bytes,
			Limit: 64 << 20, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice, Log},
			EvictedBy: Daemon,
		},
		{
			// Pictures written out for a terminal program to read, each one
			// already typed into a prompt as a path (limits N16). The bytes
			// are what limits the cache: files past a week go on their own
			// (the reading's window), except the newest forty, and past this
			// many bytes the oldest go whatever their age. So this row reads
			// full only when the cap is what is removing pictures — which is
			// said in diagnostics and the log, and pushed only when the file
			// it removed was young (artifacts.drops_young).
			Name: ArtifactsDrops, Class: UserInput, Unit: Bytes,
			Limit: 256 << 20, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources: []string{"internal/adapters/artifacts.MaxDropsBytes",
				"internal/adapters/artifacts.DropsAgeLimit"},
		},
		{
			// Pictures the byte cap of artifacts.drops removed while they
			// were less than a day old, within the last day. Each was typed
			// into a prompt recently enough to be read again after a
			// compaction or a resume, so one is the cache too small for how
			// it is used, and it is pushed; a day without one is written
			// down and not announced. The limit is one: any at all.
			Name: ArtifactsDropsYoung, Class: UserInput, Unit: Rows,
			Limit: 1, AtLimit: EvictOldest,
			Told:          []Channel{Diagnostics, Notice, Log},
			EvictedBy:     Daemon,
			QuietRecovery: true,
			Sources:       []string{"internal/adapters/artifacts.DropsYoungLimit"},
		},
		{
			// The screen captures the session list may have in flight at once
			// (internal/app/screen_held.go). This is the row that bounds how
			// deep a queue this daemon can put in front of a person's own
			// keystroke: every iTerm2 capture is an AppleScript behind one
			// process-wide Apple Events lock, and the list used to start one
			// per qualifying row per reader — twenty rows times three loops,
			// each waiting for all the ones before it. At this limit a refresh
			// is not started at all; the reader is answered from the held
			// screen and the skip is counted, because the alternative is a
			// queue whose length is the number of rows.
			Name: ScreensCaptureSlots, Class: Buffer, Unit: Rows,
			Limit: 2, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.ScreenCaptureLimit"},
		},
		{
			Name: "icons.saved", Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person,
			Sources: []string{"internal/domain/icon.MaxIconOverrides"},
		},
		{
			Name: "icons.side", Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/icon.MaxIconSide"},
		},
		{
			Name: "icons.request_bytes", Class: Buffer, Unit: Bytes,
			Limit: 96 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/icon.MaxIconRequestBytes"},
		},
		{
			// The one model offered in Settings. A response one byte larger than
			// the pinned upstream object is refused, its partial file is removed,
			// and the prior installed model (if any) stays untouched.
			Name: VoiceModelDownloadBytes, Class: Buffer, Unit: Bytes,
			Limit: 574041195, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/whisper.ModelDownloadBytesLimit"},
		},
		{
			// The person's own dictation terms (voice_vocabulary). A longer
			// list or a longer term is refused by the settings route.
			Name: "voice.vocabulary_terms", Class: Buffer, Unit: Rows,
			Limit: 200, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person,
			Sources: []string{"internal/adapters/nextconfig.MaxVoiceVocabularyTerms"},
		},
		{
			Name: "voice.vocabulary_term_characters", Class: Buffer, Unit: Characters,
			Limit: 64, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person,
			Sources: []string{"internal/adapters/nextconfig.MaxVoiceVocabularyRunes"},
		},
		{
			// Project folder names that join the vocabulary when ticked: the
			// forty the project picker lists. Further projects are not added.
			Name: "voice.vocabulary_project_terms", Class: Buffer, Unit: Rows,
			Limit: 40, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.voiceProjectTermLimit"},
		},
		{
			// How many terms one cleanup turn is shown; the rest are still
			// repaired locally by whisper.ApplyVocabulary.
			Name: "voice.polish_terms", Class: Buffer, Unit: Rows,
			Limit: 240, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/planner.MaxPolishTerms"},
		},
		{
			// A longer transcript is answered unchanged, `polish.status`
			// too_long, rather than spending a turn on it.
			Name: "voice.polish_characters", Class: Buffer, Unit: Characters,
			Limit: 4000, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/planner.MaxPolishRunes"},
		},
		{
			// A slower cleanup is abandoned and the Whisper text answered,
			// `polish.status` timeout: the person is waiting at the composer.
			Name: "voice.polish_timeout_milliseconds", Class: Buffer, Unit: Milliseconds,
			Limit: 15000, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/planner.MaxPolishDuration"},
		},
		{
			// The voice copilot's prepared briefing: a slower turn is abandoned
			// and the copilot reads the Sessions itself during the call.
			Name: "voice.copilot_briefing_timeout_milliseconds", Class: Buffer, Unit: Milliseconds,
			Limit: 90000, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/planner.MaxBriefingDuration"},
		},
		{
			// What one briefing turn is given; more is refused before a turn.
			Name: "voice.copilot_briefing_input_characters", Class: Buffer, Unit: Characters,
			Limit: 60000, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/planner.MaxBriefingInputRunes"},
		},
		{
			// A longer answer is not handed to the copilot: it rides in every
			// response's input for the rest of the call.
			Name: "voice.copilot_briefing_characters", Class: Buffer, Unit: Characters,
			Limit: 2500, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/planner.MaxBriefingRunes"},
		},
		{
			// How much of each Session's last reply a briefing reads; a longer
			// reply keeps its beginning and its end, where the question is.
			Name: "voice.copilot_briefing_reply_characters", Class: Buffer, Unit: Characters,
			Limit: 1500, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxBriefingReplyRunes"},
		},
		{
			// One spoken sentence the voice copilot's Decision API judges.
			Name: "voice.copilot_decision_said_characters", Class: Buffer, Unit: Characters,
			Limit: 1000, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person,
			Sources: []string{"internal/transport/http.maxCopilotDecisionSaidRunes"},
		},
		{
			// The decision route's request: one sentence as JSON, with room for escaping.
			Name: "voice.copilot_decision_body_bytes", Class: Buffer, Unit: Bytes,
			Limit: 65536, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person,
			Sources: []string{"internal/transport/http.copilotDecisionBodyLimit"},
		},
		{
			// The Sessions a call may hand the decision to choose among.
			Name: "voice.copilot_decision_sessions", Class: Buffer, Unit: Rows,
			Limit: 200, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxCopilotDecisionSessions"},
		},
		{
			// A slower decision is abandoned; the copilot chooses from the list itself.
			Name: "voice.copilot_decision_timeout_milliseconds", Class: Buffer, Unit: Milliseconds,
			Limit: 8000, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxCopilotDecisionDuration"},
		},
		{
			// One call journal request: a turn or two of lines.
			Name: "voice.copilot_journal_body_bytes", Class: Buffer, Unit: Bytes,
			Limit: 65536, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.copilotJournalBodyLimit"},
		},
		{
			// How many lines one journal request may append.
			Name: "voice.copilot_journal_entries", Class: Buffer, Unit: Rows,
			Limit: 200, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxCopilotJournalEntries"},
		},
		{
			// How much of one journal line is kept; the rest is cut.
			Name: "voice.copilot_journal_text_characters", Class: Buffer, Unit: Characters,
			Limit: 4000, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxCopilotJournalTextRunes"},
		},
		{
			// How long call journals are kept; an older day's file is removed.
			Name: "voice.copilot_journal_seconds", Class: Observation, Unit: Seconds,
			Limit: 1209600, AtLimit: EvictOldest, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxCopilotJournalAge"},
		},
		{
			// A pasted OpenAI key longer than this is refused by the settings route.
			Name: "voice.copilot_key_bytes", Class: Buffer, Unit: Bytes,
			Limit: 400, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person,
			Sources: []string{"internal/domain/copilot.MaxKeyBytes"},
		},
		{
			// The person's own extra instructions, carried in every response.
			Name: "voice.copilot_extra_instructions_characters", Class: Buffer, Unit: Characters,
			Limit: 2000, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person,
			Sources: []string{"internal/domain/copilot.MaxExtraInstructionsRunes"},
		},
	}
}
