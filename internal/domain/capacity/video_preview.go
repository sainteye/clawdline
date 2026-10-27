package capacity

// Video preview policy is shared with the browser's lazy poster reader.
const (
	MaximumVideoPreviewReads        = 1
	MaximumVideoPreviewSide         = 640
	MaximumVideoPreviewBytes        = 2 * 1024 * 1024
	MaximumVideoPreviewMilliseconds = 30000
)
