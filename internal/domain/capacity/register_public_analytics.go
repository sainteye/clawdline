package capacity

// Hosted report-reader bounds. Cloud collection and storage have a separate register.
func publicAnalyticsUIBounds() []Entry {
	return []Entry{
		{Name: "console.public_analytics_days", Class: Buffer, Unit: Rows, Limit: 90, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person},
		{Name: "console.public_analytics_rows", Class: Buffer, Unit: Rows, Limit: 20, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person},
		{Name: "console.public_analytics_countries", Class: Buffer, Unit: Rows, Limit: 250, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person},
		{Name: "console.public_analytics_page", Class: Buffer, Unit: Rows, Limit: 10000, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person},
		{Name: "console.public_analytics_readms", Class: Cache, Unit: Milliseconds, Limit: 10000, AtLimit: Expire, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon},
		{Name: "console.public_analytics_path", Class: Buffer, Unit: Characters, Limit: 300, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person},
		{Name: "console.public_analytics_activems", Class: Buffer, Unit: Milliseconds, Limit: 1800000, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Person},
	}
}
