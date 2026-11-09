package capacity

// Browser dictation retains one recording in memory until use or explicit discard.
const (
	MaximumVoiceRecordingBytes      = 16777216
	MaximumVoiceStopMilliseconds    = 5000
	MaximumVoicePrepareMilliseconds = 15000
	MaximumVoiceReadMilliseconds    = 120000
)
