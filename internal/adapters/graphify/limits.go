// Package graphify provides local graph creation and read-only source checks.
package graphify

const (
	MaxMetadataBytes  = 64 << 20
	MaxFileBytes      = 16 << 20
	MaxTotalBytes     = 256 << 20
	MaxScanEntries    = 20000
	CheckSecondsLimit = 5
)
