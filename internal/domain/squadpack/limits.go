// Package squadpack reads and writes offline squad definitions. All bytes in
// an archive are untrusted until Parse has verified the complete ZIP.
package squadpack

const (
	MaxArchiveBytes   = 512 << 10
	MaxRequestBytes   = 1 << 20
	MaxManifestBytes  = 128 << 10
	MaxFileBytes      = 64 << 10
	MaxExpandedBytes  = 4 << 20
	MaxEntries        = 128
	MaxExpansionRatio = 64
)
