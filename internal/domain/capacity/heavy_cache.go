package capacity

// MaxHeavyGoCacheKiB bounds only the Linux heavy wrapper's owned Go caches.
// The wrapper reads these decimal constants without compiling a policy reader.
const MaxHeavyGoCacheKiB = 4194304

// MaxHeavyGoDiskReserveKiB is the free-space floor before a managed build.
const MaxHeavyGoDiskReserveKiB = 2097152

// ToolRegister records bounds owned and measured by repository tools rather
// than the running daemon. Both register guards include these entries; daemon
// diagnostics and their copy/measurement guards use Register alone.
func ToolRegister() []Entry {
	return append([]Entry{
		{
			Name: "build.go_cache_bytes", Class: Cache, Unit: Bytes,
			Limit: MaxHeavyGoCacheKiB * 1024, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaxHeavyGoCacheKiB"},
		},
		{
			Name: "build.disk_reserve_bytes", Class: Buffer, Unit: Bytes,
			Limit: MaxHeavyGoDiskReserveKiB * 1024, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaxHeavyGoDiskReserveKiB"},
		},
		{
			Name: "build.shared_go_cache_bytes", Class: Cache, Unit: Bytes,
			Limit: MaxSharedGoCacheBytes, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Log}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaxSharedGoCacheBytes"},
		},
		{
			Name: "build.shared_go_cache_reserve_bytes", Class: Cache, Unit: Bytes,
			Limit: MaxSharedGoCacheReserveBytes, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Log}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaxSharedGoCacheReserveBytes"},
		},
		{
			Name: "build.shared_go_cache_protect_seconds", Class: Cache, Unit: Seconds,
			Limit: MaxSharedGoCacheProtectSeconds, AtLimit: Expire,
			Told: []Channel{Diagnostics, Log}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaxSharedGoCacheProtectSeconds"},
		},
		{
			Name: "build.shared_go_cache_scan_files", Class: Buffer, Unit: Rows,
			Limit: MaxSharedGoCacheScanFiles, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Log}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaxSharedGoCacheScanFiles"},
		},
	}, codegraphToolBounds()...)
}

// The default shared Go build cache (`go env GOCACHE`) on machines without the
// Linux heavy wrapper. Go itself only removes entries unused for five days and
// has no total ceiling; `clawdline go-cache trim` keeps it under this budget,
// oldest-used recognized entries first, between heavy commands.
const (
	MaxSharedGoCacheBytes int64 = 20 << 30
	// MaxSharedGoCacheReserveBytes also triggers trimming when the cache's
	// filesystem has less free space than this, even under the budget.
	MaxSharedGoCacheReserveBytes int64 = 30 << 30
	// MaxSharedGoCacheProtectSeconds keeps every entry used this recently: Go
	// refreshes an entry's mtime at most hourly, so a running build's entries
	// are never older than this.
	MaxSharedGoCacheProtectSeconds int64 = 2 * 60 * 60
	// MaxSharedGoCacheScanFiles bounds one trim's listing; a larger cache is
	// unmeasured and nothing is removed.
	MaxSharedGoCacheScanFiles int64 = 4_000_000
)
