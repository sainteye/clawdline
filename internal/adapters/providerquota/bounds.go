// Package providerquota supplies read-only account quota snapshots to the global footer.
package providerquota

const (
	CacheLimit          = 4
	ResponseBytesLimit  = 1 << 20
	RefreshSecondsLimit = 15 * 60
	RequestSecondsLimit = 10
)
