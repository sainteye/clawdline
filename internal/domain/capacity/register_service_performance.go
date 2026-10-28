package capacity

// Browser guards and remote service monitoring budgets. Remote occupancy is not
// inferred from this daemon; the Admin monitor reports its own state.
func servicePerformanceUIBounds() []Entry {
	return []Entry{
		{Name: "console.service_performance_body_bytes", Class: Buffer, Unit: Bytes, Limit: 1048576, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.service_performance_points", Class: Buffer, Unit: Rows, Limit: 1440, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.service_performance_routes", Class: Buffer, Unit: Rows, Limit: 32, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.service_performance_deployments", Class: Buffer, Unit: Rows, Limit: 100, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.service_performance_alerts", Class: Buffer, Unit: Rows, Limit: 30, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.service_performance_refresh_ms", Class: Cache, Unit: Milliseconds, Limit: 30000, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.service_performance_timeout_ms", Class: Cache, Unit: Milliseconds, Limit: 6000, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.local_performance_body_bytes", Class: Buffer, Unit: Bytes, Limit: 2097152, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.local_performance_points", Class: Buffer, Unit: Rows, Limit: 168, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.local_performance_routes", Class: Buffer, Unit: Rows, Limit: 129, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.local_performance_refresh_ms", Class: Cache, Unit: Milliseconds, Limit: 30000, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "console.local_performance_timeout_ms", Class: Cache, Unit: Milliseconds, Limit: 6000, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
	}
}

func cloudServiceMonitoringBounds() []Entry {
	return []Entry{
		{Name: "cloud.service_monitor_minute_retention_seconds", Class: Cache, Unit: Seconds, Limit: 86400, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_sample_seconds", Class: Cache, Unit: Seconds, Limit: 30, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_retention_seconds", Class: Cache, Unit: Seconds, Limit: 604800, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_store_bytes", Class: Buffer, Unit: Bytes, Limit: 100663296, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_batch_bytes", Class: Buffer, Unit: Bytes, Limit: 32768, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_routes", Class: Buffer, Unit: Rows, Limit: 32, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_batches_per_hour", Class: Buffer, Unit: Rows, Limit: 240, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_query_seconds", Class: Cache, Unit: Seconds, Limit: 2, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_query_bytes", Class: Buffer, Unit: Bytes, Limit: 1048576, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_query_points", Class: Buffer, Unit: Rows, Limit: 288, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_readers", Class: Buffer, Unit: Rows, Limit: 4, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_delete_rows", Class: Buffer, Unit: Rows, Limit: 500, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_deployment_rows", Class: Buffer, Unit: Rows, Limit: 100, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_fresh_seconds", Class: Cache, Unit: Seconds, Limit: 90, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_alert_attempts", Class: Buffer, Unit: Rows, Limit: 6, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_outbox_rows", Class: Buffer, Unit: Rows, Limit: 256, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_active_requests", Class: Buffer, Unit: Rows, Limit: 512, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_minute_buckets", Class: Buffer, Unit: Rows, Limit: 3, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_upload_attempts", Class: Buffer, Unit: Rows, Limit: 3, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_upload_deadline_ms", Class: Cache, Unit: Milliseconds, Limit: 2500, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_alert_targets", Class: Buffer, Unit: Rows, Limit: 32, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_alert_lease_seconds", Class: Cache, Unit: Seconds, Limit: 30, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_host_command_seconds", Class: Cache, Unit: Seconds, Limit: 1, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_source_bytes", Class: Buffer, Unit: Bytes, Limit: 65536, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_runtime_memory_bytes", Class: Buffer, Unit: Bytes, Limit: 134217728, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_runtime_tasks", Class: Buffer, Unit: Rows, Limit: 32, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
		{Name: "cloud.service_monitor_wal_bytes", Class: Buffer, Unit: Bytes, Limit: 16777216, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon},
	}
}
