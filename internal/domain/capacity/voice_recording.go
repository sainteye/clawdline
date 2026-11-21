package capacity

// Browser dictation retains one recording in memory until use or explicit discard.
const (
	MaximumVoiceRecordingBytes        = 16777216
	WhisperPreviewSeconds             = 3
	MaximumWhisperPreviewPCMBytes     = WhisperPreviewSeconds * 16000 * 2
	WhisperPreviewEncodedBytes        = MaximumWhisperPreviewPCMBytes / 3 * 4
	MaximumWhisperPreviewBodyBytes    = WhisperPreviewEncodedBytes + 1024
	MaximumWhisperPreviewMilliseconds = 5000
	MaximumVoiceStopMilliseconds      = 5000
	MaximumVoicePrepareMilliseconds   = 15000
	MaximumVoiceReadMilliseconds      = 120000
)
