package capacity

// The browser enforces these bounds without granting plugin code a daemon API.
const (
	MaximumViewPluginArchiveBytes        = 33554432
	MaximumViewPluginExpandedBytes       = 33554432
	MaximumViewPluginManifestBytes       = 8192
	MaximumViewPluginCoverSide           = 256
	MaximumViewPluginPackages            = 8
	MaximumViewPluginFiles               = 2
	MaximumViewPluginMessageBytes        = 1048576
	MaximumViewPluginSessions            = 256
	MaximumViewPluginPending             = 16
	MaximumViewPluginRequestMilliseconds = 10000
	MaximumViewPluginRequestsPerSecond   = 32
	MaximumViewPluginTextCharacters      = 256
	MaximumViewPluginCatalogBytes        = 1048576
	MaximumViewPluginCatalogEntries      = 256
	MaximumViewPluginUpdateMilliseconds  = 60000
)

func consoleBounds() []Entry { return append(baseConsoleBounds(), viewPluginBounds()...) }

func viewPluginBounds() []Entry {
	return []Entry{
		{Name: "console.view_plugin_catalog_bytes", Class: Buffer, Unit: Bytes, Limit: MaximumViewPluginCatalogBytes, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginCatalogBytes"}},
		{Name: "console.view_plugin_catalog_entries", Class: Buffer, Unit: Rows, Limit: MaximumViewPluginCatalogEntries, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginCatalogEntries"}},
		{Name: "console.view_plugin_update_milliseconds", Class: Buffer, Unit: Milliseconds, Limit: MaximumViewPluginUpdateMilliseconds, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginUpdateMilliseconds"}},
		{Name: "console.view_plugin_files", Class: Buffer, Unit: Rows, Limit: MaximumViewPluginFiles, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginFiles"}},
		{Name: "console.view_plugin_archive_bytes", Class: Buffer, Unit: Bytes, Limit: MaximumViewPluginArchiveBytes, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginArchiveBytes"}},
		{Name: "console.view_plugin_expanded_bytes", Class: Buffer, Unit: Bytes, Limit: MaximumViewPluginExpandedBytes, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginExpandedBytes"}},
		{Name: "console.view_plugin_manifest_bytes", Class: Buffer, Unit: Bytes, Limit: MaximumViewPluginManifestBytes, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginManifestBytes"}},
		{Name: "console.view_plugin_cover_side", Class: Buffer, Unit: Rows, Limit: MaximumViewPluginCoverSide, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginCoverSide"}},
		{Name: "console.view_plugin_packages", Class: Buffer, Unit: Rows, Limit: MaximumViewPluginPackages, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginPackages"}},
		{Name: "console.view_plugin_message_bytes", Class: Buffer, Unit: Bytes, Limit: MaximumViewPluginMessageBytes, AtLimit: Disconnect, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginMessageBytes"}},
		{Name: "console.view_plugin_sessions", Class: Buffer, Unit: Rows, Limit: MaximumViewPluginSessions, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginSessions"}},
		{Name: "console.view_plugin_pending", Class: Buffer, Unit: Rows, Limit: MaximumViewPluginPending, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginPending"}},
		{Name: "console.view_plugin_request_milliseconds", Class: Buffer, Unit: Milliseconds, Limit: MaximumViewPluginRequestMilliseconds, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginRequestMilliseconds"}},
		{Name: "console.view_plugin_requests_per_second", Class: Buffer, Unit: Rows, Limit: MaximumViewPluginRequestsPerSecond, AtLimit: Disconnect, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginRequestsPerSecond"}},
		{Name: "console.view_plugin_text_characters", Class: Buffer, Unit: Characters, Limit: MaximumViewPluginTextCharacters, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumViewPluginTextCharacters"}},
	}
}
