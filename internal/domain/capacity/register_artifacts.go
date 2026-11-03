package capacity

// artifactsBounds is every registered row named artifacts.*, icons.*,
// screens.* and voice.*: pictures, icons, screen captures and voice models.
func artifactsBounds() []Entry {
	return []Entry{
		{Name: "artifacts.output_files", Class: Buffer, Unit: Rows, Limit: 200, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/sessionfiles.MaximumFiles"}},
		{Name: "artifacts.output_index_bytes", Class: Buffer, Unit: Bytes, Limit: 2097152, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/sessionfiles.MaximumIndexBytes"}},
		{Name: "artifacts.output_path_bytes", Class: Buffer, Unit: Bytes, Limit: 4096, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/sessionfiles.MaximumPathBytes"}},
		{Name: "artifacts.output_sessions", Class: Buffer, Unit: Rows, Limit: 256, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/sessionfiles.MaximumSessions"}},
		{Name: "artifacts.output_store_bytes", Class: Buffer, Unit: Bytes, Limit: 67108864, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/sessionfiles.MaximumStoreBytes"}},
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
	}
}
