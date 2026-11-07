// Package codegraph provides local graph creation and read-only source checks.
package codegraph

const (
	MaxMetadataBytes        = 64 << 20
	MaxFileBytes            = 16 << 20
	MaxTotalBytes           = 256 << 20
	MaxScanEntries          = 20000
	MaxIgnoredListBytes     = 4 << 20
	CheckSecondsLimit       = 5
	ServiceRootsLimit       = 4
	ServiceIdleSecondsLimit = 600
	ReconcileSecondsLimit   = 60
	QueryBytesLimit         = 4096
	QueryBodyBytesLimit     = 16 << 10
	ReplyBytesLimit         = 256 << 10
	QuerySecondsLimit       = 15
	StoreBytesLimit         = 512 << 20
)
