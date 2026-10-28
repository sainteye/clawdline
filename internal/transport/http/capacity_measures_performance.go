package http

import "github.com/sainteye/clawdline/internal/domain/capacity"

func performanceCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"cloud.service_monitor_minute_retention_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"console.service_performance_body_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no browser performance state; another page occupancy is unavailable here"}
		},
		"console.service_performance_points": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no browser performance state; another page occupancy is unavailable here"}
		},
		"console.service_performance_routes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no browser performance state; another page occupancy is unavailable here"}
		},
		"console.service_performance_deployments": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no browser performance state; another page occupancy is unavailable here"}
		},
		"console.service_performance_alerts": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no browser performance state; another page occupancy is unavailable here"}
		},
		"console.service_performance_refresh_ms": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no browser performance state; another page occupancy is unavailable here"}
		},
		"console.service_performance_timeout_ms": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no browser performance state; another page occupancy is unavailable here"}
		},
		"console.local_performance_body_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no browser performance state; another page occupancy is unavailable here"}
		},
		"console.local_performance_points": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no browser performance state; another page occupancy is unavailable here"}
		},
		"console.local_performance_routes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no browser performance state; another page occupancy is unavailable here"}
		},
		"console.local_performance_refresh_ms": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no browser performance state; another page occupancy is unavailable here"}
		},
		"console.local_performance_timeout_ms": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no browser performance state; another page occupancy is unavailable here"}
		},
		"cloud.service_monitor_sample_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_retention_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_store_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_batch_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_routes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_batches_per_hour": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_query_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_query_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_history_points": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_query_points": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_readers": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_delete_rows": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_deployment_rows": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_fresh_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_alert_attempts": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_outbox_rows": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_active_requests": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_minute_buckets": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_upload_attempts": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_upload_deadline_ms": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_alert_targets": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_alert_lease_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_host_command_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_source_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_runtime_memory_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_runtime_tasks": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
		"cloud.service_monitor_wal_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this daemon holds no service-monitor state; remote occupancy is unavailable here; inspect Admin monitoring"}
		},
	}
}
