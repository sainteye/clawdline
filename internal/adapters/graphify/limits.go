// Package graphify provides optional, read-only checks for a local code graph.
package graphify

const (
	MaxMetadataBytes  = 16 << 20
	MaxFileBytes      = 16 << 20
	MaxTotalBytes     = 128 << 20
	MaxScanEntries    = 20000
	CheckSecondsLimit = 5
)
