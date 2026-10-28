package capacity

// These limits belong to each hosted help tab; Cloud storage and job bounds are
// owned by the private support service's SUPPORT_LIMITS register.
func supportUIBounds() []Entry {
	return []Entry{
		{Name: "console.community_share_fetch_seconds", Class: Cache, Unit: Seconds, Limit: 3, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.community_share_response_bytes", Class: Buffer, Unit: Bytes, Limit: 1048576, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.community_share_text_characters", Class: Buffer, Unit: Rows, Limit: 160, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.support_image_count", Class: Buffer, Unit: Rows, Limit: 5, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.support_image_source_bytes", Class: Buffer, Unit: Bytes, Limit: 10485760, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.support_image_bytes", Class: Buffer, Unit: Bytes, Limit: 1048576, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.support_image_edge", Class: Buffer, Unit: Rows, Limit: 2048, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.support_image_upload_seconds", Class: Cache, Unit: Seconds, Limit: 60, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.support_request_seconds", Class: Cache, Unit: Seconds, Limit: 15, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.support_refresh_seconds", Class: Cache, Unit: Seconds, Limit: 30, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.support_title_characters", Class: Buffer, Unit: Rows, Limit: 160, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
	}
}
