package capacity

// These limits belong to each hosted help tab; Cloud storage and job bounds are
// owned by the private support service's SUPPORT_LIMITS register.
func supportUIBounds() []Entry {
	return []Entry{
		{Name: "console.support_request_seconds", Class: Cache, Unit: Seconds, Limit: 15, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.support_refresh_seconds", Class: Cache, Unit: Seconds, Limit: 30, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.support_title_characters", Class: Buffer, Unit: Rows, Limit: 160, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
	}
}
