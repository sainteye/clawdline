package capacity

func healthInvestigationBounds() []Entry {
	return []Entry{
		{Name: "cloud.health_investigation_bundle_bytes", Class: Buffer, Unit: Bytes, Limit: 1 << 20, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/schedulewebhook.MaxInvestigationBundleBytes"}},
		{Name: "cloud.health_investigation_report_bytes", Class: Buffer, Unit: Bytes, Limit: 2 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/schedulewebhook.MaxInvestigationReportBytes"}},
		{Name: "cloud.health_investigation_report_refs", Class: Buffer, Unit: Rows, Limit: 8, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/schedulewebhook.MaxInvestigationReportRefs"}},
		{Name: "cloud.health_investigation_read_seconds", Class: Buffer, Unit: Seconds, Limit: 5, AtLimit: Disconnect, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/schedulewebhook.MaxInvestigationReadSeconds"}},
	}
}
