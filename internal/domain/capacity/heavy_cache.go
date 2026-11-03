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
	}, graphifyToolBounds()...)
}
